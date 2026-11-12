# syntax=docker/dockerfile:1
# Ferry 镜像：Node 构建管理界面 → Go 编译（内嵌界面，关闭 CGO）→ distroless 运行。
# 前两段在构建机的原生架构上跑，Go 交叉编译出目标架构，多架构构建不用模拟整个 npm 与 go 工具链。
# 版本号由 --build-arg VERSION 传入；提交号与构建时间读仓库根目录的 .build-commit、.build-date（发布流程写入），
# 没有时分别记为 unknown 和构建时刻。

FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN commit=$(cat .build-commit 2>/dev/null || echo unknown) \
 && date=$(cat .build-date 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ) \
 && pkg=github.com/lonelyman0108/ferry/internal/buildinfo \
 && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -tags embed_ui \
      -ldflags "-s -w -X $pkg.Version=$VERSION -X $pkg.Commit=$commit -X $pkg.Date=$date" \
      -o /out/ferry ./cmd/ferry \
 && mkdir -p /out/data

# distroless：自带 CA 证书和时区数据，没有 shell；以 nonroot（65532）运行。
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ferry /usr/local/bin/ferry
COPY --from=build --chown=65532:65532 /out/data /data
# 数据都在 /data：配置文件 /data/ferry.yaml；向导里 SQLite 填相对路径时也落在这里。
WORKDIR /data
ENV FERRY_CONFIG=/data/ferry.yaml \
    FERRY_CONTAINER=docker
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/ferry", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/ferry"]
CMD ["serve"]
