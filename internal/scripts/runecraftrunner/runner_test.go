package runecraftrunner

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
)

func init() {
	// Real waits are seconds; the engine below ticks in tens of milliseconds.
	arriveWait, craftWait, portalWait, xpWait = 1200*time.Millisecond, 1200*time.Millisecond, 1200*time.Millisecond, 400*time.Millisecond
}

const (
	engineTick = 25 * time.Millisecond
	essID      = int32(runecraftplan.EssenceID)
)

func objs() actionsim.Objs {
	stack := map[int32]bool{556: true, 559: true, 561: true, 563: true, 555: true, 995: true, bowNoteID: true}
	return actionsim.Objs{
		Stackable: func(id int32) bool { return stack[id] },
		NoteOf: func(id int32) int32 {
			if id == essID {
				return runecraftplan.NotedEssenceID
			}
			return 0
		},
		UnnoteOf: func(id int32) int32 {
			if id == runecraftplan.NotedEssenceID {
				return essID
			}
			return 0
		},
	}
}

// engine plays the server's side of the ruins, the altar and the portal, TICK BY TICK, as content 289 runs
// them (skill_runecraft/scripts/runecraft.rs2):
//
//   - a talisman used on the ruins (OPLOCU) from a tile the engine accepts (locreach), p_delay(1): the jump
//     into the temple is the second tick after the read;
//   - Craft-rune (OPLOC1) on the altar binds every unnoted essence at once, adds the runes (level / multiplier
//   - 1 each) and the experience, and p_delay(3): delayedUntil = tick + 1 + 3, four ticks on;
//   - the portal (OPLOC1) takes the account out at once;
//   - a packet read while the player is delayed is DROPPED without a word (every op handler's
//     `if (player.delayed) return`). World.cycle reads the packets (processClientsIn) BEFORE processPlayers clears
//     the flag (`currentTick >= delayedUntil`), so a packet read IN tick delayedUntil is still dropped: the reads
//     of ticks T+1..T+4 are lost after a p_delay(3) at tick T, and T+5 is the first free one (delayed()).
//
// quirks scripts the misbehaviour a test wants.
type engine struct {
	sim  *actionsim.Session
	chat *chatlog.Log
	a    runecraftplan.Altar

	mu           sync.Mutex
	tick         int
	delayedUntil int
	pending      [][]byte
	here         tile.Tile
	teleportAt   int
	xpT          int // experience in tenths
	level        int
	ruins        locreach.Loc

	dropped, crafts, portals, ruinsClicks int
	craftTicks                            []int
	// noXP makes a craft take the essence and give no experience; ignoreFirstAltar swallows the first altar
	// click as if it were read while delayed.
	noXP, ignoreFirstAltar bool
	altarClicks            int
	// craftLateTicks and teleportLateTicks hold the effect of a landed click back by that many ticks, as a server that is slow to
	// answer does (lagwait.go); lateCraftAt is the tick a held craft lands in.
	craftLateTicks, teleportLateTicks int
	lateCraftAt                       int
}

func newEngine(t *testing.T, a runecraftplan.Altar, pack []actionsim.Stack, at tile.Tile, level int) (*engine, *fleetrunner.Deps) {
	t.Helper()
	sim := actionsim.New(objs(), pack)
	e := &engine{sim: sim, chat: chatlog.New(), a: a, level: level, xpT: levels.XPAtLevel(level) * 10,
		ruins: locreach.Loc{X: a.Ruins.X, Z: a.Ruins.Z, Width: 3, Length: 3, Shape: 10}}
	e.setStat()
	e.moveTo(at)
	sim.OnSend = func(b []byte) {
		e.mu.Lock()
		e.pending = append(e.pending, b)
		e.mu.Unlock()
	}
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		tk := time.NewTicker(engineTick)
		defer tk.Stop()
		for {
			select {
			case <-done:
				return
			case <-tk.C:
				e.step()
			}
		}
	}()
	d := &fleetrunner.Deps{
		Session: sim, Chat: e.chat, Components: actionsim.Components(),
		Delay: func(dur time.Duration) bool { time.Sleep(dur); return true },
	}
	return e, d
}

func (e *engine) moveTo(t tile.Tile) {
	e.here = t
	e.sim.StandAt(t.X, t.Z)
}

func (e *engine) setStat() {
	xp, level := e.xpT/10, levels.LevelFromXP(e.xpT/10)
	e.level = level
	e.sim.State().Handle(serverprot.UPDATE_STAT, statPacket(Stat, xp, level))
}

func statPacket(stat, xp, level int) []byte {
	p := packet.NewWriter(8)
	p.P1(byte(stat))
	p.P4(int32(xp))
	p.P1(byte(level))
	return p.Data[:p.Pos]
}

func g2(b []byte, at int) int { return int(b[at])<<8 | int(b[at+1]) }

func (e *engine) say(s string) { e.chat.Record(s) }

// step is one server tick. The order is the engine's own (World.cycle): the packets are read and the world
// moves, then the output goes out -- PLAYER_INFO first (the client's tick counter), the inventories and stats
// after it -- so a client that sees a craft's inventory has already counted the tick it happened in.
func (e *engine) step() {
	e.mu.Lock()
	e.tick++
	e.sim.Tick()
	pend := e.pending
	e.pending = nil
	for _, b := range pend {
		e.handle(b)
	}
	if e.teleportAt != 0 && e.tick >= e.teleportAt {
		e.teleportAt = 0
		e.moveTo(e.a.Enter)
	}
	if e.lateCraftAt != 0 && e.tick >= e.lateCraftAt {
		e.lateCraftAt = 0
		e.craft()
	}
	e.mu.Unlock()
}

// delayed is whether a packet read at the start of this tick is dropped: the flag is cleared in processPlayers, after
// the read, so the tick delayedUntil itself still drops (World.ts: processClientsIn 355, processPlayers 692).
func (e *engine) delayed() bool { return e.tick <= e.delayedUntil }

func (e *engine) handle(b []byte) {
	n := len(b)
	switch {
	case n >= 13 && int(b[n-13]) == clientprot.OPLOCU:
		u := b[n-13:]
		e.useOnLoc(g2(u, 1), g2(u, 3), g2(u, 5), g2(u, 7), g2(u, 9), g2(u, 11))
	case n >= 7 && int(b[n-7]) == clientprot.OPLOC1:
		u := b[n-7:]
		e.opLoc1(g2(u, 1), g2(u, 3), g2(u, 5))
	}
}

func (e *engine) useOnLoc(x, z, loc, obj, slot, com int) {
	if e.delayed() {
		e.dropped++
		return
	}
	e.ruinsClicks++
	if loc != e.a.RuinsID || x != e.a.Ruins.X || z != e.a.Ruins.Z || com != PlayerPack {
		e.say("Nothing interesting happens.")
		return
	}
	if obj != e.a.TalismanID {
		e.say("Nothing interesting happens.")
		return
	}
	if reached, _ := locreach.Reached(e.here.X, e.here.Z, e.ruins); !reached {
		e.say("I can't reach that!")
		return
	}
	e.say("You hold the " + e.a.Name + " Talisman towards the mysterious ruins.")
	e.delayedUntil = e.tick + 2 + e.teleportLateTicks
	e.teleportAt = e.tick + 2 + e.teleportLateTicks
}

func (e *engine) opLoc1(x, z, loc int) {
	if e.delayed() {
		e.dropped++
		return
	}
	switch {
	case loc == e.a.AltarID && e.a.Inside(e.here):
		e.altarClicks++
		if e.ignoreFirstAltar && e.altarClicks == 1 {
			e.dropped++
			return
		}
		if e.craftLateTicks > 0 && e.crafts == 0 {
			// The first click is answered late; one sent meanwhile is swallowed by the delay the craft will start.
			if e.lateCraftAt == 0 {
				e.lateCraftAt = e.tick + e.craftLateTicks
			}
			return
		}
		e.craft()
	case loc == e.a.PortalID && e.a.Inside(e.here):
		e.portals++
		e.moveTo(e.a.Exit)
	}
}

func (e *engine) craft() {
	if e.level < e.a.Level {
		e.say("You need a Runecrafting level of at least " + strconv.Itoa(e.a.Level) + " to craft " + e.a.Name + " runes.")
		return
	}
	n := 0
	for _, s := range e.sim.Pack() {
		if s.ID == essID {
			n += s.Count
		}
	}
	if n == 0 {
		e.say("You do not have any rune stones to bind.")
		return
	}
	level := e.level
	e.sim.Remove(essID, n)
	e.sim.Give(int32(e.a.RuneID), n*e.a.RunesPerEssence(level))
	if !e.noXP {
		e.xpT += n * e.a.XP10
		e.setStat()
	}
	e.crafts++
	e.craftTicks = append(e.craftTicks, e.tick)
	e.delayedUntil = e.tick + 4
	e.say("You bind the temple's power into " + e.a.Name + " Runes.")
}

func (e *engine) count(id int32) int {
	n := 0
	for _, s := range e.sim.Pack() {
		if s.ID == id {
			n += s.Count
		}
	}
	return n
}

func essence(n int) []actionsim.Stack {
	out := make([]actionsim.Stack, n)
	for i := range out {
		out[i] = actionsim.Stack{ID: essID, Count: 1}
	}
	return out
}

func airKit(n int) []actionsim.Stack {
	return append([]actionsim.Stack{{ID: int32(runecraftplan.Air.TalismanID), Count: 1}}, essence(n)...)
}

var (
	airRuinsStand = tile.Tile{X: 2983, Z: 3292} // beside the 3x3 ruins at 2984,3291
	airRuinsBad   = tile.Tile{X: 2983, Z: 3290} // a corner: the engine does nothing from here
)

func airRuinsMatch() locindex.Match {
	a := runecraftplan.Air
	return locindex.Match{Loc: locindex.Loc{ID: a.RuinsID, X: a.Ruins.X, Z: a.Ruins.Z, Shape: 10}, Name: runecraftplan.RuinsName}
}

func airAltarMatch() locindex.Match {
	a := runecraftplan.Air
	return locindex.Match{Loc: locindex.Loc{ID: a.AltarID, X: a.AltarAt.X, Z: a.AltarAt.Z, Shape: 10, Angle: 1}, Name: runecraftplan.AltarName, Op: 1}
}

func airPortalMatch() locindex.Match {
	a := runecraftplan.Air
	return locindex.Match{Loc: locindex.Loc{ID: a.PortalID, X: a.PortalAt.X, Z: a.PortalAt.Z, Shape: 10}, Name: runecraftplan.PortalName, Op: 1}
}

// The talisman on the ruins from a tile the engine accepts: the account is SEEN inside the temple on the
// arrival tile two ticks later, the talisman is still in the pack (nothing deletes it), one click was sent.
func TestUseTalismanIsDoneWhenTheTempleIsSeen(t *testing.T) {
	e, d := newEngine(t, runecraftplan.Air, airKit(27), airRuinsStand, 1)
	res := UseTalisman(d, runecraftplan.Air, airRuinsMatch())
	if !res.Arrived || res.At != runecraftplan.Air.Enter || res.Clicks != 1 || res.Ticks < 2 || res.Reason != "" {
		t.Fatalf("%+v", res)
	}
	if e.count(int32(runecraftplan.Air.TalismanID)) != 1 || e.count(essID) != 27 {
		t.Errorf("the pack after: %v", e.sim.Pack())
	}
	if !strings.Contains(strings.ToLower(res.Said), "talisman") {
		t.Errorf("said %q", res.Said)
	}
}

// The corner of the 3x3 is not a tile the engine accepts: it says so and does nothing, and the use is NOT done. The refusal ends the wait
// at once (it used to cost the whole of arriveWait twice) and is not clicked again from the same tile.
func TestUseTalismanFromTheWrongTileIsAFailureNotAnAssumption(t *testing.T) {
	e, d := newEngine(t, runecraftplan.Air, airKit(27), airRuinsBad, 1)
	started := time.Now()
	res := UseTalisman(d, runecraftplan.Air, airRuinsMatch())
	if res.Arrived || res.Clicks != 1 || !strings.Contains(res.Reason, "can't reach") || !strings.Contains(res.Said, "can't reach") {
		t.Fatalf("%+v", res)
	}
	if took := time.Since(started); took > arriveWait/2 {
		t.Errorf("took %s: the refusal should end the wait, not the %s timeout", took, arriveWait/2)
	}
	if e.a.Inside(e.here) {
		t.Error("the engine moved the account")
	}
}

// A use read while the player is still delayed is dropped without a word; the second click, half a wait
// later, is the one that works.
func TestUseTalismanClicksAgainWhenTheFirstWasDropped(t *testing.T) {
	e, d := newEngine(t, runecraftplan.Air, airKit(27), airRuinsStand, 1)
	e.mu.Lock()
	e.delayedUntil = e.tick + 8 // 200 ms of delay: the first click is read inside it
	e.mu.Unlock()
	res := UseTalisman(d, runecraftplan.Air, airRuinsMatch())
	if !res.Arrived || res.Clicks != 2 || e.dropped != 1 {
		t.Fatalf("%+v, dropped %d", res, e.dropped)
	}
}

func TestUseTalismanWithoutOne(t *testing.T) {
	_, d := newEngine(t, runecraftplan.Air, essence(27), airRuinsStand, 1)
	res := UseTalisman(d, runecraftplan.Air, airRuinsMatch())
	if res.Arrived || res.Clicks != 0 || !strings.Contains(res.Reason, "no Air talisman") {
		t.Fatalf("%+v", res)
	}
}

// A craft is verified: the essence gone, the runes in, the experience up by the table's 5.0 a essence.
func TestCraftIsVerifiedByEssenceAndExperience(t *testing.T) {
	e, d := newEngine(t, runecraftplan.Air, airKit(27), runecraftplan.Air.Enter, 1)
	res := Craft(d, runecraftplan.Air, airAltarMatch())
	if !res.OK() || res.Essence != 27 || res.Left != 0 || res.Runes != 27 || res.XPGain != 135 || res.Level0 != 1 || res.Clicks != 1 {
		t.Fatalf("%+v", res)
	}
	if res.Verify.Rate != 1 || res.Verify.ExpectedXP != 135 || res.Tick == 0 {
		t.Errorf("verify %+v tick %d", res.Verify, res.Tick)
	}
	if e.count(int32(runecraftplan.Air.RuneID)) != 27 || e.count(essID) != 0 || e.count(int32(runecraftplan.Air.TalismanID)) != 1 {
		t.Errorf("pack %v", e.sim.Pack())
	}
}

// Runes per essence follow the level: Air at 11 gives two a essence, so 27 essence are 54 runes, and the
// level-up shows in the result.
func TestCraftMeasuresTheRunesTheLevelGives(t *testing.T) {
	_, d := newEngine(t, runecraftplan.Air, airKit(27), runecraftplan.Air.Enter, 11)
	res := Craft(d, runecraftplan.Air, airAltarMatch())
	if !res.OK() || res.Runes != 54 || res.XPGain != 135 {
		t.Fatalf("%+v", res)
	}
	// 27 * 5.0 = 135 xp from 1 xp short of level 20's 4470 crosses the level.
	e2, d2 := newEngine(t, runecraftplan.Air, airKit(27), runecraftplan.Air.Enter, 19)
	e2.mu.Lock()
	e2.xpT = (4470 - 10) * 10
	e2.setStat()
	e2.mu.Unlock()
	res = Craft(d2, runecraftplan.Air, airAltarMatch())
	if !res.OK() || res.Level0 != 19 || res.Level1 != 20 {
		t.Fatalf("a level-up: %+v", res)
	}
}

// The essence left the pack and no experience came: not a craft.
func TestACraftWithoutExperienceDoesNotCount(t *testing.T) {
	e, d := newEngine(t, runecraftplan.Air, airKit(27), runecraftplan.Air.Enter, 1)
	e.noXP = true
	res := Craft(d, runecraftplan.Air, airAltarMatch())
	if res.OK() || !strings.Contains(res.Reason, "experience did not rise") || res.XPGain != 0 || res.Left != 0 {
		t.Fatalf("%+v", res)
	}
}

// The game's refusals end the craft at once: no stones, a level too low.
func TestCraftRefusals(t *testing.T) {
	_, d := newEngine(t, runecraftplan.Air, []actionsim.Stack{{ID: int32(runecraftplan.Air.TalismanID), Count: 1}}, runecraftplan.Air.Enter, 1)
	if res := Craft(d, runecraftplan.Air, airAltarMatch()); res.OK() || !strings.Contains(res.Reason, "no Rune essence") || res.Clicks != 0 {
		t.Errorf("an empty pack: %+v", res)
	}
	// The Body altar at level 5.
	b := runecraftplan.Body
	e2, d2 := newEngine(t, b, append([]actionsim.Stack(nil), essence(27)...), b.Enter, 5)
	body := locindex.Match{Loc: locindex.Loc{ID: b.AltarID, X: b.AltarAt.X, Z: b.AltarAt.Z, Shape: 10}, Name: "Altar", Op: 1}
	res := Craft(d2, b, body)
	if res.OK() || !strings.Contains(res.Reason, "refused") || !strings.Contains(res.Reason, "level of at least 20") || res.Clicks != 1 {
		t.Errorf("level too low: %+v", res)
	}
	if e2.count(essID) != 27 {
		t.Error("the essence was taken")
	}
}

// An altar click swallowed by a delay is sent again.
func TestCraftClicksAgainWhenTheFirstWasDropped(t *testing.T) {
	e, d := newEngine(t, runecraftplan.Air, airKit(27), runecraftplan.Air.Enter, 1)
	e.ignoreFirstAltar = true
	res := Craft(d, runecraftplan.Air, airAltarMatch())
	if !res.OK() || res.Clicks != 2 {
		t.Fatalf("%+v", res)
	}
}

// The engine's rule the simulator above encodes, stated on its own so a change to it is a visible one: a craft at tick
// T is p_delay(3), delayedUntil = T+4, and World.cycle reads packets before processPlayers clears the flag, so the
// reads of T+1..T+4 are dropped and T+5 is the first free one. (A model that clears the flag before the read frees T+4
// and passes a portal click that the real engine loses.)
func TestThePacketReadDropsTheTickDelayedUntilItself(t *testing.T) {
	e := &engine{delayedUntil: 100 + 4}
	for _, c := range []struct {
		tick int
		drop bool
	}{{101, true}, {102, true}, {103, true}, {104, true}, {105, false}, {106, false}} {
		e.tick = c.tick
		if got := e.delayed(); got != c.drop {
			t.Errorf("a packet read at tick %d after a craft at 100: dropped %v, want %v", c.tick, got, c.drop)
		}
	}
	if runecraftplan.HoldTicks != 4 {
		t.Errorf("HoldTicks %d: the release (craftTick + HoldTicks) is derived for a p_delay(3), four ticks", runecraftplan.HoldTicks)
	}
}

// THE TICK MANIPULATION: the craft holds the player four ticks and a click read inside the hold is dropped
// without a word. PortalOut sends its click only once the client has seen the output of the last busy tick
// (craft + 4), so it is read in the first free tick (craft + 5): one click, none dropped, the account out. The
// same click sent at once is dropped, and costs a wait and a second click.
func TestThePortalIsTimedToTheEndOfTheCraftsHold(t *testing.T) {
	e, d := newEngine(t, runecraftplan.Air, airKit(27), runecraftplan.Air.Enter, 1)
	res := Craft(d, runecraftplan.Air, airAltarMatch())
	if !res.OK() {
		t.Fatalf("%+v", res)
	}
	out := PortalOut(d, runecraftplan.Air, airPortalMatch(), res.Tick, nil)
	if !out.Out || out.Clicks != 1 || e.dropped != 0 || out.At != runecraftplan.Air.Exit || out.Waited < 1 {
		t.Fatalf("timed: %+v dropped %d", out, e.dropped)
	}
	// Untimed (no craft tick known): the click lands in the hold, is dropped, and the second one works.
	e2, d2 := newEngine(t, runecraftplan.Air, airKit(27), runecraftplan.Air.Enter, 1)
	if r := Craft(d2, runecraftplan.Air, airAltarMatch()); !r.OK() {
		t.Fatalf("%+v", r)
	}
	out = PortalOut(d2, runecraftplan.Air, airPortalMatch(), 0, nil)
	if !out.Out || out.Clicks < 2 || e2.dropped < 1 {
		t.Fatalf("untimed: %+v dropped %d", out, e2.dropped)
	}
}

func TestPortalOutThatNeverTakesIsAFailure(t *testing.T) {
	e, d := newEngine(t, runecraftplan.Air, nil, runecraftplan.Air.Enter, 1)
	// A portal that is not the temple's: the engine ignores the click.
	bad := airPortalMatch()
	bad.ID++
	out := PortalOut(d, runecraftplan.Air, bad, 0, nil)
	if out.Out || out.Clicks != 3 || !strings.Contains(out.Reason, "still inside") || e.portals != 0 {
		t.Fatalf("%+v", out)
	}
}

// A full cycle: the talisman on the ruins, the craft, the portal, the account back on the road with runes.
func TestOneTripFromTheRuinsAndBack(t *testing.T) {
	e, d := newEngine(t, runecraftplan.Air, airKit(27), airRuinsStand, 1)
	a := runecraftplan.Air
	if r := UseTalisman(d, a, airRuinsMatch()); !r.Arrived {
		t.Fatalf("%+v", r)
	}
	c := Craft(d, a, airAltarMatch())
	if !c.OK() {
		t.Fatalf("%+v", c)
	}
	if p := PortalOut(d, a, airPortalMatch(), c.Tick, nil); !p.Out {
		t.Fatalf("%+v", p)
	}
	if e.crafts != 1 || e.portals != 1 || e.dropped != 0 {
		t.Errorf("crafts %d portals %d dropped %d", e.crafts, e.portals, e.dropped)
	}
	if got := Talismans(d); !got[runecraftplan.KeyAir] || len(got) != 1 {
		t.Errorf("talismans %v", got)
	}
	if Essence(d) != 0 || PackCount(d, int32(a.RuneID)) != 27 {
		t.Errorf("pack %v", e.sim.Pack())
	}
	if lv, xp := Level(d); lv != 2 || xp != 135 { // 135 xp is level 2 (83)
		t.Errorf("level %d xp %d", lv, xp)
	}
	if r, _ := runecraftplan.WhereIs(Here(d)); r != runecraftplan.RegionOutside {
		t.Errorf("back at %v", Here(d))
	}
}

// ---- the bank ----

const (
	airTal  = int32(1438)
	bodyTal = int32(1446)
)

// bankRig is a bank at tile `post`, played by actionsim and reached through the OpenBank seam.
func bankRig(t *testing.T, pack, bank []actionsim.Stack) (*actionsim.Session, *fleetrunner.Deps) {
	t.Helper()
	sim := actionsim.New(objs(), pack)
	sim.OpenBank(bank)
	old := OpenBank
	t.Cleanup(func() { OpenBank = old })
	OpenBank = func(d *fleetrunner.Deps, at tile.Tile) (*actions.Bank, bool) {
		sim.OpenBank(sim.Bank())
		return actions.NewBank(sim, actionsim.Components()), true
	}
	return sim, &fleetrunner.Deps{Session: sim, Components: actionsim.Components(), Delay: fleetrunner.RealDelay}
}

func count(rows []actionsim.Stack, id int32) int {
	n := 0
	for _, r := range rows {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

var falBank = tile.Tile{X: 3013, Z: 3355}

// The trip's load: the crafted runes, the leftovers and junk banked, the talisman kept, 27 essence drawn.
func TestVisitBanksTheRunesAndDrawsTheLoad(t *testing.T) {
	pack := []actionsim.Stack{{ID: airTal, Count: 1}, {ID: 556, Count: 54}, {ID: essID, Count: 1}, {ID: essID, Count: 1}, {ID: 317, Count: 1}}
	bank := []actionsim.Stack{{ID: essID, Count: 300}, {ID: bodyTal, Count: 2}, {ID: 556, Count: 100}}
	sim, d := bankRig(t, pack, bank)
	res := Visit(d, falBank, Want{Talisman: airTal, Essence: true}, nil)
	if !res.Opened || res.Err != "" || res.Quick {
		t.Fatalf("%+v", res)
	}
	p := sim.Pack()
	if count(p, airTal) != 1 || count(p, essID) != 27 || len(p) != 28 {
		t.Fatalf("the pack is the talisman and a load: %v", p)
	}
	b := sim.Bank()
	if count(b, essID) != 300+2-27 || count(b, 556) != 154 || count(b, 317) != 1 || count(b, bodyTal) != 2 {
		t.Fatalf("bank %v", b)
	}
	if res.PackEssence != 27 || res.Essence != 275 || !res.Held[runecraftplan.KeyAir] || res.Talisman[runecraftplan.KeyBody] != 2 || res.Deposited != 3 {
		t.Errorf("result %+v", res)
	}
}

// The talisman is drawn when the pack has none (and the wrong one banked).
func TestVisitDrawsTheTalismanTheLevelNeeds(t *testing.T) {
	pack := []actionsim.Stack{{ID: airTal, Count: 1}, {ID: 559, Count: 27}}
	bank := []actionsim.Stack{{ID: essID, Count: 100}, {ID: bodyTal, Count: 2}}
	sim, d := bankRig(t, pack, bank)
	res := Visit(d, falBank, Want{Talisman: bodyTal, Essence: true}, nil)
	if res.Err != "" || !res.Held[runecraftplan.KeyBody] || res.Held[runecraftplan.KeyAir] {
		t.Fatalf("%+v", res)
	}
	p := sim.Pack()
	if count(p, bodyTal) != 1 || count(p, airTal) != 0 || count(p, essID) != 27 {
		t.Fatalf("pack %v", p)
	}
	if b := sim.Bank(); count(b, bodyTal) != 1 || count(b, airTal) != 1 || count(b, 559) != 27 {
		t.Fatalf("bank %v", b)
	}
}

// The teleport kit comes before the essence, so a trip with the kit carries a few essence less.
func TestVisitDrawsTheKitBeforeTheEssence(t *testing.T) {
	pack := []actionsim.Stack{{ID: airTal, Count: 1}}
	bank := []actionsim.Stack{{ID: essID, Count: 300}, {ID: 563, Count: 40}, {ID: 555, Count: 40}, {ID: 556, Count: 900}}
	sim, d := bankRig(t, pack, bank)
	res := Visit(d, falBank, Want{Talisman: airTal, Essence: true, Kit: runecraftplan.KitDraw}, nil)
	if res.Err != "" {
		t.Fatalf("%+v", res)
	}
	p := sim.Pack()
	if count(p, 563) != 2 || count(p, 555) != 2 || count(p, 556) != 10 || count(p, essID) != 24 || len(p) != 28 {
		t.Fatalf("pack %v", p)
	}
	if res.PackKit != runecraftplan.KitDraw || res.PackEssence != 24 || res.Kit.Law != 38 || res.Kit.Air != 890 {
		t.Errorf("result %+v", res)
	}
}

// What the bank lacks is reported, not invented: no talisman in the bank leaves it unheld; no essence leaves
// the pack without any.
func TestVisitReportsWhatTheBankLacks(t *testing.T) {
	_, d := bankRig(t, []actionsim.Stack{{ID: 556, Count: 9}}, []actionsim.Stack{{ID: essID, Count: 300}})
	res := Visit(d, falBank, Want{Talisman: bodyTal, Essence: true}, nil)
	if res.Held[runecraftplan.KeyBody] || res.PackEssence != 28 || res.Essence != 272 {
		t.Fatalf("no talisman in the bank (the essence fills the pack): %+v", res)
	}
	sim2, d2 := bankRig(t, nil, []actionsim.Stack{{ID: bodyTal, Count: 1}})
	res = Visit(d2, falBank, Want{Talisman: bodyTal, Essence: true}, nil)
	if !res.Held[runecraftplan.KeyBody] || res.PackEssence != 0 || res.Essence != 0 {
		t.Fatalf("no essence in the bank: %+v", res)
	}
	if count(sim2.Pack(), bodyTal) != 1 {
		t.Errorf("pack %v", sim2.Pack())
	}
}

// The quick visit: standing where the bank last opened with the talisman in hand and only essence to draw, the
// booth op, the deposit, the Withdraw-All and the close are one burst; the full visit teaches it.
func TestVisitIsQuickWhereTheBankLastOpened(t *testing.T) {
	pack := []actionsim.Stack{{ID: airTal, Count: 1}, {ID: 556, Count: 54}}
	bank := []actionsim.Stack{{ID: essID, Count: 300}}
	sim, d := bankRig(t, pack, bank)
	sim.BoothOpens = true
	stand := tile.Tile{X: 3013, Z: 3355}
	sim.StandAt(stand.X, stand.Z)
	var q mastercraftrunner.QuickBank
	q.Teach(locindex.Match{Loc: locindex.Loc{ID: 2213, X: 3013, Z: 3356}, Op: 2}, stand, actionsim.SideInv)
	res := Visit(d, falBank, Want{Talisman: airTal, Essence: true}, &q)
	if !res.Quick || res.Err != "" {
		t.Fatalf("%+v", res)
	}
	p := sim.Pack()
	if count(p, airTal) != 1 || count(p, essID) != 27 || count(p, 556) != 0 {
		t.Fatalf("pack %v", p)
	}
	if b := sim.Bank(); count(b, essID) != 273 || count(b, 556) != 54 {
		t.Fatalf("bank %v", b)
	}
	if res.PackEssence != 27 || res.Essence != 273 {
		t.Errorf("result %+v", res)
	}
	// Away from the booth, or with the kit to draw, or without the talisman in hand: the full visit.
	sim.StandAt(stand.X+3, stand.Z)
	if r := Visit(d, falBank, Want{Talisman: airTal, Essence: true}, &q); r.Quick || !r.Opened {
		t.Errorf("away from the booth: %+v", r)
	}
	sim.StandAt(stand.X, stand.Z)
	q.Teach(locindex.Match{Loc: locindex.Loc{ID: 2213, X: 3013, Z: 3356}, Op: 2}, stand, actionsim.SideInv)
	if r := Visit(d, falBank, Want{Talisman: airTal, Essence: true, Kit: runecraftplan.KitDraw}, &q); r.Quick {
		t.Errorf("with the kit: %+v", r)
	}
}
