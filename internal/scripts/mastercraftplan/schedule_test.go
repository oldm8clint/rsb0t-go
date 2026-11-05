package mastercraftplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/herbplan"
)

func herbGuam() herbplan.Herb { return herbplan.Herbs[0] }

var schedNow = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

// heatWorld is a snapshot where the superheat rounds are the only ones that can be due: Magic and Smithing high, a fire staff, the
// mule holding ore for each bar and plenty of coal and Natures.
func heatWorld(stock map[string]int) Snapshot {
	base := map[string]int{"Coal": 100000, "Nature rune": 100000}
	for k, v := range stock {
		base[k] = v
	}
	s := readySnapshot(base)
	s.Now = schedNow
	s.MagicLevel, s.SmithingLevel = 99, 99
	s.FireStaff = true
	s.OwnSuperheat = func(string) int { return 0 }
	s.OwnSmith = ownOf(nil)
	return s
}

func heatSet() Settings {
	set := orderSettings()
	set.Arrows, set.Herbs, set.Smith, set.Platebody, set.Staves, set.StoreOverflow = false, false, false, false, false, false
	set.GemCutting, set.BowStringing, set.Pearls, set.BuyBolts, set.BuyArrowTips = false, false, false, false, false
	set.CannonballTarget = 0
	return set
}

func TestScheduleDefaultsAreTheOwners(t *testing.T) {
	c := DefaultSettings().Sched
	if c.TrawlerBlock != 3*time.Hour || c.SteelStart != 10000 || c.SteelStop != 10000 || c.SteelMinCasts != 500 ||
		c.AdamantRound != time.Hour || c.HerbEvery != 24*time.Hour || c.OakWindow != 2*time.Hour || c.OakEvery != 24*time.Hour || c.ChestStartKeys != 1000 {
		t.Fatalf("%+v", c)
	}
	if err := DefaultSettings().Validate(); err != nil {
		t.Fatal(err)
	}
	set := DefaultSettings()
	set.Sched.SteelStart = 20000
	if set.Validate() == nil {
		t.Error("a Steel start above the stop is refused")
	}
	set = DefaultSettings()
	set.Sched.OakWindow = 25 * time.Hour
	if set.Validate() == nil {
		t.Error("an oak window longer than its day is refused")
	}
}

// STEEL FIRST (a user request): under 10,000 combined Steel bars, with 500 bars' worth of ore, coal and Natures; no craftSuperheatBars
// setting needed.
func TestSteelPhaseStartsUnderTheStartAndNeedsFiveHundredCasts(t *testing.T) {
	set := heatSet()
	if len(set.SuperheatBars) != 0 {
		t.Fatal("the live craftSuperheatBars setting is retired: the default is empty")
	}
	stock := map[string]int{"Iron ore": 3000, "Mithril ore": 3000, "Steel bar": 4000}
	d := Decide(set, heatWorld(stock))
	if d.Mode != ModeSuperheat || d.Heat.Name != PhaseSteel || d.Superheat.Bar.Name != "Steel bar" {
		t.Fatalf("Steel first although Mithril ore is there: %s %+v (%s)", d.Mode, d.Heat, d.Why)
	}
	if d.Heat.SteelAtStart != 4000 || d.Heat.SteelStop != 10000 || d.Heat.Budget != 0 {
		t.Errorf("phase %+v", d.Heat)
	}
	// At the stop: no Steel phase; the Mithril batch is next.
	stock["Steel bar"] = 10000
	if d := Decide(set, heatWorld(stock)); d.Heat.Name != PhaseMithril || d.Superheat.Bar.Name != "Mithril bar" || d.Heat.Budget != time.Hour {
		t.Fatalf("steel at its stop: %s %+v", d.Mode, d.Heat)
	}
	// 499 bars' worth of ore: under the 500 a new Steel phase needs, so Mithril goes.
	stock["Steel bar"], stock["Iron ore"] = 100, 499
	if d := Decide(set, heatWorld(stock)); d.Heat.Name != PhaseMithril {
		t.Fatalf("499 casts: %s %+v (%s)", d.Mode, d.Heat, d.Why)
	}
	stock["Iron ore"] = 500
	if d := Decide(set, heatWorld(stock)); d.Heat.Name != PhaseSteel {
		t.Fatalf("500 casts: %s %+v", d.Mode, d.Heat)
	}
	// Combined Steel counts this account's own bank too.
	s := heatWorld(stock)
	s.OwnSmith = ownOf(map[string]int{"Steel bar": 9900})
	if d := Decide(set, s); d.Heat.Name == PhaseSteel {
		t.Errorf("the banks together hold 10,000: %+v", d.Heat)
	}
}

func TestSteelPhaseStopsAtTheStopMidRound(t *testing.T) {
	p := HeatPhase{Name: PhaseSteel, SteelStop: 10000, SteelAtStart: 9400}
	if p.SteelReached(599) || !p.SteelReached(600) {
		t.Error("9,400 + 600 reaches 10,000")
	}
	if (HeatPhase{Name: PhaseMithril}).SteelReached(1 << 20) {
		t.Error("only the Steel phase has a stock stop")
	}
}

func TestSteelHysteresisWhenTheStartIsUnderTheStop(t *testing.T) {
	set := heatSet()
	set.Sched.SteelStart = 5000
	stock := map[string]int{"Iron ore": 3000, "Steel bar": 7000}
	s := heatWorld(stock)
	if d := Decide(set, s); d.Heat.Name == PhaseSteel {
		t.Fatalf("7,000 is not under the start 5,000: %+v", d.Heat)
	}
	s.Ledger.Sched.SteelActive = true
	if d := Decide(set, s); d.Heat.Name != PhaseSteel {
		t.Fatalf("a phase under way carries on up to the stop: %s %+v", d.Mode, d.Heat)
	}
	if !s.Ledger.Sched.SettleSteel(set, 10000, true) || s.Ledger.Sched.SteelActive {
		t.Error("the stop clears it")
	}
}

// The retired setting still restricts when it is left in place, so removing it is safe in either order.
func TestRetiredSuperheatBarsSettingStillRestricts(t *testing.T) {
	set := heatSet()
	stock := map[string]int{"Iron ore": 3000, "Mithril ore": 3000, "Steel bar": 12000}
	set.SuperheatBars = []string{"Steel bar"}
	d := Decide(set, heatWorld(stock))
	if d.Mode == ModeSuperheat {
		t.Fatalf("Steel only and Steel is full: %+v", d.Heat)
	}
	set.SuperheatBars = nil
	if d := Decide(set, heatWorld(stock)); d.Heat.Name != PhaseMithril {
		t.Fatalf("setting retired: Mithril goes: %s %+v", d.Mode, d.Heat)
	}
}

func TestMithrilBatchIsPausedPerPhase(t *testing.T) {
	set := heatSet()
	stock := map[string]int{"Mithril ore": 3000, "Adamantite ore": 3000, "Steel bar": 12000}
	s := heatWorld(stock)
	if d := Decide(set, s); d.Heat.Name != PhaseMithril {
		t.Fatalf("%+v", d.Heat)
	}
	s.Ledger.Sched.PauseHeat(PhaseMithril, s.Now, set)
	d := Decide(set, s)
	if d.Heat.Name != PhaseAdamant {
		t.Fatalf("Mithril paused an hour: Adamantite is the last round: %s %+v (%s)", d.Mode, d.Heat, d.Why)
	}
	s.Now = s.Now.Add(61 * time.Minute)
	if d := Decide(set, s); d.Heat.Name != PhaseMithril {
		t.Fatalf("after the pause Mithril is due again: %+v", d.Heat)
	}
	// Setting the pause is a setting.
	set.Sched.PauseMithril = 10 * time.Minute
	s.Ledger.Sched.PauseHeat(PhaseMithril, s.Now, set)
	if until, held := s.Ledger.Sched.HeatPaused(PhaseMithril, s.Now.Add(11*time.Minute)); held {
		t.Errorf("paused until %v", until)
	}
}

// ADAMANTITE LAST: it runs only when no other round is due.
func TestAdamantiteIsTheLastRound(t *testing.T) {
	set := heatSet()
	set.GemCutting = true
	stock := map[string]int{"Adamantite ore": 3000, "Steel bar": 12000, "Uncut sapphire": 25000, "Chisel": 1}
	s := heatWorld(stock)
	s.CraftingLevel = 99
	if d := Decide(set, s); d.Mode != ModeGems {
		t.Fatalf("the gem round first: %s (%s)", d.Mode, d.Why)
	}
	set.GemCutting = false
	d := Decide(set, s)
	if d.Mode != ModeSuperheat || d.Heat.Name != PhaseAdamant || d.Heat.Budget != time.Hour || d.Superheat.Bar.Name != "Adamantite bar" {
		t.Fatalf("nothing else due: %s %+v (%s)", d.Mode, d.Heat, d.Why)
	}
	// With the Mithril batch also due, Mithril is first.
	stock["Mithril ore"] = 1000
	if d := Decide(set, heatWorld(stock)); d.Heat.Name != PhaseMithril {
		t.Fatalf("%+v", d.Heat)
	}
	// Gold only with the gauntlets, in the Mithril tier.
	set.SuperheatGold = true
	g := heatWorld(map[string]int{"Gold ore": 2000, "Steel bar": 12000})
	if d := Decide(set, g); d.Mode == ModeSuperheat {
		t.Fatalf("gold without gauntlets: %+v", d.Heat)
	}
	g.GoldGauntlets = true
	if d := Decide(set, g); d.Heat.Name != PhaseMithril || d.Superheat.Bar.Name != "Gold bar" {
		t.Fatalf("gold with the gauntlets: %s %+v", d.Mode, d.Heat)
	}
}

func TestPlatebodyRoundNoLongerWaitsForAdamantite(t *testing.T) {
	set := heatSet()
	set.Platebody = true
	stock := map[string]int{"Adamantite ore": 3000, "Adamantite bar": 800, "Steel bar": 12000}
	s := heatWorld(stock)
	if d := Decide(set, s); d.Mode != ModePlatebody {
		t.Fatalf("ore left or not, the bars make platebodies: %s (%s)", d.Mode, d.Why)
	}
}

func TestHerbsRunOnceADay(t *testing.T) {
	set := heatSet()
	set.Herbs = true
	stock := map[string]int{HerbName(herbGuam()): 2000}
	s := heatWorld(stock)
	s.HerbloreLevel, s.HerbloreKnown = 60, true
	if d := Decide(set, s); d.Mode != ModeHerbs {
		t.Fatalf("never run: due: %s (%s)", d.Mode, d.Why)
	}
	s.Ledger.Sched.NoteEnd(KeyHerbRound, s.Now.Add(-23*time.Hour))
	d := Decide(set, s)
	if d.Mode == ModeHerbs || !strings.Contains(d.Why, "herbs: the last round ended") {
		t.Fatalf("23 hours: %s (%s)", d.Mode, d.Why)
	}
	s.Ledger.Sched.NoteEnd(KeyHerbRound, s.Now.Add(-24*time.Hour))
	if d := Decide(set, s); d.Mode != ModeHerbs {
		t.Fatalf("24 hours: %s (%s)", d.Mode, d.Why)
	}
	// Herbs in this account's own bank are a resume, whatever the clock.
	s.Ledger.Sched.NoteEnd(KeyHerbRound, s.Now.Add(-time.Hour))
	s.OwnHerbs = map[int32]int{herbGuam().UnidID: 40}
	if d := Decide(set, s); d.Mode != ModeHerbs || !d.Resume {
		t.Fatalf("a cut-short round resumes: %s (%s)", d.Mode, d.Why)
	}
}

func TestOakWindowIsTwoHoursADayAndSurvivesRestarts(t *testing.T) {
	set := DefaultSettings()
	var l ScheduleLedger
	t0 := schedNow
	if !l.OaksDue(set, t0, 0) {
		t.Fatal("never chopped: due")
	}
	l.NoteOaks(set, t0, 90*time.Minute)
	if !l.OaksDue(set, t0.Add(time.Hour), 29*time.Minute) {
		t.Error("119 of 120 minutes used")
	}
	if l.OaksDue(set, t0.Add(time.Hour), 30*time.Minute) {
		t.Error("the 120 minutes are used")
	}
	// Saved and read back (the side store): the window is the same.
	l2 := ScheduleLedger{OakDay: l.OakDay, OakUsed: l.OakUsed}
	l2.NoteOaks(set, t0.Add(2*time.Hour), 30*time.Minute)
	if l2.OaksDue(set, t0.Add(3*time.Hour), 0) {
		t.Error("two hours used in the day")
	}
	if !l2.OaksDue(set, t0.Add(24*time.Hour), 0) {
		t.Error("a new day, a new window")
	}
	if !l2.RollOakDay(set, t0.Add(24*time.Hour)) || l2.OakUsed != 0 {
		t.Error("the roll starts the window afresh")
	}
	set.Sched.OakWindow = 0
	if l2.OaksDue(set, t0.Add(48*time.Hour), 0) {
		t.Error("a zero window turns the oaks off")
	}
}

func TestTipsAreDueOnlyAtAHundredThousand(t *testing.T) {
	set := DefaultSettings()
	if a, b := TipsDue(set, 99999, 500000, 99999, 500000); a || b {
		t.Error("99,999 is not due")
	}
	if a, b := TipsDue(set, 100000, 100000, 100000, 100000); !a || !b {
		t.Error("100,000 is due")
	}
	if a, _ := TipsDue(set, 200000, 50000, 0, 0); a {
		t.Error("arrows to tip are limited by the headless arrows")
	}
}

func TestNatureCampaignReadsTheCombinedBanks(t *testing.T) {
	s := Snapshot{Stock: stockMap(map[string]int{"Nature rune": 40000})}
	if combinedNature(s) != 40000 {
		t.Error("the mule's count without a fleet reading")
	}
	s.Sched = ScheduleInput{Nature: 250000, NatureKnown: true}
	if combinedNature(s) != 250000 {
		t.Error("the combined reading wins")
	}
}

func TestScheduleSettingsDecode(t *testing.T) {
	s, err := DecodeSettings(map[string]interface{}{KeyTrawlerBlockMinutes: 120, KeyTrawlerRestMinutes: 0, KeySteelStart: 5000, KeySteelStop: 10000,
		KeySteelMinCasts: 300, KeyAdamantRoundMinutes: 30, KeyPauseMithrilMinutes: 20, KeyPauseAdamantMinutes: 25, KeyPausePlatebodyMinutes: 35,
		KeyHerbEveryMinutes: 600, KeyOakMinutes: 60, KeyOakEveryMinutes: 720, KeyChestStartKeys: 2000})
	if err != nil {
		t.Fatal(err)
	}
	c := s.Sched
	if c.TrawlerBlock != 2*time.Hour || c.TrawlerRest != 0 || c.SteelStart != 5000 || c.SteelMinCasts != 300 || c.AdamantRound != 30*time.Minute ||
		c.PauseMithril != 20*time.Minute || c.PauseAdamant != 25*time.Minute || c.PausePlatebody != 35*time.Minute ||
		c.HerbEvery != 10*time.Hour || c.OakWindow != time.Hour || c.OakEvery != 12*time.Hour || c.ChestStartKeys != 2000 {
		t.Fatalf("%+v", c)
	}
}
