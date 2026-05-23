package api

import (
	"crypto/sha1"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"pspxy/internal/config"
)

const (
	hdrAccessKey   = "X-Psp-Ak"
	hdrTimestamp   = "X-Psp-Timestamp"
	hdrSignature   = "X-Psp-Signature"
	queryAccessKey = "psp_ak"
	queryTs        = "psp_ts"
	querySig       = "psp_sig"

	maxClockSkew = 5 * time.Minute
)

// SignAccessPayload 单列 api_key：调用方传入同一段 k 两次，签名 hex(SHA1(k + "\n" + unixSec + "\n" + k))。
func SignAccessPayload(accessKey, unixSec, secretKey string) string {
	canonical := strings.TrimSpace(accessKey) + "\n" + strings.TrimSpace(unixSec) + "\n" + strings.TrimSpace(secretKey)
	sum := sha1.Sum([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

func subtleStringEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func verifySigAgainst(expectHexLower, got string) bool {
	want := strings.ToLower(strings.TrimSpace(expectHexLower))
	got = strings.ToLower(strings.TrimSpace(got))
	if len(want) != len(got) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// VerifyAccessAuth 校验单列 api_key 签名（Header 优先，其次 Query）。
func VerifyAccessAuth(authCfg config.AccessAuth, r *http.Request) error {
	if !authCfg.Enabled() || r.Method == http.MethodOptions {
		return nil
	}

	var ak string
	if hk := strings.TrimSpace(r.Header.Get(hdrAccessKey)); hk != "" {
		ak = hk
	} else {
		ak = strings.TrimSpace(r.URL.Query().Get(queryAccessKey))
	}

	ts := strings.TrimSpace(r.Header.Get(hdrTimestamp))
	if ts == "" {
		ts = strings.TrimSpace(r.URL.Query().Get(queryTs))
	}

	sig := strings.TrimSpace(r.Header.Get(hdrSignature))
	if sig == "" {
		sig = strings.TrimSpace(r.URL.Query().Get(querySig))
	}

	if ak == "" || ts == "" || sig == "" {
		return fmt.Errorf("缺少鉴权参数（需提供 %s、%s、%s）", hdrAccessKey, hdrTimestamp, hdrSignature)
	}

	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("无效的时间戳")
	}
	nowUnix := time.Now().Unix()
	skewSec := int64(maxClockSkew / time.Second)
	if sec < nowUnix-skewSec || sec > nowUnix+skewSec {
		return fmt.Errorf("时间戳不在允许偏差内（±%v）", maxClockSkew)
	}

	for _, tok := range authCfg.UnifiedTokens() {
		if !subtleStringEq(ak, tok) {
			continue
		}
		want := SignAccessPayload(tok, ts, tok)
		if verifySigAgainst(want, sig) {
			return nil
		}
		return fmt.Errorf("签名无效")
	}

	return fmt.Errorf("无效的密钥")
}

// VerifyPlainCredential 明文登录：提交的 api_key 须与配置任一片段完全一致。
func VerifyPlainCredential(authCfg config.AccessAuth, apiKeySubmitted string) error {
	if !authCfg.Enabled() {
		return nil
	}
	api := strings.TrimSpace(apiKeySubmitted)
	if api == "" {
		return fmt.Errorf("凭据无效")
	}
	for _, tok := range authCfg.UnifiedTokens() {
		if subtleStringEq(api, tok) {
			return nil
		}
	}
	return fmt.Errorf("凭据无效")
}

// GinAccessAuthMiddleware 对需保护的 HTTP 路由做鉴权；未启用时放行。
func GinAccessAuthMiddleware(cfgMgr *config.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		authCfg := cfgMgr.GetConfig().Server.Auth
		if err := VerifyAccessAuth(authCfg, c.Request); err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": err.Error(),
			})
			return
		}
		c.Next()
	}
}
