# syntax=docker/dockerfile:1.6
# ============================================================
# GameOps Agent — 多阶段构建
# Stage 1: builder       使用官方 Go 1.24 镜像编译静态二进制
# Stage 2: runtime       使用 distroless 最小镜像运行（仅 ~25MB）
# ============================================================

FROM golang:1.24-alpine AS builder

# 所有依赖均为公开库，无需内网 GOPROXY
ARG GOPROXY=https://goproxy.cn,direct
ARG GOPRIVATE=""
ENV GOPROXY=${GOPROXY} \
    GOPRIVATE=${GOPRIVATE} \
    CGO_ENABLED=0 \
    GO111MODULE=on

WORKDIR /build

# 先拷依赖文件以利用层缓存
COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# 拷贝全部源码
COPY . .

# 编译（默认启用所有功能 tag）
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME
ARG BUILD_TAGS="a2a agui iwiki redis"
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath \
      -tags "${BUILD_TAGS}" \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
      -o /out/gameops-agent .

# ------------------------------------------------------------
# Runtime
# ------------------------------------------------------------
FROM alpine:latest

LABEL org.opencontainers.image.title="gameops-agent" \
      org.opencontainers.image.source="https://git.woa.com/trpc-go/gameops-agent" \
      org.opencontainers.image.licenses="Apache-2.0"

RUN addgroup -S nonroot && adduser -S nonroot -G nonroot

WORKDIR /app
COPY --from=builder /out/gameops-agent /app/gameops-agent
COPY --from=builder /build/mcp_servers.yaml /app/mcp_servers.yaml
COPY --from=builder /build/skills /app/skills
COPY --from=builder /build/deploy/guard_rules.yaml /app/deploy/guard_rules.yaml

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/app/gameops-agent"]
CMD ["-addr=:8080"]
