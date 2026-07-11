package backtest

import (
	"encoding/json"
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"math"
	"time"
)

const PredictionRuleSchemaV1 = "prediction-rule/v1"

type IndicatorDefinition struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Source      string `json:"source"`
	Unit        string `json:"unit"`
	Description string `json:"description"`
}

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type RuleEnvelope struct {
	SchemaVersion string  `json:"schemaVersion"`
	Name          string  `json:"name"`
	Scene         string  `json:"scene"`
	TimeHorizon   int     `json:"timeHorizon"`
	TargetReturn  float64 `json:"targetReturn"`
	Risk          struct {
		StopLoss    float64 `json:"stopLoss"`
		StopGain    float64 `json:"stopGain"`
		MaxHoldDays int     `json:"maxHoldDays"`
		MaxHoldings int     `json:"maxHoldings"`
	} `json:"risk"`
	Entry struct {
		All []Condition `json:"all"`
	} `json:"entry"`
	Exit struct {
		Any []Condition `json:"any"`
	} `json:"exit"`
}

func GetIndicatorRegistry() []IndicatorDefinition {
	return []IndicatorDefinition{
		{Name: "Open", Label: "开盘价", Source: "stock_feature", Unit: "price"},
		{Name: "High", Label: "最高价", Source: "stock_feature", Unit: "price"},
		{Name: "Low", Label: "最低价", Source: "stock_feature", Unit: "price"},
		{Name: "Close", Label: "收盘价", Source: "stock_feature", Unit: "price"},
		{Name: "Volume", Label: "成交量", Source: "stock_feature", Unit: "volume"},
		{Name: "MA5", Label: "5日均线", Source: "stock_feature", Unit: "price"},
		{Name: "MA10", Label: "10日均线", Source: "stock_feature", Unit: "price"},
		{Name: "MA20", Label: "20日均线", Source: "stock_feature", Unit: "price"},
		{Name: "MA60", Label: "60日均线", Source: "stock_feature", Unit: "price"},
		{Name: "MACD", Label: "MACD", Source: "stock_feature", Unit: "factor"},
		{Name: "RSI6", Label: "RSI6", Source: "stock_feature", Unit: "factor"},
		{Name: "RSI12", Label: "RSI12", Source: "stock_feature", Unit: "factor"},
		{Name: "KDJ_K", Label: "KDJ K", Source: "stock_feature", Unit: "factor"},
		{Name: "BOLLUpper", Label: "布林上轨", Source: "stock_feature", Unit: "price"},
		{Name: "BOLLMid", Label: "布林中轨", Source: "stock_feature", Unit: "price"},
		{Name: "BOLLLower", Label: "布林下轨", Source: "stock_feature", Unit: "price"},
		{Name: "VolumeRatio", Label: "量比", Source: "stock_feature", Unit: "ratio"},
		{Name: "ATR", Label: "ATR", Source: "stock_feature", Unit: "price"},
		{Name: "ChangeRate5", Label: "5日涨跌幅", Source: "stock_feature", Unit: "ratio"},
		{Name: "ChangeRate20", Label: "20日涨跌幅", Source: "stock_feature", Unit: "ratio"},
	}
}

func ValidatePredictionRule(ruleJSON string) (*Rule, []ValidationError) {
	var errs []ValidationError
	if ruleJSON == "" {
		return nil, []ValidationError{{Field: "rule", Message: "规则为空"}}
	}

	var envelope RuleEnvelope
	if err := json.Unmarshal([]byte(ruleJSON), &envelope); err == nil && envelope.SchemaVersion != "" {
		if envelope.SchemaVersion != PredictionRuleSchemaV1 {
			errs = append(errs, ValidationError{Field: "schemaVersion", Message: "不支持的规则版本"})
		}
		rule := Rule{
			EntryConditions: envelope.Entry.All,
			ExitConditions:  envelope.Exit.Any,
			StopLoss:        envelope.Risk.StopLoss,
			StopGain:        envelope.Risk.StopGain,
			MaxHoldDays:     envelope.Risk.MaxHoldDays,
			MaxHoldings:     envelope.Risk.MaxHoldings,
		}
		errs = append(errs, validateRule(rule)...)
		return &rule, errs
	}

	var rule Rule
	if err := json.Unmarshal([]byte(ruleJSON), &rule); err != nil {
		return nil, []ValidationError{{Field: "rule", Message: fmt.Sprintf("JSON 解析失败：%v", err)}}
	}
	errs = append(errs, validateRule(rule)...)
	return &rule, errs
}

func ValidateHypothesisRule(rule Rule) []ValidationError {
	return validateRule(rule)
}

func CompileRule(rule Rule) (*Rule, error) {
	if errs := validateRule(rule); len(errs) > 0 {
		b, _ := json.Marshal(errs)
		return nil, fmt.Errorf("规则校验失败：%s", string(b))
	}
	return &rule, nil
}

func NormalizeHypothesis(h Hypothesis) Hypothesis {
	if h.TimeHorizon <= 0 {
		h.TimeHorizon = 5
	}
	if h.TimeHorizon > 30 {
		h.TimeHorizon = 30
	}
	if h.TargetReturn <= 0 {
		h.TargetReturn = 0.05
	}
	if h.TargetReturn < 0.01 {
		h.TargetReturn = 0.01
	}
	if h.TargetReturn > 0.20 {
		h.TargetReturn = 0.20
	}
	if h.Rule.StopLoss <= 0 {
		h.Rule.StopLoss = 0.07
	}
	if h.Rule.StopGain <= 0 {
		h.Rule.StopGain = 0.12
	}
	if h.Rule.MaxHoldDays <= 0 {
		h.Rule.MaxHoldDays = h.TimeHorizon
	}
	if h.Rule.MaxHoldings <= 0 {
		h.Rule.MaxHoldings = 5
	}
	return h
}

func RecordGenerationAudit(sessionID uint, source string, raw string, validationErrors []ValidationError) error {
	if db.Dao == nil {
		return nil
	}
	errJSON := ""
	if len(validationErrors) > 0 {
		b, _ := json.Marshal(validationErrors)
		errJSON = string(b)
	}
	return db.Dao.Create(&models.PredictionGenerationAudit{
		SessionID:     sessionID,
		Source:        source,
		RawOutput:     raw,
		ErrorJSON:     errJSON,
		SchemaVersion: PredictionRuleSchemaV1,
		CreatedAt:     time.Now(),
	}).Error
}

func validateRule(rule Rule) []ValidationError {
	var errs []ValidationError
	allowed := map[string]bool{}
	for _, d := range GetIndicatorRegistry() {
		allowed[d.Name] = true
	}
	allowedOps := map[string]bool{
		">": true, ">=": true, "<": true, "<=": true, "==": true, "!=": true,
		"cross_up": true, "cross_down": true, "crosses_above": true, "crosses_below": true,
	}

	if len(rule.EntryConditions) == 0 {
		errs = append(errs, ValidationError{Field: "entryConditions", Message: "至少需要一个入场条件"})
	}
	for i, c := range append(rule.EntryConditions, rule.ExitConditions...) {
		prefix := fmt.Sprintf("conditions[%d]", i)
		if !allowed[c.Indicator] {
			errs = append(errs, ValidationError{Field: prefix + ".indicator", Message: "指标不在白名单内：" + c.Indicator})
		}
		if c.Ref != "" && !allowed[c.Ref] {
			errs = append(errs, ValidationError{Field: prefix + ".ref", Message: "引用指标不在白名单内：" + c.Ref})
		}
		if !allowedOps[c.Operator] {
			errs = append(errs, ValidationError{Field: prefix + ".operator", Message: "操作符不支持：" + c.Operator})
		}
		if c.Ref == "" && (math.IsNaN(c.Value) || math.IsInf(c.Value, 0)) {
			errs = append(errs, ValidationError{Field: prefix + ".value", Message: "数值非法"})
		}
	}
	if rule.StopLoss < 0.02 || rule.StopLoss > 0.12 {
		errs = append(errs, ValidationError{Field: "stopLoss", Message: "止损范围应在 0.02 - 0.12"})
	}
	if rule.StopGain < 0.03 || rule.StopGain > 0.25 {
		errs = append(errs, ValidationError{Field: "stopGain", Message: "止盈范围应在 0.03 - 0.25"})
	}
	if rule.MaxHoldDays < 0 || rule.MaxHoldDays > 30 {
		errs = append(errs, ValidationError{Field: "maxHoldDays", Message: "持有周期应在 1 - 30 个交易日"})
	}
	if rule.MaxHoldings < 0 || rule.MaxHoldings > 20 {
		errs = append(errs, ValidationError{Field: "maxHoldings", Message: "最大持仓数应在 1 - 20"})
	}
	return errs
}
