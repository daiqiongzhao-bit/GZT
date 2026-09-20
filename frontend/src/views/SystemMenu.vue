<template>
  <div class="adm-page">
    <div class="adm-head">
      <h2 class="section-title">
        菜单管理
        <span class="section-sub">M 目录 / C 菜单 / F 按钮；F 节点的「权限标识」必须与后端接口声明逐字符一致</span>
      </h2>
      <div class="adm-actions">
        <button class="btn" :disabled="loading" @click="load">刷新</button>
        <button v-if="auth.can('system:menu:add')" class="btn primary" @click="openCreate(null)">新增菜单</button>
      </div>
    </div>

    <!-- ==================== 一致性自检 + 拦截开关 ==================== -->
    <section class="panel">
      <div class="adm-sub-head">
        <h3 class="adm-sub-title">权限一致性自检</h3>
        <div class="adm-actions">
          <span class="chip" :class="enforceChip">拦截模式：{{ enforceLabel }}</span>
          <button class="btn ghost" :disabled="loadingRep" @click="loadReport">{{ loadingRep ? '自检中…' : '重新自检' }}</button>
        </div>
      </div>

      <p class="adm-hint">
        接口权限以<b>代码为唯一事实来源</b>（当前声明 {{ rep.declared.length }} 条）；菜单里的 F 节点必须与之完全对齐。
        <b>only_in_menu</b> = 菜单配了但接口没校验 —— 这就是越权口子；
        <b>only_declared</b> = 接口要权限但菜单没配 —— 表现为功能不可达。两者都应为 0。
      </p>

      <div v-if="rep.ok" class="adm-alert info">
        ✅ 自检通过：接口权限声明与菜单权限节点完全一致，无越权口子。
      </div>
      <div v-else class="adm-alert warn">
        ⚠️ 自检发现差异（可能是新增了接口/菜单但未同步），请核对下方清单。
      </div>

      <div v-if="!rep.ok || rep.undeclared_api.length" class="adm-report">{{ reportText }}</div>

      <div v-if="auth.isSuper" class="adm-enforce">
        <span class="fld">全局拦截开关（仅超级管理员可见可改）</span>
        <div class="adm-actions">
          <button
            v-for="m in MODES"
            :key="m.v"
            class="btn"
            :class="{ primary: auth.permEnforce === m.v }"
            :disabled="savingMode !== ''"
            @click="setMode(m.v)"
          >{{ m.label }}</button>
        </div>
        <p class="adm-hint">
          <b>log</b> = 只记日志不拦截（灰度观察期，先看清"谁会被拦住"）→ <b>on</b> = 正式拦截 → <b>off</b> = 完全回退旧行为。
          建议先 log 观察一段时间，确认审计日志里没有误伤，再切 on。
        </p>
      </div>
      <p v-else class="adm-hint">当前拦截模式：{{ enforceLabel }}（切换拦截开关需要超级管理员）</p>
    </section>

    <!-- ==================== 菜单树表 ==================== -->
    <section class="panel">
      <div class="adm-toolbar">
        <div class="fld-wrap">
          <label class="fld">菜单名称</label>
          <input v-model.trim="q.menu_name" class="glass-input wide" placeholder="模糊匹配" @keyup.enter="load" />
        </div>
        <div class="fld-wrap">
          <label class="fld">类型</label>
          <select v-model="q.menu_type" class="glass-input narrow">
            <option value="">全部</option>
            <option value="M">M 目录</option>
            <option value="C">C 菜单</option>
            <option value="F">F 按钮</option>
          </select>
        </div>
        <button class="btn primary" @click="load">查询</button>
        <button class="btn ghost" @click="resetQuery">重置</button>
      </div>

      <div class="adm-tablewrap">
        <table class="adm-table">
          <thead>
            <tr>
              <th>菜单名称</th>
              <th>类型</th>
              <th>路由地址</th>
              <th>组件路径</th>
              <th>权限标识</th>
              <th class="num">排序</th>
              <th>显示</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="m in flat" :key="m.id">
              <td>
                <span class="adm-indent" :style="{ width: m.depth * 18 + 'px' }"></span>
                <span v-if="m.depth" class="adm-tree-mark">└</span>
                {{ m.menu_name }}
              </td>
              <td><span class="chip" :class="typeClass(m.menu_type)">{{ typeLabel(m.menu_type) }}</span></td>
              <td class="mono dim">{{ m.path || '—' }}</td>
              <td class="mono dim">{{ m.component || '—' }}</td>
              <td class="mono">{{ m.perms || '—' }}</td>
              <td class="num dim">{{ m.order_num }}</td>
              <td>
                <span v-if="m.visible" class="chip">隐藏</span>
                <span v-else class="chip ok">显示</span>
              </td>
              <td>
                <span v-if="m.status" class="chip danger">停用</span>
                <span v-else class="chip ok">正常</span>
              </td>
              <td>
                <div class="adm-row-actions">
                  <button
                    v-if="auth.can('system:menu:add') && m.menu_type !== 'F'"
                    class="adm-link"
                    @click="openCreate(m)"
                  >新增下级</button>
                  <button v-if="auth.can('system:menu:edit')" class="adm-link" @click="openEdit(m)">编辑</button>
                  <button
                    v-if="auth.can('system:menu:remove')"
                    class="adm-link danger"
                    :disabled="childCount(m.id) > 0"
                    :title="childCount(m.id) > 0 ? '存在子节点，需先删除子节点' : ''"
                    @click="doDelete(m)"
                  >删除</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!flat.length" class="empty">{{ loading ? '加载中…' : '暂无菜单数据' }}</div>
      <p class="adm-hint">
        共 {{ flat.length }} 个节点（M 目录 {{ counts.M }} / C 菜单 {{ counts.C }} / F 按钮 {{ counts.F }}）。
        删除受两条约束：有子节点不可删、仍被角色引用不可删（后端强制，前端已置灰可预判的部分）。
      </p>
    </section>

    <!-- ==================== 新增 / 编辑 ==================== -->
    <div v-if="edit.show" class="adm-mask" @click.self="edit.show = false">
      <div class="adm-modal wide">
        <h3>{{ edit.isCreate ? '新增菜单' : '编辑菜单' }}</h3>
        <p class="adm-modal-sub">
          F（按钮）节点是功能权限的唯一载体：它的「权限标识」必须与后端 routeperm.go 中该接口的声明完全一致，
          否则一致性自检会报警（only_in_menu / only_declared）。
        </p>

        <div class="adm-grid2">
          <div class="adm-field">
            <label class="fld">上级菜单</label>
            <select v-model="edit.form.parent_id" class="glass-input">
              <option :value="0">顶级（根目录）</option>
              <option v-for="o in parentOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </div>
          <div class="adm-field">
            <label class="fld">菜单类型 *</label>
            <select v-model="edit.form.menu_type" class="glass-input">
              <option value="M">M 目录（分组容器）</option>
              <option value="C">C 菜单（可路由页面）</option>
              <option value="F">F 按钮（功能权限点）</option>
            </select>
          </div>
          <div class="adm-field">
            <label class="fld">菜单名称 *</label>
            <input v-model.trim="edit.form.menu_name" class="glass-input" placeholder="如：用户管理" />
          </div>
          <div class="adm-field">
            <label class="fld">显示排序</label>
            <input v-model.number="edit.form.order_num" type="number" class="glass-input" placeholder="越小越靠前" />
          </div>
          <div v-if="edit.form.menu_type !== 'F'" class="adm-field">
            <label class="fld">路由地址</label>
            <input v-model.trim="edit.form.path" class="glass-input" placeholder="如：/system/users" />
          </div>
          <div v-if="edit.form.menu_type === 'C'" class="adm-field">
            <label class="fld">组件路径</label>
            <input v-model.trim="edit.form.component" class="glass-input" placeholder="如：SystemUser" />
          </div>
          <div v-if="edit.form.menu_type !== 'M'" class="adm-field">
            <label class="fld">权限标识{{ edit.form.menu_type === 'F' ? ' *' : '（可选）' }}</label>
            <input v-model.trim="edit.form.perms" class="glass-input" placeholder="模块:资源:操作，如 system:user:add" />
          </div>
          <div v-if="edit.form.menu_type !== 'F'" class="adm-field">
            <label class="fld">图标</label>
            <input v-model.trim="edit.form.icon" class="glass-input" placeholder="内置图标名，如 users" />
          </div>
        </div>

        <div v-if="edit.form.menu_type !== 'F'" class="adm-grid2">
          <div class="adm-field">
            <label class="fld">显示状态</label>
            <select v-model.number="edit.form.visible" class="glass-input">
              <option :value="0">显示</option>
              <option :value="1">隐藏（仍在路由中，不出现在导航）</option>
            </select>
          </div>
          <div class="adm-field">
            <label class="fld">菜单状态</label>
            <select v-model.number="edit.form.status" class="glass-input">
              <option :value="0">正常</option>
              <option :value="1">停用（不参与权限判定）</option>
            </select>
          </div>
        </div>
        <div v-else class="adm-field">
          <label class="fld">菜单状态</label>
          <select v-model.number="edit.form.status" class="glass-input">
            <option :value="0">正常</option>
            <option :value="1">停用（不参与权限判定）</option>
          </select>
        </div>

        <div class="adm-alert info">
          约束：F 节点必须填权限标识，且权限标识唯一（重复会被拒绝）；未在表中声明的接口走
          <b>默认拒绝</b> 策略，所以"加了接口忘了加按钮"会在自检里暴露，而不是悄悄放行。
        </div>

        <div class="adm-modal-foot">
          <button class="btn ghost" @click="edit.show = false">取消</button>
          <button class="btn primary" :disabled="saving" @click="submit">{{ saving ? '保存中…' : '保存' }}</button>
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
const loadingRep = ref(false)
const saving = ref(false)
const savingMode = ref('')

const q = reactive({ menu_name: '', menu_type: '' })
const rows = ref([])     // 后端返回的扁平 SysMenu 列表（带 parent_id）
const flat = ref([])     // 建树后再扁平化 → 带 depth，用于缩进展示

// ---------------------------------------------------------------- 一致性自检
const rep = reactive({ declared: [], in_menu: [], only_declared: [], only_in_menu: [], undeclared_api: [], ok: false })

const MODES = [
  { v: 'log', label: 'log 只记不拦' },
  { v: 'on', label: 'on 正式拦截' },
  { v: 'off', label: 'off 关闭校验' }
]
const enforceLabel = computed(() => {
  const m = auth.permEnforce
  if (m === 'on') return 'on（正式拦截）'
  if (m === 'off') return 'off（关闭校验）'
  if (m === 'log') return 'log（只记日志不拦截）'
  return '加载中…'
})
const enforceChip = computed(() => (auth.permEnforce === 'on' ? 'ok' : auth.permEnforce === 'off' ? 'danger' : 'warn'))

const reportText = computed(() => {
  const L = []
  L.push(`接口声明 ${rep.declared.length} 条 / 菜单 F 节点 ${rep.in_menu.length} 条`)
  if (rep.only_in_menu.length) {
    L.push('')
    L.push(`★ 菜单配了但接口没校验（越权口子，${rep.only_in_menu.length} 条）：`)
    rep.only_in_menu.forEach((p) => L.push('   ' + p))
  }
  if (rep.only_declared.length) {
    L.push('')
    L.push(`接口要权限但菜单没配（功能不可达，${rep.only_declared.length} 条）：`)
    rep.only_declared.forEach((p) => L.push('   ' + p))
  }
  if (rep.undeclared_api.length) {
    L.push('')
    L.push(`运行期遇到的未声明接口（默认拒绝，${rep.undeclared_api.length} 条）：`)
    rep.undeclared_api.forEach((p) => L.push('   ' + p))
  }
  return L.join('\n')
})

async function loadReport() {
  loadingRep.value = true
  try {
    const r = await api.get('/system/perm/consistency')
    rep.declared = r.declared || []
    rep.in_menu = r.in_menu || []
    rep.only_declared = r.only_declared || []
    rep.only_in_menu = r.only_in_menu || []
    rep.undeclared_api = r.undeclared_api || []
    rep.ok = !!r.ok
  } catch (e) {
    alert(errMsg(e, '自检失败'))
  } finally { loadingRep.value = false }
}

async function setMode(mode) {
  if (mode === auth.permEnforce) return
  const tip = mode === 'on'
    ? '切换到 on 后会「真正拦截」无权限请求。\n请确认已用 log 模式观察过、审计日志里没有误伤，再继续。'
    : mode === 'off' ? '切换到 off 会关闭全部权限校验（回退旧行为）。' : '切换到 log：只记录不拦截。'
  if (!confirm(tip + '\n\n确定切换吗？')) return
  savingMode.value = mode
  try {
    const r = await api.post('/system/perm/enforce', { mode })
    auth.permEnforce = r.mode || mode
  } catch (e) {
    alert(errMsg(e, '切换失败'))
  } finally { savingMode.value = '' }
}

// ---------------------------------------------------------------- 列表
function typeLabel(t) { return t === 'M' ? '目录' : t === 'C' ? '菜单' : t === 'F' ? '按钮' : t }
function typeClass(t) { return t === 'F' ? 'warn' : t === 'M' ? 'danger' : 'ok' }

/** 用 parent_id 建树（后端 list 已是扁平且按 parent_id,order_num 排序） */
function buildTree(list) {
  const byId = new Map()
  for (const n of list) byId.set(n.id, { ...n, children: [] })
  const roots = []
  for (const n of byId.values()) {
    const p = byId.get(n.parent_id || 0)
    if (p) p.children.push(n)
    else roots.push(n)
  }
  return roots
}

const childCountMap = computed(() => {
  const m = new Map()
  for (const n of rows.value) m.set(n.parent_id || 0, (m.get(n.parent_id || 0) || 0) + 1)
  return m
})
function childCount(id) { return childCountMap.value.get(id) || 0 }

const counts = computed(() => {
  const c = { M: 0, C: 0, F: 0 }
  for (const n of rows.value) if (c[n.menu_type] !== undefined) c[n.menu_type]++
  return c
})

/** 只有 M/C 能作为父节点（F 是叶子） */
const parentOptions = computed(() => {
  const parents = rows.value.filter((n) => n.menu_type === 'M' || n.menu_type === 'C')
  const tree = buildTree(parents)
  return treeOptions(flattenTree(tree), 'menu_name')
})

async function load() {
  loading.value = true
  try {
    const params = {}
    if (q.menu_name) params.menu_name = q.menu_name
    if (q.menu_type) params.menu_type = q.menu_type
    const r = await api.get('/system/menu/list', params)
    rows.value = r.list || []
    flat.value = flattenTree(buildTree(rows.value))
  } catch (e) {
    rows.value = []
    flat.value = []
    alert(errMsg(e, '加载菜单失败'))
  } finally { loading.value = false }
}
function resetQuery() { q.menu_name = ''; q.menu_type = ''; load() }

// ---------------------------------------------------------------- 新增 / 编辑
const edit = reactive({
  show: false, isCreate: true, id: 0,
  form: blankForm()
})

function blankForm() {
  return {
    parent_id: 0, menu_name: '', menu_type: 'C', order_num: 0,
    path: '', component: '', perms: '', icon: '', visible: 0, status: 0,
    query: '', is_frame: 0, is_cache: 0
  }
}

function openCreate(parent) {
  Object.assign(edit, { show: true, isCreate: true, id: 0, form: blankForm() })
  if (parent) {
    edit.form.parent_id = parent.id
    // 目录下默认建菜单，菜单下默认建按钮（贴合实际使用）
    edit.form.menu_type = parent.menu_type === 'M' ? 'C' : 'F'
  }
}

function openEdit(m) {
  Object.assign(edit, {
    show: true, isCreate: false, id: m.id,
    form: {
      parent_id: m.parent_id || 0, menu_name: m.menu_name, menu_type: m.menu_type,
      order_num: m.order_num || 0, path: m.path || '', component: m.component || '',
      perms: m.perms || '', icon: m.icon || '', visible: m.visible || 0, status: m.status || 0,
      query: m.query || '', is_frame: m.is_frame || 0, is_cache: m.is_cache || 0
    }
  })
}

async function submit() {
  const f = edit.form
  // 客户端预校验（与后端 validateMenuSave 同规则，提前给出人话提示）
  if (!f.menu_name) { alert('请填写菜单名称'); return }
  if (f.menu_type === 'F' && !f.perms) { alert('F（按钮）节点必须填写权限标识'); return }
  if (f.perms && !f.perms.includes('*') && f.perms.split(':').length < 3) {
    alert('权限标识需为 模块:资源:操作 三段式，如 system:user:add')
    return
  }
  if (!edit.isCreate && f.parent_id === edit.id) { alert('上级菜单不能是自己'); return }

  saving.value = true
  try {
    const payload = { ...f }
    if (edit.isCreate) await api.post('/system/menu', payload)
    else await api.put('/system/menu', { ...payload, id: edit.id })
    edit.show = false
    await Promise.all([load(), loadReport()]) // 菜单变更会影响一致性 → 顺手重跑自检
  } catch (e) {
    alert(errMsg(e, '保存失败'))
  } finally { saving.value = false }
}

async function doDelete(m) {
  if (!confirm(`确定删除「${m.menu_name}」吗？\n若该权限节点仍被角色引用，后端会拒绝删除。`)) return
  try {
    await api.post(`/system/menu/delete/${m.id}`)
    await Promise.all([load(), loadReport()])
  } catch (e) {
    alert(errMsg(e, '删除失败'))
  }
}

function errMsg(e, dft) {
  return (e && e.response && e.response.data && e.response.data.error) || dft
}

onMounted(async () => {
  await Promise.all([load(), loadReport()])
})
</script>
