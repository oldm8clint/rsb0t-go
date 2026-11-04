// Package herbplan is HerbCleaner's own decisions, ported from the public catalogue's
// rsb0t/src/bot/scripts/HerbCleaner/HerbCleaner.ts and its HerbCleanerLogic.ts sibling. It is
// pure: no client, actions, fleet or bot imports and no I/O, so its tests run with no server.
//
// The script is a bank-standing loop: withdraw a pack of unidentified ("grimy") herbs, use each
// one's own Identify inventory option, bank the identified herbs, repeat until the bank runs out
// of every herb the run is allowed to clean.
//
// Grounding — every game fact below was confirmed against the content checkout on branch 289 and
// against the RUNNING local engine's own config archive, not trusted from the TypeScript:
//
//   - The interaction is `[opheld1,unidentified_<herb>]` in
//     content/scripts/skill_herblore/scripts/identifying/identify.rs2, which calls
//     `~attempt_identify_herb` and then `~identify_herb`. That does
//     `inv_setslot(inv, $slot, $identified_herb, 1)` — it replaces exactly ONE herb in place, at
//     the slot the click named. There is no make menu and no "how many?" dialog, so a load is
//     cleaned by a burst of single Identify clicks, one per occupied slot.
//   - Level and XP come from `param=identified_herb_level` / `param=identified_herb_exp` on the
//     CLEAN herb's obj block (content/scripts/skill_herblore/configs/herbs.obj); the level param
//     defaults to 3 when a block omits it, which is why Guam (which has no explicit level line)
//     is level 3. Every one of the 19 levels matched HerbCleanerLogic.ts exactly.
//   - Every object id below was read back from the running engine's config archive
//     (cmd/cachecfgtest) and cross-checked against content/pack/obj.pack. All 19 unidentified
//     ids and all 19 clean ids matched the TypeScript table exactly — worth recording, since a
//     past port found the TS food table wrong.
//   - Both forms are `members=yes`, and content refuses the whole interaction on a free world
//     BEFORE it checks the level (`if (map_members = ^false) { mes(^mes_members_herblore); }`).
//     The local engine's data/config/world.json has members true.
//   - There is deliberately NO Druidic Ritual gate: identify.rs2 says so in its own comment
//     ("early osrs had no druidic ritual check. This is how level 2 herblore accounts were
//     possible"). Nothing here should check quest progress.
//
// TWO ID TRAPS, both live-confirmed, and they pull in opposite directions:
//
//  1. Every unidentified herb renders under a SHARED display name — "Herb" for the 14 main herbs
//     and "Unidentified herb" for the 5 Karamja ones. Matching by name is therefore impossible:
//     it cannot tell Guam from Torstol. This package matches by object id everywhere.
//  2. The "a noted variant sits at id+1" heuristic the GemCutter port established DOES NOT
//     generalise here. For the 14 main herbs id+1 is indeed the note (199 unid -> 200 note). But
//     the 5 Karamja herbs are `tradeable=no` and have no note at all, so their id+1 is the CLEAN
//     herb (1525 unid -> 1526 clean Snake weed). Anything deriving a note id by arithmetic would
//     silently corrupt those five rows. Nothing here does; the table states both ids outright.
//
// XP UNITS, because this is easy to get wrong and bites harder here than it did for gems: the XP
// column below is in CONTENT units, which are tenths of a displayed XP point. Unlike Crafting's
// gem table, several herbs are NOT whole numbers of displayed XP — Guam's 25 is 2.5 XP. The
// engine stores XP internally as tenths and UpdateStatEncoder writes `(exp / 10) | 0` to the
// wire, so the value gamestate reports is a FLOORED whole-XP total. One Guam clean therefore
// moves the reported total by 2 or by 3 depending on where the fractional part happens to sit.
// That is why nothing in this port treats an XP delta as the authority on whether a clean
// happened: the pack's item counts are the reliable signal, and XP is only ever reported.
package herbplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Herb is one row of content's own herb-identifying table.
type Herb struct {
	// Key is the content debugname's stem, and the stable key settings and logs use.
	Key string
	// Name is the CLEAN herb's display name ("Guam leaf"), which is what the TS settings list
	// offers and what this port's own herbs setting accepts. The UNIDENTIFIED form has no
	// usable name of its own — see the package doc comment's first id trap.
	Name string
	// UnidID is the unidentified ("grimy") object that sits in the pack and carries the
	// Identify option; CleanID is what it becomes. Both are live-confirmed, and neither is
	// derived from the other — see the package doc comment's second id trap.
	UnidID  int32
	CleanID int32
	// Level is the Herblore level content requires (param=identified_herb_level, default 3).
	Level int
	// XP is content units, tenths of a displayed XP point — see the package doc comment.
	XP int
}

// DisplayXP is the XP a player sees for one identify, rounded DOWN to whole points. Several
// herbs award a fractional amount (Guam is 2.5), so this is deliberately lossy and is only fit
// for a log line — never for deciding whether an identify landed.
func (h Herb) DisplayXP() int { return h.XP / 10 }

// Herbs is content's herb table, lowest level first so the cheapest XP comes first — the order
// HerbCleanerLogic.ts's own eligibleHerbs sorts into. Ties keep the TS table's own order.
var Herbs = []Herb{
	{Key: "guam", Name: "Guam leaf", UnidID: 199, CleanID: 249, Level: 3, XP: 25},
	{Key: "snake_weed", Name: "Snake weed", UnidID: 1525, CleanID: 1526, Level: 3, XP: 25},
	{Key: "ardrigal", Name: "Ardrigal", UnidID: 1527, CleanID: 1528, Level: 3, XP: 25},
	{Key: "sito_foil", Name: "Sito foil", UnidID: 1529, CleanID: 1530, Level: 3, XP: 25},
	{Key: "volencia_moss", Name: "Volencia moss", UnidID: 1531, CleanID: 1532, Level: 3, XP: 25},
	{Key: "rogues_purse", Name: "Rogues purse", UnidID: 1533, CleanID: 1534, Level: 3, XP: 25},
	{Key: "marrentill", Name: "Marrentill", UnidID: 201, CleanID: 251, Level: 5, XP: 38},
	{Key: "tarromin", Name: "Tarromin", UnidID: 203, CleanID: 253, Level: 11, XP: 50},
	{Key: "harralander", Name: "Harralander", UnidID: 205, CleanID: 255, Level: 20, XP: 63},
	{Key: "ranarr", Name: "Ranarr weed", UnidID: 207, CleanID: 257, Level: 25, XP: 75},
	{Key: "toadflax", Name: "Toadflax", UnidID: 3049, CleanID: 2998, Level: 30, XP: 80},
	{Key: "irit", Name: "Irit leaf", UnidID: 209, CleanID: 259, Level: 40, XP: 88},
	{Key: "avantoe", Name: "Avantoe", UnidID: 211, CleanID: 261, Level: 48, XP: 100},
	{Key: "kwuarm", Name: "Kwuarm", UnidID: 213, CleanID: 263, Level: 54, XP: 113},
	{Key: "snapdragon", Name: "Snapdragon", UnidID: 3051, CleanID: 3000, Level: 59, XP: 118},
	{Key: "cadantine", Name: "Cadantine", UnidID: 215, CleanID: 265, Level: 65, XP: 125},
	{Key: "lantadyme", Name: "Lantadyme", UnidID: 2485, CleanID: 2481, Level: 67, XP: 131},
	{Key: "dwarf_weed", Name: "Dwarf weed", UnidID: 217, CleanID: 267, Level: 70, XP: 138},
	{Key: "torstol", Name: "Torstol", UnidID: 219, CleanID: 269, Level: 75, XP: 150},
}

// IdentifyOp is the inventory option every unidentified herb carries (iop1), confirmed on all 19
// ids in the running engine's config archive.
const IdentifyOp = "Identify"

// HerbNames lists every clean herb name, the option set the herbs setting accepts.
func HerbNames() []string {
	names := make([]string, 0, len(Herbs))
	for _, h := range Herbs {
		names = append(names, h.Name)
	}
	return names
}

// HerbByUnidID finds the herb an unidentified object id belongs to.
func HerbByUnidID(id int32) (Herb, bool) {
	for _, h := range Herbs {
		if h.UnidID == id {
			return h, true
		}
	}
	return Herb{}, false
}

// HerbByCleanID finds the herb a clean object id belongs to.
func HerbByCleanID(id int32) (Herb, bool) {
	for _, h := range Herbs {
		if h.CleanID == id {
			return h, true
		}
	}
	return Herb{}, false
}

// HerbByName finds a herb by its clean display name or its key, ignoring case and surrounding
// whitespace.
func HerbByName(name string) (Herb, bool) {
	want := strings.ToLower(strings.TrimSpace(name))
	for _, h := range Herbs {
		if strings.ToLower(h.Name) == want || h.Key == want {
			return h, true
		}
	}
	return Herb{}, false
}

// ---- places ----

// BankStand is the bank every trip works at: the same Seers' Village bank row
// flaxplan/bankfletchplan/gemplan already proved live, chosen over HerbCleaner.ts's own dynamic
// nearestBank(here) because this codebase has not proven a bank-catalogue primitive.
var BankStand = tile.Tile{X: 2725, Z: 3493, Level: 0}

const (
	// BoothName and BoothOp are the live-confirmed bank booth loc and the option that opens it
	// (loc 2213 "Bank booth" carries ops Use / Use-quickly). HerbCleaner.ts names the identical
	// pair in its own BOOTH constant.
	BoothName = "Bank booth"
	BoothOp   = "Use-quickly"
)

// PackSlots is a full pack.
const PackSlots = 28

// BurstSize is how many Identify clicks one burst sends before waiting for the result.
//
// Why: HerbCleaner.ts clicks every eligible unid in the pack in one unbroken pass with no
// per-item delay, which is a browser client leaning on the server to drain them. The public
// catalogue's own DartFletcherLogic.ts states the engine decodes at most five USER_EVENT packets
// per player each game tick; that claim is the TS source's and is NOT independently confirmed
// here, so five is taken as a conservative ceiling rather than a measured optimum. Kept small
// deliberately: a burst larger than the server can decode is not faster, and it makes a genuine
// stall impossible to tell from a slow tick.
const BurstSize = 5

// ---- the loop ----

// Stage is what to do next.
type Stage string

const (
	// StageClean identifies the unidentified herbs already in the pack.
	StageClean Stage = "clean"
	// StageBank banks the clean herbs and withdraws the next load of unidentified ones.
	StageBank Stage = "bank"
	// StageDone means every herb this run can clean is confirmed absent from the bank, which is
	// where HerbCleaner.ts stops itself too ("every selected herb is empty in the bank").
	StageDone Stage = "done"
)

// Snapshot is the live state DecideStage reads.
type Snapshot struct {
	// EligibleUnidsHeld: how many unidentified herbs in the pack this run is allowed to clean.
	EligibleUnidsHeld int
	// TargetsRemaining: how many herb kinds are still worth a bank trip — eligible at this
	// Herblore level, not denied by a refusal, and not yet confirmed empty in the bank.
	TargetsRemaining int
}

// DecideStage picks the next leg, ported from HerbCleaner.ts's own Clean/BankTrip task
// validators plus the stop condition in its loop(). Cleaning wins whenever it is possible at
// all, so a pack part-way through a load is never abandoned for a bank trip; the run ends only
// once the bank is known to hold nothing this account can clean AND the pack has none left.
//
// Unlike GemCutter there is no tool to check: identifying needs nothing but the herb itself.
func DecideStage(s Snapshot) Stage {
	if s.EligibleUnidsHeld > 0 {
		return StageClean
	}
	if s.TargetsRemaining == 0 {
		return StageDone
	}
	return StageBank
}

// DescribeStage is the status line for a stage.
func DescribeStage(s Stage) string {
	switch s {
	case StageClean:
		return "identifying herbs"
	case StageBank:
		return "banking clean herbs and withdrawing grimy ones"
	case StageDone:
		return "no cleanable herbs left in the bank"
	default:
		return string(s)
	}
}

// Eligible is the herbs this run may clean: allowed by Herblore level, restricted to selected
// when it is non-empty, minus any denied key, lowest level first. Ported from
// HerbCleanerLogic.ts's own eligibleHerbs plus HerbCleaner.ts's deniedKeys filter.
//
// membersWorld false drops every herb, because content refuses the whole interaction on a free
// world before it even looks at the level — the TS source has no such filter because it never
// asked the question, and a run that kept going would spin forever on an uncleanable pack.
func Eligible(herbloreLevel int, selected []string, denied map[string]bool, membersWorld bool) []Herb {
	if !membersWorld {
		return nil
	}
	wanted := map[string]bool{}
	for _, s := range selected {
		if h, ok := HerbByName(s); ok {
			wanted[h.Key] = true
		}
	}
	out := make([]Herb, 0, len(Herbs))
	for _, h := range Herbs {
		if h.Level > herbloreLevel {
			continue
		}
		if len(wanted) > 0 && !wanted[h.Key] {
			continue
		}
		if denied[h.Key] {
			continue
		}
		out = append(out, h)
	}
	return out
}

// Targets is Eligible minus the herbs already confirmed empty in the bank — what a bank trip
// should still try to withdraw, and what TargetsRemaining counts.
func Targets(eligible []Herb, confirmedEmpty map[string]bool) []Herb {
	out := make([]Herb, 0, len(eligible))
	for _, h := range eligible {
		if confirmedEmpty[h.Key] {
			continue
		}
		out = append(out, h)
	}
	return out
}

// ---- chat classifiers ----

// IsLevelRefusal matches content's own line when Herblore is below the herb's level:
// `mes("You cannot identify this herb.")`, followed by "You need a higher Herblore level.".
//
// Note what is NOT available here, unlike GemCutter's equivalent: content prints no number, so
// a refusal cannot tell the caller which level was required. The caller must deny the herb it
// actually clicked rather than re-deriving a level from the message.
func IsLevelRefusal(line string) bool {
	return strings.Contains(strings.ToLower(line), "you cannot identify this herb")
}

// IsMembersRefusal matches the line content prints on a free world — `mes(^mes_members_herblore)`
// in @attempt_identify_herb, whose text is defined in
// content/scripts/general/configs/free_to_play.constant as "You need to be on a members' world
// to gain experience in Herblore." Matched on the distinctive ends of that sentence so a
// different apostrophe or wrapping cannot break it.
//
// Worth classifying separately from a level refusal: both stop the herb being cleaned, but only
// one of them would ever be fixed by training Herblore, and content checks this one FIRST, so a
// free world produces this and never the level line.
func IsMembersRefusal(line string) bool {
	low := strings.ToLower(line)
	return strings.Contains(low, "members") && strings.Contains(low, "gain experience in herblore")
}

// IsIdentifyMessage matches content's success line for any herb. Every row's
// param=identified_herb_message is one of two shapes: "This herb is ..." for the 14 main herbs
// and "You identify the herb. It is ..." for the 5 Karamja ones, so both are accepted.
//
// This is a convenience for logging and for a harness assertion only. Nothing in the run decides
// anything from it: the pack's own item counts are the authority, because a burst of clicks can
// outrun the chat ring buffer.
func IsIdentifyMessage(line string) bool {
	low := strings.ToLower(line)
	return strings.Contains(low, "this herb is ") || strings.Contains(low, "you identify the herb")
}
