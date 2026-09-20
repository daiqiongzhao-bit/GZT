<template>
  <div class="adm-page">
    <div class="adm-head">
      <h2 class="section-title">
        部门管理
        <span class="section-sub">树形结构；ancestors 存祖级路径，是「本部门及以下」数据范围的展开依据</span>
      </h2>
      <div class="adm-actions">
        <button class="btn" :disabled="loading" @click="load">刷新</button>
        <button v-if="auth.can('system:dept:add')" class="btn primary" @click="openCreate(null)">新增部门</button>
      </div>
    </div>

    <!-- ==================== 部门管理员落地说明 ==================== -->
    <section class="panel">
      <h3 class="adm-sub-title">部门管理员是怎么落地的（零新增模型）</h3>
      <div class="adm-kv">
        <span class="k">管理边界</span>
        <span class="v">由<b>用户自己的 dept_id</b> 锚定 —— 同一个「部门管理员」角色绑给不同部门的人，边界自动不同，因此不需要新建任何"管理员表"或"管理范围"字段。</span>
        <span class="k">数据范围</span>
        <span class="v">角色上的 <b>data_scope</b>（{{ '1 全部 / 2 自定义 / 3 本部门 / 4 本部门及以下 / 5 仅本人' }}）；部门管理员标准档 = <b>4 本部门及以下</b>。</span>
        <span class="k">可见</span>
        <span class="v">本部门及子部门用户（读取时服务端注入过滤条件，前端传参无法突破）。</span>
        <span class="k">可管</span>
        <span class="v">写操作走<b>归属断言</b>：在"持有该权限的角色"子集内取最宽，避免借读范围拿写权限。</span>
        <span class="k">停用部门</span>
        <span class="v">仅"不允许新分配用户、不出现在选择器"；<b>存量用户照常计算数据范围</b>，否则一停用整部门 403，属运维事故。</span>
      </div>
    </section>

    <!-- ==================== 部门树 ==================== -->
    <section class="panel">
      <div class="adm-tablewrap">
        <table class="adm-table">
          <thead>
            <tr>
              <th>部门名称</th>
              <th>负责人</th>
              <th class="num">排序</th>
              <th class="num">用户数</th>
              <th>班次</th>
              <th>状态</th>
              <th>祖级路径（ancestors）</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="d in flat" :key="d.id">
              <td>
                <span class="adm-indent" :style="{ width: d.depth * 18 + 'px' }"></span>
                <span v-if="d.depth" class="adm-tree-mark">└</span>
                {{ d.name }}
              </td>
              <td class="dim">{{ d.leader || '—' }}</td>
              <td class="num dim">{{ d.order_num }}</td>
              <td class="num">
                <span class="chip" :class="d.user_count ? 'ok' : ''">{{ d.user_count }}</span>
              </td>
              <!-- 班次：原「设置 → 部门」里的「上下班时间 + 矩阵配色」并入此处 -->
              <td class="nowrap">
                <template v-if="shiftsOf(d.id).length">
                  <span v-for="sc in shiftsOf(d.id)" :key="sc.id" class="adm-shift-chip">
                    <i class="adm-sc-dot sm" :style="{ background: shiftColorCss(sc.color_key) }"></i>
                    {{ sc.name }} {{ sc.start_time }}-{{ sc.end_time }}
                  </span>
                </template>
                <span v-else class="dim">未配置</span>
              </td>
              <td>
                <span v-if="d.status" class="chip danger">停用</span>
                <span v-else class="chip ok">正常</span>
              </td>
              <td class="mono dim">{{ d.ancestors || '0' }}</td>
              <td>
                <div class="adm-row-actions">
                  <button v-if="auth.can('system:dept:add')" class="adm-link" @click="openCreate(d)">新增下级</button>
                  <button v-if="auth.can('system:dept:edit')" class="adm-link" @click="openEdit(d)">编辑</button>
                  <button
                    v-if="canShiftConf"
                    class="adm-link"
                    @click="openShifts(d)"
                  >班次配置</button>
                  <button
                    v-if="auth.can('system:dept:remove')"
                    class="adm-link danger"
                    :disabled="!!d.children?.length || d.user_count > 0"
                    :title="blockReason(d)"
                    @click="doDelete(d)"
                  >删除</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!flat.length" class="empty">{{ loading ? '加载中…' : '暂无部门数据' }}</div>
      <p class="adm-hint">
        共 {{ flat.length }} 个部门。删除受两条硬约束：<b>有子部门不可删</b>、<b>有用户不可删</b>（后端强制，前端已置灰可预判的部分）。
        移动部门时后端会<b>级联重写全部子孙的 ancestors</b>，并禁止把部门移到它自己或它的子部门下（防成环）。
      </p>
    </section>

    <!-- ==================== 新增 / 编辑 ==================== -->
    <div v-if="edit.show" class="adm-mask" @click.self="edit.show = false">
      <div class="adm-modal">
        <h3>{{ edit.isCreate ? '新增部门' : '编辑部门' }}</h3>
        <p class="adm-modal-sub">所属部门决定用户的<b>数据可见范围</b>与管理归属；改上级会自动重算本部门及全部子孙的 ancestors。</p>

        <div class="adm-field">
          <label class="fld">上级部门</label>
          <select v-model="edit.form.parent_id" class="glass-input">
            <option :value="0">顶级部门</option>
            <option v-for="o in parentOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
        </div>

        <div class="adm-grid2">
          <div class="adm-field">
            <label class="fld">部门名称 *</label>
            <input v-model.trim="edit.form.name" class="glass-input" placeholder="如：三亚预订仓" />
          </div>
          <div class="adm-field">
            <label class="fld">负责人</label>
            <input v-model.trim="edit.form.leader" class="glass-input" placeholder="姓名" />
          </div>
          <div class="adm-field">
            <label class="fld">显示排序</label>
            <input v-model.number="edit.form.order_num" type="number" class="glass-input" />
          </div>
          <div class="adm-field">
            <label class="fld">状态</label>
            <select v-model.number="edit.form.status" class="glass-input">
              <option :value="0">正常</option>
              <option :value="1">停用</option>
            </select>
          </div>
        </div>

        <div v-if="edit.form.status === 1 && edit.curUserCount > 0" class="adm-alert warn">
          该部门当前有 <b>{{ edit.curUserCount }}</b> 个用户。停用后这些用户<b>仍旧照常计算数据范围</b>
          （否则会造成整部门 403）；停用只影响"新分配用户"与"部门选择器"。如需真正收权，请先把用户调整到其他部门。
        </div>
        <div class="adm-alert info">
          约束：同级下不允许重名（后端按 <b>(parent_id, name)</b> 判重）；不能把部门移动到它自己或它的子部门下。
        </div>

        <div class="adm-modal-foot">
          <button class="btn ghost" @click="edit.show = false">取消</button>
          <button class="btn primary" :disabled="saving" @click="submit">{{ saving ? '保存中…' : '保存' }}</button>
        </div>
      </div>
    </div>

    <!-- ==================== 班次配置（原「设置 → 部门」的上下班时间 + 配色） ==================== -->
    <div v-if="shiftDlg.show" class="adm-mask" @click.self="shiftDlg.show = false">
      <div class="adm-modal">
        <h3>班次配置 · {{ shiftDlg.deptName }}</h3>
        <p class="adm-modal-sub">
          班次的<b>上下班时间</b>决定排班生成与逾期判定；<b>颜色</b>只影响整月班表矩阵的格子观感，可随时改。
        </p>

        <div class="adm-shift-list">
          <div v-for="sc in shiftsOf(shiftDlg.deptId)" :key="sc.id" class="adm-shift-row">
            <i
              class="adm-sc-dot"
              :style="{ background: shiftColorCss(sc.color_key) }"
              title="点击更换颜色"
              @click.stop="openColorPicker(sc)"
            ></i>
            <span class="adm-shift-name">{{ sc.name }}</span>
            <span class="adm-shift-time">{{ sc.start_time }} — {{ sc.end_time }}</span>
            <span class="adm-shift-spacer"></span>
            <button
              v-if="canShiftConf"
              class="adm-link danger"
              @click="delShift(sc)"
            >删除</button>
          </div>
          <div v-if="!shiftsOf(shiftDlg.deptId).length" class="empty">该部门尚未配置班次，在下面添加一个</div>
        </div>

        <!-- 已建班次：点色点弹出的调色板 -->
        <div v-if="colorPicker" class="adm-shift-picker" @click.stop>
          <span class="adm-shift-picker-title">选择颜色（{{ colorPicker.sc.name }}）</span>
          <div class="adm-shift-picker-row">
            <i class="adm-sc-dot" :class="{ on: (colorPicker.sc.color_key || '') === '' }" style="background:#cbd5e1" title="系统默认（无自定义色）" @click="pickColor('')"></i>
            <i v-for="pc in shiftPalette" :key="pc.key" class="adm-sc-dot" :class="{ on: colorPicker.sc.color_key === pc.key }" :style="{ background: pc.css }" :title="pc.name" @click="pickColor(pc.key)"></i>
            <label class="fld" style="font-size:11px; margin:0;">自定义</label>
            <input type="color" :value="customHex(colorPicker.sc.color_key)" @input="onCustomColor($event.target.value)" class="adm-sc-color" title="选自定义颜色" />
            <input v-model="customHexText" type="text" maxlength="7" placeholder="#3b82f6" class="glass-input sc-hex" @keyup.enter="pickColor(customHexText)" />
            <button class="btn ghost sm" @click="pickColor(customHexText)">应用</button>
            <button class="btn ghost sm" @click="colorPicker = null">关闭</button>
          </div>
        </div>

        <!-- 新增班次 -->
        <div v-if="canShiftConf" class="adm-shift-add">
          <div class="adm-shift-add-row">
            <input v-model="scForm.name" class="glass-input" placeholder="班次名，如 中班" style="max-width:150px;" />
            <input v-model="scForm.start_time" type="time" class="glass-input" style="max-width:130px;" />
            <span class="adm-shift-sep">至</span>
            <input v-model="scForm.end_time" type="time" class="glass-input" style="max-width:130px;" />
            <button class="btn primary" @click="addShift(shiftDlg.deptId)">添加班次</button>
          </div>
          <div class="adm-shift-add-row">
            <span class="adm-shift-picker-title">格子色</span>
            <i class="adm-sc-dot" :class="{ on: !scForm.color_key }" style="background:#cbd5e1" title="系统默认色" @click="scForm.color_key = ''"></i>
            <i v-for="pc in shiftPalette" :key="pc.key" class="adm-sc-dot" :class="{ on: scForm.color_key === pc.key }" :style="{ background: pc.css }" :title="pc.name" @click="scForm.color_key = pc.key"></i>
            <input type="color" :value="customHex(scForm.color_key)" @input="scForm.color_key = $event.target.value" class="adm-sc-color" title="选自定义颜色" />
            <span class="adm-shift-picker-title">（用于整月班表矩阵的格子）</span>
          </div>
        </div>
        <div v-else class="adm-alert warn">
          你没有 <b>schedule:shiftconfig</b> 权限，此处只能查看班次，无法新增/删除/改色。
        </div>

        <div class="adm-modal-foot">
          <button class="btn ghost" @click="shiftDlg.show = false">关闭</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import * as api from '@/api'
import { useAuthStore } from '@/store/auth'
import { flattenTree, treeOptions } from '@/utils/rbacTree'

const auth = useAuthStore()

const loading = ref(false)
const saving = ref(false)
const tree = ref([])
const flat = ref([])

/** 只有"正常"部门能作为父节点（后端在 CreateDept 里也拒绝停用的上级） */
const parentOptions = computed(() => {
  const active = flat.value.filter((d) => d.status === 0 && d.id !== edit.id)
  const byId = new Map(active.map((d) => [d.id, { ...d, children: [] }]))
  const roots = []
  for (const n of byId.values()) {
    const p = byId.get(n.parent_id || 0)
    if (p) p.children.push(n)
    else roots.push(n)
  }
  return treeOptions(flattenTree(roots), 'name')
})

async function load() {
  loading.value = true
  try {
    const r = await api.get('/system/dept/tree')
    tree.value = r.tree || []
    flat.value = flattenTree(tree.value, 0, [], 0)
  } catch (e) {
    tree.value = []
    flat.value = []
    alert(errMsg(e, '加载部门失败'))
  } finally { loading.value = false }
}

function blockReason(d) {
  if (d.children?.length) return `有 ${d.children.length} 个子部门，需先删除子部门`
  if (d.user_count > 0) return `有 ${d.user_count} 个用户，需先调整用户所属部门`
  return ''
}

// ---------------------------------------------------------------- 新增 / 编辑
const edit = reactive({
  show: false, isCreate: true, id: 0, curUserCount: 0,
  form: { parent_id: 0, name: '', leader: '', order_num: 0, status: 0 }
})

function openCreate(parent) {
  Object.assign(edit, {
    show: true, isCreate: true, id: 0, curUserCount: 0,
    form: { parent_id: parent ? parent.id : 0, name: '', leader: '', order_num: 0, status: 0 }
  })
}

function openEdit(d) {
  Object.assign(edit, {
    show: true, isCreate: false, id: d.id, curUserCount: d.user_count || 0,
    form: {
      parent_id: d.parent_id || 0, name: d.name, leader: d.leader || '',
      order_num: d.order_num || 0, status: d.status || 0
    }
  })
}

async function submit() {
  const f = edit.form
  if (!f.name) { alert('请填写部门名称'); return }
  if (!edit.isCreate && f.parent_id === edit.id) { alert('上级部门不能是自己'); return }
  saving.value = true
  try {
    if (edit.isCreate) {
      await api.post('/system/dept', { parent_id: f.parent_id, name: f.name, leader: f.leader, order_num: f.order_num, status: f.status })
    } else {
      await api.put('/system/dept', { id: edit.id, parent_id: f.parent_id, name: f.name, leader: f.leader, order_num: f.order_num, status: f.status })
    }
    edit.show = false
    await load()
  } catch (e) {
    alert(errMsg(e, '保存失败'))
  } finally { saving.value = false }
}

async function doDelete(d) {
  if (!confirm(`确定删除部门「${d.name}」吗？\n有子部门或有用户时后端会拒绝删除。`)) return
  try {
    await api.post(`/system/dept/delete/${d.id}`)
    await load()
  } catch (e) {
    alert(errMsg(e, '删除失败'))
  }
}

// ============================================================================
// 班次配置（v0.39.1 从「设置 → 部门」并入）
//
// 权限：读 GET /api/shift-configs 是登录级；写（POST/DELETE）要求
//       schedule:shiftconfig —— 因此班次配置按钮与新增/删除/改色都按它门控。
// ============================================================================
const canShiftConf = computed(() => auth.can('schedule:shiftconfig'))

const shiftConfigs = ref([])
const shiftDlg = reactive({ show: false, deptId: 0, deptName: '' })
const scForm = reactive({ name: '', start_time: '09:00', end_time: '18:00', color_key: '' })

async function loadShiftConfigs() {
  try {
    const r = await api.get('/shift-configs')
    shiftConfigs.value = Array.isArray(r) ? r : []
  } catch (e) { shiftConfigs.value = [] }
}
function shiftsOf(deptId) { return shiftConfigs.value.filter((sc) => sc.dept_id === deptId) }

function openShifts(d) {
  Object.assign(shiftDlg, { show: true, deptId: d.id, deptName: d.name })
  colorPicker.value = null
  scForm.name = ''
  scForm.color_key = ''
  loadShiftConfigs()
}

// 整月矩阵班次配色色板（key 与 Schedule 矩阵色类对应；空 = 系统默认色）
// 自定义颜色：color_key 以 '#' 开头视为 hex 直渲；否则按预定义 key 走映射表
const SHIFT_COLOR_MAP = { blue: '#4f46e5', green: '#059669', orange: '#d97706', purple: '#8b5cf6' }
const shiftPalette = [
  { key: 'blue', name: '蓝（默认早班色）', css: '#4f46e5' },
  { key: 'green', name: '绿', css: '#059669' },
  { key: 'orange', name: '橙（晚班默认色）', css: '#d97706' },
  { key: 'purple', name: '紫（夜班默认色）', css: '#8b5cf6' }
]
function shiftColorCss(key) {
  if (!key) return '#cbd5e1'
  if (key.startsWith('#')) return key
  return SHIFT_COLOR_MAP[key] || '#cbd5e1'
}
function customHex(key) {
  if (key && /^#[0-9a-fA-F]{6}$/.test(key)) return key
  return '#4f46e5'
}

const colorPicker = ref(null) // { sc }
const customHexText = ref('#4f46e5')
function openColorPicker(sc) {
  if (!canShiftConf.value) return
  customHexText.value = /^#[0-9a-fA-F]{6}$/.test(sc.color_key || '') ? sc.color_key : '#4f46e5'
  colorPicker.value = { sc }
}
function onCustomColor(hex) {
  if (!colorPicker.value) return
  customHexText.value = hex
  pickColor(hex)
}
async function pickColor(key) {
  if (!colorPicker.value) return
  const sc = colorPicker.value.sc
  try {
    await api.post('/shift-configs', {
      id: sc.id, dept_id: sc.dept_id, name: sc.name,
      start_time: sc.start_time, end_time: sc.end_time, color_key: key || ''
    })
    await loadShiftConfigs()
    colorPicker.value = null
  } catch (e) { alert(errMsg(e, '改色失败')) }
}

async function addShift(deptId) {
  const name = scForm.name.trim()
  if (!name) { alert('请填写班次名称，如 中班'); return }
  if (!scForm.start_time || !scForm.end_time) { alert('请选择上班/下班时间'); return }
  try {
    await api.post('/shift-configs', {
      dept_id: deptId, name,
      start_time: scForm.start_time, end_time: scForm.end_time, color_key: scForm.color_key
    })
    scForm.name = ''
    scForm.color_key = ''
    await loadShiftConfigs()
  } catch (e) { alert(errMsg(e, '添加失败')) }
}

async function delShift(sc) {
  if (!confirm(`删除班次「${sc.name} ${sc.start_time}-${sc.end_time}」？\n已排到该班次的班表不会自动重算。`)) return
  try {
    await api.del(`/shift-configs/${sc.id}`)
    await loadShiftConfigs()
  } catch (e) { alert(errMsg(e, '删除失败')) }
}

function errMsg(e, dft) {
  return (e && e.response && e.response.data && e.response.data.error) || dft
}

onMounted(async () => {
  await Promise.all([load(), loadShiftConfigs()])
})
</script>
