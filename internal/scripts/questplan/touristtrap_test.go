package questplan

import (
	"slices"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// ttIn builds a snapshot for DecideTouristTrap: a comfortable account (combat 70, Smithing 25, Fletching 15) with its pack, at a tile, in a quest at
// a stage. Fields are overridden by the caller.
type ttIn struct {
	stage       int
	at          tile.Tile
	inv, worn   map[int]int
	levels      map[string]int
	hp, maxHP   int
	ran, failed map[string]bool
	bank        map[int]int
	free        int
	notStarted  bool
}

// ttKit is a full desert kit for the first crossing: two outfits (one worn), three skins, a pass, the papers and food.
func ttKit() (inv, worn map[int]int) {
	inv = map[int]int{CoinsID: 700, ObjTtSkin4: 3, ObjTtPass: 1, ObjBronzeBar: 4, ObjHammer: 1, ObjTtFeather: 60, 385: 6,
		ObjTtShirt: 1, ObjTtRobe: 1, ObjTtBoots: 1}
	worn = map[int]int{ObjTtShirt: 1, ObjTtRobe: 1, ObjTtBoots: 1}
	return inv, worn
}

func (in ttIn) snapshot() Snapshot {
	lv := map[string]int{"attack": 60, "strength": 60, "defence": 55, "hitpoints": 70, "magic": 50, "smithing": 25, "fletching": 15, "thieving": 20, "agility": 30}
	for k, v := range in.levels {
		lv[k] = v
	}
	hp, maxHP := in.hp, in.maxHP
	if maxHP == 0 {
		hp, maxHP = 70, 70
	}
	free := in.free
	if free == 0 {
		free = 12
	}
	s := Snapshot{
		Journal: StatusInProgress, Stage: in.stage, StageKnown: true, Tile: in.at, TileKnown: true, Levels: lv, HP: hp, MaxHP: maxHP, FreeSlots: free,
		InvIDs: map[int]int{CoinsID: 700}, WornIDs: map[int]int{}, Inv: map[string]int{}, Worn: map[string]int{},
		Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{}, BankKnown: true, BankIDs: map[int]int{},
	}
	if in.inv != nil {
		s.InvIDs = map[int]int{}
		for id, n := range in.inv {
			s.InvIDs[id] = n
		}
	}
	for id, n := range in.worn {
		s.WornIDs[id] = n
	}
	for k, v := range in.ran {
		s.Ran[k] = v
	}
	for k, v := range in.failed {
		s.Failed[k] = v
	}
	if in.bank != nil {
		s.BankIDs = in.bank
	}
	if in.notStarted {
		s.Journal = StatusNotStarted
	}
	return s
}

func (in ttIn) decide() Step { return DecideTouristTrap(in.snapshot()) }

// want asserts a step's kind and, for a custom, its name and Qty (-1: any), for a talk its npc id, for a provision its kit.
type ttExpect struct {
	kind   StepKind
	custom string
	qty    int
	npc    int
	kit    string
	park   bool
}

func (w ttExpect) check(t *testing.T, what string, st Step) {
	t.Helper()
	if why := ValidateStep(st); why != "" {
		t.Errorf("%s: a malformed step: %s (%s)", what, why, st.Describe())
		return
	}
	if st.Kind != w.kind {
		t.Errorf("%s: step %q (%s), want kind %s", what, st.Describe(), st.Kind, w.kind)
		return
	}
	switch w.kind {
	case StepCustom:
		if st.Custom != w.custom || (w.qty >= 0 && st.Qty != w.qty) {
			t.Errorf("%s: custom %q qty %d (%s), want %q qty %d", what, st.Custom, st.Qty, st.Reason, w.custom, w.qty)
		}
	case StepTalk:
		if st.Stop.ID != w.npc {
			t.Errorf("%s: talk to npc %d, want %d", what, st.Stop.ID, w.npc)
		}
	case StepProvision:
		if st.KitName != w.kit {
			t.Errorf("%s: kit %q, want %q", what, st.KitName, w.kit)
		}
	case StepWait:
		if w.park != (st.ParkFor > 0) {
			t.Errorf("%s: wait %q parks %v, want %v", what, st.Reason, st.ParkFor > 0, w.park)
		}
	}
}

func ttCust(name string, qty int) ttExpect { return ttExpect{kind: StepCustom, custom: name, qty: qty} }

var (
	ttAtShop    = tile.Tile{X: 3304, Z: 3123} // Shantay's shelf
	ttAtIrena   = tile.Tile{X: 3303, Z: 3113} // just south of the pass
	ttAtDesert  = tile.Tile{X: 3250, Z: 3060} // the open desert
	ttAtBedabin = tile.Tile{X: 3171, Z: 3030} // the Bedabin camp
	ttAtGateOut = tile.Tile{X: 3272, Z: 3029} // outside the camp's gate
	ttAtCamp    = tile.Tile{X: 3290, Z: 3025} // the camp's yard
	ttAtHall    = tile.Tile{X: 3278, Z: 9420} // behind the mine doors
	ttAtLower   = tile.Tile{X: 3295, Z: 9420} // the lower mine
	ttAtDeep    = tile.Tile{X: 3320, Z: 9440} // the deep mine
	ttAtCell    = tile.Tile{X: 3285, Z: 3034} // the surface cell
	ttAtMineJ   = tile.Tile{X: 3288, Z: 9437} // the punishment mine
	ttAtMain    = tile.Tile{X: 3269, Z: 3167} // Al Kharid's bank
	ttAtUpstair = tile.Tile{X: 3290, Z: 3034, Level: 1}
)

// ---------------------------------------------------------------------------------------------------------------
// The areas and the heat.
// ---------------------------------------------------------------------------------------------------------------

func TestTouristTrapAreasAndZones(t *testing.T) {
	for _, c := range []struct {
		name string
		at   tile.Tile
		area TtArea
		zone TtZone
	}{
		{"Draynor bank", tile.Tile{X: 3093, Z: 3243}, TtAreaMainland, TtZoneNorth},
		{"Yanille, whose z is under the pass's", tile.Tile{X: 2613, Z: 3094}, TtAreaMainland, TtZoneNorth},
		{"Al Kharid's bank", ttAtMain, TtAreaMainland, TtZoneNorth},
		{"Shantay's shelf", ttAtShop, TtAreaShantayNorth, TtZoneNorth},
		{"Irena", ttAtIrena, TtAreaIrena, TtZoneOpen},
		{"the open desert", ttAtDesert, TtAreaDesert, TtZoneOpen},
		{"the Captain", TtCaptainAt, TtAreaDesert, TtZoneOpen},
		{"the gate's outside", TtGateOut, TtAreaDesert, TtZoneOpen},
		{"the gate's inside", TtGateIn, TtAreaCamp, TtZoneCamp},
		{"the slave", TtSlaveAt, TtAreaCamp, TtZoneCamp},
		{"Siad's room", TtDesk, TtAreaCampUpper, TtZoneCamp},
		{"the surface cell", ttAtCell, TtAreaSurfaceCell, TtZoneNone},
		{"a rock out of the cell", tile.Tile{X: 3277, Z: 3039}, TtAreaSurfaceCell, TtZoneNone},
		{"Al Shabim", TtAlAt, TtAreaBedabin, TtZoneOpen},
		{"the tent", TtTentInside, TtAreaTent, TtZoneOpen},
		{"the tent's outside", TtTentOutside, TtAreaBedabin, TtZoneOpen},
		{"the hall", TtHallExit, TtAreaHall, TtZoneHall},
		{"the guarded cave's hall side", TtCaveHall, TtAreaHall, TtZoneHall},
		{"the cave's lower side", TtCaveLower, TtAreaLower, TtZoneLower},
		{"the lift", TtLiftAt, TtAreaLower, TtZoneLower},
		{"the lower cart", TtLowerCart, TtAreaLower, TtZoneLower},
		{"the deep cart", TtDeepCart, TtAreaDeep, TtZoneDeep},
		{"Ana", TtAnaAt, TtAreaDeep, TtZoneDeep},
		{"the punishment mine", ttAtMineJ, TtAreaMineCell, TtZoneNone},
		{"the cell gate's east side", tile.Tile{X: 3293, Z: 9434}, TtAreaDeep, TtZoneDeep},
		{"the cart's landing", TtCartLanding, TtAreaDesert, TtZoneOpen},
	} {
		if got := TtAreaOf(c.at); got != c.area {
			t.Errorf("%s %v: area %d, want %d", c.name, c.at, got, c.area)
		}
		if got := TtAreaOf(c.at).ZoneOf(); got != c.zone {
			t.Errorf("%s %v: zone %d, want %d", c.name, c.at, got, c.zone)
		}
	}
	// South of the pass is the desert side; the shop side and the rest of the map are not.
	if !TtAreaIrena.South() || !TtAreaMineCell.South() || TtAreaShantayNorth.South() || TtAreaMainland.South() {
		t.Error("South() misplaces the pass")
	}
}

// The heat is in the desert zones and not in the two camps, and the pass is the line between.
func TestTouristTrapHeat(t *testing.T) {
	for _, c := range []struct {
		name string
		at   tile.Tile
		heat bool
	}{
		{"Irena, south of the pass", ttAtIrena, true},
		{"the open desert", ttAtDesert, true},
		{"the Captain", TtCaptainAt, true},
		{"the cart's landing", TtCartLanding, true},
		{"the shop, north of the pass", ttAtShop, false},
		{"the pass's north stand", TtPassNorth, false},
		{"the pass's south stand", TtPassSouth, true},
		{"the mining camp", TtSlaveAt, false},
		{"the camp's gate inside", TtGateIn, false},
		{"Al Shabim, in the Bedabin camp", TtAlAt, false},
		{"the Bedabin tent's door", TtTentOutside, false},
		{"the Bedabin camp's edge", tile.Tile{X: 3150, Z: 3000}, true},
		{"Al Kharid", ttAtMain, false},
		{"Lumbridge", tile.Tile{X: 3222, Z: 3218}, false},
		{"the mines", TtAnaAt, false},
	} {
		if got := TtHeat(c.at); got != c.heat {
			t.Errorf("%s %v: heat %v, want %v", c.name, c.at, got, c.heat)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------
// The first crossing.
// ---------------------------------------------------------------------------------------------------------------

func TestTouristTrapFirstCrossingBuildsItsKitInOrder(t *testing.T) {
	// No coins: the purse first, from the bank and the mule.
	ttExpect{kind: StepProvision, kit: KitTtPurse}.check(t, "no coins", ttIn{at: ttAtMain, notStarted: true, inv: map[int]int{CoinsID: 10}}.decide())
	// Coins and nothing else: the whole desert kit, its non-optional lines all sourced from Shantay's shelf.
	st := ttIn{at: ttAtMain, notStarted: true, inv: map[int]int{CoinsID: 900}}.decide()
	ttExpect{kind: StepProvision, kit: KitTtTrip}.check(t, "coins only", st)
	for _, l := range st.Kit {
		if why := ValidateKitLine(l); why != "" {
			t.Errorf("kit line %s: %s", l.Name, why)
		}
		if !l.Optional && !l.Untradeable {
			shop := false
			for _, s := range l.Sources {
				shop = shop || (s.Kind == SourceShop && s.Shop.NpcID == NpcShantay)
			}
			// The coins and the food come from the bank and the mule: no shop on this side of the pass sells either, and a food line no source feeds
			// fails the kit (the crossing parks on the food floor).
			if l.ID != CoinsID && !ttFoodLine(l.ID) && !shop {
				t.Errorf("kit line %s has no Shantay source", l.Name)
			}
		}
	}
	want := map[int]int{ObjTtShirt: 2, ObjTtRobe: 2, ObjTtBoots: 2, ObjTtSkin4: 3, ObjTtPass: 1, ObjBronzeBar: TtBars, ObjHammer: 1, ObjTtFeather: TtFeathers}
	for id, qty := range want {
		found := false
		for _, l := range st.Kit {
			if l.ID == id {
				found = true
				if l.Qty != qty {
					t.Errorf("kit line %s asks %d, want %d", l.Name, l.Qty, qty)
				}
			}
		}
		if !found {
			t.Errorf("kit lacks object %d", id)
		}
	}
	if !slices.Contains(st.KeepIDs, ObjTtMetalKey) || !slices.Contains(st.KeepIDs, 385) {
		t.Error("the kit's keep list drops the quest's tools or the food")
	}
	// The full kit, the desert outfit still in the pack: it goes on first, then the pass.
	inv, _ := ttKit()
	inv[ObjTtShirt], inv[ObjTtRobe], inv[ObjTtBoots] = 2, 2, 2 // both outfits in the pack, none on
	ttExpect{kind: StepCustom, custom: CustomTtDress, qty: TtDressDesert}.check(t, "kit in the pack", ttIn{at: ttAtShop, notStarted: true, inv: inv}.decide())
	inv, worn := ttKit()
	ttCust(CustomTtSouth, 0).check(t, "dressed", ttIn{at: ttAtShop, notStarted: true, inv: inv, worn: worn}.decide())
	// Across, the talk with Irena starts the quest.
	ttExpect{kind: StepTalk, npc: NpcTtIrena}.check(t, "at Irena", ttIn{at: ttAtIrena, notStarted: true, inv: inv, worn: worn}.decide())
	// A journal that has not been read waits; a stage the tab and the page disagree on does not start over.
	ttExpect{kind: StepWait}.check(t, "unknown tab", func() Step {
		s := ttIn{at: ttAtShop}.snapshot()
		s.Journal = StatusUnknown
		return DecideTouristTrap(s)
	}())
}

// A kit the shelf and the mule could not fill is gone on with when the lines are not critical, and parks when they are.
func TestTouristTrapGoesOnWithAKitThatFailedUnlessItIsCritical(t *testing.T) {
	inv, worn := ttKit()
	delete(inv, ObjTtFeather) // not critical at the start
	ttCust(CustomTtSouth, 0).check(t, "no feathers, kit tried", ttIn{at: ttAtShop, notStarted: true, inv: inv, worn: worn, failed: map[string]bool{"provision:" + KitTtTrip: true}}.decide())
	ttExpect{kind: StepProvision, kit: KitTtTrip}.check(t, "no feathers, kit not tried", ttIn{at: ttAtShop, notStarted: true, inv: inv, worn: worn}.decide())
	// No water at all is not something to cross with.
	inv, worn = ttKit()
	delete(inv, ObjTtSkin4)
	ttExpect{kind: StepWait, park: true}.check(t, "no water", ttIn{at: ttAtShop, notStarted: true, inv: inv, worn: worn, failed: map[string]bool{"provision:" + KitTtTrip: true}}.decide())
	// Stage 12 without a hammer is critical: the account does not cross.
	inv, worn = ttKit()
	inv[ObjTtPlans] = 1
	delete(inv, ObjHammer)
	st := ttIn{stage: TtGotPlans, at: ttAtShop, inv: inv, worn: worn, failed: map[string]bool{"provision:" + KitTtTrip: true}}.decide()
	ttExpect{kind: StepWait, park: true}.check(t, "no hammer at stage 12", st)
	if !strings.Contains(st.Reason, "Hammer") {
		t.Errorf("the park does not name the hammer: %s", st.Reason)
	}
}

// An account that is too weak for the Captain is parked before it crosses, and one that is strong enough is not; a lost hitpoint floor parks too.
func TestTouristTrapPricesTheCaptainBeforeCrossing(t *testing.T) {
	inv, worn := ttKit()
	weak := ttIn{at: ttAtShop, notStarted: true, inv: inv, worn: worn, levels: map[string]int{"attack": 5, "strength": 5, "defence": 5, "hitpoints": 28}, hp: 28, maxHP: 28}.decide()
	ttExpect{kind: StepWait, park: true}.check(t, "a combat 12 account", weak)
	if !strings.Contains(weak.Reason, "Mercenary Captain") {
		t.Errorf("the park does not name the captain: %s", weak.Reason)
	}
	ok, why := TtCaptainFit(ttIn{levels: map[string]int{"attack": 70, "strength": 70, "defence": 65, "hitpoints": 80}, maxHP: 80, hp: 80, inv: map[int]int{385: 6}}.snapshot())
	if !ok {
		t.Errorf("a combat 90 account with six sharks is refused: %s", why)
	}
	// Without the food the same account is judged on its hitpoints alone.
	ok, _ = TtCaptainFit(ttIn{levels: map[string]int{"attack": 25, "strength": 25, "defence": 25, "hitpoints": 30}, maxHP: 30, hp: 30, inv: map[int]int{}}.snapshot())
	if ok {
		t.Error("a combat 30 account with no food is allowed the captain")
	}
	// Under the floor of base hitpoints nobody crosses.
	st := ttIn{at: ttAtShop, notStarted: true, inv: inv, worn: worn, levels: map[string]int{"hitpoints": 20}, maxHP: 20, hp: 20}.decide()
	ttExpect{kind: StepWait, park: true}.check(t, "20 hitpoints", st)
	// Levels that are not read yet are not judged.
	if ok, _ := TtCaptainFit(Snapshot{InvIDs: map[int]int{}, WornIDs: map[int]int{}, Levels: map[string]int{}}); !ok {
		t.Error("an unread account is judged")
	}
}

// ---------------------------------------------------------------------------------------------------------------
// The stages, one place each.
// ---------------------------------------------------------------------------------------------------------------

func TestTouristTrapStageTable(t *testing.T) {
	kitInv, kitWorn := ttKit()
	with := func(extra map[int]int) map[int]int {
		m := map[int]int{}
		for k, v := range kitInv {
			m[k] = v
		}
		for k, v := range extra {
			if v == 0 {
				delete(m, k)
			} else {
				m[k] = v
			}
		}
		return m
	}
	slave := map[int]int{ObjTtSlaveShirt: 1, ObjTtSlaveRobe: 1, ObjTtSlaveBoots: 1}
	slaveWorn := slave
	cases := []struct {
		name string
		in   ttIn
		want ttExpect
	}{
		{"1 at Irena: the captain", ttIn{stage: TtStarted, at: ttAtIrena, inv: kitInv, worn: kitWorn}, ttCust(CustomTtCaptain, 0)},
		{"3 in the desert: the captain", ttIn{stage: TtApproached, at: ttAtGateOut, inv: kitInv, worn: kitWorn}, ttCust(CustomTtCaptain, 0)},
		{"3 at the shop: across first", ttIn{stage: TtApproached, at: ttAtShop, inv: kitInv, worn: kitWorn}, ttCust(CustomTtSouth, 0)},
		{"4 with the key outside: dress for the camp", ttIn{stage: TtKilledCapt, at: ttAtGateOut, inv: with(map[int]int{ObjTtMetalKey: 1}), worn: map[int]int{ObjTtShirt: 1, ObjTtRobe: 1, ObjTtBoots: 1, 1333: 1}}, ttExpect{kind: StepCustom, custom: CustomTtDress, qty: TtDressCamp}},
		{"4 with the key, clothes only: the gate", ttIn{stage: TtKilledCapt, at: ttAtGateOut, inv: with(map[int]int{ObjTtMetalKey: 1}), worn: kitWorn}, ttCust(CustomTtGateIn, 0)},
		{"5 in the camp: the slave", ttIn{stage: TtEnteredCmp, at: ttAtCamp, inv: with(map[int]int{ObjTtMetalKey: 1}), worn: kitWorn}, ttCust(CustomTtSlave, 0)},
		{"6 in the camp: the slave", ttIn{stage: TtSpokenSlv, at: ttAtCamp, inv: with(map[int]int{ObjTtMetalKey: 1}), worn: kitWorn}, ttCust(CustomTtSlave, 0)},
		{"7 in the camp: the slave", ttIn{stage: TtFreedSlave, at: ttAtCamp, inv: with(map[int]int{ObjTtMetalKey: 1}), worn: kitWorn}, ttCust(CustomTtSlave, 0)},
		{"8 in the camp: slave clothes on", ttIn{stage: TtTraded, at: ttAtCamp, inv: with(map[int]int{ObjTtMetalKey: 1, ObjTtSlaveShirt: 1, ObjTtSlaveRobe: 1, ObjTtSlaveBoots: 1}), worn: kitWorn}, ttExpect{kind: StepCustom, custom: CustomTtDress, qty: TtDressSlave}},
		{"8 in the camp, dressed: the doors", ttIn{stage: TtTraded, at: ttAtCamp, inv: with(map[int]int{ObjTtMetalKey: 1}), worn: slaveWorn}, ttCust(CustomTtMineIn, 0)},
		{"9 in the hall: the guard", ttIn{stage: TtEnteredMin, at: ttAtHall, inv: with(map[int]int{ObjTtMetalKey: 1}), worn: slaveWorn}, ttExpect{kind: StepTalk, npc: NpcTtCaveGuard}},
		{"10 in the hall: the way out first", ttIn{stage: TtFindingPin, at: ttAtHall, inv: with(map[int]int{ObjTtMetalKey: 1}), worn: slaveWorn}, ttCust(CustomTtMineOut, 0)},
		{"10 in the camp in slave clothes: take them off", ttIn{stage: TtFindingPin, at: ttAtCamp, inv: with(map[int]int{ObjTtMetalKey: 1}), worn: slaveWorn}, ttExpect{kind: StepCustom, custom: CustomTtDress, qty: TtDressPlain}},
		{"10 in the camp, plain: the gate", ttIn{stage: TtFindingPin, at: ttAtCamp, inv: with(map[int]int{ObjTtMetalKey: 1}), worn: kitWorn}, ttCust(CustomTtGateOut, 0)},
		{"10 at the Bedabin camp: Al Shabim", ttIn{stage: TtFindingPin, at: ttAtBedabin, inv: with(map[int]int{ObjTtMetalKey: 1}), worn: kitWorn}, ttExpect{kind: StepTalk, npc: NpcTtAlShabim}},
		{"11 with the key in the Bedabin camp: back to the camp", ttIn{stage: TtBedobinKey, at: ttAtBedabin, inv: with(map[int]int{ObjTtMetalKey: 1, ObjTtBedobinKey: 1}), worn: kitWorn}, ttExpect{kind: StepCustom, custom: CustomTtGateIn}},
		{"11 in the camp: the plans", ttIn{stage: TtBedobinKey, at: ttAtCamp, inv: with(map[int]int{ObjTtMetalKey: 1, ObjTtBedobinKey: 1}), worn: kitWorn}, ttCust(CustomTtPlans, 0)},
		{"11 lost the key: Al Shabim", ttIn{stage: TtBedobinKey, at: ttAtBedabin, inv: with(map[int]int{ObjTtMetalKey: 1}), worn: kitWorn}, ttExpect{kind: StepTalk, npc: NpcTtAlShabim}},
		{"12 with the plans in the camp: out", ttIn{stage: TtGotPlans, at: ttAtCamp, inv: with(map[int]int{ObjTtMetalKey: 1, ObjTtPlans: 1}), worn: kitWorn}, ttCust(CustomTtGateOut, 0)},
		{"12 with the plans at Al Shabim", ttIn{stage: TtGotPlans, at: ttAtBedabin, inv: with(map[int]int{ObjTtMetalKey: 1, ObjTtPlans: 1}), worn: kitWorn}, ttCust(CustomTtShowPlans, 0)},
		{"13 at the Bedabin camp: the tent", ttIn{stage: TtShownPlans, at: ttAtBedabin, inv: with(map[int]int{ObjTtPlans: 1}), worn: kitWorn}, ttCust(CustomTtTent, 0)},
		{"13 inside the tent: the anvil", ttIn{stage: TtShownPlans, at: TtTentInside, inv: with(map[int]int{ObjTtPlans: 1}), worn: kitWorn}, ttCust(CustomTtTent, 0)},
		{"14 with the tip: the feathers", ttIn{stage: TtMadeTip, at: ttAtBedabin, inv: with(map[int]int{ObjTtDartTip: 1, ObjBronzeBar: 0, ObjHammer: 0}), worn: kitWorn}, ttCust(CustomTtFletch, 0)},
		{"15 with the dart: Al Shabim", ttIn{stage: TtMadeDart, at: ttAtBedabin, inv: with(map[int]int{ObjTtDart: 1, ObjBronzeBar: 0, ObjHammer: 0, ObjTtFeather: 0}), worn: kitWorn}, ttCust(CustomTtDart, 0)},
		{"16 with the pineapple in the desert: the camp", ttIn{stage: TtLearned, at: ttAtBedabin, inv: with(map[int]int{ObjTtPineapple: 1, ObjTtMetalKey: 1, ObjBronzeBar: 0, ObjHammer: 0, ObjTtFeather: 0}), worn: kitWorn}, ttExpect{kind: StepCustom, custom: CustomTtGateIn}},
		{"16 in the hall: the guard", ttIn{stage: TtLearned, at: ttAtHall, inv: with(map[int]int{ObjTtPineapple: 1, ObjTtMetalKey: 1, ObjBronzeBar: 0, ObjHammer: 0, ObjTtFeather: 0}), worn: slaveWorn}, ttCust(CustomTtPineapple, 0)},
		{"16 lost the pineapple: Al Shabim", ttIn{stage: TtLearned, at: ttAtBedabin, inv: with(map[int]int{ObjTtMetalKey: 1, ObjBronzeBar: 0, ObjHammer: 0, ObjTtFeather: 0}), worn: kitWorn}, ttExpect{kind: StepTalk, npc: NpcTtAlShabim}},
		{"17 in the hall: the cave", ttIn{stage: TtGivenPine, at: ttAtHall, inv: with(map[int]int{ObjTtMetalKey: 1, ObjBronzeBar: 0, ObjHammer: 0, ObjTtFeather: 0}), worn: slaveWorn}, ttCust(CustomTtCave, 1)},
		{"17 in the lower mine: a barrel", ttIn{stage: TtGivenPine, at: ttAtLower, inv: with(map[int]int{ObjTtMetalKey: 1, ObjBronzeBar: 0, ObjHammer: 0, ObjTtFeather: 0}), worn: slaveWorn}, ttCust(CustomTtBarrel, 1)},
		{"17 with the barrel: the cart", ttIn{stage: TtGivenPine, at: ttAtLower, inv: with(map[int]int{ObjTtMetalKey: 1, ObjTtBarrel: 1, ObjBronzeBar: 0, ObjHammer: 0, ObjTtFeather: 0}), worn: slaveWorn}, ttCust(CustomTtCart, 1)},
		{"18 in the deep mine with the barrel: Ana", ttIn{stage: TtUsedCart, at: ttAtDeep, inv: with(map[int]int{ObjTtMetalKey: 1, ObjTtBarrel: 1, ObjBronzeBar: 0, ObjHammer: 0, ObjTtFeather: 0}), worn: slaveWorn}, ttCust(CustomTtCatch, 0)},
		{"19 with Ana in the deep mine: the checkpoint", ttIn{stage: TtRescue, at: ttAtDeep, inv: with(map[int]int{ObjTtMetalKey: 1, ObjTtAnaBarrel: 1, ObjBronzeBar: 0, ObjHammer: 0, ObjTtFeather: 0}), worn: slaveWorn}, ttCust(CustomTtDeepCheck, 0)},
		{"19 in the deep mine with nothing: the checkpoint", ttIn{stage: TtRescue, at: ttAtDeep, inv: with(map[int]int{ObjTtMetalKey: 1, ObjBronzeBar: 0, ObjHammer: 0, ObjTtFeather: 0}), worn: slaveWorn}, ttCust(CustomTtDeepCheck, 0)},
		{"19 with Ana in the lower mine: the lift", ttIn{stage: TtRescue, at: ttAtLower, inv: with(map[int]int{ObjTtMetalKey: 1, ObjTtAnaBarrel: 1, ObjBronzeBar: 0, ObjHammer: 0, ObjTtFeather: 0}), worn: slaveWorn}, ttCust(CustomTtLift, 0)},
		{"19 in the lower mine with nothing: the barrels", ttIn{stage: TtRescue, at: ttAtLower, inv: with(map[int]int{ObjTtMetalKey: 1, ObjBronzeBar: 0, ObjHammer: 0, ObjTtFeather: 0}), worn: slaveWorn}, ttCust(CustomTtBarrel, 0)},
		{"19 in the camp with nothing: the winch", ttIn{stage: TtRescue, at: ttAtCamp, inv: with(map[int]int{ObjTtMetalKey: 1, ObjBronzeBar: 0, ObjHammer: 0, ObjTtFeather: 0}), worn: kitWorn}, ttCust(CustomTtWinch, 0)},
		{"19 in the camp with Ana: the cart", ttIn{stage: TtRescue, at: ttAtCamp, inv: with(map[int]int{ObjTtMetalKey: 1, ObjTtAnaBarrel: 1, ObjBronzeBar: 0, ObjHammer: 0, ObjTtFeather: 0}), worn: kitWorn}, ttCust(CustomTtEscape, 0)},
		{"19 in the desert with Ana: Irena", ttIn{stage: TtRescue, at: tile.Tile{X: 3258, Z: 3029}, inv: with(map[int]int{ObjTtMetalKey: 1, ObjTtAnaBarrel: 1, ObjBronzeBar: 0, ObjHammer: 0, ObjTtFeather: 0}), worn: kitWorn}, ttExpect{kind: StepTalk, npc: NpcTtIrena}},
		{"27 at Irena: the reward", ttIn{stage: TtReward, at: ttAtIrena, inv: with(map[int]int{ObjTtMetalKey: 1, ObjBronzeBar: 0, ObjHammer: 0, ObjTtFeather: 0}), worn: kitWorn}, ttExpect{kind: StepTalk, npc: NpcTtIrena}},
	}
	for _, c := range cases {
		c.want.check(t, c.name, c.in.decide())
	}
}

// ---------------------------------------------------------------------------------------------------------------
// The water.
// ---------------------------------------------------------------------------------------------------------------

func TestTouristTrapWaterRule(t *testing.T) {
	_, worn := ttKit()
	dry := func(extra map[int]int) map[int]int {
		m := map[int]int{CoinsID: 700, ObjTtMetalKey: 1, 385: 6}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	// In the heat, under three doses, near the Bedabin camp, with coins: the camp's shop.
	// It is a provision step (the item rule: the bank and the mule first), never a bare buy.
	waterShop := func(what string, st Step) {
		t.Helper()
		if st.Kind != StepProvision || st.KitName != KitTtWater || len(st.Kit) != 1 || st.Kit[0].ID != ObjTtSkin4 || st.Kit[0].Qty != TtSkins ||
			len(st.Kit[0].Sources) != 1 || st.Kit[0].Sources[0].Kind != SourceShop || st.Kit[0].Sources[0].Shop.NpcID != NpcTtBedabin {
			t.Errorf("%s: %s", what, st.Describe())
		} else if why := ValidateStep(st); why != "" {
			t.Errorf("%s: a malformed step: %s", what, why)
		} else if st.Bank != AlKharidBank {
			t.Errorf("%s: the bank is %v", what, st.Bank)
		}
	}
	st := ttIn{stage: TtFindingPin, at: tile.Tile{X: 3200, Z: 3020}, inv: dry(map[int]int{ObjTtSkin1: 1}), worn: worn}.decide()
	waterShop("one dose in the desert near the Bedabin camp", st)
	// East of the camp, without the coins for three skins, or with the shop's kit failed, the pass.
	ttCust(CustomTtNorth, 0).check(t, "99 coins", ttIn{stage: TtFindingPin, at: tile.Tile{X: 3200, Z: 3020}, inv: dry(map[int]int{CoinsID: ttWaterCoins - 1}), worn: worn}.decide())
	waterShop("100 coins", ttIn{stage: TtFindingPin, at: tile.Tile{X: 3200, Z: 3020}, inv: dry(map[int]int{CoinsID: ttWaterCoins}), worn: worn}.decide())
	ttCust(CustomTtNorth, 0).check(t, "the shop's kit failed", ttIn{stage: TtFindingPin, at: tile.Tile{X: 3200, Z: 3020}, inv: dry(nil), worn: worn,
		failed: map[string]bool{"provision:" + KitTtWater: true}}.decide())
	ttCust(CustomTtNorth, 0).check(t, "one dose east of the camp", ttIn{stage: TtFindingPin, at: ttAtDesert, inv: dry(map[int]int{ObjTtSkin1: 1}), worn: worn}.decide())
	ttCust(CustomTtNorth, 0).check(t, "no coins", ttIn{stage: TtFindingPin, at: tile.Tile{X: 3200, Z: 3020}, inv: dry(map[int]int{CoinsID: 30}), worn: worn}.decide())
	// With the doses it needs it goes on.
	st = ttIn{stage: TtFindingPin, at: ttAtDesert, inv: dry(map[int]int{ObjTtSkin2: 1, ObjTtSkin1: 1}), worn: worn}.decide()
	if st.KitName == KitTtWater || (st.Kind == StepCustom && st.Custom == CustomTtNorth) {
		t.Errorf("three doses sent the account for water: %s", st.Describe())
	}
	// In the camp the heat is off: no water is looked for while the work is inside; about to leave for the desert it is.
	st = ttIn{stage: TtEnteredCmp, at: ttAtCamp, inv: dry(map[int]int{ObjTtShirt: 2, ObjTtRobe: 2, ObjTtBoots: 2}), worn: worn}.decide()
	ttCust(CustomTtSlave, 0).check(t, "dry in the camp, slave work", st)
	st = ttIn{stage: TtFindingPin, at: ttAtCamp, inv: dry(nil), worn: worn}.decide()
	ttCust(CustomTtGateOut, 0).check(t, "dry in the camp, leaving", st)
	// The Bedabin camp itself is heat-free but the account in it is in the open desert: the shop is right there.
	waterShop("dry in the Bedabin camp", ttIn{stage: TtFindingPin, at: ttAtBedabin, inv: dry(nil), worn: worn}.decide())
	// An account carrying Ana to Irena does not turn for water: the walk is under a minute and a half.
	st = ttIn{stage: TtRescue, at: tile.Tile{X: 3258, Z: 3029}, inv: dry(map[int]int{ObjTtAnaBarrel: 1}), worn: worn}.decide()
	ttExpect{kind: StepTalk, npc: NpcTtIrena}.check(t, "Ana and no water", st)
}

// ---------------------------------------------------------------------------------------------------------------
// The cells, the tent, and the levels.
// ---------------------------------------------------------------------------------------------------------------

func TestTouristTrapCellsAndTheTentAreLeftFirst(t *testing.T) {
	inv, worn := ttKit()
	ttCust(CustomTtJailCell, 0).check(t, "the surface cell, whatever the stage", ttIn{stage: TtSpokenSlv, at: ttAtCell, inv: inv, worn: worn}.decide())
	ttCust(CustomTtJailMine, 0).check(t, "the punishment mine", ttIn{stage: TtRescue, at: ttAtMineJ, inv: inv, worn: worn}.decide())
	ttCust(CustomTtTentOut, 0).check(t, "the tent at stage 11", ttIn{stage: TtBedobinKey, at: TtTentInside, inv: inv, worn: worn}.decide())
}

func TestTouristTrapLevels(t *testing.T) {
	inv, worn := ttKit()
	inv[ObjTtPlans] = 1
	// In the open desert the account walks out through the pass before the park (a parked quest leaves it to other work, and the heat runs on), with
	// the reason on the step; on the shop side it parks.
	low := ttIn{stage: TtShownPlans, at: ttAtBedabin, inv: inv, worn: worn, levels: map[string]int{"smithing": 12}}.decide()
	ttCust(CustomTtNorth, 0).check(t, "Smithing 12 at the anvil, in the desert", low)
	if !strings.Contains(low.Reason, "Smithing 20") {
		t.Errorf("the walk out does not name the level: %s", low.Reason)
	}
	low = ttIn{stage: TtShownPlans, at: ttAtShop, inv: inv, worn: worn, levels: map[string]int{"smithing": 12}}.decide()
	ttExpect{kind: StepWait, park: true}.check(t, "Smithing 12 at the anvil, at the shop", low)
	if !strings.Contains(low.Reason, "Smithing 20") {
		t.Errorf("the park does not name the level: %s", low.Reason)
	}
	inv[ObjTtDartTip] = 1
	delete(inv, ObjBronzeBar)
	low = ttIn{stage: TtMadeTip, at: ttAtBedabin, inv: inv, worn: worn, levels: map[string]int{"fletching": 4}}.decide()
	ttCust(CustomTtNorth, 0).check(t, "Fletching 4 at the dart, in the desert", low)
	low = ttIn{stage: TtMadeTip, at: ttAtShop, inv: inv, worn: worn, levels: map[string]int{"fletching": 4}}.decide()
	ttExpect{kind: StepWait, park: true}.check(t, "Fletching 4 at the dart, at the shop", low)
	// A level the snapshot has not read is not judged.
	got := ttIn{stage: TtShownPlans, at: ttAtBedabin, inv: inv, worn: worn, levels: map[string]int{"smithing": 0, "fletching": 0}}.decide()
	if got.Kind == StepWait && got.ParkFor > 0 {
		t.Errorf("unread levels parked the quest: %s", got.Reason)
	}
	// The record states both, so the ladder never starts an account below them.
	got2 := TouristTrapRecord.Requirements.Skills
	if len(got2) != 2 || got2[0].Level != TtSmithing || got2[1].Level != TtFletching {
		t.Errorf("the record's requirements are %+v", got2)
	}
}

// Lost things: the Metal key, the plans, the dart tip, the dart, the pineapple, the slave outfit, each answered by the plan's own recovery or a park
// with the reason, never a loop.
func TestTouristTrapLostThings(t *testing.T) {
	_, worn := ttKit()
	// The Metal key lost outside the camp with a bank that has none: the captain again; with the key in the bank: the bank first.
	noKey := map[int]int{CoinsID: 700, ObjTtSkin4: 3, 385: 6}
	ttCust(CustomTtCaptain, 0).check(t, "key lost, bank empty", ttIn{stage: TtKilledCapt, at: ttAtGateOut, inv: noKey, worn: worn}.decide())
	st := ttIn{stage: TtKilledCapt, at: ttAtGateOut, inv: noKey, worn: worn, bank: map[int]int{ObjTtMetalKey: 1}}.decide()
	ttCust(CustomTtNorth, 0).check(t, "key in the bank, in the desert", st)
	st = ttIn{stage: TtKilledCapt, at: ttAtMain, inv: noKey, worn: worn, bank: map[int]int{ObjTtMetalKey: 1}}.decide()
	if st.Kind != StepWithdraw || st.Items[0].ID != ObjTtMetalKey {
		t.Errorf("key in the bank, at the bank: %s", st.Describe())
	}
	// No key to leave the camp by: the desk, not the gate.
	ttCust(CustomTtKeys, 0).check(t, "leaving without the key", ttIn{stage: TtFindingPin, at: ttAtCamp, inv: noKey, worn: worn}.decide())
	// The plans lost at stage 13: Al's key, then the chest.
	lostPlans := map[int]int{CoinsID: 700, ObjTtSkin4: 3, 385: 6, ObjTtMetalKey: 1, ObjBronzeBar: 4, ObjHammer: 1, ObjTtFeather: 60}
	ttExpect{kind: StepTalk, npc: NpcTtAlShabim}.check(t, "plans lost, no key", ttIn{stage: TtShownPlans, at: ttAtBedabin, inv: lostPlans, worn: worn}.decide())
	lostPlans[ObjTtBedobinKey] = 1
	ttCust(CustomTtGateIn, 0).check(t, "plans lost, key held", ttIn{stage: TtShownPlans, at: ttAtBedabin, inv: lostPlans, worn: worn}.decide())
	// The dart tip lost after stage 14, plans and papers in the pack: the anvil again.
	again := map[int]int{CoinsID: 700, ObjTtSkin4: 3, 385: 6, ObjTtMetalKey: 1, ObjTtFeather: 60, ObjTtPlans: 1, ObjBronzeBar: 2, ObjHammer: 1}
	ttCust(CustomTtTent, 0).check(t, "tip lost", ttIn{stage: TtMadeTip, at: ttAtBedabin, inv: again, worn: worn}.decide())
	// The dart lost at stage 15 with a tip made again: the feathers.
	again[ObjTtDartTip] = 1
	ttCust(CustomTtFletch, 0).check(t, "dart lost, a tip in hand", ttIn{stage: TtMadeDart, at: ttAtBedabin, inv: again, worn: worn}.decide())
}

// Slave clothes lost after the trade: a desert set buys them back from the slave (a kit when there is none to trade), never a walk into the mine.
func TestTouristTrapLostSlaveOutfit(t *testing.T) {
	base := map[int]int{CoinsID: 700, ObjTtSkin4: 3, 385: 6, ObjTtMetalKey: 1}
	// In the camp at stage 8 without the slave outfit but with a desert set in the pack: the trade.
	with := map[int]int{ObjTtShirt: 1, ObjTtRobe: 1, ObjTtBoots: 1}
	for k, v := range base {
		with[k] = v
	}
	ttCust(CustomTtSlave, 0).check(t, "retrade", ttIn{stage: TtTraded, at: ttAtCamp, inv: with, worn: map[int]int{}}.decide())
	// With no set at all, the pass north for the shop.
	st := ttIn{stage: TtTraded, at: ttAtCamp, inv: base, worn: map[int]int{}}.decide()
	if st.Kind != StepCustom {
		t.Errorf("no outfit at all: %s", st.Describe())
	}
}

// ---------------------------------------------------------------------------------------------------------------
// Every step any state produces is well formed, and a long sweep never loops on a custom that is not registered.
// ---------------------------------------------------------------------------------------------------------------

func TestTouristTrapEveryStepIsWellFormed(t *testing.T) {
	_, kitWorn := ttKit()
	tiles := []tile.Tile{ttAtMain, ttAtShop, ttAtIrena, ttAtDesert, ttAtBedabin, TtTentInside, ttAtGateOut, ttAtCamp, ttAtUpstair, ttAtHall, ttAtLower, ttAtDeep, ttAtCell, ttAtMineJ,
		{X: 3093, Z: 3243}, {X: 3258, Z: 3029}}
	invs := []map[int]int{
		{},
		{CoinsID: 900},
		{CoinsID: 900, ObjTtSkin4: 3, ObjTtPass: 1, 385: 6},
		{CoinsID: 900, ObjTtSkin4: 1, ObjTtMetalKey: 1, ObjTtBedobinKey: 1, ObjTtPlans: 1},
		{CoinsID: 40, ObjTtSkin2: 1, ObjTtDartTip: 1, ObjTtFeather: 60, ObjTtPineapple: 1},
		{CoinsID: 900, ObjTtSkin4: 3, ObjTtBarrel: 1, ObjTtMetalKey: 1, ObjTtSlaveShirt: 1, ObjTtSlaveRobe: 1, ObjTtSlaveBoots: 1},
		{CoinsID: 900, ObjTtSkin4: 3, ObjTtAnaBarrel: 1, ObjTtMetalKey: 1, ObjTtDart: 1},
	}
	worns := []map[int]int{{}, kitWorn, {ObjTtSlaveShirt: 1, ObjTtSlaveRobe: 1, ObjTtSlaveBoots: 1}, {1333: 1, ObjTtShirt: 1}}
	stages := []int{TtNotStarted, TtStarted, TtApproached, TtKilledCapt, TtEnteredCmp, TtSpokenSlv, TtFreedSlave, TtTraded, TtEnteredMin, TtFindingPin, TtBedobinKey,
		TtGotPlans, TtShownPlans, TtMadeTip, TtMadeDart, TtLearned, TtGivenPine, TtUsedCart, TtRescue, TtReward}
	customs := map[string]bool{}
	for _, name := range []string{CustomTtSouth, CustomTtNorth, CustomTtDress, CustomTtCaptain, CustomTtGateIn, CustomTtGateOut, CustomTtKeys, CustomTtSlave,
		CustomTtMineIn, CustomTtMineOut, CustomTtPlans, CustomTtShowPlans, CustomTtTent, CustomTtFletch, CustomTtDart, CustomTtPineapple, CustomTtCave,
		CustomTtBarrel, CustomTtCart, CustomTtCatch, CustomTtDeepCheck, CustomTtLift, CustomTtWinch, CustomTtEscape, CustomTtRecapture, CustomTtJailCell,
		CustomTtJailMine, CustomTtTentOut} {
		customs[name] = true
	}
	n := 0
	for _, stage := range stages {
		for _, at := range tiles {
			for _, inv := range invs {
				for _, worn := range worns {
					for _, failed := range []map[string]bool{nil, {CustomTtWinch: true}, {CustomTtEscape: true, CustomTtWinch: true}, {"provision:" + KitTtTrip: true}} {
						st := ttIn{stage: stage, at: at, inv: inv, worn: worn, failed: failed, notStarted: stage == TtNotStarted}.decide()
						n++
						if why := ValidateStep(st); why != "" {
							t.Fatalf("stage %d at %v inv %v worn %v: %s (%s)", stage, at, inv, worn, why, st.Describe())
						}
						if st.Kind == StepCustom && !customs[st.Custom] {
							t.Fatalf("stage %d at %v: the custom %q is not one the plan lists", stage, at, st.Custom)
						}
						if st.Kind == StepProvision {
							for _, l := range st.Kit {
								if why := ValidateKitLine(l); why != "" {
									t.Fatalf("stage %d at %v: %s", stage, at, why)
								}
								// A bank visit never banks what its own kit asks for.
								if !slices.Contains(st.KeepIDs, l.ID) {
									t.Fatalf("stage %d at %v: the kit line %s (%d) is not on the visit's keep list", stage, at, l.Name, l.ID)
								}
							}
							for _, id := range TtToolIDs() {
								if !slices.Contains(st.KeepIDs, id) {
									t.Fatalf("stage %d at %v: the quest's own item %d is not on the visit's keep list", stage, at, id)
								}
							}
						}
						if st.Kind == StepWait && st.Reason == "" {
							t.Fatalf("stage %d at %v: a wait with no reason", stage, at)
						}
					}
				}
			}
		}
	}
	t.Logf("%d states, every step well formed", n)
}

// The decision never asks for the same custom forever: from every state a step is returned, and the step names a reason.
func TestTouristTrapEveryStepHasAReason(t *testing.T) {
	inv, worn := ttKit()
	for _, stage := range []int{TtStarted, TtGotPlans, TtRescue} {
		st := ttIn{stage: stage, at: ttAtDesert, inv: inv, worn: worn}.decide()
		if strings.TrimSpace(st.Reason) == "" && st.Kind != StepTalk {
			t.Errorf("stage %d: %s has no reason", stage, st.Describe())
		}
	}
}

// The keep lists: the module's tool list is the quest's own items, every one a bank visit leaves in the pack; the junk the quest hands out
// (an empty skin, a rock from the punishment mine, a kebab) is not kept, and the food is.
func TestTouristTrapKeepLists(t *testing.T) {
	if !slices.Equal(TouristTrap.ToolIDs, TtToolIDs()) {
		t.Errorf("the module's tools %v are not the plan's %v", TouristTrap.ToolIDs, TtToolIDs())
	}
	keep := ttKeep()
	for _, id := range []int{CoinsID, ObjTtSkin4, ObjTtSkin3, ObjTtSkin2, ObjTtSkin1, ObjTtShirt, ObjTtRobe, ObjTtBoots, ObjTtSlaveShirt, ObjTtSlaveRobe, ObjTtSlaveBoots,
		ObjTtMetalKey, ObjTtBedobinKey, ObjTtPlans, ObjTtDartTip, ObjTtDart, ObjTtPineapple, ObjTtBarrel, ObjTtAnaBarrel, ObjTtPass, ObjTtFeather, ObjBronzeBar, ObjHammer, 385, 373, 365} {
		if !slices.Contains(keep, id) {
			t.Errorf("item %d is not on the keep list", id)
		}
	}
	for _, id := range []int{ObjTtSkin0, ObjTtPunishRock, ObjTtKebab} {
		if slices.Contains(keep, id) {
			t.Errorf("junk item %d is on the keep list", id)
		}
	}
	// A bank visit is never planned with the account holding the stage's critical items outside the keep list: the deposit step of the sweep.
	for _, id := range ttFoodIDs {
		if !slices.Contains(keep, id) {
			t.Errorf("food %d is not kept", id)
		}
	}
	// Every piece of either outfit is a piece of clothes the camp allows.
	for _, id := range []int{ObjTtShirt, ObjTtRobe, ObjTtBoots, ObjTtSlaveShirt, ObjTtSlaveRobe, ObjTtSlaveBoots} {
		if !TtClothesID(id) {
			t.Errorf("outfit piece %d is not clothes", id)
		}
	}
}
