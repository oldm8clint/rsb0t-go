package actions

import (
	"errors"
	"regexp"
	"strings"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

// Magic: Game.castOnNpc and Game.teleport from the TS scripting API.
//
// Both address a spell by its component on the standard spellbook ("magic", interface 1151),
// with ids from content branch 289's pack/interface.pack (magic:<name>). Upstream resolves
// targeted spells through the live tree's targetBase text and teleports through button text,
// falling back to the same static ids for teleports; this client's archive decoder does not
// keep targetBase, so the table is used directly.
//
// A cast on an NPC is OPNPCT: Client.ts's TGT_NPC branch writes p2(npc index) p2(spell
// component), and Engine-TS's OpNpcTDecoder reads npcSlot then spellCom. The engine rejects it
// unless the spell component is visible (the magic tab is loaded) and allows NPC targets, and
// like OpNpc the client-side approach walk is not sent, so be in casting range first.
//
// A teleport is an IF_BUTTON on the spell. Upstream's Game.teleport returns once the click is
// sent; confirming arrival is the caller's job (internal/bot's Context.Teleport does it).

// MagicTab is the sidebar tab index of the spellbook.
const MagicTab = 6

// ErrUnknownSpell is returned for a spell name this package has no component for.
var ErrUnknownSpell = errors.New("actions: unknown spell")

// combatSpells are the standard spellbook's NPC-targetable spells.
var combatSpells = map[string]int{
	"wind strike": 1152, "confuse": 1153, "water strike": 1154, "earth strike": 1156,
	"weaken": 1157, "fire strike": 1158, "wind bolt": 1160, "curse": 1161,
	"water bolt": 1163, "earth bolt": 1166, "fire bolt": 1169, "crumble undead": 1171,
	"wind blast": 1172, "water blast": 1175, "earth blast": 1177, "fire blast": 1181,
	"wind wave": 1183, "water wave": 1185, "earth wave": 1188, "fire wave": 1189,
	"saradomin strike": 1190, "claws of guthix": 1191, "flames of zamorak": 1192,
	"iban blast": 1539, "vulnerability": 1542, "enfeeble": 1543, "stun": 1562,
	"bind": 1572, "snare": 1582, "entangle": 1592,
}

// teleportSpells are the standard spellbook's teleports, keyed like Teleport.ts's TELEPORTS.
var teleportSpells = map[string]int{
	"varrock": 1164, "lumbridge": 1167, "falador": 1170, "camelot": 1174,
	"ardougne": 1540, "watchtower": 1541, "trollheim": 7455,
}

var colourTag = regexp.MustCompile(`(?i)@[a-z0-9]{3}@`)

func spellKey(name string) string {
	key := colourTag.ReplaceAllString(name, " ")
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.TrimPrefix(key, "cast ")
	key = strings.ReplaceAll(key, "_", " ")
	return strings.Join(strings.Fields(key), " ")
}

// CombatSpellComponent returns the spellbook component of an NPC-targetable spell, matched
// case-insensitively with spaces or underscores ("Wind Strike", "wind_strike").
func CombatSpellComponent(name string) (int, bool) {
	id, ok := combatSpells[spellKey(name)]
	return id, ok
}

// TeleportComponent ports resolveTeleport: the spellbook component of a standard teleport,
// accepting "Varrock", "Varrock Teleport" or the button's own "Cast @gre@Varrock teleport".
func TeleportComponent(name string) (int, bool) {
	key := strings.TrimSuffix(spellKey(name), " teleport")
	id, ok := teleportSpells[strings.TrimSpace(key)]
	return id, ok
}

// CastOnNpc ports Game.castOnNpc: OPNPCT with the NPC's index and the spell's component.
func CastOnNpc(s Session, spell string, npcIndex int) error {
	comID, ok := CombatSpellComponent(spell)
	if !ok {
		return ErrUnknownSpell
	}
	return s.Send(func(w *packet.Packet) {
		w.P1Enc(clientprot.OPNPCT)
		w.P2(uint16(npcIndex))
		w.P2(uint16(comID))
	})
}

// locSpells are the standard spellbook's spells cast on scenery: the orb charges at the
// obelisks. Keyed without the "charge " prefix the in-game names carry.
var locSpells = map[string]int{
	"water orb": 1179, "earth orb": 1182, "fire orb": 1184, "air orb": 1186,
}

// LocSpellComponent returns the spellbook component of a spell cast on scenery ("Charge Water
// Orb", "water_orb").
func LocSpellComponent(name string) (int, bool) {
	id, ok := locSpells[strings.TrimPrefix(spellKey(name), "charge ")]
	return id, ok
}

// itemSpells are the standard spellbook's spells cast on a backpack item: the two alchemies.
// Not independently re-derived — confirmed by counting component definitions in content
// branch 289's content/scripts/skill_magic/interfaces/magic.if in file order (the same
// convention documented at the top of this file for combatSpells/teleportSpells/locSpells:
// component id = 1151 + 1-based position of the [name] block) and cross-checking that
// numbering against every one of this file's own already-proven ids: wind_strike is the 1st
// block -> 1152 (combatSpells["wind strike"]), confuse 2nd -> 1153, ... flames_of_zamorak
// 41st -> 1192, all 30-odd already-ported spells landing exactly on their known ids. On that
// same numbering, lowlvl_alchemy is the 11th block (1151+11=1162) and highlvl_alchemy the
// 27th (1151+27=1178) — see content/scripts/skill_magic/scripts/spells/alchemy.rs2's own
// [opheldt,magic:lowlvl_alchemy]/[opheldt,magic:highlvl_alchemy] entry points for the
// component names themselves.
//
// Superheat Item is 1173: content/pack/interface.pack's own line "1173=magic:superheat_item" (the
// same file gives 1162=magic:lowlvl_alchemy and 1178=magic:highlvl_alchemy, agreeing with the two
// above), entry point [opheldt,magic:superheat_item] in skill_magic/scripts/spells/superheat.rs2.
//
// Lvl-1 Enchant is 1155: "1155=magic:enchant_lvl1" in the same file (the 4th block of magic.if,
// 1151+4, after wind_strike, confuse and water_strike), entry point [opheldt,magic:enchant_lvl1] in
// skill_magic/scripts/spells/enchant.rs2. It turns a Sapphire necklace into a Games necklace(8) in its
// slot (MasterCrafter's jewellery round). Lvl-2 Enchant is 1165 ("1165=magic:enchant_lvl2", the 14th
// block: 1151+14), entry point [opheldt,magic:enchant_lvl2] in the same file; it turns an Emerald ring
// into a Ring of dueling(8) (magic_spells.dbrow [magic_spell_enchant_level2]).
var itemSpells = map[string]int{
	"low level alchemy": 1162, "high level alchemy": 1178, "superheat item": 1173,
	"lvl-1 enchant": 1155, "enchant lvl1": 1155,
	"lvl-2 enchant": 1165, "enchant lvl2": 1165,
}

// objSpells are the standard spellbook's spells cast on a ground item: Telekinetic Grab, component
// 1168 (content 289 pack/interface.pack "1168=magic:telekinetic_grab"; 1151 + its 17th place in
// skill_magic/interfaces/magic.if, the numbering the tables in this file use).
var objSpells = map[string]int{
	"telekinetic grab": 1168,
}

// ObjSpellComponent returns the spellbook component of a spell cast on a ground item
// ("Telekinetic Grab", "telekinetic_grab").
func ObjSpellComponent(name string) (int, bool) {
	id, ok := objSpells[spellKey(name)]
	return id, ok
}

// CastOnObj ports Client.ts's TGT_OBJ branch: OPOBJT with the item's absolute tile, its type and
// the spell's component (OpObjTDecoder: g2 x, g2 z, g2 obj, g2 spellCom). Client.ts first tries a
// move towards the item; that is NOT sent here, and the server never paths to an obj itself
// (Player.pathToPathingTarget is for NPCs and players), so a grab out of reach fails where the
// account stands ("I can't reach that!").
func CastOnObj(s Session, spell string, x, z, objType int) error {
	comID, ok := ObjSpellComponent(spell)
	if !ok {
		return ErrUnknownSpell
	}
	return s.Send(func(w *packet.Packet) {
		w.P1Enc(clientprot.OPOBJT)
		w.P2(uint16(x))
		w.P2(uint16(z))
		w.P2(uint16(objType))
		w.P2(uint16(comID))
	})
}

// ItemSpellComponent returns the spellbook component of a spell cast on a backpack item
// ("High Level Alchemy", "high_level_alchemy").
func ItemSpellComponent(name string) (int, bool) {
	id, ok := itemSpells[spellKey(name)]
	return id, ok
}

// CastOnItem ports Game.castOnItem: OPHELDT, Client.ts's TGT_HELD branch (own doc comment
// above cites the exact p2(a=objId) p2(b=slot) p2(c=comId) p2(targetComId=spell component)
// payload from Client.ts's onAction dispatch). Same (objID, slot, comID) shape as OpHeld's
// held-item family (Bury/Eat), plus the spell component in place of a fixed op number — this
// is a targeted op (like CastOnNpc/CastOnLoc), not an OPHELD option, so it goes through its
// own opcode rather than actions.OpHeld.
func CastOnItem(s Session, spell string, objID int32, slot int, comID int) error {
	comSpell, ok := ItemSpellComponent(spell)
	if !ok {
		return ErrUnknownSpell
	}
	err := s.Send(func(w *packet.Packet) {
		w.P1Enc(clientprot.OPHELDT)
		w.P2(uint16(objID))
		w.P2(uint16(slot))
		w.P2(uint16(comID))
		w.P2(uint16(comSpell))
	})
	if err == nil {
		// The cast's p_delay makes the engine drop a held-item click read inside it (OpHeldHandler: `if (player.delayed) return
		// false`), an eat among them: EatBite waits it out (bite.go).
		NoteDelayTicks(s, castHeldTicks(comSpell))
	}
	return err
}

// CastOnLoc ports Game.castOnLoc: OPLOCT, which is Client.ts's interactWithLoc payload (the
// loc's tile and type, as OpLoc sends) followed by p2(spell component). Like OpLoc it sends
// no approach walk, and it carries interactWithLoc's ANTICHEAT_CYCLELOGIC2.
func CastOnLoc(s Session, spell string, x, z, locType int) error {
	comID, ok := LocSpellComponent(spell)
	if !ok {
		return ErrUnknownSpell
	}
	return s.Send(func(w *packet.Packet) {
		s.Cheats().BeforeInteractWithLoc(w)
		w.P1Enc(clientprot.OPLOCT)
		w.P2(uint16(x))
		w.P2(uint16(z))
		w.P2(uint16(locType))
		w.P2(uint16(comID))
	})
}

// Teleport ports Game.teleport's dispatch: IF_BUTTON on the teleport spell. It does not wait
// for arrival.
func Teleport(s Session, name string) error {
	comID, ok := TeleportComponent(name)
	if !ok {
		return ErrUnknownSpell
	}
	return IfButton(s, comID)
}
