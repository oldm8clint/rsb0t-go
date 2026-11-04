// Package peerplan is the pure policy for FLEET-WIDE PEER ITEM SHARING: deciding what one
// account may safely part with, which of the accounts holding a spare is genuinely nearest,
// whether going to that peer beats going to the mule at all, and what an account does when it
// loses the turn to somebody else.
//
// THE GAP THIS CLOSES, with the numbers that produced it. The fleet has exactly one mule, it
// serves ONE trade at a time, and it is measured as CAPACITY-bound rather than
// scheduling-bound: 84% busy, 1 idle minute in 15.3, median gap to the next request 0s, 1.51
// trades/min against 5.0 distinct donors/min — a 3.3x shortfall. Meanwhile the roster went
// from 12 woodcutters to 94, each of which walks 83 tiles from the Seers bank to the mule's
// Catherby post for an axe the mule holds exactly ONE of, asks down orderplan.AxeHandouts'
// ladder (mithril, steel, iron, bronze), is refused at every rung, and walks back empty.
//
// And the fleet already had the axes. A dozen woodcutters that bought rune axes that same day
// BANKED the bronze axes those superseded (internal/bot/woodcutteraxe.go's bankAfterPurchase
// puts them away deliberately). The items the blocked accounts needed were sitting in other
// accounts' banks, with nothing in the codebase able to see them or move them. That is this
// package's headline case and its acceptance test.
//
// ---- THE FOUR DECISIONS, each settled explicitly rather than by default ----
//
// 1. WHAT COUNTS AS "EXTRA". A spare is a SECOND one, never the only one. Reserve below is the
// rule, and it has two halves because items do. For a LADDERED TOOL (axes, fishing gear,
// pickaxes) the holder keeps the single best rung it owns and everything else is spare —
// which is exactly the bronze-axe case, and exactly why a woodcutter holding ONE bronze axe
// and nothing better offers nothing. For anything else the floor is per-item and defaults to
// KeepLast (1), so the last unit never leaves. Coins get a named floor argued from constants
// this repo already committed to rather than picked: see ReserveCoins.
//
// 2. NEAREST BY WHAT. NOT Chebyshev, and the package will not compute one. Cost is INJECTED as
// a CostFn the caller fills from a real internal/webwalk planner, because this fleet has ship
// crossings and a fare: two accounts 40 tiles apart across water are a fare and two boats
// away, and webwalk.DefaultCost prices one KindShip edge at 36 run-tile units against a
// KindDoor's 4. A straight-line answer would happily send an account at Catherby to a "nearer"
// peer on Karamja and take longer than the mule trip it replaced, which defeats the feature.
// A peer the planner cannot route to at all is dropped, never guessed at. See Choose.
//
// 3. CONTENTION. An account serves one trade at a time exactly as the mule does, so if sixty
// accounts all pick the same nearest holder the bottleneck has simply moved. Turn-taking is
// per-HOLDER and exclusive (peerbank.Directory.Reserve, which is atomic because it must be),
// and an account that cannot get a turn GOES BACK TO WORK and retries after BusyRetry rather
// than queueing. That is not invented here: the live Seers market stall taught this fleet the
// same lesson on 2026-09-21, and internal/bot/woodcutteraxe.go's axeBusyRetry records it —
// "losing a turn is not a refusal". Choose therefore returns a RANKED list, not one name, so a
// caller that loses a turn tries the next holder in the same pass instead of starting over.
//
// 4. WHO INTERRUPTS WHOM. The ASKER travels, always. The asker is already blocked and earning
// nothing; the holder is working and earning, so the cheapest arrangement is the one that
// moves the account that has nothing better to do. That also happens to be what the existing
// machinery requires — mulerunner.Handout's own doc comment is explicit that it serves a
// partner ALREADY VISIBLE and does not travel — so the direction is free rather than a new
// trip machine. Beyond that, a holder that says it is mid-task costs InterruptPenalty extra
// tiles in the ranking, so an equally distant peer standing at a bank wins; and if the best
// peer does not beat the mule trip by Margin, the answer is GO TO THE MULE and the holder is
// never interrupted at all. Asking must never make a blocked account more stuck than it was.
//
// ---- WHY THERE IS NO NEW MESSAGE KIND ----
//
// A peer request needs no protocol of its own. mulebus.KindOrder is already "an item order
// placed over the bus, addressed To somebody"; donorrunner.Await already publishes exactly
// that and waits, receipt-verified, for the item to reach its own pack; mulerunner.Handout
// already serves one from a bank behind an orderplan.Policy. Every one of those is
// counterparty-agnostic — the only thing that made them "the mule" was the name in the To
// field. So peer sharing is the same conversation with a different addressee, and this package
// decides the addressee. That also keeps it clear of the in-flight "mulerestock" session,
// which owns the OTHER new direction (the mule broadcasting an appeal, KindAppeal/KindPledge)
// and owns internal/mulebus/bus.go; see COLLAB.md for the agreed split.
//
// Pure: no client, actions, fleet, bot or peerbank imports and no I/O, matching donorplan's,
// fisherplan's and orderplan's own package docs. It imports fisherplan and donorplan only to
// READ two existing float constants, so its reserve cannot silently drift below theirs.
package peerplan

import (
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/tile"
)

// ---- what an account holds ----

// Holding is one named item and how many. Mirrors peerbank.Holding, restated here rather than
// imported so this package stays pure and depends on nothing that holds a lock — the same
// small per-package duplication donorrunner and mulerunner already make for nonNotedID.
type Holding struct {
	Item  string
	Count int
}

// Peer is one candidate holder as the chooser sees it.
type Peer struct {
	// Account is the holder's name, which is also what a KindOrder is addressed to.
	Account string
	// At is where it was standing when it last published; Located is false when it has never
	// had a position fix, which makes it uncheosable rather than "at 0,0".
	At      tile.Tile
	Located bool
	// Bank is what can actually be withdrawn and handed over. Held is its pack and equipment.
	// Spare is judged against BOTH (an axe in hand is still an axe it is using) but only the
	// BANKED count can ever cross a trade screen — see Spare's own note.
	Bank []Holding
	Held []Holding
	// Busy is a turn already taken by somebody else. Excluded outright, never merely ranked
	// down: an account serves one trade at a time.
	Busy bool
	// Working says the holder is mid-task rather than standing at a bank. Not an exclusion —
	// a ranking penalty, see InterruptPenalty.
	Working bool
	// Age is how old this row is. Older than Reserve.MaxAge is excluded: a reading from an
	// hour ago is a guess, and walking on a guess costs the trip.
	Age time.Duration
}

// Request is one blocked account's ask.
type Request struct {
	// Asker is this account's own name — a peer never gives to itself, so this is compared.
	Asker string
	// At is where the asker is standing. Required: with no fix there is no "nearest".
	At      tile.Tile
	Located bool
	// Item is the canonical item name wanted, spelled as the game's object config spells it.
	Item string
	// Qty is how many would help. One is usually enough to unblock a script, and Choose does
	// NOT require a peer to cover the whole quantity — a peer with fewer spares than asked for
	// is still a candidate, because one axe unblocks a woodcutter just as well as three.
	Qty int
}

// ---- 1. WHAT COUNTS AS "EXTRA" ----

// Family is a ladder of interchangeable tools, worst rung first. A holder keeps the single
// best rung it owns; everything below that, and every duplicate of it, is spare.
//
// This is the rule that makes the headline case work and the negative control fail, in one
// sentence: a woodcutter holding a rune axe and a banked bronze axe keeps the rune and the
// bronze is spare; a woodcutter holding ONE bronze axe and nothing better keeps it and offers
// nothing at all.
type Family struct {
	// Name is for log lines only.
	Name string
	// Ladder is the rungs, WORST FIRST. Compared case-insensitively on the trimmed name.
	Ladder []string
}

// rank is the rung's index in the ladder, or -1 when the item is not in this family.
func (f Family) rank(item string) int {
	want := norm(item)
	for i, rung := range f.Ladder {
		if norm(rung) == want {
			return i
		}
	}
	return -1
}

func norm(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// AxeFamily is the chopping ladder, worst first. Matches the rungs internal/scripts/woodcutplan
// already names; restated rather than imported because that package is claimed by another
// session and this one must not depend on its shape changing.
func AxeFamily() Family {
	return Family{Name: "axe", Ladder: []string{
		"Bronze axe", "Iron axe", "Steel axe", "Black axe", "Mithril axe", "Adamant axe", "Rune axe",
	}}
}

// PickaxeFamily is the mining ladder, worst first.
func PickaxeFamily() Family {
	return Family{Name: "pickaxe", Ladder: []string{
		"Bronze pickaxe", "Iron pickaxe", "Steel pickaxe", "Mithril pickaxe", "Adamant pickaxe", "Rune pickaxe",
	}}
}

// NetFamily is the fishing ladder. Deliberately SHORT and deliberately not a full kit: a big
// net and a small net catch different fish at different levels, so "the best one" is not a
// well-defined thing across the whole of fishing. Only the two that genuinely supersede each
// other for the same job are laddered, and everything else in a fishing kit falls through to
// the per-item KeepLast floor, which is the safe answer.
func NetFamily() Family {
	return Family{Name: "net", Ladder: []string{"Small fishing net", "Big fishing net"}}
}

// ReserveCoins is what an account KEEPS no matter who asks, and it is derived rather than
// picked so it cannot silently fall below the floats this repo already committed to:
//
//   - fisherplan.DeathFloatCoins (500) is the BANKED death-recovery money an account needs to
//     walk to a bank, withdraw, pay a fare and buy a replacement tool after dying with an
//     empty pocket. Its own doc comment records why: 189 of 269 live accounts held zero banked
//     coins and another 46 held under 500, so 87% could not have funded their own recovery.
//   - donorplan.DefaultTravelReserve (500) is the coins kept in the POCKET across an ordinary
//     hand-over as travel and fare money.
//
// Both, plus the same again as slack, because this rule applies to every script in the fleet
// including ones whose replacement tool is dearer than the 5gp small net that float was sized
// for, and because the asymmetry is brutal: a reserve a little too large costs the fleet a
// rounding error, while a reserve one coin too small strands an account for hours — which is
// the exact failure DeathFloatCoins was written after. A test in this package pins it at or
// above both constants, so raising either cannot leave this behind.
var ReserveCoins = 2*(fisherplan.DeathFloatCoins+donorplan.DefaultTravelReserve) + 0

// CoinsItem is the canonical spelling, matching freshstart.Coins.
const CoinsItem = "Coins"

// KeepLast is the floor for any item with no named reserve and no family: never give away the
// last one. "A spare is a SECOND one, not the only one" is the whole rule, and this is it.
const KeepLast = 1

// Reserve is what a holder refuses to part with.
type Reserve struct {
	// Keep is a per-item floor by canonical name, compared case-insensitively. Overrides
	// KeepLast for that item. A family rung named here is ALSO honoured — the stricter of the
	// two wins, so an operator can protect a specific tool without rewriting the ladder.
	Keep map[string]int
	// Families are the tool ladders. An item in a family keeps the holder's best rung.
	Families []Family
	// MaxAge is how old a directory row may be and still be believed. Zero means DefaultMaxAge.
	MaxAge time.Duration
}

// DefaultMaxAge is how stale a bank reading may be before a peer is dropped from the running.
//
// Sized against a banking script's own rhythm rather than picked: a fisher or woodcutter banks
// roughly every load, a few minutes apart, so ten minutes is several trips' grace for an
// account that is simply between banks, while still dropping an account that has been away,
// stranded or logged out. The cost of believing a stale row is one wasted trip and a decline;
// the cost of dropping a fresh one is that the asker walks to the mule instead, which is what
// it did before this feature existed. So this errs long.
const DefaultMaxAge = 10 * time.Minute

// DefaultReserve is the fleet-wide rule: the three tool ladders, and a coin floor.
func DefaultReserve() Reserve {
	return Reserve{
		Keep:     map[string]int{norm(CoinsItem): ReserveCoins},
		Families: []Family{AxeFamily(), PickaxeFamily(), NetFamily()},
		MaxAge:   DefaultMaxAge,
	}
}

func (r Reserve) maxAge() time.Duration {
	if r.MaxAge <= 0 {
		return DefaultMaxAge
	}
	return r.MaxAge
}

// count totals a named item across a holdings list.
func count(list []Holding, item string) int {
	want := norm(item)
	n := 0
	for _, h := range list {
		if norm(h.Item) == want && h.Count > 0 {
			n += h.Count
		}
	}
	return n
}

// familyFor returns the ladder containing item, if any.
func (r Reserve) familyFor(item string) (Family, bool) {
	for _, f := range r.Families {
		if f.rank(item) >= 0 {
			return f, true
		}
	}
	return Family{}, false
}

// bestRank is the highest ladder rung the holder owns anywhere (bank or hand), or -1.
func bestRank(f Family, p Peer) int {
	best := -1
	for _, list := range [][]Holding{p.Bank, p.Held} {
		for _, h := range list {
			if h.Count <= 0 {
				continue
			}
			if rk := f.rank(h.Item); rk > best {
				best = rk
			}
		}
	}
	return best
}

// Spare is how many of item p could hand over without stripping itself — the answer to design
// question 1, and the only place "extra" is defined.
//
// TOTAL is judged across bank AND hand, because an axe in the pack is still an axe the account
// is using and pretending otherwise would let a holder give away the tool it is working with.
// But the number RETURNED is capped by the BANKED count, because that is all that can actually
// be withdrawn and traded: mulerunner.Handout draws from a bank booth, and a holder whose only
// spare is in its own pack has nothing this mechanism can move. The two being different is the
// point, not an oversight.
//
// The family rule and the per-item floor are both applied, and the STRICTER wins, so naming a
// rung in Keep can only ever make a holder more careful.
func Spare(p Peer, item string, r Reserve) int {
	if norm(item) == "" {
		return 0
	}
	banked := count(p.Bank, item)
	if banked <= 0 {
		return 0
	}
	total := banked + count(p.Held, item)

	keep := KeepLast
	if named, ok := r.Keep[norm(item)]; ok && named > keep {
		keep = named
	}
	if f, ok := r.familyFor(item); ok {
		// Inside a ladder the holder keeps its BEST rung, and only its best rung. An item
		// that is not the best it owns is fully spare down to zero; the best rung itself
		// keeps one.
		if best := bestRank(f, p); best >= 0 && f.rank(item) < best {
			// Superseded: the holder has something better, so it needs none of these.
			// The named floor (if any) still applies and still wins if stricter.
			if named, ok := r.Keep[norm(item)]; ok {
				keep = named
			} else {
				keep = 0
			}
		}
	}

	spare := total - keep
	if spare > banked {
		spare = banked
	}
	if spare < 0 {
		return 0
	}
	return spare
}

// ---- 2. NEAREST BY WHAT ----

// CostFn prices the trip from one tile to another in run-tile units, and reports false when
// there is NO ROUTE it can plan.
//
// It is injected rather than computed here, and that is the whole answer to design question 2.
// The caller fills it from a real internal/webwalk planner, so a ship crossing costs what
// webwalk.DefaultCost says a KindShip edge costs (36 run-tile units, against 4 for a door and
// 6 for a stair) and a fare and two boats price themselves. A straight-line distance would
// call two accounts either side of water "40 tiles apart" and send one on a journey longer than
// the mule trip it was replacing — the feature failing by doing exactly what it exists to
// prevent. A peer the planner cannot reach is dropped rather than guessed at.
//
// Returning false is not an error and is not logged as one: an unreachable peer is an ordinary
// outcome on a map with islands.
type CostFn func(from, to tile.Tile) (int, bool)

// InterruptPenalty is what a holder that says it is mid-task costs in the ranking, in the same
// run-tile units CostFn returns — the answer to design question 4's ranking half.
//
// It is a real cost, not a fudge: a holder standing at a bank can serve an order in the few
// seconds a withdraw and a trade screen take, while one mid-task has to break off, and the
// fleet's own phase timeline puts banking at 5.9% of account time against fishing's 68%, so
// the odds that a randomly chosen peer is mid-task are high and the difference is worth
// pricing. Sized at roughly ten seconds of running so it breaks ties and shifts close calls
// without ever overriding a genuinely much shorter trip.
const InterruptPenalty = 20

// Margin is how much better than the mule a peer trip must be before it is worth taking, in
// run-tile units — the answer to design question 4's go/no-go half.
//
// A peer trip that merely ties the mule trip is worse than it looks: the mule is a dedicated
// receiver whose whole job is trading, while a peer has to be interrupted, and the mule's bank
// is stocked deliberately where a peer's spare is incidental. So a peer has to WIN, not draw.
// Sized at roughly fifteen seconds of running.
const Margin = 30

// NoMuleCost is the MuleCost to pass when there is no mule to fall back on at all — any peer
// then beats the alternative of staying blocked.
const NoMuleCost = 1 << 30

// Options tunes one Choose call.
type Options struct {
	// MuleCost is what the fallback trip to the mule costs, from the same CostFn, so the two
	// are compared in one unit. Pass NoMuleCost when there is no mule.
	MuleCost int
	// MaxCost is the furthest this account will walk for a spare at all, regardless of the
	// mule. Zero means DefaultMaxCost.
	MaxCost int
	// Now is the instant the ranking is judged at; zero means the ages on each Peer are used
	// as given. Present so a test pins time rather than sleeping.
	Now time.Time
}

// DefaultMaxCost is the furthest an account will travel for a peer's spare.
//
// Anchored on the number this feature exists to beat: the live woodcutters walk 83 tiles from
// the Seers bank to the mule's Catherby post. A peer trip longer than about that is not saving
// anybody anything, and past it an account is better off chopping. Rounded to 100 run-tile
// units so a peer that is slightly further but has the item still wins over a mule that does
// not have it at all.
const DefaultMaxCost = 100

// Verdict is what Choose decided.
type Verdict string

const (
	// AskPeer: at least one peer holds a spare and genuinely beats the mule. Candidates is
	// ranked best-first.
	AskPeer Verdict = "ask-peer"
	// GoToMule: nobody near enough is worth interrupting, so use the mule as before. This is
	// a SUCCESS of the policy, not a failure — it is the answer that keeps the feature from
	// ever being slower than what it replaced.
	GoToMule Verdict = "go-to-mule"
	// NothingToDo: no peer has a spare and there is no mule either.
	NothingToDo Verdict = "nothing"
)

// Candidate is one ranked holder.
type Candidate struct {
	Peer Peer
	// Spare is how many it could hand over.
	Spare int
	// Cost is the planned route cost, and Score is Cost plus any interrupt penalty. Ranking
	// is on Score; Cost is kept so a log line can say what the trip actually costs.
	Cost  int
	Score int
}

// Choice is Choose's answer.
type Choice struct {
	Verdict Verdict
	// Candidates is every usable holder, best-first. A caller that loses a turn to contention
	// takes the next one rather than starting the whole decision again — see design question
	// 3. Empty unless Verdict is AskPeer.
	Candidates []Candidate
	// Reason is why, in words an operator can read. Always set.
	Reason string
}

// Best is the top candidate, if any.
func (c Choice) Best() (Candidate, bool) {
	if len(c.Candidates) == 0 {
		return Candidate{}, false
	}
	return c.Candidates[0], true
}

// Choose ranks the peers that can serve req and decides whether any of them beats the mule.
//
// Exclusions, each one for a stated reason rather than taste: the asker itself; a peer with no
// position fix (there is no "nearest" without one); a peer whose row is older than
// Reserve.MaxAge (a stale reading is a guess, and walking on a guess costs the trip); a peer
// whose turn is already taken (one trade at a time); a peer with no spare after Reserve; a peer
// the CostFn cannot route to; and a peer further than MaxCost.
//
// Ranking is by Score (route cost plus InterruptPenalty for a mid-task holder), then by more
// spare, then by name so the order is deterministic and two accounts deciding in the same
// instant agree on who is first — which matters, because they then contend for that turn in a
// predictable order instead of scattering.
func Choose(req Request, peers []Peer, r Reserve, cost CostFn, opt Options) Choice {
	if norm(req.Item) == "" {
		return Choice{Verdict: NothingToDo, Reason: "no item named"}
	}
	if !req.Located {
		// Without a fix of our own there is no nearest anything. Fall back rather than
		// guess: the mule's post is a fixed tile the caller can walk to by name.
		return Choice{Verdict: GoToMule, Reason: "this account has no position fix, so nearest cannot be judged"}
	}
	maxCost := opt.MaxCost
	if maxCost <= 0 {
		maxCost = DefaultMaxCost
	}
	muleCost := opt.MuleCost
	if muleCost <= 0 {
		muleCost = NoMuleCost
	}

	var out []Candidate
	skipped := 0
	for _, p := range peers {
		if norm(p.Account) == "" || norm(p.Account) == norm(req.Asker) {
			continue
		}
		if !p.Located || p.Busy || p.Age > r.maxAge() {
			skipped++
			continue
		}
		spare := Spare(p, req.Item, r)
		if spare < 1 {
			continue
		}
		if cost == nil {
			skipped++
			continue
		}
		c, ok := cost(req.At, p.At)
		if !ok || c < 0 {
			skipped++
			continue
		}
		if c > maxCost {
			skipped++
			continue
		}
		score := c
		if p.Working {
			score += InterruptPenalty
		}
		out = append(out, Candidate{Peer: p, Spare: spare, Cost: c, Score: score})
	}

	if len(out) == 0 {
		if muleCost >= NoMuleCost {
			return Choice{Verdict: NothingToDo, Reason: "no peer has a spare " + strings.ToLower(strings.TrimSpace(req.Item)) + " within reach, and there is no mule"}
		}
		return Choice{Verdict: GoToMule, Reason: "no peer has a spare " + strings.ToLower(strings.TrimSpace(req.Item)) + " within reach"}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score < out[j].Score
		}
		if out[i].Spare != out[j].Spare {
			return out[i].Spare > out[j].Spare
		}
		return out[i].Peer.Account < out[j].Peer.Account
	})

	// The go/no-go against the mule. Compared on Score, not raw Cost, so the interrupt a peer
	// costs is part of the comparison rather than something discovered afterwards.
	if best := out[0]; best.Score+Margin > muleCost {
		if muleCost >= NoMuleCost {
			// No mule at all: any reachable peer beats staying blocked, so the margin
			// rule does not apply. This is the branch that matters on a fleet whose mule
			// is out of stock, which is the case that started all of this.
			return Choice{Verdict: AskPeer, Candidates: out,
				Reason: "no mule available, so the nearest spare wins by default"}
		}
		return Choice{Verdict: GoToMule,
			Reason: "the nearest spare is " + itoa(best.Score) + " away against the mule's " + itoa(muleCost) + " — not worth interrupting anyone"}
	}
	return Choice{Verdict: AskPeer, Candidates: out,
		Reason: itoa(len(out)) + " peer(s) hold a spare; the nearest is " + out[0].Peer.Account + " at " + itoa(out[0].Cost) + " against the mule's " + itoa(muleCost)}
}

func itoa(v int) string {
	if v >= NoMuleCost {
		return "unreachable"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	if v == 0 {
		return "0"
	}
	var b [24]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// ---- 3. CONTENTION ----

// TurnTTL is how long a reserved turn lasts before it lapses on its own.
//
// It has to cover the asker's whole trip — walk there, ask, trade — or the turn expires under
// an account that is still walking and a second asker arrives on top of it. Sized against
// DefaultMaxCost (100 run-tile units, about 50 ticks of running, ~30s) plus a bank trip and a
// trade screen at the far end, with room for a random event on the way. The asker REFRESHES it
// as it walks (Directory.Reserve extends a turn the same asker already holds), so this is the
// abandonment timeout rather than the expected duration: it only ever fires for an asker that
// died, logged out or gave up without releasing.
const TurnTTL = 3 * time.Minute

// BusyRetry is how long an account that lost the turn waits before trying the whole decision
// again — and it GOES BACK TO WORK in the meantime rather than queueing.
//
// Not invented here. internal/bot/woodcutteraxe.go's axeBusyRetry is 90 seconds for exactly
// this situation at the Seers market stall, with the incident that produced it recorded in its
// own comment: an account lost the turn by seconds, served the full fifteen-minute give-up
// cooldown, and the fleet owner noticed it had not upgraded. Matched deliberately so an
// operator sees one number for "somebody else is being served", not two.
const BusyRetry = 90 * time.Second

// ---- 4. WHO INTERRUPTS WHOM, the holder's own half ----

// ServeVerdict is a holder's answer to an incoming peer request.
type ServeVerdict string

const (
	// Serve: hand it over.
	Serve ServeVerdict = "serve"
	// Refuse: this holder will not, and says why so the asker stops waiting and gets on
	// with its own recovery instead of standing still for a full timeout.
	Refuse ServeVerdict = "refuse"
)

// ServeDecision is DecideServe's answer.
type ServeDecision struct {
	Verdict ServeVerdict
	// Qty is how many to hand over — never more than the spare, and never more than asked.
	Qty int
	// Reason is short enough for a mulebus.DeclinedMessage (ReasonMax is 80).
	Reason string
}

// DecideServe is the holder's side: given what it holds and what is being asked, hand over the
// spare or refuse with a reason.
//
// Deliberately NOT a second copy of orderplan.Decide, and the difference is the whole reason
// this function exists. orderplan governs a MULE's bank — a deliberately stocked store with a
// catalogue, per-donor caps and ledger windows, whose job is giving things away. This governs
// an ORDINARY WORKING ACCOUNT's bank, where the only question that matters is "can I lose this
// without stopping work?", and the answer is Spare. A holder has no catalogue because it is not
// a shop, and needs no per-donor window because it cannot be drained: Spare recomputes from
// what is actually left every single time, so a holder with one spare axe has none after
// giving it away, by construction rather than by bookkeeping.
//
// Two traps inherited from orderplan are worth naming because this deliberately does NOT
// repeat them. orderplan.Prescreen CLAMPS an over-large order down rather than declining it,
// and orderplan.QtyMax is a PARSER bound (1,000,000) separate from the catalogue caps. Here
// there is one number, Spare, it is computed from live holdings rather than configuration, and
// an ask larger than it is clamped to it and SAID SO in the reason — so an asker that wanted
// three and got one knows that is all there was, rather than inferring a refusal.
func DecideServe(p Peer, asker, item string, qty int, r Reserve) ServeDecision {
	if norm(asker) == "" {
		return ServeDecision{Verdict: Refuse, Reason: "no asker named"}
	}
	if norm(asker) == norm(p.Account) {
		return ServeDecision{Verdict: Refuse, Reason: "that is my own name"}
	}
	if qty < 1 {
		qty = 1
	}
	spare := Spare(p, item, r)
	if spare < 1 {
		// The negative control, and the single most important refusal in the package: a
		// holder with only one of something keeps it. Named precisely so an operator can
		// tell it apart from "I have none at all".
		if count(p.Bank, item)+count(p.Held, item) > 0 {
			return ServeDecision{Verdict: Refuse, Reason: "I only have the one I am using"}
		}
		return ServeDecision{Verdict: Refuse, Reason: "I have none banked"}
	}
	give := qty
	if give > spare {
		give = spare
	}
	reason := "handing over " + itoa(give)
	if give < qty {
		reason = "only " + itoa(give) + " to spare of the " + itoa(qty) + " asked"
	}
	return ServeDecision{Verdict: Serve, Qty: give, Reason: reason}
}

// DescribeChoice is the status line an asker logs.
func DescribeChoice(c Choice) string {
	switch c.Verdict {
	case AskPeer:
		if best, ok := c.Best(); ok {
			return "asking " + best.Peer.Account + " for a spare (" + itoa(best.Cost) + " away, " + itoa(best.Spare) + " spare)"
		}
	case GoToMule:
		return "walking to the mule: " + c.Reason
	}
	return c.Reason
}
