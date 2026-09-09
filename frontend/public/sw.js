// 排班工作台 Service Worker
// v3：应用壳缓存 + 关键数据离线缓存（班表/总览/我的信息），支持无网查看本人班表。
//
// __APP_VERSION__ 会在构建时替换为当前版本号（见 Dockerfile / build.sh），
// 使每次发版 SW 内容都发生变化 → 浏览器检测到更新 → 重新激活并清理旧缓存，
// 避免 PWA 长期停留在旧应用壳（表现为「点了菜单没反应」）。
const BUILD_ID = '__APP_VERSION__'
const CACHE_SHELL = 'swb-shell-' + BUILD_ID
const CACHE_DATA = 'swb-data-' + BUILD_ID
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

// ---------- Web Push 推送展示（v0.13.0，需 HTTPS）----------
self.addEventListener('push', (e) => {
  let data = { title: '企业任务通知', body: '', url: '/' }
  try {
    if (e.data) data = Object.assign(data, e.data.json())
  } catch (err) { /* 非 JSON 负载忽略 */ }
  const opts = { body: data.body || '', data: { url: data.url || '/' }, badge: '/favicon.svg', icon: '/favicon.svg' }
  e.waitUntil(self.registration.showNotification(data.title || '企业任务通知', opts))
})

self.addEventListener('notificationclick', (e) => {
  e.notification.close()
  const url = (e.notification.data && e.notification.data.url) || '/'
  e.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((list) => {
      for (const c of list) {
        if ('focus' in c) { c.focus(); if ('navigate' in c) c.navigate(url); return }
      }
      return self.clients.openWindow(url)
    })
  )
})
