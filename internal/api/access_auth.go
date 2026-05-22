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

// SignAccessPayload 与前端一致的签名：十六进制 HEX(SHA1(ak + "\n" + ts + "\n" + sk))，ts 为 Unix 秒数字符串。
func SignAccessPayload(accessKey, unixSec, secretKey string) string {
	canonical := strings.TrimSpace(accessKey) + "\n" + strings.TrimSpace(unixSec) + "\n" + strings.TrimSpace(secretKey)
	sum := sha1.Sum([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// VerifyAccessAuth 校验 AK/SK 签名（Header 优先，其次 Query——供浏览器 WebSocket 等无法自定义 Header 的场景）。
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

	expectAK := strings.TrimSpace(authCfg.AccessKey)
	expectSK := strings.TrimSpace(authCfg.SecretKey)
	if subtle.ConstantTimeCompare([]byte(ak), []byte(expectAK)) != 1 {
		return fmt.Errorf("无效的 Access Key")
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

	want := strings.ToLower(SignAccessPayload(expectAK, ts, expectSK))
	got := strings.ToLower(sig)
	if len(want) != len(got) {
		return fmt.Errorf("签名无效")
	}
	if subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
		return fmt.Errorf("签名无效")
	}
	return nil
}

// VerifyPlainLoginCredentials 登录接口校验：服务端已启用 AK/SK 时，比对请求体中的 AK/SK 与配置是否一致。
func VerifyPlainLoginCredentials(authCfg config.AccessAuth, accessKey, secretKey string) error {
	if !authCfg.Enabled() {
		return nil
	}
	expectAK := strings.TrimSpace(authCfg.AccessKey)
	expectSK := strings.TrimSpace(authCfg.SecretKey)
	gotAK := strings.TrimSpace(accessKey)
	gotSK := strings.TrimSpace(secretKey)
	if gotAK == "" || gotSK == "" {
		return fmt.Errorf("凭据无效")
	}
	if subtle.ConstantTimeCompare([]byte(gotAK), []byte(expectAK)) != 1 {
		return fmt.Errorf("凭据无效")
	}
	if subtle.ConstantTimeCompare([]byte(gotSK), []byte(expectSK)) != 1 {
		return fmt.Errorf("凭据无效")
	}
	return nil
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
