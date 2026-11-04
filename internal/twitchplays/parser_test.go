package twitchplays

import (
	"testing"
	"time"
)

func TestParseNormalizesKnownCommand(t *testing.T) {
	c, ok := Parse("  !FISH now  ", "youtube_live", "viewer", "msg-1", time.Unix(10, 0))
	if !ok || c.Action != "fish" || len(c.Args) != 1 || c.Args[0] != "now" {
		t.Fatalf("unexpected command: %+v", c)
	}
}

func TestParseRejectsPlainAndUnknownMessages(t *testing.T) {
	if _, ok := Parse("fish", "youtube_live", "viewer", "msg-1", time.Now()); ok {
		t.Fatal("plain chat message accepted")
	}
	if _, ok := Parse("!exec rm", "youtube_live", "viewer", "msg-2", time.Now()); ok {
		t.Fatal("unknown command accepted")
	}
}
