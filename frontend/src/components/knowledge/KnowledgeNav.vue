<template>
  <!--
    KnowledgeNav.vue —— 知识库三栏布局 · 左栏（导航）
    v0.28.0 新增。职责边界：
      · 只负责「选哪个视图 / 哪个目录 / 哪个分类 / 哪个标签」
      · 不发请求、不碰数据：所有状态通过 props 传入，动作通过 emits 抛出，
        数据加载仍由 Workspace.vue 统一调度（单一数据源，避免多处刷新不一致）
      · 宽度自适应：由父级 .kb-3col 的 grid 列宽变量控制，本组件只管撑满
  -->
  <aside class="kb-nav">
    <!-- 视图切换 -->
    <nav class="kb-nav-group" aria-label="视图切换">
      <button class="kb-nav-item" :class="{ on: view === 'list' }" @click="$emit('view', 'list')">
        <span class="ni-ico">📚</span><span class="ni-txt">全部条目</span>
        <span class="ni-badge">{{ total }}</span>
      </button>
      <button class="kb-nav-item" :class="{ on: view === 'stats' }" @click="$emit('view', 'stats')">
        <span class="ni-ico">📊</span><span class="ni-txt">数据统计</span>
      </button>
      <button class="kb-nav-item" :class="{ on: view === 'trash' }" @click="$emit('view', 'trash')">
        <span class="ni-ico">🗑</span><span class="ni-txt">回收站</span>
        <span v-if="trashCount > 0" class="ni-badge warn">{{ trashCount }}</span>
      </button>
    </nav>

    <!-- 我的快捷筛选 -->
    <div class="kb-nav-group">
      <div class="kb-nav-h">快捷筛选</div>
      <button class="kb-nav-item sm" :class="{ on: starred }" @click="$emit('toggle-starred')">
        <span class="ni-ico">⭐</span><span class="ni-txt">我的收藏</span>
      </button>
      <button class="kb-nav-item sm" :class="{ on: mine }" @click="$emit('toggle-mine')">
        <span class="ni-ico">✍</span><span class="ni-txt">我写的</span>
      </button>
    </div>

    <!-- 目录树（按 parent_id 组织） -->
    <div class="kb-nav-group grow">
      <div class="kb-nav-h">
        <span>📁 目录</span>
        <button v-if="parentId" class="kb-nav-clear" title="清除目录筛选" @click="$emit('pick-parent', '')">清除</button>
      </div>
      <ul class="kb-tree">
        <li>
          <span class="kb-tree-node" :class="{ on: parentId === '' }" @click="$emit('pick-parent', '')">
            <span class="tn-ico">·</span>顶层条目（无父级）
          </span>
        </li>
        <KbTreeNode
          v-for="n in tree"
          :key="n.id"
          :node="n"
          :active="parentId"
          @pick="(id) => $emit('pick-parent', String(id))"
        />
      </ul>
      <div v-if="!tree.length" class="kb-nav-empty">还没有带层级的条目<br />在编辑页设置「所属父级」即可成树</div>
    </div>

    <!-- 分类 -->
    <div v-if="categories.length" class="kb-nav-group">
      <div class="kb-nav-h"><span>🏷 分类</span></div>
      <div class="kb-chip-wrap">
        <button
          v-for="c in categories"
          :key="c"
          class="kb-chip"
          :class="{ on: category === c }"
          @click="$emit('pick-category', category === c ? '' : c)"
        >{{ c }}</button>
      </div>
    </div>

    <!-- 标签 -->
    <div v-if="tags.length" class="kb-nav-group">
      <div class="kb-nav-h"><span># 标签</span></div>
      <div class="kb-chip-wrap">
        <button
          v-for="t in tags"
          :key="t"
          class="kb-chip"
          :class="{ on: tag === t }"
          @click="$emit('pick-tag', tag === t ? '' : t)"
        >#{{ t }}</button>
      </div>
    </div>
  </aside>
</template>

<script setup>
/**
 * 左栏导航。KbTreeNode 为本地递归组件（避免跨文件循环引用）。
 */
import { defineComponent, h } from 'vue'

const KbTreeNode = defineComponent({
  name: 'KbTreeNode',
  props: {
    node: { type: Object, required: true },
    active: { type: [String, Number], default: '' },
  },
  emits: ['pick'],
  setup(props, { emit }) {
    const renderChildren = () => {
      if (!props.node.children || !props.node.children.length) return null
      return h('ul', { class: 'kb-tree-sub' }, props.node.children.map((c) =>
        h(KbTreeNode, {
          key: c.id,
          node: c,
          active: props.active,
          onPick: (id) => emit('pick', id),
        })
      ))
    }
    return () => h('li', { class: 'kb-tree-li' }, [
      h('span', {
        class: ['kb-tree-node', { on: String(props.active) === String(props.node.id) }],
        onClick: () => emit('pick', props.node.id),
        title: props.node.title,
      }, [
        h('span', { class: 'tn-ico' }, '·'),
        h('span', { class: 'tn-txt' }, props.node.title),
        props.node.children && props.node.children.length
          ? h('span', { class: 'tn-badge' }, String(props.node.children.length))
          : null,
      ]),
      renderChildren(),
    ])
  },
})

defineProps({
  view: { type: String, default: 'list' },        // list | stats | trash
  total: { type: Number, default: 0 },
  trashCount: { type: Number, default: 0 },
  tree: { type: Array, default: () => [] },
  parentId: { type: [String, Number], default: '' },
  categories: { type: Array, default: () => [] },
  category: { type: String, default: '' },
  tags: { type: Array, default: () => [] },
  tag: { type: String, default: '' },
  mine: { type: Boolean, default: false },
  starred: { type: Boolean, default: false },
})

defineEmits([
  'view', 'pick-parent', 'pick-category', 'pick-tag',
  'toggle-mine', 'toggle-starred',
])
</script>

<style scoped>
.kb-nav {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-width: 0;
  height: 100%;
  overflow-y: auto;
  overflow-x: hidden;
  padding: 12px 10px 16px;
  scrollbar-width: thin;
}
.kb-nav::-webkit-scrollbar { width: 6px; }
.kb-nav::-webkit-scrollbar-thumb { background: var(--glass-border); border-radius: 3px; }

.kb-nav-group { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.kb-nav-group.grow { flex: 1 1 auto; min-height: 0; }
.kb-nav-h {
  display: flex; align-items: center; justify-content: space-between;
  font-size: 11.5px; color: var(--text-faint); font-weight: 600;
  padding: 4px 8px 2px; letter-spacing: .3px;
}
.kb-nav-clear {
  border: none; background: transparent; color: var(--accent);
  font-size: 11px; cursor: pointer; padding: 0 2px;
}
.kb-nav-clear:hover { text-decoration: underline; }

.kb-nav-item {
  display: flex; align-items: center; gap: 8px; width: 100%;
  border: none; background: transparent; color: var(--text-dim);
  font-size: 13px; text-align: left; cursor: pointer;
  padding: 7px 10px; border-radius: 9px; min-width: 0;
  transition: background .12s ease, color .12s ease;
}
.kb-nav-item.sm { font-size: 12.5px; padding: 6px 10px; }
.kb-nav-item:hover { background: var(--overlay-2); color: var(--text); }
.kb-nav-item.on { background: var(--accent-soft); color: var(--accent); font-weight: 600; }
.ni-ico { flex: 0 0 auto; font-size: 13px; line-height: 1; }
.ni-txt { flex: 1 1 auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ni-badge {
  flex: 0 0 auto; font-size: 10.5px; background: var(--overlay-2);
  color: var(--text-faint); border-radius: 999px; padding: 0 6px; line-height: 16px;
}
.ni-badge.warn { background: rgba(220, 38, 38, .12); color: #dc2626; }

/* 目录树 */
.kb-tree { list-style: none; margin: 0; padding: 0; }
.kb-tree-sub { list-style: none; margin: 0; padding-left: 12px; border-left: 1px dashed var(--glass-border); }
.kb-tree-node {
  display: flex; align-items: center; gap: 5px;
  font-size: 12.5px; color: var(--text-dim); cursor: pointer;
  padding: 4px 7px; border-radius: 7px; min-width: 0;
  transition: background .12s ease, color .12s ease;
}
.kb-tree-node:hover { background: var(--overlay-2); color: var(--text); }
.kb-tree-node.on { background: var(--accent-soft); color: var(--accent); font-weight: 600; }
.tn-ico { flex: 0 0 auto; font-size: 11px; }
.tn-txt { flex: 1 1 auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tn-badge {
  flex: 0 0 auto; font-size: 10px; background: var(--overlay-2);
  border-radius: 999px; padding: 0 5px; color: var(--text-faint);
}
.kb-nav-empty {
  font-size: 11.5px; color: var(--text-faint); line-height: 1.7;
  padding: 6px 8px; text-align: center;
}

/* 分类 / 标签 */
.kb-chip-wrap { display: flex; flex-wrap: wrap; gap: 5px; padding: 2px 6px; }
.kb-chip {
  border: 1px solid var(--glass-border); background: transparent;
  color: var(--text-dim); font-size: 11.5px; cursor: pointer;
  border-radius: 999px; padding: 2px 9px; max-width: 100%;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  transition: all .12s ease;
}
.kb-chip:hover { border-color: var(--accent); color: var(--accent); }
.kb-chip.on { background: var(--accent-soft); border-color: var(--accent); color: var(--accent); font-weight: 600; }
</style>
