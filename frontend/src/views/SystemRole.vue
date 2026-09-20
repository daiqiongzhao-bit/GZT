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
            <p class="pm-sub">勾上「查看」才能进这个模块，其余动作可以再单独放开</p>
          </div>
          <div class="pm-head-r">
            <button class="btn ghost sm" :disabled="perm.superLock" @click="checkAll(true)">全选</button>
            <button class="btn ghost sm" :disabled="perm.superLock" @click="checkAll(false)">全不选</button>
            <button class="btn ghost sm" :disabled="perm.superLock" @click="checkViewOnly()">仅保留「查看」</button>
            <span class="pm-count">已选 <b>{{ checkedCount }}</b> / {{ permIds.length }}</span>
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

        <!-- 左：模块目录（带已配 / 总数） 右：该模块的页面与动作 -->
        <div class="pm-body">
          <aside class="pm-side">
            <input v-model.trim="perm.kw" class="glass-input pm-search" placeholder="搜索模块 / 权限" />
            <div class="pm-sum">
              <span>全部模块</span>
              <span class="pm-sum-n">{{ checkedCount }} / {{ permIds.length }}</span>
            </div>
            <div class="pm-modlist">
              <button
                v-for="g in visibleGroups"
                :key="g.id"
                class="pm-mod"
                :class="{ on: activeGroup && activeGroup.id === g.id, zero: groupSelCount(g) === 0 }"
                @click="perm.activeGroupId = g.id"
              >
                <span class="pm-mod-n">{{ g.name }}</span>
                <span class="pm-mod-c">{{ groupSelCount(g) }}<template v-if="groupIds(g).length !== groupSelCount(g) || groupSelCount(g) === 0">/{{ groupIds(g).length }}</template></span>
              </button>
              <p v-if="!visibleGroups.length" class="pm-none">没有匹配「{{ perm.kw }}」的模块</p>
            </div>
            <p class="pm-side-tip">灰显 = 该模块一项未配</p>
          </aside>

          <div class="pm-main">
            <template v-if="activeGroup">
              <div class="pm-gh">
                <div class="pm-gh-l">
                  <button
                    class="pm-gh-all"
                    :disabled="perm.superLock"
                    @click="toggleGroup(activeGroup, !groupAllChecked(activeGroup))"
                  >{{ groupAllChecked(activeGroup) ? '取消全选' : '全选本模块' }}</button>
                  <span class="pm-gh-name">{{ activeGroup.name }}</span>
                </div>
                <span class="pm-bar-wrap">
                  <span class="rl-bar"><i :style="{ width: groupPct(activeGroup) + '%' }"></i></span>
                  <span class="rl-bar-n">已选 {{ groupSelCount(activeGroup) }} / {{ groupIds(activeGroup).length }}</span>
                </span>
              </div>

              <div class="pm-pages">
                <div v-for="p in activeGroup.pages" :key="p.id" class="pm-page">
                  <p class="pm-page-name">
                    {{ p.name }}
                    <span v-if="!perm.checked.has(p.id)" class="pm-page-off">未开通入口</span>
                  </p>
                  <div class="pm-acts">
                    <button
                      v-for="a in p.actions"
                      :key="a.id"
                      class="pm-act"
                      :class="{ on: perm.checked.has(a.id), del: a.kind === 'remove', lock: perm.superLock }"
                      :title="KIND_DESC[a.kind]"
                      :disabled="perm.superLock"
                      @click="toggleAction(p, a, !perm.checked.has(a.id))"
                    >{{ a.isPage ? '查看' : a.name }}</button>
                    <span v-if="!p.actions.length" class="dim">该页面无细分动作</span>
                  </div>
                </div>
              </div>
            </template>
            <div v-else class="empty">左侧没有可配置的模块</div>
          </div>
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
function groupPct(g) {
  const ids = groupIds(g)
  if (!ids.length) return 0
  return Math.round((groupSelCount(g) / ids.length) * 100)
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

// ---------------------------------------------------------------- 权限矩阵
const KIND_RANK = (k) => {
  const i = KIND_ORDER.indexOf(k)
  return i < 0 ? KIND_ORDER.length : i
}

/**
 * 把后端的嵌套菜单树压成「模块 → 页面 → 动作」三级矩阵。
 *
 * - 模块：menu_type=M 的目录；顶级 C（如「首页」）自身也算一个模块
 * - 页面：menu_type=C 的菜单节点，其 perms 就是"能不能进这个页面"
 * - 动作：menu_type=F 的按钮节点，挂到最近的 C 祖先下
 */
function buildMatrix(flat) {
  const byId = new Map(flat.map((x) => [x.id, x]))
  const groupMap = new Map()

  const rootGroupOf = (menuId) => {
    let cur = byId.get(menuId)
    let guard = 0
    while (cur && guard++ < 32) {
      if (cur.type === 'M') return cur.id
      const p = byId.get(cur.parent_id ?? 0)
      if (!p) return cur.id // 顶级页面：自己成为一个"模块"
      cur = p
    }
    return menuId
  }
  const ensureGroup = (id) => {
    if (!groupMap.has(id)) {
      const m = byId.get(id) || {}
      groupMap.set(id, { id, name: m.name || '其他', order: m.order_num ?? 0, pages: [], pageMap: new Map() })
    }
    return groupMap.get(id)
  }

  for (const n of flat) {
    if (n.type === 'M') { ensureGroup(n.id); continue }
    if (n.type !== 'C') continue
    const g = ensureGroup(rootGroupOf(n.id))
    if (!g.pageMap.has(n.id)) {
      const page = { id: n.id, name: n.name, perms: n.perms, actions: [] }
      g.pageMap.set(n.id, page)
      g.pages.push(page)
    }
  }

  for (const n of flat) {
    if (n.type !== 'F') continue
    let cur = byId.get(n.parent_id ?? 0)
    let guard = 0
    let page = null
    while (cur && guard++ < 32) {
      if (cur.type === 'C') {
        const g = groupMap.get(rootGroupOf(cur.id))
        page = g ? g.pageMap.get(cur.id) : null
        break
      }
      cur = byId.get(cur.parent_id ?? 0)
    }
    if (!page) continue
    page.actions.push({ id: n.id, name: n.name, perms: n.perms, kind: kindOf(n.perms) })
  }

  const out = []
  for (const g of groupMap.values()) {
    for (const p of g.pages) {
      if (p.perms) {
        // 页面自身的 perms 就是"查看"，排到最前面
        p.actions.unshift({ id: p.id, name: p.name, perms: p.perms, kind: kindOf(p.perms), isPage: true })
      }
      p.actions.sort((a, b) => KIND_RANK(a.kind) - KIND_RANK(b.kind))
    }
    out.push(g)
  }
  out.sort((a, b) => (a.order || 0) - (b.order || 0))
  return out
}

const perm = reactive({
  show: false, roleId: 0, roleName: '', superLock: false,
  checked: new Set(), kw: '', dirty: false,
  activeGroupId: 0
})
const groups = ref([])

const permIds = computed(() => {
  const ids = []
  for (const g of groups.value) for (const p of g.pages) { ids.push(p.id); for (const a of p.actions) ids.push(a.id) }
  return [...new Set(ids)]
})
// 已选计数只统计"权限节点"（页面 + 动作），模块目录节点不计入，避免出现 N/N+3 的怪数字
const checkedCount = computed(() => permIds.value.filter((id) => perm.checked.has(id)).length)

/** 左侧模块目录：搜索时按「模块名 / 页面名 / 动作名 / 权限标识」命中过滤 */
const visibleGroups = computed(() => {
  const kw = perm.kw.trim().toLowerCase()
  if (!kw) return groups.value
  const hit = (s) => String(s || '').toLowerCase().includes(kw)
  return groups.value.filter((g) =>
    hit(g.name) ||
    g.pages.some((p) => hit(p.name) || hit(p.perms) || p.actions.some((a) => hit(a.name) || hit(a.perms)))
  )
})

/** 右侧当前模块：选中项若被搜索过滤掉，自动回退到第一个 */
const activeGroup = computed(() => {
  const list = visibleGroups.value
  return list.find((g) => g.id === perm.activeGroupId) || list[0] || null
})

async function openPerm(r) {
  try {
    const d = await api.get(`/system/role/menuTree/${r.id}`)
    const flat = flattenTree(d.tree || [])
    treeFlat.value = flat
    ancMap.value = ancestorsMap(flat)
    groups.value = buildMatrix(flat)
    const checkedSet = new Set(d.checked_ids || [])
    Object.assign(perm, {
      show: true, roleId: r.id, roleName: r.role_name,
      superLock: !!d.super_lock, checked: checkedSet,
      kw: '', dirty: false
    })
    // 默认停在"配得最多"的模块；一个都没配时停在第一个，避免打开就是空白
    const idsAll = (g) => groupIds(g)
    const firstAuthed = groups.value.find((g) => idsAll(g).some((id) => checkedSet.has(id)))
    perm.activeGroupId = (firstAuthed || groups.value[0] || { id: 0 }).id
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

function groupIds(g) {
  const ids = []
  for (const p of g.pages) { ids.push(p.id); for (const a of p.actions) ids.push(a.id) }
  return [...new Set(ids)]
}
function groupSelCount(g) { return groupIds(g).filter((id) => perm.checked.has(id)).length }
function groupAllChecked(g) {
  const ids = groupIds(g)
  return ids.length > 0 && ids.every((id) => perm.checked.has(id))
}
function groupPartial(g) {
  const n = groupSelCount(g)
  return n > 0 && n < groupIds(g).length
}

function toggleGroup(g, on) {
  if (perm.superLock) return
  const s = new Set(perm.checked)
  for (const p of g.pages) {
    const ids = [p.id, ...p.actions.map((a) => a.id)]
    for (const id of ids) {
      if (on) withAncestors(s, id)
      else s.delete(id)
    }
  }
  // 整组取消时，把该模块的目录节点也摘掉（否则 role_menus 里会留下孤立的目录行）
  if (!on) s.delete(g.id)
  perm.checked = s
  perm.dirty = true
}

/**
 * 单个动作的勾选。
 * 约束（产品层，避免出现自相矛盾的授权）：
 *  - 勾任意动作 → 自动补上该页面的「查看」与祖先目录
 *  - 取消页面的「查看」→ 清掉该页面所有动作（页面都进不去了，动作没有意义）
 */
function toggleAction(p, a, on) {
  if (perm.superLock) return
  const s = new Set(perm.checked)
  if (on) {
    withAncestors(s, a.id)
    withAncestors(s, p.id)
  } else if (a.isPage) {
    s.delete(p.id)
    for (const x of p.actions) s.delete(x.id)
  } else {
    s.delete(a.id)
  }
  perm.checked = s
  perm.dirty = true
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
  for (const g of groups.value) for (const p of g.pages) {
    const view = p.actions.find((a) => a.isPage)
    if (view) withAncestors(s, view.id)
  }
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

/* 左：模块目录  右：动作面板 */
.pm-body {
  display: flex; height: 54vh; min-height: 320px; margin-top: 10px;
  border: 1px solid var(--glass-border); border-radius: 10px; overflow: hidden;
}
.pm-side {
  width: 172px; flex: none; display: flex; flex-direction: column;
  border-right: 1px solid var(--glass-border); background: var(--overlay);
}
.pm-search { margin: 10px 10px 6px; font-size: 12.5px; }
.pm-sum {
  display: flex; align-items: baseline; justify-content: space-between; gap: 8px;
  padding: 4px 12px 8px; font-size: 12px; color: var(--text-faint);
  border-bottom: 1px solid var(--glass-border);
}
.pm-sum-n { font-variant-numeric: tabular-nums; }
.pm-modlist { flex: 1; overflow: auto; padding: 6px; }
.pm-mod {
  width: 100%; display: flex; align-items: center; justify-content: space-between; gap: 8px;
  padding: 6px 8px; margin-bottom: 1px; border: 0; border-radius: 7px; cursor: pointer;
  background: transparent; color: var(--text-dim); font-size: 12.5px; text-align: left;
}
.pm-mod:hover { background: var(--overlay-2, rgba(127,127,127,0.08)); }
.pm-mod.on { background: var(--accent-soft); color: var(--accent); }
.pm-mod-n { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pm-mod-c { flex: none; font-size: 11px; color: var(--text-faint); font-variant-numeric: tabular-nums; }
.pm-mod.on .pm-mod-c { color: var(--accent); }
.pm-mod.zero { color: var(--text-faint); }
.pm-mod.zero.on { color: var(--accent); }
.pm-none { margin: 10px 6px; font-size: 12px; color: var(--text-faint); }
.pm-side-tip { margin: 0; padding: 8px 12px; border-top: 1px solid var(--glass-border); font-size: 11px; color: var(--text-faint); }

.pm-main { flex: 1; min-width: 0; overflow: auto; padding: 12px 16px; }
.pm-gh {
  display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap;
  padding-bottom: 10px; margin-bottom: 4px; border-bottom: 1px solid var(--glass-border);
}
.pm-gh-l { display: flex; align-items: center; gap: 10px; min-width: 0; }
.pm-gh-all {
  flex: none; padding: 3px 9px; border-radius: 7px; cursor: pointer; font-size: 12px;
  border: 1px solid var(--glass-border); background: transparent; color: var(--text-dim);
}
.pm-gh-all:hover:not(:disabled) { border-color: var(--accent); color: var(--accent); }
.pm-gh-all:disabled { cursor: not-allowed; color: var(--text-faint); }
.pm-gh-name { font-size: 13.5px; font-weight: 600; color: var(--text); }
.pm-pages { padding-top: 6px; }
.pm-page + .pm-page { margin-top: 14px; }
.pm-page-name { margin: 0 0 7px; font-size: 12.5px; color: var(--text-dim); }
.pm-page-off {
  margin-left: 7px; padding: 1px 6px; border-radius: 5px;
  background: var(--overlay-2, var(--overlay)); font-size: 11px; color: var(--text-faint);
}
.pm-acts { display: flex; flex-wrap: wrap; gap: 7px; }
.pm-act {
  padding: 4px 11px; border-radius: 7px; cursor: pointer; font-size: 12.5px;
  border: 1px solid var(--glass-border); background: transparent; color: var(--text-dim);
}
.pm-act:hover:not(:disabled) { border-color: var(--accent); color: var(--accent); }
.pm-act.on { background: var(--accent-soft); border-color: var(--accent); color: var(--accent); }
.pm-act.del { border-color: rgba(220, 38, 38, 0.4); color: #dc2626; }
.pm-act.del:hover:not(:disabled) { border-color: #dc2626; color: #dc2626; }
.pm-act.del.on { background: rgba(220, 38, 38, 0.12); border-color: #dc2626; color: #dc2626; }
.pm-act.lock { opacity: 0.72; cursor: not-allowed; }
</style>
