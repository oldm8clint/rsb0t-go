package gamestate

import (
	"strings"

	"rsb0t-go/internal/packet"
)

// Chat decoding for the TS scripting API's chat.message event and reader.selfChat.
//
// Client.ts files every chat line through addChat(type, text, username). Three packets feed it:
//   - MESSAGE_GAME: a plain game line (type 0), or "<name>:tradereq:" (type 4) and
//     "<name>:duelreq:" (type 8).
//   - PLAYER_INFO extended info: SAY (0x8) is overhead text the server forces (type 2, the
//     player's name), and CHAT (0x40) is a player's public chat: g2 colour<<8|effect, g1 rights,
//     g1 length, then length bytes of WordPack text (type 1 with a crown for rights 1-3, else
//     type 2). Engine-TS never sends a player their own CHAT (rsbuf/info.ts strips it), so the
//     local player's public chat never arrives here.
//   - MESSAGE_PRIVATE: g8 sender userhash, g4 message id, g1 staff level, then WordPack text to
//     the end of the packet (type 7 with a crown for staff 1-3, else type 3). The client drops a
//     message id it has already shown among the last 100.
//
// Differences from the client, on purpose: the ignore list and chat-disabled filters are not
// applied, the client-side WordFilter is not run (the engine already filters what it packs),
// trade and duel requests keep their raw "<name>:tradereq:" text instead of the display text, and
// the crown is its own field instead of an "@cr1@"/"@cr2@" prefix on the username.

// Chat line types, Client.ts addChat's first argument (Event.ChatType).
const (
	ChatGame         = 0
	ChatPublicMod    = 1
	ChatPublic       = 2
	ChatPrivate      = 3
	ChatTradeRequest = 4
	ChatFriendStatus = 5
	ChatPrivateSent  = 6
	ChatPrivateMod   = 7
	ChatDuelRequest  = 8
)

// privateMessageMemory is how many recent private message ids the client remembers.
const privateMessageMemory = 100

type chatTracker struct {
	selfText string
	selfTick int
	haveSelf bool

	privateIDs  [privateMessageMemory]int32
	privateUsed [privateMessageMemory]bool
	privateNext int

	renderLines []RenderChatLine // bounded received chat history for on-demand captures

	pending []Event // lines the PLAYER_INFO decode queued, taken by Handle
}

// wordPackTable is WordPack.ts's TABLE.
var wordPackTable = []rune{
	' ', 'e', 't', 'a', 'o', 'i', 'h', 'n', 's', 'r', 'd', 'l', 'u',
	'm', 'w', 'c', 'y', 'f', 'g', 'p', 'b', 'v', 'k', 'x', 'j', 'q', 'z',
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
	' ', '!', '?', '.', ',', ':', ';', '(', ')', '-', '&', '*', '\\', '\'', '@', '#', '+', '=', '£', '$', '%', '"', '[', ']',
}

// wordPackUnpack ports the client's WordPack.unpack over the whole of data: 13 common characters
// fit one nibble, the rest take two (a high nibble of 13-15 carries into the next), capped at 100
// characters, then the first letter and each letter after '.' or '!' are upper-cased.
func wordPackUnpack(data []byte) string {
	const invalid = rune(-1)
	chars := make([]rune, 0, 2*len(data))
	carry := -1
	take := func(nibble int) {
		switch {
		case carry != -1:
			idx := carry<<4 + nibble - 195
			if idx >= 0 && idx < len(wordPackTable) {
				chars = append(chars, wordPackTable[idx])
			} else {
				chars = append(chars, invalid) // undefined upstream, which joins as nothing
			}
			carry = -1
		case nibble < 13:
			chars = append(chars, wordPackTable[nibble])
		default:
			carry = nibble
		}
	}
	for i := 0; i < len(data) && len(chars) < 100; i++ {
		take(int(data[i]>>4) & 0xf)
		take(int(data[i]) & 0xf)
	}

	var b strings.Builder
	upper := true
	for _, c := range chars {
		if c == invalid {
			continue
		}
		if upper && c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
			upper = false
		}
		if c == '.' || c == '!' {
			upper = true
		}
		b.WriteRune(c)
	}
	return b.String()
}

// wordPackPack ports the client's WordPack.pack: at most 80 characters, lower-cased, characters
// outside the table packed as a space. It is the inverse of wordPackUnpack for tests, and what a
// public or private chat send would write.
func wordPackPack(text string) []byte {
	runes := []rune(strings.ToLower(text))
	if len(runes) > 80 {
		runes = runes[:80]
	}
	var out []byte
	carry := -1
	for _, r := range runes {
		current := 0
		for i, t := range wordPackTable {
			if t == r {
				current = i
				break
			}
		}
		if current > 12 {
			current += 195
		}
		switch {
		case carry == -1 && current < 13:
			carry = current
		case carry == -1:
			out = append(out, byte(current))
		case current < 13:
			out = append(out, byte(carry<<4+current))
			carry = -1
		default:
			out = append(out, byte(carry<<4+current>>4))
			carry = current & 0xf
		}
	}
	if carry != -1 {
		out = append(out, byte(carry<<4))
	}
	return out
}

// gameChatEvent builds the chat.message event for a MESSAGE_GAME line.
func gameChatEvent(text string) Event {
	ev := Event{Kind: EventChatMessage, ChatType: ChatGame, Text: text}
	name := text
	if i := strings.IndexByte(text, ':'); i >= 0 {
		name = text[:i]
	}
	switch {
	case strings.HasSuffix(text, ":tradereq:"):
		ev.ChatType, ev.Username = ChatTradeRequest, name
	case strings.HasSuffix(text, ":duelreq:"):
		ev.ChatType, ev.Username = ChatDuelRequest, name
	}
	return ev
}

// crownFor maps public chat rights or private staff level to the crown the client draws.
func crownFor(level int) int {
	switch {
	case level == 2 || level == 3:
		return 2
	case level == 1:
		return 1
	}
	return 0
}

// playerNameAt is a player's known display name, or "" (index -1 is the local player).
func (s *State) playerNameAt(index int) string {
	if index == -1 {
		return s.self.name
	}
	if e := s.players[index]; e != nil && e.name != "" {
		return e.name
	}
	return s.playerAppearanceNames[index]
}

// noteSay records a SAY update: the local player's selfChat, and a type 2 line for a named player.
func (s *State) noteSay(index int, text string) {
	if index == -1 {
		s.chat.selfText, s.chat.selfTick, s.chat.haveSelf = text, s.playerInfoTick(), true
	}
	if name := s.playerNameAt(index); name != "" {
		s.chat.pending = append(s.chat.pending, Event{Kind: EventChatMessage, ChatType: ChatPublic, Username: name, Text: text})
	}
}

// notePublicChat records a CHAT update. Like the client it needs the player's name first.
func (s *State) notePublicChat(index, rights int, packed []byte) {
	text := wordPackUnpack(packed)
	if index == -1 {
		s.chat.selfText, s.chat.selfTick, s.chat.haveSelf = text, s.playerInfoTick(), true
	}
	name := s.playerNameAt(index)
	if name == "" {
		return
	}
	ev := Event{Kind: EventChatMessage, ChatType: ChatPublic, Username: name, Text: text, Crown: crownFor(rights)}
	if ev.Crown != 0 {
		ev.ChatType = ChatPublicMod
	}
	s.chat.pending = append(s.chat.pending, ev)
}

// takeChatEvents hands Handle the queued lines.
func (s *State) takeChatEvents() []Event {
	events := s.chat.pending
	s.chat.pending = nil
	return events
}

// applyMessagePrivate decodes MESSAGE_PRIVATE into a chat.message event, dropping a repeat of a
// recent message id.
func (s *State) applyMessagePrivate(r *packet.Packet) []Event {
	if r.Available() < 13 {
		return nil
	}
	from := r.G8()
	id := r.G4()
	staff := int(r.G1())
	for i := range s.chat.privateIDs {
		if s.chat.privateUsed[i] && s.chat.privateIDs[i] == id {
			return nil
		}
	}
	s.chat.privateIDs[s.chat.privateNext] = id
	s.chat.privateUsed[s.chat.privateNext] = true
	s.chat.privateNext = (s.chat.privateNext + 1) % privateMessageMemory

	packed := make([]byte, r.Available())
	r.GData(len(packed), 0, packed)
	ev := Event{
		Kind:     EventChatMessage,
		ChatType: ChatPrivate,
		Username: playerScreenName(playerRawUsername(from)),
		Text:     wordPackUnpack(packed),
		Crown:    crownFor(staff),
	}
	if ev.Crown != 0 {
		ev.ChatType = ChatPrivateMod
	}
	return []Event{ev}
}

// SelfChat ports reader.selfChat: the last overhead text on the local player and the tick it
// arrived on, or "" before any. Only server-sent SAY text reaches it: the engine never echoes a
// player's own public chat back to the speaker.
//
// This client DOES send public chat (actions.SayPublic), contrary to what this comment claimed
// until 2026-09-21 — but a speaker still cannot read itself here. Measured by cmd/publicchattest
// against a local engine: A said a line, B observed it word for word, and A's own SelfChat()
// stayed "" with nothing on its own Event stream. Engine-TS's rsbuf/info.ts strips a player's
// own CHAT mask out of its own player-info block, so there is nothing to decode. A script that
// wants to know its line was really said must observe someone else react to it.
func (s *State) SelfChat() (text string, tick int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.chat.haveSelf {
		return "", 0
	}
	return s.chat.selfText, s.chat.selfTick
}

// WordPack is wordPackPack exported for a chat SEND (actions.SayPublic): the client's own
// WordPack.pack, checked byte for byte against the reference vectors in chat_test.go.
func WordPack(text string) []byte { return wordPackPack(text) }
