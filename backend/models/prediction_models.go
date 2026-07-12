package models

import "time"

// PredictionSession 用户一次完整的预测请求
type PredictionSession struct {
	ID                    uint      `json:"id" gorm:"primarykey" md:"-"`
	CandidateSnapshotID   uint      `json:"candidateSnapshotId" gorm:"index" md:"候选快照ID"`
	UniverseSelectionMode string    `json:"universeSelectionMode" gorm:"size:40" md:"股票池选择模式"`
	SelectionAsOf         time.Time `json:"selectionAsOf" md:"股票池选择时点"`
	Scene                 string    `json:"scene" gorm:"size:50;index" md:"预测场景"` // 短线爆发/波段反弹/趋势持有
	StockScope            string    `json:"stockScope" gorm:"size:100" md:"股票池"`  // 全市场/自选股/某分组
	UniverseJSON          string    `json:"universeJson" gorm:"type:text" md:"股票池快照JSON"`
	StartDate             string    `json:"startDate" gorm:"size:10" md:"回测开始日期"` // 回测开始
	EndDate               string    `json:"endDate" gorm:"size:10" md:"回测结束日期"`   // 回测结束
	ResearchEndDate       string    `json:"researchEndDate" gorm:"size:10" md:"研究窗口截止"`
	AIConfigID            int       `json:"aiConfigId" md:"AI配置ID"`
	MarketState           string    `json:"marketState" gorm:"size:30" md:"研究期市场状态"`
	MarketReturn          float64   `json:"marketReturn" md:"研究期基准收益"`
	Status                string    `json:"status" gorm:"size:20" md:"状态"` // running/done/failed
	ErrorMsg              string    `json:"errorMsg" gorm:"size:500" md:"错误信息"`
	CreatedAt             time.Time `json:"createdAt" gorm:"autoCreateTime" md:"-"`
}

func (PredictionSession) TableName() string {
	return "prediction_session"
}

// PredictionHypothesis AI 生成的预测假设
type PredictionHypothesis struct {
	ID                     uint       `json:"id" gorm:"primarykey" md:"-"`
	SessionID              uint       `json:"sessionId" gorm:"index" md:"会话ID"`
	UniverseSelectionMode  string     `json:"universeSelectionMode" gorm:"size:40;index" md:"股票池选择模式"`
	BacktestDiagnosticOnly bool       `json:"backtestDiagnosticOnly" gorm:"default:false" md:"历史回测仅诊断"`
	Name                   string     `json:"name" gorm:"size:100" md:"假设名称"`
	Description            string     `json:"description" gorm:"size:500" md:"假设描述"`
	Scene                  string     `json:"scene" gorm:"size:50;index" md:"场景"`
	RuleJSON               string     `json:"ruleJson" gorm:"type:text" md:"规则JSON"` // 可执行规则 JSON
	Params                 string     `json:"params" gorm:"type:text" md:"参数JSON"`   // 参数 JSON
	TimeHorizon            int        `json:"timeHorizon" md:"持有周期"`                 // 持有周期（交易日）
	TargetReturn           float64    `json:"targetReturn" md:"目标收益率"`               // 目标收益率
	WinRate                float64    `json:"winRate" md:"历史胜率"`                     // 历史胜率
	AvgReturn              float64    `json:"avgReturn" md:"平均收益"`                   // 平均收益
	MaxDrawdown            float64    `json:"maxDrawdown" md:"最大回撤"`                 // 最大回撤
	TradeCount             int        `json:"tradeCount" md:"历史交易次数"`                // 历史交易次数
	TotalReturn            float64    `json:"totalReturn" md:"总收益"`
	MedianReturn           float64    `json:"medianReturn" md:"中位收益"`
	ProfitLossRatio        float64    `json:"profitLossRatio" md:"盈亏比"`
	ProfitLossRatioStatus  string     `json:"profitLossRatioStatus" gorm:"size:30" md:"盈亏比状态"`
	OutSampleAvgReturn     float64    `json:"outSampleAvgReturn" md:"样本外平均收益"`
	OutSampleMaxDrawdown   float64    `json:"outSampleMaxDrawdown" md:"样本外最大回撤"`
	OutSampleTradeCount    int        `json:"outSampleTradeCount" md:"样本外交易次数"`
	BenchmarkAvailable     bool       `json:"benchmarkAvailable" gorm:"default:false" md:"基准数据可用"`
	BenchmarkReturn        float64    `json:"benchmarkReturn" md:"基准收益"`
	ExcessReturn           float64    `json:"excessReturn" md:"超额收益"`
	DataCoverage           float64    `json:"dataCoverage" md:"数据覆盖率"`
	NoLookaheadPassed      bool       `json:"noLookaheadPassed" gorm:"default:false" md:"未来函数检查"`
	BacktestConfigJSON     string     `json:"backtestConfigJson" gorm:"type:text" md:"回测配置"`
	VerdictJSON            string     `json:"verdictJson" gorm:"type:text" md:"量化裁决JSON"`
	GenerationSource       string     `json:"generationSource" gorm:"size:20" md:"生成来源"` // ai/template
	SchemaVersion          string     `json:"schemaVersion" gorm:"size:50" md:"规则版本"`
	RegistryVersion        string     `json:"registryVersion" gorm:"size:50" md:"指标注册表版本"`
	EngineVersion          string     `json:"engineVersion" gorm:"size:50" md:"回测引擎版本"`
	StrategyVersion        string     `json:"strategyVersion" gorm:"size:50" md:"策略版本"`
	FeatureVersion         string     `json:"featureVersion" gorm:"size:50" md:"特征版本"`
	LastVerdictStatus      string     `json:"lastVerdictStatus" gorm:"size:30;index" md:"最近裁决状态"`
	PaperTradeStartedAt    *time.Time `json:"paperTradeStartedAt" md:"模拟盘开始时间"`
	PaperTradeDays         int        `json:"paperTradeDays" md:"前向验证交易日"`
	PaperTradeCount        int        `json:"paperTradeCount" md:"前向验证已完成交易"`
	PaperNav               float64    `json:"paperNav" md:"前向验证净值"`
	PaperMaxDrawdown       float64    `json:"paperMaxDrawdown" md:"前向验证最大回撤"`
	PaperCash              float64    `json:"paperCash" md:"前向验证现金"`
	PaperMarketValue       float64    `json:"paperMarketValue" md:"前向验证持仓市值"`
	PaperPositionCount     int        `json:"paperPositionCount" md:"前向验证持仓数"`
	PaperReady             bool       `json:"paperReady" gorm:"default:false" md:"是否达到正式监控门槛"`
	PaperReadyReason       string     `json:"paperReadyReason" gorm:"size:300" md:"正式监控门槛说明"`
	PaperLastProcessedDate string     `json:"paperLastProcessedDate" gorm:"size:10" md:"前向验证最近处理交易日"`
	ReviewDueAt            *time.Time `json:"reviewDueAt" md:"复审时间"`
	ValidReturn            float64    `json:"validReturn" md:"实际验证累计收益"`     // 实际验证累计收益
	ValidCount             int        `json:"validCount" md:"实际验证次数"`        // 实际验证次数
	Status                 string     `json:"status" gorm:"size:20" md:"状态"` // draft/active/expired/disabled
	CreatedAt              time.Time  `json:"createdAt" gorm:"autoCreateTime" md:"-"`
}

func (PredictionHypothesis) TableName() string {
	return "prediction_hypothesis"
}

// PredictionDecision 单只股票的当前操作建议。
type PredictionDecision struct {
	ID                    uint      `json:"id" gorm:"primarykey" md:"-"`
	SessionID             uint      `json:"sessionId" gorm:"index" md:"会话ID"`
	StockCode             string    `json:"stockCode" gorm:"size:20;index:idx_prediction_decision_session_stock" md:"股票代码"`
	StockName             string    `json:"stockName" gorm:"size:50" md:"股票名称"`
	DecisionDate          string    `json:"decisionDate" gorm:"size:10;index" md:"决策日期"`
	Action                string    `json:"action" gorm:"size:20;index" md:"建议动作"` // BUY/ADD/HOLD/REDUCE/SELL/WATCH/AVOID
	PreviousAction        string    `json:"previousAction" gorm:"size:20" md:"上次建议动作"`
	ActionText            string    `json:"actionText" gorm:"size:30" md:"建议动作文本"`
	PositionAdvice        string    `json:"positionAdvice" gorm:"size:100" md:"仓位建议"`
	QuantityPercent       float64   `json:"quantityPercent" md:"建议仓位比例"`
	SuggestedQuantity     int64     `json:"suggestedQuantity" md:"建议数量"`
	SuggestedAmount       float64   `json:"suggestedAmount" md:"建议金额"`
	Confidence            string    `json:"confidence" gorm:"size:20" md:"置信度"` // high/medium/low
	QualityRating         string    `json:"qualityRating" gorm:"size:20" md:"回测质量"`
	RiskLevel             string    `json:"riskLevel" gorm:"size:20" md:"风险等级"`
	Score                 float64   `json:"score" md:"综合评分"`
	Probability           float64   `json:"probability" md:"经验后验胜率"`
	ProbabilityMethod     string    `json:"probabilityMethod" gorm:"size:40" md:"概率估计方法"`
	ExpectedReturn        float64   `json:"expectedReturn" md:"预期净收益"`
	CurrentPrice          float64   `json:"currentPrice" md:"当前价"`
	ReferencePrice        float64   `json:"referencePrice" md:"特征参考价"`
	CostPrice             float64   `json:"costPrice" md:"持仓成本价"`
	HoldingVolume         int64     `json:"holdingVolume" md:"持仓数量"`
	ProfitRate            float64   `json:"profitRate" md:"当前盈亏率"`
	BuyPriceMin           float64   `json:"buyPriceMin" md:"建议买入价下限"`
	BuyPriceMax           float64   `json:"buyPriceMax" md:"建议买入价上限"`
	SellPriceMin          float64   `json:"sellPriceMin" md:"建议卖出价下限"`
	SellPriceMax          float64   `json:"sellPriceMax" md:"建议卖出价上限"`
	DefensePrice          float64   `json:"defensePrice" md:"防守价"`
	StopLossPrice         float64   `json:"stopLossPrice" md:"止损价"`
	TakeProfitPrice       float64   `json:"takeProfitPrice" md:"止盈/减仓价"`
	MatchedStrategiesJSON string    `json:"matchedStrategiesJson" gorm:"type:text" md:"匹配策略JSON"`
	SizingJSON            string    `json:"sizingJson" gorm:"type:text" md:"仓位计算JSON"`
	CapitalFlowJSON       string    `json:"capitalFlowJson" gorm:"type:text" md:"资金流信号JSON"`
	AlertJSON             string    `json:"alertJson" gorm:"type:text" md:"预警JSON"`
	ReasonsJSON           string    `json:"reasonsJson" gorm:"type:text" md:"原因JSON"`
	RisksJSON             string    `json:"risksJson" gorm:"type:text" md:"风险JSON"`
	SampleWarning         bool      `json:"sampleWarning" md:"样本不足"`
	PoolSampleCount       int       `json:"poolSampleCount" md:"股票池回测样本数"`
	StockSampleCount      int       `json:"stockSampleCount" md:"单股回测样本数"`
	SampleSummaryJSON     string    `json:"sampleSummaryJson" gorm:"type:text" md:"样本摘要JSON"`
	DataStatusJSON        string    `json:"dataStatusJson" gorm:"type:text" md:"数据就绪状态JSON"`
	FeatureVersion        string    `json:"featureVersion" gorm:"size:50" md:"特征版本"`
	DataAsOf              time.Time `json:"dataAsOf" md:"数据截至"`
	Status                string    `json:"status" gorm:"size:20;index" md:"状态"` // draft/watch/resolved
	CreatedAt             time.Time `json:"createdAt" gorm:"autoCreateTime" md:"-"`
}

func (PredictionDecision) TableName() string {
	return "prediction_decision"
}

// PredictionAlertLog AI 预测工厂盘中提醒事件。
type PredictionAlertLog struct {
	ID              uint       `json:"id" gorm:"primarykey" md:"-"`
	AlertKey        string     `json:"alertKey" gorm:"size:160;index" md:"提醒键"`
	SessionID       uint       `json:"sessionId" gorm:"index" md:"会话ID"`
	DecisionID      uint       `json:"decisionId" gorm:"index" md:"决策ID"`
	StockCode       string     `json:"stockCode" gorm:"size:20;index" md:"股票代码"`
	StockName       string     `json:"stockName" gorm:"size:50" md:"股票名称"`
	AlertType       string     `json:"alertType" gorm:"size:40;index" md:"提醒类型"`
	Level           string     `json:"level" gorm:"size:20;index" md:"提醒级别"`
	Title           string     `json:"title" gorm:"size:120" md:"标题"`
	Message         string     `json:"message" gorm:"type:text" md:"内容"`
	TriggerPrice    float64    `json:"triggerPrice" md:"触发价格"`
	ThresholdPrice  float64    `json:"thresholdPrice" md:"阈值价格"`
	SuggestedAction string     `json:"suggestedAction" gorm:"size:20;index" md:"建议动作"`
	Scene           string     `json:"scene" gorm:"size:50;index" md:"投资场景"`
	MonitorMode     string     `json:"monitorMode" gorm:"size:20;index" md:"监控类型"`
	Status          string     `json:"status" gorm:"size:20;index" md:"状态"` // new/sent/read/ignored/resolved
	Channel         string     `json:"channel" gorm:"size:30" md:"渠道"`      // app/windows
	Reason          string     `json:"reason" gorm:"size:60" md:"原因"`
	TriggeredAt     time.Time  `json:"triggeredAt" gorm:"index" md:"触发时间"`
	SentAt          *time.Time `json:"sentAt" md:"发送时间"`
	ReadAt          *time.Time `json:"readAt" md:"读取时间"`
	CreatedAt       time.Time  `json:"createdAt" gorm:"autoCreateTime" md:"-"`
	UpdatedAt       time.Time  `json:"updatedAt" gorm:"autoUpdateTime" md:"-"`
}

func (PredictionAlertLog) TableName() string {
	return "prediction_alert_log"
}

// PredictionSignal 预测信号触发记录
type PredictionSignal struct {
	ID           uint      `json:"id" gorm:"primarykey" md:"-"`
	HypothesisID uint      `json:"hypothesisId" gorm:"index;index:idx_prediction_signal_unique,unique" md:"假设ID"`
	StockCode    string    `json:"stockCode" gorm:"size:20;index;index:idx_prediction_signal_unique,unique" md:"股票代码"`
	StockName    string    `json:"stockName" gorm:"size:50" md:"股票名称"`
	SignalDate   string    `json:"signalDate" gorm:"size:10;index;index:idx_prediction_signal_unique,unique" md:"信号日期"` // 信号日
	EntryPrice   float64   `json:"entryPrice" md:"买入价"`
	EntryDate    string    `json:"entryDate" gorm:"size:10" md:"实际模拟入场日"`
	TargetDate   string    `json:"targetDate" gorm:"size:10" md:"目标验证日"` // 目标验证日
	TargetReturn float64   `json:"targetReturn" md:"目标收益率"`
	ActualReturn float64   `json:"actualReturn" md:"实际收益"`
	MaxReturn    float64   `json:"maxReturn" md:"期间最大涨幅"`
	MaxDrawdown  float64   `json:"maxDrawdown" md:"期间最大回撤"`
	ValidatedAt  time.Time `json:"validatedAt" md:"验证时间"`
	Hit          bool      `json:"hit" md:"是否命中"`
	Status       string    `json:"status" gorm:"size:20;index" md:"状态"` // pending/validated
	DecisionID   string    `json:"decisionId" gorm:"size:64;index" md:"决策ID"`
	DataAsOf     time.Time `json:"dataAsOf" md:"数据截至"`
	ReasonsJSON  string    `json:"reasonsJson" gorm:"type:text" md:"原因JSON"`
	RisksJSON    string    `json:"risksJson" gorm:"type:text" md:"风险JSON"`
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

// PredictionPaperAccount 是策略前向验证的独立模拟账户。
type PredictionPaperAccount struct {
	ID                uint      `json:"id" gorm:"primarykey"`
	HypothesisID      uint      `json:"hypothesisId" gorm:"uniqueIndex"`
	InitialCash       float64   `json:"initialCash"`
	Cash              float64   `json:"cash"`
	MarketValue       float64   `json:"marketValue"`
	TotalValue        float64   `json:"totalValue"`
	Nav               float64   `json:"nav"`
	MaxNav            float64   `json:"maxNav"`
	MaxDrawdown       float64   `json:"maxDrawdown"`
	TradingDays       int       `json:"tradingDays"`
	TradeCount        int       `json:"tradeCount"`
	WinCount          int       `json:"winCount"`
	PositionCount     int       `json:"positionCount"`
	Status            string    `json:"status" gorm:"size:20;index"`
	StartDate         string    `json:"startDate" gorm:"size:10"`
	LastProcessedDate string    `json:"lastProcessedDate" gorm:"size:10;index"`
	CreatedAt         time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt         time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (PredictionPaperAccount) TableName() string { return "prediction_paper_account" }

// PredictionPaperPosition 保存前向验证中的真实模拟持仓。
type PredictionPaperPosition struct {
	ID                uint      `json:"id" gorm:"primarykey"`
	AccountID         uint      `json:"accountId" gorm:"index;uniqueIndex:uidx_paper_position_account_stock"`
	HypothesisID      uint      `json:"hypothesisId" gorm:"index"`
	SignalID          uint      `json:"signalId" gorm:"index"`
	StockCode         string    `json:"stockCode" gorm:"size:20;index;uniqueIndex:uidx_paper_position_account_stock"`
	StockName         string    `json:"stockName" gorm:"size:50"`
	SignalDate        string    `json:"signalDate" gorm:"size:10"`
	EntryDate         string    `json:"entryDate" gorm:"size:10"`
	EntryPrice        float64   `json:"entryPrice"`
	AvgCost           float64   `json:"avgCost"`
	CostAmount        float64   `json:"costAmount"`
	EntryFee          float64   `json:"entryFee"`
	EntrySlippage     float64   `json:"entrySlippage"`
	Quantity          float64   `json:"quantity"`
	MaxPrice          float64   `json:"maxPrice"`
	MinPrice          float64   `json:"minPrice"`
	LastPrice         float64   `json:"lastPrice"`
	BuyDayIndex       int       `json:"buyDayIndex"`
	PendingExitReason string    `json:"pendingExitReason" gorm:"size:40"`
	EntryReasonJSON   string    `json:"entryReasonJson" gorm:"type:text"`
	FeatureVersion    string    `json:"featureVersion" gorm:"size:50"`
	DataAsOf          time.Time `json:"dataAsOf"`
	CreatedAt         time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt         time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (PredictionPaperPosition) TableName() string { return "prediction_paper_position" }

// PredictionPaperTrade 保存前向验证的闭合交易及完整成本归因。
type PredictionPaperTrade struct {
	ID              uint      `json:"id" gorm:"primarykey"`
	AccountID       uint      `json:"accountId" gorm:"index"`
	HypothesisID    uint      `json:"hypothesisId" gorm:"index"`
	SignalID        uint      `json:"signalId" gorm:"index"`
	StockCode       string    `json:"stockCode" gorm:"size:20;index"`
	StockName       string    `json:"stockName" gorm:"size:50"`
	SignalDate      string    `json:"signalDate" gorm:"size:10"`
	BuyDate         string    `json:"buyDate" gorm:"size:10"`
	SellDate        string    `json:"sellDate" gorm:"size:10;index"`
	BuyPrice        float64   `json:"buyPrice"`
	SellPrice       float64   `json:"sellPrice"`
	Quantity        float64   `json:"quantity"`
	GrossBuyAmount  float64   `json:"grossBuyAmount"`
	GrossSellAmount float64   `json:"grossSellAmount"`
	Fee             float64   `json:"fee"`
	Slippage        float64   `json:"slippage"`
	ReturnRate      float64   `json:"returnRate"`
	MaxReturn       float64   `json:"maxReturn"`
	MaxDrawdown     float64   `json:"maxDrawdown"`
	HoldDays        int       `json:"holdDays"`
	ExitReason      string    `json:"exitReason" gorm:"size:40"`
	EntryReasonJSON string    `json:"entryReasonJson" gorm:"type:text"`
	FeatureVersion  string    `json:"featureVersion" gorm:"size:50"`
	DataAsOf        time.Time `json:"dataAsOf"`
	CreatedAt       time.Time `json:"createdAt" gorm:"autoCreateTime"`
}

func (PredictionPaperTrade) TableName() string { return "prediction_paper_trade" }

// PredictionPaperDaily 保存每个交易日收盘后的账户快照。
type PredictionPaperDaily struct {
	ID            uint      `json:"id" gorm:"primarykey"`
	AccountID     uint      `json:"accountId" gorm:"index;uniqueIndex:uidx_paper_daily_account_date"`
	HypothesisID  uint      `json:"hypothesisId" gorm:"index"`
	Date          string    `json:"date" gorm:"size:10;index;uniqueIndex:uidx_paper_daily_account_date"`
	Cash          float64   `json:"cash"`
	MarketValue   float64   `json:"marketValue"`
	TotalValue    float64   `json:"totalValue"`
	Nav           float64   `json:"nav"`
	Drawdown      float64   `json:"drawdown"`
	PositionCount int       `json:"positionCount"`
	TradeCount    int       `json:"tradeCount"`
	CreatedAt     time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt     time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (PredictionPaperDaily) TableName() string { return "prediction_paper_daily" }

// StockFeature 预计算的股票特征，每天收盘后更新
type StockFeature struct {
	ID             uint      `json:"id" gorm:"primarykey" md:"-"`
	StockCode      string    `json:"stockCode" gorm:"size:20;index:idx_stock_feature_date_code;index:idx_stock_feature_code_date_version;uniqueIndex:uidx_stock_feature_code_date_version" md:"股票代码"`
	Date           string    `json:"date" gorm:"size:10;index:idx_stock_feature_date_code;index:idx_stock_feature_code_date_version;uniqueIndex:uidx_stock_feature_code_date_version" md:"日期"`
	Close          float64   `json:"close" md:"收盘价"`
	Open           float64   `json:"open" md:"开盘价"`
	High           float64   `json:"high" md:"最高价"`
	Low            float64   `json:"low" md:"最低价"`
	Volume         float64   `json:"volume" md:"成交量"`
	Turnover       float64   `json:"turnover" md:"成交额"`
	MA5            float64   `json:"ma5" md:"MA5"`
	MA10           float64   `json:"ma10" md:"MA10"`
	MA20           float64   `json:"ma20" md:"MA20"`
	MA60           float64   `json:"ma60" md:"MA60"`
	MACD           float64   `json:"macd" md:"MACD"`
	RSI6           float64   `json:"rsi6" md:"RSI6"`
	RSI12          float64   `json:"rsi12" md:"RSI12"`
	KDJ_K          float64   `json:"kdjK" md:"KDJ_K"`
	BOLLUpper      float64   `json:"bollUpper" md:"布林上轨"`
	BOLLMid        float64   `json:"bollMid" md:"布林中轨"`
	BOLLLower      float64   `json:"bollLower" md:"布林下轨"`
	VolumeRatio    float64   `json:"volumeRatio" md:"量比"`
	ATR            float64   `json:"atr" md:"ATR"`
	FundFlow5      float64   `json:"fundFlow5" md:"5日资金净流入"`
	FundFlow20     float64   `json:"fundFlow20" md:"20日资金净流入"`
	ChangeRate5    float64   `json:"changeRate5" md:"5日涨跌幅"`
	ChangeRate20   float64   `json:"changeRate20" md:"20日涨跌幅"`
	DataAsOf       time.Time `json:"dataAsOf" md:"数据截至"`
	Source         string    `json:"source" gorm:"size:50" md:"数据源"`
	FeatureVersion string    `json:"featureVersion" gorm:"size:50;index:idx_stock_feature_code_date_version;uniqueIndex:uidx_stock_feature_code_date_version" md:"特征版本"`
	Adjusted       bool      `json:"adjusted" md:"是否复权"`
}

func (StockFeature) TableName() string {
	return "stock_feature"
}

type FeatureSyncJob struct {
	ID           uint       `json:"id" gorm:"primarykey"`
	JobKey       string     `json:"jobKey" gorm:"size:255;uniqueIndex"`
	StockScope   string     `json:"stockScope" gorm:"size:100;index"`
	StartDate    string     `json:"startDate" gorm:"size:10"`
	EndDate      string     `json:"endDate" gorm:"size:10"`
	Total        int        `json:"total"`
	Finished     int        `json:"finished"`
	Failed       int        `json:"failed"`
	Status       string     `json:"status" gorm:"size:20;index"`
	DataAsOf     time.Time  `json:"dataAsOf"`
	CoverageJSON string     `json:"coverageJson" gorm:"type:text"`
	ExcludedJSON string     `json:"excludedJson" gorm:"type:text"`
	RetryCount   int        `json:"retryCount"`
	StartedAt    time.Time  `json:"startedAt"`
	FinishedAt   *time.Time `json:"finishedAt"`
	ErrorMessage string     `json:"errorMessage" gorm:"type:text"`
}

func (FeatureSyncJob) TableName() string {
	return "feature_sync_job"
}

type PredictionTrade struct {
	ID              uint      `json:"id" gorm:"primarykey"`
	HypothesisID    uint      `json:"hypothesisId" gorm:"index:idx_prediction_trade_hyp_stock_date"`
	StockCode       string    `json:"stockCode" gorm:"size:20;index:idx_prediction_trade_hyp_stock_date"`
	StockName       string    `json:"stockName" gorm:"size:50"`
	SignalDate      string    `json:"signalDate" gorm:"size:10"`
	BuyDate         string    `json:"buyDate" gorm:"size:10;index:idx_prediction_trade_hyp_stock_date"`
	SellDate        string    `json:"sellDate" gorm:"size:10"`
	BuyPrice        float64   `json:"buyPrice"`
	SellPrice       float64   `json:"sellPrice"`
	Quantity        float64   `json:"quantity"`
	GrossBuyAmount  float64   `json:"grossBuyAmount"`
	GrossSellAmount float64   `json:"grossSellAmount"`
	Fee             float64   `json:"fee"`
	Slippage        float64   `json:"slippage"`
	ReturnRate      float64   `json:"returnRate"`
	MaxReturn       float64   `json:"maxReturn"`
	MaxDrawdown     float64   `json:"maxDrawdown"`
	HoldDays        int       `json:"holdDays"`
	EntryReasonJSON string    `json:"entryReasonJson" gorm:"type:text"`
	ExitReason      string    `json:"exitReason" gorm:"size:50"`
	FeatureVersion  string    `json:"featureVersion" gorm:"size:50"`
	DataAsOf        time.Time `json:"dataAsOf"`
	CreatedAt       time.Time `json:"createdAt" gorm:"autoCreateTime"`
}

func (PredictionTrade) TableName() string {
	return "prediction_trade"
}

type TradeDecisionLog struct {
	ID               uint      `json:"id" gorm:"primarykey"`
	DecisionID       string    `json:"decisionId" gorm:"size:64;uniqueIndex"`
	StockCode        string    `json:"stockCode" gorm:"size:20;index:idx_trade_decision_stock_signal"`
	StockName        string    `json:"stockName" gorm:"size:50"`
	Action           string    `json:"action" gorm:"size:30"`
	Score            float64   `json:"score"`
	ScoreType        string    `json:"scoreType" gorm:"size:30"`
	CurrentPrice     float64   `json:"currentPrice"`
	ReasonsJSON      string    `json:"reasonsJson" gorm:"type:text"`
	RisksJSON        string    `json:"risksJson" gorm:"type:text"`
	MatchedFactsJSON string    `json:"matchedFactsJson" gorm:"type:text"`
	MetricsJSON      string    `json:"metricsJson" gorm:"type:text"`
	StrategyID       uint      `json:"strategyId"`
	StrategyVersion  string    `json:"strategyVersion" gorm:"size:50"`
	FeatureVersion   string    `json:"featureVersion" gorm:"size:50"`
	SignalDate       string    `json:"signalDate" gorm:"size:10;index:idx_trade_decision_stock_signal"`
	DataAsOf         time.Time `json:"dataAsOf"`
	ValidUntil       time.Time `json:"validUntil"`
	CreatedAt        time.Time `json:"createdAt" gorm:"autoCreateTime"`
}

func (TradeDecisionLog) TableName() string {
	return "trade_decision_log"
}

type PredictionGenerationAudit struct {
	ID              uint      `json:"id" gorm:"primarykey"`
	SessionID       uint      `json:"sessionId" gorm:"index"`
	Source          string    `json:"source" gorm:"size:30"` // ai / template / validator
	Prompt          string    `json:"prompt" gorm:"type:text"`
	RawOutput       string    `json:"rawOutput" gorm:"type:text"`
	NormalizedDSL   string    `json:"normalizedDsl" gorm:"type:text"`
	ToolCallsJSON   string    `json:"toolCallsJson" gorm:"type:text"`
	AIConfigID      int       `json:"aiConfigId"`
	ModelName       string    `json:"modelName" gorm:"size:100"`
	Temperature     float64   `json:"temperature"`
	ErrorJSON       string    `json:"errorJson" gorm:"type:text"`
	SchemaVersion   string    `json:"schemaVersion" gorm:"size:50"`
	RegistryVersion string    `json:"registryVersion" gorm:"size:50"`
	EngineVersion   string    `json:"engineVersion" gorm:"size:50"`
	CreatedAt       time.Time `json:"createdAt" gorm:"autoCreateTime"`
}

func (PredictionGenerationAudit) TableName() string {
	return "prediction_generation_audit"
}

type PredictionResearchIdea struct {
	ID               uint      `json:"id" gorm:"primarykey"`
	SessionID        uint      `json:"sessionId" gorm:"index"`
	Source           string    `json:"source" gorm:"size:30;index"`
	Scene            string    `json:"scene" gorm:"size:50;index"`
	StockScope       string    `json:"stockScope" gorm:"size:100"`
	Theme            string    `json:"theme" gorm:"size:120"`
	CandidateFactors string    `json:"candidateFactors" gorm:"type:text"`
	RuleIdeas        string    `json:"ruleIdeas" gorm:"type:text"`
	DataEvidence     string    `json:"dataEvidence" gorm:"type:text"`
	RiskHypotheses   string    `json:"riskHypotheses" gorm:"type:text"`
	Questions        string    `json:"questions" gorm:"type:text"`
	Status           string    `json:"status" gorm:"size:20;index"`
	CreatedAt        time.Time `json:"createdAt" gorm:"autoCreateTime"`
}

func (PredictionResearchIdea) TableName() string {
	return "prediction_research_idea"
}

type MarketFactorDaily struct {
	ID             uint      `json:"id" gorm:"primarykey"`
	TradeDate      string    `json:"tradeDate" gorm:"size:10;index"`
	DataAsOf       time.Time `json:"dataAsOf"`
	UpCount        int       `json:"upCount"`
	DownCount      int       `json:"downCount"`
	LimitUpCount   int       `json:"limitUpCount"`
	LimitDownCount int       `json:"limitDownCount"`
	SentimentScore float64   `json:"sentimentScore"`
	Source         string    `json:"source" gorm:"size:50"`
}

func (MarketFactorDaily) TableName() string {
	return "market_factor_daily"
}

type StockMoneyFlowDaily struct {
	ID                 uint      `json:"id" gorm:"primarykey"`
	StockCode          string    `json:"stockCode" gorm:"size:20;index:idx_stock_money_flow_code_date"`
	TradeDate          string    `json:"tradeDate" gorm:"size:10;index:idx_stock_money_flow_code_date"`
	DataAsOf           time.Time `json:"dataAsOf"`
	MainNetInflow1     float64   `json:"mainNetInflow1"`
	MainNetInflow5     float64   `json:"mainNetInflow5"`
	MainNetInflow20    float64   `json:"mainNetInflow20"`
	MainNetInflowRatio float64   `json:"mainNetInflowRatio"`
	SuperLargeNet1     float64   `json:"superLargeNet1"`
	SuperLargeRatio    float64   `json:"superLargeRatio"`
	LargeNet1          float64   `json:"largeNet1"`
	LargeRatio         float64   `json:"largeRatio"`
	MediumNet1         float64   `json:"mediumNet1"`
	MediumRatio        float64   `json:"mediumRatio"`
	SmallNet1          float64   `json:"smallNet1"`
	SmallRatio         float64   `json:"smallRatio"`
	RetailNetInflow1   float64   `json:"retailNetInflow1"`
	MacMainNetInflow1  float64   `json:"macMainNetInflow1"`
	MacMainNetInflow5  float64   `json:"macMainNetInflow5"`
	MacRetailNetIn1    float64   `json:"macRetailNetIn1"`
	NorthboundNetIn    float64   `json:"northboundNetIn"`
	SectorNetInflow    float64   `json:"sectorNetInflow"`
	ConceptNetInflow   float64   `json:"conceptNetInflow"`
	Source             string    `json:"source" gorm:"size:50"`
}

func (StockMoneyFlowDaily) TableName() string {
	return "stock_money_flow_daily"
}

type SectorFlowDaily struct {
	ID         uint      `json:"id" gorm:"primarykey"`
	SectorType string    `json:"sectorType" gorm:"size:20;index:idx_sector_flow_type_name_date"` // industry / concept
	SectorName string    `json:"sectorName" gorm:"size:100;index:idx_sector_flow_type_name_date"`
	TradeDate  string    `json:"tradeDate" gorm:"size:10;index:idx_sector_flow_type_name_date"`
	DataAsOf   time.Time `json:"dataAsOf"`
	NetInflow  float64   `json:"netInflow"`
	Rank       int       `json:"rank"`
	Source     string    `json:"source" gorm:"size:50"`
}

func (SectorFlowDaily) TableName() string {
	return "sector_flow_daily"
}

type StockEventDaily struct {
	ID               uint      `json:"id" gorm:"primarykey"`
	StockCode        string    `json:"stockCode" gorm:"size:20;uniqueIndex:uidx_stock_event_code_date_version"`
	TradeDate        string    `json:"tradeDate" gorm:"size:10;uniqueIndex:uidx_stock_event_code_date_version"`
	FeatureVersion   string    `json:"featureVersion" gorm:"size:50;uniqueIndex:uidx_stock_event_code_date_version"`
	DataAsOf         time.Time `json:"dataAsOf"`
	AvailableAt      time.Time `json:"availableAt" gorm:"index"`
	ChangeEventCount int       `json:"changeEventCount"`
	HasLargeBuy      bool      `json:"hasLargeBuy"`
	HasLargeSell     bool      `json:"hasLargeSell"`
	HasLimitUp       bool      `json:"hasLimitUp"`
	HasLimitDown     bool      `json:"hasLimitDown"`
	HasRapidRise     bool      `json:"hasRapidRise"`
	HasRapidFall     bool      `json:"hasRapidFall"`
	Source           string    `json:"source" gorm:"size:50"`
}

func (StockEventDaily) TableName() string {
	return "stock_event_daily"
}

type StockRiskEvent struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	StockCode string    `json:"stockCode" gorm:"size:20;index"`
	RiskType  string    `json:"riskType" gorm:"size:30;index"` // notice/news/st/suspend/delist
	Level     string    `json:"level" gorm:"size:20"`
	Reason    string    `json:"reason" gorm:"type:text"`
	ValidFrom string    `json:"validFrom" gorm:"size:10;index"`
	ValidTo   string    `json:"validTo" gorm:"size:10;index"`
	DataAsOf  time.Time `json:"dataAsOf"`
	Source    string    `json:"source" gorm:"size:50"`
}

func (StockRiskEvent) TableName() string {
	return "stock_risk_event"
}

// CandidateSnapshot records one point-in-time candidate-universe build. External
// source results are never read directly by the strategy DSL; they enter here first.
type CandidateSnapshot struct {
	ID              uint      `json:"id" gorm:"primarykey"`
	SnapshotKey     string    `json:"snapshotKey" gorm:"size:64;uniqueIndex"`
	Name            string    `json:"name" gorm:"size:120"`
	Scene           string    `json:"scene" gorm:"size:50;index"`
	StockScope      string    `json:"stockScope" gorm:"size:100"`
	SourcesJSON     string    `json:"sourcesJson" gorm:"type:text"`
	SourceMode      string    `json:"sourceMode" gorm:"size:20"` // union/intersection/consensus
	MinimumSources  int       `json:"minimumSources"`
	QueryJSON       string    `json:"queryJson" gorm:"type:text"`
	TradeDate       string    `json:"tradeDate" gorm:"size:10;index"`
	AvailableAt     time.Time `json:"availableAt" gorm:"index"`
	DataAsOf        time.Time `json:"dataAsOf"`
	SourceVersion   string    `json:"sourceVersion" gorm:"size:50"`
	SchemaVersion   string    `json:"schemaVersion" gorm:"size:50"`
	FeatureVersion  string    `json:"featureVersion" gorm:"size:50"`
	RegistryVersion string    `json:"registryVersion" gorm:"size:50"`
	RawHash         string    `json:"rawHash" gorm:"size:64;index"`
	AblationJSON    string    `json:"ablationJson" gorm:"type:text"`
	Coverage        float64   `json:"coverage"`
	CandidateCount  int       `json:"candidateCount"`
	Status          string    `json:"status" gorm:"size:20;index"` // running/candidate/validated/failed/expired
	ErrorMessage    string    `json:"errorMessage" gorm:"size:500"`
	CreatedAt       time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt       time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (CandidateSnapshot) TableName() string { return "candidate_snapshot" }

// CandidateSnapshotItem is a normalized stock candidate with complete source
// attribution and independent scores for each investment scene.
type CandidateSnapshotItem struct {
	ID               uint      `json:"id" gorm:"primarykey"`
	SnapshotID       uint      `json:"snapshotId" gorm:"index;uniqueIndex:uidx_candidate_snapshot_stock"`
	StockCode        string    `json:"stockCode" gorm:"size:20;index;uniqueIndex:uidx_candidate_snapshot_stock"`
	StockName        string    `json:"stockName" gorm:"size:50"`
	Industry         string    `json:"industry" gorm:"size:100;index"`
	Concept          string    `json:"concept" gorm:"size:500"`
	SourceRank       int       `json:"sourceRank"`
	SourceScore      float64   `json:"sourceScore"`
	SourceCount      int       `json:"sourceCount" gorm:"index"`
	SourcesJSON      string    `json:"sourcesJson" gorm:"type:text"`
	BestScene        string    `json:"bestScene" gorm:"size:50;index"`
	ShortScore       float64   `json:"shortScore"`
	SwingScore       float64   `json:"swingScore"`
	TrendScore       float64   `json:"trendScore"`
	LongTermScore    float64   `json:"longTermScore"`
	MatchedFacts     string    `json:"matchedFacts" gorm:"type:text"`
	ReasonsJSON      string    `json:"reasonsJson" gorm:"type:text"`
	RisksJSON        string    `json:"risksJson" gorm:"type:text"`
	AdviceJSON       string    `json:"adviceJson" gorm:"type:text"`
	AvailableAt      time.Time `json:"availableAt"`
	DataAsOf         time.Time `json:"dataAsOf"`
	FeatureVersion   string    `json:"featureVersion" gorm:"size:50"`
	ScorerVersion    string    `json:"scorerVersion" gorm:"size:50"`
	InputFingerprint string    `json:"inputFingerprint" gorm:"size:64;index"`
	RawJSON          string    `json:"rawJson" gorm:"type:text"`
	CreatedAt        time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt        time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (CandidateSnapshotItem) TableName() string { return "candidate_snapshot_item" }

// CandidateSourceFact preserves source-item-level lineage instead of relying
// only on the merged JSON stored on CandidateSnapshotItem.
type CandidateSourceFact struct {
	ID             uint      `json:"id" gorm:"primarykey"`
	SnapshotID     uint      `json:"snapshotId" gorm:"index;uniqueIndex:uidx_candidate_source_fact"`
	StockCode      string    `json:"stockCode" gorm:"size:20;index;uniqueIndex:uidx_candidate_source_fact"`
	Source         string    `json:"source" gorm:"size:30;index;uniqueIndex:uidx_candidate_source_fact"`
	SourceRecordID string    `json:"sourceRecordId" gorm:"size:100;uniqueIndex:uidx_candidate_source_fact"`
	SourceRank     int       `json:"sourceRank"`
	SourceScore    float64   `json:"sourceScore"`
	EventTime      time.Time `json:"eventTime"`
	IngestedAt     time.Time `json:"ingestedAt"`
	AvailableAt    time.Time `json:"availableAt" gorm:"index"`
	DataAsOf       time.Time `json:"dataAsOf"`
	SourceVersion  string    `json:"sourceVersion" gorm:"size:50"`
	FactsJSON      string    `json:"factsJson" gorm:"type:text"`
	RawHash        string    `json:"rawHash" gorm:"size:64;index"`
	RawJSON        string    `json:"rawJson" gorm:"type:text"`
	CreatedAt      time.Time `json:"createdAt" gorm:"autoCreateTime"`
}

func (CandidateSourceFact) TableName() string { return "candidate_source_fact" }

// ScreeningExecutionSnapshot freezes the exact output received from the
// pattern/indicator pages. It prevents a page result from being mislabeled as
// a locally reproduced technical fact.
type ScreeningExecutionSnapshot struct {
	ID              uint      `json:"id" gorm:"primarykey"`
	SnapshotKey     string    `json:"snapshotKey" gorm:"size:64;uniqueIndex"`
	Source          string    `json:"source" gorm:"size:30;index"`
	Query           string    `json:"query" gorm:"type:text"`
	NormalizedQuery string    `json:"normalizedQuery" gorm:"type:text"`
	TradeDate       string    `json:"tradeDate" gorm:"size:10;index"`
	AvailableAt     time.Time `json:"availableAt" gorm:"index"`
	RawHash         string    `json:"rawHash" gorm:"size:64;index"`
	ResultCount     int       `json:"resultCount"`
	Status          string    `json:"status" gorm:"size:20;index"`
	CreatedAt       time.Time `json:"createdAt" gorm:"autoCreateTime"`
}

func (ScreeningExecutionSnapshot) TableName() string { return "screening_execution_snapshot" }

type ScreeningExecutionItem struct {
	ID          uint      `json:"id" gorm:"primarykey"`
	SnapshotID  uint      `json:"snapshotId" gorm:"index;uniqueIndex:uidx_screening_execution_item"`
	StockCode   string    `json:"stockCode" gorm:"size:20;index;uniqueIndex:uidx_screening_execution_item"`
	StockName   string    `json:"stockName" gorm:"size:50"`
	SourceRank  int       `json:"sourceRank"`
	RawHash     string    `json:"rawHash" gorm:"size:64"`
	RawJSON     string    `json:"rawJson" gorm:"type:text"`
	AvailableAt time.Time `json:"availableAt" gorm:"index"`
	CreatedAt   time.Time `json:"createdAt" gorm:"autoCreateTime"`
}

func (ScreeningExecutionItem) TableName() string { return "screening_execution_item" }

// StockScreeningFactDaily stores locally reproducible technical pattern flags.
type StockScreeningFactDaily struct {
	ID              uint      `json:"id" gorm:"primarykey"`
	StockCode       string    `json:"stockCode" gorm:"size:20;index;uniqueIndex:uidx_screening_fact_stock_date_version"`
	TradeDate       string    `json:"tradeDate" gorm:"size:10;index;uniqueIndex:uidx_screening_fact_stock_date_version"`
	FeatureVersion  string    `json:"featureVersion" gorm:"size:50;uniqueIndex:uidx_screening_fact_stock_date_version"`
	DataAsOf        time.Time `json:"dataAsOf"`
	AvailableAt     time.Time `json:"availableAt"`
	Calculated      bool      `json:"calculated" gorm:"index"`
	Coverage        float64   `json:"coverage"`
	Status          string    `json:"status" gorm:"size:20;index"`
	MACDGoldenCross bool      `json:"macdGoldenCross"`
	MACDAboveZero   bool      `json:"macdAboveZero"`
	KDJGoldenCross  bool      `json:"kdjGoldenCross"`
	MABullish       bool      `json:"maBullish"`
	MABearish       bool      `json:"maBearish"`
	BreakMA20       bool      `json:"breakMa20"`
	BollBreakout    bool      `json:"bollBreakout"`
	VolumeBreakout  bool      `json:"volumeBreakout"`
	Oversold        bool      `json:"oversold"`
	Overbought      bool      `json:"overbought"`
	PatternCount    int       `json:"patternCount"`
	ConditionsJSON  string    `json:"conditionsJson" gorm:"type:text"`
	Source          string    `json:"source" gorm:"size:50"`
	CreatedAt       time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt       time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (StockScreeningFactDaily) TableName() string { return "stock_screening_fact_daily" }

// UplimitStockDaily is the typed, auditable replacement for the previous
// unversioned map response used by the limit-up ladder page.
type UplimitStockDaily struct {
	ID                uint      `json:"id" gorm:"primarykey"`
	SnapshotKey       string    `json:"snapshotKey" gorm:"size:64;index;uniqueIndex:uidx_uplimit_stock_snapshot"`
	StockCode         string    `json:"stockCode" gorm:"size:20;index;uniqueIndex:uidx_uplimit_stock_snapshot"`
	StockName         string    `json:"stockName" gorm:"size:50"`
	TradeDate         string    `json:"tradeDate" gorm:"size:10;index"`
	PlateCode         string    `json:"plateCode" gorm:"size:30;uniqueIndex:uidx_uplimit_stock_snapshot"`
	PlateName         string    `json:"plateName" gorm:"size:100"`
	KeepTimes         int       `json:"keepTimes"`
	LimitType         string    `json:"limitType" gorm:"size:30"`
	LimitDescription  string    `json:"limitDescription" gorm:"size:100"`
	LimitTime         string    `json:"limitTime" gorm:"size:10"`
	FirstLimitTime    string    `json:"firstLimitTime" gorm:"size:10"`
	FinalLimitTime    string    `json:"finalLimitTime" gorm:"size:10"`
	SealRatioMax      float64   `json:"sealRatioMax"`
	SealRatioClose    float64   `json:"sealRatioClose"`
	Exploded          bool      `json:"exploded" gorm:"index"`
	ExplodeCount      int       `json:"explodeCount"`
	Amount            float64   `json:"amount"`
	MarketCap         float64   `json:"marketCap"`
	PlateHeat         float64   `json:"plateHeat"`
	PlateLimitCount   int       `json:"plateLimitCount"`
	PlateExplodeCount int       `json:"plateExplodeCount"`
	AvailableAt       time.Time `json:"availableAt"`
	DataAsOf          time.Time `json:"dataAsOf"`
	EventTime         time.Time `json:"eventTime"`
	IngestedAt        time.Time `json:"ingestedAt"`
	Source            string    `json:"source" gorm:"size:50"`
	SourceVersion     string    `json:"sourceVersion" gorm:"size:50"`
	RawHash           string    `json:"rawHash" gorm:"size:64"`
	RawJSON           string    `json:"rawJson" gorm:"type:text"`
	CreatedAt         time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt         time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (UplimitStockDaily) TableName() string { return "uplimit_stock_daily" }

// ModelRecommendationEvent converts free-form AI recommendations into a
// point-in-time meta signal without treating AI output as ground truth.
type ModelRecommendationEvent struct {
	ID                     uint      `json:"id" gorm:"primarykey"`
	RecommendationID       uint      `json:"recommendationId" gorm:"index"`
	SourceRecordID         string    `json:"sourceRecordId" gorm:"size:100;uniqueIndex:uidx_model_recommendation_event"`
	StockCode              string    `json:"stockCode" gorm:"size:20;index;uniqueIndex:uidx_model_recommendation_event"`
	Source                 string    `json:"source" gorm:"size:30;uniqueIndex:uidx_model_recommendation_event"`
	TradeDate              string    `json:"tradeDate" gorm:"size:10;index"`
	Scene                  string    `json:"scene" gorm:"size:50;index"`
	Horizon                int       `json:"horizon"`
	ModelName              string    `json:"modelName" gorm:"size:100"`
	ModelVersion           string    `json:"modelVersion" gorm:"size:50"`
	Rating                 string    `json:"rating" gorm:"size:30"`
	ReferencePrice         float64   `json:"referencePrice"`
	BuyPriceMin            float64   `json:"buyPriceMin"`
	BuyPriceMax            float64   `json:"buyPriceMax"`
	StopLossPrice          float64   `json:"stopLossPrice"`
	TakeProfitPrice        float64   `json:"takeProfitPrice"`
	AvailableAt            time.Time `json:"availableAt"`
	DataAsOf               time.Time `json:"dataAsOf"`
	FeatureVersion         string    `json:"featureVersion" gorm:"size:50"`
	GenerationAuditID      uint      `json:"generationAuditId" gorm:"index"`
	LineageStatus          string    `json:"lineageStatus" gorm:"size:30;index"`
	IndependentForResearch bool      `json:"independentForResearch" gorm:"default:false;index"`
	RecommendationHash     string    `json:"recommendationHash" gorm:"size:64;index"`
	ForwardReturn5         float64   `json:"forwardReturn5"`
	ForwardReturn20        float64   `json:"forwardReturn20"`
	ValidationStatus       string    `json:"validationStatus" gorm:"size:20;index"`
	CreatedAt              time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt              time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (ModelRecommendationEvent) TableName() string { return "model_recommendation_event" }

type SystemCronTaskStatus struct {
	ID            uint       `json:"id"`
	Name          string     `json:"name"`
	TaskType      string     `json:"taskType"`
	CronExpr      string     `json:"cronExpr"`
	Enable        bool       `json:"enable"`
	Status        string     `json:"status"`
	LastRunAt     *time.Time `json:"lastRunAt"`
	NextRunAt     *time.Time `json:"nextRunAt"`
	RunCount      int64      `json:"runCount"`
	LastRunResult string     `json:"lastRunResult"`
	Description   string     `json:"description"`
}

type FeatureCoverage struct {
	StockScope            string   `json:"stockScope"`
	StartDate             string   `json:"startDate"`
	EndDate               string   `json:"endDate"`
	ExpectedStockCount    int      `json:"expectedStockCount"`
	CoveredStockCount     int      `json:"coveredStockCount"`
	ExpectedTradeDays     int      `json:"expectedTradeDays"`
	CoveredTradeDays      int      `json:"coveredTradeDays"`
	CoreFieldCoverage     float64  `json:"coreFieldCoverage"`
	StockCoverage         float64  `json:"stockCoverage"`
	TradeDayCoverage      float64  `json:"tradeDayCoverage"`
	CoreIndicatorCoverage float64  `json:"coreIndicatorCoverage"`
	Ready                 bool     `json:"ready"`
	Message               string   `json:"message"`
	ExcludedReasons       []string `json:"excludedReasons"`
}
