package backtest

import (
	"go-stock/backend/models"
	"math"
	"time"
)

type PortfolioEngine struct {
	repo     *FeatureRepository
	strategy *StrategyEngine
	orders   *OrderBuilder
	risk     *RiskEngine
}

func NewPortfolioEngine(repo *FeatureRepository) *PortfolioEngine {
	if repo == nil {
		repo = NewFeatureRepository()
	}
	strategy := NewStrategyEngine()
	return &PortfolioEngine{
		repo:     repo,
		strategy: strategy,
		orders:   NewOrderBuilder(),
		risk:     NewRiskEngine(strategy),
	}
}

func (p *PortfolioEngine) RunBacktest(
	rule Rule,
	universe []string,
	tradingDays []string,
	timeHorizon int,
	config BacktestConfig,
) (*ValidationResult, error) {
	if config.EntryMode == "" {
		config = DefaultBacktestConfig()
	}
	if len(tradingDays) == 0 {
		return &ValidationResult{}, nil
	}

	fillSimulator := NewFillSimulator(config)
	cash := 1.0
	positions := make(map[string]*PortfolioPosition)
	pendingSignals := make([]Signal, 0)
	trades := make([]Trade, 0)
	dailyNAV := make([]DailyNav, 0, len(tradingDays))
	maxPortfolioValue := 1.0

	for i, date := range tradingDays {
		features := p.repo.GetByDate(date, universe)
		featureMap := stockFeatureMap(features)
		priceHints := closePriceMap(features, config)

		if len(pendingSignals) > 0 {
			held := heldStockMap(positions)
			totalBeforeEntry := portfolioTotalValue(cash, positions, featureMap)
			orders := p.orders.BuildEntryOrders(pendingSignals, priceHints, cash, totalBeforeEntry, held, rule.MaxHoldings, date)
			for _, order := range orders {
				f, ok := featureMap[order.StockCode]
				if !ok {
					continue
				}
				if order.PriceHint <= 0 {
					order.PriceHint = entryPrice(f, config)
				}
				costRate := 1 + config.FeeRate + config.Slippage
				if costRate <= 0 {
					costRate = 1
				}
				maxQuantity := cash / (order.PriceHint * costRate)
				if maxQuantity <= 0 {
					continue
				}
				if order.Quantity > maxQuantity {
					order.Quantity = maxQuantity
				}
				fill, ok := fillSimulator.Fill(order, f, date)
				if !ok || fill.NetAmount <= 0 || fill.NetAmount > cash+1e-9 {
					continue
				}
				cash -= fill.NetAmount
				dataAsOf := f.DataAsOf
				if dataAsOf.IsZero() {
					dataAsOf = time.Now()
				}
				positions[order.StockCode] = &PortfolioPosition{
					StockCode:       order.StockCode,
					StockName:       order.StockName,
					SignalDate:      order.SignalDate,
					EntryDate:       date,
					EntryPrice:      fill.Price,
					AvgCost:         fill.NetAmount / fill.Quantity,
					CostAmount:      fill.NetAmount,
					Quantity:        fill.Quantity,
					MaxPrice:        fill.Price,
					MinPrice:        fill.Price,
					LastPrice:       fill.Price,
					BuyDayIndex:     i,
					EntryReasonJSON: order.ReasonJSON,
					FeatureVersion:  f.FeatureVersion,
					DataAsOf:        dataAsOf,
				}
			}
			pendingSignals = nil
		}

		closedTrades := make([]Trade, 0)
		for code, pos := range positions {
			f, ok := featureMap[code]
			if !ok {
				continue
			}
			updatePortfolioPosition(pos, f)
			assessment := p.risk.EvaluateBacktestExit(rule, *pos, f, i, timeHorizon)
			if !assessment.ShouldExit {
				continue
			}
			order := p.orders.BuildExitOrder(*pos, date, assessment.ExitPrice, assessment.ExitReason)
			fill, ok := fillSimulator.Fill(order, f, date)
			if !ok {
				continue
			}
			cash += fill.NetAmount
			returnRate := 0.0
			if pos.CostAmount > 0 {
				returnRate = (fill.NetAmount - pos.CostAmount) / pos.CostAmount
			}
			closedTrades = append(closedTrades, Trade{
				StockCode:       pos.StockCode,
				StockName:       pos.StockName,
				SignalDate:      pos.SignalDate,
				BuyDate:         pos.EntryDate,
				SellDate:        date,
				BuyPrice:        pos.EntryPrice,
				SellPrice:       fill.Price,
				Fee:             fill.Fee,
				Slippage:        fill.SlippageCost,
				ReturnRate:      returnRate,
				MaxReturn:       safeReturn(pos.MaxPrice, pos.EntryPrice),
				MaxDrawdown:     tradeMaxDrawdown(pos.EntryPrice, pos.MinPrice),
				HoldDays:        i - pos.BuyDayIndex,
				ExitReason:      assessment.ExitReason,
				EntryReasonJSON: pos.EntryReasonJSON,
				FeatureVersion:  pos.FeatureVersion,
				DataAsOf:        pos.DataAsOf,
				Hit:             returnRate > 0,
			})
			delete(positions, code)
		}
		trades = append(trades, closedTrades...)

		totalValue := portfolioTotalValue(cash, positions, featureMap)
		if totalValue > maxPortfolioValue {
			maxPortfolioValue = totalValue
		}
		drawdown := 0.0
		if maxPortfolioValue > 0 {
			drawdown = (maxPortfolioValue - totalValue) / maxPortfolioValue
		}

		signals := p.strategy.GenerateSignals(rule, features)
		filteredSignals := make([]Signal, 0, len(signals))
		held := heldStockMap(positions)
		for _, signal := range signals {
			if held[signal.StockCode] {
				continue
			}
			filteredSignals = append(filteredSignals, signal)
		}
		pendingSignals = filteredSignals

		dailyNAV = append(dailyNAV, DailyNav{
			Date:       date,
			Nav:        totalValue,
			Drawdown:   drawdown,
			TradeCount: len(signals),
		})
	}

	lastDate := tradingDays[len(tradingDays)-1]
	lastFeatures := p.repo.GetByDate(lastDate, universe)
	lastFeatureMap := stockFeatureMap(lastFeatures)
	fillSimulator = NewFillSimulator(config)
	for code, pos := range positions {
		f, ok := lastFeatureMap[code]
		if !ok {
			continue
		}
		order := p.orders.BuildExitOrder(*pos, lastDate, f.Close, "end_of_period")
		fill, ok := fillSimulator.Fill(order, f, lastDate)
		if !ok {
			continue
		}
		returnRate := 0.0
		if pos.CostAmount > 0 {
			returnRate = (fill.NetAmount - pos.CostAmount) / pos.CostAmount
		}
		trades = append(trades, Trade{
			StockCode:       pos.StockCode,
			StockName:       pos.StockName,
			SignalDate:      pos.SignalDate,
			BuyDate:         pos.EntryDate,
			SellDate:        lastDate,
			BuyPrice:        pos.EntryPrice,
			SellPrice:       fill.Price,
			Fee:             fill.Fee,
			Slippage:        fill.SlippageCost,
			ReturnRate:      returnRate,
			MaxReturn:       safeReturn(pos.MaxPrice, pos.EntryPrice),
			MaxDrawdown:     tradeMaxDrawdown(pos.EntryPrice, pos.MinPrice),
			HoldDays:        len(tradingDays) - 1 - pos.BuyDayIndex,
			ExitReason:      "end_of_period",
			EntryReasonJSON: pos.EntryReasonJSON,
			FeatureVersion:  pos.FeatureVersion,
			DataAsOf:        pos.DataAsOf,
			Hit:             returnRate > 0,
		})
	}

	validator := &WalkForwardValidator{}
	result := validator.calculateMetrics(trades, dailyNAV)
	result.BacktestConfig = config
	result.NoLookaheadPassed = config.EntryMode == "next_open" || config.EntryMode == "next_close"
	return result, nil
}

func stockFeatureMap(features []models.StockFeature) map[string]models.StockFeature {
	result := make(map[string]models.StockFeature, len(features))
	for _, f := range features {
		result[f.StockCode] = f
	}
	return result
}

func closePriceMap(features []models.StockFeature, config BacktestConfig) map[string]float64 {
	result := make(map[string]float64, len(features))
	for _, f := range features {
		result[f.StockCode] = entryPrice(f, config)
	}
	return result
}

func entryPrice(f models.StockFeature, config BacktestConfig) float64 {
	if config.EntryMode == "next_close" || f.Open <= 0 {
		return f.Close
	}
	return f.Open
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
		if f, ok := featureMap[code]; ok && f.Close > 0 {
			price = f.Close
		}
		total += price * pos.Quantity
	}
	if math.IsNaN(total) || math.IsInf(total, 0) {
		return cash
	}
	return total
}

func updatePortfolioPosition(pos *PortfolioPosition, f models.StockFeature) {
	if f.High > 0 && f.High > pos.MaxPrice {
		pos.MaxPrice = f.High
	}
	if f.Low > 0 && (pos.MinPrice <= 0 || f.Low < pos.MinPrice) {
		pos.MinPrice = f.Low
	}
	if f.Close > 0 {
		pos.LastPrice = f.Close
	}
}

func safeReturn(newPrice float64, basePrice float64) float64 {
	if basePrice <= 0 {
		return 0
	}
	return (newPrice - basePrice) / basePrice
}

func tradeMaxDrawdown(entryPrice float64, minPrice float64) float64 {
	if entryPrice <= 0 || minPrice <= 0 || minPrice >= entryPrice {
		return 0
	}
	drawdown := (entryPrice - minPrice) / entryPrice
	return clamp(drawdown, 0, 1)
}
