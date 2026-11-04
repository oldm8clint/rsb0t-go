package mastercraftplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/runecraftplan"
)

// natureSet is the defaults with the round and the gate switched on and every other round off.
func natureSet() Settings {
	s := DefaultSettings()
	s.Rune.On, s.Rune.Manual = true, true
	return s
}

// natureSnap is a Runecrafting 44 account at the mule's post whose bank holds a Nature talisman.
func natureSnap(mod func(s *Snapshot)) Snapshot {
	s := Snapshot{
		Now: runeNow, MuleHeard: true, MuleSameWorld: true, Stock: stockMap(map[string]int{}), Ledger: &Ledger{},
		CraftingLevel: 30, FletchingLevel: 60, FletchingKnown: true,
		Rune: RuneInput{
			Known: true, Level: 44, XP: 55649, Members: true,
			BankKnown: true, BankTalisman: map[string]int{runecraftplan.KeyNature: 1}, PackTalisman: map[string]bool{},
		},
	}
	if mod != nil {
		mod(&s)
	}
	return s
}

func TestNatureDefaults(t *testing.T) {
	r := DefaultSettings().Rune
	if r.Manual || r.Station != StationGate || len(r.Runners) != 0 || r.Keep != 4000 || !r.Courier || r.CourierMax != 3000 || r.MaxHours != 8*time.Hour {
		t.Fatalf("defaults: %+v", r)
	}
	// The gate's alchemy is on by default (16 Oct 2026) and draws 100 bows a round: cast away on the road, so a trade never meets a stack.
	if !r.GateAlch || r.AlchBows != 100 || r.AlchHold {
		t.Fatalf("alchemy defaults: %+v", r)
	}
}

func TestGateAlchSettingsDecode(t *testing.T) {
	s, err := DecodeSettings(map[string]interface{}{KeyRuneGateAlch: "false", KeyRuneAlchBows: 120.0})
	if err != nil {
		t.Fatal(err)
	}
	if s.Rune.GateAlch || s.Rune.AlchBows != 120 || s.Rune.AlchHold {
		t.Fatalf("decoded %+v", s.Rune)
	}
	// The stack held through the trades may be larger than the road can cast away; the bound follows the switch.
	if s, err := DecodeSettings(map[string]interface{}{KeyRuneAlchHold: "true", KeyRuneAlchBows: 1500}); err != nil || !s.Rune.AlchHold || s.Rune.AlchBows != 1500 {
		t.Fatalf("held: %v %+v", err, s.Rune)
	}
	for name, raw := range map[string]map[string]interface{}{
		"a road stack over the road":  {KeyRuneAlchBows: runecraftplan.AlchRoadCasts + 1},
		"a held stack over its limit": {KeyRuneAlchHold: true, KeyRuneAlchBows: runecraftplan.MaxAlchBowsHold + 1},
		"hold not a bool":             {KeyRuneAlchHold: "sometimes"},
	} {
		if _, err := DecodeSettings(raw); err == nil {
			t.Errorf("%s: accepted %v", name, raw)
		}
	}
	for name, raw := range map[string]map[string]interface{}{
		"alch not a bool": {KeyRuneGateAlch: "maybe"},
		"bows negative":   {KeyRuneAlchBows: -1},
		"bows huge":       {KeyRuneAlchBows: runecraftplan.MaxAlchBows + 1},
		"bows text":       {KeyRuneAlchBows: "lots"},
	} {
		if _, err := DecodeSettings(raw); err == nil {
			t.Errorf("%s: accepted %v", name, raw)
		}
	}
	if s, err := DecodeSettings(map[string]interface{}{KeyRuneAlchBows: 0}); err != nil || s.Rune.AlchBows != 0 {
		t.Errorf("0 draws none: %v %+v", err, s.Rune)
	}
}

func TestNatureSettingsDecode(t *testing.T) {
	s, err := DecodeSettings(map[string]interface{}{
		KeyRune: true, KeyRuneManual: "true", KeyRuneStation: "Gate", KeyRuneRunners: "Acct 1, acct_2", KeyRuneKeep: "2500",
		KeyRuneCourier: "false", KeyRuneCourierMax: 1200.0, KeyRuneMaxHours: "6",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := s.Rune
	if !r.On || !r.Manual || r.Station != "gate" || strings.Join(r.Runners, "|") != "Acct 1|acct_2" || r.Keep != 2500 || r.Courier || r.CourierMax != 1200 || r.MaxHours != 6*time.Hour {
		t.Fatalf("decoded %+v", r)
	}
}

func TestNatureSettingsAreStrict(t *testing.T) {
	for name, raw := range map[string]map[string]interface{}{
		"inside is not built":       {KeyRuneStation: "inside"},
		"a station that is none":    {KeyRuneStation: "altar"},
		"manual not a bool":         {KeyRuneManual: "often"},
		"keep negative":             {KeyRuneKeep: -1},
		"keep huge":                 {KeyRuneKeep: MaxRuneKeep + 1},
		"keep text":                 {KeyRuneKeep: "lots"},
		"courier max zero":          {KeyRuneCourierMax: 0},
		"courier max huge":          {KeyRuneCourierMax: MaxRuneCourierMax + 1},
		"courier not a bool":        {KeyRuneCourier: "sometimes"},
		"hours zero":                {KeyRuneMaxHours: 0},
		"hours a day and a bit":     {KeyRuneMaxHours: 25},
		"hours text":                {KeyRuneMaxHours: "long"},
		"a runner with a long name": {KeyRuneRunners: []interface{}{"a very long account name"}},
		"a design key never built":  {"craftRuneTranche": 20000},
	} {
		if _, err := DecodeSettings(raw); err == nil {
			t.Errorf("%s: accepted %v", name, raw)
		}
	}
}

func TestDecideNatureTriggerTable(t *testing.T) {
	type tc struct {
		name string
		set  func(s *Settings)
		snap func(s *Snapshot)
		want Mode
		why  string
	}
	for _, c := range []tc{
		{"RC 44 with the talisman in the bank and the switch on: start", nil, nil, ModeNature, "talisman held"},
		{"the switch is off", func(s *Settings) { s.Rune.Manual = false }, nil, ModeSwarm, "craftRuneManual and craftRuneCampaign are off"},
		{"the master switch is off", func(s *Settings) { s.Rune.On = false }, nil, ModeSwarm, ""},
		{"the stat has not been read", nil, func(s *Snapshot) { s.Rune.Known = false }, ModeSwarm, "has not been read"},
		{"RC 43", nil, func(s *Snapshot) { s.Rune.Level = 43 }, ModeSwarm, "under the 44"},
		{"a free world", nil, func(s *Snapshot) { s.Rune.Members = false }, ModeSwarm, "not a members world"},
		{"no talisman anywhere", nil, func(s *Snapshot) { s.Rune.BankTalisman = nil }, ModeSwarm, "no Nature talisman"},
		{"the talisman on the mule", nil, func(s *Snapshot) {
			s.Rune.BankTalisman = nil
			s.Stock = stockMap(map[string]int{"Nature talisman": 7})
		}, ModeNature, "on the mule"},
		{"the talisman in the pack", nil, func(s *Snapshot) {
			s.Rune.BankTalisman = nil
			s.Rune.PackTalisman = map[string]bool{runecraftplan.KeyNature: true}
		}, ModeNature, "held"},
		{"carriers listed and at work are named", func(s *Settings) { s.Rune.Runners = []string{"a", "b", "c"} }, func(s *Snapshot) { s.Rune.Carriers = 2 }, ModeNature, "3 listed carrier(s), 2 at work"},
		{"backing off", nil, func(s *Snapshot) {
			s.Ledger.Backoffs = map[string]Backoff{string(ModeNature): {Fails: 2, Until: runeNow.Add(time.Hour)}}
		}, ModeSwarm, "backing off"},
		{"paused after a round that found nothing", nil, func(s *Snapshot) {
			s.Ledger.Rune.Gate.PauseUntil, s.Ledger.Rune.Gate.LastStop = runeNow.Add(time.Hour), "no carrier came"
		}, ModeSwarm, "paused until 21:00 (no carrier came)"},
	} {
		set := natureSet()
		if c.set != nil {
			c.set(&set)
		}
		d := Decide(set, natureSnap(c.snap))
		if d.Mode != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, d.Mode, d.Why, c.want)
			continue
		}
		if c.why != "" && !strings.Contains(d.Why, c.why) {
			t.Errorf("%s: why %q lacks %q", c.name, d.Why, c.why)
		}
		if d.Mode == ModeNature && d.Resume {
			t.Errorf("%s: a new round is not a resume", c.name)
		}
	}
}

// A gate round under way where the account stands carries on first of all: the mule heard or not, and before the levelling
// round's own resume (which also reads "standing in a temple").
func TestNatureResume(t *testing.T) {
	for name, mod := range map[string]func(s *Snapshot){
		"in the Nature temple": func(s *Snapshot) { s.Rune.Where, s.Rune.Temple = runecraftplan.RegionTemple, runecraftplan.KeyNature },
		"at the gate":          func(s *Snapshot) { s.Rune.AtGate = true },
		"at the gate with the mule not heard": func(s *Snapshot) {
			s.Rune.AtGate, s.MuleHeard = true, false
		},
		"in the temple with the stat unread": func(s *Snapshot) {
			s.Rune.Where, s.Rune.Temple, s.Rune.Known = runecraftplan.RegionTemple, runecraftplan.KeyNature, false
		},
	} {
		d := Decide(natureSet(), natureSnap(mod))
		if d.Mode != ModeNature || !d.Resume {
			t.Errorf("%s: %+v", name, d)
		}
	}
	// With the gate switch off the resume still carries on (the round then ends and walks home); with the master switch off
	// it does not.
	set := natureSet()
	set.Rune.Manual = false
	if d := Decide(set, natureSnap(func(s *Snapshot) { s.Rune.AtGate = true })); d.Mode != ModeNature || !d.Resume {
		t.Errorf("manual off at the gate: %+v", d)
	}
	set.Rune.On = false
	if d := Decide(set, natureSnap(func(s *Snapshot) { s.Rune.AtGate = true })); d.Mode == ModeNature {
		t.Errorf("round off: %+v", d)
	}
	// Another temple is the levelling round's.
	d := Decide(natureSet(), natureSnap(func(s *Snapshot) {
		s.Rune.Where, s.Rune.Temple, s.Rune.Level = runecraftplan.RegionTemple, runecraftplan.KeyBody, 30
	}))
	if d.Mode != ModeRunecraft || !d.Resume {
		t.Errorf("standing in the Body temple: %+v", d)
	}
}

// The gate round ranks after the jewellery round and before the levelling round and the batch rounds.
func TestNatureRanksBeforeTheBatchRounds(t *testing.T) {
	gems := func(s *Snapshot) {
		s.Stock = stockMap(map[string]int{"Uncut sapphire": 600, "Chisel": 1})
	}
	set := natureSet()
	set.GemThreshold = 500
	if d := Decide(set, natureSnap(gems)); d.Mode != ModeNature {
		t.Fatalf("with 600 uncut sapphires waiting: %+v", d)
	}
	set.Rune.Manual = false
	if d := Decide(set, natureSnap(gems)); d.Mode != ModeGems {
		t.Fatalf("the switch off: %+v", d)
	}
	// Products waiting to be handed over come first.
	set.Rune.Manual = true
	if d := Decide(set, natureSnap(func(s *Snapshot) { gems(s); s.Ledger.PendingDeliver = true })); d.Mode != ModeDeliver {
		t.Fatalf("a hand-over waiting: %+v", d)
	}
	// A gate round is not chained after another gate round decided on the mule's bank.
	if Chain(ModeNature, Decision{Mode: ModeNature}) || !Chain(ModeNature, Decision{Mode: ModeGems}) {
		t.Fatal("Chain")
	}
}

func TestGateAllowed(t *testing.T) {
	runners := []string{"Acct 1", "acct_2"}
	work := []string{"Spare_22"}
	for name, want := range map[string]bool{
		"acct 1": true, "Acct_1": true, "ACCT 2": true, "spare 22": true, "Bob": false, "": false, " ": false,
	} {
		if got := GateAllowed(runners, work, name); got != want {
			t.Errorf("GateAllowed(%q) = %v, want %v", name, got, want)
		}
	}
	if GateAllowed(nil, nil, "acct 1") {
		t.Fatal("nobody is allowed by an empty list")
	}
}

func TestGateLedgerCounters(t *testing.T) {
	var g GateLedger
	now := runeNow
	g.NoteTrade(25, now)
	g.NoteTrade(24, now.Add(time.Minute))
	g.NoteCraft(49, 441)
	g.NoteCourier(2300)
	if g.Trades != 2 || g.EssenceTaken != 49 || g.NaturesMade != 49 || g.XPGained != 441 || g.CourierTrades != 1 || g.CourierNatures != 2300 || !g.LastTrade.Equal(now.Add(time.Minute)) {
		t.Fatalf("%+v", g)
	}
}
