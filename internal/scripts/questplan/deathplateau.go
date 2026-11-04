package questplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Death Plateau (content id `death`; the quest varp is %death_equiproom, not %death, and with
// %death_map and %death_bits none is transmitted), from the lane Q study
// (private evidence notes) and death_journal.rs2. Two tracks run
// side by side: the equipment-room track (Denulth, Eohric, Harold's ale and dice, the IOU, the
// stone balls) and the map track (Saba, Tenzing, Dunstan, the certificate, the spiked boots, the
// supplies, the secret way). The journal renders both, so the stage this module reads packs
// them into one number: equip + 1000*map.
//
// Every re-issue in this quest is guarded by content's obj_gettotal, which counts the pack, the
// bank AND what is worn: Harold gives the IOU or the combination again, Tenzing the climbing boots
// or the secret way map, Denulth the certificate, and Dunstan will not make a second pair of
// spiked boots, only to an account that owns none of the item anywhere. A deposit-all from any
// other quest (or a hand-over to the mule) puts these items in the bank, and a module that looks
// only at the pack then asks an NPC for what the NPC will never give: 24 of the 41 accounts that
// had run this quest by 16 Oct 2026 held the combination in the bank, 11 held the spiked boots
// there, and the mule held 289 pairs. The decision therefore reads the bank (deathFetch) before it
// asks anyone for an item again.

// DeathPlateauName is the quest tab's spelling (component 8438).
const DeathPlateauName = "Death Plateau"

// DeathPlateauID is the engine's id: the ladder's rung id (content's own is `death`).
const DeathPlateauID = "deathplateau"

// Equipment-room track (%death_equiproom).
const (
	DeathStarted   = 10
	DeathEohric    = 20
	DeathHarold    = 30
	DeathEohric2   = 40
	DeathAle       = 50
	DeathIOU       = 55
	DeathCombo     = 60
	DeathUnlocked  = 70
	DeathComplete  = 80
	deathMapFactor = 1000
)

// Map track (~death_get_map).
const (
	MapSaba     = 1
	MapTenzing  = 2
	MapSmithy   = 3
	MapCert     = 4
	MapGaveCert = 5
	MapSupplies = 6
	MapGotMap   = 7
	MapScouted  = 8
)

// DeathStage packs the two tracks; DeathTracks unpacks them.
func DeathStage(equip, mapTrack int) int { return equip + deathMapFactor*mapTrack }

// DeathTracks is the equipment-room stage and the map stage of a packed stage.
func DeathTracks(stage int) (equip, mapTrack int) {
	return stage % deathMapFactor, stage / deathMapFactor
}

// Ids from content/pack.
const (
	NpcDenulth       = 1060
	NpcEohric        = 1080
	NpcHarold        = 1078
	NpcSaba          = 1070
	NpcTenzing       = 1071
	NpcDunstan       = 1082
	ObjAsgarnianAle  = 1905
	ObjDeathIOU      = 3103
	ObjCombination   = 3102
	ObjSecretMap     = 3104
	ObjClimbingBoots = 3105
	ObjSpikedBoots   = 3107
	ObjCertificate   = 3114
	ObjIronBar       = 2351
	ObjTrout         = 333
	LocHaroldDoor    = 3747
	LocSabaEntrance  = 3735
	LocSabaExit      = 3736
	LocTenzingDoor   = 3745
	LocTenzingBack   = 3746
	LocStile         = 3730
	// DeathDiceMain is the death_dice interface; the Roll Dice! button sits in a layer the
	// script hides and unhides, and the Continue (pause) button in another.
	DeathDiceMain     = 6675
	DeathDiceRollLyr  = 8420 // com_27
	DeathDiceRoll     = 8421 // com_28
	DeathDiceContLyr  = 8422 // com_29
	DeathDiceContinue = 8423 // com_30
	DeathDiceVerdict  = 8426 // com_33
	// HaroldPurseStart is what Denulth's start puts in Harold's purse; MaxDeathBet is his cap.
	HaroldPurseStart = 100
	MaxDeathBet      = 1000
)

// StoneBall is one coloured ball: where it spawns and which mechanism tile it belongs on.
type StoneBall struct {
	ID            int
	Colour        string
	Spawn, Target tile.Tile
}

// StoneBalls are quest_death.rs2's required layout (the ball ids share one name).
var StoneBalls = []StoneBall{
	{3110, "blue", tile.Tile{X: 2893, Z: 3564}, tile.Tile{X: 2894, Z: 3562}},
	{3111, "yellow", tile.Tile{X: 2893, Z: 3561}, tile.Tile{X: 2895, Z: 3562}},
	{3109, "red", tile.Tile{X: 2893, Z: 3565}, tile.Tile{X: 2894, Z: 3563}},
	{3112, "purple", tile.Tile{X: 2893, Z: 3563}, tile.Tile{X: 2895, Z: 3563}},
	{3113, "green", tile.Tile{X: 2893, Z: 3562}, tile.Tile{X: 2895, Z: 3564}},
}

// Tiles (TS areas.ts, checked against the study).
var (
	DenulthAnchor   = tile.Tile{X: 2896, Z: 3528}
	EohricAnchor    = tile.Tile{X: 2902, Z: 3565, Level: 1}
	HaroldAnchor    = tile.Tile{X: 2905, Z: 3539, Level: 1}
	HaroldDoorStand = tile.Tile{X: 2906, Z: 3543, Level: 1}
	TostigAnchor    = tile.Tile{X: 2906, Z: 3537}
	AleSpawn        = tile.Tile{X: 2906, Z: 3538}
	DunstanAnchor   = tile.Tile{X: 2919, Z: 3574}
	TenzingAnchor   = tile.Tile{X: 2820, Z: 3556}
	TenzingDoor     = tile.Tile{X: 2823, Z: 3555}
	TenzingBack     = tile.Tile{X: 2820, Z: 3558}
	StileTile       = tile.Tile{X: 2817, Z: 3562}
	SabaEntrance    = tile.Tile{X: 2858, Z: 3577}
	SabaInside      = tile.Tile{X: 2270, Z: 4759}
	SabaExit        = tile.Tile{X: 2269, Z: 4752}
	ScoutTile       = tile.Tile{X: 2864, Z: 3608}
	deathFoods      = []int{385, 373, 379, 365, 361, 329}
	deathTools      = []int{CoinsID, ObjAsgarnianAle, ObjDeathIOU, ObjCombination, ObjSecretMap, ObjClimbingBoots,
		ObjSpikedBoots, ObjCertificate, ObjIronBar, 2309, ObjTrout, 3109, 3110, 3111, 3112, 3113}
)

// InHaroldRoom is Harold's L-shaped bedroom (TS harold.ts).
func InHaroldRoom(t tile.Tile) bool {
	if t.Level != 1 || t.X < 2905 || t.Z < 3536 || t.Z > 3542 {
		return false
	}
	if t.Z >= 3540 {
		return t.X <= 2907
	}
	return t.X <= 2906
}

// InSabaCave is Saba's cave instance.
func InSabaCave(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2255 && t.X <= 2285 && t.Z >= 4740 && t.Z <= 4775
}

// Custom step names (internal/scripts/questmods).
const (
	CustomDeathHaroldTalk = "death-harold-talk"
	CustomDeathAle        = "death-ale"
	CustomDeathDice       = "death-dice"
	CustomDeathReclaim    = "death-reclaim"
	CustomDeathReadIOU    = "death-read-iou"
	CustomDeathBalls      = "death-balls"
	CustomDeathSaba       = "death-saba"
	CustomDeathTenzing    = "death-tenzing"
	CustomDeathScout      = "death-scout"
	KitDeathAle           = "death-ale-kit"
	KitDeathDice          = "death-dice-kit"
	// KitDeathBoots is one mule ask for the spiked boots: a spare pair from the bank or the mule (it held
	// 289 of them on 16 Oct 2026), or failing that the iron bar Dunstan forges a pair from. The bar is
	// an optional line, so it rides the same ask, and an account whose pair arrives pays one walk to the
	// mule, not two (a kit of the bar alone, asked after a kit of the boots alone, was two).
	KitDeathBoots = "death-boots-kit"
	// KitDeathTrout and KitDeathBread are Tenzing's supplies, one kit each (a single two-line kit
	// walked the account to the Baker for bread while the trout line, which nothing in the fleet can
	// source, was already blocked).
	KitDeathTrout = "death-trout-kit"
	KitDeathBread = "death-bread-kit"
)

// deathSupplyQty is what Tenzing asks for of each: ten loaves of bread and ten cooked trout.
const deathSupplyQty = 10

// How long the quest is skipped when a step is a dead end the account cannot get past by itself.
// A watchdog park earns the escalating Backoff (5 to 40 minutes, then 2 hours); these are the
// engine's ParkFor, the quest skipped for that long while the ladder moves on.
const (
	// DeathTroutRetry: ten cooked trout have no source. The mule holds none and publishes none, no bank
	// in the fleet held one on 16 Oct 2026, nothing the fleet runs catches or cooks trout (fly fishing
	// needs Fishing 20, a fly rod and feathers, then Cooking 15 and a fire), and the accounts that
	// run this quest are the low-skill ones. A day, so a trout supply is noticed within one.
	DeathTroutRetry = 24 * time.Hour
	// DeathBarRetry: no spare spiked boots and no iron bar in the bank or on the mule. The mule's ask
	// rides a world errand that delivers to the bank, so the next run usually finds it.
	DeathBarRetry = 3 * time.Hour
	// DeathBreadRetry: no ten loaves from the bank, the mule or the Baker, whose shelf is shared by
	// the whole fleet and regrows slowly.
	DeathBreadRetry = 2 * time.Hour
	// DeathReissueRetry: an NPC re-issue talk ended without the item although the pack and the bank
	// hold none of it. Not worth repeating inside the hour.
	DeathReissueRetry = time.Hour
)

var (
	DenulthStart = NpcStop{Npc: "Denulth", ID: NpcDenulth, Anchor: DenulthAnchor, Leash: 8,
		Prefer: []string{"Do you have any quests for me?", "No but perhaps I could try and find one?"}}
	// DenulthPlain is the talk that hands over the map or the certificate: content jumps to
	// those before its menu. When neither applies it opens a four-option menu whose "remind me"
	// answer returns to the same menu (death_ig_commander.rs2), so the preference is the exit.
	DenulthPlain = NpcStop{Npc: "Denulth", ID: NpcDenulth, Anchor: DenulthAnchor, Leash: 8,
		Prefer: []string{"That's all, thanks."}}
	EohricGuard = NpcStop{Npc: "Eohric", ID: NpcEohric, Anchor: EohricAnchor, Leash: 8,
		Prefer: []string{"I'm looking for the guard that was on last night."}}
	EohricPlain = NpcStop{Npc: "Eohric", ID: NpcEohric, Anchor: EohricAnchor, Leash: 8,
		Prefer: []string{"I'm looking for the guard that was on last night."}}
	HaroldDuty = NpcStop{Npc: "Harold", ID: NpcHarold, Anchor: HaroldAnchor, Leash: 5,
		Prefer: []string{"You're the guard that was on duty last night?"}}
	SabaPath = NpcStop{Npc: "Saba", ID: NpcSaba, Anchor: SabaInside, Leash: 8,
		Prefer: []string{"Do you know of another way up Death Plateau?"}}
	TenzingHelp = NpcStop{Npc: "Tenzing", ID: NpcTenzing, Anchor: TenzingAnchor, Leash: 6,
		Prefer: []string{"OK, I'll get those for you."}}
	DunstanSpikes = NpcStop{Npc: "Dunstan", ID: NpcDunstan, Anchor: DunstanAnchor, Leash: 8, Prefer: []string{"Yes, but I still want them."}}
	TostigShop    = ShopRef{Npc: "Tostig", NpcID: 1079, Anchor: TostigAnchor}
)

// ParseDeathPlateauJournal reads death_journal.rs2 into the packed stage. Every needle is a
// line only its own stage (or a later one, checked first) renders.
func ParseDeathPlateauJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	if strings.Contains(text, "quest complete!") {
		return DeathStage(DeathComplete, MapScouted), true
	}
	if strings.Contains(text, "i can start this quest") {
		return 0, true
	}
	equip := DeathStarted
	switch {
	case strings.Contains(text, "found the combination to the equipment room and unlocked the door"):
		equip = DeathUnlocked
	case strings.Contains(text, "written on the back of the combination"):
		equip = DeathCombo
	case strings.Contains(text, "he wrote me an iou"):
		equip = DeathIOU
	case strings.Contains(text, "i bought the guard a drink"):
		equip = DeathAle
	case strings.Contains(text, "eohric says to buy the guard a drink"):
		equip = DeathEohric2
	case strings.Contains(text, "the guard wouldn't talk to me"):
		equip = DeathHarold
	case strings.Contains(text, "eohric said that the equipment room guard"):
		equip = DeathEohric
	}
	mapTrack := 0
	switch {
	case strings.Contains(text, "i checked the secret way") || strings.Contains(text, "i have found another way up death plateau for denulth"):
		mapTrack = MapScouted
	case strings.Contains(text, "has given me a map of the secret way"):
		mapTrack = MapGotMap
	case strings.Contains(text, "i gave tenzing the ten loaves"):
		mapTrack = MapSupplies
	case strings.Contains(text, "i have given dunstan the certificate"):
		mapTrack = MapGaveCert
	case strings.Contains(text, "denulth gave me a certificate"):
		mapTrack = MapCert
	case strings.Contains(text, "dunstan will help me if i get his son signed up"):
		mapTrack = MapSmithy
	case strings.Contains(text, "tenzing will show me a secret way"):
		mapTrack = MapTenzing
	case strings.Contains(text, "saba says that there is a sherpa"):
		mapTrack = MapSaba
	}
	return DeathStage(equip, mapTrack), true
}

// DiceBankroll covers four straight losses of the purse+1 stake (101+202+404+808).
const DiceBankroll = 1515

// NextDeathStake is the stake that bankrupts a purse this size, as far as the pack and the
// 1000 cap allow (TS harold.ts nextStake).
func NextDeathStake(purse, pack int) int {
	return minInt(MaxDeathBet, minInt(pack, purse+1))
}

// DeathPurseAfter is the purse a round's verdict implies: a loss adds the stake; a win he could
// pay proves only that his purse beat the stake, so the estimate doubles toward the cap.
func DeathPurseAfter(purse, stake int, won bool) int {
	if !won {
		return purse + stake
	}
	return minInt(MaxDeathBet, purse*2+1)
}

func deathKeep() []int { return append(append([]int(nil), deathTools...), deathFoods...) }

// deathItem is a quest item that content re-issues only to an account that owns none of it.
type deathItem struct {
	id   int
	name string
}

var (
	itemIOU       = deathItem{ObjDeathIOU, "Iou"}
	itemCombo     = deathItem{ObjCombination, "Combination"}
	itemSecretMap = deathItem{ObjSecretMap, "Secret way map"}
	itemClimbing  = deathItem{ObjClimbingBoots, "Climbing boots"}
	itemCert      = deathItem{ObjCertificate, "Certificate"}
)

// deathFetch is the step that brings the quest items the next step needs back from the bank, or
// ok false when there is nothing to fetch: every item is in the pack, or the bank has been read
// this run and holds none of the missing ones (only then is the NPC's re-issue worth asking for).
// An unread bank is read first, never taken for an empty one.
func deathFetch(s Snapshot, why string, items ...deathItem) (Step, bool) {
	var missing []deathItem
	for _, it := range items {
		if s.HeldID(it.id) == 0 {
			missing = append(missing, it)
		}
	}
	if len(missing) == 0 {
		return Step{}, false
	}
	if !s.BankKnown {
		return Step{Kind: StepScanBank, Bank: FaladorWestBank, BankSet: true,
			Reason: why + ": not in the pack, and the bank has not been read this run"}, true
	}
	var draw []WithdrawItem
	for _, it := range missing {
		if n, _ := s.BankedID(it.id); n > 0 {
			draw = append(draw, WithdrawItem{Name: it.name, ID: it.id, Qty: 1})
		}
	}
	if len(draw) == 0 {
		return Step{}, false
	}
	return Step{Kind: StepWithdraw, Items: draw, Bank: FaladorWestBank, BankSet: true, KeepIDs: deathKeep(),
		Reason: why + ": the bank holds them, and the NPC counts the bank as yours"}, true
}

// deathRoom puts a bank trip before a step that is handed an item when the pack has no slot for
// it: content's inv_add on a full pack drops the item at the account's feet (a private drop that
// despawns), which for a re-issue is an item lost for good.
func deathRoom(s Snapshot, st Step) Step {
	if s.FreeSlots >= 1 {
		return st
	}
	return Step{Kind: StepDeposit, KeepIDs: deathKeep(), Bank: FaladorWestBank, BankSet: true,
		Reason: "a free slot for " + st.Describe()}
}

// deathReissue asks an NPC for an item the pack and the bank both lack. The executor returns false
// when the talk ended without the item (questmods), and a second ask in the same run is not made:
// the quest is skipped for DeathReissueRetry instead of repeating the walk and the talk.
func deathReissue(s Snapshot, custom, reason string, expect int) Step {
	if s.Failed[custom] {
		return Step{Kind: StepWait, ParkFor: DeathReissueRetry,
			Reason: reason + " did not come this run (the NPC gave nothing, or could not be reached) and the pack and the bank hold none"}
	}
	return deathRoom(s, Step{Kind: StepCustom, Custom: custom, ExpectItemID: expect, Reason: reason})
}

// deathKitBlocked is whether a kit's last round of this run found every source dead.
func deathKitBlocked(s Snapshot, kit string) bool { return s.Blocked["provision:"+kit] }

// DecideDeathPlateau is the whole quest: the equipment-room track first, then the map track,
// then Denulth with both papers (the TS def's order).
func DecideDeathPlateau(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: DenulthStart}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	equip, mp := DeathTracks(s.Stage)
	if s.TileKnown && InSabaCave(s.Tile) && mp >= MapSaba {
		return Step{Kind: StepCustom, Custom: CustomDeathSaba, Reason: "out of Saba's cave"}
	}
	if equip >= DeathUnlocked && mp >= MapScouted {
		return deathHandIn(s)
	}
	if equip < DeathUnlocked {
		return deathEquipment(s, equip)
	}
	return deathMapTrack(s, mp)
}

// deathHandIn is Denulth with both papers: the combination and the secret way map, each handed
// over in the pack (his flags remember one handed over earlier).
func deathHandIn(s Snapshot) Step {
	if st, ok := deathFetch(s, "Denulth takes the combination and the secret way map", itemCombo, itemSecretMap); ok {
		return st
	}
	switch {
	case s.HeldID(ObjCombination) == 0:
		return deathReissue(s, CustomDeathReclaim, "the combination again from Harold", ObjCombination)
	case s.HeldID(ObjSecretMap) == 0:
		return deathReissue(s, CustomDeathTenzing, "the secret way map again from Tenzing", ObjSecretMap)
	}
	return Step{Kind: StepTalk, Stop: DenulthPlain}
}

// deathEquipment is the equipment-room track up to the unlocked door.
func deathEquipment(s Snapshot, equip int) Step {
	held := func(id int) bool { return s.HeldID(id) > 0 }
	keep := deathKeep()
	switch equip {
	case DeathStarted:
		return Step{Kind: StepTalk, Stop: EohricGuard}
	case DeathEohric:
		return Step{Kind: StepCustom, Custom: CustomDeathHaroldTalk, Reason: "Harold, about last night's duty"}
	case DeathHarold:
		return Step{Kind: StepTalk, Stop: EohricPlain}
	case DeathEohric2:
		if !held(ObjAsgarnianAle) {
			return Step{Kind: StepProvision, KitName: KitDeathAle, KeepIDs: keep, Bank: FaladorWestBank, BankSet: true,
				Kit: []KitLine{{ID: ObjAsgarnianAle, Name: "Asgarnian ale", Qty: 1, Sources: []Source{
					{Kind: SourceGround, Tile: AleSpawn, RespawnTicks: 100},
					{Kind: SourceShop, Shop: TostigShop, Price: 3},
				}}}, Reason: "an Asgarnian ale for Harold"}
		}
		return Step{Kind: StepCustom, Custom: CustomDeathAle, Reason: "the ale for Harold"}
	case DeathAle:
		if s.HeldID(CoinsID) < DiceBankroll && !s.Failed["provision:"+KitDeathDice] {
			return Step{Kind: StepProvision, KitName: KitDeathDice, KeepIDs: keep, Bank: FaladorWestBank, BankSet: true,
				Kit:    []KitLine{{ID: CoinsID, Name: "Coins", Qty: DiceBankroll, Stackable: true}},
				Reason: "a bankroll for four straight losses at the dice"}
		}
		// The IOU Harold writes when he runs out of money is added to the pack.
		return deathRoom(s, Step{Kind: StepCustom, Custom: CustomDeathDice, Reason: "dice with Harold until he writes the IOU"})
	}
	// 55 and 60. The balls need neither paper: their layout is fixed (StoneBalls) and the door
	// unlocks on the stage alone. The combination is wanted again only for Denulth, at the end,
	// where deathHandIn fetches it from wherever it is.
	if equip == DeathIOU && !held(ObjCombination) {
		if held(ObjDeathIOU) {
			return Step{Kind: StepCustom, Custom: CustomDeathReadIOU, Reason: "the IOU, written on the combination's back"}
		}
		if st, ok := deathFetch(s, "the IOU Harold wrote", itemIOU); ok {
			return st
		}
		return deathReissue(s, CustomDeathReclaim, "the IOU again from Harold", ObjDeathIOU)
	}
	if s.FreeSlots < 5 {
		return Step{Kind: StepDeposit, KeepIDs: keep, Bank: FaladorWestBank, BankSet: true}
	}
	return Step{Kind: StepCustom, Custom: CustomDeathBalls, Reason: "the stone balls on the mechanism"}
}

// deathMapTrack is the map track: Saba, Tenzing, Dunstan, Denulth's certificate, the spiked boots,
// the supplies and the scout.
func deathMapTrack(s Snapshot, mp int) Step {
	held := func(id int) bool { return s.HeldID(id) > 0 }
	switch mp {
	case 0:
		return Step{Kind: StepCustom, Custom: CustomDeathSaba, Reason: "Saba, about another way up"}
	case MapSaba:
		// Tenzing hands over his climbing boots on this first visit.
		return deathRoom(s, Step{Kind: StepCustom, Custom: CustomDeathTenzing, ExpectItemID: ObjClimbingBoots, Reason: "Tenzing, for the secret way"})
	case MapTenzing:
		return Step{Kind: StepTalk, Stop: NpcStop{Npc: "Dunstan", ID: NpcDunstan, Anchor: DunstanAnchor, Leash: 8,
			Prefer: []string{"Tenzing has asked me to bring you his climbing boots"}}}
	case MapSmithy:
		return deathRoom(s, Step{Kind: StepTalk, Stop: DenulthPlain})
	case MapCert:
		if held(ObjCertificate) {
			return Step{Kind: StepTalk, Stop: DunstanSpikes}
		}
		if st, ok := deathFetch(s, "Dunstan's certificate", itemCert); ok {
			return st
		}
		return deathRoom(s, Step{Kind: StepTalk, Stop: DenulthPlain})
	case MapGaveCert:
		return deathSpikes(s)
	case MapSupplies, MapGotMap:
		// The map is Tenzing's to re-issue only while none is anywhere; the combination is fetched in the
		// same bank visit, because Denulth wants both after the scout.
		if st, ok := deathFetch(s, "the secret way map and the combination for Denulth", itemSecretMap, itemCombo); ok {
			return st
		}
		if !held(ObjSecretMap) {
			return deathReissue(s, CustomDeathTenzing, "the secret way map from Tenzing", ObjSecretMap)
		}
		return Step{Kind: StepCustom, Custom: CustomDeathScout, Reason: "scout the secret way"}
	}
	return Step{Kind: StepWait, Reason: "death plateau: unrecognised stage"}
}

// deathSpikes is the certificate-given stage: Dunstan makes the spiked boots, and Tenzing takes them
// with ten loaves and ten cooked trout.
//
// Trout come first. Nothing in the fleet supplies them, so while they cannot be had nothing else is
// worth fetching: a spiked pair or a bar fetched now sits in the pack until the next deposit-all
// (or a hand-over to the mule) takes it away, and the account walks to the Baker for bread only to
// fail on the trout line afterwards.
func deathSpikes(s Snapshot) Step {
	held := func(id int) bool { return s.HeldID(id) > 0 }
	keep := deathKeep()
	if s.HeldID(ObjTrout) < deathSupplyQty {
		if deathKitBlocked(s, KitDeathTrout) {
			return Step{Kind: StepWait, ParkFor: DeathTroutRetry,
				Reason: "no ten cooked trout for Tenzing from the bank or the mule, and nothing in the fleet catches or cooks trout"}
		}
		return Step{Kind: StepProvision, KitName: KitDeathTrout, KeepIDs: keep, Bank: FaladorWestBank, BankSet: true,
			Kit:    []KitLine{{ID: ObjTrout, Name: "Trout", Qty: deathSupplyQty}},
			Reason: "ten cooked trout for Tenzing, from the bank or the mule"}
	}
	if !held(ObjSpikedBoots) {
		// Dunstan makes a pair from a bar and the climbing boots, but he counts the bank: a pair already
		// banked is never made again and Tenzing never re-issues the boots. So a spare pair is asked for
		// first (the bank, then the mule), with a bar as an optional line of the same ask, and a pair is
		// made only when none is anywhere. A bar cannot be smelted (Smithing 15, and no executor).
		if held(ObjIronBar) && held(ObjClimbingBoots) {
			return Step{Kind: StepTalk, Stop: DunstanSpikes}
		}
		if !deathKitBlocked(s, KitDeathBoots) {
			return Step{Kind: StepProvision, KitName: KitDeathBoots, KeepIDs: keep, Bank: FaladorWestBank, BankSet: true,
				Kit: []KitLine{
					{ID: ObjSpikedBoots, Name: "Spiked boots", Qty: 1},
					{ID: ObjIronBar, Name: "Iron bar", Qty: 1, Optional: true},
				},
				Reason: "a spare pair of spiked boots, or an iron bar to make one, from the bank or the mule"}
		}
		if !held(ObjIronBar) {
			return Step{Kind: StepWait, ParkFor: DeathBarRetry,
				Reason: "no spiked boots and no iron bar for Dunstan from the bank or the mule (smelting a bar needs Smithing 15)"}
		}
		if st, ok := deathFetch(s, "the climbing boots for Dunstan", itemClimbing); ok {
			return st
		}
		return deathReissue(s, CustomDeathTenzing, "the climbing boots again from Tenzing", ObjClimbingBoots)
	}
	if s.HeldID(ObjBread) < deathSupplyQty {
		if deathKitBlocked(s, KitDeathBread) {
			return Step{Kind: StepWait, ParkFor: DeathBreadRetry,
				Reason: "no ten loaves of bread for Tenzing from the bank, the mule or the Baker's shop"}
		}
		return Step{Kind: StepProvision, KitName: KitDeathBread, KeepIDs: keep, Bank: FaladorWestBank, BankSet: true,
			Kit: []KitLine{{ID: ObjBread, Name: "Bread", Qty: deathSupplyQty, Sources: []Source{
				{Kind: SourceShop, Shop: ArdougneBaker, Price: 12},
			}}}, Reason: "ten loaves of bread for Tenzing"}
	}
	return Step{Kind: StepCustom, Custom: CustomDeathTenzing, ExpectItemID: ObjSecretMap, Reason: "the spiked boots and the supplies to Tenzing"}
}

// DeathPlateauRecord is the quest's identity (no server gates).
var DeathPlateauRecord = Record{ID: DeathPlateauID, Name: DeathPlateauName, QuestPoints: 1}

// DeathPlateau is the engine module.
var DeathPlateau = Module{
	Record: DeathPlateauRecord, Bank: FaladorWestBank, BankSet: true, Anchor: DenulthAnchor,
	ToolIDs:   deathTools,
	ReadStage: ParseDeathPlateauJournal,
	Sustain:   Sustain{FoodIDs: deathFoods, EatAt: 10},
	MaxDeaths: 3,
	Decide:    DecideDeathPlateau,
}
