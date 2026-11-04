package randomevents

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
)

// syncSession is a captured sender that two goroutines (the Guardian's and the account's script) can send through.
type syncSession struct {
	state    *gamestate.State
	counters anticheat.Counters
	mu       sync.Mutex
	ops      []byte
}

func newSyncSession(st *gamestate.State) *syncSession { return &syncSession{state: st} }

func (s *syncSession) State() *gamestate.State     { return s.state }
func (s *syncSession) Cheats() *anticheat.Counters { return &s.counters }
func (s *syncSession) Send(build func(*packet.Packet)) error {
	p := packet.NewWriter(256)
	build(p)
	s.mu.Lock()
	s.ops = append(s.ops, p.Data[0])
	s.mu.Unlock()
	return nil
}

func (s *syncSession) count(op byte) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, o := range s.ops {
		if o == op {
			n++
		}
	}
	return n
}

// scriptClicks is what a script that does not ask the Guardian sends: a bank booth, a walk to the next booth, a click on an NPC, a rub.
func scriptClicks(s actions.Session) {
	_ = actions.OpLoc(s, 2, 2585, 3418, 2213)
	_ = actions.Walk(s, []pathfind.NavPoint{{X: 2586, Z: 3418}, {X: 2585, Z: 3422}}, true)
	_ = actions.OpNpc(s, 1, 77)
	_ = actions.OpHeld(s, 4, 1707, 3, 3214)
}

func fromScript(f func()) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); f() }()
	wg.Wait()
}

// stubHandler replaces the guardian's handler for a test: it sends one NPC click of its own, signals entered, and waits for release.
func stubHandler(t *testing.T, entered, release chan struct{}) {
	t.Helper()
	prev := guardianHandler
	guardianHandler = func(s actions.Session, _ *pathfind.Pack, _ *Event, _ int, _ func(string)) error {
		_ = actions.OpNpc(s, 1, 1)
		close(entered)
		<-release
		return nil
	}
	t.Cleanup(func() { guardianHandler = prev })
}

// The 18 Oct 2026 plant death: the Guardian handled the plant while the fisher's bank code clicked a booth, walked to the next
// booth and clicked again over the top of it. While a handler runs, the account's script's clicks are dropped and the handler's
// own go out; for an event that can hurt, the hold outlasts the handler by the retry gap; a goroutine that holds the action
// lock (Lock, EmergencyFlee) is the script that has agreed to the protocol and is not held.
func TestGuardianHoldsTheAccountsInputWhileItHandlesAPlant(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Strange plant"}})
	s := newSyncSession(overlapState())
	playSound(s.state, gamestate.SoundTriffidAppear)
	addNPCFacing(s.state, 1, 500, 1, 0, -1)
	g := NewGuardian(s, nil)
	defer g.Stop()
	var logMu sync.Mutex
	var lines []string
	g.Log = func(m string) { logMu.Lock(); lines = append(lines, m); logMu.Unlock() }
	entered, release := make(chan struct{}), make(chan struct{})
	stubHandler(t, entered, release)

	done := make(chan struct{})
	go func() { g.kick(); close(done) }()
	<-entered

	fromScript(func() { scriptClicks(s) })
	if s.count(clientprot.OPLOC2) != 0 || s.count(clientprot.MOVE_GAMECLICK) != 0 || s.count(clientprot.OPHELD4) != 0 {
		t.Fatalf("the script's clicks went out while the Guardian was handling the plant: %v", s.ops)
	}
	if s.count(clientprot.OPNPC1) != 1 {
		t.Fatalf("the handler's own click (and only it) must go out; ops %v", s.ops)
	}
	close(release)
	<-done
	logMu.Lock()
	joined := strings.Join(lines, "\n")
	logMu.Unlock()
	if !strings.Contains(joined, "held off 4 click(s) the account's script sent") {
		t.Fatalf("the hold's work is not in the log:\n%s", joined)
	}

	// The plant is still there: the retry gap is held too, so a click between two tries cannot undo the pick.
	fromScript(func() { scriptClicks(s) })
	if s.count(clientprot.OPLOC2) != 0 || s.count(clientprot.MOVE_GAMECLICK) != 0 {
		t.Fatalf("the script clicked in the gap between two tries at the plant: %v", s.ops)
	}
	// A goroutine that holds the action lock has agreed to the protocol (EmergencyFlee takes it, so does a bank sequence).
	fromScript(func() {
		unlock := g.Lock()
		defer unlock()
		scriptClicks(s)
	})
	if s.count(clientprot.OPLOC2) != 1 || s.count(clientprot.MOVE_GAMECLICK) != 1 {
		t.Fatalf("the goroutine holding the action lock was held off: %v", s.ops)
	}
	// And the hold ends: with the tail run out the script is let go.
	g.holdTailUntil.Store(0)
	fromScript(func() { scriptClicks(s) })
	if s.count(clientprot.OPLOC2) != 2 {
		t.Fatalf("the hold outlived its tail: %v", s.ops)
	}
}

// A harmless event (a Genie) gives the account back the moment its handler ends: only an event that can hurt is held past it.
func TestGuardianLetsTheScriptGoWhenAHarmlessHandlerEnds(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie"}})
	s := newSyncSession(overlapState())
	heardOwnGenie(s.state)
	addNPCFacing(s.state, 1, 500, 1, 0, -1)
	g := NewGuardian(s, nil)
	defer g.Stop()
	entered, release := make(chan struct{}), make(chan struct{})
	stubHandler(t, entered, release)
	done := make(chan struct{})
	go func() { g.kick(); close(done) }()
	<-entered
	fromScript(func() { scriptClicks(s) })
	if s.count(clientprot.OPLOC2) != 0 {
		t.Fatal("the script clicked while the Genie was being handled")
	}
	close(release)
	<-done
	fromScript(func() { scriptClicks(s) })
	if s.count(clientprot.OPLOC2) != 1 || s.count(clientprot.MOVE_GAMECLICK) != 1 {
		t.Fatalf("a harmless event kept the script held past its handler: %v", s.ops)
	}
}

// Busy follows the plant's index, not its type, through the dangerous stretch of its life: the type change to the angry plant is
// not a gap in which a script is let go (the booth click two seconds before the evade began), and an engaged plant that is gone
// or long past its turn holds nothing.
func TestBusyFollowsAnEngagedPlantAcrossItsTypeChange(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{407: "Strange plant", 408: "Strange plant"}})
	s := newSyncSession(overlapState())
	addNPC(s.state, 10015, 407, 0, 1)
	g := NewGuardian(s, nil)
	defer g.Stop()
	g.plants.engage(10015)
	noLag := &gamestate.ViewLag{}
	age := func(n int) { // set the plant's age to n ticks
		advanceTicks(s.state, n-plantAgeOf(t, s.state, 10015))
		s.state.OverrideViewLag(noLag)
	}
	age(plantBusyFromTicks - 1)
	if g.plantBusy() {
		t.Fatal("the plant holds the script before it is nearly pickable")
	}
	age(plantBusyFromTicks)
	if !g.plantBusy() || !g.Busy() {
		t.Fatal("the plant must hold the script from nine ticks before it can be picked")
	}
	age(plantTurnTicks)
	addNPC(s.state, 10015, 408, 0, 1) // npc_changetype_keepall: the same index, the same tile, the angry type
	if plantAgeOf(t, s.state, 10015) != plantTurnTicks {
		t.Fatalf("the type change reset the plant's age to %d", plantAgeOf(t, s.state, 10015))
	}
	if !g.plantBusy() || !g.Busy() {
		t.Fatal("the angry plant must keep holding the script")
	}
	age(plantClearTicks)
	if g.plantBusy() {
		t.Fatal("a plant past plantClearTicks still holds the script")
	}
	if len(g.plants.indexes()) != 0 {
		t.Fatal("a plant past plantClearTicks was not forgotten")
	}
}

func plantAgeOf(t *testing.T, st *gamestate.State, index int) int {
	t.Helper()
	for _, n := range st.NPCs() {
		if n.Index == index {
			return n.VisibleTicks
		}
	}
	t.Fatalf("npc %d not in view", index)
	return 0
}

// A script that holds the action lock itself is never told to wait on the Guardian: Busy is true while it holds the lock (inFlight),
// and waiting on that from inside it would never end. BankFish holds the lock across its booth open.
func TestBusyForCallerIsFalseForTheGoroutineHoldingTheLock(t *testing.T) {
	s := newSyncSession(overlapState())
	g := NewGuardian(s, nil)
	defer g.Stop()
	fromScript(func() {
		unlock := g.Lock()
		defer unlock()
		if !Busy(s) {
			t.Error("control: Busy is true inside the lock")
		}
		if BusyForCaller(s) {
			t.Error("BusyForCaller must be false for the goroutine that holds the lock")
		}
	})
	if BusyForCaller(s) {
		t.Error("nothing is going on")
	}
	locked, free := make(chan struct{}), make(chan struct{})
	go func() {
		unlock := g.Lock()
		close(locked)
		<-free
		unlock()
	}()
	<-locked
	if !BusyForCaller(s) {
		t.Error("another goroutine's view of a held lock is Busy")
	}
	close(free)
}

// The hold is the Guardian's only while it works: a script that never sees the Guardian at all (no event) is not held.
func TestGuardianHoldsNothingWithoutAnEvent(t *testing.T) {
	s := newSyncSession(overlapState())
	g := NewGuardian(s, nil)
	defer g.Stop()
	g.kick()
	fromScript(func() { scriptClicks(s) })
	if s.count(clientprot.OPLOC2) != 1 || s.count(clientprot.OPNPC1) != 1 {
		t.Fatalf("an idle Guardian held the script: %v", s.ops)
	}
	if g.inputHeld() {
		t.Fatal("inputHeld with nothing to handle")
	}
}

// Source pins: the clicks the fix is about stay gated where they are sent, and the Guardian takes the hold around its handlers.
func TestInputHoldIsWiredIntoTheSendersAndTheGuardian(t *testing.T) {
	read := func(path string) string { return foldCRLF(mustReadFile(t, path)) }
	guardian := read("guardian.go")
	for _, want := range []string{"defer g.takeInput()()", "g.plantBusy()", "g.holdPastHandler(ev)", "g.lockHolder.Store(actions.GoroutineID())"} {
		if !strings.Contains(guardian, want) {
			t.Errorf("guardian.go lost %q", want)
		}
	}
	for file, want := range map[string]string{
		"../actions/npc.go":        "interactionHeld(s)",
		"../actions/grounditem.go": "interactionHeld(s)",
		"../actions/equipment.go":  "interactionHeld(s)",
		"../actions/useon.go":      "interactionHeld(s)",
		"../actions/loc.go":        "interactionHeld(s)",
	} {
		if !strings.Contains(read(file), want) {
			t.Errorf("%s no longer drops a held click", file)
		}
	}
}

// mustReadFile reads a source file for a pin, failing the test if it is unreadable.
func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

// foldCRLF makes a working-tree file (CRLF on this machine) read like the repository's (LF), so a pin matches on either.
func foldCRLF(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// A handler that never returns does not freeze the account's script for good: the hold has a safety valve.
func TestInputHoldHasASafetyValve(t *testing.T) {
	s := newSyncSession(overlapState())
	g := NewGuardian(s, nil)
	defer g.Stop()
	release := g.takeInput()
	defer release()
	if !g.inputHeld() {
		t.Fatal("not held while a handler runs")
	}
	g.handlingSince.Store(time.Now().Add(-inputHoldMax - time.Second).UnixNano())
	if g.inputHeld() {
		t.Fatal("a handler older than inputHoldMax still holds the account")
	}
	fromScript(func() { scriptClicks(s) })
	if s.count(clientprot.OPLOC2) != 1 {
		t.Fatalf("the script was still held past the valve: %v", s.ops)
	}
}
