package twitchplays

import (
	"testing"
	"time"
)

func TestDiscordEventMapsToChatEvent(t *testing.T) {
	now := time.Unix(123, 0)
	e := DiscordEvent(DiscordMessage{ID: "m1", AuthorID: "u1", AuthorName: "viewer", Content: "!fish", ChannelID: "c1", Received: now})
	if e.Source != "discord" || e.MessageID != "m1" || e.Author != "u1" || e.Text != "!fish" || !e.Received.Equal(now) {
		t.Fatalf("unexpected event: %+v", e)
	}
}
