package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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

	reverseProvider := flag.Bool("reverse-provider", false, "反向隧道：暴露本机 TCP（与 -reverse-local-port 等配合），服务端下发 channel UUID")
	reverseConsumer := flag.Bool("reverse-consumer", false, "反向隧道：在本机监听 -reverse-listen，经 channel 接入暴露端服务")
	rtLocalHost := flag.String("reverse-local-host", "127.0.0.1", "反向 provider：要暴露的本机主机")
	rtLocalPort := flag.Int("reverse-local-port", 0, "反向 provider：要暴露的本机 TCP 端口")
	rtListen := flag.String("reverse-listen", "", "反向 consumer：监听地址，例如 127.0.0.1:19090")
	rtChannel := flag.String("reverse-channel-id", "", "反向 consumer：provider 下发的 channel UUID")
	apiKeyFlag := flag.String("api-key", "", "服务端 api_key 中任选一段（签名为 HEX(SHA1(k+\"\\n\"+ts+\"\\n\"+k))），WebSocket Query 与其它客户端保持一致")

	flag.Parse()

	clientCfgPath := filepath.Clean(*clientYAML)
	log.Printf("tcp-proxy-client %s", Version)

	if *reverseConsumer {
		if strings.TrimSpace(*rtChannel) == "" || strings.TrimSpace(*rtListen) == "" {
			log.Fatal("反向 consumer 模式需要 -reverse-channel-id 与 -reverse-listen（如 127.0.0.1:19090）")
		}
		runReverseConsumerMain(*serverURL, strings.TrimSpace(*apiKeyFlag), *rtListen, *rtChannel)
		return
	}
	if *reverseProvider {
		if *rtLocalPort <= 0 {
			log.Fatal("反向 provider 模式需要 -reverse-local-port（要暴露的本机端口）")
		}
		runReverseProviderMain(*serverURL, strings.TrimSpace(*apiKeyFlag), *rtLocalHost, *rtLocalPort)
		return
	}

	if *localPort > 0 {
		runHeadless(serverURL, proxyID, localPort, strings.TrimSpace(*apiKeyFlag), clientCfgPath)
		return
	}

	log.Println("客户端 Web 模式（无命令行隧道参数）。脚本/自动化请指定 -local-port 与 -proxy-id。")
	reg := clienttunnel.NewRegistry()
	rev := clienttunnel.NewReverseManager()
	if err := clientui.Run(*uiListen, reg, rev, clientCfgPath); err != nil {
		log.Fatal(err)
	}
}

func runHeadless(serverURL *string, proxyID *string, localPort *int, apiKey string, clientCfgPath string) {
	addr := fmt.Sprintf("127.0.0.1:%d", *localPort)

	reg := clienttunnel.NewRegistry()
	cfg := clienttunnel.Config{
		ID:        clienttunnel.DefaultSingleProxyID,
		ServerURL: *serverURL,
		ProxyID:   *proxyID,
		LocalPort: *localPort,
		APIKey:    apiKey,
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

func runReverseProviderMain(serverURL, apiKey string, rtHost string, rtPort int) {
	ch, ws, err := clienttunnel.HandshakeReverseProvider(serverURL, rtHost, rtPort, apiKey, "")
	if err != nil {
		log.Fatalf("反向 provider 握手失败: %v", err)
	}
	fmt.Println(ch)
	log.Printf("[reverse-provider] channel_id=%s → 本机服务 %s:%d", ch, rtHost, rtPort)
	defer func() { _ = ws.Close() }()
	if err := clienttunnel.ServeReverseProvider(ws, rtHost, rtPort); err != nil {
		log.Printf("反向 provider 结束 (channel=%s): %v", ch, err)
	} else {
		log.Printf("反向 provider 已退出 (channel=%s)", ch)
	}
}

func runReverseConsumerMain(serverURL, apiKey, listenAddr, channelID string) {
	if err := clienttunnel.RunReverseConsumer(listenAddr, serverURL, channelID, apiKey); err != nil {
		log.Fatal(err)
	}
}
