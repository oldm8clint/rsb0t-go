// Package harness holds the shared plumbing a live proof tool under cmd/ needs to report a
// verdict that can be believed.
//
// It exists because this project's harnesses have repeatedly lied, and every instance was
// caught by accident rather than by the tool itself:
//
//   - At least five tools sent `::` cheats without ever setting actions.CheatEnabled, so every
//     cheat returned ErrCheatDisabled and the tool ran from wherever the account happened to
//     be. One could not spawn the event it existed to solve; another's location setup did
//     nothing at all (fixed case by case in 2fecee7 and 85242e1).
//   - Several printed a pass from a proxy an idle or stranded account satisfies anyway: XP that
//     a different action produced, a position measured before the teleport landed, an item
//     count that was already correct before the test ran, an event "cleared" by a leftover
//     monster from an earlier trial.
//   - A precondition that was never met read as a pass, because nothing checked it.
//
// The three pieces here answer those three faults: Cheats refuses to run at all rather than
// silently sending nothing, Teleport is not "done" until the account is observed on the tile,
// and Verdict cannot report success without a Check that actually ran.
//
// See docs/trustworthy_harnesses.md for how to write a new tool against this.
package harness

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/tile"
)

// productionHosts names every host this package refuses to enable cheats against. CLIENT_CHEAT
// is an opcode a real client only ever sends from a staff-only admin entry, so a non-staff
// connection sending it at all is a client fingerprint, independent of whether the engine
// honors the command — see actions.CheatEnabled's own doc comment.
var productionHosts = []string{"example.invalid"}

// ProductionURL reports whether a URL points at the real game rather than a local engine.
// Kept pure and separately tested: it is the one check standing between a -enable-cheats
// typo and a production account transmitting an opcode no real client sends.
//
// Besides the named hosts, any host that is not this machine, a private network address or a
// test name (*.localhost, *.test) counts as a real game: a public server is never a local engine,
// whatever it is called.
func ProductionURL(raw string) bool {
	lower := strings.ToLower(raw)
	for _, h := range productionHosts {
		if strings.Contains(lower, h) {
			return true
		}
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		// An address that does not parse, or has no host (no scheme in front), cannot be shown to be
		// a local engine, so it is treated as a real one. Only "nothing given" is not.
		return strings.TrimSpace(raw) != ""
	}
	h := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".test") {
		return false
	}
	if ip := net.ParseIP(h); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
		return false
	}
	return true
}

// CheatDecision is the pure half of Cheats: given the flag and the URLs a tool will talk to,
// it answers whether cheats may be enabled and what to say when they may not.
func CheatDecision(tool string, enabled bool, urls []string) (allow bool, msg string) {
	for _, u := range urls {
		if ProductionURL(u) {
			return false, tool + ": refusing to enable cheats against " + u +
				" — CLIENT_CHEAT is an opcode no real client sends, and this tool is local-engine-only"
		}
	}
	if !enabled {
		return false, tool + ": -enable-cheats is required.\n" +
			"  Every `::` command this tool sends goes through internal/actions.Cheat, which returns\n" +
			"  ErrCheatDisabled unless the gate is set, so without the flag the setup silently does\n" +
			"  nothing and the run reports on an account that never reached its start state.\n" +
			"  Pass -enable-cheats against a local engine only."
	}
	return true, ""
}

// Cheats opens the cheat gate for a tool built on `::` commands, or exits.
//
// Exiting is the point. The alternative this replaces — declaring the flag, forgetting to
// assign actions.CheatEnabled, and carrying on — is exactly how five tools came to spend days
// reporting on accounts that never reached their own start state. A tool that cannot set up
// its preconditions has nothing to say, so it says nothing.
func Cheats(tool string, enabled bool, urls ...string) {
	allow, msg := CheatDecision(tool, enabled, urls)
	if !allow {
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(2)
	}
	actions.CheatEnabled = true
}

// ErrCheatRefused wraps a cheat the engine never accepted.
var ErrCheatRefused = errors.New("cheat refused")

// Tile reads the account's own tracked position. ok is false before the first position packet.
func Tile(s actions.Session) (tile.Tile, bool) {
	x, z, level, ok := s.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}, ok
}

// WaitUntil polls cond until it holds or the timeout expires.
func WaitUntil(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return cond()
}

// Teleport sends ::tele and returns only once the account is observed on the target tile.
//
// Why the wait is not optional: a ::tele that was refused, or that is still in flight, leaves
// the account exactly where it was, and a tool that measures anything in that window is
// measuring the old position. One harness reported "back within 10 tiles after 0s" that way,
// and another walked every route from wherever the previous run had left the account.
func Teleport(s actions.Session, dest tile.Tile, timeout time.Duration) error {
	cmd := fmt.Sprintf("tele %d,%d,%d,%d,%d", dest.Level, dest.X>>6, dest.Z>>6, dest.X&0x3f, dest.Z&0x3f)
	if err := actions.Cheat(s, cmd); err != nil {
		return fmt.Errorf("::%s not sent: %w", cmd, err)
	}
	if WaitUntil(timeout, func() bool {
		at, ok := Tile(s)
		return ok && at == dest
	}) {
		return nil
	}
	at, ok := Tile(s)
	if !ok {
		return fmt.Errorf("%w: ::%s sent but this client has no position for the account yet", ErrCheatRefused, cmd)
	}
	return fmt.Errorf("%w: ::%s sent but the account is still at %d,%d,%d after %s",
		ErrCheatRefused, cmd, at.X, at.Z, at.Level, timeout)
}

// TeleportNear is Teleport for a destination the engine may round, for example a tele into a
// multi-tile loc. It accepts any landing within radius, and still refuses a teleport that
// never moved the account at all.
func TeleportNear(s actions.Session, dest tile.Tile, radius int, timeout time.Duration) error {
	cmd := fmt.Sprintf("tele %d,%d,%d,%d,%d", dest.Level, dest.X>>6, dest.Z>>6, dest.X&0x3f, dest.Z&0x3f)
	if err := actions.Cheat(s, cmd); err != nil {
		return fmt.Errorf("::%s not sent: %w", cmd, err)
	}
	if WaitUntil(timeout, func() bool {
		at, ok := Tile(s)
		return ok && at.Level == dest.Level && Chebyshev(at, dest) <= radius
	}) {
		return nil
	}
	at, _ := Tile(s)
	return fmt.Errorf("%w: ::%s sent but the account is still at %d,%d,%d (%d tiles away) after %s",
		ErrCheatRefused, cmd, at.X, at.Z, at.Level, Chebyshev(at, dest), timeout)
}

// Chebyshev is the game's own distance: the number of steps between two tiles, diagonals
// counting as one. Level is deliberately ignored, so a caller comparing across floors must
// compare Level itself.
func Chebyshev(a, b tile.Tile) int {
	dx, dz := a.X-b.X, a.Z-b.Z
	if dx < 0 {
		dx = -dx
	}
	if dz < 0 {
		dz = -dz
	}
	if dx > dz {
		return dx
	}
	return dz
}

// MovedAtLeast reports whether an account genuinely travelled, and describes what was seen
// either way, so the caller can hand the description straight to Verdict.Check.
//
// A distance floor is what separates "the walk worked" from "the account shuffled one tile
// while stuck against a wall". A same-tile reading is never travel, however long the tool
// waited for it.
func MovedAtLeast(from, to tile.Tile, min int) (bool, string) {
	if from.Level != to.Level {
		return true, fmt.Sprintf("moved %d,%d,%d -> %d,%d,%d (changed level)", from.X, from.Z, from.Level, to.X, to.Z, to.Level)
	}
	d := Chebyshev(from, to)
	if d >= min {
		return true, fmt.Sprintf("moved %d tiles, %d,%d -> %d,%d (needed %d)", d, from.X, from.Z, to.X, to.Z, min)
	}
	return false, fmt.Sprintf("moved only %d tiles, %d,%d -> %d,%d (needed %d)", d, from.X, from.Z, to.X, to.Z, min)
}

// CountChanged reports whether a counted quantity — an item in the pack, a bank stack, an XP
// total — actually changed by at least delta, and describes the reading either way.
//
// Why a delta and not "after > 0": a harness once asked a bank for items it already held and
// counted the pre-existing stack as proof the withdrawal worked. The before reading is the
// only thing that makes the after reading mean anything.
func CountChanged(what string, before, after, delta int) (bool, string) {
	got := after - before
	if delta >= 0 && got >= delta {
		return true, fmt.Sprintf("%s %d -> %d (gained %d, needed %d)", what, before, after, got, delta)
	}
	if delta < 0 && got <= delta {
		return true, fmt.Sprintf("%s %d -> %d (lost %d, needed %d)", what, before, after, -got, -delta)
	}
	return false, fmt.Sprintf("%s %d -> %d (changed by %d, needed %d)", what, before, after, got, delta)
}
