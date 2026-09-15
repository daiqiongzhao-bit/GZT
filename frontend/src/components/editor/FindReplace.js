/**
 * FindReplace.js —— 查找替换扩展（v0.28.0，Word 常用）
 *
 * 【实现原理】
 * 用 ProseMirror Decoration 做**纯视觉高亮**，不修改文档内容，
 * 因此完全不落地到 HTML，不影响存储格式，也不污染撤销栈。
 *
 * 只有「替换」动作才真正修改文档，且：
 *   · 单次替换 → 一个 transaction
 *   · 全部替换 → **一个** transaction（用户按一次 Ctrl+Z 即可整体撤销）
 *     （旧式实现常逐条 dispatch，导致撤销要按几十次，体验极差）
 *
 * 【匹配范围】
 * 用 doc.textBetween(start, end) 收集纯文本，但**必须跳过节点边界**：
 * 直接拼接 textContent 会得到「跨段落的假匹配」。
 * 因此用 doc.descendants 遍历文本节点，按块级容器分段匹配，避免跨段落误匹配。
 */
import { Extension } from '@tiptap/core'
import { Plugin, PluginKey } from '@tiptap/pm/state'
import { Decoration, DecorationSet } from '@tiptap/pm/view'

export const findReplaceKey = new PluginKey('kbFindReplace')

/** 默认状态 */
const defaultState = {
  term: '',
  replace: '',
  caseSensitive: false,
  wholeWord: false,
  matches: [], // [{ from, to }]
  current: 0, // 当前定位的匹配序号（0-based）
}

/** 转义正则元字符，让用户输入的 `.` / `*` 等按字面匹配 */
function escapeRegExp(s) {
  return String(s).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/**
 * 在文档中查找所有匹配。
 * 逐文本节点匹配 → 天然不跨节点，避免「跨段落假匹配」。
 */
function findMatches(doc, state) {
  const { term, caseSensitive, wholeWord } = state
  if (!term) return []
  let pattern = escapeRegExp(term)
  if (wholeWord) pattern = `\\b${pattern}\\b`
  const re = new RegExp(pattern, caseSensitive ? 'g' : 'gi')

  const out = []
  doc.descendants((node, pos) => {
    if (!node.isText || !node.text) return
    let m
    re.lastIndex = 0
    while ((m = re.exec(node.text)) !== null) {
      out.push({ from: pos + m.index, to: pos + m.index + m[0].length })
      // 防零宽匹配死循环
      if (m[0].length === 0) re.lastIndex++
    }
  })
  return out
}

/** 重新计算匹配集合并生成 decorations */
function recompute(state) {
  const matches = findMatches(state.doc, state)
  const next = { ...state, matches }
  if (next.current >= matches.length) next.current = matches.length ? 0 : 0
  return next
}

export const FindReplace = Extension.create({
  name: 'findReplace',

  addOptions() {
    return {
      /** 状态变化回调，组件据此同步 UI（匹配数、当前序号） */
      onChange: null,
    }
  },

  addStorage() {
    return {
      // 用 storage 暴露给组件读取
      get state() {
        return this._state || defaultState
      },
      _state: defaultState,
    }
  },

  addCommands() {
    /**
     * setState 柯里化：返回一个 Tiptap 命令函数。
     * ⚠️ 命令必须是 `() => (props) => boolean` 的形状（惰性求值），
     * 不能写成 `() => impl()`（会立即执行，破坏 chain() 语义）。
     */
    const setState = (patch) => () => {
      const prev = this.storage._state
      const merged = { ...prev, ...patch }
      // 关键字/选项变化 → 重算匹配；仅切换 current 则不重算
      const needRecompute =
        'term' in patch || 'caseSensitive' in patch || 'wholeWord' in patch
      const next = needRecompute ? recompute(merged) : merged
      this.storage._state = next
      if (this.options.onChange) this.options.onChange(next)
      return true
    }

    /** 定位到下一个/上一个匹配，并滚动可见 */
    const goTo = (dir) => () => {
      const st = this.storage._state
      if (!st.matches.length) return false
      const len = st.matches.length
      const current = (st.current + dir + len) % len
      const merged = { ...st, current }
      this.storage._state = merged
      if (this.options.onChange) this.options.onChange(merged)

      const { from, to } = merged.matches[current]
      this.editor.chain().focus().setTextSelection({ from, to }).scrollIntoView().run()
      return true
    }

    /** 替换当前匹配并跳到下一个 */
    const replaceCurrent = () => () => {
      const st = this.storage._state
      if (!st.matches.length) return false
      const { from, to } = st.matches[st.current]
      const { tr } = this.editor.state
      tr.insertText(st.replace, from, to)
      this.editor.view.dispatch(tr)

      // 替换后文本长度变化，必须重算匹配
      const refreshed = recompute({ ...st, term: st.term })
      this.storage._state = refreshed
      if (this.options.onChange) this.options.onChange(refreshed)
      return true
    }

    /** 全部替换：**一次性**构建事务，保证 Ctrl+Z 可整体撤销 */
    const replaceAll = () => () => {
      const st = this.storage._state
      if (!st.matches.length) return false
      const { tr } = this.editor.state
      // 从后往前替换，避免前面的替换使后面的偏移失效
      const sorted = [...st.matches].sort((a, b) => b.from - a.from)
      for (const { from, to } of sorted) {
        tr.insertText(st.replace, from, to)
      }
      this.editor.view.dispatch(tr)

      const refreshed = recompute({ ...st })
      this.storage._state = refreshed
      if (this.options.onChange) this.options.onChange(refreshed)
      return true
    }

    /** 清空查找态（关闭面板时调用） */
    const clearSearch = () => () => {
      const next = { ...defaultState }
      this.storage._state = next
      if (this.options.onChange) this.options.onChange(next)
      return true
    }

    return {
      setSearchTerm: (term) => setState({ term, current: 0 })(),
      setReplaceTerm: (replace) => setState({ replace })(),
      setSearchCaseSensitive: (caseSensitive) => setState({ caseSensitive, current: 0 })(),
      setSearchWholeWord: (wholeWord) => setState({ wholeWord, current: 0 })(),
      findNext: () => goTo(+1)(),
      findPrev: () => goTo(-1)(),
      replaceCurrent: () => replaceCurrent()(),
      replaceAll: () => replaceAll()(),
      clearSearch: () => clearSearch()(),
    }
  },

  addProseMirrorPlugins() {
    const ext = this
    return [
      new Plugin({
        key: findReplaceKey,
        state: {
          init: () => DecorationSet.empty,
          apply(tr, old, _oldState, newState) {
            const st = ext.storage._state
            if (!st.term || !st.matches.length) return DecorationSet.empty

            const decos = []
            st.matches.forEach((m, i) => {
              // 越界保护：文档变更后旧偏移可能失效
              if (m.from >= newState.doc.content.size || m.to > newState.doc.content.size) return
              const isCurrent = i === st.current
              decos.push(
                Decoration.inline(m.from, m.to, {
                  class: isCurrent ? 'kb-search-hit kb-search-hit-active' : 'kb-search-hit',
                }),
              )
            })
            return DecorationSet.create(newState.doc, decos)
          },
        },
        props: {
          decorations(state) {
            return findReplaceKey.getState(state)
          },
        },
      }),
    ]
  },

  addKeyboardShortcuts() {
    return {
      Escape: () => {
        // 仅在查找面板打开（有 term）时消费 Esc
        if (!this.storage._state.term) return false
        this.editor.commands.clearSearch()
        return true
      },
    }
  },
})
