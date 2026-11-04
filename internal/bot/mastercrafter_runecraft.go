package bot

// MasterCrafter's Runecrafting round (a user request: "runecraft with runners in the fleet, maxing nature
// runes; the main account, the main account, gets the Runecraft levels"). This is P1: the main account levels Runecraft 1 to 44 by
// himself, Air to 20 and Body from there, at the real altars. The plan and the trigger are
// mastercraftplan/runecraft.go and runecraftplan; the I/O is runecraftrunner. docs/runecraft_p1_2026-10-13.md has
// the design, the numbers and the live log lines.
//
//	at the post   (a new round) bank the pack, read the bank; ask the mule for what is missing -- the Air and
//	              Body talismans, essence if it publishes any, the runes of the travel teleports -- and bank it
//	a trip        bank near the ruins (Falador east for Air, Edgeville for Body): the talisman stays in the pack,
//	              the crafted runes go in, 27 essence come out (the one-tick quick bank when the account stands where
//	              the bank last opened); walk to the ruins (a Falador Teleport first when the pack holds its runes
//	              and the walk is long); the talisman on the ruins, WAITED FOR until the temple is seen; walk to
//	              the altar's side; Craft-rune (the whole pack at once, four busy ticks), VERIFIED by the essence
//	              leaving and the experience rising; the portal, its click timed to the end of the hold
//	mining        when the bank and the mule hold no essence: a stretch at the Wizards' Guild (Magic 66, Rune
//	              Mysteries, a pickaxe), 27 essence a load banked at Yanille, a batch (craftRuneBatch) at a time
//	the end       the pack banked, the walk (or Camelot Teleport) home to the mule's post, ScarabSwarm again
//
// Everything is decided afresh each step from the live state (runecraftplan.DecideLevelStage), so a restart
// anywhere -- in a temple, in the mine, on the road with a load -- carries on (mastercrafter_resume.go). Every
// step that fails is counted; three in a row end the round as a passing failure (backoff).
//
// A round NEVER ends, and ScarabSwarm never starts, inside a temple or the essence mine: the mining stretch takes the
// portal out whatever ends it (runecraftrunner.RunStretch), finish() takes the exit first when a round ends in either
// (leave: the temple's portal, the mine's, several tries), and an account that still cannot leave is held in place by
// switchTo (rescueStranded) instead of being handed to ScarabSwarm.
//
// The loop is runLevelLoop over a runeSteps world, so the whole round is tested against a simulated one
// (mastercrafter_runecraft_test.go); the live world is masterRuneWorld.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/essencerunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

const (
	// masterRuneStepFails is how many times in a row one stage may fail before the round ends as a passing failure.
	masterRuneStepFails = 3
	// masterRuneLagFails is how many failures in a row of one stage a lagging view may excuse before they count after all: a stage that
	// never succeeds still ends the round, a few minutes later.
	masterRuneLagFails = 10
	// masterRuneStall is how many times in a row the same stage may "succeed" with nothing changed before it counts
	// as a failure (a bank visit that draws nothing, a walk that arrives nowhere).
	masterRuneStall = 8
	// masterRuneMaxSteps bounds one round's steps whatever happens.
	masterRuneMaxSteps = 20000
	// masterRuneNearBank is how close (tiles) a known bank must be for a visit to use it instead of the altar's own.
	masterRuneNearBank = 60
	// masterRuneFirstLeg bounds the long walk to the ruins on foot (about 400 tiles from the mule's post), masterRuneLeg
	// any other leg, masterRuneInside a walk inside a temple.
	masterRuneFirstLeg = 10 * time.Minute
	masterRuneLeg      = 4 * time.Minute
	masterRuneInside   = 40 * time.Second
	// masterRuneTripLine is how many trips a rate line covers.
	masterRuneTripLine = 10
	// masterRuneLeaveTries is how many times the end of a round tries to take the account out of a temple or the
	// essence mine before it reports the account stranded (leave).
	masterRuneLeaveTries = 4
)

// masterRuneLeavePause is the wait between those tries (a variable so a test can shorten it).
var masterRuneLeavePause = 5 * time.Second

// runeStepResult is what one stage came to.
type runeStepResult struct {
	OK bool
	// Stopped says the bot was told to stop; Why is the reason a stage failed.
	Stopped bool
	Why     string
	// Lag says the stage failed while the account's view of the game was lagging, shaky or slow (runecraftrunner.Shaky, lagwait.go):
	// the failure is the connection's, not the stage's, and the loops do not count it toward ending the round.
	Lag bool
}

// withLag marks a failed stage as one that came out of a view that lags.
func (r runeStepResult) withLag(lag bool) runeStepResult {
	r.Lag = lag && !r.OK
	return r
}

func runeOK() runeStepResult                      { return runeStepResult{OK: true} }
func runeFail(why string) runeStepResult          { return runeStepResult{Why: why} }
func runeStopped() runeStepResult                 { return runeStepResult{Stopped: true, Why: "stopped"} }
func runeFailf(f string, a ...any) runeStepResult { return runeFail(fmt.Sprintf(f, a...)) }

// runeSteps is the round's world: what it reads and what each stage does to it.
type runeSteps interface {
	// Read is the account's state for runecraftplan.DecideLevelStage; Fingerprint is a string that changes when
	// anything a stage could have changed does (the position, the stat, the pack and bank figures).
	Read() runecraftplan.Snapshot
	Fingerprint() string
	Bank(runecraftplan.Step) runeStepResult
	AskMule(runecraftplan.Step) runeStepResult
	Mine(runecraftplan.Step) runeStepResult
	// Buy is the bounded market lot (craftRuneBuyBudget): the last rung of the essence ladder.
	Buy(runecraftplan.Step) runeStepResult
	ToRuins(runecraftplan.Step) runeStepResult
	Craft(runecraftplan.Step) runeStepResult
	PortalOut(runecraftplan.Step) runeStepResult
	// Prepare runs before every step: the dead flag cleared, a random event waited out. It reports false when the
	// bot was stopped meanwhile.
	Prepare() bool
	// Stopped says the bot was told to stop; Expired the round's time is used up (checked only outside a temple
	// and the mine, where the account can be left); Pause waits between a stage's failure and its retry.
	Stopped() bool
	Expired() bool
	Pause(time.Duration)
}

// runeLoopResult is how the loop ended.
type runeLoopResult struct {
	Reason string
	// Done: the target level is reached. Stuck: nothing to work with (a pause, not a backoff). Transient: a stage
	// failed for a passing reason. Stopped: the bot was stopped. Expired: the round's time ran out.
	Done, Stuck, Transient, Stopped, Expired bool
	Steps                                    int
	Stages                                   map[runecraftplan.Stage]int
}

// runLevelLoop decides a stage from the world's state and does it, until the target is reached, nothing is left to
// work with, a stage fails masterRuneStepFails times in a row, or the bot stops.
func runLevelLoop(w runeSteps, logf func(format string, args ...any), maxSteps int) (res runeLoopResult) {
	res.Stages = map[runecraftplan.Stage]int{}
	var last runecraftplan.Stage
	lastSig := ""
	fails, same, lagFails := 0, 0, 0
	for res.Steps < maxSteps {
		if w.Stopped() || !w.Prepare() {
			res.Stopped, res.Reason = true, "stopped"
			return res
		}
		snap := w.Read()
		step := runecraftplan.DecideLevelStage(snap)
		if snap.Where == runecraftplan.RegionOutside && w.Expired() && step.Stage != runecraftplan.StageDone {
			res.Expired, res.Reason = true, "the round's time (craftRuneRoundMinutes) is used up"
			return res
		}
		res.Steps++
		res.Stages[step.Stage]++
		if step.Stage != last {
			logf("stage %s: %s", step.Stage, step.Why)
			fails, same, lagFails = 0, 0, 0
		}
		sig := w.Fingerprint()
		if step.Stage == last && sig == lastSig {
			same++
		} else {
			same = 0
		}
		if same >= masterRuneStall {
			res.Transient, res.Reason = true, fmt.Sprintf("%s ran %d times in a row without anything changing", step.Stage, same+1)
			return res
		}
		last, lastSig = step.Stage, sig
		var r runeStepResult
		switch step.Stage {
		case runecraftplan.StageDone:
			res.Done, res.Reason = true, step.Why
			return res
		case runecraftplan.StageStuck:
			res.Stuck, res.Reason = true, step.Why
			return res
		case runecraftplan.StageBank:
			r = w.Bank(step)
		case runecraftplan.StageAskMule:
			r = w.AskMule(step)
		case runecraftplan.StageMine:
			r = w.Mine(step)
		case runecraftplan.StageBuy:
			r = w.Buy(step)
		case runecraftplan.StageWalkToRuins, runecraftplan.StageUseTalisman:
			r = w.ToRuins(step)
		case runecraftplan.StageCraft:
			r = w.Craft(step)
		case runecraftplan.StagePortalOut:
			r = w.PortalOut(step)
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
		if r.Lag && lagFails < masterRuneLagFails {
			// A failure out of a view that lags is the connection's: it is tried again from a current view and does not count toward
			// ending the round (18 Oct 2026: three unconfirmed crafts ended a gate round and closed the gate for 40 minutes).
			lagFails++
			same = 0
			logf("%s failed in a lagging view (%d of %d excused, not counted toward ending the round): %s", step.Stage, lagFails, masterRuneLagFails, r.Why)
			w.Pause(3 * time.Second)
			continue
		}
		fails++
		logf("WARNING: %s failed (%d in a row): %s", step.Stage, fails, r.Why)
		if fails >= masterRuneStepFails {
			res.Transient, res.Reason = true, fmt.Sprintf("%s failed %d times in a row: %s", step.Stage, fails, r.Why)
			return res
		}
		w.Pause(3 * time.Second)
	}
	res.Transient, res.Reason = true, fmt.Sprintf("%d steps without finishing", maxSteps)
	return res
}

// ---- the live world ----

type masterRuneWorld struct {
	b       *masterCrafterBot
	ctx     *Context
	d       *fleetrunner.Deps
	started time.Time
	set     mastercraftplan.RuneSettings
	// members is whether the world crafts members runes (the setting and the pack's flag).
	members bool

	post      tile.Tile
	postKnown bool

	// qb is the quick bank, taught at qbAt by the last full visit there.
	qb   mastercraftrunner.QuickBank
	qbAt tile.Tile

	// The bank as the last visit left it.
	bankKnown   bool
	bankEssence int
	bankTal     map[string]int
	bankKit     runecraftplan.Kit

	asked, mineFailed bool
	// buyFailed says a market lot has failed this round (it is not tried again).
	buyFailed bool
	standTry  map[string]int
	// standBad are the stand tiles a use on an altar's ruins failed from this round (the engine said "I can't reach that!" or the
	// walk did not land on the tile); the next try takes another side. Lazily made.
	standBad map[string][]tile.Tile
	plans    map[string]*runecraftrunner.TemplePlan
	// craftTick is the server tick the last craft was seen in (the portal is timed from it).
	craftTick int

	// The round's figures.
	level0, xp0           int
	trips, crafts         int
	essence, runes, xp    int
	mined                 int
	tripStart             time.Time
	tripBank, tripWork    time.Duration
	rateSaid              bool
	bankVisits, bankQuick int
	// lastAltar is the altar the round last worked at (the end of the round banks near its ruins).
	lastAltar runecraftplan.Altar
	// onCraft, when set, is told every verified craft (the Nature gate counts its Natures and experience).
	onCraft func(a runecraftplan.Altar, res runecraftrunner.CraftResult)
	// legs, when set, collects the Nature gate's per-leg timing in game ticks (mastercrafter_gatelegs.go); nil in the levelling round.
	legs *gateLegs
	// direct, when set, is the Nature gate's direct-op state (mastercrafter_directops.go); nil in the levelling round, whose legs are
	// walked and clicked as they always were. directSaid are the reasons a leg was not planned as a direct op that the log has said.
	direct     *runecraftplan.DirectOps
	directSaid map[string]bool
	// together, when set, is the Nature gate's one-write accept state (mastercrafter_together.go); nil in the levelling round. talisman is
	// the use written with the last trade's confirm accept that ToRuins has not waited on yet, togetherSaid the reasons a cycle was
	// accepted on its own that the log has said, ruins the Nature ruins' loc once it has been found.
	together     *runecraftplan.TogetherOps
	talisman     *runecraftrunner.TalismanSent
	togetherSaid map[string]bool
	ruins        locindex.Match
	haveRuins    bool
	// legcasts, when set, is the Nature gate's casts on its walking legs (mastercrafter_legcast.go); nil in the levelling round. castPlan
	// plans the cast on a leg of that many steps (nil: none) and castBook books one that landed; both are the Nature world's. legRecs are this
	// cycle's records, legRecSet which of them are set.
	legcasts  *runecraftplan.LegCasts
	castPlan  func(l runecraftplan.Leg, steps int) *runecraftrunner.LegCast
	castBook  func(l runecraftplan.Leg, rec runecraftplan.LegCastRecord)
	legRecs   [2]runecraftplan.LegCastRecord
	legRecSet [2]bool
}

func (w *masterRuneWorld) logf(format string, args ...any) {
	w.b.logf(w.ctx, "runecraft: "+format, args...)
}

func (w *masterRuneWorld) walk(dest tile.Tile, radius int, timeout time.Duration) bool {
	// Anywhere on Karamja's surface, to or from it, the walk keeps out of the jungle spiders' ground (the Nature gate's corridor;
	// the walker's own hostile avoidance knows two species and neither is a spider).
	if here := runecraftrunner.Here(w.d); runecraftplan.OnKaramja(dest) || runecraftplan.OnKaramja(here) {
		return masterWalkKaramja(w.d, dest, radius, timeout)
	}
	return masterWalkTo(w.d, dest, radius, timeout)
}

// masterWalkKaramja is runecraftrunner.WalkKaramja, the walk with the corridor's boxes as no-go rectangles, unless a test replaced it.
var masterWalkKaramja = runecraftrunner.WalkKaramja

func (w *masterRuneWorld) Stopped() bool { return w.ctx.Stopped() }

// Prepare is the start of every step: a death near the ruins leaves the account respawned with the dead latch still
// set (masterClearDead), and a random event on screen is the guardian's to finish before any click.
func (w *masterRuneWorld) Prepare() bool {
	masterClearDead(w.ctx)
	return fleetrunner.WaitForGuardian(w.d)
}

func (w *masterRuneWorld) Expired() bool {
	return mastercraftplan.RoundExpired(w.started, time.Now(), w.set.Round)
}

func (w *masterRuneWorld) Pause(d time.Duration) { w.ctx.Delay(d) }

// noteBank records what a bank visit left in the bank, in the world and the ledger.
func (w *masterRuneWorld) noteBank(essence int, tal map[string]int, kit runecraftplan.Kit) {
	w.bankKnown, w.bankEssence, w.bankTal, w.bankKit = true, essence, tal, kit
	w.b.ledger.Rune.NoteBank(essence, tal, kit, time.Now())
}

// readLiveBank loads the bank figures from the bank the session last saw open, if any.
func (w *masterRuneWorld) readLiveBank() {
	inv := w.ctx.Session.State().Inventory(actions.BankWithdraw)
	if inv == nil {
		return
	}
	ess, tal, kit := runecraftrunner.Counts(inv.NonEmpty())
	w.bankKnown, w.bankEssence, w.bankTal, w.bankKit = true, ess, tal, kit
}

func (w *masterRuneWorld) magic() int { return fleetrunner.MagicLevel(w.d) }

// kitWanted is whether travel teleports are on and Magic allows the first of them (Falador Teleport).
func (w *masterRuneWorld) kitWanted() bool {
	return w.set.Teleport && w.magic() >= mastercraftplan.ChestSpellLevel(mastercraftplan.FaladorTeleport)
}

func (w *masterRuneWorld) Read() runecraftplan.Snapshot {
	d := w.d
	level, xp := runecraftrunner.Level(d)
	here := runecraftrunner.Here(d)
	region, temple := runecraftplan.WhereIs(here)
	s := runecraftplan.Snapshot{
		Level: level, XP: xp, Target: w.set.LevelTo, Members: w.members, Where: region, Temple: temple.Key,
		PackEssence: runecraftrunner.Essence(d), PackTalisman: runecraftrunner.Talismans(d),
		BankKnown: w.bankKnown, BankEssence: w.bankEssence, BankTalisman: w.bankTal, BankKit: w.bankKit,
		Source: w.set.Essence, Batch: w.set.Batch, Asked: w.asked, MineFailed: w.mineFailed, KitWanted: w.kitWanted(),
	}
	if w.postKnown {
		s.AtPost = here.Level == w.post.Level && tile.Chebyshev(here, w.post) <= masterNearPost
	}
	if stock := muleBankCount(w.ctx, w.b.mule); stock != nil {
		s.MuleKnown = true
		count := func(item string) int { n, _ := stock(item); return max(0, n) }
		s.MuleEssence = count(runecraftplan.EssenceName)
		s.MuleTalisman = map[string]int{}
		for _, a := range runecraftplan.Altars() {
			s.MuleTalisman[a.Key] = count(a.Talisman)
		}
		s.MuleKit = runecraftplan.KitStock(count)
	}
	s.KitAskRecent = w.b.ledger.Rune.KitAskRecent(time.Now())
	s.CanMine, s.MineWhy = w.b.runeCanMine(w.ctx, w.d, w.bankKnown)
	s.BuyFailed = w.buyFailed
	if w.set.BuyBudget > 0 && w.set.Essence == runecraftplan.SourceAuto && region == runecraftplan.RegionOutside && level > 0 && level < w.set.LevelTo {
		plan, why := w.b.runeLotPlan(w.ctx, w.set, s.Remaining()-s.PackEssence-s.BankEssence)
		s.CanBuy, s.BuyQty, s.BuyWhy = why == "", plan.Qty, why
	}
	if region == runecraftplan.RegionOutside && level > 0 && level < w.set.LevelTo {
		if a, ok, _ := runecraftplan.PickAltar(level, w.members, nil); ok {
			if ruins, found := runecraftrunner.FindRuins(d, a); found {
				s.AtRuins = runecraftrunner.Reached(d, ruins)
			}
		}
	}
	return s
}

func (w *masterRuneWorld) Fingerprint() string {
	d := w.d
	level, xp := runecraftrunner.Level(d)
	return fmt.Sprintf("%v|%d|%d|%d|%d|%d|%v|%v|%d|%v|%d", runecraftrunner.Here(d), level, xp, runecraftrunner.Essence(d), w.bankEssence,
		len(runecraftrunner.Talismans(d)), w.asked, w.mineFailed, w.mined, w.buyFailed, w.b.ledger.Rune.MarketSpent)
}

// ---- bank ----

// bankFor is the bank a visit at altar a uses from here: the nearest of the mule's post, the altar's own bank and
// the gateway's within masterRuneNearBank tiles, else the altar's own.
func (w *masterRuneWorld) bankFor(a runecraftplan.Altar, here tile.Tile) (tile.Tile, bool) {
	var cands []tile.Tile
	if w.postKnown {
		cands = append(cands, w.post)
	}
	if !a.NoBank {
		cands = append(cands, a.Bank)
	}
	if gw, ok := essenceplan.GatewayByKey(w.set.Gateway); ok && w.set.Essence != runecraftplan.SourceBank && w.set.Essence != runecraftplan.SourceMule {
		cands = append(cands, gw.Bank)
	}
	best, bd := tile.Tile{}, 1<<30
	for _, t := range cands {
		if t.Level != here.Level {
			continue
		}
		if dist := tile.Chebyshev(here, t); dist <= masterRuneNearBank && dist < bd {
			best, bd = t, dist
		}
	}
	if bd < 1<<30 {
		return best, true
	}
	if a.NoBank {
		return tile.Tile{}, false
	}
	return a.Bank, true
}

func (w *masterRuneWorld) Bank(st runecraftplan.Step) runeStepResult {
	a := st.Altar
	w.lastAltar = a
	d := w.d
	here := runecraftrunner.Here(d)
	at, ok := w.bankFor(a, here)
	if !ok {
		return runeFailf("the %s altar has no bank a non-quest account can reach", a.Name)
	}
	want := runecraftrunner.Want{Talisman: int32(a.TalismanID), Essence: true}
	if w.kitWanted() {
		bank := runecraftplan.Kit{Law: 99, Water: 99, Air: 99} // unknown until read: try, the visit draws what is there
		if w.bankKnown {
			bank = w.bankKit
		}
		if runecraftplan.KitNeeded(true, fleetplan.TravelStraightTiles(at, a.Exit), runeTravelMinSave, bank) {
			want.Kit = runecraftplan.KitDraw
		}
	}
	// Onto the tile the quick bank was taught, when the account is near enough to walk it.
	if w.qbAt == at && !w.qb.Ready(d) {
		if stand, ok := w.qb.Stand(); ok && here.Level == stand.Level && tile.Chebyshev(here, stand) <= masterRuneNearBank {
			w.walk(stand, 0, masterRuneLeg)
		}
	}
	masterDismissChat(w.ctx)
	res := runecraftrunner.Visit(d, at, want, &w.qb)
	if !res.Opened {
		return runeFail(res.Err)
	}
	if !res.Quick {
		w.qbAt = at
	}
	w.bankVisits++
	if res.Quick {
		w.bankQuick++
	}
	w.tripBank += res.Took
	w.noteBank(res.Essence, res.Talisman, res.Kit)
	w.b.saveLedger(w.ctx)
	how := "full"
	if res.Quick {
		how = "quick"
	}
	kit := ""
	if want.Kit != (runecraftplan.Kit{}) {
		kit = fmt.Sprintf(", teleport kit %d Law %d Water %d Air", res.PackKit.Law, res.PackKit.Water, res.PackKit.Air)
	}
	why := ""
	if res.QuickWhy != "" {
		why = " (quick visit fell back: " + res.QuickWhy + ")"
	}
	w.logf("bank (%s %d,%d, %s visit in %s%s): pack now %d essence%s, the %s held %v; the bank holds %s essence, talismans %v, kit %d/%d/%d, %d row(s)",
		w.bankName(at), at.X, at.Z, how, res.Took.Round(100*time.Millisecond), why, res.PackEssence, kit, a.Talisman, res.Held[a.Key],
		runecraftplan.Thousands(res.Essence), res.Talisman, res.Kit.Law, res.Kit.Water, res.Kit.Air, res.Rows)
	if res.Err != "" {
		return runeFail(res.Err)
	}
	return runeOK()
}

// runeTravelMinSave is the straight-line tiles a walk must be for the pack's teleport kit to be drawn for it
// (scarabplan.TeleTravelSettings.MinSave's default).
const runeTravelMinSave = 150

// bankName names the bank by the tile in front of its booth.
func (w *masterRuneWorld) bankName(at tile.Tile) string {
	if w.postKnown && at == w.post {
		return w.b.mule + "'s post"
	}
	for _, a := range runecraftplan.Altars() {
		if !a.NoBank && at == a.Bank {
			return a.BankName
		}
	}
	if gw, ok := essenceplan.GatewayByKey(w.set.Gateway); ok && at == gw.Bank {
		return gw.BankName
	}
	return "bank"
}

// ---- ask the mule ----

func (w *masterRuneWorld) AskMule(st runecraftplan.Step) runeStepResult {
	if !w.postKnown {
		post, why, ok := w.b.muleHere(w.ctx)
		if !ok {
			return runeFail(why)
		}
		w.post, w.postKnown = post, true
	}
	lines := make([]mastercraftplan.Line, 0, len(st.Lines))
	for _, l := range st.Lines {
		lines = append(lines, mastercraftplan.Line{Item: l.Item, Qty: l.Qty})
	}
	a := w.b.askMuleWith(w.ctx, w.d, lines, masterAskOpts{noted: true})
	if a.Stopped {
		return runeStopped()
	}
	if st.Optional {
		// The teleport kit alone (runecraftplan.Step.Optional): asked at most every runecraftplan.KitAskGap whether or not the mule
		// answered, and never a failure of the round, which goes on without a fresh kit (on foot, or with what the bank holds).
		w.b.ledger.Rune.KitAskedAt = time.Now()
		w.asked = true
		w.b.saveLedger(w.ctx)
		if a.Traded == 0 {
			w.logf("the teleport kit top-up (%s) brought nothing%s: carrying on without it, not asking again for %s", describeRuneLines(st.Lines), whySuffix(a.Why), runecraftplan.KitAskGap)
			return runeOK()
		}
		w.logf("asked %s for the teleport kit (%s): got %s%s", w.b.mule, describeRuneLines(st.Lines), masterDescribe(a.Got), whySuffix(a.Why))
		if v := mastercraftrunner.DepositAll(w.d, w.post); v.Opened {
			ess, tal, kit := runecraftrunner.Counts(v.Banked)
			w.noteBank(ess, tal, kit)
			w.b.saveLedger(w.ctx)
		} else {
			w.logf("WARNING: could not bank the teleport kit the mule gave (%s): the next bank visit does", v.Err)
		}
		return runeOK()
	}
	w.logf("asked %s for %s: got %s%s", w.b.mule, describeRuneLines(st.Lines), masterDescribe(a.Got), whySuffix(a.Why))
	for _, l := range st.Lines {
		if l.Item == runecraftplan.EssenceName {
			w.b.ledger.Rune.Asked += a.Got[l.Item]
		}
	}
	// What the mule had is banked at once (the notes become the items), so the next stage reads it.
	if a.Traded > 0 {
		v := mastercraftrunner.DepositAll(w.d, w.post)
		if !v.Opened {
			return runeFail("could not bank what the mule gave: " + v.Err)
		}
		ess, tal, kit := runecraftrunner.Counts(v.Banked)
		w.noteBank(ess, tal, kit)
		w.b.saveLedger(w.ctx)
	}
	// A mule that truly holds none of it, or gave it, is asked no more this round; one that could not be reached
	// or deferred the kit is a passing failure: try again, and back off if it keeps up.
	if a.Traded > 0 || a.declinedEmpty() {
		w.asked = true
		return runeOK()
	}
	return runeFail(a.Why)
}

func whySuffix(why string) string {
	if why == "" {
		return ""
	}
	return " (" + why + ")"
}

func describeRuneLines(lines []runecraftplan.Line) string {
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		parts = append(parts, fmt.Sprintf("%s %s", runecraftplan.Thousands(l.Qty), l.Item))
	}
	return strings.Join(parts, ", ")
}

// ---- mine ----

// runeMineEnv is the mining stretch's world: the live one unless a test replaced it.
var runeMineEnv = runecraftrunner.NewLiveEnv

func (w *masterRuneWorld) Mine(st runecraftplan.Step) runeStepResult {
	gw, ok := essenceplan.GatewayByKey(w.set.Gateway)
	if !ok {
		return runeFail("craftRuneGateway " + w.set.Gateway + " is not a gateway")
	}
	if ok, why := w.b.runeCanMine(w.ctx, w.d, w.bankKnown); !ok {
		w.mineFailed = true
		return runeFail("cannot mine: " + why)
	}
	// Nothing pending in the pack goes down the mine with the account: it is banked first by the stretch.
	env := runeMineEnv(w.d, runecraftrunner.LiveOptions{Gateway: gw, Walk: w.walk})
	budget := time.Duration(st.Goal/runecraftplan.LoadSize+4) * 6 * time.Minute
	w.logf("mining stretch at the %s gateway (%s): %s essence to bank, a batch of %s; Mining %d", gw.Key, gw.NPC,
		runecraftplan.Thousands(st.Goal), runecraftplan.Thousands(w.set.Batch), essencerunner.MiningLevel(runecraftrunner.EssenceDeps(w.d)))
	res := runecraftrunner.RunStretch(env, runecraftrunner.StretchOptions{
		Goal: st.Goal, Gateway: gw, MaxFails: 4, Budget: budget, Hold: func() bool { return !w.Expired() },
		Logf: func(f string, a ...any) { w.b.logf(w.ctx, f, a...) },
	})
	l := &w.b.ledger.Rune
	l.Mined += res.Banked
	l.MineStretches++
	w.mined += res.Banked
	if res.BankEssence >= 0 {
		w.bankKnown, w.bankEssence = true, res.BankEssence
		l.BankEssence, l.BankSeen = res.BankEssence, time.Now()
	}
	w.b.saveLedger(w.ctx)
	w.logf("mining stretch over after %s: %s essence banked in %d load(s), %d teleport(s), %d failure(s); %s",
		res.Took.Round(time.Second), runecraftplan.Thousands(res.Banked), res.Loads, res.Teleports, res.Fails, res.Reason)
	if res.InMine {
		// The portal would not take the account out. That is a failure of this stage (three in a row end the round, and the
		// end of the round tries the exit again, leave, before anything is started from here), not a reason to stop mining.
		return runeFail("the mining stretch ended with the account still inside the mine: " + res.Reason)
	}
	if res.Done {
		return runeOK()
	}
	if res.Banked > 0 && !res.Refused {
		return runeOK() // something came of it: craft it, and mine again when the bank runs dry
	}
	if w.Expired() && !res.Refused && res.Fails == 0 {
		// The round's clock ended the stretch (it took the account out of the mine first): not a failure of mining. The
		// loop reads the account outside with the time used up and ends the round.
		return runeOK()
	}
	w.mineFailed = true
	return runeFail("the mining stretch brought nothing: " + res.Reason)
}

// ---- the ruins ----

func (w *masterRuneWorld) ToRuins(st runecraftplan.Step) runeStepResult {
	a, d := st.Altar, w.d
	w.lastAltar = a
	ruins, ok := runecraftrunner.FindRuins(d, a)
	if !ok {
		return runeFailf("the %s ruins (loc %d) are not in the loc index near %d,%d", a.Name, a.RuinsID, a.Ruins.X, a.Ruins.Z)
	}
	masterDismissChat(w.ctx)
	// A talisman that went out with the trade's confirm accept (mastercrafter_together.go) is waited for, not written again. It is already
	// in flight when this stage runs, so the account may land inside the temple at any point from here to the use below, and a lagging
	// view delivers the landing when AwaitView returns: an account inside the temple with a written talisman is where it was going, and
	// the stage is done (it must not walk to a stand, nor use the talisman a second time, from there).
	written, landed := w.takeTalisman(a, ruins)
	// Where the account stands is read from the view: a view that lags is waited for before a stand is chosen from it.
	runecraftrunner.AwaitView(d)
	if !landed {
		written, landed = w.recheckTalisman(a, ruins, written)
	}
	if landed {
		return w.talismanHadLanded(a, written)
	}
	here := runecraftrunner.Here(d)
	try := w.standTry[a.Key]
	if try > 0 || !runecraftrunner.Reached(d, ruins) {
		// The tiles the engine accepts a use from AND the collision pack lets an account stand on (runecraftrunner.Stands), nearest the
		// account first, the ones a use already failed from this round left out. (The first try used to take the nearest by the
		// locreach list alone, and a blocked tile led it: 16 Oct 2026, two failures of 27 seconds, four times.)
		stands := dropStands(runecraftrunner.Stands(d, ruins, here), w.standBad[a.Key])
		if len(stands) == 0 {
			delete(w.standBad, a.Key)
			stands = runecraftrunner.Stands(d, ruins, here)
		}
		if len(stands) == 0 {
			return runeFailf("no tile beside the %s ruins that the engine accepts a use from and an account can stand on", a.Name)
		}
		target := stands[0]
		budget := masterRuneLeg
		if tile.Chebyshev(here, target) > 120 {
			budget = masterRuneFirstLeg
		}
		walked := time.Now()
		if !w.walk(target, 0, budget) {
			// A tile that cannot be reached (an NPC on it, a door the planner mis-modelled): the next try is another side. Not so
			// in a view that lags: a walk that stalled there says nothing about the tile.
			lag := runecraftrunner.Shaky(d)
			if !lag {
				w.badStand(a.Key, target)
				w.standTry[a.Key]++
			}
			return runeFailf("could not walk to %v beside the %s ruins from %s", target, a.Name, fleetrunner.HereIs(d)).withLag(lag)
		}
		// A walk that ends beside the tile is not a walk to it: nothing is clicked on the ruins from a tile the engine does not accept.
		if !runecraftrunner.Reached(d, ruins) {
			if err := actions.Walk(d.Session, []pathfind.NavPoint{{X: target.X, Z: target.Z, Level: target.Level}}, true); err == nil {
				w.ctx.DelayUntil(func() bool { return runecraftrunner.Here(d) == target }, 4*time.Second)
			}
			if !runecraftrunner.Reached(d, ruins) {
				lag := runecraftrunner.Shaky(d)
				if !lag {
					w.badStand(a.Key, target)
					w.standTry[a.Key]++
				}
				return runeFailf("the walk to %v beside the %s ruins ended at %s, where the engine does not accept a use on them", target, a.Name, fleetrunner.HereIs(d)).withLag(lag)
			}
		}
		w.logf("walked to %v beside the %s ruins in %s", target, a.Name, time.Since(walked).Round(time.Second))
	}
	stand := runecraftrunner.Here(d)
	if st.Stage == runecraftplan.StageUseTalisman {
		w.legs.enter(st.Stage)
	}
	res := runecraftrunner.UseTalismanSent(d, a, ruins, written)
	if written != nil {
		w.togetherLanded(res)
	}
	if !res.Arrived {
		// A use whose result was not SEEN in a view that lags says nothing about the stand (18 Oct 2026: "not seen inside the Nature
		// temple within 6s" with the teleport's own line in the chat), so it neither counts against the stand nor against the round.
		refused := strings.Contains(strings.ToLower(res.Said), "can't reach")
		if refused || !res.Lag {
			w.standTry[a.Key]++
			// A stand the engine refused is not tried again this round; one that failed twice for no reason said is not either (the old
			// rotation through the stands, kept for a use that fails from a tile the engine accepts).
			if refused || w.standTry[a.Key] >= 2 {
				w.badStand(a.Key, stand)
			}
		}
		return runeFailf("%s (from %v, stand try %d)", res.Reason, stand, try).withLag(res.Lag && !refused)
	}
	w.standTry[a.Key] = 0
	delete(w.standBad, a.Key)
	w.legs.noteTalisman(res.ClickTick, res.Clicks)
	w.logf("%s ruins: %s used on '%s' (%d,%d) from stand (%d,%d); arrived inside at (%d,%d) after %d ticks (clicked in tick %d)",
		a.Name, a.Talisman, runecraftplan.RuinsName, ruins.X, ruins.Z, stand.X, stand.Z, res.At.X, res.At.Z, res.Ticks, res.ClickTick)
	return runeOK()
}

// badStand remembers a stand tile a use on altar key's ruins failed from.
func (w *masterRuneWorld) badStand(key string, t tile.Tile) {
	if w.standBad == nil {
		w.standBad = map[string][]tile.Tile{}
	}
	w.standBad[key] = append(w.standBad[key], t)
}

// dropStands is stands without the tiles in bad.
func dropStands(stands, bad []tile.Tile) []tile.Tile {
	if len(bad) == 0 {
		return stands
	}
	out := make([]tile.Tile, 0, len(stands))
next:
	for _, s := range stands {
		for _, b := range bad {
			if s == b {
				continue next
			}
		}
		out = append(out, s)
	}
	return out
}

// templePlan is the shortest inside walk for altar a, planned once per round.
func (w *masterRuneWorld) templePlan(a runecraftplan.Altar, altar, portal locindex.Match) *runecraftrunner.TemplePlan {
	if p, ok := w.plans[a.Key]; ok {
		return p
	}
	var out *runecraftrunner.TemplePlan
	if plan, ok := runecraftrunner.PlanTemple(w.d.Pack, w.d.Config, a, altar, portal); ok {
		out = &plan
		w.logf("%s temple walk planned: arrival -> altar stand (%d,%d) %d steps -> portal stand (%d,%d) %d steps",
			a.Name, plan.AltarStand.X, plan.AltarStand.Z, plan.ToAltar, plan.PortalStand.X, plan.PortalStand.Z, plan.ToPortal)
	}
	w.plans[a.Key] = out
	return out
}

func (w *masterRuneWorld) Craft(st runecraftplan.Step) runeStepResult {
	a, d := st.Altar, w.d
	altar, ok := runecraftrunner.FindAltar(d, a)
	if !ok {
		return runeFailf("the %s altar (loc %d) is not in the loc index near %d,%d", a.Name, a.AltarID, a.AltarAt.X, a.AltarAt.Z)
	}
	portal, ok := runecraftrunner.FindPortal(d, a)
	if !ok {
		return runeFailf("the %s temple's portal (loc %d) is not in the loc index near %d,%d", a.Name, a.PortalID, a.PortalAt.X, a.PortalAt.Z)
	}
	masterDismissChat(w.ctx)
	w.legs.enter(st.Stage)
	// The altar is clicked from the altar's own stand, every try: where the account stands is read from the view, so a view that lags
	// is waited for first, and a retry after a craft that was not seen walks onto the stand again if the account is not on one.
	runecraftrunner.AwaitView(d)
	if !runecraftrunner.Reached(d, altar) {
		// The Nature gate sends the walk and the click in one write (mastercrafter_directops.go). A leg it could not or did not do that way
		// falls through to the walk-then-click below, which is what every other round does.
		if r, handled := w.craftDirect(st, altar, portal); handled {
			return r
		}
	}
	if !runecraftrunner.Reached(d, altar) {
		target, ok := tile.Tile{}, false
		if plan := w.templePlan(a, altar, portal); plan != nil {
			target, ok = plan.AltarStand, true
		} else if stands := runecraftrunner.Stands(d, altar, runecraftrunner.Here(d)); len(stands) > 0 {
			target, ok = stands[0], true
		}
		if !ok || !w.walk(target, 0, masterRuneInside) {
			return runeFailf("could not walk to the %s altar from %s", a.Name, fleetrunner.HereIs(d)).withLag(runecraftrunner.Shaky(d))
		}
		// A walk that ends beside the stand is not a walk to it: one direct click onto the tile before the altar is clicked from afar.
		if !runecraftrunner.Reached(d, altar) {
			if err := actions.Walk(d.Session, []pathfind.NavPoint{{X: target.X, Z: target.Z, Level: target.Level}}, true); err == nil {
				w.ctx.DelayUntil(func() bool { return runecraftrunner.Reached(d, altar) }, 4*time.Second)
			}
		}
	}
	workStart := time.Now()
	res := runecraftrunner.Craft(d, a, altar)
	if !res.OK() {
		return runeFailf("craft of %d essence: %s", res.Essence, res.Reason).withLag(res.Lag)
	}
	return w.craftDone(a, res, workStart)
}

// craftDone is what a verified craft changes: the tick the portal is timed from, the round's figures, the ledger and the log.
func (w *masterRuneWorld) craftDone(a runecraftplan.Altar, res runecraftrunner.CraftResult, workStart time.Time) runeStepResult {
	w.craftTick = res.Tick
	w.legs.noteCraft(res.Tick, res.Clicks)
	w.crafts++
	w.essence += res.Essence
	w.runes += res.Runes
	w.xp += res.XPGain
	w.tripWork += time.Since(workStart)
	l := &w.b.ledger.Rune
	l.NoteCraft(a.Key, res.Essence, res.Runes, res.XPGain)
	w.b.saveLedger(w.ctx)
	w.logf("crafted %d %s runes (+%.1f xp, Runecrafting %d -> %d); essence %d left; %d-tick hold",
		res.Runes, a.Name, float64(res.XPGain), res.Level0, res.Level1, res.Left, runecraftplan.HoldTicks)
	if !w.rateSaid {
		w.rateSaid = true
		w.logf("measured experience rate: +%d xp for %d essence at the %s altar where the table says +%d: x%.2f; %d runes where %d were expected at level %d",
			res.XPGain, res.Essence, a.Name, res.Verify.ExpectedXP, res.Verify.Rate, res.Runes, runecraftplan.RunesFor(a, res.Level0, res.Essence), res.Level0)
	}
	w.levelNote(a, res.Level0, res.Level1)
	if w.onCraft != nil {
		w.onCraft(a, res)
	}
	return runeOK()
}

// levelNote logs the level-ups that matter: an altar's, and the target.
func (w *masterRuneWorld) levelNote(a runecraftplan.Altar, from, to int) {
	if to <= from {
		return
	}
	for _, next := range runecraftplan.Altars() {
		if from < next.Level && to >= next.Level && next.Level <= w.set.LevelTo {
			_, xp := runecraftrunner.Level(w.d)
			left := runecraftplan.BudgetFrom(xp, w.set.LevelTo, w.members)
			w.logf("%s reached %d (%s xp); switching to %s; %s essence to go",
				a.Name, to, runecraftplan.Thousands(xp), next.Name, runecraftplan.Thousands(left.Total))
		}
	}
}

// takePortal leaves a's temple by its portal: the approach onto a tile the engine accepts the portal from and the click,
// timed to the end of the craft's hold when craftTick (the tick a craft was seen in) is known. why is empty when the
// account is outside afterwards.
func (w *masterRuneWorld) takePortal(a runecraftplan.Altar, craftTick int) (res runecraftrunner.PortalResult, why string) {
	d := w.d
	portal, ok := runecraftrunner.FindPortal(d, a)
	if !ok {
		return res, fmt.Sprintf("the %s temple's portal (loc %d) is not in the loc index near %d,%d", a.Name, a.PortalID, a.PortalAt.X, a.PortalAt.Z)
	}
	var plan *runecraftrunner.TemplePlan
	if altar, haveAltar := runecraftrunner.FindAltar(d, a); haveAltar {
		plan = w.templePlan(a, altar, portal)
	}
	approach := func() bool {
		masterDismissChat(w.ctx) // a level-up's congratulations would stop the portal as it stops the bank
		runecraftrunner.AwaitView(d)
		if runecraftrunner.Reached(d, portal) {
			return true
		}
		target, ok := tile.Tile{}, false
		if plan != nil {
			target, ok = plan.PortalStand, true
		} else if stands := runecraftrunner.Stands(d, portal, runecraftrunner.Here(d)); len(stands) > 0 {
			target, ok = stands[0], true
		}
		return ok && w.walk(target, 0, masterRuneInside)
	}
	if direct := w.portalDirect(a, portal, craftTick); direct != nil {
		res = runecraftrunner.PortalOutCast(d, a, portal, craftTick, direct.plan, approach)
		castFailed := w.legCastScore(runecraftplan.LegPortal, *direct.cast, res.Cast, res.Out, res.Lag)
		w.portalDirectNote(res, castFailed)
	} else {
		res = runecraftrunner.PortalOut(d, a, portal, craftTick, approach)
	}
	if !res.Out {
		return res, res.Reason
	}
	w.legs.notePortal(res.Clicks, res.Waited)
	return res, ""
}

func (w *masterRuneWorld) PortalOut(st runecraftplan.Step) runeStepResult {
	a := st.Altar
	w.legs.enter(st.Stage)
	craftTick := w.craftTick
	w.craftTick = 0
	res, why := w.takePortal(a, craftTick)
	if why != "" {
		return runeFail(why).withLag(res.Lag)
	}
	w.trips++
	w.b.ledger.Rune.Trips++
	w.logf("%s temple: portal taken after waiting %d tick(s) for the craft's hold, %d click(s); back outside at (%d,%d)", a.Name, res.Waited, res.Clicks, res.At.X, res.At.Z)
	w.tripLine()
	return runeOK()
}

// tripLine is the rate line every masterRuneTripLine trips: the experience an hour and when the target comes.
func (w *masterRuneWorld) tripLine() {
	if w.trips%masterRuneTripLine != 0 || w.crafts == 0 {
		return
	}
	wall := time.Since(w.started)
	if wall <= 0 {
		return
	}
	perHour := func(n int) int { return int(float64(n) / wall.Hours()) }
	level, xp := runecraftrunner.Level(w.d)
	left := runecraftplan.BudgetFrom(xp, w.set.LevelTo, w.members)
	eta := "?"
	if per := perHour(w.essence); per > 0 && left.Total > 0 {
		eta = (time.Duration(float64(left.Total) / float64(per) * float64(time.Hour))).Round(time.Minute).String()
	}
	w.logf("pace: %d trip(s), %d craft(s), %s essence -> %s runes, +%s xp in %s: %d essence/h, %d xp/h; Runecrafting %d, %s essence to go (about %s); bank visits %d (%d quick)",
		w.trips, w.crafts, runecraftplan.Thousands(w.essence), runecraftplan.Thousands(w.runes), runecraftplan.Thousands(w.xp), wall.Round(time.Second),
		perHour(w.essence), perHour(w.xp), level, runecraftplan.Thousands(left.Total), eta, w.bankVisits, w.bankQuick)
}

// ---- the end of the round ----

// leave takes the account out of a temple or the essence mine, where a round must never end -- and ScarabSwarm never
// start: there is no bank there and nothing for either to do. A round ends there when three stages in a row fail (a
// craft that did not happen, a portal that did not take), when its steps run out, or when the mining stretch's own exit
// failed. It tries the exit up to masterRuneLeaveTries times, a masterRuneLeavePause apart, and reports whether the
// account is outside (true at once when it already is).
func (w *masterRuneWorld) leave() bool {
	for try := 1; try <= masterRuneLeaveTries; try++ {
		region, temple := runecraftplan.WhereIs(runecraftrunner.Here(w.d))
		if region == runecraftplan.RegionOutside {
			return true
		}
		if w.Stopped() {
			return false
		}
		switch region {
		case runecraftplan.RegionTemple:
			if _, why := w.takePortal(temple, 0); why != "" {
				w.logf("leaving the %s temple (try %d of %d): %s", temple.Name, try, masterRuneLeaveTries, why)
			}
		case runecraftplan.RegionMine:
			gw, _ := essenceplan.GatewayByKey(w.set.Gateway)
			env := runeMineEnv(w.d, runecraftrunner.LiveOptions{Gateway: gw, Walk: w.walk})
			if !runecraftrunner.LeaveMine(env, 1) {
				w.logf("leaving the essence mine (try %d of %d): the portal did not take the account out", try, masterRuneLeaveTries)
			}
		}
		if r, _ := runecraftplan.WhereIs(runecraftrunner.Here(w.d)); r == runecraftplan.RegionOutside {
			w.logf("left the %s (try %d)", region, try)
			return true
		}
		w.Pause(masterRuneLeavePause)
	}
	r, _ := runecraftplan.WhereIs(runecraftrunner.Here(w.d))
	return r == runecraftplan.RegionOutside
}

// finish takes the account out of a temple or the mine if it is in one, banks what the pack holds and goes home to the
// mule's post: the bank near the ruins (a visit that draws the Camelot kit when the walk home is long), then a walk that
// casts the teleport by itself. It reports what it did for the log, and stranded when the account could not be taken out
// of the temple or the mine: the caller must not start ScarabSwarm from there (switchTo rescues it first).
func (w *masterRuneWorld) finish() (what string, stranded bool) {
	d := w.d
	var steps []string
	if r, _ := runecraftplan.WhereIs(runecraftrunner.Here(d)); r != runecraftplan.RegionOutside {
		if !w.leave() {
			w.logf("WARNING: STRANDED inside the %s at %s: %d exit tries did not take the account out", r, fleetrunner.HereIs(d), masterRuneLeaveTries)
			w.b.saveLedger(w.ctx)
			return fmt.Sprintf("STRANDED inside the %s (the exit failed %d times)", r, masterRuneLeaveTries), true
		}
		steps = append(steps, "left the "+r.String())
	}
	here := runecraftrunner.Here(d)
	// Bank the pack at the nearest known bank when it is close, and draw the kit for the long walk home.
	a := w.lastAltar
	if a.Key == "" {
		level, _ := runecraftrunner.Level(d)
		a, _, _ = runecraftplan.PickAltar(max(1, level), w.members, nil)
	}
	// The pack is always banked or said loudly not to be: a load (essence, the runes just made) carried home unbanked is found by
	// nobody until the next round, and a levelling round that finds it "under way" resumes it.
	banked := false
	if at, ok := w.bankFor(a, here); ok && tile.Chebyshev(here, at) <= masterRuneNearBank*2 {
		want := runecraftrunner.Want{}
		if w.postKnown && w.kitWanted() && fleetplan.TravelStraightTiles(at, w.post) >= runeTravelMinSave {
			want.Kit = runecraftplan.Kit{Law: 1, Air: 5}
		}
		res := runecraftrunner.Visit(d, at, want, nil)
		if res.Opened {
			w.noteBank(res.Essence, res.Talisman, res.Kit)
			steps = append(steps, fmt.Sprintf("banked the pack at %s", w.bankName(at)))
			banked = true
		} else {
			w.logf("WARNING: the bank at %s would not open (%s): the pack (%s) is not banked there", w.bankName(at), res.Err, w.packLoad())
			steps = append(steps, "the bank at "+w.bankName(at)+" would not open")
		}
	}
	if w.postKnown {
		if w.walk(w.post, 3, scarabConsolidateWalk) {
			steps = append(steps, "home at the mule's post")
		} else {
			steps = append(steps, "could not reach the mule's post from "+fleetrunner.HereIs(d))
		}
		// Not banked on the way (no bank in reach, or it would not open): at the post, where there is one.
		if !banked && w.hasLoad() && tile.Chebyshev(runecraftrunner.Here(d), w.post) <= masterNearPost {
			if v := mastercraftrunner.DepositAll(d, w.post); v.Opened {
				ess, tal, kit := runecraftrunner.Counts(v.Banked)
				w.noteBank(ess, tal, kit)
				steps = append(steps, "banked the pack at the mule's post")
				banked = true
			} else {
				w.logf("WARNING: the bank at the mule's post would not open (%s) either", v.Err)
			}
		}
	}
	if !banked && w.hasLoad() {
		w.logf("WARNING: the round ends with the pack NOT BANKED: %s stay with the account at %s (the next round banks them first; a levelling round finding a load and the talisman resumes it)", w.packLoad(), fleetrunner.HereIs(d))
		steps = append(steps, "the pack ("+w.packLoad()+") is NOT banked")
	}
	w.b.saveLedger(w.ctx)
	return strings.Join(steps, "; "), false
}

// hasLoad reports whether the pack holds essence or any altar's runes (the product of a trip).
func (w *masterRuneWorld) hasLoad() bool {
	if runecraftrunner.Essence(w.d) > 0 {
		return true
	}
	for _, a := range runecraftplan.Altars() {
		if runecraftrunner.PackCount(w.d, int32(a.RuneID)) > 0 {
			return true
		}
	}
	return false
}

// packLoad describes the pack's load for a log line.
func (w *masterRuneWorld) packLoad() string {
	parts := []string{fmt.Sprintf("%d essence", runecraftrunner.Essence(w.d))}
	for _, a := range runecraftplan.Altars() {
		if n := runecraftrunner.PackCount(w.d, int32(a.RuneID)); n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, a.Rune))
		}
	}
	return strings.Join(parts, ", ")
}

// ---- the round ----

// runecraftTravel is the teleport travel a levelling round walks with: a Falador Teleport out to the altars and
// a Camelot Teleport home, cast by scarabrunner.WalkTo itself whenever the pack holds the runes (the bank visit
// draws them) and the walk saves 150 tiles. No walk alchemy, no rub: the main account does not run-stall (owner).
func runecraftTravel(set mastercraftplan.RuneSettings, members bool) *fleetrunner.Travel {
	return &fleetrunner.Travel{
		Tele:        fleetplan.TeleTravelSettings{Enabled: set.Teleport, AnyWalk: true, AirRunes: true, MinSave: runeTravelMinSave},
		Members:     members,
		KeepRunning: true,
	}
}

// runeMembers is whether this world crafts members runes: the setting and the collision pack's own flag.
func (b *masterCrafterBot) runeMembers(ctx *Context) bool {
	return b.set.MembersWorld && (ctx.Pack == nil || ctx.Pack.Members)
}

// runeCanMine says whether the account can mine essence at its gateway now: the gateway's Magic, Rune
// Mysteries, and a pickaxe worn, carried or (when the bank has been read) in the bank.
func (b *masterCrafterBot) runeCanMine(ctx *Context, d *fleetrunner.Deps, bankKnown bool) (bool, string) {
	set := b.set.Rune
	if set.Essence == runecraftplan.SourceBank || set.Essence == runecraftplan.SourceMule {
		return false, "mining is off (craftRuneEssence " + string(set.Essence) + ")"
	}
	gw, ok := essenceplan.GatewayByKey(set.Gateway)
	if !ok {
		return false, "craftRuneGateway " + set.Gateway + " is not a gateway"
	}
	if ctx == nil || ctx.Session == nil || d == nil {
		return false, "no session"
	}
	magic := fleetrunner.MagicLevel(d)
	if magic <= 0 {
		return false, "Magic has not been read yet"
	}
	if ok, why := gw.Allows(magic); !ok {
		return false, why
	}
	// Red or yellow in the quest tab is not done; a colour never sent (Unknown) is not held against the account -- the
	// gateway's refusal, read at once, says so if it is (essencerunner.TeleportInVia).
	if status, known := quests.Status(ctx.Session.State(), essenceplan.RuneMysteries); known && (status == quests.NotStarted || status == quests.InProgress) {
		return false, essenceplan.RuneMysteries + " is not complete"
	}
	ed := runecraftrunner.EssenceDeps(d)
	var bankNames []string
	if inv := ctx.Session.State().Inventory(actions.BankWithdraw); inv != nil && ctx.Config != nil {
		seen := map[string]bool{}
		for _, it := range inv.NonEmpty() {
			if o, ok := ctx.Config.Obj(int(it.ID)); ok && !seen[o.Name] {
				seen[o.Name] = true
				bankNames = append(bankNames, o.Name)
			}
		}
		bankKnown = true
	}
	if ctx.Config == nil {
		return true, ""
	}
	res := essenceplan.ResolvePick(essenceplan.BestAvailable, essencerunner.MiningLevel(ed), essencerunner.HeldNames(ed), bankNames)
	if res.Kind == essenceplan.PickStop && bankKnown {
		return false, res.Reason
	}
	return true, ""
}

// runeSnapshot fills the decision's reading of the account (Snapshot.Rune).
func (b *masterCrafterBot) runeSnapshot(ctx *Context, snap *mastercraftplan.Snapshot) {
	if !b.set.Rune.On || ctx.Session == nil {
		return
	}
	d := b.deps(ctx)
	in := &snap.Rune
	level, xp := runecraftrunner.Level(d)
	in.Known, in.Level, in.XP = level > 0, level, xp
	if level > 0 {
		b.runeLevel = level
	}
	in.Members = b.runeMembers(ctx)
	region, temple := runecraftplan.WhereIs(runecraftrunner.Here(d))
	in.Where, in.Temple = region, temple.Key
	in.PackEssence, in.PackTalisman = runecraftrunner.Essence(d), runecraftrunner.Talismans(d)
	in.AtGate = runecraftplan.NearGate(runecraftrunner.Here(d))
	in.NatureHeld = runecraftrunner.Natures(d)
	if ctx.Bus != nil && ctx.Username != "" {
		in.Carriers = len(mulebus.ReadCarriers(ctx.Bus, ctx.Username, time.Now(), 0))
	}
	if inv := ctx.Session.State().Inventory(actions.BankWithdraw); inv != nil {
		in.BankKnown = true
		in.BankEssence, in.BankTalisman, in.BankKit = runecraftrunner.Counts(inv.NonEmpty())
	} else if l := &b.ledger.Rune; l.Known() {
		in.BankKnown, in.BankEssence, in.BankTalisman, in.BankKit = true, l.BankEssence, l.BankTalisman, l.BankKit
	}
	in.CanMine, in.MineWhy = b.runeCanMine(ctx, d, in.BankKnown)
	if b.set.Rune.BuyBudget > 0 && b.set.Rune.Essence == runecraftplan.SourceAuto && level > 0 && level < b.set.Rune.LevelTo {
		need := runecraftplan.BudgetFrom(max(xp, runecraftplan.XPForLevel(max(1, level))), b.set.Rune.LevelTo, in.Members).Total
		plan, why := b.runeLotPlan(ctx, b.set.Rune, need-in.PackEssence-in.BankEssence)
		in.CanBuy, in.BuyQty, in.BuyWhy = why == "", plan.Qty, why
	}
	b.campaignSnapshot(ctx, snap)
}

// runeWorld is the levelling round's world over the live session, the mule's post read when it is known.
func (b *masterCrafterBot) runeWorld(ctx *Context, d *fleetrunner.Deps, started time.Time, level0, xp0 int) *masterRuneWorld {
	w := &masterRuneWorld{b: b, ctx: ctx, d: d, started: started, set: b.set.Rune, members: b.runeMembers(ctx),
		standTry: map[string]int{}, plans: map[string]*runecraftrunner.TemplePlan{}, level0: level0, xp0: xp0, tripStart: time.Now()}
	if p, _, ok := b.muleHere(ctx); ok {
		w.post, w.postKnown = p, true
	}
	return w
}

// runeRescue is the rescue switchTo makes when a levelling round ended with the account stranded inside a temple or the
// essence mine: the exit a round makes at its end (the portal, then the bank and the walk home), on a world of its own.
// It reports what it did and whether the account is still stranded.
func (b *masterCrafterBot) runeRescue(ctx *Context) (string, bool) {
	d := b.deps(ctx)
	level, xp := runecraftrunner.Level(d)
	w := b.runeWorld(ctx, d, time.Now(), level, xp)
	d.Travel = runecraftTravel(w.set, w.members)
	defer func() { d.Travel = nil }()
	return w.finish()
}

// runecraftRound is the levelling round: see the file comment.
func (b *masterCrafterBot) runecraftRound(ctx *Context, d *fleetrunner.Deps, started time.Time, dec mastercraftplan.Decision) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeRunecraft}
	set := b.set.Rune
	level0, xp0 := runecraftrunner.Level(d)
	if level0 == 0 {
		out.Transient, out.Why = true, "the Runecrafting level has not been read yet"
		return out
	}
	w := b.runeWorld(ctx, d, started, level0, xp0)
	if !w.members {
		w.logf("members world: no (craftMembersWorld %v, collision pack members %v): Nature would be REFUSED; Air and Body are free-world runes", b.set.MembersWorld, ctx.Pack == nil || ctx.Pack.Members)
	}
	d.Travel = runecraftTravel(set, w.members)
	defer func() { d.Travel = nil }()

	// A new round starts at the mule's post (the same walk every other round makes); one under way carries on where
	// the account stands.
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
	} else {
		w.logf("resuming from %s: %s", fleetrunner.HereIs(d), dec.Why)
	}
	w.readLiveBank()
	b.ledger.Rune.Rounds++

	plan := runecraftplan.BudgetFrom(max(xp0, runecraftplan.XPForLevel(max(1, level0))), set.LevelTo, w.members)
	snap := w.Read()
	b.logf(ctx, "runecraft: level plan - Runecraft %d -> %d needs %s essence (%s); source=%s bank %s mule %s -> %s; members world %s; teleports %s (Magic %d)",
		level0, set.LevelTo, runecraftplan.Thousands(plan.Total), plan.Describe(), set.Essence, bankWordN(snap), muleWordN(snap),
		sourceWord(snap, set), yesOK(w.members), onOff(set.Teleport), w.magic())

	res := runLevelLoop(w, func(f string, a ...any) { w.logf(f, a...) }, masterRuneMaxSteps)

	level1, xp1 := runecraftrunner.Level(d)
	if level1 > 0 {
		b.runeLevel = level1
	}
	home, stranded := "", false
	if !res.Stopped {
		home, stranded = w.finish()
	}
	out.Produced = w.essence + w.mined
	// A round that cannot take the account out of a temple or the mine is a failure for a passing reason, and switchTo does
	// not start ScarabSwarm from there (masterRoundOutcome.Stranded).
	out.Transient, out.Stranded = res.Transient || stranded, stranded
	out.Stuck = res.Stuck
	reason := res.Reason
	if home != "" {
		reason += "; " + home
	}
	out.Why = fmt.Sprintf("Runecrafting %d -> %d (+%s xp): %d trip(s), %d craft(s), %s essence -> %s runes, %s essence mined (%s)",
		level0, level1, runecraftplan.Thousands(xp1-xp0), w.trips, w.crafts, runecraftplan.Thousands(w.essence), runecraftplan.Thousands(w.runes),
		runecraftplan.Thousands(w.mined), reason)
	if res.Done {
		b.logf(ctx, "runecraft: DONE - %s", out.Why)
	}
	// Stuck rounds are paused by recordWaits; remember why for the log after a restart.
	if res.Stuck {
		b.ledger.Rune.LastStop = res.Reason
	}
	b.saveLedger(ctx)
	return out
}

// essenceSourceWord says what craftRuneEssence allows.
func essenceSourceWord(src runecraftplan.Source) string {
	switch src {
	case runecraftplan.SourceBank:
		return "this account's bank only"
	case runecraftplan.SourceMule:
		return "the mule's published stock only"
	case runecraftplan.SourceMine:
		return "its own mining only"
	}
	return "the bank, then the mule's published stock, then mining"
}

func bankWordN(s runecraftplan.Snapshot) string {
	if !s.BankKnown {
		return "?"
	}
	return runecraftplan.Thousands(s.BankEssence + s.PackEssence)
}

func muleWordN(s runecraftplan.Snapshot) string {
	if !s.MuleKnown {
		return "?"
	}
	return runecraftplan.Thousands(s.MuleEssence)
}

func yesOK(b bool) string {
	if b {
		return "ok"
	}
	return "no"
}

// sourceWord says where the first essence of the plan comes from.
func sourceWord(s runecraftplan.Snapshot, set mastercraftplan.RuneSettings) string {
	have := s.BankEssence + s.PackEssence
	switch {
	case s.BankKnown && have >= runecraftplan.MinGo:
		return "using the bank's"
	case set.Essence != runecraftplan.SourceBank && set.Essence != runecraftplan.SourceMine && s.MuleEssence >= runecraftplan.MinMuleEssence:
		return "asking the mule"
	case s.CanMine && set.Essence != runecraftplan.SourceBank && set.Essence != runecraftplan.SourceMule:
		return "mining at the " + strings.ToUpper(set.Gateway[:1]) + set.Gateway[1:]
	case !s.BankKnown:
		return "reading the bank"
	}
	return "nothing to work with"
}

// compile-time check that the live world is the interface.
var _ runeSteps = (*masterRuneWorld)(nil)
