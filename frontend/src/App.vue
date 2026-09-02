<template>
  <div class="app-bg">
    <div class="blob b1"></div>
    <div class="blob b2"></div>
    <div class="blob b3"></div>

    <div v-if="!ready" class="boot">
      <div class="boot-spin"></div>
    </div>

    <div v-else-if="auth.user" class="shell">
      <!-- 侧边栏（桌面） -->
      <aside class="sidebar glass">
        <div class="brand">
          <div class="logo">
            <img v-if="brand.logo" :src="logoUrl" :alt="company" />
            <span v-else v-html="brandLogo"></span>
          </div>
          <div class="brand-text">
            <div class="brand-name">{{ company }}</div>
            <div class="brand-slogan">{{ slogan }}</div>
          </div>
        </div>

        <nav class="nav">
          <router-link v-for="n in navItems" :key="n.to" :to="n.to" class="nav-item" active-class="active">
            <span class="nav-ico" v-html="n.icon"></span>
            <span>{{ n.label }}</span>
            <span v-if="n.to === '/tasks' && badgeTotal > 0" class="nav-badge" :class="{ danger: badgeOverdue > 0 }">{{ badgeTotal }}</span>
          </router-link>
        </nav>

        <a class="ext-dl" href="/extension.zip" download title="下载浏览器插件">
          <span class="ext-ico" v-html="puzzleIcon"></span>
          <span>下载插件</span>
        </a>

        <div class="side-foot">
          <div class="user-mini">
            <div class="avatar">{{ userInitial }}</div>
            <div>
              <div class="uname">{{ auth.user.name }}</div>
              <div class="urole">{{ auth.roleLabel }}</div>
            </div>
          </div>
          <button class="logout" @click="onLogout">退出登录</button>
        </div>
      </aside>

      <main class="main">
        <header class="topbar glass">
          <div class="top-title">{{ pageTitle }}</div>
          <div class="today">
            <span>{{ todayText }}</span>
            <button class="theme-toggle bell-wrap" :title="notifUnread > 0 ? '有 ' + notifUnread + ' 条未读通知' : '站内通知'" @click="openNotifs">
              <span class="bell-ico" v-html="bellIcon"></span>
              <span v-if="notifUnread > 0" class="bell-badge">{{ notifUnread > 99 ? '99+' : notifUnread }}</span>
            </button>
            <button class="theme-toggle" :title="theme === 'dark' ? '切换为浅色' : '切换为深色'" @click="toggleTheme" v-html="themeIcon"></button>
          </div>
        </header>

        <div class="mobile-topbar">
          <div class="top-title">{{ pageTitle }}</div>
          <div class="mobile-actions">
            <button class="m-icon bell-wrap" @click="openNotifs" :title="'站内通知'">
              <span class="bell-ico" v-html="bellIcon"></span>
              <span v-if="notifUnread > 0" class="bell-badge">{{ notifUnread > 99 ? '99+' : notifUnread }}</span>
            </button>
            <button class="m-icon" @click="toggleTheme" :title="theme === 'dark' ? '切换为浅色' : '切换为深色'" v-html="themeIcon"></button>
            <router-link to="/settings" class="m-icon" title="设置" v-html="icons.settings"></router-link>
            <button class="m-icon m-logout" @click="onLogout" title="退出登录" v-html="logoutIcon"></button>
          </div>
        </div>

        <section class="content">
          <router-view v-slot="{ Component }">
            <transition name="fade" mode="out-in">
              <component :is="Component" />
            </transition>
          </router-view>
        </section>
      </main>

      <!-- 底部悬浮导航（移动端） -->
      <nav class="mobile-nav">
        <router-link v-for="n in navItems" :key="n.to" :to="n.to" active-class="active">
          <span v-html="n.icon"></span>
          <span>{{ n.label }}</span>
          <span v-if="n.to === '/tasks' && badgeTotal > 0" class="nav-badge" :class="{ danger: badgeOverdue > 0 }">{{ badgeTotal }}</span>
        </router-link>
      </nav>

      <!-- 站内通知抽屉 -->
      <transition name="drawer">
        <div v-if="notifOpen" class="notif-mask" @click.self="notifOpen = false">
          <div class="notif-panel">
            <div class="notif-head">
              <span class="notif-title">站内通知<em v-if="notifUnread > 0" class="notif-num">{{ notifUnread }}</em></span>
              <div class="notif-head-actions">
                <button class="btn ghost sm" :disabled="!notifUnread" @click="readAll">全部已读</button>
                <button class="notif-close" @click="notifOpen = false" title="关闭">×</button>
              </div>
            </div>
            <div class="notif-list">
              <div v-for="n in notifs" :key="n.id" class="notif-item" :class="{ unread: !n.read }" @click="markRead(n)">
                <div class="ni-title">{{ n.title }}</div>
                <div class="ni-content">{{ n.content }}</div>
                <div class="ni-time">{{ fmtNotif(n.created_at) }}</div>
              </div>
              <div v-if="!notifs.length" class="notif-empty">暂无通知</div>
            </div>
          </div>
        </div>
      </transition>
    </div>

    <router-view v-else />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/store/auth'
import { navItems, icons } from '@/icons'
import { brand, loadBrand } from '@/brand'
import { applyTheme } from '@/theme'
import { get } from '@/api'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const ready = ref(false)

// 导航栏任务角标：现在就该处理的任务数（打开即见，常驻刷新）
const badgeTotal = ref(0)
const badgeOverdue = ref(0)
let badgeTimer = null
async function loadBadge() {
  try {
    const r = await get('/tasks/counts')
    // 用后端去重后的 due_total（逾期 ∪ 今日到期），不能 overdue + today 相加：
    // 逾期任务同时属于今日任务，相加会翻倍（2 条任务显示成 4）
    badgeTotal.value = r.due_total ?? ((r.overdue || 0) + (r.today || 0))
    badgeOverdue.value = r.overdue || 0
  } catch { /* 忽略网络失败 */ }
}

const theme = ref(document.documentElement.dataset.theme || 'light')
const sunIcon = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>'
const moonIcon = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.8A9 9 0 1 1 11.2 3 7 7 0 0 0 21 12.8z"/></svg>'
const themeIcon = computed(() => (theme.value === 'dark' ? sunIcon : moonIcon))
function toggleTheme() {
  theme.value = theme.value === 'dark' ? 'light' : 'dark'
  applyTheme(theme.value)
}

const company = computed(() => brand.company_name || '企业排班任务工作台')
const slogan = computed(() => brand.slogan || '三端同步 · 安全可控 · 无限扩展')
const companyInitial = computed(() => (company.value || '工')[0])

const userInitial = computed(() => (auth.user?.name || '?')[0])
const pageTitle = computed(() => route.meta.title || '工作台')
// 上传了企业 Logo 时用图片，否则用内置图标；?v=文件名 保证换图后浏览器立即刷新
const logoUrl = computed(() => '/api/settings/logo?v=' + encodeURIComponent(brand.logo || ''))
const brandLogo = '<svg viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="17" rx="3"/><path d="M3 9h18M8 2v4M16 2v4"/><path d="M8.5 14.5l2.2 2.2 4.3-4.4" stroke-width="2.4"/></svg>'
const logoutIcon = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="M16 17l5-5-5-5"/><path d="M21 12H9"/></svg>'
const puzzleIcon = '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3v3m0 0a2 2 0 1 0 0 4 2 2 0 0 0 0-4zm0 4v3m-6 4h3m0 0a2 2 0 1 0 4 0 2 2 0 0 0-4 0zm4 0h3m-3-6V6a2 2 0 1 1 4 0v3m0 0a2 2 0 1 1 0 4 2 2 0 0 1 0-4zm0 4v3a2 2 0 1 1-4 0"/></svg>'
const todayText = computed(() =>
  new Date().toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric', weekday: 'long' })
)

function onLogout() {
  auth.logout()
  router.replace('/login')
}

onMounted(async () => {
  if (auth.token) await auth.fetchMe()
  await loadBrand() // 公开接口，未登录也能拿到企业名
  ready.value = true
  if (auth.user) {
    loadBadge()
    loadNotifCount()
    badgeTimer = setInterval(() => { loadBadge(); loadNotifCount() }, 60000)
  }
})

// 切换路由时刷新角标（如从任务页返回）
watch(() => route.path, () => {
  if (auth.user) loadBadge()
})
onUnmounted(() => { if (badgeTimer) clearInterval(badgeTimer) })

// ---------- 站内通知 ----------
const bellIcon = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.7 21a2 2 0 0 1-3.4 0"/></svg>'
const notifOpen = ref(false)
const notifs = ref([])
const notifUnread = ref(0)
async function loadNotifCount() {
  if (!auth.user) return
  try {
    const r = await get('/notifications/unread-count')
    notifUnread.value = r.unread || 0
  } catch { /* 忽略 */ }
}
async function openNotifs() {
  notifOpen.value = true
  try { notifs.value = await get('/notifications') } catch { notifs.value = [] }
  loadNotifCount()
}
async function markRead(n) {
  if (n.read) return
  try {
    await fetch('/api/notifications/' + n.id + '/read', { method: 'POST', headers: { Authorization: 'Bearer ' + (localStorage.getItem('sw_token') || '') } })
    n.read = true
    notifUnread.value = Math.max(0, notifUnread.value - 1)
  } catch { /* 忽略 */ }
}
async function readAll() {
  try {
    await fetch('/api/notifications/read-all', { method: 'POST', headers: { Authorization: 'Bearer ' + (localStorage.getItem('sw_token') || '') } })
    notifs.value.forEach((n) => { n.read = true })
    notifUnread.value = 0
  } catch { /* 忽略 */ }
}
function fmtNotif(t) {
  if (!t) return ''
  const d = new Date(t)
  if (isNaN(d)) return t
  const p = (x) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
</script>

<style>
.boot { position: relative; z-index: 2; height: 100%; display: grid; place-items: center; }
.boot-spin {
  width: 38px; height: 38px; border-radius: 50%;
  border: 3px solid var(--overlay-2); border-top-color: var(--accent);
  animation: spin 0.8s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }

.fade-enter-active, .fade-leave-active { transition: opacity 0.18s ease, transform 0.18s ease; }
.fade-enter-from { opacity: 0; transform: translateY(8px); }
.fade-leave-to { opacity: 0; transform: translateY(-8px); }

.ext-dl { display: flex; align-items: center; gap: 9px; padding: 9px 12px; margin-bottom: 6px; border-radius: 11px; border: 1px solid var(--glass-border); color: var(--text-dim); text-decoration: none; font-size: 13px; cursor: pointer; transition: all 0.15s; }
.ext-dl:hover { color: var(--text); border-color: var(--accent); background: var(--overlay-2); }
.ext-ico { display: grid; place-items: center; color: var(--accent); }

.nav-badge {
  margin-left: auto;
  min-width: 20px;
  height: 20px;
  padding: 0 6px;
  border-radius: 10px;
  background: var(--accent);
  color: #fff;
  font-size: 11px;
  font-weight: 700;
  line-height: 20px;
  text-align: center;
  flex: none;
}
.nav-badge.danger { background: var(--danger); }

/* 站内通知：铃铛角标 */
.bell-wrap { position: relative; }
.bell-badge {
  position: absolute; top: -4px; right: -6px;
  min-width: 17px; height: 17px; padding: 0 4px;
  border-radius: 9px; background: var(--danger); color: #fff;
  font-size: 10px; font-weight: 700; line-height: 17px; text-align: center;
  pointer-events: none;
}
.m-icon.bell-wrap .bell-badge { top: -2px; right: -4px; }

/* 通知抽屉 */
.notif-mask {
  position: fixed; inset: 0; z-index: 200;
  background: rgba(0, 0, 0, 0.35);
  display: flex; justify-content: flex-end;
}
.notif-panel {
  width: 380px; max-width: 92vw; height: 100%;
  background: var(--bg); border-left: 1px solid var(--glass-border);
  display: flex; flex-direction: column;
  box-shadow: -8px 0 24px rgba(0, 0, 0, 0.12);
}
.notif-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 16px 18px; border-bottom: 1px solid var(--glass-border);
}
.notif-title { font-size: 16px; font-weight: 800; display: flex; align-items: center; gap: 8px; }
.notif-num {
  font-style: normal; font-size: 11px; font-weight: 700; color: #fff;
  background: var(--danger); border-radius: 9px; padding: 1px 7px;
}
.notif-head-actions { display: flex; align-items: center; gap: 10px; }
.notif-close { font-size: 22px; line-height: 1; color: var(--text-faint); background: none; border: none; cursor: pointer; padding: 4px; }
.notif-list { flex: 1; overflow-y: auto; padding: 8px 0; }
.notif-item {
  padding: 13px 18px; border-bottom: 1px solid var(--glass-border); cursor: pointer;
  transition: background 0.12s; position: relative;
}
.notif-item:hover { background: var(--overlay); }
.notif-item.unread { background: rgba(79, 70, 229, 0.05); }
.notif-item.unread::before {
  content: ''; position: absolute; left: 6px; top: 16px;
  width: 7px; height: 7px; border-radius: 50%; background: var(--accent);
}
.ni-title { font-size: 13.5px; font-weight: 700; color: var(--text); }
.ni-content { font-size: 12.5px; color: var(--text-dim); margin-top: 4px; line-height: 1.6; }
.ni-time { font-size: 11px; color: var(--text-faint); margin-top: 6px; }
.notif-empty { text-align: center; color: var(--text-faint); font-size: 13px; padding: 60px 0; }

.drawer-enter-active, .drawer-leave-active { transition: opacity 0.2s ease; }
.drawer-enter-active .notif-panel, .drawer-leave-active .notif-panel { transition: transform 0.22s ease; }
.drawer-enter-from, .drawer-leave-to { opacity: 0; }
.drawer-enter-from .notif-panel, .drawer-leave-to .notif-panel { transform: translateX(100%); }

@media (max-width: 768px) {
  .notif-panel { width: 100%; max-width: 100vw; }
}
</style>
