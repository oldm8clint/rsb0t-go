package barcrawlplan

import (
	"fmt"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// renderCard builds the scroll exactly as content/scripts/general/scroll.rs2's
// scroll_barcrawl_card writes it, so every test below runs against the real rendered text
// rather than against a convenient approximation. The per-bar captions are that script's own.
var cardCaptions = map[int]string{
	733: "BlueMoon Inn",
	848: "Blurberry's Bar",
	735: "Dead Man's Chest",
	739: "Dragon Inn",
	738: "Flying Horse Inn",
	737: "Foresters Arms",
	731: "Jolly Boar Inn",
	568: "Karamja Spirits Bar",
	736: "Rising Sun Inn",
	734: "Rusty Anchor Inn",
}

// cardOrder is the order scroll_barcrawl_card writes the lines in (com_4..com_13), which is
// alphabetical by caption and NOT the order of Bars.
var cardOrder = []int{733, 848, 735, 739, 738, 737, 731, 568, 736, 734}

func renderCard(signed map[int]bool) []string {
	lines := []string{"@blu@The Official Alfred Grimhand Barcrawl!", ""}
	for _, id := range cardOrder {
		if signed[id] {
			lines = append(lines, fmt.Sprintf("@gre@%s - Completed!", cardCaptions[id]))
		} else {
			lines = append(lines, fmt.Sprintf("@red@%s - Not Completed...", cardCaptions[id]))
		}
	}
	return lines
}

func TestEveryBarHasACaptionInTheRenderedCard(t *testing.T) {
	// Guards the fixture itself: a Bars entry with no caption would silently never be
	// exercised by any test below.
	if len(cardOrder) != len(Bars) {
		t.Fatalf("the fixture renders %d lines for %d bars", len(cardOrder), len(Bars))
	}
	for _, b := range Bars {
		if _, ok := cardCaptions[b.ID]; !ok {
			t.Errorf("bar %d (%s) has no rendered caption in the fixture", b.ID, b.Line)
		}
	}
}

func TestParseCardOnAFreshCardReportsEveryBarOutstanding(t *testing.T) {
	got, ok := ParseCard(renderCard(nil))
	if !ok {
		t.Fatal("a freshly issued card did not parse as a barcrawl card")
	}
	if got.Done {
		t.Error("a fresh card reported done")
	}
	if len(got.Remaining) != len(Bars) {
		t.Fatalf("remaining = %d, want %d", len(got.Remaining), len(Bars))
	}
	if got.SignedCount() != 0 {
		t.Errorf("SignedCount = %d, want 0", got.SignedCount())
	}
}

func TestParseCardMatchesTheNegationNotTheHappyWord(t *testing.T) {
	// "Completed!" and "Not Completed..." both contain "completed", so a positive match
	// would report every bar signed from the very first read. This is the single highest-cost
	// mistake available in this parser.
	all := renderCard(nil)
	joined := strings.ToLower(strings.Join(all, " "))
	if !strings.Contains(joined, "completed") {
		t.Fatal("fixture no longer contains the happy word; this test is not testing what it claims")
	}
	got, _ := ParseCard(all)
	if got.Done || len(got.Remaining) != len(Bars) {
		t.Fatalf("an all-red card parsed as %d signed", got.SignedCount())
	}
}

func TestParseCardReadsEachBarIndependently(t *testing.T) {
	// Every bar gets its own turn at being the only signed one, which is what catches a
	// window that reaches into the NEXT line and reads its verdict by mistake.
	for _, bar := range Bars {
		got, ok := ParseCard(renderCard(map[int]bool{bar.ID: true}))
		if !ok {
			t.Fatalf("%s: did not parse", bar.Line)
		}
		if got.SignedCount() != 1 {
			t.Errorf("%s: SignedCount = %d, want exactly 1", bar.Line, got.SignedCount())
		}
		if !got.Signed(bar) {
			t.Errorf("%s: the one signed bar read as outstanding", bar.Line)
		}
		for _, other := range Bars {
			if other.ID != bar.ID && got.Signed(other) {
				t.Errorf("%s signed, but %s also read as signed", bar.Line, other.Line)
			}
		}
	}
}

func TestParseCardProgressivelySignsTheWholeTour(t *testing.T) {
	signed := map[int]bool{}
	for i, bar := range Bars {
		signed[bar.ID] = true
		got, ok := ParseCard(renderCard(signed))
		if !ok {
			t.Fatalf("after %d bars the card did not parse", i+1)
		}
		if got.SignedCount() != i+1 {
			t.Fatalf("after signing %d bars, SignedCount = %d", i+1, got.SignedCount())
		}
		wantDone := i == len(Bars)-1
		if got.Done != wantDone {
			t.Fatalf("after %d bars Done = %v, want %v", i+1, got.Done, wantDone)
		}
	}
}

func TestParseCardRejectsTextThatIsNotACard(t *testing.T) {
	// An empty read, a stale modal or somebody else's scroll must not be confused with "no
	// bars signed" — that difference is what stops the runner re-walking a finished tour.
	for _, lines := range [][]string{
		nil,
		{},
		{""},
		{"@blu@You have completed the Mysteries Of The Runes Quest!"},
		{"@str@I spoke to Duke Horacio and he showed me a strange talisman."},
	} {
		if _, ok := ParseCard(lines); ok {
			t.Errorf("non-card text %q parsed as a barcrawl card", lines)
		}
	}
}

func TestParseCardTreatsAMissingLineAsOutstanding(t *testing.T) {
	// A half-rendered scroll must read as "not yet signed", never as signed.
	partial := []string{"@blu@The Official Alfred Grimhand Barcrawl!", "@gre@BlueMoon Inn - Completed!"}
	got, ok := ParseCard(partial)
	if !ok {
		t.Fatal("a partial card did not parse")
	}
	if got.Done {
		t.Fatal("a card missing nine of its ten lines reported done")
	}
	if got.SignedCount() != 1 {
		t.Errorf("SignedCount = %d, want 1", got.SignedCount())
	}
}

func TestParseCardHandlesTheNeedleAtTheVeryEndOfTheText(t *testing.T) {
	// The window slice must not run past the end of the string.
	got, ok := ParseCard([]string{"barcrawl", "@gre@Rusty Anchor Inn"})
	if !ok {
		t.Fatal("did not parse")
	}
	// "rusty anchor" is present with no "not completed" after it, so it reads as signed.
	if !got.Signed(barByLine(t, "rusty anchor")) {
		t.Error("a truncated but green-looking line read as outstanding")
	}
}

func barByLine(t *testing.T, line string) Bar {
	t.Helper()
	for _, b := range Bars {
		if b.Line == line {
			return b
		}
	}
	t.Fatalf("no bar with line %q", line)
	return Bar{}
}

func TestNextBarPicksTheNearestOutstanding(t *testing.T) {
	// Standing in Port Sarim, the Rusty Anchor is next.
	got, ok := NextBar(Bars, tile.Tile{X: 3045, Z: 3258}, true)
	if !ok || got.Line != "rusty anchor" {
		t.Fatalf("from Port Sarim got %q (%v)", got.Line, ok)
	}
	// Standing in Varrock, the Blue Moon is.
	got, ok = NextBar(Bars, tile.Tile{X: 3226, Z: 3400}, true)
	if !ok || got.Line != "bluemoon" {
		t.Fatalf("from Varrock got %q (%v)", got.Line, ok)
	}
}

func TestNextBarFallsBackToListOrderWithNoPosition(t *testing.T) {
	got, ok := NextBar(Bars, tile.Tile{}, false)
	if !ok || got.ID != Bars[0].ID {
		t.Fatalf("with no position got %q (%v), want the first listed bar", got.Line, ok)
	}
}

func TestNextBarReportsNothingWhenTheTourIsDone(t *testing.T) {
	if _, ok := NextBar(nil, tile.Tile{}, true); ok {
		t.Error("NextBar offered a bar from an empty remaining list")
	}
}

func TestNextBarDoesNotMutateItsInput(t *testing.T) {
	in := make([]Bar, len(Bars))
	copy(in, Bars)
	NextBar(in, tile.Tile{X: 3226, Z: 3400}, true)
	for i := range in {
		if in[i].ID != Bars[i].ID {
			t.Fatalf("NextBar reordered the caller's slice at %d", i)
		}
	}
}

func TestNextBarIsDeterministicOnATie(t *testing.T) {
	a := Bar{ID: 9, Line: "a", Tile: tile.Tile{X: 10, Z: 0}}
	b := Bar{ID: 2, Line: "b", Tile: tile.Tile{X: 0, Z: 10}}
	here := tile.Tile{}
	for i := 0; i < 20; i++ {
		got, _ := NextBar([]Bar{a, b}, here, true)
		if got.ID != 2 {
			t.Fatalf("tie broken to %d, want the lower id 2", got.ID)
		}
	}
}

func TestTooDrunkIsTheFinishedStateNotAFailedRead(t *testing.T) {
	if !TooDrunk.MatchString("You are too drunk to be able to read the barcrawl card") {
		t.Fatal("TooDrunk did not match the content's own refusal line")
	}
	if TooDrunk.MatchString("You need to have completed the Rune Mysteries Quest to use this feature.") {
		t.Error("TooDrunk matched an unrelated message")
	}
}

func TestReadGreetingOnlyCompleteMeansComplete(t *testing.T) {
	// Reading anything else as "already done" sends the bot at a gate that will not open.
	cases := []struct {
		name  string
		lines []string
		want  GuardVerdict
		ok    bool
	}{
		{"complete", []string{"'Ello friend."}, VerdictComplete, true},
		{"complete, colour tagged", []string{"@yel@'Ello friend.@whi@"}, VerdictComplete, true},
		{"not started", []string{"Oi, whaddya want?"}, VerdictRetry, true},
		{"in progress", []string{"So, how's the Barcrawl coming along?"}, VerdictRetry, true},
		{"nothing said", nil, VerdictRetry, false},
		{"empty line", []string{"", "  "}, VerdictRetry, false},
	}
	for _, c := range cases {
		got, ok := ReadGreeting(c.lines)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: ReadGreeting = %v,%v; want %v,%v", c.name, got, ok, c.want, c.ok)
		}
	}
}

// The progress greeting is the only line that says "the crawl is under way": with no card in the pack and none handed
// over, it is the guard's own proof that the card is in the bank (barbarian_outpost_guard.rs2 reissues a card whenever
// neither the pack nor the bank holds one). The live line, verbatim from the 15 Oct 2026 fleet log, must read as it.
func TestCrawlUnderwayIsTheProgressGreetingAndNothingElse(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  bool
	}{
		{"the live line", []string{"Barbarian guard So, how's the Barcrawl coming along? Click here to continue"}, true},
		{"plain", []string{"So, how's the Barcrawl coming along?"}, true},
		{"colour tagged", []string{"@yel@So, how's the Barcrawl coming along?@whi@"}, true},
		{"not started", []string{"Oi, whaddya want?"}, false},
		{"complete", []string{"'Ello friend."}, false},
		{"the lost-card reply", []string{"What are you like?|You're gonna have to start all over now."}, false},
		{"nothing said", nil, false},
	}
	for _, c := range cases {
		if got := CrawlUnderway(c.lines); got != c.want {
			t.Errorf("%s: CrawlUnderway = %v, want %v", c.name, got, c.want)
		}
	}
	// The complete greeting must never read as under way, or a finished account would be sent to draw a card it has not got.
	if v, _ := ReadGreeting([]string{"'Ello friend."}); v != VerdictComplete || CrawlUnderway([]string{"'Ello friend."}) {
		t.Error("the complete greeting also reads as under way")
	}
}

func TestCardIDIsTheObjectTheNameResolvesTo(t *testing.T) {
	// content/pack/obj.pack: 455=barcrawl_card. A drift here would keep and draw some other object while the card is banked.
	if CardID != 455 {
		t.Fatalf("CardID = %d, want 455 (obj.pack barcrawl_card)", CardID)
	}
}

func TestBarPreferAnswersTheRisingSunAleMenu(t *testing.T) {
	// Nine bars go straight into the drink; the Rising Sun's barmaid offers her ale list
	// first, and an unanswered menu means the card never signs.
	opts := []string{"Hi, what ales are you serving?", "I'm doing Alfred Grimhand's Barcrawl."}
	got, ok := pickPreferred(opts, BarPrefer)
	if !ok || !strings.Contains(got, "Alfred Grimhand") {
		t.Fatalf("BarPrefer picked %q (%v) from the Rising Sun menu", got, ok)
	}
}

func TestGuardPreferAnswersBothOutpostMenus(t *testing.T) {
	first := []string{"I want to come through this gate.", "I want some money."}
	got, ok := pickPreferred(first, GuardPrefer)
	if !ok || got != "I want to come through this gate." {
		t.Fatalf("guard menu 1 picked %q (%v)", got, ok)
	}
	second := []string{"Hmm, yep you've got me there.", "Looks can be deceiving, I am in fact a barbarian."}
	got, ok = pickPreferred(second, GuardPrefer)
	if !ok || got != "Looks can be deceiving, I am in fact a barbarian." {
		t.Fatalf("guard menu 2 picked %q (%v); the other option abandons the crawl", got, ok)
	}
}

// pickPreferred is questplan.PickPreferred's rule, duplicated here so this package keeps no
// import of questplan — the barcrawl is not a quest-engine module and must not depend on one.
func pickPreferred(options, prefer []string) (string, bool) {
	for _, p := range prefer {
		for _, o := range options {
			if strings.Contains(strings.ToLower(o), strings.ToLower(p)) {
				return o, true
			}
		}
	}
	return "", false
}

func TestCoinFloatCoversEveryDrinkWithSlack(t *testing.T) {
	drinks := 0
	for _, b := range Bars {
		drinks += b.GP
	}
	if drinks != 208 {
		t.Errorf("the ten drinks total %d gp; the content scripts total 208", drinks)
	}
	// The first full live tour spent 448gp end to end, so a float that only just clears the
	// drinks is not a float at all -- the Karamja crossings are the bulk of the cost.
	const liveMeasuredSpend = 448
	if CoinFloat < liveMeasuredSpend {
		t.Errorf("CoinFloat %d is below the %d a real tour was measured spending", CoinFloat, liveMeasuredSpend)
	}
}

func TestBarIDsAreDistinctAndNamesAreNot(t *testing.T) {
	// The reason bars are found by ID: seven of the ten render the identical display name.
	seen := map[int]bool{}
	for _, b := range Bars {
		if seen[b.ID] {
			t.Errorf("duplicate bar id %d", b.ID)
		}
		seen[b.ID] = true
	}
	bartenders := 0
	for _, b := range Bars {
		if b.Npc == "Bartender" {
			bartenders++
		}
	}
	if bartenders < 2 {
		t.Fatal("the fixture no longer has colliding display names; the find-by-id rule needs re-justifying")
	}
}

func TestBarLinesAreDistinctSubstrings(t *testing.T) {
	// If one bar's needle were a substring of another's caption, ParseCard would read the
	// wrong line's verdict.
	for _, a := range Bars {
		for _, b := range Bars {
			if a.ID == b.ID {
				continue
			}
			caption := strings.ToLower(cardCaptions[b.ID])
			if strings.Contains(caption, a.Line) {
				t.Errorf("bar %q's needle appears inside %q's caption %q", a.Line, b.Line, caption)
			}
		}
	}
}
