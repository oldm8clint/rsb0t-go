// Package bowstringplan is the pure half of BowStringer: stringing the fleet's unstrung bows so
// they alch for double (a strung Maple longbow alchs for 384 coins, the unstrung one for 192).
//
// It imports nothing from client, actions, fleet or bot. The I/O is
// internal/scripts/bowstringrunner, the sequencing internal/bot/bowstringer.go.
//
// THE CONTENT (content 289, scripts/skill_fletching/scripts/bows.rs2 and
// configs/stringing/bows.dbrow): Bow string used on an unstrung bow, either way round
// (opheldu), strings ONE bow per action with no delay and says "You add a string to the bow.".
// Members worlds only; the level is the fletching_table row's. The levels are in Bows below.
//
// STRUNG AND UNSTRUNG BOWS SHARE A NAME. "Maple longbow" is both obj 62 (unstrung) and the strung
// bow; only the id and the cost differ, the unstrung one being the cheaper. So every count in
// this script is BY ID, and UnstrungID picks the id among same-name ids by cost. What is asked of
// the mule and what its published bank is read for is BY ID too, through a QUALIFIED name
// ("Maple longbow id62", AskName, cachecfg.QualifiedName): the mule resolves a plain name to the first
// obj its bank holds, which on 6 Oct 2026 was 55,634 STRUNG Yew shortbows handed to a kit that wanted
// the unstrung ones, and its published bank, counted by name, read 130,712 strung Yew shortbows as
// bows to string (a bow round every few minutes).
package bowstringplan

import (
	"sort"
	"strings"

	"rsb0t-go/internal/cachecfg"
)

// BowString is the string's name as the game spells it.
const BowString = "Bow string"

// Batch is one trip's worth: 14 unstrung bows and 14 strings fill the 28-slot pack.
const Batch = 14

// PackSlots is the backpack's size.
const PackSlots = 28

// KitMax is the most of one line a kit order may ask for. The mule clamps an uncatalogued item to
// its own anyMaxPerOrder (orderplan.OpenMaxPerOrder, 1000, only when unset); the mule runs 1,000,000.
// Was 1000, so the main account drew 1000 bows and 1000 strings a trade with 20k+ of each waiting (owner,
// 4 Oct: "should grab all it has to ensure maximum exp perhour"). Notes take one slot whatever the
// count.
const KitMax = 100000

// Bow is one stringable bow and the Fletching level the content asks for.
type Bow struct {
	Name  string
	Level int
}

// Bows is every bow content 289 strings, highest level first (configs/stringing/bows.dbrow).
var Bows = []Bow{
	{"Magic longbow", 85},
	{"Magic shortbow", 80},
	{"Yew longbow", 70},
	{"Yew shortbow", 65},
	{"Maple longbow", 55},
	{"Maple shortbow", 50},
	{"Willow longbow", 40},
	{"Willow shortbow", 35},
	{"Oak longbow", 25},
	{"Oak shortbow", 20},
	{"Longbow", 10},
	{"Shortbow", 5},
}

// DefaultBowNames is the settings default: every bow content strings, highest level first (Bows).
// A bow the mule holds none of costs nothing: PlanOrder marks it exhausted from the published bank
// without a trade. It used to be only the bows the mule held when it was written, Yew shortbow
// down, so the main account at Fletching 96 never strung the Yew longbows the Fletching 70 accounts make
// (a user request: "we need to include magic shortbow u and magic longbow u and yew longbow u").
func DefaultBowNames() []string {
	out := make([]string, 0, len(Bows))
	for _, b := range Bows {
		out = append(out, b.Name)
	}
	return out
}

// bowXP is each bow's stringing experience in content tenths (configs/stringing/bows.dbrow:
// data=experience).
var bowXP = map[string]int{
	"magic longbow": 915, "magic shortbow": 832, "yew longbow": 750, "yew shortbow": 675,
	"maple longbow": 582, "maple shortbow": 500, "willow longbow": 415, "willow shortbow": 332,
	"oak longbow": 250, "oak shortbow": 165, "longbow": 100, "shortbow": 50,
}

// XPTenths is the Fletching experience stringing name gives, in content tenths (75.0 XP is 750).
func XPTenths(name string) (int, bool) {
	xp, ok := bowXP[strings.ToLower(strings.TrimSpace(name))]
	return xp, ok
}

// LevelFor is the Fletching level stringing name needs, matched case-insensitively.
func LevelFor(name string) (int, bool) {
	want := strings.ToLower(strings.TrimSpace(name))
	for _, b := range Bows {
		if strings.ToLower(b.Name) == want {
			return b.Level, true
		}
	}
	return 0, false
}

// Stringable reports whether an account at fletching strings name: content strings it at that level, and a shortbow only
// while the same wood's longbow is out of reach (a user request: "it should only string those when lacking the level for
// magic longbows, this goes for all bows").
func Stringable(name string, fletching int) bool {
	lvl, known := LevelFor(name)
	if !known || lvl > fletching {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(name))
	if !strings.HasSuffix(lower, "shortbow") {
		return true
	}
	long, ok := LevelFor(strings.TrimSuffix(lower, "shortbow") + "longbow")
	return !ok || long > fletching
}

// PickBow is the highest-level bow in names that fletching allows and that is not exhausted.
// Equal levels keep the list's order. ok is false when none is left.
func PickBow(names []string, fletching int, exhausted map[string]bool) (Bow, bool) {
	var cands []Bow
	for _, n := range names {
		lvl, _ := LevelFor(n)
		if !Stringable(n, fletching) || exhausted[strings.ToLower(strings.TrimSpace(n))] {
			continue
		}
		cands = append(cands, Bow{Name: canonical(n), Level: lvl})
	}
	if len(cands) == 0 {
		return Bow{}, false
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Level > cands[j].Level })
	return cands[0], true
}

func canonical(name string) string {
	want := strings.ToLower(strings.TrimSpace(name))
	for _, b := range Bows {
		if strings.ToLower(b.Name) == want {
			return b.Name
		}
	}
	return strings.TrimSpace(name)
}

// Key is the exhausted-map key for a bow name.
func Key(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// BowIDs resolves a bow name to the obj ids of its unstrung and strung forms (bowstringrunner.
// ResolveIDs reads them from the object config); ok is false for a name it cannot tell apart.
type BowIDs func(bow string) (unstrung, strung int, ok bool)

// AskName is the qualified name of one obj of a bow ("Yew shortbow id68"): what a kit asks the mule for
// and what its published bank counts that obj under, which a plain bow name cannot say (it names the
// strung bow and the unstrung one alike).
func AskName(bow string, id int) string { return cachecfg.QualifiedName(bow, id) }

// MuleUnstrung is how many UNSTRUNG bows of bow the mule's published bank holds, read from the
// per-obj rows peerrunner publishes for every bow (exact: ok true). The merged row of the plain name
// counts strung bows as well and is only a fallback, for a bank published with no per-obj row of the
// bow, where strungAtMule (the strung bows this account handed the mule, which it remembers) is taken
// off it, as before: exact is then false. stock nil (nothing published) is 0, false.
func MuleUnstrung(stock func(item string) (int, bool), bow string, unstrung, strung, strungAtMule int) (n int, exact bool) {
	if stock == nil {
		return 0, false
	}
	count := func(item string) int {
		n, ok := stock(item)
		if !ok || n < 0 {
			return 0
		}
		return n
	}
	un, st, merged := count(AskName(bow, unstrung)), count(AskName(bow, strung)), count(bow)
	if un > 0 || st > 0 || merged <= 0 {
		return un, true
	}
	return max(0, merged-max(0, strungAtMule)), false
}

// ObjInfo is what the object config says about one id sharing the bow's name.
type ObjInfo struct {
	ID    int
	Cost  int
	Noted bool
}

// UnstrungID picks the unstrung bow among every id carrying the bow's name: of the unnoted
// ids, the one with the strictly lowest cost. It refuses (ok false) when there are fewer than
// two unnoted ids -- nothing to tell apart, so a lone id could as well be the strung bow -- or
// when the two cheapest cost the same. strung is the costliest unnoted id.
func UnstrungID(objs []ObjInfo) (unstrung, strung int, ok bool) {
	var items []ObjInfo
	for _, o := range objs {
		if !o.Noted {
			items = append(items, o)
		}
	}
	if len(items) < 2 {
		return 0, 0, false
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Cost < items[j].Cost })
	if items[0].Cost == items[1].Cost {
		return 0, 0, false
	}
	return items[0].ID, items[len(items)-1].ID, true
}

// Withdraw is how many bows and strings one trip draws: equal numbers, at most Batch, at most
// what the bank holds of either, and at most half the free slots.
func Withdraw(bankBows, bankStrings, freeSlots int) int {
	n := Batch
	n = min(n, bankBows, bankStrings, freeSlots/2)
	if n < 0 {
		return 0
	}
	return n
}

// Step is what the bot does next.
type Step string

// The steps.
const (
	// StepString: the pack holds at least one unstrung bow and one string.
	StepString Step = "string"
	// StepBank: go to the bank, deposit, and draw a batch (or find there is nothing to draw).
	StepBank Step = "bank"
)

// Snapshot is the pack as the bot reads it, by id and unnoted only.
type Snapshot struct {
	PackBows, PackStrings int
}

// Decide is the next step from the pack.
func Decide(s Snapshot) Step {
	if s.PackBows > 0 && s.PackStrings > 0 {
		return StepString
	}
	return StepBank
}

// KitLine is one line of a kit order to the mule.
type KitLine struct {
	Item string
	Qty  int
}

// MuleStock is the mule's published bank count for an item: ok false when the mule has not
// published one. The count is by NAME, so a plain bow name counts the strung bows of that name too;
// the count of one obj is read under its qualified name (AskName).
type MuleStock func(item string) (count int, ok bool)

// Order is what to ask the mule for once the bank has run out.
type Order struct {
	Lines []KitLine
	// Exhausted is set when the mule has none of the bow left: mark it and pick the next.
	Exhausted bool
	// Why is the one-line reason for a log.
	Why string
}

// PlanOrder sizes a kit order for the plain bow name: PlanOrderAs asking by that name.
func PlanOrder(bow string, bankBows, bankStrings, kit int, mule MuleStock) Order {
	return PlanOrderAs(bow, bow, bankBows, bankStrings, kit, mule)
}

// PlanOrderAs sizes a kit order when the bank can no longer fund a batch. item is the name the mule is
// asked for the bow under and its published count is read under: the bow's QUALIFIED unstrung name
// (AskName) in the bot, so the mule hands the unstrung obj and no other bow of that display name. kit is
// how many bows one order asks for (clamped to 1..KitMax). The strings asked for bring the bank to one
// per bow it will then hold. mule may be nil (not published): the order is placed blind.
func PlanOrderAs(bow, item string, bankBows, bankStrings, kit int, mule MuleStock) Order {
	kit = max(1, min(kit, KitMax))
	var o Order
	bows := 0
	if bankBows < Batch {
		bows = kit
	}
	if mule != nil {
		if n, ok := mule(item); ok && n < bows {
			bows = n
		}
	}
	if bankBows+bows <= 0 {
		o.Exhausted = true
		o.Why = "the mule's published bank holds no unstrung " + bow
		return o
	}
	strs := min(bankBows+bows-bankStrings, KitMax)
	if strs > 0 && mule != nil {
		if n, ok := mule(BowString); ok && n < strs {
			strs = n
		}
	}
	if bows > 0 {
		o.Lines = append(o.Lines, KitLine{Item: item, Qty: bows})
	}
	if strs > 0 {
		o.Lines = append(o.Lines, KitLine{Item: BowString, Qty: strs})
	}
	if bankStrings+strs <= 0 {
		o.Lines = nil
		o.Why = "no " + BowString + " in the bank or the mule's published bank"
		return o
	}
	if len(o.Lines) == 0 {
		o.Why = "nothing to ask for"
	}
	return o
}

// DeclineMeansEmpty reports whether the mule's decline reason says it has none of the item.
func DeclineMeansEmpty(reason string) bool {
	r := strings.ToLower(reason)
	return strings.Contains(r, "none in the bank") || strings.Contains(r, "cannot identify")
}

// KitOutcome is what one finished kit order came to.
type KitOutcome string

// The outcomes.
const (
	// KitGained: the bank can fund stringing again.
	KitGained KitOutcome = "gained"
	// KitEmpty: the mule declined the bow as not held at all.
	KitEmpty KitOutcome = "empty"
	// KitNoBows: a trade happened but brought no unstrung bow (the mule drew the strung bow of
	// the same name, say). Counted toward the fruitless limit.
	KitNoBows KitOutcome = "no-bows"
	// KitDeferred: declined for a passing reason (its per-donor window, a queue, a timeout, the
	// mule away). Not counted: the order is simply asked again after the back-off.
	KitDeferred KitOutcome = "deferred"
)

// ClassifyKit names a kit's outcome. gainedBows is how many bow-and-string pairs the bank can
// fund after the kit (by id: a strung bow of the same name never counts), which is the gain,
// since a kit is only ordered once the bank can fund none. traded is whether anything at all
// arrived, reason the mule's decline for the bow line.
func ClassifyKit(gainedBows int, traded bool, reason string) KitOutcome {
	switch {
	case gainedBows > 0:
		return KitGained
	case DeclineMeansEmpty(reason):
		return KitEmpty
	case traded:
		return KitNoBows
	default:
		return KitDeferred
	}
}

// AfterKit is the running count of fruitless kits and whether the bow is now exhausted: an
// empty decline exhausts at once, maxFruitless KitNoBows in a row exhaust (0 never), a gain
// resets the count and a deferral leaves it.
func AfterKit(out KitOutcome, fruitlessBefore, maxFruitless int) (fruitless int, exhausted bool) {
	switch out {
	case KitGained:
		return 0, false
	case KitEmpty:
		return fruitlessBefore, true
	case KitNoBows:
		fruitless = fruitlessBefore + 1
		return fruitless, maxFruitless > 0 && fruitless >= maxFruitless
	default:
		return fruitlessBefore, false
	}
}
