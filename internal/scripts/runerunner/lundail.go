package runerunner

import (
	"fmt"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
)

// LundailLeg is one pass of the Mage Bank sub-machine.
type LundailLeg struct {
	Phase fleetplan.Phase3
	// Visit and Plan are set on the pass that bought from Lundail.
	Visit *fleetrunner.ShopVisit
	Plan  fleetplan.PurchasePlan
	// Deposited is set on the pass that banked at Gundai.
	Deposited *fleetrunner.BankedAtGundai
	// Changed reports a Trip change the caller must save before anything else.
	Changed bool
	// Failed reports a leg that did not complete; the caller waits a few ticks and tries again.
	Failed bool
	// Short reports a Gundai withdrawal that left too few coins for a worthwhile purchase (the
	// bank would not open, or held too little): the purchase was skipped and the trip heads out.
	Short bool
	Note  string
}

// LundailStep runs one leg of a Mage Bank trip already under way (the lever pulled, or the
// account in the Wilderness or the Mage Bank), with exactly the dispatch ScarabSwarm's Stage 3
// uses (internal/bot/scarabswarm_stage3.go, Phase3ToBank to Phase3ToKeep) on the buyer's own
// trip state: scarabplan.DecideStage3Phase picks the leg from the tile, the trip and the pack.
//
// No new route code: every leg is scarabrunner's (EnterMageBank, WithdrawAtGundai,
// BuyFromLundailPlanned, DepositAtGundai, ClimbOutOfMageBank, PullKeepLever). The one
// buyer-specific rule is the flee: attacked on the way in and nearer the Keep lever than the
// ladder, it leaves (the kit carries no food by rule).
//
// worthBuying, when set, is asked after the Gundai withdrawal with the coins now in the pack; false
// skips the purchase (Trip.Bought is set without a visit) so the account leaves at once rather
// than opening Lundail's shop with an empty pouch (live, 26 September: Gundai's dialogue never
// opened the bank, and the trip crossed the Wilderness to buy nothing).
//
// It does nothing outside the Wilderness and the Mage Bank; the caller ends the trip there.
func LundailStep(d *fleetrunner.Deps, trip *fleetplan.Trip, webs *fleetrunner.WebsCut, inCombat bool,
	planFor func(seen map[string]int, coins int) fleetplan.PurchasePlan, worthBuying func(coins int) bool) LundailLeg {
	here := fleetrunner.Here(d)
	leg := LundailLeg{Phase: fleetplan.DecideStage3Phase(fleetplan.Stage3Snapshot{
		Tile: here, Trip: *trip, PackOnlyBlade: fleetrunner.OnlyBladeLeft(d),
	})}
	switch leg.Phase {
	case fleetplan.Phase3ToBank:
		if here != nil && inCombat && fleetplan.WildernessHeading(*here, false, true) == fleetplan.KeepLever {
			leg.Note = "attacked on the way in, nearer the Keep lever: leaving"
			leg.Failed = !fleetrunner.PullKeepLever(d)
			return leg
		}
		leg.Failed = !fleetrunner.EnterMageBank(d, webs)
	case fleetplan.Phase3Withdraw:
		got, why := fleetrunner.WithdrawAtGundai(d, trip.PlannedCoins)
		leg.Note = fmt.Sprintf("drew %d coins at Gundai for a %d-coin purchase %s", got, trip.PlannedCoins, why)
		trip.Withdrawn = true
		leg.Changed = true
		if worthBuying != nil && !worthBuying(got) {
			trip.Bought = true
			leg.Short = true
			leg.Note += fmt.Sprintf("; %d coins buy too little here: skipping Lundail and leaving", got)
		}
	case fleetplan.Phase3Buy:
		capture := func(seen map[string]int, coins int) fleetplan.PurchasePlan {
			leg.Plan = planFor(seen, coins)
			return leg.Plan
		}
		v := fleetrunner.BuyFromLundailPlanned(d, capture)
		leg.Visit = &v
		trip.Bought = true
		leg.Changed = true
	case fleetplan.Phase3Deposit:
		res := fleetrunner.DepositAtGundai(d)
		leg.Deposited = &res
		if !res.OK {
			leg.Failed = true
			leg.Note = "Gundai deposit: " + res.Reason
		}
	case fleetplan.Phase3ClimbOut:
		leg.Failed = !fleetrunner.ClimbOutOfMageBank(d)
	case fleetplan.Phase3ToKeep:
		leg.Failed = !fleetrunner.PullKeepLever(d)
	}
	return leg
}
