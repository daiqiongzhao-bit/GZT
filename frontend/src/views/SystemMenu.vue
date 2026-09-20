<template>
  <div class="adm-page">
    <div class="adm-head">
      <h2 class="section-title">
        菜单管理
        <span class="section-sub">目录（分组）→ 菜单（页面）→ 按钮（权限点）；按钮的「权限标识」必须与后端接口声明逐字符一致</span>
      </h2>
      <div class="adm-actions">
        <button class="btn ghost" :disabled="loading" @click="load">{{ loading ? '刷新中…' : '刷新' }}</button>
        <button v-if="auth.can('system:menu:add')" class="btn primary" @click="openCreate(null)">新增菜单</button>
      </div>
    </div>

    <!-- ==================== 一致性自检（默认一行，明细按需展开） ==================== -->
    <section class="panel">
      <div class="rep-bar" :class="rep.ok ? 'ok' : 'bad'">
        <span class="rep-dot"></span>
        <span class="rep-title">{{ rep.ok ? '权限一致' : '发现差异' }}</span>
        <span class="rep-meta">
          接口 {{ rep.declared.length }} 条 · 菜单按钮 {{ rep.in_menu.length }} 条 · 差异 {{ diffTotal }} 条
        </span>
        <span class="rep-spacer"></span>
        <span class="chip" :class="enforceChip" :title="'当前拦截模式：' + enforceLabel">{{ enforceShort }}</span>
        <button class="adm-link" @click="repOpen = !repOpen">
          {{ repOpen ? '收起明细 ▴' : '查看明细 ▾' }}
        </button>
        <button class="adm-link" :disabled="loadingRep" @click="loadReport">{{ loadingRep ? '自检中…' : '重新自检' }}</button>
      </div>

      <p v-if="!rep.ok" class="adm-hint">
        菜单权限与接口声明对不齐 —— 标红的分组就是需要处理的地方，展开明细看清单。
      </p>

      <div v-if="repOpen" class="rep-body">
        <div class="rep-groups">
          <div v-for="g in repGroups" :key="g.key" class="rep-group" :class="{ zero: !g.items.length }">
            <div class="rep-g-head">
              <span class="rep-g-name">{{ g.name }}</span>
              <span class="rep-g-badge" :class="{ zero: !g.items.length }">{{ g.items.length }}</span>
            </div>
            <p class="rep-g-desc">{{ g.desc }}</p>
            <ul v-if="g.items.length" class="rep-g-list">
              <li v-for="p in g.items" :key="p"><code>{{ p }}</code></li>
            </ul>
            <p v-else class="rep-g-none">无</p>
          </div>
        </div>

        <div class="rep-foot">
          <p class="adm-hint">
            接口权限以<b>代码为唯一事实来源</b>：代码里没声明的接口一律<b>默认拒绝</b>。
            所以「加了接口忘了加按钮」会在这里暴露，而不是悄悄放行。
          </p>
          <div v-if="auth.isSuper" class="adm-enforce">
            <span class="fld">拦截模式（仅超级管理员可改）</span>
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
              <b>log</b> 只记日志不拦截（灰度观察期）→ <b>on</b> 正式拦截 → <b>off</b> 完全关闭。
              建议先用 log 观察一段，确认审计日志里没有误伤再切 on。
            </p>
          </div>
          <p v-else class="adm-hint">当前拦截模式：{{ enforceLabel }}（切换需要超级管理员）</p>
        </div>
      </div>
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

      <div class="adm-tablewrap" @click="opsOpen = 0">
        <table class="adm-table menu-table">
          <thead>
            <tr>
              <th>菜单名称</th>
              <th>权限标识</th>
              <th>路由 / 组件</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="m in visibleRows" :key="m.id">
              <td class="m-name">
                <span class="m-guides" aria-hidden="true"><i v-for="d in m.depth" :key="d"></i></span>
                <button
                  v-if="hasChild(m)"
                  class="m-fold"
                  :title="isFolded(m.id) ? '展开子节点' : '收起子节点'"
                  @click.stop="toggleFold(m.id)"
                >{{ isFolded(m.id) ? '▸' : '▾' }}</button>
                <span v-else class="m-fold ph"></span>
                <span class="m-label">{{ m.menu_name }}</span>
                <span class="chip" :class="typeClass(m.menu_type)">{{ typeLabel(m.menu_type) }}</span>
                <span v-if="hasChild(m)" class="m-count">{{ childCount(m.id) }}</span>
              </td>
              <td>
                <code v-if="m.perms" class="perm">{{ m.perms }}</code>
                <span v-else class="dim">—</span>
              </td>
              <td class="m-route">
                <span v-if="m.path" class="l1">{{ m.path }}</span>
                <span v-if="m.component" class="l2">{{ m.component }}</span>
                <span v-if="!m.path && !m.component" class="dim">—</span>
              </td>
              <td>
                <span v-if="m.status" class="st"><i class="off"></i>已停用</span>
                <span v-else class="st"><i class="on"></i>显示中<span v-if="m.visible" class="st-sub">隐藏</span></span>
              </td>
              <td>
                <div class="adm-row-actions">
                  <button v-if="auth.can('system:menu:edit')" class="adm-link" @click="openEdit(m)">编辑</button>

                  <div v-if="canMore(m)" class="adm-ops" @click.stop>
                    <button class="adm-link" :class="{ on: opsOpen === m.id }" @click="opsOpen = opsOpen === m.id ? 0 : m.id">更多 ▾</button>
                    <div v-if="opsOpen === m.id" class="adm-ops-drop">
                      <button
                        v-if="auth.can('system:menu:add') && m.menu_type !== 'F'"
                        class="adm-op"
                        @click="openCreate(m)"
                      >新增下级菜单</button>
                      <button
                        v-if="auth.can('system:menu:remove')"
                        class="adm-op danger"
                        :disabled="childCount(m.id) > 0"
                        :title="childCount(m.id) > 0 ? '存在子节点，需先删除子节点' : ''"
                        @click="doDelete(m)"
                      >删除该节点</button>
                    </div>
                  </div>

                  <span v-if="!auth.can('system:menu:edit') && !canMore(m)" class="dim">—</span>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!visibleRows.length" class="empty">{{ loading ? '加载中…' : '暂无菜单数据' }}</div>

      <div class="m-foot">
        <span>
          共 {{ rows.length }} 个节点 · 目录 {{ counts.M }} · 菜单 {{ counts.C }} · 按钮 {{ counts.F }}<template v-if="foldedGroups">（已收起 {{ foldedGroups }} 个分组）</template>
        </span>
        <span class="m-foot-actions">
          <button class="adm-link" :disabled="!foldableIds.length" @click="foldAll">折叠全部</button>
          <button class="adm-link" :disabled="!collapsed.length" @click="expandAll">展开全部</button>
        </span>
      </div>
    </section>

    <!-- ==================== 新增 / 编辑 ==================== -->
    <div v-if="edit.show" class="adm-mask" @click.self="edit.show = false">
      <div class="adm-modal wide">
        <h3>{{ edit.isCreate ? '新增菜单' : '编辑菜单' }}</h3>
        <p class="adm-modal-sub">
          三种节点：目录装菜单、菜单装按钮、按钮承载权限。选好类型后，下方只显示该类型需要的字段。
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
          按钮节点的权限标识必须唯一，且与后端接口声明一致；未声明的接口走<b>默认拒绝</b>。
          保存后列表上方的自检会立刻重跑，对不齐会直接标出来。
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
const repOpen = ref(false)      // 自检明细是否展开（默认收起，首屏只留一行）
const opsOpen = ref(0)          // 行内「更多 ▾」当前展开的行 id

const q = reactive({ menu_name: '', menu_type: '' })
const rows = ref([])     // 后端返回的扁平 SysMenu 列表（带 parent_id）
const flat = ref([])     // 建树后再扁平化 → 带 depth / children，用于树形展示

// ---------------------------------------------------------------- 折叠
// 默认把「子节点全是按钮」的菜单折叠起来：按钮数量最多、最占行，
// 而它们的存在感靠父行右侧的数量角标就能表达。
const collapsed = ref([])
const foldTouched = ref(false)
function isFolded(id) { return collapsed.value.includes(id) }

function toggleFold(id) {
  foldTouched.value = true
  const i = collapsed.value.indexOf(id)
  if (i >= 0) collapsed.value.splice(i, 1)
  else collapsed.value.push(id)
}
function foldAll() { foldTouched.value = true; collapsed.value = [...foldableIds.value] }
function expandAll() { foldTouched.value = true; collapsed.value = [] }

/** 可折叠 = 有子节点 */
const foldableIds = computed(() => rows.value.filter((n) => childCount(n.id) > 0).map((n) => n.id))
const foldedGroups = computed(() => collapsed.value.filter((id) => childCount(id) > 0).length)

function defaultFolded() {
  const byParent = new Map()
  for (const n of rows.value) {
    const p = n.parent_id || 0
    if (!byParent.has(p)) byParent.set(p, [])
    byParent.get(p).push(n)
  }
  const out = []
  for (const n of rows.value) {
    const kids = byParent.get(n.id) || []
    if (kids.length && kids.every((c) => c.menu_type === 'F')) out.push(n.id)
  }
  return out
}

/** 按折叠状态过滤后的可见行（前序遍历，父行折叠则整支跳过） */
const visibleRows = computed(() => {
  const hidden = new Set()
  const out = []
  for (const n of flat.value) {
    if (hidden.has(n.parent_id ?? 0)) { hidden.add(n.id); continue }
    out.push(n)
    if (isFolded(n.id) && childCount(n.id) > 0) hidden.add(n.id)
  }
  return out
})

function hasChild(m) { return childCount(m.id) > 0 }

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
const enforceShort = computed(() => {
  const m = auth.permEnforce
  return m === 'on' ? '拦截：on' : m === 'off' ? '拦截：off' : m === 'log' ? '拦截：log' : '拦截：…'
})
const enforceChip = computed(() => (auth.permEnforce === 'on' ? 'ok' : auth.permEnforce === 'off' ? 'danger' : 'warn'))

/** 三类差异：全部用中文说明，不再暴露 only_in_menu / only_declared 这类字段名 */
const repGroups = computed(() => [
  {
    key: 'inmenu',
    name: '菜单多配了（接口没校验）',
    desc: '菜单里有这个权限点，但后端接口没声明 —— 越权风险。要么删掉按钮，要么给接口补上声明。',
    items: rep.only_in_menu
  },
  {
    key: 'declared',
    name: '接口没配菜单（功能不可达）',
    desc: '后端接口要求这个权限，但菜单里没有对应按钮 —— 角色配置里勾不到，表现为功能点不亮。',
    items: rep.only_declared
  },
  {
    key: 'runtime',
    name: '运行期未声明接口（默认拒绝）',
    desc: '实际被请求到、但代码里没有声明的接口。默认拒绝策略下会被拦，通常是新增接口漏了声明。',
    items: rep.undeclared_api
  }
])
const diffTotal = computed(() => rep.only_in_menu.length + rep.only_declared.length + rep.undeclared_api.length)

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

/** 「更多」里是否有可放的动作 */
function canMore(m) {
  return (auth.can('system:menu:add') && m.menu_type !== 'F') || auth.can('system:menu:remove')
}

async function load() {
  loading.value = true
  try {
    const params = {}
    if (q.menu_name) params.menu_name = q.menu_name
    if (q.menu_type) params.menu_type = q.menu_type
    const r = await api.get('/system/menu/list', params)
    rows.value = r.list || []
    flat.value = flattenTree(buildTree(rows.value))
    // 用户手动折过就尊重用户的选择；否则按默认策略收起「纯按钮分组」
    if (!foldTouched.value) collapsed.value = defaultFolded()
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
  opsOpen.value = 0
  Object.assign(edit, { show: true, isCreate: true, id: 0, form: blankForm() })
  if (parent) {
    edit.form.parent_id = parent.id
    // 目录下默认建菜单，菜单下默认建按钮（贴合实际使用）
    edit.form.menu_type = parent.menu_type === 'M' ? 'C' : 'F'
  }
}

function openEdit(m) {
  opsOpen.value = 0
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
  if (f.menu_type === 'F' && !f.perms) { alert('按钮节点（F）必须填写权限标识'); return }
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
  opsOpen.value = 0
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

<style scoped>
/* ---- 树形名称列：层级引导线 + 折叠三角 ---- */
.menu-table th:first-child { min-width: 268px; }
.m-name { display: flex; align-items: center; gap: 6px; }
.m-guides { display: flex; align-self: stretch; flex: none; }
.m-guides i { display: block; width: 15px; border-right: 1px solid var(--glass-border); }
.m-fold {
  width: 17px; height: 17px; flex: none; padding: 0; border: none; border-radius: 5px;
  background: none; color: var(--text-faint); cursor: pointer; font-size: 11px; line-height: 1;
}
.m-fold:hover { background: var(--overlay); color: var(--accent); }
.m-fold.ph, .m-fold.ph:hover { cursor: default; background: none; }
.m-label { color: var(--text); }
.m-count {
  flex: none; padding: 0 6px; border-radius: 8px; background: var(--overlay);
  color: var(--text-faint); font-size: 11px; line-height: 16px;
}

/* ---- 权限标识：这一页最该一眼扫到的字段，做成高亮等宽标签 ---- */
.perm {
  padding: 2px 7px; border-radius: 6px; background: var(--accent-soft); color: var(--accent);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px;
}

/* ---- 路由 / 组件：两行小字，省掉一整列 ---- */
.m-route { display: flex; flex-direction: column; gap: 1px; }
.m-route .l1 { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; color: var(--text-dim); }
.m-route .l2 { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 11px; color: var(--text-faint); }

/* ---- 状态：一个圆点代替两个彩色 chip ---- */
.st { display: inline-flex; align-items: center; gap: 6px; font-size: 12.5px; color: var(--text-dim); white-space: nowrap; }
.st i { width: 7px; height: 7px; flex: none; border-radius: 50%; }
.st i.on { background: var(--success, #16a34a); }
.st i.off { background: var(--text-faint); }
.st-sub { padding: 0 5px; border-radius: 5px; background: var(--overlay); color: var(--text-faint); font-size: 11px; }

/* ---- 自检：一行状态条 + 折叠明细 ---- */
.rep-bar {
  display: flex; align-items: center; gap: 9px; flex-wrap: wrap;
  padding: 10px 12px; border-radius: var(--radius-sm);
  background: var(--overlay); border: 1px solid var(--glass-border);
}
.rep-bar.bad { border-color: rgba(217, 119, 6, 0.35); background: rgba(217, 119, 6, 0.10); }
.rep-dot { width: 8px; height: 8px; flex: none; border-radius: 50%; background: var(--success, #16a34a); }
.rep-bar.bad .rep-dot { background: var(--warn, #d97706); }
.rep-title { font-size: 13px; color: var(--text); }
.rep-meta { font-size: 12px; color: var(--text-faint); }
.rep-spacer { flex: 1; min-width: 8px; }
.rep-body { margin-top: 12px; }
.rep-groups { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 10px; }
.rep-group { padding: 11px 12px; border-radius: var(--radius-sm); background: var(--overlay); border: 1px solid var(--glass-border); }
.rep-group.zero { opacity: 0.7; }
.rep-g-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.rep-g-name { font-size: 12.5px; color: var(--text); }
.rep-g-badge {
  min-width: 18px; padding: 0 6px; border-radius: 9px; text-align: center;
  background: rgba(225, 29, 72, 0.14); color: var(--danger, #e11d48); font-size: 11px; line-height: 17px;
}
.rep-g-badge.zero { background: var(--overlay-2, var(--overlay)); color: var(--text-faint); }
.rep-g-desc { margin: 5px 0 0; font-size: 11.5px; color: var(--text-faint); line-height: 1.6; }
.rep-g-list { margin: 8px 0 0; padding: 0; list-style: none; max-height: 180px; overflow: auto; display: flex; flex-direction: column; gap: 4px; }
.rep-g-list code {
  display: block; padding: 3px 7px; border-radius: 6px; background: var(--bg-1);
  color: var(--text-dim); font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11.5px; word-break: break-all;
}
.rep-g-none { margin: 8px 0 0; font-size: 11.5px; color: var(--text-faint); }
.rep-foot { margin-top: 14px; padding-top: 12px; border-top: 1px solid var(--glass-border); }

/* ---- 表尾：计数 + 折叠操作 ---- */
.m-foot {
  display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap;
  margin-top: 12px; font-size: 12px; color: var(--text-faint);
}
.m-foot-actions { display: flex; gap: 4px; }
</style>
