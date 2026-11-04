package donorrunner

import (
	"fmt"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/tile"
)

// closeReason names what THIS side of a trade saw at the moment a trade screen closed with
// nothing crossing. It is the text the calling script prints, so it is written for an
// operator reading run/fleet.log, not for a developer reading a stack trace.
//
// It exists because the donor's own non-traded exit carried no reason at all: every donor
// script in the fleet logs donorrunner.Result.Reason on that branch (internal/bot/fisher.go's
// "fisher: hand-off ended without trading (%s)", and the same shape in essence.go,
// advancedfighter.go, crafter.go, thiever.go and woodcutter.go), and for the single most
// common failure the field was empty — so 265 live ProgressiveFishers could only ever say
// "hand-off ended without trading ()". Diagnosing the LootChestFisher hand-off loop against
// that string cost real time on 2026-09-21 and produced three hypotheses and no answer.
//
// Deliberately NOT a diagnosis. Every branch below reports an OBSERVATION — what screen the
// donor last saw, whether it had accepted, how many stacks were on its own offer — because
// the donor cannot see why the mule closed the screen. Where two causes share one
// observation the text says so rather than picking one.
//
// Pure (no session, no clock) so it is unit-tested directly; driveTrade supplies the five
// readings.
func closeReason(lastScreen donorplan.Screen, lastOffered int, accepted bool, planGroups, skipped int) string {
	suffix := ""
	if skipped > 0 {
		suffix = fmt.Sprintf(" (%d group(s) already skipped as untradeable)", skipped)
	}
	switch {
	case accepted && lastScreen == donorplan.ScreenConfirm:
		// This side did everything it could: it confirmed. Either the mule declined at
		// the confirm screen or it went offline/full. The donor cannot tell which.
		return fmt.Sprintf("this side had confirmed %d stack(s) — the mule either declined at the confirm screen or dropped out of it", lastOffered) + suffix
	case accepted:
		// Accepted at the offer screen, and the confirm screen never arrived. This is the
		// shape the close-flicker bug produced before stillClosed's debounce (see
		// closeDebouncePolls), so a burst of these is a reason to suspect it came back.
		return fmt.Sprintf("this side accepted %d stack(s) at the offer screen but the confirm screen never opened", lastOffered) + suffix
	case lastOffered > 0:
		return fmt.Sprintf("the trade closed with %d stack(s) on this side's offer and neither side accepted", lastOffered) + suffix
	case planGroups == 0:
		// Nothing offerable was in the pack at all. This is the empty-trade loop shape:
		// the caller's own "is there a load?" test counted something the hand-over's
		// offer rules will not offer, so the phase never changes and the trade is a
		// no-op forever. Worth naming loudly, because it is a caller bug, not a mule one.
		return "this side had nothing the hand-over would offer — the load it was sent to deliver is not offerable" + suffix
	default:
		return fmt.Sprintf("the trade closed before any of this side's %d group(s) reached the offer screen", planGroups) + suffix
	}
}

// atPost reports whether a donor standing on here is close enough to the agreed mule post
// to be worth waiting there rather than re-pathing toward a mule it cannot reach.
//
// Split out and made pure because getting it wrong is silent in both directions: too
// generous and a displaced donor waits forever somewhere the mule will never come; too
// strict and it goes back to burning a failed A* every two seconds (the very thing the
// MeetApproach branch that calls this was changed to stop doing). The level check matters
// as much as the distance one — Catherby's bank post is on level 0 and the donor can be
// upstairs.
//
// Uses donorplan.StandRadius, the same tolerance MeetWalk accepts as "arrived", so the two
// cannot disagree about whether the donor is in position.
func atPost(here, stand tile.Tile) bool {
	return here.Level == stand.Level &&
		abs(here.X-stand.X) <= donorplan.StandRadius &&
		abs(here.Z-stand.Z) <= donorplan.StandRadius
}
