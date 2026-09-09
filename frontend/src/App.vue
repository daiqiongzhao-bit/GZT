<template>
  <div class="app-bg">
    <div class="blob b1"></div>
    <div class="blob b2"></div>
    <div class="blob b3"></div>

    <div v-if="!ready" class="boot">
      <div class="boot-spin"></div>
    </div>

    <!-- 强制改密（安全策略：must_change_pwd 为真的用户必须先改强密码） -->
    <div v-else-if="auth.user && auth.user.must_change_pwd" class="force-pwd-wrap">
      <div class="force-pwd-card glass">
        <div class="force-pwd-icon" v-html="shieldIcon"></div>
        <h2 class="force-pwd-title">安全策略：请先修改密码</h2>
        <p class="force-pwd-tip">系统检测到你的密码强度不足或刚被重置。为账号安全，请先设置一个新密码：<br /><strong>至少 8 位，且需同时包含字母和数字</strong>（如 <code>Cdf123456</code>）。</p>
        <div class="force-pwd-form">
          <label class="fld">当前密码</label>
          <input v-model="fpwd.old_password" type="password" class="glass-input" autocomplete="current-password" placeholder="输入你正在使用的密码" />
          <label class="fld">新密码</label>
          <input v-model="fpwd.new_password" type="password" class="glass-input" autocomplete="new-password" placeholder="至少 8 位，含字母和数字" />
          <label class="fld">确认新密码</label>
          <input v-model="fpwd.confirm" type="password" class="glass-input" autocomplete="new-password" placeholder="再次输入新密码" @keyup.enter="doForceChangePwd" />
        </div>
        <button class="btn primary force-pwd-btn" :disabled="fpwdSaving" @click="doForceChangePwd">{{ fpwdSaving ? '保存中…' : '保存并进入系统' }}</button>
        <button class="force-pwd-logout" @click="onLogout">退出登录</button>
      </div>
    </div>

    <div v-else-if="auth.user" class="shell">
      <!-- 侧边栏（桌面） -->
      <aside class="sidebar glass">
        <div class="brand">
          <div class="logo" :class="{ 'has-logo': brand.logo }">
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

        <button v-if="canInstall" class="ext-dl install-btn" type="button" @click="installPwa" title="把应用安装到桌面，像 APP 一样打开">
          <span class="ext-ico" v-html="downloadIcon"></span>
          <span>安装到桌面</span>
        </button>

        <div class="side-foot">
          <div class="user-mini">
            <div class="avatar">{{ userInitial }}</div>
            <div>
              <div class="uname">{{ auth.user.name }}</div>
              <div class="urole">{{ auth.roleLabel }}<em v-if="appVersion" class="u-ver"> · v{{ appVersion }}</em></div>
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
          <div class="top-title">{{ pageTitle }}<em class="m-ver" v-if="appVersion"> v{{ appVersion }}</em></div>
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

        <!-- 底部版权（设置里「企业信息-版权信息」生效后可见） -->
        <footer v-if="brand.copyright" class="app-footer">© {{ brand.copyright }}</footer>
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
                <button v-if="auth.canManage" class="btn ghost sm" :class="{ on: bcastOpen }" @click="toggleBcast">{{ bcastOpen ? '收起' : '发通知' }}</button>
                <button class="btn ghost sm" :disabled="!notifUnread" @click="readAll">全部已读</button>
                <button class="notif-close" @click="notifOpen = false" title="关闭">×</button>
              </div>
            </div>
            <div v-if="bcastOpen" class="bcast-form">
              <div v-if="auth.isSuper" class="bcast-row">
                <span class="bcast-label">发给部门</span>
                <select v-model="bcastDept" class="bcast-input" :disabled="sendingBcast">
                  <option :value="0" disabled>请选择部门</option>
                  <option v-for="d in deptOpts" :key="d.id" :value="d.id">{{ indentOf(d.depth) }}{{ d.name }}</option>
                </select>
              </div>
              <p v-else class="bcast-hint">广播将发送给你所在部门（含子部门）的全部成员</p>
              <input v-model="bcastTitle" class="bcast-input" maxlength="60" placeholder="通知标题（必填）" :disabled="sendingBcast" />
              <textarea v-model="bcastContent" class="bcast-input bcast-ta" rows="3" maxlength="500" placeholder="通知内容（必填）" :disabled="sendingBcast"></textarea>
              <input v-model="bcastLink" class="bcast-input" maxlength="500" placeholder="附带链接（可选，https://…）" :disabled="sendingBcast" />
              <button class="btn primary sm full" :disabled="sendingBcast || !bcastTitle.trim() || !bcastContent.trim()" @click="doBroadcast">
                {{ sendingBcast ? '发送中…' : '发送广播' }}
              </button>
            </div>
            <div class="notif-list">
              <div v-for="n in notifs" :key="n.id" class="notif-item" :class="{ unread: !n.read }" @click="markRead(n)">
                <div class="ni-title">{{ n.title }}</div>
                <div class="ni-content">{{ n.content }}</div>
                <a v-if="n.link" class="ni-link" :href="safeUrl(n.link)" target="_blank" rel="noopener noreferrer" @click.stop>🔗 {{ linkLabel(n.link) }}</a>
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
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/store/auth'
import { navItems, icons } from '@/icons'
import { brand, loadBrand } from '@/brand'
import { applyTheme } from '@/theme'
import { get, post } from '@/api'
import { deptOptions, indentOf } from '@/utils/dept'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const ready = ref(false)

// 左下角 / 顶部版本号（取 /api/version，一次即可）
const appVersion = ref('')
async function loadVersion() {
  try {
    const v = await get('/version')
    appVersion.value = (v && v.version && String(v.version).replace(/^v/i, '')) || ''
  } catch (e) { /* 忽略，版本号非关键 */ }
}

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

const company = computed(() => brand.company_name || '企业任务通知管理')
const slogan = computed(() => brand.slogan || '三端同步 · 安全可控 · 无限扩展')
const companyInitial = computed(() => (company.value || '工')[0])

const userInitial = computed(() => (auth.user?.name || '?')[0])
const pageTitle = computed(() => route.meta.title || '知识库')
// 上传了企业 Logo 时用图片，否则用内置图标；?v=文件名 保证换图后浏览器立即刷新
const logoUrl = computed(() => '/api/settings/logo?v=' + encodeURIComponent(brand.logo || ''))
const brandLogo = '<svg viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="17" rx="3"/><path d="M3 9h18M8 2v4M16 2v4"/><path d="M8.5 14.5l2.2 2.2 4.3-4.4" stroke-width="2.4"/></svg>'
const logoutIcon = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="M16 17l5-5-5-5"/><path d="M21 12H9"/></svg>'
const shieldIcon = '<svg viewBox="0 0 24 24" width="30" height="30" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-3.5 8-10V5l-8-3-8 3v7c0 6.5 8 10 8 10z"/><path d="M9 12.5l2 2 4-4.5"/></svg>'
const puzzleIcon = '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3v3m0 0a2 2 0 1 0 0 4 2 2 0 0 0 0-4zm0 4v3m-6 4h3m0 0a2 2 0 1 0 4 0 2 2 0 0 0-4 0zm4 0h3m-3-6V6a2 2 0 1 1 4 0v3m0 0a2 2 0 1 1 0 4 2 2 0 0 1 0-4zm0 4v3a2 2 0 1 1-4 0"/></svg>'
const downloadIcon = '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>'

// v0.9.2 PWA 安装按钮：本地部署后浏览器地址栏虽有 [⬇] 图标但应用内不弹窗，
// 监听 main.js 缓存的 beforeinstallprompt 事件，由这里显式调用 prompt() 触发原生安装弹窗
const canInstall = ref(false)
function onPwaInstallable() { canInstall.value = !!window.__pwaInstallEvent }
function onPwaInstalled() { canInstall.value = false }
async function installPwa() {
  const ev = window.__pwaInstallEvent
  if (!ev) {
    // 没有可用事件：浏览器可能已经触发过 / 用户在 iOS Safari（只支持添加到主屏幕）
    if (/iPhone|iPad|iPod/.test(navigator.userAgent)) {
      alert('iOS 暂不支持应用内安装，请在 Safari 分享菜单选择「添加到主屏幕」')
    } else {
      alert('当前浏览器暂未提供安装入口，可点地址栏右侧的 [⬇] 图标手动安装')
    }
    return
  }
  try {
    await ev.prompt()
    const choice = await ev.userChoice
    if (choice && choice.outcome === 'accepted') canInstall.value = false
  } catch (e) {
    // ignore
  }
  window.__pwaInstallEvent = null
}
onMounted(() => {
  window.addEventListener('pwa-installable', onPwaInstallable)
  window.addEventListener('pwa-installed', onPwaInstalled)
  // 进入即检查一次（如果事件在 main.js 之前已触发）
  onPwaInstallable()
})
onUnmounted(() => {
  window.removeEventListener('pwa-installable', onPwaInstallable)
  window.removeEventListener('pwa-installed', onPwaInstalled)
})
const todayText = computed(() =>
  new Date().toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric', weekday: 'long' })
)

function onLogout() {
  auth.logout()
  router.replace('/login')
}

// ---------- 强制改密（v0.3.0 密码安全加固）----------
const fpwd = reactive({ old_password: '', new_password: '', confirm: '' })
const fpwdSaving = ref(false)
function isStrongPwd(p) {
  if (!p || p.length < 8) return false
  return /[0-9]/.test(p) && /[A-Za-z]/.test(p)
}
async function doForceChangePwd() {
  if (!fpwd.old_password || !fpwd.new_password) { alert('请填写当前密码和新密码'); return }
  if (!isStrongPwd(fpwd.new_password)) { alert('新密码至少 8 位，且需同时包含字母和数字'); return }
  if (fpwd.new_password !== fpwd.confirm) { alert('两次输入的新密码不一致'); return }
  fpwdSaving.value = true
  try {
    await post('/auth/change-password', { old_password: fpwd.old_password, new_password: fpwd.new_password })
    // 改密成功后清除强制标记，进入系统
    await auth.fetchMe()
    Object.assign(fpwd, { old_password: '', new_password: '', confirm: '' })
    router.replace('/')
  } catch (e) {
    alert((e.response && e.response.data && e.response.data.error) || '修改密码失败，请稍后重试')
  } finally { fpwdSaving.value = false }
}

onMounted(async () => {
  if (auth.token) await auth.fetchMe()
  await loadBrand() // 公开接口，未登录也能拿到企业名
  // 浏览器标签 title 跟随企业名（默认：企业任务通知管理）
  document.title = brand.company_name || "企业任务通知管理"
  ready.value = true
  if (auth.user) {
    loadBadge()
    loadNotifCount()
    badgeTimer = setInterval(() => { loadBadge(); loadNotifCount() }, 60000)
  }
  loadVersion()
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

// ---------- 部门广播（v0.2.0）----------
const bcastOpen = ref(false)
const bcastDept = ref(0)
const bcastTitle = ref('')
const bcastContent = ref('')
const bcastLink = ref('')
const sendingBcast = ref(false)
const deptList = ref([])
const deptOpts = computed(() => deptOptions(deptList.value))

// 仅放行 http/https 链接（防伪协议注入）；非法或空返回空
function safeUrl(u) {
  if (!u) return ''
  const s = String(u).trim()
  if (/^https?:\/\//i.test(s)) return s
  return ''
}
// 展示用的链接短文本（截断过长 URL）
function linkLabel(u) {
  const s = safeUrl(u)
  if (!s) return ''
  const host = s.replace(/^https?:\/\//i, '').split('/')[0]
  return (s.length > 46 ? host + '/…' : s)
}

async function toggleBcast() {
  bcastOpen.value = !bcastOpen.value
  if (bcastOpen.value && auth.isSuper && !deptList.value.length) {
    try { deptList.value = await get('/departments') } catch { /* 忽略 */ }
  }
}
async function doBroadcast() {
  if (!bcastTitle.value.trim() || !bcastContent.value.trim()) return
  // 超管必须选部门；部门管理员不选即发给本人部门
  if (auth.isSuper && !bcastDept.value) { alert('请选择接收部门'); return }
  sendingBcast.value = true
  try {
    const r = await post('/notifications/broadcast', {
      dept_id: auth.isSuper ? bcastDept.value : 0,
      title: bcastTitle.value.trim(),
      content: bcastContent.value.trim(),
      link: safeUrl(bcastLink.value)
    })
    bcastOpen.value = false
    bcastTitle.value = ''
    bcastContent.value = ''
    bcastLink.value = ''
    const dn = r.dept_name || '部门'
    alert(`已向「${dn}」${r.sent} 名成员发送广播通知`)
    // 发送后刷新我的通知列表与未读
    notifs.value = await get('/notifications')
    loadNotifCount()
  } catch (e) {
    alert((e.response && e.response.data && e.response.data.error) || '发送失败')
  } finally { sendingBcast.value = false }
}
</script>

<style>
/* 上传企业 Logo 后去除侧边栏品牌 logo 的蓝紫底 */
.logo.has-logo { background: transparent; box-shadow: none; }
.boot { position: relative; z-index: 2; height: 100%; display: grid; place-items: center; }
.boot-spin {
  width: 38px; height: 38px; border-radius: 50%;
  border: 3px solid var(--overlay-2); border-top-color: var(--accent);
  animation: spin 0.8s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }

/* 强制改密（v0.3.0 密码安全加固） */
.force-pwd-wrap { position: relative; z-index: 2; height: 100%; display: grid; place-items: center; padding: 20px; box-sizing: border-box; }
.force-pwd-card { width: 100%; max-width: 420px; padding: 30px 26px; border-radius: 20px; text-align: center; }
.force-pwd-icon { width: 58px; height: 58px; margin: 0 auto 14px; border-radius: 16px; background: var(--accent-soft); color: var(--accent); display: grid; place-items: center; }
.force-pwd-title { margin: 0 0 8px; font-size: 18px; font-weight: 800; color: var(--text); }
.force-pwd-tip { margin: 0 0 20px; font-size: 13px; color: var(--text-dim); line-height: 1.7; }
.force-pwd-tip strong { color: var(--danger); }
.force-pwd-tip code { background: var(--overlay-2); padding: 1px 6px; border-radius: 5px; font-size: 12px; }
.force-pwd-form { text-align: left; display: flex; flex-direction: column; gap: 6px; }
.force-pwd-form .fld { font-size: 12px; color: var(--text-faint); margin-top: 4px; }
.force-pwd-form .glass-input { width: 100%; box-sizing: border-box; padding: 11px 13px; font-size: 16px; }
.force-pwd-btn { width: 100%; margin-top: 16px; padding: 12px; font-size: 15px; border-radius: 12px; }
.force-pwd-logout { margin-top: 14px; background: none; border: none; color: var(--text-faint); font-size: 13px; cursor: pointer; padding: 6px; text-decoration: underline; }
.force-pwd-logout:hover { color: var(--danger); }

.fade-enter-active, .fade-leave-active { transition: opacity 0.18s ease, transform 0.18s ease; }
.fade-enter-from { opacity: 0; transform: translateY(8px); }
.fade-leave-to { opacity: 0; transform: translateY(-8px); }

.ext-dl { display: flex; align-items: center; gap: 9px; padding: 9px 12px; margin-bottom: 6px; border-radius: 11px; border: 1px solid var(--glass-border); color: var(--text-dim); text-decoration: none; font-size: 13px; cursor: pointer; transition: all 0.15s; background: transparent; font-family: inherit; width: 100%; text-align: left; }
.ext-dl:hover { color: var(--text); border-color: var(--accent); background: var(--overlay-2); }
.ext-dl.install-btn { color: var(--accent); border-color: rgba(79, 70, 229, 0.35); background: rgba(79, 70, 229, 0.06); }
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
  background: var(--mask);
  display: flex; justify-content: flex-end;
}
.notif-panel {
  width: 380px; max-width: 92vw; height: 100%;
  background: var(--bg-1); border-left: 1px solid var(--glass-border);
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
.notif-head-actions .btn { padding: 7px 12px; font-size: 12.5px; border-radius: 10px; }
.btn.on { color: var(--accent); border-color: var(--accent); background: var(--accent-soft); }
.notif-close { font-size: 22px; line-height: 1; color: var(--text-faint); background: none; border: none; cursor: pointer; padding: 4px; }
/* 部门广播表单（v0.2.0） */
.bcast-form { padding: 12px 16px 14px; border-bottom: 1px solid var(--glass-border); background: var(--overlay); display: flex; flex-direction: column; gap: 9px; }
.bcast-row { display: flex; flex-direction: column; gap: 4px; }
.bcast-label { font-size: 11.5px; color: var(--text-faint); }
.bcast-input { width: 100%; padding: 9px 11px; border-radius: 10px; background: var(--bg-1); border: 1px solid var(--glass-border); color: var(--text); font-size: 14px; outline: none; box-sizing: border-box; }
.bcast-input:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.bcast-input::placeholder { color: var(--text-faint); }
.bcast-ta { resize: vertical; min-height: 62px; font-family: inherit; line-height: 1.5; }
.bcast-hint { font-size: 12px; color: var(--text-faint); margin: 0; line-height: 1.5; }
.full { width: 100%; justify-content: center; }
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
.ni-link { display: inline-block; margin-top: 6px; font-size: 12px; color: var(--accent, #4f46e5); text-decoration: none; word-break: break-all; }
.ni-link:hover { text-decoration: underline; }
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
