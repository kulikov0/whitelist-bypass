package joiner

import (
	"sync"
)

type configAckTracker struct {
	mu        sync.Mutex
	acked     chan struct{}
	cancel    chan struct{}
	confirmed bool
}

func (t *configAckTracker) acknowledged() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.confirmed
}

func (t *configAckTracker) arm() (acked, cancel chan struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel != nil {
		close(t.cancel)
	}
	t.acked = make(chan struct{})
	t.cancel = make(chan struct{})
	return t.acked, t.cancel
}

func (t *configAckTracker) mark() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.confirmed = true
	if t.acked == nil {
		return
	}
	select {
	case <-t.acked:
	default:
		close(t.acked)
	}
}
