package fleet

import "time"

// Wake an existing retry owner rather than launching a second login goroutine.
// The actual handshake still goes through the fleet-wide rate gate.
func (m *Manager) waitAccountRetry(a *account, duration time.Duration) bool {
	a.mu.Lock()
	if a.retryWake == nil {
		a.retryWake = make(chan struct{}, 1)
	}
	wake := a.retryWake
	a.mu.Unlock()
	stopWait := make(chan struct{})
	defer close(stopWait)
	done := make(chan bool, 1)
	go func() { done <- m.retryWait(stopWait, duration) }()
	select {
	case <-m.stop:
		return false
	case <-wake:
		return true
	case ok := <-done:
		return ok
	}
}
