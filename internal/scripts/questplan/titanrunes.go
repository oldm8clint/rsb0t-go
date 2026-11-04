package questplan

import (
	"fmt"
	"strings"
	"time"
)

// THE RUNE GATE: an account does not go into the realm, and does not start the fight, on runes that
// cannot kill him.
//
// The kit's rune lines are Optional (a mule or a bank that lacks one must not block the quest, and
// the spell families are alternatives), so a provisioned kit proves nothing about the runes. The
// gate is where they are checked. Review of 13 Oct 2026: the live banks held chaos at 3 to 70 (a
// Wind Bolt is out of reach) and mind at 58 to 1,000, and the mule holds no mind or chaos at all
// (and, since the rune pool, hands chaos out only from a stack of 20,000), so an account could
// collect the whistles, cross to Karamja, blow into the realm and spend the sword fallback, which
// cannot win, then park and do it all again.
//
// WHAT "ENOUGH" MEANS. A cast lands with probability A/(2D) (the engine's randominc(A) >
// randominc(D) with A the player's roll and D = TitanDefenceRoll) and then hits for 0 to the
// spell's maximum, so a cast is worth TitanHitChance x MaxHit / 2 hitpoints on average: 0.83 for
// Fire Strike and 0.93 for Wind Bolt at Magic 60. He has TitanHitpoints and regenerates one every
// 100 ticks (every 20 casts at a cast per 5 ticks). The pack's runes must pay for an EXPECTED
// TitanDamageNeeded (a third over his hitpoints, which covers his regeneration and puts the kill at
// roughly nine fights in ten: titanrunes_test.go simulates the engine's roll with a hard cap on the
// iterations), counted the way the fight spends them (ForecastTitan: one spell until its runes are
// spent, then the best of what is left).

const (
	// TitanDefenceRoll is his side of a cast's roll: his Magic LEVEL, which content leaves at 1,
	// plus 9, times his magic defence 1000 plus 64 (black_knight_titan config; the probe in
	// docs/evidence/grail-titan-2026-10-13).
	TitanDefenceRoll = (1 + 9) * (1000 + 64)
	// TitanMaxCasts is the most casts one fight makes: its time budget at a cast every 3 seconds.
	TitanMaxCasts = int(TitanFightBudget / (3 * time.Second))
	// TitanDamageNeeded is the expected damage the pack's runes must pay for before the account goes
	// near the realm: his hitpoints and a third.
	TitanDamageNeeded = float64(TitanHitpoints) * 4 / 3
	// TitanParkKit keeps the quest out when the runes are short, so the next run re-provisions the
	// kit (the mule's stack, the bank's mind runes and the shops move in hours, not minutes).
	TitanParkKit = time.Hour
)

// TitanHitChance is the share of casts that land at a Magic level, with no Magic bonus (the
// accounts wear none that counts): the engine's randominc(A) > randominc(D) for A the player's
// (magic + 9) x 64 and D = TitanDefenceRoll. About one in five at Magic 60.
func TitanHitChance(magic int) float64 {
	if magic < 1 {
		return 0
	}
	a := float64((magic + 9) * 64)
	d := float64(TitanDefenceRoll)
	if a > d {
		return 1 - d/(2*a)
	}
	return a / (2 * d)
}

// TitanCastDamage is what one cast of s is worth on average: a cast lands TitanHitChance of the
// time for 0 to MaxHit.
func TitanCastDamage(magic int, s TitanSpell) float64 {
	return TitanHitChance(magic) * float64(s.MaxHit) / 2
}

// NextTitanSpell is the spell for the next cast. Before the first cast (started false) it is
// PickTitanSpell's choice, which needs TitanMinCasts of the runes to begin. After it the fight
// STAYS with its spell until the runes for it are spent (choosing afresh on every cast would swap
// between two spells cast by cast as their stocks drained past each other), then takes the best of
// what is left however few casts that is: a begun fight is finished with what it has.
func NextTitanSpell(cur TitanSpell, started bool, magic int, stock map[int]int) (TitanSpell, bool) {
	if started && cur.Name != "" && cur.Level <= magic && cur.Casts(stock) >= 1 {
		return cur, true
	}
	minCasts := TitanMinCasts
	if started {
		minCasts = 1
	}
	sp, _, ok := pickTitanSpell(magic, stock, TitanCastBudget, minCasts)
	return sp, ok
}

// TitanForecastPart is a run of casts of one spell.
type TitanForecastPart struct {
	Spell string
	Casts int
}

// TitanForecast is what a pack's runes can do to him.
type TitanForecast struct {
	// Casts is how many casts the runes pay for (at most TitanMaxCasts).
	Casts int
	// Damage is the expected damage of those casts, before his regeneration.
	Damage float64
	// Parts are the spells in the order they are cast, each once, with its casts.
	Parts []TitanForecastPart
}

// Describe is the parts in a line: "Fire Strike" for one spell, "120 Fire Strike then 140 Wind Strike"
// for more.
func (f TitanForecast) Describe() string {
	if len(f.Parts) == 1 {
		return f.Parts[0].Spell
	}
	var parts []string
	for _, p := range f.Parts {
		parts = append(parts, fmt.Sprintf("%d %s", p.Casts, p.Spell))
	}
	return strings.Join(parts, " then ")
}

// ForecastTitan casts the stock away the way the fight does and adds up the expected damage.
func ForecastTitan(magic int, stock map[int]int) TitanForecast {
	left := make(map[int]int, len(stock))
	for id, n := range stock {
		left[id] = n
	}
	var f TitanForecast
	var cur TitanSpell
	for f.Casts < TitanMaxCasts {
		sp, ok := NextTitanSpell(cur, f.Casts > 0, magic, left)
		if !ok {
			break
		}
		if len(f.Parts) == 0 || sp.Name != cur.Name {
			f.Parts = append(f.Parts, TitanForecastPart{Spell: sp.Name})
		}
		f.Parts[len(f.Parts)-1].Casts++
		cur = sp
		for _, r := range sp.Runes {
			left[r.ID] -= r.Count
		}
		f.Casts++
		f.Damage += TitanCastDamage(magic, sp)
	}
	return f
}

// TitanDamageNeededAt is the expected damage the runes must pay for against a Titan seen at health
// of total: TitanDamageNeeded scaled by what is left of him, all of it when he has not been seen.
func TitanDamageNeededAt(health, total int) float64 {
	if total <= 0 || health <= 0 || health >= total {
		return TitanDamageNeeded
	}
	return TitanDamageNeeded * float64(health) / float64(total)
}

// TitanRunesEnough is the gate: whether the pack's runes (stock, by object id) at this Magic level
// pay for the expected damage the Titan takes (TitanDamageNeededAt health total; pass 0, 0 when he
// has not been seen), and a one-line account of the forecast for the log or the park reason.
func TitanRunesEnough(magic int, stock map[int]int, health, total int) (bool, string) {
	need := TitanDamageNeededAt(health, total)
	f := ForecastTitan(magic, stock)
	if f.Casts == 0 {
		return false, fmt.Sprintf("no spell the pack's runes pay for at Magic %d (a spell needs runes for %d casts to begin)", magic, TitanMinCasts)
	}
	summary := fmt.Sprintf("%d casts (%s) at Magic %d, about %.0f damage of the %.0f he needs", f.Casts, f.Describe(), magic, f.Damage, need)
	if f.Damage < need {
		return false, "the pack's runes pay for only " + summary
	}
	return true, summary
}
