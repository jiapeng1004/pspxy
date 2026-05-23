package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"pspxy/internal/config"
	adminembed "pspxy/internal/embed/admin"
	"pspxy/internal/proxy"
	"pspxy/internal/reversetunnel"
)

// Handler API 处理器（手动依赖注入，无 Wire）
type Handler struct {
	proxyMgr *proxy.Manager
	cfgMgr   *config.Manager
	rt       *reversetunnel.Broker
	startAt  time.Time
}

// NewHandler 创建 API 处理器
func NewHandler(proxyMgr *proxy.Manager, cfgMgr *config.Manager) *Handler {
	return &Handler{
		proxyMgr: proxyMgr,
		cfgMgr:   cfgMgr,
		rt:       reversetunnel.NewBroker(),
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

	// 公开：鉴权探测与明文登录校验（不参与签名校验中间件）
	r.GET("/api/v1/auth/enabled", h.AuthEnabled)
	r.POST("/api/v1/auth/login", h.AuthLogin)

	// 需鉴权的 REST（未配置鉴权时中间件直接放行）
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

	// WebSocket 统一入口：`GET /ws/:tunnel_id`
	// — 若为管理端登记且启用的 Proxy，服务端直连 remote_address；
	// — 否则若为合法 UUID 且存在活跃 Reverse Channel，则为 Consumer 附着（与正向 Consumer 语义一致）。
	// 遗留路径 `GET /ws/rtunnel/session/:channel_id` 仍可用，仅从 Reverse Broker 附着（不进行 Proxy 分流）。
	r.GET("/ws/:tunnel_id", func(c *gin.Context) {
		authCfg := h.cfgMgr.GetConfig().Server.Auth
		if err := VerifyAccessAuth(authCfg, c.Request); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": err.Error(),
			})
			c.Abort()
			return
		}
		h.TunnelIngress(c)
	})

	// 反向隧道 Port Provider：`/ws/rtunnel/provider`（Consumer 推荐使用统一 ingress `/ws/:tunnel_id`，见 TunnelIngress）。
	r.GET("/ws/rtunnel/provider", func(c *gin.Context) {
		authCfg := h.cfgMgr.GetConfig().Server.Auth
		if err := VerifyAccessAuth(authCfg, c.Request); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": err.Error(),
			})
			c.Abort()
			return
		}
		h.RTunnelProvider(c)
	})
	r.GET("/ws/rtunnel/session/:channel_id", func(c *gin.Context) {
		authCfg := h.cfgMgr.GetConfig().Server.Auth
		if err := VerifyAccessAuth(authCfg, c.Request); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": err.Error(),
			})
			c.Abort()
			return
		}
		h.RTunnelConsumer(c)
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

// AuthEnabled 返回是否已启用鉴权（无需鉴权）。
func (h *Handler) AuthEnabled(c *gin.Context) {
	auth := h.cfgMgr.GetConfig().Server.Auth
	c.JSON(http.StatusOK, gin.H{"auth_required": auth.Enabled()})
}

// AuthLogin 明文校验提交的 api_key。未启用鉴权时直接返回 ok。
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
		APIKey string `json:"api_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "bad_request",
			"message": "无效的 JSON",
		})
		return
	}

	if err := VerifyPlainCredential(auth, req.APIKey); err != nil {
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

// TunnelIngress 统一入口 `GET /ws/:tunnel_id`：
// — 若为管理端登记且启用的 Proxy，服务端直连 remote_address（TCP Provider）
// — 否则若为 UUID 且在 Reverse Broker 中活跃，按 Consumer 附着（出站 Provider + Broker 中继）
//
// （若管理与动态通道碰巧使用同一 UUID，优先 Proxy 语义。）
func (h *Handler) TunnelIngress(c *gin.Context) {
	tunnelID := strings.TrimSpace(c.Param("tunnel_id"))
	if tunnelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "bad_request",
			"message": "missing tunnel id",
		})
		return
	}

	ts, perr := h.proxyMgr.GetTunnelServer(tunnelID)
	if perr == nil {
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
		return
	}

	chID, uerr := uuid.Parse(tunnelID)
	if uerr != nil || chID == uuid.Nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "tunnel not registered",
		})
		return
	}
	if !h.rt.ChannelActive(chID) {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "tunnel not registered or provider offline",
		})
		return
	}
	wsU := h.proxyMgr.GetWSUpgrader()
	conn, err := wsU.UpgradeConn(c.Writer, c.Request)
	if err != nil {
		return
	}
	if err := h.rt.AttachConsumer(chID, conn); err != nil {
		return
	}
}

// RTunnelConsumer 遗留路径：`GET /ws/rtunnel/session/:channel_id` — 仅从 Reverse Broker 附着，不尝试 Proxy 分流。
func (h *Handler) RTunnelConsumer(c *gin.Context) {
	chStr := strings.TrimSpace(c.Param("channel_id"))
	chID, err := uuid.Parse(chStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "bad_request",
			"message": "invalid channel_id",
		})
		return
	}
	if !h.rt.ChannelActive(chID) {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "tunnel not registered or provider offline",
		})
		return
	}
	wsU := h.proxyMgr.GetWSUpgrader()
	conn, err := wsU.UpgradeConn(c.Writer, c.Request)
	if err != nil {
		return
	}
	if err := h.rt.AttachConsumer(chID, conn); err != nil {
		return
	}
}

// RTunnelProvider 反向隧道暴露端：首条 Text 为 Offer JSON（可含可选 channel_id 以 reclaim）；服务端返回确认 channel_id。
func (h *Handler) RTunnelProvider(c *gin.Context) {
	wsU := h.proxyMgr.GetWSUpgrader()
	conn, err := wsU.UpgradeConn(c.Writer, c.Request)
	if err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	_, data, err := conn.ReadMessage()
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		_ = conn.Close()
		return
	}
	var offer reversetunnel.Offer
	if err := json.Unmarshal(data, &offer); err != nil {
		b, _ := json.Marshal(map[string]any{"ok": false, "error": "invalid offer json"})
		_ = conn.WriteMessage(websocket.TextMessage, b)
		_ = conn.Close()
		return
	}
	chID, startReadLoop, err := h.rt.RegisterProvider(conn, offer)
	if err != nil {
		b, _ := json.Marshal(map[string]any{"ok": false, "error": err.Error()})
		_ = conn.WriteMessage(websocket.TextMessage, b)
		_ = conn.Close()
		return
	}
	resp, _ := json.Marshal(map[string]any{"ok": true, "channel_id": chID.String()})
	if err := conn.WriteMessage(websocket.TextMessage, resp); err != nil {
		h.rt.AbortProviderRegistration(chID)
		_ = conn.Close()
		return
	}
	startReadLoop()
	// Broker 读循环接管 conn（关闭时卸载 channel）
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
