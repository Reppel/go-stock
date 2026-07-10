package backtest

import (
	"go-stock/backend/models"
	"testing"
)

func TestEvaluateDecisionForSceneUsesPriceThresholds(t *testing.T) {
	engine := NewAlertEngine()
	base := models.PredictionDecision{
		ID:              10,
		SessionID:       2,
		StockCode:       "sh515880",
		StockName:       "通信ETF",
		HoldingVolume:   1800,
		Action:          "REDUCE",
		CurrentPrice:    0.806,
		DefensePrice:    0.774,
		StopLossPrice:   0.743,
		TakeProfitPrice: 0.822,
	}

	if alerts := engine.EvaluateDecisionForScene(base, "短线爆发", "watch"); len(alerts) != 0 {
		t.Fatalf("static REDUCE recommendation should not alert before a threshold, got %d", len(alerts))
	}

	base.CurrentPrice = 0.742
	alerts := engine.EvaluateDecisionForScene(base, "短线爆发", "watch")
	if len(alerts) != 1 || alerts[0].Reason != "stop_loss" || alerts[0].SuggestedAction != "SELL" || alerts[0].ThresholdPrice != base.StopLossPrice {
		t.Fatalf("unexpected stop-loss alert: %#v", alerts)
	}

	base.CurrentPrice = 0.773
	alerts = engine.EvaluateDecisionForScene(base, "短线爆发", "watch")
	if len(alerts) != 1 || alerts[0].Reason != "defense_price" || alerts[0].SuggestedAction != "REDUCE" {
		t.Fatalf("unexpected defense alert: %#v", alerts)
	}

	base.CurrentPrice = 0.823
	alerts = engine.EvaluateDecisionForScene(base, "短线爆发", "watch")
	if len(alerts) != 1 || alerts[0].Reason != "take_profit" || alerts[0].SuggestedAction != "REDUCE" {
		t.Fatalf("unexpected take-profit alert: %#v", alerts)
	}
}

func TestEvaluateDecisionForSceneEntrySignal(t *testing.T) {
	decision := models.PredictionDecision{
		ID:           11,
		SessionID:    2,
		StockCode:    "sz300308",
		Action:       "BUY",
		CurrentPrice: 100,
		BuyPriceMin:  99,
		BuyPriceMax:  101,
	}
	alerts := NewAlertEngine().EvaluateDecisionForScene(decision, "趋势持有", "active")
	if len(alerts) != 1 || alerts[0].Reason != "entry_signal" || alerts[0].SuggestedAction != "BUY" || alerts[0].Scene != "趋势持有" {
		t.Fatalf("unexpected entry alert: %#v", alerts)
	}
}
