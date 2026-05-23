package config

import "strings"

// AccessAuth 服务端鉴权：server.auth.api_key 为逗号分隔的多段密钥，任一段 k 签名为 HEX(SHA1(k + "\\n" + ts + "\\n" + k))。
type AccessAuth struct {
	APIKey string `yaml:"api_key" json:"api_key,omitempty"`
}

// ParseAPIKeyTokens 将 api_key 按逗号切分（去首尾空白），去掉空片段；重复项依次保留首次出现顺序。
func ParseAPIKeyTokens(csv string) []string {
	s := strings.TrimSpace(csv)
	if s == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		t := strings.TrimSpace(part)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// UnifiedTokens 返回 api_key 解析出的可调用的密钥片段。
func (a AccessAuth) UnifiedTokens() []string {
	return ParseAPIKeyTokens(a.APIKey)
}

// Enabled 至少配置了一个有效片段即视为启用鉴权。
func (a AccessAuth) Enabled() bool {
	return len(a.UnifiedTokens()) > 0
}
