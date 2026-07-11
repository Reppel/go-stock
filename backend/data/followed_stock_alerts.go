package data

import (
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"math"
	"strings"
	"time"
)

const (
	AlarmPriceModeDisabled = "disabled"
	AlarmPriceModeFixed    = "fixed"
)

const alarmRebaseTolerance = 0.005

type FollowedStockAlertNormalizationResult struct {
	Initialized     int `json:"initialized"`
	DisabledLegacy  int `json:"disabledLegacy"`
	Rebased         int `json:"rebased"`
	SkippedNoBasis  int `json:"skippedNoBasis"`
	SkippedBadRatio int `json:"skippedBadRatio"`
}

// NormalizeFollowedStockAlertMetadata migrates the old current-price-plus-one
// default into an explicitly disabled fixed-price alert. User-entered fixed
// prices are retained and anchored to a qfq close for future corporate actions.
func NormalizeFollowedStockAlertMetadata() (*FollowedStockAlertNormalizationResult, error) {
	result := &FollowedStockAlertNormalizationResult{}
	if db.Dao == nil {
		return result, nil
	}

	var stocks []FollowedStock
	if err := db.Dao.Where("COALESCE(is_del, 0) = ?", 0).Find(&stocks).Error; err != nil {
		return nil, err
	}
	for _, stock := range stocks {
		if strings.TrimSpace(stock.AlarmPriceMode) != "" {
			continue
		}

		basisDate, basisPrice := latestFollowedStockAlarmBasis(stock.StockCode, stock.Time.Format("2006-01-02"))
		mode := AlarmPriceModeDisabled
		alarmPrice := stock.AlarmPrice
		if alarmPrice > 0 && !isLegacyDefaultAlarmPrice(alarmPrice, basisPrice) {
			mode = AlarmPriceModeFixed
		} else if alarmPrice > 0 {
			alarmPrice = 0
			result.DisabledLegacy++
		}

		updates := map[string]any{
			"alarm_price":       alarmPrice,
			"alarm_price_mode":  mode,
			"alarm_basis_date":  basisDate,
			"alarm_basis_price": basisPrice,
		}
		if err := db.Dao.Model(&FollowedStock{}).
			Where("stock_code = ?", stock.StockCode).
			Updates(updates).Error; err != nil {
			return nil, err
		}
		result.Initialized++
	}
	return result, nil
}

// RebaseFollowedStockPriceAlerts compares the qfq close on the alert's anchor
// date with its last stored value. A split or ex-right event rewrites that
// historical qfq close, yielding an idempotent adjustment factor for the fixed
// alert without interpreting ordinary market moves as corporate actions.
func RebaseFollowedStockPriceAlerts(stockCodes []string) (*FollowedStockAlertNormalizationResult, error) {
	result := &FollowedStockAlertNormalizationResult{}
	if db.Dao == nil {
		return result, nil
	}

	query := db.Dao.Where("COALESCE(is_del, 0) = ? AND alarm_price_mode = ? AND alarm_price > 0", 0, AlarmPriceModeFixed)
	if len(stockCodes) > 0 {
		variants := make([]string, 0, len(stockCodes)*3)
		for _, code := range stockCodes {
			variants = append(variants, followedStockCodeVariants(code)...)
		}
		query = query.Where("stock_code IN ?", uniqueFollowedStockCodes(variants))
	}

	var stocks []FollowedStock
	if err := query.Find(&stocks).Error; err != nil {
		return nil, err
	}
	for _, stock := range stocks {
		if strings.TrimSpace(stock.AlarmBasisDate) == "" || stock.AlarmBasisPrice <= 0 {
			basisDate, basisPrice := latestFollowedStockAlarmBasis(stock.StockCode, "")
			if basisDate == "" || basisPrice <= 0 {
				result.SkippedNoBasis++
				continue
			}
			if err := db.Dao.Model(&FollowedStock{}).Where("stock_code = ?", stock.StockCode).Updates(map[string]any{
				"alarm_basis_date":  basisDate,
				"alarm_basis_price": basisPrice,
			}).Error; err != nil {
				return nil, err
			}
			result.Initialized++
			continue
		}

		var feature models.StockFeature
		err := db.Dao.Where("stock_code IN ? AND date = ? AND adjusted = ?",
			followedStockCodeVariants(stock.StockCode), stock.AlarmBasisDate, true).
			Order("id DESC").First(&feature).Error
		if err != nil || feature.Close <= 0 {
			result.SkippedNoBasis++
			continue
		}

		ratio, ok := validAlarmAdjustmentRatio(feature.Close, stock.AlarmBasisPrice)
		if !ok {
			result.SkippedBadRatio++
			continue
		}
		updates := map[string]any{"alarm_basis_price": feature.Close}
		if math.Abs(ratio-1) >= alarmRebaseTolerance {
			now := time.Now()
			updates["alarm_price"] = roundAlertPrice(stock.AlarmPrice * ratio)
			updates["alarm_adjusted_at"] = &now
			result.Rebased++
		}
		if err := db.Dao.Model(&FollowedStock{}).Where("stock_code = ?", stock.StockCode).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	return result, nil
}

func latestFollowedStockAlarmBasis(stockCode, onOrBefore string) (string, float64) {
	if db.Dao == nil {
		return "", 0
	}
	query := db.Dao.Where("stock_code IN ? AND adjusted = ?", followedStockCodeVariants(stockCode), true)
	if strings.TrimSpace(onOrBefore) != "" {
		query = query.Where("date <= ?", onOrBefore)
	}
	var feature models.StockFeature
	if err := query.Order("date DESC, id DESC").First(&feature).Error; err == nil && feature.Close > 0 {
		return feature.Date, feature.Close
	}

	var stock FollowedStock
	if err := db.Dao.Where("stock_code = ?", strings.ToLower(strings.TrimSpace(stockCode))).First(&stock).Error; err == nil && stock.Price > 0 {
		return time.Now().Format("2006-01-02"), stock.Price
	}
	return "", 0
}

func isLegacyDefaultAlarmPrice(alarmPrice, referencePrice float64) bool {
	if alarmPrice <= 0 || referencePrice <= 0 {
		return false
	}
	// The legacy value was written from the quote at follow time. Keep the
	// migration deliberately narrow so a nearby user-entered target survives.
	tolerance := math.Max(0.02, math.Min(math.Abs(referencePrice)*0.001, 0.5))
	return math.Abs(alarmPrice-(referencePrice+1)) <= tolerance
}

func validAlarmAdjustmentRatio(currentBasis, storedBasis float64) (float64, bool) {
	if currentBasis <= 0 || storedBasis <= 0 {
		return 0, false
	}
	ratio := currentBasis / storedBasis
	return ratio, ratio >= 0.1 && ratio <= 10 && !math.IsNaN(ratio) && !math.IsInf(ratio, 0)
}

func roundAlertPrice(price float64) float64 {
	return math.Round(price*10000) / 10000
}

func followedStockCodeVariants(code string) []string {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return nil
	}
	result := []string{code}
	if len(code) == 8 {
		base := code[2:]
		switch code[:2] {
		case "sh", "sz", "bj":
			result = append(result, base, fmt.Sprintf("%s.%s", base, strings.ToUpper(code[:2])))
		}
	}
	if strings.Contains(code, ".") {
		parts := strings.Split(code, ".")
		if len(parts) == 2 && len(parts[0]) == 6 {
			result = append(result, strings.ToLower(parts[1])+parts[0], parts[0])
		}
	}
	return uniqueFollowedStockCodes(result)
}

func uniqueFollowedStockCodes(codes []string) []string {
	seen := make(map[string]struct{}, len(codes))
	result := make([]string, 0, len(codes))
	for _, code := range codes {
		code = strings.ToLower(strings.TrimSpace(code))
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		result = append(result, code)
	}
	return result
}
