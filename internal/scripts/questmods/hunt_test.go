package questmods

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Pirate's Treasure through the real engine (Engine.Run: the watchdog, the Ran/Failed bookkeeping that tells the module which of its
// customs ran, the completion record), with every step's EXECUTOR stubbed. Each step the module decides is turned into a stub
// that plays the content's own rule for it on a small world (huntState: the three smuggle varps, the account's place, the pack
// and the apron) and a simulated pack (actionsim). The module's Decide is the real one; only the quest tab's colour, the journal
// stage (from the world, by the same semantics the questplan test checks the parser against), the account's tile and the worn
// apron, which no simulated session supplies, come from the rig. The live executors (the dialogue driver, the walker, the
// fight loop, the crate readings) have their own tests; what this proves is the order, the memory and the safety of the whole.

// huntState is the content's state for the quest.
type huntState struct {
	hunt, employed, crateRum, bananas int
	at                                tile.Tile
	apronWorn                         bool
	shelf                             int // rums on Zambo's shelf
	cratesLie                         bool
	violations                        []string
	blocked                           map[string]bool
}

type huntRig struct {
	*fillerRig
	w huntState
}

func newHuntRig(t *testing.T, hunt int, at tile.Tile, pack, bank map[int]int) *huntRig {
	r := &huntRig{fillerRig: newFillerRig(t, 0, pack, bank)}
	r.w = huntState{hunt: hunt, at: at, shelf: 3, blocked: map[string]bool{}}
	return r
}

// stage is the synthetic stage the journal reads: the questplan test's oracle over the world.
func (r *huntRig) stage() int {
	w := r.w
	rumHeld := r.held(questplan.ObjKaramjaRum) > 0
	switch w.hunt {
	case 0:
		return questplan.HuntNotStarted
	case 2:
		return questplan.HuntReceivedKey
	case 3:
		return questplan.HuntReadNote
	case 4:
		return questplan.HuntComplete
	}
	E, R := w.employed, w.crateRum
	atPlantation, atStore := E&questplan.HuntEmployedPlantation != 0, E&questplan.HuntEmployedStore != 0
	switch {
	case R == questplan.HuntRumInStore && !atStore:
		return questplan.HuntShipped
	case !atStore && !atPlantation:
		if rumHeld {
			return questplan.HuntRumUnemployed
		}
		return questplan.HuntNeedRum
	case atPlantation && !atStore:
		switch {
		case R == questplan.HuntRumInPlantation && w.bananas >= questplan.HuntCrateCapacity:
			return questplan.HuntCrateFull
		case R == questplan.HuntRumInPlantation:
			return questplan.HuntRumInCrate
		case rumHeld:
			return questplan.HuntRumHeldEmployed
		}
		return questplan.HuntEmployedNeedRum
	}
	switch {
	case R == questplan.HuntRumNowhere && rumHeld:
		return questplan.HuntRumInHand
	case R == questplan.HuntRumNowhere:
		return questplan.HuntRumLost
	case atPlantation && R == questplan.HuntRumInPlantation, R == questplan.HuntRumInStore:
		return questplan.HuntStoreJob
	}
	return questplan.HuntFetchRum
}

// cross moves the account to a tile, paying the ships' fares and meeting the customs officer: a rum carried off the island is
// confiscated (and a violation: nothing in the quest may do it), and a walk without the 30 coins goes nowhere.
func (r *huntRig) cross(to tile.Tile, why string) bool {
	from := r.w.at
	if questplan.OnKaramja(from) != questplan.OnKaramja(to) {
		if !r.take(questplan.CoinsID, 30) {
			r.stubLog("%s: no 30 coins for the ship", why)
			return false
		}
		if questplan.OnKaramja(from) && r.held(questplan.ObjKaramjaRum) > 0 {
			r.w.violations = append(r.w.violations, why+": the customs officer found a rum in the pack")
			r.take(questplan.ObjKaramjaRum, r.held(questplan.ObjKaramjaRum))
		}
	}
	r.w.at = to
	return true
}

func (r *huntRig) apronOwned() bool { return r.w.apronWorn || r.held(questplan.ObjWhiteApron) > 0 }

// provision runs the real planner over the rig, with the apron worn counting as held and the shelf of Thessalia's as the source
// of an apron; it reports the plan's block, as the engine's Snapshot.Blocked would (the rig turns provisioning into a custom step).
func (r *huntRig) provisionKit(name string, kit []questplan.KitLine) bool {
	for pass := 0; pass < 16; pass++ {
		pack := r.pack()
		used := 0
		for id, n := range pack {
			if id == questplan.CoinsID {
				used++
			} else {
				used += n
			}
		}
		worn := map[int]int{}
		if r.w.apronWorn {
			worn[questplan.ObjWhiteApron] = 1
		}
		plan := questplan.PlanProvision(kit, questplan.ProvisionState{Pack: pack, Worn: worn, Bank: r.bank, BankKnown: true, FreeSlots: 28 - used,
			Levels: map[string]int{}, Exhausted: map[string]bool{}})
		switch plan.Phase {
		case questplan.PhaseSatisfied:
			return true
		case questplan.PhaseBank:
			moved := false
			for _, c := range plan.Withdraw {
				if n := min(c.Count, r.bank[c.ID]); n > 0 {
					r.bank[c.ID] -= n
					r.give(c.ID, n)
					moved = true
				}
			}
			if !moved {
				r.w.blocked[name] = true
				return false
			}
		case questplan.PhaseSource:
			task := plan.Tasks[0]
			r.give(task.Line.ID, task.Qty)
		default:
			r.w.blocked[name] = true
			return false
		}
	}
	return false
}

// wrap turns the module into stubs: the real Decide over a snapshot that carries the rig's journal, tile and worn apron.
func (r *huntRig) wrap(q questrunner.Quest, stubs map[string]questrunner.CustomFunc) questrunner.Quest {
	real := q.Module.Decide
	q.Module.ReadStage = nil
	q.Module.Decide = func(s questplan.Snapshot) questplan.Step {
		switch r.w.hunt {
		case 0:
			s.Journal, s.StageKnown = questplan.StatusNotStarted, false
		case 4:
			s.Journal = questplan.StatusComplete
		default:
			s.Journal, s.Stage, s.StageKnown = questplan.StatusInProgress, r.stage(), true
		}
		s.Tile, s.TileKnown = r.w.at, true
		if r.w.apronWorn {
			s.WornIDs = map[int]int{questplan.ObjWhiteApron: 1}
		}
		for k := range r.w.blocked {
			s.Blocked["provision:"+k] = true
		}
		st := real(s)
		switch st.Kind {
		case questplan.StepDone, questplan.StepWait:
			return st
		}
		st.Reason = st.Describe()
		switch st.Kind {
		case questplan.StepProvision:
			st.Kind, st.Custom = questplan.StepCustom, "provision:"+st.KitName
		case questplan.StepCustom:
		default:
			st.Kind, st.Custom = questplan.StepCustom, "stub:"+string(st.Kind)
		}
		return st
	}
	q.Customs = stubs
	return q
}

func (r *huntRig) stubs() map[string]questrunner.CustomFunc {
	m := map[string]questrunner.CustomFunc{}
	for _, kit := range []string{questplan.KitHuntSupplies, questplan.KitHuntSpade} {
		kit := kit
		m["provision:"+kit] = func(e *questrunner.Engine, st questplan.Step) bool {
			r.stubLog("provision %s", kit)
			bank := st.Bank
			if !r.cross(bank, "the bank") {
				return false
			}
			return r.provisionKit(kit, st.Kit)
		}
	}
	m["stub:talk"] = func(e *questrunner.Engine, st questplan.Step) bool {
		w := &r.w
		switch st.Stop.ID {
		case questplan.NpcRedbeardFrank:
			if !r.cross(questplan.FrankAnchor, "Frank") {
				return false
			}
			switch w.hunt {
			case 0:
				w.hunt = 1
			case 1:
				if r.take(questplan.ObjKaramjaRum, 1) {
					w.hunt = 2
					r.give(questplan.ObjChestKey, 1)
				}
			case 2:
				if r.held(questplan.ObjChestKey) == 0 {
					r.give(questplan.ObjChestKey, 1)
				}
			}
		case questplan.NpcLuthas:
			if !r.cross(questplan.LuthasAnchor, "Luthas") {
				return false
			}
			w.employed |= questplan.HuntEmployedPlantation
		case questplan.NpcWydin:
			if !r.cross(questplan.WydinShop.Anchor, "Wydin") {
				return false
			}
			if !r.apronOwned() {
				return false
			}
			w.employed |= questplan.HuntEmployedStore
		}
		r.stubLog("talk %d -> stage %d", st.Stop.ID, r.stage())
		return true
	}
	m["stub:equip"] = func(e *questrunner.Engine, st questplan.Step) bool {
		if !r.take(st.ItemID, 1) {
			return false
		}
		r.w.apronWorn = true
		return true
	}
	m[questplan.CustomHuntChest] = func(e *questrunner.Engine, st questplan.Step) bool { // the key on the chest
		if !r.cross(questplan.ChestStand, "the chest") || !r.take(questplan.ObjChestKey, 1) {
			return false
		}
		r.give(questplan.ObjPirateMessage, 1)
		return true
	}
	m["stub:opHeld"] = func(e *questrunner.Engine, st questplan.Step) bool { // the message read
		if r.held(questplan.ObjPirateMessage) == 0 {
			return false
		}
		if r.w.hunt == 2 {
			r.w.hunt = 3
		}
		return true
	}
	m[questplan.CustomHuntRum] = func(e *questrunner.Engine, st questplan.Step) bool {
		if !r.cross(questplan.ZamboShop.Anchor, "Zambo") {
			return false
		}
		if r.w.shelf == 0 || r.held(questplan.CoinsID) < questrunner.FareReserve+questplan.HuntRumPrice {
			return false
		}
		r.take(questplan.CoinsID, questplan.HuntRumPrice)
		r.give(questplan.ObjKaramjaRum, 1)
		r.w.shelf--
		r.stubLog("rum bought; %d coins left", r.held(questplan.CoinsID))
		return true
	}
	m[questplan.CustomHuntShip] = func(e *questrunner.Engine, st questplan.Step) bool {
		w := &r.w
		if !r.cross(questplan.PlantationCrate, "the plantation") {
			return false
		}
		switch {
		case w.crateRum == questplan.HuntRumInStore && !r.w.cratesLie:
			// "There is already some rum in Wydin's store...": nothing to ship, and a rum in the pack is spare (dropped).
			r.take(questplan.ObjKaramjaRum, r.held(questplan.ObjKaramjaRum))
			r.stubLog("ship: the crate sailed already")
			return true
		case w.crateRum == questplan.HuntRumNowhere || r.w.cratesLie:
			if r.held(questplan.ObjKaramjaRum) == 0 || r.w.cratesLie {
				r.stubLog("ship: no rum in the crate and none carried")
				return false
			}
			if w.employed&questplan.HuntEmployedPlantation == 0 { // the crate refuses: Why would I want to do that?
				r.stubLog("ship: the crate refused the rum; Luthas hires again")
				w.employed |= questplan.HuntEmployedPlantation
			}
			r.take(questplan.ObjKaramjaRum, 1)
			w.crateRum = questplan.HuntRumInPlantation
		default: // the crate holds the rum: a rum in the pack is spare (dropped)
			r.take(questplan.ObjKaramjaRum, r.held(questplan.ObjKaramjaRum))
		}
		w.bananas = questplan.HuntCrateCapacity
		// Luthas: pays 30, clears the employment, ships the crate.
		r.give(questplan.CoinsID, 30)
		w.employed &^= questplan.HuntEmployedPlantation
		w.bananas = 0
		w.crateRum = questplan.HuntRumInStore
		r.stubLog("ship: the crate has sailed")
		return true
	}
	m[questplan.CustomHuntCollect] = func(e *questrunner.Engine, st questplan.Step) bool {
		if !r.cross(questplan.WydinShop.Anchor, "Wydin's") {
			return false
		}
		if !r.w.apronWorn || r.w.crateRum != questplan.HuntRumInStore || r.w.cratesLie {
			r.stubLog("collect: nothing in the back room's crate")
			return false
		}
		r.w.crateRum = questplan.HuntRumNowhere
		r.give(questplan.ObjKaramjaRum, 1)
		return true
	}
	m[questplan.CustomHuntDig] = func(e *questrunner.Engine, st questplan.Step) bool {
		if !r.cross(questplan.DigSite, "the X") || r.held(questplan.ObjSpade) == 0 {
			return false
		}
		if r.w.hunt == 3 {
			r.w.hunt = 4
		}
		return true
	}
	return m
}

func (r *huntRig) run(budget time.Duration) (questrunner.Result, *questrunner.Engine, string) {
	e := r.engine()
	res := e.Run(r.wrap(PiratesTreasure(), r.stubs()), budget)
	return res, e, strings.Join(r.log, "\n")
}

func (r *huntRig) assertSafe(t *testing.T) {
	t.Helper()
	if len(r.w.violations) != 0 {
		t.Errorf("the quest broke a rule: %v\n%s", r.w.violations, strings.Join(r.log, "\n"))
	}
}

var huntPortSarimTile = questplan.FrankAnchor

// The factory builds the quest, every custom the module can ask for has an executor, and nothing else is registered.
func TestPiratesTreasureRegistersEveryCustomItAsksFor(t *testing.T) {
	q := PiratesTreasure()
	if q.ID() != questplan.PiratesTreasureID || q.Module.Record.Name != "Pirate's Treasure" || q.Module.Decide == nil {
		t.Fatalf("built %q %q", q.ID(), q.Module.Record.Name)
	}
	want := map[string]bool{questplan.CustomHuntRum: true, questplan.CustomHuntShip: true, questplan.CustomHuntCollect: true, questplan.CustomHuntChest: true, questplan.CustomHuntDig: true}
	if len(q.Customs) != len(want) {
		t.Errorf("customs %v", q.Customs)
	}
	for name := range want {
		if q.Customs[name] == nil {
			t.Errorf("no executor for %q", name)
		}
	}
}

// A fresh account with coins and an apron in the bank goes through the whole quest in the order the geography and the customs
// officer want, and never carries the rum off the island.
func TestPiratesTreasureRunsToCompleteThroughTheEngine(t *testing.T) {
	r := newHuntRig(t, 0, huntPortSarimTile, map[int]int{questplan.CoinsID: 50},
		map[int]int{questplan.CoinsID: 5000, questplan.ObjWhiteApron: 1, questplan.ObjSpade: 1})
	res, e, joined := r.run(30 * time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %s", res.Outcome, res.Reason, joined)
	}
	r.assertSafe(t)
	if r.w.hunt != 4 {
		t.Errorf("hunt %d at the end", r.w.hunt)
	}
	if cs := e.Store.Record(questplan.PiratesTreasureID).Completions; len(cs) != 1 || !cs[0].Earned() {
		t.Errorf("completions %+v", cs)
	}
	// The order: Frank, the supplies, the rum, Luthas's job, the crate, Wydin's job, the apron on, the back room, Frank, the chest,
	// the message, the spade, the dig.
	last := -1
	for _, want := range []string{"talk to Redbeard Frank", "provision hunt-supplies", "hunt-rum", "talk to Luthas", "hunt-ship", "talk to Wydin",
		"equip White apron", "hunt-collect", "talk to Redbeard Frank", "hunt-chest", "Read Pirate message", "provision hunt-spade", "hunt-dig", "[hunt] COMPLETED"} {
		i := strings.Index(joined[max(last, 0):], want)
		if i < 0 {
			t.Fatalf("%q does not follow in the log:\n%s", want, joined)
		}
		last = max(last, 0) + i
	}
	// The whole smuggle was one trip each way: two ship fares and the rum out of the 400 the bank gave, and Luthas's 30 coins back.
	if got := r.held(questplan.CoinsID); got != 400-30-30-30+30 {
		t.Errorf("%d coins left of the 400 the bank gave, want %d", got, 400-30-30-30+30)
	}
	if r.held(questplan.ObjKaramjaRum) != 0 || r.w.shelf != 2 {
		t.Errorf("rum %d in the pack, %d left on the shelf", r.held(questplan.ObjKaramjaRum), r.w.shelf)
	}
}

// With no apron in the bank the supplies pass buys one from Thessalia's shelf, and a pack that already holds the coins is not
// sent to the bank at all.
func TestPiratesTreasureBuysTheApronAndSkipsTheBankWhenReady(t *testing.T) {
	r := newHuntRig(t, 1, huntPortSarimTile, map[int]int{questplan.CoinsID: 1000}, map[int]int{questplan.ObjSpade: 1})
	res, _, joined := r.run(30 * time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %s", res.Outcome, res.Reason, joined)
	}
	r.assertSafe(t)
	if n := strings.Count(joined, "stub provision hunt-supplies"); n != 1 {
		t.Errorf("%d supplies passes, want the one that buys the apron", n)
	}
	if !r.w.apronWorn {
		t.Error("the apron was never worn")
	}
}

// An account that lost its rum after the first smuggle (employed at Wydin's, no job at the plantation any more) buys another,
// stashes it only after Luthas hires it again, and does the rest from the store crate.
func TestPiratesTreasureSmugglesASecondRumAfterALostOne(t *testing.T) {
	r := newHuntRig(t, 1, huntPortSarimTile, map[int]int{questplan.CoinsID: 1000, questplan.ObjWhiteApron: 1}, map[int]int{questplan.ObjSpade: 1})
	r.w.employed = questplan.HuntEmployedStore // rum-lost: the job at Wydin's, no rum anywhere
	if got := r.stage(); got != questplan.HuntRumLost {
		t.Fatalf("the rig's stage is %d, want rum-lost", got)
	}
	res, _, joined := r.run(30 * time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %s", res.Outcome, res.Reason, joined)
	}
	r.assertSafe(t)
	for _, want := range []string{"hunt-rum", "the crate refused the rum; Luthas hires again", "the crate has sailed", "hunt-collect"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the log has no %q:\n%s", want, joined)
		}
	}
	// A rum bought on the island by an account that has smuggled before reads "I have the Karamja Rum" (rum in hand): the quest
	// stashed it, it did not walk it to the ship (assertSafe), and the account is back on the mainland with the apron worn.
	if questplan.OnKaramja(r.w.at) && r.w.hunt < 4 {
		t.Errorf("the account ended on the island at hunt %d", r.w.hunt)
	}
}

// A lost chest key is re-issued; a lost message is simply fetched again; the dig needs the spade from the bank.
func TestPiratesTreasureResumesAtTheChest(t *testing.T) {
	r := newHuntRig(t, 2, tile.Tile{X: 3185, Z: 3436}, map[int]int{questplan.CoinsID: 100}, map[int]int{questplan.ObjSpade: 1})
	res, _, joined := r.run(30 * time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %s", res.Outcome, res.Reason, joined)
	}
	r.assertSafe(t)
	if strings.Contains(joined, "hunt-rum") || strings.Contains(joined, "hunt-ship") {
		t.Errorf("the smuggle ran at hunt 2:\n%s", joined)
	}
	if i, j := strings.Index(joined, "talk to Redbeard Frank"), strings.Index(joined, "hunt-chest"); i < 0 || j < i {
		t.Errorf("the key was not re-issued before the chest:\n%s", joined)
	}
}

// The rum in neither crate is a park for hours, not a loop between the two islands.
func TestPiratesTreasureWithTheRumInNeitherCrateParks(t *testing.T) {
	r := newHuntRig(t, 1, huntPortSarimTile, map[int]int{questplan.CoinsID: 1000, questplan.ObjWhiteApron: 1}, nil)
	r.w.employed, r.w.crateRum, r.w.apronWorn, r.w.cratesLie = questplan.HuntEmployedStore, questplan.HuntRumInStore, true, true
	res, _, joined := r.run(30 * time.Second)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "neither") {
		t.Fatalf("the run ended %s (%s), want a park on the two empty crates:\n%s", res.Outcome, res.Reason, joined)
	}
	r.assertSafe(t)
	// One look in each crate and no more: the island, the mainland, and the wait.
	if n := strings.Count(joined, "hunt-collect") + strings.Count(joined, "hunt-ship"); n > 6 {
		t.Errorf("%d crate visits before parking:\n%s", n, joined)
	}
}

// An account with no coins and none in the bank does not set out: the supplies pass is blocked and the quest waits and parks.
func TestPiratesTreasureWithoutCoinsParksOnTheMainland(t *testing.T) {
	r := newHuntRig(t, 1, huntPortSarimTile, map[int]int{questplan.CoinsID: 10, questplan.ObjWhiteApron: 1}, nil)
	res, _, joined := r.run(40 * time.Second)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "coins") {
		t.Fatalf("the run ended %s (%s), want a park on the coins:\n%s", res.Outcome, res.Reason, joined)
	}
	if questplan.OnKaramja(r.w.at) {
		t.Errorf("the account crossed with %d coins", r.held(questplan.CoinsID))
	}
}

// An empty rum shelf is a wait that parks, with the account on the island holding what it came with.
func TestPiratesTreasureWithAnEmptyRumShelfParks(t *testing.T) {
	r := newHuntRig(t, 1, huntPortSarimTile, map[int]int{questplan.CoinsID: 1000, questplan.ObjWhiteApron: 1}, nil)
	r.w.shelf = 0
	res, _, joined := r.run(40 * time.Second)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "Zambo") {
		t.Fatalf("the run ended %s (%s), want a park on Zambo's shelf:\n%s", res.Outcome, res.Reason, joined)
	}
	if n := strings.Count(joined, "hunt-rum"); n > 3 {
		t.Errorf("%d attempts at the shelf before parking:\n%s", n, joined)
	}
}

// Every state of the world the rig can reach leaves the engine on the way to Complete or parked, never in a loop and never with a
// rum carried off the island.
func TestPiratesTreasureFromEveryStateOfTheSmuggle(t *testing.T) {
	n := 0
	for employed := 0; employed <= 3; employed++ {
		for crateRum := 0; crateRum <= 2; crateRum++ {
			for _, bananas := range []int{0, 10} {
				for _, rum := range []int{0, 1} {
					for _, at := range []tile.Tile{huntPortSarimTile, questplan.ZamboShop.Anchor} {
						if employed == 2 && crateRum == 1 { // no script reaches it
							continue
						}
						pack := map[int]int{questplan.CoinsID: 1000, questplan.ObjWhiteApron: 1}
						if rum == 1 {
							pack[questplan.ObjKaramjaRum] = 1
						}
						r := newHuntRig(t, 1, at, pack, map[int]int{questplan.ObjSpade: 1})
						r.w.employed, r.w.crateRum, r.w.bananas = employed, crateRum, bananas
						if crateRum != 1 {
							r.w.bananas = 0
						}
						res, _, joined := r.run(40 * time.Second)
						ctx := fmt.Sprintf("employed %d crate rum %d bananas %d rum %d at %v: stage %d", employed, crateRum, bananas, rum, at, r.stage())
						if len(r.w.violations) != 0 {
							t.Fatalf("%s: %v\n%s", ctx, r.w.violations, joined)
						}
						if res.Outcome != questrunner.OutcomeComplete && res.Outcome != questrunner.OutcomeParked {
							t.Fatalf("%s: the run ended %s (%s)\n%s", ctx, res.Outcome, res.Reason, joined)
						}
						// A world the content can produce finishes the quest. (A rum in the plantation crate with no job at the plantation is
						// not one: Luthas clears the job in the same script that ships the crate.)
						if coherent := crateRum != 1 || employed&questplan.HuntEmployedPlantation != 0; coherent && res.Outcome != questrunner.OutcomeComplete {
							t.Fatalf("%s: parked (%s) in a world the content can produce\n%s", ctx, res.Reason, joined)
						}
						n++
					}
				}
			}
		}
	}
	if n < 50 {
		t.Fatalf("only %d states run", n)
	}
}
