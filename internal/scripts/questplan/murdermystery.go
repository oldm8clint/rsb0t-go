package questplan

import (
	"strconv"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Murder Mystery (content id `murder`: %murderquest, %murdersus (the culprit, rolled once at the
// start), %murder_poisonproof_progress and the %murder_evidence bits, none transmitted), from
// the lane Q study (private evidence notes). The guard completes the
// quest on three flags alone: the thread bit, the fingerprint bit and poison progress 3. The
// journal shows the first and the last; the fingerprint bit shows as a held Killers' print. The
// packed stage is 1 (started) + 10 (thread found) + 100 (poison proved).

// MurderMysteryName is the quest tab's spelling (component 7370).
const MurderMysteryName = "Murder Mystery"

// MurderMysteryID is the engine's id.
const MurderMysteryID = "murder"

// Packed stage parts.
const (
	MurderStarted = 1
	MurderThread  = 10
	MurderPoison  = 100
)

// MurderTracks unpacks the packed stage.
func MurderTracks(stage int) (started, thread, poison bool) {
	return stage%10 == 1, stage/10%10 == 1, stage/100 == 1
}

// Ids from content/pack.
const (
	NpcMurderGuard   = 812
	NpcPoisonSeller  = 820
	ObjEmptyPot      = 1931
	ObjPotOfFlour    = 1933
	ObjFlypaper      = 1811
	ObjMurderDagger  = 1813
	ObjDaggerDust    = 1814
	ObjKillersPrint  = 1815
	ObjUnknownPrint  = 1822
	ObjThreadRed     = 1808
	ObjThreadGreen   = 1809
	ObjThreadBlue    = 1810
	LocFlourBarrel   = 2662
	LocMurderSacks   = 2663
	LocSmashedWindow = 2666
)

// Suspect is one of the six: the npc, the silver item in their barrel (plain and dusted), their
// print, and the loc that proves the poison.
type Suspect struct {
	Name               string
	Npc                int
	Anchor             tile.Tile
	Silver, SilverDust int
	Print              int
	Barrel             int
	BarrelTile         tile.Tile
	PoisonLoc          int
	PoisonTile         tile.Tile
}

// Suspects are %murdersus 1..6 (murder_guard.rs2 and quest_murder.rs2).
var Suspects = []Suspect{
	{"Anna", 814, tile.Tile{X: 2734, Z: 3575}, 1796, 1797, 1816, 2656, tile.Tile{X: 2733, Z: 3575}, 2650, tile.Tile{X: 2730, Z: 3572}},
	{"Bob", 815, tile.Tile{X: 2748, Z: 3559}, 1798, 1799, 1817, 2657, tile.Tile{X: 2733, Z: 3578}, 2651, tile.Tile{X: 2730, Z: 3559}},
	{"Carol", 816, tile.Tile{X: 2734, Z: 3581, Level: 1}, 1800, 1801, 1818, 2658, tile.Tile{X: 2733, Z: 3581, Level: 1}, 2652, tile.Tile{X: 2736, Z: 3573}},
	{"David", 817, tile.Tile{X: 2739, Z: 3581}, 1802, 1803, 1819, 2659, tile.Tile{X: 2733, Z: 3578, Level: 1}, 2653, tile.Tile{X: 2740, Z: 3574, Level: 1}},
	{"Elizabeth", 818, tile.Tile{X: 2746, Z: 3581, Level: 1}, 1804, 1805, 1820, 2660, tile.Tile{X: 2747, Z: 3581, Level: 1}, 2654, tile.Tile{X: 2747, Z: 3563}},
	{"Frank", 819, tile.Tile{X: 2742, Z: 3577}, 1806, 1807, 1821, 2661, tile.Tile{X: 2747, Z: 3577, Level: 1}, 2655, tile.Tile{X: 2746, Z: 3573}},
}

// Tiles.
var (
	MurderGuardAnchor = tile.Tile{X: 2741, Z: 3562}
	PoisonSellerAt    = tile.Tile{X: 2737, Z: 3491}
	MurderDaggerTile  = tile.Tile{X: 2746, Z: 3578}
	FlourBarrelTile   = tile.Tile{X: 2735, Z: 3582}
	MurderSacksTile   = tile.Tile{X: 2731, Z: 3582}
	SmashedWindowTile = tile.Tile{X: 2747, Z: 3577}
	SeersBank         = tile.Tile{X: 2725, Z: 3491}
	ArheinShop        = ShopRef{Npc: "Arhein", Anchor: tile.Tile{X: 2803, Z: 3430}}
	murderTools       = []int{CoinsID, ObjEmptyPot, ObjPotOfFlour, ObjFlypaper, ObjMurderDagger, ObjDaggerDust, ObjKillersPrint,
		ObjUnknownPrint, ObjThreadRed, ObjThreadGreen, ObjThreadBlue, 1796, 1797, 1798, 1799, 1800, 1801, 1802, 1803,
		1804, 1805, 1806, 1807, 1816, 1817, 1818, 1819, 1820, 1821, 1812}
)

// ThreadCandidates are the two suspects a thread colour points to (get_murder_thread).
func ThreadCandidates(thread int) []int {
	switch thread {
	case ObjThreadGreen:
		return []int{0, 3} // Anna, David
	case ObjThreadRed:
		return []int{1, 2} // Bob, Carol
	case ObjThreadBlue:
		return []int{4, 5} // Elizabeth, Frank
	}
	return []int{0, 1, 2, 3, 4, 5}
}

// HeldThread is whichever thread the pack holds, or 0.
func HeldThread(s Snapshot) int {
	for _, id := range []int{ObjThreadRed, ObjThreadGreen, ObjThreadBlue} {
		if s.HeldID(id) > 0 {
			return id
		}
	}
	return 0
}

// Culprit is the suspect whose print is held beside the Killers' print (a mismatched suspect print
// is destroyed, a matched one kept), or -1 when the prints do not tell yet.
func Culprit(s Snapshot) int {
	if s.HeldID(ObjKillersPrint) == 0 {
		return -1
	}
	for i, sp := range Suspects {
		if s.HeldID(sp.Print) > 0 {
			return i
		}
	}
	return -1
}

// ParseMurderMysteryJournal reads murder_journal.rs2 into the packed stage.
func ParseMurderMysteryJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	switch {
	case text == "":
		return 0, false
	case strings.Contains(text, "quest complete!"):
		return MurderStarted + MurderThread + MurderPoison, true
	case strings.Contains(text, "i can start this quest"):
		return 0, true
	case strings.Contains(text, "indisputable evidence"):
		// The page stops listing the thread once poison is proved; the window's bit is set by
		// then in every order the runner takes (the thread comes first).
		return MurderStarted + MurderThread + MurderPoison, true
	case strings.Contains(text, "i have found some coloured thread"):
		return MurderStarted + MurderThread, true
	case strings.Contains(text, "lord sinclair"):
		return MurderStarted, true
	}
	return 0, false
}

// Custom step names (internal/scripts/questmods).
const (
	CustomMurderWindow = "murder-window"
	CustomMurderPrints = "murder-prints"
	CustomMurderPoison = "murder-poison"
	KitMurderPot       = "murder-pot"
	// KitMurderEvidence brings the quest's own items back out of the bank (murderEvidenceKit).
	KitMurderEvidence = "murder-evidence"
)

// murderEvidenceIDs are the quest's own items, every one of them a murderTools keep: the thread,
// the dagger and its dust, the prints, the flypaper, the flour, the suspects' silver (plain and
// dusted) and the poisoned pot.
func murderEvidenceIDs() []int {
	var out []int
	for _, id := range murderTools {
		if id != CoinsID && id != ObjEmptyPot {
			out = append(out, id)
		}
	}
	return out
}

// murderEvidenceKit asks for each of them OPTIONALLY, so a provisioning round reads the bank,
// frees the pack of what is not the quest's, and draws what the bank holds - and is satisfied
// when it holds none. Untradeable: the mule is never asked for evidence.
//
// Why: the content refuses to re-issue what the account owns anywhere. The window answers "You
// have already taken the thread" (quest_murder.rs2: obj_gettotal counts the pack, the BANK and
// the worn items), the dagger "I already have the murder weapon in my bank", the poisoned pot
// the same. Every time the Stage 5 trainer banked the account's thread between two runs, the next
// run walked to the window, was told so, and parked ("murder-window (a thread again ...)": 115
// parks and 82 "the dagger is not on the study floor (banked?)" lines, 29 Sep - 2 Oct), because
// decide() only looks in the bank when the engine has read it this run.
func murderEvidenceKit() []KitLine {
	var kit []KitLine
	for _, id := range murderEvidenceIDs() {
		kit = append(kit, KitLine{ID: id, Name: murderItemName(id), Qty: 1, Optional: true, Untradeable: true})
	}
	return kit
}

// murderItemName is an evidence item's log name.
func murderItemName(id int) string {
	switch id {
	case ObjThreadRed, ObjThreadGreen, ObjThreadBlue:
		return "Criminals' thread"
	case ObjMurderDagger:
		return "Murderer's dagger"
	case ObjDaggerDust:
		return "Dusted dagger"
	case ObjKillersPrint:
		return "Killers' print"
	case ObjUnknownPrint:
		return "Unknown print"
	case ObjFlypaper:
		return "Flypaper"
	case ObjPotOfFlour:
		return "Pot of flour"
	}
	return "murder item " + strconv.Itoa(id)
}

// murderNeedsEvidence is whether this run should look in the bank for the quest's items before it
// goes on: the thread bit is set (so something may have been taken), the run did not take the
// thread itself (the window step has not succeeded in it), the kit has not run, and the bank is
// unread or holds an item the pack lacks. A kit that only failed on the way (a walk to the bank
// that timed out) is run again; one that ended blocked is not, and the quest goes on without it.
func murderNeedsEvidence(s Snapshot) bool {
	if s.Ran["provision:"+KitMurderEvidence] || s.Blocked["provision:"+KitMurderEvidence] || s.Ran[CustomMurderWindow] {
		return false
	}
	if !s.BankKnown {
		return true
	}
	for _, id := range murderEvidenceIDs() {
		if n, _ := s.BankedID(id); n > 0 && s.HeldID(id) == 0 {
			return true
		}
	}
	return false
}

var (
	MurderGuardStart = NpcStop{Npc: "Guard", ID: NpcMurderGuard, Anchor: MurderGuardAnchor, Leash: 12, Prefer: []string{"Sure, I'll help."}}
	MurderGuardSolve = NpcStop{Npc: "Guard", ID: NpcMurderGuard, Anchor: MurderGuardAnchor, Leash: 12, Prefer: []string{"I know who did it!"}}
)

// DecideMurderMystery is the whole quest: the thread, then the prints in the thread's order,
// then the poison chain, then the guard.
func DecideMurderMystery(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: MurderGuardStart}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	_, thread, poison := MurderTracks(s.Stage)
	keep := murderTools
	if !thread {
		return Step{Kind: StepCustom, Custom: CustomMurderWindow, Reason: "the thread on the smashed window"}
	}
	printed := s.HeldID(ObjKillersPrint) > 0
	// The guard checks the three flags and never the items, and the prints come before the
	// poison, so a proved poison goes straight to him even if a death lost the Killers' print.
	if poison {
		return Step{Kind: StepTalk, Stop: MurderGuardSolve}
	}
	if murderNeedsEvidence(s) {
		return Step{Kind: StepProvision, KitName: KitMurderEvidence, Kit: murderEvidenceKit(), KeepIDs: keep, Bank: SeersBank, BankSet: true,
			Reason: "the quest's own items back out of the bank (the window, the dagger and the pot refuse a second copy)"}
	}
	if HeldThread(s) == 0 {
		for _, id := range []int{ObjThreadRed, ObjThreadGreen, ObjThreadBlue} {
			if n, _ := s.BankedID(id); n > 0 {
				return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Criminals' thread", ID: id, Qty: 1}},
					Bank: SeersBank, BankSet: true, KeepIDs: keep}
			}
		}
		return Step{Kind: StepCustom, Custom: CustomMurderWindow, Reason: "a thread again (the window re-issues a lost one)"}
	}
	if !printed {
		if s.HeldID(ObjEmptyPot) == 0 && s.HeldID(ObjPotOfFlour) == 0 {
			return Step{Kind: StepProvision, KitName: KitMurderPot, KeepIDs: keep, Bank: SeersBank, BankSet: true,
				Kit:    []KitLine{{ID: ObjEmptyPot, Name: "Pot", Qty: 1, Sources: []Source{{Kind: SourceShop, Shop: ArheinShop, Price: 1}}}},
				Reason: "an empty pot for the flour"}
		}
		return Step{Kind: StepCustom, Custom: CustomMurderPrints, Reason: "the dagger's print against the thread's suspects"}
	}
	return Step{Kind: StepCustom, Custom: CustomMurderPoison, Reason: "the poison: salesman, suspect, then the loc"}
}

// MurderMysteryRecord is the quest's identity (no gates).
var MurderMysteryRecord = Record{ID: MurderMysteryID, Name: MurderMysteryName, QuestPoints: 3}

// MurderMystery is the engine module.
var MurderMystery = Module{
	Record: MurderMysteryRecord, Bank: SeersBank, BankSet: true, Anchor: MurderGuardAnchor,
	ToolIDs:   murderTools,
	ReadStage: ParseMurderMysteryJournal,
	Decide:    DecideMurderMystery,
}
