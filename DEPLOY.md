# 排班任务工作台 · 安装部署手册

> 适用版本：v0.6.0 ｜ 单二进制架构：Go 后端 + Vue 前端合并在一个可执行文件里
> 部署完成后通过 `http://<你的服务器地址>:8080` 访问；默认账号见 README「账号初始化」章节，**首次登录后请立即修改密码**。

## 一、服务器要求

| 项 | 要求 |
|----|------|
| 系统 | Linux（CentOS / Ubuntu / Debian 均可），64 位 |
| 资源 | **最低 1 核 512MB 内存 / 1GB 磁盘**，实际占用极小 |
| Docker 方式 | 需安装 Docker Engine 20.10+（或 Docker Desktop） |
| 二进制方式 | 无需任何运行时依赖（连数据库都是内嵌的） |

---

## 二、方式 A：Docker 部署（推荐，最简单）

### 1. 获取镜像（二选一）

**A1. 直接用现成镜像（最快，无需源码）：**

```bash
docker pull daiqiongzhao/gzt:latest
```

**A2. 或从源码自己构建（可改代码/品牌）：**

```bash
git clone https://github.com/daiqiongzhao-bit/GZT.git
cd GZT
```

### 2. 准备 `.env` 配置文件

在项目目录（或镜像运行目录）新建 `.env`：

```bash
# 服务端口（容器内）
APP_PORT=8080
# JWT 签名密钥：务必改成自己的随机长串（至少 16 位）
JWT_SECRET=改成你的随机密钥-至少16位
# AES 加密密钥：务必改成自己的随机串（建议 32 位）
AES_KEY=改成你的另一个随机密钥-32位
```

> ⚠️ `JWT_SECRET` 和 `AES_KEY` 必须修改，否则他人可伪造令牌 / 解密数据。

### 3. 创建 `docker-compose.yml`

```yaml
services:
  shift-workbench:
    image: daiqiongzhao/gzt:latest        # 用现成镜像；若源码构建则改为 build: .
    container_name: shift-workbench
    ports:
      - "8090:8080"                        # 左边可改成任意对外端口
    environment:
      APP_PORT: "8080"
      DB_PATH: "/data/swb.db"
      JWT_SECRET: "${JWT_SECRET}"
      AES_KEY: "${AES_KEY}"
    volumes:
      - /opt/swb/data:/data                # 数据持久化目录（含数据库与备份）
    restart: unless-stopped
```

### 4. 启动

```bash
docker compose up -d
```

### 5. 完成

浏览器访问 `http://你的服务器IP:8090`，用默认账号登录，然后**立即修改密码**。

---

## 三、方式 B：单二进制部署（轻量 / 无 Docker 环境）

### 1. 获取二进制

- 从构建机编译：`cd backend && CGO_ENABLED=0 go build -ldflags="-s -w" -o swb .`
- 或直接向维护者索取编译好的 `swb` 文件（跨平台 x64）。

### 2. 运行

```bash
# 一次性运行
APP_PORT=8090 \
DB_PATH=/data/swb.db \
JWT_SECRET=你的随机密钥 \
AES_KEY=你的随机密钥 \
./swb
```

### 3. 用 systemd 守护（推荐，开机自启 + 崩溃自动拉起）

创建 `/etc/systemd/system/swb.service`：

```ini
[Unit]
Description=Shift Workbench
After=network.target

[Service]
WorkingDirectory=/opt/swb
Environment=APP_PORT=8090
Environment=DB_PATH=/data/swb.db
Environment=JWT_SECRET=你的随机密钥
Environment=AES_KEY=你的随机密钥
ExecStart=/opt/swb/swb
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

```bash
systemctl daemon-reload
systemctl enable --now swb
```

---

## 四、环境变量说明

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `APP_PORT` | 8080 | 监听端口（Docker 内固定 8080，对外用映射端口） |
| `DB_PATH` | `swb.db` | SQLite 数据库文件路径 |
| `JWT_SECRET` | 内置默认 | 令牌签名密钥，**生产必须修改** |
| `AES_KEY` | 内置默认 | Webhook 地址 / SMTP 密码等加密密钥，**生产必须修改** |
| `BACKUP_DIR` | `backups` | 自动备份目录（Docker 下为 `/data/backups`） |

---

## 五、首次登录与安全

1. 默认账号：`admin / admin123`，登录后立即在 **设置 → 个人信息** 改密码。
2. 在 **设置 → 人员管理** 创建各部门账号（普通员工只看得见本部门数据）。
3. 建议在 **设置 → 备份** 开启自动备份，并配置 **WebDAV 异地备份**（坚果云等）。
4. 生产环境建议用 Nginx / Caddy 反代到 443 端口（HTTPS），插件与浏览器通知更安全。

---

## 六、升级

- **Docker 方式**：
  ```bash
  docker compose pull          # 拉新镜像（或源码方式重新构建）
  docker compose up -d         # 重启即完成，数据不丢
  ```
- **二进制方式**：停服务 → 替换 `swb` 文件 → 启动，数据库自动迁移，无需人工操作。

---

## 七、备份与恢复

- 备份即**整库快照**（账号/任务/班表/日志/历史全部在内），可在 **设置 → 备份** 手动备份、下载、还原。
- 还原会覆盖当前数据，操作前请确认。
- 支持 **WebDAV 异地备份**（坚果云 / Nextcloud / 群晖）。

---

## 八、常见问题

| 问题 | 解决 |
|------|------|
| 端口被占用 | 改 docker-compose 左侧端口或 `APP_PORT` |
| 登录提示"账号锁定" | 等 15 分钟自动解除；或超管在 **设置→人员** 点「解锁登录」 |
| 忘记管理员密码 | 见下文"重置管理员密码" |
| 数据目录权限 | Docker 挂载目录需可写：`chmod -R 755 /opt/swb/data` |

**重置管理员密码（忘了密码时）**：停服务 → 用 `sqlite3 /data/swb.db "UPDATE users SET password_hash='$2a$10$..."` 手工写入新哈希（或找维护者协助），一般生产建议直接**用备份还原**更省事。

---

## 九、功能速览（v0.6.0）

- 班表 CSV 导入/导出、任务管理（每日/月度/单次）、负责人与班次归属
- 模板下载、任务批量操作、操作审计日志（含 IP/UA）、多渠道通知（企业微信/钉钉/飞书/邮件）
- 登录限流、超管手动解锁、在线用户监控、强制下线
- 任务到点桌面提醒（浏览器插件）、PWA 离线
- 自动备份 + WebDAV 异地备份
