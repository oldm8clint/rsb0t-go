package bot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/lootchestfisherrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/tile"
)

// jewelRig is a MasterCrafter standing at the mule's post over the bank-and-pack simulator with the real
// item config, the mule heard and publishing stock, and every piece of heavy I/O of the jewellery
// round (the mule's kit, the trade, the walks, the furnace, the spell, the shop, the teleport) a fake
// that moves the simulated pack the way the server would. The round itself, its view, its bank
// bookkeeping, its ledger and its steps are the real ones.
type jewelRig struct {
	*masterRig
	cfg  *cachecfg.Config
	mule map[string]int

	walks, casts, deliveries, phases []string
	asked                            []fleetplan.ItemCount
	falVisits                        int
	made, enchanted, bought          int
	// buy says whether the shop sells a mould; makeFails makes the furnace refuse every use.
	buy, makeFails bool
	// tele says whether a teleport lands (it needs its runes in the pack either way).
	tele bool
	// kind is what the batch makes (the walk's simulated casts need it). walkCasts is how many casts a walk that
	// carries pieces lands (0: the walk casts nothing), walkReason what the walk's enchanting reports besides;
	// pendingWalk is the result the next jewelleryWalkTake returns; enchantAt is where each standing enchant was
	// made; walkTakes counts the walks that were armed.
	kind        mastercraftplan.JewelleryKind
	walkCasts   int
	walkReason  string
	pendingWalk fleetrunner.EnchantWalkResult
	enchantAt   []tile.Tile
	walkTakes   int
	castOnWalk  func() fleetrunner.EnchantWalkResult
}

var (
	jewelFurnaceStand = tile.Tile{X: 2974, Z: 3368}
	faladorLanding    = tile.Tile{X: 2966, Z: 3379}
)

func newJewelRig(t *testing.T, bank []actionsim.Stack, stock map[string]int) *jewelRig {
	t.Helper()
	cfg := chestCfg(t)
	r, _ := chestRig(t, nil, bank, stock)
	mindHoldStat(r.sim.State(), mastercraftrunner.MagicStat, levels.XPAtLevel(40), 40)
	mindHoldStat(r.sim.State(), gemrunner.CraftingStat, levels.XPAtLevel(92), 92)
	r.sim.StandAt(atThePost.X, atThePost.Z)
	jr := &jewelRig{masterRig: r, cfg: cfg, mule: stock, buy: true, tele: true}
	id := func(name string) int32 {
		if name == "Coins" {
			return chestCoins // content has two coin objects (617 and 995): the banks hold 995
		}
		return itemID(t, cfg, name, false)
	}
	note := func(name string) int32 {
		for _, i := range cfg.ObjIDs(name) {
			if o, ok := cfg.Obj(i); ok && o.Noted() {
				return int32(i)
			}
		}
		return id(name)
	}
	nameOf := func(i int32) string { o, _ := cfg.Obj(int(i)); return o.Name }
	count := func(name string) int { return simCount(r.sim.Pack(), id(name)) }

	oldWait := jewelleryRetryWait
	jewelleryRetryWait = 0
	t.Cleanup(func() { jewelleryRetryWait = oldWait })
	oldTake := jewelleryWalkTake
	t.Cleanup(func() { jewelleryWalkTake = oldTake })
	oldFal, oldWalk, oldKit, oldDeliver := chestOpenFaladorBank, masterWalkTo, scarabKitBundle, masterDeliver
	oldFind, oldApproach, oldReached, oldMake, oldEnchant, oldBuy, oldTele := jewelleryFind, jewelleryApproach, jewelleryReached, jewelleryMake, jewelleryEnchant, jewelleryBuy, jewelleryTeleport
	t.Cleanup(func() {
		chestOpenFaladorBank, masterWalkTo, scarabKitBundle, masterDeliver = oldFal, oldWalk, oldKit, oldDeliver
		jewelleryFind, jewelleryApproach, jewelleryReached, jewelleryMake, jewelleryEnchant, jewelleryBuy, jewelleryTeleport = oldFind, oldApproach, oldReached, oldMake, oldEnchant, oldBuy, oldTele
	})
	chestOpenFaladorBank = func(*lootchestfisherrunner.Deps, tile.Tile, int) (*actions.Bank, bool) {
		jr.falVisits++
		r.sim.OpenBank(r.sim.Bank())
		return actions.NewBank(r.sim, actionsim.Components()), true
	}
	masterWalkTo = func(d *fleetrunner.Deps, to tile.Tile, _ int, _ time.Duration) bool {
		jr.walks = append(jr.walks, itoaTile(to))
		if d.Travel.EnchantArmed() {
			jr.pendingWalk = jr.castOnWalk() // the poll of a real walk would have cast these on the way
		}
		r.sim.StandAt(to.X, to.Z)
		return true
	}
	jewelleryWalkTake = func(t *fleetrunner.Travel, d *fleetrunner.Deps) fleetrunner.EnchantWalkResult {
		jr.walkTakes++
		t.EnchantWalkTake(d) // disarm, as the real one does
		res := jr.pendingWalk
		jr.pendingWalk = fleetrunner.EnchantWalkResult{}
		return res
	}
	scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, it []fleetplan.ItemCount, o fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		jr.asked = append(jr.asked, it...)
		got := map[string]int{}
		for _, line := range it {
			n := min(line.Count, jr.mule[line.Item])
			if n <= 0 {
				continue
			}
			jr.mule[line.Item] -= n
			if o.Noted {
				r.sim.Give(note(line.Item), n)
			} else {
				r.sim.Give(id(line.Item), n)
			}
			got[line.Item] += n
		}
		return fleetrunner.KitResult{Got: got, Declined: map[string]string{}}, fleetrunner.KitBundleInfo{Bundled: true}
	}
	masterDeliver = func(_ *fleetrunner.Deps, _ *donorrunner.Deps, _ string, _ tile.Tile, _, _ int, items []string) runerunner.Delivery {
		jr.deliveries = append(jr.deliveries, strings.Join(items, ","))
		del := map[string]int{}
		for _, s := range r.sim.Pack() {
			del[nameOf(s.ID)] += r.sim.Remove(s.ID, s.Count) // the mule takes what was offered
		}
		return runerunner.Delivery{Result: donorrunner.Result{Traded: true}, Delivered: del}
	}
	jewelleryFind = func(*fleetrunner.Deps, tile.Tile, int) (locindex.Match, bool) {
		return locindex.Match{Loc: locindex.Loc{ID: 2781, X: 2975, Z: 3368}, Op: 2}, true
	}
	jewelleryApproach = func(_ *fleetrunner.Deps, _ locindex.Match, _ mastercraftrunner.WalkFunc, _ time.Duration) bool {
		jr.walks = append(jr.walks, "furnace")
		r.sim.StandAt(jewelFurnaceStand.X, jewelFurnaceStand.Z)
		return true
	}
	jewelleryReached = func(*fleetrunner.Deps, locindex.Match) bool {
		x, z, _, ok := r.sim.State().Tile()
		return ok && x == jewelFurnaceStand.X && z == jewelFurnaceStand.Z
	}
	jewelleryMake = func(_ *fleetrunner.Deps, kind mastercraftplan.JewelleryKind, _ locindex.Match, _ func() bool) mastercraftrunner.NecklaceResult {
		sp := kind.Spec()
		if jr.makeFails {
			return mastercraftrunner.NecklaceResult{Reason: "no-menu"}
		}
		n := min(count("Gold bar"), count(sp.Gem))
		for i := 0; i < n; i++ {
			r.sim.Remove(id("Gold bar"), 1)
			r.sim.Remove(id(sp.Gem), 1)
			r.sim.Give(id(sp.Base), 1)
		}
		jr.made += n
		return mastercraftrunner.NecklaceResult{Made: n, Uses: (n + 9) / 10, Reason: "done"}
	}
	// enchantUpTo turns up to max of the kind's base pieces into final ones, paying the runes, and reports how many.
	enchantUpTo := func(kind mastercraftplan.JewelleryKind, max int) int {
		sp := kind.Spec()
		k := min(max, count(sp.Base), count("Cosmic rune"))
		staff := mastercraftplan.RunesPaidByStaff(mastercraftrunner.WornNames(r.b.deps(r.ctx)), true)[sp.Second]
		if !staff {
			k = min(k, count(sp.Second)/sp.SecondPer)
		}
		for i := 0; i < k; i++ {
			r.sim.Remove(id(sp.Base), 1)
			r.sim.Give(id(sp.Final), 1)
		}
		r.sim.Remove(id("Cosmic rune"), k)
		if !staff {
			r.sim.Remove(id(sp.Second), k*sp.SecondPer)
		}
		return k
	}
	jr.castOnWalk = func() fleetrunner.EnchantWalkResult {
		k := enchantUpTo(jr.kind, jr.walkCasts)
		left := count(jr.kind.Spec().Base)
		reason := jr.walkReason
		if reason == "" && left == 0 {
			reason = "done"
		}
		return fleetrunner.EnchantWalkResult{Sent: k, Landed: k, XP: 175 * k, Ticks: 3 * k, Left: left, Reason: reason}
	}
	jewelleryEnchant = func(_ *fleetrunner.Deps, kind mastercraftplan.JewelleryKind, _ func() bool) mastercraftrunner.EnchantResult {
		sp := kind.Spec()
		x, z, level, _ := r.sim.State().Tile()
		jr.enchantAt = append(jr.enchantAt, tile.Tile{X: x, Z: z, Level: level})
		k := enchantUpTo(kind, 1<<30)
		jr.enchanted += k
		reason := "done"
		if count(sp.Base) > 0 {
			reason = "runes"
		}
		return mastercraftrunner.EnchantResult{Casts: k, Sent: k, Reason: reason}
	}
	jewelleryBuy = func(_ *fleetrunner.Deps, kind mastercraftplan.JewelleryKind, s mastercraftplan.MouldShop) (bool, string) {
		if !jr.buy || count("Coins") < 5 {
			return false, s.Keeper + " has none on the shelf"
		}
		r.sim.Remove(id("Coins"), 5)
		r.sim.Give(id(kind.Spec().Mould), 1)
		jr.bought++
		return true, ""
	}
	jewelleryTeleport = func(_ *Context, spell string) (bool, error) {
		if !jr.tele {
			return false, nil
		}
		staff := mastercraftplan.RunesPaidByStaff(mastercraftrunner.WornNames(r.b.deps(r.ctx)), true)
		for name, n := range mastercraftplan.ChestSpellRunes(spell) {
			if !staff[name] {
				r.sim.Remove(id(name), n)
			}
		}
		jr.casts = append(jr.casts, spell)
		switch spell {
		case mastercraftplan.FaladorTeleport:
			r.sim.StandAt(faladorLanding.X, faladorLanding.Z)
		default:
			r.sim.StandAt(3213, 3424) // Varrock
		}
		return true, nil
	}
	return jr
}

func itoaTile(t tile.Tile) string { return fmt.Sprintf("%d,%d", t.X, t.Z) }

func (jr *jewelRig) id(t *testing.T, name string) int32 {
	if name == "Coins" {
		return chestCoins
	}
	return itemID(t, jr.cfg, name, false)
}

func (jr *jewelRig) inBank(t *testing.T, name string) int {
	return simCount(jr.sim.Bank(), jr.id(t, name))
}

func (jr *jewelRig) inPack(t *testing.T, name string) int {
	return simCount(jr.sim.Pack(), jr.id(t, name))
}

func (jr *jewelRig) logged(sub string) int {
	n := 0
	for _, l := range jr.logs {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

func jewelStock() map[string]int {
	return map[string]int{"Gold bar": 371, "Sapphire": 59102, "Cosmic rune": 4374, "Water rune": 211521}
}

func (jr *jewelRig) round(t *testing.T, dec mastercraftplan.Decision) masterRoundOutcome {
	t.Helper()
	return jr.b.jewelleryRound(jr.ctx, jr.b.deps(jr.ctx), time.Now(), dec)
}

func newBatch(n int) mastercraftplan.Decision {
	return mastercraftplan.Decision{Mode: mastercraftplan.ModeJewellery, Jewellery: mastercraftplan.JewelleryPlan{N: n}, Why: "the mule holds none"}
}

// THE ROUND, over the real bot code and the simulated server: 25 Games necklaces from an empty bank
// with a mould in it. The ask names the ingredients and the runes as notes; loads of ten are banked,
// crafted at the furnace, enchanted and banked again; the Games necklaces go home and are handed over
// by quantity; the ledger ends with nothing owed and no batch under way.
func TestJewelleryRoundMakesAndHandsOverABatch(t *testing.T) {
	cfg := chestCfg(t)
	jr := newJewelRig(t, []actionsim.Stack{{ID: itemID(t, cfg, "Necklace mould", false), Count: 1}, {ID: chestCoins, Count: 5000}}, jewelStock())
	out := jr.round(t, newBatch(25))
	if out.Produced != 25 || out.Handed != 25 || out.Transient || !out.HandoverTried || out.HandoverFailed {
		t.Fatalf("%+v\n%s", out, strings.Join(jr.logs, "\n"))
	}
	if jr.made != 25 || jr.enchanted != 25 || jr.bought != 0 {
		t.Errorf("made %d enchanted %d bought %d", jr.made, jr.enchanted, jr.bought)
	}
	asked := map[string]int{}
	for _, it := range jr.asked {
		asked[it.Item] = it.Count
	}
	// Magic 40, no staff: the Falador Teleport's Water too (the mule has no Law, Air or Fire rune).
	if asked["Gold bar"] != 25 || asked["Sapphire"] != 25 || asked["Cosmic rune"] != 25 || asked["Water rune"] != 26 || len(asked) != 4 {
		t.Errorf("asked %v", asked)
	}
	if got := strings.Join(jr.deliveries, ";"); got != "Games necklace(8)" {
		t.Errorf("hand-overs %q, want the Games necklaces alone", got)
	}
	led := jr.b.ledger.Jewellery
	if led.OwedTotal() != 0 || led.Under() || led.Target != 0 || led.Made != 25 || led.Handed != 25 || led.Rounds != 1 || led.Enchanted != 0 {
		t.Errorf("ledger %+v", led)
	}
	if jr.inBank(t, "Games necklace(8)") != 0 || jr.inBank(t, "Sapphire necklace") != 0 || jr.inBank(t, "Gold bar") != 0 || jr.inBank(t, "Necklace mould") != 1 {
		t.Errorf("bank %v", jr.sim.Bank())
	}
	if len(jr.sim.Pack()) != 0 {
		t.Errorf("the pack holds %v after the round", jr.sim.Pack())
	}
	if jr.falVisits != 3 {
		t.Errorf("%d visits to the Falador bank: two loads of ten and one of five, each banked, then the last", jr.falVisits)
	}
	// Three loads, walked: to the furnace three times, and home.
	if n := strings.Count(strings.Join(jr.walks, " "), "furnace"); n != 3 {
		t.Errorf("walks %v", jr.walks)
	}
	if post := itoaTile(scarabMulePresenceOf(jr.ctx, "mule").Post); !strings.HasSuffix(strings.Join(jr.walks, " "), post) {
		t.Errorf("the round did not end at the post %s: %v", post, jr.walks)
	}
	for _, want := range []string{"jewellery: a batch of 25 Games necklaces", "jewellery: asked mule for", "jewellery: crafting:", "jewellery: enchanting:", "HANDOVER jewellery mule=mule", "jewellery: round over: 25 Games necklace(s) made (25 Sapphire necklace(s) crafted), 25 handed to mule, 0 still owed, 0 mould(s) bought"} {
		if jr.logged(want) == 0 {
			t.Errorf("no log line %q in:\n%s", want, strings.Join(jr.logs, "\n"))
		}
	}
	// The outcome earned no wait.
	if jr.b.ledger.Jewellery.PauseUntil != (time.Time{}) || jr.b.ledger.Fails(mastercraftplan.ModeJewellery) != 0 {
		t.Errorf("waits after a round that worked: %v %v", jr.b.ledger.Jewellery.PauseUntil, jr.b.ledger.Backoffs)
	}
}

// With law and water runes and a Staff of air, the way to the furnace is a Falador Teleport from the
// post, and the way home a Varrock Teleport from the Falador bank; no walk crosses the road between.
func TestJewelleryRoundTeleportsThereAndHome(t *testing.T) {
	cfg := chestCfg(t)
	id := func(n string) int32 { return itemID(t, cfg, n, false) }
	bank := []actionsim.Stack{{ID: id("Necklace mould"), Count: 1}, {ID: id("Law rune"), Count: 10}, {ID: id("Fire rune"), Count: 10}, {ID: id("Coins"), Count: 5000}}
	jr := newJewelRig(t, bank, jewelStock())
	jr.sim.Wear(3, id("Staff of air"))
	out := jr.round(t, newBatch(12))
	if out.Produced != 12 || out.Handed != 12 || out.Transient {
		t.Fatalf("%+v\n%s", out, strings.Join(jr.logs, "\n"))
	}
	if got := strings.Join(jr.casts, ","); got != "Falador Teleport,Varrock Teleport" {
		t.Fatalf("casts %q\n%s", got, strings.Join(jr.logs, "\n"))
	}
	// The first cast is the way to the furnace, from the post; the last is home, from the Falador bank.
	walks := strings.Join(jr.walks, " ")
	if strings.Index(walks, "furnace") < 0 {
		t.Fatalf("walks %q", walks)
	}
	if jr.inBank(t, "Law rune") != 10-2 || jr.inBank(t, "Fire rune") != 10-1 {
		t.Errorf("bank runes: law %d fire %d: one cast each", jr.inBank(t, "Law rune"), jr.inBank(t, "Fire rune"))
	}
	// The mule's ask carried the runes of both teleports (law twice, a fire, no air for the staff).
	asked := map[string]int{}
	for _, it := range jr.asked {
		asked[it.Item] = it.Count
	}
	if asked["Air rune"] != 0 {
		t.Errorf("air asked with a Staff of air: %v", asked)
	}
}

// A teleport that does not land is walked instead, and the round goes on.
func TestJewelleryRoundWalksWhenATeleportDoesNotLand(t *testing.T) {
	cfg := chestCfg(t)
	id := func(n string) int32 { return itemID(t, cfg, n, false) }
	jr := newJewelRig(t, []actionsim.Stack{{ID: id("Necklace mould"), Count: 1}, {ID: id("Law rune"), Count: 10}, {ID: id("Fire rune"), Count: 10}}, jewelStock())
	jr.sim.Wear(3, id("Staff of air"))
	jr.tele = false
	out := jr.round(t, newBatch(10))
	if out.Produced != 10 || out.Handed != 10 || len(jr.casts) != 0 {
		t.Fatalf("%+v casts %v\n%s", out, jr.casts, strings.Join(jr.logs, "\n"))
	}
	if jr.logged("did not land") == 0 {
		t.Errorf("no line saying the teleport did not land")
	}
}

// No mould anywhere: the round carries coins to the shop, buys one once, and keeps it in the bank.
func TestJewelleryRoundBuysTheMouldOnce(t *testing.T) {
	jr := newJewelRig(t, []actionsim.Stack{{ID: chestCoins, Count: 5000}}, jewelStock())
	out := jr.round(t, newBatch(23))
	if out.Produced != 23 || out.Handed != 23 || out.Transient {
		t.Fatalf("%+v\n%s", out, strings.Join(jr.logs, "\n"))
	}
	if jr.bought != 1 || jr.b.ledger.Jewellery.MouldsBought != 1 || jr.inBank(t, "Necklace mould") != 1 {
		t.Errorf("bought %d (ledger %d), %d in the bank", jr.bought, jr.b.ledger.Jewellery.MouldsBought, jr.inBank(t, "Necklace mould"))
	}
	if jr.logged("bought a Necklace mould at Rommik's Crafty Supplies") != 1 {
		t.Errorf("no line saying where it was bought")
	}
	// Coins were kept for it and banked after: the bank still holds the change.
	if got := jr.inBank(t, "Coins"); got != 5000-5 {
		t.Errorf("%d coins left, want 4995", got)
	}
}

// No mould, nothing in the bank to buy one with, and the mule is asked for coins.
func TestJewelleryRoundAsksTheMuleForCoinsForTheMould(t *testing.T) {
	stock := jewelStock()
	stock["Coins"] = 1000000
	jr := newJewelRig(t, nil, stock)
	out := jr.round(t, newBatch(10))
	asked := map[string]int{}
	for _, it := range jr.asked {
		asked[it.Item] = it.Count
	}
	if asked["Coins"] != 2*mastercraftplan.MouldCoins {
		t.Errorf("asked %v", asked)
	}
	if out.Produced != 10 || jr.bought != 1 {
		t.Fatalf("%+v bought %d\n%s", out, jr.bought, strings.Join(jr.logs, "\n"))
	}
}

// Both shops are out of moulds: the batch is not made, the bars and Sapphires stay in the bank, the
// round is a passing failure (the shops restock), and it is not left looping.
func TestJewelleryRoundWithNoMouldToBuy(t *testing.T) {
	jr := newJewelRig(t, []actionsim.Stack{{ID: chestCoins, Count: 5000}}, jewelStock())
	jr.buy = false
	out := jr.round(t, newBatch(10))
	if out.Produced != 0 || out.Handed != 0 || !out.Transient || jr.made != 0 {
		t.Fatalf("%+v\n%s", out, strings.Join(jr.logs, "\n"))
	}
	if jr.logged("could not buy a Necklace mould") != 1 {
		t.Errorf("no line saying so:\n%s", strings.Join(jr.logs, "\n"))
	}
	if jr.inBank(t, "Gold bar") != 10 || jr.inBank(t, "Sapphire") != 10 {
		t.Errorf("the ingredients must stay in the bank: %v", jr.sim.Bank())
	}
	// The wait: a passing failure backs the mode off.
	if jr.b.ledger.Fails(mastercraftplan.ModeJewellery) != 1 {
		t.Errorf("backoffs %v", jr.b.ledger.Backoffs)
	}
	// The batch stays under way (its materials are in the bank): the next round resumes it.
	if !jr.b.ledger.Jewellery.Under() {
		t.Errorf("the ledger forgot the batch: %+v", jr.b.ledger.Jewellery)
	}
}

// A furnace that will not take the use makes nothing; the step fails three times and the round wraps
// up (banks, goes home) instead of looping, as a passing failure.
func TestJewelleryRoundGivesUpOnAFurnaceThatRefuses(t *testing.T) {
	cfg := chestCfg(t)
	jr := newJewelRig(t, []actionsim.Stack{{ID: itemID(t, cfg, "Necklace mould", false), Count: 1}}, jewelStock())
	jr.makeFails = true
	out := jr.round(t, newBatch(10))
	if out.Produced != 0 || !out.Transient || out.Handed != 0 {
		t.Fatalf("%+v\n%s", out, strings.Join(jr.logs, "\n"))
	}
	if jr.logged("jewellery: craft failed (3 in a row)") != 1 || jr.logged("giving up on the rest of the batch") != 1 {
		t.Errorf("logs:\n%s", strings.Join(jr.logs, "\n"))
	}
	if post := itoaTile(scarabMulePresenceOf(jr.ctx, "mule").Post); !strings.HasSuffix(strings.Join(jr.walks, " "), post) || len(jr.sim.Pack()) != 0 {
		t.Errorf("the round did not wrap up at the post %s with an empty pack: walks %v pack %v", post, jr.walks, jr.sim.Pack())
	}
	// The ingredients are back in the bank, the batch still under way.
	if jr.inBank(t, "Gold bar") != 10 || !jr.b.ledger.Jewellery.Under() {
		t.Errorf("bank %v ledger %+v", jr.sim.Bank(), jr.b.ledger.Jewellery)
	}
}

// A resumed round from the account's own bank: 37 Sapphire necklaces and runes, at the post. It never
// asks, never walks to the furnace and hands over what it makes.
func TestJewelleryRoundResumesFromSapphireNecklacesInTheBank(t *testing.T) {
	cfg := chestCfg(t)
	id := func(n string) int32 { return itemID(t, cfg, n, false) }
	bank := []actionsim.Stack{{ID: id("Sapphire necklace"), Count: 37}, {ID: id("Cosmic rune"), Count: 100}, {ID: id("Water rune"), Count: 100}, {ID: id("Games necklace(8)"), Count: 1}}
	jr := newJewelRig(t, bank, jewelStock())
	dec := mastercraftplan.Decision{Mode: mastercraftplan.ModeJewellery, Resume: true, Why: "this account's bank holds 37 Sapphire necklace(s) to enchant"}
	out := jr.round(t, dec)
	if out.Produced != 37 || out.Handed != 37 || out.Transient {
		t.Fatalf("%+v\n%s", out, strings.Join(jr.logs, "\n"))
	}
	if len(jr.asked) != 0 || jr.made != 0 || jr.falVisits != 0 {
		t.Errorf("asked %v made %d falador visits %d", jr.asked, jr.made, jr.falVisits)
	}
	for _, w := range jr.walks {
		if w == "furnace" {
			t.Errorf("walked to the furnace: %v", jr.walks)
		}
	}
	// The account's own Games necklace is not owed: it stays in the bank.
	if jr.inBank(t, "Games necklace(8)") != 1 || jr.b.ledger.Jewellery.OwedTotal() != 0 {
		t.Errorf("own necklace: %d in the bank, owed %d", jr.inBank(t, "Games necklace(8)"), jr.b.ledger.Jewellery.OwedTotal())
	}
	if jr.b.ledger.Jewellery.Handed != 37 {
		t.Errorf("handed %d", jr.b.ledger.Jewellery.Handed)
	}
}

// Necklaces owed from a round a restart interrupted are handed over and nothing else: no ask, no walk to
// the furnace, and not the account's own necklace.
func TestJewelleryRoundHandsOverWhatIsOwed(t *testing.T) {
	cfg := chestCfg(t)
	games := itemID(t, cfg, "Games necklace(8)", false)
	jr := newJewelRig(t, []actionsim.Stack{{ID: games, Count: 13}}, jewelStock())
	jr.b.ledger.Jewellery.AddOwed(int(games), 12)
	out := jr.round(t, mastercraftplan.Decision{Mode: mastercraftplan.ModeJewellery, Resume: true, Why: "12 owed"})
	if out.Handed != 12 || out.Produced != 0 || out.Transient || !out.HandoverTried || out.HandoverFailed {
		t.Fatalf("%+v\n%s", out, strings.Join(jr.logs, "\n"))
	}
	if len(jr.asked) != 0 || jr.inBank(t, "Games necklace(8)") != 1 || jr.b.ledger.Jewellery.OwedTotal() != 0 {
		t.Errorf("asked %v, bank %d, owed %d", jr.asked, jr.inBank(t, "Games necklace(8)"), jr.b.ledger.Jewellery.OwedTotal())
	}
	// A hand-over that did not cross leaves them owed, and the round says so.
	jr2 := newJewelRig(t, []actionsim.Stack{{ID: games, Count: 12}}, jewelStock())
	jr2.b.ledger.Jewellery.AddOwed(int(games), 12)
	masterDeliver = func(*fleetrunner.Deps, *donorrunner.Deps, string, tile.Tile, int, int, []string) runerunner.Delivery {
		return runerunner.Delivery{Result: donorrunner.Result{GaveUp: true, Reason: "the mule never met me"}}
	}
	out = jr2.round(t, mastercraftplan.Decision{Mode: mastercraftplan.ModeJewellery, Resume: true, Why: "12 owed"})
	if out.Handed != 0 || !out.HandoverTried || !out.HandoverFailed || jr2.b.ledger.Jewellery.OwedTotal() != 12 {
		t.Fatalf("%+v owed %d", out, jr2.b.ledger.Jewellery.OwedTotal())
	}
	if jr2.inBank(t, "Games necklace(8)") != 12 {
		t.Errorf("the necklaces must be back in the bank: %v", jr2.sim.Bank())
	}
}

// The mule's answer short of what was asked: the batch is cut to what the bank then holds.
func TestJewelleryRoundCutsTheBatchToWhatArrived(t *testing.T) {
	cfg := chestCfg(t)
	stock := jewelStock()
	stock["Gold bar"] = 7
	jr := newJewelRig(t, []actionsim.Stack{{ID: itemID(t, cfg, "Necklace mould", false), Count: 1}}, stock)
	out := jr.round(t, newBatch(50))
	if out.Produced != 7 || out.Handed != 7 || out.Transient {
		t.Fatalf("%+v\n%s", out, strings.Join(jr.logs, "\n"))
	}
	if jr.logged("the batch is cut from 50 to 7") != 1 {
		t.Errorf("no line saying so:\n%s", strings.Join(jr.logs, "\n"))
	}
	// Nothing arrived at all: the round makes nothing, pauses (the mule's bank is stale), and backs off
	// when the ask itself failed for a passing reason.
	stock2 := jewelStock()
	jr2 := newJewelRig(t, []actionsim.Stack{{ID: itemID(t, cfg, "Necklace mould", false), Count: 1}}, stock2)
	for k := range stock2 {
		stock2[k] = 0 // what the mule actually holds; its published bank (jr2's peers) still says it is full
	}
	out = jr2.round(t, newBatch(50))
	if out.Produced != 0 || out.Handed != 0 {
		t.Fatalf("an empty mule: %+v", out)
	}
}

// A batch the ledger remembers but that is not there (the bank was emptied while the script was off):
// the resume reads the bank, finds nothing to make or hand over, clears the batch, and sets no pause and
// no backoff, so the next batch the empty mule calls for starts at once.
func TestJewelleryRoundResumesAStaleBatchAndClearsIt(t *testing.T) {
	cfg := chestCfg(t)
	jr := newJewelRig(t, []actionsim.Stack{{ID: itemID(t, cfg, "Necklace mould", false), Count: 1}}, jewelStock())
	led := &jr.b.ledger.Jewellery
	led.Target, led.Crafted, led.Enchanted = 20, 20, 5
	led.Bank = mastercraftplan.JewelleryHeld{Necklaces: 15}
	out := jr.round(t, mastercraftplan.Decision{Mode: mastercraftplan.ModeJewellery, Resume: true, Why: "a batch under way"})
	if out.Produced != 0 || out.Handed != 0 || out.Transient {
		t.Fatalf("%+v\n%s", out, strings.Join(jr.logs, "\n"))
	}
	if led.Under() || led.Target != 0 || led.PauseUntil != (time.Time{}) || jr.b.ledger.Fails(mastercraftplan.ModeJewellery) != 0 {
		t.Errorf("ledger %+v backoffs %v", *led, jr.b.ledger.Backoffs)
	}
	if len(jr.asked) != 0 {
		t.Errorf("a resumed round asked for %v", jr.asked)
	}
	// The ledger's picture of the bank is the session's own reading, so a restart does not resume it.
	if jr.b.ledger.Jewellery.Bank.Necklaces != 0 {
		t.Errorf("the ledger still remembers %d Sapphire necklaces in a bank that holds none", jr.b.ledger.Jewellery.Bank.Necklaces)
	}
}

// The jewellery round's waits: one that made or handed over something clears them, a passing failure
// backs off, one with nothing to do pauses.
func TestMasterCrafterJewelleryWaits(t *testing.T) {
	r := newMasterRig(t)
	b := r.b
	b.jewelleryWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeJewellery, Transient: true, Why: "the shops are out of moulds"}, false)
	if b.ledger.Fails(mastercraftplan.ModeJewellery) != 1 {
		t.Fatalf("transient: %+v", b.ledger.Backoffs)
	}
	b.jewelleryWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeJewellery, Produced: 6}, false)
	if b.ledger.Fails(mastercraftplan.ModeJewellery) != 0 || !b.ledger.Jewellery.PauseUntil.IsZero() {
		t.Fatalf("produced: %+v %v", b.ledger.Backoffs, b.ledger.Jewellery.PauseUntil)
	}
	b.jewelleryWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeJewellery, Handed: 6}, false)
	if b.ledger.Fails(mastercraftplan.ModeJewellery) != 0 {
		t.Fatalf("handed: %+v", b.ledger.Backoffs)
	}
	// A resumed round that found nothing pauses nothing: its batch was cleared, and a pause would hold off
	// the next one the empty mule calls for.
	b.jewelleryWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeJewellery, Why: "nothing there"}, true)
	if !b.ledger.Jewellery.PauseUntil.IsZero() {
		t.Fatalf("a resume that found nothing paused: %v", b.ledger.Jewellery.PauseUntil)
	}
	b.jewelleryWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeJewellery, Why: "nothing to do"}, false)
	if b.ledger.Jewellery.PauseUntil.Before(time.Now().Add(b.set.FruitlessPause - time.Minute)) {
		t.Fatalf("nothing to do must pause: %v", b.ledger.Jewellery.PauseUntil)
	}
}

// The ledger remembers the bank's round items across a restart, until the session reads the bank.
func TestOwnJewelleryHeldRemembersTheBankAcrossARestart(t *testing.T) {
	cfg := chestCfg(t)
	neck := itemID(t, cfg, "Sapphire necklace", false)
	jr := newJewelRig(t, nil, nil)
	jr.freshSession(cfg, nil) // as a restarted script finds it: no bank read
	jr.b.ledger.Jewellery.Bank = mastercraftplan.JewelleryHeld{Necklaces: 14, Cosmic: 9}
	if got, known := jr.b.ownJewelleryHeld(jr.ctx); known || got.Necklaces != 14 || got.Cosmic != 9 {
		t.Errorf("ledger's: %+v known %v", got, known)
	}
	jr.sim.OpenBank([]actionsim.Stack{{ID: neck, Count: 3}})
	if got, known := jr.b.ownJewelleryHeld(jr.ctx); !known || got.Necklaces != 3 || got.Cosmic != 0 {
		t.Errorf("the session's reading wins: %+v known %v", got, known)
	}
}

// THE RESTART: a batch under way on the Falador side starts again where the account stands, although
// the Falador road is "far from the mule" for any other round; at Varrock, east of the mountain, it
// waits for the walk to the post; at Ardougne, on the mule's side, it runs.
func TestAJewelleryRoundUnderWayStartsOnTheFaladorSide(t *testing.T) {
	cfg := chestCfg(t)
	neck := itemID(t, cfg, "Sapphire necklace", false)
	for _, c := range []struct {
		name string
		at   tile.Tile
		want string
	}{
		{"at the Falador furnace", tile.Tile{X: 2974, Z: 3368}, "stop,round:jewellery,start"},
		{"on the Falador road", tile.Tile{X: 2951, Z: 3430}, "stop,round:jewellery,start"},
		{"Varrock, east of the mountain, not on the Falador side", tile.Tile{X: 3212, Z: 3424}, "loop"},
		{"Ardougne, on the mule's side", tile.Tile{X: 2661, Z: 3301}, "stop,round:jewellery,start"},
	} {
		r, _ := resumeLoopRig(t, []actionsim.Stack{{ID: neck, Count: 1}})
		r.b.set.GemCutting, r.b.set.BowStringing = false, false
		r.b.ledger.Jewellery.StartBatch(50)
		r.b.ledger.Jewellery.Enchanted = 10
		r.b.decide = func(ctx *Context) mastercraftplan.Decision {
			r.checks++
			p := scarabMulePresenceOf(ctx, r.b.mule)
			held, known := r.b.ownJewelleryHeld(ctx)
			return r.b.withChest(ctx, mastercraftplan.Snapshot{
				Now: time.Now(), MuleHeard: p.Heard, MuleSameWorld: p.SameWorld, CraftingLevel: 92, MagicLevel: 40,
				Stock:  func(item string) (int, bool) { return map[string]int{"Games necklace(8)": 3}[item], true },
				Ledger: &r.b.ledger, Denied: r.b.denied, OwnJewellery: held, OwnJewelleryKnown: known,
			})
		}
		r.sim.StandAt(c.at.X, c.at.Z)
		r.loop(t)
		if got := strings.Join(r.events, ","); got != c.want {
			t.Errorf("%s: events %q, want %q", c.name, got, c.want)
		}
		if c.want != "loop" && (len(r.rounds) != 1 || r.rounds[0] != mastercraftplan.ModeJewellery) {
			t.Errorf("%s: rounds %v", c.name, r.rounds)
		}
	}
	_ = cfg
}

// The jewellery round comes first when the mule holds none, then the gem round once it holds
// necklaces; the account goes back to ScarabSwarm after.
func TestMasterCrafterJewelleryRoundChainsBeforeGems(t *testing.T) {
	r := newMasterRig(t)
	stock := map[string]int{"Uncut sapphire": 900, "Chisel": 1, "Gold bar": 371, "Sapphire": 59102, "Cosmic rune": 4374, "Water rune": 211521}
	r.b.decide = func(*Context) mastercraftplan.Decision {
		r.checks++
		return mastercraftplan.DecideWithChest(r.b.set, mastercraftplan.Snapshot{
			Now: time.Now(), MuleHeard: true, MuleSameWorld: true,
			Stock:         func(item string) (int, bool) { return stock[item], true },
			CraftingLevel: 92, MagicLevel: 40, Ledger: &r.b.ledger, Denied: r.b.denied, OwnJewelleryKnown: true,
		}, mastercraftplan.ChestInput{})
	}
	r.b.round = func(_ *Context, dec mastercraftplan.Decision) masterRoundOutcome {
		r.rounds = append(r.rounds, dec.Mode)
		switch dec.Mode {
		case mastercraftplan.ModeJewellery:
			stock["Games necklace(8)"] = 50
		case mastercraftplan.ModeGems:
			stock["Uncut sapphire"] = 0
		}
		return masterRoundOutcome{Mode: dec.Mode, Produced: 10, Handed: 10, HandoverTried: true}
	}
	r.dueNow()
	r.loop(t) // the check
	r.loop(t) // the safe point: jewellery, then gems straight on
	if len(r.rounds) != 2 || r.rounds[0] != mastercraftplan.ModeJewellery || r.rounds[1] != mastercraftplan.ModeGems {
		t.Fatalf("rounds %v, want jewellery then gems", r.rounds)
	}
	if r.b.swarm == nil {
		t.Fatal("ScarabSwarm must start again after the rounds")
	}
}

// A jewellery round after a jewellery round waits for the next check: the mule's published bank may not
// show the necklaces yet.
func TestMasterCrafterDoesNotChainTwoJewelleryBatches(t *testing.T) {
	d := mastercraftplan.Decision{Mode: mastercraftplan.ModeJewellery, Jewellery: mastercraftplan.JewelleryPlan{N: 50}}
	if mastercraftplan.Chain(mastercraftplan.ModeJewellery, d) {
		t.Error("a second batch decided on the mule's bank straight after the first")
	}
	d.Resume = true
	if !mastercraftplan.Chain(mastercraftplan.ModeJewellery, d) {
		t.Error("a resume (the account's own bank) may follow at once")
	}
	if !mastercraftplan.Chain(mastercraftplan.ModeGems, mastercraftplan.Decision{Mode: mastercraftplan.ModeJewellery}) {
		t.Error("jewellery may follow a round of another kind")
	}
}

// THE RING OF DUELING ROUND (a user request), over the same rig: 25 Rings of dueling from an empty
// bank with a Ring mould in it. The ask names gold bars, cut Emeralds, Cosmic runes and three Air runes
// an enchant; the rings are crafted at the furnace from the rings menu, enchanted with Lvl-2 Enchant and
// handed over by quantity under their own name; the ledger ends with nothing owed.
func TestJewelleryRoundMakesAndHandsOverRings(t *testing.T) {
	cfg := chestCfg(t)
	jr := newJewelRig(t, []actionsim.Stack{{ID: itemID(t, cfg, "Ring mould", false), Count: 1}, {ID: chestCoins, Count: 5000}},
		map[string]int{"Gold bar": 165, "Emerald": 30051, "Cosmic rune": 4917, "Air rune": 52221, "Games necklace(8)": 10})
	dec := mastercraftplan.Decision{Mode: mastercraftplan.ModeJewellery, Kind: mastercraftplan.KindRing, Why: "the mule holds no Ring of dueling",
		Jewellery: mastercraftplan.JewelleryPlan{Kind: mastercraftplan.KindRing, N: 25}}
	out := jr.round(t, dec)
	if out.Produced != 25 || out.Handed != 25 || out.Transient || !out.HandoverTried || out.HandoverFailed {
		t.Fatalf("%+v\n%s", out, strings.Join(jr.logs, "\n"))
	}
	if jr.made != 25 || jr.enchanted != 25 || jr.bought != 0 {
		t.Errorf("made %d enchanted %d bought %d", jr.made, jr.enchanted, jr.bought)
	}
	asked := map[string]int{}
	for _, it := range jr.asked {
		asked[it.Item] = it.Count
	}
	// Magic 40, no staff: the Falador Teleport's Air too (the mule has no Law or Water rune: a teleport
	// that cannot be paid for is still asked for what the mule has).
	if asked["Gold bar"] != 25 || asked["Emerald"] != 25 || asked["Cosmic rune"] != 25 || asked["Air rune"] < 75 || asked["Sapphire"] != 0 || asked["Water rune"] != 0 {
		t.Errorf("asked %v", asked)
	}
	if got := strings.Join(jr.deliveries, ";"); got != "Ring of dueling(8)" {
		t.Errorf("hand-overs %q, want the rings alone", got)
	}
	led := jr.b.ledger.Jewellery
	if led.OwedTotal() != 0 || led.Under() || led.Target != 0 || led.Kind != mastercraftplan.KindNecklace || led.Made != 25 || led.Handed != 25 || led.Rounds != 1 {
		t.Errorf("ledger %+v", led)
	}
	if jr.inBank(t, "Ring of dueling(8)") != 0 || jr.inBank(t, "Emerald ring") != 0 || jr.inBank(t, "Gold bar") != 0 || jr.inBank(t, "Ring mould") != 1 {
		t.Errorf("bank %v", jr.sim.Bank())
	}
	if len(jr.sim.Pack()) != 0 {
		t.Errorf("the pack holds %v after the round", jr.sim.Pack())
	}
	for _, want := range []string{"jewellery: a batch of 25 Rings of dueling", "jewellery: this batch makes Rings of dueling", "jewellery: crafting:", "jewellery: enchanting:",
		"HANDOVER jewellery mule=mule", "jewellery: round over: 25 Ring of dueling(s) made (25 Emerald ring(s) crafted), 25 handed to mule, 0 still owed, 0 mould(s) bought"} {
		if jr.logged(want) == 0 {
			t.Errorf("no log line %q in:\n%s", want, strings.Join(jr.logs, "\n"))
		}
	}
}

// A ring batch with no Ring mould in the bank buys one (the same keepers), and the Necklace mould in
// the bank does not stand in for it.
func TestJewelleryRoundBuysARingMould(t *testing.T) {
	cfg := chestCfg(t)
	jr := newJewelRig(t, []actionsim.Stack{{ID: itemID(t, cfg, "Necklace mould", false), Count: 1}, {ID: chestCoins, Count: 5000}},
		map[string]int{"Gold bar": 165, "Emerald": 30051, "Cosmic rune": 4917, "Air rune": 52221})
	dec := mastercraftplan.Decision{Mode: mastercraftplan.ModeJewellery, Kind: mastercraftplan.KindRing, Jewellery: mastercraftplan.JewelleryPlan{Kind: mastercraftplan.KindRing, N: 10}}
	out := jr.round(t, dec)
	if out.Produced != 10 || out.Handed != 10 || out.Transient {
		t.Fatalf("%+v\n%s", out, strings.Join(jr.logs, "\n"))
	}
	if jr.bought != 1 || jr.inBank(t, "Ring mould") != 1 || jr.inBank(t, "Necklace mould") != 1 {
		t.Errorf("bought %d, bank %v", jr.bought, jr.sim.Bank())
	}
	if jr.logged("bought a Ring mould at") != 1 {
		t.Errorf("logs:\n%s", strings.Join(jr.logs, "\n"))
	}
}

// A restart with Emerald rings in the account's own bank and the runes carries on as a ring batch, from
// the ledger's kind and the bank, with nothing asked.
func TestJewelleryRoundResumesRings(t *testing.T) {
	cfg := chestCfg(t)
	jr := newJewelRig(t, []actionsim.Stack{
		{ID: itemID(t, cfg, "Emerald ring", false), Count: 6}, {ID: itemID(t, cfg, "Cosmic rune", false), Count: 6},
		{ID: itemID(t, cfg, "Air rune", false), Count: 18}, {ID: itemID(t, cfg, "Ring mould", false), Count: 1}}, map[string]int{})
	jr.b.ledger.Jewellery.StartKindBatch(mastercraftplan.KindRing, 6)
	dec := mastercraftplan.Decision{Mode: mastercraftplan.ModeJewellery, Kind: mastercraftplan.KindRing, Resume: true, Why: "this account's bank holds 6 Emerald ring(s) to enchant"}
	out := jr.round(t, dec)
	if out.Produced != 6 || out.Handed != 6 || out.Transient || len(jr.asked) != 0 {
		t.Fatalf("%+v asked %v\n%s", out, jr.asked, strings.Join(jr.logs, "\n"))
	}
	if got := strings.Join(jr.deliveries, ";"); got != "Ring of dueling(8)" {
		t.Errorf("hand-overs %q", got)
	}
}

// LIVE BUG, 7 Oct 2026 14:42-14:54: a 50-ring batch was made and the round never went home. The visit that ends the
// work draws the teleport home's runes (Camelot Teleport at Magic 45: 5 Air, 1 Law), Air is the ring's second rune,
// so the pack's Air read as dirt and the round banked at Falador eleven times, gave up and left 50 rings owed in the
// bank. At Magic 50 with the runes in the mule's bank the round now casts Camelot Teleport, walks to the post and
// hands the rings over; the craft and enchant lines say rings.
func TestJewelleryRingRoundGoesHomeAndHandsOverWithTheTeleportsAir(t *testing.T) {
	cfg := chestCfg(t)
	stock := map[string]int{"Gold bar": 165, "Emerald": 30051, "Cosmic rune": 4917, "Air rune": 52221, "Law rune": 900, "Water rune": 900}
	jr := newJewelRig(t, []actionsim.Stack{{ID: itemID(t, cfg, "Ring mould", false), Count: 1}, {ID: chestCoins, Count: 5000}}, stock)
	mindHoldStat(jr.sim.State(), mastercraftrunner.MagicStat, levels.XPAtLevel(50), 50)
	dec := mastercraftplan.Decision{Mode: mastercraftplan.ModeJewellery, Kind: mastercraftplan.KindRing, Why: "the mule holds no Ring of dueling",
		Jewellery: mastercraftplan.JewelleryPlan{Kind: mastercraftplan.KindRing, N: 25}}
	out := jr.round(t, dec)
	if out.Produced != 25 || out.Handed != 25 || out.Transient || !out.HandoverTried || out.HandoverFailed {
		t.Fatalf("%+v\n%s", out, strings.Join(jr.logs, "\n"))
	}
	if n := jr.logged("jewellery: bank failed"); n != 0 {
		t.Errorf("the bank step failed %d times:\n%s", n, strings.Join(jr.logs, "\n"))
	}
	if !strings.Contains(strings.Join(jr.casts, ","), "Camelot Teleport") {
		t.Errorf("casts %v: the way home starts with Camelot Teleport at Magic 50", jr.casts)
	}
	if got := strings.Join(jr.deliveries, ";"); got != "Ring of dueling(8)" {
		t.Errorf("hand-overs %q", got)
	}
	if jr.b.ledger.Jewellery.OwedTotal() != 0 || jr.inBank(t, "Ring of dueling(8)") != 0 {
		t.Errorf("owed %d, rings left in the bank %d", jr.b.ledger.Jewellery.OwedTotal(), jr.inBank(t, "Ring of dueling(8)"))
	}
}
