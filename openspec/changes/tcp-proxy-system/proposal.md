# TCP Proxy System

## Summary

构建一个 TCP 代理系统，支持通过 WebSocket/HTTP CONNECT 协议将本地 TCP 端口暴露给远程客户端，并提供 Web 图形化管理界面。系统包含服务端（Docker 部署）和多平台客户端（Go 二进制内嵌前端）。

## Problem Statement

当前需要一种灵活、可视化的 TCP 隧道解决方案，能够：
- 将本地 TCP 服务安全地暴露到公网
- 支持多种传输协议（WebSocket、HTTP CONNECT）
- 提供直观的 Web 管理界面
- 支持 Windows / Linux x64 客户端分发及 Linux 容器镜像（缩短 CI 构建面）
- 配置动态更新，无需重启服务

## Proposed Solution

### 核心架构

1. **后端服务（Server）**
   - Go + Gin 框架
   - YAML 配置存储，支持热重载
   - Alpine Docker 镜像部署（apk 使用阿里云镜像；服务端镜像仅在 CI Runner 编译后打包以利用缓存）
   - 内嵌 React 静态资源

2. **前端界面（UI）**
   - React 单页应用
   - 编译产物嵌入 Go 二进制文件
   - 支持客户端 Windows / Linux x64 二进制与 Linux amd64 Docker 镜像分发

3. **代理功能**
   - TCP 端口监听
   - WebSocket/HTTP CONNECT 协议转换
   - 客户端隧道连接还原

### 技术栈

- **后端**: Go 1.21+, Gin, gorilla/websocket
- **前端**: React 18, TypeScript, Vite
- **配置**: YAML (gopkg.in/yaml.v3)
- **构建**: GitHub Actions, Docker Buildx
- **依赖源**: goproxy.cn, npmmirror.com

## Requirements

### 功能性需求

1. **代理服务**
   - 支持同时运行多个 TCP 代理实例
   - 每个代理可独立配置协议（WebSocket 或 HTTP CONNECT）
   - 实时显示代理状态（连接数、流量统计）

2. **配置管理**
   - YAML 文件持久化存储
   - Web 界面增删改查代理配置
   - 配置变更自动热重载（无需重启）
   - 支持初始化配置导入

3. **客户端支持**
   - 建立与服务端的 WebSocket/HTTP CONNECT 连接
   - 将远程通道还原为本地 TCP 端口
   - 类似 `ssh -L` 的隧道功能

4. **Web 管理界面**
   - 代理列表展示与筛选
   - 代理配置表单（名称、端口、协议、目标地址）
   - 实时状态监控
   - 日志查看

5. **API 规范**
   - RESTful API 设计
   - 统一前缀 `/api/v1`
   - 规范化路由命名

### 非功能性需求

1. **性能**
   - 单实例支持 100+ 并发连接
   - 延迟增加 < 50ms

2. **可靠性**
   - 配置变更原子性操作
   - 异常自动恢复
   - 连接断线重连机制

3. **安全性**
   - 支持 TLS 加密（可选）
   - API 认证机制（基础）

4. **可维护性**
   - 清晰的代码结构
   - 手动依赖注入（禁止 Wire）
   - 完善的错误处理

## Impact

### 新增组件

- `cmd/server/` - 后端服务入口
- `internal/proxy/` - 代理核心逻辑
- `internal/config/` - 配置管理模块
- `internal/api/` - Gin 路由与处理器
- `frontend/web-admin/` - 服务端管理端 React SPA（嵌入 `internal/embed/admin`）
- `frontend/web-client/` - 客户端隧道 Web UI（嵌入 `internal/embed/client`）

### 新增文件

- `Dockerfile` - Alpine 容器镜像
- `.github/workflows/ci.yml` - CI/CD 工作流
- `Makefile` - 构建脚本

### 外部依赖

**Go 依赖**:
- github.com/gin-gonic/gin
- github.com/gorilla/websocket
- gopkg.in/yaml.v3
- github.com/fsnotify/fsnotify（配置热重载）

**NPM 依赖**:
- react, react-dom
- react-router-dom
- axios
- antd（UI 组件库）

## Implementation Strategy

采用分阶段实现：

1. **Phase 1**: 核心代理引擎（TCP 监听 + 协议转换）
2. **Phase 2**: 配置管理与 YAML 热重载
3. **Phase 3**: RESTful API 接口
4. **Phase 4**: React 前端开发
5. **Phase 5**: 前端嵌入与多平台构建
6. **Phase 6**: Docker 镜像与 CI/CD

每阶段完成后进行独立测试验证。
