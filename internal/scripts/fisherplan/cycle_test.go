package fisherplan

import (
	"math/rand"
	"reflect"
	"testing"
)

// Content 289 obj ids (pack/obj.pack), as the lab config archive names them.
const (
	idCasket      = 405
	idCasketNote  = 406
	idOyster      = 407
	idEmptyOyster = 409
	idPearl       = 411
	idPearlNote   = 412
	idPearls      = 413
	idTips        = 46
	idHalfTooth   = 985
	idHalfLoop    = 987
	idCrystalKey  = 989
	idChisel      = 1755
	idBass        = 363
	idMackerel    = 353
	idBoots       = 626
	idNet         = 305
	idCoins       = 995
	idSapphire    = 1623
)

var testNames = map[int]string{
	idCasket: "Casket", idCasketNote: "Casket", idOyster: "Oyster", idEmptyOyster: "Empty oyster", idPearl: "Oyster pearl",
	idPearlNote: "Oyster pearl", idPearls: "Oyster pearls", idTips: "Pearl bolttips", idHalfTooth: "Half of a key",
	idHalfLoop: "Half of a key", idCrystalKey: "Crystal key", idChisel: "Chisel", idBass: "Raw bass", idMackerel: "Raw mackerel",
	idBoots: "Leather boots", idNet: "Big fishing net", idCoins: "Coins", idSapphire: "Uncut sapphire",
}

// pack builds a view from ids in slot order; an id of 0 is an empty slot and a negative id is that
// item as a bank note.
func pack(ids ...int) []CycleItem {
	var out []CycleItem
	for slot, id := range ids {
		if id == 0 {
			continue
		}
		noted := id < 0
		if noted {
			id = -id
		}
		out = append(out, CycleItem{Slot: slot, ID: id, Name: testNames[id], Noted: noted})
	}
	return out
}

func view(fletching int, ids ...int) CycleView {
	return CycleView{Pack: pack(ids...), Opts: DefaultCycleOptions(), Fletching: fletching, Junk: IsBassOysterJunk}
}

func slots(as []CycleAction) []int {
	var out []int
	for _, a := range as {
		out = append(out, a.Slot)
	}
	return out
}

// Caskets and oysters open lowest slot first, and only the unnoted ones: a note cannot be opened, and
// the clicks that name the lowest oyster slots are the ones that find their oyster where they said.
func TestPlanOpensTakesCasketsAndOystersLowestSlotFirstAndNeverANote(t *testing.T) {
	v := view(1, idNet, idOyster, idBass, idCasket, -idCasketNote, idOyster, idEmptyOyster, idCasket)
	if got, want := slots(PlanOpens(v)), []int{1, 3, 5, 7}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PlanOpens slots = %v, want %v", got, want)
	}
	for _, a := range PlanOpens(v) {
		if a.Kind != CycleOpen {
			t.Errorf("an open planned as %q", a.Kind)
		}
	}
	// The options switch each kind off on its own.
	v.Opts.OpenCaskets = false
	if got, want := slots(PlanOpens(v)), []int{1, 5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("no caskets: %v, want just the oysters %v", got, want)
	}
	v.Opts = CycleOptions{OpenCaskets: true}
	if got, want := slots(PlanOpens(v)), []int{3, 7}; !reflect.DeepEqual(got, want) {
		t.Fatalf("no oysters: %v, want just the caskets %v", got, want)
	}
	v.Opts = CycleOptions{}
	if got := PlanOpens(v); got != nil {
		t.Fatalf("every part off still opens %v", got)
	}
}

// The junk is dropped, the Empty oyster with it, but a note and a valuable never.
func TestPlanDropsTakesTheJunkAndTheEmptyOysterButNeverANoteOrAValuable(t *testing.T) {
	v := view(1, idNet, idMackerel, idBass, idEmptyOyster, idBoots, -idMackerel, idPearl, idCasket, idHalfTooth, idCoins)
	if got, want := slots(PlanDrops(v)), []int{1, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PlanDrops slots = %v, want %v", got, want)
	}
	v.Junk = nil
	if got := PlanDrops(v); got != nil {
		t.Fatalf("a script with no junk list (ProgressiveFisher) drops %v", got)
	}
}

// Two different unnoted ids named "Half of a key" are a pair; one id alone, a note, or the casket part
// switched off is none.
func TestPlanJoinsPairsTheTwoHalvesByTheirIdsLowestSlotFirst(t *testing.T) {
	v := view(1, idHalfLoop, idBass, idHalfTooth, idHalfTooth, idHalfLoop, idHalfTooth)
	joins := PlanJoins(v)
	if len(joins) != 2 {
		t.Fatalf("%d joins from two Loop and three Tooth halves, want 2: %+v", len(joins), joins)
	}
	// Lowest slots first: the two Loop halves are slots 0 and 4, the Tooth halves 2, 3 and 5.
	want := []CycleAction{
		{Kind: CycleJoin, Slot: 0, ID: idHalfLoop, UseSlot: 2, UseID: idHalfTooth, Name: KeyHalf},
		{Kind: CycleJoin, Slot: 4, ID: idHalfLoop, UseSlot: 3, UseID: idHalfTooth, Name: KeyHalf},
	}
	if !reflect.DeepEqual(joins, want) {
		t.Fatalf("joins = %+v\nwant    %+v", joins, want)
	}
	if got := PlanJoins(view(1, idHalfTooth, idHalfTooth, idBass)); got != nil {
		t.Fatalf("two Tooth halves and no Loop half joined: %v", got)
	}
	if got := PlanJoins(view(1, idHalfTooth, -idHalfLoop)); got != nil {
		t.Fatalf("a noted half joined: %v", got)
	}
	v.Opts.OpenCaskets = false
	if got := PlanJoins(v); got != nil {
		t.Fatalf("joined with the casket part switched off: %v", got)
	}
}

// A pearl is cut only at Fletching 41 or more, with a chisel in the pack and the setting on, and never as
// a note; the plural Oyster pearls is one use as well.
func TestPlanCutsNeedFletching41AChiselAndTheSetting(t *testing.T) {
	items := []int{idNet, idPearl, idChisel, idPearls, -idPearlNote, idPearl, idBass}
	if got := PlanCuts(view(40, items...)); got != nil {
		t.Fatalf("cut at Fletching 40: %v", got)
	}
	if got := PlanCuts(view(0, items...)); got != nil {
		t.Fatalf("cut with Fletching unknown: %v", got)
	}
	cuts := PlanCuts(view(41, items...))
	if got, want := slots(cuts), []int{1, 3, 5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cut slots = %v, want %v (the note is never cut)", got, want)
	}
	for _, c := range cuts {
		if c.Kind != CycleCut || c.UseID != idChisel || c.UseSlot != 2 {
			t.Fatalf("cut %+v: want the chisel in slot 2 used on the pearl", c)
		}
	}
	if got := PlanCuts(view(99, idNet, idPearl, idBass)); got != nil {
		t.Fatalf("cut with no chisel: %v", got)
	}
	if got := PlanCuts(view(99, idNet, idPearl, -idChisel)); got != nil {
		t.Fatalf("cut with a noted chisel: %v", got)
	}
	v := view(99, items...)
	v.Opts.CutPearls = false
	if got := PlanCuts(v); got != nil {
		t.Fatalf("cut with the setting off: %v", got)
	}
}

// The second burst is the joins, the cuts, then the drops; the first is the opens with the held junk
// behind them, and with nothing to open there is no first burst at all.
func TestTheBurstsAreOpensThenJunkAndThenJoinsCutsDrops(t *testing.T) {
	v := view(41, idCasket, idMackerel, idChisel, idPearl, idHalfTooth, idHalfLoop, idEmptyOyster, idOyster)
	first := PlanFirstBurst(v)
	if got, want := slots(first), []int{0, 7, 1, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first burst slots = %v, want the opens (0, 7) then the junk (1, 6)", got)
	}
	tidy := PlanTidy(v)
	var kinds []CycleKind
	for _, a := range tidy {
		kinds = append(kinds, a.Kind)
	}
	if want := []CycleKind{CycleJoin, CycleCut, CycleDrop, CycleDrop}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("tidy kinds = %v, want %v", kinds, want)
	}
	if got := PlanFirstBurst(view(41, idMackerel, idBass)); got != nil {
		t.Fatalf("a first burst with nothing to open: %v", got)
	}
	if c := Count(append(first, tidy...)); c != (CycleCounts{Opens: 2, Joins: 1, Cuts: 1, Drops: 4}) || c.Total() != 8 {
		t.Fatalf("Count = %+v", c)
	}
}

func TestCycleDueNeedsWorkAndANearlyFullPack(t *testing.T) {
	cases := []struct {
		free int
		work bool
		want bool
	}{
		{0, true, true}, // full: fishing has stopped anyway
		{4, true, true}, // the junk drop's own threshold
		{5, true, false},
		{28, true, false},
		{0, false, false}, // full of valuables: bank instead
		{4, false, false},
	}
	for _, c := range cases {
		if got := CycleDue(c.free, c.work); got != c.want {
			t.Errorf("CycleDue(free %d, work %v) = %v, want %v", c.free, c.work, got, c.want)
		}
	}
	if HasWork(view(1, idNet, idBass)) {
		t.Error("a pack of the net and a bass has cycle work")
	}
	for name, v := range map[string]CycleView{
		"an oyster": view(1, idNet, idOyster), "a casket": view(1, idCasket), "junk": view(1, idMackerel),
		"an empty oyster": view(1, idEmptyOyster), "a key pair": view(1, idHalfTooth, idHalfLoop),
		"a cuttable pearl": view(41, idChisel, idPearl),
	} {
		if !HasWork(v) {
			t.Errorf("%s: no cycle work", name)
		}
	}
	// A pearl with no chisel (or under 41) is not work: it is banked as a pearl.
	if HasWork(view(40, idChisel, idPearl)) || HasWork(view(99, idPearl)) {
		t.Error("a pearl that cannot be cut is cycle work")
	}
}

func TestCycleTicksAndTheWalkItFitsIn(t *testing.T) {
	for _, c := range []struct{ clicks, want int }{{0, 0}, {1, 3}, {5, 3}, {6, 4}, {10, 4}, {11, 5}, {28, 8}} {
		if got := CycleTicks(c.clicks); got != c.want {
			t.Errorf("CycleTicks(%d) = %d, want %d", c.clicks, got, c.want)
		}
	}
	// The re-click radius (4) plus two tiles a tick running, one walking, for the cycle's ticks.
	if got := CycleWalkAhead(true, 10); got != 4+2*4 {
		t.Errorf("CycleWalkAhead(running, 10) = %d, want 12", got)
	}
	if got := CycleWalkAhead(false, 10); got != 4+1*4 {
		t.Errorf("CycleWalkAhead(walking, 10) = %d, want 8", got)
	}
	if CycleWalkAhead(true, 28) <= CycleWalkAhead(true, 5) {
		t.Error("a longer cycle needs no longer a walk")
	}
}

// A poll of a walk starts a cycle when it fits inside the walk, or when the pack is nearly full (it is due
// at the next hold anyway); with room to spare and a short hop it waits.
func TestWalkCycleOKFitsInTheWalkOrIsDueAnyway(t *testing.T) {
	cases := []struct {
		name         string
		ahead        int
		running      bool
		clicks, free int
		want         bool
	}{
		{"nothing to do", 40, true, 0, 0, false},
		{"fits: a long run, room to spare", 40, true, 10, 12, true},
		{"fits exactly", CycleWalkAhead(true, 12), true, 10, 12, true},
		{"a hop, room to spare: drops on the hop", 4, true, 10, 12, true},
		{"a hop, three free: drops on the hop", 4, true, 10, 3, true},
		{"a hop, two clicks, room to spare: waits", 4, true, 2, 12, false},
		{"a hop, two free: due anyway", 4, true, 10, 2, true},
		{"a hop, full: due anyway", 1, false, 3, 0, true},
		{"just short and room to spare", CycleWalkAhead(true, 12) - 1, true, 10, 9, true},
		{"just short, two clicks, room to spare", CycleWalkAhead(true, 4) - 1, true, 2, 9, false},
	}
	for _, c := range cases {
		if got := WalkCycleOK(c.ahead, c.running, c.clicks, c.free); got != c.want {
			t.Errorf("%s: WalkCycleOK(ahead %d, running %v, clicks %d, free %d) = %v, want %v", c.name, c.ahead, c.running, c.clicks, c.free, got, c.want)
		}
	}
}

// ---- a whole cycle against content's rules ----

// world applies the plan the way content and the engine do: opens first (a casket's reward, an oyster's
// pearl or shell to the lowest empty slot after the item is deleted), then the second burst. It reports
// the pack as ids by slot, and fails the test when a click names a slot that does not hold its item or
// a reward has no room.
type world struct {
	t    *testing.T
	pack [28]int
	rng  *rand.Rand
}

func (w *world) view(fletching int) CycleView {
	var items []CycleItem
	for slot, id := range w.pack {
		if id != 0 {
			items = append(items, CycleItem{Slot: slot, ID: id, Name: testNames[id]})
		}
	}
	return CycleView{Pack: items, Opts: DefaultCycleOptions(), Fletching: fletching, Junk: IsBassOysterJunk}
}

func (w *world) add(id int) {
	if id == idCoins || id == idTips { // the two stackable rewards: onto their stack when there is one
		for _, have := range w.pack {
			if have == id {
				return
			}
		}
	}
	for i, have := range w.pack {
		if have == 0 {
			w.pack[i] = id
			return
		}
	}
	w.t.Fatalf("a reward (%d) found no free slot: the cycle must never overflow the pack", id)
}

func (w *world) removeFirst(id int) {
	for i, have := range w.pack {
		if have == id {
			w.pack[i] = 0
			return
		}
	}
	w.t.Fatalf("content deleted %d and the pack holds none", id)
}

func (w *world) apply(as []CycleAction) {
	for _, a := range as {
		if w.pack[a.Slot] != a.ID {
			w.t.Fatalf("%s names slot %d for item %d, which holds %d", a.Kind, a.Slot, a.ID, w.pack[a.Slot])
		}
		switch a.Kind {
		case CycleOpen:
			switch a.ID {
			case idCasket:
				w.pack[a.Slot] = 0 // inv_delslot, then the reward
				switch w.rng.Intn(8) {
				case 0, 1, 2, 3:
					w.add(idCoins)
				case 4, 5:
					w.add(idSapphire)
				case 6:
					w.add(idHalfTooth)
				default:
					w.add(idHalfLoop)
				}
			case idOyster:
				w.removeFirst(idOyster) // inv_del: the lowest slot holding one
				if w.rng.Intn(32) < 25 {
					w.add(idPearl)
				} else {
					w.add(idEmptyOyster)
				}
			}
		case CycleDrop:
			w.pack[a.Slot] = 0
		case CycleJoin:
			if w.pack[a.UseSlot] != a.UseID {
				w.t.Fatalf("join uses slot %d for %d, which holds %d", a.UseSlot, a.UseID, w.pack[a.UseSlot])
			}
			w.removeFirst(idHalfTooth)
			w.removeFirst(idHalfLoop)
			w.add(idCrystalKey)
		case CycleCut:
			if w.pack[a.UseSlot] != idChisel {
				w.t.Fatalf("cut uses slot %d, which holds %d, not the chisel", a.UseSlot, w.pack[a.UseSlot])
			}
			w.removeFirst(a.ID)
			w.add(idTips)
		}
	}
}

func (w *world) count(id int) int {
	n := 0
	for _, have := range w.pack {
		if have == id {
			n++
		}
	}
	return n
}

// Over thousands of random packs, one cycle (first burst, then the second planned from the pack the
// first left) always: names a slot that holds the item it clicks, never overflows the pack, leaves no
// junk, no casket, no oyster, no joinable pair, and no cuttable pearl when the account may cut.
func TestAWholeCycleLeavesNothingTheCycleCouldHaveDone(t *testing.T) {
	rng := rand.New(rand.NewSource(1006))
	kinds := []int{idBass, idBass, idBass, idMackerel, idBoots, idOyster, idOyster, idCasket, idCasket, idEmptyOyster, idHalfTooth, idHalfLoop, idPearl}
	for round := 0; round < 4000; round++ {
		w := &world{t: t, rng: rng}
		w.pack[0] = idNet
		next := 1
		if rng.Intn(3) > 0 {
			w.pack[next] = idChisel
			next++
		}
		for ; next < 28 && rng.Intn(14) > 0; next++ {
			w.pack[next] = kinds[rng.Intn(len(kinds))]
		}
		fletching := []int{1, 40, 41, 99}[rng.Intn(4)]
		w.apply(PlanFirstBurst(w.view(fletching)))
		w.apply(PlanTidy(w.view(fletching))) // planned from the pack the first burst left
		after := w.view(fletching)

		for _, it := range after.Pack {
			switch it.Name {
			case "Casket", "Oyster", "Empty oyster", "Raw mackerel", "Leather boots":
				t.Fatalf("round %d (Fletching %d): a %s is left in the pack %v", round, fletching, it.Name, w.pack)
			}
		}
		if len(PlanJoins(after)) > 0 {
			t.Fatalf("round %d: a joinable pair is left: %v", round, w.pack)
		}
		_, hasChisel := ChiselIn(after.Pack)
		if hasChisel && fletching >= 41 && w.count(idPearl) > 0 {
			t.Fatalf("round %d: Fletching %d with a chisel left a pearl uncut: %v", round, fletching, w.pack)
		}
		if !hasChisel || fletching < 41 {
			if len(PlanCuts(after)) != 0 {
				t.Fatalf("round %d: a cut planned without the level or the chisel", round)
			}
		}
		if HasWork(after) {
			t.Fatalf("round %d: the cycle still has work after a whole cycle: %v", round, w.pack)
		}
	}
}
