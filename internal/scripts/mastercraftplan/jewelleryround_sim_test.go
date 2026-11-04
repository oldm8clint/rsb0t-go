package mastercraftplan

import (
	"fmt"
	"math/rand"
	"testing"
)

// A whole jewellery round played over a 28-slot pack that stacks what content stacks and a bank,
// driven by NextJewelleryStep and the visit's own functions (JewelleryVisitFor, PlanJewelleryDraws,
// JewelleryTravelRunes), the way the bot's round drives them. What it proves, which no single
// decision test does:
//
//   - no load ever overflows the pack, whatever the batch, the Magic level, the staff, the mould, the
//     coins or what the bank held to begin with;
//   - a batch the bank can fund is made in full (every Games necklace enchanted, no more Sapphire
//     necklaces crafted, and no more gold bars and Sapphires used, than the batch asked), and every
//     one is handed over, the round ending at the post;
//   - a bank that cannot fund it (no mould and no coins, no runes) ends the round at once with what
//     could be made handed over, never in a loop;
//   - no step repeats as often as the bot's own guard counts as a failure;
//   - the account's own bars and Sapphires beyond the batch are left alone;
//   - a round that resumes from a loaded pack, or from the bank, carries on and finishes.

type jrow struct {
	id int32
	n  int
}

const (
	jwLaw, jwFire, jwAir = 563, 554, 556
	jwNoteBase           = 100000
)

var jwStacks = map[int32]bool{
	RuneCosmicID: true, RuneWaterID: true, CoinsID: true, jwLaw: true, jwFire: true, jwAir: true,
}

type jewelSim struct {
	t     *testing.T
	label string
	pack  []jrow
	bank  map[int32]int
	where JewelleryPlace
	near  bool
	magic int
	paid  map[string]bool
	led   JewelleryLedger
	view  JewelleryView

	asked, handedOver, futile, shopFailed bool
	// the mule: what it holds and what a round may take
	mule map[string]int
	// counters
	crafted, enchanted, handed, bought, visits int
	ids                                        map[string]int32
	steps                                      []JewelleryStep
}

func newJewelSim(t *testing.T, label string) *jewelSim {
	return &jewelSim{t: t, label: label, bank: map[int32]int{}, where: JewelleryAtPost, magic: 40, paid: map[string]bool{RuneAir: true},
		ids: map[string]int32{RuneLaw: jwLaw, RuneFire: jwFire, RuneAir: jwAir, RuneWater: RuneWaterID, RuneCosmic: RuneCosmicID}}
}

func (s *jewelSim) fatalf(format string, args ...any) {
	s.t.Helper()
	s.t.Fatalf("%s: %s\nsteps: %v", s.label, fmt.Sprintf(format, args...), s.steps)
}

func (s *jewelSim) add(id int32, n int) {
	if n <= 0 {
		return
	}
	if jwStacks[id] || id >= jwNoteBase {
		for i := range s.pack {
			if s.pack[i].id == id {
				s.pack[i].n += n
				return
			}
		}
		s.pack = append(s.pack, jrow{id, n})
	} else {
		for i := 0; i < n; i++ {
			s.pack = append(s.pack, jrow{id, 1})
		}
	}
	if len(s.pack) > JewelleryPackSlots {
		s.fatalf("the pack overflowed (%d rows) at %v", len(s.pack), s.pack)
	}
}

func (s *jewelSim) count(id int32) int {
	n := 0
	for _, r := range s.pack {
		if r.id == id {
			n += r.n
		}
	}
	return n
}

func (s *jewelSim) remove(id int32, n int) {
	for i := 0; i < len(s.pack) && n > 0; {
		r := &s.pack[i]
		if r.id != id {
			i++
			continue
		}
		take := min(n, r.n)
		r.n -= take
		n -= take
		if r.n == 0 {
			s.pack = append(s.pack[:i], s.pack[i+1:]...)
			continue
		}
		i++
	}
	if n > 0 {
		s.fatalf("removed more of item %d than the pack held", id)
	}
}

func (s *jewelSim) held(from func(id int32) int) JewelleryHeld { return JewelleryHeldIn(from) }

func (s *jewelSim) bankOf(name string) int {
	if id, ok := s.ids[name]; ok {
		return s.bank[id]
	}
	return 0
}

func (s *jewelSim) read(expired bool) {
	v := &s.view
	*v = JewelleryView{
		Where: s.where, NearWork: s.near, Asked: s.asked, HandedOver: s.handedOver, Expired: expired,
		Futile: s.futile, ShopFailed: s.shopFailed, CraftOK: true, EnchantOK: true, WaterPaid: s.paid[RuneWater],
		Left: s.led.Left(), Owed: s.led.OwedTotal(),
	}
	v.Pack = s.held(s.count)
	v.Bank = s.held(func(id int32) int { return s.bank[id] })
	for _, r := range s.pack {
		switch r.id {
		case GoldBarID, CutSapphireID, SapphireNecklaceID, GamesNecklaceID, NecklaceMouldID, RuneCosmicID, RuneWaterID, CoinsID, jwLaw, jwFire, jwAir:
		default:
			v.PackOther++
		}
	}
}

// ask is the Ask step: the pack banked, the mule's answer arriving as notes, one row a line.
func (s *jewelSim) ask() {
	for _, r := range s.pack {
		s.bank[bankForm(r.id)] += r.n
	}
	s.pack = nil
	own := s.held(func(id int32) int { return s.bank[id] })
	lines := PlanJewelleryAsk(s.led.Target, s.magic, s.paid, stockMap(s.mule), own, s.bankOf, own.Mould > 0)
	for _, l := range lines {
		id, ok := s.ids[l.Item]
		if !ok {
			switch l.Item {
			case GoldBar:
				id = GoldBarID
			case CutSapphire:
				id = CutSapphireID
			case CoinsName:
				id = CoinsID
			default:
				s.fatalf("the ask names %q", l.Item)
			}
		}
		if l.Qty > s.mule[l.Item] {
			s.fatalf("asked %d %s of the mule's %d", l.Qty, l.Item, s.mule[l.Item])
		}
		s.mule[l.Item] -= l.Qty
		s.add(jwNoteBase+id, l.Qty)
	}
	s.asked = true
	// The post's bank reads what arrived: the round's target is what the ingredients make.
	bank := s.held(func(id int32) int { return s.bank[id] })
	for _, r := range s.pack {
		if r.id >= jwNoteBase {
			switch r.id - jwNoteBase {
			case GoldBarID:
				bank.Bars += r.n
			case CutSapphireID:
				bank.Sapphires += r.n
			}
		}
	}
	pipeline := bank.Necklaces + bank.Pairs()
	s.led.Target = min(s.led.Target, pipeline)
}

func bankForm(id int32) int32 {
	if id >= jwNoteBase {
		return id - jwNoteBase
	}
	return id
}

// bankVisit is the Bank step (masterCrafter's jewellery kit): the whole pack banked, the load drawn.
func (s *jewelSim) bankVisit(expired bool) {
	s.visits++
	for _, r := range s.pack {
		s.bank[bankForm(r.id)] += r.n
	}
	s.pack = nil
	s.read(expired)
	after := s.held(func(id int32) int { return s.bank[id] })
	in := JewelleryVisitFor(s.view, after, s.bankOf, s.magic, s.paid)
	draws := PlanJewelleryDraws(in)
	if s.view.Working() && !JewelleryLoaded(draws) {
		s.futile = true
		return
	}
	for _, d := range draws {
		id := d.ID
		if id == 0 {
			id = s.ids[d.Name]
			if id == 0 {
				s.fatalf("a draw of %q with no id", d.Name)
			}
		}
		if s.bank[id] < d.Count {
			s.fatalf("drew %d of item %d from a bank of %d", d.Count, id, s.bank[id])
		}
		s.bank[id] -= d.Count
		s.add(id, d.Count)
	}
}

func (s *jewelSim) play(expiredAfter int) {
	repeats, last := 0, JewelleryStep("")
	for n := 0; n < 300; n++ {
		s.read(expiredAfter > 0 && n >= expiredAfter)
		step := NextJewelleryStep(s.view)
		s.steps = append(s.steps, step)
		if step == last {
			repeats++
		} else {
			repeats, last = 0, step
		}
		if repeats+1 >= 8 {
			s.fatalf("%s ran %d times in a row (the bot calls that a failure)", step, repeats+1)
		}
		switch step {
		case JewelleryStepDone:
			return
		case JewelleryStepAsk:
			s.ask()
		case JewelleryStepBank:
			s.bankVisit(expiredAfter > 0 && n >= expiredAfter)
		case JewelleryStepToWork:
			s.where, s.near = JewelleryAtWorkBank, true
		case JewelleryStepToFurnace:
			s.where, s.near = JewelleryAtFurnace, true
		case JewelleryStepCraft:
			if s.where != JewelleryAtFurnace || s.count(NecklaceMouldID) == 0 {
				s.fatalf("a craft away from the furnace or without the mould")
			}
			k := min(s.count(GoldBarID), s.count(CutSapphireID))
			if k == 0 {
				s.futile = true
				break
			}
			s.remove(GoldBarID, k)
			s.remove(CutSapphireID, k)
			s.add(SapphireNecklaceID, k)
			s.crafted += k
			s.led.Crafted += k
		case JewelleryStepEnchant:
			k := s.count(SapphireNecklaceID)
			k = min(k, s.count(RuneCosmicID))
			if !s.paid[RuneWater] {
				k = min(k, s.count(RuneWaterID))
			}
			if k == 0 {
				s.futile = true
				break
			}
			for i := 0; i < k; i++ {
				s.remove(SapphireNecklaceID, 1)
				s.add(GamesNecklaceID, 1)
			}
			s.remove(RuneCosmicID, k)
			if !s.paid[RuneWater] {
				s.remove(RuneWaterID, k)
			}
			s.enchanted += k
			s.led.Enchanted += k
			s.led.AddOwed(int(GamesNecklaceID), k)
		case JewelleryStepShop:
			if s.count(CoinsID) < MouldCoins {
				s.fatalf("a shop trip without coins")
			}
			s.remove(CoinsID, 5)
			s.add(NecklaceMouldID, 1)
			s.bought++
			s.where, s.near = JewelleryAtShop, false
		case JewelleryStepHome:
			s.where, s.near = JewelleryAtPost, false
		case JewelleryStepHandover:
			for _, r := range s.pack {
				s.bank[bankForm(r.id)] += r.n
			}
			s.pack = nil
			give := min(s.led.OwedTotal(), s.bank[GamesNecklaceID])
			s.bank[GamesNecklaceID] -= give
			s.handed += give
			s.led.Settle(map[int]int{int(GamesNecklaceID): give}, func(id int) int { return s.bank[int32(id)] })
			s.handedOver = true
		}
	}
	s.fatalf("the round never ended")
}

func TestJewelleryRoundsMakeEveryNecklaceWithoutOverflowingThePack(t *testing.T) {
	rounds, produced := 0, 0
	for _, magic := range []int{25, 37, 40, 45} {
		for _, paid := range []map[string]bool{{RuneAir: true}, {RuneAir: true, RuneWater: true}, nil} {
			for _, mould := range []bool{true, false} {
				for _, ownBars := range []int{0, 17} {
					for _, batch := range []int{1, 4, 9, 10, 11, 20, 23, 50, 67} {
						label := fmt.Sprintf("magic %d paid %v mould %v ownBars %d batch %d", magic, paid, mould, ownBars, batch)
						s := newJewelSim(t, label)
						s.magic, s.paid = magic, paid
						s.mule = map[string]int{GoldBar: 371, CutSapphire: 59102, RuneCosmic: 4374, RuneWater: 211521,
							RuneLaw: 900, RuneFire: 50, CoinsName: 90000}
						s.bank[GoldBarID], s.bank[CutSapphireID] = ownBars, ownBars
						s.bank[CoinsID] = 5000
						if mould {
							s.bank[NecklaceMouldID] = 1
						}
						set := DefaultSettings()
						set.JewelleryBatch = batch
						plan := PlanJewelleryBatch(set, stockMap(s.mule), paid[RuneWater])
						if plan.N != batch {
							t.Fatalf("%s: the plan is %d", label, plan.N)
						}
						s.led.StartBatch(plan.N)
						s.play(0)
						rounds++
						produced += s.enchanted
						if s.led.Enchanted != batch || s.enchanted != batch || s.led.Crafted != batch || s.crafted != batch {
							s.fatalf("made %d crafted %d (ledger %d/%d) of %d", s.enchanted, s.crafted, s.led.Enchanted, s.led.Crafted, batch)
						}
						if s.handed != batch || s.led.OwedTotal() != 0 || s.bank[GamesNecklaceID] != 0 {
							s.fatalf("handed %d of %d, owed %d, %d still banked", s.handed, batch, s.led.OwedTotal(), s.bank[GamesNecklaceID])
						}
						if s.where != JewelleryAtPost {
							s.fatalf("the round ended at %v, not the post", s.where)
						}
						if s.bank[GoldBarID] != ownBars || s.bank[CutSapphireID] != ownBars {
							s.fatalf("the account's own bars and Sapphires changed: %d and %d, had %d", s.bank[GoldBarID], s.bank[CutSapphireID], ownBars)
						}
						if wantBought := boolInt(!mould); s.bought != wantBought || s.bank[NecklaceMouldID] != 1 {
							s.fatalf("bought %d moulds, %d in the bank (had one: %v)", s.bought, s.bank[NecklaceMouldID], mould)
						}
						if want := 371 - batch; s.mule[GoldBar] != want {
							s.fatalf("the mule gave %d gold bars for a batch of %d", 371-s.mule[GoldBar], batch)
						}
						if want := 59102 - batch; s.mule[CutSapphire] != want {
							s.fatalf("the mule gave %d Sapphires for a batch of %d", 59102-s.mule[CutSapphire], batch)
						}
					}
				}
			}
		}
	}
	if rounds == 0 || produced == 0 {
		t.Fatal("nothing ran")
	}
}

// A bank that cannot fund the batch ends the round without a loop, handing over what could be made.
func TestJewelleryRoundsThatCannotBeFunded(t *testing.T) {
	for name, setup := range map[string]func(s *jewelSim){
		"no mould and no coins": func(s *jewelSim) { delete(s.bank, NecklaceMouldID); delete(s.bank, CoinsID); s.mule[CoinsName] = 0 },
		"no Cosmic in the bank": func(s *jewelSim) { s.mule[RuneCosmic] = 0 },
		"no Water":              func(s *jewelSim) { s.mule[RuneWater] = 0 },
		"no Sapphires":          func(s *jewelSim) { s.mule[CutSapphire] = 0 },
		"no gold bars":          func(s *jewelSim) { s.mule[GoldBar] = 0 },
		"a mule that is empty":  func(s *jewelSim) { s.mule = map[string]int{} },
	} {
		s := newJewelSim(t, name)
		s.mule = map[string]int{GoldBar: 371, CutSapphire: 59102, RuneCosmic: 4374, RuneWater: 211521, RuneLaw: 900, RuneFire: 50, CoinsName: 90000}
		s.bank[CoinsID], s.bank[NecklaceMouldID] = 5000, 1
		setup(s)
		s.led.StartBatch(30)
		s.play(0)
		if s.where != JewelleryAtPost {
			s.fatalf("ended at %v", s.where)
		}
		if s.handed != s.enchanted || s.led.OwedTotal() != 0 {
			s.fatalf("made %d handed %d owed %d", s.enchanted, s.handed, s.led.OwedTotal())
		}
		if s.enchanted != 0 {
			s.fatalf("made %d necklaces from nothing", s.enchanted)
		}
	}
}

// The round's time running out stops the loads and still takes everything made home to the mule.
func TestJewelleryRoundThatRunsOutOfTime(t *testing.T) {
	for _, after := range []int{3, 6, 9, 14, 25} {
		s := newJewelSim(t, fmt.Sprintf("expired after %d steps", after))
		s.mule = map[string]int{GoldBar: 371, CutSapphire: 59102, RuneCosmic: 4374, RuneWater: 211521, RuneLaw: 900, RuneFire: 50}
		s.bank[CoinsID], s.bank[NecklaceMouldID] = 5000, 1
		s.led.StartBatch(60)
		s.play(after)
		if s.handed != s.enchanted || s.led.OwedTotal() != 0 || s.where != JewelleryAtPost {
			s.fatalf("made %d handed %d owed %d at %v", s.enchanted, s.handed, s.led.OwedTotal(), s.where)
		}
		if s.crafted-s.enchanted > 0 && s.count(SapphireNecklaceID)+s.bank[SapphireNecklaceID] != s.crafted-s.enchanted {
			s.fatalf("necklaces unaccounted for")
		}
	}
}

// Rounds that carry on from what an earlier one left.
func TestJewelleryRoundsResume(t *testing.T) {
	// From the bank: Sapphire necklaces and the runes, at the post. The round never leaves it.
	s := newJewelSim(t, "necklaces in the bank")
	s.asked = true
	s.bank[SapphireNecklaceID], s.bank[RuneCosmicID], s.bank[RuneWaterID] = 37, 100, 100
	s.led.StartBatch(37)
	s.play(0)
	if s.enchanted != 37 || s.handed != 37 || s.crafted != 0 || s.visits < 3 {
		s.fatalf("enchanted %d handed %d crafted %d in %d visits", s.enchanted, s.handed, s.crafted, s.visits)
	}
	for _, st := range s.steps {
		switch st {
		case JewelleryStepToWork, JewelleryStepToFurnace, JewelleryStepHome, JewelleryStepCraft, JewelleryStepShop, JewelleryStepAsk:
			s.fatalf("an enchant-only round took step %s", st)
		}
	}

	// From the furnace, with a loaded pack, mid-batch: 30 made, 30 to go, ten in the pack.
	s = newJewelSim(t, "a loaded pack at the furnace")
	s.asked, s.where, s.near = true, JewelleryAtFurnace, true
	s.bank[GoldBarID], s.bank[CutSapphireID], s.bank[NecklaceMouldID] = 20, 20, 0
	s.bank[RuneCosmicID], s.bank[RuneWaterID] = 100, 100
	for _, d := range []struct {
		id int32
		n  int
	}{{NecklaceMouldID, 1}, {GoldBarID, 10}, {CutSapphireID, 10}, {RuneCosmicID, 10}, {RuneWaterID, 10}} {
		s.add(d.id, d.n)
	}
	s.led.StartBatch(60)
	s.led.Crafted, s.led.Enchanted = 30, 30
	s.led.AddOwed(int(GamesNecklaceID), 30)
	s.bank[GamesNecklaceID] = 30
	s.play(0)
	if s.led.Enchanted != 60 || s.handed != 60 || s.where != JewelleryAtPost || s.led.OwedTotal() != 0 {
		s.fatalf("enchanted %d handed %d at %v owed %d", s.led.Enchanted, s.handed, s.where, s.led.OwedTotal())
	}
	if s.steps[0] != JewelleryStepCraft {
		s.fatalf("the first step was %s, want craft", s.steps[0])
	}

	// Necklaces owed and sitting in the bank: only the hand-over.
	s = newJewelSim(t, "owed")
	s.asked = true
	s.bank[GamesNecklaceID] = 12
	s.led.AddOwed(int(GamesNecklaceID), 12)
	s.play(0)
	if s.handed != 12 || len(s.steps) != 2 {
		s.fatalf("handed %d in steps %v", s.handed, s.steps)
	}

	// A necklace the account carries for its own run stalling is not owed, so it is not handed over.
	s = newJewelSim(t, "an own Games necklace")
	s.asked = true
	s.bank[GamesNecklaceID] = 13 // 12 owed + 1 its own
	s.led.AddOwed(int(GamesNecklaceID), 12)
	s.play(0)
	if s.handed != 12 || s.bank[GamesNecklaceID] != 1 {
		s.fatalf("handed %d, %d left in the bank", s.handed, s.bank[GamesNecklaceID])
	}
}

// Randomized: the same round from random states never loops, overflows or loses a necklace.
func TestJewelleryRandomRoundsNeverLoopOrLoseANecklace(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 3000; i++ {
		s := newJewelSim(t, fmt.Sprintf("random round %d", i))
		s.magic = []int{25, 37, 40, 45}[rng.Intn(4)]
		if rng.Intn(3) == 0 {
			s.paid = map[string]bool{RuneAir: true, RuneWater: true}
		}
		s.mule = map[string]int{GoldBar: rng.Intn(300), CutSapphire: rng.Intn(300), RuneCosmic: rng.Intn(300), RuneWater: rng.Intn(300),
			RuneLaw: rng.Intn(5), RuneFire: rng.Intn(5), CoinsName: rng.Intn(2) * 1000}
		s.bank[CoinsID] = rng.Intn(2) * 500
		s.bank[NecklaceMouldID] = rng.Intn(2)
		if rng.Intn(4) == 0 {
			s.bank[SapphireNecklaceID] = rng.Intn(40)
			s.bank[RuneCosmicID], s.bank[RuneWaterID] = rng.Intn(60), rng.Intn(60)
		}
		if rng.Intn(4) == 0 {
			s.bank[jwLaw], s.bank[jwFire] = 3, 3 // law and fire runes of its own: the ids are the sim's
		}
		s.bank[GoldBarID], s.bank[CutSapphireID] = rng.Intn(3)*10, rng.Intn(3)*10
		set := DefaultSettings()
		set.JewelleryBatch = 1 + rng.Intn(80)
		plan := PlanJewelleryBatch(set, stockMap(s.mule), s.paid[RuneWater])
		target := plan.N
		if target == 0 {
			target = s.bank[SapphireNecklaceID]
		}
		s.led.StartBatch(target)
		expired := 0
		if rng.Intn(5) == 0 {
			expired = 3 + rng.Intn(20)
		}
		s.play(expired)
		if s.where != JewelleryAtPost {
			s.fatalf("ended at %v", s.where)
		}
		if s.led.OwedTotal() != 0 || s.handed != s.enchanted {
			s.fatalf("made %d handed %d owed %d", s.enchanted, s.handed, s.led.OwedTotal())
		}
		if s.enchanted > target {
			s.fatalf("made %d of a batch of %d", s.enchanted, target)
		}
		left := s.count(SapphireNecklaceID) + s.bank[SapphireNecklaceID]
		runes := s.count(RuneCosmicID)+s.bank[RuneCosmicID] > 0 && (s.paid[RuneWater] || s.count(RuneWaterID)+s.bank[RuneWaterID] > 0)
		if expired == 0 && !s.futile && s.enchanted < target && left > 0 && runes {
			s.fatalf("stopped at %d of %d with %d Sapphire necklaces and runes left", s.enchanted, target, left)
		}
	}
}
