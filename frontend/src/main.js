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
    navigator.serviceWorker
      .register('/sw.js')
      .then((reg) => {
        // 检测到新版本 SW：若当前页面已被 SW 接管，提示用户刷新，
        // 否则用户会一直停留在旧应用壳（表现为点了菜单没反应）
        reg.addEventListener('updatefound', () => {
          const sw = reg.installing || reg.waiting
          if (!sw) return
          sw.addEventListener('statechange', () => {
            // 'installed'/'activated' 都算就绪：sw.js 里用了 skipWaiting，
            // 状态可能直接从 installing 跳到 activated，只判 'installed' 会漏。
            if ((sw.state === 'installed' || sw.state === 'activated') && navigator.serviceWorker.controller) {
              window.dispatchEvent(new CustomEvent('sw-updated'))
            }
          })
        })
      })
      .catch(() => {})
  })
}

// v0.9.2 PWA 安装提示：浏览器默认只在用户访问 30s+ 且有交互后才自动弹安装横幅，
// 多数情况下用户等不到。本地部署后地址栏虽有 [⬇] 图标但应用内看不到引导，
// 这里拦截 beforeinstallprompt 缓存事件，由 App.vue 的"安装到桌面"按钮调用。
// 注意：localhost / HTTPS 下 Chrome 才允许 prompt()，且站点必须满足 PWA 安装条件
//（manifest + 192/512 icon + SW + 用户未安装）。开发模式（import.meta.env.PROD=false）
// 也能捕获事件用于调试展示按钮态，但 .prompt() 在非安全上下文会被浏览器拒绝。
window.__pwaInstallEvent = null
window.addEventListener('beforeinstallprompt', (e) => {
  // 阻止 Chrome 默认横幅（30s 后才弹，且只在地址栏），统一由应用内按钮触发
  e.preventDefault()
  window.__pwaInstallEvent = e
  window.dispatchEvent(new CustomEvent('pwa-installable'))
})
window.addEventListener('appinstalled', () => {
  window.__pwaInstallEvent = null
  window.dispatchEvent(new CustomEvent('pwa-installed'))
})
