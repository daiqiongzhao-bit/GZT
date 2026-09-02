<template>
  <div class="empty-state">
    <div class="es-ico" v-html="icon || defaultIcon"></div>
    <div class="es-title">{{ title }}</div>
    <div class="es-desc">{{ desc }}</div>
    <slot />
    <button v-if="actionText" class="btn primary es-btn" @click="$emit('action')" v-html="actionText"></button>
  </div>
</template>

<script setup>
defineProps({
  title: { type: String, default: '暂无数据' },
  desc: { type: String, default: '' },
  icon: { type: String, default: '' },
  actionText: { type: String, default: '' }
})
defineEmits(['action'])

const defaultIcon =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="3"/><path d="M3 9h18M8 2v4M16 2v4"/></svg>'
</script>

<style scoped>
.empty-state {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  text-align: center; padding: 40px 20px; color: var(--text-faint);
  border: 1px dashed var(--glass-border-strong); border-radius: var(--radius);
  background: var(--overlay); margin: 8px 0;
}
.es-ico { width: 56px; height: 56px; color: var(--accent-soft-fg, var(--accent)); opacity: 0.85; margin-bottom: 12px; }
.es-ico :deep(svg) { width: 100%; height: 100%; }
.es-title { font-size: 15px; font-weight: 700; color: var(--text); margin-bottom: 6px; }
.es-desc { font-size: 13px; color: var(--text-dim); max-width: 320px; line-height: 1.6; }
.es-btn { margin-top: 16px; }
</style>
