package combatplan

import (
	"testing"

	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/tile"
)

func TestLowestMelee(t *testing.T) {
	cases := []struct {
		name  string
		in    Levels
		skill Skill
		level int
	}{
		{"strength behind", Levels{Attack: 40, Strength: 20, Defence: 35}, SkillStrength, 20},
		{"defence behind", Levels{Attack: 40, Strength: 40, Defence: 1}, SkillDefence, 1},
		{"all equal ties to attack", Levels{Attack: 30, Strength: 30, Defence: 30}, SkillAttack, 30},
		{"attack behind", Levels{Attack: 5, Strength: 40, Defence: 40}, SkillAttack, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			skill, level := LowestMelee(c.in)
			if skill != c.skill || level != c.level {
				t.Fatalf("got %s %d, want %s %d", skill, level, c.skill, c.level)
			}
		})
	}
}

func TestStatToTrain(t *testing.T) {
	g := DefaultGoals()
	if s, ok := StatToTrain(Levels{Attack: 74, Strength: 20, Defence: 60}, g); !ok || s != SkillStrength {
		t.Fatalf("got %s %v, want strength", s, ok)
	}
	// Every melee goal met: nothing to train.
	if _, ok := StatToTrain(Levels{Attack: 75, Strength: 75, Defence: 75}, g); ok {
		t.Fatal("expected no stat to train once every goal is met")
	}
	// A met goal is skipped even when it is the lowest level of the three.
	s, ok := StatToTrain(Levels{Attack: 20, Strength: 70, Defence: 71}, Goals{Attack: 20, Strength: 75, Defence: 75})
	if !ok || s != SkillStrength {
		t.Fatalf("got %s %v, want strength (attack's goal is already met at 20)", s, ok)
	}
}

func TestMeleeGoalsMet(t *testing.T) {
	g := DefaultGoals()
	if MeleeGoalsMet(Levels{Attack: 75, Strength: 75, Defence: 74}, g) {
		t.Fatal("defence 74 should not count as met")
	}
	if !MeleeGoalsMet(Levels{Attack: 75, Strength: 80, Defence: 75}, g) {
		t.Fatal("expected met")
	}
}

// TestLadderBandsCoverEveryLevel guards the schedule itself: a gap would strand a run at a
// level with no rung and no reason.
func TestLadderBandsCoverEveryLevel(t *testing.T) {
	ladder := MeleeLadder()
	for level := 1; level < 75; level++ {
		found := false
		for _, r := range ladder {
			if level >= r.FromLevel && level < r.ToLevel {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("no rung covers lowest-melee level %d", level)
		}
	}
}

func TestPickRungPrefersProvenFreeRung(t *testing.T) {
	// At 45-60 three rungs are in band: the giants rung (free, proven, 12 spawns), the free
	// sewers moss giants (4 spawns) and the unproven members Brimhaven rung (5 spawns). The
	// unproven members one must lose even though it out-supplies the sewers, and among the two
	// free proven ones the deeper supply wins — which is the giants, measured at 449 XP/min
	// against the moss giants' 220 over one window.
	got := PickRung(MeleeLadder(), 50, 60, true)
	if !got.OK {
		t.Fatalf("expected a rung, got blocked: %s", got.Why)
	}
	if got.Rung.Members || !got.Rung.TravelProven {
		t.Fatalf("got rung %q (members %v, proven %v), want a free travel-proven rung",
			got.Rung.Key, got.Rung.Members, got.Rung.TravelProven)
	}
	if got.Rung.Key != "giants" {
		t.Fatalf("got rung %q, want the giants rung: free, proven, and the deepest supply in the 45-60 band", got.Rung.Key)
	}
}

func TestPickRungRefusesGiantsBelowCombatGate(t *testing.T) {
	// The Edgeville route killed a level-3 account. Below the gate the choice must fail with
	// a reason, not silently hand back a rung.
	got := PickRung(MeleeLadder(), 25, 10, true)
	if got.OK {
		t.Fatalf("expected the giants rung to be refused at combat level 10, got %q", got.Rung.Key)
	}
	if got.Why != BlockedCombatLevel {
		t.Fatalf("got reason %q, want %q", got.Why, BlockedCombatLevel)
	}
	if got.Detail != "giants" {
		t.Fatalf("got detail %q, want the rung that was refused", got.Detail)
	}
	// Above the gate the same band works.
	if ok := PickRung(MeleeLadder(), 25, 40, true); !ok.OK || ok.Rung.Key != "giants" {
		t.Fatalf("at combat 40 expected the giants rung, got %q (%s)", ok.Rung.Key, ok.Why)
	}
}

func TestPickRungOnAFreeWorldSkipsMembersRungs(t *testing.T) {
	// Every rung this ladder offers below 75 has a free-to-play option, so a free world still
	// trains; the members ones are simply not chosen.
	for _, level := range []int{5, 15, 30, 50, 65} {
		got := PickRung(MeleeLadder(), level, 75, false)
		if !got.OK {
			t.Fatalf("level %d on a free world: blocked (%s)", level, got.Why)
		}
		if got.Rung.Members {
			t.Fatalf("level %d on a free world chose members rung %q", level, got.Rung.Key)
		}
	}
}

func TestPickRungOutsideEveryBand(t *testing.T) {
	got := PickRung(MeleeLadder(), 99, 126, true)
	if got.OK || got.Why != BlockedNoBand {
		t.Fatalf("got %v %q, want a no-band block", got.OK, got.Why)
	}
}

// TestProvenRungsAreTheThreeWalkedDungeons pins the honesty of the TravelProven flag: only the
// entrances the route proof actually walked may claim it.
func TestProvenRungsAreTheThreeWalkedDungeons(t *testing.T) {
	proven := map[string]bool{}
	for _, r := range MeleeLadder() {
		if r.TravelProven {
			proven[r.Key] = true
		}
	}
	for _, key := range []string{"cows", "alkharid", "giants", "mossgiants", "icegiants"} {
		if !proven[key] {
			t.Fatalf("rung %q should be marked travel-proven", key)
		}
	}
	for _, key := range []string{"mossgiants-brimhaven", "ogres", "firegiants"} {
		if proven[key] {
			t.Fatalf("rung %q has no walked route and must not claim TravelProven", key)
		}
	}
}

// TestBoneRungsMatchContent guards the Prayer arithmetic: the 1-20 rungs drop nothing, and
// every giant rung drops big bones unconditionally.
func TestBoneRungsMatchContent(t *testing.T) {
	want := map[string]bool{
		"cows": false, "alkharid": false,
		"giants": true, "mossgiants": true, "icegiants": true,
		"mossgiants-brimhaven": true, "ogres": true, "firegiants": true,
	}
	for _, r := range MeleeLadder() {
		if got := r.Monster.BigBones; got != want[r.Key] {
			t.Fatalf("rung %q big bones = %v, want %v", r.Key, got, want[r.Key])
		}
	}
}

// TestLeashRespectsDisengageRange is the rule the engine enforces: a monster stops chasing
// past maxrange tiles from its own spawn. A rung whose stand sits further than that from every
// spawn could never hold a fight.
func TestLeashRespectsDisengageRange(t *testing.T) {
	for _, r := range MeleeLadder() {
		if len(r.Spawns) == 0 {
			t.Fatalf("rung %q has no verified spawn tiles", r.Key)
		}
		reachable := 0
		for _, s := range r.Spawns {
			if tile.Chebyshev(r.Stand, s) <= r.Monster.DisengageTiles {
				reachable++
			}
		}
		if reachable == 0 {
			t.Fatalf("rung %q stands more than %d tiles from every one of its %d spawns, so no monster could fight it there",
				r.Key, r.Monster.DisengageTiles, len(r.Spawns))
		}
	}
}

// TestPickRungHasNoAnswerAtSeventyFive pins the trap PrayerRung exists for, so nobody "tidies"
// PrayerRung away later without meeting it again.
//
// A finished account is 75/75/75 and the ladder bands on the level of the stat being trained, so
// the band is 75 — and icegiants ends at 75 EXCLUSIVE, which means the top of the ladder is the
// one level no rung covers. combattrainer turns this answer into a returned error, so a run that
// met every combat goal with Prayer still short used to stop with an error instead of finishing
// its bones.
func TestPickRungHasNoAnswerAtSeventyFive(t *testing.T) {
	got := PickRung(MeleeLadder(), 75, 126, true)
	if got.OK {
		t.Fatalf("expected no rung at the top of the ladder, got %q", got.Rung.Key)
	}
	if got.Why != BlockedNoBand {
		t.Fatalf("got reason %q, want %q", got.Why, BlockedNoBand)
	}
}

func TestPrayerRungIsTheCheapestKillThatDropsBones(t *testing.T) {
	got := PrayerRung(MeleeLadder(), 40, false)
	if !got.OK {
		t.Fatalf("expected a bone rung at combat 40, got blocked: %s (%s)", got.Why, got.Detail)
	}
	if got.Rung.Key != "giants" {
		t.Fatalf("got rung %q, want %q: every rung here drops one big bone per kill, so the shortest-lived monster is the fastest Prayer", got.Rung.Key, "giants")
	}
	if !got.Rung.Monster.BigBones {
		t.Fatalf("rung %q was chosen to farm bones and does not drop any", got.Rung.Key)
	}
	// The cheapest kill really is the cheapest: nothing else on the ladder that drops bones has
	// fewer hitpoints.
	for _, r := range MeleeLadder() {
		if r.Monster.BigBones && r.Monster.Hitpoints < got.Rung.Monster.Hitpoints {
			t.Fatalf("rung %q drops bones and its %s has %d hitpoints against the chosen %s's %d",
				r.Key, r.Monster.Name, r.Monster.Hitpoints, got.Rung.Monster.Name, got.Rung.Monster.Hitpoints)
		}
	}
}

func TestPrayerRungRefusesBelowTheGiantsCombatGate(t *testing.T) {
	// The same gate PickRung enforces: the Edgeville route killed a level-3 account, so a bone
	// hunt must not be the way around it.
	got := PrayerRung(MeleeLadder(), 10, false)
	if got.OK {
		t.Fatalf("expected the bone rungs to be refused at combat level 10, got %q", got.Rung.Key)
	}
	if got.Why != BlockedCombatLevel || got.Detail != "giants" {
		t.Fatalf("got %q (%s), want a combat-level block naming the giants rung", got.Why, got.Detail)
	}
}

func TestPrayerRungOnAFreeWorldNeverChoosesAMembersRung(t *testing.T) {
	for _, combat := range []int{40, 80, 126} {
		got := PrayerRung(MeleeLadder(), combat, false)
		if !got.OK {
			t.Fatalf("combat %d on a free world: blocked (%s)", combat, got.Why)
		}
		if got.Rung.Members {
			t.Fatalf("combat %d on a free world chose members rung %q", combat, got.Rung.Key)
		}
		if !got.Rung.TravelProven {
			t.Fatalf("combat %d chose rung %q, which has no route this client has walked", combat, got.Rung.Key)
		}
	}
}

// TestPrayerFortyThreeIsAffordableFromTheLadderItself is the arithmetic the plan's "Prayer 43
// arrives on its own from the big bones the giant rungs drop" rests on, checked rather than
// asserted.
//
// Every giant rung drops exactly one big bone per kill (param=death_drop,big_bones, added before
// any random table is rolled) and a buried big bone is 15 Prayer XP, measured live: -mode bones
// buried 25 of them and the stat table moved 0 -> 375. Melee XP is 4 per point of damage
// (skill_combat's [proc,give_combat_experience]), so a kill is worth 4x the monster's hitpoints
// spread over the three melee stats.
//
// If this ever fails, the claim in docs/combat_trainer_plan.md is wrong and the trainer needs the
// dedicated Prayer trip that milestone 5 describes.
func TestPrayerFortyThreeIsAffordableFromTheLadderItself(t *testing.T) {
	const bigBoneXP = 15
	bonesNeeded := (levels.XPAtLevel(43) + bigBoneXP - 1) / bigBoneXP

	// Kills available between melee 25 (where the first bone-dropping rung starts) and 75, over
	// the three melee stats the ladder trains.
	//
	// Counted through PickRung one level at a time rather than by summing each rung's own band.
	// Bands are allowed to OVERLAP — the giants rung and the sewers moss giants both cover
	// 45-60, and PickRung decides between them — so summing rungs would count that stretch twice
	// and inflate the answer with kills no run would ever make. Asking the same function the bot
	// asks makes this the ladder the account actually climbs.
	kills := 0
	for level := 25; level < 75; level++ {
		pick := PickRung(MeleeLadder(), level, 200, false)
		if !pick.OK || !pick.Rung.Monster.BigBones {
			continue
		}
		xpForLevel := (levels.XPAtLevel(level+1) - levels.XPAtLevel(level)) * 3
		kills += xpForLevel / (4 * pick.Rung.Monster.Hitpoints)
	}
	if kills <= bonesNeeded {
		t.Fatalf("the free-to-play bone rungs yield about %d kills between melee 25 and 75, against the %d big bones 43 Prayer costs — Prayer would NOT arrive on its own and the trainer needs its own Prayer trip",
			kills, bonesNeeded)
	}
}

func TestFoodPerTrip(t *testing.T) {
	giants, _ := RungByKey(MeleeLadder(), "giants")
	fire, _ := RungByKey(MeleeLadder(), "firegiants")
	// A harder rung asks for more food than an easier one at the same heal size.
	if FoodPerTrip(fire, 12) <= FoodPerTrip(giants, 12) {
		t.Fatalf("fire giants (max hit %d) should want more food than giants (max hit %d)",
			fire.Monster.MaxHit, giants.Monster.MaxHit)
	}
	// And the load stays inside the cap whatever the numbers say.
	if n := FoodPerTrip(fire, 1); n > 12 {
		t.Fatalf("food per trip %d exceeds the pack cap", n)
	}
	if n := FoodPerTrip(giants, 100); n < 4 {
		t.Fatalf("food per trip %d is below the floor", n)
	}
}
