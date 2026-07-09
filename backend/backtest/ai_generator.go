package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/logger"
	"strings"
	"time"
)

// HypothesisTemplate 预测假设模板
type HypothesisTemplate struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Scenes      []string    `json:"scenes"`
	Description string      `json:"description"`
	BaseRule    Rule        `json:"baseRule"`
	Variations  []Variation `json:"variations"`
}

// Variation 参数变体
type Variation struct {
	Param  string `json:"param"`
	Values []any  `json:"values"`
}

// Hypothesis AI 生成的预测假设
type Hypothesis struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Scene        string  `json:"scene"`
	Rule         Rule    `json:"rule"`
	Params       string  `json:"params"`
	TimeHorizon  int     `json:"timeHorizon"`
	TargetReturn float64 `json:"targetReturn"`
}

// MarketContext 市场环境
type MarketContext struct {
	ShIndexReturn float64 `json:"shIndexReturn"`
	MarketState   string  `json:"marketState"`
}

// AIGenerator AI 假设生成器
type AIGenerator struct {
	openaiApi interface{}
	templates []HypothesisTemplate
}

// NewAIGenerator 创建生成器
func NewAIGenerator() *AIGenerator {
	return &AIGenerator{
		templates: LoadHypothesisTemplates(),
	}
}

// LoadHypothesisTemplates 加载预置模板
func LoadHypothesisTemplates() []HypothesisTemplate {
	return []HypothesisTemplate{
		{
			ID:          "trend_following_ma",
			Name:        "双均线趋势跟踪",
			Scenes:      []string{"短线爆发", "趋势持有"},
			Description: "当短期均线上穿长期均线，且成交量放大时，未来 {{timeHorizon}} 个交易日上涨概率较高。",
			BaseRule: Rule{
				EntryConditions: []Condition{
					{Indicator: "MA5", Operator: ">", Ref: "MA20"},
					{Indicator: "VolumeRatio", Operator: ">", Value: 1.2},
				},
				ExitConditions: []Condition{
					{Indicator: "MA5", Operator: "<", Ref: "MA20"},
				},
				StopLoss:    0.07,
				StopGain:    0.15,
				MaxHoldings: 5,
			},
			Variations: []Variation{
				{Param: "timeHorizon", Values: []any{3, 5, 10}},
				{Param: "targetReturn", Values: []any{0.03, 0.05, 0.08}},
			},
		},
		{
			ID:          "rsi_oversold_rebound",
			Name:        "RSI 超卖反弹",
			Scenes:      []string{"波段反弹"},
			Description: "当 RSI 进入超卖区间且短期跌幅较大时，未来 {{timeHorizon}} 个交易日出现反弹的概率较高。",
			BaseRule: Rule{
				EntryConditions: []Condition{
					{Indicator: "RSI6", Operator: "<", Value: 30},
					{Indicator: "ChangeRate5", Operator: "<", Value: -0.05},
				},
				ExitConditions: []Condition{
					{Indicator: "RSI6", Operator: ">", Value: 60},
				},
				StopLoss:    0.05,
				StopGain:    0.10,
				MaxHoldings: 5,
			},
			Variations: []Variation{
				{Param: "timeHorizon", Values: []any{5, 10, 15}},
				{Param: "targetReturn", Values: []any{0.03, 0.05, 0.08}},
			},
		},
		{
			ID:          "boll_breakout",
			Name:        "布林带突破",
			Scenes:      []string{"短线爆发"},
			Description: "当股价放量突破布林带上轨时，短期可能延续上涨动能。",
			BaseRule: Rule{
				EntryConditions: []Condition{
					{Indicator: "Close", Operator: ">", Ref: "BOLLUpper"},
					{Indicator: "VolumeRatio", Operator: ">", Value: 1.5},
				},
				ExitConditions: []Condition{
					{Indicator: "Close", Operator: "<", Ref: "BOLLMid"},
				},
				StopLoss:    0.07,
				StopGain:    0.12,
				MaxHoldings: 5,
			},
			Variations: []Variation{
				{Param: "timeHorizon", Values: []any{3, 5}},
				{Param: "targetReturn", Values: []any{0.03, 0.05}},
			},
		},
		{
			ID:          "macd_golden_cross",
			Name:        "MACD 金叉",
			Scenes:      []string{"短线爆发", "趋势持有"},
			Description: "MACD 指标出现金叉且零轴上方时，短期上涨概率较高。",
			BaseRule: Rule{
				EntryConditions: []Condition{
					{Indicator: "MACD", Operator: ">", Value: 0},
					{Indicator: "Close", Operator: ">", Ref: "MA20"},
				},
				ExitConditions: []Condition{
					{Indicator: "Close", Operator: "<", Ref: "MA10"},
				},
				StopLoss:    0.06,
				StopGain:    0.12,
				MaxHoldings: 5,
			},
			Variations: []Variation{
				{Param: "timeHorizon", Values: []any{5, 10}},
				{Param: "targetReturn", Values: []any{0.04, 0.08}},
			},
		},
	}
}

// Generate 生成预测假设
func (g *AIGenerator) Generate(scene string, stockScope string, ctx MarketContext) ([]Hypothesis, error) {
	// 1. 根据场景筛选模板
	candidates := g.selectTemplates(scene)
	if len(candidates) == 0 {
		candidates = g.templates
	}

	// 2. 根据市场环境调整描述
	candidates = g.applyMarketContext(candidates, ctx)

	// 3. 生成参数变体
	var hypotheses []Hypothesis
	for _, tpl := range candidates {
		variants := g.generateVariants(tpl)
		hypotheses = append(hypotheses, variants...)
	}

	return hypotheses, nil
}

// selectTemplates 按场景筛选模板
func (g *AIGenerator) selectTemplates(scene string) []HypothesisTemplate {
	var result []HypothesisTemplate
	for _, tpl := range g.templates {
		for _, s := range tpl.Scenes {
			if s == scene {
				result = append(result, tpl)
				break
			}
		}
	}
	return result
}

// applyMarketContext 根据市场环境调整模板
func (g *AIGenerator) applyMarketContext(templates []HypothesisTemplate, ctx MarketContext) []HypothesisTemplate {
	for i := range templates {
		if ctx.MarketState == "震荡市" && templates[i].ID == "rsi_oversold_rebound" {
			templates[i].Description = "震荡市中，个股回调后容易出现超卖反弹，" + templates[i].Description
		}
		if ctx.MarketState == "上涨趋势" && templates[i].ID == "trend_following_ma" {
			templates[i].Description = "上涨趋势中，趋势跟踪策略胜率较高，" + templates[i].Description
		}
	}
	return templates
}

// generateVariants 生成参数变体
func (g *AIGenerator) generateVariants(tpl HypothesisTemplate) []Hypothesis {
	var hypotheses []Hypothesis

	// 默认一套参数
	defaultParams := map[string]any{
		"timeHorizon":  5,
		"targetReturn": 0.05,
	}

	// 如果有变体参数，取第一个值组合
	if len(tpl.Variations) > 0 {
		for _, v := range tpl.Variations {
			if len(v.Values) > 0 {
				defaultParams[v.Param] = v.Values[0]
			}
		}
	}

	timeHorizon := 5
	targetReturn := 0.05

	if v, ok := defaultParams["timeHorizon"].(int); ok {
		timeHorizon = v
	}
	if v, ok := defaultParams["targetReturn"].(float64); ok {
		targetReturn = v
	}

	description := g.renderDescription(tpl.Description, defaultParams)

	h := Hypothesis{
		ID:           fmt.Sprintf("%s_default", tpl.ID),
		Name:         tpl.Name,
		Description:  description,
		Scene:        tpl.Scenes[0],
		Rule:         tpl.BaseRule,
		Params:       g.paramsToJSON(defaultParams),
		TimeHorizon:  timeHorizon,
		TargetReturn: targetReturn,
	}
	return append(hypotheses, h)
}

// renderDescription 渲染描述
func (g *AIGenerator) renderDescription(template string, params map[string]any) string {
	result := template
	for k, v := range params {
		placeholder := fmt.Sprintf("{{%s}}", k)
		result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", v))
	}
	return result
}

// paramsToJSON 参数转 JSON
func (g *AIGenerator) paramsToJSON(params map[string]any) string {
	b, _ := json.Marshal(params)
	return string(b)
}

// GenerateWithAI 调用 LLM 生成假设
func (g *AIGenerator) GenerateWithAI(scene string, stockScope string, ctx MarketContext, aiConfigId int) ([]Hypothesis, error) {
	return g.callLLM(scene, stockScope, ctx, aiConfigId)
}

func (g *AIGenerator) callLLM(scene string, stockScope string, ctx MarketContext, aiConfigId int) ([]Hypothesis, error) {
	// 如果没有配置 AI，回退到模板
	if aiConfigId <= 0 {
		logger.SugaredLogger.Info("aiConfigId 无效，fallback 到模板生成")
		return g.Generate(scene, stockScope, ctx)
	}

	prompt := g.buildLLMPrompt(scene, stockScope, ctx)

	openAi := data.NewDeepSeekOpenAi(context.Background(), aiConfigId)
	ch := openAi.NewChatStream("预测工厂", "ai-prediction", prompt, nil, nil, false)

	var content strings.Builder
	timeout := time.After(60 * time.Second)
done:
	for {
		select {
		case msg := <-ch:
			if msg == nil {
				break done
			}
			if c, ok := msg["content"].(string); ok {
				content.WriteString(c)
			}
			if doneFlag, ok := msg["done"].(bool); ok && doneFlag {
				break done
			}
		case <-timeout:
			logger.SugaredLogger.Warn("LLM 生成假设超时，fallback 到模板")
			return g.Generate(scene, stockScope, ctx)
		}
	}

	result := content.String()
	if result == "" {
		logger.SugaredLogger.Warn("LLM 返回为空，fallback 到模板")
		return g.Generate(scene, stockScope, ctx)
	}

	hypotheses, err := g.parseLLMResponse(result, scene)
	if err != nil {
		logger.SugaredLogger.Warnf("解析 LLM 假设失败: %v, fallback 到模板", err)
		return g.Generate(scene, stockScope, ctx)
	}

	if len(hypotheses) == 0 {
		logger.SugaredLogger.Warn("LLM 未生成有效假设，fallback 到模板")
		return g.Generate(scene, stockScope, ctx)
	}

	logger.SugaredLogger.Infof("LLM 生成假设成功，共 %d 条", len(hypotheses))
	return hypotheses, nil
}

func (g *AIGenerator) buildLLMPrompt(scene string, stockScope string, ctx MarketContext) string {
	templateExamples := g.templates
	var examples []string
	for _, tpl := range templateExamples {
		example := fmt.Sprintf(`- %s: %s (入场条件: %s, 出场条件: %s, timeHorizon: 5, targetReturn: 0.05)`,
			tpl.Name,
			tpl.Description,
			g.formatConditions(tpl.BaseRule.EntryConditions),
			g.formatConditions(tpl.BaseRule.ExitConditions),
		)
		examples = append(examples, example)
	}

	return fmt.Sprintf(`你是一位资深量化策略研究员。请根据以下信息，设计 2-4 个量化交易规则（Hypothesis）。

【投资场景】%s
【股票池】%s
【市场环境】%s（上证指数区间收益率 %.2f%%）

【可用技术指标】
MA5, MA10, MA20, MA60, MACD, RSI6, RSI12, KDJ_K, BOLLUpper, BOLLMid, BOLLLower, VolumeRatio, ATR, ChangeRate5, ChangeRate20, Close, Open, High, Low, Volume

【条件运算符】>, >=, <, <=, ==, !=

【参考策略模板】
%s

请直接返回一个 JSON 数组，每个元素包含以下字段：
{
  "name": "策略名称",
  "description": "策略描述",
  "scene": "%s",
  "timeHorizon": 5,
  "targetReturn": 0.05,
  "rule": {
    "entryConditions": [{"indicator": "MA5", "operator": ">", "ref": "MA20"}],
    "exitConditions": [{"indicator": "MA5", "operator": "<", "ref": "MA20"}],
    "stopLoss": 0.07,
    "stopGain": 0.15,
    "maxHoldDays": 5,
    "maxHoldings": 5
  }
}

注意：
1. 只返回 JSON 数组，不要任何解释或 markdown 代码块。
2. 策略要适合当前投资场景。
3. indicator 必须是可用技术指标之一。
4. 当条件需要参考另一个指标时用 ref，用具体数值时用 value（数字）。`,
		scene, stockScope, ctx.MarketState, ctx.ShIndexReturn*100, strings.Join(examples, "\n"), scene)
}

func (g *AIGenerator) formatConditions(conditions []Condition) string {
	var parts []string
	for _, c := range conditions {
		if c.Ref != "" {
			parts = append(parts, fmt.Sprintf("%s %s %s", c.Indicator, c.Operator, c.Ref))
		} else {
			parts = append(parts, fmt.Sprintf("%s %s %v", c.Indicator, c.Operator, c.Value))
		}
	}
	if len(parts) == 0 {
		return "无"
	}
	return strings.Join(parts, " && ")
}

func (g *AIGenerator) parseLLMResponse(result, scene string) ([]Hypothesis, error) {
	// 清理 markdown 代码块
	result = strings.TrimSpace(result)
	result = strings.TrimPrefix(result, "```json")
	result = strings.TrimPrefix(result, "```")
	result = strings.TrimSuffix(result, "```")
	result = strings.TrimSpace(result)

	var raw []map[string]any
	if err := json.Unmarshal([]byte(result), &raw); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	}

	var hypotheses []Hypothesis
	for i, item := range raw {
		name := getString(item, "name")
		desc := getString(item, "description")
		timeHorizon := getInt(item, "timeHorizon")
		targetReturn := getFloat64(item, "targetReturn")

		ruleMap, ok := item["rule"].(map[string]any)
		if !ok {
			continue
		}

		// 重新序列化 rule
		ruleJSON, _ := json.Marshal(ruleMap)
		var rule Rule
		if err := json.Unmarshal(ruleJSON, &rule); err != nil {
			logger.SugaredLogger.Warnf("解析 rule 失败: %v", err)
			continue
		}

		if name == "" {
			name = fmt.Sprintf("AI策略_%d", i+1)
		}
		if desc == "" {
			desc = name
		}

		hypotheses = append(hypotheses, Hypothesis{
			ID:           fmt.Sprintf("ai_%s_%d", scene, i),
			Name:         name,
			Description:  desc,
			Scene:        scene,
			Rule:         rule,
			Params:       g.paramsToJSON(map[string]any{"timeHorizon": timeHorizon, "targetReturn": targetReturn}),
			TimeHorizon:  timeHorizon,
			TargetReturn: targetReturn,
		})
	}

	return hypotheses, nil
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	}
	return 0
}

func getFloat64(m map[string]any, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}
