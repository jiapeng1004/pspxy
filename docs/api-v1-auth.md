# 管理端访问鉴权（公开 HTTP 接口说明）

服务端在 `server.auth` 中配置了 `access_key` + `secret_key`（均非空）时，REST API 需在请求中带签名请求头或通过登录接口完成凭据校验。

## `GET /api/v1/auth/enabled`

无需鉴权。

**响应示例**

```json
{ "auth_required": true }
```

- `auth_required`: 是否为 `true` 表示服务端已启用 AK/SK（业务请求需签名或已完成登录校验并持有 SK 用于签名）。

---

## `POST /api/v1/auth/login`

无需事先签名。**用于校验**请求体中的 AK/SK 是否与服务端 `server.auth` 配置一致。

**请求**

- `Content-Type: application/json`

```json
{
  "access_key": "配置的 access_key",
  "secret_key": "配置的 secret_key"
}
```

**响应**

- 未启用服务端鉴权时：`200`，且

```json
{ "ok": true, "auth_required": false }
```

- 已启用鉴权且凭据正确：`200`，且

```json
{ "ok": true, "auth_required": true }
```

- 缺少字段：`400`，`{"error":"bad_request",...}`
- AK/SK 不匹配：`401`，`{"error":"unauthorized","message":"凭据无效"}`

后续业务请求须在 Header 中带 `X-Psp-Ak`、`X-Psp-Timestamp`、`X-Psp-Signature`（或由管理端 SPA 自动生成），算法与 WebSocket Query 签名一致：`HEX(SHA1(ak + "\\n" + unixSec + "\\n" + sk))`。

---

建议在 **HTTPS** 或可信内网下使用明文登录体；勿将 `secret_key` 写入版本库。
