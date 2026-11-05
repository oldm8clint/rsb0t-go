package questplan

import (
	"fmt"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Tourist Trap's decision: the quest is a chain of places (TtZone: the shop, the open desert, the camp, the hall behind the mine doors, the
// lower mine, the deep mine) joined by crossings that each want their own clothes or papers, and a stage says which place the next piece of work is
// in. DecideTouristTrap reads the stage off the journal, the place off the tile, and the water, the clothes and the papers off the pack, and answers
// with the one step that makes progress: the work itself when the account is where it is done (ttStage), the crossing towards it when it is not
// (ttToward), the shop when something it cannot go on without is missing (ttRestock), the water rule's turn for water (ttWater), the cell's way
// out when it has been searched (ttJail).

// Custom step names (internal/scripts/questmods).
const (
	CustomTtSouth     = "tt-south"      // the Shantay pass, going south: a pass handed over, the disclaimer
	CustomTtNorth     = "tt-north"      // the pass going north (free), from anywhere in the open desert
	CustomTtDress     = "tt-dress"      // the clothes for a crossing (Qty: the TtDress* mode); everything else comes off at the camp
	CustomTtCaptain   = "tt-captain"    // the taunt and the fight; the Metal key
	CustomTtGateIn    = "tt-gate-in"    // the camp's gate, from outside, with the Metal key
	CustomTtGateOut   = "tt-gate-out"   // the camp's gate from inside, in no slave clothes
	CustomTtKeys      = "tt-keys"       // the Metal key (and the cell key) from Siad's desk
	CustomTtSlave     = "tt-slave"      // the slave's chains and the trade of the clothes
	CustomTtMineIn    = "tt-mine-in"    // the mine doors, in slave clothes
	CustomTtMineOut   = "tt-mine-out"   // the exit doors, from the hall, in slave clothes
	CustomTtPlans     = "tt-plans"      // Siad's bookcase, his distraction and the chest: the technical plans
	CustomTtShowPlans = "tt-show-plans" // the plans shown to Al Shabim
	CustomTtTent      = "tt-tent"       // into the Bedabin tent by the guard, the anvil: a prototype dart tip
	CustomTtFletch    = "tt-fletch"     // the feathers on the tip
	CustomTtDart      = "tt-dart"       // out of the tent and the dart to Al Shabim: the pineapple
	CustomTtPineapple = "tt-pineapple"  // the pineapple to the cave guard
	CustomTtCave      = "tt-cave"       // the guarded cave (Qty 1: into the lower mine, 0: back to the hall)
	CustomTtBarrel    = "tt-barrel"     // the lower mine's barrels (Qty 1: take an empty one, 0: look for Ana in them)
	CustomTtCart      = "tt-cart"       // the mine cart (Qty 1: from the lower mine to the deep one, 0: back)
	CustomTtCatch     = "tt-catch"      // the empty barrel on Ana
	CustomTtDeepCheck = "tt-deep"       // Ana into the deep cart, the ride up, the barrels: Ana found again
	CustomTtLift      = "tt-lift"       // Ana on the lift, under the winch guard's eye
	CustomTtWinch     = "tt-winch"      // the surface winch and the full barrel it brings Ana up in
	CustomTtEscape    = "tt-escape"     // Ana on the cart, the driver's bribe, the ride
	CustomTtRecapture = "tt-recapture"  // the canonical replay when the lift or the cart no longer holds Ana
	CustomTtJailCell  = "tt-jail-cell"  // the surface cell: the window bar and the four rocks
	CustomTtJailMine  = "tt-jail-mine"  // the punishment mine: fifteen rocks and the gate
	CustomTtTentOut   = "tt-tent-out"   // the tent's door
)

// The clothes modes of CustomTtDress (Step.Qty).
const (
	TtDressDesert = 1 // the desert outfit on; whatever else is worn stays
	TtDressCamp   = 2 // nothing but clothes: the camp finds a weapon or armour (the desert outfit on, the slave's off)
	TtDressSlave  = 3 // the slave outfit on and nothing else but clothes
	TtDressPlain  = 4 // the slave outfit off (the gate refuses it going out), the desert outfit on, nothing else but clothes
)

// KitTtTrip is the provisioning kit of a crossing south; KitTtPurse the coins alone.
const (
	KitTtTrip  = "tt-trip"
	KitTtPurse = "tt-purse"
	// KitTtWater is the Bedabin camp's shop for an account that ran short of water in the desert.
	KitTtWater = "tt-water"
)

// ttSkinPrice is a Waterskin(4)'s cost (water_sources.obj); ttWaterCoins the coins the account needs in hand to buy the three the kit asks for, with a
// little over for the shelf's rise.
const (
	ttSkinPrice  = 30
	ttWaterCoins = ttSkinPrice*TtSkins + 10
)

// ttTools are the quest's own items: every bank visit leaves them in the pack. The empty waterskin is left out on purpose: it is junk the next bank
// visit banks.
var ttTools = []int{CoinsID, ObjTtSkin4, ObjTtSkin3, ObjTtSkin2, ObjTtSkin1, ObjTtShirt, ObjTtRobe, ObjTtBoots, ObjTtSlaveShirt, ObjTtSlaveRobe,
	ObjTtSlaveBoots, ObjTtMetalKey, ObjTtCellKey, ObjTtBedobinKey, ObjTtGoodMineKey, ObjTtPlans, ObjTtDartTip, ObjTtDart, ObjTtPineapple, ObjTtBarrel,
	ObjTtAnaBarrel, ObjTtPass, ObjTtDisclaimer, ObjTtBronzeDart, ObjTtFeather, ObjTtBronzePickax, ObjBronzeBar, ObjHammer}

// TtToolIDs returns the quest's own items (a copy).
func TtToolIDs() []int { return append([]int(nil), ttTools...) }

// ttKeep is a bank visit's keep list: the tools and the food.
func ttKeep() []int { return append(append([]int(nil), ttTools...), ttFoodIDs...) }

// ttClothes are the pieces the camp lets an account wear.
var ttClothes = map[int]bool{ObjTtShirt: true, ObjTtRobe: true, ObjTtBoots: true, ObjTtSlaveShirt: true, ObjTtSlaveRobe: true, ObjTtSlaveBoots: true}

// TtClothesID reports a piece of either outfit.
func TtClothesID(id int) bool { return ttClothes[id] }

// The stops. Every menu is enumerated; Dialogue.Drive never guesses, so a menu without the answer stops the talk.
var (
	// TtIrenaStartStop starts the quest: "What's the matter?" among two, "Is there a reward if I get her back?" among three, "Okay Irena, calm down.
	// I'll get your daughter back for you." among two.
	TtIrenaStartStop = NpcStop{Npc: "Irena", ID: NpcTtIrena, Anchor: TtIrena, Leash: 6,
		Prefer: []string{"What's the matter?", "Is there a reward if I get her back?", "Okay Irena, calm down. I'll get your daughter back for you."}}
	// TtIrenaRewardStop is every talk with Irena once the quest has gone past the start. With Ana in a barrel at stage 26 she runs straight into
	// the reward menus (irena_xpreward, twice): Thieving both times, the one skill the fleet's gates and endgame want (TtRewardPick).
	TtIrenaRewardStop = NpcStop{Npc: "Irena", ID: NpcTtIrena, Anchor: TtIrena, Leash: 6, Prefer: []string{TtRewardPick}}
	// TtCaptainStop is the taunt: the five answers that end in "Very well, if you're challenging me, let's get on with it!". The first menu also offers
	// "You there!" and "Hey ugly!" (the guards' beating) and the last "Okay, I'll be moving along then." (a quiet exit): none is ever preferred.
	TtCaptainStop = NpcStop{Npc: "Mercenary Captain", ID: NpcTtCaptain, Anchor: TtCaptainAt, Leash: 12,
		Prefer: []string{"Wow! A real captain!", "I'd love to work for a tough guy like you!", "Can't I do something for a strong Captain like you?",
			"Sorry Sir, I don't think I can do that.", "It's a funny captain who can't fight his own battles!"}}
	// TtSlaveStop is the slave's whole conversation, at whatever point the stage left it: the introduction, the offer, the lock (and the retries) and the
	// trade. A failed pick offers "Yeah I'll give it another go." and "I'll try something different instead."; the first is always taken.
	TtSlaveStop = NpcStop{Npc: "Male slave", ID: NpcTtSlave, Anchor: TtSlaveAt, Leash: 8,
		Prefer: []string{"I've just arrived.", "Oh yes, that sounds interesting.", "What's that then?", "I can try to undo them for you.",
			"It's funny you should say that...", "Yeah, okay, let's give it a go.", "Yeah I'll give it another go.", "Yes, I'll trade."}}
	// TtGuardStop is the cave guard's: "I'd like to mine in a different area." among two, "Yes sir, you're quite right sir." among two and "Yes sir,
	// we understand each other perfectly." among three (the stage is 10 after the second).
	TtGuardStop = NpcStop{Npc: "Mercenary", ID: NpcTtCaveGuard, Anchor: TtGuardAt, Leash: 8,
		Prefer: []string{"I'd like to mine in a different area.", "Yes sir, you're quite right sir.", "Yes sir, we understand each other perfectly."}}
	// TtAlDealStop is Al Shabim at stage 10: "I am looking for a pineapple." and "Yes, I'm interested." (the key to Siad's chest, stage 11).
	TtAlDealStop = NpcStop{Npc: "Al Shabim", ID: NpcTtAlShabim, Anchor: TtAlAt, Leash: 10,
		Prefer: []string{"I am looking for a pineapple.", "Yes, I'm interested."}}
	// TtAlKeyStop replaces a lost key (stage 11, or 12 and later with the plans gone too: "I've lost the key and the plans!").
	TtAlKeyStop = NpcStop{Npc: "Al Shabim", ID: NpcTtAlShabim, Anchor: TtAlAt, Leash: 10,
		Prefer: []string{"I've lost the key!", "I've lost the key and the plans!"}}
	// TtAlPineappleStop replaces a lost pineapple (stage 16): "I am looking for a pineapple."
	TtAlPineappleStop = NpcStop{Npc: "Al Shabim", ID: NpcTtAlShabim, Anchor: TtAlAt, Leash: 10, Prefer: []string{"I am looking for a pineapple."}}
	// TtSiadStop is Captain Siad, once the bookcase has been searched: "I wanted to have a chat?", "You seem to have a lot of books!", "So, you're
	// interested in sailing?" and "I could tell by the cut of your jib." distract him (siad_jib: the bit is set and kept until the chest is tried).
	TtSiadStop = NpcStop{Npc: "Captain Siad", ID: NpcTtSiad, Anchor: TtSiadAt, Leash: 8,
		Prefer: []string{"I wanted to have a chat?", "You seem to have a lot of books!", "So, you're interested in sailing?", "I could tell by the cut of your jib."}}
	// TtDriverStop is the cart driver's, with 100 coins: "Nice cart.", the wheel joke, "'One good turn deserves another'", "Fired... no, shot perhaps!",
	// "In for a penny in for a pound.", "Well, you see, it's like this...", the ten gold offer and "A hundred it is!" (the ready bit is set when the
	// chat ends: it is driven to its end, never closed early). Without 100 coins the riot appeal is the way: see TtDriverRiotAnswers.
	TtDriverStop = NpcStop{Npc: "Mine cart driver", ID: NpcTtDriver, Anchor: TtDriverAt, Leash: 8,
		Prefer: []string{"Nice cart.", "One wagon wheel says to the other, 'I'll see you around'.", "'One good turn deserves another'", "Fired... no, shot perhaps!",
			"In for a penny in for a pound.", "Well, you see, it's like this...", "There's ten gold in it for you if you leave now - no questions asked.", "A hundred it is!"}}
	// TtLiftGuardAnswers are the winch guard's two menus when the account carries Ana to the lift.
	TtLiftGuardAnswers = []string{"Yes please.", "I said you were very gregarious!"}
)

// TtRewardPick is the reward menu's choice, matched as a substring: Thieving (see the package doc).
const TtRewardPick = "Thieving"

// TtDriverRiotAnswers are the answers of the driver's chat for an account with fewer than 100 coins: the prison riot appeal, which also sets the
// ready bit ("You can't leave me here, I'll get killed!"). The bribe is the clean way; this is the net.
var TtDriverRiotAnswers = []string{"Nice cart.", "One wagon wheel says to the other, 'I'll see you around'.", "'One good turn deserves another'", "Fired... no, shot perhaps!",
	"In for a penny in for a pound.", "Well, you see, it's like this...", "Prison riot in ten minutes, get your cart out of here!",
	"You can't leave me here, I'll get killed!"}

// ---------------------------------------------------------------------------------------------------------------
// What the pack says.
// ---------------------------------------------------------------------------------------------------------------

// ttFacts is what the snapshot says, read once per decision.
type ttFacts struct {
	s     Snapshot
	stage int
	tile  tile.Tile
	area  TtArea
	zone  TtZone
	// doses are the waterskins' sips; skins the full ones.
	doses, skins int
	coins        int
	// heal is the hitpoints the pack's food restores.
	heal int
	// sets are the complete outfits owned (carried or worn); worn says every piece of the outfit is on.
	desertSets, slaveSets int
	desertWorn, slaveWorn bool
	// clothesOnly says nothing but the outfits' pieces is worn; gear is how many weapons and pieces of armour are worn (each takes a pack slot when
	// the camp's strip takes it off).
	clothesOnly             bool
	gear                    int
	hasAna, hasBarrel       bool
	hasKey, hasPlans        bool
	hasTip, hasDart         bool
	hasPineapple, hasBedKey bool
}

func ttOwned(s Snapshot, id int) int { return s.InvIDs[id] + s.WornIDs[id] }

func ttRead(s Snapshot) ttFacts {
	f := ttFacts{s: s, stage: s.Stage, tile: s.Tile}
	f.area = TtAreaOf(s.Tile)
	f.zone = f.area.ZoneOf()
	f.skins = s.InvIDs[ObjTtSkin4]
	f.doses = 4*s.InvIDs[ObjTtSkin4] + 3*s.InvIDs[ObjTtSkin3] + 2*s.InvIDs[ObjTtSkin2] + s.InvIDs[ObjTtSkin1]
	f.coins = s.InvIDs[CoinsID]
	f.heal = FoodPoolHeal(s.InvIDs, ttFoodIDs)
	f.desertSets = min(ttOwned(s, ObjTtShirt), ttOwned(s, ObjTtRobe), ttOwned(s, ObjTtBoots))
	f.slaveSets = min(ttOwned(s, ObjTtSlaveShirt), ttOwned(s, ObjTtSlaveRobe), ttOwned(s, ObjTtSlaveBoots))
	f.desertWorn = s.WornIDs[ObjTtShirt] > 0 && s.WornIDs[ObjTtRobe] > 0 && s.WornIDs[ObjTtBoots] > 0
	f.slaveWorn = s.WornIDs[ObjTtSlaveShirt] > 0 && s.WornIDs[ObjTtSlaveRobe] > 0 && s.WornIDs[ObjTtSlaveBoots] > 0
	f.clothesOnly = true
	for id, n := range s.WornIDs {
		if n > 0 && !ttClothes[id] {
			f.clothesOnly = false
		}
	}
	f.gear = ttGearPieces(s)
	f.hasAna, f.hasBarrel = s.InvIDs[ObjTtAnaBarrel] > 0, s.InvIDs[ObjTtBarrel] > 0
	f.hasKey, f.hasPlans = s.InvIDs[ObjTtMetalKey] > 0, s.InvIDs[ObjTtPlans] > 0
	f.hasTip, f.hasDart = s.InvIDs[ObjTtDartTip] > 0, s.InvIDs[ObjTtDart] > 0
	f.hasPineapple, f.hasBedKey = s.InvIDs[ObjTtPineapple] > 0, s.InvIDs[ObjTtBedobinKey] > 0
	return f
}

// slaveAnyWorn reports any piece of the slave outfit on.
func (f ttFacts) slaveAnyWorn() bool {
	return f.s.WornIDs[ObjTtSlaveShirt]+f.s.WornIDs[ObjTtSlaveRobe]+f.s.WornIDs[ObjTtSlaveBoots] > 0
}

// ttGearPieces counts the weapons and armour worn: every piece the quest's clothes do not make, one pack slot each when the camp's strip takes it off.
func ttGearPieces(s Snapshot) int {
	n := 0
	for id, c := range s.WornIDs {
		if c > 0 && !ttClothes[id] {
			n++
		}
	}
	return n
}

// TtJunkIDs are what a pack under pressure drops first, in this order: an empty skin, a skin with one sip, the reward's darts, a kebab, the cell key,
// the disclaimer, the key to the wrong mine, a spent pass; and, once the tip or the dart is made, the hammer and the bars. They never include a key to
// the camp, a barrel, the plans, the outfits or the coins. The executors drop in this order (questmods ttMakeRoom) and the plan counts them
// (ttJunkSlots), so that what the plan thinks the pack can free is what the executor frees.
func TtJunkIDs(papersSpare bool) []int {
	ids := []int{ObjTtSkin0, ObjTtSkin1, ObjTtBronzeDart, ObjTtKebab, ObjTtCellKey, ObjTtDisclaimer, ObjTtGoodMineKey, ObjTtPass}
	if papersSpare {
		ids = append(ids, ObjHammer, ObjBronzeBar)
	}
	return ids
}

// ttJunkSlots is the pack slots TtJunkIDs would free: a stack (the reward's darts, a pass) is one slot, anything else one each.
func ttJunkSlots(f ttFacts) int {
	n := 0
	for _, id := range TtJunkIDs(f.hasTip || f.hasDart) {
		c := f.s.InvIDs[id]
		switch {
		case c <= 0:
		case id == ObjTtBronzeDart || id == ObjTtPass:
			n++
		default:
			n += c
		}
	}
	return n
}

// ttSpareSlots is the pack room the first kit leaves for the food and the dart's bars and hammer: the pack less the gear the camp's strip puts in it,
// the slack for what the quest hands over (and for the desert outfit the slave's dress takes off), and the lines that are always carried (the coins,
// the Metal key, the disclaimer, the pass, three skins, the spare outfit and the feathers' stack). Gear no longer worn is not reserved: it is in the
// pack already, and the first bank visit banks it.
func ttSpareSlots(f ttFacts) int {
	fixed := 4 + TtSkins + 3 + 1
	return TtPackSlots - f.gear - TtSlotSlack - fixed
}

// ttFoodLine reports the kit's food lines, which are judged as one pool by the hitpoints they restore, not line by line.
func ttFoodLine(id int) bool { return id == ttSharkID || id == ttBassID }

const (
	ttSharkID = 385
	ttBassID  = 365
)

// ---------------------------------------------------------------------------------------------------------------
// Step builders.
// ---------------------------------------------------------------------------------------------------------------

func ttCustom(name string, qty int, reason string) Step {
	return Step{Kind: StepCustom, Custom: name, Qty: qty, Reason: reason}
}

func ttWait(park time.Duration, format string, args ...any) Step {
	return Step{Kind: StepWait, ParkFor: park, Reason: fmt.Sprintf(format, args...)}
}

func ttTalk(stop NpcStop, reason string) Step {
	return Step{Kind: StepTalk, Stop: stop, Reason: reason}
}

// ttTalkUntil is a talk driven until an item is held (the pineapple, the key).
func ttTalkUntil(stop NpcStop, item int, reason string) Step {
	return Step{Kind: StepTalk, Stop: stop, UntilItemID: item, Reason: reason}
}

// ttBank is the bank a provisioning or deposit visit uses: Al Kharid's for an account at the pass or in the desert (the walk crosses the pass
// northward for nothing), else the nearest the other fillers use.
func ttBank(s Snapshot) tile.Tile {
	if s.TileKnown {
		a := TtAreaOf(s.Tile)
		if a.South() || a == TtAreaShantayNorth || princeInAlKharid(s.Tile) {
			return AlKharidBank
		}
	}
	return princeBank(s)
}

func ttProvision(s Snapshot, name string, kit []KitLine, reason string) Step {
	return Step{Kind: StepProvision, KitName: name, Kit: kit, KeepIDs: ttKeep(), Bank: ttBank(s), BankSet: true, Reason: reason}
}

// ---------------------------------------------------------------------------------------------------------------
// The kit.
// ---------------------------------------------------------------------------------------------------------------

// ttShopSource is Shantay's shelf, priced at the base cost and a rise as it empties (a shirt is 40, a robe 40, boots 20, a skin 30, a pass 5, a bar 8,
// a feather 2, a hammer 1).
func ttShopSource(price int, note string) []Source {
	return []Source{{Kind: SourceShop, Shop: ShantayShop, Price: price, Note: note}}
}

// ttWant is a kit line the account should carry across the pass at this stage: the outfits, the water, the pass, the papers for the dart, a little food.
// critAt is the count under which the line is one the quest cannot go on without once south (0: never; the line is asked for while the account is at
// the shop anyway).
type ttWant struct {
	line   KitLine
	critAt int
}

func ttWants(f ttFacts) []ttWant {
	var w []ttWant
	add := func(l KitLine, critAt int) { w = append(w, ttWant{line: l, critAt: critAt}) }
	st := f.stage
	// The outfits: two sets until the slave has traded (one stays on for the heat, one is traded), none after unless the slave outfit is gone, when one
	// set buys it back. A set is what the trade wants at all.
	switch {
	case st < TtTraded:
		crit := 0
		if st >= TtEnteredCmp {
			crit = 1
		}
		add(KitLine{ID: ObjTtShirt, Name: "Desert shirt", Qty: 2, Worn: true, Sources: ttShopSource(45, "Shantay keeps 10, one back every 500 ticks")}, crit)
		add(KitLine{ID: ObjTtRobe, Name: "Desert robe", Qty: 2, Worn: true, Sources: ttShopSource(45, "Shantay keeps 10")}, crit)
		add(KitLine{ID: ObjTtBoots, Name: "Desert boots", Qty: 2, Worn: true, Sources: ttShopSource(25, "Shantay keeps 10")}, crit)
	case st < TtReward && f.slaveSets == 0:
		add(KitLine{ID: ObjTtShirt, Name: "Desert shirt", Qty: 1, Worn: true, Sources: ttShopSource(45, "")}, 0)
		add(KitLine{ID: ObjTtRobe, Name: "Desert robe", Qty: 1, Worn: true, Sources: ttShopSource(45, "")}, 0)
		add(KitLine{ID: ObjTtBoots, Name: "Desert boots", Qty: 1, Worn: true, Sources: ttShopSource(25, "")}, 0)
	}
	// The water: three full skins, topped up from the doses the pack holds (the partly drunk skins are drunk first).
	if f.doses < TtWaterEntry {
		add(KitLine{ID: ObjTtSkin4, Name: "Waterskin(4)", Qty: f.skins + (TtWaterEntry-f.doses+3)/4, Sources: ttShopSource(35, "Shantay keeps 100")}, 0)
	}
	// The pass, for the way south.
	if f.zone == TtZoneNorth {
		add(KitLine{ID: ObjTtPass, Name: "Shantay pass", Qty: 1, Stackable: true, Sources: ttShopSource(6, "Shantay keeps 500")}, 0)
	}
	// The pack's room (ttSpareSlots) is shared by the dart's bars and hammer and by the food, the food first: the camp's strip puts every weapon and
	// piece of armour in the pack, and a kit that fills it leaves the strip nowhere to go. A first kit that cannot hold the bars and the hammer beside
	// the least food leaves them out; they are critical from the plans on (papers below), and the trip then made for them finds the pack empty of the
	// gear (the bank visit banks it).
	spare := ttSpareSlots(f)
	bars, hammer := 0, 0
	// The papers for the dart: a hammer, bronze bars and feathers while the tip and the dart are unmade (the plans shown to Al want a bar and ten
	// feathers, the anvil a hammer and a bar, the dart ten feathers).
	if st < TtLearned && !f.hasDart {
		papers := 0
		if st >= TtGotPlans {
			papers = 1
		}
		if !f.hasTip && (papers == 1 || spare >= TtFoodMin+1+TtBarMin) {
			bars, hammer = TtBars, 1
			if papers == 0 {
				bars = min(TtBars, spare-TtFoodMin-hammer)
			}
			add(KitLine{ID: ObjBronzeBar, Name: "Bronze bar", Qty: bars, Min: min(TtBarMin, bars), Sources: ttShopSource(10, "Shantay keeps 10")}, papers)
			add(KitLine{ID: ObjHammer, Name: "Hammer", Qty: hammer, Sources: ttShopSource(3, "Shantay keeps 10")}, papers)
		}
		add(KitLine{ID: ObjTtFeather, Name: "Feather", Qty: TtFeathers, Min: TtFeatherMin, Stackable: true, Sources: ttShopSource(3, "Shantay keeps 500")}, papers*10)
	}
	// Food, from the bank and the mule only (no shop on this side of the pass sells any): the captain's fight and the walks eat it, and the camp's
	// strip leaves the account bare-handed. The lines are not Optional: a kit that no source can feed fails, and the crossing parks on the floor
	// (TtFoodEntryHeal). The pack's food is judged as one pool by the hitpoints it restores (ttShortLines); the line asks for the pieces that restore
	// the floor, the bass only while the sharks are short.
	if qty := min(TtFoodWant, max(spare-bars-hammer, 0)); qty > 0 {
		add(KitLine{ID: ttSharkID, Name: "Shark", Qty: qty, Min: min(TtFoodMin, qty)}, 0)
		bassMin := (TtFoodEntryHeal + FoodItemHeal(ttBassID) - 1) / FoodItemHeal(ttBassID)
		add(KitLine{ID: ttBassID, Name: "Bass", Qty: qty, Min: min(bassMin, qty), OnlyIfShort: ttSharkID}, 0)
	}
	return w
}

func ttKitOf(ws []ttWant) []KitLine {
	kit := make([]KitLine, 0, len(ws))
	for _, w := range ws {
		kit = append(kit, w.line)
	}
	return kit
}

// ttShortLines names the lines the pack lacks by their Needs (all), and those under their critical count (critical). Optional lines are neither. A line
// that only serves another (OnlyIfShort) is short only while that one is; the food lines are short only while the pack's food restores less than the
// entry floor, whichever food it is.
func ttShortLines(f ttFacts, ws []ttWant) (critical, all []string) {
	for _, w := range ws {
		if w.line.Optional {
			continue
		}
		if ttFoodLine(w.line.ID) && f.heal >= TtFoodEntryHeal {
			continue
		}
		if m := w.line.OnlyIfShort; m != 0 {
			masterShort := false
			for _, o := range ws {
				if o.line.ID == m && o.line.Needs(f.s.InvIDs, f.s.WornIDs) > 0 {
					masterShort = true
				}
			}
			if !masterShort {
				continue
			}
		}
		if w.line.Needs(f.s.InvIDs, f.s.WornIDs) > 0 {
			all = append(all, w.line.Name)
		}
		if w.critAt > 0 && w.line.Held(f.s.InvIDs, f.s.WornIDs) < w.critAt {
			critical = append(critical, w.line.Name)
		}
	}
	return critical, all
}

// ---------------------------------------------------------------------------------------------------------------
// Crossings.
// ---------------------------------------------------------------------------------------------------------------

// ttCrossSouth is the walk from the shop side into the desert: the coins, the kit (Failed means the shop and the mule could not give it all: the
// account goes on with what is critical), the captain priced while the stage still wants him, the clothes put on, the pass.
func ttCrossSouth(f ttFacts) Step {
	s := f.s
	if s.MaxHP > 0 && s.MaxHP < TtMinHitpoints {
		return ttWait(TtLevelPark, "%d base hitpoints: the desert's wolves and the rowdy guards want %d", s.MaxHP, TtMinHitpoints)
	}
	if f.coins < TtCoinFloor {
		if s.Failed["provision:"+KitTtPurse] {
			return ttWait(TtItemPark, "the tourist trap's driver, tolls and shops need %d coins (the pack holds %d) and the bank and the mule have no more", TtCoinFloor, f.coins)
		}
		return ttProvision(s, KitTtPurse, []KitLine{{ID: CoinsID, Name: "Coins", Qty: TtCoinStart, Stackable: true}}, "the driver's bribe, the tolls and the shops")
	}
	ws := ttWants(f)
	crit, all := ttShortLines(f, ws)
	if len(all) > 0 && !s.Failed["provision:"+KitTtTrip] {
		return ttProvision(s, KitTtTrip, ttKitOf(ws), fmt.Sprintf("the desert kit: %v", all))
	}
	if len(crit) > 0 {
		return ttWait(TtItemPark, "the desert trip needs %v and the bank, the mule and Shantay's shelf have none", crit)
	}
	// The food floor: the strip leaves the account bare-handed for the guards and the wolves, with only what it can eat.
	if f.heal < TtFoodEntryHeal && !f.hasAna && f.stage < TtReward {
		return ttWait(TtItemPark, "no food for the desert: the pack's food restores %d hitpoints, the trip wants %d, and the bank and the mule have no more", f.heal, TtFoodEntryHeal)
	}
	if why := ttFightShort(f); why != "" {
		return ttWait(TtCaptainPark, "%s", why)
	}
	if f.doses < TtWaterFloor {
		return ttWait(TtItemPark, "no water for the desert: %d doses and Shantay's shelf has none", f.doses)
	}
	if !f.desertWorn && f.desertSets > 0 {
		return ttCustom(CustomTtDress, TtDressDesert, "the desert outfit on for the heat")
	}
	if s.InvIDs[ObjTtPass] == 0 {
		return ttWait(TtItemPark, "no Shantay pass for the way south")
	}
	return ttCustom(CustomTtSouth, 0, "through the Shantay pass with a pass, the disclaimer and the water")
}

// ttGateIn is the way from the open desert into the camp: the Metal key, no weapon or armour on, the gate.
func ttGateIn(f ttFacts) Step {
	if why := ttFightShort(f); why != "" {
		return ttParkSafely(f, TtCaptainPark, why)
	}
	switch {
	case !f.hasKey:
		return ttNoKey(f)
	case !f.clothesOnly:
		return ttDressCamp(f, "nothing but clothes into the camp: the guards search a weapon or armour")
	}
	return ttCustom(CustomTtGateIn, 0, "the Metal key opens the camp's gate")
}

// ttFightShort prices the fights the next piece of the quest meets and says why the account should not go ("" for none): the Mercenary Captain until
// he is dead, and a rowdy guard from the first gate until the reward, on the account's levels and the food in its pack. An account that carries Ana
// is not judged: its way is the short one to Irena.
func ttFightShort(f ttFacts) string {
	if f.hasAna {
		return ""
	}
	if f.stage <= TtApproached {
		if ok, why := TtCaptainFit(f.s); !ok {
			return "not going to the Mercenary Captain: " + why
		}
	}
	if f.stage < TtReward {
		if ok, why := TtCampFit(f.s); !ok {
			return "not going into the mining camp and the mines: " + why
		}
	}
	return ""
}

// ttParkSafely parks the quest, but from the north side of the pass: an account south of it with its water running down walks out first (the desert
// deals 1 to 10 damage a heat tick to an account with no water, and a parked quest leaves the account to other work for hours). Carrying Ana it parks
// where it stands.
func ttParkSafely(f ttFacts, park time.Duration, why string) Step {
	if f.zone >= TtZoneOpen && !f.hasAna {
		st := ttToward(f, TtZoneNorth)
		st.Reason = "out of the desert before parking: " + why
		return st
	}
	return ttWait(park, "%s", why)
}

// ttDressCamp is the strip for the camp: nothing but clothes. The weapon and armour come off into the pack, one slot each, so a pack without the room
// (after the junk it can drop) parks the quest with the slots named, not a strip that fails for ever.
func ttDressCamp(f ttFacts, reason string) Step {
	if f.gear > 0 {
		if room := f.s.FreeSlots + ttJunkSlots(f); room < f.gear {
			return ttParkSafely(f, TtItemPark, fmt.Sprintf("the camp's strip needs %d free pack slots for the %d weapons and pieces of armour worn: the pack has %d free (%d with its junk dropped)",
				f.gear, f.gear, f.s.FreeSlots, room))
		}
	}
	return ttCustom(CustomTtDress, TtDressCamp, reason)
}

// ttNoKey is a lost Metal key: the bank's copy (a key in the bank blocks the captain's gift), else Siad's desk inside the camp, else the captain's
// respawn.
func ttNoKey(f ttFacts) Step {
	s := f.s
	if f.zone == TtZoneCamp {
		return ttCustom(CustomTtKeys, 0, "the Metal key and the cell key from Siad's desk")
	}
	if !s.BankKnown {
		if f.zone != TtZoneNorth {
			return ttToward(f, TtZoneNorth)
		}
		return Step{Kind: StepScanBank, Bank: ttBank(s), BankSet: true, Reason: "the Metal key is not in the pack: the bank is read first"}
	}
	if n, _ := s.BankedID(ObjTtMetalKey); n > 0 {
		if f.zone != TtZoneNorth {
			return ttToward(f, TtZoneNorth)
		}
		return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Metal key", ID: ObjTtMetalKey, Qty: 1}}, Bank: ttBank(s), BankSet: true,
			Reason: "the Metal key from the bank"}
	}
	return ttCaptain(f, "the Metal key again: the captain is back after 50 ticks")
}

// ttCaptain is the taunt and the fight, priced.
func ttCaptain(f ttFacts, reason string) Step {
	s := f.s
	if ok, why := TtCaptainFit(s); !ok {
		return ttParkSafely(f, TtCaptainPark, "not taunting the Mercenary Captain: "+why)
	}
	if f.zone != TtZoneOpen {
		return ttToward(f, TtZoneOpen)
	}
	return ttCustom(CustomTtCaptain, 0, reason)
}

// ttMineIn is the camp's mine doors, in slave clothes.
func ttMineIn(f ttFacts) Step {
	if why := ttFightShort(f); why != "" {
		return ttParkSafely(f, TtCaptainPark, why)
	}
	switch {
	case f.slaveSets == 0:
		return ttRetrade(f)
	case !f.slaveWorn || !f.clothesOnly:
		return ttCustom(CustomTtDress, TtDressSlave, "the slave's clothes on: the mine doors let nobody else through")
	}
	return ttCustom(CustomTtMineIn, 0, "the mine doors")
}

// ttRetrade buys the slave outfit again: a desert set for the slave's clothes (the slave asks again after a catch).
func ttRetrade(f ttFacts) Step {
	if f.desertSets == 0 {
		if f.zone != TtZoneNorth {
			return ttToward(f, TtZoneNorth)
		}
		return ttCrossSouth(f)
	}
	if f.zone != TtZoneCamp {
		return ttToward(f, TtZoneCamp)
	}
	if !f.clothesOnly {
		return ttDressCamp(f, "nothing but clothes in the camp")
	}
	return ttCustom(CustomTtSlave, 0, "trade a desert set for the slave outfit again")
}

// ttToward is the first step of the way from the account's zone to goal.
func ttToward(f ttFacts, goal TtZone) Step {
	switch {
	case f.zone == goal:
		return ttWait(0, "already in zone %d", goal)
	case f.zone == TtZoneNone:
		return ttCustom(CustomTtGateOut, 0, "out of a place the chain does not join")
	case goal > f.zone:
		return ttUp(f)
	}
	return ttDown(f)
}

func ttUp(f ttFacts) Step {
	switch f.zone {
	case TtZoneNorth:
		return ttCrossSouth(f)
	case TtZoneOpen:
		return ttGateIn(f)
	case TtZoneCamp:
		return ttMineIn(f)
	case TtZoneHall:
		if f.stage < TtGivenPine {
			return ttWait(0, "the cave stays shut until the guard has his pineapple (stage %d)", f.stage)
		}
		if !f.slaveWorn || !f.clothesOnly {
			return ttCustom(CustomTtDress, TtDressSlave, "the slave's clothes on for the cave")
		}
		return ttCustom(CustomTtCave, 1, "through the guarded cave into the lower mine")
	case TtZoneLower:
		return ttCustom(CustomTtCart, 1, "the mine cart to the deep mine")
	}
	return ttWait(0, "no way up from zone %d", f.zone)
}

func ttDown(f ttFacts) Step {
	switch f.zone {
	case TtZoneDeep:
		return ttCustom(CustomTtCart, 0, "the deep mine's cart back to the lower mine")
	case TtZoneLower:
		return ttCustom(CustomTtCave, 0, "through the guarded cave back to the hall")
	case TtZoneHall:
		if !f.slaveWorn {
			return ttCustom(CustomTtDress, TtDressSlave, "the slave's clothes on for the exit doors")
		}
		return ttCustom(CustomTtMineOut, 0, "the exit doors to the camp")
	case TtZoneCamp:
		if !f.hasKey && !f.hasAna {
			// The gate wants the Metal key going out as well as coming in.
			return ttCustom(CustomTtKeys, 0, "the Metal key from Siad's desk: the gate wants it going out too")
		}
		if f.slaveAnyWorn() {
			return ttCustom(CustomTtDress, TtDressPlain, "the slave's clothes off: the gate refuses them going out")
		}
		return ttCustom(CustomTtGateOut, 0, "out of the camp by the gate")
	case TtZoneOpen:
		return ttCustom(CustomTtNorth, 0, "back through the pass for supplies")
	}
	return ttWait(0, "no way down from zone %d", f.zone)
}

// ---------------------------------------------------------------------------------------------------------------
// Water.
// ---------------------------------------------------------------------------------------------------------------

// ttOpenStage reports a stage whose next work is in the open desert, where the heat is on for the walk.
func ttOpenStage(stage int) bool {
	switch stage {
	case TtStarted, TtApproached, TtKilledCapt, TtFindingPin, TtGotPlans, TtShownPlans, TtMadeTip, TtMadeDart, TtReward:
		return true
	}
	return false
}

// ttWater is the water rule. In the heat, with fewer doses than TtWaterFloor, the account turns for water at once: the Bedabin camp's shop for an
// account that is nearer it and has the coins, the pass otherwise. About to leave the camp for the open desert it does the same, so that no leg of
// the desert is begun dry. The water is never needed inside the camp, the hall or the mines (the heat is off there), nor by an account that carries Ana
// to Irena: the walk is under a minute and a half.
func ttWater(f ttFacts) (Step, bool) {
	if f.doses >= TtWaterFloor || f.hasAna {
		return Step{}, false
	}
	heat := f.zone == TtZoneOpen
	leaving := f.zone == TtZoneCamp && ttOpenStage(f.stage)
	switch {
	case heat:
		if f.tile.X < 3235 && f.coins >= ttWaterCoins && !f.s.Failed["provision:"+KitTtWater] {
			// The shop is a provision step like every shop item (ShopItemStep: the item rule, never a bare buy): a skin is in the bank or the mule's
			// kit before it is bought, and once the kit has failed the account goes through the pass instead.
			st := ShopItemStep(f.s, KitTtWater, ObjTtSkin4, "Waterskin(4)", TtSkins, TtBedabinShop, ttSkinPrice, ttKeep())
			st.Bank, st.BankSet = ttBank(f.s), true
			st.Reason = fmt.Sprintf("%d doses of water in the desert: the Bedabin camp's shop", f.doses)
			return st, true
		}
		return ttCustom(CustomTtNorth, 0, fmt.Sprintf("%d doses of water in the desert: through the pass for more", f.doses)), true
	case leaving:
		return ttDown(f), true
	}
	return Step{}, false
}

// ---------------------------------------------------------------------------------------------------------------
// Restocking: what the quest cannot go on without.
// ---------------------------------------------------------------------------------------------------------------

// ttRestock is the walk to the shop when the stage's critical lines are short in the pack: back through the pass, the kit, and the crossing again.
// A shop that cannot give them parks the quest (ttCrossSouth does that).
func ttRestock(f ttFacts) Step {
	if f.zone == TtZoneNorth {
		return ttCrossSouth(f)
	}
	return ttToward(f, TtZoneNorth)
}

// ttCriticalShort is what the stage cannot go on without, once south: the papers for the dart at stage 12 and after, one outfit for the trade, and
// the coins for the cart driver.
func ttCriticalShort(f ttFacts) []string {
	if f.zone == TtZoneNorth || f.zone == TtZoneNone {
		return nil
	}
	crit, _ := ttShortLines(f, ttWants(f))
	if f.stage >= TtGivenPine && f.stage < TtReward && !f.hasAna && f.coins < 100 {
		crit = append(crit, "Coins")
	}
	// Food: south of the pass the account is bare-handed for the guards, the wolves and the Ugthanki. Under one shark's worth it turns for the bank
	// (the crossing south parks it if the bank and the mule have none). Nothing is left to eat for after the reward's talk, or carrying Ana.
	if f.heal < TtFoodRestockHeal && !f.hasAna && f.stage < TtReward {
		crit = append(crit, "Food")
	}
	return crit
}

// ---------------------------------------------------------------------------------------------------------------
// The cells.
// ---------------------------------------------------------------------------------------------------------------

func ttJail(f ttFacts) (Step, bool) {
	switch f.area {
	case TtAreaSurfaceCell:
		return ttCustom(CustomTtJailCell, 0, "searched and put in the cell: the window's bar and the four rocks"), true
	case TtAreaMineCell:
		return ttCustom(CustomTtJailMine, 0, "put in the punishment mine: fifteen rocks and the gate"), true
	case TtAreaTent:
		if f.stage != TtShownPlans && f.stage != TtMadeTip && f.stage != TtMadeDart {
			return ttCustom(CustomTtTentOut, 0, "out of the tent"), true
		}
	}
	return Step{}, false
}

// ---------------------------------------------------------------------------------------------------------------
// The decision.
// ---------------------------------------------------------------------------------------------------------------

// DecideTouristTrap is the whole quest.
func DecideTouristTrap(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		s.Stage, s.StageKnown = TtNotStarted, true
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	if !s.TileKnown {
		return Step{Kind: StepWait, Reason: "the account's position has not been read"}
	}
	f := ttRead(s)
	if f.stage >= TtComplete {
		return Step{Kind: StepDone}
	}
	if step, ok := ttJail(f); ok {
		return step
	}
	if why := ttLevelShort(f); why != "" {
		return ttParkSafely(f, TtLevelPark, why)
	}
	if step, ok := ttWater(f); ok {
		return step
	}
	if len(ttCriticalShort(f)) > 0 {
		return ttRestock(f)
	}
	return ttStage(f)
}

// ttLevelShort names the level the stage's piece of work wants and the account lacks ("" for none; a level the snapshot has not read is not judged).
func ttLevelShort(f ttFacts) string {
	s := f.s
	if f.stage < TtShownPlans || f.stage > TtMadeDart || f.hasDart {
		return ""
	}
	if !f.hasTip {
		if lvl := s.Level("smithing"); lvl > 0 && lvl < TtSmithing {
			return fmt.Sprintf("the experimental anvil wants Smithing %d (the account has %d)", TtSmithing, lvl)
		}
	}
	if lvl := s.Level("fletching"); lvl > 0 && lvl < TtFletching {
		return fmt.Sprintf("the prototype dart wants Fletching %d (the account has %d)", TtFletching, lvl)
	}
	return ""
}

// ttStage is the stage's piece of work and the way to the place it is done in.
func ttStage(f ttFacts) Step {
	switch f.stage {
	case TtNotStarted:
		if f.zone != TtZoneOpen {
			return ttToward(f, TtZoneOpen)
		}
		return ttTalk(TtIrenaStartStop, "start the Tourist Trap with Irena")

	case TtStarted, TtApproached:
		return ttCaptain(f, "taunt the Mercenary Captain and win the duel")

	case TtKilledCapt:
		if !f.hasKey {
			return ttNoKey(f)
		}
		if f.zone != TtZoneOpen {
			return ttToward(f, TtZoneOpen)
		}
		return ttGateIn(f)

	case TtEnteredCmp, TtSpokenSlv, TtFreedSlave:
		if f.zone != TtZoneCamp {
			return ttToward(f, TtZoneCamp)
		}
		if f.desertSets == 0 {
			return ttRetrade(f)
		}
		if !f.clothesOnly {
			return ttDressCamp(f, "nothing but clothes in the camp: the guards search a weapon or armour")
		}
		return ttCustom(CustomTtSlave, 0, "free the slave (a Thieving roll a try) and trade the clothes")

	case TtTraded, TtEnteredMin:
		if f.zone != TtZoneHall {
			return ttToward(f, TtZoneHall)
		}
		return ttTalk(TtGuardStop, "learn that the cave guard wants a Tenti pineapple")

	case TtFindingPin:
		if f.zone != TtZoneOpen {
			return ttToward(f, TtZoneOpen)
		}
		return ttTalkUntil(TtAlDealStop, ObjTtBedobinKey, "Al Shabim's pineapple deal: the key to Siad's chest")

	case TtBedobinKey:
		if !f.hasBedKey {
			return ttAlKey(f)
		}
		if f.zone != TtZoneCamp {
			return ttToward(f, TtZoneCamp)
		}
		if !f.clothesOnly {
			return ttDressCamp(f, "nothing but clothes in the camp")
		}
		return ttCustom(CustomTtPlans, 0, "distract Captain Siad and open his chest: the technical plans")

	case TtGotPlans:
		if !f.hasPlans {
			return ttPlansLost(f)
		}
		if f.zone != TtZoneOpen {
			return ttToward(f, TtZoneOpen)
		}
		return ttCustom(CustomTtShowPlans, 0, "show the plans to Al Shabim: a bar, ten feathers and a hammer")

	case TtShownPlans, TtMadeTip, TtMadeDart:
		return ttDartWork(f)

	case TtLearned:
		if !f.hasPineapple {
			if f.zone != TtZoneOpen {
				return ttToward(f, TtZoneOpen)
			}
			return ttTalkUntil(TtAlPineappleStop, ObjTtPineapple, "Al Shabim's second pineapple")
		}
		if f.zone != TtZoneHall {
			return ttToward(f, TtZoneHall)
		}
		return ttCustom(CustomTtPineapple, 0, "the pineapple to the cave guard")

	case TtGivenPine:
		return ttLowerWork(f)

	case TtUsedCart:
		return ttDeepWork(f)

	case TtRescue:
		return ttRescue(f)

	case TtReward:
		return ttReward(f)
	}
	return ttWait(0, "unrecognised Tourist Trap stage %d", f.stage)
}

// ttAlKey is Al Shabim's replacement of a lost key to Siad's chest.
func ttAlKey(f ttFacts) Step {
	if f.zone != TtZoneOpen {
		return ttToward(f, TtZoneOpen)
	}
	return ttTalkUntil(TtAlKeyStop, ObjTtBedobinKey, "Al Shabim's replacement for the lost key")
}

// ttPlansLost is the plans lost: the chest's copy, with Al Shabim's key.
func ttPlansLost(f ttFacts) Step {
	if !f.hasBedKey {
		return ttAlKey(f)
	}
	if f.zone != TtZoneCamp {
		return ttToward(f, TtZoneCamp)
	}
	if !f.clothesOnly {
		return ttDressCamp(f, "nothing but clothes in the camp")
	}
	return ttCustom(CustomTtPlans, 0, "the technical plans again")
}

// ttDartWork is stages 13 to 15, in one: the dart to Al Shabim when it is made, the feathers on the tip when it is forged, the tent and the anvil when
// neither is (a tip or a dart lost is made again from a bar: the anvil does not look at the stage).
func ttDartWork(f ttFacts) Step {
	switch {
	case f.hasDart:
		if f.zone != TtZoneOpen {
			return ttToward(f, TtZoneOpen)
		}
		return ttCustom(CustomTtDart, 0, "out of the tent and the dart to Al Shabim: the Tenti pineapple")
	case f.hasTip:
		if f.zone != TtZoneOpen {
			return ttToward(f, TtZoneOpen)
		}
		return ttCustom(CustomTtFletch, 0, "ten feathers on the tip: a prototype dart (Fletching 10)")
	case !f.hasPlans:
		return ttPlansLost(f)
	case f.zone != TtZoneOpen:
		return ttToward(f, TtZoneOpen)
	}
	return ttCustom(CustomTtTent, 0, "the tent's guard, the anvil: a prototype dart tip")
}

// ttLowerWork is stage 17: the pineapple is given; the account goes through the cave to the lower mine, takes an empty barrel and rides the cart.
func ttLowerWork(f ttFacts) Step {
	switch f.zone {
	case TtZoneDeep:
		return ttDeepWork(f)
	case TtZoneLower:
		if !f.hasBarrel {
			return ttCustom(CustomTtBarrel, 1, "an empty barrel from the lower mine")
		}
		return ttCustom(CustomTtCart, 1, "the mine cart to the deep mine, with the empty barrel")
	}
	return ttToward(f, TtZoneLower)
}

// ttDeepWork is stage 18: the deep mine's Ana, caught in an empty barrel.
func ttDeepWork(f ttFacts) Step {
	switch f.zone {
	case TtZoneDeep:
		switch {
		case f.hasBarrel:
			return ttCustom(CustomTtCatch, 0, "the empty barrel on Ana")
		case f.hasAna:
			return ttCustom(CustomTtDeepCheck, 0, "Ana into the deep cart and up to the lower mine")
		}
		return ttCustom(CustomTtCart, 0, "back to the lower mine for an empty barrel")
	case TtZoneLower:
		return ttLowerWork(f)
	}
	return ttToward(f, TtZoneLower)
}

// ttRescue is stages 19 to 26: Ana is in a barrel somewhere between the deep mine and the cart; the journal cannot say where, so the account looks.
func ttRescue(f ttFacts) Step {
	switch f.zone {
	case TtZoneNorth:
		return ttCrossSouth(f)
	case TtZoneOpen:
		if f.hasAna {
			return ttTalk(TtIrenaRewardStop, "bring Ana in her barrel to Irena")
		}
		return ttGateIn(f)
	case TtZoneDeep:
		if f.hasBarrel {
			return ttCustom(CustomTtCatch, 0, "catch Ana again: the barrel on her resets every stale bit")
		}
		return ttCustom(CustomTtDeepCheck, 0, "Ana into the deep cart, the ride up and the barrels")
	case TtZoneLower:
		switch {
		case f.hasAna:
			return ttCustom(CustomTtLift, 0, "Ana on the lift")
		case f.hasBarrel:
			return ttCustom(CustomTtCart, 1, "retry the lower mine cart with the empty barrel")
		case f.s.Failed[CustomTtBarrel] || f.s.Ran[CustomTtLift]:
			// The barrels held no Ana, or she is on the lift already (the probe found her and the lift took her): out by the cave to the winch.
			return ttDown(f)
		}
		return ttCustom(CustomTtBarrel, 0, "look for Ana in the lower mine's barrels")
	case TtZoneHall:
		return ttDown(f)
	case TtZoneCamp:
		return ttRescueCamp(f)
	}
	return ttToward(f, TtZoneCamp)
}

// ttRescueCamp is the rescue's camp yard: Ana in the pack goes out on the cart; without her the winch brings her up, and a winch that came up empty
// (or a cart that took her barrel and did not go) is answered by the replay, which catches her again and ends with the winch itself (questmods
// ttRecaptureStep), so that its success leaves her in the pack. A failure flag lasts the whole run (the engine clears it only when the same step later
// succeeds), so a replay that succeeds forgets the winch's and the cart's flags (Engine.ForgetFailure): what fails after it is a new failure, and a
// replay that has run (Ran) with the winch or the cart failing again, a replay that failed, or the cart failing with Ana still in the pack, parks the
// quest with the reason, not a loop through the mines.
func ttRescueCamp(f ttFacts) Step {
	s := f.s
	switch {
	case f.hasAna && !s.Failed[CustomTtEscape]:
		return ttCustom(CustomTtEscape, 0, "Ana on the cart, the driver and the ride")
	case f.hasAna:
		return ttWait(TtItemPark, "the cart out of the camp failed with Ana in the pack: the driver, the barrel or the ride did not work")
	case s.Failed[CustomTtRecapture]:
		return ttWait(TtItemPark, "the replay that catches Ana again and brings her up the winch failed")
	case s.Ran[CustomTtRecapture] && (s.Failed[CustomTtWinch] || s.Failed[CustomTtEscape]):
		return ttWait(TtItemPark, "Ana is lost again after the replay (the winch or the cart failed once more)")
	case f.hasBarrel || s.Failed[CustomTtWinch] || s.Failed[CustomTtEscape]:
		return ttCustom(CustomTtRecapture, 0, "the canonical replay: catch Ana again and bring her up the winch")
	}
	return ttCustom(CustomTtWinch, 0, "the winch and the full barrel at the top")
}

// ttReward is stage 27: Irena's two menus.
func ttReward(f ttFacts) Step {
	if f.zone != TtZoneOpen {
		return ttToward(f, TtZoneOpen)
	}
	return ttTalk(TtIrenaRewardStop, "claim both Tourist Trap skill rewards")
}

// ---------------------------------------------------------------------------------------------------------------
// The record and the module.
// ---------------------------------------------------------------------------------------------------------------

// TouristTrapRecord is the quest's identity. The server gates the dart's two steps on Smithing 20 (the anvil) and Fletching 10 (the feathers);
// the journal's list of requirements is the same two. The record states them, so the ladder never starts the quest below them.
var TouristTrapRecord = Record{ID: TouristTrapID, Name: TouristTrapName, QuestPoints: 2,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "smithing", Level: TtSmithing}, {Skill: "fletching", Level: TtFletching}}}}

// TouristTrap is the engine module. One death skips the quest for a day: the desert's items stay where the account died. An attacker that keeps a
// step from finishing is fought by the engine (the Rowdy Guards, the desert's wolves), and the account eats at 15.
var TouristTrap = Module{
	Record: TouristTrapRecord, Bank: AlKharidBank, BankSet: true, Anchor: TtIrena,
	ToolIDs:   ttTools,
	ReadStage: ParseTouristTrapJournal,
	Sustain:   Sustain{FoodIDs: ttFoodIDs, EatAt: 15},
	MaxDeaths: 1,
	Decide:    DecideTouristTrap,
}
