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

type fileV1 struct {
	Version int                   `yaml:"version,omitempty"`
	Proxies []clienttunnel.Config `yaml:"proxies"`
}

// LoadProxies 读取 path，返回代理列表；文件不存在返回 ErrNoClientConfigFile。
// 兼容旧版「根级单条」YAML（无 proxies 键）。
func LoadProxies(path string) ([]clienttunnel.Config, error) {
	path = filepath.Clean(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoClientConfigFile
		}
		return nil, fmt.Errorf("read client config: %w", err)
	}

	var f fileV1
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse client yaml: %w", err)
	}
	if len(f.Proxies) > 0 {
		out := make([]clienttunnel.Config, len(f.Proxies))
		for i := range f.Proxies {
			out[i] = trimCfg(f.Proxies[i])
		}
		return out, nil
	}

	var legacy clienttunnel.Config
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("parse legacy client yaml: %w", err)
	}
	legacy = trimCfg(legacy)
	if strings.TrimSpace(legacy.ServerURL) == "" || strings.TrimSpace(legacy.ProxyID) == "" {
		return []clienttunnel.Config{}, nil
	}
	if strings.TrimSpace(legacy.ID) == "" {
		legacy.ID = clienttunnel.DefaultSingleProxyID
	}
	return []clienttunnel.Config{legacy}, nil
}

// SaveProxies 将代理列表写入 path（密钥随配置落盘）。
func SaveProxies(path string, proxies []clienttunnel.Config) error {
	path = filepath.Clean(path)
	for i := range proxies {
		proxies[i] = trimCfg(proxies[i])
		if strings.TrimSpace(proxies[i].ID) == "" {
			proxies[i].ID = clienttunnel.EnsureProxyID("")
		}
	}

	payload := fileV1{Version: 1, Proxies: proxies}
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

func trimCfg(c clienttunnel.Config) clienttunnel.Config {
	c.ServerURL = strings.TrimSpace(c.ServerURL)
	c.ProxyID = strings.TrimSpace(c.ProxyID)
	c.AccessKey = strings.TrimSpace(c.AccessKey)
	c.SecretKey = strings.TrimSpace(c.SecretKey)
	c.ID = strings.TrimSpace(c.ID)
	return c
}
