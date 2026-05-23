# Specification: Proxy Core Engine

## ADDED Requirements

### Requirement: Unified Server Ingress
系统 SHALL 在**单一服务端端口**（`config.server.port`）上承载 REST API、静态管理及隧道入口。

#### Scenario: WebSocket 路径路由（统一 ingress `/ws/:tunnel_id`）
- **WHEN** 客户端连接到同源口的 `GET /ws/:tunnel_id`
- **AND** `tunnel_id` 与服务端登记的 **Proxy.id** 一致且代理已启用
- **THEN** Upgrade 后与该代理的 `remote_address` 建立 TCP，并双向转发

#### Scenario: 动态 Reverse Consumer（同一 ingress 路径）
- **WHEN** `tunnel_id` 为 UUID 且在 Reverse Broker 中存在活跃 Provider 通道（且 **`tunnel_id` 不与已登记的 Proxy.id 重合**，若重合则以 **Proxy/TCP Provider** 优先）
- **THEN** Upgrade 并按 Consumer 语义附着该通道（每 WS 会话对应一侧本地 TCP）

#### Scenario: 遗留 Reverse 会话路径（兼容）
- **WHEN** 客户端连接到 `GET /ws/rtunnel/session/:channel_id`
- **THEN** **仅** 尝试 Reverse Broker 附着（不进行 Proxy/TCP Provider 分流）

#### Scenario: 禁用或未就绪
- **WHEN** 代理被禁用或未注册，且亦非活跃 Reverse `channel_id`
- **THEN** 拒绝建立隧道连接（Upgrade 前应返回可读错误应答）

### Requirement: WebSocket-Only Tunnel
系统 SHALL **仅支持 WebSocket** 作为隧道外层协议（不再使用 HTTP CONNECT 作为代理协议）。

#### Scenario: WebSocket ↔ TCP
- **WHEN** WebSocket 已建立且后端 TCP 可用
- **THEN** WS 二进制帧透明映射到后端 TCP 流

#### Scenario: 活跃会话计数
- **WHEN** 隧道会话开始或结束
- **THEN** 更新对应代理的活跃连接计数
