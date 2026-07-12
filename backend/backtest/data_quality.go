package backtest

import (
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"math"
	"strings"
	"time"
)

// DataQualityReport 数据质量报告
type DataQualityReport struct {
	GeneratedAt          time.Time               `json:"generatedAt"`
	FeatureVersion       string                  `json:"featureVersion"`
	TotalStocks          int                     `json:"totalStocks"`
	TotalRecords         int64                   `json:"totalRecords"`
	DateRange            DateRange               `json:"dateRange"`
	CoverageByStock      []StockCoverage         `json:"coverageByStock"`
	MissingDates         []string                `json:"missingDates"`
	AnomalyCount         int                     `json:"anomalyCount"`
	Anomalies            []DataAnomaly           `json:"anomalies"`
	SuspensionStats      SuspensionStats         `json:"suspensionStats"`
	LimitUpDownStats     LimitUpDownStats        `json:"limitUpDownStats"`
	PriceConsistency     PriceConsistencyCheck   `json:"priceConsistency"`
	OverallScore         float64                 `json:"overallScore"` // 0-100
	Warnings             []string                `json:"warnings"`
}

type DateRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Days  int    `json:"days"`
}

type StockCoverage struct {
	StockCode       string  `json:"stockCode"`
	ExpectedRecords int     `json:"expectedRecords"`
	ActualRecords   int64   `json:"actualRecords"`
	Coverage        float64 `json:"coverage"`
	FirstDate       string  `json:"firstDate"`
	LastDate        string  `json:"lastDate"`
}

type DataAnomaly struct {
	StockCode string `json:"stockCode"`
	Date      string `json:"date"`
	Field     string `json:"field"`
	Value     string `json:"value"`
	Issue     string `json:"issue"`
	Severity  string `json:"severity"` // warning / error
}

type SuspensionStats struct {
	TotalSuspensionDays int64   `json:"totalSuspensionDays"`
	StocksWithSuspension int    `json:"stocksWithSuspension"`
	MaxConsecutiveSuspend int   `json:"maxConsecutiveSuspend"`
	SuspensionRatio     float64 `json:"suspensionRatio"`
}

type LimitUpDownStats struct {
	LimitUpDays   int64 `json:"limitUpDays"`
	LimitDownDays int64 `json:"limitDownDays"`
	ConsecutiveLimitUp int `json:"consecutiveLimitUp"`
}

type PriceConsistencyCheck struct {
	Passed           bool    `json:"passed"`
	MaxPriceJump     float64 `json:"maxPriceJump"`     // 最大单日价格跳变
	MaxPriceJumpStock string `json:"maxPriceJumpStock"`
	MaxPriceJumpDate  string `json:"maxPriceJumpDate"`
	NegativePriceCount int64 `json:"negativePriceCount"`
	ZeroPriceCount    int64  `json:"zeroPriceCount"`
}

// DataQualityChecker 数据质量检查器
type DataQualityChecker struct {
	startDate string
	endDate   string
	universe  []string
}

// NewDataQualityChecker 创建数据质量检查器
func NewDataQualityChecker(startDate, endDate string, universe []string) *DataQualityChecker {
	return &DataQualityChecker{
		startDate: startDate,
		endDate:   endDate,
		universe:  universe,
	}
}

// Run 执行完整的数据质量检查
func (c *DataQualityChecker) Run() *DataQualityReport {
	report := &DataQualityReport{
		GeneratedAt:    time.Now(),
		FeatureVersion: CurrentFeatureVersion,
		Warnings:       make([]string, 0),
		Anomalies:      make([]DataAnomaly, 0),
	}

	c.checkCoverage(report)
	c.checkPriceAnomalies(report)
	c.checkSuspension(report)
	c.checkPriceConsistency(report)
	c.calculateOverallScore(report)

	logger.SugaredLogger.Infof("data quality report: score=%.1f warnings=%d anomalies=%d",
		report.OverallScore, len(report.Warnings), len(report.Anomalies))
	return report
}

func (c *DataQualityChecker) checkCoverage(report *DataQualityReport) {
	if db.Dao == nil {
		report.Warnings = append(report.Warnings, "数据库未初始化，跳过覆盖率检查")
		return
	}

	query := db.Dao.Model(&models.StockFeature{}).
		Where("feature_version = ? AND adjusted = ?", CurrentFeatureVersion, true)
	if len(c.universe) > 0 {
		query = query.Where("stock_code IN ?", c.universe)
	}

	// 总记录数
	query.Count(&report.TotalRecords)

	// 日期范围
	var minDate, maxDate string
	query.Select("MIN(date), MAX(date)").Row().Scan(&minDate, &maxDate)
	report.DateRange = DateRange{Start: minDate, End: maxDate}

	// 计算预期交易日数
	start, _ := time.Parse("2006-01-02", minDate)
	end, _ := time.Parse("2006-01-02", maxDate)
	report.DateRange.Days = int(end.Sub(start).Hours() / 24)

	// 每个股票的覆盖率
	type stockCount struct {
		StockCode string
		Count     int64
		MinDate   string
		MaxDate   string
	}
	var stockCounts []stockCount
	stockQuery := db.Dao.Model(&models.StockFeature{}).
		Select("stock_code, COUNT(*) as count, MIN(date) as min_date, MAX(date) as max_date").
		Where("feature_version = ? AND adjusted = ?", CurrentFeatureVersion, true)
	if len(c.universe) > 0 {
		stockQuery = stockQuery.Where("stock_code IN ?", c.universe)
	}
	stockQuery.Group("stock_code").Order("stock_code asc").Scan(&stockCounts)

	report.TotalStocks = len(stockCounts)
	expectedDays := float64(report.DateRange.Days * 5 / 7) // 约 5/7 是交易日
	if expectedDays < 1 {
		expectedDays = 1
	}

	lowCoverageCount := 0
	for _, sc := range stockCounts {
		coverage := float64(sc.Count) / expectedDays
		if coverage > 1 {
			coverage = 1
		}
		report.CoverageByStock = append(report.CoverageByStock, StockCoverage{
			StockCode:       sc.StockCode,
			ExpectedRecords: int(expectedDays),
			ActualRecords:   sc.Count,
			Coverage:        coverage,
			FirstDate:       sc.MinDate,
			LastDate:        sc.MaxDate,
		})
		if coverage < 0.80 {
			lowCoverageCount++
		}
	}

	if lowCoverageCount > 0 {
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("%d/%d 只股票覆盖率低于 80%%", lowCoverageCount, report.TotalStocks))
	}

	// 检查缺失日期
	c.checkMissingDates(report)
}

// checkMissingDates 检查完全缺失的交易日
func (c *DataQualityChecker) checkMissingDates(report *DataQualityReport) {
	// 检查是否有完全缺失的交易日（所有股票都没有数据）
	if db.Dao == nil {
		return
	}

	// 获取所有唯一日期
	var allDates []string
	db.Dao.Model(&models.StockFeature{}).
		Select("DISTINCT date").
		Where("feature_version = ? AND adjusted = ?", CurrentFeatureVersion, true).
		Order("date asc").Pluck("date", &allDates)

	if len(allDates) < 2 {
		return
	}

	start, _ := time.Parse("2006-01-02", allDates[0])
	end, _ := time.Parse("2006-01-02", allDates[len(allDates)-1])

	dateSet := make(map[string]bool, len(allDates))
	for _, d := range allDates {
		dateSet[d] = true
	}

	// 检查工作日是否有缺失
	current := start
	for !current.After(end) {
		dateStr := current.Format("2006-01-02")
		weekday := current.Weekday()
		if weekday != time.Saturday && weekday != time.Sunday {
			if !dateSet[dateStr] {
				report.MissingDates = append(report.MissingDates, dateStr)
			}
		}
		current = current.AddDate(0, 0, 1)
	}

	if len(report.MissingDates) > 0 {
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("发现 %d 个交易日数据完全缺失", len(report.MissingDates)))
	}
}

func (c *DataQualityChecker) checkPriceAnomalies(report *DataQualityReport) {
	if db.Dao == nil {
		return
	}

	var features []models.StockFeature
	query := db.Dao.Where("feature_version = ? AND adjusted = ?", CurrentFeatureVersion, true)
	if len(c.universe) > 0 {
		query = query.Where("stock_code IN ?", c.universe)
	}
	query.Order("stock_code asc, date asc").Find(&features)

	byStock := make(map[string][]models.StockFeature)
	for _, f := range features {
		byStock[f.StockCode] = append(byStock[f.StockCode], f)
	}

	for stockCode, stockFeatures := range byStock {
		for i := 1; i < len(stockFeatures); i++ {
			prev := stockFeatures[i-1]
			curr := stockFeatures[i]

			// 检查价格跳变 > 50%（可能是复权问题或数据错误）
			if prev.Close > 0 && curr.Close > 0 {
				jump := math.Abs(curr.Close/prev.Close - 1)
				if jump > 0.50 {
					report.Anomalies = append(report.Anomalies, DataAnomaly{
						StockCode: stockCode,
						Date:      curr.Date,
						Field:     "close",
						Value:     fmt.Sprintf("%.2f -> %.2f (%.1f%%)", prev.Close, curr.Close, jump*100),
						Issue:     "价格跳变超过 50%，可能是复权因子问题",
						Severity:  "warning",
					})
					report.AnomalyCount++
				}
			}

			// 检查负价格
			if curr.Close < 0 {
				report.Anomalies = append(report.Anomalies, DataAnomaly{
					StockCode: stockCode,
					Date:      curr.Date,
					Field:     "close",
					Value:     fmt.Sprintf("%.2f", curr.Close),
					Issue:     "负价格",
					Severity:  "error",
				})
				report.AnomalyCount++
				report.PriceConsistency.NegativePriceCount++
			}

			// 检查零价格（可能是停牌但没有标记）
			if curr.Close == 0 && curr.Volume == 0 {
				report.PriceConsistency.ZeroPriceCount++
			}

			// 检查 OHLC 一致性
			if curr.High < curr.Low || curr.High < curr.Open || curr.High < curr.Close ||
				curr.Low > curr.Open || curr.Low > curr.Close {
				report.Anomalies = append(report.Anomalies, DataAnomaly{
					StockCode: stockCode,
					Date:      curr.Date,
					Field:     "ohlc",
					Value:     fmt.Sprintf("O:%.2f H:%.2f L:%.2f C:%.2f", curr.Open, curr.High, curr.Low, curr.Close),
					Issue:     "OHLC 数据不一致",
					Severity:  "error",
				})
				report.AnomalyCount++
			}
		}
	}
}

func (c *DataQualityChecker) checkSuspension(report *DataQualityReport) {
	if db.Dao == nil {
		return
	}

	var features []models.StockFeature
	query := db.Dao.Where("feature_version = ? AND adjusted = ?", CurrentFeatureVersion, true)
	if len(c.universe) > 0 {
		query = query.Where("stock_code IN ?", c.universe)
	}
	query.Order("stock_code asc, date asc").Find(&features)

	byStock := make(map[string][]models.StockFeature)
	for _, f := range features {
		byStock[f.StockCode] = append(byStock[f.StockCode], f)
	}

	maxConsecutive := 0
	stocksWithSuspension := 0
	for _, stockFeatures := range byStock {
		consecutive := 0
		hasSuspension := false
		for i := 0; i < len(stockFeatures); i++ {
			f := stockFeatures[i]
			if f.Close <= 0 || f.Volume <= 0 || (f.Open <= 0 && f.High <= 0 && f.Low <= 0) {
				consecutive++
				hasSuspension = true
				report.SuspensionStats.TotalSuspensionDays++
				if consecutive > maxConsecutive {
					maxConsecutive = consecutive
				}
			} else {
				consecutive = 0
			}
		}
		if hasSuspension {
			stocksWithSuspension++
		}
	}
	report.SuspensionStats.StocksWithSuspension = stocksWithSuspension
	report.SuspensionStats.MaxConsecutiveSuspend = maxConsecutive
	if report.TotalRecords > 0 {
		report.SuspensionStats.SuspensionRatio = float64(report.SuspensionStats.TotalSuspensionDays) / float64(report.TotalRecords)
	}
}

func (c *DataQualityChecker) checkPriceConsistency(report *DataQualityReport) {
	report.PriceConsistency.Passed = true

	if db.Dao == nil {
		report.PriceConsistency.Passed = false
		report.Warnings = append(report.Warnings, "数据库未初始化，无法检查价格一致性")
		return
	}

	// 检查前复权数据的一致性：前复权后的历史价格不应比当前价格高太多
	var features []models.StockFeature
	query := db.Dao.Where("feature_version = ? AND adjusted = ?", CurrentFeatureVersion, true)
	if len(c.universe) > 0 {
		query = query.Where("stock_code IN ?", c.universe)
	}
	query.Order("stock_code asc, date asc").Find(&features)

	byStock := make(map[string][]models.StockFeature)
	for _, f := range features {
		byStock[f.StockCode] = append(byStock[f.StockCode], f)
	}

	for stockCode, stockFeatures := range byStock {
		if len(stockFeatures) < 2 {
			continue
		}
		latest := stockFeatures[len(stockFeatures)-1]
		if latest.Close <= 0 {
			continue
		}
		// 前复权后，历史最高价不应超过最新价的 10 倍
		for _, f := range stockFeatures {
			if f.Close > 0 && f.Close > latest.Close*10 {
				report.Warnings = append(report.Warnings,
					fmt.Sprintf("%s %s 前复权价格 %.2f 是当前价 %.2f 的 %.1f 倍，可能复权因子异常",
						stockCode, f.Date, f.Close, latest.Close, f.Close/latest.Close))
				report.PriceConsistency.Passed = false
			}
		}
	}

	if report.PriceConsistency.NegativePriceCount > 0 {
		report.PriceConsistency.Passed = false
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("发现 %d 条负价格记录", report.PriceConsistency.NegativePriceCount))
	}
}

func (c *DataQualityChecker) calculateOverallScore(report *DataQualityReport) {
	score := 100.0

	// 覆盖率扣分
	lowCoverageCount := 0
	for _, sc := range report.CoverageByStock {
		if sc.Coverage < 0.80 {
			lowCoverageCount++
		}
	}
	if report.TotalStocks > 0 {
		coveragePenalty := float64(lowCoverageCount) / float64(report.TotalStocks) * 30
		score -= coveragePenalty
	}

	// 异常扣分
	score -= float64(report.AnomalyCount) * 2
	if score < 0 {
		score = 0
	}

	// 停牌扣分
	if report.SuspensionStats.SuspensionRatio > 0.10 {
		score -= 10
	}

	// 一致性扣分
	if !report.PriceConsistency.Passed {
		score -= 20
	}

	report.OverallScore = math.Max(0, math.Min(100, score))
	if report.OverallScore < 60 {
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("数据质量评分 %.1f 低于 60，建议检查数据源和同步流程", score))
	}
}

// GetLatestDataQualityReport 获取最新的数据质量报告
func GetLatestDataQualityReport(startDate, endDate string, universe []string) *DataQualityReport {
	checker := NewDataQualityChecker(startDate, endDate, universe)
	return checker.Run()
}

// QuickDataQualityCheck 快速数据质量检查（仅检查关键指标）
func QuickDataQualityCheck() []string {
	warnings := make([]string, 0)
	if db.Dao == nil {
		return append(warnings, "数据库未初始化")
	}

	// 检查最新的特征数据是否在 3 个交易日内
	var latestDate string
	db.Dao.Model(&models.StockFeature{}).
		Select("MAX(date)").
		Where("feature_version = ? AND adjusted = ?", CurrentFeatureVersion, true).
		Row().Scan(&latestDate)

	if latestDate == "" {
		warnings = append(warnings, "没有特征数据，请先同步特征数据")
		return warnings
	}

	latest, err := time.Parse("2006-01-02", latestDate)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("无法解析最新日期: %s", latestDate))
		return warnings
	}

	now := time.Now()
	daysSince := int(now.Sub(latest).Hours() / 24)
	if daysSince > 3 {
		warnings = append(warnings,
			fmt.Sprintf("最新特征数据日期为 %s（%d 天前），建议立即同步", latestDate, daysSince))
	}

	// 检查特征数据总量
	var count int64
	db.Dao.Model(&models.StockFeature{}).
		Where("feature_version = ? AND adjusted = ?", CurrentFeatureVersion, true).
		Count(&count)
	if count < 1000 {
		warnings = append(warnings, fmt.Sprintf("特征数据仅 %d 条，数据量严重不足", count))
	}

	return warnings
}

// FeatureDataSummary 特征数据摘要（用于前端展示）
type FeatureDataSummary struct {
	LatestDate    string  `json:"latestDate"`
	TotalRecords  int64   `json:"totalRecords"`
	TotalStocks   int     `json:"totalStocks"`
	AvgCoverage   float64 `json:"avgCoverage"`
	QualityScore  float64 `json:"qualityScore"`
	Warnings      []string `json:"warnings"`
	SyncNeeded    bool    `json:"syncNeeded"`
}

// GetFeatureDataSummary 获取特征数据摘要
func GetFeatureDataSummary() *FeatureDataSummary {
	summary := &FeatureDataSummary{
		Warnings: make([]string, 0),
	}
	if db.Dao == nil {
		summary.Warnings = append(summary.Warnings, "数据库未初始化")
		return summary
	}

	var latestDate string
	var count int64
	db.Dao.Model(&models.StockFeature{}).
		Select("MAX(date)").
		Where("feature_version = ? AND adjusted = ?", CurrentFeatureVersion, true).
		Row().Scan(&latestDate)
	db.Dao.Model(&models.StockFeature{}).
		Where("feature_version = ? AND adjusted = ?", CurrentFeatureVersion, true).
		Count(&count)

	summary.LatestDate = latestDate
	summary.TotalRecords = count

	// 估算股票数量
	db.Dao.Model(&models.StockFeature{}).
		Select("COUNT(DISTINCT stock_code)").
		Where("feature_version = ? AND adjusted = ?", CurrentFeatureVersion, true).
		Row().Scan(&summary.TotalStocks)

	// 快速质量检查
	quickWarnings := QuickDataQualityCheck()
	summary.Warnings = append(summary.Warnings, quickWarnings...)

	// 判断是否需要同步
	if latestDate == "" {
		summary.SyncNeeded = true
	} else {
		latest, _ := time.Parse("2006-01-02", latestDate)
		daysSince := int(time.Now().Sub(latest).Hours() / 24)
		summary.SyncNeeded = daysSince > 1
	}

	// 估算覆盖率
	if summary.TotalStocks > 0 && strings.TrimSpace(latestDate) != "" {
		// 简约估算：有最新数据的股票比例
		var latestDateCount int64
		db.Dao.Model(&models.StockFeature{}).
			Where("date = ? AND feature_version = ? AND adjusted = ?", latestDate, CurrentFeatureVersion, true).
			Count(&latestDateCount)
		summary.AvgCoverage = float64(latestDateCount) / float64(summary.TotalStocks)
	}

	summary.QualityScore = 100 - float64(len(summary.Warnings))*10
	if summary.QualityScore < 0 {
		summary.QualityScore = 0
	}

	return summary
}