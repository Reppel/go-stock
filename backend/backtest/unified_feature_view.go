package backtest

import (
	"go-stock/backend/db"
	"go-stock/backend/models"
	"math"
	"sort"
	"strings"
	"time"
)

type UnifiedFeatureView struct {
	StockCode       string               `json:"stockCode"`
	TradeDate       string               `json:"tradeDate"`
	FeatureVersion  string               `json:"featureVersion"`
	RegistryVersion string               `json:"registryVersion"`
	DataAsOf        time.Time            `json:"dataAsOf"`
	DecisionAsOf    time.Time            `json:"decisionAsOf"`
	Feature         map[string]float64   `json:"feature"`
	MoneyFlow       map[string]float64   `json:"moneyFlow"`
	Market          map[string]float64   `json:"market"`
	Sector          map[string]float64   `json:"sector"`
	Event           map[string]float64   `json:"event"`
	LimitUp         map[string]float64   `json:"limitUp"`
	Screening       map[string]float64   `json:"screening"`
	Recommendation  map[string]float64   `json:"recommendation"`
	Availability    map[string]time.Time `json:"availability"`
	Coverage        map[string]float64   `json:"coverage"`
}

type UnifiedFeatureViewService struct {
	registry []IndicatorDefinition
}

func NewUnifiedFeatureViewService() *UnifiedFeatureViewService {
	return &UnifiedFeatureViewService{registry: GetIndicatorRegistry()}
}

func (s *UnifiedFeatureViewService) Build(stockCode string, tradeDate string, feature models.StockFeature) UnifiedFeatureView {
	return s.BuildForIndicators(stockCode, tradeDate, feature, nil)
}

// BuildForIndicators materializes one point-in-time view. Supplemental sources
// are loaded lazily so technical-only backtests do not issue unused data queries.
func (s *UnifiedFeatureViewService) BuildForIndicators(stockCode string, tradeDate string, feature models.StockFeature, indicatorIDs []string) UnifiedFeatureView {
	return s.BuildForIndicatorsAsOf(stockCode, tradeDate, feature, indicatorIDs, feature.DataAsOf)
}

// BuildForIndicatorsAsOf separates feature publication time from the decision
// time. Market bars remain bounded by feature.DataAsOf while supplemental
// sources may be read only when available by decisionAsOf.
func (s *UnifiedFeatureViewService) BuildForIndicatorsAsOf(stockCode string, tradeDate string, feature models.StockFeature, indicatorIDs []string, decisionAsOf time.Time) UnifiedFeatureView {
	view := UnifiedFeatureView{
		StockCode:       stockCode,
		TradeDate:       tradeDate,
		FeatureVersion:  feature.FeatureVersion,
		RegistryVersion: CurrentIndicatorRegistry,
		DataAsOf:        feature.DataAsOf,
		Feature:         map[string]float64{},
		MoneyFlow:       map[string]float64{},
		Market:          map[string]float64{},
		Sector:          map[string]float64{},
		Event:           map[string]float64{},
		LimitUp:         map[string]float64{},
		Screening:       map[string]float64{},
		Recommendation:  map[string]float64{},
		Availability:    map[string]time.Time{},
		Coverage:        map[string]float64{},
	}
	if view.FeatureVersion == "" {
		view.FeatureVersion = CurrentFeatureVersion
	}
	if view.DataAsOf.IsZero() {
		view.DataAsOf = featureDataAsOf(tradeDate)
	}
	if decisionAsOf.IsZero() || decisionAsOf.Before(view.DataAsOf) {
		decisionAsOf = view.DataAsOf
	}
	view.DecisionAsOf = decisionAsOf
	s.fillStockFeature(&view, feature)
	loadAll := len(indicatorIDs) == 0
	needMoneyFlow, needMarket, needSector := loadAll, loadAll, loadAll
	needEvent, needLimitUp, needScreening, needRecommendation := loadAll, loadAll, loadAll, loadAll
	for _, indicatorID := range indicatorIDs {
		switch {
		case strings.HasPrefix(indicatorID, "money_flow."):
			needMoneyFlow = true
		case strings.HasPrefix(indicatorID, "market."):
			needMarket = true
		case strings.HasPrefix(indicatorID, "sector."):
			needSector = true
		case strings.HasPrefix(indicatorID, "event."):
			needEvent = true
		case strings.HasPrefix(indicatorID, "limitup."):
			needLimitUp = true
		case strings.HasPrefix(indicatorID, "screening."):
			needScreening = true
		case strings.HasPrefix(indicatorID, "recommendation."):
			needRecommendation = true
		}
	}
	if needMoneyFlow {
		s.fillMoneyFlow(&view, stockCode, tradeDate)
	}
	if needMarket {
		s.fillMarket(&view, tradeDate)
	}
	if needSector {
		s.fillSector(&view, stockCode, tradeDate)
	}
	if needEvent {
		s.fillEvent(&view, stockCode, tradeDate)
	}
	if needLimitUp {
		s.fillLimitUp(&view, stockCode, tradeDate)
	}
	if needScreening {
		s.fillScreening(&view, stockCode, tradeDate)
	}
	if needRecommendation {
		s.fillRecommendation(&view, stockCode, tradeDate)
	}
	for _, def := range s.registry {
		if _, ok := view.Coverage[def.IndicatorID]; !ok {
			view.Coverage[def.IndicatorID] = 0
		}
	}
	return view
}

func (v UnifiedFeatureView) Value(indicatorID string) (float64, bool) {
	indicatorID = canonicalIndicatorID(indicatorID)
	maps := []map[string]float64{v.Feature, v.MoneyFlow, v.Market, v.Sector, v.Event, v.LimitUp, v.Screening, v.Recommendation}
	for _, values := range maps {
		value, exists := values[indicatorID]
		if !exists {
			continue
		}
		if coverage, ok := v.Coverage[indicatorID]; ok && coverage <= 0 {
			return 0, false
		}
		availableAt := v.Availability[indicatorID]
		cutoff := v.DecisionAsOf
		if strings.HasPrefix(indicatorID, "stock_feature.") || cutoff.IsZero() {
			cutoff = v.DataAsOf
		}
		if !availableAt.IsZero() && !cutoff.IsZero() && availableAt.After(cutoff) {
			return 0, false
		}
		return value, true
	}
	return 0, false
}

func (s *UnifiedFeatureViewService) fillStockFeature(view *UnifiedFeatureView, f models.StockFeature) {
	values := map[string]float64{
		"stock_feature.Open":         f.Open,
		"stock_feature.High":         f.High,
		"stock_feature.Low":          f.Low,
		"stock_feature.Close":        f.Close,
		"stock_feature.Volume":       f.Volume,
		"stock_feature.Turnover":     f.Turnover,
		"stock_feature.MA5":          f.MA5,
		"stock_feature.MA10":         f.MA10,
		"stock_feature.MA20":         f.MA20,
		"stock_feature.MA60":         f.MA60,
		"stock_feature.MACD":         f.MACD,
		"stock_feature.RSI6":         f.RSI6,
		"stock_feature.RSI12":        f.RSI12,
		"stock_feature.KDJ_K":        f.KDJ_K,
		"stock_feature.BOLLUpper":    f.BOLLUpper,
		"stock_feature.BOLLMid":      f.BOLLMid,
		"stock_feature.BOLLLower":    f.BOLLLower,
		"stock_feature.VolumeRatio":  f.VolumeRatio,
		"stock_feature.ATR":          f.ATR,
		"stock_feature.FundFlow5":    f.FundFlow5,
		"stock_feature.FundFlow20":   f.FundFlow20,
		"stock_feature.ChangeRate5":  f.ChangeRate5,
		"stock_feature.ChangeRate20": f.ChangeRate20,
	}
	for id, value := range values {
		view.Feature[id] = value
		view.Coverage[id] = coverageForValue(value)
		view.Availability[id] = view.DataAsOf
	}
}

func (s *UnifiedFeatureViewService) fillMoneyFlow(view *UnifiedFeatureView, stockCode string, tradeDate string) {
	if db.Dao == nil {
		return
	}
	var flow models.StockMoneyFlowDaily
	if db.Dao.Where("stock_code IN ? AND trade_date = ?", stockCodeVariants(stockCode), tradeDate).First(&flow).Error != nil {
		return
	}
	values := map[string]float64{
		"money_flow.MainNetInflow1": flow.MainNetInflow1,
		"money_flow.MainNetInflow5": flow.MainNetInflow5,
	}
	dataAsOf := flow.DataAsOf
	if dataAsOf.IsZero() {
		dataAsOf = view.DataAsOf
	}
	for id, value := range values {
		view.MoneyFlow[id] = value
		view.Coverage[id] = coverageForValue(value)
		view.Availability[id] = dataAsOf
	}
}

func (s *UnifiedFeatureViewService) fillMarket(view *UnifiedFeatureView, tradeDate string) {
	if db.Dao == nil {
		return
	}
	var market models.MarketFactorDaily
	if db.Dao.Where("trade_date = ?", tradeDate).First(&market).Error != nil {
		return
	}
	upRatio := 0.0
	if market.UpCount+market.DownCount > 0 {
		upRatio = float64(market.UpCount) / float64(market.UpCount+market.DownCount)
	}
	values := map[string]float64{
		"market.SentimentScore": market.SentimentScore,
		"market.UpRatio":        upRatio,
		"market.AdvanceDecline": float64(market.UpCount - market.DownCount),
	}
	for id, value := range values {
		view.Market[id] = value
		view.Coverage[id] = coverageForValue(value)
		if market.DataAsOf.IsZero() {
			view.Availability[id] = view.DataAsOf
		} else {
			view.Availability[id] = market.DataAsOf
		}
	}
}

func (s *UnifiedFeatureViewService) fillSector(view *UnifiedFeatureView, stockCode string, tradeDate string) {
	if db.Dao == nil {
		return
	}
	industry, concepts := stockIndustryAndConcepts(stockCode)
	net, availableAt, found := pointInTimeSectorNet("industry", []string{industry}, tradeDate, view.DecisionAsOf)
	if !found {
		net, availableAt, found = pointInTimeSectorNet("concept", concepts, tradeDate, view.DecisionAsOf)
	}
	if !found {
		return
	}
	id := "sector.NetInflow"
	view.Sector[id] = net
	view.Coverage[id] = 1
	view.Availability[id] = availableAt
}

func pointInTimeSectorNet(sectorType string, names []string, tradeDate string, decisionAsOf time.Time) (float64, time.Time, bool) {
	filtered := make([]string, 0, len(names))
	for _, name := range names {
		if value := strings.TrimSpace(name); value != "" {
			filtered = append(filtered, value)
		}
	}
	if len(filtered) == 0 || db.Dao == nil {
		return 0, time.Time{}, false
	}
	var rows []models.SectorFlowDaily
	query := db.Dao.Where("sector_type = ? AND sector_name IN ? AND trade_date = ?", sectorType, uniqueStrings(filtered), tradeDate)
	if !decisionAsOf.IsZero() {
		query = query.Where("data_as_of <= ?", decisionAsOf)
	}
	if query.Order("data_as_of desc, id desc").Find(&rows).Error != nil || len(rows) == 0 {
		return 0, time.Time{}, false
	}
	values := make([]float64, 0, len(rows))
	seen := map[string]bool{}
	availableAt := time.Time{}
	for _, row := range rows {
		if seen[row.SectorName] {
			continue
		}
		seen[row.SectorName] = true
		values = append(values, row.NetInflow)
		if row.DataAsOf.After(availableAt) {
			availableAt = row.DataAsOf
		}
	}
	sort.Float64s(values)
	median := values[len(values)/2]
	if len(values)%2 == 0 {
		median = (values[len(values)/2-1] + values[len(values)/2]) / 2
	}
	return median, availableAt, true
}

func (s *UnifiedFeatureViewService) fillEvent(view *UnifiedFeatureView, stockCode, tradeDate string) {
	if db.Dao == nil {
		return
	}
	var event models.StockEventDaily
	if db.Dao.Where("stock_code IN ? AND trade_date = ? AND feature_version = ? AND available_at <= ?", stockCodeVariants(stockCode), tradeDate, view.FeatureVersion, view.DecisionAsOf).
		Order("available_at desc").First(&event).Error != nil {
		return
	}
	values := map[string]float64{
		"event.ChangeEventCount": float64(event.ChangeEventCount),
		"event.HasLargeBuy":      boolFloat(event.HasLargeBuy), "event.HasLargeSell": boolFloat(event.HasLargeSell),
		"event.HasLimitUp": boolFloat(event.HasLimitUp), "event.HasLimitDown": boolFloat(event.HasLimitDown),
		"event.HasRapidRise": boolFloat(event.HasRapidRise), "event.HasRapidFall": boolFloat(event.HasRapidFall),
	}
	for id, value := range values {
		view.Event[id], view.Coverage[id], view.Availability[id] = value, 1, event.AvailableAt
	}
}

func (s *UnifiedFeatureViewService) fillLimitUp(view *UnifiedFeatureView, stockCode, tradeDate string) {
	if db.Dao == nil {
		return
	}
	var rows []models.UplimitStockDaily
	if db.Dao.Where("stock_code IN ? AND trade_date = ? AND available_at <= ?", stockCodeVariants(stockCode), tradeDate, view.DecisionAsOf).
		Order("available_at desc").Find(&rows).Error != nil || len(rows) == 0 {
		return
	}
	latestKey := rows[0].SnapshotKey
	keepTimes, sealMax, sealClose, exploded, plateHeat := 0.0, 0.0, 0.0, 0.0, 0.0
	for _, row := range rows {
		if row.SnapshotKey != latestKey {
			continue
		}
		keepTimes = math.Max(keepTimes, float64(row.KeepTimes))
		sealMax, sealClose = math.Max(sealMax, row.SealRatioMax), math.Max(sealClose, row.SealRatioClose)
		plateHeat = math.Max(plateHeat, row.PlateHeat)
		if row.Exploded {
			exploded++
		}
	}
	values := map[string]float64{
		"limitup.KeepTimes": keepTimes, "limitup.SealRatioMax": sealMax,
		"limitup.SealRatioClose": sealClose, "limitup.ExplodedCount": exploded, "limitup.PlateHeat": plateHeat,
	}
	for id, value := range values {
		view.LimitUp[id], view.Coverage[id], view.Availability[id] = value, 1, rows[0].AvailableAt
	}
}

func (s *UnifiedFeatureViewService) fillScreening(view *UnifiedFeatureView, stockCode, tradeDate string) {
	if db.Dao == nil {
		return
	}
	var fact models.StockScreeningFactDaily
	if db.Dao.Where("stock_code IN ? AND trade_date = ? AND feature_version = ? AND calculated = ? AND available_at <= ?", stockCodeVariants(stockCode), tradeDate, view.FeatureVersion, true, view.DecisionAsOf).
		Order("available_at desc").First(&fact).Error != nil {
		return
	}
	values := map[string]float64{
		"screening.PatternCount": float64(fact.PatternCount), "screening.MACDGoldenCross": boolFloat(fact.MACDGoldenCross),
		"screening.MABullish": boolFloat(fact.MABullish), "screening.BollBreakout": boolFloat(fact.BollBreakout),
		"screening.VolumeBreakout": boolFloat(fact.VolumeBreakout), "screening.Oversold": boolFloat(fact.Oversold),
	}
	for id, value := range values {
		view.Screening[id], view.Coverage[id], view.Availability[id] = value, fact.Coverage, fact.AvailableAt
	}
}

func (s *UnifiedFeatureViewService) fillRecommendation(view *UnifiedFeatureView, stockCode, tradeDate string) {
	if db.Dao == nil {
		return
	}
	start, err := time.Parse("2006-01-02", tradeDate)
	if err != nil {
		return
	}
	var rows []models.ModelRecommendationEvent
	if db.Dao.Where("stock_code IN ? AND trade_date >= ? AND trade_date <= ? AND available_at <= ?", stockCodeVariants(stockCode), start.AddDate(0, 0, -30).Format("2006-01-02"), tradeDate, view.DecisionAsOf).
		Find(&rows).Error != nil || len(rows) == 0 {
		return
	}
	modelsSeen := map[string]bool{}
	latest := time.Time{}
	for _, row := range rows {
		modelsSeen[row.ModelName] = true
		if row.AvailableAt.After(latest) {
			latest = row.AvailableAt
		}
	}
	view.Recommendation["recommendation.ModelCount"] = float64(len(modelsSeen))
	view.Recommendation["recommendation.RecommendationCount"] = float64(len(rows))
	view.Coverage["recommendation.ModelCount"], view.Coverage["recommendation.RecommendationCount"] = 1, 1
	view.Availability["recommendation.ModelCount"], view.Availability["recommendation.RecommendationCount"] = latest, latest
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func coverageForValue(value float64) float64 {
	if value == 0 {
		return 0.5
	}
	return 1
}
