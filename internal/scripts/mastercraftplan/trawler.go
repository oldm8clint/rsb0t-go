package mastercraftplan

// THE TRAWLER ROUND (a user request): the main account is the Fishing Trawler's ANCHOR. The crew (FishingTrawler accounts,
// internal/bot/fishingtrawler.go) wait in the Port Khazard lobby and say so on the wall bus (mulebus.TrawlerLobby); when
// craftTrawlerQuorum of them are there, the main account walks to the lobby, crosses the gangplank and calls everyone aboard
// (mulebus.TrawlerBoard), plays the game like the crew but never leaves, and after the win claims the whole share at the net
// (trawlerplan.PlanClaim: the best stacks into the pack, the rest dumped), banks it and starts again while the crew is there.
//
// THE TWO-HOUR CYCLE (a user request): "fishing trawler ... in 2hr blocks until master reaches 200m exp". A block is two
// hours of trawler rounds back to back (the whole fleet comes to the lobby on the anchor's gather call, mulebus.TrawlerBlock);
// then a rest of at most two hours: every crafting round whose stock is at its threshold runs, and the next block starts as
// soon as nothing is left to craft, or when the two hours are up, a round still under way yielding at its next safe point
// (live 3 Nov 2026: a superheat round of 1h42m and a re-armed Nature campaign kept the rest going past 2h19m, and the
// users want the trawler's rate from full blocks); with nothing to craft, two hours of ScarabSwarm. Blocks stop at 200M
// Fishing XP.
//
// It is off by default (craftTrawler). On, it comes before every training round: a crew of twenty waiting in the lobby is
// waiting on this account alone, and a gem or bow round due at every check would otherwise keep them waiting for hours. A round
// under way -- the account aboard, or a won game's catch not yet claimed -- carries on before anything else, even a hand-over
// give-up: ScarabSwarm cannot be started on the boat.

import (
	"strconv"
	"time"

	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/trawlerplan"
)

// ModeTrawler is a trawler round.
const ModeTrawler Mode = "trawler"

// The settings' names.
const (
	// KeyTrawler switches the trawler round on.
	KeyTrawler = "craftTrawler"
	// KeyTrawlerQuorum is how many crew must wait in the lobby, on this account's world, for a round to start.
	KeyTrawlerQuorum = "craftTrawlerQuorum"
	// KeyTrawlerFoodLow and KeyTrawlerFoodHigh are the food schedule (TrawlerBlockOn): a block starts while the mule holds
	// under the low mark of food and ends early once it holds the high mark. A low mark of 0 runs the blocks back to back.
	KeyTrawlerFoodLow  = "craftTrawlerFoodLow"
	KeyTrawlerFoodHigh = "craftTrawlerFoodHigh"
)

func init() {
	knownKeys[KeyTrawler] = true
	knownKeys[KeyTrawlerQuorum] = true
	knownKeys[KeyTrawlerFoodLow] = true
	knownKeys[KeyTrawlerFoodHigh] = true
}

// THE RAW FISH TRIGGER (a user request, replacing the food schedule below): "trawler runs when the mule is out of Raw sea
// turtle, Raw manta ray and Raw shark; 3 hours, to restock raws". The food counted is those three RAW fish on the mule's bank
// (internal/bot trawlerFoods); a block starts while they total under craftTrawlerFoodLow (default 1: all three are out), and
// runs craftTrawlerBlockMinutes (default 180). craftTrawlerFoodHigh 0 (the default) never ends a block early; a mule bank that is
// not published does not start one (the old schedule ran blocks back to back then).
//
// THE FOOD SCHEDULE it replaced (a user request: "master wont need to run trawler full time and we should automate a schedule to trawl
// based on food consumption food cooked etc", which replaces that day's "Drop the rests"): the trawler is the fleet's food, so
// a block runs while the mule's food runs short. Food is the mule's published bank (the peer board): the top tier the trawler
// restocks, Shark, Sea turtle and Manta ray, cooked or raw, a piece each (internal/bot trawlerFoods; every food counted 9.7M
// pieces live, mostly cheap food, and never ran short). A block starts while
// it holds under craftTrawlerFoodLow, runs its two hours (or stops at a game's end once the stock reaches craftTrawlerFoodHigh),
// and the next starts only when the stock is under the low mark again; between blocks the account does its other rounds. A
// mule bank not published (or older than the peer board keeps) is not read as short: the blocks then run back to back, as
// before. Measured 4 Nov 2026 (docs/scarabswarm_aio_roadmap_2026-11-04.md): the fleet eats some 2,200-2,800 food an hour while
// Queen waves run and about 70 without them; a block brings the mule about 500 fish an hour, and the mule cooks about 1,240. So
// the low mark of 5,000 is about two hours of a Queen wave's eating, which a block starts against; without waves the stock
// seldom falls that far and master does his other rounds. The high mark mostly ends a block a market buy has made unneeded.
const (
	DefaultTrawlerFoodLow  = 1
	DefaultTrawlerFoodHigh = 0
)

// DefaultTrawlerQuorum is the crew a round waits for.
const DefaultTrawlerQuorum = 20

// TrawlerFishing is the Fishing level the claim needs (trawler_win.rs2).
const TrawlerFishing = 15

// TrawlerInput is the trawler round's reading.
type TrawlerInput struct {
	// Crew is the crew waiting in the lobby on this account's world (mulebus.TrawlerLobbyCount).
	Crew int
	// Here is where the account stands (HereKnown: read).
	Here      tile.Tile
	HereKnown bool
	// FishingLevel is the account's Fishing level, 0 until the server has sent it; FishingXP its experience.
	FishingLevel int
	FishingXP    int
	// Food is the food the mule's published bank holds (THE FOOD SCHEDULE); FoodKnown that it was read.
	Food      int
	FoodKnown bool
}

// TrawlerBackoffStep and TrawlerBackoffMax are a failed trawler round's backoff: a step a failure in a row, at most the max.
const (
	TrawlerBackoffStep = 2 * time.Minute
	TrawlerBackoffMax  = 10 * time.Minute
)

// TrawlerLedger is the trawler round's memory.
type TrawlerLedger struct {
	// Claim is set from boarding until the catch is claimed: the next round claims first. ClaimAfter is when the game boarded
	// would have ended: content pays whoever is still in the game zones at the end (trawler_win sets %trawler_catch), so a
	// game cut short by a disconnect still pays the player the server kept aboard (live 3 Nov 2026, 08:49: master dropped
	// mid-game and was refused logins with response 5 while his player sailed on), and the claim waits for that end.
	Claim      bool      `json:"claim,omitempty"`
	ClaimAfter time.Time `json:"claimAfter,omitempty"`
	// Games is the games boarded, which picks the crew watch called next (trawlerplan.GameWatch), and Peaks the crew each
	// watch's last muster boarded with, by watch: kept across restarts (live 3 Nov 2026 08:55: master reconnected after a drop,
	// counted from zero again and called watch 1 back the moment it came off the boat).
	Games int `json:"games,omitempty"`
	// BlockXP is the Fishing, Magic and Fletching experience at the block's start (zero: not read yet), for the block's own
	// experience an hour (a user request: "we also want an accurate representation of actual exp per hour").
	BlockXP [3]int `json:"blockXP,omitempty"`
	// BlockXPAt is when BlockXP was read: the block's start, or later for a ledger from before BlockXP.
	BlockXPAt time.Time                    `json:"blockXPAt,omitempty"`
	Peaks     [trawlerplan.Watches + 1]int `json:"peaks,omitempty"`
	// Rounds, Claims and Taken count rounds, claims and the items the claims moved into the pack.
	Rounds int `json:"rounds,omitempty"`
	Claims int `json:"claims,omitempty"`
	Taken  int `json:"taken,omitempty"`
	// BlockStart is when the two-hour block under way played its first round (zero: no block under way); RestUntil when
	// the rest after the last block ends at the latest; RestCrafted that a crafting round ran in that rest, so the rest ends
	// early once nothing is left to craft.
	BlockStart  time.Time `json:"blockStart"`
	RestUntil   time.Time `json:"restUntil"`
	RestCrafted bool      `json:"restCrafted,omitempty"`
}

// TrawlerBlockLen and TrawlerRestLen are the older cycle's constants, kept as the fallbacks: two-hour blocks aboard, back to back
// with no rest (a user request: "Drop the rests"). Since 5 Nov 2026 the block is craftTrawlerBlockMinutes (3 hours), started by
// the mule being out of raw fish, and a rest of craftTrawlerRestMinutes (default 60) after it lets every due round have a turn: the
// mule's cooks eat raw fish faster than a block brings them, so the trigger alone could hold the account aboard for ever.
const (
	TrawlerBlockLen = 2 * time.Hour
	TrawlerRestLen  = 0 // the rest is now Sched.TrawlerRest
)

// trawlerBlockLen is the block's length: craftTrawlerBlockMinutes (3 hours), TrawlerBlockLen when the setting is unset.
func trawlerBlockLen(set Settings) time.Duration {
	if set.Sched.TrawlerBlock > 0 {
		return set.Sched.TrawlerBlock
	}
	return TrawlerBlockLen
}

// TrawlerBlockOn reports whether a trawler block runs at now: the round is on, Fishing is under 200M XP and no rest is
// under way.
func TrawlerBlockOn(set Settings, in TrawlerInput, led *Ledger, now time.Time) bool {
	if !set.Trawler || in.FishingXP >= MaxSkillXP {
		return false
	}
	if led != nil && !led.Trawler.BlockStart.IsZero() {
		return true // a block under way runs to its end (TrawlerCycleBefore)
	}
	if led != nil && led.Trawler.resting(now) {
		return false // the rest after a block (craftTrawlerRestMinutes): the other rounds are due their turn
	}
	if foodShort, ok := trawlerFoodShort(set, in); ok {
		return foodShort
	}
	return led == nil || !led.Trawler.resting(now)
}

// trawlerFoodShort reports whether the food schedule starts a block: the mule's food under the low mark. ok is false with the
// schedule off or the food not read, and the blocks then follow the rest timer (back to back).
func trawlerFoodShort(set Settings, in TrawlerInput) (short, ok bool) {
	if set.TrawlerFoodLow <= 0 {
		return false, false
	}
	if !in.FoodKnown {
		return false, true // no published bank: no new block (the raw fish trigger needs the reading)
	}
	return in.Food < set.TrawlerFoodLow, true
}

// trawlerFoodFull reports whether the food schedule ends a block early: the mule's food at the high mark.
func trawlerFoodFull(set Settings, in TrawlerInput) bool {
	return set.TrawlerFoodLow > 0 && set.TrawlerFoodHigh > 0 && in.FoodKnown && in.Food >= set.TrawlerFoodHigh
}

// resting reports whether a rest is under way: its two hours are not up. A crafting round does not stretch it: the round
// yields at its next safe point once the block is due (the bot's trawlerWaiting).
func (t TrawlerLedger) resting(now time.Time) bool { return now.Before(t.RestUntil) }

// TrawlerCycleBefore ends a block that has run its two hours, once no game is under way (the account off the boat and no
// catch to claim): the rest starts. It reports whether the ledger changed.
func TrawlerCycleBefore(set Settings, in TrawlerInput, led *Ledger, now time.Time) bool {
	t := &led.Trawler
	if t.BlockStart.IsZero() || (now.Sub(t.BlockStart) < trawlerBlockLen(set) && !trawlerFoodFull(set, in)) || t.Claim || (in.HereKnown && trawlerplan.OnBoat(in.Here)) {
		return false
	}
	t.BlockStart, t.RestUntil, t.RestCrafted = time.Time{}, now.Add(set.Sched.TrawlerRest), false
	return true
}

// TrawlerCycleAfter records what the decision d at now means for the cycle: the first trawler round of a block starts its
// clock; a crafting round in a rest is remembered; and a rest that crafted and has nothing left to craft ends now. It reports
// whether the ledger changed.
func TrawlerCycleAfter(set Settings, led *Ledger, now time.Time, d Decision) bool {
	t := &led.Trawler
	if !set.Trawler {
		return false
	}
	rest := t.resting(now)
	switch {
	case d.Mode == ModeTrawler && !rest && t.BlockStart.IsZero():
		t.BlockStart = now
	case rest && d.Mode != ModeSwarm && d.Mode != ModeTrawler && !t.RestCrafted:
		t.RestCrafted = true
	case rest && d.Mode == ModeSwarm && t.RestCrafted && !d.Inconclusive:
		t.RestUntil, t.RestCrafted = now, false
	default:
		return false
	}
	return true
}

// trawlerResume is a trawler round under way: the account aboard, or a catch to claim.
func trawlerResume(set Settings, s Snapshot, led *Ledger) (Decision, bool) {
	in := s.Trawler
	switch {
	case in.HereKnown && trawlerplan.OnBoat(in.Here):
		return Decision{Mode: ModeTrawler, Resume: true, Continue: true, Why: "a trawler round is under way: the account is aboard the trawler"}, true
	case led.Trawler.Claim && set.Trawler:
		return Decision{Mode: ModeTrawler, Resume: true, Continue: true, Why: "a won trawler game's catch is still to be claimed at the net"}, true
	}
	return Decision{}, false
}

// decideTrawler starts a trawler round while a block runs (TrawlerBlockOn): the round walks to the lobby and waits there for the
// crew the gather call brings. why says why not.
func decideTrawler(set Settings, s Snapshot, led *Ledger) (Decision, string) {
	if !set.Trawler {
		return Decision{}, ""
	}
	in := s.Trawler
	switch {
	case in.FishingXP >= MaxSkillXP:
		return Decision{}, "trawler: Fishing is at 200M XP"
	case in.FishingLevel > 0 && in.FishingLevel < TrawlerFishing:
		return Decision{}, "trawler: Fishing " + strconv.Itoa(in.FishingLevel) + " is under the " + strconv.Itoa(TrawlerFishing) + " the claim needs"
	case !TrawlerBlockOn(set, in, led, s.Now):
		if _, ok := trawlerFoodShort(set, in); ok {
			if !in.FoodKnown {
				return Decision{}, "trawler: the mule's bank is not published, so its raw fish are not read"
			}
			return Decision{}, "trawler: the mule holds " + strconv.Itoa(in.Food) + " Raw sea turtle, Raw manta ray and Raw shark, not under the " + strconv.Itoa(set.TrawlerFoodLow) + " that starts a block"
		}
		return Decision{}, "trawler: resting until " + led.Trawler.RestUntil.Format("15:04") + " (crafting first, else ScarabSwarm)"
	}
	if until, held := led.BackedOff(ModeTrawler, s.Now); held {
		return Decision{}, "trawler rounds are backing off until " + until.Format("15:04") + " after " + strconv.Itoa(led.Fails(ModeTrawler)) + " failed in a row"
	}
	why := "the " + trawlerBlockLen(set).String() + " trawler block is on: " + strconv.Itoa(in.Crew) + " crew in the Port Khazard lobby"
	if in.FoodKnown && set.TrawlerFoodLow > 0 {
		why += ", the mule holding " + strconv.Itoa(in.Food) + " raw sea turtle, manta ray and shark (a block under " + strconv.Itoa(set.TrawlerFoodLow) + ")"
	}
	if !led.Trawler.BlockStart.IsZero() {
		why += ", " + s.Now.Sub(led.Trawler.BlockStart).Round(time.Minute).String() + " of " + trawlerBlockLen(set).String() + " played"
	}
	// Continue: the next round follows this one straight on (Chain).
	return Decision{Mode: ModeTrawler, Continue: true, Why: why}, ""
}

// TrawlerDue is a cheap test between checks: a block runs, so a check should run now rather than at the next interval.
func TrawlerDue(set Settings, in TrawlerInput, led *Ledger, now time.Time) bool {
	return TrawlerBlockOn(set, in, led, now)
}
