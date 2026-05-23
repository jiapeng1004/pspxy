package clienttunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"pspxy/internal/reversetunnel"
)

// HandshakeReverseProvider 与 `/ws/rtunnel/provider` 协商；WebSocket Query 鉴权使用单列 api_key。
// reuseChannelID 非空时在首包 offer 中带 channel_id，供服务端按该 UUID reclaim；空则由服务端新发。
func HandshakeReverseProvider(serverURL, localHost string, localPort int, apiKey string, reuseChannelID string) (channelID string, ws *websocket.Conn, err error) {
	if localHost == "" {
		localHost = "127.0.0.1"
	}
	wsConn, err := DialWebSocketPath(serverURL, "/ws/rtunnel/provider", apiKey)
	if err != nil {
		return "", nil, err
	}
	offer := map[string]any{"local_host": localHost, "local_port": localPort}
	if cid := strings.TrimSpace(reuseChannelID); cid != "" {
		offer["channel_id"] = cid
	}
	ob, _ := json.Marshal(offer)
	if err := wsConn.WriteMessage(websocket.TextMessage, ob); err != nil {
		_ = wsConn.Close()
		return "", nil, err
	}
	_, data, err := wsConn.ReadMessage()
	if err != nil {
		_ = wsConn.Close()
		return "", nil, err
	}
	var resp struct {
		OK        bool   `json:"ok"`
		ChannelID string `json:"channel_id"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		_ = wsConn.Close()
		return "", nil, fmt.Errorf("decode response: %w", err)
	}
	if !resp.OK {
		_ = wsConn.Close()
		if resp.Error != "" {
			return "", nil, fmt.Errorf("%s", resp.Error)
		}
		return "", nil, fmt.Errorf("register failed")
	}
	return resp.ChannelID, wsConn, nil
}

// ServeReverseProvider 处理 provider 侧消息循环，直到 websocket 关闭或致命错误。
func ServeReverseProvider(ws *websocket.Conn, localHost string, localPort int) error {
	if localHost == "" {
		localHost = "127.0.0.1"
	}
	var sessMu sync.Mutex
	var wsWriteMu sync.Mutex
	tcpBySession := make(map[uuid.UUID]net.Conn)

	for {
		mt, payload, err := ws.ReadMessage()
		if err != nil {
			return err
		}
		switch mt {
		case websocket.TextMessage:
			var m struct {
				Cmd string `json:"cmd"`
				SID string `json:"sid"`
			}
			if json.Unmarshal(payload, &m) != nil {
				continue
			}
			sid, perr := uuid.Parse(m.SID)
			if perr != nil {
				continue
			}
			switch m.Cmd {
			case "detach":
				sessMu.Lock()
				tc := tcpBySession[sid]
				delete(tcpBySession, sid)
				sessMu.Unlock()
				if tc != nil {
					_ = tc.Close()
				}
			case "attach":
				addr := fmt.Sprintf("%s:%d", localHost, localPort)
				tc, dialErr := net.Dial("tcp", addr)
				ack := map[string]any{"cmd": "attached", "sid": m.SID, "ok": dialErr == nil}
				if dialErr != nil {
					log.Printf("[reverse-provider] 连接本地 %s 失败: %v", addr, dialErr)
					ack["error"] = dialErr.Error()
				} else {
					sessMu.Lock()
					tcpBySession[sid] = tc
					sessMu.Unlock()
					go pumpReverseTCPToWS(ws, &wsWriteMu, tc, sid, &sessMu, tcpBySession)
				}
				akb, _ := json.Marshal(ack)
				wsWriteMu.Lock()
				werr := ws.WriteMessage(websocket.TextMessage, akb)
				wsWriteMu.Unlock()
				if werr != nil {
					return werr
				}
			}
		case websocket.BinaryMessage:
			sid, body, ok := reversetunnel.ParsePrefixedMessage(payload)
			if !ok {
				continue
			}
			sessMu.Lock()
			tc := tcpBySession[sid]
			sessMu.Unlock()
			if tc == nil {
				continue
			}
			if _, werr := tc.Write(body); werr != nil {
				log.Printf("[reverse-provider] tcp write: %v", werr)
				sessMu.Lock()
				delete(tcpBySession, sid)
				sessMu.Unlock()
				_ = tc.Close()
			}
		}
	}
}

func pumpReverseTCPToWS(ws *websocket.Conn, wsWriteMu *sync.Mutex, tcp net.Conn, sid uuid.UUID, mu *sync.Mutex, tcpBySession map[uuid.UUID]net.Conn) {
	defer func() {
		mu.Lock()
		delete(tcpBySession, sid)
		mu.Unlock()
		_ = tcp.Close()
	}()
	buf := make([]byte, 32*1024)
	for {
		n, err := tcp.Read(buf)
		if n > 0 {
			frame := reversetunnel.PrependSessionID(sid, buf[:n])
			wsWriteMu.Lock()
			werr := ws.WriteMessage(websocket.BinaryMessage, frame)
			wsWriteMu.Unlock()
			if werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// RunReverseProvider 兼容 CLI：握手后阻塞服务直到连接结束。
func RunReverseProvider(serverURL, localHost string, localPort int, apiKey string) (channelID string, err error) {
	channelID, ws, err := HandshakeReverseProvider(serverURL, localHost, localPort, apiKey, "")
	if err != nil {
		return "", err
	}
	defer func() { _ = ws.Close() }()
	log.Printf("[reverse-provider] channel_id=%s → 本机服务 %s:%d", channelID, localHost, localPort)
	return channelID, ServeReverseProvider(ws, localHost, localPort)
}

// RunReverseConsumer 监听本地 TCP，每个入站连接经 WebSocket 与暴露端通信；收到 OS 中断即退出。
func RunReverseConsumer(listenAddr, serverURL, channelID, apiKey string) error {
	path := reverseSessionPath(channelID)
	l, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", listenAddr, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sig:
			cancel()
			_ = l.Close()
		case <-ctx.Done():
		}
	}()

	defer func() { _ = l.Close() }()
	log.Printf("[reverse-consumer] 监听 %s → 远端 channel=%s", listenAddr, channelID)
	return ServeReverseConsumer(ctx, l, serverURL, path, apiKey, nil)
}

func reverseSessionPath(channelID string) string {
	return "/ws/" + channelID
}

// ServeReverseConsumer 在给定 Listener 与 context 下 accept；ctx 取消时关闭 listener 应使 Accept 返回。
// activeBridges 非 nil 时，每个已成功 accept 的连接在桥接存续期间递增/递减计数（可与 UI 快照对齐）。
func ServeReverseConsumer(ctx context.Context, l net.Listener, serverURL, sessionPath string, apiKey string, activeBridges *atomic.Int64) error {
	for {
		tcpConn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		go func(c net.Conn) {
			if activeBridges != nil {
				activeBridges.Add(1)
				defer activeBridges.Add(-1)
			}
			reverseConsumerBridgeTCP(c, serverURL, sessionPath, apiKey)
		}(tcpConn)
	}
}

func reverseConsumerBridgeTCP(c net.Conn, serverURL, sessionPath string, apiKey string) {
	defer func() { _ = c.Close() }()
	raw, err := DialWebSocketPath(serverURL, sessionPath, apiKey)
	if err != nil {
		log.Printf("[reverse-consumer] dial ws: %v", err)
		return
	}
	defer raw.Close()
	remote := &wsConnWrapper{conn: raw}
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(remote, c)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(c, remote)
		done <- struct{}{}
	}()
	<-done
}
