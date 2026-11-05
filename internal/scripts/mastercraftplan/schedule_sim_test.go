package mastercraftplan

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// A SIMULATED THREE DAYS of the owner's schedule (5 Nov 2026): a toy fleet fills and drains the mule's bank; Decide picks the round,
// the round takes its length and changes the stock, the ledger is written the way the bot writes it. The test shows every round
// rotating, the clock rules holding (herbs once a day), Adamantite only when nothing else is due, and no round starved.

type simWorld struct {
	now   time.Time
	stock map[string]int
	led   *Ledger
	set   Settings
	// ran counts rounds by name; first and last when they ran; wait is the longest a trigger waited for its round.
	ran         map[string]int
	log         []string
	keysSince   time.Time
	sapSince    time.Time
	maxKeysWait time.Duration
	maxSapWait  time.Duration
	adamantBad  []string
	trawlerTime time.Duration
	steelMax    int
	// oreScale scales the Iron and Mithril ore inflow (a world short of ore leaves idle time for the Adamantite batch).
	oreScale float64
}

func newSimWorld(oreScale float64) *simWorld {
	set := heatSet()
	set.Trawler = true
	set.GemCutting, set.Herbs, set.Platebody = true, true, true
	set.CrystalChest = true
	set.GemThreshold = 20000 // the default: the bulk sapphire trigger (craftBulkAtSapphires) and the cuttable total decide
	set.SuperheatThreshold = DefaultSuperheatThreshold
	w := &simWorld{now: schedNow, oreScale: oreScale, set: set, led: &Ledger{}, ran: map[string]int{}, stock: map[string]int{
		"Coal": 400000, "Nature rune": 400000, "Iron ore": 6000, "Mithril ore": 2500, "Adamantite ore": 1500, "Steel bar": 6000,
		"Mithril bar": 800, "Adamantite bar": 400, "Uncut sapphire": 5000, "Chisel": 1, ChestKeyItem: 300,
		HerbName(herbGuam()): 300, "Raw shark": 0, "Raw manta ray": 0, "Raw sea turtle": 0,
	}}
	return w
}

func (w *simWorld) snap() Snapshot {
	m := map[string]int{}
	for k, v := range w.stock {
		m[k] = v
	}
	s := readySnapshot(m)
	s.Now = w.now
	s.CraftingLevel, s.MagicLevel, s.SmithingLevel = 99, 99, 99
	s.HerbloreLevel, s.HerbloreKnown = 70, true
	s.FireStaff = true
	s.OwnSuperheat = func(string) int { return 0 }
	s.OwnSmith = ownOf(nil)
	s.Ledger = w.led
	raw := m["Raw shark"] + m["Raw manta ray"] + m["Raw sea turtle"]
	s.Trawler = TrawlerInput{Food: raw, FoodKnown: true, FishingLevel: 90}
	return s
}

// hours passes time, with the fleet's inflow and outflow of the toy economy.
func (w *simWorld) pass(d time.Duration) {
	h := d.Hours()
	add := func(item string, perHour float64) { w.stock[item] += int(perHour * h) }
	take := func(item string, perHour float64) { w.stock[item] = max(0, w.stock[item]-int(perHour*h)) }
	add("Iron ore", 900*w.oreScale)
	add("Mithril ore", 120*w.oreScale)
	add("Adamantite ore", 80)
	add("Uncut sapphire", 900)
	add(ChestKeyItem, 55)
	add(HerbName(herbGuam()), 90)
	take("Steel bar", 700) // the cannonball fleet
	take("Raw shark", 150) // the mule's cooks
	take("Raw manta ray", 150)
	take("Raw sea turtle", 150)
	w.now = w.now.Add(d)
}

func (w *simWorld) step() {
	s := w.snap()
	TrawlerCycleBefore(w.set, s.Trawler, w.led, w.now)
	d := DecideWithChest(w.set, s, ChestInput{})
	TrawlerCycleAfter(w.set, w.led, w.now, d)
	// How long the triggers have been waiting.
	if w.stock[ChestKeyItem] >= 1000 && w.keysSince.IsZero() {
		w.keysSince = w.now
	}
	if w.stock["Uncut sapphire"] >= w.set.BulkAtSapphires && w.sapSince.IsZero() {
		w.sapSince = w.now
	}
	name := string(d.Mode)
	if d.Mode == ModeSuperheat {
		name = "heat-" + d.Heat.Name
	}
	w.ran[name]++
	rec := func(format string, a ...interface{}) {
		w.log = append(w.log, fmt.Sprintf("%s %-14s %s", w.now.Format("Mon 15:04"), name, fmt.Sprintf(format, a...)))
	}
	switch d.Mode {
	case ModeTrawler:
		// One game (a quarter hour) restocks 75 of each raw fish.
		rec("raw %d", s.Trawler.Food)
		for _, f := range []string{"Raw shark", "Raw manta ray", "Raw sea turtle"} {
			w.stock[f] += 100
		}
		w.trawlerTime += 15 * time.Minute
		w.pass(15 * time.Minute)
	case ModeGems:
		if !w.sapSince.IsZero() {
			w.maxSapWait = max(w.maxSapWait, w.now.Sub(w.sapSince))
			w.sapSince = time.Time{}
		}
		rec("cut %d", w.stock["Uncut sapphire"])
		w.stock["Uncut sapphire"] = 0
		w.pass(40 * time.Minute)
	case ModeChest:
		if !w.keysSince.IsZero() {
			w.maxKeysWait = max(w.maxKeysWait, w.now.Sub(w.keysSince))
			w.keysSince = time.Time{}
		}
		rec("looted %d keys", w.stock[ChestKeyItem])
		w.stock[ChestKeyItem] = 0
		w.pass(90 * time.Minute)
	case ModeHerbs:
		rec("cleaned %d", w.stock[HerbName(herbGuam())])
		w.stock[HerbName(herbGuam())] = 0
		w.led.Sched.NoteEnd(KeyHerbRound, w.now.Add(30*time.Minute))
		w.pass(30 * time.Minute)
	case ModePlatebody:
		bars := w.stock["Mithril bar"] + w.stock["Adamantite bar"]
		rec("%d bars", bars)
		w.stock["Mithril bar"], w.stock["Adamantite bar"] = 0, 0
		w.led.PauseForLimit(ModePlatebody, w.now.Add(time.Hour), w.set)
		w.pass(time.Hour)
	case ModeSuperheat:
		switch d.Heat.Name {
		case PhaseSteel:
			rec("steel %d -> 10000+", s.Sched.Steel)
			// 2,000 an hour until the stock is at the stop.
			for w.stock["Steel bar"] < 10000 && w.stock["Iron ore"] >= 100 {
				w.stock["Steel bar"] += 1000
				w.stock["Iron ore"] -= 1000
				w.stock["Iron ore"] = max(0, w.stock["Iron ore"])
				w.pass(30 * time.Minute)
			}
		case PhaseMithril:
			n := min(w.stock["Mithril ore"], 2000)
			rec("%d bars", n)
			w.stock["Mithril ore"] -= n
			w.stock["Mithril bar"] += n
			w.led.Sched.PauseHeat(PhaseMithril, w.now.Add(time.Hour), w.set)
			w.pass(time.Hour)
		case PhaseAdamant:
			// Adamantite is the last round: nothing else may be due at this moment.
			if w.stock[ChestKeyItem] >= 1000 || w.stock["Uncut sapphire"] >= w.set.BulkAtSapphires {
				w.adamantBad = append(w.adamantBad, w.now.Format("Mon 15:04"))
			}
			n := min(w.stock["Adamantite ore"], 2000)
			rec("%d bars", n)
			w.stock["Adamantite ore"] -= n
			w.stock["Adamantite bar"] += n
			w.led.Sched.PauseHeat(PhaseAdamant, w.now.Add(time.Hour), w.set)
			w.pass(time.Hour)
		}
	default:
		w.pass(5 * time.Minute)
	}
	w.steelMax = max(w.steelMax, w.stock["Steel bar"])
	w.led.Sched.NoteRun(RotationKey(d), w.now)
}

func TestSimulatedDayTheRoundsRotateAndNothingStarves(t *testing.T) {
	runSimulatedDays(t, 1, []string{"trawler", "heat-steel", "heat-mithril", "gems", "herbs", "chest", "platebody"})
}

// With the Steel and Mithril ore short, the account has idle time and the last round of all, the Adamantite batch, runs: an hour on,
// an hour off.
func TestSimulatedDaysAdamantiteRunsWhenNothingElseIsDue(t *testing.T) {
	w := runSimulatedDays(t, 0.02, []string{"trawler", "gems", "herbs", "chest", "heat-adamant"})
	if n := w.ran["heat-adamant"]; n < 3 {
		t.Errorf("only %d Adamantite batches in 72 hours", n)
	}
}

func runSimulatedDays(t *testing.T, oreScale float64, mustRun []string) *simWorld {
	w := newSimWorld(oreScale)
	end := w.now.Add(72 * time.Hour)
	for steps := 0; w.now.Before(end) && steps < 5000; steps++ {
		w.step()
	}
	var names []string
	for k := range w.ran {
		names = append(names, k)
	}
	sort.Strings(names)
	var sum []string
	for _, k := range names {
		sum = append(sum, fmt.Sprintf("%s=%d", k, w.ran[k]))
	}
	t.Logf("72 simulated hours: %s; trawler aboard %s", strings.Join(sum, " "), w.trawlerTime)
	for _, l := range w.log {
		if !strings.Contains(l, "trawler") {
			t.Log(l)
		}
	}
	for _, want := range mustRun {
		if w.ran[want] == 0 {
			t.Errorf("%s never ran in 72 hours (starved): %s", want, strings.Join(sum, " "))
		}
	}
	// Herbs about once a day: three days, at most three (never the 20+ a 1,000-guam threshold alone would give).
	if w.ran["herbs"] > 3 {
		t.Errorf("herbs ran %d times in 3 days", w.ran["herbs"])
	}
	// The trawler does not take the whole time: it rests an hour after each 3 hour block.
	if frac := w.trawlerTime.Hours() / 72; frac > 0.8 {
		t.Errorf("the trawler held the account %.0f%% of the time", frac*100)
	}
	// A trigger waits for its round no more than a trawler block and its rest (4 hours) plus the round in front of it.
	if w.maxKeysWait > 6*time.Hour {
		t.Errorf("the keys waited %s for the chest", w.maxKeysWait)
	}
	if w.maxSapWait > 6*time.Hour {
		t.Errorf("the sapphires waited %s for the gem round", w.maxSapWait)
	}
	if len(w.adamantBad) > 0 {
		t.Errorf("Adamantite ran while another round was due: %v", w.adamantBad)
	}
	if oreScale == 1 && w.steelMax < 10000 {
		t.Errorf("Steel starved: never reached its 10,000 (best %d)", w.steelMax)
	}
	return w
}
