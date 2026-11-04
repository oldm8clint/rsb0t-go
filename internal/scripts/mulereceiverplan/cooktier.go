package mulereceiverplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// cooktier.go: the pure half of the mule's idle-time cooking feature — account owner's own
// ask (2026-09-16): "since theres a range next to catherby bank, it would be good to have
// the mule instead of just stand there start cooking all the raw fish our fleet has
// acquired. we should start with the lowest cooking level fish and move up once we have
// cooked all the raws of that kind." No client/actions/fleet/bot imports and no I/O, per
// this package's own established convention (see plan.go's doc comment) — mulerunner/cook.go
// is the I/O half that reads a live bank/pack and drives the real actions.
//
// Ground-truthed against this content build's own real cooking data, not RS folklore,
// though the numbers happen to match well-known RS cooking levels exactly: every Level
// below and every Raw/Cooked name was read directly from
// content/scripts/skill_cooking/configs/cooking_source/cooking_generic.dbrow (the
// levelrequired field of each cooking_generic_<fish> row) and
// content/scripts/skill_fishing/configs/fishing.obj (the real "name=" lines), both in the
// content checkout this server's own local test engine builds from — not assumed from
// memory. Narrowed to the nine fish fisherplan.FishValues already names, since those are
// the only raw fish ProgressiveFisher ever hands the mule (no herring/sardine/trout/pike/
// salmon ever reach this bank, so tiering them would be dead code) — deliberately not
// importing fisherplan itself here (this package stays a leaf; fisherplan already imports
// donorplan/freshstart, and mulerunner is the integration point that would otherwise need
// to bridge two sibling plan packages just for nine literal strings).

// FishTier is one rung of the cooking ladder: the raw fish's real display name, its cooked
// counterpart's real display name, and the real Cooking level content requires to cook it
// (a burn is still possible above this level — this ladder doesn't try to avoid burning,
// see mulerunner/cook.go's own doc comment for why that's the account owner's own ask).
type FishTier struct {
	Raw    string
	Cooked string
	Level  int
}

// FishTiers is the ladder in level order — ties (shrimps/anchovies, both level 1) broken by
// this table's own order, oldest RS convention first. "Lowest tier still banked" always
// means the first entry here whose Raw is present in the bank; the table is never
// re-sorted at call time, so keep it in level order when editing.
var FishTiers = []FishTier{
	{Raw: "Raw shrimps", Cooked: "Shrimps", Level: 1},
	{Raw: "Raw anchovies", Cooked: "Anchovies", Level: 1},
	{Raw: "Raw mackerel", Cooked: "Mackerel", Level: 10},
	{Raw: "Raw cod", Cooked: "Cod", Level: 18},
	{Raw: "Raw tuna", Cooked: "Tuna", Level: 30},
	{Raw: "Raw lobster", Cooked: "Lobster", Level: 40},
	{Raw: "Raw bass", Cooked: "Bass", Level: 43},
	{Raw: "Raw swordfish", Cooked: "Swordfish", Level: 45},
	{Raw: "Raw shark", Cooked: "Shark", Level: 80},
	// The trawler's catch (a user request: "mule1 should have manta ray and sea turtle added to the list of food it can
	// cook"): cooking_generic.dbrow levelrequired 82 and 91.
	{Raw: "Raw sea turtle", Cooked: "Sea turtle", Level: 82},
	{Raw: "Raw manta ray", Cooked: "Manta ray", Level: 91},
}

// HighestTierInBank is the account owner's own rule, updated 2026-09-17 from the original
// lowest-first spec below: "cook the highest level raw fish it has instead of the lowest,
// weve got alot of low level fish" — the fleet's own bank had built up a large low-level
// backlog, so lowest-first was spending nearly all cooking time working through shrimps/
// anchovies/mackerel while higher-tier fish sat banked untouched. bankRaw is keyed by
// lower-cased item name (mulerunner builds this from a live bank read); a count of 0 or an
// absent key both mean "none of this fish banked." cookingLevel gates which tiers are even
// attempted — a tier above the mule's own real Cooking level is skipped rather than
// returned, since the real server refuses the cook outright below its levelrequired (see
// cooking.rs2's own check, cited in this package's doc comment) and walking to the range
// only to fail every attempt would leave that tier's raw fish untouched in the bank forever
// while looking like progress in the log. ok is false when no tier both has raw fish
// banked and is within cookingLevel — including the ordinary case of an empty bank.
//
// "Move down once we have cooked all the raws of that kind" falls out of this function
// alone, with no extra state to track, the same way the original lowest-first rule did:
// each call re-derives the answer fresh from whatever bankRaw says right now, and
// mulerunner.CookRound withdraws a full pack of the returned tier every round — once that
// tier's bank count actually reaches zero, the very next call naturally returns the next
// tier down instead, without either side needing to remember "which tier were we on."
//
// Original spec, kept for context since the ladder/level-gating logic below is otherwise
// unchanged: "start with the lowest cooking level fish and move up once we have cooked all
// the raws of that kind."
func HighestTierInBank(bankRaw map[string]int, cookingLevel int) (FishTier, bool) {
	for i := len(FishTiers) - 1; i >= 0; i-- {
		t := FishTiers[i]
		if t.Level > cookingLevel {
			continue
		}
		if bankRaw[strings.ToLower(t.Raw)] > 0 {
			return t, true
		}
	}
	return FishTier{}, false
}

// ---- the real Catherby range ----

// RangeLocName is the real loc name every cooking range shares (content's own
// cooking_sources.loc: every "[range]"/"[loc_2729..2731]" entry names="Range"), confirmed
// against this content build's own config archive via cmd/cachecfgtest (loc ids 2728-2731,
// 2859, 3039, every one named exactly "Range", every Ops row blank — a range has no
// right-click "Cook" option at all; it's used purely via OPLOCU, see cook.go's own doc
// comment).
const RangeLocName = "Range"

// CatherbyRange is the real Range loc (id 2728 in this content build) nearest Catherby's
// own bank stand — ground-truthed live tonight via cmd/locbake + cmd/findloc against the
// local test engine's own real packed maps (never guessed): `findloc` centered on the bank
// stand turned up exactly one Range within the building, at this tile, Chebyshev 8 tiles
// from the bank — matching the account owner's own "right next to Catherby bank"
// description. A second Range (2818,3455) sits further off, deeper in the same building,
// and is not used here.
//
// CatherbyBankTile duplicates fisherplan.Catherby().Bank's own literal coordinate rather
// than importing fisherplan (see this file's own package doc for why) — this project's own
// established convention (see mulerunner.walkNear's doc comment: "small per-package
// duplication over a shared helper") rather than a design smell; keep the two in sync by
// hand if Catherby's bank tile ever moves.
var (
	CatherbyRange    = tile.Tile{X: 2817, Z: 3444, Level: 0}
	CatherbyBankTile = tile.Tile{X: 2809, Z: 3441, Level: 0}
)
