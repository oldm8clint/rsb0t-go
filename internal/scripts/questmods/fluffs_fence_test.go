package questmods

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The way over the lumber yard's fence, on the real collision pack, loc index and production config (skipped without
// RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG). The climb in ends on the fence loc's own tile, which the pack has
// blocked: a reviewer put a simulated account there and found every pack walk from it unreachable, so the module must step off it
// before it plans anything. The simulated server below plays the parts of the engine that matter: a walk click takes the account
// to its last waypoint, the fence's Climb-over moves it across the wall by the content script's own rule (quest_fluffs.rs2
// [oploc1,gertrudefence]: from south of the fence in onto its tile, from its row or north of it out to the tile south), and the
// ladder moves it one level at the same x,z.

type fenceWorld struct {
	t   *testing.T
	sim *actionsim.Session
	d   *questrunner.Deps
	log []string
	// walks are the destinations of the walk clicks sent, in order.
	walks []tile.Tile
	// dropWalks and dropFence are the walk clicks and fence climbs the server ignores before it starts answering.
	dropWalks, dropFence int
	// refuseOut is the number of outward fence climbs the server refuses the way the script does when another account holds the fence:
	// it teleports the account onto the fence tile (p_teleport to loc_coord) and returns.
	refuseOut int
	// climbs counts the fence climbs sent.
	climbs int
}

func newFenceWorld(t *testing.T, at tile.Tile) *fenceWorld {
	t.Helper()
	pk := fillerPack(t)
	ixPath, cfgPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if ixPath == "" || cfgPath == "" {
		t.Skip("RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG are not both set")
	}
	ix, err := locindex.Load(ixPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	old := fluffsLandWait
	fluffsLandWait = 40 * time.Millisecond
	t.Cleanup(func() { fluffsLandWait = old })

	w := &fenceWorld{t: t}
	w.sim = actionsim.New(actionsim.Objs{Stackable: func(id int32) bool { return id == questplan.CoinsID }}, nil)
	w.sim.ShowTabs()
	w.d = &questrunner.Deps{Session: w.sim, Components: actionsim.TabComponents(), Pack: pk, Locs: ix, Config: cfg,
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
		Log:   func(s string) { w.log = append(w.log, s) }}
	w.sim.StandAtLevel(at.X, at.Z, at.Level)
	w.sim.OnSend = w.serve
	return w
}

func (w *fenceWorld) here() tile.Tile { return questrunner.CurrentTile(w.d) }

func (w *fenceWorld) logged(sub string) bool { return strings.Contains(strings.Join(w.log, "\n"), sub) }

// walkDest decodes the last waypoint of a MOVE_GAMECLICK: the start tile, then signed offsets from it (actions.sendWalk).
func walkDest(b []byte) (tile.Tile, bool) {
	if len(b) < 7 || int(b[0]) != clientprot.MOVE_GAMECLICK {
		return tile.Tile{}, false
	}
	end := 2 + int(b[1])
	if end > len(b) {
		return tile.Tile{}, false
	}
	x, z := int(b[3])<<8|int(b[4]), int(b[5])<<8|int(b[6])
	dx, dz := 0, 0
	for i := 7; i+1 < end; i += 2 {
		dx, dz = int(int8(b[i])), int(int8(b[i+1]))
	}
	return tile.Tile{X: x + dx, Z: z + dz}, true
}

// locOp decodes a loc click: the opcode, then x, z and the loc id, which are its last seven bytes (anti-cheat packets may precede).
func locOp(b []byte) (x, z, id int, ok bool) {
	if len(b) < 7 {
		return 0, 0, 0, false
	}
	op := int(b[len(b)-7])
	if op != clientprot.OPLOC1 && op != clientprot.OPLOC2 && op != clientprot.OPLOC3 && op != clientprot.OPLOC4 && op != clientprot.OPLOC5 {
		return 0, 0, 0, false
	}
	n := len(b) - 6
	return int(b[n])<<8 | int(b[n+1]), int(b[n+2])<<8 | int(b[n+3]), int(b[n+4])<<8 | int(b[n+5]), true
}

// serve is the simulated server's answer to one packet.
func (w *fenceWorld) serve(b []byte) {
	cur := w.here()
	if dest, ok := walkDest(b); ok {
		w.walks = append(w.walks, dest)
		if w.dropWalks > 0 {
			w.dropWalks--
			return
		}
		w.sim.StandAtLevel(dest.X, dest.Z, cur.Level)
		return
	}
	x, z, id, ok := locOp(b)
	if !ok {
		return
	}
	at := tile.Tile{X: x, Z: z}
	switch id {
	case questplan.LocYardFence:
		w.climbs++
		if tile.Chebyshev(cur, at) > 1 {
			w.t.Errorf("the fence climb was sent from %v, more than a tile from the fence at %v", cur, at)
		}
		if w.dropFence > 0 {
			w.dropFence--
			return
		}
		switch {
		case cur.Z < at.Z: // south of the fence: in, onto its tile
			w.sim.StandAtLevel(at.X, at.Z, 0)
		case w.refuseOut > 0: // the fence's row or north of it: the script teleports to the fence tile, then finds the lock
			w.refuseOut--
			w.sim.StandAtLevel(at.X, at.Z, 0)
		default: // out, to the tile south of the fence
			w.sim.StandAtLevel(at.X, at.Z-1, 0)
		}
	case questplan.LocYardLadder:
		if tile.Chebyshev(cur, at) > 1 {
			w.t.Errorf("the ladder was climbed from %v, more than a tile from it at %v", cur, at)
		}
		w.sim.StandAtLevel(cur.X, cur.Z, 1)
	case questplan.LocYardTop:
		if tile.Chebyshev(cur, at) > 1 {
			w.t.Errorf("the ladder was descended from %v, more than a tile from it at %v", cur, at)
		}
		w.sim.StandAtLevel(cur.X, cur.Z, 0)
	}
}

// The reviewer's repro, made a test: an account that has just climbed in stands on the fence tile, and from there the module goes
// on to the yard's inside, the crates and the walkway, the three things that failed with "no path ... unreachable".
func TestFluffsFenceInwardEndsInsideAndTheYardIsPlannableFromThere(t *testing.T) {
	w := newFenceWorld(t, questplan.YardOutside)
	if !fluffsFence(w.d, true) {
		t.Fatalf("the climb in failed: %v", w.log)
	}
	if got := w.here(); got != questplan.YardInside {
		t.Fatalf("the climb in left the account on %v, want %v", got, questplan.YardInside)
	}
	if len(w.walks) == 0 || w.walks[0] != questplan.YardInside {
		t.Errorf("the first walk issued after the climb went to %v, want %v", w.walks, questplan.YardInside)
	}
	if w.climbs != 1 {
		t.Errorf("%d fence climbs sent, want 1", w.climbs)
	}
	if !fluffsInside(w.d) {
		t.Error("fluffsInside is false on the tile the climb in ends on")
	}
	// The crates: every one has a stand the module reaches from the yard's first tile.
	for _, c := range questplan.FluffsCrateTiles {
		if !fluffsApproach(w.d, c) {
			t.Errorf("no stand beside the crate at %v could be walked to from inside the fence: %v", c, w.log)
		}
		w.sim.StandAtLevel(questplan.YardInside.X, questplan.YardInside.Z, 0)
	}
	// The walkway: the ladder stand, then the climb.
	if !fluffsToWalkway(w.d) {
		t.Fatalf("the walkway was not reached from inside the fence: %v", w.log)
	}
	if !questplan.OnWalkway(w.here()) {
		t.Errorf("fluffsToWalkway ended on %v, which is not the walkway", w.here())
	}
}

// The module meets the gap from either side: a run resumed there, a step off it that was dropped, a crossing that left it on the
// fence tile. Entering the yard from the gap is one step north and nothing else.
func TestFluffsEnterYardFromTheFenceTileStepsNorth(t *testing.T) {
	w := newFenceWorld(t, questplan.YardFence)
	if !fluffsInside(w.d) {
		t.Error("fluffsInside is false on the fence tile")
	}
	if !fluffsEnterYard(w.d) {
		t.Fatalf("entering the yard from the gap failed: %v", w.log)
	}
	if w.here() != questplan.YardInside || len(w.walks) != 1 || w.walks[0] != questplan.YardInside {
		t.Errorf("ended on %v after walks %v, want one walk to %v", w.here(), w.walks, questplan.YardInside)
	}
	if w.climbs != 0 {
		t.Errorf("%d fence climbs sent from the gap, want none", w.climbs)
	}
	// The crates and the walkway follow from there as above.
	if !fluffsToWalkway(w.d) || !questplan.OnWalkway(w.here()) {
		t.Errorf("the walkway was not reached after the step off: ended on %v: %v", w.here(), w.log)
	}
}

func TestFluffsStepOffRepeatsADroppedClickAndGivesUp(t *testing.T) {
	w := newFenceWorld(t, questplan.YardFence)
	w.dropWalks = 2
	if !fluffsStepOffFence(w.d) || w.here() != questplan.YardInside {
		t.Fatalf("two dropped clicks stopped the step off: on %v, %v", w.here(), w.log)
	}
	if len(w.walks) != 3 {
		t.Errorf("%d walk clicks sent, want 3 (two dropped, one taken)", len(w.walks))
	}

	w = newFenceWorld(t, questplan.YardFence)
	w.dropWalks = 1000
	if fluffsStepOffFence(w.d) {
		t.Error("the step off succeeded though the server never moved the account")
	}
	if len(w.walks) != fluffsStepTries {
		t.Errorf("%d walk clicks sent, want %d", len(w.walks), fluffsStepTries)
	}
	if !w.logged("could not step off the fence tile") {
		t.Errorf("the log does not say why it gave up: %v", w.log)
	}

	// Off the fence tile there is nothing to do and nothing is sent.
	w = newFenceWorld(t, questplan.YardInside)
	if !fluffsStepOffFence(w.d) || len(w.walks) != 0 {
		t.Errorf("a step off from %v sent %v", w.here(), w.walks)
	}
}

// A climb the server ignores never counts as crossed, however many tries it is given, and says so.
func TestFluffsFenceThatWillNotBeClimbedFails(t *testing.T) {
	for _, inward := range []bool{true, false} {
		from := questplan.YardInside
		if inward {
			from = questplan.YardOutside
		}
		w := newFenceWorld(t, from)
		w.dropFence = 1000
		if fluffsFence(w.d, inward) {
			t.Errorf("inward=%v: the fence counted as crossed though the server ignored every climb", inward)
		}
		if w.climbs != fluffsFenceTries {
			t.Errorf("inward=%v: %d climbs sent, want %d", inward, w.climbs, fluffsFenceTries)
		}
		if w.here() != from {
			t.Errorf("inward=%v: the account moved to %v", inward, w.here())
		}
		if !w.logged("would not be climbed") {
			t.Errorf("inward=%v: the log does not say why: %v", inward, w.log)
		}
	}
}

// Out over the fence from inside, and from the gap itself; and a climb out the script refuses (another account holds the fence)
// leaves the account on the fence tile, where the next try steps off before it climbs again.
func TestFluffsFenceOutwardFromInsideFromTheGapAndAfterARefusal(t *testing.T) {
	w := newFenceWorld(t, questplan.YardInside)
	if !fluffsFence(w.d, false) || w.here() != questplan.YardOutside {
		t.Fatalf("the climb out from inside ended on %v: %v", w.here(), w.log)
	}
	if fluffsInside(w.d) {
		t.Error("fluffsInside is true outside the fence")
	}
	if len(w.walks) != 0 || w.climbs != 1 {
		t.Errorf("a climb out from inside sent walks %v and %d climbs, want none and 1", w.walks, w.climbs)
	}

	w = newFenceWorld(t, questplan.YardFence)
	if !fluffsFence(w.d, false) || w.here() != questplan.YardOutside {
		t.Fatalf("the climb out from the gap ended on %v: %v", w.here(), w.log)
	}
	if len(w.walks) != 1 || w.walks[0] != questplan.YardInside || w.climbs != 1 {
		t.Errorf("from the gap: walks %v and %d climbs, want one step to %v then one climb", w.walks, w.climbs, questplan.YardInside)
	}

	w = newFenceWorld(t, questplan.YardInside)
	w.refuseOut = 1
	if !fluffsFence(w.d, false) || w.here() != questplan.YardOutside {
		t.Fatalf("a refused climb was not retried: on %v: %v", w.here(), w.log)
	}
	if w.climbs != 2 || len(w.walks) != 1 || w.walks[0] != questplan.YardInside {
		t.Errorf("after a refusal: %d climbs and walks %v, want 2 climbs with one step off the fence tile between", w.climbs, w.walks)
	}
}

// The whole trip the module makes: in, up to the walkway, down, and out again, each from where the last left the account.
func TestFluffsTheWholeTripInAndOut(t *testing.T) {
	w := newFenceWorld(t, questplan.YardOutside)
	if !fluffsEnterYard(w.d) || w.here() != questplan.YardInside {
		t.Fatalf("entering from outside ended on %v: %v", w.here(), w.log)
	}
	if !fluffsToWalkway(w.d) || !questplan.OnWalkway(w.here()) {
		t.Fatalf("the walkway: on %v: %v", w.here(), w.log)
	}
	// Down again, then out: the order fluffsReport takes.
	if !fluffsEnterYard(w.d) || w.here().Level != 0 || !fluffsInside(w.d) {
		t.Fatalf("coming down ended on %v: %v", w.here(), w.log)
	}
	if !fluffsFence(w.d, false) || w.here() != questplan.YardOutside {
		t.Fatalf("out over the fence ended on %v: %v", w.here(), w.log)
	}
}
