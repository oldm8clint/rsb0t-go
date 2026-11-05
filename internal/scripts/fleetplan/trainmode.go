package fleetplan

import (
	"sort"
	"strings"
)

// Rune names this file uses. Unexported, and prefixed, so a sibling lane adding its own exported
// rune constants cannot collide with them.
const (
	RuneNature = "Nature rune"
	RuneLaw    = "Law rune"
	RuneFire   = "Fire rune"
	RuneAir    = "Air rune"
	RuneWater  = "Water rune"
	RuneEarth  = "Earth rune"
	RuneDeath  = "Death rune"
	RuneBlood  = "Blood rune"
)

// HighLevelAlchemy and LowLevelAlchemy are the content's two alchemy spells (magic_spells.dbrow).
// Neither is a CombatSpells entry: they target an item, not an npc.
var (
	HighLevelAlchemy = Spell{Name: "High Level Alchemy", Level: 55, XP: 650,
		Runes: []Rune{{Name: RuneNature, Count: 1}, {Name: RuneFire, Count: 5}}}
	LowLevelAlchemy = Spell{Name: "Low Level Alchemy", Level: 21, XP: 310,
		Runes: []Rune{{Name: RuneNature, Count: 1}, {Name: RuneFire, Count: 3}}}
)

// Teleport is one standard teleport: its spell and where it lands (tele_coord, the square centre;
// the engine lands within two tiles of it).
type Teleport struct {
	Spell Spell
	// Dest is where the teleport lands.
	DestX, DestZ int
}

// Teleports is the standard spellbook's quest-free teleports, lowest level first, exactly as
// magic_spells.dbrow defines them. Camelot is a members spell, which the live server allows.
var Teleports = []Teleport{
	{Spell: Spell{Name: "Varrock Teleport", Level: 25, XP: 350,
		Runes: []Rune{{Name: RuneFire, Count: 1}, {Name: RuneAir, Count: 3}, {Name: RuneLaw, Count: 1}}},
		DestX: 50*64 + 13, DestZ: 53*64 + 32},
	{Spell: Spell{Name: "Lumbridge Teleport", Level: 31, XP: 410,
		Runes: []Rune{{Name: RuneEarth, Count: 1}, {Name: RuneAir, Count: 3}, {Name: RuneLaw, Count: 1}}},
		DestX: 50*64 + 21, DestZ: 50*64 + 18},
	{Spell: Spell{Name: "Falador Teleport", Level: 37, XP: 480,
		Runes: []Rune{{Name: RuneWater, Count: 1}, {Name: RuneAir, Count: 3}, {Name: RuneLaw, Count: 1}}},
		DestX: 46*64 + 21, DestZ: 52*64 + 50},
	{Spell: Spell{Name: "Camelot Teleport", Level: 45, XP: 555, Members: true,
		Runes: []Rune{{Name: RuneAir, Count: 5}, {Name: RuneLaw, Count: 1}}},
		DestX: 43*64 + 5, DestZ: 54*64 + 22},
}

// SpendsQueenRunes reports whether casting sp spends a rune the owner has saved for the Queen: a
// Death or a Blood rune (ruling 1, 2026-09-24). No training mode may choose such a spell.
func SpendsQueenRunes(sp Spell) bool {
	for _, r := range sp.Runes {
		if r.Name == RuneDeath || r.Name == RuneBlood {
			return true
		}
	}
	return false
}

// TrainModeSettings are the modes' knobs. Everything is OFF unless Enabled: the modes change what a
// live account spends its hours on, so they are switched on per cohort after a lab proof.
type TrainModeSettings struct {
	// Enabled turns the non-combat modes on at all (stage3TrainModes, default false).
	Enabled bool
	// Alch and Teleport switch each mode (stage3AlchMode / stage3TeleportMode, default true once
	// Enabled).
	Alch     bool
	Teleport bool
	// AlchKit is how many casts one mule ask sizes the natures and the alchables for
	// (stage3AlchKit, default 1000: about 50 minutes of casting at 5 ticks a cast).
	AlchKit int
	// AlchCarry is the most casts' worth the pack holds at a time (stage3AlchCarry, default DefaultStage3AlchCarry): the
	// alch mode kits and asks for CarryKit(), the smaller of the two, and banks the rest (alchcarry.go).
	AlchCarry int
	// AlchRound is casts made per training step before control returns to the stage loop
	// (stage3AlchRound, default 25), so random events, food and the decider still get a turn.
	AlchRound int
	// AlchItems is the ranked list of item names the account may alch (stage3AlchItems, comma
	// separated; default DefaultAlchItems). Anything else in the bank is never alched.
	AlchItems []string
	// AlchMinValue is the least a cast must pay, in coins (stage3AlchMinValue, default 1): below
	// it an item is not worth a nature rune even for experience.
	AlchMinValue int
	// LawReserve is law runes the account keeps and never teleports with (stage3LawReserve,
	// default 10: a few travel teleports' worth).
	LawReserve int
	// MuleLawFloor is the mule's law stock below which the account stops asking it for laws
	// (stage3MuleLawFloor, default 500): laws are the scarce input (2,596 on the mule on
	// 2026-09-24, against 35,332 natures), so the fleet's last few are left for other uses.
	MuleLawFloor int
	// TeleportKit is law runes one mule ask asks for (stage3TeleportKit, default 100).
	TeleportKit int
	// TeleportRound is casts per teleport step (stage3TeleportRound, default 50). A teleport moves
	// the account out of East Ardougne, so a round ends with the walk home; a long round
	// amortises it.
	TeleportRound int
	// TeleportLandOnly casts only teleports whose destination is a LAND walk from East Ardougne
	// (stage3TeleportLandOnly, default true): Camelot. Varrock, Lumbridge and Falador are east of
	// White Wolf Mountain, and every round would end with two fares and two crossings home.
	TeleportLandOnly bool
}

// CarryKit is the casts one alch kit (a mule ask, a bank draw) is sized for: AlchKit, but never more than AlchCarry.
func (s TrainModeSettings) CarryKit() int {
	kit, carry := s.AlchKit, s.AlchCarry
	if kit <= 0 {
		kit = 1000
	}
	if carry <= 0 {
		carry = DefaultStage3AlchCarry
	}
	return min(kit, carry)
}

// DefaultAlchItems is the owner's alch stock (13 Oct 2026 added the Dragonstone, 6,000 a cast, and the Runite bar, 3,000 a
// cast: the mule holds 1,842 and 546 of them, a Nature rune cast on one returns about 5,980 or 2,980 where the best entry
// before them paid 3,840, and selling the same stock to a general store pays at most 3,250 a Dragonstone -- see
// docs/runerunner_ammo_sell_2026-10-13.md; 8 Oct 2026 added the Iron platebody, 336 a cast, which MasterCrafter's anvil
// round makes from the fleet's Iron bars; 5 Oct 2026: the items that pay at High Level Alchemy and
// that the mule's bank and the NPC drops hold), best-paying first by the coins a cast pays, which is
// 60% of the obj's cost in the cache (HighAlchValue; alchValueByName has every number). The bows are
// the STRUNG ones ("maple shortbow/longbow (strung), yew longbow/shortbow (strung)"): in content 289 an
// unstrung bow carries the same NAME as the strung one ("Yew shortbow" is obj 68 and obj 857, 240 and
// 480 coins a cast), so a mule ask names the strung obj by its qualified name (see AskAs) and every
// other part of this list is by plain name, which any obj of that name answers. A name two objs share
// in the cache (Ruby ring, Black dagger, the bows) is published by the mule once per obj
// (peerrunner.IDKeyedNames) so that an ask can name one.
//
// The Dragonstone left the list again on 31 Oct 2026 (owner: "they should have been saved to make glories and
// rings of wealth with"), and neverAlch refuses it whatever a setting names. The same day the strung Magic longbow
// (1,536 a cast) and Magic shortbow (960) joined it (owner: "accounts should be high alching magic longbow strung
// while waiting"): MasterCrafter strings the mule's 591k Magic longbows.
//
// 5 Nov 2026 (owner: "the platebodies are to be used for alching amongst the fleet"): MasterCrafter's platebody round makes
// Rune (39,000 a cast) and Adamant (7,680) platebodies besides the Mithril (3,120) and Iron ones above; each pays far more than
// the Nature rune a cast costs, and they head the list.
var DefaultAlchItems = []string{
	"Rune platebody", "Adamant platebody", "Adamant plateskirt", "Adamant platelegs", "Mithril platebody", "Runite bar", "Adamant chainbody",
	"Adamant sq shield", "Black platebody", "Adamant full helm", "Adamant longsword",
	"Mithril plateskirt", "Mithril platelegs", "Magic longbow", "Mithril kiteshield", "Adamant sword", "Ruby ring",
	"Steel platebody", "Mithril chainbody", "Adamant med helm", "Black plateskirt", "Black platelegs", "Magic shortbow", "Adamant mace",
	"Black chainbody", "Mithril full helm", "Mithril longsword", "Adamant axe", "Yew longbow",
	"Black sq shield", "Mithril scimitar", "Steel platelegs", "Ruby", "Black longsword",
	"Steel kiteshield", "Mithril spear", "Mithril sword", "Adamant dagger", "Yew shortbow",
	"Mithril med helm", "Steel chainbody", "Steel battleaxe", "Maple longbow", "Yew longbow id66", "Iron platebody", "Steel full helm",
	"Mithril axe", "Steel longsword", "Steel scimitar", "Rune javelin", "Maple shortbow", "Yew shortbow id68",
	"Steel sword", "Steel med helm", "Iron 2h sword", "Black dagger", "Magic staff",
	// a user request (bought for alching; "we can do alchs that dont provide profit as we dont sell the nature rune"):
	// Steel platebody, the unstrung yew bows (master strings only the magic ones; qualified, so a mule ask names the
	// unstrung obj) and Hardleather body, each at its payout's place in the list.
	"Hardleather body",
}

// alchValueByName is the High Alchemy payout of DefaultAlchItems, in coins a cast: 60% of the cost in
// the cache of the obj an ask names (the dearest un-noted obj of the name: the strung bow, the ring,
// the real dagger), read from the real item config and pinned against it by a test. The ranking of
// AlchItemsRanked reads it; a name it has no number for (a setting's own) ranks after the known ones.
var alchValueByName = map[string]int{
	"steel platebody": 1200, "yew longbow id66": 384, "yew shortbow id68": 240, "hardleather body": 102,
	"rune platebody": 39000, "adamant platebody": 7680, "dragonstone": 6000, "adamant plateskirt": 3840, "adamant platelegs": 3840, "mithril platebody": 3120,
	"runite bar": 3000, "adamant chainbody": 2880, "adamant sq shield": 2304, "black platebody": 2304,
	"adamant full helm": 2112, "adamant longsword": 1920, "mithril plateskirt": 1560,
	"mithril platelegs": 1560, "magic longbow": 1536, "mithril kiteshield": 1326, "adamant sword": 1248, "ruby ring": 1215,
	"mithril chainbody": 1170, "adamant med helm": 1152, "black plateskirt": 1152,
	"black platelegs": 1152, "magic shortbow": 960, "adamant mace": 864, "black chainbody": 864, "mithril full helm": 858,
	"mithril longsword": 780, "adamant axe": 768, "yew longbow": 768, "black sq shield": 691,
	"mithril scimitar": 624, "steel platelegs": 600, "ruby": 600, "black longsword": 576,
	"steel kiteshield": 510, "mithril spear": 507, "mithril sword": 507, "adamant dagger": 480,
	"yew shortbow": 480, "mithril med helm": 468, "steel chainbody": 450, "steel battleaxe": 390,
	"maple longbow": 384, "iron platebody": 336, "steel full helm": 330, "mithril axe": 312, "steel longsword": 300,
	"steel scimitar": 240, "rune javelin": 240, "maple shortbow": 240, "steel sword": 195,
	"steel med helm": 180, "iron 2h sword": 168, "black dagger": 144, "magic staff": 120,
}

// HighAlchValue is what one High Alchemy cast on an item of oc_cost cost pays: max(cost*6/10, 1),
// alchemy.rs2's scale(6, 10, cost) with its floor of one coin.
func HighAlchValue(cost int) int {
	v := cost * 6 / 10
	if v < 1 {
		v = 1
	}
	return v
}

// KnownAlchValue is the payout of one of DefaultAlchItems, 0 for a name this file has no number for
// (the runner then reads the obj's cost from the cache).
func KnownAlchValue(name string) int {
	return alchValueByName[strings.ToLower(strings.TrimSpace(name))]
}

// NeverAlch is what the modes refuse to alch whatever a setting says: what alchemy.rs2 refuses,
// what the stage casts or eats with, and what the stage cannot replace.
func NeverAlch(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == strings.ToLower(MagicStaff) {
		// The owner's list has it (5 Oct 2026: "magic staff", 120 coins a cast). It is a staff an account
		// can autocast with, but the one it casts with is worn and a cast is made on a carried obj, so a
		// spare Magic staff in the pack or the bank is alch stock like any other. Every other staff stays
		// refused: the Staff of air, water, earth and fire and the plain Staff are what the stages cast
		// with and draw from the bank to wield.
		return false
	}
	if n == "" || n == "coins" || IsRuneName(name) || IsAutocastStaff(name) || strings.Contains(n, "staff") {
		return true
	}
	switch n {
	case "shantay pass", "rope", "knife", "tinderbox", "waterskin(4)", "waterskin(3)", "waterskin(2)", "waterskin(1)", "waterskin(0)":
		return true
	case "dragonstone", "uncut dragonstone":
		// Kept for Amulets of glory and Rings of wealth (a user request).
		return true
	}
	return false
}

// AlchCandidate is one alchable the account (or the mule) holds.
type AlchCandidate struct {
	Name  string
	Count int
	// Value is the coins one cast pays (HighAlchValue of its cost).
	Value int
	// Edible is an obj whose first option is Eat (set by the runner); the walk-alch ranking
	// never alches one, whatever its list says.
	Edible bool
}

// RankAlchables is what may be alched, best first: only names on the settings' list, never a
// neverAlch item, never below AlchMinValue, ordered by the coins a cast pays (the experience is the
// same 65 for every item, so the nature rune is best spent where it also pays most), then by the
// list's own order.
func RankAlchables(held []AlchCandidate, s TrainModeSettings) []AlchCandidate {
	allowed := map[string]int{}
	for i, n := range s.alchItems() {
		allowed[strings.ToLower(strings.TrimSpace(n))] = i
	}
	min := s.AlchMinValue
	if min < 1 {
		min = 1
	}
	var out []AlchCandidate
	for _, c := range held {
		if c.Count <= 0 || NeverAlch(c.Name) {
			continue
		}
		if _, ok := allowed[strings.ToLower(strings.TrimSpace(c.Name))]; !ok {
			continue
		}
		if c.Value < min {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Value != out[j].Value {
			return out[i].Value > out[j].Value
		}
		return allowed[strings.ToLower(out[i].Name)] < allowed[strings.ToLower(out[j].Name)]
	})
	return out
}

func (s TrainModeSettings) alchItems() []string {
	if len(s.AlchItems) > 0 {
		return s.AlchItems
	}
	return DefaultAlchItems
}

// AlchItemsRanked is the settings' alch list ordered by KnownAlchValue (unknown names keep their
// place after the known ones): the choice ladder a mule ask walks down.
func (s TrainModeSettings) AlchItemsRanked() []string {
	items := append([]string(nil), s.alchItems()...)
	sort.SliceStable(items, func(i, j int) bool { return KnownAlchValue(items[i]) > KnownAlchValue(items[j]) })
	out := items[:0]
	for _, n := range items {
		if !NeverAlch(n) {
			out = append(out, n)
		}
	}
	return out
}

// AskNames is items as the mule is asked for them.
func AskNames(items []string, as func(string) string) []string {
	if as == nil {
		return items
	}
	out := make([]string, len(items))
	for i, n := range items {
		out[i] = as(n)
	}
	return out
}
