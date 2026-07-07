import {createApp} from 'vue'
import naive from 'naive-ui'
import App from './App.vue'
import router from './router/router'
// 引入组件库的少量全局样式变量
import 'tdesign-vue-next/es/style/index.css';

const app = createApp(App)

function isResizeObserverError(err) {
  if (!err) return false
  const msg = err.message || err.toString() || ''
  return msg.includes('ResizeObserver')
}

app.config.errorHandler = (err) => {
  if (isResizeObserverError(err)) {
    return
  }
  console.error(err)
}

window.addEventListener('error', (event) => {
  if (event.message && event.message.includes('ResizeObserver')) {
    event.preventDefault()
    event.stopPropagation()
    return true
  }
}, true)

window.addEventListener('unhandledrejection', (event) => {
  if (event.reason && isResizeObserverError(event.reason)) {
    event.preventDefault()
    event.stopPropagation()
    return true
  }
}, true)

// 拦截全局 console.error，避免 ResizeObserver 错误被展示到 UI 上
const originalConsoleError = console.error
console.error = function (...args) {
  if (args.length > 0 && typeof args[0] === 'string' && args[0].includes('ResizeObserver')) {
    return
  }
  if (args.length > 0 && args[0] instanceof Error && isResizeObserverError(args[0])) {
    return
  }
  return originalConsoleError.apply(this, args)
}

app.use(router)
app.use(naive)
app.mount('#app')