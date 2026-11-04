package essencerunner

import (
	"testing"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
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

// HeldNames feeds keepInPack, which is the keep list a bank trip deposits against, so a caller
// that needs to tell "no pickaxe" from "cannot see" asks this first.
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
// unreadable rather than as an account holding nothing.
func TestContainersReadableAnswersNoWithoutAnInterfaceArchive(t *testing.T) {
	f := newFakeSession()
	setIcon(f.state, 3, 3213)
	setIcon(f.state, 4, 1644)
	if ContainersReadable(&Deps{Session: f}) {
		t.Error("ContainersReadable with no Components = true, want false")
	}
}

// The pack half of HeldNames reads PlayerPack, which if_settab cannot take away — that is what
// keeps a blinded bank trip from depositing the pickaxe. An empty Config knows no item names, so
// this asserts the weaker but load-bearing property that the blinded path returns cleanly.
func TestHeldNamesOnABlindEmptyAccountIsEmptyNotAnError(t *testing.T) {
	f := newFakeSession()
	setIcon(f.state, 3, nullTab)
	setIcon(f.state, 4, nullTab)
	d := &Deps{Session: f, Components: blindableComponents()}
	if got := HeldNames(d); len(got) != 0 {
		t.Errorf("HeldNames on an empty account = %v, want none", got)
	}
}
