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

  // ---- v0.39.0 RBAC 系统管理（四个子模块）----
  { path: '/system/users', name: 'system-users', component: () => import('@/views/SystemUser.vue'), meta: { title: '用户管理', perm: 'system:user:list' } },
  { path: '/system/roles', name: 'system-roles', component: () => import('@/views/SystemRole.vue'), meta: { title: '角色管理', perm: 'system:role:list' } },
  { path: '/system/menus', name: 'system-menus', component: () => import('@/views/SystemMenu.vue'), meta: { title: '菜单管理', perm: 'system:menu:list' } },
  { path: '/system/depts', name: 'system-depts', component: () => import('@/views/SystemDept.vue'), meta: { title: '部门管理', perm: 'system:dept:list' } },

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
    // 登录后同步一次权限快照与企微推送白名单（决定导航项与路由准入）
    await Promise.all([auth.fetchPerms(), auth.fetchWpAccess()])
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
