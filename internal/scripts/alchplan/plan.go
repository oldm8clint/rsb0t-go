// Package alchplan is Alcher's own decisions, ported from rsb0t-scripts' alchPlan.ts and the
// state machine embedded in Alcher.ts. It is pure: no client, actions, fleet or bot imports and
// no I/O.
//
// Deliberately not ported here, and why:
//   - alchHud.ts, hudPanel.ts, bankFeed.ts, tallyFeed.ts, updateCheck.ts — the Electron wall's
//     on-screen overlay/update-checker/feed publishers; no headless equivalent.
//   - decideAlch/spareRunes (alchPlan.ts's own "alching as an idle job" section) — that pair
//     exists only for a second, not-yet-ported TS consumer ("the mule's idle alching" per
//     alchCaster.ts's own doc comment), not for Alcher.ts itself. Nothing in this port needs
//     it; add it back if a future script wants to alch between other duties.
package alchplan

import (
	"regexp"
	"strings"
)

// AlchSpell is a castable alchemy spell and what it costs.
type AlchSpell struct {
	Name string
	// Level is the Magic level the spell unlocks at.
	Level int
	// FireRunes is fire runes per cast without a fire staff; every cast also takes one nature
	// rune.
	FireRunes int
	XP        int
	// LockTicks is how many ticks the cast locks the character for; a click inside the lock is
	// dropped, not queued.
	LockTicks int
}

var LowAlch = AlchSpell{Name: "Low Level Alchemy", Level: 21, FireRunes: 3, XP: 31, LockTicks: 3}
var HighAlch = AlchSpell{Name: "High Level Alchemy", Level: 55, FireRunes: 5, XP: 65, LockTicks: 5}

const AutoSpell = "Best for level"

var SpellOptions = []string{AutoSpell, HighAlch.Name, LowAlch.Name}

const (
	NatureRune = "Nature rune"
	FireRune   = "Fire rune"
	Coins      = "Coins"
)

// FireStaffs supply fire runes, best first, so the bank's best one is the one worn.
var FireStaffs = []string{"Mystic fire staff", "Fire battlestaff", "Lava battlestaff", "Steam battlestaff", "Staff of fire"}

const DefaultItems = "Rune platebody"
const DefaultCastsPerTrip = 100

// CastTimeoutTicks: the xp lands within a tick or two of the click; past this the click was dropped.
const CastTimeoutTicks = 4

// CastsPerLoop is casts made back to back inside one loop before control returns to the runner.
const CastsPerLoop = 25
const MagicTab = 6

func IsFireStaff(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	for _, s := range FireStaffs {
		if strings.ToLower(s) == key {
			return true
		}
	}
	return false
}

// ChooseSpell ports chooseSpell: the setting's spell when the level allows it, or for the
// automatic choice the best one it allows. nil when neither is castable yet.
func ChooseSpell(setting string, magic int) *AlchSpell {
	key := strings.ToLower(strings.TrimSpace(setting))
	switch key {
	case strings.ToLower(HighAlch.Name):
		if magic >= HighAlch.Level {
			return &HighAlch
		}
		return nil
	case strings.ToLower(LowAlch.Name):
		if magic >= LowAlch.Level {
			return &LowAlch
		}
		return nil
	}
	if magic >= HighAlch.Level {
		return &HighAlch
	}
	if magic >= LowAlch.Level {
		return &LowAlch
	}
	return nil
}

// ParseItems tidies a comma-separated list: trimmed, blanks and repeats dropped, runes and
// coins never alched.
func ParseItems(raw string) []string {
	banned := map[string]bool{
		strings.ToLower(NatureRune): true,
		strings.ToLower(FireRune):   true,
		strings.ToLower(Coins):      true,
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		key := strings.ToLower(name)
		if name == "" || seen[key] || banned[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

func FireRunesPerCast(spell AlchSpell, hasStaff bool) int {
	if hasStaff {
		return 0
	}
	return spell.FireRunes
}

// CastsAffordable is how many casts the runes in hand fund.
func CastsAffordable(spell AlchSpell, nature, fire int, hasStaff bool) int {
	n := nature
	if n < 0 {
		n = 0
	}
	perCast := FireRunesPerCast(spell, hasStaff)
	if perCast <= 0 {
		// No fire cost at all (a fire staff worn) — bounded only by nature runes.
		return n
	}
	byFire := fire / perCast
	if byFire < 0 {
		byFire = 0
	}
	if byFire < n {
		return byFire
	}
	return n
}

// RunesToWithdraw is runes to draw so the pack funds casts casts on top of what it already
// holds.
func RunesToWithdraw(spell AlchSpell, casts int, heldNature, heldFire int, hasStaff bool) (nature, fire int) {
	want := casts
	if want < 0 {
		want = 0
	}
	nature = want - heldNature
	if nature < 0 {
		nature = 0
	}
	fire = want*FireRunesPerCast(spell, hasStaff) - heldFire
	if fire < 0 {
		fire = 0
	}
	return nature, fire
}

// ItemsForNatures is the listed items a restock may keep in the pack or draw from the bank: all of them while the
// account has a Nature rune to cast on them with, in the pack or in the bank, and none without (a user request:
// "if accounts dont have nature runes they shouldnt take things to alch"). A restock with no natures anywhere banks
// the items and draws none, and the run ends on "no runes left" rather than carrying notes it cannot cast.
func ItemsForNatures(items []string, packNatures, bankNatures int) []string {
	if packNatures <= 0 && bankNatures <= 0 {
		return nil
	}
	return items
}

// NextItem returns the first listed item the pack still holds, in the order given.
func NextItem(items []string, count func(name string) int) (string, bool) {
	for _, name := range items {
		if count(name) > 0 {
			return name, true
		}
	}
	return "", false
}

// ItemToWithdraw returns the first listed item the bank still holds and how many to draw; ok
// is false when the list is exhausted.
func ItemToWithdraw(items []string, bankCount func(name string) int, want int) (name string, count int, ok bool) {
	for _, n := range items {
		stock := bankCount(n)
		if stock > 0 {
			c := want
			if c > stock {
				c = stock
			}
			if c < 1 {
				c = 1
			}
			return n, c, true
		}
	}
	return "", 0, false
}

// ---- stages ----

type Stage string

const (
	StageGear    Stage = "gear"
	StageRestock Stage = "restock"
	StageAlch    Stage = "alch"
	StageDone    Stage = "done"
)

type Snapshot struct {
	// ItemInPack: a listed item is in the pack.
	ItemInPack bool
	// CastsInPack: casts the runes in the pack fund.
	CastsInPack int
	// StaffWanted: the run wants a fire staff worn and has not yet looked in the bank for one.
	StaffWanted  bool
	HasStaff     bool
	StaffChecked bool
	// BankEmpty: the bank held none of the listed items at the last restock.
	BankEmpty bool
}

// DecideStage ports decideStage: finished when the bank and pack are both out of items, the
// staff looked for once before anything else, a restock when the pack cannot fund a cast, else
// casts.
func DecideStage(s Snapshot) Stage {
	if s.BankEmpty && !s.ItemInPack {
		return StageDone
	}
	if s.StaffWanted && !s.HasStaff && !s.StaffChecked {
		return StageGear
	}
	if !s.ItemInPack || s.CastsInPack <= 0 {
		return StageRestock
	}
	return StageAlch
}

// ---- chat lines ----

// notEnough matches the server refusing a cast for want of runes — ported from NOT_ENOUGH.
var notEnough = regexp.MustCompile(`(?i)(?:do not have enough|don't have enough|not enough .*runes?)`)

// levelTooLow matches the server refusing a cast for want of level — ported from LEVEL_TOO_LOW.
var levelTooLow = regexp.MustCompile(`(?i)need a magic level of`)

func IsNotEnoughRunesMessage(text string) bool { return notEnough.MatchString(text) }
func IsLevelTooLowMessage(text string) bool    { return levelTooLow.MatchString(text) }

func DescribeStage(s Stage) string {
	switch s {
	case StageGear:
		return "fetching a fire staff"
	case StageRestock:
		return "restocking at the bank"
	case StageAlch:
		return "alching"
	case StageDone:
		return "finished"
	default:
		return string(s)
	}
}
