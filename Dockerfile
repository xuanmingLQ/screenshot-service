# 构建阶段
FROM golang:1.25-alpine AS builder

WORKDIR /app

# 安装构建依赖
RUN apk add --no-cache git ca-certificates

# 1. 创建 go.mod (基础版本)
RUN echo 'module screenshot-service' > go.mod && \
    echo '' >> go.mod && \
    echo 'go 1.25' >> go.mod

# 2. 复制源代码
COPY *.go ./

# 3. 强制更新依赖到最新版：先执行 go get -u 拉取最新库，再执行 tidy，为了解决 unknown IPAddressSpace 报错
RUN go get -u github.com/chromedp/chromedp@latest && \
    go get -u github.com/chromedp/cdproto@latest && \
    go mod tidy

# 打印一下版本，确保构建时能看到 (调试用)
RUN cat go.mod

# 构建
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags="-w -s" -o /screenshot-service .

# 运行阶段
# 使用 latest 或 edge 以匹配最新的 Chrome 特性
FROM alpine:latest

# 安装 Chromium 和必要的字体
RUN apk add --no-cache \
    chromium \
    nss \
    freetype \
    harfbuzz \
    ca-certificates \
    ttf-freefont \
    font-noto-cjk \
    font-noto-emoji \
    dumb-init \
    && rm -rf /var/cache/apk/*

# 设置 Chrome 环境变量
ENV CHROME_BIN=/usr/bin/chromium-browser \
    CHROME_PATH=/usr/lib/chromium/ \
    CHROMIUM_FLAGS="--disable-software-rasterizer --disable-dev-shm-usage"

# 创建非 root 用户
RUN addgroup -g 1000 appgroup && \
    adduser -u 1000 -G appgroup -s /bin/sh -D appuser

# 创建必要的目录
RUN mkdir -p /tmp/chrome-data && \
    chown -R appuser:appgroup /tmp/chrome-data

WORKDIR /app

# 复制二进制文件
COPY --from=builder /screenshot-service .

# 设置权限
RUN chown -R appuser:appgroup /app

# 切换用户
USER appuser

# 暴露端口
EXPOSE 8080

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# 使用 dumb-init 启动
ENTRYPOINT ["/usr/bin/dumb-init", "--"]
CMD ["./screenshot-service"]