package backtest

import (
	"go-stock/backend/models"
	"math"
)

type PortfolioEngine struct {
	repo     *FeatureRepository
	strategy *StrategyEngine
	orders   *OrderBuilder
	risk     *RiskEngine
}

type pendingPortfolioExit struct {
	reason string
}

func NewPortfolioEngine(repo *FeatureRepository) *PortfolioEngine {
	if repo == nil {
		repo = NewFeatureRepository()
	}
	strategy := NewStrategyEngine()
	return &PortfolioEngine{
		repo: repo, strategy: strategy, orders: NewOrderBuilder(), risk: NewRiskEngine(strategy),
	}
}

func (p *PortfolioEngine) RunBacktest(
	rule Rule,
	universe []string,
	tradingDays []string,
	timeHorizon int,
	config BacktestConfig,
) (*ValidationResult, error) {
	config = normalizeBacktestConfig(config)
	if len(tradingDays) == 0 {
		return &ValidationResult{BacktestConfig: config}, nil
	}

	fillSimulator := NewFillSimulator(config)
	initialCapital := config.InitialCapital
	cash := initialCapital
	positions := make(map[string]*PortfolioPosition)
	pendingSignals := make([]Signal, 0)
	pendingExits := make(map[string]pendingPortfolioExit)
	trades := make([]Trade, 0)
	dailyNAV := make([]DailyNav, 0, len(tradingDays))
	lastFeatureByStock := make(map[string]models.StockFeature)
	maxPortfolioValue := initialCapital

	for dayIndex, date := range tradingDays {
		features := p.repo.GetByDate(date, universe)
		featureMap := stockFeatureMap(features)
		previousMap := cloneFeatureMap(lastFeatureByStock)
		closedToday := 0

		// Close-based exit rules are known only after T close and execute at T+1 open.
		for code, pending := range pendingExits {
			pos, held := positions[code]
			feature, available := featureMap[code]
			if !held || !available {
				continue
			}
			previous := featurePointer(previousMap, code)
			order := p.orders.BuildExitOrder(*pos, date, entryPrice(feature, config), pending.reason)
			fill, filled := fillSimulator.Fill(order, feature, previous, date)
			if !filled {
				continue
			}
			cash += fill.NetAmount
			trades = append(trades, closedPortfolioTrade(*pos, fill, date, dayIndex, pending.reason))
			delete(positions, code)
			delete(pendingExits, code)
			closedToday++
		}

		// Do not open a position on the last backtest day because A-share T+1 cannot liquidate it.
		if len(pendingSignals) > 0 && dayIndex < len(tradingDays)-1 {
			held := heldStockMap(positions)
			totalBeforeEntry := portfolioTotalValue(cash, positions, featureMap)
			priceHints := closePriceMap(features, config)
			orders := p.orders.BuildEntryOrders(pendingSignals, priceHints, cash, totalBeforeEntry, held, rule.MaxHoldings, config.LotSize, date)
			for _, order := range orders {
				feature, available := featureMap[order.StockCode]
				if !available {
					continue
				}
				previous := featurePointer(previousMap, order.StockCode)
				if order.PriceHint <= 0 {
					order.PriceHint = entryPrice(feature, config)
				}
				order.Quantity = affordableLotQuantity(cash, order.PriceHint, config)
				if order.Amount > 0 {
					targetQuantity := math.Floor(order.Amount/order.PriceHint/float64(config.LotSize)) * float64(config.LotSize)
					if targetQuantity > 0 && targetQuantity < order.Quantity {
						order.Quantity = targetQuantity
					}
				}
				fill, filled := fillSimulator.Fill(order, feature, previous, date)
				if !filled || fill.NetAmount <= 0 || fill.NetAmount > cash+1e-6 {
					continue
				}
				cash -= fill.NetAmount
				dataAsOf := feature.DataAsOf
				if dataAsOf.IsZero() {
					dataAsOf = featureDataAsOf(order.SignalDate)
				}
				positions[order.StockCode] = &PortfolioPosition{
					StockCode: order.StockCode, StockName: order.StockName, SignalDate: order.SignalDate,
					EntryDate: date, EntryPrice: fill.Price, AvgCost: fill.NetAmount / fill.Quantity,
					CostAmount: fill.NetAmount, EntryFee: fill.Fee, EntrySlippage: fill.SlippageCost,
					Quantity: fill.Quantity, MaxPrice: fill.Price, MinPrice: fill.Price, LastPrice: fill.Price,
					BuyDayIndex: dayIndex, EntryReasonJSON: order.ReasonJSON,
					FeatureVersion: feature.FeatureVersion, DataAsOf: dataAsOf,
				}
			}
		}
		pendingSignals = nil

		for code, pos := range positions {
			if _, alreadyPending := pendingExits[code]; alreadyPending {
				continue
			}
			feature, available := featureMap[code]
			if !available {
				continue
			}
			updatePortfolioPosition(pos, feature)
			previous := featurePointer(previousMap, code)
			assessment := p.risk.EvaluateBacktestExitWithPrevious(rule, *pos, feature, previous, dayIndex, timeHorizon)
			if !assessment.ShouldExit {
				continue
			}
			if assessment.ExitAtNextOpen {
				pendingExits[code] = pendingPortfolioExit{reason: assessment.ExitReason}
				continue
			}
			order := p.orders.BuildExitOrder(*pos, date, assessment.ExitPrice, assessment.ExitReason)
			fill, filled := fillSimulator.Fill(order, feature, previous, date)
			if !filled {
				continue
			}
			cash += fill.NetAmount
			trades = append(trades, closedPortfolioTrade(*pos, fill, date, dayIndex, assessment.ExitReason))
			delete(positions, code)
			closedToday++
		}

		totalValue := portfolioTotalValue(cash, positions, featureMap)
		if totalValue > maxPortfolioValue {
			maxPortfolioValue = totalValue
		}
		drawdown := 0.0
		if maxPortfolioValue > 0 {
			drawdown = clamp((maxPortfolioValue-totalValue)/maxPortfolioValue, 0, 1)
		}

		signals := p.strategy.GenerateSignalsWithPrevious(rule, features, previousMap)
		held := heldStockMap(positions)
		filteredSignals := make([]Signal, 0, len(signals))
		for _, signal := range signals {
			if !held[signal.StockCode] {
				filteredSignals = append(filteredSignals, signal)
			}
		}
		pendingSignals = filteredSignals
		dailyNAV = append(dailyNAV, DailyNav{
			Date: date, Nav: totalValue / initialCapital, Drawdown: drawdown, TradeCount: closedToday,
		})
		for code, feature := range featureMap {
			lastFeatureByStock[code] = feature
		}
	}

	lastDate := tradingDays[len(tradingDays)-1]
	lastFeatures := p.repo.GetByDate(lastDate, universe)
	lastFeatureMap := stockFeatureMap(lastFeatures)
	for code, pos := range positions {
		feature, available := lastFeatureMap[code]
		if !available || (config.UseT1Rule && pos.BuyDayIndex >= len(tradingDays)-1) {
			continue
		}
		order := p.orders.BuildExitOrder(*pos, lastDate, feature.Close, "end_of_period")
		fill, filled := fillSimulator.Fill(order, feature, nil, lastDate)
		if !filled {
			continue
		}
		cash += fill.NetAmount
		trades = append(trades, closedPortfolioTrade(*pos, fill, lastDate, len(tradingDays)-1, "end_of_period"))
		delete(positions, code)
	}
	if len(dailyNAV) > 0 {
		finalValue := portfolioTotalValue(cash, positions, lastFeatureMap)
		last := &dailyNAV[len(dailyNAV)-1]
		last.Nav = finalValue / initialCapital
		if maxPortfolioValue > 0 {
			last.Drawdown = clamp((maxPortfolioValue-finalValue)/maxPortfolioValue, 0, 1)
		}
	}

	validator := &WalkForwardValidator{}
	result := validator.calculateMetrics(trades, dailyNAV)
	result.BacktestConfig = config
	result.TurnoverRate = portfolioTurnover(trades, initialCapital)
	result.NoLookaheadPassed = validateNoLookahead(trades, config)
	return result, nil
}

func affordableLotQuantity(cash, price float64, config BacktestConfig) float64 {
	if cash <= config.MinCommission || price <= 0 || config.LotSize <= 0 {
		return 0
	}
	costRate := 1 + config.FeeRate + config.Slippage + config.ImpactCoefficient
	quantity := (cash - config.MinCommission) / (price * costRate)
	return math.Floor(quantity/float64(config.LotSize)) * float64(config.LotSize)
}

func closedPortfolioTrade(pos PortfolioPosition, fill SimFill, sellDate string, dayIndex int, reason string) Trade {
	returnRate := 0.0
	if pos.CostAmount > 0 {
		returnRate = (fill.NetAmount - pos.CostAmount) / pos.CostAmount
	}
	return Trade{
		StockCode: pos.StockCode, StockName: pos.StockName, SignalDate: pos.SignalDate,
		BuyDate: pos.EntryDate, SellDate: sellDate, BuyPrice: pos.EntryPrice, SellPrice: fill.Price,
		Quantity: fill.Quantity, GrossBuyAmount: pos.EntryPrice * pos.Quantity, GrossSellAmount: fill.GrossAmount,
		Fee: pos.EntryFee + fill.Fee, Slippage: pos.EntrySlippage + fill.SlippageCost,
		ReturnRate: returnRate, MaxReturn: safeReturn(pos.MaxPrice, pos.EntryPrice),
		MaxDrawdown: tradeMaxDrawdown(pos.EntryPrice, pos.MinPrice), HoldDays: dayIndex - pos.BuyDayIndex,
		ExitReason: reason, EntryReasonJSON: pos.EntryReasonJSON,
		FeatureVersion: pos.FeatureVersion, DataAsOf: pos.DataAsOf, Hit: returnRate > 0,
	}
}

func validateNoLookahead(trades []Trade, config BacktestConfig) bool {
	if config.EntryMode != "next_open" {
		return false
	}
	for _, trade := range trades {
		if trade.SignalDate == "" || trade.BuyDate <= trade.SignalDate {
			return false
		}
		if config.UseT1Rule && trade.SellDate <= trade.BuyDate {
			return false
		}
	}
	return true
}

func portfolioTurnover(trades []Trade, initialCapital float64) float64 {
	if initialCapital <= 0 {
		return 0
	}
	notional := 0.0
	for _, trade := range trades {
		notional += trade.GrossBuyAmount + trade.GrossSellAmount
	}
	return notional / (2 * initialCapital)
}

func stockFeatureMap(features []models.StockFeature) map[string]models.StockFeature {
	result := make(map[string]models.StockFeature, len(features))
	for _, feature := range features {
		result[feature.StockCode] = feature
	}
	return result
}

func cloneFeatureMap(source map[string]models.StockFeature) map[string]models.StockFeature {
	result := make(map[string]models.StockFeature, len(source))
	for code, feature := range source {
		result[code] = feature
	}
	return result
}

func featurePointer(features map[string]models.StockFeature, code string) *models.StockFeature {
	feature, ok := features[code]
	if !ok {
		return nil
	}
	return &feature
}

func closePriceMap(features []models.StockFeature, config BacktestConfig) map[string]float64 {
	result := make(map[string]float64, len(features))
	for _, feature := range features {
		result[feature.StockCode] = entryPrice(feature, config)
	}
	return result
}

func entryPrice(feature models.StockFeature, config BacktestConfig) float64 {
	if config.EntryMode == "next_close" || feature.Open <= 0 {
		return feature.Close
	}
	return feature.Open
}

func heldStockMap(positions map[string]*PortfolioPosition) map[string]bool {
	held := make(map[string]bool, len(positions))
	for code := range positions {
		held[code] = true
	}
	return held
}

func portfolioTotalValue(cash float64, positions map[string]*PortfolioPosition, featureMap map[string]models.StockFeature) float64 {
	total := cash
	for code, pos := range positions {
		price := pos.LastPrice
		if feature, ok := featureMap[code]; ok && feature.Close > 0 {
			price = feature.Close
		}
		total += price * pos.Quantity
	}
	if math.IsNaN(total) || math.IsInf(total, 0) {
		return cash
	}
	return total
}

func updatePortfolioPosition(pos *PortfolioPosition, feature models.StockFeature) {
	if feature.High > pos.MaxPrice {
		pos.MaxPrice = feature.High
	}
	if feature.Low > 0 && (pos.MinPrice <= 0 || feature.Low < pos.MinPrice) {
		pos.MinPrice = feature.Low
	}
	if feature.Close > 0 {
		pos.LastPrice = feature.Close
	}
}

func safeReturn(newPrice, basePrice float64) float64 {
	if basePrice <= 0 {
		return 0
	}
	return (newPrice - basePrice) / basePrice
}

func tradeMaxDrawdown(entryPrice, minPrice float64) float64 {
	if entryPrice <= 0 || minPrice <= 0 || minPrice >= entryPrice {
		return 0
	}
	return clamp((entryPrice-minPrice)/entryPrice, 0, 1)
}
