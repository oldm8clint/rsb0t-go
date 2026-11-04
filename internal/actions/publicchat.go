package actions

import (
	"errors"
	"strings"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

// PublicChatLimit is WordPack's own ceiling: it packs at most 80 characters.
const PublicChatLimit = 80

// ErrEmptyChat is returned for a message with nothing to say.
var ErrEmptyChat = errors.New("actions: nothing to say")

// SayPublic ports the client's MESSAGE_PUBLIC write (Client.ts's chat input handler, and the TS
// bot's ClientAdapter.sayPublic): a VARBYTE packet of colour 0, effect 0 and the WordPack-packed
// text, trimmed to 80 characters. The server never echoes a public line back to its sender.
//
// Added 2026-09-19 for the Scarab Swarm's player-market purchases ("buying 100 chaos rune" said
// beside a market account).
//
// PROVEN LIVE 2026-09-21 by cmd/publicchattest, both directions, against a local engine: A said
// a line and B observed it through the ordinary PLAYER_INFO CHAT -> gamestate.notePublicChat ->
// Event path, then B said one and A observed it, with a never-said line reported by neither. It
// asserts on the received text and speaker, never on this function returning nil — a nil error
// only means bytes were written. Four things that run measured, and that anyone building on
// public chat needs:
//
//   - RANGE. A public line rides PLAYER_INFO, so it only reaches accounts the server is already
//     sending player info for. Measured cutoff on the lab engine: heard at 15 tiles, silent at
//     16, with the speaker dropping out of the listener's player list at the same step. A market
//     stall more than 15 tiles away never hears the ask, however correct this packet is.
//   - THE TEXT COMES BACK CHANGED. The engine unpacks, WordEnc-filters and re-packs; the client's
//     WordPack.unpack then sentence-cases the result and appends a trailing space whenever the
//     packed body ends on an odd nibble. "selling rune axe 4 70323 coins" was observed as
//     "Selling rune axe 4 70323 coins " — leading capital, trailing space. Compare received lines
//     case-insensitively and trimmed; never ==.
//   - THE CHAT TYPE DEPENDS ON THE SPEAKER'S RIGHTS. Engine-TS sends rights = min(staffModLevel,
//     2), and gamestate turns any non-zero into gamestate.ChatPublicMod, not ChatPublic. Every
//     lab account is staffModLevel 2, so lab lines all arrive as ChatPublicMod; production
//     players arrive as ChatPublic. A listener must accept both.
//   - NO SELF-ECHO, CONFIRMED. See gamestate.State.SelfChat's own comment.
//
// Nothing else in the fleet speaks in public chat, and no script reads it except through
// State.Subscribe — internal/chatlog (what internal/fleet hands a bot.Context, and what the
// control panel's chat feed is built from) is fed MESSAGE_GAME only, so another player's speech
// never appears there. That is a decode-surface gap, not a protocol one.
func SayPublic(s Session, text string) error {
	msg := strings.TrimSpace(text)
	if len(msg) > PublicChatLimit {
		msg = msg[:PublicChatLimit]
	}
	if msg == "" {
		return ErrEmptyChat
	}
	body := gamestate.WordPack(msg)
	return s.Send(func(w *packet.Packet) {
		w.P1Enc(clientprot.MESSAGE_PUBLIC)
		w.P1(0) // length placeholder
		w.P1(0) // colour
		w.P1(0) // effect
		w.PData(body, 0, len(body))
		w.PSize1(2 + len(body))
	})
}
