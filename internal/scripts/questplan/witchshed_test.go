package questplan

import (
	"strings"
	"testing"
	"time"
)

// shedPack builds the snapshot of an account at the shed's gate: Magic, Defence, hitpoints, and the
// pack and worn items by id.
func shedPack(magic, defence, hp int, pack, worn map[int]int) Snapshot {
	return Snapshot{Journal: StatusInProgress, Stage: WitchUnlocked, StageKnown: true,
		InvIDs: pack, WornIDs: worn, HP: hp, MaxHP: hp,
		Levels: map[string]int{"magic": magic, "defence": defence}}
}

// The packs of the accounts that died, or nearly died, in the shed (live 6 to 15 Oct 2026, the fleet log
// lines quoted per case), and of the kit as it is meant to arrive. Rune ids: 562 chaos, 556 air, 558 mind,
// 554 fire; foods 385 shark, 379 lobster.
func TestWitchShedReadyJudgesTheLivePacks(t *testing.T) {
	cases := []struct {
		name string
		s    Snapshot
		ok   bool
		why  string // a piece of the reason when not ok
	}{
		{"the kit as meant: 70 Wind Bolts, 6 sharks and 4 lobsters, Defence 20",
			shedPack(44, 20, 40, map[int]int{562: 70, 556: 140, 385: 6, 379: 4}, nil), true, ""},
		{"the same with a staff of air paying the air",
			shedPack(44, 20, 40, map[int]int{562: 70, 385: 6, 379: 4}, map[int]int{1381: 1}), true, ""},
		{"Fire Strike at Magic 13 to 16: 70 mind, 210 fire, 140 air",
			shedPack(15, 20, 40, map[int]int{558: 70, 554: 210, 556: 140, 385: 6, 379: 4}, nil), true, ""},
		{"9 Oct death: Magic 44, a staff of air, 501 mind runes, no chaos, no fire: Wind Strike, 142 casts, dead",
			shedPack(44, 20, 30, map[int]int{558: 501, 385: 2, 379: 3}, map[int]int{1381: 1}), false, "Wind Strike"},
		{"even with the food for it, Wind Strike is too slow",
			shedPack(44, 20, 40, map[int]int{558: 501, 385: 20}, map[int]int{1381: 1}), false, "casts to finish"},
		{"10 Oct death: 43 chaos runes, a staff, and no food at all, dead at 14/35",
			shedPack(54, 20, 35, map[int]int{562: 43}, map[int]int{1381: 1}), false, "runes"},
		{"11 Oct death: 4 chaos runes of 70, no food, dead at 9/38",
			shedPack(54, 20, 38, map[int]int{562: 4}, map[int]int{1381: 1}), false, "runes"},
		{"9 Oct near-miss: 11 casts of runes, out of spells with a form on it",
			shedPack(32, 15, 31, map[int]int{562: 11, 556: 22, 385: 4}, nil), false, "runes"},
		{"a thin kit: 38 chaos runes are 131 hitpoints for the forms' 144",
			shedPack(44, 20, 40, map[int]int{562: 38, 385: 6, 379: 4}, map[int]int{1381: 1}), false, "runes"},
		{"59 chaos runes of 70 (the kit short by 11) are still enough",
			shedPack(44, 20, 40, map[int]int{562: 59, 385: 6, 379: 4}, map[int]int{1381: 1}), true, ""},
		{"three sharks are not enough at 31 hitpoints and Defence 1",
			shedPack(44, 1, 31, map[int]int{562: 70, 385: 3}, map[int]int{1381: 1}), false, "food worth"},
		{"six sharks are, at the same stats",
			shedPack(44, 1, 31, map[int]int{562: 70, 385: 6, 379: 4}, map[int]int{1381: 1}), true, ""},
		{"no runes at all", shedPack(44, 20, 40, map[int]int{385: 10}, nil), false, "no combat spell"},
		{"hitpoints not read yet: only the runes are judged", shedPack(44, 20, 0, map[int]int{562: 70}, map[int]int{1381: 1}), true, ""},
		{"Water Bolt out of chaos and water falls back to Wind Bolt's runes: 20 chaos, 70 mind, 210 fire, staff",
			shedPack(44, 20, 40, map[int]int{562: 20, 558: 70, 554: 210, 385: 6, 379: 4}, map[int]int{1381: 1}), true, ""},
	}
	for _, c := range cases {
		v := WitchShedReady(c.s)
		if v.OK != c.ok {
			t.Errorf("%s: ok %v (%s), want %v", c.name, v.OK, v.Why, c.ok)
			continue
		}
		if !c.ok && !strings.Contains(v.Why, c.why) {
			t.Errorf("%s: reason %q lacks %q", c.name, v.Why, c.why)
		}
		if c.ok && v.Why != "" {
			t.Errorf("%s: ready with a reason %q", c.name, v.Why)
		}
	}
}

// The cast loop plays the highest-level spell the pack still pays for, so a stronger spell with a few
// runes is used up first and the weaker one finishes.
func TestWitchSimulatePlaysTheHighestSpellFirst(t *testing.T) {
	s := shedPack(44, 20, 40, map[int]int{562: 5, 558: 70, 554: 210, 556: 140}, nil)
	f := witchSimulate(s)
	// Magic 44 casts Fire Bolt (35: a chaos, four fire, three air) while a chaos rune lasts, then Fire Strike.
	if len(f.Spells) < 2 || f.Spells[0] != "Fire Bolt" || f.Spells[1] != "Fire Strike" {
		t.Fatalf("spells %v, want Fire Bolt then Fire Strike", f.Spells)
	}
	// 5 Fire Bolts (12) are 23.1 hitpoints, the other 120.9 take 40 Fire Strikes (8) at 3.08 a cast.
	if f.ToKill < 43 || f.ToKill > 48 {
		t.Fatalf("the forms fall on cast %d, want about 45", f.ToKill)
	}
	if got := witchSimulate(shedPack(44, 20, 40, map[int]int{}, nil)); got.Casts != 0 || got.ToKill != 0 {
		t.Fatalf("an empty pack casts %d", got.Casts)
	}
}

// The loss model against the live fights: a full kit at Defence 20 costs about 66 hitpoints (so food worth
// 100 pays it with the margin), and a Wind Strike fight costs several times that, which is what killed
// the 9 Oct Wind Strike death.
func TestWitchShedLossIsWhatTheLiveFightsCost(t *testing.T) {
	bolt := WitchShedLoss(20, float64(WitchShedHitpoints)/42)
	if bolt < 55 || bolt > 80 {
		t.Errorf("Wind Bolt at Defence 20: %.0f hitpoints, want about 66", bolt)
	}
	strike := WitchShedLoss(20, float64(WitchShedHitpoints)/188)
	if strike < 4*bolt/2 {
		t.Errorf("Wind Strike costs %.0f against Wind Bolt's %.0f: a fight four and a half times longer should cost far more", strike, bolt)
	}
	if low, high := WitchShedLoss(1, 3.4), WitchShedLoss(60, 3.4); low <= high {
		t.Errorf("Defence 1 costs %.0f and Defence 60 %.0f", low, high)
	}
	if WitchShedLoss(20, 0) != 0 {
		t.Error("a fight that deals no damage was priced")
	}
}

// The retreat rules that would have saved the five accounts that died in the shed.
func TestDecideWitchShed(t *testing.T) {
	r := WitchShedReading{HP: 31, MaxHP: 31, EatAt: 12, Food: 4, Casts: 20, CanCast: true}
	cases := []struct {
		name string
		mod  func(*WitchShedReading)
		want FightAction
		why  string
	}{
		{"a fight in hand goes on", func(*WitchShedReading) {}, FightOn, ""},
		{"out of spells with a form on the account", func(r *WitchShedReading) { r.CanCast = false; r.Casts = 11 }, FightRetreat, "after 11 casts"},
		{"the last food and the hitpoints at the eat line", func(r *WitchShedReading) { r.HP, r.Food = 12, 1 }, FightRetreat, "eat line"},
		{"the last food and one maximum hit above the eat line (the 9 Oct death, 10/30 then 6/31)", func(r *WitchShedReading) { r.HP, r.Food = 17, 1 }, FightRetreat, "eat line"},
		{"no food and 14 hitpoints (10 Oct death)", func(r *WitchShedReading) { r.HP, r.MaxHP, r.Food = 14, 35, 0 }, FightRetreat, "eat line"},
		{"no food and 9 hitpoints (11 Oct death)", func(r *WitchShedReading) { r.HP, r.MaxHP, r.Food = 9, 38, 0 }, FightRetreat, "eat line"},
		{"no food but the hitpoints are high: still casting", func(r *WitchShedReading) { r.HP, r.Food = 31, 0 }, FightOn, ""},
		{"two foods and low hitpoints: eat, keep casting", func(r *WitchShedReading) { r.HP, r.Food = 10, 2 }, FightOn, ""},
		{"the forms still standing after the cap", func(r *WitchShedReading) { r.Casts = WitchShedMaxCasts }, FightRetreat, "still standing"},
		{"one cast short of the cap", func(r *WitchShedReading) { r.Casts = WitchShedMaxCasts - 1 }, FightOn, ""},
	}
	for _, c := range cases {
		x := r
		c.mod(&x)
		v := DecideWitchShed(x)
		if v.Action != c.want {
			t.Errorf("%s: action %d (%s), want %d", c.name, v.Action, v.Why, c.want)
		}
		if c.want == FightRetreat && !strings.Contains(v.Why, c.why) {
			t.Errorf("%s: reason %q lacks %q", c.name, v.Why, c.why)
		}
	}
}

// The module's caps: one death in the shed skips the quest for a day, two deaths anywhere stop it.
func TestWitchsHouseCapsDeaths(t *testing.T) {
	if WitchsHouse.MaxDeaths != 2 {
		t.Errorf("MaxDeaths %d, want 2", WitchsHouse.MaxDeaths)
	}
	c, ok := WitchsHouse.StepDeathCaps[CustomWitchShed]
	if !ok || c.Max != 1 || c.Window != WitchShedDeathWindow || c.Skip != WitchShedDeathSkip || c.Skip < 24*time.Hour {
		t.Errorf("the shed's cap is %+v (set %v)", c, ok)
	}
}

// The kit's food leaves room for the pickups: the shark and lobster lines together are ten pieces, and
// every other line is a stack or a pair of cheese.
func TestWitchKitLeavesRoomForTheQuestItems(t *testing.T) {
	s := Snapshot{Levels: map[string]int{"magic": 44}, WornIDs: map[int]int{}, InvIDs: map[int]int{}}
	slots := KitSlots(witchKit(s, 2))
	if slots+witchMinFree+5 > 28 {
		t.Errorf("the kit takes %d of %d slots, leaving fewer than the %d the pickups need (and the run staller's five)", slots, 28, witchMinFree)
	}
	for _, l := range witchKit(s, 2) {
		if (l.ID == 385 && l.Qty != witchSharks) || (l.ID == 379 && l.Qty != witchLobsters) {
			t.Errorf("food line %s asks for %d", l.Name, l.Qty)
		}
	}
}
