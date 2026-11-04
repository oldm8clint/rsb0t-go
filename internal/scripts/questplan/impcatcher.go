package questplan

import "rsb0t-go/internal/scripts/tile"

// Imp Catcher (content id `imp`, %imp varp 160, not transmitted), from the lane Q study
// (private evidence notes). The four beads drop 5/128 each from any imp
// whatever the quest stage, have no other source (no spawn, no shop; the mule holds none today)
// and must be in the PACK at the hand-in. So the beads are a provision kit whose last rung is the
// account's own skill: the "imp-farm" method kills imps until the beads are held. About 53 kills
// on average; imps cannot hit (max hit 0) but teleport on 1 in 10 of the hits they survive, so
// melee is advised.

// ImpCatcherName is the quest tab's spelling (component 7340).
const ImpCatcherName = "Imp Catcher"

// ImpCatcherID is the engine's id.
const ImpCatcherID = "imp"

// Ids from content/pack.
const (
	NpcMizgog      = 706
	NpcImp         = 708
	ObjRedBead     = 1470
	ObjYellowBead  = 1472
	ObjBlackBead   = 1474
	ObjWhiteBead   = 1476
	MethodImpFarm  = "imp-farm"
	KitImpBeads    = "imp-beads"
	impFreeForKill = 4
)

// ImpBeads are the four beads.
var ImpBeads = []int{ObjRedBead, ObjYellowBead, ObjBlackBead, ObjWhiteBead}

// ImpField is where the kit names the farm: the stand of the Ardougne-south strip (m41_50, nine imp spawns), the TS def's
// field. The account farms the site it is given at run time (PickImpSite, impfields.go), which is Yanille's strip as often
// as this one; this is the default and the module's anchor. It was (2630,3220) until 16 Oct 2026, a tile in a block of
// trees that no walk can end on (the log's "best" walks to it ended beside it).
var ImpField = ImpSiteArdougne.Stand

// MizgogStop is Wizard Mizgog on the Wizards' Tower top floor. Option 1 of the second menu is
// the only one that starts the quest; the preference order matters because "give me a quest!"
// is also the tail of the rude third option.
var MizgogStop = NpcStop{Npc: "Wizard Mizgog", ID: NpcMizgog, Anchor: tile.Tile{X: 3103, Z: 3163, Level: 2}, Leash: 6,
	Prefer: []string{"Give me a quest please.", "Give me a quest!"}}

func impKit() []KitLine {
	names := map[int]string{ObjRedBead: "Red bead", ObjYellowBead: "Yellow bead", ObjBlackBead: "Black bead", ObjWhiteBead: "White bead"}
	var kit []KitLine
	for _, id := range ImpBeads {
		kit = append(kit, KitLine{ID: id, Name: names[id], Qty: 1, Sources: []Source{{
			Kind: SourceSkill, Skill: "attack", Method: MethodImpFarm, Tile: ImpField,
			Note: "5/128 from any imp",
		}}})
	}
	return kit
}

// impBank is the bank an Imp Catcher bank visit uses: the nearest one to where the account stands, and Draynor's only when
// its position is unknown. Every bank holds the same account's items, and the beads are farmed from the imps at Ardougne's
// south field or Yanille's north one (ImpSites), so the bank walk to Draynor was an errand across the sea. Live 4-8 Oct 2026: 5 of the quest's 6
// deaths, and 14 of its 30 parks since the door fix ("walks to 3093,3243 ended unreachable"), were that walk from Kandarin
// or Karamja: a 120-coin ship fare earned by pickpocketing the Ardougne warrior women, a Karamja coast of poison scorpions,
// and 300 to 470 tiles at 28 to 34 hitpoints. The one visit that does need the Draynor side, the hand-in to Wizard Mizgog,
// is its own StepTalk.
func impBank(s Snapshot) tile.Tile {
	if s.TileKnown {
		return NearestBank(s.Tile).Tile
	}
	return DraynorBank
}

// DecideImpCatcher is the whole quest. The beads can be farmed before the quest starts, so the
// order is free; this starts first, as content does not care.
func DecideImpCatcher(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: MizgogStop}
	}
	for _, b := range ImpBeads {
		if s.HeldID(b) == 0 {
			bank := impBank(s)
			if s.FreeSlots < impFreeForKill {
				return Step{Kind: StepDeposit, KeepIDs: append(append([]int(nil), ImpBeads...), CoinsID), Bank: bank, BankSet: true}
			}
			return Step{Kind: StepProvision, KitName: KitImpBeads, Kit: impKit(), KeepIDs: ImpBeads,
				Bank: bank, BankSet: true, Reason: "the four beads: bank, mule, then imps"}
		}
	}
	return Step{Kind: StepTalk, Stop: MizgogStop}
}

// ImpCatcherRecord is the quest's identity (no server gates).
var ImpCatcherRecord = Record{ID: ImpCatcherID, Name: ImpCatcherName, QuestPoints: 1}

// ImpCatcher is the engine module. Its Bank is Draynor's but not forced (BankSet false): every bank step names the bank
// nearest the account (impBank), and a lost fight retreats to the nearest bank too, not across the sea to Draynor.
var ImpCatcher = Module{
	Record: ImpCatcherRecord, Bank: DraynorBank, BankSet: false, Anchor: ImpField,
	ToolIDs: ImpBeads,
	Decide:  DecideImpCatcher,
}
