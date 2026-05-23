package clienttunnel

import (
	"context"
	"fmt"
	"log"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// ReverseManager 管理多条反向隧道实例（provider / consumer），可被 Web UI 启停。
type ReverseManager struct {
	mu        sync.Mutex
	providers map[string]*revProviderInst
	consumers map[string]*revConsumerInst
}

type revProviderInst struct {
	cfg        ReverseProviderPersist
	channelID  string
	ws         *websocket.Conn
	serveDone  chan struct{}
	lastRunErr error
}

type revConsumerInst struct {
	cfg           ReverseConsumerPersist
	done          chan struct{}
	lastRunErr    error
	cancel        context.CancelFunc
	listener      net.Listener
	activeBridges atomic.Int64
}

// NewReverseManager 创建空的反向隧道管理器。
func NewReverseManager() *ReverseManager {
	return &ReverseManager{
		providers: make(map[string]*revProviderInst),
		consumers: make(map[string]*revConsumerInst),
	}
}

func normalizeReverseProviderPersist(p ReverseProviderPersist) ReverseProviderPersist {
	p.ServerURL = NormalizeServerURLForWS(strings.TrimSpace(p.ServerURL))
	p.LocalHost = strings.TrimSpace(p.LocalHost)
	if p.LocalHost == "" {
		p.LocalHost = "127.0.0.1"
	}
	p.APIKey = strings.TrimSpace(p.APIKey)
	p.ID = strings.TrimSpace(p.ID)
	p.ChannelID = strings.TrimSpace(p.ChannelID)
	return p
}

func normalizeReverseConsumerPersist(c ReverseConsumerPersist) ReverseConsumerPersist {
	return NormalizeReverseConsumerPersist(c)
}

func (r *ReverseManager) validateProvider(p ReverseProviderPersist) error {
	if strings.TrimSpace(p.ServerURL) == "" {
		return fmt.Errorf("缺少 server_url")
	}
	if p.LocalPort < 1 || p.LocalPort > 65535 {
		return fmt.Errorf("local_port 无效")
	}
	return nil
}

func (r *ReverseManager) validateConsumer(c ReverseConsumerPersist) error {
	if strings.TrimSpace(c.ServerURL) == "" || c.Listen == "" || strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("server_url、listen、隧道 id（UUID）均不能为空")
	}
	return nil
}

func (r *ReverseManager) snapshotProvider(inst *revProviderInst) ReverseProviderRow {
	auth := AuthSecretsConfigured(inst.cfg.APIKey)
	chDisp := strings.TrimSpace(inst.channelID)
	if chDisp == "" {
		chDisp = strings.TrimSpace(inst.cfg.ChannelID)
	}
	row := ReverseProviderRow{
		ID:           inst.cfg.ID,
		Running:      inst.ws != nil,
		ChannelID:    chDisp,
		LocalHost:    inst.cfg.LocalHost,
		LocalPort:    inst.cfg.LocalPort,
		ServerURL:    inst.cfg.ServerURL,
		AuthHintOnly: auth,
		Enabled:      inst.cfg.EffectiveEnabled(),
	}
	if !row.Running && inst.lastRunErr != nil {
		row.Error = inst.lastRunErr.Error()
	}
	return row
}

func (r *ReverseManager) snapshotConsumer(inst *revConsumerInst) ReverseConsumerRow {
	auth := AuthSecretsConfigured(inst.cfg.APIKey)
	row := ReverseConsumerRow{
		ID:                inst.cfg.ID,
		Running:           inst.cancel != nil,
		Listen:            inst.cfg.Listen,
		ServerURL:         inst.cfg.ServerURL,
		AuthHintOnly:      auth,
		ActiveConnections: inst.activeBridges.Load(),
		Enabled:           inst.cfg.EffectiveEnabled(),
	}
	if !row.Running && inst.lastRunErr != nil {
		row.Error = inst.lastRunErr.Error()
	}
	return row
}

func (r *ReverseManager) waitProviderEnded(inst *revProviderInst) {
	if inst == nil || inst.serveDone == nil {
		return
	}
	if inst.ws != nil {
		_ = inst.ws.Close()
	}
	<-inst.serveDone
}

func (r *ReverseManager) waitConsumerEnded(inst *revConsumerInst) {
	if inst == nil {
		return
	}
	if inst.listener != nil {
		_ = inst.listener.Close()
	}
	if inst.cancel != nil {
		inst.cancel()
	}
	if inst.done != nil {
		<-inst.done
	}
}

func fuzzyHostMatch(a, b string) bool {
	na := strings.TrimSpace(a)
	if na == "" {
		na = "127.0.0.1"
	}
	nb := strings.TrimSpace(b)
	if nb == "" {
		nb = "127.0.0.1"
	}
	if na == nb {
		return true
	}
	if (na == "127.0.0.1" || na == "localhost") && (nb == "127.0.0.1" || nb == "localhost") {
		return true
	}
	return false
}

func tcpListenPort(addr string) (int, bool) {
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return -1, false
	}
	_ = host
	p, err := strconv.Atoi(portStr)
	if err != nil || p < 1 {
		return -1, false
	}
	return p, true
}

// ListenHostPortCollidesExpose 判断 consumer 监听地址是否与「暴露目标的 host:port」在 TCP 语义上争抢同一端口。
func listenHostPortCollidesExpose(listenAddr, exposeHost string, exposePort int) bool {
	lp, ok := tcpListenPort(listenAddr)
	if !ok || lp != exposePort {
		return false
	}
	lh, _, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil {
		return false
	}
	eh := strings.TrimSpace(exposeHost)
	if eh == "" {
		eh = "127.0.0.1"
	}
	return fuzzyHostMatch(lh, eh)
}

func tcpBindingsEquiv(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	ha, pa, ea := tcpListenParts(a)
	hb, pb, eb := tcpListenParts(b)
	if !ea || !eb {
		return a == b
	}
	return pa == pb && fuzzyHostMatch(ha, hb)
}

func tcpListenParts(addr string) (host string, port int, ok bool) {
	host, ps, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return "", -1, false
	}
	p, err := strconv.Atoi(ps)
	if err != nil {
		return "", -1, false
	}
	return host, p, true
}

func (r *ReverseManager) MergePreserveProviderTunnelID(p ReverseProviderPersist) ReverseProviderPersist {
	p = normalizeReverseProviderPersist(p)
	if strings.TrimSpace(p.ChannelID) != "" {
		return p
	}
	id := strings.TrimSpace(p.ID)
	if id == "" {
		return p
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prev, ok := r.providers[id]; ok && prev != nil {
		if ch := strings.TrimSpace(prev.cfg.ChannelID); ch != "" {
			p.ChannelID = ch
		} else if ch := strings.TrimSpace(prev.channelID); ch != "" {
			p.ChannelID = ch
		}
	}
	return p
}

// StartProvider 启动或重启暴露端。
// 若配置中带有上次成功的 channel_id，则优先向服务端 reclaim；握手失败或未配置时再申请新的隧道 UUID。
func (r *ReverseManager) StartProvider(p ReverseProviderPersist) (channelID string, err error) {
	p = normalizeReverseProviderPersist(p)
	if strings.TrimSpace(p.ID) == "" {
		p.ID = NewProxyID()
	}
	if err := r.validateProvider(p); err != nil {
		return "", err
	}

	r.mu.Lock()
	prev := r.providers[p.ID]
	if prev != nil {
		r.mu.Unlock()
		r.waitProviderEnded(prev)
		r.mu.Lock()
	}
	if err := r.checkProviderConflictsLocked(p.ID, &p); err != nil {
		r.mu.Unlock()
		return "", err
	}
	r.mu.Unlock()

	if !p.EffectiveEnabled() {
		r.StopProvider(p.ID)
		if err := r.putProviderInactive(p); err != nil {
			return "", err
		}
		return "", nil
	}

	preferred := strings.TrimSpace(p.ChannelID)
	var ch string
	var ws *websocket.Conn
	var herr error

	if preferred != "" {
		ch, ws, herr = HandshakeReverseProvider(p.ServerURL, p.LocalHost, p.LocalPort, p.APIKey, preferred)
		if herr != nil {
			log.Printf("[reverse-provider] reclaim 隧道 UUID=%s 失败: %v，将申请新 UUID", preferred, herr)
			ch, ws, herr = HandshakeReverseProvider(p.ServerURL, p.LocalHost, p.LocalPort, p.APIKey, "")
		}
	} else {
		ch, ws, herr = HandshakeReverseProvider(p.ServerURL, p.LocalHost, p.LocalPort, p.APIKey, "")
	}

	if herr != nil {
		r.mu.Lock()
		r.providers[p.ID] = &revProviderInst{cfg: p, channelID: "", lastRunErr: herr}
		r.mu.Unlock()
		return "", herr
	}

	persistCfg := p
	persistCfg.ChannelID = strings.TrimSpace(ch)

	serveDone := make(chan struct{})
	inst := &revProviderInst{cfg: persistCfg, channelID: ch, ws: ws, serveDone: serveDone}
	r.mu.Lock()
	r.providers[p.ID] = inst
	r.mu.Unlock()

	go func(wsRef *websocket.Conn, hh string, lp int, rowID string) {
		defer close(serveDone)
		defer func() { _ = wsRef.Close() }()
		runErr := ServeReverseProvider(wsRef, hh, lp)
		r.mu.Lock()
		defer r.mu.Unlock()
		cur := r.providers[rowID]
		if cur != nil && cur.ws == wsRef {
			cur.lastRunErr = runErr
			cur.ws = nil
			cur.serveDone = nil
		}
	}(ws, persistCfg.LocalHost, persistCfg.LocalPort, persistCfg.ID)

	log.Printf("[reverse-provider] ui id=%s channel_id=%s 本机=%s:%d", persistCfg.ID, ch, persistCfg.LocalHost, persistCfg.LocalPort)
	return ch, nil
}

func (r *ReverseManager) checkProviderConflictsLocked(selfID string, p *ReverseProviderPersist) error {
	expHost := strings.TrimSpace(p.LocalHost)
	if expHost == "" {
		expHost = "127.0.0.1"
	}

	for cid, ct := range r.consumers {
		if listenHostPortCollidesExpose(ct.cfg.Listen, expHost, p.LocalPort) {
			return fmt.Errorf("本地接入端监听 %s 会与暴露目标 %s:%d 争抢同一监听端口",
				strings.TrimSpace(ct.cfg.Listen), expHost, p.LocalPort)
		}
		_ = cid
	}

	for pid, ot := range r.providers {
		if pid == selfID {
			continue
		}
		ph := strings.TrimSpace(ot.cfg.LocalHost)
		if ph == "" {
			ph = "127.0.0.1"
		}
		if ot.cfg.LocalPort == p.LocalPort && fuzzyHostMatch(ph, expHost) {
			return fmt.Errorf("本机暴露目标端口 %d 已由另一条条目占用", p.LocalPort)
		}
	}
	return nil
}

// StopProvider 关闭暴露端 websocket（保留配置）。
func (r *ReverseManager) StopProvider(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	r.mu.Lock()
	inst := r.providers[id]
	r.mu.Unlock()
	if inst == nil {
		return
	}
	r.waitProviderEnded(inst)
}

// RemoveProvider 删除暴露端条目。
func (r *ReverseManager) RemoveProvider(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	r.mu.Lock()
	inst := r.providers[id]
	r.mu.Unlock()
	if inst != nil {
		r.waitProviderEnded(inst)
	}
	r.mu.Lock()
	delete(r.providers, id)
	r.mu.Unlock()
}

// StartConsumer 启动接入端监听；若条目已运行则重启。
func (r *ReverseManager) StartConsumer(c ReverseConsumerPersist) error {
	c = normalizeReverseConsumerPersist(c)
	if err := r.validateConsumer(c); err != nil {
		return err
	}

	r.mu.Lock()
	prev := r.consumers[c.ID]
	if prev != nil {
		r.mu.Unlock()
		r.waitConsumerEnded(prev)
		r.mu.Lock()
	}
	if err := r.checkConsumerConflictsLocked(c.ID, &c); err != nil {
		r.mu.Unlock()
		return err
	}
	r.mu.Unlock()

	if !c.EffectiveEnabled() {
		r.StopConsumer(c.ID)
		return r.putConsumerInactive(c)
	}

	l, err := net.Listen("tcp", c.Listen)
	if err != nil {
		r.mu.Lock()
		r.consumers[c.ID] = &revConsumerInst{cfg: c, lastRunErr: err}
		r.mu.Unlock()
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	path := reverseSessionPath(c.ID)
	inst := &revConsumerInst{
		cfg:      c,
		done:     done,
		cancel:   cancel,
		listener: l,
	}
	r.mu.Lock()
	r.consumers[c.ID] = inst
	r.mu.Unlock()

	go func(instRef *revConsumerInst, ln net.Listener, sessionPath string) {
		defer close(instRef.done)
		defer instRef.cancel()
		defer func() { _ = ln.Close() }()
		runErr := ServeReverseConsumer(ctx, ln, instRef.cfg.ServerURL, sessionPath, instRef.cfg.APIKey, &instRef.activeBridges)
		r.mu.Lock()
		defer r.mu.Unlock()
		cur := r.consumers[instRef.cfg.ID]
		if cur != nil && cur.listener == ln {
			cur.lastRunErr = runErr
			cur.cancel = nil
			cur.listener = nil
		}
	}(inst, l, path)

	log.Printf("[reverse-consumer] ui id=%s listen=%s path=%s", c.ID, c.Listen, path)
	return nil
}

func (r *ReverseManager) checkConsumerConflictsLocked(selfID string, c *ReverseConsumerPersist) error {
	for cid, oc := range r.consumers {
		if cid == selfID {
			continue
		}
		if tcpBindingsEquiv(oc.cfg.Listen, c.Listen) {
			return fmt.Errorf("监听地址 %s 已由另一条接入端占用", strings.TrimSpace(c.Listen))
		}
	}

	for _, pv := range r.providers {
		ph := strings.TrimSpace(pv.cfg.LocalHost)
		if ph == "" {
			ph = "127.0.0.1"
		}
		if listenHostPortCollidesExpose(c.Listen, ph, pv.cfg.LocalPort) {
			return fmt.Errorf("监听 %s 与暴露条目目标 %s:%d 在同一主机端口上会冲突",
				strings.TrimSpace(c.Listen), ph, pv.cfg.LocalPort)
		}
	}
	return nil
}

// StopConsumer 停止接入端监听。
func (r *ReverseManager) StopConsumer(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	r.mu.Lock()
	inst := r.consumers[id]
	r.mu.Unlock()
	if inst == nil {
		return
	}
	r.waitConsumerEnded(inst)
}

// RemoveConsumer 删除接入端条目。
func (r *ReverseManager) RemoveConsumer(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	r.mu.Lock()
	inst := r.consumers[id]
	r.mu.Unlock()
	if inst != nil {
		r.waitConsumerEnded(inst)
	}
	r.mu.Lock()
	delete(r.consumers, id)
	r.mu.Unlock()
}

// StopAll 停止全部反向条目（不改变已保存的配置映射——仍保留占位以便展示 last error）。
func (r *ReverseManager) StopAll() {
	r.mu.Lock()
	pIDs := make([]string, 0, len(r.providers))
	cIDs := make([]string, 0, len(r.consumers))
	for id := range r.providers {
		pIDs = append(pIDs, id)
	}
	for id := range r.consumers {
		cIDs = append(cIDs, id)
	}
	instsP := make([]*revProviderInst, 0, len(pIDs))
	for _, id := range pIDs {
		if it := r.providers[id]; it != nil {
			instsP = append(instsP, it)
		}
	}
	instsC := make([]*revConsumerInst, 0, len(cIDs))
	for _, id := range cIDs {
		if it := r.consumers[id]; it != nil {
			instsC = append(instsC, it)
		}
	}
	r.mu.Unlock()
	for _, it := range instsP {
		r.waitProviderEnded(it)
	}
	for _, it := range instsC {
		r.waitConsumerEnded(it)
	}
}

// SnapshotProviders 返回暴露端快照（排序）。
func (r *ReverseManager) SnapshotProviders() []ReverseProviderRow {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ReverseProviderRow, 0, len(r.providers))
	for _, it := range r.providers {
		out = append(out, r.snapshotProvider(it))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LocalPort != out[j].LocalPort {
			return out[i].LocalPort < out[j].LocalPort
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// SnapshotConsumers 返回接入端快照（排序）。
func (r *ReverseManager) SnapshotConsumers() []ReverseConsumerRow {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ReverseConsumerRow, 0, len(r.consumers))
	for _, it := range r.consumers {
		out = append(out, r.snapshotConsumer(it))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Listen != out[j].Listen {
			return out[i].Listen < out[j].Listen
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// PersistedProviders 用于写 YAML（含密钥）。
func (r *ReverseManager) PersistedProviders() []ReverseProviderPersist {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ReverseProviderPersist, 0, len(r.providers))
	for _, it := range r.providers {
		out = append(out, it.cfg)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LocalPort != out[j].LocalPort {
			return out[i].LocalPort < out[j].LocalPort
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// PersistedConsumers 用于写 YAML（含密钥；id 即为隧道 UUID）。
func (r *ReverseManager) PersistedConsumers() []ReverseConsumerPersist {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ReverseConsumerPersist, 0, len(r.consumers))
	for _, it := range r.consumers {
		out = append(out, it.cfg)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Listen != out[j].Listen {
			return out[i].Listen < out[j].Listen
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// putProviderInactive 仅登记配置（不握手、不占用连接），用于 enabled=false 时的启动恢复。
func (r *ReverseManager) putProviderInactive(c ReverseProviderPersist) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if prev, ok := r.providers[c.ID]; ok && prev != nil {
		if prev.ws != nil || prev.serveDone != nil {
			return fmt.Errorf("暴露端仍在运行")
		}
	}
	if err := r.checkProviderConflictsLocked(c.ID, &c); err != nil {
		return err
	}
	r.providers[c.ID] = &revProviderInst{cfg: c, channelID: "", ws: nil}
	return nil
}

// putConsumerInactive 仅保留 YAML 条目，不监听 TCP。
func (r *ReverseManager) putConsumerInactive(c ReverseConsumerPersist) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if prev, ok := r.consumers[c.ID]; ok && prev != nil {
		if prev.cancel != nil || prev.listener != nil {
			return fmt.Errorf("接入端仍在运行")
		}
	}
	if err := r.checkConsumerConflictsLocked(c.ID, &c); err != nil {
		return err
	}
	r.consumers[c.ID] = &revConsumerInst{cfg: c}
	return nil
}

func logSkipBootstrapReverse(kind, id string, err error) {
	if strings.TrimSpace(id) == "" {
		id = "(无 id)"
	}
	log.Printf("[reverse tunnel] 跳过自动恢复 %s id=%s: %v", kind, id, err)
}

// BootstrapProviders 进程启动时按配置恢复暴露端。
func (r *ReverseManager) BootstrapProviders(cfgs []ReverseProviderPersist) {
	for _, cfg := range cfgs {
		c := normalizeReverseProviderPersist(cfg)
		if strings.TrimSpace(c.ID) == "" {
			c.ID = NewProxyID()
		}
		if err := r.validateProvider(c); err != nil {
			logSkipBootstrapReverse("暴露端", c.ID, err)
			continue
		}
		if !c.EffectiveEnabled() {
			if err := r.putProviderInactive(c); err != nil {
				logSkipBootstrapReverse("暴露端", c.ID, err)
			}
			continue
		}
		if _, err := r.StartProvider(c); err != nil {
			logSkipBootstrapReverse("暴露端", c.ID, err)
		}
	}
}

// BootstrapConsumers 进程启动时按配置恢复接入端。
func (r *ReverseManager) BootstrapConsumers(cfgs []ReverseConsumerPersist) {
	for _, cfg := range cfgs {
		c := normalizeReverseConsumerPersist(cfg)
		if err := r.validateConsumer(c); err != nil {
			logSkipBootstrapReverse("接入端", c.ID, err)
			continue
		}
		if !c.EffectiveEnabled() {
			if err := r.putConsumerInactive(c); err != nil {
				logSkipBootstrapReverse("接入端", c.ID, err)
			}
			continue
		}
		if err := r.StartConsumer(c); err != nil {
			logSkipBootstrapReverse("接入端", c.ID, err)
		}
	}
}
