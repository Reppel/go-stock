package backtest

import (
	"encoding/json"
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"math"
	"sort"
	"strings"
	"time"
)

const ResearchIdeaSchemaV2 = "research-idea/v2"

type ResearchTemplate string

const (
	ResearchTemplateFactorScan         ResearchTemplate = "factor_effectiveness_scan"
	ResearchTemplateSectorRotation     ResearchTemplate = "sector_rotation_detection"
	ResearchTemplateMoneyFlowAnomaly   ResearchTemplate = "money_flow_anomaly_detection"
	ResearchTemplateFailureAttribution ResearchTemplate = "failure_sample_attribution"
	ResearchTemplateExternalCandidate  ResearchTemplate = "external_data_source_candidate"
	ResearchTemplateEventEffectiveness ResearchTemplate = "event_effectiveness_scan"
	ResearchTemplateLimitUpStrength    ResearchTemplate = "limitup_strength_scan"
	ResearchTemplateScreeningConsensus ResearchTemplate = "screening_consensus_scan"
	ResearchTemplateRecommendationMeta ResearchTemplate = "recommendation_meta_scan"
)

type ResearchService struct {
	features []models.StockFeature
	sectors  map[string]string
}

func NewResearchService() *ResearchService {
	return &ResearchService{sectors: map[string]string{}}
}

func (s *ResearchService) GenerateIdeas(sessionID uint, scene, stockScope, startDate, endDate string, universe []string) ([]models.PredictionResearchIdea, []ResearchIdea) {
	s.features = s.loadFeatureDataset(startDate, endDate, universe)
	s.loadSectors()
	templates := []ResearchTemplate{
		ResearchTemplateFactorScan,
		ResearchTemplateMoneyFlowAnomaly,
		ResearchTemplateSectorRotation,
		ResearchTemplateFailureAttribution,
		ResearchTemplateExternalCandidate,
		ResearchTemplateEventEffectiveness,
		ResearchTemplateLimitUpStrength,
		ResearchTemplateScreeningConsensus,
		ResearchTemplateRecommendationMeta,
	}
	ideas := make([]ResearchIdea, 0, len(templates))
	rows := make([]models.PredictionResearchIdea, 0, len(templates))
	for _, template := range templates {
		idea := s.buildIdea(template, scene, stockScope, startDate, endDate, universe)
		ideas = append(ideas, idea)
		row := researchIdeaModel(sessionID, idea)
		if db.Dao != nil {
			_ = db.Dao.Create(&row).Error
		}
		rows = append(rows, row)
	}
	return rows, ideas
}

func (s *ResearchService) buildIdea(template ResearchTemplate, scene, stockScope, startDate, endDate string, universe []string) ResearchIdea {
	factors := s.templateFactors(template)
	candidates := make([]ResearchCandidateFactor, 0, len(factors))
	for _, factorID := range factors {
		candidates = append(candidates, ResearchCandidateFactor{
			FactorID: factorID,
			Source:   researchFactorSource(factorID),
			Evidence: s.factorEvidence(factorID, startDate, endDate, universe),
		})
		candidates[len(candidates)-1].Confidence = researchConfidence(candidates[len(candidates)-1].Evidence)
	}
	theme := string(template)
	ruleIdeas := []string{"use only registered indicators and validate with unified feature view"}
	risks := []string{"research evidence is hypothesis-level and must pass DSL validation, backtest, out-of-sample, and paper-trade gates"}
	if template == ResearchTemplateExternalCandidate {
		ruleIdeas = []string{"candidate external data source must be synchronized, assigned available_at, coverage-tested, and registered before DSL use"}
		risks = append(risks, "external data is not tradable until promoted from candidate to active registry status")
	}
	return ResearchIdea{
		ID:               fmt.Sprintf("%s-%d", template, time.Now().UnixNano()),
		SchemaVersion:    ResearchIdeaSchemaV2,
		Template:         string(template),
		Source:           "agent_research_template",
		Scene:            scene,
		StockScope:       stockScope,
		Theme:            theme,
		CandidateFactors: candidates,
		RuleIdeas:        ruleIdeas,
		DataEvidence:     []string{fmt.Sprintf("point-in-time feature scan: %d rows through %s", len(s.features), endDate), "external-source candidates are evidence only"},
		RiskHypotheses:   risks,
		Questions:        []string{"does the factor remain stable across sectors and recent windows?"},
		CreatedAt:        time.Now().Format(time.RFC3339),
	}
}

func (s *ResearchService) templateFactors(template ResearchTemplate) []string {
	switch template {
	case ResearchTemplateFactorScan:
		return []string{"stock_feature.MA20", "stock_feature.VolumeRatio", "stock_feature.ChangeRate20"}
	case ResearchTemplateMoneyFlowAnomaly:
		return []string{"stock_feature.FundFlow5", "stock_feature.FundFlow20", "money_flow.MainNetInflow5"}
	case ResearchTemplateSectorRotation:
		return []string{"sector.NetInflow", "market.SentimentScore"}
	case ResearchTemplateFailureAttribution:
		return []string{"stock_feature.ATR", "stock_feature.ChangeRate5"}
	case ResearchTemplateExternalCandidate:
		return []string{"candidate.northbound_flow", "candidate.news_sentiment", "candidate.announcement_risk"}
	case ResearchTemplateEventEffectiveness:
		return []string{"event.ChangeEventCount", "event.HasLargeBuy", "event.HasRapidRise"}
	case ResearchTemplateLimitUpStrength:
		return []string{"limitup.KeepTimes", "limitup.SealRatioClose", "limitup.ExplodedCount"}
	case ResearchTemplateScreeningConsensus:
		return []string{"screening.PatternCount", "screening.MACDGoldenCross", "screening.MABullish"}
	case ResearchTemplateRecommendationMeta:
		return []string{"recommendation.ModelCount", "recommendation.RecommendationCount"}
	default:
		return []string{"stock_feature.Close"}
	}
}

func (s *ResearchService) factorEvidence(factorID, startDate, endDate string, universe []string) ResearchFactorEvidence {
	_ = universe
	evidence := ResearchFactorEvidence{
		StartDate:        startDate,
		EndDate:          endDate,
		Horizon:          5,
		SampleSize:       0,
		IC:               0,
		RankIC:           0,
		HitRate:          0,
		AvgForwardReturn: 0,
		PValue:           1,
		SectorIC:         0,
		SectorICStd:      0,
		TimeDecay:        "unknown",
		RecentIC:         0,
	}
	observations := s.factorObservations(factorID, evidence.Horizon)
	evidence.SampleSize = len(observations)
	if len(observations) < 3 {
		return evidence
	}
	factors, returns := observationVectors(observations)
	evidence.IC, evidence.RankIC, evidence.PValue = dailyCrossSectionalCorrelation(observations)
	evidence.AvgForwardReturn = meanFloat(returns)
	factorMean := meanFloat(factors)
	returnMean := meanFloat(returns)
	hits := 0
	for i := range factors {
		if (factors[i]-factorMean)*(returns[i]-returnMean) > 0 {
			hits++
		}
	}
	evidence.HitRate = float64(hits) / float64(len(observations))
	recent := recentObservations(observations, 0.30)
	evidence.RecentIC, _, _ = dailyCrossSectionalCorrelation(recent)
	evidence.SectorIC, evidence.SectorICStd = sectorCorrelationStats(observations)
	if math.Abs(evidence.RecentIC) < math.Abs(evidence.IC)*0.75 {
		evidence.TimeDecay = "decaying"
	} else {
		evidence.TimeDecay = "stable"
	}
	return evidence
}

func (s *ResearchService) sampleSize(startDate, endDate string, universe []string) int {
	_, _, _ = startDate, endDate, universe
	return len(s.features)
}

type researchObservation struct {
	Date          string
	StockCode     string
	Sector        string
	Factor        float64
	ForwardReturn float64
}

func (s *ResearchService) loadFeatureDataset(startDate, endDate string, universe []string) []models.StockFeature {
	if db.Dao == nil {
		return nil
	}
	var features []models.StockFeature
	query := db.Dao.Where("date >= ? AND date <= ? AND feature_version = ? AND adjusted = ?", startDate, endDate, CurrentFeatureVersion, true)
	if len(universe) > 0 {
		query = query.Where("stock_code IN ?", universe)
	}
	_ = query.Order("stock_code asc, date asc").Find(&features).Error
	return features
}

func (s *ResearchService) loadSectors() {
	for _, feature := range s.features {
		if _, exists := s.sectors[feature.StockCode]; exists {
			continue
		}
		industry, _ := stockIndustryAndConcepts(feature.StockCode)
		if strings.TrimSpace(industry) == "" {
			industry = "unknown"
		}
		s.sectors[feature.StockCode] = industry
	}
}

func (s *ResearchService) factorObservations(factorID string, horizon int) []researchObservation {
	if horizon <= 0 {
		horizon = 5
	}
	byStock := make(map[string][]models.StockFeature)
	for _, feature := range s.features {
		byStock[feature.StockCode] = append(byStock[feature.StockCode], feature)
	}
	result := make([]researchObservation, 0, len(s.features))
	engine := NewStrategyEngine()
	name := canonicalIndicatorName("", factorID)
	externalValues := s.externalFactorValues(factorID)
	for stockCode, features := range byStock {
		for i := 0; i+horizon < len(features); i++ {
			current := features[i]
			future := features[i+horizon]
			if current.Close <= 0 || future.Close <= 0 {
				continue
			}
			factor := engine.GetIndicatorValue(name, current)
			if !strings.HasPrefix(factorID, "stock_feature.") {
				var exists bool
				factor, exists = externalValues[current.Date+"|"+NormalizeCandidateStockCode(stockCode)]
				if !exists {
					continue
				}
			}
			if (strings.Contains(factorID, "FundFlow") || strings.Contains(factorID, "Volume")) && factor == 0 {
				continue
			}
			result = append(result, researchObservation{
				Date: current.Date, StockCode: stockCode, Sector: s.sectors[stockCode], Factor: factor,
				ForwardReturn: future.Close/current.Close - 1,
			})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result
}

func (s *ResearchService) externalFactorValues(factorID string) map[string]float64 {
	result := map[string]float64{}
	if db.Dao == nil || len(s.features) == 0 {
		return result
	}
	startDate, endDate := s.features[0].Date, s.features[0].Date
	for _, feature := range s.features {
		if feature.Date < startDate {
			startDate = feature.Date
		}
		if feature.Date > endDate {
			endDate = feature.Date
		}
	}
	featureAsOf := make(map[string]time.Time, len(s.features))
	for _, feature := range s.features {
		asOf := feature.DataAsOf
		if asOf.IsZero() {
			asOf = featureDataAsOf(feature.Date)
		}
		featureAsOf[feature.Date+"|"+NormalizeCandidateStockCode(feature.StockCode)] = asOf
	}
	pointInTimeAvailable := func(key string, availableAt time.Time) bool {
		asOf, ok := featureAsOf[key]
		return ok && (availableAt.IsZero() || !availableAt.After(asOf))
	}
	switch {
	case strings.HasPrefix(factorID, "event."):
		var rows []models.StockEventDaily
		db.Dao.Where("trade_date >= ? AND trade_date <= ? AND feature_version = ?", startDate, endDate, CurrentFeatureVersion).Find(&rows)
		for _, row := range rows {
			key := row.TradeDate + "|" + NormalizeCandidateStockCode(row.StockCode)
			if !pointInTimeAvailable(key, row.AvailableAt) {
				continue
			}
			value := float64(row.ChangeEventCount)
			switch factorID {
			case "event.HasLargeBuy":
				value = boolFloat(row.HasLargeBuy)
			case "event.HasRapidRise":
				value = boolFloat(row.HasRapidRise)
			}
			result[key] = value
		}
	case strings.HasPrefix(factorID, "screening."):
		var rows []models.StockScreeningFactDaily
		db.Dao.Where("trade_date >= ? AND trade_date <= ? AND feature_version = ? AND calculated = ?", startDate, endDate, CurrentFeatureVersion, true).Find(&rows)
		for _, row := range rows {
			key := row.TradeDate + "|" + NormalizeCandidateStockCode(row.StockCode)
			if !pointInTimeAvailable(key, row.AvailableAt) {
				continue
			}
			value := float64(row.PatternCount)
			switch factorID {
			case "screening.MACDGoldenCross":
				value = boolFloat(row.MACDGoldenCross)
			case "screening.MABullish":
				value = boolFloat(row.MABullish)
			}
			result[key] = value
		}
	case strings.HasPrefix(factorID, "limitup."):
		var rows []models.UplimitStockDaily
		db.Dao.Where("trade_date >= ? AND trade_date <= ?", startDate, endDate).Order("available_at asc").Find(&rows)
		latestAt := map[string]time.Time{}
		for _, row := range rows {
			key := row.TradeDate + "|" + NormalizeCandidateStockCode(row.StockCode)
			if !pointInTimeAvailable(key, row.AvailableAt) || (!latestAt[key].IsZero() && row.AvailableAt.Before(latestAt[key])) {
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
			}
			latestAt[key] = row.AvailableAt
			result[key] = value
		}
	case strings.HasPrefix(factorID, "recommendation."):
		var rows []models.ModelRecommendationEvent
		db.Dao.Where("trade_date >= ? AND trade_date <= ? AND independent_for_research = ?", startDate, endDate, true).Find(&rows)
		modelsByKey := map[string]map[string]bool{}
		counts := map[string]int{}
		for _, row := range rows {
			key := row.TradeDate + "|" + NormalizeCandidateStockCode(row.StockCode)
			if !pointInTimeAvailable(key, row.AvailableAt) {
				continue
			}
			counts[key]++
			if modelsByKey[key] == nil {
				modelsByKey[key] = map[string]bool{}
			}
			modelsByKey[key][row.ModelName] = true
		}
		for key, count := range counts {
			if factorID == "recommendation.ModelCount" {
				result[key] = float64(len(modelsByKey[key]))
			} else {
				result[key] = float64(count)
			}
		}
	}
	return result
}

func dailyCrossSectionalCorrelation(observations []researchObservation) (float64, float64, float64) {
	byDate := map[string][]researchObservation{}
	for _, observation := range observations {
		byDate[observation.Date] = append(byDate[observation.Date], observation)
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
	if len(ics) == 0 {
		return 0, 0, 1
	}
	meanIC := meanFloat(ics)
	if len(ics) < 2 {
		return meanIC, meanFloat(rankICs), 1
	}
	variance := 0.0
	for _, ic := range ics {
		variance += (ic - meanIC) * (ic - meanIC)
	}
	std := math.Sqrt(variance / float64(len(ics)-1))
	if std == 0 {
		if meanIC == 0 {
			return meanIC, meanFloat(rankICs), 1
		}
		return meanIC, meanFloat(rankICs), 0
	}
	t := math.Abs(meanIC) / (std / math.Sqrt(float64(len(ics))))
	return meanIC, meanFloat(rankICs), math.Erfc(t / math.Sqrt2)
}

func observationVectors(observations []researchObservation) ([]float64, []float64) {
	factors := make([]float64, 0, len(observations))
	returns := make([]float64, 0, len(observations))
	for _, observation := range observations {
		factors = append(factors, observation.Factor)
		returns = append(returns, observation.ForwardReturn)
	}
	return factors, returns
}

func recentObservations(observations []researchObservation, fraction float64) []researchObservation {
	if len(observations) == 0 {
		return nil
	}
	start := int(float64(len(observations)) * (1 - fraction))
	if start < 0 {
		start = 0
	}
	if start >= len(observations) {
		start = len(observations) - 1
	}
	return observations[start:]
}

func pearsonCorrelation(x, y []float64) float64 {
	if len(x) != len(y) || len(x) < 3 {
		return 0
	}
	mx, my := meanFloat(x), meanFloat(y)
	var numerator, dx, dy float64
	for i := range x {
		xd, yd := x[i]-mx, y[i]-my
		numerator += xd * yd
		dx += xd * xd
		dy += yd * yd
	}
	if dx <= 0 || dy <= 0 {
		return 0
	}
	return numerator / math.Sqrt(dx*dy)
}

func rankValues(values []float64) []float64 {
	type rankedValue struct {
		index int
		value float64
	}
	ordered := make([]rankedValue, len(values))
	for i, value := range values {
		ordered[i] = rankedValue{index: i, value: value}
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].value < ordered[j].value })
	ranks := make([]float64, len(values))
	for start := 0; start < len(ordered); {
		end := start + 1
		for end < len(ordered) && ordered[end].value == ordered[start].value {
			end++
		}
		rank := (float64(start) + float64(end-1)) / 2
		for i := start; i < end; i++ {
			ranks[ordered[i].index] = rank
		}
		start = end
	}
	return ranks
}

func correlationPValue(correlation float64, sampleSize int) float64 {
	if sampleSize < 3 || math.Abs(correlation) >= 1 {
		if math.Abs(correlation) >= 1 && sampleSize >= 3 {
			return 0
		}
		return 1
	}
	t := math.Abs(correlation) * math.Sqrt(float64(sampleSize-2)/(1-correlation*correlation))
	return math.Erfc(t / math.Sqrt2)
}

func sectorCorrelationStats(observations []researchObservation) (float64, float64) {
	groups := make(map[string][]researchObservation)
	for _, observation := range observations {
		groups[observation.Sector] = append(groups[observation.Sector], observation)
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
	return meanIC, math.Sqrt(variance / float64(len(ics)))
}

func meanFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func researchConfidence(evidence ResearchFactorEvidence) string {
	if evidence.SampleSize < 30 || evidence.PValue > 0.20 {
		return "low"
	}
	if evidence.PValue <= 0.05 && math.Abs(evidence.RecentIC) >= math.Abs(evidence.IC)*0.75 {
		return "high"
	}
	return "medium"
}

func researchFactorSource(factorID string) string {
	if strings.HasPrefix(factorID, "candidate.") {
		return "external_candidate"
	}
	if strings.HasPrefix(factorID, "money_flow.") {
		return "internal_money_flow"
	}
	if strings.HasPrefix(factorID, "market.") || strings.HasPrefix(factorID, "sector.") {
		return "internal_market"
	}
	return "internal_feature"
}

func researchIdeaModel(sessionID uint, idea ResearchIdea) models.PredictionResearchIdea {
	candidates, _ := json.Marshal(idea.CandidateFactors)
	ruleIdeas, _ := json.Marshal(idea.RuleIdeas)
	evidence, _ := json.Marshal(idea.DataEvidence)
	risks, _ := json.Marshal(idea.RiskHypotheses)
	questions, _ := json.Marshal(idea.Questions)
	return models.PredictionResearchIdea{
		SessionID:        sessionID,
		Source:           idea.Source,
		Scene:            idea.Scene,
		StockScope:       idea.StockScope,
		Theme:            idea.Theme,
		CandidateFactors: string(candidates),
		RuleIdeas:        string(ruleIdeas),
		DataEvidence:     string(evidence),
		RiskHypotheses:   string(risks),
		Questions:        string(questions),
		Status:           "generated",
	}
}
