package config

import (
	"fmt"
	"net"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// ProxyConfig 单个代理配置（隧道仅经由服务端统一端口上的 WebSocket 路径 /ws/:id）
type ProxyConfig struct {
	ID            string `yaml:"id" json:"id"`
	Name          string `yaml:"name" json:"name"`
	RemoteAddress string `yaml:"remote_address" json:"remote_address"`
	Enabled       bool   `yaml:"enabled" json:"enabled"`
}

// ServerConfig 服务配置
type ServerConfig struct {
	Port      int        `yaml:"port"`
	StaticDir string     `yaml:"static_dir"`
	Auth      AccessAuth `yaml:"auth"`
}

// Config 总配置
type Config struct {
	Proxies []ProxyConfig `yaml:"proxies" json:"proxies"`
	Server  ServerConfig  `yaml:"server" json:"server"`
}

// Manager 配置管理器（手动依赖注入，无 Wire）
type Manager struct {
	mu       sync.RWMutex
	filePath string
	config   *Config
	watcher  *Watcher
	onReload func(*Config) error
}

// NewManager 创建配置管理器
func NewManager(filePath string) (*Manager, error) {
	m := &Manager{
		filePath: filePath,
	}

	cfg, err := m.load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	m.config = cfg

	return m, nil
}

// load 从文件加载配置，不存在则创建默认
func (m *Manager) load() (*Config, error) {
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return m.createDefault()
		}
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	if cfg.Server.Port == 0 {
		cfg.Server.Port = 3000
	}
	if cfg.Server.StaticDir == "" {
		cfg.Server.StaticDir = "./frontend/web-admin"
	}

	return &cfg, nil
}

// createDefault 创建默认配置
func (m *Manager) createDefault() (*Config, error) {
	cfg := &Config{
		Proxies: []ProxyConfig{},
		Server: ServerConfig{
			Port:      3000,
			StaticDir: "./frontend/web-admin",
			Auth:      AccessAuth{},
		},
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(m.filePath, data, 0644); err != nil {
		return nil, fmt.Errorf("write default config: %w", err)
	}

	return cfg, nil
}

// Validate 验证配置合法性
func (m *Manager) Validate(cfg *Config) error {
	for _, p := range cfg.Proxies {
		if _, err := net.ResolveTCPAddr("tcp", p.RemoteAddress); err != nil {
			return fmt.Errorf("proxy %s: invalid remote address %q: %w", p.Name, p.RemoteAddress, err)
		}
	}
	return nil
}

// GetConfig 获取当前配置（线程安全）
func (m *Manager) GetConfig() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// GetServerPort 获取服务端口
func (m *Manager) GetServerPort() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.Server.Port
}

// Save 保存配置到文件（原子写入）
func (m *Manager) Save(cfg *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.Validate(cfg); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}

	tmpPath := m.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}

	if err := os.Rename(tmpPath, m.filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}

	m.config = cfg

	if m.onReload != nil {
		go m.onReload(cfg)
	}

	return nil
}

// SetReloadCallback 设置配置变更回调
func (m *Manager) SetReloadCallback(fn func(*Config) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onReload = fn
}

// AddProxy 添加代理
func (m *Manager) AddProxy(p ProxyConfig) error {
	cfg := m.GetConfig()
	cfg.Proxies = append(cfg.Proxies, p)
	return m.Save(cfg)
}

// UpdateProxy 更新代理
func (m *Manager) UpdateProxy(id string, p ProxyConfig) error {
	cfg := m.GetConfig()
	found := false
	for i := range cfg.Proxies {
		if cfg.Proxies[i].ID == id {
			p.ID = id // 保持 ID 不变
			cfg.Proxies[i] = p
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("proxy %s not found", id)
	}
	return m.Save(cfg)
}

// RemoveProxy 删除代理
func (m *Manager) RemoveProxy(id string) error {
	cfg := m.GetConfig()
	idx := -1
	for i := range cfg.Proxies {
		if cfg.Proxies[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("proxy %s not found", id)
	}
	cfg.Proxies = append(cfg.Proxies[:idx], cfg.Proxies[idx+1:]...)
	return m.Save(cfg)
}

// Reload 重新加载配置
func (m *Manager) Reload() (*Config, error) {
	cfg, err := m.load()
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.config = cfg
	m.mu.Unlock()

	if m.onReload != nil {
		go m.onReload(cfg)
	}

	return cfg, nil
}

// StartWatcher 启动文件监听（热重载）
func (m *Manager) StartWatcher() error {
	w, err := NewWatcher(m.filePath, func() {
		if _, err := m.Reload(); err != nil {
			fmt.Fprintf(os.Stderr, "config reload error: %v\n", err)
		} else {
			fmt.Println("config reloaded successfully")
		}
	})
	if err != nil {
		return err
	}
	m.watcher = w
	return nil
}

// StopWatcher 停止文件监听
func (m *Manager) StopWatcher() {
	if m.watcher != nil {
		m.watcher.Stop()
	}
}
