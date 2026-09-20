// ============================================================================
// v0.39.0 RBAC：树形结构前端工具
//
// 后端 /system/dept/tree、/system/role/menuTree/:id 返回的都是嵌套树。
// 权限分配界面用「扁平 + 缩进」渲染比递归组件更直观（一眼看到全貌、便于全选），
// 因此这里统一做扁平化与父子关系索引。
// ============================================================================

/**
 * 把嵌套树扁平化为带 depth 的一维数组（前序遍历，保持后端给定顺序）。
 *
 * ★ parent_id 兜底：后端 `/system/role/menuTree` 返回的菜单节点**只有 children
 *   没有 parent_id**（菜单表有该列，但接口未回传）。这里在遍历时把父 id 补进去，
 *   使 childrenIndex / descendantsMap 对菜单树同样可用。
 *
 * @param {Array} nodes 形如 [{ id, name, children: [...] }]
 * @param {number} depth 起始深度
 * @param {Array} out 累加结果（内部递归用）
 * @param {number} parentId 当前层的父 id（内部递归用）
 */
export function flattenTree(nodes, depth = 0, out = [], parentId = 0) {
  for (const n of nodes || []) {
    const { children, ...rest } = n
    out.push({ ...rest, parent_id: n.parent_id ?? parentId, children, depth })
    if (children && children.length) flattenTree(children, depth + 1, out, n.id)
  }
  return out
}

/**
 * 直接子节点索引：Map<parentId, childId[]>
 * 注意用 parent_id 字段（部门/菜单均带该字段）。
 */
export function childrenIndex(flat) {
  const m = new Map()
  for (const n of flat) {
    const p = n.parent_id ?? 0
    if (!m.has(p)) m.set(p, [])
    m.get(p).push(n.id)
  }
  return m
}

/** 每个节点的全部子孙 id（不含自身）：Map<id, number[]> */
export function descendantsMap(flat) {
  const cm = childrenIndex(flat)
  const cache = new Map()
  const collect = (id) => {
    if (cache.has(id)) return cache.get(id)
    const out = []
    for (const c of cm.get(id) || []) {
      out.push(c)
      out.push(...collect(c))
    }
    cache.set(id, out)
    return out
  }
  for (const n of flat) collect(n.id)
  return cache
}

/** 每个节点到根的祖先链 id（不含自身，从近到远） */
export function ancestorsMap(flat) {
  const byId = new Map(flat.map((n) => [n.id, n]))
  const out = new Map()
  for (const n of flat) {
    const chain = []
    let cur = byId.get(n.parent_id ?? 0)
    let guard = 0
    while (cur && guard++ < 64) {
      chain.push(cur.id)
      cur = byId.get(cur.parent_id ?? 0)
    }
    out.set(n.id, chain)
  }
  return out
}

/** 供 <select> 使用的选项：带层级缩进前缀（部门 / 上级菜单选择器） */
export function treeOptions(flat, labelKey = 'name') {
  return flat.map((n) => ({
    value: n.id,
    label: `${'　'.repeat(n.depth)}${n.depth ? '└ ' : ''}${n[labelKey] || ('#' + n.id)}`,
    depth: n.depth,
    raw: n
  }))
}

/** 勾选「父节点」时是否级联勾选全部子孙（严格模式的反面） */
export function applyCascade(set, flat, id, checked, dmap) {
  const next = new Set(set)
  if (checked) {
    next.add(id)
    for (const d of dmap.get(id) || []) next.add(d)
    // 顺带把祖先补齐，避免出现「勾了按钮但菜单不可达」的悬挂配置
    for (const a of ancestorsList(flat, id)) next.add(a)
  } else {
    next.delete(id)
    for (const d of dmap.get(id) || []) next.delete(d)
  }
  return next
}

function ancestorsList(flat, id) {
  const byId = new Map(flat.map((n) => [n.id, n]))
  const chain = []
  let cur = byId.get(id)
  let guard = 0
  while (cur && guard++ < 64) {
    const p = byId.get(cur.parent_id ?? 0)
    if (!p) break
    chain.push(p.id)
    cur = p
  }
  return chain
}
