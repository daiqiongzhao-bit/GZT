#!/usr/bin/env bash
# 一键构建单二进制：前端构建 -> 后端编译（嵌入前端）-> 产出 swb
set -e
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

# 打包浏览器插件（保证嵌入二进制的 extension.zip 与 extension/ 源码一致）。
# 优先 python3，回退 python；二者皆无则明确报错退出——绝不静默跳过。
PYTHON="$(command -v python3 || command -v python || true)"
if [ -z "$PYTHON" ]; then
  echo "✗ 找不到 python3 / python，无法打包浏览器插件（拒绝静默跳过）"
  exit 1
fi
echo "==> 0/3 打包浏览器插件"
"$PYTHON" tools/pack_extension.py

echo "==> 1/3 安装前端依赖"
cd frontend && pnpm install && pnpm build
cd "$ROOT"

# 注入版本号：sw.js（触发 SW 更新）+ index.html（应用壳版本自检）
VERSION="$(grep -oE 'AppVersion: *"v[0-9]+\.[0-9]+\.[0-9]+"' backend/internal/config/config.go | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
if [ -n "$VERSION" ]; then
  for f in backend/web/dist/sw.js backend/web/dist/index.html; do
    if [ -f "$f" ]; then
      sed -i.bak "s/__APP_VERSION__/${VERSION}/g" "$f" && rm -f "$f.bak"
    fi
  done
  echo "    已注入版本 ${VERSION} -> sw.js / index.html"
fi

echo "==> 2/3 编译后端（CGO 关闭，纯 Go SQLite，单文件）"
cd backend && CGO_ENABLED=0 go build -ldflags="-s -w" -o "$ROOT/swb" .
cd "$ROOT"

echo "==> 完成：生成单二进制 ./swb"
echo "    运行：./swb  然后浏览器打开 http://localhost:8080"
echo "    默认账号：admin / admin123"
