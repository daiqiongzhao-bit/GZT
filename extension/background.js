// 后台：工具栏角标 + 任务到点桌面提醒 + 旧版网页端令牌桥接
const DEFAULT_WEB = 'http://localhost:8080'  // 改成你自己的部署地址
const ALARM = 'swb-refresh'       // 角标刷新（15 分钟）
const REMIND_ALARM = 'swb-remind' // 到点提醒（1 分钟）
const REMINDED_KEY = 'swb_reminded'

function getToken() {
  return new Promise((res) => chrome.storage.local.get(['swb_token'], (r) => res(r.swb_token || '')))
}
function getBase() {
  return new Promise((res) => chrome.storage.local.get(['swb_base'], (r) => res((r.swb_base && r.swb_base.trim()) || DEFAULT_WEB)))
}
function pad(n) {
  return String(n).padStart(2, '0')
}
function localDateStr(d) {
  return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate())
}

// 拉取概览，把逾期/今日待办数量写到工具栏角标
async function refreshBadge() {
  const token = await getToken()
  if (!token) {
    chrome.action.setBadgeText({ text: '' })
    return
  }
  try {
    const WEB = await getBase()
    const r = await fetch(WEB + '/api/dashboard', { headers: { Authorization: 'Bearer ' + token } })
    if (!r.ok) {
      chrome.action.setBadgeText({ text: '' })
      return
    }
    const d = await r.json()
    const overdue = d.overdue_count || 0
    const today = d.today_tasks || 0
    let text = ''
    let color = '#4f46e5'
    if (overdue > 0) {
      text = String(overdue)
      color = '#ef4444' // 逾期红
    } else if (today > 0) {
      text = String(today)
    }
    chrome.action.setBadgeText({ text })
    chrome.action.setBadgeBackgroundColor({ color })
    chrome.storage.local.set({ swb_badge: { overdue, today, ts: Date.now() } })
  } catch (e) {
    // 网络异常不打扰用户，仅清除角标
    chrome.action.setBadgeText({ text: '' })
  }
}

// ===================== 任务到点提醒 =====================
// 未完成任务到点（可提前 N 分钟）时弹出系统桌面通知，当天/单次不重复。
async function checkReminders() {
  const cfg = await new Promise((r) => chrome.storage.local.get(['swb_remind_enabled', 'swb_lead_min'], r))
  if (cfg.swb_remind_enabled === false) return
  const token = await getToken()
  if (!token) return
  const WEB = await getBase()
  let tasks
  try {
    const r = await fetch(WEB + '/api/tasks', { headers: { Authorization: 'Bearer ' + token } })
    if (!r.ok) return
    tasks = await r.json()
  } catch (e) {
    return // 网络异常静默
  }
  const lead = Math.max(0, Number(cfg.swb_lead_min) || 0) * 60000
  const now = new Date()
  const today = localDateStr(now)
  const stored = await new Promise((r) => chrome.storage.local.get([REMINDED_KEY], r))
  const map = (stored && stored[REMINDED_KEY]) || {}
  const due = []

  for (const t of tasks || []) {
    if (!t || t.status === 'done') continue
    let at = null
    let dueKey = ''
    let title = '任务到点'
    if (t.type === 'daily' && t.time) {
      const m = /^(\d{2}):(\d{2})$/.exec(String(t.time).trim())
      if (!m) continue
      at = new Date(now.getFullYear(), now.getMonth(), now.getDate(), +m[1], +m[2], 0, 0)
      dueKey = 'daily:' + today // 每天提醒一次
      title = '每日任务到点'
    } else if (t.type === 'once' && t.deadline) {
      const d = new Date(String(t.deadline).replace('T', ' '))
      if (isNaN(d.getTime())) continue
      at = d
      dueKey = 'once:' + t.id // 单次提醒一次
      title = '任务到截止时间'
    } else if (t.type === 'monthly' && t.deadline) {
      const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(String(t.deadline))
      if (!m) continue
      at = new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], 0, 0)
      dueKey = 'monthly:' + t.id + ':' + today // 本月当日提醒一次
      title = '月度任务到点'
    } else {
      continue
    }
    const windowStart = at.getTime() - lead
    if (now.getTime() < windowStart) continue // 还没到点
    if (map[t.id] === dueKey) continue // 已提醒过
    map[t.id] = dueKey
    const shiftText = t.shift ? '[' + t.shift + '] ' : ''
    const when = now.getTime() < at.getTime() ? '即将到点' : (t.overdue ? '已逾期' : '已到点')
    due.push({ id: t.id, title: shiftText + t.title, text: when + '，请尽快处理' })
  }

  if (due.length) {
    chrome.storage.local.set({ [REMINDED_KEY]: map })
    for (const d of due) {
      chrome.notifications.create('swb-remind-' + d.id + '-' + Date.now(), {
        type: 'basic',
        iconUrl: 'icons/icon128.png',
        title: '🔔 ' + d.title,
        message: d.text,
        priority: 1
      })
    }
  }
}

chrome.runtime.onInstalled.addListener(() => {
  chrome.alarms.create(ALARM, { periodInMinutes: 15 })
  chrome.alarms.create(REMIND_ALARM, { periodInMinutes: 1 })
  refreshBadge()
})
chrome.alarms.onAlarm.addListener((a) => {
  if (a.name === ALARM) refreshBadge()
  if (a.name === REMIND_ALARM) checkReminders()
})

// 点击通知 → 打开网页端任务页
chrome.notifications.onClicked.addListener((id) => {
  chrome.notifications.clear(id)
  getBase().then((WEB) => chrome.tabs.create({ url: WEB + '/tasks' }))
})

// 旧桥接：网页端写入令牌后刷新角标
chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  if (msg && msg.type === 'SWB_TOKEN' && msg.token) {
    chrome.storage.local.set({ swb_token: msg.token }, () => {
      chrome.runtime.sendMessage({ type: 'SWB_TOKEN_SAVED' })
      refreshBadge()
      sendResponse({ ok: true })
    })
    return true
  }
  if (msg && msg.type === 'REFRESH_BADGE') {
    refreshBadge()
  }
})
