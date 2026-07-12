package backtest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	PredictionRuleSchemaV1      = "prediction-rule/v1"
	PredictionRuleSchemaV2      = "prediction-rule/v2"
	CurrentPredictionRuleSchema = PredictionRuleSchemaV2
	CurrentIndicatorRegistry    = "indicator-registry/v2"
)

type IndicatorDefinition struct {
	IndicatorID      string   `json:"indicatorId"`
	Name             string   `json:"name"`
	Label            string   `json:"label"`
	Source           string   `json:"source"`
	Table            string   `json:"table"`
	Field            string   `json:"field"`
	Type             string   `json:"type"`
	Unit             string   `json:"unit"`
	DataType         string   `json:"dataType"`
	Description      string   `json:"description"`
	AllowedOperators []string `json:"allowedOperators"`
	AvailableAt      string   `json:"availableAt"`
	Coverage         float64  `json:"coverage"`
	StartDate        string   `json:"startDate"`
	LookbackDays     int      `json:"lookbackDays"`
	MissingPolicy    string   `json:"missingPolicy"`
	Status           string   `json:"status"`
	FeatureVersion   string   `json:"featureVersion"`
	RegistryVersion  string   `json:"registryVersion"`
}

type IndicatorQuery struct {
	Source          string `json:"source"`
	Type            string `json:"type"`
	Status          string `json:"status"`
	AvailableBefore string `json:"availableBefore"`
	FeatureVersion  string `json:"featureVersion"`
}

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type RuleOperandV2 struct {
	IndicatorID string   `json:"indicatorId,omitempty"`
	Source      string   `json:"source,omitempty"`
	Value       *float64 `json:"value,omitempty"`
	Lag         int      `json:"lag,omitempty"`
}

type ConditionV2 struct {
	Left          RuleOperandV2  `json:"left"`
	Operator      string         `json:"operator"`
	Right         RuleOperandV2  `json:"right"`
	PreviousLeft  *RuleOperandV2 `json:"previousLeft,omitempty"`
	PreviousRight *RuleOperandV2 `json:"previousRight,omitempty"`
	Lag           int            `json:"lag,omitempty"`
}

type RuleEnvelope struct {
	SchemaVersion            string         `json:"schemaVersion"`
	RegistryVersion          string         `json:"registryVersion"`
	IndicatorRegistryVersion string         `json:"indicatorRegistryVersion,omitempty"`
	FeatureVersion           string         `json:"featureVersion"`
	EngineVersion            string         `json:"engineVersion"`
	Name                     string         `json:"name"`
	Scene                    string         `json:"scene"`
	TimeHorizon              int            `json:"timeHorizon"`
	TargetReturn             float64        `json:"targetReturn"`
	ResearchIdeaIDs          []uint         `json:"researchIdeaIds,omitempty"`
	ResearchIdeas            []ResearchIdea `json:"researchIdeas,omitempty"`
	Fingerprint              string         `json:"fingerprint"`
	Risk                     struct {
		StopLoss    float64 `json:"stopLoss"`
		StopGain    float64 `json:"stopGain"`
		MaxHoldDays int     `json:"maxHoldDays"`
		MaxHoldings int     `json:"maxHoldings"`
	} `json:"risk"`
	Entry struct {
		All []ConditionV2 `json:"all"`
	} `json:"entry"`
	Exit struct {
		Any []ConditionV2 `json:"any"`
	} `json:"exit"`
}

type GenerationAuditPayload struct {
	SessionID        uint
	Source           string
	Prompt           string
	RawOutput        string
	NormalizedDSL    string
	ToolCallsJSON    string
	AIConfigID       int
	ModelName        string
	Temperature      float64
	GenerationError  string
	ValidationErrors []ValidationError
}

func GetIndicatorRegistry() []IndicatorDefinition {
	priceOps := []string{">", ">=", "<", "<=", "==", "!=", "cross_up", "cross_down", "crosses_above", "crosses_below"}
	valueOps := []string{">", ">=", "<", "<=", "==", "!="}
	crossOps := []string{">", ">=", "<", "<=", "==", "!=", "cross_up", "cross_down", "crosses_above", "crosses_below"}
	defs := []IndicatorDefinition{
		{IndicatorID: "stock_feature.Open", Name: "Open", Label: "open", Source: "feature", Table: "stock_feature", Field: "open", Type: "price", Unit: "price", AllowedOperators: priceOps, AvailableAt: "T_open", LookbackDays: 1},
		{IndicatorID: "stock_feature.High", Name: "High", Label: "high", Source: "feature", Table: "stock_feature", Field: "high", Type: "price", Unit: "price", AllowedOperators: priceOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "stock_feature.Low", Name: "Low", Label: "low", Source: "feature", Table: "stock_feature", Field: "low", Type: "price", Unit: "price", AllowedOperators: priceOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "stock_feature.Close", Name: "Close", Label: "close", Source: "feature", Table: "stock_feature", Field: "close", Type: "price", Unit: "price", AllowedOperators: priceOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "stock_feature.Volume", Name: "Volume", Label: "volume", Source: "feature", Table: "stock_feature", Field: "volume", Type: "volume", Unit: "volume", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "stock_feature.Turnover", Name: "Turnover", Label: "turnover", Source: "feature", Table: "stock_feature", Field: "turnover", Type: "amount", Unit: "amount", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "stock_feature.MA5", Name: "MA5", Label: "MA5", Source: "feature", Table: "stock_feature", Field: "ma5", Type: "price", Unit: "price", AllowedOperators: priceOps, AvailableAt: "T_close", LookbackDays: 5},
		{IndicatorID: "stock_feature.MA10", Name: "MA10", Label: "MA10", Source: "feature", Table: "stock_feature", Field: "ma10", Type: "price", Unit: "price", AllowedOperators: priceOps, AvailableAt: "T_close", LookbackDays: 10},
		{IndicatorID: "stock_feature.MA20", Name: "MA20", Label: "MA20", Source: "feature", Table: "stock_feature", Field: "ma20", Type: "price", Unit: "price", AllowedOperators: priceOps, AvailableAt: "T_close", LookbackDays: 20},
		{IndicatorID: "stock_feature.MA60", Name: "MA60", Label: "MA60", Source: "feature", Table: "stock_feature", Field: "ma60", Type: "price", Unit: "price", AllowedOperators: priceOps, AvailableAt: "T_close", LookbackDays: 60},
		{IndicatorID: "stock_feature.MACD", Name: "MACD", Label: "MACD", Source: "feature", Table: "stock_feature", Field: "macd", Type: "momentum", Unit: "macd", AllowedOperators: crossOps, AvailableAt: "T_close", LookbackDays: 26},
		{IndicatorID: "stock_feature.RSI6", Name: "RSI6", Label: "RSI6", Source: "feature", Table: "stock_feature", Field: "rsi6", Type: "oscillator", Unit: "oscillator", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 6},
		{IndicatorID: "stock_feature.RSI12", Name: "RSI12", Label: "RSI12", Source: "feature", Table: "stock_feature", Field: "rsi12", Type: "oscillator", Unit: "oscillator", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 12},
		{IndicatorID: "stock_feature.KDJ_K", Name: "KDJ_K", Label: "KDJ K", Source: "feature", Table: "stock_feature", Field: "kdj_k", Type: "oscillator", Unit: "oscillator", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 9},
		{IndicatorID: "stock_feature.BOLLUpper", Name: "BOLLUpper", Label: "BOLL upper", Source: "feature", Table: "stock_feature", Field: "boll_upper", Type: "price", Unit: "price", AllowedOperators: priceOps, AvailableAt: "T_close", LookbackDays: 20},
		{IndicatorID: "stock_feature.BOLLMid", Name: "BOLLMid", Label: "BOLL mid", Source: "feature", Table: "stock_feature", Field: "boll_mid", Type: "price", Unit: "price", AllowedOperators: priceOps, AvailableAt: "T_close", LookbackDays: 20},
		{IndicatorID: "stock_feature.BOLLLower", Name: "BOLLLower", Label: "BOLL lower", Source: "feature", Table: "stock_feature", Field: "boll_lower", Type: "price", Unit: "price", AllowedOperators: priceOps, AvailableAt: "T_close", LookbackDays: 20},
		{IndicatorID: "stock_feature.VolumeRatio", Name: "VolumeRatio", Label: "volume ratio", Source: "feature", Table: "stock_feature", Field: "volume_ratio", Type: "volume", Unit: "ratio", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 5},
		{IndicatorID: "stock_feature.ATR", Name: "ATR", Label: "ATR", Source: "feature", Table: "stock_feature", Field: "atr", Type: "volatility", Unit: "price", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 14},
		{IndicatorID: "stock_feature.FundFlow5", Name: "FundFlow5", Label: "5-day fund flow", Source: "feature", Table: "stock_feature", Field: "fund_flow5", Type: "money_flow", Unit: "amount", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 5},
		{IndicatorID: "stock_feature.FundFlow20", Name: "FundFlow20", Label: "20-day fund flow", Source: "feature", Table: "stock_feature", Field: "fund_flow20", Type: "money_flow", Unit: "amount", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 20},
		{IndicatorID: "stock_feature.ChangeRate5", Name: "ChangeRate5", Label: "5-day return", Source: "feature", Table: "stock_feature", Field: "change_rate5", Type: "return", Unit: "return", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 5},
		{IndicatorID: "stock_feature.ChangeRate20", Name: "ChangeRate20", Label: "20-day return", Source: "feature", Table: "stock_feature", Field: "change_rate20", Type: "return", Unit: "return", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 20},
		{IndicatorID: "money_flow.MainNetInflow1", Name: "MainNetInflow1", Label: "main net inflow 1d", Source: "money_flow", Table: "stock_money_flow_daily", Field: "main_net_inflow1", Type: "money_flow", Unit: "amount", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "money_flow.MainNetInflow5", Name: "MainNetInflow5", Label: "main net inflow 5d", Source: "money_flow", Table: "stock_money_flow_daily", Field: "main_net_inflow5", Type: "money_flow", Unit: "amount", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 5},
		{IndicatorID: "market.SentimentScore", Name: "MarketSentimentScore", Label: "market sentiment", Source: "market", Table: "market_factor_daily", Field: "sentiment_score", Type: "market", Unit: "score", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "market.UpRatio", Name: "MarketUpRatio", Label: "market advance ratio", Source: "market", Table: "market_factor_daily", Field: "up_count", Type: "breadth", Unit: "ratio", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "market.AdvanceDecline", Name: "MarketAdvanceDecline", Label: "advance decline spread", Source: "market", Table: "market_factor_daily", Field: "up_count", Type: "breadth", Unit: "count", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "sector.NetInflow", Name: "SectorNetInflow", Label: "sector net inflow", Source: "sector", Table: "sector_flow_daily", Field: "net_inflow", Type: "money_flow", Unit: "amount", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "event.ChangeEventCount", Name: "ChangeEventCount", Label: "change event count", Source: "event", Table: "stock_event_daily", Field: "change_event_count", Type: "event", Unit: "count", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "event.HasLargeBuy", Name: "HasLargeBuy", Label: "large buy event", Source: "event", Table: "stock_event_daily", Field: "has_large_buy", Type: "event", Unit: "boolean", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "event.HasRapidRise", Name: "HasRapidRise", Label: "rapid rise event", Source: "event", Table: "stock_event_daily", Field: "has_rapid_rise", Type: "event", Unit: "boolean", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "event.HasLimitUp", Name: "HasLimitUp", Label: "limit up event", Source: "event", Table: "stock_event_daily", Field: "has_limit_up", Type: "event", Unit: "boolean", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "limitup.KeepTimes", Name: "LimitUpKeepTimes", Label: "limit-up ladder height", Source: "limitup", Table: "uplimit_stock_daily", Field: "keep_times", Type: "sentiment", Unit: "count", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "limitup.SealRatioClose", Name: "SealRatioClose", Label: "closing seal ratio", Source: "limitup", Table: "uplimit_stock_daily", Field: "seal_ratio_close", Type: "liquidity", Unit: "ratio", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "limitup.ExplodedCount", Name: "ExplodedCount", Label: "failed limit-up count", Source: "limitup", Table: "uplimit_stock_daily", Field: "explode_count", Type: "risk", Unit: "count", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "limitup.PlateHeat", Name: "PlateHeat", Label: "limit-up plate heat", Source: "limitup", Table: "uplimit_stock_daily", Field: "plate_heat", Type: "sentiment", Unit: "score", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "screening.PatternCount", Name: "PatternCount", Label: "local pattern count", Source: "screening", Table: "stock_screening_fact_daily", Field: "pattern_count", Type: "pattern", Unit: "count", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 1},
		{IndicatorID: "screening.MACDGoldenCross", Name: "ScreeningMACDGoldenCross", Label: "local MACD golden cross", Source: "screening", Table: "stock_screening_fact_daily", Field: "macd_golden_cross", Type: "pattern", Unit: "boolean", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 2},
		{IndicatorID: "screening.MABullish", Name: "ScreeningMABullish", Label: "local bullish MA", Source: "screening", Table: "stock_screening_fact_daily", Field: "ma_bullish", Type: "pattern", Unit: "boolean", AllowedOperators: valueOps, AvailableAt: "T_close", LookbackDays: 60},
		{IndicatorID: "recommendation.ModelCount", Name: "RecommendationModelCount", Label: "independent recommendation model count", Source: "recommendation", Table: "model_recommendation_event", Field: "model_name", Type: "model_consensus", Unit: "count", AllowedOperators: valueOps, AvailableAt: "event_time", LookbackDays: 30},
		{IndicatorID: "recommendation.RecommendationCount", Name: "RecommendationCount", Label: "recommendation count", Source: "recommendation", Table: "model_recommendation_event", Field: "id", Type: "model_consensus", Unit: "count", AllowedOperators: valueOps, AvailableAt: "event_time", LookbackDays: 30},
	}
	for i := range defs {
		defs[i].DataType = "float"
		defs[i].MissingPolicy = "reject"
		defs[i].Status = "active"
		if defs[i].Source != "feature" {
			defs[i].Status = "candidate"
		}
		defs[i].FeatureVersion = CurrentFeatureVersion
		defs[i].RegistryVersion = CurrentIndicatorRegistry
	}
	hydrateIndicatorRegistryMetadata(defs)
	return defs
}

type indicatorRegistryMetadata struct {
	Coverage  float64
	StartDate string
}

var indicatorMetadataCache = struct {
	sync.Mutex
	loadedAt time.Time
	values   map[string]indicatorRegistryMetadata
}{values: map[string]indicatorRegistryMetadata{}}

func hydrateIndicatorRegistryMetadata(defs []IndicatorDefinition) {
	if db.Dao == nil {
		return
	}
	indicatorMetadataCache.Lock()
	defer indicatorMetadataCache.Unlock()
	if time.Since(indicatorMetadataCache.loadedAt) > 5*time.Minute {
		values := make(map[string]indicatorRegistryMetadata, len(defs))
		for _, def := range defs {
			dateField := "trade_date"
			if def.Table == "stock_feature" {
				dateField = "date"
			}
			var startDate string
			var coverage float64
			query := db.Dao.Table(def.Table)
			if def.Table == "stock_feature" {
				query = query.Where("feature_version = ? AND adjusted = ?", CurrentFeatureVersion, true)
			}
			coverageSQL := fmt.Sprintf("COALESCE(AVG(CASE WHEN %s IS NOT NULL THEN 1.0 ELSE 0.0 END), 0)", def.Field)
			if def.Table == "stock_screening_fact_daily" {
				coverageSQL = "COALESCE(AVG(CASE WHEN calculated = 1 THEN coverage ELSE 0.0 END), 0)"
			}
			selectSQL := fmt.Sprintf("COALESCE(MIN(%s), ''), %s", dateField, coverageSQL)
			if err := query.Select(selectSQL).Row().Scan(&startDate, &coverage); err == nil {
				values[def.IndicatorID] = indicatorRegistryMetadata{Coverage: coverage, StartDate: startDate}
			}
		}
		indicatorMetadataCache.values = values
		indicatorMetadataCache.loadedAt = time.Now()
	}
	for index := range defs {
		metadata := indicatorMetadataCache.values[defs[index].IndicatorID]
		defs[index].Coverage = metadata.Coverage
		defs[index].StartDate = metadata.StartDate
	}
}

func QueryIndicatorRegistry(query IndicatorQuery) []IndicatorDefinition {
	defs := GetIndicatorRegistry()
	result := make([]IndicatorDefinition, 0, len(defs))
	for _, def := range defs {
		if query.Source != "" && def.Source != query.Source {
			continue
		}
		if query.Type != "" && def.Type != query.Type {
			continue
		}
		if query.Status != "" && def.Status != query.Status {
			continue
		}
		if query.FeatureVersion != "" && def.FeatureVersion != query.FeatureVersion {
			continue
		}
		if query.AvailableBefore != "" && def.StartDate > query.AvailableBefore {
			continue
		}
		result = append(result, def)
	}
	return result
}

func ValidatePredictionRule(ruleJSON string) (*Rule, []ValidationError) {
	rule, _, errs := ParseRuleEnvelope(ruleJSON)
	return rule, errs
}

func ParseRuleEnvelope(ruleJSON string) (*Rule, *RuleEnvelope, []ValidationError) {
	var errs []ValidationError
	if strings.TrimSpace(ruleJSON) == "" {
		return nil, nil, []ValidationError{{Field: "rule", Message: "rule is empty"}}
	}
	var probe struct {
		SchemaVersion string `json:"schemaVersion"`
	}
	if err := json.Unmarshal([]byte(ruleJSON), &probe); err == nil && probe.SchemaVersion != "" {
		if probe.SchemaVersion == PredictionRuleSchemaV1 {
			var legacy struct {
				SchemaVersion string `json:"schemaVersion"`
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
			if err := json.Unmarshal([]byte(ruleJSON), &legacy); err != nil {
				return nil, nil, []ValidationError{{Field: "rule", Message: fmt.Sprintf("decode v1 envelope failed: %v", err)}}
			}
			rule := Rule{
				EntryConditions: legacy.Entry.All,
				ExitConditions:  legacy.Exit.Any,
				StopLoss:        legacy.Risk.StopLoss,
				StopGain:        legacy.Risk.StopGain,
				MaxHoldDays:     legacy.Risk.MaxHoldDays,
				MaxHoldings:     legacy.Risk.MaxHoldings,
			}
			errs = append(errs, validateRule(rule)...)
			return &rule, nil, errs
		}
		var envelope RuleEnvelope
		if err := json.Unmarshal([]byte(ruleJSON), &envelope); err != nil {
			return nil, nil, []ValidationError{{Field: "rule", Message: fmt.Sprintf("decode envelope failed: %v", err)}}
		}
		if envelope.SchemaVersion != PredictionRuleSchemaV1 && envelope.SchemaVersion != PredictionRuleSchemaV2 {
			errs = append(errs, ValidationError{Field: "schemaVersion", Message: "unsupported rule schema: " + envelope.SchemaVersion})
		}
		if envelope.SchemaVersion == PredictionRuleSchemaV2 {
			registryVersion := firstNonBlank(envelope.RegistryVersion, envelope.IndicatorRegistryVersion)
			if registryVersion != "" && registryVersion != CurrentIndicatorRegistry {
				errs = append(errs, ValidationError{Field: "registryVersion", Message: "unsupported indicator registry: " + registryVersion})
			}
			errs = append(errs, validateExplicitCrossV2(append(append([]ConditionV2{}, envelope.Entry.All...), envelope.Exit.Any...))...)
		}
		rule := Rule{
			EntryConditions: conditionsV2ToLegacy(envelope.Entry.All),
			ExitConditions:  conditionsV2ToLegacy(envelope.Exit.Any),
			StopLoss:        envelope.Risk.StopLoss,
			StopGain:        envelope.Risk.StopGain,
			MaxHoldDays:     envelope.Risk.MaxHoldDays,
			MaxHoldings:     envelope.Risk.MaxHoldings,
		}
		errs = append(errs, validateRule(rule)...)
		return &rule, &envelope, errs
	}
	var rule Rule
	if err := json.Unmarshal([]byte(ruleJSON), &rule); err != nil {
		return nil, nil, []ValidationError{{Field: "rule", Message: fmt.Sprintf("decode rule failed: %v", err)}}
	}
	errs = append(errs, validateRule(rule)...)
	return &rule, nil, errs
}

func validateExplicitCrossV2(conditions []ConditionV2) []ValidationError {
	var errs []ValidationError
	for index, condition := range conditions {
		if !isCrossOperator(condition.Operator) {
			continue
		}
		if condition.PreviousLeft == nil || condition.PreviousRight == nil ||
			condition.PreviousLeft.Lag != 1 || condition.PreviousRight.Lag != 1 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("conditions[%d].previous", index),
				Message: "v2 cross condition requires explicit previousLeft/previousRight with lag=1",
			})
		}
	}
	return errs
}

func ValidateHypothesisRule(rule Rule) []ValidationError {
	return validateRule(rule)
}

func CompileRule(rule Rule) (*Rule, error) {
	rule = expandRuleCrossConditions(rule)
	if errs := validateRule(rule); len(errs) > 0 {
		b, _ := json.Marshal(errs)
		return nil, fmt.Errorf("rule validation failed: %s", string(b))
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
		h.Rule.StopGain = h.TargetReturn
	}
	// 回测止盈与实时决策目标使用同一参数，避免训练/执行漂移。
	h.TargetReturn = h.Rule.StopGain
	if h.Rule.MaxHoldDays <= 0 {
		h.Rule.MaxHoldDays = h.TimeHorizon
	}
	if h.Rule.MaxHoldings <= 0 {
		h.Rule.MaxHoldings = 5
	}
	h.Rule = expandRuleCrossConditions(h.Rule)
	return h
}

func BuildRuleEnvelope(h Hypothesis, researchIdeaIDs []uint) RuleEnvelope {
	h = NormalizeHypothesis(h)
	envelope := RuleEnvelope{
		SchemaVersion:   CurrentPredictionRuleSchema,
		RegistryVersion: CurrentIndicatorRegistry,
		FeatureVersion:  CurrentFeatureVersion,
		EngineVersion:   CurrentEngineVersion,
		Name:            h.Name,
		Scene:           h.Scene,
		TimeHorizon:     h.TimeHorizon,
		TargetReturn:    h.TargetReturn,
		ResearchIdeaIDs: researchIdeaIDs,
	}
	envelope.Risk.StopLoss = h.Rule.StopLoss
	envelope.Risk.StopGain = h.Rule.StopGain
	envelope.Risk.MaxHoldDays = h.Rule.MaxHoldDays
	envelope.Risk.MaxHoldings = h.Rule.MaxHoldings
	envelope.Entry.All = legacyConditionsToV2(h.Rule.EntryConditions)
	envelope.Exit.Any = legacyConditionsToV2(h.Rule.ExitConditions)
	envelope.Fingerprint = ruleFingerprint(envelope)
	return envelope
}

func RuleEnvelopeJSON(h Hypothesis, researchIdeaIDs []uint) (string, RuleEnvelope) {
	envelope := BuildRuleEnvelope(h, researchIdeaIDs)
	payload, _ := json.Marshal(envelope)
	return string(payload), envelope
}

func RuleToJSON(rule Rule) string {
	h := Hypothesis{Rule: rule, TimeHorizon: rule.MaxHoldDays, TargetReturn: 0.05}
	payload, _ := json.Marshal(BuildRuleEnvelope(h, nil))
	return string(payload)
}

func JSONToRule(s string) (Rule, error) {
	rule, _, errs := ParseRuleEnvelope(s)
	if len(errs) > 0 {
		b, _ := json.Marshal(errs)
		return Rule{}, fmt.Errorf("rule validation failed: %s", string(b))
	}
	if rule == nil {
		return Rule{}, fmt.Errorf("rule is empty")
	}
	compiled, err := CompileRule(*rule)
	if err != nil {
		return Rule{}, err
	}
	return *compiled, nil
}

func RecordGenerationAudit(sessionID uint, source string, raw string, validationErrors []ValidationError) error {
	return RecordGenerationAuditV2(GenerationAuditPayload{
		SessionID:        sessionID,
		Source:           source,
		RawOutput:        raw,
		ValidationErrors: validationErrors,
	})
}

func RecordGenerationAuditV2(payload GenerationAuditPayload) error {
	if db.Dao == nil {
		return nil
	}
	errJSON := ""
	if len(payload.ValidationErrors) > 0 || strings.TrimSpace(payload.GenerationError) != "" {
		b, _ := json.Marshal(map[string]any{
			"generationError":  payload.GenerationError,
			"validationErrors": payload.ValidationErrors,
		})
		errJSON = string(b)
	}
	return db.Dao.Create(&models.PredictionGenerationAudit{
		SessionID:       payload.SessionID,
		Source:          payload.Source,
		Prompt:          payload.Prompt,
		RawOutput:       payload.RawOutput,
		NormalizedDSL:   payload.NormalizedDSL,
		ToolCallsJSON:   payload.ToolCallsJSON,
		AIConfigID:      payload.AIConfigID,
		ModelName:       payload.ModelName,
		Temperature:     payload.Temperature,
		ErrorJSON:       errJSON,
		SchemaVersion:   CurrentPredictionRuleSchema,
		RegistryVersion: CurrentIndicatorRegistry,
		EngineVersion:   CurrentEngineVersion,
		CreatedAt:       time.Now(),
	}).Error
}

func validateRule(rule Rule) []ValidationError {
	var errs []ValidationError
	definitions := map[string]IndicatorDefinition{}
	for _, def := range GetIndicatorRegistry() {
		definitions[def.Name] = def
		definitions[def.IndicatorID] = def
	}
	if len(rule.EntryConditions) == 0 {
		errs = append(errs, ValidationError{Field: "entryConditions", Message: "at least one entry condition is required"})
	}
	for i, condition := range append(rule.EntryConditions, rule.ExitConditions...) {
		prefix := fmt.Sprintf("conditions[%d]", i)
		indicator := canonicalIndicatorName(condition.Indicator, condition.IndicatorID)
		ref := canonicalIndicatorName(condition.Ref, condition.RefID)
		leftDef, leftOK := definitions[indicator]
		if !leftOK {
			errs = append(errs, ValidationError{Field: prefix + ".indicator", Message: "indicator is not registered: " + indicator})
		} else if leftDef.Status != "active" {
			errs = append(errs, ValidationError{Field: prefix + ".indicator", Message: "indicator is not active in registry: " + indicator})
		}
		rightDef, rightOK := definitions[ref]
		if ref != "" && !rightOK {
			errs = append(errs, ValidationError{Field: prefix + ".ref", Message: "ref indicator is not registered: " + ref})
		} else if ref != "" && rightDef.Status != "active" {
			errs = append(errs, ValidationError{Field: prefix + ".ref", Message: "ref indicator is not active in registry: " + ref})
		}
		if leftOK && ref != "" && rightOK && leftDef.Unit != rightDef.Unit {
			errs = append(errs, ValidationError{Field: prefix + ".ref", Message: fmt.Sprintf("unit mismatch: %s(%s) vs %s(%s)", indicator, leftDef.Unit, ref, rightDef.Unit)})
		}
		if condition.Lag < 0 || condition.Lag > 1 {
			errs = append(errs, ValidationError{Field: prefix + ".lag", Message: "lag currently supports 0 or 1 only"})
		}
		if condition.RefLag < 0 || condition.RefLag > 1 {
			errs = append(errs, ValidationError{Field: prefix + ".refLag", Message: "refLag currently supports 0 or 1 only"})
		}
		if leftOK && !operatorAllowed(condition.Operator, leftDef.AllowedOperators) {
			errs = append(errs, ValidationError{Field: prefix + ".operator", Message: "operator is not allowed for indicator: " + condition.Operator})
		}
		if ref == "" && (math.IsNaN(condition.Value) || math.IsInf(condition.Value, 0)) {
			errs = append(errs, ValidationError{Field: prefix + ".value", Message: "value is invalid"})
		}
	}
	if rule.StopLoss < 0.02 || rule.StopLoss > 0.12 {
		errs = append(errs, ValidationError{Field: "stopLoss", Message: "stopLoss must be in 0.02 - 0.12"})
	}
	if rule.StopGain < 0.03 || rule.StopGain > 0.25 {
		errs = append(errs, ValidationError{Field: "stopGain", Message: "stopGain must be in 0.03 - 0.25"})
	}
	if rule.MaxHoldDays < 1 || rule.MaxHoldDays > 30 {
		errs = append(errs, ValidationError{Field: "maxHoldDays", Message: "maxHoldDays must be in 1 - 30"})
	}
	if rule.MaxHoldings < 1 || rule.MaxHoldings > 20 {
		errs = append(errs, ValidationError{Field: "maxHoldings", Message: "maxHoldings must be in 1 - 20"})
	}
	return errs
}

func legacyConditionsToV2(conditions []Condition) []ConditionV2 {
	result := make([]ConditionV2, 0, len(conditions))
	for _, condition := range conditions {
		leftID := canonicalIndicatorID(condition.Indicator)
		right := RuleOperandV2{Value: &condition.Value, Lag: condition.RefLag}
		if strings.TrimSpace(condition.Ref) != "" {
			right = RuleOperandV2{IndicatorID: canonicalIndicatorID(condition.Ref), Lag: condition.RefLag}
		}
		converted := ConditionV2{
			Left:     RuleOperandV2{IndicatorID: leftID, Lag: condition.Lag},
			Operator: condition.Operator,
			Right:    right,
			Lag:      condition.Lag,
		}
		if isCrossOperator(condition.Operator) {
			previousLeft := RuleOperandV2{IndicatorID: leftID, Lag: 1}
			previousRight := RuleOperandV2{Value: &condition.Value, Lag: 1}
			if strings.TrimSpace(condition.Ref) != "" {
				previousRight = RuleOperandV2{IndicatorID: canonicalIndicatorID(condition.Ref), Lag: 1}
			}
			converted.PreviousLeft = &previousLeft
			converted.PreviousRight = &previousRight
		}
		result = append(result, converted)
	}
	return result
}

func conditionsV2ToLegacy(conditions []ConditionV2) []Condition {
	result := make([]Condition, 0, len(conditions))
	for _, condition := range conditions {
		lag := condition.Left.Lag
		if lag == 0 && condition.Lag != 0 {
			lag = condition.Lag
		}
		legacy := Condition{
			Indicator:   canonicalIndicatorName("", condition.Left.IndicatorID),
			IndicatorID: condition.Left.IndicatorID,
			Operator:    condition.Operator,
			Lag:         lag,
			RefLag:      condition.Right.Lag,
		}
		if condition.Right.IndicatorID != "" {
			legacy.Ref = canonicalIndicatorName("", condition.Right.IndicatorID)
			legacy.RefID = condition.Right.IndicatorID
		} else if condition.Right.Value != nil {
			legacy.Value = *condition.Right.Value
		}
		result = append(result, legacy)
	}
	return result
}

func ruleFingerprint(envelope RuleEnvelope) string {
	envelope.Fingerprint = ""
	payload, _ := json.Marshal(envelope)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// ruleExecutionFingerprint identifies executable behavior only. Names, research
// metadata and presentation parameters must not turn one rule into many votes.
func ruleExecutionFingerprint(rule Rule) string {
	rule = expandRuleCrossConditions(rule)
	rule.EntryConditions = sortedConditions(rule.EntryConditions)
	rule.ExitConditions = sortedConditions(rule.ExitConditions)
	payload, err := json.Marshal(rule)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func sortedConditions(conditions []Condition) []Condition {
	result := append([]Condition(nil), conditions...)
	sort.Slice(result, func(i, j int) bool {
		left, _ := json.Marshal(result[i])
		right, _ := json.Marshal(result[j])
		return string(left) < string(right)
	})
	return result
}

func expandRuleCrossConditions(rule Rule) Rule {
	// Entry is an ALL group and can represent a transition as two explicit lagged
	// comparisons. Exit is an ANY group, so legacy cross operators remain atomic;
	// RuleEnvelopeV2 records their previous operands explicitly.
	rule.EntryConditions = expandCrossConditions(rule.EntryConditions)
	return rule
}

func isCrossOperator(operator string) bool {
	switch strings.TrimSpace(operator) {
	case "cross_up", "cross_down", "crosses_above", "crosses_below":
		return true
	default:
		return false
	}
}

func expandCrossConditions(conditions []Condition) []Condition {
	result := make([]Condition, 0, len(conditions)*2)
	for _, condition := range conditions {
		switch strings.TrimSpace(condition.Operator) {
		case "cross_up", "crosses_above":
			previous := condition
			previous.Operator = "<="
			previous.Lag = 1
			previous.RefLag = 1
			current := condition
			current.Operator = ">"
			current.Lag = 0
			current.RefLag = 0
			result = append(result, previous, current)
		case "cross_down", "crosses_below":
			previous := condition
			previous.Operator = ">="
			previous.Lag = 1
			previous.RefLag = 1
			current := condition
			current.Operator = "<"
			current.Lag = 0
			current.RefLag = 0
			result = append(result, previous, current)
		default:
			result = append(result, condition)
		}
	}
	return result
}

func canonicalIndicatorID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, def := range GetIndicatorRegistry() {
		if strings.EqualFold(raw, def.IndicatorID) || strings.EqualFold(raw, def.Name) {
			return def.IndicatorID
		}
	}
	if strings.Contains(raw, ".") {
		return raw
	}
	return "stock_feature." + raw
}

func canonicalIndicatorName(name, indicatorID string) string {
	raw := strings.TrimSpace(firstNonBlank(indicatorID, name))
	if raw == "" {
		return ""
	}
	for _, def := range GetIndicatorRegistry() {
		if strings.EqualFold(raw, def.IndicatorID) || strings.EqualFold(raw, def.Name) {
			return def.Name
		}
	}
	if dot := strings.LastIndex(raw, "."); dot >= 0 && dot < len(raw)-1 {
		return raw[dot+1:]
	}
	return raw
}

func operatorAllowed(operator string, allowed []string) bool {
	operator = strings.TrimSpace(operator)
	for _, item := range allowed {
		if operator == item {
			return true
		}
	}
	return false
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sortedIndicatorIDs() []string {
	defs := GetIndicatorRegistry()
	ids := make([]string, 0, len(defs))
	for _, def := range defs {
		if def.Status == "active" {
			ids = append(ids, def.IndicatorID)
		}
	}
	sort.Strings(ids)
	return ids
}
