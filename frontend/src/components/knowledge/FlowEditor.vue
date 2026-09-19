<template>
  <div class="wfc" :class="{ roam: readonly }">
    <!-- 画布工具栏 -->
    <div class="wfc-toolbar">
      <div class="wfc-tb-group" v-if="editable">
        <span class="wfc-tb-label">添加节点</span>
        <button
          v-for="(t, k) in TYPES"
          :key="k"
          class="wfc-pal"
          :style="palStyle(t)"
          @click="addNode(k)"
          :title="'添加' + t.label + '节点'"
        >{{ t.icon }} {{ t.label }}</button>
      </div>
      <div class="wfc-tb-spacer"></div>
      <button class="btn ghost sm" @click="autoLayout" title="按层自动排列节点">⤢ 自动布局</button>
      <div class="wfc-zoom">
        <button class="seg" @click="zoomBy(-0.15)">−</button>
        <span class="wfc-zoom-val">{{ Math.round(vp.scale * 100) }}%</span>
        <button class="seg" @click="zoomBy(0.15)">＋</button>
        <button class="seg" @click="fit" title="适应画布">⛶</button>
        <button class="seg" @click="resetView">1:1</button>
      </div>
    </div>

    <!-- 画布 -->
    <div
      ref="canvasEl"
      class="wfc-canvas"
      @wheel="onWheel"
      @pointerdown="onBgDown"
      @pointerup="onBgUp"
    >
      <div
        class="wfc-world"
        :style="{ width: world.w + 'px', height: world.h + 'px', transform: 'translate(' + vp.x + 'px,' + vp.y + 'px) scale(' + vp.scale + ')' }"
      >
        <!-- 背景网格 -->
        <div class="wfc-grid" :style="{ width: world.w + 'px', height: world.h + 'px' }"></div>

        <!-- 连线层 -->
        <svg class="wfc-svg" :width="world.w" :height="world.h" :viewBox="'0 0 ' + world.w + ' ' + world.h">
          <defs>
            <marker id="wfArrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
              <path d="M0,0 L10,5 L0,10 z" :fill="tempEdge ? 'var(--wf-accent)' : '#94a3b8'"></path>
            </marker>
          </defs>
          <g v-for="e in graph.edges" :key="e.id">
            <path
              :d="edgePath(e)"
              class="wfc-edgecline"
              :class="{ on: sel === e.id }"
              marker-end="url(#wfArrow)"
            ></path>
            <path :d="edgePath(e)" class="wfc-edgehit" @click.stop="sel = e.id" @dblclick.stop="editable && beginEdgeEdit(e, $event)"></path>
            <g v-if="edgeLen(e) > 90" class="wfc-elabg" :transform="'translate(' + edgeMid(e).x + ',' + edgeMid(e).y + ')'">
              <foreignObject :x="-elabW(e) / 2" :y="-14" :width="elabW(e)" :height="28">
                <div class="wfc-elab">
                  <input v-if="editingEdge === e.id" :ref="setEdgeInput" v-model="edgeDraft" class="wfc-elab-in" @blur="commitEdgeLabel(e)" @keydown.enter.prevent="commitEdgeLabel(e)" @keydown.stop />
                  <span v-else @click.stop="editable && beginEdgeEdit(e)">{{ e.label || (sel === e.id ? '添加标签' : '') }}</span>
                </div>
              </foreignObject>
            </g>
          </g>
          <!-- 临时连线 -->
          <path v-if="tempEdge" :d="tempPath" fill="none" stroke="var(--wf-accent)" stroke-width="2" stroke-dasharray="6 4" marker-end="url(#wfArrow)"></path>
        </svg>

        <!-- 节点层 -->
        <div
          v-for="n in graph.nodes"
          :key="n.id"
          class="wfc-node"
          :class="[n.type, { sel: sel === n.id }]"
          :style="nodeStyle(n)"
          @pointerdown.stop="onNodeDown(n, $event)"
          @dblclick.stop="editable && beginEdit(n)"
        >
          <span class="wfc-node-ico">{{ typeMeta(n.type).icon }}</span>
          <span class="wfc-node-txt">{{ editing === n.id ? '' : (n.label || typeMeta(n.type).label) }}</span>
          <input
            v-if="editing === n.id"
            :ref="setNodeInput"
            v-model="nodeDraft"
            class="wfc-node-in"
            placeholder="节点名称"
            @blur="commitNode(n)"
            @keydown.enter.prevent="commitNode(n)"
            @keydown.stop
          />
          <span v-if="editable" class="wfc-port in" @pointerdown.stop.prevent="startEdge(n, 'in', $event)" title="入端口"></span>
          <span v-if="editable" class="wfc-port out" @pointerdown.stop.prevent="startEdge(n, 'out', $event)" title="出端口，拖动到其它节点可连线"></span>
          <span v-if="editable && n.type === 'decision'" class="wfc-port down" @pointerdown.stop.prevent="startEdge(n, 'down', $event)" title="下方分支出端口"></span>
        </div>

        <!-- 空状态 / 提示 -->
        <div v-if="!graph.nodes.length" class="wfc-empty">
          {{ editable ? '从上方工具栏添加「开始 / 步骤 / 判断」等节点，拖动节点右/下侧圆点即可连线。' : '（本流程图为空）' }}
        </div>
      </div>

      <!-- 选中项编辑浮条 -->
      <transition name="pop">
        <div v-if="editable && sel && !editing && !editingEdge" class="wfc-selbar">
          <template v-if="selNode">
            <button class="btn ghost sm" @click="dupNode(selNode)">复制</button>
            <button class="btn ghost sm danger" @click="delNode(selNode)">删除节点</button>
            <span class="dim">· 双击节点改名称</span>
          </template>
          <template v-else>
            <button class="btn ghost sm danger" @click="delEdge(sel)">删除连线</button>
            <span class="dim">· 双击连线改标签</span>
          </template>
        </div>
      </transition>

      <div v-if="!editable" class="wfc-roamtip">只读查看 · 滚轮缩放 / 拖拽平移</div>
      <div v-else class="wfc-roamtip">滚轮缩放（Ctrl） · 空白拖拽平移 · 点击选中</div>
    </div>
  </div>
</template>

<script>
import { reactive, ref, computed, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'

const TYPES = {
  start:    { label: '开始',   color: '#10b981', icon: '▶', size: [140, 54] },
  process:  { label: '步骤',   color: '#3b82f6', icon: '▤', size: [168, 58] },
  decision: { label: '判断',   color: '#f59e0b', icon: '◇', size: [156, 92] },
  subflow:  { label: '子流程', color: '#8b5cf6', icon: '⧉', size: [172, 58] },
  manual:   { label: '人工',   color: '#14b8a6', icon: '✋', size: [168, 58] },
  warning:  { label: '校验',   color: '#f97316', icon: '⚠', size: [168, 58] },
  end:      { label: '结束',   color: '#ef4444', icon: '■', size: [140, 54] }
}
const TYPE_KEYS = Object.keys(TYPES)

// 旧版 LogicFlow 形状 → 新类型映射
const LFTYPE = { rect: 'process', diamond: 'decision', ellipse: 'process', circle: 'process', text: 'process', polygon: 'decision' }

let uid = 0
const nid = () => 'n' + (++uid)
const eid = () => 'e' + (++uid)

export default {
  name: 'FlowEditor',
  props: {
    modelValue: { type: String, default: '' },
    readonly: { type: Boolean, default: false }
  },
  emits: ['update:modelValue', 'dirty'],
  setup(props, { emit }) {
    const editable = computed(() => !props.readonly)
    const canvasEl = ref(null)
    const vp = reactive({ scale: 1, x: 0, y: 0 })
    const sel = ref(null)
    const graph = reactive({ nodes: [], edges: [] })
    const editing = ref(null)
    const nodeDraft = ref('')
    const editingEdge = ref(null)
    const edgeDraft = ref('')
    let nodeInputEl = null
    let edgeInputEl = null

    const tempEdge = ref(null) // {from, fromPort, to, toPort}
    const tempPath = ref('')
    let tempFrom = null

    const PAD = 60

    function setNodeInput(el) { nodeInputEl = el }
    function setEdgeInput(el) { edgeInputEl = el }

    // ---- 解析：新格式 {version,nodes,edges} + 旧版 LogicFlow 数据自动迁移 ----
    function lfText(t) {
      if (t == null) return ''
      if (typeof t === 'string') return t
      if (typeof t === 'object' && typeof t.value === 'string') return t.value
      return ''
    }
    function convertLegacy(g) {
      const nodes = (Array.isArray(g.nodes) ? g.nodes : []).map((n) => {
        const t = LFTYPE[n.type] || 'process'
        const w = TYPES[t].size[0], h = TYPES[t].size[1]
        // LogicFlow 的 x/y 是中心点，这里换算成左上角
        return {
          id: String(n.id ?? nid()),
          type: t,
          label: lfText(n.text) || TYPES[t].label,
          x: Math.max(0, Math.round((Number(n.x) || 200) - w / 2)),
          y: Math.max(0, Math.round((Number(n.y) || 120) - h / 2)),
          w, h
        }
      })
      const ids = new Set(nodes.map((n) => n.id))
      const edges = (Array.isArray(g.edges) ? g.edges : [])
        .filter((e) => ids.has(String(e.sourceNodeId)) && ids.has(String(e.targetNodeId)))
        .map((e) => ({
          id: String(e.id ?? eid()),
          from: String(e.sourceNodeId),
          to: String(e.targetNodeId),
          fromPort: 'out', toPort: 'in',
          label: lfText(e.text)
        }))
      return { nodes, edges }
    }
    function parse(input) {
      graph.nodes = []
      graph.edges = []
      const raw = String(input || '').trim()
      if (!raw) return
      try {
        let g = JSON.parse(raw)
        // 旧版 LogicFlow：节点 type 是 rect/diamond/ellipse 等，且没有 label 字段
        const isLegacy = Array.isArray(g.nodes) && g.nodes.length > 0 && g.nodes.some((n) => LFTYPE[n.type] && n.label === undefined)
        if (isLegacy) g = convertLegacy(g)
        graph.nodes = Array.isArray(g.nodes) ? g.nodes : []
        graph.edges = Array.isArray(g.edges) ? g.edges : []
        for (const n of graph.nodes) uid = Math.max(uid, parseInt(String(n.id).replace(/\D/g, ''), 10) || 0)
        // 保证当前节点有合法类型与尺寸
        for (const n of graph.nodes) {
          const m = typeMeta(n.type)
          if (!n.w || !n.h) { n.w = m.size[0]; n.h = m.size[1] }
          n.type = TYPE_KEYS.includes(n.type) ? n.type : 'process'
        }
        graph.edges = graph.edges.filter((e) => graph.nodes.some((n) => n.id === e.from) && graph.nodes.some((n) => n.id === e.to))
      } catch {
        graph.nodes = []
        graph.edges = []
      }
    }
    parse(props.modelValue)

    let lastEmitted = ''
    watch(() => props.modelValue, (v) => {
      const s = String(v || '')
      if (s === lastEmitted) return
      if (editing.value !== null || editingEdge.value !== null) return
      parse(s)
    })

    function typeMeta(t) { return TYPES[t] || TYPES.process }

    function nodeBounds() {
      let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity
      for (const n of graph.nodes) {
        minX = Math.min(minX, n.x); minY = Math.min(minY, n.y)
        maxX = Math.max(maxX, n.x + (n.w || 160)); maxY = Math.max(maxY, n.y + (n.h || 58))
      }
      if (!isFinite(minX)) { minX = 0; minY = 0; maxX = 1200; maxY = 800 }
      return { minX, minY, maxX, maxY }
    }

    const world = computed(() => {
      const b = nodeBounds()
      return { w: Math.max(b.maxX - b.minX + PAD * 2, 600), h: Math.max(b.maxY - b.minY + PAD * 2, 400) }
    })

    function portPos(n, port) {
      const w = n.w || 160, h = n.h || 58
      const cx = n.x + w / 2, cy = n.y + h / 2
      if (port === 'in') return { x: n.x, y: cy }
      if (port === 'out') return { x: n.x + w, y: cy }
      if (port === 'down') return { x: cx, y: n.y + h }
      return { x: cx, y: cy }
    }
    const dirVec = { in: [-1, 0], out: [1, 0], down: [0, 1], top: [0, -1] }

    function edgePath(e) {
      const s = nodeById(e.from), t = nodeById(e.to)
      if (!s || !t) return ''
      const sp = portPos(s, e.fromPort || 'out')
      const tp = portPos(t, e.toPort || 'in')
      const ds = dirVec[e.fromPort || 'out']
      const dt = dirVec[e.toPort || 'in']
      const len = Math.max(50, Math.abs(tp.x - sp.x) * 0.5, Math.abs(tp.y - sp.y) * 0.5)
      const c1 = { x: sp.x + ds[0] * len, y: sp.y + ds[1] * len }
      const c2 = { x: tp.x + dt[0] * len, y: tp.y + dt[1] * len }
      return `M${sp.x},${sp.y} C${c1.x},${c1.y} ${c2.x},${c2.y} ${tp.x},${tp.y}`
    }
    function edgeLen(e) {
      const s = nodeById(e.from), t = nodeById(e.to)
      if (!s || !t) return 0
      const a = portPos(s, e.fromPort || 'out'), b = portPos(t, e.toPort || 'in')
      return Math.hypot(a.x - b.x, a.y - b.y)
    }
    function edgeMid(e) {
      const s = nodeById(e.from), t = nodeById(e.to)
      if (!s || !t) return { x: 0, y: 0 }
      const a = portPos(s, e.fromPort || 'out'), b = portPos(t, e.toPort || 'in')
      return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }
    }
    function elabW(e) { return Math.max(48, String(e.label || '添加标签').length * 14) }

    function nodeById(id) { return graph.nodes.find((n) => n.id === id) }

    function nodeStyle(n) {
      const w = n.w || 160, h = n.h || 58
      const m = typeMeta(n.type)
      return {
        transform: `translate(${n.x}px, ${n.y}px)`,
        width: w + 'px', height: h + 'px',
        '--node': m.color
      }
    }
    const palStyle = (t) => ({ '--pal': t.color })

    // ---- 内容变更回写 ----
    function emitNow() {
      const payload = JSON.stringify({ version: 1, nodes: graph.nodes, edges: graph.edges })
      lastEmitted = payload
      emit('update:modelValue', payload)
      emit('dirty', true)
    }

    // ---- 增删节点 ----
    function addNode(type) {
      const m = TYPES[type]
      const x = PAD + (graph.nodes.length % 4) * (m.size[0] + 70)
      const y = PAD + Math.floor(graph.nodes.length / 4) * (m.size[1] + 50)
      const node = { id: nid(), type, label: m.label, x, y, w: m.size[0], h: m.size[1] }
      graph.nodes.push(node)
      sel.value = node.id
      emitNow()
    }
    function delNode(n) {
      graph.nodes = graph.nodes.filter((x) => x.id !== n.id)
      graph.edges = graph.edges.filter((e) => e.from !== n.id && e.to !== n.id)
      sel.value = null
      emitNow()
    }
    function dupNode(n) {
      const c = { ...n, id: nid(), x: n.x + 24, y: n.y + 24 }
      graph.nodes.push(c)
      sel.value = c.id
      emitNow()
    }
    function delEdge(id) { graph.edges = graph.edges.filter((e) => e.id !== id); sel.value = null; emitNow() }

    // ---- 节点内联编辑 ----
    async function beginEdit(n) {
      editing.value = n.id
      nodeDraft.value = n.label || ''
      sel.value = n.id
      await nextTick()
      if (nodeInputEl) { nodeInputEl.focus(); nodeInputEl.select() }
    }
    function commitNode(n) {
      n.label = nodeDraft.value.trim() || typeMeta(n.type).label
      editing.value = null
      emitNow()
    }

    // ---- 连线标签 ----
    async function beginEdgeEdit(e) {
      editingEdge.value = e.id
      edgeDraft.value = e.label || ''
      sel.value = e.id
      await nextTick()
      if (edgeInputEl) { edgeInputEl.focus(); edgeInputEl.select() }
    }
    function commitEdgeLabel(e) {
      e.label = edgeDraft.value.trim()
      editingEdge.value = null
      emitNow()
    }

    // ---- 节点拖拽 ----
    function onNodeDown(n, ev) {
      if (!editable.value) return
      if (ev.button !== 0) return
      sel.value = n.id
      const startX = ev.clientX, startY = ev.clientY
      const ox = n.x, oy = n.y
      const move = (me) => {
        const dx = (me.clientX - startX) / vp.scale
        const dy = (me.clientY - startY) / vp.scale
        n.x = Math.max(0, Math.round(ox + dx))
        n.y = Math.max(0, Math.round(oy + dy))
      }
      const up = () => {
        window.removeEventListener('pointermove', move)
        window.removeEventListener('pointerup', up)
        const nh = nodeById(n.id)
        if (nh && (Math.abs(nh.x - ox) > 2 || Math.abs(nh.y - oy) > 2)) emitNow()
      }
      window.addEventListener('pointermove', move)
      window.addEventListener('pointerup', up)
    }

    // ---- 连线创建 ----
    function canvasCoords(ev) {
      const r = canvasEl.value.getBoundingClientRect()
      return { x: (ev.clientX - r.left - vp.x) / vp.scale, y: (ev.clientY - r.top - vp.y) / vp.scale }
    }
    function startEdge(n, kind, ev) {
      if (!editable.value) return
      ev.stopPropagation()
      sel.value = n.id
      const p = portPos(n, kind)
      tempEdge.value = { from: n.id, fromPort: kind === 'in' ? 'out' : kind, to: null, toPort: null }
      const move = (me) => {
        const c = canvasCoords(me)
        const tgt = nodeAt(c)
        if (tgt && tgt.id !== n.id) {
          const tpt = nearestPort(tgt, c)
          tempEdge.value.to = tgt.id
          tempEdge.value.toPort = tpt
          updateTempCoords(p, portPos(tgt, tpt))
        } else {
          tempEdge.value.to = null
          updateTempCoords(p, c)
        }
      }
      const up = () => {
        window.removeEventListener('pointermove', move)
        window.removeEventListener('pointerup', up)
        const te = tempEdge.value
        if (te && te.to && te.to !== n.id) {
          addEdge(n.id, te.to, te.fromPort, te.toPort)
        }
        tempEdge.value = null
        tempPath.value = ''
      }
      window.addEventListener('pointermove', move)
      window.addEventListener('pointerup', up)
    }
    function updateTempCoords(from, to) {
      tempFrom = from
      tempPath.value = bezier(from, to, [1, 0], [-1, 0])
    }
    function bezier(a, b, da, dt) {
      const len = Math.max(50, Math.abs(b.x - a.x) * 0.5)
      const c1 = { x: a.x + da[0] * len, y: a.y + da[1] * len }
      const c2 = { x: b.x + dt[0] * len, y: b.y + dt[1] * len }
      return `M${a.x},${a.y} C${c1.x},${c1.y} ${c2.x},${c2.y} ${b.x},${b.y}`
    }
    function nodeAt(c) {
      for (let i = graph.nodes.length - 1; i >= 0; i--) {
        const n = graph.nodes[i]
        const w = n.w || 160, h = n.h || 58
        if (c.x >= n.x && c.x <= n.x + w && c.y >= n.y && c.y <= n.y + h) return n
      }
      return null
    }
    function nearestPort(n, c) {
      const inP = portPos(n, 'in'), outP = portPos(n, 'out')
      const dIn = Math.hypot(c.x - inP.x, c.y - inP.y)
      const dOut = Math.hypot(c.x - outP.x, c.y - outP.y)
      return dIn <= dOut ? 'in' : 'out'
    }
    function addEdge(from, to, fromPort, toPort) {
      // 防重复
      if (graph.edges.some((e) => e.from === from && e.to === to && (e.fromPort || 'out') === fromPort)) return
      graph.edges.push({ id: eid(), from, to, fromPort, toPort, label: '' })
      emitNow()
    }

    // ---- 画布平移 / 缩放 ----
    function onBgDown(ev) {
      if (!editable.value || ev.button !== 0) return
      if (tempEdge.value) return
      sel.value = null
      const sx = ev.clientX, sy = ev.clientY
      const ox = vp.x, oy = vp.y
      const move = (me) => {
        vp.x = ox + (me.clientX - sx)
        vp.y = oy + (me.clientY - sy)
      }
      const up = () => { window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', up) }
      window.addEventListener('pointermove', move)
      window.addEventListener('pointerup', up)
    }
    function onBgUp() {}
    function onWheel(ev) {
      if (!editable.value && !ev.ctrlKey) return
      if (ev.ctrlKey) {
        ev.preventDefault()
        const r = canvasEl.value.getBoundingClientRect()
        const c = { x: ev.clientX - r.left - vp.x, y: ev.clientY - r.top - vp.y }
        const factor = ev.deltaY > 0 ? 1.1 : 1 / 1.1
        const ns = vp.scale * factor
        if (ns < 0.2 || ns > 2.5) return
        vp.scale = ns
        vp.x = ev.clientX - r.left - c.x * factor
        vp.y = ev.clientY - r.top - c.y * factor
      } else if (editable.value) {
        ev.preventDefault()
        vp.x -= ev.deltaX
        vp.y -= ev.deltaY
      }
    }
    function zoomBy(d) {
      const ns = Math.min(2.5, Math.max(0.2, vp.scale + d))
      // 以画布中心缩放
      const r = canvasEl.value.getBoundingClientRect()
      const cx = r.width / 2 - vp.x, cy = r.height / 2 - vp.y
      const factor = ns / vp.scale
      vp.scale = ns
      vp.x = r.width / 2 - cx * factor
      vp.y = r.height / 2 - cy * factor
    }
    function resetView() {
      vp.scale = 1
      vp.x = PAD
      vp.y = PAD
    }
    function fit() {
      const r = canvasEl.value.getBoundingClientRect()
      const b = nodeBounds()
      const cw = b.maxX - b.minX + PAD * 2, ch = b.maxY - b.minY + PAD * 2
      const s = Math.min(r.width / cw, r.height / ch, 1.2)
      vp.scale = Math.max(0.2, s)
      vp.x = (r.width - cw * s) / 2 - b.minX * s + PAD * s
      vp.y = (r.height - ch * s) / 2 - b.minY * s + PAD * s
    }

    // ---- 自动布局（分层） ----
    function autoLayout() {
      if (!graph.nodes.length) return
      const indeg = {}
      for (const n of graph.nodes) indeg[n.id] = 0
      for (const e of graph.edges) { if (indeg[e.to] !== undefined) indeg[e.to]++ }
      // 拓扑分层
      const layers = []
      let frontier = graph.nodes.filter((n) => indeg[n.id] === 0)
      const placed = new Set()
      let guard = 0
      while (frontier.length && guard++ < 1000) {
        layers.push([...frontier])
        const next = []
        for (const n of frontier) placed.add(n.id)
        for (const e of graph.edges) {
          if (placed.has(e.from) && !placed.has(e.to)) {
            indeg[e.to]--
            if (indeg[e.to] <= 0) next.push(nodeById(e.to))
          }
        }
        frontier = next.filter((n) => n && !placed.has(n.id))
      }
      const placedIds = new Set(layers.flat().map((n) => n.id))
      const unplaced = graph.nodes.filter((n) => !placedIds.has(n.id))
      const gutterX = 90, gutterY = 60
      const xOff = PAD, yOff = PAD
      let cursorX = xOff
      for (const layer of layers) {
        let maxW = 0, cursorY = yOff
        for (const n of layer) {
          n.x = cursorX
          n.y = cursorY
          cursorY += (n.h || 58) + gutterY
          maxW = Math.max(maxW, n.w || 160)
        }
        cursorX += maxW + gutterX
      }
      if (unplaced.length) {
        let cy = yOff
        for (const n of unplaced) { n.x = cursorX + PAD; n.y = cy; cy += (n.h || 58) + 40 }
      }
      sel.value = null
      emitNow()
    }

    // 键盘删除
    const selNode = computed(() => graph.nodes.find((n) => n.id === sel.value) || null)
    function onKey(ev) {
      if (!editable.value || editing.value || editingEdge.value) return
      const t = ev.target
      const tag = t && t.tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || (t && t.isContentEditable)) return
      if ((ev.key === 'Delete' || ev.key === 'Backspace') && sel.value) {
        ev.preventDefault()
        if (selNode.value) delNode(selNode.value)
        else delEdge(sel.value)
      }
    }
    onMounted(() => window.addEventListener('keydown', onKey, true))
    onBeforeUnmount(() => window.removeEventListener('keydown', onKey, true))

    // ---- 导出（SVG / PNG）：把当前图序列化为独立 SVG ----
    function hexBlend(hex, base, ratio) {
      const p = (h) => [parseInt(h.slice(1, 3), 16), parseInt(h.slice(3, 5), 16), parseInt(h.slice(5, 7), 16)]
      const a = p(hex), b = p(base)
      const m = a.map((v, i) => Math.round(v * ratio + b[i] * (1 - ratio)))
      return '#' + m.map((v) => v.toString(16).padStart(2, '0')).join('')
    }
    function esc(s) { return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;') }
    function buildSvgString() {
      if (!graph.nodes.length) return ''
      const b = nodeBounds()
      const pad = 40
      const W = Math.round(b.maxX - b.minX + pad * 2), H = Math.round(b.maxY - b.minY + pad * 2)
      const ox = (x) => (x - b.minX + pad).toFixed(1), oy = (y) => (y - b.minY + pad).toFixed(1)
      let s = `<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}">`
      s += `<defs><marker id="a" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" fill="#94a3b8"/></marker></defs>`
      s += `<rect width="${W}" height="${H}" fill="#0b1220"/>`
      for (const e of graph.edges) {
        const sp = portPos(nodeById(e.from), e.fromPort || 'out')
        const tp = portPos(nodeById(e.to), e.toPort || 'in')
        const ds = dirVec[e.fromPort || 'out'], dt = dirVec[e.toPort || 'in']
        const len = Math.max(50, Math.abs(tp.x - sp.x) * 0.5, Math.abs(tp.y - sp.y) * 0.5)
        const c1 = { x: sp.x + ds[0] * len, y: sp.y + ds[1] * len }
        const c2 = { x: tp.x + dt[0] * len, y: tp.y + dt[1] * len }
        s += `<path d="M${ox(sp.x)},${oy(sp.y)} C${ox(c1.x)},${oy(c1.y)} ${ox(c2.x)},${oy(c2.y)} ${ox(tp.x)},${oy(tp.y)}" fill="none" stroke="#475569" stroke-width="2" marker-end="url(#a)"/>`
        if (e.label) {
          const mid = { x: (sp.x + tp.x) / 2, y: (sp.y + tp.y) / 2 }
          s += `<text x="${ox(mid.x)}" y="${oy(mid.y)}" text-anchor="middle" dominant-baseline="central" font-family="'PingFang SC','Microsoft YaHei',sans-serif" font-size="12" fill="#94a3b8">${esc(e.label)}</text>`
        }
      }
      for (const n of graph.nodes) {
        const m = typeMeta(n.type)
        const w = n.w || 160, h = n.h || 58
        const fill = hexBlend(m.color, '#0f172a', 0.22)
        const rx = n.type === 'start' || n.type === 'end' ? 26 : 10
        s += `<rect x="${ox(n.x)}" y="${oy(n.y)}" width="${w}" height="${h}" rx="${rx}" fill="${fill}" stroke="${hexBlend(m.color, '#0b1220', 0.6)}" stroke-width="1.5"/>`
        const cx = Number(ox(n.x + w / 2)), cy = Number(oy(n.y + h / 2))
        const label = n.label || m.label
        s += `<text x="${cx - label.length * 3}" y="${cy}" text-anchor="middle" dominant-baseline="central" font-family="'PingFang SC','Microsoft YaHei',sans-serif" font-size="13" fill="#f1f5f9">${esc(m.icon + ' ' + label)}</text>`
      }
      s += '</svg>'
      return s
    }
    async function exportBlob(fileType = 'png') {
      const svg = buildSvgString()
      if (!svg) return null
      const svgBlob = new Blob([svg], { type: 'image/svg+xml;charset=utf-8' })
      if (fileType === 'svg') return svgBlob
      const url = URL.createObjectURL(svgBlob)
      try {
        const img = await new Promise((res, rej) => { const i = new Image(); i.onload = () => res(i); i.onerror = () => rej(new Error('渲染导出图失败')); i.src = url })
        const c = document.createElement('canvas')
        c.width = img.naturalWidth || img.width
        c.height = img.naturalHeight || img.height
        const ctx = c.getContext('2d')
        ctx.drawImage(img, 0, 0)
        return await new Promise((res) => c.toBlob(res, 'image/png'))
      } finally {
        URL.revokeObjectURL(url)
      }
    }

    return {
      TYPES, graph, vp, sel, world, editable, canvasEl, tempPath, tempEdge,
      setNodeInput, setEdgeInput, editing, nodeDraft, editingEdge, edgeDraft,
      typeMeta, nodeStyle, palStyle, edgePath, edgeLen, edgeMid, elabW,
      addNode, delNode, dupNode, delEdge, beginEdit, commitNode,
      beginEdgeEdit, commitEdgeLabel, onNodeDown, startEdge,
      onBgDown, onBgUp, onWheel, zoomBy, resetView, fit, autoLayout,
      selNode
    }
  }
}
</script>

<style>
.wfc { position: relative; border-radius: 12px; overflow: hidden; background: #0b1220; height: clamp(520px, calc(100vh - 480px), 1100px); display: flex; flex-direction: column; --wf-accent: #38bdf8; }
.wfc-toolbar { display: flex; align-items: center; gap: 8px; padding: 8px 10px; background: linear-gradient(180deg, #0f172a, #0b1220); border-bottom: 1px solid #1e293b; flex-wrap: wrap; }
.wfc-tb-group { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.wfc-tb-label { font-size: 12px; color: #94a3b8; margin-right: 2px; }
.wfc-pal { display: inline-flex; align-items: center; gap: 5px; border: 1px solid #273447; background: rgba(30,41,59,.6); color: #e2e8f0; border-radius: 8px; padding: 4px 8px; font-size: 12px; cursor: pointer; transition: .15s; }
.wfc-pal:hover { border-color: var(--pal); color: var(--pal); transform: translateY(-1px); }
.wfc-tb-spacer { flex: 1; }
.wfc-zoom { display: flex; align-items: center; gap: 4px; }
.wfc-zoom-val { font-size: 12px; color: #94a3b8; width: 40px; text-align: center; }
.wfc .seg { min-width: 28px; height: 26px; padding: 0 8px; font-size: 13px; line-height: 1; color: #cbd5e1; background: rgba(30,41,59,.6); border: 1px solid #273447; border-radius: 7px; cursor: pointer; }
.wfc .seg:hover { border-color: var(--wf-accent); color: var(--wf-accent); }
.wfc .btn.ghost.sm { background: rgba(30,41,59,.6); color: #e2e8f0; border: 1px solid #273447; }
.wfc .btn.ghost.sm:hover { border-color: var(--wf-accent); color: var(--wf-accent); }
.wfc .btn.ghost.sm.danger { color: #f87171; }
.wfc .dim { color: #64748b; font-size: 12px; }
.wfc-canvas { flex: 1; position: relative; overflow: hidden; cursor: grab; }
.wfc-canvas:active { cursor: grabbing; }
.wfc-roamtip { position: absolute; left: 10px; bottom: 8px; font-size: 11px; color: #64748b; background: rgba(15,23,42,.7); padding: 3px 8px; border-radius: 6px; pointer-events: none; }
.wfc-world { position: absolute; top: 0; left: 0; transform-origin: 0 0; }
.wfc-grid { position: absolute; top: 0; left: 0; background-image: radial-gradient(rgba(148,163,184,.18) 1px, transparent 1.5px); background-size: 24px 24px; }
.wfc-svg { position: absolute; top: 0; left: 0; pointer-events: none; }
.wfc-edgecline { fill: none; stroke: #475569; stroke-width: 2; pointer-events: none; transition: stroke .15s; }
.wfc-edgecline.on { stroke: var(--wf-accent); stroke-width: 2.4; }
.wfc-edgehit { fill: none; stroke: transparent; stroke-width: 14; pointer-events: all; cursor: pointer; }
.wfc-node { position: absolute; top: 0; left: 0; display: flex; align-items: center; justify-content: center; gap: 6px; color: #f1f5f9; border-radius: 10px; border: 1.5px solid color-mix(in srgb, var(--node) 60%, transparent); background: linear-gradient(180deg, color-mix(in srgb, var(--node) 22%, #0f172a), color-mix(in srgb, var(--node) 12%, #0b1220)); box-shadow: 0 4px 14px rgba(0,0,0,.35); cursor: grab; user-select: none; font-size: 13px; transition: box-shadow .15s, border-color .15s; z-index: 2; }
.wfc-node.sel { box-shadow: 0 0 0 2px var(--wf-accent), 0 6px 18px rgba(0,0,0,.4); border-color: var(--wf-accent); z-index: 3; }
.wfc-node.diamond { border-radius: 6px; }
.wfc-node.start { border-radius: 26px; background: linear-gradient(180deg, color-mix(in srgb, var(--node) 30%, #0f172a), color-mix(in srgb, var(--node) 16%, #0b1220)); }
.wfc-node.end { border-radius: 26px; background: linear-gradient(180deg, color-mix(in srgb, var(--node) 30%, #0f172a), color-mix(in srgb, var(--node) 16%, #0b1220)); }
.wfc-node.subflow { clip-path: polygon(0 0, calc(100% - 12px) 0, 100% 12px, 100% 100%, 0 100%); }
.wfc-node.warning { border-style: dashed; }
.wfc-node-ico { font-size: 15px; color: var(--node); line-height: 1; }
.wfc-node-txt { line-height: 1.3; max-width: 90%; }
.wfc-node-in { background: #0f172a; color: #e2e8f0; border: 1px solid var(--wf-accent); border-radius: 6px; font-size: 13px; padding: 3px 6px; width: 80%; }
.wfc-port { position: absolute; width: 12px; height: 12px; border-radius: 50%; background: #0f172a; border: 2px solid var(--wf-accent); z-index: 5; cursor: crosshair; }
.wfc-port:hover { background: var(--wf-accent); transform: scale(1.3); }
.wfc-port.in { left: -7px; top: 50%; margin-top: -6px; }
.wfc-port.out { right: -7px; top: 50%; margin-top: -6px; }
.wfc-port.down { left: 50%; bottom: -7px; margin-left: -6px; }
.wfc-elab { display: flex; align-items: center; justify-content: center; width: 100%; height: 100%; font-size: 12px; color: #94a3b8; }
.wfc-elab-in { background: #0f172a; color: #e2e8f0; border: 1px solid var(--wf-accent); border-radius: 5px; font-size: 12px; padding: 1px 5px; text-align: center; }
.wfc-empty { position: absolute; left: 50%; top: 50%; transform: translate(-50%, -50%); color: #475569; font-size: 13px; background: rgba(15,23,42,.6); padding: 14px 20px; border-radius: 10px; border: 1px dashed #334155; text-align: center; max-width: 340px; line-height: 1.6; }
.wfc-selbar { position: absolute; top: 10px; left: 50%; transform: translateX(-50%); display: flex; align-items: center; gap: 6px; background: #0f172a; border: 1px solid #273447; padding: 5px 8px; border-radius: 8px; z-index: 6; box-shadow: 0 6px 20px rgba(0,0,0,.4); }
.wfc.roam { cursor: default; }
.wfc.roam .wfc-canvas { cursor: grab; }
.wfc.roam .wfc-node { cursor: default; }
</style>
