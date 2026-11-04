package wcfletchplan

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// The ladder's own camps are in the list exactly as woodcutplan.TIERS has them, so an account
// seated there chops precisely where it did before this file existed.
func TestHomeCampsAreTheLaddersOwn(t *testing.T) {
	homes := map[int]bool{}
	for _, c := range MapleCamps {
		if !c.Home {
			continue
		}
		if homes[c.Rung] {
			t.Errorf("rung %d has two Home camps", c.Rung)
		}
		homes[c.Rung] = true
		if c.Tier() != woodcutplan.TIERS[c.Rung] {
			t.Errorf("%s: tier %+v differs from woodcutplan.TIERS[%d] %+v", c.Name, c.Tier(), c.Rung, woodcutplan.TIERS[c.Rung])
		}
	}
	if !homes[MapleRung] || !homes[WillowRung] {
		t.Errorf("want a Home camp for the maple and the willow rung, have %v", homes)
	}
	if MapleCamps[0].Rung != MapleRung || !MapleCamps[0].Home {
		t.Errorf("camp 0 must be the ladder's own maple camp (a nil board seats there): %+v", MapleCamps[0])
	}
}

// A camp is its rung somewhere else: same logs, experience and gates, its own place and loc.
func TestCampTierIsItsRungElsewhere(t *testing.T) {
	for _, c := range MapleCamps {
		got, r := c.Tier(), Ladder[c.Rung].Tier
		if got.Label != r.Label || got.Logs != r.Logs || got.XP != r.XP || got.Level != r.Level || got.StartAt != r.StartAt {
			t.Errorf("%s: tier %+v is not rung %d's (%+v) apart from place and loc", c.Name, got, c.Rung, r)
		}
		if got.Loc != c.Loc || got.Anchor != c.Anchor || got.Leash != c.Leash {
			t.Errorf("%s: tier did not take the camp's own loc/anchor/leash: %+v", c.Name, got)
		}
		if c.Loc != r.Loc {
			t.Errorf("%s: loc %q is not the rung's own tree %q", c.Name, c.Loc, r.Loc)
		}
		if c.Trees <= 0 || c.Leash <= 0 || c.Steps <= 0 || c.Anchor.Level != 0 {
			t.Errorf("%s: trees %d leash %d steps %d anchor %v", c.Name, c.Trees, c.Leash, c.Steps, c.Anchor)
		}
	}
}

// Only maple camps and the willow overflow, the maples first, and every name once.
func TestMapleCampsAreMaplesThenWillows(t *testing.T) {
	seen := map[string]bool{}
	overflow := false
	for _, c := range MapleCamps {
		if seen[c.Name] {
			t.Errorf("camp name %q is used twice", c.Name)
		}
		seen[c.Name] = true
		switch c.Rung {
		case MapleRung:
			if overflow {
				t.Errorf("%s: a maple camp after the overflow camps", c.Name)
			}
		case WillowRung:
			overflow = true
		default:
			t.Errorf("%s: rung %d is neither maple nor willow", c.Name, c.Rung)
		}
		if c.Overflow() != (c.Rung != MapleRung) {
			t.Errorf("%s: Overflow() = %v", c.Name, c.Overflow())
		}
	}
	if !overflow {
		t.Error("no overflow camp")
	}
}

// The whole reach of a camp stays inside SeersRadius, so DecideWoodPhase always takes its Seers
// branch there and never the fare/ship road.
func TestEveryCampIsInsideTheSeersRadius(t *testing.T) {
	for _, c := range MapleCamps {
		if reach := tile.Chebyshev(c.Anchor, woodcutplan.Seers) + c.Leash + 2; reach > woodcutplan.SeersRadius {
			t.Errorf("%s: anchor %v leash %d reaches %d tiles from the Seers bank, past SeersRadius %d",
				c.Name, c.Anchor, c.Leash, reach, woodcutplan.SeersRadius)
		}
	}
}

// An account overflowed to a willow camp KEEPS FLETCHING: for every Fletching level the maple
// rung holds an account at (from the maple shortbow's 50 up to the yew shortbow's 65), the willow
// rung has a product it may make, and it is the willow longbow.
func TestOverflowStillFletchesWhatItChops(t *testing.T) {
	lo, hi := Ladder[MapleRung].EntryLevel(), Ladder[MapleRung+1].EntryLevel()
	for fl := lo; fl < hi; fl++ {
		for _, c := range MapleCamps {
			p, ok := Choose(c.Rung, fl, NormalShafts)
			if !ok {
				t.Errorf("%s at Fletching %d: nothing to cut its logs into", c.Name, fl)
				continue
			}
			if c.Overflow() && p.Kind != KindLongbow {
				t.Errorf("%s at Fletching %d cuts %s, want the willow longbow", c.Name, fl, p.ItemName)
			}
		}
	}
}

func TestUsesMapleCampsOnlyOnTheMapleRung(t *testing.T) {
	for i := range Ladder {
		if got := UsesMapleCamps(i); got != (i == MapleRung) {
			t.Errorf("UsesMapleCamps(%d) = %v", i, got)
		}
	}
	if Ladder[MapleRung].LogsID != MapleLogsID || Ladder[WillowRung].LogsID != WillowLogsID {
		t.Errorf("MapleRung %d / WillowRung %d point at the wrong logs", MapleRung, WillowRung)
	}
}

// ---- pricing ----

// seatAll seats the live cohort one after another from the Seers bank, as a restart's logins do,
// and returns where each went.
func seatAll(b *CampBoard, overflow bool, now time.Time) []int {
	at := make([]int, len(liveMapleCohort))
	for i := range liveMapleCohort {
		at[i] = b.Seat(fmt.Sprintf("acct%d", i), woodcutplan.Seers, overflow, now).Camp
	}
	return at
}

func campsNamed(names ...string) []Camp {
	var out []Camp
	for _, n := range names {
		for _, c := range MapleCamps {
			if c.Name == n {
				out = append(out, c)
			}
		}
	}
	return out
}

// TestMapleOptionsArePriced recomputes the option table in maplecamps.go's header, each row the
// board's own seating of the live cohort over that set of camps, priced with each account's own
// levels. A model or camp change that moves a row by more than 3% fails here, and the header's
// table has to be updated with it.
func TestMapleOptionsArePriced(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	near := []string{"Seers maples", "Seers south maples", "Camelot maples"}
	allMaples := append(append([]string{}, near...), "north-west field", "north-west field east")
	rows := []struct {
		name  string
		camps []Camp
		want  float64 // the header's number, xp/h for the cohort
	}{
		{"all 79 at the Seers maples", campsNamed("Seers maples"), 265e3},
		{"the three maple camps near the bank", campsNamed(near...), 595e3},
		{"every maple camp (mapleOverflow off)", campsNamed(allMaples...), 1_248e3},
		{"near maples + Seers willows", campsNamed(append(near, "Seers willows")...), 1_521e3},
		{"every maple camp + Seers willows", campsNamed(append(allMaples, "Seers willows")...), 1_872e3},
		{"every camp (the default)", MapleCamps, 2_147e3},
		{"willows only", campsNamed("Seers willows", "Catherby willows"), 1_667e3},
	}
	for _, r := range rows {
		at := seatAll(NewCampBoard(r.camps), true, now)
		got, cr := CohortXP(r.camps, liveMapleCohort, at, BoardPlayers)
		var parts []string
		for _, c := range cr {
			if c.Accounts > 0 {
				parts = append(parts, fmt.Sprintf("%s %d (%.0f of %.0f logs/h)", c.Camp, c.Accounts, c.Used, c.Supply))
			}
		}
		t.Logf("%-40s %9.0f xp/h, %6.0f each: %s", r.name, got, got/float64(len(liveMapleCohort)), strings.Join(parts, ", "))
		if d := math.Abs(got-r.want) / r.want; d > 0.03 {
			t.Errorf("%s: priced at %.0f xp/h, the header says %.0f (%.1f%% off) — update the table", r.name, got, r.want, 100*d)
		}
	}
}

// YEWS ARE PRICED AND REFUSED. The five accounts at Woodcutting 70+ could chop yews (the owner's
// raised gate), but at Fletching 51-63 they cannot cut a yew (the yew shortbow is 65), so a yew
// pays Woodcutting only. The test prices it both ways and pins the reason it is refused: a seat
// there pays no Fletching at all, and Fletching is the only thing that moves these accounts up
// the owner's ladder, so an account overflowed to yews would stay on the maple rung for ever.
func TestYewsForTheFewArePricedAndRefused(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	at := seatAll(NewCampBoard(nil), true, now)
	_, rows := CohortXP(MapleCamps, liveMapleCohort, at, BoardPlayers)
	for i, a := range liveMapleCohort {
		if a.Wood < Ladder[MapleRung+1].Tier.StartAt {
			continue
		}
		if _, ok := Choose(MapleRung+1, a.Fletch, NormalShafts); ok {
			t.Errorf("Woodcutting %d / Fletching %d can cut yews: this account is on the yew rung, not the maple board", a.Wood, a.Fletch)
		}
		yew := AccountLogsPerHour("Yew", a, 41, false) * Ladder[MapleRung+1].Tier.XP // the church yews, uncontended
		row := rows[at[i]]
		t.Logf("Woodcutting %d / Fletching %d: %.0f Woodcutting xp/h alone at the church yews and no Fletching, against about %.0f combined at its seat at %s",
			a.Wood, a.Fletch, yew, row.XP/float64(row.Accounts), row.Camp)
	}
}

// ---- the board ----

// THE COHORT SPREADS. The live 79 seated one after another: every camp used, the ladder's own
// maple camp no longer holds most of the cohort, the shares end up even, and the cohort is priced
// at several times the status quo.
func TestBoardSpreadsTheLiveMapleCohort(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	b := NewCampBoard(nil)
	at := seatAll(b, true, now)
	occ := b.Occupancy(now)
	for i, c := range MapleCamps {
		if occ[i] == 0 {
			t.Errorf("%s got nobody", c.Name)
		}
	}
	if occ[0] > 10 {
		t.Errorf("the ladder's own maple camp still holds %d of 79", occ[0])
	}
	lo, hi := math.Inf(1), 0.0
	for i := range MapleCamps {
		s := b.prices[i].Share(occ[i])
		lo, hi = math.Min(lo, s), math.Max(hi, s)
	}
	if hi > 1.6*lo {
		t.Errorf("shares range %.0f..%.0f xp/h: the fill is not even (%v)", lo, hi, occ)
	}
	built, _ := CohortXP(MapleCamps, liveMapleCohort, at, BoardPlayers)
	before, _ := CohortXP(MapleCamps, liveMapleCohort, make([]int, len(at)), BoardPlayers)
	if built < 5*before {
		t.Errorf("the board prices at %.0f xp/h against %.0f for the status quo; the premise is several times more", built, before)
	}
	t.Logf("79 accounts: %s; %.0f xp/h against %.0f", DescribeCamps(MapleCamps, occ), built, before)
}

func TestOverflowOffKeepsToMaples(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	b := NewCampBoard(nil)
	seatAll(b, false, now)
	for i, n := range b.Occupancy(now) {
		if n > 0 && MapleCamps[i].Overflow() {
			t.Errorf("mapleOverflow off, yet %d account(s) seated at %s", n, MapleCamps[i].Name)
		}
	}
}

// Turning the overflow off moves an account already seated at a willow camp straight back to a
// maple camp, rather than leaving it where the setting now says it may not be.
func TestOverflowSeatIsGivenUpWhenTheSettingGoesOff(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	b := NewCampBoard(nil)
	at := seatAll(b, true, now)
	who := -1
	for i, c := range at {
		if MapleCamps[c].Overflow() {
			who = i
			break
		}
	}
	if who < 0 {
		t.Fatal("nobody overflowed")
	}
	s := b.Seat(fmt.Sprintf("acct%d", who), woodcutplan.Seers, false, now.Add(time.Minute))
	if MapleCamps[s.Camp].Overflow() || !s.Moved {
		t.Errorf("with the overflow off the account stayed at %s (Moved=%v)", MapleCamps[s.Camp].Name, s.Moved)
	}
}

func TestCampSeatIsStableAndRefreshes(t *testing.T) {
	b := NewCampBoard(nil)
	now := time.Unix(1_800_000_000, 0)
	first := b.Seat("Acct_133", woodcutplan.Seers, true, now)
	if !first.Moved || first.Share <= 0 {
		t.Fatalf("a first seat should report Moved and a share: %+v", first)
	}
	for i := 1; i <= 8; i++ {
		again := b.Seat("acct 133", woodcutplan.Seers, true, now.Add(time.Duration(i)*time.Minute))
		if again.Camp != first.Camp || again.Moved {
			t.Fatalf("pass %d: seat moved %d -> %d (Moved=%v) with nothing else on the board", i, first.Camp, again.Camp, again.Moved)
		}
	}
	if occ := b.Occupancy(now.Add(8 * time.Minute)); occ[first.Camp] != 1 {
		t.Errorf("occupancy %v after steady refreshes", occ)
	}
	if occ := b.Occupancy(now.Add(8*time.Minute + PatchSeatTTL + time.Second)); occ[first.Camp] != 0 {
		t.Errorf("seat held past the TTL: %v", occ)
	}
	b.Seat("gone", woodcutplan.Seers, true, now)
	b.Leave("GONE")
	for i, n := range b.Occupancy(now) {
		if n != 0 && i != first.Camp {
			t.Errorf("seat held after Leave: %v", b.Occupancy(now))
		}
	}
}

// Rebalancing: a camp emptied by accounts that graduated is refilled from the fullest ones, but
// only after PatchRebalanceAfter and only for a clear gain.
func TestCampRebalanceRefillsAnEmptiedCamp(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	b := NewCampBoard(nil)
	at := seatAll(b, true, now)
	// Everybody at the Catherby willows graduates to yews.
	emptied := len(MapleCamps) - 1
	for i, c := range at {
		if c == emptied {
			b.Leave(fmt.Sprintf("acct%d", i))
		}
	}
	refresh := func(when time.Time) int {
		moved := 0
		for i, c := range at {
			if c == emptied {
				continue
			}
			if s := b.Seat(fmt.Sprintf("acct%d", i), woodcutplan.Seers, true, when); s.Moved {
				moved++
				if s.Camp != emptied {
					t.Errorf("acct%d rebalanced to %s, want the emptied %s", i, MapleCamps[s.Camp].Name, MapleCamps[emptied].Name)
				}
			}
		}
		return moved
	}
	// Refreshed every minute, as a live account's every-pass refresh keeps it well inside
	// PatchSeatTTL. Nobody moves before PatchRebalanceAfter, however lopsided the board.
	for m := time.Minute; m < PatchRebalanceAfter; m += time.Minute {
		if n := refresh(now.Add(m)); n != 0 {
			t.Fatalf("%d accounts moved %s in, before PatchRebalanceAfter", n, m)
		}
	}
	n := refresh(now.Add(PatchRebalanceAfter + time.Second))
	if n == 0 {
		t.Fatalf("nobody moved to the emptied %s", MapleCamps[emptied].Name)
	}
	// Only for a clear gain: the refill stops while the emptied camp is still well short of the
	// crowd it had, rather than walking half the cohort across the map.
	if left := b.Occupancy(now.Add(PatchRebalanceAfter + time.Second))[emptied]; left >= emptiedHad(at, emptied) {
		t.Errorf("%d accounts refilled %s, as many as it had (%d): the gain margin is not holding anyone back", left, MapleCamps[emptied].Name, emptiedHad(at, emptied))
	}
	t.Logf("%d account(s) moved to the emptied %s", n, MapleCamps[emptied].Name)
}

func emptiedHad(at []int, camp int) int {
	n := 0
	for _, c := range at {
		if c == camp {
			n++
		}
	}
	return n
}

func TestNilCampBoardSeatsAtTheLaddersCamp(t *testing.T) {
	var b *CampBoard
	if s := b.Seat("x", woodcutplan.Seers, true, time.Now()); s.Camp != 0 {
		t.Errorf("nil board seated at %d", s.Camp)
	}
	b.Leave("x")
	if b.Occupancy(time.Now()) != nil || b.Camps() != nil {
		t.Error("nil board reported occupancy or camps")
	}
}

func TestCampBoardsArePerWorld(t *testing.T) {
	if SharedCampBoardFor(1) == SharedCampBoardFor(2) {
		t.Error("worlds 1 and 2 share a board")
	}
	if SharedCampBoardFor(-1) != SharedCampBoardFor(0) {
		t.Error("a negative world should be world 0")
	}
}

// A restart's burst: every seat goes through one lock, and a seat depends only on the counts, so
// a hundred simultaneous callers end up exactly where a hundred one-at-a-time callers do.
func TestConcurrentCampSeatingIsAtomic(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	serial := NewCampBoard(nil)
	for i := 0; i < 100; i++ {
		serial.Seat(fmt.Sprintf("burst%d", i), woodcutplan.Seers, true, now)
	}
	b := NewCampBoard(nil)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b.Seat(fmt.Sprintf("burst%d", i), woodcutplan.Seers, true, now)
		}(i)
	}
	wg.Wait()
	got, want := b.Occupancy(now), serial.Occupancy(now)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("concurrent occupancy %v, serial %v", got, want)
	}
}

func TestDescribeCamps(t *testing.T) {
	if got := DescribeCamps(MapleCamps, make([]int, len(MapleCamps))); got != "nobody seated" {
		t.Errorf("empty board: %q", got)
	}
	occ := make([]int, len(MapleCamps))
	occ[0], occ[len(occ)-1] = 3, 7
	want := MapleCamps[len(occ)-1].Name + " 7/5, Seers maples 3/4"
	if got := DescribeCamps(MapleCamps, occ); got != want {
		t.Errorf("DescribeCamps = %q, want %q", got, want)
	}
}

// A harness pin holds an account at a named camp: through the overflow flag, past the rebalance
// clock, and until Leave, after which the account is seated by shares again.
func TestPinHoldsAnAccountAtANamedCamp(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	b := NewCampBoard(nil)
	cath := b.CampIndex("catherby WILLOWS")
	if cath < 0 || b.CampIndex("nowhere") != -1 {
		t.Fatalf("CampIndex: %d", cath)
	}
	b.Pin("Probe_1", cath)
	b.Pin("probe 2", 99) // outside the board: ignored
	first := b.Seat("probe 1", woodcutplan.Seers, false, now)
	if first.Camp != cath || !first.Moved {
		t.Fatalf("pinned account seated at %s (Moved=%v), want %s", MapleCamps[first.Camp].Name, first.Moved, MapleCamps[cath].Name)
	}
	for m := time.Minute; m <= 3*PatchRebalanceAfter; m += time.Minute {
		if s := b.Seat("probe 1", woodcutplan.Seers, false, now.Add(m)); s.Camp != cath || s.Moved {
			t.Fatalf("%s in: pinned seat moved to %s (Moved=%v)", m, MapleCamps[s.Camp].Name, s.Moved)
		}
	}
	if s := b.Seat("probe 2", woodcutplan.Seers, true, now); s.Camp == 99 {
		t.Error("a pin outside the board was honoured")
	}
	b.Leave("PROBE_1")
	if s := b.Seat("probe 1", woodcutplan.Seers, false, now.Add(time.Hour)); MapleCamps[s.Camp].Overflow() {
		t.Errorf("after Leave the pin still held: seated at %s with the overflow off", MapleCamps[s.Camp].Name)
	}
}
