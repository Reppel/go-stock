package backtest

import "time"

func shanghaiLocation() *time.Location {
	return time.FixedZone("Asia/Shanghai", 8*60*60)
}

func shanghaiNow() time.Time {
	return time.Now().In(shanghaiLocation())
}
