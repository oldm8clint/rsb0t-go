package bot

// RuneRunner: a dedicated rune buyer that feeds the mule.
//
// Owner, 2026-09-26: "the shops probably hold plenty of minds, maybe mule stays and another
// account buys them out. should be buying all runes from all 3 stores when we can, wizards guild
// locked till 66 magic". One account -- not the mule -- runs this. Each round it walks
// Catherby -> Port Sarim -> Varrock -> Ardougne -> Catherby, buys every listed rune under its price
// cap from Betty, Aubury and Lundail on the mule's world and on one other, and trades the runes to
// the mule at its post. the mule never moves.
//
// The decisions are internal/scripts/runerunnerplan's (the ring, the caps, the coins, the
// delivery gate, the hop and Lundail rules); the I/O is internal/scripts/runerunner over
// scarabrunner's proven legs. This file and runerunner_station.go sequence them: one station per
// Loop call, the state saved after every station and before every hop, because a hop logs the
// account out and the fleet starts a fresh copy of this script on the other world.
//
// ROLLOUT GATES, none of them this code's: the owner's go; the mule listing this account in
// allowedPartners (read once in the mule's OnStart, so the mule restarts); no pinWorld on this
// account (OnStart refuses one). See docs/runerunner_handoff_2026-09-26.md.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/phaselog"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/marketstockplan"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/runerunnerplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/settings"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("RuneRunner", func() Bot { return &runeRunnerBot{} })
}

// RuneRunnerSettings is the script's declared schema. Its keys are exactly
// runerunnerplan.Settings' json tags (a test keeps them identical). Loading is strict:
// decodeRuneRunnerSettings refuses an unknown key, pinWorld, a value of the wrong type, and a
// number or option outside what this schema allows BEFORE Resolve could clamp it, and then
// runerunnerplan.Settings.Validate checks everything that depends on more than one key.
var RuneRunnerSettings = settings.Schema{
	"mule": {Type: settings.TypeString, Default: runerunnerplan.DefaultMule, Label: "Mule",
		Help: "the MuleReceiver the runes are traded to; it must list this account in allowedPartners"},
	"muleStand": {Type: settings.TypeString, Default: "", Label: "Mule post (x,z)",
		Help: "empty uses the post the mule's heartbeat publishes, else 2809,3441 (Catherby bank)"},
	"shops": {Type: settings.TypeList, Default: runerunnerplan.DefaultSites(), Label: "Shops",
		Help: "Aubury, Betty, Lundail, WizardsGuild; the Wizards' Guild is left out of a round while this account's Magic is below 66"},
	"worlds": {Type: settings.TypeList, Default: []string{}, Label: "Worlds",
		Help: "rotating: the worlds to buy on (empty: 1, 2, 3). Classic ring: the mule's world always, plus the first listed other one (empty: the fleet's other)"},
	"runes": {Type: settings.TypeList, Default: []string{}, Label: "Runes (priority order)",
		Help: "empty: every rune the enabled shops stock when rotating, Mind, Air, Chaos on the classic ring"},
	"priceCaps": {Type: settings.TypeList, Default: []string{}, Label: "Price caps (Rune=gp)",
		Help: "exactly one per listed rune, 1-1000 coins; a unit is bought only while its price is at or under the cap. Empty: three times each rune's base price (Mind 18, Air 5, Chaos 90 for the classic ring's default runes)"},
	"rotate": {Type: settings.TypeString, Default: runerunnerplan.DefaultRotate, Options: runerunnerplan.RotateOptions(), Label: "Rotate over every shop and world",
		Help: "on: a circuit over every listed shop on every listed world, timed by restock; off: the classic ring; auto: rotate unless shops names one shop"},
	"rotationPhase": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultRotationPhase),
		Min: settings.Float(-1), Max: settings.Float(runerunnerplan.MaxRotationPhase), Label: "Rotation phase (-1 = from the name)",
		Help: "where on the circuit this runner works: direction and world order. Give each runner its own (0-5 for three worlds) so they never share a shop and world"},
	"maxHopsPerHour": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultMaxHopsPerHour),
		Min: settings.Float(1), Max: settings.Float(runerunnerplan.MaxMaxHopsPerHour), Label: "Rotation hops per hour",
		Help: "a rotating runner's hops in any hour; hops are also two minutes apart"},
	"homeWorld": {Type: settings.TypeNumber, Default: float64(0), Min: settings.Float(0), Max: settings.Float(255),
		Label: "Login world (0 = the roster's)", Help: "the control panel logs the account in here at a start; the script does not read it"},
	"swarmWhileWaiting": {Type: settings.TypeBoolean, Default: runerunnerplan.DefaultSwarmWhileWaiting, Label: "Run ScarabSwarm while the shops restock",
		Help: "rotating only: after a circuit, when the next is not worth swarmResumeRunes, deliver to the mule and run ScarabSwarm until it is"},
	"swarmCheckMinutes": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultSwarmCheckMinutes),
		Min: settings.Float(runerunnerplan.MinSwarmCheckMinutes), Max: settings.Float(runerunnerplan.MaxSwarmCheckMinutes), Label: "Restock check while on ScarabSwarm (minutes)"},
	"swarmResumeRunes": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultSwarmResumeRunes),
		Min: settings.Float(1), Max: settings.Float(runerunnerplan.MaxSwarmResumeRunes), Label: "Circuit worth a walk (runes)",
		Help: "the runes a whole circuit is estimated to buy under the caps at which the runner leaves (or does not start) ScarabSwarm"},
	"trawlerStintMinutes": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultTrawlerStintMinutes),
		Min: settings.Float(0), Max: settings.Float(runerunnerplan.MaxTrawlerStintMinutes), Label: "Trawler stint between rounds (minutes; 0 = until the block ends)",
		Help: "while master's trawler block is on, the account joins the crew only between two rounds (the new round's delivery made, no shop visited) and stays this long, about one game at 20"},
	"trawlerRoundHoldMaxMinutes": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultTrawlerRoundHoldMaxMinutes),
		Min: settings.Float(0), Max: settings.Float(runerunnerplan.MaxTrawlerRoundHoldMaxMinutes), Label: "Longest an unfinished round keeps the account off the trawler (minutes; 0 = no cap)",
		Help: "a round that has held the crew this long (a stalled one) joins anyway and says so"},
	"swarmSettings": {Type: settings.TypeString, Default: "", Label: "ScarabSwarm settings (JSON object)",
		Help: "passed to ScarabSwarm unchanged while it runs between circuits; mule defaults to this runner's"},
	"tripCoins": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultTripCoins),
		Min: settings.Float(1000), Max: settings.Float(10000000), Label: "Coins per draw",
		Help: "the most one bank draw puts in the pack; the purse for the whole round (every shop on every world, plus a margin) is held in the bank and drawn from it as the round goes, so a round dearer than this takes more than one draw"},
	"coinReserve": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultCoinReserve),
		Min: settings.Float(0), Label: "Coins kept in the bank"},
	"fareCoins": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultFareCoins),
		Min: settings.Float(60), Max: settings.Float(5000), Label: "Boat fare coins carried"},
	"minVisitRunes": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultMinVisitRunes),
		Min: settings.Float(1), Max: settings.Float(10000), Label: "Smallest purchase worth a visit"},
	"hopMinGain": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultHopMinGain),
		Min: settings.Float(0), Label: "Smallest other-world purchase worth two hops"},
	"maxHopsPerRound": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultMaxHopsPerRound),
		Min: settings.Float(0), Max: settings.Float(2), Label: "Hops per round (0 or 2)"},
	"lundailMaxDeaths": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultLundailMaxDeaths),
		Min: settings.Float(1), Max: settings.Float(10), Label: "Lundail deaths in 24h before Lundail stops"},
	"deliverMinRunes": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultDeliverMinRunes),
		Min: settings.Float(1), Label: "Smallest delivery"},
	"deliverMaxAgeMinutes": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultDeliverMaxAgeMinutes),
		Min: settings.Float(10), Max: settings.Float(1440), Label: "Deliver anything held after (minutes)"},
	"muleWaitMinutes": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultMuleWaitMinutes),
		Min: settings.Float(0), Max: settings.Float(60), Label: "Wait at the post for a busy mule (minutes)"},
	"idleMaxMinutes": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultIdleMaxMinutes),
		Min: settings.Float(1), Max: settings.Float(120), Label: "Longest idle wait (minutes)"},
	"tickMs": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultTickMs),
		Min: settings.Float(100), Max: settings.Float(1000), Label: "Engine tick (ms; lab 200)"},
	"checkpointDir": {Type: settings.TypeString, Default: runerunnerplan.DefaultCheckpointDir, Label: "State directory"},
	"stopAfterRounds": {Type: settings.TypeNumber, Default: float64(0), Min: settings.Float(0),
		Label: "Stop after rounds (0 = never; lab)"},
	"logDetail": {Type: settings.TypeString, Default: runerunnerplan.DefaultLogDetail,
		Options: []string{"Normal", "Verbose"}, Label: "Log detail"},
	"zaffStaffs": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultZaffStaffs),
		Min: settings.Float(0), Max: settings.Float(runerunnerplan.MaxZaffStaffs), Label: "Staffs of air from Zaff per Varrock visit (0 = off)",
		Help: "his shelf holds 2, one back every 1000 ticks; bought staffs go to the mule with the runes"},
	"zaffStaffCap": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultZaffStaffCap),
		Min: settings.Float(1), Max: settings.Float(runerunnerplan.MaxZaffStaffCap), Label: "Most coins one Staff of air may cost"},
	"zaffBattlestaffs": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultZaffBattlestaffs),
		Min: settings.Float(0), Max: settings.Float(runerunnerplan.MaxZaffStaffs), Label: "Battlestaffs from Zaff per Varrock visit (0 = off)"},
	"zaffBattlestaffCap": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultZaffBattlestaffCap),
		Min: settings.Float(1), Max: settings.Float(runerunnerplan.MaxZaffStaffCap), Label: "Most coins one Battlestaff may cost"},
	"zaffFireStaffs": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultZaffFireStaffs),
		Min: settings.Float(0), Max: settings.Float(runerunnerplan.MaxZaffStaffs), Label: "Staffs of fire from Zaff per Varrock visit (0 = off)",
		Help: "the alchers' staff: his shelf holds 2, one back every 1000 ticks; bought staffs go to the mule with the runes"},
	"zaffFireStaffCap": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultZaffFireStaffCap),
		Min: settings.Float(1), Max: settings.Float(runerunnerplan.MaxZaffStaffCap), Label: "Most coins one Staff of fire may cost"},
	"topUpBelow": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultTopUpBelow),
		Min: settings.Float(0), Max: settings.Float(runerunnerplan.MaxTopUpAmount), Label: "Ask the mule for coins below (bank + pack; 0 = never)",
		Help: "asked at the round's first station, on the mule's world, at most once per 20 minutes; with buyAll it is raised to the whole round's estimated cost (every shop on every world, projected to when the round reaches it, plus a 20% margin and the Lundail purchase); the mule's coinsPerOrder/coinsPerWindow bound what it gives"},
	"topUpAmount": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultTopUpAmount),
		Min: settings.Float(0), Max: settings.Float(runerunnerplan.MaxTopUpAmount), Label: "Coins one mule top-up asks for (0 = never)",
		Help: "the ask when the mule's coin stack is not published, and the least it asks then; with topUpWholeStack and a published stack the ask is the mule's whole stack above muleCoinFloor instead. 0 turns the top-up off"},
	"topUpWholeStack": {Type: settings.TypeBoolean, Default: runerunnerplan.DefaultTopUpWholeStack, Label: "Ask the mule for its whole coin stack",
		Help: "when bank plus pack is under the round's full bill (runes, staffs, Lundail, margin, fares and ammo): ask the mule's entire published coin stack above muleCoinFloor, in as many orders as its coinsPerOrder needs, and keep what is not spent in this account's bank for the next round. Off asks topUpAmount (the shortfall with buyAll)"},
	"muleCoinFloor": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultMuleCoinFloor),
		Min: settings.Float(0), Max: settings.Float(runerunnerplan.MaxMuleCoinFloor), Label: "Coins a top-up leaves in the mule's bank",
		Help: "kept for the cohort's small coin asks (a kit's 1,000-coin line, a fisher's fare, a trainer's blade purse): a mule found empty answers them 'none in the bank' and every account replays that for 30 minutes. 0 takes every coin"},
	"buyAll": {Type: settings.TypeBoolean, Default: runerunnerplan.DefaultBuyAll, Label: "Buy every rune a shop stocks",
		Help: "buy each shelf down to the shop's own price ceiling (six times the rune's cost) unless priceCaps is written; the coin draw and the mule top-up are sized to the whole circuit. Off: priceCaps and shortageFirst as written"},
	"walkAlch": {Type: settings.TypeBoolean, Default: runerunnerplan.DefaultWalkAlch, Label: "High Level Alchemy while walking",
		Help: "only at Magic 55+, with a kit from the mule, and only while the mule holds at least 2000 Nature runes to spare"},
	"walkRub": {Type: settings.TypeBoolean, Default: runerunnerplan.DefaultWalkRub, Label: "Rub a Games necklace or Ring of dueling while walking",
		Help: "keeps run energy up; the jewellery stays in the pack through bank deposits and is drawn from the account's bank or asked of the mule"},
	"teleTravel": {Type: settings.TypeBoolean, Default: runerunnerplan.DefaultTeleTravel, Label: "Teleport before a long walk",
		Help: "starts a walk that a teleport shortens by 150+ tiles with Varrock, Falador, Lumbridge or Camelot Teleport when the pack holds the runes (laws come from the mule when it has plenty)"},
	"shortageFirst": {Type: settings.TypeBoolean, Default: runerunnerplan.DefaultShortageFirst, Label: "Buy the runes the mule is out of first",
		Help: "when the mule's published bank holds few Nature, Chaos, Death or Fire runes: buy them first and up to the shop's price ceiling (six times base) instead of the price caps"},
	"marketBuy": {Type: settings.TypeBoolean, Default: runerunnerplan.DefaultMarketBuy, Label: "Buy scarce runes from the player market",
		Help: "after the draw: when a Seers stall lists a rune the mule is short of at a price under the ceiling, walk to Seers, buy it with the bank's coins and bank it for the next delivery; costs one cached page read per check otherwise"},
	"marketRunes": {Type: settings.TypeList, Default: marketstockplan.DefaultTargets(), Label: "Market stock goals (Rune=count)",
		Help: "the mule's stock the market station buys up to, in priority order: it buys a rune only while the mule's published bank plus what this runner holds for it is under the goal; a rune not in runes is not delivered, so it is not bought"},
	"marketStalls": {Type: settings.TypeList, Default: marketstockplan.DefaultStalls(), Label: "Market stalls asked",
		Help: "account names, in order; a stall that is not among the visible players at the Seers bank is left alone for hours"},
	"marketPricePercent": {Type: settings.TypeNumber, Default: float64(marketstockplan.DefaultPricePercent),
		Min: settings.Float(marketstockplan.MinPricePercent), Max: settings.Float(marketstockplan.MaxPricePercent), Label: "Price ceiling (% of the book's median sell price)",
		Help: "150 = up to 1.5 times the median a stall charged in the newest trades, never above the rune's fixed hard ceiling (Nature 1000, Death 600, Fire 100, Chaos 300)"},
	"marketCoins": {Type: settings.TypeNumber, Default: float64(marketstockplan.DefaultCoins),
		Min: settings.Float(1000), Max: settings.Float(marketstockplan.MaxCoins), Label: "Most coins one market station spends",
		Help: "drawn from this account's bank less coinReserve; a bigger lot asks the mule for the difference (its coinsPerOrder bounds each order)"},
	"marketCheckMinutes": {Type: settings.TypeNumber, Default: float64(marketstockplan.DefaultCheckMinutes),
		Min: settings.Float(1), Max: settings.Float(marketstockplan.MaxCheckMinutes), Label: "Minutes between looks at the market"},
	"marketMinLot": {Type: settings.TypeNumber, Default: float64(marketstockplan.DefaultMinLot),
		Min: settings.Float(1), Max: settings.Float(marketstockplan.MaxMinLot), Label: "Smallest lot worth the walk to Seers",
		Help: "a shortfall smaller than this is bought whole"},
	"marketTradeCoins": {Type: settings.TypeNumber, Default: float64(marketstockplan.DefaultTradeCoins),
		Min: settings.Float(1000), Max: settings.Float(marketstockplan.MaxTradeCoins), Label: "The stall's coin limit per trade",
		Help: "S15's trades were 1,000,000 or under; the Seers Market's own limit is 5,000,000; a bigger lot is split into trades"},
	"ammoBuy": {Type: settings.TypeBoolean, Default: runerunnerplan.DefaultAmmoBuy, Label: "Buy arrowtips, bolts and feathers on the circuit",
		Help: "rotating only: Hickton (Catherby) at both ends of the round, Gerrant (Port Sarim) beside every Betty visit and Lowe (Varrock) beside every Aubury visit, no extra hop; what is bought goes to the mule with the runes. Lowest priority: only coins the runes ahead do not need"},
	"ammoShops": {Type: settings.TypeList, Default: runerunnerplan.DefaultAmmoShops(), Label: "Ammo shops",
		Help: "Hickton, Lowe, Gerrant and Aemad (East Ardougne, with the Lundail pairs; Aemad and Kortan share one shelf); Dargaud (Ranged 40), Lathas (Biohazard), Gulluck (no route), FishingGuild (Fishing 68), Shantay and Nurmof are accepted and left out of the round with the reason"},
	"ammoEveryWorld": {Type: settings.TypeBoolean, Default: runerunnerplan.DefaultAmmoEveryWorld, Label: "Visit Hickton on every world",
		Help: "rotating only: the worlds the circuit neither starts from nor ends on get a hop and a Hickton stop of their own at the end of the round; the hop is made only when the shelf is worth ammoMinVisitCoins"},
	"ammoItems": {Type: settings.TypeList, Default: runerunnerplan.DefaultAmmoItems(), Label: "Ammo parts (priority order)",
		Help: "Bolts, Steel arrowtips, Feather, Mithril arrowtips, Adamant arrowtips, Rune arrowtips, Arrow shaft (no built shop sells it: Dargaud needs Ranged 40); the last ones have no fletching consumer yet and are gated by ammoStopAbove"},
	"ammoBuyAll": {Type: settings.TypeBoolean, Default: runerunnerplan.DefaultAmmoBuyAll, Label: "Buy each ammo part down to the shop's ceiling",
		Help: "the owner's buy-all: down to the shop's own price ceiling (six times the part's cost where the shelf holds 500 or more); off, to three times its cost. An ammoPriceCaps entry wins for its part"},
	"ammoPriceCaps": {Type: settings.TypeList, Default: []string{}, Label: "Ammo price caps (Item=gp)",
		Help: "1-1000 coins for the parts that need a cap of their own; a part without an entry takes the ammoBuyAll rule"},
	"ammoStopAbove": {Type: settings.TypeList, Default: runerunnerplan.DefaultAmmoStopAbove(), Label: "Stop buying a part when the mule holds (Item=count)",
		Help: "a part is not bought while the mule's published bank (plus what this runner carries) reaches its count; 0 or no entry is no gate. With the mule's bank unread only Bolts, Steel arrowtips and Feather are bought"},
	"ammoCoins": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultAmmoCoins),
		Min: settings.Float(0), Max: settings.Float(runerunnerplan.MaxAmmoCoins), Label: "Most coins one round spends on ammo (0 = unlimited)",
		Help: "the parts are bought in priority order, so a spent budget leaves the dearest dead stock (Adamant and Rune arrowtips) behind"},
	"ammoMinVisitCoins": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultAmmoMinVisitCoins),
		Min: settings.Float(0), Max: settings.Float(runerunnerplan.MaxAmmoMinVisitCoins), Label: "Smallest ammo purchase worth a stop (coins)"},
	"ammoSlotReserve": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultAmmoSlotReserve),
		Min: settings.Float(0), Max: settings.Float(runerunnerplan.MaxAmmoSlotReserve), Label: "Pack slots an ammo stop leaves free for runes",
		Help: "a new ammo stack is started only while a slot beyond this many is free"},
	"trawlerRope": {Type: settings.TypeBoolean, Default: runerunnerplan.DefaultTrawlerRope, Label: "Stock the mule with Rope for the Fishing Trawler's net",
		Help: "needs ammoBuy: adds Port Khazard (the trawler store, 30 Rope a world, five a minute) and Aemad with the Lundail pairs, Ned (Draynor, 15 coins a rope, no shelf) with the Betty pairs, and Rope to the ammo parts, whatever ammoShops and ammoItems list"},
	"ropeFloor": {Type: settings.TypeNumber, Default: float64(runerunnerplan.DefaultRopeFloor),
		Min: settings.Float(0), Max: settings.Float(runerunnerplan.MaxRopeFloor), Label: "Rope Ned keeps the mule stocked to",
		Help: "Ned buys only the part of this the mule's published bank and the runner's own Rope leave short, so the shops come first and he fills the rest; 0 turns Ned off. A two-hour trawler block spends 150-200"},
	"khazardRopeOnly": {Type: settings.TypeBoolean, Default: runerunnerplan.DefaultKhazardRopeOnly, Label: "Buy only Rope at Port Khazard",
		Help: "the Swamp paste and Bailing buckets on that shelf are the trawler crew's own kit (the crew buys 60 paste each; the shelf holds 500 and regrows five a minute), so the runner leaves them whatever ammoItems list; the Rope is also left on the game world while the anchor's trawler block runs"},
	"deliverNoted": {Type: settings.TypeList, Default: runerunnerplan.DefaultDeliverNoted(), Label: "Unstackable parts the delivery draws as bank notes",
		Help: "a pack takes 24 single Ropes, so a delivery of loose ones moves one pack a round whatever the bank holds; as notes any count crosses in one trade slot. Rope, Vial of water and Bailing bucket"},
}

// decodeRuneRunnerSettings is the strict load: raw keys checked, raw values checked against the
// schema (so a clamp or an unmatched option cannot hide a mistake), resolved (a panel's strings
// typed, defaults filled), decoded with unknown fields refused, and validated.
func decodeRuneRunnerSettings(raw map[string]interface{}) (runerunnerplan.Resolved, error) {
	if err := runerunnerplan.CheckRawKeys(raw); err != nil {
		return runerunnerplan.Resolved{}, err
	}
	if err := strictAgainstSchema(RuneRunnerSettings, raw, "worlds", "swarmSettings"); err != nil {
		return runerunnerplan.Resolved{}, fmt.Errorf("RuneRunner settings: %w", err)
	}
	swarmSet, err := runeRunnerSwarmSettings(raw["swarmSettings"])
	if err != nil {
		return runerunnerplan.Resolved{}, fmt.Errorf("RuneRunner settings: %w", err)
	}
	bag := RuneRunnerSettings.Resolve(raw)
	// worlds may be JSON numbers, which a string-list schema would drop to its default; the
	// plan's WorldList decodes numbers, numeric strings and "1,2" itself.
	if w, ok := raw["worlds"]; ok {
		bag.Set("worlds", w)
	}
	bag.Set("swarmSettings", swarmSet)
	s := runerunnerplan.DefaultSettings()
	if err := bag.Decode(&s); err != nil {
		return runerunnerplan.Resolved{}, fmt.Errorf("RuneRunner settings: %w", err)
	}
	return s.Resolve()
}

// runeRunnerSwarmSettings reads the swarmSettings value: absent, null or a blank string is an
// empty object; an object is copied; a string must hold a JSON object (the panel's form).
func runeRunnerSwarmSettings(v interface{}) (map[string]interface{}, error) {
	out := map[string]interface{}{}
	switch x := v.(type) {
	case nil:
	case map[string]interface{}:
		for k, val := range x {
			out[k] = val
		}
	case string:
		if strings.TrimSpace(x) == "" {
			break
		}
		if err := json.Unmarshal([]byte(x), &out); err != nil {
			return nil, fmt.Errorf("swarmSettings: %q is not a JSON object (%v)", x, err)
		}
	default:
		return nil, fmt.Errorf("swarmSettings: %v is not an object of ScarabSwarm settings", v)
	}
	if _, nested := out["swarmSettings"]; nested {
		return nil, fmt.Errorf("swarmSettings: holds a swarmSettings of its own")
	}
	return out, nil
}

// strictAgainstSchema refuses a raw value Resolve would silently replace: a number of the wrong
// type or outside Min/Max, a string not among Options, a list holding a non-string. Keys in
// `free` are the caller's to check.
func strictAgainstSchema(schema settings.Schema, raw map[string]interface{}, free ...string) error {
	skip := map[string]bool{}
	for _, k := range free {
		skip[k] = true
	}
	var problems []string
	for key, v := range raw {
		def, ok := schema[key]
		if !ok || skip[key] {
			continue
		}
		switch def.Type {
		case settings.TypeNumber:
			var n float64
			switch x := v.(type) {
			case float64:
				n = x
			case int:
				n = float64(x)
			case string:
				if _, err := fmt.Sscanf(strings.TrimSpace(x), "%g", &n); err != nil {
					problems = append(problems, fmt.Sprintf("%s: %q is not a number", key, x))
					continue
				}
			default:
				problems = append(problems, fmt.Sprintf("%s: %v is not a number", key, v))
				continue
			}
			if (def.Min != nil && n < *def.Min) || (def.Max != nil && n > *def.Max) {
				problems = append(problems, fmt.Sprintf("%s: %v is outside its range", key, v))
			}
		case settings.TypeString:
			s, ok := v.(string)
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: %v is not a string", key, v))
				continue
			}
			if len(def.Options) > 0 {
				found := false
				for _, o := range def.Options {
					if strings.EqualFold(o, strings.TrimSpace(s)) {
						found = true
					}
				}
				if !found {
					problems = append(problems, fmt.Sprintf("%s: %q is not one of %s", key, s, strings.Join(def.Options, ", ")))
				}
			}
		case settings.TypeBoolean:
			switch x := v.(type) {
			case bool:
			case string:
				if t := strings.ToLower(strings.TrimSpace(x)); t != "true" && t != "false" {
					problems = append(problems, fmt.Sprintf("%s: %q is not true or false", key, x))
				}
			default:
				problems = append(problems, fmt.Sprintf("%s: %v is not true or false", key, v))
			}
		case settings.TypeList:
			switch x := v.(type) {
			case string, []string:
			case []interface{}:
				for _, e := range x {
					if _, ok := e.(string); !ok {
						problems = append(problems, fmt.Sprintf("%s: %v is not a string", key, e))
						break
					}
				}
			default:
				problems = append(problems, fmt.Sprintf("%s: %v is not a list", key, v))
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

// runeRunnerHeartbeat is the longest the bot goes without a log line outside a walk (the stall
// watchdog acts after ten silent minutes).
const runeRunnerHeartbeat = 5 * time.Minute

// runeRunnerHopGrace is how long a hop the fleet accepted may take to end this script before the
// bot decides it did not happen and carries on.
const runeRunnerHopGrace = 3 * time.Minute

// runeRunnerQuietWait bounds a wait at a hop station for the account to be quiet enough to hop.
const runeRunnerQuietWait = 10 * time.Minute

type runeRunnerBot struct {
	set    runerunnerplan.Resolved
	walker *webwalk.Walker
	store  *settings.Store
	st     runerunnerplan.State
	// keep is the fields a newer build wrote into the state file, written back on every save;
	// blocked is a state file this build could not read and must never overwrite.
	keep    settings.Unknown
	blocked string
	webs    fleetrunner.WebsCut

	// resumed is set once this run has placed the cursor (Resume); reenter asks the next Loop to
	// place it again from the position (Entry), after a death.
	resumed bool
	reenter bool
	// hopsOff says why this run cannot hop at all ("" when it can).
	hopsOff string
	// lundailOff says why Lundail is off for this run (no blade in the bank).
	lundailOff string
	// noCoins is the last draw's NO COINS: shop stations skip until a draw succeeds.
	noCoins bool
	// baseRunes and baseCaps are the runes and caps as the settings wrote them; shortageLine and
	// shortageAt are the last shortage line logged (runerunner_shortage.go).
	baseRunes    []string
	baseCaps     runerunnerplan.Caps
	shortageLine string
	shortageAt   time.Time
	// magic is the account's CURRENT Magic level as last read (noteMagic): the Wizards' Guild
	// door's gate. 0 until the server has reported it, which keeps the guild out.
	magic int
	// hopAsked is when an accepted hop was asked for, and hopTo its world; quietSince when the
	// current hop station began waiting to be quiet.
	hopAsked time.Time
	hopTo    int
	// trawlerHeld is when the trawler gate first held the crew in round trawlerHeldRound (runerunner_trawler.go).
	trawlerHeld      time.Time
	trawlerHeldRound int
	quietSince       time.Time
	lastHurt         time.Time
	lastHP           int
	lastBeat         time.Time
	said             map[string]time.Time
	// travel is the account's walk alch, rub and teleport travel (runerunner_travel.go); nil when all
	// three settings are off. travelSaid is when the walk-alch gate last logged, travelLogged the last
	// session line, kitAskAt when the mule may be asked for the travel kit again.
	travel       *fleetrunner.Travel
	travelSaid   time.Time
	travelLogged time.Time
	kitAskAt     time.Time

	// The ScarabSwarm phase between circuits (runerunner_swarm.go): the embedded swarm, how to make
	// one (nil: the real scarabSwarmBot; a test supplies another), the runner's own settings map
	// while ctx.Settings holds ScarabSwarm's, and a return to the circuit waiting for a safe point.
	swarm          masterSwarm
	newSwarm       func() masterSwarm
	runnerSettings map[string]interface{}
	swarmBack      string
	swarmWaitSaid  time.Time
	// circuitWorthSeam replaces the board's circuit estimate in a test.
	circuitWorthSeam func(ctx *Context) int
	// marketRowsSeam and marketBookSeam replace the stalls' page and the trade book in a test
	// (runerunner_market.go); nil reads the real ones.
	marketRowsSeam func() []fleetplan.MarketListing
	marketBookSeam func(item string) (marketstockplan.Book, bool)
	// marketWalkSeam replaces the market station's walks in a test (true: arrived).
	marketWalkSeam func(label string, dest tile.Tile) bool
	// roundNeedSeam replaces roundNeed in a test.
	roundNeedSeam func(from int) runerunnerplan.RoundNeed
	// ammoBuySeam replaces the ammo shop visit (runerunner_ammo.go) in a test: it receives the plan function
	// the real visit would call with the shelf and the pack's coins.
	ammoBuySeam func(site runerunnerplan.AmmoSite, planFor func(seen map[string]int, coins int) fleetplan.PurchasePlan) runerunner.AmmoVisit
	// ammoPackSeam replaces the reading of the pack's coins, free slots and ammo parts in a test.
	ammoPackSeam func() (coins, free int, ammo map[string]int)
	// nedBuySeam replaces Ned's conversations (buyNedRopes) and bankPackSeam the bank trip after a BankAfter stop (bankPack) in a test.
	nedBuySeam   func(want int) (got int, why string)
	bankPackSeam func(at tile.Tile)
	// trawlerBlockSeam replaces the read of the anchor's trawler block (trawlerBlockWorld) in a test.
	trawlerBlockSeam func() (world int, on bool)
}

func (b *runeRunnerBot) deps(ctx *Context) *fleetrunner.Deps {
	return &fleetrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Walker: b.walker,
		Log: ctx.Log, Loot: ctx.Loot, Kills: ctx.KillLog(), Me: ctx.Username, World: ctx.World(),
		Travel: b.travel,
	}
}

// logf writes one "runerunner: " line and resets the heartbeat clock.
func (b *runeRunnerBot) logf(ctx *Context, format string, args ...interface{}) {
	b.lastBeat = time.Now()
	ctx.logf("runerunner: "+format, args...)
}

// verbosef is logf when logDetail is Verbose.
func (b *runeRunnerBot) verbosef(ctx *Context, format string, args ...interface{}) {
	if b.set.LogDetail == "Verbose" {
		b.logf(ctx, format, args...)
	}
}

// sayEvery is logf at most once per `every` for one key.
func (b *runeRunnerBot) sayEvery(ctx *Context, key string, every time.Duration, format string, args ...interface{}) {
	if t, ok := b.said[key]; ok && time.Since(t) < every {
		return
	}
	b.said[key] = time.Now()
	b.logf(ctx, format, args...)
}

// heartbeat keeps the stall watchdog informed during a wait that writes nothing else.
func (b *runeRunnerBot) heartbeat(ctx *Context, station string, here *tile.Tile) {
	if time.Since(b.lastBeat) < runeRunnerHeartbeat {
		return
	}
	x, z := 0, 0
	if here != nil {
		x, z = here.X, here.Z
	}
	b.logf(ctx, "heartbeat station=%s tile=%d,%d", station, x, z)
}

// stop ends the script for good so an operator looks (a walk on the delivery path that keeps
// failing): the fleet does not log it back in.
func (b *runeRunnerBot) stop(ctx *Context, reason string) {
	b.logf(ctx, "STOP reason=%q", reason)
	b.save(ctx)
	ctx.RequestFinish("runerunner: " + reason)
	ctx.requestStop()
}

// stuck stops the run on a walk that could not reach its station -- unless a random event holds
// the account, which ends the station instead so the next pass walks again from wherever the
// event lets it go. Live 29 Sep: Acct211 stopped for good from the mime's stage (2008,4762),
// then stood idle on Karamja once the event let it out.
func (b *runeRunnerBot) stuck(ctx *Context, reason string) {
	if ctx.Guardian != nil && ctx.Guardian.Busy() {
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeAborted, Reason: "a random event holds the account: " + reason})
		ctx.Delay(10 * time.Second)
		return
	}
	b.stop(ctx, reason)
}

func (b *runeRunnerBot) OnStart(ctx *Context) error {
	// Settings first: a refusal names the setting before anything else is checked.
	set, err := decodeRuneRunnerSettings(ctx.Settings)
	if err != nil {
		return fmt.Errorf("runerunner: %w", err)
	}
	b.set = set
	b.rememberBase()
	b.said = map[string]time.Time{}
	b.lastBeat = time.Now()
	b.resumed, b.reenter = false, false
	if ctx.Pack == nil {
		return fmt.Errorf("runerunner: no collision pack configured (fleet.config.json needs a packPath) — refusing to start rather than panic later inside pathfinding")
	}

	// The same walker construction ScarabSwarm uses: the boat chain east of Catherby needs the
	// fare and dialogue hops, and without them the planner answers "unreachable".
	if w, werr := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); werr != nil {
		b.logf(ctx, "no webwalk (%v) — the boat chain cannot be planned; walks will fall back to plain pathfinding", werr)
	} else {
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{
				Client: w.Client, Session: ctx.Session, Config: ctx.Config,
				Components: ctx.Components, Log: ctx.Log,
			}))
		}
		b.walker = w
	}
	// Every walk turns running on at 20% energy (walkstall.EnsureRun), rubs a carried Games necklace or Ring of dueling
	// (walkRub), casts High Level Alchemy while the mule can spare Nature runes (walkAlch) and starts a long walk with
	// a teleport (teleTravel): runerunner_travel.go.
	b.installTravel(ctx)
	return b.startState(ctx)
}

// startState loads the saved state and tells a restart the ring caused (a hop: HopPending set)
// from one an operator caused (anything else: the price checks are cleared, which is how a pair
// the price check took off the ring comes back).
func (b *runeRunnerBot) startState(ctx *Context) error {
	b.store = settings.NewStore(b.set.CheckpointDir)
	if err := b.loadState(ctx); err != nil {
		return err
	}
	if b.st.HopPending > 0 {
		b.logf(ctx, "resumed after the ring's hop to world %d, now on world %d, round=%d at %s",
			b.st.HopPending, ctx.World(), b.st.Round, stationKeyAt(b.st))
		b.st.HopPending = 0
	} else if cleared := b.st.ClearPriceChecks(); len(cleared) > 0 {
		b.logf(ctx, "operator restart: the price checks of %s are cleared and those pairs are back on the ring", strings.Join(cleared, ", "))
	}
	if ctx.HopWorld == nil {
		b.hopsOff = "this account has no fleet world hooks"
	}
	if b.set.Rotation && ctx.SetHopBudget != nil {
		// The ring plans up to maxHopsPerHour hops; the fleet's default bound (four in thirty
		// minutes) refused almost a quarter of them live.
		n, window := runerunnerplan.FleetHopBudget(b.set.MaxHopsPerHour)
		if err := ctx.SetHopBudget(n, window); err != nil {
			b.logf(ctx, "hop budget not raised: %v", err)
		}
	}
	b.refreshShortage(ctx)
	b.refreshTravel(ctx)
	m, known := b.muleWorld(ctx)
	b.logf(ctx, "start world=%d mule=%s mule_world=%d mule_world_known=%v shops=%s runes=%s caps=%s trip_coins=%d buy_all=%v travel=alch:%v/rub:%v/tele:%v",
		ctx.World(), b.set.Mule, m, known, strings.Join(b.set.ShopList, ","), strings.Join(b.set.RuneList, ","),
		runerunnerplan.FormatCounts(b.set.CapTable), b.set.TripCoins, b.set.BuyAll, b.set.WalkAlch, b.set.WalkRub, b.set.TeleTravel)
	if b.set.Rotation {
		b.logf(ctx, "rotation worlds=%v phase=%d of %d (setting %d) max_hops_per_hour=%d circuit=%s",
			[]int(b.set.Worlds), b.phase(ctx), runerunnerplan.RotationPhases(len(b.set.Worlds)), b.set.RotationPhase,
			b.set.MaxHopsPerHour, pairsString(runerunnerplan.RotationCircuit(b.set.ShopList, b.set.Worlds, b.phase(ctx))))
	} else {
		b.logf(ctx, "classic ring (rotate=%s, shops=%s)", b.set.Rotate, strings.Join(b.set.ShopList, ","))
	}
	if b.st.Swarm && !b.set.SwarmOn() {
		// The phase was entered under settings that no longer ask for it: check at once.
		b.st.SwarmChecked = time.Time{}
	}
	b.logf(ctx, "%s", b.swarmStatus())
	b.logf(ctx, "%s", b.ammoStartLine())
	b.save(ctx)
	return nil
}

// phase is this runner's place on the rotation circuit (rotationPhase, else its name's hash).
func (b *runeRunnerBot) phase(ctx *Context) int {
	return runerunnerplan.ResolvePhase(b.set.RotationPhase, ctx.Username, len(b.set.Worlds))
}

// noteMagic reads the account's CURRENT Magic level (the level the Wizards' Guild door checks)
// into b.magic. An unreported skill leaves the last reading.
func (b *runeRunnerBot) noteMagic(ctx *Context) {
	if ctx == nil || ctx.Session == nil {
		return
	}
	if sk, ok := ctx.Session.State().Skill(fleetplan.MagicStat); ok {
		b.magic = sk.Level
	}
}

// shopsNow is the shops a round is planned over: the shops setting less a site whose door this
// account's current Magic does not open (runerunnerplan.EligibleShops). The Wizards' Guild joins
// the circuit at the first round planned at Magic 66 (wizguild, a user request).
func (b *runeRunnerBot) shopsNow(ctx *Context) []string {
	b.noteMagic(ctx)
	shops := runerunnerplan.EligibleShops(b.set.ShopList, b.magic)
	if len(shops) != len(b.set.ShopList) {
		kept := map[string]bool{}
		for _, k := range shops {
			kept[k] = true
		}
		var left []string
		for _, k := range b.set.ShopList {
			if !kept[k] {
				left = append(left, k)
			}
		}
		if b.said == nil {
			b.said = map[string]time.Time{}
		}
		b.sayEvery(ctx, "magicgate", time.Hour, "Magic %d: %s left out of the round until its door opens (the Wizards' Guild needs %d)",
			b.magic, strings.Join(left, ","), fleetplan.WizardsGuildMagic)
	}
	return shops
}

func pairsString(ps []runerunnerplan.Pair) string {
	keys := make([]string, len(ps))
	for i, p := range ps {
		keys[i] = p.Key()
	}
	return "[" + strings.Join(keys, " ") + "]"
}

func (b *runeRunnerBot) OnStop(ctx *Context) {
	b.stopSwarm(ctx)
	b.save(ctx)
}

func (b *runeRunnerBot) Loop(ctx *Context) error {
	if b.st.Swarm {
		return b.swarmLoop(ctx)
	}
	d := b.deps(ctx)
	b.watchHurt(ctx)

	// A hop the fleet accepted ends this script; until it does, do nothing that could fight it.
	if !b.hopAsked.IsZero() {
		if hopping, known := ctx.WorldHopInProgress(); (known && hopping) || time.Since(b.hopAsked) < runeRunnerHopGrace {
			b.heartbeat(ctx, "hop"+fmt.Sprint(b.hopTo), fleetrunner.Here(d))
			ctx.Delay(time.Second)
			return nil
		}
		b.logf(ctx, "hop to world %d did not happen within %s; carrying on from world %d", b.hopTo, runeRunnerHopGrace, ctx.World())
		b.hopAsked, b.hopTo, b.st.HopPending = time.Time{}, 0, 0
		b.resumed = false
		b.save(ctx)
	}

	if ctx.Session.State().IsDead() {
		b.onDeath(ctx, d)
		return nil
	}
	b.noteMagic(ctx)
	b.travelLog(ctx)
	if !fleetrunner.WaitForGuardian(d) {
		return nil
	}
	here := fleetrunner.Here(d)
	if here == nil {
		b.heartbeat(ctx, "no-tile", nil)
		ctx.Delay(time.Second)
		return nil
	}
	area := runerunnerplan.AreaOf(here)

	// The Mage Bank trip owns the Wilderness and the Mage Bank, and finishes before anything else.
	if b.st.Trip.Active || area == runerunnerplan.AreaWild {
		b.lundailLeg(ctx, d, here, area)
		return nil
	}

	if b.set.StopAfterRounds > 0 && b.st.RoundsCompleted >= b.set.StopAfterRounds {
		b.stop(ctx, fmt.Sprintf("stopAfterRounds %d reached", b.set.StopAfterRounds))
		return nil
	}

	m, _ := b.muleWorld(ctx)
	switch {
	case len(b.st.Ring) == 0:
		// The first round starts where the account stands; a later one (the previous round closed
		// before a restart) starts at the delivery.
		b.newRound(ctx, area, b.st.Round == 0)
	case b.st.Cursor >= len(b.st.Ring):
		// The ring's last station was a hop that ended the previous run of this script.
		b.endRound(ctx)
		return nil
	case b.reenter:
		i, why := b.entry(b.st.Ring, b.st.Rotation, area, ctx.World(), m)
		b.st.JumpTo(i)
		b.logf(ctx, "re-entering the ring at %s: %s", b.st.Ring[i].Key(), why)
	case !b.resumed:
		i, why := runerunnerplan.Resume(b.st, area, ctx.World(), m)
		if i >= len(b.st.Ring) {
			b.endRound(ctx)
			return nil
		}
		if i != b.st.Cursor {
			b.st.JumpTo(i)
		}
		b.verbosef(ctx, "round=%d %s", b.st.Round, why)
	}
	b.reenter, b.resumed = false, true

	s, ok := b.st.Current()
	if !ok {
		b.newRound(ctx, area, false)
		return nil
	}
	b.verbosef(ctx, "station round=%d i=%d/%d %s start", b.st.Round, b.st.Cursor+1, len(b.st.Ring), s.Key())
	b.runStation(ctx, d, s, here)
	return nil
}

// ---- rounds ----

// muleWorld is the mule's world: known from the fleet or the bus, else the last one seen (not
// known), else this account's.
func (b *runeRunnerBot) muleWorld(ctx *Context) (int, bool) {
	if w, known := ctx.MuleWorld(b.set.Mule); known && w > 0 {
		b.st.LastMuleWorld = w
		return w, true
	}
	if b.st.LastMuleWorld > 0 {
		return b.st.LastMuleWorld, false
	}
	return ctx.World(), false
}

// newRound plans a ring and starts it: at the delivery when a round wraps, or where the account
// stands (Entry) on a first start.
func (b *runeRunnerBot) newRound(ctx *Context, area runerunnerplan.Area, fromHere bool) {
	b.refreshShortage(ctx)
	b.refreshTravel(ctx)
	if b.set.Rotation {
		b.newRotationRound(ctx, area, fromHere)
		return
	}
	m, _ := b.muleWorld(ctx)
	o := runerunnerplan.OtherWorld(m, b.set.Worlds)
	plan := runerunnerplan.PlanRound(runerunnerplan.RoundInput{
		M: m, O: o, Shops: b.shopsNow(ctx), MaxHopsPerRound: b.set.MaxHopsPerRound, HopMinGain: b.set.HopMinGain,
		HopsAvailable: b.hopsOff == "", Staffs: b.set.Staffs(), Market: b.set.MarketBuy,
		Worth: func(site string, world int) int { return b.worth(ctx, site, world) },
	})
	cursor := 0
	why := "the delivery"
	if fromHere {
		cursor, why = runerunnerplan.Entry(plan.Ring, area, ctx.World(), m)
	}
	b.st.StartRound(plan.Ring, cursor, time.Now())
	b.st.Rotation = false
	b.logf(ctx, "round=%d plan=%s start=%s (%s) o_block=%v (%s)", b.st.Round, runerunnerplan.PlanString(plan.Ring, 0),
		plan.Ring[cursor].Key(), why, plan.OBlock, plan.Why)
	b.save(ctx)
}

// newRotationRound plans a rotation round (runerunnerplan.BuildRotationRing) for this runner's
// phase and starts it: at the delivery, or where the account stands on a first start.
func (b *runeRunnerBot) newRotationRound(ctx *Context, area runerunnerplan.Area, fromHere bool) {
	m, _ := b.muleWorld(ctx)
	shops := b.shopsNow(ctx)
	placed, left := b.set.AmmoPlaced(shops)
	if len(left) > 0 {
		b.sayEvery(ctx, "ammoleft", time.Hour, "ammo: left out of the round: %s", strings.Join(left, "; "))
	}
	ring := runerunnerplan.BuildRotationRing(runerunnerplan.RotationInput{
		M: m, Worlds: b.set.Worlds, Shops: shops, Phase: b.phase(ctx), Staffs: b.set.Staffs(), Market: b.set.MarketBuy,
		Ammo: placed, AmmoEveryWorld: b.set.AmmoEveryWorld,
	})
	cursor, why := 0, "the delivery"
	if fromHere {
		cursor, why = runerunnerplan.RotationEntry(ring, area, ctx.World())
	}
	b.st.StartRound(ring, cursor, time.Now())
	b.st.Rotation = true
	b.logf(ctx, "round=%d rotation phase=%d plan=%s start=%s (%s)", b.st.Round, b.phase(ctx),
		runerunnerplan.PlanString(ring, 0), ring[cursor].Key(), why)
	b.save(ctx)
}

// entry is where a round picks up from a standing start (after a death): RotationEntry for a
// rotation round, Entry for the classic ring.
func (b *runeRunnerBot) entry(ring []runerunnerplan.Station, rotation bool, area runerunnerplan.Area, world, m int) (int, string) {
	if rotation {
		return runerunnerplan.RotationEntry(ring, area, world)
	}
	return runerunnerplan.Entry(ring, area, world, m)
}

// finish records the station at the cursor and moves on; a finished ring ends the round.
func (b *runeRunnerBot) finish(ctx *Context, r runerunnerplan.StationResult) {
	if r.At.IsZero() {
		r.At = time.Now()
	}
	s, _ := b.st.Current()
	if r.Outcome == runerunnerplan.OutcomeSkipped || r.Outcome == runerunnerplan.OutcomeWalkFailed || r.Outcome == runerunnerplan.OutcomeAborted {
		b.logf(ctx, "skip station=%s outcome=%s reason=%q", s.Key(), r.Outcome, r.Reason)
	}
	if runerunnerplan.IsShopStation(s) {
		// A shop station ends with this runner's holds there released, whatever it did: a rotation
		// hop holds the shelf before the station runs, and a skip must not leave it held.
		b.releaseRotationHolds(ctx, []runerunnerplan.Station{s})
	}
	b.quietSince = time.Time{}
	if b.st.Finish(r) {
		b.endRound(ctx)
		return
	}
	b.save(ctx)
}

// endRound closes the round: its log line and the allow-list hint, then -- when the round bought
// and delivered nothing -- a wait for the shelves to regrow, and the next round planned from the
// delivery. The closed round is saved as an empty ring before the wait, so a restart during it
// plans the next round rather than closing this one twice.
func (b *runeRunnerBot) endRound(ctx *Context) {
	b.st.RoundsCompleted++
	bought, spent, delivered := b.st.RoundTotals()
	minutes := int(time.Since(b.st.RoundStarted) / time.Minute)
	b.logf(ctx, "round=%d done minutes=%d bought=%s spent=%d delivered=%s totals=%s spent_total=%d delivered_total=%s",
		b.st.Round, minutes, runerunnerplan.FormatCounts(bought), spent, runerunnerplan.FormatCounts(delivered),
		runerunnerplan.FormatCounts(b.st.Bought), b.st.Spent, runerunnerplan.FormatCounts(b.st.Delivered))
	pack := fleetrunner.CoinsHeld(b.deps(ctx))
	b.logf(ctx, "%s", b.roundPurse(pack))
	if line := b.roundShortLine(pack); line != "" {
		b.logf(ctx, "%s", line)
	}
	if runerunnerplan.AllowListHint(b.st.DeliverRefusals) {
		b.logf(ctx, "ALLOWLIST? %s refused %d trades in a row: is this account on %s's allowedPartners?",
			b.set.Mule, b.st.DeliverRefusals, b.set.Mule)
	}
	productive, failures := b.roundProductivity()
	b.ammoRoundLine(ctx)
	stopDue := b.set.StopAfterRounds > 0 && b.st.RoundsCompleted >= b.set.StopAfterRounds
	if !stopDue && b.afterRoundSwarm(ctx) {
		return
	}
	var wait time.Duration
	if !productive && !stopDue {
		// A round with failures waits at least FailureIdle: a shelf nobody reached is still
		// estimated full, and a buyer whose walks fail fast must not lap the ring (and hop) every
		// minute.
		wait = runerunnerplan.RoundIdle(b.idleWait(ctx), time.Duration(b.set.IdleMaxMinutes)*time.Minute, failures)
	}
	b.st.Ring, b.st.Cursor = nil, 0
	b.save(ctx)
	if stopDue {
		return
	}
	if wait > 0 {
		b.idle(ctx, wait)
	}
	d := b.deps(ctx)
	b.newRound(ctx, runerunnerplan.AreaOf(fleetrunner.Here(d)), false)
}

// roundPurse is the finished round's coin account (the `purse round=...` line): what the mule gave, what the
// stations paid shops (the ammo stations' share apart), and what stays in the bank and the pack for the next round.
func (b *runeRunnerBot) roundPurse(pack int) runerunnerplan.RoundPurse {
	_, spent, _ := b.st.RoundTotals()
	return runerunnerplan.RoundPurse{
		Round: b.st.Round, Received: b.st.RoundTopUp, Spent: spent, Ammo: runerunnerplan.AmmoSpentThisRound(b.st.Ring, b.st.Done),
		Bank: b.st.BankCoins, Pack: pack,
	}
}

// roundProductivity reads the finished round: whether it bought or delivered anything, and how many stations
// failed. Ammo stops are left out of both: they never make a round productive and a failed ammo stop never
// stretches the idle wait (the feathers regrow in ten minutes and would keep the runner circuiting for nothing).
func (b *runeRunnerBot) roundProductivity() (productive bool, failures int) {
	ammoKeys := map[string]bool{}
	for _, s := range b.st.Ring {
		if runerunnerplan.IsAmmoStation(s) {
			ammoKeys[s.Key()] = true
		}
	}
	for key, r := range b.st.Done {
		if ammoKeys[key] {
			continue
		}
		if r.Outcome == runerunnerplan.OutcomeBought || r.Outcome == runerunnerplan.OutcomeDelivered {
			productive = true
		}
		if runerunnerplan.Failed(r.Outcome) {
			failures++
		}
	}
	return productive, failures
}

// roundShortLine is the round's COINS SHORT line, "" when coins left nothing undone: the stations that
// skipped for coins, and the runes (and their cost at the shelf prices) the visits left on a shelf for
// coins. With the purse sized for the whole round this line means the fleet had no more coins to give.
func (b *runeRunnerBot) roundShortLine(packCoins int) string {
	stations, runes, coins := b.st.RoundShort()
	if len(stations) == 0 && runes == 0 {
		return ""
	}
	return fmt.Sprintf("round=%d COINS SHORT: stations_skipped_for_coins=%s runes_left_for_coins=%d (about %d coins at the shelf prices) bank=%d pack=%d topup_received_total=%d -- the fleet's coins did not cover every rune this round",
		b.st.Round, strings.Join(stations, ","), runes, coins, b.st.BankCoins, packCoins, b.st.TopUpReceived)
}

// idleWait is how long to wait after a round that did nothing: min(idleMaxMinutes, the soonest
// any of the round's shop stations would be worth a visit), or idleMaxMinutes with no coins.
func (b *runeRunnerBot) idleWait(ctx *Context) time.Duration {
	maxWait := time.Duration(b.set.IdleMaxMinutes) * time.Minute
	if b.noCoins {
		return maxWait
	}
	var lines []runerunnerplan.WaitLine
	now := time.Now()
	for _, s := range b.st.Ring {
		if s.Kind != runerunnerplan.KindBuy && s.Kind != runerunnerplan.KindLundail {
			continue
		}
		// A pair that cannot be visited however full its shelf grows (off the ring, or Lundail
		// stopped by its deaths) does not time the wait. Live 3 Oct: a Lundail-only runner with
		// Lundail stopped for 12 hours by lundailMaxDeaths re-planned its round every minute,
		// 1,533 rounds, because the stopped shelf's regrowth said "one minute".
		if off, _ := b.pairGated(s.Site, s.World, now); off {
			continue
		}
		site, _ := runerunnerplan.Site(s.Site)
		est := b.estimate(ctx, site, s.World, now)
		for _, r := range b.set.RuneList {
			if site.Shop.Sells(r) {
				lines = append(lines, runerunnerplan.WaitLine{Shop: site.Shop, Rune: r, Est: est[r], Cap: b.set.CapTable.For(r)})
			}
		}
	}
	return runerunnerplan.IdleWait(maxWait, lines, b.set.MinVisitRunes, b.set.Tick())
}

// idle waits where the account stands, with a heartbeat.
func (b *runeRunnerBot) idle(ctx *Context, wait time.Duration) {
	ctx.SetPhase(phaselog.Idle)
	b.logf(ctx, "idle: round %d bought and delivered nothing; waiting %s (no_coins=%v)", b.st.Round, wait.Round(time.Second), b.noCoins)
	d := b.deps(ctx)
	until := time.Now().Add(wait)
	for time.Now().Before(until) {
		b.heartbeat(ctx, "idle", fleetrunner.Here(d))
		if !ctx.Delay(5 * time.Second) {
			return
		}
	}
}

// ---- estimates ----

// estimate is the board's stock for each listed rune a site sells on one world: the freshest
// reading projected by the content's restock rule, less every other account's live hold, and
// the base when nobody has looked.
func (b *runeRunnerBot) estimate(ctx *Context, site runerunnerplan.ShopSite, world int, now time.Time) map[string]int {
	board := fleetrunner.ShopsFor(world)
	out := map[string]int{}
	for _, r := range b.set.RuneList {
		if site.Shop.Sells(r) {
			out[r] = board.Available(site.Shop, r, ctx.Username, now, b.set.Tick())
		}
	}
	return out
}

// worth is a station's expected purchase under the caps, 0 for a pair that is off the ring.
func (b *runeRunnerBot) worth(ctx *Context, siteKey string, world int) int {
	site, ok := runerunnerplan.Site(siteKey)
	if !ok || !b.set.Enabled(siteKey) {
		return 0
	}
	now := time.Now()
	if off, _ := b.pairGated(siteKey, world, now); off {
		return 0
	}
	return runerunnerplan.Worth(site.Shop, b.estimate(ctx, site, world, now), b.set.RuneList, b.set.CapTable)
}

// pairGated reports whether a shop and world pair cannot be visited now whatever its stock: off
// the ring (strikes, the price check), or Lundail stopped (no blade, lundailMaxDeaths).
func (b *runeRunnerBot) pairGated(siteKey string, world int, now time.Time) (bool, string) {
	site, ok := runerunnerplan.Site(siteKey)
	if !ok {
		return true, "unknown shop " + siteKey
	}
	if off, why := b.st.PairOff(fmt.Sprintf("%s@%d", site.Key, world), now); off {
		return true, why
	}
	if !runerunnerplan.Eligible(site, b.magic) {
		return true, fmt.Sprintf("%s needs Magic %d (this account has %d)", site.Key, site.MinMagic, b.magic)
	}
	if site.Wilderness {
		if b.lundailOff != "" {
			return true, "Lundail is off for this run: " + b.lundailOff
		}
		if ok, why := runerunnerplan.LundailAllowed(b.st.LundailDeaths, now, b.set.LundailMaxDeaths); !ok {
			return true, why
		}
	}
	return false, ""
}

// ---- the round's purse ----

// roundNeed is the coins the stations from ring index `from` on are expected to need
// (runerunnerplan.PlanRoundCoins): every pack-paid shop, Lundail (paid from the bank at Gundai) and Zaff,
// each valued at the stock the board expects when the round REACHES it, with a margin. The draw station
// sizes the purse and the mule ask from it, the shop stations their bank draws, the market station the
// float it leaves alone. The ammo stations ahead are valued too (RoundNeed.Ammo) but sit BESIDE the need, not in
// PackNeed/Est/Total: they are lowest priority and are funded from surplus only (ammoHold, bankDraw).
func (b *runeRunnerBot) roundNeed(ctx *Context, from int) runerunnerplan.RoundNeed {
	if b.roundNeedSeam != nil {
		return b.roundNeedSeam(from)
	}
	now := time.Now()
	staff := 0
	if b.set.Staffs() {
		staff = runerunnerplan.StaffEstCost(b.set.ZaffStaffs, b.set.ZaffStaffCap) +
			runerunnerplan.ZaffEstCost(runerunnerplan.Battlestaff, b.set.ZaffBattlestaffs, b.set.ZaffBattlestaffCap) +
			runerunnerplan.ZaffEstCost(runerunnerplan.StaffOfFire, b.set.ZaffFireStaffs, b.set.ZaffFireStaffCap)
	}
	in := runerunnerplan.PurseInput{
		Ring: b.st.Ring, From: from, Now: now, Runes: b.set.RuneList, Caps: b.set.CapTable,
		Fares: b.set.FareCoins, StaffCost: staff,
		Skip: func(s runerunnerplan.Station) bool { return b.roundSkips(s, now) },
		Stock: func(site runerunnerplan.ShopSite, world int, at time.Time) map[string]int {
			return b.estimateAt(site, world, at)
		},
	}
	if b.set.AmmoOn() {
		// The ammo stations still ahead, each valued off its shelf when the round reaches it and bounded by
		// what the round's ammoCoins budget has left (none counted once it is spent). RoundNeed.Ammo is outside
		// the need the purse is sized from.
		if limit, ok := runerunnerplan.AmmoLeft(b.set.AmmoCoins, runerunnerplan.AmmoSpentThisRound(b.st.Ring, b.st.Done)); ok {
			in.AmmoLimit = limit
			in.AmmoCost = func(s runerunnerplan.Station, at time.Time) int { return b.ammoStationCost(ctx, s, at) }
		}
	}
	return runerunnerplan.PlanRoundCoins(in)
}

// roundSkips reports a station the round cannot spend coins at: a pair off the ring, a door this
// account's Magic does not open, Lundail stopped by its deaths, a Zaff station struck off.
func (b *runeRunnerBot) roundSkips(s runerunnerplan.Station, now time.Time) bool {
	if s.Kind == runerunnerplan.KindStaff || s.Kind == runerunnerplan.KindAmmo {
		off, _ := b.st.PairOff(s.Key(), now)
		return off
	}
	off, _ := b.pairGated(s.Site, s.World, now)
	return off
}

// estimateAt is the board's stock for each listed rune a site sells on one world, projected to `at`
// by the content's restock rule: the freshest sighting any account made, an upper bound, or the base
// when nobody has looked. Unlike estimate it ignores other accounts' holds (a hold is a plan, not
// coins spent), so it never makes a purse smaller than the shelf can cost.
func (b *runeRunnerBot) estimateAt(site runerunnerplan.ShopSite, world int, at time.Time) map[string]int {
	board := fleetrunner.ShopsFor(world)
	out := map[string]int{}
	for _, r := range b.set.RuneList {
		if site.Shop.Sells(r) {
			out[r] = board.Estimate(site.Shop, r, at, b.set.Tick())
		}
	}
	return out
}

// aheadEstimates are the pack-paid shop stations after the cursor, with their estimates: what the
// east leg's coins must cover (EstCost) and what a visit must leave funded (ReserveAhead).
func (b *runeRunnerBot) aheadEstimates(ctx *Context, from int) []runerunnerplan.StationEstimate {
	now := time.Now()
	var out []runerunnerplan.StationEstimate
	for i := from; i < len(b.st.Ring); i++ {
		s := b.st.Ring[i]
		if s.Kind != runerunnerplan.KindBuy {
			continue
		}
		if off, _ := b.st.PairOff(s.Key(), now); off {
			continue
		}
		site, _ := runerunnerplan.Site(s.Site)
		out = append(out, runerunnerplan.StationEstimate{Shop: site.Shop, Stock: b.estimate(ctx, site, s.World, now)})
	}
	return out
}

// ---- state ----

func stationKeyAt(st runerunnerplan.State) string {
	if s, ok := st.Current(); ok {
		return s.Key()
	}
	return "the start of a round"
}

// loadState reads the account's state leniently: fields a newer build wrote are kept and written
// back; a file this build cannot read refuses the start and is never overwritten.
func (b *runeRunnerBot) loadState(ctx *Context) error {
	b.st = runerunnerplan.NewState()
	b.keep, b.blocked = settings.Unknown{}, ""
	if ctx.Username == "" {
		return nil
	}
	path, err := b.store.Path(ctx.Username, runerunnerplan.StateStoreID)
	if err != nil {
		b.logf(ctx, "state: %v — starting fresh", err)
		return nil
	}
	bag, err := b.store.Load(ctx.Username, runerunnerplan.StateStoreID)
	if err == nil {
		if len(bag.Raw()) == 0 {
			return nil
		}
		var loaded runerunnerplan.State
		keep, decErr := bag.DecodeCompat(&loaded)
		if decErr == nil {
			loaded.Normalise()
			b.st, b.keep = loaded, keep
			if !keep.Empty() {
				b.logf(ctx, "state %s carries %d field(s) this build does not know (%s); kept and written back", path, len(keep.Paths()), strings.Join(keep.Paths(), ", "))
			}
			return nil
		}
		err = decErr
	}
	b.blocked = path
	return fmt.Errorf("runerunner: the saved state %s cannot be read by this build (%v) — refusing to start rather than overwrite it; move the file aside to start fresh", path, err)
}

// save writes the state. A state file that could not be read at start is never overwritten.
func (b *runeRunnerBot) save(ctx *Context) {
	if b.store == nil || ctx.Username == "" || b.blocked != "" {
		return
	}
	bag := settings.NewBag()
	if err := bag.EncodeCompat(b.st, b.keep); err != nil {
		b.logf(ctx, "state: encode: %v", err)
		return
	}
	if err := b.store.Save(ctx.Username, runerunnerplan.StateStoreID, bag); err != nil {
		b.sayEvery(ctx, "save", 10*time.Minute, "state: save: %v", err)
	}
}

// ---- hitpoints, death ----

// watchHurt remembers the last moment the account was in combat or lost hitpoints (MayHop's
// "quiet" rule).
func (b *runeRunnerBot) watchHurt(ctx *Context) {
	st := ctx.Session.State()
	hp, ok := st.HP()
	if st.InCombat() || (ok && b.lastHP > 0 && hp < b.lastHP) {
		b.lastHurt = time.Now()
	}
	if ok {
		b.lastHP = hp
	}
}

// hopCheck is MayHop's input now.
func (b *runeRunnerBot) hopCheck(ctx *Context, here *tile.Tile) runerunnerplan.HopCheck {
	st := ctx.Session.State()
	hp, _ := st.HP()
	maxHP, _ := st.MaxHP()
	hopping, _ := ctx.WorldHopInProgress()
	return runerunnerplan.HopCheck{
		Dead: st.IsDead(), HP: hp, MaxHP: maxHP, InCombat: st.InCombat(), LastHurt: b.lastHurt, Now: time.Now(),
		RandomEvent: randomevents.Busy(ctx.Session), Here: here, TripActive: b.st.Trip.Active,
		LoginBusy: ctx.FleetLoginBusy(), HopInProgress: hopping,
	}
}

// onDeath records a death, ends any trip (a Lundail death counts against lundailMaxDeaths), and
// has the next Loop pick the ring up again from the respawn.
func (b *runeRunnerBot) onDeath(ctx *Context, d *fleetrunner.Deps) {
	now := time.Now()
	here := fleetrunner.Here(d)
	x, z, wild := 0, 0, 0
	if here != nil {
		x, z = here.X, here.Z
		wild = fleetplan.WildernessLevel(*here)
	}
	onTrip := b.st.Trip.Active || wild > 0
	if onTrip {
		b.st.NoteLundailDeath(now)
	}
	b.logf(ctx, "DIED station=%s world=%d tile=%d,%d wild=%d lundail_deaths_24h=%d",
		stationKeyAt(b.st), ctx.World(), x, z, wild, len(runerunnerplan.PruneDeaths(b.st.LundailDeaths, now)))
	if onTrip {
		if r := b.st.EndTrip(now, "died on the trip"); b.stationIsLundail() {
			b.st.Finish(r)
		}
	}
	ctx.Session.State().ClearDead()
	b.reenter = true
	b.save(ctx)
	ctx.Delay(5 * time.Second) // the respawn
}

func (b *runeRunnerBot) stationIsLundail() bool {
	s, ok := b.st.Current()
	return ok && s.Kind == runerunnerplan.KindLundail
}
