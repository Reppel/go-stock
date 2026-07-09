package backtest

import (
	"go-stock/backend/data"
	"math"
	"strconv"
)

// KLineFeatures 从 K 线计算特征
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

// ToFeatureModel 转换为数据库模型
type ToFeatureModel struct{}

// CalculateFeaturesFromKLines 从 K 线数据计算特征
func CalculateFeaturesFromKLines(klines []data.KLineData) *KLineFeatures {
	if len(klines) == 0 {
		return &KLineFeatures{}
	}

	closes := make([]float64, 0, len(klines))
	volumes := make([]float64, 0, len(klines))
	var highs, lows []float64

	for _, k := range klines {
		c, _ := strconv.ParseFloat(k.Close, 64)
		o, _ := strconv.ParseFloat(k.Open, 64)
		h, _ := strconv.ParseFloat(k.High, 64)
		l, _ := strconv.ParseFloat(k.Low, 64)
		v, _ := strconv.ParseFloat(k.Volume, 64)

		closes = append(closes, c)
		volumes = append(volumes, v)
		highs = append(highs, math.Max(o, math.Max(h, l)))
		lows = append(lows, math.Min(o, math.Min(h, l)))
	}

	// 收盘价倒序为最新在前
	reversedCloses := reverse(closes)
	reversedVolumes := reverse(volumes)
	reversedHighs := reverse(highs)
	reversedLows := reverse(lows)

	return &KLineFeatures{
		MA5:          sma(reversedCloses, 5),
		MA10:         sma(reversedCloses, 10),
		MA20:         sma(reversedCloses, 20),
		MA60:         sma(reversedCloses, 60),
		MACD:         macd(reversedCloses),
		RSI6:         rsi(reversedCloses, 6),
		RSI12:        rsi(reversedCloses, 12),
		KDJ_K:        kdjK(reversedCloses, reversedHighs, reversedLows),
		BOLLUpper:    bollUpper(reversedCloses, 20),
		BOLLMid:      sma(reversedCloses, 20),
		BOLLLower:    bollLower(reversedCloses, 20),
		VolumeRatio:  volumeRatio(reversedVolumes),
		ATR:          atr(reversedHighs, reversedLows, reversedCloses, 14),
		ChangeRate5:  changeRate(reversedCloses, 5),
		ChangeRate20: changeRate(reversedCloses, 20),
	}
}

// reverse 反转切片
func reverse(arr []float64) []float64 {
	result := make([]float64, len(arr))
	for i, v := range arr {
		result[len(arr)-1-i] = v
	}
	return result
}

// sma 简单移动平均
func sma(values []float64, period int) float64 {
	if len(values) < period {
		return 0
	}
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += values[i]
	}
	return sum / float64(period)
}

// rsi 计算 RSI
func rsi(values []float64, period int) float64 {
	if len(values) <= period {
		return 0
	}
	var gains, losses float64
	for i := 0; i < period; i++ {
		diff := values[i] - values[i+1]
		if diff > 0 {
			gains += diff
		} else {
			losses += -diff
		}
	}
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

// macd 计算 MACD 柱状图
func macd(values []float64) float64 {
	if len(values) < 26 {
		return 0
	}
	ema12 := ema(values, 12)
	ema26 := ema(values, 26)
	return ema12 - ema26
}

// ema 计算指数移动平均
func ema(values []float64, period int) float64 {
	if len(values) < period {
		return 0
	}
	multiplier := 2.0 / (float64(period) + 1.0)
	emaValue := values[period-1]
	for i := period - 2; i >= 0; i-- {
		emaValue = values[i]*multiplier + emaValue*(1-multiplier)
	}
	return emaValue
}

// kdjK 计算 KDJ 的 K 值
func kdjK(closes, highs, lows []float64) float64 {
	period := 9
	if len(closes) < period || len(highs) < period || len(lows) < period {
		return 50
	}
	lowMin := lowest(lows[:period])
	highMax := highest(highs[:period])
	currentClose := closes[0]
	if highMax == lowMin {
		return 50
	}
	rsv := (currentClose - lowMin) / (highMax - lowMin) * 100
	return 2.0/3.0*50 + 1.0/3.0*rsv
}

// bollUpper 计算布林上轨
func bollUpper(values []float64, period int) float64 {
	if len(values) < period {
		return 0
	}
	mean := sma(values, period)
	std := stdDev(values[:period])
	return mean + 2*std
}

// bollLower 计算布林下轨
func bollLower(values []float64, period int) float64 {
	if len(values) < period {
		return 0
	}
	mean := sma(values, period)
	std := stdDev(values[:period])
	return mean - 2*std
}

// stdDev 标准差
func stdDev(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))
	var sum float64
	for _, v := range values {
		sum += math.Pow(v-mean, 2)
	}
	return math.Sqrt(sum / float64(len(values)))
}

// volumeRatio 量比
func volumeRatio(volumes []float64) float64 {
	if len(volumes) < 6 {
		return 1.0
	}
	current := volumes[0]
	avg := 0.0
	for i := 1; i < 6; i++ {
		avg += volumes[i]
	}
	avg /= 5
	if avg == 0 {
		return 1.0
	}
	return current / avg
}

// atr 真实波幅
func atr(highs, lows, closes []float64, period int) float64 {
	if len(highs) < period+1 || len(lows) < period+1 || len(closes) < period+1 {
		return 0
	}
	var sum float64
	for i := 0; i < period; i++ {
		tr1 := highs[i] - lows[i]
		tr2 := math.Abs(highs[i] - closes[i+1])
		tr3 := math.Abs(lows[i] - closes[i+1])
		tr := math.Max(tr1, math.Max(tr2, tr3))
		sum += tr
	}
	return sum / float64(period)
}

// changeRate N 日涨跌幅
func changeRate(values []float64, period int) float64 {
	if len(values) <= period {
		return 0
	}
	current := values[0]
	past := values[period]
	if past == 0 {
		return 0
	}
	return (current - past) / past
}

// lowest 最低值
func lowest(values []float64) float64 {
	min := values[0]
	for _, v := range values {
		if v < min {
			min = v
		}
	}
	return min
}

// highest 最高值
func highest(values []float64) float64 {
	max := values[0]
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	return max
}
