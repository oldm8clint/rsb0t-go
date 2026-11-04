package loot

import (
	"strings"

	"rsb0t-go/internal/bankxp"
	"rsb0t-go/internal/cachecfg"
)

// Crafting, and the fletching that is not knife-on-logs, on the Loot panel (a user request:
// "we dont have a loot tracker panel for crafting and pearl bolt tips dont show in the fletching
// section either").
//
// WHAT WAS MISSING, read rather than assumed. Every production source before this was opened by
// a loot.Window around a knife-on-logs batch (bankfletchrunner, wcfletchrunner). Nothing else
// that MAKES something reported a Drop at all:
//
//   - MasterCrafter's gem round (gemrunner.CutAllThen and mastercraftrunner.FastGems), GemCutter
//     (gemrunner.Cut): uncut gems into cut gems and Crushed gemstones. Crafting.
//   - ProgressiveCrafter's flax spinners (crafterrunner.SpinFlaxUntil) and its wool stage
//     (crafterrunner.SpinWool): Flax into Bow string, Wool into Ball of wool. Crafting.
//   - MasterCrafter's pearl round (mastercraftrunner.CutPearls, FastPearls, MakeBolts): oyster pearls
//     into Pearl bolttips, then the tips onto Bolts. FLETCHING, not Crafting: content's
//     [label,make_bolt_tips] advances stat fletching although the chisel opens it from
//     skill_crafting's uncut_gem.rs2.
//   - BowStringer and MasterCrafter's bow round (bowstringrunner.StringAll, mastercraftrunner.
//     FastBows): unstrung bows and Bow strings into strung bows. Fletching.
//
// WHY THESE REPORT COUNTS RATHER THAN OPEN A WINDOW. A Window measures the pack between two
// readings, which is right for a batch that stays in the pack. MasterCrafter's fast streams bank
// the product and draw the next load INSIDE one run (a whole load is deposited by the next load's
// Deposit-All before the client has read the pack), so a pack delta across the run is mostly
// zero. Every one of those runners already measures what it made — the pack delta of one load,
// or the skill's XP divided by the XP of one use — and Made reports exactly that. Nothing here
// predicts a count.
//
// An EVENT is still one MATERIAL CONSUMED, as production.go's ActionFletch says: one uncut gem
// (whether it was cut or smashed), one flax, one pearl, one tip put on a bolt, one bow strung.
// That is what Source.Inputs prices the card's cost from.

// SkillCrafting is spelled as gamestate.SkillNames spells it.
const SkillCrafting = "Crafting"

// ActionCraft is one crafting event: one material consumed.
const ActionCraft = "craft"

// Crafting is a crafting source for one input material (an uncut gem, Flax, Wool), named through
// cfg when it can be.
func Crafting(cfg *cachecfg.Config, objID int, fallback string) Source {
	return Production(SkillCrafting, ActionCraft, objID, objName(cfg, objID, fallback))
}

// SkillSmithing and ActionSmith are bars smelted at a furnace (the Smelter script): one event is
// one bar, its ores consumed.
//
// A bar source's ID is the BAR (so iron and steel, both cast on Iron ore, are two tallies) and its
// Name the PRIMARY ORE, which is what Source.Consumes prices as the first input; Inputs adds the
// bar's second ingredient (barSecond). The row on the card reads by the ore's name.
const (
	SkillSmithing = "Smithing"
	ActionSmith   = "smith"
)

// Smithing is a smelting source for one bar (its obj id) made from oreName.
func Smithing(barID int, oreName string) Source {
	return Production(SkillSmithing, ActionSmith, barID, oreName)
}

// SkillHerblore and ActionClean are MasterCrafter's herb round: one event is one grimy herb cleaned.
const (
	SkillHerblore = "Herblore"
	ActionClean   = "clean"
)

// Cleaning is a herblore source for one grimy herb (its obj id). Every grimy herb in content 289 is
// named "Herb", so the cache's name would put every kind on one row: the source is named "Grimy "
// and the clean herb's name instead ("Grimy guam leaf"), which the PriceBook does not know, so the
// card is a gross figure rather than one netted against the wrong price.
func Cleaning(grimyID int, cleanName string) Source {
	return Production(SkillHerblore, ActionClean, grimyID, "Grimy "+strings.ToLower(strings.TrimSpace(cleanName)))
}

// UnstrungSuffix marks a stringing source's name. In content 289 an unstrung bow and the strung
// one share their display name ("Yew longbow" both), so a stringing row named by the cache would
// read "Yew longbow -> Yew longbow" and, worse, its cost would be priced at what the STRUNG bow
// trades for (the PriceBook prices by name): every bow strung would net to minus a Bow string. The
// suffix keeps the row legible and makes the input unpriceable, which the panel shows as a gross
// figure rather than a wrong net one.
const UnstrungSuffix = " (u)"

// Stringing is a fletching source for stringing one unstrung bow (its obj id and display name).
// See UnstrungSuffix for why the name is not the cache's.
func Stringing(unstrungID int, name string) Source {
	name = strings.TrimSpace(name)
	if !strings.HasSuffix(name, UnstrungSuffix) {
		name += UnstrungSuffix
	}
	return Production(SkillFletching, ActionFletch, unstrungID, name)
}

// Product is one made item for Made: an id, a fallback name and a count.
func Product(id int, name string, count int) Item {
	return Item{ID: id, Name: name, Count: int64(count)}
}

// Made reports one measured production batch: events materials consumed and the products they
// became. A nil sink, no events or no product at all reports nothing. Names and the cache's unit
// cost come from cfg when it is there; the caller's names stand otherwise.
func Made(sink func(Drop), cfg *cachecfg.Config, src Source, events int, products ...Item) {
	if sink == nil || events <= 0 {
		return
	}
	items := make([]Item, 0, len(products))
	for _, p := range products {
		if p.Count <= 0 {
			continue
		}
		if cfg != nil {
			if o, ok := cfg.Obj(p.ID); ok {
				if o.Name != "" {
					p.Name = o.Name
				}
				p.Value = int64(o.Cost)
			}
		}
		items = append(items, p)
	}
	if len(items) == 0 {
		return
	}
	sink(Drop{Source: src, Events: events, Items: items})
}

// secondInputs is, for a production source keyed on the FIRST input of a two-input recipe, the
// second input one event also consumes: Bolts for a bolt tip put on a bolt, a Bow string for a bow
// strung.
//
// DERIVED from internal/bankxp's content-generated route table, filtered to the kinds of recipe the
// fleet runs: `fletching_*_bolts` (tips on Bolts, bolts.dbrow), `fletching_*_arrow` (arrowtips on
// headless arrows, arrows.dbrow: the source is keyed on the TIPS, so each metal is its own row) and
// `stringing_*` (bows.dbrow); plus headless arrows (an Arrow shaft and a Feather: arrows.rs2
// [label,make_headless_arrows], which has no dbrow and so no bankxp route). Not every two-input row: bankxp lists the oyster pearl rows
// (`fletching_oyster_pearl*_tips`) with Bolts as a second input, which content does not take — a
// chisel on a pearl consumes the pearl alone ([label,make_bolt_tips] inv_del(inv, $item, 1)) — so
// a rule over every two-input route would charge the tips card for Bolts it never used.
var secondInputs = func() map[int]Cost {
	m := map[int]Cost{}
	for _, r := range bankxp.Routes {
		if r.Skill != SkillFletching || len(r.Inputs) != 2 {
			continue
		}
		tipped := strings.HasPrefix(r.Key, "fletching_") && (strings.HasSuffix(r.Key, "_bolts") || strings.HasSuffix(r.Key, "_arrow"))
		if !tipped && !strings.HasPrefix(r.Key, "stringing_") {
			continue
		}
		in := r.Inputs[1]
		m[r.Inputs[0].ID] = Cost{ID: in.ID, Name: in.Name, Per: int64(max(1, in.Count))}
	}
	m[ObjArrowShaft] = Cost{ID: 314, Name: "Feather", Per: 1}
	return m
}()

// SkillCooking and ActionCook are food cooked on a range: one event is one raw food used.
const (
	SkillCooking = "Cooking"
	ActionCook   = "cook"
)

// Cooking is a cooking source for one raw food (its obj id and name). The products are the cooked
// and the burnt food, which are the Drop's items; the raw food is the input the card's cost prices.
func Cooking(cfg *cachecfg.Config, rawID int, fallback string) Source {
	return Production(SkillCooking, ActionCook, rawID, objName(cfg, rawID, fallback))
}

// ActionForge is one bar worked at an anvil into something (a dagger, nails): one event is one bar.
const ActionForge = "forge"

// Forging is a smithing source for one bar (its obj id and name) worked at an anvil. Unlike a
// smelting source (Smithing), whose name is the PRIMARY ORE and whose cost adds the bar's coal, the
// input here is the bar itself and nothing else, so Inputs prices the bar alone.
func Forging(barID int, barName string) Source {
	return Production(SkillSmithing, ActionForge, barID, barName)
}

// Flax, wool and their products (content pack/obj.pack).
const (
	ObjFlax       = 1779
	ObjBowString  = 1777
	ObjWool       = 1737
	ObjBallOfWool = 1759
)

// Spun reports flax spun into Bow strings, or wool into Balls of wool: one event a flax or a wool.
// A nil sink or no count reports nothing.
func Spun(sink func(Drop), cfg *cachecfg.Config, flax bool, n int) {
	if flax {
		Made(sink, cfg, Crafting(nil, ObjFlax, "Flax"), n, Product(ObjBowString, "Bow string", n))
		return
	}
	Made(sink, cfg, Crafting(nil, ObjWool, "Wool"), n, Product(ObjBallOfWool, "Ball of wool", n))
}

// MadeFrom reports one production batch measured as the pack's own change: events materials
// consumed, and whatever positive difference from before to after is not in skip (the knife, the
// hammer, the materials themselves). It is Made for a script that cannot name its product (a fletch
// menu that made an arrow shaft or a bow, an anvil that made a dagger) but can read the pack either
// side of the batch. Nothing here predicts a product: only what arrived is counted.
func MadeFrom(sink func(Drop), cfg *cachecfg.Config, src Source, events int, before, after Counts, skip func(id int) bool) {
	if sink == nil || events <= 0 {
		return
	}
	got := Gained(before, after, skip)
	Made(sink, cfg, src, events, Items(cfg, got)...)
}
