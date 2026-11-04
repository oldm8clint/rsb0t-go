package fleetrunner

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func travelTestConfig(t *testing.T) *cachecfg.Config {
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

// objOf is the first obj named name whose noted-ness is noted (and, among the un-noted, the one
// with the highest cost: the strung bow, not the unstrung).
func objOf(t *testing.T, cfg *cachecfg.Config, name string, noted bool) int32 {
	t.Helper()
	best, bestCost := int32(0), int32(-1)
	for _, id := range cfg.ObjIDs(name) {
		o, ok := cfg.Obj(id)
		if !ok || o.Noted() != noted {
			continue
		}
		if o.Cost > bestCost {
			best, bestCost = int32(id), o.Cost
		}
	}
	if best == 0 {
		t.Fatalf("fixture: no %s (noted %v) in this config", name, noted)
	}
	return best
}

func setStat(sim *actionsim.Session, stat, xp, level int) {
	p := packet.NewWriter(16)
	p.P1(byte(stat))
	p.P4(int32(xp))
	p.P1(byte(level))
	sim.State().Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
}

type opheldt struct{ obj, slot, com, spell int }

// opheldu is an OPHELDU packet's length: the opcode and six u16s.
const opheldu = 13

// castsSent is every OPHELDT the session was sent, whether alone or after the clear-move OPHELDU a cast leaves with.
func castsSent(sim *actionsim.Session) []opheldt {
	var out []opheldt
	for _, b := range sim.Sent() {
		at := 0
		if len(b) >= opheldu && b[0] == byte(clientprot.OPHELDU) {
			at = opheldu
		}
		if len(b) >= at+9 && b[at] == byte(clientprot.OPHELDT) {
			g := func(i int) int { return int(b[at+i])<<8 | int(b[at+i+1]) }
			out = append(out, opheldt{obj: g(1), slot: g(3), com: g(5), spell: g(7)})
		}
	}
	return out
}

// clearsSent is the OPHELDU packets that lead a cast's write: (obj, slot, useObj, useSlot).
func clearsSent(sim *actionsim.Session) [][4]int {
	var out [][4]int
	for _, b := range sim.Sent() {
		if len(b) >= opheldu && b[0] == byte(clientprot.OPHELDU) {
			g := func(i int) int { return int(b[i])<<8 | int(b[i+1]) }
			out = append(out, [4]int{g(1), g(3), g(7), g(9)})
		}
	}
	return out
}

// TestWalkAlchCastsOnTheBowNoteAndRespectsTheWalk drives one account's walk polls against the
// simulated session and the real 289 objs: the cast goes on the allow-listed bow (here a stack of
// notes, one slot), never the food the list does not hold, never in the tick of a movement click,
// never inside the last cast's delay, and never when the walk will click again before the delay
// ends.
func TestWalkAlchCastsOnTheBowNoteAndRespectsTheWalk(t *testing.T) {
	cfg := travelTestConfig(t)
	nature := objOf(t, cfg, "Nature rune", false)
	fire := objOf(t, cfg, "Fire rune", false)
	bowNote := objOf(t, cfg, "Yew shortbow", true)
	lobster := objOf(t, cfg, "Lobster", false)
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{
		{ID: lobster, Count: 1}, {ID: nature, Count: 10}, {ID: fire, Count: 50}, {ID: bowNote, Count: 20},
	})
	setStat(sim, fleetplan.MagicStat, 200000, 60)
	now := time.Unix(1_000_000, 0)
	d := &Deps{Session: sim, Config: cfg, Delay: func(time.Duration) bool { return true }}
	tr := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised(), Now: func() time.Time { return now }}
	poll := webwalk.AsidePoll{Me: webwalk.Point{X: 2700, Z: 3400}, Target: webwalk.Point{X: 2716, Z: 3400}, SinceClick: 2 * time.Second}

	// A click this tick: no cast.
	tr.alchPoll(d, webwalk.AsidePoll{Me: poll.Me, Target: poll.Target, SinceClick: 0})
	// The walk about to click again (target 6 tiles ahead): no cast.
	tr.alchPoll(d, webwalk.AsidePoll{Me: poll.Me, Target: webwalk.Point{X: 2706, Z: 3400}, SinceClick: 2 * time.Second})
	if got := castsSent(sim); len(got) != 0 {
		t.Fatalf("cast %v with a click this tick or the next click inside the delay", got)
	}
	tr.alchPoll(d, poll)
	got := castsSent(sim)
	if len(got) != 1 {
		t.Fatalf("casts sent = %v, want one", got)
	}
	spellCom, _ := actions.ItemSpellComponent("High Level Alchemy")
	if got[0].obj != int(bowNote) || got[0].slot != 3 || got[0].com != PlayerPack || got[0].spell != spellCom {
		t.Fatalf("cast = %+v, want High Level Alchemy (%d) on the Yew shortbow note %d in slot 3 of %d", got[0], spellCom, bowNote, PlayerPack)
	}
	// The server grants the experience; inside the cast's five ticks the poll counts it and sends
	// nothing more.
	setStat(sim, fleetplan.MagicStat, 200650, 60)
	now = now.Add(2900 * time.Millisecond)
	tr.alchPoll(d, poll)
	if n := len(castsSent(sim)); n != 1 {
		t.Fatalf("%d casts inside the delay", n)
	}
	if casts, xp, _, _ := tr.Counters(); casts != 1 || xp != 650 {
		t.Errorf("counters = %d casts, %d xp, want 1 and 650", casts, xp)
	}
	// After the delay the next cast goes.
	now = now.Add(200 * time.Millisecond)
	tr.alchPoll(d, poll)
	if n := len(castsSent(sim)); n != 2 {
		t.Errorf("%d casts after the delay, want 2", n)
	}
}

// TestWalkAlchNeverCastsOnFoodOrBelowTheLevel: with only food and runes in the pack, or at Magic 54,
// no cast goes out.
func TestWalkAlchNeverCastsOnFoodOrBelowTheLevel(t *testing.T) {
	cfg := travelTestConfig(t)
	nature := objOf(t, cfg, "Nature rune", false)
	fire := objOf(t, cfg, "Fire rune", false)
	lobster := objOf(t, cfg, "Lobster", false)
	bow := objOf(t, cfg, "Yew shortbow", false)
	poll := webwalk.AsidePoll{Me: webwalk.Point{X: 2700, Z: 3400}, Target: webwalk.Point{X: 2716, Z: 3400}, SinceClick: 2 * time.Second}

	s := fleetplan.WalkAlchSettings{Enabled: true, Items: []string{"Lobster", "Yew shortbow"}}.Normalised()
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: lobster, Count: 1}, {ID: nature, Count: 10}, {ID: fire, Count: 50}})
	setStat(sim, fleetplan.MagicStat, 200000, 60)
	tr := &Travel{Alch: s}
	tr.alchPoll(&Deps{Session: sim, Config: cfg}, poll)
	if got := castsSent(sim); len(got) != 0 {
		t.Fatalf("cast %v on food", got)
	}

	sim = actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: bow, Count: 1}, {ID: nature, Count: 10}, {ID: fire, Count: 50}})
	setStat(sim, fleetplan.MagicStat, 140000, 54)
	tr = &Travel{Alch: s}
	tr.alchPoll(&Deps{Session: sim, Config: cfg}, poll)
	if got := castsSent(sim); len(got) != 0 {
		t.Fatalf("cast %v at Magic 54", got)
	}
}

// TestNilTravelDoesNothing: an account without the settings walks exactly as before.
func TestNilTravelDoesNothing(t *testing.T) {
	var tr *Travel
	if tr.walkAside(nil) != nil {
		t.Error("a nil Travel gave the walk an Aside")
	}
	if tr.teleportBeforeWalk(nil, tile.Tile{X: 3200, Z: 3400}) {
		t.Error("a nil Travel teleported")
	}
	if tr.TravelKeep() != nil {
		t.Error("a nil Travel kept items")
	}
	off := &Travel{}
	if off.walkAside(nil) != nil || off.teleportBeforeWalk(nil, tile.Tile{X: 3200, Z: 3400}) {
		t.Error("a Travel with both helpers off acted")
	}
}

// TestIdleAlchCastsWhileWaitingAndWaitsOutItsDelay: on a safespot with no target, the account alchs
// the bow note instead of standing idle, never twice inside the cast's delay, and never in the
// Wilderness.
func TestIdleAlchCastsWhileWaitingAndWaitsOutItsDelay(t *testing.T) {
	cfg := travelTestConfig(t)
	nature := objOf(t, cfg, "Nature rune", false)
	fire := objOf(t, cfg, "Fire rune", false)
	bowNote := objOf(t, cfg, "Yew shortbow", true)
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: nature, Count: 10}, {ID: fire, Count: 50}, {ID: bowNote, Count: 20}})
	setStat(sim, fleetplan.MagicStat, 200000, 60)
	simPlace(sim.State(), 2530, 2979) // a Feldip ogres safespot
	now := time.Unix(1_000_000, 0)
	d := &Deps{Session: sim, Config: cfg, Delay: func(time.Duration) bool { return true }}
	tr := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised(), Now: func() time.Time { return now }}
	if !tr.IdleAlch(d) {
		t.Fatal("no cast on the safespot with a bow note, natures and fire runes")
	}
	if got := castsSent(sim); len(got) != 1 || got[0].obj != int(bowNote) {
		t.Fatalf("casts = %+v, want one on the bow note", got)
	}
	if w := tr.IdleAlchWait(); w != 3*time.Second {
		t.Errorf("IdleAlchWait = %v, want the 5 ticks of the cast", w)
	}
	now = now.Add(time.Second)
	if tr.IdleAlch(d) {
		t.Fatal("a second cast inside the delay")
	}
	now = now.Add(3 * time.Second)
	if !tr.IdleAlch(d) {
		t.Fatal("no cast once the delay was over")
	}

	wild := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: nature, Count: 10}, {ID: fire, Count: 50}, {ID: bowNote, Count: 20}})
	setStat(wild, fleetplan.MagicStat, 200000, 60)
	simPlace(wild.State(), 3280, 3938) // Rogues' Castle, deep Wilderness
	tw := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised(), Now: func() time.Time { return now }}
	if tw.IdleAlch(&Deps{Session: wild, Config: cfg, Delay: func(time.Duration) bool { return true }}) {
		t.Fatal("cast in the Wilderness")
	}
	var nilTravel *Travel
	if nilTravel.IdleAlch(d) {
		t.Fatal("a nil Travel cast")
	}
}

// TestAlchKeepsTheWalkAlchKitOnly: a bank deposit leaves natures, fire runes and allow-listed
// alchables in the pack only while walk alch is on and the account has the level -- and, for the alchables, a Nature
// rune in the pack to cast them with (the nature rule: alchnatures_test.go has the pack without one).
func TestAlchKeepsTheWalkAlchKitOnly(t *testing.T) {
	cfg := travelTestConfig(t)
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: objOf(t, cfg, "Nature rune", false), Count: 10}})
	setStat(sim, fleetplan.MagicStat, 200000, 60)
	d := &Deps{Session: sim, Config: cfg}
	tr := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised()}
	for name, want := range map[string]bool{"Nature rune": true, "Fire rune": true, "Yew shortbow": true, "Lobster": false, "Coins": false, "Staff of air": false} {
		if got := tr.AlchKeeps(d, name); got != want {
			t.Errorf("AlchKeeps(%q) = %v, want %v", name, got, want)
		}
	}
	low := actionsim.New(actionsim.Objs{}, nil)
	setStat(low, fleetplan.MagicStat, 140000, 54)
	if tr.AlchKeeps(&Deps{Session: low, Config: cfg}, "Yew shortbow") {
		t.Error("kept the kit at Magic 54")
	}
	off := &Travel{}
	if off.AlchKeeps(d, "Nature rune") {
		t.Error("kept the kit with walk alch off")
	}
	var nilTravel *Travel
	if nilTravel.AlchKeeps(d, "Nature rune") {
		t.Error("a nil Travel kept the kit")
	}
}

// A cast leaves in ONE write with the clear-move in front of it (an OPHELDU naming the bow at a slot that does not hold
// it): the engine reads both in one tick, in order, so moveClickRequest is clear when the cast delays the player and a
// zone trigger inside the delay cannot halt the walk (actions.ClearMoveRequest). The lab's NoClear sends the cast alone.
func TestWalkAlchCastCarriesAClearMoveInTheSameWrite(t *testing.T) {
	cfg := travelTestConfig(t)
	nature := objOf(t, cfg, "Nature rune", false)
	fire := objOf(t, cfg, "Fire rune", false)
	bowNote := objOf(t, cfg, "Yew shortbow", true)
	poll := webwalk.AsidePoll{Me: webwalk.Point{X: 2700, Z: 3400}, Target: webwalk.Point{X: 2740, Z: 3400}, SinceClick: 2 * time.Second}
	build := func() *actionsim.Session {
		sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: nature, Count: 10}, {ID: fire, Count: 50}, {ID: bowNote, Count: 20}})
		setStat(sim, fleetplan.MagicStat, 200000, 60)
		return sim
	}

	sim := build()
	tr := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised()}
	tr.alchPoll(&Deps{Session: sim, Config: cfg}, poll)
	sent := sim.Sent()
	if len(sent) != 1 || len(sent[0]) != opheldu+9 {
		t.Fatalf("%d writes, want one of an OPHELDU and an OPHELDT: %v", len(sent), sent)
	}
	clears := clearsSent(sim)
	if len(clears) != 1 {
		t.Fatalf("clear-moves sent: %v", clears)
	}
	const slot = 2 // the note stack is the third row: natures, fire runes, bow note
	if got := clears[0]; got[0] != int(bowNote) || got[2] != int(bowNote) || got[1] == slot || got[1] != got[3] {
		t.Errorf("clear-move %v: want the bow note at a slot other than %d, named twice alike", got, slot)
	}
	if got := castsSent(sim); len(got) != 1 || got[0].obj != int(bowNote) || got[0].slot != slot {
		t.Errorf("casts %+v, want one on the note in slot %d", got, slot)
	}
	if tr.clears != 1 || tr.castsSent != 1 {
		t.Errorf("counted %d clear-moves and %d casts, want 1 and 1", tr.clears, tr.castsSent)
	}

	alone := build()
	tn := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised(), Tune: Tuning{NoClear: true}}
	tn.alchPoll(&Deps{Session: alone, Config: cfg}, poll)
	if s := alone.Sent(); len(s) != 1 || len(s[0]) != 9 || len(clearsSent(alone)) != 0 {
		t.Errorf("NoClear sent %d writes %v and %d clear-moves; want the cast alone", len(s), s, len(clearsSent(alone)))
	}
}

// Without the clear-move the zone-trigger gate stands: a cast that a trigger on the route would hold up is not sent; with
// it, the same poll casts. The route runs along x=3009 across the map square line at z=3200.
func TestWalkAlchZoneGateOnlyStandsWithoutTheClearMove(t *testing.T) {
	cfg := travelTestConfig(t)
	nature := objOf(t, cfg, "Nature rune", false)
	fire := objOf(t, cfg, "Fire rune", false)
	bowNote := objOf(t, cfg, "Yew shortbow", true)
	var path []webwalk.Point
	for z := 3190; z <= 3240; z++ {
		path = append(path, webwalk.Point{X: 3009, Z: z})
	}
	poll := webwalk.AsidePoll{Me: webwalk.Point{X: 3009, Z: 3194}, Target: webwalk.Point{X: 3009, Z: 3240}, Path: path, SinceClick: 2 * time.Second}
	cast := func(tune Tuning) int {
		sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: nature, Count: 10}, {ID: fire, Count: 50}, {ID: bowNote, Count: 20}})
		setStat(sim, fleetplan.MagicStat, 200000, 60)
		tr := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised(), Tune: tune}
		tr.alchPoll(&Deps{Session: sim, Config: cfg}, poll)
		return len(castsSent(sim))
	}
	if n := cast(Tuning{}); n != 1 {
		t.Errorf("with the clear-move: %d casts a few tiles before a map square line, want 1", n)
	}
	if n := cast(Tuning{NoClear: true}); n != 0 {
		t.Errorf("without it: %d casts a few tiles before a map square line, want the gate to hold it back", n)
	}
	if n := cast(Tuning{ZoneGate: true}); n != 0 {
		t.Errorf("ZoneGate: %d casts, want the gate to hold it back", n)
	}
}

// A cast's answer is waited for four ticks, so an account whose experience update arrives after the second tick is not
// counted as having missed: live, a round trip near a second read as three misses and paused walk alch for ten minutes.
func TestSettleWaitsFourTicksBeforeCountingAMiss(t *testing.T) {
	cfg := travelTestConfig(t)
	nature := objOf(t, cfg, "Nature rune", false)
	fire := objOf(t, cfg, "Fire rune", false)
	bowNote := objOf(t, cfg, "Yew shortbow", true)
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: nature, Count: 10}, {ID: fire, Count: 50}, {ID: bowNote, Count: 20}})
	setStat(sim, fleetplan.MagicStat, 200000, 60)
	t0 := time.Unix(1_000_000, 0)
	now := t0
	d := &Deps{Session: sim, Config: cfg, Delay: func(time.Duration) bool { return true }}
	tr := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised(), Now: func() time.Time { return now }}
	poll := webwalk.AsidePoll{Me: webwalk.Point{X: 2700, Z: 3400}, Target: webwalk.Point{X: 2740, Z: 3400}, SinceClick: 2 * time.Second}
	tr.alchPoll(d, poll)
	for _, after := range []time.Duration{1300 * time.Millisecond, 1900 * time.Millisecond, 2300 * time.Millisecond} {
		now = t0.Add(after)
		tr.alchReady(d, now)
		if !tr.pending || tr.misses != 0 {
			t.Fatalf("%v after the cast: pending %v, %d misses; the answer is waited for four ticks", after, tr.pending, tr.misses)
		}
	}
	setStat(sim, fleetplan.MagicStat, 200650, 60) // the experience arrives late
	now = t0.Add(2500 * time.Millisecond)
	tr.alchReady(d, now)
	if tr.pending || tr.misses != 0 {
		t.Fatalf("pending %v, %d misses after the late experience", tr.pending, tr.misses)
	}
	if casts, xp, _, _ := tr.Counters(); casts != 1 || xp != 650 {
		t.Errorf("counters %d casts, %d xp, want 1 and 650", casts, xp)
	}
}

// A walk's line is made of the readings its start took and the path its polls saw, and is logged only for a walk the
// account could stall at all, running, of at least 30 tiles; and only a walk like that aims its clicks far.
func TestWalkLineAndFarClicksOnlyForAWalkTheAccountCanStall(t *testing.T) {
	cfg := travelTestConfig(t)
	nature := objOf(t, cfg, "Nature rune", false)
	fire := objOf(t, cfg, "Fire rune", false)
	bowNote := objOf(t, cfg, "Yew shortbow", true)
	build := func(running int, stacks ...actionsim.Stack) (*actionsim.Session, *Deps, *[]string) {
		sim := actionsim.New(actionsim.Objs{}, stacks)
		setStat(sim, fleetplan.MagicStat, 200000, 60)
		simPlace(sim.State(), 2700, 3400)
		sim.State().Handle(serverprot.UPDATE_RUNENERGY, []byte{100})
		sim.State().Handle(serverprot.VARP_SMALL, simPayload(func(p *packet.Packet) { p.P2(uint16(gamestate.VarpRun)); p.P1(byte(running)) }))
		lines := &[]string{}
		return sim, &Deps{Session: sim, Config: cfg, Log: func(s string) { *lines = append(*lines, s) }, Delay: func(time.Duration) bool { return true }}, lines
	}
	kit := []actionsim.Stack{{ID: nature, Count: 10}, {ID: fire, Count: 50}, {ID: bowNote, Count: 20}}

	sim, d, lines := build(1, kit...)
	tr := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised()}
	tr.beginWalk(d)
	if got := tr.clickSteps(); got != fleetplan.WalkAlchClickSteps {
		t.Errorf("clickSteps = %d for a walk with the kit, want %d", got, fleetplan.WalkAlchClickSteps)
	}
	for i := 1; i <= 40; i++ { // 40 polls, two tiles apart, a tick each
		simPlace(sim.State(), 2700+2*i, 3400)
		tr.alchPoll(d, webwalk.AsidePoll{Me: webwalk.Point{X: 2700 + 2*i, Z: 3400}, Target: webwalk.Point{X: 2900, Z: 3400}, SinceClick: 2 * time.Second})
	}
	sim.State().Handle(serverprot.UPDATE_RUNENERGY, []byte{78})
	tr.endWalk(d)
	if len(*lines) != 1 {
		t.Fatalf("%d lines logged, want one: %q", len(*lines), *lines)
	}
	for _, want := range []string{"run staller: 78 tiles in 40 ticks (1.95 a tick)", "run energy 100% -> 78%", "a tile against"} {
		if !strings.Contains((*lines)[0], want) {
			t.Errorf("line %q lacks %q", (*lines)[0], want)
		}
	}

	// Nothing to cast and nothing to rub: the walker's default clicks and no line.
	_, bare, bareLines := build(1, actionsim.Stack{ID: fire, Count: 50})
	tb := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised()}
	tb.beginWalk(bare)
	if tb.clickSteps() != 0 {
		t.Errorf("clickSteps = %d for a walk with nothing to cast or rub, want 0", tb.clickSteps())
	}
	tb.walk.tiles = 80
	tb.endWalk(bare)
	if n := len(*bareLines); n != 0 {
		t.Errorf("%d lines for a walk the account could not stall", n)
	}

	// A walking account is not logged either: there is no run energy to save.
	sim3, walking, walkingLines := build(0, kit...)
	tw := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised()}
	tw.beginWalk(walking)
	tw.walk.tiles = 80
	simPlace(sim3.State(), 2701, 3400)
	tw.endWalk(walking)
	if n := len(*walkingLines); n != 0 {
		t.Errorf("%d lines for a walking account", n)
	}
	var nilTravel *Travel
	nilTravel.beginWalk(d)
	nilTravel.endWalk(d)
	if nilTravel.clickSteps() != 0 {
		t.Error("a nil Travel aimed clicks far")
	}
}
