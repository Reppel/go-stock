package backtest

import (
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"math"
	"sort"
	"time"
)

const sqliteInClauseChunkSize = 900

// Condition 策略条件
type Condition struct {
	Indicator   string  `json:"indicator"`
	IndicatorID string  `json:"indicatorId,omitempty"`
	Source      string  `json:"source,omitempty"`
	Operator    string  `json:"operator"`
	Ref         string  `json:"ref"`
	RefID       string  `json:"refId,omitempty"`
	Value       float64 `json:"value"`
	Lag         int     `json:"lag,omitempty"`
	RefLag      int     `json:"refLag,omitempty"`
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
	SellStampDuty     float64 `json:"sellStampDuty"`
	MinCommission     float64 `json:"minCommission"`
	InitialCapital    float64 `json:"initialCapital"`
	LotSize           int     `json:"lotSize"`
	MaxParticipation  float64 `json:"maxParticipation"`
	ImpactCoefficient float64 `json:"impactCoefficient"`
	LimitBuffer       float64 `json:"limitBuffer"`
	UseT1Rule         bool    `json:"useT1Rule"`
	UseLimitRule      bool    `json:"useLimitRule"`
	UseSuspensionRule bool    `json:"useSuspensionRule"`
	MinDataCoverage   float64 `json:"minDataCoverage"`
	FeatureVersion    string  `json:"featureVersion"`
	BenchmarkCode     string  `json:"benchmarkCode"`
	PaperTradeDays    int     `json:"paperTradeDays"`
	ActiveTTLDays     int     `json:"activeTtlDays"`
}

func DefaultBacktestConfig() BacktestConfig {
	return BacktestConfig{
		EntryMode:         "next_open",
		Slippage:          0.0015,
		FeeRate:           0.0003,
		SellStampDuty:     0.0005,
		MinCommission:     5,
		InitialCapital:    1_000_000,
		LotSize:           100,
		MaxParticipation:  0.05,
		ImpactCoefficient: 0.002,
		LimitBuffer:       0,
		UseT1Rule:         true,
		UseLimitRule:      true,
		UseSuspensionRule: true,
		MinDataCoverage:   0.95,
		FeatureVersion:    CurrentFeatureVersion,
		BenchmarkCode:     "sh000300",
		PaperTradeDays:    60,
		ActiveTTLDays:     90,
	}
}

func normalizeBacktestConfig(config BacktestConfig) BacktestConfig {
	defaults := DefaultBacktestConfig()
	if isZeroBacktestConfig(config) {
		return defaults
	}
	if config.EntryMode == "" {
		config.EntryMode = defaults.EntryMode
	}
	if config.Slippage < 0 {
		config.Slippage = defaults.Slippage
	}
	if config.FeeRate <= 0 {
		config.FeeRate = defaults.FeeRate
	}
	if config.SellStampDuty <= 0 {
		config.SellStampDuty = defaults.SellStampDuty
	}
	if config.MinCommission <= 0 {
		config.MinCommission = defaults.MinCommission
	}
	if config.InitialCapital <= 0 {
		config.InitialCapital = defaults.InitialCapital
	}
	if config.LotSize <= 0 {
		config.LotSize = defaults.LotSize
	}
	if config.MaxParticipation <= 0 || config.MaxParticipation > 1 {
		config.MaxParticipation = defaults.MaxParticipation
	}
	if config.ImpactCoefficient <= 0 {
		config.ImpactCoefficient = defaults.ImpactCoefficient
	}
	if config.LimitBuffer < 0 || config.LimitBuffer > 0.01 {
		config.LimitBuffer = defaults.LimitBuffer
	}
	if config.FeatureVersion == "" {
		config.FeatureVersion = defaults.FeatureVersion
	}
	if config.BenchmarkCode == "" {
		config.BenchmarkCode = defaults.BenchmarkCode
	}
	if config.PaperTradeDays <= 0 {
		config.PaperTradeDays = defaults.PaperTradeDays
	}
	if config.ActiveTTLDays <= 0 {
		config.ActiveTTLDays = defaults.ActiveTTLDays
	}
	return config
}

func isZeroBacktestConfig(config BacktestConfig) bool {
	return config.EntryMode == "" &&
		config.Slippage == 0 &&
		config.FeeRate == 0 &&
		config.SellStampDuty == 0 &&
		config.MinCommission == 0 &&
		config.InitialCapital == 0 &&
		config.LotSize == 0 &&
		config.MaxParticipation == 0 &&
		config.ImpactCoefficient == 0 &&
		config.LimitBuffer == 0 &&
		!config.UseT1Rule &&
		!config.UseLimitRule &&
		!config.UseSuspensionRule &&
		config.MinDataCoverage == 0 &&
		config.FeatureVersion == "" &&
		config.BenchmarkCode == "" &&
		config.PaperTradeDays == 0 &&
		config.ActiveTTLDays == 0
}

// Signal 买入信号
type Signal struct {
	StockCode  string
	StockName  string
	Date       string
	Price      float64
	Score      float64
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
	Quantity        float64   `json:"quantity"`
	GrossBuyAmount  float64   `json:"grossBuyAmount"`
	GrossSellAmount float64   `json:"grossSellAmount"`
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
	WinRate               float64            `json:"winRate"`
	AvgReturn             float64            `json:"avgReturn"`
	MedianReturn          float64            `json:"medianReturn"`
	ProfitLossRatio       float64            `json:"profitLossRatio"`
	ProfitLossRatioStatus string             `json:"profitLossRatioStatus"`
	MaxDrawdown           float64            `json:"maxDrawdown"`
	TradeCount            int                `json:"tradeCount"`
	TotalReturn           float64            `json:"totalReturn"`
	AnnualizedReturn      float64            `json:"annualizedReturn"`
	AnnualizedVolatility  float64            `json:"annualizedVolatility"`
	SharpeRatio           float64            `json:"sharpeRatio"`
	SortinoRatio          float64            `json:"sortinoRatio"`
	CalmarRatio           float64            `json:"calmarRatio"`
	MaxSingleTradeLoss    float64            `json:"maxSingleTradeLoss"`
	OutSampleAvgReturn    float64            `json:"outSampleAvgReturn"`
	OutSampleMaxDrawdown  float64            `json:"outSampleMaxDrawdown"`
	OutSampleTradeCount   int                `json:"outSampleTradeCount"`
	BenchmarkCode         string             `json:"benchmarkCode"`
	BenchmarkReturn       float64            `json:"benchmarkReturn"`
	BenchmarkAvailable    bool               `json:"benchmarkAvailable"`
	ExcessReturn          float64            `json:"excessReturn"`
	TurnoverRate          float64            `json:"turnoverRate"`
	Turnover              TurnoverBreakdown  `json:"turnover"`
	AverageHoldingDays    float64            `json:"averageHoldingDays"`
	DataCoverage          float64            `json:"dataCoverage"`
	NoLookaheadPassed     bool               `json:"noLookaheadPassed"`
	BacktestConfig        BacktestConfig     `json:"backtestConfig"`
	CostSensitivity       []CostStressResult `json:"costSensitivity"`
	OverfitDiagnostics    OverfitDiagnostics `json:"overfitDiagnostics"`
	Verdict               QuantVerdict       `json:"verdict"`
	DailyNAV              []DailyNav         `json:"dailyNAV"`
	Trades                []Trade            `json:"trades"`
	WalkForwardFolds      []WalkForwardFold  `json:"walkForwardFolds"`
}

type WalkForwardFold struct {
	StartDate   string  `json:"startDate"`
	EndDate     string  `json:"endDate"`
	TradeCount  int     `json:"tradeCount"`
	AvgReturn   float64 `json:"avgReturn"`
	MaxDrawdown float64 `json:"maxDrawdown"`
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
type FeatureRepository struct {
	featureVersion string
}

func NewFeatureRepository() *FeatureRepository {
	return NewFeatureRepositoryForVersion(CurrentFeatureVersion)
}

func NewFeatureRepositoryForVersion(featureVersion string) *FeatureRepository {
	if featureVersion == "" {
		featureVersion = CurrentFeatureVersion
	}
	return &FeatureRepository{featureVersion: featureVersion}
}

// GetByDate 获取某交易日全部特征
func (r *FeatureRepository) GetByDate(date string, universe []string) []models.StockFeature {
	var features []models.StockFeature
	query := db.Dao.Where("date = ? AND feature_version = ?", date, r.featureVersion)
	if len(universe) == 0 {
		query.Find(&features)
		return features
	}

	for start := 0; start < len(universe); start += sqliteInClauseChunkSize {
		end := start + sqliteInClauseChunkSize
		if end > len(universe) {
			end = len(universe)
		}
		var chunkFeatures []models.StockFeature
		query.Where("stock_code IN ?", universe[start:end]).Find(&chunkFeatures)
		features = append(features, chunkFeatures...)
	}
	return features
}

// GetFeatureRange 获取某股票日期区间特征
func (r *FeatureRepository) GetFeatureRange(stockCode, startDate, endDate string) []models.StockFeature {
	var features []models.StockFeature
	db.Dao.Where("stock_code = ? AND date >= ? AND date <= ? AND feature_version = ?", stockCode, startDate, endDate, r.featureVersion).
		Order("date asc").
		Find(&features)
	return features
}

func (r *FeatureRepository) GetFirstOnOrAfter(stockCode, date string) (models.StockFeature, bool) {
	var feature models.StockFeature
	err := db.Dao.Where("stock_code = ? AND date >= ? AND feature_version = ?", stockCode, date, r.featureVersion).
		Order("date asc").
		First(&feature).Error
	return feature, err == nil
}

func (r *FeatureRepository) GetFirstAfter(stockCode, date string) (models.StockFeature, bool) {
	var feature models.StockFeature
	err := db.Dao.Where("stock_code = ? AND date > ? AND feature_version = ?", stockCode, date, r.featureVersion).
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
	config = normalizeBacktestConfig(config)
	tradingDays := v.getTradingDays(startDate, endDate, config.FeatureVersion, config.BenchmarkCode)
	if len(tradingDays) == 0 {
		return &ValidationResult{}, nil
	}
	repo := NewFeatureRepositoryForVersion(config.FeatureVersion)
	result, err := NewPortfolioEngine(repo).RunBacktest(rule, universe, tradingDays, timeHorizon, config)
	if err != nil {
		return result, err
	}
	result.DataCoverage = v.estimateDataCoverage(universe, tradingDays, config.FeatureVersion)
	result.WalkForwardFolds, result.OutSampleAvgReturn, result.OutSampleMaxDrawdown, result.OutSampleTradeCount =
		v.calculateWalkForwardBacktests(rule, universe, tradingDays, timeHorizon, config)
	result.BenchmarkCode = config.BenchmarkCode
	result.BenchmarkReturn, result.BenchmarkAvailable = v.calculateBenchmarkReturn(config.BenchmarkCode, startDate, endDate, config.FeatureVersion)
	if result.BenchmarkAvailable {
		result.ExcessReturn = result.TotalReturn - result.BenchmarkReturn
	}
	v.attachVerdict(rule, universe, tradingDays, timeHorizon, config, result)
	if config.MinDataCoverage > 0 && result.DataCoverage < config.MinDataCoverage {
		return result, fmt.Errorf("特征覆盖率 %.1f%% 低于最低要求 %.1f%%", result.DataCoverage*100, config.MinDataCoverage*100)
	}
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
	annualizedVolatility, downsideVolatility := navVolatility(dailyNAV)
	sharpe := safeRatio(annualizedReturn, annualizedVolatility)
	sortino := safeRatio(annualizedReturn, downsideVolatility)
	calmar := safeRatio(annualizedReturn, portfolioMaxDrawdown)
	if len(trades) == 0 {
		return &ValidationResult{
			WinRate:               0,
			AvgReturn:             0,
			MedianReturn:          0,
			ProfitLossRatio:       0,
			ProfitLossRatioStatus: "no_trades",
			MaxDrawdown:           portfolioMaxDrawdown,
			TradeCount:            0,
			TotalReturn:           totalReturn,
			AnnualizedReturn:      annualizedReturn,
			AnnualizedVolatility:  annualizedVolatility,
			SharpeRatio:           sharpe,
			SortinoRatio:          sortino,
			CalmarRatio:           calmar,
			NoLookaheadPassed:     true,
			DailyNAV:              dailyNAV,
			Trades:                trades,
		}
	}

	winCount := 0
	grossProfit := 0.0
	grossLoss := 0.0
	winningTrades := 0
	losingTrades := 0
	maxSingleTradeLoss := 0.0
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
			winningTrades++
		} else {
			grossLoss += -t.ReturnRate
			losingTrades++
			if -t.ReturnRate > maxSingleTradeLoss {
				maxSingleTradeLoss = -t.ReturnRate
			}
		}
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
	profitLossRatioStatus := "normal"
	if grossLoss > 0 && losingTrades > 0 && winningTrades > 0 {
		profitLossRatio = (grossProfit / float64(winningTrades)) / (grossLoss / float64(losingTrades))
	} else if grossProfit > 0 && losingTrades == 0 {
		profitLossRatioStatus = "no_loss"
	} else if winningTrades == 0 && losingTrades > 0 {
		profitLossRatioStatus = "no_profit"
	}

	return &ValidationResult{
		WinRate:               winRate,
		AvgReturn:             avgReturn,
		MedianReturn:          medianReturn,
		ProfitLossRatio:       profitLossRatio,
		ProfitLossRatioStatus: profitLossRatioStatus,
		MaxDrawdown:           portfolioMaxDrawdown,
		TradeCount:            len(trades),
		TotalReturn:           totalReturn,
		AnnualizedReturn:      annualizedReturn,
		AnnualizedVolatility:  annualizedVolatility,
		SharpeRatio:           sharpe,
		SortinoRatio:          sortino,
		CalmarRatio:           calmar,
		MaxSingleTradeLoss:    maxSingleTradeLoss,
		AverageHoldingDays:    avgHoldDays,
		NoLookaheadPassed:     true,
		DailyNAV:              dailyNAV,
		Trades:                trades,
	}
}

func (v *WalkForwardValidator) estimateDataCoverage(universe []string, tradingDays []string, featureVersion string) float64 {
	if len(tradingDays) == 0 {
		return 0
	}
	expectedStocks := len(universe)
	if expectedStocks == 0 {
		var stockCount int64
		db.Dao.Model(&models.StockFeature{}).
			Where("date >= ? AND date <= ? AND feature_version = ?", tradingDays[0], tradingDays[len(tradingDays)-1], featureVersion).
			Distinct("stock_code").
			Count(&stockCount)
		expectedStocks = int(stockCount)
	}
	if expectedStocks == 0 {
		return 0
	}
	var rows int64
	query := db.Dao.Model(&models.StockFeature{}).
		Where("date >= ? AND date <= ? AND feature_version = ?", tradingDays[0], tradingDays[len(tradingDays)-1], featureVersion).
		Where("open > 0 AND close > 0 AND high > 0 AND low > 0 AND volume > 0 AND adjusted = ?", true)
	if len(universe) > 0 {
		query = query.Where("stock_code IN ?", universe)
	}
	query.Count(&rows)
	expectedRows := expectedStocks * len(tradingDays)
	if expectedRows == 0 {
		return 0
	}
	return math.Min(float64(rows)/float64(expectedRows), 1)
}

func (v *WalkForwardValidator) calculateWalkForwardMetrics(trades []Trade, nav []DailyNav, tradingDays []string, purgeDays int) ([]WalkForwardFold, float64, float64, int) {
	if len(tradingDays) < 10 {
		return nil, 0, 0, 0
	}
	if purgeDays < 1 {
		purgeDays = 1
	}
	firstTest := int(float64(len(tradingDays)) * 0.4)
	remaining := len(tradingDays) - firstTest
	foldSize := remaining / 3
	if foldSize < 2 {
		firstTest = int(float64(len(tradingDays)) * 0.7)
		foldSize = len(tradingDays) - firstTest
	}
	folds := make([]WalkForwardFold, 0, 3)
	outReturns := make([]float64, 0)
	maxDrawdown := 0.0
	for foldIndex, startIndex := 0, firstTest; startIndex < len(tradingDays) && foldIndex < 3; foldIndex, startIndex = foldIndex+1, startIndex+foldSize {
		testStart := startIndex + purgeDays
		if testStart >= len(tradingDays) {
			break
		}
		testEnd := startIndex + foldSize - 1
		if foldIndex == 2 || testEnd >= len(tradingDays) {
			testEnd = len(tradingDays) - 1
		}
		if testStart > testEnd {
			continue
		}
		startDate := tradingDays[testStart]
		endDate := tradingDays[testEnd]
		foldReturns := make([]float64, 0)
		for _, trade := range trades {
			if trade.BuyDate >= startDate && trade.BuyDate <= endDate {
				foldReturns = append(foldReturns, trade.ReturnRate)
				outReturns = append(outReturns, trade.ReturnRate)
			}
		}
		foldDrawdown := navRangeMaxDrawdown(nav, startDate, endDate)
		if foldDrawdown > maxDrawdown {
			maxDrawdown = foldDrawdown
		}
		folds = append(folds, WalkForwardFold{
			StartDate: startDate, EndDate: endDate, TradeCount: len(foldReturns),
			AvgReturn: mean(foldReturns), MaxDrawdown: foldDrawdown,
		})
	}
	return folds, mean(outReturns), maxDrawdown, len(outReturns)
}

// calculateWalkForwardBacktests re-runs each temporal validation fold with
// isolated cash and positions. It avoids labelling slices of one full-period
// portfolio run as independent out-of-sample evidence.
func (v *WalkForwardValidator) calculateWalkForwardBacktests(rule Rule, universe []string, tradingDays []string, purgeDays int, config BacktestConfig) ([]WalkForwardFold, float64, float64, int) {
	if len(tradingDays) < 10 {
		return nil, 0, 0, 0
	}
	if purgeDays < 1 {
		purgeDays = 1
	}
	firstTest := int(float64(len(tradingDays)) * 0.4)
	remaining := len(tradingDays) - firstTest
	foldSize := remaining / 3
	if foldSize < 2 {
		firstTest = int(float64(len(tradingDays)) * 0.7)
		foldSize = len(tradingDays) - firstTest
	}
	folds := make([]WalkForwardFold, 0, 3)
	returns := make([]float64, 0)
	maxDrawdown := 0.0
	for foldIndex, startIndex := 0, firstTest; startIndex < len(tradingDays) && foldIndex < 3; foldIndex, startIndex = foldIndex+1, startIndex+foldSize {
		testStart := startIndex + purgeDays
		if testStart >= len(tradingDays) {
			break
		}
		testEnd := startIndex + foldSize - 1
		if foldIndex == 2 || testEnd >= len(tradingDays) {
			testEnd = len(tradingDays) - 1
		}
		if testStart > testEnd {
			continue
		}
		foldDays := append([]string(nil), tradingDays[testStart:testEnd+1]...)
		run, err := NewPortfolioEngine(NewFeatureRepositoryForVersion(config.FeatureVersion)).
			RunBacktest(rule, universe, foldDays, purgeDays, config)
		if err != nil || run == nil {
			continue
		}
		for _, trade := range run.Trades {
			returns = append(returns, trade.ReturnRate)
		}
		if run.MaxDrawdown > maxDrawdown {
			maxDrawdown = run.MaxDrawdown
		}
		folds = append(folds, WalkForwardFold{
			StartDate: foldDays[0], EndDate: foldDays[len(foldDays)-1], TradeCount: run.TradeCount,
			AvgReturn: run.AvgReturn, MaxDrawdown: run.MaxDrawdown,
		})
	}
	return folds, mean(returns), maxDrawdown, len(returns)
}

func (v *WalkForwardValidator) calculateBenchmarkReturn(benchmarkCode string, startDate string, endDate string, featureVersion string) (float64, bool) {
	if benchmarkCode == "" {
		return 0, false
	}
	var first models.StockFeature
	if err := db.Dao.Where("stock_code IN ? AND date >= ? AND date <= ? AND feature_version = ?", stockCodeVariants(benchmarkCode), startDate, endDate, featureVersion).
		Order("date asc").
		First(&first).Error; err != nil || first.Close <= 0 {
		return 0, false
	}
	var last models.StockFeature
	if err := db.Dao.Where("stock_code IN ? AND date >= ? AND date <= ? AND feature_version = ?", stockCodeVariants(benchmarkCode), startDate, endDate, featureVersion).
		Order("date desc").
		First(&last).Error; err != nil || last.Close <= 0 {
		return 0, false
	}
	return (last.Close - first.Close) / first.Close, true
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

func navVolatility(dailyNAV []DailyNav) (annualized, downsideAnnualized float64) {
	if len(dailyNAV) < 2 {
		return 0, 0
	}
	returns := make([]float64, 0, len(dailyNAV)-1)
	downsideSquares := 0.0
	downsideCount := 0
	for index := 1; index < len(dailyNAV); index++ {
		previous := dailyNAV[index-1].Nav
		if previous <= 0 {
			continue
		}
		value := dailyNAV[index].Nav/previous - 1
		returns = append(returns, value)
		if value < 0 {
			downsideSquares += value * value
			downsideCount++
		}
	}
	if len(returns) < 2 {
		return 0, 0
	}
	average := mean(returns)
	variance := 0.0
	for _, value := range returns {
		variance += math.Pow(value-average, 2)
	}
	variance /= float64(len(returns) - 1)
	annualized = math.Sqrt(variance) * math.Sqrt(252)
	if downsideCount > 0 {
		downsideAnnualized = math.Sqrt(downsideSquares/float64(downsideCount)) * math.Sqrt(252)
	}
	return annualized, downsideAnnualized
}

func navRangeMaxDrawdown(dailyNAV []DailyNav, startDate, endDate string) float64 {
	peak := 0.0
	maxDrawdown := 0.0
	for _, daily := range dailyNAV {
		if daily.Date < startDate || daily.Date > endDate {
			continue
		}
		if daily.Nav > peak {
			peak = daily.Nav
		}
		if peak > 0 {
			drawdown := (peak - daily.Nav) / peak
			if drawdown > maxDrawdown {
				maxDrawdown = drawdown
			}
		}
	}
	return clamp(maxDrawdown, 0, 1)
}

func safeRatio(numerator, denominator float64) float64 {
	if denominator <= 0 || math.IsNaN(denominator) || math.IsInf(denominator, 0) {
		return 0
	}
	return numerator / denominator
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func (v *WalkForwardValidator) getTradingDays(startDate, endDate, featureVersion, benchmarkCode string) []string {
	var days []string
	db.Dao.Model(&models.StockFeature{}).
		Where("stock_code IN ? AND date >= ? AND date <= ? AND feature_version = ?", stockCodeVariants(benchmarkCode), startDate, endDate, featureVersion).
		Distinct("date").
		Order("date asc").
		Pluck("date", &days)
	if len(days) > 0 {
		return days
	}
	db.Dao.Model(&models.StockFeature{}).
		Where("date >= ? AND date <= ? AND feature_version = ?", startDate, endDate, featureVersion).
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
