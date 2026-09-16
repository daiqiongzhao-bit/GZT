/**
 * 知识库图形条目（思维导图 / 流程图）的工具集 —— 零外部依赖。
 *
 * 为什么不用 html2canvas / dom-to-image 做导出：
 *   本项目是玻璃拟态 UI（backdrop-filter + CSS 变量 + 半透明层），这类库需要把 DOM
 *   重绘进 canvas，在企业微信内置浏览器里兼容性很差、经常白屏或丢样式。
 *   而两个绘图库的画布本身就是 SVG，直接「序列化 SVG → Image → canvas」是纯标准路径，
 *   不依赖任何 DOM 快照技巧，稳定得多，也顺带避免了 oklch 颜色解析失败这类坑。
 *
 * 导出链路：
 *   PNG ← SVG 源码 → Image → canvas → toBlob('image/png')
 *   PDF ← 同上得到位图 → 重编码 JPEG → 手工封装单页 PDF（/DCTDecode，因此无需 jsPDF）
 *   SVG ← 绘图库自带的矢量导出
 */

// ---------- 下载 ----------

export function downloadBlob(blob, filename) {
  if (!blob) return
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  // 稍后回收：立即 revoke 在部分浏览器会打断下载
  setTimeout(() => URL.revokeObjectURL(url), 4000)
}

export function downloadText(text, filename, mime = 'text/plain;charset=utf-8') {
  downloadBlob(new Blob([text], { type: mime }), filename)
}

// 安全文件名：Windows 不允许 \ / : * ? " < > | 与换行，统一替换并限长
export function safeFileName(name, ext) {
  const base = String(name || '未命名')
    .replace(/[\\/:*?"<>|\r\n\t]/g, '_')
    .replace(/\s+/g, ' ')
    .trim()
    .slice(0, 60) || '未命名'
  return ext ? `${base}.${ext}` : base
}

// ---------- SVG ----------

function loadImage(src) {
  return new Promise((resolve, reject) => {
    const img = new Image()
    // 同源 data URL，无需 CORS；canvas 也不会被污染
    img.onload = () => resolve(img)
    img.onerror = () => reject(new Error('图形渲染失败，可能是内容过大或包含不可访问的外部图片'))
    img.src = src
  })
}

function measureSvg(src) {
  const pick = (re) => {
    const m = String(src).match(re)
    return m ? parseFloat(m[1]) : 0
  }
  let width = pick(/\bwidth\s*=\s*"([\d.]+)/i)
  let height = pick(/\bheight\s*=\s*"([\d.]+)/i)
  if (!width || !height) {
    const vb = String(src).match(/viewBox\s*=\s*"([^"]+)"/i)
    if (vb) {
      const p = vb[1].trim().split(/[\s,]+/).map(Number)
      if (p.length === 4 && p[2] > 0 && p[3] > 0) {
        width = width || p[2]
        height = height || p[3]
      }
    }
  }
  return { width: width || 1200, height: height || 800 }
}

/**
 * 规范化 SVG 源码：补 xmlns 与显式宽高。
 * Chromium 在 <img> 里渲染 SVG 时，若只有 viewBox 没有 width/height，尺寸会算成 0，必须补齐。
 */
function normalizeSvg(svgSource) {
  let src = String(svgSource || '').trim()
  if (!src) throw new Error('图形内容为空')
  if (!/^<svg/i.test(src)) {
    const i = src.indexOf('<svg')
    if (i < 0) throw new Error('未找到可导出的 SVG 内容')
    src = src.slice(i)
  }
  const { width, height } = measureSvg(src)
  const add = []
  if (!/\sxmlns:xlink\s*=/.test(src)) add.push('xmlns:xlink="http://www.w3.org/1999/xlink"')
  if (!/\sxmlns\s*=/.test(src)) add.push('xmlns="http://www.w3.org/2000/svg"')
  if (!/\swidth\s*=/.test(src)) add.push(`width="${Math.round(width)}"`)
  if (!/\sheight\s*=/.test(src)) add.push(`height="${Math.round(height)}"`)
  if (add.length) src = src.replace(/^<svg/i, `<svg ${add.join(' ')}`)
  return src
}

/**
 * SVG 源码 → PNG Blob
 * @param {string} svgSource SVG 源码
 * @param {{scale?:number, background?:string}} opts scale=2 即 2 倍图（默认），background='transparent' 保留透明底
 */
export async function svgToPngBlob(svgSource, { scale = 2, background = '#ffffff' } = {}) {
  const src = normalizeSvg(svgSource)
  const { width, height } = measureSvg(src)
  const s = Math.min(Math.max(Number(scale) || 1, 1), 4) // 限制 1~4 倍，避免超大画布把内存打满
  const canvas = document.createElement('canvas')
  canvas.width = Math.max(1, Math.round(width * s))
  canvas.height = Math.max(1, Math.round(height * s))
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('当前浏览器不支持画布导出')
  if (background && background !== 'transparent') {
    ctx.fillStyle = background
    ctx.fillRect(0, 0, canvas.width, canvas.height)
  }
  // 用 base64 data URL 而不是 URL 编码：大导图用 URL 编码会超出 data URL 长度上限
  const b64 = btoa(unescape(encodeURIComponent(src)))
  const img = await loadImage('data:image/svg+xml;base64,' + b64)
  ctx.drawImage(img, 0, 0, canvas.width, canvas.height)
  const blob = await new Promise((r) => canvas.toBlob(r, 'image/png'))
  if (!blob) throw new Error('导出 PNG 失败')
  return blob
}

// ---------- PDF ----------

function blobToDataUrl(blob) {
  return new Promise((resolve, reject) => {
    const fr = new FileReader()
    fr.onload = () => resolve(fr.result)
    fr.onerror = () => reject(new Error('读取图形数据失败'))
    fr.readAsDataURL(blob)
  })
}

function dataUrlToBytes(url) {
  const b64 = String(url).split(',')[1] || ''
  const bin = atob(b64)
  const u8 = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) u8[i] = bin.charCodeAt(i)
  return u8
}

/**
 * 位图 → 单页 PDF（零依赖）
 *
 * PDF 的 /DCTDecode 过滤器就是 JPEG，所以先把位图重编码为 JPEG（质量 0.95，视觉无损），
 * 再按 PDF 1.4 语法拼出「目录 / 页面 / 图像 XObject / 内容流」并追加交叉引用表。
 * 页面固定 A4，按图片横竖比自动选方向，等比缩放居中留白。
 */
export async function imageBlobToPdf(blob, { margin = 24, quality = 0.95 } = {}) {
  const bmp = await loadImage(await blobToDataUrl(blob))
  const iw = bmp.naturalWidth || bmp.width
  const ih = bmp.naturalHeight || bmp.height
  if (!iw || !ih) throw new Error('图形尺寸异常，无法导出 PDF')

  // 1) 重编码为 JPEG（同时把透明底压成白底，PDF 里不会出现黑块）
  const canvas = document.createElement('canvas')
  canvas.width = iw
  canvas.height = ih
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('当前浏览器不支持画布导出')
  ctx.fillStyle = '#ffffff'
  ctx.fillRect(0, 0, iw, ih)
  ctx.drawImage(bmp, 0, 0, iw, ih)
  const jpeg = dataUrlToBytes(canvas.toDataURL('image/jpeg', quality))

  // 2) 页面几何：A4 595.28 × 841.89 pt，按图片方向选横竖版
  const A4W = 595.28
  const A4H = 841.89
  const landscape = iw > ih
  const pageW = landscape ? A4H : A4W
  const pageH = landscape ? A4W : A4H
  const availW = pageW - margin * 2
  const availH = pageH - margin * 2
  const k = Math.min(availW / iw, availH / ih)
  const drawW = iw * k
  const drawH = ih * k
  const offX = (pageW - drawW) / 2
  const offY = (pageH - drawH) / 2
  const content = `q\n${drawW.toFixed(2)} 0 0 ${drawH.toFixed(2)} ${offX.toFixed(2)} ${offY.toFixed(2)} cm\n/Im0 Do\nQ\n`

  // 3) 拼装 PDF：边写边记每个对象的字节偏移，供 xref 使用
  const enc = new TextEncoder()
  const parts = []
  const offsets = []
  let total = 0
  const w = (x) => {
    const u8 = typeof x === 'string' ? enc.encode(x) : x
    parts.push(u8)
    total += u8.length
  }
  const obj = (s) => { offsets.push(total); w(s) }

  w('%PDF-1.4\n')
  w(new Uint8Array([0x25, 0xe2, 0xe3, 0xcf, 0xd3, 0x0a])) // 二进制标识，声明内容含非 ASCII

  obj('1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n')
  obj('2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n')
  obj(`3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${pageW.toFixed(2)} ${pageH.toFixed(2)}] ` +
    '/Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>\nendobj\n')
  obj(`4 0 obj\n<< /Type /XObject /Subtype /Image /Width ${iw} /Height ${ih} ` +
    `/ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length ${jpeg.length} >>\nstream\n`)
  w(jpeg)
  w('\nendstream\nendobj\n')
  obj(`5 0 obj\n<< /Length ${content.length} >>\nstream\n${content}endstream\nendobj\n`)

  const startxref = total
  let xref = 'xref\n0 6\n0000000000 65535 f \n'
  for (let i = 0; i < offsets.length; i++) {
    xref += String(offsets[i]).padStart(10, '0') + ' 00000 n \n'
  }
  w(xref + `trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n${startxref}\n%%EOF\n`)

  return new Blob(parts, { type: 'application/pdf' })
}

// ---------- 图形条目正文 → 大纲（与后端 knowledgePlainText 同口径，用于卡片预览兜底）----------

/**
 * 客户端轻量大纲提取（后端正常会在列表/详情里带回 outline 字段，这里仅作兜底）。
 * @param {string} raw 图形条目的 Content（JSON 字符串）
 * @param {string} kind 'mind' | 'flow'
 */
export function diagramOutline(raw, kind) {
  const s = String(raw || '').trim()
  if (!s) return ''
  let data = null
  try { data = JSON.parse(s) } catch (e) { return '' }
  if (!data) return ''
  if (kind === 'mind') {
    const lines = []
    const walk = (n, depth) => {
      if (!n || depth > 24) return
      const topic = String(n.topic || '').trim()
      if (topic) lines.push('  '.repeat(depth) + '- ' + topic)
      for (const c of (n.children || [])) walk(c, depth + 1)
    }
    walk(data.nodeData, 0)
    return lines.join('\n')
  }
  if (kind === 'flow') {
    const nodes = Array.isArray(data.nodes) ? data.nodes : []
    const edges = Array.isArray(data.edges) ? data.edges : []
    const label = (n) => String((n && n.properties && n.properties.text) || (n && n.text && n.text.value) || '（未命名）').trim()
    const byId = {}
    nodes.forEach((n) => { byId[n.id] = label(n) })
    const out = []
    if (nodes.length) out.push('节点：')
    nodes.forEach((n) => out.push('- ' + label(n)))
    if (edges.length) out.push('连线：')
    edges.forEach((e) => {
      const s2 = byId[e.sourceNodeId] || '（未知节点）'
      const t2 = byId[e.targetNodeId] || '（未知节点）'
      const txt = String((e.properties && e.properties.text) || '').trim()
      out.push(`- ${s2} → ${t2}${txt ? '（' + txt + '）' : ''}`)
    })
    return out.join('\n')
  }
  return ''
}

/**
 * 卡片预览用的单行摘要：把大纲压平，去掉缩进与层级符号。
 * 卡片位置有限，压成一行比保留缩进更易读。
 */
export function diagramPreview(raw, kind) {
  const lines = diagramOutline(raw, kind)
    .split('\n')
    .map((l) => l.replace(/^\s*-\s*/, '').trim())
    .filter((l) => l && !l.endsWith('：'))
  return lines.join(' · ')
}
