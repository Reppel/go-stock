# 机会发现重构：推荐股票与历史快照分离

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将机会发现从单一"候选快照"模式拆分为"推荐股票"（可删除）+ "历史快照"（审计只读），新增 `recommend_stock` 表，前端双 tab 展示，分析工作台接入五源推荐股票作为股票池。

**Architecture:** 生成候选快照时同步 upsert 写入 `recommend_stock` 表（按 stockCode+tradeDate 去重）；推荐股票 tab 读写 `recommend_stock`，历史快照 tab 只读 `candidate_snapshot`；分析工作台 `snapshot_all` scope 从 `recommend_stock` 取股票代码。

**Tech Stack:** Go + GORM + Wails (frontend Vue3 + NaiveUI)

---

## File Structure

| 文件 | 改动类型 | 职责 |
|------|---------|------|
| `backend/models/prediction_models.go` | 新增 | `RecommendStock` 模型 |
| `backend/backtest/candidate_service.go` | 修改 | Generate 同步写入 recommend_stock；新增 ListRecommendStocks/DeleteRecommendStock/BatchDeleteRecommendStocks；ListSnapshots 过滤 expired |
| `backend/backtest/stock_pool.go` | 修改 | GetStockPool 新增 `snapshot_all` 分支 |
| `app.go` | 新增 | GetRecommendStocks、DeleteRecommendStock、BatchDeleteRecommendStocks API |
| `frontend/src/components/PredictionFactory.vue` | 大改 | 机会发现拆双 tab；推荐股票表格/勾选/筛选/搜索/送入量化/删除；历史快照归档全部；scopes 加五源推荐股票 |

---

### Task 1: 新增 RecommendStock 模型

**Files:**
- Modify: `backend/models/prediction_models.go`（末尾追加）

- [ ] **Step 1: 在 prediction_models.go 末尾追加 RecommendStock 模型**

```go
// RecommendStock 推荐股票，从 candidate_snapshot_item 提炼，独立于快照生命周期。
// 同 stockCode + tradeDate upsert 写入，支持物理删除而不影响审计链。
type RecommendStock struct {
	ID            uint      `json:"id" gorm:"primarykey"`
	StockCode     string    `json:"stockCode" gorm:"size:20;index:idx_recommend_stock_code_date;uniqueIndex:uidx_recommend_stock_code_date"`
	StockName     string    `json:"stockName" gorm:"size:50"`
	Industry      string    `json:"industry" gorm:"size:100"`
	Concept       string    `json:"concept" gorm:"size:500"`
	SourceCount   int       `json:"sourceCount"`
	SourceScore   float64   `json:"sourceScore"`
	BestScene     string    `json:"bestScene" gorm:"size:50"`
	ShortScore    float64   `json:"shortScore"`
	SwingScore    float64   `json:"swingScore"`
	TrendScore    float64   `json:"trendScore"`
	LongTermScore float64   `json:"longTermScore"`
	SourcesJSON   string    `json:"sourcesJson" gorm:"type:text"`
	ReasonsJSON   string    `json:"reasonsJson" gorm:"type:text"`
	RisksJSON     string    `json:"risksJson" gorm:"type:text"`
	AdviceJSON    string    `json:"adviceJson" gorm:"type:text"`
	SnapshotID    uint      `json:"snapshotId" gorm:"index"`
	TradeDate     string    `json:"tradeDate" gorm:"size:10;index:idx_recommend_stock_code_date;uniqueIndex:uidx_recommend_stock_code_date"`
	CreatedAt     time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt     time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (RecommendStock) TableName() string { return "recommend_stock" }
```

- [ ] **Step 2: 在 ensurePredictionTables 中加 AutoMigrate**

文件：`backend/backtest/api.go` 的 `ensurePredictionTables` 函数末尾，在现有 `&models.ScreeningExecutionItem{}` 后加：

```go
&models.RecommendStock{},
```

完整的 `ensurePredictionTables` 末尾应该是：

```go
&models.ScreeningExecutionSnapshot{},
&models.ScreeningExecutionItem{},
&models.RecommendStock{},
```

---

### Task 2: Generate 同步写入 recommend_stock

**Files:**
- Modify: `backend/backtest/candidate_service.go`（Generate 方法末尾）

- [ ] **Step 1: 在 Generate 方法的 Transaction 提交后、return 之前，插入 upsert 逻辑**

位置：`candidate_service.go` 第 273-275 行之间，`snapshot.CandidateCount, snapshot.Coverage, snapshot.RawHash = len(items), coverage, rawHash` 之后，`return &CandidateSnapshotDetails{...}` 之前。

```go
// 同步写入 recommend_stock，同 stockCode+tradeDate 覆盖
if len(items) > 0 {
    recommendRows := make([]models.RecommendStock, 0, len(items))
    for _, item := range items {
        recommendRows = append(recommendRows, models.RecommendStock{
            StockCode:     item.StockCode,
            StockName:     item.StockName,
            Industry:      item.Industry,
            Concept:       item.Concept,
            SourceCount:   item.SourceCount,
            SourceScore:   item.SourceScore,
            BestScene:     item.BestScene,
            ShortScore:    item.ShortScore,
            SwingScore:    item.SwingScore,
            TrendScore:    item.TrendScore,
            LongTermScore: item.LongTermScore,
            SourcesJSON:   item.SourcesJSON,
            ReasonsJSON:   item.ReasonsJSON,
            RisksJSON:     item.RisksJSON,
            AdviceJSON:    item.AdviceJSON,
            SnapshotID:    snapshot.ID,
            TradeDate:     request.TradeDate,
        })
    }
    if err := db.Dao.Clauses(clause.OnConflict{
        Columns:   []clause.Column{{Name: "stock_code"}, {Name: "trade_date"}},
        DoUpdates: clause.AssignmentColumns([]string{"stock_name", "industry", "concept", "source_count", "source_score", "best_scene", "short_score", "swing_score", "trend_score", "long_term_score", "sources_json", "reasons_json", "risks_json", "advice_json", "snapshot_id", "updated_at"}),
    }).CreateInBatches(recommendRows, 200).Error; err != nil {
        logger.SugaredLogger.Warnf("upsert recommend_stock for snapshot %d: %v", snapshot.ID, err)
    }
}
```

---

### Task 3: 新增 recommend_stock 查询/删除方法

**Files:**
- Modify: `backend/backtest/candidate_service.go`（末尾追加）

- [ ] **Step 1: 追加 ListRecommendStocks 方法**

```go
// RecommendStockQuery 推荐股票查询参数
type RecommendStockQuery struct {
	Scene    string `json:"scene"`
	Keyword  string `json:"keyword"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

// RecommendStockPageData 推荐股票分页结果
type RecommendStockPageData struct {
	List       []models.RecommendStock `json:"list"`
	Total      int64                   `json:"total"`
	Page       int                     `json:"page"`
	PageSize   int                     `json:"pageSize"`
	TotalPages int                     `json:"totalPages"`
}

// ListRecommendStocks 取最新 tradeDate 的推荐股票，按场景分排序，支持搜索和分页
func (s *CandidatePoolService) ListRecommendStocks(query RecommendStockQuery) (*RecommendStockPageData, error) {
	if db.Dao == nil {
		return nil, fmt.Errorf("数据库未初始化")
	}
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 || query.PageSize > 200 {
		query.PageSize = 50
	}

	// 取最新 tradeDate
	var latestDate string
	db.Dao.Model(&models.RecommendStock{}).Select("MAX(trade_date)").Scan(&latestDate)
	if latestDate == "" {
		return &RecommendStockPageData{List: []models.RecommendStock{}, Page: query.Page, PageSize: query.PageSize}, nil
	}

	// 按 stockCode 去重（保留最新 updated_at），用子查询
	subQuery := db.Dao.Model(&models.RecommendStock{}).
		Select("stock_code, MAX(updated_at) AS max_updated").
		Where("trade_date = ?", latestDate).
		Group("stock_code")

	q := db.Dao.Model(&models.RecommendStock{}).
		Joins("INNER JOIN (?) AS latest ON recommend_stock.stock_code = latest.stock_code AND recommend_stock.updated_at = latest.max_updated", subQuery)

	if query.Keyword != "" {
		keyword := "%" + query.Keyword + "%"
		q = q.Where("(recommend_stock.stock_code LIKE ? OR recommend_stock.stock_name LIKE ?)", keyword, keyword)
	}

	// 按场景排序
	orderField := "short_score"
	switch query.Scene {
	case "短线爆发":
		orderField = "short_score"
	case "波段反弹":
		orderField = "swing_score"
	case "趋势持有":
		orderField = "trend_score"
	default:
		orderField = "source_score"
	}
	q = q.Order(orderField + " DESC, recommend_stock.stock_code ASC")

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}

	var list []models.RecommendStock
	offset := (query.Page - 1) * query.PageSize
	if err := q.Offset(offset).Limit(query.PageSize).Find(&list).Error; err != nil {
		return nil, err
	}

	totalPages := int((total + int64(query.PageSize) - 1) / int64(query.PageSize))
	return &RecommendStockPageData{
		List:       list,
		Total:      total,
		Page:       query.Page,
		PageSize:   query.PageSize,
		TotalPages: totalPages,
	}, nil
}
```

- [ ] **Step 2: 追加 DeleteRecommendStock 和 BatchDeleteRecommendStocks 方法**

```go
// DeleteRecommendStock 物理删除单条推荐股票
func (s *CandidatePoolService) DeleteRecommendStock(id uint) error {
	if db.Dao == nil {
		return fmt.Errorf("数据库未初始化")
	}
	result := db.Dao.Where("id = ?", id).Delete(&models.RecommendStock{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("推荐股票不存在")
	}
	return nil
}

// BatchDeleteRecommendStocks 批量物理删除推荐股票
func (s *CandidatePoolService) BatchDeleteRecommendStocks(ids []uint) (int64, error) {
	if db.Dao == nil {
		return 0, fmt.Errorf("数据库未初始化")
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := db.Dao.Where("id IN ?", ids).Delete(&models.RecommendStock{})
	return result.RowsAffected, result.Error
}
```

- [ ] **Step 3: 修改 ListSnapshots 过滤 expired 状态**

```go
// 修改 existing ListSnapshots，加一行 Where 过滤
func (s *CandidatePoolService) ListSnapshots(limit int, scene string) []models.CandidateSnapshot {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var snapshots []models.CandidateSnapshot
	query := db.Dao.Where("status != ?", "expired").Order("created_at desc").Limit(limit)
	if scene != "" {
		query = query.Where("scene = ?", scene)
	}
	_ = query.Find(&snapshots).Error
	return snapshots
}
```

---

### Task 4: GetStockPool 新增 snapshot_all 分支

**Files:**
- Modify: `backend/backtest/stock_pool.go`

- [ ] **Step 1: 在 GetStockPool switch 中加 snapshot_all 分支**

在 `GetStockPool` 函数中，`case "自选股":` 之前加入：

```go
case "snapshot_all":
    return s.getRecommendStockCodes()
```

- [ ] **Step 2: 追加 getRecommendStockCodes 方法**

```go
func (s *StockPoolService) getRecommendStockCodes() []string {
    if db.Dao == nil {
        return nil
    }
    var codes []string
    db.Dao.Model(&models.RecommendStock{}).
        Distinct("stock_code").
        Order("stock_code asc").
        Pluck("stock_code", &codes)
    return codes
}
```

---

### Task 5: app.go 新增 API

**Files:**
- Modify: `app.go`

- [ ] **Step 1: 在 `DeleteCandidateSnapshot` 或 `BatchDeleteCandidateSnapshots` 之后追加三个新 API**

```go
// GetRecommendStocks 获取推荐股票列表
func (a *App) GetRecommendStocks(query backtest.RecommendStockQuery) *backtest.RecommendStockPageData {
    result, err := backtest.NewCandidatePoolService().ListRecommendStocks(query)
    if err != nil {
        logger.SugaredLogger.Errorf("GetRecommendStocks error: %v", err)
        return &backtest.RecommendStockPageData{List: []models.RecommendStock{}, Page: query.Page, PageSize: query.PageSize}
    }
    return result
}

// DeleteRecommendStock 删除单条推荐股票
func (a *App) DeleteRecommendStock(id uint) string {
    if err := backtest.NewCandidatePoolService().DeleteRecommendStock(id); err != nil {
        return "删除推荐股票失败: " + err.Error()
    }
    return "推荐股票已删除"
}

// BatchDeleteRecommendStocks 批量删除推荐股票
func (a *App) BatchDeleteRecommendStocks(ids []uint) map[string]any {
    deleted, err := backtest.NewCandidatePoolService().BatchDeleteRecommendStocks(ids)
    if err != nil {
        return map[string]any{"code": 0, "msg": err.Error()}
    }
    return map[string]any{"code": 1, "deleted": deleted}
}
```

---

### Task 6: 前端机会发现重构 - 推荐股票 Tab

**Files:**
- Modify: `frontend/src/components/PredictionFactory.vue`

- [ ] **Step 1: 导入新 API 函数**

在现有 `import { ... } from '../../wailsjs/go/main/App'` 块中追加：

```js
GetRecommendStocks,
DeleteRecommendStock,
BatchDeleteRecommendStocks,
```

- [ ] **Step 2: 新增推荐股票相关的响应式状态**

在 `const candidateSnapshots = ref([])` 后追加：

```js
const recommendStocks = ref([])
const recommendTotal = ref(0)
const recommendPage = ref(1)
const recommendPageSize = 30
const recommendScene = ref('')
const recommendKeyword = ref('')
const recommendLoading = ref(false)
const selectedRecommendIds = ref([])
const recommendTab = ref('stocks') // 'stocks' | 'history'
```

- [ ] **Step 3: 新增推荐股票加载/删除/送入量化函数**

```js
async function loadRecommendStocks() {
  recommendLoading.value = true
  try {
    const res = await GetRecommendStocks({
      scene: recommendScene.value || '',
      keyword: recommendKeyword.value || '',
      page: recommendPage.value,
      pageSize: recommendPageSize,
    })
    recommendStocks.value = res?.list || []
    recommendTotal.value = res?.total || 0
  } catch (err) {
    console.warn('加载推荐股票失败', err)
  } finally {
    recommendLoading.value = false
  }
}

async function deleteRecommendStockRow(row) {
  try {
    const res = await DeleteRecommendStock(row.id)
    if (String(res).includes('失败')) {
      message.error(res)
    } else {
      message.success(res)
      await loadRecommendStocks()
    }
  } catch (err) {
    message.error('删除推荐股票失败: ' + (err?.message || String(err)))
  }
}

async function batchDeleteRecommendStocks() {
  if (!selectedRecommendIds.value.length) {
    message.warning('请先勾选推荐股票')
    return
  }
  try {
    const res = await BatchDeleteRecommendStocks(selectedRecommendIds.value)
    message.success(`已删除 ${res.deleted || 0} 条`)
    selectedRecommendIds.value = []
    await loadRecommendStocks()
  } catch (err) {
    message.error('批量删除失败: ' + (err?.message || String(err)))
  }
}

function toggleRecommendSelect(row) {
  const idx = selectedRecommendIds.value.indexOf(row.id)
  if (idx >= 0) {
    selectedRecommendIds.value.splice(idx, 1)
  } else {
    selectedRecommendIds.value.push(row.id)
  }
}

function toggleAllRecommend() {
  if (selectedRecommendIds.value.length === recommendStocks.value.length) {
    selectedRecommendIds.value = []
  } else {
    selectedRecommendIds.value = recommendStocks.value.map(r => r.id)
  }
}

async function sendRecommendToQuantify(row) {
  const codes = row ? [row.stockCode] : selectedRecommendIds.value.map(id => {
    const found = recommendStocks.value.find(r => r.id === id)
    return found?.stockCode || ''
  }).filter(Boolean)
  if (!codes.length) {
    message.warning('请先勾选推荐股票')
    return
  }
  const scope = 'stock_' + codes.join(',')
  try {
    const res = await CreatePredictionSession(
      form.value.scene,
      scope,
      formatDate(form.value.startDate),
      formatDate(form.value.endDate),
      form.value.aiConfigId || 0
    )
    if (res.code === 1) {
      sessionResult.value = res.data
      sessionViewMode.value = 'current'
      activeWorkspace.value = 'analysis'
      activeAnalysisView.value = 'decisions'
      await Promise.all([loadMyHypotheses(), loadPredictionSessions(), loadPredictionAlerts()])
      message.success('已送入量化验证')
    } else {
      message.error(res.msg || '送入量化失败')
    }
  } catch (err) {
    message.error('送入量化失败: ' + (err?.message || String(err)))
  }
}

function recommendSourceTags(row) {
  try {
    const sources = JSON.parse(row.sourcesJson || '[]')
    return sources.map(s => sourceLabel(s))
  } catch {
    return []
  }
}

function recommendSceneScore(row) {
  const scene = recommendScene.value || row.bestScene || '短线爆发'
  if (scene === '波段反弹') return row.swingScore
  if (scene === '趋势持有') return row.trendScore
  return row.shortScore
}
```

- [ ] **Step 4: 在 onMounted 中加载推荐股票**

在 `onMounted` 中 `await loadCandidateSnapshots()` 后加：

```js
await loadRecommendStocks()
```

- [ ] **Step 5: watch recommendScene 和 recommendKeyword 时重新加载**

```js
watch(() => recommendScene.value, async () => {
  recommendPage.value = 1
  await loadRecommendStocks()
})
watch(() => recommendKeyword.value, async () => {
  recommendPage.value = 1
  await loadRecommendStocks()
})
```

- [ ] **Step 6: 替换机会发现卡片为双 tab 布局**

将现有 `opportunity-page` 的内容替换为：

```html
<section class="workspace-page opportunity-page">
  <n-alert type="info" :show-icon="false">
    五类页面数据先形成候选快照；推荐股票可独立删除，历史快照为审计记录不可删除。
  </n-alert>

  <!-- 五源候选池生成面板（折叠） -->
  <n-card size="small" title="五源候选池生成" class="candidate-command-card" :content-style="candidateFormExpanded ? '' : 'display:none'">
    <template #header-extra>
      <n-button text size="small" @click="candidateFormExpanded = !candidateFormExpanded">
        {{ candidateFormExpanded ? '收起' : '展开' }}
      </n-button>
    </template>
    <n-form :model="candidateForm" label-placement="top" :show-feedback="false" class="candidate-form">
      <n-form-item label="投资场景"><n-select v-model:value="candidateForm.scene" :options="candidateScenes"/></n-form-item>
      <n-form-item label="数据来源" class="candidate-source-field">
        <n-select v-model:value="candidateForm.sources" multiple :options="candidateSourceOptions" max-tag-count="responsive"/>
      </n-form-item>
      <n-form-item label="合并方式">
        <n-select v-model:value="candidateForm.sourceMode" :options="[
          {label: '并集：任一来源命中', value: 'union'},
          {label: '共识：至少 N 个来源', value: 'consensus'},
          {label: '交集：全部来源命中', value: 'intersection'}
        ]"/>
      </n-form-item>
      <n-form-item v-if="candidateForm.sourceMode === 'consensus'" label="最少来源">
        <n-input-number v-model:value="candidateForm.minimumSources" :min="2" :max="candidateForm.sources.length || 2"/>
      </n-form-item>
      <n-form-item label="数据日期"><n-input v-model:value="candidateForm.tradeDate" placeholder="YYYY-MM-DD"/></n-form-item>
      <n-form-item label="最多候选"><n-input-number v-model:value="candidateForm.limit" :min="10" :max="500"/></n-form-item>
      <n-form-item class="candidate-generate-action">
        <n-button type="primary" :loading="candidateLoading" @click="generateCandidates()">生成候选快照</n-button>
      </n-form-item>
    </n-form>
  </n-card>

  <!-- 双 Tab -->
  <n-tabs v-model:value="recommendTab" type="line" animated class="recommend-tabs">
    <!-- Tab 1: 推荐股票 -->
    <n-tab-pane name="stocks">
      <template #tab><span class="tab-label">推荐股票</span></template>
      <section class="recommend-panel">
        <div class="panel-heading">
          <span>推荐股票</span>
          <n-text depth="3">共 {{ recommendTotal }} 只</n-text>
          <n-space size="small">
            <n-button size="tiny" :disabled="!selectedRecommendIds.length" @click="batchDeleteRecommendStocks">
              批量删除 ({{ selectedRecommendIds.length }})
            </n-button>
            <n-button size="tiny" type="primary" :disabled="!selectedRecommendIds.length" @click="sendRecommendToQuantify()">
              送入量化 ({{ selectedRecommendIds.length }})
            </n-button>
          </n-space>
        </div>
        <n-space align="center" class="recommend-filter">
          <n-select v-model:value="recommendScene" :options="[{label:'全部',value:''},...scenes]" size="small" style="width:120px" placeholder="场景"/>
          <n-input v-model:value="recommendKeyword" size="small" placeholder="搜索股票代码/名称" clearable style="width:200px"/>
        </n-space>
        <n-spin :show="recommendLoading">
          <div class="table-shell" v-if="recommendStocks.length">
            <n-table size="small" :bordered="false" :single-line="false">
              <thead>
                <tr>
                  <th><n-checkbox :checked="selectedRecommendIds.length === recommendStocks.length" @update:checked="toggleAllRecommend"/></th>
                  <th>股票</th>
                  <th>来源</th>
                  <th>短线</th>
                  <th>波段</th>
                  <th>趋势</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="row in recommendStocks" :key="row.id">
                  <td><n-checkbox :checked="selectedRecommendIds.includes(row.id)" @update:checked="toggleRecommendSelect(row)"/></td>
                  <td>
                    <n-text strong>{{ row.stockName || row.stockCode }}</n-text>
                    <br><n-text depth="3">{{ row.stockCode }} · {{ row.industry || '-' }}</n-text>
                  </td>
                  <td><n-space size="small"><n-tag v-for="s in recommendSourceTags(row)" :key="s" size="small" :bordered="false">{{ s }}</n-tag></n-space></td>
                  <td :class="{highlight: recommendScene === '短线爆发' || !recommendScene}">{{ row.shortScore.toFixed(1) }}</td>
                  <td :class="{highlight: recommendScene === '波段反弹'}">{{ row.swingScore.toFixed(1) }}</td>
                  <td :class="{highlight: recommendScene === '趋势持有'}">{{ row.trendScore.toFixed(1) }}</td>
                  <td>
                    <n-space size="small" :wrap="false">
                      <n-button size="tiny" type="primary" @click="sendRecommendToQuantify(row)">送入量化</n-button>
                      <n-button size="tiny" quaternary circle type="error" @click="deleteRecommendStockRow(row)">
                        <template #icon><n-icon :component="TrashOutline" size="14"/></template>
                      </n-button>
                    </n-space>
                  </td>
                </tr>
              </tbody>
            </n-table>
          </div>
          <n-empty v-else description="暂无推荐股票，请先生成候选快照"/>
          <div class="pagination-row" v-if="recommendTotal > recommendPageSize">
            <n-pagination
              v-model:page="recommendPage"
              :page-size="recommendPageSize"
              :item-count="recommendTotal"
              size="small"
            />
          </div>
        </n-spin>
      </section>
    </n-tab-pane>

    <!-- Tab 2: 历史快照 -->
    <n-tab-pane name="history">
      <template #tab><span class="tab-label">历史快照</span></template>
      <div class="candidate-layout">
        <n-card size="small" class="candidate-history-card">
          <template #header>
            <div class="panel-heading">
              <span>历史快照</span>
              <n-space size="small">
                <n-button size="tiny" quaternary @click="batchDeleteCandidateSnapshots">归档全部</n-button>
              </n-space>
            </div>
          </template>
          <div class="candidate-history-list" v-if="candidateSnapshots.length">
            <button v-for="row in candidateSnapshots" :key="row.id" type="button"
                    class="candidate-history-item"
                    :class="{active: candidateDetails?.snapshot?.id === row.id}"
                    @click="openCandidateSnapshot(row.id)">
              <span><strong>{{ row.name || `快照 #${row.id}` }}</strong><small>{{ row.tradeDate }} · {{ row.scene }}</small></span>
              <n-tag size="small" :type="row.status === 'failed' ? 'error' : (row.status === 'validated' ? 'success' : 'info')">
                {{ row.candidateCount || 0 }} 只
              </n-tag>
            </button>
          </div>
          <n-empty v-else size="small" description="尚未生成候选快照"/>
        </n-card>

        <n-card size="small" class="candidate-result-card">
          <template #header>
            <div class="panel-heading">
              <span>候选结果</span>
              <n-space v-if="candidateDetails?.snapshot" align="center" size="small">
                <n-tag size="small" type="info">覆盖 {{ formatRatio(candidateDetails.snapshot.coverage) }}</n-tag>
                <n-button size="small" type="primary" :disabled="candidateForm.scene === '长期配置'" :loading="candidateLoading" @click="validateCandidateSnapshot">
                  进入量化验证
                </n-button>
              </n-space>
            </div>
          </template>
          <n-spin :show="candidateLoading">
            <div class="candidate-freshness" v-if="candidateDetails?.freshness">
              <n-tag v-for="fresh in Object.values(candidateDetails.freshness)" :key="fresh.source" size="small"
                     :type="fresh.available ? 'success' : 'warning'">
                {{ sourceLabel(fresh.source) }} {{ fresh.itemCount || 0 }} · {{ formatDateTime(fresh.availableAt) }}
              </n-tag>
            </div>
            <n-alert v-if="candidateForm.scene === '长期配置'" type="warning" :show-icon="false" class="candidate-long-warning">
              当前五源缺少历史时点基本面、估值和财务质量，长期评分标记为不可用，不输出长期买入建议。
            </n-alert>
            <div class="table-shell" v-if="candidateDetails?.items?.length">
              <n-table size="small" :bordered="false" :single-line="false" class="candidate-table">
                <thead><tr><th>排名</th><th>股票</th><th>来源</th><th>规则适配分</th><th>最适场景</th><th>候选建议</th><th></th></tr></thead>
                <tbody>
                  <tr v-for="(row, index) in candidateDetails.items" :key="row.id || row.stockCode">
                    <td>#{{ index + 1 }}</td>
                    <td><n-text strong>{{ row.stockName || row.stockCode }}</n-text><br><n-text depth="3">{{ row.stockCode }} · {{ row.industry || '-' }}</n-text></td>
                    <td><n-space size="small"><n-tag v-for="source in candidateSources(row)" :key="source" size="small" :bordered="false">{{ sourceLabel(source) }}</n-tag></n-space></td>
                    <td><n-text strong v-if="sceneScore(row) >= 0">{{ sceneScore(row).toFixed(1) }}</n-text><n-tag v-else size="small" type="warning">数据不足</n-tag></td>
                    <td>{{ row.bestScene || '-' }}</td>
                    <td><n-tag size="small" type="info">{{ candidateAdvice(row).action || 'WATCH' }}</n-tag><br><n-text depth="3">仅候选</n-text></td>
                    <td><n-button size="tiny" text type="primary" @click="openCandidateAdvice(row)">依据与风险</n-button></td>
                  </tr>
                </tbody>
              </n-table>
            </div>
            <n-empty v-else description="选择历史快照查看候选结果"/>
          </n-spin>
        </n-card>
      </div>
    </n-tab-pane>
  </n-tabs>
</section>
```

---

### Task 7: 前端 - 分析工作台股票池改造

**Files:**
- Modify: `frontend/src/components/PredictionFactory.vue`

- [ ] **Step 1: scopes 改为包含五源推荐股票，移除 snapshot 独立选项**

```js
const scopes = [
  {label: '我的自选股', value: '自选股'},
  {label: '五源推荐股票', value: 'snapshot_all'},
  {label: '指定股票', value: 'stock'},
]
```

- [ ] **Step 2: resolveScope 简化，去掉 snapshot 分支**

`resolveScope` 函数中不再需要 `snapshot` 分支的处理，因为 `snapshot_all` 直接传给后端不需要转换：

```js
function resolveScope(showWarning = true) {
  let scope = form.value.stockScope
  if (scope === 'stock') {
    if (!form.value.stockCode) {
      if (showWarning) message.warning('请输入股票代码')
      return ''
    }
    scope = 'stock_' + form.value.stockCode
  }
  return scope
}
```

- [ ] **Step 3: 移除模板中的 snapshot 候选快照选择器**

删除 `v-if="form.stockScope === 'snapshot'"` 的 `<n-form-item>` 块。

- [ ] **Step 4: 移除 form 中不再需要的 snapshotId 字段**

```js
const form = ref({
  scene: '短线爆发',
  stockScope: '自选股',
  stockCode: '',
  startDate: defaultDates.startDate,
  endDate: defaultDates.endDate,
  aiConfigId: null,
})
```

- [ ] **Step 5: 移除不再需要的 snapshotScopeOptions computed**

---

### Task 8: 清理前端旧导入和废弃代码

- [ ] **Step 1: 移除不再需要的导入**

从 `import { ... } from '../../wailsjs/go/main/App'` 中移除：
- `DeleteCandidateSnapshot`（前端不再直接调用单个归档，批量归档按钮用 `BatchDeleteCandidateSnapshots`）
- `GetCandidateSnapshot`（历史快照 tab 仍需要，保留）
- `GetCandidateSnapshots`（保留，历史快照 tab 仍需要）
- `CreatePredictionSessionFromCandidate`（保留，历史快照的"进入量化验证"按钮仍需要）

- [ ] **Step 2: 移除不再需要的函数**

可以保留 `deleteCandidateSnapshot` 函数但不再在模板中引用，或者直接删除该函数。

- [ ] **Step 3: 新增 candidateFormExpanded 状态**

```js
const candidateFormExpanded = ref(false)
```

---

### Task 9: 验证和测试

- [ ] **Step 1: 确保后端 API 注册正确**

Wails 构建时会自动扫描 `app.go` 中的 public 方法生成 bindings。确认 `GetRecommendStocks`、`DeleteRecommendStock`、`BatchDeleteRecommendStocks` 三个方法签名符合 Wails 导出规范（返回值类型清晰）。

- [ ] **Step 2: 前端构建验证**

```bash
cd frontend && npm run build
```

- [ ] **Step 3: 完整构建验证**

```bash
wails build --clean --platform windows/amd64 --nsis
```

---

## Self-Review

1. **Spec coverage:**
   - 推荐股票表 (recommend_stock): Task 1
   - Generate 同步写入: Task 2
   - 推荐股票查询/删除 API: Task 3, 5
   - 历史快照列表过滤 expired: Task 3
   - snapshot_all 股票池: Task 4
   - 前端推荐股票 tab (表格/勾选/筛选/删除/送入量化): Task 6
   - 前端历史快照 tab (归档全部): Task 6
   - 分析工作台股票池改造: Task 7
   - 清理旧代码: Task 8

2. **Placeholder scan:** No TBD, TODO, or placeholder patterns found.

3. **Type consistency:**
   - `RecommendStock` model fields match `candidate_snapshot_item` fields used in Task 2
   - `RecommendStockQuery` fields match API call in Task 6
   - `RecommendStockPageData` fields match template rendering in Task 6
   - `snapshot_all` scope in GetStockPool matches scopes array in Task 7