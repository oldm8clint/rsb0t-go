// Package crowdplay runs CrowdPlay from a Discord channel: a small Discord bot (the "crowd
// play decider") reads chat commands from ONE configured channel, turns them into the
// bounded commands of internal/twitchplays, and drives exactly one account, "crowdplay",
// through its existing session.
//
// OFF by default: nothing here runs unless cmd/controlpanel is given a token file and a
// channel (see docs/crowdplay_discord.md). Safety is layered: twitchplays refuses bank,
// trade, drop and give-away commands before anything else sees them; the Runner refuses any
// account but "crowdplay"; the game side (Sink) can only walk, fish and use an npc's first
// option, never an item, bank, trade or shop option; commands are deduplicated, rate
// limited per viewer and overall, queued with a bound, and executed one at a time.
package crowdplay

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Token is the Discord bot token. It never prints: String and GoString redact it, and no
// error in this package includes it or the file's contents.
type Token struct{ v string }

func (Token) String() string   { return "[redacted]" }
func (Token) GoString() string { return "[redacted]" }

// Empty reports whether no token is set.
func (t Token) Empty() bool { return t.v == "" }

// authorization is the REST Authorization header value.
func (t Token) authorization() string { return "Bot " + t.v }

// ReadTokenFile reads the bot token from a file by path, at runtime only. The file holds the
// token alone (surrounding whitespace is ignored). Errors name the path, never the contents.
func ReadTokenFile(path string) (Token, error) {
	if path == "" {
		return Token{}, errors.New("crowdplay: no token file configured")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Token{}, fmt.Errorf("crowdplay: reading the token file %s: %w", path, errors.Unwrap(err))
	}
	v := strings.TrimSpace(string(b))
	if v == "" || strings.ContainsAny(v, " \r\n\t") {
		return Token{}, fmt.Errorf("crowdplay: the token file %s does not hold a single token", path)
	}
	return Token{v: v}, nil
}
