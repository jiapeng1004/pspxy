# Stage 1: 构建管理端前端
FROM node:20-alpine AS web-admin-builder
WORKDIR /app/frontend/web-admin
COPY frontend/web-admin/package.json frontend/web-admin/package-lock.json* ./
RUN npm config set registry https://registry.npmmirror.com && (npm ci || npm install)
COPY frontend/web-admin/ ./
RUN npm run build

# Stage 2: 构建 Go 服务端
FROM golang:1.21-alpine AS go-builder
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
COPY --from=web-admin-builder /app/frontend/web-admin/dist ./internal/embed/admin/dist/
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server ./cmd/server

# Stage 3: 精简运行镜像
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=go-builder /app/server .
COPY config.yaml .
EXPOSE 3000
ENTRYPOINT ["/app/server"]
