package backtest

import (
	"encoding/json"
	"go-stock/backend/models"
	"math"
	"strings"
	"testing"
	"time"
)

func TestTemplateGenerationUsesOneStrategyPerApplicableFamily(t *testing.T) {
	tests := []struct {
		scene string
		want  int
	}{
		{scene: "短线爆发", want: 3},
		{scene: "波段反弹", want: 1},
		{scene: "趋势持有", want: 2},
	}
	for _, test := range tests {
		hypotheses, err := NewAIGenerator().Generate(test.scene, "自选股", MarketContext{})
		if err != nil || len(hypotheses) != test.want {
			t.Fatalf("scene %s: want %d canonical families, got %d err=%v", test.scene, test.want, len(hypotheses), err)
		}
		seen := map[string]bool{}
		for _, hypothesis := range hypotheses {
			if seen[hypothesis.Name] {
				t.Fatalf("scene %s generated duplicate family %s", test.scene, hypothesis.Name)
			}
			seen[hypothesis.Name] = true
			if strings.Contains(hypothesis.Name, "目标") {
				t.Fatalf("parameters must not be presented as separate strategies: %s", hypothesis.Name)
			}
			if hypothesis.Rule.MaxHoldDays != hypothesis.TimeHorizon || hypothesis.Rule.StopGain != hypothesis.TargetReturn {
				t.Fatalf("training/decision parameters diverge: %+v", hypothesis)
			}
		}
		if test.scene == "短线爆发" && !seen["MACD 金叉"] {
			t.Fatal("short-term family allocation must include MACD")
		}
	}
}

func TestRuleExecutionDeduplicationIgnoresPresentationMetadata(t *testing.T) {
	rule := Rule{
		EntryConditions: []Condition{{Indicator: "RSI6", Operator: "<", Value: 30}},
		StopLoss:        0.05, StopGain: 0.05, MaxHoldDays: 10, MaxHoldings: 5,
	}
	hypotheses := deduplicateHypotheses([]Hypothesis{
		{Name: "first", Rule: rule, TargetReturn: 0.03},
		{Name: "renamed", Rule: rule, TargetReturn: 0.08},
	}, 4)
	if len(hypotheses) != 1 {
		t.Fatalf("same executable rule must have one vote, got %d", len(hypotheses))
	}
}

func TestUnifiedFeatureViewExecutesExplicitLagConditions(t *testing.T) {
	engine := NewStrategyEngine()
	previous := models.StockFeature{StockCode: "test", Date: "2026-01-01", MA5: 9, MA20: 10, DataAsOf: time.Now().Add(-24 * time.Hour)}
	current := models.StockFeature{StockCode: "test", Date: "2026-01-02", MA5: 11, MA20: 10, DataAsOf: time.Now()}
	rule := NormalizeHypothesis(Hypothesis{Rule: Rule{
		EntryConditions: []Condition{{Indicator: "MA5", Operator: "cross_up", Ref: "MA20"}},
	}}).Rule
	if len(rule.EntryConditions) != 2 || !engine.MatchConditionsWithPrevious(rule.EntryConditions, current, &previous) {
		t.Fatalf("explicit lag transition must match through UnifiedFeatureView: %+v", rule.EntryConditions)
	}
}

func TestRuleEnvelopeV2RejectsImplicitCross(t *testing.T) {
	envelope := BuildRuleEnvelope(Hypothesis{
		Name: "cross", Scene: "短线爆发", TimeHorizon: 5, TargetReturn: 0.05,
		Rule: Rule{
			EntryConditions: []Condition{{Indicator: "RSI6", Operator: "<", Value: 30}},
			ExitConditions:  []Condition{{Indicator: "MA5", Operator: "cross_down", Ref: "MA20"}},
			StopLoss:        0.05, StopGain: 0.05, MaxHoldDays: 5, MaxHoldings: 5,
		},
	}, nil)
	envelope.Exit.Any[0].PreviousLeft = nil
	envelope.Exit.Any[0].PreviousRight = nil
	raw, _ := json.Marshal(envelope)
	if _, errs := ValidatePredictionRule(string(raw)); len(errs) == 0 {
		t.Fatal("v2 cross must declare previous operands explicitly")
	}
}

func TestResearchStatisticsAreComputedFromObservations(t *testing.T) {
	x := []float64{1, 2, 3, 4, 5}
	y := []float64{2, 4, 6, 8, 10}
	if correlation := pearsonCorrelation(x, y); math.Abs(correlation-1) > 1e-9 {
		t.Fatalf("expected measured correlation 1, got %v", correlation)
	}
	if p := correlationPValue(0, 100); p < 0.99 {
		t.Fatalf("zero correlation should not be significant, p=%v", p)
	}
}

func TestPaperTradeRequiresTradingDaysTradesAndPositiveNAV(t *testing.T) {
	now := time.Now()
	hypothesis := models.PredictionHypothesis{
		PaperTradeDays:      DefaultBacktestConfig().PaperTradeDays,
		PaperTradeCount:     MinPaperTradeValidations,
		PaperNav:            1.05,
		OutSampleTradeCount: 15,
		OutSampleAvgReturn:  0.01,
		BenchmarkAvailable:  true,
		ExcessReturn:        0.01,
	}
	if ready, reason := paperTradeReady(hypothesis, now); !ready {
		t.Fatalf("qualified paper trade should be active-ready: %s", reason)
	}
	hypothesis.PaperNav = 0.99
	if ready, _ := paperTradeReady(hypothesis, now); ready {
		t.Fatal("negative paper-trade outcome must not promote to active")
	}
}
