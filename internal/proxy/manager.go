package proxy

import (
	"fmt"
	"log"
	"sync"

	"pspxy/internal/config"
)

// Manager 代理管理器（手动依赖注入，无 Wire）
type Manager struct {
	mu      sync.RWMutex
	servers map[string]*TCPServer
	cfgMgr  *config.Manager
	wsUp    *WSUpgrader
}

// NewManager 创建代理管理器
func NewManager(cfgMgr *config.Manager) *Manager {
	return &Manager{
		servers: make(map[string]*TCPServer),
		cfgMgr:  cfgMgr,
		wsUp:    NewWSUpgrader(),
	}
}

// StartAll 启动所有启用的代理（登记路由，不占独立监听端口）
func (m *Manager) StartAll() error {
	cfg := m.cfgMgr.GetConfig()
	for _, pc := range cfg.Proxies {
		if err := m.AddProxy(pc); err != nil {
			log.Printf("start proxy [%s] failed: %v", pc.Name, err)
			continue
		}
	}
	return nil
}

// StopAll 停止所有代理登记
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, s := range m.servers {
		s.Stop()
		delete(m.servers, id)
	}
}

// AddProxy 添加并启动代理
func (m *Manager) AddProxy(cfg config.ProxyConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.servers[cfg.ID]; exists {
		return fmt.Errorf("proxy %s already exists", cfg.ID)
	}

	s := NewTCPServer(cfg)
	if cfg.Enabled {
		if err := s.Start(); err != nil {
			return err
		}
	}
	m.servers[cfg.ID] = s
	return nil
}

// RemoveProxy 移除并停止代理
func (m *Manager) RemoveProxy(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, exists := m.servers[id]
	if !exists {
		return fmt.Errorf("proxy %s not found", id)
	}

	s.Stop()
	delete(m.servers, id)
	return nil
}

// UpdateProxy 更新代理配置
func (m *Manager) UpdateProxy(id string, cfg config.ProxyConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	old, exists := m.servers[id]
	if !exists {
		return fmt.Errorf("proxy %s not found", id)
	}

	old.Stop()

	s := NewTCPServer(cfg)
	if cfg.Enabled {
		if err := s.Start(); err != nil {
			if old.cfg.Enabled {
				old.Start()
			}
			m.servers[id] = old
			return err
		}
	}
	m.servers[id] = s
	return nil
}

// GetAllStatus 获取所有代理状态
func (m *Manager) GetAllStatus() []ProxyStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]ProxyStatus, 0, len(m.servers))
	for _, s := range m.servers {
		result = append(result, s.StatusInfo())
	}
	return result
}

// GetStatus 获取单个代理状态
func (m *Manager) GetStatus(id string) (ProxyStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, exists := m.servers[id]
	if !exists {
		return ProxyStatus{}, fmt.Errorf("proxy %s not found", id)
	}
	return s.StatusInfo(), nil
}

// GetWSUpgrader 获取 WebSocket 升级器
func (m *Manager) GetWSUpgrader() *WSUpgrader {
	return m.wsUp
}

// GetTunnelServer 返回用于会话计数的运行时对象
func (m *Manager) GetTunnelServer(proxyID string) (*TCPServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.servers[proxyID]
	if !ok {
		return nil, fmt.Errorf("proxy %s not found", proxyID)
	}
	return s, nil
}

func (m *Manager) SyncWithConfig(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()

	activeIDs := make(map[string]bool)

	for _, pc := range cfg.Proxies {
		activeIDs[pc.ID] = true

		if s, exists := m.servers[pc.ID]; exists {
			oldCfg := s.Config()
			if oldCfg.Enabled != pc.Enabled ||
				oldCfg.RemoteAddress != pc.RemoteAddress ||
				oldCfg.Name != pc.Name {

				log.Printf("reloading proxy [%s]", pc.Name)
				s.Stop()

				ns := NewTCPServer(pc)
				if pc.Enabled {
					if err := ns.Start(); err != nil {
						log.Printf("reload proxy [%s] failed: %v", pc.Name, err)
						continue
					}
				}
				m.servers[pc.ID] = ns
			} else if err := s.ReloadConfig(pc); err != nil {
				log.Printf("reload cfg proxy [%s] failed: %v", pc.Name, err)
			}
		} else {
			ns := NewTCPServer(pc)
			if pc.Enabled {
				if err := ns.Start(); err != nil {
					log.Printf("add proxy [%s] failed: %v", pc.Name, err)
					continue
				}
			}
			m.servers[pc.ID] = ns
		}
	}

	for id, s := range m.servers {
		if !activeIDs[id] {
			log.Printf("removing proxy [%s]", s.Config().Name)
			s.Stop()
			delete(m.servers, id)
		}
	}
}
