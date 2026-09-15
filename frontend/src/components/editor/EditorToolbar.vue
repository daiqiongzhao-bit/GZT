<template>
  <div class="rte-toolbar">
    <!-- ========== ① 文本组 ========== -->
    <button type="button" class="t-btn" :class="{ on: is('bold') }" title="加粗 (Ctrl+B)" @mousedown.prevent="cmd('toggleBold')"><b>B</b></button>
    <button type="button" class="t-btn" :class="{ on: is('italic') }" title="斜体 (Ctrl+I)" @mousedown.prevent="cmd('toggleItalic')"><i>I</i></button>
    <button type="button" class="t-btn" :class="{ on: is('underline') }" title="下划线 (Ctrl+U)" @mousedown.prevent="cmd('toggleUnderline')"><u>U</u></button>
    <button type="button" class="t-btn" :class="{ on: is('strike') }" title="删除线 (Ctrl+Shift+S)" @mousedown.prevent="cmd('toggleStrike')"><s>S</s></button>
    <button type="button" class="t-btn" :class="{ on: is('superscript') }" title="上标 (Ctrl+Shift+↑)" @mousedown.prevent="cmd('toggleSuperscript')">x²</button>
    <button type="button" class="t-btn" :class="{ on: is('subscript') }" title="下标 (Ctrl+Shift+↓)" @mousedown.prevent="cmd('toggleSubscript')">x₂</button>
    <button type="button" class="t-btn" :class="{ on: is('code') }" title="行内代码 (Ctrl+E)" @mousedown.prevent="cmd('toggleCode')">⟨⟩</button>

    <span class="t-sep"></span>

    <!-- 字号 -->
    <div class="t-select-wrap">
      <select class="t-select" :value="currentFontSize" title="字号" @change="onFontSize" @mousedown.stop>
        <option value="">字号</option>
        <option v-for="s in FONT_SIZE_OPTIONS" :key="s" :value="s">{{ parseInt(s) }}</option>
      </select>
    </div>

    <!-- 字体 -->
    <div class="t-select-wrap">
      <select class="t-select" :value="currentFontFamily" title="字体" @change="onFontFamily" @mousedown.stop>
        <option v-for="f in FONT_FAMILY_OPTIONS" :key="f.value" :value="f.value">{{ f.label }}</option>
      </select>
    </div>

    <!-- 文字颜色 -->
    <div class="t-color-wrap" @mousedown.stop>
      <button type="button" class="t-btn t-color-btn" title="文字颜色" @click.stop="toggleColorPanel('text')">
        <span class="t-a">A</span>
        <span class="t-color-bar" :style="{ background: currentColor || 'var(--text)' }"></span>
      </button>
      <transition name="pop">
        <div v-if="colorPanel === 'text'" class="t-color-panel" @click.stop>
          <div class="tcp-title">文字颜色</div>
          <div class="tcp-grid">
            <button
              v-for="c in TEXT_COLOR_PRESETS" :key="c" type="button" class="tcp-swatch"
              :style="{ background: c }" :title="c" @click="setTextColor(c)"
            ></button>
          </div>
          <div class="tcp-row">
            <input type="color" class="tcp-native" :value="currentColor || '#0f172a'" @input="setTextColor($event.target.value)" />
            <button type="button" class="tcp-reset" @click="unsetTextColor">清除颜色</button>
          </div>
        </div>
      </transition>
    </div>

    <!-- 高亮 -->
    <div class="t-color-wrap" @mousedown.stop>
      <button type="button" class="t-btn" :class="{ on: is('highlight') }" title="背景高亮" @click.stop="toggleColorPanel('highlight')">
        <span class="t-hl">🖍</span>
      </button>
      <transition name="pop">
        <div v-if="colorPanel === 'highlight'" class="t-color-panel" @click.stop>
          <div class="tcp-title">背景高亮</div>
          <div class="tcp-grid">
            <button
              v-for="c in HIGHLIGHT_PRESETS" :key="c" type="button" class="tcp-swatch"
              :style="{ background: c }" :title="c" @click="setHighlight(c)"
            ></button>
          </div>
          <div class="tcp-row">
            <button type="button" class="tcp-reset" @click="unsetHighlight">清除高亮</button>
          </div>
        </div>
      </transition>
    </div>

    <button type="button" class="t-btn danger" title="清除格式" @mousedown.prevent="clearFormat">⌫ 清除</button>

    <span class="t-sep"></span>

    <!-- ========== ② 段落组 ========== -->
    <button type="button" class="t-btn" :class="{ on: is('heading', { level: 1 }) }" title="标题 1 (Ctrl+Alt+1)" @mousedown.prevent="cmd('toggleHeading', { level: 1 })">H1</button>
    <button type="button" class="t-btn" :class="{ on: is('heading', { level: 2 }) }" title="标题 2 (Ctrl+Alt+2)" @mousedown.prevent="cmd('toggleHeading', { level: 2 })">H2</button>
    <button type="button" class="t-btn" :class="{ on: is('heading', { level: 3 }) }" title="标题 3 (Ctrl+Alt+3)" @mousedown.prevent="cmd('toggleHeading', { level: 3 })">H3</button>
    <button type="button" class="t-btn" :class="{ on: is('heading', { level: 4 }) }" title="标题 4 (Ctrl+Alt+4)" @mousedown.prevent="cmd('toggleHeading', { level: 4 })">H4</button>
    <button type="button" class="t-btn" :class="{ on: is('paragraph') }" title="正文 (Ctrl+Alt+0)" @mousedown.prevent="cmd('setParagraph')">P</button>

    <span class="t-sep"></span>

    <button type="button" class="t-btn" :class="{ on: is({ textAlign: 'left' }) }" title="左对齐 (Ctrl+L)" @mousedown.prevent="cmd('setTextAlign', 'left')">⇤</button>
    <button type="button" class="t-btn" :class="{ on: is({ textAlign: 'center' }) }" title="居中 (Ctrl+E)" @mousedown.prevent="cmd('setTextAlign', 'center')">↔</button>
    <button type="button" class="t-btn" :class="{ on: is({ textAlign: 'right' }) }" title="右对齐 (Ctrl+R)" @mousedown.prevent="cmd('setTextAlign', 'right')">⇥</button>
    <button type="button" class="t-btn" :class="{ on: is({ textAlign: 'justify' }) }" title="两端对齐 (Ctrl+J)" @mousedown.prevent="cmd('setTextAlign', 'justify')">☰</button>

    <span class="t-sep"></span>

    <button type="button" class="t-btn" title="增加缩进 (Tab)" @mousedown.prevent="cmd('indent')">⇥ 缩进</button>
    <button type="button" class="t-btn" title="减少缩进 (Shift+Tab)" @mousedown.prevent="cmd('outdent')">⇤ 减少</button>

    <!-- 行距 -->
    <div class="t-select-wrap">
      <select class="t-select" :value="currentLineHeight" title="行距" @change="onLineHeight" @mousedown.stop>
        <option value="">行距</option>
        <option v-for="l in LINE_HEIGHT_OPTIONS" :key="l.value" :value="l.value">{{ l.label }}</option>
      </select>
    </div>

    <span class="t-sep"></span>

    <!-- ========== ③ 块组 ========== -->
    <button type="button" class="t-btn" :class="{ on: is('bulletList') }" title="无序列表 (Ctrl+Shift+8)" @mousedown.prevent="cmd('toggleBulletList')">• 列表</button>
    <button type="button" class="t-btn" :class="{ on: is('orderedList') }" title="有序列表 (Ctrl+Shift+7)" @mousedown.prevent="cmd('toggleOrderedList')">1. 列表</button>
    <button type="button" class="t-btn" :class="{ on: is('blockquote') }" title="引用 (Ctrl+Shift+B)" @mousedown.prevent="cmd('toggleBlockquote')">❝ 引用</button>
    <button type="button" class="t-btn" :class="{ on: is('codeBlock') }" title="代码块 (Ctrl+Alt+C)" @mousedown.prevent="cmd('toggleCodeBlock')">▤ 代码块</button>
    <button type="button" class="t-btn" title="分割线" @mousedown.prevent="cmd('setHorizontalRule')">―</button>

    <span class="t-sep"></span>

    <!-- ========== ④ 插入组 ========== -->
    <button type="button" class="t-btn" :class="{ on: is('link') }" title="插入/编辑超链接 (Ctrl+K)" @mousedown.prevent="openLinkDialog">🔗 链接</button>

    <button type="button" class="t-btn" title="插入图片（或直接粘贴/拖拽）" @mousedown.prevent="pickImage">🖼 图片</button>
    <input ref="fileInput" type="file" accept="image/*" multiple hidden @change="onFilePick" />

    <!-- 表格：仅光标在表格内时展开操作 -->
    <div class="t-table-wrap" @mousedown.stop>
      <button type="button" class="t-btn" :title="inTable ? '表格操作' : '插入表格'" @click.stop="toggleTablePanel">
        ▦ 表格<template v-if="inTable"> ▾</template>
      </button>
      <transition name="pop">
        <div v-if="tablePanel" class="t-menu" @click.stop>
          <template v-if="!inTable">
            <button class="menu-item" @click="insertTable(3, 3)">插入 3×3 表格</button>
            <button class="menu-item" @click="insertTable(4, 4)">插入 4×4 表格</button>
            <button class="menu-item" @click="insertTable(5, 6)">插入 5×6 表格</button>
          </template>
          <template v-else>
            <button class="menu-item" @click="cmd('addRowAfter')">下方插入行</button>
            <button class="menu-item" @click="cmd('addRowBefore')">上方插入行</button>
            <button class="menu-item" @click="cmd('deleteRow')">删除当前行</button>
            <button class="menu-item" @click="cmd('addColumnAfter')">右侧插入列</button>
            <button class="menu-item" @click="cmd('addColumnBefore')">左侧插入列</button>
            <button class="menu-item" @click="cmd('deleteColumn')">删除当前列</button>
            <span class="menu-div"></span>
            <button class="menu-item" @click="cmd('mergeCells')">合并单元格</button>
            <button class="menu-item" @click="cmd('splitCell')">拆分单元格</button>
            <span class="menu-div"></span>
            <button class="menu-item" @click="cmd('toggleHeaderRow')">切换表头行</button>
            <button class="menu-item" @click="cmd('toggleHeaderColumn')">切换表头列</button>
            <span class="menu-div"></span>
            <button class="menu-item danger" @click="cmd('deleteTable')">删除整个表格</button>
          </template>
        </div>
      </transition>
    </div>

    <span class="t-sep"></span>

    <button type="button" class="t-btn" :disabled="!canUndo" title="撤销 (Ctrl+Z)" @mousedown.prevent="cmd('undo')">↶</button>
    <button type="button" class="t-btn" :disabled="!canRedo" title="重做 (Ctrl+Y)" @mousedown.prevent="cmd('redo')">↷</button>

    <span class="t-sep"></span>

    <button type="button" class="t-btn" :class="{ on: findOpen }" title="查找替换 (Ctrl+F)" @mousedown.prevent="$emit('toggle-find')">🔍 查找</button>
  </div>
</template>

<script setup>
/**
 * EditorToolbar.vue —— 编辑器工具栏（v0.28.0）
 *
 * 【布局】四组：文本 / 段落 / 块 / 插入，组间用竖线分隔，窄屏自动换行。
 * 有下拉的按钮（字号/字体/行距/颜色/高亮/表格）收进面板，避免按钮墙。
 *
 * 【状态高亮】用 editor.isActive() 而非旧实现的 document.queryCommandState()——
 * 后者在空选区/嵌套结构下返回不准，会导致工具栏高亮"粘住"。
 */
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import * as api from '@/api'
import {
  FONT_SIZE_OPTIONS, FONT_FAMILY_OPTIONS, LINE_HEIGHT_OPTIONS,
  TEXT_COLOR_PRESETS, HIGHLIGHT_PRESETS,
} from './extensions'

const props = defineProps({
  editor: { type: Object, required: true },
  findOpen: { type: Boolean, default: false },
})
const emit = defineEmits(['toggle-find'])

const fileInput = ref(null)
const colorPanel = ref('') // '' | 'text' | 'highlight'
const tablePanel = ref(false)

// ---------- 状态查询 ----------
/** is('bold') 或 is({ textAlign: 'center' }) 两种调用形态 */
function is(name, attrs) {
  const ed = props.editor
  if (!ed) return false
  try {
    if (attrs) return ed.isActive(name, attrs)
    return ed.isActive(name)
  } catch {
    return false
  }
}

/** 当前光标所在位置的属性值（用于下拉回显） */
function activeAttrs() {
  const ed = props.editor
  if (!ed) return {}
  try {
    return ed.getAttributes('textStyle') || {}
  } catch {
    return {}
  }
}

const currentFontSize = computed(() => activeAttrs().fontSize || '')
const currentFontFamily = computed(() => activeAttrs().fontFamily || '')
const currentColor = computed(() => activeAttrs().color || '')
const currentLineHeight = computed(() => activeAttrs().lineHeight || '')

const inTable = computed(() => is('table'))
const canUndo = computed(() => {
  const ed = props.editor
  if (!ed || !ed.can) return true
  try { return ed.can().undo() } catch { return true }
})
const canRedo = computed(() => {
  const ed = props.editor
  if (!ed || !ed.can) return true
  try { return ed.can().redo() } catch { return true }
})

// ---------- 命令执行 ----------
/**
 * 链式执行命令并保持焦点。
 * 注意：必须 chain().focus() 后再执行，否则点击工具栏会让编辑器失焦、
 * 导致命令作用在错误位置（旧实现用 @mousedown.prevent 缓解，这里是双保险）。
 */
function cmd(name, ...args) {
  const ed = props.editor
  if (!ed) return
  const chain = ed.chain().focus()
  if (typeof chain[name] === 'function') {
    chain[name](...args).run()
  }
  colorPanel.value = ''
  tablePanel.value = false
}

function clearFormat() {
  const ed = props.editor
  if (!ed) return
  ed.chain().focus().unsetAllMarks().clearNodes().run()
}

// ---------- 字号 / 字体 / 行距 ----------
function onFontSize(e) {
  const v = e.target.value
  const ed = props.editor
  if (!ed) return
  if (!v) {
    ed.chain().focus().unsetFontSize().run()
  } else {
    ed.chain().focus().setFontSize(v).run()
  }
}

function onFontFamily(e) {
  const v = e.target.value
  const ed = props.editor
  if (!ed) return
  if (!v) ed.chain().focus().unsetFontFamily().run()
  else ed.chain().focus().setFontFamily(v).run()
}

function onLineHeight(e) {
  const v = e.target.value
  const ed = props.editor
  if (!ed) return
  if (!v) ed.chain().focus().unsetLineHeight().run()
  else ed.chain().focus().setLineHeight(v).run()
}

// ---------- 颜色 ----------
function toggleColorPanel(which) {
  colorPanel.value = colorPanel.value === which ? '' : which
}
function setTextColor(c) {
  const ed = props.editor
  if (ed) ed.chain().focus().setColor(c).run()
  colorPanel.value = ''
}
function unsetTextColor() {
  const ed = props.editor
  if (ed) ed.chain().focus().unsetColor().run()
  colorPanel.value = ''
}
function setHighlight(c) {
  const ed = props.editor
  if (ed) ed.chain().focus().setHighlight({ color: c }).run()
  colorPanel.value = ''
}
function unsetHighlight() {
  const ed = props.editor
  if (ed) ed.chain().focus().unsetHighlight().run()
  colorPanel.value = ''
}

// ---------- 链接 ----------
function openLinkDialog() {
  const ed = props.editor
  if (!ed) return
  const prev = ed.getAttributes('link').href || ''
  const url = window.prompt('请输入链接地址（http(s):// 开头，或以 / 开头表示站内路径）：', prev || 'https://')
  if (url === null) return
  const val = String(url).trim()
  if (!val) {
    ed.chain().focus().extendMarkRange('link').unsetLink().run()
    return
  }
  if (!/^https?:\/\//i.test(val) && !val.startsWith('/')) {
    window.alert('链接必须以 http(s):// 开头，或以 / 开头表示站内路径')
    return
  }
  ed.chain().focus().extendMarkRange('link').setLink({ href: val }).run()
}

// ---------- 表格 ----------
function toggleTablePanel() {
  if (!inTable.value) {
    // 不在表格内 → 先插入 3×3 再展开（与旧行为一致：点一次就有表格）
    insertTable(3, 3)
    tablePanel.value = true
    return
  }
  tablePanel.value = !tablePanel.value
}
function insertTable(rows, cols) {
  const ed = props.editor
  if (!ed) return
  ed.chain().focus().insertTable({ rows, cols, withHeaderRow: true }).run()
  tablePanel.value = false
}

// ---------- 图片 ----------
function pickImage() {
  fileInput.value && fileInput.value.click()
}
async function onFilePick(e) {
  const files = Array.from(e.target.files || [])
  e.target.value = ''
  const ed = props.editor
  if (!ed) return
  for (const f of files) {
    try {
      // 复用组件注入的上传函数（通过 editor 的 PasteImageUpload 扩展选项）
      const uploadFn = ed.extensionManager.extensions.find((x) => x.name === 'pasteImageUpload')?.options?.upload
      if (!uploadFn) continue
      const u = await uploadFn(f)
      if (!u || !u.dl) continue
      const attrs = { src: u.dl, alt: u.name || 'image', 'data-att-name': u.name || '' }
      if (u.temp) attrs['data-temp-id'] = String(u.id)
      else attrs['data-att-id'] = String(u.id)
      ed.chain().focus().insertContent({ type: 'image', attrs }).run()
    } catch (err) {
      const onErr = ed.extensionManager.extensions.find((x) => x.name === 'pasteImageUpload')?.options?.onError
      if (onErr) onErr(err)
    }
  }
}

// ---------- 点击外部关闭面板 ----------
function onDocClick(e) {
  if (e.target && e.target.closest && (e.target.closest('.t-color-wrap') || e.target.closest('.t-table-wrap'))) return
  colorPanel.value = ''
  tablePanel.value = false
}
onMounted(() => document.addEventListener('click', onDocClick, true))
onBeforeUnmount(() => document.removeEventListener('click', onDocClick, true))
</script>

<style scoped>
.rte-toolbar {
  display: flex; flex-wrap: wrap; align-items: center; gap: 3px;
  padding: 6px 8px; border-bottom: 1px solid var(--hairline);
  background: var(--overlay); border-top-left-radius: 12px; border-top-right-radius: 12px;
}
.t-btn {
  padding: 4px 8px; min-height: 28px; border-radius: 7px; border: 1px solid transparent;
  background: transparent; color: var(--text-dim); cursor: pointer; font-size: 12.5px; line-height: 1.2;
  white-space: nowrap;
}
.t-btn:hover { background: var(--overlay-2); color: var(--text); }
.t-btn.on { background: var(--accent-soft, rgba(79,70,229,0.12)); color: var(--accent); border-color: var(--accent); }
.t-btn:disabled { opacity: 0.35; cursor: not-allowed; }
.t-btn.danger:hover { color: var(--danger); }
.t-sep { width: 1px; height: 18px; background: var(--glass-border); margin: 0 4px; flex: 0 0 auto; }

.t-select-wrap { position: relative; }
.t-select {
  height: 28px; padding: 0 4px; border-radius: 7px; border: 1px solid var(--glass-border);
  background: transparent; color: var(--text-dim); font-size: 12px; cursor: pointer; max-width: 96px;
}
.t-select:hover { color: var(--text); border-color: var(--accent); }
.t-select option { background: var(--bg-1); color: var(--text); }

/* 颜色按钮 */
.t-color-wrap { position: relative; }
.t-color-btn { display: inline-flex; flex-direction: column; align-items: center; gap: 0; padding: 2px 6px; }
.t-a { font-size: 13px; font-weight: 700; line-height: 1; }
.t-color-bar { display: block; width: 16px; height: 3px; border-radius: 2px; margin-top: 1px; }
.t-hl { font-size: 13px; }

.t-color-panel {
  position: absolute; top: 34px; left: 0; z-index: 30; width: 190px;
  padding: 10px; border-radius: 12px;
  background: var(--glass-strong); backdrop-filter: blur(16px);
  border: 1px solid var(--glass-border-strong); box-shadow: var(--shadow);
}
.tcp-title { font-size: 11.5px; color: var(--text-faint); margin-bottom: 7px; }
.tcp-grid { display: grid; grid-template-columns: repeat(8, 1fr); gap: 4px; }
.tcp-swatch { width: 18px; height: 18px; border-radius: 5px; border: 1px solid var(--glass-border); cursor: pointer; padding: 0; }
.tcp-swatch:hover { transform: scale(1.12); }
.tcp-row { display: flex; align-items: center; gap: 8px; margin-top: 8px; }
.tcp-native { width: 32px; height: 24px; padding: 0; border: 1px solid var(--glass-border); border-radius: 6px; background: transparent; cursor: pointer; }
.tcp-reset { flex: 1; padding: 4px; font-size: 11.5px; border-radius: 6px; border: 1px solid var(--glass-border); background: transparent; color: var(--text-dim); cursor: pointer; }
.tcp-reset:hover { color: var(--text); border-color: var(--accent); }

/* 表格菜单 */
.t-table-wrap { position: relative; }
.t-menu {
  position: absolute; top: 34px; left: 0; z-index: 30; min-width: 168px;
  padding: 5px; border-radius: 12px;
  background: var(--glass-strong); backdrop-filter: blur(16px);
  border: 1px solid var(--glass-border-strong); box-shadow: var(--shadow);
}
.menu-item {
  display: block; width: 100%; text-align: left; padding: 6px 10px; border: 0; border-radius: 8px;
  background: transparent; color: var(--text-dim); font-size: 12.5px; cursor: pointer;
}
.menu-item:hover { background: var(--overlay-2); color: var(--text); }
.menu-item.danger:hover { color: var(--danger); }
.menu-div { display: block; height: 1px; background: var(--hairline); margin: 4px 6px; }

.pop-enter-active, .pop-leave-active { transition: opacity 0.14s ease, transform 0.14s ease; }
.pop-enter-from, .pop-leave-to { opacity: 0; transform: translateY(-4px); }
</style>
