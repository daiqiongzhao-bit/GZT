const DEFAULT_WEB = 'http://localhost:8080'  // 改成你自己的部署地址
let WEB = DEFAULT_WEB
async function loadBase() {
  const r = await new Promise((res) => chrome.storage.local.get(['swb_base'], res))
  WEB = (r.swb_base && r.swb_base.trim()) || DEFAULT_WEB
}
const $ = (id) => document.getElementById(id)

function show(id) {
  ['login', 'loading', 'dash'].forEach((x) => $(x).classList.toggle('hidden', x !== id))
}

function getToken() {
  return new Promise((res) => chrome.storage.local.get(['swb_token'], (r) => res(r.swb_token || '')))
}
function setToken(t, u) {
  return new Promise((res) => chrome.storage.local.set({ swb_token: t, swb_user: u || '' }, res))
}
function clearToken(cb) {
  chrome.storage.local.remove(['swb_token', 'swb_user'], cb)
}

async function api(path, token, opts = {}) {
  const r = await fetch(WEB + path, {
    method: opts.method || 'GET',
    headers: Object.assign({ Authorization: 'Bearer ' + token }, opts.body ? { 'Content-Type': 'application/json' } : {}),
    body: opts.body ? JSON.stringify(opts.body) : undefined
  })
  if (r.status === 401) {
    clearToken(() => show('login'))
    throw new Error('登录已过期，请重新登录')
  }
  return r.json()
}

function esc(s) {
  return String(s == null ? '' : s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]))
}
function timeOf(t) {
  if (!t) return ''
  const m = /T(\d{2}:\d{2})/.exec(t)
  return m ? m[1] : ''
}

// 先渲染缓存（秒开），再后台刷新
async function load() {
  const token = await getToken()
  if (!token) { fillFromCache(null); show('login'); return }
  // 尝试先展示上次的缓存
  chrome.storage.local.get(['swb_cache'], (r) => {
    if (r.swb_cache) render(r.swb_cache)
  })
  show('loading')
  await refresh(token)
}

async function refresh(token) {
  token = token || (await getToken())
  if (!token) { show('login'); return }
  try {
    const [dash, tasks, me, schedules] = await Promise.all([
      api('/api/dashboard', token),
      api('/api/tasks', token),
      api('/api/auth/me', token).catch(() => null),
      api('/api/schedules', token).catch(() => [])
    ])
    const data = { dash, tasks, me, schedules, ts: Date.now() }
    chrome.storage.local.set({ swb_cache: data })
    render(data)
    show('dash')
    // 通知后台刷新角标
    chrome.runtime.sendMessage({ type: 'REFRESH_BADGE' })
  } catch (e) {
    // 缓存存在则继续展示，仅提示
    chrome.storage.local.get(['swb_cache'], (r) => {
      if (r.swb_cache) {
        render(r.swb_cache)
        show('dash')
      } else {
        show('login')
      }
    })
  }
}

function render(data) {
  const { dash, tasks, me, schedules, ts } = data
  const todayStr = dash.today || new Date().toISOString().slice(0, 10)

  // 今日班次：匹配本人当日排班
  let myShift = '—'
  if (me && Array.isArray(schedules)) {
    const keys = [me.name, me.username, (me.user && me.user.name)].filter(Boolean).map((x) => String(x).toLowerCase())
    const mine = schedules.find((s) => s.date === todayStr && Array.isArray(s.people) &&
      s.people.some((p) => keys.includes(String(p).toLowerCase())))
    if (mine) myShift = mine.shift || '值班'
  }
  const shiftEl = $('my-shift')
  shiftEl.textContent = myShift === '—' ? '今日无排班' : '今日班次 · ' + myShift
  shiftEl.className = 'shift-chip' + (myShift === '—' ? ' none' : '')

  // 更新时间
  if (ts) {
    const d = new Date(ts)
    $('updated').textContent = '更新于 ' + String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0')
  }

  // 统计
  const todayTasks = (dash.today_task_list || tasks.filter((t) => t.due_today)).filter((t) => t.status !== 'done')
  const overdue = tasks.filter((t) => t.overdue && t.status !== 'done')
  const month = (dash.month_task_list || tasks.filter((t) => t.due_this_month)).filter((t) => t.status !== 'done')
  $('st-today').textContent = todayTasks.length
  $('st-over').textContent = overdue.length
  $('st-month').textContent = month.length

  // 今日待办列表（可勾选完成）
  const ul = $('today-list')
  ul.innerHTML = ''
  if (!todayTasks.length) {
    ul.innerHTML = '<li class="empty"><span>🎉 今日无待办</span></li>'
    return
  }
  todayTasks.slice(0, 14).forEach((t) => {
    const li = document.createElement('li')
    li.className = 'task'
    li.dataset.id = t.id
    const when = t.time ? timeOf('T' + t.time) : timeOf(t.deadline)
    li.innerHTML =
      `<button class="ck" title="标记完成" data-id="${t.id}"></button>` +
      `<span class="t"><span class="n">${esc(t.title)}</span>` +
      `<span class="m">${when}${t.assignee ? ' · ' + esc(t.assignee) : ''}</span></span>` +
      (t.overdue ? '<span class="badge">逾期</span>' : '')
    ul.appendChild(li)
  })
  ul.querySelectorAll('.ck').forEach((b) => (b.onclick = () => toggleTask(b)))
}

async function toggleTask(btn) {
  const id = btn.dataset.id
  const li = btn.closest('li')
  const token = await getToken()
  if (!token) { show('login'); return }
  btn.classList.add('busy')
  try {
    const j = await api('/api/tasks/' + id + '/toggle', token, { method: 'POST' })
    // 完成即移除该项
    if (j && j.status === 'done') {
      li.style.transition = 'opacity .2s'
      li.style.opacity = '0'
      setTimeout(() => li.remove(), 180)
    } else {
      btn.classList.add('on')
    }
    // 刷新统计
    refresh(token)
    chrome.runtime.sendMessage({ type: 'REFRESH_BADGE' })
  } catch (e) {
    btn.classList.remove('busy')
    alert(e.message || '操作失败')
  }
}

async function doLogin() {
  const u = $('inp-u').value.trim()
  const p = $('inp-p').value
  const err = $('login-err')
  err.textContent = ''
  if (!u || !p) { err.textContent = '请输入账号和密码'; return }
  const btn = $('btn-login')
  btn.disabled = true; btn.textContent = '登录中…'
  try {
    const r = await fetch(WEB + '/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: u, password: p, client_type: 'extension' })
    })
    const j = await r.json()
    if (!r.ok || !j.token) throw new Error(j.error || '登录失败')
    await setToken(j.token, u)
    show('loading')
    await refresh(j.token)
  } catch (e) {
    err.textContent = e.message || '登录失败'
    btn.disabled = false; btn.textContent = '登 录'
  }
}

// 事件
$('btn-login').onclick = doLogin
$('inp-p').onkeydown = (e) => { if (e.key === 'Enter') doLogin() }
$('inp-u').onkeydown = (e) => { if (e.key === 'Enter') $('inp-p').focus() }
$('btn-refresh').onclick = () => refresh()
$('btn-open-web').onclick = () => chrome.tabs.create({ url: WEB + '/login' })
$('btn-open-today').onclick = () => chrome.tabs.create({ url: WEB + '/tasks' })
$('btn-open-sched').onclick = () => chrome.tabs.create({ url: WEB + '/schedule' })
$('btn-logout').onclick = () => clearToken(() => show('login'))
$('btn-save-base').onclick = async () => {
  let v = $('inp-base').value.trim()
  if (!v) v = DEFAULT_WEB
  if (!/^https?:\/\//i.test(v)) v = 'https://' + v
  v = v.replace(/\/+$/, '')
  await new Promise((res) => chrome.storage.local.set({ swb_base: v }, res))
  WEB = v
  $('base-err').textContent = '已保存，正在刷新…'
  await refresh()
  $('base-err').textContent = ''
}
chrome.storage.local.get(['swb_user'], (r) => { if (r.swb_user) $('inp-u').value = r.swb_user })
chrome.storage.local.get(['swb_base'], (r) => { if (r.swb_base) $('inp-base').value = r.swb_base })

// ===== 任务到点提醒设置 =====
const chkRemind = $('chk-remind')
const inpLead = $('inp-lead')
function showRemindMsg(s) {
  const el = $('remind-msg')
  el.textContent = s
  setTimeout(() => { if (el.textContent === s) el.textContent = '' }, 2500)
}
chrome.storage.local.get(['swb_remind_enabled', 'swb_lead_min'], (r) => {
  chkRemind.checked = r.swb_remind_enabled !== false
  inpLead.value = r.swb_lead_min || 5
})
chkRemind.onchange = () => {
  chrome.storage.local.set({ swb_remind_enabled: chkRemind.checked }, () =>
    showRemindMsg(chkRemind.checked ? '已开启提醒' : '已关闭提醒'))
}
inpLead.onchange = () => {
  const v = Math.max(0, Math.min(120, Number(inpLead.value) || 0))
  inpLead.value = v
  chrome.storage.local.set({ swb_lead_min: v }, () => showRemindMsg('提前 ' + v + ' 分钟提醒'))
}

loadBase().then(load)
