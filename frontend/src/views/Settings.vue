<template>
  <div class="settings">
    <div class="tabs">
      <button class="tab" :class="{ active: tab === 'me' }" @click="setTab('me')">个人信息</button>
      <button v-if="auth.isSuper" class="tab" :class="{ active: tab === 'brand' }" @click="setTab('brand')">企业信息</button>
      <button v-if="auth.canManage" class="tab" :class="{ active: tab === 'tmpl' }" @click="setTab('tmpl')">模板</button>
      <button v-if="auth.canManage" class="tab" :class="{ active: tab === 'hook' }" @click="setTab('hook')">通知</button>
      <button v-if="auth.canManage" class="tab" :class="{ active: tab === 'log' }" @click="setTab('log')">操作审计</button>
      <button v-if="auth.canManage" class="tab" :class="{ active: tab === 'syslog' }" @click="setTab('syslog')">运行日志</button>
      <button v-if="auth.isSuper" class="tab" :class="{ active: tab === 'backup' }" @click="setTab('backup')">备份</button>

      <!-- v0.39.1 系统管理：原先挂在侧栏的四项并入设置，按权限过滤显示。
           v0.40.0：曾把「系统管理」做成一个可打开的入口页（进去是 4 张卡片）。
           v0.40.2：入口页与顶栏这 4 个直达标签重复（用户指出）—— 既然 4 个模块已在
           顶栏原地直达，入口页就是多余的中转，整块移除（含 ?tab=syshome）。
           竖线保留：仍用来把"系统设置项"与"系统管理模块"分开。 -->
      <template v-if="sysTabs.length">
        <span class="tab-gap" aria-hidden="true"></span>
        <button
          v-for="t in sysTabs"
          :key="t.key"
          class="tab"
          :class="{ active: tab === t.key }"
          @click="setTab(t.key)"
        >{{ t.label }}</button>
      </template>
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
        <div class="info-row"><label class="fld">最近登录</label><span class="info-val">{{ auth.user?.last_login_at ? fmt(auth.user.last_login_at) : '—' }}</span></div>
      </div>
      <h3 class="section-title" style="margin-top:20px;">修改密码</h3>
      <div class="form-col" style="max-width:380px;">
        <div><label class="fld">当前密码</label><input v-model="pw.old_password" type="password" class="glass-input" autocomplete="current-password" /></div>
        <div><label class="fld">新密码</label><input v-model="pw.new_password" type="password" class="glass-input" autocomplete="new-password" placeholder="至少 8 位，含字母和数字" /></div>
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
              <p class="hint">支持 PNG / JPG / WEBP / SVG / ICO，≤2MB。位图（PNG/JPG/ICO）上传后会自动抠掉纯色背景变透明并统一转 PNG，侧边栏与登录页立即生效；点「移除」恢复系统默认图标。<br><b>建议上传 ≥512×512 的清晰原图</b>，否则侧边栏与登录卡放大后会模糊（系统不会强行放大原图，避免失真）。</p>
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
        <div>
          <label class="fld">逾期宽限期（分钟）</label>
          <div style="display:flex; gap:8px; align-items:center; flex-wrap:wrap;">
            <input v-model.number="overdueGrace" type="number" min="0" max="1440" class="glass-input" style="max-width:130px;" />
            <button class="btn primary" :disabled="graceSaving" @click="saveOverdueGrace">{{ graceSaving ? '保存中…' : '保存宽限期' }}</button>
            <span class="section-sub">默认 30；任务的「开始时间」+ 宽限期之后才记为逾期（0 = 到点即逾期）</span>
          </div>
          <p class="hint" style="color:var(--text-dim); font-size:12px; margin:6px 0 0; line-height:1.6;">每日/月度定时任务以各自开始时间为基准，超过该时间再加此宽限才算逾期；到点推送也会相应延后。修改后即时生效，无需重启。</p>
        </div>
      </div>
      <div class="form-actions" style="justify-content:flex-start; gap:10px;">
        <button class="btn primary" :disabled="saving" @click="saveBrand">{{ saving ? '保存中…' : '保存设置' }}</button>
        <button class="btn ghost" :disabled="tzSaving" @click="saveTimezone">{{ tzSaving ? '应用中…' : '保存时区' }}</button>
      </div>
    </section>

    <!-- 模板管理（管理员可查看下载，超管可修改） -->
    <section v-if="tab === 'tmpl' && auth.canManage" class="panel">
      <h3 class="section-title">固定模板 <span class="section-sub">下载后按格式填好，在班表/任务页上传即可导入（统一 Excel .xlsx 格式）</span></h3>
      <div class="form-actions" style="justify-content:flex-start; gap:10px; flex-wrap:wrap;">
        <button class="btn primary" @click="downloadAuth('templates/schedule-template')">⬇ 班表模板（Excel 矩阵）</button>
        <button class="btn primary" @click="downloadAuth('templates/task-template')">⬇ 任务模板（每日+月度）</button>
        <button class="btn primary" @click="downloadAuth('templates/user-template')">⬇ 人员导入模板</button>
        <button v-if="auth.can('task:export')" class="btn ghost" @click="exportTasks">⬇ 导出当前任务</button>
      </div>
      <p class="hint" style="font-size:12.5px;color:var(--text-dim);margin-top:10px;line-height:1.7;">
        班表模板：人×日期矩阵，每人一行，第 1~31 列填 早/中/晚/夜/休；右上角「信息」表填部门与年月。<br>
        任务模板：Sheet「每日工作内容」（时间/时段工作/负责班次）→ 每日任务；Sheet「月度工作内容」（N号/业务主题/工作内容）→ 每月任务。
      </p>

      <div class="tmpl-head" style="display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-top:20px;">
        <!-- v0.40.12：新增/修改从 isSuper 放宽为权限点 system:template:add ——
             后端 RBAC 一直允许部门管理员（AllowPrefixes 含 system:template:），前端却用 isSuper 挡住入口，
             属前后端不一致；模板归属按部门隔离（超管为全局模板）。 -->
        <h3 class="section-title" style="margin:0;">自定义模板 <span class="section-sub" v-if="!auth.can('system:template:add')">（仅查看 / 下载，新增与修改需模板权限）</span></h3>
        <button v-if="auth.can('system:template:add')" class="btn primary sm" @click="toggleTmplForm">
          {{ tmplOpen ? '收起新增' : '＋ 新增模板' }}
        </button>
      </div>

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
            <button v-if="auth.can('system:template:add')" class="mini" @click="editTemplate(t)">修改</button>
            <button v-if="auth.can('system:template:remove')" class="del" @click="deleteTemplate(t)">×</button>
          </div>
        </div>
        <div v-if="!templates.length" class="empty">
          {{ auth.can('system:template:add') ? '暂无自定义模板，点击上方「＋ 新增模板」创建' : '暂无自定义模板（需模板权限才可新增，你可下载固定模板使用）' }}
        </div>
      </div>

      <div v-if="auth.can('system:template:add') && tmplOpen" class="add-form" style="margin-top:16px;">
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
      <div class="hook-head" style="display:flex;align-items:center;gap:10px;flex-wrap:wrap;">
        <h3 class="section-title" style="margin:0;">渠道通知 <span class="section-sub">地址与密钥均加密存储</span></h3>
        <button v-if="auth.canManage" class="btn primary sm" @click="toggleHookForm">
          {{ hookOpen ? '收起表单' : '＋ 新增渠道' }}
        </button>
      </div>
      <div class="daily-summary-toggle">
        <label style="display:flex;align-items:center;gap:10px;cursor:pointer;font-size:13px;">
          <input type="checkbox" v-model="dailySummary" @change="saveDailySummary" :disabled="!auth.isSuper || dsSaving" style="width:16px;height:16px" />
          每日任务汇总推送（每天 09:00 自动推送给所有通知渠道；关闭后不再自动发送，不影响其他到点提醒）
        </label>
        <span class="section-sub" v-if="!auth.isSuper" style="margin-left:8px">仅超管可开关</span>
        <span v-if="dsSaving" class="section-sub" style="margin-left:8px">保存中…</span>
      </div>
      <div v-if="auth.canManage && hookOpen" class="add-form" style="margin-top:6px;">
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
      <p v-if="editHookId" class="hint">正在编辑「{{ hookEditingName }}」，改完点「保存修改」；密钥留空表示不修改</p>
      <div v-if="hooks.length" class="hook-cards">
        <div v-for="w in hooks" :key="w.id" class="hook-card">
          <div class="hook-card-top">
            <div class="hook-ico" :class="'t-' + w.type">{{ typeShort(w.type) }}</div>
            <div class="hook-title">
              <div class="rn">{{ w.name }} <span class="chip" style="margin-left:6px">{{ typeLabel(w.type) }}</span></div>
              <div class="ru" style="font-size:12px;color:var(--text-dim)">推送范围：{{ deptNameOf(w.dept_id) }}</div>
            </div>
          </div>
          <div class="ru mono hook-url" :title="w.url">{{ w.url }}</div>
          <div class="hook-card-actions">
            <button v-if="auth.canManage" class="btn ghost sm" @click="editHook(w)">编辑</button>
            <button v-if="auth.canManage" class="btn danger sm" @click="delHook(w)">删除</button>
          </div>
        </div>
      </div>
      <div v-else class="empty">暂无通知渠道，点击上方「＋ 新增渠道」添加</div>

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
      <h3 class="section-title">操作审计日志 <button v-if="auth.can('system:log:export')" class="btn ghost sm" style="margin-left:auto" @click="exportLogs">⬇ 导出 CSV</button></h3>
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
      <div class="log-table">
        <div class="log-head log-row">
          <span>时间</span><span>操作人</span><span>来源</span><span>IP</span><span>操作内容</span>
        </div>
        <div class="log-list">
          <div v-for="l in logs" :key="l.id" class="log log-row">
            <span class="log-time">{{ fmt(l.created_at) }}</span>
            <span class="log-user" :title="l.user_name || ''">{{ l.user_name || '—' }}</span>
            <span class="log-src">
              <em :class="clientName(l.client) ? 'src-badge' : 'src-none'">{{ clientName(l.client) || '—' }}</em>
            </span>
            <span class="log-ip">{{ l.ip || '—' }}</span>
            <span class="log-action" :title="l.action">{{ l.action }}</span>
          </div>
          <div v-if="!logs.length" class="empty">暂无日志</div>
        </div>
      </div>
      <div class="pager" v-if="logs.length">
        <button class="btn ghost" :disabled="logPage === 0" @click="logPage > 0 && (logPage--, loadLogs())">上一页</button>
        <span class="pager-info">第 {{ logPage + 1 }} 页</span>
        <button class="btn ghost" :disabled="logs.length < logFilter.limit" @click="logPage++; loadLogs()">下一页</button>
      </div>
    </section>

    <!-- 系统运行日志（仅管理员） -->
    <section v-if="tab === 'syslog' && auth.canManage" class="panel">
      <h3 class="section-title">系统运行日志 <span class="section-sub">服务端运行期事件 / panic / 5xx / 调度失败，用于排查崩溃</span>
        <button v-if="auth.can('system:log:export')" class="btn ghost sm" style="margin-left:auto" @click="exportSysLogs">⬇ 导出 Excel</button>
      </h3>
      <div class="log-filter">
        <select v-model="sysFilter.level" class="glass-input" @change="sysPage = 0; loadSysLogs()">
          <option value="">全部级别</option>
          <option value="INFO">INFO</option>
          <option value="WARN">WARN</option>
          <option value="ERROR">ERROR</option>
          <option value="FATAL">FATAL</option>
        </select>
        <input v-model="sysFilter.source" class="glass-input" placeholder="按来源筛选(如 server/notify/backup)" @keyup.enter="sysPage = 0; loadSysLogs()" />
        <input v-model="sysFilter.q" class="glass-input" placeholder="按信息/详情关键词筛选" @keyup.enter="sysPage = 0; loadSysLogs()" />
        <input v-model="sysFilter.from" type="date" class="glass-input" @change="sysPage = 0; loadSysLogs()" />
        <span class="shift-sep">至</span>
        <input v-model="sysFilter.to" type="date" class="glass-input" @change="sysPage = 0; loadSysLogs()" />
        <select v-model.number="sysFilter.limit" class="glass-input" @change="sysPage = 0; loadSysLogs()">
          <option :value="50">50 条/页</option>
          <option :value="100">100 条/页</option>
          <option :value="200">200 条/页</option>
        </select>
        <button class="btn ghost" @click="sysPage = 0; loadSysLogs()">筛选</button>
        <button class="btn ghost" @click="resetSysFilter">重置</button>
      </div>
      <div class="log-table sys">
        <div class="log-head log-row">
          <span>时间</span><span>级别</span><span>来源</span><span>信息</span>
        </div>
        <div class="log-list">
          <div v-for="l in sysLogs" :key="l.id" class="log log-row">
            <span class="log-time">{{ fmt(l.created_at) }}</span>
            <span class="log-level" :style="sysLevelStyle(l.level)">{{ l.level }}</span>
            <span class="log-src"><em class="src-none">{{ l.source || '—' }}</em></span>
            <span class="log-action" :title="l.detail || l.message">
              <span class="la-msg">{{ l.message }}</span>
              <!-- v0.40.0：把 detail 解析成"谁 / 哪条接口 / 缺什么权限"。
                   只显示一句「权限不足」时，管理员会误以为是超管被拦（实测踩过）。 -->
              <span v-if="sysDetail(l)" class="la-detail">{{ sysDetail(l) }}</span>
            </span>
          </div>
          <div v-if="!sysLogs.length" class="empty">暂无运行日志（系统正常运行时不会自动写入，仅在发生 panic / 5xx / 调度异常时记录）</div>
        </div>
      </div>
      <div class="pager" v-if="sysLogs.length">
        <button class="btn ghost" :disabled="sysPage === 0" @click="sysPage > 0 && (sysPage--, loadSysLogs())">上一页</button>
        <span class="pager-info">第 {{ sysPage + 1 }} 页</span>
        <button class="btn ghost" :disabled="sysLogs.length < sysFilter.limit" @click="sysPage++; loadSysLogs()">下一页</button>
      </div>
    </section>

    <!-- 备份还原（仅超管） -->
    <section v-if="tab === 'backup' && auth.isSuper" class="panel">
      <h3 class="section-title">系统备份与还原 <span class="section-sub">仅超级管理员</span></h3>
      <p class="hint" style="color:var(--text-dim); font-size:13px; margin:0 0 14px; line-height:1.6;">手动备份可生成当前数据库的完整快照；还原操作危险，会立即覆盖当前数据并自动重启连接。</p>
      <div class="form-col">
        <div>
          <label class="fld">立即备份 <span style="font-weight:400;color:var(--text-faint)">（手动备份为全量快照，均含知识库附件；「全部」自动包含知识库及后续新增功能，范围仅作分类标记）</span></label>
          <div style="display:flex; gap:8px; flex-wrap:wrap; align-items:center;">
            <button class="btn ghost" :class="{ active: backupScope === 'all' }" @click="backupScope = 'all'">全部</button>
            <button class="btn ghost" :class="{ active: backupScope === 'schedule' }" @click="backupScope = 'schedule'">班表</button>
            <button class="btn ghost" :class="{ active: backupScope === 'task' }" @click="backupScope = 'task'">任务</button>
            <button class="btn ghost" :class="{ active: backupScope === 'user' }" @click="backupScope = 'user'">人员</button>
            <button class="btn ghost" :class="{ active: backupScope === 'knowledge' }" @click="backupScope = 'knowledge'">知识库</button>
            <button class="btn primary" :disabled="backupCreating" @click="createBackup">{{ backupCreating ? '备份中…' : '创建备份' }}</button>
            <label class="btn ghost imp" style="cursor:pointer;">
              ⬆ 导入备份还原
              <input type="file" accept=".db" :disabled="backupImporting" @change="importBackup" hidden />
            </label>
          </div>
          <p class="hint" style="color:var(--text-dim); font-size:12px; margin:8px 0 0; line-height:1.6;">「导入备份还原」用于把从本系统「下载」的 .db 备份文件（或另一台服务器导出的备份）恢复进来，会覆盖当前全部数据，请谨慎操作。</p>
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
            <div class="b-meta"><span class="scope-chip" :class="'s-' + (b.scope || 'all')">{{ scopeName(b.scope) }}</span> {{ b.size_human }} · {{ b.created_at }}<span v-if="b.remote" class="b-remote"> · 已同步异地</span></div>
            <div v-if="verifyMsgs[b.id]" class="b-meta" style="color:var(--accent); margin-top:4px;">{{ verifyMsgs[b.id] }}</div>
          </div>
          <div class="b-actions">
            <button class="btn ghost" @click="downloadBackup(b)">下载</button>
            <button class="btn ghost" @click="restoreBackup(b)">还原</button>
            <button class="btn ghost" :disabled="verifying[b.id]" @click="verifyBackup(b)">校验</button>
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

    <!-- ==================== v0.39.1 系统管理（原侧栏四项并入设置） ====================
         组件自带 .adm-page 版式（页面级卡片），这里不再套 .panel，避免出现"卡中卡"。
         每块都带权限判断：无权时整块不渲染，配合 onMounted 的兜底只会停在「个人信息」。 -->
    <!-- 系统管理四个子模块（懒加载）
         组件自带 .adm-page 版式（页面级卡片），这里不再套 .panel，避免出现"卡中卡"。
         每块都带权限判断：无权时整块不渲染，配合 onMounted 的兜底只会停在「个人信息」。 -->
    <section v-if="tab === 'sysuser' && auth.can('system:user:list')" class="sys-embed">
      <SystemUserPage />
    </section>
    <section v-if="tab === 'sysrole' && auth.can('system:role:list')" class="sys-embed">
      <SystemRolePage />
    </section>
    <section v-if="tab === 'sysmenu' && auth.can('system:menu:list')" class="sys-embed">
      <SystemMenuPage />
    </section>
    <section v-if="tab === 'sysdept' && auth.can('system:dept:list')" class="sys-embed">
      <SystemDeptPage />
    </section>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, watch, defineAsyncComponent } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import * as api from '@/api'
import { useAuthStore } from '@/store/auth'
import { brand, loadBrand } from '@/brand'
import { deptOptions, indentOf } from '@/utils/dept'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

// ---------------------------------------------------------------------------
// v0.39.1 系统管理：原先挂在侧栏的四项（用户/角色/菜单/部门）并入设置页
//
// 为什么这样收口：这四项与设置页原有的「人员 / 部门与班次」操作的是**同一张
// users / departments 表**，但走的是两套接口（/system/user/list 有数据范围过滤，
// 旧 /users 是登录即可的通讯录接口）。两个入口并存 = 部门管理员在旧页面能看全
// 公司人员。因此统一为：侧栏只留「设置」，四个模块在设置页内作为一级 tab。
//
// 组件用 defineAsyncComponent 懒加载：不进 Settings 主 chunk，切到该 tab 才拉取。
// ---------------------------------------------------------------------------
const SystemUserPage = defineAsyncComponent(() => import('@/views/SystemUser.vue'))
const SystemRolePage = defineAsyncComponent(() => import('@/views/SystemRole.vue'))
const SystemMenuPage = defineAsyncComponent(() => import('@/views/SystemMenu.vue'))
const SystemDeptPage = defineAsyncComponent(() => import('@/views/SystemDept.vue'))

// perm 与后端 system/routeperm.go 的 perms 逐字符一致；无权则该项不出现
const SYS_TABS = [
  { key: 'sysuser', label: '用户管理', perm: 'system:user:list' },
  { key: 'sysrole', label: '角色管理', perm: 'system:role:list' },
  { key: 'sysmenu', label: '菜单管理', perm: 'system:menu:list' },
  { key: 'sysdept', label: '部门管理', perm: 'system:dept:list' }
]
const sysTabs = computed(() => SYS_TABS.filter((t) => auth.can(t.perm)))

// v0.39.1：'dept' / 'user' 两个老 tab 已合并进系统管理四个模块，
// 因此从 TAB_KEYS 里摘掉 —— ?tab=dept / ?tab=user 也会落到「个人信息」，
// 不会再把用户带回已被取代的旧界面。
// v0.40.2：'syshome' 入口页已移除，旧链接 ?tab=syshome 同样落到「个人信息」。
const TAB_KEYS = ['me', 'brand', 'tmpl', 'hook', 'log', 'syslog', 'backup', 'logout']
  .concat(SYS_TABS.map((t) => t.key))
const initTab = String(route.query.tab || '')
const tab = ref(TAB_KEYS.includes(initTab) ? initTab : 'me')

// 切 tab 时把状态同步到 ?tab=xxx：刷新、分享链接、旧 /system/* 深链都能落回同一屏
function setTab(v) {
  tab.value = v
  const q = { ...route.query }
  if (v === 'me') delete q.tab
  else q.tab = v
  if (route.path === '/settings' && String(route.query.tab || '') === String(q.tab || '')) return
  router.replace({ path: '/settings', query: q })
}

// 允许从外部（如旧地址重定向）带着 tab 进来
watch(() => route.query.tab, (v) => {
  const k = String(v || 'me')
  if (TAB_KEYS.includes(k) && k !== tab.value) tab.value = k
})

const typeLabel = (t) => ({ wecom: '企业微信', dingtalk: '钉钉', feishu: '飞书' }[t] || '企业微信')

const saving = ref(false)
const departments = ref([])
const hooks = ref([])
const logs = ref([])
const templates = ref([])
const smtpOpen = ref(false)    // SMTP 配置：默认收起，点标题展开
const h = reactive({ name: '', url: '', dept_id: null, type: 'wecom', secret: '' })
const savingH = ref(false)
const testing = ref(false)
const editHookId = ref(0)        // 正在编辑的 Webhook id；0 表示新增
const hookEditingName = ref('')  // 编辑提示里显示的名称
const hookOpen = ref(false)      // 新增/编辑渠道表单：默认收起
function toggleHookForm() {
  if (hookOpen.value) {
    // 关闭：若在编辑态则一并取消编辑
    if (editHookId.value) cancelEditHook()
    else hookOpen.value = false
  } else {
    // 打开：确保是干净的新增态
    if (editHookId.value) cancelEditHook()
    hookOpen.value = true
  }
}
function typeShort(t) { return { wecom: '企', dingtalk: '钉', feishu: '飞' }[t] || '通' }
function deptNameOf(id) {
  const d = departments.value.find((x) => x.id === id)
  return d ? d.name : (id === 0 ? '全局' : `#${id}`)
}

// 修改个人密码
const pw = reactive({ old_password: '', new_password: '', confirm: '' })
const pwSaving = ref(false)
// 密码强度：至少 8 位且同时包含字母与数字（与后端 validPassword 一致）
function isStrongPwd(p) {
  if (!p || p.length < 8) return false
  return /[0-9]/.test(p) && /[A-Za-z]/.test(p)
}
async function changePwd() {
  if (!pw.old_password || !pw.new_password) { alert('请填写当前密码和新密码'); return }
  if (!isStrongPwd(pw.new_password)) { alert('新密码至少 8 位，且需同时包含字母和数字'); return }
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

// 操作来源显示名
function clientName(c) {
  const m = { web: '网页', pwa: 'PWA', extension: '插件' }
  return m[c] || (c && c !== 'unknown' ? c : '')
}

async function loadDepts() { departments.value = await api.get('/departments') }
async function loadHooks() { hooks.value = await api.get('/webhooks') }
// 每日任务汇总推送开关（仅超管可改）
const dailySummary = ref(true)
const dsSaving = ref(false)
async function loadDailySummary() {
  try {
    const s = await api.get('/settings')
    dailySummary.value = s.daily_summary_enabled !== false
  } catch { dailySummary.value = true }
}
async function saveDailySummary() {
  dsSaving.value = true
  try {
    await api.post('/settings/daily-summary', { enabled: dailySummary.value })
  } catch (e) { alert(e.response?.data?.error || '保存失败') }
  finally { dsSaving.value = false }
}
async function loadTemplates() { templates.value = await api.get('/templates') }

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
// 逾期宽限期（仅超管）：任务开始时间 + 宽限后才算逾期
const overdueGrace = ref(30)
const graceSaving = ref(false)
async function loadOverdueGrace() {
  try {
    const s = await api.get('/settings')
    if (typeof s.overdue_grace_minutes === 'number') overdueGrace.value = s.overdue_grace_minutes
  } catch {}
}
async function saveOverdueGrace() {
  const m = Number(overdueGrace.value)
  if (!Number.isFinite(m) || m < 0 || m > 1440) { alert('宽限期需在 0~1440 分钟之间（0=到点即逾期）'); return }
  graceSaving.value = true
  try {
    const r = await api.post('/settings/overdue-grace', { minutes: m })
    overdueGrace.value = r.minutes
    alert(`逾期宽限期已设为 ${r.minutes} 分钟，即时生效`)
  } catch (e) { alert(e.response?.data?.error || '保存失败') }
  finally { graceSaving.value = false }
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
    hookOpen.value = false   // 保存成功后收起新增/编辑表单
    await loadHooks()
  } catch (e) { alert(e.response?.data?.error || '保存失败') }
  finally { savingH.value = false }
}
async function editHook(w) {
  editHookId.value = w.id
  hookEditingName.value = w.name
  Object.assign(h, { name: w.name, url: w.url, type: w.type, secret: '', dept_id: w.dept_id })
  hookOpen.value = true
  window.scrollTo({ top: 0, behavior: 'smooth' })
}
function cancelEditHook() {
  editHookId.value = 0
  hookEditingName.value = ''
  Object.assign(h, { name: '', url: '', dept_id: h.dept_id, type: 'wecom', secret: '' })
  hookOpen.value = false
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
const tmplOpen = ref(false)   // 自定义模板新增/编辑表单：默认收起
function toggleTmplForm() {
  // 收起时若在编辑态则重置，避免再次打开残留旧内容
  if (tmplOpen.value && tmplForm.id) resetTmplForm()
  tmplOpen.value = !tmplOpen.value
}
function resetTmplForm() { Object.assign(tmplForm, { id: null, type: 'task', name: '', content: '' }) }
function editTemplate(t) {
  tmplForm.id = t.id
  tmplForm.type = t.type
  tmplForm.name = t.name
  tmplForm.content = t.content
  tmplOpen.value = true
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
    const s = await api.get('/settings/full')
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

// 系统运行日志（panic / 5xx / 调度失败），用于崩溃排查
const sysFilter = reactive({ level: '', source: '', q: '', from: '', to: '', limit: 100 })
const sysPage = ref(0)
const sysLogs = ref([])
async function loadSysLogs() {
  const params = { limit: sysFilter.limit, offset: sysPage.value * sysFilter.limit }
  if (sysFilter.level) params.level = sysFilter.level
  if (sysFilter.source) params.source = sysFilter.source
  if (sysFilter.q) params.q = sysFilter.q
  if (sysFilter.from) params.from = sysFilter.from
  if (sysFilter.to) params.to = sysFilter.to + ' 23:59:59'
  sysLogs.value = await api.get('/system-logs', params)
}
function resetSysFilter() {
  Object.assign(sysFilter, { level: '', source: '', q: '', from: '', to: '', limit: 100 })
  sysPage.value = 0
  loadSysLogs()
}
function exportSysLogs() {
  const params = []
  if (sysFilter.level) params.push('level=' + encodeURIComponent(sysFilter.level))
  if (sysFilter.source) params.push('source=' + encodeURIComponent(sysFilter.source))
  if (sysFilter.q) params.push('q=' + encodeURIComponent(sysFilter.q))
  if (sysFilter.from) params.push('from=' + encodeURIComponent(sysFilter.from))
  if (sysFilter.to) params.push('to=' + encodeURIComponent(sysFilter.to + ' 23:59:59'))
  downloadAuth('system-logs/export' + (params.length ? '?' + params.join('&') : ''))
}
function sysLevelStyle(lv) {
  if (lv === 'ERROR') return { color: '#ef4444', borderColor: '#ef4444' }
  if (lv === 'FATAL') return { color: '#b91c1c', borderColor: '#b91c1c', fontWeight: '700' }
  if (lv === 'WARN') return { color: '#f59e0b', borderColor: '#f59e0b' }
  return { color: '#3b82f6', borderColor: '#3b82f6' }
}

// 运行日志的 detail 是各模块自定义的 JSON。这里统一"说人话"：
// 权限类日志必须一眼看出「谁 / 哪条接口 / 缺什么权限」，否则只会看到一句
// 「权限不足（观察期仅记录）」，容易被误读成超级管理员被拦（现场实际问过）。
function sysDetail(l) {
  if (!l || !l.detail) return ''
  let d
  try { d = JSON.parse(l.detail) } catch (e) { return String(l.detail).slice(0, 200) }
  if (!d || typeof d !== 'object') return String(l.detail).slice(0, 200)
  const parts = []
  if (d.operator_name || d.operator_id) parts.push(`操作人：${d.operator_name || '#' + d.operator_id}`)
  if (d.role) parts.push(`角色：${d.role}`)
  if (d.method && d.route) parts.push(`接口：${d.method} ${d.route}`)
  else if (d.route) parts.push(`接口：${d.route}`)
  if (d.perm) parts.push(`缺少权限：${d.perm}`)
  if (d.target_name) parts.push(`对象：${d.target_name}`)
  if (d.action) parts.push(`动作：${d.action}`)
  if (d.ip) parts.push(`IP：${d.ip}`)
  return parts.length ? parts.join(' · ') : JSON.stringify(d)
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
    // 匹配 RFC 5987 filename*=UTF-8''... 或 fallback filename="..."
    const m = cd.match(/filename\*=UTF-8''([^;\s]+)|filename="?([^";]+)"?/)
    const fn = (m && (m[1] || m[2])) ? decodeURIComponent(m[1] || m[2]) : ('download.xlsx')
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
const backupImporting = ref(false)
const backupScope = ref('all') // 手动备份分类（all/schedule/task/user/knowledge）
const backupCfg = reactive({ frequency: 'none', retention: 10, remote_dir: '' })
const scopeNameMap = { all: '全部', schedule: '班表', task: '任务', user: '人员', knowledge: '知识库' }
function scopeName(s) { return scopeNameMap[s] || '全部' }
const backupCfgSaving = ref(false)
async function loadBackups() { backupLoading.value = true; try { const r = await api.get('/backups'); backups.value = Array.isArray(r) ? r : [] } catch { backups.value = [] } finally { backupLoading.value = false } }
async function loadBackupCfg() { try { Object.assign(backupCfg, await api.get('/backup-config')) } catch {} }
async function createBackup() { backupCreating.value = true; try { await api.post('/backups' + (backupScope.value && backupScope.value !== 'all' ? '?scope=' + backupScope.value : '')); await loadBackups() } catch (e) { alert(e.response?.data?.error || '备份失败') } finally { backupCreating.value = false } }
async function importBackup(e) {
  const file = e.target.files && e.target.files[0]
  e.target.value = ''
  if (!file) return
  if (!confirm(`导入并还原备份「${file.name}」？将覆盖当前所有数据，且不可撤销。`)) return
  backupImporting.value = true
  try { await api.upload('/backups/import', file); alert('导入还原成功，系统已切换至该备份'); await loadBackups() }
  catch (err) { alert(err.response?.data?.error || '导入失败') }
  finally { backupImporting.value = false }
}
async function setBackupFreq(f) { backupCfg.frequency = f; await saveBackupCfg() }
async function saveBackupCfg() { backupCfgSaving.value = true; try { await api.post('/backup-config', { frequency: backupCfg.frequency, retention: Number(backupCfg.retention), remote_dir: backupCfg.remote_dir }); alert('已保存') } catch (e) { alert(e.response?.data?.error || '保存失败') } finally { backupCfgSaving.value = false } }
function downloadBackup(b) { downloadAuth('backups/' + b.id + '/download') }
async function restoreBackup(b) { if (!confirm(`确认还原备份「${b.name}」？当前数据将被覆盖，且操作不可撤销！`)) return; try { await api.post('/backups/' + b.id + '/restore'); alert('已还原，页面将自动刷新'); setTimeout(() => location.reload(), 800) } catch (e) { alert(e.response?.data?.error || '还原失败') } }
async function deleteBackup(b) { if (!confirm(`删除备份「${b.name}」？`)) return; try { await api.del('/backups/' + b.id); await loadBackups() } catch (e) { alert(e.response?.data?.error || '删除失败') } }

// 备份校验（演练还原）：调用后端只读校验 + 临时库演练还原，不改动线上数据
const verifyMsgs = reactive({})
const verifying = reactive({})
async function verifyBackup(b) {
  verifying[b.id] = true
  verifyMsgs[b.id] = '校验中…'
  try {
    const r = await api.post('/backups/' + b.id + '/verify')
    const parts = ['完整性:' + r.integrity, '演练还原:' + (r.drill_ok ? '成功' : '失败'), '表:' + (r.tables ? r.tables.length : 0) + '个']
    if (r.missing_tables && r.missing_tables.length) parts.push('缺表:' + r.missing_tables.join(','))
    const icon = r.verdict === 'ok' ? '✅' : (r.verdict === 'warning' ? '⚠️' : '❌')
    verifyMsgs[b.id] = icon + (r.message || '') + ' 〔' + parts.join(' / ') + '〕'
  } catch (e) {
    verifyMsgs[b.id] = '❌ ' + (e.response?.data?.error || '校验失败')
  } finally {
    verifying[b.id] = false
  }
}

// 退出登录。
// ★ 顺序不能改（v0.40.4）：auth.logout() 内部**同步**清空 token/user/权限，
//   所以紧接着的 replace 不会命中守卫里「已登录访问 /login 就弹回首页」那条，
//   全程无"闪一下主界面"。也不要改成 await auth.logout()——远端通知有意不阻塞导航。
function onLogout() { auth.logout(); router.replace('/login') }

// 切换 tab 时按需加载
watch(tab, (v) => {
  if (v === 'backup' && auth.isSuper) { loadBackups(); loadBackupCfg() }
  if (v === 'log' && auth.canManage) { logPage.value = 0; loadLogs(); if (auth.isSuper) loadLogRetention() }
  if (v === 'syslog' && auth.canManage) { sysPage.value = 0; loadSysLogs() }
  if (v === 'tmpl' && auth.canManage) loadTemplates()
  if (v === 'hook' && auth.canManage) { loadHooks(); if (auth.isSuper) { loadSMTP(); loadDailySummary() } }
  if (v === 'brand' && auth.isSuper) loadOverdueGrace()
})

onMounted(async () => {
  // 系统管理 tab 的准入兜底：直接从旧地址 /system/xxx 进来但无对应权限时，
  // 该 tab 不在 sysTabs 里（区块也不渲染），这里退回「个人信息」而不是留空白。
  if (SYS_TABS.some((t) => t.key === tab.value) && !sysTabs.value.some((t) => t.key === tab.value)) {
    setTab('me')
  }
  await loadBrand()
  if (auth.canManage) {
    // departments 仍需加载：通知（Webhook）的部门下拉复用它
    await Promise.all([loadDepts(), loadHooks(), loadLogs()])
    if (auth.isSuper) loadSMTP()
    if (auth.isSuper) loadLogRetention()
    if (auth.isSuper) loadOverdueGrace()
  }
})
</script>

<style scoped>
.tabs { display: flex; gap: 6px; margin-bottom: 16px; flex-wrap: wrap; align-items: center; }
/* v0.39.1 系统管理分组：与常规 tab 之间加一道竖线，把「系统管理」与常规设置项分开 */
.tab-gap { width: 1px; align-self: stretch; margin: 2px 6px; background: var(--glass-border); }

/* ---- 运行日志：detail 摘要 ---- */
.log-action { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.la-msg { font-size: 12.5px; }
.la-detail { font-size: 11.5px; color: var(--text-faint); word-break: break-all; }
/* 嵌入的系统管理页自带 .adm-page（含 .panel 卡片），外层不再加内边距，避免"卡中卡" */
.sys-embed { display: block; }
.tab { border: 1px solid var(--glass-border); background: var(--glass); color: var(--text-dim); padding: 9px 16px; border-radius: 12px; cursor: pointer; font-size: 13px; }
.tab.active { background: var(--glass-strong); color: var(--text); border-color: var(--glass-border-strong); }

/* Webhook 部门必选：未选红框提示 */
select.req-miss { border-color: var(--danger, #e11d48); box-shadow: 0 0 0 2px rgba(225, 29, 72, 0.12); }
.form-col { display: flex; flex-direction: column; gap: 14px; margin-bottom: 16px; max-width: 520px; }
.info-row { display: flex; align-items: center; gap: 14px; }
.info-row .fld { width: 80px; flex: none; margin: 0; }
.info-val { font-size: 14px; color: var(--text); }
.user-toolbar .btn { padding: 8px 14px; font-size: 13px; }
.op.danger { color: var(--danger); }
.op.danger:hover { background: rgba(225,29,72,0.12); color: var(--danger); }

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
}
.log-filter { display: flex; gap: 8px; margin-bottom: 12px; flex-wrap: wrap; }
.log-filter .glass-input { max-width: 220px; padding: 8px 12px; font-size: 13px; }
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
.row { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 12px 14px; border-radius: 13px; background: var(--overlay); border: 1px solid var(--glass-border); }
.row-main { display: flex; align-items: center; gap: 11px; flex: 1; min-width: 0; }
.avatar.sm { width: 34px; height: 34px; border-radius: 10px; font-size: 13px; flex: none; display: grid; place-items: center; background: var(--brand-grad); color: #fff; font-weight: 700; }
.rn { font-size: 14px; font-weight: 600; display: flex; align-items: center; }
.ru { font-size: 12px; color: var(--text-faint); margin-top: 3px; }
.mono { font-family: ui-monospace, monospace; font-size: 12px; color: var(--text-dim); word-break: break-all; }
.del { padding: 6px 13px; border-radius: 10px; border: 1px solid var(--glass-border); background: transparent; color: var(--text-faint); cursor: pointer; font-size: 12.5px; }
.del:hover { color: var(--danger); border-color: rgba(225,29,72,0.4); }
.hook-cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(250px, 1fr)); gap: 10px; }
.hook-card { display: flex; flex-direction: column; gap: 10px; padding: 14px; border-radius: 13px; background: var(--overlay); border: 1px solid var(--glass-border); min-width: 0; }
.hook-card-top { display: flex; align-items: center; gap: 10px; min-width: 0; }
.hook-ico { width: 36px; height: 36px; flex: none; border-radius: 10px; display: grid; place-items: center; font-size: 15px; font-weight: 700; color: #fff; }
.hook-ico.t-wecom { background: #3a8bfd; }
.hook-ico.t-dingtalk { background: #2e8cff; }
.hook-ico.t-feishu { background: #3370ff; }
.hook-title { min-width: 0; }
.hook-title .rn { font-size: 13.5px; }
.hook-url { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.hook-card-actions { display: flex; gap: 8px; margin-top: auto; }
/* 审计 / 运行日志：表头与数据行共用同一套栅格模板，逐列对齐。
   要点：列宽固定，缺失值渲染占位符（而不是 v-if 移除元素），否则整行会错位。 */
.log-table { overflow-x: auto; }
.log-row {
  display: grid; align-items: baseline; gap: 12px; min-width: 760px;
  grid-template-columns: 152px 112px 76px 132px minmax(240px, 1fr);
}
.log-table.sys .log-row { min-width: 600px; grid-template-columns: 152px 64px 130px minmax(240px, 1fr); }
.log-head {
  padding: 8px 4px; font-size: 12px; font-weight: 600; letter-spacing: 0.02em;
  color: var(--text-faint); border-bottom: 1px solid var(--glass-border-strong, var(--glass-border));
}
.log-list { display: flex; flex-direction: column; gap: 2px; }
.log { padding: 10px 4px; font-size: 13px; border-bottom: 1px solid var(--hairline); }
.log-time { color: var(--text-faint); font-family: ui-monospace, monospace; font-size: 12px; white-space: nowrap; }
.log-user { color: var(--accent); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.log-ip { color: var(--text-faint); font-family: ui-monospace, monospace; font-size: 12px; white-space: nowrap; }
.log-src { min-width: 0; }
.log-src .src-badge { font-style: normal; color: var(--text-faint); font-size: 12px; white-space: nowrap; border: 1px solid var(--glass-border); border-radius: 6px; padding: 0 5px; line-height: 16px; }
.log-src .src-none { font-style: normal; color: var(--text-faint); font-size: 12px; }
.log-action { color: var(--text-dim); min-width: 0; overflow-wrap: anywhere; }
.retention-bar { display: flex; align-items: center; gap: 10px; margin-bottom: 12px; flex-wrap: wrap; }
.retention-hint { font-size: 12px; color: var(--text-faint); }
.pager { display: flex; align-items: center; gap: 14px; justify-content: center; margin-top: 14px; }
.pager-info { font-size: 13px; color: var(--text-dim); }
.backup-list { display: flex; flex-direction: column; gap: 8px; }
.row.frozen { opacity: 0.6; }
.row.frozen .avatar.frozen-av { filter: grayscale(1); }
.row-actions { display: flex; gap: 6px; align-items: center; }
.row.sel { border-color: var(--accent, #4f46e5); background: var(--accent-soft, rgba(79,70,229,0.08)); }
.btn.sm, .glass-input.sm { padding: 6px 12px; font-size: 12.5px; }
.btn.ghost.active { color: var(--accent); border-color: var(--accent); background: var(--accent-soft, rgba(79,70,229,0.12)); }
.chip.online { background: rgba(22,163,74,0.14); color: var(--success, #16a34a); border: 1px solid rgba(22,163,74,0.35); }
.mini.danger-txt { color: var(--danger, #e11d48); }
.mini.danger-txt:hover { border-color: var(--danger, #e11d48); color: var(--danger, #e11d48); }
.mini { font-size: 12px; padding: 4px 10px; border-radius: 7px; border: 1px solid var(--glass-border); background: transparent; color: var(--text-dim); cursor: pointer; }
.mini:hover { color: var(--text); border-color: var(--accent); }
.modal-head .x { width: 28px; height: 28px; border-radius: 8px; border: none; background: var(--overlay-2); color: var(--text-dim); cursor: pointer; font-size: 16px; }
.backup-row { display: flex; align-items: center; justify-content: space-between; padding: 10px 14px; border-radius: 11px; background: var(--overlay); border: 1px solid var(--glass-border); }
.b-name { font-size: 13.5px; font-weight: 600; }
.b-meta { font-size: 11.5px; color: var(--text-faint); margin-top: 2px; }
.b-remote { color: var(--success, #16a34a); }
.scope-chip { display: inline-block; padding: 1px 7px; border-radius: 7px; font-size: 10.5px; margin-right: 6px; background: var(--overlay-2); color: var(--text-dim); }
.scope-chip.s-all { background: var(--accent-soft, rgba(79,70,229,.12)); color: var(--accent, #4f46e5); }
.scope-chip.s-schedule { background: rgba(56,189,248,.12); color: var(--accent-2, #0ea5e9); }
.scope-chip.s-task { background: rgba(217,119,6,.12); color: var(--warn, #d97706); }
.scope-chip.s-user { background: rgba(5,150,105,.12); color: var(--ok, #059669); }
.scope-chip.s-knowledge { background: rgba(139,92,246,.14); color: #8b5cf6; }
.b-actions { display: flex; gap: 6px; align-items: center; }
.b-actions .btn { padding: 6px 12px; font-size: 12.5px; }
.b-actions .del { width: 24px; height: 24px; }
.btn.danger { background: var(--danger); border-color: var(--danger); color: #fff; }
.btn.danger:hover { opacity: 0.9; }
.btn.ghost.active { border-color: var(--accent); color: var(--accent); }
.btn.ghost.sm { padding: 4px 10px; font-size: 12px; margin: 0; }
.import-ta { width: 100%; resize: vertical; font-family: ui-monospace, monospace; font-size: 12.5px; line-height: 1.7; }
.shift-chip .mini { padding: 0 5px; font-size: 13px; line-height: 1; }
.shift-picker-row .fld { margin: 0; }
.shift-add .glass-input.sm { max-width: 120px; padding: 7px 10px; font-size: 13px; }
.shift-sep { color: var(--text-faint); font-size: 12px; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 820px) { .fg2 { grid-template-columns: 1fr; } }
</style>
