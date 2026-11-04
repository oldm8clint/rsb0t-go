package bot

import (
	"strings"
	"testing"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/tile"
)

func newNecklaceRig(t *testing.T) *jewelRig {
	cfg := chestCfg(t)
	return newJewelRig(t, []actionsim.Stack{{ID: itemID(t, cfg, "Necklace mould", false), Count: 1}, {ID: chestCoins, Count: 5000}}, jewelStock())
}

func (jr *jewelRig) standingAtTheFurnace() (n int) {
	for _, at := range jr.enchantAt {
		if at == (tile.Tile{X: jewelFurnaceStand.X, Z: jewelFurnaceStand.Z}) {
			n++
		}
	}
	return n
}

func (jr *jewelRig) deps() *fleetrunner.Deps { return jr.b.deps(jr.ctx) }

// THE ROUND with the walking enchants: a load of ten is crafted at the furnace and walked to the bank, four casts
// landing on the way; the six left are cast standing at the bank before the deposit; nothing is enchanted at the
// furnace. Every piece is counted once: the batch is made, owed and handed over in full.
func TestJewelleryRoundEnchantsOnTheWalkToTheBank(t *testing.T) {
	jr := newNecklaceRig(t)
	jr.walkCasts = 4
	out := jr.round(t, newBatch(25))
	if out.Produced != 25 || out.Handed != 25 || out.Transient || out.HandoverFailed {
		t.Fatalf("%+v\n%s", out, strings.Join(jr.logs, "\n"))
	}
	// Three loads (10, 10, 5): every walk casts 4.
	if jr.enchanted != 25-12 {
		t.Errorf("%d casts standing, want 13 (25 less the 12 cast on the walks)", jr.enchanted)
	}
	if n := jr.standingAtTheFurnace(); n != 0 {
		t.Errorf("%d enchant step(s) at the furnace: %v", n, jr.enchantAt)
	}
	led := jr.b.ledger.Jewellery
	if led.OwedTotal() != 0 || led.Made != 25 || led.Handed != 25 || led.Under() {
		t.Errorf("ledger %+v", led)
	}
	if jr.inBank(t, "Games necklace(8)") != 0 || jr.inBank(t, "Sapphire necklace") != 0 || len(jr.sim.Pack()) != 0 {
		t.Errorf("bank %v pack %v", jr.sim.Bank(), jr.sim.Pack())
	}
	if jr.walkTakes != 3 {
		t.Errorf("%d armed walks, want one a load", jr.walkTakes)
	}
	for _, want := range []string{
		"jewellery: walk enchant: 4 cast(s) on the walk to the Falador west bank (4 sent, +700 Magic XP over 12 tick(s)), 6 left to cast there",
		"jewellery: walk enchant: 4 cast(s) on the walk to the Falador west bank (4 sent, +700 Magic XP over 12 tick(s)), 1 left to cast there",
		"jewellery: walk enchant totals: 12 of the round's 25 enchant(s) cast on 3 walk(s) (12 cast(s) sent), 13 cast standing",
		"jewellery: enchanting:",
	} {
		if jr.logged(want) == 0 {
			t.Errorf("no log line %q in:\n%s", want, strings.Join(jr.logs, "\n"))
		}
	}
}

// Nothing is lost when the walk casts nothing (the poll never found a clear tick): the bank step enchants every
// piece standing, and the ledger counts each once.
func TestJewelleryRoundEnchantsStandingAtTheBankWhenTheWalkCastsNothing(t *testing.T) {
	jr := newNecklaceRig(t)
	out := jr.round(t, newBatch(25))
	if out.Produced != 25 || out.Handed != 25 || jr.enchanted != 25 || jr.standingAtTheFurnace() != 0 {
		t.Fatalf("%+v enchanted standing %d at the furnace %d\n%s", out, jr.enchanted, jr.standingAtTheFurnace(), strings.Join(jr.logs, "\n"))
	}
	if jr.logged("walk enchant: 0 cast(s) on the walk to the Falador west bank (0 sent") != 3 || jr.logged("jewellery: walk enchant totals: 0 of the round's 25") != 1 {
		t.Errorf("walk lines:\n%s", strings.Join(jr.logs, "\n"))
	}
}

// craftWalkEnchant false: no Travel on the round's walks, the enchants are cast at the furnace as they were.
func TestJewelleryRoundEnchantsAtTheFurnaceWithTheWalkSettingOff(t *testing.T) {
	jr := newNecklaceRig(t)
	jr.b.set.WalkEnchant = false
	jr.walkCasts = 4
	out := jr.round(t, newBatch(25))
	if out.Produced != 25 || out.Handed != 25 || jr.enchanted != 25 || jr.walkTakes != 0 {
		t.Fatalf("%+v enchanted standing %d walks taken %d\n%s", out, jr.enchanted, jr.walkTakes, strings.Join(jr.logs, "\n"))
	}
	if jr.standingAtTheFurnace() != 3 {
		t.Errorf("%d standing enchants at the furnace, want one a load: %v", jr.standingAtTheFurnace(), jr.enchantAt)
	}
	if jr.logged("walk enchant") != 0 {
		t.Errorf("walk enchant lines with the setting off:\n%s", strings.Join(jr.logs, "\n"))
	}
	if jr.deps().Travel != nil {
		t.Error("the round left a Travel on the deps")
	}
}

// A refusal on the walk (the level, the members world) ends the round's enchanting at once, as the standing step's
// would: the pieces in the pack are not asked for again.
func TestJewelleryRoundStopsEnchantingWhenTheWalkIsRefused(t *testing.T) {
	jr := newNecklaceRig(t)
	jr.walkCasts, jr.walkReason = 0, "level"
	out := jr.round(t, newBatch(25))
	if out.Produced != 0 || jr.enchanted != 0 || jr.standingAtTheFurnace() != 0 {
		t.Fatalf("%+v standing %d\n%s", out, jr.enchanted, strings.Join(jr.logs, "\n"))
	}
	if jr.logged("Lvl-1 Enchant was refused (level): no more enchants this round") != 1 {
		t.Errorf("no refusal line:\n%s", strings.Join(jr.logs, "\n"))
	}
}

// Casts on the walk that never show end the walking enchants for the rest of the round: the next loads are enchanted
// standing, at the furnace, as before.
func TestJewelleryRoundEnchantsStandingAfterTheWalkStalled(t *testing.T) {
	jr := newNecklaceRig(t)
	jr.walkReason = "stalled"
	out := jr.round(t, newBatch(25))
	if out.Produced != 25 || out.Handed != 25 {
		t.Fatalf("%+v\n%s", out, strings.Join(jr.logs, "\n"))
	}
	if jr.walkTakes != 1 || jr.logged("the casts on the walk never showed: enchanting standing for the rest of the round") != 1 {
		t.Errorf("walks taken %d:\n%s", jr.walkTakes, strings.Join(jr.logs, "\n"))
	}
	// After the stall the view no longer sends the pieces for a walk: they are cast where the account stands, the
	// furnace, as before the walking enchants.
	if jr.standingAtTheFurnace() != 2 {
		t.Errorf("%d standing enchants at the furnace, want the two loads after the stall: %v", jr.standingAtTheFurnace(), jr.enchantAt)
	}
}

// Rings of dueling are cast on the walk too (Lvl-2 Enchant, three Air runes a cast).
func TestJewelleryRoundEnchantsRingsOnTheWalk(t *testing.T) {
	cfg := chestCfg(t)
	jr := newJewelRig(t, []actionsim.Stack{{ID: itemID(t, cfg, "Ring mould", false), Count: 1}, {ID: chestCoins, Count: 5000}},
		map[string]int{"Gold bar": 165, "Emerald": 30051, "Cosmic rune": 4917, "Air rune": 52221})
	jr.kind, jr.walkCasts = mastercraftplan.KindRing, 5
	dec := mastercraftplan.Decision{Mode: mastercraftplan.ModeJewellery, Kind: mastercraftplan.KindRing, Why: "the mule holds no Ring of dueling",
		Jewellery: mastercraftplan.JewelleryPlan{Kind: mastercraftplan.KindRing, N: 20}}
	out := jr.round(t, dec)
	if out.Produced != 20 || out.Handed != 20 || jr.enchanted != 10 || jr.standingAtTheFurnace() != 0 {
		t.Fatalf("%+v standing %d at the furnace %d\n%s", out, jr.enchanted, jr.standingAtTheFurnace(), strings.Join(jr.logs, "\n"))
	}
	if jr.logged("jewellery: walk enchant: 5 cast(s) on the walk to the Falador west bank (5 sent, +875 Magic XP over 15 tick(s)), 5 left to cast there") != 2 {
		t.Errorf("walk lines:\n%s", strings.Join(jr.logs, "\n"))
	}
}
