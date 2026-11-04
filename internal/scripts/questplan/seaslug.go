package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Sea Slug Quest (content id `seaslug`, varp %seaslugquest, 1 quest point, Firemaking 30), ported from the TS def
// (defs/seaslug/*.ts) and checked against content 289: quests/quest_seaslug (the constants, quest_seaslug.rs2, seaslug_journal.rs2),
// areas/area_ardougne_east (caroline.rs2, holgart_ardougne.rs2, the Port Khazard shop) and areas/area_fishing_platform (bailey.rs2,
// holgart_island.rs2, holgart_platform.rs2, kennith.rs2, kent.rs2, fisherman.rs2).
//
// THE QUEST. Caroline, east of Ardougne at Witchaven, asks for her husband Kent and her son Kennith, who are out at the fishing
// platform. Holgart's row boat there is full of holes: it wants a swamp paste (a pot of flour and a swamp tar mixed and heated; the
// Port Khazard general store sells it, so nothing is made), and then Holgart takes the account out (a p_telejump to the platform, and
// back, to the island, and between them). On the platform Kennith hides behind the crates upstairs (Shout-across), Holgart takes the
// account on to the island where Kent is stranded (stage 5: the boat does not leave until Kent has been spoken to), and Kent pulls a
// sea slug off the account's neck and drops it on the ground (never touched: its Take costs 3 hitpoints). Bailey, on the platform,
// hands over an unlit torch; the fishermen the slugs possess let nobody up the ladder without a lit torch, so the torch is lit by
// rubbing dry sticks (Firemaking 30, the one server gate): the dry sticks are damp sticks (a ground spawn at 2784,3289) held to the
// sun with a piece of broken glass (a ground spawn at 2765,3289 and one at 2765,3278), and the sticks are not used up. Kennith will
// not walk past the slugs: the loose panel is kicked out (upstairs), Kennith called through it, and the crane rotated from its north
// side to lower him into the boat. Holgart then takes the account back to the shore and Caroline pays: 7,175 Fishing XP, an oyster
// pearl pair and the quest point.
//
// WHAT CAN HURT. Nothing here attacks. The fishermen (npcs 702..705) and the sea slugs (npc 1006, wanderrange 1) have no combat stats,
// no hunt mode and no Attack option: the possession is told in dialogue and in two scripted hits, both of them the account's own
// doing and priced here as a cap, not as a fight. The ladder up (loc 2517) smacks the account for 4 hitpoints each time it is climbed
// from stage 6 on without a lit torch ("The fishermen approach you... and smack you on the head with a fishing rod!"), and the Take on
// a sea slug costs 3. The module never takes a slug, climbs only with the torch lit, and stops climbing when the smack message arrives.
// So the worst a run can do to an account is a few hitpoints, which is why the quest has no food, no odds and no fight, and why
// MaxDeaths is 1.
//
// THE REGIONS. The platform (x 2758..2798, z 3268..3294, two floors) and the island (x 2786..2804, z 3310..3328) are not on the walk
// graph: they are reached only by Holgart's boat, a teleport, so a walk that starts there never arrives. The quest knows the three
// regions by the account's tile and sails between them by talking to Holgart, and both are pockets for every other quest's walks.
// The torch goes out on the crossing from Ardougne to the platform (board_ardougne_to_fishing_platform turns every lit torch into an
// unlit one), and only on that one.

// SeaSlugID is the engine's id and SeaSlugName the quest tab's spelling.
const (
	SeaSlugID   = "seaslug"
	SeaSlugName = "Sea Slug Quest"
)

// The %seaslugquest values (quest_seaslug.constant and quest.constant).
const (
	SlugNotStarted        = 0
	SlugStarted           = 1
	SlugSpokenHolgart     = 2
	SlugBoatRepaired      = 3
	SlugSpokenKennith     = 4
	SlugSailedKent        = 5
	SlugSpokenKent        = 6
	SlugLitTorch          = 7
	SlugKennithNeedEscape = 8
	SlugPanelOpened       = 9
	SlugNeedKennithPath   = 10
	SlugSavedKennith      = 11
	SlugComplete          = 12
)

// SlugFiremaking is the Firemaking level the dry sticks need (opheld1,dry_sticks: stat(firemaking) < 30).
const SlugFiremaking = 30

// Ids from content/pack (checked against the production config by TestSeaSlugIDsMatchTheProductionConfig in questmods).
const (
	NpcBailey         = 695 // [bailey], on the platform's lower deck
	NpcCaroline       = 696 // [caroline], at Witchaven
	NpcKennith        = 697 // [kennith], upstairs behind the crates
	NpcHolgartIsland  = 698 // [holgartsunkboat]
	NpcHolgartPlat    = 699 // [holgartplatform]
	NpcHolgartLand    = 700 // [holgartland]
	NpcKent           = 701 // [kent], on the island
	NpcPlatformSlug   = 1006
	ObjTorchLit       = 594
	ObjTorchUnlit     = 596
	ObjDampSticks     = 1467
	ObjDrySticks      = 1468
	ObjBrokenGlass    = 1469
	ObjSwampPaste     = 1941
	ObjSeaSlug        = 1466 // [seasluginv]: never taken (Take costs 3 hitpoints)
	LocSlugLadderUp   = 2517 // [slugladder], op1 Climb-up: the platform's ladder, refused without a lit torch from stage 6
	LocSlugLadderDown = 1746 // the upper deck's ladder down (a plain ladder)
	LocSlugPanel      = 2518 // [loosepanel], op1 Kick
	LocSlugCrates     = 2519 // [kennithwall], op1 Shout-across (two of them, 2763,3287 and 2764,3287)
	LocSlugCrane      = 2520 // [fishingcrane], op1 Rotate
)

// Tiles from content maps and the loc index.
var (
	// SlugCarolineAnchor and SlugHolgartLandAnchor are the two spawns at Witchaven (m42_51), SlugShore where the boat lands on the way back
	// (^seaslug_shore_coord, 0_42_51_34_41).
	SlugCarolineAnchor    = tile.Tile{X: 2716, Z: 3302}
	SlugHolgartLandAnchor = tile.Tile{X: 2720, Z: 3306}
	SlugShore             = tile.Tile{X: 2722, Z: 3305}
	// SlugPlatformArrive is where the boat sets the account down on the platform (^seaslug_platform_coord, 0_43_51_30_9), SlugHolgartPlatform
	// Holgart's spawn there and SlugBaileyAnchor Bailey's.
	SlugPlatformArrive  = tile.Tile{X: 2782, Z: 3273}
	SlugHolgartPlatform = tile.Tile{X: 2782, Z: 3276}
	SlugBaileyAnchor    = tile.Tile{X: 2763, Z: 3276}
	// SlugLadderFoot and SlugLadderTop are the tiles north of the ladder (loc 2517 at 2784,3286 on the lower deck, loc 1746 above it): the ladder's
	// own tile is not walkable, and the engine accepts the climb from its west, east and north sides (LocStands).
	SlugLadderFoot       = tile.Tile{X: 2784, Z: 3287}
	SlugLadderTop        = tile.Tile{X: 2784, Z: 3287, Level: 1}
	SlugDampSticksSpawn  = tile.Tile{X: 2784, Z: 3289}
	SlugBrokenGlassSouth = tile.Tile{X: 2765, Z: 3278}
	SlugBrokenGlassNorth = tile.Tile{X: 2765, Z: 3289}
	// SlugCratesStand, SlugPanelStand and SlugCraneStand are the upper deck's stands: south of Kennith's crates (his room is north of them),
	// east of the loose panel's wall, and north of the crane (it wants the account at least 3 tiles north of its south edge,
	// fishingcrane: coordz(coord) < coordz(movecoord(loc_coord, 0, 0, 3))).
	SlugCratesStand = tile.Tile{X: 2764, Z: 3286, Level: 1}
	SlugPanelStand  = tile.Tile{X: 2766, Z: 3288, Level: 1}
	SlugCraneStand  = tile.Tile{X: 2769, Z: 3290, Level: 1}
	// SlugIslandArrive is where the boat sets the account down on the island (^seaslug_island_coord, 0_43_51_48_56), SlugKentAnchor Kent's
	// spawn and SlugHolgartIsland the sunk boat's Holgart.
	SlugIslandArrive   = tile.Tile{X: 2800, Z: 3320}
	SlugKentAnchor     = tile.Tile{X: 2793, Z: 3321}
	SlugHolgartIsland  = tile.Tile{X: 2799, Z: 3320}
	SlugKennithAnchor  = tile.Tile{X: 2764, Z: 3288, Level: 1}
	slugBrokenGlassAll = []tile.Tile{SlugBrokenGlassSouth, SlugBrokenGlassNorth}
)

// SlugRegion is where the account stands relative to Holgart's boat.
type SlugRegion string

const (
	// SlugMainland is anywhere the walker reaches.
	SlugMainland SlugRegion = "mainland"
	// SlugPlatform is the fishing platform (either floor).
	SlugPlatform SlugRegion = "platform"
	// SlugIsland is Kent's island.
	SlugIsland SlugRegion = "island"
)

// OnSlugPlatform reports a tile of the fishing platform, upper floor included.
func OnSlugPlatform(t tile.Tile) bool {
	return t.Level <= 1 && t.X >= 2758 && t.X <= 2798 && t.Z >= 3268 && t.Z <= 3294
}

// OnSlugIsland reports a tile of Kent's island.
func OnSlugIsland(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2786 && t.X <= 2804 && t.Z >= 3310 && t.Z <= 3328
}

// SlugRegionOf is the region a tile is in.
func SlugRegionOf(t tile.Tile) SlugRegion {
	switch {
	case OnSlugPlatform(t):
		return SlugPlatform
	case OnSlugIsland(t):
		return SlugIsland
	}
	return SlugMainland
}

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomSlugToPlatform = "slug-to-platform"
	CustomSlugToIsland   = "slug-to-island"
	CustomSlugToMainland = "slug-to-mainland"
	CustomSlugDown       = "slug-down"
	CustomSlugKennith    = "slug-kennith"
	CustomSlugKent       = "slug-kent"
	CustomSlugRub        = "slug-rub"
	CustomSlugPanel      = "slug-panel"
	CustomSlugCrane      = "slug-crane"

	KitSlugPaste = "slug-paste"
)

// SlugPasteGP is the swamp paste's base price in the Port Khazard store (swamppaste cost=30): a floor for budgeting, the real price
// rises as the shelf (500 of them) empties.
const SlugPasteGP = 30

// SlugParkFor is how long the quest skips an account that cannot go on and has no way to mend it by trying again (a lost torch at
// stage 10, a shop with no paste).
const SlugParkFor = 3 * time.Hour

// slugKeep is what every bank visit of the quest leaves in the pack: the paste, the torch and its makings, and the coins for a shop.
var slugKeep = []int{ObjSwampPaste, ObjTorchLit, ObjTorchUnlit, ObjDampSticks, ObjDrySticks, ObjBrokenGlass, CoinsID}

// SlugKeepIDs returns the keep list (a copy).
func SlugKeepIDs() []int { return append([]int(nil), slugKeep...) }

// The stops. The talks with no menu carry a placeholder preference (a talk step must name one).
var (
	// SlugCarolineStop starts the quest: "Hello there." then "I suppose so, how do I get there?" / "I'm sorry, I'm too busy."; the hand-in
	// at the end has no menu.
	SlugCarolineStop = NpcStop{Npc: "Caroline", ID: NpcCaroline, Anchor: SlugCarolineAnchor, Leash: 6,
		Prefer: []string{"I suppose so, how do I get there?"}}
	// SlugHolgartLandStop is every talk with Holgart on the shore. At stage 1 and 5 it has no menu, at stage 2 with the paste it ends in
	// "I'll come back later." / "Okay, lets do it." and from stage 6 (or at 3 and 4) in "Will you take me there?" / "I'm keeping away
	// from there." (and after the quest in "Okay, let's do it.").
	SlugHolgartLandStop = NpcStop{Npc: "Holgart", ID: NpcHolgartLand, Anchor: SlugHolgartLandAnchor, Leash: 6,
		Prefer: []string{"Okay, lets do it.", "Okay, let's do it.", "Will you take me there?"}}
	// SlugBaileyStop is Bailey's torch and his lines about it, none with a menu.
	SlugBaileyStop = NpcStop{Npc: "Bailey", ID: NpcBailey, Anchor: SlugBaileyAnchor, Leash: 8, Prefer: []string{"Hello."}}
	// SlugHolgartPlatformStop: at stage 4 no menu (the boat goes to the island), at 11 none (the boat goes home), otherwise "No, I'm going to
	// stay a while." / "Okay, let's go back." (the way home).
	SlugHolgartPlatformStop = NpcStop{Npc: "Holgart", ID: NpcHolgartPlat, Anchor: SlugHolgartPlatform, Leash: 6,
		Prefer: []string{"Okay, let's go back."}}
	// SlugHolgartIslandStop: before Kent is spoken to he says where the account is and does not sail; after it "We'd better get back to the
	// platform" and the boat goes, no menu either way.
	SlugHolgartIslandStop = NpcStop{Npc: "Holgart", ID: NpcHolgartIsland, Anchor: SlugHolgartIsland, Leash: 8, Prefer: []string{"Hello."}}
	// SlugKentStop: his talk at stage 5 has no menu.
	SlugKentStop = NpcStop{Npc: "Kent", ID: NpcKent, Anchor: SlugKentAnchor, Leash: 8, Prefer: []string{"Hello."}}
)

// slugMarkers are the journal's markers, newest stage first (seaslug_journal.rs2 keeps every earlier paragraph on the page). Two pairs
// of stages read alike and the newer is not told apart: the journal prints stage 4 and 5 with one text and 8 and 9 with one text, so the
// older of each pair is returned (SlugSpokenKennith, SlugKennithNeedEscape) and the decision reads the account's tile and the world.
var slugMarkers = []struct {
	stage int
	text  string
}{
	{SlugComplete, "quest complete!"},
	{SlugSavedKennith, "i've used the crane to lower kennith into the boat"},
	{SlugNeedKennithPath, "kennith can't get downstairs without some help"},
	{SlugKennithNeedEscape, "i've created an opening to let kenneth escape"},
	{SlugLitTorch, "kennith won't go near the sea slugs"},
	{SlugSpokenKent, "i've found kent on a small island"},
	{SlugSpokenKennith, "i've found kennith"},
	{SlugBoatRepaired, "i gave holgart the swamp paste"},
	{SlugSpokenHolgart, "his boat is broken"},
	{SlugSpokenHolgart, "i need to take the swamp paste to holgart"},
	{SlugStarted, "i need to talk to holgart"},
	{SlugNotStarted, "i can start this quest by speaking to caroline"},
}

// ParseSeaSlugJournal reads %seaslugquest from the rendered journal (the older stage of an ambiguous pair). ok is false for an empty or
// unrecognised page, which is unknown, never stage 0.
func ParseSeaSlugJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	for _, m := range slugMarkers {
		if strings.Contains(text, m.text) {
			return m.stage, true
		}
	}
	return 0, false
}

// SlugPastTorch reports that the account is past the quest's crowded steps (the damp sticks, the broken glass and Bailey's torch are
// one spawn each): from the lit torch on it only talks, kicks and rotates (Module.ExclusiveDone).
func SlugPastTorch(s Snapshot) bool {
	return s.Journal == StatusComplete || (s.StageKnown && s.Stage >= SlugLitTorch)
}

// slugPasteKit is the swamp paste: the bank, the mule, and Port Khazard's general store (500 of them at 30 gp).
func slugPasteKit() []KitLine {
	return []KitLine{{ID: ObjSwampPaste, Name: "Swamp paste", Qty: 1, Stackable: true, Sources: []Source{
		{Kind: SourceShop, Shop: KhazardGeneralStore, Price: SlugPasteGP, Note: "Port Khazard's general store keeps 500"}}}}
}

// slugTalk is a talk step.
func slugTalk(stop NpcStop, reason string) Step {
	return Step{Kind: StepTalk, Stop: stop, Reason: reason}
}

// slugCustom is a custom step.
func slugCustom(name, reason string) Step {
	return Step{Kind: StepCustom, Custom: name, Reason: reason}
}

// slugTo is the step that takes the account to a region, "" when it is there.
func slugTo(have, want SlugRegion) (Step, bool) {
	if have == want {
		return Step{}, false
	}
	switch want {
	case SlugPlatform:
		return slugCustom(CustomSlugToPlatform, "Holgart's boat to the fishing platform"), true
	case SlugIsland:
		return slugCustom(CustomSlugToIsland, "Holgart's boat to Kent's island"), true
	}
	return slugCustom(CustomSlugToMainland, "Holgart's boat back to the shore"), true
}

// slugGlassSpawn is the nearer of the two broken glass spawns to t.
func slugGlassSpawn(t tile.Tile, known bool) tile.Tile {
	if !known {
		return SlugBrokenGlassNorth
	}
	best := slugBrokenGlassAll[0]
	for _, g := range slugBrokenGlassAll[1:] {
		if tile.Chebyshev(t, g) < tile.Chebyshev(t, best) {
			best = g
		}
	}
	return best
}

// slugTorchStep lights the torch, a step at a time: on the platform's lower deck, Bailey's torch, a damp stick, a piece of broken
// glass, the glass held to the stick, and the stick rubbed (Firemaking 30).
func slugTorchStep(s Snapshot) Step {
	region := SlugRegionOf(s.Tile)
	if st, go2 := slugTo(region, SlugPlatform); go2 {
		return st
	}
	if s.Tile.Level != 0 {
		return slugCustom(CustomSlugDown, "down to the platform's lower deck for the torch")
	}
	if s.Level("firemaking") < SlugFiremaking {
		return Step{Kind: StepWait, ParkFor: SlugParkFor, Reason: fmt.Sprintf("Firemaking %d: the dry sticks need %d to catch", s.Level("firemaking"), SlugFiremaking)}
	}
	if s.HeldID(ObjTorchLit)+s.HeldID(ObjTorchUnlit) == 0 {
		if s.Stage >= SlugNeedKennithPath {
			// Bailey has no line for a lost torch past stage 9 (bailey.rs2 hands one over from stage 6 to 9 only).
			return Step{Kind: StepWait, ParkFor: SlugParkFor, Reason: "no torch, and Bailey hands none over once the panel is open"}
		}
		return slugTalk(SlugBaileyStop, "Bailey's torch")
	}
	if s.HeldID(ObjDrySticks) > 0 {
		return slugCustom(CustomSlugRub, "rub the dry sticks to light the torch")
	}
	if s.HeldID(ObjDampSticks) == 0 {
		return Step{Kind: StepGrabGround, Item: "Damp sticks", ItemID: ObjDampSticks, Anchor: SlugDampSticksSpawn, Radius: 90, Reason: "damp sticks from the platform's deck"}
	}
	if s.HeldID(ObjBrokenGlass) == 0 {
		return Step{Kind: StepGrabGround, Item: "Broken glass", ItemID: ObjBrokenGlass, Anchor: slugGlassSpawn(s.Tile, s.TileKnown), Radius: 90,
			Reason: "broken glass from the platform's deck"}
	}
	return Step{Kind: StepUseOn, Item: "Broken glass", ItemID: ObjBrokenGlass, TargetKind: TargetItem, Target: "Damp sticks", TargetID: ObjDampSticks,
		Reason: "the glass held to the damp sticks dries them"}
}

// DecideSeaSlug is the whole quest, read from the journal's stage, the account's region and the pack.
func DecideSeaSlug(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		if st, away := slugTo(SlugRegionOf(s.Tile), SlugMainland); away {
			return st
		}
		return slugTalk(SlugCarolineStop, "start the Sea Slug Quest")
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	if !s.TileKnown {
		return Step{Kind: StepWait, Reason: "the account's position has not been read"}
	}
	region := SlugRegionOf(s.Tile)
	litTorch := s.HeldID(ObjTorchLit) > 0
	switch {
	case s.Stage == SlugNotStarted || s.Stage >= SlugComplete:
		return Step{Kind: StepWait, Reason: "the journal and the quest tab disagree about the Sea Slug Quest"}
	case s.Stage == SlugStarted:
		if st, away := slugTo(region, SlugMainland); away {
			return st
		}
		return slugTalk(SlugHolgartLandStop, "Holgart wants a swamp paste for his boat")
	case s.Stage == SlugSpokenHolgart:
		if st, away := slugTo(region, SlugMainland); away {
			return st
		}
		if s.HeldID(ObjSwampPaste) > 0 {
			return slugTalk(SlugHolgartLandStop, "the swamp paste to Holgart, and out to the platform")
		}
		if s.Blocked["provision:"+KitSlugPaste] {
			return Step{Kind: StepWait, ParkFor: SlugParkFor, Reason: "no swamp paste from the bank, the mule or Port Khazard's general store"}
		}
		return Step{Kind: StepProvision, KitName: KitSlugPaste, Kit: slugPasteKit(), KeepIDs: slugKeep, Bank: ArdougneSouthBank, BankSet: true,
			Reason: "a swamp paste for Holgart's boat (Port Khazard's store sells it)"}
	case s.Stage == SlugBoatRepaired:
		switch region {
		case SlugMainland:
			return slugTalk(SlugHolgartLandStop, "out to the platform")
		case SlugIsland:
			return slugCustom(CustomSlugToPlatform, "back to the platform from the island")
		}
		return slugCustom(CustomSlugKennith, "shout across the crates to Kennith")
	case s.Stage == SlugSpokenKennith || s.Stage == SlugSailedKent:
		// Stage 4 and 5 read alike: the platform's Holgart takes the account to the island at 4, the island is where 5 is, the shore's
		// Holgart takes it out to either.
		switch region {
		case SlugIsland:
			return slugCustom(CustomSlugKent, "find Kent on the island")
		case SlugPlatform:
			return slugCustom(CustomSlugToIsland, "Holgart's boat to Kent's island")
		}
		return slugTalk(SlugHolgartLandStop, "out to the platform or the island")
	case s.Stage == SlugSpokenKent || (s.Stage >= SlugLitTorch && s.Stage <= SlugNeedKennithPath && !litTorch):
		return slugTorchStep(s)
	case s.Stage == SlugLitTorch:
		if st, go2 := slugTo(region, SlugPlatform); go2 {
			return st
		}
		return slugCustom(CustomSlugKennith, "show Kennith the lit torch")
	case s.Stage == SlugKennithNeedEscape || s.Stage == SlugPanelOpened:
		if st, go2 := slugTo(region, SlugPlatform); go2 {
			return st
		}
		return slugCustom(CustomSlugPanel, "kick the panel open and call Kennith through")
	case s.Stage == SlugNeedKennithPath:
		if st, go2 := slugTo(region, SlugPlatform); go2 {
			return st
		}
		return slugCustom(CustomSlugCrane, "swing the crane over to Kennith")
	case s.Stage == SlugSavedKennith:
		if st, away := slugTo(region, SlugMainland); away {
			return st
		}
		return slugTalk(SlugCarolineStop, "Caroline's thanks")
	}
	return Step{Kind: StepWait, Reason: fmt.Sprintf("unrecognised Sea Slug Quest stage %d", s.Stage)}
}

// SeaSlugRecord is the quest's identity: Firemaking 30 is the server's own gate (the sticks never catch below it), so an account
// below it is not started on a quest it cannot finish.
var SeaSlugRecord = Record{ID: SeaSlugID, Name: SeaSlugName, QuestPoints: 1,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "firemaking", Level: SlugFiremaking}}}}

// SeaSlug is the engine module. It never fights (NoRetaliate) and nothing in it can kill an account, so the first death anywhere on its
// walks parks it for a day; the slot is given up once the torch is lit.
var SeaSlug = Module{
	Record: SeaSlugRecord, Bank: ArdougneSouthBank, BankSet: true, Anchor: SlugCarolineAnchor,
	ToolIDs:       slugKeep,
	ReadStage:     ParseSeaSlugJournal,
	NoRetaliate:   true,
	MaxDeaths:     1,
	ExclusiveDone: SlugPastTorch,
	Decide:        DecideSeaSlug,
}
