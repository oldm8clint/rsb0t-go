package bot

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/lootchestfisherrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// These tests name items, so they read the real item config (RSB0T_CONFIG_JAG, e.g. fleet-data/
// prod-config.jag) and skip without it.
func chestCfg(t *testing.T) *cachecfg.Config {
	t.Helper()
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return cfg
}

// itemID is the id of the named item, as the item (noted false) or its bank note.
func itemID(t *testing.T, cfg *cachecfg.Config, name string, noted bool) int32 {
	t.Helper()
	for _, id := range cfg.ObjIDs(name) {
		if o, ok := cfg.Obj(id); ok && o.Noted() == noted {
			return int32(id)
		}
	}
	t.Fatalf("no %q (noted %v) in the config", name, noted)
	return 0
}

func cfgObjs(cfg *cachecfg.Config) actionsim.Objs {
	return actionsim.Objs{
		Stackable: func(id int32) bool { o, ok := cfg.Obj(int(id)); return ok && o.Stackable },
		NoteOf: func(id int32) int32 {
			o, ok := cfg.Obj(int(id))
			if !ok || o.Noted() {
				return 0
			}
			for _, n := range cfg.ObjIDs(o.Name) {
				if no, ok := cfg.Obj(n); ok && no.Noted() && no.CertLink == int(id) {
					return int32(n)
				}
			}
			return 0
		},
		UnnoteOf: func(id int32) int32 {
			o, ok := cfg.Obj(int(id))
			if !ok || !o.Noted() {
				return 0
			}
			return int32(o.CertLink)
		},
	}
}

// Content has two coin objects (617 and 995); the chest pays, and the banks hold, 995.
const chestCoins = 995

// chestRig is a MasterCrafter whose account is a bank-and-pack simulator over the real item config,
// with the mule heard on world 1 publishing stock. The bank has been opened this session (a
// restarted session has not: see freshSession).
func chestRig(t *testing.T, pack, bank []actionsim.Stack, stock map[string]int) (*masterRig, *cachecfg.Config) {
	t.Helper()
	cfg := chestCfg(t)
	r := newSimRig(t, nil, nil, stock)
	r.sim = actionsim.New(cfgObjs(cfg), pack)
	r.sim.OpenBank(bank)
	r.ctx.Session = r.sim
	r.ctx.Config = cfg
	// newSimRig's bank door opens ITS simulator; this one is the account's now.
	mastercraftrunner.OpenBank = func(*fleetrunner.Deps, tile.Tile) (*actions.Bank, bool) {
		r.sim.OpenBank(r.sim.Bank())
		return actions.NewBank(r.sim, actionsim.Components()), true
	}
	mindHoldStat(r.sim.State(), mastercraftrunner.MagicStat, levels.XPAtLevel(39), 39)
	return r, cfg
}

// freshSession is the account as a restarted script finds it: the pack as it was left, and a session
// that has not opened a bank, so it knows nothing of what is in it.
func (r *masterRig) freshSession(cfg *cachecfg.Config, pack []actionsim.Stack) {
	r.sim = actionsim.New(cfgObjs(cfg), pack)
	r.ctx.Session = r.sim
	mindHoldStat(r.sim.State(), mastercraftrunner.MagicStat, levels.XPAtLevel(39), 39)
}

func (r *masterRig) chest(t *testing.T, cfg *cachecfg.Config, where tile.Tile) *masterChest {
	t.Helper()
	ids, ok := lootchestfisherrunner.ResolveKeyIDs(cfg)
	if !ok {
		t.Fatal("no Crystal key in the config")
	}
	c := &masterChest{b: r.b, ctx: r.ctx, d: r.b.deps(r.ctx), ld: r.b.chestDeps(r.ctx), post: tile.Tile{X: 2809, Z: 3441}, ids: ids, magic: 39}
	c.kit = c.kitIDs()
	c.refreshKit()
	r.sim.StandAt(where.X, where.Z)
	return c
}

var (
	faladorBank = tile.Tile{X: 2946, Z: 3369}
	atThePost   = tile.Tile{X: 2810, Z: 3441}
)

// A restarted session has not opened the bank, so the keys in it are the ledger's; the pack's are
// always added, notes included.
func TestOwnChestKeysRemembersTheBankAcrossARestart(t *testing.T) {
	cfg := chestCfg(t)
	key, noted := itemID(t, cfg, "Crystal key", false), itemID(t, cfg, "Crystal key", true)
	r, _ := chestRig(t, nil, nil, nil)
	r.freshSession(cfg, []actionsim.Stack{{ID: key, Count: 1}, {ID: key, Count: 1}})
	r.b.ledger.Chest.KeysBank = 141
	if got := r.b.ownChestKeys(r.ctx); got != 141+2 {
		t.Errorf("ledger 141 and 2 in the pack: %d", got)
	}
	r.sim.Give(noted, 300)
	if got := r.b.ownChestKeys(r.ctx); got != 141+2+300 {
		t.Errorf("with 300 noted keys in the pack: %d", got)
	}
	// The session has opened the bank since: its reading wins over the ledger's.
	r.sim.OpenBank([]actionsim.Stack{{ID: key, Count: 7}})
	if got := r.b.ownChestKeys(r.ctx); got != 7+2+300 {
		t.Errorf("a bank read this session: %d", got)
	}
	r.ctx.Config = nil
	if got := r.b.ownChestKeys(r.ctx); got != 0 {
		t.Errorf("no config: %d", got)
	}
}

// The restart that walked the main account to Catherby for nothing: a chest round under way, with keys in the
// bank, on the Falador side, is picked before a gem round the mule's bank would fund, and an account
// anywhere else decides as it always did.
func TestWithChestPicksTheRoundUnderWayOnTheFaladorSide(t *testing.T) {
	stock := map[string]int{"Uncut sapphire": 900, "Chisel": 1, mastercraftplan.ChestKeyItem: 400}
	r, cfg := chestRig(t, nil, nil, stock)
	r.freshSession(cfg, nil)
	r.b.ledger.Chest.KeysBank = 141
	snap := func() mastercraftplan.Snapshot {
		return mastercraftplan.Snapshot{
			Now: time.Now(), MuleHeard: true, MuleSameWorld: true, CraftingLevel: 30,
			Stock:  func(item string) (int, bool) { return stock[item], true },
			Ledger: &r.b.ledger, Denied: r.b.denied,
		}
	}
	r.sim.StandAt(faladorBank.X, faladorBank.Z)
	d := r.b.withChest(r.ctx, snap())
	if d.Mode != mastercraftplan.ModeChest || !d.Resume {
		t.Fatalf("on the Falador side with keys of its own: %+v", d)
	}
	r.sim.StandAt(atThePost.X, atThePost.Z)
	if d := r.b.withChest(r.ctx, snap()); d.Mode != mastercraftplan.ModeGems {
		t.Fatalf("at the post the gem round comes first, as ever: %+v", d)
	}
	r.sim.StandAt(faladorBank.X, faladorBank.Z)
	r.b.ledger.Chest.KeysBank = 0
	if d := r.b.withChest(r.ctx, snap()); d.Mode != mastercraftplan.ModeGems {
		t.Fatalf("with no keys of its own: %+v", d)
	}
}

// A resumed round does not walk to the mule's post first: it logs where it resumes from, and the
// account that is not on the Falador side still goes to the post.
func TestChestRoundResumesWithoutTheWalkToThePost(t *testing.T) {
	r, cfg := chestRig(t, nil, nil, map[string]int{mastercraftplan.ChestKeyItem: 400})
	r.freshSession(cfg, nil)
	r.b.ledger.Chest.KeysBank = 141
	walks := 0
	masterWalkTo = func(*fleetrunner.Deps, tile.Tile, int, time.Duration) bool { walks++; return true }
	r.ctx.requestStop() // the round's first step ends it: this is about how it begins
	dec := mastercraftplan.Decision{Mode: mastercraftplan.ModeChest, Resume: true}

	r.sim.StandAt(faladorBank.X, faladorBank.Z)
	r.b.chestRound(r.ctx, r.b.deps(r.ctx), time.Now(), dec)
	if walks != 0 {
		t.Errorf("%d walk(s) to the post before a resumed round", walks)
	}
	logs := strings.Join(r.logs, "\n")
	if !strings.Contains(logs, "chest: resuming from") || !strings.Contains(logs, "141 Crystal key(s) of its own") {
		t.Errorf("the log does not say it resumed: %v", r.logs)
	}

	r.logs = nil
	r.sim.StandAt(3285, 3211) // somewhere else
	r.b.chestRound(r.ctx, r.b.deps(r.ctx), time.Now(), dec)
	if walks != 1 {
		t.Errorf("%d walk(s) to the post for an account away from the Falador side, want 1", walks)
	}
	if strings.Contains(strings.Join(r.logs, "\n"), "resuming") {
		t.Errorf("resumed from far away: %v", r.logs)
	}
}

// The round's first line says what each leg does and why: Camelot Teleport needs 45, so at 39 the
// way home starts with a Varrock Teleport (a user request: "the main account doesnt use camelot teleport to
// return to the mule").
func TestChestRoundStartLineSaysHowTheWayHomeStarts(t *testing.T) {
	r, _ := chestRig(t, nil, nil, map[string]int{mastercraftplan.ChestKeyItem: 400})
	masterWalkTo = func(*fleetrunner.Deps, tile.Tile, int, time.Duration) bool { return true }
	r.ctx.requestStop()
	r.sim.StandAt(atThePost.X, atThePost.Z)
	r.b.chestRound(r.ctx, r.b.deps(r.ctx), time.Now(), mastercraftplan.Decision{Mode: mastercraftplan.ModeChest})
	logs := strings.Join(r.logs, "\n")
	for _, want := range []string{"round starts at Magic 39", "Falador Teleport (needs 37) castable", "Camelot Teleport (needs 45) not castable",
		"the way home starts with Varrock Teleport (needs 25)", "3 Strange fruit"} {
		if !strings.Contains(logs, want) {
			t.Errorf("the start line lacks %q:\n%s", want, logs)
		}
	}
	for magic, want := range map[int]string{
		17: "the way home to the mule is walked (Varrock Teleport needs Magic 25)",
		45: "the way home starts with Camelot Teleport, which lands by the mule's post",
	} {
		if got := homeWord(magic); !strings.Contains(got, want) {
			t.Errorf("Magic %d: %q lacks %q", magic, got, want)
		}
	}
}

// A Falador bank visit banks the whole pack, the chest's coins with it, draws the kit exactly, and
// leaves the keys in the bank for the ledger. With an air staff worn no Air rune is carried, and the
// ones the chest gave are banked.
func TestChestBankVisitBanksTheLootAndDrawsTheKit(t *testing.T) {
	cfg := chestCfg(t)
	id := func(name string) int32 {
		if name == "Coins" {
			return chestCoins
		}
		return itemID(t, cfg, name, false)
	}
	pack := []actionsim.Stack{
		{ID: id("Crystal key"), Count: 1}, {ID: id("Crystal key"), Count: 1},
		{ID: id("Air rune"), Count: 68}, {ID: id("Water rune"), Count: 56}, {ID: id("Law rune"), Count: 16},
		{ID: id("Coins"), Count: 6120},
		{ID: id("Uncut dragonstone"), Count: 1}, {ID: id("Uncut dragonstone"), Count: 1}, {ID: id("Runite bar"), Count: 1},
	}
	bank := []actionsim.Stack{
		{ID: id("Crystal key"), Count: 300}, {ID: id("Air rune"), Count: 500}, {ID: id("Water rune"), Count: 90},
		{ID: id("Law rune"), Count: 120}, {ID: id("Coins"), Count: 4000000}, {ID: id("Strange fruit"), Count: 60},
	}
	for _, staff := range []bool{false, true} {
		r, _ := chestRig(t, pack, bank, nil)
		if staff {
			r.sim.Wear(3, id("Staff of air"))
		}
		old := chestOpenFaladorBank
		chestOpenFaladorBank = func(*lootchestfisherrunner.Deps, tile.Tile, int) (*actions.Bank, bool) {
			r.sim.OpenBank(r.sim.Bank())
			return actions.NewBank(r.sim, actionsim.Components()), true
		}
		t.Cleanup(func() { chestOpenFaladorBank = old })
		c := r.chest(t, cfg, faladorBank)
		c.view.Where = mastercraftplan.ChestAtFalador
		ok, end, why := c.bank()
		if !ok || end != "" || why != "" {
			t.Fatalf("staff %v: the visit failed: %v %q %q", staff, ok, end, why)
		}
		got := func(name string) int { return simCount(r.sim.Pack(), id(name)) }
		wantAir := 21 // six Falador Teleports and the Varrock Teleport home, three each
		if staff {
			wantAir = 0 // an air staff pays for every Air rune: none are carried
		}
		// (The bank holds no Fire rune, so the Varrock Teleport's is not drawn: the way home is walked.)
		if got("Crystal key") != 6 || got("Water rune") != 6 || got("Law rune") != 7 || got("Air rune") != wantAir ||
			got("Coins") != mastercraftplan.ChestFares || got("Strange fruit") != 3 || got("Uncut dragonstone") != 0 || got("Runite bar") != 0 {
			t.Errorf("staff %v: the pack after the visit: %v", staff, r.sim.Pack())
		}
		if n := simCount(r.sim.Bank(), id("Coins")); n != 4000000+6120-mastercraftplan.ChestFares {
			t.Errorf("staff %v: the bank holds %d coins: the chest's must be banked with the rest", staff, n)
		}
		if staff && simCount(r.sim.Bank(), id("Air rune")) != 568 {
			t.Errorf("the Air runes in the pack were not banked: %d in the bank", simCount(r.sim.Bank(), id("Air rune")))
		}
		if c.view.KeysBank != 296 || r.b.ledger.Chest.KeysBank != 296 {
			t.Errorf("staff %v: keys left in the bank: view %d, ledger %d", staff, c.view.KeysBank, r.b.ledger.Chest.KeysBank)
		}
		if !strings.Contains(strings.Join(r.logs, "\n"), "chest: banked at Falador in ") {
			t.Errorf("staff %v: no timed log line: %v", staff, r.logs)
		}
		if c.bankVisits != 1 {
			t.Errorf("staff %v: visits %d", staff, c.bankVisits)
		}
		c.read(time.Now())
		if c.view.LootInPack {
			t.Errorf("staff %v: the kit itself is not loot", staff)
		}
	}
}

// The ask names the Strange fruit with the keys, as notes, standing at the post with the pack just
// banked; the keys in the bank are the ledger's after it.
func TestChestAskAsksForTheFruitAsNotes(t *testing.T) {
	cfg := chestCfg(t)
	key := itemID(t, cfg, "Crystal key", false)
	r, _ := chestRig(t, nil, []actionsim.Stack{{ID: key, Count: 12}},
		map[string]int{mastercraftplan.ChestKeyItem: 313, "Strange fruit": 4000, "Law rune": 900, "Water rune": 900, "Air rune": 900, "Fire rune": 900})
	var items []fleetplan.ItemCount
	var opts fleetrunner.KitBundleOptions
	scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, it []fleetplan.ItemCount, o fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		items, opts = it, o
		return fleetrunner.KitResult{Got: map[string]int{mastercraftplan.ChestKeyItem: 313}, Declined: map[string]string{}}, fleetrunner.KitBundleInfo{Bundled: true}
	}
	c := r.chest(t, cfg, atThePost)
	c.view.Where = mastercraftplan.ChestAtPost
	ok, end, why := c.ask()
	if !ok || end != "" || why != "" {
		t.Fatalf("the ask failed: %v %q %q", ok, end, why)
	}
	asked := map[string]int{}
	for _, it := range items {
		asked[it.Item] = it.Count
	}
	// The kit's runes: six Falador Teleports and the Varrock Teleport home (an air staff is not worn here).
	if asked[mastercraftplan.ChestKeyItem] != 313 || asked["Strange fruit"] != mastercraftplan.ChestFruitAsk || asked["Law rune"] != 7 ||
		asked["Water rune"] != 6 || asked["Air rune"] != 21 || asked["Fire rune"] != 1 {
		t.Errorf("asked %v", asked)
	}
	if !opts.Noted || opts.Bank != (tile.Tile{}) || !opts.KeepNotes {
		t.Errorf("the ask is noted, with the pack just banked, notes kept: %+v", opts)
	}
	if !c.view.Asked || c.b.ledger.Chest.KeysBank != 12 {
		t.Errorf("asked %v, ledger keys %d", c.view.Asked, c.b.ledger.Chest.KeysBank)
	}
	// A mule with no fruit is still asked for the keys, and the round goes on.
	r2, _ := chestRig(t, nil, []actionsim.Stack{{ID: key, Count: 12}}, map[string]int{mastercraftplan.ChestKeyItem: 313})
	var asked2 []fleetplan.ItemCount
	scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, it []fleetplan.ItemCount, _ fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		asked2 = it
		return fleetrunner.KitResult{Got: map[string]int{mastercraftplan.ChestKeyItem: 313}, Declined: map[string]string{}}, fleetrunner.KitBundleInfo{Bundled: true}
	}
	c2 := r2.chest(t, cfg, atThePost)
	if ok, _, _ := c2.ask(); !ok {
		t.Fatal("a mule without fruit failed the ask")
	}
	for _, it := range asked2 {
		if it.Item == "Strange fruit" {
			t.Errorf("asked for fruit the mule does not publish: %v", asked2)
		}
	}
}

// The ask relies on the items' own forms: the keys and the Strange fruit are unstackable with a bank
// note (so a thousand of them cross in one trade slot, and a bank deposit turns the note into the
// item), the kit's runes and the coins stack. A cache that said otherwise would make the mule decline
// the big lines ("too many to trade and it has no bank note").
func TestTheKitsItemsHaveTheFormsTheAskReliesOn(t *testing.T) {
	cfg := chestCfg(t)
	for _, name := range []string{"Crystal key", "Strange fruit"} {
		item, note := itemID(t, cfg, name, false), itemID(t, cfg, name, true)
		o, _ := cfg.Obj(int(item))
		n, _ := cfg.Obj(int(note))
		if o.Stackable || n.CertLink != int(item) {
			t.Errorf("%s: item %d stackable %v, its note %d links to %d", name, item, o.Stackable, note, n.CertLink)
		}
	}
	if got := itemID(t, cfg, "Strange fruit", false); int(got) != fleetplan.StrangeFruitID {
		t.Errorf("Strange fruit is %d in the cache, %d in scarabplan: the pack count would read the wrong id", got, fleetplan.StrangeFruitID)
	}
	for _, name := range []string{"Air rune", "Water rune", "Law rune", "Fire rune"} {
		if o, _ := cfg.Obj(int(itemID(t, cfg, name, false))); !o.Stackable {
			t.Errorf("%s does not stack", name)
		}
	}
	if o, _ := cfg.Obj(chestCoins); !o.Stackable || o.Name != "Coins" {
		t.Errorf("object %d is %q, stackable %v", chestCoins, o.Name, o.Stackable)
	}
}

// castsOf is the teleport components a run of the simulator was sent: every IF_BUTTON on a standard
// spellbook teleport (actions.Teleport), by component.
func castsOf(sim *actionsim.Session) []int {
	var out []int
	for _, b := range sim.Sent() {
		if len(b) == 3 && int(b[0]) == clientprot.IF_BUTTON {
			if com := int(b[1])<<8 | int(b[2]); com == 1164 || com == 1167 || com == 1170 || com == 1174 {
				out = append(out, com)
			}
		}
	}
	return out
}

// The way home starts with the best teleport the account can cast (a user request: "the main account doesnt
// use camelot teleport to return to the mule"): Camelot's (1174) at Magic 45 from anywhere, Varrock's
// (1164) below it from the Falador side only, none otherwise, and the walk to the post follows each.
func TestChestHomeStartsWithTheTeleportTheAccountCanCast(t *testing.T) {
	cfg := chestCfg(t)
	id := func(name string) int32 { return itemID(t, cfg, name, false) }
	kit := []actionsim.Stack{{ID: id("Fire rune"), Count: 1}, {ID: id("Air rune"), Count: 23}, {ID: id("Law rune"), Count: 7}, {ID: id("Water rune"), Count: 6}}
	for _, c := range []struct {
		name  string
		magic int
		at    tile.Tile
		staff bool
		want  []int
	}{
		{"Magic 39 on the Falador road: Varrock Teleport", 39, tile.Tile{X: 2946, Z: 3369}, false, []int{1164}},
		{"Magic 39, the chest room: Varrock Teleport", 39, tile.Tile{X: 2914, Z: 3451}, false, []int{1164}},
		{"Magic 39 with an air staff, no Air rune in the pack: Varrock Teleport", 39, tile.Tile{X: 2946, Z: 3369}, true, []int{1164}},
		{"Magic 39 on the mule's side of the mountain: not east, walk", 39, tile.Tile{X: 2542, Z: 3169}, false, nil},
		{"Magic 45 at Falador: Camelot Teleport", 45, tile.Tile{X: 2946, Z: 3369}, false, []int{1174}},
		{"Magic 45 anywhere: Camelot Teleport", 45, tile.Tile{X: 2542, Z: 3169}, false, []int{1174}},
		{"Magic 20: nothing to cast", 20, tile.Tile{X: 2946, Z: 3369}, false, nil},
	} {
		pack := kit
		if c.staff {
			pack = []actionsim.Stack{{ID: id("Fire rune"), Count: 1}, {ID: id("Law rune"), Count: 7}, {ID: id("Water rune"), Count: 6}}
		}
		r, _ := chestRig(t, pack, nil, nil)
		if c.staff {
			r.sim.Wear(3, id("Staff of air"))
		}
		var walked []tile.Tile
		masterWalkTo = func(_ *fleetrunner.Deps, to tile.Tile, _ int, _ time.Duration) bool {
			walked = append(walked, to)
			return true
		}
		r.ctx.requestStop() // a cast that does not move the simulated account gives up at once
		cc := r.chest(t, cfg, c.at)
		cc.magic = c.magic
		cc.refreshKit()
		if ok, end, why := cc.home(); !ok || end != "" || why != "" {
			t.Fatalf("%s: home failed: %v %q %q", c.name, ok, end, why)
		}
		if got := castsOf(r.sim); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: teleports cast %v, want %v", c.name, got, c.want)
		}
		if len(walked) != 1 || walked[0] != cc.post {
			t.Errorf("%s: walked to %v, want the post %v", c.name, walked, cc.post)
		}
		if c.want == nil && c.magic > 20 && !strings.Contains(strings.Join(r.logs, "\n"), "walking the whole way home") {
			t.Errorf("%s: no line says why it walks: %v", c.name, r.logs)
		}
	}
}

// A trip that leaves the chest with keys unopened says why, with the numbers the rule used.
func TestUnopenedWordNamesTheNumbers(t *testing.T) {
	got := unopenedWord(2, 7, 9, 1)
	for _, want := range []string{"2 key(s) unopened", "7 slot(s) free plus the key's own", "the worst reward needs 9", "1 Strange fruit left"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lacks %q", got, want)
		}
	}
}
