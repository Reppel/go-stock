package backtest

import (
	"encoding/json"
	"go-stock/backend/models"
	"math"
	"strings"
)

type StrategyEngine struct {
	viewService *UnifiedFeatureViewService
}

func NewStrategyEngine() *StrategyEngine {
	return &StrategyEngine{viewService: NewUnifiedFeatureViewService()}
}

func (e *StrategyEngine) GenerateSignals(rule Rule, features []models.StockFeature) []Signal {
	return e.GenerateSignalsWithPrevious(rule, features, nil)
}

func (e *StrategyEngine) GenerateSignalsWithPrevious(rule Rule, features []models.StockFeature, previous map[string]models.StockFeature) []Signal {
	signals := make([]Signal, 0)
	indicatorIDs := ruleIndicatorIDs(rule)
	for _, f := range features {
		previousFeature, hasPrevious := previous[f.StockCode]
		var prior *models.StockFeature
		if hasPrevious {
			prior = &previousFeature
		}
		currentView := e.viewService.BuildForIndicators(f.StockCode, f.Date, f, indicatorIDs)
		var previousView *UnifiedFeatureView
		if prior != nil {
			built := e.viewService.BuildForIndicators(prior.StockCode, prior.Date, *prior, indicatorIDs)
			previousView = &built
		}
		if e.MatchConditionsWithViews(rule.EntryConditions, currentView, previousView) {
			score := e.scoreFeature(rule, f, prior)
			signals = append(signals, Signal{
				StockCode:  f.StockCode,
				StockName:  f.StockCode,
				Date:       f.Date,
				Price:      f.Close,
				Score:      score,
				ReasonJSON: RuleToJSON(Rule{EntryConditions: rule.EntryConditions}),
			})
		}
	}
	return signals
}

func (e *StrategyEngine) GenerateQuantSignals(rule Rule, features []models.StockFeature) []QuantSignal {
	raw := e.GenerateSignals(rule, features)
	signals := make([]QuantSignal, 0, len(raw))
	for _, signal := range raw {
		signals = append(signals, QuantSignal{
			StockCode:  signal.StockCode,
			StockName:  signal.StockName,
			Date:       signal.Date,
			Side:       QuantSignalBuy,
			Price:      signal.Price,
			Score:      signal.Score,
			ReasonJSON: signal.ReasonJSON,
			Source:     "rule",
		})
	}
	return signals
}

func (e *StrategyEngine) MatchConditions(conditions []Condition, f models.StockFeature) bool {
	return e.MatchConditionsWithPrevious(conditions, f, nil)
}

func (e *StrategyEngine) MatchConditionsWithPrevious(conditions []Condition, f models.StockFeature, previous *models.StockFeature) bool {
	if len(conditions) == 0 {
		return false
	}
	indicatorIDs := conditionIndicatorIDs(conditions)
	currentView := e.viewService.BuildForIndicators(f.StockCode, f.Date, f, indicatorIDs)
	var previousView *UnifiedFeatureView
	if previous != nil {
		built := e.viewService.BuildForIndicators(previous.StockCode, previous.Date, *previous, indicatorIDs)
		previousView = &built
	}
	return e.MatchConditionsWithViews(conditions, currentView, previousView)
}

func (e *StrategyEngine) MatchConditionsWithViews(conditions []Condition, current UnifiedFeatureView, previous *UnifiedFeatureView) bool {
	if len(conditions) == 0 {
		return false
	}
	for _, c := range conditions {
		if !e.MatchConditionWithViews(c, current, previous) {
			return false
		}
	}
	return true
}

func (e *StrategyEngine) MatchCondition(c Condition, f models.StockFeature) bool {
	return e.MatchConditionWithPrevious(c, f, nil)
}

func (e *StrategyEngine) MatchConditionWithPrevious(c Condition, f models.StockFeature, previous *models.StockFeature) bool {
	indicatorIDs := conditionIndicatorIDs([]Condition{c})
	currentView := e.viewService.BuildForIndicators(f.StockCode, f.Date, f, indicatorIDs)
	var previousView *UnifiedFeatureView
	if previous != nil {
		built := e.viewService.BuildForIndicators(previous.StockCode, previous.Date, *previous, indicatorIDs)
		previousView = &built
	}
	return e.MatchConditionWithViews(c, currentView, previousView)
}

func (e *StrategyEngine) MatchConditionWithViews(c Condition, current UnifiedFeatureView, previous *UnifiedFeatureView) bool {
	indicatorID := canonicalIndicatorID(firstNonBlank(c.IndicatorID, c.Indicator))
	refIndicatorID := canonicalIndicatorID(firstNonBlank(c.RefID, c.Ref))
	value, ok := indicatorValueFromViews(indicatorID, current, previous, c.Lag)
	if !ok {
		return false
	}
	ref := c.Value
	if strings.TrimSpace(firstNonBlank(c.RefID, c.Ref)) != "" {
		refValue, refOK := indicatorValueFromViews(refIndicatorID, current, previous, c.RefLag)
		if !refOK {
			return false
		}
		ref = refValue
	}

	switch strings.TrimSpace(c.Operator) {
	case ">":
		return value > ref
	case ">=":
		return value >= ref
	case "<":
		return value < ref
	case "<=":
		return value <= ref
	case "==":
		return value == ref
	case "!=":
		return value != ref
	case "cross_up", "crosses_above":
		if c.Lag != 0 || previous == nil {
			return false
		}
		previousValue, previousOK := indicatorValueFromViews(indicatorID, current, previous, 1)
		if !previousOK {
			return false
		}
		previousRef := c.Value
		if strings.TrimSpace(firstNonBlank(c.RefID, c.Ref)) != "" {
			var refOK bool
			previousRef, refOK = indicatorValueFromViews(refIndicatorID, current, previous, 1)
			if !refOK {
				return false
			}
		}
		return previousValue <= previousRef && value > ref
	case "cross_down", "crosses_below":
		if c.Lag != 0 || previous == nil {
			return false
		}
		previousValue, previousOK := indicatorValueFromViews(indicatorID, current, previous, 1)
		if !previousOK {
			return false
		}
		previousRef := c.Value
		if strings.TrimSpace(firstNonBlank(c.RefID, c.Ref)) != "" {
			var refOK bool
			previousRef, refOK = indicatorValueFromViews(refIndicatorID, current, previous, 1)
			if !refOK {
				return false
			}
		}
		return previousValue >= previousRef && value < ref
	default:
		return false
	}
}

func (e *StrategyEngine) GetIndicatorValueWithLag(indicator string, f models.StockFeature, previous *models.StockFeature, lag int) (float64, bool) {
	indicatorID := canonicalIndicatorID(indicator)
	currentView := e.viewService.BuildForIndicators(f.StockCode, f.Date, f, []string{indicatorID})
	var previousView *UnifiedFeatureView
	if previous != nil {
		built := e.viewService.BuildForIndicators(previous.StockCode, previous.Date, *previous, []string{indicatorID})
		previousView = &built
	}
	return indicatorValueFromViews(indicatorID, currentView, previousView, lag)
}

func (e *StrategyEngine) MatchAnyConditionWithPrevious(conditions []Condition, f models.StockFeature, previous *models.StockFeature) bool {
	if len(conditions) == 0 {
		return false
	}
	indicatorIDs := conditionIndicatorIDs(conditions)
	currentView := e.viewService.BuildForIndicators(f.StockCode, f.Date, f, indicatorIDs)
	var previousView *UnifiedFeatureView
	if previous != nil {
		built := e.viewService.BuildForIndicators(previous.StockCode, previous.Date, *previous, indicatorIDs)
		previousView = &built
	}
	for _, condition := range conditions {
		if e.MatchConditionWithViews(condition, currentView, previousView) {
			return true
		}
	}
	return false
}

func indicatorValueFromViews(indicatorID string, current UnifiedFeatureView, previous *UnifiedFeatureView, lag int) (float64, bool) {
	switch lag {
	case 0:
		return current.Value(indicatorID)
	case 1:
		if previous == nil {
			return 0, false
		}
		return previous.Value(indicatorID)
	default:
		return 0, false
	}
}

func conditionIndicatorIDs(conditions []Condition) []string {
	seen := make(map[string]struct{})
	for _, condition := range conditions {
		for _, raw := range []string{firstNonBlank(condition.IndicatorID, condition.Indicator), firstNonBlank(condition.RefID, condition.Ref)} {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			seen[canonicalIndicatorID(raw)] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for indicatorID := range seen {
		result = append(result, indicatorID)
	}
	return result
}

func ruleIndicatorIDs(rule Rule) []string {
	return conditionIndicatorIDs(append(append([]Condition{}, rule.EntryConditions...), rule.ExitConditions...))
}

func (e *StrategyEngine) GetIndicatorValue(indicator string, f models.StockFeature) float64 {
	switch normalizeIndicator(indicator) {
	case "ma5":
		return f.MA5
	case "ma10":
		return f.MA10
	case "ma20":
		return f.MA20
	case "ma60":
		return f.MA60
	case "macd":
		return f.MACD
	case "rsi6":
		return f.RSI6
	case "rsi12":
		return f.RSI12
	case "kdj_k", "kdjk":
		return f.KDJ_K
	case "bollupper", "boll_upper":
		return f.BOLLUpper
	case "bollmid", "boll_mid":
		return f.BOLLMid
	case "bolllower", "boll_lower":
		return f.BOLLLower
	case "volumeratio", "volume_ratio":
		return f.VolumeRatio
	case "atr":
		return f.ATR
	case "fundflow5", "fund_flow5":
		return f.FundFlow5
	case "fundflow20", "fund_flow20":
		return f.FundFlow20
	case "changerate5", "change_rate5":
		return f.ChangeRate5
	case "changerate20", "change_rate20":
		return f.ChangeRate20
	case "close":
		return f.Close
	case "open":
		return f.Open
	case "high":
		return f.High
	case "low":
		return f.Low
	case "volume":
		return f.Volume
	case "turnover":
		return f.Turnover
	default:
		return 0
	}
}

func (e *StrategyEngine) ScoreEntry(rule Rule, stockCode string, features []models.StockFeature) float64 {
	score := 0.0
	for _, f := range features {
		if f.StockCode != stockCode {
			continue
		}
		for _, c := range rule.EntryConditions {
			if e.MatchCondition(c, f) {
				score += 1
			}
		}
		break
	}
	return score
}

func (e *StrategyEngine) scoreFeature(rule Rule, feature models.StockFeature, previous *models.StockFeature) float64 {
	score := float64(len(rule.EntryConditions)) * 10
	if feature.ATR > 0 && feature.Close > 0 {
		score += math.Max(0, 10-math.Min(feature.ATR/feature.Close*100, 10))
	}
	if feature.VolumeRatio > 1 {
		score += math.Min((feature.VolumeRatio-1)*5, 10)
	}
	if feature.FundFlow5 > 0 {
		score += 5
	}
	if previous != nil && feature.ChangeRate20 > previous.ChangeRate20 {
		score += 3
	}
	return score
}

func normalizeIndicator(indicator string) string {
	indicator = strings.TrimSpace(indicator)
	indicator = strings.ReplaceAll(indicator, "-", "_")
	return strings.ToLower(indicator)
}

func ruleConditionsJSON(conditions []Condition) string {
	payload, _ := json.Marshal(Rule{EntryConditions: conditions})
	return string(payload)
}
