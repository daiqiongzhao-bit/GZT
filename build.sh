#!/usr/bin/env bash
# 一键构建单二进制：前端构建 -> 后端编译（嵌入前端）-> 产出 swb
set -e
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

echo "==> 1/3 安装前端依赖"
cd frontend && pnpm install && pnpm build
cd "$ROOT"

echo "==> 2/3 编译后端（CGO 关闭，纯 Go SQLite，单文件）"
cd backend && CGO_ENABLED=0 go build -ldflags="-s -w" -o "$ROOT/swb" .
cd "$ROOT"

echo "==> 完成：生成单二进制 ./swb"
echo "    运行：./swb  然后浏览器打开 http://localhost:8080"
echo "    默认账号：admin / admin123"
