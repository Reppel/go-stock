package backtest

import "go-stock/backend/models"

type FillSimulator struct {
	config BacktestConfig
}

func NewFillSimulator(config BacktestConfig) *FillSimulator {
	if config.EntryMode == "" {
		config = DefaultBacktestConfig()
	}
	return &FillSimulator{config: config}
}

func (s *FillSimulator) CanTrade(f models.StockFeature) bool {
	if s.config.UseSuspensionRule && (f.Open <= 0 || f.Close <= 0 || f.High <= 0 || f.Low <= 0 || f.Volume <= 0) {
		return false
	}
	if s.config.UseLimitRule && f.Open > 0 && f.High == f.Low {
		return false
	}
	return true
}

func (s *FillSimulator) Fill(order SimOrder, f models.StockFeature, tradeDate string) (SimFill, bool) {
	if order.Quantity <= 0 || !s.CanTrade(f) {
		return SimFill{OrderID: order.ID, StockCode: order.StockCode, Status: "rejected", Reason: "not tradable"}, false
	}

	price := order.PriceHint
	if price <= 0 {
		price = s.executionPrice(order.Side, f)
	}
	if price <= 0 {
		return SimFill{OrderID: order.ID, StockCode: order.StockCode, Status: "rejected", Reason: "invalid price"}, false
	}

	gross := price * order.Quantity
	fee := gross * s.config.FeeRate
	slippage := gross * s.config.Slippage
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
		Quantity:     order.Quantity,
		GrossAmount:  gross,
		Fee:          fee,
		SlippageCost: slippage,
		NetAmount:    net,
		Status:       "filled",
		Reason:       order.ExitReason,
	}, true
}

func (s *FillSimulator) executionPrice(side OrderSide, f models.StockFeature) float64 {
	if side == OrderSideBuy {
		if s.config.EntryMode == "next_close" || f.Open <= 0 {
			return f.Close
		}
		return f.Open
	}
	if f.Close > 0 {
		return f.Close
	}
	return f.Open
}
