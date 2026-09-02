<template>
  <div class="settings">
    <div class="tabs">
      <button class="tab" :class="{ active: tab === 'me' }" @click="tab = 'me'">个人信息</button>
      <button v-if="auth.isSuper" class="tab" :class="{ active: tab === 'brand' }" @click="tab = 'brand'">企业信息</button>
      <button v-if="auth.canManage" class="tab" :class="{ active: tab === 'dept' }" @click="tab = 'dept'">部门</button>
      <button v-if="auth.canManage" class="tab" :class="{ active: tab === 'user' }" @click="tab = 'user'">人员</button>
      <button v-if="auth.canManage" class="tab" :class="{ active: tab === 'tmpl' }" @click="tab = 'tmpl'">模板</button>
      <button v-if="auth.canManage" class="tab" :class="{ active: tab === 'hook' }" @click="tab = 'hook'">通知</button>
      <button v-if="auth.canManage" class="tab" :class="{ active: tab === 'log' }" @click="tab = 'log'">操作审计</button>
      <button v-if="auth.isSuper" class="tab" :class="{ active: tab === 'backup' }" @click="tab = 'backup'">备份</button>
    </div>

    <!-- 个人信息 -->
    <section v-if="tab === 'me'" class="panel">
      <h3 class="section-title">个人信息</h3>
      <div class="form-col">
        <div class="info-row"><label class="fld">姓名</label><span class="info-val">{{ auth.user?.name }}</span></div>
        <div class="info-row"><label class="fld">工号</label><span class="info-val">{{ auth.user?.emp_no || '—' }}</span></div>
        <div class="info-row"><label class="fld">账号</label><span class="info-val">{{ auth.user?.username }}</span></div>
        <div class="info-row"><label class="fld">角色</label><span class="info-val">{{ auth.roleLabel }}</span></div>
        <div class="info-row"><label class="fld">部门</label><span class="info-val">{{ auth.user?.dept?.name || '—' }}</span></div>
        <div class="info-row"><label class="fld">手机号</label><span class="info-val">{{ auth.user?.mobile || '—' }}</span></div>
      </div>
      <h3 class="section-title" style="margin-top:20px;">修改密码</h3>
      <div class="form-col" style="max-width:380px;">
        <div><label class="fld">当前密码</label><input v-model="pw.old_password" type="password" class="glass-input" autocomplete="current-password" /></div>
        <div><label class="fld">新密码</label><input v-model="pw.new_password" type="password" class="glass-input" autocomplete="new-password" placeholder="至少 6 位" /></div>
        <div><label class="fld">确认新密码</label><input v-model="pw.confirm" type="password" class="glass-input" autocomplete="new-password" /></div>
        <button class="btn primary" :disabled="pwSaving" @click="changePwd">{{ pwSaving ? '保存中…' : '修改密码' }}</button>
      </div>
    </section>

    <!-- 企业信息（仅超管可见） -->
    <section v-if="tab === 'brand' && auth.isSuper" class="panel">
      <h3 class="section-title">企业品牌设置</h3>
      <div class="form-col">
        <div><label class="fld">企业名称</label><input v-model="brand.company_name" class="glass-input" /></div>
        <div><label class="fld">标语</label><input v-model="brand.slogan" class="glass-input" /></div>
        <div><label class="fld">版权信息</label><input v-model="brand.copyright" class="glass-input" /></div>
        <div><label class="fld">系统版本</label><input :value="brand.version" class="glass-input" disabled /></div>
        <div>
          <label class="fld">企业 Logo</label>
          <div class="logo-edit">
            <div class="logo-preview">
              <img v-if="brand.logo" :src="logoUrl" alt="企业 Logo" />
              <span v-else>暂无</span>
            </div>
            <div class="logo-ops">
              <input ref="logoInput" type="file" accept="image/png,image/jpeg,image/webp,image/svg+xml,image/x-icon,.ico" hidden @change="onPickLogo" />
              <div class="logo-btns">
                <button class="btn ghost" :disabled="logoSaving" @click="logoInput && logoInput.click()">{{ brand.logo ? '更换图片' : '上传图片' }}</button>
                <button v-if="brand.logo" class="btn ghost" :disabled="logoSaving" @click="removeLogo">移除</button>
              </div>
              <p class="hint">支持 PNG / JPG / WEBP / SVG / ICO，≤2MB。位图（PNG/JPG/ICO）上传后会自动抠掉纯色背景变透明并统一转 PNG，侧边栏与登录页立即生效；点「移除」恢复系统默认图标。</p>
            </div>
          </div>
        </div>
        <div>
          <label class="fld">系统时区</label>
          <select v-model="brand.timezone" class="glass-input">
            <option v-for="tz in timezones" :key="tz.id" :value="tz.id">{{ tz.label }}</option>
          </select>
          <p class="hint" style="color:var(--text-dim); font-size:12px; margin:6px 0 0; line-height:1.6;">影响任务逾期判定、今日/本月统计与到点推送时间。当前服务器时间：<b>{{ serverNow || '—' }}</b></p>
        </div>
      </div>
      <div class="form-actions" style="justify-content:flex-start; gap:10px;">
        <button class="btn primary" :disabled="saving" @click="saveBrand">{{ saving ? '保存中…' : '保存设置' }}</button>
        <button class="btn ghost" :disabled="tzSaving" @click="saveTimezone">{{ tzSaving ? '应用中…' : '保存时区' }}</button>
      </div>
    </section>

    <!-- 部门（超管管理，部门管/超管可配班次时间） -->
    <section v-if="tab === 'dept' && auth.canManage" class="panel">
      <h3 class="section-title">部门管理 <span class="section-sub">增删部门仅超管；班次上下班时间本部门管理员可配置</span></h3>
      <div v-if="auth.isSuper" class="inline-add dept-add">
        <select v-model.number="newDeptParent" class="glass-input" style="max-width:180px;">
          <option :value="0">顶级部门</option>
          <option v-for="d in deptOptions(departments)" :key="d.id" :value="d.id">{{ indentOf(d.depth) + d.name }}</option>
        </select>
        <input v-model="newDept" class="glass-input" placeholder="新部门名称" @keyup.enter="addDept" />
        <button class="btn primary" :disabled="!newDept" @click="addDept">添加</button>
      </div>
      <div class="list">
        <div v-for="d in deptOptions(departments)" :key="d.id" class="row dept-row" :style="d.depth ? 'padding-left:' + (12 + d.depth * 18) + 'px' : ''">
          <div class="row-main dept-head">
            <span class="rn">{{ d.name }}<span v-if="d.depth" class="section-sub" style="margin-left:6px">子部门</span></span>
            <button v-if="auth.isSuper" class="del" @click="delDept(d)">删除部门</button>
          </div>
          <div class="shift-cfg">
            <div class="shift-cfg-list">
              <span v-for="sc in shiftsOf(d.id)" :key="sc.id" class="shift-chip">
                {{ sc.name }} {{ sc.start_time }}-{{ sc.end_time }}
                <button class="mini" @click="delShift(sc)">×</button>
              </span>
              <span v-if="!shiftsOf(d.id).length" class="shift-none">尚未配置班次，添加一个</span>
            </div>
            <div class="shift-add">
              <input v-model="scForm.name" class="glass-input sm" placeholder="班次名，如 中班" />
              <input v-model="scForm.start_time" type="time" class="glass-input sm" />
              <span class="shift-sep">至</span>
              <input v-model="scForm.end_time" type="time" class="glass-input sm" />
              <button class="btn ghost sm" @click="addShift(d)">添加班次</button>
            </div>
          </div>
        </div>
        <div v-if="!departments.length" class="empty">暂无部门</div>
      </div>
    </section>

    <!-- 人员 -->
    <section v-if="tab === 'user'" class="panel">
      <h3 class="section-title">人员管理 <span v-if="!auth.canManage" class="section-sub">仅管理员可操作</span></h3>
      <div v-if="auth.canManage" class="user-toolbar">
        <button class="btn ghost" @click="downloadAuth('templates/user-template')">⬇ 人员导入模板</button>
        <label class="btn ghost imp-label">
          {{ userImporting ? '导入中…' : '⬆ 批量导入人员' }}
          <input type="file" accept=".xlsx,.csv" :disabled="userImporting" @change="importUsers" hidden />
        </label>
        <span class="section-sub">按模板填好上传即可，登录账号已存在则更新资料</span>
      </div>
      <div v-if="auth.canManage" class="add-user-fold">
        <button class="btn ghost" @click="showAddUser = !showAddUser">
          <svg v-if="!showAddUser" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="width:14px;height:14px;vertical-align:-2px"><path d="M12 5v14M5 12h14"/></svg>
          <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="width:14px;height:14px;vertical-align:-2px"><path d="M5 12h14"/></svg>
          {{ showAddUser ? '收起新增' : '新增人员' }}
        </button>
        <div v-show="showAddUser" class="add-form" style="margin-top:12px;">
        <div class="fg2">
          <div><label class="fld">姓名 *</label><input v-model="u.name" class="glass-input" /></div>
          <div><label class="fld">工号 *（登录账号）</label><input v-model="u.emp_no" class="glass-input" placeholder="如 3275，自动作为登录账号" /></div>
        </div>
        <div v-if="u.role === 'super_admin'" class="fg2">
          <div><label class="fld">登录账号 *（超管）</label><input v-model="u.username" class="glass-input" placeholder="超管无工号，单独填账号" /></div>
        </div>
        <div class="fg2">
          <div><label class="fld">密码 *</label><input v-model="u.password" type="password" class="glass-input" /></div>
          <div><label class="fld">角色</label>
            <select v-model="u.role" class="glass-input" :disabled="!auth.isSuper">
              <option value="dept_admin">部门管理员</option>
              <option value="executor">执行者</option>
              <option v-if="auth.isSuper" value="super_admin">超级管理员</option>
            </select>
          </div>
        </div>
        <div class="fg2">
          <div><label class="fld">部门</label>
            <select v-model="u.dept_id" class="glass-input" :disabled="!auth.isSuper">
              <option v-for="d in deptOptions(departments)" :key="d.id" :value="d.id">{{ indentOf(d.depth) + d.name }}</option>
            </select>
          </div>
          <div><label class="fld">手机号</label><input v-model="u.mobile" class="glass-input" placeholder="企业微信@提醒用（可选）" /></div>
        </div>
        <div class="form-actions"><button class="btn primary" :disabled="savingU" @click="addUser">{{ savingU ? '添加中…' : '添加人员' }}</button></div>
        </div>
      </div>
      <div class="list">
        <div v-for="p in users" :key="p.id" class="row" :class="{ frozen: p.frozen }">
          <div class="row-main">
            <span class="avatar sm" :class="{ 'frozen-av': p.frozen }">{{ (p.name || '?')[0] }}</span>
            <div>
              <div class="rn">{{ p.name }} <span class="chip" style="margin-left:6px">{{ roleMap[p.role] || p.role }}</span><span v-if="p.frozen" class="chip warn" style="margin-left:4px">已冻结</span><span v-if="p.in_group" class="chip" style="margin-left:4px;color:#16a34a;border-color:#16a34a66">群内</span><span v-if="onlineMap[p.id]" class="chip online" style="margin-left:4px">● {{ fmtOnline(onlineMap[p.id]) }}</span></div>
              <div class="ru">@{{ p.username }}<span v-if="p.emp_no"> · 工号 {{ p.emp_no }}</span> · {{ p.dept?.name || '—' }}<span v-if="p.mobile" class="mob"> · 电话 {{ p.mobile }}</span></div>
            </div>
          </div>
          <div class="row-actions" v-if="auth.isSuper || (auth.canManage && p.role !== 'super_admin')">
            <div class="ops" @click.stop>
              <button class="mini ops-btn" :class="{ on: opsOpen === p.id }" @click="toggleOps(p.id)">操作 ▾</button>
              <div v-if="opsOpen === p.id" class="ops-drop">
                <button class="op" @click="runOp(p, 'edit')">编辑资料</button>
                <button class="op" @click="runOp(p, 'freeze')">{{ p.frozen ? '解冻账号' : '冻结账号' }}</button>
                <button class="op" @click="runOp(p, 'pwd')">重置密码</button>
                <button v-if="auth.isSuper" class="op" @click="runOp(p, 'unlock')">解锁登录</button>
                <button v-if="auth.isSuper" class="op" :disabled="!onlineMap[p.id]" @click="runOp(p, 'logout')">强制下线<span v-if="!onlineMap[p.id]" class="op-hint">离线</span></button>
                <button v-if="p.id !== auth.user?.id" class="op danger" @click="runOp(p, 'del')">删除人员</button>
              </div>
            </div>
          </div>
          <div v-else class="row-actions">
            <span class="ru" style="font-size:12px">超管</span>
          </div>
        </div>
        <div v-if="!users.length" class="empty">暂无人员</div>
      </div>

      <!-- 编辑人员弹窗 -->
      <div v-if="editUser" class="modal-mask" @click.self="editUser = null">
        <div class="modal">
          <div class="modal-head"><span>编辑人员 · {{ editUser.name }}</span><button class="x" @click="editUser = null">×</button></div>
          <div class="modal-body">
            <div class="field"><label class="fld">姓名</label><input v-model="editForm.name" class="glass-input" /></div>
            <div class="field"><label class="fld">工号（登录账号）</label><input v-model="editForm.emp_no" class="glass-input" placeholder="如 3275" /><span style="display:block;font-size:12px;color:var(--text-faint);margin-top:4px">改工号会同步修改登录账号，该员工需用新工号重新登录</span></div>
            <div class="field"><label class="fld">手机号</label><input v-model="editForm.mobile" class="glass-input" placeholder="企业微信@提醒用（可选）" /></div>
            <div class="field"><label class="fld">已入群</label>
              <label style="display:flex;align-items:center;gap:8px;font-size:13px;cursor:pointer"><input type="checkbox" v-model="editForm.in_group" style="width:16px;height:16px" /> 已加入企业微信通知群（推送会@TA，名单中不重复列出）</label>
            </div>
            <div class="field"><label class="fld">角色</label>
              <select v-model="editForm.role" class="glass-input" :disabled="!auth.isSuper">
                <option value="dept_admin">部门管理员</option>
                <option value="executor">执行者</option>
                <option v-if="auth.isSuper" value="super_admin">超级管理员</option>
              </select>
            </div>
            <div class="field"><label class="fld">部门</label>
              <select v-model="editForm.dept_id" class="glass-input" :disabled="!auth.isSuper">
                <option v-for="d in deptOptions(departments)" :key="d.id" :value="d.id">{{ indentOf(d.depth) + d.name }}</option>
              </select>
            </div>
            <div class="form-actions" style="margin-top:14px;">
              <button class="btn ghost" @click="editUser = null">取消</button>
              <button class="btn primary" :disabled="editSaving" @click="saveEdit">{{ editSaving ? '保存中…' : '保存' }}</button>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- 模板管理（管理员可查看下载，超管可修改） -->
    <section v-if="tab === 'tmpl' && auth.canManage" class="panel">
      <h3 class="section-title">固定模板 <span class="section-sub">下载后按格式填好，在班表/任务页上传即可导入（Excel 或 CSV 都支持）</span></h3>
      <div class="form-actions" style="justify-content:flex-start; gap:10px; flex-wrap:wrap;">
        <button class="btn primary" @click="downloadAuth('templates/schedule-template')">⬇ 班表模板（Excel 矩阵）</button>
        <button class="btn primary" @click="downloadAuth('templates/task-template')">⬇ 任务模板（每日+月度）</button>
        <button class="btn primary" @click="downloadAuth('templates/user-template')">⬇ 人员导入模板</button>
        <button class="btn ghost" @click="downloadAuth('templates/schedule-sample')">⬇ 班表 CSV 样例</button>
        <button class="btn ghost" @click="downloadAuth('templates/task-sample')">⬇ 任务 CSV 样例</button>
        <button class="btn ghost" @click="exportTasks">⬇ 导出当前任务</button>
      </div>
      <p class="hint" style="font-size:12.5px;color:var(--text-dim);margin-top:10px;line-height:1.7;">
        班表模板：人×日期矩阵，每人一行，第 1~31 列填 早/中/晚/夜/休；右上角「信息」表填部门与年月。<br>
        任务模板：Sheet「每日工作内容」（时间/时段工作/负责班次）→ 每日任务；Sheet「月度工作内容」（N号/业务主题/工作内容）→ 每月任务。
      </p>

      <h3 class="section-title" style="margin-top:20px;">自定义模板 <span class="section-sub" v-if="!auth.isSuper">（仅查看 / 下载，修改需超管）</span></h3>
      <div class="list">
        <div v-for="t in templates" :key="t.id" class="row">
          <div class="row-main">
            <div>
              <div class="rn">{{ t.name }} <span class="chip" style="margin-left:6px">{{ t.type === 'task' ? '任务' : '班表' }}</span></div>
              <div class="ru">由 {{ t.created_by || '—' }} 维护 · {{ fmt(t.updated_at || t.created_at) }}</div>
            </div>
          </div>
          <div class="row-actions">
            <button class="mini" @click="downloadAuth('templates/' + t.id + '/download')">下载</button>
            <button v-if="auth.isSuper" class="mini" @click="editTemplate(t)">修改</button>
            <button v-if="auth.isSuper" class="del" @click="deleteTemplate(t)">×</button>
          </div>
        </div>
        <div v-if="!templates.length" class="empty">暂无自定义模板，超管可点击下方新增</div>
      </div>

      <div v-if="auth.isSuper" class="add-form" style="margin-top:16px;">
        <div class="fg2">
          <div><label class="fld">模板类型</label>
            <select v-model="tmplForm.type" class="glass-input">
              <option value="task">任务</option>
              <option value="schedule">班表</option>
            </select>
          </div>
          <div><label class="fld">模板名称</label><input v-model="tmplForm.name" class="glass-input" placeholder="如：标准每日任务模板" /></div>
        </div>
        <div><label class="fld">模板内容（CSV，含表头）</label>
          <textarea v-model="tmplForm.content" class="glass-input import-ta" rows="7"
            placeholder="标题,班次,类型,时间,优先级,备注,负责人&#10;开门检查,早班,每日,09:00,高,,"></textarea>
        </div>
        <div class="form-actions">
          <button class="btn ghost" @click="resetTmplForm">重置</button>
          <button class="btn primary" :disabled="tmplSaving" @click="saveTemplate">{{ tmplSaving ? '保存中…' : (tmplForm.id ? '保存修改' : '新增模板') }}</button>
        </div>
      </div>
    </section>

    <!-- 通知：多渠道 + 邮件 -->
    <section v-if="tab === 'hook' && auth.canManage" class="panel">
      <h3 class="section-title">渠道通知 <span class="section-sub">地址与密钥均加密存储</span></h3>
      <div v-if="auth.canManage" class="add-form">
        <div class="fg2">
          <div><label class="fld">名称</label><input v-model="h.name" class="glass-input" placeholder="如：企业微信机器人" /></div>
          <div><label class="fld">类型</label>
            <select v-model="h.type" class="glass-input">
              <option value="wecom">企业微信</option>
              <option value="dingtalk">钉钉</option>
              <option value="feishu">飞书</option>
            </select>
          </div>
        </div>
        <div class="fg2">
          <div><label class="fld">部门 <em style="font-style:normal;color:#e11d48;font-size:11px">必选</em></label>
            <select v-model="h.dept_id" class="glass-input" :class="{ 'req-miss': !h.dept_id }" :disabled="!auth.isSuper">
              <option :value="0">— 请选择部门 —</option>
              <option v-for="d in deptOptions(departments)" :key="d.id" :value="d.id">{{ indentOf(d.depth) + d.name }}</option>
            </select>
          </div>
          <div><label class="fld">加签密钥（钉钉/飞书选填）</label><input v-model="h.secret" class="glass-input" placeholder="机器人安全设置里的加签" /></div>
        </div>
        <div><label class="fld">Webhook 地址</label><input v-model="h.url" class="glass-input" placeholder="https://..." /></div>
        <div class="form-actions">
          <button class="btn ghost" :disabled="testing" @click="testHook">{{ testing ? '测试中…' : '测试连接' }}</button>
          <button v-if="editHookId" class="btn ghost" @click="cancelEditHook">取消编辑</button>
          <button class="btn primary" :disabled="savingH" @click="addHook">{{ savingH ? '保存中…' : (editHookId ? '保存修改' : '添加') }}</button>
        </div>
      </div>
      <div class="list">
        <div v-for="w in hooks" :key="w.id" class="row">
          <div class="row-main">
            <div><div class="rn">{{ w.name }} <span class="chip" style="margin-left:6px">{{ typeLabel(w.type) }}</span><span class="ru" style="font-size:12px;color:var(--text-dim)">（{{ deptNameOf(w.dept_id) }}）</span></div><div class="ru mono">{{ w.url }}</div></div>
          </div>
          <button v-if="auth.canManage" class="op" style="margin-right:4px" @click="editHook(w)">编辑</button>
          <button v-if="auth.canManage" class="del" @click="delHook(w)">删除</button>
        </div>
        <div v-if="!hooks.length" class="empty">暂无通知渠道</div>
      </div>
      <p v-if="editHookId" class="hint">正在编辑「{{ hookEditingName }}」，改完点「保存修改」；密钥留空表示不修改</p>

      <h3 class="section-title foldable" :class="{ open: smtpOpen }" style="margin-top:20px;" @click="smtpOpen = !smtpOpen">
        <svg class="caret" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 6l6 6-6 6"/></svg>
        邮件通知（SMTP） <span class="section-sub">仅超管配置，点标题展开</span>
      </h3>
      <div v-show="smtpOpen" class="form-col" style="max-width:520px;">
        <div class="fg2">
          <div><label class="fld">SMTP 主机</label><input v-model="smtp.host" class="glass-input" placeholder="如 smtp.qq.com" /></div>
          <div><label class="fld">端口</label><input v-model.number="smtp.port" type="number" class="glass-input" placeholder="465 或 587" /></div>
        </div>
        <div class="fg2">
          <div><label class="fld">发件账号</label><input v-model="smtp.user" class="glass-input" /></div>
          <div><label class="fld">发件人</label><input v-model="smtp.from" class="glass-input" placeholder="显示用发件地址" /></div>
        </div>
        <div><label class="fld">密码 / 授权码</label><input v-model="smtp.pass" type="password" class="glass-input" placeholder="留空表示不修改" /></div>
        <div><label class="fld">接收邮箱（逗号分隔）</label><input v-model="smtp.emails" class="glass-input" placeholder="a@x.com,b@y.com" /></div>
        <div class="form-actions" style="justify-content:flex-start; gap:10px;">
          <button class="btn ghost" :disabled="smtpTesting" @click="testEmail">{{ smtpTesting ? '发送中…' : '发送测试邮件' }}</button>
          <button class="btn primary" :disabled="smtpSaving" @click="saveSMTP">{{ smtpSaving ? '保存中…' : '保存邮件配置' }}</button>
        </div>
      </div>
    </section>

    <!-- 操作审计日志 -->
    <section v-if="tab === 'log' && auth.canManage" class="panel">
      <h3 class="section-title">操作审计日志 <button class="btn ghost sm" style="margin-left:auto" @click="exportLogs">⬇ 导出 CSV</button></h3>
      <div v-if="auth.isSuper" class="retention-bar">
        <label class="fld" style="margin:0;">日志保留</label>
        <input v-model.number="logRetention" type="number" min="0" max="3650" class="glass-input" style="max-width:110px;" />
        <span class="retention-hint">天（0 = 永久保留，默认 90；到期自动清理）</span>
        <button class="btn ghost" :disabled="logRetentionSaving" @click="saveLogRetention">{{ logRetentionSaving ? '保存中…' : '保存' }}</button>
      </div>
      <div class="log-filter">
        <input v-model="logFilter.user_name" class="glass-input" placeholder="按操作人筛选" @keyup.enter="logPage = 0; loadLogs()" />
        <input v-model="logFilter.action" class="glass-input" placeholder="按内容关键词筛选" @keyup.enter="logPage = 0; loadLogs()" />
        <select v-model.number="logFilter.limit" class="glass-input" @change="logPage = 0; loadLogs()">
          <option :value="50">50 条/页</option>
          <option :value="100">100 条/页</option>
          <option :value="200">200 条/页</option>
        </select>
        <button class="btn ghost" @click="logPage = 0; loadLogs()">筛选</button>
        <button class="btn ghost" @click="resetLogFilter">重置</button>
      </div>
      <div class="log-list">
        <div v-for="l in logs" :key="l.id" class="log">
          <span class="log-time">{{ fmt(l.created_at) }}</span>
          <span class="log-user">{{ l.user_name }}</span>
          <span class="log-ip" v-if="l.ip">{{ l.ip }}</span>
          <span class="log-action">{{ l.action }}</span>
        </div>
        <div v-if="!logs.length" class="empty">暂无日志</div>
      </div>
      <div class="pager" v-if="logs.length">
        <button class="btn ghost" :disabled="logPage === 0" @click="logPage > 0 && (logPage--, loadLogs())">上一页</button>
        <span class="pager-info">第 {{ logPage + 1 }} 页</span>
        <button class="btn ghost" :disabled="logs.length < logFilter.limit" @click="logPage++; loadLogs()">下一页</button>
      </div>
    </section>

    <!-- 备份还原（仅超管） -->
    <section v-if="tab === 'backup' && auth.isSuper" class="panel">
      <h3 class="section-title">系统备份与还原 <span class="section-sub">仅超级管理员</span></h3>
      <p class="hint" style="color:var(--text-dim); font-size:13px; margin:0 0 14px; line-height:1.6;">手动备份可生成当前数据库的完整快照；还原操作危险，会立即覆盖当前数据并自动重启连接。</p>
      <div class="form-col">
        <div>
          <label class="fld">立即备份</label>
          <button class="btn primary" :disabled="backupCreating" @click="createBackup">{{ backupCreating ? '备份中…' : '创建备份' }}</button>
        </div>
        <div>
          <label class="fld">自动备份频率</label>
          <div style="display:flex; gap:8px; flex-wrap:wrap;">
            <button class="btn ghost" :class="{ active: backupCfg.frequency === 'none' }" @click="setBackupFreq('none')">关闭</button>
            <button class="btn ghost" :class="{ active: backupCfg.frequency === 'daily' }" @click="setBackupFreq('daily')">每天</button>
            <button class="btn ghost" :class="{ active: backupCfg.frequency === 'weekly' }" @click="setBackupFreq('weekly')">每周</button>
          </div>
        </div>
        <div>
          <label class="fld">保留份数</label>
          <div style="display:flex; gap:8px; align-items:center;">
            <input v-model.number="backupCfg.retention" type="number" min="1" max="999" class="glass-input" style="max-width:120px;" />
            <button class="btn ghost" :disabled="backupCfgSaving" @click="saveBackupCfg">{{ backupCfgSaving ? '保存中…' : '保存' }}</button>
          </div>
        </div>
        <div>
          <label class="fld">异地备份（WebDAV）</label>
          <input v-model="backupCfg.remote_dir" class="glass-input" placeholder="留空=仅本地；https://用户:密码@dav服务器/目录" />
          <p class="hint" style="color:var(--text-dim); font-size:12px; margin:6px 0 0; line-height:1.6;">支持 WebDAV（坚果云 / Nextcloud / 群晖等）：每次备份自动上传一份到远端，并按上方保留份数清理远端旧备份。保存时会先校验连通性。</p>
        </div>
      </div>
      <h3 class="section-title" style="margin-top:20px;">备份列表 <span class="section-sub">最新在前</span></h3>
      <div v-if="backupLoading" class="empty">加载中…</div>
      <div v-else-if="!backups || !backups.length" class="empty">暂无备份</div>
      <div v-else class="backup-list">
        <div v-for="b in backups" :key="b.id" class="backup-row">
          <div>
            <div class="b-name">{{ b.name }}</div>
            <div class="b-meta">{{ b.size_human }} · {{ b.created_at }}<span v-if="b.remote" class="b-remote"> · 已同步异地</span></div>
          </div>
          <div class="b-actions">
            <button class="btn ghost" @click="downloadBackup(b)">下载</button>
            <button class="btn ghost" @click="restoreBackup(b)">还原</button>
            <button class="del" @click="deleteBackup(b)">×</button>
          </div>
        </div>
      </div>
    </section>

    <!-- 退出登录 -->
    <section v-if="tab === 'logout'" class="panel" style="text-align:center;">
      <h3 class="section-title">退出登录</h3>
      <p class="hint" style="color:var(--text-dim); font-size:13px; margin:0 0 16px; line-height:1.6;">点击下方按钮退出当前登录，并令本设备令牌立即失效。</p>
      <button class="btn danger" @click="onLogout" style="max-width:260px; margin:0 auto;">退出登录</button>
    </section>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import * as api from '@/api'
import { useAuthStore } from '@/store/auth'
import { brand, loadBrand } from '@/brand'
import { deptOptions, indentOf } from '@/utils/dept'

const auth = useAuthStore()
const router = useRouter()
const tab = ref('me')
const roleMap = { super_admin: '超级管理员', dept_admin: '部门管理员', executor: '执行者' }
const typeLabel = (t) => ({ wecom: '企业微信', dingtalk: '钉钉', feishu: '飞书' }[t] || '企业微信')

const saving = ref(false)
const departments = ref([])
const users = ref([])
const hooks = ref([])
const logs = ref([])
const templates = ref([])
const shiftConfigs = ref([])

const newDept = ref('')
const newDeptParent = ref(0)
const u = reactive({ name: '', emp_no: '', username: '', password: '', mobile: '', role: 'executor', dept_id: null, in_group: false })
const savingU = ref(false)
const showAddUser = ref(false) // 新增人员表单：默认收起，点「新增人员」展开
const smtpOpen = ref(false)    // SMTP 配置：默认收起，点标题展开
const h = reactive({ name: '', url: '', dept_id: null, type: 'wecom', secret: '' })
const savingH = ref(false)
const testing = ref(false)
const editHookId = ref(0)        // 正在编辑的 Webhook id；0 表示新增
const hookEditingName = ref('')  // 编辑提示里显示的名称
function deptNameOf(id) {
  const d = departments.value.find((x) => x.id === id)
  return d ? d.name : (id === 0 ? '全局' : `#${id}`)
}

// 修改个人密码
const pw = reactive({ old_password: '', new_password: '', confirm: '' })
const pwSaving = ref(false)
async function changePwd() {
  if (!pw.old_password || !pw.new_password) { alert('请填写当前密码和新密码'); return }
  if (pw.new_password.length < 6) { alert('新密码至少 6 位'); return }
  if (pw.new_password !== pw.confirm) { alert('两次输入的新密码不一致'); return }
  pwSaving.value = true
  try {
    await api.post('/auth/change-password', { old_password: pw.old_password, new_password: pw.new_password })
    alert('密码修改成功')
    Object.assign(pw, { old_password: '', new_password: '', confirm: '' })
  } catch (e) { alert(e.response?.data?.error || '修改失败') }
  finally { pwSaving.value = false }
}

function fmt(s) { return (s || '').replace('T', ' ').slice(0, 19) }

async function loadDepts() { departments.value = await api.get('/departments') }
async function loadUsers() { users.value = await api.get('/users') }
async function loadHooks() { hooks.value = await api.get('/webhooks') }
async function loadTemplates() { templates.value = await api.get('/templates') }
async function loadShiftConfigs() { shiftConfigs.value = await api.get('/shift-configs') }

// 部门班次配置
const scForm = reactive({ name: '', start_time: '09:00', end_time: '18:00' })
function shiftsOf(deptId) { return shiftConfigs.value.filter((sc) => sc.dept_id === deptId) }
async function addShift(d) {
  const name = scForm.name.trim()
  if (!name) { alert('请填写班次名称，如 中班'); return }
  if (!scForm.start_time || !scForm.end_time) { alert('请选择上班/下班时间'); return }
  try {
    await api.post('/shift-configs', { dept_id: d.id, name, start_time: scForm.start_time, end_time: scForm.end_time })
    scForm.name = ''
    await loadShiftConfigs()
  } catch (e) { alert(e.response?.data?.error || '添加失败') }
}
async function delShift(sc) {
  if (!confirm(`删除班次「${sc.name} ${sc.start_time}-${sc.end_time}」？`)) return
  try { await api.del(`/shift-configs/${sc.id}`); await loadShiftConfigs() } catch (e) { alert(e.response?.data?.error || '删除失败') }
}

async function saveBrand() {
  saving.value = true
  try {
    Object.assign(brand, await api.post('/settings', { company_name: brand.company_name, slogan: brand.slogan, copyright: brand.copyright }))
    alert('已保存，侧边栏与登录页已同步更新')
  } catch (e) { alert(e.response?.data?.error || '保存失败') }
  finally { saving.value = false }
}

// 企业 Logo 上传
const logoInput = ref(null)
const logoSaving = ref(false)
const logoUrl = computed(() => '/api/settings/logo?v=' + encodeURIComponent(brand.logo || ''))
async function onPickLogo(e) {
  const f = e.target.files && e.target.files[0]
  e.target.value = ''
  if (!f) return
  if (f.size > 2 * 1024 * 1024) { alert('图片不能超过 2MB'); return }
  logoSaving.value = true
  try {
    Object.assign(brand, await api.upload('/settings/logo', f))
    alert('Logo 已更新，侧边栏与登录页立即生效')
  } catch (err) { alert(err.response?.data?.error || '上传失败') }
  finally { logoSaving.value = false }
}
async function removeLogo() {
  if (!confirm('确定移除企业 Logo，恢复为默认图标？')) return
  logoSaving.value = true
  try { Object.assign(brand, await api.del('/settings/logo')) } catch (e) { alert(e.response?.data?.error || '移除失败') }
  finally { logoSaving.value = false }
}

// 系统时区
const timezones = [
  { id: 'Asia/Shanghai', label: '中国标准时间（北京）UTC+8' },
  { id: 'UTC', label: 'UTC 协调世界时' },
  { id: 'Asia/Hong_Kong', label: '香港 UTC+8' },
  { id: 'Asia/Taipei', label: '台北 UTC+8' },
  { id: 'Asia/Singapore', label: '新加坡 UTC+8' },
  { id: 'Asia/Tokyo', label: '东京 UTC+9' },
  { id: 'Asia/Seoul', label: '首尔 UTC+9' },
  { id: 'Europe/London', label: '伦敦（欧洲西部时间）' },
  { id: 'Europe/Berlin', label: '柏林（欧洲中部时间）' },
  { id: 'America/New_York', label: '纽约（东部时间）' },
  { id: 'America/Los_Angeles', label: '洛杉矶（太平洋时间）' },
  { id: 'Australia/Sydney', label: '悉尼（澳大利亚东部时间）' }
]
const tzSaving = ref(false)
const serverNow = ref('')
async function saveTimezone() {
  tzSaving.value = true
  try {
    const r = await api.post('/settings/timezone', { timezone: brand.timezone })
    serverNow.value = r.now || ''
    Object.assign(brand, { timezone: r.timezone })
    alert(`时区已保存并生效，服务器当前时间：${serverNow.value}`)
  } catch (e) { alert(e.response?.data?.error || '保存失败') }
  finally { tzSaving.value = false }
}
async function addDept() {
  if (!newDept.value) return
  try { await api.post('/departments', { name: newDept.value, parent_id: newDeptParent.value }); newDept.value = ''; newDeptParent.value = 0; await loadDepts() } catch (e) { alert(e.response?.data?.error || '添加失败') }
}
async function delDept(d) { if (!confirm(`删除部门「${d.name}」？`)) return; try { await api.del(`/departments/${d.id}`); await loadDepts() } catch (e) { alert(e.response?.data?.error || '删除失败') } }
async function addUser() {
  if (!u.name || !u.password) { alert('姓名、密码均必填'); return }
  if (u.role === 'super_admin') { if (!u.username) { alert('超级管理员需填写登录账号'); return } }
  else if (!u.emp_no) { alert('工号必填（登录账号 = 工号）'); return }
  if (u.password.length < 6) { alert('密码至少 6 位'); return }
  savingU.value = true
  try { await api.post('/users', { ...u }); Object.assign(u, { name: '', emp_no: '', username: '', password: '', mobile: '', role: 'executor', in_group: false }); showAddUser.value = false; await loadUsers() } catch (e) { alert(e.response?.data?.error || '添加失败') }
  finally { savingU.value = false }
}
async function delUser(p) { if (!confirm(`删除人员「${p.name}」？`)) return; try { await api.del(`/users/${p.id}`); await loadUsers() } catch (e) { alert(e.response?.data?.error || '删除失败') } }

// 编辑 / 冻结 / 重置密码
const editUser = ref(null)
const editForm = reactive({ name: '', emp_no: '', mobile: '', role: 'executor', dept_id: 0, in_group: false })
const editSaving = ref(false)
function openEdit(p) {
  editUser.value = p
  editForm.name = p.name
  editForm.emp_no = p.emp_no || ''
  editForm.mobile = p.mobile || ''
  editForm.role = p.role
  editForm.dept_id = p.dept_id
  editForm.in_group = !!p.in_group
}
async function saveEdit() {
  if (!editForm.name) { alert('姓名不能为空'); return }
  editSaving.value = true
  try {
    await api.put(`/users/${editUser.value.id}`, { name: editForm.name, emp_no: editForm.emp_no, mobile: editForm.mobile, role: editForm.role, dept_id: editForm.dept_id, in_group: editForm.in_group })
    editUser.value = null
    await loadUsers()
  } catch (e) { alert(e.response?.data?.error || '保存失败') } finally { editSaving.value = false }
}
async function toggleFreeze(p) {
  const action = p.frozen ? '解冻' : '冻结'
  if (!confirm(`确认${action}人员「${p.name}」？冻结后其所有登录令牌立即失效，无法登录。`)) return
  try { await api.put(`/users/${p.id}`, { frozen: !p.frozen }); await loadUsers() } catch (e) { alert(e.response?.data?.error || '操作失败') }
}
async function resetPwd(p) {
  const np = prompt(`为「${p.name}」设置新密码：`)
  if (!np) return
  if (np.length < 6) { alert('密码至少 6 位'); return }
  try { await api.post(`/users/${p.id}/reset-password`, { password: np }); alert('密码已重置') } catch (e) { alert(e.response?.data?.error || '重置失败') }
}
async function unlockUser(p) {
  if (!confirm(`解除「${p.name}」的登录锁定？该账号所有设备上的 15 分钟锁定将立即清除。`)) return
  try {
    const r = await api.post('/auth/unlock', { username: p.username })
    alert(r.cleared > 0 ? `已解除 ${p.name} 的 ${r.cleared} 个锁定，现在可以登录了` : '该账号当前未被锁定')
  } catch (e) { alert(e.response?.data?.error || '操作失败') }
}

// 在线用户监控（仅超管）
const onlineMap = ref({})
const clientLabel = (t) => ({ web: '网页', pwa: 'PWA', extension: '插件' }[t] || t || '网页')
async function loadSessions() {
  try {
    const list = await api.get('/sessions')
    const m = {}
    for (const s of list) m[s.user_id] = s
    onlineMap.value = m
  } catch { onlineMap.value = {} }
}
function fmtOnline(s) {
  const min = Math.floor((s.online_sec || 0) / 60)
  const h = Math.floor(min / 60)
  const dur = h > 0 ? `${h}时${min % 60}分` : `${min}分钟`
  const ways = (s.clients || ['web']).map(clientLabel).join('+')
  return `${ways} · 在线${dur}`
}
async function forceLogout(p) {
  if (!confirm(`强制将「${p.name}」下线？其所有设备（${(p.id in onlineMap.value ? (onlineMap.value[p.id].count || 1) : '')}个会话）登录将立即失效。`)) return
  try {
    await api.post(`/users/${p.id}/force-logout`)
    delete onlineMap.value[p.id]
    alert('已强制下线')
  } catch (e) { alert(e.response?.data?.error || '操作失败') }
}
async function addHook() {
  if (!h.url) { alert('Webhook 地址不能为空'); return }
  if (!h.dept_id) { alert('请选择 Webhook 所属部门（推送只发给该部门及子部门）'); return }
  savingH.value = true
  try {
    const body = { name: h.name, url: h.url, type: h.type, secret: h.secret, dept_id: h.dept_id }
    if (editHookId.value) {
      await api.put(`/webhooks/${editHookId.value}`, body)
      editHookId.value = 0
      hookEditingName.value = ''
    } else {
      await api.post('/webhooks', body)
    }
    Object.assign(h, { name: '', url: '', dept_id: h.dept_id, type: 'wecom', secret: '' })
    await loadHooks()
  } catch (e) { alert(e.response?.data?.error || '保存失败') }
  finally { savingH.value = false }
}
async function editHook(w) {
  editHookId.value = w.id
  hookEditingName.value = w.name
  Object.assign(h, { name: w.name, url: w.url, type: w.type, secret: '', dept_id: w.dept_id })
  window.scrollTo({ top: 0, behavior: 'smooth' })
}
function cancelEditHook() {
  editHookId.value = 0
  hookEditingName.value = ''
  Object.assign(h, { name: '', url: '', dept_id: h.dept_id, type: 'wecom', secret: '' })
}
async function testHook() {
  if (!h.url) { alert('请先填写 Webhook 地址'); return }
  testing.value = true
  try {
    const r = await api.post('/webhooks/test', { url: h.url, type: h.type, secret: h.secret })
    if (r.ok) alert(r.msg || '测试消息已发送')
    else alert('测试失败：' + (r.error || '未知错误'))
  } catch (e) { alert(e.response?.data?.error || '测试失败') }
  finally { testing.value = false }
}
async function delHook(w) { if (!confirm(`删除通知渠道「${w.name}」？`)) return; try { await api.del(`/webhooks/${w.id}`); await loadHooks() } catch (e) { alert(e.response?.data?.error || '删除失败') } }

// 模板管理
const tmplForm = reactive({ id: null, type: 'task', name: '', content: '' })
const tmplSaving = ref(false)
function resetTmplForm() { Object.assign(tmplForm, { id: null, type: 'task', name: '', content: '' }) }
function editTemplate(t) {
  tmplForm.id = t.id
  tmplForm.type = t.type
  tmplForm.name = t.name
  tmplForm.content = t.content
  window.scrollTo({ top: 9999, behavior: 'smooth' })
}
async function saveTemplate() {
  if (!tmplForm.name || !tmplForm.content) { alert('模板名称与内容均不能为空'); return }
  tmplSaving.value = true
  try {
    await api.post('/templates', { id: tmplForm.id || undefined, type: tmplForm.type, name: tmplForm.name, content: tmplForm.content })
    alert(tmplForm.id ? '模板已更新' : '模板已新增')
    resetTmplForm()
    await loadTemplates()
  } catch (e) { alert(e.response?.data?.error || '保存失败') }
  finally { tmplSaving.value = false }
}
async function deleteTemplate(t) { if (!confirm(`删除模板「${t.name}」？`)) return; try { await api.del(`/templates/${t.id}`); await loadTemplates() } catch (e) { alert(e.response?.data?.error || '删除失败') } }

// 邮件 SMTP 配置
const smtp = reactive({ host: '', port: 465, user: '', pass: '', from: '', emails: '' })
const smtpSaving = ref(false)
const smtpTesting = ref(false)
async function loadSMTP() {
  try {
    const s = await api.get('/settings')
    smtp.host = s.smtp_host || ''
    smtp.port = s.smtp_port || 465
    smtp.user = s.smtp_user || ''
    smtp.from = s.smtp_from || ''
    smtp.emails = s.notify_emails || ''
  } catch {}
}
async function saveSMTP() {
  smtpSaving.value = true
  try {
    await api.post('/settings/smtp', { smtp_host: smtp.host, smtp_port: Number(smtp.port), smtp_user: smtp.user, smtp_pass: smtp.pass, smtp_from: smtp.from, notify_emails: smtp.emails })
    smtp.pass = ''
    alert('邮件配置已保存')
  } catch (e) { alert(e.response?.data?.error || '保存失败') }
  finally { smtpSaving.value = false }
}
async function testEmail() {
  smtpTesting.value = true
  try {
    const r = await api.post('/settings/test-email')
    alert(r.msg || r.error || '已发送')
  } catch (e) { alert(e.response?.data?.error || '发送失败') }
  finally { smtpTesting.value = false }
}

// 日志筛选 + 分页
const logFilter = reactive({ user_name: '', action: '', limit: 100 })
const logPage = ref(0)
async function loadLogs() {
  const params = { limit: logFilter.limit, offset: logPage.value * logFilter.limit }
  if (logFilter.user_name) params.user_name = logFilter.user_name
  if (logFilter.action) params.action = logFilter.action
  logs.value = await api.get('/logs', params)
}
function resetLogFilter() {
  Object.assign(logFilter, { user_name: '', action: '', limit: 100 })
  logPage.value = 0
  loadLogs()
}

// 审计日志保留天数（仅超管）
const logRetention = ref(90)
const logRetentionSaving = ref(false)
async function loadLogRetention() {
  try { const s = await api.get('/settings'); logRetention.value = s.log_retention_days ?? 90 } catch {}
}
async function saveLogRetention() {
  const days = Number(logRetention.value)
  if (!Number.isFinite(days) || days < 0 || days > 3650) { alert('保留天数需在 0-3650 之间'); return }
  logRetentionSaving.value = true
  try { await api.post('/settings/log-retention', { days }); alert('已保存') } catch (e) { alert(e.response?.data?.error || '保存失败') }
  finally { logRetentionSaving.value = false }
}

// 鉴权下载（fetch + blob，自动带 Bearer）
async function downloadAuth(path) {
  const token = localStorage.getItem('sw_token')
  try {
    const r = await fetch('/api/' + path, { headers: { Authorization: 'Bearer ' + token } })
    if (!r.ok) { alert('下载失败：' + r.status); return }
    const blob = await r.blob()
    const cd = r.headers.get('content-disposition') || ''
    const m = cd.match(/filename="?([^"]+)"?/)
    const fn = m ? decodeURIComponent(m[1]) : 'download.csv'
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = fn
    a.click()
    URL.revokeObjectURL(a.href)
  } catch (e) { alert('下载失败') }
}
function exportTasks() { downloadAuth('tasks/export') }
function exportLogs() {
  const params = []
  if (logFilter.user_name) params.push('user_name=' + encodeURIComponent(logFilter.user_name))
  if (logFilter.action) params.push('action=' + encodeURIComponent(logFilter.action))
  downloadAuth('logs/export' + (params.length ? '?' + params.join('&') : ''))
}

// 备份管理
const backups = ref([])
const backupLoading = ref(false)
const backupCreating = ref(false)
const backupCfg = reactive({ frequency: 'none', retention: 10, remote_dir: '' })
const backupCfgSaving = ref(false)
async function loadBackups() { backupLoading.value = true; try { const r = await api.get('/backups'); backups.value = Array.isArray(r) ? r : [] } catch { backups.value = [] } finally { backupLoading.value = false } }
async function loadBackupCfg() { try { Object.assign(backupCfg, await api.get('/backup-config')) } catch {} }
async function createBackup() { backupCreating.value = true; try { await api.post('/backups'); await loadBackups() } catch (e) { alert(e.response?.data?.error || '备份失败') } finally { backupCreating.value = false } }
async function setBackupFreq(f) { backupCfg.frequency = f; await saveBackupCfg() }
async function saveBackupCfg() { backupCfgSaving.value = true; try { await api.post('/backup-config', { frequency: backupCfg.frequency, retention: Number(backupCfg.retention), remote_dir: backupCfg.remote_dir }); alert('已保存') } catch (e) { alert(e.response?.data?.error || '保存失败') } finally { backupCfgSaving.value = false } }
function downloadBackup(b) { downloadAuth('backups/' + b.id + '/download') }
async function restoreBackup(b) { if (!confirm(`确认还原备份「${b.name}」？当前数据将被覆盖，且操作不可撤销！`)) return; try { await api.post('/backups/' + b.id + '/restore'); alert('已还原，页面将自动刷新'); setTimeout(() => location.reload(), 800) } catch (e) { alert(e.response?.data?.error || '还原失败') } }
async function deleteBackup(b) { if (!confirm(`删除备份「${b.name}」？`)) return; try { await api.del('/backups/' + b.id); await loadBackups() } catch (e) { alert(e.response?.data?.error || '删除失败') } }

// 退出登录
function onLogout() { auth.logout(); router.replace('/login') }

// 切换 tab 时按需加载
watch(tab, (v) => {
  if (v === 'backup' && auth.isSuper) { loadBackups(); loadBackupCfg() }
  if (v === 'log' && auth.canManage) { logPage.value = 0; loadLogs(); if (auth.isSuper) loadLogRetention() }
  if (v === 'tmpl' && auth.canManage) loadTemplates()
  if (v === 'hook' && auth.canManage) { loadHooks(); if (auth.isSuper) loadSMTP() }
  if (v === 'user' && auth.canManage) { loadUsers(); if (auth.isSuper) loadSessions() }
  if (v === 'dept' && auth.canManage) { loadDepts(); loadShiftConfigs() }
})

// ---- 人员「操作」下拉菜单 ----
const opsOpen = ref(0)
function toggleOps(id) { opsOpen.value = opsOpen.value === id ? 0 : id }
async function runOp(p, act) {
  opsOpen.value = 0
  if (act === 'edit') return openEdit(p)
  if (act === 'freeze') return toggleFreeze(p)
  if (act === 'pwd') return resetPwd(p)
  if (act === 'unlock') return unlockUser(p)
  if (act === 'logout') return forceLogout(p)
  if (act === 'del') return delUser(p)
}

// ---- 人员批量导入 ----
const userImporting = ref(false)
async function importUsers(e) {
  const input = e.target
  const file = input.files && input.files[0]
  input.value = ''
  if (!file) return
  if (!confirm(`将按模板导入人员（${file.name}）。\n登录账号已存在则更新资料，不存在则新建，确认继续？`)) return
  userImporting.value = true
  try {
    const r = await api.upload('/users/import', file)
    let msg = `导入完成：新建 ${r.created || 0} 人，更新 ${r.updated || 0} 人，失败 ${r.failed || 0} 条`
    if (r.errors && r.errors.length) msg += '\n' + r.errors.slice(0, 8).join('\n')
    alert(msg)
    await loadUsers()
  } catch (err) { alert(err.response?.data?.error || '导入失败') }
  finally { userImporting.value = false }
}

onMounted(async () => {
  // 点击页面空白处收起人员操作菜单
  document.addEventListener('click', () => { opsOpen.value = 0 })
  await loadBrand()
  if (auth.canManage) {
    await Promise.all([loadDepts(), loadUsers(), loadHooks(), loadLogs()])
    // 人员创建默认选第一个部门；Webhook 部门不设默认（必选，避免绑错部门收不到推送）
    if (departments.value.length) { if (!u.dept_id) u.dept_id = departments.value[0].id }
    if (auth.isSuper) loadSMTP()
    if (auth.isSuper) loadLogRetention()
    if (auth.isSuper) loadSessions()
  }
})
</script>

<style scoped>
.tabs { display: flex; gap: 6px; margin-bottom: 16px; flex-wrap: wrap; }
.tab { border: 1px solid var(--glass-border); background: var(--glass); color: var(--text-dim); padding: 9px 16px; border-radius: 12px; cursor: pointer; font-size: 13px; }
.tab.active { background: var(--glass-strong); color: var(--text); border-color: var(--glass-border-strong); }

/* Webhook 部门必选：未选红框提示 */
select.req-miss { border-color: var(--danger, #e11d48); box-shadow: 0 0 0 2px rgba(225, 29, 72, 0.12); }
.form-col { display: flex; flex-direction: column; gap: 14px; margin-bottom: 16px; max-width: 520px; }
.info-row { display: flex; align-items: center; gap: 14px; }
.info-row .fld { width: 80px; flex: none; margin: 0; }
.info-val { font-size: 14px; color: var(--text); }
.user-toolbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 12px; }
.user-toolbar .btn { padding: 8px 14px; font-size: 13px; }
.imp-label { display: inline-flex; align-items: center; cursor: pointer; }
.mob { color: var(--accent); }

.ops { position: relative; }
.ops-btn.on { color: var(--accent); border-color: var(--accent); background: var(--accent-soft); }
.ops-drop {
  position: absolute; right: 0; top: 30px; z-index: 40; min-width: 148px;
  background: var(--glass-strong); border: 1px solid var(--glass-border-strong);
  border-radius: 12px; padding: 6px; box-shadow: 0 12px 32px rgba(0,0,0,0.28);
  display: flex; flex-direction: column; gap: 2px;
}
.op {
  text-align: left; padding: 9px 11px; border: none; background: transparent; color: var(--text);
  border-radius: 9px; cursor: pointer; font-size: 13px; white-space: nowrap; display: flex;
  align-items: center; justify-content: space-between; gap: 8px;
}
.op:hover:not(:disabled) { background: var(--accent-soft); color: var(--accent); }
.op:disabled { opacity: 0.4; cursor: not-allowed; }
.op.danger { color: var(--danger); }
.op.danger:hover { background: rgba(225,29,72,0.12); color: var(--danger); }
.op-hint { font-size: 11px; color: var(--text-faint); }

.logo-edit { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; }
.logo-preview {
  width: 64px; height: 64px; flex: none; border-radius: 16px; overflow: hidden;
  display: grid; place-items: center; background: var(--brand-grad);
  box-shadow: 0 8px 22px rgba(79, 70, 229, 0.30);
  color: #fff; font-size: 12px;
}
.logo-preview img { width: 100%; height: 100%; object-fit: contain; display: block; }
.logo-ops { flex: 1; min-width: 220px; }
.logo-btns { display: flex; gap: 8px; flex-wrap: wrap; }
.logo-btns .btn { padding: 8px 14px; font-size: 13px; }
.logo-edit .hint { color: var(--text-dim); font-size: 12px; margin: 8px 0 0; line-height: 1.6; }
@media (max-width: 560px) {
  .logo-edit { gap: 12px; }
  .logo-btns .btn { min-height: 44px; }
  .user-toolbar .btn { min-height: 44px; }
  .ops-drop { right: 0; left: auto; }
  .op { min-height: 44px; }
}
.log-filter { display: flex; gap: 8px; margin-bottom: 12px; flex-wrap: wrap; }
.log-filter .glass-input { max-width: 220px; padding: 8px 12px; font-size: 13px; }
.inline-add { display: flex; gap: 10px; margin-bottom: 16px; max-width: 520px; }
.inline-add .glass-input { flex: 1; }
.dept-add .glass-input { flex: 0 1 auto; min-width: 0; }
.fg2 { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; margin-bottom: 14px; }
.add-form { margin-bottom: 18px; padding-bottom: 18px; border-bottom: 1px solid var(--glass-border); }
.add-user-fold .btn { min-height: 44px; padding: 10px 16px; }
.foldable { cursor: pointer; user-select: none; display: flex; align-items: center; gap: 6px; border-radius: 10px; transition: background .15s; }
.foldable:hover { background: var(--overlay); }
.foldable .caret { width: 16px; height: 16px; flex: none; transition: transform .18s; color: var(--text-dim); }
.foldable.open .caret { transform: rotate(90deg); }
.form-actions { display: flex; justify-content: flex-end; align-items: flex-end; gap: 10px; }
.list { display: flex; flex-direction: column; gap: 8px; }
.row { display: flex; align-items: center; justify-content: space-between; padding: 12px 14px; border-radius: 13px; background: var(--overlay); border: 1px solid var(--glass-border); }
.row-main { display: flex; align-items: center; gap: 11px; }
.avatar.sm { width: 34px; height: 34px; border-radius: 10px; font-size: 13px; flex: none; display: grid; place-items: center; background: var(--brand-grad); color: #fff; font-weight: 700; }
.rn { font-size: 14px; font-weight: 600; display: flex; align-items: center; }
.ru { font-size: 12px; color: var(--text-faint); margin-top: 3px; }
.mono { font-family: ui-monospace, monospace; font-size: 12px; color: var(--text-dim); word-break: break-all; }
.del { padding: 6px 13px; border-radius: 10px; border: 1px solid var(--glass-border); background: transparent; color: var(--text-faint); cursor: pointer; font-size: 12.5px; }
.del:hover { color: var(--danger); border-color: rgba(225,29,72,0.4); }
.log-list { display: flex; flex-direction: column; gap: 2px; }
.log { display: flex; gap: 12px; padding: 10px 4px; font-size: 13px; border-bottom: 1px solid var(--hairline); }
.log-time { color: var(--text-faint); font-family: ui-monospace, monospace; font-size: 12px; white-space: nowrap; }
.log-user { color: var(--accent); white-space: nowrap; }
.log-ip { color: var(--text-faint); font-family: ui-monospace, monospace; font-size: 12px; white-space: nowrap; }
.log-action { color: var(--text-dim); }
.retention-bar { display: flex; align-items: center; gap: 10px; margin-bottom: 12px; flex-wrap: wrap; }
.retention-hint { font-size: 12px; color: var(--text-faint); }
.pager { display: flex; align-items: center; gap: 14px; justify-content: center; margin-top: 14px; }
.pager-info { font-size: 13px; color: var(--text-dim); }
.backup-list { display: flex; flex-direction: column; gap: 8px; }
.row.frozen { opacity: 0.6; }
.row.frozen .avatar.frozen-av { filter: grayscale(1); }
.row-actions { display: flex; gap: 6px; align-items: center; }
.chip.online { background: rgba(22,163,74,0.14); color: var(--success, #16a34a); border: 1px solid rgba(22,163,74,0.35); }
.mini.danger-txt { color: var(--danger, #e11d48); }
.mini.danger-txt:hover { border-color: var(--danger, #e11d48); color: var(--danger, #e11d48); }
.mini { font-size: 12px; padding: 4px 10px; border-radius: 7px; border: 1px solid var(--glass-border); background: transparent; color: var(--text-dim); cursor: pointer; }
.mini:hover { color: var(--text); border-color: var(--accent); }
.modal-mask { position: fixed; inset: 0; background: var(--mask); display: grid; place-items: center; z-index: 50; backdrop-filter: blur(3px); }
.modal { width: min(380px, 92vw); background: var(--glass-strong); border: 1px solid var(--glass-border); border-radius: 16px; overflow: hidden; }
.modal-head { display: flex; align-items: center; justify-content: space-between; padding: 14px 18px; border-bottom: 1px solid var(--glass-border); font-weight: 600; font-size: 14px; }
.modal-head .x { width: 28px; height: 28px; border-radius: 8px; border: none; background: var(--overlay-2); color: var(--text-dim); cursor: pointer; font-size: 16px; }
.modal-body { padding: 14px 18px; }
.backup-row { display: flex; align-items: center; justify-content: space-between; padding: 10px 14px; border-radius: 11px; background: var(--overlay); border: 1px solid var(--glass-border); }
.b-name { font-size: 13.5px; font-weight: 600; }
.b-meta { font-size: 11.5px; color: var(--text-faint); margin-top: 2px; }
.b-remote { color: var(--success, #16a34a); }
.b-actions { display: flex; gap: 6px; align-items: center; }
.b-actions .btn { padding: 6px 12px; font-size: 12.5px; }
.b-actions .del { width: 24px; height: 24px; }
.btn.danger { background: var(--danger); border-color: var(--danger); color: #fff; }
.btn.danger:hover { opacity: 0.9; }
.btn.ghost.active { border-color: var(--accent); color: var(--accent); }
.btn.ghost.sm { padding: 4px 10px; font-size: 12px; margin: 0; }
.import-ta { width: 100%; resize: vertical; font-family: ui-monospace, monospace; font-size: 12.5px; line-height: 1.7; }
.dept-row { align-items: flex-start; flex-direction: column; gap: 10px; }
.dept-head { width: 100%; justify-content: space-between; }
.shift-cfg { width: 100%; display: flex; flex-direction: column; gap: 8px; }
.shift-cfg-list { display: flex; gap: 6px; flex-wrap: wrap; }
.shift-chip { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; padding: 4px 10px; border-radius: 999px; background: var(--overlay-2); border: 1px solid var(--glass-border); color: var(--text-dim); }
.shift-chip .mini { padding: 0 5px; font-size: 13px; line-height: 1; }
.shift-none { font-size: 12px; color: var(--text-faint); }
.shift-add { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.shift-add .glass-input.sm { max-width: 120px; padding: 7px 10px; font-size: 13px; }
.shift-sep { color: var(--text-faint); font-size: 12px; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 820px) { .fg2 { grid-template-columns: 1fr; } }
</style>
