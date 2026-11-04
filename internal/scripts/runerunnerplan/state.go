package runerunnerplan

// The buyer's persisted state. A hop logs the account out and the fleet starts a fresh copy of
// the script on the other world, so everything a round needs to carry on -- where it is in the
// ring, the Mage Bank trip leg, the delivery clock, the pairs taken off the ring -- lives here and
// is saved after every station and before every hop (settings.Store, id StateStoreID, beside the
// account's other script files, the same pattern as ScarabSwarm's world errand).

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

// StateStoreID is the settings.Store id of the state file.
const StateStoreID = "RuneRunnerState"

// StateVersion is the layout this build writes.
const StateVersion = 1

// Station outcomes, as State.Done records them.
const (
	OutcomeBought     = "bought"
	OutcomeSkipped    = "skipped"
	OutcomeEmpty      = "empty"
	OutcomeCapped     = "capped"
	OutcomeDelivered  = "delivered"
	OutcomeDeferred   = "deferred"
	OutcomeHopped     = "hopped"
	OutcomeAborted    = "aborted"
	OutcomeWalkFailed = "walk-failed"
	OutcomeDrew       = "drew"
	// OutcomeHere is a rotation hop station whose world the account was already on.
	OutcomeHere = "here"
)

// StationResult is how one station of the current round ended.
type StationResult struct {
	Outcome string         `json:"outcome"`
	Reason  string         `json:"reason,omitempty"`
	Bought  map[string]int `json:"bought,omitempty"`
	Spent   int            `json:"spent,omitempty"`
	// Delivered is what a delivery moved to the mule.
	Delivered map[string]int `json:"delivered,omitempty"`
	At        time.Time      `json:"at"`
	// NoCoins is true when coins were why the station did not buy (skipped as "no coins" or
	// "cannot afford"). ShortRunes and ShortCoins are what the visit left on the shelf for lack
	// of coins and what that would have cost at the shelf's prices. The round's purse line is
	// their sum (RoundShort).
	NoCoins    bool `json:"noCoins,omitempty"`
	ShortRunes int  `json:"shortRunes,omitempty"`
	ShortCoins int  `json:"shortCoins,omitempty"`
}

// State is RuneRunner's persisted state.
type State struct {
	Version      int       `json:"version"`
	Round        int       `json:"round"`
	RoundStarted time.Time `json:"roundStarted"`
	Ring         []Station `json:"ring"`
	Cursor       int       `json:"cursor"`
	// Done is this round's finished stations by key ("Betty@1").
	Done map[string]StationResult `json:"done"`
	// Trip is the Mage Bank trip's leg, exactly scarabplan's; TripWorld the world it started on.
	Trip      fleetplan.Trip `json:"trip"`
	TripWorld int            `json:"tripWorld"`
	// TripResult is what the trip under way bought at Lundail, recorded when it bought and
	// finished into Done once the account is back in Ardougne.
	TripResult *StationResult `json:"tripResult,omitempty"`
	// LastMuleWorld is the mule's world when last known, for a round planned while it is not.
	LastMuleWorld   int       `json:"lastMuleWorld"`
	LastDelivery    time.Time `json:"lastDelivery"`
	DeliverRefusals int       `json:"deliverRefusals"`
	// LundailDeaths are deaths during a Lundail trip, pruned to DeathWindow.
	LundailDeaths []time.Time          `json:"lundailDeaths"`
	PairStrikes   map[string]int       `json:"pairStrikes"`
	PairOffUntil  map[string]time.Time `json:"pairOffUntil"`
	PriceMismatch map[string]int       `json:"priceMismatch"`
	Bought        map[string]int       `json:"bought"`
	Delivered     map[string]int       `json:"delivered"`
	Spent         int                  `json:"spent"`
	// HopPending is the world a hop was asked for, saved just before asking. The next start sees
	// it, knows the restart was the ring's own hop rather than an operator's, and clears it.
	HopPending int `json:"hopPending"`
	// HopsThisRound counts the hops asked for in this round (maxHopsPerRound bounds the ring's).
	HopsThisRound int `json:"hopsThisRound"`
	// DeliverHopRound is the round in which the delivery last asked for a hop to the mule's
	// world. One such hop per round: a second one in the same round (the first did not land, or
	// the mule's world flapped) defers the delivery instead of hopping again.
	DeliverHopRound int `json:"deliverHopRound"`
	// BankRunes and BankCoins are the last bank reading and BankReadAt when it was made.
	BankRunes  map[string]int `json:"bankRunes"`
	BankCoins  int            `json:"bankCoins"`
	BankReadAt time.Time      `json:"bankReadAt"`
	// RoundsCompleted counts finished rounds (stopAfterRounds reads it); the caller closing a
	// round counts it.
	RoundsCompleted int `json:"roundsCompleted"`
	// LastTopUpAsk is when the draw station last asked the mule for coins (TopUpRetry bounds the
	// next ask, across the restarts a hop causes); TopUpReceived totals the coins the mule gave.
	LastTopUpAsk  time.Time `json:"lastTopUpAsk,omitempty"`
	TopUpReceived int       `json:"topUpReceived,omitempty"`
	// RoundTopUp is the coins the mule gave in THIS round (the purse station's and the market's), for the
	// round-close purse line (RoundPurse). A new round clears it; saved so a hop's restart does not lose it.
	RoundTopUp int `json:"roundTopUp,omitempty"`
	// LundailKeep is the coins of the bank the round's draw station kept back for the bank-paid stations
	// still ahead (RoundNeed.BankKeep): every later shop-bank top-up leaves what RoundNeed.KeepNow of it
	// still owes Lundail, so a short purse is not drawn away leg by leg before Lundail is reached. It is
	// saved because the hops between worlds restart the script. A new round clears it.
	LundailKeep int `json:"lundailKeep,omitempty"`
	// Rotation is true when Ring is a rotation round (BuildRotationRing), which Resume and the
	// hop station read differently from the classic ring.
	Rotation bool `json:"rotation,omitempty"`
	// HopTimes are the hops this account asked for in the last HopWindow, for HopBudget. Saved
	// before every hop, so the budget survives the restart the hop causes.
	HopTimes []time.Time `json:"hopTimes,omitempty"`
	// Swarm is true while the runner runs ScarabSwarm between circuits (swarm.go); SwarmSince is
	// when it switched and SwarmChecked when it last read the restock estimate. SwarmDue marks the
	// delivery-only round that hands over before the switch.
	Swarm        bool      `json:"swarm,omitempty"`
	SwarmSince   time.Time `json:"swarmSince,omitempty"`
	SwarmChecked time.Time `json:"swarmChecked,omitempty"`
	SwarmDue     bool      `json:"swarmDue,omitempty"`
	// LastMarketCheck is when the market station last looked at the stalls (marketCheckMinutes
	// bounds the next look, across the restarts a hop causes); MarketBought totals the runes it
	// bought and MarketSpent the coins.
	LastMarketCheck time.Time      `json:"lastMarketCheck,omitempty"`
	MarketBought    map[string]int `json:"marketBought,omitempty"`
	MarketSpent     int            `json:"marketSpent,omitempty"`
	// TrawlerRound is the round in which the account last joined master's trawler crew and TrawlerSince when (the
	// trawlerStintMinutes clock, trawler.go); saved because every trawler game ends in a hop that restarts the script.
	TrawlerRound int       `json:"trawlerRound,omitempty"`
	TrawlerSince time.Time `json:"trawlerSince,omitempty"`
}

// NoteHop records a hop asked for at now, keeping HopTimes pruned to HopWindow.
func (st *State) NoteHop(now time.Time) {
	st.HopTimes = append(PruneHops(st.HopTimes, now), now)
}

// UnnoteHop forgets the hop NoteHop recorded at `at` (the fleet refused it).
func (st *State) UnnoteHop(at time.Time) {
	for i := len(st.HopTimes) - 1; i >= 0; i-- {
		if st.HopTimes[i].Equal(at) {
			st.HopTimes = append(st.HopTimes[:i], st.HopTimes[i+1:]...)
			return
		}
	}
}

// NewState is a state with nothing done yet.
func NewState() State {
	st := State{Version: StateVersion}
	st.normalise()
	return st
}

// normalise gives every map a value, so a state read from an older file is safe to write into.
func (st *State) normalise() {
	if st.Version == 0 {
		st.Version = StateVersion
	}
	if st.Done == nil {
		st.Done = map[string]StationResult{}
	}
	if st.PairStrikes == nil {
		st.PairStrikes = map[string]int{}
	}
	if st.PairOffUntil == nil {
		st.PairOffUntil = map[string]time.Time{}
	}
	if st.PriceMismatch == nil {
		st.PriceMismatch = map[string]int{}
	}
	if st.Bought == nil {
		st.Bought = map[string]int{}
	}
	if st.Delivered == nil {
		st.Delivered = map[string]int{}
	}
	if st.BankRunes == nil {
		st.BankRunes = map[string]int{}
	}
}

// Normalise is normalise for callers outside the package (a state just decoded).
func (st *State) Normalise() { st.normalise() }

// Current is the station at the cursor.
func (st State) Current() (Station, bool) {
	if st.Cursor < 0 || st.Cursor >= len(st.Ring) {
		return Station{}, false
	}
	return st.Ring[st.Cursor], true
}

// StartRound begins a round at cursor with a fresh ring.
func (st *State) StartRound(ring []Station, cursor int, now time.Time) {
	st.normalise()
	st.Round++
	st.RoundStarted = now
	st.Ring = ring
	if cursor < 0 || cursor >= len(ring) {
		cursor = 0
	}
	st.Cursor = cursor
	st.Done = map[string]StationResult{}
	st.HopsThisRound = 0
	st.LundailKeep = 0
	st.RoundTopUp = 0
}

// NoteTopUp records coins the mule gave: the all-time total and this round's.
func (st *State) NoteTopUp(coins int) {
	if coins <= 0 {
		return
	}
	st.TopUpReceived += coins
	st.RoundTopUp += coins
}

// Finish records the station at the cursor and moves on. roundOver is true when that was the
// ring's last station; the caller starts the next round.
func (st *State) Finish(r StationResult) (roundOver bool) {
	st.normalise()
	s, ok := st.Current()
	if ok {
		st.Done[s.Key()] = r
	}
	for k, n := range r.Bought {
		st.Bought[k] += n
	}
	for k, n := range r.Delivered {
		st.Delivered[k] += n
	}
	st.Spent += r.Spent
	st.Cursor++
	return st.Cursor >= len(st.Ring)
}

// JumpTo moves the cursor without recording anything (Resume, the O block skipped).
func (st *State) JumpTo(i int) {
	if i < 0 {
		i = 0
	}
	st.Cursor = i
}

// DoneThisRound reports whether a station already finished in this round.
func (st State) DoneThisRound(key string) bool {
	_, ok := st.Done[key]
	return ok
}

// RoundTotals sums this round's purchases, spend and deliveries.
func (st State) RoundTotals() (bought map[string]int, spent int, delivered map[string]int) {
	bought, delivered = map[string]int{}, map[string]int{}
	for _, r := range st.Done {
		for k, n := range r.Bought {
			bought[k] += n
		}
		for k, n := range r.Delivered {
			delivered[k] += n
		}
		spent += r.Spent
	}
	return bought, spent, delivered
}

// RoundShort is what this round left undone for lack of coins: the stations that did not buy for
// coins (sorted by key) and the runes, and their cost at the shelf's prices, that visits left on a
// shelf for coins.
func (st State) RoundShort() (stations []string, runes, coins int) {
	for k, r := range st.Done {
		if r.NoCoins {
			stations = append(stations, k)
		}
		runes += r.ShortRunes
		coins += r.ShortCoins
	}
	sort.Strings(stations)
	return stations, runes, coins
}

// DeliverHopAllowed is the one-delivery-hop-per-round rule (DeliverHopRound).
func (st State) DeliverHopAllowed() (bool, string) {
	if st.Round > 0 && st.DeliverHopRound == st.Round {
		return false, fmt.Sprintf("round %d already hopped once for its delivery", st.Round)
	}
	return true, ""
}

// NoteBank records a bank reading.
func (st *State) NoteBank(runes map[string]int, coins int, now time.Time) {
	st.BankRunes = map[string]int{}
	for k, n := range runes {
		st.BankRunes[k] = n
	}
	st.BankCoins = coins
	st.BankReadAt = now
}

// NoteLundailDeath records a death on a Lundail trip, keeping the window pruned.
func (st *State) NoteLundailDeath(now time.Time) {
	st.LundailDeaths = append(PruneDeaths(st.LundailDeaths, now), now)
}

// EndTrip closes the Mage Bank trip: what it bought (OutcomeAborted with `why` when it bought
// nothing), and the trip state reset so no later leg is chosen against it.
func (st *State) EndTrip(now time.Time, why string) StationResult {
	r := StationResult{Outcome: OutcomeAborted, Reason: why, At: now}
	if st.TripResult != nil {
		r = *st.TripResult
		r.At = now
	}
	st.Trip = fleetplan.Trip{}
	st.TripWorld = 0
	st.TripResult = nil
	return r
}

// FormatCounts is a stable one-line rendering of a rune map ("Chaos rune=10,Mind rune=900").
func FormatCounts(m map[string]int) string {
	if len(m) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += ","
		}
		out += k + "=" + strconv.Itoa(m[k])
	}
	return out
}
