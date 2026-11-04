package mastercraftplan

// The Nature gate round (runecraft P3, docs/runecraft_p3_2026-10-14.md). a user request: "runecraft with runners in the fleet,
// maxing nature runes; the main account (MasterCrafter, the main account) gets the Runecraft levels and sits at the altar; other ScarabSwarm
// accounts run essence". Once Runecrafting is 44, the main account walks to the Nature ruins on Karamja and stands OUTSIDE them (the gate):
// RuneCarrier accounts bring him 25 unnoted Rune essence a trip, he uses the Nature talisman on the ruins, crafts inside, takes the
// portal out and is back for the next carrier. Natures pile up in his pack beyond craftRuneKeep and the carriers carry them home on
// their kit trips (the courier), and what is left when the round ends is handed to the mule at its post.
//
// THE TRIGGER is one of two. craftRuneManual: with craftRune on, the switch is the owner's and a round starts whenever it is
// possible. craftRuneCampaign (P4, campaign.go): the account starts a CAMPAIGN by itself when the mule's published Nature runes fall
// under craftRuneStartBelow, and stops it at the target or when the mule holds craftRuneStopAbove.
//
// WHERE IT RANKS (Decide): a round already under way (standing in the Nature temple, or at the gate) carries on before anything
// else, the mule heard or not -- BEFORE the levelling round's own resume, which also reads "standing in a temple"; a new one
// comes after the jewellery round and before the levelling round and the batch rounds (it is long and exclusive, like the chest's).

import (
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/runecraftplan"
)

// ModeNature is a Nature gate round.
const ModeNature Mode = "nature"

// GateLedger is the gate's own memory (RuneLedger.Gate): the lifetime counters and the pause after a round that found nothing.
type GateLedger struct {
	// Rounds started, trades taken, the essence they brought, the Natures crafted and the experience, the courier hand-overs and
	// the Natures they carried, the Natures handed to the mule at the end of a round.
	Rounds         int `json:"rounds,omitempty"`
	Trades         int `json:"trades,omitempty"`
	EssenceTaken   int `json:"essenceTaken,omitempty"`
	NaturesMade    int `json:"naturesMade,omitempty"`
	XPGained       int `json:"xpGained,omitempty"`
	CourierTrades  int `json:"courierTrades,omitempty"`
	CourierNatures int `json:"courierNatures,omitempty"`
	HandedAtEnd    int `json:"handedAtEnd,omitempty"`
	// LastTrade is when essence last arrived; PauseUntil holds the round off after one that found nothing to work with.
	LastTrade  time.Time `json:"lastTrade,omitempty"`
	PauseUntil time.Time `json:"pauseUntil,omitempty"`
	// RunSince is when the gate's current run of trades began: a trade more than GateRunGap after the one before it starts a new run
	// (GoodHistory).
	RunSince time.Time `json:"runSince,omitempty"`
	// LastStop is why the last round ended, for the log after a restart.
	LastStop string `json:"lastStop,omitempty"`
}

// NoteTrade records an essence trade.
func (g *GateLedger) NoteTrade(essence int, now time.Time) {
	g.Trades++
	g.EssenceTaken += essence
	// A trade after a long silence (or the first this ledger has seen the run of) starts a run. A ledger written before RunSince existed
	// has a LastTrade and no RunSince: its run is counted from its first trade under this build.
	if g.RunSince.IsZero() || g.LastTrade.IsZero() || now.Sub(g.LastTrade) > GateRunGap {
		g.RunSince = now
	}
	g.LastTrade = now
}

// THE GATE'S GOOD HISTORY (CR-6 of docs/xp_rate_plan_2026-10-19.md, 19 Oct 2026). A Nature round that fails for a passing reason backs
// off 30 minutes, then 60, doubling to 180 (BackoffFor): a backoff made for a gate that is broken (the stand unreachable, no carrier
// ever comes, the mule away), so a round does not walk home, bank and walk back to a gate that cannot trade. A gate that has been
// trading for an hour and fails on the first or second round after it is not that gate. 18 Oct 2026 20:08 a round that had failed once
// already ended on a lagging connection, backed off 60 minutes and the gate stood closed for about 40 more minutes of 41-44k Runecrafting
// experience an hour. Such a gate gets the short wait (GateGoodBackoff, doubling the same way); a gate with no good run behind it keeps
// today's.
const (
	// GateRunGap is the longest silence between two trades that is still one run: a fleet restart is three to eight minutes of ramp and
	// a lag episode a few more.
	GateRunGap = 20 * time.Minute
	// GateGoodRun is how long a run must have lasted to count as a good history.
	GateGoodRun = time.Hour
	// GateGoodFresh is how recent the run's last trade must be: a history that ended a day ago says nothing of the failure now.
	GateGoodFresh = 45 * time.Minute
	// GateGoodBackoff is the first wait after a failed round of a gate with a good history.
	GateGoodBackoff = 5 * time.Minute
)

// GoodHistory reports whether the gate has traded for GateGoodRun in a run whose last trade was at most GateGoodFresh before now.
func (g GateLedger) GoodHistory(now time.Time) bool {
	return !g.RunSince.IsZero() && !g.LastTrade.IsZero() && g.LastTrade.Sub(g.RunSince) >= GateGoodRun && now.Sub(g.LastTrade) <= GateGoodFresh
}

// GateBackoffFor is the wait after the fails-th failed round in a row of a gate with a good history: GateGoodBackoff for the first,
// doubling for each further one, never longer than BackoffFor would make it (a backoff the operator set shorter stays shorter).
func GateBackoffFor(fails int, set Settings) time.Duration {
	long := BackoffFor(fails, set)
	if fails < 1 {
		return 0
	}
	d := GateGoodBackoff
	for i := 1; i < fails && d < long; i++ {
		d *= 2
	}
	return min(d, long)
}

// NoteCraft records one verified craft at the Nature altar.
func (g *GateLedger) NoteCraft(natures, xp int) {
	g.NaturesMade += natures
	g.XPGained += xp
}

// NoteCourier records a courier hand-over.
func (g *GateLedger) NoteCourier(natures int) {
	g.CourierTrades++
	g.CourierNatures += natures
}

// natureResume is the gate round that is already under way where the account stands: in the Nature temple, or at the gate. It is
// decided before any check of the mule, as the levelling round's is: a restart inside the temple must not leave ScarabSwarm
// walking from there while the bus fills.
func natureResume(set Settings, s Snapshot, led *Ledger) (Decision, bool) {
	if !set.Rune.On {
		return Decision{}, false
	}
	in := s.Rune
	switch {
	case in.Where == runecraftplan.RegionTemple && in.Temple == runecraftplan.KeyNature:
		return Decision{Mode: ModeNature, Resume: true, Why: "a Nature gate round is under way: standing in the Nature temple"}, true
	case in.AtGate && set.Trawler && TrawlerBlockOn(set, s.Trawler, led, s.Now):
		// A trawler block is on (a user request: the two-hour trawler run comes before another Nature cycle): the round at the
		// gate is not resumed; the trawler round walks from here.
		return Decision{}, false
	case in.AtGate && !resumeHeld(led, ModeNature, s.Now):
		// Standing at the gate after rounds that failed twice in a row: not resumed until the backoff is over (resumeHeld).
		return Decision{Mode: ModeNature, Resume: true, Why: "a Nature gate round is under way: standing at the gate"}, true
	}
	return Decision{}, false
}

// decideNature is the gate round's trigger for a new round, and the reason when it does not start one.
func decideNature(set Settings, s Snapshot, led *Ledger) (Decision, string) {
	r := set.Rune
	if !r.On {
		return Decision{}, ""
	}
	if !r.Manual && !r.Campaign {
		return Decision{}, "nature gate: " + KeyRuneManual + " and " + KeyRuneCampaign + " are off"
	}
	in := s.Rune
	if !in.Known {
		return Decision{}, "nature gate: the Runecrafting level has not been read yet"
	}
	nature := runecraftplan.Nature
	if in.Level < nature.Level {
		return Decision{}, "nature gate: Runecrafting " + strconv.Itoa(in.Level) + " is under the " + strconv.Itoa(nature.Level) + " the Nature altar needs"
	}
	if !in.Members {
		return Decision{}, "nature gate: this is not a members world, and a Nature craft on a free world does nothing"
	}
	if until, ok := led.BackedOff(ModeNature, s.Now); ok {
		return Decision{}, "nature gate rounds are backing off until " + until.Format("15:04") + " after " + strconv.Itoa(led.Fails(ModeNature)) + " failed in a row"
	}
	if s.Now.Before(led.Rune.Gate.PauseUntil) {
		return Decision{}, "nature gate rounds are paused until " + led.Rune.Gate.PauseUntil.Format("15:04") + " (" + led.Rune.Gate.LastStop + ")"
	}
	ownTal := in.PackTalisman[nature.Key] || (in.BankKnown && in.BankTalisman[nature.Key] > 0)
	if !ownTal && stockOf(s.Stock, nature.Talisman) <= 0 {
		return Decision{}, "nature gate: no " + nature.Talisman + " in the pack or the bank, and the mule publishes none"
	}
	if r.Campaign {
		return decideCampaign(set, s, led, ownTal)
	}
	who := strconv.Itoa(len(r.Runners)) + " listed carrier(s)"
	if in.Carriers > 0 {
		who += ", " + strconv.Itoa(in.Carriers) + " at work"
	}
	return Decision{Mode: ModeNature, Why: "Runecrafting " + strconv.Itoa(in.Level) + ", the Nature talisman " + natureTalismanWord(ownTal) + ", " + who + " (" + KeyRuneManual + " on)"}, ""
}

func natureTalismanWord(own bool) string {
	if own {
		return "held"
	}
	return "on the mule"
}

// GateAllowed says a name is one of the carriers: on the craftRuneRunners list, or at work for this crafter on the bus
// (atWork). Names are compared the way the server's are (case, spaces and underscores alike).
func GateAllowed(runners []string, atWork []string, name string) bool {
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "_", " ")) }
	n := norm(name)
	if n == "" {
		return false
	}
	for _, r := range runners {
		if norm(r) == n {
			return true
		}
	}
	for _, r := range atWork {
		if norm(r) == n {
			return true
		}
	}
	return false
}
