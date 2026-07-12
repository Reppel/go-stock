package backtest

import (
	"go-stock/backend/models"
	"math"
	"testing"
)

func TestNormalizeCandidateStockCode(t *testing.T) {
	cases := map[string]string{
		"600000.SH": "sh600000",
		"sz000001":  "sz000001",
		"430047":    "bj430047",
		"000001":    "sz000001",
		"bad":       "",
	}
	for input, expected := range cases {
		if actual := NormalizeCandidateStockCode(input); actual != expected {
			t.Fatalf("NormalizeCandidateStockCode(%q)=%q, want %q", input, actual, expected)
		}
	}
}

func TestCandidateAdviceNeverBecomesFormalBuy(t *testing.T) {
	advice := candidateAdvice("短线爆发", "短线爆发", map[string]float64{"短线爆发": 99}, models.StockFeature{Close: 10, ATR: 0.3}, []string{"test"}, nil)
	if advice.Action != "WATCH" || advice.FormalAdviceReady {
		t.Fatalf("five-source candidate bypassed validation gate: %+v", advice)
	}
	longTerm := candidateAdvice("长期配置", "趋势持有", map[string]float64{"长期配置": -1}, models.StockFeature{Close: 10}, nil, nil)
	if longTerm.Action != "AVOID" || longTerm.Status != "insufficient_data" {
		t.Fatalf("long-term advice must expose unavailable fundamentals: %+v", longTerm)
	}
}

func TestCandidateSourceAblation(t *testing.T) {
	items := []models.CandidateSnapshotItem{{StockCode: "sh600000"}, {StockCode: "sz000001"}}
	facts := []models.CandidateSourceFact{
		{StockCode: "sh600000", Source: "event"}, {StockCode: "sh600000", Source: "indicator"},
		{StockCode: "sz000001", Source: "event"}, {StockCode: "sz000001", Source: "pattern"},
	}
	result := candidateSourceAblation([]string{"event", "indicator", "pattern"}, 2, items, facts)
	rows := result["sources"].(map[string]any)
	event := rows["event"].(map[string]any)
	if event["remaining"].(int) != 0 || math.Abs(event["dropRatio"].(float64)-1) > 1e-9 {
		t.Fatalf("unexpected event ablation: %+v", event)
	}
}

func TestDailyCrossSectionalCorrelationDoesNotPoolDates(t *testing.T) {
	rows := []researchObservation{
		{Date: "2026-01-01", Factor: 1, ForwardReturn: 1}, {Date: "2026-01-01", Factor: 2, ForwardReturn: 2}, {Date: "2026-01-01", Factor: 3, ForwardReturn: 3},
		{Date: "2026-01-02", Factor: 1, ForwardReturn: 3}, {Date: "2026-01-02", Factor: 2, ForwardReturn: 2}, {Date: "2026-01-02", Factor: 3, ForwardReturn: 1},
	}
	ic, rankIC, _ := dailyCrossSectionalCorrelation(rows)
	if math.Abs(ic) > 1e-9 || math.Abs(rankIC) > 1e-9 {
		t.Fatalf("expected daily IC average to cancel, got IC=%f rankIC=%f", ic, rankIC)
	}
}
