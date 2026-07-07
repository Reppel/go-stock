<script setup>
import {h, computed, nextTick, onBeforeUnmount, onMounted, ref} from 'vue';
import {GetConfig, GetVersionInfo, GetUserManual, OpenURL} from "../../wailsjs/go/main/App";
import {EventsOff, Environment} from "../../wailsjs/runtime";
import {NAvatar, NButton, NTree, useNotification} from "naive-ui";

const updateLog = ref('');
const versionInfo = ref('');
const icon = ref('');
const notify = useNotification()
const showManual = ref(false)
const manualContent = ref('')
const manualId = 'manual-preview'
const darkTheme = ref(false)
const theme = computed(() => darkTheme.value ? 'dark' : 'light')
const manualScrollRef = ref(null)
const catalogList = ref([])

const buildCatalogTree = (headings) => {
  if (!headings.length) return []
  const roots = []
  const stack = []
  for (const h of headings) {
    const node = { key: h.text, label: h.text, level: h.level, children: [] }
    while (stack.length && stack[stack.length - 1].level >= h.level) {
      stack.pop()
    }
    if (stack.length) {
      stack[stack.length - 1].children.push(node)
    } else {
      roots.push(node)
    }
    stack.push(node)
  }
  const clean = (nodes) => {
    for (const n of nodes) {
      if (n.children.length === 0) delete n.children
      else clean(n.children)
    }
  }
  clean(roots)
  return roots
}

const catalogTree = computed(() => buildCatalogTree(catalogList.value))

const onTreeSelect = (keys) => {
  if (keys.length) scrollToHeading(keys[0])
}

const slugifyHeading = (text) => {
  return text
    .trim()
    .replace(/[^\w一-鿿]/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '')
}

const extractCatalog = () => {
  if (!manualScrollRef.value) return
  const headings = manualScrollRef.value.querySelectorAll('h1, h2, h3, h4, h5, h6')
  catalogList.value = Array.from(headings).map(h => ({
    text: h.textContent?.trim() || '',
    level: parseInt(h.tagName.slice(1))
  }))
}

const scrollToHeading = (headingText) => {
  if (!manualScrollRef.value) return
  const container = manualScrollRef.value
  const headings = container.querySelectorAll('h1, h2, h3, h4, h5, h6')
  for (const h of headings) {
    const text = h.textContent?.trim()
    if (text === headingText) {
      const containerRect = container.getBoundingClientRect()
      const headingRect = h.getBoundingClientRect()
      container.scrollTop += headingRect.top - containerRect.top - 10
      return
    }
  }
}

const openManual = () => {
  if (!manualContent.value) {
    GetUserManual().then(res => {
      manualContent.value = res
      showManual.value = true
      nextTick(() => { setTimeout(extractCatalog, 500) })
    })
  } else {
    showManual.value = true
    nextTick(() => { setTimeout(extractCatalog, 300) })
  }
}

onMounted(() => {
  document.title = '关于软件';
  GetConfig().then(res => {
    darkTheme.value = res.darkTheme
  })
  GetVersionInfo().then((res) => {
    updateLog.value = res.content;
    versionInfo.value = res.version;
    icon.value = res.icon;
  });
})
onBeforeUnmount(() => {
  notify.destroyAll()
  // EventsOff("updateVersion")
  // EventsOff("updateNeedAdmin")
})

</script>

<template>
  <n-space vertical size="large"  style="--wails-draggable:no-drag">
    <n-card size="large">
      <n-divider title-placement="center">关于软件</n-divider>
      <n-space vertical >
        <n-image width="100" :src="icon" />
        <h1>
          <n-badge :value="versionInfo" :offset="[80,10]"  type="success">
            <n-gradient-text type="info" :size="50" >China StockMind AI</n-gradient-text>
          </n-badge>
        </h1>
        <n-flex justify="center">
          <n-button size="tiny" @click="openManual" type="success" tertiary >查看用户手册</n-button>
        </n-flex>
        <p v-if="updateLog" style="text-align: center">更新说明：{{updateLog}}</p>
      </n-space>

      <n-modal
        v-model:show="showManual"
        preset="card"
        title="用户手册"
        style="width: 90vw; max-height: 90vh"
        :bordered="false"
        :segmented="{ content: true, footer: true }"
      >
        <div style="display: flex; max-height: 75vh;">
          <div v-if="catalogList.length" class="manual-catalog" style="width: 240px; min-width: 240px; border-right: 1px solid var(--n-border-color); padding: 8px 4px; overflow-y: auto;">
            <div style="font-weight: bold; margin-bottom: 8px; padding: 0 8px;">目录</div>
            <n-tree
              :data="catalogTree"
              :block-line="true"
              :block-node="true"
              :selectable="true"
              :cancelable="false"
              default-expand-all
              key-field="key"
              label-field="label"
              children-field="children"
              @update:selected-keys="onTreeSelect"
            />
          </div>
          <div ref="manualScrollRef" style="flex: 1; overflow-y: auto; padding: 0 16px;">
            <MdPreview style="text-align: left;" :id="manualId" v-model="manualContent" :theme="theme" :preview-theme="'github'" :md-heading-id="slugifyHeading" @onHtmlChanged="extractCatalog" />
          </div>
        </div>
      </n-modal>
    </n-card>
  </n-space>
</template>

<style scoped>
h1, h2 {
  margin: 0;
  padding: 6px 0;
}

p {
  margin: 2px 0;
}

ul {
  list-style-type: disc;
  padding-left: 20px;
}

a {
  color: #18a058;
  text-decoration: none;
}

a:hover {
  text-decoration: underline;
}

.manual-catalog > div:hover {
  color: #18a058;
}

.manual-catalog :deep(.n-tree-node-content) {
  text-align: left;
  justify-content: flex-start;
}

.manual-catalog :deep(.n-tree-node) {
  text-align: left;
}
</style>

