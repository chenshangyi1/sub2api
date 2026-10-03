# 一体化镜像：前端 build → 嵌入 Go 二进制 → alpine 运行时
#
# 三阶段构建：
#   1) frontend-builder — node + pnpm 跑 `vite build`，产物在 /web/dist
#   2) go-builder       — 把 dist 拷到 web/dist，go build 通过 //go:embed 嵌入二进制
#   3) runtime          — 极小 alpine 镜像只放一个静态二进制
#
# 由于第二阶段需要 frontend 产物，构建 context 必须是 repo 根目录：
#   docker build -t upstream-ops:dev .
#   或在 docker-compose 里写 context: .

# ---------- Stage 1: 前端 ----------
FROM node:20-alpine AS frontend-builder
WORKDIR /web

# pnpm-workspace.yaml 用了 allowBuilds: 这种 pnpm 10.4+ 才支持的字段，
# corepack 默认的 pnpm shim 版本可能太旧，显式 pin 一个已知兼容版本。
RUN corepack enable && corepack prepare pnpm@10.4.0 --activate

# 先拷依赖清单走缓存层
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
# Lockfile is the dependency source of truth; fail the build on manifest drift.
RUN pnpm install --frozen-lockfile

# 再拷源码，build 产物在 /web/dist
COPY frontend/ ./
RUN pnpm build

# ---------- Stage 2: extension worker ----------
FROM node:20-alpine AS worker-builder
WORKDIR /worker

RUN apk add --no-cache openssl

COPY worker/package.json worker/package-lock.json ./
RUN npm ci

COPY worker/ ./
RUN npm run build && npm prune --omit=dev

# ---------- Stage 2: 后端 ----------
FROM golang:1.23-alpine AS go-builder
WORKDIR /src

ARG GOPROXY=https://proxy.golang.org,direct

# 先 go.mod / go.sum 走缓存
COPY go.mod go.sum ./
RUN GOPROXY="${GOPROXY}" go mod download

# 然后整份源码
COPY . ./

# 关键：把前端 dist 替换掉占位的 web/dist，让 //go:embed 抓到真东西
RUN rm -rf ./web/dist
COPY --from=frontend-builder /web/dist ./web/dist

RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/upstream-ops \
        ./cmd/server

RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/upstream-ops-migrate \
        ./cmd/migrate

# ---------- Stage 3: 运行时 ----------
FROM node:20-alpine
ARG BUILD_DATE=""
ARG VCS_REF=""
ARG SOURCE_SHA256=""
ARG BINARY_SHA256=""
ARG TASK_ID=""
LABEL org.opencontainers.image.title="sub2api operations panel" \
      org.opencontainers.image.version="0.0.7" \
      org.opencontainers.image.source="https://github.com/bejix/upstream-ops" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      io.sub2api.source-sha256="${SOURCE_SHA256}" \
      io.sub2api.binary-sha256="${BINARY_SHA256}" \
      io.sub2api.task-id="${TASK_ID}"
RUN apk add --no-cache ca-certificates openssl tzdata wget docker-cli docker-cli-compose && \
    mkdir -p /app/data /app/worker
WORKDIR /app
COPY --from=go-builder /out/upstream-ops /usr/local/bin/upstream-ops
COPY --from=go-builder /out/upstream-ops-migrate /usr/local/bin/upstream-ops-migrate
COPY --from=worker-builder /worker/package.json /app/worker/package.json
COPY --from=worker-builder /worker/node_modules /app/worker/node_modules
COPY --from=worker-builder /worker/dist /app/worker/dist
COPY operations/docker-entrypoint.sh /usr/local/bin/ops-entrypoint
RUN chmod 0755 /usr/local/bin/ops-entrypoint
EXPOSE 8418
ENTRYPOINT ["ops-entrypoint"]
CMD ["panel"]
