package backtest

import (
	"encoding/json"
	"go-stock/backend/models"
	"strings"
)

type StrategyEngine struct{}

func NewStrategyEngine() *StrategyEngine {
	return &StrategyEngine{}
}

func (e *StrategyEngine) GenerateSignals(rule Rule, features []models.StockFeature) []Signal {
	signals := make([]Signal, 0)
	for _, f := range features {
		if e.MatchConditions(rule.EntryConditions, f) {
			signals = append(signals, Signal{
				StockCode:  f.StockCode,
				StockName:  f.StockCode,
				Date:       f.Date,
				Price:      f.Close,
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
			Score:      e.ScoreEntry(rule, signal.StockCode, features),
			ReasonJSON: signal.ReasonJSON,
			Source:     "rule",
		})
	}
	return signals
}

func (e *StrategyEngine) MatchConditions(conditions []Condition, f models.StockFeature) bool {
	for _, c := range conditions {
		if !e.MatchCondition(c, f) {
			return false
		}
	}
	return true
}

func (e *StrategyEngine) MatchCondition(c Condition, f models.StockFeature) bool {
	value := e.GetIndicatorValue(c.Indicator, f)
	ref := c.Value
	if strings.TrimSpace(c.Ref) != "" {
		ref = e.GetIndicatorValue(c.Ref, f)
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
	default:
		return false
	}
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

func normalizeIndicator(indicator string) string {
	indicator = strings.TrimSpace(indicator)
	indicator = strings.ReplaceAll(indicator, "-", "_")
	return strings.ToLower(indicator)
}

func ruleConditionsJSON(conditions []Condition) string {
	payload, _ := json.Marshal(Rule{EntryConditions: conditions})
	return string(payload)
}
