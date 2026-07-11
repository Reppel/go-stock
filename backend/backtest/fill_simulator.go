package backtest

import (
	"go-stock/backend/models"
	"math"
	"strings"
)

type FillSimulator struct {
	config BacktestConfig
}

func NewFillSimulator(config BacktestConfig) *FillSimulator {
	return &FillSimulator{config: normalizeBacktestConfig(config)}
}

func (s *FillSimulator) CanTrade(order SimOrder, feature models.StockFeature, previous *models.StockFeature) bool {
	if s.config.UseSuspensionRule && (feature.Open <= 0 || feature.Close <= 0 || feature.High <= 0 || feature.Low <= 0 || feature.Volume <= 0) {
		return false
	}
	if !s.config.UseLimitRule {
		return true
	}
	if previous == nil || previous.Close <= 0 || feature.Open <= 0 {
		return true
	}
	limitRate := priceLimitRate(order.StockCode, order.StockName)
	upperLimit := previous.Close * (1 + limitRate)
	lowerLimit := previous.Close * (1 - limitRate)
	limitBuffer := s.config.LimitBuffer
	if limitBuffer < 0 {
		limitBuffer = 0
	}
	if limitBuffer > 0.01 {
		limitBuffer = 0.01
	}
	executionPrice := order.PriceHint
	if executionPrice <= 0 {
		executionPrice = feature.Open
	}
	epsilon := 1e-8
	if order.Side == OrderSideBuy && executionPrice >= upperLimit*(1-limitBuffer)-epsilon {
		return false
	}
	if order.Side == OrderSideSell && executionPrice <= lowerLimit*(1+limitBuffer)+epsilon {
		return false
	}
	return true
}

func (s *FillSimulator) Fill(order SimOrder, feature models.StockFeature, previous *models.StockFeature, tradeDate string) (SimFill, bool) {
	if order.Quantity <= 0 || !s.CanTrade(order, feature, previous) {
		return SimFill{OrderID: order.ID, StockCode: order.StockCode, Status: "rejected", Reason: "not tradable"}, false
	}

	quantity := order.Quantity
	if order.Side == OrderSideBuy {
		if s.config.LotSize > 0 {
			quantity = math.Floor(quantity/float64(s.config.LotSize)) * float64(s.config.LotSize)
		}
		if feature.Volume > 0 && s.config.MaxParticipation > 0 {
			maxQuantity := math.Floor(feature.Volume*100*s.config.MaxParticipation/float64(s.config.LotSize)) * float64(s.config.LotSize)
			if maxQuantity > 0 && quantity > maxQuantity {
				quantity = maxQuantity
			}
		}
	}
	if quantity <= 0 {
		return SimFill{OrderID: order.ID, StockCode: order.StockCode, Status: "rejected", Reason: "below lot or liquidity limit"}, false
	}

	price := order.PriceHint
	if price <= 0 {
		price = s.executionPrice(order.Side, feature)
	}
	if price <= 0 {
		return SimFill{OrderID: order.ID, StockCode: order.StockCode, Status: "rejected", Reason: "invalid price"}, false
	}

	gross := price * quantity
	commission := gross * s.config.FeeRate
	if commission < s.config.MinCommission {
		commission = s.config.MinCommission
	}
	fee := commission
	if order.Side == OrderSideSell && isStampDutyInstrument(order.StockCode) {
		fee += gross * s.config.SellStampDuty
	}
	slippageRate := s.config.Slippage + s.marketImpactRate(quantity, feature.Volume)
	slippage := gross * slippageRate
	net := gross + fee + slippage
	if order.Side == OrderSideSell {
		net = gross - fee - slippage
	}

	return SimFill{
		OrderID:      order.ID,
		StockCode:    order.StockCode,
		StockName:    order.StockName,
		Side:         order.Side,
		TradeDate:    tradeDate,
		Price:        price,
		Quantity:     quantity,
		GrossAmount:  gross,
		Fee:          fee,
		SlippageCost: slippage,
		NetAmount:    net,
		Status:       "filled",
		Reason:       order.ExitReason,
	}, true
}

func (s *FillSimulator) marketImpactRate(quantity, volumeLots float64) float64 {
	if quantity <= 0 || volumeLots <= 0 || s.config.ImpactCoefficient <= 0 {
		return 0
	}
	participation := quantity / (volumeLots * 100)
	return math.Min(s.config.ImpactCoefficient*math.Sqrt(math.Max(participation, 0)), 0.02)
}

func (s *FillSimulator) executionPrice(side OrderSide, feature models.StockFeature) float64 {
	if side == OrderSideBuy {
		if s.config.EntryMode == "next_close" || feature.Open <= 0 {
			return feature.Close
		}
		return feature.Open
	}
	if feature.Close > 0 {
		return feature.Close
	}
	return feature.Open
}

func priceLimitRate(stockCode, stockName string) float64 {
	code := strings.ToLower(strings.TrimSpace(stockCode))
	name := strings.ToUpper(strings.TrimSpace(stockName))
	if strings.Contains(name, "ST") {
		return 0.05
	}
	code = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(code, "sh"), "sz"), "bj")
	if strings.HasPrefix(code, "688") || strings.HasPrefix(code, "300") || strings.HasPrefix(code, "301") {
		return 0.20
	}
	if strings.HasPrefix(code, "4") || strings.HasPrefix(code, "8") || strings.HasPrefix(code, "92") {
		return 0.30
	}
	return 0.10
}

func isStampDutyInstrument(stockCode string) bool {
	code := strings.ToLower(strings.TrimSpace(stockCode))
	code = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(code, "sh"), "sz"), "bj")
	return !(strings.HasPrefix(code, "5") || strings.HasPrefix(code, "1"))
}
