package backtest

import (
	"go-stock/backend/models"
	"testing"
)

func TestCrossOperatorsRequirePreviousState(t *testing.T) {
	engine := NewStrategyEngine()
	previous := models.StockFeature{MA5: 9, MA20: 10, MACD: -0.1}
	current := models.StockFeature{MA5: 11, MA20: 10, MACD: 0.1}
	if !engine.MatchConditionWithPrevious(Condition{Indicator: "MA5", Operator: "cross_up", Ref: "MA20"}, current, &previous) {
		t.Fatal("expected MA5 cross_up MA20")
	}
	if !engine.MatchConditionWithPrevious(Condition{Indicator: "MACD", Operator: "cross_up", Value: 0}, current, &previous) {
		t.Fatal("expected MACD zero-axis cross")
	}
	if engine.MatchConditionWithPrevious(Condition{Indicator: "MA5", Operator: "cross_up", Ref: "MA20"}, current, nil) {
		t.Fatal("cross operator must not match without a previous bar")
	}
	if engine.MatchConditions(nil, current) {
		t.Fatal("empty conditions must not generate a signal")
	}
}
