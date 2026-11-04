package bowstringplan

import (
	"strings"
	"testing"
)

func TestLevelsMatchContent(t *testing.T) {
	// configs/stringing/bows.dbrow, content 289.
	want := map[string]int{
		"Yew shortbow": 65, "Maple longbow": 55, "Maple shortbow": 50, "Willow longbow": 40,
		"Willow shortbow": 35, "Oak longbow": 25, "Oak shortbow": 20, "Longbow": 10, "Shortbow": 5,
		"Yew longbow": 70, "Magic shortbow": 80, "Magic longbow": 85,
	}
	for name, lvl := range want {
		if got, ok := LevelFor(name); !ok || got != lvl {
			t.Errorf("LevelFor(%q) = %d,%v, want %d", name, got, ok, lvl)
		}
	}
	for _, n := range DefaultBowNames() {
		if _, ok := LevelFor(n); !ok {
			t.Errorf("default bow %q has no level", n)
		}
	}
}

func TestPickBowByLevel(t *testing.T) {
	names := DefaultBowNames()
	cases := []struct {
		level     int
		exhausted []string
		want      string
		ok        bool
	}{
		{69, nil, "Yew shortbow", true},
		{65, nil, "Yew shortbow", true},
		{64, nil, "Maple longbow", true},
		{69, []string{"yew shortbow"}, "Maple longbow", true},
		{69, []string{"Yew shortbow", "maple longbow"}, "Willow longbow", true}, // Maple shortbows wait: Maple longbows are in reach
		{52, nil, "Maple shortbow", true},
		{5, nil, "Shortbow", true},
		{4, nil, "", false},
	}
	for _, c := range cases {
		ex := map[string]bool{}
		for _, e := range c.exhausted {
			ex[Key(e)] = true
		}
		got, ok := PickBow(names, c.level, ex)
		if ok != c.ok || got.Name != c.want {
			t.Errorf("PickBow(level %d, exhausted %v) = %q,%v, want %q,%v", c.level, c.exhausted, got.Name, ok, c.want, c.ok)
		}
	}
	// The list's order does not matter: the highest level allowed wins.
	if got, _ := PickBow([]string{"willow longbow", "Maple longbow"}, 69, nil); got.Name != "Maple longbow" {
		t.Errorf("PickBow picked %q, want the higher-level Maple longbow", got.Name)
	}
}

func TestUnstrungIDByCost(t *testing.T) {
	// Maple longbow: unstrung obj 62 (cost 320), its note, the strung bow (cost 640) and its note.
	un, st, ok := UnstrungID([]ObjInfo{
		{ID: 62, Cost: 320}, {ID: 63, Cost: 320, Noted: true}, {ID: 851, Cost: 640}, {ID: 852, Cost: 640, Noted: true},
	})
	if !ok || un != 62 || st != 851 {
		t.Fatalf("UnstrungID = %d,%d,%v, want 62,851,true", un, st, ok)
	}
	// Id order does not decide: a cheaper higher id is the unstrung one.
	if un, _, ok := UnstrungID([]ObjInfo{{ID: 10, Cost: 500}, {ID: 20, Cost: 50}}); !ok || un != 20 {
		t.Errorf("UnstrungID picked %d, want 20 (the cheaper)", un)
	}
	// A lone id (only notes beside it) or a tie cannot be told apart.
	if _, _, ok := UnstrungID([]ObjInfo{{ID: 62, Cost: 64}, {ID: 63, Cost: 64, Noted: true}}); ok {
		t.Error("UnstrungID accepted a single unnoted id")
	}
	if _, _, ok := UnstrungID([]ObjInfo{{ID: 1, Cost: 5}, {ID: 2, Cost: 5}}); ok {
		t.Error("UnstrungID accepted two ids of equal cost")
	}
}

func TestWithdrawBatch(t *testing.T) {
	cases := []struct{ bows, strs, free, want int }{
		{1000, 1000, 28, 14},
		{5, 1000, 28, 5},
		{1000, 3, 28, 3},
		{1000, 1000, 10, 5},
		{0, 1000, 28, 0},
		{1000, 0, 28, 0},
		{1000, 1000, 1, 0},
	}
	for _, c := range cases {
		if got := Withdraw(c.bows, c.strs, c.free); got != c.want {
			t.Errorf("Withdraw(%d,%d,%d) = %d, want %d", c.bows, c.strs, c.free, got, c.want)
		}
	}
}

func TestDecide(t *testing.T) {
	if Decide(Snapshot{PackBows: 14, PackStrings: 14}) != StepString {
		t.Error("a full batch should string")
	}
	if Decide(Snapshot{PackBows: 1, PackStrings: 1}) != StepString {
		t.Error("one pair should string")
	}
	for _, s := range []Snapshot{{}, {PackBows: 14}, {PackStrings: 14}} {
		if Decide(s) != StepBank {
			t.Errorf("Decide(%+v) should bank", s)
		}
	}
}

func stock(m map[string]int) MuleStock {
	return func(item string) (int, bool) {
		n, ok := m[strings.ToLower(item)]
		return n, ok
	}
}

func TestPlanOrder(t *testing.T) {
	// Bank empty, mule full: 1000 bows and 1000 strings.
	o := PlanOrder("Maple longbow", 0, 0, 1000, stock(map[string]int{"maple longbow": 172275, "bow string": 122380}))
	if o.Exhausted || len(o.Lines) != 2 || o.Lines[0] != (KitLine{"Maple longbow", 1000}) || o.Lines[1] != (KitLine{BowString, 1000}) {
		t.Fatalf("empty bank: %+v", o)
	}
	// Bank has bows but no strings: strings only, one per bow held.
	o = PlanOrder("Maple longbow", 500, 0, 1000, nil)
	if len(o.Lines) != 1 || o.Lines[0] != (KitLine{BowString, 500}) {
		t.Fatalf("bows, no strings: %+v", o)
	}
	// Bank has strings but under a batch of bows: bows, and strings to match the rest.
	o = PlanOrder("Maple longbow", 3, 400, 1000, nil)
	if len(o.Lines) != 2 || o.Lines[0].Qty != 1000 || o.Lines[1].Qty != 603 {
		t.Fatalf("few bows, some strings: %+v", o)
	}
	// Plenty of strings: bows only.
	o = PlanOrder("Maple longbow", 0, 5000, 1000, nil)
	if len(o.Lines) != 1 || o.Lines[0] != (KitLine{"Maple longbow", 1000}) {
		t.Fatalf("strings enough: %+v", o)
	}
	// The kit is clamped to the mule's per-order cap and to what it publishes.
	o = PlanOrder("Yew shortbow", 0, 0, 5000, stock(map[string]int{"yew shortbow": 300, "bow string": 100}))
	if len(o.Lines) != 2 || o.Lines[0].Qty != 300 || o.Lines[1].Qty != 100 {
		t.Fatalf("clamped: %+v", o)
	}
	o = PlanOrder("Yew shortbow", 0, 0, KitMax+5000, nil)
	if o.Lines[0].Qty != KitMax {
		t.Fatalf("kit not clamped to KitMax: %+v", o)
	}
	// The mule publishes none of the bow: exhausted, nothing asked.
	o = PlanOrder("Yew shortbow", 0, 0, 1000, stock(map[string]int{"yew shortbow": 0, "bow string": 5}))
	if !o.Exhausted || len(o.Lines) != 0 {
		t.Fatalf("mule empty: %+v", o)
	}
	// No strings anywhere: nothing to ask, not exhausted (it is the strings that ran out).
	o = PlanOrder("Yew shortbow", 0, 0, 1000, stock(map[string]int{"yew shortbow": 50, "bow string": 0}))
	if o.Exhausted || len(o.Lines) != 0 || o.Why == "" {
		t.Fatalf("no strings: %+v", o)
	}
	// Unpublished items are asked for blind.
	o = PlanOrder("Oak longbow", 0, 0, 200, stock(map[string]int{}))
	if len(o.Lines) != 2 || o.Lines[0].Qty != 200 {
		t.Fatalf("blind: %+v", o)
	}
}

func TestKitOutcomes(t *testing.T) {
	if ClassifyKit(14, true, "") != KitGained {
		t.Error("a gain is gained")
	}
	if ClassifyKit(0, false, "none in the bank") != KitEmpty {
		t.Error("none in the bank is empty")
	}
	if ClassifyKit(0, true, "") != KitNoBows {
		t.Error("a trade with no unstrung bows is no-bows")
	}
	if ClassifyKit(0, false, "already handed you one recently") != KitDeferred {
		t.Error("the per-donor window is a deferral")
	}
	// Deferrals never exhaust, however many.
	f := 0
	var ex bool
	for i := 0; i < 20; i++ {
		f, ex = AfterKit(KitDeferred, f, 3)
		if ex || f != 0 {
			t.Fatalf("deferral %d: fruitless %d exhausted %v", i, f, ex)
		}
	}
	f, ex = AfterKit(KitNoBows, 0, 3)
	f, ex = AfterKit(KitNoBows, f, 3)
	if ex || f != 2 {
		t.Fatalf("two no-bows: %d %v", f, ex)
	}
	if f, ex = AfterKit(KitGained, f, 3); ex || f != 0 {
		t.Fatalf("a gain resets: %d %v", f, ex)
	}
	f = 2
	if _, ex = AfterKit(KitNoBows, f, 3); !ex {
		t.Fatal("the third no-bows in a row should exhaust")
	}
	if _, ex = AfterKit(KitEmpty, 0, 3); !ex {
		t.Fatal("an empty decline exhausts at once")
	}
	if _, ex = AfterKit(KitNoBows, 50, 0); ex {
		t.Fatal("maxFruitless 0 never exhausts on no-bows")
	}
}

func TestDecodeSettings(t *testing.T) {
	s, err := DecodeSettings(nil)
	if err != nil || s.Mule != DefaultMule || s.KitSize != DefaultKitSize || len(s.Bows) != len(DefaultBowNames()) {
		t.Fatalf("defaults: %+v %v", s, err)
	}
	s, err = DecodeSettings(map[string]interface{}{
		"mule": "mule", "bows": []interface{}{"Maple longbow", "Willow longbow"}, "kitSize": float64(500),
		"orderBackoffMinutes": "10", "logDetail": "Verbose", "pinWorld": float64(1),
	})
	if err != nil || s.KitSize != 500 || s.OrderBackoffMinutes != 10 || len(s.Bows) != 2 || !s.Verbose() {
		t.Fatalf("custom: %+v %v", s, err)
	}
	if s, err = DecodeSettings(map[string]interface{}{"bows": "Yew shortbow, Maple longbow"}); err != nil || len(s.Bows) != 2 {
		t.Fatalf("comma list: %+v %v", s, err)
	}
	for _, bad := range []map[string]interface{}{
		{"muleAt": float64(5000)},
		{"kitSize": float64(0)},
		{"kitSize": float64(KitMax + 1)},
		{"kitSize": "lots"},
		{"bows": []interface{}{"Crossbow"}},
		{"logDetail": "Loud"},
		{"mule": ""},
		{"orderBackoffMinutes": float64(0)},
	} {
		if _, err := DecodeSettings(bad); err == nil {
			t.Errorf("DecodeSettings(%v) accepted", bad)
		}
	}
}

func TestStringableSkipsShortbowsWhileTheLongbowIsInReach(t *testing.T) {
	for _, c := range []struct {
		name string
		lvl  int
		want bool
	}{
		{"Magic shortbow", 99, false}, {"Magic shortbow", 84, true}, {"Magic longbow", 85, true},
		{"Yew shortbow", 69, true}, {"Yew shortbow", 70, false}, {"Shortbow", 10, false}, {"Shortbow", 9, true},
		{"Magic longbow", 84, false}, {"Not a bow", 99, false},
	} {
		if got := Stringable(c.name, c.lvl); got != c.want {
			t.Errorf("Stringable(%q, %d) = %v, want %v", c.name, c.lvl, got, c.want)
		}
	}
}
