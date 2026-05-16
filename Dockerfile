FROM alpine:3.21

# 使用阿里云镜像加速 apk
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories \
    && apk add --no-cache ca-certificates tzdata wget

# 创建非 root 用户
RUN addgroup -S app && adduser -S app -G app

WORKDIR /app
COPY opencode-openai-proxy-docker ./opencode-openai-proxy
COPY .env.docker.example /app/.env

# 使用非 root 用户运行
USER app

EXPOSE 8082

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8082/health || exit 1

ENTRYPOINT ["/app/opencode-openai-proxy"]
