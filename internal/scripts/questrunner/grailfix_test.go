package questrunner

import (
	"os"
	"testing"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// The Holy Grail's Titan is one npc with one set of hitpoints: the quest admits one account at a time
// until it is across his bridge, and unlike Priest in Peril it does not remember the account as past
// the step for good (an account that dies beyond him meets him again from the arrival pocket).
func TestHolyGrailIsOneAccountAtATimeAndNeverRemembersBeingPast(t *testing.T) {
	id := questplan.HolyGrailID
	if !exclusiveQuests[id] || capacityOf(id) != 1 {
		t.Fatalf("grail exclusive=%v capacity=%d", exclusiveQuests[id], capacityOf(id))
	}
	grailQuest := func(done map[string]bool, probe func()) Quest {
		q := instantQuest(id, done)
		q.Module.ExclusiveDone = func(questplan.Snapshot) bool { return true }
		q.Module.Decide = func(questplan.Snapshot) questplan.Step {
			if probe != nil {
				probe()
			}
			return questplan.Step{Kind: questplan.StepDone}
		}
		return q
	}
	a := fakeEngine(&logSink{})
	a.NarrowSlots = true
	other := &Deps{}
	var otherGot bool
	probe := func() { otherGot = tryHoldQuest(id, other, time.Now()) }
	done := map[string]bool{questplan.MerlinsCrystalID: true} // the quest's ladder prerequisite
	res := a.RunLadder([]Quest{instantQuest(questplan.MerlinsCrystalID, done), grailQuest(done, probe)}, time.Hour, time.Hour)
	releaseQuest(id, other)
	if res.Outcome != LadderDone || !otherGot {
		t.Fatalf("outcome %s; a second account took the slot once the first was across: %v", res.Outcome, otherGot)
	}
	if r := a.Store.Quests[id]; r != nil && r.Hints[HintSlotDone] {
		t.Error("the slot-done hint was stored for a quest whose step comes round again")
	}
	// Even with the hint in the store (written by anything) the account queues for the slot.
	holder := &Deps{}
	if !tryHoldQuest(id, holder, time.Now()) {
		t.Fatal("could not take the slot")
	}
	defer releaseQuest(id, holder)
	b := fakeEngine(&logSink{})
	b.NarrowSlots = true
	b.Store.Record(id).Hints = map[string]bool{HintSlotDone: true}
	done2 := map[string]bool{questplan.MerlinsCrystalID: true}
	res2 := b.RunLadder([]Quest{instantQuest(questplan.MerlinsCrystalID, done2), grailQuest(done2, nil)}, time.Hour, time.Hour)
	if done2[id] || !WaitingOnSlot(res2) {
		t.Errorf("an account queued behind the Titan ran anyway: %v %s %v", done2[id], res2.Outcome, res2.Blocked)
	}
}

// A park the old Titan fight wrote (12 hours after a fight that never reached him) is cleared the
// first time the new build's ladder meets it; a park the new fight wrote is kept.
func TestTheLadderClearsAParkAnOlderBuildWrote(t *testing.T) {
	id := questplan.HolyGrailID
	old := "the Black Knight Titan did not fall (the fight's budget is spent): not trying again for 12h0m0s"
	sink := &logSink{}
	e := fakeEngine(sink)
	e.Store.NoteParkFor(id, old, time.Now(), 12*time.Hour)
	done := map[string]bool{questplan.MerlinsCrystalID: true}
	res := e.RunLadder([]Quest{instantQuest(questplan.MerlinsCrystalID, done), instantQuest(id, done)}, time.Hour, time.Hour)
	if res.Outcome != LadderDone || !done[id] {
		t.Fatalf("the old park was kept: %s %v", res.Outcome, res.Blocked)
	}
	if !sink.has("clearing a park an older build wrote") {
		t.Errorf("no log line for the cleared park: %v", sink.lines)
	}
	if parked, _ := e.Store.Parked(id, time.Now()); parked {
		t.Error("the quest is still parked")
	}

	e2 := fakeEngine(&logSink{})
	e2.Store.NoteParkFor(id, questplan.TitanParkReasonPrefix+"the fight's budget is spent", time.Now(), questplan.TitanParkSpent)
	done2 := map[string]bool{questplan.MerlinsCrystalID: true}
	res2 := e2.RunLadder([]Quest{instantQuest(questplan.MerlinsCrystalID, done2), instantQuest(id, done2)}, time.Hour, time.Hour)
	if done2[id] || res2.Outcome != LadderStuck {
		t.Errorf("a park of the new fight was cleared: %s %v", res2.Outcome, res2.Blocked)
	}
}

// A walk of several hundred tiles that timed out well on its way is asked for again; one that got
// nowhere, or crossed a level, is not.
func TestAWalkThatGotWellOnItsWayIsRepeated(t *testing.T) {
	bank := tile.Tile{X: 2809, Z: 3440}
	draynor := tile.Tile{X: 3106, Z: 3359}
	if !walkWorthRepeating(draynor, tile.Tile{X: 2723, Z: 3425}, bank) {
		t.Error("Draynor Manor to 90 tiles short of Catherby: the live case was not repeated")
	}
	if walkWorthRepeating(draynor, tile.Tile{X: 3100, Z: 3359}, bank) {
		t.Error("a walk that got 6 tiles closer was repeated")
	}
	if walkWorthRepeating(draynor, draynor, bank) {
		t.Error("a walk that did not move was repeated")
	}
	if walkWorthRepeating(tile.Tile{X: 3106, Z: 3359, Level: 2}, tile.Tile{X: 2723, Z: 3425}, bank) {
		t.Error("a walk from another level was repeated")
	}
}

// A withdrawal in a full pack banks first (live 13 Oct 2026: "withdraw Excalibur x1" with 0 free
// slots, nothing changing for eight ticks).
func TestAWithdrawalInAFullPackMakesRoom(t *testing.T) {
	for _, c := range []struct {
		free, draws int
		want        bool
	}{{0, 1, true}, {1, 1, false}, {5, 1, false}, {0, 0, false}, {2, 3, true}, {3, 3, false}} {
		if got := withdrawNeedsRoom(c.free, c.draws); got != c.want {
			t.Errorf("free %d, %d draws: %v", c.free, c.draws, got)
		}
	}
}

// The runner opens a bank booth from a tile the engine answers (ApproachLoc over LocStands, the
// collision pack's own interact tiles), never from a diagonal: after a mule hand-over at the Catherby
// post (2808,3441) the booths at 2807,3442 and 2809,3442 touch the post corner to corner, and a click
// from there is ignored in silence (freshstartrunner.WalkToUse, cf65e3bf). The live "could not use the
// bank at 2809,3440,0" of the Holy Grail kit was a six minute walk timing out, not this; this pins the
// runner's side of it.
func TestCatherbyBoothsAreOpenedFromTilesTheEngineAnswers(t *testing.T) {
	pp, ip, cp := os.Getenv("RSB0T_COLLISION_PACK"), os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if pp == "" || ip == "" || cp == "" {
		t.Skip("RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX or RSB0T_CONFIG_JAG not set")
	}
	pk, err := pathfind.Load(pp)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := locindex.Load(ip)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	d := &Deps{Pack: pk, Locs: ix, Config: cfg}
	post := tile.Tile{X: 2808, Z: 3441}
	var booths []locindex.Match
	for _, op := range []string{"Use-quickly", "Bank", "Use"} {
		if booths = ix.Find(cfg, locindex.Query{X: post.X, Z: post.Z, Radius: 3, Name: "Bank booth", Op: op}, nil); len(booths) > 0 {
			break
		}
	}
	if len(booths) < 2 {
		t.Fatalf("%d bank booths by the Catherby post", len(booths))
	}
	for _, b := range booths {
		stands := LocStands(d, b, post)
		if len(stands) == 0 {
			t.Errorf("booth %d,%d has no stand", b.X, b.Z)
			continue
		}
		for _, s := range stands {
			if !pk.CanInteractFrom(pathfind.NavPoint{X: s.X, Z: s.Z, Level: s.Level}, pathfind.NavPoint{X: b.X, Z: b.Z, Level: b.Level}) {
				t.Errorf("booth %d,%d: stand %v is not a tile the engine answers it from", b.X, b.Z, s)
			}
		}
		// The post touches the two nearest booths corner to corner: it is never a stand for them.
		if (b.X == 2807 || b.X == 2809) && b.Z == 3442 {
			for _, s := range stands {
				if s == post {
					t.Errorf("the post %v is offered as a stand for the booth at %d,%d, a diagonal of it", post, b.X, b.Z)
				}
			}
		}
	}
}
