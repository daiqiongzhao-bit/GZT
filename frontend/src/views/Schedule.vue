<template>
  <div class="sched">
    <div class="head-row">
      <h3 class="section-title" style="margin:0">班表展示 <span class="section-sub">{{ deptName }}</span></h3>
      <div class="head-actions">
        <select v-if="auth.isSuper" v-model="importDeptId" class="glass-input dept-filter" :class="{ 'req-miss': !importDeptId }" style="max-width:170px;padding:8px 10px;" title="导入到哪个部门（必选）">
          <option :value="0">— 请选择部门 —</option>
          <option v-for="d in deptOptions(departments)" :key="d.id" :value="d.id">{{ indentOf(d.depth) + d.name }}</option>
        </select>
        <button v-if="auth.canManage" class="btn ghost" :disabled="sImporting" @click="$refs.schedFile.click()">{{ sImporting ? '导入中…' : '⬆ 导入班表' }}</button>
        <button v-if="auth.canManage" class="btn ghost" :disabled="sExporting" @click="exportSchedules">{{ sExporting ? '导出中…' : '⬇ 导出' }}</button>
        <button v-if="auth.canManage" class="btn primary" @click="openAdd(null)" v-html="icons.plus + ' 新增排班'"></button>
        <input ref="schedFile" type="file" accept=".xlsx,.csv" style="display:none" @change="uploadScheduleCSV" />
      </div>
    </div>

    <!-- 筛选 -->
    <section class="panel filter-bar">
      <div class="fg3">
        <div>
          <label class="fld">人员</label>
          <select v-model="selectedPerson" class="glass-input">
            <option value="__me__">我的（{{ auth.user.name }}）</option>
            <option v-if="auth.canManage" value="__all__">全部（部门）</option>
            <option v-for="u in users" :key="u.id" :value="u.name">{{ u.name }}</option>
          </select>
        </div>
        <div>
          <label class="fld">年份</label>
          <select v-model.number="viewYear" class="glass-input">
            <option v-for="y in yearOptions" :key="y" :value="y">{{ y }} 年</option>
          </select>
        </div>
        <div>
          <label class="fld">月份</label>
          <select v-model.number="viewMonth" class="glass-input">
            <option v-for="(_, i) in 12" :key="i" :value="i">{{ i + 1 }} 月</option>
          </select>
        </div>
        <div class="actions">
          <button class="btn ghost" @click="goToday">今天</button>
          <button class="btn ghost nav-btn" @click="prevMonth" v-html="icons.chevronLeft"></button>
          <button class="btn ghost nav-btn" @click="nextMonth" v-html="icons.chevronRight"></button>
        </div>
      </div>
    </section>

    <!-- 人员卡片 + 统计 -->
    <section class="panel person-card">
      <div class="pc-left">
        <div class="avatar" :class="{'me': selectedPerson==='__me__'}">{{ personInitial }}</div>
        <div class="pc-info">
          <div class="pc-name">
            {{ personLabel }}
            <span v-if="selectedPerson==='__me__'" class="chip accent">我</span>
          </div>
          <div class="pc-sub">{{ deptName }} · 当月共 {{ stats.totalDays }} 天</div>
        </div>
      </div>
      <div class="pc-stats">
        <div class="pcs work"><b>{{ stats.workDays }}</b><span>出勤</span></div>
        <div class="pcs morning"><b>{{ stats.morning }}</b><span>早班</span></div>
        <div class="pcs mid"><b>{{ stats.mid }}</b><span>中班</span></div>
        <div class="pcs evening"><b>{{ stats.evening }}</b><span>晚班</span></div>
        <div class="pcs rest"><b>{{ stats.rest }}</b><span>休息</span></div>
      </div>
    </section>

    <!-- 班次拖拽面板（仅管理端） -->
    <section v-if="auth.canManage" class="panel shift-palette">
      <span class="sp-label">拖拽班次到日期：</span>
      <div class="sp-chips">
        <span v-for="s in deptShifts" :key="s" class="sp-chip" :class="shiftClass(s)" draggable="true" @dragstart="onDragShift($event, s)">{{ shiftTime(s) }}</span>
        <span class="sp-chip rest" draggable="true" @dragstart="onDragShift($event, '休息')">休息</span>
      </div>
      <span class="sp-hint">提示：把已有排班拖到其它日期可跨日移动</span>
    </section>

    <!-- 空状态引导 -->
    <EmptyState
      v-if="monthItems.length === 0"
      :title="auth.canManage ? '本月还没有排班' : '本月暂无你的排班'"
      :desc="auth.canManage ? '点击右上角「新增排班」，或直接把上方班次拖到日历日期即可快速排班。' : '联系部门管理员为你排班，或切换到「全部」查看部门排班。'"
      :action-text="auth.canManage ? '新增排班' : ''"
      :icon="calIcon"
      @action="openAdd(null)"
    />

    <!-- 日历网格 -->
    <section class="panel cal-wrap">
      <div class="cal-grid cal-weekdays">
        <div v-for="w in weekdays" :key="w" class="wk" :class="{ weekend: w === '六' || w === '日' }">{{ w }}</div>
      </div>
      <div class="cal-grid cal-days">
        <div
          v-for="cell in cells"
          :key="cell.key"
          class="day"
          :class="{ out: !cell.inMonth, today: cell.isToday, sel: cell.key === selectedKey, drop: dragOverKey === cell.key }"
          @click="onDayClick(cell)"
          @dragover.prevent="onDayDragOver(cell)"
          @dragleave="onDayDragLeave(cell)"
          @drop.prevent="onDrop($event, cell)"
        >
          <div class="day-top">
            <span class="day-num">{{ cell.day }}</span>
            <span v-if="auth.canManage && cell.inMonth" class="day-add" title="新增排班" @click.stop="openAdd(cell.date)">+</span>
          </div>
          <div class="day-shifts">
            <div v-for="s in cell.items" :key="s.id" class="sched-item" :class="'si-' + shiftKey(s.shift)" :draggable="auth.canManage" @dragstart="onDragItem($event, s)">
              <span class="chip" :class="shiftClass(s.shift)">{{ shiftShort(s.shift) }}</span>
              <span v-if="auth.isSuper && deptNameOf(s)" class="chip-dept">{{ deptNameOf(s) }}</span>
              <button v-if="canDel" class="del" @click.stop="remove(s)" title="删除">×</button>
            </div>
            <div v-if="!cell.items.length && cell.inMonth" class="no-shift">
              {{ cell.isRest ? '休' : '—' }}
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- 内联新增表单 -->
    <section v-if="showAdd && auth.canManage" class="panel add-form">
      <div class="form-title">
        <span v-html="icons.plus"></span>
        <span>新增排班 · {{ form.date }}<template v-if="auth.isSuper && formDeptName"> · {{ formDeptName }}</template></span>
        <button class="btn ghost form-close" @click="showAdd = false">收起</button>
      </div>
      <div class="form-grid">
        <div>
          <label class="fld">日期</label>
          <input v-model="form.date" type="date" class="glass-input" />
        </div>
        <div>
          <label class="fld">班次</label>
          <select v-model="form.shift" class="glass-input">
            <option v-for="s in deptShifts" :key="s" :value="s">{{ s }}</option>
            <option value="休息">休息</option>
          </select>
        </div>
        <div>
          <label class="fld">所属部门</label>
          <select v-model="form.dept_id" class="glass-input" :disabled="!auth.isSuper">
            <option v-if="auth.isSuper" v-for="d in deptOptions(departments)" :key="d.id" :value="d.id">{{ indentOf(d.depth) + d.name }}</option>
            <option v-else :value="auth.user.dept_id">{{ auth.user.dept?.name || '本部门' }}</option>
          </select>
        </div>
      </div>
      <label class="fld" style="margin-top:14px">当班人员</label>
      <div class="people-pick">
        <button v-for="u in users" :key="u.id" class="person" :class="{ sel: form.people.includes(u.name) }" @click="togglePerson(u.name)">
          {{ u.name }}
        </button>
        <span v-if="!users.length" class="section-sub">暂无人员，请先在设置中添加</span>
      </div>
      <div class="form-actions">
        <button class="btn ghost" @click="showAdd = false">取消</button>
        <button class="btn primary" :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存排班' }}</button>
      </div>
    </section>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, watch } from 'vue'
import * as api from '@/api'
import { icons } from '@/icons'
import { useAuthStore } from '@/store/auth'
import { deptOptions, indentOf } from '@/utils/dept'
import EmptyState from '@/components/EmptyState.vue'

const auth = useAuthStore()
const schedules = ref([])
const users = ref([])
const departments = ref([])
const showAdd = ref(false)
const saving = ref(false)
const shiftConfigs = ref([])
// 班次 + 时间段（拖拽面板；按当前所选部门匹配，匹配不到回退到任意部门配置）
function shiftTime(s) {
  if (s === '休息') return '休息'
  const did = form.dept_id || auth.user.dept_id
  const sc = shiftConfigs.value.find((x) => x.dept_id === did && x.name === s) || shiftConfigs.value.find((x) => x.name === s)
  return sc ? `${s} ${sc.start_time}-${sc.end_time}` : s
}

// 日历格子单字简称：早/中/晚/夜/休（完整班次名与时段只在拖拽面板展示）
function shiftShort(s) {
  return { '早班': '早', '中班': '中', '晚班': '晚', '夜班': '夜', '休息': '休' }[s] || s
}

// 日历格子只显示班次名（时段只在拖拽面板上展示，避免格子里文字过密）
function shiftTimeOf(item) {
  if (item.shift === '休息') return '休息'
  return item.shift
}

const weekdays = ['一', '二', '三', '四', '五', '六', '日']

const form = reactive({ date: '', shift: '早班', people: [], dept_id: null })
// 实际提交的部门：超管用表单选择值，部门管理员锁定为本部门（后端也会强制覆盖）
const submitDeptId = computed(() => Number(form.dept_id) || auth.user.dept_id)
const formDeptName = computed(() => {
  const d = departments.value.find((x) => x.id === submitDeptId.value)
  return d ? d.name : (auth.user.dept?.name || '')
})
// 班次列表：按当前所选部门过滤，未匹配到时回退为全部（避免旧数据下拉为空）
const deptShifts = computed(() => {
  const list = shiftConfigs.value.filter((sc) => sc.dept_id === submitDeptId.value).map((sc) => sc.name)
  const set = new Set(['早班', '中班', '晚班', '夜班'])
  for (const n of list) set.add(n)
  return [...set]
})

const now = new Date()
const viewYear = ref(now.getFullYear())
const viewMonth = ref(now.getMonth()) // 0-based
const yearOptions = computed(() => {
  const arr = []
  for (let y = viewYear.value - 3; y <= viewYear.value + 1; y++) arr.push(y)
  return arr
})

// 人员筛选：默认"我的"
const selectedPerson = ref('__me__')

// 删除按钮仅在「全部」视图下显示
const canDel = computed(() => auth.canManage && selectedPerson.value === '__all__')

const todayKey = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
const selectedKey = ref(null)

// 拖拽状态
const dragOverKey = ref(null)
const dragData = ref(null)
const calIcon =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="17" rx="3"/><path d="M3 9h18M8 2v4M16 2v4M9 14l2 2 4-4"/></svg>'

function todayStr(d = new Date()) { return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}` }
function peopleOf(s) { try { return JSON.parse(s.people || '[]') } catch { return [] } }
function togglePerson(name) {
  const i = form.people.indexOf(name)
  if (i >= 0) form.people.splice(i, 1)
  else form.people.push(name)
}

function shiftKey(s) { return { '早班': 'morning', '中班': 'mid', '晚班': 'evening', '夜班': 'night' }[s] || 'other' }
function shiftClass(s) {
  return { '早班': 'accent', '中班': 'ok', '晚班': 'warn', '夜班': 'accent-3', '休息': 'rest' }[s] || ''
}

// 当前月份的所有班次
const monthItems = computed(() => {
  const y = viewYear.value, m = viewMonth.value
  const prefix = `${y}-${String(m + 1).padStart(2, '0')}-`
  return schedules.value.filter(s => s.date && s.date.startsWith(prefix))
})

// 判断一个班次是否对当前筛选人员显示
function matchesFilter(s) {
  if (selectedPerson.value === '__all__') return true
  const target = selectedPerson.value === '__me__' ? auth.user.name : selectedPerson.value
  return peopleOf(s).includes(target)
}

// 按日期聚合（仅包含筛选命中的班次）
const cells = computed(() => {
  const y = viewYear.value, m = viewMonth.value
  const first = new Date(y, m, 1)
  const startOffset = (first.getDay() + 6) % 7
  const daysInMonth = new Date(y, m + 1, 0).getDate()
  const total = startOffset + daysInMonth
  const trailing = (7 - (total % 7)) % 7
  const count = total + trailing

  const byDate = {}
  for (const s of monthItems.value) {
    if (!matchesFilter(s)) continue
    (byDate[s.date] ||= []).push(s)
  }

  const out = []
  for (let i = 0; i < count; i++) {
    const d = new Date(y, m, 1 - startOffset + i)
    const y2 = d.getFullYear(), m2 = d.getMonth(), day = d.getDate()
    const key = `${y2}-${String(m2 + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`
    out.push({
      key,
      date: key,
      day,
      inMonth: m2 === m,
      isToday: key === todayKey,
      items: byDate[key] || [],
      isRest: !byDate[key] && (d.getDay() === 0 || d.getDay() === 6) && m2 === m
    })
  }
  return out
})

// 统计
const stats = computed(() => {
  const items = monthItems.value.filter(matchesFilter)
  const y = viewYear.value, m = viewMonth.value
  const totalDays = new Date(y, m + 1, 0).getDate()
  const result = { totalDays, workDays: 0, morning: 0, mid: 0, evening: 0, rest: 0 }
  for (const s of items) {
    if (s.shift === '休息') { result.rest++; continue }
    result.workDays++
    if (s.shift === '早班') result.morning++
    else if (s.shift === '中班') result.mid++
    else if (s.shift === '晚班') result.evening++
    else if (s.shift === '夜班') result.morning++ // 夜班归到早班类目
  }
  // 出勤外的天数 = 总天数 - 出勤（部门内排班覆盖的天视为出勤）
  const workDates = new Set(items.filter(s => s.shift !== '休息').map(s => s.date))
  result.rest = totalDays - workDates.size
  return result
})

const personLabel = computed(() => {
  if (selectedPerson.value === '__me__') return auth.user.name
  if (selectedPerson.value === '__all__') return '部门全员'
  return selectedPerson.value
})
const personInitial = computed(() => (personLabel.value || '?')[0])
const deptName = computed(() => (auth.isSuper ? '全部部门' : (auth.user?.dept?.name || '')))
function deptNameOf(s) {
  const d = departments.value.find((x) => x.id === s.dept_id)
  return d ? d.name : ''
}

function prevMonth() {
  if (viewMonth.value === 0) { viewMonth.value = 11; viewYear.value-- } else viewMonth.value--
  selectedKey.value = null
}
function nextMonth() {
  if (viewMonth.value === 11) { viewMonth.value = 0; viewYear.value++ } else viewMonth.value++
  selectedKey.value = null
}
function goToday() {
  viewYear.value = now.getFullYear()
  viewMonth.value = now.getMonth()
  selectedKey.value = null
}

function onDayClick(cell) {
  selectedKey.value = cell.key
  if (auth.canManage && cell.inMonth && selectedPerson.value === '__all__') openAdd(cell.date)
}

// ---- 拖拽排班 ----
function onDragShift(e, shift) {
  dragData.value = { kind: 'shift', shift }
  e.dataTransfer.effectAllowed = 'copy'
  e.dataTransfer.setData('text/plain', JSON.stringify(dragData.value))
}
function onDragItem(e, s) {
  dragData.value = { kind: 'move', id: s.id }
  e.dataTransfer.effectAllowed = 'move'
  e.dataTransfer.setData('text/plain', JSON.stringify(dragData.value))
}
function onDayDragOver(cell) {
  if (!cell.inMonth || !auth.canManage) return
  dragOverKey.value = cell.key
}
function onDayDragLeave(cell) {
  if (dragOverKey.value === cell.key) dragOverKey.value = null
}
async function onDrop(e, cell) {
  dragOverKey.value = null
  if (!cell.inMonth || !auth.canManage) return
  let data = dragData.value
  const raw = e.dataTransfer.getData('text/plain')
  if (raw) { try { data = JSON.parse(raw) } catch { /* 保持 dragData */ } }
  if (!data) return
  try {
    if (data.kind === 'move') {
      const item = schedules.value.find((s) => s.id === data.id)
      if (!item || item.date === cell.key) return
      await api.put(`/schedules/${data.id}`, { date: cell.key, shift: item.shift, people: peopleOf(item) })
      await load()
    } else if (data.kind === 'shift') {
      let people = []
      if (selectedPerson.value === '__me__') people = [auth.user.name]
      else if (selectedPerson.value !== '__all__') people = [selectedPerson.value]
      if (people.length) {
        await api.post('/schedules', { date: cell.key, shift: data.shift, people, dept_id: submitDeptId.value })
        await load()
      } else {
        // 全部视图：打开表单让用户选择人员
        openAdd(cell.key)
        form.shift = data.shift
      }
    }
  } catch (err) {
    alert(err.response?.data?.error || '排班失败')
  } finally {
    dragData.value = null
  }
}

function openAdd(date) {
  form.date = date || todayStr()
  form.shift = '早班'
  // 部门：超管默认本部门、之后保持上次选择（方便连续给同一部门排班），部门管理员锁定本部门
  if (!auth.isSuper) form.dept_id = auth.user.dept_id
  else if (!form.dept_id) form.dept_id = auth.user.dept_id || departments.value[0]?.id || null
  // 若当前查看的是某个人，预选该人
  if (selectedPerson.value && selectedPerson.value !== '__all__' && selectedPerson.value !== '__me__') {
    form.people = [selectedPerson.value]
  } else if (selectedPerson.value === '__me__') {
    form.people = [auth.user.name]
  } else {
    form.people = []
  }
  showAdd.value = true
}

async function save() {
  if (!form.date || !form.shift || !form.people.length) { alert('日期、班次、人员均必填'); return }
  saving.value = true
  try {
    await api.post('/schedules', { date: form.date, shift: form.shift, people: [...form.people], dept_id: submitDeptId.value })
    showAdd.value = false
    form.people = []
    await load()
  } catch (e) { alert(e.response?.data?.error || '保存失败') }
  finally { saving.value = false }
}

async function remove(s) {
  if (!confirm('确定删除该排班？')) return
  try { await api.del(`/schedules/${s.id}`); await load() } catch (e) { alert(e.response?.data?.error || '删除失败') }
}

// 班表导入：必须显式选择目标部门（不设默认值，避免沿用账号所属部门而导错）
const importDeptId = ref(0)
const sImporting = ref(false)
async function uploadScheduleCSV(e) {
  const file = e.target.files && e.target.files[0]
  if (!file) return
  const target = importDeptId.value
  if (!target) { alert('请先在「导入到部门」里选择目标部门，再上传文件。'); e.target.value = ''; return }
  const dept = departments.value.find((x) => x.id === target)
  if (!confirm(`将把「${file.name}」导入到部门「${dept ? dept.name : target}」，确认？`)) { e.target.value = ''; return }
  sImporting.value = true
  try {
    const r = await api.upload('/schedules/import', file, 'file', { dept_id: target })
    let msg = `导入完成：成功 ${r.created || 0} 条，失败 ${r.failed || 0} 条`
    if (r.unknown_names && r.unknown_names.length) {
      msg += `\n⚠ 无账号人员（不会收到推送/@）：${r.unknown_names.join('、')}`
    }
    if (r.errors && r.errors.length) msg += '\n' + r.errors.slice(0, 5).join('\n')
    alert(msg)
    await load()
  } catch (err) { alert(err.response?.data?.error || '导入失败') }
  finally { sImporting.value = false; e.target.value = '' }
}

// 导出班表 CSV（鉴权下载）
const sExporting = ref(false)
async function exportSchedules() {
  sExporting.value = true
  try {
    const token = localStorage.getItem('sw_token')
    const r = await fetch('/api/schedules/export', { headers: { Authorization: 'Bearer ' + token } })
    if (!r.ok) { alert('导出失败'); return }
    const blob = await r.blob()
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = 'schedules_export.csv'
    a.click()
    URL.revokeObjectURL(a.href)
  } catch { alert('导出失败') }
  finally { sExporting.value = false }
}

async function load() {
  const [sc, us, sfs, deps] = await Promise.all([
    api.get('/schedules'),
    api.get('/users'),
    api.get('/shift-configs').catch(() => []),
    api.get('/departments').catch(() => [])
  ])
  schedules.value = sc
  users.value = us
  shiftConfigs.value = sfs
  departments.value = deps || []
  if (!form.dept_id) form.dept_id = auth.user.dept_id || departments.value[0]?.id || null
}

onMounted(load)
</script>

<style scoped>
.head-row { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; }
.head-actions { display: flex; gap: 8px; align-items: center; }

/* 导入到部门：必选，未选红框提示（与任务导入一致） */
.dept-filter.req-miss { border-color: #e11d48; box-shadow: 0 0 0 2px rgba(225, 29, 72, 0.12); }

/* 筛选条 */
.filter-bar { padding: 12px 14px; margin-bottom: 12px; }
.fg3 { display: grid; grid-template-columns: 1.5fr 0.8fr 0.8fr auto; gap: 10px; align-items: end; }
.fg3 .actions { display: flex; gap: 6px; }
.fg3 .nav-btn { padding: 6px 10px; }
.fg3 .nav-btn :deep(svg) { width: 16px; height: 16px; }

/* 人员卡片 */
.person-card { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 14px 16px; margin-bottom: 12px; flex-wrap: wrap; }
.pc-left { display: flex; align-items: center; gap: 12px; min-width: 200px; }
.pc-info .pc-name { font-size: 15px; font-weight: 700; display: flex; align-items: center; gap: 8px; }
.pc-info .pc-sub { font-size: 12px; color: var(--text-faint); margin-top: 3px; }
.pc-stats { display: flex; gap: 10px; flex-wrap: wrap; }
.pcs { background: var(--overlay); border: 1px solid var(--glass-border); border-radius: 11px; padding: 8px 12px; text-align: center; min-width: 60px; position: relative; overflow: hidden; }
.pcs::before { content: ''; position: absolute; left: 0; top: 0; bottom: 0; width: 3px; }
.pcs.work::before { background: var(--accent); }
.pcs.morning::before { background: var(--accent-2); }
.pcs.mid::before { background: var(--ok); }
.pcs.evening::before { background: var(--warn); }
.pcs.rest::before { background: var(--muted); }
.pcs b { display: block; font-size: 18px; font-weight: 800; }
.pcs span { font-size: 11px; color: var(--text-dim); }
.avatar { width: 44px; height: 44px; border-radius: 12px; display: grid; place-items: center; background: var(--brand-grad); color: #fff; font-weight: 800; font-size: 17px; }
.avatar.me { box-shadow: 0 0 0 2px var(--accent); }

.cal-head { display: flex; align-items: center; gap: 14px; margin-bottom: 12px; padding: 10px 14px; }
.nav-btn { padding: 6px 10px; }
.nav-btn :deep(svg) { width: 16px; height: 16px; }
.cur-month { display: flex; align-items: baseline; gap: 8px; margin: 0 auto; }
.cm-year { font-size: 13px; color: var(--text-dim); }
.cm-month { font-size: 19px; font-weight: 800; letter-spacing: 0.5px; }
.today-btn { padding: 6px 12px; font-size: 12.5px; }

.cal-wrap { padding: 10px; }
.cal-grid { display: grid; grid-template-columns: repeat(7, 1fr); gap: 4px; }
.cal-weekdays { margin-bottom: 4px; }
.wk { text-align: center; font-size: 11px; color: var(--text-faint); padding: 3px 0; }
.wk.weekend { color: var(--accent-3); }

.cal-days { gap: 4px; }
.day {
  min-height: 58px; border-radius: var(--radius-sm);
  background: var(--overlay); border: 1px solid var(--glass-border);
  padding: 4px 5px; display: flex; flex-direction: column; gap: 3px; cursor: pointer;
  transition: 0.15s; overflow: hidden;
}
.day:hover { border-color: var(--glass-border-strong); }
.day.out { opacity: 0.4; }
.day.today { border-color: rgba(79,70,229,0.5); box-shadow: 0 0 0 1px rgba(79,70,229,0.25) inset; }
.day.sel { border-color: var(--accent); box-shadow: 0 0 0 2px rgba(79,70,229,0.35); }

.day-top { display: flex; align-items: center; justify-content: space-between; }
.day-num { font-size: 11.5px; font-weight: 700; color: var(--text); }
.day-add {
  width: 16px; height: 16px; border-radius: 5px; display: grid; place-items: center;
  font-size: 12px; line-height: 1; color: var(--text-faint);
  border: 1px solid var(--glass-border); background: transparent; cursor: pointer;
}
.day-add:hover { color: var(--accent); border-color: rgba(79,70,229,0.4); }
.day.drop { border-color: var(--accent); background: var(--accent-soft); box-shadow: 0 0 0 2px rgba(79,70,229,0.35); }

/* 班次拖拽面板 */
.shift-palette { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; padding: 12px 14px; margin-bottom: 12px; }
.sp-label { font-size: 13px; color: var(--text-dim); white-space: nowrap; }
.sp-chips { display: flex; gap: 8px; flex-wrap: wrap; }
.sp-chip {
  font-size: 12px; padding: 5px 12px; border-radius: 999px; cursor: grab; user-select: none;
  border: 1px solid var(--glass-border); background: var(--overlay); color: var(--text-dim);
  transition: 0.15s;
}
.sp-chip:hover { border-color: var(--accent); color: var(--accent); }
.sp-chip:active { cursor: grabbing; }
.sp-chip.accent { background: var(--accent-soft); color: var(--accent); border-color: rgba(79,70,229,0.4); }
.sp-chip.ok { background: rgba(13,148,136,0.12); color: var(--ok); border-color: rgba(13,148,136,0.35); }
.sp-chip.warn { background: rgba(217,119,6,0.12); color: var(--warn); border-color: rgba(217,119,6,0.35); }
.sp-chip.accent-3 { background: rgba(139,92,246,0.12); color: var(--accent-3); border-color: rgba(139,92,246,0.35); }
.sp-chip.rest { background: rgba(107,114,128,0.14); color: var(--muted); border-color: rgba(107,114,128,0.35); }
.sp-hint { font-size: 12px; color: var(--text-faint); margin-left: auto; }
.sched-item[draggable="true"] { cursor: grab; }
.sched-item[draggable="true"]:active { cursor: grabbing; }

.day-shifts { display: flex; flex-direction: column; gap: 2px; flex: 1; overflow: hidden; }
.no-shift { font-size: 10px; color: var(--text-faint); text-align: center; line-height: 16px; }
.no-shift:first-letter { }

.sched-item {
  display: flex; align-items: center; gap: 4px; padding: 2px 5px;
  border-radius: 6px; background: var(--overlay); border: 1px solid var(--glass-border);
  border-left: 3px solid var(--glass-border); overflow: hidden;
}
.sched-item.si-morning { border-left-color: var(--accent); }
.sched-item.si-mid { border-left-color: var(--ok); }
.sched-item.si-evening { border-left-color: var(--warn); }
.sched-item.si-night { border-left-color: var(--accent-3); }
.sched-item .chip { font-size: 9.5px; padding: 1px 5px; flex: none; }
.sched-item .chip.accent-3 { background: rgba(139,92,246,0.12); border-color: rgba(139,92,246,0.35); color: var(--accent-3); }
.sched-item .chip.rest { background: rgba(5,150,105,0.12); border-color: rgba(5,150,105,0.35); color: var(--ok); }
.chip-dept {
  font-size: 9px; line-height: 1.4; padding: 0 4px; border-radius: 4px; flex: none;
  background: rgba(127,127,127,0.12); color: var(--text-faint); white-space: nowrap;
  overflow: hidden; text-overflow: ellipsis; max-width: 60px;
}
.del {
  width: 14px; height: 14px; border-radius: 4px; border: 1px solid var(--glass-border);
  background: transparent; color: var(--text-faint); cursor: pointer; font-size: 11px; line-height: 1; flex: none;
}
.del:hover { color: var(--danger); border-color: rgba(225,29,72,0.4); }

.add-form { margin-top: 16px; }
.form-title { display: flex; align-items: center; gap: 8px; font-size: 14px; font-weight: 700; margin-bottom: 14px; }
.form-title :deep(svg) { width: 16px; height: 16px; color: var(--accent); }
.form-close { margin-left: auto; padding: 6px 12px; }
.form-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 14px; }
.people-pick { display: flex; flex-wrap: wrap; gap: 8px; }
.person {
  padding: 7px 14px; border-radius: 999px; cursor: pointer; font-size: 13px;
  background: var(--glass); border: 1px solid var(--glass-border); color: var(--text-dim);
  transition: 0.15s;
}
.person.sel { background: rgba(79,70,229,0.16); border-color: rgba(79,70,229,0.4); color: var(--accent); }
.form-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 16px; }

@media (max-width: 820px) {
  .fg3 { grid-template-columns: 1fr 1fr; }
  .fg3 .actions { grid-column: span 2; justify-content: flex-end; }
  .pc-stats { width: 100%; justify-content: space-between; }
  .pcs { flex: 1; min-width: 0; }
  .cal-grid { gap: 3px; }
  .day { min-height: 48px; padding: 3px 4px; }
  .day-num { font-size: 10.5px; }
  .sched-item { padding: 1px 4px; }
  .day-add { width: 14px; height: 14px; font-size: 11px; }
  .form-grid { grid-template-columns: 1fr; }
}
</style>