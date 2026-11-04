package questplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// doricSnap is an account on its way through Doric's Quest: the quest tab, the pack by object id (a repeated id is a repeated
// item), Mining level mining, a bank that holds nothing, and a position beside Doric's smithy.
func doricSnap(journal Status, mining int, held ...int) Snapshot {
	s := Snapshot{Journal: journal, InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: 20,
		BankKnown: true, BankIDs: map[int]int{}, Tile: tile.Tile{X: 2950, Z: 3440}, TileKnown: true,
		Levels: map[string]int{"mining": mining}, Failed: map[string]bool{}, Ran: map[string]bool{}}
	for _, id := range held {
		s.InvIDs[id]++
	}
	return s
}

func doricRepeat(id, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = id
	}
	return out
}

func doricPack(clay, copper, iron int, extra ...int) []int {
	out := append(doricRepeat(DoricClay, clay), doricRepeat(DoricCopperOre, copper)...)
	out = append(out, doricRepeat(DoricIronOre, iron)...)
	return append(out, extra...)
}

func doricRan(s Snapshot) Snapshot { s.Ran["provision:"+KitDoricOres] = true; return s }

func stepKey(step Step) string {
	switch step.Kind {
	case StepTalk:
		return step.Stop.Npc
	case StepCustom:
		return step.Custom
	case StepProvision:
		return step.KitName
	}
	return ""
}

func TestDoricsQuestEveryState(t *testing.T) {
	cases := []struct {
		name string
		snap Snapshot
		kind StepKind
		want string // npc, custom name or kit name, "" for any
	}{
		{"complete", doricSnap(StatusComplete, 1), StepDone, ""},
		{"the tab is not coloured yet", doricSnap(StatusUnknown, 1), StepWait, ""},
		{"not started: Doric, nothing gathered first", doricSnap(StatusNotStarted, 1), StepTalk, "Doric"},
		{"not started with the ores in the pack: Doric starts it, the hand-in comes after", doricSnap(StatusNotStarted, 1, doricPack(6, 4, 2)...), StepTalk, "Doric"},
		{"started with 6 clay, 4 copper and 2 iron: the hand-in", doricSnap(StatusInProgress, 1, doricPack(6, 4, 2)...), StepTalk, "Doric"},
		{"started with more than enough: the hand-in", doricSnap(StatusInProgress, 1, doricPack(9, 7, 5)...), StepTalk, "Doric"},
		{"started, empty pack: the bank and the mule first", doricSnap(StatusInProgress, 1), StepProvision, KitDoricOres},
		{"started, the pass failed on the way: not asked again, a pickaxe next", func() Snapshot {
			s := doricSnap(StatusInProgress, 1)
			s.Failed["provision:"+KitDoricOres] = true
			return s
		}(), StepProvision, KitDoricPick},
		{"the pass ran, no pickaxe: the pickaxe kit", doricRan(doricSnap(StatusInProgress, 1)), StepProvision, KitDoricPick},
		{"the pass ran, a pickaxe in the pack: mine", doricRan(doricSnap(StatusInProgress, 1, ObjBronzePickaxe)), StepCustom, CustomDoricMine},
		{"the pass ran, a worn pickaxe: mine", func() Snapshot {
			s := doricRan(doricSnap(StatusInProgress, 1))
			s.WornIDs[1267] = 1
			return s
		}(), StepCustom, CustomDoricMine},
		{"a rune pickaxe at Mining 1 is no pickaxe", doricRan(doricSnap(StatusInProgress, 1, 1275)), StepProvision, KitDoricPick},
		{"a rune pickaxe at Mining 41 is one", doricRan(doricSnap(StatusInProgress, 41, 1275)), StepCustom, CustomDoricMine},
		{"clay missing, copper and iron held: mine the clay", doricRan(doricSnap(StatusInProgress, 1, doricPack(0, 4, 2, ObjBronzePickaxe)...)), StepCustom, CustomDoricMine},
		{"only iron missing at Mining 14: parked for a level, not mined", doricRan(doricSnap(StatusInProgress, 14, doricPack(6, 4, 1, ObjBronzePickaxe)...)), StepWait, ""},
		{"only iron missing at Mining 14, no pickaxe either: still the level, not a pickaxe", doricRan(doricSnap(StatusInProgress, 14, doricPack(6, 4, 0)...)), StepWait, ""},
		{"only iron missing at Mining 15: mine it", doricRan(doricSnap(StatusInProgress, 15, doricPack(6, 4, 0, ObjBronzePickaxe)...)), StepCustom, CustomDoricMine},
		{"clay and iron missing at Mining 14: the clay is mined, the iron left", doricRan(doricSnap(StatusInProgress, 14, doricPack(0, 4, 0, ObjBronzePickaxe)...)), StepCustom, CustomDoricMine},
		{"a pack with no room banks first", func() Snapshot {
			s := doricRan(doricSnap(StatusInProgress, 1, ObjBronzePickaxe))
			s.FreeSlots = 9 // 6 clay + 4 copper = 10 owed at Mining 1
			return s
		}(), StepDeposit, ""},
		{"room for exactly the ores and the pickaxe", func() Snapshot {
			s := doricRan(doricSnap(StatusInProgress, 1))
			s.FreeSlots = 11 // 6 + 4 + the pickaxe
			return s
		}(), StepProvision, KitDoricPick},
	}
	for _, c := range cases {
		step := DecideDoricsQuest(c.snap)
		if why := ValidateStep(step); why != "" {
			t.Errorf("%s: malformed step: %s", c.name, why)
		}
		if step.Kind != c.kind {
			t.Errorf("%s: got %s (%s), want %s", c.name, step.Kind, step.Describe(), c.kind)
			continue
		}
		if got := stepKey(step); c.want != "" && got != c.want {
			t.Errorf("%s: got %q, want %q (%s)", c.name, got, c.want, step.Describe())
		}
	}
}

// The iron wait is a park with a reason and a timeout: it names the level and the ore, and it is hours, not a few waits.
func TestDoricsQuestParksForTheIronItCannotMine(t *testing.T) {
	step := DecideDoricsQuest(doricRan(doricSnap(StatusInProgress, 14, doricPack(6, 4, 1, ObjBronzePickaxe)...)))
	if step.Kind != StepWait || step.ParkFor != DoricIronParkFor || DoricIronParkFor < time.Hour {
		t.Fatalf("%s park %s", step.Describe(), step.ParkFor)
	}
	for _, want := range []string{"iron", "15"} {
		if !strings.Contains(step.Reason, want) {
			t.Errorf("the reason %q does not say %q", step.Reason, want)
		}
	}
	// Mining 15 is exactly iron's level (iron_rock_table rock_level 15).
	if DoricIronLevel != 15 {
		t.Errorf("iron needs Mining %d in the plan, 15 in iron_rock_table", DoricIronLevel)
	}
}

// The first pass asks the bank and the mule for all three ores and never sends the account to a source of its own: the ores
// a short bank cannot give are mined, so the pass is optional line by line and carries no ground, skill or shop source.
func TestDoricsQuestOrePassIsBankAndMuleOnly(t *testing.T) {
	step := DecideDoricsQuest(doricSnap(StatusInProgress, 1))
	if step.Kind != StepProvision || step.KitName != KitDoricOres || len(step.Kit) != 3 {
		t.Fatalf("%s", step.Describe())
	}
	want := map[int]int{DoricClay: 6, DoricCopperOre: 4, DoricIronOre: 2}
	for _, l := range step.Kit {
		if want[l.ID] != l.Qty || !l.Optional || len(l.Sources) != 0 || l.Stackable {
			t.Errorf("line %+v", l)
		}
		delete(want, l.ID)
	}
	if len(want) != 0 {
		t.Errorf("lines missing for %v", want)
	}
	if !step.BankSet {
		t.Error("the pass names no bank")
	}
	// Banking first must leave the ores, every pickaxe and the coins in the pack.
	keep := map[int]bool{}
	for _, id := range step.KeepIDs {
		keep[id] = true
	}
	for _, id := range []int{DoricClay, DoricCopperOre, DoricIronOre, CoinsID, 1265, 1267, 1269, 1271, 1273, 1275} {
		if !keep[id] {
			t.Errorf("a bank visit would deposit obj %d", id)
		}
	}
	// With the bank and the mule it asks for the shortfall only: held ores are subtracted by the planner.
	p := PlanProvision(step.Kit, ProvisionState{Pack: map[int]int{DoricClay: 6, DoricCopperOre: 1}, Bank: map[int]int{DoricCopperOre: 20, DoricIronOre: 5},
		BankKnown: true, FreeSlots: 20, Mule: map[int]int{DoricIronOre: 11811, DoricCopperOre: 5334}, MuleKnown: true})
	if len(p.Withdraw) != 2 || p.Phase != PhaseBank {
		t.Fatalf("plan %s", p.Summary())
	}
	got := map[int]int{}
	for _, w := range p.Withdraw {
		got[w.ID] = w.Count
	}
	if got[DoricCopperOre] != 3 || got[DoricIronOre] != 2 {
		t.Errorf("withdrawals %v, want 3 copper and 2 iron", got)
	}
	// The bank holds nothing and the mule holds copper and iron but no clay: one ask for the two, nothing else, and the clay
	// (optional, no source) does not block.
	p = PlanProvision(step.Kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20,
		Mule: map[int]int{DoricIronOre: 11811, DoricCopperOre: 5334}, MuleKnown: true})
	if p.Phase != PhaseMule || len(p.MuleAsk) != 2 {
		t.Fatalf("plan %s", p.Summary())
	}
	for _, a := range p.MuleAsk {
		if a.ID == DoricClay {
			t.Errorf("the mule publishes no clay and is asked for %d", a.Count)
		}
	}
	p = PlanProvision(step.Kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20, MuleAsked: true,
		Mule: map[int]int{DoricIronOre: 11811}, MuleKnown: true})
	if p.Phase != PhaseSatisfied {
		t.Errorf("a bank and a mule that hold nothing more must leave the pass satisfied, got %s", p.Summary())
	}
}

// The pickaxe kit follows the item rule: a pickaxe the bank holds that the account can use, else the free Bronze pickaxe on the
// table at the mine, then Bob's Axes; the ground spawn comes before the shop in the plan.
func TestDoricsQuestPickaxeKit(t *testing.T) {
	s := doricRan(doricSnap(StatusInProgress, 1))
	step := DecideDoricsQuest(s)
	if step.Kind != StepProvision || step.KitName != KitDoricPick || len(step.Kit) != 1 || step.Kit[0].ID != ObjBronzePickaxe {
		t.Fatalf("%s %+v", step.Describe(), step.Kit)
	}
	line := step.Kit[0]
	if len(line.Sources) != 2 {
		t.Fatalf("sources %+v", line.Sources)
	}
	byKind := map[SourceKind]Source{}
	for _, src := range line.Sources {
		byKind[src.Kind] = src
	}
	if g, ok := byKind[SourceGround]; !ok || g.Tile != RimmingtonPickaxe || g.RespawnTicks != 100 {
		t.Errorf("ground source %+v", g)
	}
	if sh, ok := byKind[SourceShop]; !ok || sh.Shop.NpcID != BobsAxes.NpcID || sh.Price != 1 {
		t.Errorf("shop source %+v", sh)
	}
	p := PlanProvision(step.Kit, ProvisionState{Pack: map[int]int{CoinsID: 50}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20})
	if p.Phase != PhaseSource || len(p.Tasks) != 1 || p.Tasks[0].Source.Kind != SourceGround {
		t.Errorf("plan %s: the ground spawn comes before the shop", p.Summary())
	}
	// An iron pickaxe in the bank at Mining 1 is drawn, and no source is listed for it.
	s.BankIDs[1267] = 1
	step = DecideDoricsQuest(s)
	if len(step.Kit) != 1 || step.Kit[0].ID != 1267 || len(step.Kit[0].Sources) != 0 {
		t.Errorf("a banked iron pickaxe: %+v", step.Kit)
	}
	// A banked steel pickaxe wants Mining 6, a banked rune one 41; the best the account can use is drawn.
	s.BankIDs = map[int]int{1275: 1, 1269: 1, 1265: 1}
	if step = DecideDoricsQuest(s); step.Kit[0].ID != 1265 {
		t.Errorf("at Mining 1 the banked pickaxes steel and rune are out of reach: %+v", step.Kit)
	}
	s.Levels["mining"] = 6
	if step = DecideDoricsQuest(s); step.Kit[0].ID != 1269 {
		t.Errorf("at Mining 6 the steel pickaxe: %+v", step.Kit)
	}
	s.Levels["mining"] = 41
	if step = DecideDoricsQuest(s); step.Kit[0].ID != 1275 {
		t.Errorf("at Mining 41 the rune pickaxe: %+v", step.Kit)
	}
	// An unread bank is no evidence of a pickaxe.
	s.BankKnown = false
	if step = DecideDoricsQuest(s); step.Kit[0].ID != ObjBronzePickaxe {
		t.Errorf("an unread bank: %+v", step.Kit)
	}
	// No pickaxe from anywhere: wait, never loop on the kit.
	s = doricRan(doricSnap(StatusInProgress, 1))
	s.Failed["provision:"+KitDoricPick] = true
	if step = DecideDoricsQuest(s); step.Kind != StepWait || ValidateStep(step) != "" {
		t.Errorf("a pickaxe kit that failed: %s", step.Describe())
	}
}

// The pickaxe levels are the content's (pickaxes.obj levelrequire), best first.
func TestDoricsQuestPickaxeLevels(t *testing.T) {
	want := map[int]int{1265: 0, 1267: 0, 1269: 6, 1273: 21, 1271: 31, 1275: 41}
	if len(doricPickaxes) != len(want) {
		t.Fatalf("%d pickaxes", len(doricPickaxes))
	}
	last := 1 << 30
	for _, p := range doricPickaxes {
		if lvl, ok := want[p.ID]; !ok || lvl != p.Level {
			t.Errorf("pickaxe %d (%s) level %d, content says %d (known %v)", p.ID, p.Name, p.Level, lvl, ok)
		}
		if p.Level > last {
			t.Errorf("%s comes after a pickaxe of a lower level: best first", p.Name)
		}
		last = p.Level
	}
	// The best one the account can use wins, in the pack or worn.
	s := doricSnap(StatusInProgress, 22, 1265, 1269, 1275)
	s.WornIDs[1273] = 1
	if got := DoricUsablePickaxe(s); got != 1273 {
		t.Errorf("usable %d, want the worn mithril pickaxe 1273 (rune needs 41)", got)
	}
	if DoricUsablePickaxe(doricSnap(StatusInProgress, 1)) != 0 {
		t.Error("an empty pack has a usable pickaxe")
	}
}

// Every rock id the miner looks for is one of the pair for its ore, and the ores' quantities are what Doric wants.
func TestDoricsQuestConstants(t *testing.T) {
	if DoricClayQty != 6 || DoricCopperQty != 4 || DoricIronQty != 2 {
		t.Errorf("quantities %d %d %d, Doric wants 6, 4 and 2", DoricClayQty, DoricCopperQty, DoricIronQty)
	}
	if DoricsQuestRecord.QuestPoints != 1 || DoricsQuestRecord.ID != "doric" || DoricsQuestRecord.Name != "Doric's Quest" {
		t.Errorf("record %+v", DoricsQuestRecord)
	}
	m := DoricsQuest
	if m.MaxDeaths != 1 || m.ReadStage == nil || m.Decide == nil || !m.BankSet {
		t.Errorf("module %+v", m)
	}
	if len(DoricKeepIDs()) != len(doricKeep) {
		t.Error("the keep list copy")
	}
}

// Every step the module can return validates, over a sweep of packs and levels.
func TestDoricsQuestEveryReachableStepValidates(t *testing.T) {
	for _, journal := range []Status{StatusNotStarted, StatusInProgress, StatusUnknown, StatusComplete} {
		for _, mining := range []int{1, 14, 15, 99} {
			for _, clay := range []int{0, 3, 6} {
				for _, copper := range []int{0, 4} {
					for _, iron := range []int{0, 2} {
						for _, pick := range []bool{false, true} {
							for _, ran := range []bool{false, true} {
								var extra []int
								if pick {
									extra = []int{ObjBronzePickaxe}
								}
								s := doricSnap(journal, mining, doricPack(clay, copper, iron, extra...)...)
								if ran {
									s = doricRan(s)
								}
								step := DecideDoricsQuest(s)
								if why := ValidateStep(step); why != "" {
									t.Fatalf("%s mining %d pack %d/%d/%d pick %v ran %v: %s (%s)", journal, mining, clay, copper, iron, pick, ran, why, step.Describe())
								}
								// A bank visit of any step leaves the ores, every pickaxe and the coins in the pack.
								if step.Kind == StepProvision || step.Kind == StepDeposit {
									keep := map[int]bool{}
									for _, id := range step.KeepIDs {
										keep[id] = true
									}
									for _, id := range doricKeep {
										if !keep[id] {
											t.Fatalf("%s: %s would deposit obj %d", journal, step.Describe(), id)
										}
									}
									if !step.BankSet {
										t.Fatalf("%s: %s names no bank", journal, step.Describe())
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

// ParseDoricsQuestJournal against the journal as the server renders it (doric_journal.rs2: `|` is the client's line break).
func TestDoricsQuestJournal(t *testing.T) {
	for stage, text := range doricJournalFixtures {
		got, ok := ParseDoricsQuestJournal([]string{text})
		if !ok || got != stage {
			t.Errorf("stage %d: parsed %d (known %v) from %q", stage, got, ok, text)
		}
	}
	// In progress the three ingredient lines are struck through once the pack holds them: that changes no stage.
	for _, text := range doricJournalStage10 {
		if got, ok := ParseDoricsQuestJournal([]string{text}); !ok || got != DoricStarted {
			t.Errorf("parsed %d (known %v) from %q", got, ok, text)
		}
	}
	for _, none := range [][]string{nil, {""}, {"@dbl@Something else entirely"}} {
		if got, ok := ParseDoricsQuestJournal(none); ok {
			t.Errorf("%q parsed as stage %d, want unknown", none, got)
		}
	}
	// The same text split over client lines parses the same.
	got, ok := ParseDoricsQuestJournal(strings.Split(doricJournalFixtures[DoricStarted], "|"))
	if !ok || got != DoricStarted {
		t.Errorf("split lines: %d %v", got, ok)
	}
}

var doricJournalFixtures = map[int]string{
	0:   "@dbl@I can start this quest by talking to @dre@Doric@dbl@ at his home|north of @dre@Falador@dbl@.",
	10:  "@str@I have spoken to @dre@Doric||@dbl@I need to collect some items and bring them to @dre@Doric|@dre@6 Clay|@dre@4 Copper Ore|@dre@2 Iron Ore|",
	100: "@str@I have spoken to @dre@Doric||@str@I have collected some Clay, Copper Ore, and Iron Ore.||@str@Doric rewarded me for all my hard work.|@str@I can now use Doric's Anvils whenever I want.|@red@QUEST COMPLETE!",
}

var doricJournalStage10 = []string{
	"@str@I have spoken to @dre@Doric||@dbl@I need to collect some items and bring them to @dre@Doric|@str@6 Clay|@dre@4 Copper Ore|@dre@2 Iron Ore|",
	"@str@I have spoken to @dre@Doric||@dbl@I need to collect some items and bring them to @dre@Doric|@str@6 Clay|@str@4 Copper Ore|@str@2 Iron Ore|",
}
