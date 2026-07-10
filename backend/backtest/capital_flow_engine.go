package backtest

import (
	"go-stock/backend/db"
	"go-stock/backend/models"
	"strings"
	"time"
)

type CapitalFlowEngine struct{}

func NewCapitalFlowEngine() *CapitalFlowEngine {
	return &CapitalFlowEngine{}
}

func (e *CapitalFlowEngine) GetStockFlow(stockCode string, date string, f models.StockFeature) CapitalFlowSignal {
	signal := CapitalFlowSignal{
		StockCode:        stockCode,
		TradeDate:        date,
		FundFlow5:        f.FundFlow5,
		FundFlow20:       f.FundFlow20,
		Level:            "unknown",
		ProxySignal:      "public_main_order_proxy",
		NorthboundSignal: "reserved_unavailable",
	}

	var flow models.StockMoneyFlowDaily
	err := db.Dao.Where("stock_code IN ? AND trade_date <= ?", stockCodeVariants(stockCode), date).
		Order("trade_date desc").
		First(&flow).Error
	if err == nil {
		signal.TradeDate = flow.TradeDate
		signal.MainNetInflow1 = firstNonZero(flow.MacMainNetInflow1, flow.MainNetInflow1)
		signal.MainNetInflow5 = firstNonZero(flow.MacMainNetInflow5, flow.MainNetInflow5)
		signal.MainNetInflow20 = flow.MainNetInflow20
		signal.MainNetInflowRatio = flow.MainNetInflowRatio
		signal.SuperLargeNet1 = flow.SuperLargeNet1
		signal.SuperLargeRatio = flow.SuperLargeRatio
		signal.LargeNet1 = flow.LargeNet1
		signal.LargeRatio = flow.LargeRatio
		signal.MediumNet1 = flow.MediumNet1
		signal.MediumRatio = flow.MediumRatio
		signal.SmallNet1 = flow.SmallNet1
		signal.SmallRatio = flow.SmallRatio
		signal.RetailNetInflow1 = firstNonZero(flow.MacRetailNetIn1, flow.RetailNetInflow1, flow.SmallNet1)
		signal.MacMainNetInflow1 = flow.MacMainNetInflow1
		signal.MacMainNetInflow5 = flow.MacMainNetInflow5
		signal.MacRetailNetIn1 = flow.MacRetailNetIn1
		signal.NorthboundNetIn = flow.NorthboundNetIn
		signal.SectorNetInflow = flow.SectorNetInflow
		signal.ConceptNetInflow = flow.ConceptNetInflow
		signal.DataAsOf = flow.DataAsOf
	}
	if signal.DataAsOf.IsZero() {
		signal.DataAsOf = f.DataAsOf
	}
	if signal.DataAsOf.IsZero() {
		signal.DataAsOf = time.Now()
	}

	sectorNet, sectorSignal := latestSectorNet("industry", date)
	conceptNet, conceptSignal := latestSectorNet("concept", date)
	if signal.SectorNetInflow == 0 {
		signal.SectorNetInflow = sectorNet
	}
	if signal.ConceptNetInflow == 0 {
		signal.ConceptNetInflow = conceptNet
	}
	signal.SectorSignal = sectorSignal
	signal.ConceptSignal = conceptSignal
	signal.SuperLargeSignal = amountSignal(signal.SuperLargeNet1)
	signal.LargeSignal = amountSignal(signal.LargeNet1)
	signal.RetailSignal = amountSignal(signal.RetailNetInflow1)

	if signal.MainNetInflow1 == 0 && signal.MainNetInflow5 == 0 && signal.MainNetInflow20 == 0 && signal.FundFlow5 == 0 && signal.FundFlow20 == 0 {
		signal.Reasons = append(signal.Reasons, "资金流数据不足，按中性处理")
		return signal
	}

	adjustment := 0.0
	if signal.MainNetInflowRatio >= 0.03 || signal.MainNetInflow1 > 0 {
		adjustment += 4
		signal.Reasons = append(signal.Reasons, "主力资金代理信号流入")
	}
	if signal.MainNetInflowRatio <= -0.03 || signal.MainNetInflow1 < 0 {
		adjustment -= 4
		signal.Risks = append(signal.Risks, "主力资金代理信号流出")
	}
	if signal.MainNetInflow5 > 0 || signal.FundFlow5 > 0 {
		adjustment += 3
		signal.Reasons = append(signal.Reasons, "5日资金流偏正")
	}
	if signal.MainNetInflow5 < 0 || signal.FundFlow5 < 0 {
		adjustment -= 3
		signal.Risks = append(signal.Risks, "5日资金流偏负")
	}
	if signal.MainNetInflow20 > 0 || signal.FundFlow20 > 0 {
		adjustment += 2
	}
	if signal.MainNetInflow20 < 0 || signal.FundFlow20 < 0 {
		adjustment -= 2
	}

	if signal.SuperLargeNet1 > 0 {
		adjustment += 2
		signal.Reasons = append(signal.Reasons, "超大单资金代理信号流入")
	} else if signal.SuperLargeNet1 < 0 {
		adjustment -= 2
		signal.Risks = append(signal.Risks, "超大单资金代理信号流出")
	}
	if signal.LargeNet1 > 0 {
		adjustment += 1
	} else if signal.LargeNet1 < 0 {
		adjustment -= 1
	}
	if signal.MainNetInflow1 < 0 && signal.RetailNetInflow1 > 0 {
		adjustment -= 3
		signal.Risks = append(signal.Risks, "主力流出但散户代理流入，存在追高接盘风险")
	}

	if f.Open > 0 && f.Close > f.Open && (signal.MainNetInflow1 < 0 || signal.MainNetInflow5 < 0) {
		adjustment -= 2
		signal.Risks = append(signal.Risks, "股价上涨但主力资金代理信号流出，注意冲高回落")
	}
	if f.Open > 0 && f.Close < f.Open && (signal.MainNetInflow1 > 0 || signal.MainNetInflow5 > 0) {
		adjustment += 2
		signal.Reasons = append(signal.Reasons, "股价回落但主力资金代理信号流入，存在承接")
	}

	if (signal.MainNetInflow1 > 0 || signal.MainNetInflow5 > 0) && signal.SectorNetInflow > 0 {
		adjustment += 2
		signal.Reasons = append(signal.Reasons, "个股资金流入且行业板块资金偏强")
	}
	if (signal.MainNetInflow1 > 0 || signal.MainNetInflow5 > 0) && signal.SectorNetInflow < 0 {
		adjustment -= 2
		signal.Risks = append(signal.Risks, "个股资金流入但行业板块资金偏弱，降低置信度")
	}
	if (signal.MainNetInflow1 > 0 || signal.MainNetInflow5 > 0) && signal.ConceptNetInflow > 0 {
		adjustment += 1
	}
	if signal.ConceptNetInflow < 0 {
		adjustment -= 1
	}

	signal.ScoreAdjustment = adjustment
	switch {
	case adjustment >= 8:
		signal.Level = "strong_inflow"
	case adjustment > 0:
		signal.Level = "inflow"
	case adjustment <= -8:
		signal.Level = "strong_outflow"
	case adjustment < 0:
		signal.Level = "outflow"
	default:
		signal.Level = "neutral"
	}
	return signal
}

func stockCodeVariants(stockCode string) []string {
	code := strings.TrimSpace(stockCode)
	if code == "" {
		return nil
	}
	lower := strings.ToLower(code)
	upper := strings.ToUpper(code)
	noPrefix := lower
	if strings.HasPrefix(noPrefix, "sh") || strings.HasPrefix(noPrefix, "sz") || strings.HasPrefix(noPrefix, "bj") {
		noPrefix = noPrefix[2:]
	}
	variants := []string{code, lower, upper, noPrefix}
	if strings.HasPrefix(lower, "sh") {
		variants = append(variants, strings.TrimPrefix(lower, "sh")+".SH")
	}
	if strings.HasPrefix(lower, "sz") {
		variants = append(variants, strings.TrimPrefix(lower, "sz")+".SZ")
	}
	if strings.HasPrefix(lower, "bj") {
		variants = append(variants, strings.TrimPrefix(lower, "bj")+".BJ")
	}
	return uniqueStrings(variants)
}

func firstNonZero(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func amountSignal(value float64) string {
	switch {
	case value > 0:
		return "inflow"
	case value < 0:
		return "outflow"
	default:
		return "neutral"
	}
}

func latestSectorNet(sectorType string, date string) (float64, string) {
	var latest string
	query := db.Dao.Model(&models.SectorFlowDaily{}).Where("sector_type = ?", sectorType)
	if date != "" {
		query = query.Where("trade_date <= ?", date)
	}
	query.Select("MAX(trade_date)").Scan(&latest)
	if latest == "" {
		return 0, "unknown"
	}
	var rows []models.SectorFlowDaily
	db.Dao.Where("sector_type = ? AND trade_date = ?", sectorType, latest).Order("rank asc").Limit(50).Find(&rows)
	sum := 0.0
	for _, row := range rows {
		sum += row.NetInflow
	}
	switch {
	case sum > 0:
		return sum, "inflow"
	case sum < 0:
		return sum, "outflow"
	default:
		return 0, "neutral"
	}
}
