package randomevents

import (
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
)

// These cover the two name-based event kinds this task adds: KindDialogNpc and
// KindBoxLamp (see names.go). Detect() only reaches this logic once a NameResolver is
// registered via SetNameResolver, so every test here installs a fakeResolver and
// restores the previous (nil, in the normal test-run case) one afterwards — tests must
// not leak a resolver into unrelated tests run in the same process, in particular
// detect_test.go/detect_extra_test.go's existing coverage of the "no resolver" default
// behavior.

// fakeResolver is a trivial synthetic NameResolver, standing in for *cachecfg.Config
// without building a real config archive end-to-end (see names.go's NameResolver doc
// comment for why the interface exists at all).
type fakeResolver struct {
	npcNames map[int]string
	npcOps   map[int][5]string
	objNames map[int]string
}

func (f fakeResolver) Npc(id int) (cachecfg.Npc, bool) {
	name, ok := f.npcNames[id]
	if !ok {
		return cachecfg.Npc{}, false
	}
	return cachecfg.Npc{ID: id, Name: name, Ops: f.npcOps[id]}, true
}

// CertLink/CertTemplate must be -1, not the Go zero value: the real decoder writes them
// through id32, which maps "absent" to -1, and cachecfg.Obj.Noted() is literally
// `CertTemplate != -1`. A zero-valued Obj therefore reports itself as a BANK NOTE, so any
// caller that skips notes — strangeFruitCount is the one that bit us — silently counted
// nothing and the two plant-receipt tests failed for a reason that had nothing to do with
// the behaviour they were written to check. Build the ordinary-item shape the decoder
// actually produces.
func (f fakeResolver) Obj(id int) (cachecfg.Obj, bool) {
	name, ok := f.objNames[id]
	if !ok {
		return cachecfg.Obj{}, false
	}
	return cachecfg.Obj{ID: id, Name: name, CertLink: -1, CertTemplate: -1}, true
}

// withResolver installs r for the duration of one test and restores whatever was
// registered before (normally nil) when it returns.
func withResolver(t *testing.T, r NameResolver) {
	t.Helper()
	prev := currentResolver()
	SetNameResolver(r)
	t.Cleanup(func() { SetNameResolver(prev) })
}

func TestDetectNoResolverSkipsNameBasedEvents(t *testing.T) {
	// Sanity check: without SetNameResolver, an NPC type id that would otherwise match
	// a dialog name (if a resolver were installed) is silently ignored, and an
	// inventory full of box/lamp item ids never gets checked at all — exactly today's
	// pre-this-task behavior, which cmd/barbtest depends on.
	SetNameResolver(nil)
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPC(s, 1, 500, 1, 0)
	setFullInventory(s, 5064, [2]int32{995, 1})
	if got := Detect(s); got != nil {
		t.Fatalf("Detect() with no resolver = %+v, want nil", got)
	}
}

func TestDetectDialogNpcWithinEngageDistance(t *testing.T) {
	for i, name := range dialogNpcNames {
		typeID := 500 + i
		withResolver(t, fakeResolver{npcNames: map[int]string{typeID: name}})
		s := gamestate.New()
		setTile(s, 3200, 3200, 0)
		// An unknown-owner Genie, Old Man or Dwarf is only its owner's event.
		heardOwnEvent(s, name)
		addNPC(s, 9, typeID, dialogEngageDist, 0) // exact threshold
		got := Detect(s)
		if got == nil || got.Kind != KindDialogNpc || got.Name != name || got.NpcIndex != 9 || got.NpcType != typeID {
			t.Errorf("dialog npc %q at dist %d: Detect() = %+v", name, dialogEngageDist, got)
		}
	}
}

func TestDetectDialogNpcJustOutsideEngageDistance(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie"}})
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPC(s, 9, 500, dialogEngageDist+1, 0)
	if got := Detect(s); got != nil {
		t.Fatalf("dialog npc just past threshold: Detect() = %+v, want nil", got)
	}
}

// TestDetectDialogNpcPicksNearestAmongSeveral guards the real fix this session: at a
// busy shared camp "when multiple randoms spawn for multiple people in our area" (the
// account owner's own report), Detect() used to return on the FIRST matching dialog NPC
// in state.NPCs()'s own iteration order — which has nothing to do with distance — so a
// farther candidate could win over a closer, cheaper-to-check-and-dismiss one. Adds two
// candidates at different distances in an order that would expose a first-wins bug
// (farther one added first) and checks the nearer one is the one actually returned.
func TestDetectDialogNpcPicksNearestAmongSeveral(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie", 501: "Strange Plant"}})
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPCs(s,
		npcSpec{9, 500, 10, 0}, // farther, listed first
		npcSpec{3, 501, 2, 0},  // nearer, listed second
	)
	got := Detect(s)
	if got == nil || got.Kind != KindDialogNpc || got.NpcIndex != 3 || got.Name != "Strange Plant" {
		t.Fatalf("Detect() = %+v, want the nearer candidate (npc 3, Strange Plant)", got)
	}
}

func TestDetectDialogNpcNameMatchIsCaseInsensitive(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "gEnIe"}})
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	heardOwnGenie(s)
	addNPC(s, 1, 500, 1, 0)
	got := Detect(s)
	if got == nil || got.Kind != KindDialogNpc || got.Name != "gEnIe" {
		t.Fatalf("case-insensitive name match: Detect() = %+v", got)
	}
}

func TestDetectIgnoresNamedNpcNotOnTheList(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Man"}})
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPC(s, 1, 500, 1, 0)
	if got := Detect(s); got != nil {
		t.Fatalf("ordinary named npc: Detect() = %+v, want nil", got)
	}
}

func TestDetectBoxLampMatchesEitherItem(t *testing.T) {
	for _, name := range boxLampItemNames {
		withResolver(t, fakeResolver{objNames: map[int]string{831: name}})
		s := gamestate.New()
		setTile(s, 3200, 3200, 0)
		setFullInventory(s, 5064, [2]int32{995, 100}, [2]int32{831, 1})
		got := Detect(s)
		if got == nil || got.Kind != KindBoxLamp || got.Name != name {
			t.Errorf("box/lamp item %q: Detect() = %+v", name, got)
			continue
		}
		if got.ItemID != 831 || got.Slot != 1 || got.InvComID != 5064 {
			t.Errorf("box/lamp item %q: location fields = itemID=%d slot=%d invComID=%d, want 831/1/5064", name, got.ItemID, got.Slot, got.InvComID)
		}
	}
}

func TestDetectBoxLampIgnoresOrdinaryItems(t *testing.T) {
	withResolver(t, fakeResolver{objNames: map[int]string{995: "Coins"}})
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	setFullInventory(s, 5064, [2]int32{995, 100})
	if got := Detect(s); got != nil {
		t.Fatalf("ordinary item: Detect() = %+v, want nil", got)
	}
}

func TestDetectTrappedTakesPriorityOverNamedEvents(t *testing.T) {
	withResolver(t, fakeResolver{
		npcNames: map[int]string{500: "Genie"},
		objNames: map[int]string{831: "Antique lamp"},
	})
	s := gamestate.New()
	setTile(s, mazeSquareMX*64, mazeSquareMZ*64, 0)
	addNPC(s, 1, 500, 1, 0)
	setFullInventory(s, 5064, [2]int32{831, 1})
	got := Detect(s)
	if got == nil || got.Kind != KindTrapped || got.Name != "maze" {
		t.Fatalf("trapped should take priority over named events: Detect() = %+v", got)
	}
}

func TestDetectHazardTakesPriorityOverDialogNpc(t *testing.T) {
	// A dialog-list NPC and a hazard-range whirlpool NPC both within range: the loop
	// checks whirlpool/hostile before dialog for each NPC in turn, so a whirlpool at
	// the front of the NPC list wins even though a resolver is installed.
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie"}})
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPC(s, 1, 403, 2, 0) // whirlpool, dist 2 <= 3
	got := Detect(s)
	if got == nil || got.Kind != KindHazard || got.Name != "whirlpool" {
		t.Fatalf("hazard should take priority: Detect() = %+v", got)
	}
}

func TestTalkToOptionUsesResolvedOp(t *testing.T) {
	withResolver(t, fakeResolver{
		npcNames: map[int]string{500: "Genie"},
		npcOps:   map[int][5]string{500: {"", "Talk-to", "", "", ""}},
	})
	ev := &Event{NpcType: 500}
	if got := talkToOption(ev); got != 2 {
		t.Fatalf("talkToOption() = %d, want 2 (Talk-to is ops[1])", got)
	}
}

func TestTalkToOptionFallsBackToOneWithoutAMatch(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie"}})
	ev := &Event{NpcType: 500}
	if got := talkToOption(ev); got != 1 {
		t.Fatalf("talkToOption() with no Talk-to op = %d, want fallback 1", got)
	}

	SetNameResolver(nil)
	if got := talkToOption(ev); got != 1 {
		t.Fatalf("talkToOption() with no resolver = %d, want fallback 1", got)
	}
}

// TestDetectBoxLampIgnoresAStaleBankMirror is the ghost lamp measured with -phases ghostlamp and
// on the lab e2e mules (2026-09-23): gamestate keeps the bank's deposit-side mirror of the pack
// (2006) after the bank closes, so a lamp rubbed away from the real backpack still "sits" in the
// mirror, the server drops any OPHELD sent against the closed component, and the guardian gave up
// on it every 45s for ever.
//
// THE EMPTY-PACK CASE IS THE ONE THAT MATTERS, and the first version of this fix missed it:
// KnownInventories leaves EMPTY components out, so a rule that looked for the backpack in that
// map found nothing the moment the lamp -- the pack's only item -- was used, fell back to scanning
// every component, and found the ghost. The lab run of that version failed exactly this way.
func TestDetectBoxLampIgnoresAStaleBankMirror(t *testing.T) {
	withResolver(t, fakeResolver{objNames: map[int]string{2528: "Lamp", 995: "Coins"}})
	for _, c := range []struct {
		name string
		pack [][2]int32
	}{
		{"the pack emptied by using the lamp", nil},
		{"the pack holding other things", [][2]int32{{995, 100}}},
	} {
		s := gamestate.New()
		setTile(s, 3200, 3200, 0)
		setFullInventory(s, actions.PlayerPack, c.pack...)
		setFullInventory(s, 2006, [2]int32{995, 100}, [2]int32{2528, 1})
		if got := Detect(s); got != nil {
			t.Errorf("%s: a lamp only in the stale bank mirror is not an event: Detect() = %+v", c.name, got)
		}
	}
	// With the lamp really in the pack it is found there, never in the mirror, so the rub is
	// sent against a component the server will accept.
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	setFullInventory(s, 2006, [2]int32{995, 100}, [2]int32{2528, 1})
	setFullInventory(s, actions.PlayerPack, [2]int32{995, 100}, [2]int32{2528, 1})
	got := Detect(s)
	if got == nil || got.Kind != KindBoxLamp || got.InvComID != actions.PlayerPack || got.Slot != 1 {
		t.Fatalf("a lamp in the pack: Detect() = %+v, want KindBoxLamp at %d slot 1", got, actions.PlayerPack)
	}
	// And an account whose backpack component has never been sent keeps the old any-component
	// scan, so nothing that used to be detected stops being detected.
	s = gamestate.New()
	setTile(s, 3200, 3200, 0)
	setFullInventory(s, 5064, [2]int32{2528, 1})
	if got := Detect(s); got == nil || got.InvComID != 5064 {
		t.Fatalf("no backpack known yet: Detect() = %+v, want the lamp in 5064", got)
	}
}
