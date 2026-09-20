import { defineStore } from 'pinia'
import * as api from '@/api'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem('sw_token') || '',
    user: null,
    // 企微推送模块的访问权限（由 /wecom-push/access 返回）。
    // null = 尚未获取；获取失败时按「无权限」处理（失败即收紧）。
    wpAccess: null
  }),
  getters: {
    isAuthed: (s) => !!s.token,
    isSuper: (s) => s.user?.role === 'super_admin',
    canManage: (s) => ['super_admin', 'dept_admin'].includes(s.user?.role),
    // 能否进入「企微推送」：优先用后端下发的白名单判定；
    // 还没拿到配置时回落到「管理员可见」，避免管理员刷新瞬间看不到入口。
    canWecom: (s) => {
      if (s.wpAccess) return !!s.wpAccess.can_access
      return ['super_admin', 'dept_admin'].includes(s.user?.role)
    },
    // 能否修改「企微推送」的角色权限（仅超管）
    canConfigWecom: (s) => !!s.wpAccess?.can_config,
    roleLabel: (s) => {
      const m = { super_admin: '超级管理员', dept_admin: '部门管理员', executor: '执行者' }
      return m[s.user?.role] || '—'
    }
  },
  actions: {
    async login(username, password) {
      // v0.0.6：登录显式声明端类型，便于日志/会话区分来源
      const ct = (window.matchMedia && window.matchMedia('(display-mode: standalone)').matches) || window.navigator.standalone ? 'pwa' : 'web'
      const res = await api.post('/auth/login', { username, password, client_type: ct })
      this.token = res.token
      this.user = res.user
      localStorage.setItem('sw_token', res.token)
      await this.fetchWpAccess()
      return this.user
    },
    async fetchMe() {
      if (!this.token) return null
      try {
        this.user = await api.get('/auth/me')
      } catch {
        this.token = ''
        this.user = null
        localStorage.removeItem('sw_token')
      }
      return this.user
    },
    // 拉取企微推送的访问权限（该接口对任何登录用户开放，只回角色名单）
    async fetchWpAccess() {
      if (!this.token) { this.wpAccess = null; return null }
      try {
        this.wpAccess = await api.get('/wecom-push/access')
      } catch {
        this.wpAccess = { can_access: false, can_config: false, allowed_roles: [], roles: [] }
      }
      return this.wpAccess
    },
    async logout() {
      // 通知后端使当前令牌失效（令 token_version 自增）
      try {
        await api.post('/logout')
      } catch { /* 后端不可达也照常本地登出 */ }
      this.token = ''
      this.user = null
      this.wpAccess = null
      localStorage.removeItem('sw_token')
    }
  }
})
