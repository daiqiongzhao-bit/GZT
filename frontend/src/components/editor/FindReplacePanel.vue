<template>
  <transition name="fr-slide">
    <div v-if="open" class="fr-panel" role="dialog" aria-label="查找替换">
      <div class="fr-row">
        <input
          ref="termInput"
          v-model="term"
          class="fr-input"
          placeholder="查找内容"
          @keydown.enter.prevent="findNext"
          @keydown.esc.prevent="close"
        />
        <span class="fr-count">{{ countText }}</span>
        <button type="button" class="fr-btn" :disabled="!hasMatches" title="上一个" @click="findPrev">↑</button>
        <button type="button" class="fr-btn" :disabled="!hasMatches" title="下一个" @click="findNext">↓</button>
        <button type="button" class="fr-btn close" title="关闭 (Esc)" @click="close">✕</button>
      </div>
      <div class="fr-row">
        <input
          v-model="replace"
          class="fr-input"
          placeholder="替换为"
          @keydown.enter.prevent="doReplaceCurrent"
          @keydown.esc.prevent="close"
        />
        <button type="button" class="fr-btn wide" :disabled="!hasMatches" @click="doReplaceCurrent">替换</button>
        <button type="button" class="fr-btn wide" :disabled="!hasMatches" @click="doReplaceAll">全部替换</button>
      </div>
      <div class="fr-row opts">
        <label class="fr-check"><input v-model="caseSensitive" type="checkbox" @change="syncOptions" /> 区分大小写</label>
        <label class="fr-check"><input v-model="wholeWord" type="checkbox" @change="syncOptions" /> 全字匹配</label>
        <span class="fr-hint">Enter 下一个 · Esc 关闭</span>
      </div>
    </div>
  </transition>
</template>

<script setup>
/**
 * FindReplacePanel.vue —— 查找替换面板（v0.28.0，Word 常用）
 *
 * 【实现】复用 FindReplace 扩展的 commands（setSearchTerm / findNext / replaceAll 等），
 * 高亮由扩展的 ProseMirror Decoration 完成（纯视觉，不落库）。
 *
 * 【快捷键】Enter=下一个，Esc=关闭；Ctrl+F 由 RichTextEditor 在编辑器聚焦时拦截。
 */
import { ref, computed, watch, nextTick } from 'vue'

const props = defineProps({
  open: { type: Boolean, default: false },
  editor: { type: Object, default: null },
  state: { type: Object, default: () => ({ matches: [], current: 0 }) },
})
const emit = defineEmits(['update:open'])

const termInput = ref(null)
const term = ref('')
const replace = ref('')
const caseSensitive = ref(false)
const wholeWord = ref(false)

const hasMatches = computed(() => (props.state?.matches?.length || 0) > 0)
const countText = computed(() => {
  const m = props.state?.matches?.length || 0
  if (!term.value) return ''
  if (!m) return '无结果'
  return `${(props.state.current || 0) + 1}/${m}`
})

// 打开时聚焦输入框并同步已有词
watch(
  () => props.open,
  async (v) => {
    if (!v) return
    await nextTick()
    termInput.value && termInput.value.focus()
    if (props.state?.term && props.state.term !== term.value) term.value = props.state.term
  },
  { immediate: true },
)

// 输入即查（实时高亮）
watch(term, (v) => {
  const ed = props.editor
  if (!ed) return
  ed.commands.setSearchTerm(v)
})

function syncOptions() {
  const ed = props.editor
  if (!ed) return
  ed.commands.setSearchCaseSensitive(caseSensitive.value)
  ed.commands.setSearchWholeWord(wholeWord.value)
}

function findNext() {
  props.editor && props.editor.commands.findNext()
}
function findPrev() {
  props.editor && props.editor.commands.findPrev()
}
function doReplaceCurrent() {
  props.editor && props.editor.commands.setReplaceTerm(replace.value)
  props.editor && props.editor.commands.replaceCurrent()
}
function doReplaceAll() {
  props.editor && props.editor.commands.setReplaceTerm(replace.value)
  props.editor && props.editor.commands.replaceAll()
}
function close() {
  emit('update:open', false)
  props.editor && props.editor.commands.clearSearch()
}
</script>

<style scoped>
.fr-panel {
  position: absolute; top: 46px; right: 14px; z-index: 35;
  width: min(420px, calc(100% - 28px));
  padding: 10px 12px; border-radius: 14px;
  background: var(--glass-strong); backdrop-filter: blur(18px);
  border: 1px solid var(--glass-border-strong); box-shadow: var(--shadow);
}
.fr-row { display: flex; align-items: center; gap: 6px; }
.fr-row + .fr-row { margin-top: 7px; }
.fr-row.opts { gap: 14px; }
.fr-input {
  flex: 1; min-width: 0; height: 30px; padding: 0 10px; font-size: 13px;
  border-radius: 8px; border: 1px solid var(--glass-border);
  background: var(--overlay); color: var(--text); outline: none;
}
.fr-input:focus { border-color: var(--accent); }
.fr-count { font-size: 11.5px; color: var(--text-faint); white-space: nowrap; min-width: 46px; text-align: right; }
.fr-btn {
  min-width: 30px; height: 30px; padding: 0 8px; border-radius: 8px;
  border: 1px solid var(--glass-border); background: transparent;
  color: var(--text-dim); cursor: pointer; font-size: 12.5px;
}
.fr-btn:hover:not(:disabled) { color: var(--text); border-color: var(--accent); }
.fr-btn:disabled { opacity: 0.35; cursor: not-allowed; }
.fr-btn.wide { min-width: 62px; }
.fr-btn.close:hover { color: var(--danger); border-color: var(--danger); }
.fr-check { display: inline-flex; align-items: center; gap: 5px; font-size: 12px; color: var(--text-dim); cursor: pointer; }
.fr-check input { cursor: pointer; }
.fr-hint { margin-left: auto; font-size: 11px; color: var(--text-faint); }

.fr-slide-enter-active, .fr-slide-leave-active { transition: opacity 0.16s ease, transform 0.16s ease; }
.fr-slide-enter-from, .fr-slide-leave-to { opacity: 0; transform: translateY(-6px); }
</style>
