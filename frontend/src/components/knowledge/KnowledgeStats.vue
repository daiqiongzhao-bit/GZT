<template>
  <!--
    KnowledgeStats.vue —— 知识库统计看板（三栏布局下占据中+右两栏）
    v0.28.0 从 Workspace.vue 内联模板抽出，逻辑不变，仅重排布。
  -->
  <div class="kb-stats">
    <div v-if="loading" class="dim kb-stats-loading">加载统计中…</div>
    <template v-else-if="stats">
      <div class="stat-grid">
        <div class="stat-card">
          <div class="stat-num">{{ stats.total }}</div>
          <div class="stat-lbl">知识总数</div>
        </div>
        <div class="stat-card">
          <div class="stat-num">{{ stats.recent_30d }}</div>
          <div class="stat-lbl">近 30 天新增</div>
        </div>
        <div class="stat-card">
          <div class="stat-num">{{ stats.my_starred }}</div>
          <div class="stat-lbl">我的收藏</div>
        </div>
      </div>

      <div class="stat-block">
        <div class="stat-h">分类分布</div>
        <div class="bar-chart">
          <div v-for="c in stats.categories" :key="c.name" class="bar-row">
            <span class="bar-name" :title="c.name">{{ c.name }}</span>
            <span class="bar-track"><span class="bar-fill" :style="{ width: pct(c.count, stats.categories) }"></span></span>
            <span class="bar-cnt">{{ c.count }}</span>
          </div>
          <div v-if="!stats.categories.length" class="dim">暂无分类数据</div>
        </div>
      </div>

      <div class="stat-block">
        <div class="stat-h">热门标签</div>
        <div class="tag-cloud">
          <span
            v-for="t in stats.tags"
            :key="t.name"
            class="tag-pill"
            :style="{ fontSize: tagSize(t.count, stats.tags) + 'px' }"
            @click="$emit('pick-tag', t.name)"
          >#{{ t.name }} <i>{{ t.count }}</i></span>
          <span v-if="!stats.tags.length" class="dim">暂无标签</span>
        </div>
      </div>

      <div class="stat-block">
        <div class="stat-h">贡献者</div>
        <div class="author-list">
          <span v-for="a in stats.authors" :key="a.name" class="author-pill">{{ a.name }} <i>{{ a.count }}</i></span>
          <span v-if="!stats.authors.length" class="dim">暂无</span>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
defineProps({
  stats: { type: Object, default: null },
  loading: { type: Boolean, default: false },
})
defineEmits(['pick-tag'])

function pct(count, list) {
  const max = Math.max(1, ...list.map((x) => x.count))
  return ((count / max) * 100).toFixed(0) + '%'
}
function tagSize(count, list) {
  const max = Math.max(1, ...list.map((x) => x.count))
  return 12 + Math.round((count / max) * 8)
}
</script>

<style scoped>
.kb-stats { display: flex; flex-direction: column; gap: 16px; padding: 18px 20px; }
.kb-stats-loading { padding: 20px 0; }
.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}
.stat-card {
  border: 1px solid var(--glass-border); border-radius: 14px;
  padding: 16px; background: var(--bg-1); text-align: center;
}
.stat-num { font-size: 26px; font-weight: 800; color: var(--accent); }
.stat-lbl { font-size: 12px; color: var(--text-faint); margin-top: 4px; }
.stat-block { border: 1px solid var(--glass-border); border-radius: 14px; padding: 14px; background: var(--bg-1); }
.stat-h { font-size: 13px; font-weight: 600; margin-bottom: 10px; }
.bar-chart { display: flex; flex-direction: column; gap: 7px; }
.bar-row { display: flex; align-items: center; gap: 9px; font-size: 12.5px; }
.bar-name { width: 110px; flex: 0 0 auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-dim); }
.bar-track { flex: 1 1 auto; height: 8px; background: var(--overlay-2); border-radius: 999px; overflow: hidden; }
.bar-fill { display: block; height: 100%; background: var(--accent); border-radius: 999px; }
.bar-cnt { width: 30px; flex: 0 0 auto; text-align: right; color: var(--text-faint); }
.tag-cloud { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.tag-pill {
  color: var(--accent); background: var(--accent-soft);
  border-radius: 999px; padding: 2px 10px; cursor: pointer;
  transition: filter .12s ease;
}
.tag-pill:hover { filter: brightness(.95); }
.tag-pill i { font-style: normal; opacity: .65; font-size: 10.5px; }
.author-list { display: flex; flex-wrap: wrap; gap: 6px; }
.author-pill { font-size: 12px; background: var(--overlay-2); border-radius: 999px; padding: 2px 10px; color: var(--text-dim); }
.author-pill i { font-style: normal; opacity: .6; font-size: 10.5px; }

@media (max-width: 820px) {
  .kb-stats { padding: 14px; }
  .bar-name { width: 78px; }
}
</style>
