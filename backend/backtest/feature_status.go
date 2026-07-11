package backtest

import (
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"time"
)

func (s *FeatureSyncService) GetFeatureSyncJob(jobID uint) (*models.FeatureSyncJob, error) {
	if err := ensurePredictionTables(); err != nil {
		return nil, err
	}
	var job models.FeatureSyncJob
	if err := db.Dao.First(&job, jobID).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *FeatureSyncService) GetFeatureCoverageForScopeByDates(stockScope, startDate, endDate string) models.FeatureCoverage {
	if stockScope == "" {
		stockScope = "全部A股"
	}
	if endDate == "" {
		endDate = time.Now().Format("2006-01-02")
	}
	if startDate == "" {
		startDate = time.Now().AddDate(0, 0, -365).Format("2006-01-02")
	}

	coverage := models.FeatureCoverage{
		StockScope: stockScope,
		StartDate:  startDate,
		EndDate:    endDate,
	}
	if err := ensurePredictionTables(); err != nil {
		coverage.Message = err.Error()
		return coverage
	}

	universe := NewStockPoolService().GetStockPool(stockScope)
	coverage.ExpectedStockCount = len(universe)
	if coverage.ExpectedStockCount == 0 {
		coverage.Message = "股票池为空"
		coverage.ExcludedReasons = append(coverage.ExcludedReasons, "股票池为空")
		return coverage
	}
	tradingDays := observedTradingDays(startDate, endDate, CurrentFeatureVersion, DefaultBacktestConfig().BenchmarkCode)
	coverage.ExpectedTradeDays = len(tradingDays)
	if coverage.ExpectedTradeDays == 0 {
		coverage.Message = "回测区间内暂无可用交易日特征数据，请先同步特征"
		return coverage
	}

	query := db.Dao.Model(&models.StockFeature{}).Where("date >= ? AND date <= ? AND feature_version = ?", startDate, endDate, CurrentFeatureVersion)
	if !isAllStockScope(stockScope) {
		query = query.Where("stock_code IN ?", universe)
	}

	var coveredStocks int64
	query.Distinct("stock_code").Count(&coveredStocks)
	coverage.CoveredStockCount = int(coveredStocks)

	query = db.Dao.Model(&models.StockFeature{}).Where("date >= ? AND date <= ? AND feature_version = ?", startDate, endDate, CurrentFeatureVersion)
	if !isAllStockScope(stockScope) {
		query = query.Where("stock_code IN ?", universe)
	}
	var coveredDays int64
	query.Distinct("date").Count(&coveredDays)
	coverage.CoveredTradeDays = int(coveredDays)

	coreQuery := db.Dao.Model(&models.StockFeature{}).
		Where("date >= ? AND date <= ? AND feature_version = ?", startDate, endDate, CurrentFeatureVersion).
		Where("open > 0 AND close > 0 AND high > 0 AND low > 0 AND volume > 0 AND ma5 > 0 AND ma20 > 0 AND adjusted = ?", true)
	if !isAllStockScope(stockScope) {
		coreQuery = coreQuery.Where("stock_code IN ?", universe)
	}
	var coreRows int64
	coreQuery.Count(&coreRows)

	expectedRows := coverage.ExpectedTradeDays * coverage.ExpectedStockCount
	if expectedRows > 0 {
		coverage.CoreFieldCoverage = float64(coreRows) / float64(expectedRows)
	}
	if coverage.ExpectedStockCount > 0 {
		coverage.StockCoverage = float64(coverage.CoveredStockCount) / float64(coverage.ExpectedStockCount)
	}
	if coverage.ExpectedTradeDays > 0 {
		coverage.TradeDayCoverage = float64(coverage.CoveredTradeDays) / float64(coverage.ExpectedTradeDays)
	}
	coverage.CoreIndicatorCoverage = coverage.CoreFieldCoverage
	coverage.Ready = coverage.StockCoverage >= 0.95 &&
		coverage.TradeDayCoverage >= 0.95 &&
		coverage.CoreFieldCoverage >= 0.95
	if coverage.Ready {
		coverage.Message = "核心特征覆盖率达标"
	} else {
		coverage.Message = fmt.Sprintf("核心特征覆盖率不足：股票 %.1f%%，交易日 %.1f%%，字段 %.1f%%",
			coverage.StockCoverage*100,
			coverage.TradeDayCoverage*100,
			coverage.CoreFieldCoverage*100,
		)
	}
	return coverage
}

func observedTradingDays(startDate, endDate, featureVersion, benchmarkCode string) []string {
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

func (s *FeatureSyncService) GetFeatureFreshness(stockScope string) map[string]any {
	if err := ensurePredictionTables(); err != nil {
		return map[string]any{"ready": false, "message": err.Error()}
	}
	if stockScope == "" {
		stockScope = "全部A股"
	}
	query := db.Dao.Model(&models.StockFeature{}).Where("feature_version = ?", CurrentFeatureVersion)
	universe := NewStockPoolService().GetStockPool(stockScope)
	if len(universe) == 0 {
		return map[string]any{"ready": false, "message": "股票池为空", "stockScope": stockScope}
	}
	if !isAllStockScope(stockScope) {
		query = query.Where("stock_code IN ?", universe)
	}

	var latest models.StockFeature
	if err := query.Order("date desc").First(&latest).Error; err != nil {
		return map[string]any{"ready": false, "message": "暂无特征数据", "stockScope": stockScope}
	}
	dataAsOf := latest.DataAsOf
	if dataAsOf.IsZero() {
		dataAsOf = time.Now()
	}
	return map[string]any{
		"ready":          true,
		"stockScope":     stockScope,
		"latestDate":     latest.Date,
		"dataAsOf":       dataAsOf,
		"source":         latest.Source,
		"featureVersion": latest.FeatureVersion,
	}
}
