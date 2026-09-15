/**
 * ImageAttrs.js —— 自定义 Image 扩展（v0.28.0）
 *
 * 【为什么必须自定义】
 * Tiptap 默认的 Image 扩展只保留 src/alt/title，会**丢弃** data-* 属性。
 * 而本项目有三个属性是业务机制的红线：
 *
 *   data-temp-id  ← backend/internal/handlers/workspace.go:1391 adoptTempAttachments()
 *                    用正则 data-temp-id="(\d+)" 扫描正文，找到新建条目时暂存的图片并「转正」
 *                    （移动文件 → 建附件记录 → 改写 URL → 抹掉该属性）。
 *                    一旦此属性在 getHTML() 时丢失：正则扫不到 → 文件留在临时目录 →
 *                    附件表无记录 → 图片后续被清理 → **用户保存后图片全成死链**。
 *   data-att-id   ← 正式附件 ID，删除附件时按此反查正文引用
 *   data-att-name ← 附件原始文件名，用于 alt 与下载名
 *
 * 另外新增两个「Word 常用」能力：
 *   width / height  —— 拖拽缩放（写 width 属性落库）
 *   data-align      —— 左/中/右对齐（用属性而非 style="float"，避免布局类样式被白名单拦截）
 */
import Image from '@tiptap/extension-image'

/** 缩放手柄生效的最小宽度（px），防止拖成看不见的细条 */
const MIN_WIDTH = 48
/** 单次缩放的步进，避免像素级抖动 */
const RESIZE_STEP = 8

export const ImageAttrs = Image.extend({
  addAttributes() {
    return {
      // 继承父类：src / alt / title
      ...this.parent?.(),

      // ——— 附件机制红线属性：必须透传 ———
      'data-att-id': {
        default: null,
        parseHTML: (el) => el.getAttribute('data-att-id'),
        renderHTML: (attrs) => (attrs['data-att-id'] ? { 'data-att-id': attrs['data-att-id'] } : {}),
      },
      'data-temp-id': {
        default: null,
        parseHTML: (el) => el.getAttribute('data-temp-id'),
        renderHTML: (attrs) => (attrs['data-temp-id'] ? { 'data-temp-id': attrs['data-temp-id'] } : {}),
      },
      'data-att-name': {
        default: null,
        parseHTML: (el) => el.getAttribute('data-att-name'),
        renderHTML: (attrs) => (attrs['data-att-name'] ? { 'data-att-name': attrs['data-att-name'] } : {}),
      },

      // ——— 图片缩放（Word 常用）———
      width: {
        default: null,
        parseHTML: (el) => {
          const w = el.getAttribute('width')
          if (w) return parseInt(w, 10)
          // 兼容历史 style="width:320px" 形态
          const sw = el.style && el.style.width
          return sw ? parseInt(sw, 10) || null : null
        },
        renderHTML: (attrs) => (attrs.width ? { width: attrs.width } : {}),
      },
      height: {
        default: null,
        parseHTML: (el) => {
          const h = el.getAttribute('height')
          if (h) return parseInt(h, 10)
          const sh = el.style && el.style.height
          return sh ? parseInt(sh, 10) || null : null
        },
        renderHTML: (attrs) => (attrs.height ? { height: attrs.height } : {}),
      },

      // ——— 图片对齐（Word 常用）———
      // 用 data-align 而非 style="float:left"：float 属于布局类样式，
      // 安全白名单明确禁止（可被用于布局劫持），且会脱离 ProseMirror 节点模型。
      'data-align': {
        default: null,
        parseHTML: (el) => el.getAttribute('data-align'),
        renderHTML: (attrs) => (attrs['data-align'] ? { 'data-align': attrs['data-align'] } : {}),
      },
    }
  },

  addNodeView() {
    return ({ node, getPos, editor }) => {
      // 外层 wrap 承载对齐，内层 img 承载尺寸
      const wrap = document.createElement('div')
      wrap.className = 'kb-img-wrap'

      const img = document.createElement('img')
      img.src = node.attrs.src || ''
      img.className = 'kb-img'
      if (node.attrs.alt) img.alt = node.attrs.alt
      if (node.attrs.width) img.setAttribute('width', String(node.attrs.width))
      if (node.attrs.height) img.setAttribute('height', String(node.attrs.height))
      wrap.appendChild(img)

      // 对齐通过 wrap 的 data-align 表达
      const applyAlign = (align) => {
        if (align) wrap.dataset.align = align
        else delete wrap.dataset.align
      }
      applyAlign(node.attrs['data-align'])

      // 只读态不挂交互
      const readonly = editor.isEditable === false

      // ——— 缩放拖拽手柄 ———
      if (!readonly) {
        const handle = document.createElement('span')
        handle.className = 'kb-img-resize'
        handle.setAttribute('title', '拖拽调整图片宽度')
        wrap.appendChild(handle)

        handle.addEventListener('mousedown', (e) => {
          e.preventDefault()
          e.stopPropagation()
          const startX = e.clientX
          const startW = img.getBoundingClientRect().width
          let latest = Math.round(startW)

          const onMove = (ev) => {
            const delta = ev.clientX - startX
            const raw = startW + delta
            // 按步进取整，避免拖拽时数值抖动
            const w = Math.max(MIN_WIDTH, Math.round(raw / RESIZE_STEP) * RESIZE_STEP)
            latest = w
            img.setAttribute('width', String(w))
          }
          const onUp = () => {
            document.removeEventListener('mousemove', onMove)
            document.removeEventListener('mouseup', onUp)
            const pos = getPos()
            if (typeof pos !== 'number') return
            // 等比：高度按原始比例缩放（拿不到原始尺寸时交给 CSS 自适应）
            const tr = editor.view.state.tr.setNodeMarkup(pos, null, {
              ...node.attrs,
              width: latest,
              height: null,
            })
            editor.view.dispatch(tr)
          }
          document.addEventListener('mousemove', onMove)
          document.addEventListener('mouseup', onUp)
        })
      }

      // ——— 点击图片 → 对齐/尺寸浮层 ———
      if (!readonly) {
        img.addEventListener('click', (e) => {
          e.preventDefault()
          e.stopPropagation()
          // 用自定义事件交给 RichTextEditor 统一渲染浮层（避免 NodeView 内直接操作 naive-ui）
          const pos = getPos()
          if (typeof pos !== 'number') return
          editor.view.dom.dispatchEvent(
            new CustomEvent('kb-image-select', {
              bubbles: true,
              detail: { pos, attrs: { ...node.attrs } },
            }),
          )
        })
      }

      return {
        dom: wrap,
        // 外部属性变化（如浮层点击对齐按钮）时同步到 DOM
        update: (updated) => {
          if (updated.type.name !== node.type.name) return false
          img.src = updated.attrs.src || ''
          if (updated.attrs.width) img.setAttribute('width', String(updated.attrs.width))
          else img.removeAttribute('width')
          applyAlign(updated.attrs['data-align'])
          return true
        },
        // 让 NodeView 不吞掉编辑器选区行为
        selectable: false,
        draggable: true,
      }
    }
  },
}).configure({
  inline: false, // 图片独占一行（与 Word 的嵌入式图片一致）
  allowBase64: false, // 禁止 base64：图片一律走附件上传，避免正文膨胀
  HTMLAttributes: { class: 'kb-img' },
})
