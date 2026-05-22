# Specification: Proxy Core Engine

## ADDED Requirements

### Requirement: Unified Server Ingress
系统 SHALL 在**单一服务端端口**（`config.server.port`）上承载 REST API、静态管理及隧道入口。

#### Scenario: WebSocket 路径路由
- **WHEN** 客户端连接到与 API 同源口的 `GET /ws/:proxy_id`
- **THEN** 校验代理存在且启用
- **AND** Upgrade 后与该代理配置的 `remote_address` 建立 TCP，并双向转发

#### Scenario: 禁用或未就绪
- **WHEN** 代理被禁用或未注册
- **THEN** 拒绝建立隧道连接

### Requirement: WebSocket-Only Tunnel
系统 SHALL **仅支持 WebSocket** 作为隧道外层协议（不再使用 HTTP CONNECT 作为代理协议）。

#### Scenario: WebSocket ↔ TCP
- **WHEN** WebSocket 已建立且后端 TCP 可用
- **THEN** WS 二进制帧透明映射到后端 TCP 流

#### Scenario: 活跃会话计数
- **WHEN** 隧道会话开始或结束
- **THEN** 更新对应代理的活跃连接计数
