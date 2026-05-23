package proxy

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"pspxy/internal/config"
)

// Status 代理状态
type Status string

const (
	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
	StatusError   Status = "error"
)

// ProxyStatus 代理运行时状态（所有隧道入口统一为服务端端口上的 `GET /ws/:tunnel_id`）
type ProxyStatus struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	RemoteAddress string    `json:"remote_address"`
	WSPath        string    `json:"ws_path"` // 例如 /ws/<id>，与实际请求路径一致
	Enabled       bool      `json:"enabled"`
	Status        Status    `json:"status"`
	Connections   int64     `json:"connections"` // 当前活跃 WebSocket 隧道数
	StartedAt     time.Time `json:"started_at,omitempty"`
	Error         string    `json:"error,omitempty"`
}

// TCPServer 单个代理运行时登记（已不再各自监听 TCP 端口，仅标记启用与统计 WS 会话）
type TCPServer struct {
	cfg    config.ProxyConfig
	status ProxyStatus
	mu     sync.RWMutex

	activeTunnels int64 // 当前已通过 /ws/:id 建立的隧道数
}

// NewTCPServer 创建代理运行时实例
func NewTCPServer(cfg config.ProxyConfig) *TCPServer {
	status := ProxyStatus{
		ID:            cfg.ID,
		Name:          cfg.Name,
		RemoteAddress: cfg.RemoteAddress,
		WSPath:        WSIngressPath(cfg.ID),
		Enabled:       cfg.Enabled,
		Status:        StatusStopped,
	}
	s := &TCPServer{
		cfg:    cfg,
		status: status,
	}
	s.syncStatusLocked()
	return s
}

// WSIngressPath 返回该 Proxy 在服务端的 ingress 路径（与动态 Reverse 通道共用同源路径格式）
func WSIngressPath(proxyID string) string {
	return "/ws/" + proxyID
}

// Start 启用路由（不打开额外监听端口）
func (s *TCPServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.applyStartLocked(false); err != nil {
		return err
	}
	log.Printf("proxy [%s] 路由就绪 (WS %s → %s)", s.cfg.Name, s.status.WSPath, s.cfg.RemoteAddress)
	return nil
}

func (s *TCPServer) applyStartLocked(reload bool) error {
	if !s.cfg.Enabled {
		s.status.Status = StatusStopped
		s.status.Error = ""
		s.status.StartedAt = time.Time{}
		return nil
	}

	s.status.Status = StatusRunning
	s.status.Error = ""
	if !reload || s.status.StartedAt.IsZero() {
		s.status.StartedAt = time.Now()
	}
	return nil
}

// Stop 禁用路由占位（已有 WS 由各连接自行结束）
func (s *TCPServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.status.Status = StatusStopped
	s.status.StartedAt = time.Time{}

	log.Printf("proxy [%s] 已停用", s.cfg.Name)
	return nil
}

// IncrTunnel 有新的 WebSocket 隧道建立
func (s *TCPServer) IncrTunnel() {
	atomic.AddInt64(&s.activeTunnels, 1)
}

// DecrTunnel WebSocket 隧道结束
func (s *TCPServer) DecrTunnel() {
	atomic.AddInt64(&s.activeTunnels, -1)
}

// ReloadConfig 热更新运行时展示字段（不改变 ID）
func (s *TCPServer) ReloadConfig(cfg config.ProxyConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cfg = cfg
	s.status.Name = cfg.Name
	s.status.RemoteAddress = cfg.RemoteAddress
	s.status.Enabled = cfg.Enabled
	s.status.WSPath = WSIngressPath(cfg.ID)

	if cfg.Enabled {
		return s.applyStartLocked(true)
	}
	s.status.Status = StatusStopped
	s.status.StartedAt = time.Time{}
	s.status.Error = ""
	return nil
}

func (s *TCPServer) syncStatusLocked() {
	if !s.cfg.Enabled {
		s.status.Status = StatusStopped
		s.status.StartedAt = time.Time{}
		return
	}
	s.status.Status = StatusRunning
	if s.status.StartedAt.IsZero() {
		s.status.StartedAt = time.Now()
	}
}

// StatusInfo 获取状态信息
func (s *TCPServer) StatusInfo() ProxyStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	copy := s.status
	copy.WSPath = WSIngressPath(s.cfg.ID)
	copy.RemoteAddress = s.cfg.RemoteAddress
	copy.Enabled = s.cfg.Enabled
	copy.Connections = atomic.LoadInt64(&s.activeTunnels)
	return copy
}

// Config 获取配置
func (s *TCPServer) Config() config.ProxyConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// IsRunning 是否允许转发（启用且未被标记错误）
func (s *TCPServer) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Enabled && s.status.Status == StatusRunning && s.status.Error == ""
}
