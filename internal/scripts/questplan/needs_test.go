package questplan

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/levels"
)

// readSource reads a source file of this package with its line endings folded (the repository's files are CRLF).
func readSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// accountAt is a subject whose skills sit exactly at the given base levels (experience at the level's start).
func accountAt(lv map[string]int) Subject {
	a := Account{XP: map[string]int{}, Done: map[string]bool{}}
	for s, l := range lv {
		a.XP[s] = levels.XPAtLevel(l)
	}
	return Subject{Account: a, Own: map[int]int{}, MuleStock: map[int]int{}, Needs: map[string]NeedHistory{}}
}

func needKeys(ns []Need) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.Key())
	}
	return out
}

// The 14 October 2026 lesson: Record and Facts must say the same thing, or say why not. Every record the package owns, and
// the two lists of deliberate differences held exact.
func TestRecordRequirementsMatchFacts(t *testing.T) {
	// Waterfall registers its own record from waterfallplan (which this package cannot import), and the Barcrawl declares none.
	noRecord := map[string]string{
		"waterfall": "its record is registered by waterfallplan's init; internal/bot checks it against the facts",
		"barcrawl":  "questmods declares a record with no requirement at all",
	}
	for id, f := range LadderFacts {
		if f.TrainSkill != "" {
			continue
		}
		r, ok := RecordOf(id)
		if !ok {
			if _, allowed := noRecord[id]; !allowed {
				t.Errorf("quest %q has facts and no record in QuestRecords", id)
			}
			continue
		}
		for _, why := range RecordFactsDisagreements(id, f, r) {
			t.Error(why)
		}
	}
	for id, r := range QuestRecords {
		if r.ID != id {
			t.Errorf("QuestRecords[%q] holds the record of %q", id, r.ID)
		}
		if _, ok := LadderFacts[id]; !ok {
			t.Errorf("QuestRecords has %q, which LadderFacts does not know", id)
		}
	}
}

// The three lists of deliberate differences name only differences that exist: a gate that is now stated on both sides, or has
// left one, comes off its list.
func TestTheExceptionListsAreExact(t *testing.T) {
	if probs := ExceptionProblems(LadderFacts, RecordOf); len(probs) > 0 {
		for _, p := range probs {
			t.Error(p)
		}
	}
	// The check sees a stale entry.
	facts := map[string]RungFacts{"x": {ID: "x", Gates: []Gate{{Skill: "mining", Level: 5, Kind: GateServer}}}}
	rec := Record{ID: "x", Requirements: Requirements{Skills: []SkillReq{{Skill: "mining", Level: 5}}}}
	FactsOnlyGates["x:mining"] = "test"
	defer delete(FactsOnlyGates, "x:mining")
	probs := strings.Join(ExceptionProblems(facts, func(string) (Record, bool) { return rec, true }), "\n")
	if !strings.Contains(probs, "FactsOnlyGates lists x:mining") {
		t.Errorf("a stale entry is not found: %s", probs)
	}
}

// RecordFactsDisagreements finds each kind of disagreement.
func TestRecordFactsDisagreementsFindsEachKind(t *testing.T) {
	f := RungFacts{ID: "q", Gates: []Gate{{Skill: "mining", Level: 20, Kind: GateServer}, {Skill: "crafting", Level: 10, Kind: GateAdvised}}, QPNeeded: 5, Prereqs: []string{"a"}}
	r := Record{ID: "q", Requirements: Requirements{Skills: []SkillReq{{Skill: "smithing", Level: 5}, {Skill: "crafting", Level: 12}}, MinQuestPoints: 4, Quests: []string{"b"}}}
	got := strings.Join(RecordFactsDisagreements("q", f, r), "\n")
	for _, want := range []string{
		"the Record requires smithing 5 and LadderFacts has no gate",
		"the Record requires crafting 12 and LadderFacts gates it at 10",
		"LadderFacts gates mining 20 (hard) and the Record states none",
		"wants 5 quest points and the Record 4",
		"the Record requires quest b and LadderFacts does not list it",
		"LadderFacts requires quest a and the Record does not",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	// An advised fact below a hard Record gate is the same level or a disagreement, never silence.
	agree := RecordFactsDisagreements("q", RungFacts{ID: "q", Gates: []Gate{{Skill: "mining", Level: 20, Kind: GateAdvised}}},
		Record{ID: "q", Requirements: Requirements{Skills: []SkillReq{{Skill: "mining", Level: 20}}}})
	if len(agree) != 0 {
		t.Errorf("an advised gate at the record's level is agreement: %v", agree)
	}
}

// Every requirement the model derives has a way to be met or a named reason it has none (plan 3.2, test list; a superset of
// bot.TestEveryHardGateHasARungThatProducesIt, which holds the rungs' factories).
func TestEveryRequirementHasAnAcquirerOrAReason(t *testing.T) {
	table := DefaultNeedTable()
	cat := DefaultCatalogue()
	if p := cat.Problems(); len(p) > 0 {
		t.Fatalf("catalogue problems: %v", p)
	}
	for _, id := range table.Quests() {
		for _, n := range table.Requirements(id) {
			switch n.Kind {
			case NeedSkill:
				if u := cat.FirstUncovered(n.Skill, 1, n.Level); u != 0 {
					if g, ok := GapFor(n.Skill, u); !ok || g.Lane == "" || g.Reason == "" {
						t.Errorf("%s needs %s: no trainer covers level %d and no gap names it", id, n.Describe(), u)
					}
				}
			case NeedCombat:
				for _, skill := range []string{"attack", "strength", "defence", "hitpoints"} {
					if rows := cat.For(skill, 1); len(rows) == 0 || rows[0].Loop != "stage5-combat" {
						t.Errorf("%s needs %s but %s has no Stage 5 combat loop row", id, n.Describe(), skill)
					}
				}
			case NeedQuest:
				if _, ok := table.Facts(n.Prereq); !ok {
					t.Errorf("%s needs quest %q, which has no facts", id, n.Prereq)
				}
			case NeedQuestPoints:
				total := 0
				for _, q := range table.Quests() {
					f, _ := table.Facts(q)
					total += f.QP
				}
				if total < n.Level {
					t.Errorf("%s needs %d quest points and every quest of the table pays only %d", id, n.Level, total)
				}
			case NeedItem:
				// checked below, per item
			default:
				t.Errorf("%s: unknown need kind %q", id, n.Kind)
			}
		}
	}

	for _, in := range ItemNeeds {
		if in.Item <= 0 || in.Name == "" || in.Qty <= 0 || len(in.Quests) == 0 || in.Source == "" || len(in.Chain) == 0 {
			t.Errorf("item need %q is malformed: %+v", in.Name, in)
			continue
		}
		built := 0
		var last = -1
		for _, l := range in.Chain {
			if r := ruleRank(l.Kind); r < last {
				t.Errorf("%s: the chain is out of the owner's item rule order at %s", in.Name, l.Kind)
			} else {
				last = r
			}
			if l.Kind != SourceBank && l.Kind != SourceMule && l.Built {
				built++
			}
			if l.Kind == SourceSkill {
				for _, req := range append([]SkillReq{{Skill: l.Skill, Level: l.Level}}, l.Then...) {
					if req.Level <= 1 {
						continue
					}
					if u := cat.FirstUncovered(req.Skill, 1, req.Level); u != 0 {
						if _, ok := GapFor(req.Skill, u); !ok {
							t.Errorf("%s: its %s link needs %s %d, no trainer covers level %d and no gap names it", in.Name, l.Method, req.Skill, req.Level, u)
						}
					}
				}
			}
		}
		hasMule := false
		for _, l := range in.Chain {
			hasMule = hasMule || l.Kind == SourceMule
		}
		switch {
		case in.Gap == "" && built == 0 && !hasMule:
			t.Errorf("%s has no built source and no gap named", in.Name)
		case in.Gap != "" && built > 0:
			t.Errorf("%s names a gap (%s) but has a built non-skill source", in.Name, in.Gap)
		}
		if in.Gap != "" && in.Lane == "" {
			t.Errorf("%s names a gap and no lane", in.Name)
		}
		for _, q := range in.Quests {
			if _, ok := LadderFacts[q]; !ok {
				t.Errorf("%s is wanted by %q, which has no facts", in.Name, q)
			}
		}
	}
}

// A gap that the catalogue now covers is stale, and a gap's boundary is exactly where the coverage ends: the lane that lands a
// trainer takes its gap off the list.
func TestNoGapIsStale(t *testing.T) {
	cat := DefaultCatalogue()
	seen := map[string]bool{}
	for _, g := range SkillGaps {
		if g.Skill == "" || g.From < 1 || g.Lane == "" || g.Reason == "" {
			t.Errorf("gap %+v is malformed", g)
		}
		if seen[g.Skill] {
			t.Errorf("skill %s has two gaps", g.Skill)
		}
		seen[g.Skill] = true
		if len(cat.For(g.Skill, g.From)) != 0 {
			t.Errorf("gap %s from %d is stale: %s now trains that level", g.Skill, g.From, cat.For(g.Skill, g.From)[0].ID)
		}
		if g.From > 1 && len(cat.For(g.Skill, g.From-1)) == 0 {
			t.Errorf("gap %s from %d starts above uncovered ground: level %d has no trainer either", g.Skill, g.From, g.From-1)
		}
	}
}

// The skill levels of the kit lines' skill sources (the fifth source) are kept in the item chains: a Source{Kind: SourceSkill}
// with a level in a quest's own file that no chain carries is a requirement the model cannot see.
func TestEveryKitSkillSourceIsAChainLink(t *testing.T) {
	re := regexp.MustCompile(`Kind: SourceSkill, Skill: "([a-z]+)", Level: ([0-9]+)`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "needsitems.go" {
			continue // the chains themselves
		}
		src := readSource(t, f)
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			skill, level := m[1], atoi(m[2])
			if level <= 1 {
				continue
			}
			quests := questsOfFile(f)
			if len(quests) == 0 {
				t.Errorf("%s holds a %s %d skill source and no quest's ModuleFiles names the file", f, skill, level)
				continue
			}
			checked++
			found := false
			for _, q := range quests {
				for _, in := range ItemChainFor(q) {
					for _, l := range in.Chain {
						if l.Kind != SourceSkill {
							continue
						}
						if l.Skill == skill && l.Level == level {
							found = true
						}
						for _, th := range l.Then {
							found = found || (th.Skill == skill && th.Level == level)
						}
					}
				}
			}
			if !found {
				t.Errorf("%s: the %s %d skill source is in no item chain of %v", f, skill, level, quests)
			}
		}
	}
	if checked < 3 {
		t.Errorf("only %d skill sources with a level were found: the pattern no longer matches the source", checked)
	}
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// questsOfFile is the quests whose ModuleFiles name the questplan file.
func questsOfFile(name string) []string {
	var out []string
	want := "internal/scripts/questplan/" + name
	for q, files := range ModuleFiles {
		for _, f := range files {
			if f == want {
				out = append(out, q)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// RequirementsOf merges the five sources: facts, record, soft prerequisites, kit chains and module floors.
func TestRequirementsMergeTheFiveSources(t *testing.T) {
	has := func(id string, want Need) {
		t.Helper()
		for _, n := range RequirementsOf(id) {
			if n.Kind == want.Kind && n.Key() == want.Key() {
				if want.Strength != "" && n.Strength != want.Strength {
					t.Errorf("%s: %s is %s, want %s", id, n.Key(), n.Strength, want.Strength)
				}
				for _, o := range want.Origins {
					found := false
					for _, got := range n.Origins {
						found = found || got == o
					}
					if !found {
						t.Errorf("%s: %s origins %v lack %s", id, n.Key(), n.Origins, o)
					}
				}
				return
			}
		}
		t.Errorf("%s: no %s among %v", id, want.Key(), needKeys(RequirementsOf(id)))
	}
	// Facts and record both state it, the record hard: one hard need with both origins.
	has("elemental_workshop", Need{Kind: NeedSkill, Skill: "mining", Level: 20, Strength: StrengthHard, Origins: []Origin{OriginFacts, OriginRecord}})
	// Facts only, a server gate.
	has("scorpion", Need{Kind: NeedSkill, Skill: "prayer", Level: 31, Strength: StrengthHard, Origins: []Origin{OriginFacts}})
	// Record only: the facts row leaves a floor to the Record (RecordOnlyGates).
	has("vampire", Need{Kind: NeedSkill, Skill: "hitpoints", Level: 30, Strength: StrengthHard, Origins: []Origin{OriginRecord}})
	// A soft prerequisite.
	has("pip", Need{Kind: NeedQuest, Prereq: RuneMysteriesID, Strength: StrengthHard, Origins: []Origin{OriginSoft}})
	// A hard prerequisite from both the facts and the record.
	has("grail", Need{Kind: NeedQuest, Prereq: MerlinsCrystalID, Strength: StrengthHard, Origins: []Origin{OriginFacts, OriginRecord}})
	// The quest-point minimum.
	has("dragon", Need{Kind: NeedQuestPoints, Level: 32, Strength: StrengthHard, Origins: []Origin{OriginFacts, OriginRecord}})
	// The module's own floor, which neither table states.
	has("arena", Need{Kind: NeedSkill, Skill: "hitpoints", Level: ArenaMinHitpoints, Strength: StrengthHard, Origins: []Origin{OriginModule}})
	has("demon", Need{Kind: NeedCombat, Level: DemonMinCombat, Strength: StrengthHard, Origins: []Origin{OriginModule}})
	// The kit's item.
	has("spirit", Need{Kind: NeedItem, ItemID: ObjSilverBar, Qty: 1, Strength: StrengthHard, Origins: []Origin{OriginKit}})
	// An advised combat level.
	has("merlin", Need{Kind: NeedCombat, Level: 30, Strength: StrengthAdvised, Origins: []Origin{OriginFacts}})
	// A training rung is not a quest.
	if got := RequirementsOf("agility25"); len(got) != 0 {
		t.Errorf("a training rung has needs: %v", got)
	}
}

// The merge rules, on a table small enough to read.
func TestBuildNeedTableMergeRules(t *testing.T) {
	facts := map[string]RungFacts{
		"a": {ID: "a", QPNeeded: 5, MinCombat: 30,
			Gates: []Gate{
				{Skill: "Mining", Level: 20, Kind: GateAdvised},
				{Skill: "smithing", Level: 10, Kind: GateItem, WaiverItem: 77, WaiverQty: 2},
				{Skill: "crafting", Level: 15, Kind: GateServer},
				{Skill: "crafting", Level: 15, Kind: GateItem, WaiverItem: 5, WaiverQty: 1},
			},
			Prereqs: []string{"b"}, SoftPrereqs: []SoftPrereq{{ID: "b", WaiverItem: 9, WaiverQty: 1}, {ID: "c", WaiverItem: 9, WaiverQty: 1}}},
		"b": {ID: "b"},
		"c": {ID: "c"},
		"r": {ID: "r", TrainSkill: "agility", TrainLevel: 5},
	}
	rec := Record{ID: "a", Requirements: Requirements{MinQuestPoints: 8, Skills: []SkillReq{{Skill: "mining", Level: 25}}, Quests: []string{"b"}}}
	tab := BuildNeedTable(NeedSources{
		Facts:  facts,
		Record: func(id string) (Record, bool) { return rec, id == "a" },
		Floors: []ModuleFloor{{Quest: "a", Skill: "hitpoints", Level: 40, Strength: StrengthHard}, {Quest: "a", Skill: CombatSkill, Level: 40, Strength: StrengthHard}},
	})
	byKey := map[string]Need{}
	for _, n := range tab.Requirements("a") {
		byKey[n.Key()] = n
	}
	if n, ok := byKey["qp:8"]; !ok || len(n.Origins) != 2 {
		t.Errorf("the quest-point need is the larger of the two, with both origins: %+v", byKey)
	}
	if n, ok := byKey["skill:mining:25"]; !ok || n.Strength != StrengthHard {
		t.Errorf("mining: the record's hard 25 beats the facts' advised 20: %v", needKeys(tab.Requirements("a")))
	}
	if _, ok := byKey["skill:mining:20"]; ok {
		t.Error("an advised level a hard one covers is dropped")
	}
	if n := byKey["skill:smithing:10"]; n.WaiverItem != 77 || n.WaiverQty != 2 {
		t.Errorf("an item gate keeps its waiver: %+v", n)
	}
	if n := byKey["skill:crafting:15"]; n.WaiverItem != 0 {
		t.Errorf("a level a source states with no waiver binds it: %+v", n)
	}
	if n := byKey["quest:b"]; n.WaiverItem != 0 || len(n.Origins) != 3 {
		t.Errorf("a hard prerequisite beats the waivable one, with all three origins: %+v", n)
	}
	if n := byKey["quest:c"]; n.WaiverItem != 9 {
		t.Errorf("a soft prerequisite keeps its waiver: %+v", n)
	}
	if n, ok := byKey["combat:40"]; !ok || n.Strength != StrengthHard {
		t.Errorf("the module's combat floor is hard and covers the advised 30: %v", needKeys(tab.Requirements("a")))
	}
	if _, ok := byKey["skill:hitpoints:40"]; !ok {
		t.Error("the module's hitpoints floor is a need")
	}
	if got := tab.Requirements("r"); len(got) != 0 {
		t.Errorf("a training rung has no needs: %v", got)
	}
	// The merge is deterministic: the same sources give the same list, origins in the fixed order.
	again := BuildNeedTable(NeedSources{Facts: facts, Record: func(id string) (Record, bool) { return rec, id == "a" }})
	for i := 0; i < 20; i++ {
		if !reflect.DeepEqual(again.Requirements("a"), BuildNeedTable(NeedSources{Facts: facts, Record: func(id string) (Record, bool) { return rec, id == "a" }}).Requirements("a")) {
			t.Fatal("the merged needs differ between builds")
		}
	}
}

// Levels are BASE levels from experience: one point of experience below the level's start is the level below.
func TestUnmetReadsBaseLevelsFromExperience(t *testing.T) {
	sub := accountAt(map[string]int{"hitpoints": 20})
	sub.XP["hitpoints"]--
	unmet, _ := UnmetOf(sub, "mcannon")
	if len(unmet) != 1 || unmet[0].Key() != "skill:hitpoints:20" {
		t.Fatalf("Hitpoints at 4,469 experience is level 19, below the cannon's 20: %v", needKeys(unmet))
	}
	sub.XP["hitpoints"]++
	if unmet, _ = UnmetOf(sub, "mcannon"); len(unmet) != 0 {
		t.Fatalf("Hitpoints at 4,470 experience is level 20: %v", needKeys(unmet))
	}
	// No account is below 10 Hitpoints; an experience reading of 0 is a missing one, not level 1.
	if got := (Subject{Account: Account{XP: map[string]int{"hitpoints": 0}}}).Level("hitpoints"); got != 10 {
		t.Errorf("Hitpoints at 0 experience reads %d, want 10", got)
	}
}

// A level a prerequisite quest pays is not a gap for an account that has not done it yet: Jungle Potion wants Herblore 3 and
// Druidic Ritual pays 250 Herblore experience.
func TestUnmetCountsTheRewardsOfUnmetPrerequisites(t *testing.T) {
	sub := accountAt(map[string]int{"hitpoints": 40})
	unmet, _ := UnmetOf(sub, JunglePotionID)
	keys := needKeys(HardUnmet(unmet))
	if len(keys) != 1 || keys[0] != "quest:"+DruidicRitualID {
		t.Fatalf("a fresh Herblore 1 account is waiting only for Druidic Ritual: %v", keys)
	}
	// Without the prerequisite's reward the same need is a gap: a quest with no pending prerequisite does not borrow one.
	only := BuildNeedTable(NeedSources{
		Facts: map[string]RungFacts{"x": {ID: "x", Gates: []Gate{{Skill: "herblore", Level: 3, Kind: GateServer}}}},
	})
	if u, _ := only.Unmet(sub, DefaultNeedsSettings(), "x"); len(u) != 1 || u[0].Key() != "skill:herblore:3" {
		t.Errorf("Herblore 3 with no quest to pay it is unmet: %v", needKeys(u))
	}
	// Done quests need nothing.
	sub.Done[JunglePotionID] = true
	if u, _ := UnmetOf(sub, JunglePotionID); u != nil {
		t.Errorf("a done quest has unmet needs: %v", needKeys(u))
	}
}

// An item gate is waived by the owned item, and without a read of the holdings it is not judged at all.
func TestUnmetJudgesWaiversOnlyFromReadHoldings(t *testing.T) {
	sub := accountAt(map[string]int{"smithing": 1, "hitpoints": 50, "agility": 40, "prayer": 50})
	sub.Done[barcrawlID] = true
	unmet, unjudged := UnmetOf(sub, HorrorID)
	if hasKey(unmet, "skill:smithing:34") || !hasKey(unjudged, "skill:smithing:34") {
		t.Errorf("an unread bank is not an empty one: unmet %v unjudged %v", needKeys(unmet), needKeys(unjudged))
	}
	// A level that is met needs no waiver, so an unread bank does not make it unjudged.
	met := accountAt(map[string]int{"smithing": 40, "hitpoints": 50, "agility": 40, "prayer": 50})
	met.Done[barcrawlID] = true
	if unmet, unjudged = UnmetOf(met, HorrorID); hasKey(unmet, "skill:smithing:34") || hasKey(unjudged, "skill:smithing:34") {
		t.Errorf("Smithing 40 meets the gate whatever the bank holds: unmet %v unjudged %v", needKeys(unmet), needKeys(unjudged))
	}
	// A read bank with no nails: an ordinary account lacks the NAILS (the producer makes them, plan 3.5) and trains no Smithing for
	// them; the designated producer lacks the Smithing 34.
	sub.OwnKnown = true
	nails := "item:" + strconv.Itoa(ObjNails) + ":8"
	if unmet, _ = UnmetOf(sub, HorrorID); hasKey(unmet, "skill:smithing:34") || !hasKey(unmet, nails) {
		t.Errorf("a read bank with no nails leaves the nails unmet and not Smithing 34: %v", needKeys(unmet))
	}
	sub.IsProducer = true
	if unmet, _ = UnmetOf(sub, HorrorID); !hasKey(unmet, "skill:smithing:34") || hasKey(unmet, nails) {
		t.Errorf("the producer trains Smithing 34 for the nails it makes: %v", needKeys(unmet))
	}
	sub.Own[ObjNails] = 8
	if unmet, _ = UnmetOf(sub, HorrorID); hasKey(unmet, "skill:smithing:34") || hasKey(unmet, nails) {
		t.Errorf("8 owned nails waive Smithing 34 and are the nails: %v", needKeys(unmet))
	}
	sub.IsProducer = false
	if unmet, _ = UnmetOf(sub, HorrorID); hasKey(unmet, "skill:smithing:34") || hasKey(unmet, nails) {
		t.Errorf("8 owned nails leave an ordinary account nothing to ask for: %v", needKeys(unmet))
	}
}

// barcrawlID is the barcrawl's id as the facts spell it (questmods.BarcrawlID is the same string).
const barcrawlID = "barcrawl"

func hasKey(ns []Need, key string) bool {
	for _, n := range ns {
		if n.Key() == key {
			return true
		}
	}
	return false
}

// JudgeItem walks the owner's item rule: its own holdings, the mule if it can be reached, a source it can use, then training.
func TestJudgeItemWalksTheOwnersRule(t *testing.T) {
	set := DefaultNeedsSettings()
	in := ItemNeed{Item: 100, Name: "Thing", Qty: 2, Quests: []string{"q"}, WaivedBy: 7, WaivedQty: 1,
		Chain: []ChainLink{
			{Kind: SourceBank}, {Kind: SourceMule},
			{Kind: SourceShop, Built: false},
			{Kind: SourceSkill, Skill: "mining", Level: 20, Then: []SkillReq{{Skill: "smithing", Level: 20}}, Method: "mine-thing", Built: true},
		}}
	sub := accountAt(map[string]int{"mining": 5, "smithing": 25})
	sub.World, sub.MuleWorld, sub.MuleKnown = 2, 1, true

	if j := JudgeItem(sub, set, in); j.State != ItemUnjudged {
		t.Errorf("unread holdings: %v", j.State)
	}
	sub.OwnKnown = true
	if j := JudgeItem(sub, set, in); j.State != ItemTrain || len(j.Skills) != 1 || j.Skills[0].Key() != "skill:mining:20" || j.Skills[0].ServesItem != 100 {
		t.Errorf("nothing held, no reachable mule, only training open: %+v", j)
	}
	// The mule holds it on another world: with no hop the account trains; the verdict is the far mule only when nothing else opens.
	sub.MuleStock[100] = 5
	if j := JudgeItem(sub, set, in); j.State != ItemTrain {
		t.Errorf("a mule on another world does not stop the next link: %+v", j)
	}
	noTrain := in
	noTrain.Chain = in.Chain[:3]
	if j := JudgeItem(sub, set, noTrain); j.State != ItemMuleFar {
		t.Errorf("a mule on another world and nothing else: %+v", j)
	}
	set.MuleHop = true
	if j := JudgeItem(sub, set, noTrain); j.State != ItemMuleHas {
		t.Errorf("with the hop allowed the mule is reachable: %+v", j)
	}
	set.MuleHop = false
	sub.MuleWorld = 2
	if j := JudgeItem(sub, set, in); j.State != ItemMuleHas || j.Link != 1 {
		t.Errorf("the mule on the account's world: %+v", j)
	}
	// The mule holds less than the shortfall: that link does not open.
	sub.MuleStock[100] = 1
	if j := JudgeItem(sub, set, in); j.State != ItemTrain {
		t.Errorf("a mule holding 1 of 2 wanted does not cover it: %+v", j)
	}
	// Its own holdings, and an owned waiver.
	sub.Own[100] = 2
	if j := JudgeItem(sub, set, in); j.State != ItemHeld {
		t.Errorf("held: %+v", j)
	}
	sub.Own[100] = 0
	sub.Own[7] = 1
	if j := JudgeItem(sub, set, in); j.State != ItemHeld {
		t.Errorf("waived by an owned item: %+v", j)
	}
	// A skill link whose levels the account meets is a source it can use now.
	sub.Own = map[int]int{}
	sub2 := accountAt(map[string]int{"mining": 25, "smithing": 25})
	sub2.OwnKnown = true
	if j := JudgeItem(sub2, set, in); j.State != ItemProvisioner || j.Link != 3 {
		t.Errorf("the skill source can run now: %+v", j)
	}
	// A link that is not built is not a way; nothing open is stuck, with the gap as the reason.
	stuck := ItemNeed{Item: 101, Name: "Other", Qty: 1, Gap: "no source (lane X)", Chain: []ChainLink{{Kind: SourceBank}, {Kind: SourceSkill, Skill: "fishing", Level: 20, Method: "fish", Built: false}}}
	if j := JudgeItem(sub2, set, stuck); j.State != ItemStuck || j.Reason != "no source (lane X)" {
		t.Errorf("nothing open: %+v", j)
	}
	// A world-restricted source is not open elsewhere.
	market := ItemNeed{Item: 102, Name: "Rune", Qty: 1, Chain: []ChainLink{{Kind: SourceMarket, Built: true, Worlds: []int{1}}}}
	sub2.World = 3
	if j := JudgeItem(sub2, set, market); j.State != ItemStuck {
		t.Errorf("the Seers market is on world 1 only: %+v", j)
	}
	sub2.World = 1
	if j := JudgeItem(sub2, set, market); j.State != ItemProvisioner {
		t.Errorf("on world 1 it opens: %+v", j)
	}
}

// An unmet item is returned only when the provisioner cannot get it, and a training link adds its skill needs once.
func TestUnmetReturnsItemsOnlyWhenTheProvisionerCannotGetThem(t *testing.T) {
	sub := accountAt(map[string]int{"mining": 1, "smithing": 1, "hitpoints": 60, "attack": 60, "strength": 60, "defence": 60})
	sub.OwnKnown, sub.MuleKnown, sub.World, sub.MuleWorld = true, true, 1, 1
	sub.Done[PriestInPerilID], sub.Done[RestlessGhostID] = true, true
	sub.MuleStock[ObjSilverBar] = 100
	unmet, _ := UnmetOf(sub, NatureSpiritID)
	if hasKey(unmet, "item:"+strconv.Itoa(ObjSilverBar)+":1") || hasKey(unmet, "skill:mining:20") {
		t.Errorf("the mule can answer, so the silver bar is not unmet: %v", needKeys(unmet))
	}
	sub.MuleStock[ObjSilverBar] = 0
	unmet, _ = UnmetOf(sub, NatureSpiritID)
	if !hasKey(unmet, "skill:mining:20") || !hasKey(unmet, "skill:smithing:20") {
		t.Errorf("with the mule empty the chain falls to training: %v", needKeys(unmet))
	}
	for _, n := range unmet {
		if n.Skill == "mining" && n.ServesItem != ObjSilverBar {
			t.Errorf("the mining need says what it is for: %+v", n)
		}
	}
	// The trout have no source at all: the item itself is unmet, with the gap as its note.
	trout, _ := UnmetOf(sub, DeathPlateauID)
	found := false
	for _, n := range trout {
		if n.Kind == NeedItem && n.ItemID == ObjTrout {
			found = strings.Contains(n.Note, "no trout source")
		}
	}
	if !found {
		t.Errorf("trout: %v", needKeys(trout))
	}
}

// Need.Key and Describe are stable and distinct.
func TestNeedKeysAreDistinctAndDescribed(t *testing.T) {
	seen := map[string]string{}
	for _, id := range DefaultNeedTable().Quests() {
		for _, n := range RequirementsOf(id) {
			if n.Describe() == "" || n.Key() == "" {
				t.Errorf("%s: %+v has no key or description", id, n)
			}
			if prev, ok := seen[n.Key()]; ok && prev != n.Describe() {
				t.Errorf("key %s is described %q and %q", n.Key(), prev, n.Describe())
			}
			seen[n.Key()] = n.Describe()
		}
	}
}
