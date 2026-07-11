package backtest

import (
	"fmt"
	"go-stock/backend/models"
	"testing"
)

func TestWalkForwardUsesPurgedTemporalFolds(t *testing.T) {
	days := make([]string, 100)
	nav := make([]DailyNav, 100)
	trades := make([]Trade, 0, 50)
	for index := range days {
		days[index] = formatSyntheticDate(index)
		nav[index] = DailyNav{Date: days[index], Nav: 1 + float64(index)*0.001}
		if index%2 == 0 {
			trades = append(trades, Trade{BuyDate: days[index], ReturnRate: 0.01})
		}
	}
	folds, avgReturn, _, count := new(WalkForwardValidator).calculateWalkForwardMetrics(trades, nav, days, 5)
	if len(folds) != 3 || count == 0 || avgReturn <= 0 {
		t.Fatalf("unexpected walk-forward result: folds=%d count=%d avg=%v", len(folds), count, avgReturn)
	}
	if folds[0].StartDate <= days[40] {
		t.Fatalf("purge window was not applied: first test date %s", folds[0].StartDate)
	}
}

func TestNoLookaheadValidationRejectsSameDayTrade(t *testing.T) {
	config := DefaultBacktestConfig()
	if validateNoLookahead([]Trade{{SignalDate: "2026-01-01", BuyDate: "2026-01-02", SellDate: "2026-01-02"}}, config) {
		t.Fatal("same-day A-share exit must fail no-lookahead/T+1 validation")
	}
	if !validateNoLookahead([]Trade{{SignalDate: "2026-01-01", BuyDate: "2026-01-02", SellDate: "2026-01-05"}}, config) {
		t.Fatal("proper T+1 trade should pass validation")
	}
}

func TestDecisionQualityUsesOnlyMatchedPositiveStrategies(t *testing.T) {
	if got := decisionQualityRating(0, 100, false, true, true); got != "observe" {
		t.Fatalf("unmatched strategies must not produce reference quality, got %s", got)
	}
	if got := decisionQualityRating(1, 30, false, true, true); got != "reference" {
		t.Fatalf("matched positive strategy with enough stock samples should be reference quality, got %s", got)
	}
	if got := decisionQualityRating(1, 30, false, true, false); got != "blocked" {
		t.Fatalf("stale data must block decision quality, got %s", got)
	}
}

func TestMonitorReadinessRequiresBenchmarkAndCurrentVersions(t *testing.T) {
	hypothesis := models.PredictionHypothesis{
		StrategyVersion: CurrentStrategyVersion, FeatureVersion: CurrentFeatureVersion,
		NoLookaheadPassed: true, TradeCount: MinPoolStrategySamples, OutSampleTradeCount: 15,
		MaxDrawdown: 0.10, AvgReturn: 0.01, OutSampleAvgReturn: 0.005,
		DataCoverage: DefaultBacktestConfig().MinDataCoverage,
	}
	if hypothesisMonitorReady(hypothesis) {
		t.Fatal("missing benchmark data must block formal monitoring")
	}
	hypothesis.BenchmarkAvailable = true
	hypothesis.ExcessReturn = 0.01
	if !hypothesisMonitorReady(hypothesis) {
		t.Fatal("a hypothesis passing every strict gate should be monitor-ready")
	}
}

func TestRuleValidationRejectsIndicatorUnitMismatch(t *testing.T) {
	rule := Rule{
		EntryConditions: []Condition{{Indicator: "KDJ_K", Operator: "cross_up", Ref: "MA5"}},
		ExitConditions:  []Condition{{Indicator: "Close", Operator: "<", Ref: "MA10"}},
		StopLoss:        0.07, StopGain: 0.12, MaxHoldDays: 5, MaxHoldings: 5,
	}
	errs := ValidateHypothesisRule(rule)
	if len(errs) == 0 {
		t.Fatal("KDJ oscillator must not be compared with a price moving average")
	}
}

func TestIndicatorRegistrySeparatesActiveAndCandidate(t *testing.T) {
	active := QueryIndicatorRegistry(IndicatorQuery{Status: "active"})
	candidates := QueryIndicatorRegistry(IndicatorQuery{Status: "candidate"})
	if len(active) == 0 || len(candidates) == 0 {
		t.Fatalf("expected both active and candidate indicators, active=%d candidate=%d", len(active), len(candidates))
	}
	for _, indicator := range active {
		if indicator.Source != "feature" {
			t.Fatalf("only stock_feature indicators should be active in current executor, got %+v", indicator)
		}
	}
}

func TestRuleEnvelopeV2CompilesToLegacyRule(t *testing.T) {
	rule := Rule{
		EntryConditions: []Condition{{Indicator: "MA5", Operator: "cross_up", Ref: "MA20"}},
		ExitConditions:  []Condition{{Indicator: "MA5", Operator: "cross_down", Ref: "MA20"}},
		StopLoss:        0.07, StopGain: 0.12, MaxHoldDays: 5, MaxHoldings: 5,
	}
	raw := RuleToJSON(rule)
	compiled, err := JSONToRule(raw)
	if err != nil {
		t.Fatalf("v2 envelope should compile: %v", err)
	}
	if len(compiled.EntryConditions) != 2 || compiled.EntryConditions[0].Lag != 1 || compiled.EntryConditions[1].Lag != 0 ||
		compiled.EntryConditions[0].Indicator != "MA5" || compiled.EntryConditions[0].Ref != "MA20" {
		t.Fatalf("unexpected compiled rule: %+v", compiled)
	}
}

func TestProfitLossRatioReportsNoLossStatus(t *testing.T) {
	result := new(WalkForwardValidator).calculateMetrics(
		[]Trade{{ReturnRate: 0.01, Hit: true}, {ReturnRate: 0.02, Hit: true}},
		[]DailyNav{{Date: "2026-01-01", Nav: 1}, {Date: "2026-01-02", Nav: 1.02}},
	)
	if result.ProfitLossRatioStatus != "no_loss" || result.ProfitLossRatio != 0 {
		t.Fatalf("expected no_loss semantic status, got ratio=%v status=%s", result.ProfitLossRatio, result.ProfitLossRatioStatus)
	}
}

func TestPositionSizerDoesNotDefaultToOneLotWithoutAccount(t *testing.T) {
	sizing := NewPositionSizer().Size("BUY", 20, 10, 0, 0)
	if sizing.SuggestedQuantity != 0 || sizing.AccountWarning == "" {
		t.Fatalf("missing account constraints must not create a one-lot buy: %+v", sizing)
	}
}

func formatSyntheticDate(index int) string {
	return fmt.Sprintf("2026-%02d-%02d", index/28+1, index%28+1)
}
