// 知识库编辑草稿（v0.31.0）：本地 + 服务端双写，防止崩溃 / 误关 / 清缓存丢内容。
//
// 本地草稿：浏览器 IndexedDB，按 key 保存（已有条目 `e:<id>`，新建条目 `n:<clientDraftId>`）。
//   零后端依赖、崩溃秒恢复；private mode 等不可用时会静默降级（不影响正常保存）。
// 服务端草稿：/api/workspace/knowledge/draft，按 (user_id, entry_id|client_draft_id) 唯一，
//   跨设备 / 清缓存也不丢。saveK 成功后由调用方负责双向清理。
import { get, put, del } from '@/api'

const DB_NAME = 'gzt-kb-draft'
const STORE = 'drafts'
const VERSION = 1
let _dbp = null

function dbPromise() {
  if (_dbp) return _dbp
  _dbp = new Promise((resolve, reject) => {
    if (typeof indexedDB === 'undefined') return reject(new Error('no-indexeddb'))
    const req = indexedDB.open(DB_NAME, VERSION)
    req.onupgradeneeded = () => {
      const db = req.result
      if (!db.objectStoreNames.contains(STORE)) db.createObjectStore(STORE, { keyPath: 'key' })
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
  return _dbp
}

async function idbPut(rec) {
  const db = await dbPromise()
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE, 'readwrite')
    tx.objectStore(STORE).put(rec)
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error)
  })
}
async function idbGet(key) {
  const db = await dbPromise()
  return new Promise((resolve, reject) => {
    const r = db.transaction(STORE, 'readonly').objectStore(STORE).get(key)
    r.onsuccess = () => resolve(r.result || null)
    r.onerror = () => reject(r.error)
  })
}
async function idbDel(key) {
  const db = await dbPromise()
  return new Promise((resolve) => {
    const tx = db.transaction(STORE, 'readwrite')
    tx.objectStore(STORE).delete(key)
    tx.oncomplete = () => resolve()
  })
}

// 本地草稿
export const localDraft = {
  // data: { title, content, kind, category, scope, tags, parent_id, status, editor_ids }
  async save(key, data) {
    try {
      await idbPut({ key, savedAt: new Date().toISOString(), ...data })
    } catch (e) {
      /* IndexedDB 不可用时静默降级 */
    }
  },
  async load(key) {
    try {
      return await idbGet(key)
    } catch (e) {
      return null
    }
  },
  async clear(key) {
    try {
      await idbDel(key)
    } catch (e) {
      /* ignore */
    }
  },
}

function qs(params) {
  const s = new URLSearchParams()
  if (params.entry_id) s.set('entry_id', params.entry_id)
  if (params.client_draft_id) s.set('client_draft_id', params.client_draft_id)
  return s.toString()
}

// 服务端草稿（按用户隔离；已有条目需可编辑）
export const serverDraft = {
  save(payload) {
    // 后端路由是 PUT（upsert），用 post 会 404 静默丢草稿
    return put('/workspace/knowledge/draft', payload)
  },
  load(params) {
    return get('/workspace/knowledge/draft?' + qs(params))
  },
  clear(params) {
    return del('/workspace/knowledge/draft?' + qs(params))
  },
}

// 生成新建条目的稳定会话键（同一新建会话内复用，避免每次输入都新建草稿行）
export function newClientDraftID() {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) return 'c_' + crypto.randomUUID()
  return 'c_' + Date.now().toString(36) + Math.random().toString(36).slice(2, 8)
}

// 由定位参数算出本地草稿 key
export function localKey(params) {
  return params.entry_id ? 'e:' + params.entry_id : 'n:' + params.client_draft_id
}
