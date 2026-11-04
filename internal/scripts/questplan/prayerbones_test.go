package questplan

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The mule and the accounts' banks as the fleet's /api/bank-all read on 16 Oct 2026 (07:56): the mule held 17,154 Bones and
// 1,488 Big bones, no Dragon bones; fifteen accounts held 233 Bones between them.
var liveMule = map[int]int{ObjBones: 17154, ObjBigBones: 1488}

func boneState(mut func(*BonesState)) BonesState {
	s := BonesState{Need: 12_000, MuleOK: true, MuleKnown: true, Mule: liveMule, BankIs: BankHint, Left: 20 * time.Minute}
	if mut != nil {
		mut(&s)
	}
	return s
}

func TestBoneKindsAreWorthWhatTheContentSays(t *testing.T) {
	want := map[int]int{ObjBones: 45, ObjBigBones: 150, ObjDragonBones: 720}
	for _, k := range BoneKinds {
		if k.XP10 != want[k.ID] {
			t.Errorf("%s is worth %d tenths of an xp, bones.obj says %d", k.Name, k.XP10, want[k.ID])
		}
	}
	// Best first: the order is the order bones are taken in.
	for i := 1; i < len(BoneKinds); i++ {
		if BoneKinds[i].XP10 >= BoneKinds[i-1].XP10 {
			t.Errorf("%s comes after %s but is worth no less", BoneKinds[i].Name, BoneKinds[i-1].Name)
		}
	}
	if k, ok := BoneKindOf(ObjBigBones); !ok || k.Name != "Big bones" {
		t.Errorf("BoneKindOf(%d) = %+v %v", ObjBigBones, k, ok)
	}
	if _, ok := BoneKindOf(530); ok {
		t.Error("Bat bones (25 in all the banks on 16 Oct) are not a bone kind")
	}
}

// With RSB0T_CONTENT_DIR set, the table is checked against the content's own files: the experience of each bone, and that no
// shop in content 289 sells a bone (the owner's "buy bones from a shop if content has one": it has none).
func TestBoneExperienceAndShopsAgainstTheContent(t *testing.T) {
	dir := os.Getenv("RSB0T_CONTENT_DIR")
	if dir == "" {
		t.Skip("RSB0T_CONTENT_DIR not set")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "scripts", "skill_prayer", "configs", "bones.obj"))
	if err != nil {
		t.Fatal(err)
	}
	section, exp := "", map[string]int{}
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
		} else if v, ok := strings.CutPrefix(line, "param=bone_exp,"); ok {
			exp[section], _ = strconv.Atoi(v)
		}
	}
	for section, kind := range map[string]int{"bones": ObjBones, "big_bones": ObjBigBones, "dragon_bones": ObjDragonBones} {
		k, _ := BoneKindOf(kind)
		if exp[section] != k.XP10 {
			t.Errorf("%s: bones.obj [%s] bone_exp is %d, the table says %d", k.Name, section, exp[section], k.XP10)
		}
	}
	stock := regexp.MustCompile(`(?m)^stock\d+=([a-z0-9_]*bones[a-z0-9_]*),`)
	err = filepath.WalkDir(dir, func(path string, de os.DirEntry, err error) error {
		if err != nil || de.IsDir() || !strings.HasSuffix(path, ".inv") {
			return err
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if m := stock.FindSubmatch(b); m != nil {
			t.Errorf("%s stocks %s: a shop sells bones, so the Prayer rungs should buy them (see PrayerRungAsksMule)", path, m[1])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrayerNeedIsTheXPToTheLevel(t *testing.T) {
	// Levels are XP thresholds: 14,833 for 31, 27,473 for 37 and 50,339 for 43 (the OSRS curve, content 289's too).
	for _, c := range []struct{ xp, level, want int }{
		{0, 31, 14833}, {14000, 31, 833}, {14833, 31, 0}, {20000, 31, 0}, {27000, 37, 473}, {0, 43, 50339},
	} {
		if got := PrayerNeed(c.xp, c.level); got != c.want {
			t.Errorf("PrayerNeed(%d, %d) = %d, want %d", c.xp, c.level, got, c.want)
		}
	}
}

func TestBonesForRoundsUp(t *testing.T) {
	big, _ := BoneKindOf(ObjBigBones)
	bones, _ := BoneKindOf(ObjBones)
	for _, c := range []struct {
		k    BoneKind
		xp   int
		want int
	}{{big, 15, 1}, {big, 16, 2}, {big, 3100, 207}, {bones, 3100, 689}, {bones, 4, 1}, {bones, 0, 0}, {big, -5, 0}} {
		if got := c.k.BonesFor(c.xp); got != c.want {
			t.Errorf("%s for %d xp: %d, want %d", c.k.Name, c.xp, got, c.want)
		}
	}
}

// The ask the fleet's real mule would get: big bones first, as many as the need takes, never past a thousand a kind.
func TestTheMuleAskTakesTheBestBonesTheNeedWants(t *testing.T) {
	got := PlanBonesAsk(12_000, liveMule)
	if len(got) != 1 || got[0].ID != ObjBigBones || got[0].Count != 800 || got[0].Name != "Big bones" {
		t.Errorf("12,000 xp from the live mule: %+v, want 800 Big bones", got)
	}
	// Prayer 31 for an account with 3,100 xp to go: 207 big bones.
	if got := PlanBonesAsk(3100, liveMule); len(got) != 1 || got[0].Count != 207 {
		t.Errorf("3,100 xp: %+v, want 207 Big bones", got)
	}
	// The ceiling per order, and the rest in the next kind.
	got = PlanBonesAsk(40_000, liveMule)
	if len(got) != 2 || got[0].ID != ObjBigBones || got[0].Count != BoneAskMax || got[1].ID != ObjBones || got[1].Count != BoneAskMax {
		t.Errorf("40,000 xp: %+v, want 1000 Big bones and 1000 Bones", got)
	}
	// Dragon bones, when the mule has any, come before everything.
	got = PlanBonesAsk(2000, map[int]int{ObjDragonBones: 10, ObjBigBones: 500, ObjBones: 500})
	if len(got) != 2 || got[0].ID != ObjDragonBones || got[0].Count != 10 || got[1].ID != ObjBigBones {
		t.Errorf("dragon bones first: %+v", got)
	}
	// Only plain bones held: 3,100 xp is 689 of them.
	if got := PlanBonesAsk(3100, map[int]int{ObjBones: 17154}); len(got) != 1 || got[0].ID != ObjBones || got[0].Count != 689 {
		t.Errorf("plain bones: %+v, want 689", got)
	}
	// A thin stock is asked in full, then the next kind covers the rest.
	got = PlanBonesAsk(3100, map[int]int{ObjBigBones: 100, ObjBones: 500})
	if len(got) != 2 || got[0].Count != 100 || got[1].Count != 356 { // 3100-1500 = 1600 xp = 356 bones (355.6)
		t.Errorf("thin big bones: %+v, want 100 Big bones and 356 Bones", got)
	}
}

func TestTheMuleIsNotAskedForNothingWorthATrip(t *testing.T) {
	if got := PlanBonesAsk(12_000, map[int]int{}); got != nil {
		t.Errorf("an empty mule: %+v", got)
	}
	if got := PlanBonesAsk(12_000, map[int]int{ObjBones: 3}); got != nil { // 13.5 xp
		t.Errorf("three bones are not worth the walk: %+v", got)
	}
	if got := PlanBonesAsk(10, liveMule); got != nil { // 10 xp is under MinBoneAskXP
		t.Errorf("a need of 10 xp: %+v", got)
	}
	if got := PlanBonesAsk(0, liveMule); got != nil {
		t.Errorf("no need: %+v", got)
	}
}

func TestABankTripDrawsAPackOfTheBestBones(t *testing.T) {
	bank := map[int]int{ObjBigBones: 900, ObjBones: 4000}
	got := PlanBoneDraws(12_000, bank, 27)
	if len(got) != 1 || got[0].ID != ObjBigBones || got[0].Count != 27 {
		t.Errorf("a full pack of big bones: %+v", got)
	}
	// A pack with fewer free slots draws fewer; a pack of 5 free slots 5.
	if got := PlanBoneDraws(12_000, bank, 5); len(got) != 1 || got[0].Count != 5 {
		t.Errorf("five free slots: %+v", got)
	}
	if got := PlanBoneDraws(12_000, bank, 0); len(got) != 0 {
		t.Errorf("a full pack: %+v", got)
	}
	// Spills over to the next kind when the best runs out.
	got = PlanBoneDraws(12_000, map[int]int{ObjBigBones: 10, ObjBones: 4000}, 27)
	if len(got) != 2 || got[0].Count != 10 || got[1].Count != 17 {
		t.Errorf("10 big bones and 17 bones fill the pack: %+v", got)
	}
	// Never more than the need: 100 xp is 7 big bones.
	if got := PlanBoneDraws(100, bank, 27); len(got) != 1 || got[0].Count != 7 {
		t.Errorf("100 xp: %+v", got)
	}
	// Nothing banked, nothing drawn; notes are not bones in a pack.
	if got := PlanBoneDraws(100, map[int]int{533: 500, 527: 500}, 27); len(got) != 0 {
		t.Errorf("noted bones: %+v", got)
	}
}

func TestACowsRungKillsCowsOnlyAfterItsBonesStepFailed(t *testing.T) {
	at := func(prayer int, failed map[string]bool) Snapshot {
		return Snapshot{Levels: map[string]int{"prayer": prayer, "hitpoints": 40}, Failed: failed}
	}
	// A Prayer rung asks for bones first, then (once that step failed this run) kills cows, and is done at its level.
	for _, c := range []struct {
		name   string
		s      Snapshot
		custom string
		done   bool
	}{
		{"fresh", at(20, nil), CustomPrayerBones, false},
		{"another step failed", at(20, map[string]bool{"provision:x": true, CustomPrayerCows: true}), CustomPrayerBones, false},
		{"the bones step failed", at(20, map[string]bool{CustomPrayerBones: true}), CustomPrayerCows, false},
		{"the level is reached", at(37, nil), "", true},
		{"reached after a failure", at(38, map[string]bool{CustomPrayerBones: true}), "", true},
	} {
		got := CowsRungStep(c.s, "prayer", 37)
		switch {
		case c.done && got.Kind != StepDone:
			t.Errorf("%s: %+v, want done", c.name, got)
		case !c.done && (got.Kind != StepCustom || got.Custom != c.custom || got.Reason == ""):
			t.Errorf("%s: %+v, want custom %q with a reason", c.name, got, c.custom)
		}
	}
	// The other skills' rungs never see the bones step: Hitpoints for the Waterfall gate still kills cows.
	got := CowsRungStep(Snapshot{Levels: map[string]int{"hitpoints": 10}, Failed: map[string]bool{CustomPrayerBones: true}}, "hitpoints", 20)
	if got.Kind != StepCustom || got.Custom != "cows-hitpoints" {
		t.Errorf("a Hitpoints rung: %+v, want cows-hitpoints", got)
	}
	if got := CowsRungStep(Snapshot{Levels: map[string]int{"hitpoints": 20}}, "hitpoints", 20); got.Kind != StepDone {
		t.Errorf("Hitpoints 20 reached: %+v", got)
	}
	if CustomPrayerBones != "cows-prayer" {
		t.Errorf("the bones step is the rung's old custom name, %q", CustomPrayerBones)
	}
	for _, s := range []Step{CowsRungStep(at(20, nil), "prayer", 37), CowsRungStep(at(20, map[string]bool{CustomPrayerBones: true}), "prayer", 37)} {
		if why := ValidateStep(s); why != "" {
			t.Errorf("the step %+v is malformed: %s", s, why)
		}
	}
}

func TestOnlyTheQueensRungsAskTheMule(t *testing.T) {
	for id, want := range map[string]bool{"prayer31": true, KQPrayerRungID: true, "prayer43": false, "cows20": false, "attack20": false} {
		if got := PrayerRungAsksMule(id); got != want {
			t.Errorf("PrayerRungAsksMule(%q) = %v, want %v", id, got, want)
		}
	}
}

// The item rule, move by move.
func TestTheNextMoveFollowsTheItemRule(t *testing.T) {
	bones := map[int]int{ObjBigBones: 14, ObjBones: 3}
	for _, c := range []struct {
		name string
		s    BonesState
		want BonesMoveKind
	}{
		{"the level is reached", boneState(func(s *BonesState) { s.Need = 0; s.Pack = bones }), MoveDone},
		{"no time", boneState(func(s *BonesState) { s.Left = 30 * time.Second }), MoveStop},
		{"no time left, bones in hand: they are buried all the same", boneState(func(s *BonesState) { s.Left = -time.Minute; s.Pack = bones }), MoveBury},
		{"no time left and the pack's bones will not bury", boneState(func(s *BonesState) { s.Left = 0; s.Pack = bones; s.BuryBlocked = true }), MoveStop},
		{"bones in the pack are buried first, whatever else is known", boneState(func(s *BonesState) { s.Pack = bones; s.BankIs = BankUnread }), MoveBury},
		{"a bank never read is read before the mule is asked", boneState(func(s *BonesState) { s.BankIs = BankUnread }), MoveBank},
		{"an old reading with bones is worth the trip", boneState(func(s *BonesState) { s.Bank = map[int]int{ObjBones: 233} }), MoveBank},
		{"an old reading without bones is not walked to: the mule is asked", boneState(nil), MoveAsk},
		{"a fresh bank without bones: the mule", boneState(func(s *BonesState) { s.BankIs = BankFresh; s.Bank = map[int]int{} }), MoveAsk},
		{"a fresh bank with bones left (time ran out mid-draw) is drawn again", boneState(func(s *BonesState) { s.BankIs = BankFresh; s.Bank = map[int]int{ObjBones: 40} }), MoveBank},
		{"the mule's bones are in the bank after an ask", boneState(func(s *BonesState) { s.BankIs = BankDirty; s.Asks = 1 }), MoveBank},
		{"a bank that would not open is left alone, and the mule is not asked to fill it", boneState(func(s *BonesState) { s.BankIs = BankUnread; s.BankBlocked = true }), MoveCows},
		{"bones that would not bury go on to the bank", boneState(func(s *BonesState) { s.Pack = bones; s.BuryBlocked = true; s.BankIs = BankUnread }), MoveBank},
		{"bones that would not bury go to the bank even when it holds none", boneState(func(s *BonesState) { s.Pack = bones; s.BuryBlocked = true }), MoveBank},
		{"bones that would not bury after the bank has had them are left", boneState(func(s *BonesState) { s.Pack = bones; s.BuryBlocked = true; s.BankIs = BankFresh; s.MuleOK = false }), MoveCows},
		{"a rung that leaves the mule alone kills cows", boneState(func(s *BonesState) { s.MuleOK = false }), MoveCows},
		{"no mule heard", boneState(func(s *BonesState) { s.MuleKnown = false; s.Mule = nil }), MoveCows},
		{"a mule with no bones", boneState(func(s *BonesState) { s.Mule = map[int]int{} }), MoveCows},
		{"an ask that brought nothing is not repeated", boneState(func(s *BonesState) { s.AskEmpty = true; s.Asks = 1 }), MoveCows},
		{"the asks are spent", boneState(func(s *BonesState) { s.Asks = MaxBoneAsks }), MoveCows},
		{"too little time for a mule trip, enough for the cows", boneState(func(s *BonesState) { s.Left = BoneAskWindow - time.Second }), MoveCows},
		{"too little time for either", boneState(func(s *BonesState) { s.Left = CowsWindow - time.Second }), MoveStop},
		{"a need under the least ask goes to the cows", boneState(func(s *BonesState) { s.Need = 10 }), MoveCows},
	} {
		got := NextBonesMove(c.s)
		if got.Kind != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, got.Kind, got.Why, c.want)
		}
		if got.Why == "" {
			t.Errorf("%s: the move has no reason for the log", c.name)
		}
		if got.Kind == MoveAsk && len(got.Ask) == 0 {
			t.Errorf("%s: an ask with no lines", c.name)
		}
	}
}

func TestTheAskNamesWhatTheMulePublishes(t *testing.T) {
	m := NextBonesMove(boneState(nil))
	if m.Kind != MoveAsk || len(m.Ask) != 1 || m.Ask[0].Name != "Big bones" || m.Ask[0].Count != 800 {
		t.Fatalf("the move: %+v", m)
	}
	if !strings.Contains(m.Why, "17154 Bones") || !strings.Contains(m.Why, "1488 Big bones") {
		t.Errorf("the reason does not say what the mule publishes: %q", m.Why)
	}
}

func TestTheCowsReasonSaysWhyThereAreNoBones(t *testing.T) {
	for _, c := range []struct {
		s    BonesState
		want string
	}{
		{boneState(func(s *BonesState) { s.MuleOK = false }), "leaves the mule's bones to the Queen's Prayer"},
		{boneState(func(s *BonesState) { s.MuleKnown = false }), "not known"},
		{boneState(func(s *BonesState) { s.AskEmpty = true }), "brought nothing"},
		{boneState(func(s *BonesState) { s.Asks = MaxBoneAsks }), "asks of the mule are spent"},
		{boneState(func(s *BonesState) { s.Mule = map[int]int{} }), "publishes no bones"},
	} {
		if m := NextBonesMove(c.s); m.Kind != MoveCows || !strings.Contains(m.Why, c.want) {
			t.Errorf("want cows because of %q, got %s: %q", c.want, m.Kind, m.Why)
		}
	}
}

// The whole run on the live numbers, played through the pure moves: an account 3,100 xp short of Prayer 31, bank unread,
// the real mule. Ask, the bank it lands in, done; the cows are never reached.
func TestAnAccountShortOfPrayer31NeverReachesTheCows(t *testing.T) {
	s := boneState(func(s *BonesState) { s.Need = 3100; s.BankIs = BankUnread })
	var moves []BonesMoveKind
	for i := 0; i < 10 && s.Need > 0; i++ {
		m := NextBonesMove(s)
		moves = append(moves, m.Kind)
		switch m.Kind {
		case MoveBank:
			if s.BankIs == BankUnread {
				s.BankIs, s.Bank = BankFresh, map[int]int{}
			} else { // the mule's big bones, drawn and buried
				s.BankIs, s.Need = BankFresh, 0
			}
		case MoveAsk:
			s.Asks++
			s.BankIs = BankDirty
		default:
			t.Fatalf("unexpected move %s (%s)", m.Kind, m.Why)
		}
	}
	want := []BonesMoveKind{MoveBank, MoveAsk, MoveBank}
	if len(moves) != len(want) {
		t.Fatalf("moves %v, want %v", moves, want)
	}
	for i := range want {
		if moves[i] != want[i] {
			t.Fatalf("moves %v, want %v", moves, want)
		}
	}
}

// The experience a run buys per hour of each source, from the 16 Oct log and the content: what the report quotes.
// Cows: 175 xp per rung-hour measured (26 accounts, 1,412 rung-minutes, 05:23-07:56). A pack of bones is 27 buries at about 1.2 s (a tick and the poll) and 3 s to
// open the booth and draw: 36 s. A mule trip is the walk to Catherby, the line and the trade: 3 minutes at the median of the
// 35 quest kit asks of 16 Oct and 6 at the 90th percentile, so 7 is used here.
func TestTheBonesPathIsAnOrderOfMagnitudeFasterThanTheCows(t *testing.T) {
	const cowsPerHour = 175.0
	packSecs := 27*1.2 + 3
	bigPerHour := 27 * 15 / packSecs * 3600
	bonesPerHour := 27 * 4.5 / packSecs * 3600
	// Prayer 31 for an account 3,100 xp short, from a cold start: ask 7 minutes, then the draws.
	askMin := 7.0
	minutesBig := askMin + float64(207)/27*packSecs/60
	minutesBones := askMin + float64(689)/27*packSecs/60
	perHourBig := 3100 / (minutesBig / 60)
	perHourBones := 3100 / (minutesBones / 60)
	t.Logf("cows %.0f xp/h; bones draw %.0f xp/h, big bones %.0f xp/h; Prayer 31 3,100 xp short: big bones %.0f min (%.0f xp/h), bones %.0f min (%.0f xp/h) against %.0f min of cows",
		cowsPerHour, bonesPerHour, bigPerHour, minutesBig, perHourBig, minutesBones, perHourBones, 3100/cowsPerHour*60)
	if perHourBones < 10*cowsPerHour || perHourBig < 10*cowsPerHour {
		t.Errorf("the bones path is %.0f / %.0f xp/h against the cows' %.0f: not an order of magnitude", perHourBones, perHourBig, cowsPerHour)
	}
	if minutesBig > 20 || minutesBones > 40 {
		t.Errorf("Prayer 31 3,100 xp short takes %.0f minutes of big bones / %.0f of bones: one 20 minute slot / two", minutesBig, minutesBones)
	}
}
