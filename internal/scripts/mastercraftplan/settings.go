package mastercraftplan

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/shopplan"
)

// Setting keys. Every key this script reads starts with "craft": the rest of the settings map is
// ScarabSwarm's (the script runs it between rounds and hands it the same map), so only craft*
// keys are decoded here, and an unknown craft* key is an error -- a typo must not silently leave
// a default in place.
const (
	KeyGemCutting     = "craftGemCutting"
	KeyBowStringing   = "craftBowStringing"
	KeyGemThreshold   = "craftGemThreshold"
	KeyBowThreshold   = "craftBowThreshold"
	KeyCheckEvery     = "craftCheckEveryMinutes"
	KeyGems           = "craftGems"
	KeyBows           = "craftBows"
	KeyKitSize        = "craftKitSize"
	KeyMaxRound       = "craftMaxRoundMinutes"
	KeySafeHp         = "craftSafeHpPercent"
	KeyMembersWorld   = "craftMembersWorld"
	KeyFruitlessPause = "craftFruitlessPauseMinutes"
	KeyBackoff        = "craftBackoffMinutes"
	KeyBackoffMax     = "craftBackoffMaxMinutes"
	KeyMaxHandover    = "craftMaxHandoverFailures"
	KeyBulkSapphires  = "craftBulkAtSapphires"
	KeyPearls         = "craftPearls"
	KeyPearlThreshold = "craftPearlThreshold"
	KeyBoltThreshold  = "craftBoltThreshold"
	KeyFastGems       = "craftFastGems"
	KeyFastPearls     = "craftFastPearls"
	KeyFastBows       = "craftFastBows"
	// The shop trip (supply.go), the superheat round (superheat.go), the arrow round (arrows.go) and
	// the herb round (herbs.go).
	KeyBuyBolts           = "craftBuyBolts"
	KeyBuyArrowTips       = "craftBuyArrowTips"
	KeyShops              = "craftShops"
	KeyShopMaxPrice       = "craftShopMaxPrice"
	KeyShopMinShelf       = "craftShopMinShelfPercent"
	KeyShopMinWaiting     = "craftShopMinWaiting"
	KeyShopBuyBelow       = "craftShopBuyBelow"
	KeySuperheat          = "craftSuperheat"
	KeySuperheatThreshold = "craftSuperheatThreshold"
	KeySuperheatBars      = "craftSuperheatBars"
	KeySuperheatGold      = "craftSuperheatGold"
	KeySuperheatLowBars   = "craftSuperheatLowBars"
	// The anvil round (smith.go).
	KeySmith             = "craftSmith"
	KeySmithMinSteelBars = "craftSmithMinSteelBars"
	KeySmithArrowtips    = "craftSmithArrowtips"
	KeySmithIronBodies   = "craftSmithIronPlatebodies"
	KeySmithKit          = "craftSmithKit"
	KeyBuyFireStaff      = "craftBuyFireStaff"
	KeyFireStaffMaxPrice = "craftFireStaffMaxPrice"
	KeyArrows            = "craftArrows"
	KeyArrowThreshold    = "craftArrowThreshold"
	KeyArrowKit          = "craftArrowKit"
	KeyHerbs             = "craftHerbs"
	KeyHerbThreshold     = "craftHerbGuamThreshold"
	// The storage round (store.go): a nearly full mule bank sends its cold, small rows to this
	// account's own bank.
	KeyStoreOverflow  = "craftStoreOverflow"
	KeyStoreAtRows    = "craftStoreAtRows"
	KeyStoreToRows    = "craftStoreToRows"
	KeyStoreMaxCount  = "craftStoreMaxCount"
	KeyStoreQuietDays = "craftStoreQuietDays"
	// KeyIdle is what the account does between rounds: IdlePost or IdleSwarm.
	KeyIdle = "craftIdle"
	// KeyPrefix is what marks a key as this script's.
	KeyPrefix = "craft"
)

// Defaults. Each is the safe side: a round is only started when the mule holds a lot, so the walk
// and the queue behind it are worth it, and an hour-scale pause follows a round that produced
// nothing so a mule that cannot fund it is not asked again every check.
const (
	// DefaultGemThreshold is the uncut gems this account can cut, its own bank and the mule's
	// together, that start a gem round (a user request: "we should have a total of 20k gems for
	// that to trigger"; 500 in the mule's bank before); DefaultBowThreshold the bow-and-string pairs
	// the same way (owner: "20k unstrung bows and 20k bow string").
	DefaultGemThreshold  = 20000
	DefaultBowThreshold  = 20000
	DefaultCheckEvery    = 20 * time.Minute
	DefaultKitSize       = 1000
	DefaultMaxRound      = 120 * time.Minute
	DefaultSafeHpPercent = 80
	DefaultFruitless     = 180 * time.Minute
	// DefaultBackoff is the wait after the first round that failed for a passing reason; each
	// further failure in a row doubles it up to DefaultBackoffMax.
	DefaultBackoff    = 30 * time.Minute
	DefaultBackoffMax = 180 * time.Minute
	// DefaultMaxHandover is how many hand-overs to the mule in a row may fail before every round is
	// suspended (the products stay in the bank) and the log says so loudly.
	DefaultMaxHandover = 3
	// DefaultBulkSapphires is the mule's uncut sapphire stock at which a gem round is due whatever
	// GemThreshold says, and asks for every uncut gem the mule holds instead of KitSize of each
	// (a user request: "when the mule has over 50,000 uncut sapphires ... request all uncuts the
	// mule has"; 13 Oct 2026: "gemcutting loop can trigger at 10k uncut sapphires too, cutting all
	// uncut gems the mule has").
	DefaultBulkSapphires = 20000
	// DefaultPearlThreshold is the oyster pearls (both kinds together), this account's bank and the
	// mule's together, before a pearl round cuts them (a user request: "20k oyster pearls must be
	// in the mule's or the main account's bank before taking that on"; 100,000 in the mule's alone before), and
	// DefaultBoltThreshold the Bolts, both banks together, before one puts Pearl bolttips on them
	// (owner: "20,000 bolts for pearl bolt making").
	DefaultPearlThreshold = 20000
	DefaultBoltThreshold  = 100000
	// DefaultShopMaxPrice is the most a shop trip pays for one unit: 18 gp is the most a sell-1000
	// keeper ever charges for a 3 gp Bolts (shopplan.UnitPrice's haggle clamp), so the default buys
	// Hickton and Lowe out; Pearl bolts are worth far more (cost 110 in content).
	DefaultShopMaxPrice = 18
	// DefaultShopMinShelf is how full a shelf must be (estimated, percent of its baseline) before a
	// trip walks to it.
	DefaultShopMinShelf = 50
	// DefaultShopMinWaiting and DefaultShopBuyBelow: a trip runs when this account's bank holds at
	// least DefaultShopMinWaiting Pearl bolttips (or headless arrows) and fewer than
	// DefaultShopBuyBelow Bolts (or Steel arrowtips) are held by it and the mule together.
	DefaultShopMinWaiting = 1000
	DefaultShopBuyBelow   = 1000
	// DefaultSmithMinSteelBars is the Steel bars, this account's bank and the mule's together, that must be
	// MORE than held to START an anvil round (a user request: "must have over 8000 steel bars to trigger
	// it"); DefaultSmithKit the most of one bar a kit line asks the mule for.
	DefaultSmithMinSteelBars = 8000
	DefaultSmithKit          = 100000
	// DefaultSuperheatThreshold is the bars' worth of ore and runes that starts a superheat round.
	DefaultSuperheatThreshold = 500
	// DefaultArrowThreshold is the arrows' worth of parts, this account's bank and the mule's
	// together, that starts an arrow round (a user request: "at least 20,000 arrow shaft for
	// arrows"; 1,000 before), and DefaultArrowKit the most of one arrow part a kit line asks for
	// (they all stack).
	DefaultArrowThreshold = 100000
	DefaultArrowKit       = 100000
	// DefaultHerbThreshold is the grimy guams the mule must hold before a herb round (owner, 4 Oct
	// 2026: "once the mule has 1000 grimy guams").
	DefaultHerbThreshold = 1000
	// DefaultFireStaffMaxPrice is the most a Staff of fire may cost at the Seers market (Zaff's own
	// price is 1500: shopplan cost 1500 at baseline 2).
	DefaultFireStaffMaxPrice = 5000
	// The storage round (store.go). DefaultStoreAtRows is the rows in the mule's published bank at
	// which it starts: ScarabSwarm's loot hand-off stops giving the mule new item types at 240 rows
	// (288 less consolidateMuleBankReserve 48), so 235 starts it while a few types still fit.
	// DefaultStoreToRows is where it stops (it moves a bank down to there, so it does not run again
	// for the next new type); DefaultStoreMaxCount the largest row it moves, in items;
	// DefaultStoreQuietDays how long an item must have gone unasked-for.
	DefaultStoreAtRows    = 235
	DefaultStoreToRows    = 215
	DefaultStoreMaxCount  = 50
	DefaultStoreQuietDays = 7
)

// DefaultShops are the supply shops a trip visits, nearest the mule first: Hickton (Catherby) and
// Lowe (Varrock). The camp store ("Lathas") needs Biohazard and is left out.
func DefaultShops() []string { return []string{"Hickton", "Lowe"} }

// Settings are the MasterCrafter script's own settings.
type Settings struct {
	// GemCutting, BowStringing and Pearls switch each kind of round on.
	GemCutting, BowStringing, Pearls bool
	// SkillGoal holds a training round off once every skill it trains is at this level (0: off); PastGoal names the
	// rounds that run past it anyway (goal.go).
	SkillGoal int
	PastGoal  []string
	// FastGems cuts gems six ticks a load, the stream sent ahead without a reply waited for
	// (mastercraftplan/fast.go, mastercraftrunner.FastGems); off, every load is the 7-tick quick one.
	FastGems bool
	// FastPearls cuts oyster pearls into bolt tips on the same six-tick stream.
	FastPearls bool
	// BuyBolts and BuyArrowTips switch the shop trip's two purchases on (supply.go).
	BuyBolts, BuyArrowTips bool
	// ShopNames are the supply shops a trip may visit, in order (shopplan.SupplyShopByName).
	ShopNames []string
	// ShopMaxPrice is the most one unit may cost; ShopMinShelfPercent how full a shelf must be
	// estimated before a trip walks to it.
	ShopMaxPrice, ShopMinShelfPercent int
	// ShopMinWaiting and ShopBuyBelow are the trip's trigger: this account's bank holds at least
	// ShopMinWaiting tips (or headless arrows) and fewer than ShopBuyBelow Bolts (or Steel
	// arrowtips) are held by it and the mule together.
	ShopMinWaiting, ShopBuyBelow int
	// Superheat switches the superheat round on; SuperheatThreshold is the bars' worth of ore and
	// runes that starts one; SuperheatBars restricts the bars (empty: every bar Smithing allows);
	// SuperheatGold allows gold ore (off: the owner keeps it for the goldsmith gauntlets).
	Superheat          bool
	SuperheatThreshold int
	SuperheatBars      []string
	SuperheatGold      bool
	// GauntletsHeld is a runtime reading, NOT a settings key: whether the Goldsmith gauntlets are worn, in
	// the pack or in this account's bank. SuperheatGold alone no longer puts Gold bars in a plan (owner,
	// 5 Nov 2026: "we should only superheat gold once we have the gold smith gauntlets"); the caller sets
	// this from what it sees (Snapshot.GoldGauntlets, mastercraftrunner.GauntletState).
	GauntletsHeld bool
	// SuperheatLowBars allows Bronze, Iron and Silver bars (off: Steel and up only, a user request).
	SuperheatLowBars bool
	// Smith switches the anvil round on (smith.go); SmithMinSteelBars is the Steel bars (both banks together)
	// that must be exceeded to START one (it then works through every bar it can); SmithArrowtips and
	// SmithIronPlatebodies switch its two products on; SmithKit is the most of one bar a kit line asks for.
	Smith                bool
	SmithMinSteelBars    int
	SmithArrowtips       bool
	SmithIronPlatebodies bool
	SmithKit             int
	// CannonballTarget is the cannonballs the fleet should hold in stock (the mule's bank and this account's; 0 turns the round
	// off); CannonballBatch the most Steel bars one round smelts; CannonballMinBars the bars the two banks must hold to start one
	// (cannonballs.go).
	CannonballTarget, CannonballBatch, CannonballMinBars int
	// CannonRun is the cannon run round's settings (cannonrun.go): the owner's Ranged round with a Dwarf multicannon at the Khazard
	// battlefield, triggered at 10,000 available Cannonballs and run until 10,000 are used.
	CannonRun CannonRunSettings
	// SuperheatRound and PlatebodyRound are the minutes a Mithril superheat batch and a platebody round may last, and
	// RoundLimitPause how long a mode that reached its limit is paused (roundlimit.go; 0 is none).
	SuperheatRound, PlatebodyRound, RoundLimitPause time.Duration
	// Platebody switches the platebody round on; PlatebodyMinBars the bars (both banks) that start one; PlatebodyKit the bars
	// one round asks the mule for (platebody.go).
	Platebody bool
	// Staves switches the Fire battlestaff round on; StavesMinStaves and StavesMinOrbs are the Battlestaff and Fire orb the mule
	// must hold to start one (staves.go).
	Staves bool
	// Bones switches the bone-burying round on (bones.go; the stock, length and pause are in Sched).
	Bones                          bool
	StavesMinStaves, StavesMinOrbs int
	PlatebodyMinBars, PlatebodyKit int
	// BuyFireStaff buys a Staff of fire (the Seers market, else Zaff in Varrock) when neither this
	// account nor the mule has one; FireStaffMaxPrice caps the market's price.
	BuyFireStaff      bool
	FireStaffMaxPrice int
	// Arrows switches the steel arrow round on; ArrowThreshold is the arrows' worth of parts, this
	// account's bank and the mule's together, that starts one; ArrowKit the most of one part a kit
	// line asks for.
	Arrows                   bool
	ArrowThreshold, ArrowKit int
	// Herbs switches the herb round on; HerbThreshold is the grimy guams the mule must hold.
	Herbs         bool
	HerbThreshold int
	// Trawler switches the trawler round on (trawler.go, off by default); TrawlerQuorum is the crew that must wait in the
	// Port Khazard lobby for a round to start.
	Trawler       bool
	TrawlerQuorum int
	// TrawlerFoodLow and TrawlerFoodHigh are the food schedule's marks (trawler.go, THE FOOD SCHEDULE); a low mark of 0 is off.
	TrawlerFoodLow, TrawlerFoodHigh int
	// StoreOverflow switches the storage round on (store.go); StoreAtRows is the rows the mule's
	// published bank must hold before it starts and StoreToRows the rows it moves the bank down to;
	// StoreMaxCount is the most items a row may hold and still be moved; StoreQuietDays the days an
	// item must have gone without being handed out.
	StoreOverflow                 bool
	StoreAtRows, StoreToRows      int
	StoreMaxCount, StoreQuietDays int
	// Idle is what the account does between rounds (craftIdle): IdleSwarm runs ScarabSwarm, whose
	// Stage 3 walks to Ardougne to train Magic and back for every round (a user request: "the main account
	// has wandered all the way to ardougne"); IdlePost waits at the mule's post. ScarabSwarm is the
	// default again since the thresholds went to 20,000 and rounds hours apart (a user request:
	// "the main account is just standing there doing nothing").
	Idle string
	// FastBows strings bows four ticks a load of 14 (mastercraftrunner.FastBows) while the bank holds
	// a load; off, BowStringer strings them five a tick between bank visits.
	FastBows bool
	// PearlThreshold is how many oyster pearls (Oyster pearl and Oyster pearls together) this
	// account's bank and the mule's must hold together before a pearl round cuts them;
	// BoltThreshold the Bolts, both banks together, before one tips Pearl bolttips onto them.
	PearlThreshold, BoltThreshold int
	// GemThreshold is how many uncut gems of kinds this account can cut its bank and the mule's
	// must hold together before a gem round starts; BowThreshold the same for bow-and-string pairs.
	GemThreshold, BowThreshold int
	// CheckEvery is how often ScarabSwarm's run is interrupted to read the mule's published bank.
	CheckEvery time.Duration
	// GemKinds restricts the gems cut (cut display names, as GemCutter's "gems" setting); empty is
	// every gem the Crafting level allows.
	GemKinds []string
	// BowNames are the bows strung (BowStringer's "bows" setting).
	BowNames []string
	// KitSize is the most of one line a kit order asks for (1..bowstringplan.KitMax: the mule
	// clamps an order of an uncatalogued item to 1000 anyway).
	KitSize int
	// BulkAtSapphires, when above 0, is the mule's uncut sapphire stock from which a gem round is
	// due (as well as at GemThreshold) and asks for the mule's whole stock of every gem it can cut
	// (the mule's own anyMaxPerOrder still caps each line).
	BulkAtSapphires int
	// CrystalChest switches the crystal chest round on (chest.go, KeyCrystalChest).
	CrystalChest bool
	// JewelleryBatch is the Games necklaces one jewellery round makes when the mule holds none
	// (jewellery.go, KeyJewelleryBatch); 0 turns the round off.
	JewelleryBatch int
	// RingBatch is the Rings of dueling one jewellery round makes when the mule holds none
	// (jewellerykind.go, KeyRingBatch); 0 turns ring batches off.
	RingBatch int
	// ChestGamesRoute is the Games Room route to the chest (chestalch.go, KeyChestGamesRoute): GamesRouteAlways,
	// GamesRoutePost or GamesRouteOff. ChestAlch is the chest round's alchemy (KeyChestAlch).
	ChestGamesRoute string
	ChestAlch       bool
	// WalkEnchant is whether the jewellery round enchants on the walk to the bank instead of standing at the
	// furnace (jewellerywalk.go, KeyWalkEnchant).
	WalkEnchant bool
	// MaxRound bounds one round, walk included.
	MaxRound time.Duration
	// SafeHpPercent is the least hitpoints, as a percentage of the maximum, ScarabSwarm may be
	// stopped at.
	SafeHpPercent int
	// MembersWorld says opal, jade, red topaz and dragonstone may be cut (members only objects).
	MembersWorld bool
	// FruitlessPause is how long a kind of round is left alone after one that produced nothing.
	FruitlessPause time.Duration
	// Backoff and BackoffMax are the wait after a round that failed for a passing reason (the walk
	// did not arrive, the mule was away or busy or deferred the kit, a bank would not open or
	// deposit): Backoff after the first, doubling for each failure in a row, at most BackoffMax; a
	// round that works resets it. A failed hand-over to the mule backs off on its own account.
	Backoff, BackoffMax time.Duration
	// MaxHandoverFailures is how many hand-overs in a row may fail before every round is suspended
	// until the script restarts (0 never suspends).
	MaxHandoverFailures int
	// Rune is the Runecrafting levelling round's settings (runecraft.go, the craftRune* keys).
	Rune RuneSettings
	// Sched is the round schedule's settings (schedule.go).
	Sched ScheduleSettings
}

// DefaultSettings are the defaults.
func DefaultSettings() Settings {
	return Settings{
		GemCutting: true, BowStringing: true, Pearls: true, FastGems: true, FastPearls: true, FastBows: true,
		BuyBolts: true, BuyArrowTips: true, ShopNames: DefaultShops(),
		ShopMaxPrice: DefaultShopMaxPrice, ShopMinShelfPercent: DefaultShopMinShelf,
		ShopMinWaiting: DefaultShopMinWaiting, ShopBuyBelow: DefaultShopBuyBelow,
		Superheat: true, SuperheatThreshold: DefaultSuperheatThreshold,
		Smith: true, SmithMinSteelBars: DefaultSmithMinSteelBars, SmithArrowtips: true, SmithIronPlatebodies: true, SmithKit: DefaultSmithKit,
		CannonballTarget: DefaultCannonballTarget, CannonballBatch: DefaultCannonballBatch, CannonballMinBars: DefaultCannonballMinBars,
		Bones:  true,
		Staves: true, StavesMinStaves: DefaultStavesMin, StavesMinOrbs: DefaultStavesMin,
		Platebody: true, PlatebodyMinBars: DefaultPlatebodyMinBars, PlatebodyKit: DefaultPlatebodyKit,
		SuperheatRound: DefaultSuperheatRound, PlatebodyRound: DefaultPlatebodyRound, RoundLimitPause: DefaultRoundLimitPause,
		BuyFireStaff: true, FireStaffMaxPrice: DefaultFireStaffMaxPrice,
		Arrows: true, ArrowThreshold: DefaultArrowThreshold, ArrowKit: DefaultArrowKit,
		Herbs: true, HerbThreshold: DefaultHerbThreshold,
		TrawlerQuorum: DefaultTrawlerQuorum, TrawlerFoodLow: DefaultTrawlerFoodLow, TrawlerFoodHigh: DefaultTrawlerFoodHigh,
		StoreOverflow: true, StoreAtRows: DefaultStoreAtRows, StoreToRows: DefaultStoreToRows,
		StoreMaxCount: DefaultStoreMaxCount, StoreQuietDays: DefaultStoreQuietDays,
		Idle:         IdleSwarm,
		GemThreshold: DefaultGemThreshold, BowThreshold: DefaultBowThreshold,
		PearlThreshold:  DefaultPearlThreshold,
		BoltThreshold:   DefaultBoltThreshold,
		CheckEvery:      DefaultCheckEvery,
		BowNames:        bowstringplan.DefaultBowNames(),
		KitSize:         DefaultKitSize,
		CrystalChest:    true,
		JewelleryBatch:  DefaultJewelleryBatch,
		RingBatch:       DefaultRingBatch,
		ChestGamesRoute: GamesRouteAlways,
		ChestAlch:       true,
		WalkEnchant:     true,
		BulkAtSapphires: DefaultBulkSapphires,
		MaxRound:        DefaultMaxRound,
		SafeHpPercent:   DefaultSafeHpPercent,
		MembersWorld:    true,
		FruitlessPause:  DefaultFruitless,
		Backoff:         DefaultBackoff, BackoffMax: DefaultBackoffMax,
		MaxHandoverFailures: DefaultMaxHandover,
		Rune:                DefaultRuneSettings(),
		Sched:               DefaultScheduleSettings(),
		CannonRun:           DefaultCannonRunSettings(),
	}
}

var knownKeys = map[string]bool{
	KeyGemCutting: true, KeyBowStringing: true, KeyGemThreshold: true, KeyBowThreshold: true,
	KeyCheckEvery: true, KeyGems: true, KeyBows: true, KeyKitSize: true, KeyMaxRound: true,
	KeySafeHp: true, KeyMembersWorld: true, KeyFruitlessPause: true,
	KeyBackoff: true, KeyBackoffMax: true, KeyMaxHandover: true, KeyBulkSapphires: true,
	KeyPearls: true, KeyPearlThreshold: true, KeyBoltThreshold: true, KeyFastGems: true, KeyFastPearls: true, KeyFastBows: true,
	KeyBuyBolts: true, KeyBuyArrowTips: true, KeyShops: true, KeyShopMaxPrice: true, KeyShopMinShelf: true,
	KeyShopMinWaiting: true, KeyShopBuyBelow: true, KeySuperheat: true, KeySuperheatThreshold: true,
	KeySuperheatBars: true, KeySuperheatGold: true, KeyArrows: true, KeyArrowThreshold: true, KeyArrowKit: true,
	KeyHerbs: true, KeyHerbThreshold: true, KeyBuyFireStaff: true, KeyFireStaffMaxPrice: true,
	KeyStoreOverflow: true, KeyStoreAtRows: true, KeyStoreToRows: true, KeyStoreMaxCount: true, KeyStoreQuietDays: true,
	KeyIdle:             true,
	KeySuperheatLowBars: true, KeySmith: true, KeySmithMinSteelBars: true, KeySmithArrowtips: true,
	KeySmithIronBodies: true, KeySmithKit: true,
}

func init() { knownKeys[KeyRingBatch] = true }

// The values of craftIdle.
const (
	IdlePost  = "post"
	IdleSwarm = "swarm"
	// IdleOaks runs RetaliateChopper, the two-tick oak chopper at the Falador farm (a user request: "2-tick oaks" on master
	// between trawler blocks), with ScarabSwarm behind it when it stops (internal/bot/mastercrafter_oaks.go).
	IdleOaks = "oaks"
	// IdleOaksDaily chops the 2-tick oaks craftOakMinutes (2 hours) once every craftOakEveryMinutes (24 hours) and runs ScarabSwarm the
	// rest of the time (a user request; schedule.go).
	IdleOaksDaily = "oaks-daily"
)

// KnownKeys lists every craft* key this script reads, sorted.
func KnownKeys() []string {
	out := make([]string, 0, len(knownKeys))
	for k := range knownKeys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// DecodeSettings reads the craft* keys of raw over the defaults. Other keys are ScarabSwarm's and
// ignored. Numbers may be JSON numbers or numeric strings (the control panel's form sends
// strings), booleans true/false or the strings "true"/"false", lists a list or a comma-separated
// string.
func DecodeSettings(raw map[string]interface{}) (Settings, error) {
	s := DefaultSettings()
	var problems []string
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	num := func(k string, v interface{}, dst *int) {
		n, ok := toInt(v)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: %v is not a whole number", k, v))
			return
		}
		*dst = n
	}
	mins := func(k string, v interface{}, dst *time.Duration) {
		n, ok := toInt(v)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: %v is not a whole number of minutes", k, v))
			return
		}
		*dst = time.Duration(n) * time.Minute
	}
	flag := func(k string, v interface{}, dst *bool) {
		b, ok := toBool(v)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: %v is not true or false", k, v))
			return
		}
		*dst = b
	}
	for _, k := range keys {
		if !strings.HasPrefix(k, KeyPrefix) {
			continue
		}
		v := raw[k]
		if knownKeys[k] && (s.Sched.decode(k, v, num, mins) || s.CannonRun.decode(k, v, num, mins, flag)) {
			continue
		}
		if !knownKeys[k] {
			problems = append(problems, fmt.Sprintf("%s is not a setting (known: %s)", k, strings.Join(KnownKeys(), ", ")))
			continue
		}
		switch k {
		case KeyGemCutting:
			flag(k, v, &s.GemCutting)
		case KeyBowStringing:
			flag(k, v, &s.BowStringing)
		case KeyMembersWorld:
			flag(k, v, &s.MembersWorld)
		case KeyCrystalChest:
			flag(k, v, &s.CrystalChest)
		case KeyJewelleryBatch:
			num(k, v, &s.JewelleryBatch)
		case KeyRingBatch:
			num(k, v, &s.RingBatch)
		case KeyChestAlch:
			flag(k, v, &s.ChestAlch)
		case KeyWalkEnchant:
			flag(k, v, &s.WalkEnchant)
		case KeyChestGamesRoute:
			switch mode := strings.ToLower(strings.TrimSpace(fmt.Sprint(v))); mode {
			case GamesRouteAlways, GamesRoutePost, GamesRouteOff:
				s.ChestGamesRoute = mode
			default:
				problems = append(problems, fmt.Sprintf("%s: %v is not %q, %q or %q", k, v, GamesRouteAlways, GamesRoutePost, GamesRouteOff))
			}
		case KeyRune:
			flag(k, v, &s.Rune.On)
		case KeyRuneLevelTo:
			num(k, v, &s.Rune.LevelTo)
		case KeyRuneBatch:
			num(k, v, &s.Rune.Batch)
		case KeyRuneTeleport:
			flag(k, v, &s.Rune.Teleport)
		case KeyRuneRound:
			mins(k, v, &s.Rune.Round)
		case KeyRuneBuyBudget:
			num(k, v, &s.Rune.BuyBudget)
		case KeyRuneManual:
			flag(k, v, &s.Rune.Manual)
		case KeyRuneStation:
			s.Rune.Station = strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		case KeyRuneRunners:
			s.Rune.Runners = toList(v)
		case KeyRuneKeep:
			num(k, v, &s.Rune.Keep)
		case KeyRuneCourier:
			flag(k, v, &s.Rune.Courier)
		case KeyRuneCourierMax:
			num(k, v, &s.Rune.CourierMax)
		case KeyRuneGateAlch:
			flag(k, v, &s.Rune.GateAlch)
		case KeyRuneAlchBows:
			num(k, v, &s.Rune.AlchBows)
		case KeyRuneAlchHold:
			flag(k, v, &s.Rune.AlchHold)
		case KeyRuneDirectOps:
			flag(k, v, &s.Rune.DirectOps)
		case KeyRuneOneWrite:
			flag(k, v, &s.Rune.OneWrite)
		case KeyRuneLegCasts:
			if mode, ok := runecraftplan.ParseLegCastMode(fmt.Sprint(v)); ok {
				s.Rune.LegCasts = mode
			} else {
				problems = append(problems, fmt.Sprintf("%s: %v is not \"off\", \"low2\", \"high1\" or \"high2\"", k, v))
			}
		case KeyRuneCampaign:
			flag(k, v, &s.Rune.Campaign)
		case KeyRuneStartBelow:
			num(k, v, &s.Rune.StartBelow)
		case KeyRuneStopAbove:
			num(k, v, &s.Rune.StopAbove)
		case KeyRuneTarget:
			num(k, v, &s.Rune.Target)
		case KeyRuneMinEssence:
			num(k, v, &s.Rune.MinEssence)
		case KeyRuneMinRunners:
			num(k, v, &s.Rune.MinRunners)
		case KeyRuneHandOverAt:
			num(k, v, &s.Rune.HandOverAt)
		case KeyRuneHandOverMin:
			num(k, v, &s.Rune.HandOverMin)
		case KeyRuneCourierWait:
			mins(k, v, &s.Rune.CourierWait)
		case KeyRunePause:
			mins(k, v, &s.Rune.Pause)
		case KeyRuneBuyAll:
			flag(k, v, &s.Rune.BuyAll)
		case KeyRuneMuleCoinFloor:
			num(k, v, &s.Rune.MuleCoinFloor)
		case KeyRuneBuyTrip:
			num(k, v, &s.Rune.BuyTrip)
		case KeyRuneMaxHours:
			hours := 0
			num(k, v, &hours)
			s.Rune.MaxHours = time.Duration(hours) * time.Hour
		case KeyRuneEssence:
			if src := runecraftplan.Source(strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))); runecraftplan.ValidSource(src) {
				s.Rune.Essence = src
			} else {
				problems = append(problems, fmt.Sprintf("%s: %v is not \"auto\", \"bank\", \"mule\" or \"mine\"", k, v))
			}
		case KeyRuneGateway:
			if _, ok := essenceplan.GatewayByKey(fmt.Sprint(v)); ok {
				s.Rune.Gateway = strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
			} else {
				problems = append(problems, fmt.Sprintf("%s: %v is not one of %s", k, v, strings.Join(essenceplan.GatewayKeys(), ", ")))
			}
		case KeyPearls:
			flag(k, v, &s.Pearls)
		case KeyFastGems:
			flag(k, v, &s.FastGems)
		case KeyFastPearls:
			flag(k, v, &s.FastPearls)
		case KeyFastBows:
			flag(k, v, &s.FastBows)
		case KeyPearlThreshold:
			num(k, v, &s.PearlThreshold)
		case KeyBoltThreshold:
			num(k, v, &s.BoltThreshold)
		case KeyBuyBolts:
			flag(k, v, &s.BuyBolts)
		case KeyBuyArrowTips:
			flag(k, v, &s.BuyArrowTips)
		case KeySuperheat:
			flag(k, v, &s.Superheat)
		case KeySuperheatGold:
			flag(k, v, &s.SuperheatGold)
		case KeySuperheatLowBars:
			flag(k, v, &s.SuperheatLowBars)
		case KeySmith:
			flag(k, v, &s.Smith)
		case KeySmithMinSteelBars:
			num(k, v, &s.SmithMinSteelBars)
		case KeySmithArrowtips:
			flag(k, v, &s.SmithArrowtips)
		case KeySmithIronBodies:
			flag(k, v, &s.SmithIronPlatebodies)
		case KeySmithKit:
			num(k, v, &s.SmithKit)
		case KeyTrawler:
			flag(k, v, &s.Trawler)
		case KeyTrawlerQuorum:
			num(k, v, &s.TrawlerQuorum)
		case KeyTrawlerFoodLow:
			num(k, v, &s.TrawlerFoodLow)
		case KeyTrawlerFoodHigh:
			num(k, v, &s.TrawlerFoodHigh)
		case KeyArrows:
			flag(k, v, &s.Arrows)
		case KeyHerbs:
			flag(k, v, &s.Herbs)
		case KeyBuyFireStaff:
			flag(k, v, &s.BuyFireStaff)
		case KeyFireStaffMaxPrice:
			num(k, v, &s.FireStaffMaxPrice)
		case KeyHerbThreshold:
			num(k, v, &s.HerbThreshold)
		case KeyStoreOverflow:
			flag(k, v, &s.StoreOverflow)
		case KeyStoreAtRows:
			num(k, v, &s.StoreAtRows)
		case KeyStoreToRows:
			num(k, v, &s.StoreToRows)
		case KeyStoreMaxCount:
			num(k, v, &s.StoreMaxCount)
		case KeyStoreQuietDays:
			num(k, v, &s.StoreQuietDays)
		case KeyIdle:
			switch idle := strings.ToLower(strings.TrimSpace(fmt.Sprint(v))); idle {
			case IdlePost, IdleSwarm, IdleOaks, IdleOaksDaily:
				s.Idle = idle
			default:
				problems = append(problems, fmt.Sprintf("%s: %v is not %q, %q, %q or %q", k, v, IdlePost, IdleSwarm, IdleOaks, IdleOaksDaily))
			}
		case KeyShopMaxPrice:
			num(k, v, &s.ShopMaxPrice)
		case KeyShopMinShelf:
			num(k, v, &s.ShopMinShelfPercent)
		case KeyShopMinWaiting:
			num(k, v, &s.ShopMinWaiting)
		case KeyShopBuyBelow:
			num(k, v, &s.ShopBuyBelow)
		case KeySuperheatThreshold:
			num(k, v, &s.SuperheatThreshold)
		case KeyArrowThreshold:
			num(k, v, &s.ArrowThreshold)
		case KeyArrowKit:
			num(k, v, &s.ArrowKit)
		case KeyShops:
			s.ShopNames = toList(v)
		case KeySuperheatBars:
			s.SuperheatBars = toList(v)
		case KeyGemThreshold:
			num(k, v, &s.GemThreshold)
		case KeyBowThreshold:
			num(k, v, &s.BowThreshold)
		case KeyKitSize:
			num(k, v, &s.KitSize)
		case KeyBulkSapphires:
			num(k, v, &s.BulkAtSapphires)
		case KeyMaxHandover:
			num(k, v, &s.MaxHandoverFailures)
		case KeySafeHp:
			num(k, v, &s.SafeHpPercent)
		case KeyCheckEvery:
			mins(k, v, &s.CheckEvery)
		case KeyMaxRound:
			mins(k, v, &s.MaxRound)
		case KeyFruitlessPause:
			mins(k, v, &s.FruitlessPause)
		case KeyBackoff:
			mins(k, v, &s.Backoff)
		case KeyBackoffMax:
			mins(k, v, &s.BackoffMax)
		case KeyGems:
			s.GemKinds = toList(v)
		case KeyBows:
			if l := toList(v); len(l) > 0 {
				s.BowNames = l
			}
		case KeyBones:
			flag(k, v, &s.Bones)
		case KeyStaves:
			flag(k, v, &s.Staves)
		case KeyStavesMinStaff:
			num(k, v, &s.StavesMinStaves)
		case KeyStavesMinOrbs:
			num(k, v, &s.StavesMinOrbs)
		case KeyPlatebody:
			flag(k, v, &s.Platebody)
		case KeyPlatebodyMinBars:
			num(k, v, &s.PlatebodyMinBars)
		case KeyPlatebodyKit:
			num(k, v, &s.PlatebodyKit)
		case KeySuperheatRoundMinutes:
			mins(k, v, &s.SuperheatRound)
		case KeyPlatebodyRoundMinutes:
			mins(k, v, &s.PlatebodyRound)
		case KeyRoundLimitPauseMinutes:
			mins(k, v, &s.RoundLimitPause)
		case KeyCannonballTarget:
			num(k, v, &s.CannonballTarget)
		case KeyCannonballBatch:
			num(k, v, &s.CannonballBatch)
		case KeyCannonballMinBars:
			num(k, v, &s.CannonballMinBars)
		case KeySkillGoal:
			num(k, v, &s.SkillGoal)
		case KeyPastGoal:
			s.PastGoal = toList(v)
		}
	}
	if len(problems) > 0 {
		return Settings{}, fmt.Errorf("MasterCrafter settings: %s", strings.Join(problems, "; "))
	}
	return s, s.Validate()
}

// Validate reports every problem with s.
func (s Settings) Validate() error {
	p := s.validateGoal()
	if s.GemThreshold < 1 {
		p = append(p, fmt.Sprintf("%s %d is below 1", KeyGemThreshold, s.GemThreshold))
	}
	if s.BowThreshold < 1 {
		p = append(p, fmt.Sprintf("%s %d is below 1", KeyBowThreshold, s.BowThreshold))
	}
	if s.PearlThreshold < 1 {
		p = append(p, fmt.Sprintf("%s %d is below 1", KeyPearlThreshold, s.PearlThreshold))
	}
	if s.BoltThreshold < 1 {
		p = append(p, fmt.Sprintf("%s %d is below 1", KeyBoltThreshold, s.BoltThreshold))
	}
	if s.CheckEvery < time.Minute || s.CheckEvery > 24*time.Hour {
		p = append(p, fmt.Sprintf("%s %s is outside 1..1440 minutes", KeyCheckEvery, s.CheckEvery))
	}
	if s.BulkAtSapphires < 0 {
		p = append(p, fmt.Sprintf("%s %d is below 0 (0 turns bulk asks off)", KeyBulkSapphires, s.BulkAtSapphires))
	}
	if s.KitSize < 1 || s.KitSize > bowstringplan.KitMax {
		p = append(p, fmt.Sprintf("%s %d is outside 1..%d", KeyKitSize, s.KitSize, bowstringplan.KitMax))
	}
	if s.MaxRound < 5*time.Minute || s.MaxRound > 24*time.Hour {
		p = append(p, fmt.Sprintf("%s %s is outside 5..1440 minutes", KeyMaxRound, s.MaxRound))
	}
	if s.SafeHpPercent < 0 || s.SafeHpPercent > 100 {
		p = append(p, fmt.Sprintf("%s %d is outside 0..100", KeySafeHp, s.SafeHpPercent))
	}
	if s.FruitlessPause < 0 || s.FruitlessPause > 7*24*time.Hour {
		p = append(p, fmt.Sprintf("%s %s is outside 0..10080 minutes", KeyFruitlessPause, s.FruitlessPause))
	}
	if s.Backoff < time.Minute || s.Backoff > 24*time.Hour {
		p = append(p, fmt.Sprintf("%s %s is outside 1..1440 minutes", KeyBackoff, s.Backoff))
	}
	if s.BackoffMax < s.Backoff || s.BackoffMax > 7*24*time.Hour {
		p = append(p, fmt.Sprintf("%s %s is below %s or over 10080 minutes", KeyBackoffMax, s.BackoffMax, KeyBackoff))
	}
	if s.MaxHandoverFailures < 0 || s.MaxHandoverFailures > 100 {
		p = append(p, fmt.Sprintf("%s %d is outside 0..100", KeyMaxHandover, s.MaxHandoverFailures))
	}
	if s.JewelleryBatch < 0 || s.JewelleryBatch > MaxJewelleryBatch {
		p = append(p, fmt.Sprintf("%s %d is outside 0..%d (0 turns the jewellery round off)", KeyJewelleryBatch, s.JewelleryBatch, MaxJewelleryBatch))
	}
	if s.RingBatch < 0 || s.RingBatch > MaxJewelleryBatch {
		p = append(p, fmt.Sprintf("%s %d is outside 0..%d (0 turns the Ring of dueling batches off)", KeyRingBatch, s.RingBatch, MaxJewelleryBatch))
	}
	if unknown := gemplan.UnknownGems(s.GemKinds); len(unknown) > 0 {
		p = append(p, fmt.Sprintf("%s: %s %s not a gem (known: %s)", KeyGems, strings.Join(unknown, ", "), plural(len(unknown)), strings.Join(gemplan.GemNames(), ", ")))
	}
	if len(s.BowNames) == 0 {
		p = append(p, KeyBows+" is empty")
	}
	for _, b := range s.BowNames {
		if _, ok := bowstringplan.LevelFor(b); !ok {
			p = append(p, fmt.Sprintf("%s: %q is not a stringable bow", KeyBows, b))
		}
	}
	if s.ShopMaxPrice < 1 || s.ShopMaxPrice > 100000 {
		p = append(p, fmt.Sprintf("%s %d is outside 1..100000", KeyShopMaxPrice, s.ShopMaxPrice))
	}
	if s.ShopMinShelfPercent < 1 || s.ShopMinShelfPercent > 100 {
		p = append(p, fmt.Sprintf("%s %d is outside 1..100", KeyShopMinShelf, s.ShopMinShelfPercent))
	}
	if s.ShopMinWaiting < 1 {
		p = append(p, fmt.Sprintf("%s %d is below 1", KeyShopMinWaiting, s.ShopMinWaiting))
	}
	if s.ShopBuyBelow < 1 {
		p = append(p, fmt.Sprintf("%s %d is below 1", KeyShopBuyBelow, s.ShopBuyBelow))
	}
	if (s.BuyBolts || s.BuyArrowTips) && len(s.ShopNames) == 0 {
		p = append(p, KeyShops+" is empty")
	}
	for _, n := range s.ShopNames {
		if _, ok := shopplan.SupplyShopByName(n); !ok {
			p = append(p, fmt.Sprintf("%s: %q is not a supply shop (known: %s)", KeyShops, n, strings.Join(shopplan.SupplyShopNames(), ", ")))
		}
	}
	if s.SuperheatThreshold < 1 {
		p = append(p, fmt.Sprintf("%s %d is below 1", KeySuperheatThreshold, s.SuperheatThreshold))
	}
	for _, n := range s.SuperheatBars {
		if _, ok := BarByName(n); !ok {
			p = append(p, fmt.Sprintf("%s: %q is not a bar (known: %s)", KeySuperheatBars, n, strings.Join(BarNames(), ", ")))
		}
	}
	if s.SmithMinSteelBars < 0 {
		p = append(p, fmt.Sprintf("%s %d is below 0", KeySmithMinSteelBars, s.SmithMinSteelBars))
	}
	if s.TrawlerQuorum < 1 || s.TrawlerQuorum > 2000 {
		p = append(p, fmt.Sprintf("%s %d is outside 1..2000", KeyTrawlerQuorum, s.TrawlerQuorum))
	}
	if s.TrawlerFoodLow < 0 || s.TrawlerFoodHigh < 0 || (s.TrawlerFoodHigh > 0 && s.TrawlerFoodHigh <= s.TrawlerFoodLow) {
		p = append(p, fmt.Sprintf("%s %d and %s %d: the low mark is 0 (off) and the high mark 0 (no early end) or above the low mark", KeyTrawlerFoodLow, s.TrawlerFoodLow, KeyTrawlerFoodHigh, s.TrawlerFoodHigh))
	}
	if s.SmithKit < 1 || s.SmithKit > bowstringplan.KitMax {
		p = append(p, fmt.Sprintf("%s %d is outside 1..%d", KeySmithKit, s.SmithKit, bowstringplan.KitMax))
	}
	if s.Smith && !s.SmithArrowtips {
		p = append(p, KeySmith+" is on and "+KeySmithArrowtips+" is off: the round starts on Steel bars, which only the arrowtips use")
	}
	if s.ArrowThreshold < 1 {
		p = append(p, fmt.Sprintf("%s %d is below 1", KeyArrowThreshold, s.ArrowThreshold))
	}
	if s.ArrowKit < 1 {
		p = append(p, fmt.Sprintf("%s %d is below 1", KeyArrowKit, s.ArrowKit))
	}
	if s.FireStaffMaxPrice < 1 {
		p = append(p, fmt.Sprintf("%s %d is below 1", KeyFireStaffMaxPrice, s.FireStaffMaxPrice))
	}
	if s.HerbThreshold < 1 {
		p = append(p, fmt.Sprintf("%s %d is below 1", KeyHerbThreshold, s.HerbThreshold))
	}
	if s.StoreAtRows < 1 || s.StoreAtRows > BankRows {
		p = append(p, fmt.Sprintf("%s %d is outside 1..%d (a bank's rows)", KeyStoreAtRows, s.StoreAtRows, BankRows))
	}
	if s.StoreToRows < 0 || s.StoreToRows >= s.StoreAtRows {
		p = append(p, fmt.Sprintf("%s %d is below 0 or not under %s %d", KeyStoreToRows, s.StoreToRows, KeyStoreAtRows, s.StoreAtRows))
	}
	if s.StoreMaxCount < 1 {
		p = append(p, fmt.Sprintf("%s %d is below 1", KeyStoreMaxCount, s.StoreMaxCount))
	}
	if s.StoreQuietDays < 1 || s.StoreQuietDays > 365 {
		p = append(p, fmt.Sprintf("%s %d is outside 1..365", KeyStoreQuietDays, s.StoreQuietDays))
	}
	p = append(p, s.validateCannonballs()...)
	p = append(p, s.validateRoundLimits()...)
	p = append(p, s.Sched.validate()...)
	p = append(p, s.CannonRun.validate()...)
	p = append(p, s.validatePlatebody()...)
	p = append(p, s.validateStaves()...)
	p = append(p, s.Rune.validate()...)
	if !s.GemCutting && !s.BowStringing && !s.Pearls && !s.Superheat && !s.Smith && !s.Arrows && !s.Herbs {
		p = append(p, "every round is switched off: use ScarabSwarm instead")
	}
	if len(p) > 0 {
		return fmt.Errorf("MasterCrafter settings: %s", strings.Join(p, "; "))
	}
	return nil
}

func plural(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func toInt(v interface{}) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		if x != float64(int(x)) {
			return 0, false
		}
		return int(x), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		return n, err == nil
	}
	return 0, false
}

func toBool(v interface{}) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "yes", "on":
			return true, true
		case "false", "no", "off":
			return false, true
		}
	}
	return false, false
}

func toList(v interface{}) []string {
	var out []string
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	switch x := v.(type) {
	case []string:
		for _, s := range x {
			add(s)
		}
	case []interface{}:
		for _, e := range x {
			if s, ok := e.(string); ok {
				add(s)
			}
		}
	case string:
		for _, p := range strings.Split(x, ",") {
			add(p)
		}
	}
	return out
}
