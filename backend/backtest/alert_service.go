package backtest

import (
	"errors"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

const predictionAlertLookbackDays = 14

type PredictionAlertScanResult struct {
	Scanned   int                         `json:"scanned"`
	Generated int                         `json:"generated"`
	Sent      int                         `json:"sent"`
	Resolved  int                         `json:"resolved"`
	Alerts    []models.PredictionAlertLog `json:"alerts"`
}

type PredictionAlertService struct {
	engine *AlertEngine
}

type monitoredPredictionDecision struct {
	decision       models.PredictionDecision
	scene          string
	monitorMode    string
	previousAction string
}

func NewPredictionAlertService() *PredictionAlertService {
	return &PredictionAlertService{
		engine: NewAlertEngine(),
	}
}

func (s *PredictionAlertService) ScanRealtimeAlerts(sendNotification bool) (*PredictionAlertScanResult, error) {
	if err := ensurePredictionTables(); err != nil {
		return nil, err
	}

	decisions := s.latestMonitoredDecisionsByStock(500)
	result := &PredictionAlertScanResult{Scanned: len(decisions)}
	if len(decisions) == 0 {
		resolved, err := s.resolveInactiveLogs(map[string]struct{}{})
		if err != nil {
			return nil, err
		}
		result.Resolved = resolved
		return result, nil
	}

	quotes := s.refreshRealtimeQuotes(decisions)
	now := time.Now()
	activeKeys := make(map[string]struct{})
	created := make([]models.PredictionAlertLog, 0)

	for _, monitored := range decisions {
		decision := monitored.decision
		if quote, ok := quotes[normalizeAlertStockCode(decision.StockCode)]; ok {
			if quote.price > 0 {
				decision.CurrentPrice = quote.price
			}
			if strings.TrimSpace(decision.StockName) == "" && strings.TrimSpace(quote.name) != "" {
				decision.StockName = quote.name
			}
		}

		alerts := s.engine.EvaluateDecisionForScene(decision, monitored.scene, monitored.monitorMode)
		alerts = append(alerts, s.engine.EvaluateAdviceChange(decision, monitored.scene, monitored.monitorMode, monitored.previousAction)...)
		result.Generated += len(alerts)
		for _, alert := range alerts {
			key := liveAlertKey(alert)
			if key == "" {
				continue
			}
			activeKeys[key] = struct{}{}
			if s.recentlyAlerted(key) {
				continue
			}

			log := models.PredictionAlertLog{
				AlertKey:        key,
				SessionID:       decision.SessionID,
				DecisionID:      decision.ID,
				StockCode:       decision.StockCode,
				StockName:       alert.StockName,
				AlertType:       alert.Reason,
				Level:           normalizeAlertLevel(alert.Level),
				Title:           alert.Title,
				Message:         alert.Message,
				TriggerPrice:    alert.Price,
				ThresholdPrice:  alert.ThresholdPrice,
				SuggestedAction: alert.SuggestedAction,
				Scene:           alert.Scene,
				MonitorMode:     alert.MonitorMode,
				Status:          "new",
				Channel:         "app",
				Reason:          alert.Reason,
				TriggeredAt:     now,
			}
			if sendNotification && data.NewAlertWindowsApi("go-stock AI预测提醒", log.Title, log.Message, "").SendNotification() {
				sentAt := time.Now()
				log.Status = "sent"
				log.Channel = "windows,app"
				log.SentAt = &sentAt
				result.Sent++
			}
			if err := db.Dao.Create(&log).Error; err != nil {
				logger.SugaredLogger.Errorf("save prediction alert log error: %v", err)
				continue
			}
			created = append(created, log)
		}
	}

	resolved, err := s.resolveInactiveLogs(activeKeys)
	if err != nil {
		logger.SugaredLogger.Errorf("resolve prediction alert logs error: %v", err)
	}
	result.Resolved = resolved
	result.Alerts = created
	return result, nil
}

func (s *PredictionAlertService) GetAlertLogs(limit int, status string) []models.PredictionAlertLog {
	if err := ensurePredictionTables(); err != nil {
		logger.SugaredLogger.Errorf("ensure prediction tables error: %v", err)
		return nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	query := db.Dao.Model(&models.PredictionAlertLog{}).Where("status <> ?", "ignored")
	status = strings.TrimSpace(strings.ToLower(status))
	if status != "" && status != "all" {
		query = query.Where("status = ?", status)
	}
	var logs []models.PredictionAlertLog
	query.Order("triggered_at desc, id desc").Limit(limit).Find(&logs)
	return logs
}

func (s *PredictionAlertService) MarkAlertStatus(id uint, status string) error {
	if err := ensurePredictionTables(); err != nil {
		return err
	}
	status = strings.TrimSpace(strings.ToLower(status))
	if status != "read" && status != "ignored" && status != "resolved" {
		return fmt.Errorf("unsupported alert status: %s", status)
	}
	updates := map[string]any{
		"status": status,
	}
	if status == "read" || status == "ignored" {
		now := time.Now()
		updates["read_at"] = &now
	}
	return db.Dao.Model(&models.PredictionAlertLog{}).Where("id = ?", id).Updates(updates).Error
}

func (s *PredictionAlertService) latestMonitoredDecisionsByStock(limit int) []monitoredPredictionDecision {
	cutoff := time.Now().AddDate(0, 0, -predictionAlertLookbackDays)
	var rows []models.PredictionDecision
	db.Dao.Where("created_at >= ?", cutoff).
		Order("created_at desc, id desc").
		Limit(limit).
		Find(&rows)

	result := make([]monitoredPredictionDecision, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		key := normalizeAlertStockCode(row.StockCode)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		scene, mode, monitored := s.monitorContext(row.SessionID)
		if !monitored {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, monitoredPredictionDecision{
			decision:       row,
			scene:          scene,
			monitorMode:    mode,
			previousAction: s.previousMonitoredAction(row),
		})
	}
	return result
}

func (s *PredictionAlertService) monitorContext(sessionID uint) (string, string, bool) {
	var session models.PredictionSession
	if err := db.Dao.First(&session, sessionID).Error; err != nil || session.Status != "done" {
		return "", "", false
	}
	var formalCount int64
	var watchCount int64
	db.Dao.Model(&models.PredictionHypothesis{}).
		Where("session_id = ? AND status = ?", sessionID, "active").Count(&formalCount)
	db.Dao.Model(&models.PredictionHypothesis{}).
		Where("session_id = ? AND status = ?", sessionID, "watch").Count(&watchCount)
	if formalCount == 0 && watchCount == 0 {
		return "", "", false
	}
	if formalCount > 0 {
		return session.Scene, "active", true
	}
	return session.Scene, "watch", true
}

func (s *PredictionAlertService) previousMonitoredAction(current models.PredictionDecision) string {
	var rows []models.PredictionDecision
	db.Dao.Where("id <> ? AND stock_code IN ?", current.ID, stockCodeVariants(current.StockCode)).
		Order("created_at desc, id desc").
		Limit(50).
		Find(&rows)
	for _, row := range rows {
		if _, _, monitored := s.monitorContext(row.SessionID); monitored {
			return row.Action
		}
	}
	return ""
}

type alertQuoteSnapshot struct {
	price float64
	name  string
}

func (s *PredictionAlertService) refreshRealtimeQuotes(decisions []monitoredPredictionDecision) map[string]alertQuoteSnapshot {
	result := make(map[string]alertQuoteSnapshot, len(decisions))
	codes := make([]string, 0, len(decisions))
	seen := make(map[string]struct{}, len(decisions))
	for _, monitored := range decisions {
		decision := monitored.decision
		code := normalizeAlertStockCode(decision.StockCode)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		return result
	}

	stockInfos, err := data.NewStockDataApi().GetStockCodeRealTimeData(codes...)
	if err != nil {
		logger.SugaredLogger.Errorf("refresh prediction alert quotes error: %v", err)
		return result
	}
	for _, info := range *stockInfos {
		code := normalizeAlertStockCode(info.Code)
		if code == "" {
			continue
		}
		price, _ := strconv.ParseFloat(strings.TrimSpace(info.Price), 64)
		if price <= 0 {
			continue
		}
		result[code] = alertQuoteSnapshot{
			price: price,
			name:  strings.TrimSpace(info.Name),
		}
	}
	return result
}

func (s *PredictionAlertService) recentlyAlerted(alertKey string) bool {
	var latest models.PredictionAlertLog
	err := db.Dao.Where("alert_key = ?", alertKey).
		Order("triggered_at desc, id desc").
		First(&latest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false
	}
	if err != nil {
		logger.SugaredLogger.Errorf("query prediction alert log error: %v", err)
		return true
	}
	if latest.Status == "resolved" {
		return false
	}
	return true
}

func (s *PredictionAlertService) resolveInactiveLogs(activeKeys map[string]struct{}) (int, error) {
	cutoff := time.Now().AddDate(0, 0, -predictionAlertLookbackDays)
	var logs []models.PredictionAlertLog
	err := db.Dao.Where("status IN ? AND triggered_at >= ?", []string{"new", "sent", "read", "ignored"}, cutoff).
		Find(&logs).Error
	if err != nil {
		return 0, err
	}

	resolved := 0
	for _, log := range logs {
		if _, ok := activeKeys[log.AlertKey]; ok {
			continue
		}
		if err := db.Dao.Model(&models.PredictionAlertLog{}).
			Where("id = ?", log.ID).
			Update("status", "resolved").Error; err != nil {
			return resolved, err
		}
		resolved++
	}
	return resolved, nil
}

func liveAlertKey(alert PredictionAlert) string {
	code := normalizeAlertStockCode(alert.StockCode)
	reason := strings.TrimSpace(strings.ToLower(alert.Reason))
	if code == "" || reason == "" {
		return ""
	}
	return fmt.Sprintf("prediction_live:%s:%d:%s", code, alert.DecisionID, reason)
}

func normalizeAlertStockCode(code string) string {
	code = strings.TrimSpace(strings.ToLower(code))
	if code == "" {
		return ""
	}
	code = strings.ReplaceAll(code, "_", "")
	if strings.Contains(code, ".") {
		parts := strings.Split(code, ".")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return strings.ToLower(parts[1] + parts[0])
		}
	}
	for _, prefix := range []string{"sh", "sz", "bj", "hk", "gb"} {
		if strings.HasPrefix(code, prefix) {
			return code
		}
	}
	if len(code) >= 6 {
		base := code
		if len(base) > 6 {
			base = base[len(base)-6:]
		}
		switch base[0] {
		case '6':
			return "sh" + base
		case '0', '3':
			return "sz" + base
		case '8', '9':
			return "bj" + base
		}
	}
	return code
}
