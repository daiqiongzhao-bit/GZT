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

// download 取二进制附件（返回 Blob，配合 URL.createObjectURL 触发浏览器下载）。
// ★ 必须走 axios 的**配置位**传 responseType —— get(url, params) 的第二个参数是查询参数，
//   写成 get(url, { responseType: 'blob' }) 会把 responseType 当查询串发出去，
//   响应仍按文本解析，拿到的不是 Blob，createObjectURL 会直接抛错（附件永远下不来）。
//   附件走「服务端读盘 + 可能经反代」，默认 15s 偏紧，这里单独放宽到 60s。
export const download = (url, params) =>
  http.get(url, { params, responseType: 'blob', timeout: 60000 }).then((r) => r.data)
