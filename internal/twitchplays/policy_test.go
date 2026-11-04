package twitchplays

import (
	"testing"
	"time"
)

func TestRatePolicyLimitsUserAndGlobalTraffic(t *testing.T) {
	p := NewRatePolicy(time.Second, 100*time.Millisecond, nil)
	base := time.Unix(100, 0)
	c := Command{Action: "fish", Author: "a", MessageID: "1", Received: base}
	if !p.Allow(c) {
		t.Fatal("first command rejected")
	}
	c.MessageID = "2"
	c.Received = base.Add(50 * time.Millisecond)
	if p.Allow(c) {
		t.Fatal("global cooldown bypassed")
	}
	c.Author = "b"
	c.MessageID = "3"
	c.Received = base.Add(200 * time.Millisecond)
	if !p.Allow(c) {
		t.Fatal("second viewer should be allowed")
	}
	c.Author = "a"
	c.MessageID = "4"
	c.Received = base.Add(500 * time.Millisecond)
	if p.Allow(c) {
		t.Fatal("per-user cooldown bypassed")
	}
}

func TestModeratorBypassesRateLimits(t *testing.T) {
	p := NewRatePolicy(time.Hour, time.Hour, func(author string) bool { return author == "mod" })
	c := Command{Action: "stop", Author: "mod", MessageID: "1", Received: time.Unix(100, 0)}
	if !p.Allow(c) {
		t.Fatal("moderator command rejected")
	}
}
