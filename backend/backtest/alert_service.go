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

const (
	predictionAlertLookbackDays = 14
	predictionAlertCooldown     = 30 * time.Minute
	predictionQuoteMaxAge       = 3 * time.Minute
)

type PredictionAlertScanResult struct {
	Scanned   int                         `json:"scanned"`
	Generated int                         `json:"generated"`
	Sent      int                         `json:"sent"`
	Resolved  int                         `json:"resolved"`
	Stale     int                         `json:"stale"`
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
		resolved, err := s.resolveInactiveLogs(map[string]struct{}{}, nil)
		if err != nil {
			return nil, err
		}
		result.Resolved = resolved
		return result, nil
	}

	quotes := s.refreshRealtimeQuotes(decisions)
	codes := make([]string, 0, len(decisions))
	for _, monitored := range decisions {
		codes = append(codes, monitored.decision.StockCode)
	}
	holdings := holdingMap(codes)
	now := time.Now()
	activeKeys := make(map[string]struct{})
	evaluatedCodes := make(map[string]struct{})
	created := make([]models.PredictionAlertLog, 0)

	for _, monitored := range decisions {
		decision := monitored.decision
		code := normalizeAlertStockCode(decision.StockCode)
		applyRealtimeHolding(&decision, holdings[code])
		quote, ok := quotes[code]
		if !ok || quote.price <= 0 || !quote.fresh {
			result.Stale++
			continue
		}
		decision.CurrentPrice = quote.price
		if strings.TrimSpace(decision.StockName) == "" && strings.TrimSpace(quote.name) != "" {
			decision.StockName = quote.name
		}
		evaluatedCodes[code] = struct{}{}

		alerts := s.engine.EvaluateDecisionForScene(decision, monitored.scene, monitored.monitorMode)
		if decisionFreshForLiveEntry(decision, now) {
			alerts = append(alerts, s.engine.EvaluateAdviceChange(decision, monitored.scene, monitored.monitorMode, monitored.previousAction)...)
		}
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
			if err := db.Dao.Create(&log).Error; err != nil {
				logger.SugaredLogger.Errorf("save prediction alert log error: %v", err)
				continue
			}
			if sendNotification && monitored.monitorMode == "active" && data.NewAlertWindowsApi("go-stock AI预测提醒", log.Title, log.Message, "").SendNotification() {
				sentAt := time.Now()
				log.Status = "sent"
				log.Channel = "windows,app"
				log.SentAt = &sentAt
				result.Sent++
				if err := db.Dao.Model(&models.PredictionAlertLog{}).Where("id = ?", log.ID).Updates(map[string]any{
					"status": log.Status, "channel": log.Channel, "sent_at": log.SentAt,
				}).Error; err != nil {
					logger.SugaredLogger.Errorf("update prediction alert notification status error: %v", err)
				}
			}
			created = append(created, log)
		}
	}

	resolved, err := s.resolveInactiveLogs(activeKeys, evaluatedCodes)
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

	candidates := make(map[string]monitoredPredictionDecision, len(rows))
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		key := normalizeAlertStockCode(row.StockCode)
		if key == "" {
			continue
		}
		scene, mode, monitored := s.monitorContext(row.SessionID)
		if !monitored {
			continue
		}
		if existing, ok := candidates[key]; ok && (existing.monitorMode == "active" || mode != "active") {
			continue
		}
		previousAction := strings.ToUpper(strings.TrimSpace(row.PreviousAction))
		if previousAction == "" {
			previousAction = s.previousMonitoredAction(row)
		}
		candidate := monitoredPredictionDecision{
			decision:       row,
			scene:          scene,
			monitorMode:    mode,
			previousAction: previousAction,
		}
		if _, exists := candidates[key]; !exists {
			order = append(order, key)
		}
		candidates[key] = candidate
	}
	result := make([]monitoredPredictionDecision, 0, len(order))
	for _, key := range order {
		result = append(result, candidates[key])
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
	at    time.Time
	fresh bool
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
		quoteAt := parseAlertQuoteTime(info.Date, info.Time)
		result[code] = alertQuoteSnapshot{
			price: price,
			name:  strings.TrimSpace(info.Name),
			at:    quoteAt,
			fresh: freshAlertQuote(quoteAt, time.Now()),
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
		return time.Since(latest.TriggeredAt) < predictionAlertCooldown
	}
	return true
}

func (s *PredictionAlertService) resolveInactiveLogs(activeKeys, evaluatedCodes map[string]struct{}) (int, error) {
	cutoff := time.Now().AddDate(0, 0, -predictionAlertLookbackDays)
	var logs []models.PredictionAlertLog
	err := db.Dao.Where("status IN ? AND triggered_at >= ?", []string{"new", "sent", "read", "ignored"}, cutoff).
		Find(&logs).Error
	if err != nil {
		return 0, err
	}

	resolved := 0
	for _, log := range logs {
		if evaluatedCodes != nil {
			if _, ok := evaluatedCodes[normalizeAlertStockCode(log.StockCode)]; !ok {
				continue
			}
		}
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
	scene := strings.TrimSpace(strings.ToLower(alert.Scene))
	if reason == "advice_change" {
		reason += ":" + strings.ToLower(strings.TrimSpace(alert.SuggestedAction))
	}
	return fmt.Sprintf("prediction_live:%s:%s:%s", code, scene, reason)
}

func applyRealtimeHolding(decision *models.PredictionDecision, holding holdingSnapshot) {
	if decision == nil {
		return
	}
	if holding.volume <= 0 || holding.costPrice <= 0 {
		decision.HoldingVolume = 0
		decision.CostPrice = 0
		return
	}
	oldCost := decision.CostPrice
	if oldCost > 0 {
		factor := holding.costPrice / oldCost
		if factor >= 0.1 && factor <= 10 {
			if decision.DefensePrice > 0 {
				decision.DefensePrice *= factor
			}
			if decision.StopLossPrice > 0 {
				decision.StopLossPrice *= factor
			}
			if decision.TakeProfitPrice > 0 {
				decision.TakeProfitPrice *= factor
			}
		}
	}
	decision.HoldingVolume = holding.volume
	decision.CostPrice = holding.costPrice
	if strings.TrimSpace(holding.name) != "" {
		decision.StockName = holding.name
	}
}

func parseAlertQuoteTime(date, clock string) time.Time {
	date = strings.TrimSpace(strings.ReplaceAll(date, "/", "-"))
	clock = strings.TrimSpace(clock)
	if date == "" || clock == "" {
		return time.Time{}
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if parsed, err := time.ParseInLocation(layout, date+" "+clock, time.FixedZone("Asia/Shanghai", 8*60*60)); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func freshAlertQuote(quoteAt, now time.Time) bool {
	if quoteAt.IsZero() {
		return false
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	localNow := now.In(location)
	localQuote := quoteAt.In(location)
	if localNow.Format("2006-01-02") != localQuote.Format("2006-01-02") {
		return false
	}
	age := localNow.Sub(localQuote)
	return age >= -2*time.Minute && age <= predictionQuoteMaxAge
}

func decisionFreshForLiveEntry(decision models.PredictionDecision, now time.Time) bool {
	if strings.TrimSpace(decision.DecisionDate) == "" {
		return false
	}
	decisionDate, err := time.ParseInLocation("2006-01-02", decision.DecisionDate, time.FixedZone("Asia/Shanghai", 8*60*60))
	if err != nil {
		return false
	}
	age := now.In(decisionDate.Location()).Sub(decisionDate)
	return age >= 0 && age <= 4*24*time.Hour
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
