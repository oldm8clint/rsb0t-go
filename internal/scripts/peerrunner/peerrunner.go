// Package peerrunner is the live I/O half of fleet-wide peer item sharing: publishing this
// account's own bank into the shared directory, choosing and walking to the nearest peer that
// holds a spare, and — on the other side — serving a peer's request out of this account's own
// bank. internal/scripts/peerplan holds every decision; this file only does.
//
// HOW LITTLE OF THIS IS NEW, which is the main thing to understand before reading it. A peer
// request needs no new protocol, no new trade code and no new receipt rule, because all three
// already exist and none of them was ever actually mule-specific:
//
//   - The ASK is mulebus.OrderMessage addressed to the peer instead of to a mule, which is
//     literally what donorrunner.Await already publishes. Ask below therefore calls
//     donorrunner.Await with the chosen peer's name in the Mule field, and inherits its whole
//     bounded ask-and-receive loop — including the one rule that matters most, that only this
//     account's OWN PACK RISING counts as proof, never a trade closing.
//   - The SERVE is mulerunner.Handout, which withdraws from a bank and trades it to a named
//     partner behind an orderplan.Policy. Its own doc comment already scopes it to a partner
//     ALREADY VISIBLE and says it will not travel — which is exactly right here, because the
//     asker is the one that walks (peerplan design question 4).
//
// So what this package genuinely adds is: publish a bank reading, pick a counterparty, walk to
// it, take a turn, and build the one-shot policy that lets an ordinary working account serve a
// single order without becoming a shop. Everything else is delegated.
//
// WHY THE ASKER WALKS AND NOT THE HOLDER, restated here because it is the load-bearing
// asymmetry: the asker is blocked and earning nothing, the holder is working and earning. The
// fleet's own phase timeline puts fishing at 68% of account time, so interrupting a holder is
// a real cost paid against a real output, while the asker's time is already lost. Moving the
// account with nothing better to do is both cheaper and — conveniently — the direction
// mulerunner.Handout was already built for.
package peerrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/mulerunner"
	"rsb0t-go/internal/scripts/orderplan"
	"rsb0t-go/internal/scripts/peerplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/live"
)

// Delayer is the caller's own interruptible sleep, returning false to stop — the same shape
// every sibling runner package uses.
type Delayer func(time.Duration) bool

// Deps is the live context one account brings.
type Deps struct {
	Session    actions.Session
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	Log        func(string)

	// Me is this account's own name; Bus is the shared wall bus; Dir is the shared
	// directory. All three are required — with any of them missing this package does
	// nothing at all rather than half of something, and says so.
	Me  string
	Bus *mulebus.Bus
	Dir *peerbank.Directory
}

func (d *Deps) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

func (d *Deps) delay(t time.Duration) bool {
	if d.Delay == nil {
		time.Sleep(t)
		return true
	}
	return d.Delay(t)
}

// PlayerPack is the persistent backpack component id — the same constant every sibling runner
// package declares, per this project's convention of small per-package duplication. See
// donorrunner.PlayerPack's own doc comment for why a fixed component beats resolving the pack
// through the sidebar tab (content takes that tab away during ship crossings and several
// random events, and a blind read of 0 would corrupt both a publish and a receipt).
const PlayerPack = 3214

// ---- publishing this account's own row ----

// NameOf resolves an object id to its canonical name, empty when the config cannot.
func NameOf(cfg *cachecfg.Config, id int32) string {
	if cfg == nil {
		return ""
	}
	obj, ok := cfg.Obj(int(id))
	if !ok {
		return ""
	}
	return obj.Name
}

// Holdings folds slot items into named counts, merging duplicate rows and dropping anything
// the config cannot name.
//
// Bank NOTES are deliberately folded in with the ordinary item, because a noted stack in the
// bank is the same thing as the item for the purpose of "do you have a spare" — a bank
// withdraws it un-noted. (mulerunner.nonNotedID is what makes sure the ITEM and not the note
// is what actually crosses the trade screen; that is a separate concern and it already
// handles it.)
func Holdings(cfg *cachecfg.Config, items []gamestate.SlotItem) []peerbank.Holding {
	by := map[string]int{}
	order := []string{}
	for _, it := range items {
		name := NameOf(cfg, it.ID)
		if strings.TrimSpace(name) == "" {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		key := strings.TrimSpace(name)
		if _, seen := by[key]; !seen {
			order = append(order, key)
		}
		by[key] += c
	}
	out := make([]peerbank.Holding, 0, len(order))
	for _, name := range order {
		out = append(out, peerbank.Holding{Item: name, Count: by[name]})
	}
	return append(out, qualifiedHoldings(cfg, items)...)
}

// IDKeyedNames are display names several objs share that the fleet must still tell apart in a
// published bank: every grimy herb in content 289 is named "Herb" (skill_herblore/configs/
// identifying/unids.obj), and MasterCrafter's herb round decides on and asks for grimy guams alone
// (a user request); and a strung bow and its unstrung form carry ONE name ("Yew shortbow" is
// obj 857 strung and obj 68 unstrung), so a count by name read the mule's 130,712 strung Yew
// shortbows as unstrung ones and a kit ask by name was handed whichever the bank held first (live
// 6 Oct 2026: bowids-1006). Items with one of these names are published TWICE: once merged under
// the name, as before (every existing reader is unchanged), and once per obj under its qualified
// name ("Herb id199", cachecfg.QualifiedName), which cachecfg.ObjIDs, and so the mule's hand-out,
// resolves to that obj alone. A reader that sums a whole holding list must skip one of the two
// (QualifiedBase says which rows are the extra ones; PlainRows and ObjRows drop each kind).
//
// The last two are the owner's alch stock names that a second obj shares (Ruby ring 1641 and a
// one-coin obj 1651; Black dagger 1217 and the members' obj 746, also one coin), so an ask for the
// plain name could be handed the worthless one; a test over the real item config (internal/bot) pins
// that every bow and every alch stock name that a second obj shares is on this list.
var IDKeyedNames = idKeyedNames()

func idKeyedNames() []string {
	names := []string{"Herb"}
	names = append(names, bowstringplan.DefaultBowNames()...)
	return append(names, "Ruby ring", "Black dagger")
}

// IsIDKeyed reports whether name (any case) is one of IDKeyedNames.
func IsIDKeyed(name string) bool {
	name = strings.TrimSpace(name)
	for _, k := range IDKeyedNames {
		if strings.EqualFold(name, k) {
			return true
		}
	}
	return false
}

// QualifiedBase reports whether a holding's item is a qualified per-obj row (and its base name).
func QualifiedBase(item string) (string, bool) {
	base, _, ok := cachecfg.SplitQualified(item)
	return base, ok
}

// PlainRows is a published list as every reader that predates the per-obj rows saw it: the rows
// merged by name, without the extra qualified ones. A reader that counts rows or sums a list reads
// this one, or it counts a bow in the bank twice.
func PlainRows(hs []peerbank.Holding) []peerbank.Holding {
	out := make([]peerbank.Holding, 0, len(hs))
	for _, h := range hs {
		if _, qualified := QualifiedBase(h.Item); !qualified {
			out = append(out, h)
		}
	}
	return out
}

// ObjRows is a published list with a row per OBJ: the qualified rows of IDKeyedNames and every row
// of any other name, but not the merged row of a keyed name that has qualified rows (it is those
// rows added up, so a reader that sums would count it twice). A keyed name with no qualified row
// keeps its merged one: a list published before the per-obj rows, or by a build that knew no config.
func ObjRows(hs []peerbank.Holding) []peerbank.Holding {
	split := map[string]bool{}
	for _, h := range hs {
		if base, qualified := QualifiedBase(h.Item); qualified {
			split[strings.ToLower(strings.TrimSpace(base))] = true
		}
	}
	out := make([]peerbank.Holding, 0, len(hs))
	for _, h := range hs {
		if _, qualified := QualifiedBase(h.Item); !qualified && IsIDKeyed(h.Item) && split[strings.ToLower(strings.TrimSpace(h.Item))] {
			continue
		}
		out = append(out, h)
	}
	return out
}

func qualifiedHoldings(cfg *cachecfg.Config, items []gamestate.SlotItem) []peerbank.Holding {
	if cfg == nil {
		return nil
	}
	by := map[int]int{}
	var order []int
	for _, it := range items {
		o, ok := cfg.Obj(int(it.ID))
		if !ok || !IsIDKeyed(o.Name) {
			continue
		}
		id := int(it.ID)
		if o.Noted() && o.CertLink >= 0 {
			id = o.CertLink
		}
		c := max(int(it.Count), 1)
		if _, seen := by[id]; !seen {
			order = append(order, id)
		}
		by[id] += c
	}
	out := make([]peerbank.Holding, 0, len(order))
	for _, id := range order {
		name := NameOf(cfg, int32(id))
		out = append(out, peerbank.Holding{Item: cachecfg.QualifiedName(name, id), Count: by[id]})
	}
	return out
}

// CurrentTile is this account's position, and whether it has one at all.
func CurrentTile(d *Deps) (tile.Tile, bool) {
	if d.Session == nil {
		return tile.Tile{}, false
	}
	x, z, level, ok := d.Session.State().Tile()
	if !ok {
		return tile.Tile{}, false
	}
	return tile.Tile{X: x, Z: z, Level: level}, true
}

// PublishSelf records this account's row in the shared directory.
//
// bankItems is what the caller ALREADY has in hand from a bank it already opened — this
// package deliberately does not open one. That is the whole cost argument in peerbank's own
// package doc: a directory fed at the moment a banking script is standing at its bank anyway
// costs nothing, where a pull would cost a fleet snapshot and the gamestate locks /api/status
// needs. Call it once per bank trip, right after reading the bank.
//
// working is the caller's own statement that it is mid-task rather than standing at a bank; it
// only ever costs this account a ranking penalty when somebody is choosing whom to interrupt.
func PublishSelf(d *Deps, bankItems []gamestate.SlotItem, working bool) {
	if d.Dir == nil || strings.TrimSpace(d.Me) == "" {
		return
	}
	at, located := CurrentTile(d)
	var held []gamestate.SlotItem
	if d.Session != nil {
		if inv := d.Session.State().Inventory(PlayerPack); inv != nil {
			held = inv.NonEmpty()
		}
	}
	d.Dir.Publish(d.Me, at, located, Holdings(d.Config, bankItems), Holdings(d.Config, held), working)
}

// ---- real travel cost ----

// RouteCost builds the CostFn peerplan needs from a real internal/webwalk planner, so
// "nearest" is a planned route through this map's doors, stairs and ship crossings rather than
// a straight line. Returns nil when there is no collision pack, which makes peerplan.Choose
// fall back to the mule rather than guess — the correct failure.
//
// The planner is the process-wide shared one (webwalk/live.PlannerFor caches per pack), so 269
// accounts asking this question do not build 269 copies of the transport graph.
func RouteCost(pack *pathfind.Pack) peerplan.CostFn {
	if pack == nil {
		return nil
	}
	planner, err := live.PlannerFor(pack)
	if err != nil || planner == nil {
		return nil
	}
	return func(from, to tile.Tile) (int, bool) {
		r := planner.Plan(
			webwalk.Point{X: from.X, Z: from.Z, Level: from.Level},
			webwalk.Point{X: to.X, Z: to.Z, Level: to.Level},
			webwalk.Options{},
		)
		if !r.OK {
			return 0, false
		}
		return r.Cost, true
	}
}

// ---- the asker ----

// AskOptions is one attempt to source an item from a peer.
type AskOptions struct {
	// Item is the canonical item name wanted, Qty how many would help.
	Item string
	Qty  int
	// Reserve is the spare rule. Zero-valued uses peerplan.DefaultReserve.
	Reserve peerplan.Reserve
	// Cost prices a trip. Nil uses RouteCost(d.Pack); if that is also nil, Choose falls back
	// to the mule.
	Cost peerplan.CostFn
	// MuleStand and MuleName describe the fallback so the peer trip can be compared against
	// it in the same unit. Leave MuleName empty when there is no mule, and the comparison
	// becomes "any reachable peer beats staying blocked".
	MuleName  string
	MuleStand tile.Tile
	// MaxCost overrides peerplan.DefaultMaxCost.
	MaxCost int
	// Travel walks this account to within radius of dest, returning false if it could not.
	// Required — without it the asker cannot get to a peer, and Ask says so rather than
	// asking a peer it is nowhere near. Wire it to woodcutrunner.TravelTo /
	// freshstartrunner.WalkNear, whichever the calling script already uses.
	Travel func(dest tile.Tile, radius int, timeout time.Duration) bool
	// Timeout bounds the whole attempt.
	Timeout time.Duration
}

// MeetRadius is how close the asker stands to the holder before asking.
//
// mulerunner.Handout resolves the partner's player slot before it draws anything and declines
// outright when it cannot see them ("you are not in sight of me" — the live failure four
// woodcutters hit on 2026-09-21 asking the mule from the Seers bank while it posted at
// Catherby). So arriving is part of the ask, not a nicety. Matched to the radius the mule
// hand-off already uses.
const MeetRadius = 6

// DefaultAskTimeout bounds one whole attempt, walk included. Longer than
// donorrunner.DefaultAskTimeout because that budget starts with both parties already standing
// together, where this one has to cover the trip as well.
const DefaultAskTimeout = 4 * time.Minute

// AskResult is what came of it.
type AskResult struct {
	// Got is true only once this account's own pack is confirmed to hold the item. The
	// receipt is donorrunner.Await's, unchanged: a trade closing proves nothing.
	Got bool
	// From is the peer that actually handed it over, set on Got.
	From string
	// Qty is how many arrived, as the confirm screen showed.
	Qty int
	// UseMule is the policy's answer that no peer is worth it — the caller should fall back
	// to its existing mule path. This is a SUCCESS of the decision, not a failure of it.
	UseMule bool
	// Busy means every candidate's turn was taken. The caller goes back to work and retries
	// after peerplan.BusyRetry; it must NOT queue.
	Busy bool
	// Tried is how many peers were actually attempted, so a caller's log can tell "nobody
	// had one" from "I tried three and none worked".
	Tried  int
	Reason string
	// Stopped is true when the caller's own Delay asked to stop.
	Stopped bool
}

// Ask sources o.Item from the nearest peer holding a spare, walking there and trading.
//
// The shape is deliberately "try the ranked list, in order": peerplan.Choose returns every
// usable holder best-first precisely so that losing a turn to contention costs one Reserve
// call rather than the whole decision. A holder whose turn is taken is skipped immediately;
// only if EVERY candidate is taken does this report Busy.
//
// Every exit is bounded and every one of them leaves the caller able to do what it would have
// done anyway. Asking must never make a blocked account more stuck than it already was.
func Ask(d *Deps, o AskOptions) AskResult {
	if d.Dir == nil || d.Bus == nil || strings.TrimSpace(d.Me) == "" {
		return AskResult{UseMule: true, Reason: "peer sharing is not configured (no directory, bus or name)"}
	}
	if strings.TrimSpace(o.Item) == "" {
		return AskResult{Reason: "nothing asked for"}
	}
	if o.Travel == nil {
		return AskResult{UseMule: true, Reason: "no way to travel to a peer"}
	}
	qty := o.Qty
	if qty < 1 {
		qty = 1
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = DefaultAskTimeout
	}
	reserve := o.Reserve
	if len(reserve.Families) == 0 && len(reserve.Keep) == 0 {
		reserve = peerplan.DefaultReserve()
	}
	cost := o.Cost
	if cost == nil {
		cost = RouteCost(d.Pack)
	}

	at, located := CurrentTile(d)
	req := peerplan.Request{Asker: d.Me, At: at, Located: located, Item: o.Item, Qty: qty}

	now := time.Now()
	peers := PeersFrom(d.Dir, now)

	// The mule's own trip, priced with the SAME cost function so the two are comparable.
	muleCost := peerplan.NoMuleCost
	if strings.TrimSpace(o.MuleName) != "" && cost != nil && located {
		if c, ok := cost(at, o.MuleStand); ok {
			muleCost = c
		}
	}

	choice := peerplan.Choose(req, peers, reserve, cost, peerplan.Options{
		MuleCost: muleCost, MaxCost: o.MaxCost, Now: now,
	})
	switch choice.Verdict {
	case peerplan.GoToMule:
		return AskResult{UseMule: true, Reason: choice.Reason}
	case peerplan.NothingToDo:
		return AskResult{Reason: choice.Reason}
	}
	d.logf("peer: %s", peerplan.DescribeChoice(choice))

	deadline := time.Now().Add(timeout)
	tried, allBusy := 0, true
	for _, cand := range choice.Candidates {
		if time.Now().After(deadline) {
			break
		}
		holder := cand.Peer.Account

		// Take the turn BEFORE walking. Reserving on arrival would mean the walk is wasted
		// whenever somebody else got there first, and with a cohort at one bank that is the
		// common case rather than the rare one.
		if !d.Dir.Reserve(holder, d.Me, time.Now(), peerplan.TurnTTL) {
			who, _ := d.Dir.Busy(holder, time.Now())
			d.logf("peer: %q is already serving %q — trying the next holder", holder, who)
			continue
		}
		allBusy = false
		tried++

		res := d.askOne(o, cand, qty, deadline)
		d.Dir.Release(holder, d.Me)
		if res.Stopped || res.Got {
			res.Tried = tried
			return res
		}
		d.logf("peer: %q did not hand over a %s (%s)", holder, strings.ToLower(o.Item), res.Reason)
	}

	if allBusy && len(choice.Candidates) > 0 {
		// Every holder is serving somebody else. GO BACK TO WORK and retry — never queue.
		// This is the market-stall lesson: losing a turn is not a refusal.
		return AskResult{Busy: true, Tried: tried,
			Reason: fmt.Sprintf("all %d peer(s) holding a spare are already serving somebody", len(choice.Candidates))}
	}
	return AskResult{UseMule: strings.TrimSpace(o.MuleName) != "", Tried: tried,
		Reason: fmt.Sprintf("tried %d peer(s), none handed one over", tried)}
}

// askOne walks to one holder and runs the ask. The caller owns the turn for the whole call.
func (d *Deps) askOne(o AskOptions, cand peerplan.Candidate, qty int, deadline time.Time) AskResult {
	holder := cand.Peer.Account

	// Walk. The turn is refreshed on arrival so a long walk cannot let it lapse underneath
	// us — Directory.Reserve extends a turn the same asker already holds.
	if here, ok := CurrentTile(d); !ok || tile.Chebyshev(here, cand.Peer.At) > MeetRadius {
		budget := time.Until(deadline)
		if budget <= 0 {
			return AskResult{Reason: "out of time before setting off"}
		}
		if !o.Travel(cand.Peer.At, MeetRadius, budget) {
			return AskResult{Reason: "could not reach " + holder}
		}
	}
	d.Dir.Reserve(holder, d.Me, time.Now(), peerplan.TurnTTL)

	// The holder may have moved on, banked the spare, or died since it published. Nothing
	// here treats the directory row as proof — the ask itself is what settles it, and the
	// holder re-checks its own Spare against live holdings before giving anything.
	left := time.Until(deadline)
	if left <= 0 {
		return AskResult{Reason: "out of time on arrival"}
	}
	if left > donorrunner.DefaultAskTimeout {
		left = donorrunner.DefaultAskTimeout
	}

	res := donorrunner.Await(&donorrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Log: d.Log,
		Delay: func(t time.Duration) bool { return d.delay(t) },
	}, donorrunner.AwaitOptions{
		// The counterparty is an ordinary account. donorrunner's field is named Mule
		// because that is all it could once be; the code it drives never cared.
		Mule: holder, Me: d.Me, Item: o.Item, Qty: qty, Bus: d.Bus, Timeout: left,
		// donorrunner.Refusals is for an account asking BLIND. This one is not: the holder
		// was chosen because it published this item into the peerbank directory, the
		// spare check ran against that reading, and the account has already walked to it.
		// A half-hour-old refusal must not veto a fresh reading plus a completed trip.
		AskAnyway: true,
	})
	switch {
	case res.Got:
		d.logf("peer: %q handed over a %s (saved the trip to %q)", holder, strings.ToLower(o.Item), o.MuleName)
		return AskResult{Got: true, From: holder, Qty: qty}
	case res.Stopped:
		return AskResult{Stopped: true, Reason: res.Reason}
	}
	return AskResult{Reason: res.Reason}
}

// PeersFrom converts the directory's rows into the peerplan view, ageing each one.
func PeersFrom(dir *peerbank.Directory, now time.Time) []peerplan.Peer {
	if dir == nil {
		return nil
	}
	rows := dir.Entries(now)
	out := make([]peerplan.Peer, 0, len(rows))
	for _, e := range rows {
		out = append(out, peerplan.Peer{
			Account: e.Account,
			At:      e.At,
			Located: e.Located,
			Bank:    holdingsOf(e.Bank),
			Held:    holdingsOf(e.Held),
			// Working covers both "said it was mid-task" and "its turn is taken": the
			// directory folds a live reservation into Working (see Entries), and a
			// reserved holder must be EXCLUDED rather than merely ranked down, so it is
			// reported as Busy too.
			Busy:    isReserved(dir, e.Account, now),
			Working: e.Working,
			Age:     now.Sub(e.UpdatedAt),
		})
	}
	return out
}

func isReserved(dir *peerbank.Directory, account string, now time.Time) bool {
	_, busy := dir.Busy(account, now)
	return busy
}

func holdingsOf(in []peerbank.Holding) []peerplan.Holding {
	out := make([]peerplan.Holding, 0, len(in))
	for _, h := range in {
		out = append(out, peerplan.Holding{Item: h.Item, Count: h.Count})
	}
	return out
}

// ---- the holder ----

// ServeOptions is how this account answers peers.
type ServeOptions struct {
	// Reserve is the spare rule this account applies to itself. Zero-valued uses
	// peerplan.DefaultReserve.
	Reserve peerplan.Reserve
	// Allowed is the fleet's own account names. The bus carries NO AUTHENTICATION of any
	// kind — orderplan.Policy.Allowed is "the only thing standing between a message and the
	// bank", in its own words — so this is not optional and an empty list serves nobody.
	Allowed []string
	// Bank reads this account's own bank contents for the spare check, opening one if it
	// has to. Nil falls back to the last row this account published about itself, which is
	// weaker but still bounded by Spare.
	Bank func() []gamestate.SlotItem
	// Watcher, when set, is told about a completed hand-over so the asker's own in-flight
	// republishes (donorrunner.AskRepeatEvery) are not served a second time. Strongly
	// recommended: without it a holder can hand over twice. See RepeatSuppress.
	Watcher *Watcher
}

// ServeResult is what the holder did.
type ServeResult struct {
	Served  bool
	To      string
	Item    string
	Qty     int
	Reason  string
	Stopped bool
}

// Serve answers one peer's KindOrder out of this account's own bank.
//
// The decision is peerplan.DecideServe (is this a genuine spare?) and the execution is
// mulerunner.Handout (withdraw it and trade it). The policy handed to Handout is built FOR
// THIS ONE ORDER and for nothing else — see onePolicy — which is what keeps an ordinary
// working account from turning into a second mule.
//
// A refusal is always published back. That is not politeness: the asker is standing there on a
// bounded timer, and an explicit "no" gets it back to its own recovery immediately instead of
// burning the whole budget in silence. Every refusal carries its own specific reason rather
// than one shared message — the rule docs/trustworthy_harnesses.md draws out of the mule trade
// failures, where a single shared decline message hid five different causes.
func Serve(d *Deps, o ServeOptions, msg mulebus.Message) ServeResult {
	if d.Bus == nil || strings.TrimSpace(d.Me) == "" {
		return ServeResult{Reason: "peer sharing is not configured"}
	}
	if msg.Kind != mulebus.KindOrder || !mulebus.NamesMatch(msg.To, d.Me) {
		// Not addressed to this account specifically. A broadcast order is a MULE's
		// business, not an ordinary account's: answering one would have every holder in
		// the fleet stampede at a single asker, which is the bottleneck inverted.
		return ServeResult{Reason: "not addressed to me"}
	}
	from := strings.TrimSpace(msg.From)
	if !allowed(o.Allowed, from) {
		d.logf("peer: ignoring an order from %q — not on the allow list", from)
		return ServeResult{Reason: "not on the allow list"}
	}

	order, err := orderplan.Parse(msg.Text)
	if err != nil {
		d.publishDecline(from, "I could not read that order")
		return ServeResult{Reason: "unparseable order: " + err.Error()}
	}

	reserve := o.Reserve
	if len(reserve.Families) == 0 && len(reserve.Keep) == 0 {
		reserve = peerplan.DefaultReserve()
	}

	// Judged against a LIVE reading, never against the row this account published earlier:
	// the spare may already be gone, and the directory is explicitly not proof of anything.
	var bankItems []gamestate.SlotItem
	if o.Bank != nil {
		bankItems = o.Bank()
	}
	var held []gamestate.SlotItem
	if d.Session != nil {
		if inv := d.Session.State().Inventory(PlayerPack); inv != nil {
			held = inv.NonEmpty()
		}
	}
	me := peerplan.Peer{
		Account: d.Me,
		Bank:    holdingsOf(Holdings(d.Config, bankItems)),
		Held:    holdingsOf(Holdings(d.Config, held)),
	}

	decision := peerplan.DecideServe(me, from, order.Item, order.Qty, reserve)
	if decision.Verdict != peerplan.Serve {
		d.logf("peer: declining %q's order for %s: %s", from, strings.ToLower(order.Item), decision.Reason)
		d.publishDecline(from, decision.Reason)
		return ServeResult{Reason: decision.Reason}
	}

	d.logf("peer: handing %d %s to %q out of my own spare", decision.Qty, strings.ToLower(order.Item), from)
	res := mulerunner.Handout(&mulerunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Log: d.Log,
		Delay: func(t time.Duration) bool { return d.delay(t) },
	}, mulerunner.HandoutRequest{
		To:     from,
		Text:   orderplan.Format(order.Item, decision.Qty),
		Policy: onePolicy(from, order.Item, decision.Qty),
		Ledger: orderplan.NewLedger(orderplan.DefaultWindow),
	})
	switch {
	case res.Stopped:
		return ServeResult{Stopped: true}
	case res.Delivered:
		// Recorded BEFORE the bus publish, so an asker's republish landing in the same
		// instant is already suppressed. Only a confirmed delivery counts: suppressing on
		// an attempt would let one mechanical failure lock an asker out of an item it
		// never actually received — the same rule internal/bot/mule.go applies to its own
		// ledger.
		if o.Watcher != nil {
			o.Watcher.Delivered(from, time.Now())
		}
		d.Bus.Publish(mulebus.TradedMessage(d.Me, from))
		d.logf("peer: gave %d %s to %q", res.Crossed, res.Item, from)
		return ServeResult{Served: true, To: from, Item: res.Item, Qty: res.Crossed}
	}
	d.publishDecline(from, res.Reason)
	return ServeResult{Reason: res.Reason, To: from}
}

func (d *Deps) publishDecline(to, reason string) {
	if d.Bus == nil {
		return
	}
	d.Bus.Publish(mulebus.DeclinedMessage(d.Me, to, reason))
}

// onePolicy is an orderplan.Policy authorising EXACTLY this one order and nothing else: one
// donor, one item, one quantity, one order per window.
//
// Built fresh per request rather than configured once, deliberately, and this is the line
// between a peer and a mule. A mule's policy is a standing catalogue with ledger windows
// because a mule is a store that exists to give things away and therefore has to be defended
// against being drained. An ordinary account is not a store: the only quantity it will ever
// part with is peerplan.Spare, recomputed from what is actually left every single time, so it
// cannot be drained by construction — a holder with one spare axe has none the moment it gives
// it away, with no bookkeeping required.
//
// Two orderplan traps are sidestepped rather than inherited by doing it this way. Prescreen
// CLAMPS an over-large order instead of declining it, and QtyMax is a PARSER bound (1,000,000)
// separate from the catalogue caps — so a policy whose cap was configuration rather than live
// stock could quietly authorise far more than a holder can afford. Here the cap IS the spare.
func onePolicy(donor, item string, qty int) orderplan.Policy {
	if qty < 1 {
		qty = 1
	}
	return orderplan.Policy{
		Allowed:           []string{donor},
		Catalog:           []orderplan.Handout{{Item: item, Max: qty}},
		MaxOrdersPerDonor: 1,
		MaxUnitsPerItem:   qty,
	}
}

func allowed(list []string, name string) bool {
	for _, n := range list {
		if mulebus.NamesMatch(n, name) {
			return true
		}
	}
	return false
}

// ---- convenience for a bot's Loop ----

// RepeatSuppress is how long a holder ignores further orders from an account it has just
// delivered to.
//
// THIS IS NOT BELT AND BRACES, it closes a real double-hand-out. donorrunner.Await
// deliberately re-publishes its order every AskRepeatEvery (10s) while it waits, so that a
// counterparty which starts up mid-wait still hears it. A holder that treats each republish as
// a fresh order therefore hands over a SECOND item to an account that already has one — and
// unlike a mule, whose orderplan.Ledger spans a 30-minute window across calls, a peer builds a
// one-shot policy per request (see onePolicy) and so has no ledger to catch it.
//
// Two live observations sized it. First, this is not hypothetical: a single published order
// was seen arriving TWICE in one peersharetest run, because mulebus.Since compares with
// `!at.Before(checkpoint)` — an inclusive boundary — and Windows' coarse clock resolution puts
// a publish and the following Poll's checkpoint on the same tick. Second, the same duplication
// is exactly what donorrunner's own doc comment records happening to the mule with Coins on
// 2026-09-19: "the donor never saw its coins arrive, kept re-publishing the order every
// AskRepeatEvery, and the mule filled it twice."
//
// Sized just above AskRepeatEvery so at least one republish is always covered, and well under
// the minutes an account would take to genuinely need a second of anything.
const RepeatSuppress = 25 * time.Second

// Watcher drains peer orders addressed to this account. One per bot, created once.
//
// It is NOT a bare mulebus.Watcher: it collapses repeats, which a holder needs and a mule gets
// for free from its ledger. See RepeatSuppress.
type Watcher struct {
	bus *mulebus.Watcher
	// delivered is when this account last actually handed something to each asker.
	delivered map[string]time.Time
}

// NewWatcher starts watching from now, ignoring whatever was published before the script
// started — matching mulebus.NewWatcher's own rule.
func NewWatcher() *Watcher {
	return &Watcher{bus: mulebus.NewWatcher(), delivered: map[string]time.Time{}}
}

// Delivered records a completed hand-over, so the asker's own in-flight republishes are not
// mistaken for a second order. Serve calls it; a caller driving Serve itself need not.
func (w *Watcher) Delivered(to string, at time.Time) {
	if w.delivered == nil {
		w.delivered = map[string]time.Time{}
	}
	w.delivered[strings.ToLower(strings.TrimSpace(to))] = at
}

// Poll returns every peer order addressed to this account since the last call, at most ONE per
// asker and none from an asker just served. A caller then serves at most one per Loop pass: a
// hand-out costs a bank trip and a trade, and the holder's actual job is its own script.
func (w *Watcher) Poll(d *Deps) []mulebus.Message {
	return w.pollAt(d, time.Now())
}

// pollAt is Poll with the clock injected, so the suppression window is testable without
// sleeping.
func (w *Watcher) pollAt(d *Deps, now time.Time) []mulebus.Message {
	if d.Bus == nil || strings.TrimSpace(d.Me) == "" {
		return nil
	}
	var out []mulebus.Message
	seen := map[string]int{}
	for _, msg := range w.bus.Poll(d.Bus) {
		if msg.Kind != mulebus.KindOrder {
			continue
		}
		// Addressed to THIS account by name. An unaddressed order belongs to a mule.
		if !mulebus.NamesMatch(msg.To, d.Me) {
			continue
		}
		from := strings.ToLower(strings.TrimSpace(msg.From))
		if at, ok := w.delivered[from]; ok && now.Sub(at) < RepeatSuppress {
			continue
		}
		// One order per asker per batch, newest wins: an asker republishing while it walks
		// is refreshing one request, not stacking up a queue of them. Same rule
		// internal/bot/mule.go's own b.orders map applies, for the same reason.
		if i, dup := seen[from]; dup {
			out[i] = msg
			continue
		}
		seen[from] = len(out)
		out = append(out, msg)
	}
	return out
}
