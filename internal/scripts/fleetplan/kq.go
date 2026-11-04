package fleetplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

const (
	TunnelEntranceName = "Tunnel entrance"
	KalphiteQueenName  = "Kalphite Queen"
	ShantayPassItem    = "Shantay pass"
	RopeItem           = "Rope"
	// ShantayPassCost is the obj's own cost (shantayshop sells at cost with 500 in stock).
	ShantayPassCost = 5
	// TollGateCost is the Al Kharid toll (pass_toll_gate), paid walking in from Lumbridge.
	TollGateCost = 10
	// ShipFares is Port Sarim->Karamja plus Brimhaven->Ardougne, the east-west crossing.
	ShipFares = 60
	// RopeCost is the obj's own cost; Aemad's price walks up from it as his stock falls.
	RopeCost = 18
	// KQRopeLifeTicks is ^lootdrop_duration: an attached rope is shared and lasts 200 ticks.
	KQRopeLifeTicks = 200
	// KQQueenHP is kalphite.npc's hitpoints for both forms.
	KQQueenHP = 255
	// ShantayGateLocID is shantay_pass_henge_doorway; ShantayPosterChoice its warning answer.
	ShantayGateLocID    = 4031
	ShantayPosterChoice = "Yeah, that poster doesn't scare me!"
	// WaterskinItem is shantayshop's water_skin4 (100 in stock, cost 30). desert_heat.rs2 runs a
	// 150-tick timer on an account with no desert clothes and, when it fires with no water, deals
	// 1-10 damage; with a waterskin it drinks a portion instead. Measured live without one: a
	// 14-HP account reached the lair mouth on 3 HP and died to the first Kalphite Worker.
	WaterskinItem = "Waterskin(4)"
	WaterskinCost = 30
)

// IsFilledWaterskin reports whether an item name is a waterskin with water left in it.
func IsFilledWaterskin(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(n, "waterskin(") && n != "waterskin(0)"
}

// KQSettings are Stage 6's knobs. JSON keys match internal/bot's setting names.
type KQSettings struct {
	// Spell is what the account autocasts at her; "" means Stage 3's spell.
	Spell string `json:"kqSpell"`
	// CastsPerLife is the runes carried into one entry, in casts. What is carried is lost on
	// death, so this is the loss per life as much as the ammunition.
	CastsPerLife int `json:"kqCastsPerLife"`
	// Food is the MINIMUM number of food items carried per entry; FoodHeal is what the load is
	// really sized by, because five cakes and five sharks are not the same kit (cake heals 4,
	// shark 20 — consume_normal.dbrow). EatBelowHP is the emergency eat point (full-heal rule
	// otherwise, see ChooseFood).
	//
	// Live-found 2026-09-20 (run e2e6): every kit that run drew read "0 food", because Food was an
	// item count against a bank that had none and nothing anywhere said how much healing a life
	// actually needs. Her own numbers say: both her magic and her ranged attack deal randominc(31)
	// (kalphite_queen.rs2), ignoring the account's Defence, its armour and its Magic entirely —
	// only a protection prayer zeroes them — so healing is the ONLY thing a staff-and-runes
	// account can put between itself and her.
	Food       int `json:"kqFood"`
	FoodHeal   int `json:"kqFoodHeal"`
	EatBelowHP int `json:"kqEatBelowHp"`
	// Standoff is the npc_range the account keeps from her FOOTPRINT; closer than this it steps
	// away. It is RecommendedKQStandoff (3) and it used to be 8, which was the single reason the
	// route landed about two casts a life.
	//
	// MEASURED, not reasoned (cmd/scarabstage3test -phases kqdrain, Magic 59 Fire Blast, gated on
	// a holder being alive): at npc_range 8 an account sent 81 casts and had ZERO accepted; at 3
	// it had 24 to 36 accepted; at 1, 41. A cast from 8 is simply never delivered, because the
	// engine runs the interaction only from a tile that passes its own range test and will not
	// walk the account there under clientRoutefinder. The route was standing exactly where it
	// could not act, and its own log said so once the range instrumentation existed: "she came
	// within 8 of this account's footprint gap", two casts, nothing landed.
	//
	// Three is still outside melee: KQStyleOddsFor only adds the third face at npc_range <= 1.
	Standoff int `json:"kqStandoff"`
	// Spacing is how far to keep from whoever she is currently ATTACKING, which is where both
	// her huntalls are centred (kqspacing.go). scarabplan.DefaultKQSpacing is the content-derived
	// number for an account with no protection prayer.
	//
	// IT DEFAULTS TO 0, WHICH IS OFF, because its one live A/B did not support it. Matched arms
	// in one window against one Queen, eight accounts at 99 hitpoints: the spaced four landed
	// 4.8 casts each and all four died, the unspaced four landed 5.5 and all four died, and the
	// rule fired only SIX times in the whole window. Either it is not firing often enough to
	// matter or it does not help from that geometry, and neither is root-caused — the cohort was
	// teleported onto ONE tile, so every account had to walk eleven tiles to clear a hunt centred
	// on a neighbour, which costs casts. The code stays because the content argument is sound and
	// a cohort that ARRIVES spread out is a different experiment; the default stays off until
	// somebody runs it.
	Spacing int `json:"kqSpacing"`
	// Ropes carried per entry (two gates); RopeStock the bank level a restock tops up to.
	Ropes     int `json:"kqRopes"`
	RopeStock int `json:"kqRopeStock"`
	// SpareCoins ride along above the pass cost, for a second pass or the toll after a detour.
	SpareCoins int `json:"kqSpareCoins"`
	// MaxLives stops the stage after this many entries (0: never). A lab run sets it.
	MaxLives int `json:"kqMaxLives"`
	// Waterskins carried across the desert (bought at Shantay's with the pass).
	Waterskins int `json:"kqWaterskins"`
	// Antipoison is the antipoison potions a life carries (AntipoisonForms, fewest doses first); 0 carries none.
	Antipoison int `json:"kqAntipoison"`
	// Prayer is the protection prayer to hold for the whole fight, or "" to hold none. It
	// defaults to KQPrayerName (Protect from Magic, Prayer 37) because that is clause 1 of
	// PlanZeroDamage and the standing recommendation of every closed Scarab claim — and until
	// 2026-09-21 NOTHING IN THIS SCRIPT IMPLEMENTED IT, so the recommendation had never once been
	// carried out by an account. See kqprayer.go.
	//
	// An account below the level is NOT an error and does not stop the stage: DecideKQPrayer
	// returns Blocked, the runner says so once, and the account fights unprotected exactly as it
	// did before this field existed. prayerprep.go prices what getting a cohort to the level
	// really costs, and that price is why this is a setting rather than a hard requirement.
	Prayer string `json:"kqPrayer"`
	// Loot switches the ground-loot claim protocol on (lootclaim.go, KQLootWorths). It is ON by
	// default because at cohort scale the thing it prevents — every account in the room walking at
	// the same pile — is the failure, and because it can only ever run when the account is not
	// fighting. LootRadius bounds how far a pile may be from the account before it is ignored:
	// her chamber is about 50 tiles across, and a claim that cannot be walked in
	// scarabrunner.LootWalkBudget is a wasted trip the protocol exists to stop.
	Loot       bool `json:"kqLoot"`
	LootRadius int  `json:"kqLootRadius"`
	// SpawnWindow is what the account does with the ~100 ticks after she dies and before she is
	// back (kqspawnwindow.go). OFF by default and deliberately so: the plan has never been run
	// against a live cohort, and it is the kind of feature that costs health to find out.
	//
	// It stayed unwired far longer than that justifies, though — the plan layer existed and
	// tested, and the registered bot simply never called it, so every account stood still through
	// every respawn. The wiring is now there and the switch is the honest part.
	// The tag is "-" on purpose. Every other tag in this struct names ONE of internal/bot's
	// settings keys, and this field is a GROUP of five of them — the switch itself is the bool
	// "kqSpawnWindow", and the rest are "kqSpawnWindowHpFloorPercent", "kqSpawnWindowRange",
	// "kqSpawnWindowAbandonRatio" and "kqSpawnWindowPreferCacti", carried on
	// SpawnWindowSettings' own fields. Tagging the group with the switch's name would say that
	// "kqSpawnWindow" takes an object, and it takes a bool.
	SpawnWindow SpawnWindowSettings `json:"-"`
	// ReturnRoute is the owner's route home after an emergency teleport (2 Nov 2026: "teleport to lumbridge and then to
	// camelot to bank ... use a ring of duelling to teleport to duel arena to run to shantey pass"): Camelot Teleport from the
	// Lumbridge landing, the kit at the Seers bank with a Ring of dueling drawn, then the ring to the Duel Arena (scarabplan/
	// kqreturn.go). Default true; every step falls back to the walk the stage always made. ReturnLaws is the Law runes the kit
	// carries for the two casts (the escape and Camelot), 0 carries none.
	ReturnRoute bool `json:"kqReturnRoute"`
	ReturnLaws  int  `json:"kqReturnLaws"`
	// EscapePrayerPoints is the prayer points left at which the escape fires (kqEscapePrayerPoints, 3 Nov 2026,
	// docs/kq_deathless_2026-11-03.md): 101 of 139 chamber deaths in six waves came after Protect from Magic ran dry, so the
	// account leaves with a few points still on the bar instead of at zero. 0 keeps the old rule, an empty bar only.
	EscapePrayerPoints int `json:"kqEscapePrayerPoints"`
	// EscapeCamelot lets the escape cast Camelot Teleport (5 air, 1 law) when Lumbridge Teleport is refused: 162 of the 195
	// refused escapes lacked the one Earth rune (kqEscapeCamelot).
	EscapeCamelot bool `json:"kqEscapeCamelot"`
	// ReturnPray walks an escaped account to the nearest church altar and prays before the rest of the return route (kqReturnPray):
	// an escape leaves a spent bar and only a death or an altar fills it, and 21 of the 42 lone entries on 3 Nov were escapees
	// going back in with an empty bar.
	ReturnPray bool `json:"kqReturnPray"`
	// PrayerOffIdle switches the protection prayer off while she is down or out of sight (the loot phase, a spent kit): it drains one
	// point per five ticks whether or not she hits anyone, and on 3 Nov 2026 the ~60 s respawn after each kill spent about 20 of an
	// account's ~38 points before it met the next Queen (kqPrayerOffIdle). holdPrayer puts it back on when she is in range.
	PrayerOffIdle bool `json:"kqPrayerOffIdle"`
	// PrayerFlick flicks the protection prayer at her instead of holding it (kqPrayerFlick, a user request: "prayer flicking for
	// kq is a must"): every active prayer OFF and ON in one tick at least every five ticks, so the drain timer never fires and the
	// prayer is up on every tick she checks it (scarabplan/kqflick.go). Any doubt about the timing falls back to the plain hold for the
	// rest of the life. The three thresholds are kqFlickMaxLateTicks (1 to 4), kqFlickHitWindowTicks (0 to 3) and kqFlickTickGapMs
	// (600 to 10000); out of range is the default.
	PrayerFlick         bool `json:"kqPrayerFlick"`
	FlickMaxLateTicks   int  `json:"kqFlickMaxLateTicks"`
	FlickHitWindowTicks int  `json:"kqFlickHitWindowTicks"`
	FlickTickGapMs      int  `json:"kqFlickTickGapMs"`
}

// The prayer flick's defaults (scarabplan.DefaultKQFlick*; kept in step by a test there).
const (
	DefaultFlickMaxLateTicks   = 3
	DefaultFlickHitWindowTicks = 2
	DefaultFlickTickGapMs      = 1800
)

// DefaultEscapePrayerPoints is kqEscapePrayerPoints' default: six points, 18 seconds of Protect from Magic (scarabplan.
// DefaultEscapePrayerPoints; kept in step by a test).
const DefaultEscapePrayerPoints = 6

// DefaultReturnLaws is the Law runes a kit carries for the return route: Lumbridge Teleport (the escape) and Camelot Teleport
// (the way back) cost one each (skill_magic/configs/magic_spells.dbrow).
const DefaultReturnLaws = 2

// DefaultKQSettings are the defaults.
func DefaultKQSettings() KQSettings {
	// CastsPerLife and RopeStock are the production budgets (budgets.go): they were 30 and 10.
	return KQSettings{CastsPerLife: ProdKQCastsPerLife, Food: 5, FoodHeal: 100, EatBelowHP: 6, Standoff: RecommendedKQStandoff, Ropes: 2, RopeStock: ProdKQRopeStock,
		SpareCoins: 10, Waterskins: 1, Antipoison: 2, Loot: true, LootRadius: 12, Spacing: 0, Prayer: KQPrayerName,
		SpawnWindow: DefaultSpawnWindowSettings(), ReturnRoute: true, ReturnLaws: DefaultReturnLaws,
		EscapePrayerPoints: DefaultEscapePrayerPoints, EscapeCamelot: true, ReturnPray: true, PrayerOffIdle: true,
		PrayerFlick: true, FlickMaxLateTicks: DefaultFlickMaxLateTicks, FlickHitWindowTicks: DefaultFlickHitWindowTicks,
		FlickTickGapMs: DefaultFlickTickGapMs}
}

// Normalised clamps nonsense to something the stage can act on.
func (s KQSettings) Normalised() KQSettings {
	d := DefaultKQSettings()
	if s.CastsPerLife <= 0 {
		s.CastsPerLife = d.CastsPerLife
	}
	if s.Food < 0 {
		s.Food = 0
	}
	if s.FoodHeal < 0 {
		s.FoodHeal = 0
	}
	if s.EatBelowHP < 0 {
		s.EatBelowHP = 0
	}
	if s.Standoff < 2 {
		s.Standoff = d.Standoff
	}
	if s.Standoff > 10 {
		s.Standoff = 10 // standard-spell cast range
	}
	if s.Ropes < 0 || s.Ropes > 5 {
		s.Ropes = d.Ropes
	}
	if s.RopeStock < s.Ropes {
		s.RopeStock = s.Ropes
	}
	if s.SpareCoins < 0 {
		s.SpareCoins = 0
	}
	if s.MaxLives < 0 {
		s.MaxLives = 0
	}
	if s.Waterskins < 0 || s.Waterskins > 4 {
		s.Waterskins = d.Waterskins
	}
	if s.Antipoison < 0 || s.Antipoison > 6 {
		s.Antipoison = d.Antipoison
	}
	if s.Spacing < 0 || s.Spacing > 20 {
		s.Spacing = d.Spacing
	}
	if s.LootRadius <= 0 || s.LootRadius > 30 {
		s.LootRadius = d.LootRadius
	}
	if s.ReturnLaws < 0 || s.ReturnLaws > 10 {
		s.ReturnLaws = d.ReturnLaws
	}
	if s.EscapePrayerPoints < 0 || s.EscapePrayerPoints > 20 {
		s.EscapePrayerPoints = d.EscapePrayerPoints
	}
	if s.FlickMaxLateTicks < 1 || s.FlickMaxLateTicks > 4 {
		s.FlickMaxLateTicks = d.FlickMaxLateTicks
	}
	if s.FlickHitWindowTicks < 0 || s.FlickHitWindowTicks > 3 {
		s.FlickHitWindowTicks = d.FlickHitWindowTicks
	}
	if s.FlickTickGapMs < 600 || s.FlickTickGapMs > 10000 {
		s.FlickTickGapMs = d.FlickTickGapMs
	}
	return s
}

// SpellFor is the spell a life casts: the setting, else Stage 3's target spell.
func (s KQSettings) SpellFor(st Stage3Settings) Spell {
	if sp, ok := SpellByName(s.Spell); ok {
		return sp
	}
	return st.TargetSpell()
}

// KQRegion is where the account is, as Stage 6 cares about it.
type KQRegion string

const (
	KQRegionUnknown KQRegion = "unknown"
	// KQRegionChamber is the Queen's room (level 0 of the lair square).
	KQRegionChamber KQRegion = "chamber"
	// KQRegionBurrow is the upper tunnels (level 2 of the lair square).
	KQRegionBurrow KQRegion = "burrow"
	// KQRegionDesert is south of the Shantay pass, on the surface.
	KQRegionDesert KQRegion = "desert"
	// KQRegionAlKharid is east of the toll gate and north of the pass: bank and pass reachable
	// without paying the toll.
	KQRegionAlKharid KQRegion = "al-kharid"
	// KQRegionWest is the Ardougne side of the map (Stage 3's home), a restock's destination.
	KQRegionWest KQRegion = "west"
	// KQRegionElsewhere is anywhere else — Lumbridge after a death, above all.
	KQRegionElsewhere KQRegion = "elsewhere"
)

// InKQLair reports whether t is in the lair map square (54,148 and its neighbours).
func InKQLair(t tile.Tile) bool {
	return t.X >= 3456 && t.X < 3520 && t.Z >= 9472 && t.Z < 9536
}

// WestOfTheMountainX is the x below which a mainland tile is on the Catherby side of White Wolf
// Mountain (its pass is x 2828-2878). webwalk's automatic "white-wolf-dens" zone applies at EVERY
// combat level and there is no wolf-free land route across, so from anywhere west of it the only
// way to Al Kharid is the ship chain: Brimhaven -> Ardougne's pier in reverse, then Karamja ->
// Port Sarim, and the toll gate.
const WestOfTheMountainX = 2850

// WestOfTheMountain reports a surface tile on the mainland west of White Wolf Mountain: East
// Ardougne, and also Port Khazard, Yanille, Catherby, Seers' Village, the Fishing Guild and the
// rest, which the Ardougne reach (RegionOf) does not cover. Every one of them is two ship fares
// and the toll from Shantay, and the stage has to price its coins that way.
//
// Karamja is excluded, and so is the sea south-east of Ardougne (x >= 2740, z <= 3260 holds no
// mainland), because it is an island one ship from Port Sarim, not two; the underground (z >= 6400)
// and the Mage Bank (z 4715) by the z bound.
//
// Live-found 2026-09-23 (lab e2e scl18): an account restocked in East Ardougne, spent its bank to
// 17 coins on runes, and its kit was called ready because 17 buys a 5-coin pass. The walk to
// Shantay then needs the two ships, the webwalk answered "unreachable from here (best 649 tiles)"
// and the stage went round Phase6Pass 216 times over more than an hour without ever changing
// anything. KitReady now asks for the whole walk (CoinsNeeded), and a tile here is priced as West.
func WestOfTheMountain(t tile.Tile) bool {
	if t.X >= WestOfTheMountainX || t.Z >= 4000 {
		return false
	}
	return !(t.X >= 2740 && t.Z <= 3260)
}

// KitCoins is the coins an entry carries from a bank in region r: the pass and spare, plus the
// toll when the walk starts west of the gate, plus both ship fares when it starts west.
func KitCoins(r KQRegion, st KQSettings) int {
	st = st.Normalised()
	n := ShantayPassCost + st.SpareCoins + st.Waterskins*(WaterskinCost+5)
	switch r {
	case KQRegionAlKharid, KQRegionDesert:
	case KQRegionWest:
		n += ShipFares + TollGateCost
	default:
		n += TollGateCost
	}
	return n
}
