# frontend

本目录为 mono repo 中的前端合集：

- **`web-admin/`**：服务端管理端（Vite + React），构建产物嵌入 `internal/embed/admin`。
- **`web-client/`**：客户端隧道控制台（Vite + React），构建产物嵌入 `internal/embed/client`。支持**多条**本地转发（`/ws/<代理ID>` + 本地端口），配置写入工作目录下的 `config-client.yaml`（`proxies` 数组）；可用 `-client-config` 指定路径，进程启动时会按文件中条目自动尝试恢复监听。

开发或构建时请进入对应子目录执行 `npm install` / `npm run dev`，或使用仓库根的 `go run build.go web-admin`、`web-client` 或 `web`。
