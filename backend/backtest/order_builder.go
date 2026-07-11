package backtest

import (
	"fmt"
	"math"
	"sort"
)

type OrderBuilder struct{}

func NewOrderBuilder() *OrderBuilder {
	return &OrderBuilder{}
}

func (b *OrderBuilder) BuildEntryOrders(
	signals []Signal,
	featureMap map[string]float64,
	cash float64,
	totalEquity float64,
	held map[string]bool,
	maxHoldings int,
	lotSize int,
	orderDate string,
) []SimOrder {
	if maxHoldings <= 0 {
		maxHoldings = 5
	}
	if totalEquity <= 0 {
		totalEquity = cash
	}
	if cash <= 0 {
		return nil
	}

	if lotSize <= 0 {
		lotSize = 100
	}
	sort.SliceStable(signals, func(i, j int) bool {
		if signals[i].Score == signals[j].Score {
			return signals[i].StockCode < signals[j].StockCode
		}
		return signals[i].Score > signals[j].Score
	})

	targetAmount := totalEquity / float64(maxHoldings)
	orders := make([]SimOrder, 0, len(signals))
	for _, signal := range signals {
		if held[signal.StockCode] {
			continue
		}
		if len(held)+len(orders) >= maxHoldings {
			break
		}
		price := featureMap[signal.StockCode]
		if price <= 0 {
			price = signal.Price
		}
		if price <= 0 {
			continue
		}
		amount := targetAmount
		if amount > cash {
			amount = cash
		}
		if amount <= 0 {
			continue
		}
		quantity := math.Floor(amount/price/float64(lotSize)) * float64(lotSize)
		if quantity <= 0 {
			continue
		}
		amount = quantity * price
		orders = append(orders, SimOrder{
			ID:         fmt.Sprintf("entry-%s-%s", signal.StockCode, orderDate),
			StockCode:  signal.StockCode,
			StockName:  signal.StockName,
			Side:       OrderSideBuy,
			SignalDate: signal.Date,
			OrderDate:  orderDate,
			PriceHint:  price,
			Quantity:   quantity,
			Amount:     amount,
			ReasonJSON: signal.ReasonJSON,
			Source:     "strategy",
		})
		cash -= amount
	}
	return orders
}

func (b *OrderBuilder) BuildExitOrder(pos PortfolioPosition, orderDate string, priceHint float64, exitReason string) SimOrder {
	return SimOrder{
		ID:         fmt.Sprintf("exit-%s-%s-%s", pos.StockCode, exitReason, orderDate),
		StockCode:  pos.StockCode,
		StockName:  pos.StockName,
		Side:       OrderSideSell,
		SignalDate: pos.SignalDate,
		OrderDate:  orderDate,
		PriceHint:  priceHint,
		Quantity:   pos.Quantity,
		Amount:     priceHint * pos.Quantity,
		ReasonJSON: pos.EntryReasonJSON,
		ExitReason: exitReason,
		Source:     "risk",
	}
}
