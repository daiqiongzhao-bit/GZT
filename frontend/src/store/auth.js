import { defineStore } from 'pinia'
import * as api from '@/api'

// ============================================================================
// v0.39.0 RBAC：前端权限态
//
// 三条原则：
//  1. 前端权限只负责「体验」（菜单显隐 / 路由准入 / 按钮显隐），
//     真正的安全边界永远在后端 GuardByPath + 归属断言。
//  2. 权限集合来自 /system/perm/my（登录后取一次，缓存到内存 + sessionStorage），
//     含 wildcard（超管通配 *:*:*）与数据范围快照（仅供展示，不参与判定）。
//  3. ★ 取不到权限时**不拦截**（permLoaded=false → can() 恒 true）：
//     宁可让后端 403 报错，也不要在前端把所有人锁在门外（尤其防重定向死循环）。
// ============================================================================

const PERM_CACHE_KEY = 'sw_perms'

function readCache() {
  try {
    const raw = sessionStorage.getItem(PERM_CACHE_KEY)
    if (!raw) return null
    const o = JSON.parse(raw)
    if (!o || !Array.isArray(o.perms)) return null
    return o
  } catch (e) {
    return null
  }
}

export const useAuthStore = defineStore('auth', {
  state: () => {
    const c = readCache()
    return {
      token: localStorage.getItem('sw_token') || '',
      user: null,
      // 企微推送模块的访问权限（由 /wecom-push/access 返回）。
      // null = 尚未获取；获取失败时按「无权限」处理（失败即收紧）。
      wpAccess: null,
      // —— RBAC：功能权限集合（perms 为字符串数组，wildcard 表示超管通配）——
      perms: c ? c.perms : [],
      wildcard: c ? !!c.wildcard : false,
      roleKeys: c ? c.role_keys || [] : [],
      permScope: c ? c.scope || null : null,
      permEnforce: c ? c.enforce || '' : '', // off / log / on（后端当前拦截模式，仅供展示）
      permLoaded: !!c // false 时 can() 一律放行（见文件头第 3 条）
    }
  },
  getters: {
    isAuthed: (s) => !!s.token,
    // 超管判定：优先用 RBAC 通配（最准），回落旧的 users.role 字段
    isSuper: (s) => s.wildcard || s.roleKeys.includes('super_admin') || s.user?.role === 'super_admin',
    canManage: (s) => ['super_admin', 'dept_admin'].includes(s.user?.role),
    /**
     * 功能权限判定：can('system:user:add')。
     * - perm 为空 → 放行（该路由不要求权限）
     * - 权限尚未加载 → 放行（后端闸门负责真正的拒绝）
     * - 超管通配 → 放行
     */
    can: (s) => (perm) => {
      if (!perm) return true
      if (!s.permLoaded) return true
      if (s.wildcard) return true
      return s.perms.includes(perm)
    },
    /** 任一命中即可（用于"或"语义的入口，如设置页的多种可读权限） */
    canAny: (s) => (list) => {
      const arr = Array.isArray(list) ? list : [list]
      return arr.some((p) => s.can(p))
    },
    // 能否进入「企微推送」：RBAC 权限 + 后端下发的白名单双重判定
    canWecom: (s) => {
      if (!s.can('wecompush:view')) return false
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
      this.invalidatePerms()
      await Promise.all([this.fetchPerms(), this.fetchWpAccess()])
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
    /**
     * 拉取当前用户的功能权限快照。
     * 失败时**不动 permLoaded**：保持上次的成功结果；若从未成功过则保持 false（= 不拦截）。
     */
    async fetchPerms(force = false) {
      if (!this.token) {
        this.invalidatePerms()
        return null
      }
      if (this.permLoaded && !force) return this.perms
      try {
        const r = await api.get('/system/perm/my')
        this.perms = Array.isArray(r.perms) ? r.perms : []
        this.wildcard = !!r.wildcard
        this.roleKeys = Array.isArray(r.role_keys) ? r.role_keys : []
        this.permScope = r.scope || null
        this.permEnforce = r.enforce || ''
        this.permLoaded = true
        try {
          sessionStorage.setItem(PERM_CACHE_KEY, JSON.stringify({
            perms: this.perms, wildcard: this.wildcard,
            role_keys: this.roleKeys, scope: this.permScope, enforce: this.permEnforce
          }))
        } catch (e) { /* 隐私模式下 sessionStorage 可能不可写 */ }
      } catch (e) {
        // 后端不可达 / 尚未部署该接口（404）→ 不覆盖已有结果，保持宽松
        this.permLoaded = false
      }
      return this.perms
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
    /** 清空权限态（登出 / 切换账号时必须调用，否则会串菜单） */
    invalidatePerms() {
      this.perms = []
      this.wildcard = false
      this.roleKeys = []
      this.permScope = null
      this.permEnforce = ''
      this.permLoaded = false
      try { sessionStorage.removeItem(PERM_CACHE_KEY) } catch (e) { /* ignore */ }
    },
    async logout() {
      // 通知后端使当前令牌失效（令 token_version 自增）
      try {
        await api.post('/logout')
      } catch { /* 后端不可达也照常本地登出 */ }
      this.token = ''
      this.user = null
      this.wpAccess = null
      // ★ 必须清权限缓存：同一浏览器切换账号时否则会沿用上一个账号的菜单
      this.invalidatePerms()
      localStorage.removeItem('sw_token')
    }
  }
})
