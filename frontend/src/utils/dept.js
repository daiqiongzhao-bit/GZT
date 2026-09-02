// 部门层级工具：把扁平部门列表（含 parent_id）转成树 / 带深度的选择器选项
export function deptTree(departments) {
  const map = {}
  for (const d of departments) map[d.id] = { ...d, children: [] }
  const roots = []
  for (const d of departments) {
    const node = map[d.id]
    if (d.parent_id && map[d.parent_id]) map[d.parent_id].children.push(node)
    else roots.push(node)
  }
  return roots
}

// 展平为带 depth 的列表（下拉选择器用；depth 用于缩进显示父子关系）
export function deptOptions(departments) {
  const out = []
  const walk = (nodes, depth) => {
    for (const n of nodes) {
      out.push({ id: n.id, name: n.name, parent_id: n.parent_id, depth })
      walk(n.children, depth + 1)
    }
  }
  walk(deptTree(departments || []), 0)
  return out
}

// 完整路径名，如「物流部 / 三亚预订仓」（列表与提示用）
export function deptPathName(departments, deptId) {
  if (!deptId) return ''
  const map = {}
  for (const d of departments || []) map[d.id] = d
  const chain = []
  let cur = map[deptId]
  let guard = 0
  while (cur && guard++ < 10) {
    chain.unshift(cur.name)
    cur = cur.parent_id ? map[cur.parent_id] : null
  }
  return chain.join(' / ')
}

// 缩进占位（select 中半角空格宽度不稳定，用全角空格）
export function indentOf(depth) {
  return '　'.repeat(Math.max(0, depth))
}
