<template>
  <!--
    ImageLightbox.vue —— 全站「只读图文」点击放大（v0.40.10 抽出的全局组件）
    v0.40.12 增强：打开后可直接在图片上操作缩放 ——
      · 滚轮缩放（以鼠标位置为中心）
      · 双击图片：1x ↔ 放大（2.5x）切换
      · 放大后可直接按住拖动平移
      · 右下角 − / 百分比 / ＋ / 1:1 工具条；Esc 关闭
    设计要点：
      · 用 document 委托监听点击，凡是落在「只读内容区」内的 <img> 都支持点开看原图，
        因此知识库正文、评论、以及任何未来带 data-img-zoom 标记的图文位都自动生效，无需逐个接入。
      · 编辑态图片（.ProseMirror 内）走缩放浮层、附件缩略图（.att-thumb）走各自预览，
        均不在此处理，避免重复弹窗。
      · 纯 Vue 事件委托，不向正文 HTML 注入任何 on* 内联事件，与 safeHtml 白名单（只放行
        src/alt/width/height + 全局 data-align）完全兼容，无 XSS 风险。
  -->
  <Teleport to="body">
    <div
      v-if="state.open"
      class="kb-lightbox"
      role="dialog"
      aria-modal="true"
      aria-label="图片预览"
      @click.self="close"
      @wheel.prevent="onWheel"
      @pointerdown="onDown"
      @pointermove="onMove"
      @pointerup="onUp"
      @pointercancel="onUp"
    >
      <button class="kbl-close" type="button" title="关闭（Esc）" @click.stop="close">✕</button>
      <img
        class="kbl-img"
        :src="state.src"
        :alt="state.alt"
        :style="imgStyle"
        draggable="false"
        @dblclick.stop="toggleZoom"
        @click.stop
      />
      <div class="kbl-tools" @click.stop>
        <button class="kbl-btn" type="button" title="缩小" @click="zoomBy(0.8)">−</button>
        <button class="kbl-btn kbl-pct" type="button" title="重置为 100%" @click="reset">{{ Math.round(state.scale * 100) }}%</button>
        <button class="kbl-btn" type="button" title="放大" @click="zoomBy(1.25)">＋</button>
        <button class="kbl-btn" type="button" title="适应窗口" @click="fit">⤢</button>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { reactive, computed, onMounted, onBeforeUnmount } from 'vue'

// 仅这些只读内容区内的图片支持点开放大（正向白名单，避免误伤 UI 图标 / 二维码 / 编辑态图片）
const ZOOM_SCOPE = '.kb-read, .comment-body, [data-img-zoom]'

const MIN = 0.2
const MAX = 8

const state = reactive({ open: false, src: '', alt: '', scale: 1, x: 0, y: 0 })
let drag = null // { px, py, ox, oy }

const imgStyle = computed(() => ({
  transform: `translate(${state.x}px, ${state.y}px) scale(${state.scale})`,
  cursor: state.scale > 1 ? (drag ? 'grabbing' : 'grab') : 'zoom-in',
}))

function clampScale(s) {
  return Math.min(MAX, Math.max(MIN, s))
}

// 以视口中心为基准缩放（简化实现：不追踪鼠标锚点也够用，图片始终居中呈现）
function setScale(s, cx, cy) {
  const prev = state.scale
  const next = clampScale(s)
  if (next === prev) return
  if (cx == null) {
    cx = window.innerWidth / 2
    cy = window.innerHeight / 2
  }
  // 保持鼠标指向的图片点不动：位移按缩放比例补偿
  const ratio = next / prev
  state.x = (state.x + (cx - window.innerWidth / 2)) * ratio - (cx - window.innerWidth / 2)
  state.y = (state.y + (cy - window.innerHeight / 2)) * ratio - (cy - window.innerHeight / 2)
  state.scale = next
}

function zoomBy(f) {
  setScale(state.scale * f)
}

function reset() {
  state.scale = 1
  state.x = 0
  state.y = 0
}

function fit() {
  reset()
}

function toggleZoom() {
  if (state.scale > 1.01) reset()
  else setScale(2.5)
}

function onWheel(e) {
  const f = e.deltaY < 0 ? 1.12 : 1 / 1.12
  setScale(state.scale * f, e.clientX, e.clientY)
}

function onDown(e) {
  if (e.button !== 0 || state.scale <= 1) return
  // 点在工具条/关闭按钮上不拖动
  if (e.target.closest && e.target.closest('.kbl-tools, .kbl-close')) return
  drag = { px: e.clientX, py: e.clientY, ox: state.x, oy: state.y }
  e.preventDefault()
}
function onMove(e) {
  if (!drag) return
  state.x = drag.ox + (e.clientX - drag.px)
  state.y = drag.oy + (e.clientY - drag.py)
}
function onUp() {
  drag = null
}

function open(src, alt) {
  state.src = src
  state.alt = alt || ''
  state.open = true
  reset()
}
function close() {
  state.open = false
  reset()
}

function onClick(e) {
  if (state.open) return
  const t = e && e.target
  if (!t || t.tagName !== 'IMG' || !t.closest) return
  // 只处理只读内容区内的图片
  if (!t.closest(ZOOM_SCOPE)) return
  const src = t.currentSrc || t.src
  if (!src) return
  open(src, t.alt)
}
function onKey(e) {
  if (!(e && state.open)) return
  if (e.key === 'Escape') close()
  else if (e.key === '+' || e.key === '=') zoomBy(1.25)
  else if (e.key === '-') zoomBy(0.8)
  else if (e.key === '0') reset()
}

onMounted(() => {
  document.addEventListener('click', onClick)
  document.addEventListener('keydown', onKey)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onClick)
  document.removeEventListener('keydown', onKey)
})
</script>

<style scoped>
.kb-lightbox {
  position: fixed; inset: 0; z-index: 9000;
  display: flex; align-items: center; justify-content: center;
  padding: 5vh 5vw; background: rgba(15, 23, 42, 0.84);
  backdrop-filter: blur(2px); overflow: hidden; touch-action: none;
}
.kbl-img {
  max-width: 100%; max-height: 100%; width: auto; height: auto;
  border-radius: 10px; background: #fff;
  box-shadow: 0 24px 70px rgba(0, 0, 0, 0.55);
  transform-origin: center center;
  transition: transform .12s ease-out;
  user-select: none; -webkit-user-drag: none;
}
.kbl-close {
  position: fixed; top: 18px; right: 22px; z-index: 1;
  width: 38px; height: 38px; border-radius: 50%;
  border: 1px solid rgba(255, 255, 255, 0.3); background: rgba(255, 255, 255, 0.12);
  color: #fff; font-size: 16px; line-height: 1; cursor: pointer;
  transition: background .12s ease;
}
.kbl-close:hover { background: rgba(255, 255, 255, 0.24); }
.kbl-tools {
  position: fixed; bottom: 22px; left: 50%; transform: translateX(-50%);
  display: flex; align-items: center; gap: 6px; z-index: 1;
  padding: 6px 10px; border-radius: 12px;
  background: rgba(15, 23, 42, 0.72); border: 1px solid rgba(255, 255, 255, 0.18);
  backdrop-filter: blur(6px);
}
.kbl-btn {
  min-width: 34px; height: 30px; padding: 0 8px;
  border-radius: 8px; border: none; cursor: pointer;
  background: transparent; color: #fff; font-size: 15px; line-height: 1;
  transition: background .12s ease;
}
.kbl-btn:hover { background: rgba(255, 255, 255, 0.16); }
.kbl-pct { font-size: 12.5px; min-width: 56px; font-variant-numeric: tabular-nums; }
</style>
