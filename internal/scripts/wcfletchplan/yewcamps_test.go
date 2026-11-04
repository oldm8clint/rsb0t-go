package wcfletchplan

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// liveYewCohort is the (Woodcutting, Fletching) of every ProgressiveWCFletch account on the yew
// rung at the 2026-10-04 23:06 relaunch of live build 73ff4228: the levels in each account's own
// "wcfletch: rung 5/6: Yew (Woodcutting N, Fletching M)" line, 23 accounts, with the rune axe the
// cohort's settings buy. Levels only; no account is named.
var liveYewCohort = func() []Chopper {
	pairs := [][2]int{
		{74, 67}, {74, 67}, {75, 66}, {75, 67}, {75, 67}, {75, 67}, {75, 67}, {75, 67}, {75, 67}, {75, 67},
		{75, 68}, {75, 68}, {75, 68}, {76, 67}, {77, 67}, {77, 67}, {77, 67}, {77, 68}, {77, 69}, {77, 70},
		{78, 67}, {78, 68}, {79, 68},
	}
	out := make([]Chopper, len(pairs))
	for i, p := range pairs {
		out[i] = Chopper{Wood: p[0], Fletch: p[1], Axe: woodcutplan.RuneAxe}
	}
	return out
}()

// seatYewCohort seats the live yew cohort one after another from the Seers bank, as a restart's
// logins do, and returns where each went.
func seatYewCohort(b *CampBoard, now time.Time) []int {
	at := make([]int, len(liveYewCohort))
	for i := range liveYewCohort {
		at[i] = b.Seat(fmt.Sprintf("yewacct%d", i), woodcutplan.Seers, false, now).Camp
	}
	return at
}

func newYewBoard() *CampBoard { return NewCampBoardFor(YewCamps, ReferenceYewChopper, BoardPlayers) }

func yewCampsNamed(names ...string) []Camp {
	var out []Camp
	for _, n := range names {
		for _, c := range YewCamps {
			if c.Name == n {
				out = append(out, c)
			}
		}
	}
	return out
}

// The ladder's own church camp is in the list exactly as woodcutplan.TIERS has it, so an account
// seated there chops precisely where it did before the board existed, and camp 0 is the one a
// nil board seats at.
func TestYewHomeCampIsTheLaddersOwn(t *testing.T) {
	homes := 0
	for _, c := range YewCamps {
		if !c.Home {
			continue
		}
		homes++
		if c.Tier() != woodcutplan.TIERS[c.Rung] {
			t.Errorf("%s: tier %+v differs from woodcutplan.TIERS[%d] %+v", c.Name, c.Tier(), c.Rung, woodcutplan.TIERS[c.Rung])
		}
	}
	if homes != 1 {
		t.Errorf("want exactly one Home camp, have %d", homes)
	}
	if !YewCamps[0].Home || YewCamps[0].Rung != YewRung {
		t.Errorf("camp 0 must be the ladder's own yew camp (a nil board seats there): %+v", YewCamps[0])
	}
}

// A camp is its rung somewhere else: same logs, experience and gates, its own place.
func TestYewCampTierIsTheYewRungElsewhere(t *testing.T) {
	for _, c := range YewCamps {
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

// Yew camps only, every name once, and none of them an overflow: a yew seat never changes what an
// account chops, which is the owner's ladder ("stay at yews until we can cut magic shortbows").
func TestYewCampsAreYewsAndNeverOverflow(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range YewCamps {
		if seen[c.Name] {
			t.Errorf("camp name %q is used twice", c.Name)
		}
		seen[c.Name] = true
		if c.Rung != YewRung {
			t.Errorf("%s: rung %d is not the yew rung %d", c.Name, c.Rung, YewRung)
		}
		if c.Overflow() {
			t.Errorf("%s: Overflow() is true for a yew camp, which would make the board refuse it when overflow is off", c.Name)
		}
	}
	// The maple board's willow camps are still the one overflow there is.
	for _, c := range MapleCamps {
		if want := c.Rung == WillowRung; c.Overflow() != want {
			t.Errorf("%s: Overflow() = %v, want %v", c.Name, c.Overflow(), want)
		}
	}
}

// The whole reach of a camp stays inside SeersRadius, so DecideWoodPhase always takes its Seers
// branch there and never the fare/ship road.
func TestEveryYewCampIsInsideTheSeersRadius(t *testing.T) {
	for _, c := range YewCamps {
		if reach := tile.Chebyshev(c.Anchor, woodcutplan.Seers) + c.Leash + 2; reach > woodcutplan.SeersRadius {
			t.Errorf("%s: anchor %v leash %d reaches %d tiles from the Seers bank, past SeersRadius %d",
				c.Name, c.Anchor, c.Leash, reach, woodcutplan.SeersRadius)
		}
	}
}

func TestUsesYewCampsOnlyOnTheYewRung(t *testing.T) {
	for i := range Ladder {
		if got := UsesYewCamps(i); got != (i == YewRung) {
			t.Errorf("UsesYewCamps(%d) = %v", i, got)
		}
		if UsesYewCamps(i) && UsesMapleCamps(i) {
			t.Errorf("rung %d is on both boards", i)
		}
	}
	if Ladder[YewRung].LogsID != YewLogsID {
		t.Errorf("YewRung %d points at the wrong logs", YewRung)
	}
}

// Every account of the live cohort is on the yew rung by the ladder's own rules, and cuts the yew
// bow its Fletching allows: the board's price assumes it fletches (a cut pack is a walk to the
// bank with 26 bows), and a reference that could not would be pricing the camps for someone else.
func TestTheYewCohortIsOnTheYewRungAndFletchesIt(t *testing.T) {
	for _, a := range liveYewCohort {
		snap := LadderSnapshot{WoodLevel: a.Wood, FletchLevel: a.Fletch, Target: "Yew", Gate: GateStartAt, Fletching: true}
		if got := TierIndex(snap); got != YewRung {
			t.Errorf("Woodcutting %d / Fletching %d is on rung %d, not the yew rung %d", a.Wood, a.Fletch, got, YewRung)
		}
		if _, ok := Choose(YewRung, a.Fletch, NormalShafts); !ok {
			t.Errorf("Fletching %d cannot cut yew logs into anything", a.Fletch)
		}
	}
	if _, ok := Choose(YewRung, ReferenceYewChopper.Fletch, NormalShafts); !ok {
		t.Errorf("the reference yew account (Fletching %d) cannot cut yew logs", ReferenceYewChopper.Fletch)
	}
	if got := TierIndex(LadderSnapshot{WoodLevel: ReferenceYewChopper.Wood, FletchLevel: ReferenceYewChopper.Fletch, Target: "Yew", Gate: GateStartAt, Fletching: true}); got != YewRung {
		t.Errorf("the reference yew account is on rung %d", got)
	}
}

// The yew board prices for a yew-rung account. NewCampBoard's maple reference (Woodcutting 65,
// Fletching 55) cannot cut yew logs at all and swings below the gate the owner set, so it would
// price every yew camp at 175 Woodcutting xp a log with no bow and no cutting time.
func TestYewBoardIsPricedForAYewAccount(t *testing.T) {
	yew, maple := newYewBoard(), NewCampBoard(YewCamps)
	for i, c := range YewCamps {
		bow, ok := Choose(YewRung, ReferenceYewChopper.Fletch, NormalShafts)
		if !ok {
			t.Fatal("the reference cannot fletch")
		}
		if want := Ladder[YewRung].Tier.XP + bow.XP(); math.Abs(yew.prices[i].XPPerLog-want) > 1e-9 {
			t.Errorf("%s: %.1f xp a log, want %.1f (Woodcutting plus the yew shortbow)", c.Name, yew.prices[i].XPPerLog, want)
		}
		if yew.prices[i].XPPerLog <= maple.prices[i].XPPerLog || yew.prices[i].Account <= 0 {
			t.Errorf("%s: yew board %+v is not richer than the maple reference's %+v", c.Name, yew.prices[i], maple.prices[i])
		}
	}
}

// ---- pricing ----

// THE MEASUREMENT THE MODEL WAS GIVEN. Live build 73ff4228, the 23-account yew cohort, every one
// of them at the ladder's one church camp (three yews): "wcfletch: cut N yew logs into N yew
// shortbow" summed per half hour from the run's log, 4-5 October 2026 -- 530 (00:00), 464 (00:30)
// and 576 (01:00): 1,570 logs in an hour and a half, 1,047 an hour, 45 an account against 180 for
// an account that has a tree to itself. The owner's 15-minute sample at 00:20 said the same thing
// from the other side: 7,296 "no yew in sight" lines, about 580 "every yew within reach is
// excluded", 331 cut batches and 1,127 bows an hour (49 an account).
//
// The model's supply for those three yews is 772 logs an hour (woodcutplan.CampSupply at the
// board's 270 players), so its status quo sits UNDER the measurement, by a quarter. The yew rows of
// the option table are therefore a floor: the church camp gave more than the model says it can,
// most likely because the live world is busier than BoardPlayers (scale_by_playercount shortens
// every respawn by (4000-players)/4000, and a world of about 1,250 would give the measured logs),
// which lifts every camp's supply together and moves hardly any camp against another.
func TestYewStatusQuoAgainstTheMeasurement(t *testing.T) {
	const measuredLogsPerHour = 1047.0
	at := make([]int, len(liveYewCohort))
	xp, rows := CohortXP(YewCamps, liveYewCohort, at, BoardPlayers)
	ratio := rows[0].Used / measuredLogsPerHour
	t.Logf("status quo: the model gives %.0f logs/h (%.0f xp/h), the live church camp gave %.0f: model/measured %.2f",
		rows[0].Used, xp, measuredLogsPerHour, ratio)
	if ratio < 0.6 || ratio > 1.05 {
		t.Errorf("the model's status quo is %.2f of the measurement: it must be a floor (at most the measurement) and within 40%% of it, or the option table is built on sand", ratio)
	}
}

// TestYewOptionsArePriced recomputes the option table in yewcamps.go's header, each row the board's
// own seating of the live yew cohort over that set of camps, priced with each account's own levels.
// A model or camp change that moves a row by more than 3% fails here, and the header's table has to
// be updated with it.
func TestYewOptionsArePriced(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	rows := []struct {
		name  string
		camps []Camp
		want  float64 // the header's number, xp/h for the cohort
	}{
		{"all 23 at the church yews (the live behaviour)", yewCampsNamed("Seers church yews"), 187e3},
		{"church + the road yew", yewCampsNamed("Seers church yews", "Catherby road yew"), 245e3},
		{"Catherby yews + the road yew (church left)", yewCampsNamed("Catherby yews", "Catherby road yew"), 422e3},
		{"church + Catherby yews", yewCampsNamed("Seers church yews", "Catherby yews"), 486e3},
		{"every camp (the default)", YewCamps, 510e3},
	}
	var statusQuo float64
	for i, r := range rows {
		b := NewCampBoardFor(r.camps, ReferenceYewChopper, BoardPlayers)
		at := make([]int, len(liveYewCohort))
		for j := range liveYewCohort {
			at[j] = b.Seat(fmt.Sprintf("yewacct%d", j), woodcutplan.Seers, false, now).Camp
		}
		got, cr := CohortXP(r.camps, liveYewCohort, at, BoardPlayers)
		var parts []string
		logs := 0.0
		for _, c := range cr {
			if c.Accounts > 0 {
				parts = append(parts, fmt.Sprintf("%s %d (%.0f of %.0f logs/h)", c.Camp, c.Accounts, c.Used, c.Supply))
				logs += c.Used
			}
		}
		if i == 0 {
			statusQuo = got
		}
		t.Logf("%-46s %9.0f xp/h, %6.0f each, %5.0f logs/h: %s", r.name, got, got/float64(len(liveYewCohort)), logs, strings.Join(parts, ", "))
		if d := math.Abs(got-r.want) / r.want; d > 0.03 {
			t.Errorf("%s: priced at %.0f xp/h, the header says %.0f (%.1f%% off) — update the table", r.name, got, r.want, 100*d)
		}
	}
	// And the premise: spreading is several times the status quo, in the model.
	if all := rows[len(rows)-1].want; all < 2.5*statusQuo {
		t.Errorf("the default prices at %.0f xp/h against %.0f for the status quo; the premise is more than 2.5 times", all, statusQuo)
	}
}

// ---- the board ----

// THE COHORT SPREADS. The live 23 seated one after another: every camp used, the church camp no
// longer holds them all, the shares end up even, and the cohort is priced at several times the
// status quo. overflow is false, as the bot passes it: a yew board has none to give.
func TestBoardSpreadsTheLiveYewCohort(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	b := newYewBoard()
	at := seatYewCohort(b, now)
	occ := b.Occupancy(now)
	for i, c := range YewCamps {
		if occ[i] == 0 {
			t.Errorf("%s got nobody", c.Name)
		}
	}
	if occ[0] > 10 {
		t.Errorf("the church camp still holds %d of 23", occ[0])
	}
	lo, hi := math.Inf(1), 0.0
	for i := range YewCamps {
		s := b.prices[i].Share(occ[i])
		lo, hi = math.Min(lo, s), math.Max(hi, s)
	}
	if hi > 1.8*lo {
		t.Errorf("shares range %.0f..%.0f xp/h: the fill is not even (%v)", lo, hi, occ)
	}
	built, _ := CohortXP(YewCamps, liveYewCohort, at, BoardPlayers)
	before, _ := CohortXP(YewCamps, liveYewCohort, make([]int, len(at)), BoardPlayers)
	if built < 2.5*before {
		t.Errorf("the board prices at %.0f xp/h against %.0f for the status quo; the premise is several times more", built, before)
	}
	t.Logf("23 accounts: %s; %.0f xp/h against %.0f", DescribeCamps(YewCamps, occ), built, before)
}

// A yew board never refuses a camp whatever the overflow flag says. Camp.Overflow used to be "not
// a maple camp", which on this board is every camp: with the flag off every account was seated at
// camp 0 and the board did nothing.
func TestYewBoardSeatsEverywhereWhateverTheOverflowFlagSays(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, overflow := range []bool{false, true} {
		b := newYewBoard()
		for i := range liveYewCohort {
			b.Seat(fmt.Sprintf("yewacct%d", i), woodcutplan.Seers, overflow, now)
		}
		used := 0
		for _, n := range b.Occupancy(now) {
			if n > 0 {
				used++
			}
		}
		if used != len(YewCamps) {
			t.Errorf("overflow=%v: %d of %d camps used (%v)", overflow, used, len(YewCamps), b.Occupancy(now))
		}
	}
}

func TestYewBoardsArePerWorld(t *testing.T) {
	if SharedYewBoardFor(1) == SharedYewBoardFor(2) {
		t.Error("worlds 1 and 2 share a yew board")
	}
	if SharedYewBoardFor(-1) != SharedYewBoardFor(0) {
		t.Error("a negative world should be world 0")
	}
	if SharedYewBoardFor(1) != SharedYewBoardFor(1) {
		t.Error("one world, two boards")
	}
	if got := SharedYewBoardFor(1).Camps(); len(got) != len(YewCamps) || got[0].Name != YewCamps[0].Name {
		t.Errorf("the shared yew board holds %v", got)
	}
	if SharedYewBoardFor(1) == SharedCampBoardFor(1) {
		t.Error("the yew board is the maple board")
	}
}

// A seat is kept and refreshed on the yew board exactly as on the maple board.
func TestYewSeatIsStableAndLeaves(t *testing.T) {
	b := newYewBoard()
	now := time.Unix(1_800_000_000, 0)
	first := b.Seat("Yew_Acct_1", woodcutplan.Seers, false, now)
	if !first.Moved || first.Share <= 0 || first.Camp != 0 {
		t.Fatalf("an empty board seats the first account at the ladder's own camp (nearest the bank): %+v", first)
	}
	for i := 1; i <= 6; i++ {
		again := b.Seat("yew acct 1", woodcutplan.Seers, false, now.Add(time.Duration(i)*time.Minute))
		if again.Camp != first.Camp || again.Moved {
			t.Fatalf("pass %d: seat moved %d -> %d (Moved=%v) with nothing else on the board", i, first.Camp, again.Camp, again.Moved)
		}
	}
	b.Leave("YEW ACCT 1")
	if occ := b.Occupancy(now.Add(6 * time.Minute)); occ[first.Camp] != 0 {
		t.Errorf("seat held after Leave: %v", occ)
	}
}
