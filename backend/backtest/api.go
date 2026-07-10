package backtest

import (
	"encoding/json"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PredictionService 预测工厂服务
type PredictionService struct {
	aiGenerator *AIGenerator
	validator   *WalkForwardValidator
}

var (
	predictionTablesMu    sync.Mutex
	predictionTablesReady bool
)

// NewPredictionService 创建服务
func NewPredictionService() *PredictionService {
	return &PredictionService{
		aiGenerator: NewAIGenerator(),
		validator:   NewWalkForwardValidator(),
	}
}

func ensurePredictionTables() error {
	if db.Dao == nil {
		return fmt.Errorf("数据库未初始化")
	}

	predictionTablesMu.Lock()
	defer predictionTablesMu.Unlock()
	if predictionTablesReady {
		return nil
	}

	if err := db.Dao.AutoMigrate(
		&models.PredictionSession{},
		&models.PredictionHypothesis{},
		&models.PredictionDecision{},
		&models.PredictionAlertLog{},
		&models.PredictionSignal{},
		&models.PredictionHypothesisDaily{},
		&models.StockFeature{},
		&models.FeatureSyncJob{},
		&models.PredictionTrade{},
		&models.TradeDecisionLog{},
		&models.PredictionGenerationAudit{},
		&models.MarketFactorDaily{},
		&models.StockMoneyFlowDaily{},
		&models.SectorFlowDaily{},
		&models.StockEventDaily{},
		&models.StockRiskEvent{},
	); err != nil {
		return err
	}
	predictionTablesReady = true
	return nil
}

func validatePredictionRequest(scene, stockScope, startDate, endDate string) error {
	if scene == "" {
		return fmt.Errorf("请选择投资场景")
	}
	if stockScope == "" {
		return fmt.Errorf("请选择股票池")
	}

	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return fmt.Errorf("回测开始日期格式无效: %s", startDate)
	}
	end, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		return fmt.Errorf("回测结束日期格式无效: %s", endDate)
	}
	if start.After(end) {
		return fmt.Errorf("回测开始日期不能晚于结束日期")
	}
	return nil
}

// CreateSession 创建预测会话并生成假设
func (s *PredictionService) CreateSession(
	scene string,
	stockScope string,
	startDate string,
	endDate string,
	aiConfigId int,
) (*models.PredictionSession, []models.PredictionHypothesis, error) {
	if err := ensurePredictionTables(); err != nil {
		return nil, nil, fmt.Errorf("初始化预测工厂表失败: %w", err)
	}
	if err := validatePredictionRequest(scene, stockScope, startDate, endDate); err != nil {
		return nil, nil, err
	}

	session := &models.PredictionSession{
		Scene:      scene,
		StockScope: stockScope,
		StartDate:  startDate,
		EndDate:    endDate,
		Status:     "running",
	}

	if err := db.Dao.Create(session).Error; err != nil {
		return nil, nil, fmt.Errorf("创建会话失败: %w", err)
	}

	// 获取股票池
	poolService := NewStockPoolService()
	universe := poolService.GetStockPool(stockScope)
	if len(universe) == 0 {
		session.Status = "failed"
		db.Dao.Save(session)
		return nil, nil, fmt.Errorf("股票池为空，请检查自选股或股票代码是否有效")
	}
	queryUniverse := universe
	if isAllStockScope(stockScope) {
		queryUniverse = nil
	}

	// 检查当前股票池在回测区间内是否已有特征数据
	var featureCount int64
	featureQuery := db.Dao.Model(&models.StockFeature{}).Where("date >= ? AND date <= ?", startDate, endDate)
	if !isAllStockScope(stockScope) {
		featureQuery = featureQuery.Where("stock_code IN ?", universe)
	}
	featureQuery.Count(&featureCount)
	if featureCount == 0 {
		session.Status = "failed"
		db.Dao.Save(session)
		return nil, nil, fmt.Errorf("当前股票池在回测区间内没有特征数据，请先点击「同步特征数据」")
	}

	// 生成假设
	ctx := MarketContext{
		MarketState:   "震荡市",
		ShIndexReturn: 0,
	}
	hypotheses, err := s.aiGenerator.GenerateWithAI(scene, stockScope, ctx, aiConfigId)
	if err != nil {
		session.Status = "failed"
		db.Dao.Save(session)
		return nil, nil, fmt.Errorf("生成假设失败: %w", err)
	}
	if len(hypotheses) == 0 {
		session.Status = "failed"
		db.Dao.Save(session)
		return nil, nil, fmt.Errorf("未生成任何预测假设，请尝试更换场景或股票池")
	}

	// 对每个假设做回测验证
	var resultHypotheses []models.PredictionHypothesis
	config := DefaultBacktestConfig()
	for _, h := range hypotheses {
		h = NormalizeHypothesis(h)
		source := h.Source
		if source == "" {
			source = "ai"
		}
		rawRuleJSON, _ := json.Marshal(h.Rule)
		validationErrors := ValidateHypothesisRule(h.Rule)
		_ = RecordGenerationAudit(session.ID, source, string(rawRuleJSON), validationErrors)
		if len(validationErrors) > 0 {
			logger.SugaredLogger.Warnf("skip hypothesis %s: rule validation failed: %+v", h.ID, validationErrors)
			continue
		}

		result, err := s.validator.ValidateWithConfig(
			h.Rule,
			queryUniverse,
			startDate,
			endDate,
			h.TimeHorizon,
			config,
		)
		if err != nil {
			logger.SugaredLogger.Errorf("validate hypothesis %s error: %v", h.ID, err)
			continue
		}
		if result.TradeCount == 0 {
			logger.SugaredLogger.Warnf("skip hypothesis %s: no trades generated in backtest", h.ID)
			continue
		}

		ruleJSON, _ := json.Marshal(h.Rule)
		backtestConfigJSON, _ := json.Marshal(backtestPayload(config, result))
		ph := models.PredictionHypothesis{
			SessionID:            session.ID,
			Name:                 h.Name,
			Description:          h.Description,
			Scene:                h.Scene,
			RuleJSON:             string(ruleJSON),
			Params:               h.Params,
			TimeHorizon:          h.TimeHorizon,
			TargetReturn:         h.TargetReturn,
			WinRate:              result.WinRate,
			AvgReturn:            result.AvgReturn,
			MaxDrawdown:          result.MaxDrawdown,
			TradeCount:           result.TradeCount,
			TotalReturn:          result.TotalReturn,
			MedianReturn:         result.MedianReturn,
			ProfitLossRatio:      result.ProfitLossRatio,
			OutSampleAvgReturn:   result.OutSampleAvgReturn,
			OutSampleMaxDrawdown: result.OutSampleMaxDrawdown,
			DataCoverage:         result.DataCoverage,
			NoLookaheadPassed:    result.NoLookaheadPassed,
			BacktestConfigJSON:   string(backtestConfigJSON),
			GenerationSource:     source,
			SchemaVersion:        PredictionRuleSchemaV1,
			StrategyVersion:      "strategy_v1",
			FeatureVersion:       config.FeatureVersion,
			Status:               "draft",
		}

		if err := db.Dao.Create(&ph).Error; err != nil {
			logger.SugaredLogger.Errorf("save hypothesis error: %v", err)
			continue
		}

		// 保存每日净值
		for _, d := range result.DailyNAV {
			daily := models.PredictionHypothesisDaily{
				HypothesisID: ph.ID,
				Date:         d.Date,
				Nav:          d.Nav,
				Drawdown:     d.Drawdown,
				TradeCount:   d.TradeCount,
			}
			db.Dao.Create(&daily)
		}

		for _, t := range result.Trades {
			trade := models.PredictionTrade{
				HypothesisID:    ph.ID,
				StockCode:       t.StockCode,
				StockName:       t.StockName,
				SignalDate:      t.SignalDate,
				BuyDate:         t.BuyDate,
				SellDate:        t.SellDate,
				BuyPrice:        t.BuyPrice,
				SellPrice:       t.SellPrice,
				Fee:             t.Fee,
				Slippage:        t.Slippage,
				ReturnRate:      t.ReturnRate,
				MaxReturn:       t.MaxReturn,
				MaxDrawdown:     t.MaxDrawdown,
				HoldDays:        t.HoldDays,
				EntryReasonJSON: t.EntryReasonJSON,
				ExitReason:      t.ExitReason,
				FeatureVersion:  t.FeatureVersion,
				DataAsOf:        t.DataAsOf,
			}
			db.Dao.Create(&trade)
		}

		resultHypotheses = append(resultHypotheses, ph)
	}

	if len(resultHypotheses) == 0 {
		session.Status = "failed"
		db.Dao.Save(session)
		return nil, nil, fmt.Errorf("回测未产生有效假设，请检查特征数据或调整回测区间")
	}

	session.Status = "done"
	db.Dao.Save(session)
	s.generateSessionDecisions(session, resultHypotheses, universe, endDate)

	return session, resultHypotheses, nil
}

func backtestPayload(config BacktestConfig, result *ValidationResult) map[string]any {
	if result == nil {
		return map[string]any{"config": config}
	}
	quality := "weak"
	switch {
	case !result.NoLookaheadPassed:
		quality = "blocked"
	case result.TradeCount >= 30 && result.AvgReturn > 0 && result.MaxDrawdown <= 0.20 && result.OutSampleAvgReturn >= -0.02:
		quality = "reference"
	case result.TradeCount >= 10 && result.AvgReturn > 0:
		quality = "observe"
	}
	return map[string]any{
		"config": config,
		"metrics": map[string]any{
			"qualityRating":        quality,
			"annualizedReturn":     result.AnnualizedReturn,
			"benchmarkCode":        result.BenchmarkCode,
			"benchmarkReturn":      result.BenchmarkReturn,
			"excessReturn":         result.ExcessReturn,
			"turnoverRate":         result.TurnoverRate,
			"averageHoldingDays":   result.AverageHoldingDays,
			"sampleCount":          result.TradeCount,
			"outSampleAvgReturn":   result.OutSampleAvgReturn,
			"outSampleMaxDrawdown": result.OutSampleMaxDrawdown,
			"dataCoverage":         result.DataCoverage,
			"noLookaheadPassed":    result.NoLookaheadPassed,
		},
	}
}

// GetSession 获取会话详情
func (s *PredictionService) GetSession(sessionID uint) (*models.PredictionSession, []models.PredictionHypothesis, error) {
	if err := ensurePredictionTables(); err != nil {
		return nil, nil, fmt.Errorf("初始化预测工厂表失败: %w", err)
	}

	var session models.PredictionSession
	if err := db.Dao.First(&session, sessionID).Error; err != nil {
		return nil, nil, err
	}

	var hypotheses []models.PredictionHypothesis
	db.Dao.Where("session_id = ?", sessionID).Find(&hypotheses)

	return &session, hypotheses, nil
}

// GetMyHypotheses 获取用户的所有假设
func (s *PredictionService) GetMyHypotheses() []models.PredictionHypothesis {
	if err := ensurePredictionTables(); err != nil {
		logger.SugaredLogger.Errorf("ensure prediction tables error: %v", err)
		return nil
	}

	var hypotheses []models.PredictionHypothesis
	db.Dao.Where("status IN ?", []string{"active", "watch"}).Order("created_at desc").Find(&hypotheses)
	return hypotheses
}

// GetRecentSessions 获取最近预测历史。
func (s *PredictionService) GetRecentSessions(limit int) []models.PredictionSession {
	if err := ensurePredictionTables(); err != nil {
		logger.SugaredLogger.Errorf("ensure prediction tables error: %v", err)
		return nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var sessions []models.PredictionSession
	db.Dao.Order("created_at desc").Limit(limit).Find(&sessions)
	return sessions
}

// GetSessionDecisions 获取一次预测会话下的个股操作建议。
func (s *PredictionService) GetSessionDecisions(sessionID uint) []models.PredictionDecision {
	if err := ensurePredictionTables(); err != nil {
		logger.SugaredLogger.Errorf("ensure prediction tables error: %v", err)
		return nil
	}
	var decisions []models.PredictionDecision
	db.Dao.Where("session_id = ?", sessionID).
		Order("score desc, stock_code asc").
		Find(&decisions)
	return decisions
}

// SaveObservation 保存为观察，不要求达到正式监控阈值。
func (s *PredictionService) SaveObservation(hypothesisID uint) error {
	if err := ensurePredictionTables(); err != nil {
		return fmt.Errorf("初始化预测工厂表失败: %w", err)
	}

	var hypothesis models.PredictionHypothesis
	if err := db.Dao.First(&hypothesis, hypothesisID).Error; err != nil {
		return fmt.Errorf("预测假设不存在")
	}
	if hypothesis.Status == "active" {
		return nil
	}
	result := db.Dao.Model(&models.PredictionHypothesis{}).
		Where("id = ?", hypothesisID).
		Update("status", "watch")
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("预测假设不存在")
	}
	return nil
}

// SaveHypothesis 保存假设为监控
func (s *PredictionService) SaveHypothesis(hypothesisID uint) error {
	if err := ensurePredictionTables(); err != nil {
		return fmt.Errorf("初始化预测工厂表失败: %w", err)
	}

	var hypothesis models.PredictionHypothesis
	if err := db.Dao.First(&hypothesis, hypothesisID).Error; err != nil {
		return fmt.Errorf("预测假设不存在")
	}
	if !hypothesis.NoLookaheadPassed {
		return fmt.Errorf("策略未通过未来函数检查，不能保存监控")
	}
	if hypothesis.TradeCount < 30 {
		return fmt.Errorf("样本不足，仅供观察，不建议保存监控")
	}
	if hypothesis.MaxDrawdown > 0.20 {
		return fmt.Errorf("最大回撤超过 20%%，不能保存监控")
	}
	if hypothesis.AvgReturn <= 0 {
		return fmt.Errorf("平均收益未通过最低要求，不能保存监控")
	}

	if hypothesis.OutSampleAvgReturn < -0.02 {
		return fmt.Errorf("样本外收益偏弱，不建议启用正式监控")
	}

	result := db.Dao.Model(&models.PredictionHypothesis{}).
		Where("id = ?", hypothesisID).
		Update("status", "active")
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("预测假设不存在")
	}
	return nil
}

// DisableHypothesis 禁用假设
func (s *PredictionService) DisableHypothesis(hypothesisID uint) error {
	if err := ensurePredictionTables(); err != nil {
		return fmt.Errorf("初始化预测工厂表失败: %w", err)
	}

	result := db.Dao.Model(&models.PredictionHypothesis{}).
		Where("id = ?", hypothesisID).
		Update("status", "disabled")
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("预测假设不存在")
	}
	return nil
}

// GetHypothesisStats 获取假设验证统计
func (s *PredictionService) GetHypothesisStats(hypothesisID uint) (*HypothesisStats, error) {
	if err := ensurePredictionTables(); err != nil {
		return nil, fmt.Errorf("初始化预测工厂表失败: %w", err)
	}

	var total int64
	var hitCount int64
	var avgReturn float64

	db.Dao.Model(&models.PredictionSignal{}).
		Where("hypothesis_id = ? AND status = ?", hypothesisID, "validated").
		Count(&total)

	db.Dao.Model(&models.PredictionSignal{}).
		Where("hypothesis_id = ? AND status = ? AND hit = ?", hypothesisID, "validated", true).
		Count(&hitCount)

	if total > 0 {
		var sum float64
		db.Dao.Model(&models.PredictionSignal{}).
			Select("COALESCE(SUM(actual_return), 0)").
			Where("hypothesis_id = ? AND status = ?", hypothesisID, "validated").
			Row().Scan(&sum)
		avgReturn = sum / float64(total)
	}

	hitRate := 0.0
	if total > 0 {
		hitRate = float64(hitCount) / float64(total)
	}

	return &HypothesisStats{
		TotalSignals: int(total),
		HitCount:     int(hitCount),
		HitRate:      hitRate,
		AvgReturn:    avgReturn,
	}, nil
}

// HypothesisStats 假设统计
type HypothesisStats struct {
	TotalSignals int     `json:"totalSignals"`
	HitCount     int     `json:"hitCount"`
	HitRate      float64 `json:"hitRate"`
	AvgReturn    float64 `json:"avgReturn"`
}

// GetHypothesisDailyNAV 获取假设的每日净值
func (s *PredictionService) GetHypothesisDailyNAV(hypothesisID uint) []models.PredictionHypothesisDaily {
	if err := ensurePredictionTables(); err != nil {
		logger.SugaredLogger.Errorf("ensure prediction tables error: %v", err)
		return nil
	}

	var dailies []models.PredictionHypothesisDaily
	db.Dao.Where("hypothesis_id = ?", hypothesisID).Order("date asc").Find(&dailies)
	return dailies
}

func (s *PredictionService) GetBacktestTrades(hypothesisID uint) []models.PredictionTrade {
	if err := ensurePredictionTables(); err != nil {
		logger.SugaredLogger.Errorf("ensure prediction tables error: %v", err)
		return nil
	}
	var trades []models.PredictionTrade
	db.Dao.Where("hypothesis_id = ?", hypothesisID).Order("buy_date asc").Find(&trades)
	return trades
}

func (s *PredictionService) GetGenerationAudit(sessionID uint) []models.PredictionGenerationAudit {
	if err := ensurePredictionTables(); err != nil {
		logger.SugaredLogger.Errorf("ensure prediction tables error: %v", err)
		return nil
	}
	var audits []models.PredictionGenerationAudit
	db.Dao.Where("session_id = ?", sessionID).Order("created_at asc").Find(&audits)
	return audits
}

type matchedStrategyFact struct {
	ID           uint    `json:"id"`
	Name         string  `json:"name"`
	EntryMatched bool    `json:"entryMatched"`
	ExitMatched  bool    `json:"exitMatched"`
	WinRate      float64 `json:"winRate"`
	AvgReturn    float64 `json:"avgReturn"`
	MaxDrawdown  float64 `json:"maxDrawdown"`
	TradeCount   int     `json:"tradeCount"`
	MonitorReady bool    `json:"monitorReady"`
}

type quoteSnapshot struct {
	name  string
	price float64
	date  string
	at    time.Time
}

type holdingSnapshot struct {
	name         string
	costPrice    float64
	volume       int64
	marketValue  float64
	floatingRate float64
}

func (s *PredictionService) generateSessionDecisions(
	session *models.PredictionSession,
	hypotheses []models.PredictionHypothesis,
	universe []string,
	endDate string,
) []models.PredictionDecision {
	if session == nil || len(hypotheses) == 0 || len(universe) == 0 {
		return nil
	}

	features := s.latestFeatureMap(universe, endDate)
	if len(features) == 0 {
		return nil
	}
	quotes := latestQuoteMap(universe)
	holdings := holdingMap(universe)

	_ = db.Dao.Where("session_id = ?", session.ID).Delete(&models.PredictionDecision{}).Error
	decisions := make([]models.PredictionDecision, 0, len(features))
	for _, code := range universe {
		f, ok := features[strings.ToLower(code)]
		if !ok {
			continue
		}
		decision := s.buildDecisionForStock(session, hypotheses, f, quotes, holdings)
		if decision.StockCode == "" {
			continue
		}
		if err := db.Dao.Create(&decision).Error; err != nil {
			logger.SugaredLogger.Errorf("save prediction decision error: %v", err)
			continue
		}
		decisions = append(decisions, decision)
	}
	return decisions
}

func (s *PredictionService) latestFeatureMap(universe []string, endDate string) map[string]models.StockFeature {
	result := make(map[string]models.StockFeature, len(universe))
	for _, code := range universe {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		var feature models.StockFeature
		err := db.Dao.Where("stock_code = ? AND date <= ?", code, endDate).
			Order("date desc").
			First(&feature).Error
		if err == nil {
			result[strings.ToLower(code)] = feature
		}
	}
	return result
}

func latestQuoteMap(universe []string) map[string]quoteSnapshot {
	result := make(map[string]quoteSnapshot, len(universe))
	if len(universe) == 0 {
		return result
	}

	var quotes []data.StockInfo
	db.Dao.Where("code IN ?", universe).Find(&quotes)
	for _, q := range quotes {
		price := parseFloat(q.Price)
		if price <= 0 {
			continue
		}
		at := q.UpdatedAt
		if at.IsZero() && q.Date != "" && q.Time != "" {
			if parsed, err := time.ParseInLocation("2006-01-02 15:04:05", q.Date+" "+q.Time, time.Local); err == nil {
				at = parsed
			}
		}
		result[strings.ToLower(q.Code)] = quoteSnapshot{
			name:  q.Name,
			price: price,
			date:  q.Date,
			at:    at,
		}
	}
	return result
}

func holdingMap(universe []string) map[string]holdingSnapshot {
	result := make(map[string]holdingSnapshot, len(universe))
	if len(universe) == 0 {
		return result
	}

	summaries := data.NewStockDataApi().GetTradingPositionSummaries(universe)
	for _, summary := range summaries {
		result[strings.ToLower(summary.StockCode)] = holdingSnapshot{
			name:         summary.StockName,
			costPrice:    summary.AvgCostPrice,
			volume:       summary.CurrentVolume,
			marketValue:  summary.MarketValue,
			floatingRate: summary.FloatingProfitRate,
		}
	}
	return result
}

func (s *PredictionService) buildDecisionForStock(
	session *models.PredictionSession,
	hypotheses []models.PredictionHypothesis,
	f models.StockFeature,
	quotes map[string]quoteSnapshot,
	holdings map[string]holdingSnapshot,
) models.PredictionDecision {
	codeKey := strings.ToLower(f.StockCode)
	return NewDecisionEngine().Build(DecisionContext{
		Session:    session,
		Hypotheses: hypotheses,
		Feature:    f,
		Quote:      quotes[codeKey],
		Holding:    holdings[codeKey],
	})
}

func decideAction(
	hasPosition bool,
	currentPrice float64,
	defensePrice float64,
	stopLossPrice float64,
	takeProfitPrice float64,
	score float64,
	entryMatchedCount int,
	exitMatchedCount int,
	positiveMatchedCount int,
	negativeStrategyCount int,
	sampleWarning bool,
) (string, float64) {
	if hasPosition {
		if stopLossPrice > 0 && currentPrice <= stopLossPrice {
			return "SELL", 100
		}
		if defensePrice > 0 && currentPrice <= defensePrice {
			return "REDUCE", 50
		}
		if exitMatchedCount >= 2 || score < 42 {
			return "REDUCE", 50
		}
		if takeProfitPrice > 0 && currentPrice >= takeProfitPrice && score < 70 {
			return "REDUCE", 50
		}
		if sampleWarning && entryMatchedCount > 0 && negativeStrategyCount > 0 {
			return "REDUCE", 50
		}
		if score >= 82 && entryMatchedCount > 0 && positiveMatchedCount > 0 && !sampleWarning {
			return "ADD", 20
		}
		return "HOLD", 0
	}

	if score >= 78 && entryMatchedCount > 0 && positiveMatchedCount > 0 && !sampleWarning {
		return "BUY", 20
	}
	if score >= 55 && entryMatchedCount > 0 {
		return "WATCH", 0
	}
	if exitMatchedCount > 0 || score < 45 {
		return "AVOID", 0
	}
	return "WATCH", 0
}

func buildPositionAdvice(action string, quantityPercent float64, hasPosition bool) string {
	switch action {
	case "BUY":
		return fmt.Sprintf("未持仓可试探 %.0f%% 仓位", quantityPercent)
	case "ADD":
		return fmt.Sprintf("已有仓位最多加 %.0f%%，不追高", quantityPercent)
	case "HOLD":
		if hasPosition {
			return "继续持有，按防守位和止损位执行"
		}
		return "暂不操作"
	case "REDUCE":
		return fmt.Sprintf("先减仓 %.0f%%，保留一半观察", quantityPercent)
	case "SELL":
		return "风险线已破，建议全部卖出"
	case "AVOID":
		return "信号质量不足，不建议买入"
	default:
		return "观察，不建议直接交易"
	}
}

func actionText(action string) string {
	switch action {
	case "BUY":
		return "买入"
	case "ADD":
		return "加仓"
	case "HOLD":
		return "持有"
	case "REDUCE":
		return "减仓"
	case "SELL":
		return "全卖"
	case "AVOID":
		return "不建议操作"
	default:
		return "观察"
	}
}

func decisionConfidence(score float64, sampleWarning bool) string {
	if sampleWarning {
		return "low"
	}
	if score >= 75 {
		return "high"
	}
	if score >= 55 {
		return "medium"
	}
	return "low"
}

func hypothesisMonitorReady(h models.PredictionHypothesis) bool {
	return h.NoLookaheadPassed && h.TradeCount >= 30 && h.MaxDrawdown <= 0.20 && h.AvgReturn > 0
}

func parseFloat(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "-" {
		return 0
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return v
}

func clamp(v float64, min float64, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]bool, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
	}
	return result
}

// ScanSignals 扫描今日触发信号
func (s *PredictionService) ScanSignals() {
	if err := ensurePredictionTables(); err != nil {
		logger.SugaredLogger.Errorf("ensure prediction tables error: %v", err)
		return
	}

	var hypotheses []models.PredictionHypothesis
	db.Dao.Where("status = ?", "active").Find(&hypotheses)

	today := time.Now().Format("2006-01-02")
	for _, h := range hypotheses {
		var rule Rule
		if err := json.Unmarshal([]byte(h.RuleJSON), &rule); err != nil {
			continue
		}

		var session models.PredictionSession
		stockScope := "全部A股"
		if err := db.Dao.Select("stock_scope").First(&session, h.SessionID).Error; err == nil && session.StockScope != "" {
			stockScope = session.StockScope
		}

		poolService := NewStockPoolService()
		universe := poolService.GetStockPool(stockScope)
		if len(universe) == 0 {
			logger.SugaredLogger.Warnf("scan prediction signals skipped: hypothesis=%d stock scope %s is empty", h.ID, stockScope)
			continue
		}
		if isAllStockScope(stockScope) {
			universe = nil
		}

		features := s.validator.repo.GetByDate(today, universe)
		for _, f := range features {
			if s.validator.matchConditions(rule.EntryConditions, f) {
				// 检查是否已经存在
				var count int64
				db.Dao.Model(&models.PredictionSignal{}).
					Where("hypothesis_id = ? AND stock_code = ? AND signal_date = ?", h.ID, f.StockCode, today).
					Count(&count)
				if count > 0 {
					continue
				}

				targetDate, _ := time.Parse("2006-01-02", today)
				dataAsOf := f.DataAsOf
				if dataAsOf.IsZero() {
					dataAsOf = time.Now()
				}
				decisionID := fmt.Sprintf("pred-%d-%s-%s", h.ID, f.StockCode, today)
				reasonsJSON, _ := json.Marshal([]string{"active 预测假设入场条件满足"})
				risksJSON, _ := json.Marshal([]string{"仅供观察，不代表盈利概率"})
				signal := models.PredictionSignal{
					HypothesisID: h.ID,
					StockCode:    f.StockCode,
					StockName:    f.StockCode,
					SignalDate:   today,
					EntryPrice:   f.Close,
					TargetDate:   targetDate.AddDate(0, 0, h.TimeHorizon).Format("2006-01-02"),
					TargetReturn: h.TargetReturn,
					Status:       "pending",
					DecisionID:   decisionID,
					DataAsOf:     dataAsOf,
					ReasonsJSON:  string(reasonsJSON),
					RisksJSON:    string(risksJSON),
				}
				db.Dao.Create(&signal)

				metricsJSON, _ := json.Marshal(map[string]any{
					"winRate":      h.WinRate,
					"avgReturn":    h.AvgReturn,
					"maxDrawdown":  h.MaxDrawdown,
					"tradeCount":   h.TradeCount,
					"outSampleAvg": h.OutSampleAvgReturn,
				})
				matchedFactsJSON, _ := json.Marshal(map[string]any{
					"close":       f.Close,
					"ma5":         f.MA5,
					"ma20":        f.MA20,
					"volumeRatio": f.VolumeRatio,
				})
				_ = db.Dao.Create(&models.TradeDecisionLog{
					DecisionID:       decisionID,
					StockCode:        f.StockCode,
					StockName:        f.StockCode,
					Action:           "buy_watch",
					Score:            s.calculateSignalScore(h, f),
					ScoreType:        "heuristic",
					CurrentPrice:     f.Close,
					ReasonsJSON:      string(reasonsJSON),
					RisksJSON:        string(risksJSON),
					MatchedFactsJSON: string(matchedFactsJSON),
					MetricsJSON:      string(metricsJSON),
					StrategyID:       h.ID,
					StrategyVersion:  h.StrategyVersion,
					FeatureVersion:   f.FeatureVersion,
					SignalDate:       today,
					DataAsOf:         dataAsOf,
					ValidUntil:       targetDate.AddDate(0, 0, 1),
					CreatedAt:        time.Now(),
				}).Error
			}
		}
	}
}

func (s *PredictionService) calculateSignalScore(h models.PredictionHypothesis, f models.StockFeature) float64 {
	score := 50.0
	if h.OutSampleAvgReturn > 0 {
		score += 15
	}
	if h.WinRate >= 0.5 {
		score += 10
	}
	if h.MaxDrawdown <= 0.15 {
		score += 10
	}
	if f.VolumeRatio >= 1.2 {
		score += 5
	}
	if f.DataAsOf.IsZero() {
		score -= 10
	}
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

// DailyValidateSignals 每日验证 pending 信号
func (s *PredictionService) DailyValidateSignals() {
	if err := ensurePredictionTables(); err != nil {
		logger.SugaredLogger.Errorf("ensure prediction tables error: %v", err)
		return
	}

	var signals []models.PredictionSignal
	db.Dao.Where("status = ?", "pending").Find(&signals)

	today := time.Now().Format("2006-01-02")
	for _, signal := range signals {
		if signal.TargetDate <= today {
			// 获取目标日期收盘价
			feature, ok := s.validator.repo.GetFirstOnOrAfter(signal.StockCode, signal.TargetDate)
			if ok {
				exitPrice := feature.Close
				if signal.EntryPrice > 0 {
					signal.ActualReturn = (exitPrice - signal.EntryPrice) / signal.EntryPrice
				}
				signal.Hit = signal.ActualReturn >= signal.TargetReturn
			}
			signal.Status = "validated"
			signal.ValidatedAt = time.Now()
			db.Dao.Save(&signal)
		}
	}
}
