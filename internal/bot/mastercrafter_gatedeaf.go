package bot

// The Nature gate that could not hear its carriers (live 18 Oct 2026, runecraftplan/gatestall.go has the whole story). After an event
// monster's flee MasterCrafter stood on 2864,3020, west of the Ruined Pillar, three tiles from his stand. The stand click was one
// waypoint, which the server walks greedily and which parked him against the pillar for good; the carriers, all east of the pillar,
// could not ask him (the engine's walk to him ended against the same pillar, silently), so no request line ever came and for 56
// minutes the gate looked open and did nothing. Two layers answer it here:
//
//   - the stand click is the pack's route whenever the lone waypoint would not arrive (runecraftrunner.ClickStandWalk), and three
//     clicks that leave him on the same tile hand the walk to the routed walker;
//   - carriers waiting with a load and no request line coming are noticed (gateWatch): off the stand he is walked onto it, on it the gate
//     says what it can see, and a gate with no trade for ten minutes says how it stands, so silence is never the whole log;
//   - a gate that stays deaf past the walks climbs a ladder of its own (runecraftplan.DecideGateDeaf): a nudge after three minutes, the
//     end of the round after six. It has to: the fleet watchdog's restart is the cure that used to run for a silent gate, and a gate that
//     writes lines is never silent to it (any line resets its clock, internal/stallwatch), so the lines above would otherwise have
//     turned a wedged gate that the watchdog restarted after fifteen minutes into one that says "gate deaf" for ever.
//
// The carriers' half (a request nobody answers is made from another side of him) is runecraftrunner.TradeWithCrafter.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

const (
	// standNoMoveClicks is how many stand clicks in a row may leave him on the same tile before the routed walker is sent instead.
	standNoMoveClicks = 3
	// standRouteWait is how long a routed stand click is given to walk before the next one is sent from a tile he has moved to: the
	// client's view of his tile is a tick behind, and a second click from a stale tile walks him back.
	standRouteWait = 5 * time.Second
	// standClickStale is how old the last stand click may be and still count as "the click he did not move after".
	standClickStale = 6 * time.Second
	// gateEntryGap is how long Gate() may go unrun before the next run counts as a return to the gate (he was crafting, or away).
	gateEntryGap = 5 * time.Second
	// standSayEvery is the longest between two lines about the stand click.
	standSayEvery = 30 * time.Second
)

// gateDeafState is the gate's watch on its own silence.
type gateDeafState struct {
	// lastGate is when Gate() last ran its trading step; since the start of the quiet being measured (the last request line, trade or
	// return to the gate); waitingSince when carriers were first seen waiting in it.
	lastGate, since, waitingSince time.Time
	// walks are the corrective walks made in this quiet; notedAt when it was last written about; quietAt when the no-trade line was.
	walks            int
	notedAt, quietAt time.Time
	// nudged says the nudge was made in this quiet, at nudgedAt. endWhy is the reason the round must end, once the ladder's last rung
	// has been reached: Gate() fails with it until runGateLoop ends the round, and it is never cleared (the world is the round's).
	nudged   bool
	nudgedAt time.Time
	endWhy   string
	// The stand click: how many in a row left him where he was, whether the walker is due, and when the stand line was last written.
	standNoMove  int
	standEsc     bool
	standSaidAt  time.Time
	standBlindAt time.Time
}

// quietFrom starts the measured quiet afresh at t: a request line read, a trade, a return to the gate or a corrective walk. The ladder
// starts again with it; a latched end of the round (endWhy) does not.
func (st *gateDeafState) quietFrom(t time.Time) {
	st.since, st.waitingSince, st.walks, st.notedAt = t, time.Time{}, 0, time.Time{}
	st.nudged, st.nudgedAt = false, time.Time{}
}

// standClickDue says the stand click may be sent now: not before standClickEvery after the last, not while the walker is due, and a
// routed click that is carrying him is left alone while he moves.
func (w *masterNatureWorld) standClickDue(here tile.Tile) bool {
	if w.deaf.standEsc {
		return false
	}
	since := time.Since(w.standClick)
	if since < standClickEvery {
		return false
	}
	if w.standRouted && here != w.standClickAt && since < standRouteWait {
		return false
	}
	return true
}

// clickStand sends the stand click from here (one MOVE_GAMECLICK; the pack's route when the lone waypoint would park him) and keeps
// the account of what it did.
func (w *masterNatureWorld) clickStand(here, stand tile.Tile) {
	if !w.standClick.IsZero() && time.Since(w.standClick) < standClickStale && here == w.standClickAt {
		w.deaf.standNoMove++
		if w.deaf.standNoMove >= standNoMoveClicks {
			w.deaf.standEsc = true
			return
		}
	} else {
		w.deaf.standNoMove = 0
	}
	plan, err := runecraftrunner.ClickStandWalk(w.d, here, stand)
	if err != nil {
		return
	}
	w.standClick, w.standClickAt, w.standRouted = time.Now(), here, plan.Kind == runecraftrunner.StandWalkRouted
	now := time.Now()
	switch plan.Kind {
	case runecraftrunner.StandWalkRouted:
		if now.Sub(w.deaf.standSaidAt) >= standSayEvery {
			w.deaf.standSaidAt = now
			w.logf("the stand click from %d,%d would park him at %d,%d (the server walks a lone waypoint greedily, a pillar stops it for good): sending the routed way to the stand (%d,%d), %d waypoint(s)",
				here.X, here.Z, plan.Parks.X, plan.Parks.Z, stand.X, stand.Z, len(plan.Waypoints))
		}
	case runecraftrunner.StandWalkBlind:
		if plan.Parks != (tile.Tile{}) && now.Sub(w.deaf.standBlindAt) >= standSayEvery {
			w.deaf.standBlindAt = now
			w.logf("WARNING: the stand click from %d,%d would park him at %d,%d and the pack has no route to the stand (%d,%d): sending the lone click", here.X, here.Z, plan.Parks.X, plan.Parks.Z, stand.X, stand.Z)
		}
	}
}

// escalateStand hands the walk to the routed walker when the stand clicks stopped working: standNoMoveClicks of them left him on the
// same tile. It runs between GateSteps, never under an open screen.
func (w *masterNatureWorld) escalateStand() {
	if !w.deaf.standEsc {
		return
	}
	w.deaf.standEsc = false
	w.deaf.standNoMove = 0
	w.standClick = time.Time{}
	stand, ok := w.gateStand()
	here := runecraftrunner.Here(w.d)
	if !ok || here == stand || !runecraftplan.NearGate(here) {
		return
	}
	w.logf("the stand click did not move him (at %s, the stand is %d,%d, %d click(s) in a row): walking the routed way", fleetrunner.HereIs(w.d), stand.X, stand.Z, standNoMoveClicks)
	if !w.walk(stand, 0, 20*time.Second) {
		w.logf("WARNING: the routed walk onto the stand (%d,%d) did not arrive (at %s)", stand.X, stand.Z, fleetrunner.HereIs(w.d))
	}
}

// waitingCarriers is how many carriers on the bus stand at the gate with a load, asking, and who.
func (w *masterNatureWorld) waitingCarriers() (int, []string) {
	var names []string
	for _, c := range w.carrierViews() {
		if c.Role == mulebus.CarrierWaiting && c.Held >= runecraftplan.MinLoad && runecraftplan.NearGate(c.Tile) {
			names = append(names, c.Name)
		}
	}
	return len(names), names
}

// gateWatch runs after every trading step: it keeps the clock of the gate's silence (a request line read, a trade, a return from the
// temple each start it afresh), hands the walk to the routed walker when the stand clicks stopped working, acts on carriers waiting
// with no request line coming (runecraftplan.DecideGateDeaf) and writes the no-trade line.
func (w *masterNatureWorld) gateWatch(res runecraftrunner.GateResult) {
	now := time.Now()
	st := &w.deaf
	// A corrective walk below can take longer than gateEntryGap: the next call must not take the end of it for a return to the gate.
	defer func() { st.lastGate = time.Now() }()
	entered := st.lastGate.IsZero() || now.Sub(st.lastGate) > gateEntryGap
	st.lastGate = now
	if entered || res.Kind != runecraftrunner.GateNothing {
		st.quietFrom(now)
	}
	if line := w.watch.LastRequest(); line.After(st.since) {
		st.quietFrom(line)
	}
	if res.Kind != runecraftrunner.GateNothing || randomevents.Busy(w.ctx.Session) {
		return
	}
	w.escalateStand()
	waiting, names := w.waitingCarriers()
	if waiting == 0 {
		st.waitingSince = time.Time{}
	} else if st.waitingSince.IsZero() {
		st.waitingSince = now
	}
	if waiting > 0 {
		from := st.since
		if st.waitingSince.After(from) {
			from = st.waitingSince
		}
		view := runecraftplan.GateDeafView{Waiting: waiting, Quiet: now.Sub(from), OnStand: w.onStand(), Walks: st.walks, Nudged: st.nudged}
		if !st.notedAt.IsZero() {
			view.Noted, view.SinceNote = true, now.Sub(st.notedAt)
		}
		if st.nudged {
			view.SinceNudge = now.Sub(st.nudgedAt)
		}
		if dec := runecraftplan.DecideGateDeaf(view); dec.Action != runecraftplan.GateDeafNone {
			st.notedAt = now
			warn := ""
			if dec.Action == runecraftplan.GateDeafNudge || dec.Action == runecraftplan.GateDeafEnd {
				warn = "WARNING: "
			}
			w.logf("%sgate deaf: %s (%s); %s", warn, dec.Why, runecraftplan.JoinNames(names), w.deafFacts())
			switch dec.Action {
			case runecraftplan.GateDeafWalk:
				st.walks++
				if stand, ok := w.gateStand(); ok {
					if !w.walk(stand, 0, 20*time.Second) {
						w.logf("WARNING: the routed walk onto the stand (%d,%d) did not arrive (at %s)", stand.X, stand.Z, fleetrunner.HereIs(w.d))
					}
				}
				st.since, st.waitingSince = time.Now(), time.Time{}
			case runecraftplan.GateDeafNudge:
				st.nudged, st.nudgedAt = true, time.Now()
				w.nudgeDeaf()
			case runecraftplan.GateDeafEnd:
				st.endWhy = "nobody could ask the gate: " + dec.Why
			}
		}
	}
	w.quietNote(now, waiting)
}

// nudgeDeaf is the gate's own version of the fleet watchdog's nudge, and then some: it presses the continue of a chat dialog and
// closes any modal that is open (either would stop a trade screen opening), starts the request watcher at the end of the chat log
// again (a cursor that lost its place would hear no line), and steps him to a tile beside the stand and back (a walk packet clears
// whatever interaction the engine still holds of him). It runs between GateSteps with no screen open.
func (w *masterNatureWorld) nudgeDeaf() {
	d := w.d
	var did []string
	if masterDismissChat(w.ctx) {
		did = append(did, "pressed continue on a chat dialog")
	}
	// A trade screen under way is not the nudge's to close (GateStep has ended with none open, so this is a guard, not a case).
	if main, side, chat := d.Session.State().Modals(); (main != -1 || side != -1 || chat != -1) && !actions.NewTrade(d.Session).Active() {
		err := actions.CloseModal(d.Session)
		did = append(did, fmt.Sprintf("closed the open modal main/side/chat %d/%d/%d (err=%v)", main, side, chat, err))
	}
	w.watch.Reset(d)
	did = append(did, "forgot the request lines read so far")
	if stand, ok := w.gateStand(); ok && runecraftrunner.Here(d) == stand {
		if off, ok := runecraftrunner.WiggleTile(d.Pack, stand, runecraftplan.Nature.Exit); ok {
			stepped := w.walk(off, 0, 8*time.Second)
			back := w.walk(stand, 0, 15*time.Second)
			did = append(did, fmt.Sprintf("stepped to %d,%d (arrived %v) and back (arrived %v)", off.X, off.Z, stepped, back))
		}
	}
	w.logf("gate deaf: nudge: %s; now at %s", strings.Join(did, "; "), fleetrunner.HereIs(d))
}

// deafFacts is what the gate can see of its own state, for the deaf line.
func (w *masterNatureWorld) deafFacts() string {
	d := w.d
	here := runecraftrunner.Here(d)
	stand, _ := w.gateStand()
	main, side, chat := d.Session.State().Modals()
	parks := ""
	if plan := runecraftrunner.PlanStandWalk(d.Pack, here, stand); plan.Kind == runecraftrunner.StandWalkRouted {
		parks = fmt.Sprintf("; a lone stand click from here would park him at %d,%d", plan.Parks.X, plan.Parks.Z)
	}
	return fmt.Sprintf("I stand at %s, the stand is %d,%d (%s); modals main/side/chat %d/%d/%d; guardian busy %v%s",
		fleetrunner.HereIs(d), stand.X, stand.Z, runecraftplan.StandWord(w.onStand()), main, side, chat, randomevents.Busy(w.ctx.Session), parks)
}

// quietNote writes the gate's "no trade for ..." line: the first after runecraftplan.GateQuietLine without one, then every
// GateQuietLine.
func (w *masterNatureWorld) quietNote(now time.Time, waiting int) {
	base := w.lastTrade
	if base.IsZero() || base.Before(w.started) {
		base = w.started
	}
	view := runecraftplan.GateQuietView{SinceTrade: now.Sub(base)}
	if !w.deaf.quietAt.IsZero() {
		view.Noted, view.SinceLine = true, now.Sub(w.deaf.quietAt)
	}
	if !runecraftplan.GateQuietNote(view) {
		return
	}
	w.deaf.quietAt = now
	last := "none read this round"
	if t := w.watch.LastRequest(); !t.IsZero() {
		last = now.Sub(t).Round(time.Second).String() + " ago"
	}
	w.logf("no trade for %s: %d carrier(s) on the bus, %d waiting at the gate with a load; last request line %s; %s", view.SinceTrade.Round(time.Minute), len(w.carrierViews()), waiting, last, w.deafFacts())
}
