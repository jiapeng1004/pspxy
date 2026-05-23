// Package clientconfig 将客户端多代理隧道参数持久化到本地 YAML（config-client.yaml）。
package clientconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"pspxy/internal/clienttunnel"
)

// ErrNoClientConfigFile 配置文件不存在。
var ErrNoClientConfigFile = errors.New("客户端配置文件不存在")

type fileUnified struct {
	Version          int                                   `yaml:"version,omitempty"`
	Proxies          []clienttunnel.Config                 `yaml:"proxies,omitempty"`
	ReverseProviders []clienttunnel.ReverseProviderPersist `yaml:"reverse_providers,omitempty"`
	ReverseConsumers []clienttunnel.ReverseConsumerPersist `yaml:"reverse_consumers,omitempty"`
}

// LoadClientFile 读取 path，返回正向代理与反向条目；不存在则 ErrNoClientConfigFile。
func LoadClientFile(path string) (
	proxies []clienttunnel.Config,
	reverseProviders []clienttunnel.ReverseProviderPersist,
	reverseConsumers []clienttunnel.ReverseConsumerPersist,
	err error,
) {
	path = filepath.Clean(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil, ErrNoClientConfigFile
		}
		return nil, nil, nil, fmt.Errorf("read client config: %w", err)
	}

	var f fileUnified
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, nil, nil, fmt.Errorf("parse client yaml: %w", err)
	}
	if len(f.Proxies) > 0 {
		proxies = make([]clienttunnel.Config, len(f.Proxies))
		for i := range f.Proxies {
			proxies[i] = trimCfg(f.Proxies[i])
		}
	} else {
		var legacy clienttunnel.Config
		if err := yaml.Unmarshal(data, &legacy); err != nil {
			return nil, nil, nil, fmt.Errorf("parse legacy client yaml: %w", err)
		}
		legacy = trimCfg(legacy)
		if !(strings.TrimSpace(legacy.ServerURL) == "" || strings.TrimSpace(legacy.ProxyID) == "") {
			if strings.TrimSpace(legacy.ID) == "" {
				legacy.ID = clienttunnel.DefaultSingleProxyID
			}
			proxies = []clienttunnel.Config{legacy}
		}
	}
	rp := make([]clienttunnel.ReverseProviderPersist, len(f.ReverseProviders))
	for i := range f.ReverseProviders {
		rp[i] = trimReverseProv(f.ReverseProviders[i])
	}
	rc := make([]clienttunnel.ReverseConsumerPersist, len(f.ReverseConsumers))
	for i := range f.ReverseConsumers {
		rc[i] = trimReverseCon(f.ReverseConsumers[i])
	}
	return proxies, rp, rc, nil
}

// LoadProxies 读取 path，仅返回代理列表；兼容旧版根级单条 YAML。
func LoadProxies(path string) ([]clienttunnel.Config, error) {
	proxies, _, _, err := LoadClientFile(path)
	return proxies, err
}

// SaveClientFile 写入完整客户端配置（含反向隧道条目）。
func SaveClientFile(path string, proxies []clienttunnel.Config,
	reverseProviders []clienttunnel.ReverseProviderPersist,
	reverseConsumers []clienttunnel.ReverseConsumerPersist,
) error {
	path = filepath.Clean(path)
	for i := range proxies {
		proxies[i] = trimCfg(proxies[i])
		if strings.TrimSpace(proxies[i].ID) == "" {
			proxies[i].ID = clienttunnel.EnsureProxyID("")
		}
	}
	for i := range reverseProviders {
		reverseProviders[i] = trimReverseProv(reverseProviders[i])
	}
	for i := range reverseConsumers {
		reverseConsumers[i] = trimReverseCon(reverseConsumers[i])
	}

	payload := fileUnified{
		Version:          2,
		Proxies:          proxies,
		ReverseProviders: reverseProviders,
		ReverseConsumers: reverseConsumers,
	}
	data, err := yaml.Marshal(&payload)
	if err != nil {
		return fmt.Errorf("marshal client yaml: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("mkdir for client config: %w", err)
	}

	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("temp client config: %w", err)
	}
	tmpPath := f.Name()

	writeErr := func() error {
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("write temp client config: %w", err)
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("sync temp client config: %w", err)
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("close temp client config: %w", err)
		}
		return nil
	}()

	if writeErr != nil {
		return writeErr
	}
	_ = os.Chmod(tmpPath, 0600)

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename client config: %w", err)
	}
	return nil
}

// SaveProxies 写入代理列表，并尽力保留文件中已有反向条目（若文件不存在则从空反向开始）。
func SaveProxies(path string, proxies []clienttunnel.Config) error {
	rprov, rcon := loadReverseSidesOrEmpty(path)
	return SaveClientFile(path, proxies, rprov, rcon)
}

func loadReverseSidesOrEmpty(path string) ([]clienttunnel.ReverseProviderPersist, []clienttunnel.ReverseConsumerPersist) {
	_, rp, rc, err := LoadClientFile(path)
	if errors.Is(err, ErrNoClientConfigFile) {
		return nil, nil
	}
	if err != nil {
		return nil, nil
	}
	return rp, rc
}

func trimReverseProv(p clienttunnel.ReverseProviderPersist) clienttunnel.ReverseProviderPersist {
	p.ServerURL = clienttunnel.NormalizeServerURLForWS(strings.TrimSpace(p.ServerURL))
	p.LocalHost = strings.TrimSpace(p.LocalHost)
	if p.LocalHost == "" {
		p.LocalHost = "127.0.0.1"
	}
	p.APIKey = strings.TrimSpace(p.APIKey)
	p.ID = strings.TrimSpace(p.ID)
	p.ChannelID = strings.TrimSpace(p.ChannelID)
	return p
}

func trimReverseCon(c clienttunnel.ReverseConsumerPersist) clienttunnel.ReverseConsumerPersist {
	return clienttunnel.NormalizeReverseConsumerPersist(c)
}

func trimCfg(c clienttunnel.Config) clienttunnel.Config {
	c.ServerURL = clienttunnel.NormalizeServerURLForWS(strings.TrimSpace(c.ServerURL))
	c.ProxyID = strings.TrimSpace(c.ProxyID)
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.ID = strings.TrimSpace(c.ID)
	return c
}
