# Tasks: TCP Proxy System Implementation

## Phase 1: Project Setup

- [x] 1.1 初始化 Go 模块
  - [x] 创建 `go.mod`，设置 Go 1.21
  - [x] 添加依赖：gin, gorilla/websocket, yaml.v3, fsnotify, uuid
  - [x] 配置国内镜像源（goproxy.cn）

- [x] 1.2 创建项目目录结构
  - [x] `cmd/server/main.go`
  - [x] `cmd/client/main.go`
  - [x] `internal/config/`
  - [x] `internal/proxy/`
  - [x] `internal/api/`
  - [x] `internal/embed/`
  - [x] `frontend/` 下的 `web-admin/`、`web-client/`（mono repo）

- [x] 1.3 初始化 React 项目
  - [x] 使用 Vite 创建 TypeScript 项目
  - [x] 配置 Ant Design
  - [x] 设置路由（react-router-dom）
  - [x] 配置国内镜像源（npmmirror.com）

## Phase 2: Configuration Management

- [x] 2.1 实现 Config 结构体
  - [x] 定义 `ProxyConfig` 和 `ServerConfig`
  - [x] 实现 YAML 序列化/反序列化

- [x] 2.2 实现配置加载
  - [x] 从 `config.yaml` 读取配置
  - [x] 首次启动时创建默认配置
  - [x] 配置验证逻辑（端口范围、协议类型）

- [x] 2.3 实现热重载
  - [x] 集成 fsnotify 监听文件变更
  - [x] 实现配置 diff 逻辑
  - [x] 原子写入机制（临时文件 + rename）
  - [x] 错误回滚机制

## Phase 3: Proxy Core Engine

- [x] 3.1 实现 Proxy 接口
  - [x] 定义 `Proxy` 接口（Start/Stop/Status）
  - [x] 实现 `TCPServer` 结构体

- [x] 3.2 实现 WebSocket Handler
  - [x] WebSocket 升级逻辑
  - [x] 双向数据转发（io.Copy）
  - [x] 连接池管理

- [x] 3.3 实现 HTTP CONNECT Handler
  - [x] Hijack 连接
  - [x] 返回 200 Connection Established
  - [x] 透明数据转发

- [x] 3.4 实现 ProxyManager
  - [x] 代理生命周期管理（Add/Remove/Update）
  - [x] 状态追踪（运行中、已停止、错误）
  - [x] 并发控制（sync.RWMutex）

## Phase 4: RESTful API

- [x] 4.1 实现 Gin 路由
  - [x] 注册所有 API 端点
  - [x] CORS 中间件
  - [x] 错误处理中间件

- [x] 4.2 实现 Proxy CRUD 端点
  - [x] `GET /api/v1/proxies` - 列表查询
  - [x] `POST /api/v1/proxies` - 创建代理
  - [x] `GET /api/v1/proxies/:id` - 详情查询
  - [x] `PUT /api/v1/proxies/:id` - 更新代理
  - [x] `DELETE /api/v1/proxies/:id` - 删除代理

- [x] 4.3 实现配置管理端点
  - [x] `POST /api/v1/config/reload` - 手动重载
  - [x] `GET /api/v1/health` - 健康检查

- [x] 4.4 实现 WebSocket 端点
  - [x] `WS /ws/:proxy_id` - WebSocket 隧道

- [x] 4.5 参数验证与错误处理
  - [x] 端口范围验证
  - [x] 协议类型验证
  - [x] 统一错误响应格式

## Phase 5: React Frontend

- [x] 5.1 创建代理列表页面
  - [x] 表格展示所有代理
  - [x] 状态指示器（运行/停止）
  - [x] 筛选功能（按协议、启用状态）

- [x] 5.2 创建代理编辑表单
  - [x] 名称输入框
  - [x] 端口输入框（数字验证）
  - [x] 协议选择器（下拉菜单）
  - [x] 目标地址输入框
  - [x] 启用/禁用开关

- [x] 5.3 集成 API 调用
  - [x] 封装 axios 客户端
  - [x] 实现 CRUD 操作
  - [x] 错误提示（antd message）

- [x] 5.4 实时监控面板
  - [x] 连接数显示
  - [x] 运行时长显示
  - [x] 自动刷新（每 5 秒）

## Phase 6: Embedding & Build

- [x] 6.1 前端构建优化
  - [x] 配置 Vite 输出到 `dist/`
  - [x] 启用代码分割
  - [x] 压缩静态资源

- [x] 6.2 Go embed 集成
  - [x] 使用 `//go:embed dist/*` 嵌入前端资源
  - [x] 实现静态文件服务路由
  - [x] SPA 路由回退（/* -> index.html）

- [x] 6.3 多平台构建脚本
  - [x] 编写 Makefile
  - [x] 交叉编译配置（GOOS/GOARCH）
  - [x] 生成 4 个平台二进制文件

## Phase 7: Docker & CI/CD

- [x] 7.1 创建 Dockerfile
  - [x] 基于 alpine:latest
  - [x] 安装 CA 证书
  - [x] 复制 Go 二进制和静态资源
  - [x] 暴露端口 3000

- [x] 7.2 配置 GitHub Actions
  - [x] 触发条件：develop 分支推送、tag 推送
  - [x] Job 1: 构建 Docker 镜像
  - [x] Job 2: 多平台二进制构建
  - [x] 上传 artifacts

- [x] 7.3 Docker Buildx 配置
  - [x] 多架构构建（linux/amd64, linux/arm64）
  - [x] 推送到 Docker Hub

## Phase 8: Testing & Documentation

- [ ] 8.1 单元测试
  - [ ] Config 模块测试
  - [ ] Proxy 模块测试
  - [ ] API Handler 测试

- [ ] 8.2 集成测试
  - [ ] WebSocket 隧道端到端测试
  - [ ] HTTP CONNECT 隧道测试
  - [ ] 配置热重载测试

- [ ] 8.3 文档编写
  - [ ] README.md（项目介绍、快速开始）
  - [ ] API 文档（Swagger/OpenAPI）
  - [ ] 配置示例文件

- [ ] 8.4 性能测试
  - [ ] 并发连接压测
  - [ ] 延迟测试
  - [ ] 内存泄漏检测
