package questplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Gertrude's Cat (content id `fluffs`, %fluffs 0 not started .. 5 rescued, 6 complete; not transmitted), ported from the TS def
// (defs/gertrudescat.ts) and checked against content 289: quests/quest_fluffs/scripts/quest_fluffs.rs2, fluffs_journal.rs2
// and areas/area_varrock/scripts/{gertrude,shilop,wilough}.rs2.
//
// The chain, one server stage a step (each step is idempotent, so a relog or a death resumes where the journal says):
//
//	0 -> 1  Gertrude (a house west of Varrock) asks the account to look for Fluffs: "Well, I suppose I could."
//	1 -> 2  Shilop and Wilough, her sons, in the Varrock market: 100 coins buy the lumber yard's place out of them. See below.
//	2 -> 3  Fluffs sits on the lumber yard's raised walkway: a bucket of milk used on her (Pick-up and Stroke only make her claw
//	        for 3 damage, so every offer is an item used on the cat).
//	3 -> 4  A seasoned sardine (a raw sardine with doogle leaves rubbed over it) used on her. The server rolls which of six crates
//	        holds her kitten and keeps it in %fluffs_crate.
//	4 -> 5  Searching the six crate NPCs finds the kitten in one ("You find a kitten!", 4 ticks a search); the kitten used on
//	        Fluffs ends the cutscene with her running home.
//	5 -> 6  Gertrude again: a pet kitten (a follower), a chocolate cake and a stew, 1 quest point and 1525 Cooking XP
//	        (stat_advance(cooking, 15250)).
//
// What a script must plan for, from the content and the TS def's own notes:
//
//   - THE BROTHERS MUST STAND CLOSE. The script that takes the 100 coins branches on .npc_find(npc_coord, $other_boy, 3,
//     vis_lineofsight) four chat lines in, and the branch without the other brother near ends the conversation with no
//     options at all (fluffs_boy's else arm has no jump to fluffs_boy_single). So the account opens the conversation only while
//     the two stand within FluffsBrotherGap of each other, and only the coins leaving the pack prove the place was bought; a
//     conversation that ended without them is tried again (FluffsPayAttempts). They spawn side by side (3221,3434 and 3221,3435)
//     and wander two tiles from there.
//   - THE FENCE IS THE ONLY WAY INTO THE YARD. The yard is walled on its south side along z 3492-3493 except one gap whose tile
//     holds the "Broken fence" loc (2618, op1 Climb-over, an exact move over the wall), so the walker never plans into it and
//     the module crosses it by hand, both ways. Another account's climb locks the fence for 3 ticks ("The fence is being climbed
//     by someone else."), which is a retry, not a failure.
//   - THE LADDER LANDS ON THE SAME TILE. The ladder to the walkway (loc 1747 at 3310,3509) puts the player at its OWN x,z one
//     level up (ladders.rs2 climb_ladder(movecoord(coord(), 0, 1, 0))), so it is climbed from a tile whose upper twin is walkable;
//     all seven neighbours of the ladder are (checked on the real collision pack).
//   - A TALK MID-QUEST IS NOT A CLOSE. The seasoning, the kitten's hand-over and Gertrude's last pages are chat boxes that wait
//     for the continue button; closing the modal instead aborts the script (Player.closeModal clears a script suspended on a
//     pause button), so the module presses continue and never closes them.
//   - THE SEARCH IS SERVER-SIDE PATHING. A crate NPC's "Search" is walked to by the server from wherever the account stands, and a
//     long walk through the yard's inner walls gives up short with no refusal (TS def); the module walks beside the crate first
//     (the collision pack plans the yard whole: every crate has a flood-reachable neighbour from the fence).
//
// Where the items come from, in the owner's item rule (provision.go): the account's own bank, then the mule, then the world.
//   - Coins: the bank or the mule (the mule holds fifty million).
//   - Doogle leaves: five ground spawns in the woods behind Gertrude's house (m49_53). Raw sardine: Gerrant's fishing shop in Port
//     Sarim (stock 200, 10 gp). A seasoned sardine from the bank is used as it is.
//   - A bucket of milk: the bank or the mule (three on 18 Oct 2026), else an empty bucket (the farmhouse floor or the Lumbridge
//     general store, as Cook's Assistant) milked from a Lumbridge dairy cow by Cook's Assistant's own proven step.
//
// The route is Gertrude, the market, the woods behind Gertrude's house, Port Sarim, the Lumbridge farm, the lumber yard, and back
// to Gertrude: the items are fetched in that order so that no leg is walked twice. Nothing in it fights and nothing in it goes
// near the Wilderness (the yard's last row is z 3517 and the line is z 3520; TestFluffsTilesAreOutsideTheWilderness).

// GertrudesCatName is the quest tab's spelling (component 7360, `questlist:fluffs`).
const GertrudesCatName = "Gertrude's Cat"

// GertrudesCatID is the engine's id.
const GertrudesCatID = "fluffs"

// The %fluffs stages (quest_fluffs.constant and general/configs/quest.constant).
const (
	FluffsNotStarted  = 0
	FluffsStarted     = 1
	FluffsPaidBoy     = 2
	FluffsGaveMilk    = 3
	FluffsGaveSardine = 4
	FluffsRescued     = 5
	FluffsComplete    = 6
)

// Ids from content/pack (checked against the production config by TestDoricFluffsIDsMatchTheProductionConfig).
const (
	NpcGertrude    = 780  // [gertrude]
	NpcShilop      = 781  // [shilop]
	NpcWilough     = 783  // [wilough]
	NpcFluffs      = 759  // [gertrudescat]: op1 Pick-up and op3 Stroke claw for 3; only an item used on her counts
	NpcKittenCrate = 767  // [kittens_mew]: op1 Search
	LocYardFence   = 2618 // [gertrudefence], "Broken fence", op1 Climb-over
	LocYardLadder  = 1747 // [ladder], "Ladder", op1 Climb-up (the walkway's twin is 1746 [laddertop])
	LocYardTop     = 1746

	FluffsDoogleLeaves    = 1573 // [doogleleaves]
	FluffsRawSardine      = 327  // [raw_sardine]
	FluffsSeasonedSardine = 1552 // [seasoned_sardine]
	FluffsKitten          = 1554 // [gertrudekittens], "Fluffs' kitten"
)

// FluffsBoyFee is what the brothers want (fluffs_pay: inv_del(inv, coins, 100)).
const FluffsBoyFee = 100

// FluffsBrotherGap is how far apart, at most, the brothers may stand when the conversation is opened (Chebyshev). The script's own
// limit is 3 four chat lines in and they wander two tiles each, so starting at 1 leaves the room they drift in.
const FluffsBrotherGap = 1

// FluffsPayAttempts is how many conversations a run has with the brothers before the step gives up and the engine's watchdog
// takes over.
const FluffsPayAttempts = 6

// BrothersPaired reports whether Shilop at a and Wilough at b stand close enough to start the conversation.
func BrothersPaired(a, b tile.Tile) bool {
	return a.Level == b.Level && tile.Chebyshev(a, b) <= FluffsBrotherGap
}

// Tiles from content maps (m49_53, m50_54, m51_54, m46_52 and the loc and npc indexes) and the keepers.
var (
	// GertrudeAnchor is Gertrude (m49_53, 3151,3410).
	GertrudeAnchor = tile.Tile{X: 3151, Z: 3410}
	// FluffsMarket is where the brothers spawn (Shilop 3221,3434, Wilough 3221,3435) in Varrock's market.
	FluffsMarket = tile.Tile{X: 3221, Z: 3434}
	// DoogleSpawns are the doogle leaves behind Gertrude's house (m49_53 obj 1573), DoogleField their middle.
	DoogleSpawns = []tile.Tile{{X: 3151, Z: 3399}, {X: 3152, Z: 3399}, {X: 3152, Z: 3401}, {X: 3156, Z: 3401}, {X: 3157, Z: 3400}}
	DoogleField  = tile.Tile{X: 3153, Z: 3400}
	// FluffsSardineShop is Gerrant, Port Sarim's fishing shop keeper (m46_50, 3013,3225); he wanders a few tiles.
	FluffsSardineShop = ShopRef{Npc: "Gerrant", NpcID: 558, Anchor: tile.Tile{X: 3013, Z: 3225}}

	// YardOutside is the tile south of the fence, where the climb in begins; YardFence is the fence's own tile (the climb in ends
	// on it and the climb out begins on it) and YardInside the first tile north of it.
	YardOutside = tile.Tile{X: 3305, Z: 3492}
	YardFence   = tile.Tile{X: 3305, Z: 3493}
	YardInside  = tile.Tile{X: 3305, Z: 3494}
	// YardLadder is the ladder up to the walkway (level 0), and its top the same x,z a level up.
	YardLadder    = tile.Tile{X: 3310, Z: 3509}
	YardLadderTop = tile.Tile{X: 3310, Z: 3509, Level: 1}
	// YardLadderStand is a tile beside the ladder whose upper twin is on the walkway.
	YardLadderStand = tile.Tile{X: 3310, Z: 3508}
	// FluffsSpot is where Fluffs spawns on the walkway (m51_54 npc 759 at level 1), CatStand a tile beside it.
	FluffsSpot = tile.Tile{X: 3306, Z: 3512, Level: 1}
	CatStand   = tile.Tile{X: 3307, Z: 3512, Level: 1}
	// FluffsCrateTiles are the six crate NPCs (m51_54), one of which holds the kitten (the %fluffs_crate roll).
	FluffsCrateTiles = []tile.Tile{{X: 3305, Z: 3500}, {X: 3310, Z: 3499}, {X: 3303, Z: 3506}, {X: 3307, Z: 3507}, {X: 3311, Z: 3511}, {X: 3298, Z: 3514}}
)

// WildernessLine is the first row of the Wilderness (content: level 1 begins at z 3520 on level 0).
const WildernessLine = 3520

// InYard reports a tile inside the lumber yard's bounds on the ground floor (south edge z 3493, north edge z 3517, the walls'
// west side at x 3293). The ground west of x 3293 and beside the walls is outside them: where the collision pack is known, a path
// from the tile to YardInside is the proof (questmods fluffsInside).
func InYard(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 3293 && t.X <= 3326 && t.Z >= YardFence.Z && t.Z < WildernessLine
}

// OnWalkway reports a tile on the yard's raised walkway.
func OnWalkway(t tile.Tile) bool {
	return t.Level == 1 && t.X >= 3295 && t.X <= 3320 && t.Z >= 3500 && t.Z < WildernessLine
}

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomFluffsPay    = "fluffs-pay"
	CustomFluffsSeason = "fluffs-season"
	CustomFluffsMilk   = "fluffs-milk"
	CustomFluffsOffer  = "fluffs-offer"
	CustomFluffsCrates = "fluffs-crates"
	CustomFluffsReport = "fluffs-report"

	KitFluffsCoins  = "fluffs-coins"
	KitFluffsItems  = "fluffs-items"
	KitFluffsSard   = "fluffs-sardine"
	KitFluffsBucket = "fluffs-bucket"
	KitFluffsKitten = "fluffs-kitten"
)

var (
	// GertrudeStop starts the quest at its one menu ("Well, I suppose I could." / "What's in it for me?" / "Sorry, I'm too busy to
	// play pet rescue.") and takes the cat's return (the last page has no menu).
	GertrudeStop = NpcStop{Npc: "Gertrude", ID: NpcGertrude, Anchor: GertrudeAnchor, Leash: 8,
		Prefer: []string{"Well, I suppose I could."}}
	// ShilopStop and WilloughStop are the two brothers. Their menus: "Tell me sonny, or I will hurt you." / "What will make you
	// tell me?" / "Well never mind, it's Fluffs' loss.", then "I'm not paying you a penny." / "Okay then, I'll pay." The first
	// menu's answer must not be the threat (it ends the conversation unpaid), nor the refusal.
	ShilopStop = NpcStop{Npc: "Shilop", ID: NpcShilop, Anchor: FluffsMarket, Leash: 10,
		Prefer: []string{"What will make you tell me?", "I'll pay"}}
	WilloughStop = NpcStop{Npc: "Wilough", ID: NpcWilough, Anchor: FluffsMarket, Leash: 10,
		Prefer: []string{"What will make you tell me?", "I'll pay"}}
)

// fluffsKeep is what a bank visit of this quest leaves in the pack: the coins and every item the chain passes through.
var fluffsKeep = []int{CoinsID, FluffsDoogleLeaves, FluffsRawSardine, FluffsSeasonedSardine, ObjBucketOfMilk, ObjBucket, FluffsKitten}

// FluffsKeepIDs returns the keep list (a copy).
func FluffsKeepIDs() []int { return append([]int(nil), fluffsKeep...) }

// fluffsCoinsDraw and fluffsCoinsMin are the coins the brothers' step draws: the fee, the sardine and the bucket with change.
const (
	fluffsCoinsDraw = 300
	fluffsCoinsMin  = 130
)

// fluffsJournalNeedles are ParseGertrudesCatJournal's phrases, newest first, written against NormaliseLines (fluffs_journal.rs2:
// the finished lines are struck through but kept, so the newest sentence decides).
var fluffsJournalNeedles = []struct {
	stage  int
	needle string
}{
	{FluffsComplete, "quest complete"},
	{FluffsRescued, "i gave fluffs her kitten back"},
	{FluffsGaveSardine, "i gave the cat milk and sardines"},
	{FluffsGaveMilk, "i found the lost cat but it"},
	{FluffsPaidBoy, "go to their play area"},
	{FluffsStarted, "i accepted the challenge of finding"},
	{FluffsNotStarted, "i can start this quest by talking to"},
}

// ParseGertrudesCatJournal reads %fluffs from fluffs_journal.rs2's rendered text. ok is false for an empty or unrecognised
// journal, which is unknown, never stage 0.
func ParseGertrudesCatJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range fluffsJournalNeedles {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// fluffsBank is the bank a visit uses: the one nearest the account, else Varrock west's.
func fluffsBank(s Snapshot) tile.Tile {
	if s.TileKnown {
		return NearestBank(s.Tile).Tile
	}
	return VarrockWestBank
}

// fluffsCoinsKit is the coins the brothers and the shops want: the bank's, else the mule's. Stackable, so one slot.
func fluffsCoinsKit() []KitLine {
	return []KitLine{{ID: CoinsID, Name: "Coins", Qty: fluffsCoinsDraw, Min: fluffsCoinsMin, Stackable: true}}
}

// fluffsItemsKit is the bundle the bank and the mule answer for in one pass: the seasoned sardine, else its two parts, and the
// bucket of milk while the cat has not had it. Every line is optional with no source of its own; the module makes what they
// cannot (the leaves from the ground, the sardine from Gerrant, the milk from a cow).
func fluffsItemsKit(stage int) []KitLine {
	kit := []KitLine{
		{ID: FluffsSeasonedSardine, Name: "Seasoned sardine", Qty: 1, Optional: true},
		{ID: FluffsDoogleLeaves, Name: "Doogle leaves", Qty: 1, Optional: true, OnlyIfShort: FluffsSeasonedSardine},
		{ID: FluffsRawSardine, Name: "Raw sardine", Qty: 1, Optional: true, OnlyIfShort: FluffsSeasonedSardine},
	}
	if stage == FluffsPaidBoy {
		kit = append(kit, KitLine{ID: ObjBucketOfMilk, Name: "Bucket of milk", Qty: 1, Optional: true})
	}
	return kit
}

// fluffsKittenKit draws the kitten from the bank. It is optional and untradeable, so the pass reads the bank, draws the kitten
// when it is there and is satisfied when it is not.
func fluffsKittenKit() []KitLine {
	return []KitLine{{ID: FluffsKitten, Name: "Fluffs' kitten", Qty: 1, Optional: true, Untradeable: true}}
}

// fluffsSardineKit is the raw sardine from Gerrant's shelf (after the bank and the mule).
func fluffsSardineKit() []KitLine {
	return []KitLine{{ID: FluffsRawSardine, Name: "Raw sardine", Qty: 1, Sources: []Source{
		{Kind: SourceShop, Shop: FluffsSardineShop, Price: 10, Note: "Gerrant keeps 200, Port Sarim"},
	}}}
}

// fluffsFreeSlots is the room the gathering and the crate search want: an item and a spare.
const fluffsFreeSlots = 3

// DecideGertrudesCat is the whole quest.
func DecideGertrudesCat(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: GertrudeStop}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	room := func(next Step) Step {
		if s.FreeSlots < fluffsFreeSlots {
			return Step{Kind: StepDeposit, KeepIDs: fluffsKeep, Bank: fluffsBank(s), BankSet: true, Reason: "room for the cat's gifts"}
		}
		return next
	}
	switch s.Stage {
	case FluffsNotStarted:
		return Step{Kind: StepTalk, Stop: GertrudeStop}
	case FluffsStarted:
		if s.HeldID(CoinsID) < FluffsBoyFee {
			if s.Failed["provision:"+KitFluffsCoins] {
				return Step{Kind: StepWait, ParkFor: fluffsCoinsParkFor,
					Reason: "100 coins for Shilop and Wilough: the bank and the mule hold none"}
			}
			return Step{Kind: StepProvision, KitName: KitFluffsCoins, Kit: fluffsCoinsKit(), KeepIDs: fluffsKeep,
				Bank: fluffsBank(s), BankSet: true, Reason: "the 100 coins the boys want, and the sardine's and bucket's"}
		}
		return Step{Kind: StepCustom, Custom: CustomFluffsPay, Reason: "buy the lumber yard out of Shilop and Wilough, standing close together"}
	case FluffsPaidBoy, FluffsGaveMilk:
		if st, ok := fluffsGather(s, room); ok {
			return st
		}
		if s.Stage == FluffsPaidBoy {
			return Step{Kind: StepCustom, Custom: CustomFluffsOffer, ItemID: ObjBucketOfMilk, Reason: "give Fluffs the bucket of milk"}
		}
		return Step{Kind: StepCustom, Custom: CustomFluffsOffer, ItemID: FluffsSeasonedSardine, Reason: "give Fluffs the seasoned sardine"}
	case FluffsGaveSardine:
		if !held(FluffsKitten) {
			// The crate gives a kitten only while neither the pack nor the BANK holds one (kittens_mew: inv_total(bank,
			// gertrudekittens) = 0), and the trainer banks what it does not keep between two ladder passes: look there once first,
			// as Romeo & Juliet does for Juliet's message.
			if !s.Ran["provision:"+KitFluffsKitten] && !s.Failed["provision:"+KitFluffsKitten] {
				return room(Step{Kind: StepProvision, KitName: KitFluffsKitten, Kit: fluffsKittenKit(), KeepIDs: fluffsKeep,
					Bank: fluffsBank(s), BankSet: true, Reason: "the kitten, if the trainer banked it (the crates give none while it is in the bank)"})
			}
			return room(Step{Kind: StepCustom, Custom: CustomFluffsCrates, Reason: "search the six crates for the kitten"})
		}
		return Step{Kind: StepCustom, Custom: CustomFluffsOffer, ItemID: FluffsKitten, Reason: "give Fluffs her kitten"}
	case FluffsRescued:
		return Step{Kind: StepCustom, Custom: CustomFluffsReport, Reason: "leave the yard and tell Gertrude"}
	}
	return Step{Kind: StepWait, Reason: "unrecognised Gertrude's Cat stage"}
}

// fluffsCoinsParkFor is how long the quest skips an account whose bank and mule hold no coins at all.
const fluffsCoinsParkFor = 6 * time.Hour

// fluffsGather is the next step toward holding what the cat is next given (the milk at stage 2, the seasoned sardine at 2 and 3),
// in the order the map wants them fetched: the bank and the mule once, the leaves behind Gertrude's house, the sardine in Port
// Sarim, the seasoning, the bucket and the milk in Lumbridge. ok is false when the account holds everything.
func fluffsGather(s Snapshot, room func(Step) Step) (Step, bool) {
	held := func(id int) bool { return s.HeldID(id) > 0 }
	needSard := !held(FluffsSeasonedSardine)
	needMilk := s.Stage == FluffsPaidBoy && !held(ObjBucketOfMilk)
	if !needSard && !needMilk {
		return Step{}, false
	}
	// The cheap rungs once, wherever the account stands: the bank and the mule answer for the lot at no walk to a shelf or a cow.
	if !s.Ran["provision:"+KitFluffsItems] && !s.Failed["provision:"+KitFluffsItems] {
		return room(Step{Kind: StepProvision, KitName: KitFluffsItems, Kit: fluffsItemsKit(s.Stage), KeepIDs: fluffsKeep,
			Bank: fluffsBank(s), BankSet: true, Reason: "the seasoned sardine or its parts and the milk, from the bank or the mule"}), true
	}
	if needSard {
		switch {
		case !held(FluffsDoogleLeaves):
			// The leaves first: they lie behind Gertrude's house, a short walk from the market, and the sardine's shop is the long
			// leg west. Radius on a grab is the seconds to wait for a spawn to come back (five spawns, one pickup each).
			return room(Step{Kind: StepGrabGround, ItemID: FluffsDoogleLeaves, Item: "Doogle leaves", Anchor: DoogleField, Radius: 90}), true
		case !held(FluffsRawSardine):
			if s.Failed["provision:"+KitFluffsSard] {
				return Step{Kind: StepWait, Reason: "no raw sardine from the bank, the mule or Gerrant"}, true
			}
			return room(Step{Kind: StepProvision, KitName: KitFluffsSard, Kit: fluffsSardineKit(), KeepIDs: fluffsKeep,
				Bank: fluffsBank(s), BankSet: true, Reason: "a raw sardine from Gerrant's shelf"}), true
		}
		return Step{Kind: StepCustom, Custom: CustomFluffsSeason, Reason: "rub the doogle leaves over the sardine"}, true
	}
	if needMilk {
		if !held(ObjBucket) {
			if s.Failed["provision:"+KitFluffsBucket] {
				return Step{Kind: StepWait, Reason: "no bucket from the bank, the mule, the farmhouse or the general store"}, true
			}
			return room(Step{Kind: StepProvision, KitName: KitFluffsBucket, Kit: cookBucketKit(), KeepIDs: fluffsKeep,
				Bank: fluffsBank(s), BankSet: true, Reason: "an empty bucket for a Lumbridge dairy cow"}), true
		}
		return Step{Kind: StepCustom, Custom: CustomFluffsMilk, Reason: "milk a cow in the dairy field"}, true
	}
	return Step{}, false
}

// GertrudesCatRecord is the quest's identity (no server gates).
var GertrudesCatRecord = Record{ID: GertrudesCatID, Name: GertrudesCatName, QuestPoints: 1}

// GertrudesCat is the engine module. MaxDeaths 1: nothing in it can kill an account (the cat's claw is 3 and only a click on her
// makes it), so the first death anywhere on its walks parks the quest for a day instead of walking back into whatever did it.
var GertrudesCat = Module{
	Record: GertrudesCatRecord, Bank: VarrockWestBank, BankSet: true, Anchor: GertrudeAnchor,
	ToolIDs:   fluffsKeep,
	ReadStage: ParseGertrudesCatJournal,
	MaxDeaths: 1,
	Decide:    DecideGertrudesCat,
}
