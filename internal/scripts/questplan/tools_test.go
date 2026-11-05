package questplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Lane N4: the tool lines (tools.go). A hammer, a pickaxe or a tinderbox that neither the bank nor the mule holds is bought,
// not blocked on.

func toolSnap(tileAt tile.Tile, known bool, mining int, pack, worn, bank map[int]int) Snapshot {
	s := Snapshot{Tile: tileAt, TileKnown: known, Levels: map[string]int{"mining": mining}, InvIDs: pack, WornIDs: worn,
		BankKnown: bank != nil, BankIDs: bank}
	if s.InvIDs == nil {
		s.InvIDs = map[int]int{}
	}
	if s.WornIDs == nil {
		s.WornIDs = map[int]int{}
	}
	return s
}

// The done-when of lane N4: a hammer, a pickaxe and a tinderbox line each resolve through a shop when the bank and the mule
// answer nothing.
func TestHammerPickaxeAndTinderboxLinesResolveThroughAShop(t *testing.T) {
	for _, kind := range []ToolKind{ToolHammer, ToolPickaxe, ToolTinderbox} {
		line := ToolLine(kind, toolSnap(tile.Tile{}, false, 1, nil, nil, map[int]int{}))
		if why := ValidateKitLine(line); why != "" {
			t.Fatalf("%s: %s", kind, why)
		}
		st := ProvisionState{BankKnown: true, Bank: map[int]int{}, MuleKnown: true, Mule: map[int]int{}, FreeSlots: 28,
			Pack: map[int]int{CoinsID: 100}}
		p := PlanProvision([]KitLine{line}, st)
		if p.Phase != PhaseSource || len(p.Tasks) != 1 || p.Tasks[0].Source.Kind != SourceShop || p.Tasks[0].Line.ID != line.ID {
			t.Errorf("%s: an empty bank and mule must send the account to a shop: %+v", kind, p)
			continue
		}
		if p.Tasks[0].Source.Shop.Npc == "" || p.Tasks[0].Source.Shop.Anchor == (tile.Tile{}) || p.Tasks[0].Source.Price <= 0 {
			t.Errorf("%s: the shop task is not complete: %+v", kind, p.Tasks[0].Source)
		}
		if p.ShopCoins != p.Tasks[0].Source.Price {
			t.Errorf("%s: shop coins %d for one tool at %d", kind, p.ShopCoins, p.Tasks[0].Source.Price)
		}
	}
}

// The item rule's order holds for a tool line: the bank, then the mule, then the shop.
func TestAToolLineFollowsTheItemRule(t *testing.T) {
	line := ToolLine(ToolHammer, Snapshot{})
	base := ProvisionState{BankKnown: true, Bank: map[int]int{}, MuleKnown: true, Mule: map[int]int{}, FreeSlots: 28, Pack: map[int]int{CoinsID: 100}}

	bank := base
	bank.Bank = map[int]int{ObjHammer: 1}
	if p := PlanProvision([]KitLine{line}, bank); p.Phase != PhaseBank || len(p.Withdraw) != 1 || p.Withdraw[0].ID != ObjHammer {
		t.Errorf("the bank first: %+v", p)
	}
	mule := base
	mule.Mule = map[int]int{ObjHammer: 4}
	if p := PlanProvision([]KitLine{line}, mule); p.Phase != PhaseMule || len(p.MuleAsk) != 1 || p.MuleAsk[0].ID != ObjHammer {
		t.Errorf("the mule second: %+v", p)
	}
	held := base
	held.Pack = map[int]int{ObjHammer: 1}
	if p := PlanProvision([]KitLine{line}, held); p.Phase != PhaseSatisfied {
		t.Errorf("a held hammer needs nothing: %+v", p)
	}
	worn := base
	worn.Worn = map[int]int{ObjHammer: 1}
	if p := PlanProvision([]KitLine{line}, worn); p.Phase != PhaseSatisfied {
		t.Errorf("a wielded hammer is held: %+v", p)
	}
}

// The nearest shop is tried first; the second is the fall-through when the first is empty.
func TestAToolLineTriesTheNearestShopFirst(t *testing.T) {
	for _, c := range []struct {
		name  string
		at    tile.Tile
		known bool
		first string
	}{
		{"unknown position: the table's order", tile.Tile{}, false, LumbridgeStore.Npc},
		{"in Lumbridge", tile.Tile{X: 3222, Z: 3218}, true, LumbridgeStore.Npc},
		{"in Varrock", tile.Tile{X: 3213, Z: 3428}, true, VarrockGenStore.Npc},
		{"in Catherby", tile.Tile{X: 2810, Z: 3440}, true, ArheinStore.Npc},
	} {
		line := ToolLine(ToolHammer, toolSnap(c.at, c.known, 1, nil, nil, nil))
		if len(line.Sources) != 3 || line.Sources[0].Shop.Npc != c.first {
			t.Errorf("%s: first shop %q of %d, want %q", c.name, line.Sources[0].Shop.Npc, len(line.Sources), c.first)
		}
	}
	line := ToolLine(ToolHammer, toolSnap(tile.Tile{X: 3213, Z: 3428}, true, 1, nil, nil, nil))
	st := ProvisionState{BankKnown: true, Bank: map[int]int{}, FreeSlots: 28, Pack: map[int]int{CoinsID: 100},
		Exhausted: map[string]bool{SourceKey(line, line.Sources[0]): true}}
	p := PlanProvision([]KitLine{line}, st)
	if p.Phase != PhaseSource || len(p.Tasks) != 1 {
		t.Fatalf("an empty first shop must fall through to the next: %+v", p)
	}
	if got := p.Tasks[0].Source.Shop; got == VarrockGenStore {
		t.Errorf("the failed shop %+v was chosen again", got)
	}
}

func TestAPickaxeLineIsTieredLikeAnAxe(t *testing.T) {
	runeAxe, steel, bronze, iron := 1275, 1269, ObjBronzePickaxe, 1267
	for _, c := range []struct {
		name   string
		mining int
		pack   map[int]int
		worn   map[int]int
		bank   map[int]int
		want   int
		shops  bool // the line falls to the shops
	}{
		{"a usable pickaxe in the pack is the line", 10, map[int]int{steel: 1}, nil, nil, steel, false},
		{"a worn one too", 10, nil, map[int]int{iron: 1}, nil, iron, false},
		{"a rune pickaxe in the pack is too high at 30", 30, map[int]int{runeAxe: 1}, nil, map[int]int{}, bronze, true},
		{"the best usable banked one", 30, nil, nil, map[int]int{steel: 1, bronze: 1, runeAxe: 1}, steel, false},
		{"the rune one at 41", 41, nil, nil, map[int]int{steel: 1, runeAxe: 1}, runeAxe, false},
		{"an unread bank is not an empty one, and is not trusted", 30, nil, nil, nil, bronze, true},
		{"an empty bank buys", 1, nil, nil, map[int]int{}, bronze, true},
		{"a level the snapshot does not know judges none too high", 0, nil, nil, map[int]int{runeAxe: 1}, runeAxe, false},
	} {
		l := PickaxeLine(toolSnap(tile.Tile{}, false, c.mining, c.pack, c.worn, c.bank))
		if l.ID != c.want || (len(l.Sources) > 0) != c.shops || !l.Worn {
			t.Errorf("%s: line %+v, want pickaxe %d, shops %v", c.name, l, c.want, c.shops)
		}
		if why := ValidateKitLine(l); why != "" {
			t.Errorf("%s: %s", c.name, why)
		}
	}
}

func TestToolHeldAndToolLinesSkipWhatThePackHolds(t *testing.T) {
	s := toolSnap(tile.Tile{}, false, 10, map[int]int{ObjHammer: 1, 1269: 1}, map[int]int{ObjTinderbox: 1}, nil)
	for _, k := range []ToolKind{ToolHammer, ToolPickaxe, ToolTinderbox} {
		if !ToolHeld(k, s) {
			t.Errorf("%s is held", k)
		}
	}
	if ToolHeld(ToolKnife, s) || ToolHeld("no such tool", s) {
		t.Error("a tool the pack lacks is held")
	}
	lines := ToolLines(s, ToolHammer, ToolKnife, ToolPickaxe, ToolChisel)
	if len(lines) != 2 || lines[0].ID != ObjKnife || lines[1].ID != DigObjChisel {
		t.Errorf("only the knife and the chisel are lacking: %+v", lines)
	}
	// A pickaxe the Mining level cannot use is not held.
	if ToolHeld(ToolPickaxe, toolSnap(tile.Tile{}, false, 10, map[int]int{1275: 1}, nil, nil)) {
		t.Error("a rune pickaxe at Mining 10 counts as held")
	}
	if l := ToolLine("no such tool", s); ValidateKitLine(l) == "" {
		t.Error("an unknown tool must give a line ValidateKitLine rejects")
	}
}

func TestToolIDsListEveryPickaxeAndTheOtherToolsOnce(t *testing.T) {
	ids := ToolIDs(ToolPickaxe, ToolHammer, ToolTinderbox)
	if len(ids) != len(PickaxeIDs())+2 {
		t.Fatalf("ids %v", ids)
	}
	have := map[int]bool{}
	for _, id := range ids {
		have[id] = true
	}
	for _, want := range []int{1265, 1267, 1269, 1271, 1273, 1275, ObjHammer, ObjTinderbox} {
		if !have[want] {
			t.Errorf("the keep list lacks %d", want)
		}
	}
}

// The table is well formed: one row per kind, every tool has a shop that is complete, and no tool's id is another's.
func TestTheToolTableIsWellFormed(t *testing.T) {
	kinds, ids := map[ToolKind]bool{}, map[int]string{}
	for _, tool := range Tools {
		if tool.Kind == "" || tool.ID <= 0 || tool.Name == "" {
			t.Errorf("incomplete row %+v", tool)
		}
		if kinds[tool.Kind] {
			t.Errorf("%s is listed twice", tool.Kind)
		}
		kinds[tool.Kind] = true
		if other, ok := ids[tool.ID]; ok {
			t.Errorf("%s and %s share object %d", tool.Kind, other, tool.ID)
		}
		ids[tool.ID] = string(tool.Kind)
		if len(tool.Shops) == 0 {
			t.Errorf("%s has no shop: its line could be blocked", tool.Kind)
		}
		for _, sh := range tool.Shops {
			if sh.Shop.Npc == "" || sh.Shop.NpcID == 0 || sh.Shop.Anchor == (tile.Tile{}) || sh.Price <= 0 || sh.Inv == "" {
				t.Errorf("%s: incomplete shop %+v", tool.Kind, sh)
			}
		}
		line := ToolLine(tool.Kind, Snapshot{})
		if why := ValidateKitLine(line); why != "" {
			t.Errorf("%s: %s", tool.Kind, why)
		}
		if len(line.Sources) != len(tool.Shops) {
			t.Errorf("%s: %d sources for %d shops", tool.Kind, len(line.Sources), len(tool.Shops))
		}
	}
	for _, k := range []ToolKind{ToolHammer, ToolPickaxe, ToolTinderbox, ToolChisel, ToolKnife, ToolNet, ToolFishingRod, ToolFlyRod} {
		if !kinds[k] {
			t.Errorf("no row for %s", k)
		}
	}
}
