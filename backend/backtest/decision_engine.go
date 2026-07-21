package backtest

import (
	"encoding/json"
	"fmt"
	"go-stock/backend/models"
	"math"
	"strings"
	"time"
)

type DecisionEngine struct {
	strategy *StrategyEngine
	risk     *RiskEngine
	sizer    *PositionSizer
	capital  *CapitalFlowEngine
	alerts   *AlertEngine
}

func NewDecisionEngine() *DecisionEngine {
	strategy := NewStrategyEngine()
	return &DecisionEngine{
		strategy: strategy,
		risk:     NewRiskEngine(strategy),
		sizer:    NewPositionSizer(),
		capital:  NewCapitalFlowEngine(),
		alerts:   NewAlertEngine(),
	}
}

func (e *DecisionEngine) Build(ctx DecisionContext) models.PredictionDecision {
	if ctx.Session == nil || ctx.Feature.StockCode == "" {
		return models.PredictionDecision{}
	}
	f := ctx.Feature
	hypotheses := deduplicateModelHypotheses(ctx.Hypotheses)
	quote := ctx.Quote
	holding := ctx.Holding
	currentPrice := f.Close
	quoteUsable := quoteCompatibleWithFeature(quote, f)
	if quoteUsable && quote.price > 0 {
		currentPrice = quote.price
	}

	stockName := stockNameOrCode(f.StockCode, "")
	if strings.TrimSpace(holding.name) != "" {
		stockName = holding.name
	}
	if strings.TrimSpace(quote.name) != "" {
		stockName = quote.name
	}

	dataAsOf := f.DataAsOf
	if quoteUsable && !quote.at.IsZero() {
		dataAsOf = quote.at
	}
	if dataAsOf.IsZero() {
		dataAsOf = time.Now()
	}

	score := 50.0
	entryMatchedCount := 0
	exitMatchedCount := 0
	positiveMatchedCount := 0
	negativeStrategyCount := 0
	noLookaheadOK := true
	bestTargetReturn := 0.01
	bestRuleStopLoss := 0.0
	matchedFacts := make([]matchedStrategyFact, 0, len(hypotheses))
	sampleSummaries := make([]StrategySampleSummary, 0, len(hypotheses))
	reasons := make([]string, 0, 10)
	risks := make([]string, 0, 10)
	stockTradeStats := loadStockTradeStats(hypotheses, f.StockCode)
	poolSampleCount, stockSampleCount := loadUniqueTradeSamples(hypotheses, f.StockCode)
	relevantStrategyCount := 0
	relevantSampleReady := false
	probabilityWeighted := 0.0
	expectedReturnWeighted := 0.0
	targetReturnWeighted := 0.0
	matchedWeight := 0.0
	dataReady := f.Adjusted && f.FeatureVersion == CurrentFeatureVersion && featureFreshForDecision(f, time.Now())

	for _, h := range hypotheses {
		rule, err := JSONToRule(h.RuleJSON)
		if err != nil {
			continue
		}
		entryMatched := e.strategy.MatchConditionsWithPrevious(rule.EntryConditions, f, ctx.PreviousFeature)
		exitMatched := e.strategy.MatchAnyConditionWithPrevious(rule.ExitConditions, f, ctx.PreviousFeature)
		monitorReady := hypothesisMonitorReady(h)
		stockStat := stockTradeStats[h.ID]
		stockSamples := stockStat.Count
		sampleReady := h.TradeCount >= MinPoolStrategySamples && stockSamples >= MinStockStrategySamples
		if entryMatched || exitMatched {
			relevantStrategyCount++
			if sampleReady && monitorReady {
				relevantSampleReady = true
			}
			if !h.NoLookaheadPassed {
				noLookaheadOK = false
			}
			weight := math.Sqrt(float64(stockSamples) + 1)
			posteriorProbability := (float64(stockStat.Wins) + 2) / (float64(stockSamples) + 4)
			if stockSamples == 0 {
				poolWeight := math.Min(float64(h.TradeCount), 30)
				posteriorProbability = (h.WinRate*poolWeight + 2) / (poolWeight + 4)
			}
			shrunkReturn := (stockStat.AvgReturn*float64(stockSamples) + h.OutSampleAvgReturn*5) / (float64(stockSamples) + 5)
			probabilityWeighted += posteriorProbability * weight
			expectedReturnWeighted += shrunkReturn * weight
			targetReturnWeighted += h.TargetReturn * weight
			matchedWeight += weight
			if shrunkReturn < 0 {
				negativeStrategyCount++
			}
			if rule.StopLoss > 0 && (bestRuleStopLoss == 0 || rule.StopLoss < bestRuleStopLoss) {
				bestRuleStopLoss = rule.StopLoss
			}
		}

		if entryMatched {
			entryMatchedCount++
			if sampleReady && monitorReady {
				positiveMatchedCount++
			}
			if !sampleReady {
				score -= 3
			}
			reasons = append(reasons, fmt.Sprintf("命中策略「%s」入场条件", h.Name))
		}
		if exitMatched {
			exitMatchedCount++
			score -= 12
			risks = append(risks, fmt.Sprintf("策略「%s」触发退出条件", h.Name))
		}
		matchedFacts = append(matchedFacts, matchedStrategyFact{
			ID:              h.ID,
			Name:            h.Name,
			EntryMatched:    entryMatched,
			ExitMatched:     exitMatched,
			WinRate:         h.WinRate,
			AvgReturn:       h.AvgReturn,
			MaxDrawdown:     h.MaxDrawdown,
			TradeCount:      h.TradeCount,
			PoolTradeCount:  h.TradeCount,
			StockTradeCount: stockSamples,
			SampleReady:     sampleReady,
			MonitorReady:    monitorReady,
		})
		sampleSummaries = append(sampleSummaries, StrategySampleSummary{
			HypothesisID: h.ID,
			StrategyName: h.Name,
			PoolSamples:  h.TradeCount,
			StockSamples: stockSamples,
			EntryMatched: entryMatched,
			ExitMatched:  exitMatched,
			SampleReady:  sampleReady,
		})
	}
	probability := 0.5
	expectedReturn := 0.0
	if matchedWeight > 0 {
		probability = probabilityWeighted / matchedWeight
		expectedReturn = expectedReturnWeighted / matchedWeight
		bestTargetReturn = targetReturnWeighted / matchedWeight
		score += (probability-0.5)*60 + clamp(expectedReturn*100, -10, 10)
	}
	sampleWarning := relevantStrategyCount == 0 || !relevantSampleReady || !dataReady

	if f.MA20 > 0 && f.Close < f.MA20 && f.MACD < 0 {
		score -= 8
		risks = append(risks, "MACD为负且价格低于MA20，趋势仍偏弱")
	}
	if f.RSI6 > 0 && f.RSI6 < 30 {
		score += 4
		reasons = append(reasons, "RSI6处于超卖区，存在反弹观察价值")
	}
	if f.VolumeRatio >= 1.2 {
		score += 3
		reasons = append(reasons, "量比高于1.2，短线关注度提升")
	}
	if !f.Adjusted {
		risks = append(risks, "特征未复权，已阻止交易建议")
	}
	if f.FeatureVersion != CurrentFeatureVersion {
		risks = append(risks, "特征版本过旧，已阻止交易建议")
	}
	if !featureFreshForDecision(f, time.Now()) {
		risks = append(risks, "特征数据已过期，历史会话不生成当前交易动作")
	}
	if quote.price > 0 && !quoteUsable {
		risks = append(risks, "实时行情与特征日期不一致，本次使用特征收盘价")
	}
	if f.MA20 > 0 && math.Abs(f.Close/f.MA20-1) > 0.25 {
		risks = append(risks, "价格与MA20偏离过大，请复核复权口径")
	}

	capitalFlow := e.capital.GetStockFlow(f.StockCode, f.Date, f)
	score += capitalFlow.ScoreAdjustment
	reasons = append(reasons, capitalFlow.Reasons...)
	risks = append(risks, capitalFlow.Risks...)
	dataStatus := loadDecisionDataStatus(ctx.Session, f)
	risks = append(risks, dataStatus.Warnings...)

	if relevantStrategyCount == 0 || !relevantSampleReady {
		risks = append(risks, fmt.Sprintf("当前股票没有通过正式质量门槛且样本充足的命中策略：单策略至少需要股票池%d笔且单股%d笔", MinPoolStrategySamples, MinStockStrategySamples))
	}
	if !dataReady {
		score = math.Min(score, 40)
	}
	if !noLookaheadOK {
		risks = append(risks, "存在未通过未来函数检查的策略，不允许作为正式依据")
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "当前没有强入场信号")
	}
	score = clamp(score, 0, 100)

	costPrice := holding.costPrice
	holdingVolume := holding.volume
	hasPosition := costPrice > 0 && holdingVolume > 0
	profitRate := 0.0
	if hasPosition && currentPrice > 0 {
		profitRate = (currentPrice - costPrice) / costPrice
	}

	risk := e.risk.EvaluateCurrent(hasPosition, currentPrice, costPrice, bestRuleStopLoss, bestTargetReturn, score, sampleWarning || !noLookaheadOK, ctx.Session.Scene)
	reasons = append(reasons, risk.Reasons...)
	risks = append(risks, risk.Warnings...)

	defensePrice := risk.DefensePrice
	stopLossPrice := risk.StopLossPrice
	takeProfitPrice := risk.TakeProfitPrice
	buyMin := currentPrice * 0.995
	buyMax := currentPrice * 1.005
	sellMin := currentPrice * 0.995
	sellMax := currentPrice * 1.005

	action, quantityPercent := decideAction(hasPosition, currentPrice, defensePrice, stopLossPrice, takeProfitPrice, score, entryMatchedCount, exitMatchedCount, positiveMatchedCount, negativeStrategyCount, sampleWarning || !noLookaheadOK)
	if risk.ShouldExit && hasPosition {
		action = "SELL"
		quantityPercent = 100
	}
	action, quantityPercent = stabilizeAction(ctx.PreviousAction, action, quantityPercent, score, risk.ShouldExit)
	if !dataReady {
		if hasPosition {
			action = "HOLD"
		} else {
			action = "WATCH"
		}
		quantityPercent = 0
	}
	if action == "BUY" || action == "ADD" {
		quantityPercent = math.Min(quantityPercent, riskBudgetPositionPercent(bestRuleStopLoss, sampleWarning))
	}
	sizing := e.sizer.Size(action, quantityPercent, currentPrice, holdingVolume, holding.marketValue)
	if sizing.AccountWarning != "" {
		risks = append(risks, sizing.AccountWarning)
	}
	positionAdvice := buildPositionAdvice(action, quantityPercent, hasPosition)
	if sizing.SuggestedQuantity > 0 {
		positionAdvice = fmt.Sprintf("%s，约%d股/份，参考金额%.2f", positionAdvice, sizing.SuggestedQuantity, sizing.SuggestedAmount)
	}

	quality := decisionQualityRating(positiveMatchedCount, stockSampleCount, sampleWarning, noLookaheadOK, dataReady)
	confidence := decisionConfidence(probability, stockSampleCount, sampleWarning || !noLookaheadOK)

	matchedJSON, _ := json.Marshal(matchedFacts)
	reasonsJSON, _ := json.Marshal(uniqueStrings(reasons))
	risksJSON, _ := json.Marshal(uniqueStrings(risks))
	sizingJSON, _ := json.Marshal(sizing)
	capitalJSON, _ := json.Marshal(capitalFlow)
	sampleSummaryJSON, _ := json.Marshal(sampleSummaries)
	dataStatusJSON, _ := json.Marshal(dataStatus)

	decision := models.PredictionDecision{
		SessionID:             ctx.Session.ID,
		StockCode:             f.StockCode,
		StockName:             stockName,
		DecisionDate:          f.Date,
		Action:                action,
		PreviousAction:        strings.ToUpper(strings.TrimSpace(ctx.PreviousAction)),
		ActionText:            actionText(action),
		PositionAdvice:        positionAdvice,
		QuantityPercent:       quantityPercent,
		SuggestedQuantity:     sizing.SuggestedQuantity,
		SuggestedAmount:       sizing.SuggestedAmount,
		Confidence:            confidence,
		QualityRating:         quality,
		RiskLevel:             risk.RiskLevel,
		Score:                 score,
		Probability:           probability,
		ProbabilityMethod:     "beta_binomial_shrinkage",
		ExpectedReturn:        expectedReturn,
		CurrentPrice:          currentPrice,
		ReferencePrice:        f.Close,
		CostPrice:             costPrice,
		HoldingVolume:         holdingVolume,
		ProfitRate:            profitRate,
		BuyPriceMin:           buyMin,
		BuyPriceMax:           buyMax,
		SellPriceMin:          sellMin,
		SellPriceMax:          sellMax,
		DefensePrice:          defensePrice,
		StopLossPrice:         stopLossPrice,
		TakeProfitPrice:       takeProfitPrice,
		MatchedStrategiesJSON: string(matchedJSON),
		SizingJSON:            string(sizingJSON),
		CapitalFlowJSON:       string(capitalJSON),
		ReasonsJSON:           string(reasonsJSON),
		RisksJSON:             string(risksJSON),
		SampleWarning:         sampleWarning || !noLookaheadOK,
		PoolSampleCount:       poolSampleCount,
		StockSampleCount:      stockSampleCount,
		SampleSummaryJSON:     string(sampleSummaryJSON),
		DataStatusJSON:        string(dataStatusJSON),
		FeatureVersion:        f.FeatureVersion,
		DataAsOf:              dataAsOf,
		Status:                "draft",
	}
	alerts := e.alerts.EvaluateDecisionForScene(decision, ctx.Session.Scene, "draft")
	alertJSON, _ := json.Marshal(alerts)
	decision.AlertJSON = string(alertJSON)
	return decision
}

func deduplicateModelHypotheses(hypotheses []models.PredictionHypothesis) []models.PredictionHypothesis {
	result := make([]models.PredictionHypothesis, 0, len(hypotheses))
	indexByRule := make(map[string]int, len(hypotheses))
	for _, hypothesis := range hypotheses {
		rule, err := JSONToRule(hypothesis.RuleJSON)
		if err != nil {
			continue
		}
		key := ruleExecutionFingerprint(rule)
		if key == "" {
			continue
		}
		if index, exists := indexByRule[key]; exists {
			if preferHypothesis(hypothesis, result[index]) {
				result[index] = hypothesis
			}
			continue
		}
		indexByRule[key] = len(result)
		result = append(result, hypothesis)
	}
	return result
}

func preferHypothesis(candidate, current models.PredictionHypothesis) bool {
	if candidate.NoLookaheadPassed != current.NoLookaheadPassed {
		return candidate.NoLookaheadPassed
	}
	if candidate.OutSampleTradeCount != current.OutSampleTradeCount {
		return candidate.OutSampleTradeCount > current.OutSampleTradeCount
	}
	if candidate.OutSampleAvgReturn != current.OutSampleAvgReturn {
		return candidate.OutSampleAvgReturn > current.OutSampleAvgReturn
	}
	if candidate.TradeCount != current.TradeCount {
		return candidate.TradeCount > current.TradeCount
	}
	return candidate.ID < current.ID
}

func quoteCompatibleWithFeature(quote quoteSnapshot, feature models.StockFeature) bool {
	if quote.price <= 0 {
		return false
	}
	quoteDate := strings.TrimSpace(quote.date)
	if quoteDate == "" && !quote.at.IsZero() {
		quoteDate = quote.at.Format("2006-01-02")
	}
	featureDate, featureErr := time.Parse("2006-01-02", feature.Date)
	quoteDay, quoteErr := time.Parse("2006-01-02", quoteDate)
	if featureErr != nil || quoteErr != nil || quoteDay.Before(featureDate) {
		return false
	}
	return quoteDay.Sub(featureDate) <= 4*24*time.Hour
}

func featureFreshForDecision(feature models.StockFeature, now time.Time) bool {
	featureDate, err := time.Parse("2006-01-02", feature.Date)
	if err != nil {
		return false
	}
	nowDate, err := time.Parse("2006-01-02", now.Format("2006-01-02"))
	if err != nil || nowDate.Before(featureDate) {
		return false
	}
	return nowDate.Sub(featureDate) <= 4*24*time.Hour
}

func stabilizeAction(previousAction, action string, quantityPercent, score float64, emergencyExit bool) (string, float64) {
	previousAction = strings.ToUpper(strings.TrimSpace(previousAction))
	if previousAction == "" || emergencyExit {
		return action, quantityPercent
	}
	if (previousAction == "SELL" || previousAction == "AVOID") && (action == "BUY" || action == "ADD") && score < 82 {
		return "WATCH", 0
	}
	if previousAction == "HOLD" && action == "ADD" && score < 82 {
		return "HOLD", 0
	}
	if (previousAction == "BUY" || previousAction == "ADD") && action == "AVOID" && score >= 45 {
		return "WATCH", 0
	}
	return action, quantityPercent
}

func riskBudgetPositionPercent(stopLossRate float64, sampleWarning bool) float64 {
	if stopLossRate <= 0 {
		stopLossRate = 0.05
	}
	riskBudget := 0.01
	if sampleWarning {
		riskBudget = 0.005
	}
	return clamp(riskBudget/stopLossRate*100, 5, 20)
}

func decisionQualityRating(positiveMatchedCount int, stockSampleCount int, sampleWarning bool, noLookaheadOK bool, dataReady bool) string {
	if !noLookaheadOK || !dataReady {
		return "blocked"
	}
	if positiveMatchedCount == 0 || sampleWarning {
		return "observe"
	}
	if stockSampleCount >= 30 {
		return "reference"
	}
	if stockSampleCount >= MinStockStrategySamples {
		return "observe"
	}
	return "weak"
}
