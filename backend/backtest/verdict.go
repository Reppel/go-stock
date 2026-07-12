package backtest

import (
	"math"
	"strings"
	"time"
)

func (v *WalkForwardValidator) attachVerdict(rule Rule, universe []string, tradingDays []string, timeHorizon int, config BacktestConfig, result *ValidationResult) {
	if result == nil {
		return
	}
	result.CostSensitivity = v.calculateCostSensitivity(rule, universe, tradingDays, timeHorizon, config, result)
	result.OverfitDiagnostics = buildOverfitDiagnostics(rule, result)
	result.OverfitDiagnostics.ParameterStress, result.OverfitDiagnostics.ParameterStability =
		v.calculateParameterStability(rule, universe, tradingDays, timeHorizon, config, result)
	if result.OverfitDiagnostics.ParameterStability == "fragile" {
		result.OverfitDiagnostics.Warnings = append(result.OverfitDiagnostics.Warnings, "parameter perturbation is fragile")
	}
	result.Verdict = BuildQuantVerdict(result, config)
}

func (v *WalkForwardValidator) calculateCostSensitivity(rule Rule, universe []string, tradingDays []string, timeHorizon int, config BacktestConfig, base *ValidationResult) []CostStressResult {
	if len(tradingDays) == 0 {
		return nil
	}
	stresses := []struct {
		name     string
		slippage float64
	}{
		{name: "base", slippage: config.Slippage},
		{name: "slippage_1_5x", slippage: config.Slippage * 1.5},
		{name: "slippage_2x", slippage: config.Slippage * 2},
	}
	results := make([]CostStressResult, 0, len(stresses))
	for _, stress := range stresses {
		metrics := base
		if stress.name != "base" {
			stressConfig := config
			stressConfig.Slippage = stress.slippage
			run, err := NewPortfolioEngine(NewFeatureRepositoryForVersion(stressConfig.FeatureVersion)).
				RunBacktest(rule, universe, tradingDays, timeHorizon, stressConfig)
			if err == nil && run != nil {
				metrics = run
			}
		}
		if metrics == nil {
			continue
		}
		results = append(results, CostStressResult{
			Name:        stress.name,
			Slippage:    stress.slippage,
			FeeRate:     config.FeeRate,
			TotalReturn: metrics.TotalReturn,
			AvgReturn:   metrics.AvgReturn,
			MaxDrawdown: metrics.MaxDrawdown,
			TradeCount:  metrics.TradeCount,
			Passed:      metrics.TradeCount > 0 && metrics.AvgReturn > 0 && metrics.MaxDrawdown <= 0.20,
		})
	}
	return results
}

func buildOverfitDiagnostics(rule Rule, result *ValidationResult) OverfitDiagnostics {
	diagnostics := OverfitDiagnostics{
		ParameterStability: "not_tested",
		RuleComplexity:     len(rule.EntryConditions) + len(rule.ExitConditions),
	}
	if result == nil {
		return diagnostics
	}
	if result.AvgReturn != 0 {
		diagnostics.OutSampleDecay = (result.AvgReturn - result.OutSampleAvgReturn) / math.Abs(result.AvgReturn)
	}
	if len(result.WalkForwardFolds) > 0 {
		positive := 0
		for _, fold := range result.WalkForwardFolds {
			if fold.TradeCount > 0 && fold.AvgReturn > 0 {
				positive++
			}
		}
		diagnostics.FoldConsistency = float64(positive) / float64(len(result.WalkForwardFolds))
	}
	if diagnostics.RuleComplexity > 6 {
		diagnostics.Warnings = append(diagnostics.Warnings, "rule complexity is high; parameter perturbation is required before active use")
	}
	if diagnostics.OutSampleDecay > 0.5 {
		diagnostics.Warnings = append(diagnostics.Warnings, "out-of-sample return decays by more than 50%")
	}
	if diagnostics.FoldConsistency > 0 && diagnostics.FoldConsistency < 0.5 {
		diagnostics.Warnings = append(diagnostics.Warnings, "walk-forward folds are inconsistent")
	}
	return diagnostics
}

func (v *WalkForwardValidator) calculateParameterStability(rule Rule, universe []string, tradingDays []string, timeHorizon int, config BacktestConfig, base *ValidationResult) ([]ParameterStressResult, string) {
	if len(tradingDays) == 0 || base == nil {
		return nil, "not_tested"
	}
	variants := []struct {
		name  string
		scale float64
	}{{name: "threshold_0_95x", scale: 0.95}, {name: "threshold_1_05x", scale: 1.05}}
	results := make([]ParameterStressResult, 0, len(variants))
	passed := 0
	for _, variant := range variants {
		stressed := perturbRule(rule, variant.scale)
		run, err := NewPortfolioEngine(NewFeatureRepositoryForVersion(config.FeatureVersion)).
			RunBacktest(stressed, universe, tradingDays, timeHorizon, config)
		if err != nil || run == nil {
			results = append(results, ParameterStressResult{Name: variant.name})
			continue
		}
		floor := base.AvgReturn * 0.5
		ok := run.TradeCount > 0 && run.AvgReturn > 0 && run.AvgReturn >= floor && run.MaxDrawdown <= math.Max(0.25, base.MaxDrawdown*1.5)
		if ok {
			passed++
		}
		results = append(results, ParameterStressResult{
			Name: variant.name, AvgReturn: run.AvgReturn, MaxDrawdown: run.MaxDrawdown,
			TradeCount: run.TradeCount, Passed: ok,
		})
	}
	if passed == len(variants) {
		return results, "stable"
	}
	return results, "fragile"
}

func perturbRule(rule Rule, scale float64) Rule {
	result := rule
	result.EntryConditions = append([]Condition(nil), rule.EntryConditions...)
	result.ExitConditions = append([]Condition(nil), rule.ExitConditions...)
	for index := range result.EntryConditions {
		if strings.TrimSpace(result.EntryConditions[index].Ref) == "" && strings.TrimSpace(result.EntryConditions[index].RefID) == "" {
			result.EntryConditions[index].Value *= scale
		}
	}
	for index := range result.ExitConditions {
		if strings.TrimSpace(result.ExitConditions[index].Ref) == "" && strings.TrimSpace(result.ExitConditions[index].RefID) == "" {
			result.ExitConditions[index].Value *= scale
		}
	}
	result.StopLoss = clamp(rule.StopLoss*scale, 0.02, 0.12)
	result.StopGain = clamp(rule.StopGain*scale, 0.03, 0.25)
	return result
}

// ApplyResearchEvidenceToVerdict attaches research-layer stability evidence
// before the final lifecycle verdict is persisted.
func ApplyResearchEvidenceToVerdict(result *ValidationResult, ideas []ResearchIdea) {
	if result == nil {
		return
	}
	var sectorIC, sectorStd, decay float64
	count, samples := 0, 0
	for _, idea := range ideas {
		for _, candidate := range idea.CandidateFactors {
			evidence := candidate.Evidence
			if evidence.SampleSize <= 0 {
				continue
			}
			sectorIC += evidence.SectorIC
			sectorStd += evidence.SectorICStd
			if evidence.IC != 0 {
				decay += (math.Abs(evidence.IC) - math.Abs(evidence.RecentIC)) / math.Abs(evidence.IC)
			}
			samples += evidence.SampleSize
			count++
		}
	}
	if count > 0 {
		result.OverfitDiagnostics.SectorIC = sectorIC / float64(count)
		result.OverfitDiagnostics.SectorICStd = sectorStd / float64(count)
		result.OverfitDiagnostics.ICDecay = decay / float64(count)
		result.OverfitDiagnostics.ResearchSamples = samples
		if result.OverfitDiagnostics.ICDecay > 0.5 {
			result.OverfitDiagnostics.Warnings = append(result.OverfitDiagnostics.Warnings, "research IC decays by more than 50%")
		}
	}
	result.Verdict = BuildQuantVerdict(result, result.BacktestConfig)
}

// ApplyCandidateDiagnosticVerdict prevents a decision-date selected universe
// from using its selection-biased historical metrics for promotion.
func ApplyCandidateDiagnosticVerdict(result *ValidationResult, config BacktestConfig) {
	if result == nil {
		return
	}
	result.OverfitDiagnostics.Warnings = append(result.OverfitDiagnostics.Warnings,
		"candidate universe was selected at the decision date; historical backtest is diagnostic and must be confirmed by forward paper trading")
	result.Verdict = BuildQuantVerdict(result, config)
	if result.NoLookaheadPassed && result.TradeCount > 0 && result.DataCoverage >= config.MinDataCoverage {
		result.Verdict.Status = "paper_trade"
		result.Verdict.Reasons = append(result.Verdict.Reasons,
			"historical result is diagnostic only; eligibility is determined exclusively by post-snapshot forward trading")
		result.Verdict.RequiredActions = append(result.Verdict.RequiredActions,
			"complete point-in-time forward paper trading before active monitoring")
	}
}

func BuildQuantVerdict(result *ValidationResult, config BacktestConfig) QuantVerdict {
	verdict := QuantVerdict{
		Version:     CurrentVerdictVersion,
		Layer:       "quant_judgement",
		Status:      "reject",
		GeneratedAt: time.Now(),
	}
	if result == nil {
		verdict.Risks = append(verdict.Risks, "missing backtest result")
		return verdict
	}
	verdict.Turnover = result.Turnover
	verdict.CostSensitivity = result.CostSensitivity
	verdict.OverfitDiagnostics = result.OverfitDiagnostics
	if !result.NoLookaheadPassed {
		verdict.Risks = append(verdict.Risks, "no-lookahead/T+1 validation failed")
		return verdict
	}
	if result.TradeCount == 0 {
		verdict.Risks = append(verdict.Risks, "no trades generated")
		return verdict
	}
	if result.DataCoverage < config.MinDataCoverage {
		verdict.Risks = append(verdict.Risks, "feature coverage is below configured threshold")
		return verdict
	}
	if result.MaxDrawdown > 0.25 || result.AvgReturn <= 0 {
		verdict.Risks = append(verdict.Risks, "backtest return/drawdown gate failed")
		return verdict
	}
	if costSensitive(result.CostSensitivity) {
		verdict.Status = "watchlist"
		verdict.Risks = append(verdict.Risks, "strategy is cost-sensitive under stressed slippage")
		verdict.RequiredActions = append(verdict.RequiredActions, "review cost model and paper trade before active use")
		return verdict
	}
	if result.TradeCount < MinPoolStrategySamples || !result.BenchmarkAvailable {
		verdict.Status = "watchlist"
		verdict.Reasons = append(verdict.Reasons, "backtest is positive but sample or benchmark gate is incomplete")
		return verdict
	}
	if result.ExcessReturn <= 0 {
		verdict.Status = "watchlist"
		verdict.Risks = append(verdict.Risks, "strategy does not produce positive benchmark excess return")
		return verdict
	}
	if result.OutSampleTradeCount < 15 || result.OutSampleAvgReturn <= 0 {
		verdict.Status = "paper_trade"
		verdict.Reasons = append(verdict.Reasons, "backtest passed; out-of-sample evidence is not strong enough for active")
		verdict.RequiredActions = append(verdict.RequiredActions, "run paper trade for at least configured paperTradeDays")
		return verdict
	}
	if result.OverfitDiagnostics.ParameterStability != "stable" {
		verdict.Status = "paper_trade"
		verdict.Risks = append(verdict.Risks, "parameter stability gate is incomplete or fragile")
		verdict.RequiredActions = append(verdict.RequiredActions, "complete parameter perturbation and paper trade before active use")
		return verdict
	}
	if len(result.OverfitDiagnostics.Warnings) > 0 {
		verdict.Status = "active_candidate"
		verdict.Risks = append(verdict.Risks, result.OverfitDiagnostics.Warnings...)
		verdict.RequiredActions = append(verdict.RequiredActions, "complete parameter perturbation before active use")
		return verdict
	}
	verdict.Status = "active_candidate"
	verdict.Reasons = append(verdict.Reasons, "passed backtest, out-of-sample, benchmark, drawdown, and cost gates")
	return verdict
}

func costSensitive(results []CostStressResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, result := range results {
		if result.Name == "slippage_2x" && !result.Passed {
			return true
		}
	}
	return false
}

func reviewDueAtForVerdict(verdict QuantVerdict, config BacktestConfig) *time.Time {
	days := config.ActiveTTLDays
	if verdict.Status == "paper_trade" {
		days = config.PaperTradeDays
	}
	if days <= 0 {
		days = DefaultBacktestConfig().ActiveTTLDays
	}
	next := time.Now().AddDate(0, 0, days)
	return &next
}
