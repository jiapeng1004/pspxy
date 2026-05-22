package reversetunnel

import "github.com/google/uuid"

// SessionIDSize WebSocket 二进制帧前导为 UUID 原始 16 字节，用于多路复用。
const SessionIDSize = 16

// PrependSessionID 在 payload 前附加 session id（发往 provider 侧）。
func PrependSessionID(sid uuid.UUID, payload []byte) []byte {
	out := make([]byte, SessionIDSize+len(payload))
	copy(out[:SessionIDSize], sid[:])
	copy(out[SessionIDSize:], payload)
	return out
}

// ParsePrefixedMessage 解析带前导 session 的二进制帧。
func ParsePrefixedMessage(data []byte) (sid uuid.UUID, body []byte, ok bool) {
	if len(data) < SessionIDSize {
		return uuid.UUID{}, nil, false
	}
	copy(sid[:], data[:SessionIDSize])
	return sid, data[SessionIDSize:], true
}
