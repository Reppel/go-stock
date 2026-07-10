package backtest

import (
	"encoding/json"
	"errors"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"strconv"
	"sync"
	"time"

	"gorm.io/gorm"
)

// FeatureSyncService 特征数据同步服务
type FeatureSyncService struct {
	klineApi *data.EastMoneyKLineApi
}

var activeFeatureSyncJobs sync.Map

// NewFeatureSyncService 创建特征同步服务
func NewFeatureSyncService() *FeatureSyncService {
	return &FeatureSyncService{
		klineApi: data.NewEastMoneyKLineApi(data.GetSettingConfig()),
	}
}

// SyncStockFeatures 同步指定股票的特征数据
func (s *FeatureSyncService) SyncStockFeatures(stockCode string, days int) error {
	if err := ensurePredictionTables(); err != nil {
		return err
	}

	// 获取日 K 线数据
	klines := s.klineApi.GetDayKLine(stockCode, days)
	if klines == nil || len(*klines) == 0 {
		return nil
	}

	// 转换成 KLineData 切片
	klineData := *klines

	// 从后往前遍历，生成每个交易日的特征
	for i := 60; i < len(klineData); i++ {
		// 取当前及之前的数据
		subset := klineData[:i+1]

		// 计算特征
		features := CalculateFeaturesFromKLines(subset)

		// 写入数据库
		feature := s.toStockFeature(stockCode, klineData[i], features)
		if err := s.upsertFeature(feature); err != nil {
			logger.SugaredLogger.Errorf("upsert feature for %s on %s error: %v", stockCode, feature.Date, err)
			continue
		}
	}

	if rows, macRows, err := s.SyncStockMoneyFlow(stockCode, days); err != nil {
		logger.SugaredLogger.Warnf("sync money flow for %s error: %v", stockCode, err)
	} else if rows > 0 || macRows > 0 {
		logger.SugaredLogger.Infof("synced money flow for %s: eastmoney=%d mac=%d", stockCode, rows, macRows)
	}

	return nil
}

// SyncAllStockFeatures 同步所有股票特征
func (s *FeatureSyncService) SyncAllStockFeatures(stockCodes []string, days int) {
	if err := ensurePredictionTables(); err != nil {
		logger.SugaredLogger.Errorf("ensure prediction tables error: %v", err)
		return
	}

	if len(stockCodes) == 0 {
		stockCodes = NewStockPoolService().GetStockPool("全部A股")
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 5) // 限制并发数

	for _, code := range stockCodes {
		wg.Add(1)
		go func(stockCode string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := s.SyncStockFeatures(stockCode, days); err != nil {
				logger.SugaredLogger.Errorf("sync features for %s error: %v", stockCode, err)
			}
		}(code)
	}

	wg.Wait()
}

func (s *FeatureSyncService) StartFeatureSync(stockScope string, days int) (*models.FeatureSyncJob, error) {
	job, stockCodes, shouldStart, err := s.prepareFeatureSyncJob(stockScope, days)
	if err != nil {
		return nil, err
	}
	if shouldStart {
		go s.runFeatureSyncJob(stockCodes, days, job.ID)
	}
	return job, nil
}

func (s *FeatureSyncService) RunFeatureSync(stockScope string, days int) (*models.FeatureSyncJob, error) {
	job, stockCodes, shouldStart, err := s.prepareFeatureSyncJob(stockScope, days)
	if err != nil {
		return nil, err
	}
	if shouldStart {
		s.runFeatureSyncJob(stockCodes, days, job.ID)
	}
	var latest models.FeatureSyncJob
	if err := db.Dao.First(&latest, job.ID).Error; err == nil {
		return &latest, nil
	}
	return job, nil
}

func (s *FeatureSyncService) prepareFeatureSyncJob(stockScope string, days int) (*models.FeatureSyncJob, []string, bool, error) {
	if err := ensurePredictionTables(); err != nil {
		return nil, nil, false, err
	}
	if stockScope == "" {
		stockScope = "全部A股"
	}
	if days <= 0 {
		days = 365
	}

	poolService := NewStockPoolService()
	stockCodes := poolService.GetStockPool(stockScope)
	if len(stockCodes) == 0 {
		return nil, nil, false, fmt.Errorf("股票池为空：%s", stockScope)
	}

	now := time.Now()
	endDate := now.Format("2006-01-02")
	startDate := now.AddDate(0, 0, -days).Format("2006-01-02")
	jobKey := fmt.Sprintf("prediction_sync_features:%s:%d:%s", stockScope, days, endDate)

	var existing models.FeatureSyncJob
	err := db.Dao.Where("job_key = ?", jobKey).First(&existing).Error
	if err == nil {
		if existing.Status == "running" {
			if _, ok := activeFeatureSyncJobs.Load(existing.ID); ok {
				return &existing, stockCodes, false, nil
			}
			logger.SugaredLogger.Warnf("feature sync job %d was running in db but not active in current process, restarting", existing.ID)
		}
		updates := map[string]any{
			"total":         len(stockCodes),
			"finished":      0,
			"failed":        0,
			"status":        "running",
			"started_at":    now,
			"finished_at":   nil,
			"error_message": "",
			"retry_count":   gorm.Expr("retry_count + 1"),
		}
		if err := db.Dao.Model(&existing).Updates(updates).Error; err != nil {
			return nil, nil, false, err
		}
		db.Dao.First(&existing, existing.ID)
		return &existing, stockCodes, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, false, err
	}

	job := &models.FeatureSyncJob{
		JobKey:     jobKey,
		StockScope: stockScope,
		StartDate:  startDate,
		EndDate:    endDate,
		Total:      len(stockCodes),
		Status:     "running",
		DataAsOf:   now,
		StartedAt:  now,
	}
	if err := db.Dao.Create(job).Error; err != nil {
		return nil, nil, false, err
	}
	return job, stockCodes, true, nil
}

func (s *FeatureSyncService) runFeatureSyncJob(stockCodes []string, days int, jobID uint) {
	if jobID > 0 {
		if _, loaded := activeFeatureSyncJobs.LoadOrStore(jobID, struct{}{}); loaded {
			logger.SugaredLogger.Warnf("feature sync job %d is already active, skip duplicate runner", jobID)
			return
		}
		defer activeFeatureSyncJobs.Delete(jobID)
	}
	s.SyncAllStockFeaturesWithJob(stockCodes, days, jobID)
}

func (s *FeatureSyncService) SyncAllStockFeaturesWithJob(stockCodes []string, days int, jobID uint) {
	if err := ensurePredictionTables(); err != nil {
		logger.SugaredLogger.Errorf("ensure prediction tables error: %v", err)
		return
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for _, code := range stockCodes {
		wg.Add(1)
		go func(stockCode string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := s.SyncStockFeatures(stockCode, days); err != nil {
				logger.SugaredLogger.Errorf("sync features for %s error: %v", stockCode, err)
				db.Dao.Model(&models.FeatureSyncJob{}).Where("id = ?", jobID).Updates(map[string]any{
					"failed": gorm.Expr("failed + 1"),
				})
				return
			}
			db.Dao.Model(&models.FeatureSyncJob{}).Where("id = ?", jobID).Updates(map[string]any{
				"finished": gorm.Expr("finished + 1"),
			})
		}(code)
	}
	wg.Wait()

	sectorRows, conceptRows := SyncSectorMoneyFlows()
	logger.SugaredLogger.Infof("synced prediction sector money flows: industry=%d concept=%d", sectorRows, conceptRows)

	now := time.Now()
	coverage := s.GetFeatureCoverageForScopeByDates("", "", "")
	if jobID > 0 {
		var job models.FeatureSyncJob
		if err := db.Dao.First(&job, jobID).Error; err == nil {
			coverage = s.GetFeatureCoverageForScopeByDates(job.StockScope, job.StartDate, job.EndDate)
		}
	}
	coverageJSON, _ := json.Marshal(coverage)
	db.Dao.Model(&models.FeatureSyncJob{}).Where("id = ?", jobID).Updates(map[string]any{
		"status":        "done",
		"finished_at":   &now,
		"data_as_of":    now,
		"coverage_json": string(coverageJSON),
	})
}

// toStockFeature 转换特征到数据库模型
func (s *FeatureSyncService) toStockFeature(stockCode string, kline data.KLineData, features *KLineFeatures) models.StockFeature {
	close, _ := strconv.ParseFloat(kline.Close, 64)
	open, _ := strconv.ParseFloat(kline.Open, 64)
	high, _ := strconv.ParseFloat(kline.High, 64)
	low, _ := strconv.ParseFloat(kline.Low, 64)
	volume, _ := strconv.ParseFloat(kline.Volume, 64)
	turnover, _ := strconv.ParseFloat(kline.Amount, 64)

	return models.StockFeature{
		StockCode:      stockCode,
		Date:           kline.Day,
		Close:          close,
		Open:           open,
		High:           high,
		Low:            low,
		Volume:         volume,
		Turnover:       turnover,
		MA5:            features.MA5,
		MA10:           features.MA10,
		MA20:           features.MA20,
		MA60:           features.MA60,
		MACD:           features.MACD,
		RSI6:           features.RSI6,
		RSI12:          features.RSI12,
		KDJ_K:          features.KDJ_K,
		BOLLUpper:      features.BOLLUpper,
		BOLLMid:        features.BOLLMid,
		BOLLLower:      features.BOLLLower,
		VolumeRatio:    features.VolumeRatio,
		ATR:            features.ATR,
		FundFlow5:      features.FundFlow5,
		FundFlow20:     features.FundFlow20,
		ChangeRate5:    features.ChangeRate5,
		ChangeRate20:   features.ChangeRate20,
		DataAsOf:       time.Now(),
		Source:         "eastmoney",
		FeatureVersion: "daily_v1",
		Adjusted:       false,
	}
}

// upsertFeature 写入或更新特征
func (s *FeatureSyncService) upsertFeature(feature models.StockFeature) error {
	// 先查是否存在
	var existing models.StockFeature
	err := db.Dao.Where("stock_code = ? AND date = ?", feature.StockCode, feature.Date).First(&existing).Error
	if err == nil {
		// 更新
		feature.ID = existing.ID
		return db.Dao.Save(&feature).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	// 新建
	return db.Dao.Create(&feature).Error
}
