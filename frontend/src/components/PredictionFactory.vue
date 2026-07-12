<script setup>
import {computed, onMounted, onUnmounted, ref, nextTick, watch} from 'vue'
import * as echarts from 'echarts'
import {
  NButton,
  NCard,
  NForm,
  NFormItem,
  NSelect,
  NSpace,
  NSpin,
  NTag,
  NText,
  NEmpty,
  NInput,
  NInputNumber,
  NDatePicker,
  NProgress,
  NAlert,
  NTable,
  NModal,
  NPagination,
  NTabs,
  NTabPane,
  NDrawer,
  NDrawerContent,
  NIcon,
  NTooltip,
  useMessage
} from 'naive-ui'
import {
  AddOutline,
  AnalyticsOutline,
  ChevronForwardOutline,
  EyeOutline,
  ListOutline,
  NotificationsOutline,
  PlayOutline,
  PulseOutline,
  RefreshOutline,
  SettingsOutline,
  SyncOutline,
  TimeOutline,
  TrendingUpOutline,
  WalletOutline
} from '@vicons/ionicons5'
import {
  AddTradingRecord,
  CreatePredictionSession,
  RecalculatePredictionSession,
  GetPredictionSession,
  GetPredictionSessions,
  GetMyPredictionHypotheses,
  SavePredictionObservation,
  SavePredictionHypothesis,
  ActivatePredictionHypothesis,
  GetPredictionPaperTradingDetails,
  GetPredictionHypothesisDailyNAV,
  GetPredictionBacktestTrades,
  StartFeatureSync,
  GetFeatureSyncJob,
  GetFeatureCoverage,
  GetFeatureFreshness,
  GetPredictionCronStatus,
  RunPredictionCronTaskNow,
  ScanPredictionAlertsNow,
  GetPredictionAlertLogs,
  MarkPredictionAlertStatus,
  GetAiConfigs,
  GetTradingPositionSummaries,
  GetTradingRecordsByStock,
  GenerateCandidateSnapshot,
  GetCandidateSnapshot,
  GetCandidateSnapshots,
  CreatePredictionSessionFromCandidate
} from '../../wailsjs/go/main/App'

const scenes = [
  {label: '短线爆发', value: '短线爆发'},
  {label: '波段反弹', value: '波段反弹'},
  {label: '趋势持有', value: '趋势持有'},
]

const candidateScenes = [
  ...scenes,
  {label: '长期配置（仅观察）', value: '长期配置'},
]
const candidateSourceOptions = [
  {label: '股票推荐记录', value: 'recommendation'},
  {label: '异动监控', value: 'event'},
  {label: '涨停梯队', value: 'uplimit'},
  {label: '形态选股', value: 'pattern'},
  {label: '指标选股', value: 'indicator'},
]

const scopes = [
  {label: '我的自选股', value: '自选股'},
  {label: '指定股票', value: 'stock'},
]

function getDefaultDates() {
  const now = new Date()
  const year = now.getFullYear()
  const start = new Date(year, 0, 1)
  return {
    startDate: start.getTime(),
    endDate: now.getTime()
  }
}

const defaultDates = getDefaultDates()
const form = ref({
  scene: '短线爆发',
  stockScope: '自选股',
  stockCode: '',
  startDate: defaultDates.startDate,
  endDate: defaultDates.endDate,
  aiConfigId: null,
})

const message = useMessage()
const loading = ref(false)
const recalculateLoading = ref(false)
const syncLoading = ref(false)
const aiConfigOptions = ref([])
const aiConfigLoading = ref(false)
const myHypotheses = ref([])
const sessionResult = ref(null)
const backtestTrades = ref({})
const backtestTradeLoading = ref({})
const expandedStocks = ref({})
const recentSessions = ref([])
const listPageSize = 5
const backtestPage = ref(1)
const historyPage = ref(1)
const monitorPage = ref(1)
const alertPage = ref(1)
const positionRows = ref([])
const positionLoading = ref(false)
const alertRows = ref([])
const alertLoading = ref(false)
const tradeRecords = ref([])
const selectedPosition = ref(null)
const showRecordsModal = ref(false)
const showTradeModal = ref(false)
const showPaperModal = ref(false)
const paperDetailsLoading = ref(false)
const paperDetails = ref(null)
const selectedPaperHypothesis = ref(null)
const paperChartRef = ref(null)
const tradeForm = ref({
  StockCode: '',
  StockName: '',
  Direction: '买入',
  Price: 0,
  Volume: 0,
  TradingTime: Date.now(),
  Fee: 0,
  Reason: '',
})
const tradeDirectionOptions = [
  {label: '买入', value: '买入'},
  {label: '卖出', value: '卖出'},
]
const chartRef = ref(null)
const chartData = ref([])
const featureCoverage = ref(null)
const featureFreshness = ref(null)
const syncJob = ref(null)
const cronStatus = ref([])
let syncTimer = null
let positionTimer = null
const positionRefreshIntervalMs = 15000
const activeWorkspace = ref('analysis')
const activeAnalysisView = ref('decisions')
const activeMonitorView = ref('history')
const strategyDetailView = ref('attribution')
const selectedBacktestId = ref(null)
const selectedDecision = ref(null)
const decisionDrawerVisible = ref(false)
const sessionViewMode = ref('history')
const candidateLoading = ref(false)
const candidateSnapshots = ref([])
const candidateDetails = ref(null)
const candidateDrawerVisible = ref(false)
const selectedCandidate = ref(null)
const candidateForm = ref({
  scene: '短线爆发',
  stockScope: '全部A股',
  tradeDate: formatDate(Date.now()),
  sources: candidateSourceOptions.map(item => item.value),
  sourceMode: 'union',
  minimumSources: 1,
  limit: 100,
  indicatorQuery: '',
  stockCodes: [],
  sourceItems: [],
  sourceItemSource: '',
})
let chartInstance = null
let paperChartInstance = null

onMounted(async () => {
  loadPositionRows()
  positionTimer = setInterval(() => {
    if (document.visibilityState === 'visible') {
      loadPositionRows(true)
    }
  }, positionRefreshIntervalMs)
  loadAiConfigs()
  loadFeatureStatus()
  loadCronStatus()
  loadPredictionAlerts()
  await Promise.all([
    loadMyHypotheses(),
    loadPredictionSessions()
  ])
  await openLatestSession()
  await loadCandidateSnapshots()
})

onUnmounted(() => {
  if (syncTimer) clearInterval(syncTimer)
  if (positionTimer) clearInterval(positionTimer)
  if (chartInstance) {
    chartInstance.dispose()
    chartInstance = null
  }
  if (paperChartInstance) {
    paperChartInstance.dispose()
    paperChartInstance = null
  }
})

watch(() => [form.value.stockScope, form.value.stockCode], async () => {
  await loadPositionRows()
  await loadFeatureStatus()
})

watch(() => [form.value.startDate, form.value.endDate], async () => {
  await loadFeatureStatus()
})

watch(() => sessionResult.value?.session?.id, () => {
  backtestPage.value = 1
  backtestTrades.value = {}
  expandedStocks.value = {}
  selectedBacktestId.value = null
  chartData.value = []
})

watch(() => recentSessions.value.length, (length) => {
  historyPage.value = normalizePage(historyPage.value, length)
})

watch(() => myHypotheses.value.length, (length) => {
  monitorPage.value = normalizePage(monitorPage.value, length)
})

watch(() => alertRows.value.length, (length) => {
  alertPage.value = normalizePage(alertPage.value, length)
})

const sortedBacktestHypotheses = computed(() => sortLatestFirst(sessionResult.value?.hypotheses || []))
const pagedBacktestHypotheses = computed(() => paginate(sortedBacktestHypotheses.value, backtestPage.value))
const sortedRecentSessions = computed(() => sortLatestFirst(recentSessions.value))
const pagedRecentSessions = computed(() => paginate(sortedRecentSessions.value, historyPage.value))
const sortedMyHypotheses = computed(() => sortLatestFirst(myHypotheses.value))
const pagedMyHypotheses = computed(() => paginate(sortedMyHypotheses.value, monitorPage.value))
const sortedAlertRows = computed(() => sortLatestFirst(
  (alertRows.value || []).filter(row => String(row?.status || '').toLowerCase() !== 'ignored')
))
const pagedAlertRows = computed(() => paginate(sortedAlertRows.value, alertPage.value))
const currentDecisions = computed(() => sessionResult.value?.decisions || [])
const decisionSummary = computed(() => {
  const rows = currentDecisions.value
  return {
    buy: rows.filter(row => ['BUY', 'ADD'].includes(String(row.action || '').toUpperCase())).length,
    hold: rows.filter(row => ['HOLD', 'WATCH'].includes(String(row.action || '').toUpperCase())).length,
    reduce: rows.filter(row => ['REDUCE', 'SELL', 'AVOID'].includes(String(row.action || '').toUpperCase())).length,
    highRisk: rows.filter(row => String(row.riskLevel || '').toLowerCase() === 'high').length,
    strategies: sortedBacktestHypotheses.value.length
  }
})
const selectedBacktestHypothesis = computed(() => {
  const rows = sortedBacktestHypotheses.value
  return rows.find(row => itemId(row) === Number(selectedBacktestId.value || 0)) || rows[0] || null
})
const selectedBacktestTrades = computed(() => {
  const id = itemId(selectedBacktestHypothesis.value)
  return id ? (backtestTrades.value[id] || []) : []
})
const selectedBacktestStocks = computed(() => {
  const id = itemId(selectedBacktestHypothesis.value)
  return id ? stockBacktestRows(id) : []
})
const monitorSummary = computed(() => ({
  active: sortedMyHypotheses.value.filter(row => row.status === 'active').length,
  forward: sortedMyHypotheses.value.filter(row => row.status === 'paper_trade').length,
  watch: sortedMyHypotheses.value.filter(row => ['watch', 'active_candidate'].includes(row.status)).length,
  unread: sortedAlertRows.value.filter(row => ['new', 'sent'].includes(String(row.status || '').toLowerCase())).length,
  positions: positionRows.value.filter(row => Number(row.currentVolume || 0) > 0).length
}))

watch(sortedBacktestHypotheses, (rows) => {
  if (!rows.length) {
    selectedBacktestId.value = null
    return
  }
  const selectedExists = rows.some(row => itemId(row) === Number(selectedBacktestId.value || 0))
  if (!selectedExists) selectedBacktestId.value = itemId(rows[0])
  if (activeAnalysisView.value === 'backtests') {
    void selectBacktest(selectedBacktestHypothesis.value)
  }
})

watch(activeAnalysisView, (view) => {
  if (view === 'backtests' && selectedBacktestHypothesis.value) {
    void selectBacktest(selectedBacktestHypothesis.value)
  }
})

function jsonValue(value, fallback) {
  if (value == null || value === '') return fallback
  if (typeof value !== 'string') return value
  try {
    return JSON.parse(value)
  } catch (_) {
    return fallback
  }
}

function candidateSources(row) {
  return jsonValue(row?.sourcesJson, [])
}

function candidateAdvice(row) {
  return jsonValue(row?.adviceJson, {})
}

function candidateReasons(row) {
  return jsonValue(row?.reasonsJson, [])
}

function candidateRisks(row) {
  return jsonValue(row?.risksJson, [])
}

function sourceLabel(source) {
  return candidateSourceOptions.find(item => item.value === source)?.label || source
}

function sceneScore(row, scene = candidateForm.value.scene) {
  if (scene === '波段反弹') return Number(row?.swingScore || 0)
  if (scene === '趋势持有') return Number(row?.trendScore || 0)
  if (scene === '长期配置') return Number(row?.longTermScore ?? -1)
  return Number(row?.shortScore || 0)
}

async function loadCandidateSnapshots() {
  try {
    candidateSnapshots.value = await GetCandidateSnapshots(30, '') || []
  } catch (err) {
    console.warn('加载候选快照失败', err)
  }
}

async function openCandidateSnapshot(snapshotId) {
  candidateLoading.value = true
  try {
    const res = await GetCandidateSnapshot(Number(snapshotId), '')
    if (!res || res.code !== 1) throw new Error(res?.msg || '候选快照不存在')
    candidateDetails.value = res.data
    candidateForm.value.scene = res.data?.snapshot?.scene || candidateForm.value.scene
  } catch (err) {
    message.error('打开候选快照失败: ' + (err?.message || String(err)))
  } finally {
    candidateLoading.value = false
  }
}

async function generateCandidates(overrides = {}) {
  if (!(overrides.sources || candidateForm.value.sources || []).length) {
    message.warning('至少选择一个候选来源')
    return
  }
  candidateLoading.value = true
  activeWorkspace.value = 'opportunities'
  try {
    const request = {...candidateForm.value, ...overrides}
    if (request.sources.length !== 1 || request.sources[0] !== request.sourceItemSource) {
      request.sourceItems = []
    }
    request.minimumSources = request.sourceMode === 'intersection'
        ? request.sources.length
        : Math.min(Number(request.minimumSources || 1), request.sources.length)
    const res = await GenerateCandidateSnapshot(request)
    if (!res || res.code !== 1) throw new Error(res?.msg || '生成候选池失败')
    candidateDetails.value = res.data
    await loadCandidateSnapshots()
    message.success(`已生成 ${res.data?.snapshot?.candidateCount || 0} 只候选，仅作为量化验证入口`)
  } catch (err) {
    message.error('生成候选池失败: ' + (err?.message || String(err)))
  } finally {
    candidateLoading.value = false
  }
}

function handleCandidateSourceRequest(payload = {}) {
  const source = String(payload.source || '').trim()
  const sources = candidateSourceOptions.some(item => item.value === source) ? [source] : candidateForm.value.sources
  candidateForm.value.scene = payload.scene || candidateForm.value.scene
  candidateForm.value.tradeDate = payload.tradeDate || formatDate(Date.now())
  candidateForm.value.indicatorQuery = payload.indicatorQuery || ''
  candidateForm.value.stockCodes = Array.isArray(payload.stockCodes) ? payload.stockCodes : []
  candidateForm.value.sourceItems = Array.isArray(payload.sourceItems) ? payload.sourceItems : []
  candidateForm.value.sourceItemSource = source
  void generateCandidates({
    sources,
    stockCodes: candidateForm.value.stockCodes,
    sourceItems: candidateForm.value.sourceItems,
    sourceItemSource: source,
    scene: candidateForm.value.scene,
    tradeDate: candidateForm.value.tradeDate,
    sourceMode: 'union',
    minimumSources: 1,
    indicatorQuery: payload.indicatorQuery || '',
  })
}

defineExpose({handleCandidateSourceRequest})

function openCandidateAdvice(row) {
  selectedCandidate.value = row
  candidateDrawerVisible.value = true
}

async function validateCandidateSnapshot() {
  const snapshot = candidateDetails.value?.snapshot
  if (!snapshot?.id) return
  if (candidateForm.value.scene === '长期配置') {
    message.warning('长期配置缺少点时基本面和估值，目前只能保存观察，不能进入正式量化验证')
    return
  }
  if (!form.value.aiConfigId) {
    message.warning('请先选择 AI 配置')
    return
  }
  candidateLoading.value = true
  try {
    const res = await CreatePredictionSessionFromCandidate(
        snapshot.id,
        candidateForm.value.scene,
        formatDate(form.value.startDate),
        formatDate(form.value.endDate),
        Number(form.value.aiConfigId)
    )
    if (!res || res.code !== 1) throw new Error(res?.msg || '量化验证创建失败')
    sessionResult.value = res.data
    sessionViewMode.value = 'current'
    form.value.scene = candidateForm.value.scene
    activeWorkspace.value = 'analysis'
    activeAnalysisView.value = 'backtests'
    await Promise.all([loadPredictionSessions(), loadMyHypotheses(), loadCandidateSnapshots()])
    message.success('候选快照已进入回测、裁决和前向验证链路')
  } catch (err) {
    message.error('进入量化验证失败: ' + (err?.message || String(err)))
  } finally {
    candidateLoading.value = false
  }
}

async function loadAiConfigs() {
  aiConfigLoading.value = true
  try {
    const res = await GetAiConfigs()
    aiConfigOptions.value = (res || []).map(item => ({
      label: item.name || `配置 ${item.ID}`,
      value: item.ID
    }))
    if (res && res.length > 0 && form.value.aiConfigId == null) {
      form.value.aiConfigId = res[0].ID
    }
  } catch (err) {
    message.error('加载 AI 配置失败: ' + (err?.message || String(err)))
  } finally {
    aiConfigLoading.value = false
  }
}

async function loadMyHypotheses() {
  try {
    myHypotheses.value = await GetMyPredictionHypotheses()
  } catch (err) {
    message.error('加载假设列表失败: ' + (err?.message || String(err)))
  }
}

async function loadPredictionSessions() {
  try {
    recentSessions.value = await GetPredictionSessions(50)
    return recentSessions.value
  } catch (err) {
    console.warn('加载预测历史失败', err)
    return []
  }
}

async function loadPositionRows(silent = false) {
  if (positionLoading.value) return
  positionLoading.value = true
  try {
    const scope = resolveScope(false)
    if (!scope) {
      positionRows.value = []
      return
    }
    positionRows.value = await GetTradingPositionSummaries(scope) || []
  } catch (err) {
    if (silent) {
      console.warn('刷新持仓信息失败', err)
    } else {
      message.error('加载持仓信息失败: ' + (err?.message || String(err)))
    }
  } finally {
    positionLoading.value = false
  }
}

async function openTradeRecords(row) {
  selectedPosition.value = row
  showRecordsModal.value = true
  try {
    tradeRecords.value = await GetTradingRecordsByStock(row.stockCode) || []
  } catch (err) {
    message.error('加载交易流水失败: ' + (err?.message || String(err)))
  }
}

function openTradeModal(row = null, direction = '买入') {
  const code = row?.stockCode || (form.value.stockCode || '').trim()
  const name = row?.stockName || row?.stockCode || code
  if (!code) {
    message.warning('请选择或输入股票代码')
    return
  }
  selectedPosition.value = row || (selectedPosition.value?.stockCode === code ? selectedPosition.value : null)
  tradeForm.value = {
    StockCode: code,
    StockName: name,
    Direction: direction,
    Price: Number(row?.currentPrice || 0),
    Volume: 0,
    TradingTime: Date.now(),
    Fee: 0,
    Reason: '',
  }
  showTradeModal.value = true
}

function openNewTradeModal() {
  selectedPosition.value = null
  tradeForm.value = {
    StockCode: '',
    StockName: '',
    Direction: '买入',
    Price: 0,
    Volume: 0,
    TradingTime: Date.now(),
    Fee: 0,
    Reason: '',
  }
  showTradeModal.value = true
}

async function addTradeRecord() {
  const price = Number(tradeForm.value.Price || 0)
  const volume = Number(tradeForm.value.Volume || 0)
  if (!tradeForm.value.StockCode) {
    message.warning('股票代码不能为空')
    return
  }
  if (price <= 0 || volume <= 0) {
    message.warning('价格和数量必须大于 0')
    return
  }
  if (
      tradeForm.value.Direction === '卖出'
      && selectedPosition.value?.currentVolume
      && volume > Number(selectedPosition.value.currentVolume)
  ) {
    message.warning('卖出数量不能超过当前汇总持仓')
    return
  }

  try {
    const payload = {
      ...tradeForm.value,
      Price: price,
      Volume: volume,
      TradingTime: new Date(tradeForm.value.TradingTime),
      Amount: price * volume,
    }
    await AddTradingRecord(payload)
    message.success('交易流水已添加')
    showTradeModal.value = false
    await loadPositionRows()
    if (selectedPosition.value?.stockCode) {
      tradeRecords.value = await GetTradingRecordsByStock(selectedPosition.value.stockCode) || []
    }
  } catch (err) {
    message.error('添加交易流水失败: ' + (err?.message || String(err)))
  }
}

async function syncFeatures() {
  syncLoading.value = true
  try {
    const scope = resolveScope()
    if (!scope) return
    const res = await StartFeatureSync(scope, requiredFeatureBars())
    if (res.code !== 1) {
      message.error(res.msg || '同步特征失败')
      return
    }
    syncJob.value = res.data
    message.info(res.msg || '特征同步任务已启动')
    pollSyncJob(syncJob.value.id)
  } catch (err) {
    message.error('同步特征失败: ' + (err?.message || String(err)))
  } finally {
    syncLoading.value = false
  }
}

function requiredFeatureBars() {
  const start = Number(form.value.startDate || 0)
  const end = Number(form.value.endDate || Date.now())
  const calendarDays = Math.max(0, Math.ceil((end - start) / 86400000))
  return Math.min(3000, Math.max(365, Math.ceil(calendarDays * 0.75) + 90))
}

function pollSyncJob(jobId) {
  if (syncTimer) clearInterval(syncTimer)
  syncTimer = setInterval(async () => {
    try {
      const res = await GetFeatureSyncJob(jobId)
      if (res.code === 1) {
        syncJob.value = res.data
        if (res.data.status !== 'running') {
          clearInterval(syncTimer)
          syncTimer = null
          await loadFeatureStatus()
        }
      }
    } catch (_) {
      clearInterval(syncTimer)
      syncTimer = null
    }
  }, 2000)
}

async function loadFeatureStatus() {
  const scope = resolveScope(false)
  if (!scope || !form.value.startDate || !form.value.endDate) return
  try {
    const coverageRes = await GetFeatureCoverage(scope, formatDate(form.value.startDate), formatDate(form.value.endDate))
    featureCoverage.value = coverageRes?.data || null
    const freshnessRes = await GetFeatureFreshness(scope)
    featureFreshness.value = freshnessRes?.data || null
  } catch (err) {
    console.warn('加载特征状态失败', err)
  }
}

async function loadCronStatus() {
  try {
    cronStatus.value = await GetPredictionCronStatus()
  } catch (err) {
    console.warn('加载预测工厂系统任务失败', err)
  }
}

async function loadPredictionAlerts() {
  alertLoading.value = true
  try {
    alertRows.value = await GetPredictionAlertLogs(100, 'all') || []
  } catch (err) {
    console.warn('加载 AI 提醒事件失败', err)
  } finally {
    alertLoading.value = false
  }
}

async function scanPredictionAlerts() {
  alertLoading.value = true
  try {
    const res = await ScanPredictionAlertsNow(true)
    if (res.code === 1) {
      const data = res.data || {}
      message.success(`扫描完成：触发 ${data.generated || 0}，新增 ${data.alerts?.length || 0}`)
      await loadPredictionAlerts()
      await loadCronStatus()
    } else {
      message.error(res.msg || '扫描提醒失败')
    }
  } catch (err) {
    message.error('扫描提醒失败: ' + (err?.message || String(err)))
  } finally {
    alertLoading.value = false
  }
}

async function markAlert(row, status) {
  try {
    const id = Number(row?.id || row?.ID || 0)
    if (!id) return
    const res = await MarkPredictionAlertStatus(id, status)
    if (String(res).includes('失败')) {
      message.error(res)
    } else {
      message.success(res)
      await loadPredictionAlerts()
    }
  } catch (err) {
    message.error('更新提醒状态失败: ' + (err?.message || String(err)))
  }
}

async function runCronTask(taskType) {
  const res = await RunPredictionCronTaskNow(taskType)
  if (res.code === 1) {
    message.success(res.msg || '执行成功')
  } else {
    message.error(res.msg || '执行失败')
  }
  await loadCronStatus()
  if (taskType === 'prediction_scan_alerts') {
    await loadPredictionAlerts()
  }
}

async function generatePredictions() {
  loading.value = true
  try {
    if (!form.value.startDate || !form.value.endDate) {
      message.warning('请选择回测区间')
      return
    }
    if (form.value.startDate > form.value.endDate) {
      message.warning('回测开始日期不能晚于结束日期')
      return
    }

    const scope = resolveScope()
    if (!scope) return
    await loadFeatureStatus()
    if (featureCoverage.value && !featureCoverage.value.ready) {
      message.warning(featureCoverage.value.message || '核心特征覆盖率不足，请先同步特征')
      return
    }
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
      await loadMyHypotheses()
      await loadPredictionSessions()
      await loadPredictionAlerts()
      message.success('AI 预测生成成功')
    } else {
      message.error(res.msg || '生成 AI 预测失败')
    }
  } catch (err) {
    message.error('生成 AI 预测失败: ' + (err?.message || String(err)))
  } finally {
    loading.value = false
  }
}

async function recalculateCurrentSession() {
  const sessionId = Number(sessionResult.value?.session?.id || 0)
  if (!sessionId) return
  recalculateLoading.value = true
  try {
    const res = await RecalculatePredictionSession(sessionId)
    if (res.code !== 1) {
      message.error(res.msg || '重新回测失败')
      return
    }
    sessionResult.value = res.data
    await Promise.all([loadMyHypotheses(), loadPredictionAlerts()])
    message.success('重新回测完成')
  } catch (err) {
    message.error('重新回测失败: ' + (err?.message || String(err)))
  } finally {
    recalculateLoading.value = false
  }
}

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

function formatRatio(val) {
  if (val === undefined || val === null) return '-'
  return (val * 100).toFixed(1) + '%'
}

function syncProgress() {
  if (!syncJob.value || !syncJob.value.total) return 0
  return Math.round((syncJob.value.finished + syncJob.value.failed) * 100 / syncJob.value.total)
}

function itemId(item) {
  return Number(item?.id ?? item?.ID ?? 0)
}

function itemTime(item) {
  const raw = item?.createdAt
      || item?.CreatedAt
      || item?.updatedAt
      || item?.UpdatedAt
      || item?.triggeredAt
      || item?.TriggeredAt
      || item?.dataAsOf
      || item?.DataAsOf
      || item?.validatedAt
      || item?.ValidatedAt
  if (!raw || String(raw).startsWith('0001-')) return 0
  const ts = new Date(raw).getTime()
  return Number.isNaN(ts) ? 0 : ts
}

function sortLatestFirst(items) {
  return [...(items || [])].sort((a, b) => {
    const timeDiff = itemTime(b) - itemTime(a)
    if (timeDiff !== 0) return timeDiff
    return itemId(b) - itemId(a)
  })
}

function paginate(items, page) {
  const start = (Math.max(1, page) - 1) * listPageSize
  return (items || []).slice(start, start + listPageSize)
}

function normalizePage(page, length) {
  const maxPage = Math.max(1, Math.ceil((length || 0) / listPageSize))
  return Math.min(Math.max(1, page), maxPage)
}

async function saveHypothesis(id) {
  try {
    const res = await SavePredictionHypothesis(id)
    if (String(res).includes('失败')) {
      message.error(res)
    } else {
      message.success(res)
    }
    await loadMyHypotheses()
  } catch (err) {
    message.error('开始前向验证失败: ' + (err?.message || String(err)))
  }
}

async function activateHypothesis(h) {
  try {
    const res = await ActivatePredictionHypothesis(h.id)
    if (String(res).includes('失败')) message.error(res)
    else message.success(res)
    await loadMyHypotheses()
  } catch (err) {
    message.error('启用正式监控失败: ' + (err?.message || String(err)))
  }
}

async function openPaperDetails(h) {
  selectedPaperHypothesis.value = h
  showPaperModal.value = true
  paperDetailsLoading.value = true
  try {
    const res = await GetPredictionPaperTradingDetails(h.id)
    if (res?.code !== 1) throw new Error(res?.msg || '加载失败')
    paperDetails.value = res.data
    await nextTick()
    renderPaperChart()
  } catch (err) {
    paperDetails.value = null
    message.error('加载前向验证数据失败: ' + (err?.message || String(err)))
  } finally {
    paperDetailsLoading.value = false
  }
}

function renderPaperChart() {
  if (paperChartInstance) {
    paperChartInstance.dispose()
    paperChartInstance = null
  }
  const rows = paperDetails.value?.dailies || []
  if (!paperChartRef.value || rows.length === 0) return
  paperChartInstance = echarts.init(paperChartRef.value)
  paperChartInstance.setOption({
    animationDuration: 280,
    grid: {left: 48, right: 48, top: 24, bottom: 34},
    tooltip: {trigger: 'axis'},
    legend: {data: ['净值', '回撤']},
    xAxis: {type: 'category', data: rows.map(row => row.date), boundaryGap: false},
    yAxis: [
      {type: 'value', name: '净值', scale: true},
      {type: 'value', name: '回撤', axisLabel: {formatter: value => `${(Number(value) * 100).toFixed(0)}%`}}
    ],
    series: [
      {name: '净值', type: 'line', showSymbol: false, data: rows.map(row => Number(row.nav || 0)), lineStyle: {width: 2}},
      {name: '回撤', type: 'line', yAxisIndex: 1, showSymbol: false, data: rows.map(row => Number(row.drawdown || 0)), areaStyle: {opacity: 0.08}}
    ]
  })
}

async function saveObservation(id) {
  try {
    const res = await SavePredictionObservation(id)
    if (String(res).includes('失败')) {
      message.error(res)
    } else {
      message.success(res)
    }
    await loadMyHypotheses()
  } catch (err) {
    message.error('保存观察失败: ' + (err?.message || String(err)))
  }
}

async function openLatestSession() {
  if (sessionResult.value) return
  const latest = sortLatestFirst(recentSessions.value)[0]
  const latestId = itemId(latest)
  if (latestId > 0) {
    await openSession(latestId, false)
  }
}

async function openSession(sessionId, showMessage = true) {
  try {
    const res = await GetPredictionSession(sessionId)
    if (res.code === 1) {
      sessionResult.value = res.data
      sessionViewMode.value = 'history'
      activeWorkspace.value = 'analysis'
      activeAnalysisView.value = 'decisions'
      if (showMessage) message.success('已打开历史预测')
    } else {
      if (showMessage) message.error(res.msg || '打开历史预测失败')
    }
  } catch (err) {
    if (showMessage) message.error('打开历史预测失败: ' + (err?.message || String(err)))
  }
}

async function showChart(h) {
  if (!h?.id) return
  try {
    chartData.value = await GetPredictionHypothesisDailyNAV(h.id) || []
    await nextTick()
    renderChart()
  } catch (err) {
    chartData.value = []
    message.error('加载净值曲线失败: ' + (err?.message || String(err)))
  }
}

function renderChart() {
  if (chartInstance) {
    chartInstance.dispose()
    chartInstance = null
  }
  if (!chartRef.value || chartData.value.length === 0) return
  chartInstance = echarts.init(chartRef.value)
  const dates = chartData.value.map(d => d.date)
  const navs = chartData.value.map(d => d.nav)
  const option = {
    animationDuration: 280,
    grid: {left: 48, right: 20, top: 20, bottom: 34},
    tooltip: {trigger: 'axis', valueFormatter: value => Number(value || 0).toFixed(4)},
    xAxis: {
      type: 'category', data: dates, boundaryGap: false,
      axisLine: {lineStyle: {color: '#d9dde3'}},
      axisTick: {show: false},
      axisLabel: {color: '#737b88', hideOverlap: true}
    },
    yAxis: {
      type: 'value', scale: true,
      axisLabel: {color: '#737b88'},
      splitLine: {lineStyle: {color: '#edf0f3'}}
    },
    series: [{
      type: 'line',
      data: navs,
      smooth: true,
      showSymbol: false,
      lineStyle: {color: '#2563eb', width: 2},
      areaStyle: {color: 'rgba(37, 99, 235, 0.08)'}
    }]
  }
  chartInstance.setOption(option)
}

function formatPercent(val) {
  if (val === undefined || val === null) return '-'
  return (val * 100).toFixed(2) + '%'
}

function formatPrice(val) {
  if (!val) return '-'
  return Number(val).toFixed(3)
}

function formatMoney(val) {
  if (val === undefined || val === null) return '-'
  return Number(val).toFixed(2)
}

function recordField(row, pascalName, camelName) {
  return row?.[pascalName] ?? row?.TradingRecord?.[pascalName] ?? row?.tradingRecord?.[pascalName] ?? row?.[camelName]
}

function recordAmount(row) {
  const amount = Number(recordField(row, 'Amount', 'amount') || 0)
  if (amount > 0) return amount
  return Number(recordField(row, 'Price', 'price') || 0) * Number(recordField(row, 'Volume', 'volume') || 0)
}

function formatScore(val) {
  if (val === undefined || val === null) return '-'
  return Number(val).toFixed(0)
}

function confidenceText(val) {
  if (val === 'high') return '高'
  if (val === 'medium') return '中'
  return '低'
}

function riskType(val) {
  if (val === 'high') return 'error'
  if (val === 'medium') return 'warning'
  if (val === 'low') return 'success'
  return 'default'
}

function riskText(val) {
  if (val === 'high') return '高风险'
  if (val === 'medium') return '中风险'
  if (val === 'low') return '低风险'
  return '未知风险'
}

function qualityType(val) {
  if (val === 'reference') return 'success'
  if (val === 'observe') return 'warning'
  if (val === 'blocked') return 'error'
  return 'default'
}

function qualityText(val) {
  if (val === 'reference') return '可参考'
  if (val === 'observe') return '仅观察'
  if (val === 'blocked') return '禁止正式'
  if (val === 'weak') return '偏弱'
  return '未知'
}

function actionType(action) {
  if (['BUY', 'ADD'].includes(action)) return 'error'
  if (action === 'REDUCE') return 'warning'
  if (['SELL', 'AVOID'].includes(action)) return 'success'
  return 'default'
}

function statusText(status) {
  if (status === 'active') return '正式监控'
  if (status === 'paper_trade') return '前向验证'
  if (status === 'active_candidate') return '待验证'
  if (status === 'watch') return '观察'
  if (status === 'disabled') return '已禁用'
  return '草稿'
}

function statusType(status) {
  if (status === 'active') return 'success'
  if (status === 'paper_trade') return 'info'
  if (status === 'watch') return 'warning'
  return 'default'
}

function alertLevelType(level) {
  if (level === 'high') return 'error'
  if (level === 'medium') return 'warning'
  if (level === 'low') return 'success'
  return 'default'
}

function alertTypeText(type) {
  const map = {
    stop_loss: '止损',
    defense_price: '防守位',
    take_profit: '止盈/减仓',
    reduce: '减仓',
    sell: '卖出',
    capital_outflow: '资金流风险'
  }
  return map[type] || type || '提醒'
}

function alertStatusText(status) {
  const map = {
    new: '未读',
    sent: '已通知',
    read: '已读',
    ignored: '已忽略',
    resolved: '已解除'
  }
  return map[status] || status || '未知'
}

function alertThresholdText(row) {
  const threshold = Number(row?.thresholdPrice || 0)
  if (threshold > 0) return `阈值 ${formatPrice(threshold)}`
  return '资金流条件触发'
}

function alertActionText(action) {
  const map = {BUY: '买入', ADD: '加仓', HOLD: '持有', WATCH: '观察', REDUCE: '减仓', SELL: '卖出'}
  return map[String(action || '').toUpperCase()] || action || '-'
}

function alertStatusType(status) {
  if (status === 'new') return 'warning'
  if (status === 'sent') return 'info'
  if (status === 'read') return 'default'
  if (status === 'ignored') return 'default'
  if (status === 'resolved') return 'success'
  return 'default'
}

function parseJsonList(raw) {
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed : []
  } catch (_) {
    return []
  }
}

function parseJsonObject(raw) {
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : null
  } catch (_) {
    return null
  }
}

function capitalFlowText(raw) {
  const flow = parseJsonObject(raw)
  if (!flow) return ''
  const levelMap = {
    strong_inflow: '主力代理强流入',
    inflow: '主力代理流入',
    neutral: '资金流中性',
    outflow: '主力代理流出',
    strong_outflow: '主力代理强流出',
    unknown: '资金流不足'
  }
  return levelMap[flow.level] || flow.level || ''
}

function sampleSummaryText(decision) {
  const rows = parseJsonList(decision?.sampleSummaryJson || decision?.sampleSummaryJSON)
  return rows
    .filter(row => row.entryMatched || row.exitMatched)
    .map(row => `${row.strategyName || '策略'}：股票池${row.poolSamples || 0}笔/单股${row.stockSamples || 0}笔`)
    .join('；')
}

function dataStatusText(decision) {
  const status = parseJsonObject(decision?.dataStatusJson || decision?.dataStatusJSON)
  if (!status) return ''
  const flowRows = Number(status.moneyFlowRows || 0)
  return `特征 ${status.featureStartDate || '-'} 至 ${status.featureEndDate || '-'}；资金流 ${flowRows} 天`
}

function backtestMetrics(h) {
  const payload = parseJsonObject(h?.backtestConfigJson || h?.backtestConfigJSON)
  return payload?.metrics || {}
}

async function ensureBacktestTrades(hypothesis) {
  const id = Number(hypothesis?.id || 0)
  if (!id) return
  if (backtestTrades.value[id] || backtestTradeLoading.value[id]) return
  backtestTradeLoading.value = {...backtestTradeLoading.value, [id]: true}
  try {
    const rows = await GetPredictionBacktestTrades(id)
    backtestTrades.value = {...backtestTrades.value, [id]: rows || []}
  } catch (err) {
    message.error('加载交易明细失败: ' + (err?.message || String(err)))
  } finally {
    backtestTradeLoading.value = {...backtestTradeLoading.value, [id]: false}
  }
}

async function selectBacktest(hypothesis) {
  const id = itemId(hypothesis)
  if (!id) return
  selectedBacktestId.value = id
  await Promise.all([ensureBacktestTrades(hypothesis), showChart(hypothesis)])
}

function openDecisionDetail(decision) {
  selectedDecision.value = decision
  decisionDrawerVisible.value = true
}

async function openMonitoredHypothesis(hypothesis) {
  const sessionId = Number(hypothesis?.sessionId || hypothesis?.SessionID || 0)
  if (!sessionId) return
  await openSession(sessionId)
  activeAnalysisView.value = 'backtests'
  await nextTick()
  const target = sortedBacktestHypotheses.value.find(row => itemId(row) === itemId(hypothesis))
  if (target) await selectBacktest(target)
}

function matchedStrategyRows(decision) {
  return parseJsonList(decision?.matchedStrategiesJson || decision?.matchedStrategiesJSON)
      .filter(row => row.entryMatched || row.exitMatched)
}

function returnTone(value) {
  const number = Number(value || 0)
  if (number > 0) return 'positive'
  if (number < 0) return 'negative'
  return 'neutral'
}

function exitReasonText(reason) {
  const map = {
    stop_loss: '止损',
    stop_gain: '止盈',
    take_profit: '止盈',
    exit_condition: '退出条件',
    max_hold_days: '持有到期',
    end_of_period: '回测结束'
  }
  return map[reason] || reason || '-'
}

function stockBacktestRows(hypothesisId) {
  const groups = new Map()
  for (const trade of backtestTrades.value[hypothesisId] || []) {
    const code = trade.stockCode || '-'
    if (!groups.has(code)) {
      groups.set(code, {stockCode: code, stockName: trade.stockName || code, trades: [], pnl: 0})
    }
    const group = groups.get(code)
    group.trades.push(trade)
    const grossBuy = Number(trade.grossBuyAmount || 0)
    const grossSell = Number(trade.grossSellAmount || 0)
    group.pnl += grossSell - grossBuy - Number(trade.fee || 0) - Number(trade.slippage || 0)
  }
  return Array.from(groups.values()).map(group => {
    const returns = group.trades.map(row => Number(row.returnRate || 0)).sort((a, b) => a - b)
    const wins = returns.filter(value => value > 0).length
    const middle = Math.floor(returns.length / 2)
    const median = returns.length % 2 ? returns[middle] : ((returns[middle - 1] || 0) + (returns[middle] || 0)) / 2
    return {
      ...group,
      tradeCount: returns.length,
      winRate: returns.length ? wins / returns.length : 0,
      avgReturn: average(returns),
      medianReturn: median,
      avgMae: average(group.trades.map(row => Number(row.maxDrawdown || 0))),
      avgMfe: average(group.trades.map(row => Number(row.maxReturn || 0))),
      avgHoldDays: average(group.trades.map(row => Number(row.holdDays || 0))),
    }
  }).sort((a, b) => b.pnl - a.pnl)
}

function average(values) {
  if (!values.length) return 0
  return values.reduce((sum, value) => sum + Number(value || 0), 0) / values.length
}

function stockDetailKey(hypothesisId, stockCode) {
  return `${hypothesisId}:${stockCode}`
}

function toggleStockTrades(hypothesisId, stockCode) {
  const key = stockDetailKey(hypothesisId, stockCode)
  expandedStocks.value = {...expandedStocks.value, [key]: !expandedStocks.value[key]}
}

function formatMetricPercent(val) {
  if (val === undefined || val === null || val === '') return '-'
  return (Number(val) * 100).toFixed(2) + '%'
}

function todayAlertCountForHypothesis(h) {
  const sessionId = Number(h?.sessionId || h?.SessionID || 0)
  if (!sessionId) return 0
  const today = formatDate(Date.now())
  return (alertRows.value || []).filter(row => {
    const rowSession = Number(row.sessionId || row.SessionID || 0)
    const ts = row.triggeredAt || row.TriggeredAt || ''
    return rowSession === sessionId && String(ts).slice(0, 10) === today
  }).length
}

function monitorReady(h) {
  return h.strategyVersion === 'strategy_v4' && h.featureVersion === 'daily_v2_qfq' && h.noLookaheadPassed &&
      h.tradeCount >= 30 && Number(h.outSampleTradeCount || 0) >= 15 && h.maxDrawdown <= 0.2 &&
      h.avgReturn > 0 && h.outSampleAvgReturn > 0 && Number(h.dataCoverage || 0) >= 0.95 && h.benchmarkAvailable && Number(h.excessReturn || 0) > 0
}

function formatDateTime(val) {
  if (!val || String(val).startsWith('0001-')) return '-'
  const d = new Date(val)
  if (Number.isNaN(d.getTime())) return '-'
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  const hour = String(d.getHours()).padStart(2, '0')
  const minute = String(d.getMinutes()).padStart(2, '0')
  return `${year}-${month}-${day} ${hour}:${minute}`
}

function formatDate(ts) {
  const d = new Date(ts)
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}
</script>

<template>
  <div class="prediction-factory" style="--wails-draggable:no-drag;">
    <header class="factory-header">
      <div class="factory-title-block">
        <div class="factory-title">AI 预测工厂</div>
        <n-tag v-if="featureFreshness?.featureVersion" size="small" :bordered="false" type="info">
          {{ featureFreshness.featureVersion }}
        </n-tag>
      </div>
    </header>

    <n-tabs v-model:value="activeWorkspace" type="line" animated class="workspace-tabs">
      <n-tab-pane name="opportunities">
        <template #tab>
          <span class="tab-label"><n-icon :component="TrendingUpOutline"/>机会发现</span>
        </template>
        <section class="workspace-page opportunity-page">
          <n-alert type="info" :show-icon="false">
            五类页面数据先形成带来源、版本和可用时间的候选快照；这里的 WATCH 不是买入指令，进入量化验证后才会执行回测、裁决和前向模拟。
          </n-alert>
          <n-card size="small" title="五源候选池" class="candidate-command-card">
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

          <div class="candidate-layout">
            <n-card size="small" title="历史快照" class="candidate-history-card">
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
                <n-empty v-else description="选择历史快照或生成新的五源候选池"/>
              </n-spin>
            </n-card>
          </div>
        </section>
      </n-tab-pane>
      <n-tab-pane name="analysis">
        <template #tab>
          <span class="tab-label"><n-icon :component="AnalyticsOutline"/>分析工作台</span>
        </template>
        <section class="workspace-page analysis-page">
        <section class="command-surface">
          <n-form :model="form" label-placement="top" :show-feedback="false" class="command-form">
            <n-form-item label="投资场景" class="command-field">
              <n-select v-model:value="form.scene" :options="scenes"/>
            </n-form-item>
            <n-form-item label="股票池" class="command-field">
              <n-select v-model:value="form.stockScope" :options="scopes"/>
            </n-form-item>
            <n-form-item label="股票代码" v-if="form.stockScope === 'stock'" class="command-field stock-code-field">
              <n-input v-model:value="form.stockCode" placeholder="例如：000001"/>
            </n-form-item>
            <n-form-item label="AI 配置" class="command-field ai-field">
              <n-select
                  v-model:value="form.aiConfigId"
                  :options="aiConfigOptions"
                  :loading="aiConfigLoading"
                  placeholder="选择 AI 模型配置"
              />
            </n-form-item>
            <n-form-item label="回测区间" class="command-field date-field">
              <n-space :wrap="false" align="center">
                <n-date-picker v-model:value="form.startDate" type="date"/>
                <span class="date-separator">至</span>
                <n-date-picker v-model:value="form.endDate" type="date"/>
              </n-space>
            </n-form-item>
            <n-form-item class="command-actions">
              <n-space :wrap="false">
                <n-button @click="syncFeatures" :loading="syncLoading">
                  <template #icon><n-icon :component="SyncOutline"/></template>
                  同步数据
                </n-button>
                <n-button type="primary" @click="generatePredictions" :loading="loading">
                  <template #icon><n-icon :component="TrendingUpOutline"/></template>
                  生成预测
                </n-button>
              </n-space>
            </n-form-item>
          </n-form>
          <section class="data-status-strip">
            <div class="data-status-main">
              <n-space align="center" size="small">
                <n-tag :type="featureCoverage?.ready ? 'success' : 'warning'">
                  {{ featureCoverage?.ready ? '特征达标' : '需同步特征' }}
                </n-tag>
                <n-text depth="3">数据截至 {{ featureFreshness?.latestDate || '-' }}</n-text>
              </n-space>
              <div class="coverage-metrics" v-if="featureCoverage">
                <span>股票 <b>{{ formatRatio(featureCoverage.stockCoverage) }}</b></span>
                <span>交易日 <b>{{ formatRatio(featureCoverage.tradeDayCoverage) }}</b></span>
                <span>字段 <b>{{ formatRatio(featureCoverage.coreFieldCoverage) }}</b></span>
              </div>
              <div class="sync-inline" v-if="syncJob">
                <span>{{ syncJob.finished || 0 }}/{{ syncJob.total || 0 }}</span>
                <n-progress type="line" :show-indicator="false" :percentage="syncProgress()" />
                <span>{{ syncProgress() }}%</span>
              </div>
            </div>
            <n-alert v-if="syncJob?.errorMessage" type="error" :show-icon="false" class="sync-error">
              {{ syncJob.errorMessage }}
            </n-alert>
          </section>

        </section>

        <section class="results-surface" v-if="sessionResult">
          <div class="result-heading">
            <div>
              <n-space align="center" size="small">
                <h2>分析结果</h2>
                <n-tag size="small" :bordered="false" :type="sessionViewMode === 'current' ? 'success' : 'info'">
                  {{ sessionViewMode === 'current' ? '当前结果' : '历史回放' }}
                </n-tag>
              </n-space>
              <n-text depth="3">
                {{ sessionResult.session?.scene || '-' }} · {{ sessionResult.session?.stockScope || '-' }} ·
                {{ sessionResult.session?.startDate || '-' }} 至 {{ sessionResult.session?.endDate || '-' }}
              </n-text>
            </div>
            <n-tooltip trigger="hover">
              <template #trigger>
                <n-button quaternary circle :loading="recalculateLoading" @click="recalculateCurrentSession">
                  <template #icon><n-icon :component="RefreshOutline"/></template>
                </n-button>
              </template>
              重新回测
            </n-tooltip>
          </div>

          <div class="decision-summary">
            <div class="summary-cell"><span>买入/加仓</span><b class="positive">{{ decisionSummary.buy }}</b></div>
            <div class="summary-cell"><span>持有/观察</span><b>{{ decisionSummary.hold }}</b></div>
            <div class="summary-cell"><span>减仓/回避</span><b class="negative">{{ decisionSummary.reduce }}</b></div>
            <div class="summary-cell"><span>高风险</span><b class="negative">{{ decisionSummary.highRisk }}</b></div>
            <div class="summary-cell"><span>回测策略</span><b>{{ decisionSummary.strategies }}</b></div>
          </div>

          <n-spin :show="loading">
            <n-empty v-if="!sessionResult?.hypotheses?.length && !sessionResult?.decisions?.length" description="暂无预测结果"/>
            <n-tabs v-model:value="activeAnalysisView" type="line" animated class="analysis-tabs" v-else>
              <n-tab-pane name="decisions">
                <template #tab>
                  <span class="tab-label"><n-icon :component="ListOutline"/>操作建议 {{ currentDecisions.length }}</span>
                </template>
                <div class="table-shell" v-if="currentDecisions.length">
                  <n-table size="small" :bordered="false" :single-line="false" class="decision-table">
                  <thead>
                  <tr>
                    <th>股票</th>
                    <th>动作与仓位</th>
                    <th>概率与预期</th>
                    <th>价格与持仓</th>
                    <th>风险边界</th>
                    <th>质量</th>
                    <th aria-label="详情"></th>
                  </tr>
                  </thead>
                  <tbody>
                  <tr v-for="d in currentDecisions" :key="d.id || d.stockCode" class="clickable-row" @click="openDecisionDetail(d)">
                    <td>
                      <n-space vertical size="small">
                        <n-text strong>{{ d.stockName || d.stockCode }}</n-text>
                        <n-text depth="3">{{ d.stockCode }}</n-text>
                      </n-space>
                    </td>
                    <td>
                      <n-space vertical size="small">
                        <n-space align="center" size="small">
                          <n-tag size="small" :type="actionType(d.action)">{{ d.actionText || d.action }}</n-tag>
                          <n-text strong>{{ d.quantityPercent ? `${Number(d.quantityPercent).toFixed(0)}%` : '-' }}</n-text>
                        </n-space>
                        <n-text depth="3">{{ d.positionAdvice }}</n-text>
                      </n-space>
                    </td>
                    <td>
                      <n-space vertical size="small">
                        <n-text strong>{{ formatPercent(d.probability) }}</n-text>
                        <n-text :class="returnTone(d.expectedReturn)">预期 {{ formatPercent(d.expectedReturn) }}</n-text>
                      </n-space>
                    </td>
                    <td>
                      <n-space vertical size="small">
                        <n-text strong>{{ formatPrice(d.currentPrice) }}</n-text>
                        <n-text depth="3" v-if="d.costPrice">成本 {{ formatPrice(d.costPrice) }} · {{ formatPercent(d.profitRate) }}</n-text>
                        <n-text depth="3" v-else>{{ d.suggestedQuantity ? `${d.suggestedQuantity} 股/份` : '未持仓' }}</n-text>
                      </n-space>
                    </td>
                    <td>
                      <n-space vertical size="small">
                        <n-text>止损 {{ formatPrice(d.stopLossPrice) }}</n-text>
                        <n-text depth="3">止盈 {{ formatPrice(d.takeProfitPrice) }}</n-text>
                      </n-space>
                    </td>
                    <td>
                      <n-space vertical size="small">
                        <n-space size="small">
                          <n-tag size="small" :type="riskType(d.riskLevel)">{{ riskText(d.riskLevel) }}</n-tag>
                          <n-tag size="small" :type="qualityType(d.qualityRating)">{{ qualityText(d.qualityRating) }}</n-tag>
                        </n-space>
                        <n-text depth="3">评分 {{ formatScore(d.score) }} · 置信度 {{ confidenceText(d.confidence) }}</n-text>
                      </n-space>
                    </td>
                    <td><n-icon :component="ChevronForwardOutline" size="16"/></td>
                  </tr>
                  </tbody>
                  </n-table>
                </div>
                <n-empty v-else description="暂无操作建议"/>
              </n-tab-pane>

              <n-tab-pane name="backtests">
                <template #tab>
                  <span class="tab-label"><n-icon :component="PulseOutline"/>策略回测 {{ sortedBacktestHypotheses.length }}</span>
                </template>
                <div class="strategy-workbench" v-if="selectedBacktestHypothesis">
                  <section class="strategy-rank-pane">
                    <div class="pane-title strategy-rank-title">
                      <span>策略排名</span>
                      <div class="rank-title-meta">
                        <n-text depth="3">{{ sortedBacktestHypotheses.length }} 个候选</n-text>
                        <n-pagination
                            v-if="sortedBacktestHypotheses.length > listPageSize"
                            v-model:page="backtestPage"
                            :page-size="listPageSize"
                            :item-count="sortedBacktestHypotheses.length"
                            size="small"
                        />
                      </div>
                    </div>
                    <div class="strategy-list">
                      <button
                          v-for="(h, index) in pagedBacktestHypotheses"
                          :key="h.id"
                          type="button"
                          class="strategy-list-item"
                          :class="{active: itemId(h) === itemId(selectedBacktestHypothesis)}"
                          @click="selectBacktest(h)"
                      >
                        <span class="strategy-rank-index">#{{ (backtestPage - 1) * listPageSize + index + 1 }}</span>
                        <span class="strategy-item-body">
                          <span class="strategy-item-head">
                            <strong>{{ h.name }}</strong>
                            <n-tag size="small" :bordered="false" :type="qualityType(backtestMetrics(h).qualityRating)">
                              {{ qualityText(backtestMetrics(h).qualityRating) }}
                            </n-tag>
                          </span>
                          <span class="strategy-item-metrics">
                            <span>年化 <b :class="returnTone(backtestMetrics(h).annualizedReturn)">{{ formatMetricPercent(backtestMetrics(h).annualizedReturn) }}</b></span>
                            <span>回撤 <b>{{ formatPercent(h.maxDrawdown) }}</b></span>
                            <span>Sharpe <b>{{ Number(backtestMetrics(h).sharpeRatio || 0).toFixed(2) }}</b></span>
                            <span>样本外 <b :class="returnTone(h.outSampleAvgReturn)">{{ formatPercent(h.outSampleAvgReturn) }}</b></span>
                          </span>
                        </span>
                      </button>
                    </div>
                  </section>

                  <section class="strategy-detail-pane">
                    <div class="strategy-detail-header">
                      <div class="strategy-title-copy">
                        <n-space align="center" size="small" class="strategy-title-line">
                          <h3>{{ selectedBacktestHypothesis.name }}</h3>
                          <n-tag size="small" :type="monitorReady(selectedBacktestHypothesis) ? 'success' : 'warning'">
                            {{ monitorReady(selectedBacktestHypothesis) ? '可开始前向验证' : '仅建议观察' }}
                          </n-tag>
                        </n-space>
                        <n-text depth="3" class="strategy-description">{{ selectedBacktestHypothesis.description }}</n-text>
                      </div>
                      <n-space size="small" class="strategy-actions">
                        <n-button size="small" @click="saveObservation(selectedBacktestHypothesis.id)">加入观察池</n-button>
                        <n-button size="small" type="primary" @click="saveHypothesis(selectedBacktestHypothesis.id)">开始前向验证</n-button>
                      </n-space>
                    </div>

                    <div class="metric-strip strategy-metrics">
                      <div><span>胜率</span><b>{{ formatPercent(selectedBacktestHypothesis.winRate) }}</b></div>
                      <div><span>平均收益</span><b :class="returnTone(selectedBacktestHypothesis.avgReturn)">{{ formatPercent(selectedBacktestHypothesis.avgReturn) }}</b></div>
                      <div><span>最大回撤</span><b>{{ formatPercent(selectedBacktestHypothesis.maxDrawdown) }}</b></div>
                      <div><span>Sharpe</span><b>{{ Number(backtestMetrics(selectedBacktestHypothesis).sharpeRatio || 0).toFixed(2) }}</b></div>
                      <div><span>超额收益</span><b :class="returnTone(backtestMetrics(selectedBacktestHypothesis).excessReturn)">{{ formatMetricPercent(backtestMetrics(selectedBacktestHypothesis).excessReturn) }}</b></div>
                      <div><span>样本外</span><b :class="returnTone(selectedBacktestHypothesis.outSampleAvgReturn)">{{ formatPercent(selectedBacktestHypothesis.outSampleAvgReturn) }}</b></div>
                    </div>

                    <div class="chart-panel">
                      <div class="panel-heading">
                        <span>净值曲线</span>
                        <n-text depth="3">{{ selectedBacktestHypothesis.tradeCount || 0 }} 个回测样本 · 平均持有 {{ Number(backtestMetrics(selectedBacktestHypothesis).averageHoldingDays || 0).toFixed(1) }} 个交易日</n-text>
                      </div>
                      <div ref="chartRef" class="strategy-chart" v-if="chartData.length"></div>
                      <n-empty v-else size="small" description="暂无净值数据"/>
                    </div>

                    <n-tabs v-model:value="strategyDetailView" type="line" class="strategy-detail-tabs">
                      <n-tab-pane name="attribution" tab="股票归因">
                        <n-spin :show="backtestTradeLoading[selectedBacktestHypothesis.id]">
                          <div class="table-shell strategy-table-shell" v-if="selectedBacktestStocks.length">
                            <n-table size="small" :bordered="false" :single-line="false" class="strategy-data-table attribution-table">
                              <thead><tr><th>股票</th><th>样本</th><th>胜率</th><th>平均/中位</th><th>MFE/MAE</th><th>收益贡献</th><th>均持</th><th></th></tr></thead>
                              <tbody>
                              <template v-for="stock in selectedBacktestStocks" :key="stock.stockCode">
                                <tr class="clickable-row" @click="toggleStockTrades(selectedBacktestHypothesis.id, stock.stockCode)">
                                  <td class="stock-name-cell"><n-text strong>{{ stock.stockName || stock.stockCode }}</n-text><br><n-text depth="3">{{ stock.stockCode }}</n-text></td>
                                  <td class="number-cell">{{ stock.tradeCount }}</td>
                                  <td class="number-cell">{{ formatPercent(stock.winRate) }}</td>
                                  <td class="number-cell">{{ formatPercent(stock.avgReturn) }} / {{ formatPercent(stock.medianReturn) }}</td>
                                  <td class="number-cell">{{ formatPercent(stock.avgMfe) }} / {{ formatPercent(stock.avgMae) }}</td>
                                  <td class="number-cell"><span :class="returnTone(stock.pnl)">{{ formatMoney(stock.pnl) }}</span></td>
                                  <td class="number-cell">{{ stock.avgHoldDays.toFixed(1) }} 天</td>
                                  <td class="row-action-cell"><n-icon :component="ChevronForwardOutline" size="16"/></td>
                                </tr>
                                <tr v-if="expandedStocks[stockDetailKey(selectedBacktestHypothesis.id, stock.stockCode)]" class="trade-detail-row">
                                  <td colspan="8">
                                    <div class="nested-table-shell">
                                      <n-table size="small" :bordered="false">
                                        <thead><tr><th>信号日</th><th>买入/卖出</th><th>数量</th><th>收益</th><th>MFE/MAE</th><th>持有</th><th>退出</th><th>成本</th></tr></thead>
                                        <tbody>
                                        <tr v-for="trade in stock.trades" :key="trade.id">
                                          <td>{{ trade.signalDate }}</td>
                                          <td>{{ trade.buyDate }} {{ formatPrice(trade.buyPrice) }}<br>{{ trade.sellDate }} {{ formatPrice(trade.sellPrice) }}</td>
                                          <td>{{ Number(trade.quantity || 0).toFixed(0) }}</td>
                                          <td><span :class="returnTone(trade.returnRate)">{{ formatPercent(trade.returnRate) }}</span></td>
                                          <td>{{ formatPercent(trade.maxReturn) }} / {{ formatPercent(trade.maxDrawdown) }}</td>
                                          <td>{{ trade.holdDays }} 天</td>
                                          <td>{{ exitReasonText(trade.exitReason) }}</td>
                                          <td>{{ formatMoney(Number(trade.fee || 0) + Number(trade.slippage || 0)) }}</td>
                                        </tr>
                                        </tbody>
                                      </n-table>
                                    </div>
                                  </td>
                                </tr>
                              </template>
                              </tbody>
                            </n-table>
                          </div>
                          <n-empty v-else description="暂无股票归因数据"/>
                        </n-spin>
                      </n-tab-pane>

                      <n-tab-pane name="trades" :tab="`全部交易 ${selectedBacktestTrades.length}`">
                        <div class="table-shell strategy-table-shell" v-if="selectedBacktestTrades.length">
                          <n-table size="small" :bordered="false" class="strategy-data-table trade-table">
                            <thead><tr><th>股票</th><th>买入</th><th>卖出</th><th>数量</th><th>收益</th><th>持有</th><th>退出</th><th>成本</th></tr></thead>
                            <tbody>
                            <tr v-for="trade in selectedBacktestTrades" :key="trade.id">
                              <td class="stock-name-cell">{{ trade.stockName || trade.stockCode }}<br><n-text depth="3">{{ trade.stockCode }}</n-text></td>
                              <td>{{ trade.buyDate }}<br>{{ formatPrice(trade.buyPrice) }}</td>
                              <td>{{ trade.sellDate }}<br>{{ formatPrice(trade.sellPrice) }}</td>
                              <td class="number-cell">{{ Number(trade.quantity || 0).toFixed(0) }}</td>
                              <td class="number-cell"><span :class="returnTone(trade.returnRate)">{{ formatPercent(trade.returnRate) }}</span></td>
                              <td class="number-cell">{{ trade.holdDays }} 天</td>
                              <td>{{ exitReasonText(trade.exitReason) }}</td>
                              <td class="number-cell">{{ formatMoney(Number(trade.fee || 0) + Number(trade.slippage || 0)) }}</td>
                            </tr>
                            </tbody>
                          </n-table>
                        </div>
                        <n-empty v-else description="暂无交易明细"/>
                      </n-tab-pane>

                      <n-tab-pane name="folds" tab="滚动验证">
                        <div class="table-shell strategy-table-shell compact-table-shell" v-if="(backtestMetrics(selectedBacktestHypothesis).walkForwardFolds || []).length">
                          <n-table size="small" :bordered="false" class="strategy-data-table">
                            <thead><tr><th>验证折</th><th>区间</th><th>交易数</th><th>平均收益</th><th>最大回撤</th></tr></thead>
                            <tbody>
                            <tr v-for="(fold, index) in backtestMetrics(selectedBacktestHypothesis).walkForwardFolds" :key="index">
                              <td>第 {{ index + 1 }} 折</td>
                              <td>{{ fold.startDate }} 至 {{ fold.endDate }}</td>
                              <td>{{ fold.tradeCount }}</td>
                              <td><span :class="returnTone(fold.avgReturn)">{{ formatMetricPercent(fold.avgReturn) }}</span></td>
                              <td>{{ formatMetricPercent(fold.maxDrawdown) }}</td>
                            </tr>
                            </tbody>
                          </n-table>
                        </div>
                        <n-empty v-else description="暂无滚动验证数据"/>
                      </n-tab-pane>
                    </n-tabs>
                  </section>
                </div>
                <n-empty v-else description="暂无策略回测结果"/>
              </n-tab-pane>

            </n-tabs>
          </n-spin>
        </section>
        </section>
      </n-tab-pane>

      <n-tab-pane name="monitor">
        <template #tab>
          <span class="tab-label"><n-icon :component="PulseOutline"/>监控中心</span>
        </template>
        <section class="workspace-page monitor-page">
          <div class="monitor-summary">
            <div><span>正式监控</span><b>{{ monitorSummary.active }}</b></div>
            <div><span>前向验证</span><b>{{ monitorSummary.forward }}</b></div>
            <div><span>观察策略</span><b>{{ monitorSummary.watch }}</b></div>
            <div><span>未读提醒</span><b class="negative">{{ monitorSummary.unread }}</b></div>
            <div><span>当前持仓</span><b>{{ monitorSummary.positions }}</b></div>
          </div>

          <n-tabs v-model:value="activeMonitorView" type="line" animated class="monitor-tabs">
            <n-tab-pane name="history">
              <template #tab><span class="tab-label"><n-icon :component="TimeOutline"/>历史预测</span></template>
        <section class="monitor-panel">
          <div class="panel-heading"><span>历史预测</span><n-text depth="3">最近 {{ sortedRecentSessions.length }} 次</n-text></div>
          <div class="table-shell" v-if="sortedRecentSessions.length">
            <n-table size="small" :bordered="false">
              <thead><tr><th>场景</th><th>股票池</th><th>回测区间</th><th>状态</th><th>创建时间</th><th></th></tr></thead>
              <tbody>
              <tr v-for="s in pagedRecentSessions" :key="s.id" class="clickable-row" @click="openSession(s.id)">
                <td><n-text strong>{{ s.scene }}</n-text></td>
                <td>{{ s.stockScope }}</td>
                <td>{{ s.startDate }} 至 {{ s.endDate }}</td>
                <td><n-tag size="small" :type="s.status === 'done' ? 'success' : 'warning'">{{ s.status }}</n-tag></td>
                <td>{{ formatDateTime(s.createdAt) }}</td>
                <td><n-icon :component="ChevronForwardOutline" size="16"/></td>
              </tr>
              </tbody>
            </n-table>
          </div>
          <n-empty v-else description="暂无历史预测"/>
          <div class="pagination-row">
            <n-pagination
                v-if="sortedRecentSessions.length > listPageSize"
                v-model:page="historyPage"
                :page-size="listPageSize"
                :item-count="sortedRecentSessions.length"
                size="small"
            />
          </div>
        </section>
            </n-tab-pane>

            <n-tab-pane name="strategies">
              <template #tab><span class="tab-label"><n-icon :component="EyeOutline"/>策略监控</span></template>
        <section class="monitor-panel">
          <div class="panel-heading"><span>策略监控</span><n-text depth="3">观察池 → 前向验证 → 正式监控</n-text></div>
          <div class="table-shell" v-if="sortedMyHypotheses.length">
            <n-table size="small" :bordered="false">
              <thead><tr><th>策略</th><th>状态</th><th>前向进度</th><th>净值/回撤</th><th>模拟账户</th><th>回测质量</th><th>今日提醒</th><th>操作</th></tr></thead>
              <tbody>
              <tr v-for="h in pagedMyHypotheses" :key="h.id">
                <td><n-text strong>{{ h.name }}</n-text><br><n-text depth="3">{{ h.scene }}</n-text></td>
                <td><n-tag size="small" :type="statusType(h.status)">{{ statusText(h.status) }}</n-tag></td>
                <td style="min-width: 150px">
                  <template v-if="['paper_trade', 'active'].includes(h.status) || Number(h.paperTradeDays || 0) > 0">
                    <n-progress type="line" :height="6" :show-indicator="false" :percentage="Math.min(100, Number(h.paperTradeDays || 0) / 60 * 100)"/>
                    <n-text depth="3">{{ h.paperTradeDays || 0 }}/60 交易日 · {{ h.paperTradeCount || 0 }}/10 笔</n-text>
                  </template>
                  <n-text v-else depth="3">尚未开始</n-text>
                </td>
                <td>
                  <template v-if="['paper_trade', 'active'].includes(h.status) || Number(h.paperTradeDays || 0) > 0">
                    <span :class="returnTone(Number(h.paperNav || 1) - 1)">{{ Number(h.paperNav || 1).toFixed(4) }}</span><br>
                    <n-text depth="3">回撤 {{ formatPercent(h.paperMaxDrawdown) }}</n-text>
                  </template>
                  <span v-else>-</span>
                </td>
                <td>
                  <template v-if="['paper_trade', 'active'].includes(h.status) || Number(h.paperTradeDays || 0) > 0">
                    {{ formatMoney(h.paperCash) }}<br>
                    <n-text depth="3">市值 {{ formatMoney(h.paperMarketValue) }} · {{ h.paperPositionCount || 0 }} 仓</n-text>
                  </template>
                  <span v-else>-</span>
                </td>
                <td><n-tag size="small" :bordered="false" :type="qualityType(backtestMetrics(h).qualityRating)">{{ qualityText(backtestMetrics(h).qualityRating) }}</n-tag><br><n-text depth="3">{{ h.tradeCount }} 样本</n-text></td>
                <td>{{ todayAlertCountForHypothesis(h) }}</td>
                <td>
                  <n-space size="small">
                    <n-tooltip v-if="['paper_trade', 'active'].includes(h.status) || Number(h.paperTradeDays || 0) > 0" trigger="hover">
                      <template #trigger><n-button size="tiny" secondary @click="openPaperDetails(h)">验证数据</n-button></template>
                      查看现金、持仓、交易、净值和回撤
                    </n-tooltip>
                    <n-tooltip v-if="h.status === 'paper_trade'" trigger="hover">
                      <template #trigger><n-button size="tiny" type="primary" :disabled="!h.paperReady" @click="activateHypothesis(h)">启用正式监控</n-button></template>
                      {{ h.paperReadyReason || '前向验证尚未达到门槛' }}
                    </n-tooltip>
                    <n-tooltip trigger="hover">
                      <template #trigger>
                        <n-button quaternary circle size="small" @click="openMonitoredHypothesis(h)">
                          <template #icon><n-icon :component="EyeOutline"/></template>
                        </n-button>
                      </template>
                      打开回测分析
                    </n-tooltip>
                  </n-space>
                </td>
              </tr>
              </tbody>
            </n-table>
          </div>
          <n-empty v-else description="暂无保存的策略"/>
          <div class="pagination-row">
            <n-pagination
                v-if="sortedMyHypotheses.length > listPageSize"
                v-model:page="monitorPage"
                :page-size="listPageSize"
                :item-count="sortedMyHypotheses.length"
                size="small"
            />
          </div>
        </section>
            </n-tab-pane>

            <n-tab-pane name="alerts">
              <template #tab><span class="tab-label"><n-icon :component="NotificationsOutline"/>提醒事件</span></template>
        <section class="monitor-panel">
          <div class="panel-heading">
            <span>提醒事件</span>
            <n-space size="small">
              <n-tooltip trigger="hover">
                <template #trigger>
                  <n-button quaternary circle size="small" @click="loadPredictionAlerts">
                    <template #icon><n-icon :component="RefreshOutline"/></template>
                  </n-button>
                </template>
                刷新提醒
              </n-tooltip>
              <n-button size="small" type="primary" :loading="alertLoading" @click="scanPredictionAlerts">
                <template #icon><n-icon :component="PulseOutline"/></template>
                立即扫描
              </n-button>
            </n-space>
          </div>
          <n-spin :show="alertLoading">
            <div class="table-shell" v-if="sortedAlertRows.length">
              <n-table size="small" :bordered="false" :single-line="false">
                <thead><tr><th>股票</th><th>事件</th><th>建议</th><th>触发价格</th><th>阈值</th><th>时间</th><th>状态</th><th></th></tr></thead>
                <tbody>
                <tr v-for="row in pagedAlertRows" :key="row.id">
                  <td><n-text strong>{{ row.stockName || row.stockCode }}</n-text><br><n-text depth="3">{{ row.stockCode }}</n-text></td>
                  <td><n-tag size="small" :type="alertLevelType(row.level)">{{ alertTypeText(row.alertType) }}</n-tag><br><n-text depth="3">{{ row.message }}</n-text></td>
                  <td>{{ alertActionText(row.suggestedAction) }}</td>
                  <td>{{ formatPrice(row.triggerPrice) }}</td>
                  <td>{{ alertThresholdText(row) }}</td>
                  <td>{{ formatDateTime(row.triggeredAt) }}</td>
                  <td><n-tag size="small" :type="alertStatusType(row.status)">{{ alertStatusText(row.status) }}</n-tag></td>
                  <td>
                    <n-space v-if="row.status !== 'resolved'" size="small" :wrap="false">
                      <n-button size="tiny" secondary @click="markAlert(row, 'read')">已读</n-button>
                      <n-button size="tiny" secondary @click="markAlert(row, 'ignored')">忽略</n-button>
                    </n-space>
                  </td>
                </tr>
                </tbody>
              </n-table>
            </div>
            <n-empty v-else description="暂无提醒事件"/>
            <div class="pagination-row">
                <n-pagination
                    v-if="sortedAlertRows.length > listPageSize"
                    v-model:page="alertPage"
                    :page-size="listPageSize"
                    :item-count="sortedAlertRows.length"
                    size="small"
                />
            </div>
          </n-spin>
        </section>
            </n-tab-pane>

            <n-tab-pane name="positions">
              <template #tab><span class="tab-label"><n-icon :component="WalletOutline"/>持仓流水</span></template>
              <section class="monitor-panel">
                <div class="panel-heading">
                  <span>持仓流水</span>
                  <n-button size="small" type="primary" @click="openNewTradeModal">
                    <template #icon><n-icon :component="AddOutline"/></template>
                    添加流水
                  </n-button>
                </div>
                <n-spin :show="positionLoading">
                  <div class="table-shell" v-if="positionRows.length">
                    <n-table size="small" :bordered="false">
                      <thead><tr><th>股票</th><th>持仓/成本</th><th>市值</th><th>浮动盈亏</th><th>已实现</th><th>交易次数</th><th>最近交易</th><th>操作</th></tr></thead>
                      <tbody>
                      <tr v-for="row in positionRows" :key="row.stockCode">
                        <td><n-text strong>{{ row.stockName || row.stockCode }}</n-text><br><n-text depth="3">{{ row.stockCode }}</n-text></td>
                        <td>{{ row.currentVolume }} 股/份<br><n-text depth="3">均价 {{ formatPrice(row.avgCostPrice) }}</n-text></td>
                        <td>{{ formatMoney(row.marketValue) }}</td>
                        <td><span :class="returnTone(row.floatingProfit)">{{ formatMoney(row.floatingProfit) }} / {{ formatPercent(row.floatingProfitRate) }}</span></td>
                        <td><span :class="returnTone(row.realizedProfit)">{{ formatMoney(row.realizedProfit) }}</span></td>
                        <td>{{ row.buyCount }} 买 / {{ row.sellCount }} 卖</td>
                        <td>{{ formatDateTime(row.lastTradeTime) }}</td>
                        <td>
                          <n-space size="small" :wrap="false">
                            <n-button size="tiny" @click="openTradeRecords(row)">流水</n-button>
                            <n-button size="tiny" @click="openTradeModal(row, '买入')">买入</n-button>
                            <n-button size="tiny" @click="openTradeModal(row, '卖出')">卖出</n-button>
                          </n-space>
                        </td>
                      </tr>
                      </tbody>
                    </n-table>
                  </div>
                  <n-empty v-else description="暂无持仓与交易流水"/>
                </n-spin>
              </section>
            </n-tab-pane>

            <n-tab-pane name="tasks">
              <template #tab><span class="tab-label"><n-icon :component="SettingsOutline"/>数据任务</span></template>
              <section class="monitor-panel">
                <div class="panel-heading"><span>数据任务</span><n-text depth="3">特征、资金流、信号与提醒</n-text></div>
                <div class="table-shell" v-if="cronStatus.length">
                  <n-table size="small" :bordered="false">
                    <thead><tr><th>任务</th><th>状态</th><th>下次执行</th><th>最近结果</th><th></th></tr></thead>
                    <tbody>
                    <tr v-for="task in cronStatus" :key="task.taskType">
                      <td><n-text strong>{{ task.name || task.taskType }}</n-text><br><n-text depth="3">{{ task.description }}</n-text></td>
                      <td><n-tag size="small" :type="task.enable ? 'success' : 'default'">{{ task.enable ? '启用' : '停用' }}</n-tag></td>
                      <td>{{ task.nextRunAt || '-' }}</td>
                      <td>{{ task.lastRunResult || '-' }}</td>
                      <td>
                        <n-tooltip trigger="hover">
                          <template #trigger>
                            <n-button quaternary circle size="small" @click="runCronTask(task.taskType)">
                              <template #icon><n-icon :component="PlayOutline"/></template>
                            </n-button>
                          </template>
                          立即执行
                        </n-tooltip>
                      </td>
                    </tr>
                    </tbody>
                  </n-table>
                </div>
                <n-empty v-else description="暂无系统任务"/>
              </section>
            </n-tab-pane>
          </n-tabs>
        </section>
      </n-tab-pane>
    </n-tabs>

    <n-drawer v-model:show="candidateDrawerVisible" :width="480" placement="right">
      <n-drawer-content :title="`${selectedCandidate?.stockName || selectedCandidate?.stockCode || '候选'} · 候选依据`" closable>
        <template v-if="selectedCandidate">
          <div class="drawer-metrics">
            <div><span>短线</span><b>{{ Number(selectedCandidate.shortScore || 0).toFixed(1) }}</b></div>
            <div><span>波段</span><b>{{ Number(selectedCandidate.swingScore || 0).toFixed(1) }}</b></div>
            <div><span>趋势</span><b>{{ Number(selectedCandidate.trendScore || 0).toFixed(1) }}</b></div>
            <div><span>来源数</span><b>{{ selectedCandidate.sourceCount || 0 }}</b></div>
          </div>
          <section class="drawer-section">
            <div class="drawer-section-title">来源链路</div>
            <n-space><n-tag v-for="source in candidateSources(selectedCandidate)" :key="source" size="small">{{ sourceLabel(source) }}</n-tag></n-space>
            <n-text depth="3">可用时间 {{ formatDateTime(selectedCandidate.availableAt) }} · {{ selectedCandidate.scorerVersion }}</n-text>
          </section>
          <section class="drawer-section">
            <div class="drawer-section-title">命中理由</div>
            <ul class="signal-list"><li v-for="reason in candidateReasons(selectedCandidate)" :key="reason">{{ reason }}</li></ul>
          </section>
          <section class="drawer-section">
            <div class="drawer-section-title">风险与失效条件</div>
            <ul class="signal-list"><li v-for="risk in candidateRisks(selectedCandidate)" :key="risk">{{ risk }}</li></ul>
          </section>
          <section class="drawer-section">
            <div class="drawer-section-title">观察计划</div>
            <dl class="detail-list">
              <div><dt>状态</dt><dd>{{ candidateAdvice(selectedCandidate).status || 'candidate_only' }}</dd></div>
              <div><dt>参考价</dt><dd>{{ formatPrice(candidateAdvice(selectedCandidate).referencePrice) }}</dd></div>
              <div><dt>不追高价</dt><dd>{{ formatPrice(candidateAdvice(selectedCandidate).noChasePrice) }}</dd></div>
              <div><dt>失效止损</dt><dd>{{ formatPrice(candidateAdvice(selectedCandidate).stopLossPrice) }}</dd></div>
              <div><dt>最长观察</dt><dd>{{ candidateAdvice(selectedCandidate).maxHoldingDays || '-' }} 个交易日</dd></div>
            </dl>
            <n-alert type="warning" :show-icon="false">{{ candidateAdvice(selectedCandidate).formalAdviceReason || '必须先通过量化验证和前向模拟。' }}</n-alert>
          </section>
        </template>
      </n-drawer-content>
    </n-drawer>

    <n-drawer v-model:show="decisionDrawerVisible" :width="480" placement="right">
      <n-drawer-content
          :title="`${selectedDecision?.stockName || selectedDecision?.stockCode || '操作建议'} · ${selectedDecision?.actionText || selectedDecision?.action || ''}`"
          closable
      >
        <template v-if="selectedDecision">
          <div class="drawer-metrics">
            <div><span>经验后验胜率</span><b>{{ formatPercent(selectedDecision.probability) }}</b></div>
            <div><span>预期净收益</span><b :class="returnTone(selectedDecision.expectedReturn)">{{ formatPercent(selectedDecision.expectedReturn) }}</b></div>
            <div><span>综合评分</span><b>{{ formatScore(selectedDecision.score) }}</b></div>
            <div><span>建议仓位</span><b>{{ selectedDecision.quantityPercent ? `${Number(selectedDecision.quantityPercent).toFixed(0)}%` : '-' }}</b></div>
          </div>

          <section class="drawer-section">
            <div class="drawer-section-title">执行参考</div>
            <dl class="detail-list">
              <div><dt>仓位建议</dt><dd>{{ selectedDecision.positionAdvice || '-' }}</dd></div>
              <div><dt>当前 / 成本</dt><dd>{{ formatPrice(selectedDecision.currentPrice) }} / {{ formatPrice(selectedDecision.costPrice) }}</dd></div>
              <div><dt>防守 / 止损</dt><dd>{{ formatPrice(selectedDecision.defensePrice) }} / {{ formatPrice(selectedDecision.stopLossPrice) }}</dd></div>
              <div><dt>止盈 / 减仓</dt><dd>{{ formatPrice(selectedDecision.takeProfitPrice) }}</dd></div>
              <div><dt>建议数量</dt><dd>{{ selectedDecision.suggestedQuantity || 0 }} 股/份 · {{ formatMoney(selectedDecision.suggestedAmount) }}</dd></div>
            </dl>
          </section>

          <section class="drawer-section">
            <div class="drawer-section-title">判断依据</div>
            <ul class="signal-list">
              <li v-for="reason in parseJsonList(selectedDecision.reasonsJson)" :key="reason">{{ reason }}</li>
              <li v-if="!parseJsonList(selectedDecision.reasonsJson).length">暂无强信号</li>
            </ul>
          </section>

          <section class="drawer-section" v-if="parseJsonList(selectedDecision.risksJson).length">
            <div class="drawer-section-title">风险提示</div>
            <ul class="signal-list risk-list">
              <li v-for="risk in parseJsonList(selectedDecision.risksJson)" :key="risk">{{ risk }}</li>
            </ul>
          </section>

          <section class="drawer-section" v-if="matchedStrategyRows(selectedDecision).length">
            <div class="drawer-section-title">命中策略</div>
            <div class="matched-strategies">
              <div v-for="strategy in matchedStrategyRows(selectedDecision)" :key="strategy.id || strategy.name">
                <n-text strong>{{ strategy.name }}</n-text>
                <n-tag size="small" :bordered="false" :type="strategy.entryMatched ? 'success' : 'warning'">
                  {{ strategy.entryMatched ? '入场命中' : '退出命中' }}
                </n-tag>
                <n-text depth="3">单股 {{ strategy.stockTradeCount || 0 }} 笔 / 股票池 {{ strategy.poolTradeCount || strategy.tradeCount || 0 }} 笔</n-text>
              </div>
            </div>
          </section>

          <section class="drawer-section">
            <div class="drawer-section-title">数据状态</div>
            <n-space vertical size="small">
              <n-text>{{ capitalFlowText(selectedDecision.capitalFlowJson) || '资金流数据不足' }}</n-text>
              <n-text depth="3">{{ sampleSummaryText(selectedDecision) || '暂无匹配样本摘要' }}</n-text>
              <n-text depth="3">{{ dataStatusText(selectedDecision) || '暂无数据区间摘要' }}</n-text>
            </n-space>
          </section>
        </template>
      </n-drawer-content>
    </n-drawer>

    <n-modal
        v-model:show="showPaperModal"
        preset="card"
        :title="`前向验证 · ${selectedPaperHypothesis?.name || ''}`"
        style="width: 1100px; max-width: calc(100vw - 32px);"
    >
      <n-spin :show="paperDetailsLoading">
        <template v-if="paperDetails?.account?.id">
          <n-alert :type="paperDetails.ready ? 'success' : 'info'" :show-icon="false" style="margin-bottom: 14px">
            {{ paperDetails.reason }}
          </n-alert>
          <div class="metric-strip strategy-metrics">
            <div><span>交易日</span><b>{{ paperDetails.account.tradingDays || 0 }}</b></div>
            <div><span>已完成交易</span><b>{{ paperDetails.account.tradeCount || 0 }}</b></div>
            <div><span>当前净值</span><b :class="returnTone(Number(paperDetails.account.nav || 1) - 1)">{{ Number(paperDetails.account.nav || 1).toFixed(4) }}</b></div>
            <div><span>最大回撤</span><b>{{ formatPercent(paperDetails.account.maxDrawdown) }}</b></div>
            <div><span>可用现金</span><b>{{ formatMoney(paperDetails.account.cash) }}</b></div>
            <div><span>持仓市值</span><b>{{ formatMoney(paperDetails.account.marketValue) }}</b></div>
          </div>

          <div class="chart-panel" style="margin-top: 14px">
            <div class="panel-heading"><span>前向净值与回撤</span><n-text depth="3">最近处理 {{ paperDetails.account.lastProcessedDate || '-' }}</n-text></div>
            <div ref="paperChartRef" class="strategy-chart" v-if="(paperDetails.dailies || []).length"></div>
            <n-empty v-else size="small" description="等待首个交易日快照"/>
          </div>

          <n-tabs type="line" style="margin-top: 12px">
            <n-tab-pane name="positions" :tab="`当前持仓 ${(paperDetails.positions || []).length}`">
              <div class="table-shell" v-if="(paperDetails.positions || []).length">
                <n-table size="small" :bordered="false">
                  <thead><tr><th>股票</th><th>信号/入场</th><th>数量</th><th>入场价/成本</th><th>最新价</th><th>市值</th><th>浮动收益</th><th>退出状态</th></tr></thead>
                  <tbody><tr v-for="row in paperDetails.positions" :key="row.id">
                    <td>{{ row.stockName || row.stockCode }}<br><n-text depth="3">{{ row.stockCode }}</n-text></td>
                    <td>{{ row.signalDate }}<br>{{ row.entryDate }}</td>
                    <td>{{ Number(row.quantity || 0).toFixed(0) }}</td>
                    <td>{{ formatPrice(row.entryPrice) }}<br><n-text depth="3">{{ formatPrice(row.avgCost) }}</n-text></td>
                    <td>{{ formatPrice(row.lastPrice) }}</td>
                    <td>{{ formatMoney(Number(row.lastPrice || 0) * Number(row.quantity || 0)) }}</td>
                    <td><span :class="returnTone(Number(row.lastPrice || 0) / Number(row.avgCost || 1) - 1)">{{ formatPercent(Number(row.lastPrice || 0) / Number(row.avgCost || 1) - 1) }}</span></td>
                    <td>{{ row.pendingExitReason ? `等待成交：${exitReasonText(row.pendingExitReason)}` : '持有' }}</td>
                  </tr></tbody>
                </n-table>
              </div>
              <n-empty v-else size="small" description="当前无持仓"/>
            </n-tab-pane>
            <n-tab-pane name="trades" :tab="`完成交易 ${(paperDetails.trades || []).length}`">
              <div class="table-shell" v-if="(paperDetails.trades || []).length">
                <n-table size="small" :bordered="false">
                  <thead><tr><th>股票</th><th>买入/卖出</th><th>数量</th><th>收益</th><th>MFE/MAE</th><th>费用+滑点</th><th>持有</th><th>退出</th></tr></thead>
                  <tbody><tr v-for="row in paperDetails.trades" :key="row.id">
                    <td>{{ row.stockName || row.stockCode }}<br><n-text depth="3">{{ row.stockCode }}</n-text></td>
                    <td>{{ row.buyDate }} {{ formatPrice(row.buyPrice) }}<br>{{ row.sellDate }} {{ formatPrice(row.sellPrice) }}</td>
                    <td>{{ Number(row.quantity || 0).toFixed(0) }}</td>
                    <td><span :class="returnTone(row.returnRate)">{{ formatPercent(row.returnRate) }}</span></td>
                    <td>{{ formatPercent(row.maxReturn) }} / {{ formatPercent(row.maxDrawdown) }}</td>
                    <td>{{ formatMoney(Number(row.fee || 0) + Number(row.slippage || 0)) }}</td>
                    <td>{{ row.holdDays }} 天</td>
                    <td>{{ exitReasonText(row.exitReason) }}</td>
                  </tr></tbody>
                </n-table>
              </div>
              <n-empty v-else size="small" description="尚无完成交易"/>
            </n-tab-pane>
          </n-tabs>
        </template>
        <n-empty v-else description="尚未开始前向验证"/>
      </n-spin>
    </n-modal>

    <n-modal
        v-model:show="showRecordsModal"
        preset="card"
        title="交易流水"
        style="width: 920px; max-width: calc(100vw - 32px);"
    >
      <n-space vertical>
        <n-space justify="space-between" align="center">
          <n-space vertical size="small">
            <n-text strong>{{ selectedPosition?.stockName || selectedPosition?.stockCode }}</n-text>
            <n-text depth="3">
              当前持仓 {{ selectedPosition?.currentVolume || 0 }}，均价 {{ formatPrice(selectedPosition?.avgCostPrice) }}
            </n-text>
          </n-space>
          <n-space>
            <n-button size="small" @click="openTradeModal(selectedPosition, '买入')">补买入</n-button>
            <n-button size="small" @click="openTradeModal(selectedPosition, '卖出')">补卖出</n-button>
          </n-space>
        </n-space>

        <n-table size="small" :bordered="false" v-if="tradeRecords.length">
          <thead>
          <tr>
            <th>时间</th>
            <th>方向</th>
            <th>价格</th>
            <th>数量</th>
            <th>金额</th>
            <th>手续费</th>
            <th>原因</th>
          </tr>
          </thead>
          <tbody>
          <tr v-for="record in tradeRecords" :key="recordField(record, 'ID', 'id')">
            <td>{{ formatDateTime(recordField(record, 'TradingTime', 'tradingTime')) }}</td>
            <td>
              <n-tag size="small" :type="recordField(record, 'Direction', 'direction') === '买入' ? 'error' : 'success'">
                {{ recordField(record, 'Direction', 'direction') }}
              </n-tag>
            </td>
            <td>{{ formatPrice(recordField(record, 'Price', 'price')) }}</td>
            <td>{{ recordField(record, 'Volume', 'volume') }}</td>
            <td>{{ formatMoney(recordAmount(record)) }}</td>
            <td>{{ formatMoney(recordField(record, 'Fee', 'fee') || 0) }}</td>
            <td>{{ recordField(record, 'Reason', 'reason') || '-' }}</td>
          </tr>
          </tbody>
        </n-table>
        <n-empty v-else description="暂无交易流水"/>
      </n-space>
    </n-modal>

    <n-modal
        v-model:show="showTradeModal"
        preset="card"
        title="添加交易流水"
        style="width: 640px; max-width: calc(100vw - 32px);"
    >
      <n-form label-placement="left" label-width="88px">
        <n-form-item label="股票代码">
          <n-input v-model:value="tradeForm.StockCode" placeholder="例如 sh515880" />
        </n-form-item>
        <n-form-item label="股票名称">
          <n-input v-model:value="tradeForm.StockName" placeholder="可选" />
        </n-form-item>
        <n-form-item label="方向">
          <n-select v-model:value="tradeForm.Direction" :options="tradeDirectionOptions" />
        </n-form-item>
        <n-form-item label="交易时间">
          <n-date-picker v-model:value="tradeForm.TradingTime" type="datetime" style="width: 100%" />
        </n-form-item>
        <n-form-item label="成交价格">
          <n-input-number v-model:value="tradeForm.Price" :min="0" :precision="3" style="width: 100%" />
        </n-form-item>
        <n-form-item label="成交数量">
          <n-input-number v-model:value="tradeForm.Volume" :min="0" :precision="0" style="width: 100%" />
        </n-form-item>
        <n-form-item label="手续费">
          <n-input-number v-model:value="tradeForm.Fee" :min="0" :precision="2" style="width: 100%" />
        </n-form-item>
        <n-form-item label="原因">
          <n-input v-model:value="tradeForm.Reason" type="textarea" placeholder="可选" />
        </n-form-item>
        <n-space justify="end">
          <n-button @click="showTradeModal = false">取消</n-button>
          <n-button type="primary" @click="addTradeRecord">保存流水</n-button>
        </n-space>
      </n-form>
    </n-modal>
  </div>
</template>

<style scoped>
.prediction-factory {
  min-height: calc(100vh - 118px);
  padding: 0 16px 92px;
  background: #f5f7f9;
  color: #20242b;
}

.factory-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 52px;
  border-bottom: 1px solid #e2e6eb;
}

.factory-title-block,
.tab-label,
.panel-heading,
.strategy-item-head,
.strategy-detail-header {
  display: flex;
  align-items: center;
}

.factory-title-block {
  gap: 10px;
}

.factory-title {
  font-size: 19px;
  font-weight: 650;
}

.tab-label {
  gap: 6px;
}

.workspace-tabs :deep(.n-tabs-nav) {
  position: sticky;
  top: 0;
  z-index: 4;
  padding-top: 2px;
  background: #f5f7f9;
}

.workspace-page {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding-top: 2px;
}

.command-surface,
.results-surface,
.monitor-panel,
.monitor-summary {
  background: #ffffff;
  border: 1px solid #e2e6eb;
  border-radius: 6px;
}

.command-surface {
  padding: 14px 16px 12px;
}

.command-form {
  display: flex;
  align-items: flex-end;
  gap: 12px;
  flex-wrap: wrap;
}

.command-field {
  width: 142px;
  margin-bottom: 0;
}

.command-field.ai-field {
  width: 220px;
}

.command-field.date-field {
  width: 372px;
}

.command-actions {
  margin-bottom: 0;
  margin-left: auto;
}

.command-actions :deep(.n-form-item-blank) {
  justify-content: flex-end;
}

.date-separator {
  flex: 0 0 auto;
  color: #8a929f;
}

.data-status-strip {
  margin-top: 12px;
  padding-top: 11px;
  border-top: 1px solid #edf0f3;
}

.data-status-main {
  display: flex;
  align-items: center;
  gap: 20px;
  min-height: 28px;
  flex-wrap: wrap;
}

.coverage-metrics {
  display: flex;
  gap: 18px;
  color: #737b88;
  font-size: 13px;
}

.coverage-metrics b {
  color: #303640;
  font-weight: 600;
}

.sync-inline {
  display: grid;
  grid-template-columns: auto 150px auto;
  align-items: center;
  gap: 8px;
  margin-left: auto;
  color: #737b88;
  font-size: 12px;
}

.sync-error {
  margin-top: 10px;
  white-space: pre-wrap;
}

.results-surface {
  padding: 16px;
}

.result-heading,
.panel-heading {
  display: flex;
  justify-content: space-between;
  gap: 16px;
}

.result-heading {
  align-items: flex-start;
}

.result-heading h2,
.strategy-detail-header h3 {
  margin: 0;
  font-size: 17px;
  font-weight: 650;
}

.decision-summary,
.monitor-summary,
.metric-strip,
.drawer-metrics {
  display: grid;
}

.decision-summary {
  grid-template-columns: repeat(5, minmax(0, 1fr));
  margin-top: 14px;
  border: 1px solid #e7eaee;
  border-radius: 4px;
  background: #fafbfc;
}

.summary-cell,
.metric-strip > div,
.drawer-metrics > div,
.monitor-summary > div {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.summary-cell {
  padding: 10px 14px;
  border-right: 1px solid #e7eaee;
}

.summary-cell:last-child,
.metric-strip > div:last-child {
  border-right: 0;
}

.summary-cell span,
.metric-strip span,
.drawer-metrics span,
.monitor-summary span {
  color: #737b88;
  font-size: 12px;
}

.summary-cell b,
.metric-strip b,
.drawer-metrics b,
.monitor-summary b {
  font-variant-numeric: tabular-nums;
  font-size: 20px;
  line-height: 1.2;
}

.analysis-tabs,
.strategy-detail-tabs,
.monitor-tabs {
  margin-top: 10px;
}

.table-shell,
.nested-table-shell {
  width: 100%;
  overflow-x: auto;
  border: 1px solid #e7eaee;
  border-radius: 4px;
}

.table-shell :deep(table) {
  min-width: 850px;
}

.decision-table :deep(table) {
  min-width: 940px;
}

.table-shell :deep(th) {
  background: #f8f9fb;
  color: #616976;
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
}

.table-shell :deep(td) {
  vertical-align: middle;
  font-variant-numeric: tabular-nums;
}

.clickable-row {
  cursor: pointer;
}

.table-shell :deep(tbody tr:hover td) {
  background: #f5f8fc;
}

.positive {
  color: #c43d3d !important;
}

.negative {
  color: #16835b !important;
}

.neutral {
  color: #737b88 !important;
}

.strategy-workbench {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.strategy-rank-pane,
.strategy-detail-pane {
  border: 1px solid #e2e6eb;
  border-radius: 6px;
  background: #ffffff;
}

.strategy-rank-pane {
  padding: 12px 14px 14px;
}

.pane-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 13px;
  font-weight: 600;
}

.strategy-rank-title {
  margin-bottom: 10px;
}

.rank-title-meta {
  display: flex;
  align-items: center;
  gap: 12px;
}

.strategy-list {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(238px, 1fr));
  gap: 8px;
}

.strategy-list-item {
  display: grid;
  grid-template-columns: 34px minmax(0, 1fr);
  align-items: stretch;
  gap: 10px;
  width: 100%;
  min-height: 92px;
  padding: 10px 11px 11px;
  border: 1px solid #dfe3e8;
  border-radius: 4px;
  background: #ffffff;
  color: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.15s ease, background 0.15s ease;
}

.strategy-list-item:hover {
  border-color: #aeb8c5;
}

.strategy-list-item.active {
  border-color: #2563eb;
  background: #f4f7fd;
  box-shadow: inset 3px 0 0 #2563eb;
}

.strategy-rank-index {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 28px;
  border-radius: 4px;
  background: #eef2f7;
  color: #667085;
  font-size: 12px;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
}

.strategy-list-item.active .strategy-rank-index {
  background: #dbe8ff;
  color: #1d4ed8;
}

.strategy-item-body {
  min-width: 0;
}

.strategy-item-head {
  justify-content: space-between;
  gap: 8px;
}

.strategy-item-head strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.strategy-item-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 5px;
  margin-top: 9px;
  color: #737b88;
  font-size: 11px;
}

.strategy-item-metrics b {
  display: block;
  margin-top: 2px;
  color: #303640;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.strategy-detail-pane {
  min-width: 0;
  padding: 15px 16px 16px;
}

.strategy-detail-header {
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
  padding-bottom: 12px;
  border-bottom: 1px solid #edf0f3;
}

.strategy-title-copy {
  min-width: 0;
}

.strategy-title-line {
  min-width: 0;
}

.strategy-description {
  display: block;
  max-width: 920px;
  margin-top: 6px;
  line-height: 1.55;
}

.strategy-actions {
  flex: 0 0 auto;
}

.strategy-metrics {
  grid-template-columns: repeat(6, minmax(0, 1fr));
  margin-top: 12px;
  border: 1px solid #e7eaee;
  border-radius: 4px;
}

.metric-strip > div {
  padding: 9px 11px;
  border-right: 1px solid #e7eaee;
}

.metric-strip b {
  font-size: 16px;
}

.chart-panel {
  margin-top: 12px;
  padding: 11px 12px 5px;
  border: 1px solid #e7eaee;
  border-radius: 4px;
  background: #ffffff;
}

.panel-heading {
  align-items: center;
  min-height: 32px;
  margin-bottom: 10px;
  font-size: 14px;
  font-weight: 600;
}

.strategy-chart {
  width: 100%;
  height: 250px;
}

.strategy-detail-tabs {
  overflow: hidden;
  border: 1px solid #e7eaee;
  border-radius: 4px;
  background: #ffffff;
}

.strategy-detail-tabs :deep(.n-tabs-nav) {
  padding: 0 12px;
  background: #f8f9fb;
}

.strategy-detail-tabs :deep(.n-tab-pane) {
  padding: 12px;
}

.strategy-table-shell {
  max-height: min(48vh, 460px);
  overflow: auto;
}

.compact-table-shell {
  max-height: 340px;
}

.strategy-table-shell :deep(table) {
  min-width: 980px;
}

.strategy-table-shell :deep(th) {
  position: sticky;
  top: 0;
  z-index: 1;
}

.strategy-data-table :deep(td),
.strategy-data-table :deep(th) {
  padding: 10px 11px;
}

.stock-name-cell {
  min-width: 132px;
}

.number-cell {
  text-align: right;
  white-space: nowrap;
}

.row-action-cell {
  width: 34px;
  text-align: center;
}

.trade-detail-row td {
  padding: 10px 12px !important;
  background: #fafbfc !important;
}

.nested-table-shell {
  background: #ffffff;
}

.monitor-summary {
  grid-template-columns: repeat(5, minmax(0, 1fr));
  padding: 0;
}

.monitor-summary > div {
  padding: 12px 16px;
  border-right: 1px solid #e7eaee;
}

.monitor-summary > div:last-child {
  border-right: 0;
}

.monitor-panel {
  min-height: 430px;
  padding: 14px 16px 18px;
}

.pagination-row {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}

.drawer-metrics {
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.drawer-metrics > div {
  padding: 11px 12px;
  border: 1px solid #e3e7ec;
  border-radius: 4px;
  background: #f8f9fb;
}

.drawer-metrics b {
  font-size: 17px;
}

.drawer-section {
  margin-top: 20px;
}

.drawer-section-title {
  margin-bottom: 9px;
  color: #303640;
  font-size: 13px;
  font-weight: 650;
}

.detail-list {
  margin: 0;
}

.detail-list > div {
  display: grid;
  grid-template-columns: 92px minmax(0, 1fr);
  gap: 10px;
  padding: 7px 0;
  border-bottom: 1px solid #edf0f3;
}

.detail-list dt {
  color: #737b88;
}

.detail-list dd {
  margin: 0;
  text-align: right;
}

.signal-list {
  margin: 0;
  padding-left: 18px;
}

.signal-list li {
  margin: 7px 0;
  line-height: 1.55;
}

.risk-list li::marker {
  color: #c43d3d;
}

.matched-strategies {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.matched-strategies > div {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 5px 10px;
  padding: 9px 10px;
  border: 1px solid #e3e7ec;
  border-radius: 4px;
}

.matched-strategies .n-text:last-child {
  grid-column: 1 / -1;
}

.opportunity-page {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.candidate-form {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 10px;
}

.candidate-form :deep(.n-form-item) {
  width: 180px;
  margin-bottom: 0;
}

.candidate-form :deep(.candidate-source-field) {
  width: min(420px, 100%);
}

.candidate-form :deep(.candidate-generate-action) {
  width: auto;
}

.candidate-layout {
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
  gap: 12px;
  align-items: start;
}

.candidate-history-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 620px;
  overflow: auto;
}

.candidate-history-item {
  display: flex;
  width: 100%;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 9px 10px;
  border: 1px solid #e3e7ec;
  border-radius: 4px;
  background: transparent;
  color: inherit;
  cursor: pointer;
  text-align: left;
}

.candidate-history-item.active {
  border-color: #18a058;
  background: rgba(24, 160, 88, .06);
}

.candidate-history-item span:first-child {
  display: flex;
  min-width: 0;
  flex-direction: column;
}

.candidate-history-item strong,
.candidate-history-item small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.candidate-history-item small {
  margin-top: 3px;
  color: #7a828e;
}

.candidate-freshness {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 10px;
}

.candidate-long-warning {
  margin-bottom: 10px;
}

.candidate-table th,
.candidate-table td {
  vertical-align: middle;
}

@media (max-width: 1180px) {
  .command-field.date-field {
    width: 340px;
  }

  .command-actions {
    margin-left: 0;
  }

  .strategy-list {
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  }

  .strategy-metrics {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .metric-strip > div:nth-child(3) {
    border-right: 0;
  }
}

@media (max-width: 900px) {
  .prediction-factory {
    padding: 0 10px 88px;
  }

  .command-field,
  .command-field.ai-field,
  .command-field.date-field {
    width: 100%;
  }

  .command-actions,
  .command-actions :deep(.n-space) {
    width: 100%;
  }

  .command-actions :deep(.n-button) {
    flex: 1;
  }

  .decision-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .summary-cell,
  .summary-cell:nth-child(2) {
    border-bottom: 1px solid #e7eaee;
  }

  .strategy-rank-title,
  .rank-title-meta {
    align-items: flex-start;
    flex-direction: column;
  }

  .strategy-list {
    grid-template-columns: 1fr;
  }

  .strategy-item-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .strategy-detail-header {
    flex-direction: column;
  }

  .strategy-actions,
  .strategy-actions :deep(.n-button) {
    width: 100%;
  }

  .strategy-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .monitor-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .candidate-layout {
    grid-template-columns: 1fr;
  }

  .candidate-form :deep(.n-form-item),
  .candidate-form :deep(.candidate-source-field),
  .candidate-form :deep(.candidate-generate-action) {
    width: 100%;
  }
}
</style>
