import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/store/auth'

const routes = [
  { path: '/login', name: 'login', component: () => import('@/views/Login.vue'), meta: { public: true } },
  { path: '/', name: 'dashboard', component: () => import('@/views/Dashboard.vue'), meta: { title: '概览' } },
  { path: '/schedule', name: 'schedule', component: () => import('@/views/Schedule.vue'), meta: { title: '班表' } },
  { path: '/tasks', name: 'tasks', component: () => import('@/views/Tasks.vue'), meta: { title: '任务' } },
  { path: '/backup', name: 'backup', component: () => import('@/views/Backup.vue'), meta: { title: '备份还原' } },
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
  }
  if (to.meta.public && auth.isAuthed && auth.user) {
    return { path: '/' }
  }
  if (!to.meta.public && !auth.user && auth.isAuthed) {
    return { path: '/login' }
  }
  return true
})

export default router
