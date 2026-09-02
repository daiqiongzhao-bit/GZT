#!/usr/bin/env bash
# GZT（工作台）一键发布脚本
#
# 用法：
#   ./release.sh patch    # bug 修复        v0.6.0 -> v0.6.1
#   ./release.sh minor    # 新增功能迭代    v0.6.0 -> v0.7.0
#   ./release.sh major    # 正式稳定版      v0.6.0 -> v1.0.0
#   ./release.sh minor --deploy   # 构建推送后顺便重启线上容器
#
# 流程：校验工作区 -> 计算版本号 -> 打 tag -> 推送 GitHub
#       -> 构建镜像 -> 推送 Docker Hub -> （可选）重启容器
set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

IMAGE="daiqiongzhao/gzt"
CONTAINER="shift-workbench"
BRANCH="main"

# 线上部署所用的环境变量文件（JWT_SECRET / AES_KEY）。
# 不加载会导致容器回退到代码内默认密钥：全员被登出 + AES 加密数据解不开。
# 优先级：脚本同目录 .env  >  部署目录 /opt/swb/swb.env
ENV_FILE=""
if [ -f "$ROOT/.env" ]; then
  ENV_FILE="$ROOT/.env"
elif [ -f /opt/swb/swb.env ]; then
  ENV_FILE="/opt/swb/swb.env"
fi
if [ -z "$ENV_FILE" ]; then
  echo "✗ 找不到环境变量文件（./.env 或 /opt/swb/swb.env），拒绝发布以免密钥回退为默认值"
  exit 1
fi
if ! grep -qE '^[[:space:]]*(JWT_SECRET|AES_KEY)=.+' "$ENV_FILE"; then
  echo "✗ $ENV_FILE 中缺少 JWT_SECRET / AES_KEY，拒绝发布"
  exit 1
fi
echo "    环境变量文件: $ENV_FILE"

# ---------- 参数解析 ----------
BUMP="${1:-}"
DEPLOY=false
[ "${2:-}" = "--deploy" ] && DEPLOY=true

if [[ ! "$BUMP" =~ ^(patch|minor|major)$ ]]; then
  echo "用法: ./release.sh {patch|minor|major} [--deploy]"
  echo "  patch  bug 修复小版本   (v0.6.0 -> v0.6.1)"
  echo "  minor  新增功能迭代     (v0.6.0 -> v0.7.0)"
  echo "  major  正式稳定版       (v0.6.0 -> v1.0.0)"
  exit 1
fi

# ---------- 1. 前置检查 ----------
echo "==> 前置检查"
if [ -n "$(git status --porcelain)" ]; then
  echo "✗ 工作区不干净，请先提交或暂存所有改动："
  git status --short
  exit 1
fi

CURRENT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"
if [ "$CURRENT_BRANCH" != "$BRANCH" ]; then
  echo "✗ 当前分支是 $CURRENT_BRANCH，发布必须在 $BRANCH 分支"
  exit 1
fi

# 拉取最新，避免基于陈旧代码发版
git fetch origin "$BRANCH" --quiet 2>/dev/null || true
BEHIND="$(git rev-list --count HEAD..origin/$BRANCH 2>/dev/null || echo 0)"
if [ "$BEHIND" -gt 0 ]; then
  echo "✗ 本地落后远端 $BEHIND 个提交，请先 git pull"
  exit 1
fi

# ---------- 2. 计算新版本号 ----------
# 取最新的 vX.Y.Z 形式 tag
CURRENT="$(git tag -l 'v*.*.*' | sed 's/^v//' | sort -t. -k1,1n -k2,2n -k3,3n | tail -1)"
[ -z "$CURRENT" ] && CURRENT="0.0.0"

IFS='.' read -r MA MI PA <<< "$CURRENT"
case "$BUMP" in
  patch) PA=$((PA + 1)) ;;
  minor) MI=$((MI + 1)); PA=0 ;;
  major) MA=$((MA + 1)); MI=0; PA=0 ;;
esac
NEW="v${MA}.${MI}.${PA}"

echo "    当前版本: v${CURRENT}"
echo "    新版本号: ${NEW}  (${BUMP})"

# 校验 tag 不存在
if git rev-parse "$NEW" >/dev/null 2>&1; then
  echo "✗ tag $NEW 已存在"
  exit 1
fi

# ---------- 3. 确认 ----------
LAST_MSG="$(git log -1 --pretty=%s)"
echo
echo "    将基于提交 $(git rev-parse --short HEAD) 发版"
echo "    提交说明: $LAST_MSG"
[ "$DEPLOY" = true ] && echo "    ⚠ 加了 --deploy，发布后会重启容器 $CONTAINER，线上会短暂中断"
echo
read -r -p "确认发布 $NEW？[y/N] " CONFIRM
[[ ! "$CONFIRM" =~ ^[Yy]$ ]] && { echo "已取消"; exit 0; }

# ---------- 4. 打 tag 并推送 GitHub ----------
echo
echo "==> 1/4 打 tag ${NEW}"
git tag -a "$NEW" -m "${NEW}: ${LAST_MSG}"

echo "==> 2/4 推送 GitHub（含 tag）"
git push origin "$BRANCH"
git push origin "$NEW"

# ---------- 5. 构建并推送多架构 Docker 镜像 ----------
# 同时产出 linux/amd64 + linux/arm64：
#   前端构建与 Go 编译固定在 BUILDPLATFORM（原生架构）执行，
#   仅最终运行镜像按目标平台打包，因此无需 QEMU，耗时与单架构相当。
# 注意：多平台构建无法写入本地镜像库，必须直接 --push，
#       所以这里一步完成"构建 + 推送"，不再单独 docker push。
echo "==> 3/4 构建并推送多架构镜像 ${IMAGE}:${NEW}（linux/amd64 + linux/arm64）"
docker buildx build --platform linux/amd64,linux/arm64 \
  --build-arg GOPROXY=https://goproxy.cn,direct \
  -t "${IMAGE}:${NEW}" -t "${IMAGE}:latest" --push .

# ---------- 6. 可选：重启线上容器 ----------
echo
if [ "$DEPLOY" = true ]; then
  echo "==> 重启容器 ${CONTAINER}"
  docker compose --env-file "$ENV_FILE" pull || true
  docker compose --env-file "$ENV_FILE" up -d
  sleep 5
  docker ps --filter "name=${CONTAINER}" --format '{{.Names}} | {{.Status}} | {{.Ports}}'
  # 部署后自检：密钥必须注入成功，否则全员令牌失效、AES 数据解不开
  INJECTED="$(docker inspect "$CONTAINER" --format '{{range .Config.Env}}{{println .}}{{end}}' | grep -cE '^JWT_SECRET=.+' || true)"
  if [ "$INJECTED" -lt 1 ]; then
    echo
    echo "✗ 严重：容器未注入 JWT_SECRET，正在用默认密钥运行！"
    echo "  请手动执行：cd $ROOT && docker compose --env-file $ENV_FILE up -d"
    exit 1
  fi
  echo "    ✓ 密钥注入校验通过"
else
  echo "提示：镜像已推送，但线上容器仍在运行旧版本。"
  echo "      需要生效时执行："
  echo "        cd $ROOT && docker compose --env-file $ENV_FILE pull && docker compose --env-file $ENV_FILE up -d"
  echo "      （或在下次发版时加 --deploy 参数自动完成）"
fi

echo
echo "✓ 发布完成：${NEW}"
echo "  GitHub    : https://github.com/daiqiongzhao-bit/GZT/releases/tag/${NEW}"
echo "  DockerHub : https://hub.docker.com/r/${IMAGE}/tags"
