<template>
  <div class="wp">
    <!-- 顶部切换 -->
    <div class="wp-tabs">
      <button :class="['wp-tab', { on: tab === 'overview' }]" @click="tab = 'overview'">概览</button>
      <button :class="['wp-tab', { on: tab === 'tasks' }]" @click="tab = 'tasks'">任务</button>
      <button :class="['wp-tab', { on: tab === 'logs' }]" @click="tab = 'logs'">运行日志</button>
      <button :class="['wp-tab', { on: tab === 'settings' }]" @click="tab = 'settings'">设置</button>
      <span class="wp-spacer" />
      <button class="wp-btn ghost" @click="refreshAll" v-html="icons.clock" title="刷新" />
    </div>

    <!-- 概览 -->
    <section v-show="tab === 'overview'" class="wp-sec">
      <div class="wp-stats">
        <div class="wp-stat glass"><div class="wp-num">{{ summary.task_total || 0 }}</div><div class="wp-lab">任务总数</div></div>
        <div class="wp-stat glass"><div class="wp-num ok">{{ summary.task_enabled || 0 }}</div><div class="wp-lab">已启用</div></div>
        <div class="wp-stat glass"><div class="wp-num">{{ summary.today_runs || 0 }}</div><div class="wp-lab">今日运行</div></div>
        <div class="wp-stat glass"><div class="wp-num">{{ summary.today_pushed || 0 }}</div><div class="wp-lab">今日推送条数</div></div>
      </div>

      <div class="wp-grid">
        <div class="glass wp-card">
          <div class="wp-card-h">下次运行</div>
          <div class="wp-next" v-if="summary.next_run_at">
            <strong>{{ summary.next_run_at }}</strong>
            <span class="wp-chip">{{ summary.next_task }}</span>
          </div>
          <div class="wp-muted" v-else>暂无启用任务</div>
          <div class="wp-muted sm">服务器时间：{{ summary.server_time }}</div>
          <div class="wp-cli" :class="cliState.cls">
            <span class="wp-dot" />
            <div>
              <div>企微 CLI：<b>{{ cliState.text }}</b></div>
              <div class="wp-muted sm" v-if="summary.cli">{{ summary.cli.detail }}</div>
            </div>
          </div>
        </div>

        <div class="glass wp-card">
          <div class="wp-card-h">最近运行</div>
          <table class="wp-tbl" v-if="(summary.recent_logs || []).length">
            <thead><tr><th>时间</th><th>任务</th><th>触发</th><th>条数</th><th>状态</th></tr></thead>
            <tbody>
              <tr v-for="l in summary.recent_logs" :key="l.id">
                <td class="wp-muted">{{ fmt(l.run_at) }}</td>
                <td>{{ l.task_name }}</td>
                <td>{{ trigText(l.trigger) }}</td>
                <td>{{ l.count }}</td>
                <td><span class="wp-tag" :class="statusCls(l.status)">{{ l.status }}</span></td>
              </tr>
            </tbody>
          </table>
          <div class="wp-muted" v-else>尚无运行记录</div>
        </div>
      </div>
    </section>

    <!-- 任务 -->
    <section v-show="tab === 'tasks'" class="wp-sec">
      <div class="wp-toolbar">
        <button class="wp-btn primary" @click="openNew"><span v-html="icons.plus" /> 新建任务</button>
        <span class="wp-muted">共 {{ tasks.length }} 个任务</span>
      </div>
      <div class="glass wp-card">
        <table class="wp-tbl">
          <thead>
            <tr><th>名称</th><th>子表</th><th>日期列 / 提前</th><th>目标群</th><th>发送</th><th>状态</th><th>上次运行</th><th>操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="t in tasks" :key="t.id">
              <td><b>{{ t.name }}</b></td>
              <td class="wp-muted">{{ t.sheet_title }}<br /><span class="wp-muted sm">{{ t.doc_id }}</span></td>
              <td>{{ t.date_field }} <span class="wp-chip">+{{ t.offset_days }}</span></td>
              <td>{{ t.group_name }}</td>
              <td>{{ t.send_time }}</td>
              <td>
                <span class="wp-tag" :class="t.enabled ? 'on' : 'off'">{{ t.enabled ? '启用' : '停用' }}</span>
                <span class="wp-tag" :class="statusCls(t.last_status)" v-if="t.last_status">{{ t.last_status }}</span>
              </td>
              <td class="wp-muted sm">
                <template v-if="t.last_run_at">{{ fmt(t.last_run_at) }}<br />{{ t.last_count }} 条</template>
                <span v-else>—</span>
                <div class="wp-err" v-if="t.last_error">{{ t.last_error }}</div>
              </td>
              <td class="wp-acts">
                <button class="wp-mini" :title="'立即运行'" @click="runTask(t, false)">运行</button>
                <button class="wp-mini" :title="'试跑(不发送)'" @click="runTask(t, true)">试跑</button>
                <button class="wp-mini" :title="'预览SQL'" @click="previewSQL(t)">SQL</button>
                <button class="wp-mini" :title="t.enabled ? '停用' : '启用'" @click="toggleTask(t)">{{ t.enabled ? '停用' : '启用' }}</button>
                <button class="wp-mini" @click="openEdit(t)">编辑</button>
                <button class="wp-mini danger" @click="delTask(t)">删除</button>
              </td>
            </tr>
            <tr v-if="!tasks.length"><td colspan="8" class="wp-muted" style="text-align:center;padding:24px">暂无任务，点击「新建任务」</td></tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- 日志 -->
    <section v-show="tab === 'logs'" class="wp-sec">
      <div class="wp-toolbar">
        <button class="wp-btn ghost" @click="loadLogs">刷新日志</button>
        <span class="wp-muted">最近 {{ logs.length }} 条</span>
      </div>
      <div class="glass wp-card">
        <table class="wp-tbl">
          <thead><tr><th>时间</th><th>任务</th><th>触发</th><th>条数</th><th>状态</th><th>文件</th><th>说明</th><th>耗时</th></tr></thead>
          <tbody>
            <tr v-for="l in logs" :key="l.id">
              <td class="wp-muted">{{ fmt(l.run_at) }}</td>
              <td>{{ l.task_name }}</td>
              <td>{{ trigText(l.trigger) }}</td>
              <td>{{ l.count }}</td>
              <td><span class="wp-tag" :class="statusCls(l.status)">{{ l.status }}</span></td>
              <td class="wp-muted sm">
                <a v-if="l.file_name" class="wp-file-link" href="javascript:void(0)"
                   :title="'点击下载 ' + l.file_name" @click="downloadLogFile(l)">{{ l.file_name }}</a>
                <span v-else>—</span>
              </td>
              <td class="wp-muted">{{ l.message || '—' }}</td>
              <td class="wp-muted">{{ l.duration_ms ? (l.duration_ms / 1000).toFixed(1) + 's' : '—' }}</td>
            </tr>
            <tr v-if="!logs.length"><td colspan="8" class="wp-muted" style="text-align:center;padding:24px">暂无日志</td></tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- 设置 -->
    <section v-show="tab === 'settings'" class="wp-sec">
      <div class="wp-grid">
        <div class="glass wp-card">
          <div class="wp-card-h">任务失败提醒</div>
          <label class="wp-field row">
            <span>失败时通知管理员</span>
            <input type="checkbox" v-model="setForm.wp_notify_on_failure" />
          </label>
          <label class="wp-field"><span>通知群（下拉选择）</span>
            <select v-model="setForm.wp_notify_group">
              <option value="">— 请先点「刷新可发会话」加载群列表 —</option>
              <option v-for="g in groups" :key="g.chat_id" :value="g.chat_name">{{ g.chat_name }}（{{ g.chat_type }}）</option>
            </select>
          </label>
          <button class="wp-btn" @click="loadGroups">刷新可发会话</button>
          <button class="wp-btn ghost" @click="testNotify">发送测试消息</button>
          <!-- v0.41.1：独立于企微群的告警通道。填了就发，不依赖「失败时通知管理员」开关，
               也不要求已授权企微 —— 可对接自建告警系统 / 钉钉 / 飞书等任意 HTTP 端点。 -->
          <label class="wp-field" style="margin-top:10px"><span>失败告警 Webhook（独立通道，可选）</span>
            <input v-model.trim="setForm.wp_alert_webhook" placeholder="https://your-alert-system/hook" />
          </label>
          <div class="wp-muted sm" style="margin-top:4px">
            留空则不启用。任务失败时会异步 POST JSON（含任务名 / 触发方式 / 时间 / 失败原因）到该地址。
          </div>
          <button class="wp-btn primary" style="margin-top:10px" @click="saveSettings">保存设置</button>
          <div class="wp-cli" :class="cliState.cls" style="margin-top:10px">
            <span class="wp-dot" />
            <div>
              <div>企微 CLI：<b>{{ cliState.text }}</b></div>
              <div class="wp-muted sm" v-if="summary.cli">{{ summary.cli.detail }}</div>
            </div>
          </div>
          <button class="wp-btn ghost sm" @click="refreshCli">重新检查 CLI 状态</button>
        </div>

        <div class="glass wp-card">
          <div class="wp-card-h">重新授权（企业微信机器人）</div>
          <div class="wp-cli" :class="auth.authorized ? 'ok' : (auth.waiting ? 'warn' : 'bad')">
            <span class="wp-dot" />
            <div>
              <div>当前状态：<b>{{ auth.authorized ? '已授权' : (auth.waiting ? '等待扫码 / 输入' : '未授权') }}</b>
                <span v-if="auth.botId"> · Bot：{{ auth.botId }}</span></div>
              <div class="wp-muted sm" v-if="auth.err">{{ auth.err }}</div>
            </div>
          </div>

          <!-- 方式二：扫码（服务端自动等待） -->
          <div class="wp-sub">方式二：扫码授权（推荐，非交互环境可用）</div>
          <div v-if="auth.waiting" class="wp-qr">
            <img :src="qrImg" alt="二维码" class="wp-qrimg" />
            <div class="wp-muted sm">用企业微信扫上方二维码，或在手机浏览器打开：</div>
            <a :href="auth.scanUrl" target="_blank" rel="noopener" class="wp-link" style="word-break:break-all">{{ auth.scanUrl }}</a>
            <div class="wp-qrbar">
              <button class="wp-btn ghost sm" @click="cancelAuth">取消并恢复原授权</button>
              <span class="wp-muted sm">每 3 秒自动检查授权结果…</span>
            </div>
          </div>
          <button v-else class="wp-btn" @click="startQr">生成二维码</button>

          <div class="wp-divider" />

          <!-- 方式一：手动 Bot ID + Secret -->
          <div class="wp-sub">方式一：Bot ID + Secret 授权</div>
          <label class="wp-field"><span>Bot ID</span><input v-model="auth.botIdInput" placeholder="机器人 Bot ID" /></label>
          <label class="wp-field"><span>Secret</span><input v-model="auth.secretInput" type="password" placeholder="机器人 Secret" /></label>
          <button class="wp-btn primary" @click="manualAuth">手动授权</button>
        </div>

        <!-- 访问权限：仅超级管理员可见可改 -->
        <div class="glass wp-card" v-if="canConfig">
          <div class="wp-card-h">访问权限（仅超级管理员可设置）</div>
          <div class="wp-muted sm" style="margin-bottom:10px">
            勾选哪些角色可以<b>查看和修改</b>「企微推送」。超级管理员始终保留权限、不可取消；
            未勾选的角色看不到入口，即使直接访问接口也会被拒绝。
          </div>
          <label class="wp-field row" v-for="r in perm.roles" :key="r.value">
            <span>{{ r.label }}</span>
            <input type="checkbox" :value="r.value" v-model="perm.selected" :disabled="r.value === 'super_admin'" />
          </label>
          <button class="wp-btn primary" style="margin-top:10px" :disabled="perm.saving" @click="saveAccess">
            {{ perm.saving ? '保存中…' : '保存权限' }}
          </button>
        </div>
      </div>
    </section>

    <!-- 任务表单 -->
    <div class="wp-modal" v-if="showForm" @click.self="showForm = false">
      <div class="wp-modal-card glass">
        <div class="wp-modal-h">
          <b>{{ editingId ? '编辑任务' : '新建任务' }}</b>
          <button class="wp-x" @click="showForm = false">✕</button>
        </div>
        <div class="wp-modal-body">
          <label class="wp-field"><span>任务名称 *</span><input v-model="form.name" placeholder="如：满25天到期明细推送" /></label>
          <label class="wp-field row"><span>启用</span><input type="checkbox" v-model="form.enabled" /></label>
          <div class="wp-2col">
            <label class="wp-field"><span>文档 ID *</span>
              <input v-model="form.doc_id" placeholder="粘贴智能表格链接或直接填 docid，自动提取" @change="extractDocId" @paste="onDocPaste" />
            </label>
            <label class="wp-field"><span>子表名 *</span><input v-model="form.sheet_title" placeholder="如：入库明细" /></label>
          </div>
          <div class="wp-2col">
            <label class="wp-field"><span>日期列 *</span>
              <input v-model="form.date_field" list="dateFieldsList" placeholder="如：到期日期" />
            </label>
            <label class="wp-field"><span>提前/延后天数</span><input type="number" v-model="form.offset_days" /></label>
          </div>
          <datalist id="dateFieldsList"><option v-for="d in dateFields" :key="d" :value="d" /></datalist>
          <button class="wp-btn ghost sm" @click="loadFields">加载字段（自动填充日期列候选）</button>

          <div class="wp-sub">日期列（这些列在 Excel 里会被格式化为 YYYY-MM-DD）</div>
          <div class="wp-chipbar">
            <span class="wp-link" @click="autoDateCols">自动勾选日期</span>
            <span class="wp-link" @click="form.date_cols = []">清空</span>
          </div>
          <div class="wp-chips">
            <span v-for="f in fields" :key="'dchip' + f.title" class="wp-chip" :class="{ on: form.date_cols.includes(f.title) }" @click="toggleArr(form.date_cols, f.title)">{{ f.title }}</span>
          </div>
          <div class="wp-tags" v-if="form.date_cols.length">
            <span class="wp-tagitem" v-for="(c, i) in form.date_cols" :key="'dc' + i">{{ i + 1 }}. {{ c }}
              <button @click="form.date_cols.splice(i, 1)">✕</button>
            </span>
          </div>
          <div class="wp-hint" v-else>未勾选时仅「日期列」一个字段会被格式化为日期</div>

          <div class="wp-sub">附加筛选条件（可多个）</div>
          <div class="wp-conds">
            <div class="wp-cond" v-for="(c, i) in form.conditions" :key="'c' + i">
              <input class="wp-cond-f" v-model="c.field" list="allFieldsList" placeholder="列名" />
              <select v-model="c.op">
                <option v-for="o in OP_OPTS" :key="o.v" :value="o.v">{{ o.t }}</option>
              </select>
              <input v-if="needVal(c.op)" class="wp-cond-v" v-model="c.value" placeholder="比较值" />
              <button class="wp-mini danger" @click="form.conditions.splice(i, 1)">✕</button>
            </div>
            <datalist id="allFieldsList"><option v-for="f in fields" :key="f.title" :value="f.title" /></datalist>
            <button class="wp-btn ghost sm" @click="form.conditions.push({ field: '', op: 'is_null', value: '' })">+ 添加条件</button>
          </div>

          <div class="wp-sub">输出列（不选则输出表格全部字段，按表格原始列顺序）</div>
          <div class="wp-chipbar">
            <span class="wp-link" @click="form.columns = fields.map((f) => f.title)">全选</span>
            <span class="wp-link" @click="form.columns = []">清空</span>
          </div>
          <div class="wp-chips">
            <span v-for="f in fields" :key="'cchip' + f.title" class="wp-chip" :class="{ on: form.columns.includes(f.title) }" @click="toggleArr(form.columns, f.title)">{{ f.title }}</span>
          </div>
          <div class="wp-tags" v-if="form.columns.length">
            <span class="wp-tagitem" v-for="(c, i) in form.columns" :key="'col' + i">{{ i + 1 }}. {{ c }}
              <button @click="form.columns.splice(i, 1)">✕</button>
            </span>
          </div>
          <div class="wp-hint" v-else>点选芯片即选中，勾选顺序 = 导出表头顺序；下方序号列出了当前顺序</div>

          <div class="wp-2col">
            <label class="wp-field"><span>文件名模板</span>
              <input v-model="form.file_name_template" placeholder="如：满25天明细_{date}；留空用 文件名前缀_日期" />
            </label>
            <label class="wp-field"><span>发送时间 *</span><input v-model="form.send_time" placeholder="HH:MM" /></label>
          </div>
          <label class="wp-field"><span>消息模板</span>
            <textarea v-model="form.msg_template" class="wp-tpl" rows="3"
              placeholder="留空用默认文案。可用变量：{title} {date} {count} {filename} {task}&#10;如：{title} {date} 共 {count} 条，详见附件《{filename}》"></textarea>
          </label>
          <div class="wp-hint">变量：{title}=消息标题 {date}=目标日期 {count}=条数 {filename}=附件文件名 {task}=任务名；文件名模板另支持 {prefix}（即下方文件名前缀，留空时兜底）</div>
          <div class="wp-2col">
            <label class="wp-field"><span>文件名前缀</span><input v-model="form.file_prefix" placeholder="如：满25天到期明细" /></label>
            <label class="wp-field"><span>消息标题</span><input v-model="form.msg_title" placeholder="推送文案前缀，模板里用 {title} 引用" /></label>
          </div>
          <label class="wp-field"><span>目标群 *</span>
            <select v-model="form.group_name">
              <option value="">— 请选择机器人可发送的群 —</option>
              <option v-for="g in groups" :key="g.chat_id" :value="g.chat_name">{{ g.chat_name }}（{{ g.chat_type }}）</option>
            </select>
          </label>
          <label class="wp-field"><span>0 条时提示语</span><input v-model="form.empty_text" placeholder="如：今日无到期明细" /></label>

          <div class="wp-modal-foot">
            <button class="wp-btn ghost" @click="showForm = false">取消</button>
            <button class="wp-btn primary" @click="saveForm">保存</button>
          </div>
        </div>
      </div>
    </div>

    <!-- SQL 预览 -->
    <div class="wp-modal" v-if="showSql" @click.self="showSql = false">
      <div class="wp-modal-card glass">
        <div class="wp-modal-h"><b>SQL 预览</b><button class="wp-x" @click="showSql = false">✕</button></div>
        <pre class="wp-sql">{{ sqlText }}</pre>
      </div>
    </div>

    <div class="wp-toast" v-if="toastMsg" :class="toastType">{{ toastMsg }}</div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, nextTick } from 'vue'
import * as api from '@/api'
import { icons } from '@/icons'
import { useAuthStore } from '@/store/auth'

// 全局登录态（用于判断当前用户能否设置「访问权限」）
const store = useAuthStore()

// ---------- 访问权限（默认仅管理员；超管可调白名单）----------
// 仅超级管理员可修改，见后端 UpdateAccess（PUT /wecom-push/access）
const canConfig = computed(() => store.canConfigWecom)
const perm = reactive({ roles: [], selected: [], saving: false })

async function loadAccess() {
  if (!store.canConfigWecom) return // 非超管不拉，省一次请求
  try {
    const r = await api.get('/wecom-push/access')
    perm.roles = r.roles || []
    perm.selected = [...(r.allowed_roles || [])]
  } catch { /* 读取失败保持原样，不打断页面 */ }
}

async function saveAccess() {
  perm.saving = true
  try {
    const r = await api.put('/wecom-push/access', { allowed_roles: perm.selected })
    perm.selected = [...(r.allowed_roles || [])]
    await store.fetchWpAccess() // 同步全局权限，导航项随之更新
    toast('权限已保存', 'success')
  } catch (e) {
    toast(errMsg(e), 'error')
  } finally {
    perm.saving = false
  }
}

const tab = ref('overview')
const loading = ref(false)
const toastMsg = ref('')
const toastType = ref('info')
let toastTimer = null
function toast(msg, type = 'info') {
  toastMsg.value = msg
  toastType.value = type
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => (toastMsg.value = ''), 3600)
}
function errMsg(e) {
  return (e && e.response && e.response.data && e.response.data.error) || (e && e.message) || '请求失败'
}

const OP_OPTS = [
  { v: 'is_null', t: '为空' },
  { v: 'not_null', t: '非空' },
  { v: 'eq', t: '等于' },
  { v: 'ne', t: '不等于' },
  { v: 'contains', t: '包含' },
  { v: 'not_contains', t: '不包含' },
  { v: 'in', t: '属于(逗号分隔)' },
  { v: 'date_eq', t: '日期等于' }
]
const needVal = (op) => !['is_null', 'not_null'].includes(op)

const summary = ref({})
const tasks = ref([])
const logs = ref([])
const settings = ref({})
const groups = ref([])
const fields = ref([])
const dateFields = ref([])

const cliState = computed(() => {
  const c = summary.value.cli || {}
  if (c.available && c.authorized) return { cls: 'ok', text: '可用 · 已授权' + (c.version ? ' ' + c.version : '') }
  if (c.available && !c.authorized) return { cls: 'warn', text: '已安装但未授权' }
  return { cls: 'bad', text: c.available ? '未知' : '不可用' }
})

function statusCls(s) {
  if (s === 'success' || s === 'enabled' || s === 'on') return 'on'
  if (s === 'failed' || s === 'off' || s === 'danger') return 'bad'
  if (s === 'empty') return 'off'
  return 'off'
}
function trigText(t) {
  return { schedule: '定时', manual: '手动', 'dry-run': '试跑', catchup: '漏跑补偿' }[t] || t || '—'
}
function fmt(s) {
  if (!s) return '—'
  const d = new Date(s)
  if (isNaN(d)) return s
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

async function loadSummary() {
  try {
    summary.value = (await api.get('/wecom-push/summary')) || {}
  } catch (e) { toast(errMsg(e), 'error') }
}
async function loadTasks() {
  loading.value = true
  try { tasks.value = (await api.get('/wecom-push/tasks')) || [] }
  catch (e) { toast(errMsg(e), 'error') } finally { loading.value = false }
}
async function loadLogs() {
  try { logs.value = (await api.get('/wecom-push/logs', { limit: 200 })) || [] }
  catch (e) { toast(errMsg(e), 'error') }
}
// 运行日志「文件」列：点击下载附件。用 api.download（走 axios 配置位传 responseType），
// 自动带 Authorization 头，拿到 blob 后用动态 <a download> 触发，避免 <a href> 不带 Bearer 被 401 拦截。
async function downloadLogFile(l) {
  if (!l || !l.file_name) return
  try {
    const blob = await api.download('/wecom-push/files/' + encodeURIComponent(l.file_name))
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = l.file_name
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  } catch (e) { toast(errMsg(e), 'error') }
}
async function loadSettings() {
  try {
    settings.value = (await api.get('/wecom-push/settings/full')) || {}
    setForm.wp_notify_on_failure = settings.value.wp_notify_on_failure === 'true'
    setForm.wp_notify_group = settings.value.wp_notify_group || ''
    setForm.wp_alert_webhook = settings.value.wp_alert_webhook || ''
  } catch (e) { toast(errMsg(e), 'error') }
}
async function loadGroups() {
  try { const r = await api.get('/wecom-push/groups'); groups.value = (r && r.groups) || [] }
  catch (e) { toast('读取会话失败：' + errMsg(e), 'error') }
}
async function refreshCli() {
  try { const r = await api.get('/wecom-push/cli-status'); summary.value = { ...summary.value, cli: r } }
  catch (e) { toast(errMsg(e), 'error') }
}
async function refreshAll() {
  await Promise.all([loadSummary(), loadTasks(), loadLogs(), loadSettings(), loadGroups(), loadAuthStatus(), loadAccess()])
}

// 任务表单
const showForm = ref(false)
const editingId = ref(null)
const form = reactive(defaultForm())
function defaultForm() {
  return {
    name: '', enabled: true, doc_id: '', sheet_title: '', date_field: '', offset_days: 0,
    date_cols: [], conditions: [{ field: '', op: 'is_null', value: '' }], columns: [],
    file_prefix: '', msg_title: '', empty_text: '', group_name: '', send_time: '09:00',
    file_name_template: '', msg_template: ''
  }
}
// v0.40.8：文档 ID 允许粘贴完整链接，自动提取 docid（query 的 docid= 或路径最后一段）
function extractDocId() {
  const s = String(form.doc_id || '').trim()
  if (!s || !/https?:\/\//i.test(s)) return
  let out = s
  try {
    const u = new URL(s)
    const q = u.searchParams.get('docid')
    if (q) out = q
    else {
      const segs = u.pathname.split('/').filter(Boolean)
      if (segs.length) out = segs[segs.length - 1]
    }
  } catch { /* 非法 URL 保持原值，交后端兜底 */ }
  if (out !== s) form.doc_id = out
}
function onDocPaste(e) {
  // paste 后 v-model 还没更新，nextTick 再提取
  nextTick(extractDocId)
}
function parseArr(s) { try { const a = JSON.parse(s); return Array.isArray(a) ? a : [] } catch { return [] } }
function parseConds(s) {
  try { const a = JSON.parse(s); if (Array.isArray(a) && a.length) return a.map((c) => ({ field: c.field || '', op: c.op || 'is_null', value: c.value || '' })) } catch {}
  return [{ field: '', op: 'is_null', value: '' }]
}
// 芯片点选：点一下选中（追加到末尾，勾选顺序 = 输出顺序），再点取消
function toggleArr(arr, v) {
  const i = arr.indexOf(v)
  if (i >= 0) arr.splice(i, 1)
  else arr.push(v)
}
function autoDateCols() { form.date_cols = dateFields.value.slice() }
function openNew() { editingId.value = null; Object.assign(form, defaultForm()); showForm.value = true }
function openEdit(t) {
  editingId.value = t.id
  form.name = t.name; form.enabled = t.enabled; form.doc_id = t.doc_id; form.sheet_title = t.sheet_title
  form.date_field = t.date_field; form.offset_days = t.offset_days; form.date_cols = parseArr(t.date_cols)
  form.conditions = parseConds(t.conditions); form.columns = parseArr(t.columns)
  form.file_prefix = t.file_prefix || ''; form.msg_title = t.msg_title || ''; form.empty_text = t.empty_text || ''
  form.file_name_template = t.file_name_template || ''; form.msg_template = t.msg_template || ''
  form.group_name = t.group_name || ''; form.send_time = t.send_time || '09:00'
  showForm.value = true
  if (form.doc_id && form.sheet_title) loadFields()
}
async function loadFields() {
  if (!form.doc_id || !form.sheet_title) { toast('请先填写 文档ID 与 子表名', 'warn'); return }
  try {
    const r = await api.get('/wecom-push/fields', { doc_id: form.doc_id, sheet_title: form.sheet_title })
    fields.value = (r && r.fields) || []
    dateFields.value = (r && r.date_fields) || []
    toast('已加载 ' + fields.value.length + ' 个字段', 'success')
  } catch (e) { toast(errMsg(e), 'error') }
}
async function saveForm() {
  const payload = {
    name: form.name, enabled: form.enabled, doc_id: form.doc_id, sheet_title: form.sheet_title,
    date_field: form.date_field, offset_days: Number(form.offset_days) || 0,
    date_cols: form.date_cols, conditions: form.conditions.filter((c) => c.field && c.op),
    columns: form.columns, file_prefix: form.file_prefix, msg_title: form.msg_title,
    empty_text: form.empty_text, group_name: form.group_name, send_time: form.send_time,
    file_name_template: form.file_name_template, msg_template: form.msg_template
  }
  try {
    if (editingId.value) await api.put('/wecom-push/tasks/' + editingId.value, payload)
    else await api.post('/wecom-push/tasks', payload)
    toast('已保存', 'success'); showForm.value = false; await loadTasks(); await loadSummary()
  } catch (e) { toast(errMsg(e), 'error') }
}
async function delTask(t) {
  if (!confirm('确认删除任务「' + t.name + '」？')) return
  try { await api.del('/wecom-push/tasks/' + t.id); toast('已删除', 'success'); await loadTasks() }
  catch (e) { toast(errMsg(e), 'error') }
}
async function toggleTask(t) {
  try {
    const r = await api.post('/wecom-push/tasks/' + t.id + '/toggle')
    const i = tasks.value.findIndex((x) => x.id === t.id); if (i >= 0) tasks.value[i] = r
    await loadSummary()
  } catch (e) { toast(errMsg(e), 'error') }
}
async function runTask(t, dry) {
  try {
    const r = await api.post('/wecom-push/tasks/' + t.id + '/run', { dry_run: !!dry })
    toast((dry ? '试跑' : '运行') + '：' + (r.message || r.status || ''), r.status === 'failed' ? 'error' : 'success')
    await loadTasks(); await loadLogs(); await loadSummary()
  } catch (e) { toast(errMsg(e), 'error') }
}
const showSql = ref(false)
const sqlText = ref('')
async function previewSQL(t) {
  try { const r = await api.get('/wecom-push/tasks/' + t.id + '/sql'); sqlText.value = (r.sql || '') + '\n-- 目标日期: ' + (r.target_date || ''); showSql.value = true }
  catch (e) { toast(errMsg(e), 'error') }
}

// 设置
const setForm = reactive({ wp_notify_on_failure: false, wp_notify_group: '', wp_alert_webhook: '' })
async function saveSettings() {
  try {
    await api.put('/wecom-push/settings', {
      wp_notify_on_failure: setForm.wp_notify_on_failure ? 'true' : 'false', wp_notify_group: setForm.wp_notify_group,
      wp_alert_webhook: setForm.wp_alert_webhook
    })
    toast('设置已保存', 'success'); await loadSettings()
  } catch (e) { toast(errMsg(e), 'error') }
}
async function testNotify() {
  try { const r = await api.post('/wecom-push/notify/test', { group: setForm.wp_notify_group }); toast(r.message || '已发送', 'success') }
  catch (e) { toast(errMsg(e), 'error') }
}

// 重新授权（扫码 / 手动 Bot ID + Secret）
const auth = reactive({ status: 'idle', authorized: false, botId: '', botIdInput: '', secretInput: '', scanUrl: '', qrUrl: '', mode: '', waiting: false, err: '' })
const qrTick = ref(0)
const qrImg = computed(() => (auth.qrUrl ? auth.qrUrl + '?t=' + qrTick.value : ''))
let authPoll = null
function stopAuthPoll() { if (authPoll) { clearInterval(authPoll); authPoll = null } }
async function loadAuthStatus() {
  try {
    const r = await api.get('/wecom-push/auth/status')
    auth.authorized = !!r.authorized
    auth.waiting = r.status === 'waiting'
    auth.botId = r.bot_id || ''
    auth.scanUrl = r.scan_url || ''
    auth.qrUrl = r.qr_url || ''
    auth.status = r.status
    if (auth.authorized) {
      stopAuthPoll()
      toast('重新授权成功：' + (r.bot_id || ''), 'success')
    } else if (auth.waiting) {
      if (!authPoll) authPoll = setInterval(loadAuthStatus, 3000)
    } else {
      stopAuthPoll()
    }
  } catch (e) { /* 轮询静默失败 */ }
}
async function startQr() {
  try {
    const r = await api.post('/wecom-push/auth/start')
    auth.waiting = true
    auth.scanUrl = r.scan_url || ''
    auth.qrUrl = r.qr_url || ''
    auth.mode = 'qr'
    auth.err = ''
    qrTick.value = Date.now()
    toast('二维码已生成，请用企业微信扫码', 'success')
    stopAuthPoll(); authPoll = setInterval(loadAuthStatus, 3000)
  } catch (e) { toast(errMsg(e), 'error') }
}
async function cancelAuth() {
  try {
    const r = await api.post('/wecom-push/auth/cancel')
    stopAuthPoll()
    auth.waiting = false
    auth.authorized = !!r.authorized
    auth.botId = r.bot_id || ''
    auth.scanUrl = ''; auth.qrUrl = ''
    auth.err = ''
    toast('已取消，已恢复原授权', 'info')
  } catch (e) { toast(errMsg(e), 'error') }
}
async function manualAuth() {
  if (!auth.botIdInput || !auth.secretInput) { toast('请填写 Bot ID 和 Secret', 'warn'); return }
  auth.err = ''
  try {
    const r = await api.post('/wecom-push/auth/manual', { bot_id: auth.botIdInput, secret: auth.secretInput })
    auth.authorized = !!r.authorized
    auth.botId = r.bot_id || ''
    auth.botIdInput = ''; auth.secretInput = ''
    toast('授权成功：' + (r.bot_id || ''), 'success')
  } catch (e) {
    auth.err = errMsg(e)
    toast(errMsg(e), 'error')
  }
}

onMounted(refreshAll)
</script>

<style scoped>
.wp { padding: 4px 2px 40px; }
.wp-tabs { display: flex; align-items: center; gap: 6px; margin-bottom: 16px; flex-wrap: wrap; }
.wp-tab { border: 1px solid var(--glass-border); background: var(--overlay); color: var(--text-dim); padding: 8px 16px; border-radius: 10px; cursor: pointer; font-size: 13.5px; }
.wp-tab.on { background: var(--accent-soft); color: var(--accent); border-color: rgba(79, 70, 229, 0.4); font-weight: 700; }
.wp-spacer { flex: 1; }
.wp-sec { display: block; }
.wp-stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 14px; margin-bottom: 16px; }
.wp-stat { border-radius: var(--radius); padding: 16px 18px; }
.wp-num { font-size: 26px; font-weight: 800; line-height: 1; }
.wp-num.ok { color: var(--accent); }
.wp-lab { font-size: 12.5px; color: var(--text-dim); margin-top: 6px; }
.wp-grid { display: grid; grid-template-columns: 1fr; gap: 16px; }
.wp-card { border-radius: var(--radius); padding: 16px 18px; }
.wp-card-h { font-size: 13px; font-weight: 700; color: var(--text-dim); margin-bottom: 12px; }
.wp-next strong { font-size: 15px; }
.glass { background: var(--bg-1); border: 1px solid var(--glass-border); }
.wp-chip { display: inline-block; margin-left: 6px; padding: 1px 8px; border-radius: 999px; background: var(--overlay-2); color: var(--text-dim); font-size: 12px; }
.wp-muted { color: var(--text-dim); }
.wp-muted.sm { font-size: 12px; }
.wp-err { color: var(--danger); font-size: 12px; margin-top: 2px; }
.wp-cli { display: flex; gap: 10px; align-items: flex-start; margin-top: 12px; padding: 10px 12px; border-radius: 10px; background: var(--overlay); border: 1px solid var(--glass-border); }
.wp-cli.ok { border-color: rgba(34, 197, 94, 0.4); }
.wp-cli.warn { border-color: rgba(217, 119, 6, 0.4); }
.wp-cli.bad { border-color: rgba(220, 38, 38, 0.4); }
.wp-dot { width: 10px; height: 10px; border-radius: 50%; margin-top: 4px; flex: none; background: var(--text-faint); }
.wp-cli.ok .wp-dot { background: #22c55e; }
.wp-cli.warn .wp-dot { background: #d97706; }
.wp-cli.bad .wp-dot { background: #dc2626; }
.wp-tbl { width: 100%; border-collapse: collapse; font-size: 13px; }
.wp-tbl th { text-align: left; color: var(--text-faint); font-weight: 600; padding: 8px 10px; border-bottom: 1px solid var(--glass-border); white-space: nowrap; }
.wp-tbl td { padding: 9px 10px; border-bottom: 1px solid var(--overlay); vertical-align: top; }
.wp-tbl tbody tr:hover { background: var(--overlay); }
.wp-tag { display: inline-block; padding: 1px 9px; border-radius: 999px; font-size: 12px; border: 1px solid var(--glass-border); color: var(--text-dim); }
.wp-tag.on { background: rgba(34, 197, 94, 0.16); color: #16a34a; border-color: rgba(34, 197, 94, 0.4); }
.wp-tag.bad { background: rgba(220, 38, 38, 0.16); color: #dc2626; border-color: rgba(220, 38, 38, 0.4); }
.wp-tag.off { color: var(--text-faint); }
.wp-acts { display: flex; gap: 4px; flex-wrap: wrap; }
.wp-mini { border: 1px solid var(--glass-border); background: var(--overlay); color: var(--text-dim); border-radius: 8px; padding: 4px 9px; font-size: 12px; cursor: pointer; }
.wp-mini:hover { color: var(--accent); border-color: var(--accent); }
.wp-mini.danger:hover { color: #dc2626; border-color: #dc2626; }
.wp-toolbar { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.wp-btn { display: inline-flex; align-items: center; gap: 6px; border: 1px solid var(--glass-border); background: var(--overlay); color: var(--text); padding: 8px 14px; border-radius: 10px; cursor: pointer; font-size: 13.5px; }
.wp-btn.primary { background: var(--accent); color: #fff; border-color: var(--accent); }
.wp-btn.ghost { background: transparent; }
.wp-btn.sm { padding: 5px 10px; font-size: 12.5px; }
.wp-btn:hover { filter: brightness(1.05); }
.wp-field { display: flex; flex-direction: column; gap: 5px; margin-bottom: 12px; font-size: 13px; color: var(--text-dim); }
.wp-field.row { flex-direction: row; align-items: center; justify-content: space-between; }
.wp-field input, .wp-field select { border: 1px solid var(--glass-border); background: var(--bg-1); color: var(--text); border-radius: 9px; padding: 9px 11px; font-size: 13.5px; outline: none; }
.wp-field input:focus, .wp-field select:focus { border-color: var(--accent); }
.wp-2col { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.wp-sub { font-size: 12.5px; color: var(--text-dim); margin: 8px 0 6px; font-weight: 600; }
.wp-tags { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 8px; }
.wp-tagitem { display: inline-flex; align-items: center; gap: 4px; background: var(--overlay-2); border: 1px solid var(--glass-border); border-radius: 8px; padding: 3px 4px 3px 10px; font-size: 12.5px; }
.wp-tagitem button { border: none; background: transparent; color: var(--text-dim); cursor: pointer; font-size: 12px; }
.wp-taginput { border: 1px dashed var(--glass-border); background: transparent; color: var(--text); border-radius: 8px; padding: 5px 9px; font-size: 12.5px; outline: none; min-width: 160px; }
.wp-chipbar { display: flex; justify-content: flex-end; gap: 14px; margin: 2px 0 6px; }
.wp-link { color: var(--accent, #6366f1); cursor: pointer; font-size: 12.5px; user-select: none; }
.wp-link:hover { text-decoration: underline; }
.wp-file-link { color: var(--accent, #6366f1); cursor: pointer; text-decoration: underline; font-size: 12.5px; }
.wp-file-link:hover { filter: brightness(1.1); }
.wp-chips { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 8px; }
.wp-chip { padding: 4px 13px; border-radius: 999px; border: 1px solid var(--glass-border); background: var(--overlay-1, transparent); color: var(--text); font-size: 13px; cursor: pointer; user-select: none; transition: all .12s; }
.wp-chip:hover { border-color: var(--accent, #6366f1); }
.wp-chip.on { background: color-mix(in srgb, var(--accent, #6366f1) 16%, transparent); border-color: var(--accent, #6366f1); color: var(--accent, #6366f1); font-weight: 600; }
.wp-hint { font-size: 12px; color: var(--text-dim); margin: -4px 0 8px; }
.wp-divider { height: 1px; background: var(--glass-border); margin: 16px 0; }
.wp-qr { display: flex; flex-direction: column; gap: 8px; align-items: flex-start; margin-bottom: 12px; }
.wp-qrimg { width: 200px; height: 200px; border-radius: 10px; background: #fff; padding: 8px; border: 1px solid var(--glass-border); }
.wp-qrbar { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-top: 4px; }
.wp-conds { display: flex; flex-direction: column; gap: 8px; margin-bottom: 8px; }
.wp-cond { display: flex; gap: 8px; align-items: center; }
.wp-cond-f { flex: 1.2; border: 1px solid var(--glass-border); background: var(--bg-1); color: var(--text); border-radius: 8px; padding: 7px 9px; font-size: 13px; outline: none; }
.wp-cond select { border: 1px solid var(--glass-border); background: var(--bg-1); color: var(--text); border-radius: 8px; padding: 7px 9px; font-size: 13px; outline: none; }
.wp-cond-v { flex: 1; border: 1px solid var(--glass-border); background: var(--bg-1); color: var(--text); border-radius: 8px; padding: 7px 9px; font-size: 13px; outline: none; }
.wp-modal { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.45); display: grid; place-items: center; z-index: 80; padding: 20px; }
.wp-modal-card { width: min(720px, 96vw); max-height: 90vh; border-radius: var(--radius); display: flex; flex-direction: column; }
.wp-modal-h { display: flex; align-items: center; justify-content: space-between; padding: 14px 18px; border-bottom: 1px solid var(--glass-border); font-size: 15px; }
.wp-x { border: none; background: transparent; color: var(--text-dim); cursor: pointer; font-size: 16px; }
.wp-modal-body { padding: 18px; overflow: auto; }
.wp-modal-foot { display: flex; justify-content: flex-end; gap: 10px; margin-top: 16px; }
.wp-sql { background: var(--bg-1); border: 1px solid var(--glass-border); border-radius: 10px; padding: 14px; font-size: 12.5px; white-space: pre-wrap; word-break: break-all; max-height: 70vh; overflow: auto; color: var(--text); }
.wp-toast { position: fixed; left: 50%; bottom: 28px; transform: translateX(-50%); padding: 10px 18px; border-radius: 10px; font-size: 13.5px; z-index: 90; box-shadow: 0 8px 24px rgba(0, 0, 0, 0.25); }
.wp-toast.info { background: #1f2937; color: #fff; }
.wp-toast.success { background: #16a34a; color: #fff; }
.wp-toast.error { background: #dc2626; color: #fff; }
.wp-toast.warn { background: #d97706; color: #fff; }
@media (max-width: 880px) { .wp-grid, .wp-2col { grid-template-columns: 1fr; } }
</style>
