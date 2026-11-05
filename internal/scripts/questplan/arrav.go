package questplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Shield of Arrav (content id `arrav`, quest tab "Shield of Arrav"), the first stage of the Throne chain that needs a second account. Ported from
// content 289 quests/quest_blackarmgang and areas/area_varrock (reldo, baraek, straven, katrine, tramp, weaponsmaster, curator, king_roald).
//
// One account is Phoenix and one is Black Arm (MakePairs: the first of two names is the Phoenix account). The roles are fixed:
//
//	Phoenix    Reldo (a quest), the palace bookcase (a book), read it, Reldo, Baraek (20 coins), Straven in the cellar (the Phoenix HQ, a ladder under
//	           the building at 3244,3383), Jonny the Beard in the Blue Moon Inn (his intelligence report), Straven (a key), the chest in the HQ (half a
//	           shield). It GIVES the key to the Black Arm account, TAKES that account's half, hands both to the Curator for two certificates, GIVES the
//	           second certificate back, and takes its own to King Roald.
//	Black Arm  the Tramp, Katrine (two Phoenix crossbows are her price), the Phoenix key from the partner (the weapon store's door is locked to
//	           anyone else), the weapons store upstairs (the Weaponsmaster is level 23 and hostile to a non-Phoenix), two crossbows, Katrine, the cupboard
//	           upstairs in the Black Arm HQ (the other half). It GIVES its half to the Phoenix account, TAKES a certificate, and takes it to King Roald.
//
// Every trade is between the two accounts, in the Varrock museum, and every wait for the partner is bounded (ArravNext's maxWait and the custom
// steps' own deadlines): a partner that never comes is a parked quest, never a loop.

// ArravName is the quest tab's spelling (internal/quests: "Shield of Arrav").
const ArravName = "Shield of Arrav"

// Ids from content/pack.
const (
	NpcArravBaraek       = 547
	NpcArravKatrine      = 642
	NpcArravWeaponmaster = 643
	NpcArravStraven      = 644
	NpcArravJonny        = 645
	NpcArravCurator      = 646
	NpcArravTramp        = 641 // [tramppg]

	ObjArravBook      = 757 // [the_shield_of_arrav], op Read
	ObjArravKey       = 759 // [phoenixkey2]
	ObjArravReport    = 761 // [intelligence_report]
	ObjArravShield1   = 763 // the Phoenix half
	ObjArravShield2   = 765 // the Black Arm half
	ObjArravCrossbow  = 767 // [phoenix_crossbow]
	ObjArravCert      = 769 // [arravcertificate]
	LocArravBookcase  = 2402
	LocArravPhDoor    = 2397 // [phoenixdoor], op Open, a member's door
	LocArravPhDoor2   = 2398 // [phoenixdoor2], the weapon store's door: the key is used on it
	LocArravPhChest   = 2403 // [phoenixshutchest], op Open
	LocArravPhChestOp = 2404 // [phoenixopenchest], op Search
	LocArravBaDoor    = 2399
	LocArravBaCupShut = 2400 // op Open
	LocArravBaCupOpen = 2401 // op Search (op2; op1 is Shut)
	LocArravCellarLdr = 1754 // [ladder_cellar], op Climb-down: lands 6400 tiles south
	LocArravPhLadder  = 2405 // [phoenixladder], op Climb-up: back to 3244,3382
	LocArravStoreLdr  = 1747 // [ladder], at 3252,3384, op Climb-up
	LocArravStoreTop  = 1746 // [laddertop], op Climb-down
	LocArravStairs    = 1722 // [stairs] 3188,3389, op Climb-up
	LocArravStairsTop = 1723 // [stairstop], op Climb-down
)

// Tiles (content maps m50_52, m50_53, m50_54, m50_152, m49_52; the cellar is the same building 6400 tiles south).
var (
	ArravBaraekAnchor    = tile.Tile{X: 3218, Z: 3435}
	ArravBookcase        = tile.Tile{X: 3212, Z: 3493}
	ArravJonnyAnchor     = tile.Tile{X: 3223, Z: 3395}
	ArravStravenAnchor   = tile.Tile{X: 3246, Z: 9780}
	ArravCellarLadder    = tile.Tile{X: 3244, Z: 3383}
	ArravPhoenixDoor     = tile.Tile{X: 3247, Z: 9779}
	ArravPhoenixChest    = tile.Tile{X: 3235, Z: 9761}
	ArravStoreDoor       = tile.Tile{X: 3251, Z: 3385}
	ArravStoreLadder     = tile.Tile{X: 3252, Z: 3384}
	ArravStoreSmith      = tile.Tile{X: 3246, Z: 3384, Level: 1}
	ArravCrossbowA       = tile.Tile{X: 3243, Z: 3383, Level: 1}
	ArravCrossbowB       = tile.Tile{X: 3245, Z: 3385, Level: 1}
	ArravTrampAnchor     = tile.Tile{X: 3208, Z: 3391}
	ArravKatrineAnchor   = tile.Tile{X: 3186, Z: 3385}
	ArravBaDoor          = tile.Tile{X: 3185, Z: 3388}
	ArravBaStairs        = tile.Tile{X: 3188, Z: 3389}
	ArravBaCupboard      = tile.Tile{X: 3188, Z: 3385, Level: 1}
	ArravCuratorAnchor   = tile.Tile{X: 3255, Z: 3445}
	ArravMeetAnchor      = tile.Tile{X: 3255, Z: 3446}
	ArravBaraekFee       = 20
	arravKeepIDsPhoenix  = []int{ObjArravBook, ObjArravKey, ObjArravReport, ObjArravShield1, ObjArravShield2, ObjArravCert, CoinsID}
	arravKeepIDsBlackArm = []int{ObjArravKey, ObjArravCrossbow, ObjArravShield1, ObjArravShield2, ObjArravCert, CoinsID}
)

// Custom step names (internal/scripts/questmods/arrav.go).
const (
	CustomArravDown     = "arrav-down"     // the Phoenix cellar's ladder
	CustomArravSurface  = "arrav-surface"  // out of the cellar or off an upper floor, by whichever ladder or stair is near
	CustomArravJonny    = "arrav-jonny"    // kill Jonny the Beard, take the report
	CustomArravPhHalf   = "arrav-ph-half"  // the Phoenix door and chest
	CustomArravBaHalf   = "arrav-ba-half"  // the Black Arm stairs and cupboard
	CustomArravCross    = "arrav-crossbow" // the weapon store: key on the door, the ladder, the Weaponsmaster, two crossbows
	CustomArravKey      = "arrav-key"      // meet the Phoenix account for its key
	CustomArravMeet     = "arrav-meet"     // the museum: the half, the Curator, the certificate
	KitArravBaraek      = "arrav-baraek"
	arravPairParkFor    = time.Hour
	arravJournalNoStart = "i can start this quest by speaking to reldo"
)

// Stages as ParseArravJournal encodes them. A Phoenix account's stage is its %phoenixgang (0 to 10); a Black Arm account's is acct its
// %blackarmgang (0 to 4). Zero is "not started" for either.
const (
	ArravBlackArmBase = 100
)

// ParseArravJournal reads the stage from the rendered journal (blackarmgang_journal.rs2). It reads the Phoenix lines first (an account only
// ever shows one gang's): a Phoenix account's text names Reldo, Baraek or Straven, a Black Arm account's the Tramp or Katrine.
func ParseArravJournal(lines []string) (int, bool) {
	t := NormaliseLines(lines)
	if t == "" {
		return 0, false
	}
	has := func(s string) bool { return strings.Contains(t, s) }
	phoenix := has("straven") || has("fur trader") || has("quest hidden in one of the books") || has("knows anything more") || has("valuable shield") || has("vtam")
	blackArm := has("katrine") || has("black arm gang")
	switch {
	case has("quest complete") && phoenix:
		return PhoenixComplete, true
	case has("quest complete"):
		return ArravBlackArmBase + BlackArmComplete, true
	case phoenix && !(blackArm && !has("straven")):
		switch {
		case has("allowed me to join"):
			return PhoenixJoined, true
		case has("straven"):
			return 8, true
		case has("south-eastern part of varrock"):
			return 4, true
		case has("fur trader"):
			return 3, true
		case has("knows anything more"):
			return 2, true
		default:
			return 1, true
		}
	case blackArm:
		switch {
		case has("allowed me to join"):
			return ArravBlackArmBase + BlackArmJoined, true
		case has("offered to let me join") || has("two phoenix crossbows"):
			return ArravBlackArmBase + 2, true
		default:
			return ArravBlackArmBase + 1, true
		}
	case has(arravJournalNoStart):
		return 0, true
	}
	return 0, false
}

// PairState is a pair quest's view of its partner, from the runner's PairLink: who, which gang each takes, and the partner's last report.
type PairState struct {
	Known   bool   // a partner is configured and the gang roles are assigned
	Partner string // the partner's account name
	Gang    Gang   // this account's gang
	World   int    // the world this account is on (0: not said)
	View    ArravPartnerView
	// PartnerWorld is the world the partner last reported (0: not said). A pair meets on one world only.
	PartnerWorld int
}

// PairAssign is the pair of two names under MakePairs' rule and the gang name takes in it: the one that sorts first, case-insensitively, is
// the Phoenix account, so two accounts that each name the other as partner agree without talking.
func PairAssign(name, partner string) (g Gang, ok bool) {
	name, partner = strings.TrimSpace(name), strings.TrimSpace(partner)
	if name == "" || partner == "" || strings.EqualFold(name, partner) {
		return GangNone, false
	}
	a, b := name, partner
	if strings.ToLower(b) < strings.ToLower(a) {
		a, b = b, a
	}
	pairs, _ := MakePairs([]string{a, b})
	if len(pairs) != 1 {
		return GangNone, false
	}
	_, g, ok = PartnerOf(pairs, name)
	if strings.EqualFold(pairs[0].Phoenix, name) {
		return GangPhoenix, ok
	}
	return GangBlackArm, ok
}

// arravStops.
var (
	arravReldoStart = NpcStop{Npc: "Reldo", ID: NpcReldo, Anchor: SqReldoAnchor, Leash: 6, Prefer: []string{"I'm in search of a quest."}}
	arravReldoBook  = NpcStop{Npc: "Reldo", ID: NpcReldo, Anchor: SqReldoAnchor, Leash: 6, Prefer: []string{"Ok. I've read the book", "Thanks"}}
	arravBaraekStop = NpcStop{Npc: "Baraek", ID: NpcArravBaraek, Anchor: ArravBaraekAnchor, Leash: 6,
		Prefer: []string{"Can you tell me where I can find the Phoenix Gang?", "Okay. Have 20 gold coins."}}
	arravStravenJoin = NpcStop{Npc: "Straven", ID: NpcArravStraven, Anchor: ArravStravenAnchor, Leash: 6,
		Prefer: []string{"I know who you are!", "I'd like to offer you my services."}}
	arravStravenReport = NpcStop{Npc: "Straven", ID: NpcArravStraven, Anchor: ArravStravenAnchor, Leash: 6, Prefer: []string{"I have the intelligence report!"}}
	arravTrampStop     = NpcStop{Npc: "Tramp", ID: NpcArravTramp, Anchor: ArravTrampAnchor, Leash: 6,
		Prefer: []string{"Is there anything down this alleyway?", "Do you think they would let me join?"}}
	arravKatrineStart = NpcStop{Npc: "Katrine", ID: NpcArravKatrine, Anchor: ArravKatrineAnchor, Leash: 6,
		Prefer: []string{"I've heard you're the Black Arm Gang.", "I'd rather not reveal my sources.", "I want to become a member of your gang.",
			"Well, you can give me a try", "Well, people tell me I have an honest face.", "Ok, no problem."}}
	arravKatrineBack = NpcStop{Npc: "Katrine", ID: NpcArravKatrine, Anchor: ArravKatrineAnchor, Leash: 6, Prefer: []string{"Yes, I have."}}
	arravRoaldStop   = NpcStop{Npc: "King Roald", ID: NpcKingRoald, Anchor: RoaldAnchor, Leash: 8, Prefer: []string{"Greetings, your majesty."}}
)

// InPhoenixCellar reports a tile in the Phoenix Gang's cellar.
func InPhoenixCellar(t tile.Tile) bool {
	return t.Level == 0 && t.Z >= 9700 && t.X >= 3225 && t.X <= 3265
}

// OnArravUpperFloor reports a tile on a level the quest's upper rooms are on (the weapon store, the Black Arm cupboard).
func OnArravUpperFloor(t tile.Tile) bool {
	return t.Level == 1 && t.Z < 4000 && t.X >= 3170 && t.X <= 3260
}

// arravCoinsKit is the 20 coins Baraek's price takes, from the bank or the mule (no shop sells coins).
func arravCoinsKit() []KitLine {
	return []KitLine{{ID: CoinsID, Name: "Coins", Qty: ArravBaraekFee, Stackable: true}}
}

// DecideArrav is the whole quest for one account of a pair. The pair assignment comes in s.Pair; without it the quest waits an hour at a
// time and does nothing (it is never started on a guess, because a lone account that joined a gang strands half of a pair).
func DecideArrav(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	}
	if !s.Pair.Known {
		return Step{Kind: StepWait, ParkFor: arravPairParkFor,
			Reason: "Shield of Arrav needs a partner account of the other gang: set stage5ThroneChainPartner on both accounts of the pair"}
	}
	if s.Journal != StatusNotStarted && !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal has not been read yet"}
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	switch s.Pair.Gang {
	case GangPhoenix:
		return decideArravPhoenix(s, held)
	case GangBlackArm:
		return decideArravBlackArm(s, held)
	}
	return Step{Kind: StepWait, ParkFor: arravPairParkFor, Reason: "no gang was assigned to this account"}
}

func arravPackStep(s Snapshot, keep []int) (Step, bool) {
	if s.FreeSlots < 3 {
		return Step{Kind: StepDeposit, KeepIDs: keep, Bank: VarrockWestBank, BankSet: true, Reason: "room for the quest's items"}, true
	}
	return Step{}, false
}

func decideArravPhoenix(s Snapshot, held func(int) bool) Step {
	stage := s.Stage
	if s.Journal == StatusNotStarted {
		stage = 0
	}
	if stage >= ArravBlackArmBase {
		return Step{Kind: StepWait, ParkFor: arravPairParkFor, Reason: "the journal shows Black Arm lines on the Phoenix account"}
	}
	surface := func() (Step, bool) {
		if InPhoenixCellar(s.Tile) || OnArravUpperFloor(s.Tile) {
			return Step{Kind: StepCustom, Custom: CustomArravSurface, Reason: "back up to the street"}, true
		}
		return Step{}, false
	}
	switch {
	case stage == 0:
		if st, ok := surface(); ok {
			return st
		}
		return Step{Kind: StepTalk, Stop: arravReldoStart}
	case stage == 1:
		if held(ObjArravBook) {
			return Step{Kind: StepOpHeld, ItemID: ObjArravBook, Item: "Book", Op: "Read", Reason: "read The Shield of Arrav"}
		}
		if st, ok := arravPackStep(s, arravKeepIDsPhoenix); ok {
			return st
		}
		return Step{Kind: StepInteractLoc, LocID: LocArravBookcase, Loc: "Bookcase", Op: "Check", Anchor: ArravBookcase,
			ExpectItemID: ObjArravBook, ExpectItem: "Book"}
	case stage == 2:
		return Step{Kind: StepTalk, Stop: arravReldoBook}
	case stage == 3:
		if s.HeldID(CoinsID) < ArravBaraekFee {
			if s.Failed["provision:"+KitArravBaraek] {
				return Step{Kind: StepWait, ParkFor: time.Hour, Reason: "no 20 coins for Baraek from the bank or the mule"}
			}
			return Step{Kind: StepProvision, KitName: KitArravBaraek, Kit: arravCoinsKit(), KeepIDs: arravKeepIDsPhoenix, Bank: VarrockWestBank, BankSet: true,
				Reason: "the 20 coins Baraek's tip costs"}
		}
		return Step{Kind: StepTalk, Stop: arravBaraekStop}
	case stage == 4 || (stage == 8 && held(ObjArravReport)):
		if !InPhoenixCellar(s.Tile) {
			return Step{Kind: StepCustom, Custom: CustomArravDown, Reason: "down the ladder under the VTAM building"}
		}
		if stage == 8 {
			return Step{Kind: StepTalk, Stop: arravStravenReport}
		}
		return Step{Kind: StepTalk, Stop: arravStravenJoin}
	case stage == 8:
		if st, ok := surface(); ok {
			return st
		}
		return Step{Kind: StepCustom, Custom: CustomArravJonny, Reason: "Jonny the Beard's intelligence report"}
	}
	// Joined (9) or beyond. The chest gives a half to anyone who holds none (arravshield1 is re-issued until the quest is complete), so a lost
	// or given-away half is simply fetched again.
	if stage >= PhoenixJoined {
		if held(ObjArravCert) || held(ObjArravShield1) || held(ObjArravShield2) {
			if st, ok := surface(); ok {
				return st
			}
			return meetStep(s, "meet the Black Arm account in the museum")
		}
		if s.Failed["custom:"+CustomArravPhHalf] {
			return Step{Kind: StepWait, ParkFor: time.Hour, Reason: "the Phoenix chest gave no half shield"}
		}
		return Step{Kind: StepCustom, Custom: CustomArravPhHalf, Reason: "half a shield from the Phoenix Gang's chest"}
	}
	return Step{Kind: StepWait, Reason: "the Phoenix account is at stage " + itoa(stage) + ", which the module does not know"}
}

func decideArravBlackArm(s Snapshot, held func(int) bool) Step {
	stage := s.Stage - ArravBlackArmBase
	if s.Journal == StatusNotStarted || s.Stage == 0 {
		stage = 0
	}
	if s.Stage > 0 && s.Stage < ArravBlackArmBase {
		return Step{Kind: StepWait, ParkFor: arravPairParkFor, Reason: "the journal shows Phoenix lines on the Black Arm account"}
	}
	surface := func() (Step, bool) {
		if InPhoenixCellar(s.Tile) || OnArravUpperFloor(s.Tile) {
			return Step{Kind: StepCustom, Custom: CustomArravSurface, Reason: "back down to the street"}, true
		}
		return Step{}, false
	}
	switch {
	case stage == 0:
		if st, ok := surface(); ok {
			return st
		}
		return Step{Kind: StepTalk, Stop: arravTrampStop}
	case stage == 1:
		if st, ok := surface(); ok {
			return st
		}
		return Step{Kind: StepTalk, Stop: arravKatrineStart}
	case stage == 2:
		if s.HeldID(ObjArravCrossbow) >= 2 {
			if st, ok := surface(); ok {
				return st
			}
			return Step{Kind: StepTalk, Stop: arravKatrineBack}
		}
		if held(ObjArravKey) {
			return Step{Kind: StepCustom, Custom: CustomArravCross, Reason: "two Phoenix crossbows from the weapon store"}
		}
		if st, ok := surface(); ok {
			return st
		}
		return Step{Kind: StepCustom, Custom: CustomArravKey, Reason: "the Phoenix account's key, from the partner"}
	}
	if stage >= BlackArmJoined {
		if held(ObjArravCert) || held(ObjArravShield1) || held(ObjArravShield2) {
			if st, ok := surface(); ok {
				return st
			}
			return meetStep(s, "meet the Phoenix account in the museum")
		}
		if s.Failed["custom:"+CustomArravBaHalf] {
			return Step{Kind: StepWait, ParkFor: time.Hour, Reason: "the Black Arm cupboard gave no half shield"}
		}
		return Step{Kind: StepCustom, Custom: CustomArravBaHalf, Reason: "half a shield from the Black Arm cupboard"}
	}
	return Step{Kind: StepWait, Reason: "the Black Arm account is at stage " + itoa(stage) + ", which the module does not know"}
}

// meetStep is the pair's museum step: the custom executor runs ArravNext against the partner's reports until the quest's next errand is the
// King's (a certificate in the pack) or its deadline passes.
func meetStep(s Snapshot, why string) Step {
	if s.HeldID(ObjArravCert) == 1 && s.Pair.Gang != GangNone && !s.Failed["custom:"+CustomArravMeet] {
		// One certificate in the pack: it is this account's to take to the King.
		return Step{Kind: StepTalk, Stop: arravRoaldStop}
	}
	if s.Failed["custom:"+CustomArravMeet] {
		return Step{Kind: StepWait, ParkFor: arravPairParkFor, Reason: "the partner did not meet at the museum in time; trying again later"}
	}
	return Step{Kind: StepCustom, Custom: CustomArravMeet, Reason: why}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// ArravRecord is the quest's identity: 1 quest point, no server gates (an account of any level can join either gang).
var ArravRecord = Record{ID: ArravID, Name: ArravName, QuestPoints: 1}

// ArravModule is the engine module. The pair's gang comes through Snapshot.Pair, so one module serves both accounts.
var ArravModule = Module{
	Record: ArravRecord, Bank: VarrockWestBank, BankSet: true, Anchor: SqReldoAnchor,
	ToolIDs:   []int{ObjArravBook, ObjArravKey, ObjArravReport, ObjArravShield1, ObjArravShield2, ObjArravCrossbow, ObjArravCert, CoinsID},
	ReadStage: ParseArravJournal,
	Decide:    DecideArrav,
	Sustain:   Sustain{EatAt: 12},
	MaxDeaths: 3,
}
