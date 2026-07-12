package db

import (
	"go-stock/backend/models"
	"log"
	"time"
)

type ChatMemory struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	SessionID string    `gorm:"index;size:64" json:"sessionId"`
	Role      string    `gorm:"size:20" json:"role"`
	Content   string    `gorm:"type:text" json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

func (ChatMemory) TableName() string {
	return "chat_memory"
}

func (c *ChatMemory) Save() error {
	return Dao.Create(c).Error
}

func GetChatMemoryList(sessionID string, limit int) ([]ChatMemory, error) {
	var memories []ChatMemory
	err := Dao.Where("session_id = ?", sessionID).
		Order("created_at DESC").
		Limit(limit).
		Find(&memories).Error
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(memories)-1; i < j; i, j = i+1, j-1 {
		memories[i], memories[j] = memories[j], memories[i]
	}
	return memories, nil
}

func GetRecentChatMemory(sessionID string, limit int) ([]ChatMemory, error) {
	var memories []ChatMemory
	var err error
	if sessionID == "" {
		err = Dao.Order("created_at DESC").
			Limit(limit).
			Find(&memories).Error
	} else {
		err = Dao.Where("session_id = ?", sessionID).
			Order("created_at DESC").
			Limit(limit).
			Find(&memories).Error
	}
	for i, j := 0, len(memories)-1; i < j; i, j = i+1, j-1 {
		memories[i], memories[j] = memories[j], memories[i]
	}
	return memories, err
}

func ClearChatMemory(sessionID string) error {
	return Dao.Where("session_id = ?", sessionID).Delete(&ChatMemory{}).Error
}

func AutoMigrate() {
	migrateStockEventDailyV2()
	if err := Dao.AutoMigrate(
		&ChatMemory{}, &models.StockChangeHistory{}, &models.MarketStatistic{},
		&models.PredictionSession{}, &models.PredictionHypothesis{}, &models.PredictionSignal{},
		&models.PredictionHypothesisDaily{}, &models.StockFeature{}, &models.FeatureSyncJob{},
		&models.PredictionTrade{}, &models.TradeDecisionLog{}, &models.PredictionGenerationAudit{},
		&models.PredictionResearchIdea{}, &models.MarketFactorDaily{}, &models.StockMoneyFlowDaily{},
		&models.SectorFlowDaily{}, &models.StockEventDaily{}, &models.StockRiskEvent{},
		&models.CandidateSnapshot{}, &models.CandidateSnapshotItem{}, &models.CandidateSourceFact{},
		&models.StockScreeningFactDaily{}, &models.UplimitStockDaily{}, &models.ModelRecommendationEvent{},
		&models.ScreeningExecutionSnapshot{}, &models.ScreeningExecutionItem{},
	); err != nil {
		log.Printf("prediction schema migration failed: %v", err)
	}
}

// migrateStockEventDailyV2 prepares legacy installations before the v2 unique
// point-in-time key is created. Without this step duplicate legacy rows can
// make SQLite reject the new unique index during AutoMigrate.
func migrateStockEventDailyV2() {
	if Dao == nil || !Dao.Migrator().HasTable(&models.StockEventDaily{}) {
		return
	}
	if !Dao.Migrator().HasColumn(&models.StockEventDaily{}, "feature_version") {
		_ = Dao.Migrator().AddColumn(&models.StockEventDaily{}, "FeatureVersion")
	}
	_ = Dao.Exec("UPDATE stock_event_daily SET feature_version = ? WHERE feature_version IS NULL OR TRIM(feature_version) = ''", "daily_v2_qfq").Error
	_ = Dao.Exec(`DELETE FROM stock_event_daily WHERE id NOT IN (
		SELECT MAX(id) FROM stock_event_daily GROUP BY stock_code, trade_date, feature_version
	)`).Error
}
