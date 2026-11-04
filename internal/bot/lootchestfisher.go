package bot

// LootChestFisher: open the Taverley crystal chest with the keys MuleReceiver makes out of the
// fleet's key halves, and fish in between. The owner's own description (2026-09-20):
//
//	"there should also be a script made for navigating to falador, after getting the keys from
//	mulereceiver and banking them for safe travel. then you can withdraw an amount that allows
//	you to bring all of the loot the crystal chest gives back to the closest falador bank.
//	after keys are all used up, the account should return to the mulereceiver to drop off all
//	loot and get anymore keys they may have aquired in the time it took us to open, if not we
//	should fall back to the progressive fisher until theres enough crystal keys to go make it
//	worth while looting them."
//
// The deployment target is one of the live swarm's EXISTING ProgressiveFisher accounts, chosen
// by pointing that roster entry's `script` at "LootChestFisher". Three consequences shape this
// file:
//
//   - The fallback is a real hand-over to the registered ProgressiveFisher bot, not a copy of
//     fishing. This file builds the same &fisherBot{} the registry's own factory builds, from
//     the same ctx and the same settings map, so an account that falls back behaves exactly as
//     it did before it was switched over — same rungs, same bank trips, same mule hand-offs.
//     It therefore reads ProgressiveFisher's settings too: one roster entry, both schemas.
//   - Nothing the account already has is disturbed. The script never sells, never drops, and
//     never deposits anything it did not draw; the fisher's own banked fish, levels and mule
//     relationship are untouched.
//   - Crystal keys are the one thing the fisher must not hand back to the mule
//     (fisherplan.NeverHandOver) — the mule is where they came from.
//
// The one thing to understand before reading the loop: the bank is shared across the whole
// game, so "bank the keys for safe travel" is not a detour. The account deposits everything at
// the mule's bank, walks to Falador carrying nothing — the route crosses White Wolf Mountain,
// where a death costs whatever is in the pack and nothing else — and draws the keys out again
// at the other end.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/phaselog"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/lootchestfisherplan"
	"rsb0t-go/internal/scripts/lootchestfisherrunner"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/mulerunner"
	"rsb0t-go/internal/scripts/orderplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("LootChestFisher", func() Bot { return &lootChestBot{} })
}

// askRetryAfterDecline is how long to leave the mule alone after it declines a key order. The
// mule's own ledger window is 30 minutes and it allows one order per donor inside it
// (orderplan.DefaultPolicy), so asking again sooner can only be refused again.
const askRetryAfterDecline = 30 * time.Minute

// askRetryAfterSilence is the wait after an ask nothing answered — a mule that is not running,
// or not in range. Much shorter: nothing is being refused, so there is nothing to wait out.
const askRetryAfterSilence = 5 * time.Minute

// fishBetweenAsks is how long the account fishes before checking with the mule again while it
// is short of keys. Deliberately not shorter than the mule's order window: a shorter cycle
// would walk off the fishing spot for an ask that cannot be honoured yet.
const fishBetweenAsks = 30 * time.Minute

type lootChestBot struct {
	mule     string
	minKeys  int
	muleMeet tile.Tile
	walker   *webwalk.Walker
	keys     lootchestfisherrunner.KeyIDs

	// fisher is the registered ProgressiveFisher bot this account falls back to, started
	// lazily the first time the fallback is needed and stopped when it is not. Building it
	// here rather than through New("ProgressiveFisher") is the same thing — the registry's
	// factory is exactly &fisherBot{} — with the type kept, so its own OnStart/Loop/OnStop
	// can be called directly.
	fisher       *fisherBot
	fishingSince time.Time
	nextAsk      time.Time
	// nextFareAsk is the ship fare's OWN retry clock, deliberately not nextAsk. The two orders
	// fail for unrelated reasons — the mule can be out of keys while holding 28M coins — so a
	// declined key order must not also mute the fare, or an account with no fare would sit out
	// askRetryAfterDecline before it could even ask for the thing that unblocks it.
	nextFareAsk     time.Time
	knownUntradable map[int]bool
	// askWait is the wait at the mule for a key order that is not due yet (lootchest_askwait.go).
	askWait askWait

	// bankChecked/bankCheckAt are the one-off "what is actually banked" reading, taken before
	// the account settles into fishing; see countBankedKeys.
	bankChecked bool
	bankCheckAt time.Time
	// bankedKeys is how many crystal keys the bank held the last time this script had one
	// open. A bank cannot be counted without opening it, so the plan is given a remembered
	// reading rather than a guess; every step that opens a bank refreshes it.
	bankedKeys int
	// bankHaulRows is how many rows of this account's OWN bank are waiting for the mule,
	// remembered from the last bank this script had open for exactly the same reason.
	bankHaulRows int

	// muleKeys is the last thing this account heard about the MULE's crystal-key bank, and
	// nextProbe is the blind-probe clock that makes sure "I have never heard" can never be
	// permanent. See lootchestfisherplan.MuleStock and ShouldProbe.
	muleKeys  lootchestfisherplan.MuleStock
	nextProbe time.Time
	// handoffFails counts consecutive hand-offs that produced nothing, so the one account
	// running this script cannot spin at the mule forever. See giveStep's backoff and noteHandoffFailure.
	handoffFails int
	// handoffHoldUntil is when the next hand-off may be tried after one failed; until then the
	// account neither gives to the mule nor drains its bank for it. See noteHandoffFailure.
	handoffHoldUntil time.Time

	// counters, for OnStop's own line.
	opened, trips, asks, gotKeys int
}

// fareOnly is what survives the bank trip BEFORE a long leg: the ship fare and nothing else.
//
// The keys go in here, which is the whole of "banking them for safe travel" — the bank is shared,
// so they are waiting at the far end, and the walk risks only the fare. Keeping them instead
// would also loop: DecidePhase returns PhaseBankForTravel precisely because keys are in the pack,
// so a bank trip that left them there would be asked for again on the very next call, forever.
func fareOnly() map[int]bool {
	return map[int]bool{lootchestfisherplan.Coins: true}
}

// keepIDs is what survives a bank trip AT THE CHEST END, and is never counted as haul: the
// crystal keys the next chest needs, and the ship fare.
//
// The fare matters more than it looks. Every long leg is the ship chain, and the chest end of
// the run banks what it looted at Falador — so a fare treated as loot would be deposited there
// and the account would have no way back to the mule. The surplus above the reserve still goes
// to the mule, through donorrunner's own coin handling, exactly as ProgressiveFisher's does.
func (b *lootChestBot) keepIDs() map[int]bool {
	keep := map[int]bool{lootchestfisherplan.Coins: true}
	for id := range b.keys.Unnoted {
		keep[id] = true
	}
	for id := range b.keys.Noted {
		keep[id] = true
	}
	return keep
}

// handoverKeepIDs is what the hand-over will NOT offer, and it exists so that the decision to
// GO to the mule reads the same rule as the decision of what to PUT ON THE TRADE SCREEN.
//
// keepIDs above is a different question -- what to keep out of the BANK on a chest run -- and
// conflating the two is what kept spare104 at the mule post declining trades every fourteen
// seconds, live on 2026-09-22 and again after the first attempt at this fix. Its log named the
// cause every time: "the load it was sent to deliver is not offerable (1 group(s) already
// skipped as untradeable)". The group was a FISHING TOOL. fisherplan.muleRules keeps the tools
// and NeverHandOver whatever else it is told; keepIDs kept only coins and crystal keys. So a
// Big net in the pack counted as a haul worth a trip and was then never offered, and the count
// could not change however many trades were opened.
//
// The first fix caught only donorplan.Untradeable, which a fishing net is not -- it is
// perfectly tradeable, the fisher simply must not give it away. Hence resolving the SAME NAME
// LISTS muleRules uses, rather than a third hand-written set: a rule that exists twice drifts,
// and this one already had.
//
// Tools deliberately do NOT join keepIDs: at Falador the pack should go to the chest empty
// except for keys, so banking a net there is right. Only the hand-over question changes.
func (b *lootChestBot) handoverKeepIDs(ctx *Context) map[int]bool {
	keep := b.keepIDs()
	// AND WHATEVER THE HAND-OVER HAS LEARNED IT CANNOT OFFER. donorrunner seeds its skip set
	// from b.knownUntradable, so an id in there is never put on the trade screen; counting it as
	// haul here sent the account to the mule for a load it would then refuse to offer. Live
	// 2026-09-26 06:40-06:43: Cosmic, Nature and Law runes "never reached the trade screen",
	// and spare104 summoned the mule and declined an empty trade every 5 s until restarted.
	for id := range b.knownUntradable {
		keep[id] = true
	}
	if ctx == nil || ctx.Config == nil {
		return keep
	}
	for _, name := range append(fisherplan.ToolNames(), fisherplan.NeverHandOver...) {
		for _, id := range ctx.Config.ObjIDs(name) {
			keep[id] = true
		}
	}
	return keep
}

// coinSurplusSlots counts the coin row as haul only when it holds MORE than the fare. Without
// the split an account carrying exactly its reserve would either never take its chest coins to
// the mule (reserve kept, so coins never count) or walk to the mule to offer nothing at all
// (coins always count) - the second is docs/script_porting_framework.md's own bug class #11,
// which had a real production account requesting and declining an empty trade every ten seconds.
func (b *lootChestBot) coinSurplusSlots(d *lootchestfisherrunner.Deps) int {
	if lootchestfisherrunner.CoinsHeld(d) > donorplan.DefaultTravelReserve {
		return 1
	}
	return 0
}

// coinSurplusAwayFromChest is coinSurplusSlots everywhere except the far end.
//
// At Falador the surplus is not haul, because nothing there can take it: the only thing that
// consumes LootInPack at that end is bankLootStep, and that deposits everything except
// keepIDs -- which keeps the coin row, deliberately, so the account can still pay for the ship
// home. Counting it there makes a phase that can never be satisfied. See snapshot.
func (b *lootChestBot) coinSurplusAwayFromChest(d *lootchestfisherrunner.Deps) int {
	if lootchestfisherrunner.AtFalador(d) {
		return 0
	}
	return b.coinSurplusSlots(d)
}

func (b *lootChestBot) deps(ctx *Context) *lootchestfisherrunner.Deps {
	return &lootchestfisherrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		Walker: b.walker, Loot: ctx.Loot,
	}
}

func (b *lootChestBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("LootChestFisher needs a collision pack")
	}
	if ctx.Locs == nil || ctx.Config == nil {
		return fmt.Errorf("LootChestFisher needs the static loc index and the config archive")
	}
	if ctx.Components == nil {
		return fmt.Errorf("LootChestFisher needs the interface archive to use a bank")
	}
	keys, ok := lootchestfisherrunner.ResolveKeyIDs(ctx.Config)
	if !ok {
		return fmt.Errorf("this content pack has no unnoted %q", lootchestfisherrunner.KeyName)
	}
	b.keys = keys
	b.knownUntradable = map[int]bool{}

	b.mule = fisherMuleSetting(ctx.Settings)
	if b.mule == "" {
		return fmt.Errorf("LootChestFisher has no mule to get keys from: set the \"mule\" setting")
	}
	b.minKeys = IntSetting(ctx.Settings, "minKeys", lootchestfisherplan.MinKeysDefault)
	if b.minKeys < 1 {
		b.minKeys = lootchestfisherplan.MinKeysDefault
	}
	// The same setting key ProgressiveFisher reads, so switching an account over needs no
	// second tile in its roster entry.
	b.muleMeet = fisherTileSetting(ctx.Settings, "muleAtTile", mulereceiverplan.SeersStand)

	if w, err := live.NewWalker(live.Deps{Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config, Log: ctx.Log}); err == nil {
		// Ship fares and the Brimhaven customs officer are NPC hops the Walker only plans
		// through with a handler installed, and without one there is NO route between the
		// mule's Catherby post and Falador at all: webwalk's automatic White Wolf den zones
		// forbid the only land route, and the ship chain is the wolf-free alternative. Measured
		// 2026-09-20 (cmd/lootchesttest -mode longleg) before this was wired: "unreachable
		// (best 134 tiles)" after 16s, from a script that would then simply never leave
		// Catherby. Same wiring ProgressiveFisher and MuleReceiver already use, and fisher.go's
		// own comment says the same thing.
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{
				Client: w.Client, Session: ctx.Session, Config: ctx.Config,
				Components: ctx.Components, Log: ctx.Log,
			}))
		}
		b.walker = w
	} else {
		// Not fatal, but say so loudly: without it the long legs and the chest room's own
		// door cannot be routed at all, and the script will do nothing but fish.
		ctx.logf("lootchest: no webwalk walker (%v) — travel to Falador and the chest room door cannot be routed; the account will fish only", err)
	}

	// The mule must be willing to hand keys over, or this never gets off the ground. Checking
	// the catalogue here costs nothing and turns a silent "it just fishes forever" into a
	// startup line an operator can act on.
	if _, listed := orderplan.InCatalog(orderplan.DefaultPolicy(nil), lootchestfisherrunner.KeyName); !listed {
		ctx.logf("lootchest: %q is not in the mule's default hand-out catalogue — the mule will refuse every key order unless its handoutItems setting lists it",
			lootchestfisherrunner.KeyName)
	}

	ctx.logf("lootchest: mule %q, threshold %d key(s), chest at %d,%d (stand %d,%d), banking at Falador west %d,%d; below the threshold the account runs ProgressiveFisher",
		b.mule, b.minKeys,
		lootchestfisherplan.ChestTile.X, lootchestfisherplan.ChestTile.Z,
		lootchestfisherplan.ChestStand.X, lootchestfisherplan.ChestStand.Z,
		lootchestfisherplan.FaladorBankStand.X, lootchestfisherplan.FaladorBankStand.Z)
	return nil
}

// snapshot builds the plan's input from live state. bankedKeys is this bot's own last bank
// read: the bank cannot be counted without opening one, so it is remembered from the last time
// the script had one open rather than guessed.
func (b *lootChestBot) snapshot(ctx *Context, d *lootchestfisherrunner.Deps, bankedKeys int) lootchestfisherplan.Snapshot {
	// Before anything decides to walk to the mule, find out what the mule is holding from what
	// it has already published. See seedMuleKeysFromPeers: without this the account walks 200
	// tiles on a fresh login purely to be told a number the mule broadcast minutes ago.
	b.seedMuleKeysFromPeers(ctx)
	here := lootchestfisherrunner.Here(d)
	return lootchestfisherplan.Snapshot{
		AtMule:    tile.Chebyshev(here, b.muleMeet) <= muleStandRadius,
		AtFalador: lootchestfisherrunner.AtFalador(d),
		// The same radius the bank search itself uses: if OpenNearestBank would find a booth
		// from here, this is "at the bank".
		AtFaladorBank:   tile.Chebyshev(here, lootchestfisherplan.FaladorBankStand) <= bankSearchRadius,
		KeysInPack:      lootchestfisherrunner.KeysInPack(d, b.keys),
		NotedKeysInPack: lootchestfisherrunner.NotedKeysInPack(d, b.keys),
		KeysInBank:      bankedKeys,
		// The coin surplus counts as haul only where it can actually be handed over, which is
		// the MULE end. At Falador it is not haul at all, and counting it there was a live
		// deadlock (2026-09-21): the chest pays 750 coins, that exceeds
		// donorplan.DefaultTravelReserve, so coinSurplusSlots made LootInPack > 0, which
		// selected PhaseBankLoot -- but bankLootStep deposits everything EXCEPT keepIDs, and
		// keepIDs keeps coins. So the deposit moved nothing, LootInPack stayed above zero, and
		// the phase could never advance: "banked 0 slot(s) of chest loot at Falador; 5 key(s)
		// in hand" every few seconds, with five keys unspent, a bank opened and closed each
		// time round.
		//
		// The surplus is not stranded by ignoring it here. It rides home in the pack (the
		// pre-travel bank keeps fareOnly, which is the coin row), and at the mule end this term
		// applies again and PhaseHandoff gives it away -- which is what keepIDs' own comment
		// always said was meant to happen.
		LootInPack: lootchestfisherrunner.LootInPack(d, b.keys, b.handoverKeepIDs(ctx)) + b.coinSurplusAwayFromChest(d),
		PackFree:   lootchestfisherrunner.PackFree(d),
		Held:       lootchestfisherrunner.Held(d),
		MinKeys:    b.minKeys,
		// b.fisher is non-nil for exactly as long as the delegated ProgressiveFisher owns the
		// account (fishStep creates it, stopFishing clears it), which is precisely the window
		// in which position must not be allowed to decide the phase. See DecidePhase's rule 4.
		Fishing: b.fisher != nil,

		// What is known about the MULE's own bank, and whether it is time to go and find out.
		// Both are read here rather than inside the plan so the dispatcher stays pure: the
		// probe clock has to survive across Loop calls, which a pure function cannot do.
		// b.muleKeys is refreshed from the mule's own PUBLISHED bank immediately above this
		// snapshot (see seedMuleKeysFromPeers), because this is the decision that acts on it.
		Mule:  b.muleKeys,
		Now:   time.Now(),
		Probe: lootchestfisherplan.ShouldProbe(b.muleKeys, time.Now(), b.nextProbe),
		// The account's own banked haul, remembered from the last bank this script opened.
		// Zero while a failed hand-off is being backed off; see bankHaulRowsFor.
		BankHaulRows: b.bankHaulRowsFor(time.Now()),
	}
}

// muleKeep is the keep list for anything leaving this account's bank for the mule: the fishing
// tools and fisherplan.NeverHandOver, which is where "Crystal key" sits.
//
// By NAME rather than by id, deliberately: the noted crystal key carries the identical display
// name, so one entry covers both forms where an id rule would have to know two ids. The ship
// fare is not on this list and does not need to be — donorrunner's own coin split keeps
// donorplan.DefaultTravelReserve in the pocket and gives the mule only the surplus, which is
// exactly the owner's "everything in the bank besides 500gp and untradeables".
func muleKeep() []string { return fisherplan.NeverHandOver }

// muleKeepFor is muleKeep plus every item this account has learned cannot cross a trade screen,
// by name, so a banked row of one is not counted as mule haul and drawn straight back out by
// PhaseDrainBank -- the second half of the 2026-09-26 loop (give-up banks the runes, the drain
// draws them, the hand-off fails again).
func (b *lootChestBot) muleKeepFor(ctx *Context) []string {
	return muleKeepWith(b.knownUntradable, func(id int) string {
		if ctx == nil || ctx.Config == nil {
			return ""
		}
		if obj, ok := ctx.Config.Obj(id); ok {
			return obj.Name
		}
		return ""
	})
}

// muleKeepWith is muleKeepFor's pure half: NeverHandOver plus the resolvable names of known.
func muleKeepWith(known map[int]bool, nameOf func(int) string) []string {
	keep := append([]string(nil), muleKeep()...)
	for id := range known {
		if name := strings.TrimSpace(nameOf(id)); name != "" {
			keep = append(keep, name)
		}
	}
	return keep
}

// handoffHeld reports whether a recent failed hand-off is still keeping this account away from
// the mule.
func (b *lootChestBot) handoffHeld(now time.Time) bool { return now.Before(b.handoffHoldUntil) }

// noteHandoffFailure counts a failed hand-off and holds the next one off for
// lootchestfisherplan.HandoffBackoff, returning that wait.
func (b *lootChestBot) noteHandoffFailure(now time.Time) time.Duration {
	b.handoffFails++
	w := lootchestfisherplan.HandoffBackoff(b.handoffFails)
	b.handoffHoldUntil = now.Add(w)
	return w
}

// noteHandoffSuccess clears the failure count and any hold.
func (b *lootChestBot) noteHandoffSuccess() {
	b.handoffFails = 0
	b.handoffHoldUntil = time.Time{}
}

// bankHaulRowsFor is the banked mule haul as the dispatcher should see it: nothing while a
// failed hand-off is being backed off, so PhaseDrainBank does not draw the load straight back
// out and summon the mule again.
func (b *lootChestBot) bankHaulRowsFor(now time.Time) int {
	if b.handoffHeld(now) {
		return 0
	}
	return b.bankHaulRows
}

// muleStandRadius is how close counts as "at the mule".
const muleStandRadius = 8

// Loop runs one step of the cycle. Every branch is one bounded unit of work - a bank trip, a
// walk, one chest - so a stop request lands promptly, exactly as barbarian.go's own Loop does.
func (b *lootChestBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !lootchestfisherrunner.WaitForGuardian(d) {
		return nil
	}
	if ctx.Session.State().IsDead() {
		if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
			ctx.Session.State().ClearDead()
			ctx.logf("lootchest: died and respawned — carrying on (the pack was banked before the walk, so nothing was lost)")
		}
	}
	// A level-up or an event dialogue blocks everything else.
	dlg := actions.NewChatDialog(ctx.Session, ctx.Components)
	if dlg.CanContinue() {
		_ = dlg.Continue()
		ctx.Delay(600 * time.Millisecond)
		return nil
	}

	s := b.snapshot(ctx, d, b.bankedKeys)
	// ASK OVER THE BUS, WHEREVER WE ARE. A due probe no longer drags the account to the mule
	// (see lootchestfisherplan.InterruptsFishing), so the asking has to happen here instead --
	// otherwise removing the walk would also remove the only way this account ever learns the
	// count, and it would fish forever past a mule holding hundreds of keys.
	//
	// Safe to run every Loop: askForKeys is bounded by its own nextProbe clock and by the
	// mule's order cooldown, and a bus order costs one message. It is deliberately BEFORE
	// DecidePhase so an answer that arrives changes this very tick's decision rather than the
	// next one's.
	if s.Probe && s.LootInPack == 0 && s.TotalKeys() == 0 {
		b.askForKeys(ctx, d)
		s = b.snapshot(ctx, d, b.bankedKeys)
	}
	phase := lootchestfisherplan.DecidePhase(s)
	if phase != lootchestfisherplan.PhaseFish {
		b.stopFishing(ctx)
	}

	switch phase {
	case lootchestfisherplan.PhaseFish:
		return b.fishStep(ctx, d)
	case lootchestfisherplan.PhaseHandoff:
		// A PROBE IS A QUESTION, NOT A JOURNEY. DecidePhase returns PhaseHandoff either
		// because there is something to hand over, or merely because this account has never
		// heard how many keys the mule holds (Snapshot.Probe). In the second case with an
		// empty pack there is nothing to trade and nothing to collect -- the entire errand is
		// to learn a number -- and the way that number actually arrives is a WALL BUS order
		// whose decline carries it ("only 69 Crystal key banked, holding until 100"). The bus
		// reaches the mule from anywhere; the 200-tile walk contributes nothing to it.
		//
		// Live 2026-09-23, on the build that had already moved the published-bank read earlier:
		//
		//	00:26:46 [spare104] lootchest: walking to the mule (2809,3441)
		//	00:27:25 [spare104] donor: asked "mule" over the wall bus for need 500 Crystal key
		//
		// It walked first and then asked over the bus, which it could have done standing
		// still. On a cold fleet start the mule has not opened a bank yet, so nothing is
		// published and seedMuleKeysFromPeers cannot help -- which is exactly when the owner
		// keeps seeing this. Ask from where we stand instead, and let the answer decide
		// whether a journey is worth making.
		if s.LootInPack == 0 && s.KeysInPack == 0 && s.NotedKeysInPack == 0 {
			// Not due yet: pause and say so, rather than re-running Loop in a tight loop for
			// the whole retry clock (lootchest_askwait.go, live spare104 2026-09-23).
			if b.holdForKeyOrder(ctx) {
				return nil
			}
			b.askForKeys(ctx, d)
			return nil
		}
		return b.handoffStep(ctx, d, s)
	case lootchestfisherplan.PhaseBankForTravel:
		return b.bankForTravelStep(ctx, d)
	case lootchestfisherplan.PhaseTravelToFalador:
		return b.travelStep(ctx, d, lootchestfisherplan.FaladorBankStand, "the Falador west bank")
	case lootchestfisherplan.PhaseWithdrawKeys:
		return b.withdrawStep(ctx, d)
	case lootchestfisherplan.PhaseOpenChest:
		return b.openStep(ctx, d, s)
	case lootchestfisherplan.PhaseBankLoot:
		return b.bankLootStep(ctx, d)
	case lootchestfisherplan.PhaseTravelHome:
		return b.travelStep(ctx, d, b.muleMeet, "the mule")
	case lootchestfisherplan.PhaseDrainBank:
		return b.drainBankStep(ctx, d)
	}
	ctx.Delay(time.Second)
	return nil
}

// ---- the steps ----

// bankForTravelStep is the owner's "banking them for safe travel": deposit everything, wherever
// the account is standing, before a long walk. The bank is shared, so the keys are waiting at
// the far end; a death on White Wolf Mountain then costs only the walk.
func (b *lootChestBot) bankForTravelStep(ctx *Context, d *lootchestfisherrunner.Deps) error {
	ctx.SetPhase(phaselog.Banking)
	// The NEARER of the two ends, measured, not a membership test.
	//
	// This used to ask AtFalador and fall back to the mule's bank, which quietly meant "if I am
	// not provably at Falador, walk to Catherby". Live 2026-09-21 that sent an account on a
	// whole ship-chain round trip for nothing: repeated walk timeouts on the chest->bank leg
	// left it drifting east of the Falador box, AtFalador went false at x>2960, and it set off
	// for the mule's bank -- observed boarding the Port Sarim->Musa ship at 3028,3216 with
	// FIFTEEN crystal keys still sitting in the Falador bank it had just left.
	//
	// Widening the box does not fix this and I tried: the account was 38 tiles beyond even the
	// widened edge. A box can only ever answer "inside or outside", and outside has to mean
	// something -- so the moment the account is anywhere unexpected, the fallback fires. Asking
	// which bank is actually closer has no outside, degrades sensibly everywhere on the route,
	// and needs no tuning when the geography changes.
	near := b.muleMeet
	if here := lootchestfisherrunner.Here(d); tile.Chebyshev(here, lootchestfisherplan.FaladorBankStand) < tile.Chebyshev(here, b.muleMeet) {
		near = lootchestfisherplan.FaladorBankStand
	}
	bank, ok := lootchestfisherrunner.OpenNearestBank(d, near, bankSearchRadius)
	if !ok {
		ctx.logf("lootchest: could not open a bank to stow the pack before travelling — retrying")
		ctx.Delay(5 * time.Second)
		return nil
	}
	// Everything EXCEPT the fare. The wolf-free route between Catherby and Falador is the ship
	// chain (Port Sarim, Karamja, Brimhaven), and a ship wants coins — banking literally
	// everything would strand the account on the wrong side of a crossing it could otherwise
	// afford. donorplan.DefaultTravelReserve is the same 500gp ProgressiveFisher holds back for
	// the same reason, and it is the one thing a death on this leg can cost.
	lootchestfisherrunner.EnsureFare(d, bank, donorplan.DefaultTravelReserve)
	moved := lootchestfisherrunner.DepositAllExcept(d, bank, fareOnly())
	b.bankedKeys = lootchestfisherrunner.BankKeyCount(bank, b.keys)
	// Every bank this script opens also refreshes what is waiting in it for the mule: the
	// count is only knowable with a bank open, so taking it wherever one already is costs
	// nothing and keeps PhaseDrainBank from being chosen on a reading hours old.
	b.bankHaulRows = lootchestfisherrunner.BankHaulRowCount(d, bank, b.muleKeepFor(ctx))
	b.bankChecked = true
	_ = bank.Close()
	ctx.logf("lootchest: banked %d pack slot(s) before travelling, keeping %d coins as the ship fare; %d crystal key(s) in the bank",
		moved, lootchestfisherrunner.CoinsHeld(d), b.bankedKeys)
	b.askForFareIfShort(ctx, d)
	return nil
}

// askForFareIfShort asks the mule for the ship fare when the account's own bank could not
// supply it. Called after the bank is closed, because the fare arrives through a trade.
//
// Live 2026-09-20, the failure this exists for: spare104 logged
// "withdrawing 500 coin(s) for the ship fare: actions: item not found in inventory" and then
// "keeping 0 coins as the ship fare", and set off for Falador with nothing. EnsureFare can only
// draw what is already banked, and this account had no coins at all — it is a fisher, and a
// fisher that has never completed a trip has never been paid. The Catherby->Falador leg is the
// ship chain (Port Sarim, Karamja, Brimhaven) and every crossing wants coins, so a fare of zero
// strands the account on the wrong side of a crossing it could otherwise afford.
//
// The mule is the obvious source and needed no new mechanism: orderplan.DefaultCatalog already
// hands out Coins, and CoinsPerOrder is 500 — exactly donorplan.DefaultTravelReserve, the fare.
// It simply was never asked.
//
// Deliberately best-effort and non-blocking. A fare that does not arrive leaves the account
// exactly where it already was rather than wedging the cycle: the travel step's own
// "could not reach" retry still applies, and the next bank visit asks again. Bounded by
// nextFareAsk and by donorrunner.Await's own timeout, so a mule that is absent, out of range or
// out of coins can never hold this open.
func (b *lootChestBot) askForFareIfShort(ctx *Context, d *lootchestfisherrunner.Deps) {
	if ctx.Bus == nil {
		return
	}
	if lootchestfisherrunner.CoinsHeld(d) >= donorplan.DefaultTravelReserve {
		return
	}
	if time.Now().Before(b.nextFareAsk) {
		return
	}
	ctx.logf("lootchest: no ship fare in the bank — asking %q for %d coins", b.mule, orderplan.CoinsPerOrder)
	res := donorrunner.Await(&donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}, donorrunner.AwaitOptions{
		Mule: b.mule, Me: ctx.Username, Item: lootchestfisherrunner.CoinsName,
		Qty: orderplan.CoinsPerOrder, Bus: ctx.Bus,
	})
	switch {
	case res.Got:
		// Reported from the pack, not from the trade screen: the fare is only real once it is
		// actually held, which is the same receipt rule the key order and every hand-off use.
		b.nextFareAsk = time.Time{}
		ctx.logf("lootchest: %q handed over the ship fare — %d coins in the pack now", b.mule, lootchestfisherrunner.CoinsHeld(d))
	case res.Stopped:
		return
	case res.Declined:
		b.nextFareAsk = time.Now().Add(askRetryAfterDecline)
		ctx.logf("lootchest: %q declined the fare order (%s) — asking again in %s", b.mule, res.Reason, askRetryAfterDecline)
	default:
		b.nextFareAsk = time.Now().Add(askRetryAfterSilence)
		ctx.logf("lootchest: %q did not answer the fare order (%s) — asking again in %s", b.mule, res.Reason, askRetryAfterSilence)
	}
}

const bankSearchRadius = 20

func (b *lootChestBot) travelStep(ctx *Context, d *lootchestfisherrunner.Deps, dest tile.Tile, what string) error {
	ctx.SetPhase(phaselog.Walking)
	ctx.logf("lootchest: walking to %s (%d,%d)", what, dest.X, dest.Z)
	// TravelLeg, not WalkNear: with the automatic White Wolf den zones on there is no route
	// between Catherby and Falador at all for a low-combat account, so the safe route is tried
	// first and the pass only with an empty pack. See its own doc comment.
	if !lootchestfisherrunner.TravelLeg(d, dest, travelArriveRadius) {
		ctx.logf("lootchest: could not reach %s this attempt — retrying", what)
		ctx.Delay(5 * time.Second)
	}
	return nil
}

// travelArriveRadius is how close a long leg has to get before the next phase takes over. Wide
// enough that the walker's own arrival tolerance does not cause a bounce, narrow enough that
// "at Falador" and "at the mule" stay true.
const travelArriveRadius = 5

func (b *lootChestBot) withdrawStep(ctx *Context, d *lootchestfisherrunner.Deps) error {
	ctx.SetPhase(phaselog.Banking)
	bank, ok := lootchestfisherrunner.OpenNearestBank(d, lootchestfisherplan.FaladorBankStand, bankSearchRadius)
	if !ok {
		ctx.Delay(5 * time.Second)
		return nil
	}
	// Go to the chest with nothing but keys: every other slot is loot room.
	lootchestfisherrunner.DepositAllExcept(d, bank, b.keepIDs())
	banked := lootchestfisherrunner.BankKeyCount(bank, b.keys)
	// Top up to a full trip: count what is already in hand so leftovers from the last trip are
	// added to rather than ignored.
	want := lootchestfisherplan.KeysToWithdraw(banked, lootchestfisherrunner.KeysInPack(d, b.keys))
	got := lootchestfisherrunner.WithdrawKeys(d, bank, b.keys, want)
	b.bankedKeys = lootchestfisherrunner.BankKeyCount(bank, b.keys)
	// Every bank this script opens also refreshes what is waiting in it for the mule: the
	// count is only knowable with a bank open, so taking it wherever one already is costs
	// nothing and keeps PhaseDrainBank from being chosen on a reading hours old.
	b.bankHaulRows = lootchestfisherrunner.BankHaulRowCount(d, bank, b.muleKeepFor(ctx))
	_ = bank.Close()
	if got == 0 {
		ctx.logf("lootchest: asked the Falador bank for %d key(s) of %d banked and got none — retrying", want, banked)
		ctx.Delay(5 * time.Second)
		return nil
	}
	b.trips++
	ctx.logf("lootchest: trip %d — drew %d crystal key(s), %d left in the bank", b.trips, got, b.bankedKeys)
	return nil
}

func (b *lootChestBot) openStep(ctx *Context, d *lootchestfisherrunner.Deps, s lootchestfisherplan.Snapshot) error {
	ctx.SetPhase(phaselog.Looting)
	res := lootchestfisherrunner.OpenChest(d, b.keys)
	switch {
	case res.Stopped:
		return nil
	case res.Opened:
		b.opened++
		ctx.logf("lootchest: opened the crystal chest (%d this run) — %d item id(s) gained, %d key(s) left",
			b.opened, len(res.Gained), lootchestfisherrunner.KeysInPack(d, b.keys))
	default:
		ctx.logf("lootchest: the chest did not open (%s) — banking what is held and trying again", res.Reason)
		ctx.Delay(3 * time.Second)
	}
	return nil
}

func (b *lootChestBot) bankLootStep(ctx *Context, d *lootchestfisherrunner.Deps) error {
	ctx.SetPhase(phaselog.Banking)
	bank, ok := lootchestfisherrunner.OpenNearestBank(d, lootchestfisherplan.FaladorBankStand, bankSearchRadius)
	if !ok {
		ctx.Delay(5 * time.Second)
		return nil
	}
	// Keep the keys: the next open is another walk of 150 tiles away and they came from here.
	moved := lootchestfisherrunner.DepositAllExcept(d, bank, b.keepIDs())
	banked := b.bankCoinSurplus(ctx, d, bank)
	b.bankedKeys = lootchestfisherrunner.BankKeyCount(bank, b.keys)
	// Every bank this script opens also refreshes what is waiting in it for the mule: the
	// count is only knowable with a bank open, so taking it wherever one already is costs
	// nothing and keeps PhaseDrainBank from being chosen on a reading hours old.
	b.bankHaulRows = lootchestfisherrunner.BankHaulRowCount(d, bank, b.muleKeepFor(ctx))
	_ = bank.Close()
	if banked > 0 {
		ctx.logf("lootchest: banked all %d coin(s) at Falador — nothing carried to the chest", banked)
	}
	ctx.logf("lootchest: banked %d slot(s) of chest loot at Falador; %d key(s) in hand, %d in the bank",
		moved, lootchestfisherrunner.KeysInPack(d, b.keys), b.bankedKeys)
	b.joinLootedKeyHalves(ctx, d)
	return nil
}

// bankCoinSurplus banks the coins a chest run accumulates, keeping back only the fare home.
//
// Users asked for this directly (2026-09-21): spare104 was carrying its coins while
// looting chests and they should be banked once it reaches Falador. The chest's own drop table
// pays coins — "750 coins and a key half" at weight 10 is one row of several — so a long run
// accumulates a real sum and then walks it to a chest, 150 tiles from a bank, on an account that
// can die.
//
// It is NOT about pack space: coins stack into one slot whether the stack is 500 or 500,000, so
// banking them frees nothing. It is about not carrying the run's takings around. That is worth
// saying plainly, because "free a slot" would have been the obvious and wrong justification.
//
// EVERY coin, with nothing kept back. The first version of this held donorplan's 500-coin travel
// reserve for a fare home; the fleet owner saw it on a live account and said no, and on checking
// the route they are right. This run's own cycle is the mule post at Catherby to the chest at
// 2914,3451 and back, which is overland through the Taverley gate -- the ship fares in the log
// belong to a displaced account travelling in from Karamja, not to this loop. The reserve was
// funding a journey the script does not normally make.
//
// What that gives up, stated rather than hidden: an account displaced ACROSS WATER at the far end
// now carries no fare. It is not stranded -- it is standing at the Falador bank it just used,
// holding the coins it banked, and the fare handling can draw on that or ask the mule once home.
// That is one walk to a bank, against carrying the run's whole takings on an account that can die
// at every open.
func (b *lootChestBot) bankCoinSurplus(ctx *Context, d *lootchestfisherrunner.Deps, bank *actions.Bank) int {
	held := lootchestfisherrunner.CoinsHeld(d)
	if held <= 0 {
		return 0
	}
	if err := bank.DepositAll(int32(lootchestfisherplan.Coins)); err != nil {
		ctx.logf("lootchest: banking coins: %v", err)
		return 0
	}
	return held
}

// joinLootedKeyHalves turns the key halves the chest itself pays out into usable crystal keys,
// at the Falador bank, before the run decides whether to open another chest.
//
// This is not a side errand. The chest's own drop table pays "750 coins and a key half" at
// weight 10, and the same table records that key halves DO NOT STACK — so every half an
// expedition accumulates costs a whole backpack slot, and those are exactly the slots
// CanOpenAnother counts before it will allow another open. Two dead slots become one live key,
// which both frees room for loot and extends the run.
//
// Reuses mulerunner.JoinKeyHalvesRound rather than growing a second copy of the join. That one
// already does the two things this is easy to get wrong: it resolves the half ids through
// ResolveKeyIDs instead of by name (985 and 987 BOTH display the identical "Half of a key", so
// a name lookup cannot separate them), and it counts a join by the pack's own crystal-key count
// rising rather than by clicks sent.
//
// The keys it makes land in the PACK, which is where the next open wants them — DecidePhase's
// AtFalador branch sends the run straight back to the chest while KeysInPack > 0 and there is
// room. Whatever is still unspent when the keys run out is banked with everything else at
// PhaseBankForTravel and is waiting in the bank for the next trip.
func (b *lootChestBot) joinLootedKeyHalves(ctx *Context, d *lootchestfisherrunner.Deps) {
	if ctx.Config == nil || ctx.Components == nil {
		return
	}
	res, ok := mulerunner.JoinKeyHalvesRound(&mulerunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	})
	if res.Stopped {
		return
	}
	if !ok {
		// Not an error worth stopping the run for: the expedition still has whatever keys it
		// already holds, and the halves keep until the next bank visit.
		if res.Reason != "" {
			ctx.logf("lootchest: could not join the chest's own key halves (%s) — carrying on", res.Reason)
		}
		return
	}
	if res.Joined > 0 {
		// b.bankedKeys deliberately unchanged: a join withdraws halves and leaves the crystal
		// keys in the PACK, so the bank's own key count is not what moved. KeysInPack below is
		// read live from the pack, which is what the next open actually spends.
		ctx.logf("lootchest: joined %d of the chest's own key half pair(s) into crystal keys — %d key(s) in the pack now, %d half(s) left banked",
			res.Joined, lootchestfisherrunner.KeysInPack(d, b.keys), res.BankedHalves)
	}
}

// handoffStep is the whole exchange with the mule: give it everything the chest produced, then
// ask it for whatever keys it has joined since. Both halves go through donorrunner, which is
// script-agnostic on purpose (docs/script_porting_framework.md) — this script adds no trade
// state machine of its own.
// ONE MEETING, BOTH DIRECTIONS — and it is two trades, not one.
//
// The fleet owner's wording is a single exchange ("if the mule has over 100keys those should be
// traded in return for defi3lds bank") and the observed failure was half of it ("also no
// handover of loot from the chest when grabbing more keys"). This does both halves in one
// visit, but it CANNOT do them on one trade screen, and that is a property of the existing
// machinery rather than a choice made here:
//
//   - mulerunner.Handout's deliver() refuses outright if the donor puts anything on a hand-out
//     screen — "a hand-out is a pure give. Anything on the donor's side means this is not the
//     trade either of us intended" — and declines (handout.go's `len(trade.TheirOffer()) > 0`
//     branch).
//   - donorplan.DecideTrade is the mirror image on this side: "the donor only gives, and only
//     to its mule," declining any screen where the partner offers anything.
//
// Both rules are right for their own direction and a two-way screen would break both, so this
// runs GIVE first and TAKE second, back to back, while the account is already standing there.
// Give first for a concrete reason and not for tidiness: the haul leaving empties the pack,
// which is exactly the room the keys coming back need — and with the keys arriving as bank
// notes that room is one slot rather than one per key.
func (b *lootChestBot) handoffStep(ctx *Context, d *lootchestfisherrunner.Deps, s lootchestfisherplan.Snapshot) error {
	ctx.SetPhase(phaselog.Handoff)
	if s.LootInPack > 0 && b.handoffHeld(time.Now()) {
		// Backing off after a failed hand-off, and the haul is still in the pack (the give-up
		// found no bank in reach). Do not summon the mule again: try to bank it, and wait a
		// bounded minute if that fails too, rather than re-entering Loop at once.
		if !b.stowHaul(ctx, d) {
			ctx.Delay(time.Minute)
		}
	} else if s.LootInPack > 0 {
		if err := b.giveStep(ctx, d, s); err != nil {
			return err
		}
	}
	// ALWAYS, even if the give above failed. The account is standing at the mule either way,
	// and a failed hand-over is no reason to walk home without the keys that were the other
	// half of the errand. askForKeys is bounded by its own clocks in every outcome.
	b.askForKeys(ctx, d)
	return nil
}

// giveStep is the hand-over half: everything the chest produced goes to the mule.
func (b *lootChestBot) giveStep(ctx *Context, d *lootchestfisherrunner.Deps, s lootchestfisherplan.Snapshot) error {
	rd := &donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		// Bus and Me were missing here while every other donor script set them, so this
		// account was the one donor that never announced itself: donorrunner.Run publishes
		// KindReady when a load is drawn and KindCancel when it ends, and the mule uses that
		// to decide whether to keep cooking or stand ready. Without them the mule could be
		// mid-cook when this arrived, which is a plain way to lose a meeting.
		Bus: ctx.Bus, Me: ctx.Username,
	}
	// Let the hand-off reach a mule on another world; a one-world fleet no-ops.
	// See bot.WireDonorWorlds.
	WireDonorWorlds(ctx, rd)
	// The same keep rule the fisher uses, so a crystal key is never offered back to the mule
	// that made it.
	keep := fisherplan.NeverHandOver
	res := donorrunner.Run(rd, donorrunner.Options{
		Mule: b.mule, Stand: b.muleMeet,
		// BOTH are the pack's haul, and RowsInBank is NOT zero however tempting that looks.
		//
		// donorplan.Loaded reads rowsInBank first and returns Finished the moment it is zero
		// -- "nothing in the bank for the mule" -- before it ever looks at drawn. This script
		// passed a hardcoded 0 because its loot comes out of chests straight into the pack and
		// is never withdrawn from a bank, so zero read as the honest answer. It is not: it made
		// every hand-off finish instantly without trading anything, and spare104 sat at the mule
		// post retrying every five seconds with a pack full of chest loot and crystal keys it
		// could not give away (live all day, 2026-09-21, until the owner spotted the keys still
		// on it).
		//
		// donorrunner's own doc says the caller "has already withdrawn the load into the pack"
		// and that RowsInBank/Drawn describe that withdrawal. For this script the pack IS the
		// source, so the haul it is carrying is both what there was and what was drawn.
		RowsInBank: s.LootInPack, Drawn: s.LootInPack,
		Load: func() []donorplan.LoadGroup {
			return fisherplan.MuleLoad(packSlotRowsFor(ctx), donorplan.DefaultMuleFreeSlots, keep)
		},
		HaulLeft: func() int { return fisherplan.MuleHaul(packSlotRowsFor(ctx), keep) },
		// The coins above the fare go to the mule like any other haul; the fare itself stays,
		// or the account cannot pay for the ship back out again next time. Same three fields
		// and the same reserve ProgressiveFisher uses.
		CoinsName: "Coins", CoinsID: int32(lootchestfisherplan.Coins),
		CoinsHeld:       func() int { return lootchestfisherrunner.CoinsHeld(d) },
		Reserve:         donorplan.DefaultTravelReserve,
		KnownUntradable: b.knownUntradable,
	})
	switch {
	case res.Stopped:
		return nil
	case res.Traded:
		b.noteHandoffSuccess()
		ctx.logf("lootchest: handed the chest loot to %q", b.mule)
	default:
		// A wait, not a bare retry. Without one this branch returns straight into the outer
		// Loop, which calls handoffStep again immediately, and a hand-off that keeps failing
		// becomes an unbounded spin: live 2026-09-21 it wrote ~100 MB to run/fleet.log in about
		// ninety seconds (hundreds of identical lines per second) and starved the fleet's own
		// logins of CPU while it was still ramping — 105 of 299 accounts in at the time. Every
		// other step in this file that cannot make progress already waits before returning;
		// this was the one that did not.
		//
		// The trigger was a mule that could not fill the order, so the loot stayed in the pack
		// and the phase never changed. That is exactly the shape that must not busy-wait.
		//
		// The reason is printed, not summarised: this branch used to say only "did not
		// complete", which is what an operator saw for every one of the failures above
		// and gave them nothing to act on. donorrunner fills Result.Reason on every
		// non-traded exit now (see donorrunner.closeReason), so print it verbatim.
		why := res.Reason
		if why == "" {
			why = "no reason reported"
		}
		// BACK OFF BY MINUTES AND GO BACK TO WORK. This used to wait 5 s and try again, banking
		// only after five failures in a row -- and the bank then fed the load straight back
		// through PhaseDrainBank, so live 2026-09-26 06:40-06:43 spare104 summoned the mule more
		// than ten times in three minutes for three runes that could not cross. Run has already
		// waited for the mule by the time it returns here, so a failure is not a momentary busy
		// mule; bank the haul now, and leave the mule alone for HandoffBackoff, which grows with
		// every consecutive failure.
		wait := b.noteHandoffFailure(time.Now())
		ctx.logf("lootchest: hand-off to %q did not complete (%d in a row): %s — banking the load and leaving the mule alone for %s",
			b.mule, b.handoffFails, why, wait)
		b.stowHaul(ctx, d)
	}
	return nil
}

// stowHaul puts the chest haul in the bank when the mule will not take it, and reports whether
// it managed to.
//
// It is the give-up path, and it is deliberately NOT a loss: the bank is shared, so everything
// stowed here becomes an ordinary BankHaulRows row that PhaseDrainBank will offer the mule
// again on a later visit. What it actually buys is that LootInPack goes to zero, which is what
// releases the dispatcher — the account goes back to fishing or to the chest instead of
// standing in front of a mule that is not answering.
func (b *lootChestBot) stowHaul(ctx *Context, d *lootchestfisherrunner.Deps) bool {
	ctx.SetPhase(phaselog.Banking)
	bank, ok := lootchestfisherrunner.OpenNearestBank(d, lootchestfisherrunner.Here(d), bankSearchRadius)
	if !ok {
		ctx.logf("lootchest: %q will not take the haul and there is no bank in reach to stow it — carrying on and retrying", b.mule)
		return false
	}
	moved := lootchestfisherrunner.DepositAllExcept(d, bank, fareOnly())
	b.bankedKeys = lootchestfisherrunner.BankKeyCount(bank, b.keys)
	// Every bank this script opens also refreshes what is waiting in it for the mule: the
	// count is only knowable with a bank open, so taking it wherever one already is costs
	// nothing and keeps PhaseDrainBank from being chosen on a reading hours old.
	b.bankHaulRows = lootchestfisherrunner.BankHaulRowCount(d, bank, b.muleKeepFor(ctx))
	b.bankChecked = true
	_ = bank.Close()
	// handoffFails is deliberately NOT reset here: it is what makes the next backoff longer.
	ctx.logf("lootchest: %q did not take the haul (%d failed hand-off(s) in a row) — banked %d slot(s) instead; %d row(s) waiting for the mule once the hold ends",
		b.mule, b.handoffFails, moved, b.bankHaulRows)
	return true
}

// drainBankStep is the fleet owner's "hands over everything in the bank besides 500gp and
// untradeables once it has looted all keys", one packful at a time.
//
// The shape is ProgressiveFisher's own hand-off (internal/bot/fisher.go): read the bank, take
// the rows destined for the mule as BANK NOTES so a row costs one slot rather than one per
// unit, then give the lot away through donorrunner.Run. The keep list and content's own
// untradeable table are both applied by fisherplan.MuleRows, so nothing here decides what is
// tradeable — see lootchestfisherrunner/drain.go's own note on why the cache cannot answer that
// and donorplan's generated table is the authority.
//
// The 500gp is a real reserve and not a rounding: donorrunner's coin split hands the mule
// held-Reserve and leaves donorplan.DefaultTravelReserve in the pocket, which is the same
// number and the same mechanism ProgressiveFisher uses.
//
// Every exit is bounded. No bank in reach waits and returns; nothing left to draw clears the
// remembered count so the phase cannot be chosen again on a stale reading; a drawn load the
// mule will not take falls through to the same give-up as any other hand-off.
func (b *lootChestBot) drainBankStep(ctx *Context, d *lootchestfisherrunner.Deps) error {
	ctx.SetPhase(phaselog.Banking)
	bank, ok := lootchestfisherrunner.OpenNearestBank(d, b.muleMeet, bankSearchRadius)
	if !ok {
		ctx.logf("lootchest: no bank in reach to draw the mule's share out of — retrying")
		ctx.Delay(5 * time.Second)
		return nil
	}
	rows := lootchestfisherrunner.MuleBankRows(d, bank, b.muleKeepFor(ctx))
	if len(rows) == 0 {
		// The remembered count said there was something and the bank says otherwise. Believe
		// the bank, and clear the count so this phase is not chosen again on the stale reading
		// — which would be an immediate loop, since nothing else about the account changed.
		b.bankHaulRows = 0
		b.bankedKeys = lootchestfisherrunner.BankKeyCount(bank, b.keys)
		_ = bank.Close()
		ctx.logf("lootchest: nothing left in the bank for %q — back to work", b.mule)
		return nil
	}
	drawn := lootchestfisherrunner.DrawHaulForMule(d, bank, rows)
	b.bankedKeys = lootchestfisherrunner.BankKeyCount(bank, b.keys)
	left := lootchestfisherrunner.BankHaulRowCount(d, bank, b.muleKeepFor(ctx))
	b.bankHaulRows = left
	b.bankChecked = true
	_ = bank.Close()
	if drawn == 0 {
		// Nothing moved although rows existed: a row the bank no longer holds, a note mode
		// that would not switch, a full pack. Clearing the count is what stops this repeating
		// forever on a reading that cannot be acted on; the next bank visit takes it again.
		b.bankHaulRows = 0
		ctx.logf("lootchest: %d bank row(s) for %q but nothing came out of the bank — leaving it for the next visit", len(rows), b.mule)
		ctx.Delay(5 * time.Second)
		return nil
	}
	ctx.logf("lootchest: drew %d pack slot(s) of banked loot for %q, %d row(s) still banked", drawn, b.mule, left)
	return nil
}

// seedMuleKeysFromPeers refreshes this account's idea of the mule's crystal-key count from
// the mule's OWN PUBLISHED BANK, so the number never has to be learned by walking over and
// asking for keys the mule is not going to send.
//
// THE BUG THIS ENDS, from the fleet owner on 2026-09-23: "spare104 is still walking over to
// the mule????? the mule doesnt have 100 keys?????" He is right, and the walk starts the instant
// the account logs in:
//
//	00:22:30 [spare104] started
//	00:22:30 [spare104] lootchest: ... threshold 6 key(s) ... below the threshold the account
//	                   runs ProgressiveFisher
//	00:22:30 [spare104] walk to 2809,3441,0            <- the mule post
//
// DecidePhase returns PhaseHandoff on `s.Mule.WorthVisiting(s.Now) || s.Probe`, and Probe is
// "I have never heard a number". A fresh login has never heard one, so it walks to the mule to
// find out -- and then learns 69, which is below the 100 reserve, and walks back. The reading
// it needed was already published: every mule hands its whole bank to PublishBank each time it
// opens one (mulerunner.Deps.OnBankRead).
//
// The first version of this fix seeded inside askForKeys, which only runs once the account is
// ALREADY STANDING at the mule -- so it correctly stopped the pointless ORDER but not the
// pointless WALK, which is the expensive half. It belongs here, beside the snapshot that acts
// on it.
//
// It front-runs the probe rather than replacing it: a peer entry that is missing or older than
// peerplan.DefaultMaxAge leaves the reading untouched and ShouldProbe still fires, because
// asking blind beats never asking. And it never moves the reading BACKWARDS in time, so a
// count just learned from a real decline is not overwritten by an older published snapshot.
func (b *lootChestBot) seedMuleKeysFromPeers(ctx *Context) {
	stock := muleBankCount(ctx, b.mule)
	if stock == nil {
		return
	}
	have, known := stock(lootchestfisherrunner.KeyName)
	if !known {
		return
	}
	now := time.Now()
	if b.muleKeys.Heard && b.muleKeys.At.After(now) {
		return
	}
	b.muleKeys = lootchestfisherplan.MuleStock{Banked: have, At: now, Heard: true}
}

// askForKeys places one wall-bus order with the mule and reports whether any keys arrived.
//
// THREE THINGS THIS DOES THAT THE PREVIOUS VERSION DID NOT, each one a live failure the fleet
// owner reported on 2026-09-21:
//
//  1. IT ASKS FOR WHAT THE MULE ACTUALLY HAS. The old call passed orderplan.KeysPerOrder —
//     a flat 500 — every time ("donor asked the mule for 500 crystal keys instead of checking how
//     many keys the mule has"). lootchestfisherplan.Request computes the ask from the last
//     reading: the mule's WHOLE stock once it is over KeysFloor (a gate, not a reserve — the
//     owner's ruling of 2026-09-26, after 110 banked was asked for as 10: "it should have taken
//     110 keys"), clamped to the per-order cap and to real pack space.
//
//  2. IT COUNTS NOTES AS KEYS. donorrunner.Await proves a hand-out landed by watching this
//     account's pack gain the item, but it counts only UN-NOTED ids (its own unnotedIDs) — so
//     any order above 28, which is every order the mule sends as bank notes, can never satisfy
//     its receipt and comes back as "the trade closed with nothing arriving" while the notes
//     sit in the pack. The receipt here is this account's own before/after count of BOTH forms
//     (lootchestfisherrunner.KeysHeld), which is the "read the state twice, assert on what was
//     gained" rule docs/trustworthy_harnesses.md states for exactly this.
//
//  3. IT NEVER READS A POSITIONAL REFUSAL AS A STOCK ANSWER. "you are not in sight of me" says
//     nothing about how many keys the mule has, and orderplan.DeclineIsPositional exists
//     because this fleet already made that mistake at scale once (a woodcutter walked its whole
//     axe ladder down on four positional declines). A positional decline neither updates the
//     remembered count nor serves the long post-decline cooldown.
//
// Bounded in every outcome by donorrunner.Await's own timeout and by the two retry clocks, so a
// mule that is not running, not in range, or out of keys can never wedge this script.
func (b *lootChestBot) askForKeys(ctx *Context, d *lootchestfisherrunner.Deps) bool {
	if ctx.Bus == nil {
		ctx.logf("lootchest: no wall bus configured — cannot ask %q for keys", b.mule)
		return false
	}
	if time.Now().Before(b.nextAsk) {
		return false
	}
	now := time.Now()
	// Every attempt pushes the blind-probe clock, whatever comes back. That is what stops an
	// unanswerable mule turning "I have never heard a count" into a permanent reason to keep
	// walking back, and it is the exit from the unknown state.
	b.nextProbe = now.Add(probeRetry)

	// How many to ask for. A fresh reading gives the real number; without one this asks for the
	// ceiling, which is a PROBE — whatever comes back, an answer is an answer, and a decline
	// carries the mule's own count.
	want := orderplan.KeysPerOrder
	probing := true
	if b.muleKeys.Fresh(now) {
		want = lootchestfisherplan.Request(b.muleKeys.Banked, lootchestfisherrunner.PackFree(d))
		probing = false
		if want <= 0 {
			// Nothing worth asking for, and orderplan.Parse rejects a quantity of zero
			// outright, so there is no order to place at all. Say which of the two reasons it
			// is: a mule at or below the floor and a pack with no room are different problems
			// and an operator can act on one of them.
			if lootchestfisherplan.Available(b.muleKeys.Banked) <= 0 {
				ctx.logf("lootchest: %q holds %d crystal key(s), not over its %d floor — not asking, and not walking over for it",
					b.mule, b.muleKeys.Banked, lootchestfisherplan.MuleKeyThreshold)
			} else {
				ctx.logf("lootchest: %q has %d key(s) to spare but there is no room in the pack — not asking this pass",
					b.mule, lootchestfisherplan.Available(b.muleKeys.Banked))
			}
			return false
		}
	}

	held := lootchestfisherrunner.KeysHeld(d, b.keys)
	b.asks++
	if probing {
		ctx.logf("lootchest: asking %q for up to %d %s to find out what it is holding",
			b.mule, want, lootchestfisherrunner.KeyName)
	} else {
		ctx.logf("lootchest: %q banked %d %s at the last reading, over its %d floor — asking for %d (%d pack slot(s))",
			b.mule, b.muleKeys.Banked, lootchestfisherrunner.KeyName,
			lootchestfisherplan.MuleKeyThreshold, want, lootchestfisherplan.SlotsNeeded(want))
	}
	res := donorrunner.Await(&donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		Bus: ctx.Bus, Me: ctx.Username,
	}, donorrunner.AwaitOptions{
		Mule: b.mule, Me: ctx.Username, Item: lootchestfisherrunner.KeyName,
		Qty: want, Bus: ctx.Bus,
	})
	if res.Stopped {
		return false
	}

	// THE RECEIPT, taken from the pack rather than from res.Got — see point 2 above. This runs
	// on every outcome, including a decline, because a decline that arrives after the trade has
	// already closed would otherwise throw away keys that really did cross.
	got := lootchestfisherrunner.KeysHeld(d, b.keys) - held
	if got < 0 {
		got = 0
	}
	if got > 0 {
		b.gotKeys += got
		b.nextAsk = time.Time{}
		if short := lootchestfisherplan.Shortfall(want, got); short > 0 {
			// SAID OUT LOUD, ALWAYS. "the mule gave spare104 13 unnoted keys and spare104 was happy
			// with that" is precisely this line being missing: a short delivery is normal (the
			// mule's reserve, its per-window cap, a pack with no room) but it must never be
			// invisible, or the next person has to infer it from a key count that does not add
			// up.
			ctx.logf("lootchest: %q handed over %d of the %d %s asked for (%d short) — %d in the pack now, %d of them as bank notes",
				b.mule, got, want, lootchestfisherrunner.KeyName, short,
				lootchestfisherrunner.KeysHeld(d, b.keys), lootchestfisherrunner.NotedKeysInPack(d, b.keys))
		} else {
			ctx.logf("lootchest: %q handed over all %d %s asked for — %d in the pack now, %d of them as bank notes",
				b.mule, got, lootchestfisherrunner.KeyName,
				lootchestfisherrunner.KeysHeld(d, b.keys), lootchestfisherrunner.NotedKeysInPack(d, b.keys))
		}
		// Arithmetic rather than another round trip: the mule spends down to its reserve, so
		// what it has left is knowable without asking. See lootchestfisherplan.AfterDelivery.
		if b.muleKeys.Heard {
			b.muleKeys = lootchestfisherplan.MuleStock{
				Banked: lootchestfisherplan.AfterDelivery(b.muleKeys.Banked, got), At: time.Now(), Heard: true,
			}
		} else {
			// Nothing was ever heard about the stock, so the only honest reading after
			// taking everything on offer is that the mule is now empty. Assuming
			// got+threshold and subtracting got used to leave exactly the threshold --
			// i.e. "there is another full trip waiting" -- which is the belief that sent
			// the account back to a mule it had just drained.
			b.muleKeys = lootchestfisherplan.MuleStock{Banked: 0, At: time.Now(), Heard: true}
		}
		return true
	}

	switch {
	case res.Declined && orderplan.DeclineIsPositional(res.Reason):
		// Not a stock answer and not a refusal worth waiting out: the account was simply not
		// standing where the mule could see it. Short retry, and the remembered count is left
		// exactly as it was rather than being overwritten with a number nobody read.
		b.nextAsk = time.Now().Add(askRetryAfterSilence)
		// AND BRING THE PROBE CLOCK FORWARD, rather than leaving it at the full probeRetry the
		// top of this function set. A positional refusal is the one outcome a TRIP would fix and
		// another ask from here cannot: the mule answered, it simply could not see the account.
		// Without this the 30-minute ask from a fishing spot and the 30-minute probe clock push
		// each other along in lockstep and the account never goes to look.
		b.nextProbe = time.Now().Add(askRetryAfterSilence)
		ctx.logf("lootchest: %q could not see the account to hand keys over (%s) — that says nothing about its stock; walking to it instead of asking again from here",
			b.mule, res.Reason)
	case res.Declined:
		if n, ok := lootchestfisherplan.ParseBankedCount(res.Reason, lootchestfisherrunner.KeyName); ok {
			// The whole point of the ask when the answer is no: the mule's decline carries its
			// own real bank count, which is the reading that decides whether coming back is
			// worth it. orderplan's Handout.Floor doc calls this the designed channel.
			b.muleKeys = lootchestfisherplan.MuleStock{Banked: n, At: time.Now(), Heard: true}
			ctx.logf("lootchest: %q is holding %d %s, not over its %d floor — fishing until it has more",
				b.mule, n, lootchestfisherrunner.KeyName, lootchestfisherplan.MuleKeyThreshold)
			// A stock answer is not a punishment: the ordinary ask clock is enough, and the
			// long post-decline cooldown exists for a mule refusing on POLICY grounds.
			b.nextAsk = time.Now().Add(askRetryAfterSilence)
			return false
		}
		b.nextAsk = time.Now().Add(askRetryAfterDecline)
		ctx.logf("lootchest: %q declined the key order (%s) — asking again in %s", b.mule, res.Reason, askRetryAfterDecline)
	default:
		b.nextAsk = time.Now().Add(askRetryAfterSilence)
		ctx.logf("lootchest: %q did not answer the key order (%s) — asking again in %s", b.mule, res.Reason, askRetryAfterSilence)
	}
	return false
}

// probeRetry is how long to leave the mule alone after an attempt to find out what it holds.
//
// It is the clock that bounds the "I do not know" state. Sized on the mule's own ledger window
// (orderplan.DefaultWindow, 30 minutes) because that is the period its per-donor cap is
// measured over, so probing faster cannot produce an answer the previous probe did not already
// have — and slower would leave the account fishing through a stockpile it could be spending.
const probeRetry = orderplan.DefaultWindow

// fishStep hands the account to the registered ProgressiveFisher bot, and takes it back every
// fishBetweenAsks to see whether the mule has made enough keys yet.
func (b *lootChestBot) fishStep(ctx *Context, d *lootchestfisherrunner.Deps) error {
	// Before settling into fishing, find out what is actually banked. bankedKeys starts at zero
	// and only a real bank open can change it, so an account restarted with a bank full of keys
	// reads as "no keys" and would fish for a whole fishBetweenAsks before its first ask told it
	// otherwise. This is the one phase that can be entered while the count is still unknown.
	if b.countBankedKeys(ctx, d) {
		return nil
	}
	// Ask the mule BEFORE settling into fishing, not fishBetweenAsks after it.
	//
	// The old order could never place a first order at all on a real fleet. The ask below is
	// gated on time.Since(b.fishingSince) >= fishBetweenAsks (30 minutes) measured from when
	// THIS fisher started, and b.fishingSince is reset every time a new fisher is created — so
	// any account whose fishing was interrupted more often than every 30 minutes never reached
	// its own first ask. Live on 2026-09-20 spare104 was interrupted every ~4.5 minutes by the
	// PhaseTravelHome bug (see DecidePhase's rule 4) and so asked for keys exactly zero times
	// in 50 minutes, while the mule sat on 417 banked crystal keys the whole time.
	//
	// Fixing rule 4 alone would leave a 30-minute delay on every fresh start for no reason: the
	// stock is already there, and the point of this script is to open chests, with fishing only
	// the fallback while the mule has none. Asking first inverts that back to the intended
	// order. Bounded exactly as the periodic ask is — askForKeys itself honours b.nextAsk
	// (30 min after a decline, 5 min after silence) and donorrunner.Await carries its own
	// timeout, so a mule that is absent, out of range or out of keys can never wedge this.
	if b.fisher == nil && ctx.Bus != nil && time.Now().After(b.nextAsk) {
		if b.askForKeys(ctx, d) {
			// Keys arrived. Drop out without starting a fisher at all and let the next Loop
			// dispatch to the chest run.
			return nil
		}
	}
	if b.fisher == nil {
		// keepCrystalKeys: this delegate holds keys the MULE made, and handing them back
		// would trade the same keys in a circle. An ordinary fisher hands over the keys it
		// joined itself. See fisherplan.NeverHandOver.
		b.fisher = &fisherBot{keepCrystalKeys: true}
		if err := b.fisher.OnStart(ctx); err != nil {
			b.fisher = nil
			ctx.logf("lootchest: could not hand over to ProgressiveFisher (%v) — waiting before trying again", err)
			ctx.Delay(30 * time.Second)
			return nil
		}
		b.fishingSince = time.Now()
		ctx.logf("lootchest: below %d key(s) — fishing as ProgressiveFisher until the mule has made enough", b.minKeys)
	}
	if time.Since(b.fishingSince) >= fishBetweenAsks && ctx.Bus != nil && time.Now().After(b.nextAsk) {
		b.fishingSince = time.Now()
		ctx.logf("lootchest: pausing fishing to ask %q whether it has made more keys", b.mule)
		if b.askForKeys(ctx, d) {
			// Keys arrived. Drop out of fishing and let the next Loop dispatch afresh.
			b.stopFishing(ctx)
			return nil
		}
	}
	return b.fisher.Loop(ctx)
}

// countBankedKeys opens whatever bank is nearby once, reads how many crystal keys are in it,
// and reports whether it changed the answer enough to re-dispatch.
//
// Bounded and give-up-able on purpose: an account that is nowhere near a bank must not stand
// looking for one instead of fishing, so a failed attempt simply waits bankCheckRetry and the
// ordinary key order still works meanwhile.
func (b *lootChestBot) countBankedKeys(ctx *Context, d *lootchestfisherrunner.Deps) bool {
	if b.bankChecked || time.Now().Before(b.bankCheckAt) {
		return false
	}
	ctx.SetPhase(phaselog.Banking)
	defer ctx.SetPhase(phaselog.Idle)
	bank, ok := lootchestfisherrunner.OpenNearestBank(d, lootchestfisherrunner.Here(d), bankSearchRadius)
	if !ok {
		b.bankCheckAt = time.Now().Add(bankCheckRetry)
		ctx.logf("lootchest: no bank in reach to count the banked keys — carrying on and trying again in %s", bankCheckRetry)
		return false
	}
	was := b.bankedKeys
	b.bankedKeys = lootchestfisherrunner.BankKeyCount(bank, b.keys)
	// Every bank this script opens also refreshes what is waiting in it for the mule: the
	// count is only knowable with a bank open, so taking it wherever one already is costs
	// nothing and keeps PhaseDrainBank from being chosen on a reading hours old.
	b.bankHaulRows = lootchestfisherrunner.BankHaulRowCount(d, bank, b.muleKeepFor(ctx))
	b.bankChecked = true
	_ = bank.Close()
	ctx.logf("lootchest: the bank holds %d crystal key(s) (threshold %d)", b.bankedKeys, b.minKeys)
	// Only worth re-dispatching if the reading actually crosses the threshold; otherwise fall
	// straight through into fishing rather than spending a Loop call on nothing.
	return b.bankedKeys != was && b.bankedKeys >= b.minKeys
}

// bankCheckRetry is how long to leave the one-off banked-key count alone after an attempt that
// could not reach a bank.
const bankCheckRetry = 10 * time.Minute

func (b *lootChestBot) stopFishing(ctx *Context) {
	if b.fisher == nil {
		return
	}
	b.fisher.OnStop(ctx)
	b.fisher = nil
	// Deliberately not "enough keys to go": this also runs when the account has loot to hand
	// over or is already mid-trip, and a log line that names the wrong reason is worse than one
	// that names none.
	ctx.logf("lootchest: taking the account back from ProgressiveFisher")
}

func (b *lootChestBot) OnStop(ctx *Context) {
	b.stopFishing(ctx)
	ctx.logf("lootchest: stopped — %d chest(s) opened over %d trip(s), %d key order(s) placed, %d key(s) collected",
		b.opened, b.trips, b.asks, b.gotKeys)
}

// packSlotRowsFor is the pack as one donorplan row per slot, the shape MuleLoad/MuleHaul take.
func packSlotRowsFor(ctx *Context) []donorplan.ItemRow {
	inv := ctx.Session.State().Inventory(lootchestfisherrunner.PlayerPack)
	if inv == nil {
		return nil
	}
	var out []donorplan.ItemRow
	for _, it := range inv.NonEmpty() {
		name := ""
		if ctx.Config != nil {
			if obj, ok := ctx.Config.Obj(int(it.ID)); ok {
				name = obj.Name
			}
		}
		if strings.TrimSpace(name) == "" {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		// THE ID IS NOT OPTIONAL, and leaving it zero is what kept spare104 at the mule post
		// declining trades every fourteen seconds.
		//
		// donorrunner keys its learned "this never reached the trade screen" skip set on
		// LoadGroup.ID, and LoadGroup.ID comes from here. With every row built at ID 0, every
		// group collided: the FIRST group that failed to offer -- an untradeable Strange fruit
		// from a random event -- set skip[0], and withoutSkipped then stripped the whole plan,
		// Death runes and all. So the trade closed with nothing offered, the haul was still
		// there next pass, and the account came straight back.
		//
		// It also silently disabled donorplan.Untradeable, which is guarded on id > 0: the
		// 963-id content list could never match a row from this script. Every other caller
		// builds rows from a BANK read, where the id is already to hand; this one builds them
		// from the pack and dropped it.
		out = append(out, donorplan.ItemRow{Name: name, ID: int(it.ID), Count: c})
	}
	return out
}
