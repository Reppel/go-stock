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
	quote := ctx.Quote
	holding := ctx.Holding
	currentPrice := f.Close
	if quote.price > 0 {
		currentPrice = quote.price
	}

	stockName := f.StockCode
	if strings.TrimSpace(holding.name) != "" {
		stockName = holding.name
	}
	if strings.TrimSpace(quote.name) != "" {
		stockName = quote.name
	}

	dataAsOf := f.DataAsOf
	if !quote.at.IsZero() {
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
	sampleWarning := false
	noLookaheadOK := true
	bestTargetReturn := 0.01
	bestRuleStopLoss := 0.03
	matchedFacts := make([]matchedStrategyFact, 0, len(ctx.Hypotheses))
	reasons := make([]string, 0, 10)
	risks := make([]string, 0, 10)

	for _, h := range ctx.Hypotheses {
		var rule Rule
		if err := json.Unmarshal([]byte(h.RuleJSON), &rule); err != nil {
			continue
		}
		entryMatched := e.strategy.MatchConditions(rule.EntryConditions, f)
		exitMatched := e.strategy.MatchConditions(rule.ExitConditions, f)
		monitorReady := hypothesisMonitorReady(h)
		if h.TradeCount < 30 {
			sampleWarning = true
		}
		if !h.NoLookaheadPassed {
			noLookaheadOK = false
		}
		if h.AvgReturn < 0 {
			negativeStrategyCount++
		}
		if h.TargetReturn > bestTargetReturn && h.AvgReturn > 0 {
			bestTargetReturn = h.TargetReturn
		}
		if rule.StopLoss > 0 {
			bestRuleStopLoss = rule.StopLoss
		}

		if entryMatched {
			entryMatchedCount++
			score += 6
			if h.AvgReturn > 0 {
				score += math.Min(h.AvgReturn*100, 10)
				positiveMatchedCount++
			} else {
				score -= 6
			}
			if h.WinRate >= 0.5 {
				score += 6
			} else {
				score -= 4
			}
			if h.TradeCount < 10 {
				score -= 4
			}
			reasons = append(reasons, fmt.Sprintf("命中策略「%s」入场条件", h.Name))
		}
		if exitMatched {
			exitMatchedCount++
			score -= 10
			risks = append(risks, fmt.Sprintf("策略「%s」触发退出条件", h.Name))
		}
		if h.AvgReturn < 0 {
			score -= 3
		}
		matchedFacts = append(matchedFacts, matchedStrategyFact{
			ID:           h.ID,
			Name:         h.Name,
			EntryMatched: entryMatched,
			ExitMatched:  exitMatched,
			WinRate:      h.WinRate,
			AvgReturn:    h.AvgReturn,
			MaxDrawdown:  h.MaxDrawdown,
			TradeCount:   h.TradeCount,
			MonitorReady: monitorReady,
		})
	}

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
		risks = append(risks, "特征未复权，ETF分拆或除权可能放大均线误差")
	}
	if f.MA20 > 0 && math.Abs(f.Close/f.MA20-1) > 0.25 {
		risks = append(risks, "价格与MA20偏离过大，请复核复权口径")
	}

	capitalFlow := e.capital.GetStockFlow(f.StockCode, f.Date, f)
	score += capitalFlow.ScoreAdjustment
	reasons = append(reasons, capitalFlow.Reasons...)
	risks = append(risks, capitalFlow.Risks...)

	if sampleWarning {
		risks = append(risks, "回测交易样本少于30笔，仅适合保存观察")
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

	risk := e.risk.EvaluateCurrent(hasPosition, currentPrice, costPrice, bestRuleStopLoss, bestTargetReturn, score, sampleWarning || !noLookaheadOK)
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
	sizing := e.sizer.Size(action, quantityPercent, currentPrice, holdingVolume, holding.marketValue)
	if sizing.AccountWarning != "" {
		risks = append(risks, sizing.AccountWarning)
	}
	positionAdvice := buildPositionAdvice(action, quantityPercent, hasPosition)
	if sizing.SuggestedQuantity > 0 {
		positionAdvice = fmt.Sprintf("%s，约%d股/份，参考金额%.2f", positionAdvice, sizing.SuggestedQuantity, sizing.SuggestedAmount)
	}

	quality := decisionQualityRating(ctx.Hypotheses, sampleWarning, noLookaheadOK)
	confidence := decisionConfidence(score, sampleWarning || !noLookaheadOK)

	matchedJSON, _ := json.Marshal(matchedFacts)
	reasonsJSON, _ := json.Marshal(uniqueStrings(reasons))
	risksJSON, _ := json.Marshal(uniqueStrings(risks))
	sizingJSON, _ := json.Marshal(sizing)
	capitalJSON, _ := json.Marshal(capitalFlow)

	decision := models.PredictionDecision{
		SessionID:             ctx.Session.ID,
		StockCode:             f.StockCode,
		StockName:             stockName,
		DecisionDate:          f.Date,
		Action:                action,
		ActionText:            actionText(action),
		PositionAdvice:        positionAdvice,
		QuantityPercent:       quantityPercent,
		SuggestedQuantity:     sizing.SuggestedQuantity,
		SuggestedAmount:       sizing.SuggestedAmount,
		Confidence:            confidence,
		QualityRating:         quality,
		RiskLevel:             risk.RiskLevel,
		Score:                 score,
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
		FeatureVersion:        f.FeatureVersion,
		DataAsOf:              dataAsOf,
		Status:                "draft",
	}
	alerts := e.alerts.EvaluateDecision(decision)
	alertJSON, _ := json.Marshal(alerts)
	decision.AlertJSON = string(alertJSON)
	return decision
}

func decisionQualityRating(hypotheses []models.PredictionHypothesis, sampleWarning bool, noLookaheadOK bool) string {
	if !noLookaheadOK {
		return "blocked"
	}
	if len(hypotheses) == 0 || sampleWarning {
		return "observe"
	}
	totalTrades := 0
	positive := 0
	for _, h := range hypotheses {
		totalTrades += h.TradeCount
		if h.AvgReturn > 0 && h.WinRate >= 0.5 && h.MaxDrawdown <= 0.20 {
			positive++
		}
	}
	if totalTrades >= 60 && positive > 0 {
		return "reference"
	}
	if totalTrades >= 30 {
		return "observe"
	}
	return "weak"
}
