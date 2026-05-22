# Specification: Configuration Management

## ADDED Requirements

### Requirement: YAML Storage
系统 SHALL 使用 YAML 文件存储所有代理配置。

#### Scenario: 配置文件结构
- **GIVEN** 配置文件位于 `config.yaml`
- **THEN** 文件格式符合以下结构：
  ```yaml
  proxies:
    - id: "proxy-uuid"
      name: "示例代理"
      remote_address: "example.com:443"
      enabled: true
  
  server:
    port: 3000
    static_dir: "./frontend/web-admin"
  ```

  （隧道入口：`server.port`；WebSocket 路径 `/ws/<id>`。）

#### Scenario: 配置初始化
- **WHEN** 系统首次启动且配置文件不存在
- **THEN** 创建默认配置文件
- **AND** 包含空的 proxies 列表
- **AND** 包含默认 server 配置

### Requirement: Hot Reload
系统 SHALL 在配置变更后自动重新加载，无需重启。

#### Scenario: 文件变更检测
- **WHEN** `config.yaml` 文件被修改
- **THEN** 系统在 1 秒内检测到变更
- **AND** 触发配置重载流程

#### Scenario: 配置重载
- **WHEN** 检测到配置文件变更
- **THEN** 系统验证新配置合法性
- **AND** 停止已禁用的代理
- **AND** 启动新增的代理
- **AND** 更新现有代理配置
- **AND** 记录重载成功日志

#### Scenario: 无效配置处理
- **WHEN** 配置文件包含非法内容（如无效的 remote_address）
- **THEN** 系统保留旧配置
- **AND** 记录错误日志
- **AND** 返回错误通知给调用方

### Requirement: Programmatic Config Update
系统 SHALL 提供 API 接口用于程序化修改配置。

#### Scenario: 创建代理
- **WHEN** 调用 `POST /api/v1/proxies`
- **THEN** 系统生成 UUID v7 作为代理 ID
- **AND** 将新代理添加到 YAML 文件
- **AND** 立即启动该代理
- **AND** 返回 201 Created 及代理信息

#### Scenario: 更新代理
- **WHEN** 调用 `PUT /api/v1/proxies/:id`
- **THEN** 系统定位并更新对应代理配置
- **AND** 保存到 YAML 文件
- **AND** 重启该代理实例
- **AND** 返回更新后的代理信息

#### Scenario: 删除代理
- **WHEN** 调用 `DELETE /api/v1/proxies/:id`
- **THEN** 系统从 YAML 文件移除该代理
- **AND** 停止正在运行的代理实例
- **AND** 返回 204 No Content

#### Scenario: 手动触发重载
- **WHEN** 调用 `POST /api/v1/config/reload`
- **THEN** 系统重新读取 YAML 配置文件
- **AND** 同步所有代理状态
- **AND** 返回重载结果
