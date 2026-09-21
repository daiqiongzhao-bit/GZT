<template>
  <div class="adm-page">
    <div class="adm-head">
      <h2 class="section-title">
        角色管理
        <span class="section-sub">角色是唯一的授权枢纽：同时承载「能做什么」（功能权限）与「能看谁」（数据范围）</span>
      </h2>
      <div class="adm-actions">
        <button class="btn" :disabled="loading" @click="load">{{ loading ? '刷新中…' : '刷新' }}</button>
        <button v-if="auth.can('system:role:add')" class="btn primary" @click="openCreate">新增角色</button>
      </div>
    </div>

    <section class="panel">
      <!-- 说明默认收起：新人不需要一上来读三大段文字 -->
      <div class="adm-fold">
        <button class="adm-fold-head" @click="helpOpen = !helpOpen">
          <span class="tri">{{ helpOpen ? '▾' : '▸' }}</span>怎么用「角色」？点这里展开说明
        </button>
        <div v-if="helpOpen" class="adm-fold-body">
          <p><b>一个用户可以有多个角色</b>，权限取并集：例如同时挂「排班主管」和「知识库编辑」，就能做两件事。
            在「用户管理 → 编辑」里多选角色即可。</p>
          <p><b>功能权限与数据范围是两条独立的链</b>：只勾功能不设数据范围 = 进得去页面但看不到数据；
            只设数据范围不勾功能 = 看得到数据但进不去功能。两者相交才是最终可见内容。</p>
          <p><b>权限粒度到动作级</b>：每个模块下的「查看 / 新增 / 修改 / 删除 / 导入 / 导出」都能单独勾选。</p>
        </div>
      </div>

      <div class="adm-tablewrap" @click="opsOpen = 0">
        <table class="adm-table role-table">
          <thead>
            <tr>
              <th>角色名称</th>
              <th>数据范围</th>
              <th class="num">成员</th>
              <th>权限开通</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in rows" :key="r.id">
              <td class="rl-name">
                <span class="rl-title">
                  {{ r.role_name }}
                  <span v-if="r.builtin" class="chip accent rl-builtin">内置</span>
                </span>
                <span v-if="r.remark" class="rl-remark">{{ r.remark }}</span>
              </td>
              <td>
                <span class="rl-scope">{{ scopeName(r.data_scope) }}</span>
                <span v-if="r.data_scope === 2" class="rl-sub">{{ r.dept_count || 0 }} 个部门</span>
              </td>
              <td class="num">{{ r.user_count }}</td>
              <td>
                <span class="rl-bar-wrap">
                  <span class="rl-bar"><i :style="{ width: barPct(r) + '%' }"></i></span>
                  <span class="rl-bar-n">{{ r.menu_count }}<template v-if="barTotal"> / {{ barTotal }}</template></span>
                </span>
              </td>
              <td>
                <span class="rl-st" :class="{ off: r.status !== 0 }">
                  <i></i>{{ r.status === 0 ? '正常' : '已停用' }}
                </span>
              </td>
              <td>
                <div class="adm-row-actions">
                  <button
                    v-if="auth.can('system:role:menu')"
                    class="adm-link"
                    @click="openPerm(r)"
                  >权限配置</button>
                  <button v-if="auth.can('system:role:edit')" class="adm-link" @click="openEdit(r)">编辑</button>

                  <div v-if="canMore" class="adm-ops" @click.stop>
                    <button class="adm-link" :class="{ on: opsOpen === r.id }" @click="opsOpen = opsOpen === r.id ? 0 : r.id">更多 ▾</button>
                    <div v-if="opsOpen === r.id" class="adm-ops-drop">
                      <button
                        v-if="auth.can('system:role:edit')"
                        class="adm-op"
                        :disabled="r.builtin"
                        :title="r.builtin ? '内置角色不可停用' : ''"
                        @click="toggleStatus(r)"
                      >{{ r.status === 0 ? '停用该角色' : '启用该角色' }}</button>
                      <button
                        v-if="auth.can('system:role:remove')"
                        class="adm-op danger"
                        :disabled="r.builtin || r.user_count > 0"
                        :title="r.builtin ? '内置角色不可删除' : (r.user_count > 0 ? '该角色下仍有 ' + r.user_count + ' 个成员，请先调整' : '')"
                        @click="doDelete(r)"
                      >删除角色</button>
                    </div>
                  </div>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!rows.length" class="empty">{{ loading ? '加载中…' : '暂无角色' }}</div>
      <p class="adm-hint">
        「权限开通」分母取所有角色里的最大值（通常是超级管理员的全部权限），数值越大表示这个角色能做的事越多。
      </p>
    </section>

    <!-- ================= 新增 / 编辑角色 ================= -->
    <div v-if="edit.show" class="adm-mask" @click.self="edit.show = false">
      <div class="adm-modal">
        <h3>{{ edit.isCreate ? '新增角色' : '编辑角色' }}</h3>
        <p class="adm-modal-sub">填一个看得懂的名字即可；系统内部标识会自动生成，无需手工维护</p>

        <div class="adm-grid2">
          <div class="adm-field">
            <label class="fld">角色名称 *</label>
            <input v-model.trim="edit.form.role_name" class="glass-input" placeholder="如：排班主管、只读查看" />
          </div>
          <div class="adm-field">
            <label class="fld">排序（越小越靠前）</label>
            <input v-model.number="edit.form.role_sort" type="number" class="glass-input" />
          </div>
        </div>

        <div class="adm-field">
          <label class="fld">可查看的部门 *</label>
          <div class="adm-radio-row">
            <label v-for="s in dataScopes" :key="s.value" class="adm-radio" :class="{ on: edit.form.data_scope === s.value }">
              <input type="radio" :value="s.value" v-model.number="edit.form.data_scope" />
              <span>
                <span class="t">{{ s.label }}</span>
                <span class="d">{{ s.desc }}</span>
              </span>
            </label>
          </div>
        </div>

        <div v-if="edit.form.data_scope === 2" class="adm-field">
          <label class="fld">勾选允许查看的部门 *（至少 1 个）</label>
          <div class="dept-pick-tools">
            <button class="btn ghost sm" @click="pickAllDepts(true)">全选</button>
            <button class="btn ghost sm" @click="pickAllDepts(false)">清空</button>
            <span class="section-sub">已选 {{ edit.form.dept_ids.length }} 个</span>
          </div>
          <DeptPicker v-model="edit.form.dept_ids" :flat="deptFlat" :quick-pick="pickDeptSubtree" />
          <p class="adm-hint">「自定义部门」只包含<b>所选部门本身</b>，不含下级；需要含下级请改选「本部门及以下」。</p>
        </div>

        <div class="adm-grid2">
          <div class="adm-field">
            <label class="fld">状态</label>
            <select v-model.number="edit.form.status" class="glass-input" :disabled="edit.builtin">
              <option :value="0">正常</option>
              <option :value="1">停用（该角色权限立即失效）</option>
            </select>
          </div>
          <div class="adm-field">
            <label class="fld">说明（给同事看的备注）</label>
            <input v-model.trim="edit.form.remark" class="glass-input" placeholder="如：负责三亚仓排班，可改班表" />
          </div>
        </div>

        <!-- 高级：默认收起（技术字段，日常不用碰） -->
        <div class="adm-fold">
          <button class="adm-fold-head" @click="advOpen = !advOpen">
            <span class="tri">{{ advOpen ? '▾' : '▸' }}</span>高级设置（系统内部标识）
          </button>
          <div v-if="advOpen" class="adm-fold-body">
            <div class="adm-field">
              <label class="fld">系统内部标识</label>
              <input
                v-model.trim="edit.form.role_key"
                class="glass-input"
                :disabled="edit.builtin"
                placeholder="留空自动生成"
              />
              <p class="adm-hint">
                程序内部用于识别这个角色的稳定字符串（英文/数字/下划线），<b>与显示无关</b>，
                创建后不建议修改 —— 改它不会影响权限，但会让外部对接方的配置失配。
                <template v-if="edit.builtin">内置角色的标识不可修改。</template>
              </p>
            </div>
          </div>
        </div>

        <div class="adm-modal-foot">
          <button class="btn ghost" @click="edit.show = false">取消</button>
          <button class="btn primary" :disabled="saving" @click="submitEdit">{{ saving ? '保存中…' : '保存' }}</button>
        </div>
      </div>
    </div>

    <!-- ================= 权限配置（核心） ================= -->
    <div v-if="perm.show" class="adm-mask" @click.self="perm.show = false">
      <div class="adm-modal wide perm-modal">
        <div class="pm-head">
          <div class="pm-head-l">
            <h3>权限配置 · {{ perm.roleName }}</h3>
            <p class="pm-sub">像文件目录一样逐级勾选：勾子项自动带上父级，取消父级会一并清掉子项</p>
          </div>
          <div class="pm-head-r">
            <span class="pm-count">已选 <b>{{ checkedCount }}</b> / {{ permIds.length }} 项</span>
          </div>
        </div>

        <div v-if="perm.superLock" class="adm-alert warn">
          超级管理员必须拥有全部权限，因此<b>不允许取消勾选</b>（防止管理员把自己锁死）。
        </div>

        <!-- 权限类型含义（默认收起，需要时展开） -->
        <div class="adm-fold compact">
          <button class="adm-fold-head" @click="kindHelpOpen = !kindHelpOpen">
            <span class="tri">{{ kindHelpOpen ? '▾' : '▸' }}</span>各类权限分别代表什么？（点开对照）
          </button>
          <div v-if="kindHelpOpen" class="adm-fold-body">
            <div class="kind-legend">
              <div v-for="k in KIND_ORDER" :key="k" class="kl-row">
                <span class="kind-badge" :class="'k-' + k">{{ KIND_LABEL[k] }}</span>
                <span class="kl-desc">{{ KIND_DESC[k] }}</span>
              </div>
            </div>
          </div>
        </div>

        <!-- 权限树：目录 / 菜单 / 按钮逐级勾选（对齐「桌面端菜单权限」的直接交互） -->
        <div class="pm-tools">
          <input v-model.trim="perm.kw" class="glass-input pm-search" placeholder="搜索菜单 / 权限标识" />
          <div class="pm-tools-r">
            <button class="btn ghost sm" @click="expandAll(true)">全部展开</button>
            <button class="btn ghost sm" @click="expandAll(false)">全部折叠</button>
            <button class="btn ghost sm" :disabled="perm.superLock" @click="checkAll(true)">全选</button>
            <button class="btn ghost sm" :disabled="perm.superLock" @click="checkAll(false)">全不选</button>
            <button class="btn ghost sm" :disabled="perm.superLock" @click="checkViewOnly()">仅保留「查看」</button>
          </div>
        </div>

        <div class="ptree">
          <div
            v-for="n in visibleTree"
            :key="n.id"
            class="pt-row"
            :class="{ 'pt-f': n.type === 'F', 'pt-m': n.type === 'M' }"
            :style="{ paddingLeft: n.depth * 22 + 'px' }"
          >
            <button
              v-if="n.children.length"
              class="pt-tri"
              :title="permExpanded.has(n.id) ? '折叠' : '展开'"
              @click="toggleExpand(n.id)"
            >{{ permExpanded.has(n.id) ? '▾' : '▸' }}</button>
            <span v-else class="pt-tri-ph"></span>
            <label class="pt-lab">
              <input
                type="checkbox"
                class="adm-check"
                :checked="perm.checked.has(n.id)"
                :indeterminate.prop="isHalf(n)"
                :disabled="perm.superLock"
                @change="toggleNode(n, $event.target.checked)"
              />
              <span class="pt-name">{{ n.name }}</span>
              <span v-if="n.type === 'F'" class="kind-badge" :class="'k-' + n.kind" :title="KIND_DESC[n.kind]">{{ KIND_LABEL[n.kind] }}</span>
              <span v-if="n.type === 'F' && n.perms" class="pt-perms">{{ n.perms }}</span>
            </label>
          </div>
          <p v-if="!visibleTree.length" class="pm-none">没有匹配「{{ perm.kw }}」的菜单或权限</p>
        </div>

        <div class="adm-modal-foot">
          <span v-if="perm.dirty" class="section-sub" style="margin-right:auto">有未保存的修改</span>
          <button class="btn ghost" @click="perm.show = false">取消</button>
          <button class="btn primary" :disabled="saving || perm.superLock" @click="submitPerm">
            {{ perm.superLock ? '超管权限不可修改' : (saving ? '保存中…' : '保存') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, h, onMounted } from 'vue'
import * as api from '@/api'
import { useAuthStore } from '@/store/auth'
import { flattenTree, ancestorsMap } from '@/utils/rbacTree'

const auth = useAuthStore()

const loading = ref(false)
const saving = ref(false)
const rows = ref([])
const dataScopes = ref([])
const opsOpen = ref(0)
const helpOpen = ref(false)
const advOpen = ref(false)
const kindHelpOpen = ref(false)

const SCOPE_NAMES = { 1: '全部数据', 2: '自定义部门', 3: '本部门', 4: '本部门及以下', 5: '仅本人' }
const scopeName = (v) => SCOPE_NAMES[v] || ('档位 ' + v)
const canMore = computed(() => auth.canAny(['system:role:edit', 'system:role:remove']))

// ---------------------------------------------------------------- 权限开通度
// 分母：优先用「打开过权限配置后得到的精确权限节点数」；
// 否则退回「所有角色里已开通数的最大值」（超管通常是全量，实践中即等于总数）。
const permTotal = ref(0)
const barTotal = computed(() => permTotal.value || Math.max(0, ...rows.value.map((r) => r.menu_count || 0)))
function barPct(r) {
  const t = barTotal.value
  if (!t) return 0
  return Math.min(100, Math.round(((r.menu_count || 0) / t) * 100))
}

// ---------------------------------------------------------------- 权限类型
// 中文名 + 含义：这是"清晰区分各项权限含义"的落点。
// 类型由权限标识的最后一段推断（后端 perms 形如 module:object:action）。
const KIND_LABEL = {
  view: '查看', add: '新增', edit: '修改', remove: '删除',
  import: '导入', export: '导出', exec: '执行', config: '配置', other: '其他'
}
const KIND_DESC = {
  view: '可打开该模块并查看列表与详情，不能改动任何数据',
  add: '可新建记录（如新增用户、新建推送任务）',
  edit: '可修改已有记录，含启用/停用、状态调整、分配关系等',
  remove: '可删除记录，删除后不可恢复',
  import: '可从 Excel / CSV 批量导入数据',
  export: '可把列表导出为 Excel 文件',
  exec: '可触发执行类动作：生成、立即运行、发送、校验、催办',
  config: '可修改该模块的配置项与参数',
  other: '其他专属动作（如重置密码、强制下线、解锁登录）'
}
const KIND_ORDER = ['view', 'add', 'edit', 'remove', 'import', 'export', 'exec', 'config', 'other']
const KIND_BY_KEY = {
  list: 'view', view: 'view', query: 'view', detail: 'view', full: 'view', tree: 'view', info: 'view', status: 'view',
  add: 'add', create: 'add', new: 'add',
  edit: 'edit', update: 'edit', set: 'edit', toggle: 'edit', rename: 'edit', move: 'edit', sort: 'edit',
  apply: 'edit', publish: 'edit', assign: 'edit', authrole: 'edit', resetpwd: 'edit', changepwd: 'edit',
  import: 'import', upload: 'import',
  export: 'export', download: 'export',
  remove: 'remove', delete: 'remove', del: 'remove', trash: 'remove', clear: 'remove',
  run: 'exec', generate: 'exec', validate: 'exec', sync: 'exec', notify: 'exec', broadcast: 'exec',
  urge: 'exec', nudge: 'exec', push: 'exec', test: 'exec', unlock: 'exec', forcelogout: 'exec', kick: 'exec',
  config: 'config', setting: 'config', manage: 'config', scopesetting: 'config', accessconfig: 'config'
}
function kindOf(perms) {
  const key = String(perms || '').split(':').pop().toLowerCase()
  return KIND_BY_KEY[key] || 'other'
}

// ---------------------------------------------------------------- 列表
async function load() {
  loading.value = true
  try {
    const r = await api.get('/system/role/list')
    rows.value = r.list || []
    if (r.data_scopes) dataScopes.value = r.data_scopes
    opsOpen.value = 0
  } catch (e) {
    rows.value = []
    alert(errMsg(e, '加载角色失败'))
  } finally { loading.value = false }
}

// ---------------------------------------------------------------- 部门选择器
const deptFlat = ref([])
const DeptPicker = {
  props: {
    modelValue: { type: Array, default: () => [] },
    flat: { type: Array, default: () => [] },
    quickPick: { type: Function, default: null }
  },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    const has = (id) => props.modelValue.includes(id)
    const toggle = (id, on) => {
      const s = new Set(props.modelValue)
      if (on) s.add(id)
      else s.delete(id)
      emit('update:modelValue', [...s])
    }
    return () => h('div', { class: 'adm-tree', style: 'max-height:280px' }, props.flat.map((d) => h('div', { class: 'adm-tree-row', key: d.id }, [
      h('span', { class: 'adm-indent', style: `width:${d.depth * 18}px` }),
      h('span', { class: 'adm-tree-mark' }, d.depth ? '└' : '▸'),
      h('label', {}, [
        h('input', {
          class: 'adm-check', type: 'checkbox', checked: has(d.id),
          onChange: (e) => toggle(d.id, e.target.checked)
        }),
        h('span', { class: 'nm' }, [
          d.name,
          d.status !== 0 ? h('span', { class: 'chip danger', style: 'margin-left:5px' }, '已停用') : null,
          d.user_count ? h('span', { class: 'chip', style: 'margin-left:5px' }, `${d.user_count} 人`) : null
        ])
      ]),
      props.quickPick
        ? h('button', {
          class: 'adm-link',
          style: 'margin-left:8px;font-size:12px;',
          onClick: () => props.quickPick(d.id)
        }, '只选此部门')
        : null
    ])))
  }
}

async function loadDepts() {
  try {
    const r = await api.get('/system/dept/tree')
    deptFlat.value = flattenTree(r.tree || [])
  } catch (e) { deptFlat.value = [] }
}

function pickAllDepts(on) {
  // 停用的部门不勾（勾了也没有意义，且会让"可查看部门"与实际不符）
  edit.form.dept_ids = on ? deptFlat.value.filter((d) => d.status === 0).map((d) => d.id) : []
}
function pickDeptSubtree(id) {
  // 快捷：只保留某个部门本身（"自定义部门"语义不含子树）
  edit.form.dept_ids = [id]
}

// ---------------------------------------------------------------- 新增 / 编辑
const edit = reactive({
  show: false, isCreate: true, id: 0, builtin: false,
  form: { role_name: '', role_key: '', role_sort: 0, data_scope: 5, status: 0, remark: '', dept_ids: [] }
})

function openCreate() {
  Object.assign(edit, {
    show: true, isCreate: true, id: 0, builtin: false,
    form: { role_name: '', role_key: '', role_sort: 10, data_scope: 5, status: 0, remark: '', dept_ids: [] }
  })
  advOpen.value = false
  if (!deptFlat.value.length) loadDepts()
}

async function openEdit(r) {
  if (!deptFlat.value.length) await loadDepts()
  let deptIds = []
  try {
    const d = await api.get(`/system/role/detail/${r.id}`)
    deptIds = d.dept_ids || []
  } catch (e) { /* 忽略：按空处理，保存时会再次校验 */ }
  Object.assign(edit, {
    show: true, isCreate: false, id: r.id, builtin: !!r.builtin,
    form: {
      role_name: r.role_name, role_key: r.role_key, role_sort: r.role_sort,
      data_scope: r.data_scope, status: r.status, remark: r.remark || '', dept_ids: [...deptIds]
    }
  })
  advOpen.value = false
}

// 名称 → 内部标识的自动生成（只在用户没填时使用）
function autoKey(name) {
  const ascii = String(name || '').toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '')
  if (ascii) return ascii.slice(0, 32)
  return 'role_' + Date.now().toString(36)
}

async function submitEdit() {
  const f = edit.form
  if (!f.role_name) { alert('请填写角色名称'); return }
  if (f.data_scope === 2 && !f.dept_ids.length) {
    alert('可查看的部门选了「自定义部门」时，必须至少勾选 1 个部门，否则该角色下的人会什么都看不到')
    return
  }
  const key = f.role_key || autoKey(f.role_name)
  saving.value = true
  try {
    const body = {
      role_name: f.role_name, role_key: key, role_sort: f.role_sort,
      data_scope: f.data_scope, status: f.status, remark: f.remark, dept_ids: f.dept_ids
    }
    if (edit.isCreate) await api.post('/system/role', body)
    else await api.put('/system/role', { id: edit.id, ...body })
    edit.show = false
    await load()
  } catch (e) {
    alert(errMsg(e, '保存失败'))
  } finally { saving.value = false }
}

async function toggleStatus(r) {
  opsOpen.value = 0
  const next = r.status === 0 ? 1 : 0
  if (next === 1 && !confirm(`确定停用「${r.role_name}」吗？\n停用后该角色的权限立即失效，其下 ${r.user_count} 个用户会退回「仅本人」数据范围。`)) return
  try {
    await api.post('/system/role/changeStatus', { id: r.id, status: next })
    await load()
  } catch (e) { alert(errMsg(e, '修改状态失败')) }
}

async function doDelete(r) {
  opsOpen.value = 0
  if (!confirm(`确定删除角色「${r.role_name}」吗？不可撤销。`)) return
  try {
    await api.post(`/system/role/delete/${r.id}`)
    await load()
  } catch (e) { alert(errMsg(e, '删除失败')) }
}

// ---------------------------------------------------------------- 权限树
const perm = reactive({
  show: false, roleId: 0, roleName: '', superLock: false,
  checked: new Set(), kw: '', dirty: false
})
const permNodes = ref([])           // 嵌套树：目录(M) / 菜单(C) / 按钮(F) 同构渲染
const permExpanded = ref(new Set()) // 已展开的目录 / 菜单节点 id

/** 把扁平菜单列表还原成嵌套树（flattenTree 已补 parent_id） */
function buildNodes(flat) {
  const byId = new Map()
  const roots = []
  for (const n of flat) {
    byId.set(n.id, {
      id: n.id, name: n.name, type: n.type, perms: n.perms,
      kind: kindOf(n.perms), parentId: n.parent_id ?? 0, children: []
    })
  }
  for (const n of byId.values()) {
    const p = byId.get(n.parentId)
    if (p) p.children.push(n)
    else roots.push(n)
  }
  return roots
}

function subtreeHasChecked(n) {
  if (perm.checked.has(n.id)) return true
  return n.children.some(subtreeHasChecked)
}
/** 有已勾选子孙的节点默认展开：打开弹窗就能看到现有权限都配在哪 */
function expandWithChecked(nodes, s) {
  for (const n of nodes) {
    if (n.children.length && subtreeHasChecked(n)) s.add(n.id)
    expandWithChecked(n.children, s)
  }
}

function subtreeHit(n, kw) {
  if (String(n.name || '').toLowerCase().includes(kw)) return true
  if (String(n.perms || '').toLowerCase().includes(kw)) return true
  return n.children.some((c) => subtreeHit(c, kw))
}

/** 渲染列表：无搜索时按展开状态裁剪；有搜索时命中路径强制全展开 */
const visibleTree = computed(() => {
  const kw = perm.kw.trim().toLowerCase()
  const rows = []
  const walk = (nodes, depth, chainOpen) => {
    for (const n of nodes) {
      if (kw) {
        if (!subtreeHit(n, kw)) continue
        rows.push({ ...n, depth })
        walk(n.children, depth + 1, true)
      } else {
        rows.push({ ...n, depth })
        if (chainOpen && n.children.length) walk(n.children, depth + 1, permExpanded.value.has(n.id))
      }
    }
  }
  walk(permNodes.value, 0, true)
  return rows
})

function toggleExpand(id) {
  const s = new Set(permExpanded.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  permExpanded.value = s
}
function expandAll(on) {
  const s = new Set()
  if (on) {
    const walk = (nodes) => {
      for (const n of nodes) {
        if (n.children.length) { s.add(n.id); walk(n.children) }
      }
    }
    walk(permNodes.value)
  }
  permExpanded.value = s
}

function descIds(n) {
  const out = []
  const walk = (x) => {
    for (const c of x.children) { out.push(c.id); walk(c) }
  }
  walk(n)
  return out
}
function anyDescChecked(n) {
  return perm.checked.has(n.id) || n.children.some(anyDescChecked)
}
/** 半选态：自身未勾，但有子孙被勾（复选框显示横线） */
function isHalf(n) {
  return !perm.checked.has(n.id) && anyDescChecked(n)
}

/**
 * 树上单个节点的勾选：
 *  - 勾：自动补齐祖先目录 + 级联勾选全部子孙（勾目录 = 整块开通）
 *  - 取消：连同全部子孙一起摘掉（父级都关了，子项没有意义）
 */
function toggleNode(n, on) {
  if (perm.superLock) return
  const s = new Set(perm.checked)
  if (on) {
    withAncestors(s, n.id)
    s.add(n.id)
    for (const d of descIds(n)) s.add(d)
  } else {
    s.delete(n.id)
    for (const d of descIds(n)) s.delete(d)
  }
  perm.checked = s
  perm.dirty = true
}

const permIds = computed(() => {
  const ids = []
  const walk = (nodes) => {
    for (const n of nodes) {
      // 只统计"权限节点"（菜单 C + 按钮 F），目录 M 不计入，避免 N/N+3 的怪数字
      if (n.type !== 'M') ids.push(n.id)
      walk(n.children)
    }
  }
  walk(permNodes.value)
  return [...new Set(ids)]
})
// 已选计数只统计"权限节点"（页面 + 动作），模块目录节点不计入，避免出现 N/N+3 的怪数字
const checkedCount = computed(() => permIds.value.filter((id) => perm.checked.has(id)).length)

async function openPerm(r) {
  try {
    const d = await api.get(`/system/role/menuTree/${r.id}`)
    const flat = flattenTree(d.tree || [])
    treeFlat.value = flat
    ancMap.value = ancestorsMap(flat)
    permNodes.value = buildNodes(flat)
    const checkedSet = new Set(d.checked_ids || [])
    Object.assign(perm, {
      show: true, roleId: r.id, roleName: r.role_name,
      superLock: !!d.super_lock, checked: checkedSet,
      kw: '', dirty: false
    })
    // 已授权的分支默认展开，打开就能看到这个角色的权限都配在哪
    const es = new Set()
    expandWithChecked(permNodes.value, es)
    permExpanded.value = es
    permTotal.value = permIds.value.length
  } catch (e) { alert(errMsg(e, '加载权限树失败')) }
}

// 祖先链：勾了动作要把"所在页面 + 目录"一起补上，否则会出现
// 「勾了按钮但页面入口不出现」的悬挂授权。
const treeFlat = ref([])
const ancMap = ref(new Map())
function withAncestors(s, id) {
  s.add(id)
  for (const a of ancMap.value.get(id) || []) s.add(a)
  return s
}

function checkAll(on) {
  if (perm.superLock) return
  if (!on) { perm.checked = new Set(); perm.dirty = true; return }
  const s = new Set()
  for (const id of permIds.value) withAncestors(s, id)
  perm.checked = s
  perm.dirty = true
}

function checkViewOnly() {
  if (perm.superLock) return
  const s = new Set()
  const walk = (nodes) => {
    for (const n of nodes) {
      // 菜单入口(C)与查看类按钮(F)保留，其余动作全部摘掉
      if (n.type === 'C' || (n.type === 'F' && kindOf(n.perms) === 'view')) {
        withAncestors(s, n.id)
        s.add(n.id)
      }
      walk(n.children)
    }
  }
  walk(permNodes.value)
  perm.checked = s
  perm.dirty = true
}

async function submitPerm() {
  saving.value = true
  try {
    await api.put('/system/role/menu', { role_id: perm.roleId, menu_ids: [...perm.checked] })
    perm.show = false
    await load()
  } catch (e) {
    alert(errMsg(e, '保存权限失败'))
  } finally { saving.value = false }
}

function errMsg(e, dft) {
  return (e && e.response && e.response.data && e.response.data.error) || dft
}

onMounted(async () => {
  document.addEventListener('click', () => { opsOpen.value = 0 })
  await Promise.all([load(), loadDepts()])
})
</script>

<style scoped>
/* ---- 折叠块 ---- */
.adm-fold { border: 1px solid var(--glass-border); border-radius: 10px; margin: 10px 0; overflow: hidden; }
.adm-fold.compact { margin: 8px 0; }
.adm-fold-head {
  width: 100%; text-align: left; background: transparent; border: 0; cursor: pointer;
  padding: 9px 12px; font-size: 13px; color: var(--text-dim);
}
.adm-fold-head:hover { background: var(--bg-2, rgba(127,127,127,0.06)); }
.adm-fold-body { padding: 4px 14px 12px; font-size: 13px; color: var(--text-dim); line-height: 1.85; }
.adm-fold-body p { margin: 6px 0; }
.tri { display: inline-block; width: 14px; color: var(--text-faint); }

/* ---- 部门快捷操作 ---- */
.dept-pick-tools { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; }

/* ---- 列表页：角色名主副行 / 数据范围 / 权限开通进度 ---- */
.role-table th:first-child { min-width: 190px; }
.role-table th:nth-child(4) { min-width: 168px; }
.rl-name { display: flex; flex-direction: column; gap: 2px; }
.rl-title { font-size: 13px; color: var(--text); }
.rl-builtin { margin-left: 6px; }
.rl-remark { font-size: 11.5px; color: var(--text-faint); }
.rl-scope { font-size: 12.5px; color: var(--text-dim); }
.rl-sub { margin-left: 7px; font-size: 11.5px; color: var(--text-faint); }
.rl-bar-wrap { display: inline-flex; align-items: center; gap: 8px; }
.rl-bar {
  display: inline-flex; width: 72px; height: 5px; flex: none;
  border-radius: 3px; background: var(--overlay-2, var(--overlay)); overflow: hidden;
}
.rl-bar i { display: block; height: 100%; background: var(--accent, #6366f1); }
.rl-bar-n { font-size: 11.5px; color: var(--text-dim); font-variant-numeric: tabular-nums; }
.rl-st { display: inline-flex; align-items: center; gap: 6px; font-size: 12.5px; color: var(--text-dim); white-space: nowrap; }
.rl-st i { width: 7px; height: 7px; flex: none; border-radius: 50%; background: var(--success, #16a34a); }
.rl-st.off { color: var(--text-faint); }
.rl-st.off i { background: var(--text-faint); }

/* ---- 权限配置弹窗 ---- */
.perm-modal { max-width: 980px; }
.pm-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; flex-wrap: wrap; }
.pm-head-l h3 { margin: 0 0 4px; font-size: 15px; color: var(--text); }
.pm-sub { margin: 0; font-size: 12px; color: var(--text-faint); }
.pm-head-r { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.pm-count { margin-left: 4px; font-size: 12.5px; color: var(--text-dim); font-variant-numeric: tabular-nums; }
.pm-count b { color: var(--accent, #6366f1); font-weight: 600; }

.kind-legend { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 6px 16px; }
.kl-row { display: flex; align-items: baseline; gap: 8px; }
.kl-desc { font-size: 12px; color: var(--text-dim); }
.kind-badge {
  display: inline-block; padding: 1px 7px; border-radius: 999px; font-size: 11px; line-height: 1.7;
  border: 1px solid currentColor; white-space: nowrap;
}
.k-view { color: #2563eb; }
.k-add { color: #16a34a; }
.k-edit { color: #d97706; }
.k-remove { color: #dc2626; }
.k-import { color: #7c3aed; }
.k-export { color: #0891b2; }
.k-exec { color: #db2777; }
.k-config { color: #4b5563; }
.k-other { color: #6b7280; }

/* ---- 权限树 ---- */
.pm-tools { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-top: 10px; }
.pm-search { width: 210px; font-size: 12.5px; }
.pm-tools-r { margin-left: auto; display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.ptree {
  height: 54vh; min-height: 320px; overflow: auto; margin-top: 8px;
  border: 1px solid var(--glass-border); border-radius: 10px;
  padding: 8px 6px; background: var(--overlay);
}
.pt-row { display: flex; align-items: center; border-radius: 7px; padding: 1px 6px 1px 2px; }
.pt-row:hover { background: var(--overlay-2, rgba(127,127,127,0.08)); }
.pt-tri {
  width: 20px; height: 20px; flex: none; border: 0; background: transparent; cursor: pointer;
  color: var(--text-faint); font-size: 11px; line-height: 20px; padding: 0; text-align: center;
}
.pt-tri:hover { color: var(--accent); }
.pt-tri-ph { width: 20px; flex: none; }
.pt-lab { display: inline-flex; align-items: center; gap: 7px; cursor: pointer; padding: 2px 0; min-width: 0; }
.pt-name { font-size: 12.8px; color: var(--text); white-space: nowrap; }
.pt-m .pt-name { font-weight: 600; }
.pt-f .pt-name { font-size: 12.3px; color: var(--text-dim); }
.pt-perms {
  font-size: 10.5px; color: var(--text-faint);
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
}
.pt-row .kind-badge { flex: none; }
.pm-none { margin: 10px 6px; font-size: 12px; color: var(--text-faint); }
</style>
