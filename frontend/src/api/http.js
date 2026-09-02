import axios from 'axios'

const http = axios.create({ baseURL: '/api', timeout: 15000 })

http.interceptors.request.use((cfg) => {
  const t = localStorage.getItem('sw_token')
  if (t) cfg.headers.Authorization = 'Bearer ' + t
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
