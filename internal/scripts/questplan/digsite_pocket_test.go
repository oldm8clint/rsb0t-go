package questplan

// The workmen's pockets cost a hitpoint a miss (pickpocket_digworkman1 does damage_self(1), which can kill) and the quest picks them for a
// sample, a brush and up to two ropes. These tests state the pure half of that price: the floor the pocket loop steals above, the
// advice it takes from its hitpoints and its food, how long a rest parks the quest, and the food kit that goes in front of a step that picks
// pockets when the account's hitpoints would not cover a bad rope. The loop itself is run in questmods/digsite_pocket_test.go.

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDigPocketFloorAndResume(t *testing.T) {
	for _, c := range []struct{ max, floor, resume int }{
		{0, 0, 0},
		{10, 4, 7},
		{12, 4, 7},
		{16, 4, 8},
		{20, 5, 10},
		{30, 8, 15},
		{40, 10, 20},
		{70, 18, 35},
		{99, 25, 50},
	} {
		if got := DigPocketFloor(c.max); got != c.floor {
			t.Errorf("DigPocketFloor(%d) = %d, want %d", c.max, got, c.floor)
		}
		if got := DigPocketResume(c.max); got != c.resume {
			t.Errorf("DigPocketResume(%d) = %d, want %d", c.max, got, c.resume)
		}
	}
	// Whatever the maximum, the floor is under it, three over the death line when the maximum allows, and the resume is over the floor.
	for max := 1; max <= 130; max++ {
		floor, resume := DigPocketFloor(max), DigPocketResume(max)
		if floor < 0 || floor >= max {
			t.Errorf("maximum %d: floor %d is not under it", max, floor)
		}
		if max >= 10 && floor < DigPocketMinHP {
			t.Errorf("maximum %d: floor %d is under the %d hitpoints a miss needs left", max, floor, DigPocketMinHP)
		}
		if resume > max || resume <= floor {
			t.Errorf("maximum %d: resume %d is not between the floor %d and the maximum", max, resume, floor)
		}
		if max >= 10 && resume < floor+DigPocketResumeMargin {
			t.Errorf("maximum %d: resume %d is less than %d over the floor %d", max, resume, DigPocketResumeMargin, floor)
		}
	}
}

func TestDigPocketCovered(t *testing.T) {
	for _, c := range []struct {
		max  int
		want bool
	}{{0, true}, {10, false}, {20, false}, {39, false}, {40, true}, {70, true}, {99, true}} {
		if got := DigPocketCovered(c.max); got != c.want {
			t.Errorf("DigPocketCovered(%d) = %v, want %v", c.max, got, c.want)
		}
	}
	// What "covered" means: the hitpoints over the floor take a bad rope's misses.
	for max := 10; max <= 99; max++ {
		if got, want := DigPocketCovered(max), max-DigPocketFloor(max) >= DigPocketCover; got != want {
			t.Errorf("maximum %d: covered %v, hitpoints over the floor %d against %d", max, got, max-DigPocketFloor(max), DigPocketCover)
		}
	}
}

func TestDigPocketJudge(t *testing.T) {
	for _, c := range []struct {
		name         string
		hp, max, eat int
		entering     bool
		want         DigPocketAdvice
	}{
		{"full", 10, 10, 0, true, DigPocketGo},
		{"full, inside a stretch", 10, 10, 0, false, DigPocketGo},
		{"at the resume line, no food", 7, 10, 0, true, DigPocketGo},
		{"under the resume line, no food, starting", 6, 10, 0, true, DigPocketRest},
		{"under the resume line, no food, inside a stretch", 6, 10, 0, false, DigPocketGo},
		{"under the resume line, food, starting", 6, 10, 20, true, DigPocketGo},
		{"one over the floor, no food", 5, 10, 0, false, DigPocketGo},
		{"at the floor, no food", 4, 10, 0, false, DigPocketRest},
		{"at the floor, food", 4, 10, 20, false, DigPocketEat},
		{"at the floor, food, starting", 4, 10, 20, true, DigPocketEat},
		{"under the floor, no food", 2, 10, 0, false, DigPocketRest},
		{"on the death line, no food", 1, 10, 0, false, DigPocketRest},
		{"read as dead", 0, 10, 0, false, DigPocketRest},
		{"a bigger account over its floor", 19, 70, 0, false, DigPocketGo},
		{"a bigger account at its floor", 18, 70, 0, false, DigPocketRest},
		{"a bigger account at its floor with food", 18, 70, 14, false, DigPocketEat},
		{"hitpoints not read yet", 0, 0, 0, true, DigPocketGo},
		{"a maximum not read yet", 5, 0, 0, false, DigPocketGo},
	} {
		if got := DigPocketJudge(c.hp, c.max, c.eat, c.entering); got != c.want {
			t.Errorf("%s: DigPocketJudge(%d of %d, food %d, entering %v) = %s, want %s", c.name, c.hp, c.max, c.eat, c.entering, got, c.want)
		}
	}
	// No matter what the pack holds, a pocket is never picked at or under the floor: the advice there is to eat or to rest.
	for max := 10; max <= 99; max++ {
		for hp := 0; hp <= DigPocketFloor(max); hp++ {
			for _, food := range []int{0, 20} {
				for _, entering := range []bool{false, true} {
					if got := DigPocketJudge(hp, max, food, entering); got == DigPocketGo {
						t.Fatalf("%d of %d (food %d, entering %v): steals at the floor %d", hp, max, food, entering, DigPocketFloor(max))
					}
				}
			}
		}
	}
}

func TestDigPocketRestFor(t *testing.T) {
	for _, c := range []struct {
		hp, max int
		want    time.Duration
	}{
		{4, 10, DigPocketRestMin},  // three to regain and one more: four minutes, lifted to the least
		{1, 10, 7 * time.Minute},   // six to regain and one more: seven minutes
		{10, 40, 11 * time.Minute}, // ten to the resume line of 20
		{5, 70, DigPocketRestMax},  // thirty to the line of 35 and one more, lowered to the most
		{0, 99, DigPocketRestMax},
		{9, 10, DigPocketRestMin}, // already over the line: the least
	} {
		if got := DigPocketRestFor(c.hp, c.max); got != c.want {
			t.Errorf("DigPocketRestFor(%d of %d) = %s, want %s", c.hp, c.max, got, c.want)
		}
	}
	// A rested account has the hitpoints back by the time the park ends (a minute each), or the cap.
	for max := 10; max <= 99; max++ {
		for hp := 0; hp <= DigPocketFloor(max); hp++ {
			rest := DigPocketRestFor(hp, max)
			if rest < DigPocketRestMin || rest > DigPocketRestMax {
				t.Fatalf("%d of %d: rest %s is outside [%s, %s]", hp, max, rest, DigPocketRestMin, DigPocketRestMax)
			}
			regained := hp + int(rest/DigPocketRegen)
			if regained < DigPocketResume(max) && rest != DigPocketRestMax {
				t.Fatalf("%d of %d: %s regains to %d, short of the resume line %d", hp, max, rest, regained, DigPocketResume(max))
			}
		}
	}
}

// The constants rest on the content: a miss costs one hitpoint and stuns for eight ticks, the hitpoint timer runs every hundred ticks, and a rope
// is one in eleven of the picks.
func TestDigPocketConstantsMatchTheContent(t *testing.T) {
	dir := os.Getenv("RSB0T_CONTENT_DIR")
	if dir == "" {
		dir = os.Getenv("RSB0T_CONTENT")
	}
	if dir == "" {
		t.Skip("RSB0T_CONTENT_DIR is not set")
	}
	read := func(rel string) string {
		b, err := os.ReadFile(dir + "/" + rel)
		if err != nil {
			t.Skipf("the content is not at %s: %v", dir, err)
		}
		return strings.ReplaceAll(string(b), "\r", "")
	}
	pocket := read("scripts/quests/quest_itexam/scripts/digsite_workman.rs2")
	i := strings.Index(pocket, "[label,pickpocket_digworkman1]")
	if i < 0 {
		t.Fatal("the workman's pickpocket label is gone")
	}
	body := pocket[i:]
	if !strings.Contains(body, "~damage_self(1);") {
		t.Error("a miss on a workman no longer costs one hitpoint")
	}
	if !strings.Contains(body, "%stunned = calc(map_clock + 8);") {
		t.Error("a miss on a workman no longer stuns for eight ticks")
	}
	if !strings.Contains(body, "def_int $rand = random(11);") || !strings.Contains(body, "} else if ($rand < 7) {\n        inv_add(inv, rope, 1);") {
		t.Error("a rope is no longer one pick in eleven")
	}
	if !regexp.MustCompile(`stat_random\(thieving, 100, 240\)`).MatchString(body) {
		t.Error("the workman's pocket no longer rolls Thieving 100 to 240")
	}
	login := read("scripts/login_logout/login.rs2")
	if !regexp.MustCompile(`settimer\(health_regen, 100\);`).MatchString(login) {
		t.Error("the hitpoint timer is no longer a hundred ticks")
	}
	damage := read("scripts/player/scripts/damage.rs2")
	if !strings.Contains(damage, "[proc,damage_self](int $amount)") || !strings.Contains(damage, "damage(uid, 1, $amount);") {
		t.Error("damage_self no longer hits the player for the amount")
	}
}

// A step that picks pockets goes in with something to eat when the hitpoints would not cover a bad rope, once a run.
func TestDecideDigsitePocketFood(t *testing.T) {
	weak := digIn{stage: DigFirstExam, hp: 10, maxHP: 10}
	got := weak.decide()
	dgKit(KitDigPocketFood).check(t, "a 10 hitpoint account with nothing to eat", got)
	if len(got.Kit) != 2 || got.Kit[0].ID != ObjShark || got.Kit[1].ID != ObjBass {
		t.Fatalf("the pocket food kit is %+v", got.Kit)
	}
	for _, l := range got.Kit {
		if !l.Optional || l.Qty != 3 || len(l.Sources) != 0 || l.Untradeable {
			t.Errorf("kit line %+v: want three, optional, from the bank or the mule only", l)
		}
	}
	if !strings.Contains(got.Reason, "10") {
		t.Errorf("the reason %q does not say the hitpoints", got.Reason)
	}
	if got.KitName == KitDigFood {
		t.Error("the pocket food kit is the altar cave's")
	}

	// Once a run, whether it filled or failed.
	weak.ran = map[string]bool{"provision:" + KitDigPocketFood: true}
	dgCustomItem(CustomDigPickpocket, DigObjSampleGreen).check(t, "after the kit ran", weak.decide())
	weak.ran, weak.failed = nil, map[string]bool{"provision:" + KitDigPocketFood: true}
	dgCustomItem(CustomDigPickpocket, DigObjSampleGreen).check(t, "after the kit failed", weak.decide())
	// The altar cave's food kit is its own: the pocket's food does not spend it.
	weak.failed, weak.ran = nil, map[string]bool{"provision:" + KitDigPocketFood: true}
	weakCave := digIn{stage: DigRemovedBlockage, levels: map[string]int{"attack": 20, "strength": 20, "defence": 20, "hitpoints": 25, "magic": 1, "prayer": 1, "ranged": 1},
		maxHP: 25, hp: 25, ran: weak.ran}
	dgKit(KitDigFood).check(t, "the altar cave's food after the pocket's", weakCave.decide())

	// Food in the pack, or hitpoints that cover a bad rope, need no kit.
	weak = digIn{stage: DigFirstExam, hp: 10, maxHP: 10, inv: map[int]int{ObjShark: 1}}
	dgCustomItem(CustomDigPickpocket, DigObjSampleGreen).check(t, "a shark in the pack", weak.decide())
	for _, max := range []int{40, 70, 99} {
		strong := digIn{stage: DigFirstExam, hp: max, maxHP: max}
		dgCustomItem(CustomDigPickpocket, DigObjSampleGreen).check(t, "a covered account", strong.decide())
	}
	for _, max := range []int{10, 20, 39} {
		short := digIn{stage: DigFirstExam, hp: max, maxHP: max}
		dgKit(KitDigPocketFood).check(t, "an account the pocket would drain", short.decide())
	}
	// An unread maximum judges nothing.
	unread := digIn{stage: DigFirstExam}.snapshot()
	unread.MaxHP, unread.HP = 0, 0
	dgCustomItem(CustomDigPickpocket, DigObjSampleGreen).check(t, "hitpoints not read", DecideDigsite(unread))
	// Only foods the quest eats count: a pack of coins and a trowel is nothing to eat.
	weak = digIn{stage: DigFirstExam, hp: 10, maxHP: 10, inv: map[int]int{DigObjTrowel: 1}}
	dgKit(KitDigPocketFood).check(t, "no food among the tools", weak.decide())

	// The brush at stage 5 is a pocket too.
	brush := digIn{stage: DigImpressExpert, hp: 12, maxHP: 12, inv: map[int]int{DigObjTrowel: 1, DigObjSpecimenJar: 1}}
	dgKit(KitDigPocketFood).check(t, "the brush's pocket", brush.decide())
	brush.inv[ObjShark] = 2
	dgCustomItem(CustomDigPickpocket, DigObjSpecimenBrush).check(t, "the brush's pocket with food", brush.decide())

	// The steps that go down a winch carry the same kit when no rope is held (a rope is a pocket's loot, and the plan cannot see whether the
	// winch's was already tied), and not when one is.
	tools := map[int]int{DigObjTrowel: 1, DigObjVial: 1, DigObjPestle: 1, DigObjTinderbox: 1}
	for _, c := range []struct {
		name   string
		stage  int
		extra  map[int]int
		custom string
	}{
		{"the west shaft", DigMineshaftPermit, nil, CustomDigWest},
		{"the blast", DigPouredCompound, map[int]int{DigObjTinderbox: 1}, CustomDigBlast},
		{"the tablet", DigRemovedBlockage, nil, CustomDigTablet},
	} {
		inv := map[int]int{}
		if c.stage == DigMineshaftPermit {
			for k, v := range tools {
				inv[k] = v
			}
		}
		for k, v := range c.extra {
			inv[k] = v
		}
		in := digIn{stage: c.stage, hp: 14, maxHP: 14, inv: inv}
		dgKit(KitDigPocketFood).check(t, c.name+" with no rope and no food", in.decide())
		inv[DigObjRope] = 1
		dgCustom(c.custom).check(t, c.name+" with a rope", in.decide())
		delete(inv, DigObjRope)
		inv[ObjShark] = 1
		dgCustom(c.custom).check(t, c.name+" with food", in.decide())
		delete(inv, ObjShark)
		covered := digIn{stage: c.stage, hp: 70, maxHP: 70, inv: inv}
		dgCustom(c.custom).check(t, c.name+" for a covered account", covered.decide())
	}

	// Underground nothing is provisioned (a bank is not there): the way out is first.
	under := digIn{stage: DigMineshaftPermit, hp: 14, maxHP: 14, at: dgWest, inv: map[int]int{DigObjVial: 1, DigObjPestle: 1, DigObjTinderbox: 1, DigObjTrowel: 1}}
	dgCustom(CustomDigWest).check(t, "in the west shaft", under.decide())
	// A step with no pocket in it is never held up for food.
	talk := digIn{stage: DigSecondExam, hp: 10, maxHP: 10}
	if got := talk.decide(); got.Kind == StepProvision && got.KitName == KitDigPocketFood {
		t.Errorf("the second exam's talks are held up for pocket food: %s", got.Describe())
	}
}

// The loop eats what the module's sustain names, so the food the kit brings must be on that list; and the kit is the pocket's own.
func TestDigPocketFoodKitIsFoodTheModuleEats(t *testing.T) {
	foods := map[int]bool{}
	for _, id := range Digsite.Sustain.FoodIDs {
		foods[id] = true
	}
	for _, l := range digPocketFoodKit() {
		if !foods[l.ID] {
			t.Errorf("the pocket food kit brings %s (%d), which the module's sustain does not eat", l.Name, l.ID)
		}
		if FoodItemHeal(l.ID) < 10 {
			t.Errorf("%s heals %d: not worth a pack slot against a miss", l.Name, FoodItemHeal(l.ID))
		}
	}
	if Digsite.Sustain.EatAt <= 0 {
		t.Error("the module's sustain never eats")
	}
	// The kit is not the altar cave's, and every kit the plan can ask for keeps the food it just fetched.
	if KitDigPocketFood == KitDigFood {
		t.Error("the pocket food kit and the altar cave's food kit share a name, so one would spend the other")
	}
	st := digIn{stage: DigFirstExam, hp: 10, maxHP: 10}.decide()
	for _, id := range digFoodIDs {
		if !slices.Contains(st.KeepIDs, id) {
			t.Errorf("the pocket food kit's bank visit does not keep food %d", id)
		}
	}
}
