package backtest

import (
	"encoding/json"
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"math"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

var paperTradingMu sync.Mutex

// PaperTradingDetails 是前向验证页面使用的完整账户视图。
type PaperTradingDetails struct {
	Account   models.PredictionPaperAccount    `json:"account"`
	Positions []models.PredictionPaperPosition `json:"positions"`
	Trades    []models.PredictionPaperTrade    `json:"trades"`
	Dailies   []models.PredictionPaperDaily    `json:"dailies"`
	Ready     bool                             `json:"ready"`
	Reason    string                           `json:"reason"`
}

func ensurePaperAccount(tx *gorm.DB, hypothesis *models.PredictionHypothesis) (*models.PredictionPaperAccount, error) {
	var account models.PredictionPaperAccount
	err := tx.Where("hypothesis_id = ?", hypothesis.ID).First(&account).Error
	if err == nil {
		return &account, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	config := normalizeBacktestConfig(configFromHypothesis(*hypothesis))
	startDate := shanghaiNow().Format("2006-01-02")
	if hypothesis.PaperTradeStartedAt != nil {
		startDate = hypothesis.PaperTradeStartedAt.In(shanghaiLocation()).Format("2006-01-02")
	}
	account = models.PredictionPaperAccount{
		HypothesisID: hypothesis.ID,
		InitialCash:  config.InitialCapital,
		Cash:         config.InitialCapital,
		TotalValue:   config.InitialCapital,
		Nav:          1,
		MaxNav:       1,
		Status:       "running",
		StartDate:    startDate,
	}
	if err := tx.Create(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func configFromHypothesis(h models.PredictionHypothesis) BacktestConfig {
	config := DefaultBacktestConfig()
	if strings.TrimSpace(h.BacktestConfigJSON) != "" {
		_ = json.Unmarshal([]byte(h.BacktestConfigJSON), &config)
	}
	return normalizeBacktestConfig(config)
}

// ProcessPaperTrading 将所有前向验证/正式监控策略推进到最新可用交易日。
func (s *PredictionService) ProcessPaperTrading() (*PredictionSignalValidationResult, error) {
	result := &PredictionSignalValidationResult{}
	if err := ensurePredictionTables(); err != nil {
		return result, err
	}
	paperTradingMu.Lock()
	defer paperTradingMu.Unlock()

	latest, err := latestAdjustedFeatureDate()
	if err != nil || latest == "" {
		if err == nil {
			err = fmt.Errorf("没有可用于前向验证的前复权特征")
		}
		return result, err
	}
	var hypotheses []models.PredictionHypothesis
	if err := db.Dao.Where("status IN ?", []string{"paper_trade", "active"}).Find(&hypotheses).Error; err != nil {
		return result, err
	}
	result.Accounts = len(hypotheses)
	for i := range hypotheses {
		if err := s.processPaperHypothesis(&hypotheses[i], latest, result); err != nil {
			result.Failed++
			loggerPaperError(hypotheses[i].ID, err)
		}
	}
	if result.Failed > 0 {
		return result, fmt.Errorf("前向验证存在 %d 个策略处理失败", result.Failed)
	}
	return result, nil
}

func (s *PredictionService) processPaperHypothesis(h *models.PredictionHypothesis, latest string, result *PredictionSignalValidationResult) error {
	rule, err := JSONToRule(h.RuleJSON)
	if err != nil {
		return fmt.Errorf("解析策略规则失败: %w", err)
	}
	var account *models.PredictionPaperAccount
	if err := db.Dao.Transaction(func(tx *gorm.DB) error {
		var createErr error
		account, createErr = ensurePaperAccount(tx, h)
		return createErr
	}); err != nil {
		return err
	}
	dates, err := paperTradingDates(account.StartDate, account.LastProcessedDate, latest)
	if err != nil {
		return err
	}
	for _, date := range dates {
		if err := s.processPaperTradingDate(h, account, rule, date, result); err != nil {
			return fmt.Errorf("处理 %s 失败: %w", date, err)
		}
		result.ProcessedDays++
	}
	return nil
}

func paperTradingDates(startDate, lastProcessedDate, latest string) ([]string, error) {
	from := startDate
	operator := ">="
	if lastProcessedDate != "" {
		from = lastProcessedDate
		operator = ">"
	}
	var dates []string
	query := fmt.Sprintf("date %s ? AND date <= ? AND feature_version = ? AND adjusted = ?", operator)
	err := db.Dao.Model(&models.StockFeature{}).
		Where(query, from, latest, CurrentFeatureVersion, true).
		Distinct("date").Order("date asc").Pluck("date", &dates).Error
	return dates, err
}

func (s *PredictionService) processPaperTradingDate(
	h *models.PredictionHypothesis,
	account *models.PredictionPaperAccount,
	rule Rule,
	date string,
	result *PredictionSignalValidationResult,
) error {
	config := configFromHypothesis(*h)
	strategy := NewStrategyEngine()
	risk := NewRiskEngine(strategy)
	orders := NewOrderBuilder()
	fillSimulator := NewFillSimulator(config)
	now := time.Now()

	return db.Dao.Transaction(func(tx *gorm.DB) error {
		var positions []models.PredictionPaperPosition
		if err := tx.Where("account_id = ?", account.ID).Order("id asc").Find(&positions).Error; err != nil {
			return err
		}
		var pendingSignals []models.PredictionSignal
		if err := tx.Where("hypothesis_id = ? AND status = ? AND signal_date < ?", h.ID, "pending_entry", date).
			Order("signal_date asc, id asc").Find(&pendingSignals).Error; err != nil {
			return err
		}
		result.Pending += len(pendingSignals)

		codes := make([]string, 0, len(positions)+len(pendingSignals))
		for i := range positions {
			codes = append(codes, positions[i].StockCode)
		}
		for i := range pendingSignals {
			codes = append(codes, pendingSignals[i].StockCode)
		}
		codes = uniqueStrings(codes)
		var features []models.StockFeature
		if len(codes) > 0 {
			features = NewFeatureRepositoryForVersion(config.FeatureVersion).GetByDate(date, codes)
		}
		featureMap := stockFeatureMap(features)
		previousMap := previousPaperFeatures(tx, codes, date, config.FeatureVersion)
		dayIndex := account.TradingDays
		closedToday := 0

		// 收盘退出条件在下一交易日开盘成交；若停牌或跌停则继续排队。
		remaining := positions[:0]
		for i := range positions {
			pos := positions[i]
			feature, available := featureMap[pos.StockCode]
			if pos.PendingExitReason == "" || !available {
				remaining = append(remaining, pos)
				continue
			}
			portfolioPos := paperPortfolioPosition(pos)
			order := orders.BuildExitOrder(portfolioPos, date, entryPrice(feature, config), pos.PendingExitReason)
			fill, filled := fillSimulator.Fill(order, feature, featurePointer(previousMap, pos.StockCode), date)
			if !filled {
				remaining = append(remaining, pos)
				continue
			}
			if err := closePaperPosition(tx, account, &pos, fill, dayIndex, pos.PendingExitReason, now); err != nil {
				return err
			}
			closedToday++
			result.Validated++
		}
		positions = remaining

		// 同一策略内对全部昨日信号统一排序、统一仓位预算，避免逐只固定100股。
		held := make(map[string]bool, len(positions))
		for i := range positions {
			held[positions[i].StockCode] = true
		}
		signals := make([]Signal, 0, len(pendingSignals))
		signalByCode := make(map[string]*models.PredictionSignal, len(pendingSignals))
		priceHints := make(map[string]float64, len(features))
		for _, feature := range features {
			priceHints[feature.StockCode] = entryPrice(feature, config)
		}
		for i := range pendingSignals {
			signal := &pendingSignals[i]
			feature, available := featureMap[signal.StockCode]
			if !available {
				continue
			}
			signals = append(signals, Signal{
				StockCode: signal.StockCode, StockName: signal.StockName, Date: signal.SignalDate,
				Price: entryPrice(feature, config), Score: s.calculateSignalScore(*h, feature), ReasonJSON: signal.ReasonsJSON,
			})
			signalByCode[signal.StockCode] = signal
		}
		totalBeforeEntry := paperTotalValue(account.Cash, positions, featureMap)
		entryOrders := orders.BuildEntryOrders(signals, priceHints, account.Cash, totalBeforeEntry, held, rule.MaxHoldings, config.LotSize, date)
		enteredSignalIDs := make(map[uint]bool)
		for _, order := range entryOrders {
			feature, available := featureMap[order.StockCode]
			signal := signalByCode[order.StockCode]
			if !available || signal == nil {
				continue
			}
			previous := featurePointer(previousMap, order.StockCode)
			order.Quantity = affordableLotQuantityForOrder(account.Cash, order, feature, previous, config, date)
			fill, filled := fillSimulator.Fill(order, feature, previous, date)
			if !filled || fill.NetAmount <= 0 || fill.NetAmount > account.Cash+1e-6 {
				continue
			}
			account.Cash -= fill.NetAmount
			dataAsOf := feature.DataAsOf
			if dataAsOf.IsZero() {
				dataAsOf = now
			}
			position := models.PredictionPaperPosition{
				AccountID: account.ID, HypothesisID: h.ID, SignalID: signal.ID,
				StockCode: order.StockCode, StockName: firstNonBlank(order.StockName, order.StockCode),
				SignalDate: signal.SignalDate, EntryDate: date, EntryPrice: fill.Price,
				AvgCost: fill.NetAmount / fill.Quantity, CostAmount: fill.NetAmount,
				EntryFee: fill.Fee, EntrySlippage: fill.SlippageCost, Quantity: fill.Quantity,
				MaxPrice: fill.Price, MinPrice: fill.Price, LastPrice: fill.Price, BuyDayIndex: dayIndex,
				EntryReasonJSON: order.ReasonJSON, FeatureVersion: feature.FeatureVersion, DataAsOf: dataAsOf,
			}
			if err := tx.Create(&position).Error; err != nil {
				return err
			}
			if err := tx.Model(signal).Updates(map[string]any{
				"entry_date": date, "entry_price": fill.Price, "status": "pending", "target_date": "",
			}).Error; err != nil {
				return err
			}
			positions = append(positions, position)
			enteredSignalIDs[signal.ID] = true
			result.Entered++
		}
		for i := range pendingSignals {
			if enteredSignalIDs[pendingSignals[i].ID] {
				continue
			}
			if err := tx.Model(&pendingSignals[i]).Updates(map[string]any{
				"status": "missed_entry", "validated_at": now,
			}).Error; err != nil {
				return err
			}
			result.Missed++
		}

		// 以当日高低收执行止盈止损、策略退出和最大持有期。
		stillOpen := positions[:0]
		for i := range positions {
			pos := positions[i]
			feature, available := featureMap[pos.StockCode]
			if !available {
				stillOpen = append(stillOpen, pos)
				continue
			}
			portfolioPos := paperPortfolioPosition(pos)
			updatePortfolioPosition(&portfolioPos, feature)
			pos.MaxPrice, pos.MinPrice, pos.LastPrice = portfolioPos.MaxPrice, portfolioPos.MinPrice, portfolioPos.LastPrice
			assessment := risk.EvaluateBacktestExitWithPrevious(rule, portfolioPos, feature, featurePointer(previousMap, pos.StockCode), dayIndex, h.TimeHorizon)
			if !assessment.ShouldExit {
				if err := tx.Save(&pos).Error; err != nil {
					return err
				}
				stillOpen = append(stillOpen, pos)
				continue
			}
			if assessment.ExitAtNextOpen {
				pos.PendingExitReason = assessment.ExitReason
				if err := tx.Save(&pos).Error; err != nil {
					return err
				}
				stillOpen = append(stillOpen, pos)
				continue
			}
			order := orders.BuildExitOrder(portfolioPos, date, assessment.ExitPrice, assessment.ExitReason)
			fill, filled := fillSimulator.Fill(order, feature, featurePointer(previousMap, pos.StockCode), date)
			if !filled {
				if err := tx.Save(&pos).Error; err != nil {
					return err
				}
				stillOpen = append(stillOpen, pos)
				continue
			}
			if err := closePaperPosition(tx, account, &pos, fill, dayIndex, assessment.ExitReason, now); err != nil {
				return err
			}
			closedToday++
			result.Validated++
		}
		positions = stillOpen

		marketValue := paperMarketValue(positions, featureMap)
		totalValue := account.Cash + marketValue
		nav := 1.0
		if account.InitialCash > 0 {
			nav = totalValue / account.InitialCash
		}
		if nav > account.MaxNav {
			account.MaxNav = nav
		}
		drawdown := 0.0
		if account.MaxNav > 0 {
			drawdown = clamp((account.MaxNav-nav)/account.MaxNav, 0, 1)
		}
		account.MarketValue = marketValue
		account.TotalValue = totalValue
		account.Nav = nav
		account.MaxDrawdown = math.Max(account.MaxDrawdown, drawdown)
		account.PositionCount = len(positions)
		account.TradingDays++
		account.LastProcessedDate = date
		if err := tx.Save(account).Error; err != nil {
			return err
		}
		daily := models.PredictionPaperDaily{
			AccountID: account.ID, HypothesisID: h.ID, Date: date,
			Cash: account.Cash, MarketValue: marketValue, TotalValue: totalValue,
			Nav: nav, Drawdown: drawdown, PositionCount: len(positions), TradeCount: closedToday,
		}
		if err := tx.Where("account_id = ? AND date = ?", account.ID, date).
			Assign(daily).FirstOrCreate(&daily).Error; err != nil {
			return err
		}
		ready, reason := paperAccountReady(*h, *account)
		if err := tx.Model(&models.PredictionHypothesis{}).Where("id = ?", h.ID).Updates(map[string]any{
			"paper_trade_days": account.TradingDays, "paper_trade_count": account.TradeCount,
			"paper_nav": account.Nav, "paper_max_drawdown": account.MaxDrawdown,
			"paper_cash": account.Cash, "paper_market_value": account.MarketValue,
			"paper_position_count": account.PositionCount, "paper_ready": ready,
			"paper_ready_reason": reason, "paper_last_processed_date": date,
			"valid_count": account.TradeCount, "valid_return": account.Nav - 1,
		}).Error; err != nil {
			return err
		}
		h.PaperTradeDays, h.PaperTradeCount = account.TradingDays, account.TradeCount
		h.PaperNav, h.PaperMaxDrawdown = account.Nav, account.MaxDrawdown
		h.PaperReady, h.PaperReadyReason = ready, reason
		return nil
	})
}

func previousPaperFeatures(tx *gorm.DB, codes []string, date, featureVersion string) map[string]models.StockFeature {
	result := make(map[string]models.StockFeature, len(codes))
	for _, code := range codes {
		var feature models.StockFeature
		if tx.Where("stock_code = ? AND date < ? AND feature_version = ?", code, date, featureVersion).
			Order("date desc").First(&feature).Error == nil {
			result[code] = feature
		}
	}
	return result
}

func paperPortfolioPosition(pos models.PredictionPaperPosition) PortfolioPosition {
	return PortfolioPosition{
		StockCode: pos.StockCode, StockName: pos.StockName, SignalDate: pos.SignalDate,
		EntryDate: pos.EntryDate, EntryPrice: pos.EntryPrice, AvgCost: pos.AvgCost,
		CostAmount: pos.CostAmount, EntryFee: pos.EntryFee, EntrySlippage: pos.EntrySlippage,
		Quantity: pos.Quantity, MaxPrice: pos.MaxPrice, MinPrice: pos.MinPrice, LastPrice: pos.LastPrice,
		BuyDayIndex: pos.BuyDayIndex, EntryReasonJSON: pos.EntryReasonJSON,
		FeatureVersion: pos.FeatureVersion, DataAsOf: pos.DataAsOf,
	}
}

func closePaperPosition(tx *gorm.DB, account *models.PredictionPaperAccount, pos *models.PredictionPaperPosition, fill SimFill, dayIndex int, reason string, now time.Time) error {
	trade := closedPortfolioTrade(paperPortfolioPosition(*pos), fill, fill.TradeDate, dayIndex, reason)
	row := models.PredictionPaperTrade{
		AccountID: account.ID, HypothesisID: pos.HypothesisID, SignalID: pos.SignalID,
		StockCode: trade.StockCode, StockName: trade.StockName, SignalDate: trade.SignalDate,
		BuyDate: trade.BuyDate, SellDate: trade.SellDate, BuyPrice: trade.BuyPrice, SellPrice: trade.SellPrice,
		Quantity: trade.Quantity, GrossBuyAmount: trade.GrossBuyAmount, GrossSellAmount: trade.GrossSellAmount,
		Fee: trade.Fee, Slippage: trade.Slippage, ReturnRate: trade.ReturnRate,
		MaxReturn: trade.MaxReturn, MaxDrawdown: trade.MaxDrawdown, HoldDays: trade.HoldDays,
		ExitReason: trade.ExitReason, EntryReasonJSON: trade.EntryReasonJSON,
		FeatureVersion: trade.FeatureVersion, DataAsOf: trade.DataAsOf,
	}
	if err := tx.Create(&row).Error; err != nil {
		return err
	}
	account.Cash += fill.NetAmount
	account.TradeCount++
	if trade.Hit {
		account.WinCount++
	}
	if err := tx.Model(&models.PredictionSignal{}).Where("id = ?", pos.SignalID).Updates(map[string]any{
		"actual_return": trade.ReturnRate, "max_return": trade.MaxReturn, "max_drawdown": trade.MaxDrawdown,
		"hit": trade.Hit, "status": "validated", "target_date": trade.SellDate, "validated_at": now,
	}).Error; err != nil {
		return err
	}
	return tx.Delete(&models.PredictionPaperPosition{}, pos.ID).Error
}

func paperMarketValue(positions []models.PredictionPaperPosition, features map[string]models.StockFeature) float64 {
	value := 0.0
	for i := range positions {
		price := positions[i].LastPrice
		if feature, ok := features[positions[i].StockCode]; ok && feature.Close > 0 {
			price = feature.Close
		}
		value += price * positions[i].Quantity
	}
	return value
}

func paperTotalValue(cash float64, positions []models.PredictionPaperPosition, features map[string]models.StockFeature) float64 {
	return cash + paperMarketValue(positions, features)
}

func paperAccountReady(h models.PredictionHypothesis, account models.PredictionPaperAccount) (bool, string) {
	config := configFromHypothesis(h)
	reasons := make([]string, 0, 6)
	if account.TradingDays < config.PaperTradeDays {
		reasons = append(reasons, fmt.Sprintf("交易日 %d/%d", account.TradingDays, config.PaperTradeDays))
	}
	if account.TradeCount < MinPaperTradeValidations {
		reasons = append(reasons, fmt.Sprintf("完成交易 %d/%d", account.TradeCount, MinPaperTradeValidations))
	}
	if account.Nav <= 1 {
		reasons = append(reasons, "前向净值尚未超过1")
	}
	if account.MaxDrawdown > 0.20 {
		reasons = append(reasons, "前向最大回撤超过20%")
	}
	if h.OutSampleTradeCount < 15 || h.OutSampleAvgReturn <= 0 {
		reasons = append(reasons, "样本外交易或收益未达标")
	}
	if !h.BenchmarkAvailable || h.ExcessReturn <= 0 {
		reasons = append(reasons, "基准或超额收益未达标")
	}
	if len(reasons) > 0 {
		return false, strings.Join(reasons, "；")
	}
	return true, "已达到正式监控门槛，等待用户确认"
}

func (s *PredictionService) GetPaperTradingDetails(hypothesisID uint) (*PaperTradingDetails, error) {
	if err := ensurePredictionTables(); err != nil {
		return nil, err
	}
	var h models.PredictionHypothesis
	if err := db.Dao.First(&h, hypothesisID).Error; err != nil {
		return nil, fmt.Errorf("预测假设不存在")
	}
	var account models.PredictionPaperAccount
	if err := db.Dao.Where("hypothesis_id = ?", hypothesisID).First(&account).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &PaperTradingDetails{Reason: "尚未开始前向验证"}, nil
		}
		return nil, err
	}
	details := &PaperTradingDetails{Account: account}
	db.Dao.Where("account_id = ?", account.ID).Order("entry_date asc, id asc").Find(&details.Positions)
	db.Dao.Where("account_id = ?", account.ID).Order("sell_date desc, id desc").Limit(500).Find(&details.Trades)
	db.Dao.Where("account_id = ?", account.ID).Order("date asc").Find(&details.Dailies)
	details.Ready, details.Reason = paperAccountReady(h, account)
	return details, nil
}

func loggerPaperError(hypothesisID uint, err error) {
	// 单策略失败不阻断其他账户推进，错误仍返回给定时任务并进入日志。
	logger.SugaredLogger.Errorf("process paper trading hypothesis %d error: %v", hypothesisID, err)
}
