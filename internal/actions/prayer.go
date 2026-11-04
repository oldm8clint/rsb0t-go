package actions

import (
	"errors"

	"rsb0t-go/internal/scripts/levels"
)

// Prayer activation, reading which prayers are on, and prayer points.
//
// Nothing in this client had any prayer support before this file. The three pieces a caller
// needs are all here because they are inseparable in practice: a prayer button is a plain
// IF_BUTTON like the run/retaliate toggles in controls.go, but unlike those it is a *toggle*
// whose current state the server publishes in a varp, and activating it at all depends on
// having prayer points left.
//
// Where every id below comes from, all content branch 289:
//
//   - Component ids: pack/interface.pack's `prayer` block. The tab root is 5608 and the
//     fifteen buttons are 5609..5623 in the order skill_prayer/interfaces/prayer.if declares
//     them (thick skin first, protect from melee last).
//   - Varp ids: pack/varp.pack maps prayer0..prayer14 to varps 83..97, one per prayer in the
//     same order. Each is declared `transmit=yes` in skill_prayer/configs/prayer.varp, so
//     unlike most quest varps these really do reach this client (see gamestate/varp.go's own
//     doc comment on untransmitted varps reading back as a silent zero).
//   - Level requirements: skill_prayer/configs/prayers.dbrow's `data=level,N` per prayer.
//
// The server script behind each button (skill_prayer/scripts/prayers/*.rs2, e.g.
// protectfrommelee.rs2) is a toggle: one click with the prayer already on turns it OFF. That
// is why SetPrayer reads the varp first and only clicks when the state actually needs to
// change — a blind "activate" click on an already-active prayer would silently disable it,
// which for Protect from Melee at a boss is the difference between a run and a death.
//
// The same script also refuses the activation outright, with no varp change, when
// stat(prayer) < 1 (no prayer points: "You need to recharge your prayer at an altar.") or
// when stat_base(prayer) is below the prayer's level requirement. Both are silent as far as
// the varp is concerned, so a caller must verify with PrayerActive rather than assume the
// click worked — the same fire-and-forget rule every other action in this package follows.

// PrayerStat is UPDATE_STAT's index for Prayer, per gamestate.SkillNames' own citation of
// the engine's PlayerStat enum: attack, defence, strength, hitpoints, ranged, prayer, ...
const PrayerStat = 5

// PrayerTabRoot is the prayer interface's root component (pack/interface.pack's `prayer`).
// The engine only accepts an IF_BUTTON whose component's root layer is one of the player's
// currently assigned tabs (Player.isComponentVisible), and ~initalltabs assigns this one at
// login, so no client-side "open the tab" step is needed or possible — but an account whose
// tabs were hidden (the Tutorial Island trap documented in cmd/equiptest) will have every
// prayer click silently dropped.
const PrayerTabRoot = 5608

// Prayer is one entry of the prayer tab: its button, the varp the server reports its on/off
// state in, and the Prayer level it needs.
type Prayer struct {
	// Name is the prayer's own display name, exactly as prayers.dbrow's `data=name` spells it.
	Name string
	// ComID is the button's component id (IF_BUTTON).
	ComID int
	// Varp is the player variable holding 1 while this prayer is active, 0 otherwise.
	Varp int
	// Level is the base Prayer level the server requires.
	Level int
}

// The fifteen prayers, in tab order. Component id, varp and level are each sourced as
// described in this file's doc comment.
var (
	ThickSkin           = Prayer{"Thick Skin", 5609, 83, 1}
	BurstOfStrength     = Prayer{"Burst of Strength", 5610, 84, 4}
	ClarityOfThought    = Prayer{"Clarity of Thought", 5611, 85, 7}
	RockSkin            = Prayer{"Rock Skin", 5612, 86, 10}
	SuperhumanStrength  = Prayer{"Superhuman Strength", 5613, 87, 13}
	ImprovedReflexes    = Prayer{"Improved Reflexes", 5614, 88, 16}
	RapidRestore        = Prayer{"Rapid Restore", 5615, 89, 19}
	RapidHeal           = Prayer{"Rapid Heal", 5616, 90, 22}
	ProtectItem         = Prayer{"Protect Item", 5617, 91, 25}
	SteelSkin           = Prayer{"Steel Skin", 5618, 92, 28}
	UltimateStrength    = Prayer{"Ultimate Strength", 5619, 93, 31}
	IncredibleReflexes  = Prayer{"Incredible Reflexes", 5620, 94, 34}
	ProtectFromMagic    = Prayer{"Protect from Magic", 5621, 95, 37}
	ProtectFromMissiles = Prayer{"Protect from Missiles", 5622, 96, 40}
	ProtectFromMelee    = Prayer{"Protect from Melee", 5623, 97, 43}
)

var allPrayers = []Prayer{
	ThickSkin, BurstOfStrength, ClarityOfThought, RockSkin, SuperhumanStrength,
	ImprovedReflexes, RapidRestore, RapidHeal, ProtectItem, SteelSkin,
	UltimateStrength, IncredibleReflexes, ProtectFromMagic, ProtectFromMissiles,
	ProtectFromMelee,
}

// ErrNoSuchPrayer is returned for a name no prayer answers to.
var ErrNoSuchPrayer = errors.New("actions: no such prayer")

// ErrPrayerLevel is returned when the account's base Prayer level is below the prayer's own
// requirement, so the server would refuse the click without changing anything.
var ErrPrayerLevel = errors.New("actions: prayer level too low")

// ErrNoPrayerPoints is returned when the account has no prayer points left, which the server
// refuses the same silent way (prayer.rs2's `if (stat(prayer) < 1)`).
var ErrNoPrayerPoints = errors.New("actions: no prayer points left")

// Prayers returns every prayer in tab order.
func Prayers() []Prayer {
	return append([]Prayer(nil), allPrayers...)
}

// PrayerByName finds a prayer by its display name, case-insensitively.
func PrayerByName(name string) (Prayer, bool) {
	for _, p := range allPrayers {
		if foldCompare(p.Name, name) == 0 {
			return p, true
		}
	}
	return Prayer{}, false
}

// foldCompare is a local case-insensitive comparison, kept here rather than pulling in
// strings.EqualFold's allocation-free path only to avoid an import for one call site.
func foldCompare(a, b string) int {
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	for i := 0; i < len(a); i++ {
		ca, cb := lowerByte(a[i]), lowerByte(b[i])
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
	}
	return 0
}

func lowerByte(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
}

// PrayerActive reports whether the server currently has this prayer switched on, read from
// its own varp. False before the server has ever sent that varp, which is also the correct
// answer: a prayer the server never mentioned is off.
func PrayerActive(s Session, p Prayer) bool {
	return s.State().Varp(p.Varp) == 1
}

// ActivePrayers returns every prayer currently switched on.
func ActivePrayers(s Session) []Prayer {
	var on []Prayer
	for _, p := range allPrayers {
		if PrayerActive(s, p) {
			on = append(on, p)
		}
	}
	return on
}

// PrayerPoints returns the account's current prayer points — UPDATE_STAT's live "level" byte
// for the Prayer stat, which drains while a prayer is on and refills at an altar, exactly the
// current/max split gamestate.State.HP uses for Hitpoints. ok is false until the first
// UPDATE_STAT for Prayer has arrived.
func PrayerPoints(s Session) (int, bool) {
	sk, ok := s.State().Skill(PrayerStat)
	if !ok {
		return 0, false
	}
	return sk.Level, sk.Generation > 0
}

// PrayerLevel returns the account's base Prayer level, derived from its Prayer XP — the
// value the server's own `stat_base(prayer)` level checks compare against, which is NOT the
// same as PrayerPoints once any points have drained. ok is false until the first UPDATE_STAT
// for Prayer has arrived.
func PrayerLevel(s Session) (int, bool) {
	sk, ok := s.State().Skill(PrayerStat)
	if !ok || sk.Generation == 0 {
		return 0, false
	}
	level := levels.LevelFromXP(sk.XP)
	// A drained stat never reads below its base, but a base level the XP curve puts lower
	// than a currently-observed value would be wrong in the direction that matters (it would
	// refuse a prayer the account can actually use), so take the higher of the two — the same
	// reasoning gamestate.MaxHP's own doc comment records for Hitpoints on this content build.
	if sk.Level > level {
		level = sk.Level
	}
	return level, true
}

// CanUsePrayer reports whether the account's base Prayer level meets p's requirement and it
// has at least one prayer point, the two conditions the server checks before honouring a
// click. ok is false when the Prayer stat has not been received yet, so a caller can tell
// "not allowed" from "not known yet".
func CanUsePrayer(s Session, p Prayer) (allowed bool, ok bool) {
	level, haveLevel := PrayerLevel(s)
	points, havePoints := PrayerPoints(s)
	if !haveLevel || !havePoints {
		return false, false
	}
	return level >= p.Level && points > 0, true
}

// SetPrayer switches p on or off, clicking its button only when the server's own varp says
// the state actually needs to change.
//
// Why the read first: the button is a toggle (skill_prayer/scripts/prayers/*.rs2), so an
// "activate" click sent while the prayer is already on turns it off. A caller that simply
// clicks every tick to "make sure" would flicker the prayer on and off, draining points and
// leaving it off half the time.
//
// Returns nil without sending anything when the prayer is already in the requested state.
// Turning one ON also refuses early, without sending, when the account's level or points
// cannot support it — the server would ignore that click silently and the caller would be
// left believing a prayer was on. Turning one OFF never checks either, since the server
// honours a deactivation regardless.
func SetPrayer(s Session, p Prayer, on bool) error {
	if PrayerActive(s, p) == on {
		return nil
	}
	if on {
		level, haveLevel := PrayerLevel(s)
		if haveLevel && level < p.Level {
			return ErrPrayerLevel
		}
		points, havePoints := PrayerPoints(s)
		if havePoints && points < 1 {
			return ErrNoPrayerPoints
		}
	}
	return IfButton(s, p.ComID)
}

// ActivatePrayer switches p on. See SetPrayer for why it is not an unconditional click.
func ActivatePrayer(s Session, p Prayer) error { return SetPrayer(s, p, true) }

// DeactivatePrayer switches p off.
func DeactivatePrayer(s Session, p Prayer) error { return SetPrayer(s, p, false) }

// DeactivateAllPrayers switches off every prayer currently on, one click each.
func DeactivateAllPrayers(s Session) error {
	for _, p := range ActivePrayers(s) {
		if err := DeactivatePrayer(s, p); err != nil {
			return err
		}
	}
	return nil
}
