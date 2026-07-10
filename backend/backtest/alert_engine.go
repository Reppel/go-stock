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
		alerts = append(alerts, e.EvaluateDecisionForScene(decision, "短线爆发", "draft")...)
	}
	return alerts
}

func (e *AlertEngine) EvaluateDecision(decision models.PredictionDecision) []PredictionAlert {
	return e.EvaluateDecisionForScene(decision, "短线爆发", "draft")
}

// EvaluateDecisionForScene only emits stateful events. A static REDUCE/SELL
// recommendation is not a price trigger and must not repeat on every scan.
func (e *AlertEngine) EvaluateDecisionForScene(decision models.PredictionDecision, scene, monitorMode string) []PredictionAlert {
	alerts := make([]PredictionAlert, 0, 4)
	name := decision.StockName
	if strings.TrimSpace(name) == "" {
		name = decision.StockCode
	}
	if strings.TrimSpace(scene) == "" {
		scene = "短线爆发"
	}
	if strings.TrimSpace(monitorMode) == "" {
		monitorMode = "watch"
	}
	now := time.Now()
	base := func(reason, level, title, message, action string, threshold float64) PredictionAlert {
		return PredictionAlert{
			Key:             fmt.Sprintf("prediction:%d:%s:%s", decision.SessionID, strings.ToLower(decision.StockCode), reason),
			SessionID:       decision.SessionID,
			DecisionID:      decision.ID,
			StockCode:       decision.StockCode,
			StockName:       name,
			Level:           normalizeAlertLevel(level),
			Title:           title,
			Message:         message,
			Reason:          reason,
			SuggestedAction: action,
			Scene:           scene,
			MonitorMode:     monitorMode,
			Price:           decision.CurrentPrice,
			ThresholdPrice:  threshold,
			CreatedAt:       now,
		}
	}

	if decision.HoldingVolume > 0 && decision.StopLossPrice > 0 && decision.CurrentPrice > 0 && decision.CurrentPrice <= decision.StopLossPrice {
		action := sceneAlertAction(scene, "stop_loss")
		alerts = append(alerts, base(
			"stop_loss", "high", "AI预测工厂止损提醒",
			fmt.Sprintf("%s 当前价 %.3f 已跌破止损位 %.3f，建议卖出或优先控制亏损。", name, decision.CurrentPrice, decision.StopLossPrice),
			action, decision.StopLossPrice,
		))
	} else if decision.HoldingVolume > 0 && decision.DefensePrice > 0 && decision.CurrentPrice > 0 && decision.CurrentPrice <= decision.DefensePrice {
		action := sceneAlertAction(scene, "defense_price")
		alerts = append(alerts, base(
			"defense_price", "medium", "AI预测工厂防守位提醒",
			fmt.Sprintf("%s 当前价 %.3f 跌破防守位 %.3f，建议观察承接情况，必要时减仓。", name, decision.CurrentPrice, decision.DefensePrice),
			action, decision.DefensePrice,
		))
	}

	if decision.HoldingVolume > 0 && decision.TakeProfitPrice > 0 && decision.CurrentPrice >= decision.TakeProfitPrice && (decision.Action == "REDUCE" || decision.Action == "SELL") {
		action := sceneAlertAction(scene, "take_profit")
		alerts = append(alerts, base(
			"take_profit", "medium", "AI预测工厂止盈/减仓提醒",
			fmt.Sprintf("%s 当前价 %.3f 达到止盈/减仓位 %.3f，可按建议数量执行减仓。", name, decision.CurrentPrice, decision.TakeProfitPrice),
			action, decision.TakeProfitPrice,
		))
	}

	if decision.HoldingVolume == 0 && (decision.Action == "BUY" || decision.Action == "ADD") &&
		decision.CurrentPrice > 0 && decision.CurrentPrice >= decision.BuyPriceMin && decision.CurrentPrice <= decision.BuyPriceMax {
		alerts = append(alerts, base(
			"entry_signal", "low", "AI预测工厂入场提醒",
			fmt.Sprintf("%s 当前价 %.3f 位于建议入场区间 %.3f-%.3f，可按建议仓位试探。", name, decision.CurrentPrice, decision.BuyPriceMin, decision.BuyPriceMax),
			decision.Action, decision.BuyPriceMax,
		))
	}

	if decision.HoldingVolume > 0 && hasStrongCapitalOutflow(decision.CapitalFlowJSON) {
		action := sceneAlertAction(scene, "capital_outflow")
		alerts = append(alerts, base(
			"capital_outflow", "medium", "AI预测工厂资金流风险提醒",
			fmt.Sprintf("%s 出现主力资金代理强流出信号，建议降低追涨和加仓优先级。", name),
			action, 0,
		))
	}
	return alerts
}

func (e *AlertEngine) EvaluateAdviceChange(decision models.PredictionDecision, scene, monitorMode, previousAction string) []PredictionAlert {
	previousAction = strings.ToUpper(strings.TrimSpace(previousAction))
	currentAction := strings.ToUpper(strings.TrimSpace(decision.Action))
	if previousAction == "" || currentAction == "" || previousAction == currentAction {
		return nil
	}
	name := decision.StockName
	if strings.TrimSpace(name) == "" {
		name = decision.StockCode
	}
	level := "medium"
	if currentAction == "SELL" {
		level = "high"
	} else if currentAction == "BUY" || currentAction == "ADD" {
		level = "low"
	}
	return []PredictionAlert{{
		Key:             fmt.Sprintf("prediction:%d:%s:advice_change", decision.SessionID, strings.ToLower(decision.StockCode)),
		SessionID:       decision.SessionID,
		DecisionID:      decision.ID,
		StockCode:       decision.StockCode,
		StockName:       name,
		Level:           level,
		Title:           "AI预测工厂建议变化提醒",
		Message:         fmt.Sprintf("%s 操作建议由 %s 变为 %s：%s", name, previousAction, currentAction, decision.PositionAdvice),
		Reason:          "advice_change",
		SuggestedAction: currentAction,
		Scene:           scene,
		MonitorMode:     monitorMode,
		Price:           decision.CurrentPrice,
		ThresholdPrice:  0,
		CreatedAt:       time.Now(),
	}}
}

func sceneAlertAction(scene, reason string) string {
	switch scene {
	case "趋势持有":
		switch reason {
		case "defense_price", "capital_outflow":
			return "WATCH"
		case "stop_loss":
			return "SELL"
		default:
			return "REDUCE"
		}
	case "波段反弹":
		switch reason {
		case "capital_outflow":
			return "WATCH"
		case "stop_loss":
			return "SELL"
		default:
			return "REDUCE"
		}
	default:
		if reason == "stop_loss" {
			return "SELL"
		}
		return "REDUCE"
	}
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
