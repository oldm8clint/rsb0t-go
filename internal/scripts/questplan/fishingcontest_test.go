package questplan

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Fishing Contest (W13, 18 Oct 2026): the journal parser over the pages the content renders, the decision over whole simulated
// quests (a world that answers every step the way the content's scripts do), every state of the decision, the kits through the real
// planner and the price of the worm trip. The content's own facts (ids, scripts, maps) are fishingcontest_content_test.go's.

// fcJournal is fishingcompo_journal.rs2's page for a stage, every string of a branch joined in order (`|` the client's line break).
func fcJournal(stage int) string {
	h := "@str@The Dwarves will let me use the tunnel through White Wolf|@str@Mountain if I can win the Hemenster Fishing Competition.|"
	switch stage {
	case FcNotStarted:
		return "@dbl@I can start this quest by speaking to the @dre@Dwarves@dbl@ at the tunnel entrances on either side of @dre@White Wolf Mountain@dbl@.|" +
			"@str@I must have level 10 fishing."
	case FcStarted:
		return h + "@dbl@They gave me a @dre@Fishing Contest Pass@dbl@ to enter the contest. I need to bring them back the @dre@Hemenster Fishing Trophy"
	case FcInComp:
		return h + "@dbl@My @dre@fishing spot@dbl@ in the contest is @dre@next to the willow tree @dbl@I need to catch the @dre@biggest fish@dbl@ to win!"
	case FcGarlicComp:
		return h + "@dbl@My @dre@fishing spot@dbl@ in the contest is @dre@next to the pipes @dbl@I need to catch the @dre@biggest fish@dbl@ to win!"
	case FcWonComp:
		return h + "@str@I easily won the contest by catching some Giant Carp.|" +
			"@dbl@I should take the @dre@Trophy@dbl@ back to the @dre@Dwarf@dbl@ at the side of @dre@White Wolf Mountain@dbl@ and claim my @dre@reward"
	case FcComplete:
		return "@dbl@The Dwarves wanted me to earn their friendship by|@dbl@winning the Hemenster Fishing Competition.|" +
			"@dbl@I scared away a vampire with some garlic and easily won the contest by catching some Giant Carp.|" +
			"@dbl@As a reward for getting the Fishing Competition Trophy the Dwarves will let me use their tunnel to travel quickly and|" +
			"@dbl@safely under White Wolf Mountain anytime I wish.||@red@QUEST COMPLETE!"
	}
	return ""
}

func TestParseFishingContestJournalReadsEveryRenderedStage(t *testing.T) {
	for stage := FcNotStarted; stage <= FcComplete; stage++ {
		page := fcJournal(stage)
		for _, lines := range [][]string{{page}, strings.Split(page, "|")} {
			got, ok := ParseFishingContestJournal(lines)
			if !ok || got != stage {
				t.Errorf("stage %d: parsed %d %v from %q", stage, got, ok, page[:60])
			}
		}
	}
	// The not-started page with the level met reads the same.
	if got, ok := ParseFishingContestJournal([]string{strings.Replace(fcJournal(FcNotStarted), "@str@I must", "@dre@I must", 1)}); !ok || got != FcNotStarted {
		t.Errorf("the page of an account under Fishing 10 parsed %d %v", got, ok)
	}
	for _, lines := range [][]string{nil, {""}, {"@dbl@Something else entirely"}} {
		if got, ok := ParseFishingContestJournal(lines); ok {
			t.Errorf("%q parsed as %d: an unrecognised journal is unknown, never stage 0", lines, got)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The simulated quest
// ---------------------------------------------------------------------------------------------------------------------

// fcWorld is the Fishing Contest as the content runs it: %fishingcompo, %hemenster_pipe_stashed, the comp stage's fee, and the scripts'
// answers to each talk and each custom.
type fcWorld struct {
	*kcWorld
	stage int
	// pipe is %hemenster_pipe_stashed; paid is the comp stage's fee; strangerAway is how many seatings find the Stranger away.
	pipe         bool
	paid         bool
	strangerAway int
	inside       bool
	fishing      int
	defence      int
	// cupboard is whether Morgan's cupboard gives a clove (the garlic custom).
	cupboard bool
	// digs counts the trips into the wood; stashes the cloves put in the pipe.
	digs, stashes int
	// blocked is the engine's memory of a provisioning round whose every source was dead.
	blocked map[string]bool
}

func newFCWorld(t *testing.T, stage int, inv, bank map[int]int) *fcWorld {
	w := &fcWorld{kcWorld: newKCWorld(t, inv, bank), stage: stage, fishing: 40, defence: 1, cupboard: true, blocked: map[string]bool{}}
	w.tile = FcSeersBank
	w.maxHP = 40
	return w
}

func (w *fcWorld) snapshot() Snapshot {
	j := StatusInProgress
	switch w.stage {
	case FcNotStarted:
		j = StatusNotStarted
	case FcComplete:
		j = StatusComplete
	}
	s := w.base(j)
	s.Levels["fishing"], s.Levels["defence"] = w.fishing, w.defence
	for k, v := range w.blocked {
		s.Blocked[k] = v
	}
	s.Stage, s.StageKnown = w.stage, true
	return s
}

func (w *fcWorld) apply(st Step) {
	t := w.t
	t.Helper()
	if why := ValidateStep(st); why != "" {
		t.Fatalf("malformed step %s: %s", st.Describe(), why)
	}
	switch st.Kind {
	case StepProvision:
		key := "provision:" + st.KitName
		if w.provision(st) {
			w.ran[key] = true
			delete(w.failed, key)
		} else {
			w.failed[key] = true
			w.blocked[key] = true
		}
		w.log = append(w.log, key)
	case StepScanBank:
		w.bankKnown = true
		w.log = append(w.log, "scanbank")
	case StepDeposit:
		w.deposit(st)
		w.log = append(w.log, "deposit")
	case StepWithdraw:
		for _, it := range st.Items {
			n := minInt(it.Qty, w.bank[it.ID])
			w.bank[it.ID] -= n
			w.inv[it.ID] += n
		}
		w.log = append(w.log, "withdraw")
	case StepTalk:
		w.talk(st)
	case StepCustom:
		w.custom(st)
	default:
		t.Fatalf("unexpected step %s", st.Describe())
	}
}

func (w *fcWorld) talk(st Step) {
	if st.Stop.ID != NpcMountainDwarf {
		w.t.Fatalf("a talk with npc %d", st.Stop.ID)
	}
	w.tile, w.inside = FcDwarfAnchor, false
	switch {
	case w.stage == FcNotStarted:
		w.stage = FcStarted
		w.inv[ObjFishingPass]++
		w.log = append(w.log, "dwarf-start")
	case st.UntilItemID == ObjFishingPass:
		if w.inv[ObjFishingPass] > 0 || w.bank[ObjFishingPass] > 0 {
			w.t.Fatalf("a spare pass asked for with one in the pack or the bank (the dwarf gives none)")
		}
		w.inv[ObjFishingPass]++
		w.log = append(w.log, "dwarf-pass")
	case w.stage == FcWonComp:
		if w.inv[ObjFishingTrophy] == 0 {
			w.t.Fatalf("the trophy handed over without one")
		}
		w.inv[ObjFishingTrophy]--
		w.stage = FcComplete
		w.log = append(w.log, "dwarf-trophy")
	default:
		w.t.Fatalf("a talk with the dwarf at stage %d: %+v", w.stage, st)
	}
}

func (w *fcWorld) enter() {
	if w.inv[ObjFishingPass] == 0 {
		w.t.Fatalf("Morris let an account without the pass in")
	}
	w.inside, w.tile = true, FcGateInside
}

func (w *fcWorld) custom(st Step) {
	switch st.Custom {
	case CustomFishGarlic:
		if !w.cupboard {
			w.failed[st.Custom] = true
			w.log = append(w.log, st.Custom)
			return
		}
		w.inv[ObjGarlic]++
	case CustomFishDig:
		if w.inv[ObjSpade] == 0 {
			w.t.Fatalf("the vines dug without a spade")
		}
		w.digs++
		w.inv[ObjRedVineWorm] += st.Qty
		w.tile, w.inside = FcWoodOutside, false
	case CustomFishStash:
		if !w.inside {
			w.enter()
		}
		if w.inv[ObjGarlic] == 0 {
			w.t.Fatalf("the pipe stashed with no garlic")
		}
		w.inv[ObjGarlic]--
		w.stashes++
		w.pipe = true
		if w.paid && w.stage == FcInComp { // stash_garlic: seated and paid, the Stranger moves
			w.stage = FcGarlicComp
		}
	case CustomFishPay:
		if !w.inside {
			w.enter()
		}
		if w.stage != FcStarted {
			w.t.Fatalf("Bonzo paid at stage %d", w.stage)
		}
		if !w.paid {
			if w.inv[CoinsID] < FcEntryFee {
				w.t.Fatalf("the fee paid without coins")
			}
			w.inv[CoinsID] -= FcEntryFee
			w.paid = true
		}
		switch {
		case w.pipe && w.strangerAway > 0:
			w.strangerAway-- // the seating finds the Stranger away: paid, still stage 1
		case w.pipe:
			w.stage = FcGarlicComp
		default:
			w.stage = FcInComp
		}
		w.tile = FcBonzoAnchor
	case CustomFishLeave:
		if w.stage == FcInComp || w.stage == FcGarlicComp {
			w.stage, w.paid = FcStarted, false // bonzo_quits
		}
		w.inside, w.tile = false, FcGateOutside
	case CustomFishFish:
		if !w.inside {
			w.enter()
		}
		if w.stage != FcGarlicComp || w.inv[ObjFishingRod] == 0 || w.inv[ObjRedVineWorm] == 0 {
			w.t.Fatalf("fished at stage %d with the pack %v", w.stage, w.inv)
		}
		casts := 3
		for ; casts > 0 && w.inv[ObjRedVineWorm] > 0; casts-- {
			w.inv[ObjRedVineWorm]--
		}
		for ; casts > 0 && w.inv[ObjFishingBait] > 0; casts-- {
			w.inv[ObjFishingBait]--
		}
		w.inv[ObjFishingTrophy]++
		w.stage = FcWonComp
	case CustomFishSpare:
		if !w.inside {
			w.enter()
		}
		w.inv[ObjFishingTrophy]++
	default:
		w.t.Fatalf("unexpected custom %q", st.Custom)
	}
	delete(w.failed, st.Custom)
	w.ran[st.Custom] = true
	w.log = append(w.log, st.Custom)
}

func (w *fcWorld) run() (waited string, parkFor time.Duration) {
	w.t.Helper()
	for i := 0; i < 80; i++ {
		st := DecideFishingContest(w.snapshot())
		switch st.Kind {
		case StepDone:
			return "", 0
		case StepWait:
			return st.Reason, st.ParkFor
		}
		before := fmt.Sprint(w.stage, w.inv, w.bank, w.bankKnown, w.ran, w.failed, w.tile, w.paid, w.strangerAway, w.blocked)
		w.apply(st)
		if fmt.Sprint(w.stage, w.inv, w.bank, w.bankKnown, w.ran, w.failed, w.tile, w.paid, w.strangerAway, w.blocked) == before {
			w.t.Fatalf("step %d, %s, changed nothing: the module would loop (log %v)", i, st.Describe(), w.log)
		}
	}
	w.t.Fatalf("80 steps and the quest is not done (stage %d, log %v)", w.stage, w.log)
	return "", 0
}

// fcKit is a bank that holds everything the contest takes in, and food for the wood.
func fcKit() map[int]int {
	return map[int]int{ObjFishingRod: 1, ObjGarlic: 1, ObjSpade: 1, CoinsID: 5000, 385: 6, ObjFishingBait: 20}
}

// A fresh account whose bank holds the kit does the whole quest in the content's order: the dwarf, the supplies, the spade, the worms,
// the garlic in the pipe BEFORE the fee, the fee and the seat, the casts and the trophy back to the dwarf.
func TestAFreshAccountDoesTheFishingContestInOrder(t *testing.T) {
	w := newFCWorld(t, FcNotStarted, map[int]int{CoinsID: 300}, fcKit())
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	want := []string{"dwarf-start", "provision:" + KitFishSupplies, "provision:" + KitFishSpade, "provision:" + KitFishFood, CustomFishDig,
		CustomFishStash, CustomFishPay, CustomFishFish, "dwarf-trophy"}
	if !slices.Equal(w.log, want) {
		t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
	for _, id := range []int{ObjFishingTrophy, ObjRedVineWorm, ObjGarlic} { // (the pass stays: the dwarf takes the trophy only)
		if w.inv[id] != 0 {
			t.Errorf("%d of obj %d left in the pack", w.inv[id], id)
		}
	}
	if w.digs != 1 || w.stashes != 1 {
		t.Errorf("%d trips into the wood and %d cloves in the pipe, want one of each", w.digs, w.stashes)
	}
}

// The garlic goes in the pipe before the fee, whatever else: the seat is then Bonzo's script and a failed seat costs a free talk.
func TestTheGarlicIsStashedBeforeTheFee(t *testing.T) {
	w := newFCWorld(t, FcNotStarted, map[int]int{CoinsID: 300}, fcKit())
	w.run()
	stash, pay := slices.Index(w.log, CustomFishStash), slices.Index(w.log, CustomFishPay)
	if stash < 0 || pay < 0 || stash > pay {
		t.Fatalf("the stash at %d, the fee at %d: %v", stash, pay, w.log)
	}
}

// A seat that is not given (the Stranger is away from his post) leaves the fee paid and the stage at 1: the next step talks to Bonzo
// again, with no second clove fetched, no second stash and no second fee.
func TestASeatThatFailsIsRetriedWithoutAnotherCloveOrFee(t *testing.T) {
	w := newFCWorld(t, FcNotStarted, map[int]int{CoinsID: 300}, fcKit())
	w.strangerAway = 2
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	pays := 0
	for _, l := range w.log {
		if l == CustomFishPay {
			pays++
		}
	}
	if pays != 3 || w.stashes != 1 || w.inv[CoinsID] != 300-FcEntryFee {
		t.Errorf("%d seatings, %d cloves, coins %d: %v", pays, w.stashes, w.inv[CoinsID], w.log)
	}
	if slices.Contains(w.log, CustomFishGarlic) {
		t.Errorf("a second clove was fetched: %v", w.log)
	}
}

// Without the garlic in the bank or the mule, the clove comes from Morgan's cupboard, once; and a cupboard that gives none parks.
func TestTheGarlicComesFromMorgansCupboardWhenNothingElseHasIt(t *testing.T) {
	kit := fcKit()
	delete(kit, ObjGarlic)
	w := newFCWorld(t, FcStarted, map[int]int{CoinsID: 300, ObjFishingPass: 1}, kit)
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	n := 0
	for _, l := range w.log {
		if l == CustomFishGarlic {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the cupboard was visited %d times: %v", n, w.log)
	}
	w = newFCWorld(t, FcStarted, map[int]int{CoinsID: 300, ObjFishingPass: 1}, kit)
	w.cupboard = false
	why, park := w.run()
	if !strings.Contains(why, "no garlic") || park != FcRetryPark {
		t.Fatalf("waited %q for %s (log %v)", why, park, w.log)
	}
}

// The rod comes from a fishing shop (the nearer of Harry's and Gerrant's) when neither the bank nor the mule has one, and its absence
// parks the quest.
func TestTheRodIsBoughtWhenNothingElseHasIt(t *testing.T) {
	kit := fcKit()
	delete(kit, ObjFishingRod)
	w := newFCWorld(t, FcStarted, map[int]int{CoinsID: 300, ObjFishingPass: 1}, kit)
	w.shop[ObjFishingRod] = true
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	w = newFCWorld(t, FcStarted, map[int]int{CoinsID: 300, ObjFishingPass: 1}, kit)
	why, park := w.run()
	if !strings.Contains(why, "no fishing rod") || park != FcRetryPark {
		t.Fatalf("waited %q for %s (log %v)", why, park, w.log)
	}
	// The shops are listed nearer first: Harry's from Seers and Catherby, Gerrant's from Port Sarim and Draynor.
	for _, c := range []struct {
		at   tile.Tile
		want int
	}{{FcSeersBank, NpcHarry}, {tile.Tile{X: 2809, Z: 3440}, NpcHarry}, {tile.Tile{X: 3013, Z: 3222}, FluffsSardineShop.NpcID},
		{DraynorBank, FluffsSardineShop.NpcID}, {ArdougneSouthBank, NpcHarry}} {
		srcs := fcRodSources(Snapshot{Tile: c.at, TileKnown: true})
		if len(srcs) != 2 || srcs[0].Shop.NpcID != c.want || srcs[0].Kind != SourceShop || srcs[0].Price != 5 {
			t.Errorf("from %v the rod's shops are %+v, want npc %d first", c.at, srcs, c.want)
		}
	}
}

// No spade anywhere parks; a spade the planner can only get from the ground is fetched from the nearest spawn on the account's side of the
// water.
func TestTheSpadeComesFromTheNearestSpawnOnTheAccountsSide(t *testing.T) {
	kit := fcKit()
	delete(kit, ObjSpade)
	w := newFCWorld(t, FcStarted, map[int]int{CoinsID: 300, ObjFishingPass: 1}, kit)
	why, park := w.run()
	if !strings.Contains(why, "no spade") || park != FcRetryPark {
		t.Fatalf("waited %q for %s (log %v)", why, park, w.log)
	}
	w = newFCWorld(t, FcStarted, map[int]int{CoinsID: 300, ObjFishingPass: 1}, kit)
	w.ground[ObjSpade] = true
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	for _, c := range []struct {
		name string
		at   tile.Tile
		want tile.Tile
	}{
		{"Seers' Village", FcSeersBank, FcSpadeSpawns[0]},
		{"East Ardougne", ArdougneSouthBank, FcSpadeSpawns[0]},
		{"Falador", tile.Tile{X: 3013, Z: 3355}, FcSpadeSpawns[1]},
		{"Draynor", DraynorBank, FcSpadeSpawns[2]},
		{"Varrock", tile.Tile{X: 3253, Z: 3420}, FcSpadeSpawns[3]},
	} {
		srcs := FcSpadeSources(Snapshot{Tile: c.at, TileKnown: true})
		if len(srcs) != 4 || srcs[0].Tile != c.want {
			t.Errorf("from %s the spade's spawns start at %v, want %v: %+v", c.name, srcs[0].Tile, c.want, srcs)
		}
	}
	if srcs := FcSpadeSources(Snapshot{}); srcs[0].Tile != FcSpadeSpawns[0] {
		t.Errorf("before a position the spawns are in the map's order, got %v first", srcs[0].Tile)
	}
}

// The worm trip is priced: an account that cannot take the dogs on its hitpoints and food is sent for food once and then parked for hours
// with the reason, and one the dogs ignore (combat above 88) goes with nothing.
func TestTheWoodIsPricedByHitpointsAndFood(t *testing.T) {
	kit := fcKit()
	delete(kit, 385)
	w := newFCWorld(t, FcStarted, map[int]int{CoinsID: 300, ObjFishingPass: 1}, kit)
	w.bankKnown = true
	why, park := w.run()
	if !strings.Contains(why, "McGrubor") || park != FcOddsPark {
		t.Fatalf("waited %q for %s (log %v)", why, park, w.log)
	}
	if !slices.Contains(w.log, "provision:"+KitFishFood) {
		t.Errorf("no food was asked for before the park: %v", w.log)
	}
	if slices.Contains(w.log, CustomFishDig) {
		t.Errorf("the wood was entered: %v", w.log)
	}
	// 4 sharks make the trip fit a 40 hitpoint account; the immune one needs none.
	w = newFCWorld(t, FcStarted, map[int]int{CoinsID: 300, ObjFishingPass: 1, 385: 4}, kit)
	w.bankKnown = true
	if why, _ = w.run(); why != "" {
		t.Fatalf("with sharks: waited %s (log %v)", why, w.log)
	}
	w = newFCWorld(t, FcStarted, map[int]int{CoinsID: 300, ObjFishingPass: 1}, kit)
	w.bankKnown = true
	w.maxHP = 60
	snap := func(mod func(*Snapshot)) Snapshot { s := w.snapshot(); mod(&s); return s }
	for _, c := range []struct {
		name string
		s    Snapshot
		ok   bool
	}{
		{"60 hitpoints, Defence 10, no food", snap(func(s *Snapshot) {
			s.Levels = map[string]int{"hitpoints": 60, "defence": 10, "attack": 30, "strength": 30}
		}), false},
		{"60 hitpoints, Defence 10, 4 sharks", snap(func(s *Snapshot) {
			s.Levels = map[string]int{"hitpoints": 60, "defence": 10, "attack": 30, "strength": 30}
			s.InvIDs = map[int]int{385: 4}
		}), true},
		{"under the hitpoints floor", snap(func(s *Snapshot) { s.MaxHP, s.HP = 25, 25; s.InvIDs = map[int]int{385: 20} }), false},
		{"combat 90, no food", snap(func(s *Snapshot) {
			s.MaxHP, s.HP = 70, 70
			s.Levels = map[string]int{"hitpoints": 70, "defence": 80, "attack": 80, "strength": 80, "magic": 70, "prayer": 50}
		}), true},
		{"an unread maximum", snap(func(s *Snapshot) { s.MaxHP, s.HP = 0, 0 }), true},
	} {
		ok, why := FishingWoodFit(c.s)
		if ok != c.ok {
			t.Errorf("%s: fit %v (%s), want %v", c.name, ok, why, c.ok)
		}
		if !ok && why == "" {
			t.Errorf("%s: refused with no reason", c.name)
		}
	}
}

func TestTheDogsCeilingIsTwiceTheirLevel(t *testing.T) {
	if FcDogHuntCeiling != 2*44 {
		t.Errorf("the ceiling %d is not twice the guard dog's level 44", FcDogHuntCeiling)
	}
	s := Snapshot{Levels: map[string]int{"hitpoints": 70, "defence": 80, "attack": 80, "strength": 80, "magic": 70, "prayer": 50}}
	if !FcImmune(s) || FcImmune(Snapshot{Levels: map[string]int{"hitpoints": 30, "defence": 20, "attack": 20, "strength": 20}}) || FcImmune(Snapshot{}) {
		t.Errorf("immune: %v / %v / %v", FcImmune(s), FcImmune(Snapshot{Levels: map[string]int{"hitpoints": 30}}), FcImmune(Snapshot{}))
	}
	// The dog is priced as the content has it: a maximum hit of 5, 49 hitpoints.
	if FoeGuardDog.MaxHit() != 5 || FoeGuardDog.Hitpoints != 49 || FoeGuardDog.PeakHit() != 5 {
		t.Errorf("the guard dog is %+v (max hit %d)", FoeGuardDog, FoeGuardDog.MaxHit())
	}
	if f, ok := FoeByNPC(NpcGuardDog); !ok || f.Name != "Guard dog" {
		t.Errorf("FoeByNPC(99) = %+v %v", f, ok)
	}
	if FishingWoodDamage(s) >= FishingWoodDamage(Snapshot{Levels: map[string]int{"defence": 1}}) {
		t.Errorf("an immune account is priced at no fewer dog attacks")
	}
}

// Stage 2 (seated at the willow tree): garlic in the pack goes in the pipe, none and the account quits and starts again from stage 1,
// where the garlic is fetched only if the pipe is not known to be stashed.
func TestSeatedAtTheWillowTheGarlicIsStashedOrTheContestQuit(t *testing.T) {
	inv := map[int]int{CoinsID: 300, ObjFishingPass: 1, ObjFishingRod: 1, ObjRedVineWorm: 3, ObjGarlic: 1}
	w := newFCWorld(t, FcInComp, inv, fcKit())
	w.paid, w.inside, w.tile = true, true, FcBonzoAnchor
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	if want := []string{CustomFishStash, CustomFishFish, "dwarf-trophy"}; !slices.Equal(w.log, want) {
		t.Errorf("with garlic: %v, want %v", w.log, want)
	}
	delete(inv, ObjGarlic)
	w = newFCWorld(t, FcInComp, inv, fcKit())
	w.paid, w.inside, w.tile = true, true, FcBonzoAnchor
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	if w.log[0] != CustomFishLeave {
		t.Errorf("without garlic the account did %v first, want the quit", w.log)
	}
	// A stash that failed on its last try leaves the account to quit and not to stash again.
	w = newFCWorld(t, FcInComp, map[int]int{CoinsID: 300, ObjFishingPass: 1, ObjFishingRod: 1, ObjRedVineWorm: 3, ObjGarlic: 1}, nil)
	w.failed[CustomFishStash] = true
	if st := DecideFishingContest(w.snapshot()); st.Kind != StepCustom || st.Custom != CustomFishLeave {
		t.Errorf("after a failed stash decided %s", st.Describe())
	}
}

// Stage 3 (seated at the pipes): a rod and a worm, and the account fishes; either missing and it quits to fetch them. One carp caught
// and plain bait left is enough to go on, one worm and no bait for the next casts is enough to start.
func TestSeatedAtThePipesTheAccountFishesOrQuits(t *testing.T) {
	base := map[int]int{CoinsID: 300, ObjFishingPass: 1}
	for _, c := range []struct {
		name   string
		extra  map[int]int
		custom string
	}{
		{"rod and worms", map[int]int{ObjFishingRod: 1, ObjRedVineWorm: 3}, CustomFishFish},
		{"rod and one worm", map[int]int{ObjFishingRod: 1, ObjRedVineWorm: 1}, CustomFishFish},
		{"rod, a carp and bait", map[int]int{ObjFishingRod: 1, ObjRawGiantCarp: 1, ObjFishingBait: 2}, CustomFishFish},
		{"rod, a carp and no bait", map[int]int{ObjFishingRod: 1, ObjRawGiantCarp: 1}, CustomFishLeave},
		{"rod and no worm", map[int]int{ObjFishingRod: 1}, CustomFishLeave},
		{"worms and no rod", map[int]int{ObjRedVineWorm: 3}, CustomFishLeave},
	} {
		inv := map[int]int{}
		for k, v := range base {
			inv[k] = v
		}
		for k, v := range c.extra {
			inv[k] = v
		}
		w := newFCWorld(t, FcGarlicComp, inv, nil)
		w.inside, w.tile = true, FcBonzoAnchor
		st := DecideFishingContest(w.snapshot())
		if st.Kind != StepCustom || st.Custom != c.custom {
			t.Errorf("%s: decided %s, want %s", c.name, st.Describe(), c.custom)
		}
	}
	// Outside the compound without the pass: the pass first (the bank is read, then the dwarf).
	w := newFCWorld(t, FcGarlicComp, map[int]int{ObjFishingRod: 1, ObjRedVineWorm: 3}, nil)
	if st := DecideFishingContest(w.snapshot()); st.Kind != StepScanBank {
		t.Errorf("no pass, outside, the bank unread: %s", st.Describe())
	}
	w.bankKnown = true
	if st := DecideFishingContest(w.snapshot()); st.Kind != StepTalk || st.UntilItemID != ObjFishingPass {
		t.Errorf("no pass, outside, the bank read: %s", st.Describe())
	}
	w.bank[ObjFishingPass] = 1
	if st := DecideFishingContest(w.snapshot()); st.Kind != StepWithdraw || st.Items[0].ID != ObjFishingPass {
		t.Errorf("no pass, outside, one in the bank: %s", st.Describe())
	}
	// Inside, the pass is not needed again.
	w = newFCWorld(t, FcGarlicComp, map[int]int{ObjFishingRod: 1, ObjRedVineWorm: 3}, nil)
	w.inside, w.tile = true, FcBonzoAnchor
	if st := DecideFishingContest(w.snapshot()); st.Kind != StepCustom || st.Custom != CustomFishFish {
		t.Errorf("inside without the pass: %s", st.Describe())
	}
}

// Stage 4: the trophy to the dwarf from the pack, the bank, or Bonzo's spare.
func TestTheTrophyComesFromThePackTheBankOrBonzo(t *testing.T) {
	w := newFCWorld(t, FcWonComp, map[int]int{ObjFishingTrophy: 1, ObjFishingPass: 1}, nil)
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited %s (log %v)", why, w.log)
	}
	if want := []string{"dwarf-trophy"}; !slices.Equal(w.log, want) {
		t.Errorf("log %v", w.log)
	}
	w = newFCWorld(t, FcWonComp, map[int]int{ObjFishingPass: 1}, map[int]int{ObjFishingTrophy: 1})
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited %s (log %v)", why, w.log)
	}
	if want := []string{"scanbank", "withdraw", "dwarf-trophy"}; !slices.Equal(w.log, want) {
		t.Errorf("the banked trophy: %v, want %v", w.log, want)
	}
	w = newFCWorld(t, FcWonComp, map[int]int{ObjFishingPass: 1}, nil)
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited %s (log %v)", why, w.log)
	}
	if want := []string{"scanbank", CustomFishSpare, "dwarf-trophy"}; !slices.Equal(w.log, want) {
		t.Errorf("the lost trophy: %v, want %v", w.log, want)
	}
	// With no pass either, the pass comes first (the bank is read once).
	w = newFCWorld(t, FcWonComp, nil, nil)
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited %s (log %v)", why, w.log)
	}
	if want := []string{"scanbank", "dwarf-pass", CustomFishSpare, "dwarf-trophy"}; !slices.Equal(w.log, want) {
		t.Errorf("the lost pass and trophy: %v, want %v", w.log, want)
	}
	w = newFCWorld(t, FcWonComp, map[int]int{ObjFishingPass: 1}, nil)
	w.bankKnown = true
	w.failed[CustomFishSpare] = true
	if why, park := w.run(); !strings.Contains(why, "spare trophy") || park != FcRetryPark {
		t.Errorf("a failed spare waited %q for %s", why, park)
	}
}

// The pass is withdrawn from the bank before the dwarf is asked: he gives a spare only to an account whose pack and bank hold none.
func TestAPassInTheBankIsWithdrawnNotAskedFor(t *testing.T) {
	w := newFCWorld(t, FcStarted, map[int]int{CoinsID: 300}, map[int]int{ObjFishingPass: 1, ObjFishingRod: 1, ObjGarlic: 1, ObjSpade: 1, CoinsID: 100, 385: 6})
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited %s (log %v)", why, w.log)
	}
	if slices.Contains(w.log, "dwarf-pass") || w.log[0] != "scanbank" {
		t.Errorf("log %v: the bank is read first and the dwarf never asked", w.log)
	}
}

// Under Fishing 10 the account is parked for hours before it walks anywhere (the contest's spots refuse it).
func TestAnAccountUnderFishing10IsParkedBeforeItWalks(t *testing.T) {
	w := newFCWorld(t, FcNotStarted, map[int]int{CoinsID: 300}, fcKit())
	w.fishing = 9
	why, park := w.run()
	if !strings.Contains(why, "Fishing 10") || park != FcFishLevelPark || len(w.log) != 0 {
		t.Fatalf("waited %q for %s after %v", why, park, w.log)
	}
	s := newFCWorld(t, FcNotStarted, map[int]int{CoinsID: 300}, nil).snapshot()
	for _, lv := range []map[string]int{nil, {}, {"hitpoints": 40}} {
		s.Levels = lv
		if st := DecideFishingContest(s); st.Kind != StepTalk {
			t.Errorf("an unread Fishing (levels %v) decided %s", lv, st.Describe())
		}
	}
}

// The tab, the journal and a failed step: waits, never guesses.
func TestFishingContestWaitsAndParks(t *testing.T) {
	w := newFCWorld(t, FcStarted, nil, nil)
	s := w.snapshot()
	s.Journal = StatusUnknown
	if st := DecideFishingContest(s); st.Kind != StepWait {
		t.Errorf("an uncoloured tab decided %s", st.Describe())
	}
	s = w.snapshot()
	s.StageKnown = false
	if st := DecideFishingContest(s); st.Kind != StepWait {
		t.Errorf("an unread stage decided %s", st.Describe())
	}
	s = w.snapshot()
	s.Journal = StatusComplete
	if st := DecideFishingContest(s); st.Kind != StepDone {
		t.Errorf("a green tab decided %s", st.Describe())
	}
	s.Journal, s.Stage = StatusInProgress, FcComplete
	if st := DecideFishingContest(s); st.Kind != StepWait || !strings.Contains(st.Reason, "green") {
		t.Errorf("stage 5 with the tab not green: %s", st.Describe())
	}
	s.Stage = 99
	if st := DecideFishingContest(s); st.Kind != StepWait {
		t.Errorf("stage 99 decided %s", st.Describe())
	}
	// The dig, the stash, the fee, the casts and the quit each park for half an hour after a failure.
	good := map[int]int{CoinsID: 300, ObjFishingPass: 1, ObjFishingRod: 1, ObjGarlic: 1, ObjSpade: 1, 385: 6}
	for _, c := range []struct {
		name   string
		stage  int
		inv    map[int]int
		failed string
	}{
		{"the dig", FcStarted, good, CustomFishDig},
		{"the stash", FcStarted, map[int]int{CoinsID: 300, ObjFishingPass: 1, ObjFishingRod: 1, ObjGarlic: 1, ObjRedVineWorm: 3}, CustomFishStash},
		{"the fee", FcStarted, map[int]int{CoinsID: 300, ObjFishingPass: 1, ObjFishingRod: 1, ObjRedVineWorm: 3}, CustomFishPay},
		{"the casts", FcGarlicComp, map[int]int{ObjFishingPass: 1, ObjFishingRod: 1, ObjRedVineWorm: 3}, CustomFishFish},
		{"the quit", FcInComp, map[int]int{ObjFishingPass: 1}, CustomFishLeave},
	} {
		w := newFCWorld(t, c.stage, c.inv, nil)
		w.ran["provision:"+KitFishSupplies] = true
		w.ran[CustomFishStash] = c.failed != CustomFishStash
		w.inside = true
		w.failed[c.failed] = true
		why, park := w.run()
		if why == "" || park != FcRetryPark || len(w.log) != 0 {
			t.Errorf("%s: waited %q for %s after %v", c.name, why, park, w.log)
		}
	}
}

// Every step the decision can ask for, over every stage, every place and a spread of packs, is well formed and names a custom the module
// has.
func TestEveryFishingStepIsWellFormed(t *testing.T) {
	customs := map[string]bool{CustomFishGarlic: true, CustomFishDig: true, CustomFishPay: true, CustomFishStash: true, CustomFishLeave: true,
		CustomFishFish: true, CustomFishSpare: true}
	places := []tile.Tile{FcSeersBank, FcDwarfAnchor, FcGateOutside, FcBonzoAnchor, FcPipeStand, FcWoodOutside, DraynorBank, {X: 3013, Z: 3355}}
	packs := []map[int]int{nil, {ObjFishingPass: 1, CoinsID: 500}, {ObjFishingPass: 1, ObjFishingRod: 1, ObjGarlic: 1, ObjSpade: 1, 385: 4},
		{ObjFishingPass: 1, ObjFishingRod: 1, ObjRedVineWorm: 3, ObjFishingBait: 3, CoinsID: 100}, {ObjFishingTrophy: 1}, {ObjRawGiantCarp: 1, ObjFishingRod: 1}}
	failures := []string{"", CustomFishGarlic, CustomFishDig, CustomFishPay, CustomFishStash, CustomFishLeave, CustomFishFish, CustomFishSpare,
		"provision:" + KitFishSupplies, "provision:" + KitFishSpade, "provision:" + KitFishFood}
	n := 0
	for stage := FcNotStarted; stage <= FcComplete; stage++ {
		for _, at := range places {
			for _, inv := range packs {
				for _, bank := range []map[int]int{nil, {ObjFishingPass: 1, ObjFishingTrophy: 1}} {
					for _, known := range []bool{false, true} {
						for _, failed := range failures {
							for _, stashed := range []bool{false, true} {
								w := newFCWorld(t, stage, inv, bank)
								w.tile, w.bankKnown = at, known
								w.inside = InFishingCompound(at)
								if failed != "" {
									w.failed[failed] = true
								}
								if stashed {
									w.ran[CustomFishStash] = true
								}
								st := DecideFishingContest(w.snapshot())
								n++
								if st.Kind == StepWait && st.Reason == "" {
									t.Fatalf("stage %d at %v: a wait with no reason", stage, at)
								}
								if st.Kind == StepCustom && !customs[st.Custom] {
									t.Fatalf("stage %d at %v: unknown custom %q", stage, at, st.Custom)
								}
								if st.Kind != StepWait {
									if why := ValidateStep(st); why != "" {
										t.Fatalf("stage %d at %v: %s: %s", stage, at, st.Describe(), why)
									}
								}
							}
						}
					}
				}
			}
		}
	}
	t.Logf("%d states swept", n)
}

// The kits are valid lines, the supplies kit leaves the garlic out of a run that has stashed it, and each is planned by the real
// planner: the bank first, then the mule, then the sources.
func TestFishingKitsThroughThePlanner(t *testing.T) {
	s := Snapshot{Tile: FcSeersBank, TileKnown: true, Ran: map[string]bool{}}
	for _, kit := range [][]KitLine{fcSuppliesKit(s), fcSpadeKit(s), fcFoodKit()} {
		for _, l := range kit {
			if why := ValidateKitLine(l); why != "" {
				t.Error(why)
			}
		}
	}
	ids := func(kit []KitLine) []int {
		var out []int
		for _, l := range kit {
			out = append(out, l.ID)
		}
		return out
	}
	if got := ids(fcSuppliesKit(s)); !slices.Equal(got, []int{ObjFishingRod, ObjGarlic, ObjFishingBait, CoinsID}) {
		t.Errorf("the supplies kit %v", got)
	}
	s.Ran = map[string]bool{CustomFishStash: true}
	if got := ids(fcSuppliesKit(s)); !slices.Equal(got, []int{ObjFishingRod, ObjFishingBait, CoinsID}) {
		t.Errorf("the supplies kit of a run that has stashed %v", got)
	}
	// The rod is the one line that is not optional; the bank covers it, the mule is asked only for what it publishes, a shop is last.
	kit := fcSuppliesKit(Snapshot{Tile: FcSeersBank, TileKnown: true})
	plan := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{ObjFishingRod: 1, ObjGarlic: 1}, BankKnown: true, FreeSlots: 20})
	if plan.Phase == PhaseBlocked || len(plan.Withdraw) < 2 {
		t.Errorf("a bank with the rod and the garlic: %s", plan.Summary())
	}
	plan = PlanProvision(kit, ProvisionState{Pack: map[int]int{CoinsID: 500}, Bank: map[int]int{}, BankKnown: true, Mule: map[int]int{ObjFishingRod: 18, ObjGarlic: 9, ObjFishingBait: 8526},
		MuleKnown: true, FreeSlots: 20})
	if plan.Phase != PhaseMule || len(plan.MuleAsk) != 3 {
		t.Errorf("a mule with rods, garlic and bait: %s", plan.Summary())
	}
	plan = PlanProvision(kit, ProvisionState{Pack: map[int]int{CoinsID: 500}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20})
	if plan.Phase != PhaseSource || plan.Tasks[0].Source.Kind != SourceShop || plan.Tasks[0].Line.ID != ObjFishingRod {
		t.Errorf("an empty bank and no mule: %s", plan.Summary())
	}
	// The spade: bank, mule, then the spawn nearest.
	plan = PlanProvision(fcSpadeKit(Snapshot{Tile: DraynorBank, TileKnown: true}), ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20})
	if plan.Phase != PhaseSource || plan.Tasks[0].Source.Tile != FcSpadeSpawns[2] {
		t.Errorf("a spade from Draynor: %s", plan.Summary())
	}
}

func TestFishingBaitReadiness(t *testing.T) {
	for _, c := range []struct {
		inv         map[int]int
		ready, fish bool
	}{
		{nil, false, false},
		{map[int]int{ObjRedVineWorm: 3}, true, true},
		{map[int]int{ObjRedVineWorm: 1}, false, true},
		{map[int]int{ObjRedVineWorm: 1, ObjFishingBait: 2}, true, true},
		{map[int]int{ObjRedVineWorm: 2, ObjFishingBait: 1}, true, true},
		{map[int]int{ObjFishingBait: 20}, false, false},
		{map[int]int{ObjRawGiantCarp: 1, ObjFishingBait: 2}, false, true},
		{map[int]int{ObjRawGiantCarp: 1}, false, false},
	} {
		s := Snapshot{InvIDs: c.inv}
		if FcBaitReady(s) != c.ready || fcCanFish(s) != c.fish {
			t.Errorf("%v: ready %v fish %v, want %v %v", c.inv, FcBaitReady(s), fcCanFish(s), c.ready, c.fish)
		}
	}
}

// The slot is released once the account is seated at the pipes: the Stranger is the one shared step.
func TestTheSlotIsReleasedOnceSeatedAtThePipes(t *testing.T) {
	for _, c := range []struct {
		j    Status
		st   int
		sk   bool
		want bool
	}{
		{StatusNotStarted, 0, false, false}, {StatusInProgress, FcStarted, true, false}, {StatusInProgress, FcInComp, true, false},
		{StatusInProgress, FcGarlicComp, true, true}, {StatusInProgress, FcWonComp, true, true}, {StatusInProgress, FcGarlicComp, false, false},
		{StatusComplete, 0, false, true},
	} {
		if got := FcPastStranger(Snapshot{Journal: c.j, Stage: c.st, StageKnown: c.sk}); got != c.want {
			t.Errorf("%v stage %d known %v: past %v, want %v", c.j, c.st, c.sk, got, c.want)
		}
	}
}

func TestFishingContestModuleShape(t *testing.T) {
	m := FishingContest
	if m.Record.ID != "fishingcompo" || m.Record.Name != "Fishing Contest" || m.Record.QuestPoints != 1 {
		t.Errorf("record %+v", m.Record)
	}
	if len(m.Record.Requirements.Skills) != 1 || m.Record.Requirements.Skills[0] != (SkillReq{Skill: "fishing", Level: 10}) {
		t.Errorf("requirements %+v: Fishing 10 is the content's gate", m.Record.Requirements)
	}
	if m.MaxDeaths != 1 || !m.NoRetaliate || m.Decide == nil || m.ReadStage == nil || m.ExclusiveDone == nil || !m.BankSet {
		t.Errorf("module %+v", m)
	}
	if c, ok := m.StepDeathCaps[CustomFishDig]; !ok || c.Max != 1 || c.Skip != 24*time.Hour {
		t.Errorf("the worm trip's death cap %+v", c)
	}
	for _, id := range []int{ObjFishingPass, ObjFishingTrophy, ObjRedVineWorm, ObjFishingBait, ObjFishingRod, ObjGarlic, ObjSpade, ObjRawGiantCarp, CoinsID} {
		if !slices.Contains(m.ToolIDs, id) || !slices.Contains(FishKeepIDs(), id) {
			t.Errorf("obj %d is not a tool and a keep", id)
		}
	}
	for _, id := range m.Sustain.FoodIDs {
		if slices.Contains(m.ToolIDs, id) || !slices.Contains(FishKeepIDs(), id) {
			t.Errorf("food %d: tool %v keep %v", id, slices.Contains(m.ToolIDs, id), slices.Contains(FishKeepIDs(), id))
		}
	}
	// The compound is the Hemenster yard and nothing round it.
	for _, at := range []tile.Tile{FcGateInside, FcBonzoAnchor, FcPipeStand, {X: 2638, Z: 3444}, {X: 2635, Z: 3435}, {X: 2629, Z: 3413}, {X: 2636, Z: 3430}} {
		if !InFishingCompound(at) {
			t.Errorf("%v is not in the compound", at)
		}
	}
	for _, at := range []tile.Tile{FcGateOutside, FcSeersBank, FcDwarfAnchor, FcVineStand, {X: 2638, Z: 3447}, {X: 2640, Z: 3446, Level: 1}, {X: 2640, Z: 3424},
		{X: 2641, Z: 3413}, {X: 2643, Z: 3430}, {X: 2628, Z: 3420}, {X: 2638, Z: 3412}} {
		if InFishingCompound(at) {
			t.Errorf("%v is in the compound", at)
		}
	}
	// The record blocks an account under Fishing 10 and passes one at it.
	for _, c := range []struct {
		lvl int
		ok  bool
	}{{1, false}, {9, false}, {10, true}, {99, true}} {
		for _, r := range CheckRequirements(m.Record, PlayerState{SkillLevels: map[string]int{"fishing": c.lvl}}) {
			if r.OK != c.ok {
				t.Errorf("Fishing %d: requirement %+v", c.lvl, r)
			}
		}
	}
	if QuestTabName(FishingContestID) != FishingContestName {
		t.Errorf("tab name %q", QuestTabName(FishingContestID))
	}
}

// A stand in the wood is safe when it is two tiles or more from every tile a dog can stand on (its spawn and three about it).
func TestFcSafeFromDogs(t *testing.T) {
	for _, c := range []struct {
		at   tile.Tile
		safe bool
	}{
		{FcVineStand, true},
		{tile.Tile{X: 2632, Z: 3499}, false}, // a diagonal away from the box of the dog at 2636,3503
		{tile.Tile{X: 2631, Z: 3498}, true},
		{tile.Tile{X: 2669, Z: 3488}, false}, // a dog's own spawn
		{tile.Tile{X: 2671, Z: 3489}, false}, // inside its box
		{tile.Tile{X: 2673, Z: 3490}, false}, // beside the box (spawn + 3 + 1), within a hunt range of its edge
		{tile.Tile{X: 2674, Z: 3490}, true},  // two clear of it
		{tile.Tile{X: 2680, Z: 3480}, true},  // clear of everything
		{FcWoodOutside, true},
	} {
		if got := FcSafeFromDogs(c.at); got != c.safe {
			t.Errorf("%v: safe %v, want %v", c.at, got, c.safe)
		}
	}
}
