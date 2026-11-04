package questmods

import (
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// The Prayer rungs' bones loop in the order it takes them, over a world that is only numbers: a pack, a bank, a mule and a
// clock. What each source is worth and how much to take of it is questplan's (prayerbones_test.go); these tests are that the loop
// carries the moves out in that order, once each, and ends.

type fakeBones struct {
	need int
	pack map[int]int
	bank map[int]int
	left time.Duration

	hint   map[int]int
	hintOK bool

	mule      map[int]int
	muleKnown bool

	// What an ask does: arrives fraction of the first line in the pack (a pack's worth, 26) and banks the rest,
	// unless asksNothing.
	asksNothing bool
	noBooth     bool
	buryBroken  bool
	drawBroken  bool
	stopped     bool

	calls []string
	asked [][]questplan.ItemCount
	logs  []string
}

func newFakeBones(need int) *fakeBones {
	return &fakeBones{need: need, pack: map[int]int{}, bank: map[int]int{}, left: 20 * time.Minute}
}

func (f *fakeBones) call(s string) { f.calls = append(f.calls, s) }

func (f *fakeBones) Need() int { return max(f.need, 0) }

func (f *fakeBones) Pack() map[int]int {
	out := map[int]int{}
	for id, n := range f.pack {
		if n > 0 {
			out[id] = n
		}
	}
	return out
}

func (f *fakeBones) bury() {
	if f.buryBroken {
		return
	}
	f.need -= questplan.BoneXP10(f.pack) / 10
	f.pack = map[int]int{}
	f.left -= 35 * time.Second
}

func (f *fakeBones) Bury() { f.call("bury"); f.bury() }

func (f *fakeBones) Hint() (map[int]int, bool) { return f.hint, f.hintOK }

func (f *fakeBones) Mule() (map[int]int, bool) { return f.mule, f.muleKnown }

func (f *fakeBones) Bank() (map[int]int, bool) {
	f.call("bank")
	if f.noBooth {
		return nil, false
	}
	var last map[int]int
	for f.left >= questplan.BoneMinLeft {
		for id, n := range f.pack { // the deposit
			f.bank[id] += n
		}
		f.pack = map[int]int{}
		last = map[int]int{}
		for id, n := range f.bank {
			last[id] = n
		}
		draws := questplan.PlanBoneDraws(f.Need(), last, 27)
		if len(draws) == 0 || f.drawBroken {
			return last, true
		}
		for _, c := range draws {
			f.bank[c.ID] -= c.Count
			f.pack[c.ID] += c.Count
		}
		f.bury()
		if questplan.BoneCount(f.Pack()) > 0 || f.Need() <= 0 {
			return last, true
		}
	}
	return last, true
}

func (f *fakeBones) Ask(lines []questplan.ItemCount) (int, string) {
	f.call("ask")
	f.asked = append(f.asked, lines)
	f.left -= 7 * time.Minute
	if f.asksNothing {
		return 0, "the mule's line is too long"
	}
	room, arrived := 26, 0
	for _, l := range lines {
		f.mule[l.ID] -= l.Count
		n := min(room, l.Count)
		f.pack[l.ID] += n
		f.bank[l.ID] += l.Count - n
		room -= n
		arrived += n
	}
	return arrived, ""
}

func (f *fakeBones) Left() time.Duration { return f.left }
func (f *fakeBones) Stopped() bool       { return f.stopped }
func (f *fakeBones) Logf(format string, args ...any) {
	f.logs = append(f.logs, fmt.Sprintf(format, args...))
}

var liveStock = func() map[int]int { return map[int]int{questplan.ObjBones: 17154, questplan.ObjBigBones: 1488} }

func TestAnAccountShortOfPrayer31AsksTheMuleForBigBonesAndBuriesThemAtTheBank(t *testing.T) {
	f := newFakeBones(3100)
	f.hint, f.hintOK = map[int]int{995: 5000}, true // read in an earlier run: no bones
	f.mule, f.muleKnown = liveStock(), true
	got := runPrayerBones(f, true)
	if f.Need() != 0 {
		t.Fatalf("%d xp still to go; calls %v; log %v", f.Need(), f.calls, f.logs)
	}
	if want := []string{"ask", "bury", "bank"}; !slices.Equal(f.calls, want) {
		t.Errorf("calls %v, want %v: the ask, the pack's share buried where it stands, the rest drawn from the bank", f.calls, want)
	}
	if len(f.asked) != 1 || len(f.asked[0]) != 1 || f.asked[0][0].ID != questplan.ObjBigBones || f.asked[0][0].Count != 207 {
		t.Errorf("the ask was %+v, want 207 Big bones", f.asked)
	}
	if got.Mule < 3100 || got.Bank != 0 || got.ToCows || got.Stopped || got.Asks != 1 {
		t.Errorf("the experience came from %+v, want all of it from the mule", got)
	}
}

func TestAnUnreadBankIsReadBeforeTheMuleIsAsked(t *testing.T) {
	f := newFakeBones(3100)
	f.mule, f.muleKnown = liveStock(), true
	runPrayerBones(f, true)
	if len(f.calls) < 2 || f.calls[0] != "bank" || f.calls[1] != "ask" {
		t.Errorf("calls %v: the own bank is read first, then the mule is asked", f.calls)
	}
	if f.Need() != 0 {
		t.Errorf("%d xp still to go", f.Need())
	}
}

func TestTheOwnBanksBonesAreBuriedWithoutAskingTheMule(t *testing.T) {
	f := newFakeBones(2000)
	f.bank[questplan.ObjBones] = 233 // the biggest banked stack on 16 Oct was 44; this one pays for 1,048 xp
	f.bank[questplan.ObjBigBones] = 100
	f.hint, f.hintOK = map[int]int{questplan.ObjBones: 233, questplan.ObjBigBones: 100}, true
	f.mule, f.muleKnown = liveStock(), true
	got := runPrayerBones(f, true)
	if f.Need() != 0 || len(f.asked) != 0 {
		t.Fatalf("%d xp to go, asked %v, calls %v", f.Need(), f.asked, f.calls)
	}
	if got.Bank < 2000 || got.Mule != 0 {
		t.Errorf("tally %+v, want the bank's", got)
	}
}

func TestWhenTheBankFallsShortTheMuleIsAskedForTheRest(t *testing.T) {
	f := newFakeBones(3000)
	f.bank[questplan.ObjBones] = 44 // 198 xp
	f.hint, f.hintOK = map[int]int{questplan.ObjBones: 44}, true
	f.mule, f.muleKnown = liveStock(), true
	got := runPrayerBones(f, true)
	if f.Need() != 0 {
		t.Fatalf("%d xp to go; calls %v", f.Need(), f.calls)
	}
	if len(f.asked) != 1 || f.asked[0][0].Count != 187 { // (3000-198) / 15 = 186.8
		t.Errorf("the ask was %+v, want 187 Big bones for the 2,802 xp the bank did not cover", f.asked)
	}
	if got.Bank == 0 || got.Mule == 0 {
		t.Errorf("tally %+v, want both sources", got)
	}
}

func TestACowsRungOnlyLeavesForTheCowsWhenNoBoneIsToBeHad(t *testing.T) {
	f := newFakeBones(3100)
	f.hint, f.hintOK = map[int]int{}, true
	f.mule, f.muleKnown = map[int]int{}, true // the mule publishes no bones
	got := runPrayerBones(f, true)
	if len(f.calls) != 0 {
		t.Errorf("calls %v: a bank known to hold no bones is not walked to, and an empty mule is not asked", f.calls)
	}
	if !got.ToCows || got.Stopped || got.Gained() != 0 {
		t.Errorf("tally %+v, want the cows with nothing gained", got)
	}
}

func TestPrayer43NeverAsksTheMule(t *testing.T) {
	f := newFakeBones(16_000)
	f.hint, f.hintOK = map[int]int{}, true
	f.mule, f.muleKnown = liveStock(), true
	runPrayerBones(f, false)
	if len(f.asked) != 0 || slices.Contains(f.calls, "ask") {
		t.Errorf("a rung that leaves the mule's bones to the Queen's Prayer asked it: %v", f.calls)
	}
	if got := runPrayerBones(newFakeBones(16_000), false); !got.ToCows {
		t.Errorf("tally %+v: with no bone of its own it leaves for the cows", got)
	}
}

func TestAnAskThatBringsNothingIsNotRepeated(t *testing.T) {
	f := newFakeBones(3100)
	f.hint, f.hintOK = map[int]int{}, true
	f.mule, f.muleKnown = liveStock(), true
	f.asksNothing = true
	runPrayerBones(f, true)
	if len(f.asked) != 1 {
		t.Errorf("the mule was asked %d times", len(f.asked))
	}
	if want := []string{"ask"}; !slices.Equal(f.calls, want) {
		t.Errorf("calls %v, want %v: no bank trip for an ask that delivered nothing", f.calls, want)
	}
}

func TestABankThatWillNotOpenIsTriedOnceAndTheMuleIsNotAskedToFillIt(t *testing.T) {
	f := newFakeBones(3100)
	f.noBooth = true
	f.mule, f.muleKnown = liveStock(), true
	got := runPrayerBones(f, true)
	if want := []string{"bank"}; !slices.Equal(f.calls, want) || !got.ToCows {
		t.Errorf("calls %v (tally %+v), want %v and the cows: an ask would leave its thousand bones in a bank that will not open", f.calls, got, want)
	}
}

func TestBonesThatWillNotBuryAreCarriedToTheBank(t *testing.T) {
	f := newFakeBones(500)
	f.pack[questplan.ObjBigBones] = 20
	f.hint, f.hintOK = map[int]int{}, true
	f.buryBroken = true // the pack is not visible to the server
	got := runPrayerBones(f, false)
	if want := []string{"bury", "bank"}; !slices.Equal(f.calls, want) || !got.ToCows {
		t.Errorf("calls %v (tally %+v), want %v and then the cows: one failed burial, then the bank", f.calls, got, want)
	}
}

// A bank that holds bones and gives none up (a full pack, a broken booth) is left, not gone round for ever.
func TestABankThatGivesNothingUpIsLeft(t *testing.T) {
	f := newFakeBones(3100)
	f.bank[questplan.ObjBones] = 500
	f.hint, f.hintOK = map[int]int{questplan.ObjBones: 500}, true
	f.drawBroken = true
	got := runPrayerBones(f, false)
	banks := 0
	for _, c := range f.calls {
		if c == "bank" {
			banks++
		}
	}
	if banks != 1 || !got.ToCows {
		t.Errorf("calls %v (tally %+v): the bank is tried once and the cows follow", f.calls, got)
	}
}

func TestTheLoopEndsWithTheRunsTimeAndLeavesNoWalkToTheCowsInTheLastMinutes(t *testing.T) {
	f := newFakeBones(3100)
	f.left = questplan.CowsWindow - time.Second
	f.hint, f.hintOK = map[int]int{}, true
	f.mule, f.muleKnown = liveStock(), true
	if got := runPrayerBones(f, true); len(f.calls) != 0 || !got.Stopped {
		t.Errorf("calls %v with %s left, tally %+v: nothing pays for a mule trip or the cows' walk, and the run should say it is over", f.calls, f.left, got)
	}
	f = newFakeBones(3100)
	f.left = 30 * time.Second
	if got := runPrayerBones(f, true); len(f.calls) != 0 || !got.Stopped {
		t.Errorf("calls %v with 30s left, tally %+v", f.calls, got)
	}
	// A level reached is not a stop.
	f = newFakeBones(0)
	if got := runPrayerBones(f, true); got.Stopped {
		t.Error("a reached level read as a stop")
	}
}

func TestAStoppedBotMakesNoMove(t *testing.T) {
	f := newFakeBones(3100)
	f.stopped = true
	f.mule, f.muleKnown = liveStock(), true
	runPrayerBones(f, true)
	if len(f.calls) != 0 {
		t.Errorf("calls %v after a stop", f.calls)
	}
}

func TestAReachedLevelEndsAtOnce(t *testing.T) {
	f := newFakeBones(0)
	f.pack[questplan.ObjBones] = 3
	runPrayerBones(f, true)
	if len(f.calls) != 0 {
		t.Errorf("calls %v for a level already reached", f.calls)
	}
}

// The mule's rows are keyed by the first un-noted object of a name, and "Bones" names several in content 289: the members'
// variants 280-283 come before the 526 the mule really holds. The stock a kind has is every such object's row.
func TestTheMulesBonesAreCountedUnderEveryObjectOfAName(t *testing.T) {
	ids := func(name string) []int {
		switch name {
		case "Bones":
			return []int{280, 281, 282, 283, 526, 527, 2530, 2531, 3187}
		case "Big bones":
			return []int{532, 533}
		}
		return nil
	}
	isNote := func(id int) bool { return id == 527 || id == 2531 || id == 533 }
	// What donorMuleAsker.Published makes of a mule holding 17,154 Bones and 1,488 Big bones.
	pub := map[int]int{280: 17154, 532: 1488}
	got := boneStock(pub, ids, isNote)
	if got[questplan.ObjBones] != 17154 || got[questplan.ObjBigBones] != 1488 || got[questplan.ObjDragonBones] != 0 {
		t.Errorf("stock %v, want 17,154 Bones and 1,488 Big bones under their own ids", got)
	}
	// A mule with no config to name them by is read by id alone.
	if got := boneStock(map[int]int{526: 40}, nil, nil); got[questplan.ObjBones] != 40 {
		t.Errorf("by id: %v", got)
	}
}

// The real item config (RSB0T_CONFIG_JAG): the table's ids, names, notes and Bury op, and the first-id trap itself.
func TestTheBoneTableMatchesTheItemConfig(t *testing.T) {
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range questplan.BoneKinds {
		o, ok := cfg.Obj(k.ID)
		if !ok || o.Name != k.Name || o.Noted() || o.Stackable {
			t.Errorf("%s: obj %d is %+v", k.Name, k.ID, o)
			continue
		}
		if cachecfg.OpIndex(actions.HeldOps(o.IOps), "Bury") == 0 {
			t.Errorf("%s has no Bury op: %v", k.Name, o.IOps)
		}
		n, ok := cfg.Obj(k.Note)
		if !ok || !n.Noted() || n.CertLink != k.ID {
			t.Errorf("%s: obj %d is not its bank note: %+v", k.Name, k.Note, n)
		}
	}
	// The trap: the first un-noted object named "Bones" is not the 526 the mule holds.
	first := 0
	for _, id := range cfg.ObjIDs("Bones") {
		if o, ok := cfg.Obj(id); ok && !o.Noted() {
			first = id
			break
		}
	}
	if first == questplan.ObjBones {
		t.Skipf("the first un-noted Bones is %d now: the trap boneStock guards is gone", first)
	}
	isNote := func(id int) bool { o, ok := cfg.Obj(id); return ok && o.Noted() }
	got := boneStock(map[int]int{first: 17154}, cfg.ObjIDs, isNote)
	if got[questplan.ObjBones] != 17154 {
		t.Errorf("a mule row under obj %d (the first un-noted Bones) gives Bones stock %d, want 17154", first, got[questplan.ObjBones])
	}
}

// The decision through the real engine: the bones step first, the cows only once it has failed, and never the bones step again
// in that run. (The steps themselves are stubs; their world is the tests above.)
func TestAPrayerRungKillsCowsOnlyAfterTheBonesStepFailsThroughTheEngine(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	d := &questrunner.Deps{Session: sim, Components: actionsim.Components(), Log: func(string) {},
		Delay: func(time.Duration) bool { time.Sleep(15 * time.Millisecond); return true }}
	e := &questrunner.Engine{D: d, Prov: &questrunner.Provisioner{Bank: &questrunner.BankView{}}, Store: questplan.NewStore()}
	q := CowsRung("prayerkq", "prayer", 37)
	var calls []string
	q.Customs = map[string]questrunner.CustomFunc{
		questplan.CustomPrayerBones: func(*questrunner.Engine, questplan.Step) bool { calls = append(calls, "bones"); return false },
		questplan.CustomPrayerCows:  func(*questrunner.Engine, questplan.Step) bool { calls = append(calls, "cows"); return true },
	}
	for name := range q.Customs {
		if _, ok := CowsRung("prayerkq", "prayer", 37).Customs[name]; !ok {
			t.Fatalf("the rung registers no executor for %q", name)
		}
	}
	e.Run(q, 70*time.Millisecond)
	if len(calls) < 2 || calls[0] != "bones" {
		t.Fatalf("steps %v: the bones come first and the cows after them", calls)
	}
	for _, c := range calls[1:] {
		if c != "cows" {
			t.Errorf("steps %v: the bones step ran again after it had failed", calls)
			break
		}
	}
	// The hitpoints rung has no bones step at all.
	for name := range CowsRung("cows20", "hitpoints", 20).Customs {
		if name != "cows-hitpoints" {
			t.Errorf("the Hitpoints rung registers %q", name)
		}
	}
}
