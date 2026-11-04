package mastercraftplan

// The Runecrafting round (a user request: "runecraft with runners in the fleet, maxing nature runes;
// the main account, the main account, gets the Runecraft levels and sits at the altar"). This is P1 of the design
// (docs/runecraft_p1_2026-10-13.md): the main account levels Runecraft 1 to 44 alone, Air to level 20 and Body from
// there, on what the fleet already holds, so the Nature altar (level 44, members, one talisman) is open
// for the phases that follow.
//
//  1. at the mule's post: bank the pack, read the bank; ask the mule for what is missing (the Air and Body
//     talismans, essence if it publishes any, the runes of the travel teleports) and bank what comes;
//  2. a trip is: the bank near the ruins (Falador east for Air, Edgeville for Body) loads the talisman and
//     27 Rune essence, the walk to the ruins, the talisman used on them and the arrival WAITED FOR, the
//     altar's Craft-rune (the whole pack at once, four busy ticks) VERIFIED by the essence leaving and the
//     experience rising, the portal timed to the end of the hold, the walk back;
//  3. when the bank holds no essence and the mule none, the main account mines it at the Wizards' Guild (Magic 66,
//     Rune Mysteries, a pickaxe in the bank), a batch at a time, banked at Yanille.
//
// The pure decisions are internal/scripts/runecraftplan's; this file is the round's TRIGGER, its settings
// and its ledger, in the style of the chest round (chest.go).
//
// WHERE IT RANKS (Decide): a round under way (standing in a temple or the essence mine, or on the road with
// the load and the talisman) carries on before anything else, even a pending hand-over (the account cannot
// walk to the mule's post from inside a temple); a new one comes after the jewellery round (short, rare, and
// the mule's) and BEFORE the batch rounds -- a gem round that the mule's stock keeps due would otherwise pull
// the main account away from the altar every time -- and gives way to chest loot still owed to the mule
// (DecideWithChest).

import (
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/runecraftplan"
)

// ModeRunecraft is a Runecrafting levelling round.
const ModeRunecraft Mode = "runecraft"

// Setting keys. All of them are craftRune*, and every one is read by DecodeSettings.
const (
	KeyRune         = "craftRune"
	KeyRuneLevelTo  = "craftRuneLevelTo"
	KeyRuneEssence  = "craftRuneEssence"
	KeyRuneBatch    = "craftRuneBatch"
	KeyRuneGateway  = "craftRuneGateway"
	KeyRuneTeleport = "craftRuneTeleport"
	KeyRuneRound    = "craftRuneRoundMinutes"
	// KeyRuneBuyBudget is the coins a bounded market lot of essence may spend in all (0: off).
	KeyRuneBuyBudget = "craftRuneBuyBudget"
	// The Nature gate (P3, nature.go): craftRuneManual starts a gate round whenever it is possible (the owner's switch, until the
	// campaign trigger of P4), craftRuneStation where the crafter stands, craftRuneRunners the carriers he trades, craftRuneKeep the
	// Natures he never hands to a courier, craftRuneCourier and craftRuneCourierMax the courier hand-over, craftRuneMaxHours the
	// longest a gate round lasts.
	KeyRuneManual     = "craftRuneManual"
	KeyRuneStation    = "craftRuneStation"
	KeyRuneRunners    = "craftRuneRunners"
	KeyRuneKeep       = "craftRuneKeep"
	KeyRuneCourier    = "craftRuneCourier"
	KeyRuneCourierMax = "craftRuneCourierMax"
	KeyRuneMaxHours   = "craftRuneMaxHours"
	// KeyRuneGateAlch and KeyRuneAlchBows are the gate's alchemy (a user request; runecraftplan/gatealch.go): the main account casts High Level
	// Alchemy on noted strung bows with his Staff of fire, on the road to the gate and before the first trade, and draws that many bows.
	KeyRuneGateAlch = "craftRuneGateAlch"
	KeyRuneAlchBows = "craftRuneAlchBows"
	// KeyRuneAlchHold keeps the bows through the trades and casts them in the idle seconds between them (see RuneSettings.AlchHold).
	KeyRuneAlchHold = "craftRuneAlchHold"
	// KeyRuneDirectOps sends the gate's altar and portal clicks in the same write as their walks (see RuneSettings.DirectOps).
	KeyRuneDirectOps = "craftRuneDirectOps"
	// KeyRuneOneWrite writes the gate's confirm accept and the talisman use in one message (see RuneSettings.OneWrite).
	KeyRuneOneWrite = "craftRuneOneWrite"
	// KeyRuneLegCasts casts alchemy on the gate's two walking legs (see RuneSettings.LegCasts).
	KeyRuneLegCasts = "craftRuneLegCasts"
)

func init() {
	for _, k := range []string{KeyRune, KeyRuneLevelTo, KeyRuneEssence, KeyRuneBatch, KeyRuneGateway, KeyRuneTeleport, KeyRuneRound, KeyRuneBuyBudget,
		KeyRuneManual, KeyRuneStation, KeyRuneRunners, KeyRuneKeep, KeyRuneCourier, KeyRuneCourierMax, KeyRuneMaxHours, KeyRuneGateAlch, KeyRuneAlchBows, KeyRuneAlchHold,
		KeyRuneDirectOps, KeyRuneOneWrite, KeyRuneLegCasts} {
		knownKeys[k] = true
	}
}

// Defaults and bounds.
const (
	// DefaultRuneLevelTo is the level the levelling stops at: Nature opens there (production is a later phase).
	DefaultRuneLevelTo = 44
	// MaxRuneLevelTo is the highest level this phase levels to: Nature is not crafted yet.
	MaxRuneLevelTo = 44
	// DefaultRuneBatch is the essence one mining stretch brings before a craft stretch (owner's design: 1,000).
	DefaultRuneBatch = 1000
	// DefaultRuneRound bounds one levelling round, walks and mining included: it is a long exclusive round, like
	// the chest's, and the account goes back to ScarabSwarm at the end of it.
	DefaultRuneRound = 240 * time.Minute
	// MaxRuneBuyBudget is the largest market budget a setting may name: ten million coins is 100,000 essence at the stalls'
	// price, the whole of a Nature campaign, and no lot is ever meant to be that.
	MaxRuneBuyBudget = 10_000_000
)

// RuneSettings are the Runecrafting round's settings.
type RuneSettings struct {
	// On is craftRune, the master switch (default OFF: the owner flips it at the relaunch).
	On bool
	// LevelTo is craftRuneLevelTo: levelling stops when Runecrafting reaches it (2..44).
	LevelTo int
	// Essence is craftRuneEssence: where the essence comes from -- "auto" (the bank, then the mule's published
	// stock, then mining), "bank", "mule" or "mine".
	Essence runecraftplan.Source
	// Batch is craftRuneBatch: the essence one mining stretch brings (27..100000).
	Batch int
	// Gateway is craftRuneGateway: the way into the essence mine -- "guild" (the Wizards' Guild, Magic 66),
	// "aubury" or "cromperty".
	Gateway string
	// Teleport is craftRuneTeleport: long walks (post to the ruins, the mine to the ruins, home) start with a
	// teleport when the pack holds the runes (Falador Teleport out, Camelot Teleport home).
	Teleport bool
	// Round is craftRuneRoundMinutes: the bound on one round.
	Round time.Duration
	// BuyBudget is craftRuneBuyBudget: the coins, in all and for good (the ledger counts what was spent), a bounded
	// market lot of essence at the Seers stalls may spend when the bank, the mule and mining give none. 0 (the default)
	// is off. The lot is funded by the mule, only from what it holds above essenceplan.MarketMuleCoinsFloor (a million),
	// at no more than essenceplan.MarketUnitCeiling (150) an essence. 300,000 buys about 3,000 at the stalls' 100.
	BuyBudget int

	// The Nature gate (P3; nature.go and docs/runecraft_p3_2026-10-14.md). The gate is craftRune plus craftRuneManual: with both on
	// and Runecrafting at 44, the main account walks to the Nature ruins and crafts what the RuneCarrier accounts trade him.
	//
	// Manual is craftRuneManual: start a gate round whenever it is possible (Runecrafting 44, a members world, a Nature talisman),
	// with no other trigger. Default OFF; the campaign trigger (the mule out of Natures) is the next phase.
	Manual bool
	// Station is craftRuneStation: "gate" (outside the ruins, one talisman, the carriers come to him). "inside" (parked in the
	// temple, a talisman a carrier) is an experiment that is not built.
	Station string
	// Runners is craftRuneRunners: the carriers the main account trades with, by login or display name. Whoever is at work on the bus for him
	// is allowed too.
	Runners []string
	// Keep is craftRuneKeep: the Natures a courier never takes (his chest alchemy and superheat stock). Courier lets carriers carry
	// the surplus beyond Keep home on their kit trips, at most CourierMax a trade.
	Keep       int
	Courier    bool
	CourierMax int
	// MaxHours is craftRuneMaxHours: the longest a gate round lasts before the main account walks home (1 to 24).
	MaxHours time.Duration
	// GateAlch is craftRuneGateAlch: High Level Alchemy on noted strung bows on the road to the gate and before the first trade (a stack
	// held at a trade would cost the 25th essence, runecraftplan/gatealch.go). AlchBows is craftRuneAlchBows: the bows one round draws
	// (0 draws none).
	GateAlch bool
	AlchBows int
	// AlchHold is craftRuneAlchHold (default OFF): keep the bows through the trades and cast them in the idle seconds between trades,
	// when no carrier is near. The stack is a 4th pack slot, so the gate takes 24 essence a trade, not 25 (the carriers are told on the
	// gate message and load 24): about 4 percent of the essence for about a hundred casts an hour. Off, the bows are cast away on the road
	// and before the first trade, and the gate takes 25.
	AlchHold bool
	// DirectOps is craftRuneDirectOps (default ON; CR-1 of docs/xp_rate_plan_2026-10-19.md): inside the Nature temple the altar click and
	// the portal click go out in the same write as the walk to their stands, the way the real client clicks a loc, and not after a poll
	// has SEEN the account arrive. Any failure of one falls back to the walk-then-click of before (runecraftplan/direct.go). OFF is
	// that walk-then-click for every cycle.
	DirectOps bool
	// OneWrite is craftRuneOneWrite (default ON; CR-2 of docs/xp_rate_plan_2026-10-19.md): the Nature gate's confirm accept and the talisman
	// on the ruins are written in ONE message, once the carrier's accept is on the screen, so the use is read in the tick the trade completes
	// and does not wait for the account to see the trade close (runecraftplan/together.go). A carrier that has not accepted within three
	// ticks is accepted the plain way, a cycle that fails (no essence came, the use did nothing) is plain for the next ten, three in a row
	// end it for the round. OFF is the accept on its own and the talisman after the close, every cycle.
	OneWrite bool
	// LegCasts is craftRuneLegCasts (default "low2"; CX-1 of docs/xp_rate_plan_2026-10-19.md): Low or High Level Alchemy on the highest tier of
	// the noted strung bows held, cast on the two walking legs inside the Nature temple where nobody can ask for a trade ("low2": Low Level
	// Alchemy on both legs, which costs the cycle no tick; "high1": High Level Alchemy on the portal leg, two ticks; "high2": both legs, four;
	// "off": none). The casts need the bows held through the trades (AlchHold). runecraftplan/legcast.go has the engine rules and the cost.
	LegCasts runecraftplan.LegCastMode

	// The Nature campaign (P4; campaign.go and docs/runecraft_p4_2026-10-14.md): the autopilot over the gate. With craftRuneCampaign
	// on, a campaign starts BY ITSELF when the mule's published Nature rune is under StartBelow, crafts toward Target and ends when
	// the mule holds StopAbove.
	//
	// Campaign is craftRuneCampaign (default OFF; an alternative to Manual, never both).
	Campaign bool
	// StartBelow, StopAbove and Target are craftRuneStartBelow, craftRuneStopAbove and craftRuneTarget.
	StartBelow, StopAbove, Target int
	// MinEssence and MinRunners are craftRuneMinEssence and craftRuneMinRunners: what a campaign needs to start with.
	MinEssence, MinRunners int
	// HandOverAt and HandOverMin are craftRuneHandOverAt and craftRuneHandOverMin: the surplus beyond Keep at which the main account delivers
	// it himself when no courier is coming, and the least surplus worth a trip. CourierWait is craftRuneCourierWait: how long
	// without a courier counts as none coming.
	HandOverAt, HandOverMin int
	CourierWait             time.Duration
	// Pause is craftRunePauseMinutes: how long a campaign sits out after a round that ended on time, carriers or essence.
	Pause time.Duration
	// BuyAll is craftRuneBuyAll: when the mule's essence runs short, the campaign asks the mule for its WHOLE coin stack (less
	// MuleCoinFloor, in trips of BuyTrip coins when that is not 0) and spends it on every stall that lists Rune essence (owner,
	// 14 Oct 2026: "request mules entire cash stack and spend it on everything instead of just running out").
	BuyAll        bool
	MuleCoinFloor int
	BuyTrip       int
}

// The gate's defaults and bounds.
const (
	// StationGate is craftRuneStation's one value.
	StationGate = "gate"
	// StationInside is the experiment that is not built.
	StationInside = "inside"
	// DefaultRuneKeep is the Natures kept: the chest round's 2,000-rune ask plus the superheat's headroom.
	DefaultRuneKeep = 4000
	// DefaultRuneCourierMax is the Natures one courier trade carries (about 0.75M gp of risk on the road).
	DefaultRuneCourierMax = 3000
	// DefaultRuneMaxHours bounds one gate round.
	DefaultRuneMaxHours = 8
	// MaxRuneKeep and MaxRuneCourierMax bound the settings.
	MaxRuneKeep       = 100_000
	MaxRuneCourierMax = 100_000
)

// DefaultRuneSettings are the defaults: off, to 44, essence by the ladder, the Guild, teleports on; the gate off (craftRuneManual
// false), keeping 4,000 Natures, couriers allowed up to 3,000, a round of at most 8 hours.
func DefaultRuneSettings() RuneSettings {
	return RuneSettings{LevelTo: DefaultRuneLevelTo, Essence: runecraftplan.SourceAuto, Batch: DefaultRuneBatch,
		Gateway: essenceplan.GatewayGuild, Teleport: true, Round: DefaultRuneRound,
		Station: StationGate, Keep: DefaultRuneKeep, Courier: true, CourierMax: DefaultRuneCourierMax,
		MaxHours: DefaultRuneMaxHours * time.Hour, GateAlch: runecraftplan.DefaultGateAlch, AlchBows: runecraftplan.DefaultAlchBows, DirectOps: true, OneWrite: true, LegCasts: runecraftplan.DefaultLegCasts,
		StartBelow: runecraftplan.DefaultStartBelow, StopAbove: runecraftplan.DefaultStopAbove, Target: runecraftplan.DefaultTarget,
		MinEssence: runecraftplan.DefaultMinEssence, MinRunners: runecraftplan.DefaultMinRunners,
		HandOverAt: runecraftplan.DefaultHandOverAt, HandOverMin: runecraftplan.DefaultHandOverMin, CourierWait: runecraftplan.DefaultCourierWait,
		Pause: runecraftplan.DefaultPause, BuyAll: true, MuleCoinFloor: runecraftplan.DefaultMuleCoinFloor}
}

// validate reports every problem with the Runecrafting settings.
func (r RuneSettings) validate() []string {
	var p []string
	if r.LevelTo < 2 || r.LevelTo > MaxRuneLevelTo {
		p = append(p, KeyRuneLevelTo+" "+strconv.Itoa(r.LevelTo)+" is outside 2.."+strconv.Itoa(MaxRuneLevelTo)+" (Nature is not crafted by this phase)")
	}
	if !runecraftplan.ValidSource(r.Essence) {
		p = append(p, KeyRuneEssence+" "+strconv.Quote(string(r.Essence))+" is not \"auto\", \"bank\", \"mule\" or \"mine\"")
	}
	if r.Batch < runecraftplan.LoadSize || r.Batch > 100000 {
		p = append(p, KeyRuneBatch+" "+strconv.Itoa(r.Batch)+" is outside "+strconv.Itoa(runecraftplan.LoadSize)+"..100000")
	}
	if _, ok := essenceplan.GatewayByKey(r.Gateway); !ok {
		p = append(p, KeyRuneGateway+" "+strconv.Quote(r.Gateway)+" is not one of "+strings.Join(essenceplan.GatewayKeys(), ", "))
	}
	if r.Round < 5*time.Minute || r.Round > 24*time.Hour {
		p = append(p, KeyRuneRound+" "+r.Round.String()+" is outside 5..1440 minutes")
	}
	if r.BuyBudget < 0 || r.BuyBudget > MaxRuneBuyBudget {
		p = append(p, KeyRuneBuyBudget+" "+strconv.Itoa(r.BuyBudget)+" is outside 0.."+strconv.Itoa(MaxRuneBuyBudget)+" (0 turns the market lot off)")
	}
	switch r.Station {
	case StationGate:
	case StationInside:
		p = append(p, KeyRuneStation+" \"inside\" is an experiment that is not built (it needs a Nature talisman for every carrier: the fleet has seven); the gate is \""+StationGate+"\"")
	default:
		p = append(p, KeyRuneStation+" "+strconv.Quote(r.Station)+" is not \""+StationGate+"\"")
	}
	for _, n := range r.Runners {
		if strings.TrimSpace(n) == "" || len(strings.TrimSpace(n)) > 12 {
			p = append(p, KeyRuneRunners+" "+strconv.Quote(n)+" is not an account name (1 to 12 characters)")
		}
	}
	if r.Keep < 0 || r.Keep > MaxRuneKeep {
		p = append(p, KeyRuneKeep+" "+strconv.Itoa(r.Keep)+" is outside 0.."+strconv.Itoa(MaxRuneKeep))
	}
	if r.CourierMax < 1 || r.CourierMax > MaxRuneCourierMax {
		p = append(p, KeyRuneCourierMax+" "+strconv.Itoa(r.CourierMax)+" is outside 1.."+strconv.Itoa(MaxRuneCourierMax))
	}
	if r.MaxHours < time.Hour || r.MaxHours > 24*time.Hour {
		p = append(p, KeyRuneMaxHours+" "+r.MaxHours.String()+" is outside 1..24 hours")
	}
	if limit := runecraftplan.AlchBowsLimit(r.AlchHold); r.AlchBows < 0 || r.AlchBows > limit {
		p = append(p, KeyRuneAlchBows+" "+strconv.Itoa(r.AlchBows)+" is outside 0.."+strconv.Itoa(limit)+" ("+KeyRuneAlchHold+" "+strconv.FormatBool(r.AlchHold)+")")
	}
	if _, ok := runecraftplan.ParseLegCastMode(string(r.LegCasts)); !ok {
		p = append(p, KeyRuneLegCasts+" "+strconv.Quote(string(r.LegCasts))+" is not \"off\", \"low2\", \"high1\" or \"high2\"")
	}
	return append(p, r.validateCampaign()...)
}

// RuneLedger is the Runecrafting round's own memory (Ledger.Rune): the lifetime counters, what the last bank
// visit saw (a restart reads no bank until a booth opens, and the decision needs to know whether a round is
// worth starting), and the pause after a round that found nothing.
type RuneLedger struct {
	// Lifetime counters: rounds started, trips (one load to the altar and back), crafts, the essence they bound,
	// the runes by altar key, the experience gained, the essence mined and banked, mining stretches, the essence
	// asked of the mule.
	Rounds         int            `json:"rounds,omitempty"`
	Trips          int            `json:"trips,omitempty"`
	Crafts         int            `json:"crafts,omitempty"`
	EssenceCrafted int            `json:"essenceCrafted,omitempty"`
	RunesMade      map[string]int `json:"runesMade,omitempty"`
	XPGained       int            `json:"xpGained,omitempty"`
	Mined          int            `json:"mined,omitempty"`
	MineStretches  int            `json:"mineStretches,omitempty"`
	Asked          int            `json:"asked,omitempty"`
	// The market lots: the coins they paid in all (what craftRuneBuyBudget counts against), the essence they brought
	// and how many lots bought something.
	MarketSpent  int `json:"marketSpent,omitempty"`
	MarketBought int `json:"marketBought,omitempty"`
	MarketLots   int `json:"marketLots,omitempty"`
	// What the last bank visit saw: the essence, the talismans by altar key and the teleport kit, and when.
	BankEssence  int            `json:"bankEssence,omitempty"`
	BankTalisman map[string]int `json:"bankTalisman,omitempty"`
	BankKit      runecraftplan.Kit
	BankSeen     time.Time `json:"bankSeen,omitempty"`
	// PauseUntil holds the round off after one that found no essence or no talisman anywhere.
	PauseUntil time.Time `json:"pauseUntil,omitempty"`
	// LastStop is why the last round ended, for the log after a restart.
	LastStop string `json:"lastStop,omitempty"`
	// Gate is the Nature gate's counters (nature.go); Campaign the Nature campaign's state (campaign.go).
	Gate     GateLedger     `json:"gate,omitempty"`
	Campaign CampaignLedger `json:"campaign,omitempty"`
	// KitAskedAt is when a kit-only ask of the mule (the teleport kit's runes alone) was last made, answered or not
	// (runecraftplan.KitAskGap spaces them).
	KitAskedAt time.Time `json:"kitAskedAt,omitempty"`
}

// KitAskRecent reports whether a kit-only ask was made less than runecraftplan.KitAskGap before now.
func (l *RuneLedger) KitAskRecent(now time.Time) bool {
	return l != nil && !l.KitAskedAt.IsZero() && now.Sub(l.KitAskedAt) < runecraftplan.KitAskGap
}

// NoteCraft records one verified craft.
func (l *RuneLedger) NoteCraft(altarKey string, essence, runes, xp int) {
	l.Crafts++
	l.EssenceCrafted += essence
	l.XPGained += xp
	if l.RunesMade == nil {
		l.RunesMade = map[string]int{}
	}
	l.RunesMade[altarKey] += runes
}

// NoteMarket records one market lot: the coins it paid and the essence it brought.
func (l *RuneLedger) NoteMarket(spent, bought int) {
	if spent <= 0 && bought <= 0 {
		return
	}
	l.MarketSpent += max(0, spent)
	l.MarketBought += max(0, bought)
	l.MarketLots++
}

// NoteBank records what a bank visit left in the bank.
func (l *RuneLedger) NoteBank(essence int, talisman map[string]int, kit runecraftplan.Kit, now time.Time) {
	l.BankEssence, l.BankKit, l.BankSeen = essence, kit, now
	l.BankTalisman = map[string]int{}
	for k, n := range talisman {
		if n > 0 {
			l.BankTalisman[k] = n
		}
	}
	if len(l.BankTalisman) == 0 {
		l.BankTalisman = nil
	}
}

// Known reports whether any bank visit has been recorded.
func (l *RuneLedger) Known() bool { return l != nil && !l.BankSeen.IsZero() }

// RuneInput is what the Runecrafting decision reads besides the Snapshot's mule stock.
type RuneInput struct {
	// Known is set once the Runecrafting stat has been read; Level and XP are it.
	Known     bool
	Level, XP int
	// Members is whether the world crafts members runes.
	Members bool
	// Where is the account's region (a temple or the essence mine means a round is under way) and Temple the
	// altar key when it is in one.
	Where  runecraftplan.Region
	Temple string
	// PackEssence is the unnoted essence in the pack and PackTalisman the talismans held, by altar key.
	PackEssence  int
	PackTalisman map[string]bool
	// BankKnown says the bank's figures are real (read this session, or remembered by the ledger); BankEssence,
	// BankTalisman and BankKit are what it held.
	BankKnown    bool
	BankEssence  int
	BankTalisman map[string]int
	BankKit      runecraftplan.Kit
	// CanBuy says a bounded market lot of up to BuyQty essence is allowed now (essenceplan.PlanMarketLot), BuyWhy why
	// not.
	CanBuy bool
	BuyQty int
	BuyWhy string
	// CanMine says the account can mine essence at set.Rune.Gateway (its Magic, Rune Mysteries, a pickaxe in the
	// bank, worn or carried), MineWhy why not.
	CanMine bool
	MineWhy string
	// AtGate is set when the account stands on the surface within runecraftplan.GateNear of the Nature ruins' exit tile (a
	// gate round is under way where it stands); NatureHeld the Nature runes in its pack; Carriers the RuneCarrier accounts at
	// work for it on the bus.
	AtGate     bool
	NatureHeld int
	Carriers   int
	// Standby is the carriers that say they stand by for it on the bus (not at work); BuyOut the essence a whole-stack market buy-out
	// could fund now (craftRuneBuyAll, the mule's published coins above its floor, the stalls on this world) and BuyOutWhy why it
	// could fund none: the campaign's trigger weighs them against the mule's own essence.
	Standby   int
	BuyOut    int
	BuyOutWhy string
}

// underWay reports whether a round is in progress where the account stands: inside a temple or the mine, or
// carrying a load and the talisman toward the ruins.
func (in RuneInput) underWay(target int) (bool, string) {
	switch in.Where {
	case runecraftplan.RegionTemple:
		return true, "standing in the " + in.Temple + " temple"
	case runecraftplan.RegionMine:
		return true, "standing in the Rune essence mine"
	}
	if in.Known && in.Level < target && in.PackEssence >= runecraftplan.MinGo {
		if a, ok, _ := runecraftplan.PickAltar(in.Level, in.Members, nil); ok && in.PackTalisman[a.Key] {
			return true, strconv.Itoa(in.PackEssence) + " essence and the " + a.Talisman + " in the pack"
		}
	}
	return false, ""
}

// ResumeFails is how many rounds of a kind may fail in a row before a round of that kind under way OUTSIDE a temple or the mine
// stops being resumed until its backoff is over. A resume was decided before the mule is heard and before any backoff (a restart
// inside a temple must not wait for the bus), so a round that kept failing at the same step (the walk to the ruins, say) was
// resumed again at every check, a minute apart, with the pack's load still on the account.
const ResumeFails = 2

// resumeHeld reports whether the resume of a round of mode standing outside a temple or the mine is held off: it has failed
// ResumeFails times in a row and is still backing off. Inside a temple or the mine the resume is the way out and is never held.
func resumeHeld(led *Ledger, mode Mode, now time.Time) bool {
	if led.Fails(mode) < ResumeFails {
		return false
	}
	_, backedOff := led.BackedOff(mode, now)
	return backedOff
}

// runeResume is the Runecrafting round that is already under way, decided before any check of the mule: a
// restart inside a temple must not wait for the mule's heartbeat while ScarabSwarm starts walking from there. A round on the road
// with its load (outside a temple and the mine) is resumed only until it has failed ResumeFails times in a row (resumeHeld).
func runeResume(set Settings, s Snapshot, led *Ledger) (Decision, bool) {
	if !set.Rune.On {
		return Decision{}, false
	}
	if ok, why := s.Rune.underWay(set.Rune.LevelTo); ok {
		inside := s.Rune.Where == runecraftplan.RegionTemple || s.Rune.Where == runecraftplan.RegionMine
		if !inside && resumeHeld(led, ModeRunecraft, s.Now) {
			return Decision{}, false
		}
		return Decision{Mode: ModeRunecraft, Resume: true, Why: "a Runecrafting round is under way: " + why}, true
	}
	return Decision{}, false
}

// decideRune is the Runecrafting round's trigger for a new round: Runecrafting is below craftRuneLevelTo,
// the altar's talisman and an essence source exist (the bank, the mule's published stock, or mining), and no
// pause or backoff holds it off. It returns the reason when it does not start one.
func decideRune(set Settings, s Snapshot, led *Ledger) (Decision, string) {
	if !set.Rune.On {
		return Decision{}, ""
	}
	in := s.Rune
	if !in.Known {
		return Decision{}, "runecraft: the Runecrafting level has not been read yet"
	}
	if in.Level >= set.Rune.LevelTo {
		return Decision{}, "runecraft: Runecrafting " + strconv.Itoa(in.Level) + " has reached " + KeyRuneLevelTo + " " + strconv.Itoa(set.Rune.LevelTo)
	}
	if until, ok := led.BackedOff(ModeRunecraft, s.Now); ok {
		return Decision{}, "runecraft rounds are backing off until " + until.Format("15:04") + " after " + strconv.Itoa(led.Fails(ModeRunecraft)) + " failed in a row"
	}
	if s.Now.Before(led.Rune.PauseUntil) {
		return Decision{}, "runecraft rounds are paused until " + led.Rune.PauseUntil.Format("15:04") + " (" + led.Rune.LastStop + ")"
	}
	altar, ok, why := runecraftplan.PickAltar(in.Level, in.Members, nil)
	if !ok {
		return Decision{}, "runecraft: " + why
	}
	own := in.PackEssence
	if in.BankKnown {
		own += in.BankEssence
	}
	muleEss := stockOf(s.Stock, runecraftplan.EssenceName)
	muleTal := stockOf(s.Stock, altar.Talisman)
	ownTal := in.PackTalisman[altar.Key] || (in.BankKnown && in.BankTalisman[altar.Key] > 0)
	src := set.Rune.Essence
	essOK, essWord := false, ""
	switch {
	case own >= runecraftplan.MinGo:
		essOK, essWord = true, "this account holds "+runecraftplan.Thousands(own)+" essence"
	case !in.BankKnown && src != runecraftplan.SourceMule:
		// The bank has not been read this session and the ledger has no memory of it: the round reads it (and
		// pauses itself if the bank turns out to hold nothing).
		essOK, essWord = true, "this account's bank has not been read yet"
	}
	if !essOK && src != runecraftplan.SourceBank && src != runecraftplan.SourceMine && muleEss >= runecraftplan.MinMuleEssence {
		essOK, essWord = true, "the mule's published bank holds "+runecraftplan.Thousands(muleEss)+" essence"
	}
	if !essOK && src != runecraftplan.SourceBank && src != runecraftplan.SourceMule && in.CanMine {
		essOK, essWord = true, "essence can be mined at the "+set.Rune.Gateway+" gateway"
	}
	if !essOK && src == runecraftplan.SourceAuto && in.CanBuy && in.BuyQty >= runecraftplan.MinGo {
		essOK, essWord = true, "a market lot of up to "+runecraftplan.Thousands(in.BuyQty)+" essence is allowed (budget "+runecraftplan.Thousands(set.Rune.BuyBudget)+")"
	}
	if !essOK {
		w := "runecraft: no essence source (bank " + runecraftplan.Thousands(own) + ", mule " + runecraftplan.Thousands(muleEss)
		if src == runecraftplan.SourceBank || src == runecraftplan.SourceMule {
			w += ", mining off by " + KeyRuneEssence + " " + string(src)
		} else if !in.CanMine {
			w += ", mining: " + nonEmpty(in.MineWhy, "not possible")
		}
		if src == runecraftplan.SourceAuto {
			w += ", market lot: " + nonEmpty(in.BuyWhy, "off")
		}
		return Decision{}, w + ")"
	}
	if !ownTal && muleTal <= 0 {
		return Decision{}, "runecraft: no " + altar.Talisman + " in the pack or the bank, and the mule publishes none"
	}
	plan := runecraftplan.BudgetFrom(max(in.XP, runecraftplan.XPForLevel(max(1, in.Level))), set.Rune.LevelTo, in.Members)
	return Decision{Mode: ModeRunecraft, Why: "Runecrafting " + strconv.Itoa(in.Level) + " -> " + strconv.Itoa(set.Rune.LevelTo) +
		" needs " + runecraftplan.Thousands(plan.Total) + " essence (" + plan.Describe() + "); " + essWord}, ""
}

// RuneKeepNames are the item names ScarabSwarm's loot hand-off must keep on this account while the round is
// on and unfinished: the talismans and the essence (the swarm's consolidate classes them as loot and would
// carry them straight back to the mule).
//
// With the Nature gate on (craftRuneManual or craftRuneCampaign) the Nature talisman and the Nature runes join them AT ANY LEVEL: the
// keep (craftRuneKeep) sits in the main account's bank between rounds, and a daily consolidate that handed it to the mule would be the
// "trading all then asking back" the keep exists to end.
func RuneKeepNames(set Settings, level int) []string {
	if !set.Rune.On {
		return nil
	}
	var out []string
	if level < set.Rune.LevelTo {
		out = append(out, runecraftplan.EssenceName)
		for _, a := range runecraftplan.Altars() {
			if !a.Members {
				out = append(out, a.Talisman)
			}
		}
	}
	if set.Rune.Manual || set.Rune.Campaign {
		out = append(out, runecraftplan.Nature.Talisman, "Nature rune")
	}
	return out
}
