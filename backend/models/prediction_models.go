package models

import "time"

// PredictionSession 用户一次完整的预测请求
type PredictionSession struct {
	ID         uint      `json:"id" gorm:"primarykey" md:"-"`
	Scene      string    `json:"scene" gorm:"size:50;index" md:"预测场景"` // 短线爆发/波段反弹/趋势持有
	StockScope string    `json:"stockScope" gorm:"size:100" md:"股票池"`  // 全市场/自选股/某分组
	StartDate  string    `json:"startDate" gorm:"size:10" md:"回测开始日期"` // 回测开始
	EndDate    string    `json:"endDate" gorm:"size:10" md:"回测结束日期"`   // 回测结束
	Status     string    `json:"status" gorm:"size:20" md:"状态"`        // running/done/failed
	CreatedAt  time.Time `json:"createdAt" gorm:"autoCreateTime" md:"-"`
}

func (PredictionSession) TableName() string {
	return "prediction_session"
}

// PredictionHypothesis AI 生成的预测假设
type PredictionHypothesis struct {
	ID           uint      `json:"id" gorm:"primarykey" md:"-"`
	SessionID    uint      `json:"sessionId" gorm:"index" md:"会话ID"`
	Name         string    `json:"name" gorm:"size:100" md:"假设名称"`
	Description  string    `json:"description" gorm:"size:500" md:"假设描述"`
	Scene        string    `json:"scene" gorm:"size:50;index" md:"场景"`
	RuleJSON     string    `json:"ruleJson" gorm:"type:text" md:"规则JSON"` // 可执行规则 JSON
	Params       string    `json:"params" gorm:"type:text" md:"参数JSON"`   // 参数 JSON
	TimeHorizon  int       `json:"timeHorizon" md:"持有周期"`                 // 持有周期（交易日）
	TargetReturn float64   `json:"targetReturn" md:"目标收益率"`               // 目标收益率
	WinRate      float64   `json:"winRate" md:"历史胜率"`                     // 历史胜率
	AvgReturn    float64   `json:"avgReturn" md:"平均收益"`                   // 平均收益
	MaxDrawdown  float64   `json:"maxDrawdown" md:"最大回撤"`                 // 最大回撤
	TradeCount   int       `json:"tradeCount" md:"历史交易次数"`                // 历史交易次数
	ValidReturn  float64   `json:"validReturn" md:"实际验证累计收益"`             // 实际验证累计收益
	ValidCount   int       `json:"validCount" md:"实际验证次数"`                // 实际验证次数
	Status       string    `json:"status" gorm:"size:20" md:"状态"`         // draft/active/expired/disabled
	CreatedAt    time.Time `json:"createdAt" gorm:"autoCreateTime" md:"-"`
}

func (PredictionHypothesis) TableName() string {
	return "prediction_hypothesis"
}

// PredictionSignal 预测信号触发记录
type PredictionSignal struct {
	ID           uint      `json:"id" gorm:"primarykey" md:"-"`
	HypothesisID uint      `json:"hypothesisId" gorm:"index" md:"假设ID"`
	StockCode    string    `json:"stockCode" gorm:"size:20;index" md:"股票代码"`
	StockName    string    `json:"stockName" gorm:"size:50" md:"股票名称"`
	SignalDate   string    `json:"signalDate" gorm:"size:10;index" md:"信号日期"` // 信号日
	EntryPrice   float64   `json:"entryPrice" md:"买入价"`
	TargetDate   string    `json:"targetDate" gorm:"size:10" md:"目标验证日"` // 目标验证日
	TargetReturn float64   `json:"targetReturn" md:"目标收益率"`
	ActualReturn float64   `json:"actualReturn" md:"实际收益"`
	MaxReturn    float64   `json:"maxReturn" md:"期间最大涨幅"`
	MaxDrawdown  float64   `json:"maxDrawdown" md:"期间最大回撤"`
	ValidatedAt  time.Time `json:"validatedAt" md:"验证时间"`
	Hit          bool      `json:"hit" md:"是否命中"`
	Status       string    `json:"status" gorm:"size:20;index" md:"状态"` // pending/validated
}

func (PredictionSignal) TableName() string {
	return "prediction_signal"
}

// PredictionHypothesisDaily 预测假设每日表现（净值曲线）
type PredictionHypothesisDaily struct {
	ID           uint    `json:"id" gorm:"primarykey" md:"-"`
	HypothesisID uint    `json:"hypothesisId" gorm:"index" md:"假设ID"`
	Date         string  `json:"date" gorm:"size:10;index" md:"日期"`
	Nav          float64 `json:"nav" md:"净值"`          // 净值
	Drawdown     float64 `json:"drawdown" md:"回撤"`     // 当日回撤
	TradeCount   int     `json:"tradeCount" md:"交易次数"` // 当日交易次数
}

func (PredictionHypothesisDaily) TableName() string {
	return "prediction_hypothesis_daily"
}

// StockFeature 预计算的股票特征，每天收盘后更新
type StockFeature struct {
	ID           uint    `json:"id" gorm:"primarykey" md:"-"`
	StockCode    string  `json:"stockCode" gorm:"size:20;index:idx_stock_feature_date_code" md:"股票代码"`
	Date         string  `json:"date" gorm:"size:10;index:idx_stock_feature_date_code" md:"日期"`
	Close        float64 `json:"close" md:"收盘价"`
	Open         float64 `json:"open" md:"开盘价"`
	High         float64 `json:"high" md:"最高价"`
	Low          float64 `json:"low" md:"最低价"`
	Volume       float64 `json:"volume" md:"成交量"`
	Turnover     float64 `json:"turnover" md:"成交额"`
	MA5          float64 `json:"ma5" md:"MA5"`
	MA10         float64 `json:"ma10" md:"MA10"`
	MA20         float64 `json:"ma20" md:"MA20"`
	MA60         float64 `json:"ma60" md:"MA60"`
	MACD         float64 `json:"macd" md:"MACD"`
	RSI6         float64 `json:"rsi6" md:"RSI6"`
	RSI12        float64 `json:"rsi12" md:"RSI12"`
	KDJ_K        float64 `json:"kdjK" md:"KDJ_K"`
	BOLLUpper    float64 `json:"bollUpper" md:"布林上轨"`
	BOLLMid      float64 `json:"bollMid" md:"布林中轨"`
	BOLLLower    float64 `json:"bollLower" md:"布林下轨"`
	VolumeRatio  float64 `json:"volumeRatio" md:"量比"`
	ATR          float64 `json:"atr" md:"ATR"`
	FundFlow5    float64 `json:"fundFlow5" md:"5日资金净流入"`
	FundFlow20   float64 `json:"fundFlow20" md:"20日资金净流入"`
	ChangeRate5  float64 `json:"changeRate5" md:"5日涨跌幅"`
	ChangeRate20 float64 `json:"changeRate20" md:"20日涨跌幅"`
}

func (StockFeature) TableName() string {
	return "stock_feature"
}
