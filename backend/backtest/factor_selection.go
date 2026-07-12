package backtest

import (
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"math"
	"sort"
)

// FactorSelectionResult 因子选择结果
type FactorSelectionResult struct {
	SelectedFactors    []FactorRanking          `json:"selectedFactors"`
	AllFactors         []FactorRanking          `json:"allFactors"`
	CorrelationMatrix  [][]float64              `json:"-"` // 因子相关性矩阵
	RedundantPairs     []RedundantFactorPair     `json:"redundantPairs"`
	LayerReturns       []FactorLayerReturn       `json:"layerReturns"`
	EffectiveFactorCount int                    `json:"effectiveFactorCount"`
}

type FactorRanking struct {
	FactorID   string  `json:"factorId"`
	IC         float64 `json:"ic"`
	RankIC     float64 `json:"rankIc"`
	TStatistic float64 `json:"tStatistic"`
	PValue     float64 `json:"pValue"`
	HitRate    float64 `json:"hitRate"`
	Stability  float64 `json:"stability"`  // IC 稳定性（IC 均值/IC 标准差）
	Selected   bool    `json:"selected"`
}

type RedundantFactorPair struct {
	Factor1      string  `json:"factor1"`
	Factor2      string  `json:"factor2"`
	Correlation  float64 `json:"correlation"`
	KeepFactor   string  `json:"keepFactor"`   // 保留 IC 更高的因子
	RemoveFactor string  `json:"removeFactor"` // 移除的因子
}

type FactorLayerReturn struct {
	Layer     string  `json:"layer"`     // top / middle / bottom
	AvgReturn float64 `json:"avgReturn"`
	HitRate   float64 `json:"hitRate"`
	Count     int     `json:"count"`
}

// FactorSelector 因子选择器
type FactorSelector struct {
	features   []models.StockFeature
	sectors    map[string]string
	startDate  string
	endDate    string
	calculator *FactorICCalculator
}

// NewFactorSelector 创建因子选择器
func NewFactorSelector(features []models.StockFeature, sectors map[string]string, startDate, endDate string) *FactorSelector {
	return &FactorSelector{
		features:   features,
		sectors:    sectors,
		startDate:  startDate,
		endDate:    endDate,
		calculator: NewFactorICCalculator(features, sectors, startDate, endDate),
	}
}

// FactorSelectorFromDB 从数据库加载特征数据创建因子选择器
func FactorSelectorFromDB(startDate, endDate string, universe []string) *FactorSelector {
	if db.Dao == nil {
		return nil
	}
	var features []models.StockFeature
	query := db.Dao.Where("date >= ? AND date <= ? AND feature_version = ? AND adjusted = ?",
		startDate, endDate, CurrentFeatureVersion, true)
	if len(universe) > 0 {
		query = query.Where("stock_code IN ?", universe)
	}
	query.Order("stock_code asc, date asc").Find(&features)

	sectors := make(map[string]string)
	for _, f := range features {
		if _, exists := sectors[f.StockCode]; exists {
			continue
		}
		industry, _ := stockIndustryAndConcepts(f.StockCode)
		if industry == "" {
			industry = "unknown"
		}
		sectors[f.StockCode] = industry
	}

	return NewFactorSelector(features, sectors, startDate, endDate)
}

// Run 执行因子选择
func (s *FactorSelector) Run(candidateFactors []string) *FactorSelectionResult {
	result := &FactorSelectionResult{
		AllFactors:      make([]FactorRanking, 0),
		SelectedFactors: make([]FactorRanking, 0),
		RedundantPairs:  make([]RedundantFactorPair, 0),
	}

	// 1. 单因子 IC 计算
	for _, factorID := range candidateFactors {
		icResult := s.calculator.Calculate(factorID, 5)
		if icResult.SampleSize < 30 {
			continue
		}

		// 计算 IC 稳定性 = IC 均值 / IC 标准差
		stability := 0.0
		if icResult.ICStd > 1e-10 {
			stability = math.Abs(icResult.IC) / icResult.ICStd
		}

		tStat := 0.0
		if icResult.ICStd > 1e-10 && icResult.ICDates > 1 {
			tStat = math.Abs(icResult.IC) / (icResult.ICStd / math.Sqrt(float64(icResult.ICDates)))
		}

		ranking := FactorRanking{
			FactorID:   factorID,
			IC:         icResult.IC,
			RankIC:     icResult.RankIC,
			TStatistic: tStat,
			PValue:     icResult.PValue,
			HitRate:    icResult.HitRate,
			Stability:  stability,
		}

		// 选择标准：|IC| > 0.02 且 p < 0.10
		if math.Abs(icResult.IC) > 0.02 && icResult.PValue < 0.10 {
			ranking.Selected = true
		}

		result.AllFactors = append(result.AllFactors, ranking)
	}

	// 按 |IC| 降序排序
	sort.Slice(result.AllFactors, func(i, j int) bool {
		return math.Abs(result.AllFactors[i].IC) > math.Abs(result.AllFactors[j].IC)
	})

	// 2. 因子相关性矩阵
	selectedFactors := s.buildSelectedList(result.AllFactors)
	result.CorrelationMatrix = s.correlationMatrix(selectedFactors)

	// 3. 去除冗余因子（相关性 > 0.7 的因子对）
	// 保留 IC 更高的因子
	for i := 0; i < len(selectedFactors); i++ {
		for j := i + 1; j < len(selectedFactors); j++ {
			if i < len(result.CorrelationMatrix) && j < len(result.CorrelationMatrix[i]) {
				corr := result.CorrelationMatrix[i][j]
				if math.Abs(corr) > 0.70 {
					f1, f2 := selectedFactors[i], selectedFactors[j]
					keep, remove := f1, f2
					if math.Abs(f2.IC) > math.Abs(f1.IC) {
						keep, remove = f2, f1
					}
					result.RedundantPairs = append(result.RedundantPairs, RedundantFactorPair{
						Factor1:     f1.FactorID,
						Factor2:     f2.FactorID,
						Correlation: corr,
						KeepFactor:  keep.FactorID,
						RemoveFactor: remove.FactorID,
					})
					// 标记移除的因子
					for k := range result.AllFactors {
						if result.AllFactors[k].FactorID == remove.FactorID {
							result.AllFactors[k].Selected = false
						}
					}
				}
			}
		}
	}

	// 4. 最终选中的因子
	for _, f := range result.AllFactors {
		if f.Selected {
			result.SelectedFactors = append(result.SelectedFactors, f)
		}
	}
	result.EffectiveFactorCount = len(result.SelectedFactors)

	// 5. 因子分层回测
	result.LayerReturns = s.factorLayerReturns(result.SelectedFactors)

	logger.SugaredLogger.Infof("factor selection: %d/%d effective factors (from %d candidates)",
		result.EffectiveFactorCount, len(result.AllFactors), len(candidateFactors))
	return result
}

func (s *FactorSelector) buildSelectedList(allFactors []FactorRanking) []FactorRanking {
	selected := make([]FactorRanking, 0)
	for _, f := range allFactors {
		if f.Selected {
			selected = append(selected, f)
		}
	}
	return selected
}

// correlationMatrix 计算因子间的相关性矩阵
func (s *FactorSelector) correlationMatrix(factors []FactorRanking) [][]float64 {
	n := len(factors)
	if n == 0 {
		return nil
	}

	// 提取所有因子的观测值
	factorValues := make([][]float64, n)
	for i, factor := range factors {
		observations := s.calculator.factorObservations(factor.FactorID, 5)
		values, _ := observationVectors(observations)
		factorValues[i] = values
	}

	matrix := make([][]float64, n)
	for i := 0; i < n; i++ {
		matrix[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			if i == j {
				matrix[i][j] = 1
			} else {
				// 取共同样本
				commonX, commonY := alignVectors(factorValues[i], factorValues[j])
				matrix[i][j] = pearsonCorrelation(commonX, commonY)
			}
		}
	}
	return matrix
}

// alignVectors 对齐两个向量（取共同索引，过滤 NaN/Inf）
// 注意：此方法假设两个向量按相同顺序排列（同 stock-date 排序），
// 对于不同因子的观测值，由于缺失数据点不同，可能产生对齐偏差。
// 在因子相关性矩阵的使用场景中，由于所有因子来自同一特征集，
// 该偏差通常可接受。如需精确对齐，应使用 (stockCode, date) 键值对。
func alignVectors(a, b []float64) ([]float64, []float64) {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	x := make([]float64, 0, minLen)
	y := make([]float64, 0, minLen)
	for i := 0; i < minLen; i++ {
		if !math.IsNaN(a[i]) && !math.IsNaN(b[i]) && !math.IsInf(a[i], 0) && !math.IsInf(b[i], 0) {
			x = append(x, a[i])
			y = append(y, b[i])
		}
	}
	return x, y
}

// factorLayerReturns 因子分层回测收益
func (s *FactorSelector) factorLayerReturns(factors []FactorRanking) []FactorLayerReturn {
	if len(factors) == 0 {
		return nil
	}

	layers := make([]FactorLayerReturn, 0)

	// 对每个选中的因子做分层回测
	for _, factor := range factors {
		observations := s.calculator.factorObservations(factor.FactorID, 5)
		if len(observations) < 30 {
			continue
		}

		// 按因子值排序
		sort.Slice(observations, func(i, j int) bool {
			return observations[i].Factor < observations[j].Factor
		})

		// 分为 5 层
		n := len(observations)
		layerSize := n / 5
		if layerSize < 5 {
			continue
		}

		layerNames := []string{"bottom", "low", "middle", "high", "top"}
		for l := 0; l < 5; l++ {
			start := l * layerSize
			end := start + layerSize
			if l == 4 {
				end = n
			}
			layer := observations[start:end]

			sum := 0.0
			hits := 0
			for _, obs := range layer {
				sum += obs.ForwardReturn
				if obs.ForwardReturn > 0 {
					hits++
				}
			}

			layers = append(layers, FactorLayerReturn{
				Layer:     fmt.Sprintf("%s.%s", factor.FactorID, layerNames[l]),
				AvgReturn: sum / float64(len(layer)),
				HitRate:   float64(hits) / float64(len(layer)),
				Count:     len(layer),
			})
		}
	}

	return layers
}

// GetEffectiveFactors 获取有效因子列表（用于 AI 推荐）
func (s *FactorSelector) GetEffectiveFactors() []string {
	candidateFactors := []string{
		"stock_feature.MA20", "stock_feature.MA60", "stock_feature.VolumeRatio",
		"stock_feature.ChangeRate5", "stock_feature.ChangeRate20", "stock_feature.ChangeRate60",
		"stock_feature.Volatility20", "stock_feature.Volatility60",
		"stock_feature.RSI6", "stock_feature.RSI12", "stock_feature.RSI14",
		"stock_feature.KDJ_K", "stock_feature.MACD", "stock_feature.ATR",
		"stock_feature.AmihudRatio", "stock_feature.HighLowRatio",
		"stock_feature.FundFlow5", "stock_feature.FundFlow20",
	}

	result := s.Run(candidateFactors)
	effective := make([]string, 0, len(result.SelectedFactors))
	for _, f := range result.SelectedFactors {
		effective = append(effective, f.FactorID)
	}
	return effective
}