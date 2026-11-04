package runerunnerplan

// RuneRunner's settings: the struct a strict decode fills, its defaults, and Validate, which
// turns every value a shop run cannot honour into an error before the account moves. The bot's
// settings.Schema (internal/bot/runerunner.go) declares the same keys for the panel, and a test
// there keeps the two identical.

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/marketstockplan"
	"rsb0t-go/internal/scripts/tile"
)

// Defaults, one per setting.
const (
	DefaultMule = "mule"
	// DefaultTripCoins bounds one coin draw. It was 100,000, and LIVE on 7 October 2026 (spare104,
	// acct152, acct73) that was the wall: a circuit that costs 1.1 million at the caps drew 100,000,
	// and the reserve for the dearer runes at the stations ahead (1.06 million) then left nothing to
	// spend at Aubury ("bought=0" on every rune). The draw is still min(this, the bank's spare, the
	// circuit's estimated cost), so a runner whose bank is short draws what it has and asks the mule.
	DefaultTripCoins            = 1500000
	DefaultCoinReserve          = 10000
	DefaultFareCoins            = 300
	DefaultMinVisitRunes        = 50
	DefaultHopMinGain           = 200
	DefaultMaxHopsPerRound      = 2
	DefaultLundailMaxDeaths     = 2
	DefaultDeliverMinRunes      = 300
	DefaultDeliverMaxAgeMinutes = 180
	DefaultMuleWaitMinutes      = 10
	DefaultIdleMaxMinutes       = 20
	DefaultTickMs               = 600
	DefaultCheckpointDir        = "data/runerunner-checkpoints"
	DefaultLogDetail            = "Normal"
	DefaultZaffStaffs           = 0
	// DefaultZaffStaffCap is fleetplan.ZaffStaffOfAirCost: both of a full shelf's staffs (1500
	// and 1530) and nothing dearer.
	DefaultZaffStaffCap = fleetplan.ZaffStaffOfAirCost
	// DefaultZaffBattlestaffs is off; DefaultZaffBattlestaffCap takes a full shelf of five (7000 to 7560).
	DefaultZaffBattlestaffs   = 0
	DefaultZaffBattlestaffCap = 7560
	// DefaultZaffFireStaffs is off; DefaultZaffFireStaffCap is the Staff of air's (1500 and 1530 on a full shelf).
	DefaultZaffFireStaffs   = 0
	DefaultZaffFireStaffCap = fleetplan.ZaffStaffOfAirCost
	// DefaultAmmoEveryWorld visits Hickton on every world of the round (a user request).
	DefaultAmmoEveryWorld = true
	// DefaultAirCap is the air rune cap: near the base-stock price, as the owner's 26 September
	// directive asks. Air costs 4 at base; under shop.rs2's walk the price reaches 5 at 25 below
	// base, so a cap of 5 buys the top 50 of Aubury's 2000 and of Betty's 1000, and the top 17 of
	// Lundail's 200 (her delta is 30). The ceiling is 24 (six times 4). A higher cap buys more of
	// the shelf per visit: see the handoff doc's table before raising it.
	DefaultAirCap = 5
	// DefaultShortageFirst is the shortageFirst setting's default: on (see shortage.go).
	DefaultShortageFirst = true
	// DefaultBuyAll is the buyAll setting's default: on (see BuyAllCaps).
	DefaultBuyAll = true
	// DefaultRotate is the rotate setting's default: rotate unless the shops setting pins one shop
	// (RotateAuto), so the live one-shop runners keep their classic ring until an operator says on.
	DefaultRotate = RotateAuto
	// DefaultRotationPhase is the rotationPhase setting's default: -1 takes the phase from the
	// account name's hash (RotationPhaseFor).
	DefaultRotationPhase = -1
	// DefaultMaxHopsPerHour bounds a rotating runner's hops in any hour (HopBudget). It is the ceiling: the two
	// minutes between hops (RotationHopGap) already hold a runner to 30 an hour, and the old 12 left the runner
	// waiting on the budget mid-circuit (a user request: "12 hops in the last hour maxhops perhour shouldnt be a
	// thing").
	DefaultMaxHopsPerHour = MaxMaxHopsPerHour
	// MaxMaxHopsPerHour is the setting's ceiling.
	MaxMaxHopsPerHour = 30
	// MaxRotationPhase is the rotationPhase setting's ceiling (the phase is taken modulo the
	// circuit's own count, twice the worlds).
	MaxRotationPhase = 99
	// MaxRotationWorlds is how many worlds a rotation may list.
	MaxRotationWorlds = 6
	// DefaultMarketBuy is the marketBuy setting's default: on. The market station costs one cached
	// page read per check while no stall lists a worthwhile lot (marketstockplan).
	DefaultMarketBuy = true

	// DefaultAmmoBuy is the ammoBuy setting's default: off. The ammo stations are switched on per account
	// through the control panel's settings once the first visits have been watched (ammo.go).
	DefaultAmmoBuy = false
	// DefaultAmmoBuyAll is the ammoBuyAll setting's default: on, the owner's "buying all" (13 Oct 2026).
	DefaultAmmoBuyAll = true
	// DefaultAmmoCoins bounds one round's ammo spending: the steady-state bill of Bolts, Steel arrowtips and
	// Feathers is about 65,000 a round, Hickton's Rune arrowtips alone 29,900 a visit.
	DefaultAmmoCoins = 150000
	// MaxAmmoCoins is the ammoCoins ceiling.
	MaxAmmoCoins = 10000000
	// DefaultAmmoMinVisitCoins is the smallest planned purchase (in coins) an ammo stop is walked to for.
	DefaultAmmoMinVisitCoins = 3000
	// MaxAmmoMinVisitCoins is the ammoMinVisitCoins ceiling.
	MaxAmmoMinVisitCoins = 1000000
	// DefaultAmmoSlotReserve is the pack slots an ammo stop leaves free for the rune stacks still to be bought.
	DefaultAmmoSlotReserve = 1
	// MaxAmmoSlotReserve is the ammoSlotReserve ceiling.
	MaxAmmoSlotReserve = 10

	// DefaultTrawlerRope is the trawlerRope setting's default: on (a user request: the Fishing Trawler's net froze for want of
	// Rope and the mule held 37). With ammoBuy on it places Port Khazard, Aemad and Ned and buys Rope at them (ammo.go, RopeSupply).
	DefaultTrawlerRope = true
	// DefaultRopeFloor is the ropeFloor setting's default: the Rope the mule's stock is kept at by Ned (a block needs 150-200).
	DefaultRopeFloor = 250
	// MaxRopeFloor is the ropeFloor ceiling.
	MaxRopeFloor = 5000
	// DefaultKhazardRopeOnly is the khazardRopeOnly setting's default: on, the crew's paste and buckets stay on the Port Khazard shelf.
	DefaultKhazardRopeOnly = true
)

// DefaultDeliverNoted is the deliverNoted setting's default: the unstackable ammo parts the delivery draws as bank notes. A pack
// takes 24 single Ropes, so a delivery of loose ones moved one pack a round whatever the bank held (the mule held 37 on 4 Nov 2026).
func DefaultDeliverNoted() []string { return []string{AmmoRope} }

// The rotate setting's values.
const (
	// RotateAuto rotates unless the shops setting names exactly one shop.
	RotateAuto = "auto"
	// RotateOn always rotates.
	RotateOn = "on"
	// RotateOff always runs the classic ring (the mule's world plus one other).
	RotateOff = "off"
)

// RotateOptions are the rotate setting's values, in the panel's order.
func RotateOptions() []string { return []string{RotateAuto, RotateOn, RotateOff} }

// DefaultRunes is the classic ring's rune list when the runes setting is empty, in priority order: minds first (Wind Strike to Magic 17 and the 40 new
// accounts' bottleneck), then air, then chaos (Wind Bolt to 41). Air was left out while the
// cohort's Staff of air paid it; on 2026-09-26 the mule held no air runes, the Seers market had
// none, and most new accounts hold a plain Magic staff (only 4 Staffs of air were on the market),
// so they pay 1 air a Wind Strike and 2 a Wind Bolt. Death runes can be added for Wind Blast.
func DefaultRunes() []string { return []string{"Mind rune", "Air rune", "Chaos rune"} }

// DefaultPriceCaps are the classic ring's price ceilings when runes and priceCaps are both empty:
// 18 is a mind rune's ceiling at Aubury and Betty (six
// times its cost of 3), so a round buys a shelf all the way down to about 200 and nothing the
// shelf regrows between visits is lost to a full shelf; air is capped near its base price
// (DefaultAirCap); 90 is chaos's ceiling (the owner's "buy all"). A chaos cap of 45 gets the same
// hourly chaos flow at about half the price but never buys the one-time pile: see the handoff doc.
func DefaultPriceCaps() []string {
	return []string{"Mind rune=18", "Air rune=" + strconv.Itoa(DefaultAirCap), "Chaos rune=90"}
}

// PinWorldError is the answer to a pinWorld setting: the ring hops worlds by design.
const PinWorldError = "RuneRunner hops worlds; remove pinWorld"

// WorldList is the worlds setting. It decodes a JSON list of numbers, a list of numeric
// strings (the panel), or one comma-separated string.
type WorldList []int

// UnmarshalJSON implements json.Unmarshaler.
func (w *WorldList) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		return fmt.Errorf("worlds: null is not a list of worlds")
	}
	var nums []int
	if err := json.Unmarshal(b, &nums); err == nil {
		*w = nums
		return nil
	}
	var strs []string
	if err := json.Unmarshal(b, &strs); err != nil {
		var one string
		if err2 := json.Unmarshal(b, &one); err2 != nil {
			return fmt.Errorf("worlds: want a list of world numbers, got %s", s)
		}
		strs = strings.Split(one, ",")
	}
	out := WorldList{}
	for _, x := range strs {
		x = strings.TrimSpace(x)
		if x == "" {
			continue
		}
		n, err := strconv.Atoi(x)
		if err != nil {
			return fmt.Errorf("worlds: %q is not a world number", x)
		}
		out = append(out, n)
	}
	*w = out
	return nil
}

// Settings is RuneRunner's settings object. Every key is lowerCamel, as fleet.config.json
// writes them; the json tags ARE the setting keys.
type Settings struct {
	// Mule is the MuleReceiver the runes are traded to. It must list this account in its
	// allowedPartners.
	Mule string `json:"mule"`
	// MuleStand is "x,z" of the mule's post; "" uses its heartbeat's post, else 2809,3441.
	MuleStand string `json:"muleStand"`
	// Shops are the sites to buy at: Aubury, Betty, Lundail.
	Shops []string `json:"shops"`
	// Worlds are the worlds to buy on. The mule's world is always one; the first listed world
	// that is not the mule's is the other. Empty: the mule's world and the fleet's other one.
	Worlds WorldList `json:"worlds"`
	// Runes are bought in this priority order. Empty: every rune the enabled shops stock when
	// rotating (FullStockRunes), DefaultRunes on the classic ring.
	Runes []string `json:"runes"`
	// PriceCaps are "Rune=gp", exactly one per listed rune. Empty: DerivedCap for every rune
	// (DefaultPriceCaps for the classic ring's own default runes).
	PriceCaps []string `json:"priceCaps"`
	// TripCoins bounds each coin draw: one per east leg and one per Lundail crossing.
	TripCoins int `json:"tripCoins"`
	// CoinReserve stays in the bank.
	CoinReserve int `json:"coinReserve"`
	// FareCoins ride in the pack for the boat fares and are never spent in a shop.
	FareCoins int `json:"fareCoins"`
	// MinVisitRunes is the smallest purchase a station is walked to for.
	MinVisitRunes int `json:"minVisitRunes"`
	// HopMinGain is the smallest expected purchase on the other world worth its two hops.
	HopMinGain int `json:"hopMinGain"`
	// MaxHopsPerRound is 2 (the other world) or 0 (a one-world buyer).
	MaxHopsPerRound int `json:"maxHopsPerRound"`
	// LundailMaxDeaths is how many Lundail deaths in a rolling 24 hours stop Lundail visits.
	LundailMaxDeaths int `json:"lundailMaxDeaths"`
	// DeliverMinRunes is the smallest delivery worth a trade.
	DeliverMinRunes int `json:"deliverMinRunes"`
	// DeliverMaxAgeMinutes delivers whatever is held once the last delivery is this old.
	DeliverMaxAgeMinutes int `json:"deliverMaxAgeMinutes"`
	// MuleWaitMinutes is how long a deferred delivery waits at the post.
	MuleWaitMinutes int `json:"muleWaitMinutes"`
	// IdleMaxMinutes bounds a wait when nothing in the ring is worth a visit.
	IdleMaxMinutes int `json:"idleMaxMinutes"`
	// TickMs is the engine tick, for restock estimates (lab engines run 200).
	TickMs int `json:"tickMs"`
	// CheckpointDir holds <account>/RuneRunnerState.json.
	CheckpointDir string `json:"checkpointDir"`
	// StopAfterRounds finishes the script after this many rounds (0 = never; for the lab).
	StopAfterRounds int `json:"stopAfterRounds"`
	// LogDetail is Normal or Verbose.
	LogDetail string `json:"logDetail"`
	// ZaffStaffs is how many Staffs of air to buy from Zaff at each Varrock visit (0 = never).
	// His shelf holds two, one back every 1000 ticks. Bought staffs go to the mule with the runes.
	ZaffStaffs int `json:"zaffStaffs"`
	// ZaffStaffCap is the most one staff may cost.
	ZaffStaffCap int `json:"zaffStaffCap"`
	// ZaffBattlestaffs is how many Battlestaffs to buy from Zaff at each Varrock visit (0 = never), and
	// ZaffBattlestaffCap the most one may cost.
	ZaffBattlestaffs   int `json:"zaffBattlestaffs"`
	ZaffBattlestaffCap int `json:"zaffBattlestaffCap"`
	// ZaffFireStaffs is how many Staffs of fire to buy from Zaff at each Varrock visit (0 = never), and ZaffFireStaffCap the most
	// one may cost. They go to the mule with the runes.
	ZaffFireStaffs   int `json:"zaffFireStaffs"`
	ZaffFireStaffCap int `json:"zaffFireStaffCap"`
	// TopUpBelow asks the mule for coins when bank plus pack falls under it (0 = never ask).
	TopUpBelow int `json:"topUpBelow"`
	// TopUpAmount is what one top-up asks the mule for (0 = never ask) when the mule's coin stack is not
	// published, and the least it asks then. With TopUpWholeStack (the default) and a published stack the ask is
	// the whole stack above MuleCoinFloor, not this. The mule's own coinsPerOrder, coinsPerWindow and
	// handoutPerDonor bound what it hands over.
	TopUpAmount int `json:"topUpAmount"`
	// TopUpWholeStack asks the mule for its ENTIRE published coin stack above MuleCoinFloor, in as many orders as
	// its coinsPerOrder needs, whenever bank plus pack is under the round's full bill (a user request:
	// "request mules entire cash stack and spend it on everything instead of just running out"). Off asks
	// TopUpAmount (the round's shortfall with buyAll), clamped to the mule's spare, as before.
	TopUpWholeStack bool `json:"topUpWholeStack"`
	// MuleCoinFloor is the coins a top-up leaves in the mule's bank for the cohort's small asks (a kit's
	// 1,000-coin line, a fisher's fare, a trainer's blade purse); see DefaultMuleCoinFloor. 0 takes every coin.
	MuleCoinFloor int `json:"muleCoinFloor"`
	// ShortageFirst buys the runes the mule is out of first and up to the shop's price ceiling
	// (shortage.go); off keeps runes and priceCaps exactly as written.
	ShortageFirst bool `json:"shortageFirst"`
	// BuyAll buys every rune a shop stocks up to the shop's own price ceiling (six times the
	// rune's cost) in place of the derived caps (BuyAllCaps); a priceCaps list written in the settings
	// still wins. a user request: a runner that bought 34
	// Nature and 61 Chaos at the Wizards' Guild and hopped with the rest of the shelf standing and
	// coins in its bank is leaving the fleet's runes behind. Off: the derived caps
	// (three times each rune's cost) and shortageFirst, as before.
	BuyAll bool `json:"buyAll"`
	// WalkAlch casts High Level Alchemy while walking, only while the mule can spare Nature runes
	// (AlchSpare) and the account carries a kit from it; WalkRub rubs a carried Games necklace or Ring
	// of dueling every third tick of a walk; TeleTravel starts a long walk with a teleport when the
	// pack can pay one (travel.go).
	WalkAlch   bool `json:"walkAlch"`
	WalkRub    bool `json:"walkRub"`
	TeleTravel bool `json:"teleTravel"`
	// Rotate is auto, on or off: on works the rotation circuit (rotation.go) over every listed
	// shop on every listed world; off the classic ring; auto rotates unless shops names one shop.
	Rotate string `json:"rotate"`
	// RotationPhase places this runner on the circuit: -1 from the account name's hash, else
	// 0..MaxRotationPhase (taken modulo twice the worlds). Give each runner its own to spread them.
	RotationPhase int `json:"rotationPhase"`
	// MaxHopsPerHour bounds a rotating runner's hops in any hour; hops are also two minutes apart.
	MaxHopsPerHour int `json:"maxHopsPerHour"`
	// HomeWorld is the world the control panel logs the account in on at a start (0: the
	// roster's; cmd/controlpanel reads it from the script's settings). The script itself does not
	// use it: the rotation hops where it needs to, the classic ring to the mule's world.
	HomeWorld int `json:"homeWorld"`
	// SwarmWhileWaiting runs ScarabSwarm while a rotating runner's shelves regrow (swarm.go).
	SwarmWhileWaiting bool `json:"swarmWhileWaiting"`
	// SwarmCheckMinutes is how often the swarm phase reads the restock estimate.
	SwarmCheckMinutes int `json:"swarmCheckMinutes"`
	// TrawlerStintMinutes is how long the account stays with master's trawler crew after joining it between two rounds
	// (0 = until the block ends); TrawlerRoundHoldMaxMinutes is the longest an unfinished round keeps it off the crew
	// (0 = no cap). trawler.go.
	TrawlerStintMinutes        int `json:"trawlerStintMinutes"`
	TrawlerRoundHoldMaxMinutes int `json:"trawlerRoundHoldMaxMinutes"`
	// SwarmResumeRunes is the circuit's estimated purchase at which the runner (re)starts it.
	SwarmResumeRunes int `json:"swarmResumeRunes"`
	// SwarmSettings is ScarabSwarm's own settings object, passed through to it unchanged (mule
	// defaults to this runner's mule).
	SwarmSettings map[string]interface{} `json:"swarmSettings"`
	// MarketBuy adds the player-market station (marketstockplan): after the draw, buy the runes
	// the mule is short of from the Seers stalls when one lists them under the price ceiling.
	MarketBuy bool `json:"marketBuy"`
	// MarketRunes are "Rune=count" stock goals for the mule, in priority order; the station buys
	// a rune only while the mule's published bank (plus what this runner already holds for it)
	// is under its goal. Empty: marketstockplan.DefaultTargets (Nature 20000, Death 6000, Fire
	// 10000, Chaos 8000). A rune not in the runes setting is not delivered, so it is not bought.
	MarketRunes []string `json:"marketRunes"`
	// MarketStalls are the stalls asked, in order. Empty: marketstockplan.DefaultStalls.
	MarketStalls []string `json:"marketStalls"`
	// MarketPricePercent is the price ceiling as a percentage of the trade book's median SELL price
	// (150 = up to 1.5 times), never above the rune's fixed hard ceiling.
	MarketPricePercent int `json:"marketPricePercent"`
	// MarketCoins is the most one market station may spend, drawn from this account's own bank.
	MarketCoins int `json:"marketCoins"`
	// MarketCheckMinutes is the least time between two looks at the market.
	MarketCheckMinutes int `json:"marketCheckMinutes"`
	// MarketMinLot is the smallest purchase worth the walk to Seers (a smaller shortfall is bought
	// whole).
	MarketMinLot int `json:"marketMinLot"`
	// MarketTradeCoins is the stall's per-trade coin limit; a lot over it is split into trades.
	MarketTradeCoins int `json:"marketTradeCoins"`

	// AmmoBuy adds the ammo stations to a rotation round (ammo.go): Hickton in Catherby at both ends of the
	// round, Gerrant in Port Sarim beside every Betty visit and Lowe in Varrock beside every Aubury visit buy
	// the arrowtips, bolts and feathers (ammoItems) and deliver them to the mule with the runes. Rotation only.
	AmmoBuy bool `json:"ammoBuy"`
	// AmmoShops are the ammo shops to use: Hickton, Lowe, Gerrant; Dargaud, Lathas, Gulluck and FishingGuild are
	// accepted and left out of the round with the reason. Empty: the three built ones.
	AmmoShops []string `json:"ammoShops"`
	// AmmoItems are the parts to buy, in priority order. Empty: DefaultAmmoItems.
	AmmoItems []string `json:"ammoItems"`
	// AmmoBuyAll buys each part down to the shop's own price ceiling (the owner's "buy all"); off, to three
	// times its cost. An ammoPriceCaps entry wins for its item.
	AmmoBuyAll bool `json:"ammoBuyAll"`
	// AmmoPriceCaps are "Item=gp" caps, 1-1000, for the items that need one of their own.
	AmmoPriceCaps []string `json:"ammoPriceCaps"`
	// AmmoStopAbove are "Item=count" gates: an item is not bought while the mule's bank (and what this runner
	// holds for it) reaches the count; 0 is no gate. Items not listed are not gated.
	AmmoStopAbove []string `json:"ammoStopAbove"`
	// AmmoCoins is the most one round spends on ammo (0 = unlimited).
	AmmoCoins int `json:"ammoCoins"`
	// AmmoMinVisitCoins is the smallest planned purchase, in coins, an ammo stop is walked to for.
	AmmoMinVisitCoins int `json:"ammoMinVisitCoins"`
	// AmmoEveryWorld visits Hickton on every world of the round, behind a hop of its own for each world the circuit does not end
	// on or start from; such a hop is made only when the shelf there is worth ammoMinVisitCoins. Off: the mule's world and the last
	// circuit world only.
	AmmoEveryWorld bool `json:"ammoEveryWorld"`
	// AmmoSlotReserve is the free pack slots an ammo stop leaves for the rune stacks still to be bought.
	AmmoSlotReserve int `json:"ammoSlotReserve"`

	// TrawlerRope adds Port Khazard, Aemad and (with RopeFloor above 0) Ned to the placed ammo shops and Rope to the ammo parts,
	// whatever ammoShops and ammoItems list, so the mule is stocked with the Rope the Fishing Trawler's net spends. Needs ammoBuy.
	TrawlerRope bool `json:"trawlerRope"`
	// RopeFloor is the Rope the mule's published bank is kept at by Ned (15 coins a conversation, no shelf to wait on): Ned buys the
	// part of the floor the mule and this runner do not hold, and nothing once it is met. 0 turns Ned off.
	RopeFloor int `json:"ropeFloor"`
	// KhazardRopeOnly buys only the Rope at Port Khazard, whatever ammoItems list: the Swamp paste and Bailing buckets on that
	// shelf are the trawler crew's own kit (paste comes back at five a minute).
	KhazardRopeOnly bool `json:"khazardRopeOnly"`
	// DeliverNoted are the unstackable ammo parts the delivery draws from the bank as notes, so any count crosses in one trade slot.
	DeliverNoted []string `json:"deliverNoted"`
}

// DefaultSettings is every setting at its default.
func DefaultSettings() Settings {
	return Settings{
		Mule:                 DefaultMule,
		Shops:                DefaultSites(),
		Worlds:               WorldList{},
		Runes:                []string{},
		PriceCaps:            []string{},
		TripCoins:            DefaultTripCoins,
		CoinReserve:          DefaultCoinReserve,
		FareCoins:            DefaultFareCoins,
		MinVisitRunes:        DefaultMinVisitRunes,
		HopMinGain:           DefaultHopMinGain,
		MaxHopsPerRound:      DefaultMaxHopsPerRound,
		LundailMaxDeaths:     DefaultLundailMaxDeaths,
		DeliverMinRunes:      DefaultDeliverMinRunes,
		DeliverMaxAgeMinutes: DefaultDeliverMaxAgeMinutes,
		MuleWaitMinutes:      DefaultMuleWaitMinutes,
		IdleMaxMinutes:       DefaultIdleMaxMinutes,
		TickMs:               DefaultTickMs,
		CheckpointDir:        DefaultCheckpointDir,
		LogDetail:            DefaultLogDetail,
		ZaffStaffs:           DefaultZaffStaffs,
		ZaffStaffCap:         DefaultZaffStaffCap,
		ZaffBattlestaffs:     DefaultZaffBattlestaffs,
		ZaffBattlestaffCap:   DefaultZaffBattlestaffCap,
		ZaffFireStaffs:       DefaultZaffFireStaffs,
		ZaffFireStaffCap:     DefaultZaffFireStaffCap,
		TopUpBelow:           DefaultTopUpBelow,
		TopUpAmount:          DefaultTopUpAmount,
		TopUpWholeStack:      DefaultTopUpWholeStack,
		MuleCoinFloor:        DefaultMuleCoinFloor,
		ShortageFirst:        DefaultShortageFirst,
		BuyAll:               DefaultBuyAll,
		WalkAlch:             DefaultWalkAlch,
		WalkRub:              DefaultWalkRub,
		TeleTravel:           DefaultTeleTravel,
		Rotate:               DefaultRotate,
		RotationPhase:        DefaultRotationPhase,
		MaxHopsPerHour:       DefaultMaxHopsPerHour,
		SwarmWhileWaiting:    DefaultSwarmWhileWaiting,
		SwarmCheckMinutes:    DefaultSwarmCheckMinutes,
		SwarmResumeRunes:     DefaultSwarmResumeRunes,
		SwarmSettings:        map[string]interface{}{},
		MarketBuy:            DefaultMarketBuy,
		MarketRunes:          []string{},
		MarketStalls:         []string{},
		MarketPricePercent:   marketstockplan.DefaultPricePercent,
		MarketCoins:          marketstockplan.DefaultCoins,
		MarketCheckMinutes:   marketstockplan.DefaultCheckMinutes,
		MarketMinLot:         marketstockplan.DefaultMinLot,
		MarketTradeCoins:     marketstockplan.DefaultTradeCoins,
		AmmoBuy:              DefaultAmmoBuy,
		AmmoShops:            DefaultAmmoShops(),
		AmmoItems:            DefaultAmmoItems(),
		AmmoBuyAll:           DefaultAmmoBuyAll,
		AmmoPriceCaps:        []string{},
		AmmoStopAbove:        DefaultAmmoStopAbove(),
		AmmoCoins:            DefaultAmmoCoins,
		AmmoMinVisitCoins:    DefaultAmmoMinVisitCoins,
		AmmoEveryWorld:       DefaultAmmoEveryWorld,
		AmmoSlotReserve:      DefaultAmmoSlotReserve,
		TrawlerRope:          DefaultTrawlerRope,
		RopeFloor:            DefaultRopeFloor,
		KhazardRopeOnly:      DefaultKhazardRopeOnly,
		DeliverNoted:         DefaultDeliverNoted(),

		// The trawler gate (trawler.go).
		TrawlerStintMinutes:        DefaultTrawlerStintMinutes,
		TrawlerRoundHoldMaxMinutes: DefaultTrawlerRoundHoldMaxMinutes,
	}
}

// SettingKeys are the setting keys, read off Settings' json tags, sorted.
func SettingKeys() []string {
	t := reflect.TypeOf(Settings{})
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// CheckRawKeys refuses a settings object carrying pinWorld (with its own message) or any key
// Settings does not have. The strict decode would refuse an unknown key too; this names them all
// at once and gives pinWorld the reason an operator needs.
func CheckRawKeys(raw map[string]interface{}) error {
	if _, ok := raw["pinWorld"]; ok {
		return fmt.Errorf("%s", PinWorldError)
	}
	known := map[string]bool{}
	for _, k := range SettingKeys() {
		known[k] = true
	}
	var unknown []string
	for k := range raw {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown RuneRunner setting(s) %s (known: %s)", strings.Join(unknown, ", "), strings.Join(SettingKeys(), ", "))
	}
	return nil
}

// intRange is one numeric setting's bounds; max 0 means unbounded above.
type intRange struct {
	key      string
	val      int
	min, max int
}

// Rotating reports whether these settings work the rotation circuit: rotate on, or auto with
// more than one shop (an unreadable shops setting counts as not rotating; Validate reports it).
func (s Settings) Rotating() bool {
	switch strings.ToLower(strings.TrimSpace(s.Rotate)) {
	case RotateOn:
		return true
	case RotateAuto, "":
		shops, err := s.ShopKeys()
		return err == nil && len(shops) > 1
	}
	return false
}

// Filled is s with its empty lists given their effective values: the runes (FullStockRunes when
// rotating, else DefaultRunes), the caps (DefaultPriceCaps for the classic ring's own default
// runes, else DerivedCaps over the runes), and a rotation's worlds (DefaultRotationWorlds).
// Validate and Resolve read the filled copy, so an empty list is never an error by itself.
func (s Settings) Filled() Settings {
	rot := s.Rotating()
	runesDefaulted := len(s.Runes) == 0
	if runesDefaulted {
		if shops, err := s.ShopKeys(); rot && err == nil {
			s.Runes = FullStockRunes(shops)
		} else {
			s.Runes = DefaultRunes()
		}
	}
	if len(s.PriceCaps) == 0 {
		if runesDefaulted && !rot {
			s.PriceCaps = DefaultPriceCaps()
		} else {
			s.PriceCaps = DerivedCaps(s.Runes)
		}
	}
	if rot && len(s.Worlds) == 0 {
		s.Worlds = DefaultRotationWorlds()
	}
	if len(s.AmmoShops) == 0 {
		s.AmmoShops = DefaultAmmoShops()
	}
	if len(s.AmmoItems) == 0 {
		s.AmmoItems = DefaultAmmoItems()
	}
	return s
}

// Validate reports every problem with s, one per line. A nil error means the ring can run.
func (s Settings) Validate() error {
	s = s.Filled()
	rot := s.Rotating()
	var problems []string
	add := func(format string, args ...interface{}) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if strings.TrimSpace(s.Mule) == "" {
		add("mule: must name the MuleReceiver the runes go to")
	}
	if _, _, err := ParseStand(s.MuleStand); err != nil {
		add("muleStand: %v", err)
	}
	shops, shopErr := s.ShopKeys()
	if shopErr != nil {
		add("shops: %v", shopErr)
	}
	seenWorld := map[int]bool{}
	for _, w := range s.Worlds {
		if w < 1 {
			add("worlds: %d is not a world (worlds start at 1)", w)
		}
		if seenWorld[w] {
			add("worlds: %d is listed twice", w)
		}
		seenWorld[w] = true
	}
	if !rot && len(s.Worlds) > 2 {
		add("worlds: %d worlds listed; the ring handles the mule's world plus one other (rotate on works more)", len(s.Worlds))
	}
	if rot && len(s.Worlds) > MaxRotationWorlds {
		add("worlds: %d worlds listed; a rotation works at most %d", len(s.Worlds), MaxRotationWorlds)
	}
	switch strings.ToLower(strings.TrimSpace(s.Rotate)) {
	case RotateAuto, RotateOn, RotateOff:
	default:
		add("rotate: %q; it is auto, on or off", s.Rotate)
	}
	if rot && s.MaxHopsPerRound == 0 && len(s.Worlds) > 1 {
		add("maxHopsPerRound: 0 with rotate and %d worlds; a rotation hops between its worlds (list one world to stay on it)", len(s.Worlds))
	}
	if len(shops) > 0 {
		if runes, err := s.RuneNames(shops); err != nil {
			add("runes: %v", err)
		} else if _, err := ParseCaps(s.PriceCaps, runes); err != nil {
			add("priceCaps: %v", err)
		}
	}
	for _, r := range []intRange{
		{"tripCoins", s.TripCoins, 1000, 10000000},
		{"coinReserve", s.CoinReserve, 0, 0},
		{"fareCoins", s.FareCoins, 60, 5000},
		{"minVisitRunes", s.MinVisitRunes, 1, 10000},
		{"hopMinGain", s.HopMinGain, 0, 0},
		{"lundailMaxDeaths", s.LundailMaxDeaths, 1, 10},
		{"deliverMinRunes", s.DeliverMinRunes, 1, 0},
		{"deliverMaxAgeMinutes", s.DeliverMaxAgeMinutes, 10, 1440},
		{"muleWaitMinutes", s.MuleWaitMinutes, 0, 60},
		{"idleMaxMinutes", s.IdleMaxMinutes, 1, 120},
		{"tickMs", s.TickMs, 100, 1000},
		{"stopAfterRounds", s.StopAfterRounds, 0, 0},
		{"zaffStaffs", s.ZaffStaffs, 0, MaxZaffStaffs},
		{"zaffStaffCap", s.ZaffStaffCap, 1, MaxZaffStaffCap},
		{"zaffBattlestaffs", s.ZaffBattlestaffs, 0, MaxZaffStaffs},
		{"zaffBattlestaffCap", s.ZaffBattlestaffCap, 1, MaxZaffStaffCap},
		{"zaffFireStaffs", s.ZaffFireStaffs, 0, MaxZaffStaffs},
		{"zaffFireStaffCap", s.ZaffFireStaffCap, 1, MaxZaffStaffCap},
		{"topUpBelow", s.TopUpBelow, 0, MaxTopUpAmount},
		{"topUpAmount", s.TopUpAmount, 0, MaxTopUpAmount},
		{"muleCoinFloor", s.MuleCoinFloor, 0, MaxMuleCoinFloor},
		{"rotationPhase", s.RotationPhase, -1, MaxRotationPhase},
		{"maxHopsPerHour", s.MaxHopsPerHour, 1, MaxMaxHopsPerHour},
		{"homeWorld", s.HomeWorld, 0, 255},
		{"swarmCheckMinutes", s.SwarmCheckMinutes, MinSwarmCheckMinutes, MaxSwarmCheckMinutes},
		{"trawlerStintMinutes", s.TrawlerStintMinutes, 0, MaxTrawlerStintMinutes},
		{"trawlerRoundHoldMaxMinutes", s.TrawlerRoundHoldMaxMinutes, 0, MaxTrawlerRoundHoldMaxMinutes},
		{"swarmResumeRunes", s.SwarmResumeRunes, 1, MaxSwarmResumeRunes},
		{"marketPricePercent", s.MarketPricePercent, marketstockplan.MinPricePercent, marketstockplan.MaxPricePercent},
		{"marketCoins", s.MarketCoins, 1000, marketstockplan.MaxCoins},
		{"marketCheckMinutes", s.MarketCheckMinutes, 1, marketstockplan.MaxCheckMinutes},
		{"marketMinLot", s.MarketMinLot, 1, marketstockplan.MaxMinLot},
		{"marketTradeCoins", s.MarketTradeCoins, 1000, marketstockplan.MaxTradeCoins},
		{"ammoCoins", s.AmmoCoins, 0, MaxAmmoCoins},
		{"ammoMinVisitCoins", s.AmmoMinVisitCoins, 0, MaxAmmoMinVisitCoins},
		{"ammoSlotReserve", s.AmmoSlotReserve, 0, MaxAmmoSlotReserve},
		{"ropeFloor", s.RopeFloor, 0, MaxRopeFloor},
	} {
		if r.val < r.min || (r.max > 0 && r.val > r.max) {
			if r.max > 0 {
				add("%s: %d is outside %d-%d", r.key, r.val, r.min, r.max)
			} else {
				add("%s: %d is below %d", r.key, r.val, r.min)
			}
		}
	}
	if _, err := s.AmmoShopKeys(); err != nil {
		add("ammoShops: %v", err)
	}
	if _, err := s.DeliverNotedNames(); err != nil {
		add("deliverNoted: %v", err)
	}
	if items, err := s.AmmoItemNames(); err != nil {
		add("ammoItems: %v", err)
	} else {
		if _, err := ParseAmmoCaps(s.AmmoPriceCaps, items); err != nil {
			add("ammoPriceCaps: %v", err)
		}
		if _, err := ParseAmmoStop(s.AmmoStopAbove, items); err != nil {
			add("ammoStopAbove: %v", err)
		}
	}
	if _, err := marketstockplan.ParseTargets(s.MarketRunes); err != nil {
		add("marketRunes: %v", err)
	}
	if _, err := marketstockplan.ParseStalls(s.MarketStalls); err != nil {
		add("marketStalls: %v", err)
	}
	if v := s.TrawlerRoundHoldMaxMinutes; v > 0 && v < MinTrawlerRoundHoldMaxMinutes {
		add("trawlerRoundHoldMaxMinutes: %d; it is 0 (no cap) or %d-%d", v, MinTrawlerRoundHoldMaxMinutes, MaxTrawlerRoundHoldMaxMinutes)
	}
	if s.MaxHopsPerRound != 0 && s.MaxHopsPerRound != 2 {
		add("maxHopsPerRound: %d; it is 2 (the other world) or 0 (one world)", s.MaxHopsPerRound)
	}
	if strings.TrimSpace(s.CheckpointDir) == "" {
		add("checkpointDir: must not be empty")
	}
	if s.LogDetail != "Normal" && s.LogDetail != "Verbose" {
		add("logDetail: %q; it is Normal or Verbose", s.LogDetail)
	}
	if len(problems) > 0 {
		return fmt.Errorf("RuneRunner settings: %s", strings.Join(problems, "; "))
	}
	return nil
}

// ShopKeys are the shops setting in canonical spelling, checked: known, built, no duplicates.
func (s Settings) ShopKeys() ([]string, error) {
	if len(s.Shops) == 0 {
		return nil, fmt.Errorf("no shops listed (use %s)", strings.Join(DefaultSites(), ", "))
	}
	var out []string
	seen := map[string]bool{}
	for _, name := range s.Shops {
		site, ok := Site(name)
		if !ok {
			return nil, fmt.Errorf("%q is not a rune shop RuneRunner knows (%s)", name, strings.Join(DefaultSites(), ", "))
		}
		// A site behind a level gate (the Wizards' Guild, Magic 66) is accepted here: the level is
		// the account's CURRENT one, read each round (EligibleShops), not a setting.
		if !site.Built {
			return nil, fmt.Errorf("%s: route not built", site.Key)
		}
		if seen[site.Key] {
			return nil, fmt.Errorf("%s is listed twice", site.Key)
		}
		seen[site.Key] = true
		out = append(out, site.Key)
	}
	return out, nil
}

// RuneNames are the runes setting in canonical spelling and priority order, each sold by at
// least one of the enabled shops.
func (s Settings) RuneNames(shops []string) ([]string, error) {
	if len(s.Runes) == 0 {
		return nil, fmt.Errorf("no runes listed")
	}
	var out []string
	seen := map[string]bool{}
	for _, r := range s.Runes {
		name, ok := CanonicalRune(r)
		if !ok {
			return nil, fmt.Errorf("%q is not sold by any rune shop RuneRunner knows", r)
		}
		sold := false
		for _, key := range shops {
			site, _ := Site(key)
			if site.Shop.Sells(name) {
				sold = true
				break
			}
		}
		if !sold {
			return nil, fmt.Errorf("%s is not sold by any enabled shop (%s)", name, strings.Join(shops, ", "))
		}
		if seen[name] {
			return nil, fmt.Errorf("%s is listed twice", name)
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}

// MaxPriceCap is the highest cap a setting may name.
const MaxPriceCap = 1000

// BuyAllCaps is the cap table that buys a shelf all the way down: every listed rune at
// MaxPriceCap, which is above every shop's own price ceiling (six times the rune's cost; the
// dearest, Blood at 50, tops out at 300), so the shop's ceiling is the only limit.
func BuyAllCaps(runes []string) Caps {
	out := make(Caps, len(runes))
	for _, r := range runes {
		out[r] = MaxPriceCap
	}
	return out
}

// MaxZaffStaffs and MaxZaffStaffCap bound the staff settings. Zaff's shelf is two; more than that
// is bought only when players have sold him extras.
const (
	MaxZaffStaffs   = 10
	MaxZaffStaffCap = 100000
)

// ParseCaps reads "Rune=gp" entries against the listed runes: exactly one cap per listed rune,
// none for an unlisted rune, each 1..MaxPriceCap.
func ParseCaps(entries []string, runes []string) (Caps, error) {
	listed := map[string]bool{}
	for _, r := range runes {
		listed[r] = true
	}
	out := Caps{}
	for _, e := range entries {
		eq := strings.LastIndex(e, "=")
		if eq <= 0 {
			return nil, fmt.Errorf("%q is not Rune=gp", e)
		}
		name, ok := CanonicalRune(strings.TrimSpace(e[:eq]))
		if !ok {
			return nil, fmt.Errorf("%q: no shop sells %q", e, strings.TrimSpace(e[:eq]))
		}
		gp, err := strconv.Atoi(strings.TrimSpace(e[eq+1:]))
		if err != nil {
			return nil, fmt.Errorf("%q: %q is not a whole number of coins", e, strings.TrimSpace(e[eq+1:]))
		}
		if gp < 1 || gp > MaxPriceCap {
			return nil, fmt.Errorf("%q: the cap must be 1-%d coins", e, MaxPriceCap)
		}
		if !listed[name] {
			return nil, fmt.Errorf("%q caps %s, which is not in runes", e, name)
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("%s is capped twice", name)
		}
		out[name] = gp
	}
	for _, r := range runes {
		if _, ok := out[r]; !ok {
			return nil, fmt.Errorf("%s has no cap (add \"%s=<gp>\")", r, r)
		}
	}
	return out, nil
}

// ParseStand reads the muleStand setting: "" is no override (ok false), else "x,z" or "x,z,0".
// The post is a bank on the ground floor, so any other level is an error.
func ParseStand(s string) (t tile.Tile, ok bool, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return tile.Tile{}, false, nil
	}
	parts := strings.Split(s, ",")
	if len(parts) < 2 || len(parts) > 3 {
		return tile.Tile{}, false, fmt.Errorf("%q is not x,z", s)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, convErr := strconv.Atoi(strings.TrimSpace(p))
		if convErr != nil {
			return tile.Tile{}, false, fmt.Errorf("%q is not x,z", s)
		}
		nums[i] = n
	}
	if nums[2] != 0 {
		return tile.Tile{}, false, fmt.Errorf("%q is on level %d; the post is on level 0", s, nums[2])
	}
	if nums[0] <= 0 || nums[1] <= 0 {
		return tile.Tile{}, false, fmt.Errorf("%q is not a map tile", s)
	}
	return tile.Tile{X: nums[0], Z: nums[1]}, true, nil
}

// Tick is the tickMs setting as a duration.
func (s Settings) Tick() time.Duration { return time.Duration(s.TickMs) * time.Millisecond }

// Resolved is a validated Settings in the shapes the ring uses.
type Resolved struct {
	Settings
	ShopList  []string
	RuneList  []string
	CapTable  Caps
	Stand     tile.Tile
	HaveStand bool
	// Rotation is true when the runner works the rotation circuit (Settings.Rotating); its
	// Settings then carry the filled worlds (DefaultRotationWorlds when none were listed).
	Rotation bool
	// MarketTargets and MarketStallList are marketRunes and marketStalls checked, with their
	// defaults filled.
	MarketTargets   []marketstockplan.Target
	MarketStallList []string
	// AmmoSiteList, AmmoItemList, AmmoCapTable and AmmoStop are the ammo settings checked (ammo.go): the
	// listed ammo shops and parts in canonical spelling, the cap of each part, and the mule-stock gates.
	AmmoSiteList []string
	AmmoItemList []string
	AmmoCapTable Caps
	AmmoStop     map[string]int
	// DeliverNotedList is deliverNoted checked, in canonical spelling.
	DeliverNotedList []string
}

// Resolve validates s and returns it in the shapes the ring uses, with its empty lists filled
// (Filled).
func (s Settings) Resolve() (Resolved, error) {
	if err := s.Validate(); err != nil {
		return Resolved{}, err
	}
	capsWritten := len(s.PriceCaps) > 0
	s = s.Filled()
	s.Rotate = strings.ToLower(strings.TrimSpace(s.Rotate))
	shops, _ := s.ShopKeys()
	runes, _ := s.RuneNames(shops)
	caps, _ := ParseCaps(s.PriceCaps, runes)
	if s.BuyAll && !capsWritten {
		// An operator who wrote priceCaps keeps them; buyAll fills the table only when none were written.
		caps = BuyAllCaps(runes)
	}
	stand, have, _ := ParseStand(s.MuleStand)
	targets, _ := marketstockplan.ParseTargets(s.MarketRunes)
	stalls, _ := marketstockplan.ParseStalls(s.MarketStalls)
	ammoSites, _ := s.AmmoShopKeys()
	ammoItems, _ := s.AmmoItemNames()
	ropeAdded := false
	if s.TrawlerRope && s.AmmoBuy {
		ammoSites, ammoItems, ropeAdded = s.WithTrawlerRope(ammoSites, ammoItems)
	}
	ammoWritten, _ := ParseAmmoCaps(s.AmmoPriceCaps, ammoItems)
	ammoStop, _ := ParseAmmoStop(s.AmmoStopAbove, ammoItems)
	if ropeAdded {
		if ammoStop == nil {
			ammoStop = map[string]int{}
		}
		// A Rope the operator did not list takes the default gate; one they listed keeps theirs (0 is "no gate").
		ammoStop[AmmoRope] = TrawlerRopeStopAbove
	}
	noted, _ := s.DeliverNotedNames()
	return Resolved{DeliverNotedList: noted, Settings: s, ShopList: shops, RuneList: runes, CapTable: caps, Stand: stand, HaveStand: have, Rotation: s.Rotating(),
		MarketTargets: targets, MarketStallList: stalls,
		AmmoSiteList: ammoSites, AmmoItemList: ammoItems, AmmoCapTable: AmmoCaps(ammoItems, ammoWritten, s.AmmoBuyAll), AmmoStop: ammoStop}, nil
}

// WithTrawlerRope is the listed ammo sites and parts with the Fishing Trawler's Rope round added (TrawlerRopeSites, and Rope when it
// is not listed): the sites and parts in canonical spelling, and whether Rope was added by this and not listed.
func (s Settings) WithTrawlerRope(sites, items []string) (outSites, outItems []string, ropeAdded bool) {
	outSites = append([]string(nil), sites...)
	for _, k := range TrawlerRopeSites(s.RopeFloor) {
		if !enabled(outSites, k) {
			outSites = append(outSites, k)
		}
	}
	outItems = append([]string(nil), items...)
	if !enabled(outItems, AmmoRope) {
		outItems = append(outItems, AmmoRope)
		ropeAdded = true
	}
	return outSites, outItems, ropeAdded
}

// DeliverNotedNames is the deliverNoted setting in canonical spelling, checked: known ammo parts that take a pack slot a unit, no
// duplicates.
func (s Settings) DeliverNotedNames() ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, name := range s.DeliverNoted {
		it, ok := AmmoItemFor(name)
		switch {
		case !ok:
			return nil, fmt.Errorf("%q is not an ammo part RuneRunner knows", name)
		case !it.Unstackable:
			return nil, fmt.Errorf("%s stacks, so it needs no note", it.Name)
		case seen[it.Name]:
			return nil, fmt.Errorf("%s is listed twice", it.Name)
		}
		seen[it.Name] = true
		out = append(out, it.Name)
	}
	return out, nil
}

// Staffs reports whether the ring visits Zaff, for Staffs of air, Staffs of fire or Battlestaffs.
func (r Resolved) Staffs() bool {
	return r.ZaffStaffs > 0 || r.ZaffBattlestaffs > 0 || r.ZaffFireStaffs > 0
}

// AmmoOn reports whether the ring carries ammo stations: ammoBuy on, a rotation, and at least one listed ammo
// shop the ring can place with the shops setting (ammo.go).
func (r Resolved) AmmoOn() bool {
	if !r.AmmoBuy || !r.Rotation || len(r.AmmoItemList) == 0 {
		return false
	}
	placed, _ := SplitAmmoSites(r.AmmoSiteList, r.ShopList)
	return len(placed) > 0
}

// AmmoPlaced are the ammo shops the ring places, given the shops this round runs (the round's shops may be
// fewer than the shops setting while the Wizards' Guild door is shut), and the reasons for the rest.
func (r Resolved) AmmoPlaced(roundShops []string) (placed, skipped []string) {
	if !r.AmmoOn() {
		return nil, nil
	}
	return SplitAmmoSites(r.AmmoSiteList, roundShops)
}

// DeliverRuneItems is what counts toward a delivery's size: the listed runes, then Staffs of air when the ring
// buys them.
func (r Resolved) DeliverRuneItems() []string {
	out := append([]string{}, r.RuneList...)
	if r.Staffs() {
		out = append(out, fleetplan.StaffOfAir)
	}
	// What Zaff's station buys must be on the delivery list or it never reaches the mule: it is listed only when bought.
	if r.ZaffFireStaffs > 0 {
		out = append(out, StaffOfFire)
	}
	if r.ZaffBattlestaffs > 0 {
		out = append(out, Battlestaff)
	}
	return out
}

// DeliverAmmoItems is the ammo parts the delivery carries (nil with ammo off). They travel with the runes
// but never make a delivery due on their own (DeliverDueAmmo).
func (r Resolved) DeliverAmmoItems() []string {
	if !r.AmmoOn() {
		return nil
	}
	return append([]string{}, r.AmmoItemList...)
}

// DeliverItems is what the delivery hands the mule, in priority order: the listed runes, then
// Staffs of air when the ring buys them, then the ammo parts when the ring buys those. Coins and
// everything else never cross.
func (r Resolved) DeliverItems() []string {
	return append(r.DeliverRuneItems(), r.DeliverAmmoItems()...)
}

// Enabled reports whether a site is in the shops setting.
func (r Resolved) Enabled(site string) bool {
	for _, k := range r.ShopList {
		if strings.EqualFold(k, site) {
			return true
		}
	}
	return false
}

// OtherWorld is the world the O block buys on: the first listed world that is not the mule's,
// or -- with nothing listed -- the fleet's other world (2 beside 1, else 1). 0 means none: a
// one-world buyer.
func OtherWorld(m int, worlds []int) int {
	if len(worlds) == 0 {
		if m == 1 {
			return 2
		}
		return 1
	}
	for _, w := range worlds {
		if w != m {
			return w
		}
	}
	return 0
}
