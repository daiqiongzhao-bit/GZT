import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/store/auth'

const routes = [
  { path: '/login', name: 'login', component: () => import('@/views/Login.vue'), meta: { public: true } },
  { path: '/', name: 'dashboard', component: () => import('@/views/Dashboard.vue'), meta: { title: '概览' } },
  { path: '/schedule', name: 'schedule', component: () => import('@/views/Schedule.vue'), meta: { title: '班表' } },
  { path: '/planner', redirect: '/schedule' },
  { path: '/tasks', name: 'tasks', component: () => import('@/views/Tasks.vue'), meta: { title: '任务' } },
  { path: '/workspace', name: 'workspace', component: () => import('@/views/Workspace.vue'), meta: { title: '知识库' } },
  { path: '/backup', name: 'backup', component: () => import('@/views/Backup.vue'), meta: { title: '备份还原' } },
  { path: '/wecom-push', name: 'wecom-push', component: () => import('@/views/WecomPush.vue'), meta: { title: '企微推送', wecom: true } },
  { path: '/settings', name: 'settings', component: () => import('@/views/Settings.vue'), meta: { title: '设置' } },
  { path: '/:pathMatch(.*)*', redirect: '/' }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (!auth.isAuthed && !to.meta.public) {
    return { path: '/login' }
  }
  if (auth.isAuthed && !auth.user) {
    await auth.fetchMe()
    // 登录后同步一次企微推送的访问权限（决定导航项与路由准入）
    await auth.fetchWpAccess()
  }
  if (to.meta.public && auth.isAuthed && auth.user) {
    return { path: '/' }
  }
  if (!to.meta.public && !auth.user && auth.isAuthed) {
    return { path: '/login' }
  }
  // 企微推送：仅白名单角色可进入（后端另有 403 闸门做真正的安全边界）
  if (to.meta.wecom) {
    if (!auth.wpAccess) await auth.fetchWpAccess()
    if (!auth.canWecom) return { path: '/' }
  }
  return true
})

export default router
