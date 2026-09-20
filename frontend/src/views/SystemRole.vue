<template>
  <div class="adm-page">
    <div class="adm-head">
      <h2 class="section-title">
        角色管理
        <span class="section-sub">角色是唯一的授权枢纽：同时承载「功能权限」与「数据权限」两条链</span>
      </h2>
      <div class="adm-actions">
        <button class="btn" :disabled="loading" @click="load">刷新</button>
        <button v-if="auth.can('system:role:add')" class="btn primary" @click="openCreate">新增角色</button>
      </div>
    </div>

    <section class="panel">
      <div class="adm-alert info">
        功能权限（勾选菜单/按钮）与数据权限（5 档数据范围）是<b>相互独立</b>的两个维度：
        只给菜单不给数据范围 = 进得去页面但看不到数据；只给数据范围不给菜单 = 看得到数据但进不去功能。
        两者相交才是最终可见内容。
      </div>

      <div class="adm-tablewrap">
        <table class="adm-table">
          <thead>
            <tr>
              <th>角色名称</th>
              <th>role_key</th>
              <th class="num">排序</th>
              <th>数据范围</th>
              <th>状态</th>
              <th class="num">用户数</th>
              <th class="num">菜单权限</th>
              <th>备注</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in rows" :key="r.id">
              <td>
                {{ r.role_name }}
                <span v-if="r.builtin" class="chip accent" style="margin-left:6px">内置</span>
              </td>
              <td class="mono">{{ r.role_key }}</td>
              <td class="num dim">{{ r.role_sort }}</td>
              <td>
                <span class="chip">{{ scopeName(r.data_scope) }}</span>
                <span v-if="r.data_scope === 2" class="chip">自定义</span>
              </td>
              <td>
                <span v-if="r.status === 0" class="chip ok">正常</span>
                <span v-else class="chip danger">停用</span>
              </td>
              <td class="num">{{ r.user_count }}</td>
              <td class="num">{{ r.menu_count }}</td>
              <td class="dim">{{ r.remark || '—' }}</td>
              <td>
                <div class="adm-row-actions">
                  <button v-if="auth.can('system:role:edit')" class="adm-link" @click="openEdit(r)">编辑</button>
                  <button v-if="auth.can('system:role:menu')" class="adm-link" @click="openMenu(r)">分配菜单权限</button>
                  <button v-if="auth.can('system:role:dataScope')" class="adm-link" @click="openScope(r)">分配数据范围</button>
                  <button
                    v-if="auth.can('system:role:edit')"
                    class="adm-link"
                    :disabled="r.builtin"
                    :title="r.builtin ? '内置角色不可停用' : ''"
                    @click="toggleStatus(r)"
                  >{{ r.status === 0 ? '停用' : '启用' }}</button>
                  <button
                    v-if="auth.can('system:role:remove')"
                    class="adm-link danger"
                    :disabled="r.builtin || r.user_count > 0"
                    :title="r.builtin ? '内置角色不可删除' : (r.user_count > 0 ? '该角色下仍有用户，请先调整' : '')"
                    @click="doDelete(r)"
                  >删除</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!rows.length" class="empty">{{ loading ? '加载中…' : '暂无角色' }}</div>
    </section>

    <!-- ================= 新增 / 编辑 ================= -->
    <div v-if="edit.show" class="adm-mask" @click.self="edit.show = false">
      <div class="adm-modal">
        <h3>{{ edit.isCreate ? '新增角色' : '编辑角色' }}</h3>
        <p class="adm-modal-sub">role_key 是稳定的程序标识，用于后端接口权限校验；role_name 只是显示名</p>

        <div class="adm-grid2">
          <div class="adm-field">
            <label class="fld">角色名称 *</label>
            <input v-model.trim="edit.form.role_name" class="glass-input" placeholder="如：排班主管" />
          </div>
          <div class="adm-field">
            <label class="fld">role_key *（唯一，建议 小写+下划线）</label>
            <input
              v-model.trim="edit.form.role_key"
              class="glass-input"
              :disabled="edit.builtin"
              placeholder="如：schedule_lead"
            />
          </div>
          <div class="adm-field">
            <label class="fld">排序（越小越靠前）</label>
            <input v-model.number="edit.form.role_sort" type="number" class="glass-input" />
          </div>
          <div class="adm-field">
            <label class="fld">状态</label>
            <select v-model.number="edit.form.status" class="glass-input" :disabled="edit.builtin">
              <option :value="0">正常</option>
              <option :value="1">停用（不参与权限解析）</option>
            </select>
          </div>
        </div>

        <div class="adm-field">
          <label class="fld">数据范围 *</label>
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
          <label class="fld">自定义部门 *（至少选 1 个，否则角色会成为「什么都看不到」的黑洞）</label>
          <DeptPicker v-model="edit.form.dept_ids" :flat="deptFlat" />
        </div>

        <div class="adm-field">
          <label class="fld">备注</label>
          <input v-model.trim="edit.form.remark" class="glass-input" />
        </div>

        <div class="adm-modal-foot">
          <button class="btn ghost" @click="edit.show = false">取消</button>
          <button class="btn primary" :disabled="saving" @click="submitEdit">{{ saving ? '保存中…' : '保存' }}</button>
        </div>
      </div>
    </div>

    <!-- ================= 分配菜单权限 ================= -->
    <div v-if="menuDlg.show" class="adm-mask" @click.self="menuDlg.show = false">
      <div class="adm-modal wide">
        <h3>分配菜单权限 —— {{ menuDlg.roleName }}</h3>
        <p class="adm-modal-sub">勾选 = 该角色可以访问该菜单 / 使用该按钮</p>

        <div v-if="menuDlg.superLock" class="adm-alert warn">
          超级管理员必须拥有全部权限，因此<b>不允许取消勾选</b>（防止管理员把自己锁死）。
        </div>
        <div v-else class="adm-alert info">
          <b>按钮级权限必须与接口权限一一对应</b>：这里的 F 节点 perms 与后端 routePerms 逐字符一致，
          由「菜单管理 → 一致性自检」双向差集强制。勾选父节点会自动勾上全部子节点。
        </div>

        <div class="adm-tree-tools">
          <button class="btn ghost" :disabled="menuDlg.superLock" @click="checkAllMenus(true)">全选</button>
          <button class="btn ghost" :disabled="menuDlg.superLock" @click="checkAllMenus(false)">全不选</button>
          <button class="btn ghost" :disabled="menuDlg.superLock" @click="checkOnlyPerms()">仅保留权限节点</button>
          <span class="chip">已选 {{ menuDlg.checked.size }} / {{ menuFlat.length }}</span>
        </div>

        <div class="adm-tree" style="max-height:420px">
          <div v-for="m in menuFlat" :key="m.id" class="adm-tree-row">
            <span class="adm-indent" :style="{ width: (m.depth * 18) + 'px' }"></span>
            <span class="adm-tree-mark">{{ m.depth ? '└' : '▸' }}</span>
            <label>
              <input
                class="adm-check"
                type="checkbox"
                :disabled="menuDlg.superLock"
                :checked="menuDlg.checked.has(m.id)"
                @change="toggleMenu(m, $event.target.checked)"
              />
              <span class="nm">
                {{ m.name }}
                <span class="chip" :class="typeChip(m.type)" style="margin-left:5px">{{ typeLabel(m.type) }}</span>
                <span v-if="m.perms" class="chip" style="margin-left:5px">{{ m.perms }}</span>
                <span v-if="m.status !== 0" class="chip danger" style="margin-left:5px">停用</span>
              </span>
            </label>
          </div>
        </div>

        <div class="adm-modal-foot">
          <button class="btn ghost" @click="menuDlg.show = false">取消</button>
          <button class="btn primary" :disabled="saving || menuDlg.superLock" @click="submitMenu">
            {{ menuDlg.superLock ? '超管权限不可修改' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <!-- ================= 分配数据范围 ================= -->
    <div v-if="scopeDlg.show" class="adm-mask" @click.self="scopeDlg.show = false">
      <div class="adm-modal">
        <h3>分配数据范围 —— {{ scopeDlg.roleName }}</h3>
        <p class="adm-modal-sub">数据范围只用于「读查询」；写操作另有归属断言，不依赖范围并集</p>

        <div class="adm-field">
          <div class="adm-radio-row">
            <label v-for="s in dataScopes" :key="s.value" class="adm-radio" :class="{ on: scopeDlg.data_scope === s.value }">
              <input type="radio" :value="s.value" v-model.number="scopeDlg.data_scope" />
              <span>
                <span class="t">{{ s.label }}</span>
                <span class="d">{{ s.desc }}</span>
              </span>
            </label>
          </div>
        </div>

        <div v-if="scopeDlg.data_scope === 2" class="adm-field">
          <label class="fld">自定义部门 *（至少选 1 个）</label>
          <DeptPicker v-model="scopeDlg.dept_ids" :flat="deptFlat" />
        </div>
        <p v-else class="adm-hint">
          切到「自定义部门」以外的档位时，已保存的部门清单会<b>保留但被忽略</b>（切回来无需重配）。
        </p>

        <div class="adm-modal-foot">
          <button class="btn ghost" @click="scopeDlg.show = false">取消</button>
          <button class="btn primary" :disabled="saving" @click="submitScope">保存</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, h, onMounted } from 'vue'
import * as api from '@/api'
import { useAuthStore } from '@/store/auth'
import { flattenTree, descendantsMap, applyCascade } from '@/utils/rbacTree'

const auth = useAuthStore()

const loading = ref(false)
const saving = ref(false)
const rows = ref([])
const dataScopes = ref([])

const SCOPE_NAMES = { 1: '全部数据', 2: '自定义部门', 3: '本部门', 4: '本部门及以下', 5: '仅本人' }
const scopeName = (v) => SCOPE_NAMES[v] || ('档位 ' + v)
const TYPE_LABEL = { M: '目录', C: '菜单', F: '按钮' }
const typeLabel = (t) => TYPE_LABEL[t] || t
const typeChip = (t) => (t === 'F' ? '' : (t === 'C' ? 'accent' : 'warn'))

// ---------------------------------------------------------------- 列表
async function load() {
  loading.value = true
  try {
    const r = await api.get('/system/role/list')
    rows.value = r.list || []
    if (r.data_scopes) dataScopes.value = r.data_scopes
  } catch (e) {
    rows.value = []
    alert(errMsg(e, '加载角色失败'))
  } finally { loading.value = false }
}

// ---------------------------------------------------------------- 部门选择器（内联组件，避免额外文件）
const deptFlat = ref([])
const DeptPicker = {
  props: { modelValue: { type: Array, default: () => [] }, flat: { type: Array, default: () => [] } },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    const has = (id) => props.modelValue.includes(id)
    const toggle = (id, on) => {
      const s = new Set(props.modelValue)
      if (on) s.add(id)
      else s.delete(id)
      emit('update:modelValue', [...s])
    }
    return () => h('div', { class: 'adm-tree', style: 'max-height:260px' }, [
      h('div', { class: 'adm-tree-tools' }, [
        h('span', { class: 'chip' }, `已选 ${props.modelValue.length} 个部门`)
      ]),
      ...props.flat.map((d) => h('div', { class: 'adm-tree-row', key: d.id }, [
        h('span', { class: 'adm-indent', style: `width:${d.depth * 18}px` }),
        h('span', { class: 'adm-tree-mark' }, d.depth ? '└' : '▸'),
        h('label', {}, [
          h('input', {
            class: 'adm-check', type: 'checkbox', checked: has(d.id),
            onChange: (e) => toggle(d.id, e.target.checked)
          }),
          h('span', { class: 'nm' }, [
            d.name,
            d.status !== 0 ? h('span', { class: 'chip danger', style: 'margin-left:5px' }, '停用') : null,
            d.user_count ? h('span', { class: 'chip', style: 'margin-left:5px' }, `${d.user_count} 人`) : null
          ])
        ])
      ])),
      props.flat.length ? null : h('div', { class: 'empty' }, '加载部门中…')
    ])
  }
}

async function loadDepts() {
  try {
    const r = await api.get('/system/dept/tree')
    deptFlat.value = flattenTree(r.tree || [])
  } catch (e) { deptFlat.value = [] }
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
  if (!deptFlat.value.length) loadDepts()
}

async function openEdit(r) {
  if (!deptFlat.value.length) await loadDepts()
  let deptIds = []
  try {
    const d = await api.get(`/system/role/detail/${r.id}`)
    deptIds = d.dept_ids || []
  } catch (e) { /* 忽略 */ }
  Object.assign(edit, {
    show: true, isCreate: false, id: r.id, builtin: !!r.builtin,
    form: {
      role_name: r.role_name, role_key: r.role_key, role_sort: r.role_sort,
      data_scope: r.data_scope, status: r.status, remark: r.remark || '', dept_ids: [...deptIds]
    }
  })
}

async function submitEdit() {
  const f = edit.form
  if (!f.role_name) { alert('请填写角色名称'); return }
  if (!f.role_key) { alert('请填写 role_key'); return }
  if (f.data_scope === 2 && !f.dept_ids.length) {
    alert('数据范围为「自定义部门」时必须至少选择 1 个部门（否则该角色下的人什么都看不到）')
    return
  }
  saving.value = true
  try {
    if (edit.isCreate) {
      await api.post('/system/role', {
        role_name: f.role_name, role_key: f.role_key, role_sort: f.role_sort,
        data_scope: f.data_scope, status: f.status, remark: f.remark, dept_ids: f.dept_ids
      })
    } else {
      await api.put('/system/role', {
        id: edit.id, role_name: f.role_name, role_key: f.role_key, role_sort: f.role_sort,
        data_scope: f.data_scope, status: f.status, remark: f.remark, dept_ids: f.dept_ids
      })
    }
    edit.show = false
    await load()
  } catch (e) {
    alert(errMsg(e, '保存失败'))
  } finally { saving.value = false }
}

async function toggleStatus(r) {
  const next = r.status === 0 ? 1 : 0
  if (next === 1 && !confirm(`确定停用「${r.role_name}」吗？\n停用后该角色的权限立即失效，其下 ${r.user_count} 个用户会退回「仅本人」数据范围。`)) return
  try {
    await api.post('/system/role/changeStatus', { id: r.id, status: next })
    await load()
  } catch (e) { alert(errMsg(e, '修改状态失败')) }
}

async function doDelete(r) {
  if (!confirm(`确定删除角色「${r.role_name}」吗？不可撤销。`)) return
  try {
    await api.post(`/system/role/delete/${r.id}`)
    await load()
  } catch (e) { alert(errMsg(e, '删除失败')) }
}

// ---------------------------------------------------------------- 菜单权限
const menuDlg = reactive({ show: false, roleId: 0, roleName: '', superLock: false, checked: new Set() })
const menuFlat = ref([])
let menuDmap = new Map()

async function openMenu(r) {
  try {
    const d = await api.get(`/system/role/menuTree/${r.id}`)
    menuFlat.value = flattenTree(d.tree || [])
    menuDmap = descendantsMap(menuFlat.value)
    Object.assign(menuDlg, {
      show: true, roleId: r.id, roleName: r.role_name,
      superLock: !!d.super_lock, checked: new Set(d.checked_ids || [])
    })
  } catch (e) { alert(errMsg(e, '加载菜单树失败')) }
}

function toggleMenu(m, checked) {
  menuDlg.checked = applyCascade(menuDlg.checked, menuFlat.value, m.id, checked, menuDmap)
}
function checkAllMenus(on) {
  menuDlg.checked = on ? new Set(menuFlat.value.map((m) => m.id)) : new Set()
}
function checkOnlyPerms() {
  // 只保留有 perms 的节点（F 按钮 + 带 perms 的 C 菜单）+ 它们的所有祖先（否则树不可达）
  const s = new Set()
  for (const m of menuFlat.value) {
    if (!m.perms) continue
    s.add(m.id)
    let cur = m
    let guard = 0
    while (cur && guard++ < 64) {
      const p = menuFlat.value.find((x) => x.id === cur.parent_id)
      if (!p) break
      s.add(p.id)
      cur = p
    }
  }
  menuDlg.checked = s
}

async function submitMenu() {
  saving.value = true
  try {
    await api.put('/system/role/menu', { role_id: menuDlg.roleId, menu_ids: [...menuDlg.checked] })
    menuDlg.show = false
    await load()
  } catch (e) {
    alert(errMsg(e, '保存菜单权限失败'))
  } finally { saving.value = false }
}

// ---------------------------------------------------------------- 数据范围
const scopeDlg = reactive({ show: false, roleId: 0, roleName: '', data_scope: 5, dept_ids: [] })

async function openScope(r) {
  if (!deptFlat.value.length) await loadDepts()
  try {
    const d = await api.get(`/system/role/deptTree/${r.id}`)
    if (d.data_scopes) dataScopes.value = d.data_scopes
    Object.assign(scopeDlg, {
      show: true, roleId: r.id, roleName: r.role_name,
      data_scope: d.role?.data_scope ?? 5, dept_ids: [...(d.checked_ids || [])]
    })
  } catch (e) { alert(errMsg(e, '加载数据范围失败')) }
}

async function submitScope() {
  if (scopeDlg.data_scope === 2 && !scopeDlg.dept_ids.length) {
    alert('数据范围为「自定义部门」时必须至少选择 1 个部门')
    return
  }
  saving.value = true
  try {
    await api.put('/system/role/dataScope', {
      role_id: scopeDlg.roleId, data_scope: scopeDlg.data_scope, dept_ids: scopeDlg.dept_ids
    })
    scopeDlg.show = false
    await load()
  } catch (e) {
    alert(errMsg(e, '保存数据范围失败'))
  } finally { saving.value = false }
}

function errMsg(e, dft) {
  return (e && e.response && e.response.data && e.response.data.error) || dft
}

onMounted(async () => {
  await Promise.all([load(), loadDepts()])
})
</script>
