package loot

import (
	"reflect"
	"testing"
)

const (
	testShrimps = 317
	testCasket  = 405
)

func TestRandomEventRewardTable(t *testing.T) {
	// The Genie was on this list until 2026-09-21, when the owner reversed their own earlier
	// "record nothing" call and asked for it to show how many lamps it has given. It is a
	// source now; what it must NOT do is record items, which TestGenieCountsLampsAndRecordsNoItems
	// pins. Everything below still has nothing to show: a whirlpool only deletes a tool, a Big
	// fish drops the account's own tool on the floor, and a monster is never killed or looted.
	for _, name := range []string{"Lamp", "whirlpool", "Big fish", "event monster", "Swarm", ""} {
		if _, ok := RandomEventReward(name); ok {
			t.Errorf("%q must not be a recorded reward event", name)
		}
	}
	for guardianName, want := range map[string]string{
		"Strange box": "Strange box", "Mysterious Old Man": "Mysterious Old Man", "Drunken Dwarf": "Drunken Dwarf",
		"Strange plant": "Strange plant", "Strange Plant": "Strange plant", "mime": "Mime", "maze": "Maze",
	} {
		r, ok := RandomEventReward(guardianName)
		if !ok || r.Name != want {
			t.Errorf("RandomEventReward(%q) = %q, %v; want %q", guardianName, r.Name, ok, want)
		}
	}
	for _, r := range RandomEventRewards() {
		// The Genie is the one source with NO allowed items, deliberately: its lamp is spent on
		// Experience and leaves the pack again, so it counts completions and records nothing.
		if len(r.Allowed) == 0 && r.Name != "Genie" {
			t.Errorf("%s has no allowed rewards", r.Name)
		}
		if r.Allowed[objStrangeBox] || r.Allowed[objLamp] {
			t.Errorf("%s allows its own event item as a reward", r.Name)
		}
		s := r.Source()
		if s.Kind != KindRandomEvent || s.Action != ActionEvent || s.Name != r.Name {
			t.Errorf("%s source = %+v", r.Name, s)
		}
		if (s.Model == "") != (s.ID < 0) {
			t.Errorf("%s: a model needs an id and an id needs a model: %+v", r.Name, s)
		}
	}
}

func TestMeasureStrangeBox(t *testing.T) {
	box, _ := RandomEventReward("Strange box")
	// One of three boxes solved: content pays nothing yet, so a coin arriving now is not it.
	w := MeasureEventWindow(box, Counts{objStrangeBox: 3, objCoins: 5}, Counts{objStrangeBox: 2, objCoins: 8, testShrimps: 1}, true)
	if w.Completed || len(w.Reward) != 0 || len(w.Contested) != 0 {
		t.Fatalf("partial box window = %+v", w)
	}
	if w.Ignored[objCoins] != 3 || w.Ignored[testShrimps] != 1 {
		t.Fatalf("partial box window ignored = %v", w.Ignored)
	}
	// The last box: the reward and a shrimp the fishing action landed in the same window.
	w = MeasureEventWindow(box, Counts{objStrangeBox: 1, objCoins: 5}, Counts{objCoins: 85, testShrimps: 2}, false)
	if !w.Completed || !reflect.DeepEqual(w.Reward, Counts{objCoins: 80}) || w.Ignored[testShrimps] != 2 {
		t.Fatalf("final box window = %+v", w)
	}
	// A box replicating is the event, not a loss or a gain of anything else.
	w = MeasureEventWindow(box, Counts{objStrangeBox: 1}, Counts{objStrangeBox: 2}, false)
	if w.Completed || len(w.Reward) != 0 || len(w.Contested) != 0 {
		t.Fatalf("replicating box window = %+v", w)
	}
}

func TestMeasureDialogEvents(t *testing.T) {
	old, _ := RandomEventReward("Mysterious Old Man")
	w := MeasureEventWindow(old, Counts{testShrimps: 4}, Counts{testShrimps: 5, testCasket: 1}, true)
	if !w.Completed || !reflect.DeepEqual(w.Reward, Counts{testCasket: 1}) {
		t.Fatalf("old man window = %+v", w)
	}
	// Still present after handling: the reward arriving IS the completion.
	w = MeasureEventWindow(old, Counts{}, Counts{objKebab: 1}, false)
	if !w.Completed || !reflect.DeepEqual(w.Reward, Counts{objKebab: 1}) {
		t.Fatalf("old man still-present window = %+v", w)
	}
	// An already-happy Old Man cleared again pays nothing and is not a second completion.
	w = MeasureEventWindow(old, Counts{objKebab: 1}, Counts{objKebab: 1}, true)
	if w.Completed {
		t.Fatalf("old man goodbye window = %+v", w)
	}
	mime, _ := RandomEventReward("mime")
	if w = MeasureEventWindow(mime, Counts{}, Counts{}, true); !w.Completed {
		t.Fatalf("mime cleared with an emote unlock only = %+v", w)
	}
	// The Old Man handing over a Strange box is the box event arriving, not his own reward.
	w = MeasureEventWindow(old, Counts{}, Counts{objStrangeBox: 1}, true)
	if w.Completed || len(w.Reward) != 0 {
		t.Fatalf("old man box delivery window = %+v", w)
	}
	dwarf, _ := RandomEventReward("Drunken Dwarf")
	w = MeasureEventWindow(dwarf, Counts{objKebab: 1}, Counts{objKebab: 2, objBeer: 1, objCoins: 3}, true)
	if !reflect.DeepEqual(w.Reward, Counts{objKebab: 1, objBeer: 1}) || w.Ignored[objCoins] != 3 {
		t.Fatalf("dwarf window = %+v", w)
	}
}

func TestMeasureContestedWindowRecordsNothing(t *testing.T) {
	box, _ := RandomEventReward("Strange box")
	// A casket burst overlapping the last box: coins and gems could be either's.
	w := MeasureEventWindow(box, Counts{objStrangeBox: 1, testCasket: 3}, Counts{testCasket: 1, objCoins: 300, 1623: 1}, false)
	if w.Completed || len(w.Reward) != 0 {
		t.Fatalf("contested window recorded %+v", w)
	}
	if w.Contested[testCasket] != 2 || w.Ignored[objCoins] != 300 || w.Ignored[1623] != 1 {
		t.Fatalf("contested window = %+v", w)
	}
}

// TestIsEventOnlyItemKeepsEventRewardsOffGatheringCards pins the filter users asked
// for, item by item, in their own words (2026-09-21):
//
//	"strange fruit steel arrow chaos rune iron arrow kebab strange box beer death rune
//	 mithril ore nature rune casket spinach roll all random event items not from fishing
//	 spots or woodcutting spots"
//
// Live tree cards really did read "Oak logs x8048, Casket x1, Axe handle x1, Beer x1": a random
// event firing mid-chop drops its reward into the same pack the window is measuring.
//
// The two exceptions are the point of the test. A blanket rule would have deleted the fleet's
// largest loot line, because a CASKET is both a Mysterious Old Man reward and a genuine big-net
// fishing drop, and ORE is both a maze reward and something mining produces.
func TestIsEventOnlyItemKeepsEventRewardsOffGatheringCards(t *testing.T) {
	for _, c := range []struct {
		name string
		id   int
		want bool
	}{
		{"strange fruit (Strange plant)", objTriffidFruit, true},
		{"kebab (Dwarf / Old Man)", objKebab, true},
		{"beer (Drunken Dwarf)", objBeer, true},
		{"spinach roll (Old Man)", objSpinachRoll, true},
		{"strange box (its own item)", objStrangeBox, true},
		{"lamp (Genie)", objLamp, true},
		{"chaos rune (maze)", 562, true},
		{"death rune (maze)", 560, true},
		{"nature rune (maze)", 561, true},
		{"iron arrow (maze)", 884, true},
		{"steel arrow (maze)", 886, true},

		// Gatherable anyway: these must stay, or real measurements are lost.
		{"casket — big-net fishing gives these", objCasket, false},
		{"coins — every filter handles these on its own terms", objCoins, false},

		// Ore and coal were exempted here at first on the reasoning that mining produces them.
		// Wrong for this list: it is read by the FISHING and WOODCUTTING filters, and neither a
		// spot nor a tree has ever produced an ore. They come off the maze's reward table, so
		// they belong on the maze's card (owner, 2026-09-21).
		{"noted mithril ore — the maze gives this, no spot or tree does", 448, true},
		{"noted coal — likewise", 454, true},

		// Ordinary gathered things are never event-only.
		{"an id no event hands out", 1511, false},
	} {
		if got := IsEventOnlyItem(c.id); got != c.want {
			t.Errorf("%s: IsEventOnlyItem(%d) = %v, want %v", c.name, c.id, got, c.want)
		}
	}
}

// TestGenieIsTheEventThatGivesItsLamp pins what the Genie's table entry has to be for the Genie to
// show on the Loot panel at all (a user request: "genie still doesnt show"). Users asked
// on 2026-09-21 for it to show how many lamps it has given rather than lamps appearing on fishing
// cards; as written then (PaysOnCompletion, no Allowed items) it could never complete, so it never
// recorded one.
func TestGenieIsTheEventThatGivesItsLamp(t *testing.T) {
	g, ok := eventRewards["genie"]
	if !ok {
		t.Fatal("the Genie is not a recorded event source")
	}
	if g.ID != 409 {
		t.Errorf("Genie.ID = %d, want 409 (macro_geni). 411 is macro_SWARM and drew the event monster's model", g.ID)
	}
	if g.Own != objLamp || !g.GivesOwn {
		t.Errorf("Genie.Own = %d, GivesOwn = %v, want the Lamp (%d), handed over: it is gone again before the window ends", g.Own, g.GivesOwn, objLamp)
	}
	if len(g.Allowed) != 0 {
		t.Errorf("Genie.Allowed = %v, want empty: anything else gained near a Genie came from elsewhere", g.Allowed)
	}
	if s := g.Source(); s.Kind != KindRandomEvent || s.Name != "Genie" || s.Model != ModelNPC || s.ID != 409 || s.Key() != "event:genie:409" {
		t.Errorf("Genie source = %+v (key %q)", s, s.Key())
	}
}

// TestMeasureGenieWindows is the Genie's own measurement, one case per way the live fleet's log
// showed a hand-over end. The lamp is the only thing recorded, and it is one lamp per handling.
func TestMeasureGenieWindows(t *testing.T) {
	genie, _ := RandomEventReward("Genie")
	const food = 379
	for _, c := range []struct {
		name                  string
		before, seen, after   Counts
		completed             bool
		reward, ignored, lost Counts
	}{
		{"talked, rubbed and gone inside one handling (acct23, 01:32:43-46): the pack never shows it at the end",
			Counts{}, Counts{objLamp: 1}, Counts{}, true, Counts{objLamp: 1}, Counts{}, Counts{}},
		{"talked and the lamp is still in the pack at the end (acct31, 01:33:00-04): the standalone Lamp event spends it later",
			Counts{}, nil, Counts{objLamp: 1}, true, Counts{objLamp: 1}, Counts{}, Counts{}},
		{"no lamp ever: a Genie that was somebody else's, or whose reply was not accepted",
			Counts{}, nil, Counts{}, false, Counts{}, Counts{}, Counts{}},
		{"a lamp already in the pack that is still there: handed over and counted by an earlier window",
			Counts{objLamp: 1}, Counts{objLamp: 1}, Counts{objLamp: 1}, false, Counts{}, Counts{}, Counts{}},
		{"a lamp already in the pack, spent in this window: the same earlier lamp, not a second event",
			Counts{objLamp: 1}, nil, Counts{}, false, Counts{}, Counts{}, Counts{}},
		{"a second lamp on top of a leftover one, spent: that one is new",
			Counts{objLamp: 1}, Counts{objLamp: 2}, Counts{objLamp: 1}, true, Counts{objLamp: 1}, Counts{}, Counts{}},
		{"two lamps in the pack at the end is still one hand-over per handling",
			Counts{}, nil, Counts{objLamp: 2}, true, Counts{objLamp: 1}, Counts{}, Counts{}},
		{"a loss elsewhere does not unsay the lamp: nothing but the Genie produces one",
			Counts{food: 3}, Counts{objLamp: 1}, Counts{food: 2}, true, Counts{objLamp: 1}, Counts{}, Counts{food: 1}},
		{"anything else gained is somebody else's, and a box arriving does not veto the lamp",
			Counts{}, Counts{objLamp: 1}, Counts{objCoins: 40, objStrangeBox: 1}, true, Counts{objLamp: 1}, Counts{objCoins: 40, objStrangeBox: 1}, Counts{}},
	} {
		w := MeasureEventWindowSeen(genie, c.before, c.seen, c.after, true)
		if w.Completed != c.completed {
			t.Errorf("%s: Completed = %v, want %v (%+v)", c.name, w.Completed, c.completed, w)
		}
		if !reflect.DeepEqual(w.Reward, c.reward) || !reflect.DeepEqual(w.Ignored, c.ignored) || !reflect.DeepEqual(w.Contested, c.lost) {
			t.Errorf("%s: reward %v ignored %v contested %v, want %v %v %v", c.name, w.Reward, w.Ignored, w.Contested, c.reward, c.ignored, c.lost)
		}
	}
	// The plain entry point is the same function without readings of its own.
	if w := MeasureEventWindow(genie, Counts{}, Counts{objLamp: 1}, false); !w.Completed || !reflect.DeepEqual(w.Reward, Counts{objLamp: 1}) {
		t.Errorf("MeasureEventWindow with the lamp in the pack = %+v", w)
	}
	// And the readings change nothing for an event that does not give its own item.
	old, _ := RandomEventReward("Mysterious Old Man")
	if w := MeasureEventWindowSeen(old, Counts{}, Counts{objLamp: 1, objKebab: 5}, Counts{}, true); w.Completed || len(w.Reward) != 0 {
		t.Errorf("an Old Man window with a stray reading = %+v", w)
	}
}

// TestEventOwnItemsBelongOnlyToTheirEvent: 195 Lamps and Strange boxes sat on fourteen kill, casket
// and stall cards in the live file (owner: "currently lamps are showing up in wrong sections").
func TestEventOwnItemsBelongOnlyToTheirEvent(t *testing.T) {
	cow := Source{Kind: KindNPC, Action: ActionKill, Name: "Cow", ID: 81, Level: 2}
	genie, _ := RandomEventReward("Genie")
	box, _ := RandomEventReward("Strange box")
	old, _ := RandomEventReward("Mysterious Old Man")
	for _, c := range []struct {
		name string
		src  Source
		id   int
		want bool
	}{
		{"a Lamp on a Cow", cow, objLamp, true},
		{"a Lamp on a casket", Openable("Casket", 405), objLamp, true},
		{"a Lamp on a pickup", Source{Kind: KindPickup, Action: ActionTake, Name: "Knife", ID: 946}, objLamp, true},
		{"a Strange box on a Cow", cow, objStrangeBox, true},
		{"a Lamp on the Strange box event", box.Source(), objLamp, true},
		{"a Strange box on the Genie", genie.Source(), objStrangeBox, true},
		{"a Lamp on the Old Man", old.Source(), objLamp, true},
		{"a Lamp on an event the table does not know", Source{Kind: KindRandomEvent, Action: ActionEvent, Name: "Pillory", ID: 1}, objLamp, true},
		{"the Genie's Lamp on the Genie", genie.Source(), objLamp, false},
		{"the Genie's Lamp on a legacy-id Genie record", Source{Kind: KindRandomEvent, Action: ActionEvent, Name: "Genie", ID: 411, Model: ModelNPC}, objLamp, false},
		{"the Strange box on its own event", box.Source(), objStrangeBox, false},
		// A Casket is a reward, not an event's own item: big-net fishing gives them too.
		{"a Casket on a Cow", cow, objCasket, false},
		{"coins on a Cow", cow, objCoins, false},
	} {
		if got := ForeignEventItem(c.src, c.id); got != c.want {
			t.Errorf("%s: ForeignEventItem = %v, want %v", c.name, got, c.want)
		}
	}
	// WithoutForeignEventItems hands back the same slice when nothing is foreign, and never
	// changes the one it was given.
	clean := []Item{{ID: 526, Count: 1}, {ID: objCoins, Count: 3}}
	if got := WithoutForeignEventItems(cow, clean); len(got) != 2 || &got[0] != &clean[0] {
		t.Errorf("a clean list was copied or changed: %+v", got)
	}
	dirty := []Item{{ID: 526, Count: 1}, {ID: objLamp, Count: 2}, {ID: objStrangeBox, Count: 1}, {ID: objCoins, Count: 3}}
	got := WithoutForeignEventItems(cow, dirty)
	if !reflect.DeepEqual(got, []Item{{ID: 526, Count: 1}, {ID: objCoins, Count: 3}}) {
		t.Errorf("WithoutForeignEventItems = %+v", got)
	}
	if len(dirty) != 4 || dirty[1].ID != objLamp {
		t.Errorf("the caller's slice was changed: %+v", dirty)
	}
}

// TestTallyNeverFilesAnEventsOwnItemOnAnotherSource is the guard behind every window: a Drop that
// carries a Lamp for a Cow loses the Lamp, keeps the rest, and the Genie keeps its own.
func TestTallyNeverFilesAnEventsOwnItemOnAnotherSource(t *testing.T) {
	tally := NewTally(nil)
	cow := Source{Kind: KindNPC, Action: ActionKill, Name: "Cow", ID: 81, Level: 2}
	genie, _ := RandomEventReward("Genie")
	tally.Add(Drop{Source: cow, Items: []Item{{ID: 526, Name: "Bones", Count: 1}, {ID: objLamp, Name: "Lamp", Count: 1}, {ID: objStrangeBox, Name: "Strange box", Count: 1}}})
	// A drop whose only item was foreign is nothing at all, not an empty record.
	tally.Add(Drop{Source: Source{Kind: KindNPC, Action: ActionKill, Name: "Chicken", ID: 41}, Items: []Item{{ID: objLamp, Name: "Lamp", Count: 1}}})
	tally.Add(Drop{Source: genie.Source(), Events: 1, Items: []Item{{ID: objLamp, Name: "Lamp", Count: 1}}})
	byKey := map[string]Totals{}
	for _, s := range tally.Snapshot() {
		byKey[s.Source.Key()] = s
	}
	if len(byKey) != 2 {
		t.Fatalf("sources = %+v, want the Cow and the Genie only", byKey)
	}
	if c := byKey[cow.Key()]; len(c.Items) != 1 || c.Items[0].ID != 526 {
		t.Errorf("the Cow kept %+v, want its Bones alone", c.Items)
	}
	if g := byKey[genie.Source().Key()]; g.Events != 1 || len(g.Items) != 1 || g.Items[0].ID != objLamp || g.Items[0].Count != 1 {
		t.Errorf("the Genie = %+v, want one event and its one Lamp", g)
	}
}

// TestMigrateRewritesARandomEventToItsRightId: the live file held `event:genie:411` (npc 411 is the
// Swarm, so the Genie was drawn as one) and `event:mysterious old man:2344` (no npc at all).
func TestMigrateRewritesARandomEventToItsRightId(t *testing.T) {
	for _, c := range []struct {
		name         string
		from         Source
		wantID       int
		wantKey      string
		wantModel    string
		wantRewrites bool
	}{
		{"the Genie as the Swarm", Source{Kind: KindRandomEvent, Action: ActionEvent, Name: "Genie", ID: 411, Model: ModelNPC}, 409, "event:genie:409", ModelNPC, true},
		{"the Old Man as a non-npc", Source{Kind: KindRandomEvent, Action: ActionEvent, Name: "Mysterious Old Man", ID: 2344, Model: ModelNPC}, 410, "event:mysterious old man:410", ModelNPC, true},
		{"a right one is left as it is", Source{Kind: KindRandomEvent, Action: ActionEvent, Name: "Drunken Dwarf", ID: 956, Model: ModelNPC}, 956, "event:drunken dwarf:956", ModelNPC, false},
		{"the Maze keeps no model", Source{Kind: KindRandomEvent, Action: ActionEvent, Name: "Maze", ID: -1}, -1, "event:maze:-1", "", false},
		{"a name spelled another way is the same event", Source{Kind: KindRandomEvent, Action: ActionEvent, Name: " strange PLANT ", ID: 999, Model: ModelNPC}, 407, "event:strange plant:407", ModelNPC, true},
		{"an event the table does not know is left alone", Source{Kind: KindRandomEvent, Action: ActionEvent, Name: "Pillory", ID: 77, Model: ModelNPC}, 77, "event:pillory:77", ModelNPC, false},
	} {
		got := Migrate(c.from)
		if got.ID != c.wantID || got.Key() != c.wantKey || got.Model != c.wantModel {
			t.Errorf("%s: Migrate = %+v (key %q), want id %d key %q model %q", c.name, got, got.Key(), c.wantID, c.wantKey, c.wantModel)
		}
		if (got.Key() != c.from.Key()) != c.wantRewrites {
			t.Errorf("%s: key %q -> %q, rewrites = %v", c.name, c.from.Key(), got.Key(), c.wantRewrites)
		}
		if again := Migrate(got); again != got {
			t.Errorf("%s: Migrate is not idempotent: %+v then %+v", c.name, got, again)
		}
	}
	// A Swarm that is a Swarm (a kill, an npc source) is not a random event and is never touched.
	swarm := Source{Kind: KindNPC, Action: ActionKill, Name: "Genie", ID: 411}
	if got := Migrate(swarm); got != swarm {
		t.Errorf("a killed npc named Genie was rewritten: %+v", got)
	}
}

// TestEveryRandomEventIsOneCardWithARowEach is the owner's "all random events should be given just
// 1 consolidated panel": one group key for the lot (so the server counts a card's accounts as the
// union over every event), and one part key per event (so each keeps its own row). The JS side pins
// the same strings (tools/panel/loot-merge.test.mjs).
func TestEveryRandomEventIsOneCardWithARowEach(t *testing.T) {
	groups, parts := map[string]bool{}, map[string]bool{}
	for _, r := range RandomEventRewards() {
		s := r.Source()
		groups[s.GroupKey()] = true
		parts[s.PartKey()] = true
	}
	if len(groups) != 1 || !groups["randomevent"] {
		t.Errorf("group keys = %v, want exactly randomevent", groups)
	}
	if len(parts) != len(RandomEventRewards()) {
		t.Errorf("part keys = %v, want one per event (%d)", parts, len(RandomEventRewards()))
	}
	if k := (Source{Kind: KindRandomEvent, Action: ActionEvent, Name: "Genie", ID: 409, Model: ModelNPC}).PartKey(); k != "randomevent|event|genie" {
		t.Errorf("Genie PartKey = %q", k)
	}
	// Other kinds are untouched.
	if k := (Source{Kind: KindNPC, Action: ActionKill, Name: "Hill Giant", ID: 117}).GroupKey(); k != "npc|kill|hill giant" {
		t.Errorf("an npc card's GroupKey = %q", k)
	}
}

// TestUnnotedOreIsNeverFilteredSoAMinerCanCountIt guards the distinction that lets a future
// ProgressiveMiner work with no change to this package.
//
// The maze pays in CERTS -- 448 cert_mithril_ore, 454 cert_coal -- so those are event-only. A
// pickaxe produces 447 mithril_ore and 453 coal, which are different ids and appear in no
// event's Allowed set, so they must never be filtered off a rock's card.
func TestUnnotedOreIsNeverFilteredSoAMinerCanCountIt(t *testing.T) {
	for _, c := range []struct {
		name string
		id   int
		want bool
	}{
		{"cert_mithril_ore (maze pays this)", 448, true},
		{"cert_coal (maze pays this)", 454, true},
		{"mithril_ore — what a pickaxe produces", 447, false},
		{"coal — what a pickaxe produces", 453, false},
	} {
		if got := IsEventOnlyItem(c.id); got != c.want {
			t.Errorf("%s: IsEventOnlyItem(%d) = %v, want %v", c.name, c.id, got, c.want)
		}
	}
}
