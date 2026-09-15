<template>
  <!--
    KnowledgeTrash.vue —— 回收站视图（三栏布局下占据中+右两栏）
    v0.28.0 从 Workspace.vue 内联模板抽出。增补：显示剩余保留天数提示，
    避免用户不清楚「彻底删除」的时间窗口。
  -->
  <div class="kb-trash">
    <div v-if="!items.length" class="trash-empty">
      <div class="te-ico">🗑</div>
      <div class="te-txt">回收站是空的</div>
    </div>
    <template v-else>
      <div class="trash-bar">
        <span class="dim">共 {{ items.length }} 条 · 删除后的条目保留 30 天，逾期自动清理</span>
        <button class="btn ghost sm danger" @click="$emit('empty')">清空回收站</button>
      </div>
      <div class="trash-list">
        <div v-for="t in items" :key="t.id" class="trash-item">
          <div class="trash-main">
            <span class="trash-title">{{ t.title }}</span>
            <span class="dim trash-sub">
              {{ t.category || '未分类' }} · 删除于 {{ fmtTime(t.deleted_at) }}
            </span>
          </div>
          <div class="trash-ops">
            <button class="btn sm ok" @click="$emit('restore', t.id)">恢复</button>
            <button class="btn sm danger" @click="$emit('purge', t.id)">彻底删除</button>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
defineProps({
  items: { type: Array, default: () => [] },
  fmtTime: { type: Function, default: (s) => String(s || '') },
})
defineEmits(['restore', 'purge', 'empty'])
</script>

<style scoped>
.kb-trash { display: flex; flex-direction: column; gap: 12px; padding: 18px 20px; }
.trash-empty {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  gap: 10px; padding: 56px 20px;
}
.te-ico { font-size: 36px; opacity: .45; }
.te-txt { font-size: 13px; color: var(--text-faint); }
.trash-bar { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; font-size: 12px; }
.trash-list { display: flex; flex-direction: column; gap: 8px; }
.trash-item {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  border: 1px solid var(--glass-border); border-radius: 12px;
  padding: 11px 14px; background: var(--bg-1); flex-wrap: wrap;
}
.trash-main { display: flex; flex-direction: column; gap: 3px; min-width: 0; flex: 1 1 200px; }
.trash-title { font-weight: 600; font-size: 13.5px; }
.trash-sub { font-size: 11.5px; }
.trash-ops { display: flex; gap: 8px; flex: 0 0 auto; }

@media (max-width: 820px) {
  .kb-trash { padding: 14px; }
}
</style>
