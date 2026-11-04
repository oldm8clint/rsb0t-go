// Package smeltplan is SmelterBot's own decisions, ported from rsb0t's public-catalogue
// SmelterBot.ts and its SmelterBotLogic.ts sibling (src/bot/scripts/SmelterBot/). It is pure:
// no client, actions, fleet or bot imports and no I/O, so its tests run with no live server.
//
// The script is a bank-and-furnace loop: withdraw a pack of ore, walk to the furnace, click its
// Smelt option, batch the bars through the make menu, walk back and bank them, repeat until the
// bank cannot fund even one more bar.
//
// Grounding — every game fact below was confirmed against the content checkout on branch 289
// and against the running local engine's own config archive, never trusted from the TypeScript:
//
//   - The interaction is content's `[oploc2,_smithing_furnace]` in
//     scripts/skill_smithing/scripts/smelting/smelting.rs2, which runs @smelting_interface:
//     if_setobject for all eight bars followed by `if_openchat(smelting)`. So this is the
//     furnace's OPTION 2 ("Smelt"), a plain OpLoc — NOT the OpLocU use-ore-on-furnace path,
//     which the same file also handles. That matters: OpLocU is the primitive with the known
//     silent no-op on this engine/content pairing (see CLAUDE.md's Gotchas and mulerunner's
//     cook.go), and this port never needs it.
//   - It opens a MAKE MENU, not one bar per click. `if_openchat` means the menu is the CHAT
//     modal, which is the root actions.ChatDialog.MakeProducts already prefers, and each bar
//     carries Smelt-1/5/10/X buttons (`[if_button,smelting:com_15]` ... `com_12` =
//     @smelt_ore_makex). One Smelt-X therefore drains a whole pack, the same shape
//     bankfletchrunner's knife-on-logs already proved.
//   - Levels, XP and ore ratios are the `param=levelrequired` / `productexp` /
//     `ingredient{,_secondary}{,_count}` lines of each `[smelting_<bar>_bar]` block in
//     scripts/skill_smithing/configs/smelting/smelting.struct. All eight rows matched
//     SmelterBotLogic.ts's own RECIPES table exactly — levels and ratios both — which is worth
//     recording, since the precedent is that a TS table is sometimes wrong (see the food table
//     in docs/script_parity_status.md).
//   - Every object id below was read back from the RUNNING local engine's config archive
//     (cmd/cachecfgtest -find), not from the TS source, which carries no ids at all.
//
// XP UNITS, and why this port counts BARS rather than XP: the XP column here is in CONTENT
// units, which are tenths of a displayed XP point. A bronze bar's `productexp,62` is the 6.2 XP
// a player sees, and the engine FLOORS the fractional part, so six successful bronze bars
// produce 36 XP, not 37.2. A live XP delta therefore cannot be divided back into a bar count.
// Every count this port asserts on is an item delta.
//
// IRON IS NOT DETERMINISTIC, a content fact the TypeScript does not model at all: content's
// `[queue,smelting_ore]` gives iron a coin-flip — without a ring of forging, `randominc(1) = 1`
// prints "The ore is too impure and you fail to refine it." and consumes the ore with no bar.
// So an iron run legitimately produces bars for only about half its ore, and any caller
// asserting "ore consumed == bars made" must exempt iron. See FailsRandomly.
//
// Deliberately not ported here, and why:
//   - The paint/status overlay (SmelterBot.ts's own onPaint) — no headless equivalent, the same
//     boundary every other ported script draws.
//   - No mule hand-off: confirmed by reading the whole TS source before writing this.
//     SmelterBot.ts imports no donorPlan or muleProtocol; the bank is the only destination.
//   - The TS `bankStand`/`furnaceStand`/`furnaceName`/`bankBooth`/`obstacle`/`leashRadius`
//     settings are fixed constants here instead, exactly as bankfletchplan/gemplan already do:
//     this codebase walks with a real pathfinder plus a webwalk escalation rather than the TS's
//     hand-rolled walkOpening, so a user-supplied tile is a footgun rather than a feature.
//   - Cannonballs, gold gauntlets, perfect gold and the Fremennik/Legends furnaces — all real
//     branches of the same content file, none of them part of SmelterBot.ts.
package smeltplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// ---- items ----

// Ore is one ingredient of a recipe.
type Ore struct {
	// Name is the display name, and ID the live-confirmed object id. The NOTED variant of
	// every ore and every bar sits at id+1 and carries the identical display name, which is
	// why this package carries ids and the runner matches on them: a name match would count a
	// noted stack the furnace cannot smelt.
	Name string
	ID   int32
	// Per is how many of this ore one bar consumes.
	Per int
}

// Recipe is one row of content's own smelting struct table.
type Recipe struct {
	// Bar is the bar's display name, which is also what the make menu's product picture
	// resolves to and therefore what MakeMatch matches on.
	Bar string
	// BarID is the live-confirmed object id of the unnoted bar.
	BarID int32
	// Level is the Smithing level content requires (param=levelrequired).
	Level int
	// XP is CONTENT units — tenths of a displayed point. See the package doc comment.
	XP int
	// Primary is the defining ore; Secondary is coal for the alloys, and empty otherwise.
	Primary   Ore
	Secondary Ore
}

// HasSecondary reports whether the recipe needs a second ore.
func (r Recipe) HasSecondary() bool { return r.Secondary.Name != "" }

// Ingredients lists every ore the recipe consumes, primary first.
func (r Recipe) Ingredients() []Ore {
	if !r.HasSecondary() {
		return []Ore{r.Primary}
	}
	return []Ore{r.Primary, r.Secondary}
}

// SlotsPerBar is how many pack slots one bar's worth of ore occupies. Ore does not stack, so
// this is just the sum of the per-bar counts.
func (r Recipe) SlotsPerBar() int {
	n := 0
	for _, in := range r.Ingredients() {
		n += in.Per
	}
	return n
}

// FailsRandomly marks a recipe whose bars are not guaranteed: iron alone carries content's
// 50% "too impure" coin flip when no ring of forging is worn. See the package doc comment.
func (r Recipe) FailsRandomly() bool { return r.Bar == "Iron bar" }

// DisplayXP is the XP a player actually sees for one successful bar — the content number above
// divided by ten, floored the way the engine floors it.
func (r Recipe) DisplayXP() int { return r.XP / 10 }

// Recipes is content's smelting table, lowest level first. Levels, XP and ratios come from
// smelting.struct; ids were read back from the live config archive.
var Recipes = []Recipe{
	{Bar: "Bronze bar", BarID: 2349, Level: 1, XP: 62,
		Primary:   Ore{Name: "Copper ore", ID: 436, Per: 1},
		Secondary: Ore{Name: "Tin ore", ID: 438, Per: 1}},
	{Bar: "Iron bar", BarID: 2351, Level: 15, XP: 125,
		Primary: Ore{Name: "Iron ore", ID: 440, Per: 1}},
	{Bar: "Silver bar", BarID: 2355, Level: 20, XP: 137,
		Primary: Ore{Name: "Silver ore", ID: 442, Per: 1}},
	{Bar: "Steel bar", BarID: 2353, Level: 30, XP: 175,
		Primary:   Ore{Name: "Iron ore", ID: 440, Per: 1},
		Secondary: Ore{Name: "Coal", ID: 453, Per: 2}},
	{Bar: "Gold bar", BarID: 2357, Level: 40, XP: 225,
		Primary: Ore{Name: "Gold ore", ID: 444, Per: 1}},
	{Bar: "Mithril bar", BarID: 2359, Level: 50, XP: 300,
		Primary:   Ore{Name: "Mithril ore", ID: 447, Per: 1},
		Secondary: Ore{Name: "Coal", ID: 453, Per: 4}},
	{Bar: "Adamantite bar", BarID: 2361, Level: 70, XP: 375,
		Primary:   Ore{Name: "Adamantite ore", ID: 449, Per: 1},
		Secondary: Ore{Name: "Coal", ID: 453, Per: 6}},
	{Bar: "Runite bar", BarID: 2363, Level: 85, XP: 500,
		Primary:   Ore{Name: "Runite ore", ID: 451, Per: 1},
		Secondary: Ore{Name: "Coal", ID: 453, Per: 8}},
}

// BarNames lists every bar in table order, for a settings dropdown.
func BarNames() []string {
	out := make([]string, 0, len(Recipes))
	for _, r := range Recipes {
		out = append(out, r.Bar)
	}
	return out
}

// RecipeFor finds a recipe by bar name, matching ignoring case and tolerating the short forms a
// user is likely to type ("Adamant" for "Adamantite bar", "Rune" for "Runite bar", "bronze" for
// "Bronze bar"). The TS source carried an explicit Adamant->Adamantite / Rune->Runite map for
// exactly this; folding it into the lookup keeps one spelling — content's own — everywhere else.
func RecipeFor(name string) (Recipe, bool) {
	want := strings.ToLower(strings.TrimSpace(name))
	if want == "" {
		return Recipe{}, false
	}
	want = strings.TrimSuffix(want, " bar")
	switch want {
	case "adamant":
		want = "adamantite"
	case "rune":
		want = "runite"
	}
	for _, r := range Recipes {
		if strings.TrimSuffix(strings.ToLower(r.Bar), " bar") == want {
			return r, true
		}
	}
	return Recipe{}, false
}

// ---- places ----
//
// Al Kharid, the pairing SmelterBot.ts itself defaults to, and the only furnace in this content
// build whose loc carries a "Smelt" option anywhere near a bank. Both tiles below were read out
// of the baked static loc index (cmd/locbake + cmd/findloc), not copied from the TypeScript:
// the furnace is loc 2781 "Furnace" ops=[_ Smelt] at 3272,3185,0 (3x3, so the stand tile is the
// adjacent square east, matching content's own forceapproach=east), and the bank row is loc
// 2213 "Bank booth" ops=[Use, Use-quickly] at 3268,3164..3169.
var (
	// BankStand is the tile every bank trip walks to, directly east of the booth row.
	BankStand = tile.Tile{X: 3269, Z: 3167, Level: 0}
	// FurnaceStand is the tile every smelt trip walks to, east of the furnace.
	FurnaceStand = tile.Tile{X: 3275, Z: 3185, Level: 0}
)

const (
	// BoothName and BoothOp are the live-confirmed bank booth loc and the option that opens it.
	BoothName = "Bank booth"
	BoothOp   = "Use-quickly"
	// FurnaceName and FurnaceOp are the live-confirmed furnace loc and its smelting option.
	FurnaceName = "Furnace"
	FurnaceOp   = "Smelt"
	// FurnaceLeash bounds the search for a furnace around FurnaceStand.
	FurnaceLeash = 8
)

// PackSlots is a full pack.
const PackSlots = 28

// ---- the trip ----

// Stage is one leg of a smelting trip.
type Stage string

const (
	// StageSmelt has enough ore held to make at least one bar.
	StageSmelt Stage = "smelt"
	// StageBank goes back for more ore (and drops off the bars).
	StageBank Stage = "bank"
	// StageDone means the bank cannot fund even one more bar, which is where SmelterBot.ts
	// stops itself too.
	StageDone Stage = "done"
)

// Snapshot is the live state DecideStage reads.
type Snapshot struct {
	// CanSmelt: the pack holds enough of EVERY ingredient for at least one bar.
	CanSmelt bool
	// BankExhausted: a bank trip has confirmed the bank cannot fund one more bar.
	BankExhausted bool
}

// DecideStage picks the next leg, ported from SmelterBot.ts's own SmeltTrip/BankTrip task
// validators plus the stop condition its BankTrip reaches. Smelting wins whenever it is
// possible at all, so a pack part-way through a load is never abandoned for a bank trip.
//
// Gating on EVERY ingredient rather than just the primary ore is the TS source's own hard-won
// fix (its "#117" comment): a pack holding iron but no coal would otherwise skip the bank and
// then spin at the furnace making nothing.
func DecideStage(s Snapshot) Stage {
	if s.CanSmelt {
		return StageSmelt
	}
	if s.BankExhausted {
		return StageDone
	}
	return StageBank
}

// DescribeStage is the status line for a stage.
func DescribeStage(s Stage) string {
	switch s {
	case StageSmelt:
		return "smelting at the furnace"
	case StageBank:
		return "restocking ore at the bank"
	case StageDone:
		return "out of ore"
	default:
		return string(s)
	}
}

// ---- counting ----

// Counts answers how many of an object id something holds. The runner supplies a pack reader or
// a bank reader; this package never touches either.
type Counts func(id int32) int

// CanSmelt reports whether held has enough of every ingredient for at least one bar.
func CanSmelt(held Counts, r Recipe) bool {
	for _, in := range r.Ingredients() {
		if held(in.ID) < in.Per {
			return false
		}
	}
	return true
}

// BarsFundedBy is how many whole bars the given counts can fund, ignoring pack space.
func BarsFundedBy(held Counts, r Recipe) int {
	best := -1
	for _, in := range r.Ingredients() {
		n := held(in.ID) / in.Per
		if best < 0 || n < best {
			best = n
		}
	}
	if best < 0 {
		return 0
	}
	return best
}

// SetsPerTrip is how many bars' worth of ore fit in one empty pack.
func SetsPerTrip(r Recipe) int { return PackSlots / r.SlotsPerBar() }

// WithdrawStep is one withdrawal a bank trip should make.
type WithdrawStep struct {
	Ore   Ore
	Count int
}

// WithdrawFor plans one trip's withdrawals against what the bank actually holds, capped at a
// full pack. An empty result means the bank cannot fund even one bar, which is the run's own
// stop condition — the TS source's same "nearly-empty bank became a dead bot" fix.
func WithdrawFor(bank Counts, r Recipe) []WithdrawStep {
	sets := SetsPerTrip(r)
	if n := BarsFundedBy(bank, r); n < sets {
		sets = n
	}
	if sets <= 0 {
		return nil
	}
	out := make([]WithdrawStep, 0, 2)
	for _, in := range r.Ingredients() {
		out = append(out, WithdrawStep{Ore: in, Count: sets * in.Per})
	}
	return out
}

// ---- the smelt wait ----

// SmeltIdle is how long the primary ore count may stand still before a smelting batch is
// treated as finished rather than waited out. SmelterBot.ts uses the same 8s for the same
// reason: a single long delayUntil parks the whole task loop, so a furnace that quietly stopped
// consuming looks dead (its "#711").
const SmeltIdle = 8_000

// SmeltStalled reports whether a smelting batch has stopped making progress: the primary ore
// count has not fallen and idleMs has passed since it last did.
func SmeltStalled(nowCount, lastCount, idleMs int) bool {
	return nowCount >= lastCount && idleMs > SmeltIdle
}
