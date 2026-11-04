package runecraftplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The experience curve at the levels the plan is made of (RC 20 is where Body takes over from Air, 44
// where Nature opens), the owner's design numbers recomputed in the game's own arithmetic.
func TestXPTable(t *testing.T) {
	for level, want := range map[int]int{20: 4470, 27: 9730, 35: 22406, 44: 55649, 50: 101333, 72: 899257} {
		if got := XPForLevel(level); got != want {
			t.Errorf("XPForLevel(%d) = %d, want %d", level, got, want)
		}
		if got := LevelForXP(want); got != level {
			t.Errorf("LevelForXP(%d) = %d, want %d", want, got, level)
		}
		if got := LevelForXP(want - 1); got != level-1 {
			t.Errorf("LevelForXP(%d) = %d, want %d", want-1, got, level-1)
		}
	}
}

// Runes per essence is level / multiplier + 1: Air gives 2 from level 11 and 3 from level 22 (the design's
// first draft had 54 for RC 22; it is 81), Body 2 only from 46, Nature 2 only from 91.
func TestRunesPerEssence(t *testing.T) {
	cases := []struct {
		a      Altar
		level  int
		essen  int
		runes  int
		perEss int
	}{
		{Air, 1, 27, 27, 1}, {Air, 10, 27, 27, 1}, {Air, 11, 27, 54, 2}, {Air, 21, 27, 54, 2},
		{Air, 22, 27, 81, 3}, {Air, 32, 27, 81, 3}, {Air, 33, 27, 108, 4},
		{Body, 20, 27, 27, 1}, {Body, 45, 27, 27, 1}, {Body, 46, 27, 54, 2},
		{Nature, 44, 27, 27, 1}, {Nature, 90, 27, 27, 1}, {Nature, 91, 27, 54, 2},
	}
	for _, c := range cases {
		if got := c.a.RunesPerEssence(c.level); got != c.perEss {
			t.Errorf("%s at RC %d: %d runes an essence, want %d", c.a.Name, c.level, got, c.perEss)
		}
		if got := RunesFor(c.a, c.level, c.essen); got != c.runes {
			t.Errorf("%s at RC %d: %d essence make %d runes, want %d", c.a.Name, c.level, c.essen, got, c.runes)
		}
	}
	if RunesFor(Air, 5, -3) != 0 {
		t.Error("negative essence makes runes")
	}
}

// The craft's experience: a load of 27 Air essence is 135 points, Body 202 (202.5 floored), Nature 243.
func TestCraftXP(t *testing.T) {
	for _, c := range []struct {
		a    Altar
		n    int
		want int
	}{{Air, 27, 135}, {Body, 27, 202}, {Nature, 27, 243}, {Air, 1, 5}, {Body, 1, 7}} {
		if got := XPFor(c.a, c.n); got != c.want {
			t.Errorf("%s x%d: %d xp, want %d", c.a.Name, c.n, got, c.want)
		}
	}
	if XP10For(Body, 27) != 2025 {
		t.Errorf("tenths: %d", XP10For(Body, 27))
	}
}

// Runecraft 1 to 44 is Air 894 and Body 6,824 essence, 7,718 in all; Air alone would take 11,130 for the same
// experience (the plan stays Air to 20, then Body); 44 to 72 on Nature is 93,735 essence.
func TestLevelBudget(t *testing.T) {
	b := LevelBudget(1, 44, true)
	if len(b.Segments) != 2 || b.Of(KeyAir) != 894 || b.Of(KeyBody) != 6824 || b.Total != 7718 {
		t.Fatalf("1 to 44: %+v", b)
	}
	if b.Segments[0].FromLevel != 1 || b.Segments[0].ToLevel != 20 || b.Segments[1].FromLevel != 20 || b.Segments[1].ToLevel != 44 {
		t.Errorf("segments %+v", b.Segments)
	}
	if got := EssenceForXP(Air, XPForLevel(44)); got != 11130 {
		t.Errorf("Air alone to 44 = %d essence, want 11130", got)
	}
	if got := b.Describe(); got != "Air 894, Body 6,824" {
		t.Errorf("describe %q", got)
	}
	// From a stat partway through: the segment starts where the experience is.
	mid := BudgetFrom(XPForLevel(20)+1000, 44, true)
	if len(mid.Segments) != 1 || mid.Segments[0].Altar.Key != KeyBody {
		t.Fatalf("from RC 20 + 1000 xp: %+v", mid)
	}
	if want := EssenceForXP(Body, XPForLevel(44)-XPForLevel(20)-1000); mid.Total != want {
		t.Errorf("mid total %d, want %d", mid.Total, want)
	}
	// 843,608 xp at 9.0 a rune essence is 93,734.2: the budget rounds up.
	if got := LevelBudget(44, 72, true); got.Total != 93735 || got.Segments[0].Altar.Key != KeyNature {
		t.Errorf("44 to 72: %+v, want 93,735 essence on Nature", got)
	}
	// A free world cannot plan the Nature part at all.
	if got := LevelBudget(44, 72, false); got.Total != 0 {
		t.Errorf("44 to 72 on a free world: %+v", got)
	}
	if got := LevelBudget(44, 44, true); got.Total != 0 || len(got.Segments) != 0 {
		t.Errorf("44 to 44: %+v", got)
	}
}

func TestPickAltar(t *testing.T) {
	for _, c := range []struct {
		level int
		want  string
	}{{1, KeyAir}, {19, KeyAir}, {20, KeyBody}, {43, KeyBody}} {
		a, ok, why := PickAltar(c.level, true, nil)
		if !ok || a.Key != c.want || why != "" {
			t.Errorf("RC %d: %s %v %q, want %s", c.level, a.Key, ok, why, c.want)
		}
		// Air and Body are free-world runes.
		if a, ok, _ := PickAltar(c.level, false, nil); !ok || a.Key != c.want {
			t.Errorf("RC %d on a free world: %s %v", c.level, a.Key, ok)
		}
	}
	// Nature from 44: refused without a talisman and on a free world.
	if a, ok, _ := PickAltar(44, true, nil); !ok || a.Key != KeyNature {
		t.Errorf("RC 44 with the talisman unchecked: %s %v", a.Key, ok)
	}
	if _, ok, why := PickAltar(44, true, map[string]bool{KeyAir: true, KeyBody: true}); ok || !strings.Contains(why, "Nature talisman") {
		t.Errorf("RC 44 without the Nature talisman: %v %q", ok, why)
	}
	if a, ok, _ := PickAltar(44, true, map[string]bool{KeyNature: true}); !ok || a.Key != KeyNature {
		t.Errorf("RC 44 with the talisman: %s %v", a.Key, ok)
	}
	if _, ok, why := PickAltar(60, false, map[string]bool{KeyNature: true}); ok || !strings.Contains(why, "not a members world") {
		t.Errorf("RC 60 on a free world: %v %q", ok, why)
	}
}

// The members guard: Nature crafted on a free world does nothing (runecraft.rs2:56-61), so the plan never
// walks there; Air and Body are allowed everywhere.
func TestMembersGuard(t *testing.T) {
	if !Nature.Members || Air.Members || Body.Members {
		t.Fatalf("members flags: air %v body %v nature %v", Air.Members, Body.Members, Nature.Members)
	}
	s := Snapshot{Level: 44, XP: XPForLevel(44), Target: 99, Members: false, BankKnown: true}
	if step := DecideLevelStage(s); step.Stage != StageStuck || !strings.Contains(step.Why, "not a members world") {
		t.Errorf("RC 44 on a free world: %+v", step)
	}
	s.Level, s.XP, s.Target = 5, 0, 44
	s.BankTalisman, s.BankEssence = map[string]int{KeyAir: 1}, 300
	if step := DecideLevelStage(s); step.Stage != StageBank {
		t.Errorf("RC 5 on a free world is refused: %+v", step)
	}
	// Standing in the Nature temple on a free world with essence: out, not a craft.
	temple := Snapshot{Level: 50, Target: 99, Members: false, Where: RegionTemple, Temple: KeyNature, PackEssence: 20}
	if step := DecideLevelStage(temple); step.Stage != StagePortalOut {
		t.Errorf("Nature temple on a free world: %+v", step)
	}
	temple.Members = true
	if step := DecideLevelStage(temple); step.Stage != StageCraft {
		t.Errorf("Nature temple on a members world: %+v", step)
	}
}

func TestRegions(t *testing.T) {
	for _, c := range []struct {
		at     tile.Tile
		region Region
		key    string
	}{
		{tile.Tile{X: 2841, Z: 4830}, RegionTemple, KeyAir},
		{tile.Tile{X: 2843, Z: 4833}, RegionTemple, KeyAir},
		{tile.Tile{X: 2523, Z: 4838}, RegionTemple, KeyBody},
		{tile.Tile{X: 2400, Z: 4835}, RegionTemple, KeyNature},
		{tile.Tile{X: 2891, Z: 4830}, RegionMine, ""},
		{tile.Tile{X: 2983, Z: 3288}, RegionOutside, ""},
		{tile.Tile{X: 3253, Z: 3402}, RegionOutside, ""},
		{tile.Tile{X: 2841, Z: 4830, Level: 1}, RegionOutside, ""},
	} {
		r, a := WhereIs(c.at)
		if r != c.region || a.Key != c.key {
			t.Errorf("WhereIs(%v) = %v %q, want %v %q", c.at, r, a.Key, c.region, c.key)
		}
	}
	for _, a := range Altars() {
		if r, _ := WhereIs(a.Enter); r != RegionTemple {
			t.Errorf("%s: the arrival tile %v is not inside the temple", a.Name, a.Enter)
		}
		if r, _ := WhereIs(a.AltarAt); r != RegionTemple {
			t.Errorf("%s: the altar %v is not inside the temple", a.Name, a.AltarAt)
		}
		if r, _ := WhereIs(a.PortalAt); r != RegionTemple {
			t.Errorf("%s: the portal %v is not inside the temple", a.Name, a.PortalAt)
		}
		if r, _ := WhereIs(a.Exit); r != RegionOutside {
			t.Errorf("%s: the exit %v is inside a temple", a.Name, a.Exit)
		}
		if r, _ := WhereIs(a.Ruins); r != RegionOutside {
			t.Errorf("%s: the ruins %v are inside a temple", a.Name, a.Ruins)
		}
	}
}

func TestTableIsConsistent(t *testing.T) {
	objs, locs := map[int]string{}, map[int]string{}
	for _, a := range Altars() {
		for _, id := range []int{a.TalismanID, a.RuneID} {
			if prev, dup := objs[id]; dup {
				t.Errorf("%s and %s share object id %d", prev, a.Key, id)
			}
			objs[id] = a.Key
		}
		for _, id := range []int{a.RuinsID, a.AltarID, a.PortalID} {
			if prev, dup := locs[id]; dup {
				t.Errorf("%s and %s share loc id %d", prev, a.Key, id)
			}
			locs[id] = a.Key
		}
		if a.Level < 1 || a.XP10 < 1 {
			t.Errorf("%s: level %d xp %d", a.Name, a.Level, a.XP10)
		}
		if a.NoBank == (a.BankName != "") {
			t.Errorf("%s: NoBank %v with bank %q", a.Name, a.NoBank, a.BankName)
		}
		if got, ok := ByKey(a.Key); !ok || got.Name != a.Name {
			t.Errorf("ByKey(%q)", a.Key)
		}
		if got, ok := ByTalisman(a.TalismanID); !ok || got.Key != a.Key {
			t.Errorf("ByTalisman(%d)", a.TalismanID)
		}
	}
	if _, ok := ByKey("fire"); ok {
		t.Error("ByKey found fire")
	}
}

func TestVerifyCraft(t *testing.T) {
	// 27 Air essence: gone from the pack, +135 xp -- done, at the rate the table says.
	v := VerifyCraft(Air, 27, 0, 135)
	if !v.OK || v.ExpectedXP != 135 || v.Rate != 1 {
		t.Errorf("a good craft: %+v", v)
	}
	// The essence left and no experience came: the craft did not happen (a refusal, a drop).
	if v := VerifyCraft(Air, 27, 0, 0); v.OK {
		t.Errorf("no xp: %+v", v)
	}
	// Essence still in the pack: not done.
	if v := VerifyCraft(Air, 27, 5, 100); v.OK {
		t.Errorf("essence left: %+v", v)
	}
	// A server that rates XP differently is measured, not assumed: double xp is rate 2 and still a craft.
	if v := VerifyCraft(Body, 20, 0, 300); !v.OK || v.Rate < 1.9 || v.Rate > 2.1 {
		t.Errorf("double rate: %+v", v)
	}
	if v := VerifyCraft(Air, 0, 0, 0); v.OK {
		t.Errorf("nothing to craft: %+v", v)
	}
}

func TestThousands(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 7718: "7,718", 1234567: "1,234,567", -4470: "-4,470"} {
		if got := Thousands(n); got != want {
			t.Errorf("Thousands(%d) = %q, want %q", n, got, want)
		}
	}
}

// base is a fresh Runecraft 1 account at the mule's post that has read its bank.
func base() Snapshot {
	return Snapshot{
		Level: 1, XP: 0, Target: 44, Members: true, AtPost: true, Source: SourceAuto, Batch: 1000,
		BankKnown: true, BankTalisman: map[string]int{}, MuleTalisman: map[string]int{},
		PackTalisman: map[string]bool{}, MuleKnown: true,
	}
}

func TestDecideLevelStageTable(t *testing.T) {
	type tc struct {
		name  string
		mod   func(s *Snapshot)
		stage Stage
		key   string // the altar the step names
		in    string // a substring the reason holds
	}
	cases := []tc{
		{"a full load and the talisman, far from the ruins: walk", func(s *Snapshot) {
			s.PackEssence, s.PackTalisman[KeyAir] = 27, true
		}, StageWalkToRuins, KeyAir, "27 essence"},
		{"a full load and the talisman, at the ruins: use it", func(s *Snapshot) {
			s.PackEssence, s.PackTalisman[KeyAir], s.AtRuins = 27, true, true
		}, StageUseTalisman, KeyAir, ""},
		{"a short load that the kit shared the pack with still goes", func(s *Snapshot) {
			s.PackEssence, s.PackTalisman[KeyAir], s.BankEssence = 24, true, 900
		}, StageWalkToRuins, KeyAir, ""},
		{"a few leftover essence while the bank has more: top up", func(s *Snapshot) {
			s.PackEssence, s.PackTalisman[KeyAir], s.BankEssence = 5, true, 900
		}, StageBank, KeyAir, "draw"},
		{"the last few essence and nothing in the bank: go with them", func(s *Snapshot) {
			s.PackEssence, s.PackTalisman[KeyAir], s.BankEssence = 5, true, 0
		}, StageWalkToRuins, KeyAir, ""},
		{"RC 20 and up crafts Body", func(s *Snapshot) {
			s.Level, s.XP = 20, XPForLevel(20)
			s.PackEssence, s.PackTalisman[KeyBody] = 27, true
		}, StageWalkToRuins, KeyBody, ""},
		{"a load but the wrong talisman for the level: provision", func(s *Snapshot) {
			s.Level, s.XP = 20, XPForLevel(20)
			s.PackEssence, s.PackTalisman[KeyAir] = 27, true
			s.BankTalisman[KeyBody] = 2
			s.BankEssence = 100
		}, StageBank, KeyBody, ""},
		{"inside with essence: craft", func(s *Snapshot) {
			s.Where, s.Temple, s.PackEssence = RegionTemple, KeyAir, 27
		}, StageCraft, KeyAir, "27 essence"},
		{"inside, nothing to bind: portal", func(s *Snapshot) {
			s.Where, s.Temple = RegionTemple, KeyAir
		}, StagePortalOut, KeyAir, "no essence"},
		{"inside the Body temple at RC 19: the altar needs 20, so out", func(s *Snapshot) {
			s.Where, s.Temple, s.PackEssence, s.Level = RegionTemple, KeyBody, 27, 19
		}, StagePortalOut, KeyBody, "under the 20"},
		{"inside the Air temple at RC 25 with essence: craft it all the same", func(s *Snapshot) {
			s.Where, s.Temple, s.PackEssence, s.Level = RegionTemple, KeyAir, 27, 25
		}, StageCraft, KeyAir, ""},
		{"standing in the essence mine: the stretch carries on", func(s *Snapshot) {
			s.Where = RegionMine
		}, StageMine, "", "essence mine"},
		{"the target level is reached", func(s *Snapshot) {
			s.Level, s.XP = 44, XPForLevel(44)
		}, StageDone, "", "44"},
		{"the bank has not been read", func(s *Snapshot) { s.BankKnown = false }, StageBank, KeyAir, "not been read"},
		{"the bank holds the talisman and a load: draw", func(s *Snapshot) {
			s.BankTalisman[KeyAir], s.BankEssence = 1, 300
		}, StageBank, KeyAir, "300 essence"},
		{"the bank holds the essence, the mule the talisman: ask", func(s *Snapshot) {
			s.BankEssence = 300
			s.MuleTalisman[KeyAir], s.MuleTalisman[KeyBody] = 1541, 677
		}, StageAskMule, KeyAir, "talisman"},
		{"nothing anywhere, a miner: mine", func(s *Snapshot) {
			s.BankTalisman[KeyAir] = 1
			s.CanMine = true
		}, StageMine, KeyAir, "essence"},
		{"nothing anywhere, no miner: stuck", func(s *Snapshot) {
			s.BankTalisman[KeyAir] = 1
			s.MineWhy = "Rune Mysteries is not complete"
		}, StageStuck, KeyAir, "Rune Mysteries"},
		{"no talisman anywhere: stuck", func(s *Snapshot) {
			s.BankEssence = 300
		}, StageStuck, KeyAir, "Air talisman"},
	}
	for _, c := range cases {
		s := base()
		c.mod(&s)
		got := DecideLevelStage(s)
		if got.Stage != c.stage {
			t.Errorf("%s: stage %s (%s), want %s", c.name, got.Stage, got.Why, c.stage)
			continue
		}
		if c.key != "" && got.Altar.Key != c.key {
			t.Errorf("%s: altar %q, want %q", c.name, got.Altar.Key, c.key)
		}
		if c.in != "" && !strings.Contains(got.Why, c.in) {
			t.Errorf("%s: why %q lacks %q", c.name, got.Why, c.in)
		}
	}
}

// The source ladder: the bank, then the mule's published stock, then mining. A mule that holds essence is
// asked before the account mines; one that holds none is skipped; after one ask the mule is not asked
// again; craftRuneEssence restricts the ladder.
func TestEssenceLadder(t *testing.T) {
	mk := func(mod func(s *Snapshot)) Snapshot {
		s := base()
		s.BankTalisman[KeyAir] = 1
		s.CanMine = true
		mod(&s)
		return s
	}
	// 1. The mule has essence: ask for the plan's need or its stock, whichever is less.
	step := DecideLevelStage(mk(func(s *Snapshot) { s.MuleEssence = 3000 }))
	if step.Stage != StageAskMule || len(step.Lines) == 0 || step.Lines[0].Item != EssenceName || step.Lines[0].Qty != 3000 {
		t.Fatalf("mule 3000: %+v", step)
	}
	step = DecideLevelStage(mk(func(s *Snapshot) { s.MuleEssence = 50000 }))
	if step.Stage != StageAskMule || step.Lines[0].Qty != 7718 {
		t.Fatalf("mule 50000 asks the plan's need: %+v", step)
	}
	// Body's talisman rides the same ask when the bank lacks it and the mule has it.
	step = DecideLevelStage(mk(func(s *Snapshot) { s.MuleEssence = 3000; s.MuleTalisman[KeyBody] = 677 }))
	var body bool
	for _, l := range step.Lines {
		if l.Item == Body.Talisman && l.Qty == TalismanAsk {
			body = true
		}
	}
	if !body {
		t.Errorf("the Body talisman does not ride the ask: %+v", step.Lines)
	}
	// 2. Already asked, the mule declined: mine.
	step = DecideLevelStage(mk(func(s *Snapshot) { s.MuleEssence = 3000; s.Asked = true }))
	if step.Stage != StageMine {
		t.Fatalf("asked already: %+v", step)
	}
	// 3. The mule holds less than a load: not worth an ask, mine.
	step = DecideLevelStage(mk(func(s *Snapshot) { s.MuleEssence = 10 }))
	if step.Stage != StageMine {
		t.Fatalf("mule 10: %+v", step)
	}
	// 4. The mule is not heard: nothing to ask, mine.
	step = DecideLevelStage(mk(func(s *Snapshot) { s.MuleKnown = false; s.MuleEssence = 3000 }))
	if step.Stage != StageMine {
		t.Fatalf("mule unheard: %+v", step)
	}
	// 5. craftRuneEssence restrictions.
	for _, c := range []struct {
		src  Source
		want Stage
	}{{SourceBank, StageStuck}, {SourceMule, StageAskMule}, {SourceMine, StageMine}, {SourceAuto, StageAskMule}} {
		step = DecideLevelStage(mk(func(s *Snapshot) { s.Source = c.src; s.MuleEssence = 3000 }))
		if step.Stage != c.want {
			t.Errorf("source %s with a stocked mule: %s (%s), want %s", c.src, step.Stage, step.Why, c.want)
		}
	}
	// "mule" only, and the mule is empty: stuck, not mining.
	step = DecideLevelStage(mk(func(s *Snapshot) { s.Source = SourceMule }))
	if step.Stage != StageStuck || !strings.Contains(step.Why, "craftRuneEssence mule") {
		t.Errorf("mule-only with an empty mule: %+v", step)
	}
	// 6. Mining failed this round: stuck rather than a loop.
	step = DecideLevelStage(mk(func(s *Snapshot) { s.MineFailed = true }))
	if step.Stage != StageStuck || !strings.Contains(step.Why, "failed") {
		t.Errorf("mining failed: %+v", step)
	}
	// 7. The stretch is a batch of whole loads, no more than the plan needs.
	step = DecideLevelStage(mk(func(s *Snapshot) {}))
	if step.Goal != 1026 { // 1000 rounded up to whole loads of 27
		t.Errorf("batch goal %d, want 1026", step.Goal)
	}
	step = DecideLevelStage(mk(func(s *Snapshot) {
		s.Level, s.XP, s.Target = 43, XPForLevel(44)-100, 44
		s.BankTalisman[KeyBody] = 1
	}))
	if step.Stage != StageMine || step.Goal != LoadSize {
		t.Errorf("a stretch for the last 100 xp is one load: %+v", step)
	}
}

// The teleport kit: wanted only when travel teleports are on; asked for from the mule when the bank's is short,
// at the post, once; drawn only before a walk long enough to save.
func TestTeleportKit(t *testing.T) {
	s := base()
	s.BankTalisman[KeyAir], s.BankEssence = 1, 300
	s.KitWanted = true
	s.MuleKit = Kit{Law: 28403, Water: 500, Air: 683500}
	step := DecideLevelStage(s)
	if step.Stage != StageAskMule {
		t.Fatalf("short kit at the post: %+v", step)
	}
	want := map[string]int{RuneLaw: KitTarget.Law, RuneWater: KitTarget.Water, RuneAir: KitTarget.Air}
	for _, l := range step.Lines {
		if want[l.Item] != l.Qty {
			t.Errorf("kit line %+v, want %d", l, want[l.Item])
		}
		delete(want, l.Item)
	}
	if len(want) != 0 {
		t.Errorf("kit lines missing %v", want)
	}
	// Asked already: draw.
	s.Asked = true
	if step := DecideLevelStage(s); step.Stage != StageBank {
		t.Errorf("asked: %+v", step)
	}
	// Not at the post: no walk to the post for a kit alone.
	s.Asked, s.AtPost = false, false
	if step := DecideLevelStage(s); step.Stage != StageBank {
		t.Errorf("away from the post: %+v", step)
	}
	// The mule has none of it: draw.
	s.AtPost, s.MuleKit = true, Kit{}
	if step := DecideLevelStage(s); step.Stage != StageBank {
		t.Errorf("mule without runes: %+v", step)
	}
	// A bank that holds the kit needs no ask.
	s.MuleKit, s.BankKit = Kit{Law: 100, Water: 100, Air: 1000}, Kit{Law: 10, Water: 5, Air: 40}
	if step := DecideLevelStage(s); step.Stage != StageBank {
		t.Errorf("kit in the bank: %+v", step)
	}
	// Kit off: never asked.
	s.BankKit, s.KitWanted = Kit{}, false
	if step := DecideLevelStage(s); step.Stage != StageBank {
		t.Errorf("kit off: %+v", step)
	}
	// Drawn when the walk saves, and the bank has one draw.
	for _, c := range []struct {
		wanted bool
		tiles  int
		bank   Kit
		want   bool
	}{
		{true, 205, Kit{Law: 2, Water: 1, Air: 8}, true},
		{true, 80, Kit{Law: 2, Water: 1, Air: 8}, false},
		{false, 400, Kit{Law: 2, Water: 1, Air: 8}, false},
		{true, 400, Kit{Law: 2, Water: 0, Air: 8}, false},
		{true, 400, Kit{Law: 0, Water: 1, Air: 8}, false},
	} {
		if got := KitNeeded(c.wanted, c.tiles, 150, c.bank); got != c.want {
			t.Errorf("KitNeeded(%+v) = %v, want %v", c, got, c.want)
		}
	}
	if KitDraw.Slots() != 3 || (Kit{Law: 1}).Slots() != 1 {
		t.Error("kit slots")
	}
	if got := KitStock(func(item string) int { return map[string]int{RuneLaw: 3, RuneAir: 9}[item] }); got != (Kit{Law: 3, Air: 9}) {
		t.Errorf("KitStock %+v", got)
	}
}

// The step never asks for what the mule does not publish.
func TestAskNeverAsksForWhatIsNotPublished(t *testing.T) {
	s := base()
	s.BankEssence = 300
	s.MuleKnown = false
	s.MuleTalisman[KeyAir] = 99
	if step := DecideLevelStage(s); step.Stage != StageStuck {
		t.Errorf("mule unheard, talisman missing: %+v", step)
	}
	s.MuleKnown = true
	s.MuleTalisman = map[string]int{KeyAir: 1}
	step := DecideLevelStage(s)
	if step.Stage != StageAskMule || len(step.Lines) == 0 || step.Lines[0].Qty != 1 {
		t.Errorf("one talisman published: %+v", step)
	}
}

// A restart finds the account anywhere: the decision is a function of the live state alone.
func TestResumeFromAnywhere(t *testing.T) {
	// On the road to the ruins with a load (the bank has been read before the restart): carry on.
	s := base()
	s.PackEssence, s.PackTalisman[KeyAir], s.AtPost = 27, true, false
	if step := DecideLevelStage(s); step.Stage != StageWalkToRuins {
		t.Errorf("on the road: %+v", step)
	}
	// A restart: the bank is unknown and the pack holds the load but not the talisman.
	s = base()
	s.BankKnown, s.PackEssence = false, 27
	if step := DecideLevelStage(s); step.Stage != StageBank {
		t.Errorf("restart without the talisman: %+v", step)
	}
	// Inside the temple after a restart: craft what the pack holds, then out.
	s = base()
	s.Where, s.Temple, s.BankKnown, s.PackEssence = RegionTemple, KeyBody, false, 27
	s.Level = 30
	if step := DecideLevelStage(s); step.Stage != StageCraft || step.Altar.Key != KeyBody {
		t.Errorf("restart in the Body temple: %+v", step)
	}
	s.PackEssence = 0
	if step := DecideLevelStage(s); step.Stage != StagePortalOut {
		t.Errorf("restart in the Body temple, empty: %+v", step)
	}
}

// The bounded market lot is the LAST rung of the ladder: after the bank, the mule and mining, only on "auto", only when
// a lot is allowed, and not again once one has failed this round.
func TestEssenceLadderMarketLotIsTheLastRung(t *testing.T) {
	mk := func(mod func(s *Snapshot)) Snapshot {
		s := base()
		s.BankTalisman[KeyAir] = 1
		s.CanBuy, s.BuyQty = true, 2000
		mod(&s)
		return s
	}
	// Nothing can mine: the lot.
	step := DecideLevelStage(mk(func(s *Snapshot) {}))
	if step.Stage != StageBuy || step.Goal != 2000 || !strings.Contains(step.Why, "market lot") {
		t.Fatalf("no miner: %+v", step)
	}
	// Mining possible: mining first, the lot not before.
	step = DecideLevelStage(mk(func(s *Snapshot) { s.CanMine = true }))
	if step.Stage != StageMine {
		t.Fatalf("a miner is asked before the market: %+v", step)
	}
	// Mining failed this round: the lot.
	step = DecideLevelStage(mk(func(s *Snapshot) { s.CanMine, s.MineFailed = true, true }))
	if step.Stage != StageBuy {
		t.Fatalf("mining failed: %+v", step)
	}
	// The mule's essence comes before both.
	step = DecideLevelStage(mk(func(s *Snapshot) { s.MuleEssence = 3000 }))
	if step.Stage != StageAskMule {
		t.Fatalf("the mule first: %+v", step)
	}
	// Only the ladder buys: a named source never does.
	for _, src := range []Source{SourceBank, SourceMule, SourceMine} {
		step = DecideLevelStage(mk(func(s *Snapshot) { s.Source = src }))
		if step.Stage != StageStuck {
			t.Errorf("source %s buys: %+v", src, step)
		}
	}
	// A lot that failed is not tried again this round; a lot under a load is not worth the walk; no lot allowed is stuck.
	step = DecideLevelStage(mk(func(s *Snapshot) { s.BuyFailed = true }))
	if step.Stage != StageStuck || !strings.Contains(step.Why, "market lot failed") {
		t.Errorf("failed lot: %+v", step)
	}
	step = DecideLevelStage(mk(func(s *Snapshot) { s.BuyQty = MinGo - 1 }))
	if step.Stage != StageStuck || !strings.Contains(step.Why, "too small") {
		t.Errorf("tiny lot: %+v", step)
	}
	step = DecideLevelStage(mk(func(s *Snapshot) {
		s.CanBuy, s.BuyWhy = false, "the mule holds 773,226 coins, not above its 1,000,000 floor"
	}))
	if step.Stage != StageStuck || !strings.Contains(step.Why, "market lot: the mule holds 773,226") {
		t.Errorf("no lot allowed says why: %+v", step)
	}
	// The essence the lot brings is banked: the next decision is a trip, not another lot.
	step = DecideLevelStage(mk(func(s *Snapshot) { s.BankEssence = 2000 }))
	if step.Stage != StageBank {
		t.Errorf("with 2,000 banked: %+v", step)
	}
}
