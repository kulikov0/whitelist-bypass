package bitrix

import "sync"

type KickManager struct {
	kick   func(userID string) error
	selfID string
	logFn  func(string, ...any)

	mu     sync.Mutex
	active string
}

func NewKickManager(kick func(userID string) error, selfID string, logFn func(string, ...any)) *KickManager {
	return &KickManager{kick: kick, selfID: selfID, logFn: logFn}
}

func (m *KickManager) OnUserJoined(userID string) {
	if userID == "" || userID == m.selfID {
		return
	}
	m.mu.Lock()
	old := m.active
	if old == userID {
		m.mu.Unlock()
		return
	}
	m.active = userID
	m.mu.Unlock()

	if old == "" {
		m.logFn("[bx] active call guest userId=%s", userID)
		return
	}
	if err := m.kick(old); err != nil {
		m.logFn("[bx] kick failed userId=%s: %v", old, err)
		return
	}
	m.logFn("[bx] kicked previous guest userId=%s, new active=%s", old, userID)
}
