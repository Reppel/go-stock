package backtest

import (
	"encoding/json"
	"errors"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// FeatureSyncService 特征数据同步服务
type FeatureSyncService struct {
	klineApi              *data.EastMoneyKLineApi
	syncSupplementalFlows bool
}

var activeFeatureSyncJobs sync.Map

const featureWarmupBars = 60

type featureSyncOutcome struct {
	stockCode string
	err       error
}

// NewFeatureSyncService 创建特征同步服务
func NewFeatureSyncService() *FeatureSyncService {
	return &FeatureSyncService{
		klineApi:              data.NewEastMoneyKLineApi(data.GetSettingConfig()),
		syncSupplementalFlows: true,
	}
}

// TechnicalOnly keeps the scheduled feature stage focused on adjusted bars and
// technical factors. The dedicated money-flow stage runs afterwards.
func (s *FeatureSyncService) TechnicalOnly() *FeatureSyncService {
	s.syncSupplementalFlows = false
	return s
}

// SyncStockFeatures 同步指定股票的特征数据
func (s *FeatureSyncService) SyncStockFeatures(stockCode string, days int) error {
	if err := ensurePredictionTables(); err != nil {
		return err
	}

	klines := s.klineApi.GetKLineData(stockCode, "101", "1", days)
	source := "eastmoney_qfq"
	if klines == nil || len(*klines) == 0 {
		result := data.FetchKLineWithFallback(stockCode, "", "101", days, "", "qfq")
		if result == nil || result.Data == nil || len(*result.Data) == 0 {
			return fmt.Errorf("%s 前复权日K数据为空，全部行情源均不可用", stockCode)
		}
		klines = result.Data
		source = strings.TrimSpace(result.Source)
		if source == "" {
			source = "fallback_qfq"
		}
	}

	klineData := completedFeatureKLines(normalizeFeatureKLines(*klines), time.Now())
	if len(klineData) <= featureWarmupBars {
		return fmt.Errorf("%s 日K数据不足：实际%d根，至少需要%d根（含%d根指标预热）",
			stockCode, len(klineData), featureWarmupBars+1, featureWarmupBars)
	}

	expectedRows := len(klineData) - featureWarmupBars
	writtenRows := 0
	var writeErrors []string
	for i := featureWarmupBars; i < len(klineData); i++ {
		// 取当前及之前的数据
		subset := klineData[:i+1]

		// 计算特征
		features := CalculateFeaturesFromKLines(subset)

		// 写入数据库
		feature := s.toStockFeature(stockCode, klineData[i], features, source)
		if err := s.upsertFeature(feature); err != nil {
			logger.SugaredLogger.Errorf("upsert feature for %s on %s error: %v", stockCode, feature.Date, err)
			writeErrors = append(writeErrors, fmt.Sprintf("%s: %v", feature.Date, err))
			continue
		}
		writtenRows++
	}

	if writtenRows != expectedRows || len(writeErrors) > 0 {
		detail := strings.Join(writeErrors, "; ")
		if len(detail) > 500 {
			detail = detail[:500] + "..."
		}
		return fmt.Errorf("%s 特征写入不完整：应写%d条，成功%d条，错误：%s", stockCode, expectedRows, writtenRows, detail)
	}

	startDate := klineData[featureWarmupBars].Day
	endDate := klineData[len(klineData)-1].Day
	var persistedRows int64
	if err := db.Dao.Model(&models.StockFeature{}).
		Where("stock_code = ? AND date >= ? AND date <= ? AND feature_version = ?", stockCode, startDate, endDate, CurrentFeatureVersion).
		Count(&persistedRows).Error; err != nil {
		return fmt.Errorf("%s 校验特征写入结果失败: %w", stockCode, err)
	}
	if persistedRows < int64(expectedRows) {
		return fmt.Errorf("%s 特征落库校验失败：区间%s至%s应有%d条，实际%d条",
			stockCode, startDate, endDate, expectedRows, persistedRows)
	}

	if s.syncSupplementalFlows {
		if rows, macRows, err := s.SyncStockMoneyFlow(stockCode, days); err != nil {
			logger.SugaredLogger.Warnf("sync money flow for %s error: %v", stockCode, err)
		} else if rows > 0 || macRows > 0 {
			logger.SugaredLogger.Infof("synced money flow for %s: eastmoney=%d mac=%d", stockCode, rows, macRows)
		}
	}
	logger.SugaredLogger.Infof("synced features for %s: source=%s klines=%d features=%d", stockCode, source, len(klineData), writtenRows)

	return nil
}

func normalizeFeatureKLines(klines []data.KLineData) []data.KLineData {
	byDate := make(map[string]data.KLineData, len(klines))
	for _, kline := range klines {
		date := strings.TrimSpace(kline.Day)
		if date == "" {
			continue
		}
		kline.Day = date
		byDate[date] = kline
	}
	normalized := make([]data.KLineData, 0, len(byDate))
	for _, kline := range byDate {
		normalized = append(normalized, kline)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Day < normalized[j].Day
	})
	return normalized
}

func completedFeatureKLines(klines []data.KLineData, now time.Time) []data.KLineData {
	if len(klines) == 0 {
		return klines
	}
	latest := klines[len(klines)-1]
	availableAt := featureDataAsOf(latest.Day)
	if !availableAt.IsZero() && now.Before(availableAt) {
		return klines[:len(klines)-1]
	}
	return klines
}

func featureDataAsOf(day string) time.Time {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	parsed, err := time.ParseInLocation("2006-01-02 15:04", strings.TrimSpace(day)+" 15:10", location)
	if err != nil {
		return time.Time{}
	}
	return parsed
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
	stockCodes = uniqueStrings(append(stockCodes, DefaultBacktestConfig().BenchmarkCode))

	now := time.Now()
	endDate := now.Format("2006-01-02")
	startDate := now.AddDate(0, 0, -(days * 3 / 2)).Format("2006-01-02")
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
		if jobID > 0 {
			now := time.Now()
			db.Dao.Model(&models.FeatureSyncJob{}).Where("id = ?", jobID).Updates(map[string]any{
				"status":        "failed",
				"failed":        len(stockCodes),
				"finished_at":   &now,
				"error_message": err.Error(),
			})
		}
		return
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	outcomes := make(chan featureSyncOutcome, len(stockCodes))

	for _, code := range stockCodes {
		wg.Add(1)
		go func(stockCode string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := s.SyncStockFeatures(stockCode, days); err != nil {
				logger.SugaredLogger.Errorf("sync features for %s error: %v", stockCode, err)
				outcomes <- featureSyncOutcome{stockCode: stockCode, err: err}
				return
			}
			outcomes <- featureSyncOutcome{stockCode: stockCode}
		}(code)
	}
	go func() {
		wg.Wait()
		close(outcomes)
	}()

	finishedCount := 0
	failedCount := 0
	failedCodes := make([]string, 0)
	failureDetails := make([]string, 0)
	for outcome := range outcomes {
		if outcome.err != nil {
			failedCount++
			failedCodes = append(failedCodes, outcome.stockCode)
			failureDetails = append(failureDetails, fmt.Sprintf("%s: %v", outcome.stockCode, outcome.err))
		} else {
			finishedCount++
		}
		if jobID > 0 {
			db.Dao.Model(&models.FeatureSyncJob{}).Where("id = ?", jobID).Updates(map[string]any{
				"finished": finishedCount,
				"failed":   failedCount,
			})
		}
	}

	if s.syncSupplementalFlows {
		sectorRows, conceptRows := SyncSectorMoneyFlows()
		logger.SugaredLogger.Infof("synced prediction sector money flows: industry=%d concept=%d", sectorRows, conceptRows)
	}
	if result, err := data.RebaseFollowedStockPriceAlerts(stockCodes); err != nil {
		logger.SugaredLogger.Warnf("rebase followed stock price alerts error: %v", err)
	} else if result.Rebased > 0 || result.Initialized > 0 {
		logger.SugaredLogger.Infof("rebased followed stock price alerts: rebased=%d initialized=%d", result.Rebased, result.Initialized)
	}

	now := time.Now()
	coverage := s.GetFeatureCoverageForScopeByDates("", "", "")
	if jobID > 0 {
		var job models.FeatureSyncJob
		if err := db.Dao.First(&job, jobID).Error; err == nil {
			coverage = s.GetFeatureCoverageForScopeByDates(job.StockScope, job.StartDate, job.EndDate)
		}
	}
	coverageJSON, _ := json.Marshal(coverage)
	if jobID > 0 {
		status := "done"
		if failedCount > 0 && finishedCount == 0 {
			status = "failed"
		} else if failedCount > 0 {
			status = "partial"
		}
		excludedJSON, _ := json.Marshal(failedCodes)
		db.Dao.Model(&models.FeatureSyncJob{}).Where("id = ?", jobID).Updates(map[string]any{
			"status":        status,
			"finished":      finishedCount,
			"failed":        failedCount,
			"finished_at":   &now,
			"data_as_of":    now,
			"coverage_json": string(coverageJSON),
			"excluded_json": string(excludedJSON),
			"error_message": strings.Join(failureDetails, "\n"),
		})
	}
}

// toStockFeature 转换特征到数据库模型
func (s *FeatureSyncService) toStockFeature(stockCode string, kline data.KLineData, features *KLineFeatures, source string) models.StockFeature {
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
		DataAsOf:       featureDataAsOf(kline.Day),
		Source:         source,
		FeatureVersion: CurrentFeatureVersion,
		Adjusted:       true,
	}
}

// upsertFeature 写入或更新特征
func (s *FeatureSyncService) upsertFeature(feature models.StockFeature) error {
	// 先查是否存在
	var existing models.StockFeature
	err := db.Dao.Where("stock_code = ? AND date = ? AND feature_version = ?", feature.StockCode, feature.Date, feature.FeatureVersion).First(&existing).Error
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
