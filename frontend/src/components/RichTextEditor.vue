<template>
  <div class="rte" :class="{ 'rte-disabled': disabled }">
    <!-- 工具栏（编辑态显示） -->
    <EditorToolbar
      v-if="!disabled && editor"
      :editor="editor"
      :find-open="findOpen"
      @toggle-find="toggleFind"
    />

    <!-- 编辑区 -->
    <div class="rte-wrap">
      <EditorContent :editor="editor" class="rte-body" />
    </div>

    <!-- 粘贴告警（本地占位图无法读取） -->
    <div v-if="pasteWarn" class="rte-warn-bar" role="alert">{{ pasteWarn }}</div>

    <!-- 状态条 -->
    <div class="rte-foot" v-if="!disabled">
      <span class="rte-tip">像 Word 一样直接编辑：支持字号/颜色/行距/缩进/表格/图片缩放，可整篇复制粘贴（图片自动上传）</span>
      <span class="rte-count" v-if="editor">{{ textLen }} 字 · {{ imageCount }} 图</span>
    </div>

    <!-- 查找替换面板 -->
    <FindReplacePanel
      v-if="editor"
      v-model:open="findOpen"
      :editor="editor"
      :state="findState"
    />

    <!-- 图片对齐/尺寸浮层 -->
    <div
      v-if="imagePopup.visible"
      class="kb-img-popup"
      :style="{ left: imagePopup.x + 'px', top: imagePopup.y + 'px' }"
    >
      <button type="button" class="pi-btn" :class="{ on: imagePopup.attrs['data-align'] === 'left' }" title="左对齐" @click="setImageAlign('left')">⬅</button>
      <button type="button" class="pi-btn" :class="{ on: !imagePopup.attrs['data-align'] || imagePopup.attrs['data-align'] === 'center' }" title="居中" @click="setImageAlign('center')">⬛</button>
      <button type="button" class="pi-btn" :class="{ on: imagePopup.attrs['data-align'] === 'right' }" title="右对齐" @click="setImageAlign('right')">➡</button>
      <span class="pi-sep"></span>
      <button type="button" class="pi-btn" title="缩小到 25%" @click="setImageWidth(0.25)">25%</button>
      <button type="button" class="pi-btn" title="缩小到 50%" @click="setImageWidth(0.5)">50%</button>
      <button type="button" class="pi-btn" title="恢复原始宽度" @click="setImageWidth(1)">100%</button>
      <span class="pi-sep"></span>
      <button type="button" class="pi-btn danger" title="删除图片" @click="deleteImage">🗑</button>
    </div>
  </div>
</template>

<script setup>
/**
 * RichTextEditor.vue —— Tiptap(ProseMirror) 富文本编辑器封装（v0.28.0）
 *
 * 【对外契约 100% 不变】
 * 旧实现基于 contenteditable + document.execCommand，本次整体重写为 Tiptap，
 * 但文件名、路径、props 五件套、两个 emit、defineExpose 全部逐字保留，
 * 因此 Workspace.vue / KnowledgeDetail.vue 的调用代码零改动。
 *
 * 【关键实现约束】（详见 docs/knowledge-v0.28/01-知识库全新设计文档.md）
 *   1. 必须透传 data-att-id / data-temp-id / data-att-name —— 附件转正机制依赖
 *      （见 ImageAttrs.js 头注释）
 *   2. setContent 必须传 { emitUpdate: false } + lastEmitted 哨兵，防 v-model 回环
 *   3. onBeforeUnmount 必须 destroy()，否则切换条目会残留监听与内存泄漏
 *   4. 存量 HTML 走 upgradeLegacyHtml() 解析期归一化（零迁移）
 */
import { ref, reactive, computed, watch, onBeforeUnmount, nextTick } from 'vue'
import { useEditor, EditorContent } from '@tiptap/vue-3'
import * as api from '@/api'
import { buildExtensions } from './editor/extensions'
import { upgradeLegacyHtml } from './editor/legacyHtml'
import EditorToolbar from './editor/EditorToolbar.vue'
import FindReplacePanel from './editor/FindReplacePanel.vue'

const props = defineProps({
  modelValue: { type: String, default: '' },
  placeholder: { type: String, default: '在这里输入内容…' },
  disabled: { type: Boolean, default: false },
  // 关联的知识条目 ID：有 ID 时图片走正式附件；无 ID（新建未保存）时进中转缓存
  entryId: { type: Number, default: 0 },
  // 可选：自定义上传 endpoint（默认走知识附件上传）
  uploadUrl: { type: String, default: '' },
})
const emit = defineEmits(['update:modelValue', 'imageUploadError'])

// ---------- 状态 ----------
const findOpen = ref(false)
const findState = ref({ term: '', replace: '', matches: [], current: 0, caseSensitive: false, wholeWord: false })
const textLen = ref(0)
const imageCount = ref(0)
const pasteWarn = ref('')
const imagePopup = reactive({ visible: false, pos: 0, x: 0, y: 0, attrs: {} })

let _pasteWarnTimer = null
let lastEmitted = '' // 防回环哨兵

function flashPasteWarn(msg) {
  pasteWarn.value = msg
  clearTimeout(_pasteWarnTimer)
  _pasteWarnTimer = setTimeout(() => { pasteWarn.value = '' }, 6000)
}

// ---------- 图片上传 ----------
/**
 * 上传文件并返回可用于 <img src> 的地址。
 * 与旧实现（RichEditor.vue:263-272）完全一致，两条路径不可改：
 *   entryId > 0 → 正式附件端点，用 stored_name 拼 URL（不可枚举，防遍历自增 ID）
 *   entryId = 0 → 中转缓存端点，保存时由后端 adoptTempAttachments 转正
 */
async function uploadFileReturnUrl(file) {
  if (props.entryId) {
    const att = await api.upload('/workspace/knowledge/' + props.entryId + '/attachments', file)
    const key = att.stored_name || att.id
    return { dl: '/api/workspace/knowledge_attachments/' + key + '/download', id: att.id, temp: false, name: att.file_name || file.name }
  }
  const att = await api.upload('/workspace/temp-attachments', file)
  const dl = att.download_url || ('/api/workspace/temp-attachments/' + (att.stored_name || att.id) + '/download')
  return { dl, id: att.id, temp: true, name: att.file_name || file.name }
}

function onUploadError(err) {
  emit('imageUploadError', err)
  const msg =
    (err && err.response && err.response.data && err.response.data.error) ||
    (err && err.message) ||
    '图片上传失败'
  flashPasteWarn(msg)
}

// ---------- 编辑器实例 ----------
const editor = useEditor({
  // 存量 HTML 解析期归一化（font→span、align→text-align、h5h6→h4）
  content: upgradeLegacyHtml(props.modelValue || ''),
  editable: !props.disabled,
  extensions: buildExtensions({
    placeholder: props.placeholder,
    onImageUpload: uploadFileReturnUrl,
    onUploadError,
    isEditable: () => !props.disabled,
    onFindReplaceChange: (st) => { findState.value = st },
  }),
  editorProps: {
    attributes: { class: 'kb-prose' },
    // 点击图片 → 弹出对齐/尺寸浮层
    handleDOMEvents: {
      'kb-image-select': () => true,
    },
  },
  onUpdate: ({ editor: ed }) => {
    lastEmitted = ed.getHTML()
    emit('update:modelValue', lastEmitted)
    updateMeta()
  },
  onCreate: ({ editor: ed }) => {
    lastEmitted = ed.getHTML()
    updateMeta()
    // 监听 NodeView 冒泡的图片选中事件
    ed.view.dom.addEventListener('kb-image-select', onImageSelect)
  },
})

/** 图片被点击 → 计算浮层位置 */
function onImageSelect(e) {
  const { pos, attrs } = e.detail || {}
  const ed = editor.value
  if (!ed || typeof pos !== 'number') return
  try {
    const dom = ed.view.nodeDOM(pos)
    const rect = (dom && dom.getBoundingClientRect) ? dom.getBoundingClientRect() : null
    if (!rect) return
    imagePopup.pos = pos
    imagePopup.attrs = { ...attrs }
    imagePopup.x = rect.left + rect.width / 2
    imagePopup.y = rect.bottom + window.scrollY + 6
    imagePopup.visible = true
  } catch {
    /* 节点已被删除等情况，忽略 */
  }
}

/** 隐藏图片浮层 */
function hideImagePopup() {
  imagePopup.visible = false
}

/** 点击文档其他位置关闭浮层 */
function onDocClick(e) {
  if (!imagePopup.visible) return
  if (e.target && e.target.closest && e.target.closest('.kb-img-popup')) return
  hideImagePopup()
}

// ---------- 图片浮层操作 ----------
function setImageAlign(align) {
  const ed = editor.value
  if (!ed || typeof imagePopup.pos !== 'number') return
  const node = ed.state.doc.nodeAt(imagePopup.pos)
  if (!node || node.type.name !== 'image') return
  ed.view.dispatch(
    ed.view.state.tr.setNodeMarkup(imagePopup.pos, null, { ...node.attrs, 'data-align': align }),
  )
  imagePopup.attrs = { ...imagePopup.attrs, 'data-align': align }
}

function setImageWidth(ratio) {
  const ed = editor.value
  if (!ed || typeof imagePopup.pos !== 'number') return
  const node = ed.state.doc.nodeAt(imagePopup.pos)
  if (!node || node.type.name !== 'image') return

  const dom = ed.view.nodeDOM(imagePopup.pos)
  const img = dom && dom.querySelector ? dom.querySelector('img') : null
  const natural = img && img.naturalWidth ? img.naturalWidth : 0

  let width = null
  if (ratio < 1) {
    // 以图片容器当前显示宽度为基准（拿不到自然宽度时也能用）
    const base = natural || (img ? img.getBoundingClientRect().width : 0)
    width = base ? Math.round(base * ratio) : null
  }
  ed.view.dispatch(
    ed.view.state.tr.setNodeMarkup(imagePopup.pos, null, { ...node.attrs, width, height: null }),
  )
  imagePopup.attrs = { ...imagePopup.attrs, width }
}

function deleteImage() {
  const ed = editor.value
  if (!ed || typeof imagePopup.pos !== 'number') return
  const node = ed.state.doc.nodeAt(imagePopup.pos)
  if (!node) return
  ed.view.dispatch(ed.view.state.tr.delete(imagePopup.pos, imagePopup.pos + node.nodeSize))
  hideImagePopup()
}

// ---------- 查找替换 ----------
function toggleFind() {
  findOpen.value = !findOpen.value
  if (!findOpen.value && editor.value) editor.value.commands.clearSearch()
}

// ---------- 元信息 ----------
function updateMeta() {
  const ed = editor.value
  if (!ed) return
  textLen.value = ed.storage?.characterCount?.characters?.() ?? 0
  let imgs = 0
  ed.state.doc.descendants((n) => { if (n.type.name === 'image') imgs++ })
  imageCount.value = imgs
}

// ---------- 生命周期与同步 ----------
// 外部 v-model 变化 → setContent
watch(
  () => props.modelValue,
  (v) => {
    const ed = editor.value
    if (!ed) return
    if (v === lastEmitted) return // 哨兵：自己刚 emit 出去的值，不同步回来
    const html = upgradeLegacyHtml(v || '')
    if (ed.getHTML() === html) return
    // emitUpdate:false 是关键 —— 否则会触发 onUpdate → emit → 再 watch，形成回环
    ed.commands.setContent(html, { emitUpdate: false })
    lastEmitted = html
    updateMeta()
  },
)

// 只读态切换
watch(
  () => props.disabled,
  (d) => {
    const ed = editor.value
    if (ed) ed.setEditable(!d)
  },
)

// entryId 变化（新建条目保存后拿到 ID）→ 让后续图片走正式端点
watch(
  () => props.entryId,
  (id) => {
    if (id) hideImagePopup()
  },
)

// 全局点击关闭图片浮层
if (typeof document !== 'undefined') {
  document.addEventListener('click', onDocClick, true)
}

onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick, true)
  clearTimeout(_pasteWarnTimer)
  const ed = editor.value
  if (ed) {
    ed.view.dom.removeEventListener('kb-image-select', onImageSelect)
    ed.destroy() // 必须：否则切换条目会残留监听、内存泄漏
  }
})

// ---------- 对外暴露（与旧实现同名同义）----------
defineExpose({
  focus: () => editor.value && editor.value.commands.focus(),
  clear: () => {
    const ed = editor.value
    if (!ed) return
    ed.commands.clearContent(true)
    lastEmitted = ed.getHTML()
    emit('update:modelValue', lastEmitted)
    updateMeta()
  },
})
</script>

<style scoped>
.rte { border: 1px solid var(--glass-border); border-radius: 12px; background: var(--bg-1); display: flex; flex-direction: column; }
.rte-wrap { position: relative; }
.rte-body { min-height: 220px; max-height: 560px; overflow-y: auto; }
.rte-warn-bar { padding: 6px 12px; font-size: 12px; color: #b45309; background: rgba(217, 119, 6, 0.08); border-top: 1px solid var(--hairline); }
.rte-foot { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 6px 12px; border-top: 1px solid var(--hairline); font-size: 11.5px; color: var(--text-faint); }
.rte-tip { flex: 1; min-width: 0; }
.rte-count { white-space: nowrap; }
.rte-disabled .rte-body { background: var(--overlay); color: var(--text-dim); }

/* 图片浮层 */
.kb-img-popup {
  position: absolute; z-index: 40; transform: translateX(-50%);
  display: flex; align-items: center; gap: 2px; padding: 4px 6px;
  background: var(--glass-strong); backdrop-filter: blur(14px);
  border: 1px solid var(--glass-border-strong); border-radius: 10px;
  box-shadow: var(--shadow-sm);
}
.pi-btn {
  min-width: 26px; height: 26px; padding: 0 6px; border: 1px solid transparent; border-radius: 7px;
  background: transparent; color: var(--text-dim); cursor: pointer; font-size: 12px; line-height: 1;
}
.pi-btn:hover { background: var(--overlay-2); color: var(--text); }
.pi-btn.on { background: var(--accent-soft); color: var(--accent); border-color: var(--accent); }
.pi-btn.danger:hover { color: var(--danger); }
.pi-sep { width: 1px; height: 16px; background: var(--glass-border); margin: 0 3px; }
</style>
