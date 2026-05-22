package clientembed

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"strings"
)

// StaticFiles 内嵌客户端 Web UI（构建自 frontend/web-client）
//
//go:embed dist
var StaticFiles embed.FS

// CreateFileServer 创建静态文件服务处理器；/api 交给上层路由
func CreateFileServer() http.Handler {
	contentStatic, err := fs.Sub(StaticFiles, "dist")
	if err != nil {
		log.Printf("embedded client SPA not found: %v", err)
		return http.HandlerFunc(servePlaceholder)
	}

	fileServer := http.FileServer(http.FS(contentStatic))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			http.NotFound(w, r)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/")
		f, err := contentStatic.Open(path)
		if err != nil {
			r.URL.Path = "/"
		} else {
			f.Close()
		}

		fileServer.ServeHTTP(w, r)
	})
}

func servePlaceholder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!DOCTYPE html>
<html lang="zh-CN">
<head><meta charset="UTF-8"><title>TCP Proxy 客户端</title></head>
<body style="display:flex;align-items:center;justify-content:center;height:100vh;font-family:sans-serif;">
<div style="text-align:center">
<h1>TCP Proxy 客户端</h1>
<p>前端未嵌入。请执行：<code>cd frontend/web-client && npm run build</code> 或 <code>go run build.go web-client</code> 后再编译客户端。</p>
</div></body></html>`))
}
