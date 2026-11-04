package bot

// A RuneCarrier shift (runecarrier.go has the whole story): the stage loop and the live world it runs over.
//
//	ask the kit    the mule's post: bank the pack (the ring stays), ask for 3,000 NOTED Rune essence and 10,000 coins in one bundled
//	               kit order, and a Ring of dueling (8) when none is held; the essence counts against the other carriers'
//	               claims first (carrierClaims), so eight carriers reading one published bank do not all walk to the post
//	cross          the ship from Ardougne and the jungle road to Jiminua's store, around the spiders (runecraftrunner.WalkKaramja)
//	shelf          sell the notes the trip needs plus those that lift her shelf to carrierShelfFloor, buy the unnoted load back at
//	               the floor price (runecraftplan.PlanShelf; runecraftrunner.Shelf judges the visit by the pack)
//	walk to gate   the 122 steps to the ruins' exit tile
//	trade          ask the crafter every two ticks until the screen opens, Offer-All the 25 essence (or, when his gate message says he
//	               takes less, exactly that, keeping the rest for the next load), accept, check the confirm screen, accept
//	               (runecraftrunner.TradeWithCrafter); a crafter inside the temple is simply not in view yet
//	courier        with the kit spent and Natures beyond his keep at the gate, take them home (his policy: craftRuneCourierMax)
//	go home        the ship and the road back to the mule's post; the leftovers (notes, Natures) are handed to the mule
//
// A stage that fails three times in a row, or that makes no progress, does not strand the account on the island holding the kit: it
// ENDS THE SHIFT (End), and the same stage machine then sends the carrier home (DecideCarrierStage reads Over). Only a failure while
// already ending gives up, and the account is then left where it stands (the next Loop resumes a shift for a kit in the pack).
// Nothing here is believed until it is observed: a trade counts when the pack changed by what the confirm screen showed, a shelf
// visit when the pack holds the essence.

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
)

const (
	// carrierStepFails is how many times in a row one stage may fail before the shift ends.
	carrierStepFails = 3
	// carrierStall is how many times in a row the same stage may "succeed" with nothing changed before it counts as trouble
	// (the waits are not stalls).
	carrierStall = 8
	// carrierMaxSteps bounds one shift's steps (a kit of 3,000 is a thousand or so).
	carrierMaxSteps = 20000
	// carrierTradeLine is how many trips a pace line covers.
	carrierTradeLine = 10
	// carrierFareFloor is the coins under which a crossing is not started: the ship is 30 each way.
	carrierFareFloor = 70
	// carrierCrossWalk bounds the walk from the post to the store (the road to Ardougne, the ship, the jungle: about 330 steps),
	// carrierLegWalk the walk between the store and the gate, carrierHomeWalk the walk home.
	carrierCrossWalk = 40 * time.Minute
	carrierLegWalk   = 20 * time.Minute
	carrierHomeWalk  = 45 * time.Minute
	// carrierGateWalk bounds the walk from the store onto the wait tile: the leg takes about a minute and a half, so a walk that has
	// not arrived in four minutes will not (carrierLegWalk would hold a carrier for twenty).
	carrierGateWalk = 4 * time.Minute
	// carrierClosesInARow is how many trade screens may close before anybody accepted, one after another with no trade between them,
	// before that is a failure (a passing race closes one in five at the worst); carrierCapDeclines how many declines the crafter's
	// gate message explains (he takes less than the carrier held) are followed before one counts: the offer shrinks to his room the
	// next time, so a second is already unusual.
	carrierClosesInARow = 8
	carrierCapDeclines  = 4
)

// The waits of a shift. Variables so a test can shorten them.
var (
	carrierWaitStep = 3 * time.Second
	carrierRestStep = 5 * time.Second
	// carrierTradeWait bounds one trade step (a minute: the step is then read again, the gate's state and the heartbeat with it).
	carrierTradeWait   = time.Minute
	carrierTradeAbsent = 45 * time.Second
	carrierCourierWait = 2 * time.Minute
	// carrierFailPause is the wait between a stage's failure and its retry.
	carrierFailPause = 3 * time.Second
)

// The shift's I/O, replaceable in a test.
var (
	carrierWalk    = runecraftrunner.WalkKaramja
	carrierShelf   = runecraftrunner.Shelf
	carrierTrade   = runecraftrunner.TradeWithCrafter
	carrierRubItem = runecraftrunner.RubItem
	// carrierWalkAcross is the walk from the far side of the water to the post (askKit): ScarabSwarm's own, the dens and the Ardougne
	// bears kept out of and nothing else. carrierWalk cannot make it: the corridor's boxes and the wrong-boat zone (the Port Sarim
	// boat's landing and the road from Musa Point to Brimhaven) lie across the only way, and the planner answers "unreachable" for
	// every bank on the mainland (TestTheCarriersWalkFromAMainlandBankToThePostPlansOnTheRealPack).
	carrierWalkAcross = walkAcrossByWalker
	// carrierDrawFares tops the pack's coins up from the bank at the tile (scarabrunner.WithdrawCoinsTo).
	carrierDrawFares = fleetrunner.WithdrawCoinsTo
)

// carrierSteps is the shift's world: what it reads and what each stage does to it.
type carrierSteps interface {
	// Read is the state for runecraftplan.DecideCarrierStage; Fingerprint changes when anything a stage could have changed does.
	Read() runecraftplan.CarrierSnapshot
	Fingerprint() string
	// Prepare runs before every step (the death latch, a random event, the heartbeat); it reports false when the bot was stopped.
	Prepare() bool
	Stopped() bool
	Pause(time.Duration)
	// Do carries out one stage.
	Do(runecraftplan.CarrierStep) runeStepResult
	// End says the shift must end now (the stage machine then sends the carrier home); Ending whether it already has.
	End(why string)
	Ending() bool
}

// carrierLoopResult is how the loop ended.
type carrierLoopResult struct {
	Reason string
	// Done: the stage machine reached CarrierDone. Died: the account died. GaveUp: trouble while already ending. Stopped: the bot
	// was stopped.
	Done, Died, GaveUp, Stopped bool
	Steps                       int
	Stages                      map[runecraftplan.CarrierStage]int
}

// runCarrierShift decides a stage from the world's state and does it until the shift is done.
func runCarrierShift(w carrierSteps, logf func(format string, args ...any), maxSteps int) (res carrierLoopResult) {
	res.Stages = map[runecraftplan.CarrierStage]int{}
	var last runecraftplan.CarrierStage
	lastSig := ""
	fails, same := 0, 0
	// trouble ends the shift (the stage machine then goes home); trouble while already ending is a give-up.
	trouble := func(reason string) (stop bool) {
		if w.Ending() {
			res.GaveUp, res.Reason = true, reason
			return true
		}
		logf("WARNING: ending the shift: %s", reason)
		w.End(reason)
		fails, same = 0, 0
		return false
	}
	for res.Steps < maxSteps {
		if w.Stopped() || !w.Prepare() {
			res.Stopped, res.Reason = true, "stopped"
			return res
		}
		step := runecraftplan.DecideCarrierStage(w.Read())
		res.Steps++
		res.Stages[step.Stage]++
		if step.Stage != last {
			logf("stage %s: %s", step.Stage, step.Why)
			fails, same = 0, 0
		}
		waiting := step.Stage == runecraftplan.CarrierWait || step.Stage == runecraftplan.CarrierRest || step.Stage == runecraftplan.CarrierTrade
		sig := w.Fingerprint()
		if step.Stage == last && sig == lastSig && !waiting {
			same++
		} else {
			same = 0
		}
		last, lastSig = step.Stage, sig
		if same >= carrierStall {
			if trouble(fmt.Sprintf("%s ran %d times in a row without anything changing", step.Stage, same+1)) {
				return res
			}
			continue
		}
		switch step.Stage {
		case runecraftplan.CarrierDone:
			res.Done, res.Reason = true, step.Why
			return res
		case runecraftplan.CarrierRecover:
			res.Died, res.Reason = true, step.Why
			return res
		}
		r := w.Do(step)
		if r.Stopped {
			res.Stopped, res.Reason = true, "stopped"
			return res
		}
		if r.OK {
			fails = 0
			continue
		}
		fails++
		logf("WARNING: %s failed (%d in a row): %s", step.Stage, fails, r.Why)
		if fails >= carrierStepFails {
			if trouble(fmt.Sprintf("%s failed %d times in a row: %s", step.Stage, fails, r.Why)) {
				return res
			}
			continue
		}
		w.Pause(carrierFailPause)
	}
	res.GaveUp, res.Reason = true, fmt.Sprintf("%d steps without finishing", maxSteps)
	return res
}

// ---- the live world ----

type carrierWorld struct {
	b       *runeCarrierBot
	ctx     *Context
	d       *fleetrunner.Deps
	set     runecraftplan.CarrierSettings
	started time.Time

	post      tile.Tile
	postKnown bool

	kitAsked bool
	over     string
	resting  bool
	died     bool
	// buyOnly says Jiminua's shop has no room for a sale (it is bought from only); courierTried that the courier trade was tried.
	buyOnly, courierTried bool
	// waitSince is when the carrier began standing at the gate with a load and no trade.
	waitSince time.Time

	// the heartbeat on the bus (hbMu: it is also published by a goroutine for the whole shift, see startBeat)
	hbMu  sync.Mutex
	role  mulebus.CarrierRole
	pubAt time.Time

	// The shift's figures.
	trips, essence, natures   int
	paid, earned              int
	tradeFails, tradeTimeouts int
	// silentTimeouts are the attempts since the last trade in which requests were sent and the game said nothing of any of them: a crafter
	// the engine never lets this carrier ask (runecraftrunner.OfferResult.Silent).
	silentTimeouts int
	// tradeCloses are the trade screens that closed before anybody accepted (nothing crossed; the carrier asked again); closesInARow
	// those since the last trade; capDeclines the crafter's declines since the last trade that his gate message explains.
	tradeCloses    int
	closesInARow   int
	capDeclines    int
	cycleSum       time.Duration
	cycles         int
	lastTradeAt    time.Time
	delivered      map[string]int
	kept           int
	notes0, coins0 int
	said           map[string]time.Time
	// lastFail is the last stage failure's reason, for the shift's closing line.
	lastFail string
}

func (w *carrierWorld) logf(format string, args ...any) {
	w.b.logf(w.ctx, format, args...)
}

// sayEvery is logf at most once per `every` for one key.
func (w *carrierWorld) sayEvery(key string, every time.Duration, format string, args ...any) {
	if t, ok := w.said[key]; ok && time.Since(t) < every {
		return
	}
	w.said[key] = time.Now()
	w.logf(format, args...)
}

func (w *carrierWorld) Stopped() bool         { return w.ctx.Stopped() }
func (w *carrierWorld) Pause(d time.Duration) { w.ctx.Delay(d) }
func (w *carrierWorld) Ending() bool          { return w.over != "" }

func (w *carrierWorld) End(why string) {
	if w.over == "" {
		w.over = why
	}
}

// Prepare is the start of every step: the death latch (a death ends the shift: the kit is on the floor of the island and the
// respawn is in Lumbridge), a random event, the heartbeat.
func (w *carrierWorld) Prepare() bool {
	if w.ctx.Session.State().IsDead() && !w.died {
		w.died = true
		w.logf("WARNING: the account DIED at %s with %d notes, %d essence and %d coins: the shift is over", fleetrunner.HereIs(w.d),
			runecraftrunner.Notes(w.d), runecraftrunner.Essence(w.d), runecraftrunner.Coins(w.d))
	}
	if !w.died {
		masterClearDead(w.ctx)
	}
	ok := fleetrunner.WaitForGuardian(w.d)
	if ok && !w.died {
		w.heartbeat(w.role)
	}
	return ok
}

// heartbeat tells the crafter the carrier is at work (and the role): on a change at once, else every mulebus.CarrierEvery.
func (w *carrierWorld) heartbeat(role mulebus.CarrierRole) {
	if w.ctx.Bus == nil || w.ctx.Username == "" || w.set.Crafter == "" {
		return
	}
	w.hbMu.Lock()
	defer w.hbMu.Unlock()
	now := time.Now()
	if role == w.role && now.Sub(w.pubAt) < mulebus.CarrierEvery && !w.pubAt.IsZero() {
		return
	}
	if role == "" {
		role = mulebus.CarrierTransit
	}
	w.role, w.pubAt = role, now
	w.ctx.Bus.Publish(mulebus.OnWorld(mulebus.CarrierMessage(w.ctx.Username, w.set.Crafter, runecraftrunner.Here(w.d), role, runecraftrunner.Essence(w.d)), w.ctx.World()))
}

// carrierBeatEvery is how often the heartbeat goroutine looks (a variable so a test can shorten it): the heartbeat itself is published
// at most every mulebus.CarrierEvery.
var carrierBeatEvery = 5 * time.Second

// startBeat repeats the heartbeat for the whole shift, whatever the stage is doing. The heartbeat used to be published between
// steps only, and one trade step can run for minutes: live 16 Oct 2026 a carrier waited three minutes (92 requests) for a trade
// screen while the crafter, who believes a carrier for mulebus.CarrierTTL (a minute) after its last message, logged its requests as a
// stranger's and ignored them. It returns the stop.
func (w *carrierWorld) startBeat() (stop func()) {
	if w.ctx.Bus == nil || w.ctx.Username == "" || w.set.Crafter == "" {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(carrierBeatEvery)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				w.hbMu.Lock()
				role := w.role
				w.hbMu.Unlock()
				w.heartbeat(role)
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

// gate is the crafter's gate as the bus has it. A courier that has taken (or tried to take) Natures asks for no more.
func (w *carrierWorld) gate() runecraftplan.GateInfo {
	info, _ := w.b.gateInfo(w.ctx, time.Now())
	if w.courierTried || runecraftrunner.Natures(w.d) > 0 {
		info.Surplus = 0
	}
	return info
}

func (w *carrierWorld) Read() runecraftplan.CarrierSnapshot {
	d := w.d
	here := runecraftrunner.Here(d)
	st := w.ctx.Session.State()
	if hp, ok := st.HP(); ok {
		maxHP, _ := st.MaxHP()
		w.resting = runecraftplan.HPLow(hp, maxHP, w.resting)
	}
	s := runecraftplan.CarrierSnapshot{
		Dead: w.died, Resting: w.resting, Place: runecraftplan.PlaceOf(here),
		Notes: runecraftrunner.Notes(d), Unnoted: runecraftrunner.Essence(d), Natures: runecraftrunner.Natures(d), Coins: runecraftrunner.Coins(d),
		Gate: w.gate(), KitAsked: w.kitAsked, Over: w.over != "",
	}
	if w.postKnown {
		s.AtPost = here.Level == w.post.Level && tile.Chebyshev(here, w.post) <= masterNearPost
	}
	if s.Place == runecraftplan.PlaceGate && s.Unnoted >= runecraftplan.MinLoad {
		if w.waitSince.IsZero() {
			w.waitSince = time.Now()
		}
		s.GateWaited = time.Since(w.waitSince)
		if s.GateWaited >= runecraftplan.GateWaitMax {
			// Home with the kit and not across again: the gate is jammed or its crafter gone.
			why := "waited " + s.GateWaited.Round(time.Second).String() + " at the gate with a load and no trade"
			if w.silentTimeouts > 0 {
				why += fmt.Sprintf(" (the game said nothing of the requests in %d attempt(s): the crafter could not be asked from here)", w.silentTimeouts)
			}
			w.End(why)
			s.Over = true
		}
	} else if s.Unnoted < runecraftplan.MinLoad {
		w.waitSince = time.Time{}
	}
	return s
}

func (w *carrierWorld) Fingerprint() string {
	d := w.d
	return fmt.Sprintf("%v|%d|%d|%d|%d|%d|%v", runecraftrunner.Here(d), runecraftrunner.Notes(d), runecraftrunner.Essence(d), runecraftrunner.Natures(d), runecraftrunner.Coins(d), w.trips, w.kitAsked)
}

// roleOf is the role a stage is published as.
func roleOf(stage runecraftplan.CarrierStage) mulebus.CarrierRole {
	switch stage {
	case runecraftplan.CarrierTrade, runecraftplan.CarrierWait:
		return mulebus.CarrierWaiting
	case runecraftplan.CarrierCourier:
		return mulebus.CarrierCourier
	case runecraftplan.CarrierGoHome, runecraftplan.CarrierDeliver, runecraftplan.CarrierDone, runecraftplan.CarrierRecover:
		return mulebus.CarrierHome
	}
	return mulebus.CarrierTransit
}

func (w *carrierWorld) Do(step runecraftplan.CarrierStep) runeStepResult {
	w.heartbeat(roleOf(step.Stage))
	r := w.do(step)
	if !r.OK && !r.Stopped {
		w.lastFail = r.Why
	}
	return r
}

func (w *carrierWorld) do(step runecraftplan.CarrierStep) runeStepResult {
	switch step.Stage {
	case runecraftplan.CarrierRest:
		return w.rest()
	case runecraftplan.CarrierAskKit:
		return w.askKit()
	case runecraftplan.CarrierCross:
		return w.cross()
	case runecraftplan.CarrierWalkToStore:
		return w.walkToStore()
	case runecraftplan.CarrierShelf:
		return w.shelf()
	case runecraftplan.CarrierWalkToGate:
		return w.walkToGate()
	case runecraftplan.CarrierTrade:
		return w.trade()
	case runecraftplan.CarrierWait:
		return w.waitAtGate()
	case runecraftplan.CarrierCourier:
		return w.courier()
	case runecraftplan.CarrierGoHome:
		return w.goHome()
	case runecraftplan.CarrierDeliver:
		return w.deliver()
	}
	return runeFailf("no handler for stage %s", step.Stage)
}

// walk is the corridor walk: around the jungle spiders, by the ship, the run stall on.
func (w *carrierWorld) walk(dest tile.Tile, radius int, timeout time.Duration) bool {
	return carrierWalk(w.d, dest, radius, timeout)
}

// walkAcross is the walk from the far side of the water to the post (carrierWalkAcross).
func (w *carrierWorld) walkAcross(dest tile.Tile, radius int, timeout time.Duration) bool {
	return carrierWalkAcross(w.d, dest, radius, timeout)
}

// walkAcrossByWalker is scarabrunner.WalkTo for a session with a planner, and no walk at all without one: WalkTo's fallback for a
// missing walker is plain pathfinding, which knows no wolf den, and from a bank east of White Wolf Mountain its shortest line to the
// post is the mountain's pass. The shift then fails and backs off rather than walking it.
func walkAcrossByWalker(d *fleetrunner.Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
	if d.Walker == nil {
		return false
	}
	return fleetrunner.WalkTo(d, dest, radius, timeout)
}

// drawFares makes the pack hold the ships' fares for the walk from where the account stands to dest, from the nearest bank
// (scarabplan.DecideFare's draw: at least scarabplan.FareFloat, so the far side is not reached with an empty purse). It returns why it
// could not, "" when the pack holds them. A carrier does not earn them: a bank with no coins ends the shift, which backs off.
func (w *carrierWorld) drawFares(dest tile.Tile) string {
	d := w.d
	here := fleetrunner.Here(d)
	dec := fleetplan.DecideFare(fleetplan.FareInput{Here: here, Dest: &dest, Coins: runecraftrunner.Coins(d)})
	if dec.Action == fleetplan.FareNone {
		return ""
	}
	bank := questplan.NearestBank(*here)
	got, why := carrierDrawFares(d, bank.Tile, dec.Draw)
	w.logf("fares: %s; drew at the %s bank: carrying %d %s", dec.Why, bank.Name, got, why)
	if got < dec.Need {
		return fmt.Sprintf("the ships to the mule's post cost %d coins and the pack holds %d (%s)", dec.Need, got, firstNonEmpty(why, "the "+bank.Name+" bank has no more"))
	}
	return ""
}

// muleStand is where the mule is met: its heartbeat's post, else the one the shift started at, else the fixed post.
func (w *carrierWorld) muleStand() tile.Tile {
	if p := scarabMulePresenceOf(w.ctx, w.set.Mule); p.Heard && p.SameWorld {
		w.post, w.postKnown = p.Post, true
	}
	if !w.postKnown {
		w.post, w.postKnown = scarabSwarmMulePost, true
	}
	return w.post
}

// ---- the stages ----

func (w *carrierWorld) rest() runeStepResult {
	hp, _ := w.ctx.Session.State().HP()
	maxHP, _ := w.ctx.Session.State().MaxHP()
	w.sayEvery("rest", 5*time.Minute, "resting at %s: %d/%d hitpoints (back to %d%%)", fleetrunner.HereIs(w.d), hp, maxHP, runecraftplan.RestToPercent)
	if !w.ctx.Delay(carrierRestStep) {
		return runeStopped()
	}
	return runeOK()
}

// askKit is the kit ask at the mule's post: the shift's essence, coins and run stall ring. It is asked once a shift whatever
// comes of it (kitAsked): a mule that gave nothing ends the shift, it is not asked again at once.
func (w *carrierWorld) askKit() runeStepResult {
	ctx, d, set := w.ctx, w.d, w.set
	w.kitAsked = true
	now := time.Now()
	p := scarabMulePresenceOf(ctx, set.Mule)
	if !p.Heard || !p.SameWorld {
		return runeFail("the mule is not here: " + p.Why)
	}
	w.post, w.postKnown = p.Post, true
	if g := w.gate(); !g.Usable() {
		return runeFail("the gate is not open now (" + gateWord(g) + ")")
	}
	have, updated, known := muleEssence(ctx, set.Mule)
	if !known {
		return runeFail("the mule has not published its bank")
	}
	avail := max(0, have-carrierClaims.others(ctx.Username, now, updated))
	kit := runecraftplan.KitEssence(set.KitEssence, avail)
	if worth := min(runecraftplan.ShiftMinEssence, runecraftplan.KitEssence(set.KitEssence, set.KitEssence)); kit < worth || kit == 0 {
		return runeFailf("the mule holds %d Rune essence (%d after the other carriers' claims), under the %d a shift is worth", have, avail, worth)
	}
	if why, long := scarabMuleQueueTooLong(ctx, set.Mule, donorrunner.DefaultKitTimeout); long {
		return runeFail(why)
	}
	// A shift that starts across the water from the post (ScarabSwarm's quest hold parks an account at its nearest bank, Varrock's for a
	// Varrock quest: runecarrier.go farFromMule) walks there by the ships. The walker plans a route over them only while the pack pays
	// their fares, so they are drawn from the bank the account stands at first, before the mule's essence is claimed for the walk.
	walk, across := w.walk, freshstart.FareToCross(fleetrunner.Here(d), &p.Post) > 0
	if across {
		if why := w.drawFares(p.Post); why != "" {
			return runeFail(why)
		}
		walk = w.walkAcross
		w.logf("the post at %d,%d is across the water from %s: walking there by the ships, around White Wolf Mountain", p.Post.X, p.Post.Z, fleetrunner.HereIs(d))
	}
	// The claim stands until the shift ends (or the mule publishes again): the published bank lags the hand-out by a few minutes.
	carrierClaims.claim(ctx.Username, kit, now)
	if !walk(p.Post, 3, carrierHomeWalk) {
		carrierClaims.release(ctx.Username)
		return runeFailf("could not reach %s's post at %d,%d from %s", set.Mule, p.Post.X, p.Post.Z, fleetrunner.HereIs(d))
	}
	if across {
		// The ships took ten to fifteen minutes and the mule may have published its bank since: stake the claim again from now so
		// another carrier reading that publish does not count this kit as free.
		carrierClaims.claim(ctx.Username, kit, time.Now())
	}
	masterDismissChat(ctx)
	var keep []string
	if rub := carrierRubItem(d); rub != "" {
		keep = append(keep, rub)
	}
	w.logf("asking %s for %s noted Rune essence (%d trips) and %s coins (the mule holds %s, %d claimed by other carriers)", set.Mule,
		runecraftplan.Thousands(kit), runecraftplan.ShiftTrips(kit), runecraftplan.Thousands(set.KitCoins), runecraftplan.Thousands(have), have-avail)
	items := []fleetplan.ItemCount{{Item: runecraftplan.EssenceName, Count: kit}, {Item: "Coins", Count: set.KitCoins}}
	got, info := scarabKitBundle(d, ctx.Bus, ctx.Username, set.Mule, items, fleetrunner.KitBundleOptions{
		Timeout: donorrunner.DefaultKitTimeout, Bank: p.Post, Keep: keep, KeepNotes: true, Noted: true,
		FallbackTimeout: 90 * time.Second, World: ctx.CurrentWorld,
	})
	if info.Stopped {
		return runeStopped()
	}
	notes, coins := runecraftrunner.Notes(d), runecraftrunner.Coins(d)
	if notes < runecraftplan.CarrierOffer {
		carrierClaims.release(ctx.Username)
		why := firstNonEmpty(declinedWord(got.Declined), firstNonEmpty(info.Reason, "nothing arrived"))
		return runeFailf("the mule gave %d noted Rune essence (asked %d): %s", notes, kit, why)
	}
	if coins < runecraftplan.ShelfMinCoins {
		return runeFailf("the mule gave %d coins and the shelf needs %d (asked %d; %s)", coins, runecraftplan.ShelfMinCoins, set.KitCoins, declinedWord(got.Declined))
	}
	w.notes0, w.coins0 = notes, coins
	rub := w.askRing(p.Post)
	w.logf("kit: %s noted Rune essence (%d trips), %s coins, run stall item %s; the mule's answer %v", runecraftplan.Thousands(notes), runecraftplan.ShiftTrips(notes),
		runecraftplan.Thousands(coins), firstNonEmpty(rub, "NONE (the walks are not stalled)"), got.Got)
	return runeOK()
}

// askRing makes sure the pack holds a rub item when walkRub is on: a Ring of dueling (8), else a Games necklace (8), whichever the
// mule publishes; a second small order with the notes and coins already in the pack. It returns the item the pack holds.
func (w *carrierWorld) askRing(post tile.Tile) string {
	ctx, d, set := w.ctx, w.d, w.set
	if !set.WalkRub {
		return ""
	}
	if rub := carrierRubItem(d); rub != "" {
		return rub
	}
	stock := muleBankCount(ctx, set.Mule)
	for _, name := range []string{"Ring of dueling(8)", "Games necklace(8)"} {
		if stock != nil {
			if n, _ := stock(name); n <= 0 {
				continue
			}
		}
		w.logf("asking %s for a %s to rub on the road", set.Mule, name)
		scarabKitBundle(d, ctx.Bus, ctx.Username, set.Mule, []fleetplan.ItemCount{{Item: name, Count: 1}}, fleetrunner.KitBundleOptions{
			Timeout: donorrunner.DefaultKitTimeout, KeepNotes: true, PerOrder: 1, FallbackTimeout: 90 * time.Second, World: ctx.CurrentWorld,
		})
		if rub := carrierRubItem(d); rub != "" {
			return rub
		}
	}
	return ""
}

func declinedWord(m map[string]string) string {
	var parts []string
	for k, why := range m {
		parts = append(parts, k+": "+why)
	}
	return strings.Join(parts, "; ")
}

func gateWord(g runecraftplan.GateInfo) string {
	if !g.Heard {
		return "not heard"
	}
	return g.State
}

// cross is the walk from the mainland to Jiminua's store: the road to Ardougne's pier, Captain Barnaby's ship (30 coins) and the jungle
// road around the spiders.
func (w *carrierWorld) cross() runeStepResult {
	if c := runecraftrunner.Coins(w.d); c < carrierFareFloor {
		return runeFailf("%d coins cannot pay the ship (it is 30 each way)", c)
	}
	started := time.Now()
	if !w.walk(runecraftplan.StoreStand, 2, carrierCrossWalk) {
		return runeFailf("the walk to Jiminua's store did not arrive (at %s)", fleetrunner.HereIs(w.d))
	}
	w.logf("crossed to Jiminua's store (%s) in %s", fleetrunner.HereIs(w.d), time.Since(started).Round(time.Second))
	return runeOK()
}

func (w *carrierWorld) walkToStore() runeStepResult {
	if !w.walk(runecraftplan.StoreStand, 2, carrierLegWalk) {
		return runeFailf("the walk to Jiminua's store did not arrive (at %s)", fleetrunner.HereIs(w.d))
	}
	return runeOK()
}

func (w *carrierWorld) shelf() runeStepResult {
	// A crafter holding a stack of bows to alch (or a random event's fruit) takes 24 essence a trade, not 25: the load is what he says he can
	// take. A pack already holding more (loaded before his message changed) is not sold back: the trade offers his room and the rest
	// counts toward the next load (PlanShelf buys Want less what is held).
	res := carrierShelf(w.d, runecraftrunner.ShelfOptions{Floor: w.set.ShelfFloor, BuyOnly: w.buyOnly, Want: w.gate().ShelfWant()})
	w.paid += res.Cost
	w.earned += res.Income
	w.logf("%s", res.Line())
	if res.OutOfSpace && !w.buyOnly {
		w.buyOnly = true
		w.logf("WARNING: Jiminua's shop has no room for the notes (\"The shop has run out of space\"): buying from her shelf only from here on")
	}
	if res.UnnotedAt >= runecraftplan.MinLoad {
		if res.Reason != "" {
			w.logf("WARNING: the shelf visit fell short but the pack holds a load: %s", res.Reason)
		}
		return runeOK()
	}
	return runeFail(firstNonEmpty(res.Reason, fmt.Sprintf("the pack holds %d essence after the shelf", res.UnnotedAt)))
}

// walkToGate walks to the tile beside the crafter's stand (runecraftplan.GateWaitTile): orthogonally next to it, so that the trade the
// engine runs from there needs nobody to walk to anybody, and the crafter is never pulled off his stand by a carrier waiting
// somewhere else. A tile the walk cannot take it to (an NPC stands on it: six monkeys wander the ruins) is not worth a long wait: it
// goes to the gate as before, anywhere within three tiles of the portal's exit.
func (w *carrierWorld) walkToGate() runeStepResult {
	if w.walk(runecraftplan.GateWaitTile, 0, carrierGateWalk) {
		return runeOK()
	}
	if runecraftplan.PlaceOf(runecraftrunner.Here(w.d)) != runecraftplan.PlaceGate {
		if !w.walk(runecraftplan.Nature.Exit, 3, carrierLegWalk) {
			return runeFailf("the walk to the gate did not arrive (at %s)", fleetrunner.HereIs(w.d))
		}
	}
	w.sayEvery("waittile", 10*time.Minute, "the walk to the wait tile %v beside the crafter's stand did not take the carrier onto it (at %s): trading from here", runecraftplan.GateWaitTile, fleetrunner.HereIs(w.d))
	return runeOK()
}

// trade asks the crafter for a trade and carries it through. A crafter out of view (inside the temple, or away) and a screen that
// never opened are waits, not failures: the gate's own state and carrierWait decide when the carrier gives up (runecraftplan.GateWaitMax).
func (w *carrierWorld) trade() runeStepResult {
	masterDismissChat(w.ctx)
	held := runecraftrunner.Essence(w.d)
	// The crafter's room is read when the offer is made (his gate message, which he republishes the moment it changes): a carrier holding
	// more than he takes offers what he takes and keeps the rest for its next load, instead of an Offer-All he declines.
	res := carrierTrade(w.d, runecraftrunner.OfferOptions{Crafter: w.set.Crafter, Mode: runecraftrunner.OfferEssence, Timeout: carrierTradeWait, Absent: carrierTradeAbsent,
		Stand: runecraftplan.GateStandTile, Cap: func() int { return w.gate().Cap }})
	switch {
	case res.Closed:
		// A screen that closed before either side accepted: nothing crossed and nobody did anything wrong (a request or a walk from
		// either side closes it, see runecraftrunner's file comment). Ask again at once: no failure, no strike, no pause. (It used
		// to be a failure, and three in a row ended the shift.) A run of them is not a passing race, though: carrierClosesInARow of them
		// without a trade in between is a failure like any other.
		w.tradeCloses++
		w.closesInARow++
		if w.closesInARow >= carrierClosesInARow {
			n := w.closesInARow
			w.closesInARow = 0
			w.tradeFails++
			return runeFailf("the trade screen closed before it was accepted %d times in a row: %s", n, strings.TrimSpace(res.Reason))
		}
		w.sayEvery("closed", 30*time.Second, "WARNING: the trade screen with %s closed before it was accepted (%d so far this shift): %s; asking again", w.set.Crafter, w.tradeCloses, strings.TrimSpace(res.Reason))
		return runeOK()
	case res.Done:
		now := time.Now()
		w.trips++
		w.essence += res.Moved
		w.tradeFails, w.tradeTimeouts, w.silentTimeouts, w.closesInARow, w.capDeclines = 0, 0, 0, 0, 0
		w.waitSince = time.Time{}
		if !w.lastTradeAt.IsZero() {
			w.cycleSum += now.Sub(w.lastTradeAt)
			w.cycles++
		}
		w.lastTradeAt = now
		w.logf("trip %d: %s", w.trips, res.Describe(runecraftrunner.OfferEssence, w.set.Crafter))
		w.paceLine()
		return runeOK()
	case res.NotHere:
		w.sayEvery("nothere", time.Minute, "%s is not in view at the gate (inside the temple, or away): waiting with %d essence", w.set.Crafter, held)
		return runeOK()
	case res.NoScreen && res.Silent:
		// Requests went out and the game said nothing of any: the crafter is not being reached (a wall between the two of us, the engine
		// walking this carrier into it) or not hearing. Not "busy" and not "a queue": say so, once a minute.
		w.tradeTimeouts++
		w.silentTimeouts++
		w.sayEvery("deaf", time.Minute, "WARNING: crafter deaf: %s (%d silent attempt(s) this shift, %d move(s) tried); asking again", res.Reason, w.silentTimeouts, res.Moves)
		return runeOK()
	case res.NoScreen:
		w.tradeTimeouts++
		w.sayEvery("noscreen", time.Minute, "no trade screen with %s after %s and %d request(s) (he is busy or the queue is long); asking again", w.set.Crafter, res.Took.Round(time.Second), res.Requests)
		return runeOK()
	}
	// The crafter declined the load: he had no room for it. When his message now says he takes less than the carrier held, that is a
	// capacity change (a stack of bows, a random event's fruit) the next offer follows, not a trade that failed: no strike. A decline that
	// the message does not explain, or one that repeats carrierCapDeclines times, is a failure like any other.
	if res.Declined && w.crafterTakesLess(held) && w.capDeclines < carrierCapDeclines {
		w.capDeclines++
		g := w.gate()
		w.sayEvery("capdecline", time.Minute, "%s declined %d essence: he takes %d a trade now (his gate message); offering that and keeping the rest (%d decline(s) so far this trade)", w.set.Crafter, held, g.Cap, w.capDeclines)
		return runeOK()
	}
	w.tradeFails++
	w.logf("WARNING: %s", res.Describe(runecraftrunner.OfferEssence, w.set.Crafter))
	return runeFail(strings.TrimSpace(res.Reason))
}

// crafterTakesLess says the crafter's gate message now names a trade smaller than the load the carrier holds.
func (w *carrierWorld) crafterTakesLess(held int) bool {
	g := w.gate()
	return g.Heard && g.Cap >= runecraftplan.MinLoad && g.Cap < held
}

// paceLine is the line every carrierTradeLine trips: the essence an hour, the measured cycle and the shelf's cost.
func (w *carrierWorld) paceLine() {
	if w.trips%carrierTradeLine != 0 {
		return
	}
	wall := time.Since(w.started)
	if wall <= 0 {
		return
	}
	cycle := "?"
	if w.cycles > 0 {
		cycle = (w.cycleSum / time.Duration(w.cycles)).Round(time.Second).String()
	}
	w.logf("pace: %d trip(s), %s essence to %q in %s: %d essence/h; trip cycle %s; the shelf cost %d gp and paid %d gp (%d gp a trip); %d note(s) left; %d coin(s) left; %d screen(s) closed before an accept",
		w.trips, runecraftplan.Thousands(w.essence), w.set.Crafter, wall.Round(time.Second), perHourOf(w.essence, wall), cycle, w.paid, w.earned, (w.paid-w.earned)/w.trips,
		runecraftrunner.Notes(w.d), runecraftrunner.Coins(w.d), w.tradeCloses)
}

func (w *carrierWorld) waitAtGate() runeStepResult {
	w.sayEvery("wait", 2*time.Minute, "waiting at the gate with %d essence: the crafter is away (%s)", runecraftrunner.Essence(w.d), gateWord(w.gate()))
	if !w.ctx.Delay(carrierWaitStep) {
		return runeStopped()
	}
	return runeOK()
}

// courier takes the Natures the crafter holds beyond his keep, to carry them home with the leftovers: his own trip to the mule is
// 7 to 9 minutes, the carrier's is on the way. It is tried once a shift.
func (w *carrierWorld) courier() runeStepResult {
	if runecraftplan.PlaceOf(runecraftrunner.Here(w.d)) != runecraftplan.PlaceGate {
		if !w.walk(runecraftplan.GateWaitTile, 0, carrierLegWalk) {
			return runeFailf("the walk to the gate did not arrive (at %s)", fleetrunner.HereIs(w.d))
		}
		return runeOK()
	}
	masterDismissChat(w.ctx)
	res := carrierTrade(w.d, runecraftrunner.OfferOptions{Crafter: w.set.Crafter, Mode: runecraftrunner.TakeNatures, Timeout: carrierCourierWait, Absent: carrierTradeAbsent, Stand: runecraftplan.GateStandTile})
	w.courierTried = true
	if res.Done {
		w.natures += res.Moved
		w.logf("COURIER: %s", res.Describe(runecraftrunner.TakeNatures, w.set.Crafter))
		return runeOK()
	}
	w.logf("courier trade did not happen: %s (carrying on home without Natures)", res.Describe(runecraftrunner.TakeNatures, w.set.Crafter))
	return runeOK()
}

func (w *carrierWorld) goHome() runeStepResult {
	dest := w.muleStand()
	started := time.Now()
	if !w.walk(dest, 3, carrierHomeWalk) {
		return runeFailf("the walk home to the mule's post at %d,%d did not arrive (at %s)", dest.X, dest.Z, fleetrunner.HereIs(w.d))
	}
	w.logf("home at the mule's post (%s) after %s: %d notes, %d essence, %d Nature runes, %d coins in the pack", fleetrunner.HereIs(w.d),
		time.Since(started).Round(time.Second), runecraftrunner.Notes(w.d), runecraftrunner.Essence(w.d), runecraftrunner.Natures(w.d), runecraftrunner.Coins(w.d))
	return runeOK()
}

// carrierDeliverItems are what crosses to the mule at the end of a shift: the leftover essence (notes, or a load that was never
// traded) and the Natures a courier carried. The coins and the ring stay with the account.
var carrierDeliverItems = []string{runecraftplan.EssenceName, "Nature rune"}

// keepShare banks the Nature runes the carrier keeps out of a courier haul (carrierKeepNatures) so that only the rest goes to the mule:
// the haul and the carrier's own stock are one pile, and what is handed over is the pile beyond the keep. a user request: "keep
// some instead of trading all then asking back" - a carrier that gave the mule every Nature it brought and then asked it for a walk-alch
// kit made the mule's trip twice. It reports how many it kept in the bank and why it could not (the Natures then all cross).
func (w *carrierWorld) keepShare(post tile.Tile) (kept int, why string) {
	d := w.d
	keep, have := w.set.KeepNatures, runecraftrunner.Natures(d)
	if keep <= 0 || have <= 0 {
		return 0, ""
	}
	bank, ok := mastercraftrunner.OpenBank(d, post)
	if !ok || bank == nil {
		return 0, "the bank at the mule's post would not open"
	}
	defer func() { _ = bank.Close() }()
	wt := thieverrunner.Waiter(fleetrunner.Thiev(d))
	isNature := func(it gamestate.SlotItem) bool { return it.ID == runecraftrunner.NatureID }
	if err := bank.DepositAllMatching(isNature, wt); err != nil {
		return 0, "banking the Natures: " + err.Error()
	}
	total := runecraftrunner.NatureCount(bank.Items())
	give := min(have, max(0, total-keep))
	if give > 0 {
		if err := bank.WithdrawX(runecraftrunner.NatureID, int32(give), wt); err != nil {
			return 0, fmt.Sprintf("drawing the %d Natures that cross: %v", give, err)
		}
	}
	return min(total, keep), ""
}

// noteLoose turns the unnoted essence a shift ends with (a load the gate never took: the gate closed, or the trades failed) into one
// stack of notes, so that it can cross to the mule. Twenty-five single items cannot go on the mule's trade screen (masterDeliver: "a
// stack too big for the mule's free slots"; live 16 Oct 2026 two shifts ended on "deliver failed 3 times in a row" holding 25 essence),
// and the bank at the mule's post gives an item back as a note when it is asked to. Nothing is lost on the way: the essence is in the
// bank before it is drawn. It reports the notes the pack gained and why it could not.
func (w *carrierWorld) noteLoose(post tile.Tile) (noted int, why string) {
	d := w.d
	have := runecraftrunner.Essence(d)
	if have <= 0 {
		return 0, ""
	}
	bank, ok := mastercraftrunner.OpenBank(d, post)
	if !ok || bank == nil {
		return 0, "the bank at the mule's post would not open"
	}
	defer func() { _ = bank.Close() }()
	wt := thieverrunner.Waiter(fleetrunner.Thiev(d))
	isEssence := func(it gamestate.SlotItem) bool { return int(it.ID) == runecraftplan.EssenceID }
	if err := bank.DepositAllMatching(isEssence, wt); err != nil {
		return 0, "banking the loose essence: " + err.Error()
	}
	before := runecraftrunner.Notes(d)
	if err := bank.SetNoteMode(true, wt); err != nil {
		return 0, "note mode: " + err.Error()
	}
	defer func() { _ = bank.SetNoteMode(false, wt) }()
	if err := bank.WithdrawXAs(int32(runecraftplan.EssenceID), int32(have), int32(runecraftplan.NotedEssenceID), wt); err != nil {
		return 0, fmt.Sprintf("drawing %d notes: %v", have, err)
	}
	return runecraftrunner.Notes(d) - before, ""
}

// deliver hands the leftovers to the mule at its post.
func (w *carrierWorld) deliver() runeStepResult {
	ctx, d := w.ctx, w.d
	p := scarabMulePresenceOf(ctx, w.set.Mule)
	if ctx.Bus == nil || !p.Heard || !p.SameWorld {
		return runeFail("the mule is not here to take the leftovers: " + p.Why)
	}
	post := w.muleStand()
	if loose := runecraftrunner.Essence(d); loose > 0 {
		if n, why := w.noteLoose(post); why != "" {
			w.logf("WARNING: could not turn the %d loose essence into notes (%s): they are offered to the mule as they are", loose, why)
		} else {
			w.logf("the %d loose essence the shift ended with went through the bank as %d note(s), one stack for the mule", loose, n)
		}
	}
	if w.set.KeepNatures > 0 && runecraftrunner.Natures(d) > 0 {
		if kept, why := w.keepShare(post); why != "" {
			w.logf("keep: could not bank the share of Natures (%s): all of them cross to the mule", why)
		} else {
			w.logf("keep: %s Nature runes kept in the bank for this account's own alchemy (carrierKeepNatures %s), %s cross to the mule", runecraftplan.Thousands(kept),
				runecraftplan.Thousands(w.set.KeepNatures), runecraftplan.Thousands(runecraftrunner.Natures(d)))
			w.kept += kept
		}
		if runecraftrunner.Notes(d)+runecraftrunner.Essence(d)+runecraftrunner.Natures(d) == 0 {
			// The whole haul was the share: there is nothing to trade the mule.
			return runeOK()
		}
	}
	before := map[string]int{runecraftplan.EssenceName: runecraftrunner.Notes(d) + runecraftrunner.Essence(d), "Nature rune": runecraftrunner.Natures(d)}
	slots := 0
	if runecraftrunner.Notes(d) > 0 {
		slots++
	}
	if runecraftrunner.Essence(d) > 0 {
		slots++
	}
	if runecraftrunner.Natures(d) > 0 {
		slots++
	}
	rd := fleetrunner.MuleDeps(d, ctx.Bus, ctx.Username)
	WireDonorWorlds(ctx, rd)
	rd.HomeWorld = 0 // gated on the mule being on this world: no hop
	r := masterDeliver(d, rd, w.set.Mule, post, 1, slots, carrierDeliverItems)
	if r.Stopped && !r.Hopping {
		return runeStopped()
	}
	left := map[string]int{runecraftplan.EssenceName: runecraftrunner.Notes(d) + runecraftrunner.Essence(d), "Nature rune": runecraftrunner.Natures(d)}
	if w.delivered == nil {
		w.delivered = map[string]int{}
	}
	for item, n := range before {
		if gone := n - left[item]; gone > 0 {
			w.delivered[item] += gone
		}
	}
	w.logf("HANDOVER leftovers mule=%s had=%v left=%v traded=%v reason=%q", w.set.Mule, before, left, r.Traded, r.Reason)
	if left[runecraftplan.EssenceName] == 0 && left["Nature rune"] == 0 {
		return runeOK()
	}
	return runeFailf("the mule took no leftovers (%d essence, %d Nature runes still held): %s", left[runecraftplan.EssenceName], left["Nature rune"], firstNonEmpty(r.Reason, "no trade"))
}

// ---- the shift ----

// liveShift is one shift over the live session: see the file comment. resumed says a kit was found in the pack (a restart, a shift
// that gave up on the road), so the mule is not asked again at the end.
func (b *runeCarrierBot) liveShift(ctx *Context, why string, resumed bool) carrierOutcome {
	d := b.deps(ctx)
	w := &carrierWorld{b: b, ctx: ctx, d: d, set: b.set, started: time.Now(), said: map[string]time.Time{}, delivered: map[string]int{}, kitAsked: resumed}
	if p := scarabMulePresenceOf(ctx, b.set.Mule); p.Heard && p.SameWorld {
		w.post, w.postKnown = p.Post, true
	}
	w.notes0, w.coins0 = runecraftrunner.Notes(d), runecraftrunner.Coins(d)
	w.heartbeat(mulebus.CarrierTransit)
	defer w.startBeat()()
	res := runCarrierShift(w, func(f string, a ...any) { w.logf(f, a...) }, carrierMaxSteps)
	if res.Stopped {
		return carrierOutcome{Stopped: true, Trips: w.trips, Essence: w.essence, Natures: w.natures, Paid: w.paid, Earned: w.earned, Delivered: w.delivered, Why: "stopped"}
	}
	out := carrierOutcome{Trips: w.trips, Essence: w.essence, Natures: w.natures, Paid: w.paid, Earned: w.earned, Delivered: w.delivered}
	switch {
	case res.Died || w.died:
		out.Died = true
		out.Why = "the account died: " + firstNonEmpty(w.over, res.Reason)
	case res.GaveUp:
		out.Failed = true
		out.Why = "gave up: " + res.Reason + "; the shift ended because: " + w.over
	case w.over != "":
		// The shift ended early (a stage kept failing) and the carrier was sent home; a shift that made trips is not a failure.
		out.Failed = w.trips == 0
		out.Why = "ended early: " + w.over
	default:
		out.Why = res.Reason
	}
	// A shift that made no trip is a failed one whatever ended it (the kit ask that gave nothing ends it "done" at once), and backs the
	// carrier off; the reason it last failed is said.
	if out.Trips == 0 && !out.Died {
		out.Failed = true
		if w.lastFail != "" {
			out.Why += "; last failure: " + w.lastFail
		}
	}
	// A closing message so the crafter's carrier count drops at once.
	w.heartbeat(mulebus.CarrierHome)
	return out
}
