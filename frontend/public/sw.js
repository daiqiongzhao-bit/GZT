// 排班工作台 Service Worker
// v3：应用壳缓存 + 关键数据离线缓存（班表/总览/我的信息），支持无网查看本人班表。
const CACHE_SHELL = 'swb-shell-v3'
const CACHE_DATA = 'swb-data-v1'
const SHELL = ['/', '/index.html', '/manifest.webmanifest', '/favicon.svg']
// 允许离线缓存的数据接口（按 URL 缓存，单人设备适用）
const DATA_APIS = ['/api/schedules', '/api/dashboard', '/api/auth/me']

self.addEventListener('install', (e) => {
  self.skipWaiting()
  e.waitUntil(caches.open(CACHE_SHELL).then((c) => c.addAll(SHELL)).catch(() => {}))
})

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys.filter((k) => k !== CACHE_SHELL && k !== CACHE_DATA).map((k) => caches.delete(k))
      )
    ).then(() => self.clients.claim())
  )
})

self.addEventListener('fetch', (e) => {
  const req = e.request
  if (req.method !== 'GET') return
  const url = new URL(req.url)
  if (url.origin !== self.location.origin) return

  // 数据接口：网络优先，成功即缓存；离线时回退缓存
  if (url.pathname.startsWith('/api/') && DATA_APIS.some((p) => url.pathname.startsWith(p))) {
    e.respondWith(
      fetch(req)
        .then((res) => {
          if (res.ok) {
            const copy = res.clone()
            caches.open(CACHE_DATA).then((c) => c.put(req, copy)).catch(() => {})
          }
          return res
        })
        .catch(() => caches.match(req).then((r) => r || fetch(req)))
    )
    return
  }
  if (url.pathname.startsWith('/api/')) return

  // 导航：网络优先，离线回退缓存壳
  if (req.mode === 'navigate') {
    e.respondWith(
      fetch(req).catch(() => caches.match('/index.html').then((r) => r || caches.match('/')))
    )
    return
  }

  // 静态资源：网络优先 + 更新缓存
  e.respondWith(
    fetch(req)
      .then((res) => {
        if (res.ok) {
          const copy = res.clone()
          caches.open(CACHE_SHELL).then((c) => c.put(req, copy)).catch(() => {})
        }
        return res
      })
      .catch(() => caches.match(req))
  )
})
