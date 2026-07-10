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
  NStatistic,
  NGrid,
  NGi,
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
  useMessage
} from 'naive-ui'
import {
  AddTradingRecord,
  CreatePredictionSession,
  RecalculatePredictionSession,
  GetPredictionSession,
  GetPredictionSessions,
  GetMyPredictionHypotheses,
  SavePredictionObservation,
  SavePredictionHypothesis,
  GetPredictionHypothesisDailyNAV,
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
  GetTradingRecordsByStock
} from '../../wailsjs/go/main/App'

const scenes = [
  {label: '短线爆发', value: '短线爆发'},
  {label: '波段反弹', value: '波段反弹'},
  {label: '趋势持有', value: '趋势持有'},
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
const activeHypothesis = ref(null)
const featureCoverage = ref(null)
const featureFreshness = ref(null)
const syncJob = ref(null)
const cronStatus = ref([])
let syncTimer = null
let positionTimer = null
const positionRefreshIntervalMs = 15000

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
})

onUnmounted(() => {
  if (syncTimer) clearInterval(syncTimer)
  if (positionTimer) clearInterval(positionTimer)
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
    const res = await StartFeatureSync(scope, 365)
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
    message.error('启用正式监控失败: ' + (err?.message || String(err)))
  }
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
      if (showMessage) message.success('已打开历史预测')
    } else {
      if (showMessage) message.error(res.msg || '打开历史预测失败')
    }
  } catch (err) {
    if (showMessage) message.error('打开历史预测失败: ' + (err?.message || String(err)))
  }
}

async function showChart(h) {
  activeHypothesis.value = h
  chartData.value = await GetPredictionHypothesisDailyNAV(h.id)
  nextTick(() => {
    renderChart()
  })
}

function renderChart() {
  if (!chartRef.value || chartData.value.length === 0) return
  const chart = echarts.init(chartRef.value)
  const dates = chartData.value.map(d => d.date)
  const navs = chartData.value.map(d => d.nav)
  const option = {
    title: { text: '净值曲线' },
    tooltip: { trigger: 'axis' },
    xAxis: { type: 'category', data: dates },
    yAxis: { type: 'value' },
    series: [{
      type: 'line',
      data: navs,
      smooth: true,
      areaStyle: {}
    }]
  }
  chart.setOption(option)
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

function confidenceType(val) {
  if (val === 'high') return 'success'
  if (val === 'medium') return 'warning'
  return 'default'
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
  if (['BUY', 'ADD', 'HOLD'].includes(action)) return 'success'
  if (action === 'REDUCE') return 'warning'
  if (['SELL', 'AVOID'].includes(action)) return 'error'
  return 'default'
}

function statusText(status) {
  if (status === 'active') return '正式监控'
  if (status === 'watch') return '观察'
  if (status === 'disabled') return '已禁用'
  return '草稿'
}

function statusType(status) {
  if (status === 'active') return 'success'
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

function formatMetricPercent(val) {
  if (val === undefined || val === null || val === '') return '-'
  return (Number(val) * 100).toFixed(2) + '%'
}

function latestAlertForHypothesis(h) {
  const sessionId = Number(h?.sessionId || h?.SessionID || 0)
  if (!sessionId) return null
  return sortedAlertRows.value.find(row => Number(row.sessionId || row.SessionID || 0) === sessionId) || null
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
  return h.strategyVersion === 'strategy_v2' && h.noLookaheadPassed && h.tradeCount >= 30 && h.maxDrawdown <= 0.2 && h.avgReturn > 0 && h.outSampleAvgReturn >= -0.02
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
  <n-card title="AI 预测工厂" style="--wails-draggable:no-drag; min-height: 80vh;">
    <n-grid x-gap="12" :cols="2">
      <n-gi>
        <n-card title="生成新预测" size="small">
          <n-form :model="form" label-width="100">
            <n-form-item label="投资场景">
              <n-select v-model:value="form.scene" :options="scenes"/>
            </n-form-item>
            <n-form-item label="股票池">
              <n-select v-model:value="form.stockScope" :options="scopes"/>
            </n-form-item>
            <n-form-item label="股票代码" v-if="form.stockScope === 'stock'">
              <n-input v-model:value="form.stockCode" placeholder="例如：000001"/>
            </n-form-item>
            <n-form-item label="AI配置">
              <n-select
                  v-model:value="form.aiConfigId"
                  :options="aiConfigOptions"
                  :loading="aiConfigLoading"
                  placeholder="选择 AI 模型配置"
                  style="width: 220px"
              />
            </n-form-item>
            <n-form-item label="回测区间">
              <n-space>
                <n-date-picker v-model:value="form.startDate" type="date"/>
                <span>至</span>
                <n-date-picker v-model:value="form.endDate" type="date"/>
              </n-space>
            </n-form-item>
            <n-form-item>
              <n-space>
                <n-button type="primary" @click="generatePredictions" :loading="loading">
                  生成 AI 预测
                </n-button>
                <n-button @click="syncFeatures" :loading="syncLoading">
                  同步特征数据
                </n-button>
              </n-space>
            </n-form-item>
          </n-form>
          <n-card title="数据状态" size="small" style="margin-top: 12px;">
            <n-space vertical>
              <n-space>
                <n-tag :type="featureCoverage?.ready ? 'success' : 'warning'">
                  {{ featureCoverage?.ready ? '特征达标' : '需同步特征' }}
                </n-tag>
                <n-text depth="3">{{ featureCoverage?.message || '暂无覆盖率数据' }}</n-text>
              </n-space>
              <n-grid :cols="3" x-gap="8" v-if="featureCoverage">
                <n-gi><n-statistic label="股票覆盖" :value="formatRatio(featureCoverage.stockCoverage)" /></n-gi>
                <n-gi><n-statistic label="交易日覆盖" :value="formatRatio(featureCoverage.tradeDayCoverage)" /></n-gi>
                <n-gi><n-statistic label="字段覆盖" :value="formatRatio(featureCoverage.coreFieldCoverage)" /></n-gi>
              </n-grid>
              <n-text depth="3" v-if="featureFreshness?.ready">
                最新特征：{{ featureFreshness.latestDate }}，来源：{{ featureFreshness.source || '-' }}，版本：{{ featureFreshness.featureVersion || '-' }}
              </n-text>
              <div v-if="syncJob">
                <n-space justify="space-between">
                  <n-text>同步任务：{{ syncJob.status }} / {{ syncJob.finished }} 成功 / {{ syncJob.failed }} 失败 / {{ syncJob.total }} 总数</n-text>
                  <n-text>{{ syncProgress() }}%</n-text>
                </n-space>
                <n-progress type="line" :percentage="syncProgress()" />
              </div>
            </n-space>
          </n-card>

          <n-card title="持仓信息" size="small" style="margin-top: 12px;">
            <n-space vertical>
              <n-alert type="info" :show-icon="false">
                持仓来自交易日志流水，按股票代码自动汇总平均成本、当前持仓、已实现盈亏和浮动盈亏；没有流水的股票按观察股处理。
              </n-alert>
              <n-table size="small" :bordered="false" v-if="positionRows.length">
                <thead>
                <tr>
                  <th>股票</th>
                  <th>持仓/成本</th>
                  <th>市值/浮盈</th>
                  <th>已实现</th>
                  <th>交易次数</th>
                  <th>最近交易</th>
                  <th>操作</th>
                </tr>
                </thead>
                <tbody>
                <tr v-for="row in positionRows" :key="row.stockCode">
                  <td>
                    <n-space vertical size="small">
                      <n-text strong>{{ row.stockName || row.stockCode }}</n-text>
                      <n-text depth="3">{{ row.stockCode }}</n-text>
                    </n-space>
                  </td>
                  <td>
                    <n-space vertical size="small">
                      <n-text>{{ row.currentVolume }} 股/份</n-text>
                      <n-text depth="3">均价 {{ formatPrice(row.avgCostPrice) }}</n-text>
                    </n-space>
                  </td>
                  <td>
                    <n-space vertical size="small">
                      <n-text>{{ formatMoney(row.marketValue) }}</n-text>
                      <n-tag size="small" :type="row.floatingProfit >= 0 ? 'success' : 'error'">
                        {{ formatMoney(row.floatingProfit) }} / {{ formatPercent(row.floatingProfitRate) }}
                      </n-tag>
                    </n-space>
                  </td>
                  <td>
                    <n-tag size="small" :type="row.realizedProfit >= 0 ? 'success' : 'error'">
                      {{ formatMoney(row.realizedProfit) }}
                    </n-tag>
                  </td>
                  <td>
                    <n-text>{{ row.buyCount }} 买 / {{ row.sellCount }} 卖</n-text>
                  </td>
                  <td>
                    <n-text depth="3">{{ formatDateTime(row.lastTradeTime) }}</n-text>
                  </td>
                  <td>
                    <n-space>
                      <n-button size="small" @click="openTradeRecords(row)">流水</n-button>
                      <n-button size="small" @click="openTradeModal(row, '买入')">买入</n-button>
                      <n-button size="small" @click="openTradeModal(row, '卖出')">卖出</n-button>
                    </n-space>
                  </td>
                </tr>
                </tbody>
              </n-table>
              <n-empty v-else description="暂无交易流水汇总">
                <template #extra>
                  <n-button size="small" @click="openTradeModal(null, '买入')">添加交易流水</n-button>
                </template>
              </n-empty>
            </n-space>
          </n-card>

          <n-card title="预测工厂系统任务" size="small" style="margin-top: 12px;">
            <n-space vertical>
              <n-space
                  v-for="task in cronStatus"
                  :key="task.taskType"
                  justify="space-between"
                  align="center"
              >
                <n-space vertical size="small">
                  <n-text strong>{{ task.name || task.taskType }}</n-text>
                  <n-text depth="3">状态：{{ task.enable ? '启用' : '停用' }}，下次：{{ task.nextRunAt || '-' }}</n-text>
                </n-space>
                <n-button size="small" @click="runCronTask(task.taskType)">立即执行</n-button>
              </n-space>
              <n-empty v-if="!cronStatus.length" description="暂无系统任务状态"/>
            </n-space>
          </n-card>
        </n-card>

        <n-card title="本次预测结果" size="small" style="margin-top: 12px;" v-if="sessionResult">
          <n-spin :show="loading">
            <n-empty v-if="!sessionResult?.hypotheses?.length && !sessionResult?.decisions?.length" description="暂无预测结果"/>
            <n-space vertical v-else>
              <n-card title="本次操作建议" size="small" v-if="sessionResult?.decisions?.length">
                <n-table size="small" :bordered="false">
                  <thead>
                  <tr>
                    <th>股票</th>
                    <th>动作</th>
                    <th>建议数量/金额</th>
                    <th>当前价</th>
                    <th>防守/止损</th>
                    <th>止盈/减仓</th>
                    <th>评分</th>
                  </tr>
                  </thead>
                  <tbody>
                  <tr v-for="d in sessionResult.decisions" :key="d.id || d.stockCode">
                    <td>
                      <n-space vertical size="small">
                        <n-text strong>{{ d.stockName || d.stockCode }}</n-text>
                        <n-text depth="3">{{ d.stockCode }}</n-text>
                      </n-space>
                    </td>
                    <td>
                      <n-space vertical size="small">
                        <n-tag :type="actionType(d.action)">{{ d.actionText || d.action }}</n-tag>
                        <n-text depth="3">{{ d.positionAdvice }}</n-text>
                      </n-space>
                    </td>
                    <td>
                      <n-space vertical size="small">
                        <n-text>{{ d.suggestedQuantity ? `${d.suggestedQuantity} 股/份` : '-' }}</n-text>
                        <n-text depth="3">{{ d.suggestedAmount ? formatMoney(d.suggestedAmount) : '-' }}</n-text>
                      </n-space>
                    </td>
                    <td>
                      <n-space vertical size="small">
                        <n-text>{{ formatPrice(d.currentPrice) }}</n-text>
                        <n-text depth="3" v-if="d.costPrice">成本 {{ formatPrice(d.costPrice) }} / {{ formatPercent(d.profitRate) }}</n-text>
                      </n-space>
                    </td>
                    <td>
                      <n-space vertical size="small">
                        <n-text>防守 {{ formatPrice(d.defensePrice) }}</n-text>
                        <n-text depth="3">止损 {{ formatPrice(d.stopLossPrice) }}</n-text>
                      </n-space>
                    </td>
                    <td>{{ formatPrice(d.takeProfitPrice) }}</td>
                    <td>
                      <n-space vertical size="small">
                        <n-text>{{ formatScore(d.score) }}</n-text>
                        <n-tag size="small" :type="confidenceType(d.confidence)">
                          置信度 {{ confidenceText(d.confidence) }}
                        </n-tag>
                        <n-space size="small">
                          <n-tag size="small" :type="riskType(d.riskLevel)">{{ riskText(d.riskLevel) }}</n-tag>
                          <n-tag size="small" :type="qualityType(d.qualityRating)">{{ qualityText(d.qualityRating) }}</n-tag>
                        </n-space>
                        <n-tag v-if="d.sampleWarning" size="small" type="warning">样本不足</n-tag>
                      </n-space>
                    </td>
                  </tr>
                  </tbody>
                </n-table>
                <n-space vertical style="margin-top: 8px;">
                  <n-alert
                      v-for="d in sessionResult.decisions"
                      :key="`reason-${d.id || d.stockCode}`"
                      type="default"
                      :show-icon="false"
                  >
                    <b>{{ d.stockName || d.stockCode }}</b>：
                    {{ parseJsonList(d.reasonsJson).slice(0, 2).join('；') || '暂无强信号' }}
                    <span v-if="capitalFlowText(d.capitalFlowJson)">
                      资金流：{{ capitalFlowText(d.capitalFlowJson) }}
                    </span>
                    <span v-if="sampleSummaryText(d)">
                      样本：{{ sampleSummaryText(d) }}
                    </span>
                    <span v-if="dataStatusText(d)">
                      数据：{{ dataStatusText(d) }}
                    </span>
                    <span v-if="parseJsonList(d.alertJson).length">
                      提醒：{{ parseJsonList(d.alertJson).map(a => alertTypeText(a.reason)).join('，') }}
                    </span>
                    <span v-if="parseJsonList(d.risksJson).length">
                      风险：{{ parseJsonList(d.risksJson).slice(0, 2).join('；') }}
                    </span>
                  </n-alert>
                </n-space>
              </n-card>

              <n-card title="策略回测结果" size="small">
                <template #header-extra>
                  <n-button size="small" :loading="recalculateLoading" @click="recalculateCurrentSession">
                    重新回测
                  </n-button>
                </template>
                <n-space vertical>
              <n-card v-for="h in pagedBacktestHypotheses" :key="h.id" size="small">
                <div style="display: flex; justify-content: space-between; align-items: center;">
                  <div>
                    <div style="font-weight: bold; font-size: 16px;">{{ h.name }}</div>
                    <n-text depth="3">{{ h.description }}</n-text>
                  </div>
                  <n-space>
                    <n-tag size="small" :type="monitorReady(h) ? 'success' : 'warning'">
                      {{ monitorReady(h) ? '可正式监控' : '仅建议观察' }}
                    </n-tag>
                    <n-button size="small" @click="saveObservation(h.id)">保存观察</n-button>
                    <n-button size="small" type="primary" @click="saveHypothesis(h.id)">启用正式监控</n-button>
                  </n-space>
                </div>
                <n-grid x-gap="12" :cols="4" style="margin-top: 12px;">
                  <n-gi>
                    <n-statistic label="胜率" :value="formatPercent(h.winRate)"/>
                  </n-gi>
                  <n-gi>
                    <n-statistic label="平均收益" :value="formatPercent(h.avgReturn)"/>
                  </n-gi>
                  <n-gi>
                    <n-statistic label="最大回撤" :value="formatPercent(h.maxDrawdown)"/>
                  </n-gi>
                  <n-gi>
                    <n-statistic label="交易次数" :value="h.tradeCount"/>
                  </n-gi>
                </n-grid>
                <n-space size="small" style="margin-top: 10px;" align="center">
                  <n-tag size="small" :type="qualityType(backtestMetrics(h).qualityRating)">
                    {{ qualityText(backtestMetrics(h).qualityRating) }}
                  </n-tag>
                  <n-text depth="3">年化 {{ formatMetricPercent(backtestMetrics(h).annualizedReturn) }}</n-text>
                  <n-text depth="3">基准 {{ backtestMetrics(h).benchmarkCode || 'sh000300' }} {{ formatMetricPercent(backtestMetrics(h).benchmarkReturn) }}</n-text>
                  <n-text depth="3">超额 {{ formatMetricPercent(backtestMetrics(h).excessReturn) }}</n-text>
                  <n-text depth="3">换手 {{ formatMetricPercent(backtestMetrics(h).turnoverRate) }}</n-text>
                  <n-text depth="3">均持 {{ Number(backtestMetrics(h).averageHoldingDays || 0).toFixed(1) }} 天</n-text>
                </n-space>
              </n-card>
                  <n-pagination
                      v-if="sortedBacktestHypotheses.length > listPageSize"
                      v-model:page="backtestPage"
                      :page-size="listPageSize"
                      :item-count="sortedBacktestHypotheses.length"
                      size="small"
                  />
                </n-space>
              </n-card>
            </n-space>
          </n-spin>
        </n-card>
      </n-gi>

      <n-gi>
        <n-card title="历史预测" size="small">
          <n-empty v-if="!sortedRecentSessions.length" description="暂无历史预测"/>
          <n-space vertical v-else>
            <n-card v-for="s in pagedRecentSessions" :key="s.id" size="small">
              <n-space justify="space-between" align="center">
                <n-space vertical size="small">
                  <n-text strong>{{ s.scene }} / {{ s.stockScope }}</n-text>
                  <n-text depth="3">{{ s.startDate }} 至 {{ s.endDate }}</n-text>
                  <n-tag size="small" :type="s.status === 'done' ? 'success' : 'warning'">{{ s.status }}</n-tag>
                </n-space>
                <n-button size="small" @click="openSession(s.id)">打开</n-button>
              </n-space>
            </n-card>
            <n-pagination
                v-if="sortedRecentSessions.length > listPageSize"
                v-model:page="historyPage"
                :page-size="listPageSize"
                :item-count="sortedRecentSessions.length"
                size="small"
            />
          </n-space>
        </n-card>

        <n-card title="观察/正式监控" size="small" style="margin-top: 12px;">
          <n-empty v-if="!sortedMyHypotheses.length" description="暂无保存的假设"/>
          <n-space vertical v-else>
            <n-card v-for="h in pagedMyHypotheses" :key="h.id" size="small">
              <div style="display: flex; justify-content: space-between; align-items: center;">
                <div>
                  <div style="font-weight: bold;">{{ h.name }}</div>
                  <n-text depth="3">{{ h.description }}</n-text>
                </div>
                <n-space>
                  <n-button size="small" @click="showChart(h)">查看曲线</n-button>
                  <n-tag :type="statusType(h.status)">
                    {{ statusText(h.status) }}
                  </n-tag>
                </n-space>
              </div>
              <n-grid x-gap="12" :cols="4" style="margin-top: 12px;">
                <n-gi>
                  <n-statistic label="历史胜率" :value="formatPercent(h.winRate)"/>
                </n-gi>
                <n-gi>
                  <n-statistic label="平均收益" :value="formatPercent(h.avgReturn)"/>
                </n-gi>
                <n-gi>
                  <n-statistic label="最大回撤" :value="formatPercent(h.maxDrawdown)"/>
                </n-gi>
                <n-gi>
                  <n-statistic label="交易次数" :value="h.tradeCount"/>
                </n-gi>
              </n-grid>
              <n-space size="small" style="margin-top: 10px;" align="center">
                <n-tag size="small" :type="qualityType(backtestMetrics(h).qualityRating)">
                  {{ qualityText(backtestMetrics(h).qualityRating) }}
                </n-tag>
                <n-text depth="3">今日提醒 {{ todayAlertCountForHypothesis(h) }}</n-text>
                <n-text depth="3" v-if="latestAlertForHypothesis(h)">
                  最近：{{ latestAlertForHypothesis(h).stockName || latestAlertForHypothesis(h).stockCode }}
                  {{ alertTypeText(latestAlertForHypothesis(h).alertType) }}
                  {{ formatDateTime(latestAlertForHypothesis(h).triggeredAt) }}
                </n-text>
                <n-text depth="3" v-else>最近：暂无触发</n-text>
              </n-space>
            </n-card>
            <n-pagination
                v-if="sortedMyHypotheses.length > listPageSize"
                v-model:page="monitorPage"
                :page-size="listPageSize"
                :item-count="sortedMyHypotheses.length"
                size="small"
            />
          </n-space>
        </n-card>

        <n-card title="AI提醒事件" size="small" style="margin-top: 12px;">
          <n-spin :show="alertLoading">
            <n-space vertical>
              <n-space justify="space-between" align="center">
                <n-text depth="3">盘中止损、防守位、止盈/减仓和资金流风险提醒</n-text>
                <n-space>
                  <n-button size="small" @click="loadPredictionAlerts">刷新</n-button>
                  <n-button size="small" type="primary" @click="scanPredictionAlerts">立即扫描</n-button>
                </n-space>
              </n-space>
              <n-empty v-if="!sortedAlertRows.length" description="暂无提醒事件"/>
              <n-space vertical v-else>
                <n-card v-for="row in pagedAlertRows" :key="row.id" size="small">
                  <n-space justify="space-between" align="start">
                    <n-space vertical size="small">
                      <n-space align="center">
                        <n-text strong>{{ row.stockName || row.stockCode }}</n-text>
                        <n-tag size="small" :type="alertLevelType(row.level)">
                          {{ alertTypeText(row.alertType) }}
                        </n-tag>
                        <n-tag v-if="row.suggestedAction" size="small" type="info">
                          建议 {{ alertActionText(row.suggestedAction) }}
                        </n-tag>
                        <n-tag size="small" :type="alertStatusType(row.status)">
                          {{ alertStatusText(row.status) }}
                        </n-tag>
                      </n-space>
                      <n-text depth="3">{{ row.message }}</n-text>
                      <n-text depth="3">
                        {{ formatDateTime(row.triggeredAt) }}
                        · 当前 {{ formatPrice(row.triggerPrice) }}
                        · {{ alertThresholdText(row) }}
                      </n-text>
                    </n-space>
                    <n-space v-if="row.status !== 'resolved'" size="small">
                      <n-button size="tiny" secondary @click="markAlert(row, 'read')">已读</n-button>
                      <n-button size="tiny" secondary @click="markAlert(row, 'ignored')">忽略</n-button>
                    </n-space>
                  </n-space>
                </n-card>
                <n-pagination
                    v-if="sortedAlertRows.length > listPageSize"
                    v-model:page="alertPage"
                    :page-size="listPageSize"
                    :item-count="sortedAlertRows.length"
                    size="small"
                />
              </n-space>
            </n-space>
          </n-spin>
        </n-card>
      </n-gi>
    </n-grid>

    <n-card title="净值曲线" size="small" style="margin-top: 12px;" v-if="activeHypothesis">
      <div ref="chartRef" style="width: 100%; height: 400px;"></div>
    </n-card>

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
  </n-card>
</template>
