<script setup>
import {onMounted, ref, nextTick} from 'vue'
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
  NDatePicker,
  useMessage
} from 'naive-ui'
import {
  CreatePredictionSession,
  GetMyPredictionHypotheses,
  SavePredictionHypothesis,
  GetPredictionHypothesisDailyNAV,
  SyncStockFeatures,
  GetAiConfigs
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
const syncLoading = ref(false)
const aiConfigOptions = ref([])
const aiConfigLoading = ref(false)
const myHypotheses = ref([])
const sessionResult = ref(null)
const chartRef = ref(null)
const chartData = ref([])
const activeHypothesis = ref(null)

onMounted(() => {
  loadMyHypotheses()
  loadAiConfigs()
})

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

async function syncFeatures() {
  syncLoading.value = true
  try {
    let scope = form.value.stockScope
    if (scope === 'stock') {
      if (!form.value.stockCode) {
        message.warning('请输入股票代码')
        return
      }
      scope = 'stock_' + form.value.stockCode
    }
    const res = await SyncStockFeatures(scope)
    message.info(res)
  } catch (err) {
    message.error('同步特征失败: ' + (err?.message || String(err)))
  } finally {
    syncLoading.value = false
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

    let scope = form.value.stockScope
    if (scope === 'stock') {
      if (!form.value.stockCode) {
        message.warning('请输入股票代码')
        return
      }
      scope = 'stock_' + form.value.stockCode
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

async function saveHypothesis(id) {
  try {
    const res = await SavePredictionHypothesis(id)
    message.success(res)
    await loadMyHypotheses()
  } catch (err) {
    message.error('保存失败: ' + (err?.message || String(err)))
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
  return (val * 100).toFixed(2) + '%'
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
        </n-card>

        <n-card title="本次预测结果" size="small" style="margin-top: 12px;" v-if="sessionResult">
          <n-spin :show="loading">
            <n-empty v-if="!sessionResult?.hypotheses?.length" description="暂无预测结果"/>
            <n-space vertical v-else>
              <n-card v-for="h in sessionResult.hypotheses" :key="h.id" size="small">
                <div style="display: flex; justify-content: space-between; align-items: center;">
                  <div>
                    <div style="font-weight: bold; font-size: 16px;">{{ h.name }}</div>
                    <n-text depth="3">{{ h.description }}</n-text>
                  </div>
                  <n-button size="small" @click="saveHypothesis(h.id)">保存监控</n-button>
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
              </n-card>
            </n-space>
          </n-spin>
        </n-card>
      </n-gi>

      <n-gi>
        <n-card title="我的预测假设" size="small">
          <n-empty v-if="!myHypotheses.length" description="暂无保存的假设"/>
          <n-space vertical v-else>
            <n-card v-for="h in myHypotheses" :key="h.id" size="small">
              <div style="display: flex; justify-content: space-between; align-items: center;">
                <div>
                  <div style="font-weight: bold;">{{ h.name }}</div>
                  <n-text depth="3">{{ h.description }}</n-text>
                </div>
                <n-space>
                  <n-button size="small" @click="showChart(h)">查看曲线</n-button>
                  <n-tag :type="h.status === 'active' ? 'success' : 'default'">
                    {{ h.status === 'active' ? '监控中' : '已禁用' }}
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
            </n-card>
          </n-space>
        </n-card>
      </n-gi>
    </n-grid>

    <n-card title="净值曲线" size="small" style="margin-top: 12px;" v-if="activeHypothesis">
      <div ref="chartRef" style="width: 100%; height: 400px;"></div>
    </n-card>
  </n-card>
</template>
