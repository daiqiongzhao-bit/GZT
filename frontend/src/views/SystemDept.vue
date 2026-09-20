<template>
  <div class="adm-page">
    <div class="adm-head">
      <h2 class="section-title">
        部门管理
        <span class="section-sub">树形结构；ancestors 存祖级路径，是「本部门及以下」数据范围的展开依据</span>
      </h2>
      <div class="adm-actions">
        <button class="btn" :disabled="loading" @click="load">刷新</button>
        <button v-if="auth.can('system:dept:add')" class="btn primary" @click="openCreate(null)">新增部门</button>
      </div>
    </div>

    <!-- ==================== 部门管理员落地说明 ==================== -->
    <section class="panel">
      <h3 class="adm-sub-title">部门管理员是怎么落地的（零新增模型）</h3>
      <div class="adm-kv">
        <span class="k">管理边界</span>
        <span class="v">由<b>用户自己的 dept_id</b> 锚定 —— 同一个「部门管理员」角色绑给不同部门的人，边界自动不同，因此不需要新建任何"管理员表"或"管理范围"字段。</span>
        <span class="k">数据范围</span>
        <span class="v">角色上的 <b>data_scope</b>（{{ '1 全部 / 2 自定义 / 3 本部门 / 4 本部门及以下 / 5 仅本人' }}）；部门管理员标准档 = <b>4 本部门及以下</b>。</span>
        <span class="k">可见</span>
        <span class="v">本部门及子部门用户（读取时服务端注入过滤条件，前端传参无法突破）。</span>
        <span class="k">可管</span>
        <span class="v">写操作走<b>归属断言</b>：在"持有该权限的角色"子集内取最宽，避免借读范围拿写权限。</span>
        <span class="k">停用部门</span>
        <span class="v">仅"不允许新分配用户、不出现在选择器"；<b>存量用户照常计算数据范围</b>，否则一停用整部门 403，属运维事故。</span>
      </div>
    </section>

    <!-- ==================== 部门树 ==================== -->
    <section class="panel">
      <div class="adm-tablewrap">
        <table class="adm-table">
          <thead>
            <tr>
              <th>部门名称</th>
              <th>负责人</th>
              <th class="num">排序</th>
              <th class="num">用户数</th>
              <th>状态</th>
              <th>祖级路径（ancestors）</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="d in flat" :key="d.id">
              <td>
                <span class="adm-indent" :style="{ width: d.depth * 18 + 'px' }"></span>
                <span v-if="d.depth" class="adm-tree-mark">└</span>
                {{ d.name }}
              </td>
              <td class="dim">{{ d.leader || '—' }}</td>
              <td class="num dim">{{ d.order_num }}</td>
              <td class="num">
                <span class="chip" :class="d.user_count ? 'ok' : ''">{{ d.user_count }}</span>
              </td>
              <td>
                <span v-if="d.status" class="chip danger">停用</span>
                <span v-else class="chip ok">正常</span>
              </td>
              <td class="mono dim">{{ d.ancestors || '0' }}</td>
              <td>
                <div class="adm-row-actions">
                  <button v-if="auth.can('system:dept:add')" class="adm-link" @click="openCreate(d)">新增下级</button>
                  <button v-if="auth.can('system:dept:edit')" class="adm-link" @click="openEdit(d)">编辑</button>
                  <button
                    v-if="auth.can('system:dept:remove')"
                    class="adm-link danger"
                    :disabled="!!d.children?.length || d.user_count > 0"
                    :title="blockReason(d)"
                    @click="doDelete(d)"
                  >删除</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!flat.length" class="empty">{{ loading ? '加载中…' : '暂无部门数据' }}</div>
      <p class="adm-hint">
        共 {{ flat.length }} 个部门。删除受两条硬约束：<b>有子部门不可删</b>、<b>有用户不可删</b>（后端强制，前端已置灰可预判的部分）。
        移动部门时后端会<b>级联重写全部子孙的 ancestors</b>，并禁止把部门移到它自己或它的子部门下（防成环）。
      </p>
    </section>

    <!-- ==================== 新增 / 编辑 ==================== -->
    <div v-if="edit.show" class="adm-mask" @click.self="edit.show = false">
      <div class="adm-modal">
        <h3>{{ edit.isCreate ? '新增部门' : '编辑部门' }}</h3>
        <p class="adm-modal-sub">所属部门决定用户的<b>数据可见范围</b>与管理归属；改上级会自动重算本部门及全部子孙的 ancestors。</p>

        <div class="adm-field">
          <label class="fld">上级部门</label>
          <select v-model="edit.form.parent_id" class="glass-input">
            <option :value="0">顶级部门</option>
            <option v-for="o in parentOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
        </div>

        <div class="adm-grid2">
          <div class="adm-field">
            <label class="fld">部门名称 *</label>
            <input v-model.trim="edit.form.name" class="glass-input" placeholder="如：三亚预订仓" />
          </div>
          <div class="adm-field">
            <label class="fld">负责人</label>
            <input v-model.trim="edit.form.leader" class="glass-input" placeholder="姓名" />
          </div>
          <div class="adm-field">
            <label class="fld">显示排序</label>
            <input v-model.number="edit.form.order_num" type="number" class="glass-input" />
          </div>
          <div class="adm-field">
            <label class="fld">状态</label>
            <select v-model.number="edit.form.status" class="glass-input">
              <option :value="0">正常</option>
              <option :value="1">停用</option>
            </select>
          </div>
        </div>

        <div v-if="edit.form.status === 1 && edit.curUserCount > 0" class="adm-alert warn">
          该部门当前有 <b>{{ edit.curUserCount }}</b> 个用户。停用后这些用户<b>仍旧照常计算数据范围</b>
          （否则会造成整部门 403）；停用只影响"新分配用户"与"部门选择器"。如需真正收权，请先把用户调整到其他部门。
        </div>
        <div class="adm-alert info">
          约束：同级下不允许重名（后端按 <b>(parent_id, name)</b> 判重）；不能把部门移动到它自己或它的子部门下。
        </div>

        <div class="adm-modal-foot">
          <button class="btn ghost" @click="edit.show = false">取消</button>
          <button class="btn primary" :disabled="saving" @click="submit">{{ saving ? '保存中…' : '保存' }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import * as api from '@/api'
import { useAuthStore } from '@/store/auth'
import { flattenTree, treeOptions } from '@/utils/rbacTree'

const auth = useAuthStore()

const loading = ref(false)
const saving = ref(false)
const tree = ref([])
const flat = ref([])

/** 只有"正常"部门能作为父节点（后端在 CreateDept 里也拒绝停用的上级） */
const parentOptions = computed(() => {
  const active = flat.value.filter((d) => d.status === 0 && d.id !== edit.id)
  const byId = new Map(active.map((d) => [d.id, { ...d, children: [] }]))
  const roots = []
  for (const n of byId.values()) {
    const p = byId.get(n.parent_id || 0)
    if (p) p.children.push(n)
    else roots.push(n)
  }
  return treeOptions(flattenTree(roots), 'name')
})

async function load() {
  loading.value = true
  try {
    const r = await api.get('/system/dept/tree')
    tree.value = r.tree || []
    flat.value = flattenTree(tree.value, 0, [], 0)
  } catch (e) {
    tree.value = []
    flat.value = []
    alert(errMsg(e, '加载部门失败'))
  } finally { loading.value = false }
}

function blockReason(d) {
  if (d.children?.length) return `有 ${d.children.length} 个子部门，需先删除子部门`
  if (d.user_count > 0) return `有 ${d.user_count} 个用户，需先调整用户所属部门`
  return ''
}

// ---------------------------------------------------------------- 新增 / 编辑
const edit = reactive({
  show: false, isCreate: true, id: 0, curUserCount: 0,
  form: { parent_id: 0, name: '', leader: '', order_num: 0, status: 0 }
})

function openCreate(parent) {
  Object.assign(edit, {
    show: true, isCreate: true, id: 0, curUserCount: 0,
    form: { parent_id: parent ? parent.id : 0, name: '', leader: '', order_num: 0, status: 0 }
  })
}

function openEdit(d) {
  Object.assign(edit, {
    show: true, isCreate: false, id: d.id, curUserCount: d.user_count || 0,
    form: {
      parent_id: d.parent_id || 0, name: d.name, leader: d.leader || '',
      order_num: d.order_num || 0, status: d.status || 0
    }
  })
}

async function submit() {
  const f = edit.form
  if (!f.name) { alert('请填写部门名称'); return }
  if (!edit.isCreate && f.parent_id === edit.id) { alert('上级部门不能是自己'); return }
  saving.value = true
  try {
    if (edit.isCreate) {
      await api.post('/system/dept', { parent_id: f.parent_id, name: f.name, leader: f.leader, order_num: f.order_num, status: f.status })
    } else {
      await api.put('/system/dept', { id: edit.id, parent_id: f.parent_id, name: f.name, leader: f.leader, order_num: f.order_num, status: f.status })
    }
    edit.show = false
    await load()
  } catch (e) {
    alert(errMsg(e, '保存失败'))
  } finally { saving.value = false }
}

async function doDelete(d) {
  if (!confirm(`确定删除部门「${d.name}」吗？\n有子部门或有用户时后端会拒绝删除。`)) return
  try {
    await api.post(`/system/dept/delete/${d.id}`)
    await load()
  } catch (e) {
    alert(errMsg(e, '删除失败'))
  }
}

function errMsg(e, dft) {
  return (e && e.response && e.response.data && e.response.data.error) || dft
}

onMounted(load)
</script>
