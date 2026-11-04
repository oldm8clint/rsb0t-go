// Package kqkitplan is the owner's "the mule hands out kits ready for taking on the Kalphite
// Queen" (2 October 2026): which accounts a KQ kit is worth handing to, and what the kit is, as
// mule ask lines the ScarabSwarm mule-ask entry point already knows how to serve
// (scarabplan.AskLine -> FilterAskByBank -> one bundled kit order).
//
// It builds no combat. Stage 6 (scarabplan/kq.go, scarabrunner/kq.go) is the fight; this is only
// the kit and the gate in front of it, and both reuse Stage 6's own content-grounded numbers
// (KQSettings, PlanEntryKit's coins, the rope and waterskin items).
//
// THE CONTENT FACTS the gate is built on (the live server content, revision 289):
//
//   - Her magic attack hits every account within 10 tiles of her target (huntall(coord, 10, 1)) for
//     randominc(31), and her ranged attack up to 5 accounts within 5 tiles for randominc(31); both
//     ignore Defence and armour and are zeroed only by the matching protection prayer
//     (area_kalphite/scripts/kalphite_queen.rs2:90-99 and :135-139). So 31 is the number that
//     matters: an account with 31 hitpoints or fewer can die to one hit, whatever it wears.
//   - Each form has 255 hitpoints (kalphite.npc:210), attack/strength/defence 300, magic 150; combat
//     level 333 (vislevel). The second form (kalphite_flyingqueen) heals to full on the change.
//   - Protect from Magic is Prayer 37, Protect from Missiles 40, Protect from Melee 43
//     (skill_prayer/configs/prayers.dbrow:109,118,127). No shop and no Seers Market listing sells a
//     prayer potion (2 Oct /api/market), so a protected account's prayer lasts what its points last.
//
// It is pure: no client, actions, fleet or bot imports.
package kqkitplan

import (
	"strings"

	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/fleetplan"
)

// Content numbers; see the package doc for where each comes from.
const (
	QueenMaxHit              = 31
	QueenHitpoints           = 255
	ProtectFromMagicLevel    = 37
	ProtectFromMissilesLevel = 40
	ProtectFromMeleeLevel    = 43
)

// Skills are the levels the gate reads.
type Skills struct {
	Magic, Hitpoints, Prayer int
}

// Tier is how ready an account is.
type Tier int

const (
	// TierNone: one of her hits can kill it, or it cannot cast the kit's spell.
	TierNone Tier = iota
	// TierSwarm is Stage 6's own naked-staff design: survives any ONE hit and casts the spell.
	// Death is still the normal end of a life; the Queen's damage persists across lives and
	// accounts, which is the whole swarm argument (docs/fleet_expansion_2026-09-18.md).
	TierSwarm
	// TierProtected adds Protect from Magic: her magic attack, the one that reaches every account
	// within ten tiles, does nothing; her ranged attack still does.
	TierProtected
	// TierSteady adds the hitpoints to take two ranged maximums between bites while protected.
	TierSteady
)

// SteadyHitpoints is two of her maximum hits plus one.
const SteadyHitpoints = 2*QueenMaxHit + 1

// String is the tier's log name.
func (t Tier) String() string {
	switch t {
	case TierSwarm:
		return "swarm"
	case TierProtected:
		return "protected"
	case TierSteady:
		return "steady"
	}
	return "none"
}

// Eligibility is the account's tier for a kit that casts spell, and the reason for the tier.
func Eligibility(sk Skills, spell fleetplan.Spell) (Tier, string) {
	switch {
	case spell.Name == "":
		return TierNone, "no spell for the kit"
	case sk.Magic < spell.Level:
		return TierNone, "Magic " + itoa(sk.Magic) + " cannot cast " + spell.Name + " (" + itoa(spell.Level) + ")"
	case sk.Hitpoints <= QueenMaxHit:
		return TierNone, "Hitpoints " + itoa(sk.Hitpoints) + ": one of her " + itoa(QueenMaxHit) + "s can kill it"
	case sk.Prayer < ProtectFromMagicLevel:
		return TierSwarm, "survives one hit; no Protect from Magic (Prayer " + itoa(sk.Prayer) + " of " + itoa(ProtectFromMagicLevel) + ")"
	case sk.Hitpoints < SteadyHitpoints:
		return TierProtected, "Protect from Magic; Hitpoints " + itoa(sk.Hitpoints) + " under " + itoa(SteadyHitpoints)
	}
	return TierSteady, "Protect from Magic and two ranged maximums of hitpoints"
}

// Census counts accounts per tier.
func Census(accounts []Skills, spell fleetplan.Spell) map[Tier]int {
	out := map[Tier]int{}
	for _, a := range accounts {
		t, _ := Eligibility(a, spell)
		out[t]++
	}
	return out
}

// StaffLadder is the staff a kit asks for, best first: only a Staff of air pays a bolt's air.
var StaffLadder = []string{"Staff of air", "Magic staff", "Staff"}

// FoodLadder is the kit's food, biggest heal first; nothing under 12 a bite (a cake is 4 a bite
// against her 31s). Bulk asks take whichever the mule holds most of.
var FoodLadder = []string{"Manta ray", "Sea turtle", "Shark", "Swordfish", "Bass", "Lobster"}

// PackSlots is the backpack (28) and KitSlotsSpare what a kit leaves for the pass, waterskin and
// what the Shantay visit buys.
const (
	PackSlots     = 28
	KitSlotsSpare = 4
)

// KitInput is one kit decision.
type KitInput struct {
	Spell fleetplan.Spell
	// Staff is the autocast staff the account already holds, bank or pack, "" for none.
	Staff string
	// KQ is Stage 6's settings: casts per life, the healing a life carries, ropes, coins.
	KQ fleetplan.KQSettings
	// Region is where the account banks before it sets out (the coins it needs depend on it).
	Region fleetplan.KQRegion
	// Lives is how many lives' worth of runes one hand-out covers; < 1 means 1.
	Lives int
	// Have is what the account already holds, bank and pack, by item name; FoodHeld is its food
	// count across every food.
	Have     map[string]int
	FoodHeld int
}

// Kit is the mule ask for a KQ kit: only what the account is short of. An account that holds
// the whole kit already gets no lines.
func Kit(in KitInput) []fleetplan.AskLine {
	kq := in.KQ.Normalised()
	lives := max(1, in.Lives)
	var lines []fleetplan.AskLine
	staff := in.Staff
	if staff == "" {
		lines = append(lines, fleetplan.AskLine{Choices: append([]string(nil), StaffLadder...), Count: 1, TryDown: true})
		staff = StaffLadder[0] // the runes are planned for the best rung; a lower rung means more air next time
	}
	slots := 0
	chaosLined := false
	for _, r := range fleetplan.RunesForCasts(in.Spell, kq.CastsPerLife*lives, fleetplan.StaffProvides(staff)) {
		if strings.EqualFold(r.Name, fleetplan.ChaosRune) {
			// Every kit carries the owner's 1,000 chaos to train with (2 Oct 2026), whatever the life's
			// casts need: topped up to it, carried and banked together, only what the account lacks.
			chaosLined = true
			r.Count = max(r.Count, StarterChaos)
		}
		if short := r.Count - have(in.Have, r.Name); short > 0 {
			lines = append(lines, fleetplan.AskLine{Choices: []string{r.Name}, Count: short, FairShare: fleetplan.ScarceRunes[r.Name]})
		}
		slots++
	}
	if !chaosLined {
		if short := StarterChaos - have(in.Have, fleetplan.ChaosRune); short > 0 {
			lines = append(lines, fleetplan.AskLine{Choices: []string{fleetplan.ChaosRune}, Count: short})
		}
		slots++
	}
	if short := kq.Ropes - have(in.Have, fleetplan.RopeItem); short > 0 {
		lines = append(lines, fleetplan.AskLine{Choices: []string{fleetplan.RopeItem}, Count: short})
	}
	slots += kq.Ropes // a rope does not stack
	slots++           // coins
	if short := fleetplan.KitCoins(in.Region, kq) - have(in.Have, "Coins"); short > 0 {
		lines = append(lines, fleetplan.AskLine{Choices: []string{"Coins"}, Count: short})
	}
	if food := FoodFor(kq.FoodHeal, PackSlots-KitSlotsSpare-slots) - max(0, in.FoodHeld); food > 0 {
		lines = append(lines, fleetplan.AskLine{Choices: append([]string(nil), FoodLadder...), Count: food, Bulk: true})
	}
	return lines
}

// FoodFor is the bites that carry heal hitpoints at the ladder's smallest heal (so whichever rung
// the mule hands over, the life carries at least heal), capped at the slots left.
func FoodFor(heal, slots int) int {
	if heal <= 0 || slots <= 0 {
		return 0
	}
	per := minHeal()
	n := (heal + per - 1) / per
	return min(n, slots)
}

func minHeal() int {
	m := 0
	for _, f := range FoodLadder {
		h := fighterfood.FoodHealAmount(f)
		if m == 0 || (h > 0 && h < m) {
			m = h
		}
	}
	if m <= 0 {
		return 1
	}
	return m
}

func have(m map[string]int, name string) int {
	for k, v := range m {
		if strings.EqualFold(strings.TrimSpace(k), name) {
			return v
		}
	}
	return 0
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
