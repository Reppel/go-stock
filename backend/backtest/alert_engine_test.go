package backtest

import (
	"go-stock/backend/models"
	"math"
	"testing"
	"time"
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

func TestRealtimeAlertRequiresFreshSameDayQuote(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, 7, 13, 10, 0, 0, 0, loc)
	if !freshAlertQuote(now.Add(-2*time.Minute), now) {
		t.Fatal("same-day quote within freshness window should be accepted")
	}
	if freshAlertQuote(now.Add(-4*time.Minute), now) {
		t.Fatal("stale quote must not drive a realtime alert")
	}
	if freshAlertQuote(now.AddDate(0, 0, -1), now) {
		t.Fatal("previous-day quote must not drive a realtime alert")
	}
}

func TestRealtimeHoldingOverridesFrozenDecision(t *testing.T) {
	decision := models.PredictionDecision{
		HoldingVolume: 1000, CostPrice: 10,
		DefensePrice: 9.8, StopLossPrice: 9.5, TakeProfitPrice: 11,
	}
	applyRealtimeHolding(&decision, holdingSnapshot{volume: 2000, costPrice: 8})
	if decision.HoldingVolume != 2000 || decision.CostPrice != 8 {
		t.Fatalf("holding was not refreshed: %#v", decision)
	}
	if math.Abs(decision.DefensePrice-7.84) > 1e-9 || math.Abs(decision.StopLossPrice-7.6) > 1e-9 || math.Abs(decision.TakeProfitPrice-8.8) > 1e-9 {
		t.Fatalf("risk thresholds were not rebased to current cost: %#v", decision)
	}
	applyRealtimeHolding(&decision, holdingSnapshot{})
	if decision.HoldingVolume != 0 || decision.CostPrice != 0 {
		t.Fatalf("closed position must disable holding alerts: %#v", decision)
	}
}

func TestEvaluateDecisionForSceneEntrySignal(t *testing.T) {
	decision := models.PredictionDecision{
		ID:            11,
		SessionID:     2,
		StockCode:     "sz300308",
		Action:        "BUY",
		CurrentPrice:  100,
		BuyPriceMin:   99,
		BuyPriceMax:   101,
		QualityRating: "reference",
		Confidence:    "medium",
		RiskLevel:     "low",
	}
	alerts := NewAlertEngine().EvaluateDecisionForScene(decision, "趋势持有", "active")
	if len(alerts) != 1 || alerts[0].Reason != "entry_signal" || alerts[0].SuggestedAction != "BUY" || alerts[0].Scene != "趋势持有" {
		t.Fatalf("unexpected entry alert: %#v", alerts)
	}
	if watchAlerts := NewAlertEngine().EvaluateDecisionForScene(decision, "趋势持有", "watch"); len(watchAlerts) != 0 {
		t.Fatalf("watch-only strategies must not emit executable entry alerts: %#v", watchAlerts)
	}
}

func TestSelectMonitoredHypothesesPrefersFormalStrategies(t *testing.T) {
	hypotheses := []models.PredictionHypothesis{
		{ID: 1, Status: "watch"},
		{ID: 2, Status: "active"},
		{ID: 3, Status: "watch"},
	}
	selected := selectMonitoredHypotheses(hypotheses)
	if len(selected) != 1 || selected[0].ID != 2 {
		t.Fatalf("formal monitoring must not mix in watch-only strategies: %#v", selected)
	}
}
