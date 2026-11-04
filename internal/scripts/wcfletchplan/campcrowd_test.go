package wcfletchplan

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

var crowdT0 = time.Unix(1_800_000_000, 0)

// A reading is the busiest the camp has been in the window: a lower reading made later does not
// cancel a higher one that is still inside it, and the higher one leaves exactly when the window
// does.
func TestCrowdKeepsTheBusiestReadingInTheWindow(t *testing.T) {
	b := newYewBoard()
	b.ObserveCrowd(0, 5, crowdT0)
	b.ObserveCrowd(0, 1, crowdT0.Add(2*time.Minute))
	if got := b.Crowd(0, crowdT0.Add(2*time.Minute)); got != 5 {
		t.Errorf("two minutes in, the busiest reading is 5, got %d", got)
	}
	if got := b.Crowd(0, crowdT0.Add(CrowdWindow)); got != 5 {
		t.Errorf("a reading is still in the window at exactly CrowdWindow: want 5, got %d", got)
	}
	if got := b.Crowd(0, crowdT0.Add(CrowdWindow+time.Second)); got != 1 {
		t.Errorf("past the window the 5 is gone and the later 1 stands, got %d", got)
	}
	if got := b.Crowd(1, crowdT0.Add(time.Minute)); got != 0 {
		t.Errorf("another camp has no reading: %d", got)
	}
	if got := b.Crowds(crowdT0.Add(time.Minute)); len(got) != len(YewCamps) || got[0] != 5 || got[1] != 0 || got[2] != 0 {
		t.Errorf("Crowds = %v", got)
	}
}

// With nobody left at the camp to look, the last reading stands until CrowdTTL after it was taken
// and then goes: a crowd nobody has seen for five minutes is not one the board can price.
func TestCrowdLastReadingStandsUntilTheTTL(t *testing.T) {
	b := newYewBoard()
	b.ObserveCrowd(1, 4, crowdT0)
	for _, c := range []struct {
		after time.Duration
		want  int
	}{
		{time.Minute, 4},
		{CrowdWindow, 4},
		{CrowdWindow + time.Second, 4}, // past the window: the last reading stands
		{CrowdTTL - time.Second, 4},
		{CrowdTTL, 4},
		{CrowdTTL + time.Second, 0},
		{time.Hour, 0},
	} {
		if got := b.Crowd(1, crowdT0.Add(c.after)); got != c.want {
			t.Errorf("%s after the only reading: %d, want %d", c.after, got, c.want)
		}
	}
	// A reading of nobody is a reading too: after the window it IS the last thing seen.
	b.ObserveCrowd(1, 0, crowdT0.Add(time.Minute))
	if got := b.Crowd(1, crowdT0.Add(time.Minute+time.Second)); got != 4 {
		t.Errorf("a quieter glance does not cancel a busier one inside the window: %d", got)
	}
	if got := b.Crowd(1, crowdT0.Add(CrowdWindow+time.Second)); got != 0 {
		t.Errorf("once the 4 has left the window the 0 taken a minute later is what stands: %d", got)
	}
}

// Fifteen-second readings from a camp's worth of accounts for an hour keep a handful of entries,
// not thousands: that is the point of a sliding-window maximum.
func TestCrowdReadingsStayBounded(t *testing.T) {
	b := newYewBoard()
	for i := 0; i < 24_000; i++ {
		now := crowdT0.Add(time.Duration(i) * 150 * time.Millisecond) // 23 accounts every 15 s is 1.5 a second; this is faster
		b.ObserveCrowd(0, (i/97)%9, now)
	}
	if n := len(b.crowds[0].peaks); n > 10 {
		t.Errorf("%d peaks held after 24,000 readings, want at most one per distinct value", n)
	}
	if got := b.Crowd(0, crowdT0.Add(time.Hour)); got < 0 || got > 8 {
		t.Errorf("Crowd = %d, outside the readings made", got)
	}
}

func TestCrowdIgnoresWhatItCannotUse(t *testing.T) {
	var nilBoard *CampBoard
	nilBoard.ObserveCrowd(0, 3, crowdT0)
	if nilBoard.Crowd(0, crowdT0) != 0 || nilBoard.Crowds(crowdT0) != nil {
		t.Error("a nil board reported a crowd")
	}
	b := newYewBoard()
	b.ObserveCrowd(-1, 3, crowdT0)
	b.ObserveCrowd(len(YewCamps), 3, crowdT0)
	b.ObserveCrowd(0, -7, crowdT0)
	for i := range YewCamps {
		if got := b.Crowd(i, crowdT0); got != 0 {
			t.Errorf("camp %d reads %d after readings outside the board and a negative one", i, got)
		}
	}
	if b.Crowd(99, crowdT0) != 0 || b.Crowd(-1, crowdT0) != 0 {
		t.Error("a camp outside the board has a crowd")
	}
}

// ---- what a reading does to a seat ----

// A new seat goes elsewhere when strangers are at the camp it would have taken, and comes back to
// it once they are forgotten.
func TestStrangersSendANewSeatElsewhere(t *testing.T) {
	calm := newYewBoard().Seat("a", woodcutplan.Seers, false, crowdT0)
	if calm.Camp != 0 || calm.Crowd != 0 {
		t.Fatalf("an empty board seats the first account at the church camp nearest the bank: %+v", calm)
	}

	b := newYewBoard()
	b.ObserveCrowd(0, 6, crowdT0)
	s := b.Seat("a", woodcutplan.Seers, false, crowdT0.Add(time.Second))
	if s.Camp == 0 {
		t.Fatalf("six strangers at the church yews and the account was still seated there: %+v", s)
	}
	if s.Crowd != 0 || s.Occupants != 1 {
		t.Errorf("the camp it went to has no strangers: %+v", s)
	}

	// Forgotten after CrowdTTL with nobody left to look: a new account takes the church camp again.
	later := crowdT0.Add(CrowdTTL + 2*time.Second)
	if got := b.Seat("b", woodcutplan.Seers, false, later); got.Camp != 0 {
		t.Errorf("after %s the strangers are forgotten and the church camp is the best again, got camp %d", CrowdTTL, got.Camp)
	}
}

// The strangers at a camp are counted in the share a seat there reports, and in its crowd.
func TestStrangersAreCountedInASeatsShare(t *testing.T) {
	alone := newYewBoard()
	alone.Pin("p", 0)
	base := alone.Seat("p", woodcutplan.Seers, false, crowdT0)

	b := newYewBoard()
	b.Pin("p", 0)
	b.ObserveCrowd(0, 3, crowdT0)
	got := b.Seat("p", woodcutplan.Seers, false, crowdT0.Add(time.Second))
	if got.Camp != 0 || got.Crowd != 3 || got.Occupants != 1 {
		t.Fatalf("pinned seat: %+v", got)
	}
	if want := b.prices[0].Share(1 + 3); got.Share != want || got.Share >= base.Share {
		t.Errorf("share %.0f, want the share of four at the camp %.0f (alone: %.0f)", got.Share, want, base.Share)
	}
}

// STRANGERS NEVER WALK A SEATED ACCOUNT OFF ITS CAMP. The a user request: "it should have kept tick
// manipulating the tree until there's really none available". A stranger at the camp the account is seated at is
// counted in what the seat reports, and in what a NEW account is told, and in nothing that moves this account.
func TestStrangersNeverMoveASeatedAccountOffItsCamp(t *testing.T) {
	b := newYewBoard()
	first := b.Seat("a", woodcutplan.Seers, false, crowdT0)
	if first.Camp != 0 {
		t.Fatalf("first seat: %+v", first)
	}
	// An hour of passes, the account itself seeing eight strangers every minute: well past PatchRebalanceAfter, many times.
	for m := time.Minute; m <= time.Hour; m += time.Minute {
		now := crowdT0.Add(m)
		b.ObserveCrowd(0, 8, now)
		s := b.Seat("a", woodcutplan.Seers, false, now)
		if s.Moved || s.Camp != 0 {
			t.Fatalf("%s in, eight strangers at its camp: moved to camp %d (Moved=%v)", m, s.Camp, s.Moved)
		}
		if s.Crowd != 8 {
			t.Fatalf("%s in: the seat reports %d strangers, want the 8 the board counts", m, s.Crowd)
		}
	}
	// A new account does step round the camp: the price of a seat there still includes them.
	if got := b.Seat("b", woodcutplan.Seers, false, crowdT0.Add(time.Hour)); got.Camp == 0 {
		t.Errorf("a new seat was put at the camp with eight strangers on it: %+v", got)
	}
}

// What still moves a seated account once its rebalance clock is up is OUR OWN seats: a camp our cohort has filled
// is left for a better one, and the camp it would go to is priced with the strangers at it, so the account does not
// walk into somebody else's crowd.
func TestRebalanceMovesForOurOwnSeatsAndNotIntoStrangers(t *testing.T) {
	// crowded seats "a" at the church camp, keeps it alive every minute as a live account's passes do, and puts n of our
	// own accounts there as live seats at the moment its rebalance clock comes round.
	crowded := func(n int) (*CampBoard, time.Time) {
		b := newYewBoard()
		if s := b.Seat("a", woodcutplan.Seers, false, crowdT0); s.Camp != 0 {
			t.Fatalf("first seat: %+v", s)
		}
		for m := time.Minute; m < PatchRebalanceAfter; m += time.Minute {
			if s := b.Seat("a", woodcutplan.Seers, false, crowdT0.Add(m)); s.Moved || s.Camp != 0 {
				t.Fatalf("%s in: the lone account moved (%+v)", m, s)
			}
		}
		at := crowdT0.Add(PatchRebalanceAfter)
		for i := 0; i < n; i++ {
			b.seats[normWho(fmt.Sprintf("rebalance_own_%d", i))] = patchSeat{patch: 0, since: at, seen: at}
		}
		return b, at
	}

	b, at := crowded(12)
	if s := b.Seat("a", woodcutplan.Seers, false, at); !s.Moved || s.Camp == 0 {
		t.Fatalf("twelve of our own accounts at its camp and the clock up: the account stayed (%+v)", s)
	}

	// The same twelve, and a crowd of strangers at every other camp that leaves none of them CampRebalanceGain times
	// better than the camp it is at: it stays rather than walk into them.
	b, at = crowded(12)
	for i := 1; i < len(YewCamps); i++ {
		b.ObserveCrowd(i, 60, at)
	}
	if s := b.Seat("a", woodcutplan.Seers, false, at); s.Moved || s.Camp != 0 {
		t.Errorf("every other camp thick with strangers: the account went to camp %d (Moved=%v)", s.Camp, s.Moved)
	}

	// Strangers at the camp it is at, on top of our twelve, change nothing about where it goes: it leaves for the camp
	// that is free, exactly as without them.
	b, at = crowded(12)
	b.ObserveCrowd(0, 30, at)
	if s := b.Seat("a", woodcutplan.Seers, false, at); !s.Moved || s.Camp == 0 {
		t.Errorf("thirty strangers at the camp stopped our own twelve from moving the account: %+v", s)
	}
}

// ---- who is ours ----

func TestOwnNamesAreKeptTheWayTheGameKeepsAccounts(t *testing.T) {
	NoteOwn("Crowdtest_Acct_163")
	for _, name := range []string{"crowdtest acct 163", "Crowdtest Acct 163", " CROWDTEST_ACCT_163 "} {
		if !IsOwn(name) {
			t.Errorf("IsOwn(%q) = false: the screen name PLAYER_INFO reports and the roster's username are one account", name)
		}
	}
	if IsOwn("Otherplayer_1") || IsOwn("somebodyelse") || IsOwn("") || IsOwn("   ") {
		t.Error("a different name was taken for ours")
	}
	NoteOwn("")
	NoteOwn("   ")
	if IsOwn("") {
		t.Error("the empty name was recorded")
	}
}

// Every board records the account it seats: a rung-1 account standing beside the church yews is
// found on the patch board, never on the yew board, and is still not a stranger there.
func TestSeatingRecordsTheAccountAsOurs(t *testing.T) {
	// Names unique to this run: the registry is process-wide, so a second pass (-count 2) would
	// find the first one's.
	tag := fmt.Sprint(time.Now().UnixNano())
	yew, patch, maple := "Seated_Yew_"+tag, "Seated_Patch_"+tag, "Seated_Maple_"+tag
	for _, n := range []string{yew, patch, maple} {
		if IsOwn(n) {
			t.Fatalf("%q is in the registry before anything seated it", n)
		}
	}
	newYewBoard().Seat(yew, woodcutplan.Seers, false, crowdT0)
	NewPatchBoard(nil).Seat(patch, woodcutplan.Seers, crowdT0)
	NewCampBoard(nil).Seat(maple, woodcutplan.Seers, true, crowdT0)
	for _, n := range []string{yew, patch, maple} {
		if !IsOwn(n) {
			t.Errorf("%q was seated and is not ours", n)
		}
	}
}

// ---- when every camp is too crowded ----

func TestStandingOfAnEmptyBoardIsUncontended(t *testing.T) {
	st := newYewBoard().Standing(crowdT0)
	if st.Crowded() || st.Observed() || st.Ratio != 1 || st.Camp < 0 || st.Share <= 0 {
		t.Errorf("an empty board: %+v", st)
	}
	var nilBoard *CampBoard
	if st := nilBoard.Standing(crowdT0); st.Crowded() || st.Camp != -1 {
		t.Errorf("a nil board: %+v", st)
	}
}

// Crowded means EVERY camp: strangers at two of three camps leave the third worth a seat.
func TestStandingIsCrowdedOnlyWhenEveryCampIs(t *testing.T) {
	b := newYewBoard()
	b.ObserveCrowd(0, 40, crowdT0)
	b.ObserveCrowd(1, 40, crowdT0)
	st := b.Standing(crowdT0)
	if st.Crowded() || st.Camp != 2 {
		t.Errorf("two camps crowded, the road yew free: %+v", st)
	}
	if !st.Observed() || st.Strangers != 80 {
		t.Errorf("strangers: %+v", st)
	}
	b.ObserveCrowd(2, 40, crowdT0)
	st = b.Standing(crowdT0)
	if !st.Crowded() || st.Ratio >= CrowdedBelow {
		t.Errorf("every camp crowded: %+v", st)
	}
	// And it passes: the strangers are forgotten and nobody of ours was ever there.
	if st := b.Standing(crowdT0.Add(CrowdTTL + time.Second)); st.Crowded() || st.Observed() {
		t.Errorf("after the readings expire: %+v", st)
	}
}

// Our own seats crowd a board as strangers do: the share a new seat would get is counted over both.
func TestStandingCountsSeatsAndStrangersTogether(t *testing.T) {
	b := newYewBoard()
	for i := 0; i < 60; i++ {
		b.Seat(fmt.Sprintf("standing%d", i), woodcutplan.Seers, false, crowdT0)
	}
	st := b.Standing(crowdT0)
	if st.Seated != 60 || !st.Crowded() {
		t.Errorf("sixty accounts on three camps that feed about seventeen: %+v", st)
	}
}

// A board with nothing priced on it (a camp with no trees has no supply) cannot say one seat is
// worth less than another, so it is never called crowded, whatever it is told about strangers.
func TestStandingOfABoardWithNothingPricedIsNotCrowded(t *testing.T) {
	bare := []Camp{{Name: "no trees", Rung: YewRung, Loc: "Yew", Anchor: tile.Tile{X: 2709, Z: 3462}, Leash: 2, Trees: 0, Steps: 10}}
	b := NewCampBoardFor(bare, ReferenceYewChopper, BoardPlayers)
	b.ObserveCrowd(0, 30, crowdT0)
	if st := b.Standing(crowdT0); st.Crowded() || st.Ratio != 1 {
		t.Errorf("a board with no supply anywhere: %+v", st)
	}
}

func TestCrowdWatchSaysSoOnceAfterTenMinutesThenEveryThirty(t *testing.T) {
	var w CrowdWatch
	if w.Step(false, crowdT0) {
		t.Error("said so about a board that is not crowded")
	}
	for m := time.Duration(0); m < CrowdAdviceAfter; m += time.Minute {
		if w.Step(true, crowdT0.Add(m)) {
			t.Fatalf("said so after %s, before %s", m, CrowdAdviceAfter)
		}
	}
	if !w.Step(true, crowdT0.Add(CrowdAdviceAfter)) {
		t.Fatal("did not say so after the board stayed crowded for the whole period")
	}
	for m := CrowdAdviceAfter + time.Minute; m < CrowdAdviceAfter+CrowdAdviceEvery; m += time.Minute {
		if w.Step(true, crowdT0.Add(m)) {
			t.Fatalf("said so again %s after the first time", m-CrowdAdviceAfter)
		}
	}
	if !w.Step(true, crowdT0.Add(CrowdAdviceAfter+CrowdAdviceEvery)) {
		t.Error("did not say so again after CrowdAdviceEvery")
	}
}

// A board that stops being crowded starts the ten minutes again.
func TestCrowdWatchStartsOverWhenTheBoardEases(t *testing.T) {
	var w CrowdWatch
	w.Step(true, crowdT0)
	w.Step(true, crowdT0.Add(8*time.Minute))
	w.Step(false, crowdT0.Add(9*time.Minute))
	if w.Step(true, crowdT0.Add(12*time.Minute)) {
		t.Error("said so twelve minutes in with an easy minute at nine")
	}
	if w.Step(true, crowdT0.Add(21*time.Minute)) {
		t.Error("said so nine minutes after crowded came back")
	}
	if !w.Step(true, crowdT0.Add(22*time.Minute)) {
		t.Error("did not say so ten minutes after crowded came back")
	}
}

func boardsFor(boards map[int]*CampBoard) func(int) *CampBoard {
	return func(w int) *CampBoard { return boards[w] }
}

func crowdAll(b *CampBoard, n int) {
	for i := range YewCamps {
		b.ObserveCrowd(i, n, crowdT0)
	}
}

// The advice names the world whose board has the best seat, says when that is a guess (none of
// our accounts is there to see who else is), and stays quiet about a world that is only a little
// better.
func TestAdviseWorldNamesTheBestWorld(t *testing.T) {
	here, w2, w3 := newYewBoard(), newYewBoard(), newYewBoard()
	crowdAll(here, 40)
	crowdAll(w3, 40)
	adv := AdviseWorld(1, AdviceWorlds, boardsFor(map[int]*CampBoard{1: here, 2: w2, 3: w3}), crowdT0)
	if adv.Better != 2 {
		t.Fatalf("world 2 is empty and the others are crowded, got Better=%d (%+v)", adv.Better, adv.Standings)
	}
	if !adv.Standings[1].Crowded() || adv.Standings[2].Crowded() || adv.Standings[2].Observed() || !adv.Standings[3].Crowded() {
		t.Errorf("standings: %+v", adv.Standings)
	}
	if got := fmt.Sprint(adv.Worlds); got != "[1 2 3]" {
		t.Errorf("Worlds = %s", got)
	}
	line := adv.Describe()
	for _, want := range []string{"consider homeWorld 2", "xp/h against", "none of ours is seated there", "world 3 about"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q is missing from %q", want, line)
		}
	}
}

func TestAdviseWorldHasNothingToSayWhenNoWorldIsClearlyBetter(t *testing.T) {
	// Every world as crowded as this one.
	boards := map[int]*CampBoard{}
	for w := 1; w <= 3; w++ {
		boards[w] = newYewBoard()
		crowdAll(boards[w], 40)
	}
	adv := AdviseWorld(1, AdviceWorlds, boardsFor(boards), crowdT0)
	if adv.Better != 0 || !strings.HasPrefix(adv.Describe(), "no other world looks clearly better") {
		t.Errorf("Better=%d, line %q", adv.Better, adv.Describe())
	}

	// A world that is better, but by less than WorldHopGain, is not worth a hop.
	here, other := newYewBoard(), newYewBoard()
	crowdAll(here, 40)
	crowdAll(other, 34)
	adv = AdviseWorld(1, []int{2}, boardsFor(map[int]*CampBoard{1: here, 2: other}), crowdT0)
	hs, os := adv.Standings[1].Share, adv.Standings[2].Share
	if os <= hs || os >= WorldHopGain*hs {
		t.Fatalf("the test needs world 2 better than world 1 by under %.1fx: %.0f against %.0f", WorldHopGain, os, hs)
	}
	if adv.Better != 0 {
		t.Errorf("recommended world %d for a gain of only %.2fx", adv.Better, os/hs)
	}
}

// Two worlds equally good: the first listed wins, and the world asked about is never its own advice.
func TestAdviseWorldBreaksTiesByOrderAndNeverNamesItself(t *testing.T) {
	boards := map[int]*CampBoard{1: newYewBoard(), 2: newYewBoard(), 3: newYewBoard(), 4: nil}
	crowdAll(boards[1], 40)
	adv := AdviseWorld(1, []int{3, 2, 1, 3, 0, 4}, boardsFor(boards), crowdT0)
	if adv.Better != 3 {
		t.Errorf("worlds 3 and 2 are equally good and 3 was listed first: Better=%d", adv.Better)
	}
	if fmt.Sprint(adv.Worlds) != "[1 3 2 4]" {
		t.Errorf("Worlds = %v: here first, each candidate once, never world 0", adv.Worlds)
	}
	if adv.Standings[4].Camp != -1 {
		t.Errorf("a world with no board: %+v", adv.Standings[4])
	}
	// Asked about world 2, the same boards: world 1 is crowded and 3 is empty.
	if adv := AdviseWorld(2, AdviceWorlds, boardsFor(boards), crowdT0); adv.Better != 0 {
		t.Errorf("world 2 is empty, nothing is 1.5x better: Better=%d", adv.Better)
	}
}

func TestDescribeCampsWithCrowd(t *testing.T) {
	if got := DescribeCampsWithCrowd(YewCamps, make([]int, 3), make([]int, 3)); got != "nobody seated" {
		t.Errorf("empty: %q", got)
	}
	got := DescribeCampsWithCrowd(YewCamps, []int{9, 14, 0}, []int{2, 0, 4})
	want := "Catherby yews 14/7, Seers church yews 9/3 +2 others, Catherby road yew 0/1 +4 others"
	if got != want {
		t.Errorf("DescribeCampsWithCrowd = %q, want %q", got, want)
	}
	// Short slices are tolerated: a caller with only the seats.
	if got := DescribeCampsWithCrowd(YewCamps, []int{2}, nil); got != "Seers church yews 2/3" {
		t.Errorf("seats only: %q", got)
	}
}

// Every entry point is one lock: readings, seats, standings and leaves from many accounts at once
// do not tear the board (run with -race where cgo is available).
func TestCrowdAndSeatsFromManyAccountsAtOnce(t *testing.T) {
	b := newYewBoard()
	var wg sync.WaitGroup
	for g := 0; g < 24; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			who := fmt.Sprintf("concurrent%d", g)
			for i := 0; i < 200; i++ {
				now := crowdT0.Add(time.Duration(i) * time.Second)
				s := b.Seat(who, woodcutplan.Seers, false, now)
				b.ObserveCrowd(s.Camp, (g+i)%5, now)
				b.Standing(now)
				b.Crowds(now)
				if i%50 == 49 {
					b.Leave(who)
				}
			}
		}(g)
	}
	wg.Wait()
	if st := b.Standing(crowdT0.Add(time.Hour)); st.Camp < 0 || st.Seated != 0 {
		t.Errorf("an hour on, the board should have lost every seat: %+v", st)
	}
}
