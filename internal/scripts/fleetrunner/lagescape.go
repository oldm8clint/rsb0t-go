package fleetrunner

import (
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// EscapeResult is what LagEscape did.
type EscapeResult int

const (
	// EscapeIdle: the rule did not call for leaving. The caller carries on as it always did.
	EscapeIdle EscapeResult = iota
	// EscapeHeld: the rule called for leaving and there was no way to (no teleport it can pay for, no walk allowed here or none out of
	// reach). The caller eats where it stands.
	EscapeHeld
	// EscapeTeleported: the teleport went out. The account is leaving; the caller should end its pass.
	EscapeTeleported
	// EscapeWalked: the account walked or fled; it is somewhere else now (or tried to be).
	EscapeWalked
)

// Acted reports whether the account teleported or walked: the caller has nothing more to do this pass.
func (r EscapeResult) Acted() bool { return r == EscapeTeleported || r == EscapeWalked }

const (
	// escapeNear is how close (tiles from the nearest edge of the NPC) an attackable NPC that does not face the account counts as able to
	// hit it, while the account is being hit: gamestate's deathNearbyRadius, the radius the death line's "near it" list uses.
	escapeNear = 3
	// escapeTeleportGap is how long a cast of the escape is not repeated. A cast takes three ticks to land and a late server takes seconds
	// more (a bite took fourteen); a second cast sent while the first is on its way only burns runes.
	escapeTeleportGap = 15 * time.Second
	// escapeSayEvery spaces a repeated line of the escape's about the same account.
	escapeSayEvery = 20 * time.Second
	// poisonTicks is the window in which the latest hitmark may still be a poison one (gamestate's combat window).
	poisonTicks = 10
)

// EscapeFlee is the guardian's walking flee, asked for by LagEscape; a variable so a test can say what it did without a pack to plan on.
var EscapeFlee = randomevents.FleeAttackers

// askFlee asks for the guardian's walking flee; it plans on the collision pack, so an account with none (a bare test tool) has none.
func askFlee(d *Deps) bool {
	return d.Pack != nil && EscapeFlee(d.Session, d.Pack, d.Log)
}

// reachScan is who can hit the account where it stands.
type reachScan struct {
	// hits is the worst single hit of each NPC that can hit it (facing it, or near it while it is being hit), unnamed hitter excluded.
	Hits []int
	// names are those NPCs' names, for the log.
	Names []string
	// facing is how many of them face the account.
	Facing int
	// pens are the safespot pens (those with a table of threats) whose ground the account stands on, and heldHere whether one of them
	// holds its tile.
	Pens     []fleetplan.TrainPen
	HeldHere bool
	// inCombat is a hitmark in the last ten ticks, poisonOnly whether the latest one was poison's.
	InCombat, PoisonOnly bool
}

// hit is whether the account is being hit by something that can be left: a hitmark in the combat window that is not poison's.
func (r reachScan) hit() bool { return r.InCombat && !r.PoisonOnly }

// Threatened is whether anything is hitting the account: an NPC faces it, or it took a hit that was not poison's.
func (r reachScan) Threatened() bool { return r.Facing > 0 || r.hit() }

// ScanReach reads who can hit the account now.
func ScanReach(d *Deps) reachScan {
	var r reachScan
	st := d.Session.State()
	here := CurrentTile(d)
	if st == nil || here == nil {
		return r
	}
	r.InCombat = st.InCombat()
	// The latest hitmark is poison's when a poison hit is inside the combat window and no other hit came after it (HitWithin counts the
	// ticks since the poison hit, which a later hit of any kind falls inside).
	if tick, ok := st.LastPoisonHit(); ok {
		if since := st.Tick() - tick; since < poisonTicks && !st.HitWithin(since) {
			r.PoisonOnly = true
		}
	}
	r.Pens = PensAround(*here)
	for _, p := range r.Pens {
		if PenHeld(p).Holds(here.X, here.Z, here.Level) {
			r.HeldHere = true
			break
		}
	}
	self := st.Self()
	type near struct {
		hit  int
		name string
	}
	var nearby []near
	for _, n := range st.NPCs() {
		if n.Level != here.Level || (n.TotalHealth > 0 && n.Health == 0) {
			continue // another floor, or a kill still lying there
		}
		name, size, attackable := "", 1, false
		if d.Config != nil {
			if t, ok := d.Config.Npc(n.Type); ok {
				name, size, attackable = t.Name, max(t.Size, 1), HasAttackOp(t.Ops)
			}
		}
		hit := fleetplan.NPCTypeMaxHit(n.Type, name) // a random event monster by its tier, else by name
		if self >= 0 && n.TargetsPlayer(self) {
			r.Hits, r.Names, r.Facing = append(r.Hits, hit), append(r.Names, name), r.Facing+1
			continue
		}
		// Only a known hunter or a random event monster counts from near by: in a crowd (Ardougne's market and castle) every Man,
		// Paladin and Guard within three tiles at the name default of 8 put the line at its cap after any lag.
		_, event := fleetplan.EventMonsterMaxHit(n.Type)
		hunter := event || fleetplan.KnownHunter(name)
		if attackable && hunter && tile.Chebyshev(tile.Tile{X: n.X, Z: n.Z}, tile.Tile{X: here.X, Z: here.Z})-(size-1) <= escapeNear {
			nearby = append(nearby, near{hit, name})
		}
	}
	if r.Facing > 0 || r.hit() {
		for _, n := range nearby {
			r.Hits, r.Names = append(r.Hits, n.hit), append(r.Names, n.name)
		}
	}
	return r
}

// escapeMemo is when LagEscape last said each of its lines for one account: one slot per line, so two lines alternating in one call
// (a "go" then a "nowalk" on every walk poll) are each said once per escapeSayEvery, not on every poll.
type escapeMemo struct {
	said map[string]time.Time
}

var escapeMemos = struct {
	sync.Mutex
	m map[string]*escapeMemo
}{m: map[string]*escapeMemo{}}

// SayEscape logs a line of the escape's at most once in escapeSayEvery for the same thing.
func SayEscape(d *Deps, what, format string, args ...any) {
	key := SafeGapKey(d)
	escapeMemos.Lock()
	m := escapeMemos.m[key]
	if m == nil {
		m = &escapeMemo{said: map[string]time.Time{}}
		escapeMemos.m[key] = m
	}
	last, seen := m.said[what]
	repeat := seen && time.Since(last) < escapeSayEvery
	if !repeat {
		m.said[what] = time.Now()
	}
	escapeMemos.Unlock()
	if !repeat {
		d.Logf(format, args...)
	}
}

// refugePens is the safespot pens' ground as one held-tile test and one safespot test.
func refugePens(pens []fleetplan.TrainPen) (held func(x, z int, level int) bool, spot func(t tile.Tile) bool) {
	grids := make([]*fleetplan.HeldGrid, 0, len(pens))
	for _, p := range pens {
		grids = append(grids, PenHeld(p))
	}
	held = func(x, z, level int) bool {
		for _, g := range grids {
			if g.Holds(x, z, level) {
				return true
			}
		}
		return false
	}
	spot = func(t tile.Tile) bool {
		for _, p := range pens {
			if p.OnSafeSpot(&t) {
				return true
			}
		}
		return false
	}
	return held, spot
}

// escapePenWalk walks the account off a safespot pen's held ground to the nearest tile no hunter holds, a safespot first among equals,
// eating on the way (runPath): spotreturn.go's refuge, without the bite before the first step. With no such tile within
// refugeMaxSteps it asks the guardian's flee instead. EscapeHeld when the tile is not held (the herd lets go of a target that stands on
// ground it does not hold) or nothing could be done.
func escapePenWalk(d *Deps, here tile.Tile, r reachScan, label string) EscapeResult {
	held, spotOf := refugePens(r.Pens)
	if !held(here.X, here.Z, here.Level) {
		return EscapeHeld
	}
	path, ok := fleetplan.NearestRefuge(here, func(x, z int) bool { return held(x, z, here.Level) }, PackCanStep(d.Pack), spotOf, RefugeMaxSteps)
	if !ok {
		SayEscape(d, "no-refuge", "lag escape [%s]: no ground that no hunter holds within %d steps of %s: asking for the guardian's flee", label, RefugeMaxSteps, HereIs(d))
		if askFlee(d) {
			return EscapeWalked
		}
		return EscapeHeld
	}
	refuge := path[len(path)-1]
	SayEscape(d, "refuge", "lag escape [%s]: %d facing it, %v near; %d steps from %s to %d,%d, the nearest ground no hunter holds (%d of the herd's tiles on the way)",
		label, r.Facing, r.Names, len(path), HereIs(d), refuge.X, refuge.Z, countHeld(path, held))
	// Retaliation off first, and not waited for: an account that answers the hit by attacking walks INTO the herd (the server brings it
	// next to its attacker), and the button's own click cancels a walk sent before it (combatrunner's flee notes say as much).
	if d.Session.State().AutoRetaliateOn() {
		_ = actions.SetAutoRetaliate(d.Session, false)
	}
	RunPath(d, here, path, refuge)
	return EscapeWalked
}

func countHeld(path []tile.Tile, held func(x, z, level int) bool) int {
	n := 0
	for _, t := range path {
		if held(t.X, t.Z, t.Level) {
			n++
		}
	}
	return n
}

// escapeNotHere is where the escape never runs: the Wilderness and the Mage Bank (Stage 3's legs there have one answer to danger, keep
// moving to the nearer exit, and a teleport is refused above level 20), and the Kalphite Queen's lair (Stage 6 has its own fight and life
// rules, kqlife.go, and a flee or a cast out of the fight is not its to take).
func escapeNotHere(t tile.Tile) bool {
	return fleetplan.WildernessLevel(t) > 0 || fleetplan.InMageBank(t) || fleetplan.InKQLair(t)
}

// LagEscape runs the escape rule once for the account: when it is being hit, its view or its bites run late and its hitpoints are at or
// under what its hitters can deal before a bite sent now can land (scarabplan.DecideEscape), it leaves. label names the caller in the log.
// allowWalk says whether the caller lets it walk (false where the caller owns the account's feet: a walk in progress, a quest leg, another
// goroutine); then the escape is a teleport or nothing.
//
// The cheap tests come first, in the order they fail for a healthy account: it runs on every Loop pass, every walk poll and every quest eat
// tick of every account.
func LagEscape(d *Deps, allowWalk bool, label string) EscapeResult {
	if d == nil || d.Session == nil {
		return EscapeIdle
	}
	st := d.Session.State()
	if st == nil {
		return EscapeIdle
	}
	hp, okHP := st.HP()
	maxHP, okMax := st.MaxHP()
	if !okHP || !okMax || hp <= 0 || maxHP <= 0 || hp > (maxHP*fleetplan.EscapeCapPercent+99)/100 {
		return EscapeIdle
	}
	if here := CurrentTile(d); here != nil && escapeNotHere(*here) {
		return EscapeIdle
	}
	vl := st.ViewLag()
	late, delay := false, time.Duration(0)
	if bl, ok := actions.LastBiteLag(st); ok && time.Since(bl.At) <= fleetplan.EscapeBiteFresh {
		late, delay = true, bl.Took
	}
	if age, ok := actions.BiteInFlightAt(st, time.Now()); ok && age >= actions.BiteLagAfter {
		late, delay = true, max(delay, age)
	}
	if vl.Recent.Ticks <= 0 && !vl.Lagging && !late {
		return EscapeIdle
	}
	r := ScanReach(d)
	// On a safespot pen's ground that no hunter holds there is nothing to leave: the herd lets go of a target that stands on ground it does
	// not hold (spotreturn.go), and a hit still in the ten-tick window was landed before the account got here. A teleport out of it would
	// burn a Law rune on an account that had already escaped; it eats where it stands.
	if len(r.Pens) > 0 && !r.HeldHere {
		return EscapeIdle
	}
	hits, unseen := r.Hits, false
	threatened := r.Threatened()
	if threatened && len(hits) == 0 {
		hits, unseen = []int{fighterfood.LagUnseenHit}, true
	}
	v := fleetplan.DecideEscape(fleetplan.EscapeInput{
		HP: hp, MaxHP: maxHP, Known: true, Hits: hits, Unseen: unseen, Threatened: threatened,
		LagTicks: vl.Recent.Ticks, ViewLagging: vl.Lagging, BiteLate: late, BiteDelay: delay,
	})
	if !v.Go {
		return EscapeIdle
	}
	SayEscape(d, "go", "lag escape [%s]: %s (facing it: %d; near it: %v; view lag %d ticks, bites %s late)", label, v.Why, r.Facing, r.Names, vl.Recent.Ticks, delay.Round(100*time.Millisecond))

	// 1. A teleport it can pay for.
	if spell := randomevents.PayableTeleport(d.Session); spell != "" {
		if randomevents.EscapeTeleport(d.Session, "lag escape ["+label+"]", escapeTeleportGap, d.Log) {
			actions.NoteDelayTicks(d.Session, 3) // the cast holds the player: an eat sent in its ticks would be dropped
			return EscapeTeleported
		}
	}
	// 2. A walk out of reach, on a current view and where the caller lets it.
	switch {
	case !allowWalk:
		SayEscape(d, "nowalk", "lag escape [%s]: no teleport it can pay for, and %s does not let the account walk here: eating where it stands", label, label)
		return EscapeHeld
	case vl.Lagging:
		SayEscape(d, "viewlag", "lag escape [%s]: no teleport it can pay for, and the view is lagging (%s): a click from a tile that old walks the account back; eating where it stands", label, vl.Why())
		return EscapeHeld
	}
	here := CurrentTile(d)
	if here == nil {
		return EscapeHeld
	}
	if len(r.Pens) > 0 {
		return escapePenWalk(d, *here, r, label)
	}
	SayEscape(d, "flee", "lag escape [%s]: no teleport it can pay for: the guardian's walking flee from %s", label, HereIs(d))
	if askFlee(d) {
		return EscapeWalked
	}
	return EscapeHeld
}
