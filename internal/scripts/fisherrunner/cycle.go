package fisherrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fastopen"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/pearlplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// THE DROP CYCLE, the I/O half (the rules, and the content and engine facts behind them, are
// fisherplan/cycle.go's). One call, RunCycle, does a whole tidy-up of the pack: it opens the caskets
// and oysters, joins the key halves, cuts the Oyster pearls into Pearl bolttips when the account
// can, and drops the junk with the Empty oysters, all as user events of the engine's five a tick.
//
// It is ONE STEP on purpose: it runs to its end or to a stop, touches nothing but the backpack, and
// can be called from anywhere a fisher has the pack to itself (between two fishing holds, inside a
// walk, at the bank before the booth is clicked). A tick manipulation that writes batches of its
// own can call it between two of them: Cycle.PerTick lowers the clicks a tick the cycle spends so the
// two together stay inside the engine's five.
//
// It is called from four places, each with its own cost:
//
//   - FishUntil, when the pack is nearly full (fisherplan.CycleDue): the hold ends, the cycle runs, and
//     the spot is clicked again. This is the only place it costs fishing time: the clicks clear the
//     fishing interaction (engine OpHeldHandler clearPendingAction), so one cycle is one re-click.
//   - A walk (walkAside): the clicks leave the walk queue alone, so a cycle in the middle of a walk
//     that is long enough costs nothing, and what was caught before the walk is tidied by the time the
//     account is back at its spot.
//   - The bank, before the booth is clicked (OpenCatherbyBank): what is left of the last pack.
//   - The Loop boundary (internal/bot/bassoyster.go), for a pack that filled some other way.
//
// What it records: the caskets and oysters it opens go to the Loot panel exactly as the hand-off's
// opens do (recordOpenablesLoot), minus anything the script throws away; the tips a cut makes go on
// the Fletching card; nothing it drops is ever recorded as loot.

// FletchingStat is UPDATE_STAT's index for Fletching (gamestate.SkillNames: ... Cooking, Woodcutting,
// Fletching, Fishing ...), the skill a pearl cut advances.
const FletchingStat = 9

// FletchingLevel is the account's live Fletching level, 0 when the server has not sent one.
func FletchingLevel(d *Deps) int {
	skill, ok := d.Session.State().Skill(FletchingStat)
	if !ok {
		return 0
	}
	return skill.Level
}

// Cycle is one account's drop cycle: its switches and what it has done so far. A nil *Cycle in
// Deps.Cycle (ProgressiveFisher's) does nothing at all.
type Cycle struct {
	Opts fisherplan.CycleOptions
	// OnWalks also runs the cycle in the middle of a walk that is long enough for it (cycleOnWalks).
	OnWalks bool
	// PerTick is how many user events of each tick's five the cycle may use; 0 is all five. A tick
	// manipulation that sends clicks of its own sets it to what is left.
	PerTick int

	// Totals since the account started, for the stop line.
	Runs, Caskets, Oysters, Pearls, Empties, Joins, Cuts, Tips, Dropped int

	// lastWalkCheck throttles the walk's look at the pack to once every walkCycleEvery; walkOffUntil
	// pauses the walk's cycles after one that did nothing (the engine refused its clicks).
	lastWalkCheck time.Time
	walkOffUntil  time.Time
}

// walkCycleEvery is how often a walk looks at the pack for cycle work: two ticks, which is as fast as
// a catch can matter and costs a map read per slot a few times a minute.
const walkCycleEvery = 1200 * time.Millisecond

// walkCycleBackoff is how long a walk leaves the cycle alone after one that did nothing.
const walkCycleBackoff = 30 * time.Second

// CycleResult is what one RunCycle did.
type CycleResult struct {
	// Counts is the clicks the engine took: opens, joins, cuts and drops.
	Counts fisherplan.CycleCounts
	// Caskets and Oysters are the openables opened, Pearls the Oyster pearls those oysters gave and
	// Empties the Empty oysters thrown away (the ones held and the ones just opened).
	Caskets, Oysters, Pearls, Empties int
	// Tips is the Pearl bolttips the cuts added to the pack.
	Tips int
	// Raw is the raw fish among the dropped slots: they leave the pack count and a session's target alike.
	Raw int
	// Ticks is the game ticks the cycle spanned.
	Ticks int
	// Reason says why nothing ran, when nothing did.
	Reason string
	// Stopped is true when the caller's delay asked to stop.
	Stopped bool
}

// Progress reports whether the cycle did anything, which is what FishUntil keys its retry on.
func (r CycleResult) Progress() bool { return r.Counts.Total() > 0 }

// cycleItems is the pack as the cycle's rules read it.
func cycleItems(d *Deps) []fisherplan.CycleItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil || d.Config == nil {
		return nil
	}
	var out []fisherplan.CycleItem
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok {
			continue
		}
		out = append(out, fisherplan.CycleItem{Slot: it.Slot, ID: int(it.ID), Name: obj.Name, Noted: obj.Noted()})
	}
	return out
}

// cycleView is what the cycle's rules read right now.
func cycleView(d *Deps) fisherplan.CycleView {
	return fisherplan.CycleView{Pack: cycleItems(d), Opts: d.Cycle.Opts, Fletching: FletchingLevel(d), Junk: d.Junk}
}

// CycleDue reports whether the pack is nearly full and holds something for a cycle to do: the moment
// FishUntil ends a hold for one.
func CycleDue(d *Deps) bool {
	if d.Cycle == nil {
		return false
	}
	return fisherplan.CycleDue(FreeSlots(d), fisherplan.HasWork(cycleView(d)))
}

// KeptFullSlots is how many slots of kept catch (everything the drop cycle would not drop) make a
// BassOysterCaskets pack full for banking.
const KeptFullSlots = 27

// KeptFull reports whether the pack is full of the catch the account keeps: dropping the junk would leave
// at most one free slot. Banking then beats fishing on, where every junk roll filled the pack, opened
// content's "You can't carry any more fish." mesbox and cost a one-slot drop cycle and a re-click (live,
// 7 Oct 2026: 69 of 375 drop cycles in 15 minutes). False without a cycle or junk (ProgressiveFisher).
func KeptFull(d *Deps) bool {
	if d.Cycle == nil || d.Junk == nil {
		return false
	}
	used := 28 - FreeSlots(d)
	return used-len(fisherplan.PlanDrops(cycleView(d))) >= KeptFullSlots
}

// CycleWork reports whether a cycle would click anything now, however full the pack is.
func CycleWork(d *Deps) bool {
	return d.Cycle != nil && fisherplan.HasWork(cycleView(d))
}

// cycleBlocked says why the backpack cannot be clicked now: a bank or shop is the main modal (the
// backpack component is not visible then and the engine refuses the click), or a random event is
// being handled and nothing else may start an interaction on top of it.
func cycleBlocked(d *Deps) string {
	if main, _, _ := d.Session.State().Modals(); main != gamestate.NoModal {
		return "a bank or shop is open"
	}
	if randomevents.Busy(d.Session) {
		return "a random event is present"
	}
	return ""
}

// dropOption is the held op index of the item's own "Drop" (5 for every item in content 289), 0 when
// it has none.
func dropOption(d *Deps, id int) int {
	obj, ok := d.Config.Obj(id)
	if !ok {
		return 0
	}
	return actions.OptionIndex(actions.HeldOps(obj.IOps), "Drop")
}

// openOption is "Open", the first held op of a casket and an oyster.
const openOption = 1

// cycleSteps turns planned actions into fastopen steps, and returns the actions they stand for in
// the same order (a drop of an item with no Drop op is left out).
func cycleSteps(d *Deps, as []fisherplan.CycleAction) ([]fastopen.Step, []fisherplan.CycleAction) {
	steps := make([]fastopen.Step, 0, len(as))
	kept := make([]fisherplan.CycleAction, 0, len(as))
	for _, a := range as {
		switch a.Kind {
		case fisherplan.CycleOpen:
			steps = append(steps, fastopen.Step{ID: int32(a.ID), Slot: a.Slot, Op: openOption})
		case fisherplan.CycleDrop:
			op := dropOption(d, a.ID)
			if op == 0 {
				continue
			}
			steps = append(steps, fastopen.Step{ID: int32(a.ID), Slot: a.Slot, Op: op})
		case fisherplan.CycleJoin, fisherplan.CycleCut:
			steps = append(steps, fastopen.Step{ID: int32(a.ID), Slot: a.Slot, Use: true, UseID: int32(a.UseID), UseSlot: a.UseSlot})
		default:
			continue
		}
		kept = append(kept, a)
	}
	return steps, kept
}

// burst sends one burst and returns the actions whose item left its slot, which is what the engine
// took (a click it refused leaves its item where it was).
func burst(d *Deps, as []fisherplan.CycleAction) (done []fisherplan.CycleAction, r fastopen.Result) {
	steps, sent := cycleSteps(d, as)
	if len(steps) == 0 {
		return nil, fastopen.Result{Reason: "none"}
	}
	r = fastopen.Run(d.Session, fastopen.Delayer(d.Delay), PlayerPack, steps, fastopen.Options{PerTick: d.Cycle.PerTick})
	pending := map[[2]int]bool{}
	for _, p := range fastopen.Pending(d.Session.State(), PlayerPack, steps) {
		pending[[2]int{p.Slot, int(p.ID)}] = true
	}
	for _, a := range sent {
		if !pending[[2]int{a.Slot, a.ID}] {
			done = append(done, a)
		}
	}
	return done, r
}

// RunCycle runs one whole drop cycle (where is for the log: "fishing", "on a walk", "at the bank").
// It does nothing while a bank or shop is open or a random event is present, and sends nothing when
// the pack holds nothing for it.
func RunCycle(d *Deps, where string) (res CycleResult) {
	if d.Cycle == nil {
		return res
	}
	if why := cycleBlocked(d); why != "" {
		res.Reason = why
		return res
	}
	st := d.Session.State()
	startTick := st.Tick()
	defer func() {
		res.Ticks = st.Tick() - startTick
		res.finish(d, where)
	}()

	// The first burst: the opens, and the junk already held behind them. What the opens made is not in
	// the pack yet, so the second burst is planned from the pack as they leave it.
	v := cycleView(d)
	if first := fisherplan.PlanFirstBurst(v); len(first) > 0 {
		before := loot.PackCounts(st.Inventory(PlayerPack))
		done, r := burst(d, first)
		res.absorb(done)
		res.recordOpens(d, before)
		if r.Reason == "stopped" {
			res.Stopped = true
			return res
		}
		v = cycleView(d)
	}
	if tidy := fisherplan.PlanTidy(v); len(tidy) > 0 {
		tipsBefore := packTotal(d, pearlplan.TipsID)
		done, r := burst(d, tidy)
		res.absorb(done)
		res.Tips = max(packTotal(d, pearlplan.TipsID)-tipsBefore, 0)
		res.recordCuts(d, done)
		res.Stopped = r.Reason == "stopped"
	} else if res.Caskets > 0 {
		// Nothing follows the opens, so nothing closes the last casket's reward box: do it here. The
		// reward is already in the pack.
		if _, _, chat := st.Modals(); chat != gamestate.NoModal {
			_ = actions.CloseModal(d.Session)
		}
	}
	return res
}

// packTotal counts the pack's items with obj id.
func packTotal(d *Deps, id int32) int {
	n := 0
	if inv := d.Session.State().Inventory(PlayerPack); inv != nil {
		for _, it := range inv.NonEmpty() {
			if it.ID == id {
				n += max(int(it.Count), 1)
			}
		}
	}
	return n
}

// absorb adds the clicks the engine took to the result.
func (r *CycleResult) absorb(done []fisherplan.CycleAction) {
	c := fisherplan.Count(done)
	r.Counts.Opens += c.Opens
	r.Counts.Joins += c.Joins
	r.Counts.Cuts += c.Cuts
	r.Counts.Drops += c.Drops
	for _, a := range done {
		switch {
		case a.Kind == fisherplan.CycleOpen && strings.EqualFold(strings.TrimSpace(a.Name), fisherplan.Casket):
			r.Caskets++
		case a.Kind == fisherplan.CycleOpen:
			r.Oysters++
		case a.Kind == fisherplan.CycleDrop && fisherplan.IsRawFish(a.Name):
			r.Raw++
		}
		if a.Kind == fisherplan.CycleDrop && strings.EqualFold(strings.TrimSpace(a.Name), fisherplan.EmptyOyster) {
			r.Empties++
		}
	}
}

// recordOpens reports what the opens of the first burst yielded to the Loot panel, measured as the
// pack's own change since before (taken just before the first click). It runs straight after the
// burst, before the second one joins, cuts or drops anything, so a crystal key or a tip never lands on
// a casket's card, and what the script throws away (the Empty oyster) is never recorded.
func (r *CycleResult) recordOpens(d *Deps, before loot.Counts) {
	if r.Caskets+r.Oysters == 0 {
		return
	}
	after := loot.PackCounts(d.Session.State().Inventory(PlayerPack))
	nameOf := func(id int) (string, bool) {
		obj, ok := d.Config.Obj(id)
		if !ok {
			return "", false
		}
		return obj.Name, obj.Noted()
	}
	y := measureOpenables(before, after, nameOf, d.Junk)
	// The pearls the oysters gave, for the log.
	for id, n := range y.gained["Oyster"] {
		name, _ := nameOf(id)
		if strings.EqualFold(strings.TrimSpace(name), pearlplan.Pearl) || strings.EqualFold(strings.TrimSpace(name), pearlplan.Pearls) {
			r.Pearls += int(n)
		}
	}
	if d.Loot != nil {
		reportOpenables(d, y)
	}
}

// recordCuts reports the pearls the second burst cut to the Fletching card, as MasterCrafter's pearl
// round does: an event is one pearl, the product the tips it made (six from an Oyster pearl, 24 from
// Oyster pearls).
func (r *CycleResult) recordCuts(d *Deps, done []fisherplan.CycleAction) {
	if d.Loot == nil {
		return
	}
	cut := map[int]int{}
	for _, a := range done {
		if a.Kind == fisherplan.CycleCut {
			cut[a.ID]++
		}
	}
	for id, n := range cut {
		k, ok := pearlplan.KindByID(int32(id))
		if !ok {
			continue
		}
		loot.Made(d.Loot, d.Config, loot.Fletching(d.Config, int(k.ID), k.Name), n, loot.Product(int(pearlplan.TipsID), pearlplan.Tips, n*k.Tips))
	}
}

// finish counts the run and says what it did, in one line.
func (r *CycleResult) finish(d *Deps, where string) {
	if !r.Progress() {
		return
	}
	c := d.Cycle
	c.Runs++
	c.Caskets += r.Caskets
	c.Oysters += r.Oysters
	c.Pearls += r.Pearls
	c.Empties += r.Empties
	c.Joins += r.Counts.Joins
	c.Cuts += r.Counts.Cuts
	c.Tips += r.Tips
	c.Dropped += r.Counts.Drops
	d.logf("%s", r.line(d, where))
}

// line is the one log line of a cycle that did something: every part that ran, then the pack.
func (r CycleResult) line(d *Deps, where string) string {
	var parts []string
	if r.Caskets > 0 {
		parts = append(parts, fmt.Sprintf("opened %d casket(s)", r.Caskets))
	}
	if r.Oysters > 0 {
		parts = append(parts, fmt.Sprintf("opened %d oyster(s) (%d pearl(s), %d empty shell(s) dropped)", r.Oysters, r.Pearls, r.Empties))
	}
	if r.Counts.Joins > 0 {
		parts = append(parts, fmt.Sprintf("joined %d key half pair(s) into crystal key(s)", r.Counts.Joins))
	}
	if r.Counts.Cuts > 0 {
		parts = append(parts, fmt.Sprintf("cut %d oyster pearl(s) into %d Pearl bolttips", r.Counts.Cuts, r.Tips))
	}
	junk := r.Counts.Drops - r.Empties
	if junk > 0 {
		parts = append(parts, fmt.Sprintf("dropped %d junk slot(s) (%d raw fish)", junk, r.Raw))
	}
	if len(parts) == 0 && r.Empties > 0 {
		parts = append(parts, fmt.Sprintf("dropped %d empty oyster(s)", r.Empties))
	}
	return fmt.Sprintf("drop cycle (%s): %s — %d click(s) in %d tick(s), %d free slot(s) now",
		where, strings.Join(parts, ", "), r.Counts.Total(), r.Ticks, FreeSlots(d))
}

// cycleMidSession is FishUntil's cycle: the raw fish dropped leave the pack count and the session's
// target alike, so the target still counts catches. It reports whether anything was done; FishUntil
// stops trying for the rest of the session when nothing was.
func cycleMidSession(d *Deps, lastCount, target *int) bool {
	res := RunCycle(d, "fishing")
	if !res.Progress() {
		return false
	}
	*target -= res.Raw
	d.RawDropped += res.Raw
	*lastCount = CurrentRawFish(d)
	return true
}

// beforeBank is the cycle at the bank, before the booth is clicked: what is left of the last pack.
func beforeBank(d *Deps) {
	RunCycle(d, "at the bank, before banking")
}

// walkAside is the side work of a WalkNear walk (webwalk.WalkOptions.Aside), or nil when there is none:
// the run staller's rub and the drop cycle, in that order.
func (d *Deps) walkAside() func(webwalk.AsidePoll) {
	cycle := d.Cycle != nil && d.Cycle.OnWalks
	stall := d.Stall.active()
	if !cycle && !stall {
		return nil
	}
	return func(p webwalk.AsidePoll) {
		if stall {
			d.Stall.poll(d, p)
		}
		if cycle {
			walkCycle(d, p)
		}
	}
}

// walkCycle is the cycle's part of a walk's poll (webwalk.WalkOptions.Aside): when the pack holds
// something for it and fisherplan.WalkCycleOK says so (the click the walker is following still has far
// enough to go that the cycle fits inside the walk, or the pack is nearly full), run it. The clicks
// leave the walk queue alone (the engine's OpHeldHandler only clears the interaction), so the account
// walks on through it.
func walkCycle(d *Deps, p webwalk.AsidePoll) {
	c := d.Cycle
	if c == nil || !c.OnWalks {
		return
	}
	// A rub's p_delay refuses a click read inside it, and the cycle's clicks would be thrown away.
	if d.Stall.RubedRecently() {
		return
	}
	now := time.Now()
	if now.Sub(c.lastWalkCheck) < walkCycleEvery || now.Before(c.walkOffUntil) {
		return
	}
	c.lastWalkCheck = now
	st := d.Session.State()
	main, side, _ := st.Modals()
	if st.InCombat() || main != gamestate.NoModal || side != gamestate.NoModal {
		return
	}
	v := cycleView(d)
	first, tidy := fisherplan.PlanFirstBurst(v), fisherplan.PlanTidy(v)
	clicks := len(first) + len(tidy)
	if clicks == 0 {
		return
	}
	ahead := tile.Chebyshev(tile.Tile{X: p.Me.X, Z: p.Me.Z}, tile.Tile{X: p.Target.X, Z: p.Target.Z})
	if !fisherplan.WalkCycleOK(ahead, st.RunEnabled(), clicks, FreeSlots(d)) {
		return
	}
	if res := RunCycle(d, "on a walk"); !res.Progress() && res.Reason == "" && !res.Stopped {
		c.walkOffUntil = now.Add(walkCycleBackoff) // the clicks went out and the engine took none: not every poll
	}
}
