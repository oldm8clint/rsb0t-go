package questplan

import (
	"slices"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// cookSnap is an account on its way through Cook's Assistant: the quest tab, the pack by object id, a bank read that holds
// nothing, and a position beside the Cook.
func cookSnap(journal Status, held ...int) Snapshot {
	s := Snapshot{Journal: journal, InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: 20,
		BankKnown: true, BankIDs: map[int]int{}, Tile: tile.Tile{X: 3210, Z: 3216}, TileKnown: true,
		Failed: map[string]bool{}, Ran: map[string]bool{}}
	for _, id := range held {
		s.InvIDs[id]++
	}
	return s
}

func TestCooksAssistantEveryStage(t *testing.T) {
	kitRan := func(s Snapshot) Snapshot { s.Ran["provision:"+KitCook] = true; return s }
	cases := []struct {
		name string
		snap Snapshot
		kind StepKind
		want string // npc, custom name, kit name or loc, "" for any
	}{
		{"complete", cookSnap(StatusComplete), StepDone, ""},
		{"the tab is not coloured yet", cookSnap(StatusUnknown), StepWait, ""},
		{"not started: the Cook", cookSnap(StatusNotStarted), StepTalk, "Cook"},
		{"not started with every ingredient: still the start first", cookSnap(StatusNotStarted, ObjEgg, ObjBucketOfMilk, ObjPotOfFlour), StepTalk, "Cook"},
		{"started with the three in the pack: the hand-in", cookSnap(StatusInProgress, ObjEgg, ObjBucketOfMilk, ObjPotOfFlour), StepTalk, "Cook"},
		{"started, nothing held: one bundled pass first", cookSnap(StatusInProgress), StepProvision, KitCook},
		{"the bundled pass failed on the way: not asked again", func() Snapshot {
			s := cookSnap(StatusInProgress)
			s.Failed["provision:"+KitCook] = true
			return s
		}(), StepGrabGround, "Egg"},
		{"the pass ran, no egg: the coop floor", kitRan(cookSnap(StatusInProgress)), StepGrabGround, "Egg"},
		{"egg held, no milk, no bucket: a bucket", kitRan(cookSnap(StatusInProgress, ObjEgg)), StepProvision, KitCookBucket},
		{"egg and bucket held: milk the cow", kitRan(cookSnap(StatusInProgress, ObjEgg, ObjBucket)), StepCustom, CustomCookMilk},
		{"egg and milk held, no pot: a pot", kitRan(cookSnap(StatusInProgress, ObjEgg, ObjBucketOfMilk)), StepProvision, KitCookPot},
		{"egg, milk and pot held, no grain: the wheat", kitRan(cookSnap(StatusInProgress, ObjEgg, ObjBucketOfMilk, ObjEmptyPot)), StepPickLoc, "Wheat"},
		{"egg, milk, pot and grain held: the mill", kitRan(cookSnap(StatusInProgress, ObjEgg, ObjBucketOfMilk, ObjEmptyPot, ObjGrain)), StepCustom, CustomCookMill},
		{"flour held, no egg: the egg first (the order is egg, milk, flour)", kitRan(cookSnap(StatusInProgress, ObjPotOfFlour, ObjBucketOfMilk)), StepGrabGround, "Egg"},
		{"a full pack banks first", func() Snapshot {
			s := kitRan(cookSnap(StatusInProgress))
			s.FreeSlots = 1
			return s
		}(), StepDeposit, ""},
	}
	for _, c := range cases {
		step := DecideCooksAssistant(c.snap)
		if why := ValidateStep(step); why != "" {
			t.Errorf("%s: malformed step: %s", c.name, why)
		}
		if step.Kind != c.kind {
			t.Errorf("%s: got %s (%s), want %s", c.name, step.Kind, step.Describe(), c.kind)
			continue
		}
		var got string
		switch step.Kind {
		case StepTalk:
			got = step.Stop.Npc
		case StepCustom:
			got = step.Custom
		case StepProvision:
			got = step.KitName
		case StepGrabGround, StepPickLoc:
			got = step.Item
			if step.Kind == StepPickLoc {
				got = step.Loc
			}
		}
		if c.want != "" && got != c.want {
			t.Errorf("%s: got %q, want %q (%s)", c.name, got, c.want, step.Describe())
		}
	}
}

// A kit that failed leaves the quest waiting rather than walking to a cow with no bucket or a mill with no pot.
func TestCooksAssistantWaitsWhenTheToolKitFailed(t *testing.T) {
	for _, c := range []struct {
		kit  string
		held []int
	}{
		{KitCookBucket, []int{ObjEgg}},
		{KitCookPot, []int{ObjEgg, ObjBucketOfMilk}},
	} {
		s := cookSnap(StatusInProgress, c.held...)
		s.Ran["provision:"+KitCook] = true
		s.Failed["provision:"+c.kit] = true
		if step := DecideCooksAssistant(s); step.Kind != StepWait || ValidateStep(step) != "" {
			t.Errorf("%s failed: got %s (%s), want a wait", c.kit, step.Kind, step.Describe())
		}
	}
}

// The bundled pass: the item rule's bank and mule rungs are implied for every line, so the explicit sources are the
// egg's two floor spawns and, for an account that stands nearer to Port Sarim than to the Cook, the flour's shop; nothing is
// gathered by skill (that would run before the shop).
func TestCooksAssistantKitFollowsTheItemRule(t *testing.T) {
	atPortSarim := cookSnap(StatusInProgress)
	atPortSarim.Tile = tile.Tile{X: 3020, Z: 3215}
	kit := cookKit(atPortSarim)
	byID := map[int]KitLine{}
	for _, l := range kit {
		byID[l.ID] = l
		if why := ValidateKitLine(l); why != "" {
			t.Errorf("kit line %s: %s", l.Name, why)
		}
		if !l.Optional || l.Qty != 1 {
			t.Errorf("kit line %s: optional %v qty %d, want optional and 1", l.Name, l.Optional, l.Qty)
		}
		for _, src := range l.Sources {
			if src.Kind == SourceSkill {
				t.Errorf("%s has a skill source: the item rule would gather before it shops", l.Name)
			}
		}
	}
	if len(byID) != 3 {
		t.Fatalf("kit lines %v, want the egg, the milk and the flour", kit)
	}
	if egg := byID[ObjEgg]; len(egg.Sources) != 2 || egg.Sources[0].Kind != SourceGround || egg.Sources[0].Tile != CookEggSpawns[0] || egg.Sources[1].Tile != CookEggSpawns[1] {
		t.Errorf("egg sources %+v, want the two coop spawns", egg.Sources)
	}
	if milk := byID[ObjBucketOfMilk]; len(milk.Sources) != 0 {
		t.Errorf("milk sources %+v: no shop in reach stocks it, only the bank and the mule", milk.Sources)
	}
	if flour := byID[ObjPotOfFlour]; len(flour.Sources) != 1 || flour.Sources[0].Kind != SourceShop || flour.Sources[0].Shop != WydinShop {
		t.Errorf("flour sources at Port Sarim %+v, want Wydin's shelf", flour.Sources)
	}
	// An account standing at the Cook has the windmill 150 steps away and Wydin 314: the shelf is not listed for it.
	atCook := cookKit(cookSnap(StatusInProgress))
	for _, l := range atCook {
		if l.ID == ObjPotOfFlour && len(l.Sources) != 0 {
			t.Errorf("flour sources at the Cook %+v, want none: the mill is nearer than the shop", l.Sources)
		}
	}
	// ... and neither is it before a position packet has arrived.
	blind := cookSnap(StatusInProgress)
	blind.TileKnown = false
	for _, l := range cookKit(blind) {
		if l.ID == ObjPotOfFlour && len(l.Sources) != 0 {
			t.Errorf("flour sources with no position %+v, want none", l.Sources)
		}
	}

	// Bank, then the mule, then the ground, then the shop: PlanProvision on a bank that holds the milk and a mule that holds
	// the flour asks for nothing from the world but the egg.
	p := PlanProvision(kit, ProvisionState{
		Pack: map[int]int{}, Worn: map[int]int{}, FreeSlots: 20,
		Bank: map[int]int{ObjBucketOfMilk: 1}, BankKnown: true,
		Mule: map[int]int{ObjPotOfFlour: 3}, MuleKnown: true,
	})
	if p.Phase != PhaseBank || len(p.Withdraw) != 1 || p.Withdraw[0].ID != ObjBucketOfMilk {
		t.Fatalf("plan %s, want the milk from the bank first", p.Summary())
	}
	p = PlanProvision(kit, ProvisionState{
		Pack: map[int]int{ObjBucketOfMilk: 1}, Worn: map[int]int{}, FreeSlots: 20,
		Bank: map[int]int{}, BankKnown: true,
		Mule: map[int]int{ObjPotOfFlour: 3}, MuleKnown: true,
	})
	if p.Phase != PhaseMule || len(p.MuleAsk) != 1 || p.MuleAsk[0].ID != ObjPotOfFlour {
		t.Fatalf("plan %s, want the flour from the mule next", p.Summary())
	}
	p = PlanProvision(kit, ProvisionState{
		Pack: map[int]int{ObjBucketOfMilk: 1, ObjPotOfFlour: 1}, Worn: map[int]int{}, FreeSlots: 20,
		Bank: map[int]int{}, BankKnown: true,
	})
	if p.Phase != PhaseSource || len(p.Tasks) != 1 || p.Tasks[0].Source.Kind != SourceGround || p.Tasks[0].Source.Tile != CookEggSpawns[0] {
		t.Fatalf("plan %s, want the egg off the coop floor", p.Summary())
	}
	// With nothing anywhere the coop and the shelf are both worked.
	p = PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Worn: map[int]int{}, FreeSlots: 20, Bank: map[int]int{}, BankKnown: true, Exhausted: map[string]bool{}})
	var kinds []SourceKind
	for _, task := range p.Tasks {
		kinds = append(kinds, task.Source.Kind)
	}
	if !slices.Contains(kinds, SourceGround) || !slices.Contains(kinds, SourceShop) {
		t.Fatalf("plan %s, want the coop floor and Wydin's shelf", p.Summary())
	}
	// With the egg and the flour settled and only the milk missing, a kit with no source for it is satisfied (every line is
	// optional): the quest's own cow comes next, not a BLOCKED round.
	p = PlanProvision(atCook, ProvisionState{Pack: map[int]int{ObjEgg: 1, ObjPotOfFlour: 1}, Worn: map[int]int{}, FreeSlots: 20, Bank: map[int]int{}, BankKnown: true})
	if p.Phase != PhaseSatisfied {
		t.Fatalf("plan %s, want satisfied with the milk still to milk", p.Summary())
	}
}

// The deposit-first of every provisioning round and deposit step leaves the ingredients, their makers and the coins in
// the pack: a bank visit that banked the egg would send the quest back to the coop for it.
func TestCooksAssistantKeepListsHoldEveryIngredient(t *testing.T) {
	want := []int{ObjEgg, ObjBucketOfMilk, ObjPotOfFlour, ObjBucket, ObjEmptyPot, ObjGrain, CoinsID}
	for _, id := range want {
		if !slices.Contains(CooksAssistant.ToolIDs, id) {
			t.Errorf("Module.ToolIDs lacks %d: the engine's own deposits would bank it", id)
		}
	}
	steps := map[string]Step{
		"bundled": DecideCooksAssistant(cookSnap(StatusInProgress)),
		"bucket":  DecideCooksAssistant(func() Snapshot { s := cookSnap(StatusInProgress, ObjEgg); s.Ran["provision:"+KitCook] = true; return s }()),
		"pot": DecideCooksAssistant(func() Snapshot {
			s := cookSnap(StatusInProgress, ObjEgg, ObjBucketOfMilk)
			s.Ran["provision:"+KitCook] = true
			return s
		}()),
		"deposit": DecideCooksAssistant(func() Snapshot {
			s := cookSnap(StatusInProgress)
			s.Ran["provision:"+KitCook] = true
			s.FreeSlots = 0
			return s
		}()),
	}
	for name, st := range steps {
		if st.Kind != StepProvision && st.Kind != StepDeposit {
			t.Errorf("%s: got %s", name, st.Describe())
			continue
		}
		for _, id := range want {
			if !slices.Contains(st.KeepIDs, id) {
				t.Errorf("%s step's keep list lacks %d (%v)", name, id, st.KeepIDs)
			}
		}
		if !st.BankSet || st.Bank != DraynorBank {
			t.Errorf("%s step banks at %v (set %v), want Draynor's: Lumbridge has no bank", name, st.Bank, st.BankSet)
		}
	}
	if k := CookKeepIDs(); !slices.Equal(k, want) {
		t.Errorf("CookKeepIDs %v", k)
	}
}

// The tools' own kits: a free bucket on the farmhouse floor (twenty tiles from the cows) before the store, a free pot on
// the kitchen floor before the store.
func TestCooksAssistantToolKits(t *testing.T) {
	for _, c := range []struct {
		name  string
		kit   []KitLine
		id    int
		floor tile.Tile
	}{
		{"bucket", cookBucketKit(), ObjBucket, FarmBucketSpawn},
		{"pot", cookPotKit(), ObjEmptyPot, KitchenPotSpawn},
	} {
		if len(c.kit) != 1 || c.kit[0].ID != c.id || c.kit[0].Qty != 1 || c.kit[0].Optional {
			t.Fatalf("%s kit %+v", c.name, c.kit)
		}
		if why := ValidateKitLine(c.kit[0]); why != "" {
			t.Errorf("%s: %s", c.name, why)
		}
		src := c.kit[0].Sources
		if len(src) != 2 || src[0].Kind != SourceGround || src[0].Tile != c.floor || src[1].Kind != SourceShop || src[1].Shop != LumbridgeStore {
			t.Errorf("%s sources %+v, want the floor then the Lumbridge store", c.name, src)
		}
		// Ground before the shop in the owner's rule, so the free copy is tried first.
		p := PlanProvision(c.kit, ProvisionState{Pack: map[int]int{}, Worn: map[int]int{}, FreeSlots: 20, Bank: map[int]int{}, BankKnown: true})
		if p.Phase != PhaseSource || len(p.Tasks) != 1 || p.Tasks[0].Source.Kind != SourceGround {
			t.Errorf("%s plan %s, want the floor spawn first", c.name, p.Summary())
		}
	}
}

// Content's menus, as quest_cook.rs2 writes them, against the stop's preference list.
func TestCookStopPicksTheRightOptionsAtEveryMenu(t *testing.T) {
	menus := []struct {
		options []string
		want    string
	}{
		{[]string{"What's wrong?", "Well you could give me all your money!", "You don't look very happy.", "Nice hat!"}, "What's wrong?"},
		{[]string{"Yes, I'll help you.", "No, I don't feel like it. Maybe later."}, "Yes, I'll help you."},
		// The branch after "You don't look very happy.": the menu is asked again with the rude options gone.
		{[]string{"What's wrong?", "I'd take the rest of the day off if I were you."}, "What's wrong?"},
	}
	for _, m := range menus {
		if got, ok := PickPreferred(m.options, CookStop.Prefer); !ok || got != m.want {
			t.Errorf("menu %v: picked %q (%v), want %q", m.options, got, ok, m.want)
		}
	}
}

func TestCooksAssistantModuleIdentity(t *testing.T) {
	m := CooksAssistant
	if m.Record.ID != "cook" || m.Record.Name != "Cook's Assistant" || m.Record.QuestPoints != 1 {
		t.Errorf("record %+v", m.Record)
	}
	if QuestTabName("cook") != m.Record.Name {
		t.Errorf("QuestTabName %q", QuestTabName("cook"))
	}
	if m.ReadStage == nil {
		t.Error("Cook's Assistant reads its stage for the log and the lab's journal check")
	}
	if m.MaxDeaths != 1 || m.Sustain.EatAt != 0 || m.NoRetaliate {
		t.Errorf("a quest with no combat: MaxDeaths %d, sustain %+v, no-retaliate %v", m.MaxDeaths, m.Sustain, m.NoRetaliate)
	}
	if !m.BankSet || m.Bank != DraynorBank {
		t.Errorf("bank %v set %v", m.Bank, m.BankSet)
	}
}

// cook_journal.rs2's strings, as the client renders them.
func TestParseCooksAssistantJournalAgainstTheContentText(t *testing.T) {
	notStarted := []string{"@dbl@I can start this quest by speaking to the @dre@Cook@dbl@ in the|", "@dre@Kitchen@dbl@ on the ground floor of @dre@Lumbridge Castle@dbl@."}
	started := []string{"@dbl@It's the @dre@Duke of Lumbridges'@dbl@ birthday and I have to help @dbl@his @dre@Cook@dbl@ make him a birthday cake. To do this I need to @dbl@bring him the following ingredients:|",
		"@str@I have a bucket of milk with me|", "@dbl@an @dre@egg|", "@dbl@a @dre@pot@dbl@ of @dre@flour|"}
	complete := []string{"@str@It was the Duke of Lumbridge's birthday, but his cook had @str@forgotten to buy the ingredients he needed to make him a @str@cake.||", "@red@QUEST COMPLETE!"}
	for _, c := range []struct {
		name  string
		lines []string
		stage int
		ok    bool
	}{
		{"not started", notStarted, 0, true},
		{"started", started, CookStarted, true},
		{"complete", complete, CookComplete, true},
		{"nothing rendered", nil, 0, false},
		{"an unrelated page", []string{"@dbl@Something else entirely."}, 0, false},
	} {
		if stage, ok := ParseCooksAssistantJournal(c.lines); stage != c.stage || ok != c.ok {
			t.Errorf("%s: stage %d ok %v, want %d %v", c.name, stage, ok, c.stage, c.ok)
		}
	}
}
