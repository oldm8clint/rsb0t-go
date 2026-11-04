package loot

import (
	"strings"

	"rsb0t-go/internal/bankxp"
	"rsb0t-go/internal/cachecfg"
)

// Production: the skills that CONSUME an item to make another one, and the one card each gets
// (2026-09-21, owner: "arrow shafts are showing up in the woodcutting section probably should
// have a fletching section for all fletching materials the fleet makes").
//
// WHY THIS IS A SEPARATE KIND FROM GATHERING
//
// gather.go already enumerated every skill in content 289 and split them three ways, and it put
// Fletching on the production side of that line for a reason that still holds: a gathering
// source is a thing in the WORLD the account acts on — a fishing spot, a tree, a rock — and its
// loot is an item that did not exist before. A production source is an item in the account's own
// PACK, and its "loot" is what that item turned into. Folding fletching into KindGathering would
// have been one line instead of this file, and it would have made the panel's Gathering filter
// mean two different things.
//
// So production is its own kind, with its own filter entry on the panel, and it shares exactly
// one mechanism with gathering: a source carries a Skill, and Source.GroupKey collapses every
// source that names the same skill onto one card titled by that skill. That is what puts Logs,
// Oak logs and anything else a future fletching mode cuts onto a single "Fletching" card while
// the per-source rows underneath still say which material each batch came from.
//
// THE BUG THIS EXISTS TO FIX, because it was not "the woodcutter reported the wrong source"
//
// Nothing ever reported an arrow shaft. bankfletchrunner.Fletch sends one MakeX for the whole
// pack of logs and then returns as soon as the shaft count has risen ONCE — one tick into a
// batch that runs for another twenty-odd. ProgressiveWoodcutter's Draynor start camp then went
// straight back to chopping, woodcutrunner.beginChop snapshotted a pack that was still missing
// most of the batch, and every shaft that landed after that point showed up as a positive pack
// delta inside a window whose source was the tree. The Woodcutting card was reporting them
// because, to the measurement, they arrived while the account was chopping.
//
// Recording a Fletching source is therefore only half the fix; the other half is in
// bankfletchrunner (wait for the batch to settle, so the shafts are inside the fletch window
// rather than the next chop window) and in woodcutrunner.notAChop (a tree can never give a
// fletching product, whatever the pack does).
//
// WHAT THE FLEET ACTUALLY FLETCHES TODAY
//
// Arrow shafts AND unstrung bows, and nothing else. When this file was written it was shafts
// alone; ProgressiveWCFletch has since landed and cuts oak and above into unstrung short and
// long bows, which is the 2026-09-22 bug report below. Read rather than assumed:
//   - bankfletchplan.ArrowShaft is "the only product this first port makes", and its plan.go
//     header lists "string"/"cut+string"/"attach" mode — bow strings onto bows, shafts onto
//     arrowheads — as deliberately NOT ported. That is still true of BankFletcher.
//   - woodcutplan.FletchesToShafts is true for plain Logs alone; oak and above have no `shafts`
//     column in cut_logs.dbrow at all, which is exactly where the Draynor start camp ends.
//     ProgressiveWCFletch's ladder picks up there and cuts BOWS from oak upward
//     (internal/scripts/wcfletchplan/ladder.go, built from internal/bankxp's cut_logs rows).
//   - Stringing those bows is still not ported: no account in this fleet turns an unstrung bow
//     into a strung one, so a strung bow on this card would always be a bought one.
//   - The fleet's other "string" product is a BOW STRING, and that is Crafting, not Fletching:
//     crafterrunner spins flax at a spinning wheel (internal/scripts/crafterplan/stage.go).
//     It does not belong on this card and is not put on it.
//
// FletchingProducts below is DERIVED from internal/bankxp's content-generated route table, not
// named. It was a hand-written list until the unstrung bows had to go in it, and at thirteen ids
// a list is a thing that drifts from content silently; deriving is not a guess, because bankxp
// carries the cut_logs rows themselves. What is NOT derivable is the name lookup — see the
// unstrung-bow comment below for the display-name collision that forces bows to be id-only.

// KindProduction groups every production source in the panel. The card is titled by Skill.
const KindProduction = "production"

// SkillFletching is spelled as gamestate.SkillNames spells it, exactly as the gathering skills
// in gather.go are.
const SkillFletching = "Fletching"

// ActionFletch is what one event of fletching is, for the panel's verb. An EVENT here is one
// MATERIAL CONSUMED — one log cut, not the fifteen shafts it became.
//
// That is the same convention every other action uses ("how many times did this happen"), and it
// is load-bearing rather than cosmetic: Source.Consumes multiplies Events by the input's price to
// net a card's profit off its turnover (cost.go), so counting products here would have made a
// fletching card's cost fifteen times the truth. The shafts are not lost — they are the Drop's
// Items, exactly where a tree's logs are.
const ActionFletch = "fletch"

// ArrowShaft is the first fletching product the fleet made, and ObjArrowShaft its obj id.
// Both are named here as well as in bankfletchplan because internal/loot must not import a
// script package. The id is `52=arrow_shaft` on line 53 of content/pack/obj.pack — read out of
// the pack, not recalled — and it sits beside ObjCasket in randomevent.go, which is the same
// kind of constant for the same kind of reason.
const (
	ArrowShaft    = "Arrow shaft"
	ObjArrowShaft = 52
)

// THE UNSTRUNG BOWS, and why they are IDs ONLY (2026-09-22, owner: "fletching loot panel:
// unstrung bows cut from various logs arent showing in the fletching panel, some bows are
// showing in woodcutting panel").
//
// ProgressiveWCFletch (47 accounts live when this was written) cuts logs into unstrung bows as
// well as arrow shafts, so the header's "arrow shafts, and nothing else" is no longer true.
//
// THE TRAP, which is why there is no name entry for a single bow below. In content 289 the
// unstrung and the strung bow of every species SHARE A DISPLAY NAME, with no "(u)" suffix
// anywhere; only `desc` differs. Read out of the content checkout rather than recalled:
//
//	[unstrung_oak_shortbow] name=Oak shortbow  desc=An unstrung oak shortbow, ...
//	[oak_shortbow]          name=Oak shortbow  desc=A shortbow made out of oak, ...
//
// So a name entry would also match the strung bow — the one the combat scripts BUY from Lowe,
// which no account in this fleet fletches. TestStrungBowsAreNotFletchingProducts pins that: it
// asks IsFletchingProduct the identical name with each of the two ids and requires opposite
// answers, which only an id-keyed check can give. Everything that counts a bow anywhere in this
// repo keys on the obj id for the same reason (wcfletchrunner.CountByID's own comment).

// cutLogsProductIDs is every obj a knife-on-logs cut can put in the pack: the arrow shaft and
// the twelve unstrung bows.
//
// DERIVED from internal/bankxp's generated route table, not typed out, so regenerating that
// table from a content checkout carries this with it. The filter is the `fletching_*` key prefix
// plus a `_shafts`/`_shortbow`/`_longbow` suffix, which is exactly the cut_logs rows:
//
//   - `stringing_*` routes are excluded although they are Fletching too. Their products are the
//     STRUNG bows, nothing in this repo strings a bow (wcfletchplan's package doc: stringing and
//     attaching are deliberately not ported), and including them would put a bought bow on the
//     Fletching card.
//   - `fletching_*_arrow` and `fletching_*_bolts` are excluded by the suffix. Same reasoning:
//     nothing here attaches heads or tips, and a Bronze arrow is a common npc drop that belongs
//     on the npc's own card.
//
// It does NOT panic on an empty or short derivation, unlike wcfletchplan.buildLadder: this
// package is linked into the live control panel, and a panel that refuses to start is a worse
// failure than a card that under-reports. TestCutLogsProductsAreExactlyTheThirteen is what
// catches drift, at build time. ObjArrowShaft is seeded as a literal as well so the behaviour
// that shipped in 2026-09-21 cannot regress even if the table were emptied.
var cutLogsProductIDs = func() map[int]bool {
	m := map[int]bool{ObjArrowShaft: true}
	for _, r := range bankxp.Routes {
		if r.Skill != SkillFletching || !strings.HasPrefix(r.Key, "fletching_") {
			continue
		}
		switch {
		case strings.HasSuffix(r.Key, "_shafts"),
			strings.HasSuffix(r.Key, "_shortbow"),
			strings.HasSuffix(r.Key, "_longbow"):
			m[r.Product.ID] = true
		}
	}
	return m
}()

// fletchingProducts is every obj a fletching action in this codebase can put in the pack, BY ID
// and — for the arrow shaft alone — by lowercased name. See the header for why it is a list and
// not a rule, and the unstrung-bow comment above for why no bow name is in it.
//
// Two lookups, because the two callers know different things. A skip filter inside a measurement
// window always has the id and may have no *cachecfg.Config at all (an account whose cache never
// loaded), while a caller holding a resolved obj has the name and should not have to care what
// the id is. For the shaft either is enough; for a bow only the id ever is.
var (
	fletchingProductIDs   = cutLogsProductIDs
	fletchingProductNames = map[string]bool{strings.ToLower(ArrowShaft): true}
)

// IsFletchingProduct reports whether an obj is something FLETCHING made, by id or by name.
//
// Pass whichever you have: an unknown id with a known name matches an arrow shaft, and so does a
// known id with an empty name. A BOW is matched by id only — passing its name alone cannot
// answer the question, because the strung bow the combat scripts buy has the same one.
//
// It exists for the gathering skip filters as much as for this file — a tree cannot give an
// arrow shaft or an unstrung bow however the pack moved while the account was chopping, and
// saying so in one place means a product added above is kept off every gathering card at once.
func IsFletchingProduct(id int, name string) bool {
	if fletchingProductIDs[id] {
		return true
	}
	return fletchingProductNames[strings.ToLower(strings.TrimSpace(name))]
}

// Production is one production source: the INPUT item the skill consumed.
//
// Keying on the input rather than the output is what makes this symmetric with gathering — a
// gathering source is the entity acted on, and here that is the item in the pack the knife was
// used on. The outputs are the Drop's Items, exactly as a tree's logs are. Model is ModelObj so
// the panel draws the material's own item model on the card's pedestal.
func Production(skill, action string, objID int, name string) Source {
	return Source{
		Kind:   KindProduction,
		Skill:  skill,
		Action: action,
		Name:   name,
		ID:     objID,
		Model:  ModelObj,
	}
}

// Fletching is a fletch source for one input material (Logs, and whatever a later mode cuts),
// named through cfg when it can be so the card shows the cache's own wording.
func Fletching(cfg *cachecfg.Config, objID int, fallback string) Source {
	return Production(SkillFletching, ActionFletch, objID, objName(cfg, objID, fallback))
}

func objName(cfg *cachecfg.Config, objID int, fallback string) string {
	if cfg != nil {
		if o, ok := cfg.Obj(objID); ok && o.Name != "" {
			return o.Name
		}
	}
	return fallback
}
