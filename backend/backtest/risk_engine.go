package backtest

import (
	"go-stock/backend/models"
	"math"
)

type RiskEngine struct {
	strategy *StrategyEngine
}

func NewRiskEngine(strategy *StrategyEngine) *RiskEngine {
	if strategy == nil {
		strategy = NewStrategyEngine()
	}
	return &RiskEngine{strategy: strategy}
}

func (r *RiskEngine) EvaluateBacktestExit(rule Rule, pos PortfolioPosition, f models.StockFeature, dayIndex int, timeHorizon int) RiskAssessment {
	stopLoss := normalizePct(rule.StopLoss, 0.03, 0.15)
	stopGain := normalizePct(rule.StopGain, 0.02, 0.30)
	stopLossPrice := pos.EntryPrice * (1 - stopLoss)
	takeProfitPrice := pos.EntryPrice * (1 + stopGain)
	defensePrice := pos.EntryPrice * 0.98

	assessment := RiskAssessment{
		RiskLevel:       "medium",
		CanEnter:        true,
		DefensePrice:    defensePrice,
		StopLossPrice:   stopLossPrice,
		TakeProfitPrice: takeProfitPrice,
	}
	if f.Low > 0 && f.Low <= stopLossPrice {
		assessment.RiskLevel = "high"
		assessment.ShouldExit = true
		assessment.ExitReason = "stop_loss"
		assessment.ExitPrice = stopLossPrice
		assessment.Warnings = append(assessment.Warnings, "触发止损")
		return assessment
	}
	if f.High > 0 && f.High >= takeProfitPrice {
		assessment.RiskLevel = "medium"
		assessment.ShouldExit = true
		assessment.ExitReason = "stop_gain"
		assessment.ExitPrice = takeProfitPrice
		assessment.Reasons = append(assessment.Reasons, "触发止盈")
		return assessment
	}
	if r.strategy.MatchConditions(rule.ExitConditions, f) {
		assessment.RiskLevel = "medium"
		assessment.ShouldExit = true
		assessment.ExitReason = "exit_condition"
		assessment.ExitPrice = f.Close
		assessment.Warnings = append(assessment.Warnings, "触发策略退出条件")
		return assessment
	}

	maxHoldDays := rule.MaxHoldDays
	if maxHoldDays <= 0 {
		maxHoldDays = timeHorizon
	}
	if maxHoldDays > 0 && dayIndex-pos.BuyDayIndex >= maxHoldDays {
		assessment.RiskLevel = "medium"
		assessment.ShouldExit = true
		assessment.ExitReason = "max_hold_days"
		assessment.ExitPrice = f.Close
		assessment.Warnings = append(assessment.Warnings, "达到最大持有天数")
		return assessment
	}
	return assessment
}

func (r *RiskEngine) EvaluateCurrent(
	hasPosition bool,
	currentPrice float64,
	costPrice float64,
	bestStopLoss float64,
	bestTargetReturn float64,
	score float64,
	sampleWarning bool,
) RiskAssessment {
	if bestStopLoss <= 0 {
		bestStopLoss = 0.03
	}
	bestStopLoss = normalizePct(bestStopLoss, 0.03, 0.07)
	bestTargetReturn = normalizePct(bestTargetReturn, 0.01, 0.08)
	if bestTargetReturn < 0.01 {
		bestTargetReturn = 0.01
	}

	assessment := RiskAssessment{
		RiskLevel: "medium",
		CanEnter:  true,
		RiskScore: clamp(100-score, 0, 100),
	}
	if !hasPosition {
		if sampleWarning || score < 55 {
			assessment.RiskLevel = "high"
			assessment.CanEnter = false
			assessment.Warnings = append(assessment.Warnings, "样本或评分不足，不建议新开仓")
		} else if score >= 75 {
			assessment.RiskLevel = "low"
		}
		return assessment
	}

	assessment.DefensePrice = costPrice * 0.98
	assessment.StopLossPrice = costPrice * (1 - bestStopLoss)
	assessment.TakeProfitPrice = costPrice * (1 + bestTargetReturn)
	if currentPrice <= assessment.StopLossPrice && assessment.StopLossPrice > 0 {
		assessment.RiskLevel = "high"
		assessment.ShouldExit = true
		assessment.ExitReason = "stop_loss"
		assessment.ExitPrice = assessment.StopLossPrice
		assessment.Warnings = append(assessment.Warnings, "当前价触发止损线")
		return assessment
	}
	if currentPrice <= assessment.DefensePrice && assessment.DefensePrice > 0 {
		assessment.RiskLevel = "high"
		assessment.Warnings = append(assessment.Warnings, "当前价跌破防守位")
		return assessment
	}
	if currentPrice >= assessment.TakeProfitPrice && assessment.TakeProfitPrice > 0 {
		assessment.RiskLevel = "medium"
		assessment.Reasons = append(assessment.Reasons, "当前价进入止盈/减仓区间")
	}
	if score >= 75 && !sampleWarning {
		assessment.RiskLevel = "low"
	}
	if sampleWarning {
		assessment.RiskLevel = "high"
		assessment.Warnings = append(assessment.Warnings, "回测样本不足")
	}
	return assessment
}

func normalizePct(value float64, min float64, max float64) float64 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return min
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
