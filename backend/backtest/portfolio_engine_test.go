package backtest

import "testing"

func TestTradeMaxDrawdown(t *testing.T) {
	if got := tradeMaxDrawdown(100, 50); got != 0.5 {
		t.Fatalf("expected 0.5 drawdown, got %v", got)
	}
	if got := tradeMaxDrawdown(100, 150); got != 0 {
		t.Fatalf("expected no adverse drawdown, got %v", got)
	}
	if got := tradeMaxDrawdown(100, 0); got != 0 {
		t.Fatalf("expected invalid low price to be ignored, got %v", got)
	}
}
