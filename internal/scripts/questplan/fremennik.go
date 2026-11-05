package questplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// The Fremennik Trials (content id `viking`, varp 347 a vote counter and varp 348 the trials'
// bits, neither transmitted), from the lane Q study (private evidence notes
// fremennik.md) read against content 289's quests/quest_viking. Seven council members vote,
// one per trial, and all seven are needed; Brundt then completes the quest.
//
// The journal (viking_journal.rs2) shows the vote count, each trial as not started / started /
// complete, and the Merchant's chain position from 3 to 13 by its "is looking for" line. This
// module packs those into the stage number: votes + 10*(sum of status_i * 3^i) + 100000*chain.

const FremennikName = "The Fremennik Trials"

const FremennikID = "fremennik"

// FremComplete is the packed stage of a completed quest.
const FremComplete = 99

// The seven trials, in the order this module runs them: the longhall two first, then the
// two east of town, the maze, the Merchant's chain, and last Peer and Thorvald, both of which
// want an empty pack.
type FremTrial int

const (
	TrialManni FremTrial = iota
	TrialOlaf
	TrialSigli
	TrialSwensen
	TrialSigmund
	TrialPeer
	TrialThorvald
	fremTrials
)

// Trial status in the journal.
const (
	TrialNotStarted = 0
	TrialStarted    = 1
	TrialDone       = 2
)

const (
	NpcBrundt     = 1294
	NpcAskeladden = 1295
	NpcManni      = 1286
	NpcThora      = 1300
	NpcOlaf       = 1269
	NpcSigli      = 1281
	NpcSwensen    = 1283
	NpcPeer       = 1288
	NpcThorvald   = 1289
	NpcSigmund    = 1282
	NpcYrsa       = 1301
	NpcSailor     = 1304
	NpcFisherman  = 1302
	NpcSkulgrimen = 1303
	NpcWorkman    = 1287
	NpcLalli      = 1270
	NpcPoisonMan  = 820
	NpcDraugen    = 1279

	ObjRawShark      = 383
	ObjCabbage       = 1965
	ObjPotato        = 1942
	ObjUnstrungLyre  = 3688
	ObjStrungLyre    = 3689
	ObjEnchantedLyre = 3690
	ObjLyreBranch    = 3692
	ObjGoldenFleece  = 3693
	ObjGoldenWool    = 3694
	ObjPetRock       = 3695
	ObjTalismanFull  = 3696
	ObjTalismanEmpty = 3697
	ObjRareFlower    = 3698
	ObjVikingSong    = 3699
	ObjNewBoots      = 3700
	ObjHuntingMap    = 3701
	ObjCustomString  = 3702
	ObjExoticFish    = 3703
	ObjFishingMap    = 3704
	ObjForecast      = 3705
	ObjChampToken    = 3706
	ObjCocktail      = 3707
	ObjNote          = 3708
	ObjNote2         = 3709
	ObjNote3         = 3710
	ObjBeerKeg       = 3711
	ObjLowAlcKeg     = 3712
	ObjFirecracker   = 3713
	ObjFirecrackerLt = 3714
	ObjMithrilSword  = 1285
	ObjKnife         = 946

	// ManniKegPrice is what the poison salesman asks for a low alcohol keg (poison_salesman.rs2
	// [label,poison_salesman_beerkeg]).
	ManniKegPrice = 250
	// ManniKegRefusal is the longhall table's answer to an account that owns a keg of beer in its pack, its bank or
	// its worn items ([opobj3,viking_beerkeg], viking_reveller.rs2).
	ManniKegRefusal = "You already have a keg of beer."

	LocAltarFosse    = 4141
	LocSwayingTree   = 4142
	LocBackstageDoor = 4148
	LocLalliCauldron = 4149
	LocMazeLadder    = 4158
	LocMazeExit      = 4160
	LocLonghallPipe  = 4162
	LocSeerLadderUp  = 4163
	LocSeerDoor1     = 4165
	LocSeerDoor2     = 4166
	LocWarriorLadder = 4187
	LocLoftLadder    = 4189
)

var (
	BrundtAnchor     = tile.Tile{X: 2659, Z: 3669}
	AskeladdenAnchor = tile.Tile{X: 2658, Z: 3660}
	ManniAnchor      = tile.Tile{X: 2660, Z: 3673}
	ThoraAnchor      = tile.Tile{X: 2662, Z: 3673}
	OlafAnchor       = tile.Tile{X: 2673, Z: 3683}
	SigliAnchor      = tile.Tile{X: 2660, Z: 3653}
	SwensenAnchor    = tile.Tile{X: 2646, Z: 3660}
	PeerAnchor       = tile.Tile{X: 2634, Z: 3668}
	ThorvaldAnchor   = tile.Tile{X: 2666, Z: 3693}
	SigmundAnchor    = tile.Tile{X: 2641, Z: 3680}
	YrsaAnchor       = tile.Tile{X: 2625, Z: 3675}
	SailorAnchor     = tile.Tile{X: 2629, Z: 3693}
	FishermanAnchor  = tile.Tile{X: 2641, Z: 3699}
	SkulgrimenAnchor = tile.Tile{X: 2663, Z: 3694}
	WorkmanAnchor    = tile.Tile{X: 2655, Z: 3592}
	LalliAnchor      = tile.Tile{X: 2770, Z: 3622}
	PoisonManAnchor  = tile.Tile{X: 2695, Z: 3495}
	// The longhall: the beer keg's table spot, the drain pipe outside, the backstage door and a
	// stage tile (the stage zone is 2655-2662,3682-3685).
	LonghallKegSpot = tile.Tile{X: 2660, Z: 3676}
	LonghallPipe    = tile.Tile{X: 2663, Z: 3674}
	LonghallInside  = tile.Tile{X: 2659, Z: 3672}
	BackstageDoor   = tile.Tile{X: 2667, Z: 3683}
	StageTile       = tile.Tile{X: 2659, Z: 3683}
	SwayingTree     = tile.Tile{X: 2738, Z: 3638}
	FosseAltar      = tile.Tile{X: 2626, Z: 3598}
	LalliCauldron   = tile.Tile{X: 2772, Z: 3623}
	RellekkaVeg     = tile.Tile{X: 2675, Z: 3653}
	MazeLadder      = tile.Tile{X: 2644, Z: 3657}
	MazeExit        = tile.Tile{X: 2665, Z: 10037}
	SeerDoor1       = tile.Tile{X: 2631, Z: 3667}
	SeerDoor2       = tile.Tile{X: 2636, Z: 3667}
	WarriorLadder   = tile.Tile{X: 2667, Z: 3694}
	LoftLadder      = tile.Tile{X: 2667, Z: 3694, Level: 1}
	fremFoods       = []int{385, 373, 379, 365, 361, 329, 333}
	fremChainGoods  = []int{ObjRareFlower, ObjVikingSong, ObjNewBoots, ObjHuntingMap, ObjCustomString, ObjExoticFish, ObjFishingMap,
		ObjForecast, ObjChampToken, ObjCocktail, ObjNote, ObjNote2, ObjNote3}
	fremTools = append(append([]int{CoinsID, ObjBeer, ObjTinderbox, ObjRawShark, ObjCabbage, ObjPotato, ObjOnion, ObjKnife,
		ObjUnstrungLyre, ObjStrungLyre, ObjEnchantedLyre, ObjLyreBranch, ObjGoldenFleece, ObjGoldenWool, ObjPetRock,
		ObjTalismanFull, ObjTalismanEmpty, ObjBeerKeg, ObjLowAlcKeg, ObjFirecracker, ObjFirecrackerLt, ObjMithrilSword}, fremChainGoods...), Axes...)
)

// The Merchant's maze of portals under Swensen, told apart by loc id only (the wrong portal,
// 4157, throws the account to a random tile). Each is used in turn; MazePortals[i] is where
// portal i+1 stands (viking_hallifred.rs2:248-289, m41_156.jm2).
var MazePortals = []struct {
	ID int
	At tile.Tile
}{
	{4150, tile.Tile{X: 2631, Z: 10002}}, {4151, tile.Tile{X: 2639, Z: 10015}}, {4152, tile.Tile{X: 2656, Z: 10004}},
	{4153, tile.Tile{X: 2665, Z: 10018}}, {4154, tile.Tile{X: 2630, Z: 10023}}, {4155, tile.Tile{X: 2656, Z: 10037}},
	{4156, tile.Tile{X: 2666, Z: 10029}},
}

// InSwensenMaze is the maze under Swensen's house.
func InSwensenMaze(t tile.Tile) bool {
	return t.X >= 2624 && t.X <= 2687 && t.Z >= 9984 && t.Z <= 10047
}

// Koschei's four forms in Thorvald's arena (content quest_viking.npc [viking_enemy1..4], pack ids 1290-1293). The
// account defeats the first three, each defeat restoring his hitpoints and turning him into the next
// (viking_thorvald.rs2 ai_queue3); the fourth has 255 hitpoints, 255 defence and a one-tick attack rate, and the
// trial is passed when a hit of his lands on exactly 0 (the "honour death", [label,viking_honour_death]).
const (
	NpcKoscheiFirst  = 1290
	NpcKoscheiFourth = 1293
)

// InThorvaldArena is Koschei's arena (level 2 of 41_157), InThorvaldLoft the loft the honour
// death teleports to.
func InThorvaldArena(t tile.Tile) bool {
	return t.X >= 2624 && t.X <= 2687 && t.Z >= 10048 && t.Z <= 10111
}
func InThorvaldLoft(t tile.Tile) bool {
	return t.Level == 1 && t.X >= 2660 && t.X <= 2675 && t.Z >= 3686 && t.Z <= 3700
}

// InSeerHouse is Peer's house (both floors) past the entrance door.
func InSeerHouse(t tile.Tile) bool {
	return t.X >= 2629 && t.X <= 2639 && t.Z >= 3660 && t.Z <= 3666
}

// The Merchant's chain: who to ask at each chain position (the NPC that advances it), and who
// takes each trade good (and gives the next). viking_*.rs2's merchant labels.
var MerchantAsk = map[int]int{
	1: NpcSailor, 2: NpcOlaf, 3: NpcYrsa, 4: NpcBrundt, 5: NpcSigli, 6: NpcSkulgrimen, 7: NpcFisherman,
	8: NpcSwensen, 9: NpcPeer, 10: NpcThorvald, 11: NpcManni, 12: NpcThora, 13: NpcAskeladden,
}
var MerchantGive = []struct{ Item, Npc int }{
	{ObjRareFlower, NpcSigmund}, {ObjVikingSong, NpcSailor}, {ObjNewBoots, NpcOlaf}, {ObjNote, NpcYrsa},
	{ObjHuntingMap, NpcBrundt}, {ObjCustomString, NpcSigli}, {ObjExoticFish, NpcSkulgrimen}, {ObjFishingMap, NpcFisherman},
	{ObjForecast, NpcSwensen}, {ObjNote3, NpcPeer}, {ObjChampToken, NpcThorvald}, {ObjCocktail, NpcManni}, {ObjNote2, NpcThora},
}

var fremAnchors = map[int]tile.Tile{
	NpcBrundt: BrundtAnchor, NpcAskeladden: AskeladdenAnchor, NpcManni: ManniAnchor, NpcThora: ThoraAnchor, NpcOlaf: OlafAnchor,
	NpcSigli: SigliAnchor, NpcSwensen: SwensenAnchor, NpcPeer: PeerAnchor, NpcThorvald: ThorvaldAnchor, NpcSigmund: SigmundAnchor,
	NpcYrsa: YrsaAnchor, NpcSailor: SailorAnchor, NpcFisherman: FishermanAnchor, NpcSkulgrimen: SkulgrimenAnchor,
	NpcLalli: LalliAnchor, NpcWorkman: WorkmanAnchor, NpcPoisonMan: PoisonManAnchor,
}
var fremNames = map[int]string{
	NpcBrundt: "Brundt", NpcAskeladden: "Askeladden", NpcManni: "Manni", NpcThora: "Thora", NpcOlaf: "Olaf",
	NpcSigli: "Sigli", NpcSwensen: "Swensen", NpcPeer: "Peer", NpcThorvald: "Thorvald", NpcSigmund: "Sigmund",
	NpcYrsa: "Yrsa", NpcSailor: "Sailor", NpcFisherman: "Fisherman", NpcSkulgrimen: "Skulgrimen",
	NpcLalli: "Lalli", NpcWorkman: "Council workman", NpcPoisonMan: "Poison Salesman",
}

// MerchantStop is a talk that answers the chain's own menu (every council member asks it first
// while the Merchant's trial is open), then the yes of Askeladden's sale.
func MerchantStop(npc int) NpcStop {
	return NpcStop{Npc: fremNames[npc], ID: npc, Anchor: fremAnchors[npc], Leash: 10,
		Prefer: []string{"Ask about the Merchant's trial", "Yes"}}
}

// TrialStop is a talk for a member's OWN trial: "Ask about becoming a Fremennik" past the
// chain's menu, then the trial's own acceptance.
func TrialStop(npc int) NpcStop {
	return NpcStop{Npc: fremNames[npc], ID: npc, Anchor: fremAnchors[npc], Leash: 10,
		Prefer: []string{"Ask about becoming a Fremennik", "What's a Draugen?", "Yes"}}
}

// FremStop is a plain talk to any of the quest's NPCs.
func FremStop(npc int, prefer ...string) NpcStop {
	if len(prefer) == 0 {
		prefer = []string{"Ask about becoming a Fremennik"}
	}
	return NpcStop{Npc: fremNames[npc], ID: npc, Anchor: fremAnchors[npc], Leash: 10, Prefer: prefer}
}

var BrundtStart = NpcStop{Npc: "Brundt", ID: NpcBrundt, Anchor: BrundtAnchor, Leash: 10, Prefer: []string{
	"Do you have any quests?", "Yes, I am interested.", "I want to become a Fremennik!"}}

// FremFacts is the decoded packed stage.
type FremFacts struct {
	Votes  int
	Status [fremTrials]int
	// Chain is the Merchant's chain position when the journal names it (3-13), else 0.
	Chain int
}

func (f FremFacts) Encode() int {
	code, mul := 0, 1
	for _, s := range f.Status {
		code += s * mul
		mul *= 3
	}
	return f.Votes + 10*code + 100000*f.Chain
}

func DecodeFremennik(n int) FremFacts {
	var f FremFacts
	f.Chain = n / 100000
	f.Votes = n % 10
	code := n % 100000 / 10
	for i := range f.Status {
		f.Status[i] = code % 3
		code /= 3
	}
	return f
}

var fremVoteWords = []struct {
	needle string
	votes  int
}{
	{"i have seven votes", 7}, {"i have six votes so far", 6}, {"i have five votes so far", 5}, {"i have four votes so far", 4},
	{"i have three votes so far", 3}, {"i have two votes so far", 2}, {"i have one vote so far", 1}, {"i don't have any votes yet", 0},
}

var fremTrialNeedles = [fremTrials]struct{ done, started string }{
	TrialManni:    {"i now have the reveller's vote", "the reveller will vote for me"},
	TrialOlaf:     {"i now have the bard's vote", "the bard will vote for me"},
	TrialSigli:    {"i now have the hunter's vote", "the hunter will vote for me"},
	TrialSwensen:  {"i now have the navigator's vote", "the navigator will vote for me"},
	TrialSigmund:  {"i now have the merchant's vote", "the merchant will vote for me"},
	TrialPeer:     {"i now have the seer's vote", "the seer will vote for me"},
	TrialThorvald: {"i now have the warrior's vote", "the warrior will vote for me"},
}

var fremChainNeedles = []struct {
	needle string
	chain  int
}{
	{"the bard is looking for some new boots", 3}, {"the shopkeeper is looking for a tax reduction", 4},
	{"the chieftan wants a map of new hunting grounds", 5}, {"the hunter is looking for a custom bowstring", 6},
	{"the armourer is looking for a rare inedible fish", 7}, {"the fisherman is looking for a map of fishing spots", 8},
	{"the navigator is looking for a weather forecast", 9}, {"the seer is looking for a warrior to be his bodyguard", 10},
	{"the warrior is looking for a champions token", 11}, {"the reveller is looking for a legendary cocktail", 12},
	{"all askeladden wants is some money", 13},
}

// ParseFremennikJournal reads the packed stage.
func ParseFremennikJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	switch {
	case text == "":
		return 0, false
	case strings.Contains(text, "quest complete!"):
		return FremComplete, true
	case strings.Contains(text, "i can start this quest by speaking to chieftain brundt"):
		return 0, true
	case !strings.Contains(text, "i spoke to brundt the fremennik chieftan"):
		return 0, false
	}
	var f FremFacts
	f.Votes = -1
	for _, v := range fremVoteWords {
		if strings.Contains(text, v.needle) {
			f.Votes = v.votes
			break
		}
	}
	if f.Votes < 0 {
		return 0, false
	}
	for i, n := range fremTrialNeedles {
		switch {
		case strings.Contains(text, n.done):
			f.Status[i] = TrialDone
		case strings.Contains(text, n.started):
			f.Status[i] = TrialStarted
		}
	}
	for _, c := range fremChainNeedles {
		if strings.Contains(text, c.needle) {
			f.Chain = c.chain
		}
	}
	return f.Encode(), true
}

const (
	CustomFremManni    = "frem-manni"
	CustomFremOlaf     = "frem-olaf"
	CustomFremSigli    = "frem-sigli"
	CustomFremSwensen  = "frem-swensen"
	CustomFremSigmund  = "frem-sigmund"
	CustomFremPeer     = "frem-peer"
	CustomFremThorvald = "frem-thorvald"
	CustomFremLoft     = "frem-loft"
	KitFremManni       = "frem-manni-kit"
	KitFremOlaf        = "frem-olaf-kit"
	KitFremSigli       = "frem-sigli-kit"
	KitFremSigmund     = "frem-sigmund-kit"
	KitFremThorvald    = "frem-thorvald-kit"
)

// VarrockSwordShop is the Varrock sword shop's keeper (content varrock.npc [swordshop1], spawn m50_53 3203,3397), the nearest shop that sells a
// Mithril sword (stock5, 3 at 3000 coins). The kit had no source for it: on 5 Nov 2026 the Sigli kit "could not be filled" on every account
// that reached it ("1 Mithril sword: the mule publishes none; no ground, skill or shop source listed"), and the Manni kit stalled the same
// way on a Tinderbox that the Lumbridge and Varrock general stores sell for a coin.
var VarrockSwordShop = ShopRef{Npc: "Shop keeper", NpcID: 551, Anchor: tile.Tile{X: 3203, Z: 3397}}

// MithrilSwordPrice is the sword shop's base price (varrock.inv [swordshop] stock5=mithril_sword,3,3000).
const MithrilSwordPrice = 3000

func fremKit(trial FremTrial, s Snapshot) []KitLine {
	switch trial {
	case TrialManni:
		kit := []KitLine{
			{ID: ObjBeer, Name: "Beer", Qty: 1},
			{ID: ObjTinderbox, Name: "Tinderbox", Qty: 1, Sources: []Source{
				{Kind: SourceShop, Shop: LumbridgeStore, Price: 1, Note: "Lumbridge general store keeps 2"},
				{Kind: SourceShop, Shop: VarrockGenStore, Price: 1, Note: "Varrock general store keeps 2"}}},
		}
		// The poison salesman's low alcohol keg is the only thing Manni's trial buys, and it is bought once: an
		// account that holds one (the bank step below takes a banked one out first) needs no coins, and one that
		// still has to buy asks for the price, not a margin over it (on 16 Oct 2026 13 of the 29 banks that hold low
		// alcohol kegs held no coins at all, and the kit asked 300 gp).
		if s.HeldID(ObjLowAlcKeg) == 0 {
			kit = append(kit, KitLine{ID: CoinsID, Name: "Coins", Qty: ManniKegPrice, Stackable: true})
		}
		return kit
	case TrialOlaf:
		kit := []KitLine{
			{ID: ObjKnife, Name: "Knife", Qty: 1, Sources: []Source{{Kind: SourceGround, Tile: tile.Tile{X: 2681, Z: 3717}, RespawnTicks: 100}}},
			{ID: ObjRawShark, Name: "Raw shark", Qty: 1},
		}
		// The swaying tree wants any axe the Woodcutting level can use: one the account owns, else an iron axe
		// (the bank, the mule, Aemad's shop).
		if !AxeHeld(s) {
			kit = append(kit, AxeLine(s))
		}
		return kit
	case TrialSigli:
		return []KitLine{
			{ID: ObjMithrilSword, Name: "Mithril sword", Qty: 1, Worn: true, Sources: []Source{
				{Kind: SourceShop, Shop: VarrockSwordShop, Price: MithrilSwordPrice, Note: "Varrock's sword shop keeps 3 (3000 coins each), restocking slowly"}}},
			{ID: 385, Name: "Shark", Qty: 10},
		}
	case TrialSigmund:
		return []KitLine{{ID: CoinsID, Name: "Coins", Qty: 5000, Stackable: true}}
	case TrialThorvald:
		return []KitLine{{ID: 385, Name: "Shark", Qty: 20}}
	}
	return nil
}

// fremManniBank takes the kegs of Manni's trial out of the bank before anything is bought or picked up.
//
// Content issues neither keg to an account that owns one anywhere. The longhall's table answers a second keg of beer
// with "You already have a keg of beer." whenever the pack, the bank and the worn items together hold one
// ([opobj3,viking_beerkeg] tests ~obj_gettotal, inv_procs.rs2), and the poison salesman sells a low alcohol keg to
// anyone with 250 gp whatever the bank holds. The fleet's between-run bank dump puts every quest item away, so the
// next run of an account whose trial failed stood at the table refused or bought another keg: 27 of the 28 "no keg of
// beer on the table" lines of 13-14 Oct 2026 (three bursts of nine, twelve seconds apart) are one account, the one
// bank in the fleet that holds a keg of beer, and the same bank holds fifteen low alcohol kegs; 190 of them lay in 29
// banks on 16 Oct, about 47,500 gp of 250 gp purchases.
//
// So a pack without the keg of beer reads the bank first (a bank not yet read this run is not an empty one), and a keg
// the bank holds comes out, the low alcohol keg too when the pack has none. Zero Step means nothing to fetch.
func fremManniBank(s Snapshot, keep []int) (Step, bool) {
	kegHeld := s.HeldID(ObjBeerKeg) > 0
	lowHeld := s.HeldID(ObjLowAlcKeg) > 0
	if !kegHeld && !s.BankKnown {
		return Step{Kind: StepScanBank, Bank: SeersBank, BankSet: true,
			Reason: "the bank, for a keg of beer or a low alcohol keg the account already owns (the longhall gives no second keg)"}, true
	}
	var draw []WithdrawItem
	if n, _ := s.BankedID(ObjBeerKeg); !kegHeld && n > 0 {
		draw = append(draw, WithdrawItem{Name: "Keg of beer", ID: ObjBeerKeg, Qty: 1})
	}
	if n, _ := s.BankedID(ObjLowAlcKeg); !lowHeld && n > 0 {
		draw = append(draw, WithdrawItem{Name: "Low alcohol keg", ID: ObjLowAlcKeg, Qty: 1})
	}
	if len(draw) == 0 {
		return Step{}, false
	}
	return Step{Kind: StepWithdraw, Items: draw, Bank: SeersBank, BankSet: true, KeepIDs: keep,
		Reason: "the kegs of Manni's trial out of the bank (content gives an account that owns one no second)"}, true
}

func fremTrialNpc(t FremTrial) int {
	return [fremTrials]int{NpcManni, NpcOlaf, NpcSigli, NpcSwensen, NpcSigmund, NpcPeer, NpcThorvald}[t]
}

var fremCustoms = [fremTrials]string{CustomFremManni, CustomFremOlaf, CustomFremSigli, CustomFremSwensen, CustomFremSigmund, CustomFremPeer, CustomFremThorvald}
var fremKits = [fremTrials]string{KitFremManni, KitFremOlaf, KitFremSigli, "", KitFremSigmund, "", KitFremThorvald}

// NextTrial is the first trial not yet complete, in the module's order.
func (f FremFacts) NextTrial() (FremTrial, bool) {
	for t := FremTrial(0); t < fremTrials; t++ {
		if f.Status[t] != TrialDone {
			return t, true
		}
	}
	return 0, false
}

func DecideFremennik(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: BrundtStart}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	if s.Stage == FremComplete {
		return Step{Kind: StepDone}
	}
	f := DecodeFremennik(s.Stage)
	here := s.TileKnown
	if here && InThorvaldLoft(s.Tile) {
		return Step{Kind: StepCustom, Custom: CustomFremLoft, Reason: "down from the longhall loft"}
	}
	if f.Votes >= 7 {
		return Step{Kind: StepTalk, Stop: FremStop(NpcBrundt, "Ask about becoming a Fremennik")}
	}
	t, ok := f.NextTrial()
	if !ok {
		return Step{Kind: StepTalk, Stop: FremStop(NpcBrundt, "Ask about becoming a Fremennik")}
	}
	keep := append(append([]int(nil), fremTools...), fremFoods...)
	// A trial in a pocket finishes before anything else.
	if here && InSwensenMaze(s.Tile) {
		return Step{Kind: StepCustom, Custom: CustomFremSwensen, Reason: "through the Navigator's maze"}
	}
	if here && InThorvaldArena(s.Tile) {
		return Step{Kind: StepCustom, Custom: CustomFremThorvald, Reason: "Koschei, unarmed"}
	}
	if here && InSeerHouse(s.Tile) {
		return Step{Kind: StepCustom, Custom: CustomFremPeer, Reason: "the Seer's house puzzle"}
	}
	// Manni's two kegs are quest items the content will not issue again while the account owns one ANYWHERE.
	if t == TrialManni && f.Status[t] == TrialStarted {
		if st, ok := fremManniBank(s, keep); ok {
			return st
		}
	}
	// The trial's kit comes BEFORE its first word. The bank is at Seers and the council in Rellekka, and the
	// way between runs over the basalt rocks (a dice roll, and 1-10 damage on a miss): a kit fetched after the
	// start talk sent the account over them three times where one is enough (live 14 Oct 2026: Olaf's talk, then
	// the Seers bank for the kit, then Rellekka again).
	if kit := fremKits[t]; kit != "" && !s.Ran["provision:"+kit] {
		if s.Failed["provision:"+kit] {
			return Step{Kind: StepWait, Reason: "the " + kit + " could not be filled from the bank, the mule or a source"}
		}
		if t == TrialOlaf && !AxeHeld(s) && !s.BankKnown {
			// An axe in the bank is the kit's axe: an unread bank is not an empty one.
			return Step{Kind: StepScanBank, Bank: SeersBank, BankSet: true, Reason: "the bank, for an axe for the swaying tree"}
		}
		return Step{Kind: StepProvision, KitName: kit, Kit: fremKit(t, s), KeepIDs: keep, Bank: SeersBank, BankSet: true,
			Reason: "the kit for " + fremNames[fremTrialNpc(t)] + "'s trial"}
	}
	if f.Status[t] == TrialNotStarted && t != TrialSigmund {
		return Step{Kind: StepTalk, Stop: TrialStop(fremTrialNpc(t))}
	}
	if f.Status[t] == TrialNotStarted {
		return Step{Kind: StepTalk, Stop: FremStop(NpcSigmund, "Ask about becoming a Fremennik", "Yes")}
	}
	return Step{Kind: StepCustom, Custom: fremCustoms[t], Reason: fremNames[fremTrialNpc(t)] + "'s trial"}
}

// MerchantNext is who the chain wants next: the taker of a held trade good first, else the NPC
// the journal's chain position names (1 and 2 read the same: the sailor, then Olaf).
func MerchantNext(s Snapshot, chain int, askedSailor bool) (npc int, give bool) {
	for _, g := range MerchantGive {
		if s.HeldID(g.Item) > 0 {
			return g.Npc, true
		}
	}
	if chain == 0 {
		if askedSailor {
			return NpcOlaf, false
		}
		return NpcSailor, false
	}
	return MerchantAsk[chain], false
}

var FremennikRecord = Record{ID: FremennikID, Name: FremennikName, QuestPoints: 3}

var Fremennik = Module{
	Record: FremennikRecord, Bank: SeersBank, BankSet: true, Anchor: BrundtAnchor,
	ToolIDs:   fremTools,
	ReadStage: ParseFremennikJournal,
	Sustain:   Sustain{FoodIDs: fremFoods, EatAt: 15},
	MaxDeaths: 3,
	Decide:    DecideFremennik,
}
