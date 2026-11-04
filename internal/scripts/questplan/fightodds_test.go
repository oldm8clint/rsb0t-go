package questplan

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/cachecfg"
)

// ---- the content the tables rest on (RSB0T_CONTENT or a sibling "content" checkout, skipped without it) ----

// contentRoot finds the Content checkout the foe and food tables are pinned against.
func contentRoot(t *testing.T) string {
	t.Helper()
	cands := []string{os.Getenv("RSB0T_CONTENT"), "../../../../content", "../../../../../content"}
	for _, c := range cands {
		if c == "" {
			continue
		}
		if st, err := os.Stat(filepath.Join(c, "pack", "npc.pack")); err == nil && !st.IsDir() {
			return c
		}
	}
	t.Skip("set RSB0T_CONTENT to a Content checkout to verify the foe and food tables against it")
	return ""
}

// packNames reads a content pack file ("id=name" lines) into id -> name.
func packNames(t *testing.T, path string) map[int]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int]string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		i := strings.IndexByte(line, '=')
		if i < 0 {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSpace(line[:i]))
		if err == nil {
			out[id] = strings.TrimSpace(line[i+1:])
		}
	}
	return out
}

// npcBlocks reads every [name] block of every .npc file under scripts into name -> key -> value (a param line
// "param=attackbonus,22" becomes key "param:attackbonus" with value "22").
func npcBlocks(t *testing.T, root string) map[string]map[string]string {
	t.Helper()
	out := map[string]map[string]string{}
	err := filepath.Walk(filepath.Join(root, "scripts"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".npc") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var cur map[string]string
		for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
				cur = map[string]string{}
				out[line[1:len(line)-1]] = cur
			case cur != nil && strings.Contains(line, "="):
				k, v, _ := strings.Cut(line, "=")
				if k == "param" {
					pk, pv, _ := strings.Cut(v, ",")
					cur["param:"+pk] = pv
				} else {
					cur[k] = v
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func intOf(m map[string]string, key string, def int) int {
	if v, ok := m[key]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// Every quest foe's stats in the table are the config's: the stats that make its hits and its defence, the
// attack rate, the bonuses (the ones the earlier models left out) and the lowest of its three defence bonuses.
func TestQuestFoesMatchTheContent(t *testing.T) {
	root := contentRoot(t)
	names := packNames(t, filepath.Join(root, "pack", "npc.pack"))
	blocks := npcBlocks(t, root)
	for _, id := range QuestFoeIDs() {
		foe, _ := FoeByNPC(id)
		cfg := blocks[names[id]]
		if cfg == nil {
			t.Errorf("npc %d (%s): no [%s] block in the content", id, foe.Name, names[id])
			continue
		}
		defBonus := min(intOf(cfg, "param:stabdefence", 0), intOf(cfg, "param:slashdefence", 0), intOf(cfg, "param:crushdefence", 0))
		got := MeleeFoe{
			Attack: intOf(cfg, "attack", 1), Strength: intOf(cfg, "strength", 1), Defence: intOf(cfg, "defence", 1),
			Magic: intOf(cfg, "magic", 1), Hitpoints: intOf(cfg, "hitpoints", 1), AttackTicks: intOf(cfg, "param:attackrate", 4),
			AttackBonus: intOf(cfg, "param:attackbonus", 0), StrengthBonus: intOf(cfg, "param:strengthbonus", 0), DefenceBonus: defBonus,
		}
		want := foe
		want.Name, want.ZapMax, want.ZapOneIn = "", 0, 0
		if got != want {
			t.Errorf("npc %d (%s, [%s]): table %+v, content %+v", id, foe.Name, names[id], want, got)
		}
	}
}

// The numbers the file comments quote: the maximum hits of the arena's beasts and Sir Mordred, with their
// strength bonuses, and the General's and Bouncer's.
func TestQuestFoeMaxHits(t *testing.T) {
	for _, c := range []struct {
		foe  MeleeFoe
		want int
	}{{FoeArenaOgre, 8}, {FoeArenaScorpion, 5}, {FoeArenaBouncer, 13}, {FoeMordred, 5}, {FoeRenegadeKnight, 5}, {FoeCountDraynor, 3}, {FoeKhazardCommander, 5}} {
		if got := c.foe.MaxHit(); got != c.want {
			t.Errorf("%s: max hit %d, want %d", c.foe.Name, got, c.want)
		}
	}
	// The bonus-free formula the earlier models used says 6 for the ogre and 4 for Mordred.
	if npcMaxHit(FoeArenaOgre.Strength) != 6 || npcMaxHit(FoeMordred.Strength) != 4 {
		t.Errorf("the bare-strength maxima moved: ogre %d, Mordred %d", npcMaxHit(FoeArenaOgre.Strength), npcMaxHit(FoeMordred.Strength))
	}
}

// The ids and names of the foes and the foods are the cache's (RSB0T_CONFIG_JAG): a foe the table keys by an id
// that is another npc in this build would be priced as a different monster.
func TestFoeAndFoodIDsAreTheCaches(t *testing.T) {
	cp := os.Getenv("RSB0T_CONFIG_JAG")
	if cp == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	for id, name := range map[int]string{NpcArenaOgre: "Khazard Ogre", NpcArenaScorpion: "Khazard Scorpion", NpcArenaBouncer: "Bouncer",
		NpcGeneralKhazard: "General Khazard", NpcMordred: "Sir Mordred", NpcRenegadeKnight: "Renegade Knight", NpcCountDraynor: "Count Draynor",
		NpcTempleGuardian: "Temple guardian", NpcJailer: "Jailer", NpcGhastShown: "Ghast", NpcGiantBat: "Giant bat",
		NpcKhazardCommander: "Khazard commander"} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != name {
			t.Errorf("npc %d: the cache has %q, the table means %q", id, n.Name, name)
		}
		if foe, ok := FoeByNPC(id); !ok || foe.Name != name {
			t.Errorf("FoeByNPC(%d) = %q, want %q", id, foe.Name, name)
		}
	}
	// The beasts are size 2, which is why a lure walks and the stand is no corner.
	for _, id := range []int{NpcArenaOgre, NpcArenaScorpion, NpcArenaBouncer} {
		if n, _ := cfg.Npc(id); n.Size != 2 {
			t.Errorf("npc %d is size %d, want 2", id, n.Size)
		}
	}
	for id, name := range map[int]string{385: "Shark", 373: "Swordfish", 379: "Lobster", 365: "Bass", 361: "Tuna", 329: "Salmon", 333: "Trout",
		1891: "Cake", 1893: "2/3 cake", 1895: "Slice of cake", 1897: "Chocolate cake", 1899: "2/3 chocolate cake", 1901: "Chocolate slice",
		2309: "Bread", 2142: "Cooked meat"} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != name {
			t.Errorf("obj %d: the cache has %q, the table means %q", id, o.Name, name)
		}
		if FoodBiteHeal(id) == 0 {
			t.Errorf("obj %d (%s) is not in the food table", id, name)
		}
	}
}

// The bite sizes and the number of bites of each food are content's: consume_normal.dbrow's heal_N rows, and the
// cakes' three bites (cake, partial cake, slice).
func TestFoodTableMatchesTheContent(t *testing.T) {
	root := contentRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "scripts", "player", "configs", "consumption", "consume_normal.dbrow"))
	if err != nil {
		t.Skip("no consume_normal.dbrow in this checkout")
	}
	objs := packNames(t, filepath.Join(root, "pack", "obj.pack"))
	byName := map[string]int{}
	for id, n := range objs {
		byName[n] = id
	}
	heal := map[string]int{} // object name -> bite
	var cur int
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "[heal_"):
			cur, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(line, "[heal_"), "]"))
		case strings.HasPrefix(line, "data=consumable,"):
			heal[strings.TrimPrefix(line, "data=consumable,")] = cur
		}
	}
	for id := range foodTable {
		name := objs[id]
		if name == "" {
			t.Errorf("food %d has no name in obj.pack", id)
			continue
		}
		if got := heal[name]; got != foodTable[id].Bite {
			t.Errorf("%s (%d): table bite %d, content %d", name, id, foodTable[id].Bite, got)
		}
	}
	// A cake is three bites, each leaving the next form behind.
	if foodTable[byName["cake"]].Bites != 3 || foodTable[byName["partial_cake"]].Bites != 2 || foodTable[byName["cake_slice"]].Bites != 1 {
		t.Errorf("the cake's bites are %+v %+v %+v", foodTable[byName["cake"]], foodTable[byName["partial_cake"]], foodTable[byName["cake_slice"]])
	}
}

// ---- food, the eat line and the level to start at ----

func TestFoodBitesAndPool(t *testing.T) {
	inv := map[int]int{385: 2, 1891: 2, 1893: 1, 1901: 1, 995: 500, 554: 99}
	foods := []int{385, 1891, 1893, 1901}
	if got := FoodPoolHeal(inv, foods); got != 2*20+2*12+8+5 {
		t.Errorf("pool %d, want %d", got, 2*20+2*12+8+5)
	}
	// Coins and runes are not food even when the pool counts everything the table knows.
	if got := FoodPoolHeal(inv, nil); got != 2*20+2*12+8+5 {
		t.Errorf("pool of everything %d", got)
	}
	// Two sharks, then the order of the list: two cakes of three bites, a 2/3 cake of two, a chocolate slice of one.
	want := []int{20, 20, 4, 4, 4, 4, 4, 4, 4, 4, 5}
	got := FoodBites(inv, foods)
	if len(got) != len(want) {
		t.Fatalf("bites %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bites %v, want %v", got, want)
		}
	}
}

// The eat line is twice the foe's hardest hit, never under the module's own, never over three quarters of the
// hitpoints, and 0 stays 0.
func TestFoeEatLine(t *testing.T) {
	for _, c := range []struct {
		name              string
		base, peak, maxHP int
		want              int
	}{
		{"the ogre (8) on 53 hitpoints, the arena's 14", 14, 8, 53, 16},
		{"the scorpion (5): the module's 14 stays", 14, 5, 53, 14},
		{"Bouncer (13) on 53 hitpoints", 14, 13, 53, 26},
		{"Mordred (5) on 53: the module's 12 stays", 12, 5, 53, 12},
		{"never over three quarters: a foe of 30 on 40 hitpoints", 14, 30, 40, 30},
		{"unless the module's own line already is", 35, 30, 40, 35},
		{"hitpoints unread", 14, 8, 0, 16},
		{"a module that never eats never eats", 0, 13, 53, 0},
	} {
		if got := FoeEatLine(c.base, c.peak, c.maxHP); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
	s := Sustain{FoodIDs: []int{1891}, EatAt: 14}.VsFoe(FoeArenaOgre, 53)
	if s.EatAt != 16 || len(s.FoodIDs) != 1 {
		t.Errorf("VsFoe: %+v", s)
	}
}

// The starting level: four of the foe's hardest hits and four fifths of the hitpoints, whichever is more.
func TestFightStartHP(t *testing.T) {
	for _, c := range []struct{ maxHP, peak, want int }{
		{53, 8, 42}, {53, 5, 42}, {53, 13, 52}, {53, 20, 53}, {28, 13, 28}, {0, 8, 0},
	} {
		if got := FightStartHP(c.maxHP, c.peak); got != c.want {
			t.Errorf("FightStartHP(%d, %d) = %d, want %d", c.maxHP, c.peak, got, c.want)
		}
	}
}

// ---- the simulation ----

// plan builds a snapshot of an account the way the live logs describe them: hitpoints, levels and a pack.
func oddsSnap(stage, magic, defence, attack, strength, maxHP int, inv map[int]int) Snapshot {
	return Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: inv, WornIDs: map[int]int{},
		HP: maxHP, MaxHP: maxHP, Levels: map[string]int{"magic": magic, "defence": defence, "attack": attack, "strength": strength}}
}

// Fight Arena, 16 Oct 2026 01:09-01:13: 53 hitpoints, Magic 55, 22 pieces of cake and runes for Fire Strike at
// stage 6. It beat the ogre (53 down to 14), was at 19 coming out of the cell, beat the scorpion, and died to
// Bouncer with every cake still in the pack. The gate prices the chain from the first beast and refuses it at
// the last: the simulation has the account dying to Bouncer with 38 of 66 bites eaten.
func TestArenaChainOfTheCakeAccount(t *testing.T) {
	s := oddsSnap(6, 55, 30, 1, 1, 53, map[int]int{arenaMindRune: 200, arenaFireRune: 224, arenaAirRune: 400, 1891: 22})
	odds := ArenaOdds(s)
	if odds.OK {
		t.Fatal("the chain is priced winnable with 22 cakes against Bouncer")
	}
	if len(odds.Sims) != 3 {
		t.Fatalf("simulated %d fights, want 3 (the ogre and the scorpion won, Bouncer lost): %+v", len(odds.Sims), odds.Sims)
	}
	for i, want := range []bool{true, true, false} {
		if odds.Sims[i].Won != want {
			t.Errorf("fight %d won=%v, want %v: %+v", i, odds.Sims[i].Won, want, odds.Sims[i])
		}
	}
	if !strings.Contains(odds.Why, "Bouncer") || !strings.Contains(odds.Why, "dies") {
		t.Errorf("the reason does not name Bouncer: %q", odds.Why)
	}
	// The ogre takes tens of hitpoints off it, as the log saw (53 down to 14 with two bites), not nothing.
	if lost := 53 - odds.Sims[0].MinHP; lost < 20 || lost > 45 {
		t.Errorf("the ogre fight costs %.0f hitpoints at the lowest, the log saw 39", lost)
	}
}

// Fight Arena, 12 Oct 2026: an account at 41 hitpoints cast Wind Bolts at Bouncer every three seconds, ate twelve
// sharks in ninety seconds and died with one left. The simulation reproduces the pace (the model's Bouncer takes
// two hitpoints a tick, the log's took 1.9) and the verdict.
func TestArenaBouncerOfTheTwelfthOfOctober(t *testing.T) {
	s := oddsSnap(10, 20, 1, 1, 1, 41, map[int]int{arenaChaosRune: 100, arenaAirRune: 300, 385: 13})
	odds := ArenaOdds(s)
	if odds.OK || len(odds.Sims) != 1 || odds.Sims[0].Won {
		t.Fatalf("Bouncer from 41 hitpoints with 13 sharks and Wind Bolts is priced a win: %+v", odds)
	}
	r := odds.Sims[0]
	perTick := FoePerAttack(FoeArenaBouncer, MeleeFighter{Defence: 1}) * FightIncomingScale / float64(FoeArenaBouncer.AttackTicks)
	if perTick < 1.5 || perTick > 2.4 {
		t.Errorf("the model's Bouncer takes %.2f hitpoints a tick, the log's took 1.9", perTick)
	}
	if r.BitesUsed < 9 {
		t.Errorf("the account ate %d sharks, the log's ate 12", r.BitesUsed)
	}
}

// A pack that wins the chain is let in: Fire Blast at Magic 66 and a dozen sharks.
func TestArenaChainOfAStrongPack(t *testing.T) {
	s := oddsSnap(5, 66, 30, 1, 1, 60, map[int]int{arenaDeathRune: 60, arenaFireRune: 300, arenaAirRune: 240, 385: 12})
	if odds := ArenaOdds(s); !odds.OK {
		t.Fatalf("a Fire Blast pack with 12 sharks is refused: %s", odds.Why)
	}
	// ...and its Bouncer is lost without the sharks, whatever the spell.
	s.InvIDs = map[int]int{arenaDeathRune: 60, arenaFireRune: 300, arenaAirRune: 240, 1891: 6}
	if odds := ArenaOdds(s); odds.OK {
		t.Fatal("the same runes with six cakes are let in")
	}
}

// The chain is priced from the stage: at stage 10 only Bouncer is left, at 11 nothing.
func TestArenaBeastsFromTheStage(t *testing.T) {
	for stage, want := range map[int]int{1: 3, 5: 3, 6: 3, 8: 2, 9: 2, 10: 1, 11: 0, 12: 0} {
		if got := len(ArenaBeastsFrom(stage)); got != want {
			t.Errorf("stage %d: %d beasts, want %d", stage, got, want)
		}
	}
	if !ArenaOdds(oddsSnap(11, 55, 30, 1, 1, 53, nil)).OK {
		t.Error("stage 11 has nothing left to price")
	}
}

// An unread hitpoints maximum or Magic level is not judged, as every other gate here treats an unread stat.
func TestOddsDoNotJudgeWhatIsNotRead(t *testing.T) {
	s := oddsSnap(5, 55, 30, 1, 1, 53, map[int]int{385: 1})
	s.MaxHP = 0
	if !ArenaOdds(s).OK {
		t.Error("judged an account whose hitpoints are not read")
	}
	s = oddsSnap(5, 0, 30, 1, 1, 53, map[int]int{385: 1})
	if !ArenaOdds(s).OK {
		t.Error("judged an account whose Magic is not read")
	}
}

// The cast plan takes the highest spell the stock pays for before each cast and drops to a lower one when its
// runes are gone, as BestCastableSpell does: one chaos rune is one Fire Bolt and then Fire Strike's runes cast.
func TestCastPlanFallsDownTheSpellList(t *testing.T) {
	plan := CastPlan(map[int]int{arenaChaosRune: 1, arenaAirRune: 20, arenaFireRune: 40, arenaMindRune: 5}, nil, 55, 400)
	if len(plan) < 2 || plan[0].Name != "Fire Bolt" || plan[0].Actions != 1 {
		t.Fatalf("plan %+v", plan)
	}
	if plan[1].Name != "Fire Strike" {
		t.Errorf("the second spell is %s, want Fire Strike", plan[1].Name)
	}
	if got := SpellsOf(plan); !strings.HasPrefix(got, "1 Fire Bolt, then ") {
		t.Errorf("SpellsOf = %q", got)
	}
	// A worn staff of air pays the air, so Wind Strike needs only the mind runes.
	staff := CastPlan(map[int]int{arenaMindRune: 3}, map[int]int{1381: 1}, 5, 400)
	if len(staff) != 1 || staff[0].Name != "Wind Strike" || staff[0].Actions != 3 {
		t.Errorf("staff plan %+v", staff)
	}
	// The stock is not changed.
	stock := map[int]int{arenaMindRune: 3, arenaAirRune: 3}
	CastPlan(stock, nil, 5, 400)
	if stock[arenaMindRune] != 3 {
		t.Error("CastPlan spent the caller's stock")
	}
}

// SimulateFight on the numbers a reader can check by hand: a foe of 10 hitpoints that never hits, and one that
// hits for exactly what the account heals.
func TestSimulateFightArithmetic(t *testing.T) {
	harmless := MeleeFoe{Name: "post", Attack: 1, Strength: 1, Defence: 1, Hitpoints: 10, AttackTicks: 4}
	res := SimulateFight(FightSimInput{Foe: harmless, Fighter: MeleeFighter{Defence: 99}, HP: 20, MaxHP: 20,
		Phases: []FightPhase{{Name: "poke", PerAction: 2, ActionTicks: 4}}, Scale: 0.0001})
	// Acts on ticks 1, 5, 9, 13, 17: the fifth action takes the tenth hitpoint.
	if !res.Won || res.Ticks != 17 || res.Done[0] != 5 {
		t.Errorf("harmless foe: %+v", res)
	}
	// With nothing to attack with the fight runs out of the account's phases.
	res = SimulateFight(FightSimInput{Foe: harmless, Fighter: MeleeFighter{Defence: 99}, HP: 20, MaxHP: 20,
		Phases: []FightPhase{{Name: "one poke", PerAction: 2, ActionTicks: 4, Actions: 1}}, Scale: 0.0001, MaxTicks: 50})
	if res.Won || res.How != "has nothing left to attack with" {
		t.Errorf("one poke: %+v", res)
	}
	// A foe that hits for 3 a tick kills a 20 hitpoint account in seven ticks' worth of attacks.
	killer := MeleeFoe{Name: "killer", Attack: 200, Strength: 300, Defence: 1, Hitpoints: 100, AttackTicks: 1}
	res = SimulateFight(FightSimInput{Foe: killer, Fighter: MeleeFighter{Defence: 1}, HP: 20, MaxHP: 20,
		Phases: []FightPhase{{Name: "poke", PerAction: 1, ActionTicks: 4}}})
	if res.Won || res.How != "dies" || res.Ticks > 10 {
		t.Errorf("killer: %+v", res)
	}
	// A bite is eaten at the line, heals its amount (capped at the maximum) and delays the next action by three ticks.
	biter := MeleeFoe{Name: "biter", Attack: 200, Strength: 100, Defence: 1, Hitpoints: 1000, AttackTicks: 4}
	res = SimulateFight(FightSimInput{Foe: biter, Fighter: MeleeFighter{Defence: 1}, HP: 20, MaxHP: 20, EatLine: 12, MaxTicks: 30,
		Bites: []int{20, 20, 20, 20}, Phases: []FightPhase{{Name: "poke", PerAction: 1, ActionTicks: 4}}})
	if res.BitesUsed < 1 || res.MinHP <= 0 || res.How != "runs out of time" {
		t.Errorf("did not eat at the line and live: %+v", res)
	}
}

// JudgeFight's margins: a win that dips under the foe's hardest hit, or leaves no bite, is not a win to enter on.
func TestJudgeFightMargins(t *testing.T) {
	foe := FoeArenaOgre
	if ok, why := JudgeFight(FightSimResult{Won: true, MinHP: 20, BitesUsed: 5}, foe, 20); !ok {
		t.Errorf("a clear win is refused: %s", why)
	}
	if ok, _ := JudgeFight(FightSimResult{Won: true, MinHP: 7, BitesUsed: 5}, foe, 20); ok {
		t.Error("a win that falls to 7 against a hit of 8 is accepted")
	}
	if ok, _ := JudgeFight(FightSimResult{Won: true, MinHP: 30, BitesUsed: 20}, foe, 20); ok {
		t.Error("a win that eats every bite is accepted")
	}
	if ok, _ := JudgeFight(FightSimResult{Won: false, How: "dies"}, foe, 20); ok {
		t.Error("a loss is accepted")
	}
}

// ---- Merlin's Crystal ----

// Merlin's Crystal, 16 Oct 2026 01:13-01:18: 53 hitpoints, Magic 55, the fight kit failed (Betty had no chaos
// rune and the mule ask was spent), the pack held a few chaos runes, the fire runes and the air for three Fire
// Bolts, and six cakes and a handful of chocolate slices. The caster's Attack is 1. The account cast three bolts, meleed
// for three minutes and died with none left. The gate refuses that pack, naming what the runes pay for.
func TestMerlinGateRefusesTheFailedKit(t *testing.T) {
	s := oddsSnap(3, 55, 30, 1, 1, 53, map[int]int{objChaosRune: 3, objAirRune: 9, arenaFireRune: 894, 561: 60, 1891: 6, 1901: 5})
	v := MerlinMordredReady(s)
	if v.OK {
		t.Fatal("the failed kit's pack is let upstairs")
	}
	if !strings.Contains(v.Why, "Sir Mordred") || !strings.Contains(v.Why, "3 Fire Bolt") {
		t.Errorf("the reason does not say what the runes pay for: %q", v.Why)
	}
	r := v.Odds.Sims[0]
	// The simulation has it die in about the time the log has it die (three minutes of ticks), having eaten them all.
	if r.Won || r.Ticks < 200 || r.Ticks > 400 || r.BitesUsed < 20 {
		t.Errorf("the simulated fight: %+v (the log's lasted 3m20s and ate 20 bites)", r)
	}
}

// ...and the kit that was meant to be there, sixty chaos runes and a hundred and twenty air, passes with a margin
// to spare; so does an account that can swing, with no runes at all.
func TestMerlinGateLetsTheKitAndTheFighterIn(t *testing.T) {
	kit := oddsSnap(3, 55, 30, 1, 1, 53, map[int]int{objChaosRune: 60, objAirRune: 120, 385: 10, 379: 6})
	if v := MerlinMordredReady(kit); !v.OK {
		t.Errorf("the intended kit is refused: %s", v.Why)
	}
	fighter := oddsSnap(3, 55, 30, 40, 40, 53, map[int]int{385: 10})
	if v := MerlinMordredReady(fighter); !v.OK {
		t.Errorf("an Attack 40, Strength 40 account with sharks is refused: %s", v.Why)
	}
	// Levels not read: nothing to judge.
	if v := MerlinMordredReady(Snapshot{InvIDs: map[int]int{}, WornIDs: map[int]int{}}); !v.OK {
		t.Errorf("an unread account is judged: %s", v.Why)
	}
}

// The decision: outside the keep the refused pack waits and parks; inside, it walks out; and a pack with no food
// at all keeps the older answer.
func TestMerlinDecidesByTheGate(t *testing.T) {
	s := oddsSnap(MerlinLancelot, 55, 30, 1, 1, 53, map[int]int{objChaosRune: 3, objAirRune: 9, arenaFireRune: 894, 1891: 6, 1901: 5})
	s.Ran, s.Failed = map[string]bool{}, map[string]bool{"provision:" + KitMerlinFight: true}
	st := DecideMerlinsCrystal(s)
	if st.Kind != StepWait || st.ParkFor != MerlinOddsParkFor || !strings.Contains(st.Reason, "Sir Mordred") {
		t.Fatalf("outside: %s (park %v)", st.Describe(), st.ParkFor)
	}
	if why := ValidateStep(st); why != "" {
		t.Errorf("malformed: %s", why)
	}
	s.Tile, s.TileKnown = MordredAnchor, true
	if st := DecideMerlinsCrystal(s); st.Kind != StepCustom || st.Custom != CustomMerlinLeaveKeep {
		t.Fatalf("inside: %s", st.Describe())
	}
	// The kit ran and the pack is the intended one: up the stairs.
	ok := oddsSnap(MerlinLancelot, 55, 30, 1, 1, 53, map[int]int{objChaosRune: 60, objAirRune: 120, 385: 10})
	ok.Ran, ok.Failed = map[string]bool{"provision:" + KitMerlinFight: true}, map[string]bool{}
	if st := DecideMerlinsCrystal(ok); st.Kind != StepCustom || st.Custom != CustomMerlinKeep {
		t.Errorf("the intended pack: %s", st.Describe())
	}
	// No food, kit failed: the older wait, with no park of its own.
	bare := oddsSnap(MerlinLancelot, 55, 30, 1, 1, 53, map[int]int{objChaosRune: 60, objAirRune: 120})
	bare.Ran, bare.Failed = map[string]bool{}, map[string]bool{"provision:" + KitMerlinFight: true}
	if st := DecideMerlinsCrystal(bare); st.Kind != StepWait || strings.Contains(st.Reason, "not going up") {
		t.Errorf("no food: %s", st.Describe())
	}
}

// ---- the race ----

// The Merlin trace: three minutes of bites at 8 to 12 of 53 hitpoints without a scratch on the target.
func TestRaceLostWithoutProgress(t *testing.T) {
	var r FightRace
	start := time.Unix(1_000_000, 0)
	hp := 53
	r.Observe(start, hp)
	// A quiet half minute: not enough.
	for s := 1; s <= 30; s++ {
		hp = max(hp-1, 12)
		r.Observe(start.Add(time.Duration(s)*time.Second), hp)
	}
	now := start.Add(30 * time.Second)
	rr := RaceReading{Elapsed: r.Elapsed(now), Taken: r.Taken(), FoeHealth: 38, FoeTotal: 38, HP: hp, FoodHeal: 110, PeakHit: 5}
	if lost, _ := RaceLost(rr); lost {
		t.Error("lost at 30 seconds")
	}
	// A minute and a half in, hp oscillating around 12 as the account eats and is hit: the sum keeps counting.
	for s := 31; s <= 90; s++ {
		if s%2 == 0 {
			hp += 4
		} else {
			hp -= 3
		}
		hp = max(min(hp, 53), 4)
		r.Observe(start.Add(time.Duration(s)*time.Second), hp)
	}
	now = start.Add(90 * time.Second)
	rr = RaceReading{Elapsed: r.Elapsed(now), Taken: r.Taken(), FoeHealth: 38, FoeTotal: 38, HP: hp, FoodHeal: 110, PeakHit: 5}
	lost, why := RaceLost(rr)
	if !lost || !strings.Contains(why, "has not moved") {
		t.Errorf("not lost at 90 seconds with the bar unmoved: %v %q (taken %.0f)", lost, why, r.Taken())
	}
}

// A race that is being won is not called lost: a quarter of the target gone in 30 seconds at a cost the pack covers,
// and a fight that costs little.
func TestRaceNotLostWhenWinning(t *testing.T) {
	rr := RaceReading{Elapsed: 30 * time.Second, Taken: 12, FoeHealth: 20, FoeTotal: 38, HP: 41, FoodHeal: 100, PeakHit: 5}
	if lost, why := RaceLost(rr); lost {
		t.Errorf("a winning race is lost: %s", why)
	}
	// Barely hurt, nothing taken off: nothing to say.
	rr = RaceReading{Elapsed: 5 * time.Minute, Taken: 2, FoeHealth: 38, FoeTotal: 38, HP: 53, FoodHeal: 100, PeakHit: 5}
	if lost, _ := RaceLost(rr); lost {
		t.Error("lost with two hitpoints taken")
	}
	// Under the warm-up, whatever the numbers.
	rr = RaceReading{Elapsed: 10 * time.Second, Taken: 30, FoeHealth: 38, FoeTotal: 38, HP: 5, FoodHeal: 0, PeakHit: 5}
	if lost, _ := RaceLost(rr); lost {
		t.Error("lost inside the warm-up")
	}
}

// A slow race the pack cannot pay is called lost on its pace, early: 6 of 38 taken in 40 seconds at 25 hitpoints lost
// is 200 more seconds of 0.6 a second against 28 hitpoints and food worth 40.
func TestRaceLostOnItsPace(t *testing.T) {
	rr := RaceReading{Elapsed: 40 * time.Second, Taken: 25, FoeHealth: 32, FoeTotal: 38, HP: 28, FoodHeal: 40, PeakHit: 5}
	lost, why := RaceLost(rr)
	if !lost || !strings.Contains(why, "at this pace") {
		t.Errorf("%v %q", lost, why)
	}
	// The same pace with food enough is a fight to go on with.
	rr.FoodHeal = 400
	if lost, why := RaceLost(rr); lost {
		t.Errorf("lost with 400 hitpoints of food: %s", why)
	}
}

// DecideFight retreats on the race, and is unchanged when the reading carries none.
func TestDecideFightReadsTheRace(t *testing.T) {
	base := FightReading{HP: 30, MaxHP: 53, EatAt: 14, Food: 10, SinceClick: 10 * time.Second, TargetInCombat: true}
	if v := DecideFight(base); v.Action == FightRetreat {
		t.Errorf("a plain fight retreats: %s", v.Why)
	}
	base.Race = RaceReading{Elapsed: 90 * time.Second, Taken: 80, FoeHealth: 38, FoeTotal: 38, HP: 30, FoodHeal: 80, PeakHit: 5}
	if v := DecideFight(base); v.Action != FightRetreat || !strings.Contains(v.Why, "has not moved") {
		t.Errorf("a lost race: %+v", v)
	}
}

// The Merlin account's hitpoints, read off its own log (every "ate X at A/53 hitpoints, now B" is a look at A and then at B), replayed
// through the race. Sir Mordred's bar is unknown, so both ends are tried: not scratched at all, and the fourteen hitpoints three Fire
// Bolts take off at an average roll. The race calls it lost at the first look of the melee (68 seconds after the casting began, 13
// hitpoints and food worth 72 left) and the account died 96 seconds later with none: it keeps the food for the walk out.
func TestRaceOnTheMerlinTrace(t *testing.T) {
	type look struct{ sec, before, after, heal int }
	looks := []look{
		{68, 9, 13, 4}, {71, 8, 12, 4}, {72, 12, 13, 4}, {78, 11, 15, 4}, {90, 11, 15, 4}, {104, 9, 13, 4}, {109, 8, 12, 4}, {111, 12, 9, 4},
		{112, 9, 13, 4}, {114, 8, 13, 5}, {116, 12, 17, 5}, {142, 12, 17, 5}, {147, 10, 15, 5}, {150, 12, 17, 5}, {154, 12, 17, 5},
		{157, 12, 17, 5}, {164, 12, 17, 5},
	}
	const deathSec = 207 // 01:18:20, the log's DEATH line, from the first cast at 01:14:53
	base := time.Date(2026, 10, 16, 1, 14, 53, 0, time.UTC)
	for _, progress := range []int{0, 14} {
		var r FightRace
		r.Observe(base, 53)
		pool := 0
		for _, l := range looks {
			pool += l.heal
		}
		firedAt, foodThen := -1, 0
		for _, l := range looks {
			r.Observe(base.Add(time.Duration(l.sec-1)*time.Second), l.before)
			now := base.Add(time.Duration(l.sec) * time.Second)
			r.Observe(now, l.after)
			pool -= l.heal
			rr := RaceReading{Elapsed: r.Elapsed(now), Taken: r.Taken(), HP: l.after, FoodHeal: pool, PeakHit: 5}
			if progress > 0 {
				rr.FoeHealth, rr.FoeTotal = 38-progress, 38
			}
			if lost, _ := RaceLost(rr); lost && firedAt < 0 {
				firedAt, foodThen = l.sec, pool
			}
		}
		if firedAt < 0 || firedAt > 70 {
			t.Errorf("progress %d: the race fired at %ds, want by the first look at 68s", progress, firedAt)
		}
		if deathSec-firedAt < 90 || foodThen < 60 {
			t.Errorf("progress %d: fired %ds before the death with food worth %d, want 90s and 60", progress, deathSec-firedAt, foodThen)
		}
	}
}

// ...and the Fight Arena account's ogre fight, which it WON (53 hitpoints down to 14 in 38 seconds, two bites, the ogre dead a minute
// later), is not called lost at any look: a fight that costs 39 hitpoints and is winning is a fight to go on with.
func TestRaceLetsTheOgreWinStand(t *testing.T) {
	var r FightRace
	base := time.Date(2026, 10, 16, 1, 9, 58, 0, time.UTC)
	hpAt := func(sec int) int { // 53 down to 14 by 38 s
		return max(53-sec*39/38, 14)
	}
	r.Observe(base, 53)
	for sec := 1; sec <= 38; sec++ {
		now := base.Add(time.Duration(sec) * time.Second)
		r.Observe(now, hpAt(sec))
		// The ogre's 60 hitpoints fall in step with the casts: all of them by 62 s.
		taken := min(60, sec*60/62)
		rr := RaceReading{Elapsed: r.Elapsed(now), Taken: r.Taken(), FoeHealth: 60 - taken, FoeTotal: 60, HP: hpAt(sec), FoodHeal: 22 * 12, PeakHit: 8}
		if taken == 0 {
			rr.FoeHealth, rr.FoeTotal = 0, 0 // no hitmark on it yet
		}
		if lost, why := RaceLost(rr); lost {
			t.Fatalf("the won ogre fight is called lost at %ds: %s", sec, why)
		}
	}
}

// ---- a fight the content scripts the account to lose: Koschei's fourth form (Fremennik Trials) ----

// Koschei's forms are the content's [viking_enemy1..4] at ids 1290-1293, and only the fourth is a scripted loss: 255
// hitpoints and 255 defence at an attack rate of one tick, a fight that ends when a hit of his lands on exactly 0
// inside the arena (viking_thorvald.rs2, [label,viking_honour_death]). The first three each end by restoring his
// hitpoints and turning him into the next form, which is an ordinary fight to be judged by its pace.
func TestScriptedLossIsKoscheisFourthForm(t *testing.T) {
	root := contentRoot(t)
	names := packNames(t, filepath.Join(root, "pack", "npc.pack"))
	blocks := npcBlocks(t, root)
	for i := 0; i < 4; i++ {
		if got, want := names[NpcKoscheiFirst+i], "viking_enemy"+strconv.Itoa(i+1); got != want {
			t.Errorf("npc %d is %q in the content, the table means %q", NpcKoscheiFirst+i, got, want)
		}
	}
	if NpcKoscheiFourth != NpcKoscheiFirst+3 {
		t.Errorf("the fourth form is %d, the first %d", NpcKoscheiFourth, NpcKoscheiFirst)
	}
	fourth := blocks["viking_enemy4"]
	if fourth == nil || intOf(fourth, "hitpoints", 0) != 255 || intOf(fourth, "defence", 0) != 255 || intOf(fourth, "param:attackrate", 4) != 1 {
		t.Errorf("[viking_enemy4] is not the 255-hitpoint, one-tick form: %v", fourth)
	}
	b, err := os.ReadFile(filepath.Join(root, "scripts", "quests", "quest_viking", "scripts", "viking_thorvald.rs2"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "@viking_honour_death(npc_uid)") {
		t.Error("viking_thorvald.rs2 no longer scripts the honour death the pass rests on")
	}
	for id := NpcKoscheiFirst - 10; id <= NpcKoscheiFourth+10; id++ {
		if got, want := ScriptedLoss(id), id == NpcKoscheiFourth; got != want {
			t.Errorf("ScriptedLoss(%d) = %v, want %v", id, got, want)
		}
	}
}

// The forms' names in the cache: every form of the arena's warrior reads as Koschei, so a table keyed by these ids
// means the right man.
func TestKoscheiFormsAreTheCaches(t *testing.T) {
	cp := os.Getenv("RSB0T_CONFIG_JAG")
	if cp == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	for id := NpcKoscheiFirst; id <= NpcKoscheiFourth; id++ {
		if n, ok := cfg.Npc(id); !ok || !strings.Contains(strings.ToLower(n.Name), "koschei") {
			t.Errorf("npc %d: the cache has %q, the table means Koschei", id, n.Name)
		}
	}
}

// DecideFight retreats from no scripted loss, whatever the race or the bites say; the same readings against an
// ordinary foe retreat, so it is the flag and nothing else that holds the fight. The race reading is the one a
// Thorvald fight gives after 25 seconds of the fourth form: 20 hitpoints lost, 5 of his 255 taken off, no food in the
// pack (the call passes an empty Sustain), a projected cost of about 1000 against 33.
func TestDecideFightLeavesAScriptedLossAlone(t *testing.T) {
	race := RaceReading{Elapsed: 25 * time.Second, Taken: 20, FoeHealth: 250, FoeTotal: 255, HP: 33}
	for _, c := range []struct {
		name string
		r    FightReading
	}{
		{"the race", FightReading{HP: 33, MaxHP: 53, SinceClick: 10 * time.Second, TargetInCombat: true, Race: race}},
		{"the last bite at the eat line", FightReading{HP: 10, MaxHP: 53, EatAt: 14, Food: 1, SinceClick: 10 * time.Second, TargetInCombat: true}},
		{"the stalled bites", FightReading{HP: 40, MaxHP: 53, EatAt: 14, Food: 9, EatsSinceProgress: FightStallEats, SinceClick: 10 * time.Second, TargetInCombat: true}},
	} {
		if v := DecideFight(c.r); v.Action != FightRetreat {
			t.Errorf("%s: an ordinary foe does not retreat (%+v), so the case proves nothing", c.name, v)
		}
		c.r.Scripted = true
		if v := DecideFight(c.r); v.Action == FightRetreat {
			t.Errorf("%s: a scripted loss retreats: %s", c.name, v.Why)
		}
	}
	// The attack is still sent again when it has stopped: the flag silences the retreat rules and nothing else.
	idle := FightReading{HP: 33, MaxHP: 53, SinceClick: FightClickAny, Scripted: true, Race: race}
	if v := DecideFight(idle); v.Action != FightReengage {
		t.Errorf("a scripted loss whose attack went unsent for %s: %+v, want a re-engage", FightClickAny, v)
	}
}
