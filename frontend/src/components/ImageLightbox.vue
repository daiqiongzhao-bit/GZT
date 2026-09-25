<template>
  <!--
    ImageLightbox.vue —— 全站「只读图文」点击放大（v0.40.10 抽出的全局组件）
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
      @click="close"
    >
      <button class="kbl-close" type="button" title="关闭（Esc）" @click.stop="close">✕</button>
      <img class="kbl-img" :src="state.src" :alt="state.alt" />
    </div>
  </Teleport>
</template>

<script setup>
import { reactive, onMounted, onBeforeUnmount } from 'vue'

// 仅这些只读内容区内的图片支持点开放大（正向白名单，避免误伤 UI 图标 / 二维码 / 编辑态图片）
const ZOOM_SCOPE = '.kb-read, .comment-body, [data-img-zoom]'

const state = reactive({ open: false, src: '', alt: '' })

function open(src, alt) {
  state.src = src
  state.alt = alt || ''
  state.open = true
}
function close() {
  state.open = false
}

function onClick(e) {
  const t = e && e.target
  if (!t || t.tagName !== 'IMG' || !t.closest) return
  // 只处理只读内容区内的图片
  if (!t.closest(ZOOM_SCOPE)) return
  const src = t.currentSrc || t.src
  if (!src) return
  open(src, t.alt)
}
function onKey(e) {
  if (e && e.key === 'Escape') close()
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
  cursor: zoom-out; backdrop-filter: blur(2px);
}
.kbl-img {
  max-width: 100%; max-height: 100%; width: auto; height: auto;
  border-radius: 10px; background: #fff;
  box-shadow: 0 24px 70px rgba(0, 0, 0, 0.55);
}
.kbl-close {
  position: fixed; top: 18px; right: 22px; z-index: 1;
  width: 38px; height: 38px; border-radius: 50%;
  border: 1px solid rgba(255, 255, 255, 0.3); background: rgba(255, 255, 255, 0.12);
  color: #fff; font-size: 16px; line-height: 1; cursor: pointer;
  transition: background .12s ease;
}
.kbl-close:hover { background: rgba(255, 255, 255, 0.24); }
</style>
