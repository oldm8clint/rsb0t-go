package wcfletchplan

// The fletching ladder: which tree to chop, and what to cut its logs into.
//
// EVERY NUMBER HERE IS DERIVED, NOT TYPED. The level gates and experience figures are read at
// init out of internal/bankxp.Routes, which internal/bankxp/gen generated from the LostCityRS
// Content checkout this server runs (branch 289) — so a regeneration that changes a level
// changes this ladder too, and TestLadderMatchesContent fails loudly rather than the script
// quietly fletching against a remembered number. The one thing stated here rather than derived
// is the six log obj ids, and each carries the line of content/pack/obj.pack that proves it.
//
// The content rows behind all of it are content/scripts/skill_fletching/configs/cut_logs/
// cut_logs.dbrow (one `[fletching_<species>]` section per log) and its driver
// content/scripts/skill_fletching/scripts/cut_logs.rs2.
//
// # Three facts read out of cut_logs.rs2 that shape the whole design
//
//  1. THE MENU ALWAYS OFFERS BOTH BOWS, whatever the account's level. `~skill_multi2` is called
//     with $obj2 (shortbow) and $obj3 (longbow) unconditionally, and the level test
//     `if (stat(fletching) < $level)` runs AFTER the product has been chosen — it answers with a
//     `~doubleobjbox` message and no experience. So "the make menu offers a longbow" is NOT
//     evidence the account may make one, and a runner that picked the longbow because it was on
//     screen would produce nothing for ever. The level gate is Choose's, in this package, and
//     the menu is only ever asked for the product Choose already decided on.
//
//  2. ONLY PLAIN LOGS CUT INTO ARROW SHAFTS. `data=shafts,15` appears on `[fletching_normal]`
//     and on no other section, and cut_logs.rs2 branches on `if ($shaft_count < 1)` — oak and
//     above go to the two-option bow menu with no shaft option at all. woodcutplan.FletchesToShafts
//     already records this; the ladder below simply has a nil Shaft on every rung but the first.
//
//  3. FLETCHING IS A MEMBERS ACTION. cut_logs.rs2's first statement is
//     `if (map_members = ^false) { mes(^mes_members_fletching); return; }`, and the engine
//     answers MAP_MEMBERS from `Environment.node.members`, a WORLD-wide flag that defaults to
//     true (engine/src/util/WorldConfig.ts:96). It is not per-area, so nothing here needs to
//     check where the account is standing — but a world started with NODE_MEMBERS=false would
//     make every fletch a silent no-op, which is worth knowing before blaming this script.
//
// # And one that shapes how anything is COUNTED
//
// THE UNSTRUNG BOW AND THE STRUNG BOW SHARE A DISPLAY NAME. content/scripts/skill_fletching/
// configs/stringing/bows.obj gives `[unstrung_oak_shortbow]` `name=Oak shortbow`, and
// content/scripts/skill_combat/configs/ranged/bows.obj gives `[oak_shortbow]` the identical
// `name=Oak shortbow`; only `desc` differs, and there is no "(u)" suffix anywhere in content
// 289. So every count and every hand-over rule in this package keys on the OBJ ID, never on the
// name. A name-keyed count would silently include a strung bow a random event dropped.
//
// The make menu is the one place a name match is safe, and only because content never offers a
// strung bow as a fletching product: `data=objchoices` names the unstrung pair.

import (
	"fmt"
	"sort"
	"strings"

	"rsb0t-go/internal/bankxp"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// The log obj ids, read directly out of content/pack/obj.pack (the line number is the file's own
// `<id>=<content key>` line). Stated rather than derived because they are what the derivation is
// keyed on; TestLogIDsAgreeWithContentNames pins each one against the name bankxp read for it.
const (
	NormalLogsID = 1511 // obj.pack:1512  1511=logs
	OakLogsID    = 1521 // obj.pack:1522  1521=oak_logs
	WillowLogsID = 1519 // obj.pack:1520  1519=willow_logs
	MapleLogsID  = 1517 // obj.pack:1518  1517=maple_logs
	YewLogsID    = 1515 // obj.pack:1516  1515=yew_logs
	MagicLogsID  = 1513 // obj.pack:1514  1513=magic_logs
)

// ArrowShaftID is the arrow shaft obj (obj.pack:53  52=arrow_shaft). Stackable, which is the
// whole reason the first rung never has to bank.
const ArrowShaftID = 52

// Knife is the fletching tool's item name — the same constant woodcutplan already carries, named
// here so this package's own rules read without reaching across for one string.
const Knife = woodcutplan.Knife

// ProductKind is which of the three things a log can become.
type ProductKind string

// The product kinds. KindNone means "this rung's logs cannot be fletched at the level given",
// which a caller must answer by banking the logs rather than by opening a menu.
const (
	KindNone     ProductKind = ""
	KindShaft    ProductKind = "shaft"
	KindShortbow ProductKind = "shortbow"
	KindLongbow  ProductKind = "longbow"
)

// Product is one thing a log can be cut into.
type Product struct {
	Kind ProductKind
	// ItemID is the product obj id. Everything that COUNTS this product keys on it, because the
	// unstrung and strung bows share a display name (see this file's header).
	ItemID int
	// ItemName is the display name, for a log line only.
	ItemName string
	// Level is the Fletching level content refuses the cut below — `data=<kind>,<obj>,<level>,
	// <xp10>` on the rung's own cut_logs.dbrow section.
	Level int
	// XP10 is experience in TENTHS of a point per log, exactly as content stores it.
	XP10 int
	// PerLog is how many of ItemID one log yields: 15 for arrow shafts (`data=shafts,15`), 1 for
	// either bow.
	PerLog int
	// MakeMatch is what the make menu's product name is matched on
	// (actions.FindMakeProduct does a case-insensitive substring match against the obj's config
	// name). "shortbow" and "longbow" are each a substring of exactly one of the two products
	// content offers, and neither is a substring of the other.
	MakeMatch string
	// RouteKey and Source are the bankxp row and the content file this came from, so any single
	// number above can be checked in one grep.
	RouteKey string
	Source   string
}

// XP is experience per log as a whole number of points, for a log line.
func (p Product) XP() float64 { return float64(p.XP10) / 10 }

// Rung is one step of the ladder: a tree, and what its logs can be cut into.
type Rung struct {
	// Tier is woodcutplan's own tier, unchanged — the tree's loc name, its anchor, its leash and
	// the WOODCUTTING level content opens it at. Reused rather than restated so the two scripts
	// chop in exactly the same places.
	Tier     woodcutplan.Tier
	LogsID   int
	LogsName string
	// Shaft is non-nil only on the first rung; see fact 2 in this file's header.
	Shaft    *Product
	Shortbow *Product
	Longbow  *Product
}

// EntryLevel is the FLETCHING level this rung is climbed to at: the LOWEST level any product on
// it can be made at.
//
// On rungs 1-5 that is the shortbow, which is the fleet owner's ladder stated in his own words:
// "staying at normal trees until we unlock cutting oak logs into shortbows, stay at oaks till we
// can cut willow logs into shortbows, stay at willows till we can cut maples into shortbows, stay
// at maples until we can cut yew logs into shortbows, stay at yews until we can cut magic
// shortbows". The SHORTBOW is the gate on purpose — the longbow of the same species comes five
// levels later and is an upgrade WITHIN the rung, not a reason to move up it.
//
// On rung 0 it is the arrow shaft, at Fletching 1, so the first rung is the ladder's floor and
// has no real gate — which is what makes a brand-new account start there.
func (r Rung) EntryLevel() int {
	lowest := 0
	for _, p := range []*Product{r.Shaft, r.Shortbow, r.Longbow} {
		if p == nil {
			continue
		}
		if lowest == 0 || p.Level < lowest {
			lowest = p.Level
		}
	}
	if lowest < 1 {
		return 1
	}
	return lowest
}

// Ladder is the rungs in tree order, built once at init from content-derived data.
var Ladder = buildLadder()

// tierLogID maps woodcutplan's own tier labels onto the log obj ids above. Keyed by LABEL rather
// than by log name so a rename on either side is a missing key (a loud panic at init) instead of
// a silently short ladder.
var tierLogID = map[string]int{
	"Normal tree": NormalLogsID,
	"Oak":         OakLogsID,
	"Willow":      WillowLogsID,
	"Maple":       MapleLogsID,
	"Yew":         YewLogsID,
	"Magic":       MagicLogsID,
}

// buildLadder derives every rung from internal/bankxp.Routes.
//
// It panics rather than degrading if a rung comes out without a shortbow or a longbow. A ladder
// that is quietly one rung short would present as an account that never leaves willows, with
// nothing in any log to say why — the exact class of silent-wrong-number failure
// docs/trustworthy_harnesses.md exists to stop. This runs at process start, so a panic here is a
// build-time-quality failure, not a live one.
func buildLadder() []Rung {
	out := make([]Rung, 0, len(woodcutplan.TIERS))
	for _, tier := range woodcutplan.TIERS {
		id, ok := tierLogID[tier.Label]
		if !ok {
			panic(fmt.Sprintf("wcfletchplan: woodcutplan tier %q has no log obj id — the ladder and woodcutplan.TIERS have drifted", tier.Label))
		}
		r := Rung{Tier: tier, LogsID: id, LogsName: tier.Logs}
		for _, route := range bankxp.RoutesFor(id) {
			// Every log id also carries a Firemaking `burn_*` route, and every UNSTRUNG bow id
			// carries a `stringing_*` one. Only this skill and only the cut_logs rows are ours.
			if route.Skill != "Fletching" || !strings.HasPrefix(route.Key, "fletching_") {
				continue
			}
			p := Product{
				ItemID:   route.Product.ID,
				ItemName: route.Product.Name,
				Level:    route.Level,
				XP10:     route.XP10,
				PerLog:   route.Product.Count,
				RouteKey: route.Key,
				Source:   route.Source,
			}
			if p.PerLog < 1 {
				p.PerLog = 1
			}
			switch {
			case strings.HasSuffix(route.Key, "_shafts"):
				p.Kind, p.MakeMatch = KindShaft, "shaft"
				r.Shaft = &p
			case strings.HasSuffix(route.Key, "_shortbow"):
				p.Kind, p.MakeMatch = KindShortbow, "shortbow"
				r.Shortbow = &p
			case strings.HasSuffix(route.Key, "_longbow"):
				p.Kind, p.MakeMatch = KindLongbow, "longbow"
				r.Longbow = &p
			}
		}
		if r.Shortbow == nil || r.Longbow == nil {
			panic(fmt.Sprintf("wcfletchplan: no cut_logs shortbow/longbow route in internal/bankxp for %s (obj %d) — regenerate internal/bankxp/table.go", tier.Logs, id))
		}
		out = append(out, r)
	}
	return out
}

// RungIndexForLogs is the ladder position of a log obj id; ok is false for anything that is not
// one of the six.
func RungIndexForLogs(id int) (int, bool) {
	for i, r := range Ladder {
		if r.LogsID == id {
			return i, true
		}
	}
	return 0, false
}

// ---- what counts as hand-over stock ----

// productIDs is every id this script ever MAKES: the arrow shaft and the twelve unstrung bows.
// Built from the ladder, so it cannot drift from it.
var productIDs = func() map[int]bool {
	m := map[int]bool{}
	for _, r := range Ladder {
		for _, p := range []*Product{r.Shaft, r.Shortbow, r.Longbow} {
			if p != nil {
				m[p.ItemID] = true
			}
		}
	}
	return m
}()

// IsHandoverProduct reports whether an obj id is something this script fletched and therefore
// wants on the trade screen first.
//
// BY ID, DELIBERATELY. The strung Oak shortbow (obj 843) is a different item with the identical
// display name, so a name-keyed version of this would also claim any strung bow the account
// happened to be holding. See this file's header.
func IsHandoverProduct(id int) bool { return productIDs[id] }

// HandoverProductIDs is every id IsHandoverProduct accepts, sorted — for a log line and for
// tests that want to assert the set rather than probe it.
func HandoverProductIDs() []int {
	out := make([]int, 0, len(productIDs))
	for id := range productIDs {
		out = append(out, id)
	}
	sort.Ints(out)
	return out
}

// LogIDs is every log obj id the ladder chops, sorted.
func LogIDs() []int {
	out := make([]int, 0, len(Ladder))
	for _, r := range Ladder {
		out = append(out, r.LogsID)
	}
	sort.Ints(out)
	return out
}

// IsLadderLogs reports whether an obj id is one of the six logs.
func IsLadderLogs(id int) bool {
	_, ok := RungIndexForLogs(id)
	return ok
}
