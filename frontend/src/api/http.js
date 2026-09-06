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
      if (location.pathname !== '/login') location.href = '/login'
    }
    return Promise.reject(err)
  }
)

export default http
