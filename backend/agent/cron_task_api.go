package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go-stock/backend/backtest"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/logger"
	"go-stock/backend/models"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

type CronTaskApi struct{}

type taskSkippedError struct{ reason string }

func (e *taskSkippedError) Error() string { return e.reason }

type taskPartialError struct{ reason string }

func (e *taskPartialError) Error() string { return e.reason }

var (
	activeCronTaskRuns sync.Map
	aTradingDayCache   sync.Map
	shanghaiLocation   = time.FixedZone("Asia/Shanghai", 8*60*60)
)

type tradingDayCacheValue struct {
	trading bool
	expires time.Time
}

func skipTask(reason string) error    { return &taskSkippedError{reason: reason} }
func partialTask(reason string) error { return &taskPartialError{reason: reason} }

func NewCronTaskApi() *CronTaskApi {
	return &CronTaskApi{}
}

func (a *CronTaskApi) normalizeTask(task *models.CronTask) {
	if task == nil {
		return
	}
	if task.Module == "" {
		task.Module = "user"
	}
	if task.Owner == "" {
		task.Owner = "user"
	}
	if !task.IsSystem {
		task.Visible = true
		task.AllowDelete = true
	}
	if task.Status == "" {
		task.Status = "active"
	}
}

func (a *CronTaskApi) Create(task *models.CronTask) error {
	a.normalizeTask(task)
	return db.Dao.Create(task).Error
}

func (a *CronTaskApi) Update(task *models.CronTask) error {
	if task == nil || task.ID == 0 {
		return fmt.Errorf("无效的任务ID")
	}

	var existing models.CronTask
	if err := db.Dao.First(&existing, task.ID).Error; err != nil {
		return err
	}
	if existing.IsSystem && !existing.AllowDelete {
		task.Module = existing.Module
		task.IsSystem = existing.IsSystem
		task.Visible = existing.Visible
		task.AllowDelete = existing.AllowDelete
		task.Owner = existing.Owner
	} else {
		a.normalizeTask(task)
	}

	updates := map[string]any{
		"name":         task.Name,
		"cron_expr":    task.CronExpr,
		"task_type":    task.TaskType,
		"target":       task.Target,
		"params":       task.Params,
		"enable":       task.Enable,
		"status":       task.Status,
		"description":  task.Description,
		"module":       task.Module,
		"is_system":    task.IsSystem,
		"visible":      task.Visible,
		"allow_delete": task.AllowDelete,
		"owner":        task.Owner,
	}

	return db.Dao.Model(&models.CronTask{}).
		Where("id = ?", task.ID).
		Updates(updates).Error
}

func (a *CronTaskApi) Delete(id uint) error {
	var task models.CronTask
	if err := db.Dao.First(&task, id).Error; err != nil {
		return err
	}
	if task.IsSystem && !task.AllowDelete {
		return fmt.Errorf("系统内置任务不允许删除，请使用禁用")
	}
	return db.Dao.Delete(&models.CronTask{}, id).Error
}

func (a *CronTaskApi) GetByID(id uint) (*models.CronTask, error) {
	var task models.CronTask
	err := db.Dao.First(&task, id).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (a *CronTaskApi) List(query *models.CronTaskQuery) *models.CronTaskPageResp {
	var tasks []models.CronTask
	var total int64

	dbQuery := db.Dao.Model(&models.CronTask{})

	if query.Name != "" {
		dbQuery = dbQuery.Where("name LIKE ?", "%"+query.Name+"%")
	}
	if query.TaskType != "" {
		dbQuery = dbQuery.Where("task_type = ?", query.TaskType)
	}
	if query.Status != "" {
		dbQuery = dbQuery.Where("status = ?", query.Status)
	}
	if query.Module != "" {
		dbQuery = dbQuery.Where("module = ?", query.Module)
	}
	if !query.IncludeSystem {
		dbQuery = dbQuery.Where("COALESCE(is_system, ?) = ?", false, false)
	}
	if query.Enable != nil {
		dbQuery = dbQuery.Where("enable = ?", *query.Enable)
	}

	dbQuery.Count(&total)

	page := query.Page
	pageSize := query.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}

	err := dbQuery.Offset((page - 1) * pageSize).Limit(pageSize).Order("created_at DESC").Find(&tasks).Error
	if err != nil {
		logger.SugaredLogger.Errorf("查询定时任务列表失败:%s", err.Error())
		return nil
	}

	return &models.CronTaskPageResp{
		Total: int(total),
		Data:  tasks,
	}
}

func (a *CronTaskApi) GetAll() []models.CronTask {
	var tasks []models.CronTask
	db.Dao.Where("enable = ?", true).Order("created_at DESC").Find(&tasks)
	return tasks
}

func (a *CronTaskApi) ExistsByTaskType(taskType string) bool {
	var count int64
	db.Dao.Model(&models.CronTask{}).Where("task_type = ?", taskType).Count(&count)
	return count > 0
}

func (a *CronTaskApi) GetByTaskType(taskType string) (*models.CronTask, error) {
	var task models.CronTask
	if err := db.Dao.Where("task_type = ?", taskType).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func (a *CronTaskApi) EnsureSystemTask(task *models.CronTask) error {
	if task == nil {
		return fmt.Errorf("任务为空")
	}
	task.Module = "prediction_factory"
	task.IsSystem = true
	task.Visible = false
	task.AllowDelete = false
	task.Owner = "system"
	if task.Status == "" {
		task.Status = "active"
	}

	var existing models.CronTask
	err := db.Dao.Where("task_type = ?", task.TaskType).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return db.Dao.Create(task).Error
	}
	if err != nil {
		return err
	}
	updates := map[string]any{
		"name":         task.Name,
		"cron_expr":    task.CronExpr,
		"target":       task.Target,
		"params":       task.Params,
		"status":       task.Status,
		"description":  task.Description,
		"module":       task.Module,
		"is_system":    task.IsSystem,
		"visible":      task.Visible,
		"allow_delete": task.AllowDelete,
		"owner":        task.Owner,
	}
	if existing.CronExpr != task.CronExpr {
		updates["next_run_at"] = nil
	}
	return db.Dao.Model(&existing).Updates(updates).Error
}

func (a *CronTaskApi) EnableTask(id uint, enable bool) error {
	return db.Dao.Model(&models.CronTask{}).Where("id = ?", id).Updates(map[string]any{
		"enable": enable,
	}).Error
}

func (a *CronTaskApi) UpdateRunInfo(id uint, lastRunAt time.Time, nextRunAt *time.Time, lastRunResult string) error {
	return db.Dao.Model(&models.CronTask{}).Where("id = ?", id).Updates(map[string]any{
		"last_run_at":     lastRunAt,
		"next_run_at":     nextRunAt,
		"run_count":       gorm.Expr("run_count + 1"),
		"last_run_result": lastRunResult,
	}).Error
}

func (a *CronTaskApi) GetTaskTypes() []lo.Tuple2[string, string] {
	return []lo.Tuple2[string, string]{
		{A: "stock_analysis", B: "股票分析"},
		{A: "market_analysis", B: "市场分析"},
		{A: "global_stock_index_cache", B: "全球指数缓存"},
		{A: "stock_change_save", B: "异动数据保存"},
	}
}

func (a *CronTaskApi) ValidateCronExpr(expr string) error {
	_, err := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(expr)
	return err
}

func (a *CronTaskApi) CalculateNextRunTimes(cronExpr string, count int) []time.Time {
	if count <= 0 {
		return []time.Time{}
	}

	schedule, err := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(cronExpr)
	if err != nil {
		logger.SugaredLogger.Errorf("解析 Cron 表达式失败：%v", err)
		return []time.Time{}
	}

	times := make([]time.Time, 0, count)
	next := time.Now()
	for i := 0; i < count; i++ {
		next = schedule.Next(next)
		times = append(times, next)
	}
	return times
}

func (a *CronTaskApi) SearchTasks(keyword string) []models.CronTask {
	var tasks []models.CronTask
	query := db.Dao.Model(&models.CronTask{})
	if keyword != "" {
		keyword = strings.TrimSpace(keyword)
		query = query.Where("name LIKE ? OR target LIKE ? OR description LIKE ?",
			"%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
	}
	query.Order("created_at DESC").Limit(20).Find(&tasks)
	return tasks
}

func (a *CronTaskApi) ExecuteTask(ctx context.Context, task *models.CronTask) error {
	if task == nil {
		return fmt.Errorf("定时任务为空")
	}
	logger.SugaredLogger.Infof("开始执行定时任务：%s (ID: %d)", task.Name, task.ID)

	now := time.Now()
	nextRunAt := a.CalculateNextRunTime(task.CronExpr)
	runKey := fmt.Sprintf("%d:%s", task.ID, task.TaskType)
	if _, loaded := activeCronTaskRuns.LoadOrStore(runKey, struct{}{}); loaded {
		runResult := "跳过: 上一轮任务仍在执行"
		_ = a.UpdateRunInfo(task.ID, now, &nextRunAt, runResult)
		logger.SugaredLogger.Warnf("跳过重叠定时任务：%s", task.Name)
		return nil
	}
	defer activeCronTaskRuns.Delete(runKey)

	var runResult string
	err := a.executeTaskByType(ctx, task)
	var skipped *taskSkippedError
	var partial *taskPartialError
	switch {
	case errors.As(err, &skipped):
		runResult = "跳过: " + skipped.reason
		logger.SugaredLogger.Infof("定时任务跳过：%s，原因：%s", task.Name, skipped.reason)
		err = nil
	case errors.As(err, &partial):
		runResult = "部分成功: " + partial.reason
		logger.SugaredLogger.Warnf("定时任务部分成功：%s，详情：%s", task.Name, partial.reason)
		err = nil
	case err != nil:
		runResult = "失败: " + err.Error()
		logger.SugaredLogger.Errorf("执行定时任务失败：%s, 错误：%v", task.Name, err)
	default:
		runResult = "成功"
	}

	err2 := a.UpdateRunInfo(task.ID, now, &nextRunAt, runResult)
	if err2 != nil {
		logger.SugaredLogger.Errorf("更新任务运行信息失败：%v", err2)
	}

	return err
}

func (a *CronTaskApi) executeTaskByType(ctx context.Context, task *models.CronTask) error {
	switch task.TaskType {
	case "stock_analysis":
		return a.executeStockAnalysis(ctx, task)
	case "market_analysis":
		return a.executeMarketAnalysis(ctx, task)
	case "global_stock_index_cache":
		return a.executeGlobalStockIndexCache(ctx, task)
	case "fund_analysis":
		return a.executeFundAnalysis(ctx, task)
	case "news_fetch":
		return a.executeNewsFetch(ctx, task)
	case "stock_monitor":
		return a.executeStockMonitor(ctx, task)
	case "stock_change_save":
		return a.executeStockChangeSave(ctx, task)
	case "prediction_sync_features":
		return a.executePredictionSyncFeatures(ctx, task)
	case "prediction_sync_money_flow":
		return a.executePredictionSyncMoneyFlow(ctx, task)
	case "prediction_sync_candidates":
		return a.executePredictionSyncCandidates(ctx, task)
	case "prediction_scan_signals":
		return a.executePredictionScanSignals(ctx, task)
	case "prediction_scan_alerts":
		return a.executePredictionScanAlerts(ctx, task)
	case "prediction_validate_signals":
		return a.executePredictionValidateSignals(ctx, task)
	case "custom":
		return a.executeCustomTask(ctx, task)
	default:
		logger.SugaredLogger.Warnf("未知任务类型：%s", task.TaskType)
		return fmt.Errorf("未知任务类型：%s", task.TaskType)
	}
}

func (a *CronTaskApi) CalculateNextRunTime(cronExpr string) time.Time {
	schedule, err := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(cronExpr)
	if err != nil {
		return time.Now().Add(time.Hour)
	}
	return schedule.Next(time.Now())
}

func (a *CronTaskApi) executeStockAnalysis(ctx context.Context, task *models.CronTask) error {
	logger.SugaredLogger.Infof("执行股票分析任务：%s", task.Name)
	var params struct {
		PromptId    int    `json:"promptId"`
		AiConfigId  int    `json:"aiConfigId"`
		SysPromptId int    `json:"sysPromptId"`
		Thinking    bool   `json:"thinking"`
		StockCode   string `json:"stockCode"`
		StockName   string `json:"stockName"`
		AgentMode   string `json:"agentMode"`
	}
	if task.Params != "" {
		err := json.Unmarshal([]byte(task.Params), &params)
		if err != nil {
			logger.SugaredLogger.Errorf("解析任务参数失败：%v", err)
			return err
		}
	}

	prompt := fmt.Sprintf("分析总结市场资讯，针对%s[%s]，找出潜在投资机会", params.StockName, params.StockCode)
	prompt = data.NewPromptTemplateApi().GetPromptTemplateByID(params.PromptId)
	var tools []data.Tool
	tools = data.Tools(tools)
	msgs := data.NewDeepSeekOpenAi(ctx, params.AiConfigId).NewChatStream(params.StockName, data.ConvertTushareCodeToStockCode(params.StockCode), prompt, &params.SysPromptId, tools, params.Thinking)
	content := &strings.Builder{}
	for msg := range msgs {
		if v, ok := msg["content"].(string); ok {
			content.WriteString(v)
		}
	}
	logger.SugaredLogger.Infof("content:%s", content.String())
	data.NewDeepSeekOpenAi(ctx, params.AiConfigId).SaveAIResponseResult(params.StockCode, params.StockName, content.String(), "", prompt)
	return nil
}

func (a *CronTaskApi) executeFundAnalysis(ctx context.Context, task *models.CronTask) error {
	var params struct {
		FundCodes  []string `json:"fund_codes"`
		AiConfigId int      `json:"ai_config_id"`
	}

	if task.Params != "" {
		err := json.Unmarshal([]byte(task.Params), &params)
		if err != nil {
			logger.SugaredLogger.Errorf("解析任务参数失败：%v", err)
			return err
		}
	}

	for _, fundCode := range params.FundCodes {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			logger.SugaredLogger.Infof("分析基金：%s", fundCode)
		}
	}

	return nil
}

func (a *CronTaskApi) executeNewsFetch(ctx context.Context, task *models.CronTask) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		data.NewMarketNewsApi().TelegraphList(30)
		logger.SugaredLogger.Info("新闻抓取完成")
		return nil
	}
}

func (a *CronTaskApi) executeStockMonitor(ctx context.Context, task *models.CronTask) error {
	var params struct {
		StockCodes      []string `json:"stock_codes"`
		PriceThreshold  float64  `json:"price_threshold"`
		ChangeThreshold float64  `json:"change_threshold"`
	}

	if task.Params != "" {
		err := json.Unmarshal([]byte(task.Params), &params)
		if err != nil {
			logger.SugaredLogger.Errorf("解析任务参数失败：%v", err)
			return err
		}
	}

	for _, stockCode := range params.StockCodes {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			logger.SugaredLogger.Infof("监控股票：%s", stockCode)
		}
	}

	return nil
}

func (a *CronTaskApi) executeCustomTask(ctx context.Context, task *models.CronTask) error {
	logger.SugaredLogger.Infof("执行自定义任务：%s", task.Name)
	return nil
}

func (a *CronTaskApi) executeMarketAnalysis(ctx context.Context, task *models.CronTask) error {
	logger.SugaredLogger.Infof("执行市场分析任务：%s", task.Name)
	var params struct {
		PromptId    int    `json:"promptId"`
		AiConfigId  int    `json:"aiConfigId"`
		SysPromptId int    `json:"sysPromptId"`
		Thinking    bool   `json:"thinking"`
		AgentMode   string `json:"agentMode"`
	}
	if task.Params != "" {
		err := json.Unmarshal([]byte(task.Params), &params)
		if err != nil {
			logger.SugaredLogger.Errorf("解析任务参数失败：%v", err)
			return err
		}
	}

	prompt := "分析总结市场资讯，找出潜在投资机会"
	prompt = data.NewPromptTemplateApi().GetPromptTemplateByID(params.PromptId)
	content := &strings.Builder{}

	ch := NewStockAiAgentApi().ChatWithContext(ctx, prompt, params.AiConfigId, &params.SysPromptId, false, 0, false, params.AgentMode)
	for msg := range ch {
		if msg.ReasoningContent != "" {
			content.WriteString(msg.ReasoningContent)
		}
		content.WriteString(msg.Content)
	}
	logger.SugaredLogger.Infof("content:%s", content.String())
	data.NewDeepSeekOpenAi(ctx, params.AiConfigId).SaveAIResponseResult("市场分析", "市场分析", content.String(), "", prompt)
	return nil
}

func (a *CronTaskApi) executeGlobalStockIndexCache(ctx context.Context, task *models.CronTask) error {
	logger.SugaredLogger.Infof("执行全球指数缓存任务：%s", task.Name)
	var params struct {
		CrawlTimeOut uint `json:"crawlTimeOut"`
	}
	if task.Params != "" {
		err := json.Unmarshal([]byte(task.Params), &params)
		if err != nil {
			logger.SugaredLogger.Errorf("解析任务参数失败：%v", err)
			return err
		}
	}
	if params.CrawlTimeOut == 0 {
		params.CrawlTimeOut = 30
	}
	return data.NewMarketNewsApi().CacheGlobalStockIndexes(params.CrawlTimeOut)
}

func (a *CronTaskApi) executePredictionSyncFeatures(ctx context.Context, task *models.CronTask) error {
	logger.SugaredLogger.Infof("执行预测工厂特征同步任务：%s", task.Name)
	if !isATradingDay(time.Now()) {
		return skipTask("当前不是A股交易日")
	}
	var params struct {
		StockScope string `json:"stockScope"`
		Days       int    `json:"days"`
	}
	if task.Params != "" {
		if err := json.Unmarshal([]byte(task.Params), &params); err != nil {
			logger.SugaredLogger.Errorf("解析任务参数失败：%v", err)
			return err
		}
	}
	if params.StockScope == "" {
		params.StockScope = "全部A股"
	}
	if params.Days <= 0 {
		params.Days = 365
	}

	service := backtest.NewFeatureSyncService().TechnicalOnly()
	job, err := service.RunFeatureSync(params.StockScope, params.Days)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("特征同步未返回任务状态")
	}
	switch job.Status {
	case "running":
		return skipTask(fmt.Sprintf("特征同步任务 %d 已在运行", job.ID))
	case "failed":
		return fmt.Errorf("特征同步失败：%s", strings.TrimSpace(job.ErrorMessage))
	case "partial":
		return partialTask(fmt.Sprintf("特征同步完成 %d/%d，失败 %d：%s", job.Finished, job.Total, job.Failed, strings.TrimSpace(job.ErrorMessage)))
	}
	freshness := service.GetFeatureFreshness(params.StockScope)
	latestDate, _ := freshness["latestDate"].(string)
	today := time.Now().In(shanghaiLocation).Format("2006-01-02")
	if latestDate != today {
		return partialTask(fmt.Sprintf("技术特征接口完成，但最新交易日仍为 %s", latestDate))
	}
	return nil
}

func (a *CronTaskApi) executePredictionSyncMoneyFlow(ctx context.Context, task *models.CronTask) error {
	logger.SugaredLogger.Infof("执行预测工厂资金流同步任务：%s", task.Name)
	if !isATradingDay(time.Now()) {
		return skipTask("当前不是A股交易日")
	}
	var params struct {
		StockScope string `json:"stockScope"`
		Days       int    `json:"days"`
	}
	if task.Params != "" {
		if err := json.Unmarshal([]byte(task.Params), &params); err != nil {
			logger.SugaredLogger.Errorf("解析任务参数失败：%v", err)
			return err
		}
	}
	if params.StockScope == "" {
		params.StockScope = "自选股"
	}
	if params.Days <= 0 {
		params.Days = 120
	}
	stockCodes := backtest.NewStockPoolService().GetStockPool(params.StockScope)
	result, err := backtest.NewFeatureSyncService().SyncMoneyFlows(stockCodes, params.Days)
	if err != nil {
		return err
	}
	logger.SugaredLogger.Infof("预测工厂资金流同步完成：股票 %d，当日新鲜 %d，资金流 %d，MAC %d，行业 %d(%s)，概念 %d(%s)，失败 %d，部分历史缺失 %d",
		result.StockCount, result.FreshStocks, result.FlowRows, result.MacRows, result.SectorRows, result.SectorTradeDate,
		result.ConceptRows, result.ConceptTradeDate, result.FailedStocks, result.PartialStocks)
	if result.FailedStocks >= len(stockCodes) && len(stockCodes) > 0 {
		return fmt.Errorf("全部 %d 只股票资金流同步失败", len(stockCodes))
	}
	today := time.Now().In(shanghaiLocation).Format("2006-01-02")
	if result.LatestTradeDate != today {
		return partialTask(fmt.Sprintf("未取得当日资金流，最新数据为 %s", result.LatestTradeDate))
	}
	if result.SectorTradeDate != today || result.ConceptTradeDate != today {
		return partialTask(fmt.Sprintf("板块资金流未全部就绪：行业 %s，概念 %s", result.SectorTradeDate, result.ConceptTradeDate))
	}
	if result.FreshStocks < len(stockCodes) || result.FailedStocks > 0 || result.PartialStocks > 0 {
		return partialTask(fmt.Sprintf("当日新鲜 %d/%d，失败 %d，历史数据不完整 %d", result.FreshStocks, len(stockCodes), result.FailedStocks, result.PartialStocks))
	}
	return nil
}

func (a *CronTaskApi) executePredictionSyncCandidates(ctx context.Context, task *models.CronTask) error {
	logger.SugaredLogger.Infof("执行预测工厂五源候选快照任务：%s", task.Name)
	if !isATradingDay(time.Now()) {
		return skipTask("当前不是A股交易日")
	}
	now := time.Now().In(shanghaiLocation)
	tradeDate := now.Format("2006-01-02")
	freshness := backtest.NewFeatureSyncService().GetFeatureFreshness("全部A股")
	if latestDate, _ := freshness["latestDate"].(string); latestDate != tradeDate {
		return skipTask(fmt.Sprintf("当日技术特征未就绪，最新数据为 %s", latestDate))
	}
	details, err := backtest.NewCandidatePoolService().Generate(backtest.CandidateGenerateRequest{
		Name:           tradeDate + " 五源自动候选池",
		Scene:          "短线爆发",
		StockScope:     "全部A股",
		TradeDate:      tradeDate,
		Sources:        []string{"recommendation", "event", "uplimit", "pattern", "indicator"},
		SourceMode:     "union",
		MinimumSources: 1,
		Limit:          200,
	})
	if err != nil {
		return err
	}
	if details == nil || details.Snapshot.Status == "failed" {
		message := "未生成候选快照"
		if details != nil && strings.TrimSpace(details.Snapshot.ErrorMessage) != "" {
			message = details.Snapshot.ErrorMessage
		}
		return partialTask(message)
	}
	logger.SugaredLogger.Infof("五源候选快照完成：快照 %d，候选 %d，覆盖率 %.2f", details.Snapshot.ID, details.Snapshot.CandidateCount, details.Snapshot.Coverage)
	if details.Snapshot.Coverage < 1 {
		return partialTask(fmt.Sprintf("快照 %d 已生成 %d 只候选，来源覆盖率 %.0f%%：%s", details.Snapshot.ID, details.Snapshot.CandidateCount, details.Snapshot.Coverage*100, details.Snapshot.ErrorMessage))
	}
	return nil
}

func (a *CronTaskApi) executePredictionScanSignals(ctx context.Context, task *models.CronTask) error {
	logger.SugaredLogger.Infof("执行预测工厂扫描信号任务：%s", task.Name)
	if !isATradingDay(time.Now()) {
		return skipTask("当前不是A股交易日")
	}
	result, err := backtest.NewPredictionService().ScanSignals()
	if err != nil {
		return err
	}
	today := time.Now().In(shanghaiLocation).Format("2006-01-02")
	if result.FeatureDate != today {
		return skipTask(fmt.Sprintf("当日技术特征未就绪，最新数据为 %s", result.FeatureDate))
	}
	if result.ActiveHypotheses == 0 {
		if result.RefreshedDecisions > 0 {
			return partialTask(fmt.Sprintf("已刷新 %d 条观察建议，没有启用正式监控的信号策略", result.RefreshedDecisions))
		}
		return skipTask("没有启用正式或观察监控的策略")
	}
	logger.SugaredLogger.Infof("预测工厂信号扫描完成：正式策略 %d，监控策略 %d，刷新会话 %d/建议 %d，特征 %d，新增信号 %d",
		result.ActiveHypotheses, result.MonitoredHypotheses, result.RefreshedSessions, result.RefreshedDecisions, result.FeatureRows, result.Generated)
	return nil
}

func (a *CronTaskApi) executePredictionScanAlerts(ctx context.Context, task *models.CronTask) error {
	logger.SugaredLogger.Infof("执行预测工厂盘中提醒任务：%s", task.Name)
	if !isTradingTime() {
		return skipTask("当前不在A股竞价交易时段")
	}

	params := struct {
		SendNotification bool `json:"sendNotification"`
	}{
		SendNotification: true,
	}
	if task.Params != "" {
		if err := json.Unmarshal([]byte(task.Params), &params); err != nil {
			logger.SugaredLogger.Errorf("解析任务参数失败：%v", err)
			return err
		}
	}
	result, err := backtest.NewPredictionAlertService().ScanRealtimeAlerts(params.SendNotification)
	if err != nil {
		return err
	}
	logger.SugaredLogger.Infof("预测工厂盘中提醒扫描完成：扫描 %d，行情无效 %d，触发 %d，新增 %d，通知 %d，解除 %d",
		result.Scanned, result.Stale, result.Generated, len(result.Alerts), result.Sent, result.Resolved)
	if result.Scanned == 0 {
		return skipTask("没有正式或观察中的监控策略")
	}
	if result.Scanned > 0 && result.Stale == result.Scanned {
		return partialTask("全部监控标的缺少当日三分钟内的新鲜行情，未进行阈值判断")
	}
	return nil
}

func (a *CronTaskApi) executePredictionValidateSignals(ctx context.Context, task *models.CronTask) error {
	logger.SugaredLogger.Infof("执行预测工厂验证信号任务：%s", task.Name)
	if !isATradingDay(time.Now()) {
		return skipTask("当前不是A股交易日")
	}
	result, err := backtest.NewPredictionService().DailyValidateSignals()
	if err != nil {
		return err
	}
	if result.Accounts == 0 {
		return skipTask("没有前向验证或正式监控账户")
	}
	if result.ProcessedDays == 0 {
		return skipTask("模拟账户已是最新交易日状态")
	}
	logger.SugaredLogger.Infof("预测工厂前向验证完成：账户 %d，推进交易日 %d，待处理 %d，入场 %d，错过 %d，平仓 %d",
		result.Accounts, result.ProcessedDays, result.Pending, result.Entered, result.Missed, result.Validated)
	return nil
}

func (a *CronTaskApi) executeStockChangeSave(ctx context.Context, task *models.CronTask) error {
	logger.SugaredLogger.Infof("执行异动数据保存任务：%s", task.Name)

	if !isTradingTime() {
		logger.SugaredLogger.Info("当前不在A股交易时间，跳过异动数据保存")
		return nil
	}

	var params struct {
		ChangeTypes []int `json:"changeTypes"`
		DeleteDays  int   `json:"deleteDays"`
	}

	if task.Params != "" {
		err := json.Unmarshal([]byte(task.Params), &params)
		if err != nil {
			logger.SugaredLogger.Errorf("解析任务参数失败：%v", err)
			return err
		}
	}

	if len(params.ChangeTypes) == 0 {
		params.ChangeTypes = []int{
			8201, 8202, 8193, 4, 32, 64, 8207, 8209, 8211, 8213, 8215,
			8204, 8203, 8194, 8, 16, 128, 8208, 8210, 8212, 8214, 8216,
		}
	}

	api := data.NewStockChangesApi()
	result := api.GetStockChanges(params.ChangeTypes, 0, 500)
	if result == nil || len(result.Data) == 0 {
		logger.SugaredLogger.Info("没有获取到异动数据")
		return nil
	}

	savedCount, err := data.NewStockChangeHistoryService().SaveStockChangesWithDedup(result.Data)
	if err != nil {
		logger.SugaredLogger.Errorf("保存异动数据失败：%v", err)
		return err
	}

	logger.SugaredLogger.Infof("成功保存 %d 条异动数据（去重后）", savedCount)

	if params.DeleteDays > 0 {
		err = data.NewStockChangeHistoryService().DeleteOldData(params.DeleteDays)
		if err != nil {
			logger.SugaredLogger.Warnf("删除旧数据失败：%v", err)
		} else {
			logger.SugaredLogger.Infof("已删除 %d 天前的历史数据", params.DeleteDays)
		}
	}

	return nil
}

func isTradingTime() bool {
	now := time.Now().In(shanghaiLocation)
	if !isATradingDay(now) {
		return false
	}
	return isATradingSessionClock(now)
}

func isATradingSessionClock(now time.Time) bool {
	hour, minute := now.Hour(), now.Minute()
	currentTime := hour*100 + minute

	morningStart := 930
	morningEnd := 1130
	afternoonStart := 1300
	afternoonEnd := 1500

	isMorning := currentTime >= morningStart && currentTime <= morningEnd
	isAfternoon := currentTime >= afternoonStart && currentTime <= afternoonEnd

	return isMorning || isAfternoon
}

func isATradingDay(at time.Time) bool {
	local := at.In(shanghaiLocation)
	if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
		return false
	}
	date := local.Format("2006-01-02")
	if cached, ok := aTradingDayCache.Load(date); ok {
		value := cached.(tradingDayCacheValue)
		if time.Now().Before(value.expires) {
			return value.trading
		}
		aTradingDayCache.Delete(date)
	}

	type holidayResponse struct {
		Code    int `json:"code"`
		Holiday struct {
			Holiday bool `json:"holiday"`
		} `json:"holiday"`
	}
	var response holidayResponse
	resp, err := data.SharedHTTPClient.R().SetResult(&response).
		Get(fmt.Sprintf("https://timor.tech/api/holiday/info/%s", date))
	if err == nil && resp.StatusCode() == 200 && response.Code == 0 {
		trading := !response.Holiday.Holiday
		aTradingDayCache.Store(date, tradingDayCacheValue{trading: trading, expires: time.Now().Add(24 * time.Hour)})
		return trading
	}

	// Calendar service failure must not block a real trading day. Downstream
	// feature-date and realtime-quote freshness gates still prevent stale work.
	aTradingDayCache.Store(date, tradingDayCacheValue{trading: true, expires: time.Now().Add(10 * time.Minute)})
	return true
}
