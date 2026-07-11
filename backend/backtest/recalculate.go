package backtest

import (
	"encoding/json"
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"strings"

	"gorm.io/gorm"
)

type recalculatedHypothesis struct {
	hypothesis models.PredictionHypothesis
	result     *ValidationResult
	config     BacktestConfig
}

// RecalculateSession reruns stored executable rules without asking the AI to
// generate a new strategy, then replaces the stale backtest artifacts.
func (s *PredictionService) RecalculateSession(sessionID uint) (*models.PredictionSession, []models.PredictionHypothesis, error) {
	if err := ensurePredictionTables(); err != nil {
		return nil, nil, err
	}
	var session models.PredictionSession
	if err := db.Dao.First(&session, sessionID).Error; err != nil {
		return nil, nil, fmt.Errorf("预测会话不存在: %w", err)
	}
	var hypotheses []models.PredictionHypothesis
	if err := db.Dao.Where("session_id = ?", sessionID).Order("id asc").Find(&hypotheses).Error; err != nil {
		return nil, nil, err
	}
	if len(hypotheses) == 0 {
		return nil, nil, fmt.Errorf("预测会话没有可重新回测的策略")
	}

	universe := NewStockPoolService().GetStockPool(session.StockScope)
	if strings.TrimSpace(session.UniverseJSON) != "" {
		_ = json.Unmarshal([]byte(session.UniverseJSON), &universe)
	}
	if len(universe) == 0 {
		return nil, nil, fmt.Errorf("股票池为空，无法重新回测")
	}
	queryUniverse := universe
	if isAllStockScope(session.StockScope) {
		queryUniverse = nil
	}

	pending := make([]recalculatedHypothesis, 0, len(hypotheses))
	for _, hypothesis := range hypotheses {
		var rule Rule
		if err := json.Unmarshal([]byte(hypothesis.RuleJSON), &rule); err != nil {
			return nil, nil, fmt.Errorf("策略 %s 规则无效: %w", hypothesis.Name, err)
		}
		if validationErrors := ValidateHypothesisRule(rule); len(validationErrors) > 0 {
			return nil, nil, fmt.Errorf("策略 %s 未通过规则校验: %v", hypothesis.Name, validationErrors)
		}
		config := storedBacktestConfig(hypothesis.BacktestConfigJSON)
		result, err := s.validator.ValidateWithConfig(rule, queryUniverse, session.StartDate, session.EndDate, hypothesis.TimeHorizon, config)
		if err != nil {
			return nil, nil, fmt.Errorf("重新回测策略 %s 失败: %w", hypothesis.Name, err)
		}
		if result.TradeCount == 0 {
			return nil, nil, fmt.Errorf("策略 %s 重新回测后没有有效交易", hypothesis.Name)
		}

		payload, _ := json.Marshal(backtestPayload(config, result))
		hypothesis.WinRate = result.WinRate
		hypothesis.AvgReturn = result.AvgReturn
		hypothesis.MaxDrawdown = result.MaxDrawdown
		hypothesis.TradeCount = result.TradeCount
		hypothesis.TotalReturn = result.TotalReturn
		hypothesis.MedianReturn = result.MedianReturn
		hypothesis.ProfitLossRatio = result.ProfitLossRatio
		hypothesis.OutSampleAvgReturn = result.OutSampleAvgReturn
		hypothesis.OutSampleMaxDrawdown = result.OutSampleMaxDrawdown
		hypothesis.OutSampleTradeCount = result.OutSampleTradeCount
		hypothesis.BenchmarkAvailable = result.BenchmarkAvailable
		hypothesis.DataCoverage = result.DataCoverage
		hypothesis.NoLookaheadPassed = result.NoLookaheadPassed
		hypothesis.BacktestConfigJSON = string(payload)
		hypothesis.StrategyVersion = CurrentStrategyVersion
		hypothesis.FeatureVersion = config.FeatureVersion
		if hypothesis.Status == "active" && !hypothesisMonitorReady(hypothesis) {
			hypothesis.Status = "watch"
		}
		pending = append(pending, recalculatedHypothesis{hypothesis: hypothesis, result: result, config: config})
	}

	if err := db.Dao.Transaction(func(tx *gorm.DB) error {
		for _, item := range pending {
			if err := tx.Save(&item.hypothesis).Error; err != nil {
				return err
			}
			if err := tx.Where("hypothesis_id = ?", item.hypothesis.ID).Delete(&models.PredictionHypothesisDaily{}).Error; err != nil {
				return err
			}
			if err := tx.Where("hypothesis_id = ?", item.hypothesis.ID).Delete(&models.PredictionTrade{}).Error; err != nil {
				return err
			}
			for _, daily := range item.result.DailyNAV {
				row := models.PredictionHypothesisDaily{
					HypothesisID: item.hypothesis.ID,
					Date:         daily.Date, Nav: daily.Nav, Drawdown: daily.Drawdown, TradeCount: daily.TradeCount,
				}
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
			}
			for _, trade := range item.result.Trades {
				row := models.PredictionTrade{
					HypothesisID: item.hypothesis.ID, StockCode: trade.StockCode, StockName: stockNameOrCode(trade.StockCode, trade.StockName),
					SignalDate: trade.SignalDate, BuyDate: trade.BuyDate, SellDate: trade.SellDate,
					BuyPrice: trade.BuyPrice, SellPrice: trade.SellPrice, Quantity: trade.Quantity,
					GrossBuyAmount: trade.GrossBuyAmount, GrossSellAmount: trade.GrossSellAmount,
					Fee: trade.Fee, Slippage: trade.Slippage,
					ReturnRate: trade.ReturnRate, MaxReturn: trade.MaxReturn, MaxDrawdown: trade.MaxDrawdown,
					HoldDays: trade.HoldDays, EntryReasonJSON: trade.EntryReasonJSON, ExitReason: trade.ExitReason,
					FeatureVersion: trade.FeatureVersion, DataAsOf: trade.DataAsOf,
				}
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return nil, nil, fmt.Errorf("保存重新回测结果失败: %w", err)
	}

	updated := make([]models.PredictionHypothesis, 0, len(pending))
	for _, item := range pending {
		updated = append(updated, item.hypothesis)
	}
	s.generateSessionDecisions(&session, updated, universe, session.EndDate)
	return &session, updated, nil
}

func storedBacktestConfig(raw string) BacktestConfig {
	config := DefaultBacktestConfig()
	var payload struct {
		Config BacktestConfig `json:"config"`
	}
	if json.Unmarshal([]byte(raw), &payload) == nil && payload.Config.EntryMode != "" {
		config = payload.Config
	}
	config.FeatureVersion = CurrentFeatureVersion
	if config.BenchmarkCode == "" {
		config.BenchmarkCode = DefaultBacktestConfig().BenchmarkCode
	}
	return normalizeBacktestConfig(config)
}
