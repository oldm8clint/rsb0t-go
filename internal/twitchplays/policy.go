package twitchplays

import (
	"sync"
	"time"
)

// RatePolicy prevents a single viewer or a busy stream from filling the
// controller faster than the game can safely consume commands.
type RatePolicy struct {
	PerUser    time.Duration
	Global     time.Duration
	Moderator  func(author string) bool
	mu         sync.Mutex
	lastUser   map[string]time.Time
	lastGlobal time.Time
}

func NewRatePolicy(perUser, global time.Duration, moderator func(string) bool) *RatePolicy {
	return &RatePolicy{PerUser: perUser, Global: global, Moderator: moderator, lastUser: make(map[string]time.Time)}
}

// Allow applies cooldowns to non-moderator commands. Moderators bypass rate
// limits so that emergency controls remain responsive.
func (p *RatePolicy) Allow(c Command) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Moderator != nil && p.Moderator(c.Author) {
		return true
	}
	now := c.Received
	if now.IsZero() {
		now = time.Now()
	}
	if p.PerUser > 0 {
		if last, ok := p.lastUser[c.Author]; ok && now.Sub(last) < p.PerUser {
			return false
		}
	}
	if p.Global > 0 && !p.lastGlobal.IsZero() && now.Sub(p.lastGlobal) < p.Global {
		return false
	}
	p.lastUser[c.Author] = now
	p.lastGlobal = now
	return true
}
