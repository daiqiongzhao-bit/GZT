<template>
  <!--
    KnowledgeList.vue —— 知识库三栏布局 · 中栏（条目列表）
    v0.28.0 新增。职责边界：
      · 只渲染列表 + 筛选栏 + 空态，不负责正文渲染与编辑
      · 所有权限判断（canEdit/canManage）由父级通过函数 prop 注入，
        避免权限规则在两处实现产生分歧
  -->
  <section class="kb-list-pane">
    <!-- 顶部工具栏 -->
    <header class="kb-list-head">
      <div class="kb-search-row">
        <input
          :value="query"
          class="kb-search"
          type="search"
          placeholder="🔍 搜索标题 / 内容 / 分类 / 标签"
          @input="$emit('update:query', $event.target.value)"
        />
        <button class="kb-new" title="新建知识条目" @click="$emit('create')">+</button>
      </div>

      <div class="kb-filter-row">
        <select class="kb-sel" :value="sort" @change="$emit('update:sort', $event.target.value)" title="排序方式">
          <option value="recent">最近更新</option>
          <option value="created">最新创建</option>
          <option value="hot">最热（阅读量）</option>
        </select>
        <button
          class="kb-toggle"
          :class="{ on: starred }"
          title="只看收藏"
          @click="$emit('update:starred', !starred)"
        >⭐</button>
        <button
          class="kb-toggle"
          :class="{ on: mine }"
          title="只看我写的"
          @click="$emit('update:mine', !mine)"
        >✍</button>
        <slot name="actions"></slot>
      </div>

      <!-- 生效中的筛选条件（可逐个移除） -->
      <div v-if="activeFilters.length" class="kb-active-filters">
        <span
          v-for="f in activeFilters"
          :key="f.key"
          class="kb-af"
          @click="$emit('clear-filter', f.key)"
        >{{ f.label }} <i>✕</i></span>
      </div>

      <div class="kb-count dim">
        共 {{ items.length }} 条
        <span v-if="items.length && filtered"> · 筛选后</span>
      </div>
    </header>

    <!-- 列表主体 -->
    <div class="kb-list-body">
      <div v-if="loading" class="kb-skel">
        <div v-for="i in 4" :key="i" class="kb-skel-item">
          <span class="sk-l1"></span><span class="sk-l2"></span><span class="sk-l3"></span>
        </div>
      </div>

      <template v-else-if="items.length">
        <article
          v-for="k in items"
          :key="k.id"
          class="kb-item"
          :class="{ on: String(selectedId) === String(k.id), mine: k.owner_id === currentUserId, pinned: k.pinned }"
          @click="$emit('open', k)"
        >
          <div class="ki-head">
            <span v-if="k.pinned" class="ki-pin" title="已置顶">📌</span>
            <span class="ki-title" v-html="highlight(k.title, query)"></span>
            <span
              class="ki-star"
              :class="{ on: k.starred }"
              :title="k.starred ? '取消收藏' : '收藏'"
              @click.stop="$emit('star', k)"
            >{{ k.starred ? '★' : '☆' }}</span>
          </div>

          <div class="ki-preview" v-html="highlight(preview(k), query)"></div>

          <div class="ki-foot">
            <span class="ki-chip" :class="scopeChip(k.scope)">{{ scopeLabel(k.scope) }}</span>
            <span v-if="k.category" class="ki-chip accent">{{ k.category }}</span>
            <span v-if="k.status === 'draft'" class="ki-chip warn">草稿</span>
            <span v-for="t in parseTags(k.tags).slice(0, 2)" :key="t" class="ki-tag">#{{ t }}</span>
            <span class="ki-meta dim">
              {{ k.owner_name === currentUserName ? '我' : k.owner_name }} · {{ fmtTime(k.updated_at || k.created_at) }}
              <template v-if="k.view_count"> · 👁 {{ k.view_count }}</template>
            </span>
          </div>

          <div class="ki-ops" @click.stop>
            <button v-if="canEdit(k)" class="ki-op" @click="$emit('edit', k)">编辑</button>
            <button
              v-if="canManage(k)"
              class="ki-op"
              :title="k.pinned ? '取消置顶' : '置顶'"
              @click="$emit('pin', k)"
            >{{ k.pinned ? '取消置顶' : '置顶' }}</button>
            <button v-if="canManage(k)" class="ki-op danger" @click="$emit('remove', k)">删除</button>
          </div>
        </article>
      </template>

      <div v-else class="kb-empty">
        <div class="kb-empty-ico">📭</div>
        <div class="kb-empty-txt">{{ emptyText }}</div>
        <button v-if="!filtered" class="btn primary sm" @click="$emit('create')">+ 新建第一条</button>
      </div>
    </div>
  </section>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  items: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
  selectedId: { type: [String, Number], default: '' },
  query: { type: String, default: '' },
  sort: { type: String, default: 'recent' },
  category: { type: String, default: '' },
  tag: { type: String, default: '' },
  parentTitle: { type: String, default: '' },
  mine: { type: Boolean, default: false },
  starred: { type: Boolean, default: false },
  currentUserId: { type: [String, Number], default: 0 },
  currentUserName: { type: String, default: '' },
  // 注入式工具函数：权限与格式化规则由父级单一来源提供
  canEdit: { type: Function, default: () => () => false },
  canManage: { type: Function, default: () => () => false },
  fmtTime: { type: Function, default: (s) => String(s || '') },
  preview: { type: Function, default: () => '' },
  highlight: { type: Function, default: (s) => String(s || '') },
  parseTags: { type: Function, default: () => [] },
  scopeLabel: { type: Function, default: (s) => String(s || '') },
  scopeChip: { type: Function, default: () => '' },
})

defineEmits([
  'open', 'create', 'edit', 'remove', 'star', 'pin',
  'update:query', 'update:sort', 'update:mine', 'update:starred',
  'clear-filter',
])

const filtered = computed(() =>
  !!(props.query || props.category || props.tag || props.parentTitle || props.mine || props.starred)
)

const activeFilters = computed(() => {
  const out = []
  if (props.query) out.push({ key: 'query', label: `搜索：${props.query}` })
  if (props.category) out.push({ key: 'category', label: `分类：${props.category}` })
  if (props.tag) out.push({ key: 'tag', label: `#${props.tag}` })
  if (props.parentTitle) out.push({ key: 'parent', label: `目录：${props.parentTitle}` })
  if (props.mine) out.push({ key: 'mine', label: '只看我写的' })
  if (props.starred) out.push({ key: 'starred', label: '只看收藏' })
  return out
})

const emptyText = computed(() =>
  filtered.value ? '没有匹配的知识条目，试试放宽筛选条件' : '还没有知识条目，点「+」沉淀第一条吧'
)
</script>

<style scoped>
.kb-list-pane {
  display: flex;
  flex-direction: column;
  min-width: 0;
  height: 100%;
  border-left: 1px solid var(--hairline);
  border-right: 1px solid var(--hairline);
  background: var(--bg-1);
}

/* 顶部工具栏：窄屏不换行、不横向滚动 */
.kb-list-head {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 12px 10px;
  border-bottom: 1px solid var(--hairline);
}
.kb-search-row { display: flex; align-items: center; gap: 8px; }
.kb-search {
  flex: 1 1 auto;
  min-width: 0;
  border: 1px solid var(--glass-border);
  background: var(--overlay);
  color: var(--text);
  border-radius: 10px;
  padding: 7px 11px;
  font-size: 13px;
  outline: none;
}
.kb-search:focus { border-color: var(--accent); }
.kb-search::-webkit-search-cancel-button { cursor: pointer; }
.kb-new {
  flex: 0 0 auto;
  width: 32px; height: 32px;
  border: none; border-radius: 10px;
  background: var(--accent); color: #fff;
  font-size: 19px; line-height: 1; cursor: pointer;
  display: inline-flex; align-items: center; justify-content: center;
  transition: filter .12s ease;
}
.kb-new:hover { filter: brightness(1.08); }

.kb-filter-row { display: flex; align-items: center; gap: 6px; min-width: 0; }
.kb-sel {
  flex: 1 1 auto; min-width: 0;
  border: 1px solid var(--glass-border);
  background: var(--overlay);
  color: var(--text-dim);
  border-radius: 9px; padding: 5px 8px; font-size: 12px; outline: none; cursor: pointer;
}
.kb-toggle {
  flex: 0 0 auto;
  border: 1px solid var(--glass-border); background: transparent;
  color: var(--text-dim); border-radius: 9px;
  padding: 4px 8px; font-size: 12.5px; cursor: pointer;
  transition: all .12s ease;
}
.kb-toggle:hover { border-color: var(--accent); color: var(--accent); }
.kb-toggle.on { background: var(--accent-soft); border-color: var(--accent); color: var(--accent); }

.kb-active-filters { display: flex; flex-wrap: wrap; gap: 5px; }
.kb-af {
  display: inline-flex; align-items: center; gap: 4px;
  font-size: 11px; color: var(--accent);
  background: var(--accent-soft); border-radius: 999px;
  padding: 2px 8px; cursor: pointer; max-width: 100%;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.kb-af i { font-style: normal; opacity: .7; }
.kb-af:hover i { opacity: 1; }
.kb-count { font-size: 11px; }

/* 列表主体 */
.kb-list-body { flex: 1 1 auto; overflow-y: auto; overflow-x: hidden; padding: 8px; scrollbar-width: thin; }
.kb-list-body::-webkit-scrollbar { width: 7px; }
.kb-list-body::-webkit-scrollbar-thumb { background: var(--glass-border); border-radius: 4px; }

.kb-item {
  position: relative;
  border: 1px solid transparent;
  border-radius: 11px;
  padding: 10px 11px;
  margin-bottom: 6px;
  cursor: pointer;
  display: flex; flex-direction: column; gap: 6px;
  transition: background .12s ease, border-color .12s ease;
  min-width: 0;
}
.kb-item:hover { background: var(--overlay); border-color: var(--glass-border); }
.kb-item.on { background: var(--accent-soft); border-color: var(--accent); }
.kb-item.mine::before,
.kb-item.pinned::before {
  content: ''; position: absolute; left: 0; top: 9px; bottom: 9px;
  width: 2.5px; border-radius: 0 3px 3px 0;
}
.kb-item.mine::before { background: var(--accent); }
.kb-item.pinned::before { background: #d97706; }

.ki-head { display: flex; align-items: flex-start; gap: 6px; min-width: 0; }
.ki-pin { flex: 0 0 auto; font-size: 11px; line-height: 1.5; }
.ki-title {
  flex: 1 1 auto; min-width: 0;
  font-size: 13.5px; font-weight: 700; line-height: 1.45; color: var(--text);
  overflow: hidden; text-overflow: ellipsis;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical;
}
.ki-star {
  flex: 0 0 auto; font-size: 14px; line-height: 1;
  color: var(--text-faint); cursor: pointer; padding: 0 2px;
}
.ki-star.on { color: #f59e0b; }
.ki-star:hover { color: #f59e0b; }

.ki-preview {
  font-size: 12px; line-height: 1.6; color: var(--text-dim);
  overflow: hidden; display: -webkit-box;
  -webkit-line-clamp: 2; -webkit-box-orient: vertical;
  word-break: break-word;
}
.ki-preview :deep(mark), .ki-title :deep(mark) {
  background: #fde68a; color: inherit; border-radius: 3px; padding: 0 1px;
}

.ki-foot { display: flex; align-items: center; gap: 5px; flex-wrap: wrap; font-size: 10.5px; }
.ki-chip {
  border-radius: 999px; padding: 1px 7px;
  background: var(--overlay-2); color: var(--text-faint);
  white-space: nowrap; max-width: 110px;
  overflow: hidden; text-overflow: ellipsis;
}
.ki-chip.accent { background: var(--accent-soft); color: var(--accent); }
.ki-chip.warn { background: rgba(217, 119, 6, .14); color: #d97706; }
.ki-tag { color: var(--accent); white-space: nowrap; }
.ki-meta { margin-left: auto; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

.ki-ops {
  display: flex; gap: 5px; flex-wrap: wrap;
  max-height: 0; overflow: hidden; opacity: 0;
  transition: max-height .18s ease, opacity .18s ease, margin-top .18s ease;
}
.kb-item:hover .ki-ops, .kb-item.on .ki-ops { max-height: 32px; opacity: 1; margin-top: 2px; }
.ki-op {
  border: 1px solid var(--glass-border); background: transparent;
  color: var(--text-dim); font-size: 11.5px; cursor: pointer;
  border-radius: 7px; padding: 2px 9px;
  transition: all .12s ease;
}
.ki-op:hover { border-color: var(--accent); color: var(--accent); }
.ki-op.danger:hover { border-color: #dc2626; color: #dc2626; }

/* 空态与骨架屏 */
.kb-empty {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  gap: 10px; padding: 48px 20px; text-align: center;
}
.kb-empty-ico { font-size: 34px; opacity: .5; }
.kb-empty-txt { font-size: 12.5px; color: var(--text-faint); line-height: 1.7; }

.kb-skel { display: flex; flex-direction: column; gap: 8px; padding: 4px; }
.kb-skel-item {
  border: 1px solid var(--glass-border); border-radius: 11px;
  padding: 12px; display: flex; flex-direction: column; gap: 8px;
}
.kb-skel-item span {
  display: block; height: 10px; border-radius: 5px;
  background: linear-gradient(90deg, var(--overlay) 25%, var(--overlay-2) 50%, var(--overlay) 75%);
  background-size: 200% 100%; animation: kb-shimmer 1.3s infinite;
}
.sk-l1 { width: 62%; height: 12px !important; }
.sk-l2 { width: 100%; }
.sk-l3 { width: 42%; }
@keyframes kb-shimmer {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}
</style>
