package mastercraftrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/mastercraftplan"
)

var tBowIDs = bowstringrunner.IDs{Unstrung: tBowU, Strung: tBowStrung, String: tString}

// bowRig is an account at the booth with an empty pack, bows and strings in the bank, Fletching at
// fletchXP tenths, and the booth op sent: how the bow round starts a run.
type bowRig struct {
	e          *tickEngine
	d          *fleetrunner.Deps
	q          *QuickBank
	bows, strs int
	xp0        int
}

func newBowRig(t *testing.T, bows, strs, fletchXP int) *bowRig {
	t.Helper()
	e, d, q := newTickEngine(t, nil, []struct{ id, n int32 }{{tBowU, int32(bows)}, {tString, int32(strs)}})
	e.fletchXP = fletchXP
	e.push()
	if !q.Prime(d) {
		t.Fatal("the booth op did not go out")
	}
	return &bowRig{e: e, d: d, q: q, bows: bows, strs: strs, xp0: fletchXP}
}

// conserve: every bow is unstrung (bank or pack) or strung, each strung one used a string, and the
// strung ones are the XP's.
func (r *bowRig) conserve(t *testing.T) {
	t.Helper()
	count := func(id int32) int {
		n := int(r.e.packTotal(id))
		for _, b := range r.e.bank {
			if b.id == id {
				n += int(b.n)
			}
		}
		return n
	}
	un, strung, strs := count(tBowU), count(tBowStrung), count(tString)
	if un+strung != r.bows || strs+strung != r.strs {
		t.Fatalf("%d unstrung + %d strung of %d bows, %d strings of %d", un, strung, r.bows, strs, r.strs)
	}
	if got := (r.e.fletchXP - r.xp0) / tBowXP; got != strung {
		t.Fatalf("%d bows by the XP, %d strung", got, strung)
	}
}

// THE POINT: a bow load is FOUR engine ticks for 14 bows, with nothing waited for: Deposit-All,
// Withdraw-X 14 bows and the count, Withdraw-All of the strings, and the first use in one tick, five
// uses a tick after, the last three, a close and the booth op in the fourth. The first load of a run
// that starts with an empty pack sends a close where the deposit goes.
func TestFastBowsFourTicksALoad(t *testing.T) {
	r := newBowRig(t, 14*12+3, 14*12+9, levels.XPAtLevel(80)*10)
	const loads = 10
	res := FastBows(r.d, r.q, "Yew longbow", tBowIDs, tBowXP, FastOptions{MaxLoads: loads})
	if res.Reason != "done" || res.Loads != loads || res.Cuts != 14*loads || res.XP != 14*loads*tBowXP/10 {
		t.Fatalf("%s", res.Describe())
	}
	if r.e.refused != 0 || r.e.badBytes != 0 {
		t.Fatalf("the engine refused %d event(s), %d bad frames", r.e.refused, r.e.badBytes)
	}
	for i := 1; i < loads; i++ {
		if gap := r.e.cutTicks[14*(i+1)-1] - r.e.cutTicks[14*i-1]; gap != 4 {
			t.Fatalf("load %d took %d ticks, want 4", i, gap)
		}
	}
	for tick, n := range r.e.usesTick {
		if n > 5 {
			t.Fatalf("tick %d applied %d uses", tick, n)
		}
	}
	if res.Chunks != mastercraftplan.BowStream.Chunks(loads) || mastercraftplan.BowStream.Ticks() != 4 {
		t.Fatalf("%d chunks", res.Chunks)
	}
	if ok, _, why := bowLayout(packItems(r.d), tBowIDs); !ok || r.e.open {
		t.Fatalf("pack: %s, bank open %v", why, r.e.open)
	}
	r.conserve(t)
	// The next run starts from that pack: the deposit is a real one now.
	r.q.Prime(r.d)
	again := FastBows(r.d, r.q, "Yew longbow", tBowIDs, tBowXP, FastOptions{MaxLoads: 2})
	if again.Reason != "done" || again.Loads != 2 || r.e.refused != 0 {
		t.Fatalf("the next run: %s", again.Describe())
	}
	r.conserve(t)
}

// Fewer than 14 of either in the bank: the run does not start, the visit banks the pack and draws
// nothing, and Short says the bank's stock is why (the caller orders a kit).
func TestFastBowsShortStock(t *testing.T) {
	r := newBowRig(t, 14*2, 13, levels.XPAtLevel(80)*10)
	res := FastBows(r.d, r.q, "Yew longbow", tBowIDs, tBowXP, FastOptions{MaxLoads: 5})
	if res.Reason != "not-fast" || !res.Short || res.Load == nil || res.Load.Err != "" || len(packItems(r.d)) != 0 {
		t.Fatalf("%s / %+v", res.Describe(), res.Load)
	}
	r.conserve(t)
}

// The level-up guard on Fletching XP: the load whose booth tick (its last three uses) gains a level
// is the run's last.
func TestFastBowsEndsBeforeALevelInABoothTick(t *testing.T) {
	// The 27th bow of the run (load 2, use 13: its booth tick) reaches Fletching 81.
	xp := levels.XPAtLevel(81)*10 - 26*tBowXP - 100
	if got := mastercraftplan.BowStream.Loads(8, xp/10, tBowXP); got != 2 {
		t.Fatalf("Loads = %d, want 2", got)
	}
	r := newBowRig(t, 14*10, 14*10, xp)
	r.e.levelUps = true
	res := FastBows(r.d, r.q, "Yew longbow", tBowIDs, tBowXP, FastOptions{MaxLoads: 8})
	if res.Reason != "done" || res.Planned != 2 || res.Loads != 2 || r.e.refused != 0 || r.e.levelUpsSeen != 1 {
		t.Fatalf("%s, %d refused, %d level-ups", res.Describe(), r.e.refused, r.e.levelUpsSeen)
	}
	// The next run's booth op closes the box and opens the bank.
	r.q.Prime(r.d)
	if again := FastBows(r.d, r.q, "Yew longbow", tBowIDs, tBowXP, FastOptions{MaxLoads: 2}); again.Reason != "done" || r.e.chatBox {
		t.Fatalf("after the level-up: %s", again.Describe())
	}
	r.conserve(t)
}

// A stray click desyncs the stream; the caller's recovery (bank the whole pack, then a new run)
// resyncs it, and no bow or string is lost.
func TestFastBowsStrayClickDesyncsAndRecovers(t *testing.T) {
	r := newBowRig(t, 14*20, 14*20, levels.XPAtLevel(80)*10)
	r.e.strayAt = map[int]bool{r.e.ticks + 7: true}
	res := FastBows(r.d, r.q, "Yew longbow", tBowIDs, tBowXP, FastOptions{MaxLoads: 10})
	if res.Reason != "desync" || res.Loads >= 10 || r.e.refused == 0 {
		t.Fatalf("%s, %d refused", res.Describe(), r.e.refused)
	}
	r.conserve(t)
	// The runs that follow resync: a pack that is not a finished load is banked whole (not-fast, not
	// Short) and the next run starts from the empty pack; a pack the drain left as a finished load
	// starts a run at once. Either way a clean run comes within two tries.
	for try := 0; ; try++ {
		refused0 := r.e.refused
		r.q.Prime(r.d)
		again := FastBows(r.d, r.q, "Yew longbow", tBowIDs, tBowXP, FastOptions{MaxLoads: 4})
		r.conserve(t)
		if again.Reason == "done" && again.Loads == 4 && r.e.refused == refused0 {
			break
		}
		if try == 1 || again.Reason != "not-fast" || again.Short || len(packItems(r.d)) != 0 {
			t.Fatalf("try %d: %s, pack %v", try, again.Describe(), packItems(r.d))
		}
	}
}

// The resync visit itself: a pack that is no finished load (a stray unstrung bow among the strung
// ones) is banked whole, and nothing is drawn.
func TestFastBowsBanksAMixedPack(t *testing.T) {
	r := newBowRig(t, 14*5, 14*5, levels.XPAtLevel(80)*10)
	r.e.pack[0] = struct{ id, n int32 }{tBowStrung, 1}
	r.e.pack[1] = struct{ id, n int32 }{tBowU, 1}
	r.e.pack[20] = struct{ id, n int32 }{tString, 1}
	r.e.push()
	r.bows, r.strs = r.bows+2, r.strs+2 // the strung bow counts its string
	res := FastBows(r.d, r.q, "Yew longbow", tBowIDs, tBowXP, FastOptions{MaxLoads: 4})
	if res.Reason != "not-fast" || res.Short || len(packItems(r.d)) != 0 {
		t.Fatalf("%s, pack %v", res.Describe(), packItems(r.d))
	}
	r.e.fletchXP += tBowXP // the strung bow the rig started with
	r.conserve(t)
}

func TestBowRefusal(t *testing.T) {
	for said, want := range map[string]string{
		"You need level 70 Fletching to string that bow.":                     "refused",
		"You need to be on a members' world to gain experience in Fletching.": "members",
		"You add a string to the bow.":                                        "",
	} {
		if got, level := BowRefusal(said); got != want || (want == "refused" && level != 70) {
			t.Errorf("%q: %q %d", said, got, level)
		}
	}
}
