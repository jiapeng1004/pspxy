# Specification: Client Tunnel

## ADDED Requirements

### Requirement: Client Connection
客户端 SHALL 能够连接到服务端的 WebSocket 或 HTTP CONNECT 端点。

#### Scenario: WebSocket 客户端连接
- **WHEN** 客户端发起 WebSocket 连接到 `ws://server:port/ws/:proxy_id`
- **THEN** 建立加密或非加密连接
- **AND** 发送认证信息（如需要）
- **AND** 进入数据传输模式

#### Scenario: HTTP CONNECT 客户端连接
- **WHEN** 客户端发送 CONNECT 请求到 `server:port`
- **THEN** 服务端建立隧道
- **AND** 客户端获得透明的 TCP 连接

### Requirement: Local TCP Port Forwarding
客户端 SHALL 将远程通道映射为本地 TCP 端口。

#### Scenario: 启动本地监听
- **WHEN** 客户端配置本地端口 3306 转发到远程数据库
- **THEN** 客户端在 `127.0.0.1:3306` 监听
- **AND** 接受本地应用连接
- **AND** 通过远程通道转发数据

#### Scenario: 数据双向转发
- **WHEN** 本地应用发送数据到 `127.0.0.1:3306`
- **THEN** 客户端通过 WebSocket/CONNECT 隧道发送数据到服务端
- **AND** 服务端转发数据到实际的目标地址
- **AND** 响应数据沿相反路径返回

### Requirement: Reconnection Logic
客户端 SHALL 在连接断开时自动重连。

#### Scenario: 连接丢失
- **WHEN** 与服务端的连接意外中断
- **THEN** 客户端等待指数退避时间（1s, 2s, 4s, 8s...）
- **AND** 尝试重新连接
- **AND** 最多重试 10 次
- **AND** 每次重连失败记录日志

#### Scenario: 重连成功
- **WHEN** 客户端成功重新连接
- **THEN** 恢复本地端口监听
- **AND** 记录重连成功日志
- **AND** 通知用户（如系统托盘提示）

### Requirement: Multi-Platform Binary
客户端 SHALL 提供多平台独立二进制文件。

#### Scenario: 平台支持
- **WHEN** 构建客户端
- **THEN** 生成以下平台的二进制文件：
  - Windows x86_64 (`tcp-proxy-client-windows-amd64.exe`)
  - macOS x86_64 (`tcp-proxy-client-darwin-amd64`)
  - macOS ARM64 (`tcp-proxy-client-darwin-arm64`)
  - Linux x86_64 (`tcp-proxy-client-linux-amd64`)
- **AND** 每个二进制文件内嵌 React 前端资源
