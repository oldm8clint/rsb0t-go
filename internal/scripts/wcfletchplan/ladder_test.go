package wcfletchplan

import (
	"testing"

	"rsb0t-go/internal/bankxp"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// TestLadderMatchesContent pins every derived number against content.
//
// The ladder is DERIVED at init from internal/bankxp.Routes rather than typed, which is what
// stops a remembered number reaching the game — but a derivation nobody checks is just a
// different way to be silently wrong. This table is the check: it is the six
// `[fletching_<species>]` sections of content/scripts/skill_fletching/configs/cut_logs/
// cut_logs.dbrow read by hand, in the form `data=<kind>,<obj>,<level>,<xp10>`, plus the log obj
// ids from content/pack/obj.pack. If internal/bankxp/table.go is regenerated against a content
// checkout whose numbers differ, this fails and names the row.
func TestLadderMatchesContent(t *testing.T) {
	type want struct {
		label    string
		logsID   int
		logsName string
		// shaftID/shaftLevel/shaftXP10/shaftPer are 0 where the section has no `shafts` column,
		// which is every section but [fletching_normal].
		shaftID, shaftLevel, shaftXP10, shaftPer int
		shortID, shortLevel, shortXP10           int
		longID, longLevel, longXP10              int
	}
	// cut_logs.dbrow, section by section:
	//
	//   [fletching_normal]  log=logs        shafts=15
	//                       shortbow=unstrung_shortbow,5,50   longbow=unstrung_longbow,10,100
	//   [fletching_oak]     log=oak_logs
	//                       shortbow=unstrung_oak_shortbow,20,165   longbow=...,25,250
	//   [fletching_willow]  log=willow_logs
	//                       shortbow=...,35,333              longbow=...,40,415
	//   [fletching_maple]   log=maple_logs
	//                       shortbow=...,50,500              longbow=...,55,583
	//   [fletching_yew]     log=yew_logs
	//                       shortbow=...,65,675              longbow=...,70,750
	//   [fletching_magic]   log=magic_logs
	//                       shortbow=...,80,833              longbow=...,85,915
	//
	// The shaft experience is not a column: cut_logs.rs2 pays `multiply($shaft_count, 5)` tenths,
	// so 15 shafts are 75 tenths = 7.5 xp.
	wants := []want{
		{"Normal tree", 1511, "Logs", 52, 1, 75, 15, 50, 5, 50, 48, 10, 100},
		{"Oak", 1521, "Oak logs", 0, 0, 0, 0, 54, 20, 165, 56, 25, 250},
		{"Willow", 1519, "Willow logs", 0, 0, 0, 0, 60, 35, 333, 58, 40, 415},
		{"Maple", 1517, "Maple logs", 0, 0, 0, 0, 64, 50, 500, 62, 55, 583},
		{"Yew", 1515, "Yew logs", 0, 0, 0, 0, 68, 65, 675, 66, 70, 750},
		{"Magic", 1513, "Magic logs", 0, 0, 0, 0, 72, 80, 833, 70, 85, 915},
	}
	if len(Ladder) != len(wants) {
		t.Fatalf("ladder has %d rungs, want %d", len(Ladder), len(wants))
	}
	for i, w := range wants {
		r := Ladder[i]
		if r.Tier.Label != w.label || r.LogsID != w.logsID || r.LogsName != w.logsName {
			t.Errorf("rung %d = %q logs %d %q, want %q logs %d %q", i, r.Tier.Label, r.LogsID, r.LogsName, w.label, w.logsID, w.logsName)
		}
		if w.shaftID == 0 {
			if r.Shaft != nil {
				t.Errorf("rung %d (%s) has an arrow-shaft product; only [fletching_normal] carries `data=shafts`", i, w.label)
			}
		} else {
			if r.Shaft == nil {
				t.Fatalf("rung %d (%s) has no arrow-shaft product", i, w.label)
			}
			checkProduct(t, w.label+" shaft", *r.Shaft, KindShaft, w.shaftID, w.shaftLevel, w.shaftXP10, w.shaftPer, "shaft")
		}
		if r.Shortbow == nil || r.Longbow == nil {
			t.Fatalf("rung %d (%s) is missing a bow product", i, w.label)
		}
		checkProduct(t, w.label+" shortbow", *r.Shortbow, KindShortbow, w.shortID, w.shortLevel, w.shortXP10, 1, "shortbow")
		checkProduct(t, w.label+" longbow", *r.Longbow, KindLongbow, w.longID, w.longLevel, w.longXP10, 1, "longbow")
	}
}

func checkProduct(t *testing.T, what string, p Product, kind ProductKind, id, level, xp10, per int, match string) {
	t.Helper()
	if p.Kind != kind {
		t.Errorf("%s: kind = %q, want %q", what, p.Kind, kind)
	}
	if p.ItemID != id {
		t.Errorf("%s: obj id = %d, want %d", what, p.ItemID, id)
	}
	if p.Level != level {
		t.Errorf("%s: fletching level = %d, want %d (cut_logs.dbrow)", what, p.Level, level)
	}
	if p.XP10 != xp10 {
		t.Errorf("%s: xp10 = %d, want %d (cut_logs.dbrow)", what, p.XP10, xp10)
	}
	if p.PerLog != per {
		t.Errorf("%s: per log = %d, want %d", what, p.PerLog, per)
	}
	if p.MakeMatch != match {
		t.Errorf("%s: make-menu match = %q, want %q", what, p.MakeMatch, match)
	}
	if p.RouteKey == "" || p.Source == "" {
		t.Errorf("%s: carries no content citation (key %q, source %q)", what, p.RouteKey, p.Source)
	}
}

// TestLogIDsAgreeWithContentNames is the other half of the pin: the six ids stated in ladder.go
// must be the ids content gave the names woodcutplan chops. Without this, a transposed id would
// build a perfectly self-consistent ladder against the wrong log.
func TestLogIDsAgreeWithContentNames(t *testing.T) {
	for _, r := range Ladder {
		routes := bankxp.RoutesFor(r.LogsID)
		if len(routes) == 0 {
			t.Fatalf("obj %d (%s) has no bankxp route at all", r.LogsID, r.LogsName)
		}
		got := routes[0].Primary().Name
		if !bankxp.NameMatches(got, r.Tier.Logs) {
			t.Errorf("obj %d is %q in content but woodcutplan tier %q chops %q", r.LogsID, got, r.Tier.Label, r.Tier.Logs)
		}
	}
}

// TestMakeMatchesAreUnambiguous guards the one place this script matches a make-menu product by
// NAME. actions.FindMakeProduct does a case-insensitive substring match, so "shortbow" must not
// also match the longbow and vice versa — if it did, a rung would silently produce the wrong bow.
func TestMakeMatchesAreUnambiguous(t *testing.T) {
	for _, r := range Ladder {
		products := []*Product{r.Shaft, r.Shortbow, r.Longbow}
		for _, want := range products {
			if want == nil {
				continue
			}
			hits := 0
			for _, other := range products {
				if other == nil {
					continue
				}
				if containsFold2(other.ItemName, want.MakeMatch) {
					hits++
				}
			}
			if hits != 1 {
				t.Errorf("%s: match %q hits %d of the rung's products, want exactly 1", r.Tier.Label, want.MakeMatch, hits)
			}
		}
	}
}

func containsFold2(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexFold(s, sub) >= 0
}

func indexFold(s, sub string) int {
	ls, lsub := lower(s), lower(sub)
	for i := 0; i+len(lsub) <= len(ls); i++ {
		if ls[i:i+len(lsub)] == lsub {
			return i
		}
	}
	return -1
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// TestLongbowAlwaysBeatsShortbow is the invariant behind "upgrade to longbows when we can": the
// longbow must cost more level AND pay more experience on every rung, or displacing the shortbow
// with it would be a downgrade somewhere.
func TestLongbowAlwaysBeatsShortbow(t *testing.T) {
	for _, r := range Ladder {
		if r.Longbow.Level <= r.Shortbow.Level {
			t.Errorf("%s: longbow level %d is not above shortbow level %d", r.Tier.Label, r.Longbow.Level, r.Shortbow.Level)
		}
		if r.Longbow.XP10 <= r.Shortbow.XP10 {
			t.Errorf("%s: longbow pays %d tenths, shortbow pays %d — upgrading would lose experience", r.Tier.Label, r.Longbow.XP10, r.Shortbow.XP10)
		}
	}
}

// TestEntryLevelsAreTheOwnersLadder is the ladder the fleet owner described, rung by rung.
func TestEntryLevelsAreTheOwnersLadder(t *testing.T) {
	want := []int{1, 20, 35, 50, 65, 80}
	for i, w := range want {
		if got := Ladder[i].EntryLevel(); got != w {
			t.Errorf("rung %d (%s) entry level = %d, want %d", i, Ladder[i].Tier.Label, got, w)
		}
	}
}

// TestEntryLevelsStrictlyIncrease keeps FletchRungIndex's single scan correct: a ladder whose
// gates were not monotonic could not be resolved by "the highest rung whose gate is met".
func TestEntryLevelsStrictlyIncrease(t *testing.T) {
	for i := 1; i < len(Ladder); i++ {
		if Ladder[i].EntryLevel() <= Ladder[i-1].EntryLevel() {
			t.Errorf("rung %d entry %d is not above rung %d entry %d", i, Ladder[i].EntryLevel(), i-1, Ladder[i-1].EntryLevel())
		}
	}
}

// TestHandoverProductsAreTheUnstrungBowsAndTheShaft pins the hand-over set. These thirteen ids
// are what the mule should receive; the STRUNG bows (839/841/843/845/847/849/851/853/855/857/
// 859/861, content/scripts/skill_combat/configs/ranged/bows.obj) must not be in it, even though
// six of them share a display name with an id that is.
func TestHandoverProductsAreTheUnstrungBowsAndTheShaft(t *testing.T) {
	want := []int{48, 50, 52, 54, 56, 58, 60, 62, 64, 66, 68, 70, 72}
	got := HandoverProductIDs()
	if len(got) != len(want) {
		t.Fatalf("hand-over ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hand-over ids = %v, want %v", got, want)
		}
	}
	for _, strung := range []int{839, 841, 843, 845, 847, 849, 851, 853, 855, 857, 859, 861} {
		if IsHandoverProduct(strung) {
			t.Errorf("strung bow obj %d counts as hand-over stock; it is a different item that shares a display name", strung)
		}
	}
}

// TestLadderTiersAreWoodcutplansOwn is the anti-fork guard. This script must chop in exactly the
// places ProgressiveWoodcutter does, or the side-by-side comparison users want is measuring
// two changes at once.
func TestLadderTiersAreWoodcutplansOwn(t *testing.T) {
	if len(Ladder) != len(woodcutplan.TIERS) {
		t.Fatalf("ladder has %d rungs against woodcutplan's %d tiers", len(Ladder), len(woodcutplan.TIERS))
	}
	for i, r := range Ladder {
		if r.Tier != woodcutplan.TIERS[i] {
			t.Errorf("rung %d's tier is not woodcutplan.TIERS[%d] — the two ladders have forked", i, i)
		}
	}
}

// TestIsLadderLogs covers the six and rejects a near miss.
func TestIsLadderLogs(t *testing.T) {
	for _, id := range LogIDs() {
		if !IsLadderLogs(id) {
			t.Errorf("obj %d is a ladder log but IsLadderLogs said no", id)
		}
	}
	for _, id := range []int{0, 52, 946, 1512, 1510} {
		if IsLadderLogs(id) {
			t.Errorf("obj %d is not a ladder log but IsLadderLogs said yes", id)
		}
	}
	if _, ok := RungIndexForLogs(OakLogsID); !ok {
		t.Error("oak logs should resolve to a rung")
	}
	if _, ok := RungIndexForLogs(1); ok {
		t.Error("obj 1 should not resolve to a rung")
	}
}
