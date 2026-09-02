import http from './http'

export const get = (url, params) => http.get(url, { params }).then((r) => r.data)
export const post = (url, data) => http.post(url, data).then((r) => r.data)
export const put = (url, data) => http.put(url, data).then((r) => r.data)
export const del = (url) => http.delete(url).then((r) => r.data)

// upload 以 multipart/form-data 上传文件（用于 CSV/Excel 导入，extra 为附加字段如 dept_id）
export const upload = (url, file, field = 'file', extra = {}) => {
  const fd = new FormData()
  fd.append(field, file)
  for (const [k, v] of Object.entries(extra)) {
    if (v !== undefined && v !== null && v !== '') fd.append(k, v)
  }
  return http.post(url, fd).then((r) => r.data)
}
