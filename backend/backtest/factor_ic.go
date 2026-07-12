package backtest

import (
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"math"
	"sort"
	"strings"
	"time"
)

// FactorICCalculator 通用因子 IC 计算器，为所有因子类型提供真实的 IC/RankIC/SectorIC 计算。
// 替代 research_service.go 中零值 mock 数据，确保 AI 基于真实因子证据生成策略。
type FactorICCalculator struct {
	features    []models.StockFeature
	sectors     map[string]string
	startDate   string
	endDate     string
	featureAsOf map[string]time.Time
}

// NewFactorICCalculator 创建因子 IC 计算器
func NewFactorICCalculator(features []models.StockFeature, sectors map[string]string, startDate, endDate string) *FactorICCalculator {
	calc := &FactorICCalculator{
		features:  features,
		sectors:   sectors,
		startDate: startDate,
		endDate:   endDate,
	}
	calc.buildFeatureAsOf()
	return calc
}

func (c *FactorICCalculator) buildFeatureAsOf() {
	c.featureAsOf = make(map[string]time.Time, len(c.features))
	for _, f := range c.features {
		asOf := f.DataAsOf
		if asOf.IsZero() {
			asOf = featureDataAsOf(f.Date)
		}
		c.featureAsOf[f.Date+"|"+NormalizeCandidateStockCode(f.StockCode)] = asOf
	}
}

func (c *FactorICCalculator) pointInTimeAvailable(key string, availableAt time.Time) bool {
	asOf, ok := c.featureAsOf[key]
	return ok && (availableAt.IsZero() || !availableAt.After(asOf))
}

// FactorICResult 统一的因子 IC 计算结果
type FactorICResult struct {
	FactorID   string
	SampleSize int
	IC         float64
	RankIC     float64
	ICStd      float64
	ICDates    int
	HitRate    float64
	PValue     float64
	SectorIC   float64
	SectorICStd float64
	TimeDecay  string
	RecentIC   float64
	AvgForwardReturn float64
}

// Calculate 计算单个因子的完整 IC 指标
func (c *FactorICCalculator) Calculate(factorID string, horizon int) FactorICResult {
	if horizon <= 0 {
		horizon = 5
	}
	result := FactorICResult{FactorID: factorID}
	observations := c.factorObservations(factorID, horizon)
	result.SampleSize = len(observations)
	if len(observations) < 3 {
		return result
	}

	factors, returns := observationVectors(observations)
	result.IC, result.RankIC, result.ICStd, result.ICDates, result.PValue = c.dailyCrossSectionalCorrelation(observations)
	result.AvgForwardReturn = meanFloat(returns)
	factorMean := meanFloat(factors)
	returnMean := meanFloat(returns)
	hits := 0
	for i := range factors {
		if (factors[i]-factorMean)*(returns[i]-returnMean) > 0 {
			hits++
		}
	}
	result.HitRate = float64(hits) / float64(len(observations))

	recent := recentObservations(observations, 0.30)
	result.RecentIC, _, _, _, _ = c.dailyCrossSectionalCorrelation(recent)
	result.SectorIC, result.SectorICStd = c.sectorCorrelationStats(observations)

	if math.Abs(result.RecentIC) < math.Abs(result.IC)*0.75 {
		result.TimeDecay = "decaying"
	} else {
		result.TimeDecay = "stable"
	}
	return result
}

// ToResearchEvidence 将 IC 结果转换为 ResearchFactorEvidence
func (r FactorICResult) ToResearchEvidence(startDate, endDate string, horizon int) ResearchFactorEvidence {
	return ResearchFactorEvidence{
		StartDate:        startDate,
		EndDate:          endDate,
		Horizon:          horizon,
		SampleSize:       r.SampleSize,
		IC:               r.IC,
		RankIC:           r.RankIC,
		HitRate:          r.HitRate,
		AvgForwardReturn: r.AvgForwardReturn,
		PValue:           r.PValue,
		SectorIC:         r.SectorIC,
		SectorICStd:      r.SectorICStd,
		TimeDecay:        r.TimeDecay,
		RecentIC:         r.RecentIC,
	}
}

func (c *FactorICCalculator) factorObservations(factorID string, horizon int) []researchObservation {
	byStock := make(map[string][]models.StockFeature)
	for _, f := range c.features {
		byStock[f.StockCode] = append(byStock[f.StockCode], f)
	}

	result := make([]researchObservation, 0, len(c.features))
	engine := NewStrategyEngine()
	name := canonicalIndicatorName("", factorID)
	externalValues := c.loadExternalFactorValues(factorID)

	for stockCode, features := range byStock {
		for i := 0; i+horizon < len(features); i++ {
			current := features[i]
			future := features[i+horizon]
			if current.Close <= 0 || future.Close <= 0 {
				continue
			}

			var factor float64
			var exists bool
			if strings.HasPrefix(factorID, "stock_feature.") {
				factor = engine.GetIndicatorValue(name, current)
				exists = true
			} else {
				factor, exists = externalValues[current.Date+"|"+NormalizeCandidateStockCode(stockCode)]
			}
			if !exists {
				continue
			}
			if (strings.Contains(factorID, "FundFlow") || strings.Contains(factorID, "Volume")) && factor == 0 {
				continue
			}

			result = append(result, researchObservation{
				Date:          current.Date,
				StockCode:     stockCode,
				Sector:        c.sectors[stockCode],
				Factor:        factor,
				ForwardReturn: future.Close/current.Close - 1,
			})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result
}

// loadExternalFactorValues 加载外部因子值（非 stock_feature 命名空间）
func (c *FactorICCalculator) loadExternalFactorValues(factorID string) map[string]float64 {
	result := map[string]float64{}
	if db.Dao == nil || len(c.features) == 0 {
		return result
	}

	switch {
	case strings.HasPrefix(factorID, "money_flow."):
		c.loadMoneyFlowValues(factorID, result)
	case strings.HasPrefix(factorID, "market."):
		c.loadMarketFactorValues(factorID, result)
	case strings.HasPrefix(factorID, "sector."):
		c.loadSectorFlowValues(factorID, result)
	case strings.HasPrefix(factorID, "event."):
		c.loadEventValues(factorID, result)
	case strings.HasPrefix(factorID, "screening."):
		c.loadScreeningValues(factorID, result)
	case strings.HasPrefix(factorID, "limitup."):
		c.loadLimitUpValues(factorID, result)
	case strings.HasPrefix(factorID, "recommendation."):
		c.loadRecommendationValues(factorID, result)
	case strings.HasPrefix(factorID, "candidate."):
		c.loadCandidateValues(factorID, result)
	}
	return result
}

func (c *FactorICCalculator) loadMoneyFlowValues(factorID string, result map[string]float64) {
	var rows []models.StockMoneyFlowDaily
	db.Dao.Where("trade_date >= ? AND trade_date <= ?", c.startDate, c.endDate).Order("trade_date asc").Find(&rows)
	for _, row := range rows {
		key := row.TradeDate + "|" + NormalizeCandidateStockCode(row.StockCode)
		value := row.MainNetInflow1
		switch factorID {
		case "money_flow.MainNetInflow5":
			value = row.MainNetInflow5
		case "money_flow.MainNetInflow20":
			value = row.MainNetInflow20
		case "money_flow.SuperLargeNetInflow1":
			value = row.SuperLargeNet1
		case "money_flow.LargeNetInflow1":
			value = row.LargeNet1
		case "money_flow.RetailNetInflow1":
			value = row.RetailNetInflow1
		case "money_flow.NorthboundNetInflow1":
			value = row.NorthboundNetIn
		}
		result[key] = value
	}
}

func (c *FactorICCalculator) loadMarketFactorValues(factorID string, result map[string]float64) {
	var rows []models.MarketFactorDaily
	db.Dao.Where("trade_date >= ? AND trade_date <= ?", c.startDate, c.endDate).Order("trade_date asc").Find(&rows)
	for _, row := range rows {
		key := row.TradeDate + "|" + "market" // 大盘因子不区分股票
		value := row.SentimentScore
		switch factorID {
		case "market.UpCount":
			value = float64(row.UpCount)
		case "market.DownCount":
			value = float64(row.DownCount)
		case "market.LimitUpCount":
			value = float64(row.LimitUpCount)
		case "market.LimitDownCount":
			value = float64(row.LimitDownCount)
		case "market.SentimentScore":
			value = row.SentimentScore
		}
		result[key] = value
	}
}

func (c *FactorICCalculator) loadSectorFlowValues(factorID string, result map[string]float64) {
	// SectorFlowDaily 是 per-sector 而非 per-stock，通过股票行业/概念映射 join。
	// 当前仅支持 sector.NetInflow 一个因子；后续扩展 (NetInflow5/NetInflow20)
	// 需要 SectorFlowDaily 模型增加对应字段及 switch 分支。
	// SectorFlowDaily is per-sector, not per-stock, so we need to join via stock industry/concept
	// Build a map of stock->sector_name for the feature set
	stockSectors := c.resolveStockSectors()

	// Build a map of (date, sector_name) -> net_inflow
	var rows []models.SectorFlowDaily
	db.Dao.Where("trade_date >= ? AND trade_date <= ?", c.startDate, c.endDate).Order("trade_date asc").Find(&rows)
	// For each stock, find its sector flow
	for stockCode, sectorInfo := range stockSectors {
		for _, row := range rows {
			key := row.TradeDate + "|" + stockCode
			matched := false
			if row.SectorType == "industry" && row.SectorName == sectorInfo.industry {
				matched = true
			} else if row.SectorType == "concept" && sectorInfo.concepts[row.SectorName] {
				matched = true
			}
			if matched {
				result[key] = row.NetInflow
			}
		}
	}
}

type stockSectorInfo struct {
	industry string
	concepts map[string]bool
}

func (c *FactorICCalculator) resolveStockSectors() map[string]stockSectorInfo {
	out := make(map[string]stockSectorInfo, len(c.sectors)+len(c.features)/10)
	// First, use pre-loaded sectors from research_service
	for stockCode, sector := range c.sectors {
		info := out[stockCode]
		info.industry = sector
		info.concepts = map[string]bool{}
		out[stockCode] = info
	}
	// Then, resolve missing stocks
	for _, f := range c.features {
		normalized := NormalizeCandidateStockCode(f.StockCode)
		if _, exists := out[normalized]; exists {
			continue
		}
		industry, concepts := stockIndustryAndConcepts(f.StockCode)
		conceptMap := make(map[string]bool, len(concepts))
		for _, concept := range concepts {
			conceptMap[concept] = true
		}
		out[normalized] = stockSectorInfo{industry: industry, concepts: conceptMap}
	}
	return out
}

func (c *FactorICCalculator) loadEventValues(factorID string, result map[string]float64) {
	var rows []models.StockEventDaily
	db.Dao.Where("trade_date >= ? AND trade_date <= ? AND feature_version = ?", c.startDate, c.endDate, CurrentFeatureVersion).Find(&rows)
	for _, row := range rows {
		key := row.TradeDate + "|" + NormalizeCandidateStockCode(row.StockCode)
		if !c.pointInTimeAvailable(key, row.AvailableAt) {
			continue
		}
		value := float64(row.ChangeEventCount)
		switch factorID {
		case "event.HasLargeBuy":
			value = boolFloat(row.HasLargeBuy)
		case "event.HasRapidRise":
			value = boolFloat(row.HasRapidRise)
		case "event.HasLimitUp":
			value = boolFloat(row.HasLimitUp)
		case "event.HasLimitDown":
			value = boolFloat(row.HasLimitDown)
		case "event.ChangeEventCount":
			value = float64(row.ChangeEventCount)
		}
		result[key] = value
	}
}

func (c *FactorICCalculator) loadScreeningValues(factorID string, result map[string]float64) {
	var rows []models.StockScreeningFactDaily
	db.Dao.Where("trade_date >= ? AND trade_date <= ? AND feature_version = ? AND calculated = ?", c.startDate, c.endDate, CurrentFeatureVersion, true).Find(&rows)
	for _, row := range rows {
		key := row.TradeDate + "|" + NormalizeCandidateStockCode(row.StockCode)
		if !c.pointInTimeAvailable(key, row.AvailableAt) {
			continue
		}
		value := float64(row.PatternCount)
		switch factorID {
		case "screening.MACDGoldenCross":
			value = boolFloat(row.MACDGoldenCross)
		case "screening.KDJGoldenCross":
			value = boolFloat(row.KDJGoldenCross)
		case "screening.MABullish":
			value = boolFloat(row.MABullish)
		case "screening.MABearish":
			value = boolFloat(row.MABearish)
		case "screening.BollBreakout":
			value = boolFloat(row.BollBreakout)
		case "screening.VolumeBreakout":
			value = boolFloat(row.VolumeBreakout)
		case "screening.Oversold":
			value = boolFloat(row.Oversold)
		case "screening.Overbought":
			value = boolFloat(row.Overbought)
		}
		result[key] = value
	}
}

func (c *FactorICCalculator) loadLimitUpValues(factorID string, result map[string]float64) {
	var rows []models.UplimitStockDaily
	db.Dao.Where("trade_date >= ? AND trade_date <= ?", c.startDate, c.endDate).Order("available_at asc").Find(&rows)
	latestAt := map[string]time.Time{}
	for _, row := range rows {
		key := row.TradeDate + "|" + NormalizeCandidateStockCode(row.StockCode)
		if !c.pointInTimeAvailable(key, row.AvailableAt) || (!latestAt[key].IsZero() && row.AvailableAt.Before(latestAt[key])) {
			continue
		}
		value := float64(row.KeepTimes)
		switch factorID {
		case "limitup.SealRatioClose":
			value = row.SealRatioClose
		case "limitup.ExplodedCount":
			value = float64(row.ExplodeCount)
			if row.Exploded && value == 0 {
				value = 1
			}
		case "limitup.PlateHeat":
			value = float64(row.PlateHeat)
		case "limitup.PlateLimitCount":
			value = float64(row.PlateLimitCount)
		}
		latestAt[key] = row.AvailableAt
		result[key] = value
	}
}

func (c *FactorICCalculator) loadRecommendationValues(factorID string, result map[string]float64) {
	var rows []models.ModelRecommendationEvent
	db.Dao.Where("trade_date >= ? AND trade_date <= ? AND independent_for_research = ?", c.startDate, c.endDate, true).Find(&rows)
	modelsByKey := map[string]map[string]bool{}
	counts := map[string]int{}
	for _, row := range rows {
		key := row.TradeDate + "|" + NormalizeCandidateStockCode(row.StockCode)
		if !c.pointInTimeAvailable(key, row.AvailableAt) {
			continue
		}
		counts[key]++
		if modelsByKey[key] == nil {
			modelsByKey[key] = map[string]bool{}
		}
		modelsByKey[key][row.ModelName] = true
	}
	for key, count := range counts {
		switch factorID {
		case "recommendation.ModelCount":
			result[key] = float64(len(modelsByKey[key]))
		default:
			result[key] = float64(count)
		}
	}
}

func (c *FactorICCalculator) loadCandidateValues(factorID string, result map[string]float64) {
	// candidate 命名空间因子来自外部候选数据源，需要查看 candidate_source_fact 表
	var facts []models.CandidateSourceFact
	db.Dao.Where("available_at >= ? AND available_at <= ?", c.startDate, c.endDate).Order("available_at asc").Find(&facts)
	for _, fact := range facts {
		key := fact.AvailableAt.Format("2006-01-02") + "|" + NormalizeCandidateStockCode(fact.StockCode)
		switch factorID {
		case "candidate.northbound_flow":
			// 从事实数据中提取北向资金流
			if flow, ok := fact.FactFloat("northbound_flow"); ok {
				result[key] = flow
			}
		case "candidate.news_sentiment":
			if sentiment, ok := fact.FactFloat("news_sentiment"); ok {
				result[key] = sentiment
			}
		case "candidate.announcement_risk":
			if risk, ok := fact.FactFloat("announcement_risk"); ok {
				result[key] = risk
			}
		}
	}
	if len(result) == 0 {
		logger.SugaredLogger.Debugf("factor_ic: no candidate data for %s in range [%s, %s]", factorID, c.startDate, c.endDate)
	}
}

// dailyCrossSectionalCorrelation 计算日截面的 IC 均值、RankIC 均值、IC 标准差、IC 日期数、P 值
func (c *FactorICCalculator) dailyCrossSectionalCorrelation(observations []researchObservation) (icMean, rankICMean, icStd float64, icDates int, pValue float64) {
	byDate := map[string][]researchObservation{}
	for _, o := range observations {
		byDate[o.Date] = append(byDate[o.Date], o)
	}
	ics, rankICs := make([]float64, 0, len(byDate)), make([]float64, 0, len(byDate))
	for _, rows := range byDate {
		if len(rows) < 3 {
			continue
		}
		factors, returns := observationVectors(rows)
		ics = append(ics, pearsonCorrelation(factors, returns))
		rankICs = append(rankICs, pearsonCorrelation(rankValues(factors), rankValues(returns)))
	}
	icDates = len(ics)
	if len(ics) == 0 {
		return 0, 0, 0, 0, 1
	}
	meanIC := meanFloat(ics)
	meanRankIC := meanFloat(rankICs)
	if len(ics) < 2 {
		return meanIC, meanRankIC, 0, icDates, 1
	}
	variance := 0.0
	for _, ic := range ics {
		variance += (ic - meanIC) * (ic - meanIC)
	}
	std := math.Sqrt(variance / float64(len(ics)-1))
	if std == 0 {
		if meanIC == 0 {
			return meanIC, meanRankIC, std, icDates, 1
		}
		return meanIC, meanRankIC, std, icDates, 0
	}
	t := math.Abs(meanIC) / (std / math.Sqrt(float64(len(ics))))
	return meanIC, meanRankIC, std, icDates, math.Erfc(t / math.Sqrt2)
}

func (c *FactorICCalculator) sectorCorrelationStats(observations []researchObservation) (float64, float64) {
	groups := make(map[string][]researchObservation)
	for _, o := range observations {
		groups[o.Sector] = append(groups[o.Sector], o)
	}
	ics := make([]float64, 0, len(groups))
	for _, group := range groups {
		if len(group) < 10 {
			continue
		}
		factors, returns := observationVectors(group)
		ics = append(ics, pearsonCorrelation(factors, returns))
	}
	if len(ics) == 0 {
		return 0, 0
	}
	meanIC := meanFloat(ics)
	variance := 0.0
	for _, ic := range ics {
		variance += (ic - meanIC) * (ic - meanIC)
	}
	if len(ics) < 2 {
		return meanIC, 0
	}
	return meanIC, math.Sqrt(variance / float64(len(ics)-1))
}

// boolFloat is defined in unified_feature_view.go