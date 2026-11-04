package questrunner

// The quest engine and the account's run staller (rubwalk-1006): the walks it makes carry the Walker's Prepare, its
// bank deposits leave Deps.KeepInPack in the pack, and Deps.AtBank runs after them.

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func stallConfig(t *testing.T) *cachecfg.Config {
	t.Helper()
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func stallObj(t *testing.T, cfg *cachecfg.Config, name string) int32 {
	t.Helper()
	for _, id := range cfg.ObjIDs(name) {
		if o, ok := cfg.Obj(id); ok && !o.Noted() {
			return int32(id)
		}
	}
	t.Fatalf("fixture: no %q in this config", name)
	return 0
}

// stallRig is an account at an open, empty bank holding a Games necklace, a Lobster and a Bronze axe, whose quest Deps
// carry the given hooks.
func stallRig(t *testing.T, cfg *cachecfg.Config, keepInPack func(string) bool, atBank func(*actions.Bank, actions.Waiter)) (d *Deps, bank *actions.Bank, games, lobster, axe int32) {
	t.Helper()
	games, lobster, axe = stallObj(t, cfg, "Games necklace(8)"), stallObj(t, cfg, "Lobster"), stallObj(t, cfg, "Bronze axe")
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: games, Count: 1}, {ID: lobster, Count: 1}, {ID: axe, Count: 1}})
	sim.OpenBank(nil)
	d = &Deps{
		Session: sim, Config: cfg, Components: actionsim.Components(), Log: func(string) {},
		Delay:      func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
		KeepInPack: keepInPack, AtBank: atBank,
	}
	return d, actions.NewBank(sim, d.Components), games, lobster, axe
}

// Every deposit the engine makes, the quest modules' own included (they all go through DepositExcept), leaves what
// Deps.KeepInPack names in the pack, and AtBank runs once after it with the bank still open.
func TestDepositExceptKeepsWhatKeepInPackNamesAndRunsAtBank(t *testing.T) {
	cfg := stallConfig(t)

	// Control: no hooks, a keep function that keeps only the axe: the necklace goes with the lobster.
	d, bank, games, lobster, axe := stallRig(t, cfg, nil, nil)
	DepositExcept(d, bank, func(id int) bool { return id == int(axe) })
	if got := PackCounts(d); got[int(games)] != 0 || got[int(lobster)] != 0 || got[int(axe)] != 1 {
		t.Fatalf("control: the pack holds %v after a deposit that keeps only the axe, want the axe alone", got)
	}

	// KeepInPack names the jewellery: it stays with the axe, the lobster goes, and AtBank ran once, bank open.
	var atBank []bool
	var bankSeen *actions.Bank
	keepInPack := func(name string) bool { return strings.HasPrefix(name, "Games necklace") }
	d, bank, games, lobster, axe = stallRig(t, cfg, keepInPack, func(b *actions.Bank, w actions.Waiter) {
		bankSeen = b
		atBank = append(atBank, b.IsOpen() && w != nil)
	})
	DepositExcept(d, bank, func(id int) bool { return id == int(axe) })
	if got := PackCounts(d); got[int(games)] != 1 || got[int(lobster)] != 0 || got[int(axe)] != 1 {
		t.Errorf("with KeepInPack the pack holds %v after the deposit, want the necklace and the axe", got)
	}
	if len(atBank) != 1 || !atBank[0] || bankSeen != bank {
		t.Errorf("AtBank ran %v time(s) (open with a waiter: %v), want once, with the open bank", len(atBank), atBank)
	}

	// A keep function that keeps everything banks nothing, and a nil keep set still works for the module that has none.
	d, bank, games, lobster, axe = stallRig(t, cfg, keepInPack, nil)
	DepositExcept(d, bank, func(int) bool { return false })
	if got := PackCounts(d); got[int(games)] != 1 || got[int(lobster)] != 0 || got[int(axe)] != 0 {
		t.Errorf("keeping nothing of its own, with KeepInPack: the pack holds %v, want the necklace alone", got)
	}
}

// The engine's own keep function (a deposit step, a provisioning round's depositable count) asks KeepInPack too, so the
// provisioner does not count the jewellery as a slot its deposit would free.
func TestKeepFuncAsksKeepInPack(t *testing.T) {
	cfg := stallConfig(t)
	games, axe := stallObj(t, cfg, "Games necklace(8)"), stallObj(t, cfg, "Bronze axe")
	e := fakeEngine(&logSink{})
	e.D.Config = cfg
	if keep := e.keepFunc(questplan.Step{}); keep(int(games)) || keep(int(axe)) {
		t.Fatal("with no KeepInPack the engine's keep function kept something no step named")
	}
	e.D.KeepInPack = func(name string) bool { return strings.HasPrefix(name, "Games necklace") }
	keep := e.keepFunc(questplan.Step{Keep: []string{"Bronze axe"}})
	if !keep(int(games)) || !keep(int(axe)) {
		t.Errorf("with KeepInPack the engine kept the necklace %v and the step's axe %v, want both", keep(int(games)), keep(int(axe)))
	}
	if other := stallObj(t, cfg, "Lobster"); keep(int(other)) {
		t.Error("the engine kept a Lobster nobody asked it to")
	}
}

// ---- the walks: a Walker with a Prepare runs it for questrunner's own Travel ----

// flatCollision is a walkable rectangle on level 0.
type flatCollision struct{ x0, x1, z0, z1 int }

var flatDX = [8]int{0, 1, 0, -1, 1, 1, -1, -1}
var flatDZ = [8]int{1, 0, -1, 0, 1, -1, -1, 1}

func (c flatCollision) Walkable(x, z, level int) bool {
	return level == 0 && x >= c.x0 && x <= c.x1 && z >= c.z0 && z <= c.z1
}

func (c flatCollision) ExitMask(x, z, level int) byte {
	var m byte
	if !c.Walkable(x, z, level) {
		return 0
	}
	for d := 0; d < 8; d++ {
		if c.Walkable(x+flatDX[d], z+flatDZ[d], level) {
			m |= 1 << uint(d)
		}
	}
	return m
}

func (c flatCollision) WallMask(int, int, int) byte { return 0 }

// stepClient is a webwalk.Client over the flat world: a click sets the route, a tick of waiting walks one tile of it.
type stepClient struct {
	me    webwalk.Point
	path  []webwalk.Point
	now   time.Time
	acc   time.Duration
	walks int
}

func (c *stepClient) Tile() (webwalk.Point, bool) { return c.me, true }

func (c *stepClient) Walk(path []webwalk.Point, run bool) error {
	c.walks++
	c.path = append([]webwalk.Point(nil), path...)
	return nil
}

func (c *stepClient) FindLocs(webwalk.LocQuery) []webwalk.LocMatch { return nil }
func (c *stepClient) OpLoc(op, x, z, id int) error                 { return nil }
func (c *stepClient) CantReachSince(time.Time) bool                { return false }
func (c *stepClient) Now() time.Time                               { return c.now }

func (c *stepClient) Delay(d time.Duration) bool {
	if d <= 0 {
		return true
	}
	c.now = c.now.Add(d)
	c.acc += d
	for c.acc >= 600*time.Millisecond {
		c.acc -= 600 * time.Millisecond
		for len(c.path) > 0 && c.path[0] == c.me {
			c.path = c.path[1:]
		}
		if len(c.path) == 0 {
			continue
		}
		next := c.path[0]
		c.me = webwalk.Point{X: c.me.X + sgn(next.X-c.me.X), Z: c.me.Z + sgn(next.Z-c.me.Z)}
	}
	return true
}

func sgn(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// stallDeps is quest Deps over a Walker on a 90 by 7 flat world with the given Prepare.
func stallDeps(prepare func(*webwalk.WalkOptions) func()) *Deps {
	col := flatCollision{0, 90, 0, 6}
	cl := &stepClient{me: webwalk.Point{X: 2, Z: 3}, now: time.Unix(1000, 0)}
	w := webwalk.NewWalker(webwalk.NewPlanner(col, &webwalk.Graph{}), cl)
	w.Prepare = prepare
	return &Deps{Session: fakeSession{gamestate.New()}, Delay: func(time.Duration) bool { return true }, Log: func(string) {}, Walker: w}
}

// questrunner.Travel and TravelAvoiding walk through the Walker, so the account's Prepare (the run staller) sees every
// quest leg with what the quest asked for, and its done function runs as the leg returns.
func TestTravelWalksThroughTheWalkersPrepare(t *testing.T) {
	var seen []webwalk.WalkOptions
	polls, dones := 0, 0
	d := stallDeps(func(o *webwalk.WalkOptions) func() {
		seen = append(seen, *o)
		o.Aside = func(webwalk.AsidePoll) { polls++ }
		return func() { dones++ }
	})
	if !Travel(d, tile.Tile{X: 80, Z: 3}, 2) {
		t.Fatal("the quest leg did not arrive")
	}
	if len(seen) != 1 || seen[0].Radius != 2 || seen[0].Timeout != TravelTimeout || !seen[0].Combat.RunWhenAttacked || seen[0].Aside != nil {
		t.Fatalf("Prepare saw %+v, want one walk with the quest's radius 2, the travel budget, run-when-attacked and no Aside of its own", seen)
	}
	if polls == 0 || dones != 1 {
		t.Errorf("the Aside was polled %d time(s) and done ran %d time(s) for one leg, want some and one", polls, dones)
	}

	// The leg that keeps out of rectangles is a walk of the same Walker.
	d = stallDeps(func(o *webwalk.WalkOptions) func() {
		seen = append(seen[:0], *o)
		o.Aside = func(webwalk.AsidePoll) { polls++ }
		return func() { dones++ }
	})
	polls, dones = 0, 0
	avoid := []webwalk.Rect{{MinX: 40, MaxX: 41, MinZ: 5, MaxZ: 6}}
	if !TravelAvoiding(d, tile.Tile{X: 80, Z: 3}, 1, avoid) {
		t.Fatal("the avoiding leg did not arrive")
	}
	if len(seen) != 1 || seen[0].Radius != 1 || len(seen[0].AvoidRects) != 1 || seen[0].Aside != nil {
		t.Fatalf("Prepare saw %+v for the avoiding leg, want radius 1, the one rectangle and no Aside of its own", seen)
	}
	if polls == 0 || dones != 1 {
		t.Errorf("avoiding leg: Aside polled %d time(s), done ran %d, want some and one", polls, dones)
	}

	// A Walker with no Prepare walks as ever.
	plain := stallDeps(nil)
	if !Travel(plain, tile.Tile{X: 80, Z: 3}, 2) {
		t.Fatal("a Walker with no Prepare did not arrive")
	}
}
