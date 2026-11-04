package questplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Horror from the Deep (content id `horror`, varp 351 `deephorror`, not transmitted), from the
// lane Q study (private evidence notes) read against content 289's
// quests/quest_horror. The journal (horror_journal.rs2) shows the stage and, at stage 1, whether
// both bridge halves are fixed (the line turns @str@) and whether Gunnjorn's key was handed over;
// at stage 2 it shows the lens (glass) and torch (tar) flags. This module packs those into the
// stage number: stage + 100*bridge + 200*key + 400*glass + 800*tar + 1600*lit.
//
// The route, all scripted teleports between four pockets nothing walks between:
//   - stage 1: Gunnjorn's key (the Barbarian course pipe, Agility 35, behind the Barcrawl gate),
//     a plank with 4 nails and a hammer on each half of the broken bridge (2596/2598,3608), then
//     Walk-through on the lighthouse doorway twice (unlock, then enter: stage 2, quest copy);
//   - stage 2: in the quest copy, swamp tar, then a tinderbox, then molten glass on the lighting
//     mechanism at the top (stage 4, and a teleport to the real top floor);
//   - stage 4: back into the quest copy by the top stairs, down, the iron ladder to the
//     basement, the six wall slots (fire/water/earth/air rune, a stab/slash weapon, an arrow),
//     Open the far-right wall, the ladder down to the cavern, Jossik summons the junior;
//   - stage 5: Jossik summons the mother, who takes damage only from the element (or style) her
//     current colour names and switches every 30 ticks; her ranged volley (0-24, no accuracy
//     roll) is zeroed only by Protect from Missiles, so the module waits for Prayer 40.

const HorrorName = "Horror from the Deep"

const HorrorID = "horror"

const (
	HorrorStarted  = 1
	HorrorEntered  = 2
	HorrorRepaired = 4
	HorrorJunior   = 5
	HorrorComplete = 10
)

const (
	NpcLarrissa      = 1336
	NpcGunnjorn      = 607
	NpcJossikInjured = 1335
	NpcHorrorJunior  = 1347
	// The mother's six forms: white/air, blue/water, red/fire, brown/earth, green/ranged,
	// orange/melee (quest_horror.npc).
	NpcMotherAir    = 1351
	NpcMotherWater  = 1352
	NpcMotherFire   = 1353
	NpcMotherEarth  = 1354
	NpcMotherRanged = 1355
	NpcMotherMelee  = 1356

	ObjLighthouseKey = 3848
	ObjRustyCasket   = 3849
	ObjSwampTar      = 1939
	ObjMoltenGlass   = 1775
	ObjBucketOfSand  = 1783
	ObjSodaAsh       = 1781
	ObjSeaweed       = 401
	ObjBronzeDagger  = 1205
	ObjBronzeArrow   = 882
	ObjFireRune      = 554
	ObjWaterRune     = 555
	ObjAirRune       = 556
	ObjEarthRune     = 557
	ObjMindRune      = 558
	ObjChaosRune     = 562
	ObjStaffOfAir    = 1381
	ObjShark         = 385

	LocBridgeLeft      = 4615
	LocBridgeRight     = 4616
	LocHorrorDoorway   = 4577
	LocHorrorStairBase = 4568
	LocHorrorStairMid  = 4569
	LocHorrorStairTop  = 4570
	LocLightMechanism  = 4588
	LocHorrorLadder    = 4383
	LocWallMidLeft     = 4543
	LocWallFarRight    = 4545
	LocCavernLadder    = 4485
	LocCavernLadderUp  = 4413
	LocHorrorExitUp    = 4412
)

var (
	LarrissaAnchor = tile.Tile{X: 2508, Z: 3635}
	GunnjornAnchor = tile.Tile{X: 2540, Z: 3548}
	// BridgeLeftTile is the west (lighthouse-side) half, BridgeRightTile the east half; the
	// span between is 2597,3608. BridgeEast is the Rellekka-side bank.
	BridgeLeftTile   = tile.Tile{X: 2596, Z: 3608}
	BridgeRightTile  = tile.Tile{X: 2598, Z: 3608}
	BridgeWest       = tile.Tile{X: 2595, Z: 3608}
	BridgeEast       = tile.Tile{X: 2599, Z: 3608}
	HorrorDoorway    = tile.Tile{X: 2509, Z: 3636}
	HorrorDoorStand  = tile.Tile{X: 2509, Z: 3635}
	CopyStairBase    = tile.Tile{X: 2442, Z: 4600}
	CopyStairMid     = tile.Tile{X: 2442, Z: 4600, Level: 1}
	LightMechanism   = tile.Tile{X: 2443, Z: 4599, Level: 2}
	RealStairTop     = tile.Tile{X: 2506, Z: 3641, Level: 2}
	CopyLadder       = tile.Tile{X: 2445, Z: 4604}
	WallMidLeft      = tile.Tile{X: 2514, Z: 4627, Level: 1}
	WallFarRight     = tile.Tile{X: 2516, Z: 4627, Level: 1}
	WallSouth        = tile.Tile{X: 2515, Z: 4626, Level: 1}
	CavernLadderTop  = tile.Tile{X: 2515, Z: 4630, Level: 1}
	CavernLanding    = tile.Tile{X: 2515, Z: 4632}
	CavernLadderUp   = tile.Tile{X: 2515, Z: 4631}
	JossikAnchor     = tile.Tile{X: 2518, Z: 4633}
	PostQuestLadder  = tile.Tile{X: 2519, Z: 9994, Level: 1}
	LumbridgeSwamp   = tile.Tile{X: 3173, Z: 3178}
	OutpostPlanks    = tile.Tile{X: 2552, Z: 3574}
	SeersRange       = tile.Tile{X: 2715, Z: 3476}
	ArdougneFurnace  = tile.Tile{X: 2602, Z: 3311}
	horrorFoods      = []int{385, 373, 379, 365, 361, 329, 333}
	horrorWallItems  = []int{ObjFireRune, ObjWaterRune, ObjEarthRune, ObjAirRune, ObjBronzeDagger, ObjBronzeArrow}
	horrorFightRunes = []int{ObjChaosRune, ObjMindRune, ObjWaterRune, ObjEarthRune, ObjFireRune, ObjAirRune}
	horrorTools      = append(append([]int{CoinsID, ObjLighthouseKey, ObjRustyCasket, 960, 1539, 2347, 2353, ObjSwampTar, ObjMoltenGlass,
		ObjTinderbox, ObjBucketOfSand, ObjSodaAsh, ObjSeaweed, ObjBronzeDagger, ObjBronzeArrow, ObjStaffOfAir}, horrorFightRunes...), Axes...)
)

// MotherForms are the mother's ids.
var MotherForms = []int{NpcMotherAir, NpcMotherWater, NpcMotherFire, NpcMotherEarth, NpcMotherRanged, NpcMotherMelee}

// MotherElement names the spell element that damages a mother form ("" for the ranged and
// melee forms, which a caster waits out).
func MotherElement(id int) string {
	switch id {
	case NpcMotherAir:
		return "Wind"
	case NpcMotherWater:
		return "Water"
	case NpcMotherEarth:
		return "Earth"
	case NpcMotherFire:
		return "Fire"
	}
	return ""
}

// HorrorFacts is the decoded packed stage.
type HorrorFacts struct {
	Stage                        int
	Bridge, Key, Glass, Tar, Lit bool
}

func DecodeHorror(n int) HorrorFacts {
	return HorrorFacts{Stage: n % 100, Bridge: n/100%2 == 1, Key: n/200%2 == 1, Glass: n/400%2 == 1, Tar: n/800%2 == 1, Lit: n/1600%2 == 1}
}

func (f HorrorFacts) Encode() int {
	n := f.Stage
	for _, b := range []struct {
		on bool
		v  int
	}{{f.Bridge, 100}, {f.Key, 200}, {f.Glass, 400}, {f.Tar, 800}, {f.Lit, 1600}} {
		if b.on {
			n += b.v
		}
	}
	return n
}

// ParseHorrorJournal reads the packed stage. The bridge flag needs the raw colour tags: both the
// open and the done line say "repair the bridge", the open one with @dre@ on those words.
func ParseHorrorJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	raw := strings.ToLower(strings.Join(lines, " "))
	var f HorrorFacts
	switch {
	case text == "":
		return 0, false
	case strings.Contains(text, "quest complete!"):
		return HorrorComplete, true
	case strings.Contains(text, "i can start this quest by speaking to larrissa"):
		return 0, true
	case strings.Contains(text, "i must defeat these sea monsters"):
		f.Stage = HorrorJunior
	case strings.Contains(text, "i must search this place"):
		f.Stage = HorrorRepaired
	case strings.Contains(text, "fixing the lighthouse lamp"), strings.Contains(text, "i have fixed the lighthouse lens"),
		strings.Contains(text, "i have re-tarred the lighthouse torch"), strings.Contains(text, "i have relit the lighthouse torch"):
		f.Stage = HorrorEntered
		f.Glass = strings.Contains(text, "i have fixed the lighthouse lens")
		f.Tar = strings.Contains(text, "i have re-tarred the lighthouse torch")
		f.Lit = strings.Contains(text, "i have relit the lighthouse torch")
	case strings.Contains(text, "larrissa is very worried"):
		f.Stage = HorrorStarted
		f.Bridge = strings.Contains(text, "repair the bridge") && !strings.Contains(raw, "@dre@repair the bridge")
		f.Key = strings.Contains(text, "use the key i got from gunnjorn")
	default:
		return 0, false
	}
	if f.Stage > HorrorStarted {
		f.Bridge, f.Key = true, true
	}
	return f.Encode(), true
}

// The pockets.
func InHorrorQuestCopy(t tile.Tile) bool {
	return t.X >= 2432 && t.X <= 2495 && t.Z >= 4544 && t.Z <= 4607
}
func InHorrorBasement(t tile.Tile) bool {
	return t.Level == 1 && t.X >= 2496 && t.X <= 2559 && t.Z >= 4608 && t.Z <= 4671
}

// InHorrorBasementNorth is the basement north of the strange wall (the ladder down side).
func InHorrorBasementNorth(t tile.Tile) bool { return InHorrorBasement(t) && t.Z >= WallFarRight.Z }
func InHorrorCavern(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2496 && t.X <= 2559 && t.Z >= 4608 && t.Z <= 4671
}
func InHorrorPostPocket(t tile.Tile) bool {
	return t.X >= 2496 && t.X <= 2559 && t.Z >= 9984 && t.Z <= 10047
}

// InRealLighthouseUpstairs is the real lighthouse's upper floors, where the repair teleport lands.
func InRealLighthouseUpstairs(t tile.Tile) bool {
	return t.Level >= 1 && t.X >= 2500 && t.X <= 2515 && t.Z >= 3636 && t.Z <= 3648
}

// inHorrorPocket is any pocket a walk cannot leave by walking.
func inHorrorPocket(t tile.Tile) bool {
	return InHorrorQuestCopy(t) || InHorrorBasement(t) || InHorrorCavern(t) || InHorrorPostPocket(t) || InRealLighthouseUpstairs(t)
}

const (
	CustomHorrorBridge = "horror-bridge"
	CustomHorrorDoor   = "horror-door"
	CustomHorrorLight  = "horror-light"
	CustomHorrorDown   = "horror-down"
	CustomHorrorJunior = "horror-junior"
	CustomHorrorMother = "horror-mother"
	CustomHorrorLeave  = "horror-leave"
	KitHorrorBridge    = "horror-bridge-kit"
	KitHorrorLight     = "horror-light-kit"
	KitHorrorFight     = "horror-fight-kit"
	MethodMakeGlass    = "make-glass"
)

var (
	LarrissaStop = NpcStop{Npc: "Larrissa", ID: NpcLarrissa, Anchor: LarrissaAnchor, Leash: 6, Prefer: []string{
		"With what?", "But how can I help?", "Okay, I'll help!", "I'll see what I can do"}}
	// GunnjornStop's talk has no menu; the preference only guards a menu appearing anyway.
	GunnjornStop = NpcStop{Npc: "Gunnjorn", ID: NpcGunnjorn, Anchor: GunnjornAnchor, Leash: 8, Prefer: []string{"Larrissa"}}
)

// horrorBridgeKit is two planks with their eight nails (and the steel bars to smith them) and a
// hammer: bank, the ONE bundled mule ask, the outpost planks, then smithing.
func horrorBridgeKit(s Snapshot) []KitLine {
	return append([]KitLine{
		{ID: 960, Name: "Plank", Qty: 2, Sources: []Source{
			{Kind: SourceGround, Tile: OutpostPlanks, RespawnTicks: 100},
			{Kind: SourceGround, Tile: tile.Tile{X: 2553, Z: 3575}, RespawnTicks: 100},
		}},
		{ID: ObjNails, Name: "Nails", Qty: 8, Stackable: true, Sources: []Source{
			{Kind: SourceSkill, Skill: "smithing", Level: 34, Method: MethodSmithNails},
		}},
		{ID: ObjHammer, Name: "Hammer", Qty: 1},
	}, NailBars(8, s.HeldID(ObjNails), s.HeldID(ObjSteelBar))...)
}

// horrorLightKit is the tar (the Lumbridge swamp spawn), the glass (bank, mule, or made from
// the mule's seaweed and sand) and a tinderbox.
func horrorLightKit(s Snapshot) []KitLine {
	kit := []KitLine{
		{ID: ObjSwampTar, Name: "Swamp tar", Qty: 1, Stackable: true, Sources: []Source{{Kind: SourceGround, Tile: LumbridgeSwamp, RespawnTicks: 100}}},
		{ID: ObjTinderbox, Name: "Tinderbox", Qty: 1},
		{ID: ObjMoltenGlass, Name: "Molten glass", Qty: 1, Sources: []Source{{Kind: SourceSkill, Skill: "crafting", Level: 1, Method: MethodMakeGlass, Tile: ArdougneFurnace}}},
	}
	if s.HeldID(ObjMoltenGlass) == 0 {
		if s.HeldID(ObjBucketOfSand) == 0 {
			kit = append(kit, KitLine{ID: ObjBucketOfSand, Name: "Bucket of sand", Qty: 1, Optional: true})
		}
		if s.HeldID(ObjSodaAsh) == 0 && s.HeldID(ObjSeaweed) == 0 {
			kit = append(kit, KitLine{ID: ObjSeaweed, Name: "Seaweed", Qty: 1, Optional: true})
		}
	}
	return kit
}

// horrorFightKit is the caster's kit: runes for the junior's and the mother's bolts (and the
// wall's four), a staff of air, food, and at stage 4 the wall's weapon and arrow.
func horrorFightKit(stage int) []KitLine {
	kit := []KitLine{
		{ID: ObjStaffOfAir, Name: "Staff of air", Qty: 1, Worn: true},
		{ID: ObjChaosRune, Name: "Chaos rune", Qty: 120, Stackable: true},
		{ID: ObjMindRune, Name: "Mind rune", Qty: 60, Stackable: true},
		{ID: ObjWaterRune, Name: "Water rune", Qty: 80, Stackable: true},
		{ID: ObjEarthRune, Name: "Earth rune", Qty: 80, Stackable: true},
		{ID: ObjFireRune, Name: "Fire rune", Qty: 100, Stackable: true},
		{ID: ObjAirRune, Name: "Air rune", Qty: 10, Stackable: true},
		{ID: ObjShark, Name: "Shark", Qty: 16},
	}
	if stage < HorrorJunior {
		kit = append(kit,
			KitLine{ID: ObjBronzeDagger, Name: "Bronze dagger", Qty: 1},
			KitLine{ID: ObjBronzeArrow, Name: "Bronze arrow", Qty: 1, Stackable: true})
	}
	return kit
}

// HorrorMotherPrayer is the Prayer level the mother fight needs: Protect from Missiles.
const HorrorMotherPrayer = 40

func DecideHorror(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		if s.TileKnown && InHorrorPostPocket(s.Tile) {
			return Step{Kind: StepCustom, Custom: CustomHorrorLeave, Reason: "up the post-quest ladder"}
		}
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: LarrissaStop}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	f := DecodeHorror(s.Stage)
	keep := append(append([]int(nil), horrorTools...), horrorFoods...)
	pocket := s.TileKnown && inHorrorPocket(s.Tile)
	if s.TileKnown && InHorrorPostPocket(s.Tile) {
		return Step{Kind: StepCustom, Custom: CustomHorrorLeave, Reason: "up the post-quest ladder"}
	}
	switch {
	case f.Stage <= HorrorStarted:
		if !f.Key && s.HeldID(ObjLighthouseKey) == 0 {
			if s.Level("agility") < 35 {
				return Step{Kind: StepWait, Reason: "Gunnjorn is past the Barbarian course pipe (Agility 35)"}
			}
			return Step{Kind: StepTalk, Stop: GunnjornStop}
		}
		if !f.Bridge {
			if s.HeldID(960) < 2 || s.HeldID(ObjNails) < 8 || s.HeldID(ObjHammer) == 0 {
				if s.Failed["provision:"+KitHorrorBridge] {
					return Step{Kind: StepWait, Reason: "the bridge kit could not be filled (nails need Smithing 34, steel bars or the bank)"}
				}
				return Step{Kind: StepProvision, KitName: KitHorrorBridge, Kit: horrorBridgeKit(s), KeepIDs: keep,
					Bank: SeersBank, BankSet: true, Reason: "two planks, eight nails and a hammer for the bridge"}
			}
			return Step{Kind: StepCustom, Custom: CustomHorrorBridge, Reason: "a plank on each half of the bridge"}
		}
		return Step{Kind: StepCustom, Custom: CustomHorrorDoor, Reason: "unlock and enter the lighthouse"}
	case f.Stage < HorrorRepaired:
		short := (!f.Tar && s.HeldID(ObjSwampTar) == 0) || s.HeldID(ObjTinderbox) == 0 || (!f.Glass && s.HeldID(ObjMoltenGlass) == 0)
		if short && !pocket {
			if s.Failed["provision:"+KitHorrorLight] {
				return Step{Kind: StepWait, Reason: "no swamp tar, molten glass or tinderbox (bank, mule, swamp, furnace)"}
			}
			return Step{Kind: StepProvision, KitName: KitHorrorLight, Kit: horrorLightKit(s), KeepIDs: keep,
				Bank: SeersBank, BankSet: true, Reason: "tar, glass and a tinderbox for the light"}
		}
		if short {
			return Step{Kind: StepCustom, Custom: CustomHorrorLeave, Reason: "out of the lighthouse for the light kit"}
		}
		return Step{Kind: StepCustom, Custom: CustomHorrorLight, Reason: "tar, tinderbox and glass on the lighting mechanism"}
	}
	// Stage 4 (the junior) and 5 (the mother).
	inCavern := s.TileKnown && InHorrorCavern(s.Tile)
	if f.Stage >= HorrorJunior && s.Level("prayer") < HorrorMotherPrayer && !inCavern {
		return Step{Kind: StepWait, Reason: "the dagannoth mother needs Protect from Missiles (Prayer 40)"}
	}
	if !pocket && !s.Ran["provision:"+KitHorrorFight] && !s.Failed["provision:"+KitHorrorFight] {
		return Step{Kind: StepProvision, KitName: KitHorrorFight, Kit: horrorFightKit(f.Stage), KeepIDs: keep,
			Bank: SeersBank, BankSet: true, Reason: "runes, a staff, food and the wall's six for the cavern"}
	}
	if s.WornIDs[ObjStaffOfAir] == 0 && s.HeldID(ObjStaffOfAir) > 0 {
		return Step{Kind: StepEquip, ItemID: ObjStaffOfAir, Item: "Staff of air"}
	}
	if !inCavern {
		return Step{Kind: StepCustom, Custom: CustomHorrorDown, Reason: "through the lighthouse, the strange wall and down to the cavern"}
	}
	if f.Stage == HorrorRepaired {
		return Step{Kind: StepCustom, Custom: CustomHorrorJunior, Reason: "Jossik summons the junior dagannoth"}
	}
	return Step{Kind: StepCustom, Custom: CustomHorrorMother, Reason: "Jossik summons the dagannoth mother"}
}

var HorrorRecord = Record{ID: HorrorID, Name: HorrorName, QuestPoints: 2}

var Horror = Module{
	Record: HorrorRecord, Bank: SeersBank, BankSet: true, Anchor: LarrissaAnchor,
	ToolIDs:   horrorTools,
	ReadStage: ParseHorrorJournal,
	Sustain:   Sustain{FoodIDs: horrorFoods, EatAt: 20},
	MaxDeaths: 3,
	Decide:    DecideHorror,
}
