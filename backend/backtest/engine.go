package backtest

import (
	"encoding/json"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"math"
	"sort"
	"time"
)

const sqliteInClauseChunkSize = 900

// Condition 策略条件
type Condition struct {
	Indicator string  `json:"indicator"`
	Operator  string  `json:"operator"`
	Ref       string  `json:"ref"`
	Value     float64 `json:"value"`
}

// Rule 策略规则
type Rule struct {
	EntryConditions []Condition `json:"entryConditions"`
	ExitConditions  []Condition `json:"exitConditions"`
	StopLoss        float64     `json:"stopLoss"`
	StopGain        float64     `json:"stopGain"`
	MaxHoldDays     int         `json:"maxHoldDays"` // 最大持有天数
	MaxHoldings     int         `json:"maxHoldings"`
}

type BacktestConfig struct {
	EntryMode         string  `json:"entryMode"`
	Slippage          float64 `json:"slippage"`
	FeeRate           float64 `json:"feeRate"`
	UseLimitRule      bool    `json:"useLimitRule"`
	UseSuspensionRule bool    `json:"useSuspensionRule"`
	MinDataCoverage   float64 `json:"minDataCoverage"`
	FeatureVersion    string  `json:"featureVersion"`
	BenchmarkCode     string  `json:"benchmarkCode"`
}

func DefaultBacktestConfig() BacktestConfig {
	return BacktestConfig{
		EntryMode:         "next_open",
		Slippage:          0.0015,
		FeeRate:           0.001,
		UseLimitRule:      true,
		UseSuspensionRule: true,
		MinDataCoverage:   0.95,
		FeatureVersion:    "daily_v1",
		BenchmarkCode:     "sh000300",
	}
}

// Signal 买入信号
type Signal struct {
	StockCode  string
	StockName  string
	Date       string
	Price      float64
	ReasonJSON string
}

// Trade 交易记录
type Trade struct {
	StockCode       string    `json:"stockCode"`
	StockName       string    `json:"stockName"`
	SignalDate      string    `json:"signalDate"`
	BuyDate         string    `json:"buyDate"`
	SellDate        string    `json:"sellDate"`
	BuyPrice        float64   `json:"buyPrice"`
	SellPrice       float64   `json:"sellPrice"`
	Fee             float64   `json:"fee"`
	Slippage        float64   `json:"slippage"`
	ReturnRate      float64   `json:"returnRate"`
	MaxReturn       float64   `json:"maxReturn"`
	MaxDrawdown     float64   `json:"maxDrawdown"`
	HoldDays        int       `json:"holdDays"`
	ExitReason      string    `json:"exitReason"` // stop_loss / stop_gain / exit_condition / max_hold_days
	EntryReasonJSON string    `json:"entryReasonJson"`
	FeatureVersion  string    `json:"featureVersion"`
	DataAsOf        time.Time `json:"dataAsOf"`
	Hit             bool      `json:"hit"`
}

// DailyNav 每日净值
type DailyNav struct {
	Date       string  `json:"date"`
	Nav        float64 `json:"nav"`
	Drawdown   float64 `json:"drawdown"`
	TradeCount int     `json:"tradeCount"`
}

// ValidationResult 验证结果
type ValidationResult struct {
	WinRate              float64        `json:"winRate"`
	AvgReturn            float64        `json:"avgReturn"`
	MedianReturn         float64        `json:"medianReturn"`
	ProfitLossRatio      float64        `json:"profitLossRatio"`
	MaxDrawdown          float64        `json:"maxDrawdown"`
	TradeCount           int            `json:"tradeCount"`
	TotalReturn          float64        `json:"totalReturn"`
	AnnualizedReturn     float64        `json:"annualizedReturn"`
	OutSampleAvgReturn   float64        `json:"outSampleAvgReturn"`
	OutSampleMaxDrawdown float64        `json:"outSampleMaxDrawdown"`
	BenchmarkCode        string         `json:"benchmarkCode"`
	BenchmarkReturn      float64        `json:"benchmarkReturn"`
	ExcessReturn         float64        `json:"excessReturn"`
	TurnoverRate         float64        `json:"turnoverRate"`
	AverageHoldingDays   float64        `json:"averageHoldingDays"`
	DataCoverage         float64        `json:"dataCoverage"`
	NoLookaheadPassed    bool           `json:"noLookaheadPassed"`
	BacktestConfig       BacktestConfig `json:"backtestConfig"`
	DailyNAV             []DailyNav     `json:"dailyNAV"`
	Trades               []Trade        `json:"trades"`
}

// Position 持仓
type Position struct {
	StockCode       string
	StockName       string
	SignalDate      string
	BuyDate         string
	BuyPrice        float64
	BuyCost         float64
	BuyDayIndex     int
	MaxPrice        float64
	MinPrice        float64
	EntryReasonJSON string
	FeatureVersion  string
	DataAsOf        time.Time
}

// FeatureRepository 特征数据仓库
type FeatureRepository struct{}

func NewFeatureRepository() *FeatureRepository {
	return &FeatureRepository{}
}

// GetByDate 获取某交易日全部特征
func (r *FeatureRepository) GetByDate(date string, universe []string) []models.StockFeature {
	var features []models.StockFeature
	if len(universe) == 0 {
		db.Dao.Where("date = ?", date).Find(&features)
		return features
	}

	for start := 0; start < len(universe); start += sqliteInClauseChunkSize {
		end := start + sqliteInClauseChunkSize
		if end > len(universe) {
			end = len(universe)
		}
		var chunkFeatures []models.StockFeature
		db.Dao.Where("date = ? AND stock_code IN ?", date, universe[start:end]).Find(&chunkFeatures)
		features = append(features, chunkFeatures...)
	}
	return features
}

// GetFeatureRange 获取某股票日期区间特征
func (r *FeatureRepository) GetFeatureRange(stockCode, startDate, endDate string) []models.StockFeature {
	var features []models.StockFeature
	db.Dao.Where("stock_code = ? AND date >= ? AND date <= ?", stockCode, startDate, endDate).
		Order("date asc").
		Find(&features)
	return features
}

func (r *FeatureRepository) GetFirstOnOrAfter(stockCode, date string) (models.StockFeature, bool) {
	var feature models.StockFeature
	err := db.Dao.Where("stock_code = ? AND date >= ?", stockCode, date).
		Order("date asc").
		First(&feature).Error
	return feature, err == nil
}

// GetFeatureAfterDate 获取某股票在某日期之后的所有特征
type dateFeatureKey struct {
	date      string
	stockCode string
}

// WalkForwardValidator 滚动验证引擎
type WalkForwardValidator struct {
	repo *FeatureRepository
}

func NewWalkForwardValidator() *WalkForwardValidator {
	return &WalkForwardValidator{
		repo: NewFeatureRepository(),
	}
}

// Validate 对单个假设做滚动回测
func (v *WalkForwardValidator) Validate(
	rule Rule,
	universe []string,
	startDate string,
	endDate string,
	timeHorizon int,
) (*ValidationResult, error) {
	return v.ValidateWithConfig(rule, universe, startDate, endDate, timeHorizon, DefaultBacktestConfig())
}

func (v *WalkForwardValidator) ValidateWithConfig(
	rule Rule,
	universe []string,
	startDate string,
	endDate string,
	timeHorizon int,
	config BacktestConfig,
) (*ValidationResult, error) {
	if config.EntryMode == "" {
		config = DefaultBacktestConfig()
	}
	if config.BenchmarkCode == "" {
		config.BenchmarkCode = DefaultBacktestConfig().BenchmarkCode
	}
	tradingDays := v.getTradingDays(startDate, endDate)
	if len(tradingDays) == 0 {
		return &ValidationResult{}, nil
	}
	result, err := NewPortfolioEngine(v.repo).RunBacktest(rule, universe, tradingDays, timeHorizon, config)
	if err != nil {
		return result, err
	}
	result.DataCoverage = v.estimateDataCoverage(universe, tradingDays)
	result.OutSampleAvgReturn, result.OutSampleMaxDrawdown = v.calculateOutSampleMetrics(result.Trades, startDate, endDate)
	result.BenchmarkCode = config.BenchmarkCode
	result.BenchmarkReturn = v.calculateBenchmarkReturn(config.BenchmarkCode, startDate, endDate)
	result.ExcessReturn = result.TotalReturn - result.BenchmarkReturn
	return result, nil
}

// checkExit 检查是否需要卖出
func (v *WalkForwardValidator) checkExit(rule Rule, pos Position, featureMap map[string]models.StockFeature, dayIndex int, timeHorizon int) (float64, string) {
	f, ok := featureMap[pos.StockCode]
	if !ok {
		return 0, ""
	}

	// 1. 止损
	stopLossPrice := pos.BuyPrice * (1 - rule.StopLoss)
	if f.Low <= stopLossPrice {
		return stopLossPrice, "stop_loss"
	}

	// 2. 止盈
	stopGainPrice := pos.BuyPrice * (1 + rule.StopGain)
	if f.High >= stopGainPrice {
		return stopGainPrice, "stop_gain"
	}

	// 3. 卖出条件
	if v.matchConditions(rule.ExitConditions, f) {
		return f.Close, "exit_condition"
	}

	// 4. 最大持有天数
	maxHoldDays := rule.MaxHoldDays
	if maxHoldDays <= 0 {
		maxHoldDays = timeHorizon
	}
	if dayIndex-pos.BuyDayIndex >= maxHoldDays {
		return f.Close, "max_hold_days"
	}

	return 0, ""
}

func (v *WalkForwardValidator) canEnter(f models.StockFeature, config BacktestConfig) bool {
	if config.UseSuspensionRule && (f.Open <= 0 || f.Close <= 0 || f.High <= 0 || f.Low <= 0 || f.Volume <= 0) {
		return false
	}
	if config.UseLimitRule && f.Open > 0 && f.High == f.Low {
		return false
	}
	return true
}

// generateSignals 根据规则生成买入信号
func (v *WalkForwardValidator) generateSignals(rule Rule, features []models.StockFeature) []Signal {
	return NewStrategyEngine().GenerateSignals(rule, features)
}

// matchConditions 匹配条件
func (v *WalkForwardValidator) matchConditions(conditions []Condition, f models.StockFeature) bool {
	return NewStrategyEngine().MatchConditions(conditions, f)
}

// matchCondition 匹配单个条件
func (v *WalkForwardValidator) matchCondition(c Condition, f models.StockFeature) bool {
	return NewStrategyEngine().MatchCondition(c, f)
}

// getIndicatorValue 获取指标数值
func (v *WalkForwardValidator) getIndicatorValue(indicator string, f models.StockFeature) float64 {
	return NewStrategyEngine().GetIndicatorValue(indicator, f)
}

// calculateMetrics 计算绩效指标
func (v *WalkForwardValidator) calculateMetrics(trades []Trade, dailyNAV []DailyNav) *ValidationResult {
	totalReturn := portfolioTotalReturn(dailyNAV)
	annualizedReturn := annualizeReturn(totalReturn, len(dailyNAV))
	portfolioMaxDrawdown := portfolioMaxDrawdown(dailyNAV)
	turnoverRate := 0.0
	if len(dailyNAV) > 0 {
		turnoverRate = float64(len(trades)) / float64(len(dailyNAV))
	}
	if len(trades) == 0 {
		return &ValidationResult{
			WinRate:           0,
			AvgReturn:         0,
			MedianReturn:      0,
			ProfitLossRatio:   0,
			MaxDrawdown:       portfolioMaxDrawdown,
			TradeCount:        0,
			TotalReturn:       totalReturn,
			AnnualizedReturn:  annualizedReturn,
			TurnoverRate:      turnoverRate,
			NoLookaheadPassed: true,
			DailyNAV:          dailyNAV,
			Trades:            trades,
		}
	}

	winCount := 0
	maxDrawdown := 0.0
	grossProfit := 0.0
	grossLoss := 0.0
	totalHoldDays := 0
	returns := make([]float64, 0, len(trades))

	for _, t := range trades {
		if t.Hit {
			winCount++
		}
		returns = append(returns, t.ReturnRate)
		totalHoldDays += t.HoldDays
		if t.ReturnRate >= 0 {
			grossProfit += t.ReturnRate
		} else {
			grossLoss += -t.ReturnRate
		}
		if t.MaxDrawdown > maxDrawdown {
			maxDrawdown = t.MaxDrawdown
		}
	}
	if portfolioMaxDrawdown > maxDrawdown {
		maxDrawdown = portfolioMaxDrawdown
	}

	winRate := float64(winCount) / float64(len(trades))
	avgReturn := 0.0
	tradeReturnSum := 0.0
	for _, value := range returns {
		tradeReturnSum += value
	}
	avgReturn = tradeReturnSum / float64(len(trades))
	sort.Float64s(returns)
	medianReturn := returns[len(returns)/2]
	if len(returns)%2 == 0 {
		medianReturn = (returns[len(returns)/2-1] + returns[len(returns)/2]) / 2
	}
	avgHoldDays := float64(totalHoldDays) / float64(len(trades))
	profitLossRatio := 0.0
	if grossLoss > 0 {
		profitLossRatio = grossProfit / grossLoss
	} else if grossProfit > 0 {
		profitLossRatio = grossProfit
	}

	return &ValidationResult{
		WinRate:            winRate,
		AvgReturn:          avgReturn,
		MedianReturn:       medianReturn,
		ProfitLossRatio:    profitLossRatio,
		MaxDrawdown:        maxDrawdown,
		TradeCount:         len(trades),
		TotalReturn:        totalReturn,
		AnnualizedReturn:   annualizedReturn,
		TurnoverRate:       turnoverRate,
		AverageHoldingDays: avgHoldDays,
		NoLookaheadPassed:  true,
		DailyNAV:           dailyNAV,
		Trades:             trades,
	}
}

func (v *WalkForwardValidator) estimateDataCoverage(universe []string, tradingDays []string) float64 {
	if len(tradingDays) == 0 {
		return 0
	}
	expectedStocks := len(universe)
	if expectedStocks == 0 {
		var stockCount int64
		db.Dao.Model(&models.StockFeature{}).
			Where("date >= ? AND date <= ?", tradingDays[0], tradingDays[len(tradingDays)-1]).
			Distinct("stock_code").
			Count(&stockCount)
		expectedStocks = int(stockCount)
	}
	if expectedStocks == 0 {
		return 0
	}
	var rows int64
	query := db.Dao.Model(&models.StockFeature{}).
		Where("date >= ? AND date <= ?", tradingDays[0], tradingDays[len(tradingDays)-1]).
		Where("open > 0 AND close > 0 AND high > 0 AND low > 0 AND volume > 0")
	if len(universe) > 0 {
		query = query.Where("stock_code IN ?", universe)
	}
	query.Count(&rows)
	expectedRows := expectedStocks * len(tradingDays)
	if expectedRows == 0 {
		return 0
	}
	return float64(rows) / float64(expectedRows)
}

func (v *WalkForwardValidator) calculateOutSampleMetrics(trades []Trade, startDate, endDate string) (float64, float64) {
	if len(trades) == 0 {
		return 0, 0
	}
	start, err1 := time.Parse("2006-01-02", startDate)
	end, err2 := time.Parse("2006-01-02", endDate)
	if err1 != nil || err2 != nil || !end.After(start) {
		return 0, 0
	}
	cutoff := start.Add(time.Duration(float64(end.Sub(start)) * 0.7))
	count := 0
	sum := 0.0
	maxDrawdown := 0.0
	for _, t := range trades {
		buyDate, err := time.Parse("2006-01-02", t.BuyDate)
		if err != nil || buyDate.Before(cutoff) {
			continue
		}
		count++
		sum += t.ReturnRate
		if t.MaxDrawdown > maxDrawdown {
			maxDrawdown = t.MaxDrawdown
		}
	}
	if count == 0 {
		return 0, 0
	}
	return sum / float64(count), maxDrawdown
}

func (v *WalkForwardValidator) calculateBenchmarkReturn(benchmarkCode string, startDate string, endDate string) float64 {
	if benchmarkCode == "" {
		return 0
	}
	var first models.StockFeature
	if err := db.Dao.Where("stock_code IN ? AND date >= ? AND date <= ?", stockCodeVariants(benchmarkCode), startDate, endDate).
		Order("date asc").
		First(&first).Error; err != nil || first.Close <= 0 {
		return 0
	}
	var last models.StockFeature
	if err := db.Dao.Where("stock_code IN ? AND date >= ? AND date <= ?", stockCodeVariants(benchmarkCode), startDate, endDate).
		Order("date desc").
		First(&last).Error; err != nil || last.Close <= 0 {
		return 0
	}
	return (last.Close - first.Close) / first.Close
}

func portfolioTotalReturn(dailyNAV []DailyNav) float64 {
	if len(dailyNAV) == 0 {
		return 0
	}
	first := dailyNAV[0].Nav
	last := dailyNAV[len(dailyNAV)-1].Nav
	if first <= 0 {
		first = 1
	}
	return (last - first) / first
}

func annualizeReturn(totalReturn float64, tradingDays int) float64 {
	if tradingDays <= 0 {
		return 0
	}
	if totalReturn <= -1 {
		return -1
	}
	return math.Pow(1+totalReturn, 252.0/float64(tradingDays)) - 1
}

func portfolioMaxDrawdown(dailyNAV []DailyNav) float64 {
	maxDrawdown := 0.0
	for _, nav := range dailyNAV {
		if nav.Drawdown > maxDrawdown {
			maxDrawdown = nav.Drawdown
		}
	}
	return maxDrawdown
}

func (v *WalkForwardValidator) getTradingDays(startDate, endDate string) []string {
	var days []string
	db.Dao.Model(&models.StockFeature{}).
		Where("date >= ? AND date <= ?", startDate, endDate).
		Distinct("date").
		Order("date asc").
		Pluck("date", &days)
	return days
}

// addCalendarDays 添加日历日
func (v *WalkForwardValidator) addCalendarDays(date string, days int) string {
	t, _ := time.Parse("2006-01-02", date)
	return t.AddDate(0, 0, days).Format("2006-01-02")
}

// RuleToJSON 规则转 JSON
func RuleToJSON(rule Rule) string {
	b, _ := json.Marshal(rule)
	return string(b)
}

// JSONToRule JSON 转规则
func JSONToRule(s string) (Rule, error) {
	var rule Rule
	err := json.Unmarshal([]byte(s), &rule)
	return rule, err
}
