package backtest

import (
	"go-stock/backend/models"
	"testing"
	"time"
)

func TestMergeStockMoneyFlowPreservesIndependentSources(t *testing.T) {
	eastmoney := models.StockMoneyFlowDaily{
		ID: 7, StockCode: "sh515880", TradeDate: "2026-07-13",
		MainNetInflow1: 10, MainNetInflow5: 30, MainNetInflow20: 80,
		MainNetInflowRatio: 0.03, SuperLargeNet1: 4, Source: "eastmoney",
		DataAsOf: time.Date(2026, 7, 13, 15, 20, 0, 0, time.Local),
	}
	tdx := models.StockMoneyFlowDaily{
		StockCode: "sh515880", TradeDate: "2026-07-13",
		MainNetInflow1: 11, MainNetInflow5: 31,
		MacMainNetInflow1: 11, MacMainNetInflow5: 31, MacRetailNetIn1: -2,
		Source: "tdx_mac", DataAsOf: eastmoney.DataAsOf.Add(time.Minute),
	}

	merged := mergeStockMoneyFlow(eastmoney, tdx)
	if merged.ID != eastmoney.ID || merged.MainNetInflow20 != 80 || merged.MainNetInflowRatio != 0.03 || merged.SuperLargeNet1 != 4 {
		t.Fatalf("tdx merge overwrote eastmoney fields: %#v", merged)
	}
	if merged.MacMainNetInflow1 != 11 || merged.MacMainNetInflow5 != 31 || merged.MacRetailNetIn1 != -2 {
		t.Fatalf("tdx fields were not merged: %#v", merged)
	}
	if merged.Source != "eastmoney+tdx_mac" {
		t.Fatalf("unexpected merged source: %s", merged.Source)
	}

	reversed := mergeStockMoneyFlow(tdx, eastmoney)
	if reversed.MainNetInflow20 != 80 || reversed.MacMainNetInflow5 != 31 || reversed.Source != "eastmoney+tdx_mac" {
		t.Fatalf("eastmoney merge did not preserve tdx fields: %#v", reversed)
	}
}
