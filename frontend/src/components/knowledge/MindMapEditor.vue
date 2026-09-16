<template>
  <div class="mm-wrap">
    <div class="mm-bar">
      <span class="mm-hint">{{ readonly ? '只读预览' : 'Tab 加子节点 · Enter 加同级 · F2 改文字 · 拖拽换父级 · 右键更多' }}</span>
      <span class="mm-count">{{ nodeCount }} 个节点</span>
    </div>
    <div ref="host" class="mm-host"></div>
  </div>
</template>

<script setup>
/**
 * 思维导图编辑器（mind-elixir 封装）
 *
 * - v-model 绑定的是导图数据 JSON 字符串，与后端 KnowledgeEntry.Content 同构，
 *   父组件（KnowledgeDetail）按 kind === 'mind' 直接渲染本组件即可。
 * - 画布固定浅色底：导出 PNG/PDF 时白底最清晰，深色主题下分支线也不会看不见。
 */
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
import MindElixir from 'mind-elixir'
import { zh_CN } from 'mind-elixir/i18n'
import 'mind-elixir/style.css'

const props = defineProps({
  modelValue: { type: String, default: '' },
  readonly: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue', 'dirty'])

const host = ref(null)
const nodeCount = ref(0)

let mind = null
let lastEmitted = ''
let timer = null

// 空数据用库自带的静态工厂生成，避免手写结构踩版本差异
function blank() {
  return MindElixir.new('中心主题')
}

function parse(raw) {
  const s = String(raw || '').trim()
  if (!s) return null
  try {
    const d = JSON.parse(s)
    return d && d.nodeData ? d : null
  } catch (e) {
    return null
  }
}

// 各版本取数据的方法名略有差异，逐级兜底，保证回写永远是完整数据
function readData() {
  if (!mind) return null
  try {
    if (typeof mind.getData === 'function') return mind.getData()
  } catch (e) { /* 落到下面的字段拼装 */ }
  return {
    nodeData: mind.nodeData,
    arrows: mind.arrows || [],
    summaries: mind.summaries || [],
    direction: mind.direction,
    theme: mind.theme,
    linkData: {},
  }
}

function countNodes(n) {
  if (!n) return 0
  let c = 1
  for (const ch of (n.children || [])) c += countNodes(ch)
  return c
}

function syncOut() {
  if (!mind || props.readonly) return
  const data = readData()
  if (!data) return
  nodeCount.value = countNodes(data.nodeData)
  const s = JSON.stringify(data)
  if (s === lastEmitted) return
  lastEmitted = s
  emit('update:modelValue', s)
  emit('dirty', true)
}

function scheduleSync() {
  clearTimeout(timer)
  timer = setTimeout(syncOut, 300)
}

function initFrom(raw) {
  if (!mind) return
  const err = mind.init(parse(raw) || blank())
  if (err) console.warn('[思维导图] 数据解析失败，已回退为空白图', err)
  lastEmitted = String(raw || '')
  const d = readData()
  nodeCount.value = countNodes(d && d.nodeData)
}

onMounted(() => {
  mind = new MindElixir({
    el: host.value,
    direction: MindElixir.RIGHT,
    editable: !props.readonly,
    contextMenu: props.readonly ? false : { locale: zh_CN },
    toolBar: !props.readonly,
    keypress: !props.readonly,
    allowUndo: true,
    overflowHidden: false,
    theme: MindElixir.THEME,
  })
  initFrom(props.modelValue)
  if (!props.readonly) mind.bus.addListener('operation', scheduleSync)
})

onBeforeUnmount(() => {
  clearTimeout(timer)
  try { if (mind) mind.destroy() } catch (e) { /* 卸载异常不影响页面 */ }
  mind = null
})

watch(() => props.modelValue, (v) => {
  if (!mind) return
  if (String(v || '') === lastEmitted) return // 自己刚回写的，忽略，避免重置视图
  initFrom(v)
})

/**
 * 导出为 Blob（供父组件走统一导出工具）
 * @param {'png'|'svg'} fileType
 */
async function exportBlob(fileType = 'png') {
  if (!mind) return null
  // svg：保留完整样式（含 HTML 节点）供再编辑；png：走纯 SVG 文本渲染，canvas 转换最稳
  if (fileType === 'svg') return mind.exportSvg(false)
  return await mind.exportPng(true)
}

defineExpose({ exportBlob, getData: readData })
</script>

<style scoped>
.mm-wrap { display: flex; flex-direction: column; gap: 8px; }
.mm-bar {
  display: flex; align-items: center; justify-content: space-between; gap: 10px;
  font-size: 12px; color: var(--text-dim);
}
.mm-hint { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mm-count { flex: 0 0 auto; color: var(--text-faint); }
.mm-host {
  height: 460px; position: relative; overflow: hidden;
  border: 1px solid var(--hairline); border-radius: 10px; background: #fff;
}
</style>
