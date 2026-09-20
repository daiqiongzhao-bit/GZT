<template>
  <div class="adm-page">
    <div class="adm-head">
      <h2 class="section-title">
        用户管理
        <span class="section-sub">列表按当前登录人的数据范围自动过滤（部门管理员仅见本部门及以下）</span>
      </h2>
      <div class="adm-actions">
        <button class="btn" :disabled="loading" @click="load">刷新</button>
        <button v-if="auth.can('system:user:add')" class="btn primary" @click="openCreate">新增用户</button>
      </div>
    </div>

    <section class="panel">
      <div class="adm-toolbar">
        <div class="fld-wrap">
          <label class="fld">关键字</label>
          <input v-model.trim="q.keyword" class="glass-input wide" placeholder="登录名 / 姓名 / 工号 / 手机号" @keyup.enter="search" />
        </div>
        <div class="fld-wrap">
          <label class="fld">部门</label>
          <select v-model="q.dept_id" class="glass-input">
            <option value="">全部（受数据范围约束）</option>
            <option v-for="o in deptOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
        </div>
        <div class="fld-wrap">
          <label class="fld">状态</label>
          <select v-model="q.frozen" class="glass-input narrow">
            <option value="">全部</option>
            <option value="0">正常</option>
            <option value="1">已禁用</option>
          </select>
        </div>
        <button class="btn primary" @click="search">查询</button>
        <button class="btn ghost" @click="resetQuery">重置</button>
      </div>

      <p class="adm-hint">
        当前数据范围：<b>{{ scopeLabel }}</b>
        <template v-if="auth.permScope && auth.permScope.fallback_self">（未匹配到任何档位，已收紧为「仅本人」）</template>
        —— 这是服务端强制注入的过滤条件，前端传参无法突破。
      </p>

      <div class="adm-tablewrap">
        <table class="adm-table">
          <thead>
            <tr>
              <th>登录名</th>
              <th>姓名</th>
              <th>工号</th>
              <th>手机号</th>
              <th>部门</th>
              <th>角色</th>
              <th>状态</th>
              <th>最近登录</th>
              <th class="num">令牌版本</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="u in rows" :key="u.id">
              <td class="mono">{{ u.username }}</td>
              <td>{{ u.name }}</td>
              <td class="dim">{{ u.emp_no || '—' }}</td>
              <td class="dim">{{ u.mobile || '—' }}</td>
              <td>{{ u.dept_name || ('#' + u.dept_id) }}</td>
              <td>
                <span v-for="(rn, i) in u.role_names || []" :key="i" class="chip" style="margin-right:4px">{{ rn }}</span>
                <span v-if="!(u.role_names || []).length" class="chip">{{ roleLabel(u.role) }}</span>
              </td>
              <td class="nowrap">
                <span v-if="u.frozen" class="chip danger">已禁用</span>
                <span v-else class="chip ok">正常</span>
                <span v-if="u.on_leave" class="chip warn">休假</span>
                <span v-if="u.must_change_pwd" class="chip">待改密</span>
              </td>
              <td class="dim nowrap">{{ u.last_login_at || '—' }}</td>
              <td class="num dim">{{ u.token_version }}</td>
              <td>
                <div class="adm-row-actions">
                  <button v-if="auth.can('system:user:edit')" class="adm-link" @click="openEdit(u)">编辑</button>
                  <button v-if="auth.can('system:user:authRole')" class="adm-link" @click="openRole(u)">分配角色</button>
                  <button v-if="auth.can('system:user:resetPwd')" class="adm-link" @click="doResetPwd(u)">重置密码</button>
                  <button
                    v-if="auth.can('system:user:edit')"
                    class="adm-link"
                    :disabled="u.id === auth.user?.id"
                    :title="u.id === auth.user?.id ? '不能禁用当前登录账号' : ''"
                    @click="toggleFrozen(u)"
                  >{{ u.frozen ? '启用' : '禁用' }}</button>
                  <button
                    v-if="auth.can('system:user:remove')"
                    class="adm-link danger"
                    :disabled="u.id === auth.user?.id"
                    @click="doDelete(u)"
                  >删除</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!rows.length" class="empty">{{ loading ? '加载中…' : '没有符合条件的用户（也可能全部落在你的数据范围之外）' }}</div>

      <div class="adm-pager">
        <span>共 {{ total }} 条</span>
        <button class="btn ghost" :disabled="page <= 1" @click="go(page - 1)">上一页</button>
        <span>{{ page }} / {{ maxPage }}</span>
        <button class="btn ghost" :disabled="page >= maxPage" @click="go(page + 1)">下一页</button>
      </div>
    </section>

    <!-- ================= 新增 / 编辑 ================= -->
    <div v-if="edit.show" class="adm-mask" @click.self="edit.show = false">
      <div class="adm-modal">
        <h3>{{ edit.isCreate ? '新增用户' : '编辑用户' }}</h3>
        <p class="adm-modal-sub">角色决定<b>能做什么</b>（功能权限），部门决定<b>能看谁</b>（数据范围）</p>

        <div class="adm-grid2">
          <div class="adm-field">
            <label class="fld">登录名 *</label>
            <input v-model.trim="edit.form.username" class="glass-input" :disabled="!edit.isCreate" placeholder="登录用账号" />
          </div>
          <div class="adm-field">
            <label class="fld">姓名</label>
            <input v-model.trim="edit.form.name" class="glass-input" placeholder="留空则用登录名" />
          </div>
          <div class="adm-field">
            <label class="fld">工号</label>
            <input v-model.trim="edit.form.emp_no" class="glass-input" />
          </div>
          <div class="adm-field">
            <label class="fld">手机号</label>
            <input v-model.trim="edit.form.mobile" class="glass-input" placeholder="企业微信 @ 提醒用" />
          </div>
          <div class="adm-field">
            <label class="fld">归属部门 *</label>
            <select v-model="edit.form.dept_id" class="glass-input">
              <option :value="0">请选择部门</option>
              <option v-for="o in deptOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </div>
          <div v-if="edit.isCreate" class="adm-field">
            <label class="fld">初始密码 *（至少 6 位）</label>
            <input v-model="edit.form.password" class="glass-input" placeholder="首次登录后强制改密" />
          </div>
        </div>

        <div class="adm-field">
          <label class="fld">角色 *（可多选，数据范围取并集；功能权限取并集）</label>
          <div class="adm-tree" style="max-height:200px">
            <div v-for="r in roles" :key="r.id" class="adm-tree-row">
              <label>
                <input class="adm-check" type="checkbox" :value="r.id" v-model="edit.form.role_ids" />
                <span class="nm">{{ r.role_name }} <span class="chip" style="margin-left:4px">{{ r.role_key }}</span></span>
              </label>
            </div>
            <div v-if="!roles.length" class="empty">加载角色中…</div>
          </div>
        </div>

        <label class="adm-inline">
          <input class="adm-check" type="checkbox" v-model="edit.form.on_leave" />
          休假 / 停职（不计入「全员」当班与推送）
        </label>

        <div class="adm-modal-foot">
          <button class="btn ghost" @click="edit.show = false">取消</button>
          <button class="btn primary" :disabled="saving" @click="submitEdit">{{ saving ? '保存中…' : '保存' }}</button>
        </div>
      </div>
    </div>

    <!-- ================= 分配角色 ================= -->
    <div v-if="roleDlg.show" class="adm-mask" @click.self="roleDlg.show = false">
      <div class="adm-modal">
        <h3>分配角色</h3>
        <p class="adm-modal-sub">{{ roleDlg.username }} —— 改变角色会立即令该用户的旧令牌失效（强制重新登录）</p>
        <div class="adm-alert warn">
          只能分配<b>自己也有权分配</b>的角色（不能分配权限高于自己的角色）；
          系统必须保留至少 1 个启用状态的超级管理员，因此不能摘掉最后一个超管。
        </div>
        <div class="adm-tree">
          <div v-for="r in roles" :key="r.id" class="adm-tree-row">
            <label>
              <input class="adm-check" type="checkbox" :value="r.id" v-model="roleDlg.role_ids" />
              <span class="nm">
                {{ r.role_name }} <span class="chip" style="margin-left:4px">{{ r.role_key }}</span>
                <span class="chip" style="margin-left:4px">{{ scopeName(r.data_scope) }}</span>
              </span>
            </label>
          </div>
        </div>
        <div class="adm-modal-foot">
          <button class="btn ghost" @click="roleDlg.show = false">取消</button>
          <button class="btn primary" :disabled="saving" @click="submitRole">保存</button>
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
const rows = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const maxPage = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))

const q = reactive({ keyword: '', dept_id: '', frozen: '' })
const deptOptions = ref([])
const roles = ref([])

const SCOPE_NAMES = {
  1: '全部数据', 2: '自定义部门', 3: '本部门', 4: '本部门及以下', 5: '仅本人'
}
const scopeName = (v) => SCOPE_NAMES[v] || ('档位 ' + v)
const scopeLabel = computed(() => {
  const s = auth.permScope
  if (!s) return '（加载中…）'
  if (s.all) return '全部数据（不受部门限制）'
  const ids = (s.dept_ids || []).join(', ')
  return `${ids ? '部门 ' + ids : '仅本人'}${s.include_self ? ' + 本人' : ''}`
})
const roleLabel = (r) => ({ super_admin: '超级管理员', dept_admin: '部门管理员', executor: '执行者' }[r] || r || '—')

// ---------------------------------------------------------------- 列表
async function load() {
  loading.value = true
  try {
    const params = { page: page.value, page_size: pageSize }
    if (q.keyword) params.keyword = q.keyword
    if (q.dept_id !== '') params.dept_id = q.dept_id
    if (q.frozen !== '') params.frozen = q.frozen
    const r = await api.get('/system/user/list', params)
    rows.value = r.list || []
    total.value = r.total || 0
  } catch (e) {
    rows.value = []
    total.value = 0
    alert(errMsg(e, '加载用户列表失败'))
  } finally {
    loading.value = false
  }
}
function search() { page.value = 1; load() }
function resetQuery() { q.keyword = ''; q.dept_id = ''; q.frozen = ''; page.value = 1; load() }
function go(p) { page.value = p; load() }

async function loadDepts() {
  try {
    const r = await api.get('/system/dept/treeSelect')
    deptOptions.value = treeOptions(flattenTree(r.tree || []))
  } catch (e) { deptOptions.value = [] }
}
// 角色选择器用 /system/role/options：它只返回「当前登录人有权分配的角色」。
// 不能改用 /system/role/list —— 那是角色管理页的接口（system:role:list），
// 部门管理员没有该权限，否则这里会 403、选择器永远停在「加载角色中…」。
async function loadRoles() {
  try {
    const r = await api.get('/system/role/options')
    roles.value = r.list || []
  } catch (e) { roles.value = [] }
}

// ---------------------------------------------------------------- 新增 / 编辑
const edit = reactive({
  show: false, isCreate: true, id: 0,
  form: { username: '', password: '', name: '', emp_no: '', mobile: '', dept_id: 0, role_ids: [], on_leave: false }
})

function openCreate() {
  Object.assign(edit, {
    show: true, isCreate: true, id: 0,
    form: { username: '', password: '', name: '', emp_no: '', mobile: '', dept_id: auth.user?.dept_id || 0, role_ids: [], on_leave: false }
  })
  if (!roles.value.length) loadRoles()
}

async function openEdit(u) {
  if (!roles.value.length) await loadRoles()
  let roleIds = u.role_ids || []
  // 列表接口已带 role_ids；若为空再取详情，确保编辑时勾选准确
  if (!roleIds.length) {
    try {
      const d = await api.get(`/system/user/detail/${u.id}`)
      roleIds = d.role_ids || []
    } catch (e) { /* 忽略，按空处理 */ }
  }
  Object.assign(edit, {
    show: true, isCreate: false, id: u.id,
    form: {
      username: u.username, password: '', name: u.name, emp_no: u.emp_no || '',
      mobile: u.mobile || '', dept_id: u.dept_id, role_ids: [...roleIds], on_leave: !!u.on_leave
    }
  })
}

async function submitEdit() {
  const f = edit.form
  if (!f.username) { alert('请填写登录名'); return }
  if (!f.dept_id) { alert('请选择归属部门'); return }
  if (!f.role_ids.length) { alert('请至少为用户分配 1 个角色（系统不再隐式补 executor）'); return }
  if (edit.isCreate && (!f.password || f.password.length < 6)) { alert('初始密码至少 6 位'); return }
  saving.value = true
  try {
    if (edit.isCreate) {
      await api.post('/system/user', {
        username: f.username, password: f.password, name: f.name, emp_no: f.emp_no,
        mobile: f.mobile, dept_id: f.dept_id, role_ids: f.role_ids, on_leave: f.on_leave
      })
    } else {
      await api.put('/system/user', {
        id: edit.id, name: f.name, emp_no: f.emp_no, mobile: f.mobile,
        dept_id: f.dept_id, role_ids: f.role_ids, on_leave: f.on_leave
      })
    }
    edit.show = false
    await load()
  } catch (e) {
    alert(errMsg(e, '保存失败'))
  } finally { saving.value = false }
}

// ---------------------------------------------------------------- 分配角色
const roleDlg = reactive({ show: false, id: 0, username: '', role_ids: [] })
async function openRole(u) {
  if (!roles.value.length) await loadRoles()
  let ids = u.role_ids || []
  if (!ids.length) {
    try {
      const d = await api.get(`/system/user/detail/${u.id}`)
      ids = d.role_ids || []
    } catch (e) { /* ignore */ }
  }
  Object.assign(roleDlg, { show: true, id: u.id, username: u.username, role_ids: [...ids] })
}
async function submitRole() {
  if (!roleDlg.role_ids.length) { alert('请至少为用户分配 1 个角色'); return }
  saving.value = true
  try {
    await api.post('/system/user/authRole', { id: roleDlg.id, role_ids: roleDlg.role_ids })
    roleDlg.show = false
    await load()
  } catch (e) {
    alert(errMsg(e, '分配角色失败'))
  } finally { saving.value = false }
}

// ---------------------------------------------------------------- 其它动作
async function doResetPwd(u) {
  if (!confirm(`确定重置「${u.name || u.username}」的密码吗？\n重置后该用户旧令牌立即失效，且下次登录必须改密。`)) return
  try {
    const r = await api.post('/system/user/resetPwd', { id: u.id })
    alert(`已重置。临时密码：${r.password}\n（请通过安全渠道告知本人；首次登录强制改密）`)
    await load()
  } catch (e) { alert(errMsg(e, '重置密码失败')) }
}

async function toggleFrozen(u) {
  const next = !u.frozen
  if (next && !confirm(`确定禁用「${u.name || u.username}」吗？该账号将立即无法登录。`)) return
  try {
    await api.post('/system/user/changeStatus', { id: u.id, frozen: next })
    await load()
  } catch (e) { alert(errMsg(e, '修改状态失败')) }
}

async function doDelete(u) {
  if (!confirm(`确定删除「${u.name || u.username}」吗？\n该操作会同时解除其全部角色绑定，且不可撤销。`)) return
  try {
    await api.post(`/system/user/delete/${u.id}`)
    await load()
  } catch (e) { alert(errMsg(e, '删除失败')) }
}

function errMsg(e, dft) {
  return (e && e.response && e.response.data && e.response.data.error) || dft
}

onMounted(async () => {
  await Promise.all([load(), loadDepts(), loadRoles()])
})
</script>
