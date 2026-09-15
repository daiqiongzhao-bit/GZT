/**
 * extensions.js —— Tiptap 扩展统一装配（v0.28.0）
 *
 * 【版本】Tiptap 3.31.3（全系同版本号，2026-09-07 联网核验 npm latest）
 *
 * 【v3 与 v2 的包结构差异】（照 v2 教程写必报错）
 *   · Table/TableRow/TableCell/TableHeader 现由 @tiptap/extension-table 一个包导出
 *   · History 改名 UndoRedo，迁入 @tiptap/extensions
 *   · Placeholder / CharacterCount / Dropcursor / Gapcursor / Focus 迁入 @tiptap/extensions
 *   · Color / FontFamily / FontSize / LineHeight / BackgroundColor 收进 TextStyleKit
 *   · Underline 已内置进 StarterKit，无需单独安装
 */
import StarterKit from '@tiptap/starter-kit'
import { TextStyleKit } from '@tiptap/extension-text-style'
import { Placeholder, CharacterCount, UndoRedo } from '@tiptap/extensions'
import { Table, TableRow, TableCell, TableHeader } from '@tiptap/extension-table'
import TextAlign from '@tiptap/extension-text-align'
import Highlight from '@tiptap/extension-highlight'
import Link from '@tiptap/extension-link'
import Superscript from '@tiptap/extension-superscript'
import Subscript from '@tiptap/extension-subscript'
import { ImageAttrs } from './ImageAttrs'
import { Indent } from './Indent'
import { LegacyHtmlCompat } from './LegacyHtmlCompat'
import { FindReplace } from './FindReplace'
import { PasteImageUpload } from './PasteImageUpload'

/**
 * 构建扩展数组。
 * @param {object} opts
 * @param {string} opts.placeholder 占位文案（跟随组件 props.placeholder）
 * @param {Function} opts.onImageUpload 上传函数，返回 { dl, id, temp, name }
 * @param {Function} opts.onUploadError 上传失败回调
 * @param {Function} opts.isEditable 是否可编辑（关闭图片粘贴用）
 * @param {Function} opts.onFindReplaceChange 查找状态变化回调
 */
export function buildExtensions({ placeholder, onImageUpload, onUploadError, isEditable, onFindReplaceChange }) {
  return [
    StarterKit.configure({
      // 本次支持 H1–H4（用户已确认；存量 h5/h6 由 upgradeLegacyHtml 降级为 h4）
      heading: { levels: [1, 2, 3, 4] },
      codeBlock: { HTMLAttributes: { class: 'kb-codeblock' } },
      // 关掉内置 Link，改用下方独立配置（需要 openOnClick:false 以便点击编辑而非跳转）
      link: false,
      // bulletList/orderedList/listItem/listKeymap/underline/strike/bold/italic/code/blockquote 用默认
    }),

    // 文本外观：颜色 / 背景 / 字体 / 字号 / 行距
    TextStyleKit.configure({
      backgroundColor: { types: ['textStyle'] },
      color: { types: ['textStyle'] },
      fontFamily: { types: ['textStyle'] },
      fontSize: { types: ['textStyle'] },
      lineHeight: { types: ['textStyle'] },
    }),

    // 对齐（左/中/右/两端）；heading/paragraph/表格单元格都要支持
    TextAlign.configure({
      types: ['heading', 'paragraph', 'tableCell', 'tableHeader'],
      alignments: ['left', 'center', 'right', 'justify'],
    }),

    // 高亮（多色）
    Highlight.configure({ multicolor: true }),

    // 链接：点击不跳转（编辑态点链接应进入编辑而非离开页面）
    Link.configure({
      openOnClick: false,
      autolink: true,
      HTMLAttributes: { rel: 'noopener noreferrer', target: '_blank' },
    }),

    // 表格：resizable 支持列宽拖拽
    Table.configure({
      resizable: true,
      HTMLAttributes: { class: 'kb-table' },
    }),
    // ⚠️ TableRow / TableHeader / TableCell 必须全注册，
    //    缺任何一个都会导致 ProseMirror schema 不认识 <table>，
    //    存量表格内容被**整体丢弃**（不是渲染错，是内容消失）。
    TableRow,
    TableHeader,
    TableCell,

    Superscript,
    Subscript,

    // 自定义扩展
    ImageAttrs,
    Indent,
    LegacyHtmlCompat,
    FindReplace.configure({ onChange: onFindReplaceChange }),
    PasteImageUpload.configure({
      upload: onImageUpload,
      onError: onUploadError,
      enabled: () => (isEditable ? isEditable() : true),
    }),

    Placeholder.configure({ placeholder: () => placeholder || '在这里输入内容…' }),
    CharacterCount,
    // 撤销栈深度加大：长文档编辑可回退更多步
    UndoRedo.configure({ depth: 200, newGroupDelay: 500 }),
  ]
}

/**
 * 供工具栏使用的字号选项（Word 常用档位）
 */
export const FONT_SIZE_OPTIONS = ['12px', '14px', '16px', '18px', '20px', '24px', '28px', '32px', '40px']

/**
 * 供工具栏使用的字体选项（中西文常用）
 */
export const FONT_FAMILY_OPTIONS = [
  { label: '默认', value: '' },
  { label: '微软雅黑', value: '微软雅黑' },
  { label: '宋体', value: '宋体' },
  { label: '黑体', value: '黑体' },
  { label: '楷体', value: '楷体' },
  { label: '仿宋', value: '仿宋' },
  { label: 'Arial', value: 'Arial' },
  { label: 'Times New Roman', value: 'Times New Roman' },
]

/**
 * 行距选项
 */
export const LINE_HEIGHT_OPTIONS = [
  { label: '1.0', value: '1' },
  { label: '1.15', value: '1.15' },
  { label: '1.5', value: '1.5' },
  { label: '1.8', value: '1.8' },
  { label: '2.0', value: '2' },
  { label: '2.5', value: '2.5' },
]

/**
 * 文字颜色预设（配合取色器使用）
 */
export const TEXT_COLOR_PRESETS = [
  '#0f172a', '#475569', '#e11d48', '#d97706', '#059669', '#0ea5e9', '#4f46e5', '#8b5cf6',
]

/**
 * 高亮背景预设
 */
export const HIGHLIGHT_PRESETS = [
  '#fef08a', '#bbf7d0', '#bfdbfe', '#fecaca', '#e9d5ff', '#fed7aa',
]
