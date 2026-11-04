package mastercraftplan

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/runecraftplan"
)

// campaignSet is the defaults with the round and the campaign on and the switch off (they are alternatives).
func campaignSet() Settings {
	s := DefaultSettings()
	s.Rune.On, s.Rune.Campaign = true, true
	return s
}

// campaignSnap is a Runecrafting 44 account at the mule's post: the mule holds 72 Natures, 12,400 essence and a talisman, six carriers
// stand by.
func campaignSnap(mod func(s *Snapshot)) Snapshot {
	return natureSnap(func(s *Snapshot) {
		s.Stock = stockMap(map[string]int{"Nature rune": 72, "Rune essence": 12400, "Nature talisman": 7, "Coins": 773226})
		s.Rune.Standby = 6
		if mod != nil {
			mod(s)
		}
	})
}

func TestCampaignDefaults(t *testing.T) {
	r := DefaultSettings().Rune
	if r.Campaign || r.StartBelow != 1000 || r.StopAbove != 60000 || r.Target != 100000 || r.MinEssence != 5000 || r.MinRunners != 3 ||
		r.HandOverAt != 14000 || r.HandOverMin != 5000 || r.CourierWait != 2*time.Hour || r.Pause != 45*time.Minute ||
		!r.BuyAll || r.MuleCoinFloor != 10000 || r.BuyTrip != 0 {
		t.Fatalf("defaults: %+v", r)
	}
	// The defaults validate (a campaign off changes nothing for a fleet that does not use it).
	if _, err := DecodeSettings(map[string]interface{}{KeyRune: true}); err != nil {
		t.Fatal(err)
	}
}

func TestCampaignSettingsDecode(t *testing.T) {
	s, err := DecodeSettings(map[string]interface{}{
		KeyRune: true, KeyRuneCampaign: "true", KeyRuneStartBelow: "1500", KeyRuneStopAbove: 70000.0, KeyRuneTarget: "90000", KeyRuneMinEssence: 4000,
		KeyRuneMinRunners: "4", KeyRuneHandOverAt: 15000, KeyRuneHandOverMin: "6000", KeyRuneCourierWait: 90, KeyRunePause: "30",
		KeyRuneBuyAll: "false", KeyRuneMuleCoinFloor: 20000, KeyRuneBuyTrip: "3000000",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := s.Rune
	if !r.Campaign || r.StartBelow != 1500 || r.StopAbove != 70000 || r.Target != 90000 || r.MinEssence != 4000 || r.MinRunners != 4 ||
		r.HandOverAt != 15000 || r.HandOverMin != 6000 || r.CourierWait != 90*time.Minute || r.Pause != 30*time.Minute || r.BuyAll || r.MuleCoinFloor != 20000 || r.BuyTrip != 3_000_000 {
		t.Fatalf("decoded %+v", r)
	}
}

func TestCampaignSettingsAreStrict(t *testing.T) {
	on := map[string]interface{}{KeyRune: true, KeyRuneCampaign: true}
	with := func(extra map[string]interface{}) map[string]interface{} {
		m := map[string]interface{}{}
		for k, v := range on {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	for name, raw := range map[string]map[string]interface{}{
		"campaign and manual together":     with(map[string]interface{}{KeyRuneManual: true}),
		"campaign without the master":      {KeyRuneCampaign: true},
		"start below zero":                 with(map[string]interface{}{KeyRuneStartBelow: 0}),
		"stop at the start":                with(map[string]interface{}{KeyRuneStopAbove: 1000}),
		"stop under the start":             with(map[string]interface{}{KeyRuneStartBelow: 5000, KeyRuneStopAbove: 4000}),
		"target under a thousand":          with(map[string]interface{}{KeyRuneTarget: 999}),
		"target huge":                      with(map[string]interface{}{KeyRuneTarget: MaxRuneCampaignNumber + 1}),
		"min essence negative":             with(map[string]interface{}{KeyRuneMinEssence: -1}),
		"min runners eleven":               with(map[string]interface{}{KeyRuneMinRunners: 11}),
		"hand over at under a thousand":    with(map[string]interface{}{KeyRuneHandOverAt: 999}),
		"hand over min above at":           with(map[string]interface{}{KeyRuneHandOverAt: 5000, KeyRuneHandOverMin: 6000}),
		"hand over min zero":               with(map[string]interface{}{KeyRuneHandOverMin: 0}),
		"courier wait zero":                with(map[string]interface{}{KeyRuneCourierWait: 0}),
		"courier wait two days":            with(map[string]interface{}{KeyRuneCourierWait: 2880}),
		"pause zero":                       with(map[string]interface{}{KeyRunePause: 0}),
		"pause text":                       with(map[string]interface{}{KeyRunePause: "soon"}),
		"floor negative":                   with(map[string]interface{}{KeyRuneMuleCoinFloor: -1}),
		"a trip of ten coins":              with(map[string]interface{}{KeyRuneBuyTrip: 10}),
		"buy all not a bool":               with(map[string]interface{}{KeyRuneBuyAll: "maybe"}),
		"campaign not a bool":              {KeyRune: true, KeyRuneCampaign: "often"},
		"an unknown campaign key":          with(map[string]interface{}{"craftRuneCampaignTarget": 5}),
		"the old courier wait spelt short": with(map[string]interface{}{"craftRuneCourierWait": 90}),
	} {
		if _, err := DecodeSettings(raw); err == nil {
			t.Errorf("%s: accepted %v", name, raw)
		}
	}
	// A trip of zero is the whole stack and is fine; so is a floor of zero (take every coin).
	if _, err := DecodeSettings(with(map[string]interface{}{KeyRuneBuyTrip: 0, KeyRuneMuleCoinFloor: 0, KeyRuneMinEssence: 0, KeyRuneMinRunners: 0})); err != nil {
		t.Fatal(err)
	}
}

// ---- the trigger through Decide ----

func TestCampaignTriggerThroughDecide(t *testing.T) {
	type tc struct {
		name string
		set  func(s *Settings)
		snap func(s *Snapshot)
		want Mode
		why  string
	}
	for _, c := range []tc{
		{"the mule is out of Natures: start", nil, nil, ModeNature, "nature campaign: start - mule Nature 72 < 1,000"},
		{"the mule holds 5,000", nil, func(s *Snapshot) {
			s.Stock = stockMap(map[string]int{"Nature rune": 5000, "Rune essence": 12400, "Nature talisman": 7})
		}, ModeSwarm, "holds 5,000 Nature runes"},
		{"the mule has not published", nil, func(s *Snapshot) { s.Stock = nil }, ModeSwarm, "has not published"},
		{"Runecrafting 43", nil, func(s *Snapshot) { s.Rune.Level = 43 }, ModeSwarm, "under the 44"},
		{"essence 3k and no buy-out", nil, func(s *Snapshot) {
			s.Stock = stockMap(map[string]int{"Nature rune": 72, "Rune essence": 3000, "Nature talisman": 7})
			s.Rune.BuyOutWhy = "the mule's coins are not published"
		}, ModeSwarm, "holds 3,000 Rune essence, no buy-out"},
		{"essence 0 but the whole stack funds a buy-out", nil, func(s *Snapshot) {
			s.Stock = stockMap(map[string]int{"Nature rune": 72, "Nature talisman": 7, "Coins": 11_229_284})
			s.Rune.BuyOut = 112_192
		}, ModeNature, "112,192 more by a buy-out"},
		{"one carrier standing by", nil, func(s *Snapshot) { s.Rune.Standby = 1 }, ModeSwarm, "1 carrier(s) ready"},
		{"carriers from the list", func(s *Settings) { s.Rune.Runners = []string{"a", "b", "c"} }, func(s *Snapshot) { s.Rune.Standby = 0 }, ModeNature, "3 carriers ready"},
		{"no talisman anywhere", nil, func(s *Snapshot) {
			s.Rune.BankTalisman = nil
			s.Stock = stockMap(map[string]int{"Nature rune": 72, "Rune essence": 12400})
		}, ModeSwarm, "no Nature talisman"},
		{"a free world", nil, func(s *Snapshot) { s.Rune.Members = false }, ModeSwarm, "not a members world"},
		{"the mule is not heard: the swarm, inconclusive", nil, func(s *Snapshot) { s.MuleHeard = false }, ModeSwarm, "not heard"},
		{"both switches off", func(s *Settings) { s.Rune.Campaign = false }, nil, ModeSwarm, "craftRuneManual and craftRuneCampaign are off"},
		{"the master switch off", func(s *Settings) { s.Rune.On = false }, nil, ModeSwarm, ""},
		{"the gate's backoff holds the campaign too", nil, func(s *Snapshot) {
			s.Ledger.Backoffs = map[string]Backoff{string(ModeNature): {Fails: 2, Until: runeNow.Add(time.Hour)}}
		}, ModeSwarm, "backing off"},
	} {
		set := campaignSet()
		if c.set != nil {
			c.set(&set)
		}
		d := Decide(set, campaignSnap(c.snap))
		if d.Mode != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, d.Mode, d.Why, c.want)
			continue
		}
		if c.why != "" && !strings.Contains(d.Why, c.why) {
			t.Errorf("%s: why %q lacks %q", c.name, d.Why, c.why)
		}
		if d.Mode == ModeNature && (!d.Campaign || d.Continue || d.Resume) {
			t.Errorf("%s: a new campaign is a campaign round, not a continuation or a resume: %+v", c.name, d)
		}
	}
}

// A decision that is not a campaign's (the switch) says so.
func TestManualRoundIsNotACampaignRound(t *testing.T) {
	d := Decide(natureSet(), natureSnap(nil))
	if d.Mode != ModeNature || d.Campaign || d.Continue {
		t.Fatalf("%+v", d)
	}
}

// A finished campaign does not start another until the mule's Natures fall under the start level.
func TestCampaignHysteresisThroughDecide(t *testing.T) {
	disarmed := func(s *Snapshot) { s.Ledger.Rune.Campaign.Disarmed = true }
	mule := func(n int) func(s *Snapshot) {
		return func(s *Snapshot) {
			disarmed(s)
			s.Stock = stockMap(map[string]int{"Nature rune": n, "Rune essence": 12400, "Nature talisman": 7})
		}
	}
	for _, c := range []struct {
		mule int
		want Mode
	}{{59000, ModeSwarm}, {1500, ModeSwarm}, {1000, ModeSwarm}, {999, ModeNature}, {0, ModeNature}} {
		if d := Decide(campaignSet(), campaignSnap(mule(c.mule))); d.Mode != c.want {
			t.Errorf("finished, the mule at %d: %s (%s), want %s", c.mule, d.Mode, d.Why, c.want)
		}
	}
	d := Decide(campaignSet(), campaignSnap(mule(59000)))
	if !strings.Contains(d.Why, "re-arms when the mule's Nature runes fall under 1,000") {
		t.Errorf("it must say what re-arms it: %s", d.Why)
	}
}

// A campaign under way carries on with the mule holding anything, chains straight on, and sits out its pause.
func TestCampaignUnderWay(t *testing.T) {
	active := func(mod func(s *Snapshot)) func(s *Snapshot) {
		return func(s *Snapshot) {
			s.Ledger.Rune.Campaign = CampaignLedger{Active: true, Started: runeNow.Add(-5 * time.Hour), Crafted: 31000}
			s.Stock = stockMap(map[string]int{"Nature rune": 30000, "Rune essence": 9000, "Nature talisman": 7})
			if mod != nil {
				mod(s)
			}
		}
	}
	d := Decide(campaignSet(), campaignSnap(active(nil)))
	if d.Mode != ModeNature || !d.Campaign || !d.Continue || d.Resume || !strings.Contains(d.Why, "go on") {
		t.Fatalf("an active campaign with the mule at 30,000 goes on: %+v", d)
	}
	// ...and chains straight on after a round of its own kind, which a new gate round decided on the mule's bank does not.
	if !Chain(ModeNature, d) {
		t.Fatal("a campaign's next round must chain after a gate round")
	}
	if Chain(ModeNature, Decision{Mode: ModeNature, Campaign: true}) {
		t.Fatal("a campaign that is only starting waits for the next check")
	}
	// A pause holds it off, and the other rounds get their turn.
	paused := active(func(s *Snapshot) {
		s.Ledger.Rune.Campaign.PausedUntil, s.Ledger.Rune.Campaign.LastWhy = runeNow.Add(30*time.Minute), "the round's time is used up"
		s.Stock = stockMap(map[string]int{"Nature rune": 30000, "Rune essence": 9000, "Nature talisman": 7, "Uncut sapphire": 25000, "Chisel": 1})
	})
	set := campaignSet()
	set.GemThreshold = 500
	d = Decide(set, campaignSnap(paused))
	if d.Mode != ModeGems {
		t.Fatalf("a paused campaign gives the gem round its turn: %s (%s)", d.Mode, d.Why)
	}
	d = Decide(campaignSet(), campaignSnap(active(func(s *Snapshot) {
		s.Ledger.Rune.Campaign.PausedUntil, s.Ledger.Rune.Campaign.LastWhy = runeNow.Add(30*time.Minute), "the round's time is used up"
	})))
	if d.Mode != ModeSwarm || !strings.Contains(d.Why, "nature campaign: paused until 20:30 (the round's time is used up)") {
		t.Fatalf("with no round to give the turn to, ScarabSwarm says why: %s (%s)", d.Mode, d.Why)
	}
	// The pause is over.
	d = Decide(campaignSet(), campaignSnap(active(func(s *Snapshot) { s.Ledger.Rune.Campaign.PausedUntil = runeNow.Add(-time.Minute) })))
	if d.Mode != ModeNature || !d.Continue {
		t.Fatalf("after the pause: %+v", d)
	}
	// A campaign whose essence has run out (the mule's, and no buy-out) waits for essence, not for a pause.
	d = Decide(campaignSet(), campaignSnap(active(func(s *Snapshot) {
		s.Stock = stockMap(map[string]int{"Nature rune": 30000, "Rune essence": 100, "Nature talisman": 7})
	})))
	if d.Mode != ModeSwarm || !strings.Contains(d.Why, "100 Rune essence") {
		t.Fatalf("no essence: %s (%s)", d.Mode, d.Why)
	}
}

// The campaign beats the batch rounds (a gem round that the mule's stock keeps due must not pull the main account off the gate), loses to chest
// loot owed to the mule, and a hand-over waiting comes before it.
func TestCampaignOrdering(t *testing.T) {
	gems := func(s *Snapshot) {
		s.Stock = stockMap(map[string]int{"Nature rune": 72, "Rune essence": 12400, "Nature talisman": 7, "Uncut sapphire": 600, "Chisel": 1})
	}
	set := campaignSet()
	set.GemThreshold = 500
	if d := Decide(set, campaignSnap(gems)); d.Mode != ModeNature {
		t.Fatalf("with 600 uncut sapphires waiting: %+v", d)
	}
	if d := Decide(set, campaignSnap(func(s *Snapshot) { gems(s); s.Ledger.PendingDeliver = true })); d.Mode != ModeDeliver {
		t.Fatalf("a hand-over waiting: %+v", d)
	}
	// Chest loot owed to the mule comes before a NEW campaign round and before the next round of one under way (the round resumes
	// where it stands, and a gate round under way is never pre-empted).
	set.CrystalChest = true
	owed := func(s *Snapshot) {
		campaignSnap(nil)
		s.Ledger.Chest.Owed = map[string]int{"Rune platelegs": 3}
	}
	d := DecideWithChest(set, campaignSnap(owed), ChestInput{})
	if d.Mode != ModeChest || !strings.Contains(d.Why, "comes before a new nature round") {
		t.Fatalf("owed chest loot: %s (%s)", d.Mode, d.Why)
	}
	atGate := func(s *Snapshot) { owed(s); s.Rune.AtGate = true }
	if d := DecideWithChest(set, campaignSnap(atGate), ChestInput{}); d.Mode != ModeNature || !d.Resume {
		t.Fatalf("a round under way carries on first: %s (%s)", d.Mode, d.Why)
	}
}

// A restart standing at the gate or in the temple resumes whatever switch is on (the loop then ends it by its own rule).
func TestCampaignResumeAtTheGate(t *testing.T) {
	for name, mod := range map[string]func(s *Snapshot){
		"at the gate":          func(s *Snapshot) { s.Rune.AtGate = true },
		"in the Nature temple": func(s *Snapshot) { s.Rune.Where, s.Rune.Temple = runecraftplan.RegionTemple, runecraftplan.KeyNature },
		"at the gate, the mule not heard": func(s *Snapshot) {
			s.Rune.AtGate, s.MuleHeard = true, false
		},
	} {
		d := Decide(campaignSet(), campaignSnap(mod))
		if d.Mode != ModeNature || !d.Resume {
			t.Errorf("%s: %+v", name, d)
		}
	}
}

// ---- the ledger ----

func TestCampaignLedgerLifecycle(t *testing.T) {
	var c CampaignLedger
	now := runeNow
	c.Begin(now, 72)
	c.NoteCraft(25)
	c.NoteCraft(25)
	c.NoteTrade(50)
	c.NoteCourier(2300)
	c.NoteRound()
	c.NoteHandOver(20300, true)
	c.NoteHandOver(1000, false)
	c.NoteBuyOut(40000, 4_100_000)
	if !c.Active || c.Number != 1 || c.Crafted != 50 || c.EssenceTaken != 50 || c.CourierNatures != 2300 || c.Rounds != 1 || c.HandedOver != 21300 || c.OwnTrips != 1 ||
		c.BuyOuts != 1 || c.Bought != 40000 || c.BoughtSpent != 4_100_000 || c.MuleAtStart != 72 || !c.Started.Equal(now) {
		t.Fatalf("%+v", c)
	}
	// Negative figures are never subtracted.
	c.NoteCraft(-5)
	c.NoteTrade(-5)
	c.NoteBuyOut(-1, -1)
	if c.Crafted != 50 || c.EssenceTaken != 50 || c.Bought != 40000 {
		t.Fatalf("%+v", c)
	}
	// A hand-over trip changes nothing about the campaign's standing.
	c.End(runecraftplan.EndHandOver, "own trip", now, 45*time.Minute)
	if !c.Active || !c.PausedUntil.IsZero() || c.LastEnd != "hand-over" {
		t.Fatalf("%+v", c)
	}
	// The round's time pauses it...
	c.End(runecraftplan.EndTime, "eight hours", now, 45*time.Minute)
	if until, paused := c.Paused(now.Add(time.Minute)); !paused || !until.Equal(now.Add(45*time.Minute)) {
		t.Fatalf("%+v", c)
	}
	if _, paused := c.Paused(now.Add(46 * time.Minute)); paused {
		t.Fatal("the pause is over")
	}
	for _, k := range []runecraftplan.EndKind{runecraftplan.EndCarriers, runecraftplan.EndDry} {
		c.PausedUntil = time.Time{}
		c.End(k, "x", now, 10*time.Minute)
		if _, paused := c.Paused(now); !paused {
			t.Errorf("%s pauses", k)
		}
	}
	// ...a hand-over clears a pause (the trip ends a round and the next must not be held off by an old one).
	c.End(runecraftplan.EndHandOver, "again", now, time.Hour)
	if !c.PausedUntil.IsZero() {
		t.Fatal("a hand-over trip clears the pause")
	}
	// The target finishes it for good: not active, disarmed, counted.
	c.End(runecraftplan.EndTarget, "100,000 crafted", now, 0)
	if c.Active || !c.Disarmed || c.Done != 1 {
		t.Fatalf("%+v", c)
	}
	if _, paused := c.Paused(now); paused {
		t.Fatal("a campaign that is not active is never paused")
	}
	// The next campaign counts on from the last and clears the hysteresis.
	c.Begin(now.Add(24*time.Hour), 500)
	if !c.Active || c.Disarmed || c.Number != 2 || c.Done != 1 || c.Crafted != 0 || c.Bought != 0 || c.LastEnd != "" {
		t.Fatalf("%+v", c)
	}
}

func TestCampaignReconcile(t *testing.T) {
	set := DefaultSettings().Rune
	now := runeNow
	// Active with the target already reached (a restart between the craft and the end): finished.
	c := CampaignLedger{Active: true, Crafted: 100000}
	if got := c.Reconcile(set, 0, false, now); got == "" || c.Active || !c.Disarmed || c.Done != 1 {
		t.Fatalf("%q %+v", got, c)
	}
	// The mule held the stop level while the campaign sat out its pause: finished.
	c = CampaignLedger{Active: true, Crafted: 40000, PausedUntil: now.Add(time.Hour)}
	if got := c.Reconcile(set, 61200, true, now); got == "" || c.Active || !c.Disarmed {
		t.Fatalf("%q %+v", got, c)
	}
	// An unpublished mule finishes nothing on its stock.
	c = CampaignLedger{Active: true, Crafted: 40000}
	if got := c.Reconcile(set, 70000, false, now); got != "" || !c.Active {
		t.Fatalf("%q %+v", got, c)
	}
	// A finished campaign re-arms under the start level and not at it.
	c = CampaignLedger{Disarmed: true}
	if got := c.Reconcile(set, 1000, true, now); got != "" || !c.Disarmed {
		t.Fatalf("at the level: %q %+v", got, c)
	}
	if got := c.Reconcile(set, 999, true, now); got == "" || c.Disarmed {
		t.Fatalf("under the level: %q %+v", got, c)
	}
	// An active one is not touched when nothing is reached.
	c = CampaignLedger{Active: true, Crafted: 5000}
	if got := c.Reconcile(set, 300, true, now); got != "" || !c.Active {
		t.Fatalf("%q %+v", got, c)
	}
	var nilLedger *CampaignLedger
	if nilLedger.Reconcile(set, 0, true, now) != "" {
		t.Fatal("nil")
	}
	if _, paused := nilLedger.Paused(now); paused {
		t.Fatal("nil is never paused")
	}
}

// The campaign persists crafted, started, the pause and the stop reason through the ledger's JSON, and an old ledger without it reads.
func TestCampaignLedgerRoundTrip(t *testing.T) {
	led := Ledger{}
	led.Rune.Campaign = CampaignLedger{
		Active: true, Number: 3, Done: 2, Started: runeNow.Add(-6 * time.Hour), MuleAtStart: 72, Crafted: 31025, EssenceTaken: 31025, Rounds: 2, OwnTrips: 1,
		HandedOver: 20300, CourierNatures: 6900, BuyOuts: 1, Bought: 100000, BoughtSpent: 10_300_000,
		PausedUntil: runeNow.Add(30 * time.Minute), LastEnd: string(runecraftplan.EndTime), LastWhy: "eight hours", LastEndAt: runeNow,
	}
	raw, err := json.Marshal(led)
	if err != nil {
		t.Fatal(err)
	}
	var back Ledger
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&back); err != nil {
		t.Fatalf("strict decode: %v\n%s", err, raw)
	}
	if !reflect.DeepEqual(led.Rune.Campaign, back.Rune.Campaign) {
		t.Fatalf("round trip:\n%+v\n%+v", led.Rune.Campaign, back.Rune.Campaign)
	}
	for _, key := range []string{`"campaign":{"active":true`, `"crafted":31025`, `"pausedUntil"`, `"lastEnd":"max-hours"`, `"disarmed"`} {
		if key == `"disarmed"` {
			if strings.Contains(string(raw), key) {
				t.Errorf("a false flag is omitted: %s", raw)
			}
			continue
		}
		if !strings.Contains(string(raw), key) {
			t.Errorf("%s lacks %s", raw, key)
		}
	}
	// A ledger written before the campaign existed has none, and loads as one that is not active.
	var old Ledger
	if err := json.Unmarshal([]byte(`{"rune":{"rounds":3,"gate":{"rounds":1}}}`), &old); err != nil || old.Rune.Campaign.Active || old.Rune.Gate.Rounds != 1 {
		t.Fatalf("%v %+v", err, old.Rune)
	}
}

// own() for the chest round's alchemy ask counts the Natures the main account keeps: banked at the end of a gate round (the keep), so the ask
// is for nothing and the 4,000 are not asked of the mule that has none.
func TestChestAskCountsTheKeptNatures(t *testing.T) {
	stock := stockMap(map[string]int{"Nature rune": 72, "Ruby": 5000})
	own := func(m map[string]int) func(string) int { return func(item string) int { return m[item] } }
	// Nothing kept: the ask is the mule's 72 (all it has) and the noted rubies against them.
	lines := ChestAlchAsk(stock, own(map[string]int{}))
	if len(lines) != 2 || lines[0] != (Line{Item: NatureKit, Qty: 72}) {
		t.Fatalf("nothing kept: %v", lines)
	}
	// The keep of 4,000 held (the bank after the gate round, or the pack): no Nature line, whatever the mule holds.
	stock = stockMap(map[string]int{"Nature rune": 90000, "Ruby": 5000})
	lines = ChestAlchAsk(stock, own(map[string]int{NatureKit: DefaultRuneKeep}))
	for _, l := range lines {
		if l.Item == NatureKit {
			t.Fatalf("main holds %d Natures and must not ask for more: %v", DefaultRuneKeep, lines)
		}
	}
	// 1,500 kept: asks the 500 that tops it up to the 2,000 the chest round carries over four visits... only under four visits' worth.
	lines = ChestAlchAsk(stock, own(map[string]int{NatureKit: 150}))
	if len(lines) == 0 || lines[0] != (Line{Item: NatureKit, Qty: ChestNaturesAsk - 150}) {
		t.Fatalf("150 held: %v", lines)
	}
	if DefaultRuneKeep < ChestNaturesAsk {
		t.Fatalf("the keep %d must cover the chest round's ask %d", DefaultRuneKeep, ChestNaturesAsk)
	}
}

// While the Nature gate is on the swarm's loot hand-off keeps the talisman and the Natures, at any level: the keep must not go to the mule
// on the daily consolidate and be asked back.
func TestRuneKeepNamesKeepsTheNatureStock(t *testing.T) {
	has := func(names []string, want string) bool {
		for _, n := range names {
			if n == want {
				return true
			}
		}
		return false
	}
	set := campaignSet()
	for _, level := range []int{0, 5, 44, 72} {
		got := RuneKeepNames(set, level)
		if !has(got, "Nature rune") || !has(got, "Nature talisman") {
			t.Errorf("level %d: %v", level, got)
		}
		if level >= 44 && has(got, "Rune essence") {
			t.Errorf("level %d: the levelling's essence is not kept past the target: %v", level, got)
		}
	}
	if !has(RuneKeepNames(set, 5), "Rune essence") || !has(RuneKeepNames(set, 5), "Air talisman") {
		t.Error("the levelling round's items are still kept below the target")
	}
	set.Rune.Campaign, set.Rune.Manual = false, true
	if !has(RuneKeepNames(set, 60), "Nature rune") {
		t.Error("the switch keeps them too")
	}
	set.Rune.Manual = false
	if got := RuneKeepNames(set, 60); got != nil {
		t.Errorf("neither on: %v", got)
	}
	set.Rune.On, set.Rune.Campaign = false, true
	if got := RuneKeepNames(set, 60); got != nil {
		t.Errorf("the master switch is off: %v", got)
	}
}
