package backtest

import (
	"go-stock/backend/data"
	"math"
	"strconv"
)

// KLineFeatures contains point-in-time daily features calculated from adjusted bars.
type KLineFeatures struct {
	MA5          float64
	MA10         float64
	MA20         float64
	MA60         float64
	MACD         float64
	RSI6         float64
	RSI12        float64
	KDJ_K        float64
	BOLLUpper    float64
	BOLLMid      float64
	BOLLLower    float64
	VolumeRatio  float64
	ATR          float64
	FundFlow5    float64
	FundFlow20   float64
	ChangeRate5  float64
	ChangeRate20 float64
}

// CalculateFeaturesFromKLines expects bars ordered from oldest to newest.
func CalculateFeaturesFromKLines(klines []data.KLineData) *KLineFeatures {
	if len(klines) == 0 {
		return &KLineFeatures{}
	}

	closes := make([]float64, 0, len(klines))
	volumes := make([]float64, 0, len(klines))
	highs := make([]float64, 0, len(klines))
	lows := make([]float64, 0, len(klines))
	for _, k := range klines {
		closePrice, _ := strconv.ParseFloat(k.Close, 64)
		openPrice, _ := strconv.ParseFloat(k.Open, 64)
		highPrice, _ := strconv.ParseFloat(k.High, 64)
		lowPrice, _ := strconv.ParseFloat(k.Low, 64)
		volume, _ := strconv.ParseFloat(k.Volume, 64)
		closes = append(closes, closePrice)
		volumes = append(volumes, volume)
		highs = append(highs, math.Max(openPrice, math.Max(highPrice, lowPrice)))
		lows = append(lows, math.Min(openPrice, math.Min(highPrice, lowPrice)))
	}

	bollMid, bollUpper, bollLower := bollBands(closes, 20)
	return &KLineFeatures{
		MA5:          sma(closes, 5),
		MA10:         sma(closes, 10),
		MA20:         sma(closes, 20),
		MA60:         sma(closes, 60),
		MACD:         macd(closes),
		RSI6:         rsi(closes, 6),
		RSI12:        rsi(closes, 12),
		KDJ_K:        kdjK(closes, highs, lows),
		BOLLUpper:    bollUpper,
		BOLLMid:      bollMid,
		BOLLLower:    bollLower,
		VolumeRatio:  volumeRatio(volumes),
		ATR:          atr(highs, lows, closes, 14),
		ChangeRate5:  changeRate(closes, 5),
		ChangeRate20: changeRate(closes, 20),
	}
}

// sma returns the moving average ending at the latest bar.
func sma(values []float64, period int) float64 {
	if period <= 0 || len(values) < period {
		return 0
	}
	start := len(values) - period
	sum := 0.0
	for _, value := range values[start:] {
		sum += value
	}
	return sum / float64(period)
}

// rsi implements Wilder's smoothed RSI over the complete available history.
func rsi(values []float64, period int) float64 {
	if period <= 0 || len(values) <= period {
		return 0
	}
	gain := 0.0
	loss := 0.0
	for i := 1; i <= period; i++ {
		delta := values[i] - values[i-1]
		if delta >= 0 {
			gain += delta
		} else {
			loss -= delta
		}
	}
	avgGain := gain / float64(period)
	avgLoss := loss / float64(period)
	for i := period + 1; i < len(values); i++ {
		delta := values[i] - values[i-1]
		currentGain := math.Max(delta, 0)
		currentLoss := math.Max(-delta, 0)
		avgGain = (avgGain*float64(period-1) + currentGain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + currentLoss) / float64(period)
	}
	if avgLoss == 0 {
		if avgGain == 0 {
			return 50
		}
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - 100/(1+rs)
}

// macd returns the standard MACD histogram: 2 * (DIF - DEA).
func macd(values []float64) float64 {
	if len(values) < 26 {
		return 0
	}
	ema12 := values[0]
	ema26 := values[0]
	dea := 0.0
	alpha12 := 2.0 / 13.0
	alpha26 := 2.0 / 27.0
	alphaSignal := 2.0 / 10.0
	for i := 1; i < len(values); i++ {
		ema12 = values[i]*alpha12 + ema12*(1-alpha12)
		ema26 = values[i]*alpha26 + ema26*(1-alpha26)
		dif := ema12 - ema26
		dea = dif*alphaSignal + dea*(1-alphaSignal)
	}
	return 2 * ((ema12 - ema26) - dea)
}

// kdjK calculates the recursively smoothed K value rather than resetting K daily.
func kdjK(closes, highs, lows []float64) float64 {
	const period = 9
	if len(closes) < period || len(highs) != len(closes) || len(lows) != len(closes) {
		return 50
	}
	k := 50.0
	for i := period - 1; i < len(closes); i++ {
		lowMin := lowest(lows[i-period+1 : i+1])
		highMax := highest(highs[i-period+1 : i+1])
		rsv := 50.0
		if highMax > lowMin {
			rsv = (closes[i] - lowMin) / (highMax - lowMin) * 100
		}
		k = 2.0/3.0*k + 1.0/3.0*rsv
	}
	return k
}

func bollBands(values []float64, period int) (mid, upper, lower float64) {
	if period <= 0 || len(values) < period {
		return 0, 0, 0
	}
	window := values[len(values)-period:]
	mid = sma(values, period)
	std := stdDev(window)
	return mid, mid + 2*std, mid - 2*std
}

func stdDev(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	mean := 0.0
	for _, value := range values {
		mean += value
	}
	mean /= float64(len(values))
	variance := 0.0
	for _, value := range values {
		variance += math.Pow(value-mean, 2)
	}
	return math.Sqrt(variance / float64(len(values)))
}

func volumeRatio(volumes []float64) float64 {
	if len(volumes) < 6 {
		return 1
	}
	current := volumes[len(volumes)-1]
	start := len(volumes) - 6
	average := 0.0
	for _, value := range volumes[start : len(volumes)-1] {
		average += value
	}
	average /= 5
	if average <= 0 {
		return 1
	}
	return current / average
}

func atr(highs, lows, closes []float64, period int) float64 {
	if period <= 0 || len(closes) < period+1 || len(highs) != len(closes) || len(lows) != len(closes) {
		return 0
	}
	start := len(closes) - period
	sum := 0.0
	for i := start; i < len(closes); i++ {
		previousClose := closes[i-1]
		trueRange := math.Max(highs[i]-lows[i], math.Max(math.Abs(highs[i]-previousClose), math.Abs(lows[i]-previousClose)))
		sum += trueRange
	}
	return sum / float64(period)
}

func changeRate(values []float64, period int) float64 {
	if period <= 0 || len(values) <= period {
		return 0
	}
	current := values[len(values)-1]
	past := values[len(values)-1-period]
	if past == 0 {
		return 0
	}
	return (current - past) / past
}

func lowest(values []float64) float64 {
	minimum := values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
	}
	return minimum
}

func highest(values []float64) float64 {
	maximum := values[0]
	for _, value := range values[1:] {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}
