<template>
  <div class="planner">
    <!-- ============ 顶部：范围选择 + 主操作 ============ -->
    <section class="panel head-panel">
      <div class="head-grid">
        <div>
          <label class="fld">部门<span class="req-star">*</span></label>
          <select v-model.number="deptId" class="glass-input" @change="onDeptChange">
            <option v-if="auth.isSuper" :value="0">— 请选择部门 —</option>
            <option v-for="d in deptOpts" :key="d.id" :value="d.id">{{ indentOf(d.depth) + d.name }}</option>
          </select>
        </div>
        <div>
          <label class="fld">年份</label>
          <select v-model.number="year" class="glass-input" @change="onRangeChange">
            <option v-for="y in yearOptions" :key="y" :value="y">{{ y }} 年</option>
          </select>
        </div>
        <div>
          <label class="fld">月份</label>
          <select v-model.number="month" class="glass-input" @change="onRangeChange">
            <option v-for="m in 12" :key="m" :value="m">{{ m }} 月</option>
          </select>
        </div>
        <div class="head-actions">
          <button class="btn primary" :disabled="!canOperate || generating" @click="doGenerate">
            <span v-html="icons.plus"></span>{{ generating ? '生成中…' : '一键生成' }}
          </button>
          <button class="btn ghost" :disabled="!hasPlan || validing" @click="doValidate">{{ validing ? '校验中…' : '重新校验' }}</button>
          <button class="btn ghost" :disabled="!hasPlan || applying" @click="doApply">{{ applying ? '发布中…' : '发布到班表' }}</button>
        </div>
      </div>

      <div v-if="!canOperate" class="tip-line warn">
        你是执行者角色：可以提交/查看自己的需求，规则配置与排班生成由部门管理员操作。
      </div>
      <div v-else-if="!deptId" class="tip-line warn">请先在上方选择要排班的部门。</div>
    </section>

    <!-- ============ 标签页 ============ -->
    <div class="tabs">
      <button v-for="t in visibleTabs" :key="t.key" class="tab" :class="{ on: tab === t.key }" @click="tab = t.key">
        <span v-html="t.icon"></span>{{ t.label }}
        <span v-if="t.key === 'preview' && violations.length" class="tab-badge danger">{{ violations.length }}</span>
        <span v-else-if="t.key === 'requests' && myRequests.length" class="tab-badge">{{ myRequests.length }}</span>
      </button>
    </div>

    <!-- ============================================================
         Tab 1 规则配置
         ============================================================ -->
    <template v-if="tab === 'rules'">
      <section class="panel">
        <h3 class="section-title">排班规则 <span class="section-sub">{{ deptName || '未选择部门' }}</span></h3>
        <p class="hint">规则按部门保存，作用于「一键生成」。带 <b>*</b> 的为生成器硬约束。</p>

        <div class="rule-grid">
          <div class="rule-item">
            <label class="fld">最高连续休息天数 <b class="req-star">*</b></label>
            <input v-model.number="rule.max_rest_streak" type="number" min="1" max="31" class="glass-input" />
            <p class="sub">超过即报「连续休息超限」（规则2）</p>
          </div>
          <div class="rule-item">
            <label class="fld">最高连续上班天数 <b class="req-star">*</b></label>
            <input v-model.number="rule.max_work_streak" type="number" min="1" max="31" class="glass-input" />
            <p class="sub">超过即报「连续上班超限」（规则2）</p>
          </div>
          <div class="rule-item">
            <label class="fld">本月应上班天数 <b class="req-star">*</b></label>
            <input v-model.number="rule.month_work_days" type="number" min="1" max="31" class="glass-input" />
            <p class="sub">生成器的出勤目标（规则3）</p>
          </div>
          <div class="rule-item">
            <label class="fld">每班次最少人数 <b class="req-star">*</b></label>
            <input v-model.number="rule.min_per_shift" type="number" min="0" max="99" class="glass-input" />
            <p class="sub">固定班次人员不计入该名额（规则7）</p>
          </div>
          <div class="rule-item">
            <label class="fld">早班代表班次</label>
            <select v-model="rule.morning_shift_name" class="glass-input">
              <option value="">自动推断</option>
              <option v-for="s in shiftNames" :key="s" :value="s">{{ s }}</option>
            </select>
            <p class="sub">用于规则5「休假前须早班」</p>
          </div>
          <div class="rule-item">
            <label class="fld">晚班代表班次</label>
            <select v-model="rule.evening_shift_name" class="glass-input">
              <option value="">自动推断</option>
              <option v-for="s in shiftNames" :key="s" :value="s">{{ s }}</option>
            </select>
            <p class="sub">用于规则5「休假后须晚班」</p>
          </div>
        </div>

        <div class="switches">
          <label class="sw"><input type="checkbox" v-model="rule.require_morning_before_rest" /><span>休假前一天尽量排早班（规则5，软约束）</span></label>
          <label class="sw"><input type="checkbox" v-model="rule.require_evening_after_rest" /><span>休假回来第一天尽量排晚班（规则5，软约束）</span></label>
          <label class="sw"><input type="checkbox" v-model="rule.allow_exceed_month_days" /><span>允许超过本月应上班天数（规则3 只提示不判违规）</span></label>
        </div>

        <div class="rule-foot">
          <span v-if="rule.updated_by" class="section-sub">上次由 {{ rule.updated_by }} 修改</span>
          <button class="btn primary" :disabled="!canOperate || savingRule" @click="saveRule">{{ savingRule ? '保存中…' : '保存规则' }}</button>
        </div>
      </section>

      <!-- 固定班次人员（规则1） -->
      <section class="panel">
        <h3 class="section-title">固定班次设置 <span class="section-sub">规则1 · 固定人员不参与倒班，也不占用每班最少人数</span></h3>
        <p class="hint">例如行政岗固定上「早班」，他每天都是早班，不参与轮转，也不会被计入每班 ≥{{ rule.min_per_shift }} 人的名额。</p>

        <div v-if="members.length" class="fixed-table">
          <div class="ft-head">
            <span>姓名</span><span>工号</span><span>模式</span><span>固定班次</span><span>生效星期</span><span class="ta-c">操作</span>
          </div>
          <div v-for="m in members" :key="m.id" class="ft-row">
            <span class="ft-name">{{ m.name }}</span>
            <span class="ft-mono">{{ m.emp_no || '—' }}</span>
            <span>
              <select v-model="prefOf(m.id).mode" class="glass-input sm" :disabled="!canOperate">
                <option value="rotate">参与倒班</option>
                <option value="fixed">固定班次</option>
              </select>
            </span>
            <span>
              <select v-model="prefOf(m.id).fixed_shift" class="glass-input sm" :disabled="!canOperate || prefOf(m.id).mode !== 'fixed'">
                <option value="">— 请选择 —</option>
                <option v-for="s in shiftNames" :key="s" :value="s">{{ s }}</option>
              </select>
            </span>
            <span>
              <input v-model="prefOf(m.id).fixed_week_days" class="glass-input sm" :disabled="!canOperate || prefOf(m.id).mode !== 'fixed'"
                     placeholder="留空=每天，如 1,2,3" :title="'1=周一 … 7=周日，留空表示每天'" />
            </span>
            <span class="ta-c">
              <button class="btn ghost sm-btn" :disabled="!canOperate" @click="savePref(m)">保存</button>
              <button v-if="m.__hasPref" class="btn ghost sm-btn danger" :disabled="!canOperate" @click="clearPref(m)">取消</button>
            </span>
          </div>
        </div>
        <p v-else class="empty">本部门暂无人员，请先到「设置 - 人员管理」添加。</p>
      </section>
    </template>

    <!-- ============================================================
         Tab 2 员工需求
         ============================================================ -->
    <template v-if="tab === 'requests'">
      <section class="panel">
        <h3 class="section-title">提交排班需求 <span class="section-sub">规则4 · 提交后立即锁定，本人不可修改或删除</span></h3>
        <div class="req-form">
          <div>
            <label class="fld">类型</label>
            <select v-model="reqForm.type" class="glass-input">
              <option value="rest">休假</option>
              <option value="work">指定上班</option>
            </select>
          </div>
          <div>
            <label class="fld">重复方式</label>
            <select v-model="reqForm.repeat" class="glass-input">
              <option value="once">指定日期区间</option>
              <option value="weekly">每周固定</option>
            </select>
          </div>
          <template v-if="reqForm.repeat === 'once'">
            <div>
              <label class="fld">开始日期</label>
              <input v-model="reqForm.start_date" type="date" class="glass-input" />
            </div>
            <div>
              <label class="fld">结束日期</label>
              <input v-model="reqForm.end_date" type="date" class="glass-input" />
            </div>
          </template>
          <template v-else>
            <div class="span2">
              <label class="fld">星期（可多选）</label>
              <div class="wd-picks">
                <button v-for="w in weekPicks" :key="w.v" class="wd" :class="{ on: reqForm.week_days.includes(w.v) }"
                        type="button" @click="toggleWeek(w.v)">{{ w.label }}</button>
              </div>
            </div>
          </template>
          <div v-if="canOperate">
            <label class="fld">为谁提交</label>
            <select v-model.number="reqForm.user_id" class="glass-input">
              <option :value="0">我（{{ auth.user.name }}）</option>
              <option v-for="m in members" :key="m.id" :value="m.id">{{ m.name }}</option>
            </select>
          </div>
          <div class="span2">
            <label class="fld">事由（可选）</label>
            <input v-model="reqForm.reason" class="glass-input" placeholder="如：回乡探亲 / 家里有事" />
          </div>
        </div>
        <div class="rule-foot">
          <span class="tip-line">提交后该需求会被<b>锁定</b>，生成排班时强制满足；如填错请联系管理员解锁。</span>
          <button class="btn primary" :disabled="submittingReq" @click="submitRequest">{{ submittingReq ? '提交中…' : '提交并锁定' }}</button>
        </div>
      </section>

      <section class="panel">
        <h3 class="section-title">
          需求列表
          <span class="section-sub">{{ isAdminView ? deptName || '全部' : '仅显示我的需求' }}</span>
          <span v-if="lockedCount" class="chip warn">已锁定 {{ lockedCount }}</span>
        </h3>
        <div v-if="requests.length" class="req-cards">
          <div v-for="r in requests" :key="r.id" class="req-card" :class="{ locked: r.status === 'locked' }">
            <div class="rc-top">
              <b>{{ r.user_name }}</b>
              <span class="chip" :class="r.type === 'rest' ? 'warn' : 'ok'">{{ r.type === 'rest' ? '休假' : '指定上班' }}</span>
              <span class="chip" :class="r.status === 'locked' ? 'danger' : ''">{{ r.status === 'locked' ? '已锁定' : '待定' }}</span>
            </div>
            <div class="rc-desc">{{ describe(r) }}</div>
            <div v-if="r.reason" class="rc-reason">事由：{{ r.reason }}</div>
            <div class="rc-foot">
              <span class="rc-time">{{ (r.locked_at || r.created_at || '').slice(0, 16).replace('T', ' ') }}</span>
              <button v-if="canOperate && r.status === 'locked'" class="btn ghost sm-btn" @click="unlock(r)">解锁</button>
              <button v-if="canOperate" class="btn ghost sm-btn danger" @click="delRequest(r)">删除</button>
            </div>
          </div>
        </div>
        <p v-else class="empty">暂无排班需求。</p>
      </section>
    </template>

    <!-- ============================================================
         Tab 3 特殊工作日
         ============================================================ -->
    <template v-if="tab === 'special'">
      <section class="panel">
        <h3 class="section-title">特殊工作日 <span class="section-sub">规则6 · 店庆/节假日等，全员（含固定班次人员）默认上班</span></h3>
        <p class="hint">员工若已提交<b>已锁定</b>的休假需求，该日仍按休假处理（员工需求优先）。</p>
        <div class="sp-form">
          <div>
            <label class="fld">日期</label>
            <input v-model="spForm.date" type="date" class="glass-input" />
          </div>
          <div>
            <label class="fld">事由</label>
            <input v-model="spForm.name" class="glass-input" placeholder="如：店庆 / 双十一 / 春节前备货" />
          </div>
          <div>
            <label class="fld">全员上班</label>
            <select v-model="spForm.all_staff" class="glass-input">
              <option :value="true">是（默认上班）</option>
              <option :value="false">否（仅标记）</option>
            </select>
          </div>
          <div class="sp-btn">
            <button class="btn primary" :disabled="!canOperate || savingSp" @click="saveSpecialDay">{{ savingSp ? '保存中…' : '添加' }}</button>
          </div>
        </div>
      </section>

      <section class="panel">
        <h3 class="section-title">本月特殊工作日 <span class="section-sub">{{ year }} 年 {{ month }} 月</span></h3>
        <div class="cal-mini">
          <div v-for="c in miniCells" :key="c.key" class="mc" :class="{ out: !c.inMonth, sp: c.sp, today: c.isToday }">
            <span class="mc-d">{{ c.day }}</span>
            <span v-if="c.sp" class="mc-tag" :title="c.sp.name">{{ c.sp.name }}</span>
          </div>
        </div>
        <div v-if="specialDays.length" class="sp-list">
          <div v-for="s in specialDays" :key="s.id" class="sp-item">
            <b>{{ s.date }}</b>
            <span>{{ s.name }}</span>
            <span class="chip" :class="s.all_staff ? 'ok' : ''">{{ s.all_staff ? '全员上班' : '仅标记' }}</span>
            <button v-if="canOperate" class="del" @click="delSpecialDay(s)" title="删除">×</button>
          </div>
        </div>
        <p v-else class="empty">本月暂无特殊工作日。</p>
      </section>
    </template>

    <!-- ============================================================
         Tab 4 生成预览 / 微调 / 发布
         ============================================================ -->
    <template v-if="tab === 'preview'">
      <section v-if="!hasPlan" class="panel">
        <p class="empty">还没有生成结果。回到上方点击「一键生成」，或先在「规则配置」里确认参数。</p>
      </section>

      <template v-else>
        <!-- 概览条 -->
        <section class="panel sum-panel">
          <div class="sum-cards">
            <div class="sum-card"><b>{{ people.length }}</b><span>参与排班</span></div>
            <div class="sum-card"><b>{{ planDays }}</b><span>本月天数</span></div>
            <div class="sum-card" :class="{ danger: violations.length > 0 }"><b>{{ violations.length }}</b><span>规则提示</span></div>
            <div class="sum-card"><b>{{ shiftNames.length }}</b><span>可用班次</span></div>
          </div>
          <div class="legend">
            <span v-for="s in shiftNames" :key="s" class="lg"><i :class="shiftCls(s)"></i>{{ s }}</span>
            <span class="lg"><i class="rest"></i>休息</span>
          </div>
        </section>

        <!-- 提示与警告 -->
        <section v-if="notes.length || warnings.length" class="panel">
          <div v-for="(n, i) in notes" :key="'n' + i" class="msg ok">{{ n }}</div>
          <div v-for="(w, i) in warnings" :key="'w' + i" class="msg warn">{{ w }}</div>
        </section>

        <!-- 违规明细 -->
        <section class="panel">
          <h3 class="section-title">
            规则提示 <span class="section-sub">发布前请确认</span>
            <button class="btn ghost sm-btn" style="margin-left:auto" :disabled="!hasPlan || validing" @click="doValidate">重新校验</button>
          </h3>
          <div v-if="violations.length" class="vio-groups">
            <div v-for="g in violationGroups" :key="g.rule" class="vio-group">
              <div class="vg-head" @click="g.open = !g.open">
                <span class="vg-caret">{{ g.open ? '▾' : '▸' }}</span>
                <b>{{ g.label }}</b>
                <span class="chip" :class="g.level === 'error' ? 'danger' : 'warn'">{{ g.level === 'error' ? '违规' : '提醒' }}</span>
                <span class="section-sub">{{ g.items.length }} 条</span>
                <span class="vg-who">{{ g.people.join('、') }}</span>
              </div>
              <ul v-show="g.open" class="vg-list">
                <li v-for="(v, i) in g.items.slice(0, 60)" :key="i">
                  <span class="vg-date">{{ v.date }}{{ v.end_date ? ' ~ ' + v.end_date : '' }}</span>
                  <span class="vg-person">{{ v.person }}</span>
                  <span class="vg-reason">{{ v.reason }}</span>
                </li>
                <li v-if="g.items.length > 60" class="vg-more">… 另有 {{ g.items.length - 60 }} 条，发布后可在班表页查看</li>
              </ul>
            </div>
          </div>
          <p v-else class="msg ok">本计划未发现规则冲突。</p>
        </section>

        <!-- 整月矩阵微调 -->
        <section class="panel">
          <h3 class="section-title">
            整月排班 <span class="section-sub">点击任意格子可手动切换班次（改动会即时重算校验）</span>
            <label class="sw inline"><input type="checkbox" v-model="onlyViolations" /><span>只看有问题的行</span></label>
          </h3>

          <div class="matrix-scroll">
            <table class="matrix">
              <thead>
                <tr>
                  <th class="mx-name">姓名</th>
                  <th v-for="d in dayList" :key="d.d" class="mx-day"
                      :class="{ wk: d.weekend, sp: !!specialMap[d.key], dgr: dayHasViolation(d.key) }"
                      :title="(specialMap[d.key] ? specialMap[d.key] + ' · ' : '') + d.key">
                    {{ d.d }}<em>{{ d.wk }}</em>
                  </th>
                  <th class="mx-sum">出勤</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="p in matrixPeople" :key="p.name" :class="{ fixed: p.is_fixed, conflict: personViolations(p.name).length }">
                  <td class="mx-name">
                    <span class="mx-nm">{{ p.name }}</span>
                    <span v-if="p.is_fixed" class="mx-fixed" :title="'固定班次：' + p.fixed_shift">固</span>
                  </td>
                  <td v-for="d in dayList" :key="d.key" class="mx-cell"
                      :class="[shiftCls(cellOf(p.name, d.key)), { wk: d.weekend, sp: !!specialMap[d.key], vio: hasCellViolation(p.name, d.key) }]"
                      :title="cellTitle(p.name, d)"
                      @click="cycleCell(p.name, d.key)">
                    {{ cellLabel(cellOf(p.name, d.key)) }}
                  </td>
                  <td class="mx-sum">{{ workDaysOf(p.name) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <p class="hint">提示：固定班次人员（标「固」）的格子已按规则1 锁死，点击不会改变；先取消其固定班次才能手动调整。</p>
        </section>

        <section class="panel pub-panel">
          <div>
            <h3 class="section-title" style="margin:0 0 6px">发布到班表</h3>
            <p class="hint">发布会<b>先清空</b>该部门 {{ year }} 年 {{ month }} 月的既有班表，再写入本计划，并向被排班人员推送站内通知。</p>
          </div>
          <button class="btn primary" :disabled="applying" @click="doApply">{{ applying ? '发布中…' : '发布到班表' }}</button>
        </section>
      </template>
    </template>

    <!-- 手动调整班次的浮层 -->
    <div v-if="editCell" class="picker-mask" @click.self="editCell = null">
      <div class="picker glass">
        <div class="pk-head">
          <b>{{ editCell.name }}</b>
          <span class="section-sub">{{ editCell.date }}</span>
        </div>
        <div class="pk-body">
          <button v-for="s in shiftNames" :key="s" class="pk-btn" :class="shiftCls(s)"
                  :disabled="editCell.fixed" @click="setCell(s)">{{ s }}</button>
          <button class="pk-btn rest" :disabled="editCell.fixed" @click="setCell('休息')">休息</button>
        </div>
        <p v-if="editCell.fixed" class="msg warn" style="margin:0 12px 12px">
          「{{ editCell.name }}」为固定班次（{{ editCell.fixedShift }}），不参与倒班，需先在「规则配置」取消固定后才能手动调整。
        </p>
        <div class="pk-foot">
          <button class="btn ghost" @click="editCell = null">关闭</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, watch } from 'vue'
import * as api from '@/api'
import { icons } from '@/icons'
import { useAuthStore } from '@/store/auth'
import { deptOptions, indentOf } from '@/utils/dept'

const auth = useAuthStore()

// ---------- 基础状态 ----------
const tab = ref(auth.canManage ? 'rules' : 'requests')
const deptId = ref(0)
const departments = ref([])
const users = ref([])
const shiftConfigs = ref([])
const year = ref(new Date().getFullYear())
const month = ref(new Date().getMonth() + 1)

const yearOptions = computed(() => {
  const y = new Date().getFullYear()
  return Array.from({ length: 7 }, (_, i) => y - 2 + i)
})
const deptOpts = computed(() => deptOptions(departments.value))
const deptName = computed(() => deptOpts.value.find((d) => d.id === deptId.value)?.name || '')

const canOperate = computed(() => auth.canManage && deptId.value > 0)
const isAdminView = computed(() => auth.canManage)

// 当前部门直属成员（按工号/姓名排序）
const members = computed(() =>
  users.value
    .filter((u) => u.dept_id === deptId.value && u.status !== 'frozen')
    .slice()
    .sort((a, b) => (a.emp_no || '').localeCompare(b.emp_no || '', undefined, { numeric: true }) || a.name.localeCompare(b.name))
)

// 当前部门班次名（规则8）
const shiftNames = computed(() => {
  const list = shiftConfigs.value.filter((x) => x.dept_id === deptId.value)
  if (!list.length) return ['早班', '中班', '晚班', '夜班'] // 与后端 displayShifts 的回退一致
  return list
    .slice()
    .sort((a, b) => String(a.start_time || '').localeCompare(String(b.start_time || '')))
    .map((x) => x.name)
    .filter((n, i, arr) => n && arr.indexOf(n) === i)
})

const visibleTabs = computed(() => {
  const all = [
    { key: 'rules', label: '规则配置', icon: icons.settings, manage: true },
    { key: 'requests', label: '员工需求', icon: icons.users, manage: false },
    { key: 'special', label: '特殊工作日', icon: icons.calendar, manage: true },
    { key: 'preview', label: '生成预览', icon: icons.planner, manage: true }
  ]
  return all.filter((t) => !t.manage || auth.canManage)
})

// ---------- 规则 ----------
const rule = ref({
  max_rest_streak: 3,
  max_work_streak: 6,
  month_work_days: 22,
  require_morning_before_rest: true,
  require_evening_after_rest: true,
  morning_shift_name: '',
  evening_shift_name: '',
  min_per_shift: 2,
  allow_exceed_month_days: false,
  updated_by: ''
})
const savingRule = ref(false)

async function loadRule() {
  if (!deptId.value) return
  try {
    const r = await api.get('/shift-rules', { dept_id: deptId.value })
    rule.value = {
      max_rest_streak: r.max_rest_streak ?? 3,
      max_work_streak: r.max_work_streak ?? 6,
      month_work_days: r.month_work_days ?? 22,
      require_morning_before_rest: !!r.require_morning_before_rest,
      require_evening_after_rest: !!r.require_evening_after_rest,
      morning_shift_name: r.morning_shift_name || '',
      evening_shift_name: r.evening_shift_name || '',
      min_per_shift: r.min_per_shift ?? 2,
      allow_exceed_month_days: !!r.allow_exceed_month_days,
      updated_by: r.updated_by || ''
    }
  } catch (e) {
    toast(e, '规则加载失败')
  }
}

async function saveRule() {
  if (!canOperate.value) return
  savingRule.value = true
  try {
    const r = await api.put('/shift-rules', { dept_id: deptId.value, ...rule.value })
    rule.value.updated_by = r.updated_by || ''
    alert('规则已保存')
  } catch (e) {
    toast(e, '保存失败')
  } finally {
    savingRule.value = false
  }
}

// ---------- 固定班次偏好 ----------
const prefs = ref({}) // userId -> {mode, fixed_shift, fixed_week_days}
function prefEmpty() { return { mode: 'rotate', fixed_shift: '', fixed_week_days: '' } }
function prefOf(uid) {
  if (!prefs.value[uid]) prefs.value[uid] = prefEmpty()
  return prefs.value[uid]
}
async function loadPrefs() {
  if (!deptId.value) return
  try {
    const list = await api.get('/shift-prefs', { dept_id: deptId.value })
    const m = {}
    for (const p of list || []) {
      m[p.user_id] = { mode: p.mode || 'rotate', fixed_shift: p.fixed_shift || '', fixed_week_days: p.fixed_week_days || '' }
    }
    prefs.value = m
    // 标记哪些人已有记录（用于显示「取消」按钮）
    const has = new Set((list || []).map((p) => p.user_id))
    members.value.forEach((u) => { u.__hasPref = has.has(u.id) })
  } catch (e) {
    toast(e, '固定班次加载失败')
  }
}
async function savePref(m) {
  const p = prefOf(m.id)
  if (p.mode === 'fixed' && !p.fixed_shift) { alert('请选择固定班次'); return }
  try {
    await api.put('/shift-prefs', {
      user_id: m.id,
      dept_id: deptId.value,
      mode: p.mode,
      fixed_shift: p.fixed_shift,
      fixed_week_days: p.fixed_week_days
    })
    await loadPrefs()
  } catch (e) {
    toast(e, '保存失败')
  }
}
async function clearPref(m) {
  if (!confirm(`取消「${m.name}」的固定班次，恢复参与倒班？`)) return
  try {
    await api.del(`/shift-prefs/${m.id}`)
    await loadPrefs()
  } catch (e) {
    toast(e, '操作失败')
  }
}

// ---------- 员工需求 ----------
const requests = ref([])
const reqForm = reactive({ type: 'rest', repeat: 'once', start_date: '', end_date: '', week_days: [], reason: '', user_id: 0 })
const submittingReq = ref(false)
const weekPicks = [
  { v: 1, label: '一' }, { v: 2, label: '二' }, { v: 3, label: '三' }, { v: 4, label: '四' },
  { v: 5, label: '五' }, { v: 6, label: '六' }, { v: 7, label: '日' }
]
function toggleWeek(v) {
  const i = reqForm.week_days.indexOf(v)
  if (i >= 0) reqForm.week_days.splice(i, 1)
  else reqForm.week_days.push(v)
}
const myRequests = computed(() => requests.value.filter((r) => r.user_id === auth.user?.id))
const lockedCount = computed(() => requests.value.filter((r) => r.status === 'locked').length)

async function loadRequests() {
  try {
    const params = auth.canManage && deptId.value ? { dept_id: deptId.value } : undefined
    requests.value = (await api.get('/shift-requests', params)) || []
  } catch (e) {
    toast(e, '需求加载失败')
  }
}
function fmtDate(d) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
async function submitRequest() {
  const payload = {
    type: reqForm.type,
    repeat: reqForm.repeat,
    reason: reqForm.reason
  }
  if (auth.canManage && reqForm.user_id) payload.user_id = reqForm.user_id
  if (reqForm.repeat === 'weekly') {
    if (!reqForm.week_days.length) { alert('请至少选择一个星期'); return }
    payload.week_days = reqForm.week_days.slice().sort((a, b) => a - b).join(',')
  } else {
    if (!reqForm.start_date) { alert('请选择开始日期'); return }
    payload.start_date = reqForm.start_date
    payload.end_date = reqForm.end_date || reqForm.start_date
  }
  submittingReq.value = true
  try {
    await api.post('/shift-requests', payload)
    alert('已提交并锁定')
    Object.assign(reqForm, { start_date: '', end_date: '', week_days: [], reason: '', user_id: 0 })
    await loadRequests()
    invalidatePlan()
  } catch (e) {
    toast(e, '提交失败')
  } finally {
    submittingReq.value = false
  }
}
async function unlock(r) {
  if (!confirm(`解锁「${r.user_name}」的 ${describe(r)}？解锁后本人可自行修改。`)) return
  try {
    await api.post(`/shift-requests/${r.id}/unlock`, {})
    await loadRequests()
    invalidatePlan()
  } catch (e) {
    toast(e, '解锁失败')
  }
}
async function delRequest(r) {
  if (!confirm(`删除「${r.user_name}」的 ${describe(r)}？`)) return
  try {
    await api.del(`/shift-requests/${r.id}`)
    await loadRequests()
    invalidatePlan()
  } catch (e) {
    toast(e, '删除失败')
  }
}
function describe(r) {
  const typ = r.type === 'work' ? '指定上班' : '休假'
  if (r.repeat === 'weekly') {
    const nm = { 1: '一', 2: '二', 3: '三', 4: '四', 5: '五', 6: '六', 7: '日' }
    const days = String(r.week_days || '').split(',').filter(Boolean).map((d) => '周' + (nm[d] || d))
    return '每周 ' + (days.join('、') || '—') + ' ' + typ
  }
  if (r.start_date === r.end_date) return `${r.start_date} ${typ}`
  return `${r.start_date} ~ ${r.end_date} ${typ}`
}

// ---------- 特殊工作日 ----------
const specialDays = ref([])
const spForm = reactive({ date: '', name: '', all_staff: true })
const savingSp = ref(false)
const specialMap = computed(() => {
  const m = {}
  for (const s of specialDays.value) m[s.date] = s.name
  return m
})
async function loadSpecial() {
  if (!deptId.value) return
  try {
    specialDays.value = (await api.get('/special-workdays', { dept_id: deptId.value, from: monthRange().from, to: monthRange().to })) || []
  } catch (e) {
    toast(e, '特殊工作日加载失败')
  }
}
async function saveSpecialDay() {
  if (!spForm.date) { alert('请选择日期'); return }
  if (!spForm.name.trim()) { alert('请填写事由'); return }
  savingSp.value = true
  try {
    await api.post('/special-workdays', { dept_id: deptId.value, date: spForm.date, name: spForm.name.trim(), all_staff: spForm.all_staff })
    spForm.name = ''
    await loadSpecial()
    invalidatePlan()
  } catch (e) {
    toast(e, '添加失败')
  } finally {
    savingSp.value = false
  }
}
async function delSpecialDay(s) {
  if (!confirm(`删除特殊工作日「${s.date} ${s.name}」？`)) return
  try {
    await api.del(`/special-workdays/${s.id}`)
    await loadSpecial()
    invalidatePlan()
  } catch (e) {
    toast(e, '删除失败')
  }
}

// ---------- 生成 / 校验 / 发布 ----------
const generating = ref(false)
const validing = ref(false)
const applying = ref(false)
const plan = ref(null)          // { "2026-09-01": { "张三": "早班" } }
const people = ref([])
const violations = ref([])
const notes = ref([])
const warnings = ref([])
const hasPlan = computed(() => !!plan.value && Object.keys(plan.value).length > 0)

function invalidatePlan() {
  // 规则/需求/特殊工作日变更后，旧计划已不代表当前配置
  if (hasPlan.value) plan.value = null
}

function monthRange() {
  const from = `${year.value}-${String(month.value).padStart(2, '0')}-01`
  const lastDay = new Date(year.value, month.value, 0).getDate()
  const to = `${year.value}-${String(month.value).padStart(2, '0')}-${String(lastDay).padStart(2, '0')}`
  return { from, to }
}

async function doGenerate() {
  if (!canOperate.value) return
  generating.value = true
  try {
    const r = await api.post('/schedules/generate', { dept_id: deptId.value, year: year.value, month: month.value })
    plan.value = r.plan || {}
    people.value = r.people || []
    violations.value = r.violations || []
    notes.value = r.notes || []
    warnings.value = r.warnings || []
    tab.value = 'preview'
  } catch (e) {
    alert(e.response?.data?.error || '生成失败')
  } finally {
    generating.value = false
  }
}

async function doValidate() {
  if (!hasPlan.value) return
  validing.value = true
  try {
    const r = await api.post('/schedules/validate', {
      dept_id: deptId.value, year: year.value, month: month.value, plan: plan.value
    })
    violations.value = r.violations || []
  } catch (e) {
    toast(e, '校验失败')
  } finally {
    validing.value = false
  }
}

async function doApply() {
  if (!hasPlan.value) return
  const errN = violations.value.filter((v) => v.level === 'error').length
  let msg = `确认把 ${year.value} 年 ${month.value} 月的排班发布到班表？\n\n该部门本月既有班表将被覆盖。`
  if (errN) msg += `\n\n⚠ 当前仍有 ${errN} 条违规未处理，建议先修正。`
  if (!confirm(msg)) return
  applying.value = true
  try {
    const r = await api.post('/schedules/apply', {
      dept_id: deptId.value, year: year.value, month: month.value, plan: plan.value
    })
    violations.value = r.violations || []
    let out = `发布成功：写入 ${r.created} 条班次记录。`
    if (r.notes?.length) out += '\n\n' + r.notes.join('\n')
    alert(out)
  } catch (e) {
    alert(e.response?.data?.error || '发布失败')
  } finally {
    applying.value = false
  }
}

// ---------- 矩阵与微调 ----------
const onlyViolations = ref(false)
const editCell = ref(null)

const dayList = computed(() => {
  const lastDay = new Date(year.value, month.value, 0).getDate()
  return Array.from({ length: lastDay }, (_, i) => {
    const d = i + 1
    const dt = new Date(year.value, month.value - 1, d)
    return {
      d,
      key: `${year.value}-${String(month.value).padStart(2, '0')}-${String(d).padStart(2, '0')}`,
      weekend: dt.getDay() === 0 || dt.getDay() === 6,
      wk: ['日', '一', '二', '三', '四', '五', '六'][dt.getDay()]
    }
  })
})
const planDays = computed(() => dayList.value.length)

const matrixPeople = computed(() => {
  let list = people.value.slice()
  if (onlyViolations.value) {
    const bad = new Set(violations.value.map((v) => v.person))
    list = list.filter((p) => bad.has(p.name))
  }
  return list
})

function cellOf(name, dateKey) {
  return (plan.value?.[dateKey] || {})[name] || '休息'
}
function cellLabel(s) {
  if (!s || s === '休息') return '休'
  return String(s).replace('班', '') || s
}
function cellTitle(name, d) {
  const s = cellOf(name, d.key)
  const sp = specialMap.value[d.key] ? `特殊工作日：${specialMap.value[d.key]}\n` : ''
  return `${sp}${name} · ${d.key}\n班次：${s === '休息' ? '休息' : s}\n点击可修改`
}
function workDaysOf(name) {
  return dayList.value.filter((d) => {
    const s = cellOf(name, d.key)
    return s && s !== '休息'
  }).length
}

// 班次配色（与班表页一致：早=蓝、中=绿、晚=橙、夜=紫，未知班次回落灰）
function shiftCls(s) {
  if (!s || s === '休息') return 'rest'
  const key = shiftConfigs.value.find((x) => x.dept_id === deptId.value && x.name === s)?.color_key
  const map = { blue: 'accent', green: 'ok', orange: 'warn', purple: 'purple' }
  if (key && map[key]) return map[key]
  return { 早班: 'accent', 中班: 'ok', 晚班: 'warn', 夜班: 'purple' }[s] || 'other'
}

function cycleCell(name, dateKey) {
  // 固定班次人员（规则1）：打开浮层但禁用全部班次按钮，并在浮层内说明原因。
  // 用浮层而非 alert，是为了让「为什么不能改」与可点的选项出现在同一处。
  const p = people.value.find((x) => x.name === name)
  editCell.value = { name, date: dateKey, fixed: !!(p && p.is_fixed), fixedShift: p?.fixed_shift || '' }
}
async function setCell(s) {
  if (!editCell.value || editCell.value.fixed) return
  const { name, date } = editCell.value
  if (!plan.value[date]) plan.value[date] = {}
  plan.value[date][name] = s
  editCell.value = null
  await doValidate()
}

// 单元格级违规定位：把「人 + 日期区间」展开成可命中集合
const cellVioSet = computed(() => {
  const set = new Set()
  for (const v of violations.value) {
    if (!v.date) continue
    const end = v.end_date || v.date
    let d = new Date(v.date + 'T00:00:00')
    const e = new Date(end + 'T00:00:00')
    let guard = 0
    while (!isNaN(d) && d <= e && guard++ < 400) {
      set.add(v.person + '|' + d.toISOString().slice(0, 10))
      d = new Date(d.getTime() + 86400000)
    }
  }
  return set
})
function hasCellViolation(name, dateKey) { return cellVioSet.value.has(name + '|' + dateKey) }
function dayHasViolation(dateKey) { return violations.value.some((v) => (v.end_date || v.date) >= dateKey && v.date <= dateKey) }
function personViolations(name) { return violations.value.filter((v) => v.person === name) }

const violationGroups = computed(() => {
  const m = new Map()
  for (const v of violations.value) {
    if (!m.has(v.rule)) m.set(v.rule, { rule: v.rule, label: v.label, level: v.level, items: [], people: new Set(), open: true })
    const g = m.get(v.rule)
    g.items.push(v)
    g.people.add(v.person)
    if (v.level === 'error') g.level = 'error' // 同规则内以更严重级别展示
  }
  return Array.from(m.values()).map((g) => ({ ...g, people: Array.from(g.people).slice(0, 8) }))
})

// 迷你月历（特殊工作日页）
const miniCells = computed(() => {
  const first = new Date(year.value, month.value - 1, 1)
  const offset = (first.getDay() + 6) % 7 // 周一为一周起点
  const lastDay = new Date(year.value, month.value, 0).getDate()
  const today = new Date()
  const out = []
  for (let i = 0; i < offset; i++) out.push({ key: 'p' + i, day: '', inMonth: false })
  for (let d = 1; d <= lastDay; d++) {
    const key = `${year.value}-${String(month.value).padStart(2, '0')}-${String(d).padStart(2, '0')}`
    out.push({
      key, day: d, inMonth: true, sp: specialDays.value.find((s) => s.date === key) || null,
      isToday: today.getFullYear() === year.value && today.getMonth() + 1 === month.value && today.getDate() === d
    })
  }
  return out
})

// ---------- 生命周期 ----------
function toast(e, fallback) {
  alert(e.response?.data?.error || fallback)
}

async function loadBase() {
  const [deps, us, sfs] = await Promise.all([
    api.get('/departments').catch(() => []),
    api.get('/users').catch(() => []),
    api.get('/shift-configs').catch(() => [])
  ])
  departments.value = deps || []
  users.value = us || []
  shiftConfigs.value = sfs || []
  if (!deptId.value) {
    deptId.value = auth.isSuper
      ? (departments.value.find((d) => (us || []).some((u) => u.dept_id === d.id))?.id || departments.value[0]?.id || 0)
      : (auth.user?.dept_id || 0)
  }
}

async function refreshDeptData() {
  if (!deptId.value) return
  await Promise.all([loadRule(), loadPrefs(), loadRequests(), loadSpecial()])
}

function onDeptChange() {
  invalidatePlan()
  people.value = []
  violations.value = []
  notes.value = []
  warnings.value = []
  refreshDeptData()
}
function onRangeChange() {
  invalidatePlan()
  loadSpecial()
}

onMounted(async () => {
  await loadBase()
  await refreshDeptData()
})

// 需求列表在「员工需求」标签下才需要最新；切换时刷新一次，避免看到过期数据
watch(tab, (t) => {
  if (t === 'requests') loadRequests()
  if (t === 'special') loadSpecial()
})
</script>

<style scoped>
.planner { display: flex; flex-direction: column; gap: 14px; }

/* 顶部面板 */
.head-panel { padding: 16px 20px; }
.head-grid { display: grid; grid-template-columns: repeat(3, minmax(150px, 1fr)) auto; gap: 12px; align-items: end; }
.head-actions { display: flex; gap: 8px; align-items: end; }
.req-star { color: var(--danger); font-style: normal; }
.tip-line { font-size: 12px; color: var(--text-faint); }
.tip-line.warn { color: var(--warn); }
.hint { font-size: 12px; color: var(--text-faint); margin: 0 0 12px; line-height: 1.6; }

/* 标签页 */
.tabs { display: flex; gap: 6px; flex-wrap: wrap; }
.tab {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 9px 15px; border-radius: 12px; font-size: 13px; cursor: pointer;
  border: 1px solid var(--glass-border); background: var(--glass); color: var(--text-dim); transition: .15s;
}
.tab:hover { border-color: var(--glass-border-strong); color: var(--text); }
.tab.on { background: var(--accent-soft); color: var(--accent); border-color: var(--glass-border-strong); font-weight: 700; }
.tab svg { width: 15px; height: 15px; }
.tab-badge { min-width: 17px; height: 17px; padding: 0 5px; border-radius: 9px; font-size: 11px; line-height: 17px; text-align: center; background: var(--accent-soft); color: var(--accent); }
.tab-badge.danger { background: rgba(225, 29, 72, .12); color: var(--danger); }

/* 规则表单 */
.rule-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 16px; }
.rule-item .sub { font-size: 11.5px; color: var(--text-faint); margin: 6px 0 0; line-height: 1.5; }
.switches { display: flex; flex-direction: column; gap: 10px; margin: 18px 0 0; }
.sw { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--text-dim); cursor: pointer; }
.sw input { accent-color: var(--accent); width: 15px; height: 15px; }
.sw.inline { margin-left: auto; font-weight: 400; }
.rule-foot { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 18px; padding-top: 14px; border-top: 1px solid var(--glass-border); flex-wrap: wrap; }

/* 固定班次表 */
.fixed-table { font-size: 13px; }
.ft-head, .ft-row { display: grid; grid-template-columns: 1.1fr .9fr 1.1fr 1.1fr 1.2fr auto; gap: 10px; align-items: center; padding: 9px 0; }
.ft-head { color: var(--text-faint); font-size: 12px; border-bottom: 1px solid var(--glass-border); }
.ft-row { border-bottom: 1px solid var(--glass-border); }
.ft-row:last-child { border-bottom: none; }
.ft-name { font-weight: 600; }
.ft-mono { font-family: ui-monospace, monospace; font-size: 12px; color: var(--text-faint); }
.glass-input.sm { padding: 6px 9px; font-size: 12.5px; border-radius: 9px; }
.sm-btn { padding: 6px 11px; font-size: 12px; border-radius: 9px; }
.ta-c { text-align: center; }

/* 需求表单 */
.req-form { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 14px; }
.span2 { grid-column: span 2; }
.wd-picks { display: flex; gap: 6px; flex-wrap: wrap; }
.wd { width: 36px; height: 34px; border-radius: 10px; border: 1px solid var(--glass-border); background: var(--overlay); color: var(--text-dim); font-size: 13px; cursor: pointer; }
.wd.on { background: var(--accent-soft); color: var(--accent); border-color: var(--glass-border-strong); font-weight: 700; }

.req-cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 12px; }
.req-card { border: 1px solid var(--glass-border); border-radius: 14px; padding: 13px 14px; background: var(--overlay); }
.req-card.locked { border-color: rgba(225, 29, 72, .28); }
.rc-top { display: flex; align-items: center; gap: 7px; flex-wrap: wrap; margin-bottom: 7px; }
.rc-desc { font-size: 13px; font-weight: 600; color: var(--text); }
.rc-reason { font-size: 12px; color: var(--text-dim); margin-top: 5px; }
.rc-foot { display: flex; align-items: center; gap: 8px; margin-top: 11px; }
.rc-time { font-size: 11.5px; color: var(--text-faint); margin-right: auto; font-family: ui-monospace, monospace; }

/* 特殊工作日 */
.sp-form { display: grid; grid-template-columns: 1fr 1.4fr 1fr auto; gap: 12px; align-items: end; }
.sp-btn { display: flex; }
.cal-mini { display: grid; grid-template-columns: repeat(7, 1fr); gap: 5px; margin-bottom: 16px; }
.mc { min-height: 46px; border-radius: 10px; border: 1px solid var(--glass-border); padding: 4px 6px; font-size: 12px; color: var(--text-dim); display: flex; flex-direction: column; gap: 2px; }
.mc.out { border: none; background: transparent; }
.mc.today { border-color: var(--accent); }
.mc.sp { background: rgba(5, 150, 105, .10); border-color: rgba(5, 150, 105, .34); }
.mc-d { font-weight: 600; }
.mc-tag { font-size: 10px; color: var(--ok); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sp-list { display: flex; flex-direction: column; gap: 8px; }
.sp-item { display: flex; align-items: center; gap: 10px; font-size: 13px; padding: 8px 0; border-bottom: 1px solid var(--glass-border); }
.sp-item b { font-family: ui-monospace, monospace; min-width: 92px; }
.sp-item .del { margin-left: auto; border: none; background: none; color: var(--text-faint); cursor: pointer; font-size: 17px; line-height: 1; }
.sp-item .del:hover { color: var(--danger); }

/* 概览条 */
.sum-panel { display: flex; align-items: center; gap: 18px; flex-wrap: wrap; }
.sum-cards { display: flex; gap: 12px; flex-wrap: wrap; }
.sum-card { min-width: 86px; border: 1px solid var(--glass-border); border-radius: 12px; padding: 10px 14px; text-align: center; background: var(--overlay); }
.sum-card b { display: block; font-size: 20px; font-weight: 800; }
.sum-card span { font-size: 11.5px; color: var(--text-faint); }
.sum-card.danger b { color: var(--danger); }
.legend { display: flex; gap: 12px; flex-wrap: wrap; margin-left: auto; font-size: 12px; color: var(--text-dim); }
.lg { display: inline-flex; align-items: center; gap: 5px; }
.legend i { width: 11px; height: 11px; border-radius: 4px; display: inline-block; }
.legend i.accent { background: #4f46e5; } .legend i.ok { background: #059669; }
.legend i.warn { background: #d97706; } .legend i.purple { background: #7c3aed; }
.legend i.other { background: #64748b; } .legend i.rest { background: var(--text-faint); opacity: .5; }

/* 消息 */
.msg { font-size: 12.5px; padding: 9px 12px; border-radius: 10px; margin: 0 0 8px; line-height: 1.6; }
.msg:last-child { margin-bottom: 0; }
.msg.ok { background: rgba(5, 150, 105, .08); border: 1px solid rgba(5, 150, 105, .26); color: var(--ok); }
.msg.warn { background: rgba(217, 119, 6, .09); border: 1px solid rgba(217, 119, 6, .28); color: var(--warn); }

/* 违规分组 */
.vio-groups { display: flex; flex-direction: column; gap: 10px; }
.vio-group { border: 1px solid var(--glass-border); border-radius: 12px; overflow: hidden; }
.vg-head { display: flex; align-items: center; gap: 9px; padding: 11px 13px; cursor: pointer; background: var(--overlay); font-size: 13px; flex-wrap: wrap; }
.vg-caret { color: var(--text-faint); }
.vg-who { margin-left: auto; font-size: 11.5px; color: var(--text-faint); }
.vg-list { margin: 0; padding: 4px 0; list-style: none; max-height: 300px; overflow-y: auto; }
.vg-list li { display: grid; grid-template-columns: 150px 90px 1fr; gap: 10px; padding: 7px 14px; font-size: 12.5px; border-bottom: 1px solid var(--glass-border); }
.vg-list li:last-child { border-bottom: none; }
.vg-date { font-family: ui-monospace, monospace; color: var(--text-faint); }
.vg-person { font-weight: 600; }
.vg-reason { color: var(--text-dim); }
.vg-more { color: var(--text-faint); font-style: italic; display: block; }

/* 整月矩阵 */
.matrix-scroll { overflow-x: auto; }
.matrix { border-collapse: separate; border-spacing: 0; font-size: 11.5px; }
.matrix th, .matrix td { border-bottom: 1px solid var(--glass-border); padding: 0; text-align: center; }
.mx-name { position: sticky; left: 0; z-index: 2; background: var(--glass-strong); min-width: 104px; text-align: left; padding: 6px 10px !important; font-size: 12.5px; white-space: nowrap; }
thead .mx-name { background: var(--glass-strong); font-weight: 700; }
.mx-day { min-width: 30px; width: 30px; padding: 5px 0 !important; font-weight: 500; color: var(--text-dim); font-size: 11px; line-height: 1.25; }
.mx-day em { display: block; font-style: normal; font-size: 9.5px; color: var(--text-faint); }
.mx-day.wk { color: var(--warn); }
.mx-day.sp { background: rgba(5, 150, 105, .13); }
.mx-day.dgr { box-shadow: inset 0 -2px 0 var(--danger); }
.mx-sum { position: sticky; right: 0; z-index: 2; background: var(--glass-strong); min-width: 48px; font-weight: 700; padding: 6px 8px !important; }
.mx-cell { cursor: pointer; height: 27px; transition: .12s; border-right: 1px solid transparent; }
.mx-cell:hover { filter: brightness(1.14); box-shadow: inset 0 0 0 1.5px var(--accent); }
.mx-cell.wk { background: rgba(217, 119, 6, .05); }
.mx-cell.sp { background: rgba(5, 150, 105, .10); }
.mx-cell.vio { box-shadow: inset 0 0 0 1.5px var(--danger); }
.mx-cell.accent { background: rgba(79, 70, 229, .16); color: #4f46e5; font-weight: 600; }
.mx-cell.ok { background: rgba(5, 150, 105, .15); color: #059669; font-weight: 600; }
.mx-cell.warn { background: rgba(217, 119, 6, .16); color: #b45309; font-weight: 600; }
.mx-cell.purple { background: rgba(124, 58, 237, .15); color: #7c3aed; font-weight: 600; }
.mx-cell.other { background: rgba(100, 116, 139, .14); color: #475569; font-weight: 600; }
.mx-cell.rest { color: var(--text-faint); }
.mx-nm { font-weight: 600; }
.mx-fixed { display: inline-block; margin-left: 5px; padding: 1px 5px; border-radius: 5px; font-size: 9.5px; background: rgba(217, 119, 6, .14); color: var(--warn); font-weight: 700; vertical-align: 1px; }
.matrix tbody tr.fixed .mx-name { background: rgba(217, 119, 6, .06); }
.matrix tbody tr.conflict .mx-name { box-shadow: inset 3px 0 0 var(--danger); }

.pub-panel { display: flex; align-items: center; gap: 18px; flex-wrap: wrap; }
.pub-panel > div { flex: 1; min-width: 240px; }

/* 班次选择浮层 */
.picker-mask { position: fixed; inset: 0; z-index: 60; background: rgba(15, 23, 42, .45); display: grid; place-items: center; padding: 16px; }
.picker { border-radius: 16px; width: min(420px, 94vw); overflow: hidden; }
.pk-head { display: flex; align-items: baseline; gap: 10px; padding: 14px 16px 10px; font-size: 14px; }
.pk-body { display: flex; flex-wrap: wrap; gap: 8px; padding: 0 16px 14px; }
.pk-btn { padding: 9px 15px; border-radius: 11px; border: 1px solid var(--glass-border); background: var(--overlay); color: var(--text); font-size: 13px; cursor: pointer; }
.pk-btn:hover:not(:disabled) { border-color: var(--accent); }
.pk-btn:disabled { opacity: .4; cursor: not-allowed; }
.pk-btn.rest { color: var(--text-dim); }
.pk-foot { display: flex; justify-content: flex-end; padding: 0 16px 14px; }

/* 移动端 */
@media (max-width: 820px) {
  .head-grid { grid-template-columns: 1fr 1fr; }
  .head-actions { grid-column: span 2; }
  .head-actions .btn { flex: 1; justify-content: center; }
  .rule-grid { grid-template-columns: 1fr; }
  .fixed-table { overflow-x: auto; }
  .ft-head, .ft-row { grid-template-columns: 96px 1fr auto; min-width: 480px; }
  .ft-head { display: none; }
  .ft-row { padding: 12px 0; row-gap: 8px; }
  .req-form { grid-template-columns: 1fr; }
  .span2 { grid-column: span 1; }
  .sp-form { grid-template-columns: 1fr; }
  .sp-btn .btn { width: 100%; justify-content: center; }
  .sum-cards { width: 100%; }
  .sum-card { flex: 1; min-width: 0; }
  .legend { margin-left: 0; }
  .vg-list li { grid-template-columns: 1fr; gap: 2px; }
  .pub-panel .btn { width: 100%; justify-content: center; }
}
</style>
