<template>
  <div class="apidocs">
    <div class="ad-head">
      <h3 class="section-title">{{ spec.info?.title }} <span class="section-sub">v{{ spec.info?.version }}</span></h3>
      <button class="btn ghost" @click="showJson = !showJson">{{ showJson ? '收起 JSON' : '查看原始 JSON' }}</button>
    </div>
    <p class="ad-desc">{{ spec.info?.description }}</p>
    <pre v-if="showJson" class="ad-json">{{ jsonText }}</pre>

    <div v-for="grp in groups" :key="grp.tag" class="ad-group">
      <h4 class="ad-group-h">{{ grp.tag }} <span class="section-sub">{{ grp.items.length }} 个接口</span></h4>
      <div class="ad-item" v-for="it in grp.items" :key="it.path + it.method">
        <span class="ad-method" :class="mClass(it.method)">{{ it.method.toUpperCase() }}</span>
        <div class="ad-body">
          <div class="ad-path">{{ it.path }}</div>
          <div class="ad-summary">{{ it.summary }}</div>
          <div class="ad-perm">所需权限：<code>{{ it.perm || '（无 / 公开）' }}</code></div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import * as api from '@/api'

const spec = ref({})
const showJson = ref(false)
const jsonText = computed(() => JSON.stringify(spec.value, null, 2))

const groups = computed(() => {
  const paths = spec.value.paths || {}
  const byTag = {}
  const order = []
  for (const path of Object.keys(paths)) {
    const item = paths[path] || {}
    for (const m of Object.keys(item)) {
      const op = item[m] || {}
      const tag = (op.tags && op.tags[0]) || '其他'
      if (!byTag[tag]) {
        byTag[tag] = []
        order.push(tag)
      }
      byTag[tag].push({
        path,
        method: m,
        summary: op.summary || '',
        perm: op['x-permission'] || '',
      })
    }
  }
  return order.map((tag) => ({ tag, items: byTag[tag] }))
})

function mClass(m) {
  return { get: 'get', post: 'post', put: 'put', delete: 'del', patch: 'patch' }[m] || 'get'
}

onMounted(async () => {
  try {
    spec.value = await api.get('/openapi.json')
  } catch (e) {
    spec.value = {}
  }
})
</script>

<style scoped>
.apidocs { max-width: 980px; margin: 0 auto; }
.ad-head { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
.ad-desc { color: var(--text-dim); font-size: 13px; line-height: 1.8; margin: 8px 0 16px; white-space: pre-wrap; }
.ad-json {
  background: var(--bg-1); border: 1px solid var(--glass-border); border-radius: var(--radius);
  padding: 16px; font-size: 12px; line-height: 1.6; overflow-x: auto; max-height: 480px; color: var(--text-dim);
}
.ad-group { margin-bottom: 22px; }
.ad-group-h { font-size: 15px; margin: 0 0 10px; display: flex; align-items: center; gap: 10px; }
.ad-item {
  display: flex; gap: 12px; padding: 12px 14px; border: 1px solid var(--hairline);
  border-radius: 12px; margin-bottom: 8px; background: var(--bg-1);
}
.ad-method {
  flex: none; width: 58px; text-align: center; font-size: 11px; font-weight: 700; letter-spacing: 0.5px;
  padding: 4px 0; border-radius: 8px; align-self: flex-start;
}
.ad-method.get { background: rgba(59,130,246,0.15); color: #3b82f6; }
.ad-method.post { background: rgba(34,197,94,0.15); color: #22c55e; }
.ad-method.put { background: rgba(245,158,11,0.15); color: #f59e0b; }
.ad-method.del { background: rgba(239,68,68,0.15); color: #ef4444; }
.ad-method.patch { background: rgba(168,85,247,0.15); color: #a855f7; }
.ad-body { min-width: 0; }
.ad-path { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13.5px; font-weight: 600; word-break: break-all; }
.ad-summary { font-size: 13px; color: var(--text-dim); margin: 2px 0 4px; }
.ad-perm { font-size: 12px; color: var(--text-faint); }
.ad-perm code { background: var(--overlay-2); padding: 1px 6px; border-radius: 6px; font-size: 11.5px; color: var(--accent); }
</style>
