package backtest

import (
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"strings"
)

type stockTradeStat struct {
	Count     int
	Wins      int
	AvgReturn float64
}

func loadStockTradeStats(hypotheses []models.PredictionHypothesis, stockCode string) map[uint]stockTradeStat {
	stats := make(map[uint]stockTradeStat, len(hypotheses))
	variants := stockCodeVariants(stockCode)
	if len(variants) == 0 {
		return stats
	}
	for _, hypothesis := range hypotheses {
		var trades []models.PredictionTrade
		db.Dao.
			Where("hypothesis_id = ? AND stock_code IN ?", hypothesis.ID, variants).
			Find(&trades)
		stat := stockTradeStat{Count: len(trades)}
		for _, trade := range trades {
			stat.AvgReturn += trade.ReturnRate
			if trade.ReturnRate > 0 {
				stat.Wins++
			}
		}
		if stat.Count > 0 {
			stat.AvgReturn /= float64(stat.Count)
		}
		stats[hypothesis.ID] = stat
	}
	return stats
}

func loadUniqueTradeSamples(hypotheses []models.PredictionHypothesis, stockCode string) (poolCount, stockCount int) {
	ids := make([]uint, 0, len(hypotheses))
	for _, hypothesis := range hypotheses {
		ids = append(ids, hypothesis.ID)
	}
	if len(ids) == 0 {
		return 0, 0
	}
	var trades []models.PredictionTrade
	db.Dao.Where("hypothesis_id IN ?", ids).Find(&trades)
	poolKeys := make(map[string]struct{}, len(trades))
	stockKeys := make(map[string]struct{})
	variants := make(map[string]bool)
	for _, variant := range stockCodeVariants(stockCode) {
		variants[strings.ToLower(variant)] = true
	}
	for _, trade := range trades {
		key := fmt.Sprintf("%s|%s|%s", strings.ToLower(trade.StockCode), trade.BuyDate, trade.SellDate)
		poolKeys[key] = struct{}{}
		if variants[strings.ToLower(trade.StockCode)] {
			stockKeys[key] = struct{}{}
		}
	}
	return len(poolKeys), len(stockKeys)
}

func loadDecisionDataStatus(session *models.PredictionSession, feature models.StockFeature) DecisionDataStatus {
	status := DecisionDataStatus{}
	if session == nil || strings.TrimSpace(feature.StockCode) == "" {
		status.Warnings = []string{"缺少预测数据范围或股票代码"}
		return status
	}

	featureQuery := db.Dao.Model(&models.StockFeature{}).
		Where("stock_code IN ? AND date >= ? AND date <= ? AND feature_version = ?", stockCodeVariants(feature.StockCode), session.StartDate, session.EndDate, CurrentFeatureVersion)
	featureQuery.Count(&status.FeatureRows)
	var first models.StockFeature
	if featureQuery.Order("date asc").First(&first).Error == nil {
		status.FeatureStartDate = first.Date
	}
	var last models.StockFeature
	if featureQuery.Order("date desc").First(&last).Error == nil {
		status.FeatureEndDate = last.Date
	}
	if status.FeatureRows > 0 {
		var unadjusted int64
		featureQuery.Where("adjusted = ?", false).Count(&unadjusted)
		status.FeatureAdjusted = unadjusted == 0
	}

	flowQuery := db.Dao.Model(&models.StockMoneyFlowDaily{}).
		Where("stock_code IN ? AND trade_date >= ? AND trade_date <= ?", stockCodeVariants(feature.StockCode), session.StartDate, session.EndDate)
	flowQuery.Count(&status.MoneyFlowRows)
	var firstFlow models.StockMoneyFlowDaily
	if flowQuery.Order("trade_date asc").First(&firstFlow).Error == nil {
		status.MoneyFlowStartDate = firstFlow.TradeDate
	}
	var lastFlow models.StockMoneyFlowDaily
	if flowQuery.Order("trade_date desc").First(&lastFlow).Error == nil {
		status.MoneyFlowEndDate = lastFlow.TradeDate
	}
	status.MoneyFlowReady = status.MoneyFlowRows >= 20

	if status.FeatureRows == 0 {
		status.Warnings = append(status.Warnings, "当前股票没有可用特征数据")
	} else if !status.FeatureAdjusted {
		status.Warnings = append(status.Warnings, "特征数据未复权，涉及分红/拆分的指标需要谨慎")
	}
	if !status.MoneyFlowReady {
		status.Warnings = append(status.Warnings, "资金流历史数据不足，仅使用有限资金流信号")
	}
	return status
}
