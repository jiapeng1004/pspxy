package proxy

import (
	"log"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  32 * 1024,
	WriteBufferSize: 32 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有来源
	},
}

// WSUpgrader WebSocket 升级处理器
type WSUpgrader struct{}

// NewWSUpgrader 创建 WebSocket 升级器
func NewWSUpgrader() *WSUpgrader {
	return &WSUpgrader{}
}

// UpgradeConn 将 HTTP 连接升级为 WebSocket（与 HandleUpgrade 共用同一 Upgrader）。
func (u *WSUpgrader) UpgradeConn(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	return wsUpgrader.Upgrade(w, r, nil)
}

// HandleUpgrade 处理 WebSocket 升级请求
// targetAddr: 目标 TCP 地址 (host:port)
func (u *WSUpgrader) HandleUpgrade(w http.ResponseWriter, r *http.Request, targetAddr string) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// 连接目标
	targetConn, err := net.DialTimeout("tcp", targetAddr, 10*time.Second)
	if err != nil {
		log.Printf("ws dial target %s: %v", targetAddr, err)
		return
	}
	defer targetConn.Close()

	// 双向数据转发
	done := make(chan struct{}, 2)

	go func() {
		defer func() { done <- struct{}{} }()
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if msgType == websocket.BinaryMessage || msgType == websocket.TextMessage {
				if _, err := targetConn.Write(data); err != nil {
					return
				}
			}
		}
	}()

	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 32*1024)
		for {
			n, err := targetConn.Read(buf)
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()

	<-done
	log.Printf("ws tunnel closed for %s", targetAddr)
}
