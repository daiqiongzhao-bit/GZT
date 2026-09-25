/**
 * PasteImageUpload.js —— 粘贴 / 拖拽图片上传（v0.28.0）
 *
 * 【为什么用扩展而不是写在组件里】
 * 旧实现（RichTextEditor.vue:192-313）在 contenteditable 的 @paste 上做 DOM 操作，
 * 依赖 execCommand('insertHTML')。迁移到 ProseMirror 后，插入必须走 transaction，
 * 因此改用 Tiptap 的 handlePaste / handleDrop，用 editor.commands.insertContent 落内容。
 *
 * 【必须保留的能力】（用户最认可的功能：整篇 Word 复制过来）
 *   1. 剪贴板 image blob（截图 / 复制的图片）→ 上传 → 插入
 *   2. Word 的 file:/// 占位图 → 用剪贴板 blob 按序替换（Word 复制时每张图都在剪贴板里）
 *   3. base64 data:image → 上传后替换 src（避免正文膨胀）
 *   4. HTML 排版（标题/列表/粗体/对齐）保留 —— 由 Tiptap 原生解析
 *   5. 拖拽图片文件到编辑器 → 上传插入（本次新增）
 *
 * 【上传端点选择】（与原实现完全一致，不可改）
 *   entryId > 0 → POST /workspace/knowledge/:id/attachments（正式附件，用 stored_name 拼 URL 防枚举）
 *   entryId = 0 → POST /workspace/temp-attachments（中转缓存，保存时由 adoptTempAttachments 转正）
 */
import { Extension } from '@tiptap/core'
import { Plugin } from '@tiptap/pm/state'

/**
 * 粘贴图片的默认最大宽度（px）。
 * 截图常是原尺寸 1000~2000px，若不加约束、直接以自然宽度整行铺开，
 * 详情页就会「那么大」占满一屏。超出此宽度则在插入时即按该宽度落库，
 * 用户仍可在编辑态用右下角手柄拖拽到任意尺寸。
 */
const MAX_PASTE_WIDTH = 760

/** data:URL → File（用于把粘贴的 base64 图片转成可上传的文件） */
function dataURLToFile(dataURL, filename) {
  const m = /^data:([^;]*);base64,(.*)$/.exec(dataURL)
  const mime = m ? m[1] : 'image/png'
  const bin = atob(m ? m[2] : '')
  const arr = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i)
  return new File([arr], filename, { type: mime })
}

/** 构造图片节点属性（含附件机制红线属性）。
 *  width 可选：粘贴图片超过 MAX_PASTE_WIDTH 时，预先落一个合理宽度，避免「那么大」。 */
function imgAttrs(u, width) {
  const attrs = { src: u.dl, alt: u.name || 'image' }
  if (width) attrs.width = width
  if (u.temp) attrs['data-temp-id'] = String(u.id)
  else attrs['data-att-id'] = String(u.id)
  attrs['data-att-name'] = u.name || ''
  return attrs
}

/** 读取图片文件的自然尺寸（用于给粘贴图片设一个合理的默认宽度） */
function naturalSize(file) {
  return new Promise((resolve) => {
    const url = URL.createObjectURL(file)
    const im = new Image()
    im.onload = () => {
      const w = im.naturalWidth || 0
      const h = im.naturalHeight || 0
      URL.revokeObjectURL(url)
      resolve({ w, h })
    }
    im.onerror = () => { URL.revokeObjectURL(url); resolve({ w: 0, h: 0 }) }
    im.src = url
  })
}

export const PasteImageUpload = Extension.create({
  name: 'pasteImageUpload',

  addOptions() {
    return {
      /** 上传函数：由组件注入，返回 { dl, id, temp, name } */
      upload: null,
      /** 错误回调：由组件注入（用于 emit('imageUploadError') 与提示） */
      onError: null,
      /** 是否允许插入图片（disabled 态为 false） */
      enabled: () => true,
    }
  },

  addProseMirrorPlugins() {
    const { upload, onError, enabled } = this.options
    const editor = this.editor

    /** 上传并把图片插入到当前选区 */
    const uploadAndInsert = async (file) => {
      try {
        const u = await upload(file)
        if (!u || !u.dl) return
        // 超宽截图按 MAX_PASTE_WIDTH 预缩，避免复制粘贴进来就「那么大」
        const { w } = await naturalSize(file)
        const width = w && w > MAX_PASTE_WIDTH ? MAX_PASTE_WIDTH : null
        editor.chain().focus().insertContent({ type: 'image', attrs: imgAttrs(u, width) }).run()
      } catch (err) {
        if (onError) onError(err)
      }
    }

    /**
     * 处理富文本 HTML：把内嵌图片（base64 / file:///）上传后就地替换为可访问 URL。
     * blobs 为剪贴板里的图片文件，用于替换 Word 产生的 file:/// 占位图（按序消费）。
     */
    const embedImagesFromHtml = async (html, blobs) => {
      const tpl = document.createElement('template')
      tpl.innerHTML = html
      const imgs = Array.from(tpl.content.querySelectorAll('img'))
      let bi = 0
      for (const img of imgs) {
        const src = img.getAttribute('src') || ''
        if (/^https?:\/\//i.test(src)) continue // 外链图片保留原样

        if (/^data:image\//i.test(src)) {
          try {
            const u = await upload(dataURLToFile(src, 'paste-image.png'))
            applyImgAttrs(img, u)
          } catch {
            /* 单张失败不影响其余 */
          }
          continue
        }
        if (/^file:\/\//i.test(src)) {
          // Word 复制的图片常以 file:/// 引用，浏览器无法读取；用剪贴板 blob 按序替换
          if (bi < blobs.length) {
            try {
              const u = await upload(blobs[bi])
              applyImgAttrs(img, u)
            } catch {
              img.remove()
            }
            bi++
          } else {
            img.remove() // 无数据源，移除避免破图
          }
          continue
        }
      }
      return { html: tpl.innerHTML, leftover: blobs.slice(bi) }
    }

    /** 给 <img> 设置上传后的地址与附件标记 */
    const applyImgAttrs = (img, u) => {
      img.setAttribute('src', u.dl)
      img.removeAttribute('srcset')
      if (u.temp) {
        img.setAttribute('data-temp-id', String(u.id))
        img.removeAttribute('data-att-id')
      } else {
        img.setAttribute('data-att-id', String(u.id))
        img.removeAttribute('data-temp-id')
      }
      img.setAttribute('data-att-name', u.name || '')
      img.setAttribute('alt', u.name || 'image')
    }

    return [
      // ⚠️ 必须用 new Plugin(...) 包一层，不能直接返回裸对象。
      //    ProseMirror 内部会读 plugin.spec.state；裸对象没有 .spec，
      //    会在 EditorState 构造阶段抛
      //    「Cannot read properties of undefined (reading 'state')」，
      //    导致整个编辑器（工具栏 + 编辑区）空白 —— v0.28.0 实测踩坑。
      new Plugin({
        key: 'kbPasteImage',
        props: {
          handlePaste: (view, event) => {
            if (!enabled()) return false
            const cd = event.clipboardData
            if (!cd) return false

            const blobs = Array.from(cd.items || [])
              .filter((it) => it.kind === 'file' && it.type && it.type.startsWith('image/'))
              .map((it) => it.getAsFile())
              .filter(Boolean)
            const html = cd.getData ? cd.getData('text/html') : ''

            // ① 富文本（来自 Word / 网页 / 手册）
            if (html) {
              event.preventDefault()
              ;(async () => {
                const { html: handled, leftover } = await embedImagesFromHtml(html, blobs)
                editor.chain().focus().insertContent(handled).run()
                // HTML 里没对应 <img> 的剩余 blob（如单独复制的一张图）补插到光标处
                for (const b of leftover) await uploadAndInsert(b)
              })()
              return true
            }

            // ② 纯图片（无 HTML）：逐张上传插入
            if (blobs.length) {
              event.preventDefault()
              ;(async () => {
                for (const b of blobs) await uploadAndInsert(b)
              })()
              return true
            }

            // ③ 纯文本：交回默认行为
            return false
          },

          handleDrop: (view, event) => {
            if (!enabled()) return false
            const dt = event.dataTransfer
            if (!dt) return false
            const files = Array.from(dt.files || []).filter((f) => f.type && f.type.startsWith('image/'))
            if (!files.length) return false

            event.preventDefault()
            ;(async () => {
              for (const f of files) await uploadAndInsert(f)
            })()
            return true
          },
        },
      }),
    ]
  },
})
