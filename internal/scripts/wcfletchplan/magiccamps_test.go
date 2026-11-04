package wcfletchplan

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/woodcutplan"
)

// worldMagicCohort is four accounts of the magic cohort switched over on 12 October 2026 (Woodcutting
// 89-92, Fletching 81-83, rune axes, four to a world on worlds 1-3): the brief's ranges, spread, as
// one world's share of the twelve. Levels only; no account is named.
var worldMagicCohort = []Chopper{
	{Wood: 89, Fletch: 81, Axe: woodcutplan.RuneAxe},
	{Wood: 90, Fletch: 82, Axe: woodcutplan.RuneAxe},
	{Wood: 91, Fletch: 82, Axe: woodcutplan.RuneAxe},
	{Wood: 92, Fletch: 83, Axe: woodcutplan.RuneAxe},
}

func magicCampsNamed(names ...string) []Camp {
	var out []Camp
	for _, n := range names {
		for _, c := range MagicCamps {
			if c.Name == n {
				out = append(out, c)
			}
		}
	}
	return out
}

// seatOn seats n accounts one after another from the Seers bank, as a restart's logins do, on a fresh
// board over camps built the way the magic board is, and returns the board and where each went.
func seatOn(camps []Camp, n int, now time.Time) (*CampBoard, []int) {
	b := NewCampBoardFor(camps, ReferenceMagicChopper, BoardPlayers).withNearTie(MagicNearTie)
	at := make([]int, n)
	for i := range at {
		at[i] = b.Seat(fmt.Sprintf("magicacct%d", i), woodcutplan.Seers, false, now).Camp
	}
	return b, at
}

// The ladder's own camp is first and exactly woodcutplan.TIERS' Magic tier, so an account seated
// there chops precisely where it did before the board existed, and a nil board seats there.
func TestMagicHomeCampIsTheLaddersOwn(t *testing.T) {
	c := MagicCamps[0]
	if !c.Home || c.Rung != MagicRung || c.Tier() != woodcutplan.TIERS[MagicRung] {
		t.Errorf("camp 0 must be the ladder's own magic camp: %+v vs %+v", c, woodcutplan.TIERS[MagicRung])
	}
	homes := 0
	for _, c := range MagicCamps {
		if c.Home {
			homes++
		}
	}
	if homes != 1 {
		t.Errorf("want exactly one Home camp, have %d", homes)
	}
}

// Magic camps only, the magic rung's own tree, every name once, none an overflow (a magic seat never
// changes what an account chops), and a camp tier that is the magic rung somewhere else.
func TestMagicCampsAreMagicTreesAndNeverOverflow(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range MagicCamps {
		if seen[c.Name] {
			t.Errorf("camp name %q is used twice", c.Name)
		}
		seen[c.Name] = true
		if c.Rung != MagicRung || c.Loc != Ladder[MagicRung].Tier.Loc {
			t.Errorf("%s: rung %d loc %q, want the magic rung %d and %q", c.Name, c.Rung, c.Loc, MagicRung, Ladder[MagicRung].Tier.Loc)
		}
		if c.Overflow() {
			t.Errorf("%s: Overflow() is true, which would make the board refuse every magic camp", c.Name)
		}
		got, r := c.Tier(), Ladder[MagicRung].Tier
		if got.Label != r.Label || got.Logs != r.Logs || got.XP != r.XP || got.Anchor != c.Anchor || got.Leash != c.Leash {
			t.Errorf("%s: tier %+v is not the magic rung at the camp", c.Name, got)
		}
		if c.Trees <= 0 || c.Leash <= 0 || c.Steps <= 0 || c.Anchor.Level != 0 {
			t.Errorf("%s: trees %d leash %d steps %d anchor %v", c.Name, c.Trees, c.Leash, c.Steps, c.Anchor)
		}
	}
	for i := range Ladder {
		if UsesMagicCamps(i) != (i == MagicRung) {
			t.Errorf("UsesMagicCamps(%d) = %v", i, UsesMagicCamps(i))
		}
		if UsesMagicCamps(i) && (UsesYewCamps(i) || UsesMapleCamps(i)) {
			t.Errorf("rung %d is on two boards", i)
		}
	}
}

// Every camp's whole reach is on the Seers side (woodcutplan.SeersSideRadius), where DecideWoodPhase
// takes the Seers branch and never the fare road. The garden is past SeersRadius (80), which the yew and
// maple camps keep inside; that radius is only "arrived at Seers", and nothing an account on this rung
// does past it differs (the bank, the market and the hand-off all walk there from wherever it stands).
func TestEveryMagicCampIsOnTheSeersSide(t *testing.T) {
	for _, c := range MagicCamps {
		for _, edge := range []struct{ dx, dz int }{{-1, -1}, {-1, 1}, {1, -1}, {1, 1}} {
			at := c.Anchor
			at.X += edge.dx * (c.Leash + 2)
			at.Z += edge.dz * (c.Leash + 2)
			if !woodcutplan.OnSeersSide(at) {
				t.Errorf("%s: %v (the camp's reach) is not on the Seers side", c.Name, at)
			}
		}
	}
}

// Every camp banks at the Seers bank, which the data test measures as the nearest safe one to each; the
// zero Bank is the Seers bank too, so the maple and yew camps (which name none) bank where they always did.
func TestMagicCampsBankAtSeers(t *testing.T) {
	for _, c := range MagicCamps {
		if c.BankOf() != SeersBank {
			t.Errorf("%s banks at %+v", c.Name, c.BankOf())
		}
	}
	if (Camp{}).BankOf() != SeersBank || YewCamps[1].BankOf() != SeersBank || MapleCamps[2].BankOf() != SeersBank {
		t.Error("a camp that names no bank must bank at Seers")
	}
	if SeersBank.Stand != woodcutplan.Seers {
		t.Errorf("the Seers bank's stand %v is not the tile every woodcutting step walks to %v", SeersBank.Stand, woodcutplan.Seers)
	}
}

// The reference is a magic-rung account that cuts the magic shortbow, and the board prices every camp for
// it: 250 Woodcutting plus 83.3 Fletching a log, a swing of 20 in 256 at Woodcutting 90 with the rune axe.
func TestMagicBoardIsPricedForAMagicAccount(t *testing.T) {
	snap := LadderSnapshot{WoodLevel: ReferenceMagicChopper.Wood, FletchLevel: ReferenceMagicChopper.Fletch, Target: "Magic", Gate: GateStartAt, Fletching: true}
	if got := TierIndex(snap); got != MagicRung {
		t.Errorf("the reference is on rung %d, not the magic rung", got)
	}
	for _, a := range worldMagicCohort {
		snap.WoodLevel, snap.FletchLevel = a.Wood, a.Fletch
		if TierIndex(snap) != MagicRung {
			t.Errorf("Woodcutting %d / Fletching %d is not on the magic rung", a.Wood, a.Fletch)
		}
	}
	b := NewMagicBoard()
	for i, c := range MagicCamps {
		p := b.Prices()[i]
		if math.Abs(p.XPPerLog-(250+83.3)) > 1e-9 {
			t.Errorf("%s: %.1f xp a log, want 333.3 (Woodcutting and the magic shortbow)", c.Name, p.XPPerLog)
		}
		if p.Supply <= 0 || p.Account <= 0 {
			t.Errorf("%s: unpriced %+v", c.Name, p)
		}
		t.Logf("%-24s %d trees, %3d steps: supply %.0f logs/h, one account %.1f logs/h (%.0f xp/h), capacity %d",
			c.Name, c.Trees, c.Steps, p.Supply, p.Account, p.Share(1), p.Capacity())
	}
	if b.tie != MagicNearTie {
		t.Errorf("the magic board's near tie is %v", b.tie)
	}
	// The supply and the capacity the header quotes.
	for i, want := range []struct {
		supply float64
		cap    int
	}{{386, 3}, {515, 5}} {
		p := b.Prices()[i]
		if math.Abs(p.Supply-want.supply) > 1 || p.Capacity() != want.cap {
			t.Errorf("%s: supply %.1f capacity %d, the header says %.0f and %d", MagicCamps[i].Name, p.Supply, p.Capacity(), want.supply, want.cap)
		}
	}
}

func TestCapacityIsSupplyOverOneAccount(t *testing.T) {
	cases := []struct {
		p    CampPrice
		want int
	}{
		{CampPrice{Supply: 386, Account: 100}, 3},
		{CampPrice{Supply: 400, Account: 100}, 4},
		{CampPrice{Supply: 50, Account: 100}, 1}, // a camp too small for one account still seats one
		{CampPrice{}, 1},
	}
	for _, c := range cases {
		if got := c.p.Capacity(); got != c.want {
			t.Errorf("Capacity(%+v) = %d, want %d", c.p, got, c.want)
		}
	}
}

// THE MEASUREMENT THE MODEL WAS GIVEN. Live build 8c77500e, 12 October 2026 20:25-20:58: the twelve magic
// accounts four to a world, all at the ladder's three trees, took 80, 86 and 109 logs an account-hour on
// worlds 1, 2 and 3 (the 15-minute idle reports), 367 a world on average. The model gives four accounts
// there fewer: under the measurement, as the yew board's status quo is, so every row of the option table
// is a floor (the chop manipulation's 3-tick swing and a busier world than BoardPlayers both lift every
// camp together).
func TestMagicStatusQuoAgainstTheMeasurement(t *testing.T) {
	const measuredLogsPerWorldHour = (80.0 + 86 + 109) / 3 * 4
	_, rows := CohortXP(MagicCamps, worldMagicCohort, make([]int, len(worldMagicCohort)), BoardPlayers)
	ratio := rows[0].Used / measuredLogsPerWorldHour
	t.Logf("status quo: the model gives %.0f logs/h a world, the live camp gave %.0f: model/measured %.2f", rows[0].Used, measuredLogsPerWorldHour, ratio)
	if ratio < 0.6 || ratio > 1.05 {
		t.Errorf("the model's status quo is %.2f of the measurement: it must be a floor and within 40%% of it", ratio)
	}
}

// TestMagicOptionsArePriced recomputes the option table in magiccamps.go's header: each row the board's
// own seating of one world's four magic accounts over that set of camps, priced with each account's own
// levels. A model or camp change that moves a row by more than 3% fails here, and the header's table has
// to be updated with it.
func TestMagicOptionsArePriced(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	rows := []struct {
		name  string
		camps []Camp
		want  float64 // the header's number, xp/h for the world's four
		occ   []int
	}{
		{"all four at the Seers trees (the live behaviour)", magicCampsNamed("Seers magic trees"), 82.7e3, nil},
		{"all four in the Sorcerers' Tower garden", magicCampsNamed("Sorcerers' Tower garden"), 89.3e3, nil},
		{"both camps (THE DEFAULT)", MagicCamps, 104.5e3, []int{2, 2}},
	}
	var statusQuo, spread float64
	for i, r := range rows {
		b, at := seatOn(r.camps, len(worldMagicCohort), now)
		got, cr := CohortXP(r.camps, worldMagicCohort, at, BoardPlayers)
		var parts []string
		logs := 0.0
		for _, c := range cr {
			if c.Accounts > 0 {
				parts = append(parts, fmt.Sprintf("%s %d (%.0f of %.0f logs/h)", c.Camp, c.Accounts, c.Used, c.Supply))
				logs += c.Used
			}
		}
		t.Logf("%-50s %7.0f xp/h, %6.0f each, %4.0f logs/h (%3.0f each): %s", r.name, got, got/4, logs, logs/4, strings.Join(parts, ", "))
		if r.want > 0 {
			if d := math.Abs(got-r.want) / r.want; d > 0.03 {
				t.Errorf("%s: priced at %.0f xp/h, the header says %.0f (%.1f%% off) - update the table", r.name, got, r.want, 100*d)
			}
		}
		if r.occ != nil {
			if occ := b.Occupancy(now); fmt.Sprint(occ) != fmt.Sprint(r.occ) {
				t.Errorf("%s: seated %v, the header says %v", r.name, occ, r.occ)
			}
		}
		switch i {
		case 0:
			statusQuo = got
		case len(rows) - 1:
			spread = got
		}
	}
	if spread < 1.2*statusQuo {
		t.Errorf("both camps price at %.0f xp/h against %.0f at the one; the premise is a fifth more", spread, statusQuo)
	}
}

// THE LIVE COHORT. On 13 October the owner moved all 23 accounts that can chop and fletch magic logs to the
// magic target with homeWorld 1, 2 and 3 (8, 8 and 7): eight accounts on a world's seven trees. The board seats
// eight four and four (the capacities are 3 and 5, and the garden's four trees feed its four), seven three and
// four, and the header's rows are these.
func TestMagicEightToAWorld(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	cohort := func(n int) []Chopper {
		out := make([]Chopper, n)
		for i := range out {
			out[i] = ReferenceMagicChopper
		}
		return out
	}
	for _, c := range []struct {
		n       int
		occ     []int
		xp      float64 // the header's number for the camps seated, xp/h for the world
		seersXP float64 // the same accounts all at the Seers trees
	}{{8, []int{4, 4}, 173.1e3, 112.6e3}, {7, []int{3, 4}, 159.6e3, 107.9e3}} {
		b, at := seatOn(MagicCamps, c.n, now)
		if occ := b.Occupancy(now); fmt.Sprint(occ) != fmt.Sprint(c.occ) {
			t.Errorf("%d to a world are seated %v, want %v", c.n, occ, c.occ)
		}
		got, rows := CohortXP(MagicCamps, cohort(c.n), at, BoardPlayers)
		_, seersAt := seatOn(magicCampsNamed("Seers magic trees"), c.n, now)
		seers, _ := CohortXP(magicCampsNamed("Seers magic trees"), cohort(c.n), seersAt, BoardPlayers)
		t.Logf("%d to a world: %v, %.0f xp/h (%.0f each) against %.0f (%.0f each) all at the Seers trees: %+v", c.n, b.Occupancy(now), got, got/float64(c.n), seers, seers/float64(c.n), rows)
		if math.Abs(got-c.xp)/c.xp > 0.03 || math.Abs(seers-c.seersXP)/c.seersXP > 0.03 {
			t.Errorf("%d to a world price %.0f and %.0f at the Seers trees; the header says %.0f and %.0f - update it", c.n, got, seers, c.xp, c.seersXP)
		}
		if got < 1.4*seers {
			t.Errorf("%d to a world: both camps %.0f against %.0f at the Seers trees; the premise is two fifths more", c.n, got, seers)
		}
	}
}

// THE NEAR TIE. A world's four, seated from the Seers bank: 2 + 2 on the magic board, 3 + 1 (and less
// experience) at the near tie every other board uses. A seat is stable once taken, and a world's board
// gives each new account the camp its share says, with its capacity.
func TestMagicNearTieSeatsTwoAndTwo(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	b, at := seatOn(MagicCamps, 4, now)
	if occ := b.Occupancy(now); occ[0] != 2 || occ[1] != 2 {
		t.Fatalf("the magic board seats a world's four %v, want 2 + 2", occ)
	}
	wide := NewCampBoardFor(MagicCamps, ReferenceMagicChopper, BoardPlayers)
	wideAt := make([]int, 4)
	for i := range wideAt {
		wideAt[i] = wide.Seat(fmt.Sprintf("magicacct%d", i), woodcutplan.Seers, false, now).Camp
	}
	if occ := wide.Occupancy(now); occ[0] != 3 || occ[1] != 1 {
		t.Errorf("at patchNearTie the four were seated %v; the header says 3 + 1", occ)
	}
	got, _ := CohortXP(MagicCamps, worldMagicCohort, at, BoardPlayers)
	old, _ := CohortXP(MagicCamps, worldMagicCohort, wideAt, BoardPlayers)
	t.Logf("2 + 2: %.0f xp/h a world; 3 + 1: %.0f", got, old)
	if got <= old || math.Abs(old-98e3)/98e3 > 0.03 {
		t.Errorf("2 + 2 prices at %.0f and 3 + 1 at %.0f; the header says about 104.5k and 98k", got, old)
	}
	// Stable: the same accounts asking again keep their camps.
	for i := range at {
		if s := b.Seat(fmt.Sprintf("magicacct%d", i), woodcutplan.Seers, false, now.Add(time.Minute)); s.Camp != at[i] || s.Moved {
			t.Errorf("account %d moved %d -> %d", i, at[i], s.Camp)
		}
	}
	// A fifth makes three at one camp and two at the other: the third seat at either earns within the tie
	// of the other's (23.2k at the Seers trees, 24.4k in the garden), so the nearer one takes it.
	b.Seat("magicacct4", woodcutplan.Seers, false, now)
	if occ := b.Occupancy(now); occ[0]+occ[1] != 5 || occ[0] < 2 || occ[1] < 2 {
		t.Errorf("five seated %v", occ)
	}
}

func TestMagicBoardsArePerWorldAndTheirOwn(t *testing.T) {
	if SharedMagicBoardFor(1) == SharedMagicBoardFor(2) || SharedMagicBoardFor(1) != SharedMagicBoardFor(1) {
		t.Error("one magic board per world")
	}
	if SharedMagicBoardFor(-3) != SharedMagicBoardFor(0) {
		t.Error("a negative world should be world 0")
	}
	if SharedMagicBoardFor(1) == SharedYewBoardFor(1) || SharedMagicBoardFor(1) == SharedCampBoardFor(1) {
		t.Error("the magic board is another rung's")
	}
	if got := SharedMagicBoardFor(1).Camps(); len(got) != len(MagicCamps) || got[0].Name != MagicCamps[0].Name {
		t.Errorf("the shared magic board holds %v", got)
	}
}
