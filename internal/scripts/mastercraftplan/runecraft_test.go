package mastercraftplan

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/runecraftplan"
)

var runeNow = time.Date(2026, 10, 13, 20, 0, 0, 0, time.UTC)

// runeSet is the defaults with the round switched on and every other round off, so the decision under test
// is the only one that can answer.
func runeSet() Settings {
	s := DefaultSettings()
	s.Rune.On = true
	return s
}

// runeSnap is a Runecrafting 1 account at the mule's post whose bank holds essence and an Air talisman.
func runeSnap(mod func(s *Snapshot)) Snapshot {
	s := Snapshot{
		Now: runeNow, MuleHeard: true, MuleSameWorld: true, Stock: stockMap(map[string]int{}), Ledger: &Ledger{},
		CraftingLevel: 30, FletchingLevel: 60, FletchingKnown: true,
		Rune: RuneInput{
			Known: true, Level: 1, XP: 0, Members: true,
			BankKnown: true, BankEssence: 300, BankTalisman: map[string]int{runecraftplan.KeyAir: 1},
			PackTalisman: map[string]bool{},
		},
	}
	if mod != nil {
		mod(&s)
	}
	return s
}

func TestRuneDefaultsAreOffAndTheOwners(t *testing.T) {
	r := DefaultSettings().Rune
	if r.On || r.LevelTo != 44 || r.Essence != runecraftplan.SourceAuto || r.Batch != 1000 || r.Gateway != "guild" || !r.Teleport || r.Round != 240*time.Minute || r.BuyBudget != 0 {
		t.Fatalf("defaults: %+v", r)
	}
	if _, err := DecodeSettings(map[string]interface{}{}); err != nil {
		t.Fatal(err)
	}
}

func TestRuneSettingsAreStrict(t *testing.T) {
	good, err := DecodeSettings(map[string]interface{}{KeyRune: "true", KeyRuneEssence: "Mine", KeyRuneGateway: "AUBURY", KeyRuneLevelTo: "30", KeyRuneBatch: 540, KeyRuneRound: "90", KeyRuneTeleport: "off"})
	if err != nil {
		t.Fatal(err)
	}
	r := good.Rune
	if !r.On || r.Essence != runecraftplan.SourceMine || r.Gateway != "aubury" || r.LevelTo != 30 || r.Batch != 540 || r.Round != 90*time.Minute || r.Teleport {
		t.Fatalf("decoded %+v", r)
	}
	for name, raw := range map[string]map[string]interface{}{
		"level 1":             {KeyRuneLevelTo: 1},
		"level over 44":       {KeyRuneLevelTo: 45},
		"level text":          {KeyRuneLevelTo: "forty"},
		"essence from shop":   {KeyRuneEssence: "buy"},
		"batch under a load":  {KeyRuneBatch: 20},
		"batch huge":          {KeyRuneBatch: 100001},
		"gateway sedridor":    {KeyRuneGateway: "sedridor"},
		"round of a minute":   {KeyRuneRound: 1},
		"on not a bool":       {KeyRune: "maybe"},
		"teleport a number":   {KeyRuneTeleport: "sometimes"},
		"buy budget negative": {KeyRuneBuyBudget: -1},
		"buy budget huge":     {KeyRuneBuyBudget: MaxRuneBuyBudget + 1},
		"buy budget text":     {KeyRuneBuyBudget: "lots"},
		"station inside":      {"craftRuneStation": "inside"},
		"typo":                {"craftRunes": true},
	} {
		if _, err := DecodeSettings(raw); err == nil {
			t.Errorf("%s: accepted %v", name, raw)
		}
	}
	if _, err := DecodeSettings(map[string]interface{}{"craftRuneStation": "inside"}); err == nil || !strings.Contains(err.Error(), "craftRuneStation") {
		t.Errorf("an unbuilt craftRuneStation must be named: %v", err)
	}
}

func TestDecideRuneTriggerTable(t *testing.T) {
	type tc struct {
		name string
		set  func(s *Settings)
		snap func(s *Snapshot)
		want Mode
		why  string
	}
	cases := []tc{
		{"the bank holds essence and the talisman: start", nil, nil, ModeRunecraft, "Air 894, Body 6,824"},
		{"the master switch is off", func(s *Settings) { s.Rune.On = false }, nil, ModeSwarm, ""},
		{"the stat has not been read", nil, func(s *Snapshot) { s.Rune.Known = false }, ModeSwarm, "has not been read"},
		{"already at the target", nil, func(s *Snapshot) { s.Rune.Level, s.Rune.XP = 44, 55649 }, ModeSwarm, "reached craftRuneLevelTo 44"},
		{"a lower target already reached", func(s *Settings) { s.Rune.LevelTo = 20 }, func(s *Snapshot) { s.Rune.Level = 20 }, ModeSwarm, "reached"},
		{"RC 20: Body", nil, func(s *Snapshot) {
			s.Rune.Level, s.Rune.XP = 20, 4470
			s.Rune.BankTalisman = map[string]int{runecraftplan.KeyBody: 2}
		}, ModeRunecraft, "Body 6,824"},
		{"no talisman in the bank, the mule publishes one", nil, func(s *Snapshot) {
			s.Rune.BankTalisman = nil
			s.Stock = stockMap(map[string]int{"Air talisman": 1541})
		}, ModeRunecraft, ""},
		{"no talisman anywhere", nil, func(s *Snapshot) { s.Rune.BankTalisman = nil }, ModeSwarm, "Air talisman"},
		{"the talisman in the pack counts", nil, func(s *Snapshot) {
			s.Rune.BankTalisman = nil
			s.Rune.PackTalisman = map[string]bool{runecraftplan.KeyAir: true}
		}, ModeRunecraft, ""},
		{"a few essence in the bank: not a trip", nil, func(s *Snapshot) { s.Rune.BankEssence = 5 }, ModeSwarm, "no essence source"},
		{"empty bank, the mule publishes essence", nil, func(s *Snapshot) {
			s.Rune.BankEssence = 0
			s.Stock = stockMap(map[string]int{"Rune essence": 3000})
		}, ModeRunecraft, "mule's published bank holds 3,000 essence"},
		{"empty bank, the mule publishes less than a load", nil, func(s *Snapshot) {
			s.Rune.BankEssence = 0
			s.Stock = stockMap(map[string]int{"Rune essence": 20})
		}, ModeSwarm, "no essence source"},
		{"empty bank, an account that can mine", nil, func(s *Snapshot) {
			s.Rune.BankEssence, s.Rune.CanMine = 0, true
		}, ModeRunecraft, "mined at the guild gateway"},
		{"empty bank, cannot mine: says why", nil, func(s *Snapshot) {
			s.Rune.BankEssence, s.Rune.MineWhy = 0, "Wizard Distentor is behind the Wizards' Guild door, which needs Magic 66 (have 40)"
		}, ModeSwarm, "Magic 66"},
		{"essence from the bank only", func(s *Settings) { s.Rune.Essence = runecraftplan.SourceBank }, func(s *Snapshot) {
			s.Rune.BankEssence, s.Rune.CanMine = 0, true
			s.Stock = stockMap(map[string]int{"Rune essence": 3000})
		}, ModeSwarm, "mining off by craftRuneEssence bank"},
		{"essence from the mule only, mule empty", func(s *Settings) { s.Rune.Essence = runecraftplan.SourceMule }, func(s *Snapshot) {
			s.Rune.BankEssence, s.Rune.CanMine = 0, true
		}, ModeSwarm, "no essence source"},
		{"essence from mining only", func(s *Settings) { s.Rune.Essence = runecraftplan.SourceMine }, func(s *Snapshot) {
			s.Rune.BankEssence, s.Rune.CanMine = 0, true
			s.Stock = stockMap(map[string]int{"Rune essence": 3000})
		}, ModeRunecraft, "mined"},
		{"the bank is not known yet: the round reads it", nil, func(s *Snapshot) {
			s.Rune.BankKnown, s.Rune.BankEssence, s.Rune.BankTalisman = false, 0, nil
			s.Stock = stockMap(map[string]int{"Air talisman": 9})
		}, ModeRunecraft, "has not been read"},
		{"backing off after failed rounds", nil, func(s *Snapshot) {
			s.Ledger.Backoffs = map[string]Backoff{string(ModeRunecraft): {Fails: 2, Until: runeNow.Add(time.Hour)}}
		}, ModeSwarm, "backing off"},
		{"paused after a round that found nothing", nil, func(s *Snapshot) {
			s.Ledger.Rune.PauseUntil, s.Ledger.Rune.LastStop = runeNow.Add(time.Hour), "no essence source"
		}, ModeSwarm, "paused until 21:00"},
		{"a free world still levels on Air", nil, func(s *Snapshot) { s.Rune.Members = false }, ModeRunecraft, ""},
		// The bounded market lot: the last source, only on "auto" and only when a lot is allowed.
		{"empty bank, cannot mine, a market lot is allowed", func(s *Settings) { s.Rune.BuyBudget = 300000 }, func(s *Snapshot) {
			s.Rune.BankEssence, s.Rune.CanBuy, s.Rune.BuyQty = 0, true, 2000
		}, ModeRunecraft, "market lot of up to 2,000 essence is allowed (budget 300,000)"},
		{"empty bank, no lot allowed: says why", func(s *Settings) { s.Rune.BuyBudget = 300000 }, func(s *Snapshot) {
			s.Rune.BankEssence, s.Rune.BuyWhy = 0, "the mule holds 773,226 coins, not above its 1000000 floor"
		}, ModeSwarm, "market lot: the mule holds 773,226 coins"},
		{"empty bank, the lot is off", nil, func(s *Snapshot) { s.Rune.BankEssence = 0 }, ModeSwarm, "market lot: off"},
		{"a lot under a load is no source", func(s *Settings) { s.Rune.BuyBudget = 300000 }, func(s *Snapshot) {
			s.Rune.BankEssence, s.Rune.CanBuy, s.Rune.BuyQty = 0, true, 5
		}, ModeSwarm, "no essence source"},
		{"a named source never buys", func(s *Settings) { s.Rune.BuyBudget = 300000; s.Rune.Essence = runecraftplan.SourceMine }, func(s *Snapshot) {
			s.Rune.BankEssence, s.Rune.CanBuy, s.Rune.BuyQty = 0, true, 2000
		}, ModeSwarm, "no essence source"},
	}
	for _, c := range cases {
		set := runeSet()
		if c.set != nil {
			c.set(&set)
		}
		d := Decide(set, runeSnap(c.snap))
		if d.Mode != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, d.Mode, d.Why, c.want)
			continue
		}
		if c.why != "" && !strings.Contains(d.Why, c.why) {
			t.Errorf("%s: why %q lacks %q", c.name, d.Why, c.why)
		}
		if d.Mode == ModeRunecraft && d.Resume {
			t.Errorf("%s: a new round is not a resume", c.name)
		}
	}
}

// The round ranks after the jewellery round and before the batch rounds: a gem round the mule's stock keeps due
// does not pull the main account away from the altar; with the switch off the gem round is back.
func TestRuneRanksBeforeTheBatchRounds(t *testing.T) {
	gems := func(s *Snapshot) {
		s.Stock = stockMap(map[string]int{"Uncut sapphire": 600, "Chisel": 1, "Air talisman": 5})
	}
	set := runeSet()
	set.GemThreshold = 500
	if d := Decide(set, runeSnap(gems)); d.Mode != ModeRunecraft {
		t.Fatalf("with 600 uncut sapphires waiting: %+v", d)
	}
	set.Rune.On = false
	if d := Decide(set, runeSnap(gems)); d.Mode != ModeGems {
		t.Fatalf("switched off: %+v", d)
	}
	// Products waiting to be handed over are the mule's: the hand-over comes first (a round NOT under way).
	set.Rune.On = true
	if d := Decide(set, runeSnap(func(s *Snapshot) { gems(s); s.Ledger.PendingDeliver = true })); d.Mode != ModeDeliver {
		t.Fatalf("a pending hand-over: %+v", d)
	}
}

// A round under way is decided before the mule is heard, before a pending hand-over, and whatever the level:
// a restart inside a temple must not leave ScarabSwarm to walk from there.
func TestARuneRoundUnderWayCarriesOn(t *testing.T) {
	set := runeSet()
	under := func(mod func(s *Snapshot)) Decision {
		return Decide(set, runeSnap(func(s *Snapshot) {
			s.MuleHeard, s.MuleSameWorld = false, false // the bus is empty after a restart
			s.Ledger.PendingDeliver = true
			mod(s)
		}))
	}
	for name, mod := range map[string]func(s *Snapshot){
		"in the Air temple":  func(s *Snapshot) { s.Rune.Where, s.Rune.Temple = runecraftplan.RegionTemple, runecraftplan.KeyAir },
		"in the Body temple": func(s *Snapshot) { s.Rune.Where, s.Rune.Temple = runecraftplan.RegionTemple, runecraftplan.KeyBody },
		"in the essence mine": func(s *Snapshot) {
			s.Rune.Where = runecraftplan.RegionMine
		},
		"on the road with a load and the talisman": func(s *Snapshot) {
			s.Rune.PackEssence, s.Rune.PackTalisman = 27, map[string]bool{runecraftplan.KeyAir: true}
		},
		"in a temple at the target level (to be got out)": func(s *Snapshot) {
			s.Rune.Where, s.Rune.Temple, s.Rune.Level, s.Rune.XP = runecraftplan.RegionTemple, runecraftplan.KeyBody, 44, 55649
		},
	} {
		d := under(mod)
		if d.Mode != ModeRunecraft || !d.Resume || !strings.Contains(d.Why, "under way") {
			t.Errorf("%s: %+v", name, d)
		}
	}
	// Not under way: a load without the talisman, a few essence, or the switch off.
	for name, mod := range map[string]func(s *Snapshot){
		"a load without the talisman": func(s *Snapshot) { s.Rune.PackEssence = 27 },
		"a handful of essence": func(s *Snapshot) {
			s.Rune.PackEssence, s.Rune.PackTalisman = 5, map[string]bool{runecraftplan.KeyAir: true}
		},
		"the wrong talisman for the level": func(s *Snapshot) {
			s.Rune.Level, s.Rune.XP = 20, 4470
			s.Rune.PackEssence, s.Rune.PackTalisman = 27, map[string]bool{runecraftplan.KeyAir: true}
		},
	} {
		if d := under(mod); d.Resume && d.Mode == ModeRunecraft {
			t.Errorf("%s: %+v", name, d)
		}
	}
	set.Rune.On = false
	if d := under(func(s *Snapshot) { s.Rune.Where, s.Rune.Temple = runecraftplan.RegionTemple, runecraftplan.KeyAir }); d.Mode == ModeRunecraft {
		t.Errorf("switched off: %+v", d)
	}
}

// Chest loot still owed to the mule comes before a NEW levelling round; a round under way goes on first.
func TestChestOwedComesBeforeANewRuneRound(t *testing.T) {
	set := runeSet()
	snap := func(mod func(s *Snapshot)) Snapshot {
		return runeSnap(func(s *Snapshot) {
			s.Ledger.Chest.Owed = map[string]int{"995": 5}
			if mod != nil {
				mod(s)
			}
		})
	}
	if d := DecideWithChest(set, snap(nil), ChestInput{}); d.Mode != ModeChest || !d.Resume {
		t.Fatalf("owed loot and a new round: %+v", d)
	}
	under := snap(func(s *Snapshot) { s.Rune.Where, s.Rune.Temple = runecraftplan.RegionTemple, runecraftplan.KeyAir })
	if d := DecideWithChest(set, under, ChestInput{}); d.Mode != ModeRunecraft || !d.Resume {
		t.Fatalf("owed loot and a round under way: %+v", d)
	}
	// Nothing owed: the levelling round.
	if d := DecideWithChest(set, runeSnap(nil), ChestInput{}); d.Mode != ModeRunecraft {
		t.Fatalf("nothing owed: %+v", d)
	}
}

func TestARuneRoundIsNotChainedToItself(t *testing.T) {
	if Chain(ModeRunecraft, Decision{Mode: ModeRunecraft}) {
		t.Error("a fresh levelling round straight after a levelling round")
	}
	if !Chain(ModeRunecraft, Decision{Mode: ModeRunecraft, Resume: true}) {
		t.Error("a round under way is chained")
	}
	if !Chain(ModeRunecraft, Decision{Mode: ModeDeliver, Resume: true}) {
		t.Error("a hand-over after the round")
	}
}

func TestRuneLedgerRoundTripsAndCounts(t *testing.T) {
	var l Ledger
	l.Rune.NoteCraft(runecraftplan.KeyAir, 27, 27, 135)
	l.Rune.NoteCraft(runecraftplan.KeyAir, 27, 54, 135)
	l.Rune.NoteCraft(runecraftplan.KeyBody, 27, 27, 202)
	l.Rune.NoteBank(540, map[string]int{runecraftplan.KeyAir: 2, runecraftplan.KeyBody: 0}, runecraftplan.Kit{Law: 4, Air: 20}, runeNow)
	l.Rune.Rounds, l.Rune.Mined, l.Rune.PauseUntil, l.Rune.LastStop = 2, 1026, runeNow.Add(time.Hour), "no essence source"
	if l.Rune.Crafts != 3 || l.Rune.EssenceCrafted != 81 || l.Rune.XPGained != 472 || l.Rune.RunesMade[runecraftplan.KeyAir] != 81 || l.Rune.RunesMade[runecraftplan.KeyBody] != 27 {
		t.Fatalf("counters %+v", l.Rune)
	}
	if !reflect.DeepEqual(l.Rune.BankTalisman, map[string]int{runecraftplan.KeyAir: 2}) || !l.Rune.Known() {
		t.Fatalf("bank %+v", l.Rune)
	}
	raw, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	var back Ledger
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Rune, l.Rune) {
		t.Fatalf("round trip:\n got %+v\nwant %+v\njson %s", back.Rune, l.Rune, raw)
	}
	var empty RuneLedger
	if empty.Known() {
		t.Error("an empty ledger knows a bank")
	}
	empty.NoteBank(0, nil, runecraftplan.Kit{}, runeNow)
	if empty.BankTalisman != nil {
		t.Errorf("empty talismans kept: %v", empty.BankTalisman)
	}
}

func TestRuneKeepNames(t *testing.T) {
	set := runeSet()
	got := RuneKeepNames(set, 5)
	for _, want := range []string{"Rune essence", "Air talisman", "Body talisman"} {
		found := false
		for _, g := range got {
			found = found || g == want
		}
		if !found {
			t.Errorf("%q is not kept: %v", want, got)
		}
	}
	for _, g := range got {
		if g == "Nature talisman" {
			t.Errorf("the Nature talisman is not this phase's: %v", got)
		}
	}
	if RuneKeepNames(set, 44) != nil {
		t.Error("kept at the target")
	}
	set.Rune.On = false
	if RuneKeepNames(set, 5) != nil {
		t.Error("kept while off")
	}
}

// The ledger counts the market lots for good: the budget is spent once, across rounds and restarts.
func TestRuneLedgerCountsMarketLots(t *testing.T) {
	var l RuneLedger
	l.NoteMarket(0, 0) // nothing bought, nothing paid: not a lot
	l.NoteMarket(-5, -5)
	if l.MarketLots != 0 || l.MarketSpent != 0 {
		t.Fatalf("%+v", l)
	}
	l.NoteMarket(240_000, 2400)
	l.NoteMarket(10_500, 100)
	if l.MarketSpent != 250_500 || l.MarketBought != 2500 || l.MarketLots != 2 {
		t.Fatalf("%+v", l)
	}
	raw, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	var back RuneLedger
	if err := json.Unmarshal(raw, &back); err != nil || back.MarketSpent != 250_500 || back.MarketBought != 2500 || back.MarketLots != 2 {
		t.Fatalf("%s -> %+v %v", raw, back, err)
	}
}
