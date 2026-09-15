/**
 * legacyHtml.js —— 存量 HTML 解析期归一化（v0.28.0）
 *
 * 【背景】
 * 本次用 Tiptap(ProseMirror) 替换自研 contenteditable 编辑器，但**存储仍是 HTML 字符串、不做批量迁移**。
 * 存量数据由旧的 document.execCommand 产出，含 Tiptap 默认无法理解的结构：
 *   · <font size="3" color="red" face="宋体">  —— execCommand('fontSize') / Word 粘贴的产物
 *   · <p align="center">                        —— execCommand('justifyCenter') 的产物
 *   · <h5> / <h6>                               —— 本次只支持 H1–H4
 * 若不在解析前转换，这些属性会被 ProseMirror schema 静默丢弃 —— 用户打开老条目一保存，排版就没了。
 *
 * 【零迁移原理】
 * 转换只发生在**解析期**（喂给 useEditor / setContent 之前）；
 * getHTML() 产出的是新形态（<span style> / style="text-align"），
 * 因此用户**首次编辑保存后，该条目的 HTML 自动完成归一化**，无需跑全库脚本。
 */

// CSS 字号 px 映射：`<font size>` 的 1~7 级 → px。
// 采用浏览器默认的 16px 基准，与 Word 常见字号（小四≈12pt≈16px）对齐。
const FONT_SIZE_MAP = ['10px', '13px', '16px', '18px', '24px', '32px', '48px']

// 允许携带 align 属性的块级标签（旧编辑器只对这些标签调用 justify* 命令）
const ALIGN_TAGS = 'p|h[1-6]|div|td|th|li|blockquote'

/**
 * 把存量 HTML 归一化为 Tiptap 可理解的新形态。
 * 纯字符串处理，无副作用，可在 useEditor 与 setContent 两处安全复用。
 *
 * @param {string} html
 * @returns {string}
 */
export function upgradeLegacyHtml(html) {
  if (!html) return ''
  let s = String(html)

  // ① <font size color face> → <span style="...">
  //    这是存量数据里最常见、也是丢失代价最高的一种（字号/颜色一整片消失）。
  s = s.replace(/<font\b([^>]*)>/gi, (_m, attrs) => {
    const size = /\bsize\s*=\s*["']?(\d)["']?/i.exec(attrs)
    const color = /\bcolor\s*=\s*["']([^"']+)["']/i.exec(attrs)
    const face = /\bface\s*=\s*["']([^"']+)["']/i.exec(attrs)

    const styles = []
    if (size) {
      const px = FONT_SIZE_MAP[parseInt(size[1], 10) - 1]
      if (px) styles.push(`font-size:${px}`)
    }
    if (color) styles.push(`color:${color[1]}`)
    if (face) styles.push(`font-family:${face[1]}`)

    return styles.length ? `<span style="${styles.join(';')}">` : '<span>'
  })
  s = s.replace(/<\/font>/gi, '</span>')

  // ② align="center" → style="text-align:center"
  //    TextAlign 扩展只读 style，不认 align 属性；不转换则居中/右对齐会丢失。
  s = s.replace(
    new RegExp(`<(${ALIGN_TAGS})\\b([^>]*?)\\balign\\s*=\\s*["']?(left|center|right|justify)["']?`, 'gi'),
    (_m, tag, pre, val) => {
      // 若标签已有 style，则合并而非覆盖
      if (/style\s*=\s*"/i.test(pre)) {
        return `<${tag}${pre.replace(/style\s*=\s*"/i, `style="text-align:${val};`)}`.replace(/\s+$/, '')
      }
      return `<${tag}${pre} style="text-align:${val}"`
    },
  )

  // ③ h5 / h6 → h4（本次只支持 H1–H4，降级而非丢弃，保证内容不消失）
  s = s.replace(/<h5\b([^>]*)>/gi, '<h4$1>').replace(/<\/h5>/gi, '</h4>')
  s = s.replace(/<h6\b([^>]*)>/gi, '<h4$1>').replace(/<\/h6>/gi, '</h4>')

  // ④ <strike> → <s>（Tiptap Strike 扩展默认解析 s/del，strike 是旧标签）
  s = s.replace(/<strike\b([^>]*)>/gi, '<s$1>').replace(/<\/strike>/gi, '</s>')

  return s
}

/**
 * 判断一段 HTML 是否含存量结构（用于兼容性统计与日志）。
 * @param {string} html
 * @returns {{hasFont:boolean, hasAlign:boolean, hasH5H6:boolean}}
 */
export function detectLegacy(html) {
  const s = String(html || '')
  return {
    hasFont: /<font\b/i.test(s),
    hasAlign: /\balign\s*=/i.test(s),
    hasH5H6: /<h[56]\b/i.test(s),
  }
}
