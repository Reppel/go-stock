package backtest

import (
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"math"
	"sort"
)

// PortfolioRiskReport 组合风险报告
type PortfolioRiskReport struct {
	TotalStrategies      int                   `json:"totalStrategies"`
	ActiveStrategies     int                   `json:"activeStrategies"`
	MaxDrawdown          float64               `json:"maxDrawdown"`
	MaxDrawdownDuration  int                   `json:"maxDrawdownDuration"` // 最大回撤持续天数
	VaR95                float64               `json:"var95"`               // 95% VaR
	VaR99                float64               `json:"var99"`               // 99% VaR
	CVaR95               float64               `json:"cvar95"`              // 95% Expected Shortfall
	SharpeRatio          float64               `json:"sharpeRatio"`
	SortinoRatio         float64               `json:"sortinoRatio"`
	CalmarRatio          float64               `json:"calmarRatio"`
	AnnualizedReturn     float64               `json:"annualizedReturn"`
	AnnualizedVolatility float64               `json:"annualizedVolatility"`
	IndustryConcentration []IndustryConcentration `json:"industryConcentration"`
	StrategyCorrelations  []StrategyCorrelationPair `json:"strategyCorrelations"`
	Warnings             []string              `json:"warnings"`
	CircuitBreakerActive bool                  `json:"circuitBreakerActive"`
	CircuitBreakerReason string                `json:"circuitBreakerReason"`
}

type IndustryConcentration struct {
	Industry    string  `json:"industry"`
	Weight      float64 `json:"weight"`
	StockCount  int     `json:"stockCount"`
	RiskLevel   string  `json:"riskLevel"` // low / medium / high
}

type StrategyCorrelationPair struct {
	Strategy1   string  `json:"strategy1"`
	Strategy2   string  `json:"strategy2"`
	Correlation float64 `json:"correlation"`
	Warning     bool    `json:"warning"`
}

// PortfolioRiskAnalyzer 组合风险分析器
type PortfolioRiskAnalyzer struct {
	hypotheses   []models.PredictionHypothesis
	dailyReturns []float64
	navCurve     []float64
}

// NewPortfolioRiskAnalyzer 创建组合风险分析器
func NewPortfolioRiskAnalyzer() *PortfolioRiskAnalyzer {
	return &PortfolioRiskAnalyzer{}
}

// Analyze 分析组合风险
func (p *PortfolioRiskAnalyzer) Analyze(hypotheses []models.PredictionHypothesis, dailyNAV []DailyNav) *PortfolioRiskReport {
	report := &PortfolioRiskReport{
		TotalStrategies:       len(hypotheses),
		Warnings:              make([]string, 0),
		IndustryConcentration: make([]IndustryConcentration, 0),
		StrategyCorrelations:  make([]StrategyCorrelationPair, 0),
	}

	// 只统计活跃策略
	for _, h := range hypotheses {
		if h.Status == "active" || h.Status == "paper_trade" || h.Status == "active_candidate" {
			report.ActiveStrategies++
		}
	}

	// 计算日收益率曲线
	if len(dailyNAV) > 1 {
		p.dailyReturns = make([]float64, 0, len(dailyNAV)-1)
		p.navCurve = make([]float64, len(dailyNAV))
		for i, nav := range dailyNAV {
			p.navCurve[i] = nav.Nav
			if i > 0 && dailyNAV[i-1].Nav > 0 {
				p.dailyReturns = append(p.dailyReturns, nav.Nav/dailyNAV[i-1].Nav-1)
			}
		}
	}

	// 计算性能指标
	p.calculatePerformanceMetrics(report, dailyNAV)

	// 计算 VaR/CVaR
	p.calculateVaR(report)

	// 计算最大回撤
	p.calculateMaxDrawdown(report)

	// 行业集中度分析
	p.calculateIndustryConcentration(report, hypotheses)

	// 策略相关性分析
	p.calculateStrategyCorrelations(report, hypotheses)

	// 熔断检查
	p.checkCircuitBreaker(report)

	return report
}

func (p *PortfolioRiskAnalyzer) calculatePerformanceMetrics(report *PortfolioRiskReport, dailyNAV []DailyNav) {
	if len(dailyNAV) < 2 {
		return
	}

	// 总收益
	startNAV := dailyNAV[0].Nav
	endNAV := dailyNAV[len(dailyNAV)-1].Nav
	// 年化收益率（假设 252 个交易日）
	tradingDays := len(dailyNAV)
	if tradingDays > 0 && startNAV > 0 {
		totalReturn := endNAV/startNAV - 1
		report.AnnualizedReturn = math.Pow(1+totalReturn, 252.0/float64(tradingDays)) - 1
	}

	// 年化波动率
	if len(p.dailyReturns) > 0 {
		report.AnnualizedVolatility = stdDev(p.dailyReturns) * math.Sqrt(252)
	}

	// Sharpe Ratio（假设无风险利率 2%）
	if report.AnnualizedVolatility > 0 {
		report.SharpeRatio = (report.AnnualizedReturn - 0.02) / report.AnnualizedVolatility
	}

	// Sortino Ratio（下行波动率）
	downsideReturns := make([]float64, 0)
	for _, r := range p.dailyReturns {
		if r < 0 {
			downsideReturns = append(downsideReturns, r)
		}
	}
	if len(downsideReturns) > 0 {
		downsideVol := stdDev(downsideReturns) * math.Sqrt(252)
		if downsideVol > 0 {
			report.SortinoRatio = (report.AnnualizedReturn - 0.02) / downsideVol
		}
	}

	// Calmar Ratio
	if report.MaxDrawdown > 0 {
		report.CalmarRatio = report.AnnualizedReturn / report.MaxDrawdown
	}
}

func (p *PortfolioRiskAnalyzer) calculateVaR(report *PortfolioRiskReport) {
	if len(p.dailyReturns) < 10 {
		return
	}

	// 历史模拟法 VaR
	sorted := make([]float64, len(p.dailyReturns))
	copy(sorted, p.dailyReturns)
	sort.Float64s(sorted)

	// 95% VaR
	idx95 := int(float64(len(sorted)) * 0.05)
	if idx95 < 0 {
		idx95 = 0
	}
	report.VaR95 = -sorted[idx95]

	// 99% VaR
	idx99 := int(float64(len(sorted)) * 0.01)
	if idx99 < 0 {
		idx99 = 0
	}
	report.VaR99 = -sorted[idx99]

	// CVaR (Expected Shortfall) — 最差 5% 的平均损失
	worstCount := len(sorted) / 20
	if worstCount < 1 {
		worstCount = 1
	}
	sum := 0.0
	for i := 0; i < worstCount; i++ {
		sum += sorted[i]
	}
	report.CVaR95 = -sum / float64(worstCount)

	if report.VaR95 > 0.05 {
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("95%% VaR 为 %.2f%%，组合风险较高", report.VaR95*100))
	}
}

func (p *PortfolioRiskAnalyzer) calculateMaxDrawdown(report *PortfolioRiskReport) {
	if len(p.navCurve) < 2 {
		return
	}

	peak := p.navCurve[0]
	maxDD := 0.0
	peakIndex := 0
	ddDuration := 0

	for i, nav := range p.navCurve {
		if nav > peak {
			peak = nav
			peakIndex = i
		} else {
			dd := (peak - nav) / peak
			if dd > maxDD {
				maxDD = dd
				ddDuration = i - peakIndex
			}
		}
	}

	report.MaxDrawdown = maxDD
	report.MaxDrawdownDuration = ddDuration

	if maxDD > 0.15 {
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("最大回撤 %.1f%%，超过 15%% 警戒线", maxDD*100))
	}
}

func (p *PortfolioRiskAnalyzer) calculateIndustryConcentration(report *PortfolioRiskReport, hypotheses []models.PredictionHypothesis) {
	industryCounts := make(map[string]int)
	totalPositions := 0

	for _, h := range hypotheses {
		if h.Status != "active" && h.Status != "paper_trade" {
			continue
		}
		// 从 backtest trades 中获取各股票的行业分布
		var trades []models.PredictionTrade
		if db.Dao != nil {
			db.Dao.Where("hypothesis_id = ?", h.ID).Find(&trades)
		}
		for _, t := range trades {
			industry, _ := stockIndustryAndConcepts(t.StockCode)
			if industry == "" {
				industry = "unknown"
			}
			industryCounts[industry]++
			totalPositions++
		}
	}

	if totalPositions == 0 {
		return
	}

	// 检查是否有行业过度集中
	for industry, count := range industryCounts {
		weight := float64(count) / float64(totalPositions)
		riskLevel := "low"
		if weight > 0.30 {
			riskLevel = "high"
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("行业 %s 集中度 %.0f%%，超过 30%% 上限", industry, weight*100))
		} else if weight > 0.20 {
			riskLevel = "medium"
		}
		report.IndustryConcentration = append(report.IndustryConcentration, IndustryConcentration{
			Industry:  industry,
			Weight:    weight,
			StockCount: count,
			RiskLevel: riskLevel,
		})
	}
}

func (p *PortfolioRiskAnalyzer) calculateStrategyCorrelations(report *PortfolioRiskReport, hypotheses []models.PredictionHypothesis) {
	// 提取每个策略的日收益率序列
	type strategyReturns struct {
		name    string
		returns []float64
	}

	strategies := make([]strategyReturns, 0)
	for _, h := range hypotheses {
		if h.Status != "active" && h.Status != "paper_trade" {
			continue
		}
		// 从 backtest trades 中计算日收益率
		// 简化：使用已有的 daily NAV 数据
		var dailyReturns []float64
		if db.Dao != nil {
			var dailies []models.PredictionHypothesisDaily
			db.Dao.Where("hypothesis_id = ?", h.ID).Order("date asc").Find(&dailies)
			if len(dailies) > 1 {
				for i := 1; i < len(dailies); i++ {
					if dailies[i-1].Nav > 0 {
						dailyReturns = append(dailyReturns, dailies[i].Nav/dailies[i-1].Nav-1)
					}
				}
			}
		}
		if len(dailyReturns) > 10 {
			strategies = append(strategies, strategyReturns{name: h.Name, returns: dailyReturns})
		}
	}

	// 计算策略间相关性
	for i := 0; i < len(strategies); i++ {
		for j := i + 1; j < len(strategies); j++ {
			ax, ay := alignVectors(strategies[i].returns, strategies[j].returns)
			corr := pearsonCorrelation(ax, ay)
			pair := StrategyCorrelationPair{
				Strategy1:   strategies[i].name,
				Strategy2:   strategies[j].name,
				Correlation: corr,
			}
			if math.Abs(corr) > 0.70 {
				pair.Warning = true
				report.Warnings = append(report.Warnings,
					fmt.Sprintf("策略 %s 与 %s 相关性 %.2f，存在同质化风险",
						strategies[i].name, strategies[j].name, corr))
			}
			report.StrategyCorrelations = append(report.StrategyCorrelations, pair)
		}
	}
}

func (p *PortfolioRiskAnalyzer) checkCircuitBreaker(report *PortfolioRiskReport) {
	// 熔断条件检查
	triggers := 0

	if report.MaxDrawdown > 0.20 {
		triggers++
		report.Warnings = append(report.Warnings, "组合回撤超过 20% 熔断线")
	}

	if report.VaR95 > 0.08 {
		triggers++
		report.Warnings = append(report.Warnings, "95% VaR 超过 8% 熔断线")
	}

	if report.AnnualizedVolatility > 0.50 {
		triggers++
		report.Warnings = append(report.Warnings, "年化波动率超过 50% 熔断线")
	}

	if triggers >= 2 {
		report.CircuitBreakerActive = true
		report.CircuitBreakerReason = fmt.Sprintf("触发 %d 个熔断条件：建议暂停所有新开仓，仅保留现有持仓的风控", triggers)
	}
}

// GetPortfolioRiskReport 获取组合风险报告
func GetPortfolioRiskReport(hypotheses []models.PredictionHypothesis) *PortfolioRiskReport {
	analyzer := NewPortfolioRiskAnalyzer()

	// 汇总所有策略的 daily NAV 为组合 NAV
	combinedNAV := make([]DailyNav, 0)
	navByDate := make(map[string]float64)
	navDateOrder := make([]string, 0)

	for _, h := range hypotheses {
		if h.Status != "active" && h.Status != "paper_trade" {
			continue
		}
		if db.Dao != nil {
			var dailies []models.PredictionHypothesisDaily
			db.Dao.Where("hypothesis_id = ?", h.ID).Order("date asc").Find(&dailies)
			for _, d := range dailies {
				if _, exists := navByDate[d.Date]; !exists {
					navDateOrder = append(navDateOrder, d.Date)
				}
				navByDate[d.Date] += d.Nav
			}
		}
	}

	sort.Strings(navDateOrder)
	for _, date := range navDateOrder {
		combinedNAV = append(combinedNAV, DailyNav{Date: date, Nav: navByDate[date]})
	}

	return analyzer.Analyze(hypotheses, combinedNAV)
}

// PortfolioAllocation 组合分配方案
type PortfolioAllocation struct {
	Method          string             `json:"method"`
	Description     string             `json:"description"`
	Allocations     []StrategyAllocation `json:"allocations"`
	ExpectedReturn  float64            `json:"expectedReturn"`
	ExpectedRisk    float64            `json:"expectedRisk"`
	SharpeRatio     float64            `json:"sharpeRatio"`
	Diversification float64            `json:"diversification"` // 1 - avg(correlation)
}

// StrategyAllocation 单个策略的分配权重
type StrategyAllocation struct {
	HypothesisID   uint    `json:"hypothesisId"`
	Name           string  `json:"name"`
	Weight         float64 `json:"weight"`
	WinRate        float64 `json:"winRate"`
	AvgReturn      float64 `json:"avgReturn"`
	MaxDrawdown    float64 `json:"maxDrawdown"`
	SharpeRatio    float64 `json:"sharpeRatio"`
	Contribution   float64 `json:"contribution"` // 对组合收益的贡献
}

// PortfolioOptimizer 组合优化器
type PortfolioOptimizer struct {
	hypotheses []models.PredictionHypothesis
	returns    [][]float64 // 每个策略的日收益率序列
	names      []string
	ids        []uint
}

// NewPortfolioOptimizer 创建组合优化器
func NewPortfolioOptimizer(hypotheses []models.PredictionHypothesis) *PortfolioOptimizer {
	opt := &PortfolioOptimizer{
		hypotheses: hypotheses,
		returns:    make([][]float64, 0),
		names:      make([]string, 0),
		ids:        make([]uint, 0),
	}

	// 加载每个策略的日收益率
	for _, h := range hypotheses {
		if h.Status != "active" && h.Status != "paper_trade" {
			continue
		}
		var dailies []models.PredictionHypothesisDaily
		if db.Dao != nil {
			db.Dao.Where("hypothesis_id = ?", h.ID).Order("date asc").Find(&dailies)
		}
		if len(dailies) < 10 {
			continue
		}
		dailyReturns := make([]float64, 0, len(dailies)-1)
		for i := 1; i < len(dailies); i++ {
			if dailies[i-1].Nav > 0 {
				dailyReturns = append(dailyReturns, dailies[i].Nav/dailies[i-1].Nav-1)
			}
		}
		if len(dailyReturns) < 10 {
			continue
		}
		opt.returns = append(opt.returns, dailyReturns)
		opt.names = append(opt.names, h.Name)
		opt.ids = append(opt.ids, h.ID)
	}
	return opt
}

// Optimize 运行所有优化方法，返回多个分配方案
func (o *PortfolioOptimizer) Optimize() []PortfolioAllocation {
	if len(o.returns) < 2 {
		return nil
	}
	results := make([]PortfolioAllocation, 0, 4)

	// 1. 等权组合
	results = append(results, o.equalWeight())

	// 2. 风险平价
	results = append(results, o.riskParity())

	// 3. 最小方差
	results = append(results, o.minVariance())

	// 4. 最大夏普
	results = append(results, o.maxSharpe())

	return results
}

// equalWeight 等权分配
func (o *PortfolioOptimizer) equalWeight() PortfolioAllocation {
	n := len(o.returns)
	weight := 1.0 / float64(n)
	allocations := make([]StrategyAllocation, n)
	for i := 0; i < n; i++ {
		stats := o.strategyStats(i)
		allocations[i] = StrategyAllocation{
			HypothesisID: o.ids[i],
			Name:         o.names[i],
			Weight:       weight,
			WinRate:      stats.winRate,
			AvgReturn:    stats.avgReturn,
			MaxDrawdown:  stats.maxDrawdown,
			SharpeRatio:  stats.sharpe,
			Contribution: weight * stats.avgReturn,
		}
	}
	expRet, expRisk, sharpe := o.portfolioMetrics(allocations)
	return PortfolioAllocation{
		Method:         "equal_weight",
		Description:    "等权分配：每个策略分配相同权重，简单透明，适合策略数量较少且质量相近的场景",
		Allocations:    allocations,
		ExpectedReturn: expRet,
		ExpectedRisk:  expRisk,
		SharpeRatio:   sharpe,
		Diversification: o.diversificationScore(allocations),
	}
}

// riskParity 风险平价：每个策略的风险贡献相等
func (o *PortfolioOptimizer) riskParity() PortfolioAllocation {
	n := len(o.returns)
	vols := make([]float64, n)
	for i := 0; i < n; i++ {
		vols[i] = stdDev(o.returns[i]) * math.Sqrt(252)
		if vols[i] < 1e-10 {
			vols[i] = 0.01
		}
	}

	// 风险平价权重 = 1/vol_i / sum(1/vol_j)
	invVolSum := 0.0
	for i := 0; i < n; i++ {
		invVolSum += 1.0 / vols[i]
	}
	weights := make([]float64, n)
	for i := 0; i < n; i++ {
		weights[i] = (1.0 / vols[i]) / invVolSum
	}

	allocations := make([]StrategyAllocation, n)
	for i := 0; i < n; i++ {
		stats := o.strategyStats(i)
		allocations[i] = StrategyAllocation{
			HypothesisID: o.ids[i],
			Name:         o.names[i],
			Weight:       weights[i],
			WinRate:      stats.winRate,
			AvgReturn:    stats.avgReturn,
			MaxDrawdown:  stats.maxDrawdown,
			SharpeRatio:  stats.sharpe,
			Contribution: weights[i] * stats.avgReturn,
		}
	}
	expRet, expRisk, sharpe := o.portfolioMetrics(allocations)
	return PortfolioAllocation{
		Method:         "risk_parity",
		Description:    "风险平价：低波动策略获更高权重，各策略风险贡献均衡，适合追求稳健的组合",
		Allocations:    allocations,
		ExpectedReturn: expRet,
		ExpectedRisk:  expRisk,
		SharpeRatio:   sharpe,
		Diversification: o.diversificationScore(allocations),
	}
}

// minVariance 最小方差组合
func (o *PortfolioOptimizer) minVariance() PortfolioAllocation {
	n := len(o.returns)
	if n == 0 {
		return PortfolioAllocation{Method: "min_variance"}
	}

	// 简化版：用波动率倒数加权（严格版需要协方差矩阵求逆）
	// 对于本地 SQLite 量化工具，简化版已足够
	vols := make([]float64, n)
	for i := 0; i < n; i++ {
		vols[i] = stdDev(o.returns[i]) * math.Sqrt(252)
		if vols[i] < 1e-10 {
			vols[i] = 0.01
		}
	}

	// 考虑策略间相关性：降低高相关策略的权重
	covPenalty := make([]float64, n)
	for i := 0; i < n; i++ {
		avgCorr := 0.0
		count := 0
		for j := 0; j < n; j++ {
			if i != j {
				ax, ay := alignVectors(o.returns[i], o.returns[j])
				corr := pearsonCorrelation(ax, ay)
				avgCorr += math.Abs(corr)
				count++
			}
		}
		if count > 0 {
			avgCorr /= float64(count)
		}
		covPenalty[i] = 1.0 + avgCorr // 高相关性 → 高惩罚 → 低权重
	}

	invWeighted := 0.0
	for i := 0; i < n; i++ {
		invWeighted += 1.0 / (vols[i] * covPenalty[i])
	}
	weights := make([]float64, n)
	for i := 0; i < n; i++ {
		weights[i] = (1.0 / (vols[i] * covPenalty[i])) / invWeighted
	}

	allocations := make([]StrategyAllocation, n)
	for i := 0; i < n; i++ {
		stats := o.strategyStats(i)
		allocations[i] = StrategyAllocation{
			HypothesisID: o.ids[i],
			Name:         o.names[i],
			Weight:       weights[i],
			WinRate:      stats.winRate,
			AvgReturn:    stats.avgReturn,
			MaxDrawdown:  stats.maxDrawdown,
			SharpeRatio:  stats.sharpe,
			Contribution: weights[i] * stats.avgReturn,
		}
	}
	expRet, expRisk, sharpe := o.portfolioMetrics(allocations)
	return PortfolioAllocation{
		Method:         "min_variance",
		Description:    "最小方差：综合考虑波动率和策略相关性，降低高相关高波动策略的权重，追求最小组合波动",
		Allocations:    allocations,
		ExpectedReturn: expRet,
		ExpectedRisk:  expRisk,
		SharpeRatio:   sharpe,
		Diversification: o.diversificationScore(allocations),
	}
}

// maxSharpe 最大夏普组合
func (o *PortfolioOptimizer) maxSharpe() PortfolioAllocation {
	n := len(o.returns)
	if n == 0 {
		return PortfolioAllocation{Method: "max_sharpe"}
	}

	// 简化版：按夏普比率加权，同时惩罚高波动和高相关性
	sharpes := make([]float64, n)
	penalties := make([]float64, n)
	for i := 0; i < n; i++ {
		stats := o.strategyStats(i)
		sharpes[i] = stats.sharpe
		if sharpes[i] < 0 {
			sharpes[i] = 0
		}

		// 相关性惩罚
		avgCorr := 0.0
		count := 0
		for j := 0; j < n; j++ {
			if i != j {
				ax, ay := alignVectors(o.returns[i], o.returns[j])
				corr := pearsonCorrelation(ax, ay)
				avgCorr += math.Abs(corr)
				count++
			}
		}
		if count > 0 {
			avgCorr /= float64(count)
		}
		penalties[i] = 1.0 + avgCorr*0.5
	}

	scoreSum := 0.0
	for i := 0; i < n; i++ {
		scoreSum += sharpes[i] / penalties[i]
	}
	weights := make([]float64, n)
	if scoreSum > 0 {
		for i := 0; i < n; i++ {
			weights[i] = (sharpes[i] / penalties[i]) / scoreSum
		}
	} else {
		// 所有夏普为负时，退化为等权
		for i := 0; i < n; i++ {
			weights[i] = 1.0 / float64(n)
		}
	}

	allocations := make([]StrategyAllocation, n)
	for i := 0; i < n; i++ {
		stats := o.strategyStats(i)
		allocations[i] = StrategyAllocation{
			HypothesisID: o.ids[i],
			Name:         o.names[i],
			Weight:       weights[i],
			WinRate:      stats.winRate,
			AvgReturn:    stats.avgReturn,
			MaxDrawdown:  stats.maxDrawdown,
			SharpeRatio:  stats.sharpe,
			Contribution: weights[i] * stats.avgReturn,
		}
	}
	expRet, expRisk, sharpe := o.portfolioMetrics(allocations)
	return PortfolioAllocation{
		Method:         "max_sharpe",
		Description:    "最大夏普：高夏普低相关策略获更高权重，追求最优风险调整后收益",
		Allocations:    allocations,
		ExpectedReturn: expRet,
		ExpectedRisk:  expRisk,
		SharpeRatio:   sharpe,
		Diversification: o.diversificationScore(allocations),
	}
}

type strategyStats struct {
	winRate     float64
	avgReturn   float64
	maxDrawdown float64
	sharpe      float64
	volatility  float64
}

func (o *PortfolioOptimizer) strategyStats(idx int) strategyStats {
	returns := o.returns[idx]
	if len(returns) == 0 {
		return strategyStats{}
	}

	avgRet := meanFloat(returns)
	vol := stdDev(returns)
	annRet := avgRet * 252
	annVol := vol * math.Sqrt(252)

	// 胜率
	wins := 0
	for _, r := range returns {
		if r > 0 {
			wins++
		}
	}
	winRate := float64(wins) / float64(len(returns))

	// 最大回撤（简化：从日收益率累积）
	nav := 1.0
	peak := 1.0
	maxDD := 0.0
	for _, r := range returns {
		nav *= (1 + r)
		if nav > peak {
			peak = nav
		}
		dd := (peak - nav) / peak
		if dd > maxDD {
			maxDD = dd
		}
	}

	// 夏普
	sharpe := 0.0
	if annVol > 0 {
		sharpe = (annRet - 0.02) / annVol
	}

	return strategyStats{
		winRate:     winRate,
		avgReturn:   annRet,
		maxDrawdown: maxDD,
		sharpe:      sharpe,
		volatility:  annVol,
	}
}

// portfolioMetrics 计算组合层面的指标
func (o *PortfolioOptimizer) portfolioMetrics(allocations []StrategyAllocation) (expRet, expRisk, sharpe float64) {
	n := len(allocations)
	if n == 0 {
		return 0, 0, 0
	}

	weights := make([]float64, n)
	annRets := make([]float64, n)
	annVols := make([]float64, n)
	for i, a := range allocations {
		weights[i] = a.Weight
		annRets[i] = a.AvgReturn
		annVols[i] = 0
		for j := 0; j < len(o.returns[i]); j++ {
			annVols[i] += o.returns[i][j] * o.returns[i][j]
		}
		annVols[i] = math.Sqrt(annVols[i]/float64(len(o.returns[i]))) * math.Sqrt(252)
	}

	// 组合预期收益 = Σ w_i * r_i
	for i := 0; i < n; i++ {
		expRet += weights[i] * annRets[i]
	}

	// 组合风险 = sqrt(ΣΣ w_i * w_j * σ_i * σ_j * ρ_ij)
	variance := 0.0
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			corr := 0.0
			if i == j {
				corr = 1.0
			} else {
				ax, ay := alignVectors(o.returns[i], o.returns[j])
				corr = pearsonCorrelation(ax, ay)
			}
			variance += weights[i] * weights[j] * annVols[i] * annVols[j] * corr
		}
	}
	expRisk = math.Sqrt(variance)

	if expRisk > 0 {
		sharpe = (expRet - 0.02) / expRisk
	}

	return expRet, expRisk, sharpe
}

// diversificationScore 计算分散化得分 (1 - avg_correlation)
func (o *PortfolioOptimizer) diversificationScore(allocations []StrategyAllocation) float64 {
	n := len(allocations)
	if n < 2 {
		return 1.0
	}
	indices := make([]int, 0, n)
	for i, a := range allocations {
		if a.Weight > 0.01 {
			indices = append(indices, i)
		}
	}
	if len(indices) < 2 {
		return 1.0
	}
	totalCorr := 0.0
	count := 0
	for _, i := range indices {
		for _, j := range indices {
			if i >= j {
				continue
			}
			ax, ay := alignVectors(o.returns[i], o.returns[j])
			corr := pearsonCorrelation(ax, ay)
			totalCorr += corr
			count++
		}
	}
	if count == 0 {
		return 1.0
	}
	return 1.0 - totalCorr/float64(count)
}

// GetPortfolioOptimization 获取组合优化方案
func GetPortfolioOptimization(hypotheses []models.PredictionHypothesis) []PortfolioAllocation {
	opt := NewPortfolioOptimizer(hypotheses)
	return opt.Optimize()
}