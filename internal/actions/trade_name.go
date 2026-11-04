package actions

import (
	"errors"
	"strings"
)

// ErrPlayerNotVisible is returned when no visible player has the requested name.
var ErrPlayerNotVisible = errors.New("actions: no visible player with that name")

// SameUsername compares two player names the way usernames compare: ignoring case, surrounding
// space, and the difference between a space and an underscore ("Bob_Smith" is "bob smith").
func SameUsername(a, b string) bool {
	norm := func(s string) string {
		return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "_", " "))
	}
	return norm(a) != "" && norm(a) == norm(b)
}

// RequestPlayer ports Trade.request(playerName): "Trade with" on the visible player with that
// name, found through gamestate's decoded PLAYER_INFO appearance names. It does not walk.
func (t *Trade) RequestPlayer(name string) error {
	for _, p := range t.s.State().Players() {
		if SameUsername(p.Name, name) {
			return OpPlayer4(t.s, int32(p.Index))
		}
	}
	return ErrPlayerNotVisible
}
