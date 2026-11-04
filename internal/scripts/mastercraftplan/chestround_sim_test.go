package mastercraftplan

import (
	"math/rand"
	"testing"

	"rsb0t-go/internal/scripts/lootchestfisherplan"
)

// A whole chest round played over the real drop table and a 28-slot pack that stacks what content
// stacks, driven by NextChestStep and the kit's own functions (ChestRoom, ChestRoomWithFruit,
// ChestPackHasLoot, ChestKitWant, PlanChestDraws). What it proves, which no single decision test does:
//
//   - no reward ever overflows the pack (content drops the overflow at the player's feet, where the
//     round would have to pick it up), whatever the rolls, the kit, the staff or the Magic level;
//   - every key held is spent, and the round ends at the post with the loot handed over;
//   - no step repeats as often as the bot's own guard (masterChestRepeats, 8) counts as a failure, so
//     a trip that is working is never reported as stuck;
//   - the kit itself is never taken for loot, so no bank visit is made for nothing.

// roundSim is one round's account: a pack of rows in slot order and the bank's keys.
type roundSim struct {
	t        *testing.T
	rng      *rand.Rand
	magic    int
	paid     map[string]bool
	pack     []BankRow
	bankKeys int
	view     ChestView
	opened   int
	eaten    int
	visits   int
	steps    []ChestStep
}

var simStacks = map[int]bool{
	995: true, // coins
	554: true, 555: true, 556: true, 557: true, 558: true, 559: true, 560: true, 561: true, 562: true, 563: true, 564: true,
	lootchestfisherplan.NotedIronOre: true, lootchestfisherplan.NotedCoal: true,
}

var simKit = KitIDs{
	Keys:  []int{lootchestfisherplan.CrystalKey},
	Runes: map[string][]int{RuneAir: {556}, RuneWater: {555}, RuneLaw: {563}, RuneFire: {554}},
	Coins: []int{995},
	Fruit: []int{464},
}

func (s *roundSim) add(id, n int) {
	if simStacks[id] {
		for i := range s.pack {
			if s.pack[i].ID == id {
				s.pack[i].Count += n
				return
			}
		}
		s.pack = append(s.pack, BankRow{ID: id, Count: n})
		return
	}
	for i := 0; i < n; i++ {
		s.pack = append(s.pack, BankRow{ID: id, Count: 1})
	}
}

func (s *roundSim) count(id int) int {
	n := 0
	for _, r := range s.pack {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

func (s *roundSim) drop(id int) bool {
	for i, r := range s.pack {
		if r.ID == id {
			s.pack = append(s.pack[:i], s.pack[i+1:]...)
			return true
		}
	}
	return false
}

// read is the bot's masterChest.read over the simulated pack.
func (s *roundSim) read(expired bool) {
	v := &s.view
	free := lootchestfisherplan.PackSlots - len(s.pack)
	held := func(id int) int { return s.count(id) }
	v.KeysPack = s.count(lootchestfisherplan.CrystalKey)
	v.KeysBank = s.bankKeys
	v.Room = ChestRoom(free, held)
	v.Fruit = s.count(464)
	v.EatForRoom = !v.Room && v.Fruit > 0 && ChestRoomWithFruit(free, v.Fruit, held)
	v.LootInPack = ChestPackHasLoot(s.pack, simKit, ChestRunes(s.magic, s.paid))
	v.Expired = expired
}

// bank is a kit visit: the whole pack goes in, the kit comes out (mastercraftrunner.ChestKit).
func (s *roundSim) bank(atPost, expired bool) {
	s.visits++
	s.bankKeys += s.count(lootchestfisherplan.CrystalKey)
	s.pack = nil
	want := ChestKitWant(atPost, s.magic, s.paid, expired, s.bankKeys)
	bank := []BankRow{{ID: lootchestfisherplan.CrystalKey, Count: s.bankKeys}, {ID: 556, Count: 1 << 20}, {ID: 555, Count: 1 << 20},
		{ID: 563, Count: 1 << 20}, {ID: 554, Count: 1 << 20}, {ID: 995, Count: 1 << 30}, {ID: 464, Count: 1000}}
	for _, d := range PlanChestDraws(bank, simKit, want) {
		s.add(d.ID, d.Count)
		if d.ID == lootchestfisherplan.CrystalKey {
			s.bankKeys -= d.Count
		}
	}
}

// open spends a key, adds the dragonstone and one roll of the table, and reports whether the pack
// holds more than it can: content's inv_add drops what does not fit.
func (s *roundSim) open() (overflow bool) {
	if !s.drop(lootchestfisherplan.CrystalKey) {
		s.t.Fatal("an open with no key in the pack")
	}
	s.add(lootchestfisherplan.UncutDragonstone, 1)
	roll, acc := s.rng.Intn(lootchestfisherplan.RandomRange), 0
	for _, b := range lootchestfisherplan.Table() {
		acc += b.Weight
		if roll < acc {
			for _, r := range b.Rewards {
				s.add(r.ID, r.Count)
			}
			break
		}
	}
	s.opened++
	return len(s.pack) > lootchestfisherplan.PackSlots
}

// play runs one round from the mule's post (or, resumed, from the Falador bank) with keys in the bank.
func (s *roundSim) play(keys int, resumed bool) {
	s.bankKeys = keys
	s.view = ChestView{Where: ChestAtPost}
	if resumed {
		s.view = ChestView{Where: ChestAtFalador, Asked: true, PostBanked: true, Resumed: true}
	}
	repeats, last := 0, ChestStep("")
	for n := 0; n < 40+10*keys; n++ {
		s.read(false)
		step := NextChestStep(s.view)
		s.steps = append(s.steps, step)
		if step == last {
			repeats++
		} else {
			repeats, last = 0, step
		}
		if repeats+1 >= 8 {
			s.t.Fatalf("magic %d paid %v keys %d: %s ran %d times in a row (the bot calls that a failure): %v", s.magic, s.paid, keys, step, repeats+1, s.steps)
		}
		switch step {
		case ChestStepDone:
			return
		case ChestStepAsk:
			s.view.Asked = true
		case ChestStepBank:
			atPost := s.view.Where == ChestAtPost
			s.bank(atPost, false)
			if atPost {
				s.view.PostBanked = true
			}
		case ChestStepToChest:
			s.view.Where = ChestAtChest
		case ChestStepOpen:
			if s.open() {
				s.t.Fatalf("magic %d paid %v keys %d: a reward overflowed the pack after %d opens: %v", s.magic, s.paid, keys, s.opened, s.pack)
			}
		case ChestStepEat:
			if !s.drop(464) {
				s.t.Fatal("ate a fruit it does not hold")
			}
			s.eaten++
		case ChestStepToFalador:
			s.view.Where = ChestAtFalador
		case ChestStepHome:
			if s.view.Where == ChestAtFalador && (s.view.LootInPack || s.view.KeysPack > 0) {
				s.bank(false, true)
			}
			s.view.Where = ChestAtPost
		case ChestStepHandover:
			s.view.HandedOver = true
		}
	}
	s.t.Fatalf("magic %d paid %v keys %d: the round never ended: %v", s.magic, s.paid, keys, s.steps)
}

func TestRandomRoundsNeverOverflowThePackAndSpendEveryKey(t *testing.T) {
	var opened, visits, eaten, rounds, trips int
	for _, magic := range []int{25, 37, 39, 45} {
		for _, staff := range []bool{false, true} {
			var paid map[string]bool
			if staff {
				paid = map[string]bool{RuneAir: true}
			}
			for _, keys := range []int{1, 2, 5, 6, 7, 12, 40, 85} {
				for _, resumed := range []bool{false, true} {
					if resumed && magic < 37 {
						continue
					}
					for seed := int64(1); seed <= 25; seed++ {
						s := &roundSim{t: t, rng: rand.New(rand.NewSource(seed*1000 + int64(keys))), magic: magic, paid: paid}
						s.play(keys, resumed)
						if s.opened != keys || s.bankKeys != 0 || s.count(lootchestfisherplan.CrystalKey) != 0 {
							t.Fatalf("magic %d paid %v keys %d resumed %v: %d opened, %d left in the bank, %d in the pack: %v", magic, paid, keys, resumed, s.opened, s.bankKeys, s.count(lootchestfisherplan.CrystalKey), s.steps)
						}
						if s.view.Where != ChestAtPost || !s.view.HandedOver {
							t.Fatalf("magic %d keys %d: the round ended at %v, handed over %v", magic, keys, s.view.Where, s.view.HandedOver)
						}
						opened += s.opened
						visits += s.visits
						eaten += s.eaten
						rounds++
						for _, st := range s.steps {
							if st == ChestStepToChest {
								trips++
							}
						}
					}
				}
			}
		}
	}
	t.Logf("%d rounds, %d chests opened over %d trips (%.2f a trip), %d bank visits, %d Strange fruit eaten for room", rounds, opened, trips, float64(opened)/float64(trips), visits, eaten)
	if float64(opened)/float64(trips) < 4.5 {
		t.Errorf("only %.2f chests opened a trip: the kit leaves too little room", float64(opened)/float64(trips))
	}
}
