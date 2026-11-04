package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/essencerunner"
)

// The EssenceMiner derives its whole stage from HeldNames, whose WORN half goes through sidebar
// tab 4. Content nulls that tab for the length of a macro event, and a wielded pickaxe then
// reads as missing: ResolvePick never returns PickHeld and DecideStage answers StagePickaxe,
// sending the miner to the Varrock East bank for a pickaxe it is already holding. containersBlind
// is what stops a pass being decided on that reading, so these tests drive it through the real
// essencerunner.ContainersReadable rather than a stand-in.

type essenceFakeSession struct {
	state  *gamestate.State
	cheats *anticheat.Counters
}

func newEssenceFakeSession() *essenceFakeSession {
	return &essenceFakeSession{state: gamestate.New(), cheats: &anticheat.Counters{}}
}

func (f *essenceFakeSession) Send(build func(w *packet.Packet)) error { return nil }
func (f *essenceFakeSession) State() *gamestate.State                 { return f.state }
func (f *essenceFakeSession) Cheats() *anticheat.Counters             { return f.cheats }

// essenceBlindableComponents is the archive shape Equipment walks live: each sidebar tab's root
// interface wrapping the one TYPE_INV component that actually holds the items.
func essenceBlindableComponents() archive.Components {
	return archive.Components{
		3213: {ID: 3213, Type: archive.TypeLayer, Children: []int{3214}},
		3214: {ID: 3214, Type: archive.TypeInv, ObjOps: true},
		1644: {ID: 1644, Type: archive.TypeLayer, Children: []int{1688}},
		1688: {ID: 1688, Type: archive.TypeInv},
	}
}

// essenceSetIcon is IF_SETICON, what content's if_settab sends. A root of 65535 is the null the
// server writes for if_settab(null, tab); gamestate stores it as -1.
func essenceSetIcon(s *gamestate.State, tab, root int) {
	p := packet.NewWriter(8)
	p.P2(uint16(root))
	p.P1(byte(tab))
	s.Handle(serverprot.IF_SETICON, p.Data[:p.Pos])
}

const essenceNullTab = 65535

// essenceBlindBot builds a miner and the deps behind it with the backpack tab assigned pack and
// the worn tab assigned worn, either of which may be essenceNullTab. warnedAt is seeded because
// OnStart normally does it and containersBlind warns.
func essenceBlindBot(pack, worn int) (*essenceBot, *essencerunner.Deps) {
	f := newEssenceFakeSession()
	essenceSetIcon(f.state, 3, pack)
	essenceSetIcon(f.state, 4, worn)
	b := &essenceBot{warnedAt: map[string]time.Time{}}
	return b, &essencerunner.Deps{Session: f, Components: essenceBlindableComponents()}
}

func TestEssenceSkipsThePassWhileTheWornPanelIsNulled(t *testing.T) {
	b, d := essenceBlindBot(3213, essenceNullTab)
	if !b.containersBlind(&Context{}, d) {
		t.Fatal("a nulled worn tab must skip the pass: a wielded pickaxe reads as missing, and DecideStage then answers StagePickaxe and takes a bank trip for nothing")
	}
	if b.blindSince.IsZero() {
		t.Error("the blind window must start timing on the first unreadable pass")
	}
}

func TestEssenceSkipsThePassWhileTheBackpackPanelIsNulled(t *testing.T) {
	b, d := essenceBlindBot(essenceNullTab, 1644)
	if !b.containersBlind(&Context{}, d) {
		t.Fatal("a nulled backpack tab must skip the pass too")
	}
}

func TestEssenceRunsThePassWhenBothPanelsRead(t *testing.T) {
	b, d := essenceBlindBot(3213, 1644)
	if b.containersBlind(&Context{}, d) {
		t.Fatal("both panels readable must run the pass, not skip it")
	}
	if !b.blindSince.IsZero() {
		t.Error("a readable pass must leave the blind window clear")
	}
}

// The degrade half, and the reason this is not woodcutrunner's hard gate: past the limit the
// miner proceeds on the blind read and takes the wasted trip, which the next readable pass
// corrects, rather than looping forever making no progress.
func TestEssenceDegradesRatherThanStallingPastTheLimit(t *testing.T) {
	b, d := essenceBlindBot(essenceNullTab, essenceNullTab)
	if !b.containersBlind(&Context{}, d) {
		t.Fatal("still inside the limit, a blind pass must be skipped")
	}
	b.blindSince = time.Now().Add(-essenceBlindLimit - time.Second)
	if b.containersBlind(&Context{}, d) {
		t.Fatal("past essenceBlindLimit the pass must run anyway: a skip that never gives up is the zero-progress loop this script has already produced five times")
	}
}

// A readable pass between two blind ones restarts the clock, so a run blinded twice gets the
// whole wait each time instead of inheriting the first event's elapsed time.
func TestEssenceBlindWindowRestartsAfterAReadablePass(t *testing.T) {
	b, blind := essenceBlindBot(essenceNullTab, essenceNullTab)
	if !b.containersBlind(&Context{}, blind) {
		t.Fatal("expected the first blind pass to be skipped")
	}
	if b.blindSince.IsZero() {
		t.Fatal("expected the blind window to be timing")
	}
	_, readable := essenceBlindBot(3213, 1644)
	if b.containersBlind(&Context{}, readable) {
		t.Fatal("a readable pass must run")
	}
	if !b.blindSince.IsZero() {
		t.Fatal("a readable pass must clear the blind window, so a later event gets the full wait")
	}
}
