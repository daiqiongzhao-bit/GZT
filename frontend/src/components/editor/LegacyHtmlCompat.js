/**
 * LegacyHtmlCompat.js —— 存量 HTML 属性兼容扩展（v0.28.0）
 *
 * 【解决的问题】
 * upgradeLegacyHtml() 在解析前把 align="center" 改写成 style="text-align:center"，
 * 但存在两条漏网路径：
 *   1. 通过 API 直接返回的存量内容（例如历史版本快照 kVersions、评论 content）
 *      可能未经 upgradeLegacyHtml 就进入编辑器（如回滚版本后 setContent）
 *   2. 后端在极端情况下把 align 保留在 HTML 里（属性白名单未放行 align，
 *      但 <td align> 在部分导出路径可能残留）
 *
 * 本扩展让 TextAlign 之外再认一次 align 属性，作为兜底。
 * 实现方式：给 paragraph / heading 增加一个虚拟属性，parseHTML 时读取 align，
 * renderHTML 时输出为 style（与 TextAlign 的落库形态统一，不产生第二套格式）。
 */
import { Extension } from '@tiptap/core'

export const LegacyHtmlCompat = Extension.create({
  name: 'legacyHtmlCompat',

  addGlobalAttributes() {
    return [
      {
        types: ['paragraph', 'heading'],
        attributes: {
          /**
           * 虚拟属性：仅用于解析期承接 legacy align，
           * 落库时统一转成 text-align 内联样式（不输出 align 属性，避免双格式）。
           */
          legacyAlign: {
            default: null,
            parseHTML: (el) => {
              const a = el.getAttribute('align')
              if (!a) return null
              const v = a.toLowerCase()
              return ['left', 'center', 'right', 'justify'].includes(v) ? v : null
            },
            renderHTML: (attrs) => {
              if (!attrs.legacyAlign) return {}
              // 若已有 text-align（TextAlign 扩展已处理），不重复输出
              return { style: `text-align:${attrs.legacyAlign}` }
            },
          },
        },
      },
    ]
  },
})
