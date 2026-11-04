package mulerunner

// cook.go: the mule's idle-time cooking of the fleet's accumulated raw fish at the real
// Catherby range next to its own bank stand — account owner's own ask (2026-09-16): idle
// time between trades shouldn't be wasted standing still when the mule's own bank already
// holds a backlog of donated raw fish. Wired from internal/bot/mule.go's Loop, strictly
// inside the !res.Handled branch of RunCycle's own result — see this package's own doc
// comment and RunCycle's own for why that alone already guarantees "muling always comes
// first": Loop is called fresh every tick and always runs RunCycle (which checks for and
// fully handles a trade) before ever reaching this code, so one bounded cook-and-bank
// round here (CookRound performs exactly one: withdraw a full pack of the current lowest
// tier, cook it out, bank the result, then return) never delays a trade beyond the very
// next Loop call. Gated behind muleBot's own cookIdle setting (default off) — see mule.go.
//
// Ground-truthed against the real local test engine, not guessed:
//   - The real Range loc (id 2728) sits at 2817,3444,0 — mulereceiverplan.CatherbyRange —
//     found via `go run ./cmd/locbake` + `go run ./cmd/findloc` centered on Catherby's own
//     bank stand against this content build's own real packed maps and config archive.
//     Every Range variant's own Ops came back blank (no right-click "Cook" option at all);
//     content/scripts/skill_cooking/scripts/cooking.rs2 confirms why:
//     `[oplocu,_cooking_oven] @attempt_cook_item(last_useitem)` — a range is used purely
//     via OPLOCU, identical in kind to using flax on a spinning wheel
//     (crafterrunner.SpinFlaxUntil) or a vial on a fountain (vialrunner.FillOnce/FillLeg) —
//     this function follows their own established "OpLocU repeatedly until the pack no
//     longer holds the input item" shape, not a new pattern.
//   - A real closed door sits directly between Catherby's bank and this exact Range,
//     confirmed via the same findloc sweep: a "Door" loc (real id, "Open" op) at
//     2816,3438,0 — squarely on the direct path east from the bank stand toward the
//     range. A plain FindPath+Walk (this package's own unexported walkNear, used
//     elsewhere in this file for the bank booth's own immediate neighbourhood) cannot
//     open it: internal/pathfind's static collision pack bakes every door permanently
//     shut, the exact class of stall already found and fixed live in several other
//     scripts tonight (see freshstartrunner.WalkNear's own doc comment on
//     tryOpenDoor/tryChangeLevel, and mulerunner.walkNear's own citation of the
//     BankFish/BankAll fix that made both delegate to it for this same reason). CookRound
//     therefore requires Deps.WalkNear to be set (internal/bot/mule.go wires it to
//     freshstartrunner.WalkNear via muleFreshStartDeps) and refuses to run rather than
//     risk exactly that stall with a plain walk.
//   - Every raw-fish object name and every cooking level in
//     mulereceiverplan.FishTiers was read directly from this content build's own
//     content/scripts/skill_cooking/configs/cooking_source/cooking_generic.dbrow and
//     content/scripts/skill_fishing/configs/fishing.obj — see that table's own doc
//     comment for the full citation.
//
// Burn handling: none needed beyond what already exists. A burn still consumes the raw
// fish and produces a different (burnt) item in the same slot, so the same "wait for the
// raw fish count in the pack to drop" loop FillOnce/SpinFlaxUntil already use for their
// own single-item-transforms-in-place case covers a burn exactly like a success — this
// feature does not try to avoid burning (no gauntlets/level-past-burn-cap logic, no
// tracking which specific tier's burn rate is worst): the account owner's own ask was
// "start cooking the raw fish," not "cook it perfectly," and every raw fish this ladder
// ever tiers is well above its own zero-burn-chance level by the time a real fleet's mule
// bank accumulates enough of it to be worth cooking at all.
//
// STATUS (2026-09-20): the cook action works, and the long-running "OpLocU silently
// no-ops on this engine/content pairing" belief that rounds 1-4 of task_6b2af879 recorded
// here was WRONG. OpLocU is fine. Two ordinary mistakes were being read as one protocol
// bug, and this file had one of each.
//
//  1. THE REAL BUG HERE — the approach tile. Engine-TS accepts the packet, sets the
//     interaction, and then runs the loc's [oplocu,...] trigger only from a tile its own
//     reachedLoc test accepts (Player.tryInteract). With clientRoutefinder on, it will not
//     walk the account into position: the client is expected to have done that. The
//     Catherby range is a 1x2 loc at angle 1 with forceapproach 13, so only its own two
//     footprint tiles and the two tiles due south of them work. CookRound walked to
//     `WalkNear(CatherbyRange, 1, ...)`, which also admits the west and north tiles, and
//     from those the engine did nothing but send "I can't reach that!" — a game message
//     nothing in this path was reading, which is where "no chat message" came from.
//     Fixed by approachRange below, on internal/locreach; see that package's doc comment.
//
//  2. THE SANITY CHECK THAT MISLED EVERYONE — round 4's "the same silent no-op reproduces
//     on crafterrunner's own proven spinning wheel, so it must be the protocol" used loc
//     2644 at 2711,3471. The loc actually standing at 2711,3471 in this content build is
//     type 595 (bigtable2); 2644 (spinningwheel) is not there. So that send was rejected
//     at OpLocUHandler's World.getLoc gate, which IS silent — a different failure from the
//     range's, mistaken for the same one. crafterrunner itself was never affected: its
//     findWheel resolves the wheel by NAME through the loc index and hits the real one.
//     Round 2's "World.getLoc returning null is ruled out" was not a real elimination — it
//     re-read the CLIENT's loc index, which is not the thing the server consults.
//
// Evidence (private instrumented engine copy, console.log at every gate in
// OpLocUHandler.ts and in Player.tryInteract, 2026-09-20). Raw shrimps on range 2728 at
// 2817,3444, one OPLOCU per tile:
//
//	2817,3444  gate3 ok, inOperable=true   -> "You put the shrimps on the stove."  cooked
//	2818,3444  gate3 ok, inOperable=true   -> cooked
//	2817,3443  gate3 ok, inOperable=true   -> cooked
//	2816,3444  gate3 ok, inOperable=FALSE  -> "I can't reach that!"   no fish touched
//	2817,3445  gate3 ok, inOperable=FALSE  -> "I can't reach that!"   no fish touched
//
// and, for the same account and component in the same run, loc 2644 at 2711,3471 from all
// five tiles: `REJECT gate3 World.getLoc null`, nothing else, every time — while loc 595 at
// that same tile with the same flax succeeded from all five. So the component, the inv
// listener, the slot, the obj id and the members gate were all fine throughout.
//
// The rest of this file is unchanged and was already live-verified round by round: the
// withdraw-detection bug (bank.Inventory(), not the ordinary pack view, while the bank is
// still open — countByName's own doc comment), the door-crossing bug crossCatherbyDoor
// exists for, and a wrong near/far pick inside its first version.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/rangecook"
	"rsb0t-go/internal/scripts/tile"
)

// CookingStat is UPDATE_STAT's own stat index for Cooking — gamestate.SkillNames[7] ==
// "Cooking", cross-checked the same way barbarianrunner.AgilityStat/fisherrunner.FishingStat
// already were.
const CookingStat = 7

// cookRangeSearchRadius is generous enough to find the range from anywhere WalkNear's own
// radius=1 arrival tolerance could leave the account standing, without risking a match on
// the OTHER real Range this same building holds further off (2818,3455 — over 11 tiles
// from CatherbyRange itself).
const cookRangeSearchRadius = 5

// cookGuardMax bounds CookAtRange's own loop the same way vialrunner.fillGuardMax bounds
// FillLeg: a full pack (28) plus a small margin, so a genuinely stuck cook attempt (e.g. a
// resolved id that mysteriously never leaves the pack) can't spin forever.
const cookGuardMax = mulereceiverplan.PackSlots + 2

// CookResult is one CookRound call's outcome, for logging.
type CookResult struct {
	Tier      mulereceiverplan.FishTier
	Withdrawn int
	Cooked    int
	Banked    int
}

func currentTile(d *Deps) *tile.Tile {
	x, z, level, ok := d.Session.State().Tile()
	if !ok {
		return nil
	}
	return &tile.Tile{X: x, Z: z, Level: level}
}

// ---- the one real closed door on this route ----
//
// Ground-truthed live tonight via a throwaway cmd/doorprobe diagnostic (not guessed, and
// not kept — the finding is recorded here instead), after
// freshstartrunner.WalkNear's own generic tryOpenDoor recovery repeatedly failed to cross
// it (every attempt: walk beside it, OpLoc "Open," "find() still reports it closed after
// 5s," then the raw step-through itself also timing out — captured live by cmd/cooktest
// before this fix). Root cause, confirmed by sending the identical OpLoc by hand from
// different tiles: this specific "Door" (loc type 1530, at 2816,3438,0, squarely between
// Catherby's bank and its range) only accepts its "Open" option from its OWN tile — from
// the south-adjacent tile (2816,3437, the first of tryOpenDoor's own four
// orthogonal-neighbour approach candidates) the exact same click came back a real
// "I can't reach that!" chat line every time, while sending it from the door's own tile
// opened it immediately (a real id swap to 1531, two live loc changes observed).
// freshstartrunner's own doorApproach list only ever tries a fixture's four neighbour
// tiles, never the fixture's own tile (right for most doors, which sit on a shared edge
// between two neighbours — wrong for this one, which occupies its own otherwise-walkable
// tile with only its north exit blocked, per cmd/doordiag's own per-tile exit/wall dump).
// freshstartrunner itself is out of scope to change from here (see this package's own doc
// comment), so crossing this one door is handled narrowly, by hand, right here instead —
// not a new generic mechanism, just this one known fixture.
var (
	catherbyDoorTile   = rangecook.CatherbyDoorTile
	catherbyDoorInside = rangecook.CatherbyDoorInside
)

// crossCatherbyDoor gets the account across catherbyDoorTile/catherbyDoorInside's own
// single blocked edge, in whichever direction the account's own current position implies.
// Since 3 October 2026 the crossing itself is rangecook.CrossCatherbyDoor (moved there
// unchanged so ScarabSwarm's self-cooked food can cross the same door); the history above
// and below stays here, where it was found.
//
// It returns why it failed rather than logging it, because the caller knows what this file
// cannot: whether the failure was a real door problem or a yield. Live 2026-09-20 (build
// 3299c3c) every "could not reach 2816,3439,0 beside the Catherby range's own door" and
// "could not get through the door" line landed in the same second as a donor's "on the way to
// hand off" announcement — the round's yieldable delay was failing every wait at once, and the
// door itself was fine each time (ensureHome walked straight back through it seconds later).
func crossCatherbyDoor(d *Deps) (bool, string) {
	return rangecook.CrossCatherbyDoor(cookDeps(d))
}

// cookDeps is d as rangecook needs it. Delay and WalkNear are read at call time, so CookRound's
// yieldable delay (it swaps d.Delay for the round) is the one rangecook waits on.
func cookDeps(d *Deps) *rangecook.Deps {
	return &rangecook.Deps{
		Session: d.Session, Locs: d.Locs, Config: d.Config,
		Delay:    func(t time.Duration) bool { return d.Delay(t) },
		WalkNear: func(dest tile.Tile, radius int, timeout time.Duration) bool { return d.WalkNear(dest, radius, timeout) },
		Log:      d.Log,
		Loot:     d.Loot,
	}
}

// onRangeSide reports whether t is inside the small room the Catherby range stands in, on the
// far side of catherbyDoorTile from the bank. cmd/doordiag on the collision pack: the room is a
// 13-tile component with bounding box x 2815..2818, z 3439..3443 on level 0, and the only way
// out towards the bank is the door's own edge. The door's own tile (z 3438) is on the bank side.
func onRangeSide(t tile.Tile) bool { return rangecook.OnCatherbyRangeSide(t) }

// cookingLevel is the mule's own real, live Cooking level — 1 (never 0) when the stat
// hasn't been seen yet, matching a brand-new account's own real floor rather than
// reporting "cooking level 0" (which HighestTierInBank would correctly treat as "nothing is
// cookable," but for the wrong reason).
func cookingLevel(d *Deps) int {
	skill, ok := d.Session.State().Skill(CookingStat)
	if !ok || skill.Level < 1 {
		return 1
	}
	return skill.Level
}

// countByName sums every slot in items whose resolved name matches name (case-
// insensitive), the one shared implementation behind every by-name count/lookup below.
// Takes items explicitly rather than always re-reading the pack, because a live count
// during a withdrawal/deposit must read whichever view is actually live at that instant:
// the account's own regular Inventory tab component does NOT reflect a withdrawal/deposit
// while the bank interface is open (confirmed live: a withdrawal that genuinely succeeded
// — the very next bank read showed the item really gone — still read back 0 in the
// ordinary Inventory tab moments later, because the bank replaces the visible pack with
// its own side "deposit" panel component while it's open, exactly the distinction
// BankReceived/DepositAllInPack already respect by using bank.Inventory(), not
// actions.NewEquipment(...).Inventory(), for their own post-action waits). Callers must
// pass bank.Inventory() while a bank is open and actions.NewEquipment(...).Inventory()
// once it's closed — never the wrong one for the moment.
func countByName(d *Deps, items []gamestate.SlotItem, name string) int {
	n := 0
	for _, it := range items {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !strings.EqualFold(obj.Name, name) {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		n += c
	}
	return n
}

func idForName(d *Deps, items []gamestate.SlotItem, name string) (int32, bool) {
	for _, it := range items {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && strings.EqualFold(obj.Name, name) {
			return it.ID, true
		}
	}
	return 0, false
}

// PlayerPack is the persistent inventory-tab component id — the same real, live-proven
// constant barbarianrunner.PlayerPack/fisherrunner.PlayerPack/crafterrunner.PlayerPack/
// vialrunner.PlayerPack already establish (this project's own established convention of
// small per-package duplication over a shared constant, per those packages' own doc
// comments). Used here instead of actions.NewEquipment(d.Session, d.Components).Inventory()
// (which this package's own DepositAllInPack/BankReceived still use, proven fine for
// their own call context — right after a trade or at the very top of Loop) for a real,
// live-found reason specific to this feature: found tonight via cmd/cooktest, right after
// a bank-open/close, a real door crossing and a multi-tile walk, Equipment.Inventory()'s
// own dynamic SideIcon-based tab resolution returned ZERO entries while the account's
// pack genuinely held 5 real, just-withdrawn Raw shrimps — confirmed by reading this exact
// fixed component id in the same moment and seeing all 5, correctly named and counted.
// Root cause not fully chased down (some interaction between the bank/door/range
// sequence's own modal and sidebar-tab transitions and Equipment's tabInvComponent
// resolution, plausibly the same class of "which tab is logically active" state this
// project's docs describe elsewhere for random-event/bank races) — this fixed id sidesteps
// it entirely, the same way every OTHER runner package's own pack read already does,
// rather than chasing a fix inside internal/actions (out of this package's own scope).
const PlayerPack = 3214

// packItems is the account's own ordinary pack via PlayerPack, nil-safe.
func packItems(d *Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

// bankRawFishCounts reads every one of mulereceiverplan.FishTiers' own raw-fish names out
// of a live bank read, keyed lower-case — exactly the shape HighestTierInBank's own doc
// comment requires. Counts every item in items, not only known raw fish, since the map is
// only ever consulted by name — a bank full of unrelated items just contributes keys
// HighestTierInBank never looks at.
func bankRawFishCounts(d *Deps, items []gamestate.SlotItem) map[string]int {
	counts := map[string]int{}
	for _, it := range items {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		counts[strings.ToLower(strings.TrimSpace(obj.Name))] += c
	}
	return counts
}

// findRange locates the real Catherby range from wherever the account currently stands —
// requires having already walked within cookRangeSearchRadius of
// mulereceiverplan.CatherbyRange (CookRound's own job, via Deps.WalkNear).
func findRange(d *Deps) (locindex.Match, bool) {
	return rangecook.FindRange(cookDeps(d), cookRangeSearchRadius)
}

// approachRange walks the account onto a tile the ENGINE will accept an OPLOCU against this
// range from, and reports whether it got there.
//
// This is the fix for the silent no-op this file's doc comment used to blame on OpLocU
// itself. The engine runs a loc's [oplocu,...] trigger only from a tile its own reachedLoc
// test accepts, and with clientRoutefinder on it will not walk the account into position:
// from a tile it refuses, the whole interaction produces nothing but an "I can't reach
// that!" message nothing here was reading. The Catherby range is a 1x2 loc at angle 1 with
// forceapproach 13, so of the tiles CookRound's old `WalkNear(CatherbyRange, 1, ...)` could
// leave the account on, only its own footprint and the tiles due south work — measured live,
// see internal/locreach's own doc comment for the transcript.
func approachRange(d *Deps, rng locindex.Match) bool {
	return rangecook.ApproachRange(cookDeps(d), rng)
}

// cookOutTier uses the pack's own held raw fish of tier on the range at rng repeatedly —
// SpinFlaxUntil/FillOnce's own established shape (see this file's own doc comment) — until
// none remain (all cooked or burnt, the ordinary end), a use fails and does not clear
// after a delay, or a stop is requested. Returns how many were consumed.
func cookOutTier(d *Deps, rng locindex.Match, tier mulereceiverplan.FishTier) int {
	// CookRound logs the yield once, whichever wait inside noticed it first. A trade that arrives while the batch cooks waits for
	// it (CookRound restores the plain delay first): a random event still stops it.
	stop := func() bool { return randomevents.Busy(d.Session) }
	return rangecook.CookOut(cookDeps(d), rng, tier.Raw, cookGuardMax, stop)
}

// CookRound performs exactly one bounded idle-cooking round: pick the highest cooking-level
// tier still banked and within the mule's own real Cooking level
// (mulereceiverplan.HighestTierInBank), withdraw a full pack of its raw fish, walk to the
// real Catherby range and cook it out, walk back, and bank whatever resulted (cooked and
// any burnt food alike, via DepositAllInPack — see that function's own doc comment). See
// this file's own package doc comment for why one bounded round per call is exactly what
// muleBot.Loop's own !res.Handled branch needs to never delay a trade.
//
// Requires d.Pack, d.Locs, d.Config, d.Components and d.WalkNear — logs and returns false
// rather than acting on an incompletely wired Deps, the same convention
// BankReceived/DepositAllInPack already use for the first four; WalkNear specifically
// (not this package's own plain walkNear) because a real door sits across the only route
// to the range (see this file's own doc comment) and a plain FindPath+Walk cannot open it.
func CookRound(d *Deps) (CookResult, bool) {
	// restore puts the ordinary delay back. Only the way OUT and the cooking itself are idle
	// work that a trade may interrupt; the way home is not. Live 2026-09-20 (build 3299c3c)
	// the yieldable delay stayed in force for the return trip, so once a donor announced itself
	// every wait failed at once: "could not reach 2816,3439,0 beside the Catherby range's own
	// door" and "could not get back through the door" were logged in the very second of the
	// yield, three times in nine minutes, and the mule was left in the range room with its pack
	// for ensureHome to fetch.
	restore := func() {}
	yielding := func() bool { return false }
	// stopped is a stop request (the bot is being stopped), read through the ordinary delay: a
	// zero-length Context.Delay reports it without waiting.
	orig := d.Delay
	stopped := func() bool { return orig != nil && !orig(0) }
	if d.IdlePending != nil {
		// WalkNear's adapter closes over this Deps too, so its travel waits
		// yield along with range-use waits.
		delay := d.Delay
		d.Delay = func(duration time.Duration) bool {
			for duration > 0 {
				if d.IdlePending() {
					return false
				}
				chunk := duration
				if chunk > 100*time.Millisecond {
					chunk = 100 * time.Millisecond
				}
				if !delay(chunk) {
					return false
				}
				duration -= chunk
			}
			return !d.IdlePending()
		}
		restore = func() { d.Delay = delay }
		defer restore()
		yielding = d.IdlePending
		if d.IdlePending() {
			return CookResult{}, false
		}
	}
	if d.Pack == nil || d.Locs == nil || d.Config == nil || d.Components == nil {
		d.logf("cooking skipped: no pack/loc-index/config/components configured")
		return CookResult{}, false
	}
	if d.WalkNear == nil {
		d.logf("cooking skipped: no door-aware WalkNear wired into Deps — refusing to risk a plain walk stalling at the real door between Catherby's bank and its range (see cook.go's own doc comment)")
		return CookResult{}, false
	}

	level := cookingLevel(d)
	bank, ok := openNearestBankBooth(d)
	if !ok {
		return CookResult{}, false
	}
	if d.DeferCook != nil && d.DeferCook(bank.Items()) {
		bank.Close()
		return CookResult{}, false
	}
	counts := bankRawFishCounts(d, bank.Items())
	tier, ok := mulereceiverplan.HighestTierInBank(counts, level)
	if !ok {
		bank.Close()
		d.logf("cooking: no raw fish within Cooking level %d found in the bank — nothing to cook this round", level)
		return CookResult{}, false
	}
	rawID, ok := idForName(d, bank.Items(), tier.Raw)
	if !ok {
		bank.Close()
		d.logf("cooking: %s reported banked but its item id could not be resolved", tier.Raw)
		return CookResult{}, false
	}
	// before/the wait below both read bank.Inventory() (the bank's own side deposit
	// panel), not the account's ordinary pack — see countByName's own doc comment for
	// why: the ordinary Inventory tab component does not reflect a withdrawal while the
	// bank interface is still open, found live via this exact call before the fix.
	before := countByName(d, bank.Inventory(), tier.Raw)
	// The last yield point before the withdrawal. Past it the click is sent, and from then on
	// the round must see the withdrawal through to a pack it can account for.
	if yielding() {
		bank.Close()
		return CookResult{}, false
	}
	if err := bank.WithdrawAll(rawID); err != nil {
		bank.Close()
		d.logf("cooking: withdraw %s: %v", tier.Raw, err)
		return CookResult{}, false
	}
	// THE WITHDRAWAL IS WAITED FOR WITH THE ORDINARY DELAY, NOT THE YIELDABLE ONE. Live
	// 2026-09-23 09:05:49 (build c0da98ee): a donor's order landed in the same second as this
	// click, the yieldable delay failed its first 100 ms wait, the wait read "none arrived",
	// and the round logged "withdrawing Raw shark did not add any to the pack -- aborting this
	// round" and returned with the bank closed -- while the 28 raw sharks it had in fact
	// withdrawn landed in the pack a moment later. The hand-out it had yielded to then opened
	// the bank with a full pack, its Withdraw-1 of a Crystal key drew nothing ("timed out
	// waiting for the bank to hand over one"), and the donor was declined "could not draw it out
	// of the bank" and told to come back in 30 minutes. A yield only ever needs the round to
	// stop soon; it never needs this wait to lie about what the server did.
	waitFor(withDelay(d, orig), fmt.Sprintf("%s in pack after withdrawing", tier.Raw), 5*time.Second, func() bool {
		return countByName(d, bank.Inventory(), tier.Raw) > before
	})
	withdrawn := countByName(d, bank.Inventory(), tier.Raw) - before
	res := CookResult{Tier: tier, Withdrawn: withdrawn}
	if withdrawn <= 0 || yielding() {
		// Whatever this round leaves in the pack goes back NOW, while the bank is still open,
		// so the trade or hand-out it is yielding to starts with an empty pack rather than one
		// the next DepositAllInPack may read before the server has filled it.
		banked := bankPackAtOpenBank(withDelay(d, orig), bank)
		bank.Close()
		if withdrawn <= 0 {
			d.logf("cooking: withdrawing %s did not add any to the pack — aborting this round (banked %d stack(s) back)", tier.Raw, banked)
		} else {
			d.logf("cooking: yielding to event handling or an incoming trade right after withdrawing %d %s — banked %d stack(s) back before leaving the booth", withdrawn, tier.Raw, banked)
		}
		return res, false
	}
	bank.Close()
	d.logf("cooking: withdrew %d %s (Cooking level %d, tier requires %d)", withdrawn, tier.Raw, level, tier.Level)

	// goBack ends the round early: whatever stopped it, the withdrawn fish go back to the bank
	// now, with the ordinary delay, rather than riding in the pack to the trading post.
	goBack := func(why string) (CookResult, bool) {
		restore()
		if stopped() {
			d.logf("cooking: stop requested %s — the pack is banked before the next trade after a restart", why)
			return res, false
		}
		if yielding() {
			d.logf("cooking: yielding to event handling or an incoming trade %s — taking the pack back to the bank", why)
		} else {
			d.logf("cooking: %s — taking the pack back to the bank", why)
		}
		return comeBackAndBank(d, res)
	}

	// A brief settle wait after the bank's own close-modal transition, before the first
	// OpLoc a door-open recovery might send — mirrors openNearestBankBooth's own
	// post-open settle delay (see its own call site) for the same class of race this
	// project's docs describe repeatedly: an action sent too soon after a modal
	// transition can be silently dropped by the server while the client's own modal
	// state is still settling.
	if !d.Delay(600 * time.Millisecond) {
		return goBack("right after withdrawing")
	}

	// crossCatherbyDoor first — the real closed door on this route (see its own doc
	// comment) that freshstartrunner.WalkNear's own generic recovery cannot open by
	// itself. Once across, the range sits in the same small connected pocket as the
	// door's inside tile, so the ordinary WalkNear right after this succeeds via a plain
	// FindPath — no door involved a second time.
	if crossed, why := crossCatherbyDoor(d); !crossed {
		if yielding() {
			return goBack("on the way to the range")
		}
		return goBack("could not get through the door to the Catherby range (" + why + ")")
	}
	if !d.WalkNear(mulereceiverplan.CatherbyRange, 1, 60*time.Second) {
		if yielding() {
			return goBack("on the way to the range")
		}
		return goBack("could not reach the Catherby range")
	}
	rng, ok := findRange(d)
	if !ok {
		return goBack("reached the Catherby range's own tile but no Range loc is in sight")
	}
	if !approachRange(d, rng) {
		if yielding() {
			return goBack("on the way to the range")
		}
		return goBack("could not stand where the engine will accept a use on the Catherby range")
	}

	// At the range the batch is cooked out before a trade that arrived is served: a cook of 28 takes about a minute, and a yield
	// mid-batch cost the walk home with half the pack raw, the bank and the walk back for the rest (a user request: "optimize
	// mule handouts and times when accounts come to it ... so the mule has more time to cook"; live 21:00-22:00, 9 of 55 rounds
	// yielded part-way, e.g. "processed 9 of 28 Raw shark"). The way out still yields: nothing is lost there.
	restore()
	res.Cooked = cookOutTier(d, rng, tier)
	// Logged here rather than inside cookOutTier: the yield can be noticed by the loop's own
	// check or by any wait inside it (the pacing delay after each fish, the wait for the count
	// to drop), and on 2026-09-20 a lab run that hit the pacing delay printed no yield line at
	// all although the round had yielded and banked correctly.
	yielded := yielding() || randomevents.Busy(d.Session)
	restore()
	if yielded {
		d.logf("cooking: yielding to event handling or an incoming trade")
	}
	if res.Cooked == 0 {
		// Real, live-found gap (see this file's own package doc comment, "HONEST
		// STATUS"): every OpLocU attempt against the range sent cleanly but never
		// actually cooked anything on the engine this was tested against. Reported as
		// a failed round, not a quiet success, so an operator's log honestly shows
		// nothing was accomplished rather than implying the opposite — the withdrawn
		// raw fish is still banked back below, not lost.
		d.logf("cooking: %d %s sent to the range but none were actually cooked — see cook.go's own doc comment for the known gap this points at", withdrawn, tier.Raw)
	} else {
		d.logf("cooking: processed %d of %d %s at the range", res.Cooked, withdrawn, tier.Raw)
	}
	if stopped() {
		d.logf("cooking: stop requested at the range — the pack is banked before the next trade after a restart")
		return res, false
	}
	return comeBackAndBank(d, res)
}

// withDelay is d with its Delay swapped for delay, for one wait that must not yield (see
// CookRound's withdrawal). A shallow copy: every other field, the session included, is shared.
func withDelay(d *Deps, delay Delayer) *Deps {
	if delay == nil {
		return d
	}
	cp := *d
	cp.Delay = delay
	return &cp
}

// bankPackAtOpenBank deposits every stack the bank's own side panel shows in the pack, through a
// bank that is already open, and returns how many kinds went: one Deposit-All per kind, all sent
// together and confirmed once, anything short one at a time (depositKindsFast). The shared "the
// pack must be empty before anything else happens here" step of a cook round that stops at the
// booth and of a hand-out that finds the pack holding something (makeRoom).
func bankPackAtOpenBank(d *Deps, bank *lockedBank) int {
	seen := map[int32]bool{}
	var ids []int32
	for _, it := range bank.Inventory() {
		if !seen[it.ID] {
			seen[it.ID] = true
			ids = append(ids, it.ID)
		}
	}
	return depositKindsFast(d, bank, ids)
}

// comeBackAndBank walks the account from wherever the round left it — the range room, the
// door, or the bank side — back to Catherby's bank and banks the whole pack: cooked fish,
// burnt fish and any raw fish a cut-short round never used. It runs with the ordinary delay
// (CookRound restores it first): a trade that interrupted the round is waiting at the post,
// and a yieldable delay here would fail every wait at once.
func comeBackAndBank(d *Deps, res CookResult) (CookResult, bool) {
	if here := currentTile(d); here != nil && onRangeSide(*here) {
		if crossed, why := crossCatherbyDoor(d); !crossed {
			d.logf("cooking: could not get back through the door to Catherby's bank (%s) — will bank next cycle instead", why)
			return res, false
		}
	}
	if !d.WalkNear(mulereceiverplan.CatherbyBankTile, 3, 60*time.Second) {
		d.logf("cooking: could not walk back to the Catherby bank after cooking — will bank next cycle instead")
		return res, false
	}
	res.Banked, _ = DepositAllInPack(d)
	d.logf("cooking: banked %d stack(s) after cooking", res.Banked)
	return res, res.Cooked > 0
}
