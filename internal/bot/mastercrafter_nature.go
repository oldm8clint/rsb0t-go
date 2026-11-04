package bot

// MasterCrafter's Nature gate round (runecraft P3, docs/runecraft_p3_2026-10-14.md). a user request: "runecraft with runners
// in the fleet, maxing nature runes; the main account (MasterCrafter, the main account) gets the Runecraft levels and sits at the altar".
// Runecrafting 44 reached, the main account stands OUTSIDE the Nature ruins on Karamja and crafts what RuneCarrier accounts bring him:
//
//	at the post   (a new round) bank the pack but the Natures held, draw the Nature talisman (ask the mule for one if the bank has
//	              none) and the fare home (100 coins): the gate pack is the talisman, the Nature stack and the fare, three slots
//	the road      walk to the ruins by the ship from Ardougne and the jungle road, planned around the jungle spiders (the walker's
//	              own hostile avoidance knows two species and neither is a spider: runecraftplan.AvoidBoxes)
//	the gate      stand on the tile beside the ruins nearest the portal's exit, publish the gate on the bus (opening, open) and answer
//	              the carriers' trade requests (runecraftrunner.GateStep): 25 unnoted essence a trade, accepted once the offer has
//	              settled; hand a courier the Natures beyond craftRuneKeep when one asks
//	the cycle     the talisman on the ruins as soon as the trade closes (the stand tile is one the engine accepts), the altar's
//	              Craft-rune, the portal's click timed to the end of the craft's four-tick hold, back to the stand tile
//	the end       time (craftRuneMaxHours), no carrier for a quarter hour, or no essence for half an hour: leave by the portal,
//	              walk home, hand the mule the Natures beyond craftRuneKeep, bank the pack
//
// With craftRuneCampaign (P4, mastercrafter_campaign.go) the same round is a campaign's: a campaign starts by itself when the mule
// is out of Natures, tops its essence up first (the mule's whole coin stack spent at the Seers stalls) when the mule is short, ends
// at the target or when the mule holds enough, and delivers its own surplus when no courier is coming; the keep is drawn into the
// pack at the start and counted with the bank at the end, so it is kept once however many rounds a campaign takes.
//
// The gate's state (opening, open, away, closed, and the Natures a courier may take) is put on the bus every five seconds by a
// goroutine for the whole round (startBeat), not only between steps: the walk to the gate is ten minutes of one step and a carrier
// believes a gate message for fifteen seconds.
//
// Everything is decided afresh each step from the live state (runecraftplan.DecideGateStage), so a restart in the temple or at the
// gate carries on (mastercrafter_resume.go). The loop is runGateLoop over a natureSteps world so the whole round is tested against a
// simulated one (mastercrafter_nature_test.go); the live world is masterNatureWorld, which is a masterRuneWorld (the ruins, the altar
// and the portal are the levelling round's code, unchanged) plus the gate.

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

const (
	// natureStepFails is how many times in a row one stage may fail before the round ends as a passing failure.
	natureStepFails = 3
	// natureStall is how many times in a row the same stage may "succeed" with nothing changed before it counts as a failure
	// (the gate's wait for a carrier is not one).
	natureStall = 8
	// natureMaxSteps bounds one round's steps whatever happens (a gate round of eight hours is a few thousand).
	natureMaxSteps = 100000
	// natureLagFails is how many failures in a row of one stage a lagging view may excuse before they count after all (the stage
	// then ends the round like any other that never succeeds, a few minutes later).
	natureLagFails = masterRuneLagFails
	// natureTradeFails is how many trades in a row may fail (refused by the engine, closed under us) before the stage fails.
	natureTradeFails = 6
	// natureLagTrades is how many trades in a row a lagging view may excuse before they count after all (CR-6 of
	// docs/xp_rate_plan_2026-10-19.md). 18 Oct 2026 19:46-19:52 ten trades failed in a row while the view went silent every few seconds
	// (the confirm screen "does not match" the offer, the trade closed with nothing gained), the stage failed three times, the round
	// ended and the gate stood closed for 40 minutes. A trade that failed with the view lagging says nothing about the gate: it is
	// tried again from a current view and not counted, up to this many; a view that never recovers still ends the round.
	natureLagTrades = 30
	// natureTradeLine is how many trades a rate line covers.
	natureTradeLine = 10
	// natureHome bounds the walk home from the island: the ship, the jungle road and about 450 tiles.
	natureHome = 20 * time.Minute
)

// natureWait is how long one GateStep waits for a request before the loop reads the world again (a variable so a test can shorten it).
var natureWait = 2 * time.Second

// gateBeatEvery is how often the gate's last state is put on the bus again while a step is busy (a variable so a test can shorten it).
var gateBeatEvery = mulebus.GateEvery

// gateBeat is the gate's last published state, kept on the bus by a goroutine for the length of the round: the walk to the gate is
// ten minutes of ONE step and a carrier believes a gate message for only mulebus.GateTTL (15 s), so without it every carrier that
// started a shift on "opening" would hear a closed gate before it had the kit.
type gateBeat struct {
	mu    sync.Mutex
	state mulebus.GateState
	eta   time.Duration
	set   bool
}

func (g *gateBeat) put(state mulebus.GateState, eta time.Duration) {
	g.mu.Lock()
	g.state, g.eta, g.set = state, eta, true
	g.mu.Unlock()
}

func (g *gateBeat) get() (mulebus.GateState, time.Duration, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state, g.eta, g.set
}

// masterGateStep is runecraftrunner.GateStep, the crafter's half of a trade, unless a test replaced it.
var masterGateStep = runecraftrunner.GateStep

// natureSteps is the gate round's world: what it reads and what each stage does to it.
type natureSteps interface {
	// Read is the crafter's state for runecraftplan.DecideGateStage; Fingerprint is a string that changes when anything a stage
	// could have changed does.
	Read() runecraftplan.GateSnapshot
	Fingerprint() string
	// Gate stands at the gate and answers the carriers for a moment; ToRuins walks to the ruins and uses the talisman; Craft binds
	// the essence; PortalOut leaves the temple.
	Gate() runeStepResult
	ToRuins(runecraftplan.Step) runeStepResult
	Craft(runecraftplan.Step) runeStepResult
	PortalOut(runecraftplan.Step) runeStepResult
	// Prepare runs before every step (the dead flag, a random event, the heartbeat); it reports false when the bot was stopped.
	Prepare() bool
	Stopped() bool
	Pause(time.Duration)
}

// natureLoopResult is how the loop ended.
type natureLoopResult struct {
	Reason string
	// Done: the end rule held and the pack is empty. Stuck: nothing to work with. Transient: a stage failed for a passing reason.
	Done, Stuck, Transient, Stopped bool
	Steps                           int
	Stages                          map[runecraftplan.GateStage]int
}

// runGateLoop decides a stage from the world's state and does it, until the round ends, nothing is left to work with, a stage fails
// natureStepFails times in a row, or the bot stops.
func runGateLoop(w natureSteps, logf func(format string, args ...any), maxSteps int) (res natureLoopResult) {
	res.Stages = map[runecraftplan.GateStage]int{}
	var last runecraftplan.GateStage
	lastSig := ""
	fails, same, lagFails := 0, 0, 0
	nature := runecraftplan.Nature
	for res.Steps < maxSteps {
		if w.Stopped() || !w.Prepare() {
			res.Stopped, res.Reason = true, "stopped"
			return res
		}
		step := runecraftplan.DecideGateStage(w.Read())
		res.Steps++
		res.Stages[step.Stage]++
		if step.Stage != last {
			if step.Stage != runecraftplan.GateTrade || last == "" {
				logf("gate stage %s: %s", step.Stage, step.Why)
			}
			fails, same, lagFails = 0, 0, 0
		}
		sig := w.Fingerprint()
		if step.Stage == last && sig == lastSig && step.Stage != runecraftplan.GateTrade {
			same++
		} else {
			same = 0
		}
		if same >= natureStall {
			res.Transient, res.Reason = true, fmt.Sprintf("%s ran %d times in a row without anything changing", step.Stage, same+1)
			return res
		}
		last, lastSig = step.Stage, sig
		var r runeStepResult
		switch step.Stage {
		case runecraftplan.GateDone:
			res.Done, res.Reason = true, step.Why
			return res
		case runecraftplan.GateStuck:
			res.Stuck, res.Reason = true, step.Why
			return res
		case runecraftplan.GateTrade:
			r = w.Gate()
		case runecraftplan.GateWalkToRuins:
			r = w.ToRuins(runecraftplan.Step{Stage: runecraftplan.StageWalkToRuins, Altar: nature, Why: step.Why})
		case runecraftplan.GateUseTalisman:
			r = w.ToRuins(runecraftplan.Step{Stage: runecraftplan.StageUseTalisman, Altar: nature, Why: step.Why})
		case runecraftplan.GateCraft:
			r = w.Craft(runecraftplan.Step{Stage: runecraftplan.StageCraft, Altar: nature, Why: step.Why})
		case runecraftplan.GatePortalOut:
			r = w.PortalOut(runecraftplan.Step{Stage: runecraftplan.StagePortalOut, Altar: nature, Why: step.Why})
		default:
			res.Transient, res.Reason = true, "no handler for stage "+string(step.Stage)
			return res
		}
		if r.Stopped {
			res.Stopped, res.Reason = true, "stopped"
			return res
		}
		if r.OK {
			fails, lagFails = 0, 0
			continue
		}
		if r.Lag && lagFails < natureLagFails {
			// A failure out of a view that lags is the connection's: the stage is tried again from a current view and the failure does
			// not count toward ending the round. 18 Oct 2026 05:30: three crafts in a row were not seen in six seconds each, the round
			// ended, the crafter walked to the mule's post and the gate stood closed for 40 minutes.
			lagFails++
			same = 0
			logf("gate %s failed in a lagging view (%d of %d excused, not counted toward ending the round): %s", step.Stage, lagFails, natureLagFails, r.Why)
			w.Pause(3 * time.Second)
			continue
		}
		fails++
		logf("WARNING: gate %s failed (%d in a row): %s", step.Stage, fails, r.Why)
		if fails >= natureStepFails {
			res.Transient, res.Reason = true, fmt.Sprintf("%s failed %d times in a row: %s", step.Stage, fails, r.Why)
			return res
		}
		w.Pause(3 * time.Second)
	}
	res.Transient, res.Reason = true, fmt.Sprintf("%d steps without finishing", maxSteps)
	return res
}

// ---- the live world ----

type masterNatureWorld struct {
	*masterRuneWorld
	watch *runecraftrunner.GateWatcher

	// stand is the tile beside the ruins the talisman is accepted from that is nearest the portal's exit.
	stand     tile.Tile
	haveStand bool

	// The heartbeat: the state last published, when, and the trade capacity it said (a change of it is published at once).
	pubState mulebus.GateState
	pubAt    time.Time
	pubCap   int
	// stray is the pack items the gate has no use for (mastercrafter_gatestray.go).
	stray gateStrayState
	// carriers is the bus's carriers for this crafter, read at carriersAt; carrierSeen the last moment one was.
	carriers    []mulebus.CarrierView
	carriersAt  time.Time
	carrierSeen time.Time
	// lastTrade is when essence last arrived (zero: none this round).
	lastTrade time.Time
	// strangerSaid rate-limits the log line for a request from a stranger.
	strangerSaid map[string]time.Time
	// standClick is when the walk onto the stand tile was last clicked, standClickAt the tile it was clicked from and standRouted whether
	// it carried the pack's route (mastercrafter_gatedeaf.go); ledgerDirty says the ledger has a trade it has not written.
	standClick   time.Time
	standClickAt tile.Tile
	standRouted  bool
	ledgerDirty  bool
	// deaf is the gate's watch on its own silence (mastercrafter_gatedeaf.go).
	deaf gateDeafState
	// alch is the round's alchemy (mastercrafter_gatealch.go); lastPartner and lastPartnerAt the carrier that last traded, which is on its
	// way to the store and not asking (the alchemy's clearance rule skips it).
	alch          gateAlchState
	lastPartner   string
	lastPartnerAt time.Time
	// legCastSaid are the reasons a leg was not cast on that the log has said (mastercrafter_legcast.go).
	legCastSaid map[string]bool

	// The round's figures.
	closedTrades                        int
	trades, courierTrades               int
	essence, natures, xp, courierNature int
	failedTrades                        int
	lagTrades                           int // the trades in a row that failed in a lagging view and were not counted in failedTrades
	cycleSum                            time.Duration
	cycles                              int
	lastCycleAt                         time.Time
	died                                bool
	// beat is the state the heartbeat goroutine repeats (startBeat).
	beat gateBeat

	// camp is the campaign's part of the round (mastercrafter_campaign.go); handed is what the main account handed the mule at the end of the
	// round and handOverFailed whether a hand-over was due and did not happen.
	camp                          natureCampaignState
	handed                        int
	handOverFailed, handOverTried bool
}

func (w *masterNatureWorld) logf(format string, args ...any) {
	w.b.logf(w.ctx, "runecraft: gate: "+format, args...)
}

// carrierViews reads the bus's carriers for this crafter, at most every two seconds.
func (w *masterNatureWorld) carrierViews() []mulebus.CarrierView {
	now := time.Now()
	if now.Sub(w.carriersAt) < 2*time.Second && !w.carriersAt.IsZero() {
		return w.carriers
	}
	w.carriersAt = now
	w.carriers = nil
	if w.ctx.Bus != nil && w.ctx.Username != "" {
		w.carriers = mulebus.ReadCarriers(w.ctx.Bus, w.ctx.Username, now, 0)
	}
	if len(w.carriers) > 0 {
		w.carrierSeen = now
	}
	return w.carriers
}

func (w *masterNatureWorld) carrierNames() []string {
	var out []string
	for _, c := range w.carrierViews() {
		out = append(out, c.Name)
	}
	return out
}

func (w *masterNatureWorld) allowed(name string) bool {
	return mastercraftplan.GateAllowed(w.set.Runners, w.carrierNames(), name)
}

// courierQty is the Natures the named partner may take now: its message says it is a courier and he holds beyond his keep.
func (w *masterNatureWorld) courierQty(name string) int {
	if !w.set.Courier {
		return 0
	}
	for _, c := range w.carrierViews() {
		if actions.SameUsername(c.Name, name) && c.Role == mulebus.CarrierCourier {
			return runecraftplan.CourierQuantity(runecraftrunner.Natures(w.d), w.set.Keep, w.set.CourierMax)
		}
	}
	return 0
}

// publish tells the carriers where the gate stands, at most once every mulebus.GateEvery unless the state changed.
func (w *masterNatureWorld) publish(state mulebus.GateState, eta time.Duration) {
	if w.ctx.Bus == nil || w.ctx.Username == "" {
		return
	}
	w.beat.put(state, eta)
	now := time.Now()
	capacity := runecraftrunner.TradeCapacity(w.d)
	if state == w.pubState && capacity == w.pubCap && now.Sub(w.pubAt) < mulebus.GateEvery {
		return
	}
	w.pubState, w.pubAt, w.pubCap = state, now, capacity
	surplus := 0
	if w.set.Courier && state != mulebus.GateClosed {
		surplus = runecraftplan.CourierQuantity(runecraftrunner.Natures(w.d), w.set.Keep, w.set.CourierMax)
	}
	w.ctx.Bus.Publish(mulebus.OnWorld(mulebus.GateMessage(w.ctx.Username, runecraftrunner.Here(w.d), state, eta, surplus).WithCapacity(capacity), w.ctx.World()))
}

// republish puts the gate's last state on the bus again NOW: the carriers read the room he has from that message and believe it until the
// next, so a change in it (a random event's fruit in the pack, the bows gone) must not wait for the next beat.
func (w *masterNatureWorld) republish() {
	if state, eta, ok := w.beat.get(); ok && state != mulebus.GateClosed {
		w.pubAt = time.Time{}
		w.publish(state, eta)
	}
}

// capacityChanged republishes the gate when the room he has for a trade is not the one the last message said.
func (w *masterNatureWorld) capacityChanged() {
	if _, _, ok := w.beat.get(); ok && runecraftrunner.TradeCapacity(w.d) != w.pubCap {
		w.republish()
	}
}

// startBeat repeats the gate's last state every gateBeatEvery until the returned stop is called, whatever the loop is doing (a walk,
// a bank visit, a long wait inside a step). A closed gate is announced by finishNature and not repeated.
func (w *masterNatureWorld) startBeat() (stop func()) {
	if w.ctx.Bus == nil || w.ctx.Username == "" {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(gateBeatEvery)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				state, eta, ok := w.beat.get()
				if !ok || state == mulebus.GateClosed {
					continue
				}
				surplus := 0
				if w.set.Courier {
					surplus = runecraftplan.CourierQuantity(runecraftrunner.Natures(w.d), w.set.Keep, w.set.CourierMax)
				}
				w.ctx.Bus.Publish(mulebus.OnWorld(mulebus.GateMessage(w.ctx.Username, runecraftrunner.Here(w.d), state, eta, surplus).WithCapacity(runecraftrunner.TradeCapacity(w.d)), w.ctx.World()))
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

// heartbeat publishes the state the crafter's place says: open at the gate and in the temple, opening on the road.
func (w *masterNatureWorld) heartbeat() {
	here := runecraftrunner.Here(w.d)
	if region, _ := runecraftplan.WhereIs(here); region == runecraftplan.RegionTemple || runecraftplan.NearGate(here) {
		w.publish(mulebus.GateOpen, 0)
		return
	}
	w.publish(mulebus.GateOpening, 6*time.Minute)
}

// Prepare is the start of every step: the death latch (a death ends the round: the Natures and the talisman are on the floor of
// the island and a respawn is in Lumbridge), a random event, the heartbeat.
func (w *masterNatureWorld) Prepare() bool {
	if w.ctx.Session.State().IsDead() && !w.died {
		w.died = true
		w.logf("WARNING: the account died at %s: the round ends", fleetrunner.HereIs(w.d))
	}
	ok := w.masterRuneWorld.Prepare()
	if ok && !w.died {
		w.heartbeat()
	}
	return ok
}

// endReason is why the round must end now, "" while it goes on.
func (w *masterNatureWorld) endReason() string {
	if w.died {
		return "the account died"
	}
	if w.camp.forceEnd != "" {
		return w.camp.forceEnd
	}
	// The trawler block comes first (a user request): the round leaves by the portal and the trawler round takes over.
	if w.b.set.Trawler && w.b.trawlerWaiting(w.ctx, time.Now()) {
		return "the two-hour trawler block is on: the gate yields to it"
	}
	if why := w.alchStuckWhy(); why != "" {
		return why
	}
	if !w.set.Manual && !w.set.Campaign {
		return mastercraftplan.KeyRuneManual + " and " + mastercraftplan.KeyRuneCampaign + " are off"
	}
	if w.set.Campaign {
		return w.campaignEnd()
	}
	carriers := w.carrierViews()
	end, why := runecraftplan.DecideGateEnd(runecraftplan.GateEndView{
		Now: time.Now(), Started: w.started, MaxRound: w.set.MaxHours, LastTrade: w.lastTrade, LastCarrier: w.carrierSeen, Carriers: len(carriers),
	})
	if end {
		return why
	}
	return ""
}

func (w *masterNatureWorld) Read() runecraftplan.GateSnapshot {
	d := w.d
	level, _ := runecraftrunner.Level(d)
	here := runecraftrunner.Here(d)
	region, temple := runecraftplan.WhereIs(here)
	s := runecraftplan.GateSnapshot{
		Level: level, Members: w.members, Where: region, Temple: temple.Key,
		PackEssence: runecraftrunner.Essence(d), Talisman: runecraftrunner.Talismans(d)[runecraftplan.KeyNature],
	}
	if region == runecraftplan.RegionOutside {
		if ruins, ok := runecraftrunner.FindRuins(d, runecraftplan.Nature); ok {
			s.AtRuins = runecraftrunner.Reached(d, ruins)
		}
	}
	s.End = w.endReason()
	return s
}

func (w *masterNatureWorld) Fingerprint() string {
	d := w.d
	level, xp := runecraftrunner.Level(d)
	return fmt.Sprintf("%v|%d|%d|%d|%d|%d|%d", runecraftrunner.Here(d), level, xp, runecraftrunner.Essence(d), runecraftrunner.Natures(d), w.trades, w.courierTrades)
}

// gateStand is the tile beside the ruins the talisman is accepted from that is nearest the portal's exit.
func (w *masterNatureWorld) gateStand() (tile.Tile, bool) {
	if w.haveStand {
		return w.stand, true
	}
	ruins, ok := runecraftrunner.FindRuins(w.d, runecraftplan.Nature)
	if !ok {
		return tile.Tile{}, false
	}
	stand, ok := runecraftrunner.GateStand(w.d, ruins)
	if !ok {
		return tile.Tile{}, false
	}
	w.stand, w.haveStand = stand, true
	w.logf("the gate stand is (%d,%d), %d tile(s) from the portal's exit (%d,%d)", stand.X, stand.Z, tile.Chebyshev(stand, runecraftplan.Nature.Exit),
		runecraftplan.Nature.Exit.X, runecraftplan.Nature.Exit.Z)
	return stand, true
}

// PortalOut leaves the temple and forgets every trade request read so far: a request line is only good for the gate it was sent at, and
// the crafter has been away (runecraftrunner.GateWatcher.Reset). The carriers ask again the moment he is back in view.
func (w *masterNatureWorld) PortalOut(st runecraftplan.Step) runeStepResult {
	r := w.masterRuneWorld.PortalOut(st)
	if r.OK {
		w.watch.Reset(w.d)
		if line := w.legs.finish(st.Altar); line != "" {
			w.logf("%s", line)
		}
		if line := w.legCastLine(); line != "" {
			w.logf("cast on leg: %s", line)
		}
	}
	return r
}

// standClickEvery is how soon the walk onto the stand tile is clicked again when the account is still off it: two ticks.
const standClickEvery = 1200 * time.Millisecond

// onStand says the account stands on the stand tile (true when there is no stand to stand on).
func (w *masterNatureWorld) onStand() bool {
	stand, ok := w.gateStand()
	return !ok || runecraftrunner.Here(w.d) == stand
}

// gateIdle is what the crafter does in the polls of a GateStep while no answer of his is out: it walks him onto the stand tile with ONE
// click (the walker's plan and poll would cost a tick or two the carrier is waiting through) and writes the ledger when no carrier is
// waiting. The click is the lone stand waypoint where the server's greedy walk of it arrives and the pack's route where it would park
// him against a pillar (runecraftrunner.ClickStandWalk; 18 Oct 2026, 56 minutes). It never runs while an answer is out or a screen is open: a walk packet, like a request, closes
// an open trade screen for both players (runecraftrunner's file comment). The walk is not blocking: GateStep goes on reading the
// carriers' requests meanwhile and answers once he is on the stand (runecraftplan.GateStandHold at most).
func (w *masterNatureWorld) gateIdle() {
	d := w.d
	if actions.NewTrade(d.Session).Active() {
		return
	}
	// A chat box left open (a level-up, a Genie lamp's "Your wish has been granted!") stops every trade screen from opening: live
	// 16 Oct 2026 the carriers sent 50 requests a minute for 15 minutes at a gate whose crafter answered nobody.
	if masterDismissChat(w.ctx) {
		return
	}
	if stand, ok := w.gateStand(); ok {
		if here := runecraftrunner.Here(d); here != stand && runecraftplan.NearGate(here) && w.standClickDue(here) {
			w.clickStand(here, stand)
		}
	}
	w.clearStray()
	w.gateAlch()
	w.capacityChanged()
	if w.watch.Pending() == 0 {
		w.flushLedger()
	}
}

// flushLedger writes the ledger when a trade has changed it. The write is a disk write: it is done in the gate's idle polls and at
// the end of a round, never between a trade and the talisman.
func (w *masterNatureWorld) flushLedger() {
	if w.ledgerDirty {
		w.ledgerDirty = false
		w.b.saveLedger(w.ctx)
	}
}

// Gate is the trading stage: stand on the stand tile and answer the carriers for natureWait. A gate that stayed deaf to the carriers it
// could see waiting, past its walks and its nudge (mastercrafter_gatedeaf.go), fails here until runGateLoop ends the round: the carriers
// are told the gate is closed and go home instead of asking a crafter who answers nobody.
func (w *masterNatureWorld) Gate() runeStepResult {
	if why := w.deaf.endWhy; why != "" {
		return runeFail(why)
	}
	d := w.d
	here := runecraftrunner.Here(d)
	if !runecraftplan.NearGate(here) {
		// Away from the gate: a respawn, a displaced account. Walk back once; a failure ends the round.
		stand, ok := w.gateStand()
		if !ok {
			stand = runecraftplan.Nature.Exit
		}
		w.logf("not at the gate (at %s): walking back", fleetrunner.HereIs(d))
		if !w.walk(stand, 0, natureHome) {
			return runeFailf("could not walk back to the gate from %s", fleetrunner.HereIs(d)).withLag(runecraftrunner.Shaky(d))
		}
		w.watch.Reset(d)
		return runeOK()
	}
	w.heartbeat() // the gate is open: tell the carriers before waiting for one
	res := masterGateStep(d, w.watch, runecraftrunner.GateOptions{
		Allowed: w.allowed, CourierQty: w.courierQty, Ending: func() bool { return w.endReason() != "" }, Wait: natureWait,
		OnStand: w.onStand, Idle: w.gateIdle, Hold: w.alchHold, Delayed: w.alchLocked, Together: w.gateTogether(),
	})
	w.togetherSeen(res)
	w.gateWatch(res)
	if why := w.deaf.endWhy; why != "" {
		return runeFail(why)
	}
	now := time.Now()
	for _, s := range res.Strange {
		if now.Sub(w.strangerSaid[strings.ToLower(s)]) > 5*time.Minute {
			w.strangerSaid[strings.ToLower(s)] = now
			w.logf("trade request from %s, who is not one of the carriers (craftRuneRunners %v, %d at work on the bus): ignored", s, w.set.Runners, len(w.carrierViews()))
		}
	}
	if res.Together.Sent && res.Kind != runecraftrunner.GateReceived {
		// The talisman went with the accept and the essence did not come (mastercrafter_together.go): a failed trade whatever the view did.
		return w.togetherLost(res)
	}
	l := &w.b.ledger.Rune.Gate
	switch res.Kind {
	case runecraftrunner.GateNothing:
		w.flushLedger()
		w.campaignTick()
		return runeOK()
	case runecraftrunner.GateClosed:
		// The screen closed before anybody accepted: nothing crossed and nobody failed. The carrier asks again.
		w.closedTrades++
		w.logf("trade screen with %s closed before it was accepted (%s); ask@%d answer@%d open@%d close@%d", res.Partner, strings.TrimSpace(res.Reason), res.Ticks.Ask, res.Ticks.Answer, res.Ticks.Open, res.Ticks.Close)
		return runeOK()
	case runecraftrunner.GateReceived:
		w.trades++
		w.essence += res.Essence
		w.failedTrades, w.lagTrades = 0, 0
		if !w.lastCycleAt.IsZero() {
			w.cycleSum += now.Sub(w.lastCycleAt)
			w.cycles++
		}
		w.lastCycleAt, w.lastTrade = now, now
		w.lastPartner, w.lastPartnerAt = res.Partner, now
		w.legs.arm(res.Ticks.Close)
		w.legs.noteOpen(res.Ticks.Open)
		w.legRecSet = [2]bool{} // a cycle that was cut short leaves no record to be printed with the next one's
		if res.Together.Sent && res.Together.Talisman != nil {
			w.legs.noteTogetherPending(res.Together.Talisman.Tick)
		}
		l.NoteTrade(res.Essence, now)
		w.noteCampaignTrade(res.Essence)
		w.ledgerDirty = true // written in the next idle poll, not between this trade and the talisman
		w.logf("trade %d from %s: %d essence in %s (pack %d essence; ticks ask %d answer %d open %d close %d%s)", w.trades, res.Partner, res.Essence, res.Took.Round(100*time.Millisecond), runecraftrunner.Essence(d),
			res.Ticks.Ask, res.Ticks.Answer, res.Ticks.Open, res.Ticks.Close, togetherWord(res))
		w.tradeLine()
		w.campaignTick()
		return runeOK()
	case runecraftrunner.GateGave:
		w.courierTrades++
		w.courierNature += res.Natures
		w.failedTrades, w.lagTrades = 0, 0
		l.NoteCourier(res.Natures)
		w.noteCampaignCourier(res.Natures, now)
		w.ledgerDirty = true
		w.logf("courier %s took %d Nature runes in %s (held %d, keep %d)", res.Partner, res.Natures, res.Took.Round(100*time.Millisecond), runecraftrunner.Natures(d), w.set.Keep)
		return runeOK()
	case runecraftrunner.GateRefused:
		w.logf("trade with %s declined: %s (the pack holds %d free slot(s); the gate says it takes %d a trade)", res.Partner, res.Reason, runecraftrunner.PackFree(d), runecraftrunner.TradeCapacity(d))
		// The carrier reads what he takes from his message: say it again now, so its next offer is the right one.
		w.republish()
		w.warnStrays()
		return runeOK()
	}
	if res.Lag && w.lagTrades < natureLagTrades {
		// A trade that failed in a view that lags is the connection's: it is not counted toward ending the round (CR-6).
		w.lagTrades++
		w.logf("trade with %s failed in a lagging view (%d of %d excused, not counted toward ending the round): %s", res.Partner, w.lagTrades, natureLagTrades, res.Reason)
		return runeOK()
	}
	w.failedTrades++
	w.logf("WARNING: trade with %s failed (%d in a row): %s", res.Partner, w.failedTrades, res.Reason)
	if w.failedTrades >= natureTradeFails {
		return runeFailf("%d trades in a row failed, the last with %s: %s", w.failedTrades, res.Partner, res.Reason)
	}
	return runeOK()
}

// tradeLine is the rate line every natureTradeLine trades: the essence and Natures an hour and the measured cycle.
func (w *masterNatureWorld) tradeLine() {
	if w.trades%natureTradeLine != 0 {
		return
	}
	wall := time.Since(w.started)
	if wall <= 0 {
		return
	}
	perHour := func(n int) int { return int(float64(n) / wall.Hours()) }
	cycle := "?"
	if w.cycles > 0 {
		cycle = (w.cycleSum / time.Duration(w.cycles)).Round(100 * time.Millisecond).String()
	}
	level, _ := runecraftrunner.Level(w.d)
	w.logf("pace: %d trade(s), %s essence -> %s Nature runes (+%s xp) in %s: %d essence/h, %d xp/h; cycle %s a trade; %d carrier(s) at work %v; courier trades %d (%s Natures); %d screen(s) closed before an accept; Runecrafting %d; held %s Natures%s",
		w.trades, runecraftplan.Thousands(w.essence), runecraftplan.Thousands(w.natures), runecraftplan.Thousands(w.xp), wall.Round(time.Second), perHour(w.essence), perHour(w.xp), cycle,
		len(w.carrierViews()), w.carrierNames(), w.courierTrades, runecraftplan.Thousands(w.courierNature), w.closedTrades, level, runecraftplan.Thousands(runecraftrunner.Natures(w.d)), w.alchPace())
	if w.direct.On() {
		w.logf("%s", w.direct.Summary())
	}
	if w.together.On() {
		w.logf("%s", w.together.Summary())
	}
	if w.legcasts.On() {
		w.logf("%s", w.legcasts.Summary())
	}
}

// ---- the round ----

// natureWorld is the gate round's world over the live session.
func (b *masterCrafterBot) natureWorld(ctx *Context, d *fleetrunner.Deps, started time.Time, level0, xp0 int) *masterNatureWorld {
	rw := b.runeWorld(ctx, d, started, level0, xp0)
	rw.legs = newGateLegs(d)
	rw.direct = runecraftplan.NewDirectOps(b.set.Rune.DirectOps)
	if rw.direct.On() {
		rw.logf("direct ops: ON (%s): inside the temple the altar click and the portal click go out in the same write as their walks, the portal's at the first read after the craft's hold; any leg that fails is walked and clicked the old way (%d cycles, %d failures in a row end it for the round)",
			mastercraftplan.KeyRuneDirectOps, runecraftplan.DirectBackoff, runecraftplan.DirectGiveUp)
	} else {
		rw.logf("direct ops: OFF (%s false): the altar and portal legs are walked, then clicked", mastercraftplan.KeyRuneDirectOps)
	}
	rw.together = runecraftplan.NewTogetherOps(b.set.Rune.OneWrite)
	if rw.together.On() {
		rw.logf("one-write accept: ON (%s): the confirm accept and the talisman on the ruins are written as one message once the carrier's accept is on the screen (waiting up to %s for it, then the accept alone); a cycle that fails goes the plain way for %d cycles, %d in a row end it for the round",
			mastercraftplan.KeyRuneOneWrite, runecraftplan.GateTogetherWait, runecraftplan.TogetherBackoff, runecraftplan.TogetherGiveUp)
	} else {
		rw.logf("one-write accept: OFF (%s false): the confirm accept is sent alone and the talisman after the trade has closed", mastercraftplan.KeyRuneOneWrite)
	}
	w := &masterNatureWorld{masterRuneWorld: rw, watch: runecraftrunner.NewGateWatcher(d), strangerSaid: map[string]time.Time{}, carrierSeen: started}
	w.camp.on = b.set.Rune.Campaign
	w.initAlch()
	rw.legcasts = runecraftplan.NewLegCasts(b.set.Rune.LegCasts)
	rw.castPlan, rw.castBook = w.legCastPlan, w.legCastBook
	w.legCastStartup()
	rw.onCraft = func(a runecraftplan.Altar, res runecraftrunner.CraftResult) {
		if a.Key != runecraftplan.KeyNature {
			return
		}
		w.natures += res.Runes
		w.xp += res.XPGain
		b.ledger.Rune.Gate.NoteCraft(res.Runes, res.XPGain)
		w.noteCampaignCraft(res.Runes)
		if w.crafts%5 == 0 || w.crafts == 1 {
			w.logf("crafted %d Nature runes from %d essence (+%.1f xp, Runecrafting %d -> %d); cycle %s", res.Runes, res.Essence, float64(res.XPGain), res.Level0, res.Level1, w.cycleWord())
		}
	}
	return w
}

func (w *masterNatureWorld) cycleWord() string {
	if w.cycles == 0 {
		return "not measured yet"
	}
	return (w.cycleSum / time.Duration(w.cycles)).Round(100 * time.Millisecond).String()
}

// provision banks the pack but the Natures and loads the gate pack at the mule's post: the Nature talisman and the fare home. The
// mule is asked for a talisman once when the bank has none. It reports why it could not.
func (w *masterNatureWorld) provision() (why string, ok bool) {
	a := runecraftplan.Nature
	d := w.d
	alch := w.alchWant()
	want := w.gateWant(alch)
	for attempt := 0; attempt < 2; attempt++ {
		masterDismissChat(w.ctx)
		res := runecraftrunner.Visit(d, w.post, want, nil)
		if !res.Opened {
			return "the bank at the mule's post would not open: " + res.Err, false
		}
		w.noteBank(res.Essence, res.Talisman, res.Kit)
		w.b.saveLedger(w.ctx)
		held := runecraftrunner.Talismans(d)[a.Key]
		w.logf("provisioned at %s's post: the %s %s, %d coins for the fare, %s Natures kept (keep %s; %s more in the bank); the bank holds %d %s (%d row(s))",
			w.b.mule, a.Talisman, heldWord(held), runecraftrunner.Coins(d), runecraftplan.Thousands(runecraftrunner.Natures(d)), runecraftplan.Thousands(w.set.Keep),
			runecraftplan.Thousands(res.BankNatures), res.Talisman[a.Key], a.Talisman, res.Rows)
		if held && runecraftrunner.Coins(d) >= runecraftplan.GateFare/2 {
			w.provisionAlch(alch, res)
			return "", true
		}
		if held {
			return fmt.Sprintf("the bank gave %d coins for the fare home, %d wanted", runecraftrunner.Coins(d), runecraftplan.GateFare), false
		}
		if attempt == 1 {
			break
		}
		r := w.AskMule(runecraftplan.Step{Stage: runecraftplan.StageAskMule, Altar: a, Lines: []runecraftplan.Line{{Item: a.Talisman, Qty: 1}}})
		if r.Stopped {
			return "stopped", false
		}
		if !r.OK {
			return "no " + a.Talisman + " in the bank and the mule gave none: " + r.Why, false
		}
	}
	return "no " + a.Talisman + " after asking the mule", false
}

func heldWord(b bool) string {
	if b {
		return "is held"
	}
	return "is NOT held"
}

// toGate walks from the post to the gate stand, around the spiders and by the ship from Ardougne, and reports why it did not arrive.
func (w *masterNatureWorld) toGate() (why string, ok bool) {
	dest := runecraftplan.Nature.Exit
	if stand, found := w.gateStand(); found {
		dest = stand
	}
	w.publish(mulebus.GateOpening, 8*time.Minute)
	started := time.Now()
	w.alchRoad(true) // the bows are cast away on the way (the walk's own alchemy: a cast does not stop a walk)
	walked := w.walk(dest, 0, natureHome)
	w.alchRoad(false)
	if !walked {
		return fmt.Sprintf("could not walk to the gate at %d,%d from %s", dest.X, dest.Z, fleetrunner.HereIs(w.d)), false
	}
	w.alchArrived()
	w.logf("walked to the gate stand (%d,%d) in %s; opening the gate to the carriers", dest.X, dest.Z, time.Since(started).Round(time.Second))
	w.publish(mulebus.GateOpen, 0)
	w.camp.openedAt = time.Now()
	return "", true
}

// finishNature takes the account out of the temple, publishes the gate closed, walks home, hands the mule the Natures beyond the
// keep and banks the pack. It reports what it did, and stranded when the account could not leave the temple (switchTo then rescues it).
func (w *masterNatureWorld) finishNature() (what string, stranded bool) {
	d := w.d
	var steps []string
	w.publish(mulebus.GateAway, 15*time.Minute)
	if r, _ := runecraftplan.WhereIs(runecraftrunner.Here(d)); r != runecraftplan.RegionOutside {
		if !w.leave() {
			w.publish(mulebus.GateClosed, 0)
			w.logf("WARNING: STRANDED inside the %s at %s: %d exit tries did not take the account out", r, fleetrunner.HereIs(d), masterRuneLeaveTries)
			w.b.saveLedger(w.ctx)
			return fmt.Sprintf("STRANDED inside the %s (the exit failed %d times)", r, masterRuneLeaveTries), true
		}
		steps = append(steps, "left the "+r.String())
	}
	w.publish(mulebus.GateClosed, 0)
	// A message for every carrier that has not heard the gate close yet.
	w.pubAt = time.Time{}
	w.publish(mulebus.GateClosed, 0)
	if w.postKnown {
		if w.walk(w.post, 3, natureHome) {
			steps = append(steps, "home at the mule's post")
		} else {
			steps = append(steps, "could not reach the mule's post from "+fleetrunner.HereIs(d))
			w.b.saveLedger(w.ctx)
			return strings.Join(steps, "; "), false
		}
		// The pack is banked FIRST and the Natures counted in the bank: the ones he held and the ones already banked are one stock,
		// and what he hands the mule is that stock beyond the keep. (Counting the pack alone left a keep in the bank from every round
		// of a campaign, and a campaign of seven rounds kept seven.)
		if v := mastercraftrunner.DepositAll(d, w.post); v.Opened {
			ess, tal, kit := runecraftrunner.Counts(v.Banked)
			w.noteBank(ess, tal, kit)
			steps = append(steps, "banked the pack")
			total := runecraftrunner.NatureCount(v.Banked)
			if surplus := runecraftplan.HandOverSurplus(total, w.set.Keep); surplus > 0 {
				w.handOverTried = true
				handed, why := w.b.handOverNatures(w.ctx, d, w.post, surplus)
				w.handed += handed
				w.b.ledger.Rune.Gate.HandedAtEnd += handed
				w.noteCampaignHandOver(handed)
				steps = append(steps, fmt.Sprintf("handed %s of the %s Nature runes held to %s (kept %s)", runecraftplan.Thousands(handed), runecraftplan.Thousands(total), w.b.mule, runecraftplan.Thousands(w.set.Keep)))
				if why != "" {
					w.handOverFailed = true
					steps = append(steps, "hand-over: "+why)
				}
				// What the trade left in the pack (a note stack the mule did not take) goes back into the bank.
				if v2 := mastercraftrunner.DepositAll(d, w.post); v2.Opened {
					ess, tal, kit := runecraftrunner.Counts(v2.Banked)
					w.noteBank(ess, tal, kit)
				} else {
					w.logf("WARNING: the bank at the mule's post would not open after the hand-over (%s): %d Nature runes and the rest of the pack stay with the account", v2.Err, runecraftrunner.Natures(d))
					steps = append(steps, "the pack is NOT banked after the hand-over")
				}
			}
		} else {
			w.logf("WARNING: the bank at the mule's post would not open (%s): the pack (%d Nature runes, the talisman, %d coins) stays with the account", v.Err,
				runecraftrunner.Natures(d), runecraftrunner.Coins(d))
			steps = append(steps, "the pack is NOT banked")
			if w.set.Keep < runecraftrunner.Natures(d) {
				w.handOverTried, w.handOverFailed = true, true
			}
		}
	}
	w.b.saveLedger(w.ctx)
	return strings.Join(steps, "; "), false
}

// handOverNatures draws n Nature runes from the bank at the post (banking the pack first, as every hand-over does) and trades them
// to the mule; what did not cross is banked. It returns what crossed and why not all did.
func (b *masterCrafterBot) handOverNatures(ctx *Context, d *fleetrunner.Deps, post tile.Tile, n int) (handed int, why string) {
	p := scarabMulePresenceOf(ctx, b.mule)
	switch {
	case ctx.Bus == nil || ctx.Username == "":
		return 0, "no wall bus to hand over on"
	case !p.Heard || !p.SameWorld:
		return 0, "the mule is not here: " + p.Why
	}
	h, got := mastercraftrunner.DrawLoot(d, post, []mastercraftrunner.LootWant{{ID: runecraftrunner.NatureID, Name: "Nature rune", Qty: n}})
	if !h.Opened || h.Err != "" && h.Rows == 0 {
		return 0, "drawing the Natures: " + h.Err
	}
	if h.Rows == 0 {
		return 0, "none of the Natures were in the bank to draw"
	}
	rd := fleetrunner.MuleDeps(d, ctx.Bus, ctx.Username)
	WireDonorWorlds(ctx, rd)
	rd.HomeWorld = 0 // gated on the mule being on this world: no hop
	r := masterDeliver(d, rd, b.mule, post, h.Rows, h.Slots, []string{"Nature rune"})
	result := "traded"
	switch {
	case r.Hopping:
		result = "hopping"
	case r.GaveUp:
		result = "gave-up"
	case !r.Traded:
		result = "not-traded"
	}
	b.logf(ctx, "HANDOVER nature mule=%s drawn=%s delivered=%s result=%s reason=%q", b.mule, masterDescribe(h.Got), masterDescribe(r.Delivered), result, r.Reason)
	handed = r.Delivered["Nature rune"]
	if !r.Traded {
		why = fmt.Sprintf("the trade did not happen (%s): %s", result, r.Reason)
	}
	_ = got
	return handed, why
}

// natureRound is the gate round: see the file comment.
func (b *masterCrafterBot) natureRound(ctx *Context, d *fleetrunner.Deps, started time.Time, dec mastercraftplan.Decision) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeNature}
	set := b.set.Rune
	level0, xp0 := runecraftrunner.Level(d)
	if level0 == 0 {
		out.Transient, out.Why = true, "the Runecrafting level has not been read yet"
		return out
	}
	w := b.natureWorld(ctx, d, started, level0, xp0)
	defer w.legs.close()
	defer w.startBeat()()
	if !w.members {
		w.logf("REFUSING the Nature round: this is not a members world (craftMembersWorld %v, collision pack members %v): the craft does nothing here", b.set.MembersWorld, ctx.Pack == nil || ctx.Pack.Members)
		out.Why = "not a members world"
		return out
	}
	d.Travel = runecraftTravel(set, w.members)
	defer func() { d.Travel = nil }()
	l := &b.ledger.Rune.Gate

	if !dec.Resume {
		if !w.postKnown {
			_, why, _ := b.muleHere(ctx)
			out.Transient, out.Why = true, why
			return out
		}
		if why, ok := b.arrive(ctx, d, w.post); !ok {
			out.Transient, out.Why = true, why
			return out
		}
		w.readLiveBank()
		l.Rounds++
		b.logf(ctx, "runecraft: gate: round start - Runecrafting %d, carriers %v (craftRuneRunners) + whoever is at work; keep %s Natures, couriers %s up to %s, at most %s; Natures held %s",
			level0, set.Runners, runecraftplan.Thousands(set.Keep), onOff(set.Courier), runecraftplan.Thousands(set.CourierMax), set.MaxHours, runecraftplan.Thousands(runecraftrunner.Natures(d)))
		w.campaignRoundStart(dec)
		if w.camp.on {
			// The refill comes before the gate pack: it banks the pack, walks to the market and back, and the essence it brings is
			// what the carriers' kits are made of.
			if why, ok := w.campaignSupply(); !ok {
				out.Transient, out.Why = true, why
				return out
			}
		}
		if why, ok := w.provision(); !ok {
			out.Transient, out.Why = true, why
			if r, _ := runecraftplan.WhereIs(runecraftrunner.Here(d)); r == runecraftplan.RegionOutside {
				// Nothing was started on the island: the post is where the account is.
				b.saveLedger(ctx)
			}
			return out
		}
		if why, ok := w.toGate(); !ok {
			out.Transient, out.Why = true, why
			what, stranded := w.finishNature()
			out.Why += "; " + what
			out.Stranded = stranded
			return out
		}
	} else {
		w.readLiveBank()
		w.logf("resuming from %s: %s", fleetrunner.HereIs(d), dec.Why)
		w.campaignRoundStart(dec)
		w.alchArrived() // bows left in the pack by a round that was cut short are cast away before the gate trades
	}

	res := runGateLoop(w, func(f string, a ...any) { w.logf(f, a...) }, natureMaxSteps)

	level1, xp1 := runecraftrunner.Level(d)
	if level1 > 0 {
		b.runeLevel = level1
	}
	home, stranded := "", false
	if !res.Stopped {
		home, stranded = w.finishNature()
	} else {
		w.publish(mulebus.GateClosed, 0)
	}
	out.Produced = w.natures
	out.Handed = w.handed
	out.Transient, out.Stranded = res.Transient || stranded, stranded
	out.Stuck = res.Stuck
	out.CampaignDecided = w.campaignRoundEnd(res)
	reason := res.Reason
	if home != "" {
		reason += "; " + home
	}
	out.Why = fmt.Sprintf("Runecrafting %d -> %d (+%s xp): %d trade(s) of %s essence -> %s Nature runes, %d courier trade(s) (%s Natures), %s",
		level0, level1, runecraftplan.Thousands(xp1-xp0), w.trades, runecraftplan.Thousands(w.essence), runecraftplan.Thousands(w.natures),
		w.courierTrades, runecraftplan.Thousands(w.courierNature), reason)
	if res.Stuck {
		l.LastStop = res.Reason
	} else if res.Done {
		l.LastStop = res.Reason
		b.logf(ctx, "runecraft: gate: DONE - %s", out.Why)
	}
	b.saveLedger(ctx)
	return out
}

// compile-time check that the live world is the interface.
var _ natureSteps = (*masterNatureWorld)(nil)
