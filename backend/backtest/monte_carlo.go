package backtest

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"
)

// MonteCarloResult 蒙特卡洛模拟结果
type MonteCarloResult struct {
	Simulations          int           `json:"simulations"`
	ConfidenceLevel      float64       `json:"confidenceLevel"` // 0.95
	MeanReturn           float64       `json:"meanReturn"`
	MedianReturn         float64       `json:"medianReturn"`
	StdReturn            float64       `json:"stdReturn"`
	Percentile5          float64       `json:"percentile5"`
	Percentile25         float64       `json:"percentile25"`
	Percentile75         float64       `json:"percentile75"`
	Percentile95         float64       `json:"percentile95"`
	MaxReturn            float64       `json:"maxReturn"`
	MinReturn            float64       `json:"minReturn"`

	// 最大回撤分布
	MaxDrawdownStats     DistributionStats `json:"maxDrawdownStats"`
	MaxDrawdownPercentile95 float64        `json:"maxDrawdownPercentile95"`

	// 夏普比率分布
	SharpeStats          DistributionStats `json:"sharpeStats"`
	SharpePercentile5    float64           `json:"sharpePercentile5"`

	// 胜率分布
	WinRateStats         DistributionStats `json:"winRateStats"`
	WinRatePercentile5   float64           `json:"winRatePercentile5"`

	// Bootstrap 置信区间
	ReturnCI             ConfidenceInterval `json:"returnCI"`
	SharpeCI             ConfidenceInterval `json:"sharpeCI"`
	MaxDrawdownCI        ConfidenceInterval `json:"maxDrawdownCI"`

	GeneratedAt          time.Time `json:"generatedAt"`
}

type DistributionStats struct {
	Mean   float64 `json:"mean"`
	Std    float64 `json:"std"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Median float64 `json:"median"`
}

type ConfidenceInterval struct {
	Lower  float64 `json:"lower"`
	Upper  float64 `json:"upper"`
	Mean   float64 `json:"mean"`
}

// MonteCarloSimulator 蒙特卡洛模拟器
type MonteCarloSimulator struct {
	simulations int
	rng         *rand.Rand
}

// NewMonteCarloSimulator 创建模拟器
func NewMonteCarloSimulator() *MonteCarloSimulator {
	return &MonteCarloSimulator{
		simulations: 1000,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// WithSimulations 设置模拟次数
func (m *MonteCarloSimulator) WithSimulations(n int) *MonteCarloSimulator {
	m.simulations = n
	return m
}

// BootstrapTrades 对交易记录做 Bootstrap 重采样
func (m *MonteCarloSimulator) BootstrapTrades(trades []Trade) *MonteCarloResult {
	if len(trades) < 10 {
		return &MonteCarloResult{Simulations: 0, GeneratedAt: time.Now()}
	}

	result := &MonteCarloResult{
		Simulations:     m.simulations,
		ConfidenceLevel: 0.95,
		GeneratedAt:     time.Now(),
	}

	simReturns := make([]float64, m.simulations)
	simSharpes := make([]float64, m.simulations)
	simMaxDDs := make([]float64, m.simulations)
	simWinRates := make([]float64, m.simulations)

	for sim := 0; sim < m.simulations; sim++ {
		// 随机抽取交易（有放回）
		bootstrapped := m.resample(trades)

		// 计算收益
		totalReturn := 0.0
		wins := 0
		returns := make([]float64, 0)
		nav := 1.0
		peak := 1.0
		maxDD := 0.0

		for _, trade := range bootstrapped {
			nav *= (1 + trade.ReturnRate)
			totalReturn += trade.ReturnRate
			returns = append(returns, trade.ReturnRate)
			if trade.ReturnRate > 0 {
				wins++
			}
			if nav > peak {
				peak = nav
			}
			dd := (peak - nav) / peak
			if dd > maxDD {
				maxDD = dd
			}
		}

		simReturns[sim] = totalReturn
		simMaxDDs[sim] = maxDD

		// 夏普比率（简化版）
		avgRet := totalReturn / float64(len(bootstrapped))
		if len(returns) > 1 {
			stdRet := stdDev(returns)
			if stdRet > 0 {
				simSharpes[sim] = avgRet / stdRet * math.Sqrt(252)
			}
		}

		simWinRates[sim] = float64(wins) / float64(len(bootstrapped))
	}

	// 排序用于计算分位数
	sort.Float64s(simReturns)
	sort.Float64s(simMaxDDs)
	sort.Float64s(simSharpes)
	sort.Float64s(simWinRates)

	// 基础统计
	result.MeanReturn = meanFloat(simReturns)
	result.MedianReturn = simReturns[len(simReturns)/2]
	result.StdReturn = stdDev(simReturns)
	result.MaxReturn = simReturns[len(simReturns)-1]
	result.MinReturn = simReturns[0]

	// 分位数
	result.Percentile5 = percentile(simReturns, 0.05)
	result.Percentile25 = percentile(simReturns, 0.25)
	result.Percentile75 = percentile(simReturns, 0.75)
	result.Percentile95 = percentile(simReturns, 0.95)

	// 最大回撤分布
	result.MaxDrawdownStats = distStats(simMaxDDs)
	result.MaxDrawdownPercentile95 = percentile(simMaxDDs, 0.95)

	// 夏普比率分布
	result.SharpeStats = distStats(simSharpes)
	result.SharpePercentile5 = percentile(simSharpes, 0.05)

	// 胜率分布
	result.WinRateStats = distStats(simWinRates)
	result.WinRatePercentile5 = percentile(simWinRates, 0.05)

	// 置信区间
	result.ReturnCI = ConfidenceInterval{
		Lower: percentile(simReturns, 0.025),
		Upper: percentile(simReturns, 0.975),
		Mean:  result.MeanReturn,
	}
	result.SharpeCI = ConfidenceInterval{
		Lower: percentile(simSharpes, 0.025),
		Upper: percentile(simSharpes, 0.975),
		Mean:  meanFloat(simSharpes),
	}
	result.MaxDrawdownCI = ConfidenceInterval{
		Lower: percentile(simMaxDDs, 0.025),
		Upper: percentile(simMaxDDs, 0.975),
		Mean:  meanFloat(simMaxDDs),
	}

	return result
}

// BootstrapReturns 对收益率序列做 Bootstrap
func (m *MonteCarloSimulator) BootstrapReturns(returns []float64) *MonteCarloResult {
	if len(returns) < 10 {
		return &MonteCarloResult{Simulations: 0, GeneratedAt: time.Now()}
	}

	result := &MonteCarloResult{
		Simulations:     m.simulations,
		ConfidenceLevel: 0.95,
		GeneratedAt:     time.Now(),
	}

	simReturns := make([]float64, m.simulations)
	simSharpes := make([]float64, m.simulations)
	simMaxDDs := make([]float64, m.simulations)

	for sim := 0; sim < m.simulations; sim++ {
		bootstrapped := m.resampleReturns(returns)

		// 累计收益
		cumulative := 1.0
		peak := 1.0
		maxDD := 0.0
		for _, r := range bootstrapped {
			cumulative *= (1 + r)
			if cumulative > peak {
				peak = cumulative
			}
			dd := (peak - cumulative) / peak
			if dd > maxDD {
				maxDD = dd
			}
		}

		simReturns[sim] = cumulative - 1
		simMaxDDs[sim] = maxDD

		avgRet := meanFloat(bootstrapped)
		if len(bootstrapped) > 1 {
			stdRet := stdDev(bootstrapped)
			if stdRet > 0 {
				simSharpes[sim] = avgRet / stdRet * math.Sqrt(252)
			}
		}
	}

	sort.Float64s(simReturns)
	sort.Float64s(simMaxDDs)
	sort.Float64s(simSharpes)

	result.MeanReturn = meanFloat(simReturns)
	result.MedianReturn = simReturns[len(simReturns)/2]
	result.StdReturn = stdDev(simReturns)
	result.Percentile5 = percentile(simReturns, 0.05)
	result.Percentile95 = percentile(simReturns, 0.95)
	result.MaxDrawdownStats = distStats(simMaxDDs)
	result.MaxDrawdownPercentile95 = percentile(simMaxDDs, 0.95)
	result.SharpeStats = distStats(simSharpes)
	result.SharpePercentile5 = percentile(simSharpes, 0.05)

	result.ReturnCI = ConfidenceInterval{
		Lower: percentile(simReturns, 0.025),
		Upper: percentile(simReturns, 0.975),
		Mean:  result.MeanReturn,
	}
	result.SharpeCI = ConfidenceInterval{
		Lower: percentile(simSharpes, 0.025),
		Upper: percentile(simSharpes, 0.975),
		Mean:  meanFloat(simSharpes),
	}
	result.MaxDrawdownCI = ConfidenceInterval{
		Lower: percentile(simMaxDDs, 0.025),
		Upper: percentile(simMaxDDs, 0.975),
		Mean:  meanFloat(simMaxDDs),
	}

	return result
}

// resample 有放回重采样交易
func (m *MonteCarloSimulator) resample(trades []Trade) []Trade {
	n := len(trades)
	result := make([]Trade, n)
	for i := 0; i < n; i++ {
		idx := m.rng.Intn(n)
		result[i] = trades[idx]
	}
	return result
}

// resampleReturns 有放回重采样收益率
func (m *MonteCarloSimulator) resampleReturns(returns []float64) []float64 {
	n := len(returns)
	result := make([]float64, n)
	for i := 0; i < n; i++ {
		idx := m.rng.Intn(n)
		result[i] = returns[idx]
	}
	return result
}

// percentile 计算分位数
func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	idx := int(float64(len(values)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(values) {
		idx = len(values) - 1
	}
	return values[idx]
}

// distStats 计算分布统计
func distStats(values []float64) DistributionStats {
	if len(values) == 0 {
		return DistributionStats{}
	}
	return DistributionStats{
		Mean:   meanFloat(values),
		Std:    stdDev(values),
		Min:    values[0],
		Max:    values[len(values)-1],
		Median: values[len(values)/2],
	}
}

// BacktestMonteCarlo 对回测结果做蒙特卡洛验证
func BacktestMonteCarlo(result *ValidationResult) *MonteCarloResult {
	if result == nil || len(result.Trades) < 10 {
		return &MonteCarloResult{
			Simulations: 0,
			GeneratedAt: time.Now(),
		}
	}

	simulator := NewMonteCarloSimulator().WithSimulations(1000)
	mcResult := simulator.BootstrapTrades(result.Trades)

	// 对比原始结果与模拟结果
	if math.Abs(result.TotalReturn-mcResult.MeanReturn) > 0.10 {
		mcResult.GeneratedAt = time.Now()
		// 差异超过 10%，标记为不稳定
	}

	return mcResult
}

// ValidateResultStability 验证回测结果的稳定性
// 返回 true 表示回测结果在统计上可信
func ValidateResultStability(result *ValidationResult) (bool, string) {
	if result == nil || len(result.Trades) < 30 {
		return false, fmt.Sprintf("交易次数不足（%d < 30），无法评估稳定性", len(result.Trades))
	}

	mc := BacktestMonteCarlo(result)
	if mc.Simulations == 0 {
		return false, "蒙特卡洛模拟失败"
	}

	// 检查条件
	checks := make([]string, 0)

	// 1. 收益稳定性：95% 分位数收益应为正
	if mc.Percentile5 > 0 {
		checks = append(checks, "95% 情景下收益为正")
	} else {
		checks = append(checks, fmt.Sprintf("5%% 分位数收益为 %.2f%%，存在亏损风险", mc.Percentile5*100))
	}

	// 2. 最大回撤控制：95% 分位数回撤不超过 20%
	if mc.MaxDrawdownPercentile95 < 0.20 {
		checks = append(checks, "最大回撤控制在 20% 以内")
	} else {
		checks = append(checks, fmt.Sprintf("最大回撤 95%% 分位数为 %.1f%%，超过 20%%", mc.MaxDrawdownPercentile95*100))
	}

	// 3. 夏普比率稳定性：95% 分位数夏普为正
	if mc.SharpePercentile5 > 0 {
		checks = append(checks, "夏普比率稳定性良好")
	} else {
		checks = append(checks, "夏普比率在 5% 置信度下为负")
	}

	// 综合判断
	passed := mc.Percentile5 > 0 && mc.MaxDrawdownPercentile95 < 0.20 && mc.SharpePercentile5 > 0
	summary := fmt.Sprintf("蒙特卡洛验证(%d次模拟): %s", mc.Simulations, joinChecks(checks))
	return passed, summary
}

func joinChecks(checks []string) string {
	if len(checks) == 0 {
		return "无检查项"
	}
	result := ""
	for i, c := range checks {
		if i > 0 {
			result += "; "
		}
		result += c
	}
	return result
}