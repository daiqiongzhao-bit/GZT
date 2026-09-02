<template>
  <div class="dash">
    <!-- 自定义工具栏 -->
    <div class="dash-toolbar">
      <h3 class="section-title" style="margin:0">工作台总览</h3>
      <button class="btn ghost" :class="{ on: editMode }" @click="editMode = !editMode" v-html="icons.settings + ' 自定义'"></button>
    </div>

    <!-- 编辑模式：勾选可见 + 排序 -->
    <section v-if="editMode" class="panel edit-panel">
      <div class="ep-group">
        <div class="ep-title">统计卡片</div>
        <div v-for="id in statAll" :key="id" class="ep-row">
          <label class="ep-check"><input type="checkbox" :checked="pref.stats.includes(id)" @change="toggleCard('stats', id)" /> {{ statTitle(id) }}</label>
          <div class="ep-move">
            <button class="mini" :disabled="pref.stats.indexOf(id) <= 0" @click="moveCard('stats', id, -1)">↑</button>
            <button class="mini" :disabled="pref.stats.indexOf(id) >= pref.stats.length - 1" @click="moveCard('stats', id, 1)">↓</button>
          </div>
        </div>
      </div>
      <div class="ep-group">
        <div class="ep-title">详情面板</div>
        <div v-for="id in panelAll" :key="id" class="ep-row">
          <label class="ep-check"><input type="checkbox" :checked="pref.panels.includes(id)" @change="toggleCard('panels', id)" /> {{ PANELS[id] }}</label>
          <div class="ep-move">
            <button class="mini" :disabled="pref.panels.indexOf(id) <= 0" @click="moveCard('panels', id, -1)">↑</button>
            <button class="mini" :disabled="pref.panels.indexOf(id) >= pref.panels.length - 1" @click="moveCard('panels', id, 1)">↓</button>
          </div>
        </div>
      </div>
      <div class="ep-foot">
        <button class="btn ghost" @click="resetPref">恢复默认</button>
        <span class="section-sub">布局自动保存到本机</span>
      </div>
    </section>

    <!-- 统计卡片 -->
    <div class="stats">
      <div v-for="c in statCards" :key="c.id" class="stat glass">
        <div class="stat-ico" :class="c.tone" v-html="icons[c.icon]"></div>
        <div class="stat-body">
          <div class="stat-num" :class="{ danger: c.danger }">{{ c.num }}</div>
          <div class="stat-label">{{ c.label }}</div>
        </div>
      </div>
    </div>

    <!-- 详情面板 -->
    <div class="panels">
      <section v-for="p in panelCards" :key="p.id" class="panel">
        <!-- 今日当班 -->
        <template v-if="p.id === 'on_duty_list'">
          <h3 class="section-title">今日当班 <span class="section-sub">{{ dash.today }} · 共 {{ dutyRows.length }} 人</span></h3>
          <div v-if="dutyRows.length" class="table-wrap">
            <table class="task-table duty-table">
              <thead>
                <tr>
                  <th class="col-dept">部门</th>
                  <th class="col-name">姓名</th>
                  <th class="col-shift">班次</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(r, i) in dutyRows" :key="i">
                  <td class="col-dept"><span class="dept-tag">{{ r.dept_name || '未分配' }}</span></td>
                  <td class="col-name">{{ r.name }}</td>
                  <td class="col-shift"><span class="chip" :class="shiftClass(r.shift)">{{ r.shift }}</span></td>
                </tr>
              </tbody>
            </table>
          </div>
          <EmptyState v-else title="今日暂无排班" desc="还没有人为今天排班，去班表页排一班吧。" :icon="calIcon" />
        </template>

        <!-- 今日任务 -->
        <template v-else-if="p.id === 'today_tasks_list'">
          <h3 class="section-title">今日任务 <span class="section-sub">今日应办</span></h3>
          <div v-if="todayTaskList.length" class="table-wrap">
            <table class="task-table">
              <thead>
                <tr>
                  <th class="col-check"></th>
                  <th class="col-title">任务内容</th>
                  <th class="col-when">时间</th>
                  <th class="col-who">负责人</th>
                  <th class="col-prio">优先级</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="t in todayTaskList" :key="t.id" :class="{ done: t.status === 'done' }">
                  <td class="col-check">
                    <button class="check" :class="{ on: t.status === 'done' }" @click="toggle(t)" v-html="icons.check"></button>
                  </td>
                  <td class="col-title">
                    <div class="t-title-row">
                      <span class="t-title">{{ t.title }}</span>
                      <span v-if="t.overdue && t.status !== 'done'" class="chip danger">逾期</span>
                    </div>
                    <div v-if="t.note" class="t-note">{{ t.note }}</div>
                  </td>
                  <td class="col-when"><span class="when">{{ t.time || (t.deadline ? dlText(t.deadline) : '—') }}</span></td>
                  <td class="col-who">{{ t.assignee || '—' }}</td>
                  <td class="col-prio"><span class="chip" :class="prioClass(t.priority)">{{ prioText(t.priority) }}</span></td>
                </tr>
              </tbody>
            </table>
          </div>
          <EmptyState v-else title="今天没有待办任务 🎉" desc="今日任务已全部清空，好好享受吧。" :icon="checkIcon" />
        </template>

        <!-- 本月任务 -->
        <template v-else-if="p.id === 'month_tasks_list'">
          <div class="section-head">
            <h3 class="section-title">本月任务 <span class="section-sub">本月待办清单</span></h3>
            <div class="month-bar">
              <span class="mb-num">{{ monthDone }}/{{ dash.month_tasks }} 已完成</span>
              <div class="bar"><div class="bar-fill" :style="{ width: pct(monthDone, dash.month_tasks) }"></div></div>
            </div>
          </div>
          <div v-if="monthTaskList.length" class="table-wrap">
            <table class="task-table">
              <thead>
                <tr>
                  <th class="col-check"></th>
                  <th class="col-type">类型</th>
                  <th class="col-title">任务内容</th>
                  <th class="col-when">时间 / 截止</th>
                  <th class="col-who">负责人</th>
                  <th class="col-prio">优先级</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="t in monthTaskList" :key="t.id" :class="{ done: t.status === 'done' }">
                  <td class="col-check">
                    <button class="check" :class="{ on: t.status === 'done' }" @click="toggle(t)" v-html="icons.check"></button>
                  </td>
                  <td class="col-type"><span class="chip" :class="typeClass(t.type)">{{ typeText(t.type) }}</span></td>
                  <td class="col-title">
                    <div class="t-title-row">
                      <span class="t-title">{{ t.title }}</span>
                      <span v-if="t.overdue && t.status !== 'done'" class="chip danger">逾期</span>
                    </div>
                    <div v-if="t.note" class="t-note">{{ t.note }}</div>
                  </td>
                  <td class="col-when">
                    <span v-if="t.time" class="when">{{ t.time }}</span>
                    <span v-else-if="t.deadline" class="when">{{ dlText(t.deadline) }}</span>
                    <span v-else class="when faint">—</span>
                  </td>
                  <td class="col-who">{{ t.assignee || '—' }}</td>
                  <td class="col-prio"><span class="chip" :class="prioClass(t.priority)">{{ prioText(t.priority) }}</span></td>
                </tr>
              </tbody>
            </table>
          </div>
          <EmptyState v-else title="本月暂无待办任务" desc="还没有创建本月任务，去任务页新建一条吧。" :icon="calIcon" />
        </template>

        <!-- 当月任务（月度任务全量） -->
        <template v-else-if="p.id === 'monthly_tasks_list'">
          <div class="section-head">
            <h3 class="section-title">当月任务 <span class="section-sub">{{ monthLabel }} · 月度任务全量</span></h3>
            <div class="month-bar">
              <span class="mb-num">{{ monthlyDone }}/{{ monthlyList.length }} 已完成</span>
              <div class="bar"><div class="bar-fill" :style="{ width: pct(monthlyDone, monthlyList.length) }"></div></div>
            </div>
          </div>
          <div v-if="monthlyList.length" class="table-wrap">
            <table class="task-table">
              <thead>
                <tr>
                  <th class="col-check"></th>
                  <th v-if="auth.isSuper" class="col-dept">部门</th>
                  <th class="col-title">任务内容</th>
                  <th class="col-when">截止</th>
                  <th class="col-who">负责人</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="t in monthlyList" :key="t.id" :class="{ done: t.status === 'done' }">
                  <td class="col-check">
                    <button class="check" :class="{ on: t.status === 'done' }" @click="toggle(t)" v-html="icons.check"></button>
                  </td>
                  <td v-if="auth.isSuper" class="col-dept"><span class="dept-tag">{{ deptName(t.dept_id) }}</span></td>
                  <td class="col-title">
                    <div class="t-title-row">
                      <span class="t-title">{{ t.title }}</span>
                      <span v-if="t.overdue && t.status !== 'done'" class="chip danger">逾期</span>
                    </div>
                    <div v-if="t.note" class="t-note">{{ t.note }}</div>
                  </td>
                  <td class="col-when"><span class="when">{{ t.deadline ? dayText(t.deadline) : '—' }}</span></td>
                  <td class="col-who">{{ t.assignee || '—' }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <EmptyState v-else title="本月暂无月度任务" desc="去任务页新建「每月」类型的任务即可在此汇总。" :icon="calIcon" />
        </template>
      </section>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import * as api from '@/api'
import { icons } from '@/icons'
import { useAuthStore } from '@/store/auth'
import EmptyState from '@/components/EmptyState.vue'

const auth = useAuthStore()
const dash = ref({ on_duty_count: 0, today_tasks: 0, month_tasks: 0, monthly_tasks: 0, overdue_count: 0, on_duty: [], on_duty_rows: [], today: '', today_task_list: [], month_task_list: [], monthly_task_list: [] })
const schedules = ref([])
const departments = ref([])

const todayTaskList = computed(() => dash.value.today_task_list || [])
const monthTaskList = computed(() => dash.value.month_task_list || [])
const monthDone = computed(() => monthTaskList.value.filter((t) => t.status === 'done').length)
// 今日当班：部门 / 姓名 / 班次
const dutyRows = computed(() => dash.value.on_duty_rows || [])
// 当月任务 = 月度任务全量
const monthlyList = computed(() => dash.value.monthly_task_list || [])
const monthlyDone = computed(() => monthlyList.value.filter((t) => t.status === 'done').length)
const monthLabel = computed(() => {
  const d = new Date()
  return `${d.getFullYear()} 年 ${d.getMonth() + 1} 月`
})
function deptName(id) {
  const d = departments.value.find((x) => x.id === id)
  return d ? d.name : '—'
}
function dayText(s) {
  const m = String(s).match(/^(\d{4})-(\d{2})-(\d{2})/)
  return m ? `${Number(m[3])} 号` : s.replace('T', ' ').slice(0, 16)
}
function shiftClass(s) { return { '早班': 'accent', '中班': 'accent-3', '晚班': 'warn', '夜班': 'warn', '休息': '' }[s] || 'accent-3' }
const pct = (a, b) => (b ? Math.round((a / b) * 100) : 0) + '%'

const calIcon = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="17" rx="3"/><path d="M3 9h18M8 2v4M16 2v4M9 14l2 2 4-4"/></svg>'
const checkIcon = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>'

// 我的本月出勤天数（由班表推算）
const myWorkDays = computed(() => {
  const d = new Date()
  const prefix = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-`
  const me = auth.user?.name
  const set = new Set()
  for (const s of schedules.value) {
    if (!s.date || !s.date.startsWith(prefix)) continue
    if (s.shift === '休息') continue
    if (me && peopleOf(s).includes(me)) set.add(s.date)
  }
  return set.size
})

// ---- 可自定义布局 ----
const STORE_KEY = 'swb_dash_layout'
const statAll = ['on_duty', 'today_tasks', 'month_tasks', 'monthly_tasks', 'overdue', 'my_rate', 'my_work']
const panelAll = ['on_duty_list', 'today_tasks_list', 'month_tasks_list', 'monthly_tasks_list']
const PANELS = { on_duty_list: '今日当班', today_tasks_list: '今日任务', month_tasks_list: '本月任务', monthly_tasks_list: '当月任务' }
const defaultPref = () => ({ stats: [...statAll], panels: [...panelAll] })
const pref = ref(defaultPref())
const editMode = ref(false)

function statTitle(id) {
  return { on_duty: '今日当班人数', today_tasks: '今日待办任务', month_tasks: '本月待办任务', monthly_tasks: '当月任务', overdue: '逾期事项', my_rate: '本月完成率', my_work: '我的出勤(天)' }[id] || id
}
function loadPref() {
  try {
    const raw = localStorage.getItem(STORE_KEY)
    if (raw) {
      const p = JSON.parse(raw)
      if (Array.isArray(p.stats) && Array.isArray(p.panels)) {
        // 老版本保存的布局补齐本次新增的卡片，避免升级后新面板不显示
        for (const id of statAll) if (!p.stats.includes(id)) p.stats.push(id)
        for (const id of panelAll) if (!p.panels.includes(id)) p.panels.push(id)
        pref.value = p
        savePref()
      }
    }
  } catch {}
}
function savePref() { localStorage.setItem(STORE_KEY, JSON.stringify(pref.value)) }
function toggleCard(group, id) {
  const arr = pref.value[group]
  const i = arr.indexOf(id)
  if (i >= 0) arr.splice(i, 1)
  else arr.push(id)
  savePref()
}
function moveCard(group, id, dir) {
  const arr = pref.value[group]
  const i = arr.indexOf(id)
  if (i < 0) return
  const j = i + dir
  if (j < 0 || j >= arr.length) return
  arr.splice(i, 1)
  arr.splice(j, 0, id)
  savePref()
}
function resetPref() { pref.value = defaultPref(); savePref() }

const statCards = computed(() => {
  const d = dash.value
  const all = {
    on_duty: { id: 'on_duty', num: d.on_duty_count, label: '今日当班人数', icon: 'users', tone: 'on' },
    today_tasks: { id: 'today_tasks', num: d.today_tasks, label: '今日待办任务', icon: 'tasks', tone: 'task' },
    month_tasks: { id: 'month_tasks', num: d.month_tasks, label: '本月待办任务', icon: 'calendar', tone: 'month' },
    monthly_tasks: { id: 'monthly_tasks', num: monthlyList.value.length, label: '当月任务', icon: 'calendar', tone: 'month' },
    overdue: { id: 'overdue', num: d.overdue_count, label: '逾期事项', icon: 'alert', tone: 'warn', danger: d.overdue_count > 0 },
    my_rate: { id: 'my_rate', num: pct(monthDone.value, d.month_tasks), label: '本月完成率', icon: 'check', tone: 'on' },
    my_work: { id: 'my_work', num: myWorkDays.value, label: '我的出勤(天)', icon: 'users', tone: 'task' }
  }
  return pref.value.stats.filter((id) => all[id]).map((id) => all[id])
})
const panelCards = computed(() => pref.value.panels.filter((id) => PANELS[id]).map((id) => ({ id, title: PANELS[id] })))

function peopleOf(s) {
  try { return JSON.parse(s.people || '[]') } catch { return [] }
}
function typeText(t) { return { daily: '每日', monthly: '每月', once: '单次' }[t] || t }
function typeClass(t) { return { daily: 'accent', monthly: 'warn', once: '' }[t] || '' }
function prioClass(p) { return { high: 'danger', medium: 'warn', low: 'ok' }[p] || '' }
function prioText(p) { return { high: '高优', medium: '中优', low: '低优' }[p] || '中优' }
function dlText(s) { return s.replace('T', ' ').slice(0, 16) }

async function toggle(t) {
  const prev = t.status
  t.status = t.status === 'done' ? 'todo' : 'done'
  try {
    const updated = await api.post(`/tasks/${t.id}/toggle`)
    if (updated && updated.status) t.status = updated.status
  } catch { t.status = prev }
}

onMounted(async () => {
  loadPref()
  const [d, sc, deps] = await Promise.all([api.get('/dashboard'), api.get('/schedules'), api.get('/departments')])
  dash.value = d
  schedules.value = sc
  departments.value = deps
})
</script>

<style scoped>
.dash-toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; }
.dash-toolbar .btn.on { background: var(--accent-soft); color: var(--accent); border-color: rgba(79,70,229,0.4); }

.edit-panel { padding: 16px; margin-bottom: 16px; }
.ep-group { margin-bottom: 14px; }
.ep-title { font-size: 13px; font-weight: 700; color: var(--text-dim); margin-bottom: 8px; }
.ep-row { display: flex; align-items: center; justify-content: space-between; padding: 7px 0; border-bottom: 1px solid var(--hairline); }
.ep-check { display: flex; align-items: center; gap: 8px; font-size: 13.5px; cursor: pointer; }
.ep-check input { width: 16px; height: 16px; accent-color: var(--accent); }
.ep-move { display: flex; gap: 6px; }
.mini { width: 28px; height: 26px; border-radius: 8px; border: 1px solid var(--glass-border); background: var(--overlay); color: var(--text-dim); cursor: pointer; font-size: 13px; }
.mini:hover:not(:disabled) { color: var(--accent); border-color: var(--accent); }
.mini:disabled { opacity: 0.4; cursor: not-allowed; }
.ep-foot { display: flex; align-items: center; gap: 12px; margin-top: 6px; }

.stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 16px; margin-bottom: 16px; }
.stat { border-radius: var(--radius); padding: 18px 20px; display: flex; align-items: center; gap: 14px; }
.stat-ico { width: 44px; height: 44px; border-radius: 13px; display: grid; place-items: center; flex: none; }
.stat-ico svg { width: 22px; height: 22px; }
.stat-ico.on { background: var(--accent-soft); color: var(--accent); }
.stat-ico.task { background: rgba(14,165,233,0.15); color: var(--accent-2); }
.stat-ico.month { background: rgba(139,92,246,0.15); color: var(--accent-3); }
.stat-ico.warn { background: rgba(217,119,6,0.15); color: var(--warn); }
.stat-num { font-size: 28px; font-weight: 800; line-height: 1; }
.stat-num.danger { color: var(--danger); }
.stat-label { font-size: 12.5px; color: var(--text-dim); margin-top: 5px; }

.panels { display: grid; grid-template-columns: repeat(auto-fit, minmax(340px, 1fr)); gap: 16px; align-items: start; }
/* grid 子项默认 min-width:auto，宽表格会把整页撑出横向滚动条 */
.panels > .panel { min-width: 0; }
.table-wrap { max-width: 100%; }

.duty-table .col-dept { width: 96px; }
.duty-table .col-name { font-weight: 600; }
.duty-table .col-shift { width: 84px; }
.dept-tag { color: var(--text-dim); font-size: 12.5px; white-space: nowrap; }

.section-head { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 14px; flex-wrap: wrap; }
.month-bar { display: flex; align-items: center; gap: 10px; min-width: 200px; flex: 1; max-width: 320px; }
.mb-num { font-size: 12px; color: var(--text-dim); white-space: nowrap; }
.bar { flex: 1; height: 8px; border-radius: 999px; background: var(--overlay-2); overflow: hidden; }
.bar-fill { height: 100%; border-radius: 999px; background: linear-gradient(90deg, var(--accent-3), var(--accent-2)); transition: width 0.4s ease; }

.table-wrap { width: 100%; overflow-x: auto; }
.task-table { width: 100%; border-collapse: collapse; font-size: 13.5px; }
.task-table thead th {
  text-align: left; padding: 10px 14px; font-size: 11.5px; font-weight: 600;
  color: var(--text-faint); letter-spacing: 0.4px; border-bottom: 1px solid var(--glass-border); white-space: nowrap;
}
.task-table tbody td { padding: 12px 14px; border-bottom: 1px solid var(--hairline); vertical-align: middle; }
.task-table tbody tr { transition: background 0.15s; }
.task-table tbody tr:hover { background: var(--overlay); }
.task-table tbody tr:last-child td { border-bottom: none; }
.task-table tbody tr.done { opacity: 0.45; }
.task-table tbody tr.done .t-title { text-decoration: line-through; }
.col-check { width: 44px; }
.check { width: 26px; height: 26px; border-radius: 8px; cursor: pointer; border: 1px solid var(--glass-border); background: transparent; color: var(--accent); display: grid; place-items: center; }
.check svg { width: 16px; height: 16px; opacity: 0; }
.check.on { background: var(--accent); color: #fff; }
.check.on svg { opacity: 1; }
.col-title { min-width: 180px; }
.t-title-row { display: flex; align-items: center; gap: 8px; }
.t-title { font-weight: 600; }
.t-note { font-size: 12px; color: var(--text-faint); margin-top: 4px; }
.col-when .when { color: var(--text-dim); white-space: nowrap; }
.col-when .when.faint { color: var(--text-faint); }
.col-who { color: var(--text-dim); white-space: nowrap; }

@media (max-width: 820px) {
  .col-type, .col-prio, .col-who { display: none; }
  .panels { grid-template-columns: 1fr; }
  .duty-table .col-dept { width: auto; }
}
</style>
