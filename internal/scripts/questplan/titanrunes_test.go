package questplan

import (
	"math"
	"math/rand/v2"
	"strings"
	"testing"
)

func TestTitanHitChanceAndCastDamage(t *testing.T) {
	// Magic 60: (60+9)*64 = 4416 against 10640 is 4416/21280.
	if p := TitanHitChance(60); math.Abs(p-4416.0/21280.0) > 1e-9 {
		t.Errorf("hit chance at Magic 60: %v", p)
	}
	// The live measurement the fight was designed on: a Wind Bolt (maxhit 9) averages 0.94 a cast.
	var wind, fire TitanSpell
	for _, s := range TitanSpells {
		switch s.Name {
		case "Wind Bolt":
			wind = s
		case "Fire Strike":
			fire = s
		}
	}
	if d := TitanCastDamage(60, wind); d < 0.92 || d > 0.96 {
		t.Errorf("Wind Bolt at Magic 60 is worth %v a cast", d)
	}
	if d := TitanCastDamage(60, fire); d < 0.80 || d > 0.86 {
		t.Errorf("Fire Strike at Magic 60 is worth %v a cast", d)
	}
	if TitanHitChance(0) != 0 || TitanHitChance(-5) != 0 {
		t.Error("an unread Magic level lands casts")
	}
	for m := 1; m < 120; m++ {
		if p := TitanHitChance(m); p <= 0 || p >= 1 || p < TitanHitChance(m-1) {
			t.Fatalf("hit chance at Magic %d: %v", m, p)
		}
	}
}

func kitStock(families ...string) map[int]int {
	out := map[int]int{}
	for _, l := range TitanRuneLines() {
		for _, f := range families {
			switch {
			case f == "wind" && (l.ID == ObjChaosRune || l.ID == ObjAirRune),
				f == "strike" && (l.ID == ObjMindRune || l.ID == ObjFireRune || l.ID == ObjAirRune):
				out[l.ID] = l.Qty
			}
		}
	}
	return out
}

// The fight spends one spell until its runes are spent and then the best of the rest, and the
// forecast counts it the same way: no swapping between two spells cast by cast.
func TestForecastTitanKeepsToOneSpellAtATime(t *testing.T) {
	// The kit's air is shared by the families (counted once), so the whole kit pays for one family's
	// casts; with the air of both, the two are cast in turn.
	if f := ForecastTitan(60, kitStock("wind", "strike")); f.Casts != TitanCastBudget || len(f.Parts) != 1 {
		t.Errorf("the whole kit: %d casts of %s, want %d of one spell", f.Casts, f.Describe(), TitanCastBudget)
	}
	both := kitStock("wind", "strike")
	both[ObjAirRune] *= 2
	f := ForecastTitan(60, both)
	if f.Casts <= TitanCastBudget || f.Casts > TitanMaxCasts || len(f.Parts) < 2 {
		t.Fatalf("both families: %d casts (%s)", f.Casts, f.Describe())
	}
	// The first spell is the one the stock pays the most damage with, and no spell comes back after
	// another took over: that would be the cast-by-cast swapping the fight must not do.
	first, _, _ := PickTitanSpell(60, both, TitanCastBudget)
	if f.Parts[0].Spell != first.Name {
		t.Errorf("the forecast begins with %s, the picker with %s", f.Parts[0].Spell, first.Name)
	}
	total, seenSpell := 0, map[string]bool{}
	for _, p := range f.Parts {
		if seenSpell[p.Spell] {
			t.Errorf("%s comes back after another spell took over: %s", p.Spell, f.Describe())
		}
		seenSpell[p.Spell] = true
		total += p.Casts
	}
	if total != f.Casts {
		t.Errorf("parts add up to %d casts, not %d", total, f.Casts)
	}
	if f.Damage < 0.8*float64(TitanCastBudget) {
		t.Errorf("damage %.0f for %d casts", f.Damage, f.Casts)
	}
	// The fight's own choice, cast by cast, agrees with the forecast.
	left := map[int]int{}
	for id, n := range both {
		left[id] = n
	}
	var cur TitanSpell
	var seen []string
	for i := 0; i < TitanMaxCasts; i++ {
		sp, ok := NextTitanSpell(cur, i > 0, 60, left)
		if !ok {
			break
		}
		if sp.Name != cur.Name {
			seen = append(seen, sp.Name)
		}
		cur = sp
		for _, r := range sp.Runes {
			left[r.ID] -= r.Count
		}
	}
	if want := len(f.Parts); len(seen) != want {
		t.Errorf("the fight cast %v, the forecast %s", seen, f.Describe())
	}
	for i := range seen {
		if i < len(f.Parts) && seen[i] != f.Parts[i].Spell {
			t.Errorf("the fight cast %v, the forecast %s", seen, f.Describe())
			break
		}
	}
	// A begun fight is finished with what it has, even a handful of casts; a fight does not begin on them.
	few := map[int]int{ObjChaosRune: 5, ObjAirRune: 10}
	if _, ok := NextTitanSpell(TitanSpell{}, false, 60, few); ok {
		t.Error("five chaos runes began a fight")
	}
	wb := TitanSpells[4]
	if wb.Name != "Wind Bolt" {
		t.Fatalf("spell table changed: %s", wb.Name)
	}
	if sp, ok := NextTitanSpell(wb, true, 60, few); !ok || sp.Name != "Wind Bolt" {
		t.Errorf("a begun fight dropped its last five casts: %q %v", sp.Name, ok)
	}
	// A Magic level under the spell's drops it, begun or not.
	if _, ok := NextTitanSpell(wb, true, 10, few); ok {
		t.Error("Wind Bolt cast at Magic 10")
	}
}

// The review's cases for the gate: what an account may carry into the realm and what it may not.
func TestTitanRunesEnough(t *testing.T) {
	full := func(m map[int]int, magic int) (bool, string) { return TitanRunesEnough(magic, m, 0, 0) }
	for _, c := range []struct {
		name  string
		magic int
		pack  map[int]int
		want  bool
		part  string
	}{
		{"the whole kit", 60, kitStock("wind", "strike"), true, "260 casts (Wind Bolt)"},
		{"the wind family alone", 55, kitStock("wind"), true, "Wind Bolt"},
		{"the strike family alone (no chaos anywhere)", 55, kitStock("strike"), true, "Fire Strike"},
		{"the strike family at the lowest Magic the accounts have", 55, kitStock("strike"), true, "Fire Strike"},
		// Under Magic 55 the Fire Strike family's kit is no longer enough on its own: the gate says so.
		{"the strike family at Magic 50", 50, kitStock("strike"), false, "about 185 damage"},
		// The reviewer's live banks: chaos 3 to 70, mind 500 to 1,000, and one bank at 58 mind.
		{"chaos 70 and mind 500, air and fire from the mule", 60, map[int]int{ObjChaosRune: 70, ObjMindRune: 500, ObjAirRune: 520, ObjFireRune: 780}, true, ""},
		{"58 mind and no chaos", 60, map[int]int{ObjMindRune: 58, ObjAirRune: 520, ObjFireRune: 780}, false, "only 58 casts (Fire Strike)"},
		{"58 mind and 70 chaos", 60, map[int]int{ObjMindRune: 58, ObjChaosRune: 70, ObjAirRune: 520, ObjFireRune: 780}, false, "70 Fire Bolt then 58 Fire Strike"},
		{"chaos 3", 60, map[int]int{ObjChaosRune: 3, ObjAirRune: 520}, false, "no spell"},
		// An empty mule: the bank had mind but the mule held no air or fire.
		{"mind but no air or fire (an empty mule)", 60, map[int]int{ObjMindRune: 500}, false, "no spell"},
		{"air and fire but no mind or chaos (the mule gave, the bank had none)", 60, map[int]int{ObjAirRune: 520, ObjFireRune: 780}, false, "no spell"},
		{"an empty pack", 60, map[int]int{}, false, "no spell"},
		{"a nil pack", 60, nil, false, "no spell"},
		{"Magic not read", 0, kitStock("wind", "strike"), false, "no spell"},
		// Not enough of the families' runes at a low level: Wind Strike (maxhit 2) is 400 casts of 0.2.
		{"Magic 12 with mind and air for 400 casts", 12, map[int]int{ObjMindRune: 1000, ObjAirRune: 1000}, false, "Wind Strike"},
		// Half a kit of fire runes: 260 mind but fire for 120 casts and no chaos.
		{"fire for 120 casts", 60, map[int]int{ObjMindRune: 260, ObjAirRune: 520, ObjFireRune: 360}, false, "120 Fire Strike then 140 Wind Strike"},
		{"the kit with a tenth of the fire missing", 60, map[int]int{ObjMindRune: 260, ObjAirRune: 520, ObjFireRune: 702}, true, ""},
		{"the kit with a fifth of the fire missing", 60, map[int]int{ObjMindRune: 260, ObjAirRune: 520, ObjFireRune: 624}, false, "208 Fire Strike then 52 Wind Strike"},
	} {
		ok, why := full(c.pack, c.magic)
		if ok != c.want {
			t.Errorf("%s: ok=%v (%s)", c.name, ok, why)
		}
		if why == "" {
			t.Errorf("%s: no account of the forecast", c.name)
		}
		if c.part != "" && !strings.Contains(why, c.part) {
			t.Errorf("%s: %q lacks %q", c.name, why, c.part)
		}
	}
	// The same thin stock is enough against a Titan already beaten down: a resumed fight is not turned away.
	thin := map[int]int{ObjMindRune: 60, ObjAirRune: 120, ObjFireRune: 180}
	if ok, _ := TitanRunesEnough(60, thin, 0, 0); ok {
		t.Error("60 casts of Fire Strike are enough for a Titan nobody has touched")
	}
	if ok, why := TitanRunesEnough(60, thin, 30, TitanHitpoints); !ok {
		t.Errorf("60 casts are not enough for a Titan at 30/142: %s", why)
	}
	if need := TitanDamageNeededAt(71, 142); math.Abs(need-TitanDamageNeeded/2) > 1e-9 {
		t.Errorf("need at half health: %v", need)
	}
	for _, c := range [][2]int{{0, 0}, {-1, 142}, {142, 142}, {200, 142}, {50, 0}} {
		if TitanDamageNeededAt(c[0], c[1]) != TitanDamageNeeded {
			t.Errorf("need at %v is not the whole", c)
		}
	}
}

// The kit's own quantities pay for the gate with room to spare at the lowest Magic of the cohort
// (55; the Stage 5 accounts are 55 to 66), and one mule order carries each line.
func TestTitanKitPassesItsOwnGate(t *testing.T) {
	for _, magic := range []int{55, 60, 66, 99} {
		for _, fam := range [][]string{{"wind"}, {"strike"}, {"wind", "strike"}} {
			f := ForecastTitan(magic, kitStock(fam...))
			if f.Damage < TitanDamageNeeded*1.05 {
				t.Errorf("Magic %d, %v: the kit pays %.0f damage for the %.0f needed", magic, fam, f.Damage, TitanDamageNeeded)
			}
		}
	}
	lines := TitanRuneLines()
	want := map[int]int{ObjChaosRune: 260, ObjAirRune: 520, ObjMindRune: 260, ObjFireRune: 780}
	for _, l := range lines {
		if want[l.ID] != l.Qty {
			t.Errorf("%s: %d, want %d", l.Name, l.Qty, want[l.ID])
		}
		// One mule order carries at most this many of one item (orderplan.OpenMaxPerOrder).
		if l.Qty > 1000 {
			t.Errorf("%s: %d is more than one mule order carries", l.Name, l.Qty)
		}
	}
}

// killRate casts the stock away the way the fight does against a simulation of the engine's roll
// and counts the fights that kill him: a cast lands when randominc(A) > randominc(D) (A the player's
// (magic+9)*64, D his 10640), for randominc(maxhit); he regenerates a hitpoint every 20 casts (one
// per 100 ticks at a cast every 5). The iteration count is capped hard: trials x TitanMaxCasts.
func killRate(magic int, stock map[int]int, trials int, seed uint64) float64 {
	if trials > 3000 {
		trials = 3000
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	// The spell of every cast, as the fight chooses them.
	var seq []TitanSpell
	left := map[int]int{}
	for id, n := range stock {
		left[id] = n
	}
	var cur TitanSpell
	for len(seq) < TitanMaxCasts {
		sp, ok := NextTitanSpell(cur, len(seq) > 0, magic, left)
		if !ok {
			break
		}
		cur = sp
		for _, r := range sp.Runes {
			left[r.ID] -= r.Count
		}
		seq = append(seq, sp)
	}
	a, d := (magic+9)*64, TitanDefenceRoll
	kills := 0
	for i := 0; i < trials; i++ {
		hp := TitanHitpoints
		for n, sp := range seq {
			if rng.IntN(a+1) > rng.IntN(d+1) {
				hp -= rng.IntN(sp.MaxHit + 1)
			}
			if hp <= 0 {
				kills++
				break
			}
			if (n+1)%20 == 0 && hp < TitanHitpoints {
				hp++
			}
		}
	}
	return float64(kills) / float64(trials)
}

// The review of 13 Oct 2026 simulated the engine's roll and found the first kit (180 casts of Fire
// Strike) killed 49% of the time. The gate's own minimum must do far better, and the kit better
// still. Bounded: 2,000 trials of at most 400 casts a cell.
func TestTitanGateMeansAKill(t *testing.T) {
	const magic, trials = 60, 2000
	// The old kit, for the record.
	old := map[int]int{ObjMindRune: 180, ObjFireRune: 540, ObjAirRune: 360}
	if ok, _ := TitanRunesEnough(magic, old, 0, 0); ok {
		t.Error("the first kit (180 casts of Fire Strike) passes the gate")
	}
	if r := killRate(magic, old, trials, 1); r > 0.65 {
		t.Errorf("the first kit kills %.0f%% of the time: the simulation disagrees with the review's 49%%", r*100)
	}
	// The smallest stock of each family the gate lets through.
	for _, fam := range []struct {
		name string
		mk   func(n int) map[int]int
	}{
		{"Fire Strike", func(n int) map[int]int { return map[int]int{ObjMindRune: n, ObjFireRune: 3 * n, ObjAirRune: 2 * n} }},
		{"Wind Bolt", func(n int) map[int]int { return map[int]int{ObjChaosRune: n, ObjAirRune: 2 * n} }},
	} {
		lowest := 0
		for n := TitanMinCasts; n <= TitanMaxCasts; n++ {
			if ok, _ := TitanRunesEnough(magic, fam.mk(n), 0, 0); ok {
				lowest = n
				break
			}
		}
		if lowest == 0 {
			t.Fatalf("%s: no stock passes the gate", fam.name)
		}
		atMin, under, kit := killRate(magic, fam.mk(lowest), trials, 2), killRate(magic, fam.mk(lowest-10), trials, 3), killRate(magic, fam.mk(TitanCastBudget), trials, 4)
		t.Logf("%s at Magic %d: the gate's minimum is %d casts and kills %.1f%% of fights; 10 casts under kill %.1f%%; the kit's %d casts kill %.1f%%",
			fam.name, magic, lowest, atMin*100, under*100, TitanCastBudget, kit*100)
		if atMin < 0.80 {
			t.Errorf("%s: the smallest stock the gate passes (%d casts) kills %.0f%% of the time", fam.name, lowest, atMin*100)
		}
		if under > atMin {
			t.Errorf("%s: 10 casts under the gate's minimum kill more often (%.1f%%) than the minimum (%.1f%%)", fam.name, under*100, atMin*100)
		}
		if kit < 0.90 {
			t.Errorf("%s: the kit's %d casts kill only %.0f%% of the time", fam.name, TitanCastBudget, kit*100)
		}
	}
}
