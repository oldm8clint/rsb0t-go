package questplan

import (
	"strings"
	"testing"
)

func reportWorld() (NeedTable, Catalogue) {
	facts := map[string]RungFacts{
		"a": {ID: "a", Name: "A", QP: 2, Gates: []Gate{{Skill: "mining", Level: 20, Kind: GateServer}, {Skill: "agility", Level: 25, Kind: GateServer}}},
		"b": {ID: "b", Name: "B", QP: 1, Prereqs: []string{"a"}, XP: map[string]int{"herblore": 300}, MinCombat: 40},
		"c": {ID: "c", Name: "C", QP: 1, Gates: []Gate{{Skill: "herblore", Level: 3, Kind: GateServer}}, Prereqs: []string{"b"}},
	}
	tab := BuildNeedTable(NeedSources{Facts: facts})
	cat := NewCatalogue(func() TrainerSpec {
		r := row("agi", "agility", 1, 35)
		r.Rungs = map[int]string{25: "agility25"}
		return r
	}())
	return tab, cat
}

func TestFleetNeedsCountsAccountsPerNeedAndNeverNamesOne(t *testing.T) {
	tab, cat := reportWorld()
	low := accountAt(map[string]int{"agility": 1, "mining": 1})
	mid := accountAt(map[string]int{"agility": 30, "mining": 1})
	high := accountAt(map[string]int{"agility": 30, "mining": 25})
	done := accountAt(map[string]int{"agility": 30, "mining": 25})
	done.Done["a"] = true
	subs := []Subject{low, mid, high, done}
	rep := FleetNeeds(subs, []string{"a", "b", "c"}, tab, cat, DefaultNeedsSettings(), false)
	if rep.Accounts != 4 || len(rep.Quests) != 3 {
		t.Fatalf("report: %+v", rep)
	}
	a := rep.Quests[0]
	if a.Quest != "a" || a.Done != 1 || a.Ready != 1 || a.Blocked != 2 {
		t.Errorf("a: %+v", a)
	}
	counts := map[string]NeedCount{}
	for _, c := range a.Hard {
		counts[c.Need.Key()] = c
	}
	if c := counts["skill:mining:20"]; c.Accounts != 2 || c.Status != NeedUnreachable || c.Lane != "T1" {
		t.Errorf("mining: %+v (two accounts are below 20, this world's catalogue has no Mining row, and SkillGaps names lane T1)", c)
	}
	if c := counts["skill:agility:25"]; c.Accounts != 1 || c.Status != NeedStint || c.Acquirer != "agi" {
		t.Errorf("agility: %+v", c)
	}
	// b waits for a, c for b; c's Herblore 3 is paid by b's reward and so is not a need of an account that has not done b.
	b, c := rep.Quests[1], rep.Quests[2]
	if b.Done != 0 || b.Blocked != 3 || len(b.Advised) != 1 || b.Advised[0].Need.Kind != NeedCombat {
		t.Errorf("b: %+v", b)
	}
	for _, n := range c.Hard {
		if n.Need.Skill == "herblore" {
			t.Errorf("c's Herblore 3 is covered by b's reward: %+v", n)
		}
	}
	// The training bill: mining is short for two accounts and uncovered; agility for two with a path.
	bill := map[string]SkillBill{}
	for _, sb := range rep.Bill {
		bill[sb.Skill] = sb
	}
	if sb := bill["mining"]; sb.Accounts != 2 || sb.Covered || sb.MissingXP <= 0 {
		t.Errorf("mining bill: %+v", sb)
	}
	if sb := bill["agility"]; sb.Accounts != 1 || !sb.Covered || sb.Hours <= 0 || sb.Acquirer != "agi" {
		t.Errorf("agility bill: %+v", sb)
	}
	if len(rep.Unreachable) != 1 || rep.Unreachable[0].Need.Key() != "skill:mining:20" || rep.Unreachable[0].Accounts != 2 {
		t.Errorf("unreachable: %+v", rep.Unreachable)
	}
	// Restricted to the named quests, the others and their bill are left out.
	only := FleetNeeds(subs, []string{"b"}, tab, cat, DefaultNeedsSettings(), true)
	if len(only.Quests) != 1 || only.Quests[0].Quest != "b" {
		t.Errorf("only: %+v", only.Quests)
	}
	if len(only.Bill) != 0 {
		t.Errorf("b asks for no level, so its bill is empty (a's Mining and Agility are outside the list): %+v", only.Bill)
	}
}

// Every account is counted once per quest, whatever it has done.
func TestFleetNeedsCountsEveryAccountOncePerQuest(t *testing.T) {
	tab, cat := DefaultNeedTable(), DefaultCatalogue()
	var subs []Subject
	for _, lv := range []map[string]int{{}, {"agility": 40, "hitpoints": 60}, {"prayer": 50, "smithing": 40, "crafting": 40, "woodcutting": 40, "fletching": 30}} {
		s := accountAt(lv)
		subs = append(subs, s)
		d := accountAt(lv)
		for _, id := range tab.Quests()[:10] {
			d.Done[id] = true
		}
		subs = append(subs, d)
	}
	rep := FleetNeeds(subs, OwnerLadder, tab, cat, DefaultNeedsSettings(), false)
	if len(rep.Quests) != len(tab.Quests()) {
		t.Fatalf("%d quests reported, the table has %d", len(rep.Quests), len(tab.Quests()))
	}
	for _, q := range rep.Quests {
		if q.Done+q.Ready+q.Blocked != len(subs) {
			t.Errorf("%s: %d done + %d ready + %d blocked of %d accounts", q.Quest, q.Done, q.Ready, q.Blocked, len(subs))
		}
	}
	// The order comes first, in the owner's order.
	if rep.Quests[0].Quest != OwnerLadder[0] {
		t.Errorf("first quest %s, the owner's first is %s", rep.Quests[0].Quest, OwnerLadder[0])
	}
}

func TestAcquirerOfNamesTheWayOrTheGap(t *testing.T) {
	cat, set := DefaultCatalogue(), DefaultNeedsSettings()
	cases := []struct {
		need   Need
		status NeedStatus
		label  string
		lane   string
	}{
		{Need{Kind: NeedSkill, Skill: "agility", Level: 25}, NeedStint, "gnome-course", ""},
		{Need{Kind: NeedSkill, Skill: "agility", Level: 40}, NeedUnreachable, "no trainer for agility 35", "T8"},
		{Need{Kind: NeedSkill, Skill: "mining", Level: 20}, NeedUnreachable, "no trainer for mining 1", "T1"},
		{Need{Kind: NeedSkill, Skill: "magic", Level: 43}, NeedLoop, "stage5-magic", ""},
		{Need{Kind: NeedSkill, Skill: "prayer", Level: 43}, NeedSettings, "prayer-bones", ""},
		{Need{Kind: NeedCombat, Level: 40}, NeedLoop, "stage5-combat", ""},
		{Need{Kind: NeedQuest, Prereq: "pip"}, NeedWaitQuest, "quest pip", ""},
		{Need{Kind: NeedQuestPoints, Level: 32}, NeedWaitQuest, "quest points", ""},
		{Need{Kind: NeedItem, ItemID: ObjAntiDragon, Qty: 1}, NeedUnreachable, "no acquirer", "X4"},
		{Need{Kind: NeedItem, ItemID: ObjShark, Qty: 10}, NeedProvision, "the mule", "X3"},
	}
	for _, c := range cases {
		label, status, lane := AcquirerOf(cat, set, c.need)
		if status != c.status || !strings.Contains(label, c.label) || lane != c.lane {
			t.Errorf("%s: got %q %s lane %q, want %q %s lane %q", c.need.Key(), label, status, lane, c.label, c.status, c.lane)
		}
	}
	set.MaxRisk = R0
	if _, status, _ := AcquirerOf(cat, set, Need{Kind: NeedSkill, Skill: "prayer", Level: 43}); status != NeedSettings {
		t.Errorf("the ceiling: %s", status)
	}
	set = DefaultNeedsSettings()
	set.Allow = map[string]bool{"prayer-bones": true}
	if _, status, _ := AcquirerOf(cat, set, Need{Kind: NeedSkill, Skill: "prayer", Level: 43}); status != NeedStint {
		t.Errorf("an allowed R1 rung is a stint: %s", status)
	}
}
