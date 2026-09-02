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
}

export default brand
