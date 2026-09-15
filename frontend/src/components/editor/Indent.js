/**
 * Indent.js —— 段落缩进扩展（v0.28.0）
 *
 * 【设计决策：为什么用 data-indent 而不是 style="margin-left"】
 * 两种方案都可行，但：
 *   · 写 inline style → 依赖后端白名单放行 margin-left（已放行），但每级缩进会写入具体 px，
 *     切换主题/窄屏时无法自适应，且与「段落属性」的语义不匹配（缩进是结构而非样式）。
 *   · 写 data-indent → 展示端用 CSS 按级别映射 em（1 级=2em），
 *     在窄屏/大字号下自动适配，且属性白名单已放行 data-indent。
 * 故选后者。
 *
 * 【Tab 键冲突处理】
 * Tab 在列表内应交回给 ListKeymap 处理「降级列表项」，不能拦截，
 * 否则用户在列表里按 Tab 无法创建嵌套列表 —— 这是比缩进更高频的操作。
 */
import { Extension } from '@tiptap/core'

/** 最大缩进级别，防止无限缩进把内容挤出可视区 */
const MAX_INDENT = 8

export const Indent = Extension.create({
  name: 'indent',

  addGlobalAttributes() {
    return [
      {
        types: ['paragraph', 'heading'],
        attributes: {
          indent: {
            default: 0,
            parseHTML: (el) => parseInt(el.getAttribute('data-indent') || '0', 10) || 0,
            renderHTML: (attrs) => (attrs.indent ? { 'data-indent': attrs.indent } : {}),
          },
        },
      },
    ]
  },

  addCommands() {
    const applyIndent = (delta) => ({ tr, state, dispatch }) => {
      const { from, to } = state.selection
      let changed = false
      state.doc.nodesBetween(from, to, (node, pos) => {
        if (!['paragraph', 'heading'].includes(node.type.name)) return
        const cur = node.attrs.indent || 0
        const next = Math.min(MAX_INDENT, Math.max(0, cur + delta))
        if (next === cur) return
        changed = true
        if (dispatch) {
          tr.setNodeMarkup(pos, null, { ...node.attrs, indent: next })
        }
      })
      return changed
    }

    return {
      /** 增加缩进 */
      indent: () => applyIndent(+1),
      /** 减少缩进 */
      outdent: () => applyIndent(-1),
    }
  },

  addKeyboardShortcuts() {
    const inList = () =>
      this.editor.isActive('listItem') || this.editor.isActive('taskItem')
    return {
      // 列表内不拦截，交回 ListKeymap（Tab = 嵌套列表项）
      Tab: () => (inList() ? false : this.editor.commands.indent()),
      'Shift-Tab': () => (inList() ? false : this.editor.commands.outdent()),
    }
  },
})
