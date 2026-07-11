package backtest

import (
	"go-stock/backend/models"
	"testing"
)

func TestRiskEngineEnforcesT1AndGapStop(t *testing.T) {
	engine := NewRiskEngine(nil)
	rule := Rule{StopLoss: 0.10, StopGain: 0.20, MaxHoldDays: 5}
	position := PortfolioPosition{EntryPrice: 100, BuyDayIndex: 5}
	feature := models.StockFeature{Open: 80, High: 90, Low: 79, Close: 85}
	if assessment := engine.EvaluateBacktestExit(rule, position, feature, 5, 5); assessment.ShouldExit {
		t.Fatal("A-share position must not exit on its entry day")
	}
	assessment := engine.EvaluateBacktestExit(rule, position, feature, 6, 5)
	if !assessment.ShouldExit || assessment.ExitReason != "stop_loss" || assessment.ExitPrice != 80 {
		t.Fatalf("gap stop must fill at worse opening price, got %+v", assessment)
	}
}

func TestCloseConditionExecutesNextOpen(t *testing.T) {
	engine := NewRiskEngine(nil)
	rule := Rule{
		StopLoss: 0.12, StopGain: 0.20, MaxHoldDays: 10,
		ExitConditions: []Condition{{Indicator: "Close", Operator: "cross_down", Ref: "MA10"}},
	}
	previous := models.StockFeature{Close: 11, MA10: 10}
	current := models.StockFeature{Open: 10, High: 10, Low: 9, Close: 9, MA10: 10}
	assessment := engine.EvaluateBacktestExitWithPrevious(rule, PortfolioPosition{EntryPrice: 10, BuyDayIndex: 1}, current, &previous, 2, 10)
	if !assessment.ShouldExit || !assessment.ExitAtNextOpen || assessment.ExitReason != "exit_condition" {
		t.Fatalf("close condition should queue a next-open exit, got %+v", assessment)
	}
}

func TestFillSimulatorRejectsLimitUpAndUsesLots(t *testing.T) {
	config := DefaultBacktestConfig()
	simulator := NewFillSimulator(config)
	previous := models.StockFeature{Close: 100}
	limitUp := models.StockFeature{Open: 110, High: 112, Low: 109, Close: 111, Volume: 100000}
	order := SimOrder{ID: "buy", StockCode: "sh600000", Side: OrderSideBuy, Quantity: 150, PriceHint: 110}
	if _, ok := simulator.Fill(order, limitUp, &previous, "2026-01-02"); ok {
		t.Fatal("normal-board stock opening at limit-up must reject a buy")
	}
	tradable := models.StockFeature{Open: 105, High: 108, Low: 103, Close: 106, Volume: 100000}
	order.PriceHint = tradable.Open
	fill, ok := simulator.Fill(order, tradable, &previous, "2026-01-02")
	if !ok || fill.Quantity != 100 {
		t.Fatalf("buy quantity must round down to one board lot, got %+v", fill)
	}
}

func TestFillSimulatorUsesRequestedExecutionPriceForLimitCheck(t *testing.T) {
	config := DefaultBacktestConfig()
	simulator := NewFillSimulator(config)
	previous := models.StockFeature{Close: 100}
	recoveredFromLimitDown := models.StockFeature{Open: 90, High: 96, Low: 90, Close: 95, Volume: 100000}
	order := SimOrder{ID: "sell", StockCode: "sh600000", Side: OrderSideSell, Quantity: 100, PriceHint: 95}
	if _, ok := simulator.Fill(order, recoveredFromLimitDown, &previous, "2026-01-02"); !ok {
		t.Fatal("a close sale above the lower limit should be tradable even when the stock opened limit-down")
	}
}

func TestSellStampDutyExcludesETF(t *testing.T) {
	config := DefaultBacktestConfig()
	simulator := NewFillSimulator(config)
	feature := models.StockFeature{Open: 10, High: 11, Low: 9, Close: 10, Volume: 100000}
	stockFill, stockOK := simulator.Fill(SimOrder{ID: "s1", StockCode: "sh600000", Side: OrderSideSell, Quantity: 1000, PriceHint: 10}, feature, nil, "2026-01-02")
	etfFill, etfOK := simulator.Fill(SimOrder{ID: "s2", StockCode: "sh515880", Side: OrderSideSell, Quantity: 1000, PriceHint: 10}, feature, nil, "2026-01-02")
	if !stockOK || !etfOK || stockFill.Fee <= etfFill.Fee {
		t.Fatalf("stock sell fee should include stamp duty: stock=%v etf=%v", stockFill.Fee, etfFill.Fee)
	}
}

func TestParseStockConceptsPrefersJSONArray(t *testing.T) {
	concepts := parseStockConcepts(`["人工智能","商业航天","人工智能"]`)
	if len(concepts) != 2 || concepts[0] != "人工智能" || concepts[1] != "商业航天" {
		t.Fatalf("unexpected concepts: %#v", concepts)
	}
}
