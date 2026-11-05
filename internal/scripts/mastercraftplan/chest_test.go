package mastercraftplan

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/lootchestfisherplan"
	"rsb0t-go/internal/scripts/orderplan"
)

// chestSnap is a snapshot where no other round is due: the mule is here and holds nothing a gem,
// pearl or bow round wants.
func chestSnap(now time.Time, keys int) Snapshot {
	return Snapshot{
		Now: now, MuleHeard: true, MuleSameWorld: true,
		Stock:         stockMap(map[string]int{ChestKeyItem: keys}),
		CraftingLevel: 30, FletchingLevel: 60, FletchingKnown: true,
		Ledger: &Ledger{},
	}
}

// The trigger is the owner's 400 keys in the mule's bank (6 Oct 2026); the mule's own key gate stays
// at more than 100.
func TestChestTriggerIsAThousand(t *testing.T) {
	threshold, floor := ChestKeyThreshold, orderplan.KeysFloor
	if threshold != 100 || floor != threshold {
		t.Fatalf("threshold %d, mule floor %d", threshold, floor)
	}
	if ChestReadyFor(DefaultSettings(), 999) || !ChestReadyFor(DefaultSettings(), 1000) {
		t.Fatal("ChestReadyFor must be 1000 or more (a user request)")
	}
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	set := orderSettings()
	if d := DecideWithChest(set, chestSnap(now, 999), ChestInput{}); d.Mode != ModeSwarm || !strings.Contains(d.Why, "under the threshold 1000") {
		t.Errorf("999 keys: %+v", d)
	}
	d := DecideWithChest(set, chestSnap(now, 1000), ChestInput{})
	if d.Mode != ModeChest || d.Resume {
		t.Errorf("1000 keys: %+v", d)
	}
	set.CrystalChest = false
	if d := DecideWithChest(set, chestSnap(now, 5000), ChestInput{}); d.Mode != ModeSwarm {
		t.Errorf("switched off: %+v", d)
	}
}

// The chest is a scheduled round (a user request): at 1,000 keys it comes ahead of every NEW round but the trawler, a
// hand-over and a round under way.
func TestChestComesAheadOfNewRoundsButNotTheTrawlerOrAHandOver(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	set := orderSettings()
	s := chestSnap(now, 1000)
	s.Stock = stockMap(map[string]int{ChestKeyItem: 1000, "Uncut sapphire": 600, "Chisel": 1, "Maple longbow": 500, "Bow string": 500})
	if d := DecideWithChest(set, s, ChestInput{}); d.Mode != ModeChest {
		t.Errorf("the chest before a new gem round: %+v", d)
	}
	set.GemCutting = false
	if d := DecideWithChest(set, s, ChestInput{}); d.Mode != ModeChest {
		t.Errorf("the chest before a new bow round: %+v", d)
	}
	// 999 keys: the gem round has its turn, and the chest waits.
	set.GemCutting = true
	s.Stock = stockMap(map[string]int{ChestKeyItem: 999, "Uncut sapphire": 600, "Chisel": 1})
	if d := DecideWithChest(set, s, ChestInput{}); d.Mode != ModeGems {
		t.Errorf("under the threshold the gems go: %+v", d)
	}
	s.Stock = stockMap(map[string]int{ChestKeyItem: 1000, "Uncut sapphire": 600, "Chisel": 1})
	s.Ledger.PendingDeliver = true
	if d := DecideWithChest(set, s, ChestInput{}); d.Mode != ModeDeliver {
		t.Errorf("a pending hand-over first: %+v", d)
	}
	s.Ledger.PendingDeliver = false
	ts := set
	ts.Trawler = true
	s.Trawler.Food, s.Trawler.FoodKnown = 0, true
	if d := DecideWithChest(ts, s, ChestInput{}); d.Mode != ModeTrawler {
		t.Errorf("the trawler block first: %+v", d)
	}
}

func TestChestNeedsTheMuleAndHonoursWaits(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	set := orderSettings()
	s := chestSnap(now, 400)
	s.MuleHeard = false
	if d := DecideWithChest(set, s, ChestInput{OwnKeys: 50}); d.Mode != ModeSwarm {
		t.Errorf("mule not heard: %+v", d)
	}
	s = chestSnap(now, 400)
	s.MuleSameWorld = false
	if d := DecideWithChest(set, s, ChestInput{}); d.Mode != ModeSwarm {
		t.Errorf("other world: %+v", d)
	}
	s = chestSnap(now, 400)
	s.Ledger.Chest.PauseUntil = now.Add(time.Hour)
	if d := DecideWithChest(set, s, ChestInput{}); d.Mode != ModeSwarm || !strings.Contains(d.Why, "paused") {
		t.Errorf("paused: %+v", d)
	}
	s = chestSnap(now, 400)
	s.Ledger.Fail(ModeChest, now, set)
	if d := DecideWithChest(set, s, ChestInput{}); d.Mode != ModeSwarm || !strings.Contains(d.Why, "backing off") {
		t.Errorf("backing off: %+v", d)
	}
	s = chestSnap(now, 400)
	s.Ledger.Fail(ModeDeliver, now, set)
	if d := DecideWithChest(set, s, ChestInput{}); d.Mode != ModeSwarm {
		t.Errorf("hand-overs backing off: %+v", d)
	}
	s = chestSnap(now, 400)
	s.Stock = nil
	if d := DecideWithChest(set, s, ChestInput{}); d.Mode != ModeSwarm || !d.Inconclusive {
		t.Errorf("no published bank: %+v", d)
	}
}

// Keys left in this account's bank, and loot still owed to the mule, resume the round whatever the
// mule holds.
func TestChestResumes(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	set := orderSettings()
	if d := DecideWithChest(set, chestSnap(now, 3), ChestInput{OwnKeys: 12}); d.Mode != ModeChest || !d.Resume {
		t.Errorf("own keys: %+v", d)
	}
	s := chestSnap(now, 3)
	s.Ledger.Chest.AddOwed(lootchestfisherplan.RuniteBar, 3)
	if d := DecideWithChest(set, s, ChestInput{}); d.Mode != ModeChest || !d.Resume {
		t.Errorf("owed loot: %+v", d)
	}
	if !Chain(ModeGems, DecideWithChest(set, s, ChestInput{})) {
		t.Error("a chest round chains after a gem round")
	}
}

// The ask is ALL the mule's keys (up to its own per-order cap), never a part of them.
func TestChestAskIsAllTheKeys(t *testing.T) {
	ask := func(keys int) []Line {
		return ChestAsk(stockMap(map[string]int{ChestKeyItem: keys}), 17, nil, nil)
	}
	if l := ask(100); len(l) != 0 {
		t.Errorf("at the gate: %+v", l)
	}
	if l := ask(101); len(l) != 1 || l[0] != (Line{Item: ChestKeyItem, Qty: 101}) {
		t.Errorf("101: %+v", l)
	}
	if l := ask(340); len(l) != 1 || l[0].Qty != 340 {
		t.Errorf("340: %+v", l)
	}
	if l := ask(900); len(l) != 1 || l[0].Qty != orderplan.KeysPerOrder {
		t.Errorf("over the mule's per-order cap: %+v", l)
	}
	// Magic 17 casts neither teleport: no runes are asked for.
	if l := ChestAsk(stockMap(map[string]int{ChestKeyItem: 200, "Law rune": 500, "Air rune": 500, "Water rune": 500}), 17, nil, nil); len(l) != 1 {
		t.Errorf("magic 17 asked for runes: %+v", l)
	}
	// Magic 45: the kit's runes the own bank is short of, at most what the mule publishes.
	own := map[string]int{"Air rune": 30, "Law rune": 2}
	l := ChestAsk(stockMap(map[string]int{ChestKeyItem: 200, "Law rune": 3, "Air rune": 500, "Water rune": 500}), 45, nil,
		func(r string) int { return own[r] })
	want := []Line{{ChestKeyItem, 200}, {"Law rune", 3}, {"Water rune", 6}}
	if !reflect.DeepEqual(l, want) {
		t.Errorf("magic 45: %+v, want %+v", l, want)
	}
}

func TestChestTeleportsByMagic(t *testing.T) {
	for _, c := range []struct {
		magic    int
		fal, cam bool
		runes    map[string]int
	}{
		{17, false, false, map[string]int{}},
		// Varrock Teleport (25) is the way home's teleport below Camelot's 45: one cast of it.
		{25, false, false, map[string]int{"Fire rune": 1, "Air rune": 3, "Law rune": 1}},
		{36, false, false, map[string]int{"Fire rune": 1, "Air rune": 3, "Law rune": 1}},
		{37, true, false, map[string]int{"Water rune": 6, "Air rune": 21, "Law rune": 7, "Fire rune": 1}},
		{45, true, true, map[string]int{"Water rune": 6, "Air rune": 23, "Law rune": 7}},
	} {
		fal, cam := ChestCastable(c.magic)
		if fal != c.fal || cam != c.cam {
			t.Errorf("magic %d: falador %v camelot %v", c.magic, fal, cam)
		}
		if r := ChestRunes(c.magic, nil); !reflect.DeepEqual(r, c.runes) {
			t.Errorf("magic %d runes %v, want %v", c.magic, r, c.runes)
		}
	}
	held := map[string]int{"Water rune": 1, "Air rune": 3, "Law rune": 1}
	have := func(r string) int { return held[r] }
	if !CanCastWith(FaladorTeleport, 37, nil, have) || CanCastWith(FaladorTeleport, 36, nil, have) {
		t.Error("Falador Teleport is level 37 and one water, three air, one law")
	}
	if CanCastWith(CamelotTeleport, 45, nil, have) {
		t.Error("Camelot Teleport needs five air")
	}
	varrock := map[string]int{"Fire rune": 1, "Air rune": 3, "Law rune": 1}
	haveV := func(r string) int { return varrock[r] }
	if !CanCastWith(VarrockTeleport, 25, nil, haveV) || CanCastWith(VarrockTeleport, 24, nil, haveV) {
		t.Error("Varrock Teleport is level 25 and one fire, three air, one law")
	}
	if CanCastWith("no such spell", 99, nil, func(string) int { return 99 }) {
		t.Error("an unknown spell is never cast")
	}
}

// a user request: "the main account doesnt use camelot teleport to return to the mule ... walked from falador
// all the way to the spirit tree". Camelot Teleport needs Magic 45 and the main account was 39 and 40, so below
// it the way home starts with the Varrock Teleport (25) when the account is on the Falador side, where
// it saves the walk to Varrock's spirit tree; and from anywhere else it starts with nothing, since
// Varrock is east of the mountain the post is west of.
func TestChestHomeSpell(t *testing.T) {
	for _, c := range []struct {
		magic     int
		onFalador bool
		spell     string
		anyway    string
	}{
		{17, true, "", ""},
		{24, true, "", ""},
		{25, true, VarrockTeleport, VarrockTeleport},
		{39, true, VarrockTeleport, VarrockTeleport},
		{40, false, "", VarrockTeleport},
		{44, true, VarrockTeleport, VarrockTeleport},
		{45, true, CamelotTeleport, CamelotTeleport},
		{45, false, CamelotTeleport, CamelotTeleport},
		{99, false, CamelotTeleport, CamelotTeleport},
	} {
		if got := ChestHomeSpellFrom(c.magic, c.onFalador); got != c.spell {
			t.Errorf("magic %d on the Falador side %v: %q, want %q", c.magic, c.onFalador, got, c.spell)
		}
		if got := ChestHomeSpell(c.magic); got != c.anyway {
			t.Errorf("magic %d: %q, want %q", c.magic, got, c.anyway)
		}
	}
	// The kit carries the runes of exactly the spell chosen, one cast of it.
	if got := ChestRunes(40, map[string]bool{RuneAir: true}); !reflect.DeepEqual(got, map[string]int{RuneWater: 6, RuneLaw: 7, RuneFire: 1}) {
		t.Errorf("magic 40 with an air staff: %v", got)
	}
}

// Six keys a trip, topped up from the bank as LootChestFisher does; none at the post when the walk
// to Falador cannot be a teleport (the long walk is made without keys).
func TestChestSixKeysATrip(t *testing.T) {
	if ChestKeysPerTrip != 6 {
		t.Fatalf("keys per trip %d", ChestKeysPerTrip)
	}
	for _, c := range []struct {
		atPost              bool
		magic, banked, held int
		want                int
	}{
		{false, 17, 300, 0, 6},
		{false, 17, 300, 2, 4},
		{false, 17, 3, 0, 3},
		{false, 17, 300, 6, 0},
		{true, 17, 300, 0, 0},
		{true, 37, 300, 0, 6},
	} {
		if got := ChestKeysToDraw(c.atPost, c.magic, c.banked, c.held); got != c.want {
			t.Errorf("%+v: drew %d", c, got)
		}
	}
}

// The free-slot rule: an open only with room for the worst reward, the slot of the key being spent
// counted free (content deletes the key before it adds the reward), and the kit itself always leaves
// that room on an otherwise empty pack.
func TestChestFreeSlotRule(t *testing.T) {
	empty := func(int) int { return 0 }
	worst := lootchestfisherplan.WorstCaseNewSlots(empty)
	if worst != 12 {
		t.Fatalf("worst case on an empty pack %d", worst)
	}
	if ChestRoom(worst-2, empty) || !ChestRoom(worst-1, empty) {
		t.Error("the room rule is the worst case less the slot the key frees")
	}
	// The old rule asked for the whole worst case, so one slot short of it stopped a trip with a key
	// in hand (the main account, 5 Oct 2026, 21:35).
	if lootchestfisherplan.CanOpenAnother(worst-1, empty) {
		t.Error("CanOpenAnother is the rule without the key's slot, and LootChestFisher still uses it")
	}
	for _, magic := range []int{17, 37, 45, 99} {
		for _, paid := range []map[string]bool{nil, {RuneAir: true}} {
			if free := lootchestfisherplan.PackSlots - ChestKitSlots(magic, paid); !ChestRoom(free, empty) {
				t.Errorf("magic %d paid %v: the kit (%d slots) leaves %d free, under the worst case", magic, paid, ChestKitSlots(magic, paid), free)
			}
		}
	}
}

// chestSim plays the round's steps against a toy account: each open spends a key and adds loot
// slots, a bank visit banks the loot and draws keys, the travel steps move it.
type chestSim struct {
	v          ChestView
	magic      int
	freeSlots  int
	opens      int
	expireAt   int
	steps      []ChestStep
	lootPerOpn int
	// muleKeys is what the mule publishes; the sim sets MuleKeysReady from it each step.
	muleKeys int
}

func (c *chestSim) run(t *testing.T) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if c.expireAt > 0 && c.opens >= c.expireAt {
			c.v.Expired = true
		}
		c.v.Room = c.freeSlots+1 >= 12
		c.v.EatForRoom = !c.v.Room && c.v.Fruit > 0 && c.freeSlots+c.v.Fruit+1 >= 12
		c.v.MuleKeysReady = ChestReady(c.muleKeys)
		st := NextChestStep(c.v)
		c.steps = append(c.steps, st)
		switch st {
		case ChestStepDone:
			return
		case ChestStepAsk:
			// What ask() does to the view: a second ask after a resumed round's hand-over starts the
			// round again from the post.
			c.v.Asked, c.v.KeysNoted = true, 8
			c.v.HandedOver, c.v.Resumed, c.v.PostBanked = false, false, false
		case ChestStepEat:
			c.v.Fruit--
			c.freeSlots++
		case ChestStepBank:
			atPost := c.v.Where == ChestAtPost
			c.v.KeysBank += c.v.KeysNoted
			c.v.KeysNoted = 0
			if atPost {
				c.v.PostBanked = true
			}
			if c.v.Expired {
				c.v.KeysBank += c.v.KeysPack
				c.v.KeysPack = 0
			}
			c.v.LootInPack = false
			c.freeSlots = 28 - c.v.KeysPack
			if !c.v.Expired {
				n := ChestKeysToDraw(atPost, c.magic, c.v.KeysBank, c.v.KeysPack)
				c.v.KeysBank -= n
				c.v.KeysPack += n
				c.freeSlots -= n
			}
		case ChestStepToChest:
			c.v.Where = ChestAtChest
		case ChestStepOpen:
			c.v.KeysPack--
			c.opens++
			c.freeSlots += 1 - c.lootPerOpn
			c.v.LootInPack = true
		case ChestStepToFalador:
			c.v.Where = ChestAtFalador
		case ChestStepHome:
			c.v.KeysBank += c.v.KeysPack
			c.v.KeysPack, c.v.LootInPack = 0, false
			c.v.Where = ChestAtPost
		case ChestStepHandover:
			c.v.HandedOver = true
		}
	}
	t.Fatalf("the round never ended: %v", c.steps)
}

func joinSteps(s []ChestStep) string {
	parts := make([]string, len(s))
	for i, x := range s {
		parts[i] = string(x)
	}
	return strings.Join(parts, " ")
}

// The order of the round, walking (Magic 17): ask, bank the keys at the post, walk to Falador with
// none, draw six, open six, back to Falador, bank and draw the rest, open them, bank, home, hand
// over.
func TestChestRoundOrderWalking(t *testing.T) {
	c := &chestSim{v: ChestView{Where: ChestAtPost}, magic: 17, freeSlots: 28, lootPerOpn: 1}
	c.run(t)
	want := "ask bank to-falador bank to-chest open open open open open open to-falador bank to-chest open open to-falador bank home handover done"
	if got := joinSteps(c.steps); got != want {
		t.Errorf("order:\n got %s\nwant %s", got, want)
	}
}

// With the teleports castable the six keys leave from the post (the Falador Teleport is the first
// leg).
func TestChestRoundOrderTeleporting(t *testing.T) {
	c := &chestSim{v: ChestView{Where: ChestAtPost}, magic: 45, freeSlots: 28, lootPerOpn: 1}
	c.run(t)
	want := "ask bank to-chest open open open open open open to-falador bank to-chest open open to-falador bank home handover done"
	if got := joinSteps(c.steps); got != want {
		t.Errorf("order:\n got %s\nwant %s", got, want)
	}
}

// A pack that runs out of room goes back to the bank with keys in hand, and the next trip tops
// them up rather than drawing six more.
func TestChestRoundOrderNoRoom(t *testing.T) {
	c := &chestSim{v: ChestView{Where: ChestAtPost}, magic: 17, freeSlots: 28, lootPerOpn: 4}
	c.run(t)
	got := joinSteps(c.steps)
	// 22 free after drawing six; each open takes 3 net: 22, 19, 16, 13 -> four opens, then 10 < 12.
	if !strings.HasPrefix(got, "ask bank to-falador bank to-chest open open open open to-falador bank to-chest open open open open to-falador") {
		t.Errorf("no-room order: %s", got)
	}
	if c.opens != 8 || !strings.HasSuffix(got, "home handover done") {
		t.Errorf("opens %d: %s", c.opens, got)
	}
}

// When the round's time runs out mid-trip the loot is banked and the account goes home to hand it
// over; the unspent keys stay in the bank for the next round.
func TestChestRoundOrderExpired(t *testing.T) {
	c := &chestSim{v: ChestView{Where: ChestAtPost}, magic: 17, freeSlots: 28, lootPerOpn: 1, expireAt: 3}
	c.run(t)
	want := "ask bank to-falador bank to-chest open open open to-falador home handover done"
	if got := joinSteps(c.steps); got != want {
		t.Errorf("order:\n got %s\nwant %s", got, want)
	}
	if c.v.KeysBank != 5 {
		t.Errorf("keys left in the bank %d, want 5", c.v.KeysBank)
	}
}

func TestChestLedgerOwed(t *testing.T) {
	var c ChestLedger
	c.AddOwed(lootchestfisherplan.Coins, 2000)
	c.AddOwed(lootchestfisherplan.Coins, 750)
	c.AddOwed(lootchestfisherplan.RuniteBar, 3)
	c.AddOwed(0, 5)
	c.AddOwed(lootchestfisherplan.Ruby, 0)
	if c.OwedTotal() != 2753 {
		t.Fatalf("owed %v", c.Owed)
	}
	ids, owed := c.OwedIDs()
	if !reflect.DeepEqual(ids, []int{lootchestfisherplan.Coins, lootchestfisherplan.RuniteBar}) || owed[lootchestfisherplan.Coins] != 2750 {
		t.Errorf("%v %v", ids, owed)
	}
	// 2000 coins crossed; the bank holds only 1 runite bar now.
	c.Settle(map[int]int{lootchestfisherplan.Coins: 2000}, func(id int) int {
		if id == lootchestfisherplan.RuniteBar {
			return 1
		}
		return 1e6
	})
	_, owed = c.OwedIDs()
	if owed[lootchestfisherplan.Coins] != 750 || owed[lootchestfisherplan.RuniteBar] != 1 {
		t.Errorf("after settle %v", owed)
	}
	c.Settle(map[int]int{lootchestfisherplan.Coins: 750, lootchestfisherplan.RuniteBar: 1}, nil)
	if c.Owed != nil || c.OwedTotal() != 0 {
		t.Errorf("settled: %v", c.Owed)
	}
}

func TestNewChestDrops(t *testing.T) {
	st := lootchestfisherplan.ChestStand
	old := GroundStack{ID: lootchestfisherplan.RuniteBar, Count: 1, X: st.X, Z: st.Z}
	before := []GroundStack{old}
	after := []GroundStack{
		old, // was there before the open
		{ID: lootchestfisherplan.RuniteBar, Count: 1, X: st.X, Z: st.Z},
		{ID: lootchestfisherplan.UncutDragonstone, Count: 1, X: st.X, Z: st.Z},
		{ID: 1511, Count: 1, X: st.X, Z: st.Z},                                      // logs: not the chest's
		{ID: lootchestfisherplan.Ruby, Count: 1, X: st.X + 1, Z: st.Z},              // another tile
		{ID: lootchestfisherplan.Ruby, Count: 1, X: st.X, Z: st.Z, Level: st.Level}, // the chest's
	}
	got := NewChestDrops(before, after, st.X, st.Z, st.Level)
	if len(got) != 3 || got[0].ID != lootchestfisherplan.RuniteBar || got[1].ID != lootchestfisherplan.UncutDragonstone || got[2].ID != lootchestfisherplan.Ruby {
		t.Errorf("%+v", got)
	}
	if !ChestDropIDs()[440] || !ChestDropIDs()[453] || !ChestDropIDs()[lootchestfisherplan.LawRune] {
		t.Error("the ores' items and the runes are chest drops")
	}
}

func TestDecodeCrystalChest(t *testing.T) {
	if !orderSettings().CrystalChest {
		t.Error("the chest round is on by default")
	}
	s, err := DecodeSettings(map[string]interface{}{KeyCrystalChest: "false"})
	if err != nil || s.CrystalChest {
		t.Errorf("%v %+v", err, s.CrystalChest)
	}
	if _, err := DecodeSettings(map[string]interface{}{KeyCrystalChest: 3}); err == nil {
		t.Error("a number is not a switch")
	}
}

// A pack too full for the next open that would have the room once its fruit were eaten eats them at
// the chest, then opens; away from the chest it goes to the bank as it always did.
func TestChestEatsFruitForRoom(t *testing.T) {
	v := ChestView{Asked: true, PostBanked: true, Where: ChestAtChest, KeysPack: 3, Fruit: 2, EatForRoom: true}
	if got := NextChestStep(v); got != ChestStepEat {
		t.Errorf("at the chest, no room, fruit would make it: %s", got)
	}
	v.EatForRoom = false // eating them would not make room
	if got := NextChestStep(v); got != ChestStepToFalador {
		t.Errorf("at the chest, no room, fruit would not help: %s", got)
	}
	v.EatForRoom, v.Where = true, ChestAtOther
	if got := NextChestStep(v); got != ChestStepToFalador {
		t.Errorf("on the road: %s", got)
	}
	v.Where, v.Room = ChestAtChest, true
	if got := NextChestStep(v); got != ChestStepOpen {
		t.Errorf("room: %s", got)
	}
	// Out of time: no fruit is eaten, the loot goes home.
	v.Room, v.Expired = false, true
	if got := NextChestStep(v); got == ChestStepEat {
		t.Errorf("expired: %s", got)
	}
}

// The round of a restarted account: on the Falador side with keys of its own, no ask first. It banks
// there (the keys and the kit are drawn), runs the trips, goes home and hands over, as the order
// always went after the ask.
func TestChestRoundOrderResumedFromFalador(t *testing.T) {
	c := &chestSim{v: ChestView{Where: ChestAtFalador, Asked: true, PostBanked: true, Resumed: true, KeysBank: 14},
		magic: 39, freeSlots: 28, lootPerOpn: 1}
	c.v.KeysNoted = 0
	c.run(t)
	want := "bank to-chest open open open open open open to-falador bank to-chest open open open open open open to-falador bank to-chest open open to-falador bank home handover done"
	if got := joinSteps(c.steps); got != want {
		t.Errorf("order:\n got %s\nwant %s", got, want)
	}
	if c.opens != 14 || c.v.KeysBank != 0 {
		t.Errorf("opens %d, keys left in the bank %d: every key it held is spent", c.opens, c.v.KeysBank)
	}
}

// A resumed round on the road, between Falador and the chest, with no keys in the pack: to the bank
// first (the kit), never to the chest with nothing to open.
func TestChestRoundOrderResumedFromTheRoad(t *testing.T) {
	c := &chestSim{v: ChestView{Where: ChestAtOther, Asked: true, PostBanked: true, Resumed: true, KeysBank: 7},
		magic: 39, freeSlots: 28, lootPerOpn: 1}
	c.run(t)
	want := "to-falador bank to-chest open open open open open open to-falador bank to-chest open to-falador bank home handover done"
	if got := joinSteps(c.steps); got != want {
		t.Errorf("order:\n got %s\nwant %s", got, want)
	}
}

// Once a resumed round has handed over, it asks the mule for its keys, as it never did: the keys it
// held were the round's, the mule's are the next round's, and the account is at the post anyway.
func TestChestResumedRoundAsksAfterTheHandover(t *testing.T) {
	c := &chestSim{v: ChestView{Where: ChestAtFalador, Asked: true, PostBanked: true, Resumed: true, KeysBank: 6},
		magic: 39, freeSlots: 28, lootPerOpn: 1, muleKeys: 400}
	c.run(t)
	// The sim's ask hands over 8 keys: a trip of six and a trip of two.
	want := "bank to-chest open open open open open open to-falador bank home handover ask bank to-chest open open open open open open to-falador bank to-chest open open to-falador bank home handover done"
	if got := joinSteps(c.steps); got != want {
		t.Errorf("order:\n got %s\nwant %s", got, want)
	}
	got := joinSteps(c.steps)
	if n := strings.Count(got, "handover"); n != 2 {
		t.Errorf("%d hand-overs in %s", n, got)
	}
	// A mule with too few keys (or none published) is not asked: the round ends at the hand-over.
	c = &chestSim{v: ChestView{Where: ChestAtFalador, Asked: true, PostBanked: true, Resumed: true, KeysBank: 6},
		magic: 39, freeSlots: 28, lootPerOpn: 1, muleKeys: 100}
	c.run(t)
	if got := joinSteps(c.steps); strings.Contains(got, "ask") || !strings.HasSuffix(got, "home handover done") {
		t.Errorf("mule at its gate: %s", got)
	}
	// A normal round (not resumed) never asks twice.
	c = &chestSim{v: ChestView{Where: ChestAtPost}, magic: 39, freeSlots: 28, lootPerOpn: 1, muleKeys: 400}
	c.run(t)
	if n := strings.Count(joinSteps(c.steps), "ask"); n != 1 {
		t.Errorf("%d asks in %s", n, joinSteps(c.steps))
	}
	// And a round out of time hands over and ends.
	c = &chestSim{v: ChestView{Where: ChestAtFalador, Asked: true, PostBanked: true, Resumed: true, KeysBank: 60},
		magic: 39, freeSlots: 28, lootPerOpn: 1, muleKeys: 400, expireAt: 4}
	c.run(t)
	if got := joinSteps(c.steps); strings.Contains(got, "ask") {
		t.Errorf("an expired round asked: %s", got)
	}
}
