package questplan

import (
	"fmt"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// The Holy Grail (content id `grail`, %grail, not transmitted), from the quest-chain study
// (private evidence notes). What the port relies on:
//
//   - Merlin's Crystal must be complete (Arthur offers the Grail only then) and Excalibur is
//     needed again: the Black Knight Titan (120, 142 HP, max 15) dies only if Excalibur is WORN
//     when he reaches 0 (the damage source is not checked, so a spell kill with it worn counts);
//   - Entrana refuses every weapon and armour piece, so the account strips at a bank first;
//   - the whistles (two) come from Draynor Manor's top-floor door while the napkin is held, and
//     only blow from the "six heads" plot on Karamja (x 2740-2743, z 3234-3237);
//   - beating the Titan changes no stage, so position proves the crossing;
//   - the Fisher King's diagonal door has no webwalk edge; the realm leg is one custom step;
//   - the Titan stands on the realm's only bridge, so nothing past him (the Fisherman, the bell, the
//     castle) is reachable from the arrival pocket until he has fallen. How the fight is won, and
//     why every account of 13 Oct 2026 stood on 2793,4721 swinging at nothing, is grailtitan.go.

// HolyGrailName is the quest tab's spelling.
const HolyGrailName = "Holy Grail"

// HolyGrailID is the engine's id.
const HolyGrailID = "grail"

// Stages (quest_grail.constant).
const (
	GrailStarted     = 2
	GrailSpokeMerlin = 3
	GrailSpokeCrone  = 4
	GrailFailedTitan = 7
	GrailFindPercy   = 8
	GrailGaveWhistle = 9
	GrailComplete    = 10
)

// Ids from content/pack.
const (
	NpcMerlinWorkshop = 213
	NpcHighPriest     = 216
	NpcGalahad        = 218
	NpcGrailFisherman = 219
	NpcFisherKing     = 220
	NpcTitan          = 221
	NpcPercival       = 211
	NpcShipMonk       = 657
	// NpcShipMonkEntrana is the Entrana-side monk who sails back (shipmonk2, m44_52).
	NpcShipMonkEntrana = 658
	ObjNapkin          = 15
	ObjMagicWhistle    = 16
	ObjGrailBell       = 17
	ObjHolyGrail       = 19
	LocMerlinWorkshop  = 24
	LocWhistleDoor     = 22
	LocPercySacks      = 23
)

// Tiles from content maps and the study.
var (
	MerlinWorkshopDoor = tile.Tile{X: 2764, Z: 3503, Level: 1}
	MerlinWorkshopNpc  = tile.Tile{X: 2767, Z: 3500, Level: 1}
	HighPriestAnchor   = tile.Tile{X: 2851, Z: 3349}
	GalahadAnchor      = tile.Tile{X: 2612, Z: 3474}
	WhistleDoorTile    = tile.Tile{X: 3106, Z: 3361, Level: 2}
	WhistleTake        = tile.Tile{X: 3106, Z: 3359, Level: 2}
	SixHeads           = tile.Tile{X: 2741, Z: 3235}
	TitanTile          = tile.Tile{X: 2791, Z: 4722}
	GrailFisherman     = tile.Tile{X: 2802, Z: 4706}
	GrailBellRing      = tile.Tile{X: 2763, Z: 4693}
	FisherKingTile     = tile.Tile{X: 2762, Z: 4688, Level: 1}
	FisherKingDoorSt   = tile.Tile{X: 2767, Z: 4688, Level: 1}
	PercySacks         = tile.Tile{X: 2960, Z: 3507}
	GrailTableTile     = tile.Tile{X: 2649, Z: 4684, Level: 2}
	PortSarimMonks     = tile.Tile{X: 3045, Z: 3235}
	FaladorEastBank    = tile.Tile{X: 3013, Z: 3355}
	grailFoods         = []int{385, 373, 379, 365, 361, 329, 1891, 1893, 1895, 333, 2309, 2142}
	grailTools         = []int{ObjNapkin, ObjMagicWhistle, ObjGrailBell, ObjHolyGrail, ObjExcalibur, CoinsID}
	// grailKeep is what a bank visit of this quest leaves in the pack: the tools and the runes the
	// Titan fight casts with.
	grailKeep = append(append([]int(nil), grailTools...), TitanRuneIDs()...)
)

// InGrailRealm reports whether t is in the Fisher King's realm (map squares 41-43, 73).
func InGrailRealm(t tile.Tile) bool { return t.X>>6 >= 41 && t.X>>6 <= 43 && t.Z>>6 == 73 }

// EntranaDock is the middle of the three Entrana ship monks (content m44_52: 2830,3335,
// 2835,3336, 2839,3335).
var EntranaDock = tile.Tile{X: 2835, Z: 3336}

// OnEntrana reports whether t is on Entrana.
func OnEntrana(t tile.Tile) bool { return t.X >= 2800 && t.X <= 2870 && t.Z >= 3320 && t.Z <= 3395 }

// Custom step names (internal/scripts/questmods).
const (
	CustomGrailMerlin       = "grail-merlin"
	CustomGrailEntrana      = "grail-entrana"
	CustomGrailLeaveEntrana = "grail-leave-entrana"
	CustomGrailWhistles     = "grail-whistles"
	CustomGrailRealm        = "grail-realm"
	CustomGrailPercival     = "grail-percival"
	CustomGrailTake         = "grail-take"
	CustomGrailWear         = "grail-wear-excalibur"
	KitGrailFight           = "grail-fight"
	// CustomGrailLeaveRealm blows the account out of the arrival pocket when it stands there
	// without Excalibur worn.
	CustomGrailLeaveRealm = "grail-leave-realm"
)

var (
	ArthurGrailStop = NpcStop{Npc: "King Arthur", ID: NpcKingArthur, Anchor: ArthurAnchor, Leash: 6,
		Prefer: []string{"Tell me of this quest.", "I'd enjoy trying that."}}
	GalahadStop = NpcStop{Npc: "Brother Galahad", ID: NpcGalahad, Anchor: GalahadAnchor, Leash: 6,
		Prefer: []string{"I seek an item from the realm of the Fisher King.", "I have lost the holy table napkin"}}
)

var grailJournal = []struct {
	stage  int
	needle string
}{
	{GrailComplete, "quest complete!"},
	{GrailGaveWhistle, "i honoured the fisher king's request"},
	{GrailFindPercy, "the fisher king is very sick"},
	{GrailFailedTitan, "i need to find a weapon"},
	{GrailSpokeCrone, "according to a crone on entrana"},
	{GrailSpokeMerlin, "merlin suggested two things"},
	{GrailStarted, "i should start my quest by speaking to"},
	{0, "i can start this quest by speaking to"},
}

// ParseHolyGrailJournal reads grail_journal.rs2's rendered stage.
func ParseHolyGrailJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range grailJournal {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// grailFightKit is what the Titan fight carries: Excalibur (taken from the bank when it is there; a
// copy already WORN counts, and a kit that cannot find one is not blocked on it, because the quest
// gets it back from the Lady of the Lake for 500 coins itself, grailExcaliburStep), a little food
// (he cannot reach a caster two tiles away; poison and the walk past him are what bite), and the
// runes of the spells that kill him (TitanRuneLines). The rune lines are optional so that a kit
// never blocks on the family a bank lacks; DecideHolyGrail refuses to go on without enough of them.
//
// Live 13 Oct 2026: with the line a hard "1 Excalibur: a quest item (no mule, no shop)" and no Worn
// flag, every account that was already wearing Excalibur read the kit as BLOCKED, and the food and
// runes it came for were never drawn.
func grailFightKit() []KitLine {
	kit := []KitLine{
		{ID: ObjExcalibur, Name: "Excalibur", Qty: 1, Untradeable: true, Optional: true, Worn: true},
		{ID: 385, Name: "Shark", Qty: 12, Optional: true},
		{ID: 379, Name: "Lobster", Qty: 4, Optional: true},
	}
	return append(kit, TitanRuneLines()...)
}

// grailKitDue is whether the fight kit still has to be provisioned: not once it has been (Ran), and
// not when its sources are dead (Blocked); a round that only failed on the way, a long walk to the
// bank that timed out say, is tried again from where the walk got to. Live 13 Oct 2026: every
// "could not use the bank at 2809,3440,0" was a six minute walk from Draynor Manor timing out 90
// tiles short, after which the kit was never retried and the account went on without its food.
func grailKitDue(s Snapshot) bool {
	k := "provision:" + KitGrailFight
	return !s.Ran[k] && !s.Blocked[k]
}

// DecideHolyGrail is the whole quest.
func DecideHolyGrail(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: ArthurGrailStop}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	switch s.Stage {
	case GrailStarted:
		return Step{Kind: StepCustom, Custom: CustomGrailMerlin, Reason: "Merlin in his Camelot workshop"}
	case GrailSpokeMerlin:
		return Step{Kind: StepCustom, Custom: CustomGrailEntrana, Reason: "the High Priest on Entrana (no weapons or armour)"}
	case GrailSpokeCrone, GrailFailedTitan:
		// The walker has no way off Entrana: the questproof EMPTY lab run grail-empty-2 took
		// the High Priest's brief and then tried to walk to Brother Galahad from the island,
		// unreachable, for the rest of its budget. The Entrana monks sail it back.
		if s.TileKnown && OnEntrana(s.Tile) {
			return Step{Kind: StepCustom, Custom: CustomGrailLeaveEntrana, Reason: "the Entrana monks sail back to Port Sarim"}
		}
		if s.TileKnown && InGrailRealm(s.Tile) {
			// The Titan is invincible to an account that does not wear Excalibur, and trying him
			// sets the quest back to stage 7: never meet him without it.
			if OnTitanSide(s.Tile) && s.WornIDs[ObjExcalibur] == 0 {
				return Step{Kind: StepCustom, Custom: CustomGrailLeaveRealm, Reason: "in the arrival pocket without Excalibur worn: the Titan is invincible to it"}
			}
			return Step{Kind: StepCustom, Custom: CustomGrailRealm, Reason: "in the realm: the Titan, the bell, the Fisher King"}
		}
		// The napkin: the bank first (Galahad counts it as the account's own and the account banks it between two runs),
		// a free slot for his script, and only then Galahad (grailbank.go).
		if st, ok := grailNapkinStep(s); ok {
			return st
		}
		// The kit comes before the whistles: Galahad's house is 200 tiles from the Catherby bank, and
		// Draynor Manor is 800 steps of route from it (live 13 Oct 2026: every kit that came after
		// the whistles timed its six minute walk out 90 tiles short).
		if grailKitDue(s) {
			return Step{Kind: StepProvision, KitName: KitGrailFight, Kit: grailFightKit(),
				KeepIDs: append(append([]int(nil), grailKeep...), grailFoods...), Bank: CatherbyBank, BankSet: true,
				Reason: "Excalibur, food and runes for the Black Knight Titan"}
		}
		// The runes are REQUIRED, though the kit's lines are optional (see TitanRunesEnough): no
		// whistles, no Karamja and no realm on runes that cannot kill him. Parked, the quest frees its
		// one-at-a-time slot and the next run provisions the kit afresh.
		if s.Level("magic") <= 0 {
			return Step{Kind: StepWait, Reason: "grail: the Magic level has not been read, so the runes cannot be weighed"}
		}
		if ok, why := TitanRunesEnough(s.Level("magic"), s.InvIDs, 0, 0); !ok {
			return Step{Kind: StepWait, Reason: TitanParkReasonPrefix + why, ParkFor: TitanParkKit}
		}
		// Two whistles (one in, one out): the bank's, when it holds them, before Draynor Manor's top floor (grailbank.go).
		if st, ok := grailWhistleStep(s, grailWhistlesNeeded, true, CatherbyBank, "two whistles from Draynor Manor's top floor"); ok {
			return st
		}
		if s.WornIDs[ObjExcalibur] == 0 {
			if !held(ObjExcalibur) {
				return grailExcaliburStep(s)
			}
			return Step{Kind: StepEquip, ItemID: ObjExcalibur, Item: "Excalibur"}
		}
		// The bell the bank holds, drawn before the whistle: nothing can be drawn in the realm.
		if st, ok := grailBellStep(s, CatherbyBank); ok {
			return st
		}
		return Step{Kind: StepCustom, Custom: CustomGrailRealm, Reason: "blow a whistle at the six heads"}
	case GrailFindPercy:
		if s.TileKnown && InGrailRealm(s.Tile) {
			return Step{Kind: StepCustom, Custom: CustomGrailRealm, Reason: "blow out of the realm"}
		}
		if st, ok := grailWhistleStep(s, 1, false, CatherbyBank, "a whistle for Sir Percival"); ok {
			return st
		}
		return Step{Kind: StepCustom, Custom: CustomGrailPercival, Reason: "Sir Percival in the Goblin Village sacks"}
	case GrailGaveWhistle:
		if held(ObjHolyGrail) {
			if s.TileKnown && InGrailRealm(s.Tile) {
				return Step{Kind: StepCustom, Custom: CustomGrailTake, Reason: "blow out with the Grail"}
			}
			return Step{Kind: StepTalk, Stop: ArthurGrailStop}
		}
		// A Grail in the bank is the account's own: content's pickup counts the bank and refuses a second.
		if !(s.TileKnown && InGrailRealm(s.Tile)) {
			if st, ok := grailGrailStep(s); ok {
				return st
			}
			if st, ok := grailWhistleStep(s, 1, false, CatherbyBank, "a whistle to reach the renewed realm"); ok {
				return st
			}
		}
		return Step{Kind: StepCustom, Custom: CustomGrailTake, Reason: "the Grail in the renewed realm"}
	}
	return Step{Kind: StepWait, Reason: "grail: unrecognised stage"}
}

// KitGrailExcaliburCoins is the coins the Lady of the Lake takes for a lost Excalibur.
const KitGrailExcaliburCoins = "grail-excalibur-coins"

// GrailExcaliburPrice is what the Lady of the Lake asks for Excalibur once it has been bound
// (lady_of_the_lake.rs2: %arthur past arthur_excalibur_bound, and no Excalibur in the pack, the
// bank or worn).
const GrailExcaliburPrice = 500

// LadyExcaliburStop is the Lady of the Lake's resale: "I seek the sword Excalibur." is offered
// only while the account owns none, and after Merlin's Crystal she returns it for 500 coins.
var LadyExcaliburStop = NpcStop{Npc: "The Lady of the Lake", ID: NpcLadyOfTheLake, Anchor: LadyAnchor, Leash: 6,
	Prefer: []string{"I seek the sword Excalibur."}}

// grailExcaliburStep gets Excalibur back when it is not in the pack or worn. Banked, it is the
// fight kit's line (withdrawn from the bank); owned nowhere (lost, or a lab account that never
// had it), the quest acquires it itself by the item rule: 500 coins from the bank or the mule,
// then the Lady of the Lake's resale. The old step waited here forever.
func grailExcaliburStep(s Snapshot) Step {
	if n, known := s.BankedID(ObjExcalibur); known && n > 0 {
		// KeepIDs: a pack with no room is banked first, and the food and runes are not the junk.
		return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Excalibur", ID: ObjExcalibur, Qty: 1}}, Bank: CatherbyBank, BankSet: true,
			KeepIDs: append(append([]int(nil), grailKeep...), grailFoods...)}
	}
	if !s.BankKnown {
		return Step{Kind: StepScanBank, Bank: CatherbyBank, BankSet: true}
	}
	if s.HeldID(CoinsID) < GrailExcaliburPrice {
		if s.Failed["provision:"+KitGrailExcaliburCoins] {
			return Step{Kind: StepWait, Reason: fmt.Sprintf("grail: no Excalibur and not %d coins from the bank or the mule for the Lady of the Lake", GrailExcaliburPrice)}
		}
		return Step{Kind: StepProvision, KitName: KitGrailExcaliburCoins, Bank: CatherbyBank, BankSet: true,
			Kit:     []KitLine{{ID: CoinsID, Name: "Coins", Qty: GrailExcaliburPrice, Stackable: true}},
			KeepIDs: append(append([]int(nil), grailKeep...), grailFoods...),
			Reason:  "coins for the Lady of the Lake's Excalibur"}
	}
	return Step{Kind: StepTalk, Stop: LadyExcaliburStop, UntilItemID: ObjExcalibur}
}

// HolyGrailRecord is the quest's identity: Merlin's Crystal is the server's gate; Attack 20 is
// needed to wield Excalibur for the Titan.
var HolyGrailRecord = Record{ID: HolyGrailID, Name: HolyGrailName, QuestPoints: 2,
	Requirements: Requirements{Quests: []string{MerlinsCrystalID}, Skills: []SkillReq{{Skill: "attack", Level: 20}}}}

// HolyGrail is the engine module.
var HolyGrail = Module{
	Record: HolyGrailRecord, Bank: CatherbyBank, BankSet: true, Anchor: CatherbyBank,
	ToolIDs:   grailKeep,
	ReadStage: ParseHolyGrailJournal,
	Sustain:   Sustain{FoodIDs: grailFoods, EatAt: 16},
	MaxDeaths: 3,
	Decide:    DecideHolyGrail,
	// The Titan is the one-at-a-time step: see GrailPastTitan.
	ExclusiveDone: GrailPastTitan,
}
