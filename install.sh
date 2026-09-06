#!/bin/bash
# ============================================================
# 企业任务通知管理（GZT）v0.4.0 一键安装脚本
#
# 用法（任选其一，均不依赖任何个人服务器）：
#   方式A（推荐）— 服务器能联网，直接远程执行，无需下载文件（脚本托管于
#     GitHub 公开仓库 + 全球 CDN 镜像，与作者个人服务器/域名无关）：
#     bash <(curl -sL https://cdn.jsdelivr.net/gh/daiqiongzhao-bit/GZT@main/install.sh)
#   方式B — 下载到本地再执行：
#     curl -sL https://cdn.jsdelivr.net/gh/daiqiongzhao-bit/GZT@main/install.sh -o gzt-install.sh && bash gzt-install.sh
#
# 自动完成：装 Docker → 生成密钥 → 拉镜像 → 启动 → 打印网址
# 支持系统：Ubuntu / Debian / CentOS 7+（64 位）
# ============================================================
set -e

echo "=============================================="
echo "  企业任务通知管理 (GZT) 一键安装"
echo "=============================================="

# ---------- 1. 检测 root ----------
if [ "$(id -u)" != "0" ]; then
  echo "✗ 请用 root 执行（sudo -i 或 sudo bash 该脚本）"
  exit 1
fi

# ---------- 2. 检测并安装 Docker ----------
need_install=0
if ! command -v docker >/dev/null 2>&1; then
  need_install=1
fi
if ! docker compose version >/dev/null 2>&1 && ! docker-compose version >/dev/null 2>&1; then
  need_install=1
fi

if [ "$need_install" = "1" ]; then
  echo "==> 检测到未安装 Docker，正在自动安装（约需 1-3 分钟）..."
  . /etc/os-release 2>/dev/null || ID="unknown"
  case "$ID" in
    ubuntu|debian)
      export DEBIAN_FRONTEND=noninteractive
      apt-get update -y >/dev/null 2>&1 || true
      apt-get install -y ca-certificates curl gnupg >/dev/null 2>&1
      install -m 0755 -d /etc/apt/keyrings
      curl -fsSL https://download.docker.com/linux/$ID/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg >/dev/null 2>&1
      echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/$ID $(. /etc/os-release && echo $VERSION_CODENAME) stable" > /etc/apt/sources.list.d/docker.list
      apt-get update -y >/dev/null 2>&1 || true
      apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin >/dev/null 2>&1
      ;;
    centos|rhel|rocky|almalinux|fedora)
      yum install -y yum-utils >/dev/null 2>&1
      yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo >/dev/null 2>&1
      yum install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin >/dev/null 2>&1
      ;;
    *)
      echo "✗ 无法识别的系统，请手动安装 Docker 后重跑本脚本"
      echo "   参考: https://docs.docker.com/engine/install/"
      exit 1
      ;;
  esac
  systemctl enable --now docker >/dev/null 2>&1 || true
  echo "    ✓ Docker 安装完成"
fi

# 兼容 compose 命令（旧版用 docker-compose）
COMPOSE="docker compose"
if ! docker compose version >/dev/null 2>&1; then
  if docker-compose version >/dev/null 2>&1; then COMPOSE="docker-compose"; fi
fi

# ---------- 3. 生成密钥并准备配置 ----------
DIR=/opt/gzt
mkdir -p "$DIR"
cd "$DIR"

JSEC=$(openssl rand -hex 24 2>/dev/null || head -c 40 /dev/urandom | tr -dc 'a-zA-Z0-9')
AKEY=$(openssl rand -hex 24 2>/dev/null || head -c 40 /dev/urandom | tr -dc 'a-zA-Z0-9')

cat > .env <<EOF
JWT_SECRET=$JSEC
AES_KEY=$AKEY
EOF

cat > docker-compose.yml <<'EOF'
services:
  shift-workbench:
    image: daiqiongzhao/gzt:latest
    container_name: shift-workbench
    ports:
      - "8090:8080"
    environment:
      APP_PORT: "8080"
      DB_PATH: "/data/swb.db"
      JWT_SECRET: "${JWT_SECRET}"
      AES_KEY: "${AES_KEY}"
      TZ: "Asia/Shanghai"
    volumes:
      - /opt/swb/data:/data
    restart: unless-stopped
EOF

echo "==> 密钥已自动生成（保存在 /opt/gzt/.env，无需你管理）"

# ---------- 4. 拉镜像并启动 ----------
echo "==> 拉取镜像并启动..."
$COMPOSE up -d 2>/dev/null || { $COMPOSE pull && $COMPOSE up -d; }

# ---------- 5. 等待就绪并打印结果 ----------
sleep 4
IP=$(curl -s --max-time 3 ifconfig.me 2>/dev/null || hostname -I 2>/dev/null | awk '{print $1}' || echo "你的服务器IP")

echo
echo "======================================================"
echo "  ✓ 安装完成！"
echo
echo "  访问地址 : http://$IP:8090"
echo
echo "  初始账号 : admin"
echo "  初始密码 : admin123"
echo
echo "  ※ 首次登录系统会强制你设置一个新密码，请务必设置"
echo "    并记住，用于后续登录。"
echo
echo "  数据目录 : /opt/swb/data（删除容器不丢数据）"
echo "  升级命令 : cd /opt/gzt && $COMPOSE pull && $COMPOSE up -d"
echo "======================================================"
