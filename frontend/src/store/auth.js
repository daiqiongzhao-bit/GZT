import { defineStore } from 'pinia'
import * as api from '@/api'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem('sw_token') || '',
    user: null
  }),
  getters: {
    isAuthed: (s) => !!s.token,
    isSuper: (s) => s.user?.role === 'super_admin',
    canManage: (s) => ['super_admin', 'dept_admin'].includes(s.user?.role),
    roleLabel: (s) => {
      const m = { super_admin: '超级管理员', dept_admin: '部门管理员', executor: '执行者' }
      return m[s.user?.role] || '—'
    }
  },
  actions: {
    async login(username, password) {
      const res = await api.post('/auth/login', { username, password })
      this.token = res.token
      this.user = res.user
      localStorage.setItem('sw_token', res.token)
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
    async logout() {
      // 通知后端使当前令牌失效（令 token_version 自增）
      try {
        await api.post('/logout')
      } catch { /* 后端不可达也照常本地登出 */ }
      this.token = ''
      this.user = null
      localStorage.removeItem('sw_token')
    }
  }
})
