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
    DB_PATH=/data/swb.db \
    JWT_SECRET=please-change-me-in-prod-strong \
    AES_KEY=please-change-this-aes-key-2024-now
ENTRYPOINT ["/usr/local/bin/swb"]
