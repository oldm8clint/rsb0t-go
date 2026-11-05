package questrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// CustomFunc executes one StepCustom. It returns false when it did not do its job; the engine
// then records the step's name in Snapshot.Failed for decide() to read.
type CustomFunc func(e *Engine, step questplan.Step) bool

// Quest is one runnable quest: the pure module plus the live executors its custom steps name.
type Quest struct {
	Module  questplan.Module
	Customs map[string]CustomFunc
	// Done, when set, replaces the quest tab as the completion test: a training rung (Agility
	// 25, Crafting 40) has no tab, so its Done reads the server's level instead, and a
	// miniquest with no tab (the Barcrawl) reads the store's record of the server's own verdict
	// (the gate guard's greeting), which is the only place that verdict survives.
	Done func(e *Engine) bool
	// Finish, when set, runs once when the quest ends complete, before Run returns: best-effort
	// tidying (Waterfall re-equips what its tomb strip banked). Its failure is logged and never
	// changes the outcome: the server already says the quest is done.
	Finish func(e *Engine)
}

// ID is the quest's record id.
func (q Quest) ID() string { return q.Module.Record.ID }

// Engine drives quests for one account. It is QuestEngine.ts's execute loop plus everything the
// Go port adds: the provisioner, sustain, the journal stage, death counting and the side store.
type Engine struct {
	D *Deps
	// Prov is the provisioner; nil gets a mule-less one on first use.
	Prov *Provisioner
	// Store is the side store, nil to remember nothing between runs. Save persists it and may
	// be nil.
	Store *questplan.Store
	Save  func()
	// SlotFor, when set, is the time one ladder run of a quest may take (a quest's own estimate
	// rather than one number for every quest); RunLadder falls back to its perQuest argument for
	// a quest it answers 0 for, or when it is nil.
	SlotFor func(id string) time.Duration
	// NarrowSlots makes a one-at-a-time quest (exclusiveQuests) hold its fleet-wide slot only
	// until its module says the account is past the shared step (Module.ExclusiveDone), not for
	// the whole run, and not at all on later runs of an account that has passed it.
	NarrowSlots bool
	// QuestFor, when set, builds the registered quest with the given id (bot.QuestFactories). It is how a ladder
	// reads a prerequisite it does not list that has no quest tab (the Barcrawl miniquest): through the quest's own
	// Done (priorDone). Without it such a prerequisite is read from the side store's SeenComplete alone.
	QuestFor func(id string) (Quest, bool)
	// Unhold, when set, lifts a questplan.HeldRungs hold for the rung it answers true for (with the reason, logged once): the opt-in
	// chain tier (stage5ThroneChain) lets Dragon Slayer in for an account whose melee levels say it can go.
	Unhold func(id string) (bool, string)
	// outside caches what QuestFor answered, by quest id (a nil entry is "not registered"); outsideSaid holds the
	// out-of-list prerequisites already logged as read complete.
	outside     map[string]*Quest
	outsideSaid map[string]bool
	// lastStuckPocket is when a stuck ladder last took the account out of a pocket it stood in (leaveStuckPocket).
	lastStuckPocket time.Time
	// Account and Build identify who and what the engine is, for the completions it records
	// (questplan.Completion): the player's name and the binary's VCS revision (bot.BuildIdentity).
	// Both may be empty (a lab harness); the completion is still recorded and logged.
	Account string
	Build   string

	// Per-run state. The stage is re-read from the journal after every step that advanced the
	// world, because a journal read opens a modal and so is not free.
	failed map[string]bool
	// blocked is the subset of failed that is a provision kit whose every source was dead (the
	// round ended BLOCKED), as opposed to one that failed on the way (a walk that timed out):
	// waiting is right for the first, trying again for the second.
	blocked map[string]bool
	ran     map[string]bool
	// runStart is when the current Run began. A bank view older than it is not trusted by the
	// snapshot: between two runs the Stage 5 trainer banks what it does not keep (quest items
	// included), so the previous run's reading says a thread is in the pack that is in the bank.
	runStart time.Time
	// runDeadline is when the current Run ends (RunLeft); trained is the experience its custom steps reported
	// gaining (NoteTrained). Both are reset by Run.
	runDeadline time.Time
	trained     int
	// yielded is a custom step's request to end the Run now (YieldRun).
	yielded bool
	// recover is set by a death: before the next step the engine draws a travel float from
	// the nearest bank, because a respawned account holds no coins for the ship fares back.
	recover bool
	// parkReq is a custom step's request to park the whole quest for a long skip (RequestPark), served by Run once the
	// step returns.
	parkReq    *parkRequest
	stage      int
	stageKnown bool
	stageFresh bool
	quest      Quest
}

// parkRequest is what RequestPark records.
type parkRequest struct {
	reason string
	skip   time.Duration
}

// RequestPark asks Run to park the quest for skip once the running custom step returns, in the side store (so a
// watchdog restart, a relaunch or the next ladder pass does not re-enter it), with reason in the log. For a step that
// has learned the account cannot do this now and that trying again in five minutes changes nothing: the Titan that did
// not fall in 25 minutes (live 6 Oct 2026, Problem and Howdy Folk, four watchdog restarts into the same fight).
func (e *Engine) RequestPark(reason string, skip time.Duration) {
	e.parkReq = &parkRequest{reason: reason, skip: skip}
}

// RunLeft is how long the current Run has left, so a custom step that works for as long as it is given (a training
// rung) can end with the run and not past it; 0 outside a Run.
func (e *Engine) RunLeft() time.Duration {
	if e.runDeadline.IsZero() {
		return 0
	}
	return max(time.Until(e.runDeadline), 0)
}

// YieldRun ends the current Run as a timeout does, once the running custom step returns, without waiting out the rest of
// its slot: for a training rung that has nothing left to do this slot (no bone to be had and too little time to walk to
// the cows), so that the ladder goes on to the next quest now and the engine does not call the step again and again until
// the deadline.
func (e *Engine) YieldRun() { e.yielded = true }

// NoteTrained tells Run that a training step gained xp experience in a skill: a run of a rung that gained
// ProgressTrainXP counts as having moved the quest on, so its timeout waits the shortest backoff and not the
// escalating one (Store.NoteTimeout). A rung's journal cannot say it; its experience can.
func (e *Engine) NoteTrained(xp int) {
	if xp > 0 {
		e.trained += xp
	}
}

// Deps exposes the dependency set to custom executors.
func (e *Engine) Deps() *Deps { return e.D }

// Current is the quest being run.
func (e *Engine) Current() Quest { return e.quest }

// Provisioner exposes the provisioner to custom executors (a custom step may provision a kit
// of its own mid-step).
func (e *Engine) Provisioner() *Provisioner { return e.prov() }

func (e *Engine) prov() *Provisioner {
	if e.Prov == nil {
		e.Prov = &Provisioner{}
	}
	if e.Prov.Bank == nil {
		e.Prov.Bank = &BankView{}
	}
	return e.Prov
}

func (e *Engine) save() {
	if e.Save != nil {
		e.Save()
	}
}

// Snapshot reads the live session into the snapshot decide() takes.
func (e *Engine) Snapshot(noProgress int) questplan.Snapshot {
	d := e.D
	m := e.quest.Module
	snap := BuildSnapshot(d, m.Record.Name, noProgress)
	pv := e.prov()
	if pv.Bank.Known && !pv.Bank.At.Before(e.runStart) {
		snap.BankKnown = true
		snap.BankIDs = pv.Bank.Counts
		snap.Bank = map[string]int{}
		for id, n := range pv.Bank.Counts {
			snap.Bank[itemName(d, id)] += n
		}
		snap.BankCoins = pv.Bank.Counts[questplan.CoinsID]
	}
	snap.HP, snap.MaxHP = Hitpoints(d)
	if d.Pair != nil {
		snap.Pair = d.Pair.State()
	}
	snap.Levels = BaseLevels(d)
	snap.QuestPoints = QuestPoints(d)
	if e.Store != nil {
		snap.Deaths = e.Store.Record(m.Record.ID).Deaths
	}
	snap.Failed = map[string]bool{}
	for k, v := range e.failed {
		snap.Failed[k] = v
	}
	snap.Blocked = map[string]bool{}
	for k, v := range e.blocked {
		snap.Blocked[k] = v
	}
	snap.Ran = map[string]bool{}
	for k, v := range e.ran {
		snap.Ran[k] = v
	}
	// The soft prerequisites' tabs (Priest in Peril's essence mine needs Rune Mysteries), read
	// fresh every tick: the quest tab is a cached colour, not a journal read.
	snap.QuestsDone = map[string]bool{}
	for _, sp := range questplan.LadderFacts[m.Record.ID].SoftPrereqs {
		snap.QuestsDone[sp.ID] = ReadStatus(d, questplan.QuestTabName(sp.ID)) == questplan.StatusComplete
	}
	if m.ReadStage != nil && snap.Journal == questplan.StatusInProgress {
		if !e.stageFresh {
			e.readStage()
		}
		snap.Stage, snap.StageKnown = e.stage, e.stageKnown
	}
	return snap
}

// readStage opens the journal and parses it with the module's ReadStage.
func (e *Engine) readStage() {
	lines, err := ReadJournal(e.D, e.quest.Module.Record.Name)
	if err != nil {
		e.D.logf("[%s] reading the journal: %v", e.quest.ID(), err)
		e.stageKnown = false
		return
	}
	e.stage, e.stageKnown = e.quest.Module.ReadStage(lines)
	e.stageFresh = e.stageKnown
}

// StageIs reports whether the last journal reading was exactly stage n (re-reading it first when
// a step has made the reading stale).
func (e *Engine) StageIs(n int) bool {
	if e.quest.Module.ReadStage == nil {
		return false
	}
	if !e.stageFresh {
		e.readStage()
	}
	return e.stageKnown && e.stage == n
}

// InvalidateStage makes the next snapshot re-read the journal; a custom executor calls it when
// it changed the stage without the engine seeing a step end.
func (e *Engine) InvalidateStage() { e.stageFresh = false }

// Run drives one quest to completion, to parking, or to the budget.
//
// This is QuestEngine.execute's single-quest core. Parking, deaths and the last step are
// written to the side store; completion is read from the quest tab, never from the store.
func (e *Engine) Run(q Quest, budget time.Duration) Result {
	d := e.D
	e.quest = q
	e.failed = map[string]bool{}
	e.blocked = map[string]bool{}
	e.ran = map[string]bool{}
	e.parkReq = nil
	e.prov().TakeTrain() // a train need left by an earlier run is not this quest's
	d.stalls.reset()
	e.stageFresh = false
	e.runStart = time.Now()
	e.runDeadline = e.runStart.Add(budget)
	e.trained = 0
	e.yielded = false
	id := q.ID()
	if e.Store != nil {
		e.Store.NoteAttempt(id, time.Now())
		e.save()
	}

	var watchdog questplan.Watchdog
	prog := &progressTracker{}
	slotFreed := false
	deadline := time.Now().Add(budget)
	lastStep := ""
	lastCustom := "" // the custom step the last tick ran, for Module.StepDeathCaps
	waitKey, waitCount := "", 0
	waitAny := 0 // consecutive wait steps of ANY reason: two reasons that alternate never reach WaitPark on one key
	var danger dangerWatch
	travelBlocks := 0 // consecutive provisioning rounds blocked by an unreachable source
	sus := q.Module.Sustain
	// Where a lost fight walks to, and whether a fight may touch auto-retaliate. A retreat left
	// by an earlier run is not this quest's.
	d.retreatBank, d.retreatBankSet = q.Module.Bank, q.Module.BankSet
	d.noRetaliate = q.Module.NoRetaliate
	tools := moduleTools(d, q.Module)
	d.toolProtect.Store(&tools)
	d.takeRetreats()
	// The sustain runs beside every step, not only between them: a long walk through a
	// dungeon (Waterfall's Tree Gnome Village leg) is where the account gets hit.
	stopEating := make(chan struct{})
	go eatLoop(d, sus, stopEating)
	defer close(stopEating)
	if q.Module.NoRetaliate {
		if err := actions.SetAutoRetaliate(d.Session, false); err != nil {
			d.logf("[%s] auto-retaliate off: %v", id, err)
		}
		defer func() { _ = actions.SetAutoRetaliate(d.Session, true) }()
	}

	park := func(reason string, tick int, desc string) Result {
		d.logf("[%s] parking: %s", id, reason)
		if e.Store != nil {
			e.Store.NotePark(id, reason, time.Now())
			e.Store.Record(id).LastStep = desc
			e.save()
		}
		return Result{Outcome: OutcomeParked, Reason: reason, Ticks: tick, LastStep: desc}
	}

	// A death is looked for over the whole tick, not only inside a step: the second Waterfall
	// EMPTY lab run died while the snapshot was reading the journal between two steps, the
	// death was never noticed, and the respawned account walked off with no travel float.
	deathMark := time.Now()
	died := func(tick int, desc string) (Result, bool) {
		if !DiedSince(d, deathMark) {
			deathMark = time.Now()
			return Result{}, false
		}
		deathMark = time.Now()
		deaths := 1
		if e.Store != nil {
			deaths = e.Store.NoteDeath(id, time.Now())
			e.save()
		}
		d.logf("[%s] DIED during %q (death %d for this quest)", id, desc, deaths)
		watchdog.Reset()
		// Everything carried is gone or suspect: every kit is re-provisioned from scratch.
		e.failed = map[string]bool{}
		e.blocked = map[string]bool{}
		e.ran = map[string]bool{}
		e.recover = true
		if c, ok := q.Module.StepDeathCaps[lastCustom]; ok && lastCustom != "" && c.Max > 0 {
			n := 1
			if e.Store != nil {
				n = e.Store.NoteStepDeath(id, lastCustom, time.Now(), c.Window)
				e.save()
			}
			if n >= c.Max {
				reason := fmt.Sprintf("%d death(s) on %q inside %s (its cap is %d): skipping the quest for %s", n, lastCustom, c.Window, c.Max, c.Skip)
				d.logf("[%s] parking: %s", id, reason)
				if e.Store != nil {
					e.Store.NoteParkFor(id, reason, time.Now(), c.Skip)
					e.Store.Record(id).LastStep = desc
					e.save()
				}
				return Result{Outcome: OutcomeParked, Reason: reason, Ticks: tick, LastStep: desc}, true
			}
		}
		if max := q.Module.MaxDeaths; max > 0 && deaths >= max {
			// A day, not the escalating 5-40 minute Backoff: live on 4-5 Oct 2026 mackerel reached
			// Grand Tree's cap of 2 at 00:29, was back at the Black Demon at 00:37 and died a third
			// time at 00:42. A quest at its cap is tried at most once a day after that (the same
			// day the warlord's step cap parks for).
			reason := fmt.Sprintf("%d death(s), the quest's cap is %d: skipping the quest for %s", deaths, max, MaxDeathsSkip)
			d.logf("[%s] parking: %s", id, reason)
			if e.Store != nil {
				e.Store.NoteParkFor(id, reason, time.Now(), MaxDeathsSkip)
				e.Store.Record(id).LastStep = desc
				e.save()
			}
			return Result{Outcome: OutcomeParked, Reason: reason, Ticks: tick, LastStep: desc}, true
		}
		// The respawn takes a few ticks to land.
		d.Delay(4 * time.Second)
		return Result{}, false
	}

	// A fight the account was losing and walked away from (fightloop.go retreatFromFight) is a
	// near-death: counted against the quest like a death is, so a quest that keeps losing parks
	// instead of killing the account. The first parks for the escalating Backoff (the account is low
	// on food and the next run provisions again); the NearDeathsToPark-th inside NearDeathWindow
	// skips the quest for a day.
	lostFight := func(tick int, desc string) (Result, bool) {
		n, why := d.takeRetreats()
		if n == 0 {
			return Result{}, false
		}
		count := n
		if e.Store != nil {
			for i := 0; i < n; i++ {
				count = e.Store.NoteNearDeath(id, time.Now(), questplan.NearDeathWindow)
			}
			e.save()
		}
		cap := q.Module.MaxNearDeaths
		if cap <= 0 {
			cap = questplan.NearDeathsToPark
		}
		d.logf("[%s] LOST A FIGHT during %q and walked away (near-death %d, the cap is %d inside %s): %s", id, desc, count, cap, questplan.NearDeathWindow, why)
		if count >= cap {
			reason := fmt.Sprintf("%d lost fight(s) inside %s, the quest's cap is %d: skipping the quest for %s", count, questplan.NearDeathWindow, cap, MaxDeathsSkip)
			d.logf("[%s] parking: %s", id, reason)
			if e.Store != nil {
				e.Store.NoteParkFor(id, reason, time.Now(), MaxDeathsSkip)
				e.Store.Record(id).LastStep = desc
				e.save()
			}
			return Result{Outcome: OutcomeParked, Reason: reason, Ticks: tick, LastStep: desc}, true
		}
		return park(fmt.Sprintf("lost a fight: %s", why), tick, desc), true
	}

	// The fail-safe every quest inherits: hitpoints falling while the quest makes no progress (a wait
	// step, or a step that failed) walk the account off, and then park the quest, rather than stand in
	// the danger (live 6 Nov 2026: a Waterfall run died twice at East Ardougne market with no food).
	dangerCheck := func(stalled bool, hp, maxHP, tick int, desc string) (Result, bool) {
		switch danger.Note(hp, maxHP, stalled) {
		case dangerWalkAway:
			here := currentTile(d)
			dest, name := d.retreatDest(here)
			d.logf("[%s] hitpoints falling (%d of %d) while %q makes no progress: walking away to %s", id, hp, maxHP, desc, name)
			if !Travel(d, dest, 3) {
				d.logf("[%s] could not reach %s from %d,%d,%d", id, name, here.X, here.Z, here.Level)
			}
		case dangerPark:
			reason := fmt.Sprintf("hitpoints keep falling (%d of %d) while %q makes no progress: not standing in it", hp, maxHP, desc)
			d.logf("[%s] parking for %s: %s", id, DangerParkFor, reason)
			if e.Store != nil {
				e.Store.NoteParkFor(id, reason, time.Now(), DangerParkFor)
				e.Store.Record(id).LastStep = desc
				e.save()
			}
			return Result{Outcome: OutcomeParked, Reason: reason, Ticks: tick, LastStep: desc}, true
		}
		return Result{}, false
	}

	for tick := 1; time.Now().Before(deadline) && !e.yielded; tick++ {
		if !WaitForGuardian(d) {
			return Result{Outcome: OutcomeStopped, Ticks: tick, LastStep: lastStep}
		}
		if res, stop := died(tick, lastStep); stop {
			return res
		}
		EatIfNeeded(d, sus.FoodIDs, sus.EatAt)
		if e.recover {
			e.recover = false
			e.recoverAfterDeath()
		}
		snap := e.Snapshot(watchdog.Count())
		if !slotFreed && e.freeSlotIfPast(snap) {
			slotFreed = true
			d.logf("[%s] past its one-at-a-time step: the fleet-wide slot is free for the next account", id)
		}
		step := q.Module.Decide(snap)
		desc := step.Describe()
		lastCustom = ""
		if step.Kind == questplan.StepCustom {
			lastCustom = step.Custom
		}
		prog.note(snap, step, desc)
		if desc != lastStep {
			stage := "?"
			if snap.StageKnown {
				stage = fmt.Sprint(snap.Stage)
			}
			d.logf("[%s] %s (journal %s, stage %s, hp %d/%d, %d free slots)", id, desc, snap.Journal, stage, snap.HP, snap.MaxHP, snap.FreeSlots)
			lastStep = desc
			if e.Store != nil {
				e.Store.Record(id).LastStep = desc
			}
		}

		if step.Kind == questplan.StepDone {
			if e.Store != nil {
				e.Store.ClearPark(id, time.Now())
				rec := e.Store.Record(id)
				// A quest with a Done of its own has no tab to read (the Barcrawl miniquest): its Done reads this very
				// flag, which the custom step that handed the card in has just set, and a journal that is "unknown" by
				// construction must not wipe it (the ladder read "barcrawl not complete" after a finished tour).
				rec.SeenComplete = snap.Journal == questplan.StatusComplete || (q.Done != nil && rec.SeenComplete)
			}
			if endsTheQuest(snap.Journal) {
				e.recordCompletion(id, tick)
			}
			if e.Store != nil {
				e.save()
			}
			if q.Finish != nil {
				q.Finish(e)
			}
			return Result{Outcome: OutcomeComplete, Ticks: tick, LastStep: desc}
		}

		if step.AdvancesWorld() {
			count := watchdog.Note(questplan.ProgressSignature(snap))
			if count == questplan.NoProgressWarn {
				d.logf("[%s] %d ticks with nothing changing on %q", id, count, desc)
			}
			if count >= questplan.NoProgressPark {
				return park(fmt.Sprintf("no progress for %d ticks on %q", count, desc), tick, desc)
			}
		}

		if step.Kind == questplan.StepWait {
			if step.ParkFor > 0 {
				// A gate that will not open for hours (a fight the account cannot survive): park for that
				// long now, not after WaitPark ticks and for the 5 minute Backoff.
				reason := step.Reason
				d.logf("[%s] parking for %s: %s", id, step.ParkFor, reason)
				if e.Store != nil {
					e.Store.NoteParkFor(id, reason, time.Now(), step.ParkFor)
					e.Store.Record(id).LastStep = desc
					e.save()
				}
				return Result{Outcome: OutcomeParked, Reason: reason, Ticks: tick, LastStep: desc}
			}
			if desc == waitKey {
				waitCount++
			} else {
				waitKey, waitCount = desc, 1
			}
			waitAny++
			if waitCount >= WaitPark {
				return park(fmt.Sprintf("waited %d ticks on %q without the world ever saying more", waitCount, desc), tick, desc)
			}
			if waitAny >= WaitParkAny {
				return park(fmt.Sprintf("waited %d ticks in a row on changing reasons (last %q) without the world ever saying more", waitAny, desc), tick, desc)
			}
			if res, stop := dangerCheck(true, snap.HP, snap.MaxHP, tick, desc); stop {
				return res
			}
			ClearLeftoverModal(d)
			e.stageFresh = false
			if !d.Delay(1200 * time.Millisecond) {
				return Result{Outcome: OutcomeStopped, Ticks: tick, LastStep: desc}
			}
			continue
		}
		waitKey, waitCount, waitAny = "", 0, 0

		mark := time.Now()
		ok := e.execute(step)
		e.stageFresh = false
		hpNow, maxNow := Hitpoints(d)
		if res, stop := dangerCheck(!ok, hpNow, maxNow, tick, desc); stop {
			return res
		}
		// A provisioning round that ended waiting for a LEVEL (questplan.PhaseTrain) ends the quest's run: the stint is not
		// worked inside the quest's slot, which a crowded quest would hold for hours (the completeness critic's item 19). The
		// quest parks on "needs:<skill>:<level>", RunLadder releases the slot when Run returns, and the ladder ends the park
		// the moment the level is there. The need is taken from the provisioner either way, so a stale one never parks a
		// later, unrelated failure.
		if train := e.prov().TakeTrain(); !ok && len(train) > 0 && e.parkReq == nil {
			e.RequestPark(questplan.TrainReason(train), questplan.TrainParkFor)
		}
		// A kit whose mule ask a world errand carries is not a dead end: the quest steps aside for the errand, and the next run
		// finds the items in the pack or the bank instead of waiting on the blocked kit.
		if why := e.prov().TakeErrand(); !ok && why != "" && e.parkReq == nil {
			e.RequestPark("waiting for the mule's kit, carried by a world errand: "+why, ErrandParkFor)
		}
		if key := failKey(step); key != "" {
			if ok {
				delete(e.failed, key)
				delete(e.blocked, key)
				e.ran[key] = true
				travelBlocks = 0
			} else {
				e.failed[key] = true
				if step.Kind == questplan.StepProvision && e.prov().LastBlocked {
					e.blocked[key] = true
				} else {
					delete(e.blocked, key)
				}
				// A kit whose every source is unreachable is not retried every few seconds for the whole slot:
				// after ProvisionTravelParkRounds such rounds the quest parks (live 7 Oct 2026, Imp Catcher).
				if step.Kind == questplan.StepProvision && e.prov().LastTravelBlocked {
					travelBlocks++
					if travelBlocks >= ProvisionTravelParkRounds {
						return park(fmt.Sprintf("%d provisioning rounds in a row of %q found every source unreachable", travelBlocks, step.KitName), tick, desc)
					}
				} else {
					travelBlocks = 0
				}
			}
		}
		if res, stop := died(tick, desc); stop {
			return res
		}
		if res, stop := lostFight(tick, desc); stop {
			return res
		}
		// A quest whose walks keep ending stranded or unreachable is parked, not retried for the whole slot
		// (live 7 Oct 2026: the Wizards' Tower door, 32 laps by one account).
		if why := d.stalls.parkReason(); why != "" {
			reason := "its walks cannot get there: " + why
			d.logf("[%s] parking for %s: %s", id, TravelParkFor, reason)
			if e.Store != nil {
				e.Store.NoteParkFor(id, reason, time.Now(), TravelParkFor)
				e.Store.Record(id).LastStep = desc
				e.save()
			}
			return Result{Outcome: OutcomeParked, Reason: reason, Ticks: tick, LastStep: desc}
		}
		if req := e.parkReq; req != nil {
			e.parkReq = nil
			d.logf("[%s] parking for %s: %s", id, req.skip, req.reason)
			if e.Store != nil {
				e.Store.NoteParkFor(id, req.reason, time.Now(), req.skip)
				e.Store.Record(id).LastStep = desc
				e.save()
			}
			return Result{Outcome: OutcomeParked, Reason: req.reason, Ticks: tick, LastStep: desc}
		}
		// A step that failed while something is hitting the account does not get retried into
		// the same beating: the second and third Waterfall EMPTY lab runs stood at Golrie's gate
		// ("I can't reach that!") while the dungeon's zombies and hobgoblins wore them down.
		if !ok && !q.Module.NoRetaliate && !DiedSince(d, mark) {
			if npc, hit := Attacker(d); hit && fightable(d, npc) {
				d.logf("[%s] %s is attacking while %q fails: fighting back", id, npc.Name, desc)
				FightBack(d, npc, sus)
				if res, stop := died(tick, desc); stop {
					return res
				}
				if res, stop := lostFight(tick, desc); stop {
					return res
				}
			}
		}
		if !d.Delay(600 * time.Millisecond) {
			return Result{Outcome: OutcomeStopped, Ticks: tick, LastStep: desc}
		}
	}
	prog.TrainedXP = e.trained
	return Result{Outcome: OutcomeTimeout, Ticks: 0, LastStep: lastStep, Progressed: prog.Moved()}
}

// failKey is the Snapshot.Failed key a step's failure is recorded under, "" for steps whose
// failure the watchdog alone judges.
func failKey(s questplan.Step) string {
	switch s.Kind {
	case questplan.StepCustom:
		return s.Custom
	case questplan.StepProvision:
		return "provision:" + s.KitName
	}
	return ""
}

// Execute runs one step with the engine's state. A custom executor composes generic steps with
// it (a fight, a use-on, a provision) instead of re-implementing them.
func (e *Engine) Execute(step questplan.Step) bool { return e.execute(step) }

// execute runs one step with the engine's state.
func (e *Engine) execute(step questplan.Step) bool {
	d := e.D
	if why := questplan.ValidateStep(step); why != "" {
		d.logf("refusing a malformed step: %s", why)
		return false
	}
	m := e.quest.Module
	if step.Bare {
		// Every bank visit of this step (a deposit, a provisioning round, a withdrawal's room-making) is bare.
		defer d.BarePack()()
	}
	switch step.Kind {
	case questplan.StepProvision:
		ok, why := e.prov().Provision(d, step.KitName, step.Kit, e.keepFunc(step), e.bankFor(step))
		if !ok {
			d.logf("[%s] provisioning %s failed: %s", e.quest.ID(), step.KitName, why)
		}
		return ok
	case questplan.StepWithdraw:
		return e.withdraw(step)
	case questplan.StepDeposit:
		bank, ok := OpenBankAt(d, e.bankFor(step))
		if !ok {
			return false
		}
		DepositExcept(d, bank, e.keepFunc(step))
		e.prov().Bank.Note(BankCounts(bank))
		_ = bank.Close()
		return true
	case questplan.StepScanBank:
		bank, ok := OpenBankAt(d, e.bankFor(step))
		if !ok {
			return false
		}
		e.prov().Bank.Note(BankCounts(bank))
		_ = bank.Close()
		return true
	case questplan.StepFight:
		return execFight(d, step, m.Sustain)
	case questplan.StepCustom:
		fn := e.quest.Customs[step.Custom]
		if fn == nil {
			d.logf("[%s] no executor registered for the custom step %q", e.quest.ID(), step.Custom)
			return false
		}
		return fn(e, step)
	}
	return ExecuteStep(d, step, m.Hops)
}

// keepFunc is what a bank visit may leave in the pack: the step's keep lists and the module's
// tools, by name (substring, as TS depositPlan) and by id.
func (e *Engine) keepFunc(step questplan.Step) func(id int) bool {
	m := e.quest.Module
	ids := map[int]bool{}
	for _, id := range step.KeepIDs {
		ids[id] = true
	}
	for _, id := range m.ToolIDs {
		ids[id] = true
	}
	names := append(append([]string(nil), step.Keep...), m.Tools...)
	return func(id int) bool {
		if ids[id] {
			return true
		}
		name := ObjName(e.D, id)
		n := strings.ToLower(name)
		for _, k := range names {
			if k != "" && strings.Contains(n, strings.ToLower(k)) {
				return true
			}
		}
		// Deps.KeepInPack: what the account keeps for itself whatever the quest's own lists say (the run staller), except
		// on a step that is a bare-pack stretch (Step.Bare): the place it goes next refuses that kit.
		return !step.Bare && e.D.KeepInPack != nil && e.D.KeepInPack(name)
	}
}

// bankFor is a step's bank, else the module's, else the provisioner's default.
func (e *Engine) bankFor(step questplan.Step) tile.Tile {
	if step.BankSet {
		return step.Bank
	}
	if e.quest.Module.BankSet {
		return e.quest.Module.Bank
	}
	return e.prov().DefaultBank
}

func (e *Engine) withdraw(step questplan.Step) bool {
	d := e.D
	bank, ok := OpenBankAt(d, e.bankFor(step))
	if !ok {
		return false
	}
	defer func() { _ = bank.Close() }()
	counts := BankCounts(bank)
	e.prov().Bank.Note(counts)
	all := true
	var draws []Draw
	for _, it := range step.Items {
		id := it.ID
		if id == 0 {
			id, _ = ResolveItem(d, 0, it.Name)
		}
		if id == 0 || counts[id] == 0 {
			d.logf("the bank holds no %s", it.Name)
			all = false
			continue
		}
		draws = append(draws, Draw{ID: id, N: it.Qty})
	}
	// A full pack cannot take the withdrawal (live 13 Oct 2026: "withdraw Excalibur x1" with 0 free
	// slots, eight ticks of nothing changing, then a park): bank what the step does not keep first.
	if withdrawNeedsRoom(FreeSlots(d), len(draws)) {
		d.logf("the pack has %d free slots for %d withdrawals: banking what the step does not keep first", FreeSlots(d), len(draws))
		DepositExcept(d, bank, e.keepFunc(step))
		e.prov().Bank.Note(BankCounts(bank))
	}
	// Every line in one burst, confirmed once (WithdrawExactMany).
	for i, got := range WithdrawExactMany(d, bank, draws) {
		if got < draws[i].N {
			all = false
		}
	}
	e.prov().Bank.Note(BankCounts(bank))
	return all
}

// withdrawNeedsRoom reports whether a withdrawal of draws lines cannot be placed in a pack with free
// slots free (each line is taken to need a slot: stackables that already sit in the pack are the
// rare case, and a deposit that was not needed costs a second).
func withdrawNeedsRoom(free, draws int) bool { return draws > 0 && free < draws }

// ---------------------------------------------------------------------------
// The ladder
// ---------------------------------------------------------------------------

// LadderOutcome is how RunLadder ended.
type LadderOutcome string

const (
	// LadderDone means every quest in the ladder reads complete.
	LadderDone LadderOutcome = "done"
	// LadderStuck means nothing left is runnable now: blocked or parked.
	LadderStuck LadderOutcome = "stuck"
	// LadderStopped means the bot was asked to stop.
	LadderStopped LadderOutcome = "stopped"
	// LadderBudget means the overall budget ran out.
	LadderBudget LadderOutcome = "budget"
)

// LadderResult is RunLadder's answer.
type LadderResult struct {
	Outcome LadderOutcome
	// Completed are the quests this call saw turn (or already read) complete.
	Completed []string
	// Blocked is each unrunnable quest's reasons.
	Blocked map[string][]string
	// Progressed is whether anything in the pass moved a quest on: one completed, or one ran its
	// slot out having advanced (Result.Progressed). A pass that ended out of budget or stuck but
	// Progressed was working, not wedged: its caller retries it soon instead of treating it as a
	// failure to give up on.
	Progressed bool
	// SlotBusy are the quests the pass could not start because another account held their slot (their Blocked reason is
	// the busy one), in ladder order: what a caller waiting for a free slot looks at the table for (SlotsFree).
	SlotBusy []string
	// ParkedUntil is when each quest the pass found parked may be tried again (the side store's backoff), so a caller can
	// come back when the park ends and not at the end of a long fixed wait.
	ParkedUntil map[string]time.Time
}

// completedQuests reads every ladder quest's tab.
func (e *Engine) completedQuests(qs []Quest) map[string]bool {
	out := map[string]bool{}
	for _, q := range qs {
		if q.Done != nil {
			out[q.ID()] = q.Done(e)
			continue
		}
		if ReadStatus(e.D, q.Module.Record.Name) == questplan.StatusComplete {
			out[q.ID()] = true
		}
	}
	// A prerequisite outside the list is read from its own tab: a ladder of The Holy Grail
	// alone (the questproof EMPTY lab run grail-empty-1, Merlin's Crystal complete on the
	// server) stayed blocked on "prerequisite quest not complete: merlin" for its whole budget.
	// One with no tab (the Barcrawl) is read through its own Done or the side store (priorDone).
	for _, id := range prereqsOutside(qs, out) {
		if e.priorDone(id) {
			out[id] = true
		}
	}
	return out
}

// priorDone reads a quest the ladder at hand does not list: a prerequisite of one it does, or a quest a park waits for.
// A quest with a tab is read from the tab, the server's own verdict (the side store's SeenComplete is only a hint about
// it and never a gate). A quest with none, the Barcrawl miniquest whose progress is not transmitted, cannot be read from
// the server at all, so it is read as a ladder that lists it reads it: through its own Done when QuestFor knows it,
// else from the side store's record of the verdict its step took. Live 15-19 Oct 2026 the unproven tail, which lists
// Scorpion Catcher and Horror from the Deep but not the proven Barcrawl, held them at "prerequisite quest not
// complete: barcrawl" on 24 to 51 accounts a day (1,911 and 1,655 events in 15 days) though the head had finished the crawl.
// The read creates no record in the store.
func (e *Engine) priorDone(id string) bool {
	if name := questplan.QuestTabName(id); name != "" && HasQuestTab(name) {
		return ReadStatus(e.D, name) == questplan.StatusComplete
	}
	done, how := false, ""
	if q, ok := e.outsideQuest(id); ok && q.Done != nil {
		done, how = q.Done(e), "its own Done"
	} else if e.Store != nil {
		r := e.Store.Quests[id]
		done, how = r != nil && r.SeenComplete, "the side store's record"
	}
	if done && !e.outsideSaid[id] {
		if e.outsideSaid == nil {
			e.outsideSaid = map[string]bool{}
		}
		e.outsideSaid[id] = true
		e.D.logf("[ladder] %s is not in this ladder and has no quest tab: read complete from %s", id, how)
	}
	return done
}

// outsideQuest is the registered quest with the given id (QuestFor), built once per engine.
func (e *Engine) outsideQuest(id string) (Quest, bool) {
	if e.QuestFor == nil {
		return Quest{}, false
	}
	if q, seen := e.outside[id]; seen {
		if q == nil {
			return Quest{}, false
		}
		return *q, true
	}
	if e.outside == nil {
		e.outside = map[string]*Quest{}
	}
	q, ok := e.QuestFor(id)
	if !ok {
		e.outside[id] = nil
		return Quest{}, false
	}
	e.outside[id] = &q
	return q, true
}

// prereqsOutside lists the prerequisite quest ids of qs that are neither in qs nor already in
// done, each once, in order.
func prereqsOutside(qs []Quest, done map[string]bool) []string {
	in := map[string]bool{}
	for _, q := range qs {
		in[q.ID()] = true
	}
	var out []string
	for _, q := range qs {
		for _, id := range append(append([]string(nil), q.Module.Record.Requirements.Quests...), questplan.LadderFacts[q.ID()].Prereqs...) {
			if !in[id] && !done[id] {
				in[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// RunLadder runs quests in order: each pass picks the first quest that is not complete (read
// from the quest tab), not parked (the side store's backoff) and whose requirements hold
// (levels, quest points and prerequisite quests, read live), and runs it with perQuest. It
// returns when every quest is complete, when nothing is runnable, or when the budget is spent.
//
// A quest whose HARD level gate (questplan.LadderFacts: a server gate, the owner's rule, a module
// gate) is not met never runs, whatever its Record declares and whether or not its park has run
// out: it is held back, with the gate in Blocked and in the log, and the quests after it in the
// list go on. A training rung that cannot run (not in the list, parked, over its slot) is not
// skipped for the quest behind it: the quest stays held until the level is really there.
func (e *Engine) RunLadder(qs []Quest, perQuest, budget time.Duration) LadderResult {
	d := e.D
	deadline := time.Now().Add(budget)
	res := LadderResult{Blocked: map[string][]string{}, ParkedUntil: map[string]time.Time{}}
	ran := 0 // quests this pass has started
	inLadder := map[string]bool{}
	for _, q := range qs {
		inLadder[q.ID()] = true
	}
	heldSaid := map[string]bool{} // gate holds already logged this call: quest id and reasons
	for time.Now().Before(deadline) {
		done := e.completedQuests(qs)
		player := questplan.PlayerState{QuestPoints: QuestPoints(d), SkillLevels: BaseLevels(d), CompletedQuests: done}
		var next *Quest
		res.Blocked = map[string][]string{}
		res.SlotBusy, res.ParkedUntil = nil, map[string]time.Time{}
		for i := range qs {
			q := qs[i]
			if done[q.ID()] {
				continue
			}
			// What the quest's record and the ladder's prerequisites say, read live.
			requirements := func() []string {
				var reasons []string
				for _, r := range questplan.CheckRequirements(q.Module.Record, player) {
					if !r.OK {
						reasons = append(reasons, r.Reason)
					}
				}
				// The ladder's own hard prerequisites (questplan.LadderFacts Prereqs): a quest whose
				// route needs another though its server record does not. Horror from the Deep and Scorpion
				// Catcher lie behind the Barbarian Outpost gate, which only Alfred Grimhand's Barcrawl
				// opens: live 6 Oct 2026, spare103 and emod spent every Horror slot at that gate ("answered
				// with a dialogue instead of opening", 30+ times each) with Barcrawl parked.
				for _, p := range questplan.LadderFacts[q.ID()].Prereqs {
					if !done[p] {
						reasons = append(reasons, "prerequisite quest not complete: "+p)
					}
				}
				return reasons
			}
			// The ladder's own hard level gates, before the park: a quest the account cannot do yet is
			// not retried when its backoff runs out. Live 14 Oct 2026, twenty-two accounts at
			// Woodcutting 2 and Fletching 1 ran the Fremennik Trials (its record declares no level), walked
			// to Rellekka over the basalt rocks and parked at Olaf's swaying tree, which wants Woodcutting 40.
			if holds := questplan.LadderHolds(q.Module.Record, player); len(holds) > 0 {
				res.Blocked[q.ID()] = append(questplan.HoldReasons(holds), requirements()...)
				e.noteLadderHold(q.ID(), holds, inLadder, heldSaid)
				continue
			}
			if e.Store != nil {
				if parked, why := e.Store.Parked(q.ID(), time.Now()); parked {
					if questplan.ParkSuperseded(q.ID(), why) {
						// A park written by a build whose cause has since been fixed (the old Titan fight's
						// 12 hours, live 13 Oct 2026): the new build gets its try now, not tomorrow.
						d.logf("[ladder] %s: clearing a park an older build wrote (%s)", q.ID(), why)
						e.Store.ClearPark(q.ID(), time.Now())
						e.save()
					} else if skill, level, ok := questplan.ParseNeedsPark(why); ok && player.SkillLevels[skill] >= level {
						// A park that waited for a stint (the provisioner's train need) ends when the level is there, not
						// when its two hours do.
						d.logf("[ladder] %s: %s %d is reached, so the park waiting on it ends (%s)", q.ID(), skill, level, why)
						e.Store.ClearPark(q.ID(), time.Now())
						e.save()
					} else if dep := questplan.ParkAwaitedQuest(q.ID(), why); dep != "" && (done[dep] || e.priorDone(dep)) {
						// A park that was waiting for another quest (Priest in Peril's essence, for Rune
						// Mysteries) ends when that quest reads complete, not when its backoff does.
						d.logf("[ladder] %s: %s is complete, so the park waiting on it ends (%s)", q.ID(), dep, why)
						e.Store.ClearPark(q.ID(), time.Now())
						e.save()
					} else {
						res.Blocked[q.ID()] = []string{"parked: " + why}
						if r := e.Store.Quests[q.ID()]; r != nil && r.BackoffUntil > 0 {
							res.ParkedUntil[q.ID()] = time.UnixMilli(r.BackoffUntil)
						}
						continue
					}
				}
			}
			if reasons := requirements(); len(reasons) > 0 {
				res.Blocked[q.ID()] = reasons
				continue
			}
			// A rung the ladder is told not to run yet (questplan.HeldRungs: Dragon Slayer, which has never run
			// live and which the quest-point fillers put within reach). It comes last of the checks and before
			// the slot is taken: an account short of the 32 quest points reads as it always did, and this line
			// appears only for an account the quest would otherwise have started on.
			if why := questplan.RungHeld(q.ID()); why != "" {
				lifted := false
				if e.Unhold != nil {
					if ok, reason := e.Unhold(q.ID()); ok {
						lifted = true
						e.noteRungUnheld(q.ID(), reason, heldSaid)
					} else if reason != "" {
						why += "; " + reason
					}
				}
				if !lifted {
					res.Blocked[q.ID()] = []string{"held back: " + why}
					e.noteRungHeld(q.ID(), why, heldSaid)
					continue
				}
			}
			if e.slotNeeded(q.ID()) && !tryHoldQuest(q.ID(), d, time.Now()) {
				res.Blocked[q.ID()] = []string{busyReason}
				res.SlotBusy = append(res.SlotBusy, q.ID())
				continue
			}
			next = &qs[i]
			break
		}
		res.Completed = res.Completed[:0]
		for _, q := range qs {
			if done[q.ID()] {
				res.Completed = append(res.Completed, q.ID())
			}
		}
		if len(res.Completed) == len(qs) {
			res.Outcome = LadderDone
			return res
		}
		if next == nil {
			// Nothing is runnable, and nothing the account does next walks it out of a sealed place it stands in through
			// this ladder: the pocket's own way out is tried here, so the stuck stages that follow do not start in it.
			if here, ok := currentTileOK(d); ok {
				e.leaveStuckPocket(here)
			}
			res.Outcome = LadderStuck
			return res
		}
		slot := perQuest
		if e.SlotFor != nil {
			if s := e.SlotFor(next.ID()); s > 0 {
				slot = s
			}
		}
		left := time.Until(deadline)
		run, clipped, fits := questplan.LadderSlot(slot, left)
		if !fits && ran == 0 && left > 0 {
			// The first quest of a pass always gets what the budget has, as before: a pass is
			// never refused outright for a slot longer than its whole budget.
			run, clipped, fits = left, true, true
		}
		if !fits {
			releaseQuest(next.ID(), d)
			d.logf("[ladder] not starting %s: %s of the pass budget is left and its slot is %s", next.ID(), left.Round(time.Second), slot)
			res.Outcome = LadderBudget
			return res
		}
		ran++
		d.logf("[ladder] running %s (%d/%d complete, slot %s)", next.ID(), len(res.Completed), len(qs), run.Round(time.Second))
		r := e.Run(*next, run)
		releaseQuest(next.ID(), d)
		d.logf("[ladder] %s ended %s (%s)", next.ID(), r.Outcome, r.Reason)
		if r.Outcome == OutcomeStopped {
			res.Outcome = LadderStopped
			return res
		}
		if r.Outcome == OutcomeComplete || (r.Outcome == OutcomeTimeout && r.Progressed) {
			res.Progressed = true
		}
		if r.Outcome == OutcomeTimeout {
			e.noteSlotEnd(next.ID(), r, clipped)
		}
		hp, max := Hitpoints(d)
		if why := passEndsHurt(r, hp, max); why != "" {
			d.logf("[ladder] ending the pass after %s: %s", next.ID(), why)
			res.Outcome = LadderBudget
			return res
		}
	}
	res.Outcome = LadderBudget
	return res
}

// passEndsHurt says why a ladder pass must end after run r left the account at hp of max: a fight it lost
// and walked away from, or under half its hitpoints. The next quest would start hurt and often still under
// attack. Live 16 Oct 2026 an account lost Tree Gnome Village's tower fight at 14/53 with no food, the ladder
// ran Merlin's Crystal at 7/53 and the Agility rung at 4/53, and it died on the walk out. The stage loop eats
// and banks before the next pass. An unread hitpoints count (max 0) ends nothing.
func passEndsHurt(r Result, hp, max int) string {
	if r.Outcome == OutcomeParked && (strings.HasPrefix(r.Reason, "lost a fight") || strings.Contains(r.Reason, "lost fight(s)")) {
		return fmt.Sprintf("it lost a fight (%d/%d hitpoints)", hp, max)
	}
	if max > 0 && hp*2 < max {
		return fmt.Sprintf("%d/%d hitpoints is under half", hp, max)
	}
	return ""
}

// noteLadderHold logs, once a call, that quest id is held back by hard level gates and says which
// rungs train them and whether this ladder has them: the line an operator reads to learn why a
// quest the ladder lists never started.
func (e *Engine) noteLadderHold(id string, holds []questplan.LadderHold, inLadder, said map[string]bool) {
	reasons := questplan.HoldReasons(holds)
	key := id + "|" + strings.Join(reasons, "|")
	if said[key] {
		return
	}
	said[key] = true
	var trains []string
	for _, h := range holds {
		if h.Rung == "" {
			continue
		}
		where := "not in this ladder"
		if inLadder[h.Rung] {
			where = "in this ladder"
		}
		trains = append(trains, fmt.Sprintf("%s (%s)", h.Rung, where))
	}
	line := fmt.Sprintf("[ladder] %s is held back, not run: %s", id, strings.Join(reasons, "; "))
	if len(trains) > 0 {
		line += "; trained by " + strings.Join(trains, ", ")
	}
	e.D.logf("%s", line)
}

// noteRungHeld logs, once a call, that quest id is not run because questplan.HeldRungs holds it, and why.
// noteRungUnheld logs, once a call, that an opted-in account is let past a rung's hold and why.
func (e *Engine) noteRungUnheld(id, why string, said map[string]bool) {
	key := id + "|rung unheld"
	if said[key] {
		return
	}
	said[key] = true
	e.D.logf("[ladder] %s: its hold is lifted for this account (%s)", id, why)
}

func (e *Engine) noteRungHeld(id, why string, said map[string]bool) {
	key := id + "|rung held"
	if said[key] {
		return
	}
	said[key] = true
	e.D.logf("[ladder] %s is held back, not run: %s", id, why)
}

// noteSlotEnd remembers a run that ran its slot out. A slot the pass budget cut short is the
// budget's doing, not the quest's, so nothing is recorded; one the quest ran whole is parked for
// the shortest backoff when it moved the quest on, and like any other failure when it did not
// (Store.NoteTimeout).
func (e *Engine) noteSlotEnd(id string, r Result, clipped bool) {
	if e.Store == nil || clipped {
		return
	}
	e.Store.NoteTimeout(id, "budget ran out: "+r.LastStep, time.Now(), r.Progressed)
	e.save()
}

// recordCompletion notes a quest ending complete: in the side store (which build finished it, on which
// account, after how many decisions) and as a log line, so cmd/questpromote can count the accounts that
// finished a quest on a build that already held its module as it is now. SeenComplete, the old one-bit
// hint, cannot say that. tick is the decide pass the quest reported Done on: 1 means it was complete before
// the run did anything, and such a record says so (Completion.Earned) rather than passing for evidence.
// endsTheQuest reports whether a Done step on a journal reading j is a completion. A journal that still
// reads not started or in progress means a partial run (Fight Arena's way-out-only pass returns Done at
// stage < 12), which the promotion evidence must not count. Rungs and miniquests have no tab (unknown).
func endsTheQuest(j questplan.Status) bool {
	return j != questplan.StatusInProgress && j != questplan.StatusNotStarted
}

func (e *Engine) recordCompletion(id string, tick int) {
	c := questplan.Completion{At: time.Now().UnixMilli(), Account: e.Account, Build: e.Build, Ticks: tick}
	if e.Store != nil {
		e.Store.NoteCompletion(id, c)
	}
	e.D.logf("%s", questplan.CompletionLogLine(id, c))
}

// deathTravelFloat is the coins drawn after a death: several ship fares home.
const deathTravelFloat = 300

// recoverAfterDeath waits for the respawn, then draws a travel float from the nearest bank so
// the walk back to the quest (over ships, whose fares a respawned account cannot pay) can
// succeed, and refreshes the bank view. Measured on the first Waterfall lab run: the respawned
// account's walk from Lumbridge to East Ardougne found no path at all without fares.
func (e *Engine) recoverAfterDeath() {
	d := e.D
	d.Delay(3 * time.Second)
	ClearLeftoverModal(d)
	if PackCounts(d)[questplan.CoinsID] >= deathTravelFloat/2 {
		return
	}
	bank := questplan.NearestBank(CurrentTile(d))
	d.logf("[%s] after the death: drawing a travel float at %s", e.quest.ID(), bank.Name)
	b, ok := OpenBankAt(d, bank.Tile)
	if !ok {
		d.logf("[%s] the %s bank could not be used", e.quest.ID(), bank.Name)
		return
	}
	counts := BankCounts(b)
	if n := counts[questplan.CoinsID]; n > 0 {
		want := deathTravelFloat
		if n < want {
			want = n
		}
		WithdrawExact(d, b, questplan.CoinsID, want)
	}
	e.prov().Bank.Note(BankCounts(b))
	_ = b.Close()
}

// eatLoop eats by the sustain while a quest runs, but never while a dialogue or a modal is up
// (eating would close it). Under it, while a Travel leg is under way, sits the floor (EatFloor): any carried food at or under
// half the hitpoints, whatever the module's Sustain names, and for a module with no Sustain at all. The loop therefore runs
// for every quest, not only the ones that name food.
func eatLoop(d *Deps, sus questplan.Sustain, stop <-chan struct{}) {
	t := time.NewTicker(600 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		main, side, chat := d.Session.State().Modals()
		if main != gamestate.NoModal || side != gamestate.NoModal || chat != gamestate.NoModal {
			continue
		}
		if d.Escape != nil && d.Escape() {
			continue // it teleported out of what its bites cannot outrun: nothing to eat for here
		}
		if EatIfNeeded(d, sus.FoodIDs, sus.EatAt) {
			continue
		}
		if d.walking.Load() > 0 {
			EatFloor(d)
		}
	}
}

// moduleTools says which items are a module's own tools, by id (Module.ToolIDs) and by name substring (Module.Tools, as
// Engine.keepFunc matches them): the floor bite leaves those to the quest.
func moduleTools(d *Deps, m questplan.Module) func(id int) bool {
	ids := map[int]bool{}
	for _, id := range m.ToolIDs {
		ids[id] = true
	}
	var names []string
	for _, n := range m.Tools {
		if n != "" {
			names = append(names, strings.ToLower(n))
		}
	}
	return func(id int) bool {
		if ids[id] {
			return true
		}
		if len(names) == 0 {
			return false
		}
		name := strings.ToLower(ObjName(d, id))
		for _, n := range names {
			if strings.Contains(name, n) {
				return true
			}
		}
		return false
	}
}

// fightable reports whether the account should fight an attacker back rather than keep moving:
// one within ten combat levels of the account (an unknown level on either side counts as no).
func fightable(d *Deps, npc entityquery.Entity) bool {
	own, ok := d.Session.State().CombatLevel()
	if !ok || npc.CombatLevel <= 0 {
		return false
	}
	return npc.CombatLevel <= own+10
}

// MaxDeathsSkip is how long a quest that has reached its Module.MaxDeaths is skipped after each
// further death.
const MaxDeathsSkip = 24 * time.Hour
