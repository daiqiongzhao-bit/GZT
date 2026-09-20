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

      <!-- 多部门管理时：顶部直接切部门，比每次去下拉里翻找快得多 -->
      <div v-if="deptOptions.length > 1" class="dept-switch">
        <span class="ds-label">切换部门</span>
        <button class="ds-chip" :class="{ on: q.dept_id === '' }" @click="switchDept('')">
          全部 <span class="ds-n">{{ deptOptions.length }}</span>
        </button>
        <button
          v-for="o in deptOptions"
          :key="o.value"
          class="ds-chip"
          :class="{ on: String(q.dept_id) === String(o.value) }"
          @click="switchDept(o.value)"
        >{{ cleanDeptLabel(o.label) }}</button>
      </div>

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
                <span v-if="u.must_change_pwd" class="chip">待改密</span>
                <!-- 在线：明确是"哪个端"在线（网页 / PWA / 插件），而不是笼统一句"在线" -->
                <span
                  v-if="onlineOf(u.id)"
                  class="chip ok"
                  :title="onlineTitle(u.id)"
                >{{ onlineOf(u.id).label }}在线 {{ fmtOnline(onlineOf(u.id).sec) }}</span>
              </td>
              <td class="dim nowrap">{{ u.last_login_at || '—' }}</td>
              <td>
                <div class="adm-row-actions">
                  <!-- v0.40.0：行内只留一个入口。
                       分配角色 / 重置密码 / 解锁 / 强制下线 / 禁用 / 删除 都是"改这个用户"的一部分，
                       全部收进「编辑」弹窗 —— 一行 4 个按钮既难扫读，也容易误点高危动作。 -->
                  <button v-if="canEditUser" class="adm-link" @click="openEdit(u)">编辑</button>
                  <span v-else class="dim">—</span>
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
          <label class="fld">角色 *（可多选 —— 一个人可以同时是「排班主管」和「知识库编辑」，权限取并集）</label>
          <div class="adm-tree" style="max-height:190px">
            <div v-for="r in roles" :key="r.id" class="adm-tree-row">
              <label>
                <input
                  class="adm-check"
                  type="checkbox"
                  :value="r.id"
                  v-model="edit.form.role_ids"
                  :disabled="roleLocked"
                />
                <span class="nm">
                  {{ r.role_name }}
                  <span class="chip" style="margin-left:5px">{{ scopeName(r.data_scope) }}</span>
                </span>
              </label>
            </div>
            <div v-if="!roles.length" class="empty">加载角色中…</div>
          </div>
          <p v-if="roleLocked" class="adm-hint" style="margin-top:6px;color:#d97706;">
            你没有「分配角色」权限，角色勾选已锁定（姓名、手机号、部门等资料仍可修改）。
          </p>
          <p v-else class="adm-hint" style="margin-top:6px;">
            只勾「查看」的角色 = 只能看；勾了「修改 / 删除」才有对应能力。
            想改角色能做什么，去「角色管理 → 权限配置」。
          </p>
        </div>

        <div class="adm-field">
          <label class="fld">账号状态</label>
          <label class="adm-inline">
            <input class="adm-check" type="checkbox" v-model="edit.form.in_group" />
            已加入企业微信通知群（到点推送会 @TA）
          </label>
          <label class="adm-inline">
            <input class="adm-check" type="checkbox" v-model="edit.form.on_leave" />
            休假 / 停职（不计入「全员」当班与推送）
          </label>
          <label v-if="!edit.isCreate" class="adm-inline">
            <input
              class="adm-check"
              type="checkbox"
              v-model="edit.form.frozen"
              :disabled="edit.id === auth.user?.id"
            />
            禁用该账号（立即无法登录，不能禁用当前登录的自己）
          </label>
        </div>

        <!-- 高危 / 低频动作：默认收起，避免误点 -->
        <div v-if="!edit.isCreate" class="adm-fold">
          <button class="adm-fold-head" @click="editMore = !editMore">
            <span class="tri">{{ editMore ? '▾' : '▸' }}</span>其他操作（重置密码 / 解锁 / 踢下线 / 删除）
          </button>
          <div v-if="editMore" class="adm-fold-body">
            <div class="op-row">
              <button v-if="auth.can('system:user:resetPwd')" class="btn ghost sm" @click="doResetPwd(edit.row)">重置密码</button>
              <span class="op-desc">生成随机临时密码并使旧令牌失效，本人首次登录须改密</span>
            </div>
            <div v-if="auth.can('system:log:unlock')" class="op-row">
              <button class="btn ghost sm" @click="doUnlock(edit.row)">解锁登录</button>
              <span class="op-desc">解除连续输错密码导致的锁定（5 次失败锁 15 分钟）</span>
            </div>
            <div v-if="auth.can('system:user:forceLogout')" class="op-row">
              <button class="btn ghost sm" :disabled="!onlineOf(edit.id)" @click="doForceLogout(edit.row)">强制下线</button>
              <span class="op-desc">
                {{ onlineOf(edit.id) ? '该用户当前有在线会话，点击后其全部设备需重新登录' : '该用户当前不在线' }}
              </span>
            </div>
            <div v-if="auth.can('system:user:remove')" class="op-row">
              <button
                class="btn danger sm"
                :disabled="edit.id === auth.user?.id"
                @click="doDelete(edit.row)"
              >删除账号</button>
              <span class="op-desc">连同角色绑定一并删除，不可撤销</span>
            </div>
          </div>
        </div>

        <div class="adm-modal-foot">
          <span v-if="!canSaveUser" class="adm-hint" style="margin-right:auto;color:#d97706;">
            你没有「{{ edit.isCreate ? '新增用户' : '编辑用户' }}」权限，此处只能查看
          </span>
          <button class="btn ghost" @click="edit.show = false">取消</button>
          <button class="btn primary" :disabled="saving || !canSaveUser" @click="submitEdit">
            {{ saving ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <!-- ============ 临时密码结果（v0.40.4） ============
         原来用原生 alert 显示临时密码，弹窗里的文本既选不中也复制不了；
         而临时密码是"只出现这一次"的凭据，管理员必须能当场带走。
         故改为结果弹窗：密码等宽大字 + 复制按钮（含非安全上下文降级）+ 整段单击选中。 -->
    <div v-if="pwdRes.show" class="adm-mask" @click.self="pwdRes.show = false">
      <div class="adm-modal" role="dialog" aria-modal="true" aria-label="临时密码">
        <div class="pwd-head">
          <span class="pwd-ico" aria-hidden="true">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6L9 17l-5-5"/></svg>
          </span>
          <div class="pwd-head-t">
            <h3>{{ pwdRes.batch ? '批量重置完成' : '密码已重置' }}</h3>
            <p class="adm-modal-sub">
              <template v-if="pwdRes.batch">共 {{ pwdRes.items.length }} 个账号拿到了新密码</template>
              <template v-else>{{ pwdRes.name }}<template v-if="pwdRes.username"> · {{ pwdRes.username }}</template></template>
            </p>
          </div>
        </div>

        <!-- 单个用户：密码大字 + 复制 -->
        <div v-if="!pwdRes.batch" class="pwd-row">
          <code class="pwd-code" tabindex="0" title="点击整段选中，可直接 Ctrl+C" @click="pickText">{{ pwdRes.password }}</code>
          <button
            class="btn primary pwd-cp"
            :class="{ done: pwdCopied === 'one' }"
            @click="copyText(pwdRes.password, 'one')"
          >{{ pwdCopied === 'one' ? '已复制' : '复制' }}</button>
        </div>
        <p v-if="pwdCopyFail" class="pwd-cpfail">自动复制被浏览器拦截，请点击密码框全选后按 Ctrl+C 复制。</p>

        <!-- 批量：逐行复制 + 复制全部（制表符分隔，可直接粘进 Excel 两列） -->
        <template v-else>
          <div class="pwd-batch-bar">
            <span class="pwd-batch-n">成功 {{ pwdRes.items.length }} 人</span>
            <button
              class="btn ghost sm pwd-cp-all"
              :class="{ done: pwdCopied === 'all' }"
              @click="copyAllPwd"
            >{{ pwdCopied === 'all' ? '已复制全部' : '复制全部' }}</button>
          </div>
          <div class="pwd-list">
            <div v-for="(it, i) in pwdRes.items" :key="i" class="pwd-li">
              <span class="pwd-li-n" :title="it.name">{{ it.name }}</span>
              <code class="pwd-li-p" tabindex="0" title="点击整段选中，可直接 Ctrl+C" @click="pickText">{{ it.password }}</code>
              <button
                class="btn ghost sm pwd-cp-sm"
                :class="{ done: pwdCopied === 'i' + i }"
                @click="copyText(it.password, 'i' + i)"
              >{{ pwdCopied === 'i' + i ? '已复制' : '复制' }}</button>
            </div>
            <p v-if="pwdCopyFail" class="pwd-cpfail">自动复制被浏览器拦截，请点击密码框全选后按 Ctrl+C 复制。</p>
            <p v-if="pwdRes.fail" class="pwd-fail">失败 {{ pwdRes.fail }} 人：{{ pwdRes.failMsgs.join('；') }}</p>
          </div>
        </template>

        <div class="adm-alert warn pwd-warn">
          请通过安全渠道告知本人；首次登录会强制改密。<b>关闭后这些密码不再显示</b>，请先复制留存。
        </div>

        <div class="adm-modal-foot">
          <button class="btn ghost" @click="pwdRes.show = false">我已复制，关闭</button>
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
// 行内「编辑」入口：只要对用户有任意一项管理能力就显示。
// 按钮本身只做展示，各项动作在弹窗内仍按各自权限点单独门控（后端 GuardByPath 兜底）。
const canEditUser = computed(() => auth.canAny([
  'system:user:edit', 'system:user:authRole', 'system:user:resetPwd',
  'system:user:forceLogout', 'system:user:remove', 'system:log:unlock'
]))
// 角色勾选是否锁定：编辑已有用户时需要有「分配角色」权限（后端同样做字段级校验）；
// 新建用户不受限 —— 创建时分配角色是流程必需，由「新增用户」权限覆盖。
const canAssignRole = computed(() => auth.can('system:user:authRole'))
const roleLocked = computed(() => !edit.isCreate && !canAssignRole.value)
// 保存按钮门控：新建要「新增用户」权限，编辑要「编辑用户」权限。
// 只有「分配角色」而没有「编辑」的人打开弹窗时，保存会被禁用（后端 routeperm 同样会 403）。
const canSaveUser = computed(() => (edit.isCreate ? auth.can('system:user:add') : auth.can('system:user:edit')))

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
//
// 返回值里 clients = 登录端（web / pwa / extension）。只显示"在线"信息量太低 ——
// 管理员真正想知道的是"他是在电脑网页上、还是装了 PWA、还是在浏览器插件里挂着"。
const CLIENT_LABEL = { web: '网页', pwa: 'PWA', extension: '插件' }
const onlineMap = ref({}) // user_id → { sec, clients, count, ips, loginAt, lastSeen }
async function loadSessions() {
  if (!auth.can('system:user:sessions')) { onlineMap.value = {}; return }
  try {
    const list = await api.get('/sessions')
    const m = {}
    for (const s of (Array.isArray(list) ? list : [])) {
      m[s.user_id] = {
        sec: s.online_sec || 0,
        clients: s.clients || [],
        count: s.count || 1,
        ips: s.ips || [],
        loginAt: s.login_at || '',
        lastSeen: s.last_seen || ''
      }
    }
    onlineMap.value = m
  } catch (e) { onlineMap.value = {} }
}
function onlineOf(id) {
  const o = onlineMap.value[id]
  if (!o) return null
  return { label: clientText(o.clients), sec: o.sec }
}
function clientText(clients) {
  const arr = (clients || []).map((c) => CLIENT_LABEL[c] || c)
  return arr.length ? arr.join(' / ') : '网页'
}
function fmtOnline(sec) {
  const s = Number(sec) || 0
  if (s < 60) return s + 's'
  if (s < 3600) return Math.floor(s / 60) + 'm'
  return Math.floor(s / 3600) + 'h' + Math.floor((s % 3600) / 60) + 'm'
}
function onlineTitle(id) {
  const o = onlineMap.value[id]
  return `在线 ${fmtOnline(o.sec)}（15 分钟内有请求即视为在线）
登录端：${clientText(o.clients)}${o.count > 1 ? `，共 ${o.count} 个会话` : ''}
${o.ips.length ? 'IP：' + o.ips.join(', ') + '\n' : ''}最近活跃：${o.lastSeen || '—'}`
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
  const items = []
  const r = await eachSelected(async (id, u) => {
    const res = await api.post('/system/user/resetPwd', { id })
    items.push({ name: (u && (u.name || u.username)) || ('#' + id), password: res.password || '' })
  })
  // v0.40.4：结果改为弹窗展示（逐行可复制 + 复制全部），不再拼进 alert 文本
  pwdCopied.value = ''
  Object.assign(pwdRes, {
    show: true, batch: true,
    name: '', username: '', password: '',
    items, fail: r.fail, failMsgs: r.msgs.slice(0, 8)
  })
  load()
}
async function batchDelete() {
  if (!needSel()) return
  if (!confirm(`确定删除已选的 ${selectedIds.value.length} 个用户吗？\n该操作会同时解除其全部角色绑定，且不可撤销。`)) return
  const r = await eachSelected((id) => api.post(`/system/user/delete/${id}`))
  report('批量删除', r)
}

// ---------------------------------------------------------------- 新增 / 编辑
// row：当前编辑的列表行（危险操作的确认文案要用到姓名，操作后也要刷新）
const edit = reactive({
  show: false, isCreate: true, id: 0, row: null,
  form: { username: '', password: '', name: '', emp_no: '', mobile: '', dept_id: 0, role_ids: [], in_group: false, on_leave: false, frozen: false }
})
const editMore = ref(false) // 「其他操作」折叠区

// ---------------------------------------------------------------- 临时密码结果
// 重置密码后弹出的结果卡：临时密码是"只出现这一次"的凭据，
// 必须让管理员能一键复制带走（原生 alert 里的文字选不中也复制不了）。
const pwdRes = reactive({
  show: false, batch: false,
  name: '', username: '', password: '',
  items: [],     // 批量：[{ name, password }]
  fail: 0, failMsgs: []
})
// 当前处于"已复制"反馈态的按钮标识（1.6s 后自动回落）
const pwdCopied = ref('')
// 两条复制通道都失败时的行内提示（不用 window.prompt —— 阻塞式，打断操作）
const pwdCopyFail = ref(false)

// 复制到剪贴板。
//
// ★★ 必须有 execCommand 降级：线上是 http://…:8090，属于**非安全上下文**，
//    此时 navigator.clipboard 是 undefined（不是"报错"，是根本不存在），
//    只写 clipboard 分支的话按钮点下去毫无反应。
function copyText(text, key) {
  const flash = () => {
    pwdCopyFail.value = false
    pwdCopied.value = key
    setTimeout(() => { if (pwdCopied.value === key) pwdCopied.value = '' }, 1600)
  }
  const warn = () => {
    pwdCopyFail.value = true
    setTimeout(() => { pwdCopyFail.value = false }, 3000)
  }
  const legacy = () => {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.style.position = 'fixed'
    ta.style.top = '-1000px'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    ta.setSelectionRange(0, ta.value.length)
    let ok = false
    try { ok = document.execCommand('copy') } catch (e) { ok = false }
    document.body.removeChild(ta)
    if (ok) flash()
    else warn()
  }
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).then(flash).catch(legacy)
  } else {
    legacy()
  }
}

// 批量复制：姓名<TAB>密码，一行一条 —— 粘进 Excel / 微信都是整齐两列
function copyAllPwd() {
  copyText(pwdRes.items.map((it) => `${it.name}\t${it.password}`).join('\n'), 'all')
}

// 单击整段选中：即使不点复制按钮，也能直接 Ctrl+C 带走
function pickText(e) {
  const el = e.currentTarget
  const range = document.createRange()
  range.selectNodeContents(el)
  const sel = window.getSelection()
  sel.removeAllRanges()
  sel.addRange(range)
}

function openCreate() {
  Object.assign(edit, {
    show: true, isCreate: true, id: 0, row: null,
    form: {
      username: '', password: '', name: '', emp_no: '', mobile: '',
      dept_id: auth.user?.dept_id || 0, role_ids: [], in_group: false, on_leave: false, frozen: false
    }
  })
  editMore.value = false
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
    show: true, isCreate: false, id: u.id, row: u,
    form: {
      username: u.username, password: '', name: u.name, emp_no: u.emp_no || '',
      mobile: u.mobile || '', dept_id: u.dept_id, role_ids: [...roleIds],
      in_group: !!u.in_group, on_leave: !!u.on_leave, frozen: !!u.frozen
    }
  })
  editMore.value = false
}

async function submitEdit() {
  const f = edit.form
  if (!f.username) { alert('请填写登录名'); return }
  if (!f.dept_id) { alert('请选择归属部门'); return }
  if (!f.role_ids.length) { alert('请至少为用户分配 1 个角色（系统不再隐式补执行者）'); return }
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
      // 启用 / 禁用走独立接口（PUT /system/user 不承载 frozen，避免"改个名字顺手把账号启停改了"）
      const was = !!edit.row?.frozen
      if (f.frozen !== was) {
        await api.post('/system/user/changeStatus', { id: edit.id, frozen: f.frozen })
      }
    }
    edit.show = false
    await load()
  } catch (e) {
    alert(errMsg(e, '保存失败'))
  } finally { saving.value = false }
}

// 角色分配已并入「编辑」弹窗（同一个表单里改资料 + 改角色，一次提交）。
// 单独弹窗的语义问题：改完角色还要回来再改部门，两次请求、两次 token 失效。

// ---------------------------------------------------------------- 其它动作
async function doResetPwd(u) {
  if (!confirm(`确定重置「${u.name || u.username}」的密码吗？\n重置后该用户旧令牌立即失效，且下次登录必须改密。`)) return
  try {
    const r = await api.post('/system/user/resetPwd', { id: u.id })
    // v0.40.4：改用结果弹窗而不是 alert，临时密码才能一键复制
    pwdCopied.value = ''
    Object.assign(pwdRes, {
      show: true, batch: false,
      name: u.name || u.username, username: u.username || '',
      password: r.password || '',
      items: [], fail: 0, failMsgs: []
    })
    await load()
  } catch (e) { alert(errMsg(e, '重置密码失败')) }
}

async function doDelete(u) {
  if (!u) return
  if (!confirm(`确定删除「${u.name || u.username}」吗？\n该操作会同时解除其全部角色绑定，且不可撤销。`)) return
  try {
    await api.post(`/system/user/delete/${u.id}`)
    edit.show = false // 被删的用户已不存在，弹窗必须关掉
    await load()
  } catch (e) { alert(errMsg(e, '删除失败')) }
}

// 解锁登录：解除该账号在所有 IP 上的登录失败锁定（连续 5 次失败会锁 15 分钟）
async function doUnlock(u) {
  if (!u) return
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
  if (!u) return
  if (!confirm(`强制下线「${u.name || u.username}」？\n其所有设备的令牌立即失效，需重新登录。`)) return
  try {
    await api.post(`/users/${u.id}/force-logout`)
    await load()
  } catch (e) { alert(errMsg(e, '强制下线失败')) }
}

// ---------------------------------------------------------------- 部门切换
// 顶部"切换部门"：仅在当前登录人**能管多个部门**时出现（数据范围过滤后的部门数 > 1）。
function cleanDeptLabel(label) {
  return String(label || '').replace(/^[\s　]*└?\s*/, '')
}
function switchDept(v) {
  q.dept_id = v === '' ? '' : v
  page.value = 1
  load()
}

function errMsg(e, dft) {
  return (e && e.response && e.response.data && e.response.data.error) || dft
}

onMounted(async () => {
  await Promise.all([load(), loadDepts(), loadRoles()])
})
</script>

<style scoped>
/* ---- 顶部「切换部门」 ---- */
.dept-switch { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; margin: 8px 0 12px; }
.ds-label { font-size: 12px; color: var(--text-faint); margin-right: 2px; }
.ds-chip {
  padding: 3px 11px; border-radius: 999px; border: 1px solid var(--glass-border);
  background: transparent; cursor: pointer; font-size: 12.5px; color: var(--text-dim);
}
.ds-chip:hover { border-color: var(--accent, #6366f1); color: inherit; }
.ds-chip.on { background: var(--accent, #6366f1); border-color: var(--accent, #6366f1); color: #fff; }
.ds-n { opacity: 0.72; font-size: 11px; }

/* ---- 弹窗内折叠区 ---- */
.adm-fold { border: 1px solid var(--glass-border); border-radius: 10px; margin: 10px 0; overflow: hidden; }
.adm-fold-head {
  width: 100%; text-align: left; background: transparent; border: 0; cursor: pointer;
  padding: 9px 12px; font-size: 13px; color: var(--text-dim);
}
.adm-fold-head:hover { background: rgba(127, 127, 127, 0.06); }
.adm-fold-body { padding: 4px 14px 12px; }
.tri { display: inline-block; width: 14px; color: var(--text-faint); }
.op-row { display: flex; align-items: center; gap: 10px; padding: 5px 0; }
.op-desc { font-size: 12px; color: var(--text-faint); line-height: 1.6; }

/* ---- 临时密码结果弹窗（v0.40.4） ---- */
.pwd-head { display: flex; align-items: flex-start; gap: 10px; margin-bottom: 16px; }
.pwd-ico {
  flex: none; width: 30px; height: 30px; border-radius: 9px;
  display: grid; place-items: center;
  background: rgba(5, 150, 105, 0.13); color: var(--ok, #059669);
}
.pwd-head-t { min-width: 0; }
.pwd-head-t h3 { margin: 0 0 3px; }
.pwd-head-t .adm-modal-sub { margin: 0; }

/* 密码本体：等宽 + 大字距，避免 l/1、O/0 看错 */
.pwd-row { display: flex; align-items: stretch; gap: 8px; }
.pwd-code, .pwd-li-p {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  color: var(--text);
  background: var(--overlay);
  border: 1px solid var(--glass-border);
  border-radius: 10px;
  cursor: text;
}
.pwd-code { flex: 1 1 auto; min-width: 0; font-size: 17px; font-weight: 600; letter-spacing: 1.2px; padding: 11px 13px; overflow-x: auto; white-space: nowrap; }
.pwd-code::selection, .pwd-li-p::selection { background: var(--accent-soft, rgba(79, 70, 229, 0.18)); }
.pwd-code:focus-visible, .pwd-li-p:focus-visible { outline: 2px solid var(--accent, #6366f1); outline-offset: 2px; }

.pwd-cp { flex: none; padding: 0 16px; }
/* 用 button.pwd-*.done（0,2,1）压过 .btn.primary / .btn.ghost（0,2,0），不依赖打包顺序 */
button.pwd-cp.done, button.pwd-cp-sm.done, button.pwd-cp-all.done {
  background: var(--ok, #059669); border-color: var(--ok, #059669); color: #fff;
}

.pwd-batch-bar { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.pwd-batch-n { font-size: 12px; color: var(--text-dim); }
.pwd-cp-all { margin-left: auto; }

.pwd-list { max-height: 260px; overflow: auto; border: 1px solid var(--glass-border); border-radius: 10px; }
.pwd-li { display: flex; align-items: center; gap: 8px; padding: 7px 8px 7px 11px; border-bottom: 1px solid var(--glass-border); }
.pwd-li:last-child { border-bottom: 0; }
.pwd-li-n { flex: none; width: 68px; font-size: 12.5px; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pwd-li-p { flex: 1 1 auto; min-width: 0; font-size: 13px; letter-spacing: 0.6px; padding: 4px 9px; overflow-x: auto; white-space: nowrap; }
.pwd-cp-sm { flex: none; }
.pwd-fail { margin: 0; padding: 7px 11px; font-size: 12px; color: var(--danger); background: var(--overlay); }
.pwd-cpfail { margin: 7px 0 0; font-size: 12px; color: var(--warn, #d97706); line-height: 1.6; }
.pwd-warn { margin: 14px 0 0; }
</style>
