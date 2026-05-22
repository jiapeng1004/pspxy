package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"pspxy/internal/config"
	adminembed "pspxy/internal/embed/admin"
	"pspxy/internal/proxy"
)

// Handler API 处理器（手动依赖注入，无 Wire）
type Handler struct {
	proxyMgr *proxy.Manager
	cfgMgr   *config.Manager
	startAt  time.Time
}

// NewHandler 创建 API 处理器
func NewHandler(proxyMgr *proxy.Manager, cfgMgr *config.Manager) *Handler {
	return &Handler{
		proxyMgr: proxyMgr,
		cfgMgr:   cfgMgr,
		startAt:  time.Now(),
	}
}

// Run 启动 HTTP 服务器
func (h *Handler) Run(port int) error {
	r := gin.Default()

	// CORS 中间件
	r.Use(corsMiddleware())
	// 错误恢复中间件
	r.Use(gin.Recovery())

	// 公开：AK/SK 探测与登录校验（不参与签名校验中间件）
	r.GET("/api/v1/auth/enabled", h.AuthEnabled)
	r.POST("/api/v1/auth/login", h.AuthLogin)

	// 需鉴权的 REST（未配置 AK/SK 时中间件直接放行）
	v1 := r.Group("/api/v1")
	v1.Use(GinAccessAuthMiddleware(h.cfgMgr))
	{
		v1.GET("/proxies", h.ListProxies)
		v1.POST("/proxies", h.CreateProxy)
		v1.GET("/proxies/:id", h.GetProxy)
		v1.PUT("/proxies/:id", h.UpdateProxy)
		v1.DELETE("/proxies/:id", h.DeleteProxy)

		v1.POST("/config/reload", h.ReloadConfig)
		v1.GET("/health", h.HealthCheck)
	}

	// WebSocket 隧道：浏览器无法自定义 Header 时可使用 Query（psp_ak/psp_ts/psp_sig）
	r.GET("/ws/:proxy_id", func(c *gin.Context) {
		authCfg := h.cfgMgr.GetConfig().Server.Auth
		if err := VerifyAccessAuth(authCfg, c.Request); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": err.Error(),
			})
			c.Abort()
			return
		}
		h.WebSocketTunnel(c)
	})

	// 静态文件服务（SPA 回退）
	r.NoRoute(h.ServeStatic)

	return r.Run(fmt.Sprintf(":%d", port))
}

// corsMiddleware CORS 中间件
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Psp-Ak,X-Psp-Timestamp,X-Psp-Signature")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// AuthEnabled 返回是否配置了 AK/SK（无需鉴权）。
func (h *Handler) AuthEnabled(c *gin.Context) {
	auth := h.cfgMgr.GetConfig().Server.Auth
	c.JSON(http.StatusOK, gin.H{"auth_required": auth.Enabled()})
}

// AuthLogin 校验提交的 AK/SK 是否与服务端配置一致（无需事先签名）。未启用服务端鉴权时直接返回 ok。
func (h *Handler) AuthLogin(c *gin.Context) {
	auth := h.cfgMgr.GetConfig().Server.Auth
	if !auth.Enabled() {
		c.JSON(http.StatusOK, gin.H{
			"ok":            true,
			"auth_required": false,
		})
		return
	}

	var req struct {
		AccessKey string `json:"access_key" binding:"required"`
		SecretKey string `json:"secret_key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "bad_request",
			"message": "请提交 access_key 与 secret_key",
		})
		return
	}

	if err := VerifyPlainLoginCredentials(auth, req.AccessKey, req.SecretKey); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "unauthorized",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"auth_required": true,
	})
}

// ========== 代理 CRUD ==========

// CreateProxyRequest 创建代理请求
type CreateProxyRequest struct {
	Name          string `json:"name" binding:"required"`
	RemoteAddress string `json:"remote_address" binding:"required"`
	Enabled       bool   `json:"enabled"`
}

// ListProxies 获取代理列表
func (h *Handler) ListProxies(c *gin.Context) {
	enabled := c.Query("enabled")

	proxies := h.proxyMgr.GetAllStatus()

	result := make([]proxy.ProxyStatus, 0)
	for _, p := range proxies {
		if enabled == "true" && !p.Enabled {
			continue
		}
		result = append(result, p)
	}

	c.JSON(http.StatusOK, gin.H{"proxies": result})
}

// CreateProxy 创建代理
func (h *Handler) CreateProxy(c *gin.Context) {
	var req CreateProxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_failed",
			"message": err.Error(),
		})
		return
	}

	pc := config.ProxyConfig{
		ID:            uuid.New().String(),
		Name:          req.Name,
		RemoteAddress: req.RemoteAddress,
		Enabled:       req.Enabled,
	}

	// 保存到 YAML
	if err := h.cfgMgr.AddProxy(pc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "config_save_failed",
			"message": err.Error(),
		})
		return
	}

	// 启动代理
	if err := h.proxyMgr.AddProxy(pc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "proxy_start_failed",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, pc)
}

// GetProxy 获取单个代理
func (h *Handler) GetProxy(c *gin.Context) {
	id := c.Param("id")

	status, err := h.proxyMgr.GetStatus(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, status)
}

// UpdateProxy 更新代理
func (h *Handler) UpdateProxy(c *gin.Context) {
	id := c.Param("id")

	var req CreateProxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_failed",
			"message": err.Error(),
		})
		return
	}

	pc := config.ProxyConfig{
		ID:            id,
		Name:          req.Name,
		RemoteAddress: req.RemoteAddress,
		Enabled:       req.Enabled,
	}

	// 更新 YAML
	if err := h.cfgMgr.UpdateProxy(id, pc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "config_update_failed",
			"message": err.Error(),
		})
		return
	}

	// 更新运行中代理
	if err := h.proxyMgr.UpdateProxy(id, pc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "proxy_update_failed",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, pc)
}

// DeleteProxy 删除代理
func (h *Handler) DeleteProxy(c *gin.Context) {
	id := c.Param("id")

	// 从 YAML 移除
	if err := h.cfgMgr.RemoveProxy(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": err.Error(),
		})
		return
	}

	// 停止运行中代理
	if err := h.proxyMgr.RemoveProxy(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "proxy_remove_failed",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

// ========== 配置管理 ==========

// ReloadConfig 重新加载配置
func (h *Handler) ReloadConfig(c *gin.Context) {
	cfg, err := h.cfgMgr.Reload()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "reload_failed",
			"message": err.Error(),
		})
		return
	}

	// 同步代理状态
	h.proxyMgr.SyncWithConfig(cfg)

	c.JSON(http.StatusOK, gin.H{
		"status":  "reloaded",
		"proxies": len(cfg.Proxies),
	})
}

// HealthCheck 健康检查
func (h *Handler) HealthCheck(c *gin.Context) {
	proxies := h.proxyMgr.GetAllStatus()
	running := 0
	for _, p := range proxies {
		if p.Status == proxy.StatusRunning {
			running++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":          "healthy",
		"uptime":          int(time.Since(h.startAt).Seconds()),
		"proxies_running": running,
		"proxies_total":   len(proxies),
	})
}

// ========== 隧道端点 ==========

// WebSocketTunnel WebSocket 隧道：`GET /ws/:proxy_id`，与 REST API 共享服务端端口。
func (h *Handler) WebSocketTunnel(c *gin.Context) {
	proxyID := c.Param("proxy_id")

	ts, err := h.proxyMgr.GetTunnelServer(proxyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": err.Error(),
		})
		return
	}

	pc := ts.Config()
	if !pc.Enabled {
		c.JSON(http.StatusForbidden, gin.H{
			"error":   "disabled",
			"message": "proxy is disabled",
		})
		return
	}
	if !ts.IsRunning() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "unavailable",
			"message": "proxy is not accepting tunnels",
		})
		return
	}

	ts.IncrTunnel()
	defer ts.DecrTunnel()

	h.proxyMgr.GetWSUpgrader().HandleUpgrade(c.Writer, c.Request, pc.RemoteAddress)
}

// ServeStatic SPA 静态文件回退
func (h *Handler) ServeStatic(c *gin.Context) {
	// 如果是 API 路径，返回 404
	if len(c.Request.URL.Path) >= 4 && c.Request.URL.Path[:4] == "/api" {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "endpoint not found",
		})
		return
	}

	// 使用内嵌文件服务
	adminembed.CreateFileServer().ServeHTTP(c.Writer, c.Request)
}
