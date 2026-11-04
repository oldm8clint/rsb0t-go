package gemrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/gemplan"
)

// engineSim plays the engine's side of a chisel on uncut gems, as content writes it
// (skill_crafting/scripts/gem/uncut_gem.rs2): each OPHELDU is validated against the pack, then
// inv_del removes the LOWEST slot holding the gem and inv_add puts the product in the lowest empty
// slot, with no delay -- and at most five user events a tick are read (the engine's limit).
type engineSim struct {
	st      *gamestate.State
	chat    *chatlog.Log
	cheats  anticheat.Counters
	pack    [28]struct{ id, n int32 }
	pending [][]byte
	level   int
	xp      int
	// dropAll makes the engine ignore every use (a stalled burst); refuse says what a use is
	// answered with instead of a cut ("level", "members").
	dropAll bool
	refuse  string
	// dropSlot, when above 0, makes the engine drop the one use naming that slot (lag: the pack
	// changed under it).
	dropSlot int
	// lagOneTick holds every pack update back a tick, as a slow connection shows it; held is the
	// update waiting.
	lagOneTick bool
	held       []byte
	// smashEvery makes every nth crushable cut a smash.
	smashEvery int
	cuts       int

	ticks, busyTicks, mostInATick, mostQueued int
}

// idlePlayerInfo is a PLAYER_INFO with no local update and no other players (1 bit 0, 8 bits 0,
// 11 bits 2047), which is all gamestate.State.Tick needs to count a server tick.
var idlePlayerInfo = []byte{0x00, 0x7f, 0xf0}

func (e *engineSim) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(32)
	build(w)
	e.pending = append(e.pending, append([]byte(nil), w.Data[:w.Pos]...))
	return nil
}
func (e *engineSim) State() *gamestate.State     { return e.st }
func (e *engineSim) Cheats() *anticheat.Counters { return &e.cheats }

func g2(b []byte, at int) int { return int(b[at])<<8 | int(b[at+1]) }

func (e *engineSim) push() {
	p := packet.NewWriter(256)
	p.P2(uint16(PlayerPack))
	p.P2(28)
	for _, s := range e.pack {
		if s.id == 0 {
			p.P2(0)
			p.P1(0)
			continue
		}
		p.P2(uint16(s.id + 1))
		p.P1(byte(s.n))
	}
	if e.lagOneTick {
		e.held = append([]byte(nil), p.Data[:p.Pos]...)
		return
	}
	e.st.Handle(serverprot.UPDATE_INV_FULL, p.Data[:p.Pos])
}

// stat pushes the Crafting stat. The wire carries whole XP (the engine's internal tenths, which
// content's stat_advance works in, divided by ten: gemplan.Gem.DisplayXP).
func (e *engineSim) stat() {
	xp := e.xp / 10
	e.st.Handle(serverprot.UPDATE_STAT, []byte{byte(CraftingStat), byte(xp >> 24), byte(xp >> 16), byte(xp >> 8), byte(xp), byte(e.level)})
}

// tick is one game tick: the uses read this tick are applied.
func (e *engineSim) tick() {
	e.ticks++
	defer e.st.Handle(serverprot.PLAYER_INFO, idlePlayerInfo) // the server's every-tick PLAYER_INFO
	// Last tick's pack update, held back a tick, arrives now.
	if e.held != nil {
		e.st.Handle(serverprot.UPDATE_INV_FULL, e.held)
		e.held = nil
	}
	if e.dropAll {
		e.pending = nil
		return
	}
	// The engine reads a player's queued packets in order until five user events were handled;
	// the rest wait in the socket buffer for the next tick (NetworkPlayer.ts decodeIn).
	uses := e.pending
	if len(uses) > 5 {
		uses = uses[:5]
	}
	e.pending = append([][]byte(nil), e.pending[len(uses):]...)
	e.mostQueued = max(e.mostQueued, len(e.pending))
	done := 0
	for _, b := range uses {
		if len(b) != 13 || int(b[0]) != clientprot.OPHELDU {
			continue
		}
		obj, slot, useObj, useSlot := int32(g2(b, 1)), g2(b, 3), int32(g2(b, 7)), g2(b, 9)
		if slot >= 28 || useSlot >= 28 || e.pack[slot].id != obj || e.pack[useSlot].id != useObj {
			continue // not what the pack holds: the engine drops it
		}
		if e.dropSlot > 0 && slot == e.dropSlot {
			continue
		}
		gem, ok := gemplan.GemByUncutID(obj)
		if !ok || useObj != gemplan.ChiselID {
			continue
		}
		switch {
		case e.refuse == "members":
			e.chat.Record("You need to be on a members' server to use this feature.")
			continue
		case e.level < gem.Level:
			e.chat.Record("You need a Crafting level of " + itoa(gem.Level) + " to cut " + gem.Name + "s.")
			continue
		}
		for i := range e.pack { // inv_del: the lowest slot holding the gem
			if e.pack[i].id == gem.UncutID {
				e.pack[i].id, e.pack[i].n = 0, 0
				break
			}
		}
		product, xp := gem.CutID, gem.XP
		e.cuts++
		if gem.CanCrush && e.smashEvery > 0 && e.cuts%e.smashEvery == 0 {
			product, xp = gemplan.CrushedGemstoneID, (gem.XP+2)/4
		}
		for i := range e.pack { // inv_add: the lowest empty slot
			if e.pack[i].id == 0 {
				e.pack[i].id, e.pack[i].n = product, 1
				break
			}
		}
		e.xp += xp
		done++
	}
	if done > 0 {
		e.busyTicks++
		e.mostInATick = max(e.mostInATick, done)
		e.push()
		e.stat()
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// newEngine is a session whose pack holds chisel (when withChisel) and n uncut gems of gem, with
// Crafting at level, and the Deps a CutAll runs on. Delay advances one game tick.
func newEngine(t *testing.T, gem gemplan.Gem, n, level int, withChisel bool) (*engineSim, *Deps) {
	t.Helper()
	e := &engineSim{st: gamestate.New(), chat: chatlog.New(), level: level}
	slot := 0
	if withChisel {
		e.pack[slot].id, e.pack[slot].n = gemplan.ChiselID, 1
		slot++
	}
	for i := 0; i < n && slot < 28; i++ {
		e.pack[slot].id, e.pack[slot].n = gem.UncutID, 1
		slot++
	}
	comps := archive.Components{
		3213: {ID: 3213, Type: archive.TypeLayer, Children: []int{3214}},
		3214: {ID: 3214, Type: archive.TypeInv, ObjOps: true},
	}
	e.st.Handle(serverprot.IF_SETICON, []byte{byte(3213 >> 8), byte(3213 & 0xff), 3})
	e.push()
	e.stat()
	d := &Deps{
		Session: e, Components: comps, Chat: e.chat,
		Delay: func(time.Duration) bool { e.tick(); time.Sleep(time.Millisecond); return true },
	}
	return e, d
}

func gemByKey(t *testing.T, key string) gemplan.Gem {
	t.Helper()
	for _, g := range gemplan.Gems {
		if g.Key == key {
			return g
		}
	}
	t.Fatalf("no gem %q", key)
	return gemplan.Gem{}
}

// THE POINT OF CutAll: a pack of 27 is six bursts, five gems in one engine tick, against the one
// gem a tick that the same slot named five times managed.
func TestCutAllCutsAPackFiveATick(t *testing.T) {
	sap := gemByKey(t, "sapphire")
	e, d := newEngine(t, sap, 27, 30, true)
	r := CutAll(d, sap)
	if r.Reason != "done" || r.Cuts != 27 || r.Crushed != 0 || r.Bursts != 6 {
		t.Fatalf("%+v", r)
	}
	if e.mostInATick != 5 {
		t.Fatalf("most gems cut in one tick: %d, want 5", e.mostInATick)
	}
	if e.busyTicks != 6 {
		t.Fatalf("%d ticks did the work, want 6 (27 gems five a tick)", e.busyTicks)
	}
	if CountByID(d, sap.CutID) != 27 || CountByID(d, sap.UncutID) != 0 || !HasChisel(d) {
		t.Fatalf("pack: %d cut, %d uncut, chisel %v", CountByID(d, sap.CutID), CountByID(d, sap.UncutID), HasChisel(d))
	}
	if r.XPGained != 27*sap.DisplayXP() {
		t.Fatalf("XP gained %d, want %d", r.XPGained, 27*sap.DisplayXP())
	}
	if got := r.Describe(); got == "" {
		t.Fatal("no description")
	}
}

func TestCutAllFewerThanABurst(t *testing.T) {
	ruby := gemByKey(t, "ruby")
	_, d := newEngine(t, ruby, 3, 60, true)
	if r := CutAll(d, ruby); r.Reason != "done" || r.Cuts != 3 || r.Bursts != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestCutAllSmashedCrushableGemsAreCounted(t *testing.T) {
	opal := gemByKey(t, "opal")
	e, d := newEngine(t, opal, 20, 5, true)
	e.smashEvery = 2
	r := CutAll(d, opal)
	if r.Reason != "done" || r.Cuts != 10 || r.Crushed != 10 {
		t.Fatalf("%+v", r)
	}
	if CountByID(d, 1633) != 10 {
		t.Fatalf("%d crushed gemstones in the pack", CountByID(d, 1633))
	}
}

func TestCutAllLevelRefusalDeniesTheGemAtOnce(t *testing.T) {
	sap := gemByKey(t, "sapphire")
	_, d := newEngine(t, sap, 27, 10, true)
	r := CutAll(d, sap)
	if r.Reason != "refused" || r.RefusedLevel != 20 || r.Cuts != 0 || r.Bursts != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestCutAllMembersRefusal(t *testing.T) {
	opal := gemByKey(t, "opal")
	e, d := newEngine(t, opal, 10, 30, true)
	e.refuse = "members"
	if r := CutAll(d, opal); r.Reason != "members" || r.Cuts != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestCutAllNoChisel(t *testing.T) {
	sap := gemByKey(t, "sapphire")
	e, d := newEngine(t, sap, 5, 30, false)
	if r := CutAll(d, sap); r.Reason != "no-chisel" || r.Bursts != 0 {
		t.Fatalf("%+v", r)
	}
	if len(e.pending) != 0 {
		t.Fatal("a burst was sent with no chisel")
	}
}

func TestCutAllNothingToCut(t *testing.T) {
	sap := gemByKey(t, "sapphire")
	_, d := newEngine(t, sap, 0, 30, true)
	if r := CutAll(d, sap); r.Reason != "done" || r.Cuts != 0 || r.Bursts != 0 {
		t.Fatalf("%+v", r)
	}
}

// A burst the engine ignores (and says nothing about) ends the loop as stalled instead of spinning.
func TestCutAllStalledBurst(t *testing.T) {
	sap := gemByKey(t, "sapphire")
	e, d := newEngine(t, sap, 27, 30, true)
	e.dropAll = true
	r := CutAll(d, sap)
	// Pipelined: every burst goes out a tick apart without waiting, and the one confirm at the end
	// sees nothing changed.
	if r.Reason != "stalled" || r.Cuts != 0 || r.Bursts != 6 || r.Left != 27 {
		t.Fatalf("%+v", r)
	}
}

func TestCutAllStopsWhenToldTo(t *testing.T) {
	sap := gemByKey(t, "sapphire")
	e, d := newEngine(t, sap, 27, 30, true)
	e.dropAll = true
	d.Delay = func(time.Duration) bool { return false }
	if r := CutAll(d, sap); r.Reason != "stopped" {
		t.Fatalf("%+v", r)
	}
}

// THE PIPELINE: each burst goes out on the next tick, to slots no earlier burst named, before the
// earlier one shows in the pack, so the 27 cuts take six consecutive engine ticks and the cut is
// over one tick after the last burst. The bank open (AfterLast) goes out right behind the last
// burst, before the engine has read it.
func TestCutAllPipelinesOneBurstATickAndOpensTheBankBehindTheLast(t *testing.T) {
	ruby := gemByKey(t, "ruby")
	e, d := newEngine(t, ruby, 27, 60, true)
	// The client sees each burst only a tick late: hold every pack update back one tick.
	e.lagOneTick = true
	var openAt, queuedAtOpen int
	r := CutAllThen(d, ruby, func() { openAt, queuedAtOpen = e.ticks, len(e.pending) })
	if r.Reason != "done" || r.Cuts != 27 || r.Bursts != 6 || r.Left != 0 {
		t.Fatalf("%+v", r)
	}
	if e.busyTicks != 6 || e.mostInATick != 5 {
		t.Fatalf("%d busy ticks, at most %d in one: want 6 and 5", e.busyTicks, e.mostInATick)
	}
	if openAt != 5 || queuedAtOpen != 2 {
		t.Fatalf("the bank open went out after tick %d with %d uses unread: want after tick 5, behind the last burst of 2", openAt, queuedAtOpen)
	}
	if e.ticks > 8 {
		t.Fatalf("the cut took %d ticks, want the six that cut and the lag of one", e.ticks)
	}
	if r.XPGained != 27*ruby.DisplayXP() {
		t.Fatalf("XP %d", r.XPGained)
	}
}

// A use the engine drops (the pack changed under it) is left held, not retried: the next bank
// visit deposits it and draws it again.
func TestCutAllReportsADroppedUseAsPartial(t *testing.T) {
	ruby := gemByKey(t, "ruby")
	e, d := newEngine(t, ruby, 27, 60, true)
	e.dropSlot = 3
	r := CutAll(d, ruby)
	if r.Reason != "partial" || r.Cuts != 26 || r.Left != 1 || r.Bursts != 6 {
		t.Fatalf("%+v", r)
	}
}

// Bursts that reach the engine faster than it reads them wait in its buffer, five read a tick:
// nothing is lost, the cut only finishes later.
func TestCutAllBurstsTheEngineCannotReadYetAreQueuedNotLost(t *testing.T) {
	sap := gemByKey(t, "sapphire")
	e, d := newEngine(t, sap, 27, 30, true)
	eq := actions.NewEquipment(e, d.Components)
	named := map[int]bool{}
	for i := 0; i < 3; i++ { // three bursts before the engine reads anything
		slots, err := eq.UseOnHeldManyExcept(gemplan.ChiselID, sap.UncutID, 5, named)
		if err != nil || len(slots) != 5 {
			t.Fatalf("burst %d: %v %v", i, slots, err)
		}
		for _, s := range slots {
			named[s] = true
		}
	}
	e.tick()
	if CountByID(d, sap.UncutID) != 22 || len(e.pending) != 10 {
		t.Fatalf("after one tick: %d uncut, %d queued; want 22 and 10", CountByID(d, sap.UncutID), len(e.pending))
	}
	e.tick()
	e.tick()
	if CountByID(d, sap.UncutID) != 12 || len(e.pending) != 0 {
		t.Fatalf("after three ticks: %d uncut, %d queued; want 12 and 0", CountByID(d, sap.UncutID), len(e.pending))
	}
}
