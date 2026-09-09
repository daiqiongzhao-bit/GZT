<template>
  <div class="rte" :class="{ 'rte-disabled': disabled }">
    <!-- 工具栏 -->
    <div class="rte-toolbar" v-if="!disabled">
      <button type="button" class="t-btn" :class="{ on: active.bold }" @mousedown.prevent="exec('bold')" title="加粗 (Ctrl+B)"><b>B</b></button>
      <button type="button" class="t-btn" :class="{ on: active.italic }" @mousedown.prevent="exec('italic')" title="斜体 (Ctrl+I)"><i>I</i></button>
      <button type="button" class="t-btn" :class="{ on: active.underline }" @mousedown.prevent="exec('underline')" title="下划线 (Ctrl+U)"><u>U</u></button>
      <button type="button" class="t-btn" :class="{ on: active.strike }" @mousedown.prevent="exec('strikeThrough')" title="删除线"><s>S</s></button>
      <span class="t-sep"></span>
      <button type="button" class="t-btn" :class="{ on: active.h2 }" @mousedown.prevent="execBlock('H2')" title="二级标题">H2</button>
      <button type="button" class="t-btn" :class="{ on: active.h3 }" @mousedown.prevent="execBlock('H3')" title="三级标题">H3</button>
      <button type="button" class="t-btn" :class="{ on: active.p }" @mousedown.prevent="execBlock('P')" title="正文">P</button>
      <span class="t-sep"></span>
      <button type="button" class="t-btn" :class="{ on: active.ul }" @mousedown.prevent="exec('insertUnorderedList')" title="无序列表">• 列表</button>
      <button type="button" class="t-btn" :class="{ on: active.ol }" @mousedown.prevent="exec('insertOrderedList')" title="有序列表">1. 列表</button>
      <button type="button" class="t-btn" @mousedown.prevent="exec('justifyLeft')" title="左对齐">⇤</button>
      <button type="button" class="t-btn" @mousedown.prevent="exec('justifyCenter')" title="居中">≡</button>
      <span class="t-sep"></span>
      <button type="button" class="t-btn" @mousedown.prevent="insertLink" title="插入超链接">🔗 链接</button>
      <button type="button" class="t-btn" :disabled="!canInsertImage" @mousedown.prevent="pickImage" :title="canInsertImage ? '插入图片' : imageBtnHint">🖼 图片</button>
      <button type="button" class="t-btn" @mousedown.prevent="insertHr" title="分割线">―</button>
      <button type="button" class="t-btn danger" @mousedown.prevent="exec('removeFormat')" title="清除格式">⌫ 清除</button>
      <input ref="fileInput" type="file" accept="image/*" multiple hidden @change="onFilePick" />
    </div>

    <!-- 编辑区 -->
    <div
      ref="editable"
      class="rte-body"
      contenteditable="true"
      :data-placeholder="placeholder"
      @paste="onPaste"
      @input="onInput"
      @blur="onInput"
      @keydown="onKeydown"
      @mouseup="updateActive"
      @keyup="updateActive"
    ></div>

    <!-- 状态条 -->
    <div class="rte-foot" v-if="!disabled">
      <span class="rte-tip">支持 Markdown 风格的换行；粘贴图片自动上传；外部粘贴的富文本会自动去除字体/字号等噪声样式</span>
      <span class="rte-count" v-if="modelValue">{{ textLen }} 字 · {{ imageCount }} 图</span>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, nextTick } from 'vue'
import * as api from '@/api'

const props = defineProps({
  modelValue: { type: String, default: '' },
  placeholder: { type: String, default: '在这里输入内容…' },
  disabled: { type: Boolean, default: false },
  // 关联的知识条目 ID：有 ID 时允许插入图片（粘贴 / 按钮）；无 ID 时禁用图片插入（要求先保存）
  entryId: { type: Number, default: 0 },
  // 可选：自定义上传 endpoint（默认走知识附件上传）；返回的 url 用于 <img src>
  uploadUrl: { type: String, default: '' }
})
const emit = defineEmits(['update:modelValue', 'imageUploadError'])

const editable = ref(null)
const fileInput = ref(null)
const uploading = ref(false)
const active = reactive({ bold: false, italic: false, underline: false, strike: false, h2: false, h3: false, p: false, ul: false, ol: false })
const textLen = ref(0)
const imageCount = ref(0)

const canInsertImage = computed(() => !!props.entryId && !props.disabled)
const imageBtnHint = computed(() => props.entryId ? '' : '请先保存条目后再插入图片')

// ---------- 内容初始化 / 同步 ----------
watch(() => props.modelValue, (v) => {
  const el = editable.value
  if (!el) return
  const html = sanitize(v || '')
  if (el.innerHTML !== html) el.innerHTML = html
  updateMeta()
})
onMounted(async () => {
  const el = editable.value
  if (!el) return
  // 首次：把外层传入的内容渲染进来
  el.innerHTML = sanitize(props.modelValue || '')
  await nextTick()
  updateMeta()
})

// ---------- v-model 双向绑定 ----------
let lastEmitted = ''
function onInput() {
  const el = editable.value
  if (!el) return
  const html = sanitize(el.innerHTML)
  if (html !== lastEmitted) {
    lastEmitted = html
    emit('update:modelValue', html)
  }
  updateMeta()
}

// ---------- 输入 ----------
function onKeydown(e) {
  // Ctrl+B / I / U
  if ((e.ctrlKey || e.metaKey) && !e.shiftKey && !e.altKey) {
    const k = e.key.toLowerCase()
    if (k === 'b') { e.preventDefault(); exec('bold') }
    else if (k === 'i') { e.preventDefault(); exec('italic') }
    else if (k === 'u') { e.preventDefault(); exec('underline') }
  }
}

// ---------- execCommand（exec / execBlock） ----------
function exec(cmd, val = null) {
  editable.value && editable.value.focus()
  document.execCommand(cmd, false, val)
  onInput()
  updateActive()
}
function execBlock(tag) {
  editable.value && editable.value.focus()
  document.execCommand('formatBlock', false, tag)
  onInput()
  updateActive()
}
function insertLink() {
  const url = prompt('请输入链接地址（http(s):// 开头）：', 'https://')
  if (!url) return
  if (!/^https?:\/\//i.test(url) && !url.startsWith('/')) { alert('链接必须以 http(s):// 开头，或以 / 开头表示站内路径'); return }
  const sel = window.getSelection()
  let text = sel && sel.toString() ? sel.toString() : url
  const html = `<a href="${escapeAttr(url)}" target="_blank" rel="noopener noreferrer">${escapeHtml(text)}</a>`
  exec('insertHTML', html)
}
function insertHr() { exec('insertHTML', '<hr/>') }

// ---------- 图片：粘贴 / 选择 ----------
function pickImage() {
  if (!canInsertImage.value) {
    alert(imageBtnHint.value || '当前不可插入图片')
    return
  }
  fileInput.value && fileInput.value.click()
}
async function onFilePick(e) {
  const files = Array.from(e.target.files || [])
  e.target.value = ''
  for (const f of files) await uploadAndInsert(f)
}
async function onPaste(e) {
  // 浏览器粘贴：优先处理图片；文字走默认（会自动走 execCommand 的 sanitize）
  const items = e.clipboardData && e.clipboardData.items ? Array.from(e.clipboardData.items) : []
  const imageItems = items.filter((it) => it.kind === 'file' && it.type && it.type.startsWith('image/'))
  if (imageItems.length) {
    if (!canInsertImage.value) {
      e.preventDefault()
      alert('当前还未保存条目，无法上传图片。请先点「保存」再粘贴图片。')
      return
    }
    e.preventDefault()
    for (const it of imageItems) {
      const f = it.getAsFile()
      if (f) await uploadAndInsert(f)
    }
    return
  }
  // 文字粘贴：清理常见噪声样式（保留段落/列表/粗体/链接）
  const html = e.clipboardData && e.clipboardData.getData ? e.clipboardData.getData('text/html') : ''
  const text = e.clipboardData && e.clipboardData.getData ? e.clipboardData.getData('text/plain') : ''
  if (html) {
    e.preventDefault()
    exec('insertHTML', cleanPastedHtml(html))
  } else if (text) {
    // 让默认行为走（自动换行）
  }
}
async function uploadAndInsert(file) {
  if (!props.entryId) return
  uploading.value = true
  try {
    const url = props.uploadUrl || ('/workspace/knowledge/' + props.entryId + '/attachments')
    const att = await api.upload(url, file)
    if (!att || !att.id) throw new Error('上传返回为空')
    const dl = '/api/workspace/knowledge_attachments/' + att.id + '/download'
    const imgHtml = `<img src="${dl}" alt="${escapeAttr(att.file_name || 'image')}" data-att-id="${att.id}" data-att-name="${escapeAttr(att.file_name || '')}"/>`
    exec('insertHTML', imgHtml)
  } catch (err) {
    emit('imageUploadError', err)
    alert((err && err.response && err.response.data && err.response.data.error) || (err && err.message) || '图片上传失败')
  } finally {
    uploading.value = false
  }
}

// ---------- 工具 ----------
function updateMeta() {
  const el = editable.value
  if (!el) return
  const txt = (el.innerText || '').replace(/\s+/g, ' ').trim()
  textLen.value = txt.length
  imageCount.value = el.querySelectorAll('img').length
}
function updateActive() {
  // 用 queryCommandState 读取当前选区状态，更新工具栏高亮
  try {
    active.bold = document.queryCommandState('bold')
    active.italic = document.queryCommandState('italic')
    active.underline = document.queryCommandState('underline')
    active.strike = document.queryCommandState('strikeThrough')
    // 当前块类型
    let blk = ''
    const sel = window.getSelection()
    if (sel && sel.rangeCount) {
      let n = sel.anchorNode
      while (n && n.nodeType !== 1) n = n.parentNode
      while (n) {
        const tag = (n.tagName || '').toUpperCase()
        if (['H1','H2','H3','H4','P','BLOCKQUOTE','PRE'].includes(tag)) { blk = tag; break }
        if (n === editable.value) break
        n = n.parentNode
      }
    }
    active.h2 = blk === 'H2'; active.h3 = blk === 'H3'; active.p = blk === 'P'
    active.ul = document.queryCommandState('insertUnorderedList')
    active.ol = document.queryCommandState('insertOrderedList')
  } catch (e) { /* 忽略 */ }
}

// 净化外层 v-model 内容（防止历史脏数据 / 被注入的 script 等）
function sanitize(html) {
  if (!html) return ''
  let s = String(html)
  // 去除 <script>/<style>/<iframe>/<object>/<embed> 及 on* 事件
  s = s.replace(/<\s*(script|style|iframe|object|embed|link|meta)\b[^>]*>[\s\S]*?<\s*\/\s*\1\s*>/gi, '')
  s = s.replace(/<\s*(script|style|iframe|object|embed|link|meta)\b[^>]*\/?>/gi, '')
  s = s.replace(/\son\w+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)/gi, '')
  s = s.replace(/(href|src)\s*=\s*"javascript:[^"]*"/gi, '')
  return s
}

// 净化粘贴进来的富文本：去掉字体/字号/颜色等噪声，只保留语义化标签
function cleanPastedHtml(html) {
  let s = String(html || '')
  s = s.replace(/<\s*(script|style|iframe|object|embed|link|meta)\b[^>]*>[\s\S]*?<\s*\/\s*\1\s*>/gi, '')
  s = s.replace(/<\s*(script|style|iframe|object|embed|link|meta)\b[^>]*\/?>/gi, '')
  s = s.replace(/\son\w+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)/gi, '')
  s = s.replace(/(href|src)\s*=\s*"javascript:[^"]*"/gi, '')
  s = s.replace(/<font\b[^>]*>/gi, '').replace(/<\/font>/gi, '')
  // 去掉 style 上的字体/字号/颜色/背景（保留 text-align、padding 等结构性样式）
  s = s.replace(/\sstyle="([^"]*)"/gi, (m, body) => {
    const keep = body.split(';').map(s => s.trim()).filter(Boolean).filter((p) => {
      const k = p.split(':')[0].trim().toLowerCase()
      return ['text-align','padding','padding-left','padding-right','margin','margin-left','margin-right'].includes(k)
    })
    return keep.length ? ' style="' + keep.join(';') + ';)' : ''
  })
  s = s.replace(/\sclass="[^"]*"/gi, '')
  return s
}
function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]))
}
function escapeAttr(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]))
}

defineExpose({ focus: () => editable.value && editable.value.focus(), clear: () => { if (editable.value) editable.value.innerHTML = ''; onInput() } })
</script>

<style scoped>
.rte { border: 1px solid var(--glass-border); border-radius: 12px; background: var(--bg-1); display: flex; flex-direction: column; }
.rte-toolbar { display: flex; flex-wrap: wrap; gap: 4px; padding: 6px 8px; border-bottom: 1px solid var(--hairline); background: var(--overlay); border-top-left-radius: 12px; border-top-right-radius: 12px; }
.t-btn { padding: 4px 9px; min-height: 28px; border-radius: 7px; border: 1px solid transparent; background: transparent; color: var(--text-dim); cursor: pointer; font-size: 12.5px; line-height: 1.2; }
.t-btn:hover { background: var(--overlay-2); color: var(--text); }
.t-btn.on { background: var(--accent-soft, rgba(79,70,229,0.12)); color: var(--accent); border-color: rgba(79,70,229,0.3); }
.t-btn:disabled { opacity: 0.4; cursor: not-allowed; }
.t-btn.danger:hover { color: var(--danger); }
.t-sep { width: 1px; background: var(--glass-border); margin: 4px 4px; }
.rte-body {
  min-height: 180px; max-height: 480px; overflow-y: auto;
  padding: 14px 16px; line-height: 1.7; font-size: 14px;
  color: var(--text); outline: none;
  border-bottom-left-radius: 12px; border-bottom-right-radius: 12px;
}
.rte-body:empty::before { content: attr(data-placeholder); color: var(--text-faint); pointer-events: none; }
.rte-body :deep(h2) { font-size: 18px; font-weight: 700; margin: 14px 0 8px; }
.rte-body :deep(h3) { font-size: 16px; font-weight: 700; margin: 12px 0 6px; }
.rte-body :deep(p) { margin: 6px 0; }
.rte-body :deep(ul), .rte-body :deep(ol) { padding-left: 22px; margin: 6px 0; }
.rte-body :deep(blockquote) { border-left: 3px solid var(--accent); padding-left: 10px; color: var(--text-dim); margin: 8px 0; }
.rte-body :deep(a) { color: var(--accent); text-decoration: underline; }
.rte-body :deep(img) { max-width: 100%; height: auto; border-radius: 6px; margin: 4px 0; cursor: pointer; }
.rte-body :deep(hr) { border: 0; border-top: 1px dashed var(--glass-border); margin: 12px 0; }
.rte-body :deep(pre) { background: var(--overlay-2); padding: 8px 10px; border-radius: 8px; font-size: 12.5px; overflow-x: auto; }
.rte-body :deep(code) { background: var(--overlay-2); padding: 1px 5px; border-radius: 4px; font-size: 12.5px; }
.rte-foot { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 6px 12px; border-top: 1px solid var(--hairline); font-size: 11.5px; color: var(--text-faint); }
.rte-tip { flex: 1; min-width: 0; }
.rte-count { white-space: nowrap; }
.rte-disabled .rte-body { background: var(--overlay); color: var(--text-dim); cursor: not-allowed; }
</style>