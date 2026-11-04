package questplan

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/cachecfg"
)

// The quest-point fillers built after Cook's Assistant and Sheep Shearer (Goblin Diplomacy, Romeo & Juliet, then Ernest the Chicken,
// Pirate's Treasure and Prince Ali Rescue) are the owner ladder's last stops before Dragon Slayer (W13 of
// docs/quest_plan_2026-10-15.md), in that order, and the first two are run by the engine's ladder with no gate of their own (the
// later fillers' own checks are in their own tests).
func TestTheFillersSitJustBeforeDragonSlayerAndGateNothing(t *testing.T) {
	n := len(OwnerLadder)
	g := slices.Index(OwnerLadder, GoblinDiplomacyID)
	if n < 3 || OwnerLadder[n-1] != "dragon" || g < 0 || !slices.Equal(OwnerLadder[g:], ownerFillerTail[2:]) {
		t.Fatalf("the ladder ends %v, want ... %v", OwnerLadder[max(0, n-len(ownerFillerTail)):], ownerFillerTail[2:])
	}
	for _, id := range []string{GoblinDiplomacyID, RomeoJulietID} {
		f := LadderFacts[id]
		if !f.Filler || f.QP != 5 || len(f.Gates) != 0 || len(f.Prereqs) != 0 || f.QPNeeded != 0 || f.EstMinutes <= 0 {
			t.Errorf("%s facts %+v: a filler gives 5 quest points and gates on nothing", id, f)
		}
		if QuestTabName(id) != f.Name {
			t.Errorf("%s: the quest tab name %q is not the facts' %q", id, QuestTabName(id), f.Name)
		}
		if holds := LadderHolds(map[string]Record{GoblinDiplomacyID: GoblinDiplomacyRecord, RomeoJulietID: RomeoJulietRecord}[id],
			PlayerState{SkillLevels: map[string]int{}}); len(holds) != 0 {
			t.Errorf("%s is held by %+v for an account with no levels", id, holds)
		}
		if _, ok := ModuleFiles[id]; !ok {
			t.Errorf("%s has no ModuleFiles entry", id)
		}
	}
	if LadderFacts[GoblinDiplomacyID].XP["crafting"] != 200 {
		t.Errorf("Goblin Diplomacy pays %d Crafting XP, quest_gobdip.rs2 pays stat_advance(crafting, 2000) = 200", LadderFacts[GoblinDiplomacyID].XP["crafting"])
	}
	if len(LadderFacts[RomeoJulietID].XP) != 0 {
		t.Errorf("Romeo & Juliet pays no experience: %v", LadderFacts[RomeoJulietID].XP)
	}
	// The slots: 40 and 25 minutes of estimate, plus the quarter.
	if got := QuestSlot(GoblinDiplomacyID, 20*time.Minute); got != 50*time.Minute {
		t.Errorf("Goblin Diplomacy slot %s", got)
	}
	if got := QuestSlot(RomeoJulietID, 20*time.Minute); got != 31*time.Minute+15*time.Second {
		t.Errorf("Romeo & Juliet slot %s", got)
	}
}

// A filler in the owner's order is not pulled ahead of the quests a deferral reorders: it has no blocker of any kind, so
// it would be the first quest "ready" every time a rung is deferred for want of combat levels, and Goblin Diplomacy would
// run ahead of Merlin's Crystal.
func TestAFillerIsNeverPulledAheadByADeferral(t *testing.T) {
	facts := map[string]RungFacts{
		"hard":   {ID: "hard", Name: "Hard", MinCombat: 90},
		"filler": {ID: "filler", Name: "Filler", QP: 5, Filler: true},
		"easy":   {ID: "easy", Name: "Easy", QP: 1},
	}
	p := PlanLadder([]string{"hard", "filler", "easy"}, facts, stage5Graduate())
	if got := planIDs(p); strings.Join(got, ",") != "easy,hard,filler" {
		t.Fatalf("plan %v, want easy first (pulled ahead of the deferred rung), then hard, then the filler in its place", got)
	}
	if rungOf(p, "filler").Inserted {
		t.Error("a filler named in the order is not an insertion")
	}
}

// Content 289's objects, NPCs and locs by the ids the two modules name, against the production config archive
// (RSB0T_CONFIG_JAG; skipped without it): a wrong id here sends an account to the wrong NPC or asks the provisioner for the
// wrong item.
func TestTheFillersIDsAreWhatContent289CallsThem(t *testing.T) {
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int]string{
		ObjGoblinMailOrange: "Orange goblin mail", ObjGoblinMailBlue: "Blue goblin mail", ObjGoblinMail: "Goblin mail",
		ObjRedDye: "Red dye", ObjYellowDye: "Yellow dye", ObjBlueDye: "Blue dye", ObjOrangeDye: "Orange dye",
		ObjWoadLeaf: "Woad leaf", ObjRedberries: "Redberries", ObjOnion: "Onion", CoinsID: "Coins",
		ObjCadavaBerries: "Cadava berries", ObjJulietMessage: "Message", ObjCadavaPotion: "Cadava potion",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want {
			t.Errorf("obj %d is %q (ok %v), want %q", id, o.Name, ok, want)
		}
		if o.Noted() {
			t.Errorf("obj %d is a note", id)
		}
		// Coins and woad leaves stack; the rest take a slot each, which is what the kits' Stackable flags say.
		if wantStack := id == CoinsID || id == ObjWoadLeaf; o.Stackable != wantStack {
			t.Errorf("obj %d stackable %v, want %v (the kits' pack-slot arithmetic follows it)", id, o.Stackable, wantStack)
		}
	}
	for id, want := range map[int]string{
		NpcRustyAnchorBartender: "Bartender", NpcGeneralBentnoze: "General Bentnoze", NpcGeneralWartface: "General Wartface",
		NpcGoblinGreenArmour: "Goblin", NpcGoblinRedArmour: "Goblin", NpcAggie: "Aggie", NpcWyson: "Wyson the gardener",
		NpcRomeo: "Romeo", NpcJuliet: "Juliet", NpcFatherLawrence: "Father Lawrence", NpcApothecary: "Apothecary",
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want {
			t.Errorf("npc %d is %q (ok %v), want %q", id, n.Name, ok, want)
			continue
		}
		hasOp := func(op string) bool {
			for _, o := range n.Ops {
				if strings.EqualFold(o, op) {
					return true
				}
			}
			return false
		}
		if id == NpcGoblinGreenArmour || id == NpcGoblinRedArmour {
			if !hasOp("Attack") {
				t.Errorf("npc %d (%s) has no Attack op: %v", id, n.Name, n.Ops)
			}
		} else if !hasOp("Talk-to") {
			t.Errorf("npc %d (%s) has no Talk-to op: %v", id, n.Name, n.Ops)
		}
	}
	l, ok := cfg.Loc(LocOnionPlant)
	if !ok || l.Name != "Onion" {
		t.Fatalf("loc %d is %q (ok %v)", LocOnionPlant, l.Name, ok)
	}
	pick := false
	for _, op := range l.Ops {
		pick = pick || strings.EqualFold(op, "Pick")
	}
	if !pick {
		t.Errorf("the onion plant offers %v, not Pick", l.Ops)
	}
}
