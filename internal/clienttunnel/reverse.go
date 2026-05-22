package clienttunnel

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"pspxy/internal/reversetunnel"
)

// RunReverseProvider 连接服务端反向隧道，把本机 TCP 暴露给其他客户端；
// 成功协商后 stdout 可用的 channel_id（同时打日志）。
func RunReverseProvider(serverURL, localHost string, localPort int, accessKey, secretKey string) (channelID string, err error) {
	if localHost == "" {
		localHost = "127.0.0.1"
	}
	ws, err := DialWebSocketPath(serverURL, "/ws/rtunnel/provider", accessKey, secretKey)
	if err != nil {
		return "", err
	}
	defer func() { _ = ws.Close() }()

	offer := map[string]any{"local_host": localHost, "local_port": localPort}
	ob, _ := json.Marshal(offer)
	if err := ws.WriteMessage(websocket.TextMessage, ob); err != nil {
		return "", err
	}
	_, data, err := ws.ReadMessage()
	if err != nil {
		return "", err
	}
	var resp struct {
		OK        bool   `json:"ok"`
		ChannelID string `json:"channel_id"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if !resp.OK {
		if resp.Error != "" {
			return "", fmt.Errorf("%s", resp.Error)
		}
		return "", fmt.Errorf("register failed")
	}
	channelID = resp.ChannelID
	fmt.Println(channelID) // 便于脚本捕获
	log.Printf("[reverse-provider] channel_id=%s → 本机服务 %s:%d", channelID, localHost, localPort)

	var sessMu sync.Mutex
	var wsWriteMu sync.Mutex // gorilla 连接禁止并发 WriteMessage
	tcpBySession := make(map[uuid.UUID]net.Conn)

	for {
		mt, payload, err := ws.ReadMessage()
		if err != nil {
			return channelID, err
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
					return channelID, werr
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

// RunReverseConsumer 监听本地 TCP，每个入站连接经 WebSocket `/ws/rtunnel/session/:channel_id` 与暴露端服务通信。
func RunReverseConsumer(listenAddr, serverURL, channelID, accessKey, secretKey string) error {
	path := fmt.Sprintf("/ws/rtunnel/session/%s", channelID)
	l, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", listenAddr, err)
	}
	defer func() { _ = l.Close() }()
	log.Printf("[reverse-consumer] 监听 %s → 远端 channel=%s", listenAddr, channelID)

	for {
		tcpConn, err := l.Accept()
		if err != nil {
			return err
		}
		go func(c net.Conn) {
			defer c.Close()
			raw, err := DialWebSocketPath(serverURL, path, accessKey, secretKey)
			if err != nil {
				log.Printf("[reverse-consumer] dial ws: %v", err)
				return
			}
			defer raw.Close()
			remote := &wsConnWrapper{conn: raw}
			done := make(chan struct{}, 2)
			go func() {
				io.Copy(remote, c)
				done <- struct{}{}
			}()
			go func() {
				io.Copy(c, remote)
				done <- struct{}{}
			}()
			<-done
		}(tcpConn)
	}
}
