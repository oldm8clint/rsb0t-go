// Package gemplan is GemCutter's own decisions, ported from the public catalogue's
// rsb0t/src/bot/scripts/GemCutter/GemCutter.ts and its GemCutterLogic.ts sibling. It is pure:
// no client, actions, fleet or bot imports and no I/O, so its tests run with no live server.
//
// The script itself is a bank-standing loop: withdraw a pack of uncut gems, use a chisel on
// each one (OPHELDU — a direct item-on-item use, NOT a make menu; confirmed in the content
// checkout, see "Grounding" below), bank the cut gems, repeat until the bank runs out.
//
// Grounding — every game fact below was confirmed against the content checkout on branch 289
// and against the live local engine's own config archive, not trusted from the TypeScript:
//
//   - The interaction is `[opheldu,chisel]` in content/scripts/skill_crafting/scripts/
//     uncut_gem.rs2, which dispatches straight to `@crafting_gem` and cuts exactly ONE gem per
//     use. There is no make menu and no "how many?" dialog, unlike BankFletcher's knife-on-logs.
//   - Level and XP per gem come from content/scripts/skill_crafting/configs/gem/gem.dbrow.
//     The TypeScript's own table matched the content row for row, so nothing needed correcting
//     here — which is itself worth recording, since a past port found the TS food table wrong.
//   - Every object id below was read back from the RUNNING local engine's config archive
//     (cmd/cachecfgtest -find), not from the TS source: all sixteen gem ids plus the chisel and
//     the crushed gemstone matched the TS values exactly.
//   - Members flags are from that same live archive: opal, jade, red topaz and dragonstone are
//     members-only objects, and content's `@crafting_gem` refuses them outright on a
//     free world (`if (oc_members($uncut_gem) = true) { if (map_members = ^false) ... return; }`).
//     Members is carried per gem so a caller on a free world can tell "cannot cut here" apart
//     from "level too low". The local engine's own data/config/world.json has members true.
//
// XP UNITS, because this is easy to get wrong: the XP column below is in CONTENT units, which
// are tenths of a displayed XP point (`stat_advance(crafting, 500)` for a sapphire is the 50 XP
// a player sees). gamestate's own UPDATE_STAT decoder already divides by ten, so a live XP
// delta read off the session is in DISPLAYED points and is one tenth of the number here. Use
// DisplayXP when comparing the two.
//
// Deliberately not ported here, and why:
//   - The paint/status overlay (GemCutter.ts's own onPaint) — no headless equivalent, the same
//     boundary every other ported script draws.
//   - No mule hand-off: confirmed by reading the whole TS source before writing this, the same
//     check every no-mule port in this codebase documents. GemCutter.ts imports no donorPlan or
//     muleProtocol at all; the bank is the only destination for every trip.
//   - GemCutter.ts's `nearestBank(here)` dynamic bank choice — this port walks to one proven
//     bank stand instead, exactly as bankfletchplan/flaxplan already do, rather than depending
//     on a bank-catalogue primitive this codebase has not proven.
package gemplan

import (
	"regexp"
	"strconv"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// ---- items ----

const (
	// Chisel is the tool every cut needs; ChiselID is its live-confirmed object id.
	Chisel         = "Chisel"
	ChiselID int32 = 1755
	// CrushedGemstone is what a failed cut leaves behind (opal, jade and red topaz only —
	// they are the only rows in gem.dbrow carrying a success_rate).
	CrushedGemstone         = "Crushed gemstone"
	CrushedGemstoneID int32 = 1633
)

// Gem is one row of content's own gem_cutting_table.
type Gem struct {
	// Key is the content debugname's stem, and the stable key settings and logs use.
	Key string
	// Name is the CUT gem's display name ("Sapphire"), which is what the TS settings list
	// offers and what this port's own gems setting accepts.
	Name string
	// UncutName is the uncut object's display name ("Uncut sapphire").
	UncutName string
	// UncutID and CutID are live-confirmed object ids. The noted variant of each sits at
	// id+1 and shares the display NAME, which is why this package matches gems by id and
	// never by name alone — a name match would happily pick up a noted stack the chisel
	// cannot cut.
	UncutID int32
	CutID   int32
	// Level is the Crafting level content requires.
	Level int
	// XP is content units (tenths of a displayed XP point) — see the package doc comment.
	XP int
	// CanCrush marks a gem whose row carries a success_rate, so a cut can fail and leave a
	// Crushed gemstone instead of the cut gem.
	CanCrush bool
	// Members marks a members-only object, which content refuses to cut on a free world.
	Members bool
}

// DisplayXP is the XP a player actually sees for one successful cut — the content number
// divided by ten. Live XP deltas read off a session are in these units.
func (g Gem) DisplayXP() int { return g.XP / 10 }

// Gems is content's gem_cutting_table, lowest level first so the cheapest XP comes first —
// the order GemCutterLogic.ts's own eligibleGems sorts into.
var Gems = []Gem{
	{Key: "opal", Name: "Opal", UncutName: "Uncut opal", UncutID: 1625, CutID: 1609, Level: 1, XP: 150, CanCrush: true, Members: true},
	{Key: "jade", Name: "Jade", UncutName: "Uncut jade", UncutID: 1627, CutID: 1611, Level: 13, XP: 200, CanCrush: true, Members: true},
	{Key: "red_topaz", Name: "Red topaz", UncutName: "Uncut red topaz", UncutID: 1629, CutID: 1613, Level: 16, XP: 250, CanCrush: true, Members: true},
	{Key: "sapphire", Name: "Sapphire", UncutName: "Uncut sapphire", UncutID: 1623, CutID: 1607, Level: 20, XP: 500},
	{Key: "emerald", Name: "Emerald", UncutName: "Uncut emerald", UncutID: 1621, CutID: 1605, Level: 27, XP: 675},
	{Key: "ruby", Name: "Ruby", UncutName: "Uncut ruby", UncutID: 1619, CutID: 1603, Level: 34, XP: 850},
	{Key: "diamond", Name: "Diamond", UncutName: "Uncut diamond", UncutID: 1617, CutID: 1601, Level: 43, XP: 1075},
	{Key: "dragonstone", Name: "Dragonstone", UncutName: "Uncut dragonstone", UncutID: 1631, CutID: 1615, Level: 55, XP: 1375, Members: true},
}

// GemNames lists every cut gem name, the option set the gems setting accepts.
func GemNames() []string {
	names := make([]string, 0, len(Gems))
	for _, g := range Gems {
		names = append(names, g.Name)
	}
	return names
}

// GemByUncutID finds the gem an uncut object id belongs to.
func GemByUncutID(id int32) (Gem, bool) {
	for _, g := range Gems {
		if g.UncutID == id {
			return g, true
		}
	}
	return Gem{}, false
}

// GemByCutID finds the gem a cut object id belongs to.
func GemByCutID(id int32) (Gem, bool) {
	for _, g := range Gems {
		if g.CutID == id {
			return g, true
		}
	}
	return Gem{}, false
}

// GemByName finds a gem by its cut display name or its key, ignoring case and surrounding
// whitespace.
func GemByName(name string) (Gem, bool) {
	want := strings.ToLower(strings.TrimSpace(name))
	for _, g := range Gems {
		if strings.ToLower(g.Name) == want || g.Key == want {
			return g, true
		}
	}
	return Gem{}, false
}

// ---- places ----

// BankStand is the bank every trip works at: the same Seers' Village bank row
// flaxplan/bankfletchplan/woodcutplan already proved live, chosen over GemCutter.ts's own
// dynamic nearestBank for the reason this package's doc comment gives.
var BankStand = tile.Tile{X: 2725, Z: 3493, Level: 0}

const (
	// BoothName and BoothOp are the live-confirmed bank booth loc and the option that opens
	// it (loc 2213 "Bank booth" carries ops Use / Use-quickly).
	BoothName = "Bank booth"
	BoothOp   = "Use-quickly"
)

// PackSlots is a full pack.
const PackSlots = 28

// ---- the loop ----

// Stage is what to do next.
type Stage string

const (
	// StageCut chisels the uncut gems already in the pack.
	StageCut Stage = "cut"
	// StageBank banks the cut gems and withdraws the next load of uncut ones.
	StageBank Stage = "bank"
	// StageDone means every gem this run can cut is confirmed absent from the bank, which is
	// where GemCutter.ts stops itself too.
	StageDone Stage = "done"
)

// Snapshot is the live state DecideStage reads.
type Snapshot struct {
	// HasChisel: a chisel is in the pack.
	HasChisel bool
	// EligibleUncutHeld: how many uncut gems in the pack this run is allowed and able to cut.
	EligibleUncutHeld int
	// TargetsRemaining: how many gem kinds are still worth a bank trip — eligible at this
	// Crafting level, not denied by a refusal, and not yet confirmed empty in the bank.
	TargetsRemaining int
}

// DecideStage picks the next leg, ported from GemCutter.ts's own Cut/BankTrip task validators
// plus the stop condition in its loop(). Cutting wins whenever it is possible at all, so a
// pack part-way through a load is never abandoned for a bank trip; the run ends only once the
// bank is known to hold nothing this account can cut AND the pack has no cuttable gem left.
func DecideStage(s Snapshot) Stage {
	if s.HasChisel && s.EligibleUncutHeld > 0 {
		return StageCut
	}
	if s.TargetsRemaining == 0 {
		return StageDone
	}
	return StageBank
}

// DescribeStage is the status line for a stage.
func DescribeStage(s Stage) string {
	switch s {
	case StageCut:
		return "cutting gems"
	case StageBank:
		return "banking cut gems and withdrawing uncut ones"
	case StageDone:
		return "no cuttable gems left in the bank"
	default:
		return string(s)
	}
}

// Eligible is the gems this run may cut: allowed by Crafting level, restricted to selected
// when it is non-empty, minus any denied key, lowest level first. Ported from
// GemCutterLogic.ts's own eligibleGems plus GemCutter.ts's deniedKeys filter.
//
// membersWorld false drops members-only gems, which content refuses to cut at all on a free
// world — the TS source has no such filter because it never asked the question, and a run that
// kept trying would spin forever on an uncuttable pack.
func Eligible(craftingLevel int, selected []string, denied map[string]bool, membersWorld bool) []Gem {
	wanted := map[string]bool{}
	for _, s := range selected {
		if g, ok := GemByName(s); ok {
			wanted[g.Key] = true
		}
	}
	out := make([]Gem, 0, len(Gems))
	for _, g := range Gems {
		if g.Level > craftingLevel {
			continue
		}
		if len(wanted) > 0 && !wanted[g.Key] {
			continue
		}
		if denied[g.Key] {
			continue
		}
		if g.Members && !membersWorld {
			continue
		}
		out = append(out, g)
	}
	return out
}

// Targets is Eligible minus the gems already confirmed empty in the bank — what a bank trip
// should still try to withdraw, and what TargetsRemaining counts.
func Targets(eligible []Gem, confirmedEmpty map[string]bool) []Gem {
	out := make([]Gem, 0, len(eligible))
	for _, g := range eligible {
		if confirmedEmpty[g.Key] {
			continue
		}
		out = append(out, g)
	}
	return out
}

// ---- chat classifiers ----

// levelRefusal matches content's own objbox line, which is built as
// "You need a Crafting level of <level> to cut <name>." — the same message GemCutter.ts
// watches for, but matched here against the content source rather than the TS string.
var levelRefusal = regexp.MustCompile(`You need a Crafting level of (\d+) to cut `)

// LevelRefusal reports the Crafting level a refusal line demanded. A run that sees one has
// mis-read its own level and should deny that gem rather than retry it forever.
func LevelRefusal(line string) (level int, ok bool) {
	m := levelRefusal.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// IsCutMessage matches content's success line, "You cut the <product>."
func IsCutMessage(line string) bool {
	return strings.Contains(line, "You cut the ")
}

// IsSmashMessage matches content's failure line for a crushable gem: "You mis-hit the chisel
// and smash the <product> to pieces!" — a real outcome, not an error, and one that still
// awards a quarter of the XP.
func IsSmashMessage(line string) bool {
	return strings.Contains(line, "mis-hit the chisel")
}

// IsMembersRefusal matches the line content prints when a members-only gem is chiselled on a
// free world — `mes(^mes_members_feature)` in @crafting_gem, whose text is defined in
// content/scripts/general/configs/free_to_play.constant as "You need to be on a members'
// server to use this feature." Matched on the distinctive middle of that sentence so a
// different apostrophe or wrapping cannot break it.
//
// Worth classifying separately from a level refusal: both stop the gem being cut, but only one
// of them would be fixed by training Crafting, and a run that confused them would deny a gem
// permanently for the wrong reason.
func IsMembersRefusal(line string) bool {
	return strings.Contains(line, "members") && strings.Contains(line, "server to use this feature")
}

// ---- pacing ----

// BurstSize is how many chisel uses one cutting burst sends before waiting for the result.
//
// Why: GemCutter.ts queues up to 60 uses at once, which is a browser client leaning on the
// server to drain them. The public catalogue's own DartFletcherLogic.ts states the engine
// decodes at most five USER_EVENT packets per player each game tick; that claim is the TS
// source's, not independently confirmed here, so five is taken as a conservative ceiling
// rather than a measured optimum. Kept small deliberately: a burst larger than the server can
// decode is not faster, and it makes a genuine stall impossible to tell from a slow tick.
const BurstSize = 5
