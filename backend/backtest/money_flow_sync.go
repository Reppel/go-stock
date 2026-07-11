package backtest

import (
	"errors"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

type MoneyFlowSyncResult struct {
	StockCount      int `json:"stockCount"`
	FlowRows        int `json:"flowRows"`
	SectorRows      int `json:"sectorRows"`
	ConceptRows     int `json:"conceptRows"`
	MacRows         int `json:"macRows"`
	FailedStocks    int `json:"failedStocks"`
	PartialStocks   int `json:"partialStocks"`
	HistoricalIssue int `json:"historicalIssue"`
}

func (s *FeatureSyncService) SyncMoneyFlows(stockCodes []string, days int) (*MoneyFlowSyncResult, error) {
	if err := ensurePredictionTables(); err != nil {
		return nil, err
	}
	result := &MoneyFlowSyncResult{}
	if days <= 0 {
		days = 365
	}
	if len(stockCodes) == 0 {
		return result, nil
	}

	for _, code := range uniqueStrings(stockCodes) {
		rows, macRows, err := s.SyncStockMoneyFlow(code, days)
		if err != nil {
			if macRows > 0 {
				result.StockCount++
				result.MacRows += macRows
				result.PartialStocks++
				result.HistoricalIssue++
				logger.SugaredLogger.Warnf("sync money flow for %s partially available: %v", code, err)
				continue
			}
			result.FailedStocks++
			logger.SugaredLogger.Warnf("sync money flow for %s error: %v", code, err)
			continue
		}
		result.StockCount++
		result.FlowRows += rows
		result.MacRows += macRows
	}
	sectorRows, conceptRows := SyncSectorMoneyFlows()
	result.SectorRows = sectorRows
	result.ConceptRows = conceptRows
	return result, nil
}

func (s *FeatureSyncService) SyncStockMoneyFlow(stockCode string, days int) (int, int, error) {
	if err := ensurePredictionTables(); err != nil {
		return 0, 0, err
	}
	code := normalizeMoneyFlowStockCode(stockCode)
	if code == "" {
		return 0, 0, fmt.Errorf("empty stock code")
	}
	rows := data.NewStockDataApi().GetStockHistoryMoneyData(code)
	if len(rows) == 0 {
		// EastMoney occasionally returns an empty response transiently. Retry once
		// before marking the historical source as unavailable.
		rows = data.NewStockDataApi().GetStockHistoryMoneyData(code)
	}
	if len(rows) == 0 {
		macRows := syncMACMoneyFlow(code)
		return 0, macRows, fmt.Errorf("东方财富历史资金流暂无数据")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Date < rows[j].Date })
	if days > 0 && len(rows) > days {
		rows = rows[len(rows)-days:]
	}

	mainValues := make([]float64, len(rows))
	for i, row := range rows {
		mainValues[i] = parseMoneyFlowFloat(row.F62)
	}

	saved := 0
	for i, row := range rows {
		tradeDate := normalizeMoneyFlowDate(row.Date)
		if tradeDate == "" {
			continue
		}
		flow := models.StockMoneyFlowDaily{
			StockCode:          code,
			TradeDate:          tradeDate,
			DataAsOf:           featureDataAsOf(tradeDate),
			MainNetInflow1:     mainValues[i],
			MainNetInflow5:     rollingSum(mainValues, i, 5),
			MainNetInflow20:    rollingSum(mainValues, i, 20),
			MainNetInflowRatio: parseMoneyFlowFloat(row.F184) / 100.0,
			SuperLargeNet1:     parseMoneyFlowFloat(row.F66),
			SuperLargeRatio:    parseMoneyFlowFloat(row.F69) / 100.0,
			LargeNet1:          parseMoneyFlowFloat(row.F72),
			LargeRatio:         parseMoneyFlowFloat(row.F75) / 100.0,
			MediumNet1:         parseMoneyFlowFloat(row.F78),
			MediumRatio:        parseMoneyFlowFloat(row.F81) / 100.0,
			SmallNet1:          parseMoneyFlowFloat(row.F84),
			SmallRatio:         parseMoneyFlowFloat(row.F87) / 100.0,
			RetailNetInflow1:   parseMoneyFlowFloat(row.F84),
			Source:             "eastmoney",
		}
		if err := upsertStockMoneyFlow(flow); err != nil {
			logger.SugaredLogger.Warnf("upsert stock money flow %s %s error: %v", code, tradeDate, err)
			continue
		}
		updateFeatureFundFlow(code, tradeDate, flow.MainNetInflow5, flow.MainNetInflow20)
		saved++
	}
	macRows := syncMACMoneyFlow(code)
	return saved, macRows, nil
}

func SyncSectorMoneyFlows() (int, int) {
	sectorSaved := 0
	if _, err := data.NewBKFundFlowApi().FetchAndSave(); err != nil {
		logger.SugaredLogger.Warnf("fetch board fund flow error: %v", err)
	}
	var sectorLatest string
	db.Dao.Model(&models.BKFundFlow{}).Select("MAX(snap_time)").Scan(&sectorLatest)
	if sectorLatest != "" {
		var rows []models.BKFundFlow
		db.Dao.Where("snap_time = ?", sectorLatest).Order("net_inflow desc").Limit(100).Find(&rows)
		for i, row := range rows {
			flow := models.SectorFlowDaily{
				SectorType: "industry",
				SectorName: row.Name,
				TradeDate:  snapDate(sectorLatest),
				DataAsOf:   parseSnapTime(sectorLatest),
				NetInflow:  float64(row.NetInflow),
				Rank:       i + 1,
				Source:     "eastmoney_board",
			}
			if upsertSectorFlow(flow) == nil {
				sectorSaved++
			}
		}
	}

	conceptSaved := 0
	if _, err := data.NewConceptFundFlowApi().FetchAndSave(); err != nil {
		logger.SugaredLogger.Warnf("fetch concept fund flow error: %v", err)
	}
	var conceptLatest string
	db.Dao.Model(&models.ConceptFundFlow{}).Select("MAX(snap_time)").Scan(&conceptLatest)
	if conceptLatest != "" {
		var rows []models.ConceptFundFlow
		db.Dao.Where("snap_time = ?", conceptLatest).Order("net_inflow desc").Limit(100).Find(&rows)
		for i, row := range rows {
			flow := models.SectorFlowDaily{
				SectorType: "concept",
				SectorName: row.Name,
				TradeDate:  snapDate(conceptLatest),
				DataAsOf:   parseSnapTime(conceptLatest),
				NetInflow:  float64(row.NetInflow),
				Rank:       i + 1,
				Source:     "eastmoney_concept",
			}
			if upsertSectorFlow(flow) == nil {
				conceptSaved++
			}
		}
	}
	return sectorSaved, conceptSaved
}

func syncMACMoneyFlow(stockCode string) int {
	row := data.NewTdxKLineApi().GetMACCapitalFlow(stockCode)
	if row == nil {
		return 0
	}
	code := normalizeMoneyFlowStockCode(stockCode)
	today := time.Now().Format("2006-01-02")
	flow := models.StockMoneyFlowDaily{
		StockCode:         code,
		TradeDate:         today,
		DataAsOf:          time.Now(),
		MainNetInflow1:    row.TodayMainNetIn,
		MainNetInflow5:    row.FiveDayMainNetIn,
		SuperLargeNet1:    row.FiveDaySuperNet,
		LargeNet1:         row.FiveDayLargeNet,
		MediumNet1:        row.FiveDayMediumNet,
		SmallNet1:         row.FiveDaySmallNet,
		RetailNetInflow1:  row.TodayRetailNetIn,
		MacMainNetInflow1: row.TodayMainNetIn,
		MacMainNetInflow5: row.FiveDayMainNetIn,
		MacRetailNetIn1:   row.TodayRetailNetIn,
		Source:            "tdx_mac",
	}
	if err := upsertStockMoneyFlow(flow); err != nil {
		logger.SugaredLogger.Warnf("upsert mac money flow %s error: %v", code, err)
		return 0
	}
	updateFeatureFundFlow(code, today, flow.MainNetInflow5, flow.MainNetInflow20)
	return 1
}

func upsertStockMoneyFlow(flow models.StockMoneyFlowDaily) error {
	var existing models.StockMoneyFlowDaily
	err := db.Dao.Where("stock_code = ? AND trade_date = ?", flow.StockCode, flow.TradeDate).First(&existing).Error
	if err == nil {
		flow.ID = existing.ID
		return db.Dao.Save(&flow).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return db.Dao.Create(&flow).Error
}

func upsertSectorFlow(flow models.SectorFlowDaily) error {
	var existing models.SectorFlowDaily
	err := db.Dao.Where("sector_type = ? AND sector_name = ? AND trade_date = ?", flow.SectorType, flow.SectorName, flow.TradeDate).
		First(&existing).Error
	if err == nil {
		flow.ID = existing.ID
		return db.Dao.Save(&flow).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return db.Dao.Create(&flow).Error
}

func updateFeatureFundFlow(stockCode string, tradeDate string, flow5 float64, flow20 float64) {
	if stockCode == "" || tradeDate == "" {
		return
	}
	db.Dao.Model(&models.StockFeature{}).
		Where("stock_code IN ? AND date = ? AND feature_version = ?", stockCodeVariants(stockCode), tradeDate, CurrentFeatureVersion).
		Updates(map[string]any{
			"fund_flow5":  flow5,
			"fund_flow20": flow20,
		})
}

func parseMoneyFlowFloat(raw string) float64 {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, ",", ""))
	if raw == "" || raw == "-" {
		return 0
	}
	value, _ := strconv.ParseFloat(raw, 64)
	return value
}

func rollingSum(values []float64, idx int, window int) float64 {
	if window <= 0 || idx < 0 || idx >= len(values) {
		return 0
	}
	start := idx - window + 1
	if start < 0 {
		start = 0
	}
	sum := 0.0
	for i := start; i <= idx; i++ {
		sum += values[i]
	}
	return sum
}

func normalizeMoneyFlowStockCode(code string) string {
	code = strings.TrimSpace(strings.ToLower(code))
	if code == "" {
		return ""
	}
	if strings.Contains(code, " - ") {
		code = strings.Split(code, " - ")[0]
	}
	if strings.Contains(code, ".") {
		parts := strings.Split(code, ".")
		if len(parts) == 2 {
			return strings.ToLower(parts[1] + parts[0])
		}
	}
	if strings.HasPrefix(code, "sh") || strings.HasPrefix(code, "sz") || strings.HasPrefix(code, "bj") {
		return code
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

func normalizeMoneyFlowDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, layout := range []string{"2006-01-02", "20060102", "2006/01/02"} {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t.Format("2006-01-02")
		}
	}
	if len(raw) >= 10 {
		return raw[:10]
	}
	return raw
}

func snapDate(snap string) string {
	if len(snap) >= 10 {
		return snap[:10]
	}
	return time.Now().Format("2006-01-02")
}

func parseSnapTime(snap string) time.Time {
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", snap, time.Local); err == nil {
		return t
	}
	if t, err := time.ParseInLocation("2006-01-02", snapDate(snap), time.Local); err == nil {
		return t
	}
	return time.Now()
}
