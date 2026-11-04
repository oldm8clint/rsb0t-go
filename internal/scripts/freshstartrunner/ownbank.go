package freshstartrunner

import (
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// Funding a boat fare out of the account's OWN BANK before earning it the hard way.
//
// THE LIVE COST OF NOT DOING THIS, 2026-09-22. acct25 (ProgressiveFisher, Big net, 18 fish
// caught that run) died, respawned at Lumbridge and needed 60 coins for two ship fares. It had
// THIRTEEN THOUSAND THREE HUNDRED AND FORTY coins in its own bank. Instead it pickpocketed
// twenty Men for 63 coins and sold its starter kit for 50 gp, and its own route to the ship ran
//
//	[diag] FindPath 3209,3246,0 -> 3093,3243,0
//
// which is freshstart.Draynor -- the staging waypoint the LegPier route already walks through,
// twelve tiles from the Draynor bank. It walked past its own money to go and steal some.
//
// Banks are shared in this content: one bank, reachable from any booth, so the Draynor booth
// holds whatever the account banked at Catherby. That is what makes this cheap rather than a
// detour -- the account was going to Draynor anyway.
//
// WHY THIS IS IN THE RUNNER AND NOT IN freshstart.DecidePhase. The plan is pure and has no way
// to know whether a bank is reachable, what it holds, or whether a trip has already been tried
// and failed. Putting a PhaseBank in it would need all three threaded through Snapshot by every
// caller. The decision here is narrow -- "before you pickpocket, look in your own bank once" --
// and belongs beside the pickpocketing it precedes.

// ownBankTriedAt throttles the attempt per session, the same unbounded-but-harmless per-session
// map shape this package already uses for its sail lines.
var ownBankTriedAt sync.Map // actions.Session -> time.Time

// OwnBankRetry is how long a failed or empty bank trip suppresses the next one.
//
// Long enough that an account whose bank really is empty is not walked to Draynor and back
// every time it is short, short enough that an account that banks between attempts is not
// locked out of its own money for a whole session. The pickpocket fallback runs the whole time,
// so the cost of waiting is zero.
const OwnBankRetry = 10 * time.Minute

// ownBankRadius is how far from the Draynor waypoint to look for a booth. The bank is twelve
// tiles from it; twenty covers the walk landing anywhere sensible without reaching for a booth
// in another town.
const ownBankRadius = 20

// WithdrawFareFromOwnBank walks to the bank the route already passes and takes out coins,
// returning how many the pack gained. Reports false when it did not get to try, so the caller
// falls through to earning the fare instead of standing still.
//
// Deliberately withdraws EVERYTHING rather than exactly the shortfall: the account is about to
// cross two ship fares, a skirt and whatever the far side costs, and a second trip back here
// costs far more than carrying the coins. The mule hand-off banks the surplus later anyway.
func WithdrawFareFromOwnBank(d *Deps, need int) (got int, ok bool) {
	if d == nil || d.Session == nil || d.Locs == nil || d.Config == nil || d.Components == nil {
		return 0, false
	}
	if last, seen := ownBankTriedAt.Load(d.Session); seen {
		if time.Since(last.(time.Time)) < OwnBankRetry {
			return 0, false
		}
	}
	ownBankTriedAt.Store(d.Session, time.Now())

	before := coinsHeld(d)
	d.logf("short of the fare (%d of %d coins) — trying this account's own bank at Draynor before pickpocketing for it",
		before, need)

	here := currentTile(d)
	if here == nil {
		return 0, false
	}
	if tile.Chebyshev(*here, freshstart.Draynor) > 6 {
		if !WalkNear(d, freshstart.Draynor, 5, 4*time.Minute) {
			d.logf("could not reach the Draynor bank — earning the fare instead")
			return 0, false
		}
	}
	bank, opened := openBoothNear(d, ownBankRadius)
	if !opened {
		d.logf("no bank booth answered at Draynor — earning the fare instead")
		return 0, false
	}
	defer func() { _ = bank.Close() }()

	coinsID, found := int32(0), false
	for _, it := range bank.Items() {
		obj, okObj := d.Config.Obj(int(it.ID))
		if okObj && !obj.Noted() && strings.EqualFold(strings.TrimSpace(obj.Name), freshstart.Coins) && it.Count > 0 {
			coinsID, found = it.ID, true
			break
		}
	}
	if !found {
		d.logf("this account's bank holds no coins — earning the fare instead")
		return 0, false
	}
	// Only the fare and a margin, never the whole purse: live 29 Sep, acct131 drew all 444,277
	// of its banked coins for a 60-coin fare and walked the map with them, one death from losing it.
	draw := int32(need - before + OwnBankFareMargin)
	if err := bank.WithdrawX(coinsID, draw, bankWaiter(d)); err != nil {
		d.logf("withdrawing coins: %v — earning the fare instead", err)
		return 0, false
	}
	// Read the pack back rather than trusting the send: a withdraw that silently did nothing
	// and a bank that was empty look identical from here, and only one of them is worth
	// retrying sooner. docs/trustworthy_harnesses.md's own rule, applied to a live step.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) && coinsHeld(d) <= before {
		if !d.Delay(400 * time.Millisecond) {
			break
		}
	}
	got = coinsHeld(d) - before
	if got <= 0 {
		d.logf("the coin withdraw did not land — earning the fare instead")
		return 0, false
	}
	d.logf("took %d coins out of this account's own bank (%d now held) — no pickpocketing needed",
		got, before+got)
	return got, true
}

// OwnBankFareMargin is what a fare draw takes beyond the fare itself, for the next leg's fare.
const OwnBankFareMargin = 1000

// openBoothNear opens the nearest bank booth within radius, mirroring alchrunner's own
// OpenNearestBank rather than inventing a second way to do it.
func openBoothNear(d *Deps, radius int) (*actions.Bank, bool) {
	here := currentTile(d)
	if here == nil {
		return nil, false
	}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	booths := d.Locs.Find(d.Config, locindex.Query{
		X: here.X, Z: here.Z, Level: here.Level, Radius: radius,
		Name: bankBoothName, Op: bankBoothOp,
	}, live)
	for i, booth := range booths {
		if i >= 4 {
			break
		}
		if !WalkToUse(d, tile.Tile{X: booth.X, Z: booth.Z, Level: booth.Level}, 60*time.Second) {
			continue
		}
		if err := actions.OpLoc(d.Session, booth.Op, booth.X, booth.Z, booth.ID); err != nil {
			continue
		}
		b := actions.NewBank(d.Session, d.Components)
		if waitFor(d, "the bank to open", 20*time.Second, b.IsOpen) {
			d.Delay(500 * time.Millisecond)
			return b, true
		}
	}
	return nil, false
}

const (
	bankBoothName = "Bank booth"
	bankBoothOp   = "Use-quickly"
)

// bankWaiter adapts Deps.Delay to actions.Waiter for a bank withdraw.
type ownBankWaiter struct{ d *Deps }

func bankWaiter(d *Deps) ownBankWaiter { return ownBankWaiter{d} }

func (w ownBankWaiter) Delay(t time.Duration) bool { return w.d.Delay(t) }
func (w ownBankWaiter) DelayTicks(n int) bool {
	return w.d.Delay(time.Duration(n) * 600 * time.Millisecond)
}
func (w ownBankWaiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		if cond() {
			return true
		}
		if !w.d.Delay(200 * time.Millisecond) {
			return false
		}
	}
	return cond()
}
