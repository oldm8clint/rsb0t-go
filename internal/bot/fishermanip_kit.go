package bot

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/chopmanipplan"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/fisherrunner"
	"rsb0t-go/internal/scripts/orderplan"
)

// THE KIT TRIP (a user request: "tick manipulation while fishing needs to be improved and taken on all fishing accounts").
//
// The kit (a knife and a few plain logs) was asked only on the mule hand-off, which comes every ~2,000 banked fish, so most
// fishers fished plain for hours with a mule holding 226 knives and 14,000 logs. A fisher that holds no kit, whose own bank has
// none either, now walks to the mule's post once, asks for it in one kit order and walks back: a few minutes, once, against a
// yield of x1.5 to x2.3 for the rest of its life (the bank then holds the kit: the deposit banks the logs and DrawManipKit draws
// them back at every bank visit). It is at most one trip in manipKitEvery whatever the answer, and it is made right after a bank
// visit, when the pack is empty of fish.
//
// Every fisher script is a fisherBot (ProgressiveFisher, BassOysterCaskets, and LootChestFisher's fallback, which builds the same
// bot from the same settings), and FishUntil has one call site, so this reaches every fishing account.

const (
	// manipKitEvery is the least time between two kit trips, answered or not.
	manipKitEvery = 20 * time.Minute
	// manipKitRetry is how soon the check is made again when the trip could not be planned (the mule's bank unpublished).
	manipKitRetry = 2 * time.Minute
	// manipKitRoom is the free pack slots the trip wants: the kit and what rides with it.
	manipKitRoom = 8
)

// manipKitNeeded reports whether the account lacks the kit in the pack and in the bank as last read, and says what is missing.
func (b *fisherBot) manipKitNeeded(d *fisherrunner.Deps) (missing string, need bool) {
	if !b.manipOn {
		return "", false
	}
	knifePack := fisherrunner.KnifeHeld(d)
	logsPack := fisherrunner.ManipLogsHeld(d)
	if knifePack && logsPack >= 1 {
		return "", false
	}
	if b.bankRead == nil {
		return "", false // the bank has not been read: the bank visit draws what it holds
	}
	knife := knifePack || b.bankRead.Knife > 0
	logs := logsPack >= 1 || b.bankRead.Logs[chopmanipplan.PlainLogs] > 0
	if knife && logs {
		return "", false // the next bank visit draws it
	}
	var m []string
	if !knife {
		m = append(m, "a knife")
	}
	if !logs {
		m = append(m, "plain logs")
	}
	return strings.Join(m, " and "), true
}

// manipKitStep makes the kit trip when it is due and reports whether it took this Loop call. Only called with the phase on fishing.
func (b *fisherBot) manipKitStep(ctx *Context, d *fisherrunner.Deps) bool {
	if b.mule == "" || ctx.Bus == nil || time.Now().Before(b.manipKitAt) {
		return false
	}
	missing, need := b.manipKitNeeded(d)
	if !need || fisherrunner.FreeSlots(d) < manipKitRoom {
		return false
	}
	if w, known := ctx.MuleWorld(b.mule); known && w > 0 && ctx.World() > 0 && w != ctx.World() {
		b.manipKitAt = time.Now().Add(manipKitEvery)
		ctx.logf("fish manip: not fetching the kit: %q is on world %d and this account on %d", b.mule, w, ctx.World())
		return false
	}
	stock := muleBankCount(ctx, b.mule)
	if stock == nil {
		b.manipKitAt = time.Now().Add(manipKitRetry)
		return false // the mule's bank is not published, or stale: nothing to judge by
	}
	in := b.fisherKitInput(ctx, d, stock)
	kit, none := fisherKitPlan(in)
	b.manipKitAt = time.Now().Add(manipKitEvery)
	if !kitHasManip(kit) {
		ctx.logf("fish manip: %q's published bank holds none of the kit (%s missing; short of %s): fishing plain", b.mule, missing, shortNames(none))
		return false
	}
	ctx.logf("fish manip: asking %s for the kit (knife + %d logs) on a trip to its post at %d,%d: %s missing from the pack and the bank", b.mule, fisherrunner.ManipLogsWanted, b.muleStand.X, b.muleStand.Z, missing)
	ctx.SetPhase("handoff")
	b.guildHandoffWalk(ctx, d)
	arrived := false
	for i := 0; i < 3 && !arrived; i++ {
		arrived = fisherrunner.WalkNear(d, b.muleStand, 3)
	}
	if !arrived {
		ctx.logf("fish manip: the walk to the mule's post did not finish — fishing on, the kit is asked again in %s", manipKitEvery)
		return true
	}
	res := b.askKit(ctx, d, in, kit, "on a kit trip")
	b.kitAskAt = time.Now().Add(kitAskEvery)
	if res.Stopped {
		return true
	}
	if len(res.Got) > 0 {
		ctx.logf("fish manip: kit trip done (%v): fishing with the pattern from now on", res.Got)
	} else {
		ctx.logf("fish manip: kit trip brought nothing (%s): fishing plain, asking again in %s", res.Reason, manipKitEvery)
	}
	if !fisherrunner.WalkNear(d, b.layout.Bank, 8) {
		ctx.logf("fish manip: could not walk back to the bank at %d,%d — the next Loop call keeps trying", b.layout.Bank.X, b.layout.Bank.Z)
	}
	return true
}

// kitHasManip reports whether a planned kit carries the knife or the plain logs the manipulation needs.
func kitHasManip(kit []orderplan.KitLine) bool {
	for _, l := range kit {
		if !l.Noted && (strings.EqualFold(l.Item, fisherplan.KnifeName) || strings.EqualFold(l.Item, chopmanipplan.PlainLogs)) {
			return true
		}
	}
	return false
}
