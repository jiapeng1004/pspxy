package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"pspxy/internal/api"
	"pspxy/internal/config"
	"pspxy/internal/proxy"
)

func main() {
	// 加载配置
	cfgMgr, err := config.NewManager("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 创建代理管理器
	proxyMgr := proxy.NewManager(cfgMgr)

	// 启动所有启用的代理
	if err := proxyMgr.StartAll(); err != nil {
		log.Fatalf("Failed to start proxies: %v", err)
	}

	// 创建 API 处理器（手动依赖注入）
	handler := api.NewHandler(proxyMgr, cfgMgr)

	// 启动 HTTP 服务器
	serverPort := cfgMgr.GetServerPort()
	log.Printf("Starting server on :%d", serverPort)

	go func() {
		if err := handler.Run(serverPort); err != nil {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	proxyMgr.StopAll()
}
