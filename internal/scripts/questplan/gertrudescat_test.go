package questplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// fluffsSnap is an account on its way through Gertrude's Cat: the quest tab, the journal stage (-1 for unread), the pack by object
// id, a bank that holds nothing, and a position in Varrock.
func fluffsSnap(journal Status, stage int, held ...int) Snapshot {
	s := Snapshot{Journal: journal, InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: 20,
		BankKnown: true, BankIDs: map[int]int{}, Tile: tile.Tile{X: 3185, Z: 3430}, TileKnown: true,
		Levels: map[string]int{}, Failed: map[string]bool{}, Ran: map[string]bool{}}
	if stage >= 0 {
		s.Stage, s.StageKnown = stage, true
	}
	for _, id := range held {
		s.InvIDs[id]++
	}
	return s
}

func fluffsRan(s Snapshot, kits ...string) Snapshot {
	for _, k := range kits {
		s.Ran["provision:"+k] = true
	}
	return s
}

func TestGertrudesCatEveryState(t *testing.T) {
	const (
		leaves = FluffsDoogleLeaves
		raw    = FluffsRawSardine
		seas   = FluffsSeasonedSardine
		milk   = ObjBucketOfMilk
		bucket = ObjBucket
		kit    = FluffsKitten
	)
	cases := []struct {
		name string
		snap Snapshot
		kind StepKind
		want string // npc, custom name or kit name, "" for any
		item int    // the item id an offer carries, 0 for any
	}{
		{"complete", fluffsSnap(StatusComplete, -1), StepDone, "", 0},
		{"the tab is not coloured yet", fluffsSnap(StatusUnknown, -1), StepWait, "", 0},
		{"not started: Gertrude", fluffsSnap(StatusNotStarted, -1), StepTalk, "Gertrude", 0},
		{"started, journal unread: wait", fluffsSnap(StatusInProgress, -1), StepWait, "", 0},
		{"stage 0 in progress: Gertrude again", fluffsSnap(StatusInProgress, FluffsNotStarted), StepTalk, "Gertrude", 0},

		{"stage 1 with no coins: the coins", fluffsSnap(StatusInProgress, FluffsStarted), StepProvision, KitFluffsCoins, 0},
		{"stage 1, the coins kit failed: a park", func() Snapshot {
			s := fluffsSnap(StatusInProgress, FluffsStarted)
			s.Failed["provision:"+KitFluffsCoins] = true
			return s
		}(), StepWait, "", 0},
		{"stage 1 with 99 coins: still the coins", func() Snapshot {
			s := fluffsSnap(StatusInProgress, FluffsStarted)
			s.InvIDs[CoinsID] = 99
			return s
		}(), StepProvision, KitFluffsCoins, 0},
		{"stage 1 with 100 coins: pay the brothers", func() Snapshot {
			s := fluffsSnap(StatusInProgress, FluffsStarted)
			s.InvIDs[CoinsID] = 100
			return s
		}(), StepCustom, CustomFluffsPay, 0},

		{"stage 2, empty pack: the bank and the mule first", fluffsSnap(StatusInProgress, FluffsPaidBoy), StepProvision, KitFluffsItems, 0},
		{"stage 2, the pass ran: the doogle leaves", fluffsRan(fluffsSnap(StatusInProgress, FluffsPaidBoy), KitFluffsItems), StepGrabGround, "", leaves},
		{"stage 2, leaves held: Gerrant's sardine", fluffsRan(fluffsSnap(StatusInProgress, FluffsPaidBoy, leaves), KitFluffsItems), StepProvision, KitFluffsSard, 0},
		{"stage 2, a sardine and no leaves: the leaves", fluffsRan(fluffsSnap(StatusInProgress, FluffsPaidBoy, raw), KitFluffsItems), StepGrabGround, "", leaves},
		{"stage 2, leaves and a sardine: season", fluffsRan(fluffsSnap(StatusInProgress, FluffsPaidBoy, leaves, raw), KitFluffsItems), StepCustom, CustomFluffsSeason, 0},
		{"stage 2, seasoned, no milk, no bucket: the bucket", fluffsRan(fluffsSnap(StatusInProgress, FluffsPaidBoy, seas), KitFluffsItems), StepProvision, KitFluffsBucket, 0},
		{"stage 2, seasoned and a bucket: milk the cow", fluffsRan(fluffsSnap(StatusInProgress, FluffsPaidBoy, seas, bucket), KitFluffsItems), StepCustom, CustomFluffsMilk, 0},
		{"stage 2, everything: give the milk", fluffsRan(fluffsSnap(StatusInProgress, FluffsPaidBoy, seas, milk), KitFluffsItems), StepCustom, CustomFluffsOffer, milk},
		{"stage 2, everything from the bank: no pass at all", fluffsSnap(StatusInProgress, FluffsPaidBoy, seas, milk), StepCustom, CustomFluffsOffer, milk},
		{"stage 2, milk but no sardine: the sardine first", fluffsRan(fluffsSnap(StatusInProgress, FluffsPaidBoy, milk), KitFluffsItems), StepGrabGround, "", leaves},
		{"stage 2, the items pass failed on the way: not asked again", func() Snapshot {
			s := fluffsSnap(StatusInProgress, FluffsPaidBoy)
			s.Failed["provision:"+KitFluffsItems] = true
			return s
		}(), StepGrabGround, "", leaves},
		{"stage 2, the sardine kit failed: wait", func() Snapshot {
			s := fluffsRan(fluffsSnap(StatusInProgress, FluffsPaidBoy, leaves), KitFluffsItems)
			s.Failed["provision:"+KitFluffsSard] = true
			return s
		}(), StepWait, "", 0},
		{"stage 2, the bucket kit failed: wait", func() Snapshot {
			s := fluffsRan(fluffsSnap(StatusInProgress, FluffsPaidBoy, seas), KitFluffsItems)
			s.Failed["provision:"+KitFluffsBucket] = true
			return s
		}(), StepWait, "", 0},

		{"stage 3, seasoned: give the sardine", fluffsRan(fluffsSnap(StatusInProgress, FluffsGaveMilk, seas), KitFluffsItems), StepCustom, CustomFluffsOffer, seas},
		{"stage 3 with milk in the pack: still the sardine, milk is done", fluffsRan(fluffsSnap(StatusInProgress, FluffsGaveMilk, seas, milk), KitFluffsItems), StepCustom, CustomFluffsOffer, seas},
		{"stage 3, nothing: the pass, then never the milk", fluffsSnap(StatusInProgress, FluffsGaveMilk), StepProvision, KitFluffsItems, 0},
		{"stage 3, nothing, pass ran: the leaves", fluffsRan(fluffsSnap(StatusInProgress, FluffsGaveMilk), KitFluffsItems), StepGrabGround, "", leaves},
		{"stage 3, leaves and sardine: season", fluffsRan(fluffsSnap(StatusInProgress, FluffsGaveMilk, leaves, raw), KitFluffsItems), StepCustom, CustomFluffsSeason, 0},

		{"stage 4, no kitten: the bank is looked in first", fluffsSnap(StatusInProgress, FluffsGaveSardine), StepProvision, KitFluffsKitten, 0},
		{"stage 4, the bank looked: search the crates", fluffsRan(fluffsSnap(StatusInProgress, FluffsGaveSardine), KitFluffsKitten), StepCustom, CustomFluffsCrates, 0},
		{"stage 4, the kitten pass failed: search the crates all the same", func() Snapshot {
			s := fluffsSnap(StatusInProgress, FluffsGaveSardine)
			s.Failed["provision:"+KitFluffsKitten] = true
			return s
		}(), StepCustom, CustomFluffsCrates, 0},
		{"stage 4 with the kitten: give it to Fluffs", fluffsSnap(StatusInProgress, FluffsGaveSardine, kit), StepCustom, CustomFluffsOffer, kit},

		{"stage 5: tell Gertrude", fluffsSnap(StatusInProgress, FluffsRescued), StepCustom, CustomFluffsReport, 0},
		{"stage 6 in progress is unrecognised, never acted on", fluffsSnap(StatusInProgress, FluffsComplete), StepWait, "", 0},

		{"a full pack banks before the leaves", func() Snapshot {
			s := fluffsRan(fluffsSnap(StatusInProgress, FluffsPaidBoy), KitFluffsItems)
			s.FreeSlots = 2
			return s
		}(), StepDeposit, "", 0},
		{"a full pack banks before the crates", func() Snapshot {
			s := fluffsRan(fluffsSnap(StatusInProgress, FluffsGaveSardine), KitFluffsKitten)
			s.FreeSlots = 1
			return s
		}(), StepDeposit, "", 0},
		{"a full pack still gives the milk", func() Snapshot {
			s := fluffsRan(fluffsSnap(StatusInProgress, FluffsPaidBoy, seas, milk), KitFluffsItems)
			s.FreeSlots = 0
			return s
		}(), StepCustom, CustomFluffsOffer, milk},
	}
	for _, c := range cases {
		step := DecideGertrudesCat(c.snap)
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
		if c.item != 0 && step.ItemID != c.item {
			t.Errorf("%s: the step carries item %d, want %d (%s)", c.name, step.ItemID, c.item, step.Describe())
		}
	}
}

// Fluffs takes her gifts in the content's order: the milk needs stage 2, the sardine stage 3 and the kitten stage 4, and an
// offer at the wrong stage is only "doesn't seem to be hungry". The module therefore never offers the milk after the sardine's
// stage and never offers the sardine before the milk's.
func TestGertrudesCatOffersEachGiftAtItsOwnStage(t *testing.T) {
	everything := []int{FluffsSeasonedSardine, ObjBucketOfMilk, FluffsKitten}
	want := map[int]int{FluffsPaidBoy: ObjBucketOfMilk, FluffsGaveMilk: FluffsSeasonedSardine, FluffsGaveSardine: FluffsKitten}
	for stage, item := range want {
		s := fluffsRan(fluffsSnap(StatusInProgress, stage, everything...), KitFluffsItems, KitFluffsKitten)
		step := DecideGertrudesCat(s)
		if step.Kind != StepCustom || step.Custom != CustomFluffsOffer || step.ItemID != item {
			t.Errorf("stage %d holding all three: %s (item %d), want the offer of %d", stage, step.Describe(), step.ItemID, item)
		}
	}
}

// The coins kit is the fee, the sardine and the bucket with change, in one stack.
func TestGertrudesCatCoinsKit(t *testing.T) {
	step := DecideGertrudesCat(fluffsSnap(StatusInProgress, FluffsStarted))
	if step.Kind != StepProvision || len(step.Kit) != 1 {
		t.Fatalf("%s", step.Describe())
	}
	l := step.Kit[0]
	if l.ID != CoinsID || !l.Stackable || l.Min < FluffsBoyFee+10 || l.Qty < l.Min || l.Optional {
		t.Errorf("coins line %+v", l)
	}
	// The bank holds 400: 300 are drawn. The bank holds 150: that is enough to go on with (Min), and it is taken.
	p := PlanProvision(step.Kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{CoinsID: 400}, BankKnown: true, FreeSlots: 20})
	if len(p.Withdraw) != 1 || p.Withdraw[0].Count != l.Qty {
		t.Errorf("a rich bank: %s", p.Summary())
	}
	p = PlanProvision(step.Kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{CoinsID: l.Min + 20}, BankKnown: true, FreeSlots: 20})
	if p.Phase != PhaseBank || len(p.Withdraw) != 1 || p.Withdraw[0].Count != l.Min+20 {
		t.Errorf("a bank with just enough: %s", p.Summary())
	}
	// Nothing in the bank, 51 million on the mule: the one ask covers it.
	p = PlanProvision(step.Kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20,
		Mule: map[int]int{CoinsID: 51457534}, MuleKnown: true})
	if p.Phase != PhaseMule || len(p.MuleAsk) != 1 || p.MuleAsk[0].ID != CoinsID {
		t.Errorf("a mule with coins: %s", p.Summary())
	}
}

// The items pass is bank and mule only, line by line optional, and asks for the seasoned sardine first with its two parts wanted
// only while it is short; the milk line exists at stage 2 alone.
func TestGertrudesCatItemsPass(t *testing.T) {
	kit := fluffsItemsKit(FluffsPaidBoy)
	if len(kit) != 4 {
		t.Fatalf("stage 2 kit %+v", kit)
	}
	for _, l := range kit {
		if !l.Optional || len(l.Sources) != 0 || l.Qty != 1 {
			t.Errorf("line %+v", l)
		}
	}
	if kit[0].ID != FluffsSeasonedSardine || kit[1].OnlyIfShort != FluffsSeasonedSardine || kit[2].OnlyIfShort != FluffsSeasonedSardine {
		t.Errorf("the parts are not wanted only while the seasoned sardine is short: %+v", kit)
	}
	if len(fluffsItemsKit(FluffsGaveMilk)) != 3 {
		t.Error("the milk is wanted after the cat has had it")
	}
	// The bank holds the seasoned sardine: its parts are not asked for once it is in the pack.
	p := PlanProvision(kit, ProvisionState{Pack: map[int]int{FluffsSeasonedSardine: 1}, Bank: map[int]int{FluffsDoogleLeaves: 3, FluffsRawSardine: 3},
		BankKnown: true, FreeSlots: 20})
	if len(p.Withdraw) != 0 {
		t.Errorf("a seasoned sardine in the pack still draws its parts: %s", p.Summary())
	}
	// The mule's three buckets of milk are asked for, and nothing else of the lot (it publishes none of the rest).
	p = PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20,
		Mule: map[int]int{ObjBucketOfMilk: 3, FluffsRawSardine: 0}, MuleKnown: true})
	if p.Phase != PhaseMule || len(p.MuleAsk) != 1 || p.MuleAsk[0].ID != ObjBucketOfMilk {
		t.Errorf("plan %s", p.Summary())
	}
}

// The sardine comes from Gerrant's shelf and the bucket from the farmhouse floor or the Lumbridge shop, after the bank and the
// mule; the leaves are grabbed from the ground behind Gertrude's house, five spawns inside the grab's reach.
func TestGertrudesCatSources(t *testing.T) {
	step := DecideGertrudesCat(fluffsRan(fluffsSnap(StatusInProgress, FluffsPaidBoy, FluffsDoogleLeaves), KitFluffsItems))
	if step.Kind != StepProvision || step.KitName != KitFluffsSard || len(step.Kit) != 1 || len(step.Kit[0].Sources) != 1 {
		t.Fatalf("%s", step.Describe())
	}
	src := step.Kit[0].Sources[0]
	if src.Kind != SourceShop || src.Shop.NpcID != 558 || src.Shop.Npc != "Gerrant" || src.Price != 10 {
		t.Errorf("sardine source %+v", src)
	}
	step = DecideGertrudesCat(fluffsRan(fluffsSnap(StatusInProgress, FluffsPaidBoy, FluffsSeasonedSardine), KitFluffsItems))
	if step.Kind != StepProvision || step.KitName != KitFluffsBucket || len(step.Kit) != 1 || step.Kit[0].ID != ObjBucket {
		t.Fatalf("%s", step.Describe())
	}
	if len(step.Kit[0].Sources) != 2 || step.Kit[0].Sources[0].Kind != SourceGround || step.Kit[0].Sources[1].Kind != SourceShop {
		t.Errorf("bucket sources %+v: the floor before the shop", step.Kit[0].Sources)
	}
	// Every kit keeps the cat's gifts in the pack through its bank visit.
	keep := map[int]bool{}
	for _, id := range step.KeepIDs {
		keep[id] = true
	}
	for _, id := range []int{CoinsID, FluffsDoogleLeaves, FluffsRawSardine, FluffsSeasonedSardine, ObjBucketOfMilk, ObjBucket, FluffsKitten} {
		if !keep[id] {
			t.Errorf("a bank visit would deposit obj %d", id)
		}
	}
	// The leaves: a grab at the middle of the five spawns, every one within the grab's six tiles, and a wait for a respawn.
	step = DecideGertrudesCat(fluffsRan(fluffsSnap(StatusInProgress, FluffsPaidBoy), KitFluffsItems))
	if step.Kind != StepGrabGround || step.Anchor != DoogleField || step.Radius < 30 {
		t.Fatalf("%s anchor %v radius %d", step.Describe(), step.Anchor, step.Radius)
	}
	if len(DoogleSpawns) != 5 {
		t.Errorf("%d doogle spawns, the map has 5", len(DoogleSpawns))
	}
	for _, sp := range DoogleSpawns {
		if tile.Chebyshev(sp, DoogleField) > 6 {
			t.Errorf("the spawn %v is out of the grab's 6 tiles of %v", sp, DoogleField)
		}
	}
}

func TestBrothersPaired(t *testing.T) {
	a := tile.Tile{X: 3221, Z: 3434}
	for _, c := range []struct {
		b    tile.Tile
		want bool
	}{
		{tile.Tile{X: 3221, Z: 3435}, true},  // their spawns
		{tile.Tile{X: 3222, Z: 3435}, true},  // diagonal
		{tile.Tile{X: 3221, Z: 3434}, true},  // the same tile
		{tile.Tile{X: 3223, Z: 3434}, false}, // two apart: the script's own limit is 3 four lines in, and they wander
		{tile.Tile{X: 3221, Z: 3437}, false},
		{tile.Tile{X: 3221, Z: 3435, Level: 1}, false},
	} {
		if got := BrothersPaired(a, c.b); got != c.want {
			t.Errorf("brothers at %v and %v: paired %v, want %v", a, c.b, got, c.want)
		}
	}
	if FluffsBrotherGap < 1 || FluffsBrotherGap > 2 {
		t.Errorf("gap %d: the content's limit is 3 four chat lines in", FluffsBrotherGap)
	}
}

// The yard and the walkway, and that nothing the quest does lies in the Wilderness (z 3520 and north on the ground floor).
func TestFluffsTilesAreOutsideTheWilderness(t *testing.T) {
	all := []tile.Tile{GertrudeAnchor, FluffsMarket, DoogleField, FluffsSardineShop.Anchor, YardOutside, YardFence, YardInside, YardLadder, YardLadderTop,
		YardLadderStand, FluffsSpot, CatStand}
	all = append(append(all, DoogleSpawns...), FluffsCrateTiles...)
	for _, tl := range all {
		if tl.Z >= WildernessLine {
			t.Errorf("%v is in the Wilderness", tl)
		}
	}
	if len(FluffsCrateTiles) != 6 {
		t.Errorf("%d crates, the map has 6", len(FluffsCrateTiles))
	}
	for _, c := range FluffsCrateTiles {
		if !InYard(c) {
			t.Errorf("crate %v is not in the yard", c)
		}
	}
	if InYard(YardOutside) || !InYard(YardFence) || !InYard(YardInside) || InYard(tile.Tile{X: 3305, Z: 3500, Level: 1}) {
		t.Error("the yard's south edge is the fence")
	}
	if !OnWalkway(CatStand) || !OnWalkway(FluffsSpot) || !OnWalkway(YardLadderTop) || OnWalkway(YardInside) {
		t.Error("the walkway is the level above the yard")
	}
	if InYard(tile.Tile{X: 3305, Z: 3520}) {
		t.Error("the Wilderness line is not the yard")
	}
}

// ParseGertrudesCatJournal against the journal as the server renders it, every stage (fluffs_journal.rs2), and the newest
// sentence decides: each later stage's text holds the earlier stages' lines struck through.
func TestGertrudesCatJournal(t *testing.T) {
	for stage, text := range fluffsJournalFixtures {
		got, ok := ParseGertrudesCatJournal([]string{text})
		if !ok || got != stage {
			t.Errorf("stage %d: parsed %d (known %v) from %q", stage, got, ok, text)
		}
		got, ok = ParseGertrudesCatJournal(strings.Split(text, "|"))
		if !ok || got != stage {
			t.Errorf("stage %d split into lines: parsed %d (known %v)", stage, got, ok)
		}
	}
	if len(fluffsJournalFixtures) != 7 {
		t.Errorf("%d fixtures, the journal has seven branches", len(fluffsJournalFixtures))
	}
	for _, none := range [][]string{nil, {""}, {"@dbl@Something else entirely"}} {
		if got, ok := ParseGertrudesCatJournal(none); ok {
			t.Errorf("%q parsed as stage %d, want unknown", none, got)
		}
	}
}

// Generated from content 289 by joining every string of each branch of fluffs_journal.rs2 in order (the journal is
// `~quest_journal(title, $text)` over exactly that text, with `|` the client's line break).
var fluffsJournalFixtures = map[int]string{
	0: "@dbl@I can start this quest by talking to @dre@Gertrude@dbl@. She can be found in a house south of the road leading|@dre@west out of Varrock@dbl@.",
	1: "@str@I accepted the challenge of finding Gertrude's lost cat.||@dbl@I need to @dre@speak to Shilop and Wilough@dbl@ at the @dre@marketplace@dbl@.",
	2: "@str@I accepted the challenge of finding Gertrude's lost cat.|@str@I spoke to Shilop, Gertrude's Son.||@dbl@I need to @dre@go to their play area@dbl@ and @dre@find the lost cat and return it to Gertrude@dbl@.",
	3: "@str@I accepted the challenge of finding Gertrude's lost cat.|@str@I spoke to Shilop, Gertrude's Son.|@str@I found the lost cat but it won't come back.||@dbl@I still need to @dre@get her to follow me home@dbl@.",
	4: "@str@I accepted the challenge of finding Gertrude's lost cat.|@str@I spoke to Shilop, Gertrude's Son.|@str@I found the lost cat but it won't come back.|@str@I gave the cat milk and sardines.||@dbl@I still need to @dre@get her to follow me home@dbl@.",
	5: "@str@I accepted the challenge of finding Gertrude's lost cat.|@str@I spoke to Shilop, Gertrude's Son.|@str@The cat didn't follow me back.|@str@I gave the cat milk and sardines.|@str@I gave Fluffs her kitten back.||@dbl@She ran off home.",
	6: "@str@I helped Gertrude to find her lost cat,|@str@I fed it and returned her missing kitten,|@str@Gertrude gave me my very own pet for a reward.||@red@QUEST COMPLETE!",
}

func TestGertrudesCatConstants(t *testing.T) {
	if GertrudesCatRecord.QuestPoints != 1 || GertrudesCatRecord.ID != "fluffs" || GertrudesCatRecord.Name != "Gertrude's Cat" {
		t.Errorf("record %+v", GertrudesCatRecord)
	}
	m := GertrudesCat
	if m.MaxDeaths != 1 || m.ReadStage == nil || m.Decide == nil || !m.BankSet {
		t.Errorf("module %+v", m)
	}
	if FluffsBoyFee != 100 {
		t.Errorf("fee %d, fluffs_pay deletes 100 coins", FluffsBoyFee)
	}
	if len(FluffsKeepIDs()) != len(fluffsKeep) {
		t.Error("the keep list copy")
	}
	// The stages are the content's (quest_fluffs.constant and quest.constant).
	for i, v := range []int{FluffsNotStarted, FluffsStarted, FluffsPaidBoy, FluffsGaveMilk, FluffsGaveSardine, FluffsRescued, FluffsComplete} {
		if v != i {
			t.Errorf("stage %d is %d", i, v)
		}
	}
}

// Every step the module can return validates, over a sweep of stages, packs and kit memories.
func TestGertrudesCatEveryReachableStepValidates(t *testing.T) {
	ids := []int{FluffsDoogleLeaves, FluffsRawSardine, FluffsSeasonedSardine, ObjBucketOfMilk, ObjBucket, FluffsKitten, CoinsID}
	for _, journal := range []Status{StatusNotStarted, StatusInProgress, StatusUnknown, StatusComplete} {
		for stage := -1; stage <= FluffsComplete; stage++ {
			for mask := 0; mask < 1<<len(ids); mask++ {
				var held []int
				for i, id := range ids {
					if mask&(1<<i) != 0 {
						held = append(held, id)
						if id == CoinsID {
							held = append(held, doricRepeat(CoinsID, 150)...)
						}
					}
				}
				for _, ran := range []bool{false, true} {
					s := fluffsSnap(journal, stage, held...)
					if ran {
						s = fluffsRan(s, KitFluffsItems, KitFluffsKitten)
					}
					step := DecideGertrudesCat(s)
					if why := ValidateStep(step); why != "" {
						t.Fatalf("%s stage %d mask %b ran %v: %s (%s)", journal, stage, mask, ran, why, step.Describe())
					}
					// A bank visit of any step leaves the coins and every gift in the pack.
					if step.Kind == StepProvision || step.Kind == StepDeposit {
						keep := map[int]bool{}
						for _, id := range step.KeepIDs {
							keep[id] = true
						}
						for _, id := range fluffsKeep {
							if !keep[id] {
								t.Fatalf("%s stage %d mask %b ran %v: %s would deposit obj %d", journal, stage, mask, ran, step.Describe(), id)
							}
						}
						if !step.BankSet {
							t.Fatalf("%s stage %d: %s names no bank", journal, stage, step.Describe())
						}
					}
				}
			}
		}
	}
}
