import axios from 'axios'

const http = axios.create({ baseURL: '/api', timeout: 15000 })

// 端类型判定：PWA standalone → pwa，普通浏览器标签 → web
function detectClientType() {
  try {
    const st = (window.matchMedia && window.matchMedia('(display-mode: standalone)').matches) || window.navigator.standalone
    return st ? 'pwa' : 'web'
  } catch (e) { return 'web' }
}

http.interceptors.request.use((cfg) => {
  const t = localStorage.getItem('sw_token')
  if (t) cfg.headers.Authorization = 'Bearer ' + t
  cfg.headers['X-Client-Type'] = detectClientType()
  return cfg
})

http.interceptors.response.use(
  (r) => r,
  (err) => {
    const status = err.response?.status
    if (status === 401) {
      localStorage.removeItem('sw_token')
      if (location.pathname !== '/login') {
        // ★ 走 router 而不是 location.href（v0.40.4）：
        //   location.href 是整页重载，会白屏闪一下、丢掉 SPA 状态，而且与 router 双轨。
        //   改用 router.replace 前必须**同时清空 Pinia 里的登录态**，
        //   否则守卫会把 /login 判成"已登录访问公开页"再弹回首页（导航被吃掉）。
        //   动态 import 是为了绕开 http → store/auth → api → http 的循环依赖。
        Promise.all([import('@/store/auth'), import('@/router')])
          .then(([{ useAuthStore }, { default: router }]) => {
            const a = useAuthStore()
            a.token = ''
            a.user = null
            a.wpAccess = null
            a.invalidatePerms()
            return router.replace('/login')
          })
          .catch(() => { location.href = '/login' }) // router 不可用时退回硬跳转
      }
    }
    return Promise.reject(err)
  }
)

export default http
