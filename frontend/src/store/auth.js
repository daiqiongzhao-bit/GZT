import { defineStore } from 'pinia'
import * as api from '@/api'
// 直接用底层实例：登出通知要显式带旧令牌（见 logout() 注释），
// 而 @/api 的 post(url, data) 包装没有透传 config 的口子。
import http from '@/api/http'

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

// 权限快照缓存有效期。
//
// ★ 为什么必须有 TTL：管理员在「角色管理」里改完权限后，如果浏览器一直沿用
//   内存/sessionStorage 里的旧快照，就会出现现场这种"权限已经改了、页面还是老样子"
//   的错觉（实测：取消了「首页」权限，导航里仍显示概览，点进去后端记 403）。
//   折中：缓存只用于**首屏防闪**，5 分钟内有效；应用每次启动都会强制重取一次。
const PERM_CACHE_TTL = 5 * 60 * 1000

function readCache() {
  try {
    const raw = sessionStorage.getItem(PERM_CACHE_KEY)
    if (!raw) return null
    const o = JSON.parse(raw)
    if (!o || !Array.isArray(o.perms)) return null
    if (!o.at || Date.now() - o.at > PERM_CACHE_TTL) return null
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
    /**
     * 能否进入「企微推送」。
     *
     * ★ 主判据是 RBAC 的 wecompush:view —— 角色管理页勾上「企微推送 → 查看」即可见。
     *   历史实现要求"RBAC 权限 + 模块白名单"**同时**满足，导致按 RBAC 给自定义角色
     *   授权后导航依然不出现（现场原话："我给了企微推送权限，他反而没有"）。
     *   现在把模块白名单降级为**叠加的兼容项**（旧配置里给内置角色整体放行的场景），
     *   权限体系保持单一真源。
     */
    canWecom: (s) => {
      if (s.can('wecompush:view')) return true
      return !!s.wpAccess?.can_access
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
            role_keys: this.roleKeys, scope: this.permScope, enforce: this.permEnforce,
            at: Date.now() // 写入时间戳，供 readCache 判断新鲜度
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
    // 退出登录。
    //
    // ★★ 为什么"清本地态"必须发生在任何 await 之前（v0.40.4 修的真实缺陷）：
    //    调用方 onLogout() 的写法是 `auth.logout(); router.replace('/login')` ——
    //    replace 是同步发起的。历史实现把 `await api.post('/logout')` 放在最前面，
    //    于是 replace 触发导航时 this.token / this.user 还没清，
    //    路由守卫 router/index.js 里这条就会命中：
    //        if (to.meta.public && auth.isAuthed && auth.user) return { path: '/' }
    //    → 导航被改道回首页，用户会看到**主界面闪一下**，
    //    随后首页请求带着已失效令牌拿 401、被 http.js 兜底硬跳回 /login 才变成登录页。
    //    先同步清空 → 守卫读到 isAuthed=false → /login 正常渲染，全程无闪烁。
    logout() {
      const stale = this.token
      // ① 同步清空本地登录态（顺序不可调换）
      this.token = ''
      this.user = null
      this.wpAccess = null
      // 必须清权限缓存：同一浏览器切换账号时否则会沿用上一个账号的菜单
      this.invalidatePerms()
      localStorage.removeItem('sw_token')
      if (!stale) return Promise.resolve()
      // ② 再后台尽力通知后端令该令牌失效（不阻塞导航，失败也不影响本地登出）。
      //    显式带上旧令牌：localStorage 上面已清空，请求拦截器不会再自动附加，
      //    否则这次 /logout 退化成匿名请求、旧令牌实际并未失效（安全回归）。
      return http.post('/auth/logout', null, { headers: { Authorization: 'Bearer ' + stale } })
        .catch(() => { /* 后端不可达也照常本地登出 */ })
    }
  }
})
