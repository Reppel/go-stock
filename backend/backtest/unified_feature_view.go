package backtest

import (
	"go-stock/backend/db"
	"go-stock/backend/models"
	"strings"
	"time"
)

type UnifiedFeatureView struct {
	StockCode       string               `json:"stockCode"`
	TradeDate       string               `json:"tradeDate"`
	FeatureVersion  string               `json:"featureVersion"`
	RegistryVersion string               `json:"registryVersion"`
	DataAsOf        time.Time            `json:"dataAsOf"`
	Feature         map[string]float64   `json:"feature"`
	MoneyFlow       map[string]float64   `json:"moneyFlow"`
	Market          map[string]float64   `json:"market"`
	Sector          map[string]float64   `json:"sector"`
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
		Availability:    map[string]time.Time{},
		Coverage:        map[string]float64{},
	}
	if view.FeatureVersion == "" {
		view.FeatureVersion = CurrentFeatureVersion
	}
	if view.DataAsOf.IsZero() {
		view.DataAsOf = featureDataAsOf(tradeDate)
	}
	s.fillStockFeature(&view, feature)
	loadAll := len(indicatorIDs) == 0
	needMoneyFlow, needMarket, needSector := loadAll, loadAll, loadAll
	for _, indicatorID := range indicatorIDs {
		switch {
		case strings.HasPrefix(indicatorID, "money_flow."):
			needMoneyFlow = true
		case strings.HasPrefix(indicatorID, "market."):
			needMarket = true
		case strings.HasPrefix(indicatorID, "sector."):
			needSector = true
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
	for _, def := range s.registry {
		if _, ok := view.Coverage[def.IndicatorID]; !ok {
			view.Coverage[def.IndicatorID] = 0
		}
	}
	return view
}

func (v UnifiedFeatureView) Value(indicatorID string) (float64, bool) {
	indicatorID = canonicalIndicatorID(indicatorID)
	maps := []map[string]float64{v.Feature, v.MoneyFlow, v.Market, v.Sector}
	for _, values := range maps {
		value, exists := values[indicatorID]
		if !exists {
			continue
		}
		if coverage, ok := v.Coverage[indicatorID]; ok && coverage <= 0 {
			return 0, false
		}
		availableAt := v.Availability[indicatorID]
		if !availableAt.IsZero() && !v.DataAsOf.IsZero() && availableAt.After(v.DataAsOf) {
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
	net, _ := latestNamedSectorNet("industry", []string{industry}, tradeDate)
	if net == 0 {
		net, _ = latestNamedSectorNet("concept", concepts, tradeDate)
	}
	id := "sector.NetInflow"
	view.Sector[id] = net
	view.Coverage[id] = coverageForValue(net)
	view.Availability[id] = view.DataAsOf
}

func coverageForValue(value float64) float64 {
	if value == 0 {
		return 0.5
	}
	return 1
}
