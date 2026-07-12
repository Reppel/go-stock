package backtest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	CandidateSchemaVersion = "candidate-snapshot/v1"
	CandidateSourceVersion = "five-source/v1"
	CandidateScorerVersion = "scene-router/v1"
)

var candidateSourceOrder = []string{"recommendation", "event", "uplimit", "pattern", "indicator"}

type CandidateGenerateRequest struct {
	Name           string                 `json:"name"`
	Scene          string                 `json:"scene"`
	StockScope     string                 `json:"stockScope"`
	StockCodes     []string               `json:"stockCodes"`
	SourceItems    []CandidateSourceInput `json:"sourceItems"`
	TradeDate      string                 `json:"tradeDate"`
	Sources        []string               `json:"sources"`
	SourceMode     string                 `json:"sourceMode"`
	MinimumSources int                    `json:"minimumSources"`
	Limit          int                    `json:"limit"`
	IndicatorQuery string                 `json:"indicatorQuery"`
	MinAmount      float64                `json:"minAmount"`
	Force          bool                   `json:"force"`
}

type CandidateSourceInput struct {
	StockCode string `json:"stockCode"`
	StockName string `json:"stockName"`
	Rank      int    `json:"rank"`
	RawJSON   string `json:"rawJson"`
}

type CandidateInvestmentAdvice struct {
	Action             string   `json:"action"`
	Status             string   `json:"status"`
	Scene              string   `json:"scene"`
	ReferencePrice     float64  `json:"referencePrice"`
	BuyPriceMin        float64  `json:"buyPriceMin"`
	BuyPriceMax        float64  `json:"buyPriceMax"`
	NoChasePrice       float64  `json:"noChasePrice"`
	StopLossPrice      float64  `json:"stopLossPrice"`
	TakeProfitPrice    float64  `json:"takeProfitPrice"`
	MaxHoldingDays     int      `json:"maxHoldingDays"`
	PositionCap        float64  `json:"positionCap"`
	NextExecution      string   `json:"nextExecution"`
	Reasons            []string `json:"reasons"`
	Risks              []string `json:"risks"`
	Invalidation       []string `json:"invalidation"`
	FormalAdviceReady  bool     `json:"formalAdviceReady"`
	FormalAdviceReason string   `json:"formalAdviceReason"`
}

type CandidateSnapshotDetails struct {
	Snapshot  models.CandidateSnapshot            `json:"snapshot"`
	Items     []models.CandidateSnapshotItem      `json:"items"`
	Facts     []models.CandidateSourceFact        `json:"facts"`
	Freshness map[string]CandidateSourceFreshness `json:"freshness"`
}

type CandidateSourceFreshness struct {
	Source      string    `json:"source"`
	Available   bool      `json:"available"`
	ItemCount   int       `json:"itemCount"`
	DataAsOf    time.Time `json:"dataAsOf"`
	AvailableAt time.Time `json:"availableAt"`
	Message     string    `json:"message"`
}

type sourceCandidate struct {
	StockCode      string
	StockName      string
	Industry       string
	Concept        string
	Source         string
	SourceRecordID string
	Rank           int
	Score          float64
	EventTime      time.Time
	AvailableAt    time.Time
	DataAsOf       time.Time
	Facts          map[string]any
	Reasons        []string
	Risks          []string
	Raw            any
}

type mergedCandidate struct {
	StockCode string
	StockName string
	Industry  string
	Concept   string
	Sources   map[string]sourceCandidate
	Feature   models.StockFeature
}

type CandidatePoolService struct{}

func NewCandidatePoolService() *CandidatePoolService { return &CandidatePoolService{} }

func NormalizeCandidateStockCode(raw string) string {
	code := strings.ToLower(strings.TrimSpace(raw))
	if code == "" {
		return ""
	}
	code = strings.ReplaceAll(code, "_", "")
	if dot := strings.Index(code, "."); dot >= 0 {
		left, right := code[:dot], code[dot+1:]
		if right == "sh" || right == "ss" || right == "sz" || right == "bj" {
			code = right + left
		} else {
			code = left
		}
	}
	for _, prefix := range []string{"sh", "sz", "bj"} {
		if strings.HasPrefix(code, prefix) {
			numeric := digitsOnly(strings.TrimPrefix(code, prefix))
			if len(numeric) == 6 {
				return prefix + numeric
			}
		}
	}
	numeric := digitsOnly(code)
	if len(numeric) != 6 {
		return ""
	}
	switch numeric[0] {
	case '4', '8', '9':
		return "bj" + numeric
	case '5', '6':
		return "sh" + numeric
	default:
		return "sz" + numeric
	}
}

func digitsOnly(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s *CandidatePoolService) Generate(request CandidateGenerateRequest) (*CandidateSnapshotDetails, error) {
	if db.Dao == nil {
		return nil, fmt.Errorf("数据库未初始化")
	}
	now := shanghaiNow()
	if request.TradeDate == "" {
		request.TradeDate = now.Format("2006-01-02")
	}
	request = normalizeCandidateRequest(request)
	if _, err := time.Parse("2006-01-02", request.TradeDate); err != nil {
		return nil, fmt.Errorf("候选数据日期无效: %s", request.TradeDate)
	}
	allCandidates := make([]sourceCandidate, 0, 512)
	freshness := make(map[string]CandidateSourceFreshness, len(request.Sources))
	var sourceErrors []string
	needsLocalScreening := len(request.SourceItems) == 0 && (containsString(request.Sources, "pattern") || containsString(request.Sources, "indicator"))
	var localScreeningErr error
	if needsLocalScreening {
		localScreeningErr = s.BuildScreeningFacts(request.TradeDate)
	}
	for _, source := range request.Sources {
		if localScreeningErr != nil && (source == "pattern" || source == "indicator") {
			freshness[source] = CandidateSourceFreshness{Source: source, Message: localScreeningErr.Error()}
			sourceErrors = append(sourceErrors, source+": "+localScreeningErr.Error())
			continue
		}
		rows, sourceFreshness, err := s.collectSource(source, request, now)
		freshness[source] = sourceFreshness
		if err != nil {
			sourceErrors = append(sourceErrors, source+": "+err.Error())
			continue
		}
		allCandidates = append(allCandidates, rows...)
	}
	queryJSON, _ := json.Marshal(request)
	sourceInputHash := candidateSourceInputHash(allCandidates, sourceErrors)
	snapshotKey := candidateHash(map[string]any{
		"request": json.RawMessage(queryJSON), "sourceInputHash": sourceInputHash,
		"sourceVersion": CandidateSourceVersion, "schemaVersion": CandidateSchemaVersion,
	})
	var existing models.CandidateSnapshot
	if err := db.Dao.Where("snapshot_key = ?", snapshotKey).First(&existing).Error; err == nil {
		if !request.Force {
			return s.GetSnapshot(existing.ID, now)
		}
		db.Dao.Model(&existing).Updates(map[string]any{"status": "expired", "error_message": "force regenerated"})
	} else if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	sourcesJSON, _ := json.Marshal(request.Sources)
	snapshot := models.CandidateSnapshot{
		SnapshotKey: snapshotKey, Name: request.Name, Scene: request.Scene, StockScope: request.StockScope,
		SourcesJSON: string(sourcesJSON), SourceMode: request.SourceMode, MinimumSources: request.MinimumSources,
		QueryJSON: string(queryJSON), TradeDate: request.TradeDate, AvailableAt: now, DataAsOf: now,
		SourceVersion: CandidateSourceVersion, SchemaVersion: CandidateSchemaVersion,
		FeatureVersion: CurrentFeatureVersion, RegistryVersion: CurrentIndicatorRegistry, Status: "running",
	}
	if err := db.Dao.Create(&snapshot).Error; err != nil {
		return nil, err
	}

	items, facts := s.mergeAndScore(snapshot.ID, request, allCandidates, now)
	if request.Limit > 0 && len(items) > request.Limit {
		allowed := make(map[string]bool, request.Limit)
		for _, item := range items[:request.Limit] {
			allowed[item.StockCode] = true
		}
		items = items[:request.Limit]
		filtered := facts[:0]
		for _, fact := range facts {
			if allowed[fact.StockCode] {
				filtered = append(filtered, fact)
			}
		}
		facts = filtered
	}
	status := "candidate"
	errMsg := strings.Join(sourceErrors, "；")
	if len(items) == 0 {
		status = "failed"
		if errMsg == "" {
			errMsg = "五源均未产生符合条件的候选股票"
		}
	}
	coverage := 0.0
	if len(request.Sources) > 0 {
		coverage = float64(len(request.Sources)-len(sourceErrors)) / float64(len(request.Sources))
	}
	rawHash := candidateHash(map[string]any{"items": items, "facts": facts})
	ablationJSON, _ := json.Marshal(candidateSourceAblation(request.Sources, request.MinimumSources, items, facts))
	if err := db.Dao.Transaction(func(tx *gorm.DB) error {
		if len(items) > 0 {
			if err := tx.CreateInBatches(items, 200).Error; err != nil {
				return err
			}
		}
		if len(facts) > 0 {
			if err := tx.CreateInBatches(facts, 300).Error; err != nil {
				return err
			}
		}
		return tx.Model(&models.CandidateSnapshot{}).Where("id = ?", snapshot.ID).Updates(map[string]any{
			"status": status, "error_message": errMsg, "candidate_count": len(items),
			"coverage": coverage, "raw_hash": rawHash, "ablation_json": string(ablationJSON), "data_as_of": now,
		}).Error
	}); err != nil {
		_ = db.Dao.Model(&models.CandidateSnapshot{}).Where("id = ?", snapshot.ID).Updates(map[string]any{
			"status": "failed", "error_message": err.Error(),
		}).Error
		return nil, err
	}
	snapshot.Status, snapshot.ErrorMessage, snapshot.AblationJSON = status, errMsg, string(ablationJSON)
	snapshot.CandidateCount, snapshot.Coverage, snapshot.RawHash = len(items), coverage, rawHash

	// 自动触发特征数据同步，确保五源候选股票有基本特征数据
	if len(items) > 0 {
		s.ensureCandidateFeatures(items)
	}

	return &CandidateSnapshotDetails{Snapshot: snapshot, Items: items, Facts: facts, Freshness: freshness}, nil
}

func candidateSourceInputHash(rows []sourceCandidate, sourceErrors []string) string {
	fingerprints := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		fingerprints = append(fingerprints, map[string]any{
			"source": row.Source, "stockCode": row.StockCode, "sourceRecordId": row.SourceRecordID,
			"score": row.Score, "availableAt": row.AvailableAt, "dataAsOf": row.DataAsOf,
			"rawHash": candidateHash(row.Raw),
		})
	}
	sort.Slice(fingerprints, func(i, j int) bool {
		left := fmt.Sprintf("%v|%v|%v", fingerprints[i]["source"], fingerprints[i]["stockCode"], fingerprints[i]["sourceRecordId"])
		right := fmt.Sprintf("%v|%v|%v", fingerprints[j]["source"], fingerprints[j]["stockCode"], fingerprints[j]["sourceRecordId"])
		return left < right
	})
	return candidateHash(map[string]any{"rows": fingerprints, "errors": sourceErrors})
}

func candidateSourceAblation(sources []string, minimumSources int, items []models.CandidateSnapshotItem, facts []models.CandidateSourceFact) map[string]any {
	stockSources := map[string]map[string]bool{}
	for _, fact := range facts {
		if stockSources[fact.StockCode] == nil {
			stockSources[fact.StockCode] = map[string]bool{}
		}
		stockSources[fact.StockCode][fact.Source] = true
	}
	baseline := len(items)
	result := map[string]any{
		"type": "coverage_only", "eligibleForPromotion": false,
		"baseline": baseline, "minimumSources": minimumSources, "sources": map[string]any{},
	}
	rows := result["sources"].(map[string]any)
	for _, removed := range sources {
		remaining := 0
		for _, item := range items {
			count := len(stockSources[item.StockCode])
			if stockSources[item.StockCode][removed] {
				count--
			}
			if count >= minimumSources {
				remaining++
			}
		}
		dropRatio := 0.0
		if baseline > 0 {
			dropRatio = float64(baseline-remaining) / float64(baseline)
		}
		rows[removed] = map[string]any{"remaining": remaining, "dropRatio": dropRatio}
	}
	return result
}

func normalizeCandidateRequest(request CandidateGenerateRequest) CandidateGenerateRequest {
	if strings.TrimSpace(request.Scene) == "" {
		request.Scene = "短线爆发"
	}
	if strings.TrimSpace(request.StockScope) == "" {
		request.StockScope = "全部A股"
	}
	if len(request.Sources) == 0 {
		request.Sources = append([]string{}, candidateSourceOrder...)
	}
	seen := map[string]bool{}
	normalized := make([]string, 0, len(request.Sources))
	for _, source := range request.Sources {
		source = strings.ToLower(strings.TrimSpace(source))
		if !seen[source] && containsString(candidateSourceOrder, source) {
			seen[source] = true
			normalized = append(normalized, source)
		}
	}
	request.Sources = normalized
	codeSeen := map[string]bool{}
	normalizedCodes := make([]string, 0, len(request.StockCodes))
	for _, rawCode := range request.StockCodes {
		code := NormalizeCandidateStockCode(rawCode)
		if code != "" && !codeSeen[code] {
			codeSeen[code] = true
			normalizedCodes = append(normalizedCodes, code)
		}
	}
	request.StockCodes = normalizedCodes
	normalizedItems := make([]CandidateSourceInput, 0, len(request.SourceItems))
	for index, item := range request.SourceItems {
		item.StockCode = NormalizeCandidateStockCode(item.StockCode)
		if item.StockCode == "" {
			continue
		}
		if item.Rank <= 0 {
			item.Rank = index + 1
		}
		normalizedItems = append(normalizedItems, item)
		if !codeSeen[item.StockCode] {
			codeSeen[item.StockCode] = true
			request.StockCodes = append(request.StockCodes, item.StockCode)
		}
	}
	request.SourceItems = normalizedItems
	if request.SourceMode != "intersection" && request.SourceMode != "consensus" {
		request.SourceMode = "union"
	}
	if request.SourceMode == "intersection" {
		request.MinimumSources = len(request.Sources)
	}
	if request.MinimumSources <= 0 {
		request.MinimumSources = 1
	}
	if request.MinimumSources > len(request.Sources) {
		request.MinimumSources = len(request.Sources)
	}
	if request.Limit <= 0 || request.Limit > 500 {
		request.Limit = 100
	}
	if request.Name == "" {
		request.Name = request.TradeDate + " " + request.Scene + " 五源机会池"
	}
	return request
}

func (s *CandidatePoolService) collectSource(source string, request CandidateGenerateRequest, now time.Time) ([]sourceCandidate, CandidateSourceFreshness, error) {
	var rows []sourceCandidate
	var err error
	switch source {
	case "recommendation":
		rows, err = s.collectRecommendations(request, now)
	case "event":
		rows, err = s.collectEvents(request, now)
	case "uplimit":
		rows, err = s.collectUplimit(request, now)
	case "pattern", "indicator":
		if len(request.SourceItems) > 0 {
			rows, err = s.captureScreeningExecution(source, request, now)
		} else {
			rows, err = s.collectScreening(source, request, now)
		}
	default:
		err = fmt.Errorf("不支持的数据源")
	}
	fresh := CandidateSourceFreshness{Source: source, Available: err == nil && len(rows) > 0, ItemCount: len(rows)}
	for _, row := range rows {
		if row.DataAsOf.After(fresh.DataAsOf) {
			fresh.DataAsOf = row.DataAsOf
		}
		if row.AvailableAt.After(fresh.AvailableAt) {
			fresh.AvailableAt = row.AvailableAt
		}
	}
	if err != nil {
		fresh.Message = err.Error()
	} else if len(rows) == 0 {
		fresh.Message = "当前条件没有候选"
	} else {
		fresh.Message = fmt.Sprintf("已取得%d只候选", len(rows))
	}
	return rows, fresh, err
}

func (s *CandidatePoolService) collectRecommendations(request CandidateGenerateRequest, now time.Time) ([]sourceCandidate, error) {
	end, _ := time.ParseInLocation("2006-01-02", request.TradeDate, shanghaiLocation())
	end = end.Add(24*time.Hour - time.Nanosecond)
	start := end.AddDate(0, 0, -30)
	var recommendations []models.AiRecommendStocks
	if err := db.Dao.Where("data_time >= ? AND data_time <= ?", start, end).Order("data_time desc").Find(&recommendations).Error; err != nil {
		return nil, err
	}
	rows := make([]sourceCandidate, 0, len(recommendations))
	for rank, recommendation := range recommendations {
		code := NormalizeCandidateStockCode(recommendation.StockCode)
		if code == "" || recommendation.DataTime == nil {
			continue
		}
		ratingScore := ratingCandidateScore(recommendation.Rating)
		refPrice := parseCandidateFloat(recommendation.StockClosePrice)
		if refPrice <= 0 {
			refPrice = parseCandidateFloat(recommendation.StockPrice)
		}
		stopLoss := parseCandidateFloat(recommendation.RecommendStopLossPrice)
		availableAt := recommendation.DataTime.In(shanghaiLocation())
		hash := candidateHash(map[string]any{"id": recommendation.ID, "model": recommendation.ModelName, "code": code, "time": availableAt})
		event := models.ModelRecommendationEvent{
			RecommendationID: recommendation.ID, SourceRecordID: strconv.FormatUint(uint64(recommendation.ID), 10),
			StockCode: code, Source: "ai_recommend_stocks", TradeDate: availableAt.Format("2006-01-02"),
			Scene: request.Scene, Horizon: sceneHorizon(request.Scene), ModelName: recommendation.ModelName,
			ModelVersion: "unknown", GenerationAuditID: 0, LineageStatus: "unverified", IndependentForResearch: false,
			Rating: recommendation.Rating, ReferencePrice: refPrice,
			BuyPriceMin: recommendation.RecommendBuyPriceMin, BuyPriceMax: recommendation.RecommendBuyPriceMax,
			StopLossPrice: stopLoss, TakeProfitPrice: recommendation.RecommendStopProfitPriceMin,
			AvailableAt: availableAt, DataAsOf: availableAt, FeatureVersion: CurrentFeatureVersion,
			RecommendationHash: hash, ValidationStatus: "candidate",
		}
		db.Dao.Where("source_record_id = ? AND stock_code = ? AND source = ?",
			event.SourceRecordID, event.StockCode, event.Source).Delete(&models.ModelRecommendationEvent{})
		_ = db.Dao.Create(&event).Error
		facts := map[string]any{
			"modelName": recommendation.ModelName, "rating": recommendation.Rating,
			"referencePrice": refPrice, "buyPriceMin": recommendation.RecommendBuyPriceMin,
			"buyPriceMax": recommendation.RecommendBuyPriceMax,
		}
		rows = append(rows, sourceCandidate{
			StockCode: code, StockName: recommendation.StockName, Industry: recommendation.BkName,
			Source: "recommendation", SourceRecordID: strconv.FormatUint(uint64(recommendation.ID), 10), Rank: rank + 1,
			Score: ratingScore, EventTime: availableAt, AvailableAt: availableAt, DataAsOf: availableAt,
			Facts: facts, Reasons: []string{fmt.Sprintf("%s模型给出%s评级", recommendation.ModelName, recommendation.Rating)},
			Risks: []string{"AI推荐仅作为弱证据，不作为收益标签"}, Raw: map[string]any{"id": recommendation.ID, "reason": recommendation.RecommendReason},
		})
	}
	_ = now
	return rows, nil
}

func (s *CandidatePoolService) collectEvents(request CandidateGenerateRequest, now time.Time) ([]sourceCandidate, error) {
	var events []models.StockChangeHistory
	if err := db.Dao.Where("change_date = ? AND created_at <= ?", request.TradeDate, now).Order("change_time asc").Find(&events).Error; err != nil {
		return nil, err
	}
	byStock := map[string][]models.StockChangeHistory{}
	for _, event := range events {
		code := NormalizeCandidateStockCode(event.StockCode)
		if code != "" {
			byStock[code] = append(byStock[code], event)
		}
	}
	rows := make([]sourceCandidate, 0, len(byStock))
	for code, stockEvents := range byStock {
		facts := summarizeStockEvents(stockEvents)
		bullish := intFromAny(facts["bullishCount"])
		bearish := intFromAny(facts["bearishCount"])
		score := clamp(45+float64(bullish)*7-float64(bearish)*8, 0, 100)
		if bullish == 0 && bearish > 0 {
			continue
		}
		last := stockEvents[len(stockEvents)-1]
		eventTime := parseShanghaiEventTime(request.TradeDate, last.ChangeTime, now)
		availableAt := maxStockEventCreatedAt(stockEvents, eventTime)
		reasons := []string{fmt.Sprintf("当日%d次利好异动、%d次利空异动", bullish, bearish)}
		risks := []string{}
		if bearish > 0 {
			risks = append(risks, "同时存在利空异动，需等待量化确认")
		}
		rows = append(rows, sourceCandidate{
			StockCode: code, StockName: last.StockName, Industry: last.Industry, Concept: last.Concept,
			Source: "event", SourceRecordID: request.TradeDate + "-" + code, Score: score,
			EventTime: eventTime, AvailableAt: availableAt, DataAsOf: eventTime,
			Facts: facts, Reasons: reasons, Risks: risks, Raw: stockEvents,
		})
	}
	sortSourceCandidates(rows)
	for i := range rows {
		rows[i].Rank = i + 1
	}
	if err := s.AggregateStockEvents(request.TradeDate, now); err != nil {
		logger.SugaredLogger.Warnf("aggregate stock events %s: %v", request.TradeDate, err)
	}
	return rows, nil
}

func summarizeStockEvents(events []models.StockChangeHistory) map[string]any {
	bullishTypes := map[int]bool{4: true, 16: true, 32: true, 64: true, 8193: true, 8201: true, 8202: true, 8207: true, 8209: true, 8211: true, 8213: true, 8215: true}
	bearishTypes := map[int]bool{8: true, 128: true, 8194: true, 8203: true, 8204: true, 8208: true, 8210: true, 8212: true, 8214: true, 8216: true}
	bullish, bearish, largeBuy, largeSell, rapidRise, rapidFall, limitUp, limitDown := 0, 0, 0, 0, 0, 0, 0, 0
	amount := 0.0
	for _, event := range events {
		if bullishTypes[event.ChangeType] {
			bullish++
		}
		if bearishTypes[event.ChangeType] {
			bearish++
		}
		switch event.ChangeType {
		case 8193:
			largeBuy++
		case 8194:
			largeSell++
		case 8201, 8202:
			rapidRise++
		case 8203, 8204:
			rapidFall++
		case 4:
			limitUp++
		case 8:
			limitDown++
		}
		amount += event.Amount
	}
	return map[string]any{
		"eventCount": len(events), "bullishCount": bullish, "bearishCount": bearish,
		"largeBuyCount": largeBuy, "largeSellCount": largeSell, "rapidRiseCount": rapidRise,
		"rapidFallCount": rapidFall, "limitUpCount": limitUp, "limitDownCount": limitDown, "eventAmount": amount,
	}
}

func (s *CandidatePoolService) AggregateStockEvents(tradeDate string, decisionAsOf time.Time) error {
	var events []models.StockChangeHistory
	query := db.Dao.Where("change_date = ?", tradeDate)
	if !decisionAsOf.IsZero() {
		query = query.Where("created_at <= ?", decisionAsOf)
	}
	if err := query.Find(&events).Error; err != nil {
		return err
	}
	byStock := map[string][]models.StockChangeHistory{}
	for _, event := range events {
		code := NormalizeCandidateStockCode(event.StockCode)
		if code != "" {
			byStock[code] = append(byStock[code], event)
		}
	}
	rows := make([]models.StockEventDaily, 0, len(byStock))
	for code, stockEvents := range byStock {
		facts := summarizeStockEvents(stockEvents)
		latestEventTime := time.Time{}
		for _, event := range stockEvents {
			at := parseShanghaiEventTime(tradeDate, event.ChangeTime, decisionAsOf)
			if at.After(latestEventTime) {
				latestEventTime = at
			}
		}
		availableAt := maxStockEventCreatedAt(stockEvents, latestEventTime)
		rows = append(rows, models.StockEventDaily{
			StockCode: code, TradeDate: tradeDate, FeatureVersion: CurrentFeatureVersion,
			DataAsOf: latestEventTime, AvailableAt: availableAt,
			ChangeEventCount: intFromAny(facts["eventCount"]), HasLargeBuy: intFromAny(facts["largeBuyCount"]) > 0,
			HasLargeSell: intFromAny(facts["largeSellCount"]) > 0, HasLimitUp: intFromAny(facts["limitUpCount"]) > 0,
			HasLimitDown: intFromAny(facts["limitDownCount"]) > 0, HasRapidRise: intFromAny(facts["rapidRiseCount"]) > 0,
			HasRapidFall: intFromAny(facts["rapidFallCount"]) > 0, Source: "stock_change_history/v1",
		})
	}
	if len(rows) == 0 {
		return nil
	}
	// 先删后插，避免 ON CONFLICT 依赖唯一索引（旧数据库可能缺索引）
	for _, row := range rows {
		db.Dao.Where("stock_code = ? AND trade_date = ? AND feature_version = ?",
			row.StockCode, row.TradeDate, row.FeatureVersion).Delete(&models.StockEventDaily{})
	}
	return db.Dao.CreateInBatches(rows, 200).Error
}

func maxStockEventCreatedAt(events []models.StockChangeHistory, fallback time.Time) time.Time {
	result := time.Time{}
	for _, event := range events {
		if event.CreatedAt.After(result) {
			result = event.CreatedAt
		}
	}
	if result.IsZero() {
		return fallback
	}
	return result
}

func (s *CandidatePoolService) BuildScreeningFacts(tradeDate string) error {
	repo := NewFeatureRepositoryForVersion(CurrentFeatureVersion)
	features := repo.GetByDate(tradeDate, nil)
	if len(features) == 0 {
		return fmt.Errorf("%s没有当前版本特征", tradeDate)
	}
	var previousDate string
	_ = db.Dao.Model(&models.StockFeature{}).Where("date < ? AND feature_version = ? AND adjusted = ?", tradeDate, CurrentFeatureVersion, true).
		Select("MAX(date)").Scan(&previousDate).Error
	previousMap := stockFeatureMap(repo.GetByDate(previousDate, nil))
	rows := make([]models.StockScreeningFactDaily, 0, len(features))
	for _, feature := range features {
		previous, hasPrevious := previousMap[feature.StockCode]
		conditions := make([]string, 0, 8)
		macdGolden := hasPrevious && previous.MACD <= 0 && feature.MACD > 0
		maBullish := feature.MA5 > feature.MA20 && feature.MA20 > feature.MA60 && feature.MA60 > 0
		maBearish := feature.MA5 < feature.MA20 && feature.MA20 < feature.MA60 && feature.MA60 > 0
		breakMA20 := hasPrevious && previous.Close <= previous.MA20 && feature.Close > feature.MA20 && feature.MA20 > 0
		bollBreakout := hasPrevious && previous.Close <= previous.BOLLUpper && feature.Close > feature.BOLLUpper && feature.BOLLUpper > 0
		volumeBreakout := feature.VolumeRatio >= 1.5
		oversold := feature.RSI6 > 0 && feature.RSI6 < 30
		overbought := feature.RSI6 > 70
		for name, matched := range map[string]bool{
			"MACD_GOLDEN_FORK": macdGolden, "MA_BULLISH": maBullish, "BREAK_MA20": breakMA20,
			"BOLL_BREAKOUT": bollBreakout, "VOLUME_BREAKOUT": volumeBreakout, "RSI_OVERSOLD": oversold,
		} {
			if matched {
				conditions = append(conditions, name)
			}
		}
		sort.Strings(conditions)
		conditionsJSON, _ := json.Marshal(conditions)
		dataAsOf := feature.DataAsOf
		if dataAsOf.IsZero() {
			dataAsOf = featureDataAsOf(tradeDate)
		}
		rows = append(rows, models.StockScreeningFactDaily{
			StockCode: feature.StockCode, TradeDate: tradeDate, FeatureVersion: CurrentFeatureVersion,
			DataAsOf: dataAsOf, AvailableAt: dataAsOf, Calculated: true, Coverage: boolCoverage(hasPrevious), Status: "ready",
			MACDGoldenCross: macdGolden, MACDAboveZero: feature.MACD > 0, KDJGoldenCross: false,
			MABullish: maBullish, MABearish: maBearish, BreakMA20: breakMA20, BollBreakout: bollBreakout,
			VolumeBreakout: volumeBreakout, Oversold: oversold, Overbought: overbought,
			PatternCount: len(conditions), ConditionsJSON: string(conditionsJSON), Source: "local_stock_feature/v1",
		})
	}
	// 先删后插，避免 ON CONFLICT 依赖唯一索引
	for _, row := range rows {
		db.Dao.Where("stock_code = ? AND trade_date = ? AND feature_version = ?",
			row.StockCode, row.TradeDate, row.FeatureVersion).Delete(&models.StockScreeningFactDaily{})
	}
	return db.Dao.CreateInBatches(rows, 300).Error
}

func boolCoverage(hasPrevious bool) float64 {
	if hasPrevious {
		return 1
	}
	return 0.5
}

func (s *CandidatePoolService) collectScreening(source string, request CandidateGenerateRequest, now time.Time) ([]sourceCandidate, error) {
	var facts []models.StockScreeningFactDaily
	if err := db.Dao.Where("trade_date = ? AND feature_version = ? AND calculated = ?", request.TradeDate, CurrentFeatureVersion, true).Find(&facts).Error; err != nil {
		return nil, err
	}
	rows := make([]sourceCandidate, 0, len(facts))
	for _, fact := range facts {
		matched := false
		score := 0.0
		reasons := []string{}
		if source == "pattern" {
			matched = fact.BollBreakout || fact.VolumeBreakout || fact.BreakMA20 || fact.MABullish
			score = clamp(40+float64(fact.PatternCount)*10, 0, 100)
			if matched {
				reasons = append(reasons, fmt.Sprintf("本地可复现形态命中%d项", fact.PatternCount))
			}
		} else {
			matched = fact.MACDGoldenCross || fact.Oversold || (fact.MACDAboveZero && fact.MABullish)
			score = 45
			if fact.MACDGoldenCross {
				score += 20
				reasons = append(reasons, "MACD零轴金叉")
			}
			if fact.Oversold {
				score += 15
				reasons = append(reasons, "RSI超卖")
			}
			if fact.MABullish {
				score += 10
				reasons = append(reasons, "均线多头")
			}
		}
		if !matched {
			continue
		}
		var conditions []string
		_ = json.Unmarshal([]byte(fact.ConditionsJSON), &conditions)
		rows = append(rows, sourceCandidate{
			StockCode: fact.StockCode, StockName: stockNameOrCode(fact.StockCode, ""), Source: source,
			SourceRecordID: strconv.FormatUint(uint64(fact.ID), 10), Score: clamp(score, 0, 100),
			EventTime: fact.DataAsOf, AvailableAt: fact.AvailableAt, DataAsOf: fact.DataAsOf,
			Facts:   map[string]any{"conditions": conditions, "patternCount": fact.PatternCount, "coverage": fact.Coverage},
			Reasons: reasons, Risks: []string{}, Raw: fact,
		})
	}
	sortSourceCandidates(rows)
	for i := range rows {
		rows[i].Rank = i + 1
	}
	_ = now
	return rows, nil
}

func (s *CandidatePoolService) captureScreeningExecution(source string, request CandidateGenerateRequest, now time.Time) ([]sourceCandidate, error) {
	if source != "pattern" && source != "indicator" {
		return nil, fmt.Errorf("unsupported screening execution source: %s", source)
	}
	contentHash := candidateHash(map[string]any{
		"source": source, "query": request.IndicatorQuery, "tradeDate": request.TradeDate,
		"items": request.SourceItems,
	})
	snapshot := models.ScreeningExecutionSnapshot{
		SnapshotKey: contentHash, Source: source, Query: request.IndicatorQuery,
		NormalizedQuery: strings.TrimSpace(strings.ToLower(request.IndicatorQuery)),
		TradeDate:       request.TradeDate, AvailableAt: now, RawHash: contentHash,
		ResultCount: len(request.SourceItems), Status: "captured",
	}
	if err := db.Dao.Where("snapshot_key = ?", contentHash).FirstOrCreate(&snapshot).Error; err != nil {
		return nil, err
	}
	items := make([]models.ScreeningExecutionItem, 0, len(request.SourceItems))
	for _, input := range request.SourceItems {
		rawJSON := input.RawJSON
		if strings.TrimSpace(rawJSON) == "" {
			raw, _ := json.Marshal(map[string]any{"stockCode": input.StockCode, "stockName": input.StockName, "rank": input.Rank})
			rawJSON = string(raw)
		}
		items = append(items, models.ScreeningExecutionItem{
			SnapshotID: snapshot.ID, StockCode: input.StockCode, StockName: input.StockName,
			SourceRank: input.Rank, RawHash: candidateHash(json.RawMessage(rawJSON)), RawJSON: rawJSON,
			AvailableAt: snapshot.AvailableAt,
		})
	}
	if len(items) > 0 {
		if err := db.Dao.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(items, 200).Error; err != nil {
			return nil, err
		}
	}
	var stored []models.ScreeningExecutionItem
	if err := db.Dao.Where("snapshot_id = ?", snapshot.ID).Order("source_rank asc, stock_code asc").Find(&stored).Error; err != nil {
		return nil, err
	}
	features := candidateFeatureMap(request.TradeDate)
	rows := make([]sourceCandidate, 0, len(stored))
	for _, item := range stored {
		feature := features[item.StockCode]
		dataAsOf := feature.DataAsOf
		if dataAsOf.IsZero() {
			dataAsOf = featureDataAsOf(request.TradeDate)
		}
		score := clamp(65-float64(item.SourceRank-1)*0.25, 40, 65)
		facts := map[string]any{
			"executionSnapshotId": snapshot.ID, "query": snapshot.Query,
			"normalizedQuery": snapshot.NormalizedQuery, "sourceRank": item.SourceRank,
		}
		rows = append(rows, sourceCandidate{
			StockCode: item.StockCode, StockName: item.StockName, Source: source,
			SourceRecordID: fmt.Sprintf("%d:%d", snapshot.ID, item.ID), Rank: item.SourceRank, Score: score,
			EventTime: snapshot.AvailableAt, AvailableAt: snapshot.AvailableAt, DataAsOf: dataAsOf,
			Facts: facts, Reasons: []string{fmt.Sprintf("%s页面结果排名第%d", sourceLabelCN(source), item.SourceRank)},
			Risks: []string{"页面来源为候选证据，尚未进入本地 DSL"}, Raw: json.RawMessage(item.RawJSON),
		})
	}
	return rows, nil
}

func sourceLabelCN(source string) string {
	if source == "pattern" {
		return "形态选股"
	}
	if source == "indicator" {
		return "指标选股"
	}
	return source
}

func (s *CandidatePoolService) collectUplimit(request CandidateGenerateRequest, now time.Time) ([]sourceCandidate, error) {
	if err := s.SyncUplimitSnapshot(request.TradeDate, now); err != nil {
		return nil, err
	}
	var latestKey string
	if err := db.Dao.Model(&models.UplimitStockDaily{}).
		Where("trade_date = ? AND available_at <= ?", request.TradeDate, now).
		Order("available_at desc").Limit(1).Pluck("snapshot_key", &latestKey).Error; err != nil {
		return nil, err
	}
	if latestKey == "" {
		return nil, nil
	}
	var limitRows []models.UplimitStockDaily
	if err := db.Dao.Where("snapshot_key = ? AND available_at <= ?", latestKey, now).Find(&limitRows).Error; err != nil {
		return nil, err
	}
	merged := map[string]sourceCandidate{}
	for _, row := range limitRows {
		code := NormalizeCandidateStockCode(row.StockCode)
		if code == "" {
			continue
		}
		score := 45 + float64(row.KeepTimes)*9 + math.Min(row.SealRatioClose, 20)
		if row.Exploded {
			score -= 25
		}
		candidate := sourceCandidate{
			StockCode: code, StockName: row.StockName, Industry: row.PlateName, Source: "uplimit",
			SourceRecordID: strconv.FormatUint(uint64(row.ID), 10), Score: clamp(score, 0, 100),
			EventTime: row.EventTime, AvailableAt: row.AvailableAt, DataAsOf: row.DataAsOf,
			Facts: map[string]any{
				"keepTimes": row.KeepTimes, "limitType": row.LimitType, "limitTime": row.LimitTime,
				"sealRatioMax": row.SealRatioMax, "sealRatioClose": row.SealRatioClose,
				"exploded": row.Exploded, "explodeCount": row.ExplodeCount, "plateHeat": row.PlateHeat,
				"plateLimitCount": row.PlateLimitCount, "plateExplodeCount": row.PlateExplodeCount,
			},
			Reasons: []string{fmt.Sprintf("%d板，板块%s", row.KeepTimes, row.PlateName)}, Raw: row,
		}
		if row.Exploded {
			candidate.Risks = append(candidate.Risks, "存在炸板记录")
		}
		current, exists := merged[code]
		if !exists || candidate.Score > current.Score {
			merged[code] = candidate
		}
	}
	rows := make([]sourceCandidate, 0, len(merged))
	for _, row := range merged {
		rows = append(rows, row)
	}
	sortSourceCandidates(rows)
	for i := range rows {
		rows[i].Rank = i + 1
	}
	return rows, nil
}

func (s *CandidatePoolService) SyncUplimitSnapshot(tradeDate string, now time.Time) error {
	result := data.NewMarketNewsApi().GetUplimitHot(tradeDate, 20)
	if intFromAny(result["code"]) != 20000 {
		return fmt.Errorf("涨停梯队接口不可用: %s", stringFromAny(result["message"]))
	}
	dataMap, ok := result["data"].(map[string]any)
	if !ok {
		return fmt.Errorf("涨停梯队响应格式异常")
	}
	raw, _ := json.Marshal(result)
	snapshotKey := candidateHash(map[string]any{"tradeDate": tradeDate, "sourceVersion": CandidateSourceVersion, "rawHash": candidateHash(json.RawMessage(raw))})
	plateNames, plateHeat := uplimitPlateMetadata(dataMap)
	rows := parseUplimitRows(dataMap, tradeDate, snapshotKey, now, plateNames, plateHeat, string(raw))
	if len(rows) == 0 {
		return nil
	}
	return db.Dao.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 200).Error
}

func uplimitPlateMetadata(dataMap map[string]any) (map[string]string, map[string]float64) {
	names := map[string]string{}
	heat := map[string]float64{}
	plateRows, _ := dataMap["plate"].([]any)
	for _, raw := range plateRows {
		values, _ := raw.([]any)
		if len(values) < 3 {
			continue
		}
		name, code := stringFromAny(values[0]), stringFromAny(values[1])
		names[code], heat[code] = name, floatFromAny(values[2])
	}
	return names, heat
}

func parseUplimitRows(dataMap map[string]any, tradeDate, snapshotKey string, now time.Time, plateNames map[string]string, plateHeat map[string]float64, rawJSON string) []models.UplimitStockDaily {
	result := make([]models.UplimitStockDaily, 0, 100)
	for _, sourceKey := range []string{"plate_stocks", "plate_stocks_zb"} {
		plates, _ := dataMap[sourceKey].(map[string]any)
		for plateCode, rawStocks := range plates {
			stocks, _ := rawStocks.([]any)
			for _, rawStock := range stocks {
				stock, _ := rawStock.(map[string]any)
				code := NormalizeCandidateStockCode(stringFromAny(stock["stock_code"]))
				if code == "" {
					continue
				}
				rowRaw, _ := json.Marshal(stock)
				limitTime := stringFromAny(stock["up_limit_time"])
				eventTime := parseShanghaiEventTime(tradeDate, limitTime, now)
				result = append(result, models.UplimitStockDaily{
					SnapshotKey: snapshotKey, StockCode: code, StockName: stringFromAny(stock["stock_name"]),
					TradeDate: tradeDate, PlateCode: plateCode, PlateName: plateNames[plateCode],
					KeepTimes: intFromAny(stock["up_limit_keep_times"]), LimitType: stringFromAny(stock["up_limit_type"]),
					LimitDescription: stringFromAny(stock["up_limit_desc"]), LimitTime: limitTime,
					FirstLimitTime: firstNonBlank(stringFromAny(stock["first_up_limit_time"]), limitTime),
					FinalLimitTime: firstNonBlank(stringFromAny(stock["final_up_limit_time"]), limitTime),
					SealRatioMax:   floatFromAny(stock["fd_max"]), SealRatioClose: floatFromAny(stock["fd_close"]),
					Exploded: sourceKey == "plate_stocks_zb", ExplodeCount: intFromAny(stock["explode_count"]),
					Amount: floatFromAny(stock["amount"]), MarketCap: floatFromAny(stock["market_c"]),
					PlateHeat: plateHeat[plateCode], PlateLimitCount: len(stocks), EventTime: eventTime,
					IngestedAt: now, AvailableAt: now, DataAsOf: now, Source: "zizizaizai_uplimit",
					SourceVersion: "v3/open/review/uplimit/hot", RawHash: candidateHash(stock), RawJSON: string(rowRaw),
				})
			}
		}
	}
	_ = rawJSON
	return result
}

func (s *CandidatePoolService) mergeAndScore(snapshotID uint, request CandidateGenerateRequest, rows []sourceCandidate, now time.Time) ([]models.CandidateSnapshotItem, []models.CandidateSourceFact) {
	allowed := candidateScopeSet(request.StockScope)
	explicit := make(map[string]bool, len(request.StockCodes))
	for _, code := range request.StockCodes {
		explicit[code] = true
	}
	merged := map[string]*mergedCandidate{}
	facts := make([]models.CandidateSourceFact, 0, len(rows))
	for _, row := range rows {
		code := NormalizeCandidateStockCode(row.StockCode)
		if code == "" || (len(allowed) > 0 && !allowed[code]) || (len(explicit) > 0 && !explicit[code]) {
			continue
		}
		candidate := merged[code]
		if candidate == nil {
			candidate = &mergedCandidate{StockCode: code, StockName: row.StockName, Industry: row.Industry, Concept: row.Concept, Sources: map[string]sourceCandidate{}}
			merged[code] = candidate
		}
		current, exists := candidate.Sources[row.Source]
		if !exists || row.Score > current.Score {
			candidate.Sources[row.Source] = row
		}
		if candidate.StockName == "" {
			candidate.StockName = row.StockName
		}
		factsJSON, _ := json.Marshal(row.Facts)
		rawJSON, _ := json.Marshal(row.Raw)
		facts = append(facts, models.CandidateSourceFact{
			SnapshotID: snapshotID, StockCode: code, Source: row.Source, SourceRecordID: firstNonBlank(row.SourceRecordID, candidateHash(row.Raw)),
			SourceRank: row.Rank, SourceScore: row.Score, EventTime: row.EventTime, IngestedAt: now,
			AvailableAt: row.AvailableAt, DataAsOf: row.DataAsOf, SourceVersion: CandidateSourceVersion,
			FactsJSON: string(factsJSON), RawHash: candidateHash(row.Raw), RawJSON: string(rawJSON),
		})
	}
	featureMap := candidateFeatureMap(request.TradeDate)
	items := make([]models.CandidateSnapshotItem, 0, len(merged))
	for code, candidate := range merged {
		if len(candidate.Sources) < request.MinimumSources {
			continue
		}
		candidate.Feature = featureMap[code]
		if request.MinAmount > 0 && candidate.Feature.Turnover < request.MinAmount {
			continue
		}
		scores := sceneScores(*candidate)
		bestScene := bestCandidateScene(scores)
		sourceNames := make([]string, 0, len(candidate.Sources))
		reasons, risks := make([]string, 0), make([]string, 0)
		factsBySource := map[string]any{}
		availableAt, dataAsOf := time.Time{}, time.Time{}
		sourceScore := 0.0
		for source, sourceRow := range candidate.Sources {
			sourceNames = append(sourceNames, source)
			reasons = append(reasons, sourceRow.Reasons...)
			risks = append(risks, sourceRow.Risks...)
			factsBySource[source] = sourceRow.Facts
			sourceScore += sourceRow.Score
			if sourceRow.AvailableAt.After(availableAt) {
				availableAt = sourceRow.AvailableAt
			}
			if sourceRow.DataAsOf.After(dataAsOf) {
				dataAsOf = sourceRow.DataAsOf
			}
		}
		sort.Strings(sourceNames)
		sourceScore /= float64(len(sourceNames))
		advice := candidateAdvice(request.Scene, bestScene, scores, candidate.Feature, reasons, risks)
		sourcesJSON, _ := json.Marshal(sourceNames)
		matchedFacts, _ := json.Marshal(factsBySource)
		reasonsJSON, _ := json.Marshal(uniqueStrings(reasons))
		risksJSON, _ := json.Marshal(uniqueStrings(risks))
		adviceJSON, _ := json.Marshal(advice)
		inputFingerprint := candidateHash(map[string]any{"sources": factsBySource, "feature": candidate.Feature, "scorer": CandidateScorerVersion})
		items = append(items, models.CandidateSnapshotItem{
			SnapshotID: snapshotID, StockCode: code, StockName: stockNameOrCode(code, candidate.StockName),
			Industry: candidate.Industry, Concept: candidate.Concept, SourceScore: sourceScore,
			SourceCount: len(sourceNames), SourcesJSON: string(sourcesJSON), BestScene: bestScene,
			ShortScore: scores["短线爆发"], SwingScore: scores["波段反弹"], TrendScore: scores["趋势持有"], LongTermScore: scores["长期配置"],
			MatchedFacts: string(matchedFacts), ReasonsJSON: string(reasonsJSON), RisksJSON: string(risksJSON), AdviceJSON: string(adviceJSON),
			AvailableAt: availableAt, DataAsOf: dataAsOf, FeatureVersion: CurrentFeatureVersion,
			ScorerVersion: CandidateScorerVersion, InputFingerprint: inputFingerprint,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := candidateSceneScore(items[i], request.Scene), candidateSceneScore(items[j], request.Scene)
		if left == right {
			if items[i].SourceCount == items[j].SourceCount {
				return items[i].StockCode < items[j].StockCode
			}
			return items[i].SourceCount > items[j].SourceCount
		}
		return left > right
	})
	for i := range items {
		items[i].SourceRank = i + 1
	}
	return items, facts
}

func sceneScores(candidate mergedCandidate) map[string]float64 {
	sourceBase := math.Min(float64(len(candidate.Sources))*10, 40)
	short, swing, trend := 25+sourceBase, 25+sourceBase*0.7, 25+sourceBase*0.6
	if event, ok := candidate.Sources["event"]; ok {
		short += (event.Score - 50) * 0.35
		swing += (event.Score - 50) * 0.15
	}
	if limit, ok := candidate.Sources["uplimit"]; ok {
		short += (limit.Score - 40) * 0.45
		trend += (limit.Score - 50) * 0.10
	}
	if pattern, ok := candidate.Sources["pattern"]; ok {
		short += (pattern.Score - 50) * 0.20
		trend += (pattern.Score - 50) * 0.25
	}
	if indicator, ok := candidate.Sources["indicator"]; ok {
		short += (indicator.Score - 50) * 0.15
		swing += (indicator.Score - 50) * 0.30
		trend += (indicator.Score - 50) * 0.30
	}
	f := candidate.Feature
	if f.Close > 0 {
		if f.VolumeRatio >= 1.5 {
			short += 8
		}
		if f.RSI6 > 0 && f.RSI6 < 30 {
			swing += 15
		}
		if f.MA20 > f.MA60 && f.Close > f.MA20 {
			trend += 15
		}
		if f.ATR/f.Close > 0.06 {
			short -= 5
			trend -= 12
		}
	}
	// The five sources do not contain point-in-time fundamentals. Long-term is
	// deliberately unavailable rather than represented as a misleading zero.
	return map[string]float64{
		"短线爆发": clamp(short, 0, 100), "波段反弹": clamp(swing, 0, 100),
		"趋势持有": clamp(trend, 0, 100), "长期配置": -1,
	}
}

func candidateAdvice(requestedScene, bestScene string, scores map[string]float64, feature models.StockFeature, reasons, risks []string) CandidateInvestmentAdvice {
	scene := requestedScene
	if scene == "" {
		scene = bestScene
	}
	advice := CandidateInvestmentAdvice{
		Action: "WATCH", Status: "candidate_only", Scene: scene, ReferencePrice: feature.Close,
		MaxHoldingDays: sceneHorizon(scene), PositionCap: scenePositionCap(scene), NextExecution: "下一交易日开盘，且仅在量化回测和裁决通过后",
		Reasons: uniqueStrings(reasons), Risks: uniqueStrings(risks), FormalAdviceReady: false,
		FormalAdviceReason: "候选源只能生成观察建议，必须继续通过四策略回测、量化裁决和前向验证",
		Invalidation:       []string{"数据过期或覆盖率不足", "次日停牌或涨停无法成交", "开盘价超出允许区间", "市场环境与场景不匹配"},
	}
	if scene == "长期配置" {
		advice.Action = "AVOID"
		advice.Status = "insufficient_data"
		advice.FormalAdviceReason = "缺少点时财务质量、估值、盈利增长和长期风险数据"
		advice.Risks = append(advice.Risks, "五源数据不足以判断长期投资价值")
		return advice
	}
	if feature.Close <= 0 {
		advice.Status = "missing_price"
		advice.FormalAdviceReason = "缺少当前版本前复权收盘特征"
		return advice
	}
	atrRate := 0.04
	if feature.ATR > 0 {
		atrRate = clamp(feature.ATR/feature.Close*1.5, 0.03, 0.10)
	}
	advice.BuyPriceMin = feature.Close * 0.98
	advice.BuyPriceMax = feature.Close * 1.02
	advice.NoChasePrice = feature.Close * 1.05
	advice.StopLossPrice = feature.Close * (1 - atrRate)
	advice.TakeProfitPrice = feature.Close * (1 + sceneTargetReturn(scene))
	if scores[scene] < 55 {
		advice.Risks = append(advice.Risks, "当前场景适配分不足55，仅保留观察")
	}
	return advice
}

func candidateSceneScore(item models.CandidateSnapshotItem, scene string) float64 {
	switch scene {
	case "波段反弹":
		return item.SwingScore
	case "趋势持有":
		return item.TrendScore
	case "长期配置":
		return item.LongTermScore
	default:
		return item.ShortScore
	}
}

func bestCandidateScene(scores map[string]float64) string {
	best, bestScore := "短线爆发", -math.MaxFloat64
	for _, scene := range []string{"短线爆发", "波段反弹", "趋势持有"} {
		if scores[scene] > bestScore {
			best, bestScore = scene, scores[scene]
		}
	}
	return best
}

func sceneHorizon(scene string) int {
	switch scene {
	case "波段反弹":
		return 20
	case "趋势持有":
		return 60
	case "长期配置":
		return 250
	default:
		return 5
	}
}

func sceneTargetReturn(scene string) float64 {
	switch scene {
	case "波段反弹":
		return 0.10
	case "趋势持有":
		return 0.15
	default:
		return 0.05
	}
}

func scenePositionCap(scene string) float64 {
	switch scene {
	case "波段反弹":
		return 0.15
	case "趋势持有":
		return 0.20
	default:
		return 0.10
	}
}

func candidateFeatureMap(tradeDate string) map[string]models.StockFeature {
	features := NewFeatureRepositoryForVersion(CurrentFeatureVersion).GetByDate(tradeDate, nil)
	result := make(map[string]models.StockFeature, len(features))
	for _, feature := range features {
		code := NormalizeCandidateStockCode(feature.StockCode)
		if code != "" {
			result[code] = feature
		}
	}
	return result
}

func candidateScopeSet(scope string) map[string]bool {
	if isAllStockScope(scope) || scope == "" {
		return nil
	}
	codes := NewStockPoolService().GetStockPool(scope)
	result := make(map[string]bool, len(codes))
	for _, code := range codes {
		if normalized := NormalizeCandidateStockCode(code); normalized != "" {
			result[normalized] = true
		}
	}
	return result
}

func (s *CandidatePoolService) GetSnapshot(id uint, decisionAsOf time.Time) (*CandidateSnapshotDetails, error) {
	var snapshot models.CandidateSnapshot
	if err := db.Dao.First(&snapshot, id).Error; err != nil {
		return nil, fmt.Errorf("候选快照不存在")
	}
	if decisionAsOf.IsZero() {
		decisionAsOf = shanghaiNow()
	}
	if decisionAsOf.Before(snapshot.AvailableAt) {
		return nil, fmt.Errorf("候选快照在 %s 尚不可见", decisionAsOf.Format(time.RFC3339))
	}
	var items []models.CandidateSnapshotItem
	if err := db.Dao.Where("snapshot_id = ? AND available_at <= ?", id, decisionAsOf).Order("source_rank asc").Find(&items).Error; err != nil {
		return nil, err
	}
	var facts []models.CandidateSourceFact
	if err := db.Dao.Where("snapshot_id = ? AND available_at <= ?", id, decisionAsOf).Order("stock_code asc, source asc").Find(&facts).Error; err != nil {
		return nil, err
	}
	return &CandidateSnapshotDetails{Snapshot: snapshot, Items: items, Facts: facts, Freshness: freshnessFromFacts(facts)}, nil
}

func (s *CandidatePoolService) ListSnapshots(limit int, scene string) []models.CandidateSnapshot {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var snapshots []models.CandidateSnapshot
	query := db.Dao.Order("created_at desc").Limit(limit)
	if scene != "" {
		query = query.Where("scene = ?", scene)
	}
	_ = query.Find(&snapshots).Error
	return snapshots
}

func freshnessFromFacts(facts []models.CandidateSourceFact) map[string]CandidateSourceFreshness {
	result := map[string]CandidateSourceFreshness{}
	seen := map[string]map[string]bool{}
	for _, fact := range facts {
		fresh := result[fact.Source]
		fresh.Source, fresh.Available = fact.Source, true
		if seen[fact.Source] == nil {
			seen[fact.Source] = map[string]bool{}
		}
		seen[fact.Source][fact.StockCode] = true
		fresh.ItemCount = len(seen[fact.Source])
		if fact.DataAsOf.After(fresh.DataAsOf) {
			fresh.DataAsOf = fact.DataAsOf
		}
		if fact.AvailableAt.After(fresh.AvailableAt) {
			fresh.AvailableAt = fact.AvailableAt
		}
		result[fact.Source] = fresh
	}
	return result
}

func (s *CandidatePoolService) DeleteSnapshot(id uint) error {
	result := db.Dao.Model(&models.CandidateSnapshot{}).Where("id = ?", id).Updates(map[string]any{
		"status": "expired", "error_message": "用户归档；候选项和来源事实为审计记录，永久保留",
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("候选快照不存在")
	}
	return nil
}

func candidateHash(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func ratingCandidateScore(rating string) float64 {
	rating = strings.ToLower(strings.TrimSpace(rating))
	switch {
	case strings.Contains(rating, "强烈"), strings.Contains(rating, "strong"), strings.Contains(rating, "买入"):
		return 75
	case strings.Contains(rating, "推荐"), strings.Contains(rating, "增持"), strings.Contains(rating, "buy"):
		return 65
	case strings.Contains(rating, "谨慎"), strings.Contains(rating, "中性"), strings.Contains(rating, "hold"):
		return 50
	default:
		return 45
	}
}

func parseCandidateFloat(value string) float64 {
	value = strings.TrimSpace(strings.TrimSuffix(value, "%"))
	result, _ := strconv.ParseFloat(value, 64)
	return result
}

func parseShanghaiEventTime(date, value string, fallback time.Time) time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, strings.TrimSpace(date+" "+value), shanghaiLocation()); err == nil {
			return parsed
		}
	}
	if !fallback.IsZero() {
		return fallback
	}
	return time.Now()
}

func sortSourceCandidates(rows []sourceCandidate) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Score == rows[j].Score {
			return rows[i].StockCode < rows[j].StockCode
		}
		return rows[i].Score > rows[j].Score
	})
}

func intFromAny(value any) int {
	return int(math.Round(floatFromAny(value)))
}

func floatFromAny(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		result, _ := v.Float64()
		return result
	case string:
		result, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, "%")), 64)
		return result
	default:
		return 0
	}
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	if value, ok := value.(string); ok {
		return value
	}
	return fmt.Sprint(value)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// ensureCandidateFeatures 自动同步五源候选股票的特征数据（仅同步缺失的股票，避免重复）
func (s *CandidatePoolService) ensureCandidateFeatures(items []models.CandidateSnapshotItem) {
	if len(items) == 0 || db.Dao == nil {
		return
	}
	codes := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		code := strings.TrimSpace(item.StockCode)
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		return
	}
	var existingCodes []string
	db.Dao.Model(&models.StockFeature{}).
		Where("stock_code IN ? AND feature_version = ? AND adjusted = ?", codes, CurrentFeatureVersion, true).
		Distinct("stock_code").Pluck("stock_code", &existingCodes)
	existingSet := make(map[string]bool, len(existingCodes))
	for _, code := range existingCodes {
		existingSet[code] = true
	}
	needSync := make([]string, 0, len(codes))
	for _, code := range codes {
		if !existingSet[code] {
			needSync = append(needSync, code)
		}
	}
	if len(needSync) == 0 {
		logger.SugaredLogger.Infof("candidate features: all %d stocks already have feature data", len(codes))
		return
	}
	logger.SugaredLogger.Infof("candidate features: auto-syncing %d/%d stocks", len(needSync), len(codes))
	go func() {
		service := NewFeatureSyncService()
		for _, code := range needSync {
			if err := service.SyncStockFeatures(code, 365); err != nil {
				logger.SugaredLogger.Warnf("candidate features: sync %s failed: %v", code, err)
			}
		}
		logger.SugaredLogger.Infof("candidate features: auto-sync completed for %d stocks", len(needSync))
	}()
}
