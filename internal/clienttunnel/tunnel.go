package clienttunnel

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"pspxy/internal/api"
)

// Config 客户端隧道运行时参数（服务端单端口，`/ws/:proxy_id` 路由后端）
type Config struct {
	ID        string `json:"id,omitempty" yaml:"id,omitempty"`
	ServerURL string `json:"server_url" yaml:"server_url"`
	ProxyID   string `json:"proxy_id" yaml:"proxy_id"`
	LocalPort int    `json:"local_port" yaml:"local_port"`
	AccessKey string `json:"access_key,omitempty" yaml:"access_key,omitempty"`
	SecretKey string `json:"secret_key,omitempty" yaml:"secret_key,omitempty"`
}

func (c Config) validate() error {
	if strings.TrimSpace(c.ServerURL) == "" {
		return errors.New("server_url 必填")
	}
	if strings.TrimSpace(c.ProxyID) == "" {
		return errors.New("proxy_id 必填")
	}
	if c.LocalPort < 1 || c.LocalPort > 65535 {
		return errors.New("local_port 必须在 1-65535 之间")
	}
	return nil
}

// NormalizeServerURLForWS 将 bare host:port / http(s):// 规范为可被 url.Parse / websocket.Dial 使用的 ws(s)://。
func NormalizeServerURLForWS(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	switch {
	case strings.HasPrefix(lower, "http://"):
		return "ws://" + s[len("http://"):]
	case strings.HasPrefix(lower, "https://"):
		return "wss://" + s[len("https://"):]
	case strings.HasPrefix(lower, "ws://") || strings.HasPrefix(lower, "wss://"):
		return s
	}
	if !strings.Contains(s, "://") {
		return "ws://" + s
	}
	return s
}

// PublicLastConfig 返回给前端的状态快照（不包含密钥）。
type PublicLastConfig struct {
	ServerURL            string `json:"server_url"`
	ProxyID              string `json:"proxy_id"`
	LocalPort            int    `json:"local_port"`
	AccessAuthConfigured bool   `json:"access_auth_configured,omitempty"`
}

// Status 运行时状态快照
type Status struct {
	Running           bool             `json:"running"`
	Error             string           `json:"error,omitempty"`
	Listen            string           `json:"listen,omitempty"`
	Message           string           `json:"message,omitempty"`
	ActiveConnections int64            `json:"active_connections,omitempty"`
	LastConfig        PublicLastConfig `json:"last_config,omitempty"`
	// ClientConfigFile HTTP / 脚本启动持久化成功后由 handler / 外层填充的文件绝对路径。
	ClientConfigFile string `json:"client_config_file,omitempty"`
	// PersistWarning 隧道已运行时若写盘失败则由 handler 填入（成功时为空）。
	PersistWarning string `json:"persist_warning,omitempty"`
}

// Tunnel 本地 TCP ←→ 服务端 WebSocket 隧道
type Tunnel struct {
	mu          sync.Mutex
	listener    net.Listener
	wg          sync.WaitGroup
	activeConns atomic.Int64
	runErr      string
	activeCfg   atomic.Value // Config
}

// NewTunnel 创建一个可重复启动/停止的隧道实例
func NewTunnel() *Tunnel {
	t := new(Tunnel)
	t.activeCfg.Store(Config{})
	return t
}

// Start 会先停止已有监听再按新参数启动
func (t *Tunnel) Start(cfg Config) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := cfg.validate(); err != nil {
		return err
	}

	t.stopUnsafeLocked()
	t.runErr = ""

	addr := fmt.Sprintf("127.0.0.1:%d", cfg.LocalPort)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.runErr = err.Error()
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	t.listener = l
	t.activeCfg.Store(cfg)

	t.wg.Add(1)
	go t.acceptLoop(l)

	log.Printf("[client tunnel] listening %s -> WS %s/ws/%s", addr, trimSchemeHost(cfg.ServerURL), cfg.ProxyID)
	return nil
}

func trimSchemeHost(raw string) string {
	u, err := url.Parse(NormalizeServerURLForWS(raw))
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Scheme + "://" + u.Host
}

// Stop 关闭监听与 accept 循环。
func (t *Tunnel) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stopUnsafeLocked()
}

func (t *Tunnel) stopUnsafeLocked() error {
	if t.listener == nil {
		return nil
	}

	err := t.listener.Close()
	t.listener = nil
	t.wg.Wait()

	log.Printf("[client tunnel] stopped")
	return err
}

func (t *Tunnel) acceptLoop(l net.Listener) {
	defer t.wg.Done()

	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}

		cfg := t.activeCfg.Load().(Config)
		t.activeConns.Add(1)
		go func(c net.Conn, ccfg Config) {
			defer t.activeConns.Add(-1)
			handlePair(c, ccfg)
		}(conn, cfg)
	}
}

// Snapshot 返回当前是否在监听等信息
func (t *Tunnel) Snapshot() Status {
	raw := t.activeCfg.Load().(Config)
	t.mu.Lock()
	listener := t.listener
	errStr := t.runErr
	t.mu.Unlock()

	st := Status{
		LastConfig: PublicLastConfig{
			ServerURL: strings.TrimSpace(raw.ServerURL),
			ProxyID:   strings.TrimSpace(raw.ProxyID),
			LocalPort: raw.LocalPort,
			AccessAuthConfigured: strings.TrimSpace(raw.AccessKey) != "" &&
				strings.TrimSpace(raw.SecretKey) != "",
		},
		ActiveConnections: t.activeConns.Load(),
		Error:             errStr,
	}
	if listener != nil {
		st.Running = true
		st.Listen = listener.Addr().String()
		st.Message = fmt.Sprintf("websocket /ws/%s -> %s", raw.ProxyID, raw.ServerURL)
	}
	return st
}

func handlePair(localConn net.Conn, cfg Config) {
	defer localConn.Close()

	remote, err := dialWebSocket(cfg)
	if err != nil {
		log.Printf("连接远端失败: %v", err)
		return
	}
	defer remote.Close()

	done := make(chan struct{}, 2)
	go func() {
		io.Copy(remote, localConn)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(localConn, remote)
		done <- struct{}{}
	}()
	<-done
}

var wsDialer = websocket.Dialer{
	ReadBufferSize:  32 * 1024,
	WriteBufferSize: 32 * 1024,
}

func dialWebSocket(cfg Config) (net.Conn, error) {
	serverURL := NormalizeServerURLForWS(cfg.ServerURL)
	proxyID := strings.TrimSpace(cfg.ProxyID)

	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("parse server url: %w", err)
	}
	u.Path = fmt.Sprintf("/ws/%s", proxyID)

	ak := strings.TrimSpace(cfg.AccessKey)
	sk := strings.TrimSpace(cfg.SecretKey)
	if ak != "" && sk != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		sig := strings.ToLower(api.SignAccessPayload(ak, ts, sk))
		q := u.Query()
		q.Set("psp_ak", ak)
		q.Set("psp_ts", ts)
		q.Set("psp_sig", sig)
		u.RawQuery = q.Encode()
	}

	var lastErr error
	for i := 0; i < 10; i++ {
		conn, _, err := wsDialer.Dial(u.String(), nil)
		if err == nil {
			return &wsConnWrapper{conn: conn}, nil
		}
		lastErr = err
		wait := time.Duration(1<<uint(i)) * time.Second
		if wait > 60*time.Second {
			wait = 60 * time.Second
		}
		time.Sleep(wait)
	}
	return nil, fmt.Errorf("websocket 重连失败: %w", lastErr)
}

type wsConnWrapper struct {
	conn   *websocket.Conn
	reader io.Reader
}

func (w *wsConnWrapper) Read(b []byte) (int, error) {
	for {
		if w.reader != nil {
			n, err := w.reader.Read(b)
			if errors.Is(err, io.EOF) {
				w.reader = nil
				if n > 0 {
					return n, nil
				}
				continue
			}
			return n, err
		}

		_, msg, err := w.conn.ReadMessage()
		if err != nil {
			return 0, err
		}
		w.reader = &byteReader{data: msg}
	}
}

func (w *wsConnWrapper) Write(b []byte) (int, error) {
	err := w.conn.WriteMessage(websocket.BinaryMessage, b)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

func (w *wsConnWrapper) Close() error {
	return w.conn.Close()
}

func (w *wsConnWrapper) LocalAddr() net.Addr {
	return w.conn.LocalAddr()
}

func (w *wsConnWrapper) RemoteAddr() net.Addr {
	return w.conn.RemoteAddr()
}

func (w *wsConnWrapper) SetDeadline(t time.Time) error {
	if err := w.conn.SetReadDeadline(t); err != nil {
		return err
	}
	return w.conn.SetWriteDeadline(t)
}

func (w *wsConnWrapper) SetReadDeadline(t time.Time) error {
	return w.conn.SetReadDeadline(t)
}

func (w *wsConnWrapper) SetWriteDeadline(t time.Time) error {
	return w.conn.SetWriteDeadline(t)
}

type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) Read(b []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(b, r.data[r.pos:])
	r.pos += n
	return n, nil
}

var _ net.Conn = (*wsConnWrapper)(nil)
