package fleet

import (
	"testing"

	"rsb0t-go/internal/gamestate"
)

// TestPublicChatLine pins which chat Events reach a.chat, and therefore the control panel.
//
// Until 2026-09-21 startReadLoop recorded MESSAGE_GAME and nothing else, so the panel's chat
// feed could not show one word of player speech and no script could hear a market stall answer.
// The two public types are BOTH accepted on purpose: the type follows the speaker's rights, so
// lab accounts (staffModLevel 2) speak as ChatPublicMod while production players speak as
// ChatPublic. Accepting only one would pass every lab test and hear nothing live.
func TestPublicChatLine(t *testing.T) {
	for _, c := range []struct {
		name string
		ev   gamestate.Event
		want string
		ok   bool
	}{
		{
			name: "an ordinary player, as production sends it",
			ev:   gamestate.Event{Kind: gamestate.EventChatMessage, ChatType: gamestate.ChatPublic, Username: "Seers Market", Text: "Selling rune axe 150000 coins"},
			want: "Seers Market: Selling rune axe 150000 coins", ok: true,
		},
		{
			name: "a staff-rights speaker, as every lab account sends it",
			ev:   gamestate.Event{Kind: gamestate.EventChatMessage, ChatType: gamestate.ChatPublicMod, Username: "Pchata", Text: "buying 1 rune axe"},
			want: "Pchata: buying 1 rune axe", ok: true,
		},
		{
			name: "a game message is not player speech and is recorded elsewhere",
			ev:   gamestate.Event{Kind: gamestate.EventChatMessage, ChatType: gamestate.ChatGame, Text: "You catch a mackerel."},
		},
		{
			name: "a private message is not public chat",
			ev:   gamestate.Event{Kind: gamestate.EventChatMessage, ChatType: gamestate.ChatPrivate, Username: "mule", Text: "on my way"},
		},
		{
			name: "an unattributed line is useless to a reader",
			ev:   gamestate.Event{Kind: gamestate.EventChatMessage, ChatType: gamestate.ChatPublic, Text: "who said this?"},
		},
	} {
		got, ok := publicChatLine(c.ev)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: publicChatLine = (%q, %v), want (%q, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}
