# Specification: RESTful API

## ADDED Requirements

### Requirement: API Versioning
所有 API 端点 SHALL 使用 `/api/v1` 前缀。

#### Scenario: 版本化路由
- **WHEN** 客户端访问 API
- **THEN** 所有路由以 `/api/v1` 开头
- **AND** 未来版本可使用 `/api/v2` 等

### Requirement: Proxy List Endpoint
系统 SHALL 提供获取代理列表的端点。

#### Scenario: 获取所有代理
- **WHEN** `GET /api/v1/proxies`
- **THEN** 返回 JSON 数组包含所有代理
- **AND** 每个代理对象包含：id, name, remote_address, enabled, ws_path, status（及连接统计等运行时字段）
- **AND** 响应状态码 200

#### Scenario: 筛选代理
- **WHEN** `GET /api/v1/proxies?enabled=true`
- **THEN** 仅返回启用的代理

### Requirement: Proxy CRUD Endpoints
系统 SHALL 提供完整的代理增删改查端点。

#### Scenario: 创建代理
- **WHEN** `POST /api/v1/proxies` with body:
  ```json
  {
    "name": "测试代理",
    "remote_address": "target.example.com:80",
    "enabled": true
  }
  ```
- **THEN** 系统创建新代理
- **AND** 自动生成 UUID v7 ID
- **AND** 保存到 YAML 配置
- **AND** 启动代理服务
- **AND** 返回 201 Created

#### Scenario: 获取单个代理
- **WHEN** `GET /api/v1/proxies/:id`
- **THEN** 返回指定代理详细信息
- **AND** 包含实时状态（连接数、运行时长）

#### Scenario: 更新代理
- **WHEN** `PUT /api/v1/proxies/:id`
- **THEN** 系统更新配置
- **AND** 保存至 YAML
- **AND** 重启代理实例
- **AND** 返回更新后数据

#### Scenario: 删除代理
- **WHEN** `DELETE /api/v1/proxies/:id`
- **THEN** 系统停止代理
- **AND** 从 YAML 移除配置
- **AND** 返回 204 No Content

### Requirement: Error Handling
所有 API 端点 SHALL 使用统一的错误响应格式。

#### Scenario: 参数验证失败
- **WHEN** 请求参数不合法
- **THEN** 返回 400 Bad Request
- **AND** 响应体包含：
  ```json
  {
    "error": "validation_failed",
    "message": "Key: 'CreateProxyRequest.Name' Error:Field validation for 'Name' failed on the 'required' tag",
    "details": {}
  }
  ```

#### Scenario: 资源未找到
- **WHEN** 请求的代理 ID 不存在
- **THEN** 返回 404 Not Found
- **AND** 响应体包含错误信息

#### Scenario: 服务器内部错误
- **WHEN** 系统发生未预期错误
- **THEN** 返回 500 Internal Server Error
- **AND** 记录详细错误日志
- **AND** 响应体不包含敏感信息

### Requirement: Health Check
系统 SHALL 提供健康检查端点。

#### Scenario: 健康检查
- **WHEN** `GET /api/v1/health`
- **THEN** 返回 200 OK
- **AND** 响应体包含：
  ```json
  {
    "status": "healthy",
    "uptime": 3600,
    "proxies_running": 5
  }
  ```
