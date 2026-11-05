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
	// DefaultSwarmReadyPercent is the share of a full-shelf circuit's purchase the next circuit must expect (a user request: "time how
	// long it takes for the shops to restock before starting another shopping round"). Measured the same day: a shelf regrows at a fixed
	// rate (Betty's runes one per 10 ticks, a thousand in 100 minutes) and acct's rounds, 43-57 minutes, ran back to back, so the
	// stock a round found was only what had regrown since the last one. Waiting for 60% buys the same runes an hour and gives the
	// difference to ScarabSwarm; 100 would let the shelves cap and lose stock.
	DefaultSwarmReadyPercent = 60
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
	// FullWorth is what the same circuit would buy with every shelf regrown to its base (the board's estimate a day on), ReadyPercent the
	// swarmReadyPercent setting: the circuit is not walked until it would buy that share of FullWorth. MuleShort is the mule being out
	// of a scarce rune, which halves the share.
	FullWorth, ReadyPercent int
	MuleShort               bool
}

// Target is the circuit worth the runner leaves ScarabSwarm for (or stays on the circuit at): swarmResumeRunes, or ReadyPercent of
// FullWorth when that is more -- half of it while the mule is short of a scarce rune, which is a reason to come back sooner but not to
// walk a circuit that has barely regrown (the stock a shelf yields is the same, walked or not).
func (c SwarmCheck) Target() int {
	t := c.Resume
	if c.ReadyPercent > 0 {
		pct := c.ReadyPercent
		if c.MuleShort {
			pct /= 2
		}
		t = max(t, c.FullWorth*pct/100)
	}
	return t
}

// targetWhy words the target for a log line: the setting alone, or the share of full shelves that raised it.
func (c SwarmCheck) targetWhy() string {
	if t := c.Target(); t > c.Resume {
		return fmt.Sprintf("%d (%d%% of the %d a full-shelf circuit buys; swarmResumeRunes %d)", t, c.ReadyPercent, c.FullWorth, c.Resume)
	}
	return fmt.Sprintf("swarmResumeRunes %d", c.Resume)
}

// SwarmAfterRound says whether a rotation round that has just ended hands over and switches to
// ScarabSwarm: the circuit is not worth another walk yet.
func SwarmAfterRound(c SwarmCheck) (bool, string) {
	switch {
	case !c.On:
		return false, "swarmWhileWaiting is off"
	case !c.Rotation:
		return false, "the classic ring does not switch"
	case c.CircuitWorth >= c.Target():
		return false, fmt.Sprintf("the next circuit expects %d runes (%s)", c.CircuitWorth, c.targetWhy())
	}
	return true, fmt.Sprintf("the next circuit expects %d runes, under %s", c.CircuitWorth, c.targetWhy())
}

// SwarmResume says whether a runner running ScarabSwarm goes back to the circuit (at the next safe
// point): the circuit is worth it again, or swarmWhileWaiting was turned off.
func SwarmResume(c SwarmCheck) (bool, string) {
	switch {
	case !c.On || !c.Rotation:
		return true, "swarmWhileWaiting is off or the runner no longer rotates"
	case c.CircuitWorth >= c.Target():
		return true, fmt.Sprintf("the circuit expects %d runes again (%s)", c.CircuitWorth, c.targetWhy())
	}
	return false, fmt.Sprintf("the circuit expects %d runes, under %s", c.CircuitWorth, c.targetWhy())
}

// RestockWait is how long until a circuit is worth walking: the first step (a multiple of step, up to horizon) at which worthAt
// reaches target. 0 and true when it already does; false when the horizon passes first.
func RestockWait(worthAt func(after time.Duration) int, target int, step, horizon time.Duration) (time.Duration, bool) {
	if worthAt == nil || step <= 0 {
		return 0, false
	}
	for d := time.Duration(0); d <= horizon; d += step {
		if worthAt(d) >= target {
			return d, true
		}
	}
	return 0, false
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
