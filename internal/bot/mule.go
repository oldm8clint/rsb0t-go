package bot

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/mulestatus"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/phaselog"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/fisherrunner"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/mulerunner"
	"rsb0t-go/internal/scripts/orderplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("MuleReceiver", func() Bot { return &muleBot{} })
}

// muleBot watches for incoming trade requests and accepts them from an allowed partner —
// exactly the live-proven mechanics cmd/muletest already cleared (see
// internal/scripts/mulerunner, extracted from that tool so both share one implementation),
// just driven by a Runner instead of a linear main().
//
// MuleReceiver's natural unit of work is "one trade," not "one obstacle" (see
// internal/bot/barbarian.go) or "one fish+bank phase" (see internal/bot/fisher.go) — but
// unlike either of those, there is no guarantee a trade request ever arrives, so a Loop call
// that blocked on mulerunner.RunCycle with no timeout would never return control to the
// Runner between trades and Stop() would depend entirely on ctx.Delay's own mid-wait
// cancellation rather than ever getting a clean "yield" point. Each Loop call instead runs
// RunCycle bounded by idleTimeoutSec: it returns as soon as one trade is fully handled
// (accepted, declined, or interrupted mid-flight by a stop request), or after the timeout
// elapses with nothing at all — whichever comes first — then yields back to the Runner
// either way, giving Stop() a real checkpoint even during a long quiet spell and giving a
// dashboard a periodic "still waiting" log line rather than total silence.
//
// Scope, deliberately narrow but wider than before — this wraps cmd/muletest's own proven
// mechanism plus the wall-bus's own *receiving* half (see below), not mulereceiverplan's
// fuller trip-planning/rune-kit/PM-order design (a separate, larger integration left for
// whoever picks that up next): still no travelling to a pickup, no PM orders, no idle
// alching. Banking is the same best-effort "deposit what was just received in the nearest
// bank, if one is close by" cmd/muletest itself did — not a trip home.
//
// ---- the wall bus (internal/mulebus) ----
//
// rsb0t-scripts' MuleReceiver.ts coordinated with donor scripts over a same-origin
// BroadcastChannel (muleProtocol.ts) shared by every bot tab in one Electron renderer.
// This project runs every account as a goroutine in one process instead of a tab in one
// renderer, so internal/mulebus replaces that transport with a plain in-process, shared,
// polled message board — see its own package doc for why that is "simpler than the
// original, not harder" but still new design, not a line-for-line port.
//
// This bot publishes a "mule-here" heartbeat (current tile, free pack slots, always
// "home" state — see the honest limit below) every mulebus.HeartbeatInterval, and drains
// every other message kind on each Loop call via a mulebus.Watcher: "acct"/"cancel"
// are fed into a real mulereceiverplan.PickupQueue (the exact same pure queue
// MuleReceiver.ts's own `pickups` field used, already ported), and "order"/"ready" are
// logged so an operator watching the dashboard can see them arrive. A "collect" from
// anyone not on allowedPartners is logged once and dropped, the same allow-list
// completeTrade already enforces on the trade side.
//
// **What this does NOT yet do, stated plainly**: it never acts on a queued pickup — no
// travel, no trade initiated toward a donor, and it always announces "home" (the
// outbound/meeting/homebound states real trip machinery would need don't exist here yet).
// Building that is real, separate follow-on work (a stage machine much like
// barbarianplan.DecideStage's, per COLLAB.md's own template) — this wires the mule's own
// half of the interface and makes it observable, honestly short of driving a real hand-off
// end to end. Requires ctx.Bus; skipped entirely (logged once) if it's nil.
//
// Settings, all optional except allowedPartners (read via IntSetting/BoolSetting/
// stringListSetting, so both Go values and the shapes a JSON-decoded fleet.config.json
// roster entry produces work):
//
//	allowedPartners []string           — REQUIRED, no default: every partner name this
//	                                      account will reciprocate a trade request from,
//	                                      accept an already-open offer screen from, or take
//	                                      a wall-bus pickup ask from. Accepts a Go []string,
//	                                      a JSON array of strings, or a single
//	                                      comma-separated string (reusing
//	                                      mulereceiverplan.WithNames' own parsing, the same
//	                                      convention its "partners" setting already uses).
//	                                      Matching cmd/muletest's own hard requirement:
//	                                      OnStart fails rather than silently accepting from
//	                                      anyone if this is empty, even for a quick test.
//	postTile        tile  default the Catherby bank (2809,3441,0) — where this mule posts:
//	                                      the tile it stands on, walks back to after an event
//	                                      or a bank trip, and banks at. Takes any shape
//	                                      settings.Bag.Tile accepts, so a JSON roster can say
//	                                      "2725,3491" (the Seers' Village stand every donor
//	                                      script already defaults its own muleAtTile to),
//	                                      {"x":2725,"z":3491} or [2725,3491]; the level is
//	                                      optional and 0 when left out. THE DEFAULT IS
//	                                      DELIBERATELY THE CATHERBY BANK, unchanged from
//	                                      before this setting existed: a second fleet runs
//	                                      this same code with its mule posted there, and a
//	                                      changed default would relocate it silently. A
//	                                      roster that wants the mule anywhere else names the
//	                                      tile. OnStart logs which of the three cases it took
//	                                      (set, given-but-unreadable, or absent) because a
//	                                      mis-typed tile setting is otherwise invisible: the
//	                                      bag hands back the default and the run looks healthy
//	                                      while standing in the wrong town.
//	travelToRequests bool default false — go to the donors instead of waiting for them: group
//	                                      every live pickup request by the place it was asked
//	                                      from, travel to one camp, serve everyone there, then
//	                                      travel once to the next (the account owner's own ask,
//	                                      2026-09-20 — the fishers' camp is the Catherby bank,
//	                                      the woodcutters' the Seers' Village bank). OFF by
//	                                      default: a mule that leaves its post is a behaviour
//	                                      change for every roster that never asked for one, and
//	                                      a second fleet runs this code with a fixed post. With
//	                                      it off nothing in muletrip.go runs at all. Needs
//	                                      wallBus, since the requests arrive on the bus.
//	walkRadius      int   default 150   — a camp within this many tiles is walked to. Past it
//	                                      mulereceiverplan.PlanTrip needs a teleport it can
//	                                      afford BOTH ways, and this mule does not cast, so the
//	                                      trip is refused out loud. The default covers the 84
//	                                      tiles between the two banks this fleet uses.
//	maxLegTiles     int   default 150   — PlanTrip's own cap on one leg.
//	campRadius      int   default 25    — how close two requests must be to count as one camp,
//	                                      and how close the mule must land to have arrived.
//	campDwellSec    int   default 180   — the longest one batch is served before the mule
//	                                      re-chooses. A batch that outlives its donors' own
//	                                      patience (donorplan.MeetTimeout) sheds its tail; the
//	                                      leftovers re-ask and are then the oldest in the queue.
//	maxCampsInARow  int   default 3     — batches served at one camp in a row before another
//	                                      waiting camp takes its turn regardless of age. 0
//	                                      switches the cap off and leaves the choice on age
//	                                      alone. This is what stops 44 fishers starving two
//	                                      woodcutters.
//	bank            bool  default true — deposit what was received in the nearest bank
//	                                      booth after each accepted trade, best-effort
//	                                      (skipped with a log line if ctx.Pack/ctx.Locs/
//	                                      ctx.Components aren't all available, or no booth
//	                                      is within range) — mirrors cmd/muletest's own
//	                                      -locs/-config optionality.
//	maxTrades       int   default 0    — stop after accepting this many trades; 0 means run
//	                                      until Stop() is called. cmd/muletest's own default
//	                                      was 5, as a supervised-first-run guard; the
//	                                      registered bot defaults to unlimited, matching
//	                                      BarbarianAgility's/ProgressiveFisher's own
//	                                      maxLaps/maxCycles convention (0 = unlimited).
//	idleTimeoutSec  int   default 60   — how long one Loop call waits for a trade request
//	                                      before yielding back to the Runner with nothing to
//	                                      report. Purely a yield/observability knob, not a
//	                                      behavior change: Stop() already interrupts a wait
//	                                      immediately via ctx.Delay regardless of this value.
//	kitBatch        int   default 3 (1..5) -- how many queued kit orders one bank visit draws and trades out
//	                      one after the other (mule_kit.go, mulerunner.HandoutKits); 1 serves one kit a visit
//	handout         bool  default true — act on wall-bus item orders: withdraw what a blocked
//	                                      donor asked for, open a trade with it and hand it
//	                                      over (see the "hand-outs" section further down this
//	                                      file, and internal/scripts/orderplan for every safety
//	                                      rule). Requires wallBus. false keeps the previous
//	                                      behaviour exactly: orders are heard and logged only.
//	handoutItems    []string             — the CURATED catalogue, each item on its own terms.
//	                                      Defaults to orderplan.DefaultCatalog (the fishing
//	                                      ladder's tools, a knife, coins, crystal keys, the four
//	                                      cheaper axes). Same list-shape parsing as
//	                                      allowedPartners. It is no longer the only set that may
//	                                      be handed out — see handoutAnything — but a catalogue
//	                                      entry still always wins, so every hand-picked ceiling,
//	                                      floor and reserve on these items still applies.
//	handoutAnything bool  default true — serve an order naming an item the catalogue is silent
//	                                      about out of whatever the bank really holds. The fleet
//	                                      owner's instruction, 2026-09-22: the live mule's bank
//	                                      holds 226 items and the catalogue reaches eleven of
//	                                      them, so its one Adamant axe could not be given to
//	                                      anyone. false restores the pre-2026-09-22 behaviour
//	                                      exactly. Bounded by the three knobs below and by
//	                                      allowedPartners; see orderplan.OpenStock.
//	anyMaxPerOrder  int   default 1000  — most units of ONE uncatalogued item per order.
//	anyPerWindow    int   default 10000 — most units of one uncatalogued item to everyone
//	                                      combined per handoutWindowMin.
//	anyKeepBack     int   default 0     — a reserve of every uncatalogued item that stays in the
//	                                      bank. Zero by default on purpose: the axe the owner
//	                                      wants handed out is the only one there is.
//	handoutNever    []string             — extra names the "anything" path must never serve,
//	                                      added to orderplan.NeverOpen (Coins, Crystal key,
//	                                      Half of a key — each kept to its catalogue terms).
//	handoutRuneFloor int  default 20000   — THE RUNE POOL (a user request: "stack up 20k of
//	                                      runes before it starts handing out 1k of each"): a gated
//	                                      rune is handed out only while the mule holds at least
//	                                      this many of it. A gate, not a reserve: at 20,000 it
//	                                      serves, at 19,000 it waits until the stack is rebuilt.
//	                                      0 switches the rule off.
//	handoutRuneMax  int   default 1000    — the most of one gated rune one hand-out gives.
//	handoutRunes    []string default ["Chaos rune","Death rune","Blood rune"] — the gated runes
//	                                      (the ones the pickpockets pay; the thieves hand them
//	                                      over in lots of thousands, consolidatePocketHold). Every
//	                                      other rune is handed out as before. A rune in handoutItems
//	                                      is on the terms given there, as every catalogue entry is.
//	handoutMaxPerOrder int default 1     — most units of one item in a single order. An order
//	                                      above this is capped down to it, never exceeded.
//	handoutPerDonor int   default 1      — most orders one donor may have fulfilled per window.
//	handoutPerItem  int   default 10     — most units of any one item handed to everyone
//	                                      combined per window; the cap that actually bounds a
//	                                      bug or a compromised donor name, since the per-donor
//	                                      cap alone means little across ~255 accounts.
//	handoutWindowMin int  default 30     — the window, in minutes, both caps are measured over.
//	wallBus         bool  default true — publish heartbeats and listen for pickup/order
//	                                      messages on ctx.Bus (see above). false disables
//	                                      the bus entirely, matching cmd/muletest's own
//	                                      pre-bus behavior exactly.
//	cookIdle        bool  default false — account owner's own ask (2026-09-16): cook the
//	                                      fleet's accumulated raw fish at the real range
//	                                      next to Catherby's bank instead of just standing
//	                                      idle between trades. Off by default so this never
//	                                      surprises an operator running MuleReceiver
//	                                      without expecting it. When on, one bounded
//	                                      cook-and-bank round (mulerunner.CookRound —
//	                                      lowest cooking-level tier still banked and
//	                                      within the mule's own real Cooking level, a full
//	                                      pack's worth, cooked out, banked) runs only in
//	                                      Loop's own !res.Handled branch, i.e. only once a
//	                                      whole idleTimeoutSec has passed with no trade
//	                                      request seen at all — muling a real trade always
//	                                      takes priority, never interrupted mid-cook,
//	                                      since RunCycle is checked fresh every Loop call
//	                                      before this ever runs (see mulerunner/cook.go's
//	                                      own doc comment for the full account, including
//	                                      the real door this requires walking through).
//	joinKeyHalves   bool  default true  — owner's own ask (2026-09-20): while idle, withdraw
//	                                      14 of each banked key half, use them on each other
//	                                      to make crystal keys, and bank those
//	                                      (mulerunner.JoinKeyHalvesRound). Runs in the same
//	                                      quiet window as the openables pass, straight after
//	                                      it and before any cooking, because opening a casket
//	                                      is what produces a half. On by default, unlike
//	                                      cookIdle, because it never leaves the mule's own
//	                                      post — it is a bank open, a pack of halves and a
//	                                      few ticks of clicking — so it cannot make the mule
//	                                      miss a trade the way a walk to the range can. Set
//	                                      it false on a mule whose bank should be left alone.
type muleBot struct {
	// trapWaitLogged keeps ensureHome from repeating its Mime/maze wait line every cycle.
	trapWaitLogged bool
	// openablesDue asks the next quiet cycle to open any caskets and oysters in the bank
	// before cooking (owner, 2026-09-19). Set at start, whenever a trade brings one in, when a
	// pass ends with some still banked, and when a cook round finds any in the bank (DeferCook).
	openablesDue bool
	// openablesRetryAt pauses the openables job after a pass that could open none of the ones
	// still banked, so a casket that will not open cannot stop cooking for good.
	openablesRetryAt time.Time
	// joinKeys/keysDue/keysRetryAt are the key-half joining job (owner, 2026-09-20), the same
	// shape as the openables job above and deliberately scheduled straight AFTER it: a casket
	// is what produces a key half ([opheld1,casket]), so joining first would work on a bank the
	// openables pass is about to add to. keysDue is set at start, whenever a trade brings a half
	// in, and whenever a round leaves a joinable pair still banked; keysRetryAt pauses the job
	// after a round that could join none of them, so halves that will not join cannot stop the
	// cooking that follows.
	joinKeys    bool
	keysDue     bool
	keysRetryAt time.Time

	// walker plans the mule's recovery walks over webwalk's door/gate/stair/ship-fare graph —
	// the same live.NewWalker ProgressiveFisher builds (see fisher.go's walker field). Without
	// it every recovery walk fell back to plain same-level pathfinding, which cannot see a gate
	// more than a door-scan radius away nor board a ship. Live 2026-09-19: a fail-teleport left
	// `mule` at Brimhaven's northern dock (2772,3235). The Brimhaven->Ardougne ship boards two
	// tiles away and Luthas's banana fare job is reachable through the gate at 2816,3182, but the
	// plain walker saw neither and the mule stayed stranded across a fleet restart with zero
	// trades completed. nil (plain-walker fallback) if live.NewWalker fails at OnStart.
	walker *webwalk.Walker

	watcher     *mulerunner.RequestWatcher
	allowed     map[string]bool
	partners    []string
	bank        bool
	maxTrades   int
	tradesDone  int
	idleTimeout time.Duration

	wallBus      bool
	busWatcher   *mulebus.Watcher
	pickups      *mulereceiverplan.PickupQueue
	lastAnnounce time.Time
	warnedNoBus  bool
	// warnedReady keeps the allow-list refusal in pollBus to one line per donor name rather
	// than one per announcement. Bounded by the roster, like orders and incoming beside it.
	warnedReady map[string]bool

	// cookIdle: see OnStart's own doc comment on the cookIdle setting.
	cookIdle bool
	// cookAfterIdle is how long the stand must stay genuinely quiet before an idle cooking
	// round may start, and lastTradeAt is when this bot last completed one. See OnStart's
	// own comment where cookAfterIdle is read for the measured reason it is not the same
	// value as idleTimeout.
	cookAfterIdle time.Duration
	lastTradeAt   time.Time
	// incoming holds donors that announced a hand-off over the bus (KindReady) and when, until
	// they trade, cancel or incomingTTL passes. With none pending the mule cooks at once
	// (owner 2026-09-19: no idle waiting unless a donor says it wants to hand off).
	incoming map[string]time.Time
	// incomingSeen is when each announced donor was last HEARD from (its announcement, or a
	// KindCollect heartbeat it repeats every mulebus.CollectInterval while it waits near the
	// post). incomingTTL runs from this, not from the announcement, so a donor that is still
	// standing in the queue is never expired out of it while it waits its turn; incoming itself
	// keeps the announcement time, which is the queue ORDER. Keyed like incoming, by muleKey.
	incomingSeen map[string]time.Time
	// enroute holds donors that have drawn a load for this mule and are still walking over
	// (mulebus.KindReserve), keyed by muleKey, until they announce, cancel or enrouteTTL passes.
	// It is counted in the queue depth the heartbeat publishes and in NOTHING else: in
	// particular it never stops a cook round (owner 2026-09-19 -- a mule must not stand idle for
	// a donor who is not there yet).
	enroute map[string]time.Time
	// lastEarlyExit, waitStarted and earlyWaited back Deps.LeaveWait: when the last early exit
	// from the idle wait happened, when the current wait began, and how long the last early exit
	// waited (for the "left the wait early" line).
	lastEarlyExit, waitStarted time.Time
	earlyWaited                time.Duration
	// lastBusTick rate-limits busTick.
	lastBusTick time.Time

	// cycleSeen is this mule's OWN measured hand-off cycle, an exponential moving average of the
	// gap between consecutive completed trades, and it is what a queuing donor's eta is built
	// from. Zero until the mule has completed two trades back to back with somebody still
	// queued behind them; cycle() falls back to muleCycle until then. See cycle().
	cycleSeen time.Duration

	// post is where this mule stands: its trading post, its idle stand and the bank it uses,
	// read once from the "postTile" setting (see OnStart). It defaults to the Catherby bank,
	// which is where every mule in this codebase posted before the setting existed — a mule
	// that is already running somewhere must not move because a key was added. Read through
	// postTile(), never directly, so a muleBot built without OnStart still has a post.
	post tile.Tile

	// homeTile is the bank trading post this run is holding — post, captured on the first
	// ensureHome. Event recovery must return here before the mule announces availability or
	// takes another trade.
	homeTile *tile.Tile

	// travel and the four numbers beside it are the "travelToRequests" batching settings; trip
	// is the journey under way, nil whenever the mule is posted. servedAt/servedInARow are the
	// starvation cap's memory: which camp the last batches were served at, and how many in a
	// row. See muletrip.go — with travel off, trip stays nil and none of it runs.
	travel         bool
	walkRadius     int
	maxLegTiles    int
	campRadius     int
	maxCampsInARow int
	campDwell      time.Duration
	trip           *muleTrip
	servedAt       *tile.Tile
	servedInARow   int
	// returningHome marks the walk back after a batch, so the trip home is not reported as a
	// random event having moved the mule. Only ever set while travelling.
	returningHome bool

	// handout/handoutPolicy/handoutLedger/orders: see the "hand-outs" section below and the
	// handout* settings in OnStart's own doc comment. orders is keyed by lower-cased donor
	// name so a donor re-publishing while it waits refreshes one entry rather than stacking.
	handout       bool
	handoutPolicy orderplan.Policy
	handoutLedger *orderplan.Ledger
	orders        map[string]string
	// ordersAt is when each queued order was last heard (its donor re-publishes it every
	// donorrunner.AskRepeatEvery while it waits); pruneOrders drops one not heard for
	// muleOrderTTL. Scarab lane P round 3, NB3 -- a declared narrow edit in lane H's file.
	ordersAt map[string]time.Time
	// orderAt is when each queued order (same key) was first queued: served oldest first.
	// Not ordersAt: a re-publish refreshes that one every ten seconds, which would reorder
	// the queue by who spoke last.
	orderAt map[string]time.Time
	// served remembers, per donor (the same key as orders), the order text serveOrder last
	// finished and when, so the re-publishes a waiting donor made DURING that hand-out are not
	// queued and served a second time. See orderplan.StaleRepeat.
	served map[string]servedOrder
	// serving is the order serveOrder is fulfilling this moment; see takeOrder.
	serving servingOrder
	// bankPublishedAt is when this mule last published its bank to the fleet (mule_bankseen.go).
	bankPublishedAt time.Time
	// kits, servingKit and servedKits are the bundled kit orders' own queue, in-progress and
	// just-served records (mule_kit.go), keyed like orders.
	kits       map[string]queuedKit
	servingKit servingKitOrder
	servedKits map[string]servedKit
	// kitTurnOwed is set when a kit was served while fishers waited, and cleared by the next
	// RunCycle turn: see mule_kit.go.
	kitTurnOwed bool
	// load is the rolling record of kit waits, idle time and cook rounds the mule logs (loadstats.go), loadSaid when.
	load     mulereceiverplan.LoadStats
	loadSaid time.Time
	// kitBatch is how many queued kits one bank visit may draw and trade out (setting kitBatch, default
	// orderplan.KitBatchMax; 1 serves one kit a visit, as before): mule_kit.go, mulerunner.HandoutKits.
	kitBatch int
	// cookBuffer is the cooked-food stock to hold (setting cookBufferFood, 0 = off); cookedSeen,
	// rawSeen and cookedKnown the bank's last reading of it, lastBufferRound when a buffer round last
	// started and cookingBuffer whether one is running (mule_cookbuffer.go).
	cookBuffer      int
	cookedSeen      int
	rawSeen         int
	cookedKnown     bool
	lastBufferRound time.Time
	cookingBuffer   bool

	// restock is this mule's own coin-replenishment state, nil when the feature is off
	// (the default). One field, in the fisherhandout.go/woodcutterhandout.go style, so
	// internal/bot/mulerestock.go stays additive to this file — see COLLAB.md.
	restock *muleRestock

	// status is this mule's LIVE state for the control panel's mule dashboard
	// (internal/mulestatus): donors on the way, the trade in progress and its stage, cycle
	// timing. Published through ctx.Publish; finished trades go to the fleet's trade record
	// through recordTrade/recordHandout (muleledger.go), never kept here. Reporting only:
	// nothing in this file reads it back to decide anything.
	status *mulestatus.Tracker
}

// The mule returns to its exact bank tile between work cycles.

func muleFreshStartDeps(d *mulerunner.Deps, w *webwalk.Walker) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: freshstartrunner.Delayer(d.Delay), Log: d.Log,
		Walker: w,
	}
}

// muleFisherDeps adapts mulerunner.Deps to fisherrunner.Deps, the same field-for-field copy
// muleFreshStartDeps does for freshstartrunner.Deps — needed for goToCatherby below to reuse
// fisherrunner.ReturnToCatherby (same-level walk, InCatherby short-circuit, and its own
// cross-water freshstartrunner fallback) rather than re-deriving a second, weaker version of
// the exact travel logic ProgressiveFisher already relies on for this identical trip.
func muleFisherDeps(d *mulerunner.Deps, w *webwalk.Walker) *fisherrunner.Deps {
	return &fisherrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: fisherrunner.Delayer(d.Delay), Log: d.Log,
		Walker: w,
	}
}

// goToCatherby walks a not-yet-arrived mule to Catherby before homeTile is ever captured —
// account owner's own ask (2026-09-16): the mule was manually started with a temporary
// ProgressiveFisher run just to relocate it near Catherby before switching back to
// MuleReceiver, because MuleReceiver itself had no notion of "get to my post" at all —
// ensureHome only ever captured wherever the account happened to already be, home tile and
// all, which is how the mule's home ended up pinned at Seers the first time this was tried.
// Reusing fisherrunner.ReturnToCatherby is deliberately "the same fresh start as the fishing
// script" users asked for: same same-level WalkNear attempts, same InCatherby
// short-circuit if already there, and the same travelViaFreshStart cross-water fallback for
// an account that starts out stranded on the wrong side of the water entirely — not a
// second, hand-rolled travel routine that could drift from what's already proven live.
// meetTileRadius is how close to the exact bank tile counts as "already at the post,"
// deliberately much tighter than fisherplan.InCatherby's own 60-tile "somewhere in the
// area" radius: a fisher hand-off walks to a specific fixed tile (its own "muleAtTile"
// setting, 2809,3441,0 by default — see internal/bot/fisher.go) and then needs a real
// adjacent player to trade with, not just any account somewhere in Catherby. Bank fishing
// itself only ever leaves an account within the bank's own click radius of that tile, so
// this stays tight without risking a spurious extra walk once actually posted.
const meetTileRadius = 5

// NOTE (2026-09-20): nothing calls this. ensureHome already does the same job for any post —
// it captures postTile as homeTile on the first Loop call and walks there, through returnHome
// when webwalk is available — so this is a second, weaker copy of that route. It is kept,
// parameterised by the post rather than hardcoded to Catherby, so that reviving it cannot
// quietly reintroduce the Catherby assumption this setting exists to remove.
func (b *muleBot) goToPost(ctx *Context) {
	here := currentTile(ctx)
	if here == nil {
		return
	}
	dest := b.postTile()
	if here.Level == dest.Level && tile.Chebyshev(*here, dest) <= meetTileRadius {
		return
	}
	if ctx.Pack == nil {
		ctx.logf("mule: not at the post and no collision pack configured — cannot travel there; staying put")
		return
	}
	ctx.logf("mule: not at the post %d,%d,%d (at %d,%d,%d) — walking there before taking up post", dest.X, dest.Z, dest.Level, here.X, here.Z, here.Level)
	ctx.SetPhase(phaselog.Walking)
	// ReturnToCatherby is Catherby's own travel routine and says nothing about any other
	// town, so it only applies when the post is in Catherby. Elsewhere the local walk below
	// is the whole of this function.
	fd := muleFisherDeps(b.deps(ctx), b.walker)
	if fisherplan.InCatherby(dest) && !fisherrunner.ReturnToCatherby(fd) {
		ctx.logf("mule: could not reach the Catherby area — will take up post wherever this ends up and keep retrying next cycle")
		return
	}
	// ReturnToCatherby's own success only means "somewhere within InCatherby's 60-tile
	// radius" (it short-circuits there already) — close the last stretch to the actual
	// booth tile fishers walk to, a short local walk once already in the area.
	ffd := muleFreshStartDeps(b.deps(ctx), b.walker)
	if freshstartrunner.WalkNear(ffd, dest, meetTileRadius, 30*time.Second) {
		ctx.logf("mule: arrived at the post %d,%d,%d", dest.X, dest.Z, dest.Level)
	} else {
		ctx.logf("mule: could not close the last stretch to the post tile — taking up post here instead")
	}
}

// hasSetting reports whether a key is present at all, whatever its value.
func hasSetting(m map[string]interface{}, key string) bool {
	_, ok := m[key]
	return ok
}

// tileSettingSet reports whether key holds a value that settings.Bag.Tile can actually read
// as a tile. It asks twice with two different fallbacks: an unreadable value hands each
// fallback straight back, a readable one returns the same real tile both times.
//
// Why not just compare the result against the default: a roster that deliberately names the
// default tile would then be reported as a typo, and the whole point of the check is to tell
// "you asked for this" apart from "your key did nothing". A wrong OPTION NAME has produced a
// silent, healthy-looking no-op in this codebase before (a spell setting pinned to "Air"
// named a rune and a staff but no spell), which is why the caller says out loud which case
// it took rather than trusting the value it got back.
func tileSettingSet(m map[string]interface{}, key string) bool {
	a := tile.Tile{X: -1, Z: -1}
	b := tile.Tile{X: -2, Z: -2}
	return fisherTileSetting(m, key, a) != a || fisherTileSetting(m, key, b) != b
}

// currentTile mirrors freshstartrunner's own unexported helper of the same name — a plain
// tile.Tile read via ctx.Session.State().Tile(), nil when no position is known yet.
func currentTile(ctx *Context) *tile.Tile {
	x, z, level, ok := ctx.Session.State().Tile()
	if !ok {
		return nil
	}
	return &tile.Tile{X: x, Z: z, Level: level}
}

// guardianWaitLogEvery matches barbarianrunner/fisherrunner's own identical constant —
// logging once and then at most this often while still waiting, instead of once per
// 500ms poll. See barbarianrunner.WaitForGuardian's doc comment for the real production
// case (a 2+ minute evade event, hundreds of near-identical "waiting" lines) this is for.
const guardianWaitLogEvery = 10 * time.Second

// waitForGuardian pauses while a random event is detected at all — pending or already being
// handled by the account's background Guardian — mirroring barbarianrunner.WaitForGuardian/
// fisherrunner's own identical helper (see their doc comments): a script should never open a
// trade offer screen, click a bank, or start any other interaction on top of an event the
// Guardian hasn't finished with yet. MuleReceiver had no such check before this — a real,
// confirmed gap, since it's the one registered script that previously never paused for a
// pending event at all before acting. Returns false if a stop was requested while waiting.
// Uses randomevents.Busy rather than raw Detect — see fisherrunner.WaitForGuardian's own
// doc comment for the real live gap this closes (Detect() alone can't tell an event
// Guardian has genuinely given up on for now from one it's still actively working).
// emergencyFleeDriftTiles is 0 — this bot opts out of EmergencyFlee's own drift check
// (see that function's own doc comment on why every caller now supplies its own
// home/driftTiles pair) rather than passing a static reference: muleBot already tracks its
// own dynamically-captured homeTile (see ensureHome/ensureNotDisplaced above), which is
// strictly more precise than any fixed point could be for an account that could be posted
// anywhere, and running both at once would risk the exact competing-recovery race this
// whole night's fixes have been closing elsewhere.
const emergencyFleeDriftTiles = 0

var emergencyFleeHome pathfind.NavPoint // unused while emergencyFleeDriftTiles <= 0

func waitForGuardian(ctx *Context) bool {
	randomevents.EmergencyFlee(ctx.Session, ctx.Pack, emergencyFleeHome, emergencyFleeDriftTiles, ctx.Log)
	if !randomevents.Busy(ctx.Session) {
		return true
	}
	ctx.logf("[guardian] a random event is present; waiting")
	defer ctx.PushPhase(phaselog.Event)()
	lastLog := time.Now()
	for randomevents.Busy(ctx.Session) {
		randomevents.EmergencyFlee(ctx.Session, ctx.Pack, emergencyFleeHome, emergencyFleeDriftTiles, ctx.Log)
		if !ctx.Delay(500 * time.Millisecond) {
			return false
		}
		if time.Since(lastLog) >= guardianWaitLogEvery {
			ctx.logf("[guardian] still waiting on a random event")
			lastLog = time.Now()
		}
	}
	return true
}

// ensureHome returns to the bank post after event handling, banking or cooking.
// Failed recovery keeps the mule unavailable and retries on its next cycle.
// postTile is where this mule posts: its "postTile" setting, or the Catherby bank for a
// muleBot that never ran OnStart (a test, or a future caller building one directly).
func (b *muleBot) postTile() tile.Tile {
	if b.post == (tile.Tile{}) {
		return fisherplan.Catherby().Bank
	}
	return b.post
}

// postSlack is how far from its post tile the mule may stand and still be at its post. Banking,
// cooking and a trade's own pathing leave it a tile off (2810,3441 in front of the next booth,
// 2809,3440 after stepping towards a donor), and the walk back that an exact match demanded
// collided with the next trade: live on 73ff4228, 23:46-23:59 on 4 Oct 2026, the mule started a
// one-tile walk home, reciprocated a donor's request a second later, and the step closed the
// trade screen it had just opened (Acct176, Acct213; each retry then succeeded). The heartbeat
// also read the one tile as homebound, so donors waited for a mule that was already there.
const postSlack = 1

// atPost reports whether here counts as standing at the post home.
func atPost(here, home tile.Tile) bool {
	return here.Level == home.Level && tile.Chebyshev(here, home) <= postSlack
}

func (b *muleBot) ensureHome(ctx *Context) bool {
	if b.homeTile == nil {
		home := b.postTile()
		b.homeTile = &home
	}
	x, z, level, ok := ctx.Session.State().Tile()
	if !ok {
		return false
	}
	here := tile.Tile{X: x, Z: z, Level: level}
	// Where the mule is supposed to be RIGHT NOW, which is not always its post any more. A mule
	// on a trip is not displaced — it is exactly where it decided to go — so reading every
	// non-post tile as "a random event may have moved me" and walking back would make a trip
	// impossible. With travelToRequests off b.trip is always nil and every line below this
	// branch decides precisely what it decided before.
	if b.trip != nil {
		return b.atCampOrTravel(ctx, here, b.trip.camp.At)
	}
	if atPost(here, *b.homeTile) {
		b.returningHome = false
		return true
	}
	if randomevents.InTrapEventSquare(here.X, here.Z) {
		// A Mime or maze returns the account itself when it ends; retry next cycle.
		if !b.trapWaitLogged {
			ctx.logf("in a Mime or maze event area at %d,%d — waiting for the event to send me back rather than walking", here.X, here.Z)
			b.trapWaitLogged = true
		}
		return false
	}
	b.trapWaitLogged = false
	// The walk back after serving a camp is not a displacement, and saying so would have an
	// operator hunting a random event that never happened. The recovery itself is the same one
	// either way — only the sentence changes.
	if b.returningHome {
		ctx.logf("heading back to the post at %d,%d,%d after serving a pickup camp (now %d,%d,%d)", b.homeTile.X, b.homeTile.Z, b.homeTile.Level, here.X, here.Z, here.Level)
	} else {
		ctx.logf("displaced from home (now %d,%d,%d, home is %d,%d,%d) — a random event may have moved me; attempting to walk back", here.X, here.Z, here.Level, b.homeTile.X, b.homeTile.Z, b.homeTile.Level)
	}
	ctx.SetPhase(phaselog.Walking)
	if ctx.Pack == nil {
		ctx.logf("cannot attempt recovery — no collision pack configured")
		return false
	}
	if b.walker != nil {
		// Plans with White Wolf Mountain's dens as no-go zones and takes the boat chain
		// when that is the only wolf-free way back — see mulehome.go.
		return b.returnHome(ctx)
	}
	// No webwalk: the plain walks below are all that is left, and they cannot avoid the pass.
	//
	// ReturnToCatherby is Catherby's OWN travel routine, so it may only run when the post is
	// in Catherby. A mule posted elsewhere (postTile) would otherwise be walked across the
	// map to Catherby and then asked to close the "last stretch" to a post in another town —
	// the recovery would fight the setting on every displacement. With a distant post and no
	// webwalk there is nothing better than the plain WalkNear below, which is honest about
	// failing.
	homeInCatherby := fisherplan.InCatherby(*b.homeTile)
	if homeInCatherby && fisherplan.InCatherby(here) {
		// The whole walk home below is skipped on a 60-tile radius. Say so when the tile it is
		// skipping from cannot actually reach the post; observability only, see
		// internal/scripts/fisherrunner/arrivalnote.go.
		fisherrunner.NoteArrival(ctx.Pack, ctx.Log, "the mule's displacement check", here, here, *b.homeTile)
	}
	if homeInCatherby && !fisherplan.InCatherby(here) && !fisherrunner.ReturnToCatherby(muleFisherDeps(b.deps(ctx), b.walker)) {
		ctx.logf("mule: could not return to Catherby; retrying before taking trades")
		return false
	}
	fd := muleFreshStartDeps(b.deps(ctx), b.walker)
	if freshstartrunner.WalkNear(fd, *b.homeTile, 0, 60*time.Second) {
		ctx.logf("back home")
		return true
	}
	ctx.logf("mule: could not reach the bank post; retrying before taking trades")
	return false
}

func (b *muleBot) deps(ctx *Context) *mulerunner.Deps {
	started := time.Now()
	d := &mulerunner.Deps{
		Session:    ctx.Session,
		Pack:       ctx.Pack,
		Locs:       ctx.Locs,
		Config:     ctx.Config,
		Components: ctx.Components,
		Chat:       ctx.Chat,
		Delay:      ctx.Delay,
		Log:        ctx.Log,
		Loot:       ctx.Loot,
	}
	// TELL THE FLEET WHAT IS IN THIS BANK, every time mulerunner already has it open.
	//
	// THE FLEET OWNER'S REPORT, 2026-09-22: "woodcutters are asking the mule for axes they dont
	// even have instock ... they should beable to know whats in their bank to request". They
	// had no way to know. A donor asked a fixed best-first ladder (orderplan.AxeHandouts:
	// mithril, steel, iron, bronze) and learned the answer only from the decline that came
	// back, then threw it away and asked the same four questions two minutes later -- after an
	// 83-tile walk to Catherby. Measured the same day: this mule's bank held exactly ONE
	// chopping axe, an Adamant axe, and adamant is deliberately not on that ladder at all, so
	// every rung was empty and every cycle was wasted.
	//
	// One map write into the shared peerbank directory, at a moment the bank is already open
	// and decoded: no snapshot, no fleet poll, nothing on /api/status's locks. PublishBank is
	// a no-op when the fleet has no directory (ctx.Peers nil), so a fleet without peer sharing
	// behaves exactly as it did and the donor falls back to the blind ladder.
	//
	// working is false: a mule standing at its own post IS at a bank, which is what that flag
	// means, and it only ever affects who a peer chooser prefers to interrupt.
	d.OnBankRead = func(items []gamestate.SlotItem) {
		b.noteBankFood(ctx, items)
		PublishBank(ctx, items, false)
		b.bankPublishedAt = time.Now()
	}
	// And every other bank open, rate-limited: see mule_bankseen.go.
	d.OnBankOpen = func(items []gamestate.SlotItem) { b.publishBankThrottled(ctx, items, time.Now()) }
	// A visit's closing reading is its freshest and always goes out (review of
	// claude/handoff-muleseen: the throttle dropped it whenever the open had just published).
	d.OnBankClose = func(items []gamestate.SlotItem) { b.publishBankNow(ctx, items, time.Now()) }
	d.IdlePending = func() bool {
		if actions.NewTrade(ctx.Session).Active() {
			return true
		}
		// A donor announcing a hand-off mid-round sends the mule back to its post.
		if b.busWatcher != nil && ctx.Bus != nil {
			b.pollBus(ctx)
		}
		// Only a donor that announced just now, or a trade request, sends it back: donors on a long walk kept it off the
		// range all night (5 Nov 2026: 0 cook rounds in an hour at 30% idle).
		if !b.fishersQuiet() {
			return true
		}
		// A queued wall-bus ORDER counts too, and used not to — the bug this whole
		// mechanism was supposed to prevent, missed because an order is neither a trade
		// request nor a pickup.
		//
		// pollBus above has just run takeOrder, so an order that arrived mid-round is
		// already sitting in b.orders by the time this returns; every other branch here
		// then answered "nothing pending" and the round carried on for its full ~99
		// seconds. Live 2026-09-20: spare104 published its key order at 23:02:34 while a
		// cook round was running, the round finished at 23:04:14, and by then the donor
		// had already given up (donorrunner.Await timed out at 23:04:04) and walked off
		// the stand — the hand-out then failed with "you are not in sight of me" and the
		// account went back to fishing with zero keys, against 417 sitting in this mule's
		// own bank. The donor's own wait is shorter than a cook round, so with an order
		// queued the round can only ever lose the race.
		pending := b.ordersPending()
		if b.cookingBuffer {
			pending = len(b.orders) // a buffer round runs over queued KITS only (mule_cookbuffer.go)
		}
		if pending > 0 {
			return true
		}
		if ctx.Chat == nil {
			return false
		}
		for _, line := range strings.Split(ctx.Chat.Since(started), "\n") {
			if name, ok := mulerunner.TradeRequestFrom(line); ok && busAllowed(b.allowed, name) {
				return true
			}
		}
		return false
	}
	// WalkNear backs mulerunner.CookRound's own travel to/from the Catherby range with
	// freshstartrunner's door-aware walk (tryOpenDoor/tryChangeLevel recovery) instead of
	// mulerunner's own plain FindPath+Walk — a real door sits across the only route
	// between Catherby's bank and its range (ground-truthed live, see
	// mulerunner/cook.go's own doc comment), the exact class of stall this project's own
	// WalkNear duplication rule (mulerunner.walkNear's doc comment) already found and
	// fixed elsewhere for any real multi-tile trip. Set unconditionally here rather than
	// only when cookIdle is on — cheap (a closure over already-built fields) and keeps
	// this function the one place a mulerunner.Deps gets built from a Context.
	d.WalkNear = func(dest tile.Tile, radius int, timeout time.Duration) bool {
		return freshstartrunner.WalkNear(muleFreshStartDeps(d, b.walker), dest, radius, timeout)
	}
	// Caskets and oysters come before cooking whenever the bank holds any, whichever way they
	// got there: a trade, a pass cut short, or a detection that missed. The cook round already
	// opens the bank, so checking here costs nothing extra.
	d.DeferCook = func(items []gamestate.SlotItem) bool {
		n := bankedOpenableCount(ctx.Config, items)
		if n == 0 || time.Now().Before(b.openablesRetryAt) {
			return false
		}
		b.openablesDue = true
		ctx.logf("cooking: %d casket(s)/oyster(s) in the bank — opening them before cooking", n)
		return true
	}
	// Serve the queue in the order it announced, and keep reading the bus while waiting, so a
	// donor's announcement is answered within a second (see mulerunner.Deps.Priority / Tick).
	d.Queue = func() []string {
		b.incomingPending()
		return mulereceiverplan.QueueOrder(b.incoming)
	}
	// EVERY WAIT keeps the wall bus current, at most once a second: the heartbeat (which
	// carries the queue depth donors read before drawing a load) and the donors' own messages.
	// Both used to happen only between trades, so while a queue existed -- when the mule is
	// trading or banking nearly all the time -- announcements were answered in batches up to a
	// whole trade late, and a donor deciding whether to draw read a queue depth just as old.
	// A queued order that has become servable ends the idle wait (mulereceiverplan.LeaveWait): the
	// wait cannot see a donor's cancel arrive, so a kit held behind a just-finished donor used to sit
	// out the whole idleTimeout.
	d.LeaveWait = func() bool {
		g := b.orderGate()
		if !mulereceiverplan.LeaveWait(g, b.watcher.QueuedRequests(), time.Since(b.lastEarlyExit)) {
			return false
		}
		b.lastEarlyExit = time.Now()
		b.earlyWaited = time.Since(b.waitStarted)
		return true
	}
	d.Delay = func(dur time.Duration) bool {
		b.busTick(ctx)
		return ctx.Delay(dur)
	}
	d.Stage = func(partner, stage string) {
		ctx.SetPhase(phaselog.Trading)
		if b.status != nil {
			b.status.Stage(partner, stage)
		}
	}
	return d
}

// stringListSetting reads a []string-shaped setting, tolerating the shapes a JSON-decoded
// map[string]interface{} or a literal Go value can produce: a Go []string, a JSON
// []interface{} of strings, or a single comma-separated string — parsed via
// mulereceiverplan.WithNames/ListPartners, the same convention mulereceiverplan's own
// "partners" setting already uses, rather than inventing a second name-list parser.
func stringListSetting(m map[string]interface{}, key string) []string {
	switch v := m[key].(type) {
	case []string:
		return append([]string(nil), v...)
	case []interface{}:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = mulereceiverplan.WithNames(out, s, mulereceiverplan.ListPartners)
			}
		}
		return out
	case string:
		return mulereceiverplan.WithNames(nil, v, mulereceiverplan.ListPartners)
	default:
		return nil
	}
}

func (b *muleBot) OnStart(ctx *Context) error {
	b.partners = stringListSetting(ctx.Settings, "allowedPartners")
	if len(b.partners) == 0 {
		return fmt.Errorf("mule: allowedPartners is required and empty — name every partner this run should accept from, even for a test (matches cmd/muletest's own -allow requirement)")
	}
	b.allowed = mulerunner.AllowList(b.partners)
	ctx.logf("accepting trades only from: %v", b.partners)

	// The post is read before anything else uses it: ensureHome captures it as homeTile on the
	// very first Loop call, and openBankedOpenables banks against it.
	//
	// Announced out loud, in all three cases, because a mis-typed tile setting is otherwise
	// completely silent — the bag hands back the default and the run looks healthy while
	// standing in the wrong town, with 44 donors walking to a stand nobody is on. The
	// "given but unreadable" case is told apart from "not given" by tileSettingSet rather
	// than by comparing against the default, so a roster that deliberately names Catherby's
	// own tile is not reported as a typo.
	defaultPost := fisherplan.Catherby().Bank
	b.post = fisherTileSetting(ctx.Settings, "postTile", defaultPost)
	switch {
	case tileSettingSet(ctx.Settings, "postTile"):
		ctx.logf("posting at %d,%d,%d (postTile)", b.post.X, b.post.Z, b.post.Level)
	case hasSetting(ctx.Settings, "postTile"):
		ctx.logf(`mule: postTile was given but is not a readable tile — it takes "x,z[,level]" (for example "2725,3491" for the Seers' Village stand), {"x":..,"z":..} or [x,z]; posting at the default %d,%d,%d instead`, b.post.X, b.post.Z, b.post.Level)
	default:
		ctx.logf("posting at %d,%d,%d (the default; set postTile to post somewhere else)", b.post.X, b.post.Z, b.post.Level)
	}

	// Batching. Announced the same way the post is, and for the same reason: a mule that
	// silently did or did not travel is indistinguishable in a log from one that had nothing to
	// do, and this decides whether the account ever leaves its bank.
	ts := readTravelSettings(ctx.Settings)
	b.travel, b.walkRadius, b.maxLegTiles = ts.on, ts.walkRadius, ts.maxLegTiles
	b.campRadius, b.maxCampsInARow, b.campDwell = ts.campRadius, ts.maxCampsInARow, ts.dwell
	if b.travel {
		ctx.logf("travelling to requests is ON: camps are requests within %d tiles of each other, a camp up to %d tiles away is walked to, each batch gets up to %s, and no camp is served more than %d time(s) in a row while another waits",
			b.campRadius, b.walkRadius, b.campDwell, b.maxCampsInARow)
	} else {
		ctx.logf("travelling to requests is off — waiting at the post for donors to come here (set travelToRequests to change that)")
	}

	b.bank = BoolSetting(ctx.Settings, "bank", true)
	b.maxTrades = IntSetting(ctx.Settings, "maxTrades", 0)
	idleSec := IntSetting(ctx.Settings, "idleTimeoutSec", 60)
	b.idleTimeout = time.Duration(idleSec) * time.Second
	// cookAfterIdleSec must stay comfortably ABOVE donorplan.MeetTimeout (60s), which is
	// how long a donor stands at the meet spot re-requesting every donorplan.RequestEvery
	// before giving up. Measured 2026-09-18 (see COLLAB.md's mule throughput analysis):
	// with the old behavior the cooking round started on exactly the same 60s boundary the
	// donors time out on, so the two were perfectly out of phase — the mule left for a
	// 99-second round right as the next donor was about to give up, 3 rounds took it away
	// for 296s of a 22-minute window (22%), and 28% of donor arrivals landed while it was
	// gone. 180s meant a genuinely quiet stand, not a donor mid-approach.
	//
	// 60s since 2026-09-19, the owner's call: the mule stood idle up to three minutes between
	// hand-offs. A trade request or an open trade stops a cook round (d.IdlePending, added
	// after that measurement), so a donor arriving mid-round is served after the walk back
	// rather than left to time out. Re-measure donor waits with tools/mule-throughput.py.
	// 0 since donors announce hand-offs over the bus (incoming below): the quiet-stand wait only
	// covered donors the mule could not see coming. A trade request still interrupts a round.
	cookAfterSec := IntSetting(ctx.Settings, "cookAfterIdleSec", 0)
	b.cookAfterIdle = time.Duration(cookAfterSec) * time.Second
	b.lastTradeAt = time.Now()
	b.incoming = map[string]time.Time{}
	b.watcher = mulerunner.NewRequestWatcher()
	if w, err := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); err != nil {
		ctx.logf("mule: no webwalk (%v) — recovery walks use the plain walker and cannot take ships or distant gates", err)
	} else {
		// Ship fares and the Brimhaven customs officer are NPC hops the Walker only plans
		// through with a handler installed; returnHome's wolf-free route from the east is
		// exactly that boat chain. Same wiring as BarbarianAgility's walker.
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{
				Client: w.Client, Session: ctx.Session, Config: ctx.Config,
				Components: ctx.Components, Log: ctx.Log,
			}))
		}
		b.walker = w
	}

	b.wallBus = BoolSetting(ctx.Settings, "wallBus", true)
	if b.wallBus {
		b.busWatcher = mulebus.NewWatcher()
		b.pickups = mulereceiverplan.NewPickupQueue(mulebus.CollectTTL)
	}

	b.orders = map[string]string{}
	b.handout = BoolSetting(ctx.Settings, "handout", true)
	b.kitBatch = min(max(IntSetting(ctx.Settings, "kitBatch", orderplan.KitBatchMax), 1), 5)
	// The allow list for hand-outs is deliberately the SAME list as for trades. A hand-out
	// spends this account's items exactly as a trade spends its time, so it gets the same
	// gate and there is no second list to keep in sync (the class of mistake that made
	// underscore-vs-space silently reject every partner, see mulerunner.normalisePlayerName).
	b.handoutPolicy = orderplan.DefaultPolicy(b.partners)
	// Coin ceilings sized to what this mule is expected to FUND, not to what replacing a lost
	// tool costs. Unset leaves orderplan's conservative 500/25,000, which is deliberately too
	// small for a market purchase: the Seers market wants about 150,000 for a rune axe, and an
	// order against the default is clamped to 500, so the buyer reaches the market holding
	// nothing. Raising it is a wealth-exposure decision (that constant bounds the worst case at
	// CoinsPerWindow rather than at a bank holding millions), so it lives in this mule's own
	// roster entry where it can be seen, rather than becoming everyone's new default.
	b.handoutPolicy.Catalog = orderplan.WithCoinLimits(
		b.handoutPolicy.Catalog,
		IntSetting(ctx.Settings, "coinsPerOrder", 0),
		IntSetting(ctx.Settings, "coinsPerWindow", 0),
	)
	if names := stringListSetting(ctx.Settings, "handoutItems"); len(names) > 0 {
		max := IntSetting(ctx.Settings, "handoutMaxPerOrder", 1)
		if max < 1 {
			max = 1
		}
		catalog := make([]orderplan.Handout, 0, len(names))
		for _, n := range names {
			catalog = append(catalog, orderplan.Handout{Item: n, Max: max})
		}
		b.handoutPolicy.Catalog = catalog
	}
	// HAND OUT ANYTHING THE BANK ACTUALLY HOLDS, not only what the curated catalogue names.
	//
	// THE FLEET OWNER'S REPORT, 2026-09-22: "mulereciever wasnt updated to include handing out
	// of all items in bank if requested from another account". Measured the same day against
	// the live panel: the mule's bank holds 226 distinct items and the catalogue above can hand
	// out eleven of them — including exactly ONE chopping axe, an Adamant axe, which no ask
	// could reach because orderplan.AxeHandouts stops at mithril.
	//
	// On by default, which is the owner's instruction. Off with `handoutAnything: false`, which
	// restores the pre-2026-09-22 behaviour exactly (an item off the catalogue is refused on
	// sight). See orderplan.OpenStock for what still bounds it, and for the wealth-distribution
	// argument this overrides — that argument is not discarded, it survives as the rule that a
	// catalogue entry always wins, so Coins, Crystal key and the fishing ladder keep every
	// hand-picked ceiling, floor and reserve they already had.
	if BoolSetting(ctx.Settings, "handoutAnything", true) {
		open := orderplan.DefaultOpenStock()
		open.Max = IntSetting(ctx.Settings, "anyMaxPerOrder", open.Max)
		open.WindowMax = IntSetting(ctx.Settings, "anyPerWindow", open.WindowMax)
		// A reserve on every uncatalogued item. Zero by default and it must stay zero for
		// the mule: that bank's one Adamant axe is the order this whole change exists to
		// serve, and any reserve above zero refuses it.
		open.KeepBack = IntSetting(ctx.Settings, "anyKeepBack", open.KeepBack)
		if extra := stringListSetting(ctx.Settings, "handoutNever"); len(extra) > 0 {
			open.Never = append(append([]string(nil), open.Never...), extra...)
		}
		b.handoutPolicy.Open = open
	} else {
		b.handoutPolicy.Open = orderplan.OpenStock{}
	}
	b.handoutPolicy.MaxOrdersPerDonor = IntSetting(ctx.Settings, "handoutPerDonor", b.handoutPolicy.MaxOrdersPerDonor)
	b.handoutPolicy.MaxUnitsPerItem = IntSetting(ctx.Settings, "handoutPerItem", b.handoutPolicy.MaxUnitsPerItem)
	// THE RUNE POOL (orderplan/runegate.go): the runes the pickpockets pay are handed out only from a stack of
	// handoutRuneFloor, and handoutRuneMax at a time. The ScarabSwarm accounts read the same rule (muleRune*).
	b.handoutPolicy.Runes = runeGateFrom(ctx.Settings, "handoutRuneFloor", "handoutRuneMax", "handoutRunes")
	windowMin := IntSetting(ctx.Settings, "handoutWindowMin", int(orderplan.DefaultWindow/time.Minute))
	b.handoutLedger = orderplan.NewLedger(time.Duration(windowMin) * time.Minute)
	if b.handout {
		ctx.logf("hand-outs on: will give %v (at most %d per order, %d per donor and %d of each item per %dm) to anyone on the allow list that asks over the wall bus",
			orderplan.CatalogNames(b.handoutPolicy), orderplan.CatalogMax(b.handoutPolicy),
			b.handoutPolicy.MaxOrdersPerDonor, b.handoutPolicy.MaxUnitsPerItem, windowMin)
		// Said out loud on its own line, because "anything in the bank" is a bigger statement
		// than a catalogue and an operator must see which mode this mule started in.
		if o := b.handoutPolicy.Open; o.Enabled {
			ctx.logf("hand-outs: and ANYTHING ELSE this bank actually holds, if asked for by name — at most %d per order and %d per %dm, keeping %d back, never %v",
				o.Max, o.WindowMax, windowMin, o.KeepBack, o.Never)
		} else {
			ctx.logf("hand-outs: only the items above (handoutAnything is off) — an order for anything else is refused on sight")
		}
		if g := b.handoutPolicy.Runes; g.Active() {
			ctx.logf("hand-outs: the rune pool — %v are handed out only while this bank holds at least %d of one, and at most %d at a time", g.Runes, g.Floor, g.Max)
		}
	}

	b.cookIdle = BoolSetting(ctx.Settings, "cookIdle", false)
	b.openablesDue = true
	if b.cookIdle {
		ctx.logf("cookIdle is on: will cook the fleet's banked raw fish at the Catherby range while idle between trades")
	}
	b.cookBuffer = max(0, IntSetting(ctx.Settings, "cookBufferFood", 0))
	if b.cookBuffer > 0 && b.cookIdle {
		ctx.logf("cookBufferFood is %d: will keep cooking in front of queued kits (never trade requests or donors) until %d cooked food is banked", b.cookBuffer, b.cookBuffer)
	}
	b.joinKeys = BoolSetting(ctx.Settings, "joinKeyHalves", true)
	b.keysDue = b.joinKeys
	if b.joinKeys {
		ctx.logf("joinKeyHalves is on: will join the fleet's banked key halves into crystal keys while idle, after each openables pass")
	}
	b.status = mulestatus.NewTracker(ctx.Username, nil)
	b.status.SetCatalog(orderplan.CatalogNames(b.handoutPolicy))
	// After the hand-out policy exists, because the restock floor is derived from this mule's
	// OWN per-window coin ceiling (see setUpRestock).
	b.setUpRestock(ctx)
	ctx.Publish(b.status)
	ctx.SetPhase(phaselog.Idle)
	return nil
}

// announce publishes a "mule-here" heartbeat on ctx.Bus, rate-limited to
// mulebus.HeartbeatInterval — mirrors MuleReceiver.ts's own announce(), called on every
// loop() tick but internally rate-limited the same way. Free pack slots come from
// ctx.Components (nil-safe: reports -1, "unknown", rather than guessing, if the
// interface archive wasn't configured for this account).
func (b *muleBot) announce(ctx *Context) {
	if time.Since(b.lastAnnounce) < mulebus.HeartbeatInterval {
		return
	}
	x, z, level, ok := ctx.Session.State().Tile()
	if !ok {
		return
	}
	free := -1
	if ctx.Components != nil {
		// mulerunner.PlayerPack (3214), not actions.NewEquipment(...).Inventory() — see
		// that constant's own doc comment (cook.go) for the live-found reading this
		// avoids: the dynamic tab-resolution path came back with zero pack entries after
		// a bank/door/walk sequence, which would have announced 28 free slots on the bus
		// instead of whatever the pack actually holds. actions.Equipment now falls back
		// to this same component when the tab is unresolvable (see actions.PlayerPack),
		// so the two routes agree; naming the component here keeps the heartbeat's
		// reading independent of that fallback.
		inv := ctx.Session.State().Inventory(mulerunner.PlayerPack)
		held := 0
		if inv != nil {
			held = len(inv.NonEmpty())
		}
		free = mulereceiverplan.PackSlots - held
	}
	b.lastAnnounce = time.Now()
	// The heartbeat finally carries a real state rather than a hardcoded "home" — the bus had no
	// vocabulary for "on my way to you" before this. busState answers StateHome for a posted
	// mule, so a fixed-post fleet's heartbeats are unchanged.
	// Stamped with the mule's WORLD as well as its tile (2026-09-22, w2.example.invalid opened).
	// This is how every donor script answers "which world is the mule on" without the roster
	// hardcoding it -- see Context.MuleWorld. It works precisely because the bus is
	// in-process and world-agnostic: a donor on world 2 hears this heartbeat from a mule on
	// world 1, when the two have no in-game way to hear each other at all.
	// And the queue: how many donors this mule already expects and what one costs it, so a
	// donor can see a long wait BEFORE it draws a load and walks it over (donorplan.DeferDraw).
	// And its POST as the Home tile (Scarab lane P, 2026-09-23 -- a DECLARED NARROW EDIT in lane
	// H's file). The live tile is where the mule stands this second, which during an idle cook
	// round is the Catherby range, not the booth it serves from; a donor that walked to the live
	// tile waited in the range house while the mule came back to its post and gave up ("you never
	// opened the trade"). Nothing but ScarabSwarm reads Home, and the state is left as it was.
	post := b.postTile()
	ctx.Bus.Publish(mulebus.OnWorld(mulebus.WithQueue(
		mulebus.Heartbeat(ctx.Username, tile.Tile{X: x, Z: z, Level: level}, free, b.busState(ctx), &post),
		b.queueDepth(), b.cycle()),
		ctx.World()))
}

// pollBus drains every wall-bus message published since the last call and acts on the
// kinds this bot understands — see the package doc's "what this does NOT yet do" for the
// honest limit (heard and queued, never travelled to).
func (b *muleBot) pollBus(ctx *Context) {
	now := time.Now()
	// Pass 1 applies the whole batch; pass 2 answers the donors that announced in it. Quoting
	// each announcement where it sat in the batch counted donors whose cancel came later in the
	// SAME batch: live 2026-09-23 00:45:01 a quote counted 36 donors against 30 still waiting.
	var announced []string
	for _, msg := range b.busWatcher.Poll(ctx.Bus) {
		if !mulebus.AddressedTo(msg, ctx.Username) {
			continue // meant for a different mule on the same wall
		}
		key := muleKey(msg.From)
		switch msg.Kind {
		case mulebus.KindCollect:
			if !busAllowed(b.allowed, msg.From) {
				ctx.logf("wall bus: ignoring a pickup request from %q — not on the allow list", msg.From)
				continue
			}
			if _, seen := b.pickups.LastHeard(msg.From); !seen {
				ctx.logf("wall bus: %q asks for a pickup at %d,%d,%d (%d slots)", msg.From, msg.Tile.X, msg.Tile.Z, msg.Tile.Level, msg.Slots)
			}
			b.pickups.Offer(now, msg.From, msg.Tile, msg.Slots)
			// A donor repeating its collect is a donor still waiting: keep its place alive.
			if _, queued := b.incoming[key]; queued {
				b.markSeen(key, now)
			}
		case mulebus.KindReserve:
			if busAllowed(b.allowed, msg.From) {
				if b.enroute == nil {
					b.enroute = map[string]time.Time{}
				}
				b.enroute[key] = now
			}
		case mulebus.KindCancel:
			if msg.KitID != "" {
				b.cancelKit(ctx, key, msg.KitID)
				continue
			}
			_, wasPickup := b.pickups.LastHeard(msg.From)
			b.pickups.Drop(msg.From)
			delete(b.enroute, key)
			// The trade requests it left in the chat log go with it, so the mule never
			// reciprocates to a donor that has already walked away.
			if b.watcher != nil {
				b.watcher.Forget(msg.From)
			}
			// A cancel withdraws a queued hand-out order too (NB3): the donor gave up waiting
			// for it, so serving it later -- orders go before donors -- helps nobody.
			if _, queued := b.orders[key]; queued {
				delete(b.orders, key)
				delete(b.ordersAt, key)
				delete(b.orderAt, key)
				ctx.logf("wall bus: %q withdrew its order", msg.From)
			}
			if _, was := b.incoming[key]; was {
				delete(b.incoming, key)
				delete(b.incomingSeen, key)
			} else if wasPickup {
				ctx.logf("wall bus: %q cancelled its pickup request", msg.From)
			}
		case mulebus.KindOrder:
			if !busAllowed(b.allowed, msg.From) {
				ctx.logf("wall bus: ignoring an order from %q — not on the allow list", msg.From)
				continue
			}
			b.takeOrder(ctx, msg)
		case mulebus.KindKitOrder:
			if !busAllowed(b.allowed, msg.From) {
				ctx.logf("wall bus: ignoring a kit order from %q — not on the allow list", msg.From)
				continue
			}
			b.takeKit(ctx, msg)
		case mulebus.KindReady:
			if !busAllowed(b.allowed, msg.From) {
				// Said out loud, once per name, rather than dropped in silence. KindCollect and
				// KindOrder both log their allow-list refusals; this one did not, so a donor
				// missing from allowedPartners (or spelled differently in the two places) looked
				// from this side exactly like a stand nobody was walking to — and from the
				// donor's side like a mule that never answered, until its own 10-minute
				// give-up. With 44 fishers naming one mule that is a whole account's afternoon
				// lost to a typo nothing ever printed.
				if b.warnedReady == nil {
					b.warnedReady = map[string]bool{}
				}
				if !b.warnedReady[key] {
					b.warnedReady[key] = true
					ctx.logf("wall bus: ignoring a hand-off announcement from %q — not on the allow list, so its trade will be refused too", msg.From)
				}
				continue
			}
			if b.incoming == nil {
				b.incoming = map[string]time.Time{}
			}
			// The announcement time is the donor's place in the queue, so a repeat keeps it.
			if _, queued := b.incoming[key]; !queued {
				b.incoming[key] = now
				// A new hand-off: its donor may be asked first again (see RequestWatcher.Forget).
				if b.watcher != nil {
					b.watcher.Announced(msg.From)
				}
			}
			b.markSeen(key, now)
			delete(b.enroute, key)
			ctx.logf("wall bus: %q is on the way to hand off", msg.From)
			announced = append(announced, msg.From)
		}
	}
	// ANSWER EVERY ANNOUNCEMENT, so the donor queues instead of giving up. A donor that has heard
	// a KindComing waits on the mule's promise (donorplan.MeetTimeoutComing) instead of
	// MeetTimeout's one minute; before 2026-09-22 nothing sent one from a STATIONARY mule, and
	// every donor fell back to the one-minute timeout. The quote is the donor's place in the
	// queue the mule actually serves (earliest announced first, see mulerunner's pick), times
	// what one hand-off costs this mule, and it is refreshed after every trade (refreshQuotes),
	// so it stays a turn time rather than a guess made once on arrival.
	if len(announced) > 0 {
		b.incomingPending()
		if ctx.Bus != nil {
			for _, from := range announced {
				if _, still := b.incoming[muleKey(from)]; still {
					ctx.Bus.Publish(mulebus.ComingMessage(ctx.Username, from, mulebus.TravelWalk, b.quote(muleKey(from))))
				}
			}
		}
	}
	b.syncIncoming()
}

// busTick publishes the heartbeat and reads the bus, at most once a second. See deps' Delay.
func (b *muleBot) busTick(ctx *Context) {
	if !b.wallBus || b.busWatcher == nil || ctx.Bus == nil || time.Since(b.lastBusTick) < time.Second {
		return
	}
	b.lastBusTick = time.Now()
	b.announce(ctx)
	b.pollBus(ctx)
}

// muleKey is the one name rule for this mule's own queue maps: lower case, trimmed, underscore
// read as space -- the same fold mulerunner applies to trade-request names and busAllowed to
// the allow list. A bus message carries a LOGIN name ("big_net") where a trade request and a
// completed trade carry the DISPLAY name ("Big net"); keyed on strings.ToLower alone, a
// completion could never remove the entry its own announcement made.
func muleKey(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "_", " ")
}

// markSeen records that the announced donor key was heard from at now.
func (b *muleBot) markSeen(key string, now time.Time) {
	if b.incomingSeen == nil {
		b.incomingSeen = map[string]time.Time{}
	}
	b.incomingSeen[key] = now
}

// quote is how long the announced donor key should expect to wait: its place in the queue
// (mulereceiverplan.QueuePosition, the order mulerunner serves in) times cycle().
func (b *muleBot) quote(key string) time.Duration {
	return mulereceiverplan.QueueETA(mulereceiverplan.QueuePosition(b.incoming, key), b.cycle())
}

// refreshQuotes re-sends every announced donor its current quote. Called after every handled
// trade: the queue has just moved, and a donor's promise (donorrunner overwrites it on every
// KindComing) should move with it rather than lapse on an estimate made when it arrived.
func (b *muleBot) refreshQuotes(ctx *Context) {
	if ctx.Bus == nil || !b.wallBus {
		return
	}
	b.incomingPending()
	for key := range b.incoming {
		ctx.Bus.Publish(mulebus.ComingMessage(ctx.Username, key, mulebus.TravelWalk, b.quote(key)))
	}
}

// queueDepth is how many donors this mule expects: announced at its post, plus reserved and
// still walking. It is what the heartbeat publishes for a donor deciding whether to draw a load.
func (b *muleBot) queueDepth() int {
	// Queued kits are work in front of a donor too: see mule_kit.go. So are the per-item hand-out
	// orders, which serveOrder takes before a donor as well (Scarab lane P re-review NB2, a DECLARED
	// NARROW EDIT in lane H's file): live 2026-09-23, 23 hand-outs and 0 fisher trades in five
	// minutes while the published queue read short, so neither the fishers' draw gate nor the
	// Scarab ask gate held back.
	n := b.incomingPending() + len(b.kits) + len(b.orders)
	for key, at := range b.enroute {
		if time.Since(at) > enrouteTTL {
			delete(b.enroute, key)
			continue
		}
		if _, announced := b.incoming[key]; !announced {
			n++
		}
	}
	return n
}

// syncIncoming copies the announced-donor list into the published status.
func (b *muleBot) syncIncoming() {
	if b.status != nil {
		b.status.SetIncoming(b.incoming)
	}
}

// ---- hand-outs: giving a blocked donor the item it asked for ----
//
// The other half of the wall bus, and the gap this file's own doc comment above admits to:
// "it never acts on a queued pickup — no travel, no trade initiated toward a donor." A
// hand-OUT is the narrow, useful slice of that which needs no travel machinery at all, because
// the donors it serves are already standing at the same bank (production account `spare28`, the
// incident this was built for, sat blocked about two tiles from this account for hours).
//
// The decision is not made here: orderplan owns it, and every safety rule — allow list,
// catalogue, per-request cap, per-donor and fleet-wide window caps, stock — lives there where
// it is unit-tested without a session. This file only decides WHEN to look, and publishes the
// answer back.

// takeOrder screens an order off the bus and either queues it or declines it on the spot.
// Declining immediately rather than silently dropping it is the point: the donor is waiting on
// a bounded timer, and an answer of "no" gets it back to its own recovery straight away.
func (b *muleBot) takeOrder(ctx *Context, msg mulebus.Message) {
	if !b.handout {
		ctx.Bus.Publish(mulebus.DeclinedMessage(ctx.Username, msg.From, "hand-outs are switched off"))
		return
	}
	key := strings.ToLower(strings.TrimSpace(msg.From))
	// A repeat of the order being served, or just served, is dropped BEFORE it is screened. The
	// screen can refuse it -- the hand-out's own ledger entry has just used up the donor's window
	// -- and that refusal is published as a KindDeclined naming no item, which the donor's NEXT
	// donorrunner.Await (a different item) would read as its own answer and give up on. Found by
	// review of claude/handoff0923 round 1.
	//
	// The order being served RIGHT NOW: the mule reads its bus from inside every wait (busTick),
	// so the donor's re-publishes arrive while the hand-out is still running -- before b.served
	// below is written -- and would otherwise queue the same ask again.
	if b.serving.text != "" && b.serving.key == key && strings.EqualFold(strings.TrimSpace(b.serving.text), strings.TrimSpace(msg.Text)) {
		return
	}
	if last, ok := b.served[key]; ok && orderplan.StaleRepeat(last.text, last.at, msg.Text, time.Now()) {
		// A re-publish made while that very order was being served, not a new ask.
		ctx.logf("wall bus: ignoring %q's repeat of %q — that order was just served", msg.From, msg.Text)
		return
	}
	// Screened here, fulfilled later: orderplan.Prescreen is everything decidable from the
	// message alone, so a malformed or unauthorised order costs a log line rather than a walk
	// to a bank booth. The full orderplan.Decide (which adds the real stock check) runs inside
	// mulerunner.Handout once the bank is actually open.
	decision := orderplan.Prescreen(time.Now(), msg.From, msg.Text, b.handoutPolicy, b.handoutLedger)
	if decision.Verdict != orderplan.Fulfil {
		ctx.logf("wall bus: declining %q's order %q: %s", msg.From, msg.Text, decision.Reason)
		ctx.Bus.Publish(mulebus.DeclinedMessage(ctx.Username, msg.From, decision.Reason))
		return
	}
	if _, queued := b.orders[key]; !queued {
		ctx.logf("wall bus: %q needs %d %s — queued", msg.From, decision.Qty, decision.Item)
		if b.orderAt == nil {
			b.orderAt = map[string]time.Time{}
		}
		b.orderAt[key] = time.Now()
	}
	// Keyed by donor, so a donor re-publishing while it waits (donorrunner.AskRepeatEvery)
	// refreshes one entry instead of stacking up a queue of the same request.
	b.orders[key] = msg.Text
	if b.ordersAt == nil {
		b.ordersAt = map[string]time.Time{}
	}
	b.ordersAt[key] = time.Now()
}

// muleOrderTTL is how long a queued order survives without its donor re-publishing it: three of
// donorrunner.AskRepeatEvery. A waiting donor re-publishes every ten seconds; one that timed out
// and left stops, and its order would otherwise be served -- ahead of every donor -- to nobody.
const muleOrderTTL = 30 * time.Second

// pruneOrders drops the queued orders nobody has re-published within muleOrderTTL. An order with
// no recorded time (queued by code that predates ordersAt) is kept.
func (b *muleBot) pruneOrders(now time.Time) {
	for key := range b.orders {
		if at, ok := b.ordersAt[key]; ok && now.Sub(at) > muleOrderTTL {
			delete(b.orders, key)
			delete(b.ordersAt, key)
			delete(b.orderAt, key)
		}
	}
}

// servingOrder is the hand-out in progress, empty between hand-outs.
type servingOrder struct{ key, text string }

// servedOrder is one entry of muleBot.served.
type servedOrder struct {
	text string
	at   time.Time
}

// serveOrder fulfils at most one queued order, and only one: a hand-out takes a bank trip and a
// trade, and this account's actual job is receiving the fleet's hauls. Returns true if it spent
// this Loop call on one.
func (b *muleBot) serveOrder(ctx *Context, d *mulerunner.Deps) bool {
	b.pruneOrders(time.Now())
	b.dropDeadKits(ctx)
	// ONE HAND-OUT, OF EITHER KIND, PER FISHING TURN (round 7). Live 2026-09-24 the mule made 23
	// hand-outs and 0 fisher trades in five minutes: Loop serves an order before RunCycle, so a
	// steady stream of single-item orders held every fisher off exactly as a kit backlog did.
	// Every served hand-out now owes the fishing queue a turn (kitTurnOwed, cleared by RunCycle),
	// and while a fisher waits nothing else is served until it has had it. A quiet queue ignores
	// the debt, so hand-outs still run back to back when nobody is waiting.
	if b.kitTurnOwed && !b.fishersQuiet() {
		return false
	}
	if len(b.orders) == 0 {
		return len(b.kits) > 0 && b.serveKit(ctx, d)
	}
	// Oldest first (it was Go's random map order). A due kit that has waited longer than every
	// item order goes ahead of them.
	var from, text string
	var oldest time.Time
	for k, v := range b.orders {
		at := b.orderAt[k]
		if from == "" || at.Before(oldest) {
			from, text, oldest = k, v, at
		}
	}
	if _, kit, due := b.kitDue(); due && len(b.kits) > 0 && kit.at.Before(oldest) {
		return b.serveKit(ctx, d)
	}
	ctx.SetPhase(phaselog.Handout)
	delete(b.orders, from)
	delete(b.ordersAt, from)
	delete(b.orderAt, from)
	b.kitTurnOwed = true

	b.serving = servingOrder{key: from, text: text}
	res := mulerunner.Handout(d, mulerunner.HandoutRequest{
		To: from, Text: text, Policy: b.handoutPolicy, Ledger: b.handoutLedger,
	})
	b.serving = servingOrder{}
	// Whatever the outcome: every re-publish the donor made while this ran is the same ask, and
	// it has had its answer (the items, or a decline on the bus).
	if b.served == nil {
		b.served = map[string]servedOrder{}
	}
	for k, v := range b.served {
		if time.Since(v.at) > orderplan.RepeatWindow {
			delete(b.served, k)
		}
	}
	b.served[from] = servedOrder{text: text, at: time.Now()}
	switch {
	case res.Stopped:
		return true
	case res.Delivered:
		// Recorded only on a confirmed delivery — a ledger of attempts would let a run of
		// mechanical failures lock a donor out of something it never actually received.
		b.handoutLedger.Record(time.Now(), from, res.Item, res.Crossed)
		b.noteHandoutTrade()
		ctx.logf("handed %d %s to %q", res.Crossed, res.Item, from)
		ctx.Bus.Publish(mulebus.TradedMessage(ctx.Username, from))
		recordHandout(ctx, from, res)
	default:
		ctx.logf("could not hand %q what it asked for: %s", from, res.Reason)
		ctx.Bus.Publish(mulebus.DeclinedMessage(ctx.Username, from, res.Reason))
		recordHandout(ctx, from, res)
	}
	// The donor reciprocated our own trade request, which leaves an ordinary
	// "<donor>:tradereq:" line in this account's chat log for a trade that is already over.
	// Left there, the next RunCycle would reciprocate it and sit on an empty offer screen
	// until its own settle timeout — see DropRequestsFrom's own doc comment.
	if dropped := b.watcher.DropRequestsFrom(d, from); dropped > 0 {
		ctx.logf("wall bus: ignored %d leftover trade request line(s) from the hand-out with %q", dropped, from)
	}
	return true
}

// busAllowed checks a bus message's From name against the exact same allow-list map
// mulerunner.Reciprocate/completeTrade already enforce on the trade side (built by
// mulerunner.AllowList) — a pickup ask spends the mule's time exactly like a trade does, so
// it gets the same gate, and since it's the SAME map, it needs the SAME key normalization
// mulerunner.normalisePlayerName now applies (underscore-to-space, added tonight to fix
// real in-game trade requests being rejected — see that function's own doc comment): the
// map's own keys are stored in that normalized form regardless of which login-username
// spelling an operator typed, so a bus message's From field (this project's own Go code
// publishing an account's login username, e.g. "big_net") must be normalized the identical
// way to ever find its own entry. Duplicated rather than calling mulerunner's exported
// helper directly because it's a two-line string transform, not worth a cross-package
// dependency for — but MUST stay byte-for-byte identical to it, not just "similar."
func busAllowed(allowed map[string]bool, name string) bool {
	return allowed[strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "_", " ")]
}

func (b *muleBot) Loop(ctx *Context) error {
	if !waitForGuardian(ctx) {
		return nil // stop requested while waiting
	}
	if !b.ensureHome(ctx) {
		ctx.Delay(time.Second)
		return nil
	}

	d := b.deps(ctx)
	if line, ok := b.load.Summary(time.Now()); ok && time.Since(b.loadSaid) >= 10*time.Minute {
		b.loadSaid = time.Now()
		ctx.logf("%s", line)
	}

	// Account owner's own ask (2026-09-16): go into every trade with an empty pack, not
	// just once at startup. Cheap when there is nothing to do — DepositAllInPack reads the
	// pack first and returns immediately without walking anywhere if it's already empty, so
	// this costs nothing on the common case (every Loop call between trades) and only
	// actually walks to the bank on the rare call where leftover items are found.
	if b.bank {
		if packHeld(ctx) > 0 {
			ctx.SetPhase(phaselog.Banking)
		}
		if deposited, ok := mulerunner.DepositAllInPack(d); ok && deposited > 0 {
			ctx.logf("banked %d leftover stack(s) from the pack before taking on a trade", deposited)
		}
		// Publish the bank if it never has been since this mule started, or not lately.
		b.refreshPublishedBank(ctx, d)
	}

	if b.wallBus {
		if ctx.Bus == nil {
			if !b.warnedNoBus {
				ctx.logf("wall bus: wallBus is on but ctx.Bus is nil — skipping (no shared Bus was configured for this fleet)")
				b.warnedNoBus = true
			}
		} else {
			b.announce(ctx)
			b.pollBus(ctx)
			// One step of the coin-restock conversation: refresh the coin count if it is
			// stale and the mule is idle, appeal if it is below its floor, send for at most
			// a few of the accounts that answered. No-op unless "restock" is on. It never
			// trades or walks — the donors it sends for arrive as ordinary incoming trade
			// requests that RunCycle below was already serving.
			b.stepRestock(ctx, d)
			// Before RunCycle, not after. RunCycle blocks for a whole idleTimeout (60s by
			// default) when nothing arrives, and the accounts that place orders are by
			// definition blocked and doing nothing at all while they wait — making them
			// queue behind a full idle window would give back most of the latency this
			// feature exists to remove. An ordinary hand-OVER is never delayed by this:
			// serveOrder does nothing at all when no order is queued, and RunCycle is
			// entered immediately afterwards either way.
			if b.cookBufferStep(ctx, d) { // off unless cookBufferFood > 0 (mule_cookbuffer.go)
				return nil
			}
			if b.handout && b.serveOrder(ctx, d) {
				b.watcher.Prune(d)
				// A hand-out moves the queue's clock just as a trade does: every donor waiting
				// through it heard its promise last BEFORE it, and a refreshed promise shrinks to
				// place x cycle, so a run of hand-outs with no hand-off between them let the
				// donors about to be served lapse (review of claude/handoff0923 round 1; live
				// 00:41:12-00:44:57 was 3m45s of hand-outs and nothing else).
				b.refreshQuotes(ctx)
				return nil
			}
			// Which camp to serve next, and setting off for it. Handing control straight back
			// matters: RunCycle below waits a whole idleTimeout, so a trip decided now would
			// otherwise not actually start for another minute.
			if b.travel && b.manageTrip(ctx) {
				b.watcher.Prune(d)
				return nil
			}
		}
	}

	// With idle cooking on and no donor announced, only glance for a trade request before
	// cooking. Owner 2026-09-19: the full idleTimeout (60s) stood the mule at the bank between
	// every cook round. Donors announce when nearly there (pollBus above) and a trade request
	// interrupts a round (IdlePending), so nobody is left waiting.
	wait := b.cycleWait(b.watcher.QueuedRequests())
	ctx.SetPhase(phaselog.Idle)
	b.waitStarted = time.Now()
	res := mulerunner.RunCycle(d, b.watcher, b.allowed, wait)
	if res.Early {
		// Not a fishing turn (nobody was served) and not a timeout: back to the top of Loop, where
		// serveOrder sees the same gate LeaveWait just passed.
		g := b.orderGate()
		ctx.logf("left the idle wait early after %s: %d order(s) and %d kit(s) servable (fishers quiet=%v, oldest kit %s, wait was %s)",
			b.earlyWaited.Round(100*time.Millisecond), g.SingleOrders, g.Kits, g.FishersQuiet, g.OldestKitAge.Round(time.Second), wait)
		return nil
	}
	b.kitTurnOwed = false // the fishing queue has had its turn
	// The ONLY place a restock donation is counted, and it counts this mule's own pack gain
	// (CycleResult.Received, set only once completeTrade confirmed both screens). A no-op
	// unless the partner is a donor this mule actually sent for.
	b.noteRestockReceipt(ctx, res)

	if !res.Handled {
		b.status.Idle()
		ctx.SetPhase(phaselog.Idle)
		b.load.NoteIdle(time.Now(), time.Since(b.waitStarted))
		if wait == b.idleTimeout {
			ctx.logf("no trade request in the last %s", b.idleTimeout)
		}
		// Caskets and oysters first: donors open their own before a hand-off, but any that
		// reach the bank unopened are opened here, in the same quiet the cooking waits for.
		if b.openablesDue && b.trip == nil && !time.Now().Before(b.openablesRetryAt) && b.watcher.QueuedRequests() == 0 && b.incomingPending() == 0 && b.ordersPending() == 0 {
			b.openBankedOpenables(ctx)
			return nil
		}
		// Then the key halves those caskets produced, before any cooking: a crystal key is
		// only worth making while the account that opens chests is waiting for one.
		if b.joinKeys && b.keysDue && b.trip == nil && !time.Now().Before(b.keysRetryAt) && b.watcher.QueuedRequests() == 0 && b.incomingPending() == 0 && b.ordersPending() == 0 {
			b.joinBankedKeyHalves(ctx)
			return nil
		}
		if b.cookIdle {
			// One bounded cook-and-bank round, never more — see mulerunner.CookRound's
			// own doc comment and cookIdle's own setting doc comment above.
			//
			// Three gates, all added 2026-09-18 on measured evidence (COLLAB.md's mule
			// throughput analysis): RunCycle returning Handled=false only means "nothing
			// arrived in the last idleTimeout", which is NOT the same as "the mule is
			// free". A cooking round walks it off its stand to the Catherby range for ~99
			// seconds, and donors that arrive meanwhile give up after their own
			// donorplan.MeetTimeout without ever being served. Measured: 22% of a
			// 22-minute window spent away, 28% of donor arrivals landing during it, while
			// the mule was otherwise idle ~57% of the time — i.e. throughput was lost to
			// absence, not to a lack of capacity. The backlog is a symptom of trades
			// having been failing, so draining it comes first.
			// Only a donor that has actually ASKED to trade counts as waiting. An earlier
			// version of this gate also refused to cook whenever any allow-listed player
			// stood within 16 tiles, which was wrong twice over, live-found 2026-09-18:
			// this mule's stand IS the Catherby bank, where every one of the ~255 fishers
			// banks its catch, so allow-listed accounts are in range almost continuously
			// whether or not they want a hand-off — and a single stuck account parked on
			// the tile (production account "spare28") pinned the condition true permanently.
			// Between them that suppressed idle cooking entirely rather than merely
			// deferring it. RunCycle has already reported no request in the last
			// idleTimeout by the time this runs, donors re-request every
			// donorplan.RequestEvery while they genuinely wait, and cookAfterIdle keeps a
			// wide margin over donorplan.MeetTimeout — so a real queue cannot hide here.
			queued := b.watcher.QueuedRequests()
			switch {
			case b.trip != nil:
				ctx.logf("not cooking: on the way to a pickup camp")
			case b.travel && b.pickups != nil && b.pickups.Size(time.Now()) > 0:
				ctx.logf("not cooking: %d pickup request(s) waiting to be collected", b.pickups.Size(time.Now()))
			case queued > 0:
				ctx.logf("not cooking: %d queued trade request(s) still to serve", queued)
			case b.ordersPending() > 0:
				// The companion to IdlePending's own b.orders check above: that one aborts
				// a round an order interrupts, this one stops a round starting on top of an
				// order already waiting. serveOrder runs before RunCycle, so a queued order
				// normally goes out first — but only once per Loop call, so a second order
				// can still be waiting here, and starting a ~99s round in front of it
				// spends the donor's whole donorrunner.Await on cooking.
				ctx.logf("not cooking: %d queued hand-out order(s) still to serve", b.ordersPending())
			case !b.fishersQuiet():
				ctx.logf("not cooking: a donor has just announced")
			case time.Since(b.lastTradeAt) < b.cookAfterIdle:
				ctx.logf("not cooking: only %s since the last trade (need %s of a quiet stand)",
					time.Since(b.lastTradeAt).Round(time.Second), b.cookAfterIdle)
			default:
				ctx.SetPhase(phaselog.Cooking)
				if _, ok := mulerunner.CookRound(d); ok {
					b.load.NoteCookRound(time.Now())
					ctx.logf("cooking round complete")
				}
				ctx.SetPhase(phaselog.Idle)
			}
		}
		return nil
	}
	recordTrade(ctx, res) // the fleet's trade record (muleledger.go); records only
	if !res.Accepted {
		ctx.logf("trade with %q not completed (declined, or interrupted before it closed)", res.Partner)
		b.status.Failed(res.Partner)
		b.refreshQuotes(ctx)
		ctx.SetPhase(phaselog.Idle)
		return nil
	}

	b.tradesDone++
	if ctx.Config != nil {
		for _, it := range res.Received {
			obj, ok := ctx.Config.Obj(int(it.ID))
			if !ok {
				continue
			}
			if fisherrunner.IsOpenable(obj.Name) {
				b.openablesDue = true
			}
			// A donor that hands over a key half (its own caskets produced one, or a Strange
			// box did) makes the joining round due again, whether or not it is on cooldown:
			// the bank genuinely changed, which is exactly what the cooldown was waiting for.
			if b.joinKeys && obj.Name == mulerunner.KeyHalfName {
				b.keysDue = true
				b.keysRetryAt = time.Time{}
			}
		}
	}
	// Resets the quiet-stand clock the idle cooking round waits on, so a steady stream of
	// hand-offs keeps the mule at its post indefinitely and cooking only ever happens in a
	// genuine lull. See OnStart's own comment where cookAfterIdle is read.
	// Measured BEFORE lastTradeAt moves, and before this partner leaves the queue, so "queued"
	// means "somebody else was waiting through that whole cycle".
	b.noteCycle(len(b.incoming) - 1)
	b.lastTradeAt = time.Now()
	// The partner KEEPS its place in the queue until it cancels, which donorrunner does the
	// moment its hand-off ends. A donor whose load did not fit one trade screen comes straight
	// back for a second round, and it is still at the head of the queue for it -- the owner's
	// 2026-09-14 ask, "waits for any other trades from that same person in case they have more
	// items". Deleting it here sent that second round to the back of the queue, behind everybody
	// the mule was about to ask first (RequestWatcher.pick).
	delete(b.enroute, muleKey(res.Partner))
	b.status.Completed(res.Partner)
	b.refreshQuotes(ctx)
	ctx.logf("trade #%d accepted with %q: %d item stack(s) received", b.tradesDone, res.Partner, len(res.Received))

	if b.bank && len(res.Received) > 0 {
		ctx.SetPhase(phaselog.Banking)
		// BankReceived logs its own "banked N of N" line; a second copy here doubled it.
		mulerunner.BankReceived(d, res.Received)
	}
	b.status.Banked()
	ctx.SetPhase(phaselog.Idle)

	// Explicit, visible version of the account owner's own ask (2026-09-14): "waits for
	// any other trades from that same person in case they have more items." Mechanically
	// this was already true before this line existed — RunCycle loops straight back into
	// watching for the next request (from res.Partner or anyone else still on the allow
	// list) rather than this bot ever travelling off or doing other work between trades,
	// and RequestWatcher's checkpoint only advances inside Poll, so a request sent while
	// this Loop call was busy banking is still caught by the very next RunCycle rather
	// than lost. This line only makes that behavior legible on the dashboard/log instead
	// of silently true — a donor mid multi-round hand-over (haul that didn't fit one trade
	// screen, see donorplan.Handover.Traded) genuinely does send a second request within
	// seconds, and an operator watching the log should see this bot is still waiting for
	// it, not assume it moved on.
	ctx.logf("staying available for a follow-up trade from %q, or anyone else on the allow list", res.Partner)

	if b.maxTrades > 0 && b.tradesDone >= b.maxTrades {
		ctx.requestStop()
	}
	return nil
}

func (b *muleBot) OnStop(ctx *Context) {
	ctx.logf("mule: stopped after %d accepted trade(s)", b.tradesDone)
}

// incomingTTL bounds how long an announced donor holds the mule at its post: a Fishing Guild
// donor walks about three minutes to Catherby, and one that never arrives must not stop cooking
// for good.
const incomingTTL = 6 * time.Minute

// enrouteTTL bounds how long a reservation (a donor that drew a load and set off) counts in the
// published queue depth without the donor announcing, cancelling or trading. A Seers-to-Catherby
// walk is about a minute; a boat detour is four.
const enrouteTTL = 6 * time.Minute

// incomingPending is how many announced donors are still expected, dropping expired ones.
func (b *muleBot) incomingPending() int {
	expired := false
	for name, at := range b.incoming {
		if seen, ok := b.incomingSeen[name]; ok && seen.After(at) {
			at = seen
		}
		if time.Since(at) > incomingTTL {
			delete(b.incoming, name)
			delete(b.incomingSeen, name)
			expired = true
		}
	}
	if expired {
		b.syncIncoming()
	}
	return len(b.incoming)
}

// cycleWait is how long one Loop call waits for a trade request: the full idleTimeout, or just
// cookGlance when idle cooking is on and no donor is announced or queued.
func (b *muleBot) cycleWait(queued int) time.Duration {
	// At a camp the drain and dwell checks live in manageTrip, one Loop call apart, so a full
	// idle wait here would spend most of a batch's dwell blocked on one trade that may never
	// come. Unreachable with travelToRequests off, where trip is always nil.
	if b.trip != nil && b.idleTimeout > campGlance {
		return campGlance
	}
	if b.cookIdle && b.fishersQuiet() && queued == 0 && b.idleTimeout > cookGlance {
		return cookGlance
	}
	return b.idleTimeout
}

// cookGlance is how long an idle-cooking mule with no donor on the way checks for a trade
// request before starting its next cook round.
const cookGlance = 3 * time.Second

// openBankedOpenables opens every casket and oyster in the mule's bank at its Catherby post,
// a pack at a time, with the fishers' burst opener (fisherrunner.OpenBankedOpenables), and
// banks what they held. A bank it cannot reach or reopen, a pass that ends with openables
// still banked, or a pass that yields to a trade leaves the job due for the next quiet cycle.
//
// Live 2026-09-20 (build 3299c3c): "opened 560" is exactly fisherrunner's maxOpenRounds (20)
// times a 28-slot pack, and the log shows the booth reopened exactly 20 times. That pass hit its
// round cap with caskets and oysters still banked, reported ok, cleared this job, and the mule
// cooked from then on with them sitting in the bank. The job now stays due until a bank read
// shows none left, and CookRound's DeferCook refuses to cook while any are there.
func (b *muleBot) openBankedOpenables(ctx *Context) {
	b.openablesDue = false
	md := b.deps(ctx)
	fd := muleFisherDeps(md, b.walker)
	fd.Loot = ctx.Loot // the loot panel counts what the mule's own opening yields
	// The mule opens its own bank at its own post, wherever postTile put it. OpenCatherbyBank
	// reads nothing from the layout but Bank (the tile it walks to before looking for the
	// nearest booth), so swapping that one field is the whole of "bank where I stand"; a
	// Catherby-posted mule gets the identical layout it always had.
	layout := fisherplan.Catherby()
	layout.Bank = b.postTile()
	bank, ok := fisherrunner.OpenCatherbyBank(fd, layout)
	if !ok {
		ctx.logf("openables: could not open the bank — trying again next quiet cycle")
		b.openablesDue = true
		return
	}
	// Each round of a pass takes about nine seconds, so a pass of up to 20 rounds would hold a
	// donor that asked to trade for three minutes, well past its own meet timeout. Between
	// rounds, with the bank reopened and the pack deposited, a trade request, an open trade or
	// a donor on the way ends the pass; the trade is served and the pass resumes next quiet
	// cycle.
	yielded := false
	reopen := func() (*actions.Bank, bool) {
		bank, ok := fisherrunner.OpenCatherbyBank(fd, layout)
		if ok && md.IdlePending() {
			fisherrunner.DepositExcept(fd, bank, nil)
			_ = bank.Close()
			yielded = true
			return nil, false
		}
		return bank, ok
	}
	bank, opened, ok := fisherrunner.OpenBankedOpenables(fd, bank, reopen, nil)
	left := -1
	if bank != nil {
		left = bankedOpenableCount(ctx.Config, bank.Items())
		_ = bank.Close()
	}
	switch {
	case !yielded && ok && opened == 0 && left > 0:
		b.openablesRetryAt = time.Now().Add(openablesRetryAfter)
		ctx.logf("openables: %d casket(s)/oyster(s) in the bank but none would open — cooking carries on, trying them again in %s", left, openablesRetryAfter)
	case yielded:
		ctx.logf("openables: opened %d banked casket(s)/oyster(s), then stopped for an incoming trade — the rest next quiet cycle", opened)
	case left > 0:
		ctx.logf("openables: opened %d banked casket(s)/oyster(s); %d still in the bank — continuing next quiet cycle, before any cooking", opened, left)
	case opened > 0:
		ctx.logf("openables: opened %d banked casket(s)/oyster(s) before cooking", opened)
	}
	if !ok || left != 0 {
		b.openablesDue = true
	}
}

// openablesRetryAfter is how long openBankedOpenables waits after a pass that opened nothing.
const openablesRetryAfter = 10 * time.Minute

// keysRetryAfter is how long joinBankedKeyHalves waits after a round that joined nothing.
// Shorter than openablesRetryAfter: a failed round costs only a bank open, and the usual
// reason for one (the halves arrived a moment ago and the bank read was stale) clears fast.
const keysRetryAfter = 5 * time.Minute

// joinBankedKeyHalves runs one mulerunner.JoinKeyHalvesRound at the mule's post and decides
// whether the job stays due. Like the openables pass it clears its own flag first and sets it
// again on anything short of "no joinable pair left in the bank", so a round that is
// interrupted or falls short is retried rather than silently dropped.
func (b *muleBot) joinBankedKeyHalves(ctx *Context) {
	b.keysDue = false
	ctx.SetPhase(phaselog.Banking)
	defer ctx.SetPhase(phaselog.Idle)
	res, ok := mulerunner.JoinKeyHalvesRound(b.deps(ctx))
	switch {
	case !ok:
		// Not a transient miss: no bank, no components, or content without two halves. Do
		// not hammer it.
		b.keysDue = true
		b.keysRetryAt = time.Now().Add(keysRetryAfter)
		ctx.logf("keys: could not run a joining round (%s) — trying again in %s", res.Reason, keysRetryAfter)
	case res.Stopped:
		b.keysDue = true
		ctx.logf("keys: joined %d crystal key(s) before being asked to stop", res.Joined)
	case res.Joined > 0:
		b.keysDue = true // there may be more pairs than one pack held
		b.keysRetryAt = time.Time{}
		ctx.logf("keys: joined %d crystal key(s) from %d pair(s) of halves and banked them", res.Joined, res.Drawn)
	case res.BankedHalves == 0:
		// Nothing to do, and nothing changed. Stays not-due until a trade brings a half in.
		ctx.logf("keys: no joinable pair of key halves in the bank")
	default:
		b.keysDue = true
		b.keysRetryAt = time.Now().Add(keysRetryAfter)
		ctx.logf("keys: %d pair(s) of halves in the bank but none would join (%s) — trying again in %s",
			res.BankedHalves, res.Reason, keysRetryAfter)
	}
}

// bankedOpenableCount is how many caskets and oysters items (a bank read) holds.
func bankedOpenableCount(cfg *cachecfg.Config, items []gamestate.SlotItem) int {
	if cfg == nil {
		return 0
	}
	return openableCount(items, func(id int) (string, bool) {
		obj, ok := cfg.Obj(id)
		if !ok {
			return "", false
		}
		return obj.Name, obj.Noted()
	})
}

// openableCount is bankedOpenableCount's pure core: nameOf reports an item's name and whether
// it is a note (a note cannot be opened, and a bank never holds one anyway).
func openableCount(items []gamestate.SlotItem, nameOf func(id int) (string, bool)) int {
	n := 0
	for _, it := range items {
		name, noted := nameOf(int(it.ID))
		if noted || !fisherrunner.IsOpenable(name) {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		n += c
	}
	return n
}

// packHeld is how many of the mule's own pack slots hold something (mulerunner.PlayerPack, the
// same read announce uses).
func packHeld(ctx *Context) int {
	inv := ctx.Session.State().Inventory(mulerunner.PlayerPack)
	if inv == nil {
		return 0
	}
	return len(inv.NonEmpty())
}

// muleCycle is what one hand-off costs this mule end to end BEFORE it has measured itself, used
// to give a queuing donor an eta on a KindComing. CLAUDE.md's own mule-throughput row measures
// the cycle median at 30s: 20s the donor spends drawing and settling its offer (the mule only
// waits, one partner at a time, so it cannot be overlapped) plus 10s the mule spends banking.
const muleCycle = 30 * time.Second

// muleCycleFloor is the lowest MEASURED cycle this mule will quote. It was muleCycle itself, 30s,
// which the mule stopped being on 3dfb543a: with the donor's offers sent in one burst the median
// gap between trades measured 15s live, so every quote and every queue-depth estimate ran twice
// too long.
const muleCycleFloor = 10 * time.Second

// muleCycleCap bounds the measured cycle so one pathological trade cannot make every waiting
// donor's estimate absurd. A donor's own MeetTimeoutComing caps the wait regardless.
const muleCycleCap = 2 * time.Minute

// cycle is what this mule currently believes one hand-off costs it: its own measurement once it
// has one, the 30s book figure until then, and never below the book figure.
//
// The constant alone was measured to be optimistic under load. Live 2026-09-23 the fleet ran
// 1.08 trades/min against 6.2 announcements/min, so a real cycle near 55s was being quoted as
// 30s, and every donor was told a time the mule then missed. An eta is only worth publishing if
// the mule keeps it -- the donor abandons its load on that promise, and an abandoned load is
// what deadlocks an account.
func (b *muleBot) cycle() time.Duration {
	if b.cycleSeen <= 0 {
		return muleCycle
	}
	if b.cycleSeen < muleCycleFloor {
		return muleCycleFloor
	}
	if b.cycleSeen > muleCycleCap {
		return muleCycleCap
	}
	return b.cycleSeen
}

// noteCycle folds the gap since the previous completed trade into cycleSeen.
//
// Only counted when somebody was ALREADY QUEUED behind that trade: the gap between two trades
// with an empty queue is mostly the mule standing idle waiting for a donor to turn up, which is
// not what a queuing donor is asking about. Bounded at both ends so a restart, a clock jump or a
// stall cannot poison the average.
func (b *muleBot) noteCycle(queued int) {
	if queued <= 0 || b.lastTradeAt.IsZero() {
		return
	}
	gap := time.Since(b.lastTradeAt)
	if gap < 5*time.Second || gap > 5*time.Minute {
		return
	}
	if b.cycleSeen <= 0 {
		b.cycleSeen = gap
		return
	}
	b.cycleSeen = (3*b.cycleSeen + gap) / 4
}

// noteHandoutTrade folds a delivered hand-out (a kit or a per-item order) into the measured cycle,
// as completeTrade does for a donor trade, and moves lastTradeAt.
//
// LIVE 26 Sep, 16:30-17:24 on 26b2816f (mulequeue, a DECLARED NARROW EDIT in lane H's file): only
// donor trades were measured -- 13 of the hour's 125 -- so each gap spanned every kit served and
// cook round in between, and the mule quoted 1m35s a trade while its own log served a queued kit
// a median 20 s after the ask (4 s from draw to delivery). The queue it publishes counts kits and
// orders as entries, so the cycle it publishes must be what one ENTRY costs, kits included. Every
// ScarabSwarm ask multiplied that 1m35s by the queue and gave up for the shops.
//
// Queued is what is still waiting at the post after this one: kits, orders and announced donors
// (not the donors still walking over, whose gap would be the walk, not the mule).
func (b *muleBot) noteHandoutTrade() {
	b.noteCycle(len(b.kits) + len(b.orders) + len(b.incoming))
	b.lastTradeAt = time.Now()
}
