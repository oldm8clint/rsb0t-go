package bot

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/lootchestfisherplan"
	"rsb0t-go/internal/scripts/lootchestfisherrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// THE CHEST ROUND'S ALCHEMY AND GAMES ROOM ROUTE (a user request), over the bank-and-pack simulator with
// the real item config: the kit's extras, the alchemy that makes room at the chest, and the route's decision.

func magicRig(t *testing.T, pack, bank []actionsim.Stack, magic int) (*masterRig, func(string) int32) {
	t.Helper()
	cfg := chestCfg(t)
	id := func(name string) int32 {
		if name == "Coins" {
			return chestCoins
		}
		return itemID(t, cfg, name, false)
	}
	r, _ := chestRig(t, pack, bank, nil)
	mindHoldStat(r.sim.State(), mastercraftrunner.MagicStat, levels.XPAtLevel(magic), magic)
	return r, id
}

func TestChestBankVisitDrawsTheAlchemyKitAndTheNecklace(t *testing.T) {
	cfg := chestCfg(t)
	idOf := func(name string) int32 {
		if name == "Coins" {
			return chestCoins
		}
		return itemID(t, cfg, name, false)
	}
	bank := []actionsim.Stack{
		{ID: idOf("Crystal key"), Count: 300}, {ID: idOf("Air rune"), Count: 500}, {ID: idOf("Water rune"), Count: 90}, {ID: idOf("Law rune"), Count: 120},
		{ID: idOf("Coins"), Count: 4000000}, {ID: idOf("Strange fruit"), Count: 60}, {ID: idOf("Nature rune"), Count: 700},
		{ID: idOf("Ruby"), Count: 200}, {ID: idOf("Diamond"), Count: 50}, {ID: idOf("Games necklace(8)"), Count: 3},
	}
	pack := []actionsim.Stack{{ID: idOf("Adamant sq shield"), Count: 1}, {ID: idOf("Ruby"), Count: 1}, {ID: idOf("Ruby"), Count: 1}}
	r, id := magicRig(t, pack, bank, 66)
	old := chestOpenFaladorBank
	chestOpenFaladorBank = func(*lootchestfisherrunner.Deps, tile.Tile, int) (*actions.Bank, bool) {
		r.sim.OpenBank(r.sim.Bank())
		return actions.NewBank(r.sim, actionsim.Components()), true
	}
	t.Cleanup(func() { chestOpenFaladorBank = old })
	r.sim.Wear(3, id("Staff of fire"))
	c := r.chest(t, cfg, faladorBank)
	c.magic = 66
	c.opt = mastercraftplan.ChestKitOptions{Alch: true, Route: true, PostRoute: true}
	c.kit = c.kitIDs()
	c.refreshKit()
	c.view.Where = mastercraftplan.ChestAtFalador
	if !c.paid[mastercraftplan.RuneFire] {
		t.Fatalf("a Staff of fire pays the Fire runes: %v", c.paid)
	}
	if ok, end, why := c.bank(); !ok || end != "" || why != "" {
		t.Fatalf("the visit failed: %v %q %q", ok, end, why)
	}
	got := func(name string) int { return simCount(r.sim.Pack(), id(name)) }
	notedRuby := itemID(t, cfg, "Ruby", true)
	notedDiamond := itemID(t, cfg, "Diamond", true)
	if got("Crystal key") != 6 || got("Nature rune") != mastercraftplan.ChestNaturesCarry || got("Games necklace(8)") != 1 ||
		simCount(r.sim.Pack(), notedRuby) != mastercraftplan.ChestFodderCarry || simCount(r.sim.Pack(), notedDiamond) != 0 ||
		got("Ruby") != 0 || got("Adamant sq shield") != 0 || got("Fire rune") != 0 || got("Air rune") != 23 || got("Law rune") != 7 || got("Water rune") != 6 {
		t.Errorf("the pack after the visit: %v", r.sim.Pack())
	}
	// The chest's loot and the gems went into the bank with the rest; the necklace and the rubies were drawn from it.
	if simCount(r.sim.Bank(), id("Adamant sq shield")) != 1 || simCount(r.sim.Bank(), id("Ruby")) != 200+2-mastercraftplan.ChestFodderCarry {
		t.Errorf("the bank after the visit: shield %d, rubies %d", simCount(r.sim.Bank(), id("Adamant sq shield")), simCount(r.sim.Bank(), id("Ruby")))
	}
	c.read(time.Now())
	if c.view.LootInPack {
		t.Errorf("the kit itself is not loot: %v", r.sim.Pack())
	}
	// The same pack with the options off holds loot: the extras are the kit only while the round carries them.
	c.opt = mastercraftplan.ChestKitOptions{}
	c.kit = c.kitIDs()
	c.refreshKit()
	c.read(time.Now())
	if !c.view.LootInPack {
		t.Error("a Games necklace and Nature runes are loot to a round with the extras off")
	}
}

func TestChestAlchMakesRoomAtTheChestAndLeavesTheMuleItsDue(t *testing.T) {
	cfg := chestCfg(t)
	idOf := func(name string) int32 { return itemID(t, cfg, name, false) }
	pack := []actionsim.Stack{
		{ID: idOf("Crystal key"), Count: 1}, {ID: idOf("Crystal key"), Count: 1},
		{ID: idOf("Ruby"), Count: 1}, {ID: idOf("Ruby"), Count: 1}, {ID: idOf("Diamond"), Count: 1}, {ID: idOf("Diamond"), Count: 1}, {ID: idOf("Adamant sq shield"), Count: 1},
		{ID: idOf("Nature rune"), Count: 10}, {ID: idOf("Fire rune"), Count: 50}, {ID: chestCoins, Count: 240}, // the ships' fare
	}
	for i := 0; i < 14; i++ {
		pack = append(pack, actionsim.Stack{ID: idOf("Bronze dagger"), Count: 1})
	}
	r, id := magicRig(t, pack, nil, 66)
	c := r.chest(t, cfg, lootchestfisherplan.ChestStand)
	c.magic = 66
	c.opt = mastercraftplan.ChestKitOptions{Alch: true, Route: true}
	c.travel = chestTravel(c.opt, r.b.set)
	c.kit = c.kitIDs()
	c.refreshKit()
	led := &r.b.ledger.Chest
	led.AddOwed(int(id("Ruby")), 2)
	led.AddOwed(int(id("Diamond")), 2)
	led.AddOwed(int(id("Adamant sq shield")), 1)
	led.AddOwed(int(id("Uncut dragonstone")), 4) // not alchable, stays owed
	// The server's side of one cast: the dearest piece of alchable loot becomes coins.
	var casts []string
	old := chestIdleAlch
	chestIdleAlch = func(_ *fleetrunner.Travel, d *fleetrunner.Deps) bool {
		for _, name := range []string{"Adamant sq shield", "Diamond", "Ruby"} {
			if simCount(r.sim.Pack(), id(name)) > 0 {
				r.sim.Remove(id(name), 1)
				r.sim.Give(id("Coins"), 600)
				r.sim.Remove(id("Nature rune"), 1)
				casts = append(casts, name)
				return true
			}
		}
		return false
	}
	t.Cleanup(func() { chestIdleAlch = old })

	c.view.Asked, c.view.PostBanked = true, true
	c.read(time.Now())
	if c.view.Where != mastercraftplan.ChestAtChest || c.view.Room || c.view.Alchables != 5 || !c.view.AlchReady || !c.view.AlchForRoom {
		t.Fatalf("at the chest with a full pack: where %v room %v alchables %d ready %v for room %v",
			c.view.Where, c.view.Room, c.view.Alchables, c.view.AlchReady, c.view.AlchForRoom)
	}
	if next := mastercraftplan.NextChestStep(c.view); next != mastercraftplan.ChestStepAlch {
		t.Fatalf("the next step is %s, want alch", next)
	}
	steps := 0
	for ; steps < 8; steps++ {
		c.read(time.Now())
		next := mastercraftplan.NextChestStep(c.view)
		if next == mastercraftplan.ChestStepOpen {
			break
		}
		if next != mastercraftplan.ChestStepAlch {
			t.Fatalf("step %d: %s", steps, next)
		}
		if ok, end, why := c.alchForRoom(); !ok || end != "" || why != "" {
			t.Fatalf("step %d: %v %q %q", steps, ok, end, why)
		}
	}
	if next := func() mastercraftplan.ChestStep { c.read(time.Now()); return mastercraftplan.NextChestStep(c.view) }(); next != mastercraftplan.ChestStepOpen {
		t.Fatalf("after %d casts the chest still cannot be opened: %s (pack %v)", len(casts), next, r.sim.Pack())
	}
	if len(casts) == 0 || len(casts) > 5 || casts[0] != "Adamant sq shield" {
		t.Errorf("casts %v: the dearest first, no more than the room needed", casts)
	}
	// What was cast away is not the mule's any more; what was not stays owed.
	if led.Owed[strconv.Itoa(int(id("Adamant sq shield")))] != 0 || led.Owed[strconv.Itoa(int(id("Uncut dragonstone")))] != 4 {
		t.Errorf("owed after the casts: %v", led.Owed)
	}
	owed := led.OwedTotal()
	if want := 2 + 2 + 1 + 4 - len(casts); owed != want {
		t.Errorf("owed %d, want %d after %d casts", owed, want, len(casts))
	}
	if c.alched != len(casts) || !strings.Contains(strings.Join(r.logs, "\n"), "chest: alched 1 piece(s) of loot") {
		t.Errorf("alched %d, logs %v", c.alched, r.logs)
	}
}

// A cast that takes nothing twice turns the alchemy off for the round, and the round goes on to the bank.
func TestChestAlchGivesUpAfterTwoEmptyCasts(t *testing.T) {
	cfg := chestCfg(t)
	idOf := func(name string) int32 { return itemID(t, cfg, name, false) }
	pack := []actionsim.Stack{{ID: idOf("Ruby"), Count: 1}, {ID: idOf("Nature rune"), Count: 10}, {ID: idOf("Fire rune"), Count: 50}}
	r, _ := magicRig(t, pack, nil, 66)
	c := r.chest(t, cfg, lootchestfisherplan.ChestStand)
	c.magic = 66
	c.opt = mastercraftplan.ChestKitOptions{Alch: true}
	c.travel = chestTravel(c.opt, r.b.set)
	old := chestIdleAlch
	chestIdleAlch = func(*fleetrunner.Travel, *fleetrunner.Deps) bool { return true } // sent, and nothing happens
	t.Cleanup(func() { chestIdleAlch = old })
	for i := 0; i < 2; i++ {
		if ok, _, _ := c.alchForRoom(); ok {
			t.Fatalf("cast %d: an empty cast is not a step that worked", i)
		}
	}
	if !c.alchOff || c.alchReady() {
		t.Errorf("alchOff %v, ready %v", c.alchOff, c.alchReady())
	}
}

func TestChestAlchReadyNeedsMagicRunesAndAStaffOrFire(t *testing.T) {
	cfg := chestCfg(t)
	idOf := func(name string) int32 { return itemID(t, cfg, name, false) }
	for _, tc := range []struct {
		name  string
		pack  []actionsim.Stack
		staff bool
		magic int
		want  bool
	}{
		{"natures and a staff", []actionsim.Stack{{ID: idOf("Nature rune"), Count: 5}}, true, 66, true},
		{"natures and five fire runes", []actionsim.Stack{{ID: idOf("Nature rune"), Count: 5}, {ID: idOf("Fire rune"), Count: 5}}, false, 66, true},
		{"natures and four fire runes", []actionsim.Stack{{ID: idOf("Nature rune"), Count: 5}, {ID: idOf("Fire rune"), Count: 4}}, false, 66, false},
		{"no natures", []actionsim.Stack{{ID: idOf("Fire rune"), Count: 50}}, true, 66, false},
		{"Magic 54", []actionsim.Stack{{ID: idOf("Nature rune"), Count: 5}}, true, 54, false},
	} {
		r, id := magicRig(t, tc.pack, nil, tc.magic)
		if tc.staff {
			r.sim.Wear(3, id("Staff of fire"))
		}
		c := r.chest(t, cfg, lootchestfisherplan.ChestStand)
		c.magic = tc.magic
		c.opt = mastercraftplan.ChestKitOptions{Alch: true}
		c.travel = chestTravel(c.opt, r.b.set)
		if got := c.alchReady(); got != tc.want {
			t.Errorf("%s: %v", tc.name, got)
		}
		c.opt.Alch = false
		if c.alchReady() {
			t.Errorf("%s: the option is off", tc.name)
		}
	}
}

func TestChestTravelStallsWithAlchemyAndTheRub(t *testing.T) {
	set := mastercraftplan.DefaultSettings()
	if tr := chestTravel(mastercraftplan.ChestKitOptions{}, set); tr != nil {
		t.Errorf("no options, no staller: %+v", tr)
	}
	tr := chestTravel(mastercraftplan.ChestKitOptions{Alch: true, Route: true}, set)
	if tr == nil || !tr.Alch.Enabled || !tr.Rub || tr.Alch.MinMagic != 55 || len(tr.Alch.Items) != len(mastercraftplan.ChestAlchNames()) || tr.Alch.Items[0] != "Rune platelegs" {
		t.Errorf("alch + route: %+v", tr)
	}
	tr = chestTravel(mastercraftplan.ChestKitOptions{Route: true}, set)
	if tr == nil || tr.Alch.Enabled || !tr.Rub {
		t.Errorf("route only: the rub stalls the walks: %+v", tr)
	}
}

// THE ROUTE: from the post and the Falador bank a Games necklace takes the account to Burthorpe, the walk on to the
// chest follows, and no Falador Teleport is cast; a route that fails goes on by the old legs and is dropped after two.
func TestChestToChestTakesTheGamesRoomRoute(t *testing.T) {
	cfg := chestCfg(t)
	idOf := func(name string) int32 { return itemID(t, cfg, name, false) }
	pack := []actionsim.Stack{
		{ID: idOf("Games necklace(8)"), Count: 1}, {ID: idOf("Crystal key"), Count: 1},
		{ID: idOf("Air rune"), Count: 23}, {ID: idOf("Law rune"), Count: 7}, {ID: idOf("Water rune"), Count: 6},
	}
	for _, tc := range []struct {
		name      string
		mode      string
		at        tile.Tile
		where     mastercraftplan.ChestPlace
		wantRoute bool
	}{
		{"from the post, always", mastercraftplan.GamesRouteAlways, atThePost, mastercraftplan.ChestAtPost, true},
		{"from the Falador bank, always", mastercraftplan.GamesRouteAlways, faladorBank, mastercraftplan.ChestAtFalador, true},
		{"from the post, post", mastercraftplan.GamesRoutePost, atThePost, mastercraftplan.ChestAtPost, true},
		{"from the post, off", mastercraftplan.GamesRouteOff, atThePost, mastercraftplan.ChestAtPost, false},
		{"mid-walk, always", mastercraftplan.GamesRouteAlways, tile.Tile{X: 2899, Z: 3564}, mastercraftplan.ChestAtOther, false},
	} {
		r, _ := magicRig(t, pack, nil, 66)
		r.b.set.ChestGamesRoute = tc.mode
		var routes int
		var walked []tile.Tile
		oldRoute, oldWalk := chestGamesRoute, masterWalkTo
		chestGamesRoute = func(*fleetrunner.Deps, func() bool) mastercraftrunner.GamesRouteResult {
			routes++
			r.sim.StandAt(2899, 3564)
			return mastercraftrunner.GamesRouteResult{Rubbed: true, Landed: true, Out: true, Climbs: 2}
		}
		masterWalkTo = func(_ *fleetrunner.Deps, to tile.Tile, _ int, _ time.Duration) bool {
			walked = append(walked, to)
			return true
		}
		t.Cleanup(func() { chestGamesRoute, masterWalkTo = oldRoute, oldWalk })
		c := r.chest(t, cfg, tc.at)
		c.magic = 66
		c.refreshKit()
		c.view.Where = tc.where
		if ok, end, why := c.toChest(); !ok || end != "" || why != "" {
			t.Fatalf("%s: %v %q %q", tc.name, ok, end, why)
		}
		if (routes == 1) != tc.wantRoute {
			t.Errorf("%s: the route ran %d times", tc.name, routes)
		}
		if tc.wantRoute {
			if got := castsOf(r.sim); len(got) != 0 {
				t.Errorf("%s: a teleport was cast after the route: %v", tc.name, got)
			}
			if c.routeFlown != 1 || !strings.Contains(strings.Join(r.logs, "\n"), "Games Room route: rubbed") {
				t.Errorf("%s: flown %d, logs %v", tc.name, c.routeFlown, r.logs)
			}
		}
		if len(walked) == 0 || walked[len(walked)-1] != lootchestfisherplan.ChestStand {
			t.Errorf("%s: walked %v, want to end at the chest stand", tc.name, walked)
		}
	}
}

func TestChestGamesRouteFailsOverAndGivesUp(t *testing.T) {
	cfg := chestCfg(t)
	idOf := func(name string) int32 { return itemID(t, cfg, name, false) }
	pack := []actionsim.Stack{{ID: idOf("Games necklace(8)"), Count: 1}}
	r, _ := magicRig(t, pack, nil, 66)
	routes := 0
	oldRoute, oldWalk := chestGamesRoute, masterWalkTo
	chestGamesRoute = func(*fleetrunner.Deps, func() bool) mastercraftrunner.GamesRouteResult {
		routes++
		return mastercraftrunner.GamesRouteResult{Why: "the rub's menu did not offer Burthorpe"}
	}
	masterWalkTo = func(*fleetrunner.Deps, tile.Tile, int, time.Duration) bool { return true }
	t.Cleanup(func() { chestGamesRoute, masterWalkTo = oldRoute, oldWalk })
	c := r.chest(t, cfg, atThePost)
	c.magic = 66
	for i := 0; i < 4; i++ {
		c.view.Where = mastercraftplan.ChestAtPost
		if ok, _, why := c.toChest(); !ok {
			t.Fatalf("toChest %d: %s", i, why)
		}
	}
	if routes != 2 || !c.routeOff || c.routeFails != 2 {
		t.Errorf("routes %d, off %v, fails %d: two failures turn it off for the round", routes, c.routeOff, c.routeFails)
	}
	if !strings.Contains(strings.Join(r.logs, "\n"), "failed twice: not tried again this round") {
		t.Errorf("logs %v", r.logs)
	}
}
