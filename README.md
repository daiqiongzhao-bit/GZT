# 企业任务通知管理

> 面向团队 / 门店的「排班 + 任务通知 + 定时提醒」一体工作台（原名：企业排班任务工作台）

单二进制 / Docker 双架构部署的全栈应用：**Go（Gin + GORM + 纯 Go SQLite）后端 + Vue3（Vite）前端**，前端静态资源经 `embed` 内联进同一个可执行文件，**零外部运行时依赖**，拷贝即用。

## 一句话定位

把「排班、每日/月度任务、定时 Webhook 推送、整月可视矩阵」收进一个自托管工作台：管理者排班派活，成员到点收到任务汇总与当班提醒；每日/月度任务按周期自动重置、逾期自动判定，昨天没做完的不会凭空消失。

---

## 功能总览

### 排班管理
- **整月日历视图**：周一起始，每日格子显示班次与当班人员，可自由翻看任意月份
- **整月矩阵视图（Excel 风格）**（v0.2.0+）：整部门一次成表，列 = 姓名/工号/当月日期，行 = 人员；每格显示班次，底部「每日当班」行自动统计当日人数并做 **早 / 中 / 晚 / 夜 四班分列 + 当日总当班**
  - 休息班次也会入格显示（不再凭空消失），但**不计入工时与当班统计**
  - 班次颜色可配：部门管理页可为每个班次选色（蓝/绿/橙/紫），矩阵按色块区分班型，图例同步
- **Excel / CSV 导入**：人×日期矩阵模板一键导入；导入时逐个人名核对系统账号，**无账号人员单独列出警告**（推送/@ 不到），班表照常导入不阻断
- 导入幂等：重复的日期+班次+人员自动跳过

### 任务通知
- 任务类型：**每日 / 每月 / 单次**，逾期自动判定，每日 00:00、每月 1 日**周期自动重置**（月度截止日自动推进到当月）
- **Webhook 定时推送**：每天 09:00 自动推送当日任务汇总并 **@ 当班人员**；到点任务逐条提醒
- **站内通知 + 部门广播**（v0.2.0+）：可向整个部门广播通知，成员在铃铛抽屉查看
- 支持企业微信 / 钉钉 / 飞书 / 邮件
- **推送名单与 @ 分离**：成员可标记「已加入企业微信通知群」，推送只 @ 已入群成员，名单只列出未入群成员，不重复
- 推送排除休息班次；当班判定排除休息人员

### 概览看板
- 今日当班（按部门、绑定任务）、本月任务进度与完成率、今日应办与逾期任务一键处理
- 任务支持删除与批量删除

### 组织与权限
- 部门树（`parent_id`，支持子部门）、人员管理（账号 = 工号）
- **三级角色 RBAC**：超级管理员 / 部门管理员 / 执行者，部门数据隔离（含子孙部门可见性）
- 人员批量导入 + **CSV 导出**（v0.2.0+）
- 手机号自动归一化（去空格/横线/前缀），保证企业微信 @ 能按纯数字匹配

### 品牌与安全
- **企业 Logo 上传**（建议 ≥512×512 透明 PNG / ICO），上传后自动去除背景底色；登录页、侧边栏、浏览器标签页 **favicon 全部跟随**（v0.2.1/0.2.2），支持恢复默认
- 企业信息自定义：系统名称、版权信息（页面底部展示）、公告等
- Webhook 地址 **AES-256-GCM 加密**存储，非超管仅见脱敏地址
- JWT 登录（`client_type` 三端标识）、密码 bcrypt 哈希、登录限流、账号冻结、单设备登出、超管强制全员下线

### 备份与运维
- 整库备份 / 还原，**按范围分类**（全部 / 班表 / 任务 / 人员，v0.2.0+），支持自动定时备份
- 操作日志区分来源（网页 / PWA / 插件）并记录 IP，界面显示版本号

### 界面与终端
- 玻璃拟态 UI：深色基底 + 毛玻璃面板，桌面侧边栏 / 移动端悬浮底栏自适应
- **PWA**：内置 manifest + 版本化 Service Worker，可「添加到主屏幕」当 APP 用；离线可开壳
- **浏览器插件**（Manifest V3，只读助手）：弹窗看当日任务/逾期，登录经网页桥接页同步令牌
- 三端统一鉴权，单设备登出互不影响

---

## 技术栈

| 层 | 技术 |
|----|------|
| 后端 | Go 1.21 + Gin + GORM + 纯 Go SQLite（glebarez/sqlite，CGO=0） |
| 前端 | Vue 3（Composition API）+ Vite + Naive UI + Pinia + axios |
| 打包 | 后端 `embed` 内联前端 `dist`，产出单二进制 `swb` |
| 部署 | Docker 多阶段构建，`linux/amd64` + `linux/arm64` 双架构 manifest list |
| 运行时依赖 | 无（连数据库都内嵌），镜像仅含单二进制 + 证书 + 时区 |

---

## 目录结构

```
GZT/
├── backend/              # Go 后端（Gin + GORM + 纯 Go SQLite）
│   ├── internal/         # config / db / models / middleware / handlers / service
│   ├── web/dist/         # 前端构建产物（被 embed 内联）
│   └── main.go           # 路由 + 静态托管 + SPA 回退
├── frontend/             # Vue3 前端
│   └── src/              # views / store / api / router / styles / icons
├── tests/                # 单元测试与线上验证脚本（verify_*.py）
├── extension/            # 浏览器插件（Manifest V3，只读型助手）
├── Dockerfile
├── docker-compose.yml
├── build.sh              # 本地一键构建单二进制
└── .env.example
```

---

## 快速开始

### 方式一：本地构建单二进制（零容器）

```bash
bash build.sh            # 安装前端依赖、构建前端、编译后端 -> 生成 ./swb
./swb                    # 默认监听 :8080，数据存 ./shift_workbench.db
```

浏览器打开 `http://localhost:8080`

### 方式二：Docker / Compose

```bash
docker compose up -d --build
# 访问 http://localhost:8080 ，数据库持久化于 ./data/swb.db
```

直接使用已发布镜像（无需本地构建，amd64 / arm64 自动匹配）：

```bash
docker pull daiqiongzhao/gzt:latest
```

> 多架构构建说明：`Dockerfile` 中的前端构建与 Go 编译都固定在 `BUILDPLATFORM`（原生架构）执行，
> 仅最终运行镜像按目标平台拉取，因此构建 arm64 镜像无需 QEMU 模拟，耗时与单架构基本一致。

### 方式三：仅前端开发联调

```bash
cd frontend && pnpm install && pnpm dev   # Vite 开发服务器，代理 /api -> :8080
# 另开终端：cd backend && go run .
```

---

## 账号初始化

首次启动且数据库为空时，自动注入**默认超级管理员**与业务演示数据（部门、班表、任务含逾期示例、企业设置）：

| 账号 | 密码 | 角色 |
|---|---|---|
| `admin` | `admin123` | 超级管理员 |

> ⚠️ 部署完成后请**第一时间修改默认密码**（登录 → 右上角 → 修改密码）。
> 若数据库已存在数据，则不会重复注入。

> **账号 = 工号**：普通员工登录账号统一为工号（如工号 3275 → 账号 3275），新建/编辑/导入时系统自动强制。超管账号无工号，单独设置。

---

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `APP_PORT` | `8080` | 监听端口 |
| `DB_PATH` | `shift_workbench.db` | SQLite 文件路径 |
| `JWT_SECRET` | 内置默认 | JWT 签名密钥（**生产务必修改**） |
| `AES_KEY` | 内置默认 | Webhook 加密密钥（**生产务必修改**，建议 32 位） |
| `BACKUP_DIR` | `./backups` | 备份目录（可异地目录） |
| `CORS_ORIGINS` | `*` | 允许的跨域来源（逗号分隔） |

---

## 任务逾期与周期重置

| 类型 | 今日应办 `due_today` | 逾期 `overdue` |
|------|----------------------|----------------|
| 每日 daily | 未完成即今日应办 | 已过当天执行时间（如 09:00、10:00…）仍未完成 |
| 每月 monthly | 当月日期 == 锚定日且未完成 | **截止日当天 23:59 前都算准时**，次日仍未完成才逾期（09:00 仅作提醒时点） |
| 单次 once | `deadline` 日期 == 今天且未完成 | `deadline` 早于当前时间仍未完成 |

- 每日/月度任务**按周期自动重置**：每日 00:00、每月 1 日回到待办；月度截止日自动推进到当月（如 8/31 → 9/30，月末溢出自动取当月最后一天）；幂等设计，进程重启/容器休眠不丢状态，完成审计历史原样保留
- 已完成任务不参与统计；`/tasks` 每条附带 `due_today` / `overdue` / `due_this_month` 瞬态字段

---

## 权限矩阵（RBAC + 部门隔离）

| 操作 | 超管 | 部门管 | 执行者 |
|------|------|--------|--------|
| 建/改部门、改设置 | ✅ | ❌ | ❌ |
| 建/删任务、班表、人员、Webhook | ✅ | ✅（仅本部门） | ❌ |
| 完成任务 `toggle` | ✅ | ✅（仅本部门） | ✅（仅本部门） |
| 读仪表盘/任务/班表/人员 | ✅ | ✅（仅本部门） | ✅（仅本部门） |
| 看 Webhook 地址 | 明文 | 明文 | 脱敏 `••••` |
| 备份/还原系统 | ✅ | ❌ | ❌ |

要点：所有列表接口按 `dept_id` 过滤，跨部返回 403；部门管理员可见本部门及子孙部门。

---

## 排班与整月矩阵

- **整月日历视图**：周一起始，每日格子显示班次与当班人员
- **整月矩阵视图**：整部门 Excel 风格成表（列 = 序号/姓名/工号 × 当月日期），每日底部自动统计 **早 / 中 / 晚 / 夜四班当班人数 + 当日总当班**（`休息` 入格但不计当班）
- **班次颜色可配**：部门管理页为班次选色，矩阵按色块区分、图例同步
- 支持 Excel（人×日期矩阵模板）/ CSV 导入；导入时自动核对人员账号，无账号的列出警告（不会收到推送/@）
- 今日当班统计与推送**排除休息班次**

---

## 系统备份与还原

- 手动备份：`POST /backups`，存为 `swb-backup-YYYY-MM-DD-HHMMSS.db`
- 按范围备份：可只备份班表 / 任务 / 人员某一部分
- 自动备份：`POST /backup-config` 配置频率/保留份数/异地目录，后台定时执行
- 还原：`POST /backups/:id/restore` 先备份当前库再覆盖（危险操作前端二次确认）

---

## API 概览（前缀 `/api`）

- `POST /auth/login` 登录 · `GET /auth/me` 当前用户 · `POST /auth/change-password` 改密 · `POST /auth/unlock` 解锁定
- `GET /dashboard` 概览聚合（今日当班/任务/逾期/本月进度）
- `GET/POST/DELETE /departments`（写限超管）
- `GET/POST /users` · `PUT/DELETE /users/:id` · `POST /users/import`（导入）· `GET /users/export`（CSV 导出）· `POST /users/:id/reset-password`
- `GET/POST /schedules` · `PUT/DELETE /schedules/:id` · `POST /schedules/import`（Excel/CSV 导入，返回无账号人员警告）
- `GET/POST /tasks` · `PUT/DELETE /tasks/:id` · `POST /tasks/:id/toggle` · `POST /tasks/batch-delete` · `GET /tasks/counts`
- `GET /tasks/:id/completions` 完成历史 · `GET /completions` 全局完成记录（按部门/时间筛选）
- `GET/POST/PUT/DELETE /webhooks` · `POST /webhooks/notify` 手动推送今日任务提醒
- `GET/POST /settings`（超管） · `GET /logs` · `POST /notifications/:id/read` · `POST /notifications/broadcast`（部门广播）
- `POST /logout` · `POST /users/:id/force-logout`（超管强制下线） · `GET /sessions`
- `GET /backups` · `POST /backups`（手动备份）· `POST /backups/:id/restore` · `POST /backup-config`（自动备份）

---

## 已知限制

- 单 SQLite 文件，未做读写分离/水平扩展；高并发写入需换 Postgres（GORM 驱动可平滑切换）
- 班表/推送按姓名匹配人员，同部门内姓名需唯一
- PWA 为「离线开壳 + 在线数据」级别：断网可打开页面，但任务/排班等数据需联网
- 浏览器插件为只读助手，不支持在插件内创建/编辑/完成任务
- 企业微信 @ 依赖「当班 + 在群里 + 填了手机号」三条件，机器人只能 @ 群成员

---

## 浏览器插件

`extension/`（Manifest V3）只读助手：开发者模式加载即可使用，令牌经网页端桥接页（`/ext-bridge.html`）同步，与网页/PWA 令牌隔离。详见 `extension/README.md`。

> 开源版本默认值指向 `http://localhost:8080`；部署到自己的地址后需改两处：
> `extension/background.js` 与 `extension/popup.js` 的 `DEFAULT_WEB`，
> 以及 `extension/manifest.json` 中 `externally_connectable.matches`（网页端令牌桥接用）。

---

## 测试

```bash
# 单元测试（需 Go 1.21+）
cd backend && go test ./...

# 线上端到端（需部署后可访问）
python3 tests/verify_full.py        # 全面体检
python3 tests/verify_v0130.py       # 周期重置
python3 tests/verify_v0131.py       # 月度逾期判定
python3 tests/verify_v0140.py       # 班表无账号警告 + 账号=工号
```

---

## 版本历史

| 版本 | 变更类型 | 内容 |
|---|---|---|
| **v0.2.2** | 增强 | 浏览器标签页 favicon 跟随企业 Logo；侧边栏 Logo 去蓝底（透明背景，`:has()` 双保险兼容旧 WebView）；整月矩阵增强——休息班次入格显示（不计工时/当班）、当班人数做**早/中/晚/夜四班区分 + 当日总当班**、班次颜色可配并同步图例、legend 早/中颜色修正 |
| **v0.2.1** | 修复 | 5 项反馈修复：上传 Logo 蓝底/模糊（提示上传 ≥512×512 清晰原图）、侧边栏/登录页 Logo 背景优化、全站改名为「企业任务通知管理」（标题/favicon/title 动态同步）、设置里版权信息显示到页面底部 footer、概览本月完成率恒 0%（任务统计不再过滤已完成项） |
| **v0.2.0** | 增强 | ①人员批量 CSV 导出 ②部门广播通知（Notification 模型 + 列表/未读/已读/全部已读）③备份按范围分类（全部/班表/任务/人员）④班表整部门整月矩阵视图（Excel 风格 + 工时统计）⑤版本号紧凑显示 |
| v0.1.0 | 增强 | 操作日志区分来源（网页/PWA/插件）并记录 IP，界面显示版本号；任务完成/重开幂等化；站内通知抽屉主题修复；Dashboard 勾选优化；班表跨部门迁移与去重 |
| v0.0.5 | 修复 | 班表人员归属校验、支持跨部门迁移与编辑、跨部门去重（一人一天一个班次） |
| v0.0.4 | 修复 | 站内通知抽屉透明底色修复（补 CSS 变量） |
| v0.0.3 | 修复 | Dashboard 勾选框 SVG 隐藏问题 |
| v0.0.2 | 修复 | 清理过期版本化体检脚本、任务完成/重开幂等 |
| v0.0.1 | 初始 | 全新起点：班表管理、任务（每日/月度/单次 + 周期自动重置）、Webhook 定时推送与 @、RBAC 三级权限、账号 = 工号、手机号归一化、导入导出、PWA 三端 + 浏览器插件、推送名单与 @ 分离（in_group 标记） |

> 语义化版本说明：v0.0.1–v0.0.5 → v0.1.0 → v0.2.0 → v0.2.1 → v0.2.2，release.sh 从最新 tag 自动 bump。

---

## 部署

生产环境以 Docker Compose 方式运行，直接拉取镜像 `daiqiongzhao/gzt:latest` 即可，无需本地构建。
该镜像为 **amd64 + arm64 双架构 manifest list**，同一条 compose 配置可在 x86 服务器与 ARM 设备上通用。

发布流程见 `release.sh`：语义化版本 tag → GitHub → Docker Hub（**同时产出 amd64 + arm64 多架构镜像**）→ 重启容器。每个版本都会留下语义化 tag（如 `v0.2.2`），回滚时把 compose 里的 `latest` 换成对应版本号即可。

详细安装部署步骤（服务器要求、Docker/二进制两种方式、Nginx 反代、品牌配置等）见 [DEPLOY.md](./DEPLOY.md)。
