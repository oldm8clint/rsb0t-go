package live

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/webwalk"
)

type fakeSession struct {
	st    *gamestate.State
	sends int
}

func (f *fakeSession) Send(func(w *packet.Packet)) error { f.sends++; return nil }
func (f *fakeSession) State() *gamestate.State           { return f.st }
func (f *fakeSession) Cheats() *anticheat.Counters       { return nil }

func TestClientOverAFreshSession(t *testing.T) {
	s := &fakeSession{st: gamestate.New()}
	chat := chatlog.New()
	c := NewClient(Deps{Session: s, Chat: chat})
	if _, ok := c.Tile(); ok {
		t.Error("a fresh session has no tile yet")
	}
	if err := c.Walk([]webwalk.Point{{X: 3200, Z: 3200}, {X: 3205, Z: 3200}}, false); err != nil || s.sends != 1 {
		t.Errorf("walk: err %v, sends %d", err, s.sends)
	}
	if err := c.OpLoc(1, 3201, 3201, 1530); err != nil || s.sends != 2 {
		t.Errorf("oploc: err %v, sends %d", err, s.sends)
	}
	mark := time.Now()
	if c.CantReachSince(mark) {
		t.Error("no chat yet")
	}
	chat.Record("I can't reach that!")
	if !c.CantReachSince(mark) {
		t.Error("missed the can't-reach line")
	}
	if !c.Delay(0) {
		t.Error("a nil Delay never stops")
	}
	if got := c.FindLocs(webwalk.LocQuery{Radius: 3, Names: []string{"Door"}}); got != nil {
		t.Errorf("no loc index should find nothing, got %v", got)
	}
	ws := c.WorldState()
	if ws.FreeSlots != 28 || ws.ItemCount("Coins") != 0 || ws.QuestStatus("Cook's Assistant") != webwalk.QuestUnknown {
		t.Errorf("fresh world state = %+v", ws)
	}
	if _, err := NewWalker(Deps{Session: s}); err == nil {
		t.Error("NewWalker without a pack should fail")
	}
}

// TestFindsTheSeersHopsOnRealData needs RSB0T_COLLISION_PACK, with locs.lcli.gz and
// config.jag beside it (as in the shared data directory).
func TestFindsTheSeersHopsOnRealData(t *testing.T) {
	packPath := os.Getenv("RSB0T_COLLISION_PACK")
	if packPath == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	dir := filepath.Dir(packPath)
	ix, err := locindex.Load(filepath.Join(dir, "locs.lcli.gz"))
	if err != nil {
		t.Skipf("no loc index beside the pack: %v", err)
	}
	cfg, err := cachecfg.LoadFile(filepath.Join(dir, "config.jag"))
	if err != nil {
		t.Skipf("no config archive beside the pack: %v", err)
	}
	pk, err := pathfind.Load(packPath)
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSession{st: gamestate.New()}
	c := NewClient(Deps{Session: s, Pack: pk, Locs: ix, Config: cfg})

	doors := c.FindLocs(webwalk.LocQuery{At: webwalk.Point{X: 2716, Z: 3472}, Radius: 1, IDs: []int{1530}, Op: "Open"})
	if len(doors) == 0 || doors[0].X != 2716 || doors[0].Z != 3472 || doors[0].Op == 0 {
		t.Errorf("wheelhouse door = %+v", doors)
	}
	ladders := c.FindLocs(webwalk.LocQuery{At: webwalk.Point{X: 2715, Z: 3470}, Radius: 1, Names: []string{"Ladder"}, Op: "Climb-up"})
	if len(ladders) == 0 || ladders[0].ID != 1747 {
		t.Errorf("wheelhouse ladder = %+v", ladders)
	}

	w1, err := NewWalker(Deps{Session: s, Pack: pk, Locs: ix, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	w2, _ := NewWalker(Deps{Session: &fakeSession{st: gamestate.New()}, Pack: pk})
	if w1.Planner != w2.Planner {
		t.Error("accounts on one pack should share a Planner")
	}
}
