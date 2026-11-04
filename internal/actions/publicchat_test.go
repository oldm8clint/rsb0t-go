package actions

import (
	"bytes"
	"strings"
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/clientprot"
)

// The ClientAdapter.sayPublic layout: opcode, VARBYTE length, colour 0, effect 0, WordPack text.
func TestSayPublicWritesTheClientLayout(t *testing.T) {
	s := newFakeSession()
	if err := SayPublic(s, "  buying 100 chaos rune "); err != nil {
		t.Fatal(err)
	}
	body := gamestate.WordPack("buying 100 chaos rune")
	want := append([]byte{clientprot.MESSAGE_PUBLIC, byte(2 + len(body)), 0, 0}, body...)
	if got := s.lastSent(); !bytes.Equal(got, want) {
		t.Fatalf("SayPublic = % x, want % x", got, want)
	}
	if err := SayPublic(s, "   "); err != ErrEmptyChat {
		t.Fatalf("empty message: %v", err)
	}
	if err := SayPublic(s, strings.Repeat("a", 200)); err != nil {
		t.Fatal(err)
	}
	if got := s.lastSent(); int(got[1]) != 2+len(gamestate.WordPack(strings.Repeat("a", PublicChatLimit))) {
		t.Fatalf("a long message was not cut to %d characters", PublicChatLimit)
	}
}
