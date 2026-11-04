package woodcutrunner

import (
	"testing"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/tile"
)

type fakeSession struct {
	state  *gamestate.State
	cheats *anticheat.Counters
}

func newFakeSession() *fakeSession {
	return &fakeSession{state: gamestate.New(), cheats: &anticheat.Counters{}}
}

func (f *fakeSession) Send(build func(w *packet.Packet)) error { return nil }
func (f *fakeSession) State() *gamestate.State                 { return f.state }
func (f *fakeSession) Cheats() *anticheat.Counters             { return f.cheats }

// blindableComponents is the archive shape Equipment walks live: each sidebar tab's root
// interface wrapping the one TYPE_INV component that actually holds the items.
func blindableComponents() archive.Components {
	return archive.Components{
		3213: {ID: 3213, Type: archive.TypeLayer, Children: []int{3214}},
		3214: {ID: 3214, Type: archive.TypeInv, ObjOps: true},
		1644: {ID: 1644, Type: archive.TypeLayer, Children: []int{1688}},
		1688: {ID: 1688, Type: archive.TypeInv},
	}
}

// setIcon is IF_SETICON, the packet content's if_settab sends. A root of 65535 is the null the
// server writes for if_settab(null, tab); gamestate stores it as -1.
func setIcon(s *gamestate.State, tab, root int) {
	p := packet.NewWriter(8)
	p.P2(uint16(root))
	p.P1(byte(tab))
	s.Handle(serverprot.IF_SETICON, p.Data[:p.Pos])
}

const nullTab = 65535

// ContainersReadable is what stops a blinded pass from deciding a phase, so it has to answer
// "no" for every way content can take the two panels away, not just the obvious one. Tabs 3 and 4
// are nulled together by ~macro_events_set_tabs and by ~set_sail, but each is asserted on its own
// so a future change that only restores one cannot read as fully recovered.
func TestContainersReadableAnswersNoWhileEitherPanelIsNulled(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(s *gamestate.State)
		want  bool
	}{
		{"both panels assigned", func(s *gamestate.State) {
			setIcon(s, 3, 3213)
			setIcon(s, 4, 1644)
		}, true},
		{"backpack tab nulled", func(s *gamestate.State) {
			setIcon(s, 3, nullTab)
			setIcon(s, 4, 1644)
		}, false},
		{"worn tab nulled", func(s *gamestate.State) {
			setIcon(s, 3, 3213)
			setIcon(s, 4, nullTab)
		}, false},
		{"both tabs nulled, as a macro event or a voyage leaves them", func(s *gamestate.State) {
			setIcon(s, 3, nullTab)
			setIcon(s, 4, nullTab)
		}, false},
		{"no IF_SETICON seen yet", func(s *gamestate.State) {}, false},
	} {
		f := newFakeSession()
		c.setup(f.state)
		d := &Deps{Session: f, Components: blindableComponents()}
		if got := ContainersReadable(d); got != c.want {
			t.Errorf("%s: ContainersReadable = %v, want %v", c.name, got, c.want)
		}
	}
}

// A run with no interface archive at all can never resolve either panel, so it reads as
// unreadable rather than as an account holding nothing. That is the same answer, for the same
// reason, and internal/bot/woodcutter.go's OnStart already refuses to start without Components.
func TestContainersReadableAnswersNoWithoutAnInterfaceArchive(t *testing.T) {
	f := newFakeSession()
	setIcon(f.state, 3, 3213)
	setIcon(f.state, 4, 1644)
	if ContainersReadable(&Deps{Session: f}) {
		t.Error("ContainersReadable with no Components = true, want false")
	}
}

// The pack half of HeldAxes must keep working while the panels are blind: it reads PlayerPack,
// which if_settab cannot take away. An empty Config knows no axe names, so this asserts the
// weaker but still load-bearing property that a blinded read is not an error and does not reach
// for the worn panel.
func TestHeldAxesStillReadsThePackWhileBlind(t *testing.T) {
	f := newFakeSession()
	setIcon(f.state, 3, nullTab)
	setIcon(f.state, 4, nullTab)
	d := &Deps{Session: f, Components: blindableComponents()}
	if got := HeldAxes(d); len(got) != 0 {
		t.Errorf("HeldAxes on an empty pack = %v, want none", got)
	}
}

// TestStandOccupiedIsExactTile pins the rule that decides whether a stand tile counts as
// occupied — which since 2026-09-23 orders it LAST rather than skipping it (freeFirst: players do
// not block players, measured).
//
// Exact-tile on purpose. Being shy of a NEIGHBOUR would rule out most of a dense camp: at the
// Seers normal camp the 12 trees share 18 accepted stand tiles, so a one-tile exclusion zone
// around every visible account would leave a second account almost nowhere to stand, which is
// the opposite of the problem this solves.
func TestStandOccupiedIsExactTile(t *testing.T) {
	at := tile.Tile{X: 2731, Z: 3481}
	players := []gamestate.Player{{Name: "Acct219", X: 2731, Z: 3481, Level: 0}}

	if busy, who := standOccupiedBy(players, at); !busy || who != "Acct219" {
		t.Errorf("standOccupiedBy(%v) = %v, %q - want true, Acct219", at, busy, who)
	}
	// One tile away is not in the way, diagonals included.
	for _, near := range []tile.Tile{
		{X: 2732, Z: 3481}, {X: 2730, Z: 3481}, {X: 2731, Z: 3482}, {X: 2731, Z: 3480},
		{X: 2732, Z: 3482},
	} {
		if busy, _ := standOccupiedBy(players, near); busy {
			t.Errorf("standOccupiedBy(%v) = true - only the exact tile is occupied", near)
		}
	}
	// Another floor is a different tile entirely.
	up := at
	up.Level = 1
	if busy, _ := standOccupiedBy(players, up); busy {
		t.Errorf("standOccupiedBy(%v) = true - that is a different level", up)
	}
	// An empty world is never busy, and neither is a missing session: this runs on every
	// candidate of every chop, so it must not panic on either.
	if busy, _ := standOccupiedBy(nil, at); busy {
		t.Error("an empty player list should never be occupied")
	}
	if busy, _ := standOccupied(&Deps{}, at); busy {
		t.Error("standOccupied with no session should be false, not a panic")
	}
	if busy, _ := standOccupied(nil, at); busy {
		t.Error("standOccupied(nil) should be false, not a panic")
	}
}
