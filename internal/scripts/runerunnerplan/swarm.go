package runerunnerplan

// ScarabSwarm while the shelves regrow.
//
// a user request 22:20: "runerunner should go back to scarabswarm after buying all the
// runes from all 3 shops on all 3 worlds, while it waits for the shops to restock". So a rotating
// runner whose circuit has just finished, and whose next circuit is not worth walking -- the
// board's restock estimate over every pair, under the caps, comes to less than swarmResumeRunes
// -- first hands what it holds to the mule (a delivery-only round on the mule's world, the gate
// lowered to a single rune), then runs ScarabSwarm, whole, from its own checkpoint and with the
// settings the swarmSettings object passes through. Every swarmCheckMinutes it reads the estimate
// again; once a circuit would buy swarmResumeRunes it stops ScarabSwarm at a safe point (the
// rules MasterCrafter stops it by, mastercraftplan.SafePoint) and starts a rotation round from
// where the account stands.

import (
	"fmt"
	"time"
)

// Defaults and bounds of the swarm settings.
const (
	DefaultSwarmWhileWaiting = true
	DefaultSwarmCheckMinutes = 20
	MinSwarmCheckMinutes     = 5
	MaxSwarmCheckMinutes     = 240
	// DefaultSwarmResumeRunes is the circuit's estimated purchase worth leaving ScarabSwarm for. A
	// full three-world circuit is about an hour of walking and seven hops; at the default caps a
	// fully regrown Aubury or Betty pair alone is worth about 1,600 runes and a Lundail pair about
	// 700, so 1,500 is roughly one good shop's worth across the whole circuit.
	DefaultSwarmResumeRunes = 1500
	MaxSwarmResumeRunes     = 100000
)

// SwarmCheck is what the switch decisions read.
type SwarmCheck struct {
	// On is the swarmWhileWaiting setting; Rotation whether the runner rotates (the classic ring
	// never switches).
	On, Rotation bool
	// CircuitWorth is the runes a circuit would buy now under the caps, from the board's estimate
	// (CircuitWorth over the bot's per-pair worth).
	CircuitWorth int
	// Resume is the swarmResumeRunes setting.
	Resume int
}

// SwarmAfterRound says whether a rotation round that has just ended hands over and switches to
// ScarabSwarm: the circuit is not worth another walk yet.
func SwarmAfterRound(c SwarmCheck) (bool, string) {
	switch {
	case !c.On:
		return false, "swarmWhileWaiting is off"
	case !c.Rotation:
		return false, "the classic ring does not switch"
	case c.CircuitWorth >= c.Resume:
		return false, fmt.Sprintf("the next circuit expects %d runes (swarmResumeRunes %d)", c.CircuitWorth, c.Resume)
	}
	return true, fmt.Sprintf("the next circuit expects %d runes, under swarmResumeRunes %d", c.CircuitWorth, c.Resume)
}

// SwarmResume says whether a runner running ScarabSwarm goes back to the circuit (at the next safe
// point): the circuit is worth it again, or swarmWhileWaiting was turned off.
func SwarmResume(c SwarmCheck) (bool, string) {
	switch {
	case !c.On || !c.Rotation:
		return true, "swarmWhileWaiting is off or the runner no longer rotates"
	case c.CircuitWorth >= c.Resume:
		return true, fmt.Sprintf("the circuit expects %d runes again (swarmResumeRunes %d)", c.CircuitWorth, c.Resume)
	}
	return false, fmt.Sprintf("the circuit expects %d runes, under swarmResumeRunes %d", c.CircuitWorth, c.Resume)
}

// CircuitWorth sums worth(site, world) over the circuit's pairs.
func CircuitWorth(pairs []Pair, worth func(site string, world int) int) int {
	if worth == nil {
		return 0
	}
	total := 0
	for _, p := range pairs {
		total += worth(p.Site, p.World)
	}
	return total
}

// SwarmCheckDue reports whether the swarm phase's restock check is due.
func SwarmCheckDue(last, now time.Time, every time.Duration) bool {
	return last.IsZero() || now.Sub(last) >= every
}

// SwarmSettingsMap is the settings ScarabSwarm runs with: a copy of the swarmSettings object,
// with this runner's mule when the object names none.
//
// THE CIRCUIT'S COINS STAY (live, 7 Oct 2026: acct35's ScarabSwarm phase handed its 261,293 circuit
// coins to the mule on a kit ask, then the next circuit had to ask the mule for them back). Unless
// the object says otherwise, a kit ask hands nothing over (consolidateOnKitAsk false) and the daily
// trip hands over NO coins (consolidateCoinsReserve = SwarmKeepAllCoins). It was MaxCircuitFunds, 8,000,000,
// from 13 Oct 2026; on 14 Oct 2026 the runner was told to take the mule's ENTIRE stack (11,229,284 coins
// that day) and keep what the round does not spend in its own bank for the next one, and an 8,000,000 reserve
// would have handed the 3,000,000 above it straight back on the next daily trip: a hand-back and a
// re-ask every round. The runner's coins reach the shops, not the mule.
// Everything the runner buys reaches the mule at its own delivery anyway.
func (s Settings) SwarmSettingsMap() map[string]interface{} {
	out := make(map[string]interface{}, len(s.SwarmSettings)+3)
	for k, v := range s.SwarmSettings {
		out[k] = v
	}
	if m, ok := out["mule"].(string); !ok || m == "" {
		out["mule"] = s.Mule
	}
	if _, ok := out["consolidateOnKitAsk"]; !ok {
		out["consolidateOnKitAsk"] = false
	}
	if _, ok := out["consolidateCoinsReserve"]; !ok {
		out["consolidateCoinsReserve"] = float64(SwarmKeepAllCoins)
	}
	return out
}

// SwarmKeepAllCoins is the consolidateCoinsReserve a swarm phase runs with: two billion, just under the
// 2,147,483,647 one coin stack holds, so no coin in the bank is a surplus and neither the daily trip nor a kit
// ask hands the mule any. The bank is the runner's purse.
const SwarmKeepAllCoins = 2000000000

// SwarmOn reports whether the runner switches to ScarabSwarm between circuits.
func (r Resolved) SwarmOn() bool { return r.Rotation && r.SwarmWhileWaiting }

// SwarmEvery is the swarmCheckMinutes setting as a duration.
func (s Settings) SwarmEvery() time.Duration { return time.Duration(s.SwarmCheckMinutes) * time.Minute }

// DeliveryRing is the one-station round a runner makes before it switches to ScarabSwarm: the
// delivery on the mule's world.
func DeliveryRing(m int) []Station { return []Station{{Kind: KindDeliver, World: m}} }
