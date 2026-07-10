package backtest

import (
	"go-stock/backend/db"
	"go-stock/backend/models"
	"strings"
)

func loadStockTradeCounts(hypotheses []models.PredictionHypothesis, stockCode string) map[uint]int {
	counts := make(map[uint]int, len(hypotheses))
	variants := stockCodeVariants(stockCode)
	if len(variants) == 0 {
		return counts
	}
	for _, hypothesis := range hypotheses {
		var count int64
		db.Dao.Model(&models.PredictionTrade{}).
			Where("hypothesis_id = ? AND stock_code IN ?", hypothesis.ID, variants).
			Count(&count)
		counts[hypothesis.ID] = int(count)
	}
	return counts
}

func loadDecisionDataStatus(session *models.PredictionSession, feature models.StockFeature) DecisionDataStatus {
	status := DecisionDataStatus{}
	if session == nil || strings.TrimSpace(feature.StockCode) == "" {
		status.Warnings = []string{"缺少预测数据范围或股票代码"}
		return status
	}

	featureQuery := db.Dao.Model(&models.StockFeature{}).
		Where("stock_code IN ? AND date >= ? AND date <= ?", stockCodeVariants(feature.StockCode), session.StartDate, session.EndDate)
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
