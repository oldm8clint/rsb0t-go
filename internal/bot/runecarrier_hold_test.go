package bot

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// carrierhold-1017: a carrier in ScarabSwarm's Stage 5 quest hold could not start its shift. The hold waits at the nearest bank, a
// Varrock quest's is Varrock's west bank, and the carrier's safe point wanted the account on the mule's side of White Wolf Mountain or
// near the post. Live 17 Oct 2026 14:47 to 15:13: six lines of "a shift is due (the gate is open, the mule holds 152466 Rune
// essence) but ScarabSwarm is not at a safe point: not on the mule's side of White Wolf Mountain or near its post - carrying on",
// no shift. The tests below stand the rig where that account stood (the bank's square in Varrock, (3182,3433)).

var (
	varrockWestBank  = tile.Tile{X: 3185, Z: 3436}
	varrockWestStand = tile.Tile{X: 3182, Z: 3433}
	carrierPost      = tile.Tile{X: 2809, Z: 3441}
)

// postWalk is the walk the rig records to the mule's post as the heartbeat of its mule has it, plain or across the water.
func (c *carrierRig) postWalk(across bool) string {
	post := scarabMulePresenceOf(c.ctx, "mule").Post
	if across {
		return fmt.Sprintf("across %d,%d/3", post.X, post.Z)
	}
	return fmt.Sprintf("%d,%d/3", post.X, post.Z)
}

// acrossLog is what the two I/O seams of a shift that starts across the water saw.
type acrossLog struct {
	draws []string
	bank  int // coins the bank holds
}

// standAcross puts the rig's account at `at` and replaces the walk across the water and the draw of the fares: the walk records itself
// among the rig's walks (as "across x,z/radius") and arrives, the draw tops the pack up from a bank of bankCoins.
func (c *carrierRig) standAcross(t *testing.T, at tile.Tile, bankCoins int) *acrossLog {
	t.Helper()
	oldWalk, oldDraw := carrierWalkAcross, carrierDrawFares
	t.Cleanup(func() { carrierWalkAcross, carrierDrawFares = oldWalk, oldDraw })
	l := &acrossLog{bank: bankCoins}
	carrierWalkAcross = func(_ *fleetrunner.Deps, dest tile.Tile, radius int, _ time.Duration) bool {
		c.walks = append(c.walks, fmt.Sprintf("across %d,%d/%d", dest.X, dest.Z, radius))
		c.sim.StandAt(dest.X, dest.Z)
		return true
	}
	carrierDrawFares = func(d *fleetrunner.Deps, bank tile.Tile, want int) (int, string) {
		l.draws = append(l.draws, fmt.Sprintf("%d,%d/%d", bank.X, bank.Z, want))
		if give := min(want-runecraftrunner.Coins(d), l.bank); give > 0 {
			c.sim.Give(coinsObj, give)
			l.bank -= give
		}
		return runecraftrunner.Coins(d), ""
	}
	c.sim.StandAt(at.X, at.Z)
	return l
}

// The place rule. A bank on the far side of the water counts only while ScarabSwarm's quest hold has the account settled there and
// idle (swarmHold.idlingAtBank, asked for the window scarabplan.HoldSettledFresh) and the account is still in that bank's square.
func TestCarrierSafePointPlaceRuleAcceptsAHoldSettledAtABank(t *testing.T) {
	type tc struct {
		name string
		at   tile.Tile
		idle bool
		far  bool
	}
	for _, c := range []tc{
		// What the rule was before: the mule's side of the mountain, or a short walk from the post.
		{"the post", carrierPost, false, false},
		{"East Ardougne's south bank", tile.Tile{X: 2655, Z: 3286}, false, false},
		{"20 tiles from the post", tile.Tile{X: 2829, Z: 3441}, false, false},
		{"Taverley side, no hold", tile.Tile{X: 2900, Z: 3450}, false, true},
		// The live account.
		{"Varrock's west bank, hold idle", varrockWestStand, true, false},
		{"Varrock's west bank, the swarm not idle (a quest under way)", varrockWestStand, false, true},
		{"Varrock, 23 tiles from the bank, hold idle", tile.Tile{X: 3208, Z: 3424}, true, true},
		// Every bank the hold can settle an account at on the far side of the water.
		{"Draynor's bank", tile.Tile{X: 3093, Z: 3243}, true, false},
		{"Varrock's east bank", tile.Tile{X: 3253, Z: 3420}, true, false},
		{"Edgeville's bank", tile.Tile{X: 3094, Z: 3491}, true, false},
		{"Falador's east bank", tile.Tile{X: 3013, Z: 3355}, true, false},
		{"Falador's west bank", tile.Tile{X: 2946, Z: 3368}, true, false},
		{"Al Kharid's bank", tile.Tile{X: 3269, Z: 3167}, true, false},
		{"Draynor's bank, the swarm not idle", tile.Tile{X: 3093, Z: 3243}, false, true},
	} {
		cc, sw := newCarrierBotRig(t, nil)
		cc.sim.StandAt(c.at.X, c.at.Z)
		sw.idle = c.idle
		if got := cc.b.farFromMule(cc.ctx); got != c.far {
			t.Errorf("%s (%v, hold idle %v): farFromMule = %v, want %v", c.name, c.at, c.idle, got, c.far)
		}
		for _, within := range sw.asked {
			if within != fleetplan.HoldSettledFresh {
				t.Errorf("%s: the hold was asked about the last %s, want %s", c.name, within, fleetplan.HoldSettledFresh)
			}
		}
	}
	// A swarm that cannot say (anything but the real one and a test's that does) never opens the rule.
	cc, _ := newCarrierBotRig(t, nil)
	cc.b.swarm = &settingsSwarm{}
	cc.sim.StandAt(varrockWestStand.X, varrockWestStand.Z)
	if !cc.b.farFromMule(cc.ctx) {
		t.Errorf("a swarm with no hold to read opened the place rule at Varrock's west bank")
	}
}

// The live account, replayed through Loop: the gate open, the mule full, ScarabSwarm idle at Varrock's west bank. The shift starts (it
// is stopped first, run, and started again); the same account in a quest at that bank keeps the old answer and the old log line.
func TestCarrierLoopStartsAShiftFromTheBankTheQuestHoldWaitsAt(t *testing.T) {
	c, sw := newCarrierBotRig(t, nil)
	c.openGate(mulebus.GateOpen, 0)
	c.sim.StandAt(varrockWestStand.X, varrockWestStand.Z)

	// A quest under way at the bank: the old rule, the log line the live fleet printed.
	if err := c.b.Loop(c.ctx); err != nil || strings.Join(c.events, ",") != "loop" || c.b.pending == "" {
		t.Fatalf("events %v pending %q", c.events, c.b.pending)
	}
	if !c.logged("not at a safe point: not on the mule's side of White Wolf Mountain or near its post") {
		t.Fatalf("log: %v", c.logs)
	}

	// The hold settles: the next Loop starts the shift.
	sw.idle = true
	c.events = nil
	if err := c.b.Loop(c.ctx); err != nil || strings.Join(c.events, ",") != "stop,shift,start" {
		t.Fatalf("events %v\n%v", c.events, c.logs)
	}
	if !c.logged("SHIFT starting at 3182,3433,0") {
		t.Fatalf("log: %v", c.logs)
	}
}

// The shift's first stage from there. The pack holds no coins and the ships cost 60: the fares are drawn from the nearest bank first
// (scarabplan.FareFloat of them), the walk to the post is the plain ScarabSwarm walk (the dens and the bears; WalkKaramja's boxes cannot
// be crossed, see runecraftrunner's real-pack test), and the mule is asked at the post as always.
func TestCarrierAskKitFromAcrossTheWaterDrawsTheFaresAndWalksThePlainRoute(t *testing.T) {
	c := newCarrierRig(t, nil, map[string]int{"Rune essence": 20000, "Ring of dueling(8)": 35})
	c.openGate(mulebus.GateOpen, 0)
	c.serveKit(3000, 10000, true)
	l := c.standAcross(t, varrockWestStand, 5000)
	w := c.world()
	if res := w.askKit(); !res.OK {
		t.Fatalf("%+v\n%v", res, c.logs)
	}
	if want := []string{fmt.Sprintf("%d,%d/%d", varrockWestBank.X, varrockWestBank.Z, fleetplan.FareFloat)}; strings.Join(l.draws, ";") != strings.Join(want, ";") {
		t.Fatalf("fare draws %v, want %v", l.draws, want)
	}
	if got := strings.Join(c.walks, ";"); got != c.postWalk(true) {
		t.Fatalf("walks %q, want %q: the corridor walk (carrierWalk) must not be the one from across the water", got, c.postWalk(true))
	}
	if !c.logged("fares: 0 of 60 coins for the ships from Asgarnia/Misthalin to Kandarin; drew at the Varrock west bank: carrying 600") ||
		!c.logged("is across the water from 3182,3433,0: walking there by the ships") {
		t.Fatalf("log: %v", c.logs)
	}
	if !w.kitAsked || runecraftrunner.Notes(w.d) != 3000 || runecraftrunner.Coins(w.d) != 10600 {
		t.Fatalf("asked %v notes %d coins %d", w.kitAsked, runecraftrunner.Notes(w.d), runecraftrunner.Coins(w.d))
	}
}

// Fares already in the pack are not drawn again; a bank that has no coins ends the shift as a failed one (it backs off) without a step
// of the walk, a claim on the mule's essence, or a kit ask: a walk that is not paid for is "unreachable" and a shift on it a wasted one.
func TestCarrierAskKitFromAcrossTheWaterNeedsTheFares(t *testing.T) {
	c := newCarrierRig(t, nil, map[string]int{"Rune essence": 20000, "Ring of dueling(8)": 35})
	c.openGate(mulebus.GateOpen, 0)
	c.serveKit(3000, 10000, true)
	l := c.standAcross(t, varrockWestStand, 5000)
	c.sim.Give(coinsObj, 80)
	if res := c.world().askKit(); !res.OK {
		t.Fatalf("%+v\n%v", res, c.logs)
	}
	if len(l.draws) != 0 || strings.Join(c.walks, ";") != c.postWalk(true) {
		t.Fatalf("with 80 coins in the pack: draws %v walks %v", l.draws, c.walks)
	}

	// The bank is empty and the pack holds 20 coins.
	c2 := newCarrierRig(t, nil, map[string]int{"Rune essence": 20000, "Ring of dueling(8)": 35})
	c2.openGate(mulebus.GateOpen, 0)
	c2.serveKit(3000, 10000, true)
	l2 := c2.standAcross(t, varrockWestStand, 0)
	c2.sim.Give(coinsObj, 20)
	out := c2.b.liveShift(c2.ctx, "test", false)
	if out.Trips != 0 || !out.Failed || !strings.Contains(out.Why, "the ships to the mule's post cost 60 coins and the pack holds 20 (the Varrock west bank has no more)") {
		t.Fatalf("%+v\n%v", out, c2.logs)
	}
	if len(c2.walks) != 0 || len(c2.kitCalls) != 0 || len(l2.draws) != 1 {
		t.Fatalf("walks %v kit calls %v draws %v: nothing is walked or asked when the fares cannot be paid", c2.walks, c2.kitCalls, l2.draws)
	}
	if got := carrierClaims.others("someone else", time.Now(), time.Time{}); got != 0 {
		t.Fatalf("a shift that never set out holds a claim on the mule's essence: %d", got)
	}
	c2.b.afterShift(c2.ctx, out)
	if c2.b.fails != 1 || !c2.b.backoffUntil.After(time.Now()) {
		t.Fatalf("fails %d until %v: the shift is not backed off", c2.b.fails, c2.b.backoffUntil)
	}
}

// A shift that starts on the mule's side of the mountain is exactly what it was: no fares, the corridor walk.
func TestCarrierAskKitOnTheMulesSideStillWalksTheCorridorWalk(t *testing.T) {
	for _, at := range []tile.Tile{{X: 2655, Z: 3286}, {X: 2700, Z: 3300}, carrierPost, {X: 2725, Z: 3491}} {
		c := newCarrierRig(t, nil, map[string]int{"Rune essence": 20000, "Ring of dueling(8)": 35})
		c.openGate(mulebus.GateOpen, 0)
		c.serveKit(3000, 10000, true)
		l := c.standAcross(t, at, 5000)
		if res := c.world().askKit(); !res.OK {
			t.Fatalf("from %v: %+v\n%v", at, res, c.logs)
		}
		if len(l.draws) != 0 || strings.Join(c.walks, ";") != c.postWalk(false) {
			t.Errorf("from %v: draws %v walks %v, want no draw and the corridor walk to the post (%s)", at, l.draws, c.walks, c.postWalk(false))
		}
		if c.logged("across the water") || c.logged("fares:") {
			t.Errorf("from %v: the log talks of the ships: %v", at, c.logs)
		}
	}
}

// The production bindings of the two seams: the walk across the water is ScarabSwarm's own (not WalkKaramja, whose boxes leave no route
// from the mainland) and the fares come out of the bank with the fleet's one coin draw. Tests replace both in their rigs and put them
// back, so this reads what the shift runs with.
func TestCarrierAcrossTheWaterSeamsAreTheProductionOnes(t *testing.T) {
	ptr := func(f any) uintptr { return reflect.ValueOf(f).Pointer() }
	if ptr(carrierWalkAcross) != ptr(walkAcrossByWalker) {
		t.Error("carrierWalkAcross is not walkAcrossByWalker")
	}
	// Without a planner the walk across is refused, never WalkTo's plain-pathfinding fallback over the mountain's pass.
	if walkAcrossByWalker(&fleetrunner.Deps{}, tile.Tile{X: 2809, Z: 3441}, 3, time.Second) {
		t.Error("walkAcrossByWalker walked without a planner")
	}
	if ptr(carrierWalkAcross) == ptr(carrierWalk) || ptr(carrierWalk) != ptr(runecraftrunner.WalkKaramja) {
		t.Error("the shift's own walk is not the corridor walk any more, or the walk across the water is the same function")
	}
	if ptr(carrierDrawFares) != ptr(fleetrunner.WithdrawCoinsTo) {
		t.Error("carrierDrawFares is not scarabrunner.WithdrawCoinsTo")
	}
}
