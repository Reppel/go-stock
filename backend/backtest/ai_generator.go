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
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Scenes      []string `json:"scenes"`
	Description string   `json:"description"`
	BaseRule    Rule     `json:"baseRule"`
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
	Source       string  `json:"source"`
}

// MarketContext 市场环境
type MarketContext struct {
	ShIndexReturn float64 `json:"shIndexReturn"`
	MarketState   string  `json:"marketState"`
}

// AIGenerator AI 假设生成器
type AIGenerator struct {
	openaiApi           interface{}
	templates           []HypothesisTemplate
	lastPrompt          string
	lastRawOutput       string
	lastModelName       string
	lastTemperature     float64
	lastGenerationError string
}

const (
	maxGeneratedHypotheses = 4
)

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
					{Indicator: "MA5", Operator: "cross_up", Ref: "MA20"},
					{Indicator: "VolumeRatio", Operator: ">", Value: 1.2},
				},
				ExitConditions: []Condition{
					{Indicator: "MA5", Operator: "cross_down", Ref: "MA20"},
				},
				StopLoss:    0.07,
				StopGain:    0.15,
				MaxHoldings: 5,
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
		},
		{
			ID:          "macd_golden_cross",
			Name:        "MACD 金叉",
			Scenes:      []string{"短线爆发", "趋势持有"},
			Description: "MACD 指标出现金叉且零轴上方时，短期上涨概率较高。",
			BaseRule: Rule{
				EntryConditions: []Condition{
					{Indicator: "MACD", Operator: "cross_up", Value: 0},
					{Indicator: "Close", Operator: ">", Ref: "MA20"},
				},
				ExitConditions: []Condition{
					{Indicator: "MACD", Operator: "cross_down", Value: 0},
				},
				StopLoss:    0.06,
				StopGain:    0.12,
				MaxHoldings: 5,
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

	// 3. 一个模板家族只生成一个可执行策略。参数变化属于稳定性测试，
	// 不作为独立策略落库，也不参与多策略投票。
	hypotheses := make([]Hypothesis, 0, len(candidates))
	for _, tpl := range candidates {
		hypotheses = append(hypotheses, g.generateCanonicalHypothesis(tpl, scene))
	}

	return deduplicateHypotheses(hypotheses, maxGeneratedHypotheses), nil
}

func (g *AIGenerator) generateCanonicalHypothesis(tpl HypothesisTemplate, scene string) Hypothesis {
	timeHorizon, targetReturn := canonicalTemplateParameters(tpl.ID, scene)
	rule := tpl.BaseRule
	rule.MaxHoldDays = timeHorizon
	rule.StopGain = targetReturn
	params := map[string]any{"timeHorizon": timeHorizon, "targetReturn": targetReturn}
	return Hypothesis{
		ID: tpl.ID, Name: tpl.Name, Description: g.renderDescription(tpl.Description, params),
		Scene: scene, Rule: rule, Params: g.paramsToJSON(params), TimeHorizon: timeHorizon,
		TargetReturn: targetReturn, Source: "template",
	}
}

func canonicalTemplateParameters(templateID, scene string) (int, float64) {
	switch templateID {
	case "rsi_oversold_rebound":
		return 10, 0.05
	case "boll_breakout":
		return 5, 0.05
	case "trend_following_ma", "macd_golden_cross":
		if scene == "趋势持有" {
			return 10, 0.08
		}
		return 5, 0.05
	default:
		return 5, 0.05
	}
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

func deduplicateHypotheses(hypotheses []Hypothesis, limit int) []Hypothesis {
	result := make([]Hypothesis, 0, len(hypotheses))
	seen := make(map[string]struct{}, len(hypotheses))
	for _, hypothesis := range hypotheses {
		key := ruleExecutionFingerprint(hypothesis.Rule)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, hypothesis)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result
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
func (g *AIGenerator) GenerateWithAI(scene string, stockScope string, ctx MarketContext, aiConfigId int) (hypotheses []Hypothesis, err error) {
	return g.GenerateWithAIWithResearch(scene, stockScope, ctx, aiConfigId, nil)
}

// GenerateWithAIWithResearch 只允许 AI 基于标准化研究想法生成规则；模板降级仍保持可用。
func (g *AIGenerator) GenerateWithAIWithResearch(scene string, stockScope string, ctx MarketContext, aiConfigId int, researchIdeas []ResearchIdea) (hypotheses []Hypothesis, err error) {
	if g == nil {
		return nil, fmt.Errorf("AI 生成器未初始化")
	}
	defer func() {
		if r := recover(); r != nil {
			logger.SugaredLogger.Errorf("prediction GenerateWithAI panic: %v", r)
			g.lastGenerationError = fmt.Sprintf("AI 生成异常: %v", r)
			hypotheses, err = g.Generate(scene, stockScope, ctx)
		}
	}()
	return g.callLLM(scene, stockScope, ctx, aiConfigId, researchIdeas)
}

func (g *AIGenerator) callLLM(scene string, stockScope string, ctx MarketContext, aiConfigId int, researchIdeas []ResearchIdea) ([]Hypothesis, error) {
	g.lastModelName = ""
	g.lastTemperature = 0
	g.lastGenerationError = ""
	// 如果没有配置 AI，回退到模板
	if aiConfigId <= 0 {
		g.lastGenerationError = "aiConfigId 无效，使用模板"
		logger.SugaredLogger.Info("aiConfigId 无效，fallback 到模板生成")
		return g.Generate(scene, stockScope, ctx)
	}
	if len(researchIdeas) == 0 {
		return nil, fmt.Errorf("AI 策略生成必须先产出 ResearchIdea v2")
	}

	prompt := g.buildLLMPrompt(scene, stockScope, ctx, researchIdeas)
	g.lastPrompt = prompt
	g.lastRawOutput = ""

	llmCtx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	openAi := data.NewDeepSeekOpenAi(llmCtx, aiConfigId)
	g.lastModelName = openAi.Model
	g.lastTemperature = openAi.Temperature
	if strings.TrimSpace(openAi.BaseUrl) == "" || strings.TrimSpace(openAi.ApiKey) == "" || strings.TrimSpace(openAi.Model) == "" {
		g.lastGenerationError = "AI 配置不完整，使用模板"
		logger.SugaredLogger.Warn("AI 配置不完整，fallback 到模板生成")
		return g.Generate(scene, stockScope, ctx)
	}

	ch := make(chan map[string]any, 512)
	go func() {
		defer close(ch)
		defer func() {
			if r := recover(); r != nil {
				logger.SugaredLogger.Errorf("prediction AskAi panic: %v", r)
				ch <- map[string]any{"code": 0, "content": fmt.Sprintf("AI 调用异常: %v", r)}
			}
		}()
		messages := []map[string]interface{}{
			{
				"role":    "user",
				"content": prompt,
			},
		}
		data.AskAi(openAi, nil, messages, ch, prompt, false)
	}()

	var content strings.Builder
	var streamErr string
	timeout := time.After(7 * time.Minute)
done:
	for {
		select {
		case msg, ok := <-ch:
			if !ok || msg == nil {
				break done
			}
			if code, ok := msg["code"].(int); ok && code == 0 {
				if c, ok := msg["content"].(string); ok && c != "" {
					streamErr = c
				}
				continue
			}
			if c, ok := msg["content"].(string); ok {
				content.WriteString(c)
			}
		case <-timeout:
			g.lastGenerationError = "LLM 生成假设超时，使用模板"
			logger.SugaredLogger.Warn("LLM 生成假设超时，fallback 到模板")
			return g.Generate(scene, stockScope, ctx)
		}
	}

	result := content.String()
	g.lastRawOutput = result
	if result == "" {
		g.lastGenerationError = firstNonBlank(streamErr, "LLM 返回为空，使用模板")
		if streamErr != "" {
			logger.SugaredLogger.Warnf("LLM 返回错误：%s，fallback 到模板", streamErr)
		}
		logger.SugaredLogger.Warn("LLM 返回为空，fallback 到模板")
		return g.Generate(scene, stockScope, ctx)
	}

	hypotheses, err := g.parseLLMResponse(result, scene)
	if err != nil {
		g.lastGenerationError = err.Error()
		logger.SugaredLogger.Warnf("解析 LLM 假设失败: %v, fallback 到模板", err)
		return g.Generate(scene, stockScope, ctx)
	}

	if len(hypotheses) == 0 {
		g.lastGenerationError = "LLM 未生成有效假设，使用模板"
		logger.SugaredLogger.Warn("LLM 未生成有效假设，fallback 到模板")
		return g.Generate(scene, stockScope, ctx)
	}

	logger.SugaredLogger.Infof("LLM 生成假设成功，共 %d 条", len(hypotheses))
	return hypotheses, nil
}

func (g *AIGenerator) buildLLMPrompt(scene string, stockScope string, ctx MarketContext, researchIdeas []ResearchIdea) string {
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
	indicatorIDs := strings.Join(sortedIndicatorIDs(), ", ")
	researchJSON, _ := json.Marshal(researchIdeas)

	return fmt.Sprintf(`你是一位资深量化策略研究员。请根据以下信息，设计 1-4 个互不重复的量化交易规则（Hypothesis）。参数扰动不算新策略。

【投资场景】%s
【股票池】%s
【市场环境】%s（上证指数区间收益率 %.2f%%）

【标准研究想法 ResearchIdea v2】
%s

【可用指标注册表】
只能引用以下 indicatorId，禁止编造新指标：
%s

【条件运算符】>, >=, <, <=, ==, !=；exit 可使用带显式 previousLeft/previousRight 的 cross_down

【参考策略模板】
%s

请直接返回一个 JSON 数组，每个元素优先使用 prediction-rule/v2 envelope：
{
  "schemaVersion": "prediction-rule/v2",
  "registryVersion": "indicator-registry/v2",
  "featureVersion": "daily_v3_qfq",
  "engineVersion": "quant-engine/v3",
  "name": "策略名称",
  "description": "策略描述",
  "scene": "%s",
  "timeHorizon": 5,
  "targetReturn": 0.05,
  "entry": {"all": [{"left": {"indicatorId": "stock_feature.MA5", "lag": 1}, "operator": "<=", "right": {"indicatorId": "stock_feature.MA20", "lag": 1}}, {"left": {"indicatorId": "stock_feature.MA5", "lag": 0}, "operator": ">", "right": {"indicatorId": "stock_feature.MA20", "lag": 0}}]},
  "exit": {"any": [{"left": {"indicatorId": "stock_feature.MA5", "lag": 0}, "operator": "cross_down", "right": {"indicatorId": "stock_feature.MA20", "lag": 0}, "previousLeft": {"indicatorId": "stock_feature.MA5", "lag": 1}, "previousRight": {"indicatorId": "stock_feature.MA20", "lag": 1}}]},
  "risk": {"stopLoss": 0.07, "stopGain": 0.15, "maxHoldDays": 5, "maxHoldings": 5}
}

注意：
1. 只返回 JSON 数组，不要任何解释或 markdown 代码块。
2. 策略要适合当前投资场景。
3. indicatorId 必须来自指标注册表。
4. lag=0 表示信号日收盘后可得值；lag=1 表示上一交易日值。
5. entry 的“上穿/金叉”必须显式写成 lag=1 的 <= 与 lag=0 的 > 两个 all 条件；exit 的下穿必须在单个 cross_down 条件中显式给出 previousLeft/previousRight，禁止隐式 previous。
6. 规则必须能追溯到上面的 ResearchIdea v2，不得引用研究模板之外的候选数据。`,
		scene, stockScope, ctx.MarketState, ctx.ShIndexReturn*100, string(researchJSON), indicatorIDs, strings.Join(examples, "\n"), scene)
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
		// LLM 可能返回单个对象而非数组，尝试兜底解析
		var single map[string]any
		if err2 := json.Unmarshal([]byte(result), &single); err2 != nil {
			return nil, fmt.Errorf("解析 JSON 失败（数组和对象均失败）: %w", err)
		}
		raw = []map[string]any{single}
	}

	var hypotheses []Hypothesis
	for i, item := range raw {
		name := getString(item, "name")
		desc := getString(item, "description")
		timeHorizon := getInt(item, "timeHorizon")
		targetReturn := getFloat64(item, "targetReturn")

		var rule Rule
		if schema := getString(item, "schemaVersion"); schema != "" {
			envelopeJSON, _ := json.Marshal(item)
			parsed, _, errs := ParseRuleEnvelope(string(envelopeJSON))
			if len(errs) > 0 || parsed == nil {
				logger.SugaredLogger.Warnf("解析 RuleEnvelope 失败: %+v", errs)
				continue
			}
			rule = *parsed
		} else {
			ruleMap, ok := item["rule"].(map[string]any)
			if !ok {
				continue
			}
			ruleJSON, _ := json.Marshal(ruleMap)
			if err := json.Unmarshal(ruleJSON, &rule); err != nil {
				logger.SugaredLogger.Warnf("解析 rule 失败: %v", err)
				continue
			}
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
			Source:       "ai",
		})
	}

	return deduplicateHypotheses(hypotheses, maxGeneratedHypotheses), nil
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
