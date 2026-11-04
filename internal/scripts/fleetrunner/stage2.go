package fleetrunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/thieverplan"
	"rsb0t-go/internal/scripts/thieverrunner"
)

// Thiev adapts a scarabrunner Deps to a thieverrunner Deps — the same field-for-field copy
// fresh() already does for freshstartrunner, and the same shape every sibling runner that
// borrows another runner's primitives uses.
func Thiev(d *Deps) *thieverrunner.Deps {
	return &thieverrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: thieverrunner.Delayer(d.Delay), Log: d.Log,
		Loot: d.Loot, OnStun: d.OnStun,
	}
}

// FreeSlots is the pack's empty slots.
func FreeSlots(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return PackSlots
	}
	free := PackSlots - len(inv.NonEmpty())
	if free < 0 {
		return 0
	}
	return free
}

// PackSlots is a full pack, matching thieverrunner.PackSlots.
const PackSlots = 28

// MoveTimeout bounds one walk to a rung. Generous: the market legs are short, but a walk that
// starts right after a boat landing or a random-event displacement can be genuinely long, and
// the walker's own escalation ladder (doors, level changes, pocket escapes) takes real time
// before it gives up.
const MoveTimeout = 90 * time.Second

// MoveToMethod walks to where a rung is worked: a stall's own stand tile when it has one (the
// merchant refuses whoever he can see, so the stand tiles are out of his line of sight —
// thieverplan.Method.Stands), otherwise the rung's anchor.
//
// Always through freshstartrunner.WalkWithWalker, never a bare Pack.FindPath: see this file's
// own doc comment.
func MoveToMethod(d *Deps, m thieverplan.Method, standAt int) bool {
	dest, radius := m.Anchor, m.Leash-1
	if radius < 0 {
		radius = 0
	}
	if m.Kind == thieverplan.KindStall && len(m.Stands) > 0 {
		dest, radius = m.Stands[standAt%len(m.Stands)], 0
	}
	return freshstartrunner.WalkWithWalker(Fresh(d), dest, radius, MoveTimeout)
}

// StealBakersStallClassified is StealBakersStall plus the reason a refusal happened, read off
// the chat produced between the click and the result — see scarabplan.StallRefusal for why each
// case wants a different response, and where each message comes from in this content.
//
// Live-found 2026-09-18 and the reason this exists at all: a 150-attempt run against the real
// stall logged 40 refusals, and the stand-tile rotation fired 12 times chasing them. Almost all
// of those were the combat lockout rather than a guard's line of sight — a roused Ardougne guard
// keeps %lastcombat fresh, so every steal is refused for as long as it keeps swinging, and each
// rotation spent a four-waypoint walk achieving nothing.
func StealBakersStallClassified(d *Deps, standAt int) (thieverrunner.StallResult, fleetplan.StallRefusal) {
	clickAt := time.Now()
	res := thieverrunner.StealStall(Thiev(d), fleetplan.BakersStall(), standAt)
	if res.Success || !res.Attempted {
		return res, fleetplan.StallRefusalNone
	}
	if d.Chat == nil {
		return res, fleetplan.StallRefusalUnknown
	}
	return res, fleetplan.ClassifyStallChat(d.Chat.Since(clickAt))
}

// PickpocketKnight pickpockets one Knight of Ardougne.
func PickpocketKnight(d *Deps) thieverrunner.PickResult {
	return thieverrunner.Pickpocket(Thiev(d), fleetplan.ArdougneKnight())
}

// HereIs is a short "x,z,level" for a log line.
func HereIs(d *Deps) string {
	t := CurrentTile(d)
	if t == nil {
		return "an unknown tile"
	}
	return fmt.Sprintf("%d,%d,%d", t.X, t.Z, t.Level)
}
