# 多阶段构建：前端 Vite -> Go 单二进制 -> 极简运行镜像
# 构建上下文：项目根目录（shift-workbench/）
#
# 多架构说明（amd64 / arm64）：
#   所有 RUN 阶段都固定在 BUILDPLATFORM（原生 amd64）执行，
#   只有最后一阶段的基础镜像按 TARGETPLATFORM 拉取，
#   因此无需 QEMU 模拟，构建速度与单架构基本一致。

# 阶段 1：构建前端（平台无关产物，始终在原生架构上跑）
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend
WORKDIR /app
RUN npm i -g pnpm@9
COPY frontend ./frontend
COPY backend ./backend
RUN cd frontend && pnpm install && pnpm build

# 注入版本号：sw.js（内容变化触发浏览器更新 SW）+ index.html（应用壳版本自检）。
# 目的：避免 PWA 长期停留在旧应用壳，出现「点了菜单没反应」这类问题。
RUN VERSION=$(grep -oE 'AppVersion: *"v[0-9]+\.[0-9]+\.[0-9]+"' backend/internal/config/config.go | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
    if [ -n "$VERSION" ]; then \
      for f in backend/web/dist/sw.js backend/web/dist/index.html; do \
        [ -f "$f" ] && sed -i "s/__APP_VERSION__/${VERSION}/g" "$f"; \
      done; \
    fi; \
    exit 0

# 阶段 2：编译后端（嵌入前端产物，按目标架构交叉编译）
FROM --platform=$BUILDPLATFORM golang:1.21-alpine AS backend
ARG TARGETARCH
ARG GOPROXY=https://proxy.golang.org,direct
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum ./
RUN GOPROXY=${GOPROXY} go mod download
COPY --from=frontend /app/backend ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /out/app .

# 阶段 3：系统依赖（CA 证书 + 时区数据），在原生架构上准备，供多架构镜像共用
FROM --platform=$BUILDPLATFORM alpine:3.20 AS sysdeps
RUN apk add --no-cache ca-certificates tzdata

# 阶段 4：运行镜像（按目标平台自动匹配 amd64 / arm64）
# 本阶段不含任何 RUN，因此构建 arm64 镜像时无需 QEMU 模拟
FROM alpine:3.20
COPY --from=sysdeps /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=sysdeps /usr/share/zoneinfo /usr/share/zoneinfo
WORKDIR /data
COPY --from=backend /out/app /usr/local/bin/swb
EXPOSE 8080
VOLUME ["/data"]
ENV APP_PORT=8080 \
    DB_PATH=/data/swb.db
# 不在镜像内硬编码 JWT_SECRET / AES_KEY 默认值。
# 运行时可选注入（compose / docker run -e）；未注入时应用会在数据卷 /data/secrets.env
# 首次启动自动生成强随机密钥并持久化，之后每次启动复用（见 config.resolveSecrets）。
# 注意：注入弱默认值、或对已有数据卷更换密钥，会导致应用拒绝启动以保护既有数据。
ENTRYPOINT ["/usr/local/bin/swb"]
