# 企业排班任务工作台（v0.14.3）

按产品级方案落地的**前后端分离 + 单二进制部署**全栈应用：Go(Gin+GORM+SQLite) 后端、Vue3(Vite+Naive UI+Pinia) 前端，最终打包为一个可执行文件，内置前端静态资源，零外部运行时依赖。

## 特性

- **单二进制**：`go build` 后只有一个可执行文件，前端资源通过 `embed` 内联，部署只需拷贝一个文件
- **鉴权与 RBAC**：JWT 登录，三级角色（超级管理员 / 部门管理员 / 执行者），部门数据隔离（含祖先部门可见）
- **安全**：Webhook 地址 AES-256-GCM 加密存储，非超管仅见脱敏地址；密码 bcrypt 哈希；登录限流、账号冻结、强制下线（`token_version`）
- **业务模块**：概览、排班（班表）、任务（每日/每月/单次 + 逾期判定）、部门/人员、Webhook 通知、企业设置、系统日志、备份还原
- **周期任务自动重置**（v0.13.0）：每日任务每天 00:00 自动回到待办，月度任务每月 1 日重置并把截止日推进到当月（月末溢出自动取当月最后一天，如 8/31 → 9/30）；幂等设计，进程重启/容器休眠不丢状态；完成审计历史原样保留
- **月度任务完成期限一整天**（v0.13.1）：截止日 09:00 只是晨间推送提醒时点，不是硬性期限；当天 23:59 前完成均算准时，次日仍未完成才判逾期
- **班表导入无账号人员警告**（v0.14.0）：导入班表（Excel/CSV）时逐个人名核对系统账号，无账号的单独列出警告（推送/@ 不到），班表照常导入不阻断
- **账号 = 工号统一规则**（v0.14.0/1）：新建用户账号自动 = 工号（超管除外）；改工号同步改登录账号并强制重新登录；导入匹配兼容历史数据
- **手机号自动归一化**（v0.14.3）：保存手机号时自动去空格/横线/括号、剥离 +86/0086 前缀，保证企业微信 @ 能按纯数字匹配
- **通知推送排除休息班次**（v0.14.2）：休息人员不参与当班列表与推送 @，与仪表盘口径一致
- **Webhook 通知**：支持企业微信/钉钉/飞书/邮件，每天 09:00 自动推送今日任务汇总并 @ 当班人员；到点任务逐条提醒；Webhook 可编辑（名称/类型/地址/密钥/部门）
- **玻璃拟态 UI**：深色基底 + 柔光色斑 + 毛玻璃面板，桌面侧边栏 / 移动端悬浮底栏自适应
- **PWA**：内置 manifest 与版本化 Service Worker，可"添加到主屏幕"；离线可开壳（应用静态资源缓存，API 始终走网络）
- **浏览器插件**：`extension/`（Manifest V3）只读型助手，弹窗查看当日任务/逾期，登录经网页端桥接页同步令牌
  - 开源版本默认值指向 `http://localhost:8080`；部署到自己的地址后需改两处：
    `extension/background.js` 与 `extension/popup.js` 的 `DEFAULT_WEB`，
    以及 `extension/manifest.json` 中 `externally_connectable.matches`（网页端令牌桥接用）
- **三端统一鉴权**：网页 / PWA / 浏览器插件共用 JWT 体系，令牌带 `client_type` 标识；支持单设备登出与超管强制全员下线
- **CORS**：后端内置 CORS 中间件，允许插件跨域调用 API（仅放行 `Authorization` 头，不启用 credential）
- **Docker**：多阶段构建，镜像仅含单二进制 + 证书/时区，数据库通过卷持久化
- **多架构镜像**：同一 tag 同时提供 `linux/amd64` 与 `linux/arm64`，Docker 会按设备架构自动匹配——x86 服务器、飞牛/群晖 NAS、ARM 开发板均可直接拉取同一个 `latest`

## 目录结构

```
shift-workbench/
├── backend/              # Go 后端（Gin + GORM + 纯 Go SQLite）
│   ├── internal/         # config / db / models / middleware / handlers / service
│   ├── web/dist/         # 前端构建产物（被 embed 内联）
│   └── main.go           # 路由 + 静态托管 + SPA 回退
├── frontend/             # Vue3 前端
│   └── src/              # views / store / api / router / styles / icons
├── tests/                # 单元测试与线上验证脚本（verify_*.py）
├── Dockerfile
├── docker-compose.yml
├── extension/            # 浏览器插件（Manifest V3，只读型助手）
├── build.sh              # 本地一键构建单二进制
└── .env.example
```

## 快速开始

### 方式一：本地构建单二进制（推荐，零容器）

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

## 账号初始化

首次启动且数据库为空时，自动注入**默认超级管理员**与业务演示数据（部门、班表、任务含逾期示例、企业设置）：

| 账号 | 密码 | 角色 |
|---|---|---|
| `admin` | `admin123` | 超级管理员 |

> ⚠️ 部署完成后请**第一时间修改默认密码**（登录 → 右上角 → 修改密码）。
> 若数据库已存在数据，则不会重复注入。

首个超管需手动创建：用 `sqlite3` 向 `users` 表插入一条 `role='super_admin'` 的记录（密码用 bcrypt 哈希），或通过已有超管在后台「人员」页添加。示例：

```bash
HASH=$(python3 -c "import bcrypt,sys; print(bcrypt.hashpw(b'MyPass123', bcrypt.gensalt()).decode())")
sqlite3 shift_workbench.db "INSERT INTO users(username,password_hash,name,role,dept_id) VALUES('admin','$HASH','系统管理员','super_admin',1);"
```

> **账号 = 工号**：普通员工登录账号统一为工号（如工号 3275 → 账号 3275），新建/编辑/导入时系统自动强制。超管账号无工号，单独设置。

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `APP_PORT` | `8080` | 监听端口 |
| `DB_PATH` | `shift_workbench.db` | SQLite 文件路径 |
| `JWT_SECRET` | 内置默认 | JWT 签名密钥（**生产务必修改**） |
| `AES_KEY` | 内置默认 | Webhook 加密密钥（**生产务必修改**，建议 32 位） |
| `BACKUP_DIR` | `./backups` | 备份目录（可异地目录） |
| `CORS_ORIGINS` | `*` | 允许的跨域来源（逗号分隔） |

## API 概览（前缀 `/api`）

- `POST /auth/login` 登录 · `GET /auth/me` 当前用户 · `POST /auth/change-password` 改密 · `POST /auth/unlock` 解锁定
- `GET /dashboard` 概览聚合（今日当班/任务/逾期/本月进度）
- `GET/POST/DELETE /departments`（写限超管）
- `GET/POST /users` · `PUT/DELETE /users/:id` · `POST /users/import`（导入）· `POST /users/:id/reset-password`
- `GET/POST /schedules` · `PUT/DELETE /schedules/:id` · `POST /schedules/import`（Excel/CSV 导入，返回无账号人员警告）
- `GET/POST /tasks` · `PUT/DELETE /tasks/:id` · `POST /tasks/:id/toggle` · `POST /tasks/batch-delete` · `GET /tasks/counts`
- `GET /tasks/:id/completions` 完成历史 · `GET /completions` 全局完成记录（按部门/时间筛选）
- `GET/POST/PUT/DELETE /webhooks` · `POST /webhooks/notify` 手动推送今日任务提醒
- `GET/POST /settings`（超管） · `GET /logs` · `POST /notifications/:id/read`
- `POST /logout` · `POST /users/:id/force-logout`（超管强制下线） · `GET /sessions`
- `GET /backups` · `POST /backups`（手动备份）· `POST /backups/:id/restore` · `POST /backup-config`（自动备份）

## 版本历史

| 版本 | 内容 |
|------|------|
| v0.12.5–v0.12.7 | 交接表直连导入、当班统计排除休息（Dashboard） |
| v0.12.8 | 月度任务截止 09:00、按完整日期防跨月误判 |
| v0.12.9–v0.12.14 | 推送三大修复（部门可见、09:00 自动汇总、跨月误推）、Webhook 编辑 |
| v0.13.0 | **周期任务自动重置**（每日/月度），含月末溢出处理 |
| v0.13.1 | 月度任务完成期限放宽为"截止日当天一整天"，09:00 仅作提醒 |
| v0.14.0/1 | 班表导入无账号人员警告；账号=工号统一规则 |
| v0.14.2 | 通知推送排除休息班次（与 Dashboard 口径一致） |
| v0.14.3 | 手机号自动归一化（去空格/横线，兼容 +86） |

## 任务判定规则（前后端共用逻辑）

| 类型 | 今日应办 `due_today` | 逾期 `overdue` |
|------|----------------------|----------------|
| 每日 daily | 未完成即今日应办 | 已过当天执行时间（如 09:00、10:00…）仍未完成 |
| 每月 monthly | 当月日期 == 锚定日且未完成 | **截止日当天 23:59 前都算准时**，次日仍未完成才逾期（09:00 仅作提醒时点） |
| 单次 once | `deadline` 日期 == 今天且未完成 | `deadline` 早于当前时间仍未完成 |

- 每日/月度任务**按周期自动重置**：每日 00:00、每月 1 日回到待办，月度截止日自动推进到当月（8/31 → 9/30）
- 已完成任务不参与统计；`/tasks` 每条附带 `due_today` / `overdue` / `due_this_month` 瞬态字段

## 权限矩阵（RBAC + 部门隔离）

| 操作 | 超管 | 部门管 | 执行者 |
|------|------|--------|--------|
| 建/改部门、改设置 | ✅ | ❌ | ❌ |
| 建/删任务、班表、人员、Webhook | ✅ | ✅（仅本部门） | ❌ |
| 完成任务 `toggle` | ✅ | ✅（仅本部门） | ✅（仅本部门） |
| 读仪表盘/任务/班表/人员 | ✅ | ✅（仅本部门） | ✅（仅本部门） |
| 看 Webhook 地址 | 明文 | 明文 | 脱敏 `••••` |
| 备份/还原系统 | ✅ | ❌ | ❌ |

要点：所有列表接口按 `dept_id` 过滤，跨部返回 403；部门管理员可见本部门及子孙部门（祖先部门 Webhook 可见子部门任务）。

## 班表

- 整月日历视图（周一起始），每日格子显示班次与当班人员，可查看任意月份
- 支持 Excel（人×日期矩阵模板）/ CSV 导入；**导入时自动核对人员账号，无账号的列出警告（不会收到推送/@）**
- 导入幂等：重复的日期+班次+人员自动跳过
- 今日当班统计与推送 **排除休息班次**

## 系统备份与还原

- 手动备份：`POST /backups`，存为 `swb-backup-YYYY-MM-DD-HHMMSS.db`
- 自动备份：`POST /backup-config` 配置频率/保留份数/异地目录，后台定时执行
- 还原：`POST /backups/:id/restore` 先备份当前库再覆盖（危险操作前端二次确认）

## 已知限制

- 单 SQLite 文件，未做读写分离/水平扩展；高并发写入需换 Postgres（GORM 驱动可平滑切换）
- 班表/推送按姓名匹配人员，同部门内姓名需唯一
- PWA 为「离线开壳 + 在线数据」级别：断网可打开页面，但任务/排班等数据需联网
- 浏览器插件为只读助手，不支持在插件内创建/编辑/完成任务
- 企业微信 @ 依赖「当班 + 在群里 + 填了手机号」三条件，机器人只能 @ 群成员

## 浏览器插件

`extension/`（Manifest V3）只读助手：开发者模式加载即可使用，令牌经网页端桥接页（`/ext-bridge.html`）同步，与网页/PWA 令牌隔离。详见 `extension/README.md`。

## 测试

```bash
# 单元测试（需 Go 1.21+）
cd backend && go test ./...

# 线上端到端（需部署后可访问）
python3 tests/verify_full.py        # 全面体检（33 项）
python3 tests/verify_v0130.py       # 周期重置
python3 tests/verify_v0131.py       # 月度逾期判定
python3 tests/verify_v0140.py       # 班表无账号警告 + 账号=工号
```

## 部署

生产环境以 Docker Compose 方式运行，直接拉取镜像 `daiqiongzhao/gzt:latest` 即可，无需本地构建。
该镜像为 **amd64 + arm64 双架构 manifest list**，同一条 compose 配置可在 x86 服务器与 ARM 设备上通用。

发布流程见 `release.sh`：语义化版本 tag → GitHub → Docker Hub（**同时产出 amd64 + arm64 多架构镜像**）→ 重启容器。每个版本都会留下语义化 tag（如 `v0.15.1`），回滚时把 compose 里的 `latest` 换成对应版本号即可。
