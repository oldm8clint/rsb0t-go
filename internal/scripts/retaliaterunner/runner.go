// Package retaliaterunner is the I/O half of RetaliateChopper, the two-tick oak chopper described in
// docs/two_tick_retaliate_research_2026-11-04.md. The pure half is internal/scripts/retaliateplan (the controller that decides
// what to write each tick, the site table, the gear and aggro planning, the safety checks); this package walks, wields, casts
// and writes the packets, and keeps the phases:
//
//	Prepare   the shortbow worn and no ammo, the axe in the pack, auto-retaliate on, the Rapid style.
//	Stand     the stand tile chosen from the collision pack and the loc reach model, and walked to.
//	Aggro     the two farm rats woken with a stat-lowering spell, AggroGap ticks apart, then led to the stand tile.
//	Run       the tick loop: one packet a tick, a Clear then a Click, alternating (retaliateplan.Controller).
//	Fallback  ordinary chopping (the knife trick or a plain click) when the rats cannot be phased or cannot be woken.
//
// The account is never allowed to fight a rat: the bow is checked worn every few ticks (a bare-handed retaliation would kill
// a 2-hitpoint rat), no ammo may be worn, a rat showing a hitmark ends the loop at once, and the loop stops when hitpoints fall
// (the rats hit for 0, so any real damage is another monster).
//
// Nothing here is proven live. Every decision that is only a guess about the engine logs a line that says what it saw.
package retaliaterunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/chopmanipplan"
	"rsb0t-go/internal/scripts/retaliateplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// Deps is the woodcutting runner's: the session, the collision pack, the loc index, the config, the walker and the log.
type Deps = woodcutrunner.Deps

// Stat indexes (gamestate/skills.go).
const (
	statRanged = 4
	statMagic  = 6
	statWC     = 8
)

// playerFace is added to a player's slot in an NPC's face-entity value.
const playerFace = 32768

// PlayerPack is the persistent backpack component id.
const PlayerPack = woodcutrunner.PlayerPack

// Phase is where the runner is in its setup.
type Phase int

// The phases.
const (
	PhasePrepare Phase = iota
	PhaseStand
	PhaseAggro
	PhaseRun
	PhaseFallback
)

func (p Phase) String() string {
	return [...]string{"prepare", "stand", "aggro", "run", "fallback"}[p]
}

// tree is one oak the stand tile can chop.
type tree struct {
	m locindex.Match
}

// Runner is one account's RetaliateChopper. Build it with New and call Step from the script's Loop.
type Runner struct {
	Set  retaliateplan.Settings
	Site retaliateplan.Site

	phase     Phase
	stand     tile.Tile
	haveStand bool
	badStand  map[tile.Tile]bool
	trees     []tree
	cur       int // the tree being clicked, -1 for none standing

	ctl     *retaliateplan.Controller
	rephase retaliateplan.Rephaser
	picker  *retaliateplan.DropPicker
	manip   *woodcutrunner.ManipState

	fallbackUntil time.Time
	fallbackSaid  bool
	aggroFails    int

	stopped string

	// totals for the stop line
	startedAt time.Time
	startXP   int
	totLogs   int64
	totTicks  int
	lastClick int // tick of the newest plain click while unlocked
}

// New returns a runner for the settings.
func New(set retaliateplan.Settings) *Runner {
	set = set.Normalised()
	site, _ := retaliateplan.SiteByName(set.Site)
	return &Runner{
		Set: set, Site: site, badStand: map[tile.Tile]bool{}, cur: -1,
		ctl: retaliateplan.NewController(set), rephase: retaliateplan.Rephaser{Max: set.PhaseTries},
		picker: retaliateplan.NewDropPicker(), manip: woodcutrunner.NewManipState(),
	}
}

// Stopped is the reason the runner gave up for good ("" while it is running).
func (r *Runner) Stopped() string { return r.stopped }

// Phase is the current phase.
func (r *Runner) Phase() Phase { return r.phase }

func (r *Runner) logf(d *Deps, format string, args ...any) {
	if d.Log != nil {
		d.Log("retaliate: " + fmt.Sprintf(format, args...))
	}
}

func (r *Runner) vlog(d *Deps, format string, args ...any) {
	if r.Set.Verbose() {
		r.logf(d, format, args...)
	}
}

func (r *Runner) stop(d *Deps, format string, args ...any) {
	r.stopped = fmt.Sprintf(format, args...)
	r.logf(d, "STOPPED: %s", r.stopped)
}

// Summary is the stop line.
func (r *Runner) Summary(d *Deps) string {
	s := r.ctl.Stats()
	return fmt.Sprintf("%d logs in %d looping ticks, +%d woodcutting xp; %s", r.totLogs, r.totTicks, woodcutrunner.WoodcuttingXP(d)-r.startXP, s.Line())
}

// Start records the baselines. Call once before the first Step.
func (r *Runner) Start(d *Deps) {
	r.startedAt = time.Now()
	r.startXP = woodcutrunner.WoodcuttingXP(d)
	if r.Site.Name == "" {
		r.stop(d, "unknown site %q", r.Set.Site)
	}
}

// Step runs one phase and returns. A stopped runner does nothing.
func (r *Runner) Step(d *Deps) {
	if r.stopped != "" {
		d.Delay(3 * time.Second)
		return
	}
	if !woodcutrunner.WaitForGuardian(d) {
		return
	}
	st := d.Session.State()
	if st.IsDead() {
		if hp, ok := st.HP(); ok && hp > 0 {
			st.ClearDead()
		} else {
			r.stop(d, "the account died")
			return
		}
	}
	switch r.phase {
	case PhasePrepare:
		r.prepare(d)
	case PhaseStand:
		r.goStand(d)
	case PhaseAggro:
		r.aggro(d)
	case PhaseRun:
		r.run(d)
	case PhaseFallback:
		r.fallback(d)
	}
}

// ---- small readers ----

func here(d *Deps) tile.Tile {
	x, z, level, _ := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

func objName(d *Deps, id int32) string {
	if d.Config == nil {
		return ""
	}
	o, ok := d.Config.Obj(int(id))
	if !ok || o.Noted() {
		return ""
	}
	return o.Name
}

func npcName(d *Deps, typ int) string {
	if d.Config == nil {
		return ""
	}
	n, ok := d.Config.Npc(typ)
	if !ok {
		return ""
	}
	return n.Name
}

// packCounts is the pack's item counts by lower-case name.
func packCounts(d *Deps) map[string]int {
	out := map[string]int{}
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return out
	}
	for _, it := range inv.NonEmpty() {
		if n := objName(d, it.ID); n != "" {
			out[strings.ToLower(n)] += int(it.Count)
		}
	}
	return out
}

func (r *Runner) packLogs(d *Deps) []retaliateplan.DropCand {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	var out []retaliateplan.DropCand
	for _, it := range inv.NonEmpty() {
		if strings.EqualFold(objName(d, it.ID), r.Site.Logs) {
			out = append(out, retaliateplan.DropCand{Slot: it.Slot, ID: it.ID})
		}
	}
	return out
}

// worn reads the weapon and the ammo from the worn panel.
func worn(d *Deps) (weapon, ammo string, weaponID, ammoID int32) {
	eq := actions.NewEquipment(d.Session, d.Components)
	for _, it := range eq.Worn() {
		switch it.Slot {
		case retaliateplan.WearRHand:
			weapon, weaponID = objName(d, it.ID), it.ID
		case retaliateplan.WearQuiver:
			ammo, ammoID = objName(d, it.ID), it.ID
		}
	}
	return
}

func waitFor(d *Deps, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !d.Delay(100 * time.Millisecond) {
			return false
		}
	}
	return cond()
}

// waitTick waits until the client has seen tick target, polling finely so a write made right after reads the same phase of
// the 600 ms cycle every time. It returns false when the caller should stop or the ticks stopped coming.
func waitTick(d *Deps, target int) bool {
	st := d.Session.State()
	deadline := time.Now().Add(time.Duration(target-st.Tick()+4) * 700 * time.Millisecond)
	for st.Tick() < target {
		if time.Now().After(deadline) || !d.Delay(5*time.Millisecond) {
			return false
		}
	}
	return true
}

// ---- Prepare ----

func (r *Runner) prepare(d *Deps) {
	if d.Components == nil {
		r.stop(d, "no interface archive configured: cannot read or change the worn items")
		return
	}
	st := d.Session.State()
	eq := actions.NewEquipment(d.Session, d.Components)
	for i := 0; i < 8; i++ {
		weapon, ammo, _, ammoID := worn(d)
		view := retaliateplan.GearView{Weapon: weapon, Ammo: ammo, AxeHeld: woodcutrunner.HasWorkingAxe(d)}
		if sk, ok := st.Skill(statRanged); ok {
			view.Ranged = sk.Level
		}
		inv := st.Inventory(PlayerPack)
		if inv != nil {
			for _, it := range inv.NonEmpty() {
				if n := objName(d, it.ID); retaliateplan.IsShortbow(n) {
					view.PackBows = append(view.PackBows, n)
				}
			}
		}
		step, with := retaliateplan.PlanGear(view)
		switch step {
		case retaliateplan.GearOK:
			r.controls(d)
			if r.stopped == "" {
				r.logf(d, "gear ready: %s wielded, no ammo, axe in the pack, auto-retaliate on, Rapid style", weapon)
				r.phase = PhaseStand
			}
			return
		case retaliateplan.GearRemoveQuiver:
			r.logf(d, "taking off the ammo (%s): a worn quiver turns a slipped click into fired arrows", with)
			if err := eq.Unequip(ammoID); err != nil {
				r.logf(d, "could not remove the ammo: %v", err)
			}
			waitFor(d, 4*time.Second, func() bool { _, a, _, _ := worn(d); return a == "" })
		case retaliateplan.GearWield:
			r.logf(d, "wielding the %s", with)
			id := int32(-1)
			if inv != nil {
				for _, it := range inv.NonEmpty() {
					if strings.EqualFold(objName(d, it.ID), with) {
						id = it.ID
						break
					}
				}
			}
			if id < 0 {
				r.logf(d, "the %s vanished from the pack", with)
				continue
			}
			if err := eq.Equip(id); err != nil {
				r.logf(d, "could not wield the %s: %v", with, err)
			}
			waitFor(d, 4*time.Second, func() bool { w, _, _, _ := worn(d); return strings.EqualFold(w, with) })
		case retaliateplan.GearNoBow:
			r.stop(d, "no shortbow the account's Ranged level can wield is worn or in the pack (weapon %q, Ranged %d): give it a Shortbow (no Ranged level needed) and assign the script again", weapon, view.Ranged)
			return
		case retaliateplan.GearNoAxe:
			r.stop(d, "no axe in the pack: the chop needs one carried while the bow is wielded (the woodcutting content takes the axe from the pack)")
			return
		}
		if !d.Delay(300 * time.Millisecond) {
			return
		}
	}
	r.logf(d, "the gear would not settle after 8 steps; trying again")
	d.Delay(2 * time.Second)
}

// controls turns auto-retaliate on and selects the Rapid style, and checks both took.
func (r *Runner) controls(d *Deps) {
	st := d.Session.State()
	if !st.AutoRetaliateOn() {
		_ = actions.SetAutoRetaliate(d.Session, true)
		if !waitFor(d, 4*time.Second, st.AutoRetaliateOn) {
			r.logf(d, "auto-retaliate did not turn on; the loop would never flinch, so waiting")
			d.Delay(2 * time.Second)
			r.stop(d, "auto-retaliate would not turn on")
			return
		}
	}
	if st.CombatMode() != retaliateplan.BowStyleRapid {
		if !waitFor(d, 4*time.Second, func() bool { return actions.CombatModes(d.Session) != nil }) {
			r.logf(d, "the bow's combat tab has not loaded yet")
		}
		if err := actions.SetCombatMode(d.Session, retaliateplan.BowStyleRapid); err != nil {
			r.logf(d, "could not press Rapid: %v", err)
		}
		if !waitFor(d, 4*time.Second, func() bool { return st.CombatMode() == retaliateplan.BowStyleRapid }) {
			r.stop(d, "the Rapid style would not select (combat mode %d): the flinch would be 2 ticks, not 1, and the loop could not work", st.CombatMode())
		}
	}
}

// ---- Stand ----

// oaks is every oak of the site in the map (the live changes are not applied, so a felled one is still listed).
func (r *Runner) oaks(d *Deps) []locindex.Match {
	if d.Locs == nil || d.Config == nil {
		return nil
	}
	all := d.Locs.Find(d.Config, locindex.Query{X: r.Site.Anchor.X, Z: r.Site.Anchor.Z, Level: r.Site.Anchor.Level,
		Radius: r.Site.TreeRadius, Name: r.Site.Tree, Op: r.Site.Op}, nil)
	seen := map[tile.Tile]bool{}
	var out []locindex.Match
	for _, m := range all {
		at := tile.Tile{X: m.X, Z: m.Z, Level: m.Level}
		if !seen[at] {
			seen[at] = true
			out = append(out, m)
		}
	}
	return out
}

func (r *Runner) open(d *Deps, t tile.Tile) int {
	if d.Pack == nil {
		return 4
	}
	n := 0
	for _, o := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if d.Pack.Walkable(t.X+o[0], t.Z+o[1], t.Level) {
			n++
		}
	}
	return n
}

// chooseStand picks the stand tile and the oaks it can chop.
func (r *Runner) chooseStand(d *Deps) bool {
	oaks := r.oaks(d)
	if len(oaks) == 0 {
		r.logf(d, "no %s in the loc index within %d tiles of %d,%d", strings.ToLower(r.Site.Tree), r.Site.TreeRadius, r.Site.Anchor.X, r.Site.Anchor.Z)
		return false
	}
	from := here(d)
	reach := map[tile.Tile][]locindex.Match{}
	for _, m := range oaks {
		loc, ok := woodcutrunner.LocOf(d.Config, m)
		if !ok {
			continue
		}
		for _, t := range woodcutrunner.StandTiles(d.Pack, loc, from) {
			reach[t] = append(reach[t], m)
		}
	}
	var cands []retaliateplan.StandCand
	for t, ms := range reach {
		if r.badStand[t] {
			continue
		}
		walk := d.Pack == nil || d.Pack.Walkable(t.X, t.Z, t.Level)
		cands = append(cands, retaliateplan.StandCand{Tile: t, Trees: len(ms), Walkable: walk, Open: r.open(d, t)})
	}
	pick, ok := r.Site.ChooseStand(cands)
	if want, has := r.Set.StandTile(); has {
		if ms, reachable := reach[want]; reachable && !r.badStand[want] {
			pick, ok = want, true
			_ = ms
		} else {
			r.logf(d, "the stand tile %d,%d from the settings cannot chop any %s (or the server said it is not multi-combat): choosing one automatically", want.X, want.Z, strings.ToLower(r.Site.Tree))
		}
	}
	if !ok {
		r.logf(d, "no stand tile passes: %d candidate(s) read from the collision pack and the reach model, none walkable, in the multi-combat zone, inside both rats' leash with two open neighbours", len(cands))
		return false
	}
	r.stand, r.haveStand = pick, true
	r.trees = nil
	for _, m := range reach[pick] {
		r.trees = append(r.trees, tree{m: m})
	}
	var names []string
	for _, t := range r.trees {
		names = append(names, fmt.Sprintf("%d,%d", t.m.X, t.m.Z))
	}
	r.logf(d, "stand tile %d,%d (%d of %d candidates; %d open neighbours) chops %d oak(s): %s",
		pick.X, pick.Z, len(cands), len(reach), r.open(d, pick), len(r.trees), strings.Join(names, " and "))
	return true
}

func (r *Runner) goStand(d *Deps) {
	if !r.haveStand && !r.chooseStand(d) {
		r.aggroFails++
		if r.aggroFails >= 3 {
			r.toFallback(d, "no usable stand tile")
			return
		}
		d.Delay(3 * time.Second)
		return
	}
	for try := 0; try < 3; try++ {
		if here(d) == r.stand {
			break
		}
		if !woodcutrunner.WalkNear(d, r.stand, 0) && here(d) != r.stand {
			r.logf(d, "walking to the stand tile %d,%d did not finish (at %v)", r.stand.X, r.stand.Z, here(d))
		}
		if !d.Delay(300 * time.Millisecond) {
			return
		}
	}
	if here(d) != r.stand {
		r.logf(d, "could not reach the stand tile %d,%d (at %v): choosing another", r.stand.X, r.stand.Z, here(d))
		r.badStand[r.stand] = true
		r.haveStand = false
		return
	}
	st := d.Session.State()
	waitFor(d, 3*time.Second, func() bool { _, ok := st.Multiway(); return ok })
	if multi, ok := st.Multiway(); ok && !multi {
		r.logf(d, "the server says %d,%d is NOT a multi-combat tile: a second rat would be refused there; choosing another", r.stand.X, r.stand.Z)
		r.badStand[r.stand] = true
		r.haveStand = false
		return
	} else if !ok {
		r.logf(d, "the server has not said whether %d,%d is multi-combat; going on (the site table says it is)", r.stand.X, r.stand.Z)
	}
	r.phase = PhaseAggro
	if r.ratsOnUs(d) >= 2 {
		r.vlog(d, "both rats are already on the account")
		r.phase = PhaseRun
	}
}

// ---- Aggro ----

// rats are the rat NPCs in view.
func (r *Runner) rats(d *Deps) []gamestate.NPC {
	var out []gamestate.NPC
	for _, n := range d.Session.State().NPCs() {
		if n.Level == r.Site.Anchor.Level && strings.EqualFold(npcName(d, n.Type), r.Site.RatName) {
			out = append(out, n)
		}
	}
	return out
}

// ratsOnUs is how many rats are facing the account (an NPC fighting a player faces it).
func (r *Runner) ratsOnUs(d *Deps) int {
	st := d.Session.State()
	me := st.Self() + playerFace
	n := 0
	for _, rat := range r.rats(d) {
		if rat.FaceEntity == me {
			n++
		}
	}
	return n
}

func dist(a tile.Tile, n gamestate.NPC) int {
	return tile.Chebyshev(a, tile.Tile{X: n.X, Z: n.Z, Level: n.Level})
}

// aggro wakes the rats that are not on the account yet: the nearer first, the second AggroGap ticks later (the spell sets
// %action_delay to clock+5, and 6 is 2 mod 4, the offset two attackrate-4 monsters need to hit on alternate ticks), from the
// staging tile so that neither has hit before the second is cast (single-combat refuses the second cast otherwise), then leads
// them to the stand tile.
func (r *Runner) aggro(d *Deps) {
	st := d.Session.State()
	magic := 0
	if sk, ok := st.Skill(statMagic); ok {
		magic = sk.Level
	}
	spell, why, ok := retaliateplan.ChooseAggro(r.Set.AggroList(), magic, packCounts(d))
	if !ok {
		r.logf(d, "no aggro spell is possible (%s): the rats cannot be woken, so this account chops the ordinary way for %d minutes and checks again", why, r.Set.FallbackMins)
		r.toFallback(d, "no aggro spell")
		return
	}
	if why != "" {
		r.logf(d, "aggro spell %s (skipped: %s)", spell.Name, why)
	}
	if spell.Damages {
		r.logf(d, "WARNING: %s can kill a rat; a dead rat respawns elsewhere and its phase is lost", spell.Name)
	}
	if tile.Chebyshev(here(d), r.Site.Staging) > 3 {
		if !woodcutrunner.WalkNear(d, r.Site.Staging, 2) {
			r.logf(d, "could not walk to the staging tile %d,%d (at %v)", r.Site.Staging.X, r.Site.Staging.Z, here(d))
		}
		if !d.Delay(600 * time.Millisecond) {
			return
		}
	}
	rats := r.rats(d)
	me := here(d)
	var idle []gamestate.NPC
	for _, rat := range rats {
		if rat.FaceEntity != st.Self()+playerFace {
			idle = append(idle, rat)
		}
	}
	r.vlog(d, "%d rat(s) in view, %d not on the account yet, standing at %v", len(rats), len(idle), me)
	if len(rats) < 2 {
		r.aggroFails++
		r.logf(d, "only %d rat(s) in view from %v (the farm rats wander): waiting (%d of 3)", len(rats), me, r.aggroFails)
		if r.aggroFails >= 3 {
			r.toFallback(d, "the rats are not in view")
		} else {
			d.Delay(4 * time.Second)
		}
		return
	}
	if len(idle) > 0 {
		for i := 0; i < len(idle); i++ {
			for j := i + 1; j < len(idle); j++ {
				if dist(me, idle[j]) < dist(me, idle[i]) {
					idle[i], idle[j] = idle[j], idle[i]
				}
			}
		}
		t0 := st.Tick()
		if !waitTick(d, t0+1) {
			return
		}
		t1 := st.Tick()
		if err := actions.CastOnNpc(d.Session, spell.Name, idle[0].Index); err != nil {
			r.logf(d, "could not cast %s: %v", spell.Name, err)
			return
		}
		r.logf(d, "aggro: %s on rat %d (distance %d) written after tick %d", spell.Name, idle[0].Index, dist(me, idle[0]), t1)
		if len(idle) > 1 {
			if !waitTick(d, t1+retaliateplan.AggroGap) {
				return
			}
			t2 := st.Tick()
			if err := actions.CastOnNpc(d.Session, spell.Name, idle[1].Index); err != nil {
				r.logf(d, "could not cast %s on the second rat: %v", spell.Name, err)
			} else {
				r.logf(d, "aggro: %s on rat %d (distance %d) written after tick %d, %d ticks after the first", spell.Name, idle[1].Index, dist(here(d), idle[1]), t2, t2-t1)
			}
		}
		d.Delay(400 * time.Millisecond)
	}
	// lead them to the stand tile
	r.phase = PhaseStand
	if !woodcutrunner.WalkNear(d, r.stand, 0) {
		r.logf(d, "the walk back to the stand tile %d,%d did not finish (at %v)", r.stand.X, r.stand.Z, here(d))
	}
	r.aggroFails = 0
	r.phase = PhaseRun
	r.logf(d, "the account is at %v with %d rat(s) on it; measuring the hit spacing", here(d), r.ratsOnUs(d))
}

// ---- Rephase ----

// startRephase moves to the next phase after the loop asked for a re-phase.
func (r *Runner) startRephase(d *Deps, why string) {
	try, ok := r.rephase.Next()
	if !ok {
		r.logf(d, "%d re-phase tries did not give 2-tick hits (%s): falling back to ordinary chopping", r.Set.PhaseTries, why)
		r.toFallback(d, "the rats would not phase")
		return
	}
	on := r.ratsOnUs(d)
	r.logf(d, "re-phase %d of %d: %s; %d rat(s) are on the account", try, r.Set.PhaseTries, why, on)
	if on < 2 {
		r.phase = PhaseAggro
		return
	}
	nudges := r.Site.NudgeTiles(r.stand, try-1, func(t tile.Tile) bool { return d.Pack == nil || d.Pack.Walkable(t.X, t.Z, t.Level) })
	for _, t := range nudges {
		if !woodcutrunner.Routable(d.Pack, here(d), t) {
			continue
		}
		r.logf(d, "re-phase: stepping to %d,%d and back so the rats have to re-approach", t.X, t.Z)
		woodcutrunner.WalkNear(d, t, 0)
		st := d.Session.State()
		waitTick(d, st.Tick()+8)
		woodcutrunner.WalkNear(d, r.stand, 0)
		r.phase = PhaseRun
		return
	}
	r.logf(d, "re-phase: no walkable tile to step to; going back through the aggro")
	r.phase = PhaseAggro
}

// ---- Fallback ----

func (r *Runner) tier() woodcutplan.Tier {
	return woodcutplan.Tier{
		Label: "Oak (Falador south, retaliate site)", Level: 15, StartAt: 15, Loc: r.Site.Tree, Logs: r.Site.Logs, XP: 37.5,
		Anchor: r.Site.Anchor, Leash: r.Site.TreeRadius,
	}
}

// toFallback starts the ordinary chop for FallbackMins.
func (r *Runner) toFallback(d *Deps, why string) {
	r.phase = PhaseFallback
	r.fallbackUntil = time.Now().Add(time.Duration(r.Set.FallbackMins) * time.Minute)
	r.fallbackSaid = false
	r.logf(d, "FALLBACK: %s; chopping %s for %d minutes, then the two-tick setup is tried again", why, r.Set.Fallback, r.Set.FallbackMins)
	_ = actions.SetAutoRetaliate(d.Session, false)
}

func (r *Runner) fallback(d *Deps) {
	if time.Now().After(r.fallbackUntil) {
		r.logf(d, "fallback over: trying the two-tick setup again")
		r.rephase = retaliateplan.Rephaser{Max: r.Set.PhaseTries}
		r.aggroFails = 0
		r.haveStand = false
		r.phase = PhasePrepare
		return
	}
	if !r.fallbackSaid {
		r.fallbackSaid = true
	}
	d.Manip = &woodcutrunner.ManipConfig{Enabled: r.Set.Fallback == retaliateplan.FallbackKnife, State: r.manip}
	res := woodcutrunner.Chop(d, r.tier())
	switch res.Reason {
	case "full":
		r.dropAll(d)
	case "stopped":
		return
	default:
		d.Delay(2 * time.Second)
	}
}

// ---- Pack ----

// dropAll drops every log of the site's kind in one burst (five a tick).
func (r *Runner) dropAll(d *Deps) {
	eq := actions.NewEquipment(d.Session, d.Components)
	logs := r.packLogs(d)
	if len(logs) == 0 {
		return
	}
	slots := make([]gamestate.SlotItem, 0, len(logs))
	for _, l := range logs {
		slots = append(slots, gamestate.SlotItem{Slot: l.Slot, Item: gamestate.Item{ID: l.ID, Count: 1}})
	}
	out := eq.DropMany(d.Config, slots, woodcutrunner.Waiter(d))
	r.logf(d, "pack full: %s", out.Log())
}

// bank walks to the bank, deposits the logs and comes back. The rats are lost on the way, so the caller sets the phase to Aggro.
func (r *Runner) bank(d *Deps) bool {
	b, ok := woodcutrunner.OpenBankAt(d, r.Site.Bank)
	if !ok {
		r.logf(d, "could not open the bank at %d,%d: dropping the logs instead", r.Site.Bank.X, r.Site.Bank.Z)
		return false
	}
	keep := []string{"knife", "body rune", "water rune", "earth rune", "air rune", "mind rune"}
	keep = append(keep, woodcutrunner.HeldAxes(d)...)
	n := woodcutrunner.DepositAllExcept(d, b, keep)
	_ = b.Close()
	r.logf(d, "banked %d kind(s) of item at the Falador bank", n)
	return true
}

// livePack reports whether the loop's reading of a tree is still standing.
func (r *Runner) up(d *Deps, t tree) bool {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	_, ok := d.Locs.Nearest(d.Config, locindex.Query{X: t.m.X, Z: t.m.Z, Level: t.m.Level, Radius: 0, Name: r.Site.Tree, Op: r.Site.Op}, live)
	return ok
}

// swinging reports whether the axe animation was seen lately.
func swinging(d *Deps) bool {
	st := d.Session.State()
	seq, at := st.SelfAnim()
	return chopmanipplan.Swinging(seq, at, st.Tick())
}
