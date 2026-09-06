// 全局品牌信息（响应式）：App 侧边栏 / 登录页 / 设置页共用，保存后即时生效
import { reactive } from 'vue'
import http from './api/http'

export const brand = reactive({
  company_name: '',
  slogan: '',
  copyright: '',
  version: '',
  timezone: 'Asia/Shanghai',
  logo: '' // 企业 Logo 文件名，空表示使用内置图标
})

export async function loadBrand() {
  try {
    const s = await http.get('/settings')
    Object.assign(brand, s.data || s)
  } catch { /* 未登录或接口不可用时忽略 */ }
  applyFavicon(brand.logo) // 浏览器标签 / 手机桌面图标跟随企业 Logo
}

// 把浏览器标签 favicon + 手机桌面图标切换到上传的企业 Logo（PNG）。
// 未上传时回退到内置 /favicon.svg。
export function applyFavicon(logoName) {
  const url = logoName ? '/api/settings/logo?v=' + encodeURIComponent(logoName) : '/favicon.svg'
  const type = logoName ? 'image/png' : 'image/svg+xml'
  const icon = document.getElementById('app-icon')
  if (icon) { icon.setAttribute('href', url); icon.setAttribute('type', type) }
  const touch = document.getElementById('app-icon-touch')
  if (touch) touch.setAttribute('href', url)
}

export default brand
