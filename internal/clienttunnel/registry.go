package clienttunnel

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// DefaultSingleProxyID 单隧道/旧版 Web 使用一条固定插槽，便于与旧配置兼容。
const DefaultSingleProxyID = "default"

// Registry 管理多条本地 TCP → 远端 WebSocket 隧道（每条独立监听端口）。
type Registry struct {
	mu    sync.Mutex
	items map[string]*registryInstance
}

type registryInstance struct {
	tun *Tunnel
	cfg Config
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{items: make(map[string]*registryInstance)}
}

// EnsureProxyID 若 id 为空则生成；空串表示使用默认单槽位 id。
func EnsureProxyID(id string) string {
	s := strings.TrimSpace(id)
	if s != "" {
		return s
	}
	return DefaultSingleProxyID
}

// NewProxyID 用于 UI 新增行：随机唯一 id。
func NewProxyID() string {
	return uuid.New().String()
}

// PublicFromConfig 状态展示用（不含密钥明文）。
func PublicFromConfig(c Config) PublicLastConfig {
	su := NormalizeServerURLForWS(strings.TrimSpace(c.ServerURL))
	return PublicLastConfig{
		ServerURL: su,
		ProxyID:   strings.TrimSpace(c.ProxyID),
		LocalPort: c.LocalPort,
		AccessAuthConfigured: strings.TrimSpace(c.AccessKey) != "" &&
			strings.TrimSpace(c.SecretKey) != "",
	}
}

func (r *Registry) trimCopy(c Config) Config {
	c.ServerURL = NormalizeServerURLForWS(strings.TrimSpace(c.ServerURL))
	c.ProxyID = strings.TrimSpace(c.ProxyID)
	c.AccessKey = strings.TrimSpace(c.AccessKey)
	c.SecretKey = strings.TrimSpace(c.SecretKey)
	c.ID = strings.TrimSpace(c.ID)
	return c
}

func (r *Registry) localPortBusy(port int, exceptID string) bool {
	if port < 1 {
		return false
	}
	for id, it := range r.items {
		if id == exceptID {
			continue
		}
		if it.cfg.LocalPort == port {
			return true
		}
	}
	return false
}

// Start 启动或重启一条代理：若 id 对应实例不存在则创建；会持久化 lastCfg。
func (r *Registry) Start(cfg Config) error {
	cfg = r.trimCopy(cfg)
	cfg.ID = EnsureProxyID(cfg.ID)
	if err := cfg.validate(); err != nil {
		return err
	}
	if r.localPortBusy(cfg.LocalPort, cfg.ID) {
		return fmt.Errorf("本地端口 %d 已被其它代理占用", cfg.LocalPort)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	inst, ok := r.items[cfg.ID]
	if !ok {
		inst = &registryInstance{tun: NewTunnel()}
		r.items[cfg.ID] = inst
	}
	if err := inst.tun.Start(cfg); err != nil {
		return err
	}
	inst.cfg = cfg
	return nil
}

// Stop 停止指定 id 的监听（保留配置以便再次启动或写盘）。
func (r *Registry) Stop(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		id = DefaultSingleProxyID
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	inst, ok := r.items[id]
	if !ok {
		return nil
	}
	return inst.tun.Stop()
}

// StopAll 停止全部监听。
func (r *Registry) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, it := range r.items {
		_ = it.tun.Stop()
	}
}

// Remove 停止并删除一条配置（从注册表移除）。
func (r *Registry) Remove(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("缺少代理 id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	inst, ok := r.items[id]
	if !ok {
		return nil
	}
	_ = inst.tun.Stop()
	delete(r.items, id)
	return nil
}

// PersistedConfigs 返回当前所有已登记条目的配置副本（含未在监听的条目），用于写 YAML。
func (r *Registry) PersistedConfigs() []Config {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Config, 0, len(r.items))
	for _, it := range r.items {
		out = append(out, it.cfg)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LocalPort != out[j].LocalPort {
			return out[i].LocalPort < out[j].LocalPort
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// ProxyRow 单条代理运行态（API 返回）。
type ProxyRow struct {
	ID                string           `json:"id"`
	Running           bool             `json:"running"`
	Error             string           `json:"error,omitempty"`
	Listen            string           `json:"listen,omitempty"`
	Message           string           `json:"message,omitempty"`
	ActiveConnections int64            `json:"active_connections,omitempty"`
	LastConfig        PublicLastConfig `json:"last_config"`
}

// MultiStatus 多代理聚合状态。
type MultiStatus struct {
	Proxies          []ProxyRow `json:"proxies"`
	ClientConfigFile string     `json:"client_config_file,omitempty"`
	PersistWarning   string     `json:"persist_warning,omitempty"`
}

// Snapshot 聚合全部实例状态。
func (r *Registry) Snapshot() MultiStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := make([]ProxyRow, 0, len(r.items))
	for id, it := range r.items {
		snap := it.tun.Snapshot()
		pub := PublicFromConfig(it.cfg)
		rows = append(rows, ProxyRow{
			ID:                id,
			Running:           snap.Running,
			Error:             snap.Error,
			Listen:            snap.Listen,
			Message:           snap.Message,
			ActiveConnections: snap.ActiveConnections,
			LastConfig:        pub,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].LastConfig.LocalPort != rows[j].LastConfig.LocalPort {
			return rows[i].LastConfig.LocalPort < rows[j].LastConfig.LocalPort
		}
		return rows[i].ID < rows[j].ID
	})
	return MultiStatus{Proxies: rows}
}

// BootstrapFromConfigs 启动多条（用于从 YAML 恢复）；单条失败仅打日志，继续其它条目。
func (r *Registry) BootstrapFromConfigs(cfgs []Config) {
	for _, c := range cfgs {
		cc := r.trimCopy(c)
		cc.ID = EnsureProxyID(cc.ID)
		if err := cc.validate(); err != nil {
			logSkipBootstrap(cc.ID, err)
			continue
		}
		if err := r.Start(cc); err != nil {
			logSkipBootstrap(cc.ID, err)
		}
	}
}

func logSkipBootstrap(id string, err error) {
	if strings.TrimSpace(id) == "" {
		id = "(无 id)"
	}
	log.Printf("[client tunnel] 跳过自动恢复 id=%s: %v", id, err)
}
