// v0.17.0：默认地址不再指向 localhost。
// 历史问题：默认 http://localhost:8080 在普通用户机器上必然连不通，
// 而失败时只提示「今日无排班」，用户完全看不出是地址错了。
const DEFAULT_WEB = 'http://49.51.200.165:8090'
let WEB = DEFAULT_WEB
async function loadBase() {
  const r = await new Promise((res) => chrome.storage.local.get(['swb_base'], res))
  WEB = (r.swb_base && r.swb_base.trim()) || DEFAULT_WEB
}
// v0.14.2：从 manifest 读取真实版本号显示在标题栏
// 用于排查 Chrome 扩展缓存问题——若角标显示 v0.0.1 则说明插件版本未刷新到最新
try {
  const ver = chrome.runtime.getManifest().version
  const el = document.getElementById('plugin-ver')
  if (el) el.textContent = 'v' + ver
} catch (_) { /* 非扩展环境忽略 */ }
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
    const data = { dash, tasks, me, schedules, ts: Date.now(), err: '' }
    chrome.storage.local.set({ swb_cache: data })
    render(data)
    renderBanner('')
    show('dash')
    // 通知后台刷新角标
    chrome.runtime.sendMessage({ type: 'REFRESH_BADGE' })
  } catch (e) {
    // v0.17.0：不再静默失败。拉取失败时若还展示旧缓存，用户会以为「今日无排班」。
    // 统一给出可定位的原因（地址不通 / 登录过期 / 服务异常）。
    const msg = describeError(e)
    chrome.storage.local.get(['swb_cache'], (r) => {
      const cached = r.swb_cache
      if (cached) {
        render(Object.assign({}, cached, { err: msg }))
      } else {
        const card = $('shift-card')
        if (card) { card.className = 'shift-card off'; card.textContent = '数据加载失败' }
        show('dash')
      }
      show('dash')
      renderBanner(msg)
    })
  }
}

// 把异常转成用户能看懂、能自行动手修复的提示
function describeError(e) {
  const raw = (e && e.message) || '未知错误'
  if (/Failed to fetch|NetworkError|load failed/i.test(raw)) {
    return '连不上服务器（' + WEB + '）。请点上方「⚙ 服务器地址」确认地址填写正确且网络可达。'
  }
  return raw
}

// 顶部错误横幅：有错必显，避免「静默显示成无数据」
function renderBanner(msg) {
  let el = $('err-banner')
  if (!msg) {
    if (el) el.remove()
    return
  }
  if (!el) {
    el = document.createElement('div')
    el.id = 'err-banner'
    el.className = 'err-banner'
    const hd = document.querySelector('.hd')
    if (hd && hd.parentNode) hd.parentNode.insertBefore(el, hd.nextSibling)
  }
  el.textContent = '⚠ ' + msg
}


function render(data) {
  const { dash, tasks, me, schedules, ts } = data
  const todayStr = dash.today || new Date().toISOString().slice(0, 10)

  // 有失败原因时先显示横幅（缓存渲染路径同样要提示，避免误读成「无排班」）
  if (data.err) renderBanner(data.err)

  // ---- 今日班次 ----
  // v0.17.0：优先用服务端算好的 my_shift（姓名/工号/用户名任一命中即当班），
  // 本地匹配只作为老版本服务端的兜底。此前纯本地按姓名匹配，
  // 姓名与排班名单不完全一致时会误判成「今日无排班」。
  let myShift = String(dash.my_shift || '').trim()
  let shiftTime = String(dash.my_shift_time || '').trim()
  const meName = String((dash.me_name || (me && me.name) || '')).trim()
  const meNo = String((dash.me_emp_no || (me && me.emp_no) || '')).trim()
  if (!myShift && me && Array.isArray(schedules)) {
    const keys = [me.name, me.username, me.emp_no, (me.user && me.user.name)]
      .filter(Boolean).map((x) => String(x).trim())
    const mine = schedules.find((s) => s.date === todayStr && Array.isArray(s.people) &&
      s.people.some((p) => keys.includes(String(p).trim())))
    if (mine) myShift = mine.shift || '值班'
  }

  // 姓名（工号）行
  const meLine = $('me-line')
  if (meLine) {
    const who = meName ? (meNo ? meName + '（' + meNo + '）' : meName) : ''
    meLine.textContent = who
    meLine.title = who
  }

  // 班次卡片
  const card = $('shift-card')
  if (card) {
    if (myShift) {
      card.className = 'shift-card on'
      card.innerHTML =
        '<span class="sc-label">今日班次</span>' +
        '<span class="sc-shift">' + esc(myShift) + '</span>' +
        (shiftTime ? '<span class="sc-time">' + esc(shiftTime) + '</span>' : '')
    } else {
      card.className = 'shift-card off'
      card.textContent = '今日无排班'
      card.title = '今日班表中没有匹配到你的姓名或工号；若确有排班，请确认班表里填写的姓名与账号一致'
    }
  }

  // 更新时间
  if (ts) {
    const d = new Date(ts)
    $('updated').textContent = '更新于 ' + String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0')
  }

  // 统计
  const todayTasks = (dash.today_task_list || tasks.filter((t) => t.due_today)).filter((t) => t.status !== 'done')
  const overdue = tasks.filter((t) => t.overdue && t.status !== 'done')
  const running = tasks.filter((t) => t.running && t.status !== 'done')
  // 「即将开始」dashboard 已带 starting_count；优先用字段，没有再回退到遍历
  const startingCount = (typeof dash.starting_count === 'number')
    ? dash.starting_count
    : todayTasks.filter((t) => t.starting && !t.running && !t.overdue).length
  const month = (dash.month_task_list || tasks.filter((t) => t.due_this_month)).filter((t) => t.status !== 'done')
  $('st-today').textContent = todayTasks.length
  $('st-starting').textContent = startingCount
  $('st-running').textContent = running.length
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
    // v0.17.0：负责人显示「姓名（工号）」，并补上班次标签。
    // 弹窗仅 340px 宽，任务行里不再重复班次时间段（顶部班次卡片已有），
    // 否则「时间 · 姓名（工号） · 09:00-18:00」会被截断成省略号。
    const who = assigneeTextOf(t)
    const shift = String(t.shift || '').trim()
    const metaBits = []
    if (when) metaBits.push(esc(when))
    if (who) metaBits.push(esc(who))
    // 「全员」= 谁当班谁负责，标出来反而占位，不显示标签
    const showShiftTag = shift && shift !== '全员'
    li.innerHTML =
      `<button class="ck" title="标记完成" data-id="${t.id}"></button>` +
      (showShiftTag ? `<span class="sh ${esc(shiftClassOf(shift))}">${esc(shiftLabelOf(shift))}</span>` : '') +
      `<span class="t"><span class="n">${esc(t.title)}</span>` +
      `<span class="m">${metaBits.join(' · ')}</span></span>` +
      (t.overdue ? '<span class="badge">逾期</span>' : '') +
      (t.running && !t.overdue ? '<span class="badge run">执行中</span>' : '') +
      (t.starting && !t.running && !t.overdue ? '<span class="badge start">即将开始</span>' : '')
    ul.appendChild(li)
  })
  ul.querySelectorAll('.ck').forEach((b) => (b.onclick = () => toggleTask(b)))
}

// 负责人文案：优先服务端下发的 assignee_text（含工号），否则本地解析
function assigneeTextOf(t) {
  if (t.assignee_text) return String(t.assignee_text)
  if (Array.isArray(t.assignee_names) && t.assignee_names.length) return t.assignee_names.join('、')
  if (t.assignees) {
    try { const a = JSON.parse(t.assignees); if (Array.isArray(a) && a.length) return a.join('、') } catch (e) { /* 忽略 */ }
  }
  return t.assignee ? String(t.assignee) : ''
}

// 班次简称：插件只显示班次名，不显示时间段（时间段放 meta 里）
function shiftLabelOf(s) {
  return String(s || '').trim()
}
function shiftClassOf(s) {
  return { '早班': 's-early', '中班': 's-mid', '晚班': 's-late', '夜班': 's-night', '早晚': 's-both' }[String(s || '').trim()] || ''
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
