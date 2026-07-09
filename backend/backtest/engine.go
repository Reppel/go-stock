package backtest

import (
	"encoding/json"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"time"
)

const sqliteInClauseChunkSize = 900

// Condition 策略条件
type Condition struct {
	Indicator string  `json:"indicator"`
	Operator  string  `json:"operator"`
	Ref       string  `json:"ref"`
	Value     float64 `json:"value"`
}

// Rule 策略规则
type Rule struct {
	EntryConditions []Condition `json:"entryConditions"`
	ExitConditions  []Condition `json:"exitConditions"`
	StopLoss        float64     `json:"stopLoss"`
	StopGain        float64     `json:"stopGain"`
	MaxHoldDays     int         `json:"maxHoldDays"` // 最大持有天数
	MaxHoldings     int         `json:"maxHoldings"`
}

// Signal 买入信号
type Signal struct {
	StockCode string
	StockName string
	Date      string
	Price     float64
}

// Trade 交易记录
type Trade struct {
	StockCode   string  `json:"stockCode"`
	StockName   string  `json:"stockName"`
	BuyDate     string  `json:"buyDate"`
	SellDate    string  `json:"sellDate"`
	BuyPrice    float64 `json:"buyPrice"`
	SellPrice   float64 `json:"sellPrice"`
	ReturnRate  float64 `json:"returnRate"`
	MaxReturn   float64 `json:"maxReturn"`
	MaxDrawdown float64 `json:"maxDrawdown"`
	HoldDays    int     `json:"holdDays"`
	ExitReason  string  `json:"exitReason"` // stop_loss / stop_gain / exit_condition / max_hold_days
	Hit         bool    `json:"hit"`
}

// DailyNav 每日净值
type DailyNav struct {
	Date       string  `json:"date"`
	Nav        float64 `json:"nav"`
	Drawdown   float64 `json:"drawdown"`
	TradeCount int     `json:"tradeCount"`
}

// ValidationResult 验证结果
type ValidationResult struct {
	WinRate     float64    `json:"winRate"`
	AvgReturn   float64    `json:"avgReturn"`
	MaxDrawdown float64    `json:"maxDrawdown"`
	TradeCount  int        `json:"tradeCount"`
	TotalReturn float64    `json:"totalReturn"`
	DailyNAV    []DailyNav `json:"dailyNAV"`
	Trades      []Trade    `json:"trades"`
}

// Position 持仓
type Position struct {
	StockCode   string
	StockName   string
	BuyDate     string
	BuyPrice    float64
	BuyDayIndex int
	MaxPrice    float64
	MinPrice    float64
}

// FeatureRepository 特征数据仓库
type FeatureRepository struct{}

func NewFeatureRepository() *FeatureRepository {
	return &FeatureRepository{}
}

// GetByDate 获取某交易日全部特征
func (r *FeatureRepository) GetByDate(date string, universe []string) []models.StockFeature {
	var features []models.StockFeature
	if len(universe) == 0 {
		db.Dao.Where("date = ?", date).Find(&features)
		return features
	}

	for start := 0; start < len(universe); start += sqliteInClauseChunkSize {
		end := start + sqliteInClauseChunkSize
		if end > len(universe) {
			end = len(universe)
		}
		var chunkFeatures []models.StockFeature
		db.Dao.Where("date = ? AND stock_code IN ?", date, universe[start:end]).Find(&chunkFeatures)
		features = append(features, chunkFeatures...)
	}
	return features
}

// GetFeatureRange 获取某股票日期区间特征
func (r *FeatureRepository) GetFeatureRange(stockCode, startDate, endDate string) []models.StockFeature {
	var features []models.StockFeature
	db.Dao.Where("stock_code = ? AND date >= ? AND date <= ?", stockCode, startDate, endDate).
		Order("date asc").
		Find(&features)
	return features
}

func (r *FeatureRepository) GetFirstOnOrAfter(stockCode, date string) (models.StockFeature, bool) {
	var feature models.StockFeature
	err := db.Dao.Where("stock_code = ? AND date >= ?", stockCode, date).
		Order("date asc").
		First(&feature).Error
	return feature, err == nil
}

// GetFeatureAfterDate 获取某股票在某日期之后的所有特征
type dateFeatureKey struct {
	date      string
	stockCode string
}

// WalkForwardValidator 滚动验证引擎
type WalkForwardValidator struct {
	repo *FeatureRepository
}

func NewWalkForwardValidator() *WalkForwardValidator {
	return &WalkForwardValidator{
		repo: NewFeatureRepository(),
	}
}

// Validate 对单个假设做滚动回测
func (v *WalkForwardValidator) Validate(
	rule Rule,
	universe []string,
	startDate string,
	endDate string,
	timeHorizon int,
) (*ValidationResult, error) {

	trades := make([]Trade, 0)
	dailyNAV := make([]DailyNav, 0)

	// 交易日历生成
	tradingDays := v.getTradingDays(startDate, endDate)
	if len(tradingDays) == 0 {
		return &ValidationResult{}, nil
	}

	portfolioValue := 1.0
	maxPortfolioValue := 1.0

	// 当前持仓
	var positions []Position

	for i, date := range tradingDays {
		// 获取当日全部特征
		features := v.repo.GetByDate(date, universe)
		featureMap := make(map[string]models.StockFeature)
		for _, f := range features {
			featureMap[f.StockCode] = f
		}

		// 检查持仓是否需要卖出
		nextPositions := make([]Position, 0)
		var closedTrades []Trade
		for _, pos := range positions {
			exitPrice, exitReason := v.checkExit(rule, pos, featureMap, i, timeHorizon)
			if exitPrice > 0 {
				// 卖出
				returnRate := (exitPrice - pos.BuyPrice) / pos.BuyPrice
				trade := Trade{
					StockCode:   pos.StockCode,
					StockName:   pos.StockName,
					BuyDate:     pos.BuyDate,
					SellDate:    date,
					BuyPrice:    pos.BuyPrice,
					SellPrice:   exitPrice,
					ReturnRate:  returnRate,
					MaxReturn:   (pos.MaxPrice - pos.BuyPrice) / pos.BuyPrice,
					MaxDrawdown: (pos.BuyPrice - pos.MinPrice) / pos.BuyPrice,
					HoldDays:    i - pos.BuyDayIndex,
					ExitReason:  exitReason,
					Hit:         returnRate > 0,
				}
				closedTrades = append(closedTrades, trade)
			} else {
				// 继续持仓，更新最大/最小价
				f, ok := featureMap[pos.StockCode]
				if !ok {
					nextPositions = append(nextPositions, pos)
					continue
				}
				if f.High > pos.MaxPrice {
					pos.MaxPrice = f.High
				}
				if f.Low < pos.MinPrice {
					pos.MinPrice = f.Low
				}
				nextPositions = append(nextPositions, pos)
			}
		}
		positions = nextPositions

		// 计算当日收益
		if len(closedTrades) > 0 {
			trades = append(trades, closedTrades...)
			for _, t := range closedTrades {
				portfolioValue *= (1 + t.ReturnRate/float64(len(positions)+len(closedTrades)))
			}
		}

		// 生成新的买入信号
		heldStocks := make(map[string]bool, len(positions))
		for _, pos := range positions {
			heldStocks[pos.StockCode] = true
		}
		signals := v.generateSignals(rule, features)
		for _, signal := range signals {
			if heldStocks[signal.StockCode] {
				continue
			}
			// 限制最大持仓数
			if rule.MaxHoldings > 0 && len(positions) >= rule.MaxHoldings {
				break
			}
			positions = append(positions, Position{
				StockCode:   signal.StockCode,
				StockName:   signal.StockName,
				BuyDate:     signal.Date,
				BuyPrice:    signal.Price,
				BuyDayIndex: i,
				MaxPrice:    signal.Price,
				MinPrice:    signal.Price,
			})
			heldStocks[signal.StockCode] = true
		}

		if portfolioValue > maxPortfolioValue {
			maxPortfolioValue = portfolioValue
		}
		drawdown := (maxPortfolioValue - portfolioValue) / maxPortfolioValue

		dailyNAV = append(dailyNAV, DailyNav{
			Date:       date,
			Nav:        portfolioValue,
			Drawdown:   drawdown,
			TradeCount: len(signals),
		})
	}

	// 未平仓的按最后一日收盘价卖出
	lastDate := tradingDays[len(tradingDays)-1]
	for _, pos := range positions {
		features := v.repo.GetByDate(lastDate, []string{pos.StockCode})
		exitPrice := pos.BuyPrice
		if len(features) > 0 {
			exitPrice = features[0].Close
		}
		returnRate := (exitPrice - pos.BuyPrice) / pos.BuyPrice
		trade := Trade{
			StockCode:   pos.StockCode,
			StockName:   pos.StockName,
			BuyDate:     pos.BuyDate,
			SellDate:    lastDate,
			BuyPrice:    pos.BuyPrice,
			SellPrice:   exitPrice,
			ReturnRate:  returnRate,
			MaxReturn:   (pos.MaxPrice - pos.BuyPrice) / pos.BuyPrice,
			MaxDrawdown: (pos.BuyPrice - pos.MinPrice) / pos.BuyPrice,
			HoldDays:    len(tradingDays) - 1 - pos.BuyDayIndex,
			ExitReason:  "end_of_period",
			Hit:         returnRate > 0,
		}
		trades = append(trades, trade)
	}

	return v.calculateMetrics(trades, dailyNAV), nil
}

// checkExit 检查是否需要卖出
func (v *WalkForwardValidator) checkExit(rule Rule, pos Position, featureMap map[string]models.StockFeature, dayIndex int, timeHorizon int) (float64, string) {
	f, ok := featureMap[pos.StockCode]
	if !ok {
		return 0, ""
	}

	// 1. 止损
	stopLossPrice := pos.BuyPrice * (1 - rule.StopLoss)
	if f.Low <= stopLossPrice {
		return stopLossPrice, "stop_loss"
	}

	// 2. 止盈
	stopGainPrice := pos.BuyPrice * (1 + rule.StopGain)
	if f.High >= stopGainPrice {
		return stopGainPrice, "stop_gain"
	}

	// 3. 卖出条件
	if v.matchConditions(rule.ExitConditions, f) {
		return f.Close, "exit_condition"
	}

	// 4. 最大持有天数
	maxHoldDays := rule.MaxHoldDays
	if maxHoldDays <= 0 {
		maxHoldDays = timeHorizon
	}
	if dayIndex-pos.BuyDayIndex >= maxHoldDays {
		return f.Close, "max_hold_days"
	}

	return 0, ""
}

// generateSignals 根据规则生成买入信号
func (v *WalkForwardValidator) generateSignals(rule Rule, features []models.StockFeature) []Signal {
	var signals []Signal
	for _, f := range features {
		if v.matchConditions(rule.EntryConditions, f) {
			signals = append(signals, Signal{
				StockCode: f.StockCode,
				StockName: f.StockCode,
				Date:      f.Date,
				Price:     f.Close,
			})
		}
	}
	return signals
}

// matchConditions 匹配条件
func (v *WalkForwardValidator) matchConditions(conditions []Condition, f models.StockFeature) bool {
	for _, c := range conditions {
		if !v.matchCondition(c, f) {
			return false
		}
	}
	return true
}

// matchCondition 匹配单个条件
func (v *WalkForwardValidator) matchCondition(c Condition, f models.StockFeature) bool {
	value := v.getIndicatorValue(c.Indicator, f)
	var ref float64
	if c.Ref != "" {
		ref = v.getIndicatorValue(c.Ref, f)
	} else {
		ref = c.Value
	}

	switch c.Operator {
	case ">":
		return value > ref
	case ">=":
		return value >= ref
	case "<":
		return value < ref
	case "<=":
		return value <= ref
	case "==":
		return value == ref
	case "!=":
		return value != ref
	default:
		return false
	}
}

// getIndicatorValue 获取指标数值
func (v *WalkForwardValidator) getIndicatorValue(indicator string, f models.StockFeature) float64 {
	switch indicator {
	case "MA5":
		return f.MA5
	case "MA10":
		return f.MA10
	case "MA20":
		return f.MA20
	case "MA60":
		return f.MA60
	case "MACD":
		return f.MACD
	case "RSI6":
		return f.RSI6
	case "RSI12":
		return f.RSI12
	case "KDJ_K":
		return f.KDJ_K
	case "BOLLUpper":
		return f.BOLLUpper
	case "BOLLMid":
		return f.BOLLMid
	case "BOLLLower":
		return f.BOLLLower
	case "VolumeRatio":
		return f.VolumeRatio
	case "ATR":
		return f.ATR
	case "ChangeRate5":
		return f.ChangeRate5
	case "ChangeRate20":
		return f.ChangeRate20
	case "Close":
		return f.Close
	case "Open":
		return f.Open
	case "High":
		return f.High
	case "Low":
		return f.Low
	case "Volume":
		return f.Volume
	default:
		return 0
	}
}

// calculateMetrics 计算绩效指标
func (v *WalkForwardValidator) calculateMetrics(trades []Trade, dailyNAV []DailyNav) *ValidationResult {
	if len(trades) == 0 {
		return &ValidationResult{
			WinRate:     0,
			AvgReturn:   0,
			MaxDrawdown: 0,
			TradeCount:  0,
			TotalReturn: 0,
			DailyNAV:    dailyNAV,
			Trades:      trades,
		}
	}

	winCount := 0
	totalReturn := 0.0
	maxDrawdown := 0.0

	for _, t := range trades {
		if t.Hit {
			winCount++
		}
		totalReturn += t.ReturnRate
		if t.MaxDrawdown > maxDrawdown {
			maxDrawdown = t.MaxDrawdown
		}
	}

	winRate := float64(winCount) / float64(len(trades))
	avgReturn := totalReturn / float64(len(trades))

	return &ValidationResult{
		WinRate:     winRate,
		AvgReturn:   avgReturn,
		MaxDrawdown: maxDrawdown,
		TradeCount:  len(trades),
		TotalReturn: totalReturn,
		DailyNAV:    dailyNAV,
		Trades:      trades,
	}
}

// getTradingDays 获取交易日列表（简化版，实际应读取本地交易日历）
func (v *WalkForwardValidator) getTradingDays(startDate, endDate string) []string {
	var days []string
	start, _ := time.Parse("2006-01-02", startDate)
	end, _ := time.Parse("2006-01-02", endDate)

	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			days = append(days, d.Format("2006-01-02"))
		}
	}
	return days
}

// addCalendarDays 添加日历日
func (v *WalkForwardValidator) addCalendarDays(date string, days int) string {
	t, _ := time.Parse("2006-01-02", date)
	return t.AddDate(0, 0, days).Format("2006-01-02")
}

// RuleToJSON 规则转 JSON
func RuleToJSON(rule Rule) string {
	b, _ := json.Marshal(rule)
	return string(b)
}

// JSONToRule JSON 转规则
func JSONToRule(s string) (Rule, error) {
	var rule Rule
	err := json.Unmarshal([]byte(s), &rule)
	return rule, err
}
