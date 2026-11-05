package questmods

// Lost City through the real engine (Engine.Run) on a simulated session, in the manner of the other fillers' (workshop_engine_test.go): the module's
// own Decide runs against snapshots built from a fake pack, the account's tile, its levels and the quest tab's colour, and every step it asks for is
// performed by a stub that changes that world the way the server would. What is proven is the state machine under the engine's bookkeeping (Ran and
// Failed, the watchdog, the park requests, the death cap, the completion record and the closing hook): the quest reaches Complete in the order the plan
// intends, an account the model refuses never leaves the mainland, and a leg that cannot be done parks the quest with its reason. What is not proven here
// is each custom's own live execution (the strip, the ship, the ladder, the zombies, the corridor, the casts, the Magic door, the shed): those are the
// real-data tests and the live logs.

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Skill indices of the protocol's stat table (gamestate.SkillNames).
const (
	lcMagicStat       = 6
	lcWoodcuttingStat = 8
	lcCraftingStat    = 12
)

// lcRig is Lost City's state on the rig: r.stage is %zanaris and the tab follows it. casts are the spellbook buttons the account pressed (the
// component ids, in order); dropCasts makes the server ignore them, as a dropped click would be.
type lcRig struct {
	*csRig
	spirits, trips int
	casts          []int
	dropCasts      bool
}

// server answers the exit teleports as teleport.rs2 does: the spell's level and runes are checked, the runes spent and the account set down at the
// spell's landing. Every press of a spell of the book is recorded.
func (r *lcRig) server(b []byte) {
	if len(b) < 3 || int(b[0]) != clientprot.IF_BUTTON {
		return
	}
	comp := int(b[1])<<8 | int(b[2])
	if !lcSpellButton(comp) {
		return // a tab of the side bar (the engine reads the quest list through them)
	}
	r.casts = append(r.casts, comp)
	if r.dropCasts {
		return
	}
	for _, e := range questplan.LcExits() {
		if want, ok := actions.TeleportComponent(e.Name); !ok || want != comp {
			continue
		}
		if questrunner.BaseLevels(r.e.D)["magic"] < e.Level {
			return
		}
		for id, n := range e.Runes {
			if r.held(id) < n {
				return
			}
		}
		for id, n := range e.Runes {
			r.take(id, n)
		}
		r.at(e.Landing)
	}
}

// newLcRigBase is newCSRig with the runes stackable, as they are in the game: the kit's two hundred of them would not fit a pack of single slots.
func newLcRigBase(t *testing.T, at tile.Tile) *csRig {
	t.Helper()
	stackable := map[int32]bool{questplan.CoinsID: true}
	for _, id := range questplan.LcRuneIDs {
		stackable[int32(id)] = true
	}
	sim := actionsim.New(actionsim.Objs{Stackable: func(id int32) bool { return stackable[id] }}, nil)
	sim.StandAt(at.X, at.Z)
	r := &csRig{t: t, sim: sim}
	d := &questrunner.Deps{
		Session: sim, Components: actionsim.Components(),
		Log:   func(s string) { r.log = append(r.log, s) },
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
	}
	prov := &questrunner.Provisioner{Bank: &questrunner.BankView{Counts: map[int]int{}, Known: true, At: time.Now().Add(time.Hour)}}
	r.e = &questrunner.Engine{D: d, Prov: prov, Store: questplan.NewStore()}
	return r
}

// newLcRig is the fleet's usual caster (Attack 53, Strength 41, Defence 50, Hitpoints 55, Magic 66) at stage on the tile.
func newLcRig(t *testing.T, stage int, at tile.Tile) *lcRig {
	return newLcRigWith(t, stage, at, [5]int{53, 41, 50, 55, 66})
}

// newLcRigWith seeds the levels once (the highest hitpoints level seen is the account's maximum, so they cannot be seeded twice).
func newLcRigWith(t *testing.T, stage int, at tile.Tile, levels [5]int) *lcRig {
	r := &lcRig{csRig: newLcRigBase(t, at)}
	r.sim.OnSend = r.server
	r.stageKnown = true
	r.setStage(stage)
	lcSeed(r.csRig, levels[0], levels[1], levels[2], levels[3], levels[4])
	return r
}

// lcSeed seeds the account's levels: the fleet's usual caster by default.
func lcSeed(r *csRig, att, str, def, hp, mag int) {
	osSeed(r, osAttack, att)
	osSeed(r, osStrength, str)
	osSeed(r, osDefence, def)
	osSeed(r, gamestate.HitpointsStat, hp)
	osSeed(r, lcMagicStat, mag)
	osSeed(r, lcCraftingStat, 40)
	osSeed(r, lcWoodcuttingStat, 40)
}

func (r *lcRig) setStage(n int) {
	r.stage = n
	status := quests.InProgress
	switch n {
	case questplan.LcNotStarted:
		status = quests.NotStarted
	case questplan.LcComplete:
		status = quests.Complete
	}
	r.setTab(questplan.LostCityName, status)
}

func (r *lcRig) at(at tile.Tile) { r.sim.StandAtLevel(at.X, at.Z, at.Level) }

// lcKitGive gives the pack what a trip's kit round would: every required line up to its quantity.
func (r *lcRig) kit(fight bool) {
	s := questplan.Snapshot{Levels: map[string]int{"magic": 66}}
	for _, l := range questplan.LcTripKit(s, fight) {
		if have := r.held(l.ID); have < l.Qty {
			r.give(l.ID, l.Qty-have)
		}
	}
}

// apply plays the content's side of the quest and the world's.
func (r *lcRig) apply(step questplan.Step) bool {
	switch step.Kind {
	case questplan.StepTalk:
		if step.Stop.ID == questplan.NpcLcWarrior && r.stage == questplan.LcNotStarted {
			r.setStage(questplan.LcStarted)
		}
		return true
	case questplan.StepDeposit, questplan.StepScanBank:
		return true
	case questplan.StepWithdraw:
		for _, it := range step.Items {
			r.give(it.ID, it.Qty)
		}
		return true
	case questplan.StepProvision:
		switch step.KitName {
		case questplan.KitLcAxe:
			r.give(step.Kit[0].ID, 1)
		case questplan.KitLcTrip:
			for _, l := range step.Kit {
				if have := r.held(l.ID); have < l.Qty {
					r.give(l.ID, l.Qty-have)
				}
			}
		default:
			r.t.Errorf("an unknown kit %q", step.KitName)
			return false
		}
		return true
	case questplan.StepUseOn:
		if step.ItemID != questplan.ObjKnife || step.TargetID != questplan.ObjLcBranch || r.held(questplan.ObjLcBranch) == 0 || r.held(questplan.ObjKnife) == 0 {
			r.t.Errorf("a use-on that is not the knife on a held branch: %+v", step)
			return false
		}
		r.take(questplan.ObjLcBranch, 1)
		r.give(questplan.ObjLcStaff, 1)
		if r.stage == questplan.LcTreeChopped {
			r.setStage(questplan.LcStaffMade)
		}
		return true
	case questplan.StepCustom:
		return r.custom(step)
	}
	r.t.Errorf("the scenario has no answer for the step %q (%s)", step.Kind, step.Describe())
	return false
}

func (r *lcRig) custom(step questplan.Step) bool {
	switch step.Custom {
	case questplan.CustomLcShamus:
		if r.held(questplan.ObjBronzeAxe)+r.held(questplan.ObjIronAxe) == 0 {
			r.t.Errorf("Shamus asked for with no axe in the pack")
			return false
		}
		r.setStage(questplan.LcSpokenShamus)
	case questplan.CustomLcSail:
		// The strip: every axe and the staff banked, bare; the ship sets the account down on Entrana's deck.
		for _, id := range questplan.LcForbiddenOnEntrana() {
			if n := r.held(id); n > 0 {
				r.take(id, n)
			}
		}
		r.at(tile.Tile{X: 2834, Z: 3331, Level: 1})
	case questplan.CustomLcDescend:
		r.at(questplan.LcDungeonArrival)
	case questplan.CustomLcAxe:
		r.give(questplan.ObjBronzeAxe, 1)
	case questplan.CustomLcSpirit:
		r.spirits++
		r.at(questplan.LcTreeStand)
		r.take(questplan.ObjShark, 3)
		r.take(questplan.ObjFireRune, 105)
		r.take(questplan.ObjAirRune, 84)
		r.take(questplan.ObjDeathRune, 21)
		r.setStage(questplan.LcSpiritDefeated)
	case questplan.CustomLcBranch:
		r.give(questplan.ObjLcBranch, 1)
		if r.stage == questplan.LcSpiritDefeated {
			r.setStage(questplan.LcTreeChopped)
		}
	case questplan.CustomLcPortal:
		// The real way out, against the rig's server: the cast needs the runes the kit left after the fight.
		if !lcLeaveDungeon(r.e.D) {
			r.t.Errorf("the way out of the dungeon failed with the pack %v: %v", r.pack(), r.log)
			return false
		}
	case questplan.CustomLcZanaris:
		if r.held(questplan.ObjLcStaff) == 0 {
			r.t.Errorf("the shed's door asked for with no staff")
			return false
		}
		r.at(questplan.LcZanarisArrival)
		r.setStage(questplan.LcComplete)
	default:
		r.t.Errorf("the scenario has no answer for the custom %q", step.Custom)
		return false
	}
	return true
}

// run is csRig.run with the quest's closing hook recorded and not run (the walk out of Zanaris wants a walker).
func (r *lcRig) runQuest(apply func(questplan.Step) bool) (questrunner.Result, int) {
	q := LostCity()
	finished := 0
	q.Finish = func(*questrunner.Engine) { finished++ }
	return r.csRig.run(q, apply), finished
}

// The whole quest in the content's order, every executor a stub.
func TestLostCityReachesCompleteThroughTheEngine(t *testing.T) {
	r := newLcRig(t, questplan.LcNotStarted, questplan.DraynorBank)
	res, finished := r.runQuest(r.apply)
	if res.Outcome != questrunner.OutcomeComplete || finished != 1 {
		t.Fatalf("the run ended %s (%s), the closing hook ran %d times: %v", res.Outcome, res.Reason, finished, r.log)
	}
	r.wantSteps("talk to Warrior", "provision lc-axe", "lc-shamus", "provision lc-trip", "lc-sail", "lc-descend", "lc-axe", "lc-spirit", "lc-branch",
		"use Knife on Dramen branch", "lc-portal", "lc-zanaris")
	// The way out was the Varrock Teleport, cast once, with the runes the kit held back from the fight: the account was set down in Varrock, the
	// Wilderness never entered, and what is left of the pack is the staff, the food and the knife.
	if len(r.casts) != 1 || r.casts[0] != lcVarrockComponent(t) {
		t.Errorf("the spellbook buttons pressed are %v, want the Varrock Teleport's alone", r.casts)
	}
	for _, id := range []int{questplan.ObjFireRune, questplan.ObjAirRune, questplan.ObjLcLawRune, questplan.ObjDeathRune} {
		if n := r.held(id); n != 0 {
			t.Errorf("%d of rune %d are left: the kit is exactly the fight and the cast out", n, id)
		}
	}
	if got := r.e.Store.Record("zanaris").Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	// The account sailed without an axe or a staff, and came out of the dungeon with the staff.
	if r.held(questplan.ObjLcStaff) != 1 || r.held(questplan.ObjLcBranch) != 0 {
		t.Errorf("the pack ends with %d staves and %d branches", r.held(questplan.ObjLcStaff), r.held(questplan.ObjLcBranch))
	}
	if r.spirits != 1 {
		t.Errorf("the spirit was fought %d times", r.spirits)
	}
}

// An account the model refuses is parked at the very first tick, for six hours, with the reason, and not one step is taken: the warrior is not
// talked to, no axe is bought, nothing is carried to Entrana.
func TestLostCityAnAccountThatCannotBeatTheSpiritNeverBegins(t *testing.T) {
	for _, c := range []struct {
		name     string
		levels   [5]int
		contains string
	}{
		{"a melee account with no Magic", [5]int{75, 75, 70, 74, 1}, "Magic 1/25"},
		{"a weak caster at the Magic of the exit", [5]int{1, 1, 1, 60, 25}, "cannot beat the Tree spirit"},
		{"a caster with too few hitpoints for the corridor", [5]int{40, 40, 30, 40, 66}, "greater demons"},
	} {
		r := newLcRigWith(t, questplan.LcNotStarted, questplan.DraynorBank, c.levels)
		res, finished := r.runQuest(r.apply)
		if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, c.contains) || len(r.steps) != 0 || finished != 0 {
			t.Fatalf("%s: the run ended %s (%s) after %v, closing hook %d", c.name, res.Outcome, res.Reason, r.described(), finished)
		}
		if parked, _ := r.e.Store.Parked(questplan.LostCityID, time.Now().Add(5*time.Hour)); !parked {
			t.Errorf("%s: not parked for the six hours", c.name)
		}
		if parked, _ := r.e.Store.Parked(questplan.LostCityID, time.Now().Add(7*time.Hour)); parked {
			t.Errorf("%s: parked past the six hours", c.name)
		}
	}
	// The same refusal holds at stage 1, after the warrior, before any axe is looked for.
	r := newLcRigWith(t, questplan.LcStarted, questplan.DraynorBank, [5]int{75, 75, 70, 74, 1})
	if res, _ := r.runQuest(r.apply); res.Outcome != questrunner.OutcomeParked || len(r.steps) != 0 {
		t.Fatalf("stage 1: the run ended %s (%s) after %v", res.Outcome, res.Reason, r.described())
	}
}

// The spirit that is not beaten parks the quest for six hours at once (the Decide's wait), the fight is run once, and nothing after it is.
func TestLostCityASpiritThatWasNotBeatenParksTheQuestForSixHours(t *testing.T) {
	r := newLcRig(t, questplan.LcSpokenShamus, questplan.LcZombieStand)
	r.kit(true)
	r.give(questplan.ObjBronzeAxe, 1)
	runs := 0
	res, _ := r.runQuest(func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomLcSpirit {
			runs++
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "Tree spirit") {
		t.Fatalf("the run ended %s (%s) after %d runs: %v", res.Outcome, res.Reason, runs, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.LostCityID, time.Now().Add(5*time.Hour)); !parked {
		t.Error("not parked for the six hours")
	}
	if parked, _ := r.e.Store.Parked(questplan.LostCityID, time.Now().Add(7*time.Hour)); parked {
		t.Error("parked past the six hours")
	}
}

// A death at the spirit parks the quest for a day at once (MaxDeaths 1 and the step's own cap), not after the escalating backoff.
func TestLostCityADeathAtTheSpiritParksItForADay(t *testing.T) {
	r := newLcRig(t, questplan.LcSpokenShamus, questplan.LcZombieStand)
	r.e.D.Chat = chatlog.New()
	r.kit(true)
	r.give(questplan.ObjIronAxe, 1)
	runs := 0
	res, _ := r.runQuest(func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomLcSpirit {
			runs++
			r.e.D.Chat.Record("Oh dear, you are dead!")
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "cap is 1") {
		t.Fatalf("the run ended %s (%s) after %d runs: %v", res.Outcome, res.Reason, runs, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.LostCityID, time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
}

// Monks that will not sail park the quest for half an hour, with the reason; the sail is run once.
func TestLostCityMonksThatWillNotSailParkTheQuestForHalfAnHour(t *testing.T) {
	r := newLcRig(t, questplan.LcSpokenShamus, questplan.DraynorBank)
	runs := 0
	res, _ := r.runQuest(func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomLcSail {
			runs++
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "monks did not sail") {
		t.Fatalf("the run ended %s (%s) after %d runs: %v", res.Outcome, res.Reason, runs, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.LostCityID, time.Now().Add(29*time.Minute)); !parked {
		t.Error("not parked for the half hour")
	}
	if parked, _ := r.e.Store.Parked(questplan.LostCityID, time.Now().Add(31*time.Minute)); parked {
		t.Error("parked past the half hour")
	}
}

// The zombies that give no axe: the custom asks for a park of its own (the account stays on their ground for the next run), served as asked.
func TestLostCityZombiesThatGiveNoAxeParkTheQuest(t *testing.T) {
	r := newLcRig(t, questplan.LcSpiritDefeated, questplan.LcZombieStand)
	r.kit(false)
	runs := 0
	res, _ := r.runQuest(func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomLcAxe {
			runs++
			r.e.RequestPark("no axe from the Entrana zombies in 12m0s (9 zombies killed)", questplan.LcLegPark)
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "Entrana zombies") {
		t.Fatalf("the run ended %s (%s) after %d runs: %v", res.Outcome, res.Reason, runs, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.LostCityID, time.Now().Add(29*time.Minute)); !parked {
		t.Error("not parked for the half hour")
	}
}

// Stage 4 with the branch and a knife in the Wilderness (an account that came out of the Magic door and was relogged): carved where it stands, then the shed.
func TestLostCityABranchIsCarvedWhereTheAccountStands(t *testing.T) {
	r := newLcRig(t, questplan.LcTreeChopped, questplan.LcWildernessLanding)
	r.give(questplan.ObjLcBranch, 1)
	r.give(questplan.ObjKnife, 1)
	res, _ := r.runQuest(r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps("use Knife on Dramen branch", "lc-zanaris")
}

// The staff the stage loop banked is drawn from the bank, not fetched from the dungeon again.
func TestLostCityAStaffInTheBankIsDrawn(t *testing.T) {
	r := newLcRig(t, questplan.LcStaffMade, questplan.DraynorBank)
	r.e.Prov.Bank.Counts[questplan.ObjLcStaff] = 1
	res, _ := r.runQuest(r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps("withdraw Dramen staff", "lc-zanaris")
	// With none in the bank the account goes down again for a branch: a trip's kit, the ship, the ladder, an axe, a branch, the knife and the door.
	r = newLcRig(t, questplan.LcStaffMade, questplan.DraynorBank)
	res, _ = r.runQuest(r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the second run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps("provision lc-trip", "lc-sail", "lc-descend", "lc-axe", "lc-branch", "use Knife on Dramen branch", "lc-portal", "lc-zanaris")
	if r.spirits != 0 {
		t.Errorf("the spirit was fought %d times on a trip that needs none", r.spirits)
	}
	// The trip's kit has the corridor's food and the runes of the cast out, and none of the spell's.
	got := map[int]int{}
	for _, l := range r.stepNamed("provision lc-trip").Kit {
		got[l.ID] = l.Qty
	}
	if want := map[int]int{questplan.ObjKnife: 1, questplan.ObjShark: questplan.LcTripSharks, questplan.ObjFireRune: 1, questplan.ObjAirRune: 3, questplan.ObjLcLawRune: 1}; !mapsEqualInts(got, want) {
		t.Errorf("the second trip's kit is %v, want %v", got, want)
	}
	if len(r.casts) != 1 || r.casts[0] != lcVarrockComponent(t) {
		t.Errorf("the spellbook buttons pressed are %v, want the Varrock Teleport's alone", r.casts)
	}
}

// A complete tab is Done at the first tick.
func TestLostCityACompleteTabIsDone(t *testing.T) {
	r := newLcRig(t, questplan.LcComplete, questplan.DraynorBank)
	res, finished := r.runQuest(r.apply)
	if res.Outcome != questrunner.OutcomeComplete || len(r.steps) != 0 || finished != 1 {
		t.Fatalf("the run ended %s (%s) after %v, closing hook %d", res.Outcome, res.Reason, r.described(), finished)
	}
}

// The closing hook does nothing for an account that is not in Zanaris (and wants no walker for it).
func TestLostCityTheClosingHookLeavesAnAccountOutsideZanarisAlone(t *testing.T) {
	r := newLcRig(t, questplan.LcComplete, questplan.DraynorBank)
	r.e.D.Walker = nil
	lcLeaveZanaris(r.e)
	if len(r.log) != 0 {
		t.Errorf("the hook said %v", r.log)
	}
}

func mapsEqualInts(a, b map[int]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// lcSpellButton reports a button of the teleport spells of the book.
func lcSpellButton(comp int) bool {
	for _, name := range []string{"varrock", "lumbridge", "falador", "camelot", "ardougne", "watchtower", "trollheim"} {
		if id, ok := actions.TeleportComponent(name); ok && id == comp {
			return true
		}
	}
	return false
}

// lcVarrockComponent is the spellbook button of the Varrock Teleport (the table actions.Teleport casts by).
func lcVarrockComponent(t *testing.T) int {
	t.Helper()
	id, ok := actions.TeleportComponent("Varrock Teleport")
	if !ok {
		t.Fatal("actions does not know the Varrock Teleport")
	}
	return id
}

// pack lists the rig's pack by object id, for a failure message.
func (r *lcRig) pack() map[int]int { return questrunner.PackCounts(r.e.D) }

// The way out of the dungeon, by itself, on the rig: a teleport from where the account stands (the corridor and the Magic door never touched), the
// nearest the pack allows, the runes spent and no others; with nothing to cast the last resort is tried and says so; a cast the server drops is
// retried and then left for the next run, never sent into the Wilderness.
func TestLostCityTheWayOutOfTheDungeonIsATeleport(t *testing.T) {
	sentDoor := func(r *lcRig) bool {
		for _, b := range r.sim.Sent() {
			if len(b) == 0 {
				continue
			}
			switch int(b[0]) {
			case clientprot.OPLOC1, clientprot.OPLOC2, clientprot.OPLOC3, clientprot.OPLOC4, clientprot.OPLOC5:
				return true
			}
		}
		return false
	}
	logged := func(r *lcRig, sub string) bool {
		for _, l := range r.log {
			if strings.Contains(l, sub) {
				return true
			}
		}
		return false
	}

	// Varrock's runes: the kit's. Cast from the tree's stand; Varrock's three runes spent; the account in Varrock.
	r := newLcRig(t, questplan.LcSpiritDefeated, questplan.LcTreeStand)
	r.kit(false)
	if !lcLeaveDungeon(r.e.D) {
		t.Fatalf("the way out failed: %v", r.log)
	}
	if got := questrunner.CurrentTile(r.e.D); got != questplan.LcKitExit().Landing {
		t.Errorf("the account is at %v, want Varrock's landing %v", got, questplan.LcKitExit().Landing)
	}
	if len(r.casts) != 1 || r.casts[0] != lcVarrockComponent(t) || sentDoor(r) {
		t.Errorf("casts %v, the door touched %v", r.casts, sentDoor(r))
	}
	if r.held(questplan.ObjFireRune) != 0 || r.held(questplan.ObjAirRune) != 0 || r.held(questplan.ObjLcLawRune) != 0 || r.held(questplan.ObjShark) != questplan.LcTripSharks {
		t.Errorf("the pack after the cast: %v", r.pack())
	}
	if !logged(r, "out of the dungeon by the Varrock Teleport") || logged(r, "last resort") {
		t.Errorf("the log: %v", r.log)
	}
	// Out already: nothing sent.
	before := len(r.casts)
	if !lcLeaveDungeon(r.e.D) || len(r.casts) != before {
		t.Errorf("an account that is out was cast for again")
	}

	// An earth rune in the pack: Lumbridge's, the nearer; Varrock's runes are kept.
	r = newLcRig(t, questplan.LcSpiritDefeated, questplan.LcZombieStand)
	r.kit(false)
	r.give(questplan.ObjEarthRune, 1)
	if !lcLeaveDungeon(r.e.D) {
		t.Fatalf("the way out failed: %v", r.log)
	}
	lumbridge, _ := actions.TeleportComponent("Lumbridge Teleport")
	if len(r.casts) != 1 || r.casts[0] != lumbridge || questrunner.CurrentTile(r.e.D) != questplan.LcExits()[0].Landing {
		t.Errorf("casts %v, at %v: want Lumbridge's", r.casts, questrunner.CurrentTile(r.e.D))
	}
	if r.held(questplan.ObjFireRune) != 1 || r.held(questplan.ObjEarthRune) != 0 {
		t.Errorf("the pack after the cast: %v", r.pack())
	}

	// Nothing to cast with (the law rune is gone): nothing is pressed and the last resort is announced. The rig stands the account at the Magic door's
	// stand (it has no walker) and holds no loc index, so the door is not found and the account is still in the dungeon.
	r = newLcRig(t, questplan.LcSpiritDefeated, questplan.LcDoorStand)
	r.kit(false)
	r.take(questplan.ObjLcLawRune, 1)
	if lcLeaveDungeon(r.e.D) {
		t.Fatal("the account left the dungeon with nothing to cast and no walker")
	}
	if len(r.casts) != 0 || !logged(r, "0 of 1 law rune") || !logged(r, "last resort") {
		t.Errorf("casts %v, the log: %v", r.casts, r.log)
	}

	// Magic 24: Varrock's runes are held and the level is not.
	r = newLcRigWith(t, questplan.LcSpiritDefeated, questplan.LcDoorStand, [5]int{53, 41, 50, 55, 24})
	r.kit(false)
	if lcLeaveDungeon(r.e.D) || len(r.casts) != 0 || !logged(r, "Magic 24 is under the 25") || !logged(r, "last resort") {
		t.Errorf("Magic 24: casts %v, the log: %v", r.casts, r.log)
	}

	// A cast the server drops: pressed lcCastTries times, the runes never spent, the Magic door left alone and the Wilderness never entered.
	old := lcCastWait
	lcCastWait = 40 * time.Millisecond
	defer func() { lcCastWait = old }()
	r = newLcRig(t, questplan.LcSpiritDefeated, questplan.LcTreeStand)
	r.kit(false)
	r.dropCasts = true
	if lcLeaveDungeon(r.e.D) {
		t.Fatal("the account left the dungeon though every cast was dropped")
	}
	if len(r.casts) != lcCastTries || sentDoor(r) || logged(r, "last resort") || r.held(questplan.ObjLcLawRune) != 1 {
		t.Errorf("casts %v, door touched %v, the log: %v", r.casts, sentDoor(r), r.log)
	}
	if !questplan.InLcDungeon(questrunner.CurrentTile(r.e.D)) {
		t.Errorf("the account moved to %v", questrunner.CurrentTile(r.e.D))
	}
}

// The zombies fought are the west side's, outside the black demons' ground and not in another player's fight.
func TestLostCityWhichZombiesAreFought(t *testing.T) {
	zombie := func(z, x int, mod func(*entityquery.Entity)) entityquery.Entity {
		e := entityquery.Entity{ID: questplan.NpcLcZombie, X: x, Z: z}
		if mod != nil {
			mod(&e)
		}
		return e
	}
	for _, c := range []struct {
		name string
		e    entityquery.Entity
		want bool
	}{
		{"on the west side", zombie(9760, 2846, nil), true},
		{"at the northern edge", zombie(9764, 2849, nil), true},
		{"wandered north", zombie(9765, 2845, nil), false},
		{"in a black demon's ground", zombie(9768, 2855, nil), false},
		{"another npc", zombie(9760, 2846, func(e *entityquery.Entity) { e.ID = 77 }), false},
		{"in another player's fight", zombie(9760, 2846, func(e *entityquery.Entity) { e.InCombat, e.FaceEntity = true, 32768+5 }), false},
		{"in the account's own fight", zombie(9760, 2846, func(e *entityquery.Entity) { e.InCombat, e.FaceEntity = true, 32768+3 }), true},
	} {
		if got := lcZombieOK(c.e, 3); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// The strip's keep set holds nothing the monks refuse and does hold what the trip needs.
func TestLostCityTheStripKeepsTheTripAndNothingForbidden(t *testing.T) {
	keep := lcKeepSet()
	for _, id := range questplan.LcForbiddenOnEntrana() {
		if keep[id] {
			t.Errorf("the strip keeps %d, which the monks refuse", id)
		}
	}
	for _, id := range []int{questplan.ObjKnife, questplan.ObjShark, questplan.ObjFireRune, questplan.ObjAirRune, questplan.ObjDeathRune, questplan.ObjLcLawRune, questplan.CoinsID, questplan.ObjLcBranch} {
		if !keep[id] {
			t.Errorf("the strip does not keep %d", id)
		}
	}
}
