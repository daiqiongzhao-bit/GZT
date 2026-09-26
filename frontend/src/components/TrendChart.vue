<template>
  <div class="trend-card">
    <div class="section-head">
      <h3 class="section-title">近 {{ trends.days }} 天业务趋势 <span class="section-sub">完成任务 · 新建任务 · 排班人数 · 知识库新增</span></h3>
    </div>
    <div class="trend-legend">
      <span v-for="m in trendMetrics" :key="m.key" class="trend-legend-item">
        <i class="trend-dot" :style="{ background: m.color }"></i>{{ m.label }}
      </span>
    </div>
    <svg v-if="trendSvg" class="trend-svg" :viewBox="`0 0 ${trendW} ${trendH}`">
      <line v-for="g in 4" :key="'g' + g" :x1="trendPad" :x2="trendW - trendPad"
            :y1="trendPad + (trendH - 2 * trendPad) * (g - 1) / 4" :y2="trendPad + (trendH - 2 * trendPad) * (g - 1) / 4" class="trend-grid" />
      <polyline v-for="l in trendSvg.lines" :key="l.key" :points="l.pts"
                :stroke="l.color" fill="none" stroke-width="2" stroke-linejoin="round" stroke-linecap="round" />
    </svg>
    <EmptyState v-else title="暂无趋势数据" desc="近 30 天内没有可统计的业务数据。" :icon="chartIcon" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import * as api from '@/api'
import EmptyState from '@/components/EmptyState.vue'

// v0.41.3：从 Dashboard 拆出为独立组件，放到设置页「数据趋势」tab。
const trends = ref({ days: 30, series: [] })
const trendMetrics = [
  { key: 'tasks_done', label: '完成任务', color: '#22c55e' },
  { key: 'tasks_created', label: '新建任务', color: '#3b82f6' },
  { key: 'on_duty_people', label: '排班人数', color: '#f59e0b' },
  { key: 'knowledge_added', label: '知识库新增', color: '#a855f7' },
]
const trendW = 720, trendH = 220, trendPad = 30
const trendSvg = computed(() => {
  const s = trends.value.series || []
  if (!s.length) return null
  const n = s.length
  const x = (i) => trendPad + (trendW - 2 * trendPad) * (n === 1 ? 0.5 : i / (n - 1))
  const lines = trendMetrics.map((m) => {
    const vals = s.map((r) => r[m.key] || 0)
    const max = Math.max(1, ...vals)
    const pts = vals.map((v, i) => `${x(i).toFixed(1)},${(trendH - trendPad - (trendH - 2 * trendPad) * (v / max)).toFixed(1)}`).join(' ')
    return { ...m, pts }
  })
  return { lines }
})

const chartIcon = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M3 3v18h18"/><path d="M7 16v-3"/><path d="M11 16V8"/><path d="M15 16v-5"/><path d="M19 16v-7"/></svg>'

onMounted(async () => {
  try {
    trends.value = await api.get('/dashboard/trends')
  } catch {
    trends.value = { days: 30, series: [] }
  }
})
</script>

<style scoped>
.trend-card { padding: 16px 18px; }
.trend-legend { display: flex; flex-wrap: wrap; gap: 14px; margin-bottom: 10px; }
.trend-legend-item { display: inline-flex; align-items: center; gap: 6px; font-size: 12.5px; color: var(--text-dim); }
.trend-dot { width: 10px; height: 10px; border-radius: 50%; display: inline-block; }
.trend-svg { width: 100%; height: auto; display: block; }
.trend-grid { stroke: var(--hairline); stroke-width: 1; }
@media (max-width: 560px) {
  .trend-card { padding: 12px; }
}
</style>
