package questplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// This file ports defs/runemysteries.ts. It is the first quest def in this slice because it is
// the one EssenceMiner is gated on: content/scripts/skill_runecraft/scripts/essence_mine.rs2
// opens with `if(%runemysteries < ^runemysteries_complete) { mes("You need to have completed
// the Rune Mysteries Quest to use this feature."); return; }`, so every essence teleport —
// Sedridor's, Aubury's, Cromperty's, Distentor's, Brimstail's — refuses until this quest is
// done, and the refusal is an ordinary game message a script that does not read MESSAGE_GAME
// never sees.

// RuneMysteriesName is the quest tab's own spelling, which is the key internal/quests.Status
// takes. It is "Rune Mysteries Quest", NOT "Rune Mysteries": internal/quests/table.go row 18
// is `{"Rune Mysteries Quest", 7335, false}`, matching content 289's questlist.if. Getting
// this wrong returns StatusUnknown forever, and the module would then wait for a journal that
// never loads.
const RuneMysteriesName = "Rune Mysteries Quest"

// RuneMysteriesID is this module's queue id.
const RuneMysteriesID = "runemysteries"

// The three NPCs, their stands and the options to take. Every option string below was read out
// of this content build's own scripts rather than a wiki:
// areas/area_lumbridge/scripts/duke_horacio.rs2, areas/area_wizard_tower/scripts/sedridor.rs2
// and areas/area_varrock/scripts/aubury.rs2.
var (
	// DukeStop is Duke Horacio, upstairs in Lumbridge Castle (level 1).
	DukeStop = NpcStop{
		Npc:    "Duke Horacio",
		Anchor: tile.Tile{X: 3212, Z: 3220, Level: 1},
		Leash:  6,
		// duke_handle_rune_mysteries's multi2/multi3, then duke_rune_mysteries_start's
		// p_choice2. "Sure, no problem." is the branch that sets the varp and hands over
		// the talisman; "Not right now." refuses it.
		Prefer: []string{"Have you any quests for me?", "Sure, no problem."},
	}

	// SedridorStop is the head wizard, in the Wizards' Tower BASEMENT — z 9572 is in the
	// underground band, which is why this module carries hops at all. The npc's content id
	// is head_wizard but his display name is "Sedridor"
	// (quests/quest_runemysteries/configs/quest_runemysteries.npc).
	SedridorStop = NpcStop{
		Npc:    "Sedridor",
		Anchor: tile.Tile{X: 3103, Z: 9572, Level: 0},
		Leash:  8,
		// Three menus across two visits: rune_mysteries' p_choice3, seridor_3's p_choice2
		// and head_wizard_incredible's p_choice2. Order matters — see the note below on
		// why "I'm looking for the head wizard." must come first.
		Prefer: []string{"I'm looking for the head wizard.", "Ok, here you are.", "Yes, certainly."},
		// Why an approach waypoint: the basement's inner door loc is unwalkable, so a
		// long walk aimed straight at the anchor hangs from across the map.
		Approach: []tile.Tile{{X: 3108, Z: 9572, Level: 0}},
	}

	// AuburyStop is Aubury in his Varrock rune shop.
	AuburyStop = NpcStop{
		Npc:    "Aubury",
		Anchor: tile.Tile{X: 3253, Z: 3402, Level: 0},
		Leash:  8,
		// aubury's opnpc1 puts up multi2 or multi3 depending on the varp, and the package
		// line is only present on the multi3. Matching by text rather than index is what
		// makes one prefer list cover both.
		Prefer: []string{"I have been sent here with a package for you."},
	}
)

// WizardHops is the ladder down into and back out of the Wizards' Tower basement.
//
// # These tiles are this repo's, not the TypeScript def's, and the difference is live-found
//
// defs/runemysteries.ts gives the down hop `stand: (3105,3162)` with `walk: (3108,3162)`. Run
// against this content build (cmd/runemysteriestest, 2026-09-21) that stands the account on the
// wrong side of the door at (3107,3162) and the local pathfinder answers
// `no path from 3108,3162,0 to 3105,3162,0: unreachable`, then the engine answers every climb
// with "I can't reach that!" — eight ticks of it, and the quest parked on "talk to Sedridor".
//
// (3105,3162) is not the down ladder's stand tile at all: it is where the UP ladder LANDS.
// internal/webwalk/graph.json already carries both crossings, baked from the map rather than
// guessed, and they are:
//
//	Climb-down  loc 2147 @ (3104,3162)  stand (3104,3163)  ->  lands (3104,9576)
//	Climb-up    loc 2148 @ (3103,9576)  stand (3103,9577)  ->  lands (3105,3162)
//
// The practical consequence is bigger than two corrected tiles: because the graph bakes these
// as ordinary dungeon edges, the webwalk Walker crosses them on its own, and questrunner.GotoNpc
// therefore tries a plain long walk FIRST and only falls back to these hops. They are kept
// because the ported engine's hop machinery is what a quest needs for a level change the graph
// does NOT bake, and because the fallback should be right rather than merely present.
var WizardHops = []LadderHop{
	{
		Stand:   tile.Tile{X: 3104, Z: 3163, Level: 0},
		LocName: "Ladder",
		Op:      "Climb-down",
		Arrive:  tile.Tile{X: 3104, Z: 9576, Level: 0},
	},
	{
		Stand:   tile.Tile{X: 3103, Z: 9577, Level: 0},
		LocName: "Ladder",
		Op:      "Climb-up",
		Arrive:  tile.Tile{X: 3105, Z: 3162, Level: 0},
	},
}

// The three quest items, by their display names as this content build's .obj files spell them.
// "Notes" is the display name of research_notes and "Research package" of research_package
// (quests/quest_runemysteries/configs/quest_runemysteries.obj); neither collides with anything
// else, so the name maps are safe here and InvIDs is not needed.
const (
	AirTalisman     = "air talisman"
	ResearchPackage = "research package"
	ResearchNotes   = "notes"
)

// runeMysteriesProbes is the recovery rotation: when nothing in the pack says where the quest
// is, talk to each of the three in turn until one of them moves it on.
//
// This is the module's answer to a real hole rather than a fallback for tidiness. Two stages
// leave the pack empty and the quest mid-flight — `given_package` (Aubury has taken the
// package and is "looking it over") and `given_talisman` (Sedridor has the talisman but has
// not handed the package back) — and in both the only way forward is to talk to the right NPC
// again. Aubury is first because `given_package` is the stage a run actually lands in.
var runeMysteriesProbes = []NpcStop{AuburyStop, SedridorStop, DukeStop}

// The quest items by exact object id (content/pack/obj.pack: air_talisman, research_package,
// research_notes), and the quest's six varp stages (quest_runemysteries.constant). The stage is
// read from the journal (ParseRuneMysteriesJournal) because the pack cannot say it: two of the
// stages leave the pack empty by design, and a third looks empty whenever the item is banked.
const (
	ObjAirTalisman     = 1438
	ObjResearchPackage = 290
	ObjResearchNotes   = 291

	RMStarted         = 1 // Duke has handed over the talisman: take it to Sedridor
	RMGivenTalisman   = 2 // Sedridor holds the talisman: talk to him again for the package
	RMReceivedPackage = 3 // the package is in hand: take it to Aubury
	RMGivenPackage    = 4 // Aubury holds the package: talk to him again for his notes
	RMReceivedNotes   = 5 // the notes are in hand: take them to Sedridor
	RMComplete        = 6
)

// RuneMysteriesBank is the Draynor bank the quest draws its items from: it is on the way from
// Lumbridge to the Wizards' Tower and on from there to Varrock.
var RuneMysteriesBank = tile.Tile{X: 3093, Z: 3243, Level: 0}

// RuneMysteriesToolIDs are the quest items by id: what a bank visit of the quest keeps.
var RuneMysteriesToolIDs = []int{ObjAirTalisman, ObjResearchPackage, ObjResearchNotes}

// rmJournal are the stage needles, newest first (runemysteries_journal.rs2). Each is the stage's
// own last line, the one text no later stage still carries: the earlier stages' lines stay in the
// journal struck through, so an older needle would match every later stage as well.
var rmJournal = []struct {
	stage  int
	needle string
}{
	{RMComplete, "quest complete"},
	{RMReceivedNotes, "i should take the notes to sedridor"},
	{RMGivenPackage, "i should speak to aubury again when he has finished examining"},
	{RMReceivedPackage, "i should take this research package to aubury"},
	{RMGivenTalisman, "i should talk to the head wizard again"},
	{RMStarted, "i need to find the head wizard and give him the talisman"},
	{0, "i can start this quest by speaking to duke horacio"},
}

// ParseRuneMysteriesJournal reads the quest's stage from the rendered journal (0 to 6). An
// unrecognised or empty journal is not stage 0: it is unread.
func ParseRuneMysteriesJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range rmJournal {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// MineEssenceRefusal is why an account cannot use Aubury's essence teleport yet, or "" when it
// can: content/scripts/skill_runecraft/scripts/essence_mine.rs2 refuses every essence teleport
// until %runemysteries is complete, with an ordinary game message nothing in the fleet reads. Only
// a quest tab that reads complete passes: an uncoloured one is not a reading.
func MineEssenceRefusal(rm Status) string {
	if rm == StatusComplete {
		return ""
	}
	return "Rune Mysteries is " + string(rm) + ", and Aubury's teleport is refused until it is complete"
}

// rmHeld is whether the pack holds a quest item, by id or by its display name.
func rmHeld(s Snapshot, id int, name string) bool { return s.HeldID(id) > 0 || s.Has(name) }

// rmStageRow is what one stage of the quest asks for.
type rmStageRow struct {
	// item is what the step needs in the pack ("" and 0 for none); the quest's own items are what
	// the content refuses to re-issue while the account owns them ANYWHERE.
	itemID   int
	itemName string
	// with is the step when the item is in the pack, without the step when it is nowhere (the NPC
	// who re-hands it).
	with, without NpcStop
}

var rmStages = map[int]rmStageRow{
	RMStarted:         {ObjAirTalisman, AirTalisman, SedridorStop, DukeStop},
	RMGivenTalisman:   {with: SedridorStop},
	RMReceivedPackage: {ObjResearchPackage, ResearchPackage, AuburyStop, SedridorStop},
	RMGivenPackage:    {with: AuburyStop},
	RMReceivedNotes:   {ObjResearchNotes, ResearchNotes, SedridorStop, AuburyStop},
}

// decideRuneMysteriesAtStage is the quest when the journal gave the stage: one step, no probing.
//
// Live 14 Oct 2026, 04:06-04:45: eight Rune Mysteries runs, none complete, each holding one of the
// four fleet-wide slots for 10 to 20 minutes while it walked Aubury, Sedridor and the Duke in turn
// with an empty pack (33 accounts waited, "busy: another account is on its one-at-a-time step").
// The Stage 5 trainer banks the quest's items between runs, and the content counts the BANK
// (obj_gettotal is the pack, the bank and the worn items): with the package banked Sedridor says
// "not yet" and does not re-hand it, Aubury offers no package option, and with the notes banked
// Aubury offers his rune shop ("Yes please!") instead of re-issuing them. So an item the stage
// needs is drawn from the bank first, and only an item that is nowhere is asked of the NPC who
// re-hands it.
func decideRuneMysteriesAtStage(s Snapshot) (Step, bool) {
	row, ok := rmStages[s.Stage]
	if !ok {
		return Step{}, false
	}
	if row.itemID == 0 || rmHeld(s, row.itemID, row.itemName) {
		return Step{Kind: StepTalk, Stop: row.with}, true
	}
	if !s.BankKnown {
		return Step{Kind: StepScanBank, Bank: RuneMysteriesBank, BankSet: true,
			Reason: "the " + row.itemName + " may be banked: the NPCs count the bank and will not re-issue it"}, true
	}
	if n, _ := s.BankedID(row.itemID); n > 0 {
		return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: row.itemName, ID: row.itemID, Qty: 1}},
			Bank: RuneMysteriesBank, BankSet: true, KeepIDs: RuneMysteriesToolIDs,
			Reason: "the " + row.itemName + " the quest needs is in the bank"}, true
	}
	return Step{Kind: StepTalk, Stop: row.without}, true
}

// DecideRuneMysteries is the whole quest. Ported from defs/runemysteries.ts decide().
//
// The held-item ordering below is the state machine, and it is ordered by what each item
// PROVES rather than by the order the quest visits people:
//
//   - Holding the notes proves Aubury has already had the package, so the only move left is
//     Sedridor — checked first so it outranks the talisman branch. That matters because the
//     final handover gives the air talisman BACK
//     (sedridor.rs2 head_wizard_notes: `inv_del(research_notes); inv_add(air_talisman)`), so
//     for one tick the pack can hold both, and checking the talisman first would send the
//     account to Sedridor for a talisman he no longer wants.
//   - Holding the package proves the talisman is delivered, so the move is Aubury.
//   - Holding the talisman alone proves the quest has started and nothing is delivered.
func DecideRuneMysteries(snap Snapshot) Step {
	switch snap.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		// Not StatusNotStarted. The quest tab is uncoloured for the first moments after
		// login, and starting the quest from here would restart a finished one.
		return Step{Kind: StepWait, Reason: "quest journal not loaded"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: DukeStop}
	}
	if snap.StageKnown {
		if step, ok := decideRuneMysteriesAtStage(snap); ok {
			return step
		}
	}
	// The stage is unread: the held items say where the quest is, and an empty pack is probed.
	if snap.Has(ResearchNotes) {
		return Step{Kind: StepTalk, Stop: SedridorStop}
	}
	if snap.Has(ResearchPackage) {
		return Step{Kind: StepTalk, Stop: AuburyStop}
	}
	if snap.Has(AirTalisman) {
		return Step{Kind: StepTalk, Stop: SedridorStop}
	}
	probe := runeMysteriesProbes[mod(snap.NoProgress, len(runeMysteriesProbes))]
	return Step{Kind: StepTalk, Stop: probe}
}

// mod is a non-negative modulo, because NoProgress is an int and Go's % keeps the sign of the
// dividend — a negative would panic on the slice index.
func mod(a, n int) int {
	if n <= 0 {
		return 0
	}
	r := a % n
	if r < 0 {
		r += n
	}
	return r
}

// RuneMysteries is the module.
//
// Requirements is empty and Items is empty, and both are correct rather than unfinished: the
// quest has no skill, quest-point or prerequisite gate in this content build, and it needs no
// items — the Duke hands over the only one it uses, and re-hands it on
// duke_rune_mysteries_in_progress if it is lost.
var RuneMysteries = Module{
	Record: Record{
		ID:          RuneMysteriesID,
		Name:        RuneMysteriesName,
		QuestPoints: 1,
	},
	Bank:      RuneMysteriesBank,
	BankSet:   true,
	Tools:     []string{AirTalisman, ResearchPackage, ResearchNotes},
	ToolIDs:   RuneMysteriesToolIDs,
	Hops:      WizardHops,
	ReadStage: ParseRuneMysteriesJournal,
	Decide:    DecideRuneMysteries,
}
