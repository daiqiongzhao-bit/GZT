<template>
  <!--
    ParentTreePicker.vue —— 「所属父级（层级树）」树形选择器
    设计要点：
      · 替换原来的扁平 <select>：条目多起来后扁平列表根本找不到父级
      · 自带搜索过滤；点空白处自动收起
      · 会剔除「自己 + 自己的所有后代」，从根上杜绝把条目挂成自己的子孙（成环）
  -->
  <div ref="root" class="ptp">
    <button
      type="button"
      class="ptp-btn"
      :class="{ open: open, empty: !modelValue }"
      @click="toggle"
    >
      <span class="ptp-txt">{{ currentLabel }}</span>
      <span class="ptp-caret">▾</span>
    </button>

    <div v-if="open" class="ptp-pop">
      <div class="ptp-search">
        <input v-model="kw" class="glass-input" placeholder="搜索条目…" @keydown.esc="open = false" />
      </div>
      <div class="ptp-list">
        <button
          type="button"
          class="ptp-row root"
          :class="{ on: !modelValue }"
          @click="pick(0)"
        >
          <span class="ptp-name">（顶级，无父级）</span>
        </button>
        <button
          v-for="n in visibleRows"
          :key="n.id"
          type="button"
          class="ptp-row"
          :class="{ on: String(modelValue) === String(n.id) }"
          :style="{ paddingLeft: 10 + n.depth * 15 + 'px' }"
          :title="n.title"
          @click="pick(n.id)"
        >
          <span class="ptp-mark">{{ n.depth ? (n.hasChildren ? '▾' : '·') : (n.hasChildren ? '▾' : '') }}</span>
          <span class="ptp-name">{{ n.title }}</span>
        </button>
        <div v-if="!visibleRows.length" class="ptp-empty dim">
          {{ kw.trim() ? '没有匹配的条目' : '还没有其它条目可作父级' }}
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'

const props = defineProps({
  modelValue: { type: [Number, String], default: 0 },
  /** 扁平条目列表，元素需含 { id, title, parent_id } */
  items: { type: Array, default: () => [] },
  /** 正在编辑的条目 id：必须把它自己和它的后代都排除，否则会挂成自己的子孙 */
  excludeId: { type: [Number, String], default: 0 },
})
const emit = defineEmits(['update:modelValue'])

const root = ref(null)
const open = ref(false)
const kw = ref('')

/** 排除自身与自身后代后，剩下来的可选条目 */
const usable = computed(() => {
  const all = props.items || []
  const selfId = String(props.excludeId || 0)
  const byParent = new Map()
  all.forEach((x) => {
    const p = String(x.parent_id || 0)
    if (!byParent.has(p)) byParent.set(p, [])
    byParent.get(p).push(x)
  })
  const banned = new Set()
  if (selfId !== '0') {
    banned.add(selfId)
    const stack = [selfId]
    while (stack.length) {
      const cur = stack.pop()
      for (const child of byParent.get(cur) || []) {
        const cid = String(child.id)
        if (!banned.has(cid)) { banned.add(cid); stack.push(cid) }
      }
    }
  }
  return all.filter((x) => !banned.has(String(x.id)))
})

/** 有子节点的 id 集合，用于渲染展开箭头 */
const withChildren = computed(() => {
  const s = new Set()
  usable.value.forEach((x) => { if (x.parent_id) s.add(String(x.parent_id)) })
  return s
})

/** 深度优先摊平；父级不在可选集合里的条目按根处理，避免整棵子树消失 */
const rows = computed(() => {
  const list = usable.value
  const map = new Map(list.map((x) => [String(x.id), { id: x.id, title: x.title || '（无标题）', children: [] }]))
  const roots = []
  list.forEach((x) => {
    const node = map.get(String(x.id))
    const pid = String(x.parent_id || 0)
    const parent = pid !== '0' && pid !== String(x.id) ? map.get(pid) : null
    if (parent) parent.children.push(node)
    else roots.push(node)
  })
  const out = []
  const walk = (nodes, depth) => {
    nodes.forEach((n) => {
      out.push({ id: n.id, title: n.title, depth, hasChildren: n.children.length > 0 })
      walk(n.children, depth + 1)
    })
  }
  walk(roots, 0)
  return out
})

const visibleRows = computed(() => {
  const k = kw.value.trim().toLowerCase()
  if (!k) return rows.value
  // 搜索态摊平展示，不再保留缩进层级（否则匹配到的深层节点会被上级淹没）
  return rows.value
    .filter((n) => String(n.title).toLowerCase().includes(k))
    .map((n) => ({ ...n, depth: 0 }))
})

const currentLabel = computed(() => {
  if (!props.modelValue) return '（顶级，无父级）'
  const hit = (props.items || []).find((x) => String(x.id) === String(props.modelValue))
  return hit ? hit.title : '（顶级，无父级）'
})

function toggle() {
  open.value = !open.value
  if (!open.value) kw.value = ''
}
function pick(id) {
  emit('update:modelValue', id)
  open.value = false
  kw.value = ''
}
function onDocClick(e) {
  if (root.value && !root.value.contains(e.target)) { open.value = false; kw.value = '' }
}
onMounted(() => document.addEventListener('click', onDocClick))
onBeforeUnmount(() => document.removeEventListener('click', onDocClick))
</script>

<style scoped>
.ptp { position: relative; min-width: 0; }

.ptp-btn {
  width: 100%; display: flex; align-items: center; justify-content: space-between; gap: 8px;
  border: 1px solid var(--glass-border); background: var(--overlay);
  color: var(--text); border-radius: 10px; padding: 8px 12px;
  font-size: 13px; cursor: pointer; text-align: left; min-width: 0;
}
.ptp-btn:hover { border-color: var(--accent); }
.ptp-btn.open { border-color: var(--accent); }
.ptp-txt { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ptp-btn.empty .ptp-txt { color: var(--text-faint); }
.ptp-caret { flex: 0 0 auto; font-size: 10px; color: var(--text-faint); }

.ptp-pop {
  position: absolute; left: 0; right: 0; top: calc(100% + 5px); z-index: 30;
  background: var(--bg-1); border: 1px solid var(--glass-border); border-radius: 12px;
  box-shadow: 0 12px 30px var(--overlay); overflow: hidden;
}
.ptp-search { padding: 7px; border-bottom: 1px solid var(--hairline); }
.ptp-search .glass-input { font-size: 12.5px; padding: 6px 9px; }
.ptp-list { max-height: 260px; overflow-y: auto; padding: 5px; scrollbar-width: thin; }
.ptp-list::-webkit-scrollbar { width: 7px; }
.ptp-list::-webkit-scrollbar-thumb { background: var(--glass-border); border-radius: 4px; }

.ptp-row {
  width: 100%; display: flex; align-items: center; gap: 5px;
  border: 0; background: transparent; color: var(--text);
  padding: 6px 10px; border-radius: 8px; cursor: pointer;
  font-size: 12.5px; text-align: left; min-width: 0;
}
.ptp-row:hover { background: var(--overlay-2); }
.ptp-row.on { background: var(--accent-soft); color: var(--accent); font-weight: 600; }
.ptp-row.root { margin-bottom: 3px; border-bottom: 1px solid var(--hairline); border-radius: 8px 8px 0 0; }
.ptp-mark { flex: 0 0 11px; font-size: 9px; color: var(--text-faint); }
.ptp-name { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ptp-empty { padding: 9px 10px; font-size: 12px; text-align: center; }
</style>
