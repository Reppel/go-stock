package backtest

import (
	"fmt"
	"go-stock/backend/data"
	"math"
	"testing"
)

func TestCalculateFeaturesUsesChronologicalAdjustedBars(t *testing.T) {
	bars := make([]data.KLineData, 80)
	for index := range bars {
		price := 100 + float64(index)
		bars[index] = data.KLineData{
			Day: fmt.Sprintf("2026-01-%02d", index+1), Open: fmt.Sprintf("%.2f", price),
			Close: fmt.Sprintf("%.2f", price), High: fmt.Sprintf("%.2f", price+1),
			Low: fmt.Sprintf("%.2f", price-1), Volume: fmt.Sprintf("%d", 1000+index*10),
		}
	}
	features := CalculateFeaturesFromKLines(bars)
	if math.Abs(features.MA5-177) > 1e-9 {
		t.Fatalf("expected latest MA5 177, got %v", features.MA5)
	}
	expectedChange := (179.0 - 174.0) / 174.0
	if math.Abs(features.ChangeRate5-expectedChange) > 1e-9 {
		t.Fatalf("expected chronological 5-day change %v, got %v", expectedChange, features.ChangeRate5)
	}
	if features.RSI6 < 99 || features.MACD <= 0 || features.KDJ_K <= 50 || features.KDJ_K > 100 || features.ATR <= 0 {
		t.Fatalf("unexpected trending features: %+v", features)
	}
}

func TestFlatSeriesProducesNeutralMomentum(t *testing.T) {
	bars := make([]data.KLineData, 70)
	for index := range bars {
		bars[index] = data.KLineData{Open: "10", Close: "10", High: "10", Low: "10", Volume: "1000"}
	}
	features := CalculateFeaturesFromKLines(bars)
	if features.RSI6 != 50 || math.Abs(features.MACD) > 1e-12 || math.Abs(features.KDJ_K-50) > 1e-9 {
		t.Fatalf("flat series should be neutral, got %+v", features)
	}
}
