package data

import "testing"

func TestLegacyDefaultAlarmPriceDetection(t *testing.T) {
	if !isLegacyDefaultAlarmPrice(1.757, 0.76) {
		t.Fatal("legacy current-price-plus-one alarm should be detected")
	}
	if isLegacyDefaultAlarmPrice(0.88, 0.76) {
		t.Fatal("a plausible user fixed-price alert must be preserved")
	}
	if isLegacyDefaultAlarmPrice(105, 100) {
		t.Fatal("a nearby user target must not be classified as a price-plus-one default")
	}
}

func TestAlarmAdjustmentRatioForSplit(t *testing.T) {
	ratio, ok := validAlarmAdjustmentRatio(0.7885, 1.5769)
	if !ok || ratio < 0.499 || ratio > 0.501 {
		t.Fatalf("unexpected split adjustment ratio: ratio=%v ok=%v", ratio, ok)
	}
	if got := roundAlertPrice(1.757 * ratio); got < 0.878 || got > 0.879 {
		t.Fatalf("unexpected rebased alert price: %v", got)
	}
}
