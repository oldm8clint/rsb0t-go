package bot

import (
	"errors"
	"time"

	"rsb0t-go/internal/actions"
)

// Game-namespace helpers that need a wait, so they live on Context (whose waits stop with the
// bot) rather than in internal/actions.

// teleportArrivalDistance is how far, in tiles on either axis, the player must end up from
// where the cast started for Teleport to call it arrived. Every standard teleport crosses far
// more of the map than this; a level change also counts.
const teleportArrivalDistance = 10

// defaultTeleportTimeout covers the cast animation (about four ticks) with room to spare.
const defaultTeleportTimeout = 10 * time.Second

var (
	errNoSession = errors.New("bot: context has no session")
	errNoTile    = errors.New("bot: player tile not known yet")
)

// Teleport casts a standard spellbook teleport (actions.Teleport) and, unlike upstream's
// Game.teleport, which returns once the click is sent, waits until the player has actually
// moved: more than teleportArrivalDistance tiles from the starting tile on either axis, or to
// another level. arrived is false when that does not happen within timeout (0 means 10 s) or the
// bot is stopped; err is set only when the cast could not be sent at all. A missing rune or a
// too-low Magic level shows up as arrived == false, with the server's reason in the chat log.
func (c *Context) Teleport(name string, timeout time.Duration) (arrived bool, err error) {
	st := c.gameState()
	if st == nil {
		return false, errNoSession
	}
	x, z, level, ok := st.Tile()
	if !ok {
		return false, errNoTile
	}
	if err := actions.Teleport(c.Session, name); err != nil {
		return false, err
	}
	if timeout <= 0 {
		timeout = defaultTeleportTimeout
	}
	return c.DelayUntil(func() bool {
		nx, nz, nl, ok := st.Tile()
		return ok && (nl != level || absInt(nx-x) > teleportArrivalDistance || absInt(nz-z) > teleportArrivalDistance)
	}, timeout), nil
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
