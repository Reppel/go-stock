package backtest

import (
	"errors"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"strconv"
	"sync"

	"gorm.io/gorm"
)

// FeatureSyncService 特征数据同步服务
type FeatureSyncService struct {
	klineApi *data.EastMoneyKLineApi
}

// NewFeatureSyncService 创建特征同步服务
func NewFeatureSyncService() *FeatureSyncService {
	return &FeatureSyncService{
		klineApi: data.NewEastMoneyKLineApi(data.GetSettingConfig()),
	}
}

// SyncStockFeatures 同步指定股票的特征数据
func (s *FeatureSyncService) SyncStockFeatures(stockCode string, days int) error {
	if err := ensurePredictionTables(); err != nil {
		return err
	}

	// 获取日 K 线数据
	klines := s.klineApi.GetDayKLine(stockCode, days)
	if klines == nil || len(*klines) == 0 {
		return nil
	}

	// 转换成 KLineData 切片
	klineData := *klines

	// 从后往前遍历，生成每个交易日的特征
	for i := 60; i < len(klineData); i++ {
		// 取当前及之前的数据
		subset := klineData[:i+1]

		// 计算特征
		features := CalculateFeaturesFromKLines(subset)

		// 写入数据库
		feature := s.toStockFeature(stockCode, klineData[i], features)
		if err := s.upsertFeature(feature); err != nil {
			logger.SugaredLogger.Errorf("upsert feature for %s on %s error: %v", stockCode, feature.Date, err)
			continue
		}
	}

	return nil
}

// SyncAllStockFeatures 同步所有股票特征
func (s *FeatureSyncService) SyncAllStockFeatures(stockCodes []string, days int) {
	if err := ensurePredictionTables(); err != nil {
		logger.SugaredLogger.Errorf("ensure prediction tables error: %v", err)
		return
	}

	if len(stockCodes) == 0 {
		stockCodes = NewStockPoolService().GetStockPool("全部A股")
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 5) // 限制并发数

	for _, code := range stockCodes {
		wg.Add(1)
		go func(stockCode string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := s.SyncStockFeatures(stockCode, days); err != nil {
				logger.SugaredLogger.Errorf("sync features for %s error: %v", stockCode, err)
			}
		}(code)
	}

	wg.Wait()
}

// toStockFeature 转换特征到数据库模型
func (s *FeatureSyncService) toStockFeature(stockCode string, kline data.KLineData, features *KLineFeatures) models.StockFeature {
	close, _ := strconv.ParseFloat(kline.Close, 64)
	open, _ := strconv.ParseFloat(kline.Open, 64)
	high, _ := strconv.ParseFloat(kline.High, 64)
	low, _ := strconv.ParseFloat(kline.Low, 64)
	volume, _ := strconv.ParseFloat(kline.Volume, 64)
	turnover, _ := strconv.ParseFloat(kline.Amount, 64)

	return models.StockFeature{
		StockCode:    stockCode,
		Date:         kline.Day,
		Close:        close,
		Open:         open,
		High:         high,
		Low:          low,
		Volume:       volume,
		Turnover:     turnover,
		MA5:          features.MA5,
		MA10:         features.MA10,
		MA20:         features.MA20,
		MA60:         features.MA60,
		MACD:         features.MACD,
		RSI6:         features.RSI6,
		RSI12:        features.RSI12,
		KDJ_K:        features.KDJ_K,
		BOLLUpper:    features.BOLLUpper,
		BOLLMid:      features.BOLLMid,
		BOLLLower:    features.BOLLLower,
		VolumeRatio:  features.VolumeRatio,
		ATR:          features.ATR,
		FundFlow5:    features.FundFlow5,
		FundFlow20:   features.FundFlow20,
		ChangeRate5:  features.ChangeRate5,
		ChangeRate20: features.ChangeRate20,
	}
}

// upsertFeature 写入或更新特征
func (s *FeatureSyncService) upsertFeature(feature models.StockFeature) error {
	// 先查是否存在
	var existing models.StockFeature
	err := db.Dao.Where("stock_code = ? AND date = ?", feature.StockCode, feature.Date).First(&existing).Error
	if err == nil {
		// 更新
		feature.ID = existing.ID
		return db.Dao.Save(&feature).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	// 新建
	return db.Dao.Create(&feature).Error
}
