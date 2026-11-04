package combatrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/combatplan"
)

// The live half of training something other than melee: pointing a bow at Ranged, keeping a
// staff autocasting, and reading back what is actually worn.
//
// trainer.go's EnsureTrainingStyle covers melee, where the answer is one of four style buttons
// on the weapon's own tab. Neither of the other two disciplines works that way:
//
//   - A bow's tab (combat_bow, combat_crossbow, combat_thrown) is labelled Accurate, Rapid and
//     Longrange, and every one of those pays Ranged. There is no "train Ranged" button to find,
//     so the choice is about speed rather than about which stat advances. Rapid is the one a
//     trainer wants: the same Ranged XP per point of damage as Accurate, more shots per minute.
//     Longrange splits the XP with Defence, which is the wrong answer once melee is finished.
//   - A staff has melee style buttons like any other weapon, and using them would swing it.
//     Magic XP comes from the spell, so the lever is autocast, not the style tab.
//
// The rule from runner.go still governs: internal/actions is fire-and-forget, so nothing here
// reports success on a click. Each function sends and then verifies against observed state.

// RangedStyle is the style a Ranged trainer selects. See the note above for why Rapid rather
// than Accurate or Longrange.
const RangedStyle = actions.StyleRapid

// EnsureRangedStyle points the worn bow at Rapid, confirmed against the combat mode the client
// reports rather than against the click.
//
// It answers false rather than erroring when the weapon has no ranged tab at all, because that
// is not a failure of this function: it means the bow never got equipped, and the caller's own
// "is a bow worn" check is the honest place for that to surface.
func EnsureRangedStyle(d *Deps) bool {
	if actions.HasCombatStyle(d.Session, RangedStyle) {
		return true
	}
	if _, err := actions.SetCombatStyle(d.Session, RangedStyle); err != nil {
		d.logf("set the ranged style: %v", err)
		return false
	}
	if waitFor(d, "the ranged style to change", 3*time.Second, func() bool {
		return actions.HasCombatStyle(d.Session, RangedStyle)
	}) {
		d.logf("combat style is now %s", RangedStyle)
		return true
	}
	return false
}

// EnsureStyleFor points the weapon at whatever the chosen discipline needs.
//
// Melee delegates to EnsureTrainingStyle, which needs to know WHICH melee stat; Ranged sets the
// bow's own style; Magic returns true because the lever there is autocast, handled separately by
// the caller, which is the only thing that knows which spell it last set.
func EnsureStyleFor(d *Deps, style combatplan.Style, meleeStat combatplan.Skill) bool {
	switch style {
	case combatplan.StyleRanged:
		return EnsureRangedStyle(d)
	case combatplan.StyleMagic:
		return true
	default:
		return EnsureTrainingStyle(d, meleeStat)
	}
}

// ---- reading what is worn ----

// bowWords and staffWords are how this trainer recognises what it is holding, by the display
// names the gear table and Zaff's shelf actually use.
var (
	bowWords   = []string{"shortbow", "longbow", "bow"}
	staffWords = []string{"staff"}
)

func wearingAnyOf(d *Deps, words []string) bool {
	for _, name := range WornNames(d) {
		lower := strings.ToLower(name)
		for _, w := range words {
			if strings.Contains(lower, w) {
				return true
			}
		}
	}
	return false
}

// WieldingBow reports whether a bow is actually on the body. Worn, not carried: a bow in the
// pack fires nothing, and the whole point of the Ranged proof's melee-XP control is that an
// unequipped bow shows up as the account punching the monster instead.
func WieldingBow(d *Deps) bool { return wearingAnyOf(d, bowWords) }

// WieldingStaff reports whether a staff is on the body, which is what puts combat_staff_2 in
// the weapon tab and so what makes the autocast buttons clickable at all.
func WieldingStaff(d *Deps) bool { return wearingAnyOf(d, staffWords) }

// WornCount totals the quantity of a named item in the WORN panel, not the pack.
//
// Arrows are the reason this exists: they are equipped into the ammunition slot, so once a run
// is firing them HeldCount reads zero while hundreds are nocked. A restock decision that asked
// the pack would restock forever.
func WornCount(d *Deps, name string) int {
	if d.Config == nil {
		return 0
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	n := 0
	for _, it := range eq.Worn() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && strings.EqualFold(obj.Name, name) {
			c := int(it.Count)
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// ArrowsAvailable is every arrow of one kind the account can still fire: the ones already
// nocked plus the ones in the pack.
func ArrowsAvailable(d *Deps, arrowName string) int {
	return WornCount(d, arrowName) + HeldCount(d, arrowName)
}

// ---- the bridge to the pure spell planner ----

// SpellOptions converts the autocast spell table into the shape combatplan reasons about.
//
// It exists so combatplan can decide which spell is actually suppliable without importing
// internal/actions, which would break the rule that every decision in that package is testable
// with no session and no client. The table itself stays in actions, next to the clicks, so
// there is one copy of content's spell list rather than two that can drift.
func SpellOptions() []combatplan.SpellOption {
	spells := actions.AutocastSpells()
	out := make([]combatplan.SpellOption, 0, len(spells))
	for _, s := range spells {
		runes := make([]combatplan.RuneCost, 0, len(s.Runes))
		for _, r := range s.Runes {
			runes = append(runes, combatplan.RuneCost{Name: r.Name, Count: r.Count})
		}
		out = append(out, combatplan.SpellOption{
			Name: s.Name, Level: s.Level, Members: s.Members, Runes: runes,
		})
	}
	return out
}

// AutocastOn reports whether the server says the account is autocasting.
//
// This is the ONLY half of the autocast state any client can read. Which spell is set is
// unreadable: content declares autocast_spell (varp 59) without transmit=yes, so the engine
// never puts it on the wire, and it was measured reading 0 while autocasting was plainly on.
// A caller that needs to know which spell is running must remember what it set, which is why
// EnsureAutocast takes the caller's own record rather than reading one.
func AutocastOn(d *Deps) bool { return actions.AutocastEnabled(d.Session) }

// EnsureAutocast makes sure the account is autocasting want, given the spell the caller last
// set successfully.
//
// current is the caller's own memory, empty when it has never set one or when the run has
// restarted. The three-click sequence is re-run when the remembered spell is not the wanted one
// or when the server says autocasting is off; it is skipped when both agree, because the
// sequence takes a second and a half and Loop is called constantly.
//
// The honest limit is the unreadable varp: if something outside this script changed the spell,
// this cannot tell, because nothing can. What it can tell is that autocasting stopped, which is
// what an empty rune pack looks like, and that re-runs the sequence.
func EnsureAutocast(d *Deps, want actions.Spell, current string, timeout time.Duration) error {
	if strings.EqualFold(current, want.Name) && AutocastOn(d) {
		return nil
	}
	if err := SetAutocast(d, want, timeout); err != nil {
		return fmt.Errorf("combatrunner: could not set %s to autocast: %w", want.Name, err)
	}
	return nil
}
