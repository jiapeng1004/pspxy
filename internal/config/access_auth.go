package config

import "strings"

// AccessAuth AK/SK 简易鉴权；access_key、secret_key 均非空时视为开启。
type AccessAuth struct {
	AccessKey string `yaml:"access_key" json:"access_key,omitempty"`
	SecretKey string `yaml:"secret_key" json:"secret_key,omitempty"`
}

// Enabled 是否启用鉴权
func (a AccessAuth) Enabled() bool {
	return strings.TrimSpace(a.AccessKey) != "" && strings.TrimSpace(a.SecretKey) != ""
}
