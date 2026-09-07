<template>
  <div class="workspace">
    <div class="ws-head">
      <h2 class="page-title">工作台</h2>
      <div class="ws-tool">
        <div class="tabs">
          <button class="tab" :class="{ active: tab === 'knowledge' }" @click="switchTab('knowledge')">
            迷你知识库<template v-if="tab==='knowledge'&&auth.user"> · 沉淀方法/流程</template>
          </button>
          <button class="tab" :class="{ active: tab === 'logs' }" @click="switchTab('logs')">工作日志</button>
          <button class="tab" :class="{ active: tab === 'handover' }" @click="switchTab('handover')">
            交接接力<template v-if="inboxUnread>0&&tab!=='handover'">·<i class="handover-badge">{{ inboxUnread }}</i></template>
          </button>
        </div>
        <button class="btn ghost export-bundle" @click="exportBundle" title="把当前可见的知识/日志/交接及附件打包成一个 zip，便于备份或迁移到正式环境">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" style="width:15px;height:15px;vertical-align:-2px"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="M7 10l5 5 5-5"/><path d="M12 15V3"/></svg>
          打包下载 zip
        </button>
      </div>
    </div>

    <!-- ========== 迷你知识库 ========== -->
    <template v-if="tab === 'knowledge'">
      <section class="panel">
        <div class="panel-head">
          <h3 class="section-title">知识条目 <span class="section-sub">共 {{ knowledge.length }} 条</span></h3>
          <div class="head-actions">
            <button v-if="!editingK" class="btn ghost" @click="exportK" title="把当前筛选可见的知识条目导出为文本">导出知识</button>
            <button v-if="!editingK" class="btn primary" @click="openNewK">+ 新建</button>
            <button v-else class="btn ghost" @click="cancelEditK">返回列表</button>
          </div>
        </div>

        <!-- 内联新增/编辑表单 -->
        <form v-if="editingK" class="edit-form" @submit.prevent="saveK">
          <div class="fg2">
            <div>
              <label class="fld">标题 *</label>
              <input v-model="kForm.title" class="glass-input" required placeholder="如：周年庆小红书发布 SOP" />
            </div>
            <div>
              <label class="fld">分类</label>
              <input v-model="kForm.category" class="glass-input" list="kCats" placeholder="如：活动SOP / 检查流程 / 话术" />
              <datalist id="kCats">
                <option v-for="c in categories" :key="c" :value="c" />
              </datalist>
            </div>
          </div>
          <div>
            <label class="fld">内容</label>
            <textarea v-model="kForm.content" class="glass-input ta" rows="6" placeholder="把方法、步骤、注意事项写下来，方便以后/同事查阅"></textarea>
          </div>
          <div class="fg2 bot">
            <div class="scope-switch">
              <label class="fld">可见范围</label>
              <select v-model="kForm.scope" class="glass-input">
                <option value="department">同部门共享</option>
                <option value="private">仅自己可见</option>
              </select>
            </div>
            <div class="form-actions">
              <button type="button" class="btn ghost" @click="cancelEditK">取消</button>
              <button type="submit" class="btn primary" :disabled="savingK">{{ savingK ? '保存中…' : (kForm.id ? '保存修改' : '保存') }}</button>
            </div>
          </div>
        </form>

        <!-- 筛选 -->
        <div v-else class="filter-bar">
          <input v-model="kQuery" class="glass-input search" placeholder="🔍 搜索标题 / 内容 / 分类" @input="loadK" />
          <select v-model="kCategory" class="glass-input" @change="loadK">
            <option value="">全部分类</option>
            <option v-for="c in categories" :key="c" :value="c">{{ c }}</option>
          </select>
          <label class="chk mine"><input type="checkbox" v-model="kMine" @change="loadK" /> 只看我写的</label>
        </div>

        <!-- 列表 -->
        <div v-if="knowledge.length" class="k-grid">
          <div v-for="k in knowledge" :key="k.id" class="k-card" :class="{ mine: k.owner_id === auth.user?.id }">
            <div class="k-top">
              <span class="chip" :class="scopeChip(k.scope)">{{ scopeLabel(k.scope) }}</span>
              <span v-if="k.category" class="chip accent">{{ k.category }}</span>
              <span class="k-owner">{{ k.owner_name === (auth.user && auth.user.username) ? '我' : k.owner_name }}</span>
            </div>
            <div class="k-title" @click="openDetailK(k)">{{ k.title }}</div>
            <div class="k-preview" @click="openDetailK(k)">{{ (k.content || '').slice(0, 160) || '（暂无内容）' }}</div>
            <div class="k-foot">
              <span class="dim">{{ fmtTime(k.updated_at || k.created_at) }}</span>
              <span v-if="k.owner_id === auth.user?.id" class="ops">
                <button class="del" @click="openEditK(k)">编辑</button>
                <button class="del danger" @click="removeK(k)">删除</button>
              </span>
            </div>
          </div>
        </div>
        <div v-else class="empty">{{ kQuery ? '没有匹配的知识条目' : '还没有知识条目，点右上「新建」沉淀第一条吧' }}</div>
      </section>

      <!-- 知识详情弹窗 -->
      <div v-if="viewK" class="modal-mask" @click.self="viewK = null">
        <div class="modal">
          <div class="modal-head">
            <span class="chip" :class="scopeChip(viewK.scope)">{{ scopeLabel(viewK.scope) }}</span>
            <span v-if="viewK.category" class="chip accent">{{ viewK.category }}</span>
            <button class="modal-close" @click="viewK = null">✕</button>
          </div>
          <h3 class="modal-title">{{ viewK.title }}</h3>
          <div class="modal-meta dim">
            {{ viewK.owner_name === (auth.user && auth.user.username) ? '我' : viewK.owner_name }} · 更新于 {{ fmtTime(viewK.updated_at || viewK.created_at) }}
          </div>
          <div class="modal-scroll">
            <div class="modal-body">{{ viewK.content || '（暂无内容）' }}</div>

            <!-- 附件区 -->
            <div v-if="viewK" class="modal-attach">
              <div class="att-head">
                <span class="att-title">附件（{{ kAtts.length }}）</span>
                <div v-if="canEditK(viewK)" class="att-upload">
                  <span v-if="uploading" class="dim up-txt">上传中…</span>
                  <label class="btn sm">+ 上传文件
                    <input type="file" multiple :disabled="uploading" @change="uploadAtts" hidden />
                  </label>
                </div>
              </div>
              <p v-if="canEditK(viewK)" class="att-hint dim">支持任意文件类型（图片可预览），单文件 ≤ 100MB</p>
              <div v-if="kAtts.length" class="att-list">
                <div v-for="a in kAtts" :key="a.id" class="att-item">
                  <img v-if="a.mime && a.mime.startsWith('image/')" :src="thumbUrl(a)" class="att-thumb" :alt="a.file_name" @click="previewAtt(a)" title="点击预览" />
                  <span v-else class="att-ico" @click="downloadAtt(a)" :title="'下载 ' + a.file_name">{{ fileIcon(a.file_name) }}</span>
                  <span class="att-name" :title="'下载 ' + a.file_name" @click="downloadAtt(a)">{{ a.file_name }}</span>
                  <span class="att-size dim">{{ fmtSize(a.size) }}</span>
                  <button v-if="canDelAtt(a)" class="del danger" @click="removeAtt(a)">删除</button>
                </div>
              </div>
              <div v-else class="empty att-empty">还没有附件，可上传截图 / 文档等补充资料</div>
            </div>
          </div>

          <div class="modal-foot">
            <button v-if="viewK.owner_id === auth.user?.id" class="btn ghost" @click="editFromView">编辑</button>
            <button class="btn primary" @click="viewK = null">关闭</button>
          </div>
        </div>
      </div>
    </template>

    <!-- ========== 工作日志 ========== -->
    <template v-if="tab === 'logs'">
      <section class="panel">
        <div class="panel-head">
          <h3 class="section-title">工作日志
            <span class="section-sub">{{ viewDate }} · 共 {{ logs.length }} 篇</span>
          </h3>
          <div class="head-actions">
            <input type="date" v-model="viewDate" class="glass-input date-input" @change="loadLogs" />
            <button v-if="!editingLog" class="btn primary" @click="openNewLog">+ 写一篇</button>
            <button v-else class="btn ghost" @click="cancelEditLog">收起</button>
          </div>
        </div>

        <!-- 我写的/部门共享 切换（日志可共享，便于组内互见交接当天进展） -->
        <div class="log-view-toggle">
          <button class="seg" :class="{ on: logScope === 'mine' }" @click="setLogScope('mine')">我写的</button>
          <button class="seg" :class="{ on: logScope === 'dept' }" @click="setLogScope('dept')">部门共享日志</button>
        </div>

        <!-- 内联写日志 -->
        <form v-if="editingLog" class="edit-form" @submit.prevent="saveLog">
          <div class="fg2">
            <div>
              <label class="fld">日期 *</label>
              <input v-model="logForm.log_date" type="date" class="glass-input" required />
            </div>
            <div>
              <label class="fld">本篇主题</label>
              <input v-model="logForm.title" class="glass-input" placeholder="如：早班开档 / 周年庆活动跟进" />
            </div>
          </div>
          <div>
            <label class="fld"><b style="color:var(--ok)">✅ 今天做了什么</b></label>
            <textarea v-model="logForm.done" class="glass-input ta" rows="3" placeholder="逐条写下今天完成的事项"></textarea>
          </div>
          <div>
            <label class="fld"><b style="color:var(--warn)">⏳ 还没做完的 / 待办遗留</b></label>
            <textarea v-model="logForm.pending" class="glass-input ta" rows="2" placeholder="哪些还没做完，可让交接/同事清楚从哪继续"></textarea>
          </div>
          <div class="fg2 bot">
            <div class="scope-switch">
              <label class="fld">可见范围</label>
              <select v-model="logForm.scope" class="glass-input">
                <option value="private">仅自己可见</option>
                <option value="department">同部门共享</option>
              </select>
            </div>
            <div class="form-actions">
              <button type="button" class="btn ghost" @click="cancelEditLog">取消</button>
              <button type="submit" class="btn primary" :disabled="savingLog">{{ savingLog ? '保存中…' : (logForm.id ? '保存修改' : '保存') }}</button>
            </div>
          </div>
        </form>

        <!-- 日志列表 -->
        <div v-if="logs.length" class="log-list">
          <div v-for="l in logs" :key="l.id" class="log-card">
            <div class="log-head">
              <span class="log-time">{{ l.log_date }}</span>
              <span v-if="l.title" class="log-title">{{ l.title }}</span>
              <span class="chip" :class="scopeChip(l.scope)">{{ scopeLabel(l.scope) }}</span>
              <span class="dim owner">{{ l.owner_name === (auth.user && auth.user.username) ? '我' : l.owner_name }}</span>
              <span v-if="l.owner_id === auth.user?.id" class="ops">
                <button class="del" @click="openEditLog(l)">编辑</button>
                <button class="del danger" @click="removeLog(l)">删除</button>
              </span>
            </div>
            <div v-if="l.done" class="log-sec done">
              <span class="sec-label">今天做了什么</span>{{ l.done }}
            </div>
            <div v-if="l.pending" class="log-sec pend">
              <span class="sec-label">还没做完的</span>{{ l.pending }}
            </div>
            <div v-if="!l.done && !l.pending" class="dim">（本篇暂无内容）</div>
          </div>
        </div>
        <div v-else class="empty">这一天还没有日志，点「写一篇」记录今天的工作吧</div>
      </section>
    </template>

    <!-- ========== 交接接力 ========== -->
    <template v-if="tab === 'handover'">
      <section class="panel">
        <div class="panel-head">
          <h3 class="section-title">交接接力 <span class="section-sub">把做到哪、接下来做什么清楚转给下一个人</span></h3>
          <div class="head-actions">
            <div class="sub-tabs">
              <button class="seg" :class="{ on: hScope === 'inbox' }" @click="setHScope('inbox')">我收到的<template v-if="inboxUnread"> ({{ inboxUnread }})</template></button>
              <button class="seg" :class="{ on: hScope === 'outbox' }" @click="setHScope('outbox')">我发出的</button>
            </div>
            <button v-if="!editingH" class="btn primary" @click="openNewH">+ 发起交接</button>
            <button v-else class="btn ghost" @click="cancelEditH">返回列表</button>
          </div>
        </div>

        <!-- 新建交接 -->
        <form v-if="editingH" class="edit-form" @submit.prevent="saveH">
          <div>
            <label class="fld">交接标题 *</label>
            <input v-model="hForm.title" class="glass-input" required placeholder="如：周年庆小红书素材整理（未完成部分）" />
          </div>
          <div class="fg2">
            <div>
              <label class="fld">接收人 *</label>
              <select v-model="hForm.assignee_id" class="glass-input" required>
                <option value="" disabled>请选择系统人员</option>
                <option v-for="u in users" :key="u.id" :value="u.id" :disabled="u.id === auth.user?.id">
                  {{ u.name }}<template v-if="u.dept">（{{ u.dept.name }}）</template>
                </option>
              </select>
            </div>
          </div>
          <div>
            <label class="fld"><b style="color:var(--accent)">📌 我做到哪了（当前进展）</b></label>
            <textarea v-model="hForm.from_progress" class="glass-input ta" rows="2" placeholder="已完成/进行到哪一步，让接收人快速上手"></textarea>
          </div>
          <div>
            <label class="fld"><b style="color:var(--warn)">🔜 需要接收人继续做的事</b></label>
            <textarea v-model="hForm.todo" class="glass-input ta" rows="3" placeholder="还没做完的，需要下一个人继续处理的事项"></textarea>
          </div>
          <div class="fg2 bot">
            <div class="form-actions right">
              <button type="button" class="btn ghost" @click="cancelEditH">取消</button>
              <button type="submit" class="btn primary" :disabled="savingH">{{ savingH ? '发出中…' : '发出交接' }}</button>
            </div>
          </div>
        </form>

        <!-- 交接列表 -->
        <template v-else>
          <div v-if="handovers.length" class="h-list">
            <div v-for="h in handovers" :key="h.id" class="h-card" :class="'st-' + h.status">
              <div class="h-head">
                <span class="chip" :class="statusChip(h.status)">{{ statusLabel(h.status) }}</span>
                <span class="h-title">{{ h.title }}</span>
                <span class="dim">#{{ h.id }}</span>
                <span class="ops">
                  <button v-if="h.sender_id === auth.user?.id" class="del danger" @click="removeH(h)">删除</button>
                </span>
              </div>
              <div class="h-meta dim">
                {{ hScope === 'inbox' ? '来自' : '发给' }} <b>{{ hScope === 'inbox' ? h.sender_name : h.assignee_name }}</b>
                · {{ fmtTime(h.created_at) }}
                <template v-if="h.status === 'done'"> · 完成于 {{ fmtTime(h.completed_at) }}</template>
              </div>
              <div v-if="h.from_progress" class="h-prog">
                <span class="sec-label">当前进展</span>{{ h.from_progress }}
              </div>
              <div v-if="h.todo" class="h-todo">
                <span class="sec-label">待继续</span>{{ h.todo }}
              </div>
              <div v-if="h.status === 'in_progress'" class="h-note">
                <span class="sec-label">接收人备注</span><span class="dim">{{ h.note || '（处理中，暂无备注）' }}</span>
              </div>
              <div v-if="h.status === 'done' && h.note" class="h-note done-note">
                <span class="sec-label">完成说明</span>{{ h.note }}
              </div>
              <!-- 操作区：接收人可推进 -->
              <div v-if="h.assignee_id === auth.user?.id && h.status !== 'done'" class="h-actions">
                <template v-if="h.status === 'pending'">
                  <button class="btn sm ok" @click="setHStatus(h, 'in_progress')">👌 接手处理</button>
                </template>
                <template v-if="h.status === 'in_progress' || h.status === 'pending'">
                  <button class="btn sm primary" @click="promptDone(h)">✅ 标记完成</button>
                </template>
              </div>
              <div v-if="h.status === 'in_progress'" class="h-actor-edit">
                <button v-if="h.assignee_id === auth.user?.id" class="btn sm" @click="promptNote(h)">✍️ 更新进度备注</button>
              </div>
            </div>
          </div>
          <div v-else class="empty">
            {{ hScope === 'inbox' ? '还没有人交接给你' : '你还没发起过交接' }}，点右上「发起交接」把未完成的事交给下一个人
          </div>
        </template>
      </section>
    </template>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed, watch } from 'vue'
import * as api from '@/api'
import { useAuthStore } from '@/store/auth'
import { getCurrentInstance } from 'vue'

const { proxy } = getCurrentInstance()
const $msg = proxy?.$msg
const auth = useAuthStore()

const tab = ref('knowledge')

// ---------- 通用 ----------
function toast(t, type = 'success') { $msg ? $msg[type](t) : alert(t) }
function todayStr() {
  const d = new Date()
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}
function fmtTime(s) {
  if (!s) return ''
  // 服务端 time 格式 2026-09-07T10:00:00Z → 本地 YYYY-MM-DD HH:mm
  const d = new Date(s)
  if (isNaN(d)) return String(s).slice(0, 16)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
const scopeLabel = (s) => (s === 'private' ? '仅自己' : '同部门共享')
const scopeChip = (s) => (s === 'private' ? '' : 'accent')
function switchTab(t) { tab.value = t }

// ================= 迷你知识库 =================
const knowledge = ref([])
const categories = ref([])
const kQuery = ref('')
const kCategory = ref('')
const kMine = ref(false)
const editingK = ref(false)
const savingK = ref(false)
const viewK = ref(null)
const kForm = reactive({ id: 0, title: '', category: '', content: '', scope: 'department' })

let kTimer = null
async function loadK() {
  clearTimeout(kTimer)
  kTimer = setTimeout(async () => {
    try {
      const params = {}
      if (kQuery.value.trim()) params.kw = kQuery.value.trim()
      if (kCategory.value) params.category = kCategory.value
      if (kMine.value) params.mine = 1
      knowledge.value = await api.get('/workspace/knowledge', params)
    } catch (e) { toast(e.response?.data?.error || '加载失败', 'error') }
  }, kMine.value || kCategory.value ? 0 : 250)
}
async function loadCats() {
  try { categories.value = await api.get('/workspace/knowledge/categories') } catch {}
}
function openNewK() {
  Object.assign(kForm, { id: 0, title: '', category: '', content: '', scope: 'department' })
  editingK.value = true
}
function openEditK(k) {
  Object.assign(kForm, { id: k.id, title: k.title, category: k.category, content: k.content, scope: k.scope })
  editingK.value = true
}
function cancelEditK() { editingK.value = false }
async function saveK() {
  savingK.value = true
  try {
    if (kForm.id) { await api.put('/workspace/knowledge/' + kForm.id, kForm); toast('已保存') }
    else { await api.post('/workspace/knowledge', kForm); toast('已创建') }
    editingK.value = false
    await Promise.all([loadK(), loadCats()])
  } catch (e) { toast(e.response?.data?.error || '保存失败', 'error') }
  finally { savingK.value = false }
}
function openDetailK(k) {
  viewK.value = k
  loadKAtts(k.id)
}
function editFromView() {
  if (!viewK.value) return
  const k = viewK.value
  viewK.value = null
  openEditK(k)
}

// ---- 知识库附件 ----
const kAtts = ref([])
const uploading = ref(false)
const baseUrl = (import.meta.env?.BASE_URL || '') + 'api'

async function loadKAtts(id) {
  try { kAtts.value = await api.get('/workspace/knowledge/' + id + '/attachments') }
  catch (e) { toast(e.response?.data?.error || '附件加载失败', 'error') }
}
function canEditK(k) { return k && (auth.user?.id === k.owner_id || auth.isSuper) }
function canDelAtt(a) {
  if (!viewK.value) return false
  // 仅条目创建者 / 上传者 / 超管可删
  return auth.isSuper || viewK.value.owner_id === auth.user?.id || a.owner_id === auth.user?.id
}
// 图片缩略/预览 objectURL 缓存
const thumbCache = new Map()
function thumbUrl(a) {
  if (thumbCache.has(a.id)) return thumbCache.get(a.id)
  const tok = localStorage.getItem('sw_token')
  fetch(baseUrl + '/workspace/knowledge_attachments/' + a.id + '/download', { headers: tok ? { Authorization: 'Bearer ' + tok } : {} })
    .then((r) => r.ok ? r.blob() : Promise.reject())
    .then((b) => { const u = URL.createObjectURL(b); thumbCache.set(a.id, u); /* 触发视图刷新 */ if (viewK.value) viewK.value = { ...viewK.value } })
    .catch(() => {})
  return ''
}
function fetchAtt(a, isPreview) {
  const tok = localStorage.getItem('sw_token')
  return fetch(baseUrl + '/workspace/knowledge_attachments/' + a.id + '/download', {
    headers: tok ? { Authorization: 'Bearer ' + tok } : {}
  }).then((r) => { if (!r.ok) throw new Error('下载失败'); return r.blob() })
}
async function downloadAtt(a) {
  try {
    const blob = await fetchAtt(a, false)
    const u = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = u; link.download = a.file_name; document.body.appendChild(link); link.click()
    link.remove(); URL.revokeObjectURL(u)
  } catch (e) { toast(e.message || '下载失败', 'error') }
}
async function previewAtt(a) {
  try {
    const blob = await fetchAtt(a, true)
    const u = URL.createObjectURL(blob)
    const win = window.open(); if (win) { win.document.write('<iframe src="' + u + '" style="width:100%;height:100%;border:0"></iframe>'); win.document.title = a.file_name }
  } catch (e) { toast(e.message || '预览失败', 'error') }
}
async function uploadAtts(e) {
  const files = Array.from(e.target.files || [])
  e.target.value = ''
  if (!files.length || !viewK.value) return
  uploading.value = true
  try {
    for (const f of files) {
      await api.upload('/workspace/knowledge/' + viewK.value.id + '/attachments', f)
    }
    toast('已上传 ' + files.length + ' 个附件')
    loadKAtts(viewK.value.id)
  } catch (err) { toast(err.response?.data?.error || '上传失败', 'error') }
  finally { uploading.value = false }
}
async function removeAtt(a) {
  if (!confirm('删除附件「' + a.file_name + '」？')) return
  try { await api.del('/workspace/knowledge_attachments/' + a.id); toast('已删除'); loadKAtts(viewK.value.id) }
  catch (e) { toast(e.response?.data?.error || '删除失败', 'error') }
}
function fmtSize(n) {
  if (!n && n !== 0) return ''
  const u = ['B', 'KB', 'MB', 'GB']
  let i = 0, v = n
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return (i === 0 ? v : v.toFixed(1)) + ' ' + u[i]
}
function fileIcon(name) {
  const ext = (name.split('.').pop() || '').toLowerCase()
  const imgs = ['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp']
  if (imgs.includes(ext)) return '🖼'
  if (['pdf'].includes(ext)) return '📄'
  if (['doc', 'docx'].includes(ext)) return '📝'
  if (['xls', 'xlsx', 'csv'].includes(ext)) return '📊'
  if (['ppt', 'pptx'].includes(ext)) return '📽'
  if (['zip', 'rar', '7z', 'tar', 'gz'].includes(ext)) return '🗜'
  if (['mp4', 'mov', 'avi', 'mkv'].includes(ext)) return '🎬'
  return '📎'
}

// ---- 导出 ----
function exportK() {
  const params = new URLSearchParams()
  if (kQuery.value.trim()) params.set('kw', kQuery.value.trim())
  if (kCategory.value) params.set('category', kCategory.value)
  const q = params.toString()
  const tok = localStorage.getItem('sw_token')
  fetch(baseUrl + '/workspace/knowledge/export' + (q ? '?' + q : ''), { headers: tok ? { Authorization: 'Bearer ' + tok } : {} })
    .then((r) => { if (!r.ok) throw new Error(); return r.blob() })
    .then((blob) => { const u = URL.createObjectURL(blob); const a = document.createElement('a'); a.href = u; a.download = '知识库导出.txt'; a.click(); a.remove(); URL.revokeObjectURL(u) })
    .catch(() => toast('导出失败', 'error'))
}
function exportBundle() {
  const tok = localStorage.getItem('sw_token')
  const a = document.createElement('a')
  a.href = baseUrl + '/workspace/export/bundle'
  a.download = '' // 交给服务端 Content-Disposition
  if (tok) {
    // 需要鉴权 header，走 fetch blob
    fetch(baseUrl + '/workspace/export/bundle', { headers: { Authorization: 'Bearer ' + tok } })
      .then((r) => { if (!r.ok) throw new Error(); const cd = r.headers.get('Content-Disposition') || ''; const m = /filename\*=UTF-8''([^;]+)/.exec(cd); const fn = m ? decodeURIComponent(m[1]) : ('工作台数据_' + Date.now() + '.zip'); return r.blob().then((b) => ({ b, fn })) })
      .then(({ b, fn }) => { const u = URL.createObjectURL(b); const x = document.createElement('a'); x.href = u; x.download = fn; x.click(); x.remove(); URL.revokeObjectURL(u); toast('打包已生成') })
      .catch(() => toast('导出失败', 'error'))
  } else {
    a.href = '/login'
    a.click()
  }
}
async function removeK(k) {
  if (!confirm(`删除知识条目「${k.title}」？此操作不可恢复。`)) return
  try { await api.del('/workspace/knowledge/' + k.id); toast('已删除'); await Promise.all([loadK(), loadCats()]) }
  catch (e) { toast(e.response?.data?.error || '删除失败', 'error') }
}

// ================= 工作日志 =================
const logs = ref([])
const viewDate = ref(todayStr())
const logScope = ref('mine')
const editingLog = ref(false)
const savingLog = ref(false)
const logForm = reactive({ id: 0, log_date: '', title: '', done: '', pending: '', scope: 'private' })

async function loadLogs() {
  try {
    const params = { date: viewDate.value }
    if (logScope.value === 'dept') { /* 部门共享：mine 缺省看全部可见(共享) */ }
    else params.mine = 1
    logs.value = await api.get('/workspace/logs', params)
  } catch (e) { toast(e.response?.data?.error || '加载失败', 'error') }
}
function setLogScope(s) { logScope.value = s; loadLogs() }
function openNewLog() {
  Object.assign(logForm, { id: 0, log_date: viewDate.value, title: '', done: '', pending: '', scope: 'private' })
  editingLog.value = true
}
function openEditLog(l) {
  Object.assign(logForm, { id: l.id, log_date: l.log_date, title: l.title, done: l.done, pending: l.pending, scope: l.scope })
  editingLog.value = true
}
function cancelEditLog() { editingLog.value = false }
async function saveLog() {
  savingLog.value = true
  try {
    if (logForm.id) { await api.put('/workspace/logs/' + logForm.id, logForm); toast('已保存') }
    else { await api.post('/workspace/logs', logForm); toast('已记录') }
    editingLog.value = false
    loadLogs()
  } catch (e) { toast(e.response?.data?.error || '保存失败', 'error') }
  finally { savingLog.value = false }
}
async function removeLog(l) {
  if (!confirm('删除这篇日志？')) return
  try { await api.del('/workspace/logs/' + l.id); toast('已删除'); loadLogs() }
  catch (e) { toast(e.response?.data?.error || '删除失败', 'error') }
}

// ================= 交接接力 =================
const users = ref([])
const handovers = ref([])
const hScope = ref('inbox')
const editingH = ref(false)
const savingH = ref(false)
const hForm = reactive({ title: '', assignee_id: '', from_progress: '', todo: '' })
const inboxUnread = computed(() => handovers.value.filter((h) => h.status === 'pending').length)

const statusLabel = (s) => ({ pending: '待接手', in_progress: '处理中', done: '已完成' }[s] || s)
const statusChip = (s) => ({ pending: 'warn', in_progress: 'accent', done: 'ok' }[s] || '')

async function loadUsers() {
  try {
    const all = await api.get('/users')
    users.value = all.filter((u) => !u.frozen) // 冻结的不参与交接接收
  } catch {}
}
async function loadHandovers() {
  try { handovers.value = await api.get('/workspace/handovers', { role: hScope.value, status: 'all' }) }
  catch (e) { toast(e.response?.data?.error || '加载失败', 'error') }
}
function setHScope(s) { hScope.value = s; loadHandovers() }
function openNewH() {
  Object.assign(hForm, { title: '', assignee_id: '', from_progress: '', todo: '' })
  editingH.value = true
  loadUsers()
}
function cancelEditH() { editingH.value = false }
async function saveH() {
  savingH.value = true
  try {
    await api.post('/workspace/handovers', hForm)
    toast('交接已发出，接收人会收到通知')
    editingH.value = false
    loadHandovers()
  } catch (e) { toast(e.response?.data?.error || '发出失败', 'error') }
  finally { savingH.value = false }
}
async function setHStatus(h, st) {
  try { await api.post('/workspace/handovers/' + h.id + '/status', { status: st }); toast('已更新'); loadHandovers() }
  catch (e) { toast(e.response?.data?.error || '操作失败', 'error') }
}
async function promptDone(h) {
  const note = prompt(`为交接「${h.title}」填写完成说明（可留空）：`)
  if (note === null) return
  try { await api.post('/workspace/handovers/' + h.id + '/status', { status: 'done', note: note || '' }); toast('已标记完成'); loadHandovers() }
  catch (e) { toast(e.response?.data?.error || '操作失败', 'error') }
}
async function promptNote(h) {
  const note = prompt('更新当前处理进展备注：', h.note || '')
  if (note === null) return
  try { await api.post('/workspace/handovers/' + h.id + '/status', { status: 'in_progress', note }); toast('已更新'); loadHandovers() }
  catch (e) { toast(e.response?.data?.error || '操作失败', 'error') }
}
async function removeH(h) {
  if (!confirm('删除这条交接？')) return
  try { await api.del('/workspace/handovers/' + h.id); toast('已删除'); loadHandovers() }
  catch (e) { toast(e.response?.data?.error || '删除失败', 'error') }
}

onMounted(() => {
  loadK()
  loadCats()
  loadLogs()
  loadHandovers()
  if (auth.isSuper || auth.canManage) loadUsers() // 提前加载，便于发起交接
})

watch(viewDate, loadLogs)
watch(tab, (t) => { if (t === 'handover') loadHandovers(); if (t === 'logs') loadLogs(); if (t === 'knowledge') { loadK(); loadCats() } })
</script>

<style scoped>
.page-title { font-size: 20px; font-weight: 700; margin: 0; }
.ws-head { display: flex; align-items: flex-end; justify-content: space-between; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
.ws-tool { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.export-bundle { white-space: nowrap; font-size: 12.5px; padding: 8px 14px; color: var(--accent); border-color: rgba(79,70,229,0.3); }
.export-bundle:hover { background: var(--accent-soft); }
.panel { padding: 18px; border-radius: 16px; background: var(--glass); border: 1px solid var(--glass-border); margin-bottom: 18px; }
.panel-head { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; margin-bottom: 12px; }
.head-actions { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.section-title { font-size: 15px; font-weight: 600; margin: 0; }
.section-sub { font-size: 12px; color: var(--text-faint); font-weight: 400; }

/* 顶部 Tab */
.tabs { display: inline-flex; background: var(--overlay); border: 1px solid var(--glass-border); border-radius: 12px; padding: 3px; gap: 2px; }
.tab { border: none; background: transparent; color: var(--text-dim); padding: 8px 16px; border-radius: 9px; cursor: pointer; font-size: 13px; white-space: nowrap; }
.tab.active { background: var(--accent-soft); color: var(--accent); font-weight: 600; }
.handover-badge { font-style: normal; background: var(--danger); color: #fff; border-radius: 999px; font-size: 11px; padding: 0 6px; margin-left: 3px; }

/* 通用表单 */
.edit-form { border: 1px dashed var(--glass-border-strong); border-radius: 14px; padding: 16px; margin: 4px 0 16px; background: var(--overlay); display: flex; flex-direction: column; gap: 12px; }
.fld { display: block; font-size: 12px; color: var(--text-dim); margin-bottom: 6px; }
.glass-input { width: 100%; padding: 9px 12px; border-radius: 11px; background: var(--bg-1); border: 1px solid var(--glass-border); color: var(--text); font-size: 13.5px; outline: none; }
.glass-input:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
textarea.ta { resize: vertical; line-height: 1.6; }
.fg2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.fg2.bot { align-items: end; }
.btn { padding: 8px 16px; border-radius: 11px; border: 1px solid var(--glass-border); background: var(--overlay-2); color: var(--text); cursor: pointer; font-size: 13px; }
.btn.primary { background: var(--brand-grad, var(--accent)); color: #fff; border: none; font-weight: 600; }
.btn.ghost { background: transparent; }
.btn.sm { padding: 6px 12px; font-size: 12px; }
.btn.ok { background: rgba(5,150,105,0.12); color: var(--ok); border-color: rgba(5,150,105,0.35); }
.btn:disabled { opacity: 0.6; cursor: not-allowed; }
.form-actions { display: flex; gap: 8px; justify-content: flex-end; }
.form-actions.right { justify-content: flex-end; }
.chip { display: inline-block; font-size: 11px; padding: 2px 9px; border-radius: 999px; background: var(--overlay-2); color: var(--text-dim); }
.chip.accent { background: var(--accent-soft); border-color: rgba(79,70,229,0.3); color: var(--accent); }
.dim { color: var(--text-faint); font-weight: 400; }
.empty { text-align: center; color: var(--text-faint); padding: 36px 0; font-size: 13px; }
.ops { display: inline-flex; gap: 6px; }
.del { padding: 4px 10px; border-radius: 8px; border: 1px solid var(--glass-border); background: transparent; color: var(--text-faint); cursor: pointer; font-size: 12px; }
.del:hover { color: var(--text); border-color: var(--glass-border-strong); }
.del.danger:hover { color: var(--danger); border-color: rgba(225,29,72,0.4); }
.scope-switch select { width: auto; min-width: 150px; }
.chk.mine { display: inline-flex; align-items: center; gap: 5px; font-size: 12.5px; color: var(--text-dim); cursor: pointer; white-space: nowrap; }
.chk.mine input { width: 15px; height: 15px; }

/* 知识库 */
.filter-bar { display: flex; gap: 10px; flex-wrap: wrap; align-items: center; margin-bottom: 14px; }
.filter-bar .search { flex: 1; min-width: 200px; }
.filter-bar select { width: auto; }
.k-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(250px, 1fr)); gap: 12px; }
.k-card { border: 1px solid var(--glass-border); border-radius: 14px; padding: 13px 14px; background: var(--bg-1); display: flex; flex-direction: column; gap: 8px; transition: transform .12s ease, box-shadow .12s ease; cursor: pointer; }
.k-card:hover { transform: translateY(-2px); box-shadow: 0 6px 18px rgba(15,23,42,0.08); }
.k-card.mine { border-left: 3px solid var(--accent); }
.k-top { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.k-owner { margin-left: auto; font-size: 11.5px; color: var(--text-faint); }
.k-title { font-size: 14px; font-weight: 700; color: var(--text); line-height: 1.4; }
.k-preview { font-size: 12.5px; color: var(--text-dim); line-height: 1.6; white-space: pre-wrap; word-break: break-word; max-height: 4.2em; overflow: hidden; }
.k-foot { display: flex; align-items: center; justify-content: space-between; margin-top: auto; font-size: 11.5px; }
.k-foot .ops { display: inline-flex; gap: 6px; }

/* 日志 */
.log-view-toggle, .sub-tabs { display: inline-flex; border: 1px solid var(--glass-border); border-radius: 10px; overflow: hidden; }
.seg { padding: 6px 13px; font-size: 12.5px; border: none; background: transparent; color: var(--text-dim); cursor: pointer; }
.seg + .seg { border-left: 1px solid var(--glass-border); }
.seg.on { background: var(--accent-soft); color: var(--accent); font-weight: 600; }
.date-input { width: auto; }
.log-list { display: flex; flex-direction: column; gap: 12px; }
.log-card { border: 1px solid var(--glass-border); border-radius: 14px; padding: 13px 15px; background: var(--bg-1); }
.log-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.log-time { font-size: 12px; color: var(--text-faint); font-weight: 600; }
.log-title { font-size: 14px; font-weight: 700; color: var(--text); }
.log-head .owner { margin-left: auto; }
.log-sec { font-size: 13px; line-height: 1.7; color: var(--text); margin-top: 10px; white-space: pre-wrap; }
.log-sec.done { border-left: 3px solid var(--ok); padding-left: 10px; }
.log-sec.pend { border-left: 3px solid var(--warn); padding-left: 10px; }
.sec-label { display: inline-block; font-size: 11px; color: var(--text-faint); margin-right: 8px; font-weight: 600; letter-spacing: .3px; }

/* 交接 */
.h-list { display: flex; flex-direction: column; gap: 12px; }
.h-card { border: 1px solid var(--glass-border); border-radius: 14px; padding: 14px 16px; background: var(--bg-1); }
.h-card.st-done { background: var(--overlay); }
.h-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.h-title { font-size: 14px; font-weight: 700; color: var(--text); flex: 1; min-width: 160px; }
.h-head .ops { margin-left: auto; }
.h-meta { font-size: 12px; margin-top: 7px; }
.h-meta b { color: var(--text-dim); }
.h-prog, .h-todo, .h-note { font-size: 13px; line-height: 1.7; margin-top: 9px; white-space: pre-wrap; border-radius: 10px; padding: 8px 11px; background: var(--overlay); }
.h-prog { border-left: 3px solid var(--accent); }
.h-todo { border-left: 3px solid var(--warn); }
.h-actions, .h-actor-edit { margin-top: 11px; display: flex; gap: 8px; flex-wrap: wrap; }

@media (max-width: 820px) {
  .fg2 { grid-template-columns: 1fr; }
  .ws-head { flex-direction: column; align-items: flex-start; }
  .tabs { width: 100%; }
  .tab { flex: 1; padding: 10px 6px; text-align: center; }
  .k-grid { grid-template-columns: 1fr; }
  .panel-head { flex-direction: column; align-items: flex-start; }
  .scope-switch select, .date-input { width: 100%; }
}

/* 知识详情弹窗 */
.modal-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.5); display: flex; align-items: center; justify-content: center; z-index: 100; padding: 20px; }
.modal { background: var(--bg-1); border: 1px solid var(--glass-border); border-radius: 16px; width: min(560px, 100%); max-height: 82vh; display: flex; flex-direction: column; overflow: hidden; }
.modal-head { display: flex; align-items: center; gap: 6px; padding: 14px 16px 0; }
.modal-close { margin-left: auto; border: none; background: transparent; color: var(--text-faint); font-size: 16px; cursor: pointer; }
.modal-close:hover { color: var(--text); }
.modal-title { font-size: 16px; font-weight: 700; margin: 10px 16px 4px; }
.modal-meta { font-size: 12px; margin: 0 16px 12px; }
.modal-scroll { flex: 1; overflow-y: auto; padding: 0 16px; }
.modal-body { font-size: 13.5px; line-height: 1.8; color: var(--text); white-space: pre-wrap; word-break: break-word; }
.modal-foot { display: flex; justify-content: flex-end; gap: 8px; padding: 12px 16px; border-top: 1px solid var(--hairline); margin-top: 12px; flex-shrink: 0; }

/* 附件区 */
.modal-attach { margin-top: 14px; border-top: 1px solid var(--hairline); padding-top: 12px; }
.att-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
.att-title { font-size: 13px; font-weight: 600; color: var(--text); }
.att-upload { display: inline-flex; align-items: center; gap: 8px; }
.att-upload .btn { margin: 0; display: inline-flex; align-items: center; gap: 5px; }
.up-txt { font-size: 12px; }
.att-hint { font-size: 11.5px; margin: 6px 0 10px; }
.att-list { display: flex; flex-direction: column; gap: 7px; max-height: 240px; overflow-y: auto; padding-right: 2px; }
.att-item { display: flex; align-items: center; gap: 10px; border: 1px solid var(--glass-border); border-radius: 10px; padding: 7px 10px; background: var(--overlay); }
.att-thumb { width: 40px; height: 40px; object-fit: cover; border-radius: 7px; cursor: pointer; flex-shrink: 0; background: var(--overlay-2); }
.att-ico { width: 40px; height: 40px; display: flex; align-items: center; justify-content: center; font-size: 20px; flex-shrink: 0; background: var(--overlay-2); border-radius: 7px; cursor: pointer; }
.att-name { flex: 1; min-width: 0; font-size: 12.5px; color: var(--text); cursor: pointer; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.att-name:hover { color: var(--accent); text-decoration: underline; }
.att-size { font-size: 11px; flex-shrink: 0; }
.att-item .del { flex-shrink: 0; }
.att-empty { padding: 18px 0; }

@media (max-width: 640px) {
  .modal { width: 100%; max-height: 90vh; }
  .modal-scroll { padding: 0 14px; }
  .modal-title { margin-left: 14px; }
  .modal-meta { margin-left: 14px; margin-right: 14px; }
  .modal-head { padding-left: 14px; }
  .modal-foot { padding: 12px 14px calc(12px + env(safe-area-inset-bottom)); }
  .att-item { flex-wrap: wrap; }
  .att-name { min-width: 120px; }
  .att-list { max-height: 200px; }
}
</style>
