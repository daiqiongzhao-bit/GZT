import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import './styles/global.css'
import { applyInitialTheme, bindApp, getInitialTheme } from './theme'
import { darkTheme, lightTheme } from 'naive-ui'
import { createDiscreteApi } from 'naive-ui'

// 先应用主题，避免首屏闪烁
applyInitialTheme()

const app = createApp(App)

const discrete = createDiscreteApi(['message', 'dialog', 'notification'], {
  configProviderProps: getInitialTheme() === 'dark' ? { theme: darkTheme } : { theme: lightTheme }
})
// 全局可用的消息/对话框实例
app.config.globalProperties.$msg = discrete.message
app.config.globalProperties.$dialog = discrete.dialog
app.config.globalProperties.$notification = discrete.notification

app.use(createPinia())
app.use(router)
app.mount('#app')
bindApp(app)

// PWA：生产构建下注册 Service Worker
if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {})
  })
}
