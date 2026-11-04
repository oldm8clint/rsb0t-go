package questplan

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// Sheep Herder (W13, 19 Oct 2026): the journal parser over the pages the content renders, every state of Decide, the kits, the module's
// identity and the push loop against small synthetic grounds. The loop against the real collision pack is questmods'
// (sheepherder_sim_test.go); the ids, tiles, menus and scripts the plan rests on are checked against the content in
// sheepherder_content_test.go.

// ---------------------------------------------------------------------------------------------------------------------
// The journal
// ---------------------------------------------------------------------------------------------------------------------

const (
	herdHead = "@str@Councillor Halgrive asked me to dispose of four|@str@plague-bearing sheep just north of Ardougne|@str@and I accepted.|" +
		"@str@He gave me some poisoned sheep feed to do this.|"
	herdBought = "|@str@I bought some protective clothing from Dr Orbon in the|@str@chapel north of Ardougne Zoo. I could now kill the sheep.|"
	herdProdLn = "|@str@I equipped a prod and then I used it to herd the diseased|@str@sheep to a pen where I could safely kill them and|@str@incinerate their bones.|"
)

// herdSheepLine is the page's line for sheep n (0 to 3) in state st (sheepherder_journal.rs2).
func herdSheepLine(n, st int) string {
	ord := herdOrdinals[n]
	switch st {
	case HerdSheepBurnt:
		return "|@str@I have killed the " + ord + " sheep and incinerated its bones.|"
	case HerdSheepKilled:
		return "|@dbl@I have killed the " + ord + " sheep. Now I must incinerate its @dre@bones@dbl@.|"
	case HerdSheepPenned:
		return "|@dbl@I have herded the " + ord + " sheep to the pen. Now I must kill it safely.|"
	}
	return "|@dbl@I must find the " + ord + " sheep and herd it to the pen.|"
}

// herdPage is the whole page for the four sheep in the given states, as the script renders it.
func herdPage(states [4]int) string {
	if states == [4]int{3, 3, 3, 3} {
		return herdHead + herdBought + herdProdLn + "|@dbl@I should return to @dre@Councillor Halgrive @dbl@to collect the reward he has promised me for my hard work."
	}
	page := herdHead + herdBought
	for n, st := range states {
		page += herdSheepLine(n, st)
	}
	return page
}

func TestSheepHerderJournalPages(t *testing.T) {
	for _, c := range []struct {
		name string
		page string
		want int
	}{
		{"not started", "@dbl@I can start this quest by speaking to @dre@Councillor Halgrive @dbl@near to the @dre@Zoo @dbl@in @dre@East Ardougne@dbl@.", HerdNotStarted},
		{"need the suit", herdHead + "|@dbl@I need to get some protective clothing from @dre@Dr Orbon @dbl@who is in the church just north of @dre@Ardougne Zoo", HerdNeedSuit},
		{"nothing done", herdPage([4]int{}), HerdDisposing},
		{"first herded", herdPage([4]int{1}), HerdDisposing + 1},
		{"first killed", herdPage([4]int{2}), HerdDisposing + 2},
		{"first burnt", herdPage([4]int{3}), HerdDisposing + 3},
		{"mixed", herdPage([4]int{3, 2, 1, 0}), HerdDisposing + 3 + 2<<2 + 1<<4},
		{"last two", herdPage([4]int{3, 3, 2, 1}), HerdDisposing + 3 + 3<<2 + 2<<4 + 1<<6},
		{"all burnt", herdPage([4]int{3, 3, 3, 3}), HerdAllBurnt},
		{"complete", herdHead + herdBought + herdProdLn + "|@str@I returned to let Councillor Halgrive know that the plagued|@str@sheep were no more and claimed my reward.|@red@QUEST COMPLETE!", HerdComplete},
	} {
		got, ok := ParseSheepHerderJournal(strings.Split(c.page, "|"))
		if !ok || got != c.want {
			t.Errorf("%s: stage %d (ok %v), want %d", c.name, got, ok, c.want)
		}
	}
	// A page the module has no needle for is unknown, never stage 0 (which would restart a started quest).
	for _, page := range []string{"", "@dbl@Some other quest's page", "I bought something else"} {
		if got, ok := ParseSheepHerderJournal([]string{page}); ok {
			t.Errorf("%q parsed as stage %d", page, got)
		}
	}
	// Every combination of the four sheep's states round-trips through the page and the stage's two bits each.
	for code := 0; code < 256; code++ {
		var states [4]int
		for i := range states {
			states[i] = code >> (2 * i) & 3
		}
		stage, ok := ParseSheepHerderJournal(strings.Split(herdPage(states), "|"))
		if !ok || stage != HerdStageWith(states) {
			t.Fatalf("states %v: stage %d (ok %v), want %d", states, stage, ok, HerdStageWith(states))
		}
		for n := 1; n <= 4; n++ {
			if got := HerdSheepState(stage, n); got != states[n-1] {
				t.Errorf("states %v: sheep %d reads %d", states, n, got)
			}
		}
	}
}

func TestSheepHerderStageHelpers(t *testing.T) {
	if HerdSheepState(HerdNeedSuit, 1) != HerdSheepWild || HerdSheepState(HerdComplete, 2) != HerdSheepWild || HerdSheepState(HerdDisposing, 5) != HerdSheepWild {
		t.Error("a stage outside the disposing range, or a sheep outside 1 to 4, reads a state")
	}
	if HerdNextSheep(HerdDisposing) != 1 || HerdNextSheep(HerdStageWith([4]int{3, 3, 0, 0})) != 3 || HerdNextSheep(HerdStageWith([4]int{3, 2, 3, 3})) != 2 || HerdNextSheep(HerdAllBurnt) != 0 {
		t.Error("HerdNextSheep is not the first sheep that is not burnt")
	}
	for n := 1; n <= 4; n++ {
		if NpcPlagueSheep(n) != 291+n || ObjSheepBones(n) != 279+n {
			t.Errorf("sheep %d: npc %d, remains %d", n, NpcPlagueSheep(n), ObjSheepBones(n))
		}
	}
	if !InHerdPen(HerdGateInside) || InHerdPen(HerdGateOutside) {
		t.Error("the pen's west edge is the tile east of the gate's own tile")
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Decide
// ---------------------------------------------------------------------------------------------------------------------

func herdSnap(stage int, inv map[int]int, worn ...int) Snapshot {
	s := Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: 20,
		Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{}, TileKnown: true, Tile: HalgriveAnchor}
	for id, n := range inv {
		s.InvIDs[id] = n
	}
	for _, id := range worn {
		s.WornIDs[id] = 1
	}
	return s
}

// herdGeared is an account with everything on and in hand but the sheep done.
func herdGeared(stage int, extra map[int]int) Snapshot {
	inv := map[int]int{ObjSheepFeed: 1}
	for id, n := range extra {
		inv[id] = n
	}
	return herdSnap(stage, inv, ObjPlagueJacket, ObjPlagueTrousers, ObjCattleProd)
}

func TestDecideSheepHerder(t *testing.T) {
	talks := func(st Step, npc string) bool { return st.Kind == StepTalk && st.Stop.Npc == npc }
	custom := func(st Step, name string, n int) bool {
		return st.Kind == StepCustom && st.Custom == name && st.Qty == n
	}

	// The tab.
	if st := DecideSheepHerder(Snapshot{Journal: StatusComplete}); st.Kind != StepDone {
		t.Errorf("complete: %v", st)
	}
	if st := DecideSheepHerder(Snapshot{Journal: StatusUnknown}); st.Kind != StepWait || st.ParkFor != 0 {
		t.Errorf("unknown tab: %v", st)
	}
	if st := DecideSheepHerder(Snapshot{Journal: StatusNotStarted}); !talks(st, "Councillor Halgrive") {
		t.Errorf("not started: %v", st)
	}
	if st := DecideSheepHerder(Snapshot{Journal: StatusInProgress}); st.Kind != StepWait {
		t.Errorf("a started quest whose stage was not read: %v", st)
	}
	if st := DecideSheepHerder(herdSnap(HerdNotStarted, nil)); !talks(st, "Councillor Halgrive") {
		t.Errorf("stage 0: %v", st)
	}
	if st := DecideSheepHerder(herdSnap(HerdComplete, nil)); st.Kind != StepDone {
		t.Errorf("stage complete: %v", st)
	}
	if st := DecideSheepHerder(herdSnap(HerdAllBurnt+1, nil)); st.Kind != StepWait {
		t.Errorf("a stage nothing names: %v", st)
	}

	// Stage 1: the coins, then Doctor Orbon.
	if st := DecideSheepHerder(herdSnap(HerdNeedSuit, nil)); st.Kind != StepProvision || st.KitName != KitHerdCoins || st.Kit[0].ID != CoinsID || st.Kit[0].Qty != 100 {
		t.Errorf("stage 1 without coins: %v", st)
	}
	if st := DecideSheepHerder(herdSnap(HerdNeedSuit, map[int]int{CoinsID: 99})); st.Kind != StepProvision {
		t.Errorf("99 coins: %v", st)
	}
	if st := DecideSheepHerder(herdSnap(HerdNeedSuit, map[int]int{CoinsID: 100})); !talks(st, "Doctor Orbon") || st.Stop.Prefer[0] != "Ok, I'll take it." {
		t.Errorf("100 coins: %v", st)
	}
	blocked := herdSnap(HerdNeedSuit, nil)
	blocked.Blocked["provision:"+KitHerdCoins] = true
	if st := DecideSheepHerder(blocked); st.Kind != StepWait || st.ParkFor != HerdCoinsPark || !strings.Contains(st.Reason, "100 coins") {
		t.Errorf("coins blocked: %v", st)
	}

	// Stage 2, nothing in hand: the bank's gear first (once a run), then the shop.
	bare := herdSnap(HerdDisposing, nil)
	if st := DecideSheepHerder(bare); st.Kind != StepProvision || st.KitName != KitHerdGear {
		t.Fatalf("a bare account at stage 2: %v", st)
	}
	bare.Ran["provision:"+KitHerdGear] = true
	if st := DecideSheepHerder(bare); st.Kind != StepProvision || st.KitName != KitHerdCoins {
		t.Errorf("no suit and no coins: %v", st)
	}
	bare.InvIDs[CoinsID] = 100
	if st := DecideSheepHerder(bare); !talks(st, "Doctor Orbon") {
		t.Errorf("no suit and the coins: %v", st)
	}
	// A failed gear round is not repeated either.
	failed := herdSnap(HerdDisposing, nil)
	failed.Failed["provision:"+KitHerdGear] = true
	failed.InvIDs[CoinsID] = 100
	if st := DecideSheepHerder(failed); !talks(st, "Doctor Orbon") {
		t.Errorf("a failed gear round: %v", st)
	}

	// The suit: in the pack, it is put on behind room for what it replaces; worn, the prod comes next.
	suit := herdSnap(HerdDisposing, map[int]int{ObjPlagueJacket: 1, ObjPlagueTrousers: 1, ObjCattleProd: 1, ObjSheepFeed: 1})
	suit.FreeSlots = HerdEquipRoom - 1
	if st := DecideSheepHerder(suit); st.Kind != StepDeposit || len(st.KeepIDs) == 0 || !slices.Contains(st.KeepIDs, ObjPlagueJacket) || !slices.Contains(st.KeepIDs, ObjSheepFeed) {
		t.Errorf("a full pack: %v", st)
	}
	suit.FreeSlots = HerdEquipRoom
	if st := DecideSheepHerder(suit); st.Kind != StepEquip || st.ItemID != ObjPlagueJacket {
		t.Errorf("the jacket: %v", st)
	}
	suit.WornIDs[ObjPlagueJacket] = 1
	delete(suit.InvIDs, ObjPlagueJacket)
	if st := DecideSheepHerder(suit); st.Kind != StepEquip || st.ItemID != ObjPlagueTrousers {
		t.Errorf("the trousers: %v", st)
	}
	// One half lost: bought again (he sells both), never worn half-dressed into the enclosure.
	half := herdSnap(HerdDisposing, map[int]int{CoinsID: 100, ObjSheepFeed: 1}, ObjPlagueJacket)
	half.Ran["provision:"+KitHerdGear] = true
	if st := DecideSheepHerder(half); !talks(st, "Doctor Orbon") {
		t.Errorf("half a suit: %v", st)
	}

	// The prod: fetched from the barn, then put on; a failed fetch parks.
	noProd := herdSnap(HerdDisposing, map[int]int{ObjSheepFeed: 1}, ObjPlagueJacket, ObjPlagueTrousers)
	noProd.Ran["provision:"+KitHerdGear] = true
	if st := DecideSheepHerder(noProd); !custom(st, CustomHerdProd, 0) {
		t.Errorf("no prod: %v", st)
	}
	noProd.Failed[CustomHerdProd] = true
	if st := DecideSheepHerder(noProd); st.Kind != StepWait || st.ParkFor != HerdLegPark {
		t.Errorf("a failed prod fetch: %v", st)
	}
	prodHeld := herdSnap(HerdDisposing, map[int]int{ObjSheepFeed: 1, ObjCattleProd: 1}, ObjPlagueJacket, ObjPlagueTrousers)
	if st := DecideSheepHerder(prodHeld); st.Kind != StepEquip || st.ItemID != ObjCattleProd {
		t.Errorf("the prod in the pack: %v", st)
	}

	// The feed: Halgrive gives another to a pack with none.
	noFeed := herdSnap(HerdDisposing, nil, ObjPlagueJacket, ObjPlagueTrousers, ObjCattleProd)
	noFeed.Ran["provision:"+KitHerdGear] = true
	if st := DecideSheepHerder(noFeed); !talks(st, "Councillor Halgrive") {
		t.Errorf("no feed: %v", st)
	}

	// The sheep, in order, and the remains first.
	for n := 1; n <= 4; n++ {
		var states [4]int
		for i := 0; i < n-1; i++ {
			states[i] = HerdSheepBurnt
		}
		if st := DecideSheepHerder(herdGeared(HerdStageWith(states), nil)); !custom(st, CustomHerdSheep, n) {
			t.Errorf("sheep %d: %v", n, st)
		}
	}
	penned := herdGeared(HerdStageWith([4]int{1}), nil)
	penned.Ran["provision:"+KitHerdGear] = true
	if st := DecideSheepHerder(penned); !custom(st, CustomHerdSheep, 1) {
		t.Errorf("a herded sheep with no remains in the pack is herded and killed again: %v", st)
	}
	killed := herdGeared(HerdStageWith([4]int{3, 2}), map[int]int{ObjSheepBones(2): 1})
	if st := DecideSheepHerder(killed); !custom(st, CustomHerdBurn, 2) {
		t.Errorf("remains in the pack: %v", st)
	}
	out := herdGeared(HerdStageWith([4]int{0, 2}), map[int]int{ObjSheepBones(2): 1})
	if st := DecideSheepHerder(out); !custom(st, CustomHerdBurn, 2) {
		t.Errorf("remains of a sheep killed out of turn are burnt before sheep 1 is touched: %v", st)
	}
	burnFailed := herdGeared(HerdStageWith([4]int{3, 2}), map[int]int{ObjSheepBones(2): 1})
	burnFailed.Failed[CustomHerdBurn] = true
	if st := DecideSheepHerder(burnFailed); st.Kind != StepWait || st.ParkFor != HerdLegPark {
		t.Errorf("a failed burn: %v", st)
	}
	legFailed := herdGeared(HerdDisposing, nil)
	legFailed.Failed[CustomHerdSheep] = true
	if st := DecideSheepHerder(legFailed); st.Kind != StepWait || st.ParkFor != HerdLegPark || !strings.Contains(st.Reason, "sheep 1") {
		t.Errorf("a failed herd: %v", st)
	}
	// A sheep killed whose remains were banked: the journal reads it as herded, the gear round draws the remains once.
	lost := herdSnap(HerdStageWith([4]int{1}), map[int]int{ObjSheepFeed: 1}, ObjPlagueJacket, ObjPlagueTrousers, ObjCattleProd)
	if st := DecideSheepHerder(lost); st.Kind != StepProvision || st.KitName != KitHerdGear {
		t.Errorf("a herded sheep and no remains: %v", st)
	}
	// All burnt: the reward, with or without the rest of the kit.
	if st := DecideSheepHerder(herdSnap(HerdAllBurnt, nil)); !talks(st, "Councillor Halgrive") {
		t.Errorf("all burnt: %v", st)
	}
}

// Every step Decide can ask for over a sweep of states is well formed, and the talks name a preference the stops carry.
func TestSheepHerderStepsAreWellFormed(t *testing.T) {
	stages := []int{HerdNotStarted, HerdNeedSuit, HerdComplete}
	for code := 0; code < 256; code += 5 {
		stages = append(stages, HerdDisposing+code)
	}
	stages = append(stages, HerdAllBurnt)
	for _, stage := range stages {
		for mask := 0; mask < 1<<7; mask++ {
			inv := map[int]int{}
			var worn []int
			for i, id := range []int{CoinsID, ObjPlagueJacket, ObjPlagueTrousers, ObjCattleProd, ObjSheepFeed, ObjSheepBones(1), ObjSheepBones(2)} {
				if mask>>i&1 == 1 {
					if id == ObjPlagueJacket || id == ObjPlagueTrousers || id == ObjCattleProd {
						worn = append(worn, id)
					} else if id == CoinsID {
						inv[id] = 150
					} else {
						inv[id] = 1
					}
				}
			}
			s := herdSnap(stage, inv, worn...)
			s.FreeSlots = (mask % 3) * 3
			if mask%7 == 0 {
				s.Ran["provision:"+KitHerdGear] = true
			}
			st := DecideSheepHerder(s)
			if st.Kind == StepWait || st.Kind == StepDone {
				continue
			}
			if why := ValidateStep(st); why != "" {
				t.Fatalf("stage %d mask %b: %s (%s)", stage, mask, why, st.Describe())
			}
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The module
// ---------------------------------------------------------------------------------------------------------------------

func TestSheepHerderModuleAndRow(t *testing.T) {
	m := SheepHerder
	if m.Record.ID != "sheepherder" || m.Record.Name != "Sheep Herder" || m.Record.QuestPoints != 4 || len(m.Record.Requirements.Skills) != 0 {
		t.Errorf("record %+v", m.Record)
	}
	if !m.NoRetaliate || m.MaxDeaths != 1 || m.ReadStage == nil || m.ExclusiveDone == nil || len(m.ToolIDs) == 0 || m.Bank != EastArdougneNorth {
		t.Errorf("module %+v", m)
	}
	// The keep list carries every quest item (the between-run dump banks the untradeable ones).
	for _, id := range []int{CoinsID, ObjPlagueJacket, ObjPlagueTrousers, ObjCattleProd, ObjSheepFeed, ObjSheepBones(1), ObjSheepBones(2), ObjSheepBones(3), ObjSheepBones(4)} {
		if !slices.Contains(m.ToolIDs, id) || !slices.Contains(HerdKeepIDs(), id) {
			t.Errorf("item %d is not kept", id)
		}
	}
	// The slot is held until the fourth sheep is burnt.
	for _, c := range []struct {
		s    Snapshot
		want bool
	}{
		{Snapshot{Journal: StatusComplete}, true},
		{Snapshot{Journal: StatusInProgress, Stage: HerdAllBurnt, StageKnown: true}, true},
		{Snapshot{Journal: StatusInProgress, Stage: HerdStageWith([4]int{3, 3, 3, 2}), StageKnown: true}, false},
		{Snapshot{Journal: StatusInProgress, Stage: HerdNeedSuit, StageKnown: true}, false},
		{Snapshot{Journal: StatusInProgress, Stage: HerdAllBurnt}, false},
	} {
		if got := m.ExclusiveDone(c.s); got != c.want {
			t.Errorf("ExclusiveDone(%+v) = %v, want %v", c.s, got, c.want)
		}
	}
	f, ok := LadderFacts[SheepHerderID]
	if !ok || f.ID != SheepHerderID || f.Name != SheepHerderName || f.QP != 4 || !f.Filler || f.Finale || len(f.Gates) != 0 || f.EstMinutes <= 0 {
		t.Errorf("ladder row %+v", f)
	}
	if QuestTabName(SheepHerderID) != "Sheep Herder" {
		t.Errorf("QuestTabName = %q", QuestTabName(SheepHerderID))
	}
	// A row's slot (the estimate and a quarter) is shorter than an hour, well under the 90 minutes a slot holder keeps (questrunner.slotStale).
	if got := QuestSlot(SheepHerderID, 20*time.Minute); got >= time.Hour {
		t.Errorf("slot %s is not under an hour", got)
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The push loop on synthetic ground
// ---------------------------------------------------------------------------------------------------------------------

// openGround is a rectangle of floor with an optional set of blocked tiles; every step between floor tiles is legal.
type openGround struct {
	x0, x1, z0, z1 int
	blocked        map[[2]int]bool
}

func (g openGround) Walkable(x, z int) bool {
	return x >= g.x0 && x <= g.x1 && z >= g.z0 && z <= g.z1 && !g.blocked[[2]int{x, z}]
}

func (g openGround) CanStep(x, z, dx, dz int) bool { return g.Walkable(x, z) && g.Walkable(x+dx, z+dz) }

func TestHerdPlannerOnOpenGround(t *testing.T) {
	g := openGround{x0: 0, x1: 40, z0: 0, z1: 40}
	goal := HerdBox{X0: 30, X1: 32, Z0: 20, Z1: 23}
	plan := NewHerdPlan(g, 5, 5, goal)
	// A sheep 25 tiles west and 15 south of the zone is 25 pushes east of its column and the rest north: the Manhattan distance to the
	// nearest zone tile (the zone's western edge is x 30).
	if c, ok := plan.Cost(5, 5); !ok || c != 25+15 {
		t.Errorf("cost from (5,5): %d (ok %v), want 40", c, ok)
	}
	if c, ok := plan.Cost(31, 21); !ok || c != 0 {
		t.Errorf("cost inside the zone: %d (ok %v)", c, ok)
	}
	dirs := HerdDirections(g, 5, 5, goal, nil)
	if len(dirs) != 4 {
		t.Fatalf("directions %v: all four pushes are legal on open ground", dirs)
	}
	if first := dirs[0]; first != (HerdDir{1, 0}) && first != (HerdDir{0, 1}) {
		t.Errorf("the best push is %v, want east or north", first)
	}
	// A wall of blocked tiles with a gap: the route goes through the gap.
	wall := map[[2]int]bool{}
	for z := 0; z <= 40; z++ {
		if z != 3 {
			wall[[2]int{20, z}] = true
		}
	}
	gw := openGround{x0: 0, x1: 40, z0: 0, z1: 40, blocked: wall}
	pw := NewHerdPlan(gw, 5, 20, goal)
	cw, ok := pw.Cost(5, 20)
	if !ok || cw <= 25+3 {
		t.Errorf("walled ground: cost %d (ok %v), want a detour through the gap at z 3", cw, ok)
	}
	// A tile the stand cannot be reached from: a sheep with a wall behind it cannot be pushed into the wall's direction.
	if HerdPushable(gw, 19, 20, HerdDir{1, 0}) {
		t.Error("a push into a wall is legal")
	}
	if !HerdPushable(g, 5, 5, HerdDir{0, 1}) {
		t.Error("a push on open ground is illegal")
	}
}

func TestHerdPlannerTraps(t *testing.T) {
	// A one-tile pocket reached by a one-tile corridor: a sheep that wanders into it has no stand behind it for any push.
	blocked := map[[2]int]bool{}
	for x := 0; x <= 20; x++ {
		for z := 0; z <= 20; z++ {
			blocked[[2]int{x, z}] = true
		}
	}
	for x := 2; x <= 18; x++ {
		for z := 8; z <= 12; z++ {
			delete(blocked, [2]int{x, z})
		}
	}
	delete(blocked, [2]int{19, 10}) // the corridor: a notch off the room's east wall
	g := openGround{x0: 0, x1: 20, z0: 0, z1: 20, blocked: blocked}
	if !HerdPinned(g, 19, 10) {
		t.Error("the notch is not a trap")
	}
	if HerdPinned(g, 10, 10) {
		t.Error("the middle of the room is a trap")
	}
	goal := HerdBox{X0: 2, X1: 3, Z0: 9, Z1: 11}
	plan := NewHerdPlan(g, 15, 10, goal)
	// The tiles next to the trap are charged the off-network penalty; the room away from it is not.
	if c, ok := plan.Cost(10, 10); !ok || c >= HerdOffNetwork {
		t.Errorf("the room's middle costs %d (ok %v)", c, ok)
	}
	if c, ok := plan.Cost(18, 10); ok && c < HerdOffNetwork {
		t.Errorf("a tile touching the trap costs %d: it should be off the clear network", c)
	}
	if _, ok := plan.Cost(19, 10); ok {
		t.Error("the trap itself has a route")
	}
}

// lineWorld is a deterministic sheep that obeys prod_sheep exactly and never wanders, on open ground, for the loop's edge cases.
type lineWorld struct {
	t        *testing.T
	g        openGround
	sheep    []HerdPos
	me       HerdPos
	ticks    int
	pen      HerdBox
	gate     HerdBox
	penned   bool
	pinned   map[HerdPos]bool    // sheep standing here never moves when prodded (a fence behind it)
	warp     map[HerdPos]HerdPos // a sheep pushed onto the key tile is found on the value (a wander into a trap)
	findFail bool
	stopAt   int
	prods    int
}

func (w *lineWorld) Copies() []HerdPos { return append([]HerdPos(nil), w.sheep...) }
func (w *lineWorld) Find() (HerdPos, bool) {
	if w.findFail {
		return HerdPos{}, false
	}
	return HerdPick(w.g, w.gate, w.sheep)
}
func (w *lineWorld) Approach(p HerdPos) bool { return true }
func (w *lineWorld) Me() (HerdPos, bool)     { return w.me, true }
func (w *lineWorld) WalkTo(x, z int) bool {
	if !w.g.Walkable(x, z) {
		return false
	}
	w.ticks += max(abs(x-w.me.X), abs(z-w.me.Z))
	w.me = HerdPos{x, z}
	return true
}
func (w *lineWorld) Prod(at HerdPos) HerdProd {
	w.prods++
	w.ticks++
	for i, s := range w.sheep {
		if s != at {
			continue
		}
		// The jump over the fence is read at the sheep's coordinate before it moves.
		if w.gate.Contains(s.X, s.Z) {
			w.sheep[i] = HerdPos{w.pen.X0 + 1, w.pen.Z0 + 1}
			return HerdProd{Sent: true, Moved: true}
		}
		dx, dz := coordDirectionPlan(w.me, s)
		next := HerdPos{s.X + dx, s.Z + dz}
		if w.pinned[s] || !w.g.CanStep(s.X, s.Z, dx, dz) {
			return HerdProd{Sent: true}
		}
		if to, ok := w.warp[next]; ok {
			next = to
		}
		w.sheep[i] = next
		return HerdProd{Sent: true, Moved: true, After: next, Seen: true}
	}
	return HerdProd{}
}
func (w *lineWorld) Wait(ticks int) { w.ticks += ticks }
func (w *lineWorld) Stop() bool     { return w.stopAt > 0 && w.ticks >= w.stopAt }
func (w *lineWorld) Logf(format string, args ...any) {
	w.t.Logf("[t%d] "+format, append([]any{w.ticks}, args...)...)
}

// coordDirectionPlan is the content's ~coord_direction(player, sheep).
func coordDirectionPlan(p, s HerdPos) (int, int) {
	dx, dz := s.X-p.X, s.Z-p.Z
	if dx > 0 {
		if abs(dz) > abs(dx) {
			if dz > 0 {
				return 0, 1
			}
			return 0, -1
		}
		return 1, 0
	}
	if abs(dz) > abs(dx) {
		if dz > 0 {
			return 0, 1
		}
		return 0, -1
	}
	return -1, 0
}

func newLineWorld(t *testing.T, sheep ...HerdPos) *lineWorld {
	return &lineWorld{t: t, g: openGround{x0: 0, x1: 60, z0: 0, z1: 60}, sheep: sheep, me: HerdPos{2, 2},
		pen: HerdBox{X0: 40, X1: 55, Z0: 20, Z1: 35}, gate: HerdBox{X0: 37, X1: 39, Z0: 28, Z1: 31}, pinned: map[HerdPos]bool{}, warp: map[HerdPos]HerdPos{}}
}

func TestHerdLegPensASheepOnOpenGround(t *testing.T) {
	w := newLineWorld(t, HerdPos{10, 10})
	res := HerdLeg(w, w.g, w.pen, w.gate)
	if !res.Penned || res.Why != "" {
		t.Fatalf("not penned: %+v after %d prods", res, w.prods)
	}
	// The shortest route is 28 pushes east and 18 north (to the zone's south-west corner (37,28)); a deterministic sheep needs no more
	// than that and a push to jump.
	if res.Pushes < 46 || res.Pushes > 60 {
		t.Errorf("%d pushes, want about the 46 of the Manhattan route", res.Pushes)
	}
}

func TestHerdLoopFollowsAnotherCopyWhenTheOneItHerdsIsTrapped(t *testing.T) {
	// Two copies; the one the leg follows is pushed onto a tile that wanders it into a one-tile trap (every side blocked). The copy is out of
	// the lock radius of where the last push left it, so the loop looks again and takes the other copy, the only one that has a push route.
	w := newLineWorld(t, HerdPos{30, 25}, HerdPos{12, 12})
	w.g.blocked = map[[2]int]bool{{44, 5}: true, {46, 5}: true, {45, 4}: true, {45, 6}: true}
	w.warp[HerdPos{30, 26}] = HerdPos{45, 5}
	res := HerdLeg(w, w.g, w.pen, w.gate)
	if !res.Penned {
		t.Fatalf("not penned: %+v", res)
	}
	if res.Pushes < 30 {
		t.Errorf("%d pushes: the other copy is 40 tiles from the fence", res.Pushes)
	}
	for _, s := range w.sheep {
		if s == (HerdPos{45, 5}) {
			return // the trapped copy is where it was left
		}
	}
	t.Error("the trapped copy was not the one that was left behind")
}

func TestHerdLegReportsWhatStoppedIt(t *testing.T) {
	// Every copy refuses: the leg ends with the reason, after HerdAttempts tries.
	w := newLineWorld(t, HerdPos{30, 25})
	w.pinned[HerdPos{30, 25}] = true
	res := HerdLeg(w, w.g, w.pen, w.gate)
	if res.Penned || !strings.Contains(res.Why, "refused") {
		t.Errorf("a sheep that never moves: %+v", res)
	}
	// No sheep with a route: the reason says so at once.
	w = newLineWorld(t)
	w.findFail = true
	if res = HerdLeg(w, w.g, w.pen, w.gate); res.Penned || !strings.Contains(res.Why, "no sheep") {
		t.Errorf("no sheep: %+v", res)
	}
	// The world's stop ends the leg without another attempt.
	w = newLineWorld(t, HerdPos{10, 10})
	w.stopAt = 20
	if res = HerdLeg(w, w.g, w.pen, w.gate); res.Penned || res.Why != "out of time" {
		t.Errorf("out of time: %+v", res)
	}
}

func TestHerdLoopRoutesRoundABlockedTile(t *testing.T) {
	// A blocked tile east of the sheep: no push ever drives the sheep onto it, and the sheep is penned all the same.
	w := newLineWorld(t, HerdPos{35, 25})
	w.g.blocked = map[[2]int]bool{{36, 25}: true, {36, 26}: true, {36, 24}: true}
	res := HerdLoop(w, w.g, w.pen, w.gate, HerdPos{35, 25})
	if !res.Penned {
		t.Fatalf("not penned: %+v", res)
	}
	if w.g.blocked[[2]int{w.sheep[0].X, w.sheep[0].Z}] {
		t.Error("the sheep ended on a blocked tile")
	}
}

func TestHerdBehind(t *testing.T) {
	dir := HerdDir{1, 0}
	sheep := HerdPos{10, 10}
	for step := 1; step <= HerdApproachRay; step++ {
		if !HerdBehind(HerdPos{10 - step, 10}, sheep, dir) {
			t.Errorf("%d tiles behind is not behind", step)
		}
	}
	if HerdBehind(HerdPos{10 - HerdApproachRay - 1, 10}, sheep, dir) || HerdBehind(HerdPos{9, 11}, sheep, dir) || HerdBehind(HerdPos{11, 10}, sheep, dir) {
		t.Error("off the line, or in front, counts as behind")
	}
}
