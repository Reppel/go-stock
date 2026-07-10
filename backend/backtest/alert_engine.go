package backtest

import (
	"encoding/json"
	"fmt"
	"go-stock/backend/models"
	"strings"
	"time"
)

type AlertEngine struct{}

func NewAlertEngine() *AlertEngine {
	return &AlertEngine{}
}

func (e *AlertEngine) EvaluateDecisions(decisions []models.PredictionDecision) []PredictionAlert {
	alerts := make([]PredictionAlert, 0)
	for _, decision := range decisions {
		alerts = append(alerts, e.EvaluateDecision(decision)...)
	}
	return alerts
}

func (e *AlertEngine) EvaluateDecision(decision models.PredictionDecision) []PredictionAlert {
	alerts := make([]PredictionAlert, 0, 4)
	name := decision.StockName
	if strings.TrimSpace(name) == "" {
		name = decision.StockCode
	}
	now := time.Now()
	base := func(reason, level, title, message string, threshold float64) PredictionAlert {
		return PredictionAlert{
			Key:            fmt.Sprintf("prediction:%d:%s:%s", decision.SessionID, strings.ToLower(decision.StockCode), reason),
			SessionID:      decision.SessionID,
			DecisionID:     decision.ID,
			StockCode:      decision.StockCode,
			StockName:      name,
			Level:          normalizeAlertLevel(level),
			Title:          title,
			Message:        message,
			Reason:         reason,
			Price:          decision.CurrentPrice,
			ThresholdPrice: threshold,
			CreatedAt:      now,
		}
	}

	if decision.HoldingVolume > 0 && decision.StopLossPrice > 0 && decision.CurrentPrice > 0 && decision.CurrentPrice <= decision.StopLossPrice {
		alerts = append(alerts, base(
			"stop_loss",
			"high",
			"AI预测工厂止损提醒",
			fmt.Sprintf("%s 当前价 %.3f 已触发止损价 %.3f，建议优先控制亏损。", name, decision.CurrentPrice, decision.StopLossPrice),
			decision.StopLossPrice,
		))
	}
	if decision.HoldingVolume > 0 && decision.DefensePrice > 0 && decision.CurrentPrice > 0 && decision.CurrentPrice <= decision.DefensePrice {
		alerts = append(alerts, base(
			"defense_price",
			"medium",
			"AI预测工厂防守位提醒",
			fmt.Sprintf("%s 当前价 %.3f 跌破防守位 %.3f，建议检查是否需要减仓。", name, decision.CurrentPrice, decision.DefensePrice),
			decision.DefensePrice,
		))
	}
	if decision.HoldingVolume > 0 && decision.TakeProfitPrice > 0 && decision.CurrentPrice >= decision.TakeProfitPrice && decision.Action == "REDUCE" {
		alerts = append(alerts, base(
			"take_profit",
			"medium",
			"AI预测工厂止盈/减仓提醒",
			fmt.Sprintf("%s 当前价 %.3f 进入止盈/减仓区间 %.3f，可按建议数量执行。", name, decision.CurrentPrice, decision.TakeProfitPrice),
			decision.TakeProfitPrice,
		))
	}
	if decision.HoldingVolume > 0 && (decision.Action == "SELL" || decision.Action == "REDUCE") {
		alerts = append(alerts, base(
			strings.ToLower(decision.Action),
			decision.RiskLevel,
			"AI预测工厂操作提醒",
			fmt.Sprintf("%s 当前建议：%s，%s。", name, decision.ActionText, decision.PositionAdvice),
			decision.CurrentPrice,
		))
	}
	if decision.HoldingVolume > 0 && hasStrongCapitalOutflow(decision.CapitalFlowJSON) {
		alerts = append(alerts, base(
			"capital_outflow",
			"medium",
			"AI预测工厂资金流风险提醒",
			fmt.Sprintf("%s 出现主力资金代理强流出信号，建议降低追涨和加仓优先级。", name),
			decision.CurrentPrice,
		))
	}
	return alerts
}

func normalizeAlertLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "high", "error":
		return "high"
	case "medium", "warning":
		return "medium"
	case "low", "success":
		return "low"
	default:
		return "medium"
	}
}

func hasStrongCapitalOutflow(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	var flow struct {
		Level string `json:"level"`
	}
	if err := json.Unmarshal([]byte(raw), &flow); err != nil {
		return false
	}
	return flow.Level == "strong_outflow"
}
