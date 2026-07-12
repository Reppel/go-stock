package backtest

import (
	"encoding/json"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"gorm.io/gorm"
	"math"
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
	predictionDecisionsMu sync.Mutex
	predictionTablesReady bool
)

var monitoredHypothesisStatuses = []string{"active", "active_candidate", "paper_trade", "watch"}

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
		&models.PredictionPaperAccount{},
		&models.PredictionPaperPosition{},
		&models.PredictionPaperTrade{},
		&models.PredictionPaperDaily{},
		&models.StockFeature{},
		&models.FeatureSyncJob{},
		&models.PredictionTrade{},
		&models.TradeDecisionLog{},
		&models.PredictionGenerationAudit{},
		&models.PredictionResearchIdea{},
		&models.MarketFactorDaily{},
		&models.StockMoneyFlowDaily{},
		&models.SectorFlowDaily{},
		&models.StockEventDaily{},
		&models.StockRiskEvent{},
		&models.CandidateSnapshot{},
		&models.CandidateSnapshotItem{},
		&models.CandidateSourceFact{},
		&models.StockScreeningFactDaily{},
		&models.UplimitStockDaily{},
		&models.ModelRecommendationEvent{},
		&models.ScreeningExecutionSnapshot{},
		&models.ScreeningExecutionItem{},
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
		Scene:               scene,
		StockScope:          stockScope,
		CandidateSnapshotID: candidateSnapshotIDFromScope(stockScope),
		StartDate:           startDate,
		EndDate:             endDate,
		Status:              "running",
	}
	if session.CandidateSnapshotID > 0 {
		session.UniverseSelectionMode = "point_in_time_candidate"
		var candidateSnapshot models.CandidateSnapshot
		if err := db.Dao.First(&candidateSnapshot, session.CandidateSnapshotID).Error; err != nil {
			return nil, nil, fmt.Errorf("候选快照不存在")
		}
		if err := validateCandidateSnapshotForSession(candidateSnapshot, endDate); err != nil {
			return nil, nil, err
		}
		session.SelectionAsOf = candidateSnapshot.AvailableAt
	} else {
		session.UniverseSelectionMode = "research_universe"
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
	universe = uniqueStrings(universe)
	universeJSON, _ := json.Marshal(universe)
	session.UniverseJSON = string(universeJSON)
	db.Dao.Save(session)
	queryUniverse := universe
	if isAllStockScope(stockScope) {
		queryUniverse = nil
	}

	// 检查当前股票池在回测区间内是否已有当前版本的完整前复权特征。
	var featureCount int64
	featureQuery := db.Dao.Model(&models.StockFeature{}).
		Where("date >= ? AND date <= ? AND feature_version = ? AND adjusted = ?", startDate, endDate, CurrentFeatureVersion, true)
	if !isAllStockScope(stockScope) {
		featureQuery = featureQuery.Where("stock_code IN ?", universe)
	}
	featureQuery.Count(&featureCount)
	if featureCount == 0 {
		session.Status = "failed"
		db.Dao.Save(session)
		return nil, nil, fmt.Errorf("当前股票池在回测区间内没有特征数据，请先点击「同步特征数据」")
	}
	coverage := NewFeatureSyncService().GetFeatureCoverageForScopeByDates(stockScope, startDate, endDate)
	if !coverage.Ready {
		session.Status = "failed"
		session.ErrorMsg = coverage.Message
		db.Dao.Save(session)
		return nil, nil, fmt.Errorf("%s，请按当前回测区间重新同步特征数据", coverage.Message)
	}

	// 生成假设只能读取研究窗口，验证窗口不得反向影响市场状态或 ResearchIdea。
	researchEndDate := endDate
	tradingDays := s.validator.getTradingDays(startDate, endDate, CurrentFeatureVersion, DefaultBacktestConfig().BenchmarkCode)
	if len(tradingDays) >= 10 {
		cutoff := int(math.Floor(float64(len(tradingDays))*0.4)) - 1
		if cutoff < 0 {
			cutoff = 0
		}
		researchEndDate = tradingDays[cutoff]
	}
	benchmarkReturn, benchmarkReady := s.validator.calculateBenchmarkReturn(DefaultBacktestConfig().BenchmarkCode, startDate, researchEndDate, CurrentFeatureVersion)
	marketState := "震荡市"
	if benchmarkReady && benchmarkReturn >= 0.05 {
		marketState = "上涨趋势"
	} else if benchmarkReady && benchmarkReturn <= -0.05 {
		marketState = "下跌趋势"
	}
	ctx := MarketContext{
		MarketState:   marketState,
		ShIndexReturn: benchmarkReturn,
	}
	session.ResearchEndDate = researchEndDate
	session.AIConfigID = aiConfigId
	session.MarketState = marketState
	session.MarketReturn = benchmarkReturn
	db.Dao.Save(session)
	researchRows, researchIdeas := NewResearchService().GenerateIdeas(session.ID, scene, stockScope, startDate, researchEndDate, queryUniverse)
	researchIdeaIDs := make([]uint, 0, len(researchRows))
	for _, row := range researchRows {
		if row.ID > 0 {
			researchIdeaIDs = append(researchIdeaIDs, row.ID)
		}
	}
	hypotheses, err := s.aiGenerator.GenerateWithAIWithResearch(scene, stockScope, ctx, aiConfigId, researchIdeas)
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
	researchToolCalls, _ := json.Marshal(map[string]any{
		"tool": "standard_research_templates", "schemaVersion": ResearchIdeaSchemaV2,
		"researchIdeaIds": researchIdeaIDs, "researchEndDate": researchEndDate,
	})
	for _, h := range hypotheses {
		h = NormalizeHypothesis(h)
		source := h.Source
		if source == "" {
			source = "ai"
		}
		ruleJSON, envelope := RuleEnvelopeJSON(h, researchIdeaIDs)
		envelope.ResearchIdeas = researchIdeas
		normalizedDSL, _ := json.Marshal(envelope)
		validationErrors := ValidateHypothesisRule(h.Rule)
		_ = RecordGenerationAuditV2(GenerationAuditPayload{
			SessionID:        session.ID,
			Source:           source,
			Prompt:           s.aiGenerator.lastPrompt,
			RawOutput:        firstNonBlank(s.aiGenerator.lastRawOutput, ruleJSON),
			NormalizedDSL:    string(normalizedDSL),
			ToolCallsJSON:    string(researchToolCalls),
			AIConfigID:       aiConfigId,
			ModelName:        s.aiGenerator.lastModelName,
			Temperature:      s.aiGenerator.lastTemperature,
			GenerationError:  s.aiGenerator.lastGenerationError,
			ValidationErrors: validationErrors,
		})
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
		ApplyResearchEvidenceToVerdict(result, researchIdeas)
		if session.CandidateSnapshotID > 0 {
			ApplyCandidateDiagnosticVerdict(result, config)
		}

		backtestConfigJSON, _ := json.Marshal(backtestPayload(config, result))
		verdictJSON, _ := json.Marshal(result.Verdict)
		ph := models.PredictionHypothesis{
			SessionID:              session.ID,
			UniverseSelectionMode:  session.UniverseSelectionMode,
			BacktestDiagnosticOnly: session.CandidateSnapshotID > 0,
			Name:                   h.Name,
			Description:            h.Description,
			Scene:                  h.Scene,
			RuleJSON:               ruleJSON,
			Params:                 h.Params,
			TimeHorizon:            h.TimeHorizon,
			TargetReturn:           h.TargetReturn,
			WinRate:                result.WinRate,
			AvgReturn:              result.AvgReturn,
			MaxDrawdown:            result.MaxDrawdown,
			TradeCount:             result.TradeCount,
			TotalReturn:            result.TotalReturn,
			MedianReturn:           result.MedianReturn,
			ProfitLossRatio:        result.ProfitLossRatio,
			ProfitLossRatioStatus:  result.ProfitLossRatioStatus,
			OutSampleAvgReturn:     result.OutSampleAvgReturn,
			OutSampleMaxDrawdown:   result.OutSampleMaxDrawdown,
			OutSampleTradeCount:    result.OutSampleTradeCount,
			BenchmarkAvailable:     result.BenchmarkAvailable,
			BenchmarkReturn:        result.BenchmarkReturn,
			ExcessReturn:           result.ExcessReturn,
			DataCoverage:           result.DataCoverage,
			NoLookaheadPassed:      result.NoLookaheadPassed,
			BacktestConfigJSON:     string(backtestConfigJSON),
			VerdictJSON:            string(verdictJSON),
			GenerationSource:       source,
			SchemaVersion:          CurrentPredictionRuleSchema,
			RegistryVersion:        CurrentIndicatorRegistry,
			EngineVersion:          CurrentEngineVersion,
			StrategyVersion:        CurrentStrategyVersion,
			FeatureVersion:         config.FeatureVersion,
			LastVerdictStatus:      result.Verdict.Status,
			ReviewDueAt:            reviewDueAtForVerdict(result.Verdict, config),
			Status:                 "draft",
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
				StockName:       stockNameOrCode(t.StockCode, t.StockName),
				SignalDate:      t.SignalDate,
				BuyDate:         t.BuyDate,
				SellDate:        t.SellDate,
				BuyPrice:        t.BuyPrice,
				SellPrice:       t.SellPrice,
				Quantity:        t.Quantity,
				GrossBuyAmount:  t.GrossBuyAmount,
				GrossSellAmount: t.GrossSellAmount,
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
	if _, err := s.generateSessionDecisions(session, resultHypotheses, universe, endDate); err != nil {
		logger.SugaredLogger.Errorf("generate prediction decisions for session %d error: %v", session.ID, err)
	}

	return session, resultHypotheses, nil
}

func validateCandidateSnapshotForSession(snapshot models.CandidateSnapshot, endDate string) error {
	if snapshot.AvailableAt.IsZero() {
		return fmt.Errorf("候选快照缺少选择时点，不能用于验证")
	}
	if snapshot.Status != "candidate" && snapshot.Status != "validated" {
		return fmt.Errorf("候选快照状态为 %s，不能用于验证", snapshot.Status)
	}
	if strings.TrimSpace(snapshot.TradeDate) == "" || endDate > snapshot.TradeDate {
		return fmt.Errorf("历史诊断区间结束日不能晚于候选快照数据日 %s", snapshot.TradeDate)
	}
	return nil
}

func backtestPayload(config BacktestConfig, result *ValidationResult) map[string]any {
	if result == nil {
		return map[string]any{"config": config}
	}
	quality := "weak"
	switch {
	case !result.NoLookaheadPassed:
		quality = "blocked"
	case result.TradeCount >= MinPoolStrategySamples && result.OutSampleTradeCount >= 15 &&
		result.AvgReturn > 0 && result.MaxDrawdown <= 0.20 && result.OutSampleAvgReturn > 0 &&
		result.DataCoverage >= config.MinDataCoverage && result.BenchmarkAvailable && result.ExcessReturn > 0:
		quality = "reference"
	case result.TradeCount >= 10 && result.AvgReturn > 0:
		quality = "observe"
	}
	return map[string]any{
		"config": config,
		"metrics": map[string]any{
			"qualityRating":         quality,
			"annualizedReturn":      result.AnnualizedReturn,
			"annualizedVolatility":  result.AnnualizedVolatility,
			"sharpeRatio":           result.SharpeRatio,
			"sortinoRatio":          result.SortinoRatio,
			"calmarRatio":           result.CalmarRatio,
			"maxSingleTradeLoss":    result.MaxSingleTradeLoss,
			"benchmarkCode":         result.BenchmarkCode,
			"benchmarkReturn":       result.BenchmarkReturn,
			"benchmarkAvailable":    result.BenchmarkAvailable,
			"excessReturn":          result.ExcessReturn,
			"turnoverRate":          result.TurnoverRate,
			"turnover":              result.Turnover,
			"averageHoldingDays":    result.AverageHoldingDays,
			"profitLossRatioStatus": result.ProfitLossRatioStatus,
			"sampleCount":           result.TradeCount,
			"outSampleAvgReturn":    result.OutSampleAvgReturn,
			"outSampleMaxDrawdown":  result.OutSampleMaxDrawdown,
			"outSampleTradeCount":   result.OutSampleTradeCount,
			"walkForwardFolds":      result.WalkForwardFolds,
			"costSensitivity":       result.CostSensitivity,
			"overfitDiagnostics":    result.OverfitDiagnostics,
			"verdict":               result.Verdict,
			"dataCoverage":          result.DataCoverage,
			"noLookaheadPassed":     result.NoLookaheadPassed,
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
	db.Dao.Where("status IN ?", []string{"active", "watch", "paper_trade", "active_candidate"}).Order("created_at desc").Find(&hypotheses)
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
	return s.refreshAfterMonitorStatusChange(hypothesis.SessionID)
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
	if hypothesis.StrategyVersion != CurrentStrategyVersion {
		return fmt.Errorf("该结果使用旧版回测口径，请重新生成预测后再启用正式监控")
	}
	if hypothesis.FeatureVersion != CurrentFeatureVersion {
		return fmt.Errorf("该结果使用旧版或非复权特征，请同步数据并重新回测后再启用正式监控")
	}
	if !hypothesis.NoLookaheadPassed {
		return fmt.Errorf("策略未通过未来函数检查，不能保存监控")
	}
	if !hypothesis.BacktestDiagnosticOnly && hypothesis.TradeCount < MinPoolStrategySamples {
		return fmt.Errorf("样本不足，仅供观察，不建议保存监控")
	}
	if !hypothesis.BacktestDiagnosticOnly && hypothesis.MaxDrawdown > 0.20 {
		return fmt.Errorf("最大回撤超过 20%%，不能保存监控")
	}
	if !hypothesis.BacktestDiagnosticOnly && hypothesis.AvgReturn <= 0 {
		return fmt.Errorf("平均收益未通过最低要求，不能保存监控")
	}
	if hypothesis.DataCoverage < DefaultBacktestConfig().MinDataCoverage {
		return fmt.Errorf("特征覆盖率不足，不能保存监控")
	}
	if !hypothesis.BacktestDiagnosticOnly && !hypothesis.BenchmarkAvailable {
		return fmt.Errorf("基准数据缺失，无法确认超额收益，不能启用正式监控")
	}
	if !hypothesis.BacktestDiagnosticOnly && hypothesis.ExcessReturn <= 0 {
		return fmt.Errorf("策略未取得正超额收益，不能进入正式模拟盘")
	}

	if hypothesis.LastVerdictStatus != "" && hypothesis.LastVerdictStatus != "active_candidate" && hypothesis.LastVerdictStatus != "active" && hypothesis.LastVerdictStatus != "paper_trade" {
		return fmt.Errorf("量化裁决状态为 %s，不能直接启用 active，请先完成观察或模拟盘", hypothesis.LastVerdictStatus)
	}

	now := time.Now()
	if hypothesis.Status == "paper_trade" {
		return nil
	}

	// 回测通过只能开始独立模拟账户的前向验证，正式监控需要用户再次确认。
	if err := db.Dao.Transaction(func(tx *gorm.DB) error {
		hypothesis.Status = "paper_trade"
		if hypothesis.PaperTradeStartedAt == nil {
			hypothesis.PaperTradeStartedAt = &now
		}
		account, err := ensurePaperAccount(tx, &hypothesis)
		if err != nil {
			return err
		}
		ready, reason := paperAccountReady(hypothesis, *account)
		if err := tx.Model(&models.PredictionHypothesis{}).Where("id = ?", hypothesisID).Updates(map[string]any{
			"status": "paper_trade", "last_verdict_status": "paper_trade",
			"paper_trade_started_at": hypothesis.PaperTradeStartedAt, "review_due_at": nil,
			"paper_trade_days": account.TradingDays, "paper_trade_count": account.TradeCount,
			"paper_nav": account.Nav, "paper_max_drawdown": account.MaxDrawdown,
			"paper_cash": account.Cash, "paper_market_value": account.MarketValue,
			"paper_position_count": account.PositionCount, "paper_ready": ready,
			"paper_ready_reason": reason, "paper_last_processed_date": account.LastProcessedDate,
		}).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	return s.refreshAfterMonitorStatusChange(hypothesis.SessionID)
}

func paperTradeReady(hypothesis models.PredictionHypothesis, now time.Time) (bool, string) {
	_ = now // 门槛按真实交易日快照计数，不再按日历时间推算。
	account := models.PredictionPaperAccount{
		TradingDays: hypothesis.PaperTradeDays, TradeCount: hypothesis.PaperTradeCount,
		Nav: hypothesis.PaperNav, MaxDrawdown: hypothesis.PaperMaxDrawdown,
	}
	return paperAccountReady(hypothesis, account)
}

// ActivateHypothesis 在前向验证达标后，由用户明确确认启用正式监控。
func (s *PredictionService) ActivateHypothesis(hypothesisID uint) error {
	if err := ensurePredictionTables(); err != nil {
		return err
	}
	var hypothesis models.PredictionHypothesis
	if err := db.Dao.First(&hypothesis, hypothesisID).Error; err != nil {
		return fmt.Errorf("预测假设不存在")
	}
	if hypothesis.Status == "active" {
		return nil
	}
	if hypothesis.Status != "paper_trade" {
		return fmt.Errorf("请先开始前向验证")
	}
	ready, reason := paperTradeReady(hypothesis, time.Now())
	if !ready {
		return fmt.Errorf("前向验证尚未达到正式监控门槛: %s", reason)
	}
	reviewDue := time.Now().AddDate(0, 0, DefaultBacktestConfig().ActiveTTLDays)
	if err := db.Dao.Model(&models.PredictionHypothesis{}).Where("id = ?", hypothesisID).Updates(map[string]any{
		"status": "active", "last_verdict_status": "active", "review_due_at": &reviewDue,
	}).Error; err != nil {
		return err
	}
	return s.refreshAfterMonitorStatusChange(hypothesis.SessionID)
}

// DisableHypothesis 禁用假设
func (s *PredictionService) DisableHypothesis(hypothesisID uint) error {
	if err := ensurePredictionTables(); err != nil {
		return fmt.Errorf("初始化预测工厂表失败: %w", err)
	}

	var hypothesis models.PredictionHypothesis
	if err := db.Dao.First(&hypothesis, hypothesisID).Error; err != nil {
		return fmt.Errorf("预测假设不存在")
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
	return s.refreshAfterMonitorStatusChange(hypothesis.SessionID)
}

func (s *PredictionService) refreshAfterMonitorStatusChange(sessionID uint) error {
	var monitored int64
	if err := db.Dao.Model(&models.PredictionHypothesis{}).
		Where("session_id = ? AND status IN ?", sessionID, monitoredHypothesisStatuses).
		Count(&monitored).Error; err != nil {
		return err
	}
	if monitored == 0 {
		return nil
	}
	featureDate, err := latestAdjustedFeatureDate()
	if err == nil && featureDate != "" {
		_, err = s.refreshMonitoredSessionDecisions(sessionID, featureDate)
	}
	if err == nil && featureDate != "" {
		return nil
	}
	_ = db.Dao.Where("session_id = ?", sessionID).Delete(&models.PredictionDecision{}).Error
	if err == nil {
		err = fmt.Errorf("no adjusted feature is available")
	}
	return fmt.Errorf("策略状态已更新，但操作建议刷新失败，盘中提醒已暂停: %w", err)
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
	ID              uint    `json:"id"`
	Name            string  `json:"name"`
	EntryMatched    bool    `json:"entryMatched"`
	ExitMatched     bool    `json:"exitMatched"`
	WinRate         float64 `json:"winRate"`
	AvgReturn       float64 `json:"avgReturn"`
	MaxDrawdown     float64 `json:"maxDrawdown"`
	TradeCount      int     `json:"tradeCount"`
	PoolTradeCount  int     `json:"poolTradeCount"`
	StockTradeCount int     `json:"stockTradeCount"`
	SampleReady     bool    `json:"sampleReady"`
	MonitorReady    bool    `json:"monitorReady"`
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
) ([]models.PredictionDecision, error) {
	if session == nil || len(hypotheses) == 0 || len(universe) == 0 {
		return nil, fmt.Errorf("missing session, monitored strategy, or stock universe")
	}
	predictionDecisionsMu.Lock()
	defer predictionDecisionsMu.Unlock()

	features := s.latestFeatureMap(universe, endDate)
	if len(features) == 0 {
		return nil, fmt.Errorf("no adjusted feature is available on or before %s", endDate)
	}
	quotes := latestQuoteMap(universe)
	holdings := holdingMap(universe)
	previousFeatures := s.previousFeatureMap(features)
	previousActions := latestDecisionActionMap(universe, session.ID)

	decisions := make([]models.PredictionDecision, 0, len(features))
	for _, code := range universe {
		f, ok := features[strings.ToLower(code)]
		if !ok {
			continue
		}
		decision := s.buildDecisionForStock(session, hypotheses, f, previousFeatures, previousActions, quotes, holdings)
		if decision.StockCode == "" {
			continue
		}
		decisions = append(decisions, decision)
	}
	if len(decisions) == 0 {
		return nil, fmt.Errorf("no prediction decision was generated for session %d", session.ID)
	}
	if err := db.Dao.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("session_id = ?", session.ID).Delete(&models.PredictionDecision{}).Error; err != nil {
			return err
		}
		return tx.CreateInBatches(&decisions, 25).Error
	}); err != nil {
		return nil, err
	}
	return decisions, nil
}

func (s *PredictionService) latestFeatureMap(universe []string, endDate string) map[string]models.StockFeature {
	result := make(map[string]models.StockFeature, len(universe))
	for _, code := range universe {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		var feature models.StockFeature
		err := db.Dao.Where("stock_code = ? AND date <= ? AND feature_version = ? AND adjusted = ?", code, endDate, CurrentFeatureVersion, true).
			Order("date desc").
			First(&feature).Error
		if err == nil {
			result[strings.ToLower(code)] = feature
		}
	}
	return result
}

func (s *PredictionService) previousFeatureMap(current map[string]models.StockFeature) map[string]models.StockFeature {
	result := make(map[string]models.StockFeature, len(current))
	for key, feature := range current {
		var previous models.StockFeature
		if db.Dao.Where("stock_code = ? AND date < ? AND feature_version = ?", feature.StockCode, feature.Date, CurrentFeatureVersion).
			Order("date desc").First(&previous).Error == nil {
			result[key] = previous
		}
	}
	return result
}

func latestDecisionActionMap(universe []string, sessionID uint) map[string]string {
	result := make(map[string]string, len(universe))
	var decisions []models.PredictionDecision
	db.Dao.Where("stock_code IN ? AND session_id = ?", universe, sessionID).
		Order("created_at desc").Find(&decisions)
	for _, decision := range decisions {
		key := strings.ToLower(decision.StockCode)
		if _, exists := result[key]; !exists {
			result[key] = decision.Action
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
	previousFeatures map[string]models.StockFeature,
	previousActions map[string]string,
	quotes map[string]quoteSnapshot,
	holdings map[string]holdingSnapshot,
) models.PredictionDecision {
	codeKey := strings.ToLower(f.StockCode)
	var previous *models.StockFeature
	if value, ok := previousFeatures[codeKey]; ok {
		previous = &value
	}
	return NewDecisionEngine().Build(DecisionContext{
		Session:         session,
		Hypotheses:      hypotheses,
		Feature:         f,
		PreviousFeature: previous,
		Quote:           quotes[codeKey],
		Holding:         holdings[codeKey],
		PreviousAction:  previousActions[codeKey],
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

func decisionConfidence(probability float64, stockSampleCount int, sampleWarning bool) string {
	if sampleWarning || stockSampleCount < MinStockStrategySamples {
		return "low"
	}
	distance := math.Abs(probability - 0.5)
	if stockSampleCount >= 30 && distance >= 0.15 {
		return "high"
	}
	if distance >= 0.07 {
		return "medium"
	}
	return "low"
}

func hypothesisMonitorReady(h models.PredictionHypothesis) bool {
	return h.StrategyVersion == CurrentStrategyVersion && h.FeatureVersion == CurrentFeatureVersion &&
		(h.EngineVersion == "" || h.EngineVersion == CurrentEngineVersion) &&
		h.NoLookaheadPassed && h.TradeCount >= MinPoolStrategySamples && h.OutSampleTradeCount >= 15 &&
		h.MaxDrawdown <= 0.20 && h.AvgReturn > 0 && h.OutSampleAvgReturn > 0 &&
		h.DataCoverage >= DefaultBacktestConfig().MinDataCoverage && h.BenchmarkAvailable && h.ExcessReturn > 0 &&
		(h.LastVerdictStatus == "" || h.LastVerdictStatus == "active_candidate" || h.LastVerdictStatus == "active")
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

type PredictionDecisionRefreshResult struct {
	FeatureDate         string `json:"featureDate"`
	MonitoredHypotheses int    `json:"monitoredHypotheses"`
	RefreshedSessions   int    `json:"refreshedSessions"`
	RefreshedDecisions  int    `json:"refreshedDecisions"`
	FailedSessions      int    `json:"failedSessions"`
}

func (s *PredictionService) RefreshMonitoredDecisions(featureDate string) (*PredictionDecisionRefreshResult, error) {
	result := &PredictionDecisionRefreshResult{}
	if err := ensurePredictionTables(); err != nil {
		return result, err
	}
	s.reviewHypothesisLifecycle()
	featureDate = strings.TrimSpace(featureDate)
	if featureDate == "" {
		latest, err := latestAdjustedFeatureDate()
		if err != nil {
			return result, err
		}
		featureDate = latest
	}
	result.FeatureDate = featureDate
	if featureDate == "" {
		return result, fmt.Errorf("no adjusted feature is available for monitored decisions")
	}
	s.downgradeRegimeMismatches(featureDate)

	var hypotheses []models.PredictionHypothesis
	if err := db.Dao.Where("status IN ?", monitoredHypothesisStatuses).Find(&hypotheses).Error; err != nil {
		return result, err
	}
	result.MonitoredHypotheses = len(hypotheses)
	sessionIDs := make(map[uint]struct{})
	for _, hypothesis := range hypotheses {
		sessionIDs[hypothesis.SessionID] = struct{}{}
	}
	for sessionID := range sessionIDs {
		decisions, err := s.refreshMonitoredSessionDecisions(sessionID, featureDate)
		if err != nil {
			result.FailedSessions++
			logger.SugaredLogger.Errorf("refresh monitored decisions for session %d error: %v", sessionID, err)
			continue
		}
		result.RefreshedSessions++
		result.RefreshedDecisions += len(decisions)
	}
	if result.FailedSessions > 0 {
		return result, fmt.Errorf("%d monitored sessions failed to refresh decisions", result.FailedSessions)
	}
	return result, nil
}

func (s *PredictionService) reviewHypothesisLifecycle() {
	if db.Dao == nil {
		return
	}
	now := time.Now()
	_ = db.Dao.Model(&models.PredictionHypothesis{}).
		Where("status = ? AND review_due_at IS NOT NULL AND review_due_at <= ?", "active", now).
		Updates(map[string]any{
			"status":              "watch",
			"last_verdict_status": "watchlist",
		}).Error
	var paperTrades []models.PredictionHypothesis
	if err := db.Dao.Where("status = ?", "paper_trade").Find(&paperTrades).Error; err == nil {
		for _, hypothesis := range paperTrades {
			ready, reason := paperTradeReady(hypothesis, now)
			_ = db.Dao.Model(&models.PredictionHypothesis{}).Where("id = ?", hypothesis.ID).Updates(map[string]any{
				"paper_ready": ready, "paper_ready_reason": reason,
			}).Error
			// 达标只标记为“可启用”，不自动越过用户确认进入正式监控。
			if !ready && hypothesis.PaperTradeDays >= DefaultBacktestConfig().PaperTradeDays &&
				hypothesis.PaperTradeCount >= MinPaperTradeValidations && hypothesis.PaperNav <= 1 {
				_ = db.Dao.Model(&models.PredictionHypothesis{}).Where("id = ?", hypothesis.ID).Updates(map[string]any{
					"status": "watch", "last_verdict_status": "watchlist",
				}).Error
			}
		}
	}
	var active []models.PredictionHypothesis
	if err := db.Dao.Where("status = ? AND valid_count >= ?", "active", MinPaperTradeValidations).Find(&active).Error; err != nil {
		return
	}
	for _, h := range active {
		actualMean, actualStd, count := validatedReturnStats(h.ID)
		if h.AvgReturn == 0 || count < MinPaperTradeValidations {
			continue
		}
		threshold := 2 * actualStd / math.Sqrt(float64(count))
		if threshold <= 0 {
			threshold = math.Abs(h.AvgReturn) * 0.25
		}
		if actualMean < 0 || actualMean < h.AvgReturn-threshold {
			_ = db.Dao.Model(&models.PredictionHypothesis{}).Where("id = ?", h.ID).Updates(map[string]any{
				"status":              "watch",
				"last_verdict_status": "watchlist",
			}).Error
		}
	}
}

func validatedReturnStats(hypothesisID uint) (float64, float64, int) {
	var signals []models.PredictionSignal
	if db.Dao == nil || db.Dao.Where("hypothesis_id = ? AND status = ?", hypothesisID, "validated").Find(&signals).Error != nil {
		return 0, 0, 0
	}
	if len(signals) == 0 {
		return 0, 0, 0
	}
	mean := 0.0
	for _, signal := range signals {
		mean += signal.ActualReturn
	}
	mean /= float64(len(signals))
	variance := 0.0
	for _, signal := range signals {
		variance += (signal.ActualReturn - mean) * (signal.ActualReturn - mean)
	}
	if len(signals) > 1 {
		variance /= float64(len(signals) - 1)
	}
	return mean, math.Sqrt(variance), len(signals)
}

func (s *PredictionService) downgradeRegimeMismatches(featureDate string) {
	end, err := time.Parse("2006-01-02", featureDate)
	if err != nil || db.Dao == nil {
		return
	}
	startDate := end.AddDate(0, 0, -120).Format("2006-01-02")
	benchmarkReturn, available := s.validator.calculateBenchmarkReturn(DefaultBacktestConfig().BenchmarkCode, startDate, featureDate, CurrentFeatureVersion)
	if !available {
		return
	}
	regime := "震荡市"
	if benchmarkReturn >= 0.05 {
		regime = "上涨趋势"
	} else if benchmarkReturn <= -0.05 {
		regime = "下跌趋势"
	}
	var active []models.PredictionHypothesis
	if db.Dao.Where("status = ?", "active").Find(&active).Error != nil {
		return
	}
	for _, hypothesis := range active {
		mismatch := (hypothesis.Scene == "趋势持有" && regime != "上涨趋势") ||
			(hypothesis.Scene == "短线爆发" && regime == "下跌趋势")
		if !mismatch {
			continue
		}
		_ = db.Dao.Model(&models.PredictionHypothesis{}).Where("id = ?", hypothesis.ID).Updates(map[string]any{
			"status": "watch", "last_verdict_status": "watchlist",
		}).Error
	}
}

func latestAdjustedFeatureDate() (string, error) {
	var latest string
	err := db.Dao.Model(&models.StockFeature{}).
		Where("date <= ? AND feature_version = ? AND adjusted = ?", shanghaiNow().Format("2006-01-02"), CurrentFeatureVersion, true).
		Select("COALESCE(MAX(date), '')").Scan(&latest).Error
	return latest, err
}

func (s *PredictionService) refreshMonitoredSessionDecisions(sessionID uint, featureDate string) ([]models.PredictionDecision, error) {
	var session models.PredictionSession
	if err := db.Dao.First(&session, sessionID).Error; err != nil {
		return nil, err
	}
	if session.Status != "done" {
		return nil, fmt.Errorf("prediction session %d is not complete", sessionID)
	}
	var hypotheses []models.PredictionHypothesis
	if err := db.Dao.Where("session_id = ? AND status IN ?", sessionID, monitoredHypothesisStatuses).Find(&hypotheses).Error; err != nil {
		return nil, err
	}
	hypotheses = selectMonitoredHypotheses(hypotheses)
	if len(hypotheses) == 0 {
		return nil, nil
	}

	var universe []string
	if strings.TrimSpace(session.UniverseJSON) != "" {
		if err := json.Unmarshal([]byte(session.UniverseJSON), &universe); err != nil {
			return nil, fmt.Errorf("decode frozen stock universe: %w", err)
		}
	}
	if len(universe) == 0 {
		universe = NewStockPoolService().GetStockPool(session.StockScope)
	}
	universe = uniqueStrings(universe)
	if len(universe) == 0 {
		return nil, fmt.Errorf("monitored session %d has an empty stock universe", sessionID)
	}

	decisionSession := session
	decisionSession.EndDate = featureDate
	return s.generateSessionDecisions(&decisionSession, hypotheses, universe, featureDate)
}

func selectMonitoredHypotheses(hypotheses []models.PredictionHypothesis) []models.PredictionHypothesis {
	hasActive := false
	for _, hypothesis := range hypotheses {
		if hypothesis.Status == "active" {
			hasActive = true
			break
		}
	}
	selected := make([]models.PredictionHypothesis, 0, len(hypotheses))
	for _, hypothesis := range hypotheses {
		if (hasActive && hypothesis.Status == "active") ||
			(!hasActive && (hypothesis.Status == "active_candidate" || hypothesis.Status == "paper_trade" || hypothesis.Status == "watch")) {
			selected = append(selected, hypothesis)
		}
	}
	return selected
}

type PredictionSignalScanResult struct {
	ActiveHypotheses    int    `json:"activeHypotheses"`
	MonitoredHypotheses int    `json:"monitoredHypotheses"`
	FeatureDate         string `json:"featureDate"`
	FeatureRows         int    `json:"featureRows"`
	RefreshedSessions   int    `json:"refreshedSessions"`
	RefreshedDecisions  int    `json:"refreshedDecisions"`
	Generated           int    `json:"generated"`
	Failed              int    `json:"failed"`
}

// ScanSignals 扫描今日触发信号
func (s *PredictionService) ScanSignals() (*PredictionSignalScanResult, error) {
	result := &PredictionSignalScanResult{}
	if err := ensurePredictionTables(); err != nil {
		return result, err
	}

	var hypotheses []models.PredictionHypothesis
	if err := db.Dao.Where("status IN ?", []string{"active", "paper_trade"}).Find(&hypotheses).Error; err != nil {
		return result, err
	}
	for _, hypothesis := range hypotheses {
		if hypothesis.Status == "active" {
			result.ActiveHypotheses++
		}
	}

	today := shanghaiNow().Format("2006-01-02")
	if err := db.Dao.Model(&models.StockFeature{}).
		Where("date <= ? AND feature_version = ? AND adjusted = ?", today, CurrentFeatureVersion, true).
		Select("MAX(date)").Scan(&result.FeatureDate).Error; err != nil {
		return result, err
	}
	if result.FeatureDate == "" || result.FeatureDate != today {
		logger.SugaredLogger.Warnf("scan prediction signals skipped: current feature not ready, latest=%s today=%s", result.FeatureDate, today)
		return result, nil
	}
	refreshResult, refreshErr := s.RefreshMonitoredDecisions(result.FeatureDate)
	if refreshResult != nil {
		result.MonitoredHypotheses = refreshResult.MonitoredHypotheses
		result.RefreshedSessions = refreshResult.RefreshedSessions
		result.RefreshedDecisions = refreshResult.RefreshedDecisions
		result.Failed += refreshResult.FailedSessions
	}

	for _, h := range hypotheses {
		rule, err := JSONToRule(h.RuleJSON)
		if err != nil {
			result.Failed++
			continue
		}

		var session models.PredictionSession
		stockScope := "全部A股"
		if err := db.Dao.Select("stock_scope", "universe_json").First(&session, h.SessionID).Error; err == nil && session.StockScope != "" {
			stockScope = session.StockScope
		}

		universe := NewStockPoolService().GetStockPool(stockScope)
		if strings.TrimSpace(session.UniverseJSON) != "" {
			_ = json.Unmarshal([]byte(session.UniverseJSON), &universe)
		}
		if len(universe) == 0 {
			logger.SugaredLogger.Warnf("scan prediction signals skipped: hypothesis=%d stock scope %s is empty", h.ID, stockScope)
			continue
		}
		if isAllStockScope(stockScope) {
			universe = nil
		}

		features := s.validator.repo.GetByDate(result.FeatureDate, universe)
		result.FeatureRows += len(features)
		currentMap := stockFeatureMap(features)
		previousMap := s.previousFeatureMap(currentMap)
		for _, f := range features {
			previous := featurePointer(previousMap, strings.ToLower(f.StockCode))
			if previous == nil {
				previous = featurePointer(previousMap, f.StockCode)
			}
			if !NewStrategyEngine().MatchConditionsWithPrevious(rule.EntryConditions, f, previous) {
				continue
			}

			var count int64
			if err := db.Dao.Model(&models.PredictionSignal{}).
				Where("hypothesis_id = ? AND stock_code = ? AND signal_date = ?", h.ID, f.StockCode, result.FeatureDate).
				Count(&count).Error; err != nil {
				result.Failed++
				continue
			}
			if count > 0 {
				continue
			}

			dataAsOf := f.DataAsOf
			if dataAsOf.IsZero() {
				dataAsOf = time.Now()
			}
			decisionID := fmt.Sprintf("pred-%d-%s-%s", h.ID, f.StockCode, result.FeatureDate)
			reasonsJSON, _ := json.Marshal([]string{fmt.Sprintf("%s 预测假设入场条件满足", h.Status)})
			risksJSON, _ := json.Marshal([]string{"仅供观察，不代表盈利概率"})
			signal := models.PredictionSignal{
				HypothesisID: h.ID, StockCode: f.StockCode, StockName: f.StockCode,
				SignalDate: result.FeatureDate, TargetReturn: h.TargetReturn, Status: "pending_entry",
				DecisionID: decisionID, DataAsOf: dataAsOf, ReasonsJSON: string(reasonsJSON), RisksJSON: string(risksJSON),
			}
			metricsJSON, _ := json.Marshal(map[string]any{
				"winRate": h.WinRate, "avgReturn": h.AvgReturn, "maxDrawdown": h.MaxDrawdown,
				"tradeCount": h.TradeCount, "outSampleAvg": h.OutSampleAvgReturn,
			})
			matchedFactsJSON, _ := json.Marshal(map[string]any{
				"close": f.Close, "ma5": f.MA5, "ma20": f.MA20, "volumeRatio": f.VolumeRatio,
			})
			decisionLog := models.TradeDecisionLog{
				DecisionID: decisionID, StockCode: f.StockCode, StockName: f.StockCode,
				Action: "buy_watch", Score: s.calculateSignalScore(h, f), ScoreType: "heuristic", CurrentPrice: f.Close,
				ReasonsJSON: string(reasonsJSON), RisksJSON: string(risksJSON), MatchedFactsJSON: string(matchedFactsJSON),
				MetricsJSON: string(metricsJSON), StrategyID: h.ID, StrategyVersion: h.StrategyVersion,
				FeatureVersion: f.FeatureVersion, SignalDate: result.FeatureDate, DataAsOf: dataAsOf,
				ValidUntil: time.Now().AddDate(0, 0, 5), CreatedAt: time.Now(),
			}
			if err := db.Dao.Transaction(func(tx *gorm.DB) error {
				if err := tx.Create(&signal).Error; err != nil {
					return err
				}
				return tx.Create(&decisionLog).Error
			}); err != nil {
				logger.SugaredLogger.Errorf("create prediction signal %s error: %v", decisionID, err)
				result.Failed++
				continue
			}
			result.Generated++
		}
	}
	if refreshErr != nil {
		return result, refreshErr
	}
	if result.Failed > 0 {
		return result, fmt.Errorf("信号扫描存在 %d 条处理失败", result.Failed)
	}
	return result, nil
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

type PredictionSignalValidationResult struct {
	Accounts      int `json:"accounts"`
	ProcessedDays int `json:"processedDays"`
	Pending       int `json:"pending"`
	Entered       int `json:"entered"`
	Missed        int `json:"missed"`
	Validated     int `json:"validated"`
	Waiting       int `json:"waiting"`
	Failed        int `json:"failed"`
}

// DailyValidateSignals 推进持久化前向模拟账户；保留方法名以兼容既有定时任务。
func (s *PredictionService) DailyValidateSignals() (*PredictionSignalValidationResult, error) {
	return s.ProcessPaperTrading()
}

func (s *PredictionService) nthTradingDateAfter(date string, horizon int) string {
	if horizon <= 0 {
		horizon = 1
	}
	var dates []string
	db.Dao.Model(&models.StockFeature{}).
		Where("stock_code IN ? AND date > ? AND feature_version = ?", stockCodeVariants(DefaultBacktestConfig().BenchmarkCode), date, CurrentFeatureVersion).
		Distinct("date").Order("date asc").Limit(horizon).Pluck("date", &dates)
	if len(dates) < horizon {
		return ""
	}
	return dates[horizon-1]
}
