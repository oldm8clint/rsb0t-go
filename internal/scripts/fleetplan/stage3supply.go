package fleetplan

import (
	"strings"
)

// DefaultStage3Spell is the spell Stage 3 stocks runes for: Fire Strike, at the owner's Magic
// target of 13 (owner decision, 2026-09-19): it hits 8 against Wind Bolt's 9 at 17, measured 0.82
// damage a cast against the Queen against Wind Bolt's 0.62, and costs no chaos rune. The bolt
// tiers come later, once Paladin chaos at 70 Thieving exists to pay for them.
//
// The supply caveat that decision carries, from the content numbers: Fire Strike costs 1 mind,
// 3 fire and 2 air a cast, and the Mage Bank regrows 120 of each elemental and 120 mind an hour
// for the whole world — about 40 Fire Strikes an hour, world-wide, once the starting stock is
// gone. So for a swarm the elemental runes, not chaos, are the shop-side ceiling; the stockpile
// the mule builds (and Aubury, if ever enabled) is what has to cover the difference.
const DefaultStage3Spell = "Fire Strike"

// Stage3Settings are Stage 3's knobs. JSON keys match internal/bot's setting names.
type Stage3Settings struct {
	// Spell is the spell whose runes are stocked (a CombatSpells name).
	Spell string `json:"stage3Spell"`
	// TopTrainSpell is stage3TrainTopSpell as the rune supply read it (bot/scarabswarm_trainladder.go):
	// from its level every training cast is that spell. Set by the bot, not decoded here; only the
	// mind-rune holder arithmetic reads it (MindsStillNeeded).
	TopTrainSpell string `json:"-"`
	// CastsTarget is how many casts' worth of runes, banked, completes the stage's supply goal.
	CastsTarget int `json:"stage3CastsTarget"`
	// CastsPerTrip caps one supply trip's purchase or ask, in casts: a Mage Bank, Aubury or Guild purchase
	// and a mule ask alike. 1,000 since 6 Oct 2026 (it was 250), the size of one training round
	// (DefaultTrainCasts) and of the mule's per-order ceiling on one item (orderplan.OpenMaxPerOrder), so
	// an ask for a round is never cut by this cap.
	CastsPerTrip int `json:"stage3CastsPerTrip"`
	// MinTripRunes is the smallest purchase worth crossing the Wilderness for. Below it the account
	// thieves and waits for Lundail's stock to regrow — "timing the shop for restock".
	MinTripRunes int `json:"stage3MinTripRunes"`
	// CoinReserve stays in the bank, never carried: fares home after a Wilderness death.
	CoinReserve int `json:"stage3CoinReserve"`
	// Staff is the staff the account fights with ("" for none, or "Staff", "Staff of air"...);
	// its free rune is left off every shopping list.
	Staff string `json:"stage3Staff"`
	// MaxPriceMultiple stops buying a rune once Lundail's price passes this multiple of its base
	// cost (her delta of 30 walks a line up to 6x); 0 for no cap.
	MaxPriceMultiple int `json:"stage3MaxPriceMultiple"`
	// FoodKeep is bakery food withdrawn to thieve with between trips; EatBelowHP the eat point.
	// MagicTarget is the level training stops at (owner: 13, Fire Strike); TrainSpell and
	// TrainCasts what training casts and how many casts' runes one round stocks.
	MagicTarget int    `json:"stage3MagicTarget"`
	TrainSpell  string `json:"stage3TrainSpell"`
	TrainCasts  int    `json:"stage3TrainCasts"`
	FoodKeep    int    `json:"stage3FoodKeep"`
	EatBelowHP  int    `json:"stage3EatBelowHp"`
	Supply      SupplySettings
	// MuleHolds is set by the bot each pass: whether the mule's published bank holds a rune worth asking
	// for (MuleRuneFloor). Every SHOP and MARKET want -- ShopWant, AuburyWant, GuildWant -- leaves such a
	// rune out, so an account never walks to Lundail, Aubury, the Wizards' Guild or a market stall for what
	// the mule holds (a user request: mule first); MuleWantAt, the ask to the mule itself, is not cut.
	// nil: no mule, or no fresh reading of its bank, and every want is as it always was.
	MuleHolds func(rune string) bool `json:"-"`
}

// DefaultStage3Settings are the defaults, each justified in its field's comment above.
func DefaultStage3Settings() Stage3Settings {
	return Stage3Settings{
		Spell:            DefaultStage3Spell,
		CastsTarget:      ProdStage3CastsTarget, // was 500: budgets.go
		CastsPerTrip:     1000,
		MinTripRunes:     50,
		CoinReserve:      2000,
		Staff:            "",
		MaxPriceMultiple: 3,
		MagicTarget:      DefaultStage3MagicTarget,
		TrainSpell:       DefaultTrainSpell,
		TrainCasts:       DefaultTrainCasts,
		FoodKeep:         8,
		EatBelowHP:       6,
		Supply:           DefaultSupplySettings(),
	}
}

// Normalised clamps nonsense back to something the stage can act on.
func (s Stage3Settings) Normalised() Stage3Settings {
	d := DefaultStage3Settings()
	if _, ok := SpellByName(s.Spell); !ok {
		s.Spell = d.Spell
	}
	if s.CastsTarget <= 0 {
		s.CastsTarget = d.CastsTarget
	}
	if s.CastsPerTrip <= 0 {
		s.CastsPerTrip = d.CastsPerTrip
	}
	if s.MinTripRunes < 1 {
		s.MinTripRunes = 1
	}
	if s.CoinReserve < 0 {
		s.CoinReserve = 0
	}
	if s.MagicTarget < 1 || s.MagicTarget > 99 {
		s.MagicTarget = d.MagicTarget
	}
	if sp, ok := SpellByName(s.TrainSpell); !ok || sp.UsesFire() {
		s.TrainSpell = d.TrainSpell // never a Fire spell for training (UsesFire)
	}
	if s.TrainCasts <= 0 {
		s.TrainCasts = d.TrainCasts
	}
	if s.MaxPriceMultiple < 0 {
		s.MaxPriceMultiple = 0
	}
	if s.FoodKeep < 0 {
		s.FoodKeep = 0
	}
	if s.FoodKeep > 20 {
		s.FoodKeep = 20
	}
	if s.EatBelowHP < 0 {
		s.EatBelowHP = 0
	}
	return s
}

// TargetSpell is the settings' spell.
func (s Stage3Settings) TargetSpell() Spell {
	sp, ok := SpellByName(s.Normalised().Spell)
	if !ok {
		sp, _ = SpellByName(DefaultStage3Spell)
	}
	return sp
}

// StaffFree is what the configured staff makes free.
func (s Stage3Settings) StaffFree() Provided { return StaffProvides(s.Staff) }

// MulesHold is MuleHolds, false when the bot set none.
func (s Stage3Settings) MulesHold(rune string) bool { return s.MuleHolds != nil && s.MuleHolds(rune) }

// IsRuneName reports whether an item name is a rune, for reading counts off a pack or a bank.
func IsRuneName(name string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(name)), " rune")
}
