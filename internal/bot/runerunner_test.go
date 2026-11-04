package bot

import (
	"sort"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/runerunnerplan"
	"rsb0t-go/internal/settings"
)

func TestRuneRunnerRegistered(t *testing.T) {
	b, ok := New("RuneRunner")
	if !ok {
		t.Fatal("RuneRunner is not registered")
	}
	if _, ok := b.(*runeRunnerBot); !ok {
		t.Fatalf("New(RuneRunner) = %T", b)
	}
	found := false
	for _, n := range Names() {
		if n == "RuneRunner" {
			found = true
		}
	}
	if !found {
		t.Error("Names() lacks RuneRunner")
	}
}

// The panel schema and the plan's strict struct are the same keys: a key in one and not the other
// is either a setting the panel offers and the decode refuses, or one the decode takes and the
// panel hides.
func TestRuneRunnerSchemaMatchesPlanSettings(t *testing.T) {
	var schemaKeys []string
	for k := range RuneRunnerSettings {
		schemaKeys = append(schemaKeys, k)
	}
	sort.Strings(schemaKeys)
	if got, want := strings.Join(schemaKeys, ","), strings.Join(runerunnerplan.SettingKeys(), ","); got != want {
		t.Fatalf("schema keys\n  %s\nplan Settings tags\n  %s", got, want)
	}
	// Each schema default is the plan's default.
	s, err := decodeRuneRunnerSettings(map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	d := runerunnerplan.DefaultSettings().Filled()
	if s.Mule != d.Mule || s.TripCoins != d.TripCoins || s.FareCoins != d.FareCoins || s.TickMs != d.TickMs ||
		s.CheckpointDir != d.CheckpointDir || strings.Join(s.Shops, ",") != strings.Join(d.Shops, ",") ||
		strings.Join(s.PriceCaps, ",") != strings.Join(d.PriceCaps, ",") || s.DeliverMaxAgeMinutes != d.DeliverMaxAgeMinutes ||
		s.IdleMaxMinutes != d.IdleMaxMinutes || s.MaxHopsPerRound != d.MaxHopsPerRound || s.LogDetail != d.LogDetail ||
		strings.Join(s.Runes, ",") != strings.Join(d.Runes, ",") || s.ZaffStaffs != d.ZaffStaffs || s.ZaffStaffCap != d.ZaffStaffCap {
		t.Errorf("schema defaults decode to %+v, plan defaults are %+v", s.Settings, d)
	}
}

func TestRuneRunnerSettingsDecode(t *testing.T) {
	// The panel's strings are typed; worlds may be numbers.
	s, err := decodeRuneRunnerSettings(map[string]interface{}{
		"tripCoins": "50000", "worlds": []interface{}{1.0, 2.0}, "logDetail": "verbose",
		"priceCaps": "Mind rune=12,Air rune=6,Chaos rune=45", "shops": "Aubury,Betty", "rotate": "off",
		"zaffStaffs": "2", "zaffStaffCap": 1550.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.TripCoins != 50000 || len(s.Worlds) != 2 || s.Worlds[1] != 2 || s.LogDetail != "Verbose" ||
		s.CapTable["Chaos rune"] != 45 || s.CapTable["Air rune"] != 6 || s.Enabled("Lundail") ||
		s.ZaffStaffs != 2 || s.ZaffStaffCap != 1550 || !s.Staffs() {
		t.Errorf("decoded %+v", s)
	}
	for _, c := range []struct {
		name string
		raw  map[string]interface{}
		want string
	}{
		{"out of range is refused, not clamped", map[string]interface{}{"tripCoins": 500.0}, "tripCoins"},
		{"a non-number", map[string]interface{}{"fareCoins": "lots"}, "not a number"},
		{"an unmatched option", map[string]interface{}{"logDetail": "Loud"}, "not one of"},
		{"a list of numbers where runes go", map[string]interface{}{"runes": []interface{}{1.0}}, "not a string"},
		{"unknown key", map[string]interface{}{"budget": 5.0}, "unknown RuneRunner setting"},
		{"pinWorld", map[string]interface{}{"pinWorld": 2.0}, "RuneRunner hops worlds; remove pinWorld"},
		{"too many staffs is refused, not clamped", map[string]interface{}{"zaffStaffs": 11.0}, "zaffStaffs"},
		{"a staff cap of 0", map[string]interface{}{"zaffStaffCap": 0.0}, "zaffStaffCap"},
		{"runes without an air cap", map[string]interface{}{"priceCaps": []interface{}{"Mind rune=18", "Chaos rune=90"}}, "Blood rune has no cap"},
	} {
		if _, err := decodeRuneRunnerSettings(c.raw); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

// OnStart refuses pinWorld and an empty mule before it touches anything else.
func TestRuneRunnerOnStartRefusals(t *testing.T) {
	for _, c := range []struct {
		name string
		set  map[string]interface{}
		want string
	}{
		{"pinWorld", map[string]interface{}{"pinWorld": 1.0}, "RuneRunner hops worlds; remove pinWorld"},
		{"empty mule", map[string]interface{}{"mule": ""}, "mule:"},
		{"blank mule", map[string]interface{}{"mule": "   "}, "mule:"},
	} {
		ctx := NewContext(nil, "buyer", nil, nil, nil, nil, nil, nil, nil, c.set, nil)
		err := (&runeRunnerBot{}).OnStart(ctx)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	// Good settings get as far as the collision pack.
	ctx := NewContext(nil, "buyer", nil, nil, nil, nil, nil, nil, nil, map[string]interface{}{"checkpointDir": t.TempDir()}, nil)
	if err := (&runeRunnerBot{}).OnStart(ctx); err == nil || !strings.Contains(err.Error(), "collision pack") {
		t.Errorf("no pack: %v", err)
	}
}

func startedRuneRunner(t *testing.T, dir string, lines *[]string) (*runeRunnerBot, *Context) {
	t.Helper()
	ctx := NewContext(nil, "buyer", nil, nil, nil, nil, nil, nil, nil, map[string]interface{}{"checkpointDir": dir},
		func(l string) { *lines = append(*lines, l) })
	b := &runeRunnerBot{said: map[string]time.Time{}}
	set, err := decodeRuneRunnerSettings(ctx.Settings)
	if err != nil {
		t.Fatal(err)
	}
	b.set = set
	if err := b.startState(ctx); err != nil {
		t.Fatal(err)
	}
	return b, ctx
}

// A hop restarts the script: the state it saved says so, and the price-check disables survive it;
// any other restart is an operator's, which clears them.
func TestRuneRunnerStateAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	b, ctx := startedRuneRunner(t, dir, &lines)
	if b.hopsOff == "" {
		t.Error("a Context with no fleet hooks cannot hop")
	}
	b.st.StartRound(runerunnerplan.BuildRing(runerunnerplan.RingInput{M: 1, O: 2, Shops: runerunnerplan.BuiltSites(), OBlock: true}), 4, time.Now())
	b.st.NotePrice("Betty@1", 1000, 2000)
	b.st.NotePrice("Betty@1", 1000, 2000)
	b.st.HopPending = 2
	b.save(ctx)

	b2, _ := startedRuneRunner(t, dir, &lines)
	if b2.st.HopPending != 0 || b2.st.Cursor != 4 || b2.st.Round != 1 {
		t.Errorf("after the hop: pending %d cursor %d round %d", b2.st.HopPending, b2.st.Cursor, b2.st.Round)
	}
	if off, _ := b2.st.PairOff("Betty@1", time.Now()); !off {
		t.Error("a hop restart must not clear a price-check disable")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "resumed after the ring's hop to world 2") {
		t.Errorf("no resume line in %q", lines)
	}

	b3, _ := startedRuneRunner(t, dir, &lines)
	if off, _ := b3.st.PairOff("Betty@1", time.Now()); off {
		t.Error("an operator restart clears the price-check disable")
	}
}

// A state file this build cannot read refuses the start and is never overwritten.
func TestRuneRunnerUnreadableStateRefuses(t *testing.T) {
	dir := t.TempDir()
	bag := settings.NewBag()
	bag.Set("round", "three")
	if err := settings.NewStore(dir).Save("buyer", runerunnerplan.StateStoreID, bag); err != nil {
		t.Fatal(err)
	}
	ctx := NewContext(nil, "buyer", nil, nil, nil, nil, nil, nil, nil, map[string]interface{}{"checkpointDir": dir}, nil)
	b := &runeRunnerBot{said: map[string]time.Time{}}
	b.set, _ = decodeRuneRunnerSettings(ctx.Settings)
	if err := b.startState(ctx); err == nil || !strings.Contains(err.Error(), "cannot be read") {
		t.Fatalf("unreadable state: %v", err)
	}
	b.st.Round = 99
	b.save(ctx)
	back, err := settings.NewStore(dir).Load("buyer", runerunnerplan.StateStoreID)
	if err != nil || back.Raw()["round"] != "three" {
		t.Errorf("the unreadable file was overwritten: %v %v", back.Raw(), err)
	}
}
