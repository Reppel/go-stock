package backtest

import (
	"encoding/json"
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
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
		&models.PredictionSignal{},
		&models.PredictionHypothesisDaily{},
		&models.StockFeature{},
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
	for _, h := range hypotheses {
		if h.TimeHorizon <= 0 {
			h.TimeHorizon = 5
		}
		if h.TargetReturn <= 0 {
			h.TargetReturn = 0.05
		}

		result, err := s.validator.Validate(
			h.Rule,
			queryUniverse,
			startDate,
			endDate,
			h.TimeHorizon,
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
		ph := models.PredictionHypothesis{
			SessionID:    session.ID,
			Name:         h.Name,
			Description:  h.Description,
			Scene:        h.Scene,
			RuleJSON:     string(ruleJSON),
			Params:       h.Params,
			TimeHorizon:  h.TimeHorizon,
			TargetReturn: h.TargetReturn,
			WinRate:      result.WinRate,
			AvgReturn:    result.AvgReturn,
			MaxDrawdown:  result.MaxDrawdown,
			TradeCount:   result.TradeCount,
			Status:       "draft",
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

		resultHypotheses = append(resultHypotheses, ph)
	}

	if len(resultHypotheses) == 0 {
		session.Status = "failed"
		db.Dao.Save(session)
		return nil, nil, fmt.Errorf("回测未产生有效假设，请检查特征数据或调整回测区间")
	}

	session.Status = "done"
	db.Dao.Save(session)

	return session, resultHypotheses, nil
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
	db.Dao.Where("status = ?", "active").Order("created_at desc").Find(&hypotheses)
	return hypotheses
}

// SaveHypothesis 保存假设为监控
func (s *PredictionService) SaveHypothesis(hypothesisID uint) error {
	if err := ensurePredictionTables(); err != nil {
		return fmt.Errorf("初始化预测工厂表失败: %w", err)
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
				signal := models.PredictionSignal{
					HypothesisID: h.ID,
					StockCode:    f.StockCode,
					StockName:    f.StockCode,
					SignalDate:   today,
					EntryPrice:   f.Close,
					TargetDate:   targetDate.AddDate(0, 0, h.TimeHorizon).Format("2006-01-02"),
					TargetReturn: h.TargetReturn,
					Status:       "pending",
				}
				db.Dao.Create(&signal)
			}
		}
	}
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
