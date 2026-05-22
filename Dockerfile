# 服务端运行时镜像（仅 Alpine + CI 预编译的 server 二进制）。
# npm / go build 请在 CI（或本机构建脚本）完成后再组上下文：
#   internal/embed/admin/dist ← web-admin/build 产物
#   go build → docker-server-bundle/server
#
# 示例：
#   cd frontend/web-admin && npm ci && npm run build && cd ../..
#   rm -rf internal/embed/admin/dist && mkdir -p internal/embed/admin/dist && cp -r frontend/web-admin/dist/. internal/embed/admin/dist/
#   mkdir -p docker-server-bundle && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o docker-server-bundle/server ./cmd/server
#   cp config.yaml docker-server-bundle/
#   docker buildx build --platform linux/amd64 -f Dockerfile ./docker-server-bundle

FROM alpine:latest
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
  && apk --no-cache add ca-certificates tzdata \
    && apk --no-cache  add curl
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY server /app/server
COPY config.yaml /app/config.yaml
RUN chmod +x /app/server
EXPOSE 3000
ENTRYPOINT ["/app/server"]
