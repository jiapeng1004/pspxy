package reversetunnel

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Offer 暴露端首条 Text 消息：要桥接的本地 TCP。
type Offer struct {
	LocalHost string `json:"local_host"`
	LocalPort int    `json:"local_port"`
}

type attachedMsg struct {
	Cmd   string `json:"cmd"`
	SID   string `json:"sid"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type channel struct {
	id       uuid.UUID
	agent    *websocket.Conn
	offer    Offer
	mu       sync.Mutex
	wsMu     sync.Mutex // 同一条 agent 禁止并发 WriteMessage
	sessions map[uuid.UUID]*websocket.Conn

	ackWaitMu sync.Mutex
	ackWait   map[uuid.UUID]chan attachResult
}

type attachResult struct {
	err error
}

// Broker 反向隧道：一个 provider WebSocket，多路复用多条 consumer 会话。
type Broker struct {
	mu       sync.RWMutex
	channels map[uuid.UUID]*channel
}

// NewBroker 创建 Broker。
func NewBroker() *Broker {
	return &Broker{channels: make(map[uuid.UUID]*channel)}
}

// RegisterProvider 注册暴露端 ws，返回 channel_id 与须在写出 ACK 成功后调用的 Runner（启动 broker 读循环）。
func (b *Broker) RegisterProvider(agent *websocket.Conn, offer Offer) (_ uuid.UUID, start func(), _ error) {
	if offer.LocalPort < 1 || offer.LocalPort > 65535 {
		return uuid.UUID{}, nil, errors.New("local_port must be 1-65535")
	}
	if offer.LocalHost == "" {
		offer.LocalHost = "127.0.0.1"
	}
	ch := &channel{
		id:       uuid.New(),
		agent:    agent,
		offer:    offer,
		sessions: make(map[uuid.UUID]*websocket.Conn),
		ackWait:  make(map[uuid.UUID]chan attachResult),
	}
	b.mu.Lock()
	b.channels[ch.id] = ch
	b.mu.Unlock()
	return ch.id, func() { go b.agentReadLoop(ch) }, nil
}

// AbortProviderRegistration 在尚未启动 agentReadLoop 时撤销注册（仅用于 ACK 写失败）。
func (b *Broker) AbortProviderRegistration(id uuid.UUID) {
	b.mu.Lock()
	ch := b.channels[id]
	delete(b.channels, id)
	b.mu.Unlock()
	if ch != nil && ch.agent != nil {
		_ = ch.agent.Close()
	}
}

func (b *Broker) removeChannel(id uuid.UUID) {
	b.mu.Lock()
	ch, ok := b.channels[id]
	delete(b.channels, id)
	b.mu.Unlock()
	if !ok || ch == nil {
		return
	}
	ch.mu.Lock()
	for sid, c := range ch.sessions {
		if c != nil {
			_ = c.Close()
		}
		delete(ch.sessions, sid)
	}
	ch.mu.Unlock()

	ch.ackWaitMu.Lock()
	for sid, wc := range ch.ackWait {
		select {
		case wc <- attachResult{err: errors.New("provider disconnected")}:
		default:
		}
		delete(ch.ackWait, sid)
	}
	ch.ackWaitMu.Unlock()
}

func (b *Broker) agentReadLoop(ch *channel) {
	defer b.removeChannel(ch.id)
	defer func() { _ = ch.agent.Close() }()

	for {
		mt, data, err := ch.agent.ReadMessage()
		if err != nil {
			return
		}
		switch mt {
		case websocket.TextMessage:
			var m attachedMsg
			if json.Unmarshal(data, &m) != nil || m.Cmd != "attached" {
				continue
			}
			sid, perr := uuid.Parse(m.SID)
			if perr != nil {
				continue
			}
			ch.ackWaitMu.Lock()
			wc, ok := ch.ackWait[sid]
			delete(ch.ackWait, sid)
			ch.ackWaitMu.Unlock()
			if !ok {
				continue
			}
			var resErr error
			if !m.OK {
				if m.Error != "" {
					resErr = errors.New(m.Error)
				} else {
					resErr = errors.New("attach failed")
				}
			}
			select {
			case wc <- attachResult{err: resErr}:
			default:
			}

		case websocket.BinaryMessage:
			sid, body, ok := ParsePrefixedMessage(data)
			if !ok {
				continue
			}
			ch.mu.Lock()
			cw := ch.sessions[sid]
			ch.mu.Unlock()
			if cw == nil {
				continue
			}
			if err := cw.WriteMessage(websocket.BinaryMessage, body); err != nil {
				b.closeSession(ch, sid)
			}
		}
	}
}

func (b *Broker) closeSession(ch *channel, sid uuid.UUID) {
	ch.mu.Lock()
	cw, ok := ch.sessions[sid]
	delete(ch.sessions, sid)
	ch.mu.Unlock()
	if ok && cw != nil {
		_ = cw.Close()
	}
	detach, _ := json.Marshal(map[string]string{"cmd": "detach", "sid": sid.String()})
	_ = ch.agent.SetWriteDeadline(time.Now().Add(15 * time.Second))
	ch.wsMu.Lock()
	_ = ch.agent.WriteMessage(websocket.TextMessage, detach)
	ch.wsMu.Unlock()
	_ = ch.agent.SetWriteDeadline(time.Time{})
}

// AttachConsumer 建立一条 consumer↔provider 桥接；成功返回后由内部 goroutine 负责 consumer→agent 方向。
func (b *Broker) AttachConsumer(chID uuid.UUID, cons *websocket.Conn) error {
	b.mu.RLock()
	ch := b.channels[chID]
	b.mu.RUnlock()
	if ch == nil {
		_ = cons.Close()
		return errors.New("channel not found or provider offline")
	}

	sid := uuid.New()
	ackCh := make(chan attachResult, 1)
	ch.ackWaitMu.Lock()
	ch.ackWait[sid] = ackCh
	ch.ackWaitMu.Unlock()

	attach, _ := json.Marshal(map[string]string{"cmd": "attach", "sid": sid.String()})
	ch.wsMu.Lock()
	err := ch.agent.WriteMessage(websocket.TextMessage, attach)
	ch.wsMu.Unlock()
	if err != nil {
		ch.ackWaitMu.Lock()
		delete(ch.ackWait, sid)
		ch.ackWaitMu.Unlock()
		_ = cons.Close()
		return err
	}

	var resErr error
	select {
	case ar := <-ackCh:
		resErr = ar.err
	case <-time.After(20 * time.Second):
		resErr = errors.New("attach timeout")
	}
	if resErr != nil {
		ch.ackWaitMu.Lock()
		delete(ch.ackWait, sid)
		ch.ackWaitMu.Unlock()
		_ = cons.Close()
		return resErr
	}

	ch.mu.Lock()
	ch.sessions[sid] = cons
	ch.mu.Unlock()

	go func() {
		defer b.closeSession(ch, sid)
		for {
			mt, payload, err := cons.ReadMessage()
			if err != nil {
				return
			}
			if mt != websocket.BinaryMessage {
				continue
			}
			frame := PrependSessionID(sid, payload)
			ch.wsMu.Lock()
			werr := ch.agent.WriteMessage(websocket.BinaryMessage, frame)
			ch.wsMu.Unlock()
			if werr != nil {
				return
			}
		}
	}()
	return nil
}

// ChannelCount 当前活跃暴露通道数（测试/观测）。
func (b *Broker) ChannelCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.channels)
}
