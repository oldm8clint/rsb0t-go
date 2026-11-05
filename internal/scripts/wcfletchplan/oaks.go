package wcfletchplan

// THE 2-TICK OAKS, LEVEL-DRIVEN (a user request: "ensure the wcfletch script is tick manipulating throughout the levels and
// utilizing 2t oaks to boost wc levels significantly ... might not beable to be done by all accounts at once but theres 3 worlds").
// Pure.
//
// THE OLD RULE was a fixed daily window: twoTickOaks, default off, two hours of every 24 whatever the account's level. This replaces the
// default with a rule that reads the level: an account chops the Falador farm's oaks on the rats' two-tick rhythm (the retaliate chopper,
// docs/two_tick_retaliate_research_2026-11-04.md) while the model below says the oaks pay more Woodcutting experience an hour than the
// tree its own ladder rung would be chopping with the knife manipulation (a roll every 3 ticks, chopmanipplan), and a slot is free on
// its world. An explicit twoTickOaks true or false keeps its old meaning (the fixed window, or never).
//
// THE MODEL. Experience an hour = rolls an hour x chance a roll pays x experience a log x the share of time at a tree:
//
//   - rolls: 6,000 ticks an hour over the period; the knife manipulation's period is 3 (chopmanipplan.Period), the two-tick oaks' is 2
//     when every roll lands on the rats' rhythm and 3 when the chopper has fallen back to the knife (retaliateplan's minGain, 0.7 of the
//     gaps kept, is the share it is held to), so the oaks' effective rolls are Realised x 3,000 + (1 - Realised) x 2,000;
//   - chance: the engine's stat_random over trees.dbrow's successchance pair for the axe, capped at 1 (SwingChance's formula);
//   - uptime: retaliateplan.Uptime for the oaks (a felled oak, one log in eight, takes 30 ticks to grow back; the farm has OakTreesInReach
//     of them in reach), a flat RungUptime for the ladder's own rung (a camp of many trees).
//
// What it says, from the dbrow's pairs (Woodcutting experience alone, the owner's aim; the oaks' logs are dropped, so the rung's own
// Fletching experience is what an account gives up): at every level from 16 up and with every axe the oaks pay 1.2 to 3.2 times what the
// rung does (Rune axe, 99: 85,263 an hour against 38,672 at the Magic rung; Woodcutting 40, 71,092 against 48,885 at Willow); the plain tree
// at 15 is the one rung the oaks do not beat by the margin with a slow axe. OaksPay is the comparison and OakCrossover the first level where
// the rung overtakes the oaks (0 when none up to 99, the answer for the rune axe), so twoTickOakMaxLevel is the owner's lever: the model
// alone would send every account at 15 or above to the oaks.
//
// THE SLOTS. The farm has two rats and the chopper needs both, so ONE account per world chops it (retaliateChopperBot's own note). The
// fleet's three worlds therefore hold three oak choppers at a time. OakSlots is the board of who holds which world's slot: an account takes
// the slot when it is free (or already its own), holds it for a stint (twoTickOakStintMinutes, 120 by default), then rests
// (twoTickOakRestMinutes, 360) so the others get their turn; a holder that stops heard from for OakSlotStale gives the slot up.

import (
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/scripts/retaliateplan"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// Settings' names and defaults.
const (
	// OakMinWoodcutting is the Woodcutting level an oak needs (content: levelrequired 15).
	OakMinWoodcutting = 15
	// OakTreesInReach is the oaks the Falador south farm has within the chopper's reach (retaliateplan Trees: "Falador south (3)").
	OakTreesInReach = 3
	// RungUptime is the share of time the ladder's own rung spends at a tree (not content: a camp of many trees, the walk to the next).
	RungUptime = 0.9
	// DefaultOakRealised is the share of the oaks' rolls that land on the rats' two-tick rhythm (retaliateplan's minGain default).
	DefaultOakRealised = 0.7
	// DefaultOakMargin is how many times the rung's experience an hour the oaks must pay to be chosen.
	DefaultOakMargin = 1.15
	// DefaultOakSlots is the oak choppers one world holds at once.
	DefaultOakSlots = 1
	// OakSlotStale is how long a holder may go unheard before its slot is free again.
	OakSlotStale = 10 * time.Minute
)

// Defaults of the stint and the rest.
const (
	DefaultOakStint = 120 * time.Minute
	DefaultOakRest  = 360 * time.Minute
)

// The oak and plain tree's success pairs for each axe (trees.dbrow [oak_tree_table] and [normal_tree_table], branch 289).
var oakAndNormalChance = map[string]map[string][2]int{
	"Oak": {
		"Bronze axe": {32, 100}, "Iron axe": {48, 150}, "Steel axe": {64, 200}, "Black axe": {72, 225},
		"Mithril axe": {80, 250}, "Adamant axe": {96, 300}, woodcutplan.RuneAxe: {112, 350},
	},
	"Normal tree": {
		"Bronze axe": {64, 200}, "Iron axe": {96, 300}, "Steel axe": {128, 400}, "Black axe": {144, 450},
		"Mithril axe": {160, 500}, "Adamant axe": {192, 600}, woodcutplan.RuneAxe: {224, 700},
	},
}

// treeRollChance is the chance one roll pays a log at the tier named tierLabel with axe, any tier the ladder has (0 when unknown).
func treeRollChance(tierLabel string, level int, axe string) float64 {
	if pairs, ok := oakAndNormalChance[tierLabel]; ok {
		for name, p := range pairs {
			if strings.EqualFold(name, strings.TrimSpace(axe)) {
				return retaliateplan.RollChance(level, p[0], p[1])
			}
		}
		return 0
	}
	return SwingChance(tierLabel, level, axe)
}

// OakXPPerHour is the Woodcutting experience an hour at the farm's oaks on the two-tick rhythm, realised the given share of the time.
func OakXPPerHour(level int, axe string, realised float64) float64 {
	p := treeRollChance("Oak", level, axe)
	if p <= 0 {
		return 0
	}
	realised = min(max(realised, 0), 1)
	// Rolls at 2 ticks when on the rhythm, at the knife's 3 when fallen back (the chopper's fallback is the knife).
	rolls := realised*retaliateplan.TicksPerHour/2 + (1-realised)*retaliateplan.TicksPerHour/chopmanipPeriod
	chop := retaliateplan.RollsToFell(p) * 2
	up := retaliateplan.Uptime(OakTreesInReach, chop, 3, 30)
	return rolls * p * 37.5 * up
}

// chopmanipPeriod is the ticks between rolls under the knife manipulation (chopmanipplan.Period).
const chopmanipPeriod = 3

// RungXPPerHour is the Woodcutting experience an hour at the ladder's rung for level and target (woodcutplan.CurrentTierIndex), with the
// knife manipulation: a roll every 3 ticks.
func RungXPPerHour(level int, axe, target string) (xp float64, rung string) {
	t := woodcutplan.TIERS[woodcutplan.CurrentTierIndex(level, target)]
	p := treeRollChance(t.Label, level, axe)
	return float64(retaliateplan.TicksPerHour) / chopmanipPeriod * p * t.XP * RungUptime, t.Label
}

// OaksPay reports whether the oaks pay at least margin times the Woodcutting experience an hour of the account's own rung, for the model
// above; the numbers come back for the log line. Under Woodcutting 15 they never do.
func OaksPay(level int, axe, target string, realised, margin float64) (pays bool, oak, rung float64, rungLabel string) {
	if level < OakMinWoodcutting {
		return false, 0, 0, ""
	}
	oak = OakXPPerHour(level, axe, realised)
	rung, rungLabel = RungXPPerHour(level, axe, target)
	return oak > 0 && oak >= margin*rung, oak, rung, rungLabel
}

// OakCrossover is the first Woodcutting level, from 15 to 99, at which the ladder's rung beats the oaks by the margin: the level an
// account stops being sent to the oaks. 0 when the oaks win at every level.
func OakCrossover(axe, target string, realised, margin float64) int {
	for l := OakMinWoodcutting; l <= 99; l++ {
		if pays, _, _, _ := OaksPay(l, axe, target, realised, margin); !pays {
			return l
		}
	}
	return 0
}

// ---- the policy ----

// OakMode is how the oaks are decided.
type OakMode string

// The modes.
const (
	// OakAuto is the level-driven rule (the default).
	OakAuto OakMode = "auto"
	// OakWindow is the old fixed daily window (twoTickOaks true).
	OakWindow OakMode = "window"
	// OakOff never chops the oaks (twoTickOaks false).
	OakOff OakMode = "off"
)

// OakSettings are the level-driven rule's knobs.
type OakSettings struct {
	Mode OakMode
	// Stint is the longest one account holds the slot; Rest the time it stays off the oaks after a stint.
	Stint, Rest time.Duration
	// Slots is the oak choppers one world holds at once.
	Slots int
	// MaxLevel stops the oaks at this Woodcutting level whatever the model says (0: the model alone).
	MaxLevel int
	// Realised and Margin are the model's inputs.
	Realised, Margin float64
}

// DefaultOakSettings is the shipped rule: level-driven, one chopper a world, two hours on and six off.
func DefaultOakSettings() OakSettings {
	return OakSettings{Mode: OakAuto, Stint: DefaultOakStint, Rest: DefaultOakRest, Slots: DefaultOakSlots, Realised: DefaultOakRealised, Margin: DefaultOakMargin}
}

// OakAsk is what the decision reads of the account.
type OakAsk struct {
	Level int
	Axe   string
	// Target is the ladder's target tree (woodcutplan target label).
	Target string
	World  int
	Now    time.Time
	// Holding is whether the account's chopper is running; Since when its stint began; RestUntil the end of its rest.
	Holding   bool
	Since     time.Time
	RestUntil time.Time
	// Account names it on the slot board; Board is the slot board (nil: no cap).
	Account string
	Board   *OakSlots
}

// OakAction is the answer.
type OakAction int

// The actions.
const (
	// OakIdle: no oaks, carry on with the ladder.
	OakIdle OakAction = iota
	// OakChop: chop the oaks now (start, or carry on).
	OakChop
	// OakStop: stop the chopper (the stint is over, or the model no longer says the oaks pay).
	OakStop
)

// OakDecision is the action and why.
type OakDecision struct {
	Action OakAction
	Why    string
}

// DecideOaks is the level-driven rule for OakAuto (the other modes are the bot's: the fixed window and never). It starts a stint when the
// oaks pay and the world has a free slot, carries it on until the stint is over, and stops it the moment the model stops saying the oaks pay.
func DecideOaks(set OakSettings, in OakAsk) OakDecision {
	pays, oak, rung, rungLabel := OaksPay(in.Level, in.Axe, in.Target, set.Realised, set.Margin)
	why := func(s string) string { return s }
	if set.MaxLevel > 0 && in.Level >= set.MaxLevel {
		pays = false
		why = func(s string) string { return s + "; twoTickOakMaxLevel " + itoa(set.MaxLevel) + " reached" }
	}
	model := func() string {
		return "oaks " + itoa(int(oak)) + " xp/h against " + itoa(int(rung)) + " at the " + strings.ToLower(rungLabel) + " rung (Woodcutting " + itoa(in.Level) + ", " + in.Axe + ")"
	}
	switch {
	case in.Holding && !pays:
		return OakDecision{OakStop, why("the oaks no longer pay: " + model())}
	case in.Holding && set.Stint > 0 && in.Now.Sub(in.Since) >= set.Stint:
		if in.Board != nil {
			in.Board.Release(in.World, in.Account)
		}
		return OakDecision{OakStop, "the stint of " + set.Stint.String() + " is over"}
	case in.Holding:
		if in.Board != nil {
			in.Board.Touch(in.World, in.Account, in.Now)
		}
		return OakDecision{OakChop, "stint under way"}
	case !pays:
		if in.Level < OakMinWoodcutting {
			return OakDecision{OakIdle, "Woodcutting " + itoa(in.Level) + " is under the oaks' " + itoa(OakMinWoodcutting)}
		}
		return OakDecision{OakIdle, why("the oaks do not pay: " + model())}
	case in.Now.Before(in.RestUntil):
		return OakDecision{OakIdle, "resting from the oaks until " + in.RestUntil.Format("15:04")}
	}
	if in.Board != nil && !in.Board.Claim(in.World, in.Account, set.Slots, in.Now) {
		return OakDecision{OakIdle, "world " + itoa(in.World) + "'s " + itoa(set.Slots) + " oak slot(s) are held: " + strings.Join(in.Board.Holders(in.World, in.Now), ", ")}
	}
	return OakDecision{OakChop, "starting a stint: " + model()}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// ---- the slot board ----

// OakSlots is the process-wide board of who holds each world's oak slots.
type OakSlots struct {
	mu    sync.Mutex
	seen  map[int]map[string]time.Time
	stale time.Duration
}

// NewOakSlots is an empty board whose holders expire after stale unheard (OakSlotStale when 0).
func NewOakSlots(stale time.Duration) *OakSlots {
	if stale <= 0 {
		stale = OakSlotStale
	}
	return &OakSlots{seen: map[int]map[string]time.Time{}, stale: stale}
}

// ClaimedOaks is the board the bot uses: one for the process.
var ClaimedOaks = NewOakSlots(0)

func (b *OakSlots) live(world int, now time.Time) map[string]time.Time {
	m := b.seen[world]
	for a, t := range m {
		if now.Sub(t) > b.stale {
			delete(m, a)
		}
	}
	return m
}

// Claim takes a slot of the world for account when fewer than cap are held (or account already holds one), and reports whether it holds one.
func (b *OakSlots) Claim(world int, account string, cap int, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.live(world, now)
	if m == nil {
		m = map[string]time.Time{}
		b.seen[world] = m
	}
	if _, held := m[account]; held {
		m[account] = now
		return true
	}
	if cap < 1 {
		cap = DefaultOakSlots
	}
	if len(m) >= cap {
		return false
	}
	m[account] = now
	return true
}

// Touch records that account is still working its slot.
func (b *OakSlots) Touch(world int, account string, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if m := b.seen[world]; m != nil {
		if _, ok := m[account]; ok {
			m[account] = now
		}
	}
}

// Release gives account's slot of the world back.
func (b *OakSlots) Release(world int, account string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.seen[world], account)
}

// Holders names who holds the world's slots.
func (b *OakSlots) Holders(world int, now time.Time) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for a := range b.live(world, now) {
		out = append(out, a)
	}
	return out
}

// Held is how many slots of the world are held.
func (b *OakSlots) Held(world int, now time.Time) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.live(world, now))
}
