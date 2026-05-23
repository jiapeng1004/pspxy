# 管理端访问鉴权（公开 HTTP 接口说明）

服务端在 `server.auth` 启用鉴权时（见下），REST/WebSocket 需带签名或由登录接口校验凭据。

**启用条件：** `api_key` 非空——值为逗号分隔的多个密钥片段；任一片段 **k** 可作为 `psp_ak` / `X-Psp-Ak`，签名算法为：  
`HEX(SHA1(k + "\\n" + unixSec + "\\n" + k))`。

## `GET /api/v1/auth/enabled`

无需鉴权。

**响应示例**

```json
{ "auth_required": true }
```

- `auth_required`: `true` 表示已启用上述鉴权（业务请求需要签名或通过登录）。

---

## `POST /api/v1/auth/login`

无需事先签名。用于明文校验凭据是否与配置一致。

**请求**

`Content-Type: application/json`

任选服务端 `api_key` 中配置的**一段密钥**填入：

```json
{ "api_key": "与 server.auth.api_key 中某一段完全一致" }
```

**响应**

- 未启用服务端鉴权：`200`，`{"ok":true,"auth_required":false}`。
- 已启用且凭据正确：`200`，`{"ok":true,"auth_required":true}`。
- JSON 无效：`400`。
- 不匹配：`401`。

后续 REST 请求的 Header：`X-Psp-Ak`、`X-Psp-Timestamp`、`X-Psp-Signature`（管理端 SPA 自动生成），算法与 Query `psp_*` 一致。

---

建议在 **HTTPS** 或可信内网下使用明文登录；勿把完整 `api_key` 列表提交到不信任环境。
