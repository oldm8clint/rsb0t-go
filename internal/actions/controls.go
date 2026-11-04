package actions

import (
	"errors"
	"strings"
)

// Game controls: the run toggle, auto retaliate and the weapon's combat style, porting the
// setters in the TS scripting API's Game namespace (Game.ts, ClientAdapter.setRun and
// setRetaliate). Each is a plain IF_BUTTON on a component the engine handles by name, so
// the ids below come from content branch 289's pack/interface.pack, and the handlers from its
// scripts:
//
//   - interface_controls/scripts/player_controls.rs2: controls:com_2 sets auto retaliate on,
//     com_3 off; com_4 turns run off, com_5 on (it stays off below 1% energy).
//   - skill_combat/scripts/player/player_attackstyles.rs2: each weapon tab's style button calls
//     ~set_attackstyle(n), which sets the com_mode varp to n.
//
// Upstream finds these buttons by scanning the live component tree (the controls root with an
// "Auto retaliate" label, and BUTTON_SELECT components comparing com_mode). This client's
// archive decoder keeps neither labels by position nor button scripts, so the result of that
// scan is tabled here instead. The state they change is read back through
// gamestate.State.RunEnabled, AutoRetaliateOn and CombatMode.
//
// The engine ignores an IF_BUTTON on a component that is not visible, so these need the
// relevant side tab loaded (Tutorial Island hides them until its script unlocks each tab).

// Controls tab buttons (interface "controls", id 147).
const (
	ControlsRetaliateOn  = 150 // controls:com_2
	ControlsRetaliateOff = 151 // controls:com_3
	ControlsRunOff       = 152 // controls:com_4
	ControlsRunOn        = 153 // controls:com_5
)

// CombatTab is the sidebar tab index of the weapon's combat-style interface.
const CombatTab = 0

// ErrNoCombatStyle is returned when the current weapon's tab offers no button for the
// requested mode or style.
var ErrNoCombatStyle = errors.New("actions: the current combat tab has no such style")

// SetRun ports Game.setRun: the controls tab's run on/off button.
func SetRun(s Session, on bool) error {
	if on {
		return IfButton(s, ControlsRunOn)
	}
	return IfButton(s, ControlsRunOff)
}

// SetAutoRetaliate ports Game.setAutoRetaliate.
func SetAutoRetaliate(s Session, on bool) error {
	if on {
		return IfButton(s, ControlsRetaliateOn)
	}
	return IfButton(s, ControlsRetaliateOff)
}

// CombatModeButton is one style button on a weapon's combat tab.
type CombatModeButton struct {
	// Mode is the com_mode value the button selects.
	Mode int
	// ComID is the button's component id.
	ComID int
	// Label is the style caption beside it: Accurate, Aggressive, Controlled or Defensive
	// for melee, Accurate, Rapid or Longrange for ranged.
	Label string
}

// combatTabs maps each combat-style interface root (what IF_SETICON loads into CombatTab) to
// its buttons in mode order. Ids are content 289's pack/interface.pack; each label is the
// "(Style)" text in the same layer whose vertical centre falls inside the button, the rule
// ClientAdapter's readSelectButtonLabelsByVarp uses, resolved from the interface's .if source.
var combatTabs = map[int][]CombatModeButton{
	328:  {{0, 336, "Accurate"}, {1, 335, "Aggressive"}, {2, 334, "Defensive"}},                               // combat_staff_2
	425:  {{0, 433, "Accurate"}, {1, 432, "Aggressive"}, {2, 431, "Defensive"}},                               // combat_blunt
	776:  {{0, 782, "Accurate"}, {1, 785, "Aggressive"}, {2, 784, "Aggressive"}, {3, 783, "Defensive"}},       // combat_scythe
	1698: {{0, 1704, "Accurate"}, {1, 1707, "Aggressive"}, {2, 1706, "Aggressive"}, {3, 1705, "Defensive"}},   // combat_axe
	1749: {{0, 1757, "Accurate"}, {1, 1756, "Rapid"}, {2, 1755, "Longrange"}},                                 // combat_crossbow
	1764: {{0, 1772, "Accurate"}, {1, 1771, "Rapid"}, {2, 1770, "Longrange"}},                                 // combat_bow
	2276: {{0, 2282, "Accurate"}, {1, 2285, "Aggressive"}, {2, 2284, "Aggressive"}, {3, 2283, "Defensive"}},   // combat_stabsword
	2423: {{0, 2429, "Accurate"}, {1, 2432, "Aggressive"}, {2, 2431, "Controlled"}, {3, 2430, "Defensive"}},   // combat_hacksword
	3796: {{0, 3802, "Accurate"}, {1, 3805, "Aggressive"}, {2, 3804, "Controlled"}, {3, 3803, "Defensive"}},   // combat_spiked
	4446: {{0, 4454, "Accurate"}, {1, 4453, "Rapid"}, {2, 4452, "Longrange"}},                                 // combat_thrown
	4679: {{0, 4685, "Controlled"}, {1, 4688, "Controlled"}, {2, 4687, "Controlled"}, {3, 4686, "Defensive"}}, // combat_spear
	4705: {{0, 4711, "Accurate"}, {1, 4714, "Aggressive"}, {2, 4713, "Aggressive"}, {3, 4712, "Defensive"}},   // combat_heavysword
	5570: {{0, 5576, "Accurate"}, {1, 5579, "Aggressive"}, {2, 5578, "Aggressive"}, {3, 5577, "Defensive"}},   // combat_pickaxe
	5855: {{0, 5860, "Accurate"}, {1, 5862, "Aggressive"}, {2, 5861, "Defensive"}},                            // combat_unarmed
	7762: {{0, 7768, "Accurate"}, {1, 7771, "Aggressive"}, {2, 7770, "Controlled"}, {3, 7769, "Defensive"}},   // combat_claw
	8460: {{0, 8466, "Controlled"}, {1, 8468, "Aggressive"}, {2, 8467, "Defensive"}},                          // combat_polearm
}

// CombatModesFor returns the style buttons of the combat interface rootComID, or nil when it
// is not a known combat tab.
func CombatModesFor(rootComID int) []CombatModeButton {
	return append([]CombatModeButton(nil), combatTabs[rootComID]...)
}

// CombatModes ports Game.combatStyles: the current weapon tab's buttons, or nil before
// IF_SETICON has loaded a known combat tab.
func CombatModes(s Session) []CombatModeButton {
	return CombatModesFor(s.State().SideIcon(CombatTab))
}

// SetCombatMode ports Game.setCombatMode: select the button for com_mode value mode.
func SetCombatMode(s Session, mode int) error {
	root := s.State().SideIcon(CombatTab)
	buttons, ok := combatTabs[root]
	if !ok {
		return ErrNotOnScreen
	}
	for _, b := range buttons {
		if b.Mode == mode {
			return IfButton(s, b.ComID)
		}
	}
	return ErrNoCombatStyle
}

// CombatStyle is a melee training style, as scripts name them.
type CombatStyle string

const (
	StyleAttack     CombatStyle = "attack"
	StyleStrength   CombatStyle = "strength"
	StyleControlled CombatStyle = "controlled"
	StyleDefence    CombatStyle = "defence"
	// StyleRapid and StyleLongrange are the ranged tabs' own styles. They are here because
	// without them a bow could not be pointed at anything: combat_bow, combat_crossbow and
	// combat_thrown label their buttons Accurate, Rapid and Longrange, labelStyle knew only the
	// four melee captions, so ResolveCombatStyle fell through to its Defensive fallback, found
	// no Defensive button on a ranged tab either, and answered "this weapon has no such style".
	// A Ranged run therefore had no way to choose between paying Ranged quickly and paying
	// Ranged and Defence together. Rapid is the one a trainer wants: same Ranged XP per hit as
	// Accurate, more hits per minute.
	StyleRapid     CombatStyle = "rapid"
	StyleLongrange CombatStyle = "longrange"
)

// ParseCombatStyle ports tryParseCombatStyle: the style a script setting names, accepting
// the interface's own words (accurate, aggressive, shared, defensive) and "defense".
func ParseCombatStyle(name string) (CombatStyle, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "attack", "accurate":
		return StyleAttack, true
	case "strength", "aggressive":
		return StyleStrength, true
	case "controlled", "shared":
		return StyleControlled, true
	case "defence", "defense", "defensive":
		return StyleDefence, true
	case "rapid":
		return StyleRapid, true
	case "longrange", "long range":
		return StyleLongrange, true
	}
	return "", false
}

// labelStyle ports parseInterfaceCombatStyle: a melee caption's training style.
func labelStyle(label string) (CombatStyle, bool) {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(label), "()")) {
	case "accurate":
		return StyleAttack, true
	case "aggressive":
		return StyleStrength, true
	case "controlled":
		return StyleControlled, true
	case "defensive":
		return StyleDefence, true
	case "rapid":
		return StyleRapid, true
	case "longrange":
		return StyleLongrange, true
	}
	return "", false
}

// CombatStyleResolution is what a requested style maps to on the current weapon.
type CombatStyleResolution struct {
	Requested CombatStyle
	Effective CombatStyle
	Mode      int
}

// ResolveCombatStyle ports resolveCombatStyle: the first button training style, or, when the
// weapon has none, the last Defensive button (which covers "controlled" on a three-style
// weapon and stays deterministic for spears and polearms). ok is false when neither exists,
// e.g. on a ranged weapon's tab. Duplicate modes keep their first label.
func ResolveCombatStyle(style CombatStyle, buttons []CombatModeButton) (CombatStyleResolution, bool) {
	seen := map[int]bool{}
	fallback := -1
	var fallbackRes CombatStyleResolution
	for _, b := range buttons {
		effective, ok := labelStyle(b.Label)
		if !ok || seen[b.Mode] {
			continue
		}
		seen[b.Mode] = true
		if effective == style {
			return CombatStyleResolution{Requested: style, Effective: effective, Mode: b.Mode}, true
		}
		if effective == StyleDefence {
			fallback = b.Mode
			fallbackRes = CombatStyleResolution{Requested: style, Effective: effective, Mode: b.Mode}
		}
	}
	if fallback == -1 {
		return CombatStyleResolution{}, false
	}
	return fallbackRes, true
}

// SetCombatStyle ports Game.setCombatStyle: resolve style on the current weapon's tab and
// select it. The returned resolution says which style was actually picked.
func SetCombatStyle(s Session, style CombatStyle) (CombatStyleResolution, error) {
	buttons := CombatModes(s)
	if buttons == nil {
		return CombatStyleResolution{}, ErrNotOnScreen
	}
	res, ok := ResolveCombatStyle(style, buttons)
	if !ok {
		return CombatStyleResolution{}, ErrNoCombatStyle
	}
	return res, SetCombatMode(s, res.Mode)
}

// HasCombatStyle ports Game.hasCombatStyle: the current com_mode is the mode style resolves
// to on this weapon.
func HasCombatStyle(s Session, style CombatStyle) bool {
	res, ok := ResolveCombatStyle(style, CombatModes(s))
	return ok && s.State().CombatMode() == res.Mode
}
