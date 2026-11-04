package actions

import (
	"errors"
	"regexp"
)

// Autocast: choosing a combat spell the account casts automatically with a staff, reading
// back what is set, and recognising the server's "you do not have enough runes" refusal.
//
// Nothing in this client had any autocast support before this file. Content branch 289
// implements it for Strike through Wave with any staff
// (skill_combat/scripts/player/auto_cast.rs2), and the whole feature is three plain
// IF_BUTTON clicks plus two varps:
//
//  1. combat_staff_2:auto_choose (component 353) runs @open_autocast_interface, which sets
//     %attackstyle_magic to 0 and does `if_settab(staff_spells, 0)` — the spell list
//     replaces the weapon tab in sidebar slot 0.
//  2. staff_spells:ssb0..ssb15 (components 1830..1845) each run @set_autocast_spell for one
//     spell: it stores the spell in %autocast_spell, sets %attackstyle_magic to 2, and calls
//     ~initalltabs, which puts the ordinary weapon tab back.
//  3. combat_staff_2:auto_toggle (component 349) flips bit 0 of %attackstyle_magic, taking it
//     from 2 to 3 — which is what actually switches autocasting on.
//
// Component ids are content 289's pack/interface.pack; varp ids are pack/varp.pack
// (autocast_spell is varp 59, attackstyle_magic is varp 108, and all.varp marks
// attackstyle_magic `transmit=yes` so it genuinely reaches this client).
//
// The "enabled" test is deliberately the odd one content itself uses
// (auto_cast.rs2's [proc,player_autocast_enabled]):
//
//	if (testbit(%attackstyle_magic, 0) = ^true & %attackstyle_magic = 3 & %autocast_spell > 0)
//
// The bit test is redundant with the equality, so in practice autocast is on exactly when
// attackstyle_magic is 3 and a spell is set. AutocastEnabled below mirrors that rather than
// inventing a looser rule, so this client's answer always matches the server's.
//
// Two refusals a caller must expect, both of which leave the varps untouched:
//
//   - Step 2 runs ~check_spell_requirements (skill_magic/scripts/magic.rs2) BEFORE storing
//     anything, so choosing a spell fails outright when the account lacks the Magic level,
//     the runes, or members for a Wave. The runes are checked at *set* time, not only at cast
//     time, which is why IsNoRunesLine matters here and not just during a fight.
//   - Step 3 refuses with "You need to choose a spell first." when %attackstyle_magic is 0.
//
// Every function here only sends its click, per this package's fire-and-forget rule. Verify
// with AutocastSpell/AutocastEnabled afterwards — internal/scripts/combatrunner.SetAutocast
// is the wrapper that does the waiting and checking.

// Autocast component ids (content 289 pack/interface.pack).
const (
	// AutocastChooseButton is combat_staff_2:auto_choose, which opens the spell list.
	AutocastChooseButton = 353
	// AutocastToggleButton is combat_staff_2:auto_toggle, which switches autocasting on
	// or off once a spell has been chosen.
	AutocastToggleButton = 349
	// StaffSpellsRoot is the staff_spells interface, which if_settab puts in sidebar slot 0.
	StaffSpellsRoot = 1829
	// CombatStaffRoot is combat_staff_2, the staff's own weapon tab that carries both the
	// choose and toggle buttons.
	CombatStaffRoot = 328
)

// Autocast varps (content 289 pack/varp.pack).
const (
	// VarpAutocastSpell holds the chosen spell's own id, 0 for none.
	VarpAutocastSpell = 59
	// VarpAttackStyleMagic is 0 with no spell chosen, 2 with one chosen but autocast off,
	// and 3 with autocast on.
	VarpAttackStyleMagic = 108
)

// The %attackstyle_magic values content uses: 0 with no spell chosen, 2 once
// set_autocast_spell has stored one, and 3 once auto_toggle has switched autocasting on.
const (
	attackStyleMagicSpellChosen = 2
	attackStyleMagicAutocasting = 3
)

// Rune is one rune an autocast spell consumes per cast.
type Rune struct {
	// Name is the rune's display name as the obj config spells it, for a caller resolving
	// it through cachecfg.
	Name string
	// Count is how many that spell needs per cast, before any staff substitution.
	Count int
}

// Spell is one of the sixteen spells the staff spell list offers.
type Spell struct {
	// Name is the spell's display name, as magic_combat_spells.dbrow's `data=name` spells it.
	Name string
	// ID is the spell's own content id, which is what %autocast_spell holds.
	ID int
	// Button is the staff_spells component that selects it.
	Button int
	// Level is the Magic level it requires.
	Level int
	// Members is true for a spell only a members world allows — the four Waves.
	Members bool
	// Runes are the runes one cast consumes. A staff matching a rune's element supplies
	// that rune for free (skill_magic/scripts/magic.rs2's [proc,staff_runes]), so a caller
	// costing a trip must subtract whatever its staff covers.
	Runes []Rune
}

// The sixteen autocastable spells, in staff_spells button order (ssb0..ssb15).
//
// Ids come from skill_combat/configs/magic/spells.constant — note wind_strike is 51, not 0,
// which is exactly the kind of value that would be wrong if guessed from the button order.
// Level, members and runes come from skill_combat/configs/magic/magic_combat_spells.dbrow.
var autocastSpells = []Spell{
	{"Wind Strike", 51, 1830, 1, false, []Rune{{"Mind rune", 1}, {"Air rune", 1}}},
	{"Water Strike", 1, 1831, 5, false, []Rune{{"Mind rune", 1}, {"Water rune", 1}, {"Air rune", 1}}},
	{"Earth Strike", 2, 1832, 9, false, []Rune{{"Mind rune", 1}, {"Earth rune", 2}, {"Air rune", 1}}},
	{"Fire Strike", 3, 1833, 13, false, []Rune{{"Mind rune", 1}, {"Fire rune", 3}, {"Air rune", 2}}},
	{"Wind Bolt", 4, 1834, 17, false, []Rune{{"Chaos rune", 1}, {"Air rune", 2}}},
	{"Water Bolt", 5, 1835, 23, false, []Rune{{"Chaos rune", 1}, {"Water rune", 2}, {"Air rune", 2}}},
	{"Earth Bolt", 6, 1836, 29, false, []Rune{{"Chaos rune", 1}, {"Earth rune", 3}, {"Air rune", 2}}},
	{"Fire Bolt", 7, 1837, 35, false, []Rune{{"Chaos rune", 1}, {"Fire rune", 4}, {"Air rune", 3}}},
	{"Wind Blast", 8, 1838, 41, false, []Rune{{"Death rune", 1}, {"Air rune", 3}}},
	{"Water Blast", 9, 1839, 47, false, []Rune{{"Death rune", 1}, {"Water rune", 3}, {"Air rune", 3}}},
	{"Earth Blast", 10, 1840, 53, false, []Rune{{"Death rune", 1}, {"Earth rune", 4}, {"Air rune", 3}}},
	{"Fire Blast", 11, 1841, 59, false, []Rune{{"Death rune", 1}, {"Fire rune", 5}, {"Air rune", 4}}},
	{"Wind Wave", 12, 1842, 62, true, []Rune{{"Blood rune", 1}, {"Air rune", 5}}},
	{"Water Wave", 13, 1843, 65, true, []Rune{{"Blood rune", 1}, {"Water rune", 7}, {"Air rune", 5}}},
	{"Earth Wave", 14, 1844, 70, true, []Rune{{"Blood rune", 1}, {"Earth rune", 7}, {"Air rune", 5}}},
	{"Fire Wave", 15, 1845, 75, true, []Rune{{"Blood rune", 1}, {"Fire rune", 7}, {"Air rune", 5}}},
}

// ErrNoSuchSpell is returned for a spell name or id the staff spell list does not offer.
var ErrNoSuchSpell = errors.New("actions: no such autocast spell")

// AutocastSpells returns every autocastable spell, in button order.
func AutocastSpells() []Spell {
	return append([]Spell(nil), autocastSpells...)
}

// SpellByName finds an autocastable spell by display name, case-insensitively.
func SpellByName(name string) (Spell, bool) {
	for _, s := range autocastSpells {
		if foldCompare(s.Name, name) == 0 {
			return s, true
		}
	}
	return Spell{}, false
}

// SpellByID finds an autocastable spell by the content id %autocast_spell reports.
func SpellByID(id int) (Spell, bool) {
	for _, s := range autocastSpells {
		if s.ID == id {
			return s, true
		}
	}
	return Spell{}, false
}

// BestAutocastSpell returns the highest-level spell the account can cast at magicLevel,
// restricted to the given element and to non-members spells when members is false. ok is
// false when nothing qualifies.
//
// Element is matched on the spell name's first word (Wind, Water, Earth, Fire); an empty
// element accepts any.
func BestAutocastSpell(magicLevel int, element string, members bool) (Spell, bool) {
	best := Spell{}
	found := false
	for _, s := range autocastSpells {
		if s.Level > magicLevel {
			continue
		}
		if s.Members && !members {
			continue
		}
		if element != "" && !hasElementPrefix(s.Name, element) {
			continue
		}
		if !found || s.Level > best.Level {
			best, found = s, true
		}
	}
	return best, found
}

func hasElementPrefix(spellName, element string) bool {
	if len(element) > len(spellName) {
		return false
	}
	for i := 0; i < len(element); i++ {
		if lowerByte(spellName[i]) != lowerByte(element[i]) {
			return false
		}
	}
	// The element must be a whole word, so the next character is the separating space.
	return len(spellName) > len(element) && spellName[len(element)] == ' '
}

// OpenAutocastMenu clicks combat_staff_2:auto_choose, which swaps the staff's weapon tab for
// the spell list. Requires a staff to be wielded, since that is what puts combat_staff_2 in
// sidebar slot 0 in the first place.
func OpenAutocastMenu(s Session) error {
	return IfButton(s, AutocastChooseButton)
}

// ChooseAutocastSpell clicks one spell's button in the open spell list. The server refuses it
// silently — no varp change — when the account lacks the Magic level, the runes, or members
// for a Wave, so verify with AutocastSpell rather than assuming.
func ChooseAutocastSpell(s Session, spell Spell) error {
	return IfButton(s, spell.Button)
}

// ToggleAutocast clicks combat_staff_2:auto_toggle, flipping autocasting on or off. The
// server refuses it with "You need to choose a spell first." while no spell is set.
func ToggleAutocast(s Session) error {
	return IfButton(s, AutocastToggleButton)
}

// WHICH spell is autocasting cannot be read by this client at all.
//
// Confirmed live, not reasoned: with a staff wielded, mind runes in the pack and Wind Strike
// accepted by the server, varp 59 read 0 while varp 108 read 2 — and 2 is the value
// set_autocast_spell writes only after it has stored the spell. The cause is in the varp
// config: content/scripts/_unpack/225/all.varp declares
//
//	[autocast_spell]
//	scope=perm
//
// with no `transmit=yes`, while [attackstyle_magic] has `protect=no` and `transmit=yes`. The
// engine's Player.setVar only writes a varp to the client when transmit is true
// (engine/src/engine/entity/Player.ts), so varp 59 never reaches any client, real or headless
// — see gamestate/varp.go's own doc comment on untransmitted varps reading back as a silent
// zero, indistinguishable from "never set".
//
// So the readable state is attackstyle_magic alone: 0 means no spell chosen, 2 means a spell
// is chosen with autocasting off, 3 means autocasting. A caller that needs to know WHICH
// spell must remember what it set.

// AutocastSpell returns the spell the server has stored, read from varp 59.
//
// On content branch 289 this ALWAYS returns false, because varp 59 is not transmitted (see
// the note above). It is kept because the read is correct the moment a build marks that varp
// transmit=yes, and because a caller reaching for it should meet this explanation rather than
// a silently-zero answer. Verify autocast with AutocastEnabled instead.
func AutocastSpell(s Session) (Spell, bool) {
	id := s.State().Varp(VarpAutocastSpell)
	if id == 0 {
		return Spell{}, false
	}
	return SpellByID(id)
}

// AutocastEnabled reports whether the account is actually autocasting.
//
// Content's own test (player_autocast_enabled) also requires %autocast_spell > 0, but that
// varp never reaches this client, so testing it here would make this function permanently
// false. attackstyle_magic == 3 is only ever reached via set_autocast_spell (which sets 2)
// followed by auto_toggle (which flips bit 0), and auto_toggle refuses outright while the
// value is 0 — so 3 already implies a spell is stored.
func AutocastEnabled(s Session) bool {
	return s.State().Varp(VarpAttackStyleMagic) == attackStyleMagicAutocasting
}

// AutocastSpellChosen reports whether a spell is stored at all, whether or not autocasting is
// switched on — the state where ToggleAutocast will be accepted rather than refused.
func AutocastSpellChosen(s Session) bool {
	v := s.State().Varp(VarpAttackStyleMagic)
	return v == attackStyleMagicSpellChosen || v == attackStyleMagicAutocasting
}

// noRunesLine matches the server's own out-of-runes refusal. Content writes it as
// `mes("You do not have enough <rune> Runes to cast this spell.")` in
// skill_magic/scripts/magic.rs2's check_spell_requirements, with the rune name interpolated,
// so the pattern has to span it. This is the Go port of the TypeScript client's
// fighterPlan.ts isNoRunesLine, whose regex was /you do not have enough .*rune/i — kept
// deliberately identical in looseness so both clients classify the same lines.
var noRunesLine = regexp.MustCompile(`(?i)you do not have enough .*rune`)

// IsNoRunesLine reports whether a MESSAGE_GAME line is the server saying a spell could not be
// cast for want of runes — the signal a magic script needs to stop casting and restock,
// rather than silently meleeing on.
func IsNoRunesLine(text string) bool {
	return noRunesLine.MatchString(text)
}

var spellLevelLine = regexp.MustCompile(`(?i)your magic level is not high enough`)

// IsSpellLevelLine reports whether a line is the server refusing a spell for want of Magic
// level (magic.rs2's "Your Magic level is not high enough for this spell.").
func IsSpellLevelLine(text string) bool {
	return spellLevelLine.MatchString(text)
}

var membersSpellLine = regexp.MustCompile(`(?i)members' server to use this spell`)

// IsMembersSpellLine reports whether a line is the server refusing a spell because the world
// is not members (magic.rs2's "You need to be on a members' server to use this spell.") —
// the refusal every Wave gets on a free world.
func IsMembersSpellLine(text string) bool {
	return membersSpellLine.MatchString(text)
}
