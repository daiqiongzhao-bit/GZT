<template>
  <div class="adm-page">
    <div class="adm-head">
      <h2 class="section-title">
        用户管理
        <span class="section-sub">列表按当前登录人的数据范围自动过滤（部门管理员仅见本部门及以下）</span>
      </h2>
      <div class="adm-actions">
        <!-- v0.39.1：原「设置 → 人员」的批量导入 / 导出 / 批量操作并入此处，各自独立门控 -->
        <button v-if="auth.can('system:user:import')" class="btn ghost" @click="downloadAuth('templates/user-template')">导入模板</button>
        <label v-if="auth.can('system:user:import')" class="btn ghost adm-imp-label">
          {{ importing ? '导入中…' : '批量导入' }}
          <input type="file" accept=".xlsx,.csv" :disabled="importing" @change="importUsers" hidden />
        </label>
        <button v-if="auth.can('system:user:export')" class="btn ghost" @click="downloadAuth('users/export')">导出</button>
        <button
          v-if="canBatch"
          class="btn ghost"
          :class="{ active: batchMode }"
          @click="toggleBatch"
        >{{ batchMode ? '退出批量' : '批量操作' }}</button>
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

      <!-- ================= 批量操作栏 ================= -->
      <div v-if="batchMode" class="adm-batch">
        <div class="adm-batch-row">
          <label class="adm-inline">
            <input class="adm-check" type="checkbox" :checked="allSelected" @change="toggleAll" /> 全选本页
          </label>
          <span class="adm-batch-count">已选 {{ selectedIds.length }} 人</span>
        </div>
        <div class="adm-batch-row">
          <template v-if="auth.can('system:user:edit')">
            <select v-model.number="batchDept" class="glass-input" style="max-width:180px;">
              <option :value="0">改部门…</option>
              <option v-for="o in deptOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
            <button class="btn ghost sm" :disabled="!batchDept" @click="batchSetDept">应用改部门</button>
            <button class="btn ghost sm" @click="batchInGroup(true)">设群内</button>
            <button class="btn ghost sm" @click="batchInGroup(false)">取消群内</button>
            <button class="btn ghost sm" @click="batchFreeze(true)">禁用</button>
            <button class="btn ghost sm" @click="batchFreeze(false)">启用</button>
          </template>
          <button v-if="auth.can('system:user:resetPwd')" class="btn ghost sm" @click="batchResetPwd">重置密码</button>
          <button v-if="auth.can('system:user:forceLogout')" class="btn ghost sm" @click="batchForceLogout">强制下线</button>
          <button v-if="auth.can('system:user:remove')" class="btn danger sm" @click="batchDelete">删除</button>
          <span v-if="!canBatch" class="adm-hint" style="margin:0;">你没有可执行的批量操作权限</span>
        </div>
        <p class="adm-hint" style="margin:4px 0 0;">
          批量操作<b>逐个</b>调用单条接口（而不是一次批处理），因此每一项都受各自权限点保护：
          改部门/群内/启停用 = <code>system:user:edit</code>、重置密码 = <code>system:user:resetPwd</code>、
          强制下线 = <code>system:user:forceLogout</code>、删除 = <code>system:user:remove</code>。
        </p>
      </div>

      <div class="adm-tablewrap">
        <table class="adm-table">
          <thead>
            <tr>
              <th v-if="batchMode" style="width:34px;"></th>
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
              <td v-if="batchMode">
                <input class="adm-check" type="checkbox" :value="u.id" v-model="selectedIds" :disabled="u.id === auth.user?.id" />
              </td>
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
                <span v-if="u.in_group" class="chip" style="color:#16a34a;border-color:#16a34a66">群内</span>
                <span v-if="onlineMap[u.id]" class="chip ok" :title="onlineTitle(u.id)">在线 {{ fmtOnline(onlineMap[u.id]) }}</span>
                <span v-if="u.must_change_pwd" class="chip">待改密</span>
              </td>
              <td class="dim nowrap">{{ u.last_login_at || '—' }}</td>
              <td class="num dim">{{ u.token_version }}</td>
              <td>
                <div class="adm-row-actions">
                  <button v-if="auth.can('system:user:edit')" class="adm-link" @click="openEdit(u)">编辑</button>
                  <button v-if="auth.can('system:user:authRole')" class="adm-link" @click="openRole(u)">分配角色</button>
                  <button v-if="auth.can('system:user:resetPwd')" class="adm-link" @click="doResetPwd(u)">重置密码</button>

                  <!-- 更多：高危 / 低频动作收进下拉，避免一行 7 个按钮 -->
                  <div v-if="hasMore(u)" class="adm-ops" @click.stop>
                    <button class="adm-link" :class="{ on: opsOpen === u.id }" @click="opsOpen = opsOpen === u.id ? 0 : u.id">更多 ▾</button>
                    <div v-if="opsOpen === u.id" class="adm-ops-drop">
                      <button v-if="auth.can('system:log:unlock')" class="adm-op" @click="doUnlock(u)">解锁登录</button>
                      <button
                        v-if="auth.can('system:user:forceLogout')"
                        class="adm-op"
                        :disabled="!onlineMap[u.id]"
                        :title="onlineMap[u.id] ? '' : '该用户当前不在线'"
                        @click="doForceLogout(u)"
                      >强制下线<span v-if="!onlineMap[u.id]" class="adm-op-hint">离线</span></button>
                      <button
                        v-if="auth.can('system:user:edit')"
                        class="adm-op"
                        :disabled="u.id === auth.user?.id"
                        :title="u.id === auth.user?.id ? '不能禁用当前登录账号' : ''"
                        @click="toggleFrozen(u)"
                      >{{ u.frozen ? '启用账号' : '禁用账号' }}</button>
                      <button
                        v-if="auth.can('system:user:remove')"
                        class="adm-op danger"
                        :disabled="u.id === auth.user?.id"
                        :title="u.id === auth.user?.id ? '不能删除当前登录账号' : ''"
                        @click="doDelete(u)"
                      >删除用户</button>
                    </div>
                  </div>
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
          <div class="adm-tree" style="max-height:180px">
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
          <input class="adm-check" type="checkbox" v-model="edit.form.in_group" />
          已加入企业微信通知群（到点推送会 @TA，名单中不重复列出）
        </label>
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
const importing = ref(false)
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

// 批量操作开关：至少要有一项批量能力才显示该按钮
const canBatch = computed(() => auth.canAny([
  'system:user:edit', 'system:user:resetPwd', 'system:user:forceLogout', 'system:user:remove'
]))
// 行内「更多」下拉是否有可选项
function hasMore(u) {
  const self = u.id === auth.user?.id
  return auth.can('system:log:unlock') ||
    auth.can('system:user:forceLogout') ||
    (auth.can('system:user:edit') && !self) ||
    (auth.can('system:user:remove') && !self)
}

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
    // 勾选态跨页保留没有意义（批量动作只作用于已选 id），换页即清空
    selectedIds.value = []
    opsOpen.value = 0
    loadSessions()
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

// ---------------------------------------------------------------- 在线状态
// GET /api/sessions 要求 system:user:sessions（部门管理员被显式排除），
// 因此取不到时只是不显示「在线」标签，不影响其它功能。
const onlineMap = ref({})
async function loadSessions() {
  if (!auth.can('system:user:sessions')) { onlineMap.value = {}; return }
  try {
    const list = await api.get('/sessions')
    const m = {}
    for (const s of (Array.isArray(list) ? list : [])) m[s.user_id] = s.online_sec || 0
    onlineMap.value = m
  } catch (e) { onlineMap.value = {} }
}
function fmtOnline(sec) {
  const s = Number(sec) || 0
  if (s < 60) return s + 's'
  if (s < 3600) return Math.floor(s / 60) + 'm'
  return Math.floor(s / 3600) + 'h' + Math.floor((s % 3600) / 60) + 'm'
}
function onlineTitle(id) {
  const s = onlineMap.value[id]
  return `在线时长 ${fmtOnline(s)}（15 分钟内有活跃请求即视为在线）`
}

// ---------------------------------------------------------------- 导入 / 导出
function downloadAuth(path) {
  const token = localStorage.getItem('sw_token')
  fetch('/api/' + path, { headers: { Authorization: 'Bearer ' + token } })
    .then(async (r) => {
      if (!r.ok) { alert('下载失败：' + r.status); return }
      const blob = await r.blob()
      const cd = r.headers.get('content-disposition') || ''
      const m = cd.match(/filename\*=UTF-8''([^;\s]+)|filename="?([^";]+)"?/)
      const fn = (m && (m[1] || m[2])) ? decodeURIComponent(m[1] || m[2]) : 'download.xlsx'
      const a = document.createElement('a')
      a.href = URL.createObjectURL(blob)
      a.download = fn
      a.click()
      URL.revokeObjectURL(a.href)
    })
    .catch(() => alert('下载失败'))
}

async function importUsers(e) {
  const input = e.target
  const file = input.files && input.files[0]
  input.value = ''
  if (!file) return
  if (!confirm(`将按模板导入用户（${file.name}）。\n登录账号已存在则更新资料，不存在则新建，确认继续？`)) return
  importing.value = true
  try {
    const r = await api.upload('/users/import', file)
    let msg = `导入完成：新建 ${r.created || 0} 人，更新 ${r.updated || 0} 人，失败 ${r.failed || 0} 条`
    if (r.errors && r.errors.length) msg += '\n' + r.errors.slice(0, 8).join('\n')
    alert(msg)
    await load()
  } catch (err) { alert(errMsg(err, '导入失败')) }
  finally { importing.value = false }
}

// ---------------------------------------------------------------- 批量操作
const batchMode = ref(false)
const selectedIds = ref([])
const batchDept = ref(0)
const opsOpen = ref(0) // 行内「更多 ▾」当前展开的用户 id（0 = 全部收起）
const allSelected = computed(() => rows.value.length > 0 && selectedIds.value.length === rows.value.length)
function toggleAll(e) {
  // 不允许批量作用于当前登录账号（后端也会拒绝，这里提前排除以免"选了却没生效"）
  selectedIds.value = e.target.checked ? rows.value.filter((u) => u.id !== auth.user?.id).map((u) => u.id) : []
}
function toggleBatch() {
  batchMode.value = !batchMode.value
  if (!batchMode.value) selectedIds.value = []
}
function needSel() {
  if (!selectedIds.value.length) { alert('请先勾选要操作的用户'); return false }
  return true
}
/** 逐个调用单条接口，返回 {ok, fail, msgs} —— 避免一次批处理绕过单项权限点 */
async function eachSelected(fn) {
  const ids = [...selectedIds.value]
  let ok = 0
  const msgs = []
  for (const id of ids) {
    const u = rows.value.find((x) => x.id === id)
    try { await fn(id, u); ok++ }
    catch (err) { msgs.push(`${u ? u.name || u.username : '#' + id}: ${errMsg(err, '失败')}`) }
  }
  return { ok, fail: msgs.length, msgs }
}
function report(label, r) {
  let msg = `${label}完成：成功 ${r.ok} 人`
  if (r.fail) msg += `，失败 ${r.fail} 人\n` + r.msgs.slice(0, 8).join('\n')
  alert(msg)
  load()
}

// 改部门 / 群内走批量接口（后端 /api/users/batch 本身就是为这两件事设计的，
// 且带 canManageDept 归属断言，权限点 system:user:edit）
async function batchSetDept() {
  if (!needSel() || !batchDept.value) return
  const name = (deptOptions.value.find((o) => o.value === batchDept.value) || {}).label || batchDept.value
  if (!confirm(`把已选的 ${selectedIds.value.length} 人改到部门「${name}」？`)) return
  try {
    const r = await api.post('/users/batch', { ids: selectedIds.value, action: 'set_dept', dept_id: batchDept.value })
    report('批量改部门', { ok: r.processed || 0, fail: r.skipped || 0, msgs: r.skipped ? ['部分用户不在你的管理范围内，已跳过'] : [] })
  } catch (e) { alert(errMsg(e, '批量改部门失败')) }
}
async function batchInGroup(v) {
  if (!needSel()) return
  if (!confirm(`把已选的 ${selectedIds.value.length} 人标记为「${v ? '已入群' : '未入群'}」？`)) return
  try {
    const r = await api.post('/users/batch', { ids: selectedIds.value, action: 'set_in_group', in_group: v })
    report(v ? '设为群内' : '取消群内', { ok: r.processed || 0, fail: r.skipped || 0, msgs: [] })
  } catch (e) { alert(errMsg(e, '操作失败')) }
}
async function batchFreeze(v) {
  if (!needSel()) return
  if (!confirm(`确认${v ? '禁用' : '启用'}已选的 ${selectedIds.value.length} 个账号？${v ? '\n禁用后其登录令牌立即失效。' : ''}`)) return
  const r = await eachSelected((id) => api.post('/system/user/changeStatus', { id, frozen: v }))
  report(v ? '批量禁用' : '批量启用', r)
}
async function batchForceLogout() {
  if (!needSel()) return
  if (!confirm(`强制下线已选的 ${selectedIds.value.length} 人？（其所有设备令牌立即失效，需重新登录）`)) return
  const r = await eachSelected((id) => api.post(`/users/${id}/force-logout`))
  report('批量强制下线', r)
}
async function batchResetPwd() {
  if (!needSel()) return
  if (!confirm(`重置已选 ${selectedIds.value.length} 人的密码？\n系统会为每人生成临时密码，旧令牌全部失效，首次登录强制改密。`)) return
  const pwds = []
  const r = await eachSelected(async (id, u) => {
    const res = await api.post('/system/user/resetPwd', { id })
    pwds.push(`${u ? u.name || u.username : '#' + id} → ${res.password}`)
  })
  let msg = `重置完成：成功 ${r.ok} 人`
  if (r.fail) msg += `，失败 ${r.fail} 人\n` + r.msgs.slice(0, 8).join('\n')
  if (pwds.length) msg += '\n\n临时密码（请通过安全渠道告知本人）：\n' + pwds.slice(0, 20).join('\n')
  alert(msg)
  load()
}
async function batchDelete() {
  if (!needSel()) return
  if (!confirm(`确定删除已选的 ${selectedIds.value.length} 个用户吗？\n该操作会同时解除其全部角色绑定，且不可撤销。`)) return
  const r = await eachSelected((id) => api.post(`/system/user/delete/${id}`))
  report('批量删除', r)
}

// ---------------------------------------------------------------- 新增 / 编辑
const edit = reactive({
  show: false, isCreate: true, id: 0,
  form: { username: '', password: '', name: '', emp_no: '', mobile: '', dept_id: 0, role_ids: [], in_group: false, on_leave: false }
})

function openCreate() {
  Object.assign(edit, {
    show: true, isCreate: true, id: 0,
    form: {
      username: '', password: '', name: '', emp_no: '', mobile: '',
      dept_id: auth.user?.dept_id || 0, role_ids: [], in_group: false, on_leave: false
    }
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
      mobile: u.mobile || '', dept_id: u.dept_id, role_ids: [...roleIds],
      in_group: !!u.in_group, on_leave: !!u.on_leave
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
        mobile: f.mobile, dept_id: f.dept_id, role_ids: f.role_ids,
        in_group: f.in_group, on_leave: f.on_leave
      })
    } else {
      await api.put('/system/user', {
        id: edit.id, name: f.name, emp_no: f.emp_no, mobile: f.mobile,
        dept_id: f.dept_id, role_ids: f.role_ids,
        in_group: f.in_group, on_leave: f.on_leave
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

// 解锁登录：解除该账号在所有 IP 上的登录失败锁定（连续 5 次失败会锁 15 分钟）
async function doUnlock(u) {
  opsOpen.value = 0
  try {
    const r = await api.post('/auth/unlock', { username: u.username })
    const n = r.cleared || 0
    alert(n > 0
      ? `已解除「${u.name || u.username}」的登录锁定（清除 ${n} 条失败记录）。`
      : `「${u.name || u.username}」当前没有登录锁定记录，无需解锁。`)
  } catch (e) { alert(errMsg(e, '解锁失败')) }
}

// 强制下线：递增 token_version + 清会话，该用户所有设备需重新登录
async function doForceLogout(u) {
  opsOpen.value = 0
  if (!confirm(`强制下线「${u.name || u.username}」？\n其所有设备的令牌立即失效，需重新登录。`)) return
  try {
    await api.post(`/users/${u.id}/force-logout`)
    await load()
  } catch (e) { alert(errMsg(e, '强制下线失败')) }
}

function errMsg(e, dft) {
  return (e && e.response && e.response.data && e.response.data.error) || dft
}

onMounted(async () => {
  // 行内「更多 ▾」：点击页面其它地方自动收起（下拉自身用 @click.stop 阻挡冒泡）
  document.addEventListener('click', () => { opsOpen.value = 0 })
  await Promise.all([load(), loadDepts(), loadRoles()])
})
</script>
