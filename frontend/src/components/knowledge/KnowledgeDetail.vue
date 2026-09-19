<template>
  <!--
    KnowledgeDetail.vue —— 知识库三栏布局 · 右栏（详情 / 编辑）
    v0.28.0 新增。设计要点：
      · 「直接编辑」：默认就是可编辑的富文本，不再是「查看 → 点编辑 → 再进表单」的三跳
      · 「所见即所得」：渲染态与编辑态共用 .kb-prose 排版（styles/editor.css），
        所以详情页看到的字号/颜色/居中/图片对齐，与编辑时逐像素一致
      · 「Word 习惯」：工具条 40 项常驻顶部（加粗/字号/行距/缩进/表格/图片缩放/上下标…）
      · 有未保存改动时离开会拦截，避免误丢
  -->
  <section class="kb-detail-pane">
    <!-- 空态：既没选中条目、也不在新建状态 -->
    <div v-if="!entry && !editing" class="kb-detail-empty">
      <div class="de-ico">📖</div>
      <div class="de-txt">
        <p>从左侧选一条知识，或直接新建</p>
        <p class="dim">支持像 Word 一样编辑：字号 / 颜色 / 居中 / 行距 / 缩进 / 表格 / 图片缩放 / 上下标</p>
      </div>
      <button class="btn primary sm" @click="$emit('create')">+ 新建条目</button>
    </div>

    <template v-else>
      <!-- 头部：标题 + 元信息 + 操作 -->
      <header class="kb-detail-head">
        <div class="dh-top">
          <div class="dh-title-wrap">
            <div class="dh-title-row">
              <input
                v-if="editing"
                v-model="draft.title"
                class="dh-title-input"
                placeholder="请输入标题"
                aria-required="true"
              />
              <span v-if="editing" class="dh-req" title="必填">*</span>
              <h2 v-else class="dh-title" @dblclick="canEdit && startEdit()">{{ view.title }}</h2>
            </div>
            <!-- v0.36.2：作者 / 更新于 / 浏览 / 协作者 移到标题下空白，常驻可见 -->
            <div class="dh-sub dim">
              {{ view.owner_name === currentUserName ? '我' : view.owner_name }} · 更新于 {{ fmtTime(view.updated_at || view.created_at) }}
              <template v-if="view.view_count"> · 👁 {{ view.view_count }}</template>
              <template v-if="editorsText(entry)"> · ✍ {{ editorsText(entry) }}</template>
            </div>
          </div>

          <div class="dh-ops">
            <button class="icon-btn" :class="{ on: view.starred }" :title="view.starred ? '取消收藏' : '收藏'" @click="$emit('star', view)">{{ view.starred ? '★' : '☆' }}</button>
            <button
              v-if="view.owner_id === currentUserId || isSuper"
              class="icon-btn"
              :class="{ on: view.pinned }"
              :title="view.pinned ? '取消置顶' : '置顶'"
              @click="$emit('pin', view)"
            >{{ view.pinned ? '📌' : '📍' }}</button>
            <button v-if="!isDiagram" class="icon-btn" title="打印 / 另存为 PDF" @click="printEntry">🖨</button>
            <!-- 图形条目（思维导图 / 流程图）导出：PNG / SVG / PDF / 源文件 -->
            <div v-if="isDiagram" class="dh-export" @click.stop>
              <button class="icon-btn" title="导出图片 / PDF / 源文件" @click="exportOpen = !exportOpen">⬇</button>
              <div v-if="exportOpen" class="dh-export-menu">
                <button :disabled="exporting" @click="exportAs('png')">导出 PNG 图片</button>
                <button :disabled="exporting" @click="exportAs('svg')">导出 SVG 矢量图</button>
                <button :disabled="exporting" @click="exportAs('pdf')">导出 PDF 文档</button>
                <button :disabled="exporting" @click="exportAs('json')">导出源文件（JSON）</button>
                <div class="dhe-tip">{{ exporting ? '正在生成…' : 'JSON 可备份，也可再导入编辑' }}</div>
              </div>
            </div>
            <button v-if="!editing && canEdit" class="btn primary sm" @click="startEdit">✎ 编辑</button>
          </div>
        </div>

        <!-- v0.36.3：元信息仅阅读态展示；编辑态与下方表单（分类/状态/标签）重复，隐藏以压缩头部 -->
        <div v-if="!editing" class="dh-meta dim">
          <span class="ki-chip" :class="scopeChip(view.scope)">{{ scopeLabel(view.scope) }}</span>
          <span v-if="view.category" class="ki-chip accent">{{ view.category }}</span>
          <span v-if="isDiagram" class="ki-chip accent">{{ kindLabelOf }}</span>
          <span v-if="view.status === 'draft'" class="ki-chip warn">草稿</span>
          <span v-for="t in parseTags(view.tags)" :key="t" class="dh-tag">#{{ t }}</span>
        </div>

        <!-- 编辑态：基础字段 + 内容类型（v0.36.0 压成一行，空间让给画布） -->
        <div v-if="editing" class="dh-form dh-form-compact">
          <div ref="rootKind" class="dhf-fld chip-fld">
            <span class="dhf-label">内容</span>
            <button
              type="button"
              class="kind-chip"
              :class="curKind"
              :disabled="!!draft.id"
              @click="onKindChipClick"
            >
              <span class="kind-chip-ico">{{ kindMeta.icon }}</span>
              <span class="kind-chip-txt">{{ kindMeta.label }}</span>
              <span v-if="!draft.id" class="kind-chip-caret">▾</span>
            </button>
            <div v-if="kindMenuOpen" class="kind-menu" @click.stop>
              <button
                v-for="k in KIND_OPTIONS"
                :key="k.value"
                type="button"
                class="kind-menu-item"
                :class="{ on: curKind === k.value }"
                @click="pickKind(k.value)"
              >
                <span class="kind-ico">{{ k.icon }}</span>
                <span class="kind-txt">{{ k.label }}</span>
                <span class="kind-sub">{{ k.desc }}</span>
              </button>
            </div>
          </div>
          <label class="dhf-fld">
            <span>分类</span>
            <input v-model="draft.category" class="glass-input" list="kbCatList" placeholder="如：活动SOP / 检查流程 / 话术" />
            <datalist id="kbCatList">
              <option v-for="c in categories" :key="c" :value="c" />
            </datalist>
          </label>
          <label class="dhf-fld">
            <span>状态</span>
            <select v-model="draft.status" class="glass-input">
              <option value="published">已发布</option>
              <option value="draft">草稿（仅自己可见）</option>
            </select>
          </label>
          <label class="dhf-fld">
            <span>所属父级</span>
            <ParentTreePicker
              v-model="draft.parent_id"
              :items="parentCandidates"
              :exclude-id="draft.id"
            />
          </label>
          <label class="dhf-fld grow">
            <span>标签</span>
            <div class="tag-edit">
              <span v-for="(t, i) in draft.tags" :key="t" class="chip pick-chip">
                {{ t }}<button type="button" class="x" @click="draft.tags.splice(i, 1)" aria-label="移除">×</button>
              </span>
              <input
                v-model="tagDraft"
                class="glass-input tag-input"
                placeholder="回车添加"
                @keydown.enter.prevent="addTag"
              />
            </div>
          </label>
        </div>
      </header>

      <!-- 正文：按内容类型渲染（doc=富文本 / mind=思维导图 / flow=流程图） -->
      <div class="kb-detail-body" :class="{ 'kb-body-flush': isDiagram, 'kb-body-canvas': editing && isDiagram }">
        <MindMapEditor
          v-if="curKind === 'mind'"
          ref="diagramRef"
          :model-value="editing ? draft.content : (view.content || '')"
          :readonly="!editing"
          @update:model-value="onContent"
          @dirty="onDiagramDirty"
        />
        <FlowEditor
          v-else-if="curKind === 'flow'"
          ref="diagramRef"
          :model-value="editing ? draft.content : (view.content || '')"
          :readonly="!editing"
          @update:model-value="onContent"
          @dirty="onDiagramDirty"
        />
        <template v-else>
          <RichTextEditor
            v-if="editing"
            :model-value="draft.content"
            :entry-id="draft.id || 0"
            placeholder="像 Word 一样直接编辑：可粘贴截图、插入图片 / 文档 / 超链接 / 表格 / 代码块"
            @update:model-value="onContent"
            @image-upload-error="$emit('content-error', $event)"
          />
          <div
            v-else
            class="kb-prose kb-read"
            v-html="safeHtml(view.content) || '<span class=\'dim\'>（暂无内容，点右上「编辑」开始写）</span>'"
          ></div>
        </template>
      </div>

      <!-- 编辑态：附件 / 协作 / 可见范围（v0.36.0 折叠为一行，图形条目默认收起） -->
      <details v-if="editing" class="dh-fold" :open="!isDiagram">
        <summary class="dh-fold-bar">
          <span class="dh-fold-tt">附件 · 协作 · 可见范围</span>
          <span class="dh-fold-cnt dim">{{ attCount ? `已选 ${attCount} 个附件` : '点开展开设置' }}</span>
          <span class="dh-fold-arrow">▾</span>
        </summary>
        <div class="dh-form dh-form-lower">
        <!-- 附件：编辑时即可上传到中转缓存，保存后自动转正（单文件 ≤ 100MB） -->
        <div class="dhf-attach">
          <span class="dhf-label">附件</span>
          <p class="att-hint dim">编辑时可上传，保存前文件缓存，保存后自动生成（单文件 ≤ 100MB）</p>
          <div class="att-list">
            <div v-for="a in pendingAtts" :key="'p' + a.tempId" class="att-item">
              <span class="att-ico">{{ fileIcon(a.fileName) }}</span>
              <span class="att-name">{{ a.fileName }}</span>
              <span class="att-size dim">{{ fmtSize(a.size) }}</span>
              <button class="del danger" @click="$emit('remove-pending-att', a)">删除</button>
            </div>
            <template v-if="draft.id">
              <div v-for="a in attachments" :key="'a' + a.id" class="att-item">
                <span class="att-ico">{{ fileIcon(a.file_name) }}</span>
                <span class="att-name">{{ a.file_name }}</span>
                <span class="att-size dim">{{ fmtSize(a.size) }}</span>
                <button class="del danger" @click="$emit('remove-att', a)">删除</button>
              </div>
            </template>
            <div v-if="!pendingAtts.length && !(draft.id && attachments.length)" class="empty att-empty">还没有附件，可上传截图 / 文档等补充资料</div>
          </div>
          <div class="att-upload-row">
            <label v-if="canUpload" class="btn sm">+ 上传文件
              <input type="file" multiple :disabled="uploading" @change="$emit('upload', $event)" hidden />
            </label>
            <span v-if="uploading" class="dim up-txt">上传中…</span>
          </div>
        </div>

        <!-- 协作者（仅创建者 / 超管可调） -->
        <div v-if="canSetEditors" class="dhf-collab">
          <span class="dhf-label">协作（可查看并编辑本文档的同事）</span>
          <div class="tag-edit">
            <span v-for="(nm, i) in collabNames" :key="'c' + i" class="chip pick-chip">
              {{ nm }}<button type="button" class="x" @click="draft.editor_ids.splice(i, 1)" aria-label="移除">×</button>
            </span>
            <button type="button" class="btn ghost sm" @click="showCollab = !showCollab">
              {{ showCollab ? '收起名单' : '+ 添加协作者' }}
            </button>
          </div>
          <div v-if="showCollab" class="picker-list">
            <label v-for="u in collabCandidates" :key="u.id" class="picker-row">
              <input type="checkbox" :checked="draft.editor_ids.includes(u.id)" @change="toggleCollab(u.id)" />
              <span class="picker-name">{{ u.name }}<span v-if="u.dept" class="dim">（{{ u.dept.name }}）</span></span>
            </label>
            <div v-if="!collabCandidates.length" class="dim picker-empty">没有可选的同事</div>
          </div>
        </div>

        <div class="dhf-row">
          <label class="dhf-fld wide">
            <span>可见范围</span>
            <select v-model="draft.scope" class="glass-input">
              <option value="public" v-if="isSuper">全公司可见</option>
              <option value="department">同部门共享</option>
              <option value="private">仅自己可见</option>
            </select>
          </label>
        </div>
        </div>
      </details>

      <!-- 编辑态底栏 -->
      <footer v-if="editing" class="kb-detail-foot">
        <span class="df-tip dim">
          <template v-if="dirty">● 有未保存改动</template>
          <template v-else>改动会自动同步到正文</template>
        </span>
        <div class="df-btns">
          <button class="btn ghost" @click="cancelEdit">取消</button>
          <button class="btn primary" :disabled="saving || !draft.title.trim()" @click="submit">
            {{ saving ? '创建中…' : (draft.id ? '保存修改' : '创建') }}
          </button>
        </div>
      </footer>

      <!-- 阅读态：子标签（评论 / 版本 / 双链 / 附件 / 变更记录） -->
      <div v-else class="kb-detail-tabs">
        <div class="k-tabs">
          <button class="k-tab" :class="{ on: sub === 'comments' }" @click="go('comments')">评论 ({{ comments.length }})</button>
          <button class="k-tab" :class="{ on: sub === 'versions' }" @click="go('versions')">版本 ({{ versions.length }})</button>
          <button class="k-tab" :class="{ on: sub === 'links' }" @click="go('links')">双向链接</button>
          <button class="k-tab" :class="{ on: sub === 'files' }" @click="go('files')">附件 ({{ attachments.length }})</button>
          <button class="k-tab" :class="{ on: sub === 'history' }" @click="go('history')">变更记录</button>
        </div>

        <div class="k-tab-body">
          <!-- 评论 -->
          <div v-if="sub === 'comments'" class="k-comments">
            <div v-if="!comments.length" class="dim">还没有评论，来写第一条吧（可用 @姓名 提醒同事）</div>
            <div v-for="c in topComments" :key="c.id" class="comment">
              <div class="comment-head"><b>{{ c.user_name }}</b><span class="dim">{{ fmtTime(c.created_at) }}</span></div>
              <div class="comment-body" v-html="safeHtml(c.content)"></div>
              <div class="comment-ops">
                <button class="del" @click="$emit('reply', c.id, c.user_name)">回复</button>
                <button v-if="c.user_id === currentUserId || isSuper" class="del danger" @click="$emit('del-comment', c.id)">删除</button>
              </div>
              <div v-for="r in repliesOf(c.id)" :key="r.id" class="comment reply">
                <div class="comment-head"><b>{{ r.user_name }}</b><span class="dim">{{ fmtTime(r.created_at) }}</span></div>
                <div class="comment-body" v-html="safeHtml(r.content)"></div>
                <div class="comment-ops">
                  <button class="del danger" @click="$emit('del-comment', r.id)">删除</button>
                </div>
              </div>
            </div>
            <div class="comment-editor">
              <textarea
                :value="commentText"
                class="glass-input ta"
                rows="2"
                :placeholder="replyTo ? '回复中…' : '发表评论，@姓名 可提醒对方'"
                @input="$emit('update:commentText', $event.target.value)"
              ></textarea>
              <div class="comment-send">
                <button v-if="replyTo" class="btn ghost sm" @click="$emit('cancel-reply')">取消回复</button>
                <button class="btn primary sm" :disabled="commentBusy || !commentText.trim()" @click="$emit('send-comment')">
                  {{ commentBusy ? '发送中…' : '发表评论' }}
                </button>
              </div>
            </div>
          </div>

          <!-- 版本 -->
          <div v-else-if="sub === 'versions'" class="k-versions">
            <div v-if="!versions.length" class="dim">暂无历史版本（每次保存会自动快照）</div>
            <div v-for="v in versions" :key="v.id" class="version-item">
              <div class="version-head">
                <span class="chip accent">v{{ v.version }}</span><b>{{ v.operator_name }}</b>
                <span class="dim">{{ fmtTime(v.created_at) }}</span>
              </div>
              <div class="version-meta dim">{{ v.title }} · {{ v.category || '未分类' }}</div>
              <button v-if="canEdit" class="btn ghost sm" @click="$emit('restore', v.id)">回滚到此版本</button>
            </div>
          </div>

          <!-- 双链 -->
          <div v-else-if="sub === 'links'" class="k-links">
            <div class="link-group">
              <div class="link-h">↗ 本条目引用的（出链）</div>
              <div v-if="!outlinks.length" class="dim">正文中使用 <code>[[标题]]</code> 语法即可建立引用</div>
              <div v-for="e in outlinks" :key="'o' + e.id" class="link-item" @click="$emit('open-ref', e)">{{ e.title }}</div>
            </div>
            <div class="link-group">
              <div class="link-h">↘ 引用本条目的（反向链接）</div>
              <div v-if="!backlinks.length" class="dim">还没有其它条目引用本条目</div>
              <div v-for="e in backlinks" :key="'b' + e.id" class="link-item" @click="$emit('open-ref', e)">{{ e.title }}</div>
            </div>
          </div>

          <!-- 附件 -->
          <div v-else-if="sub === 'files'" class="modal-attach">
            <div class="att-head">
              <span class="att-title">附件（{{ attachments.length }}）</span>
              <div v-if="canEdit" class="att-upload">
                <span v-if="uploading" class="dim up-txt">上传中…</span>
                <label class="btn sm">+ 上传文件
                  <input type="file" multiple :disabled="uploading" @change="$emit('upload', $event)" hidden />
                </label>
              </div>
            </div>
            <p v-if="canEdit" class="att-hint dim">支持任意文件类型（图片可预览、PDF 可在线阅读），单文件 ≤ 100MB</p>
            <div v-if="attachments.length" class="att-list">
              <div v-for="a in attachments" :key="a.id" class="att-item">
                <img
                  v-if="a.mime && a.mime.startsWith('image/')"
                  :src="thumbOf(a)" class="att-thumb" :alt="a.file_name"
                  title="点击预览" @click="$emit('preview-att', a)"
                />
                <span v-else-if="isPdf(a)" class="att-ico pdf" :title="'在线阅读 ' + a.file_name" @click="$emit('preview-att', a)">{{ fileIcon(a.file_name) }}</span>
                <span v-else class="att-ico" :title="'下载 ' + a.file_name" @click="$emit('download-att', a)">{{ fileIcon(a.file_name) }}</span>
                <span class="att-name" :title="'下载 ' + a.file_name" @click="$emit('download-att', a)">{{ a.file_name }}</span>
                <button v-if="isPdf(a)" class="att-read" @click="$emit('preview-att', a)">阅读</button>
                <span class="att-size dim">{{ fmtSize(a.size) }}</span>
                <button v-if="canDelAtt(a)" class="del danger" @click="$emit('remove-att', a)">删除</button>
              </div>
            </div>
            <div v-else class="empty att-empty">还没有附件，可上传截图 / 文档等补充资料</div>
          </div>

          <!-- 变更记录 -->
          <div v-else class="modal-history">
            <ul class="hist-list">
              <li v-if="!history.length" class="dim hist-empty">暂无变更记录</li>
              <li v-for="h in history" :key="h.id" class="hist-item">
                <div class="hist-head">
                  <span class="chip accent hist-act">{{ histLabel(h.action) }}</span>
                  <b>{{ h.operator_name }}</b>
                  <span class="dim">{{ fmtTime(h.created_at) }}</span>
                </div>
                <div v-if="h.detail" class="hist-detail">
                  <div v-for="(ln, i) in histLines(h.detail)" :key="i" class="hist-line" :class="ln.cls">{{ ln.text || ' ' }}</div>
                </div>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </template>
  </section>
</template>

<script setup>
import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import RichTextEditor from '@/components/RichTextEditor.vue'
import MindMapEditor from '@/components/knowledge/MindMapEditor.vue'
import FlowEditor from '@/components/knowledge/FlowEditor.vue'
import ParentTreePicker from '@/components/knowledge/ParentTreePicker.vue'
import { downloadBlob, downloadText, safeFileName, imageBlobToPdf } from '@/utils/diagram'

const props = defineProps({
  entry: { type: Object, default: null },
  editing: { type: Boolean, default: false },
  saving: { type: Boolean, default: false },
  // 草稿由父级持有（保存逻辑需要它），本组件只做双向绑定
  draft: { type: Object, required: true },
  categories: { type: Array, default: () => [] },
  parentCandidates: { type: Array, default: () => [] },
  collabCandidates: { type: Array, default: () => [] },
  canEdit: { type: Boolean, default: false },
  canSetEditors: { type: Boolean, default: false },
  isSuper: { type: Boolean, default: false },
  currentUserId: { type: [String, Number], default: 0 },
  currentUserName: { type: String, default: '' },
  attachments: { type: Array, default: () => [] },
  pendingAtts: { type: Array, default: () => [] },
  uploading: { type: Boolean, default: false },
  comments: { type: Array, default: () => [] },
  versions: { type: Array, default: () => [] },
  outlinks: { type: Array, default: () => [] },
  backlinks: { type: Array, default: () => [] },
  history: { type: Array, default: () => [] },
  commentText: { type: String, default: '' },
  commentBusy: { type: Boolean, default: false },
  replyTo: { type: [String, Number], default: 0 },
  // 注入式工具函数
  safeHtml: { type: Function, default: (s) => String(s || '') },
  fmtTime: { type: Function, default: (s) => String(s || '') },
  fmtSize: { type: Function, default: () => '' },
  parseTags: { type: Function, default: () => [] },
  scopeLabel: { type: Function, default: (s) => String(s || '') },
  scopeChip: { type: Function, default: () => '' },
  editorsText: { type: Function, default: () => '' },
  histLabel: { type: Function, default: (a) => String(a || '') },
  histLines: { type: Function, default: () => [] },
  isPdf: { type: Function, default: () => false },
  fileIcon: { type: Function, default: () => '📄' },
  thumbOf: { type: Function, default: () => '' },
  canDelAtt: { type: Function, default: () => false },
})

const emit = defineEmits([
  'start-edit', 'cancel-edit', 'submit', 'create',
  'star', 'pin', 'open-ref', 'reply', 'cancel-reply', 'del-comment', 'send-comment',
  'restore', 'upload', 'preview-att', 'download-att', 'remove-att', 'remove-pending-att',
  'update:commentText', 'content-error', 'dirty-change',
])

const sub = ref('comments')
const tagDraft = ref('')
const showCollab = ref(false)
const dirty = ref(false)

// 内容类型 chip 菜单（v0.32.0：从顶部卡片改为标题旁 chip）
const rootKind = ref(null)
const kindMenuOpen = ref(false)
const canUpload = computed(() => props.canEdit || !props.draft.id)
const kindMeta = computed(() => KIND_OPTIONS.find((k) => k.value === curKind.value) || KIND_OPTIONS[0])

// ---------- 图形条目（思维导图 / 流程图，v0.30.0）----------
const diagramRef = ref(null)   // 指向当前渲染的图形编辑器（v-if 保证同时只有一个）
const exportOpen = ref(false)
const exporting = ref(false)

const KIND_OPTIONS = [
  { value: 'doc', icon: '📄', label: '富文本文档', desc: '图文 / 表格 / 代码块' },
  { value: 'mind', icon: '🧠', label: '思维导图', desc: '梳理结构与要点' },
  { value: 'flow', icon: '🔀', label: '流程图', desc: '画审批与操作步骤' },
]

/** 当前内容类型：编辑态看草稿，阅读态看条目；未知值一律按文档处理（历史数据兼容） */
const curKind = computed(() => {
  const raw = (props.editing ? props.draft && props.draft.kind : props.entry && props.entry.kind)
    || (props.draft && props.draft.kind) || 'doc'
  return raw === 'mind' || raw === 'flow' ? raw : 'doc'
})
const isDiagram = computed(() => curKind.value !== 'doc')
const kindLabelOf = computed(() => (curKind.value === 'mind' ? '思维导图' : '流程图'))

function markDirty() {
  if (!dirty.value) { dirty.value = true; emit('dirty-change', true) }
}

function pickKind(k) {
  if ((props.draft.kind || 'doc') === k) { kindMenuOpen.value = false; return }
  // 切换类型会把正文语义整个换掉（HTML ↔ 绘图 JSON 无法互转），已写内容必须确认放弃
  if (String(props.draft.content || '').trim() && !confirm('切换内容类型会清空当前正文，确定继续吗？')) { kindMenuOpen.value = false; return }
  props.draft.kind = k
  props.draft.content = ''
  kindMenuOpen.value = false
  markDirty()
}

// 新建态点 chip 弹出类型菜单；编辑态（已有 id）只读不可切
function onKindChipClick() {
  if (props.draft.id) return
  kindMenuOpen.value = !kindMenuOpen.value
}

function onDiagramDirty() { markDirty() }

// 本组件不持有全局 toast，借用父级已接好的 content-error 通道提示
function exportErr(msg) { emit('content-error', msg) }

/** 导出当前图形条目：png / svg / pdf / json */
async function exportAs(fmt) {
  exportOpen.value = false
  const ed = diagramRef.value
  const title = (props.entry && props.entry.title) || (props.draft && props.draft.title) || '未命名'
  try {
    if (fmt === 'json') {
      const raw = String((props.entry && props.entry.content) || (props.draft && props.draft.content) || '')
      if (!raw.trim()) return exportErr('当前图形还是空的，先画点内容再导出')
      downloadText(raw, safeFileName(title, 'json'), 'application/json;charset=utf-8')
      return
    }
    if (!ed || typeof ed.exportBlob !== 'function') return exportErr('图形编辑器还没准备好，请稍后重试')
    exporting.value = true
    if (fmt === 'svg') {
      const svg = await ed.exportBlob('svg')
      if (!svg) throw new Error('导出 SVG 失败')
      downloadBlob(svg, safeFileName(title, 'svg'))
      return
    }
    const png = await ed.exportBlob('png')
    if (!png) throw new Error('导出图片失败')
    if (fmt === 'png') downloadBlob(png, safeFileName(title, 'png'))
    else downloadBlob(await imageBlobToPdf(png), safeFileName(title, 'pdf'))
  } catch (e) {
    exportErr(e && e.message ? e.message : '导出失败')
  } finally {
    exporting.value = false
  }
}

/**
 * 统一的「当前展示对象」。
 * 新建条目时 entry 还是 null（后端尚未落库），但右栏必须能进入编辑态，
 * 所以回退到 draft（标题/分类/状态等都从 draft 读）。
 * 这样模板里全部用 view.* 即可，不用到处写 `entry || draft`。
 */
const view = computed(() => props.entry || (props.editing ? props.draft : null))

function go(t) {
  sub.value = t
  emit('sub-change', t)
}

function startEdit() {
  tagDraft.value = ''
  showCollab.value = false
  dirty.value = false
  emit('start-edit')
}

function cancelEdit() {
  if (dirty.value && !confirm('有未保存的改动，确定放弃吗？')) return
  dirty.value = false
  emit('cancel-edit')
}

function submit() {
  dirty.value = false
  emit('submit')
}

function onContent(v) {
  if (props.draft.content !== v) {
    props.draft.content = v
    if (!dirty.value) { dirty.value = true; emit('dirty-change', true) }
  }
}

watch(() => props.editing, (v) => { if (!v) { dirty.value = false; emit('dirty-change', false) } })

// 类型菜单：点击空白处自动收起
function onDocClickKind(e) {
  if (kindMenuOpen.value && rootKind.value && !rootKind.value.contains(e.target)) kindMenuOpen.value = false
}
onMounted(() => document.addEventListener('click', onDocClickKind))
onBeforeUnmount(() => document.removeEventListener('click', onDocClickKind))

function addTag() {
  const t = tagDraft.value.trim()
  if (t && !props.draft.tags.includes(t)) props.draft.tags.push(t)
  tagDraft.value = ''
}
function toggleCollab(uid) {
  const i = props.draft.editor_ids.indexOf(uid)
  if (i >= 0) props.draft.editor_ids.splice(i, 1)
  else props.draft.editor_ids.push(uid)
}

const attCount = computed(() => (props.pendingAtts || []).length + (props.draft.id ? (props.attachments || []).length : 0))
const collabNames = computed(() => (props.draft.editor_ids || []).map((id) => {
  const u = (props.collabCandidates || []).find((x) => x.id === id)
  return u ? u.name : ('#' + id)
}))
const topComments = computed(() => (props.comments || []).filter((c) => !c.parent_id))
function repliesOf(id) { return (props.comments || []).filter((c) => String(c.parent_id) === String(id)) }

// 打印：只把正文丢进打印容器，避免把整个后台壳子打出来
function printEntry() {
  if (!props.entry) return
  const w = window.open('', '_blank', 'width=820,height=900')
  if (!w) return
  const css = getComputedStyle(document.documentElement)
  const vars = ['--text', '--text-dim', '--accent', '--hairline', '--overlay-2']
    .map((k) => `${k}:${css.getPropertyValue(k)}`).join(';')
  w.document.write(`<!doctype html><html><head><meta charset="utf-8"><title>${props.entry.title}</title>
    <style>
      :root{${vars}}
      body{font-family:system-ui,-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;
           max-width:760px;margin:36px auto;padding:0 22px;color:var(--text);line-height:1.85}
      h1{font-size:22px;margin:0 0 8px}
      .meta{font-size:12px;color:var(--text-dim);margin-bottom:22px;
            padding-bottom:12px;border-bottom:1px solid var(--hairline)}
      img{max-width:100%;height:auto}
      table{border-collapse:collapse;width:100%}
      th,td{border:1px solid var(--hairline);padding:6px 10px}
    </style></head><body>
    <h1>${props.entry.title}</h1>
    <div class="meta">${props.entry.category || '未分类'} · ${props.fmtTime(props.entry.updated_at || props.entry.created_at)}</div>
    ${props.safeHtml(props.entry.content || '')}
    </body></html>`)
  w.document.close()
  nextTick(() => { w.focus(); w.print() })
}
</script>

<style scoped>
.kb-detail-pane {
  display: flex;
  flex-direction: column;
  min-width: 0;
  /* ⚠️ v0.28.1：必须有 min-height:0，否则 flex 子项不会收缩，
     内部 .kb-detail-body 无法触发滚动，底部按钮被挤出。 */
  min-height: 0;
  height: 100%;
  background: var(--bg-1);
}

/* 空态 */
.kb-detail-empty {
  flex: 1; display: flex; flex-direction: column;
  align-items: center; justify-content: center;
  gap: 14px; text-align: center; padding: 40px 24px;
}
.de-ico { font-size: 44px; opacity: .45; }
.de-txt p { margin: 0 0 6px; font-size: 13.5px; color: var(--text-dim); }
.de-txt p.dim { font-size: 12px; }

/* 头部 */
.kb-detail-head { flex: 0 0 auto; padding: 14px 18px 10px; border-bottom: 1px solid var(--hairline); }
.dh-top { display: flex; align-items: flex-start; gap: 10px; min-width: 0; }
.dh-title { flex: 1 1 auto; min-width: 0; font-size: 18px; font-weight: 700; margin: 0; line-height: 1.4; word-break: break-word; }
.dh-title-input {
  flex: 1 1 auto; min-width: 0;
  border: 1px solid var(--glass-border); background: var(--overlay);
  color: var(--text); border-radius: 10px; padding: 8px 12px;
  font-size: 16px; font-weight: 700; outline: none;
}
.dh-title-input:focus { border-color: var(--accent); }
.dh-req { flex: 0 0 auto; color: #dc2626; font-weight: 700; font-size: 16px; line-height: 34px; padding-left: 2px; }
.dh-ops { flex: 0 0 auto; display: flex; align-items: center; gap: 5px; }

/* v0.36.2：标题列（标题行 + 作者/时间常驻行 + 折叠提示行） */
.dh-title-wrap { flex: 1 1 auto; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.dh-title-row { display: flex; align-items: flex-start; gap: 4px; min-width: 0; }
.dh-sub { font-size: 11.5px; color: var(--text-faint); line-height: 1.5; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.dh-meta {
  display: flex; align-items: center; gap: 6px; flex-wrap: wrap;
  font-size: 11.5px; margin-top: 8px;
}
.dh-tag { color: var(--accent); }
.dh-mtime { margin-left: auto; }

/* 编辑表单 */
.dh-form { margin-top: 12px; display: flex; flex-direction: column; gap: 10px; }
.dhf-row { display: flex; gap: 10px; flex-wrap: wrap; }
.dhf-fld { flex: 1 1 160px; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.dhf-fld.wide { flex: 1 1 100%; }
.dhf-fld > span, .dhf-label { font-size: 11.5px; color: var(--text-faint); font-weight: 600; }
.dhf-collab { display: flex; flex-direction: column; gap: 6px; }

/* 正文区 */
.kb-detail-body { flex: 1 1 auto; min-height: 0; overflow-y: auto; padding: 16px 18px 22px; scrollbar-width: thin; }
.kb-detail-body::-webkit-scrollbar { width: 8px; }
.kb-detail-body::-webkit-scrollbar-thumb { background: var(--glass-border); border-radius: 4px; }

/* 编辑态底栏 */
.kb-detail-foot {
  flex: 0 0 auto; display: flex; align-items: center; justify-content: space-between;
  gap: 10px; padding: 10px 18px; border-top: 1px solid var(--hairline);
  background: var(--bg-1); flex-wrap: wrap; z-index: 5;
  /* v0.36.1：底栏作为抽屉面板的 flex 末子，自然落在面板底部，始终可见
     （编辑区内部滚动，底栏不被带出）。不再用 sticky——它的滚动祖先是整页，
     页面一滚动会贴到视口底部、压住全局页脚。 */
  box-shadow: 0 -3px 10px rgba(15,23,42,0.06);
}
.df-tip { font-size: 11.5px; }
.df-btns { display: flex; gap: 8px; }

/* 阅读态子标签 */
.kb-detail-tabs { flex: 0 0 auto; display: flex; flex-direction: column; min-height: 0; }
.k-tabs { display: flex; gap: 3px; padding: 0 18px; border-bottom: 1px solid var(--hairline); border-top: 1px solid var(--hairline); flex-wrap: wrap; }
.k-tab {
  border: none; background: transparent; color: var(--text-dim);
  padding: 8px 11px; border-radius: 9px 9px 0 0; cursor: pointer; font-size: 12.5px;
}
.k-tab.on { color: var(--accent); font-weight: 600; box-shadow: inset 0 -2px 0 var(--accent); }
.k-tab-body { max-height: 320px; overflow-y: auto; padding: 12px 18px 16px; scrollbar-width: thin; }
.k-tab-body::-webkit-scrollbar { width: 7px; }
.k-tab-body::-webkit-scrollbar-thumb { background: var(--glass-border); border-radius: 4px; }

/* 评论 */
.k-comments { display: flex; flex-direction: column; gap: 10px; }
.comment { border-left: 2px solid var(--glass-border); padding-left: 10px; }
.comment.reply { margin-left: 14px; border-left-color: var(--accent-soft); }
.comment-head { display: flex; align-items: center; gap: 8px; font-size: 12.5px; }
.comment-body { font-size: 13px; line-height: 1.7; margin: 3px 0; word-break: break-word; }
.comment-ops { display: flex; gap: 8px; }
.comment-editor { display: flex; flex-direction: column; gap: 8px; margin-top: 4px; }
.comment-send { display: flex; justify-content: flex-end; gap: 8px; }
.ta { resize: vertical; }

/* 版本 */
.k-versions { display: flex; flex-direction: column; gap: 10px; }
.version-item { border: 1px solid var(--glass-border); border-radius: 10px; padding: 10px 12px; display: flex; flex-direction: column; gap: 6px; }
.version-head { display: flex; align-items: center; gap: 8px; font-size: 12.5px; }
.version-meta { font-size: 12px; }

/* 双链 */
.k-links { display: flex; flex-direction: column; gap: 14px; }
.link-group { display: flex; flex-direction: column; gap: 6px; }
.link-h { font-size: 12.5px; font-weight: 600; color: var(--text-dim); }
.link-item { font-size: 13px; color: var(--accent); cursor: pointer; padding: 3px 0; }
.link-item:hover { text-decoration: underline; }

/* 附件 */
.modal-attach { display: flex; flex-direction: column; gap: 8px; }
.att-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.att-title { font-size: 13px; font-weight: 600; }
.att-list { display: flex; flex-direction: column; gap: 6px; }
.att-item { display: flex; align-items: center; gap: 9px; border: 1px solid var(--glass-border); border-radius: 9px; padding: 6px 9px; min-width: 0; }
.att-thumb { width: 36px; height: 36px; object-fit: cover; border-radius: 6px; cursor: pointer; flex: 0 0 auto; }
.att-ico { width: 36px; height: 36px; display: inline-flex; align-items: center; justify-content: center; font-size: 17px; background: var(--overlay-2); border-radius: 6px; cursor: pointer; flex: 0 0 auto; }
.att-name { flex: 1 1 auto; min-width: 0; font-size: 12.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; cursor: pointer; }
.att-name:hover { color: var(--accent); }
.att-size { flex: 0 0 auto; font-size: 11px; }
.att-read, .del { border: none; background: transparent; color: var(--text-dim); font-size: 11.5px; cursor: pointer; padding: 2px 6px; border-radius: 6px; }
.att-read:hover { color: var(--accent); }
.del:hover { color: var(--accent); }
.del.danger:hover { color: #dc2626; }
.att-hint { font-size: 11px; }
.att-empty { font-size: 12.5px; }

/* 变更记录 */
.modal-history { display: flex; flex-direction: column; gap: 8px; }
.hist-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 9px; }
.hist-item { border-left: 2px solid var(--glass-border); padding-left: 10px; }
.hist-head { display: flex; align-items: center; gap: 7px; font-size: 12.5px; }
.hist-detail { margin-top: 4px; font-size: 12px; }
.hist-line { line-height: 1.6; }
.hist-line.add { color: #16a34a; }
.hist-line.del { color: #dc2626; }
.hist-empty { font-size: 12.5px; }

/* 窄屏兜底 */
@media (max-width: 820px) {
  .kb-detail-head { padding: 12px 13px 9px; }
  .kb-detail-body { padding: 12px 13px 18px; }
  .k-tabs { padding: 0 12px; }
  .k-tab-body { padding: 10px 13px 14px; max-height: 260px; }
  .dh-title { font-size: 16px; }
  .dh-mtime { margin-left: 0; flex-basis: 100%; }
}

/* ---------- 图形条目（v0.30.0）---------- */
.dh-export { position: relative; display: inline-flex; }
.dh-export-menu {
  position: absolute; right: 0; top: calc(100% + 6px); z-index: 5;
  min-width: 176px; padding: 6px; display: flex; flex-direction: column; gap: 2px;
  background: var(--bg-1); border: 1px solid var(--glass-border); border-radius: 10px;
  box-shadow: 0 10px 26px var(--overlay);
}
.dh-export-menu button {
  padding: 7px 10px; font-size: 13px; text-align: left; color: var(--text);
  background: transparent; border: 0; border-radius: 7px; cursor: pointer;
}
.dh-export-menu button:hover:not(:disabled) { background: var(--overlay-2); color: var(--accent); }
.dh-export-menu button:disabled { opacity: .5; cursor: not-allowed; }
.dhe-tip { padding: 4px 10px 2px; font-size: 11px; color: var(--text-faint); }

.kind-pick { display: flex; gap: 8px; flex-wrap: wrap; }
.kind-opt {
  display: flex; flex-direction: column; align-items: flex-start; gap: 2px;
  min-width: 140px; padding: 9px 12px; text-align: left; cursor: pointer;
  color: var(--text); background: var(--overlay-2);
  border: 1px solid var(--glass-border); border-radius: 10px;
}
.kind-opt:hover { border-color: var(--accent); }
.kind-opt.on { border-color: var(--accent); background: var(--accent-soft); }
.kind-ico { font-size: 15px; line-height: 1.1; }
.kind-txt { font-size: 13px; font-weight: 600; }
.kind-sub { font-size: 11px; color: var(--text-faint); }

/* 图形编辑器自带边框与提示条，正文区收紧留白，把空间让给画布 */
.kb-detail-body.kb-body-flush { padding: 10px 10px 14px; }
/* v0.36.0：编辑态图形条目 —— 画布吃掉正文区全部剩余高度（表单一行、附件折叠） */
.kb-detail-body.kb-body-canvas {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: 4px 12px 8px;
}
.kb-body-canvas .mm,
.kb-body-canvas .wfc { flex: 1 1 auto; height: auto; min-height: 480px; }
/* 只读查看：给足高度 */
.kb-detail-body.kb-body-flush .mm,
.kb-detail-body.kb-body-flush .wfc { min-height: max(560px, calc(100vh - 420px)); }

/* v0.36.0：基础字段压成一行（内容类型 chip / 分类 / 状态 / 所属父级 / 标签） */
.dh-form-compact { flex-direction: row; flex-wrap: wrap; gap: 8px 14px; margin-top: 10px; align-items: center; }
/* v0.36.3：label 与控件同行，把 内容/分类/状态/所属父级/标签 压成一行 */
.dh-form-compact .dhf-fld { flex: 1 1 150px; min-width: 140px; flex-direction: row; align-items: center; gap: 6px; }
.dh-form-compact .dhf-fld > span, .dh-form-compact .dhf-label { white-space: nowrap; }
.dh-form-compact .dhf-fld.grow { flex: 1.8 1 220px; }
.chip-fld { flex: 0 0 auto !important; min-width: 0 !important; position: relative; }
.chip-fld .kind-menu { left: 0; }

/* v0.36.0：附件/协作/可见范围 折叠条 */
.dh-fold { border-top: 1px solid var(--hairline); margin-top: 8px; }
.dh-fold-bar {
  display: flex; align-items: center; gap: 10px;
  padding: 8px 18px; cursor: pointer; user-select: none;
  font-size: 12.5px; color: var(--text-dim); list-style: none;
}
.dh-fold-bar::-webkit-details-marker { display: none; }
.dh-fold-bar:hover { color: var(--accent); }
.dh-fold-tt { font-weight: 600; }
.dh-fold-cnt { font-size: 11.5px; }
.dh-fold-arrow { margin-left: auto; font-size: 10px; transition: transform .15s; }
.dh-fold[open] .dh-fold-arrow { transform: rotate(180deg); }
.dh-fold .dh-form-lower { margin-top: 0; padding-top: 2px; }

/* ---------- 内容类型 chip（v0.32.0：从顶部卡片改为标题旁 chip）---------- */
.dh-content-head {
  position: relative;
  display: flex; align-items: center; gap: 10px;
  padding: 12px 18px 0; flex: 0 0 auto;
}
.dh-content-head .dhf-label { font-size: 11.5px; color: var(--text-faint); font-weight: 600; }
.kind-chip {
  display: inline-flex; align-items: center; gap: 6px;
  border: 1px solid var(--accent); color: var(--accent);
  background: var(--accent-soft); border-radius: 999px;
  padding: 5px 12px; font-size: 12.5px; font-weight: 600; cursor: pointer;
}
.kind-chip[disabled] { cursor: default; opacity: .95; }
.kind-chip-ico { font-size: 13px; line-height: 1; }
.kind-chip-caret { font-size: 9px; opacity: .8; }
.kind-menu {
  position: absolute; left: 18px; top: calc(100% - 2px); z-index: 30;
  min-width: 232px; padding: 6px; display: flex; flex-direction: column; gap: 2px;
  background: var(--bg-1); border: 1px solid var(--glass-border); border-radius: 12px;
  box-shadow: 0 12px 30px var(--overlay);
}
.kind-menu-item {
  display: flex; flex-direction: column; align-items: flex-start; gap: 1px;
  padding: 8px 11px; text-align: left; cursor: pointer;
  color: var(--text); background: transparent; border: 0; border-radius: 8px;
  font-size: 13px;
}
.kind-menu-item:hover { background: var(--overlay-2); }
.kind-menu-item.on { background: var(--accent-soft); }
.kind-menu-item .kind-ico { font-size: 14px; line-height: 1.1; }
.kind-menu-item .kind-txt { font-size: 13px; font-weight: 600; }
.kind-menu-item .kind-sub { font-size: 11px; color: var(--text-faint); }

/* 编辑态下半部表单（附件 / 协作 / 可见范围） */
.dh-form-lower { margin-top: 14px; padding: 0 18px; gap: 12px; }
.dhf-attach { display: flex; flex-direction: column; gap: 7px; border-top: 1px solid var(--hairline); padding-top: 12px; }
.dhf-attach .att-hint { font-size: 11.5px; margin: 0; }
.dhf-attach .att-list { display: flex; flex-direction: column; gap: 6px; }
.dhf-attach .att-item { display: flex; align-items: center; gap: 9px; border: 1px solid var(--glass-border); border-radius: 9px; padding: 6px 9px; min-width: 0; }
.dhf-attach .att-ico { width: 34px; height: 34px; display: inline-flex; align-items: center; justify-content: center; font-size: 16px; background: var(--overlay-2); border-radius: 6px; flex: 0 0 auto; }
.dhf-attach .att-name { flex: 1 1 auto; min-width: 0; font-size: 12.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.dhf-attach .att-size { flex: 0 0 auto; font-size: 11px; }
.dhf-attach .att-empty { font-size: 12.5px; }
.dhf-attach .att-upload-row { display: flex; align-items: center; gap: 10px; }
</style>
