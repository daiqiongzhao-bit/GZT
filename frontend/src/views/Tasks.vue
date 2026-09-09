<template>
  <div class="tasks">
    <!-- 概览：今日 / 本月 / 全部 总览 -->
    <section class="panel overview">
      <button class="ov-card" :class="{ active: tab === 'today' }" @click="tab = 'today'">
        <div class="ov-head">
          <span class="ov-label">今日任务</span>
          <span class="ov-num">{{ todayPending }}<em>待办</em></span>
        </div>
        <div class="bar"><div class="bar-fill accent" :style="{ width: pct(todayDone, todayAll.length) }"></div></div>
        <div class="ov-sub">
          <span>已完成 {{ todayDone }}/{{ todayAll.length }}</span>
          <span v-if="todayOverdue" class="ov-od">逾期 {{ todayOverdue }}</span>
          <span v-else class="ov-faint">共 {{ todayAll.length }} 项</span>
        </div>
      </button>
      <button class="ov-card" :class="{ active: tab === 'month' }" @click="tab = 'month'">
        <div class="ov-head">
          <span class="ov-label">本月任务</span>
          <span class="ov-num">{{ monthPending }}<em>待办</em></span>
        </div>
        <div class="bar"><div class="bar-fill violet" :style="{ width: pct(monthDone, monthAll.length) }"></div></div>
        <div class="ov-sub">
          <span>已完成 {{ monthDone }}/{{ monthAll.length }}</span>
          <span class="ov-faint">共 {{ monthAll.length }} 项</span>
        </div>
      </button>
      <button class="ov-card" :class="{ active: tab === 'all' }" @click="tab = 'all'">
        <div class="ov-head">
          <span class="ov-label">所有任务</span>
          <span class="ov-num">{{ allPending }}<em>未完成</em></span>
        </div>
        <div class="bar"><div class="bar-fill slate" :style="{ width: pct(allDone, tasks.length) }"></div></div>
        <div class="ov-sub">
          <span>已完成 {{ allDone }}/{{ tasks.length }}</span>
          <span v-if="allOverdue" class="ov-od">逾期 {{ allOverdue }}</span>
          <span v-else class="ov-faint">共 {{ tasks.length }} 项</span>
        </div>
      </button>
    </section>

    <!-- 周期重置说明：每周任务每天 00:00 重置、每月任务每月 1 日重置。
         不加说明的话，用户看到昨天做过的任务又变回待办，会以为数据丢了 -->
    <p class="reset-tip">
      <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"
        stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M21 12a9 9 0 1 1-3.5-7.1" />
        <path d="M21 3v5h-5" />
      </svg>
      周期任务每天 00:00 自动重置为待办，每月任务每月 1 日自动重置并把截止日推进到当月；
      历史完成记录仍在「完成记录」中保留，不会丢失
    </p>

    <div class="head-row">
      <div class="tabs">
        <button class="tab" :class="{ active: tab === 'today' }" @click="tab = 'today'">今日 <i v-if="counts.today">{{ counts.today }}</i></button>
        <button class="tab" :class="{ active: tab === 'month' }" @click="tab = 'month'">本月 <i v-if="counts.month">{{ counts.month }}</i></button>
        <button class="tab" :class="{ active: tab === 'overdue' }" @click="tab = 'overdue'">逾期 <i v-if="counts.overdue" class="bad">{{ counts.overdue }}</i></button>
        <button class="tab" :class="{ active: tab === 'all' }" @click="tab = 'all'">全部 <i>{{ tasks.length }}</i></button>
      </div>
      <div class="head-actions">
        <select v-if="auth.isSuper" v-model="filterDept" class="glass-input dept-filter" title="按部门筛选">
          <option :value="0">全部部门</option>
          <option v-for="d in deptOptions(departments)" :key="d.id" :value="d.id">{{ indentOf(d.depth) + d.name }}</option>
        </select>
        <button v-if="auth.canManage" class="btn primary" @click="toggleAdd" v-html="icons.plus + ' 新建任务'"></button>
        <button v-if="auth.canManage" class="btn ghost" :disabled="importing" @click="showImport = !showImport">{{ importing ? '导入中…' : '导入任务' }}</button>
        <button v-if="auth.canManage" class="btn ghost" :class="{ active: batchMode }" @click="toggleBatch">{{ batchMode ? '退出批量' : '批量操作' }}</button>
        <button v-if="auth.canManage" class="btn ghost" :disabled="exporting" @click="exportTasks">{{ exporting ? '导出中…' : '⬇ 导出' }}</button>
        <button v-if="auth.canManage" class="btn ghost" :disabled="notifying" @click="sendNotify">{{ notifying ? '发送中…' : '📣 发送今日提醒' }}</button>
      </div>
    </div>

    <!-- 批量操作栏 -->
    <transition name="slide">
      <div v-if="batchMode && auth.canManage" class="batch-bar">
        <label class="sel-all"><input type="checkbox" :checked="allSelected" @change="toggleSelectAll" /> 全选本页（{{ selectedIds.length }}）</label>
        <div class="batch-actions">
          <button class="btn ghost" :disabled="!selectedIds.length || batching" @click="batchAct('complete')">完成</button>
          <button class="btn ghost" :disabled="!selectedIds.length || batching" @click="batchAct('reopen')">重开</button>
          <button class="btn danger sm" :disabled="!selectedIds.length || batching" @click="batchAct('delete')">删除</button>
        </div>
      </div>
    </transition>

    <section v-if="showAdd && auth.canManage" class="panel add-form">
      <div v-if="editId" class="edit-tip">✏️ 正在编辑任务 #{{ editId }}，保存后到点提醒将按新设置生效</div>
      <div class="fg2">
        <div>
          <label class="fld">任务内容 *</label>
          <input v-model="form.title" class="glass-input" placeholder="例如：提交周报" />
        </div>
        <div>
          <label class="fld">班次</label>
          <select v-model="form.shift" class="glass-input">
            <option value="全员">全员</option>
            <option value="早晚">早晚都执行</option>
            <option v-for="sc in deptShifts" :key="sc.id" :value="sc.name">{{ sc.name }}（{{ sc.start_time }}-{{ sc.end_time }}）</option>
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
      <div class="fg2">
        <div>
          <label class="fld">类型</label>
          <select v-model="form.type" class="glass-input">
            <option value="daily">每周</option>
            <option value="monthly">每月</option>
            <option value="once">单次</option>
          </select>
        </div>
        <div>
          <label class="fld">{{ form.type === 'daily' ? '执行时间' : '截止时间' }}</label>
          <input v-if="form.type === 'daily'" v-model="form.time" type="time" class="glass-input" />
          <input v-else v-model="form.deadline" type="datetime-local" class="glass-input" />
        </div>
      </div>
      <div v-if="form.type === 'daily'" class="fg1">
        <label class="fld">按周执行 <em class="faint">（勾选星期才做，不勾选=每天都执行）</em></label>
        <div class="wd-pick">
          <label v-for="d in [1, 2, 3, 4, 5, 6, 7]" :key="d" class="wd-chip" :class="{ on: form.weekdays.includes(d) }">
            <input type="checkbox" :value="d" v-model="form.weekdays" />
            <span>{{ ['周一', '周二', '周三', '周四', '周五', '周六', '周日'][d - 1] }}</span>
          </label>
        </div>
      </div>
      <div class="fg1">
        <label class="fld">负责人 <em class="faint">（单人/多人，可空；空=部门公共任务）</em></label>
        <div class="wd-pick ap-pick">
          <label v-for="u in userOptions" :key="u.id" class="wd-chip ap" :class="{ on: form.assignees.includes(u.name) }">
            <input type="checkbox" :value="u.name" v-model="form.assignees" />
            <span>{{ u.name }}<em v-if="u.dept">（{{ u.dept.name }}）</em></span>
          </label>
          <span v-if="!userOptions.length" class="faint">所选部门暂无人员，请先在「设置-人员」中添加后再指派</span>
        </div>
      </div>
      <div class="fg2">
        <div>
          <label class="fld">优先级</label>
          <select v-model="form.priority" class="glass-input">
            <option value="high">高</option>
            <option value="medium">中</option>
            <option value="low">低</option>
          </select>
        </div>
        <div>
          <label class="fld">备注</label>
          <input v-model="form.note" class="glass-input" placeholder="可选" />
        </div>
      </div>
      <div class="form-actions">
        <button class="btn ghost" @click="showAdd = false; editId = null">取消</button>
        <button class="btn primary" :disabled="saving" @click="save">{{ saving ? '保存中…' : (editId ? '保存修改' : '保存') }}</button>
      </div>
    </section>

    <!-- 批量导入 -->
    <section v-if="showImport && auth.canManage" class="panel add-form">
      <div class="form-title">
        <span v-html="icons.plus"></span>
        <span>批量导入任务</span>
        <button class="btn ghost form-close" @click="showImport = false">收起</button>
      </div>
      <textarea v-model="importText" class="glass-input import-ta" rows="9"
        placeholder="每行一个任务，格式：标题 | 班次 | 类型 | 时间/截止&#10;班次：全员 / 早班 / 晚班 / 早晚（默认全员）&#10;类型：每周 / 每月 / 单次（默认每周）&#10;时间/截止：每周填 08:00；每月或单次填 2026-08-30T18:00&#10;示例：&#10;开门检查 | 早班 | 每周 | 09:00&#10;晚班盘点 | 晚班 | 每周 | 21:00&#10;月底对账 | 早晚 | 每月 | 2026-08-31T17:00"></textarea>
      <div class="import-tip">将识别 {{ importCount }} 条有效任务</div>
      <div class="import-file">
        <span class="fld">导入到部门 <em class="req">必选</em></span>
        <select v-model="importDept" class="glass-input dept-filter" :class="{ 'req-miss': !importDept }">
          <option :value="0">— 请选择部门 —</option>
          <option v-for="d in deptOptions(departments)" :key="d.id" :value="d.id">{{ indentOf(d.depth) + d.name }}</option>
        </select>
      </div>
      <div class="import-file">
        <span class="fld">或上传文件（Excel 模板 / CSV）</span>
        <input type="file" accept=".xlsx,.csv" @change="uploadCSV" :disabled="importing" />
        <span class="section-sub" style="margin-left:8px">按表头名称自动识别列（序号等无关列会忽略），固定模板在 设置-模板 里下载</span>
      </div>
      <div class="form-actions">
        <button class="btn ghost" @click="showImport = false">取消</button>
        <button class="btn primary" :disabled="importing || !importCount" @click="doImport">开始导入</button>
      </div>
    </section>

    <section class="panel task-list-panel">
      <div ref="tableWrapEl" class="table-wrap" @scroll="updateStickyScroll">
        <table class="task-table">
          <thead>
            <tr>
              <th v-if="batchMode" class="col-sel"></th>
              <th class="col-check"></th>
              <th v-if="auth.isSuper" class="col-dept">部门</th>
              <th class="col-type">类型</th>
              <th class="col-shift">班次</th>
              <th class="col-title">任务内容</th>
              <th class="col-when">时间 / 截止</th>
              <th class="col-prio">优先级</th>
              <th class="col-act"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="t in filtered" :key="t.id" :class="{ done: t.status === 'done' }" class="clickable" @click="batchMode ? toggleSelect(t) : toggle(t)">
              <td v-if="batchMode" class="col-sel">
                <input type="checkbox" :checked="selectedIds.includes(t.id)" @click.stop="toggleSelect(t)" />
              </td>
              <td class="col-check">
                <button class="check" :class="{ on: t.status === 'done' }" @click.stop="toggle(t)" v-html="icons.check"></button>
              </td>
              <td v-if="auth.isSuper" class="col-dept"><span class="dept-tag">{{ deptName(t.dept_id) }}</span></td>
              <td class="col-type"><span class="chip" :class="typeClass(t.type)">{{ typeText(t.type) }}</span></td>
              <td class="col-shift"><span class="chip" :class="shiftClass(t.shift)">{{ shiftLabel(t) }}</span></td>
              <td class="col-title">
                <div class="t-title-row">
                  <span class="t-title">{{ t.title }}</span>
                  <span v-if="t.soon_overdue && t.status !== 'done'" class="chip soon" title="距截止 30 分钟内，请尽快处理">即将逾期</span>
                  <span v-if="t.overdue && t.status !== 'done'" class="chip danger">逾期</span>
                </div>
                <div v-if="t.note" class="t-note">{{ t.note }}</div>
                <div v-if="assigneeTextOf(t) || (t.week_days && t.type === 'daily')" class="t-tags">
                  <span v-if="assigneeTextOf(t)" class="t-tag">👤 {{ assigneeTextOf(t) }}</span>
                  <span v-if="t.week_days && t.type === 'daily' && weekLabelOf(t.week_days)" class="t-tag">🗓 {{ weekLabelOf(t.week_days) }}</span>
                </div>
                <div v-if="t.status === 'done' && t.completed_by" class="t-done-by">
                  ✓ 由 {{ t.completed_by }} 于 {{ fmtDone(t.completed_at) }} 完成
                </div>
              </td>
              <td class="col-when">
                <span v-if="t.time" class="when">{{ t.time }}</span>
                <span v-if="t.deadline" class="when">{{ dlText(t.deadline, t.type) }}</span>
                <span v-if="!t.time && !t.deadline" class="when faint">—</span>
              </td>
              <td class="col-prio"><span class="chip" :class="prioClass(t.priority)">{{ prioText(t.priority) }}</span></td>
              <td class="col-act">
                <div class="row-ops" @click.stop>
                  <button class="rec row-ops-btn" @click.stop="toggleRowMenu(t, $event)" :title="'更多操作 · ' + t.title" aria-label="更多操作">⋯</button>
                  <transition name="pop">
                      <div v-if="rowMenuId === t.id" class="row-ops-menu" @click.stop>
                        <button class="menu-item" @click="closeRowMenu(); openRecord(t)">
                          <span class="mi-ico">📋</span><span>完成记录</span>
                        </button>
                        <template v-if="auth.canManage">
                          <button class="menu-item" @click="closeRowMenu(); openEdit(t)">
                            <span class="mi-ico">✏️</span><span>编辑任务</span>
                          </button>
                          <button class="menu-item danger" @click="closeRowMenu(); remove(t)">
                            <span class="mi-ico">🗑</span><span>删除任务</span>
                          </button>
                        </template>
                      </div>
                    </transition>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <EmptyState v-if="!filtered.length" :title="emptyText" desc="去新建一条任务，或切换到其它标签页查看。" :action-text="auth.canManage ? '新建任务' : ''" :icon="calIcon" @action="showAdd = true" />
    </section>

    <!-- 固定底部的水平滚动条（始终可见，不随垂直滚动消失） -->
    <div v-show="tableWrapEl && hasHScroll" class="sticky-hscroll" @scroll="onStickyScroll">
      <div class="sticky-hscroll-inner" :style="{ width: stickyWidth + 'px' }"></div>
    </div>

    <!-- 完成记录弹窗 -->
    <div v-if="recordOpen" class="modal-mask" @click.self="recordOpen = false">
      <div class="modal">
        <div class="modal-head">
          <span>完成记录 · {{ recordTaskTitle }}</span>
          <button class="x" @click="recordOpen = false">×</button>
        </div>
        <div class="modal-body">
          <div v-if="recordLoading" class="rec-loading">加载中…</div>
          <div v-else-if="!records.length" class="rec-empty">暂无完成记录</div>
          <ul v-else class="rec-list">
            <li v-for="r in records" :key="r.id">
              <span class="rec-who">{{ r.user_name }}</span>
              <span class="rec-time">{{ fmtFull(r.completed_at) }}</span>
            </li>
          </ul>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { useAutoRefresh } from '@/autoRefresh'
import * as api from '@/api'
import { icons } from '@/icons'
import { useAuthStore } from '@/store/auth'
import { deptOptions, indentOf } from '@/utils/dept'
import EmptyState from '@/components/EmptyState.vue'

const auth = useAuthStore()
const tasks = ref([])
const schedules = ref([])
const tab = ref('today')
const showAdd = ref(false)
const calIcon = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="17" rx="3"/><path d="M3 9h18M8 2v4M16 2v4M9 14l2 2 4-4"/></svg>'
const saving = ref(false)
const editId = ref(null)
// v0.8.0：weekdays=按周执行（1=周一…7=周日，多选，空=每天）；assignees=负责人（多人姓名）
const form = reactive({ title: '', type: 'daily', shift: '全员', time: '', deadline: '', priority: 'medium', note: '', dept_id: null, weekdays: [], assignees: [] })
const users = ref([])
const shiftConfigs = ref([])
const departments = ref([])
// 表单可选的负责人：按所选部门过滤（超管跨部门时可任选系统人员）
const userOptions = computed(() => {
  let list = users.value
  const did = form.dept_id
  if (did) list = list.filter((u) => u.dept_id === did || (u.dept && u.dept.parent_id === did))
  return list
})
const emptyForm = { title: '', type: 'daily', shift: '全员', time: '', deadline: '', priority: 'medium', note: '', dept_id: null, weekdays: [], assignees: [] }
// 当前表单所选部门的班次（含时间）
const deptShifts = computed(() => {
  const did = form.dept_id || auth.user.dept_id
  return shiftConfigs.value.filter((sc) => sc.dept_id === did)
})

// 批量导入状态
const showImport = ref(false)
const importing = ref(false)
const importText = ref('')
// 导入目标部门：不设默认值，必须由操作人显式选择，避免沿用账号所属部门而导错
const importDept = ref(0)

// 部门筛选（超管按部门查看任务）
const filterDept = ref(0)
function deptName(id) {
  const d = departments.value.find((x) => x.id === id)
  return d ? d.name : '—'
}

// 批量操作状态
const batchMode = ref(false)
const batching = ref(false)
const selectedIds = ref([])
const allSelected = computed(() => filtered.value.length > 0 && filtered.value.every((t) => selectedIds.value.includes(t.id)))
function toggleBatch() {
  batchMode.value = !batchMode.value
  if (!batchMode.value) selectedIds.value = []
}
function toggleSelect(t) {
  const i = selectedIds.value.indexOf(t.id)
  if (i >= 0) selectedIds.value.splice(i, 1)
  else selectedIds.value.push(t.id)
}
function toggleSelectAll(e) {
  if (e.target.checked) selectedIds.value = filtered.value.map((t) => t.id)
  else selectedIds.value = []
}
async function batchAct(action) {
  if (!selectedIds.value.length) return
  const label = { complete: '完成', reopen: '重开', delete: '删除' }[action]
  if (action === 'delete' && !confirm(`⚠️ 确认删除选中的 ${selectedIds.value.length} 条任务？删除后不可恢复。`)) return
  if (action !== 'delete' && !confirm(`确认将选中的 ${selectedIds.value.length} 条任务${label}？`)) return
  batching.value = true
  try {
    if (action === 'delete') {
      const r = await api.post('/tasks/batch-delete', { ids: selectedIds.value })
      let msg = `已删除 ${r.deleted || 0} 条`
      if (r.failed) msg += `，${r.failed} 条未删除` + ((r.errors && r.errors.length) ? '：\n' + r.errors.slice(0, 5).join('\n') : '')
      alert(msg)
    } else {
      const r = await api.post('/tasks/batch', { ids: selectedIds.value, action })
      alert(`已处理 ${r.processed || 0} 条${r.skipped ? '，跳过 ' + r.skipped + ' 条' : ''}`)
    }
    selectedIds.value = []
    await load()
  } catch (e) { alert(e.response?.data?.error || '操作失败') }
  finally { batching.value = false }
}

// CSV 文件导入
async function uploadCSV(e) {
  const file = e.target.files && e.target.files[0]
  if (!file) return
  const target = importDept.value
  if (!target) { alert('请先在「导入到部门」里选择目标部门，再上传文件。'); e.target.value = ''; return }
  if (!confirm(`将把「${file.name}」导入到部门「${deptName(target)}」（Excel 或 CSV），确认？`)) { e.target.value = ''; return }
  importing.value = true
  try {
    const r = await api.upload('/tasks/import', file, 'file', { dept_id: target })
    let msg = `导入完成：成功 ${r.created || 0} 条，失败 ${r.failed || 0} 条`
    if (r.columns) msg += '\n\n列识别结果：\n' + r.columns
    if (r.errors && r.errors.length) msg += '\n' + r.errors.slice(0, 5).join('\n')
    alert(msg)
    showImport.value = false
    await load()
  } catch (err) { alert(err.response?.data?.error || '导入失败') }
  finally { importing.value = false; e.target.value = '' }
}

// 导出任务 Excel（鉴权下载；按 Content-Disposition 取中文文件名）
const exporting = ref(false)
async function exportTasks() {
  exporting.value = true
  try {
    const token = localStorage.getItem('sw_token')
    const r = await fetch('/api/tasks/export', { headers: { Authorization: 'Bearer ' + token } })
    if (!r.ok) { alert('导出失败'); return }
    const blob = await r.blob()
    const cd = r.headers.get('content-disposition') || ''
    const m = cd.match(/filename\*=UTF-8''([^;]+)|filename="?([^";]+)"?/)
    const today = new Date().toISOString().slice(0, 10)
    const fn = (m && (m[1] || m[2])) ? decodeURIComponent(m[1] || m[2]) : ('任务列表_' + today.replace(/-/g, '') + '.xlsx')
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = fn
    a.click()
    URL.revokeObjectURL(a.href)
  } catch { alert('导出失败') }
  finally { exporting.value = false }
}
const typeMap = { '每周': 'daily', '每日': 'daily', '每天': 'daily', '每月': 'monthly', '单次': 'once', '临时': 'once', 'daily': 'daily', 'monthly': 'monthly', 'once': 'once' }
const shiftMap = { '早班': '早班', '晚班': '晚班', '早晚': '早晚', '早晚班': '早晚', '全员': '全员', '所有人': '全员' }
// 解析导入文本 -> 任务对象列表（标题 | 班次 | 类型 | 时间）
const parsedTasks = computed(() => {
  const out = []
  for (const raw of importText.value.split('\n')) {
    const line = raw.trim()
    if (!line) continue
    const parts = line.split('|').map((s) => s.trim())
    const title = parts[0]
    if (!title) continue
    const shift = shiftMap[parts[1]] || '全员'
    const type = typeMap[parts[2]] || 'daily'
    const when = parts[3] || ''
    const t = { title, shift, type }
    if (type === 'daily') t.time = /^\d{2}:\d{2}$/.test(when) ? when : ''
    else t.deadline = fillDueTime(type, when)
    t.priority = 'medium'
    t.note = ''
    out.push(t)
  }
  return out
})
const importCount = computed(() => parsedTasks.value.length)

// 完成记录弹窗状态
const recordOpen = ref(false)
const recordLoading = ref(false)
const records = ref([])
const recordTaskTitle = ref('')

// 行内操作菜单（⋯ 按钮弹出的下拉）
const rowMenuId = ref(null)
function toggleRowMenu(t) { rowMenuId.value = rowMenuId.value === t.id ? null : t.id }
function closeRowMenu() { rowMenuId.value = null }
// 点击页面其它位置关闭菜单
function onDocClick(e) {
  if (rowMenuId.value == null) return
  // 已经在 row-ops 内会被 stopPropagation 拦掉，这里只兜住外部点击
  if (e.target.closest && e.target.closest('.row-ops')) return
  rowMenuId.value = null
}

// 班次文案/样式
function shiftText(s) { return { '全员': '全员', '早班': '早班', '晚班': '晚班', '早晚': '早晚' }[s] || s }
function shiftClass(s) { return { '全员': '', '早班': 'accent', '晚班': 'warn', '早晚': 'accent-3' }[s] || 'accent-3' }
// 班次显示：只显示班次名（不附带时间段，列表更整洁；时间查看可看任务详情）
function shiftLabel(t) {
  return shiftText(t.shift)
}
// 按周执行可读文案：week_days="1,3,5" → "周一、周三、周五"
function weekLabelOf(wd) {
  if (!wd) return ''
  const names = { 1: '周一', 2: '周二', 3: '周三', 4: '周四', 5: '周五', 6: '周六', 7: '周日' }
  return String(wd).split(',').map((n) => names[parseInt(n, 10)]).filter(Boolean).join('、')
}
// 负责人可读文案：优先解析 assignees(JSON 数组)，否则回退 assignee
function assigneeTextOf(t) {
  if (t.assignees) { try { const arr = JSON.parse(t.assignees); if (Array.isArray(arr) && arr.length) return arr.join('、') } catch (e) {} }
  return t.assignee || ''
}

// 执行者按今日班次过滤：谁当班谁负责
function todayStr() { const d = new Date(); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}` }
function peopleOf(s) { try { return JSON.parse(s.people || '[]') } catch { return [] } }
const myShifts = computed(() => {
  if (auth.canManage) return ['早班', '晚班'] // 管理员看全部，无需过滤
  const set = new Set()
  const today = todayStr()
  for (const s of schedules.value) {
    if (s.date !== today) continue
    if (peopleOf(s).includes(auth.user.name)) set.add(s.shift)
  }
  return [...set]
})
function shiftMatch(t) {
  if (!t.shift || t.shift === '全员') return true
  if (t.shift === '早晚') return myShifts.value.includes('早班') || myShifts.value.includes('晚班')
  return myShifts.value.includes(t.shift)
}
const visibleTasks = computed(() => tasks.value.filter((t) => auth.canManage || shiftMatch(t)))

// 每月任务只统计 type=monthly，避免每日任务（due_this_month 同样为 true）混入
const isMonthlyTask = (t) => t.type === 'monthly' && t.due_this_month

const counts = computed(() => ({
  today: visibleTasks.value.filter((t) => t.due_today && t.status !== 'done').length,
  month: visibleTasks.value.filter((t) => isMonthlyTask(t) && t.status !== 'done').length,
  overdue: visibleTasks.value.filter((t) => t.overdue && t.status !== 'done').length
}))

const todayAll = computed(() => visibleTasks.value.filter((t) => t.due_today))
const todayDone = computed(() => todayAll.value.filter((t) => t.status === 'done').length)
const todayPending = computed(() => todayAll.value.filter((t) => t.status !== 'done').length)
const todayOverdue = computed(() => todayAll.value.filter((t) => t.overdue && t.status !== 'done').length)
const monthAll = computed(() => visibleTasks.value.filter(isMonthlyTask))
const monthDone = computed(() => monthAll.value.filter((t) => t.status === 'done').length)
const monthPending = computed(() => monthAll.value.filter((t) => t.status !== 'done').length)
const allDone = computed(() => visibleTasks.value.filter((t) => t.status === 'done').length)
const allPending = computed(() => visibleTasks.value.filter((t) => t.status !== 'done').length)
const allOverdue = computed(() => visibleTasks.value.filter((t) => t.overdue && t.status !== 'done').length)
const pct = (a, b) => (b ? Math.round((a / b) * 100) : 0) + '%'

const filtered = computed(() => {
  let list = visibleTasks.value
  if (filterDept.value) list = list.filter((t) => t.dept_id === filterDept.value)
  if (tab.value === 'today') return list.filter((t) => t.due_today && t.status !== 'done')
  if (tab.value === 'month') return list.filter((t) => isMonthlyTask(t) && t.status !== 'done')
  if (tab.value === 'overdue') return list.filter((t) => t.overdue && t.status !== 'done')
  return list
})

const emptyText = computed(() => {
  if (tab.value === 'overdue') return '没有逾期事项，保持得很好 👍'
  if (tab.value === 'today') return '今天没有待办任务 🎉'
  if (tab.value === 'month') return '本月暂无待办任务'
  return '暂无任务'
})

function typeText(t) { return { daily: '每周', monthly: '每月', once: '单次' }[t] || t }
function typeClass(t) { return { daily: 'accent', monthly: 'warn', once: '' }[t] || '' }
function prioClass(p) { return { high: 'danger', medium: 'warn', low: 'ok' }[p] || '' }
function prioText(p) { return { high: '高', medium: '中', low: '低' }[p] || '中' }
// 截止日显示：月度任务只显示日期。
// deadline 里的 09:00 是晨间推送提醒时点，不是完成期限（实际期限是截止日当天 23:59 前），
// 把时刻显示出来会让人误以为「早上 9 点之前必须做完」
function dlText(s, type) {
  if (!s) return ''
  if (type === 'monthly') {
    const p = s.slice(0, 10).split('-')
    if (p.length === 3) return `${Number(p[1])}/${Number(p[2])}`
    return s.slice(0, 10)
  }
  return s.replace('T', ' ').slice(0, 16)
}

// 完成时间格式化：2026-08-27 10:43
function fmtDone(s) {
  if (!s) return ''
  const d = new Date(s)
  if (isNaN(d)) return ''
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
// 完整时间戳：2026年08月27日 10:43:31
function fmtFull(s) {
  if (!s) return ''
  const d = new Date(s)
  if (isNaN(d)) return ''
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}年${p(d.getMonth() + 1)}月${p(d.getDate())}日 ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

async function toggle(t) {
  const toDone = t.status !== 'done'
  const msg = toDone ? `确认完成任务「${t.title}」？` : `确认将任务「${t.title}」重新打开？`
  if (!confirm(msg)) return
  const prev = t.status
  t.status = toDone ? 'done' : 'todo'
  try {
    const updated = await api.post(`/tasks/${t.id}/toggle`, { to: toDone ? 'done' : 'todo' })
    if (updated && updated.status) {
      t.status = updated.status // 以服务端为准
      t.completed_by = updated.completed_by || ''
      t.completed_at = updated.completed_at || ''
    }
  } catch (e) {
    t.status = prev // 失败回滚
    alert(e.response?.data?.error || '操作失败')
  }
}
async function doImport() {
  const list = parsedTasks.value
  if (!list.length) return
  const target = importDept.value
  if (!target) { alert('请先在「导入到部门」里选择目标部门。'); return }
  if (!confirm(`确认把 ${list.length} 条任务导入到部门「${deptName(target)}」？`)) return
  importing.value = true
  let ok = 0
  try {
    for (const item of list) {
      try {
        await api.post('/tasks', { ...item, dept_id: target })
        ok++
      } catch { /* 单条失败跳过 */ }
    }
    alert(`导入完成：成功 ${ok} 条，失败 ${list.length - ok} 条`)
    importText.value = ''
    showImport.value = false
    await load()
  } catch (e) { alert(e.response?.data?.error || '导入失败') }
  finally { importing.value = false }
}
async function remove(t) {
  if (!confirm(`⚠️ 确认删除任务「${t.title}」？删除后不可恢复。`)) return
  try { await api.del(`/tasks/${t.id}`); await load() } catch (e) { alert(e.response?.data?.error || '删除失败') }
}
async function openRecord(t) {
  recordTaskTitle.value = t.title
  recordOpen.value = true
  recordLoading.value = true
  records.value = []
  try {
    records.value = await api.get(`/tasks/${t.id}/completions`)
  } catch (e) {
    alert(e.response?.data?.error || '加载失败')
  } finally {
    recordLoading.value = false
  }
}
function toggleAdd() {
  showAdd.value = !showAdd.value
  if (showAdd.value) {
    editId.value = null
    Object.assign(form, { ...emptyForm, dept_id: auth.isSuper ? (departments.value[0]?.id || null) : (auth.user.dept_id || null) })
  }
}
function openEdit(t) {
  editId.value = t.id
  let names = []
  if (t.assignees) { try { const arr = JSON.parse(t.assignees); if (Array.isArray(arr)) names = arr } catch (e) {} }
  if (!names.length && t.assignee) names = t.assignee.split(/[、;；,，/]+/).map((s) => s.trim()).filter(Boolean)
  let wds = []
  if (t.week_days) wds = String(t.week_days).split(',').map((s) => parseInt(s, 10)).filter((n) => n >= 1 && n <= 7)
  Object.assign(form, {
    title: t.title, type: t.type, shift: t.shift || '全员',
    time: t.time || '', deadline: t.deadline || '',
    priority: t.priority || 'medium', note: t.note || '',
    dept_id: t.dept_id || auth.user.dept_id,
    weekdays: wds, assignees: names
  })
  showAdd.value = true
  window.scrollTo({ top: 0, behavior: 'smooth' })
}
// 月度任务：只填了日期没填时间时，默认截止到当天 09:00
// （当天 9 点前完成都算准时，9 点后仍未完成才算逾期）
function fillDueTime(type, deadline) {
  if (type === 'monthly' && /^\d{4}-\d{2}-\d{2}$/.test(deadline)) return deadline + 'T09:00'
  return deadline
}
async function save() {
  if (!form.title) { alert('任务内容不能为空'); return }
  if (!form.dept_id) { alert('请选择所属部门'); return }
  saving.value = true
  try {
    const payload = {
      title: form.title, type: form.type, shift: form.shift, time: form.time,
      deadline: fillDueTime(form.type, form.deadline),
      priority: form.priority, note: form.note, dept_id: form.dept_id,
      assignees: form.assignees,
      week_days: form.type === 'daily' && form.weekdays.length ? form.weekdays.join(',') : ''
    }
    if (editId.value) await api.put(`/tasks/${editId.value}`, payload)
    else await api.post('/tasks', payload)
    showAdd.value = false
    editId.value = null
    Object.assign(form, emptyForm)
    await load()
  } catch (e) { alert(e.response?.data?.error || '保存失败') }
  finally { saving.value = false }
}

// 发送今日任务提醒到企业微信群
const notifying = ref(false)
async function sendNotify() {
  if (!confirm('将今日待办任务提醒发送到已配置的企业微信群（自动@当班人员），确认发送？')) return
  notifying.value = true
  try {
    const r = await api.post('/webhooks/notify')
    alert(r?.msg || `已发送到 ${r?.sent || 0} 个群`)
  } catch (e) { alert(e.response?.data?.error || '发送失败') }
  finally { notifying.value = false }
}

async function load() {
  const [ts, sc, scs, deps, us] = await Promise.all([
    api.get('/tasks'),
    api.get('/schedules'),
    api.get('/shift-configs').catch(() => []),
    api.get('/departments').catch(() => []),
    api.get('/users').catch(() => [])
  ])
  tasks.value = ts
  schedules.value = sc
  shiftConfigs.value = scs
  departments.value = deps
  users.value = us
  if (auth.isSuper && !form.dept_id && departments.value.length) form.dept_id = departments.value[0].id
  // 等表格渲染完再测一次是否溢出，更新固定滚动条
  await nextTick()
  updateStickyScroll()
}

// 固定底部滚动条：表格横向溢出时，把滚动条镜像到底部，避免被遮在视口底部不可见
const tableWrapEl = ref(null)
const hasHScroll = ref(false)
const stickyWidth = ref(0)
function updateStickyScroll() {
  const el = tableWrapEl.value
  if (!el) { hasHScroll.value = false; return }
  // 表格实际滚动宽度（clientWidth 包含 padding/border，scrollWidth 是内容总宽）
  const overflow = el.scrollWidth - el.clientWidth
  if (overflow > 1) {
    hasHScroll.value = true
    stickyWidth.value = el.scrollWidth
  } else {
    hasHScroll.value = false
  }
}
function onStickyScroll(e) {
  const el = tableWrapEl.value
  if (!el) return
  // 同步原表格的滚动位置
  el.scrollLeft = e.target.scrollLeft
}
onMounted(() => {
  load()
  useAutoRefresh(load, true)
  window.addEventListener('resize', updateStickyScroll)
  document.addEventListener('click', onDocClick)
})
onUnmounted(() => {
  useAutoRefresh(load, false)
  window.removeEventListener('resize', updateStickyScroll)
  document.removeEventListener('click', onDocClick)
})
</script>

<style scoped>
.overview { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; margin-bottom: 16px; }
.ov-card { text-align: left; padding: 14px; border-radius: 14px; background: var(--overlay); border: 1px solid var(--glass-border); cursor: pointer; display: flex; flex-direction: column; gap: 9px; transition: all 0.15s; color: inherit; font: inherit; }
.ov-card:hover { border-color: var(--accent); background: var(--overlay-2); }
.ov-card.active { border-color: var(--accent); box-shadow: 0 0 0 1px var(--accent) inset; }
.ov-item { display: flex; flex-direction: column; gap: 9px; }
.ov-head { display: flex; align-items: baseline; justify-content: space-between; }
.ov-label { font-size: 13px; color: var(--text-dim); font-weight: 600; }
.ov-num { font-size: 20px; font-weight: 800; }
.ov-num em { font-style: normal; font-size: 11px; color: var(--text-faint); font-weight: 400; margin-left: 4px; }
.ov-sub { display: flex; align-items: center; justify-content: space-between; gap: 8px; font-size: 12px; color: var(--text-faint); }
.ov-od { color: var(--danger); font-weight: 700; }
.ov-faint { color: var(--text-faint); }
.reset-tip { display: flex; align-items: center; gap: 6px; margin: 0 0 12px; padding: 8px 12px;
  font-size: 12.5px; line-height: 1.6; color: var(--text-faint);
  background: var(--overlay-2); border-radius: 8px; }
.reset-tip svg { flex: none; opacity: .75; }
@media (max-width: 768px) { .reset-tip { align-items: flex-start; } }
.bar { height: 8px; border-radius: 999px; background: var(--overlay-2); overflow: hidden; }
.bar-fill { height: 100%; border-radius: 999px; transition: width 0.4s ease; }
.bar-fill.accent { background: linear-gradient(90deg, var(--accent), var(--accent-2)); }
.bar-fill.violet { background: linear-gradient(90deg, var(--accent-2), var(--accent-3)); }
.bar-fill.slate { background: linear-gradient(90deg, var(--accent-3), var(--ok)); }

.head-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 16px; flex-wrap: wrap; }
.head-actions { display: flex; gap: 8px; align-items: center; }
.tabs { display: flex; gap: 6px; background: var(--overlay); border: 1px solid var(--glass-border); border-radius: 13px; padding: 4px; }
.tab { border: none; background: transparent; color: var(--text-dim); padding: 8px 16px; border-radius: 10px; cursor: pointer; font-size: 13px; display: inline-flex; align-items: center; gap: 6px; }
.tab.active { background: var(--glass-strong); color: var(--text); }
.tab i { font-style: normal; font-size: 11px; background: var(--overlay-2); padding: 1px 7px; border-radius: 999px; }
.tab i.bad { background: rgba(225,29,72,0.22); color: var(--danger); }

.add-form { margin-bottom: 16px; }
.edit-tip { font-size: 12.5px; color: var(--accent); background: var(--overlay-2); padding: 8px 12px; border-radius: 10px; margin-bottom: 12px; }
.fg2 { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; margin-bottom: 14px; }
.form-actions { display: flex; justify-content: flex-end; gap: 10px; }
.form-title { display: flex; align-items: center; gap: 8px; font-size: 14px; font-weight: 700; margin-bottom: 12px; }
.form-title :deep(svg) { width: 16px; height: 16px; color: var(--accent); }
.form-close { margin-left: auto; padding: 6px 12px; }
.import-ta { width: 100%; resize: vertical; font-family: ui-monospace, monospace; font-size: 12.5px; line-height: 1.7; }
.import-tip { font-size: 12px; color: var(--text-dim); margin-top: 8px; }

/* 毛玻璃表格 */
.table-wrap { width: 100%; max-width: 100%; overflow-x: auto; }
/* 固定底部的水平滚动条：始终贴在视口底部（页面底），不随垂直滚动消失 */
.sticky-hscroll {
  position: fixed; left: 0; right: 0; bottom: 0;
  height: 14px;
  overflow-x: auto; overflow-y: hidden;
  background: var(--bg-1, rgba(255,255,255,0.85));
  border-top: 1px solid var(--glass-border);
  z-index: 30;
  padding-bottom: env(safe-area-inset-bottom);
}
.sticky-hscroll-inner { height: 1px; }
.task-table { width: 100%; border-collapse: collapse; font-size: 13.5px; }
.task-table thead th {
  text-align: left; padding: 10px 14px; font-size: 11.5px; font-weight: 600;
  color: var(--text-faint); letter-spacing: 0.4px; border-bottom: 1px solid var(--glass-border);
  white-space: nowrap;
}
.task-table tbody td {
  padding: 12px 14px; border-bottom: 1px solid var(--hairline); vertical-align: middle;
}
.task-table tbody tr { transition: background 0.15s; }
.task-table tbody tr.clickable { cursor: pointer; }
.task-table tbody tr:hover { background: var(--overlay); }
.task-table tbody tr:last-child td { border-bottom: none; }
.task-table tbody tr.done { opacity: 0.45; }
.task-table tbody tr.done .t-title { text-decoration: line-through; }
.t-done-by { margin-top: 4px; font-size: 12px; color: var(--ok); }
.t-done-by::before { content: "✓ "; }

/* 记录按钮 */
.col-act { display: flex; gap: 6px; align-items: center; }
.row-ops { position: relative; display: inline-block; }
.row-ops-btn {
  width: 30px; height: 30px; line-height: 1; font-size: 18px;
  display: inline-flex; align-items: center; justify-content: center;
  border-radius: 9px; border: 1px solid var(--glass-border);
  background: var(--overlay); color: var(--text-dim);
  cursor: pointer;
}
.row-ops-btn:hover { color: var(--text); border-color: var(--accent); }
.row-ops-menu {
  position: absolute; right: 0; top: calc(100% + 6px);
  background: var(--glass-strong, rgba(255,255,255,0.96));
  border: 1px solid var(--glass-border);
  border-radius: 12px; padding: 6px;
  box-shadow: 0 8px 24px rgba(15,23,42,0.16);
  min-width: 150px; z-index: 40;
  display: flex; flex-direction: column; gap: 2px;
}
.menu-item {
  display: flex; align-items: center; gap: 10px;
  padding: 8px 12px; border: 0; background: transparent;
  border-radius: 8px; cursor: pointer; font-size: 13px;
  color: var(--text); text-align: left;
}
.menu-item:hover { background: var(--overlay); }
.menu-item.danger { color: var(--danger); }
.menu-item.danger:hover { background: rgba(225,29,72,0.08); }
.mi-ico { font-size: 14px; line-height: 1; opacity: 0.85; }
/* 行内菜单展开/收折叠动画 */
.pop-enter-active, .pop-leave-active { transition: opacity .12s ease, transform .12s ease; }
.pop-enter-from, .pop-leave-to { opacity: 0; transform: translateY(-4px); }
.dept-filter { width: auto; min-width: 130px; padding: 8px 10px; font-size: 13px; height: 38px; }
.col-dept { width: 110px; }
.dept-tag { color: var(--text-dim); font-size: 12.5px; white-space: nowrap; }
.rec { font-size: 12px; padding: 3px 9px; border-radius: 7px; cursor: pointer; border: 1px solid var(--glass-border); background: var(--overlay); color: var(--text-dim); }
.rec:hover { color: var(--text); border-color: var(--accent); }
.del { display: inline-flex; align-items: center; gap: 3px; height: 26px; padding: 0 8px; border-radius: 8px; cursor: pointer; border: 1px solid var(--glass-border); background: transparent; color: var(--danger); font-size: 12.5px; line-height: 1; }
.del:hover { background: rgba(225,29,72,0.1); border-color: rgba(225,29,72,0.45); }
.del-x { font-size: 15px; }

/* 弹窗 */
.modal-mask { position: fixed; inset: 0; background: var(--mask); display: grid; place-items: center; z-index: 50; backdrop-filter: blur(3px); }
.modal { width: min(440px, 92vw); background: var(--glass-strong); border: 1px solid var(--glass-border); border-radius: 16px; overflow: hidden; box-shadow: var(--shadow); }
.modal-head { display: flex; align-items: center; justify-content: space-between; padding: 14px 18px; border-bottom: 1px solid var(--glass-border); font-weight: 600; font-size: 14px; }
.modal-head .x { width: 28px; height: 28px; border-radius: 8px; border: none; background: var(--overlay-2); color: var(--text-dim); cursor: pointer; font-size: 16px; }
.modal-body { padding: 14px 18px; max-height: 60vh; overflow-y: auto; }
.rec-loading, .rec-empty { color: var(--text-dim); font-size: 13px; padding: 12px 0; text-align: center; }
.rec-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
.rec-list li { display: flex; align-items: center; justify-content: space-between; padding: 10px 12px; border-radius: 10px; background: var(--overlay); font-size: 13px; }
.rec-who { font-weight: 600; }
.rec-time { color: var(--text-dim); font-variant-numeric: tabular-nums; }

.col-check { width: 44px; }
.check { width: 26px; height: 26px; border-radius: 8px; cursor: pointer; border: 1px solid var(--glass-border); background: transparent; color: var(--accent); display: grid; place-items: center; }
/* 图标由 v-html 注入，不带 scoped 的 data-v 属性，必须用 :deep 才能命中 */
.check :deep(svg) { width: 16px; height: 16px; opacity: 0; }
.check.on { background: var(--accent); color: #fff; }
.check.on :deep(svg) { opacity: 1; }

.col-title { min-width: 200px; }
.t-title-row { display: flex; align-items: center; gap: 8px; }
.t-title { font-weight: 600; }
.t-note { font-size: 12px; color: var(--text-faint); margin-top: 4px; }
.col-when .when { color: var(--text-dim); white-space: nowrap; }
.col-when .when.faint { color: var(--text-faint); }
.col-act { width: 44px; text-align: right; }
.del { width: 26px; height: 26px; border-radius: 8px; border: 1px solid var(--glass-border); background: transparent; color: var(--text-faint); cursor: pointer; font-size: 16px; line-height: 1; }
.del:hover { color: var(--danger); border-color: rgba(225,29,72,0.4); }
/* 按周执行 + 负责人选择 */
.fg1 { display: flex; flex-direction: column; gap: 6px; margin: 10px 0; }
.fld { font-size: 13px; color: var(--text-dim); }
.wd-pick { display: flex; flex-wrap: wrap; gap: 8px; }
.wd-chip { display: inline-flex; align-items: center; gap: 4px; padding: 4px 10px; border-radius: 999px; border: 1px solid var(--glass-border); cursor: pointer; font-size: 13px; color: var(--text-dim); background: transparent; }
.wd-chip input { display: none; }
.wd-chip.on { background: var(--accent); border-color: var(--accent); color: #fff; }
.wd-chip em { font-style: normal; opacity: 0.75; font-size: 12px; }
.t-tags { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 5px; }
.t-tag { font-size: 12px; color: var(--text-dim); background: var(--glass-bg); border: 1px solid var(--glass-border); border-radius: 999px; padding: 1px 8px; }
.ap-pick .wd-chip.on { background: var(--ok); border-color: var(--ok); }

@media (max-width: 820px) {
  .overview { grid-template-columns: 1fr; }
  .fg2 { grid-template-columns: 1fr; }
  .col-type, .col-prio { display: none; }
}
/* 批量操作 */
.batch-bar { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 14px; margin-bottom: 14px; border-radius: 12px; background: var(--overlay); border: 1px solid var(--accent); flex-wrap: wrap; }
.sel-all { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--text-dim); cursor: pointer; }
.sel-all input { width: 16px; height: 16px; accent-color: var(--accent); }
.batch-actions { display: flex; gap: 8px; }
.btn.danger.sm { padding: 6px 12px; font-size: 12.5px; background: var(--danger); border-color: var(--danger); color: #fff; }
.btn.danger.sm:hover { opacity: 0.9; }
.btn.ghost.active { border-color: var(--accent); color: var(--accent); }
.col-sel { width: 36px; text-align: center; }
.col-sel input { width: 17px; height: 17px; accent-color: var(--accent); cursor: pointer; }
.import-file { display: flex; align-items: center; gap: 10px; margin-top: 10px; font-size: 13px; color: var(--text-dim); }
.import-file input[type=file] { font-size: 12px; color: var(--text-dim); }
.import-file .req { font-style: normal; font-size: 11px; font-weight: 700; color: var(--danger); margin-left: 4px; }
.dept-filter.req-miss { border-color: var(--danger); box-shadow: 0 0 0 2px rgba(225, 29, 72, 0.12); }
.slide-enter-active, .slide-leave-active { transition: all 0.2s ease; }
.slide-enter-from, .slide-leave-to { opacity: 0; transform: translateY(-6px); }
</style>
