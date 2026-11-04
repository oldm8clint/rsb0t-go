package waterfallplan

import (
	"fmt"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// Leg is one unit of work the runner performs. Every leg is safe to repeat: the runner re-reads
// the world and asks Decide again after each one, so a leg that half-worked is simply chosen
// again or superseded.
type Leg string

const (
	LegDone Leg = "done"
	LegWait Leg = "wait"

	LegKitTomb    Leg = "bank for the Tree Gnome Village and tomb trip"
	LegKitDungeon Leg = "bank for the dungeon trip"

	LegAlmera   Leg = "talk to Almera"
	LegRaft     Leg = "board the log raft"
	LegHudon    Leg = "talk to Hudon"
	LegSwimOff  Leg = "swim off Hudon's mound"
	LegBookcase Leg = "search the tourist centre bookcase"
	LegReadBook Leg = "read the Book on Baxtorian"

	LegTGVDown    Leg = "climb down into the Tree Gnome Village dungeon"
	LegGolrieKey  Leg = "search the crate for Golrie's key"
	LegGolrieGate Leg = "use the key on Golrie's gate"
	LegGolrie     Leg = "talk to Golrie for the pebble"
	LegTGVUp      Leg = "climb out of the Tree Gnome Village dungeon"

	LegHeal      Leg = "eat before Glarial's tomb"
	LegTombstone Leg = "use the pebble on Glarial's tombstone"
	LegChest     Leg = "take the amulet from the tomb chest"
	LegCoffin    Leg = "take the urn from the coffin"
	LegTombUp    Leg = "climb out of Glarial's tomb"

	LegRopeRock  Leg = "rope across to the rock"
	LegRopeTree  Leg = "rope down to the ledge"
	LegBarrel    Leg = "barrel off the ledge"
	LegLedgeDoor Leg = "open the ledge door"

	LegBaxKey      Leg = "search the crate for the dungeon key"
	LegLaneIn      Leg = "cross the fire-giant lane and unlock the south door"
	LegPuzzleDoor  Leg = "unlock the pillar-room door"
	LegPillars     Leg = "place the runes on the pillars"
	LegStatue      Leg = "put the amulet on the statue"
	LegFreeSlots   Leg = "make room for the reward"
	LegMakeRoom    Leg = "eat or drop food for a free slot"
	LegChalice     Leg = "pour the ashes into the chalice"
	LegWashout     Leg = "take the chalice's washout"
	LegExitRaised  Leg = "leave the raised room by its door"
	LegExitPuzzle  Leg = "leave the pillar room"
	LegExitSouth   Leg = "unlock the south door from the north"
	LegLaneOut     Leg = "cross the fire-giant lane back to the spider room"
	LegEntryDoor   Leg = "open the entry door back to the ledge"
	LegStrandedBnk Leg = "stranded on the tree bank without a rope"
)

// Decision is Decide's answer: the leg, and a one-line reason for the log. Item is the food a
// LegHeal eats (an exact object id).
type Decision struct {
	Leg  Leg
	Why  string
	Item int
}

func (d Decision) String() string {
	if d.Why == "" {
		return string(d.Leg)
	}
	return string(d.Leg) + " (" + d.Why + ")"
}

// Snapshot is everything Decide reads. The runner builds it from the live session; nothing in it
// is remembered by the plan.
type Snapshot struct {
	// Status is the quest tab's colour.
	Status questplan.Status
	// Stage is the journal's reading, when StageKnown.
	Stage      Stage
	StageKnown bool
	// Tile is where the account stands.
	Tile      tile.Tile
	TileKnown bool
	// Inv and Worn count the pack and the worn slots by exact object id.
	Inv, Worn map[int]int
	// Bank is the last bank read by exact object id, when BankKnown. An unread bank is not an
	// empty one.
	Bank      map[int]int
	BankKnown bool
	// IsFood reports whether an object id is food (the runner resolves "has an Eat op").
	IsFood func(id int) bool
	// FreeSlots is the pack's free slot count.
	FreeSlots int
	// HP and MaxHP are the current and maximum hitpoints.
	HP, MaxHP int
	// Ledger is this visit's pillar acknowledgements. The engine path leaves it empty (the
	// pillars executor keeps its own, re-checked each visit); PillarsFailed is then its only
	// signal, set when that executor last reported failure (a rune kind ran out).
	Ledger        PillarLedger
	PillarsFailed bool
	// Failed is the engine's failed-step memory (questplan.Snapshot.Failed): which food top-up
	// source has already failed.
	Failed map[string]bool
	// KitShort is set by the runner when the last kit pass for Trip could not fill it; Decide
	// then waits instead of setting off short. KitWhy says what was short.
	KitShort     bool
	KitShortTrip Trip
	KitWhy       string
	Settings     Settings
	// Ran names the engine steps that succeeded this run (questplan.Snapshot.Ran): the food
	// top-up follows a kit pass that has already run.
	Ran map[string]bool
}

func (s Snapshot) held(id int) int  { return s.Inv[id] }
func (s Snapshot) worn(id int) bool { return s.Worn[id] > 0 }
func (s Snapshot) has(id int) bool  { return s.Inv[id] > 0 || s.Worn[id] > 0 }

// owned counts an item held, worn or banked (the bank only when it has been read).
func (s Snapshot) owned(id int) bool {
	if s.has(id) {
		return true
	}
	return s.BankKnown && s.Bank[id] > 0
}

func (s Snapshot) food() int { return FoodCount(s.Inv, s.IsFood) }

// A step that picks something up (the book, Golrie's key, the amulet, the urn, the dungeon key)
// needs a free slot, and a pack full of food makes one by eating or dropping a bite (LegMakeRoom)
// where it stands. Only a pack with no food and no room leaves to make room at a bank. Walking out
// instead was a livelock: on 2026-09-26 the live account "acct" entered the dungeon with 0
// free slots, the dungeon's "no free slot for the key" escape opened the entry door back to the
// ledge, the ledge's "dungeon ready" opened it again, and it flipped between the two 158 times.
func (s Snapshot) canMakeRoom() bool { return s.food() > 0 }

// roomForKey is whether the dungeon trip can take the Baxtorian key: a free slot, the key
// already, or food to make a slot with.
func (s Snapshot) roomForKey() bool {
	return s.FreeSlots >= 1 || s.has(ItemBaxKey) || s.canMakeRoom()
}

// relicsOwned reports whether the amulet and the full urn are both owned somewhere reachable
// from a bank visit. At stage 8 the statue has consumed the amulet, but re-entering the
// dungeon after leaving needs a new one (the ledge door floods without it), so the rule is the
// same at every stage from 4.
func (s Snapshot) relicsOwned() bool { return s.owned(ItemAmulet) && s.owned(ItemFullUrn) }

func wait(why string) Decision                { return Decision{Leg: LegWait, Why: why} }
func leg(l Leg, why string) Decision          { return Decision{Leg: l, Why: why} }
func legf(l Leg, f string, a ...any) Decision { return Decision{Leg: l, Why: fmt.Sprintf(f, a...)} }

// Decide picks the next leg. It is restartable from any stage in any area: the stage comes from
// the journal, the place from the tile, and progress from items by exact id.
func Decide(s Snapshot) Decision {
	area := AreaOf(s.Tile, s.TileKnown)
	if area == AreaUnknown {
		return wait("no position yet")
	}
	if s.Status == questplan.StatusUnknown {
		return wait("the quest tab has not been coloured yet")
	}
	if s.Status == questplan.StatusComplete || (s.StageKnown && s.Stage == StageComplete) {
		return afterComplete(s, area)
	}
	stage := StageNotStarted
	if s.Status == questplan.StatusInProgress {
		if !s.StageKnown {
			return wait("the journal stage has not been read")
		}
		if !s.Stage.Valid() {
			return wait(fmt.Sprintf("the journal reads an unknown stage %d", int(s.Stage)))
		}
		stage = s.Stage
		// A stale journal can lag the world by one leg (it is read between legs). Where the
		// account stands proves a floor under the stage, and acting on the floor is always safe.
		stage = floorStage(stage, area)
	}

	switch {
	case area.InWaterfallDungeon():
		return inDungeon(s, stage, area)
	case area == AreaTomb:
		return inTomb(s)
	case area == AreaTGV:
		return inTGV(s, stage)
	case area.OnFalls():
		return onFalls(s, stage, area)
	}
	return onMainland(s, stage)
}

// floorStage raises a lagging journal reading to what the account's position proves.
func floorStage(stage Stage, area Area) Stage {
	switch {
	case area == AreaRaisedRoom && stage < StageRaisedFloor:
		// The only ways in are the statue (sets 8) and the key at 8+.
		return StageRaisedFloor
	case area.InWaterfallDungeon() && stage < StageEnteredWaterfall:
		// The ledge door sets 5 on the way in.
		return StageEnteredWaterfall
	case area == AreaTomb && stage < StageEnteredTomb:
		return StageEnteredTomb
	}
	return stage
}

// afterComplete walks the account out of wherever it is. There is no washout after completion:
// the chalice's op1 answers "The chalice only contains some old ashes" at stage 10.
func afterComplete(s Snapshot, area Area) Decision {
	switch area {
	case AreaMainland:
		return leg(LegDone, "the quest tab is green")
	case AreaRaisedRoom:
		return leg(LegExitRaised, "complete; the chalice no longer washes anyone out")
	}
	return escape(s, area, "complete")
}

// escape is the way out of each one-way place, used whenever the current stage has no business
// there.
func escape(s Snapshot, area Area, why string) Decision {
	switch area {
	case AreaMound:
		return leg(LegSwimOff, why)
	case AreaTreeBank:
		if s.has(ItemRope) {
			return leg(LegRopeTree, why+"; the ledge's barrel is the way off")
		}
		return leg(LegStrandedBnk, why)
	case AreaLedge:
		return leg(LegBarrel, why)
	case AreaTGV:
		return leg(LegTGVUp, why)
	case AreaTomb:
		return leg(LegTombUp, why)
	case AreaRaisedRoom:
		if s.Status != questplan.StatusComplete {
			return leg(LegWashout, why+"; before completion the chalice washes the account out unhurt")
		}
		return leg(LegExitRaised, why)
	case AreaPuzzleRoom:
		return leg(LegExitPuzzle, why)
	case AreaCorridor:
		return leg(LegExitSouth, why)
	case AreaDungeonSouth:
		if InLane(s.Tile) {
			return leg(LegLaneOut, why)
		}
		return leg(LegEntryDoor, why)
	case AreaDungeonOther:
		return leg(LegEntryDoor, why)
	}
	return leg(LegDone, why)
}

func onMainland(s Snapshot, stage Stage) Decision {
	switch stage {
	case StageNotStarted:
		if ok, why := TombLoadout(stage, s.Settings).Ready(s.Inv, s.Worn, s.IsFood); !ok && !s.shortFor(TripTomb) {
			return leg(LegKitTomb, why)
		}
		return leg(LegAlmera, "")
	case StageStarted:
		return leg(LegRaft, "stage 1: the raft to Hudon")
	case StageSpokenToHudon:
		if s.held(ItemBook) > 0 {
			return leg(LegReadBook, "")
		}
		if s.FreeSlots < 1 {
			if s.canMakeRoom() {
				return leg(LegMakeRoom, "no free slot for the book")
			}
			return leg(LegKitTomb, "no free slot for the book")
		}
		return leg(LegBookcase, "")
	case StageReadBook:
		return tombTrip(s, stage)
	}
	// Stage 4 and on: the relics first, then the dungeon kit, then the river.
	if !s.relicsOwned() {
		return tombTrip(s, stage)
	}
	dl := DungeonLoadout(stage, s.Settings)
	if ok, why := dl.Ready(s.Inv, s.Worn, s.IsFood); !ok {
		if s.shortFor(TripDungeon) {
			return wait("the dungeon kit is short: " + s.KitWhy)
		}
		return leg(LegKitDungeon, why)
	}
	if s.worn(ItemAmulet) {
		// The statue takes the amulet from the PACK; a worn one cannot be used on it.
		return leg(LegKitDungeon, "the amulet must travel in the pack, not worn")
	}
	if !s.roomForKey() {
		// No room and no food: the kit's bank visit deposits what the trip does not need.
		return leg(LegKitDungeon, "no free slot for the dungeon key")
	}
	return leg(LegRaft, "the raft is the way to the falls")
}

// tombTrip covers stage 3 on the mainland and relic recovery at any later stage: a tomb-safe
// pack, the pebble from Golrie when it is not owned, then the tombstone.
func tombTrip(s Snapshot, stage Stage) Decision {
	tl := TombLoadout(stage, s.Settings)
	if ok, why := tl.Ready(s.Inv, s.Worn, s.IsFood); !ok {
		if s.shortFor(TripTomb) {
			return wait("the tomb kit is short: " + s.KitWhy)
		}
		return leg(LegKitTomb, why)
	}
	if s.held(ItemPebble) > 0 {
		if id := s.tombEntryBite(); id != 0 {
			return Decision{Leg: LegHeal, Item: id,
				Why: fmt.Sprintf("%d of %d hitpoints: Glarial's tomb is a cave of zombies, skeletons and moss giants, and the account goes in stripped", s.HP, s.MaxHP)}
		}
		return leg(LegTombstone, "")
	}
	if s.BankKnown && s.Bank[ItemPebble] > 0 {
		return leg(LegKitTomb, "the pebble is in the bank")
	}
	return leg(LegTGVDown, "Golrie has Glarial's pebble")
}

// TombEntryHealthPct is the share of its hitpoints an account must have before it goes into
// Glarial's tomb, and eats up to when it has less and a bite fits.
//
// Live 5 Oct 2026, acct (31 hitpoints, combat 34): it used the pebble at 21/31 after a six minute
// walk in from the Tree Gnome Village, was hit from 15:13:57, ate five breads (20 hitpoints) and
// died at 15:15:09 walking back to the ladder with none left: it took about 40 hitpoints in 79
// seconds, 0.5 a second. Had it gone in at 31 it would have had 51 against that 41. The tomb (content
// map m39_153.jm2) holds 3 moss giants (level 42, maximum 6), 6 skeletons and 6 zombies in the
// cave between the landing, the chest and the coffin, the account goes in with nothing worn
// (waterfall_can_enter_glarials_tomb), and the module never fights back. Cant Stop Me used the
// pebble at 16/32 the same day and died on the walk in.
const TombEntryHealthPct = 90

// biteHeal is the hitpoints one bite of each food restores, content's figures (a cake is three
// bites of 4; FoodHeal counts it whole).
func biteHeal(id int) int {
	switch id {
	case 1891, 1893, 1895:
		return 4
	}
	if h := FoodHeal[id]; h > 0 {
		return h
	}
	return FoodUnit
}

// tombEntryBite is the food to eat now so the account goes into the tomb healthy: below
// TombEntryHealthPct of its hitpoints, the biggest bite that does not overheal (a shark at 27/31
// would waste 16). 0 when it is healthy enough, holds no food, or no bite fits what is missing.
func (s Snapshot) tombEntryBite() int {
	if s.MaxHP <= 0 || s.HP*100 >= s.MaxHP*TombEntryHealthPct || s.IsFood == nil {
		return 0
	}
	missing := s.MaxHP - s.HP
	best, bestHeal := 0, 0
	for _, id := range sortedIDs(s.Inv) {
		if s.Inv[id] <= 0 || !s.IsFood(id) {
			continue
		}
		if h := biteHeal(id); h <= missing && h > bestHeal {
			best, bestHeal = id, h
		}
	}
	return best
}

func (s Snapshot) shortFor(t Trip) bool { return s.KitShort && s.KitShortTrip == t }

func inTGV(s Snapshot, stage Stage) Decision {
	if stage < StageReadBook {
		return escape(s, AreaTGV, "the dungeon has nothing for stage "+fmt.Sprint(int(stage)))
	}
	if s.held(ItemPebble) > 0 {
		return leg(LegTGVUp, "the pebble is held")
	}
	if stage >= StageEnteredTomb && s.relicsOwned() {
		return leg(LegTGVUp, "the relics are owned")
	}
	if InGolrieCell(s.Tile) {
		return leg(LegGolrie, "")
	}
	if s.held(ItemGolrieKey) > 0 {
		return leg(LegGolrieGate, "")
	}
	if s.FreeSlots < 1 {
		if s.canMakeRoom() {
			return leg(LegMakeRoom, "no free slot for Golrie's key")
		}
		return escape(s, AreaTGV, "no free slot for the key")
	}
	return leg(LegGolrieKey, "")
}

// InGolrieCell reports whether t is inside Golrie's locked cell, north of his gate (2515,9575).
func InGolrieCell(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2510 && t.X <= 2521 && t.Z >= 9576 && t.Z <= 9590
}

func inTomb(s Snapshot) Decision {
	if !s.has(ItemAmulet) {
		if s.FreeSlots < 1 {
			if s.canMakeRoom() {
				return leg(LegMakeRoom, "no free slot for the amulet")
			}
			return leg(LegTombUp, "no free slot for the amulet")
		}
		return leg(LegChest, "")
	}
	if s.held(ItemFullUrn) == 0 {
		if s.FreeSlots < 1 {
			if s.canMakeRoom() {
				return leg(LegMakeRoom, "no free slot for the urn")
			}
			return leg(LegTombUp, "no free slot for the urn")
		}
		return leg(LegCoffin, "")
	}
	return leg(LegTombUp, "the amulet and the urn are held")
}

func onFalls(s Snapshot, stage Stage, area Area) Decision {
	switch area {
	case AreaMound:
		if stage == StageStarted {
			return leg(LegHudon, "")
		}
		if stage >= StageEnteredTomb && s.dungeonReady(stage) {
			return leg(LegRopeRock, "")
		}
		return leg(LegSwimOff, "")
	case AreaTreeBank:
		return escape(s, area, "")
	case AreaLedge:
		if stage >= StageEnteredTomb && s.dungeonReady(stage) && s.has(ItemAmulet) {
			return leg(LegLedgeDoor, "")
		}
		// Without the amulet the corridor floods for 8 damage; the barrel is a free washout.
		return leg(LegBarrel, "not ready for the dungeon")
	}
	return escape(s, area, "")
}

// dungeonReady is the dungeon loadout's check made from the river, where no bank can fix it,
// including room for the dungeon key: a full pack with nothing to eat takes the barrel home, not
// the ledge door (which the dungeon's own escape would only open back out again).
func (s Snapshot) dungeonReady(stage Stage) bool {
	ok, _ := DungeonLoadout(stage, s.Settings).Ready(s.Inv, s.Worn, s.IsFood)
	return ok && !s.worn(ItemAmulet) && s.roomForKey()
}

func inDungeon(s Snapshot, stage Stage, area Area) Decision {
	// Without the urn, or without the amulet before the statue, the dungeon has nothing left to
	// give: leave by the cheapest exit.
	if s.held(ItemFullUrn) == 0 {
		return escape(s, area, "the full urn is not held")
	}
	if stage < StageRaisedFloor && s.held(ItemAmulet) == 0 {
		return escape(s, area, "the amulet is not in the pack")
	}
	switch area {
	case AreaRaisedRoom:
		if stage < StageRaisedFloor {
			return leg(LegWashout, "in the raised room before stage 8")
		}
		if s.FreeSlots < 5 {
			return legf(LegFreeSlots, "%d free slots; the reward needs 5", s.FreeSlots)
		}
		return leg(LegChalice, "")
	case AreaPuzzleRoom:
		if stage >= StageRaisedFloor {
			return leg(LegPuzzleDoor, "stage 8: the key on this door goes straight to the raised room")
		}
		if s.Ledger.Complete() {
			if stage != StageEnteredPuzzle {
				return wait(fmt.Sprintf("every pillar is acknowledged but the journal reads %s", stage))
			}
			return leg(LegStatue, "all 18 pillar placements acknowledged this visit")
		}
		if s.Ledger.Empty() {
			// The engine path: the executor re-checks every pair ("You remember putting that
			// type of rune there." costs nothing), so only its own failure means a kind ran out.
			// A rune count alone proves nothing here: the ninth EMPTY lab run placed all six
			// air and earth, and the empty ledger then read "no air left" and walked out at 17/18.
			if s.PillarsFailed {
				return escape(s, area, "the pillars executor failed (a rune kind ran out)")
			}
			return leg(LegPillars, "the executor keeps this visit's ledger")
		}
		if id, short := s.Ledger.RunesShort(s.Inv); short {
			return escape(s, area, fmt.Sprintf("no %s left for pillars %s", RuneName(id), s.Ledger))
		}
		return legf(LegPillars, "%s", s.Ledger)
	case AreaCorridor:
		return leg(LegPuzzleDoor, "")
	case AreaDungeonSouth:
		if s.held(ItemBaxKey) == 0 {
			if InLane(s.Tile) {
				return leg(LegLaneOut, "the key is in the crate room")
			}
			if s.FreeSlots < 1 {
				if s.canMakeRoom() {
					return leg(LegMakeRoom, "no free slot for the dungeon key")
				}
				return escape(s, area, "no free slot for the key")
			}
			return leg(LegBaxKey, "")
		}
		if s.food() == 0 && s.HP < s.MaxHP {
			return escape(s, area, "no food left for the lane")
		}
		return leg(LegLaneIn, "")
	}
	return leg(LegEntryDoor, "somewhere the route does not cover")
}
