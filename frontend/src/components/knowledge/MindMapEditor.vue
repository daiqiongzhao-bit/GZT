<template>
  <div class="mm" :class="{ roam: readonly }">
    <div class="mm-toolbar">
      <div class="mm-tb-group" v-if="!readonly">
        <button class="btn ghost sm" :disabled="!selNode" @click="addChild(selNode)">+ 子节点 <kbd>Tab</kbd></button>
        <button class="btn ghost sm" :disabled="!selNode" @click="addSibling(selNode)">+ 同级 <kbd>Enter</kbd></button>
        <button class="btn ghost sm" :disabled="!selNode" @click="toggleCollapse(selNode)">{{ selNode && selNode.collapsed ? '展开' : '折叠' }}</button>
        <button class="btn ghost sm danger" :disabled="!selNode || selNode === tree" @click="removeNode(selNode)">删除</button>
        <span class="mm-tb-sep"></span>
        <span class="mm-tb-label">主题</span>
        <button v-for="(t, i) in themes" :key="i" class="mm-theme" :class="{ on: theme === i }" :style="{ background: t.root }" :title="t.name" @click="theme = i"></button>
      </div>
      <div class="mm-tb-spacer"></div>
      <div class="mm-zoom">
        <button class="seg" @click="zoomBy(-0.15)">−</button>
        <span class="mm-zoom-val">{{ Math.round(vp.scale * 100) }}%</span>
        <button class="seg" @click="zoomBy(0.15)">＋</button>
        <button class="seg" @click="fit">⛶</button>
        <button class="seg" @click="resetView">1:1</button>
      </div>
    </div>

    <div ref="canvasEl" class="mm-canvas" @wheel="onWheel" @pointerdown="onBgDown" @dragover.prevent @drop.prevent>
      <div class="mm-world" :style="{ width: world.w + 'px', height: world.h + 'px', transform: 'translate(' + vp.x + 'px,' + vp.y + 'px) scale(' + vp.scale + ')' }">
        <div class="mm-grid" :style="{ width: world.w + 'px', height: world.h + 'px' }"></div>
        <svg class="mm-svg" :width="world.w" :height="world.h" :viewBox="'0 0 ' + world.w + ' ' + world.h">
          <g v-for="(c, i) in connectList" :key="i">
            <path :d="c.path" class="mm-line" :style="{ stroke: colorAt(c.depth) }"></path>
          </g>
        </svg>

        <template v-for="(pos, id) in positions" :key="id">
          <div
            v-if="pos.visible"
            class="mm-node"
            :class="[pos.isRoot ? 'root' : '', { sel: sel === id, edit: editing === id }]"
            :style="{ left: pos.x + 'px', top: pos.y + 'px', '--mmc': colorAt(pos.depth) }"
            :draggable="!readonly"
            @dragstart="onDragStart(pos, $event)"
            @dragend="dnd = null"
            @dragover.stop.prevent="dnd = id"
            @dragleave="dnd === id && (dnd = null)"
            @drop.stop.prevent="onDropTo(id, $event)"
            @click.stop="!readonly && (sel = id, editing = null)"
            @dblclick.stop="!readonly && beginEdit(id)"
          >
            <span class="mm-grip" v-if="!readonly && !pos.isRoot">⠿</span>
            <span class="mm-text">{{ pos.n.text }}</span>
            <input v-if="editing === id" :ref="setNodeInput" v-model="draft" class="mm-input" @blur="commit(id)" @keydown.enter.prevent="commit(id)" @keydown.esc.prevent="editing=null" @keydown.stop />
            <span v-if="!readonly && childrenOf(pos.n).length" class="mm-fold" @mousedown.stop @click.stop="toggleCollapse(pos.n)">{{ pos.n.collapsed ? '◀' : '▶' }}</span>
          </div>
        </template>

        <div v-if="!connCount" class="mm-empty">{{ readonly ? '（该思维导图为空）' : '双击中心节点输入主题，按 Tab 添加分支，Enter 添加同级分支。' }}</div>
      </div>

      <div class="mm-hint">{{ readonly ? '只读查看 · 滚轮缩放 / 拖拽平移' : '滚轮缩放 · 空白拖拽平移 · 双击节点改名 · 拖节点图标可重定父级' }}</div>
    </div>
  </div>
</template>

<script>
import { reactive, ref, computed, nextTick, onMounted, onBeforeUnmount, watch } from 'vue'

const themes = [
  { name: '默认', root: '#38bdf8', colors: ['#38bdf8', '#818cf8', '#f472b6', '#34d399', '#fbbf24', '#f87171'] },
  { name: '森林', root: '#22c55e', colors: ['#22c55e', '#84cc16', '#10b981', '#14b8a6', '#a3e635', '#4ade80'] },
  { name: '暖橙', root: '#f97316', colors: ['#f97316', '#fbbf24', '#fb7185', '#a78bfa', '#f59e0b', '#fb923c'] },
  { name: '深海', root: '#0ea5e9', colors: ['#0ea5e9', '#3b82f6', '#6366f1', '#06b6d4', '#22d3ee', '#60a5fa'] },
  { name: '紫罗兰', root: '#a855f7', colors: ['#a855f7', '#d946ef', '#ec4899', '#8b5cf6', '#c084fc', '#e879f9'] }
]

let uid = 0

function mkNode(text = '未命名') {
  return { id: 'm' + (++uid), text, children: [], collapsed: false }
}

export default {
  name: 'MindMapEditor',
  props: {
    modelValue: { type: String, default: '' },
    readonly: { type: Boolean, default: false }
  },
  emits: ['update:modelValue', 'dirty'],
  setup(props, { emit }) {
    const canvasEl = ref(null)
    const vp = reactive({ scale: 1, x: 100, y: 100 })
    const sel = ref(null)
    const editing = ref(null)
    const draft = ref('')
    let nodeInputEl = null
    const theme = ref(0)
    const dnd = ref(null)
    let tree = null
    let dndInfo = null
    let lastEmitted = ''

    const HGAP = 130
    const ROW = 48

    // ---- 输入框 ref（v-for 内用函数式 ref，避免收集成数组）----
    function setNodeInput(el) { nodeInputEl = el }

    // ---- 解析：新格式 {id,text,children} + 旧版 mind-elixir {nodeData:{topic,children}} 自动迁移 ----
    function parse(input) {
      const raw = String(input || '').trim()
      if (!raw) { tree = null; return }
      try {
        const t = JSON.parse(raw)
        if (t && t.nodeData && (t.nodeData.topic !== undefined || t.nodeData.text !== undefined)) { tree = digLegacy(t.nodeData); return }
        if (t && (t.text !== undefined || t.id !== undefined) && (t.children === undefined || Array.isArray(t.children))) { tree = dig(t); return }
        tree = null
      } catch {
        tree = null
      }
    }
    function dig(t) {
      if (typeof t.id === 'number') uid = Math.max(uid, t.id)
      const n = { id: t.id ?? ('m' + (++uid)), text: String(t.text || '未命名'), collapsed: !!t.collapsed, children: [] }
      for (const c of t.children || []) n.children.push(dig(c))
      return n
    }
    // mind-elixir 旧数据：{topic, id?, expanded?, children?}
    function digLegacy(t) {
      const n = {
        id: t.id != null ? String(t.id) : 'm' + (++uid),
        text: String(t.topic || t.text || '未命名'),
        collapsed: t.expanded === false,
        children: []
      }
      for (const c of t.children || []) n.children.push(digLegacy(c))
      return n
    }
    parse(props.modelValue)
    // 新建空图：直接给一个中心主题，见内容即编辑
    if (!tree && !props.readonly) tree = mkNode('中心主题')

    function childrenOf(n) { return (n && n.children) || [] }

    // 展开的所有可见节点（不含 collapsed 的子孙）
    function visibleNodes(n = tree, out = []) {
      if (!n) return out
      out.push(n)
      if (!n.collapsed) for (const c of n.children) visibleNodes(c, out)
      return out
    }

    // 逻辑树布局
    const layoutState = reactive({ positions: {}, conn: [] })
    let cursorLeaf = 0
    function computeLayout() {
      if (!tree) { layoutState.positions = {}; layoutState.conn = []; return }
      const pos = {}
      const conn = []
      cursorLeaf = 0
      const walk = (n, depth) => {
        const visible = n.collapsed ? [] : n.children
        if (!visible.length) {
          pos[n.id] = { x: depth * HGAP, y: cursorLeaf * ROW, depth, isRoot: depth === 0, visible: true, n }
          cursorLeaf++
        } else {
          for (const c of visible) walk(c, depth + 1)
          const first = pos[visible[0].id], last = pos[visible[visible.length - 1].id]
          pos[n.id] = { x: depth * HGAP, y: (first.y + last.y) / 2, depth, isRoot: depth === 0, visible: true, n }
          for (const c of visible) {
            const p = pos[n.id], cp = pos[c.id]
            const mx = p.x + (cp.x - p.x) * 0.5
            conn.push({ depth: depth + 1, path: `M${p.x},${p.y} L${mx},${p.y} L${mx},${cp.y} L${cp.x},${cp.y}` })
          }
        }
      }
      walk(tree, 0)
      let minY = Infinity
      for (const k in pos) minY = Math.min(minY, pos[k].y)
      for (const k in pos) pos[k].y -= minY
      layoutState.positions = pos
      layoutState.conn = conn
    }
    computeLayout()

    const positions = computed(() => layoutState.positions)
    const connectList = computed(() => layoutState.conn)
    const connCount = computed(() => visibleNodes(tree).length)
    const nodeById = computed(() => {
      const m = {}
      for (const n of visibleNodes(tree)) m[n.id] = n
      return m
    })
    const selNode = computed(() => (sel.value && nodeById.value[sel.value]) || null)

    const world = computed(() => {
      let maxX = 900, maxY = 500
      for (const k in layoutState.positions) {
        maxX = Math.max(maxX, layoutState.positions[k].x + 180)
        maxY = Math.max(maxY, layoutState.positions[k].y + 60)
      }
      return { w: maxX, h: maxY + 80 }
    })

    function colorAt(depth) { return themes[theme.value].colors[depth % themes[theme.value].colors.length] }

    function emitNow() {
      if (!tree) return
      lastEmitted = JSON.stringify(tree)
      emit('update:modelValue', lastEmitted)
      emit('dirty', true)
    }

    // 布局要在 DOM 更新后反映
    function relayout() { computeLayout() }

    // 外部内容变化（切换条目等）：非编辑中才重载
    watch(() => props.modelValue, (v) => {
      const s = String(v || '')
      if (s === lastEmitted) return
      lastEmitted = s
      if (editing.value !== null) return
      parse(s)
      if (!tree && !props.readonly) tree = mkNode('中心主题')
      sel.value = null
      relayout()
    })

    // ---- 编辑操作 ----
    function addChild(n) {
      if (!n) return
      const c = mkNode('未命名')
      n.children.push(c)
      sel.value = c.id
      curateCollapsed(n)
      relayout()
      emitNow()
      beginEdit(c.id)
    }
    function addSibling(n) {
      if (!n || n === tree) return
      const p = parentOf(n)
      if (!p) return
      const i = p.children.indexOf(n)
      const s = mkNode('未命名')
      p.children.splice(i + 1, 0, s)
      sel.value = s.id
      relayout()
      emitNow()
      beginEdit(s.id)
    }
    function parentOf(n) {
      const find = (cur) => {
        if (!cur) return null
        for (const c of cur.children) if (c === n) return cur
        for (const c of cur.children) { const r = find(c); if (r) return r }
        return null
      }
      return find(tree)
    }
    function curateCollapsed(n) { if (n.collapsed) n.collapsed = false }
    function toggleCollapse(n) { if (!n) return; n.collapsed = !n.collapsed; relayout(); emitNow() }
    function removeNode(n) {
      if (!n || n === tree) return
      const p = parentOf(n)
      if (!p) return
      const i = p.children.indexOf(n)
      p.children.splice(i, 1, ...(n.children || [])) // 子节点上提为同级
      for (const c of n.children || []) { if (c.collapsed) c.collapsed = false }
      sel.value = p.id
      relayout()
      emitNow()
    }
    async function beginEdit(id) {
      editing.value = id
      draft.value = nodeById.value[id].text
      await nextTick()
      if (nodeInputEl) { nodeInputEl.focus(); nodeInputEl.select() }
    }
    function commit(id) {
      const n = nodeById.value[id]
      if (n) n.text = draft.value.trim() || '未命名'
      editing.value = null
      emitNow()
    }

    // ---- 拖拽改父级 ----
    function onDragStart(pos, ev) {
      dndInfo = pos.n
      sel.value = pos.n.id
      ev.dataTransfer && ev.dataTransfer.setData('text/plain', pos.n.id)
    }
    function onDropTo(targetId, ev) {
      const tgt = nodeById.value[targetId]
      if (!dndInfo || !tgt || dndInfo === tgt) { dnd.value = null; return }
      // 禁止把节点拖到自己的子孙上（成环）
      let cur = tgt
      while (cur) { if (cur === dndInfo) { dnd.value = null; return } cur = parentOf(cur) }
      const p = parentOf(dndInfo)
      const isRoot = !p
      if (isRoot) { dnd.value = null; return }
      const i = p.children.indexOf(dndInfo)
      p.children.splice(i, 1)
      if (tgt.collapsed) tgt.collapsed = false
      tgt.children.push(dndInfo)
      sel.value = dndInfo.id
      relayout()
      emitNow()
      dnd.value = null
    }

    // ---- 平移、缩放 ----
    function onBgDown(ev) {
      if (ev.button !== 0) return
      if (editing.value) return
      const sx = ev.clientX, sy = ev.clientY
      const ox = vp.x, oy = vp.y
      const move = (me) => { vp.x = ox + (me.clientX - sx); vp.y = oy + (me.clientY - sy) }
      const up = () => { window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', up) }
      window.addEventListener('pointermove', move)
      window.addEventListener('pointerup', up)
    }
    function onWheel(ev) {
      const r = canvasEl.value.getBoundingClientRect()
      if (ev.ctrlKey) {
        ev.preventDefault()
        const c = { x: ev.clientX - r.left - vp.x, y: ev.clientY - r.top - vp.y }
        const ns = vp.scale * (ev.deltaY > 0 ? 1.1 : 1 / 1.1)
        if (ns < 0.15 || ns > 2.5) return
        const factor = ns / vp.scale
        vp.scale = ns
        vp.x = ev.clientX - r.left - c.x * factor
        vp.y = ev.clientY - r.top - c.y * factor
      } else {
        ev.preventDefault()
        vp.x -= ev.deltaX
        vp.y -= ev.deltaY
      }
    }
    function zoomBy(d) {
      const r = canvasEl.value.getBoundingClientRect()
      const cx = r.width / 2 - vp.x, cy = r.height / 2 - vp.y
      const ns = Math.min(2.5, Math.max(0.15, vp.scale + d))
      const factor = ns / vp.scale
      vp.scale = ns
      vp.x = r.width / 2 - cx * factor
      vp.y = r.height / 2 - cy * factor
    }
    function resetView() { vp.scale = 1; vp.x = 80; vp.y = 40 }
    function fit() {
      const r = canvasEl.value.getBoundingClientRect()
      const s = Math.min(r.width / world.value.w, r.height / world.value.h, 1.3)
      vp.scale = Math.max(0.15, s)
      vp.x = (r.width - world.value.w * s) / 2
      vp.y = (r.height - world.value.h * s) / 2
    }

    function onKey(ev) {
      if (editing.value || readonly.value) return
      // 焦点在其它输入框/可编辑区时，不抢全局快捷键
      const t = ev.target
      const tag = t && t.tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || (t && t.isContentEditable)) return
      const n = selNode.value
      if (!n) return
      if (ev.key === 'Tab') { ev.preventDefault(); addChild(n) }
      else if (ev.key === 'Enter') { ev.preventDefault(); addSibling(n) }
      else if ((ev.key === 'Delete' || ev.key === 'Backspace')) { ev.preventDefault(); removeNode(n) }
    }
    onMounted(() => window.addEventListener('keydown', onKey, true))
    onBeforeUnmount(() => window.removeEventListener('keydown', onKey, true))

    // ---- 导出（SVG / PNG）：把当前布局序列化为独立 SVG ----
    function hexBlend(hex, base, ratio) {
      // 把 hex 以 ratio 比例混入 base（模拟 color-mix）
      const p = (h) => [parseInt(h.slice(1, 3), 16), parseInt(h.slice(3, 5), 16), parseInt(h.slice(5, 7), 16)]
      const a = p(hex), b = p(base)
      const m = a.map((v, i) => Math.round(v * ratio + b[i] * (1 - ratio)))
      return '#' + m.map((v) => v.toString(16).padStart(2, '0')).join('')
    }
    function esc(s) { return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;') }
    function nodeBox(n, depth, x, y) {
      // 估算节点盒尺寸（与 DOM 近似即可）
      const txt = String(n.text || '未命名')
      const isRoot = depth === 0
      const w = Math.max(64, txt.length * 14 + (isRoot ? 44 : 34))
      const h = isRoot ? 44 : 34
      return { w, h }
    }
    function buildSvgString() {
      if (!tree) return ''
      const pos = layoutState.positions
      const ids = Object.keys(pos)
      if (!ids.length) return ''
      let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity
      for (const k of ids) {
        const b = nodeBox(pos[k].n, pos[k].depth, pos[k].x, pos[k].y)
        minX = Math.min(minX, pos[k].x - b.w / 2); minY = Math.min(minY, pos[k].y - b.h / 2)
        maxX = Math.max(maxX, pos[k].x + b.w / 2); maxY = Math.max(maxY, pos[k].y + b.h / 2)
      }
      const pad = 40
      const W = Math.round(maxX - minX + pad * 2), H = Math.round(maxY - minY + pad * 2)
      const ox = (x) => (x - minX + pad).toFixed(1), oy = (y) => (y - minY + pad).toFixed(1)
      let s = `<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}">`
      s += `<rect width="${W}" height="${H}" fill="#0b1220"/>`
      for (const c of layoutState.conn) {
        s += `<path d="${c.path.replace(/(M|L)(\d+(\.\d+)?),(\d+(\.\d+)?)/g, (m, cmd, x1, _x, y1) => cmd + ox(parseFloat(x1)) + ',' + oy(parseFloat(y1)))}" fill="none" stroke="${colorAt(c.depth)}" stroke-width="2.2" stroke-linejoin="round" stroke-linecap="round" opacity=".8"/>`
      }
      for (const k of ids) {
        const p = pos[k]
        const b = nodeBox(p.n, p.depth, p.x, p.y)
        const col = colorAt(p.depth)
        const fill = hexBlend(col, p.isRoot ? '#0f172a' : '#101a2e', p.isRoot ? 0.3 : 0.24)
        const x = ox(p.x - b.w / 2), y = oy(p.y - b.h / 2)
        s += `<rect x="${x}" y="${y}" width="${b.w}" height="${b.h}" rx="${p.isRoot ? 18 : 12}" fill="${fill}" stroke="${col}" stroke-width="${p.isRoot ? 2 : 1.5}"/>`
        s += `<text x="${ox(p.x)}" y="${oy(p.y)}" text-anchor="middle" dominant-baseline="central" font-family="'PingFang SC','Microsoft YaHei',sans-serif" font-size="${p.isRoot ? 15 : 13}" font-weight="${p.isRoot ? 700 : 400}" fill="#f1f5f9">${esc(p.n.text)}</text>`
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
      themes, readonly: props.readonly, canvasEl, vp, world, positions, connectList,
      connCount, sel, editing, draft, setNodeInput, theme, dnd, selNode,
      colorAt, childrenOf,
      addChild, addSibling, toggleCollapse, removeNode, beginEdit, commit,
      onDragStart, onDropTo, onBgDown, onWheel, zoomBy, resetView, fit
    }
  }
}
</script>

<style>
.mm { position: relative; overflow: hidden; background: #0b1220; height: clamp(520px, calc(100vh - 480px), 1100px); display: flex; flex-direction: column; border-radius: 12px; }
.mm-toolbar { display: flex; align-items: center; gap: 6px; padding: 8px 10px; background: linear-gradient(180deg, #0f172a, #0b1220); border-bottom: 1px solid #1e293b; flex-wrap: wrap; }
.mm-tb-group { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.mm-tb-sep { width: 1px; height: 18px; background: #1e293b; }
.mm-tb-label { font-size: 12px; color: #94a3b8; }
.mm-theme { width: 16px; height: 16px; border-radius: 50%; border: 2px solid transparent; cursor: pointer; }
.mm-theme.on { border-color: #fff; box-shadow: 0 0 0 2px rgba(255,255,255,.2); }
.mm-tb-spacer { flex: 1; }
.mm-zoom { display: flex; align-items: center; gap: 4px; }
.mm-zoom-val { font-size: 12px; color: #94a3b8; width: 40px; text-align: center; }
.mm .seg { min-width: 28px; height: 26px; padding: 0 8px; font-size: 13px; line-height: 1; color: #cbd5e1; background: rgba(30,41,59,.6); border: 1px solid #273447; border-radius: 7px; cursor: pointer; }
.mm .seg:hover { border-color: #38bdf8; color: #38bdf8; }
.mm-canvas { flex: 1; position: relative; overflow: hidden; cursor: grab; }
.mm-canvas:active { cursor: grabbing; }
.mm-world { position: absolute; top: 0; left: 0; transform-origin: 0 0; }
.mm-grid { position: absolute; top: 0; left: 0; background-image: radial-gradient(rgba(148,163,184,.14) 1px, transparent 1.5px); background-size: 26px 26px; }
.mm-svg { position: absolute; top: 0; left: 0; pointer-events: none; }
.mm-line { fill: none; stroke-width: 2.2; stroke-linejoin: round; stroke-linecap: round; opacity: .8; }
.mm-node { position: absolute; display: flex; align-items: center; gap: 6px; transform: translate(-50%, -50%); color: #f1f5f9; background: linear-gradient(180deg, color-mix(in srgb, var(--mmc) 26%, #0f172a), color-mix(in srgb, var(--mmc) 14%, #0b1220)); border: 1.5px solid color-mix(in srgb, var(--mmc) 55%, transparent); border-radius: 14px; padding: 7px 13px; font-size: 13px; cursor: grab; user-select: none; box-shadow: 0 3px 12px rgba(0,0,0,.3); white-space: nowrap; z-index: 2; transition: box-shadow .15s, border-color .15s; }
.mm-node.root { font-size: 15px; font-weight: 700; border-radius: 18px; padding: 10px 18px; border-color: var(--mmc); box-shadow: 0 4px 18px color-mix(in srgb, var(--mmc) 30%, transparent); }
.mm-node.sel { box-shadow: 0 0 0 2px #fff, 0 5px 16px rgba(0,0,0,.35); z-index: 3; }
.mm-node.edit { z-index: 4; }
.mm-grip { color: color-mix(in srgb, var(--mmc) 70%, #fff); font-size: 13px; cursor: grab; }
.mm-input { background: #0f172a; color: #e2e8f0; border: 1px solid #38bdf8; border-radius: 6px; font-size: 13px; padding: 3px 6px; width: 160px; }
.mm-fold { color: color-mix(in srgb, var(--mmc) 70%, #fff); font-size: 10px; cursor: pointer; background: rgba(0,0,0,.25); border-radius: 50%; width: 16px; height: 16px; display: inline-flex; align-items: center; justify-content: center; }
.mm-hint { position: absolute; left: 10px; bottom: 8px; font-size: 11px; color: #64748b; background: rgba(15,23,42,.7); padding: 3px 8px; border-radius: 6px; pointer-events: none; }
.mm-empty { position: absolute; left: 50%; top: 50%; transform: translate(-50%, -50%); color: #475569; font-size: 13px; background: rgba(15,23,42,.6); padding: 14px 20px; border-radius: 10px; border: 1px dashed #334155; text-align: center; max-width: 340px; line-height: 1.7; }
.mm.roam { cursor: default; }
.mm.roam .mm-canvas { cursor: grab; }
.mm.roam .mm-node { cursor: default; }
.mm kbd { font-family: inherit; font-size: 10px; background: #1e293b; border-radius: 4px; padding: 1px 4px; color: #94a3b8; }
</style>
