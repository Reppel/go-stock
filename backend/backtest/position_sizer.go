package backtest

import "math"

type PositionSizer struct {
	boardLot int64
}

func NewPositionSizer() *PositionSizer {
	return &PositionSizer{boardLot: 100}
}

func (s *PositionSizer) Size(
	action string,
	quantityPercent float64,
	currentPrice float64,
	holdingVolume int64,
	holdingMarketValue float64,
) PositionSizing {
	return s.SizeWithAccount(action, quantityPercent, currentPrice, holdingVolume, holdingMarketValue, DefaultAccountConstraint())
}

func DefaultAccountConstraint() AccountConstraint {
	return AccountConstraint{
		CashConfigured:           false,
		MaxSinglePositionPercent: 0.20,
		MinTradeUnit:             100,
	}
}

func (s *PositionSizer) SizeWithAccount(
	action string,
	quantityPercent float64,
	currentPrice float64,
	holdingVolume int64,
	holdingMarketValue float64,
	account AccountConstraint,
) PositionSizing {
	if account.MinTradeUnit <= 0 {
		account.MinTradeUnit = s.boardLot
	}
	if account.MaxSinglePositionPercent <= 0 {
		account.MaxSinglePositionPercent = 0.20
	}
	if currentPrice <= 0 {
		return PositionSizing{Action: action, QuantityPercent: quantityPercent, MinTradeUnit: account.MinTradeUnit, CashConfigured: account.CashConfigured}
	}
	if quantityPercent < 0 {
		quantityPercent = 0
	}
	if quantityPercent > 100 {
		quantityPercent = 100
	}

	result := PositionSizing{
		Action:          action,
		QuantityPercent: quantityPercent,
		ReferencePrice:  currentPrice,
		MinTradeUnit:    account.MinTradeUnit,
		CashConfigured:  account.CashConfigured,
		AvailableCash:   account.AvailableCash,
	}
	switch action {
	case "SELL", "REDUCE":
		qty := int64(math.Floor(float64(holdingVolume) * quantityPercent / 100.0))
		qty = s.roundDownLot(qty)
		if action == "SELL" && qty == 0 && holdingVolume > 0 {
			qty = holdingVolume
		}
		result.SuggestedQuantity = qty
		result.SuggestedAmount = float64(qty) * currentPrice
		result.EstimatedRelease = result.SuggestedAmount
	case "BUY", "ADD":
		baseAmount := holdingMarketValue
		if !account.CashConfigured && baseAmount <= 0 {
			result.AccountWarning = "未配置可用资金和总资产，买入/加仓仅给出目标比例，不生成可执行数量"
			result.SuggestedQuantity = 0
			result.SuggestedAmount = 0
			return result
		}
		if baseAmount <= 0 {
			baseAmount = currentPrice * float64(account.MinTradeUnit)
		}
		amount := baseAmount * quantityPercent / 100.0
		if amount < currentPrice*float64(account.MinTradeUnit) {
			amount = currentPrice * float64(account.MinTradeUnit)
		}
		if account.CashConfigured {
			if account.AvailableCash > 0 && amount > account.AvailableCash {
				amount = account.AvailableCash
			}
			if account.TotalAsset > 0 {
				maxAmount := account.TotalAsset * account.MaxSinglePositionPercent
				result.MaxPositionAmount = maxAmount
				if maxAmount > 0 && amount+holdingMarketValue > maxAmount {
					amount = maxAmount - holdingMarketValue
				}
			}
		} else {
			result.AccountWarning = "未配置可用资金，买入/加仓仅给比例和最小一手试探金额"
		}
		qty := s.roundDownLot(int64(math.Floor(amount / currentPrice)))
		result.SuggestedQuantity = qty
		result.SuggestedAmount = float64(qty) * currentPrice
	default:
		result.SuggestedQuantity = 0
		result.SuggestedAmount = 0
	}
	return result
}

func (s *PositionSizer) roundDownLot(qty int64) int64 {
	if s.boardLot <= 0 || qty <= 0 {
		return qty
	}
	return qty / s.boardLot * s.boardLot
}
