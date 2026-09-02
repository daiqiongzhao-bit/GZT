import { createDiscreteApi } from 'naive-ui'
import { darkTheme, lightTheme } from 'naive-ui'

const KEY = 'gzt-theme'

export function getInitialTheme() {
  const saved = localStorage.getItem(KEY)
  return saved === 'dark' || saved === 'light' ? saved : 'light'
}

// 在挂载前调用，避免主题闪烁
export function applyInitialTheme() {
  document.documentElement.dataset.theme = getInitialTheme()
}

let app = null
export function bindApp(a) {
  app = a
  applyTheme(getInitialTheme())
}

// 运行时切换：更新 <html data-theme> + localStorage + naive-ui 离散组件主题
export function applyTheme(t) {
  document.documentElement.dataset.theme = t
  localStorage.setItem(KEY, t)
  if (!app) return
  const d = createDiscreteApi(['message', 'dialog', 'notification'], {
    configProviderProps: t === 'dark' ? { theme: darkTheme } : { theme: lightTheme }
  })
  app.config.globalProperties.$msg = d.message
  app.config.globalProperties.$dialog = d.dialog
  app.config.globalProperties.$notification = d.notification
}
