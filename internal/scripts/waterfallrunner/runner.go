// Package waterfallrunner is the live half of the Waterfall Quest on the ONE quest engine: the
// custom steps waterfallplan.StepFor names, registered on a questrunner.Quest. Every generic leg
// (Almera, the bookcase, the book, the Tree Gnome Village dungeon, Golrie, the tombstone, the
// coffin, the keyed doors, the chalice, the kits) runs on the engine's own step executors; what
// is here needs an exact stand, a forced dialogue, a wait for the scene, or the pillar ledger.
package waterfallrunner

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	wf "rsb0t-go/internal/scripts/waterfallplan"
)

// Quest is the Waterfall Quest ready for questrunner.Engine.Run or RunLadder.
func Quest(set wf.Settings) questrunner.Quest {
	set = set.Normalised()
	r := &runner{set: set}
	return questrunner.Quest{
		Module: wf.Module(set),
		Finish: r.reequip,
		Customs: map[string]questrunner.CustomFunc{
			wf.CustomStrip:     r.strip,
			wf.CustomRaft:      r.raft,
			wf.CustomHudon:     r.hudon,
			wf.CustomSwimOff:   r.swimOff,
			wf.CustomChest:     r.chest,
			wf.CustomRopeRock:  r.ropeRock,
			wf.CustomRopeTree:  r.ropeTree,
			wf.CustomBarrel:    r.barrel,
			wf.CustomLedgeDoor: r.ledgeDoor,
			wf.CustomBaxCrate:  r.baxCrate,
			wf.CustomLaneIn:    r.laneIn,
			wf.CustomLaneOut:   r.laneOut,
			wf.CustomPillars:   r.pillars,
			wf.CustomFreeSlots: r.freeSlots,
		},
	}
}

type runner struct {
	set wf.Settings
}

func logf(d *questrunner.Deps, f string, a ...any) {
	if d.Log != nil {
		d.Log("[waterfall] " + fmt.Sprintf(f, a...))
	}
}

func here(d *questrunner.Deps) (tile.Tile, bool) {
	x, z, l, ok := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: l}, ok
}

func area(d *questrunner.Deps) wf.Area {
	t, ok := here(d)
	return wf.AreaOf(t, ok)
}

func waitArea(d *questrunner.Deps, timeout time.Duration, want func(wf.Area) bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if want(area(d)) {
			return true
		}
		if !d.Delay(200 * time.Millisecond) {
			return false
		}
	}
	return want(area(d))
}

func is(a wf.Area) func(wf.Area) bool { return func(b wf.Area) bool { return a == b } }

// findLoc finds a loc by exact id near a tile.
func findLoc(d *questrunner.Deps, id int, near tile.Tile, radius int) (locindex.Match, bool) {
	return questrunner.FindLoc(d, id, "", "", near, radius)
}

// opLocFrom sends op on the loc from wherever the account stands (the aploc triggers here run
// at range and the engine checks the zone itself).
func opLocFrom(d *questrunner.Deps, m locindex.Match, op string) error {
	typ, ok := d.Config.Loc(m.ID)
	if !ok {
		return fmt.Errorf("no config for loc %d", m.ID)
	}
	n := 0
	for i, o := range typ.Ops {
		if strings.EqualFold(o, op) {
			n = i + 1
		}
	}
	if n == 0 {
		return fmt.Errorf("loc %d has no %q op (%v)", m.ID, op, typ.Ops)
	}
	questrunner.ClearLeftoverModal(d)
	return actions.OpLoc(d.Session, n, m.X, m.Z, m.ID)
}

// useFrom uses a held item on a loc from wherever the account stands.
func useFrom(d *questrunner.Deps, item int, m locindex.Match) error {
	if !questrunner.PackVisible(d) {
		return fmt.Errorf("the backpack is not visible to the server")
	}
	questrunner.ClearLeftoverModal(d)
	return actions.NewEquipment(d.Session, d.Components).UseOnLoc(int32(item), m.X, m.Z, m.ID)
}

// healToFull eats until hitpoints are at the maximum or the food runs out, and reports whether
// the account is at full health.
// healUnhit eats towards full health only while nothing is attacking the account: bread heals
// four, and the eighth EMPTY lab run ate fourteen of them in place at the Baxtorian crate while
// skeletons hit harder than that, and died. Under attack the right move is to leave.
func (r *runner) healUnhit(d *questrunner.Deps) {
	for i := 0; i < 28; i++ {
		if _, hit := questrunner.Attacker(d); hit {
			return
		}
		hp, max := questrunner.Hitpoints(d)
		if max == 0 || hp >= max {
			return
		}
		if !questrunner.EatIfNeeded(d, wf.Foods, max-1) {
			return
		}
	}
}

// baxCrate searches the Baxtorian crate from its west stand, the TS's tile: the south one
// touches the level-45 skeleton at 2589,9886.
func (r *runner) baxCrate(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.HeldID(d, wf.ItemBaxKey) > 0 {
		return true
	}
	_ = actions.SetRun(d.Session, true)
	if !questrunner.Travel(d, wf.BaxCrateStand, 0) {
		return false
	}
	r.healUnhit(d)
	m, ok := findLoc(d, wf.LocBaxCrate, wf.BaxCrateTile, 2)
	if !ok {
		logf(d, "no Baxtorian crate loaded")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		logf(d, "search the crate: %v", err)
		return false
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if questrunner.HeldID(d, wf.ItemBaxKey) > 0 {
			return true
		}
		d.Delay(200 * time.Millisecond)
	}
	return false
}

func (r *runner) healToFull(d *questrunner.Deps) bool {
	for i := 0; i < 28; i++ {
		hp, max := questrunner.Hitpoints(d)
		if max == 0 || hp >= max {
			return true
		}
		if !questrunner.EatIfNeeded(d, wf.Foods, max-1) {
			return false
		}
	}
	hp, max := questrunner.Hitpoints(d)
	return hp >= max
}

// ---------------------------------------------------------------------------
// Bank-side
// ---------------------------------------------------------------------------

// strip goes to the bank, takes everything worn off and banks whatever the tomb refuses (or,
// for the dungeon trip, only takes the amulet off so it travels in the pack). It banks the run
// staller's kit too (stripKit): a Ring of dueling or a rune in the pack fails TombSafe as surely
// as a sword does.
func (r *runner) strip(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, wf.ArdougneBank, 4) {
		return false
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	questrunner.PackVisible(d)
	var worn []int
	for _, it := range eq.Worn() {
		if it.ID > 0 {
			worn = append(worn, int(it.ID))
		}
	}
	rememberWorn(e, worn)
	questrunner.StripWorn(d)
	bank, ok := questrunner.OpenBankAt(d, wf.ArdougneBank)
	if !ok {
		return false
	}
	stripped := stripKit(d, bank)
	e.Provisioner().Bank.Note(questrunner.BankCounts(bank))
	_ = bank.Close()
	return stripped && len(questrunner.WornCounts(d)) == 0
}

// rememberWorn records what the account wears, in the quest's store, before the strip takes it off: the quest's Finish
// (reequip) puts it back on, the tomb refusing it. A Ghostspeak amulet the Restless Ghost left worn is one of them; if the
// quest never finishes, Nature Spirit withdraws and wears the amulet itself at its stage 70.
func rememberWorn(e *questrunner.Engine, worn []int) {
	if e.Store == nil || len(worn) == 0 {
		return
	}
	rec := e.Store.Record(wf.QuestID)
	if rec.Hints == nil {
		rec.Hints = map[string]bool{}
	}
	for _, id := range worn {
		rec.Hints[fmt.Sprintf("%s%d", wornHint, id)] = true
	}
	if e.Save != nil {
		e.Save()
	}
}

// tombAllowed says whether a pack item may stay in the pack through the strip: what the tomb lets in and food.
func tombAllowed(id int) bool {
	ok, _ := wf.TombSafe(map[int]int{id: 1}, nil, wf.IsFood)
	return ok
}

// stripKit banks every pack item the tomb refuses at the open bank, the run staller's kit included, and reports whether the
// pack is tomb-safe afterwards. It is a bare deposit (questrunner.DepositBare): through DepositExcept the staller's runes
// and Ring of dueling (Deps.KeepInPack) stayed in the pack and Deps.AtBank drew the ring back out behind the deposit, so
// the next decide saw a pack TombSafe refused and sent the account to strip again until the quest parked (live 14-15 Oct
// 2026: 238 wf-strip parks, no Waterfall completion). The kit is drawn again by the first ordinary deposit after the tomb.
func stripKit(d *questrunner.Deps, bank *actions.Bank) bool {
	questrunner.DepositBare(d, bank, tombAllowed)
	ok, why := wf.TombSafe(questrunner.PackCounts(d), questrunner.WornCounts(d), wf.IsFood)
	if !ok {
		logf(d, "the strip left the pack not tomb-safe: %s", why)
	}
	return ok
}

// wornHint prefixes the store hints that name gear the tomb strip took off.
const wornHint = "worn:"

// wornIDs are the object ids the store's hints say the tomb strip took off, sorted.
func wornIDs(hints map[string]bool) []int {
	var ids []int
	for k, on := range hints {
		var id int
		if on && strings.HasPrefix(k, wornHint) {
			if _, err := fmt.Sscanf(strings.TrimPrefix(k, wornHint), "%d", &id); err == nil && id > 0 {
				ids = append(ids, id)
			}
		}
	}
	sort.Ints(ids)
	return ids
}

// reequip withdraws and wears again the gear the tomb strip banked, once the quest is complete.
func (r *runner) reequip(e *questrunner.Engine) {
	if e.Store == nil {
		return
	}
	d := e.Deps()
	rec := e.Store.Record(wf.QuestID)
	ids := wornIDs(rec.Hints)
	if len(ids) == 0 {
		return
	}
	logf(d, "putting back on the %d item(s) the tomb strip banked", len(ids))
	bank := questplan.NearestBank(questrunner.CurrentTile(d))
	b, ok := questrunner.OpenBankAt(d, bank.Tile)
	if !ok {
		logf(d, "re-equip: no bank reached; the gear stays banked")
		return
	}
	banked := questrunner.BankCounts(b)
	for _, id := range ids {
		if questrunner.HeldID(d, id) == 0 {
			n := 1
			if o, ok := d.Config.Obj(id); ok && o.Stackable && banked[id] > 1 {
				n = banked[id] // a worn ammo stack comes back whole, not as one arrow
			}
			questrunner.WithdrawExact(d, b, id, n)
		}
	}
	_ = b.Close()
	for _, id := range ids {
		hint := fmt.Sprintf("%s%d", wornHint, id)
		if questrunner.HeldID(d, id) > 0 && e.Execute(questplan.Step{Kind: questplan.StepEquip, ItemID: id, Item: questrunner.ObjName(d, id)}) {
			delete(rec.Hints, hint)
		} else if questrunner.HeldID(d, id) == 0 && banked[id] == 0 {
			// Neither carried nor banked: the quest used it up (Glarial's amulet goes to the
			// statue). Forget it, or every later Finish walks to a bank for it again.
			delete(rec.Hints, hint)
		}
	}
	if e.Save != nil {
		e.Save()
	}
}

// ---------------------------------------------------------------------------
// The river
// ---------------------------------------------------------------------------

// raft boards the log raft and, at stage 1, drives the forced Hudon conversation that sets
// stage 2 mid-way (quest_waterfall.rs2 [oploc1,lograft_waterfall_quest]).
func (r *runner) raft(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if a := area(d); a == wf.AreaMound {
		return true
	}
	if !questrunner.Travel(d, wf.RaftStand, 1) {
		return false
	}
	m, ok := findLoc(d, wf.LocRaft, wf.RaftStand, 6)
	if !ok {
		logf(d, "no log raft near %v", wf.RaftStand)
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Board"); err != nil {
		logf(d, "board the raft: %v", err)
		return false
	}
	if !waitArea(d, 15*time.Second, is(wf.AreaMound)) {
		logf(d, "the raft did not land on Hudon's mound")
		return false
	}
	dl := questrunner.NewDialogue(d)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) && !dl.Ready() {
		d.Delay(200 * time.Millisecond)
	}
	for page := 0; page < 16 && dl.Ready(); page++ {
		if !dl.Drive([]string{"Maybe I could help"}) {
			break
		}
	}
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return true
}

// hudon talks to Hudon from inside his zone (he only answers there: hudon.rs2 inzone check).
func (r *runner) hudon(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	questrunner.WalkNear(d, wf.HudonStand, 0)
	stop := questplan.NpcStop{Npc: "Hudon", ID: wf.NpcHudon, Anchor: wf.HudonStand, Leash: 6, Prefer: []string{"Maybe I could help"}}
	if !questrunner.OpenDialogue(d, stop) {
		return false
	}
	questrunner.NewDialogue(d).Drive(stop.Prefer)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return true
}

// swimOff swims from the mound: a no-damage washout to 2527,3413 ([aploc1,crossing_rock]
// requires standing in the Hudon zone north of the rock).
func (r *runner) swimOff(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.WalkNear(d, wf.SwimStand, 0) {
		return false
	}
	m, ok := findLoc(d, wf.LocRock, wf.RockTile, 2)
	if !ok {
		if m, ok = findLoc(d, wf.LocRockRoped, wf.RockTile, 2); !ok {
			logf(d, "no crossing rock loaded")
			return false
		}
	}
	if err := opLocFrom(d, m, "Swim to"); err != nil {
		logf(d, "swim: %v", err)
		return false
	}
	return waitArea(d, 20*time.Second, is(wf.AreaMainland))
}

// ropeRock throws the rope onto the rock and wades to the tree bank ([aplocu,crossing_rock]).
func (r *runner) ropeRock(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.WalkNear(d, wf.SwimStand, 0) {
		return false
	}
	m, ok := findLoc(d, wf.LocRock, wf.RockTile, 2)
	if !ok {
		logf(d, "no crossing rock loaded (another account's rope may be on it)")
		d.Delay(3 * time.Second)
		return false
	}
	if err := useFrom(d, wf.ItemRope, m); err != nil {
		logf(d, "rope on the rock: %v", err)
		return false
	}
	return waitArea(d, 12*time.Second, is(wf.AreaTreeBank))
}

// ropeTree ties the rope to the dead tree and lowers the account onto the ledge. Op1 on the
// tree is the 8-damage trap, so only the rope is ever used on it.
func (r *runner) ropeTree(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	questrunner.WalkNear(d, wf.DeadTreeStand, 0)
	m, ok := findLoc(d, wf.LocDeadTree, wf.DeadTreeTile, 2)
	if !ok {
		logf(d, "no dead tree loaded")
		return false
	}
	if err := useFrom(d, wf.ItemRope, m); err != nil {
		logf(d, "rope on the tree: %v", err)
		return false
	}
	return waitArea(d, 10*time.Second, is(wf.AreaLedge))
}

// barrel is the ledge's no-damage washout.
func (r *runner) barrel(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	m, ok := findLoc(d, wf.LocBarrel, wf.BarrelTile, 3)
	if !ok {
		logf(d, "no escape barrel on the ledge")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Get in"); err != nil {
		logf(d, "barrel: %v", err)
		return false
	}
	return waitArea(d, 15*time.Second, is(wf.AreaMainland))
}

// ledgeDoor enters the dungeon at full health with run on. The amulet (carried) is what keeps
// the corridor from flooding; decide() never sends this leg without it.
func (r *runner) ledgeDoor(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !r.healToFull(d) {
		logf(d, "entering the dungeon below full health: no food left to eat")
	}
	_ = actions.SetRun(d.Session, true)
	m, ok := findLoc(d, wf.LocLedgeDoor, wf.LedgeDoorTile, 3)
	if !ok {
		logf(d, "no ledge door loaded")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
		logf(d, "ledge door: %v", err)
		return false
	}
	ok = waitArea(d, 15*time.Second, func(a wf.Area) bool { return a.InWaterfallDungeon() })
	e.InvalidateStage()
	return ok
}

// chest opens the tomb chest if it is shut, then searches it for the amulet.
func (r *runner) chest(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if m, ok := findLoc(d, wf.LocChestClosed, wf.ChestTile, 3); ok {
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			logf(d, "open the chest: %v", err)
			return false
		}
		d.Delay(1800 * time.Millisecond)
	}
	m, ok := findLoc(d, wf.LocChestOpen, wf.ChestTile, 3)
	if !ok {
		logf(d, "the tomb chest did not open")
		return false
	}
	before := questrunner.HeldID(d, wf.ItemAmulet)
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		logf(d, "search the chest: %v", err)
		return false
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if questrunner.HeldID(d, wf.ItemAmulet) > before {
			return true
		}
		d.Delay(200 * time.Millisecond)
	}
	return false
}

// ---------------------------------------------------------------------------
// The fire-giant lane
// ---------------------------------------------------------------------------

// laneBusy reports whether a fire giant stands within two tiles of the lane's walk line.
func laneBusy(d *questrunner.Deps) (bool, string) {
	giants := entityquery.Npcs(d.Session.State(), d.Config).Within(30).
		Where(func(n entityquery.Entity) bool { return n.ID == wf.NpcFireGnt }).Results()
	for _, g := range giants {
		gt := tile.Tile{X: g.X, Z: g.Z}
		for z := wf.LaneGate.Z + 1; z <= wf.SouthDoorSouth.Z; z++ {
			if tile.Chebyshev(gt, tile.Tile{X: 2566, Z: z}) <= 2 {
				return true, fmt.Sprintf("a fire giant at %d,%d", g.X, g.Z)
			}
		}
	}
	return false, ""
}

// laneIn waits south of the lane for the fire giants to wander off (bounded), then runs the
// lane and uses the key on the south door at once. An account already in the lane (a retry)
// goes straight to the door: the tenth EMPTY lab run, its first key use dropped, walked back
// through the Large door into the spider room to "wait" again and died between the two.
func (r *runner) laneIn(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	_ = actions.SetRun(d.Session, true)
	if t, ok := here(d); !ok || !wf.InLane(t) {
		// Heal before the spider room, not in it: the fourth Waterfall EMPTY lab run arrived at
		// the lane door at 14/20 and died waiting there with the shadow spiders on it. But never
		// in place under attack (the eighth died eating at the crate): then the move is to go.
		r.healUnhit(d)
		if !questrunner.Travel(d, wf.LaneWait, 1) {
			return false
		}
		deadline := time.Now().Add(time.Duration(r.set.LaneWaitSeconds) * time.Second)
		for time.Now().Before(deadline) {
			busy, why := laneBusy(d)
			if !busy {
				break
			}
			// The wait is south of the door, in the shadow-spider room: being hit there is worse
			// than a fire giant near the lane, so a spider on the account ends the wait.
			if npc, hit := questrunner.Attacker(d); hit {
				logf(d, "%s is on the account at the lane door: running the lane now", npc.Name)
				break
			}
			if hp, max := questrunner.Hitpoints(d); max > 0 && hp*4 < max*3 {
				logf(d, "hitpoints %d/%d at the lane door: running the lane now", hp, max)
				break
			}
			logf(d, "waiting at the lane: %s", why)
			d.Delay(1200 * time.Millisecond)
		}
		// Travel, not a bare approach: the Large door into the lane shuts behind a crossing and
		// only the walker's door hops open it.
		if !questrunner.Travel(d, wf.SouthDoorSouth, 0) {
			return false
		}
	}
	return keyThrough(r, d, wf.SouthDoorTile, func(t tile.Tile) bool { return t.Z >= wf.SouthDoorNorth.Z })
}

// keyThrough uses the Baxtorian key on the keyed door at door until through holds. Each use
// waits out a bite first: eating delays the player, and a use sent inside the delay is dropped
// without a word (the tenth EMPTY lab run lost its first south-door use to exactly that). A
// silent use is sent again, up to three times.
func keyThrough(r *runner, d *questrunner.Deps, door tile.Tile, through func(tile.Tile) bool) bool {
	m, ok := findLoc(d, wf.LocKeyedDoor, door, 2)
	if !ok {
		logf(d, "no keyed door loaded at %d,%d", door.X, door.Z)
		return false
	}
	for try := 0; try < 3; try++ {
		if !questrunner.ApproachLoc(d, m) {
			return false
		}
		if questrunner.EatIfNeeded(d, wf.Foods, r.set.EatAtDungeon) {
			d.Delay(1300 * time.Millisecond)
		}
		if err := useFrom(d, wf.ItemBaxKey, m); err != nil {
			logf(d, "key on the door at %d,%d: %v", door.X, door.Z, err)
			return false
		}
		if waitUntilTile(d, 4*time.Second, through) {
			return true
		}
		logf(d, "the key on the door at %d,%d did nothing: again", door.X, door.Z)
	}
	return false
}

func waitUntilTile(d *questrunner.Deps, timeout time.Duration, cond func(tile.Tile) bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if t, ok := here(d); ok && cond(t) {
			return true
		}
		d.Delay(200 * time.Millisecond)
	}
	return false
}

func waitNorth(d *questrunner.Deps, z int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if t, ok := here(d); ok && t.Z >= z {
			return true
		}
		d.Delay(200 * time.Millisecond)
	}
	return false
}

// laneOut crosses the lane southwards: from the corridor the key opens the south door, then
// the lane is run back to the spider room.
func (r *runner) laneOut(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if t, ok := here(d); ok && t.Z >= wf.SouthDoorNorth.Z && area(d) == wf.AreaCorridor {
		// The corridor is the one quiet place on this side: heal to full there and let the lane
		// clear before stepping into it. The eleventh EMPTY lab run completed the quest, stepped
		// out at 16/20 with a fire giant beside the Large door, drew a random event in the lane,
		// and died holding the reward.
		r.healUnhit(d)
		deadline := time.Now().Add(time.Duration(r.set.LaneWaitSeconds) * time.Second)
		for time.Now().Before(deadline) {
			busy, why := laneBusy(d)
			if !busy {
				break
			}
			logf(d, "waiting in the corridor: %s", why)
			d.Delay(1200 * time.Millisecond)
		}
		if !keyThrough(r, d, wf.SouthDoorTile, func(t tile.Tile) bool { return t.Z < wf.SouthDoorNorth.Z }) {
			return false
		}
	}
	_ = actions.SetRun(d.Session, true)
	return questrunner.Travel(d, wf.LaneWait, 1)
}

// ---------------------------------------------------------------------------
// The pillars and the statue
// ---------------------------------------------------------------------------

// pillars places every (pillar, rune) pair, acknowledging each from the server's own words,
// and uses the amulet on the statue ONLY when all 18 were acknowledged in this visit. Any other
// statue use drops a 20-damage boulder, which kills a 20-hitpoint account at full health.
func (r *runner) pillars(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	var ledger wf.PillarLedger
	for {
		p, k, ok := ledger.Next()
		if !ok {
			break
		}
		rune := wf.PuzzleRunes[k]
		if questrunner.HeldID(d, rune) == 0 {
			logf(d, "no %s left for pillar %s: %s", wf.RuneName(rune), wf.PillarNames[p], ledger)
			return false
		}
		m, found := findLoc(d, wf.LocPillar, wf.Pillars[p], 1)
		if !found {
			logf(d, "pillar %s is not loaded", wf.PillarNames[p])
			return false
		}
		questrunner.WalkNear(d, wf.PillarStand(p), 0)
		// A placement delays the player a tick (p_delay(0) between "You place the rune on the
		// stand." and "...puff of smoke."), and a use sent inside that tick is dropped without a
		// word: the ninth EMPTY lab run lost every third use that way. So each use waits out the
		// placement's second message, and a silent use is sent again.
		ack := wf.AckNone
		for try := 0; try < 3 && ack == wf.AckNone; try++ {
			mark := time.Now()
			if err := questrunner.UseItemOnLoc(d, rune, m); err != nil {
				logf(d, "%s on pillar %s: %v", wf.RuneName(rune), wf.PillarNames[p], err)
				return false
			}
			deadline := time.Now().Add(6 * time.Second)
			for time.Now().Before(deadline) && ack == wf.AckNone {
				d.Delay(300 * time.Millisecond)
				if d.Chat != nil {
					ack = wf.ClassifyPillarMessage(d.Chat.Since(mark))
				}
			}
			if ack == wf.AckPlaced {
				settle := time.Now().Add(2 * time.Second)
				for time.Now().Before(settle) && !strings.Contains(strings.ToLower(d.Chat.Since(mark)), "puff of smoke") {
					d.Delay(200 * time.Millisecond)
				}
				d.Delay(600 * time.Millisecond)
			}
		}
		if ack == wf.AckNone {
			logf(d, "%s on pillar %s was not acknowledged: leaving the statue alone (%s)", wf.RuneName(rune), wf.PillarNames[p], ledger)
			return false
		}
		ledger.Ack(p, k, ack)
	}
	if !ledger.Complete() {
		return false
	}
	logf(d, "all 18 pillar placements acknowledged this visit; the amulet goes on the statue")
	m, found := findLoc(d, wf.LocStatue, wf.StatueTile, 2)
	if !found {
		logf(d, "no statue of Glarial loaded")
		return false
	}
	questrunner.WalkNear(d, wf.StatueStand, 0)
	if err := questrunner.UseItemOnLoc(d, wf.ItemAmulet, m); err != nil {
		logf(d, "amulet on the statue: %v", err)
		return false
	}
	ok := waitArea(d, 20*time.Second, is(wf.AreaRaisedRoom))
	e.InvalidateStage()
	return ok
}

// freeSlots makes free slots by eating, then dropping, food: the step's Qty of them (the
// chalice's five for the reward, one for a key, the book, the amulet or the urn). It eats the
// food whose one bite frees its slot soonest (wf.SlotFood), so a pack of cakes is not bitten
// three times per slot.
func (r *runner) freeSlots(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	need := step.Qty
	if need <= 0 {
		need = wf.RewardSlots
	}
	for i := 0; i < 20 && questrunner.FreeSlots(d) < need; i++ {
		food := wf.SlotFood(questrunner.PackCounts(d))
		if food == 0 {
			logf(d, "fewer than %d free slots and no food to eat or drop", need)
			return false
		}
		hp, max := questrunner.Hitpoints(d)
		op := "Drop"
		if hp < max {
			op = "Eat"
		}
		if err := questrunner.OpHeldByName(d, food, op); err != nil {
			logf(d, "%s %s: %v", op, questrunner.ObjName(d, food), err)
			return false
		}
		d.Delay(900 * time.Millisecond)
	}
	return questrunner.FreeSlots(d) >= need
}
