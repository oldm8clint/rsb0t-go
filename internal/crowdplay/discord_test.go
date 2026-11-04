package crowdplay

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/crowdplay/fakediscord"
	"rsb0t-go/internal/twitchplays"
)

func TestDiscordReadsOneChannelFromPeopleAndRepliesQuietly(t *testing.T) {
	fake, err := fakediscord.Start("fake-token-for-tests")
	if err != nil {
		t.Fatal(err)
	}
	defer fake.Close()
	d, err := NewDiscord(DiscordConfig{Token: Token{v: "fake-token-for-tests"}, ChannelID: "4242", APIBase: fake.APIBase(), ReplyInterval: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []twitchplays.DiscordMessage
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx, func(m twitchplays.DiscordMessage) { mu.Lock(); got = append(got, m); mu.Unlock() })
	}()
	if !fake.WaitReady(5 * time.Second) {
		t.Fatal("the bot never identified")
	}
	id := fake.Identified()[0]
	if intents, _ := id["intents"].(float64); int(intents) != intentGuildMessages|intentMessageContent {
		t.Errorf("intents = %v", id["intents"])
	}
	for _, m := range []fakediscord.Message{
		{ID: "1", Channel: "4242", AuthorID: "10", Author: "alice", Content: "!walk n"},
		{ID: "2", Channel: "9999", AuthorID: "11", Author: "bob", Content: "!walk s"},                 // another channel
		{ID: "3", Channel: "4242", AuthorID: "12", Author: "somebot", Content: "!bank", Bot: true},    // a bot
		{ID: "4", Channel: "4242", AuthorID: "900", Author: "crowd play decider", Content: "!walk e"}, // itself
		{ID: "5", Channel: "4242", AuthorID: "13", Author: "carol", Content: "!fish"},
	} {
		if err := fake.Send(m); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	if len(got) != 2 || got[0].ID != "1" || got[1].ID != "5" || got[0].AuthorID != "10" {
		t.Fatalf("delivered %+v, want messages 1 and 5 only", got)
	}
	mu.Unlock()

	if err := d.Reply(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	_ = d.Reply(ctx, "second, inside the reply interval") // dropped
	replies := fake.Replies()
	if len(replies) != 1 || replies[0].Content != "first" || replies[0].Channel != "4242" || replies[0].Mentions {
		t.Fatalf("replies = %+v", replies)
	}
	if d.Dropped() != 1 || fake.BadAuth() != 0 {
		t.Fatalf("dropped %d, bad auth %d", d.Dropped(), fake.BadAuth())
	}
	// Heartbeats flow (the fake asks for one a second).
	time.Sleep(1300 * time.Millisecond)
	if fake.Heartbeats() < 1 {
		t.Error("no heartbeat sent")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not end with its context")
	}
}

func TestDiscordStopsOnARefusedToken(t *testing.T) {
	fake, err := fakediscord.Start("the-right-token")
	if err != nil {
		t.Fatal(err)
	}
	defer fake.Close()
	d, _ := NewDiscord(DiscordConfig{Token: Token{v: "a-wrong-token"}, ChannelID: "1", APIBase: fake.APIBase(),
		GatewayURL: "ws" + strings.TrimPrefix(fake.URL, "http") + "/gateway"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = d.Run(ctx, func(twitchplays.DiscordMessage) {})
	if err == nil || !strings.Contains(err.Error(), "4004") || strings.Contains(err.Error(), "a-wrong-token") {
		t.Fatalf("Run = %v, want a 4004 refusal that does not print the token", err)
	}
}

func TestDiscordConfigIsChecked(t *testing.T) {
	if _, err := NewDiscord(DiscordConfig{ChannelID: "1"}); err == nil {
		t.Error("no token accepted")
	}
	if _, err := NewDiscord(DiscordConfig{Token: Token{v: "x"}}); err == nil {
		t.Error("no channel accepted")
	}
	if _, err := NewDiscord(DiscordConfig{Token: Token{v: "x"}, ChannelID: "#general"}); err == nil {
		t.Error("a channel name accepted")
	}
}
