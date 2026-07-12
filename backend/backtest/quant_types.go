package backtest

import (
	"go-stock/backend/models"
	"time"
)

const (
	CurrentStrategyVersion   = "strategy_v4"
	CurrentFeatureVersion    = "daily_v3_qfq"
	CurrentEngineVersion     = "quant-engine/v3"
	CurrentVerdictVersion    = "quant-verdict/v2"
	MinPoolStrategySamples   = 30
	MinStockStrategySamples  = 10
	MinPaperTradeValidations = 10
)

type ResearchIdea struct {
	ID               string                    `json:"id"`
	SchemaVersion    string                    `json:"schemaVersion"`
	Template         string                    `json:"template"`
	Source           string                    `json:"source"`
	Scene            string                    `json:"scene"`
	StockScope       string                    `json:"stockScope"`
	Theme            string                    `json:"theme"`
	CandidateFactors []ResearchCandidateFactor `json:"candidateFactors"`
	RuleIdeas        []string                  `json:"ruleIdeas"`
	DataEvidence     []string                  `json:"dataEvidence"`
	RiskHypotheses   []string                  `json:"riskHypotheses"`
	Questions        []string                  `json:"questions"`
	CreatedAt        string                    `json:"createdAt"`
}

type ResearchCandidateFactor struct {
	FactorID   string                 `json:"factorId"`
	Source     string                 `json:"source"`
	Evidence   ResearchFactorEvidence `json:"evidence"`
	Confidence string                 `json:"confidence"`
}

type ResearchFactorEvidence struct {
	StartDate        string  `json:"startDate"`
	EndDate          string  `json:"endDate"`
	Horizon          int     `json:"horizon"`
	SampleSize       int     `json:"sampleSize"`
	IC               float64 `json:"ic"`
	RankIC           float64 `json:"rankIC"`
	HitRate          float64 `json:"hitRate"`
	AvgForwardReturn float64 `json:"avgForwardReturn"`
	PValue           float64 `json:"pValue"`
	SectorIC         float64 `json:"sectorIC"`
	SectorICStd      float64 `json:"sectorICStd"`
	TimeDecay        string  `json:"timeDecay"`
	RecentIC         float64 `json:"recentIC"`
}

type QuantVerdict struct {
	Version            string             `json:"version"`
	Layer              string             `json:"layer"`
	Status             string             `json:"status"`
	Reasons            []string           `json:"reasons"`
	Risks              []string           `json:"risks"`
	RequiredActions    []string           `json:"requiredActions"`
	CostSensitivity    []CostStressResult `json:"costSensitivity"`
	OverfitDiagnostics OverfitDiagnostics `json:"overfitDiagnostics"`
	Turnover           TurnoverBreakdown  `json:"turnover"`
	GeneratedAt        time.Time          `json:"generatedAt"`
}

type CostStressResult struct {
	Name        string  `json:"name"`
	Slippage    float64 `json:"slippage"`
	FeeRate     float64 `json:"feeRate"`
	TotalReturn float64 `json:"totalReturn"`
	AvgReturn   float64 `json:"avgReturn"`
	MaxDrawdown float64 `json:"maxDrawdown"`
	TradeCount  int     `json:"tradeCount"`
	Passed      bool    `json:"passed"`
}

type OverfitDiagnostics struct {
	OutSampleDecay     float64                 `json:"outSampleDecay"`
	FoldConsistency    float64                 `json:"foldConsistency"`
	ParameterStability string                  `json:"parameterStability"`
	ParameterStress    []ParameterStressResult `json:"parameterStress"`
	RuleComplexity     int                     `json:"ruleComplexity"`
	SectorIC           float64                 `json:"sectorIC"`
	SectorICStd        float64                 `json:"sectorICStd"`
	ICDecay            float64                 `json:"icDecay"`
	ResearchSamples    int                     `json:"researchSamples"`
	Warnings           []string                `json:"warnings"`
}

type ParameterStressResult struct {
	Name        string  `json:"name"`
	AvgReturn   float64 `json:"avgReturn"`
	MaxDrawdown float64 `json:"maxDrawdown"`
	TradeCount  int     `json:"tradeCount"`
	Passed      bool    `json:"passed"`
}

type TurnoverBreakdown struct {
	BuyTurnover      float64 `json:"buyTurnover"`
	SellTurnover     float64 `json:"sellTurnover"`
	TwoSidedTurnover float64 `json:"twoSidedTurnover"`
}

type QuantSignalSide string

const (
	QuantSignalBuy  QuantSignalSide = "BUY"
	QuantSignalSell QuantSignalSide = "SELL"
)

type QuantSignal struct {
	StockCode  string          `json:"stockCode"`
	StockName  string          `json:"stockName"`
	Date       string          `json:"date"`
	Side       QuantSignalSide `json:"side"`
	Price      float64         `json:"price"`
	Score      float64         `json:"score"`
	ReasonJSON string          `json:"reasonJson"`
	Source     string          `json:"source"`
}

type OrderSide string

const (
	OrderSideBuy  OrderSide = "BUY"
	OrderSideSell OrderSide = "SELL"
)

type SimOrder struct {
	ID         string    `json:"id"`
	StockCode  string    `json:"stockCode"`
	StockName  string    `json:"stockName"`
	Side       OrderSide `json:"side"`
	SignalDate string    `json:"signalDate"`
	OrderDate  string    `json:"orderDate"`
	PriceHint  float64   `json:"priceHint"`
	Quantity   float64   `json:"quantity"`
	Amount     float64   `json:"amount"`
	ReasonJSON string    `json:"reasonJson"`
	ExitReason string    `json:"exitReason"`
	Source     string    `json:"source"`
}

type SimFill struct {
	OrderID      string    `json:"orderId"`
	StockCode    string    `json:"stockCode"`
	StockName    string    `json:"stockName"`
	Side         OrderSide `json:"side"`
	TradeDate    string    `json:"tradeDate"`
	Price        float64   `json:"price"`
	Quantity     float64   `json:"quantity"`
	GrossAmount  float64   `json:"grossAmount"`
	Fee          float64   `json:"fee"`
	SlippageCost float64   `json:"slippageCost"`
	NetAmount    float64   `json:"netAmount"`
	Status       string    `json:"status"`
	Reason       string    `json:"reason"`
}

type PortfolioPosition struct {
	StockCode       string    `json:"stockCode"`
	StockName       string    `json:"stockName"`
	SignalDate      string    `json:"signalDate"`
	EntryDate       string    `json:"entryDate"`
	EntryPrice      float64   `json:"entryPrice"`
	AvgCost         float64   `json:"avgCost"`
	CostAmount      float64   `json:"costAmount"`
	EntryFee        float64   `json:"entryFee"`
	EntrySlippage   float64   `json:"entrySlippage"`
	Quantity        float64   `json:"quantity"`
	MaxPrice        float64   `json:"maxPrice"`
	MinPrice        float64   `json:"minPrice"`
	LastPrice       float64   `json:"lastPrice"`
	BuyDayIndex     int       `json:"buyDayIndex"`
	EntryReasonJSON string    `json:"entryReasonJson"`
	FeatureVersion  string    `json:"featureVersion"`
	DataAsOf        time.Time `json:"dataAsOf"`
}

type PortfolioSnapshot struct {
	Date          string  `json:"date"`
	Cash          float64 `json:"cash"`
	MarketValue   float64 `json:"marketValue"`
	TotalValue    float64 `json:"totalValue"`
	Drawdown      float64 `json:"drawdown"`
	PositionCount int     `json:"positionCount"`
	TradeCount    int     `json:"tradeCount"`
}

type RiskAssessment struct {
	RiskLevel       string   `json:"riskLevel"`
	CanEnter        bool     `json:"canEnter"`
	ShouldExit      bool     `json:"shouldExit"`
	ExitReason      string   `json:"exitReason"`
	ExitPrice       float64  `json:"exitPrice"`
	ExitAtNextOpen  bool     `json:"exitAtNextOpen"`
	DefensePrice    float64  `json:"defensePrice"`
	StopLossPrice   float64  `json:"stopLossPrice"`
	TakeProfitPrice float64  `json:"takeProfitPrice"`
	RiskScore       float64  `json:"riskScore"`
	Reasons         []string `json:"reasons"`
	Warnings        []string `json:"warnings"`
}

type PositionSizing struct {
	Action            string  `json:"action"`
	TargetPercent     float64 `json:"targetPercent"`
	QuantityPercent   float64 `json:"quantityPercent"`
	SuggestedQuantity int64   `json:"suggestedQuantity"`
	SuggestedAmount   float64 `json:"suggestedAmount"`
	ReferencePrice    float64 `json:"referencePrice"`
	EstimatedRelease  float64 `json:"estimatedRelease"`
	MinTradeUnit      int64   `json:"minTradeUnit"`
	CashConfigured    bool    `json:"cashConfigured"`
	AvailableCash     float64 `json:"availableCash"`
	MaxPositionAmount float64 `json:"maxPositionAmount"`
	AccountWarning    string  `json:"accountWarning"`
}

type AccountConstraint struct {
	AvailableCash            float64 `json:"availableCash"`
	CashConfigured           bool    `json:"cashConfigured"`
	MaxSinglePositionPercent float64 `json:"maxSinglePositionPercent"`
	TotalAsset               float64 `json:"totalAsset"`
	MinTradeUnit             int64   `json:"minTradeUnit"`
}

type CapitalFlowSignal struct {
	StockCode          string    `json:"stockCode"`
	TradeDate          string    `json:"tradeDate"`
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
	FundFlow5          float64   `json:"fundFlow5"`
	FundFlow20         float64   `json:"fundFlow20"`
	ScoreAdjustment    float64   `json:"scoreAdjustment"`
	Level              string    `json:"level"`
	SuperLargeSignal   string    `json:"superLargeSignal"`
	LargeSignal        string    `json:"largeSignal"`
	RetailSignal       string    `json:"retailSignal"`
	SectorSignal       string    `json:"sectorSignal"`
	ConceptSignal      string    `json:"conceptSignal"`
	NorthboundSignal   string    `json:"northboundSignal"`
	Reasons            []string  `json:"reasons"`
	Risks              []string  `json:"risks"`
	DataAsOf           time.Time `json:"dataAsOf"`
	ProxySignal        string    `json:"proxySignal"`
}

type DecisionContext struct {
	Session         *models.PredictionSession
	Hypotheses      []models.PredictionHypothesis
	Feature         models.StockFeature
	PreviousFeature *models.StockFeature
	Quote           quoteSnapshot
	Holding         holdingSnapshot
	PreviousAction  string
}

type StrategySampleSummary struct {
	HypothesisID uint   `json:"hypothesisId"`
	StrategyName string `json:"strategyName"`
	PoolSamples  int    `json:"poolSamples"`
	StockSamples int    `json:"stockSamples"`
	EntryMatched bool   `json:"entryMatched"`
	ExitMatched  bool   `json:"exitMatched"`
	SampleReady  bool   `json:"sampleReady"`
}

type DecisionDataStatus struct {
	FeatureRows        int64    `json:"featureRows"`
	FeatureStartDate   string   `json:"featureStartDate"`
	FeatureEndDate     string   `json:"featureEndDate"`
	FeatureAdjusted    bool     `json:"featureAdjusted"`
	MoneyFlowRows      int64    `json:"moneyFlowRows"`
	MoneyFlowStartDate string   `json:"moneyFlowStartDate"`
	MoneyFlowEndDate   string   `json:"moneyFlowEndDate"`
	MoneyFlowReady     bool     `json:"moneyFlowReady"`
	Warnings           []string `json:"warnings"`
}

type PredictionAlert struct {
	Key             string    `json:"key"`
	SessionID       uint      `json:"sessionId"`
	DecisionID      uint      `json:"decisionId"`
	StockCode       string    `json:"stockCode"`
	StockName       string    `json:"stockName"`
	Level           string    `json:"level"`
	Title           string    `json:"title"`
	Message         string    `json:"message"`
	Reason          string    `json:"reason"`
	SuggestedAction string    `json:"suggestedAction"`
	Scene           string    `json:"scene"`
	MonitorMode     string    `json:"monitorMode"`
	Price           float64   `json:"price"`
	ThresholdPrice  float64   `json:"thresholdPrice"`
	CreatedAt       time.Time `json:"createdAt"`
}
