package freshstart

import (
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The decisions inside newAccount.ts's steps, without the waits and clicks around them.

// LegTimeout bounds one boat leg: two ships, a walk across Karamja, and on from Ardougne.
const LegTimeout = 10 * time.Minute

// Button is one button under an interface.
type Button struct {
	ComID  int
	Label  string
	Hidden bool
}

// AcceptButton picks the appearance screen's Accept button: the one found by caption when there
// is one (byText >= 0), otherwise the first visible button whose label contains "accept". It
// returns -1 when there is none, and the account must be accepted by hand.
func AcceptButton(byText int, buttons []Button) int {
	if byText >= 0 {
		return byText
	}
	for _, b := range buttons {
		if !b.Hidden && strings.Contains(strings.ToLower(b.Label), "accept") {
			return b.ComID
		}
	}
	return -1
}

// SkipMatch says how the tutorial-skip dialogue option was chosen.
type SkipMatch int

// How ChooseSkipOption matched.
const (
	SkipNone SkipMatch = iota
	SkipWanted
	SkipYes
	SkipFirst
)

// ChooseSkipOption picks the option asking to skip the tutorial: the first containing want
// (case-insensitive), else the first containing "yes", else the first option, which the TS step
// logs a warning for. It returns -1 and SkipNone when there are no options.
func ChooseSkipOption(options []string, want string) (int, SkipMatch) {
	if len(options) == 0 {
		return -1, SkipNone
	}
	if w := strings.ToLower(strings.TrimSpace(want)); w != "" {
		for i, o := range options {
			if strings.Contains(strings.ToLower(o), w) {
				return i, SkipWanted
			}
		}
	}
	for i, o := range options {
		if strings.Contains(strings.ToLower(o), "yes") {
			return i, SkipYes
		}
	}
	return 0, SkipFirst
}

// TutorialAction is the next thing the tutorial step does.
type TutorialAction string

// The tutorial step's actions.
const (
	TutorialChoose   TutorialAction = "choose an option"
	TutorialContinue TutorialAction = "continue the dialogue"
	TutorialNoGuide  TutorialAction = "no guide in reach"
	// TutorialWalk walks beside the guide, then talks to it in the same pass.
	TutorialWalk TutorialAction = "walk to the guide"
	TutorialTalk TutorialAction = "talk to the guide"
)

// NextTutorialAction decides the tutorial step. guideDistance is the nearest guide's distance, or
// -1 when none offering Talk-to is in sight.
func NextTutorialAction(options int, canContinue bool, guideDistance int) TutorialAction {
	switch {
	case options > 0:
		return TutorialChoose
	case canContinue:
		return TutorialContinue
	case guideDistance < 0:
		return TutorialNoGuide
	case guideDistance > 3:
		return TutorialWalk
	}
	return TutorialTalk
}

// The brightness setting, from newAccount.ts.
const (
	// BrightnessVarp is option_brightness: 1 dark, 2 normal, 3 bright, 4 very bright.
	BrightnessVarp = 166
	VeryBright     = 4
	OptionsTab     = 11
	InventoryTab   = 3
	// BrightTries presses are tried before a client that will not take them is left alone.
	BrightTries = 3
)

// VeryBrightButton is the Very Bright button on whichever interface the options tab holds: the
// standard one (root 904) or the low-detail one (4445). They are unlabelled, so pressed by id.
func VeryBrightButton(root int) (comID int, ok bool) {
	switch root {
	case 904:
		return 911, true
	case 4445:
		return 6276, true
	}
	return 0, false
}

// Brightness tracks turning the brightness up, done once per account.
type Brightness struct {
	tries  int
	gaveUp bool
}

// Done reports very bright already, or given up on so the run is not held up for it.
func (b *Brightness) Done(varp int) bool {
	return b.gaveUp || varp == VeryBright
}

// Missed records a press that did not take, and reports true once the run gives up on it.
func (b *Brightness) Missed() bool {
	b.tries++
	if b.tries >= BrightTries {
		b.gaveUp = true
	}
	return b.gaveUp
}

// Refused reports whether a sale the store did not pay for should not be offered again this run.
func Refused(sold, coinsGained int) bool {
	return sold <= 0 || coinsGained <= 0
}

// SellQueue is SellPlan without the names already refused this run (keys lower-case).
func SellQueue(pack []PackItem, keep []string, refused map[string]bool) []SellLine {
	var out []SellLine
	for _, line := range SellPlan(pack, keep) {
		if !refused[strings.ToLower(line.Name)] {
			out = append(out, line)
		}
	}
	return out
}

// FareThief is the pickpocketing state: pockets picked, coins stolen, the stun and the rest.
type FareThief struct {
	Pockets  int
	StolenGP int

	stunned      bool
	stunnedUntil int
	resting      bool
}

// NoteChat reads a game message: a failed attempt says so, and the stun is timed from it.
func (f *FareThief) NoteChat(text string, tick int) {
	t := strings.ToLower(text)
	if strings.Contains(t, "been stunned") || strings.Contains(t, "fail to pick") {
		f.stunned = true
		f.stunnedUntil = tick + StunTicks
	}
}

// Stunned reports whether the account is still stunned at tick.
func (f *FareThief) Stunned(tick int) bool {
	return f.stunned && tick <= f.stunnedUntil
}

// Rest updates resting from the health share; started is true on the pass resting begins.
func (f *FareThief) Rest(hpFraction, restBelow float64) (resting, started bool) {
	was := f.resting
	f.resting = ShouldRestWith(hpFraction, f.resting, restBelow, ResumeAtHP)
	return f.resting, f.resting && !was
}

// Pocketed records a pickpocket that gave experience, and the coins it gained.
func (f *FareThief) Pocketed(coinsGained int) {
	f.Pockets++
	if coinsGained > 0 {
		f.StolenGP += coinsGained
	}
}

// PickpocketSpotFor is where to pick pockets. Off the mainland the configured spot is across a paid
// ship, so the account works where it stands instead (away).
func PickpocketSpotFor(here *tile.Tile, spot tile.Tile) (use tile.Tile, away bool) {
	if here != nil && !InLumbridge(*here) && tile.Chebyshev(*here, spot) > 150 {
		return *here, true
	}
	return spot, false
}

// NPC is one visible NPC, by name.
type NPC struct {
	Name     string
	Tile     tile.Tile
	Distance int
	Ops      []string
}

// HasOp reports whether ops holds op, case-insensitively.
func HasOp(ops []string, op string) bool {
	for _, o := range ops {
		if strings.EqualFold(strings.TrimSpace(o), op) {
			return true
		}
	}
	return false
}

// ChooseTarget picks who to pickpocket: an NPC called name with a Pickpocket op, on the spot's
// level within the leash, adjacent ones first and then the nearest. It returns -1 for none.
func ChooseTarget(npcs []NPC, name string, spot tile.Tile) int {
	var idx []int
	for i, n := range npcs {
		if strings.EqualFold(strings.TrimSpace(n.Name), strings.TrimSpace(name)) && HasOp(n.Ops, PickpocketOp) &&
			n.Tile.Level == spot.Level && tile.Chebyshev(n.Tile, spot) <= PickpocketLeash {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return -1
	}
	sort.SliceStable(idx, func(a, b int) bool {
		na, nb := npcs[idx[a]], npcs[idx[b]]
		if fa, fb := na.Distance > 1, nb.Distance > 1; fa != fb {
			return !fa
		}
		return na.Distance < nb.Distance
	})
	return idx[0]
}

// WalkToTargetBeyond is the distance past which the step walks to its target before clicking.
const WalkToTargetBeyond = 8

// FundAction is what the fund step does when no target is in reach.
type FundAction string

// The no-target actions.
const (
	FundWaitAway FundAction = "wait: short of the fare with nobody to pickpocket here"
	FundWalk     FundAction = "walk to the pickpocket spot"
	FundWaitHere FundAction = "wait for a target"
)

// NoTargetAction decides the fund step with no target: wait when away from the mainland, walk back
// when more than six tiles from the spot, otherwise wait there.
func NoTargetAction(away bool, gapToSpot int) FundAction {
	switch {
	case away:
		return FundWaitAway
	case gapToSpot > 6:
		return FundWalk
	}
	return FundWaitHere
}

// BoatLeg is where the next boat leg walks to: the Brimhaven pier, or on to dest.
func BoatLeg(here, dest tile.Tile) (tile.Tile, Leg) {
	leg := TravelLeg(here)
	if leg == LegPier {
		return BrimhavenPier, leg
	}
	return dest, leg
}
