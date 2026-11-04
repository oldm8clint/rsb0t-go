// Package runecraftplan is the pure half of the fleet's Runecrafting (a user request: "runecraft
// with runners in the fleet, maxing nature runes; the main account gets the Runecraft levels"). This first step is
// P1 of the design (docs/runecraft_p1_2026-10-13.md): MasterCrafter levels Runecraft 1 to 44 alone, Air
// to 20 and Body from there, on essence from his bank, the mule or his own mining at the Wizards'
// Guild. Nature (RC 44, members, a talisman) is in the table for the later phases and refused here
// without a talisman or on a free world.
//
// It imports no client, actions, fleet or bot code and does no I/O. What it owns:
//
//   - the ALTAR TABLE (altars.go): per rune the level, XP, rune multiplier, talisman, the ruins that
//     take the talisman, the temple's arrival and altar and portal, the bank nearest the ruins;
//   - the arithmetic (xp.go): XP for a level, runes per essence, the essence a level plan needs;
//   - the DECISIONS (level.go): which altar for a level (PickAltar) and what to do next
//     (DecideLevelStage) from where the account stands and what it holds.
//
// Everything is content 289 (scripts/skill_runecraft), re-read for this step:
//
//   - a talisman is used ON the ruins (oplocu; the ruins have no option at all) and nothing deletes it:
//     runecraft.rs2 teleport_to_ruins, p_delay(1) then p_telejump to the temple's enter_coord. The level
//     is checked at the ALTAR, not at the ruins;
//   - the altar (op1 Craft-rune, or the essence used on it) binds EVERY unnoted Rune essence in the pack
//     at once: runes = essence * (level / multiplier + 1), XP = essence * the table's tenths, then
//     p_delay(3) -- four busy ticks;
//   - the portal (op1 Use) takes the account to the temple's exit_coord with no delay at all;
//   - Nature is a MEMBERS rune: on a free world the craft does nothing (runecraft.rs2:56-61).
package runecraftplan

import "rsb0t-go/internal/scripts/tile"

// Item and loc names the runner resolves through the cache, and the numbers every trip shares.
const (
	// EssenceName is the pack and bank item; EssenceID the unnoted id (blankrune) and NotedEssenceID the
	// bank note's (cert_blankrune): only the unnoted one is bound ("You do not have any rune stones to bind").
	EssenceName    = "Rune essence"
	EssenceID      = 1436
	NotedEssenceID = 1437

	// RuinsName, AltarName and PortalName are the locs' names (runecraft.loc); AltarOp and PortalOp their
	// option names. The ruins have no option.
	RuinsName  = "Mysterious ruins"
	AltarName  = "Altar"
	AltarOp    = "Craft-rune"
	PortalName = "Portal"
	PortalOp   = "Use"

	// PackSlots is a full pack; LoadSize the essence one trip carries with the talisman in the other slot.
	PackSlots = 28
	LoadSize  = PackSlots - 1
	// MinGo is the fewest essence worth walking to the ruins with when the bank holds more: a draw that
	// had to share the pack with a teleport kit is a little short and goes anyway.
	MinGo = 20
	// HoldTicks is the busy time after a craft (p_delay(3) is four ticks): every packet the engine reads in the
	// four ticks after the craft is dropped, so the portal click is sent once the client has seen the output of
	// tick craft+HoldTicks, to be read in the first free tick, craft+HoldTicks+1.
	HoldTicks = 4
	// RuinsRadius is how far from the table's tile the ruins, altar and portal are searched for.
	RuinsRadius = 6
	// MineMapsquareX and MineMapsquareZ are the essence mine's mapsquare (45,75): the temples are the
	// squares beside it.
	MineMapsquareX = 45
	MineMapsquareZ = 75
)

// The rune keys.
const (
	KeyAir    = "air"
	KeyBody   = "body"
	KeyNature = "nature"
)

// Altar is one rune's row of the table.
type Altar struct {
	// Key is KeyAir and so on; Name the rune's display name ("Air").
	Key  string
	Name string
	// Level is the Runecrafting level the ALTAR needs; XP10 the XP a rune essence gives in tenths of a
	// point (50 is 5.0); Multiplier the level step at which a further rune per essence comes (0: never).
	Level      int
	XP10       int
	Multiplier int
	// Members is set for a rune the free worlds do not craft.
	Members bool
	// Talisman and TalismanID name the item that opens the ruins; Rune and RuneID the product.
	Talisman   string
	TalismanID int
	Rune       string
	RuneID     int
	// RuinsID is the ruins' loc type and Ruins its south-west tile (the loc is 3x3, shape 10, angle 0).
	RuinsID int
	Ruins   tile.Tile
	// Exit is where the temple's portal puts the account, beside the ruins.
	Exit tile.Tile
	// Enter is where the ruins put the account inside; the temple is the mapsquare around it.
	Enter tile.Tile
	// AltarID and AltarAt are the altar loc (3x3) and its south-west tile; PortalID and PortalAt the exit
	// portal inside.
	AltarID  int
	AltarAt  tile.Tile
	PortalID int
	PortalAt tile.Tile
	// Bank is the booth bank nearest the ruins by the collision pack (pack steps from Bank to Exit are in
	// BankSteps); NoBank is set for Nature, whose nearest bank is the quest-gated Shilo Village one.
	Bank      tile.Tile
	BankName  string
	BankSteps int
	NoBank    bool
}

// Inside reports whether t is in this altar's temple (its mapsquare: the temples are 64x64 squares of
// their own at z 4800).
func (a Altar) Inside(t tile.Tile) bool {
	return t.Level == 0 && t.X>>6 == a.Enter.X>>6 && t.Z>>6 == a.Enter.Z>>6
}

// RunesPerEssence is runes per essence at level: level / multiplier + 1 (runecraft.rs2:80-84, integer
// division; a rune with no multiplier column gives one).
func (a Altar) RunesPerEssence(level int) int {
	if a.Multiplier <= 0 || level < 1 {
		return 1
	}
	return level/a.Multiplier + 1
}

// The three rows P1 and its successors use. Positions are the loc index's, checked against the real
// index and the collision pack by TestAltarTableAgainstTheRealData (set RSB0T_LOC_INDEX,
// RSB0T_CONFIG_JAG and RSB0T_COLLISION_PACK), numbers by TestAltarTableMatchesContent
// (RSB0T_CONTENT_DIR).
var (
	// Air: level 1, 5.0 XP, a second rune at 11. The Falador east bank is 81 pack steps from the exit
	// (Draynor 125, Falador west 140, Edgeville 262).
	Air = Altar{
		Key: KeyAir, Name: "Air", Level: 1, XP10: 50, Multiplier: 11,
		Talisman: "Air talisman", TalismanID: 1438, Rune: "Air rune", RuneID: 556,
		RuinsID: 2452, Ruins: tile.Tile{X: 2984, Z: 3291}, Exit: tile.Tile{X: 2983, Z: 3288},
		Enter: tile.Tile{X: 2841, Z: 4830}, AltarID: 2478, AltarAt: tile.Tile{X: 2843, Z: 4833},
		PortalID: 2465, PortalAt: tile.Tile{X: 2841, Z: 4828},
		Bank: tile.Tile{X: 3013, Z: 3355}, BankName: "Falador east", BankSteps: 81,
	}
	// Body: level 20, 7.5 XP, a second rune at 46. The Edgeville bank is 116 pack steps from the exit
	// (Falador west 135, Varrock west 150, Falador east 160).
	Body = Altar{
		Key: KeyBody, Name: "Body", Level: 20, XP10: 75, Multiplier: 46,
		Talisman: "Body talisman", TalismanID: 1446, Rune: "Body rune", RuneID: 559,
		RuinsID: 2457, Ruins: tile.Tile{X: 3052, Z: 3444}, Exit: tile.Tile{X: 3050, Z: 3442},
		Enter: tile.Tile{X: 2523, Z: 4838}, AltarID: 2483, AltarAt: tile.Tile{X: 2524, Z: 4831},
		PortalID: 2470, PortalAt: tile.Tile{X: 2523, Z: 4825},
		Bank: tile.Tile{X: 3094, Z: 3491}, BankName: "Edgeville", BankSteps: 116,
	}
	// Nature: level 44, 9.0 XP, a second rune only at 91, MEMBERS. Karamja has no bank a non-quest
	// account can reach.
	Nature = Altar{
		Key: KeyNature, Name: "Nature", Level: 44, XP10: 90, Multiplier: 91, Members: true,
		Talisman: "Nature talisman", TalismanID: 1462, Rune: "Nature rune", RuneID: 561,
		RuinsID: 2460, Ruins: tile.Tile{X: 2868, Z: 3018}, Exit: tile.Tile{X: 2865, Z: 3022},
		Enter: tile.Tile{X: 2400, Z: 4835}, AltarID: 2486, AltarAt: tile.Tile{X: 2399, Z: 4840},
		PortalID: 2473, PortalAt: tile.Tile{X: 2400, Z: 4834},
		NoBank: true,
	}
)

// Altars is the table, lowest level first.
func Altars() []Altar { return []Altar{Air, Body, Nature} }

// ByKey is the row for a key (case-insensitive handled by the caller's lower-casing).
func ByKey(key string) (Altar, bool) {
	for _, a := range Altars() {
		if a.Key == key {
			return a, true
		}
	}
	return Altar{}, false
}

// ByTalisman is the row whose talisman is object id.
func ByTalisman(id int) (Altar, bool) {
	for _, a := range Altars() {
		if a.TalismanID == id {
			return a, true
		}
	}
	return Altar{}, false
}

// Region is where an account stands as far as Runecrafting goes.
type Region int

// The regions.
const (
	// RegionOutside is anywhere else: the road, a bank, the ruins' doorstep.
	RegionOutside Region = iota
	// RegionTemple is inside one of the altars' temples (Temple names it).
	RegionTemple
	// RegionMine is inside the Rune essence mine.
	RegionMine
)

func (r Region) String() string {
	switch r {
	case RegionTemple:
		return "temple"
	case RegionMine:
		return "mine"
	}
	return "outside"
}

// WhereIs says which region t is in and, inside a temple, whose.
func WhereIs(t tile.Tile) (Region, Altar) {
	for _, a := range Altars() {
		if a.Inside(t) {
			return RegionTemple, a
		}
	}
	if t.Level == 0 && t.X>>6 == MineMapsquareX && t.Z>>6 == MineMapsquareZ {
		return RegionMine, Altar{}
	}
	return RegionOutside, Altar{}
}
