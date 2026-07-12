package backtest

import (
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"math"
	"sort"
)

// FactorPreprocessor 因子预处理管道：去极值、标准化、行业中性化、缺失值填充
type FactorPreprocessor struct {
	winsorizeLower float64
	winsorizeUpper float64
}

// NewFactorPreprocessor 创建因子预处理器
func NewFactorPreprocessor() *FactorPreprocessor {
	return &FactorPreprocessor{
		winsorizeLower: 0.01,
		winsorizeUpper: 0.99,
	}
}

// PreprocessedFactors 预处理后的因子集
type PreprocessedFactors struct {
	FactorID      string             `json:"factorId"`
	RawValues     []float64          `json:"-"` // 原始值
	Winsorized    []float64          `json:"-"` // 去极值后
	Standardized  []float64          `json:"-"` // 标准化后
	Neutralized   []float64          `json:"-"` // 行业中性化后
	Stats         FactorStats        `json:"stats"`
	OutlierCount  int                `json:"outlierCount"`
	MissingCount  int                `json:"missingCount"`
}

// FactorStats 因子统计信息
type FactorStats struct {
	Count    int     `json:"count"`
	Mean     float64 `json:"mean"`
	Std      float64 `json:"std"`
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	Skewness float64 `json:"skewness"`
	Kurtosis float64 `json:"kurtosis"`
}

// Preprocess 执行完整的因子预处理管道
func (p *FactorPreprocessor) Preprocess(factorID string, values []float64, industries []string) *PreprocessedFactors {
	if len(values) == 0 {
		return &PreprocessedFactors{FactorID: factorID, Stats: FactorStats{Count: 0}}
	}

	result := &PreprocessedFactors{
		FactorID:  factorID,
		RawValues: make([]float64, len(values)),
	}
	copy(result.RawValues, values)

	// 统计缺失值
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			result.MissingCount++
		}
	}

	// 1. 去极值（Winsorize）
	result.Winsorized = p.winsorize(values)
	result.OutlierCount = p.countOutliers(values, result.Winsorized)

	// 2. 标准化（Z-Score）
	result.Standardized = p.zscore(result.Winsorized)

	// 3. 行业中性化（如果提供了行业信息）
	if len(industries) == len(values) {
		result.Neutralized = p.neutralizeIndustry(result.Winsorized, industries)
	} else {
		result.Neutralized = make([]float64, len(values))
		copy(result.Neutralized, result.Winsorized)
	}

	// 计算统计信息
	result.Stats = p.computeStats(result.Winsorized)

	return result
}

// winsorize 去极值处理：将超过上下分位数的值替换为分位数值
func (p *FactorPreprocessor) winsorize(values []float64) []float64 {
	if len(values) < 10 {
		result := make([]float64, len(values))
		copy(result, values)
		return result
	}

	// 排序以计算分位数
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	lowerIdx := int(float64(len(sorted)-1) * p.winsorizeLower)
	upperIdx := int(float64(len(sorted)-1) * p.winsorizeUpper)
	lowerBound := sorted[lowerIdx]
	upperBound := sorted[upperIdx]

	result := make([]float64, len(values))
	for i, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			result[i] = lowerBound // 将无效值替换为下界
			continue
		}
		if v < lowerBound {
			result[i] = lowerBound
		} else if v > upperBound {
			result[i] = upperBound
		} else {
			result[i] = v
		}
	}
	return result
}

// countOutliers 统计被去极值处理的值数量
func (p *FactorPreprocessor) countOutliers(original, winsorized []float64) int {
	count := 0
	for i := range original {
		if math.Abs(original[i]-winsorized[i]) > 1e-10 {
			count++
		}
	}
	return count
}

// zscore Z-Score 标准化
func (p *FactorPreprocessor) zscore(values []float64) []float64 {
	if len(values) < 2 {
		result := make([]float64, len(values))
		copy(result, values)
		return result
	}

	mean := p.mean(values)
	std := p.std(values)
	if std < 1e-10 {
		// 标准差接近零，所有值相同，返回零向量
		result := make([]float64, len(values))
		return result
	}

	result := make([]float64, len(values))
	for i, v := range values {
		result[i] = (v - mean) / std
	}
	return result
}

// neutralizeIndustry 行业中性化：减去行业均值
func (p *FactorPreprocessor) neutralizeIndustry(values []float64, industries []string) []float64 {
	if len(values) != len(industries) || len(values) < 2 {
		result := make([]float64, len(values))
		copy(result, values)
		return result
	}

	// 按行业分组计算均值
	industryMeans := make(map[string]float64)
	industryCounts := make(map[string]int)
	for i, industry := range industries {
		if industry == "" {
			industry = "unknown"
		}
		industryMeans[industry] += values[i]
		industryCounts[industry]++
	}
	for industry := range industryMeans {
		if industryCounts[industry] > 0 {
			industryMeans[industry] /= float64(industryCounts[industry])
		}
	}

	// 减去行业均值
	result := make([]float64, len(values))
	for i, v := range values {
		industry := industries[i]
		if industry == "" {
			industry = "unknown"
		}
		mean := industryMeans[industry]
		result[i] = v - mean
	}

	return result
}

// computeStats 计算因子统计信息
func (p *FactorPreprocessor) computeStats(values []float64) FactorStats {
	stats := FactorStats{Count: len(values)}
	if len(values) == 0 {
		return stats
	}

	stats.Mean = p.mean(values)
	stats.Std = p.std(values)
	stats.Min = values[0]
	stats.Max = values[0]

	for _, v := range values {
		if v < stats.Min {
			stats.Min = v
		}
		if v > stats.Max {
			stats.Max = v
		}
	}

	// 偏度
	if stats.Std > 1e-10 {
		n := float64(len(values))
		sum := 0.0
		for _, v := range values {
			sum += math.Pow((v-stats.Mean)/stats.Std, 3)
		}
		stats.Skewness = sum / n
	}

	// 峰度
	if stats.Std > 1e-10 {
		n := float64(len(values))
		sum := 0.0
		for _, v := range values {
			sum += math.Pow((v-stats.Mean)/stats.Std, 4)
		}
		stats.Kurtosis = sum/n - 3 // 超额峰度
	}

	return stats
}

func (p *FactorPreprocessor) mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func (p *FactorPreprocessor) std(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	mean := p.mean(values)
	sum := 0.0
	for _, v := range values {
		sum += (v - mean) * (v - mean)
	}
	return math.Sqrt(sum / float64(len(values)-1))
}

// PreprocessFeatures 批量预处理特征（在同步完成后调用）
func PreprocessFeatures(stockCodes []string) *PreprocessResult {
	processor := NewFactorPreprocessor()
	result := &PreprocessResult{
		FactorStats: make(map[string]FactorStats),
		ProcessedAt: shanghaiNow().Format("2006-01-02 15:04:05"),
	}

	if db.Dao == nil {
		result.Error = "数据库未初始化"
		return result
	}

	// 获取特征数据
	var features []models.StockFeature
	query := db.Dao.Where("feature_version = ? AND adjusted = ?", CurrentFeatureVersion, true)
	if len(stockCodes) > 0 {
		query = query.Where("stock_code IN ?", stockCodes)
	}
	query.Order("stock_code asc, date asc").Find(&features)

	if len(features) == 0 {
		result.Error = "没有特征数据"
		return result
	}

	// 提取所有因子值
	factorIDs := []string{
		"stock_feature.MA20", "stock_feature.VolumeRatio", "stock_feature.ChangeRate20",
		"stock_feature.Volatility20", "stock_feature.RSI14", "stock_feature.ATR",
		"stock_feature.AmihudRatio", "stock_feature.ChangeRate60",
		"stock_feature.MACD", "stock_feature.RSI6", "stock_feature.KDJ_K",
		"stock_feature.ChangeRate5", "stock_feature.FundFlow5", "stock_feature.FundFlow20",
	}

	// 收集行业信息
	industries := make([]string, len(features))
	industryCache := make(map[string]string)
	for i, f := range features {
		if industry, exists := industryCache[f.StockCode]; exists {
			industries[i] = industry
		} else {
			industry, _ := stockIndustryAndConcepts(f.StockCode)
			if industry == "" {
				industry = "unknown"
			}
			industryCache[f.StockCode] = industry
			industries[i] = industry
		}
	}

	engine := NewStrategyEngine()

	// 按日期分组，按日期截面进行预处理，避免跨时间前视偏差
	byDate := make(map[string][]int) // date -> feature indices
	for i, f := range features {
		byDate[f.Date] = append(byDate[f.Date], i)
	}

	// 对每个因子在每个日期截面上进行预处理
	dateFactorStats := make(map[string]map[string]FactorStats) // date -> factorID -> stats
	for date, indices := range byDate {
		if len(indices) < 3 {
			continue
		}
		dateValues := make([]float64, len(indices))
		dateIndustries := make([]string, len(indices))
		for j, idx := range indices {
			dateIndustries[j] = industries[idx]
		}

		for _, factorID := range factorIDs {
			for j, idx := range indices {
				dateValues[j] = engine.GetIndicatorValue(canonicalIndicatorName("", factorID), features[idx])
			}
			pp := processor.Preprocess(factorID, dateValues, dateIndustries)
			if dateFactorStats[factorID] == nil {
				dateFactorStats[factorID] = make(map[string]FactorStats)
			}
			dateFactorStats[factorID][date] = pp.Stats
		}
	}

	// 汇总跨日期的统计信息
	for _, factorID := range factorIDs {
		allCounts := 0
		allMeans := 0.0
		allStds := 0.0
		allSkew := 0.0
		allKurt := 0.0
		dateCount := 0
		for _, stats := range dateFactorStats[factorID] {
			allCounts += stats.Count
			allMeans += stats.Mean
			allStds += stats.Std
			allSkew += stats.Skewness
			allKurt += stats.Kurtosis
			dateCount++
		}
		if dateCount > 0 {
			dn := float64(dateCount)
			aggStats := FactorStats{
				Count:    allCounts,
				Mean:     allMeans / dn,
				Std:      allStds / dn,
				Skewness: allSkew / dn,
				Kurtosis: allKurt / dn,
			}
			result.FactorStats[factorID] = aggStats
			result.TotalFactors++
			result.TotalObservations += allCounts
			logger.SugaredLogger.Debugf("factor %s: dates=%d observations=%d mean=%.4f std=%.4f",
				factorID, dateCount, aggStats.Count, aggStats.Mean, aggStats.Std)
		}
	}

	logger.SugaredLogger.Infof("preprocess: %d factors, %d observations for %d stocks",
		result.TotalFactors, result.TotalObservations, len(features))

	result.Success = true
	return result
}

// PreprocessResult 预处理结果
type PreprocessResult struct {
	FactorStats       map[string]FactorStats `json:"factorStats"`
	TotalFactors      int                    `json:"totalFactors"`
	TotalObservations int                    `json:"totalObservations"`
	ProcessedAt       string                 `json:"processedAt"`
	Success           bool                   `json:"success"`
	Error             string                 `json:"error,omitempty"`
}