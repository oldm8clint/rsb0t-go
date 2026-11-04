package fleetrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/webwalk"
)

// opsSent is how many packets of opcode op the simulated session was sent.
func opsSent(sim *actionsim.Session, op int) int {
	n := 0
	for _, b := range sim.Sent() {
		if len(b) > 0 && b[0] == byte(op) {
			n++
		}
	}
	return n
}

// runningAt is a simulated account standing at 2700,3400 with the run toggle on and full run energy.
func runningAt(sim *actionsim.Session) {
	simPlace(sim.State(), 2700, 3400)
	sim.State().Handle(serverprot.UPDATE_RUNENERGY, []byte{100})
	sim.State().Handle(serverprot.VARP_SMALL, simPayload(func(p *packet.Packet) { p.P2(uint16(gamestate.VarpRun)); p.P1(1) }))
}

// A far poll of a long walk: the click went out two seconds ago and its target is 40 tiles ahead.
var farPoll = webwalk.AsidePoll{Me: webwalk.Point{X: 2700, Z: 3400}, Target: webwalk.Point{X: 2740, Z: 3400}, SinceClick: 2 * time.Second}

// WalkPrepare gives a walk whose caller brought no Aside the run staller, from the same functions WalkTo uses: the
// aside (a rub here), the far clicks, and the rub's menu closed as the walk returns. A caller's own Aside and
// ClickSteps are the caller's.
func TestWalkPrepareGivesAWalkTheStallerAndClosesTheRubMenuAfterIt(t *testing.T) {
	cfg := travelTestConfig(t)
	necklace := objOf(t, cfg, "Games necklace(8)", false)
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: necklace, Count: 1}})
	runningAt(sim)
	now := time.Unix(1_000_000, 0)
	d := &Deps{Session: sim, Config: cfg, Log: func(string) {}, Delay: func(time.Duration) bool { return true }}
	tr := &Travel{Rub: true, Now: func() time.Time { return now }}
	prepare := tr.WalkPrepare(func() *Deps { return d })
	if prepare == nil {
		t.Fatal("a Travel that rubs gave no Prepare")
	}

	o := webwalk.WalkOptions{Radius: 3}
	done := prepare(&o)
	if o.Aside == nil || done == nil {
		t.Fatalf("a walk with no Aside got Aside %v, done %v", o.Aside != nil, done != nil)
	}
	if o.ClickSteps != fleetplan.WalkAlchClickSteps {
		t.Errorf("ClickSteps = %d for a walk that will rub, want %d", o.ClickSteps, fleetplan.WalkAlchClickSteps)
	}
	if o.Radius != 3 {
		t.Errorf("Radius = %d: Prepare changed what the caller asked for", o.Radius)
	}
	o.Aside(farPoll)
	if opsSent(sim, clientprot.OPHELD4) != 1 || tr.Rubs() != 1 {
		t.Fatalf("the Prepare's Aside sent %d rub(s), the Travel counts %d, want 1 and 1", opsSent(sim, clientprot.OPHELD4), tr.Rubs())
	}
	if opsSent(sim, clientprot.CLOSE_MODAL) != 0 {
		t.Fatal("the rub's menu was closed while the walk was still going")
	}
	// The rub's p_delay(1) makes the engine drop an eat read inside it: the rub notes the delay, and a bite waits for it.
	if left := actions.DelayLeft(sim, time.Now()); left <= 0 || left > rubHeldTicks*600*time.Millisecond {
		t.Errorf("the rub noted a delay of %v, want up to %d ticks", left, rubHeldTicks)
	}
	done()
	if n := opsSent(sim, clientprot.CLOSE_MODAL); n != 1 {
		t.Errorf("%d menu close(s) as the walk returned, want 1", n)
	}
	done() // the walk is over: nothing left open, nothing more sent
	if n := opsSent(sim, clientprot.CLOSE_MODAL); n != 1 {
		t.Errorf("a second done sent %d menu closes in all, want 1", n)
	}

	// A caller with an Aside of its own keeps it, and Prepare leaves the walk alone.
	own := func(webwalk.AsidePoll) {}
	o = webwalk.WalkOptions{Aside: own, ClickSteps: 60}
	if done := prepare(&o); done != nil || o.Aside == nil || o.ClickSteps != 60 {
		t.Errorf("a walk with its own Aside was changed: done %v, Aside %v, ClickSteps %d", done != nil, o.Aside != nil, o.ClickSteps)
	}
	// Far clicks the caller chose stay; the Aside is still given.
	o = webwalk.WalkOptions{ClickSteps: 60}
	if done := prepare(&o); done == nil || o.Aside == nil || o.ClickSteps != 60 {
		t.Errorf("a walk with ClickSteps 60 and no Aside: done %v, Aside %v, ClickSteps %d, want the Aside and 60", done != nil, o.Aside != nil, o.ClickSteps)
	}
}

// A walk the account could not stall (no rub item, nothing to cast) still gets the aside, which does nothing, but aims
// its clicks as the walker always did; and a Travel with nothing to stall gives no Prepare at all.
func TestWalkPrepareAimsFarOnlyForAWalkTheAccountCanStall(t *testing.T) {
	cfg := travelTestConfig(t)
	sim := actionsim.New(actionsim.Objs{}, nil)
	runningAt(sim)
	d := &Deps{Session: sim, Config: cfg, Log: func(string) {}, Delay: func(time.Duration) bool { return true }}
	tr := &Travel{Rub: true}
	o := webwalk.WalkOptions{}
	if done := tr.WalkPrepare(func() *Deps { return d })(&o); done == nil || o.Aside == nil {
		t.Fatalf("done %v, Aside %v: want both for a rubbing Travel", done != nil, o.Aside != nil)
	}
	if o.ClickSteps != 0 {
		t.Errorf("ClickSteps = %d for an account with no jewellery and nothing to cast, want the default 0", o.ClickSteps)
	}

	var nilTravel *Travel
	if nilTravel.WalkPrepare(func() *Deps { return d }) != nil {
		t.Error("a nil Travel gave a Prepare")
	}
	if (&Travel{}).WalkPrepare(func() *Deps { return d }) != nil {
		t.Error("a Travel with walk alch and the rub off gave a Prepare")
	}
	if (&Travel{Tele: fleetplan.TeleTravelSettings{Enabled: true}}).WalkPrepare(func() *Deps { return d }) != nil {
		t.Error("teleport travel alone gave a Prepare: it stalls nothing")
	}
	if tr.WalkPrepare(nil) != nil {
		t.Error("no Deps provider gave a Prepare")
	}
	if prep := tr.WalkPrepare(func() *Deps { return nil }); prep == nil {
		t.Fatal("a provider that may answer nil gave no Prepare")
	} else if o := (webwalk.WalkOptions{}); prep(&o) != nil || o.Aside != nil {
		t.Error("a nil Deps at walk time still gave the walk an Aside")
	}
}

// DrawRubItem draws exactly ONE Games necklace or Ring of dueling, as an item and not a note, when the pack holds none
// and keeps two slots free; it draws nothing with one carried, with the pack too full, with the rub off, or on a nil
// Travel.
func TestDrawRubItemDrawsExactlyOneWhenThePackHoldsNone(t *testing.T) {
	cfg := travelTestConfig(t)
	games := objOf(t, cfg, "Games necklace(8)", false)
	gamesNote := objOf(t, cfg, "Games necklace(8)", true)
	ring := objOf(t, cfg, "Ring of dueling(8)", false)
	lobster := objOf(t, cfg, "Lobster", false)
	bank := []actionsim.Stack{{ID: games, Count: 5}, {ID: ring, Count: 3}}
	objs := actionsim.Objs{NoteOf: func(id int32) int32 {
		if id == games {
			return gamesNote
		}
		return 0
	}}
	held := func(d *Deps) int {
		return CountPackNamed(d, "Games necklace(8)") + CountPackNamed(d, "Ring of dueling(8)")
	}
	run := func(pack []actionsim.Stack, tr *Travel, notes bool) (*Deps, *actions.Bank, *actionsim.Session) {
		sim := actionsim.New(objs, pack)
		sim.OpenBank(bank)
		d := quickDeps(sim, cfg)
		b := actions.NewBank(sim, d.Components)
		if notes {
			if err := b.SetNoteMode(true, nil); err != nil || !b.NoteMode() {
				t.Fatalf("fixture: could not turn note mode on: %v", err)
			}
		}
		tr.DrawRubItem(d, b, nil)
		return d, b, sim
	}

	// Nothing carried: one necklace, however many the bank holds.
	d, _, sim := run(nil, &Travel{Rub: true}, false)
	if n := held(d); n != 1 || CountPackNamed(d, "Games necklace(8)") != 1 {
		t.Fatalf("drew %d piece(s) of jewellery, want one Games necklace", n)
	}
	if left := stackCount(sim.Bank(), games); left != 4 {
		t.Errorf("the bank holds %d Games necklaces after the draw, want 4", left)
	}
	// A second visit with the necklace in the pack draws nothing more.
	d2, _, sim2 := run([]actionsim.Stack{{ID: games, Count: 1}}, &Travel{Rub: true}, false)
	if n := held(d2); n != 1 || stackCount(sim2.Bank(), games) != 5 {
		t.Errorf("a pack that held a necklace holds %d piece(s) after the visit and the bank %d, want 1 and 5", n, stackCount(sim2.Bank(), games))
	}
	// A Ring of dueling is as good as a necklace: carried, nothing is drawn.
	d3, _, _ := run([]actionsim.Stack{{ID: ring, Count: 1}}, &Travel{Rub: true}, false)
	if n := CountPackNamed(d3, "Games necklace(8)"); n != 0 {
		t.Errorf("drew %d Games necklace(s) for an account carrying a Ring of dueling", n)
	}
	// Items, never notes: a bank left in note mode would hand over a note, which has no Rub option.
	d4, b4, _ := run(nil, &Travel{Rub: true}, true)
	if CountPackUnnoted(d4, "Games necklace(8)") != 1 || CountPackNamed(d4, "Games necklace(8)") != 1 || b4.NoteMode() {
		t.Errorf("a draw from a bank in note mode left %d necklace(s), %d of them items, note mode %v: want one item and note mode off",
			CountPackNamed(d4, "Games necklace(8)"), CountPackUnnoted(d4, "Games necklace(8)"), b4.NoteMode())
	}
	// The pack keeps two slots free: 25 used leaves room for the draw, 26 does not.
	slots := func(n int) []actionsim.Stack {
		var out []actionsim.Stack
		for i := 0; i < n; i++ {
			out = append(out, actionsim.Stack{ID: lobster, Count: 1})
		}
		return out
	}
	if d5, _, _ := run(slots(25), &Travel{Rub: true}, false); held(d5) != 1 {
		t.Errorf("25 slots used: drew %d, want 1 (three free, two left after)", held(d5))
	}
	if d6, _, _ := run(slots(26), &Travel{Rub: true}, false); held(d6) != 0 {
		t.Errorf("26 slots used: drew %d, want none (it would leave one slot free)", held(d6))
	}
	// Off, or no Travel at all: nothing.
	if d7, _, _ := run(nil, &Travel{}, false); held(d7) != 0 {
		t.Errorf("walkRub off: drew %d", held(d7))
	}
	var nilTravel *Travel
	if d8, _, _ := run(nil, nilTravel, false); held(d8) != 0 {
		t.Errorf("a nil Travel drew %d", held(d8))
	}
	// A bank with no jewellery: nothing to draw, and no error.
	sim9 := actionsim.New(objs, nil)
	sim9.OpenBank([]actionsim.Stack{{ID: lobster, Count: 4}})
	d9 := quickDeps(sim9, cfg)
	(&Travel{Rub: true}).DrawRubItem(d9, actions.NewBank(sim9, d9.Components), nil)
	if held(d9) != 0 {
		t.Errorf("drew %d piece(s) from a bank with none", held(d9))
	}
}

// DrawAlchKit still draws the jewellery, now through DrawRubItem: the training kit's bank visit is unchanged.
func TestDrawAlchKitDrawsTheJewelleryThroughDrawRubItem(t *testing.T) {
	cfg := travelTestConfig(t)
	games := objOf(t, cfg, "Games necklace(8)", false)
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.OpenBank([]actionsim.Stack{{ID: games, Count: 2}})
	d := quickDeps(sim, cfg)
	tr := &Travel{Rub: true}
	tr.DrawAlchKit(d, actions.NewBank(sim, d.Components), nil)
	if n := CountPackNamed(d, "Games necklace(8)"); n != 1 {
		t.Errorf("the kit visit drew %d necklace(s), want 1", n)
	}
}

// flatPlane is a walkable rectangle on level 0.
type flatPlane struct{ x0, x1, z0, z1 int }

var planeDX = [8]int{0, 1, 0, -1, 1, 1, -1, -1}

var planeDZ = [8]int{1, 0, -1, 0, 1, -1, -1, 1}

func (c flatPlane) Walkable(x, z, level int) bool {
	return level == 0 && x >= c.x0 && x <= c.x1 && z >= c.z0 && z <= c.z1
}

func (c flatPlane) ExitMask(x, z, level int) byte {
	var m byte
	if !c.Walkable(x, z, level) {
		return 0
	}
	for d := 0; d < 8; d++ {
		if c.Walkable(x+planeDX[d], z+planeDZ[d], level) {
			m |= 1 << uint(d)
		}
	}
	return m
}

func (c flatPlane) WallMask(int, int, int) byte { return 0 }

// tickClient is a webwalk.Client over the flat plane: a click sets the route, and each 600 ms of waiting walks one tile of
// it. Its clock is the one the Travel under test reads.
type tickClient struct {
	me    webwalk.Point
	path  []webwalk.Point
	now   time.Time
	acc   time.Duration
	walks int
}

func (c *tickClient) Tile() (webwalk.Point, bool) { return c.me, true }

func (c *tickClient) Walk(path []webwalk.Point, run bool) error {
	c.walks++
	c.path = append([]webwalk.Point(nil), path...)
	return nil
}

func (c *tickClient) FindLocs(webwalk.LocQuery) []webwalk.LocMatch { return nil }

func (c *tickClient) OpLoc(op, x, z, id int) error { return nil }

func (c *tickClient) CantReachSince(time.Time) bool { return false }

func (c *tickClient) Now() time.Time { return c.now }

func (c *tickClient) Delay(d time.Duration) bool {
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
		step := func(v int) int {
			switch {
			case v > 0:
				return 1
			case v < 0:
				return -1
			}
			return 0
		}
		c.me = webwalk.Point{X: c.me.X + step(next.X-c.me.X), Z: c.me.Z + step(next.Z-c.me.Z)}
	}
	return true
}

// The owner's failure of 6 October: a walk made through the Walker directly (a quest leg, not scarabrunner.WalkTo) with a
// Games necklace in the pack rubbed it never. With the Travel's Prepare installed on the Walker the same walk rubs all the
// way, aims its clicks far (fewer of them), and closes the rub's menu as it returns; without it, nothing changes.
func TestAWalkerWithThePrepareStallsAWalkItsCallerKnowsNothingAbout(t *testing.T) {
	cfg := travelTestConfig(t)
	necklace := objOf(t, cfg, "Games necklace(8)", false)
	run := func(prepared bool) (rubs, clicks, closes int, closedLast bool) {
		sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: necklace, Count: 1}})
		runningAt(sim)
		cl := &tickClient{me: webwalk.Point{X: 2, Z: 3}, now: time.Unix(1_000_000, 0)}
		tr := &Travel{Rub: true, Now: cl.Now}
		d := &Deps{Session: sim, Config: cfg, Log: func(string) {}, Delay: func(time.Duration) bool { return true }}
		w := webwalk.NewWalker(webwalk.NewPlanner(flatPlane{0, 420, 0, 6}, &webwalk.Graph{}), cl)
		if prepared {
			w.Prepare = tr.WalkPrepare(func() *Deps { return d })
		}
		res := w.WalkTo(webwalk.Point{X: 402, Z: 3}, webwalk.WalkOptions{Radius: 1, Timeout: 30 * time.Minute})
		if !res.Arrived() {
			t.Fatalf("prepared=%v: the walk did not arrive: %+v", prepared, res)
		}
		sent := sim.Sent()
		closedLast = len(sent) > 0 && sent[len(sent)-1][0] == byte(clientprot.CLOSE_MODAL)
		return opsSent(sim, clientprot.OPHELD4), cl.walks, opsSent(sim, clientprot.CLOSE_MODAL), closedLast
	}

	rubs, clicks, closes, closedLast := run(true)
	plainRubs, plainClicks, plainCloses, _ := run(false)
	if plainRubs != 0 || plainCloses != 0 {
		t.Errorf("a Walker with no Prepare rubbed %d time(s) and closed %d menu(s)", plainRubs, plainCloses)
	}
	// 400 tiles at a tile a tick: a rub every third tick but for the ticks around each click.
	if rubs < 60 {
		t.Errorf("the staller rubbed %d time(s) over a 400-tile walk, want at least 60", rubs)
	}
	if clicks >= plainClicks || clicks > 6 {
		t.Errorf("%d walker clicks with the staller against %d without: want fewer, and at most 6 (clicks aimed %d tiles ahead)", clicks, plainClicks, fleetplan.WalkAlchClickSteps)
	}
	if closes != 1 || !closedLast {
		t.Errorf("the rub's menu was closed %d time(s), last packet a close %v: want once, as the walk returned", closes, closedLast)
	}
}

// StallMode names how a walk is stalled and why not, for the log line every walk's start makes.
func TestStallModeNamesTheStallOrWhatIsMissing(t *testing.T) {
	cfg := travelTestConfig(t)
	necklace := objOf(t, cfg, "Games necklace(8)", false)
	run := func(items []actionsim.Stack, tr *Travel) (bool, string) {
		sim := actionsim.New(actionsim.Objs{}, items)
		runningAt(sim)
		d := &Deps{Session: sim, Config: cfg, Log: func(string) {}, Delay: func(time.Duration) bool { return true }}
		return tr.StallMode(d)
	}
	if ok, why := run([]actionsim.Stack{{ID: necklace, Count: 1}}, &Travel{Rub: true}); !ok || why != "rub (Games necklace(8))" {
		t.Errorf("a carried necklace: %v %q, want a rub naming it", ok, why)
	}
	if ok, why := run(nil, &Travel{Rub: true}); ok || why != "none (no rub item)" {
		t.Errorf("rub on, nothing carried: %v %q", ok, why)
	}
	if ok, why := run(nil, &Travel{Rub: true, Alch: fleetplan.WalkAlchSettings{Enabled: true}}); ok || !strings.Contains(why, "no rub item") || !strings.Contains(why, "no alch kit") {
		t.Errorf("rub and alch on, neither kit: %v %q", ok, why)
	}
	if ok, why := run([]actionsim.Stack{{ID: necklace, Count: 1}}, &Travel{}); ok || why != "none (walkRub off)" {
		t.Errorf("walkRub off with a necklace: %v %q", ok, why)
	}
	// The log line: once per distinct answer, naming the destination.
	var lines []string
	sim := actionsim.New(actionsim.Objs{}, nil)
	runningAt(sim)
	d := &Deps{Session: sim, Config: cfg, Log: func(s string) { lines = append(lines, s) }, Delay: func(time.Duration) bool { return true }}
	tr := &Travel{Rub: true, Now: func() time.Time { return time.Unix(1_000_000, 0) }}
	prep := tr.WalkPrepare(func() *Deps { return d })
	for i := 0; i < 3; i++ {
		o := webwalk.WalkOptions{Dest: webwalk.Point{X: 2700, Z: 3400}}
		prep(&o)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "run staller: walk to (2700,3400): none (no rub item)") {
		t.Errorf("log lines %q, want one naming the destination and what is missing", lines)
	}
}
