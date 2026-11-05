package questplan

import (
	"sort"

	"rsb0t-go/internal/scripts/tile"
)

// THE TOOL LINES (docs/quest_selfsufficiency_plan_2026-10-19.md, 3.2 F; lane N4).
//
// A hand tool (a hammer, a pickaxe, a tinderbox ...) is the one thing every skill trainer and a good many quests need and
// almost nothing in the fleet owns: the mule holds 9 tinderboxes and no fishing rod that fly-fishes, and the accounts' banks
// hold a pickaxe or a hammer only when some earlier quest left one. Each module that wanted one wrote its own kit line (Doric's
// pickaxe, Vampire Slayer's hammer, the Elemental Workshop's), and a trainer lane that wrote none had no way for an account
// to come by the tool at all.
//
// This is the one library, modeled on axe.go's AxeLine: a tool line is a kit line whose sources are the owner's item rule
// for a tool, in its order and none other. PlanProvision tries the bank, then the mule, then any ground spawn the caller
// adds to the line (a knife lying in a place the module knows), then the shops here, and every tool in the table has a
// shop (ToolSpec.Shops), so a tool line always resolves: an account with a few coins in the bank and an empty mule is sent to
// the nearest general store, never blocked. The shops are the ones the content says stock the tool
// (TestEveryToolShopStocksItsToolInTheContent reads the inventory behind each keeper), nearest the account first.
//
// A pickaxe is the one tiered tool (like an axe): the best the bank holds that the Mining level can use, else a bronze one.

// ToolKind names a hand tool.
type ToolKind string

const (
	ToolHammer     ToolKind = "hammer"
	ToolPickaxe    ToolKind = "pickaxe"
	ToolTinderbox  ToolKind = "tinderbox"
	ToolChisel     ToolKind = "chisel"
	ToolKnife      ToolKind = "knife"
	ToolNet        ToolKind = "small fishing net"
	ToolFishingRod ToolKind = "fishing rod"
	ToolFlyRod     ToolKind = "fly fishing rod"
)

// ObjFlyFishingRod is [fly_fishing_rod]; the others a tool needs are named where their quest first used them (ObjHammer,
// ObjTinderbox, ObjKnife, DigObjChisel, TbObjNet, ObjFishingRod, ObjBronzePickaxe).
const ObjFlyFishingRod = 309

// ToolShop is one shop that stocks a tool.
type ToolShop struct {
	Shop ShopRef
	// Price is coins each at the content's base cost: a floor for budgeting, as Source.Price is.
	Price int
	// Inv is the content inventory the keeper owns (the npc's owned_shop param), which the content test holds to the stock.
	Inv  string
	Note string
}

// ToolSpec is one tool and where it comes from beyond the bank and the mule.
type ToolSpec struct {
	Kind ToolKind
	ID   int
	Name string
	// Shops stock the tool, in no order: ToolLine sorts them nearest the account first.
	Shops []ToolShop
}

// The general stores that stock the cheap tools. Lumbridge, Varrock and Catherby are on the Stage 5 accounts' beat; each
// keeps a handful of each tool and restocks within a hundred ticks.
var (
	toolLumbridge = ToolShop{Shop: LumbridgeStore, Inv: "generalshop1", Note: "Lumbridge general store"}
	toolVarrock   = ToolShop{Shop: VarrockGenStore, Inv: "generalshop2", Note: "Varrock general store"}
	toolCatherby  = ToolShop{Shop: ArheinStore, Inv: "arheinstore", Note: "Arhein's store, Catherby"}
)

func withPrice(s ToolShop, price int) ToolShop {
	s.Price = price
	return s
}

// Tools is the table.
var Tools = []ToolSpec{
	{Kind: ToolHammer, ID: ObjHammer, Name: "Hammer", Shops: []ToolShop{
		withPrice(toolLumbridge, 1), withPrice(toolVarrock, 1), withPrice(toolCatherby, 1)}},
	{Kind: ToolTinderbox, ID: ObjTinderbox, Name: "Tinderbox", Shops: []ToolShop{
		withPrice(toolLumbridge, 2), withPrice(toolVarrock, 2), withPrice(toolCatherby, 2)}},
	{Kind: ToolChisel, ID: DigObjChisel, Name: "Chisel", Shops: []ToolShop{
		withPrice(toolLumbridge, 2), withPrice(toolVarrock, 2), withPrice(toolCatherby, 2)}},
	// The bronze pickaxe, from Bob's in Lumbridge or Arhein's in Catherby. Better pickaxes are the bank's (PickaxeLine), never
	// bought: no shop here stocks one. The free pickaxe on the table south of Rimmington's mine (RimmingtonPickaxe) is left out
	// on purpose: it is Doric's, one spawn that comes back after a hundred ticks, and a trainer that sent hundreds of accounts
	// to it would queue them there; a shop keeps five and restocks.
	{Kind: ToolPickaxe, ID: ObjBronzePickaxe, Name: "Bronze pickaxe", Shops: []ToolShop{
		{Shop: BobsAxes, Price: 1, Inv: "axeshop", Note: "Bob's Brilliant Axes, Lumbridge, keeps five"},
		{Shop: ArheinStore, Price: 1, Inv: "arheinstore", Note: "Arhein's store, Catherby, keeps two"},
	}},
	// The knife: ground spawns are the fleet's usual source (woodcutplan.KnifeSpawns, shared by 94 accounts, and a Fletching
	// rung's kit lists them itself); Shantay's shop at the desert pass is the shop that sells one on the road from Al Kharid.
	{Kind: ToolKnife, ID: ObjKnife, Name: "Knife", Shops: []ToolShop{
		{Shop: ShantayShop, Price: 6, Inv: "shantayshop", Note: "Shantay at the desert pass keeps ten"}}},
	{Kind: ToolNet, ID: TbObjNet, Name: "Small fishing net", Shops: []ToolShop{
		{Shop: FluffsSardineShop, Price: 5, Inv: "fishingshop", Note: "Gerrant's fishing supplies, Port Sarim"},
		{Shop: FcHarry, Price: 5, Inv: "fishingshop2", Note: "Harry's fishing shop, Catherby"}}},
	{Kind: ToolFishingRod, ID: ObjFishingRod, Name: "Fishing rod", Shops: []ToolShop{
		{Shop: FluffsSardineShop, Price: 5, Inv: "fishingshop", Note: "Gerrant's fishing supplies, Port Sarim"},
		{Shop: FcHarry, Price: 5, Inv: "fishingshop2", Note: "Harry's fishing shop, Catherby"}}},
	{Kind: ToolFlyRod, ID: ObjFlyFishingRod, Name: "Fly fishing rod", Shops: []ToolShop{
		{Shop: FluffsSardineShop, Price: 5, Inv: "fishingshop", Note: "Gerrant's fishing supplies, Port Sarim"}}},
}

// ToolSpecOf is the table row of kind.
func ToolSpecOf(kind ToolKind) (ToolSpec, bool) {
	for _, t := range Tools {
		if t.Kind == kind {
			return t, true
		}
	}
	return ToolSpec{}, false
}

// pickaxeTiers are the pickaxes best first with the Mining level each needs (Doric's table, which pickaxe_checker.rs2 fixes).
func pickaxeTiers() []pickaxeRung { return doricPickaxes }

// PickaxeIDs are the pickaxes' object ids, best first, for a keep list.
func PickaxeIDs() []int {
	out := make([]int, 0, len(doricPickaxes))
	for _, p := range doricPickaxes {
		out = append(out, p.ID)
	}
	return out
}

// ToolIDs are the object ids that count as the tool, for a keep list: every pickaxe for a pickaxe, the one id otherwise.
func ToolIDs(kinds ...ToolKind) []int {
	var out []int
	for _, k := range kinds {
		if k == ToolPickaxe {
			out = append(out, PickaxeIDs()...)
			continue
		}
		if t, ok := ToolSpecOf(k); ok {
			out = append(out, t.ID)
		}
	}
	return out
}

// ToolHeld reports a usable copy of the tool in the pack or worn. A pickaxe counts when the Mining level can use it (a level
// the snapshot does not know, 0, judges none too high).
func ToolHeld(kind ToolKind, s Snapshot) bool {
	if kind == ToolPickaxe {
		return heldPickaxe(s) != 0
	}
	t, ok := ToolSpecOf(kind)
	return ok && (s.InvIDs[t.ID] > 0 || s.WornIDs[t.ID] > 0)
}

// heldPickaxe is the best usable pickaxe in the pack or worn, 0 for none.
func heldPickaxe(s Snapshot) int {
	level := s.Level("mining")
	for _, p := range pickaxeTiers() {
		if (level <= 0 || level >= p.Level) && (s.InvIDs[p.ID] > 0 || s.WornIDs[p.ID] > 0) {
			return p.ID
		}
	}
	return 0
}

// ToolLine is the kit line for the tool, for a pack that holds none: the bank first, then the mule (every tradeable line gets
// both), then its shops, nearest the account first. A tool that is not in the table is a programming error and gets a line
// with no id, which ValidateKitLine rejects.
func ToolLine(kind ToolKind, s Snapshot) KitLine {
	if kind == ToolPickaxe {
		return PickaxeLine(s)
	}
	t, ok := ToolSpecOf(kind)
	if !ok {
		return KitLine{Name: string(kind), Qty: 1}
	}
	return toolLineAt(t, s.Tile, s.TileKnown)
}

// toolLineAt builds the line with the shops ordered by distance from here (the table's order when here is unknown). The
// worn flag counts a wielded copy as held.
func toolLineAt(t ToolSpec, here tile.Tile, known bool) KitLine {
	shops := append([]ToolShop(nil), t.Shops...)
	if known {
		sort.SliceStable(shops, func(i, j int) bool {
			return tile.Chebyshev(here, shops[i].Shop.Anchor) < tile.Chebyshev(here, shops[j].Shop.Anchor)
		})
	}
	line := KitLine{ID: t.ID, Name: t.Name, Qty: 1, Worn: true}
	for _, sh := range shops {
		line.Sources = append(line.Sources, Source{Kind: SourceShop, Shop: sh.Shop, Price: sh.Price, Note: sh.Note})
	}
	return line
}

// PickaxeLine is the kit line for a pickaxe to mine with. A usable pickaxe already in the pack or worn is the line itself (so
// nothing is fetched); else the best one the bank holds that the Mining level can use; else a bronze pickaxe, which the
// provisioner takes from the bank or the mule if either has one and otherwise buys from Bob's or Arhein's. A bank that has not been read is not an empty one (the caller scans it first); a Mining level the snapshot
// does not know (0) judges no pickaxe too high.
func PickaxeLine(s Snapshot) KitLine {
	if id := heldPickaxe(s); id != 0 {
		return pickaxeLineOf(id)
	}
	level := s.Level("mining")
	if s.BankKnown {
		for _, p := range pickaxeTiers() {
			if s.BankIDs[p.ID] > 0 && (level <= 0 || level >= p.Level) {
				return pickaxeLineOf(p.ID)
			}
		}
	}
	t, _ := ToolSpecOf(ToolPickaxe)
	return toolLineAt(t, s.Tile, s.TileKnown)
}

// pickaxeLineOf is the line for the pickaxe with that id, which the pack, the worn slot or the bank has: no source of its own.
func pickaxeLineOf(id int) KitLine {
	name := "Pickaxe"
	for _, p := range pickaxeTiers() {
		if p.ID == id {
			name = p.Name
		}
	}
	return KitLine{ID: id, Name: name, Qty: 1, Worn: true}
}

// ToolLines are the kit lines for the tools of kinds the pack does not already hold, in the order given: what a trainer
// or a quest hands the provisioner as its tool kit.
func ToolLines(s Snapshot, kinds ...ToolKind) []KitLine {
	var out []KitLine
	for _, k := range kinds {
		if ToolHeld(k, s) {
			continue
		}
		out = append(out, ToolLine(k, s))
	}
	return out
}
