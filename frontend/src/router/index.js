import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/store/auth'

// ============================================================================
// meta.perm：进入该路由所需的功能权限（与后端 routeperm.go 的 perms 逐字符一致）
//   - 字符串：需要该权限
//   - 数组：任一命中即可
//   - 省略/空：不校验（登录即可）
//
// ★ 为什么用「静态路由 + 守卫校验」而不是 router.addRoute 动态注册：
//   GZT 的导航是扁平的、页面数量固定，动态注册只能带来 3 个风险
//   （登出漏 removeRoute 导致串菜单、刷新竞态白屏、构建期无法静态分析），
//   却没有收益。守卫校验同样达成"没权限进不去"，且后端 GuardByPath 才是真正的边界。
// ============================================================================

const routes = [
  { path: '/login', name: 'login', component: () => import('@/views/Login.vue'), meta: { public: true } },
  { path: '/', name: 'dashboard', component: () => import('@/views/Dashboard.vue'), meta: { title: '概览', perm: 'dashboard:view' } },
  { path: '/schedule', name: 'schedule', component: () => import('@/views/Schedule.vue'), meta: { title: '班表', perm: 'schedule:view' } },
  { path: '/planner', redirect: '/schedule' },
  { path: '/tasks', name: 'tasks', component: () => import('@/views/Tasks.vue'), meta: { title: '任务', perm: 'task:view' } },
  { path: '/workspace', name: 'workspace', component: () => import('@/views/Workspace.vue'), meta: { title: '知识库', perm: 'knowledge:view' } },
  { path: '/backup', name: 'backup', component: () => import('@/views/Backup.vue'), meta: { title: '备份还原', perm: 'system:backup:list' } },
  { path: '/wecom-push', name: 'wecom-push', component: () => import('@/views/WecomPush.vue'), meta: { title: '企微推送', wecom: true, perm: 'wecompush:view' } },
  { path: '/settings', name: 'settings', component: () => import('@/views/Settings.vue'), meta: { title: '设置' } },

  // ---- v0.39.1 系统管理：四个子模块已并入「设置」页的「系统管理」分组 ----
  // 侧栏不再单独出现；旧地址保留重定向，书签 / 外链 / 深链仍然可用。
  // 注意：redirect 在路由解析阶段完成，beforeEach 的 perm 校验不会作用到它，
  //       因此准入由设置页的 sysTabs 过滤兜底（无权时自动退回「个人信息」），
  //       真正的边界依旧是后端 GuardByPath。
  { path: '/system/users', redirect: { path: '/settings', query: { tab: 'sysuser' } } },
  { path: '/system/roles', redirect: { path: '/settings', query: { tab: 'sysrole' } } },
  { path: '/system/menus', redirect: { path: '/settings', query: { tab: 'sysmenu' } } },
  { path: '/system/depts', redirect: { path: '/settings', query: { tab: 'sysdept' } } },

  { path: '/:pathMatch(.*)*', redirect: '/' }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// 判定 meta.perm（支持字符串 / 数组）
function permOk(auth, perm) {
  if (!perm) return true
  if (Array.isArray(perm)) return perm.some((p) => auth.can(p))
  return auth.can(perm)
}

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (!auth.isAuthed && !to.meta.public) {
    return { path: '/login' }
  }
  if (auth.isAuthed && !auth.user) {
    await auth.fetchMe()
    // ★ 强制刷新（force=true），不沿用 sessionStorage 里的旧快照。
    //   历史实现这里传的是默认的 force=false，而 permLoaded 已被缓存置为 true，
    //   于是"权限明明改了、刷新页面也不生效"（现场：取消了首页权限仍显示概览）。
    //   刷新页面 = 一次新的会话状态，应当以服务端为准。
    await Promise.all([auth.fetchPerms(true), auth.fetchWpAccess()])
  }
  if (to.meta.public && auth.isAuthed && auth.user) {
    return { path: '/' }
  }
  if (!to.meta.public && !auth.user && auth.isAuthed) {
    return { path: '/login' }
  }
  // 企微推送：白名单角色 + RBAC 权限双重判定
  if (to.meta.wecom) {
    if (!auth.permLoaded) await auth.fetchPerms()
    if (!auth.wpAccess) await auth.fetchWpAccess()
    if (!auth.canWecom) return { path: '/' }
  }
  // ★ 功能权限准入。注意 '/' 自身带 perm，若用户连 dashboard:view 都没有，
  //   再重定向到 '/' 会形成死循环 —— 因此首页永远放行，由页面内部呈现"无可访问模块"。
  if (!permOk(auth, to.meta.perm)) {
    if (to.path === '/') return true
    return { path: '/' }
  }
  return true
})

export default router
