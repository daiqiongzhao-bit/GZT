<template>
  <div class="fl-wrap">
    <div class="fl-bar">
      <div class="fl-tools">
        <button class="fl-btn" :disabled="readonly" title="撤销" @click="undo">↶</button>
        <button class="fl-btn" :disabled="readonly" title="重做" @click="redo">↷</button>
        <span class="fl-sep"></span>
        <button class="fl-btn" title="放大" @click="zoomIn">＋</button>
        <button class="fl-btn" title="缩小" @click="zoomOut">－</button>
        <button class="fl-btn" title="适应画布" @click="fit">⤢</button>
        <span class="fl-sep"></span>
        <button class="fl-btn danger" :disabled="readonly" title="清空画布" @click="clearAll">清空</button>
      </div>
      <span class="fl-count">图形 {{ count }} 个</span>
    </div>

    <div class="fl-body">
      <aside v-if="!readonly" class="fl-palette">
        <div v-for="g in palette" :key="g.name" class="fl-group">
          <div class="fl-group-title">{{ g.name }}</div>
          <button
            v-for="it in g.items"
            :key="g.name + it.text"
            class="fl-item"
            :title="'添加：' + it.text"
            @click="addNode(it)"
          >{{ it.text }}</button>
        </div>
        <p class="fl-tip">拖拽节点边缘的小圆点即可连线；选中后按 Delete 删除</p>
      </aside>

      <div class="fl-canvas">
        <div ref="host" class="fl-host"></div>
        <!-- 只读态直接盖一层：比逐个关闭交互开关更彻底，且不受库升级影响 -->
        <div v-if="readonly" class="fl-ro"></div>
      </div>
    </div>
  </div>
</template>

<script setup>
/**
 * 流程图编辑器（LogicFlow 封装，Apache-2.0）
 *
 * - v-model 绑定 LogicFlow GraphData 的 JSON 字符串（{ nodes, edges }），与后端 Content 同构。
 * - 调色板是自己用核心 API（addNode）实现的，不依赖 extension 的 DndPanel，
 *   减少对插件内部实现的耦合；导出用 extension 的 Snapshot 插件（纯 SVG→canvas，无 html2canvas）。
 * - 画布固定浅色底，理由同思维导图：导出白底更清晰。
 */
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
import LogicFlow from '@logicflow/core'
import { Snapshot } from '@logicflow/extension'
import '@logicflow/core/lib/style/index.css'
import '@logicflow/extension/lib/style/index.css'

LogicFlow.use(Snapshot)

const props = defineProps({
  modelValue: { type: String, default: '' },
  readonly: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue', 'dirty'])

const host = ref(null)
const count = ref(0)

let lf = null
let lastEmitted = ''
let timer = null

const palette = [
  {
    name: '基本形状',
    items: [
      { type: 'rect', text: '开始 / 结束', props: { radius: 16, style: { radius: 16 } } },
      { type: 'rect', text: '处理' },
      { type: 'diamond', text: '判断' },
      { type: 'ellipse', text: '数据 / 输入' },
      { type: 'circle', text: '连接点' },
      { type: 'text', text: '文字标注', props: { style: { fontSize: 14 } } },
    ],
  },
]

function parse(raw) {
  const s = String(raw || '').trim()
  if (!s) return null
  try {
    const d = JSON.parse(s)
    if (!d || !Array.isArray(d.nodes)) return null
    return { nodes: d.nodes, edges: Array.isArray(d.edges) ? d.edges : [] }
  } catch (e) {
    return null
  }
}

function syncOut() {
  if (!lf || props.readonly) return
  const d = lf.getGraphData() || {}
  const data = { nodes: d.nodes || [], edges: d.edges || [] }
  count.value = data.nodes.length
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

function render(raw) {
  if (!lf) return
  lf.render(parse(raw) || { nodes: [], edges: [] })
  lastEmitted = String(raw || '')
  const d = lf.getGraphData() || {}
  count.value = (d.nodes || []).length
}

onMounted(() => {
  lf = new LogicFlow({
    container: host.value,
    grid: { size: 12, visible: true, type: 'dot', config: { color: '#dde3ec', thickness: 1 } },
    keyboard: { enabled: !props.readonly },
    edgeType: 'polyline',
    adjustEdge: !props.readonly,
    allowRotate: false,
    allowResize: !props.readonly,
    history: true,
    textEdit: { adjustEdge: false },
  })

  // 浅色主题；连线箭头由边主题的 arrow 控制，LogicFlow 默认就会绘制箭头
  lf.setTheme({
    baseNode: { fill: '#ffffff', stroke: '#3d6df0', strokeWidth: 1.6, radius: 6 },
    rect: { fill: '#ffffff', stroke: '#3d6df0', strokeWidth: 1.6, radius: 6 },
    circle: { fill: '#ffffff', stroke: '#3d6df0', strokeWidth: 1.6 },
    ellipse: { fill: '#ffffff', stroke: '#3d6df0', strokeWidth: 1.6 },
    diamond: { fill: '#ffffff', stroke: '#3d6df0', strokeWidth: 1.6 },
    text: { fill: 'transparent', stroke: 'transparent' },
    baseEdge: { stroke: '#5b6673', strokeWidth: 1.6 },
    polyline: { offset: 8, arrow: { offset: 7, verticalLength: 5 } },
    line: { arrow: { offset: 7, verticalLength: 5 } },
    bezier: { arrow: { offset: 7, verticalLength: 5 } },
  })

  if (props.readonly) {
    try {
      lf.updateEditConfig({
        nodeMovable: false,
        edgeMovable: false,
        nodeTextEdit: false,
        edgeTextEdit: false,
        adjustEdge: false,
        adjustNodePosition: false,
        allowRotate: false,
        allowResize: false,
        allowAddEdge: false,
      })
    } catch (e) { /* 老版本没有这些开关，靠上面的遮罩兜底 */ }
  }

  render(props.modelValue)

  const events = [
    'node:add', 'node:delete', 'node:drop', 'node:dragstop', 'node:resize',
    'node:properties-change', 'edge:add', 'edge:delete', 'edge:adjust',
    'text:update', 'history:change',
  ]
  events.forEach((e) => {
    try { lf.on(e, scheduleSync) } catch (err) { /* 事件名不存在时忽略 */ }
  })
})

onBeforeUnmount(() => {
  clearTimeout(timer)
  try { if (lf) lf.destroy() } catch (e) { /* 卸载异常不影响页面 */ }
  lf = null
})

watch(() => props.modelValue, (v) => {
  if (!lf) return
  if (String(v || '') === lastEmitted) return // 自己刚回写的，忽略，避免重置视图
  render(v)
})

function addNode(it) {
  if (!lf || props.readonly) return
  const n = count.value
  const model = lf.addNode({
    type: it.type,
    x: 200 + (n % 4) * 180,
    y: 120 + Math.floor(n / 4) * 120,
    text: it.text,
    properties: it.props ? JSON.parse(JSON.stringify(it.props)) : {},
  })
  if (model && model.id) {
    try { lf.selectElementById(model.id) } catch (e) { /* 选中失败不影响新增 */ }
  }
  syncOut()
}

function undo() { if (lf && !props.readonly) lf.undo() }
function redo() { if (lf && !props.readonly) lf.redo() }
function zoomIn() { if (lf) lf.zoom(true) }
function zoomOut() { if (lf) lf.zoom(false) }
function fit() { if (lf && lf.fitView) lf.fitView(32) }

function clearAll() {
  if (!lf || props.readonly) return
  if (!confirm('确定清空当前流程图的全部节点与连线？')) return
  try {
    if (typeof lf.clearData === 'function') lf.clearData()
    else lf.render({ nodes: [], edges: [] })
  } catch (e) {
    lf.render({ nodes: [], edges: [] })
  }
  syncOut()
}

/**
 * 导出为 Blob（供父组件走统一导出工具）
 * @param {'png'|'svg'|'jpeg'} fileType
 */
async function exportBlob(fileType = 'png') {
  if (!lf) return null
  const opts = { fileType, backgroundColor: '#ffffff', padding: 24, partial: false }
  // 优先用 getSnapshotBlob（只回数据不触发下载）；没有再用 getSnapshot，并显式不传文件名
  const r = typeof lf.getSnapshotBlob === 'function'
    ? await lf.getSnapshotBlob(opts)
    : await lf.getSnapshot(undefined, opts)
  // 快照插件各版本可能回 Blob 或 { data: Blob|string }，这里统一收敛成 Blob
  let raw = r
  if (raw && !(raw instanceof Blob) && raw.data !== undefined) raw = raw.data
  if (raw instanceof Blob) return raw
  if (typeof raw === 'string') {
    const res = await fetch(raw)
    return await res.blob()
  }
  return null
}

defineExpose({ exportBlob })
</script>

<style scoped>
.fl-wrap { display: flex; flex-direction: column; gap: 8px; }
.fl-bar { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.fl-tools { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; }
.fl-sep { width: 1px; height: 16px; background: var(--hairline); margin: 0 4px; }
.fl-btn {
  min-width: 30px; height: 28px; padding: 0 9px; font-size: 13px; line-height: 1;
  color: var(--text); background: var(--overlay-2); border: 1px solid var(--glass-border);
  border-radius: 7px; cursor: pointer;
}
.fl-btn:hover:not(:disabled) { border-color: var(--accent); color: var(--accent); }
.fl-btn:disabled { opacity: .45; cursor: not-allowed; }
.fl-btn.danger:hover:not(:disabled) { border-color: #e5484d; color: #e5484d; }
.fl-count { flex: 0 0 auto; font-size: 12px; color: var(--text-faint); }
.fl-body {
  display: flex; gap: 10px; height: 460px;
  border: 1px solid var(--hairline); border-radius: 10px; overflow: hidden; background: #fff;
}
.fl-palette {
  flex: 0 0 132px; overflow-y: auto; padding: 10px 8px;
  border-right: 1px solid var(--hairline); background: var(--overlay-2);
}
.fl-group + .fl-group { margin-top: 12px; }
.fl-group-title { font-size: 12px; color: var(--text-dim); margin-bottom: 6px; }
.fl-item {
  display: block; width: 100%; margin-bottom: 6px; padding: 7px 8px;
  font-size: 12px; text-align: left; color: var(--text); background: var(--bg-1);
  border: 1px solid var(--glass-border); border-radius: 7px; cursor: pointer;
}
.fl-item:hover { border-color: var(--accent); color: var(--accent); }
.fl-tip { margin: 12px 0 0; font-size: 11px; line-height: 1.6; color: var(--text-faint); }
.fl-canvas { position: relative; flex: 1 1 auto; min-width: 0; }
.fl-host { width: 100%; height: 100%; background: #fff; }
.fl-ro { position: absolute; inset: 0; cursor: default; }

@media (max-width: 760px) {
  .fl-body { flex-direction: column; height: auto; }
  .fl-palette { flex: 0 0 auto; border-right: none; border-bottom: 1px solid var(--hairline); }
  .fl-host { height: 360px; }
}
</style>
