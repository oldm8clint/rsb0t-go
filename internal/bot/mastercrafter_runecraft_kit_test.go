package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The teleport kit rides the bank visit before a LONG walk and nothing else: at the mule's post (175 tiles from the Air
// ruins) the visit draws the Falador Teleport's and the Camelot Teleport's runes with the talisman and a load of 24
// essence; the visits by the Falador bank (81 steps from the ruins) draw 27 essence and no kit; the visit that ends the
// round, at Edgeville, 285 tiles from the post, draws the Camelot kit for the walk home. The walks themselves are the
// test's stub (it moves the account, it casts nothing), so the kit stays in the pack and the next visit banks it.
//
// Needs the real data (RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK); without it this skips.
func TestTheLiveRoundDrawsTheTeleportKitOnlyBeforeLongWalks(t *testing.T) {
	r, e := liveRig(t)
	set := r.b.set
	set.Rune.On, set.Rune.LevelTo, set.Rune.Teleport, set.Rune.Round = true, 21, true, 30*time.Minute
	r.b.set = set
	mindHoldStat(e.sim.State(), mastercraftrunner.MagicStat, levels.XPAtLevel(78), 78)
	e.sim.OpenBank([]actionsim.Stack{
		{ID: runecraftplan.EssenceID, Count: 5000},
		{ID: int32(runecraftplan.Air.TalismanID), Count: 1}, {ID: int32(runecraftplan.Body.TalismanID), Count: 1},
		{ID: runecraftrunner.LawRuneID, Count: 50}, {ID: runecraftrunner.WaterRuneID, Count: 50}, {ID: runecraftrunner.AirRuneID, Count: 200},
	})
	post := tile.Tile{X: 2809, Z: 3441}
	r.ctx.CurrentWorld = 1
	r.ctx.Peers = peerbank.New()
	r.ctx.Peers.Publish("mule", post, true, nil, nil, false)
	r.ctx.Bus.Publish(mulebus.OnWorld(mulebus.Heartbeat("mule", tile.Tile{X: 2809, Z: 3440}, 20, mulebus.StateHome, nil), 1))
	e.mu.Lock()
	e.xpT = (levels.XPAtLevel(20) - 300) * 10
	e.setStat()
	e.moveTo(tile.Tile{X: 2820, Z: 3440})
	e.mu.Unlock()
	time.Sleep(3 * liveTick)

	out := r.b.runecraftRound(r.ctx, r.b.deps(r.ctx), time.Now(), mastercraftplan.Decision{Mode: mastercraftplan.ModeRunecraft, Why: "test"})
	logs := strings.Join(r.logs, "\n")
	if out.Transient || out.Produced < 100 {
		t.Fatalf("%+v\n%s", out, logs)
	}
	var banks []string
	for _, l := range r.logs {
		if strings.Contains(l, "runecraft: bank (") {
			banks = append(banks, l)
		}
	}
	if len(banks) < 3 {
		t.Fatalf("%d bank visits:\n%s", len(banks), logs)
	}
	// The first visit is at the post, and draws the kit; the rest, by the ruins, do not.
	if !strings.Contains(banks[0], "mule's post") || !strings.Contains(banks[0], "pack now 24 essence, teleport kit 2 Law 2 Water 10 Air") {
		t.Errorf("the first visit: %s", banks[0])
	}
	for i, b := range banks[1:] {
		if strings.Contains(b, "teleport kit") || !strings.Contains(b, "pack now 27 essence") {
			t.Errorf("visit %d drew a kit or short of a load: %s", i+1, b)
		}
	}
	// The end of the round: the pack banked at Edgeville and the Camelot kit (1 Law, 5 Air) drawn for the walk home.
	if !strings.Contains(out.Why, "banked the pack at Edgeville; home at the mule's post") {
		t.Errorf("the end: %s", out.Why)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.packCount(runecraftrunner.LawRuneID) != 1 || e.packCount(runecraftrunner.AirRuneID) != 5 || e.packCount(runecraftrunner.WaterRuneID) != 0 {
		t.Errorf("the home kit in the pack: %v", e.sim.Pack())
	}
	if e.packCount(runecraftplan.EssenceID) != 0 {
		t.Errorf("essence left in the pack: %v", e.sim.Pack())
	}
}
