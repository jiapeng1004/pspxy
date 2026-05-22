package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"pspxy/internal/clientconfig"
	"pspxy/internal/clienttunnel"
	"pspxy/internal/clientui"
)

// Version 由构建脚本 / CI 通过 -ldflags "-X main.Version=..." 注入。
var Version = "dev"

func main() {
	serverURL := flag.String("server", "ws://localhost:3000", "服务端 WebSocket 基址（如 ws://host:port，隧道路径会自动拼为 /ws/:proxy-id）")
	proxyID := flag.String("proxy-id", "", "代理 ID（与服务端配置的 id 一致，对应路径 /ws/<id>）")
	localPort := flag.Int("local-port", 0, "本地监听端口；0 表示启动内置 Web UI（默认 :9420）")
	uiListen := flag.String("ui-listen", ":9420", "Web UI 监听地址（仅 local-port 为 0 时有效）")
	clientYAML := flag.String("client-config", "config-client.yaml", "客户端隧道持久化配置（YAML）；Web 启动隧道成功后会写入，进程下次启动会从该文件自动还原")

	flag.Parse()

	clientCfgPath := filepath.Clean(*clientYAML)
	log.Printf("tcp-proxy-client %s", Version)

	if *localPort > 0 {
		runHeadless(serverURL, proxyID, localPort, clientCfgPath)
		return
	}

	log.Println("客户端 Web 模式（无命令行隧道参数）。脚本/自动化请指定 -local-port 与 -proxy-id。")
	reg := clienttunnel.NewRegistry()
	if err := clientui.Run(*uiListen, reg, clientCfgPath); err != nil {
		log.Fatal(err)
	}
}

func runHeadless(serverURL *string, proxyID *string, localPort *int, clientCfgPath string) {
	addr := fmt.Sprintf("127.0.0.1:%d", *localPort)

	reg := clienttunnel.NewRegistry()
	cfg := clienttunnel.Config{
		ID:        clienttunnel.DefaultSingleProxyID,
		ServerURL: *serverURL,
		ProxyID:   *proxyID,
		LocalPort: *localPort,
	}

	if err := reg.Start(cfg); err != nil {
		log.Fatal(err)
	}
	if err := clientconfig.SaveProxies(clientCfgPath, reg.PersistedConfigs()); err != nil {
		log.Printf("写入客户端配置文件 %s 失败（隧道已在运行）: %v", clientCfgPath, err)
	} else if absPath, absErr := filepath.Abs(clientCfgPath); absErr == nil {
		log.Printf("已将隧道参数写入 %s", absPath)
	} else {
		log.Printf("已将隧道参数写入 %s", clientCfgPath)
	}
	log.Printf("无头模式监听: %s（经 WebSocket 连到服务端单端口路由）", addr)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	reg.StopAll()
}
