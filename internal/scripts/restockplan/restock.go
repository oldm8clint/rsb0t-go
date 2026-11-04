// Package restockplan is the pure policy for a mule REPLENISHING ITSELF: deciding it is
// running low, asking the fleet, choosing which of the accounts that answer actually walk over,
// and — on the other side — deciding what one account may safely part with.
//
// THE GAP THIS CLOSES, stated as plainly as internal/bot/mule.go states its own. The mule can
// hand out (orderplan + mulerunner.Handout) and it can receive what a donor brings on the
// donor's own schedule (mulerunner.completeTrade), and that is all. It has NO WAY TO RESTOCK
// ANYTHING: no market-buy path, no acquiretask caller, nothing that turns "I am out of X" into
// "I now have X". What it holds is only ever what donors happened to give it.
//
// That gap went live twice on 2026-09-21, which is why this exists:
//
//   - COINS. The roster was ramped from 12 woodcutters to 94, and each one buys a rune axe from
//     the Seers player market for 150,000 coins drawn from the mule. `mule` started the day at
//     29,837,076 coins and was at roughly 9M by the evening with 82 accounts still unequipped —
//     about 12.3M still to pay out. It runs dry before the cohort is equipped, and when it does,
//     every remaining woodcutter parks. (Its own 6M-per-30-minutes hand-out window fired
//     repeatedly — "hand-out limit for that item is spent" — which paces the burn but creates
//     no coins.)
//   - AXES. The same mule holds ONE chopping axe against 94 accounts asking for one. Each asks
//     down orderplan.AxeHandouts' ladder, is refused at every rung, and falls through to buying
//     a rune axe — which is what spends the coins above.
//
// WHAT THIS PACKAGE GENERALISES AND WHAT IT DOES NOT, because the second failure above is the
// obvious next ask. Everything here is item-generic: Appeal carries an item name, Board
// schedules donors for any item, and Spare is the generic "held minus reserve, if it clears the
// minimum" rule. What is item-SPECIFIC, and what only coins has today, is knowing what an
// account can safely part with — SpareCoins below, with its reserve argued from this repo's own
// existing recovery floats. A "spare axes" rule is a different question with a different answer
// ("any axe beyond the one I am chopping with"), and it is not written or proven here. So: the
// transport, the appeal, the anti-stampede scheduling and the receipt discipline generalise to
// items; the donor-side judgement does not, and claiming otherwise would be exactly the
// "rushed, unverified" mistake this repo's own instructions warn about.
//
// It is pure: no client, actions, fleet or bot imports and no I/O, matching donorplan's,
// fisherplan's and orderplan's own package docs. It imports orderplan only — itself pure — to
// read back the per-window hand-out ceiling a mule is actually configured with, so its restock
// floor is derived from the same number that bounds its hand-outs rather than a second copy
// that can drift. It does NOT reuse orderplan's text grammar; see the closing note in this file
// for why an appeal cannot travel through it.
package restockplan

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/orderplan"
)

// CoinsItem is the canonical spelling of the one item whose donor-side rule is implemented and
// proven. Matches freshstart.Coins and orderplan's own catalogue entry exactly; restated here
// rather than imported so this package keeps orderplan as its only dependency.
const CoinsItem = "Coins"

// ---- "a decent amount of coins": what an account may part with ----

// DonorReserveCoins is what an account KEEPS no matter how badly the mule needs money. An
// account must never be stripped of what it needs to look after itself, so the reserve is
// argued from the numbers this repo already committed to rather than picked:
//
//   - fisherplan.DeathFloatCoins is 500: the banked death-recovery money an account needs to
//     "walk to a bank, withdraw, pay a fare, buy a replacement tool" after dying with an empty
//     pocket. That constant's own doc comment records the measurement that produced it — 189 of
//     269 live accounts held zero banked coins and another 46 held under 500, so 87% could not
//     have funded their own recovery.
//   - donorplan.DefaultTravelReserve is 500: the coins kept in the POCKET across an ordinary
//     mule hand-over as travel and fare money. A restock donation is an ordinary hand-over, so
//     donorrunner.Options.Reserve enforces this half at the trade screen already.
//   - freshstart.DefaultFare is 60: two ship fares at 30 each.
//
// 2,000 is four times the death float, which is the figure this project sized for the CHEAPEST
// recovery (a 5gp small fishing net). This rule applies to every script in the fleet, including
// ones whose own replacement tool is dearer and ones whose reserve this package cannot know, so
// it deliberately leaves room for a recovery that goes wrong twice, plus both fares, plus the
// pocket reserve the trade screen keeps back anyway.
//
// The cost of being generous here is negligible and worth stating: 2,000 coins is 1.3% of one
// rune axe. Against 12.3M still to raise, a reserve four times larger than strictly needed
// costs the fleet a rounding error, while a reserve one coin too small strands an account for
// hours — the exact failure fisherplan.DeathFloatCoins was written after.
//
// A test in this package pins it at or above fisherplan.DeathFloatCoins, so if that float is
// ever raised this reserve cannot silently fall below it.
const DonorReserveCoins = 2000

// MinDonationCoins is the smallest donation worth making the trip for. Below it an account
// keeps working and says nothing.
//
// This is a THROUGHPUT bound, not a safety one, and it comes from measurements already in this
// repo. tools/mule-throughput.py found the mule CAPACITY-bound: one trade partner at a time,
// 1.51 trades/min sustained, a 30-second median cycle of which 20 seconds is the donor drawing
// and settling its offer and 10 the mule banking. So a donation trip costs a fixed ~30 seconds
// of the mule's only trade slot no matter how much it carries, plus one to three minutes of the
// donor's own skilling — about 517 XP at the fleet's measured median 15,517 fishing XP/hour.
//
// 5,000 makes that trade obviously worth taking: it is a thirtieth of the 150,000-coin rune axe
// the coins are for, so thirty donation trips buy a whole axe. At 500 — donorplan's travel
// reserve, the sort of float a fisher carries — it would take three hundred trips and over
// three hours of the mule's entire trade capacity to buy that same axe, which is worse than
// useless because it also blocks the hand-overs that are the mule's actual job.
const MinDonationCoins = 5000

// Spare is the generic "what can I part with" arithmetic: everything held, less what is already
// committed, less the reserve that must stay — reported only if it clears min.
//
// held is pack plus bank plus anywhere else the caller counts; earmarked is what this account
// has ALREADY committed to spend and must not offer. That second argument is not optional
// politeness, it is the one thing that stops a disastrous loop: a woodcutter holding the
// 150,000 coins the mule just gave it to buy an axe with is, by the arithmetic alone, the
// richest donor in the fleet. Handing it straight back would be an infinite round trip that
// equips nobody. The caller is the only side that knows what its own coins are FOR, so it must
// say, and a caller that passes 0 is asserting it has no pending commitment.
//
// Negative inputs are treated as zero rather than rejected: a caller reading a count that is
// briefly unavailable should pledge nothing, not pledge a negative number or panic.
func Spare(held, earmarked, reserve, min int) (int, bool, string) {
	if held < 0 {
		held = 0
	}
	if earmarked < 0 {
		earmarked = 0
	}
	if reserve < 0 {
		reserve = 0
	}
	free := held - earmarked - reserve
	if free <= 0 {
		return 0, false, fmt.Sprintf("holding %d, %d earmarked, %d reserved — nothing spare", held, earmarked, reserve)
	}
	if min > 0 && free < min {
		return 0, false, fmt.Sprintf("only %d spare, under the %d worth walking for", free, min)
	}
	return free, true, ""
}

// SpareCoins is Spare with this package's own coin reserve and minimum, which is what every
// donor script should call unless it has a reason of its own to disagree.
//
// banked and pocket are counted together because the donation flow withdraws the bank into the
// pack and hands over the pack: donorrunner.Options.Reserve keeps DonorReserveCoins back at the
// trade screen, so the reserve is enforced on the real total either way.
func SpareCoins(banked, pocket, earmarked int) (int, bool, string) {
	if banked < 0 {
		banked = 0
	}
	if pocket < 0 {
		pocket = 0
	}
	return Spare(banked+pocket, earmarked, DonorReserveCoins, MinDonationCoins)
}

// ---- the mule's side: when to appeal, and for how much ----

// FloorFactor and TargetFactor turn a mule's own configured hand-out window into its restock
// floor and target, so neither is a magic number that a re-configured mule silently outgrows.
//
// FloorFactor 2: orderplan.Policy already bounds what this mule may legally hand out inside one
// ledger window (Handout.WindowMax, the live fleet's 6,000,000 coins per 30 minutes). Holding
// two full windows' worth means the mule cannot be surprised by its own maximum permitted burn
// while a restock is in flight — and a restock cycle (appeal, pledge, grant, walk, trade) is
// minutes, not windows. With the live configuration that is a 12M floor, which the 9M balance
// on 2026-09-21 was already under; with orderplan's conservative 25,000 default it is 50,000.
// It scales with the configuration instead of against it.
//
// TargetFactor 4 (so the target is twice the floor) is hysteresis and nothing more. An appeal
// that asked for exactly enough to cross back over the floor would put the mule straight back
// under it on the next hand-out and appeal again, and a fleet-wide broadcast every few seconds
// is its own kind of stampede. With the live configuration the target is 24M — close to the
// 29,837,076 the mule actually started the day with, which is a reassuring sanity check on the
// factor rather than the reason for it.
const (
	FloorFactor  = 2
	TargetFactor = 4
)

// AppealInterval is how often a mule that is still below its floor re-broadcasts. Deliberately
// slower than donorrunner.AskRepeatEvery's 10 seconds: one publish already reaches every bot
// running at the time (mulebus.Watcher is checkpoint-based and never misses a message between
// two polls), so re-broadcasting exists only for accounts that log in, reconnect or start their
// script after the first appeal went out. A minute is fast enough for that and slow enough that
// a fleet of ninety is not re-screening an appeal continuously.
const AppealInterval = time.Minute

// AppealTTL is how long a broadcast appeal is still worth answering.
//
// It exists because of a live failure on 2026-09-21: the donor half originally decided on an
// appeal at the moment the message arrived, and mulebus delivers each message exactly once. A
// ProgressiveFisher spends most of its time NOT in its ordinary fishing phase — it is banking,
// walking, buying — and every one of those reads as busy, correctly. So appeal after appeal was
// consumed by a poll that happened to land during a bank trip and simply lost, and an account
// with 57,995 spare coins answered none of them. An appeal is a standing need for as long as
// the mule is still short, not an instant, so the donor remembers the last one and re-screens
// it against its own state on every later poll until it goes stale.
//
// Two AppealIntervals: long enough to cover a whole bank-and-walk cycle of a busy script, short
// enough that a donor never sets off answering a need the mule has since had met — and a mule
// that is still short re-broadcasts inside this window anyway, refreshing it.
const AppealTTL = 2 * AppealInterval

// Need is what a mule knows about its own stock of one item at one moment.
type Need struct {
	// Item is the canonical item name.
	Item string
	// Held is everything the mule can actually draw on: its bank plus its own pack. Read from
	// a REAL bank open, never from a cached snapshot — see mulerunner.ReadBankTotals' own
	// callers for why a second "snapshot" without an open hands back the first.
	Held int
	// Floor is the level below which it appeals, Target what it asks to be topped up to.
	// Both zero means "no restocking configured", which never appeals.
	Floor, Target int
	// Promised is what has already been granted to donors that have not arrived yet
	// (Board.Outstanding). Subtracting it is what stops the mule asking the fleet for the same
	// money three times over while three donors are walking to it with it.
	Promised int
	// LastAppeal is when this mule last broadcast for this item; the zero time means never.
	LastAppeal time.Time
}

// Appeal is one broadcast: what to ask the fleet for.
type Appeal struct {
	Item string
	// Qty is how much would bring the mule back to its target, less what is already promised.
	// It is a WANT, not an instruction to any one donor — no donor is expected to cover it,
	// and Board stops granting once enough has been promised.
	Qty int
}

// DecideAppeal is the whole "am I running low" question. It returns the appeal to broadcast, or
// false and the reason it said nothing.
//
// PROACTIVE, NOT ON EMPTY, and this is a deliberate reading of the owner's own words. The ask
// was "if the mule runs out of coins to hand out, have the mule able to check all the other
// accounts" — but a mule that waits until zero has already blocked every account waiting on it
// for the whole length of a restock cycle, and on 2026-09-21 that was 82 woodcutters. Appealing
// at the floor costs nothing (an unanswered appeal is one bus message) and, because the floor is
// two of this mule's own maximum hand-out windows, it is raised long before the mule could
// legally have spent itself dry. "Runs out" is a state this makes unreachable rather than one it
// reacts to.
func DecideAppeal(now time.Time, n Need) (Appeal, bool, string) {
	if strings.TrimSpace(n.Item) == "" {
		return Appeal{}, false, "no item named"
	}
	if n.Floor <= 0 || n.Target <= 0 {
		return Appeal{}, false, "restocking is switched off"
	}
	if n.Target <= n.Floor {
		// Not a configuration this package can act on sensibly: the appeal would ask for at
		// most zero. Refusing loudly beats appealing for nothing on a loop.
		return Appeal{}, false, fmt.Sprintf("target %d is not above floor %d", n.Target, n.Floor)
	}
	promised := n.Promised
	if promised < 0 {
		promised = 0
	}
	held := n.Held
	if held < 0 {
		held = 0
	}
	// Count what is already walking towards us as if it had arrived. Without this the mule
	// broadcasts the same shortfall every minute while three donors are already carrying it,
	// and the fleet answers three times over.
	effective := held + promised
	if effective >= n.Floor {
		return Appeal{}, false, fmt.Sprintf("%d %s in hand (%d of it promised), floor %d", effective, n.Item, promised, n.Floor)
	}
	if !n.LastAppeal.IsZero() && now.Sub(n.LastAppeal) < AppealInterval {
		return Appeal{}, false, fmt.Sprintf("appealed %s ago, next in %s", now.Sub(n.LastAppeal).Round(time.Second), AppealInterval)
	}
	want := n.Target - effective
	if want < 1 {
		want = 1
	}
	return Appeal{Item: n.Item, Qty: want}, true, ""
}

// FloorAndTarget derives a mule's restock floor and target from the per-window hand-out ceiling
// it is actually configured with (orderplan.Handout.WindowMax for that item, which is what
// orderplan.Prescreen enforces). A non-positive window means "no ceiling configured", and this
// returns zeroes — which DecideAppeal reads as restocking switched off, failing closed.
func FloorAndTarget(perWindow int) (floor, target int) {
	if perWindow <= 0 {
		return 0, 0
	}
	return perWindow * FloorFactor, perWindow * TargetFactor
}

// WindowFor is the per-window ceiling pol actually applies to item — the catalogue entry's own
// WindowMax if it has one, otherwise the policy-wide MaxUnitsPerItem. Exactly the choice
// orderplan.Prescreen makes internally; read back here so a mule's floor is derived from the
// same number that bounds its hand-outs rather than a second copy of it that can drift.
func WindowFor(pol orderplan.Policy, item string) int {
	h, ok := orderplan.InCatalog(pol, item)
	if !ok {
		return 0
	}
	if h.WindowMax > 0 {
		return h.WindowMax
	}
	return pol.MaxUnitsPerItem
}

// ---- the donor's side: answering an appeal ----

// AnswerCooldown is how long one account waits after donating before it will answer another
// appeal. It spreads the load across the fleet instead of draining whichever account happens to
// poll first, and it guarantees real skilling happens between trips — the same reasoning
// internal/bot/fisher.go's own hand-off jitter records after measuring 13 donors each burning a
// full timeout in one twelve-minute window.
const AnswerCooldown = 10 * time.Minute

// DonorState is what an account knows about its own ability to answer.
type DonorState struct {
	// Banked and Pocket are its coin counts. Banked comes from its own last real bank reading;
	// a pledge is provisional by construction (see mulebus.PledgeMessage) and only the mule's
	// own received count ever settles what arrived.
	Banked, Pocket int
	// Earmarked is what this account has already committed to spend. See Spare — passing this
	// wrong is the one way to build an infinite round trip.
	Earmarked int
	// LastDonated is when it last completed one; zero means never.
	LastDonated time.Time
	// Busy is true when the script is in the middle of something a detour would ruin — a
	// hand-over already under way, a trade screen open, a random event. A donation is never
	// urgent enough to interrupt anything.
	Busy bool
}

// DecideAnswer decides whether this account pledges to an appeal, and for how much.
//
// It only ever answers for CoinsItem. That is not a placeholder: the donor-side judgement for
// any other item is a different rule that this package does not implement (see the package
// doc), and answering an appeal for an item whose "spare" rule is unwritten would mean guessing
// at what an account can afford to lose.
func DecideAnswer(now time.Time, a Appeal, d DonorState) (int, bool, string) {
	if !strings.EqualFold(strings.TrimSpace(a.Item), CoinsItem) {
		return 0, false, fmt.Sprintf("I have no rule for sparing %q", a.Item)
	}
	if a.Qty < 1 {
		return 0, false, "the appeal asks for nothing"
	}
	if d.Busy {
		return 0, false, "busy with my own script"
	}
	if !d.LastDonated.IsZero() && now.Sub(d.LastDonated) < AnswerCooldown {
		return 0, false, fmt.Sprintf("donated %s ago, next no sooner than %s", now.Sub(d.LastDonated).Round(time.Second), AnswerCooldown)
	}
	spare, ok, why := SpareCoins(d.Banked, d.Pocket, d.Earmarked)
	if !ok {
		return 0, false, why
	}
	// Never pledge more than the mule actually asked for. Ninety accounts each pledging their
	// whole surplus against a want of 15M is how a fleet over-answers, and Board would then be
	// granting trips for money nobody needs.
	if spare > a.Qty {
		spare = a.Qty
	}
	return spare, true, ""
}

// ---- the anti-stampede: who actually walks ----

// DefaultMaxInFlight is how many donors may hold a live grant at once.
//
// ONE would be safe and too slow, NINETY is the contention failure the Seers market stall
// taught this fleet on 2026-09-21 — one trade at a time and everyone else standing there
// waiting. The measured cycle (tools/mule-throughput.py) settles it: a 30-second median of
// which the mule only WAITS for 20, against a donor walk that is minutes. With one donor in
// flight the mule idles for the whole walk; with three, one is trading while two travel, and
// the queue can never exceed what a single trade slot absorbs inside a grant's lifetime.
//
// It is a parameter, not a constant, so a mule with a nearer donor camp can raise it.
const DefaultMaxInFlight = 3

// GrantTTL is how long a grant stays live before its slot is returned to the queue. Long enough
// for a real cross-map walk (donorrunner.walkNear alone budgets 75 seconds per leg and a donor
// may have to finish a fishing spot first), short enough that an account that died, logged out
// or was pulled into a random event does not hold a slot for the rest of the run.
const GrantTTL = 5 * time.Minute

// PledgeTTL is how long a pledge is still worth acting on. Shorter than GrantTTL on purpose: a
// pledge is a reading of a bank the donor is not standing at, and the older it is the less it
// says. A donor still willing re-publishes on the next appeal.
const PledgeTTL = 3 * time.Minute

type pledge struct {
	at   time.Time
	from string
	qty  int
}

type granted struct {
	at   time.Time
	from string
	qty  int
}

// Board is the mule's queue of accounts that have answered an appeal and the bounded set it has
// actually told to come. It holds no policy about WHAT to ask for — that is DecideAppeal — only
// about who goes and when.
//
// Go-idiom deviation, the same one orderplan.Ledger, mulereceiverplan's Collection/PickupQueue
// and mulebus.Roster already make and document: every method takes the moment as an explicit
// time.Time rather than reading the wall clock, so this stays usable from a pure test.
type Board struct {
	maxInFlight int
	pledges     []pledge
	grants      []granted
}

// NewBoard returns an empty board. maxInFlight below 1 falls back to DefaultMaxInFlight rather
// than to zero, because a board that can never grant anything is a silent way to switch the
// whole feature off and would look exactly like a fleet that simply had no coins.
func NewBoard(maxInFlight int) *Board {
	if maxInFlight < 1 {
		maxInFlight = DefaultMaxInFlight
	}
	return &Board{maxInFlight: maxInFlight}
}

// MaxInFlight is how many grants this board will hold live at once.
func (b *Board) MaxInFlight() int { return b.maxInFlight }

func (b *Board) sweep(now time.Time) {
	kp := b.pledges[:0]
	for _, p := range b.pledges {
		if now.Sub(p.at) < PledgeTTL {
			kp = append(kp, p)
		}
	}
	b.pledges = kp
	kg := b.grants[:0]
	for _, g := range b.grants {
		if now.Sub(g.at) < GrantTTL {
			kg = append(kg, g)
		}
	}
	b.grants = kg
}

// normalise folds a name the identical way orderplan.normalise, mulerunner.normalisePlayerName
// and donorrunner.normaliseTargetName already do (lowercase, underscore to space) — an
// account's login username uses an underscore where its in-game display name shows a space.
// MUST stay identical to those, not merely similar.
func normalise(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "_", " ")
}

// Pledge records (or refreshes) one account's offer. A second pledge from the same account
// replaces the first rather than queueing twice: it is a fresher reading of the same bank.
func (b *Board) Pledge(now time.Time, from string, qty int) {
	n := normalise(from)
	if n == "" || qty < 1 {
		return
	}
	b.sweep(now)
	for i := range b.pledges {
		if b.pledges[i].from == n {
			b.pledges[i] = pledge{at: now, from: n, qty: qty}
			return
		}
	}
	b.pledges = append(b.pledges, pledge{at: now, from: n, qty: qty})
}

// Waiting is how many live pledges have not been granted yet.
func (b *Board) Waiting(now time.Time) int {
	b.sweep(now)
	n := 0
	for _, p := range b.pledges {
		if !b.holdsGrant(p.from) {
			n++
		}
	}
	return n
}

// InFlight is how many grants are live.
func (b *Board) InFlight(now time.Time) int {
	b.sweep(now)
	return len(b.grants)
}

// Outstanding is how much has been granted and not yet arrived — what DecideAppeal subtracts so
// the mule does not ask the fleet for money three donors are already walking over with.
func (b *Board) Outstanding(now time.Time) int {
	b.sweep(now)
	n := 0
	for _, g := range b.grants {
		n += g.qty
	}
	return n
}

func (b *Board) holdsGrant(normalised string) bool {
	for _, g := range b.grants {
		if g.from == normalised {
			return true
		}
	}
	return false
}

// Granted reports whether from currently holds a live grant.
func (b *Board) Granted(now time.Time, from string) bool {
	b.sweep(now)
	return b.holdsGrant(normalise(from))
}

// Grant is one account told to come, and for how much.
type Grant struct {
	To  string
	Qty int
}

// NextGrants chooses who walks. It returns at most (maxInFlight - live grants) accounts, and
// stops early once the grants live after this call cover want.
//
// LARGEST PLEDGE FIRST, which is the whole point and is measured rather than assumed. The mule
// is capacity-bound at one trade partner at a time: tools/mule-throughput.py found 1.51
// trades/min against 5.0 distinct donors/min, a 3.3x shortfall, with each trade costing a fixed
// ~30 seconds whatever it carries. Serving the richest donor first therefore raises the most
// coins per unit of the one resource that is actually scarce. Ties break on the name so the
// order is stable and a test can assert it.
//
// want <= 0 means "as much as anyone will give": every free slot is filled.
func (b *Board) NextGrants(now time.Time, want int) []Grant {
	b.sweep(now)
	slots := b.maxInFlight - len(b.grants)
	if slots <= 0 {
		return nil
	}
	covered := b.Outstanding(now)
	free := make([]pledge, 0, len(b.pledges))
	for _, p := range b.pledges {
		if !b.holdsGrant(p.from) {
			free = append(free, p)
		}
	}
	sort.SliceStable(free, func(i, j int) bool {
		if free[i].qty != free[j].qty {
			return free[i].qty > free[j].qty
		}
		return free[i].from < free[j].from
	})
	var out []Grant
	for _, p := range free {
		if slots <= 0 {
			break
		}
		if want > 0 && covered >= want {
			break
		}
		b.grants = append(b.grants, granted{at: now, from: p.from, qty: p.qty})
		out = append(out, Grant{To: p.from, Qty: p.qty})
		covered += p.qty
		slots--
	}
	// A granted pledge leaves the queue: the account is now walking, and its next pledge (if
	// it has more to give) is a fresh reading after it has handed this one over.
	if len(out) > 0 {
		kept := b.pledges[:0]
		for _, p := range b.pledges {
			if !b.holdsGrant(p.from) {
				kept = append(kept, p)
			}
		}
		b.pledges = kept
	}
	return out
}

// Arrived releases from's grant, whether the donation succeeded or not. Call it on ANY
// terminal outcome — coins received, trade declined, donor gave up — because the slot is about
// occupancy, not success: a donor that arrived and failed is no longer walking, and holding its
// slot open for the rest of GrantTTL would throttle the queue for nothing.
//
// Receipt is a separate question and is never settled here. Whether coins actually arrived is
// decided by the mule's own count rising (see restockrunner and mulerunner.completeTrade), not
// by this call.
func (b *Board) Arrived(now time.Time, from string) bool {
	b.sweep(now)
	n := normalise(from)
	kept, found := b.grants[:0], false
	for _, g := range b.grants {
		if g.from == n {
			found = true
			continue
		}
		kept = append(kept, g)
	}
	b.grants = kept
	return found
}

// Describe is a one-line state summary for a log an operator reads at a glance.
func (b *Board) Describe(now time.Time) string {
	return fmt.Sprintf("%d pledge(s) waiting, %d of %d donor(s) in flight carrying %d",
		b.Waiting(now), b.InFlight(now), b.maxInFlight, b.Outstanding(now))
}

// ---- why there is no text grammar here ----

// There deliberately is NO AppealText/ParseAppealText pair, and the reason is worth recording
// because the obvious design has it: orderplan.Format/Parse is the order grammar both ends of a
// hand-OUT already agree on, and reusing it for an appeal would make both directions of the
// conversation read identically in a log.
//
// It cannot carry an appeal. orderplan.QtyMax is 1,000,000, and that bound is not incidental —
// its own doc comment records that it was 1,000 until 2026-09-21, that this silently made the
// PARSER the real limit on every coin order the fleet sent, and that a woodcutter asking for a
// 150,000-coin rune axe was refused by it however high the catalogue's ceilings were raised. A
// hand-out quantity is what ONE donor may take, so a million is ample headroom for it. An
// appeal's Qty is a different quantity of a different kind: what the whole fleet is being asked
// to make up, which on the live mule's own configuration is 15,000,000. Writing it through a
// parser bounded at a million would truncate it or refuse it, and the first version of this
// package's own test caught exactly that.
//
// So the appeal travels as STRUCTURED FIELDS (mulebus.Message.Item and .Qty), never as text,
// and the two grammars are kept apart rather than one being widened to fit the other. Widening
// QtyMax to cover an appeal would loosen the hand-out parser for every donor order on the bus
// to serve a message that never needed a parser at all.
