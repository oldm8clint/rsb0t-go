// Package hops runs the webwalk hops a Walker cannot run with a plain loc op: NPC fares
// (ships, carts, the Brimhaven customs officer) and special crossings that are an op followed
// by a dialogue. It ports exec/specialCrossing.ts's handleSpecialCrossing for those cases.
//
//	w, _ := live.NewWalker(deps)
//	hops.Install(w, hops.New(hops.Deps{Client: live.NewClient(deps), Session: deps.Session,
//	    Config: deps.Config, Components: deps.Components, Log: deps.Log}))
//
// Install sets Walker.NPCHop, Walker.SpecialHop and Walker.CanHop, so the planner only routes
// through crossings Supports accepts. Crossings that use an item on a loc, and anything the
// graph marks with an item to use, are refused and so never planned.
package hops

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/webwalk"
)

// Deps is what a Handler needs. Client is the same webwalk.Client the Walker uses.
type Deps struct {
	Client     webwalk.Client
	Session    actions.Session
	Config     *cachecfg.Config
	Components archive.Components
	Log        func(string)
}

// Handler runs NPC hops and dialogue crossings for one account.
type Handler struct {
	d Deps
	// turn is the place in a queued barrier's line the crossing in progress holds (locHop); drive gives it up when a
	// dialogue starts, since a dialogue leaves the barrier alone.
	turn *webwalk.LaneTurn
}

// New returns a Handler over d.
func New(d Deps) *Handler { return &Handler{d: d} }

// Install makes w plan and run the hops h supports.
func Install(w *webwalk.Walker, h *Handler) {
	w.NPCHop = h.Hop
	w.SpecialHop = h.Hop
	w.CanHop = h.Supports
}

const (
	tick = 600 * time.Millisecond
	// dialogueSteps is SHIP_DIALOGUE_STEPS: dialogue passes, one tick each, before giving up.
	dialogueSteps = 40
	// lateLandingTicks is how much longer a ship crossing whose dialogue has been answered waits for the sailing to land
	// once dialogueSteps is spent: set_sail's p_delay is 7 ticks and the observed lag after it is another few.
	lateLandingTicks = 15
	// npcNearStand and npcNearMe are the TS query radii: the NPC at this pier first, so a
	// customs officer on the other route cannot take the hop, then any in range.
	npcNearStand = 10
	npcNearMe    = 12
	// arrivalRadius is specialCrossing.ts's default arrivalRadius for NPC hops.
	arrivalRadius = 2
	// approachFor bounds walking up to an NPC the stand is not beside, and approachMinPerTarget
	// is the floor on what any one candidate square gets.
	//
	// Twenty seconds, and a per-candidate slice sized by the distance, because the old six
	// seconds divided among approachTargets' nine squares could not walk anywhere. Measured on
	// the live fleet 2026-09-22 (run/fleet.log, 269 accounts): "Ardougne->Brimhaven ship: could
	// not get beside Captain Barnaby" appears 1,063 times against 74 arrivals on that ship, and
	// it is the reason a Stage 3 account in East Ardougne could not reach Varrock — the boat
	// chain is the only wolf-free way east, so a fare that will not start is a whole continent
	// unreachable. The tiles the failures name span 2674..2683, 3270..3276: Barnaby walks his
	// dock and is routinely four to nine tiles from the edge's baked stand at 2683,3272.
	// approachFor/len(targets) gave each candidate 666ms — one 600ms tick, i.e. ONE step — and
	// every slice re-clicked a different square, so each click cancelled the last one's walk and
	// the account shuffled on the spot until the budget ran out.
	//
	// The overall budget is only ever spent when the account is genuinely far from the npc: the
	// arrival test below returns the moment it is within one tile.
	approachFor          = 20 * time.Second
	approachMinPerTarget = 4 * tick
	// settleFor is how long a crossing may sit with no dialogue and no movement before it is
	// called failed.
	settleFor = 8 * tick
	// stepOntoTicks bounds finishOnLandingTile. Ten ticks because the barrier it waits out is
	// three, and the click that follows still has to be walked.
	stepOntoTicks = 10
)

// Supports reports whether Hop can run e: an NPC hop with an NPC to talk to, or a special
// crossing that is an NPC's dialogue or a loc op with an optional dialogue. Use-item
// crossings are refused.
func (h *Handler) Supports(e *webwalk.Edge) bool {
	if h.d.Components == nil || h.d.Config == nil {
		// Without the interface archive the dialogue is invisible: no options, no Continue.
		return false
	}
	if e.Special != nil {
		if e.Special.UseItem != "" {
			return false
		}
		return e.Special.NPC != "" || e.HasLoc() || e.LocName != ""
	}
	return !e.HasLoc() && e.LocName != ""
}

func (h *Handler) logf(format string, args ...interface{}) {
	if h.d.Log != nil {
		h.d.Log(fmt.Sprintf(format, args...))
	}
}

func (h *Handler) label(e *webwalk.Edge) string {
	if e.Special != nil && e.Special.Label != "" {
		return e.Special.Label
	}
	return fmt.Sprintf("%s %s", e.Action, e.LocName)
}

// Hop runs one crossing with the player at or near e.From and returns true once the player
// has landed.
func (h *Handler) Hop(e *webwalk.Edge) bool {
	if !h.Supports(e) {
		h.logf("%s: not a crossing this handler runs", h.label(e))
		return false
	}
	npc := e.LocName
	if e.Special != nil && e.Special.NPC != "" {
		npc = e.Special.NPC
	}
	if !e.HasLoc() || (e.Special != nil && e.Special.NPC != "") {
		return h.npcHop(e, npc)
	}
	return h.locHop(e)
}

func (h *Handler) me() (webwalk.Point, bool) { return h.d.Client.Tile() }

func (h *Handler) delay(d time.Duration) bool { return h.d.Client.Delay(d) }

// landed is the TS arrived(): within the arrival radius of the landing on its level, except that
// the tile the hop is operated FROM is never a landing.
//
// That exception is not theoretical. It was found live, and by a fix that caused it: once the Al
// Kharid toll edge was moved to land one tile beyond the gate, its From (3267) and its To (3269)
// were two tiles apart, which is exactly arrivalRadius. So the first check drive makes — taken
// immediately after the op and BEFORE the dialogue has been answered — reported the crossing
// complete while the player had not moved an inch and the toll had not been paid. The walker then
// tried to step off a gate square it was never on, failed, and burned its repath budget.
//
// A player standing where the hop started has not crossed anything, whatever the geometry says,
// so this is the correct rule independently of the radius.
//
// It is also not a toll-gate special case, which was measured rather than assumed. The corrected
// graph holds 27 same-level special crossings whose two ends are within arrivalRadius of each
// other, and only 4 of them are this gate: the rest are the Karamja shipyard gates, both Desert
// Mining Camp gates, the Mort Myre gates, the Baxtorian and Temple of Ikov keyed doors, the hill
// giant hut door and the Draynor Manor bookcases. Every one of those could report a crossing
// complete before its dialogue was answered, because the player begins each of them standing on
// From. So this rule closes the same latent hole in 27 crossings, and the toll gate is simply
// where it was finally noticed.
func (h *Handler) landed(e *webwalk.Edge) bool {
	me, ok := h.me()
	if !ok {
		return false
	}
	if me == e.From {
		return false
	}
	return me.Level == e.To.Level && webwalk.Chebyshev(me, e.To) <= arrivalRadius
}

type npcMatch struct {
	index, op int
	at        webwalk.Point
}

// findNPC is Npcs.query().name(npc).action(act).withinOf(stand, 10).nearest(), then within 12
// of the player.
func (h *Handler) findNPC(name, act string, stand webwalk.Point) (npcMatch, bool) {
	me, _ := h.me()
	best, bestD := npcMatch{}, 1<<30
	pass := func(center webwalk.Point, radius int) bool {
		for _, n := range h.d.Session.State().NPCs() {
			t, ok := h.d.Config.Npc(n.Type)
			if !ok || !strings.EqualFold(t.Name, name) {
				continue
			}
			op := cachecfg.OpIndex(t.Ops, act)
			if op == 0 {
				continue
			}
			at := webwalk.Point{X: n.X, Z: n.Z, Level: n.Level}
			if at.Level != center.Level || webwalk.Chebyshev(at, center) > radius {
				continue
			}
			if d := webwalk.Chebyshev(at, me); d < bestD {
				best, bestD = npcMatch{index: n.Index, op: op, at: at}, d
			}
		}
		return bestD < 1<<30
	}
	if pass(stand, npcNearStand) || pass(me, npcNearMe) {
		return best, true
	}
	return npcMatch{}, false
}

// approachTarget is the square to walk at when closing on an npc: whichever of the eight tiles
// around it is nearest to where we are now. Pure, so the choice is testable without a session.
//
// approachTargets is the squares to aim at when closing on an npc, best first.
//
// THE NPC'S OWN TILE COMES FIRST, and that is deliberate. Many npcs can be walked onto — they do
// not block movement — so aiming at one is the shortest correct answer and the engine puts the
// account exactly where the interaction wants it. But some cannot: a banker behind a desk stands
// on a blocked square (fleet owner, 2026-09-21), and for those a walk at the npc gets as close as
// the route finder manages and may stop out of range. So the eight neighbours follow as
// fallbacks.
//
// Order matters the other way too. Aiming at a neighbour FIRST would be wrong for an npc standing
// somewhere its neighbours are not walkable — Captain Barnaby is on a dock, where the squares
// beside him can be water — so a neighbours-only rule trades one failure for another. Trying the
// npc first and the neighbours after cannot regress either case.
//
// Neighbours are ordered by Chebyshev distance and then by true (squared euclidean) distance, so
// the tile straight ahead is preferred over the two corners beside it: a corner is entered
// diagonally, and a diagonal step is refused when either square it cuts between is blocked.
func approachTargets(me, at webwalk.Point) []webwalk.Point {
	out := make([]webwalk.Point, 0, 9)
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if dx == 0 && dz == 0 {
				continue
			}
			out = append(out, webwalk.Point{X: at.X + dx, Z: at.Z + dz, Level: at.Level})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := webwalk.Chebyshev(me, out[i]), webwalk.Chebyshev(me, out[j])
		if ci != cj {
			return ci < cj
		}
		dix, diz := me.X-out[i].X, me.Z-out[i].Z
		djx, djz := me.X-out[j].X, me.Z-out[j].Z
		return dix*dix+diz*diz < djx*djx+djz*djz
	})
	return append([]webwalk.Point{at}, out...)
}

// approachBudget is how long one candidate square gets: long enough to walk there, with a
// floor so a one-tile shuffle still has room to resolve. Pure, so the arithmetic that the
// old approachFor/len(targets) got wrong is testable without a session.
func approachBudget(from, dest webwalk.Point) time.Duration {
	d := webwalk.Chebyshev(from, dest)
	b := time.Duration(d+2) * tick
	if b < approachMinPerTarget {
		b = approachMinPerTarget
	}
	return b
}

// approach walks next to an NPC the player is not beside: the engine runs the client route
// finder, so an NPC op from several tiles away does not walk the player there. relocate, when
// set, re-reads where the npc is now; a dock captain walks his pier while we cross it.
func (h *Handler) approach(at webwalk.Point, relocate func() (webwalk.Point, bool)) bool {
	me, ok := h.me()
	if !ok {
		return false
	}
	if webwalk.Chebyshev(me, at) <= 1 {
		return true
	}
	// Try the npc's own tile, then the squares beside it (see approachTargets). Production
	// logged `Ardougne->Brimhaven ship: could not get beside Captain Barnaby at 2677,3275,0`
	// followed by `'Captain Barnaby' not interactable`, then burned a repath and about 24
	// seconds before a second attempt happened to land in range (spare104, 2026-09-21).
	// woodcutrunner's own doc comment records the same line against the same npc, so it is not
	// specific to one route.
	//
	// The arrival test below is the authority throughout: it asks whether we are within one
	// tile OF THE NPC, never whether we reached the square we aimed at. So a candidate that
	// turns out to be unreachable costs one share of the budget and nothing else.
	deadline := h.d.Client.Now().Add(approachFor)
	for h.d.Client.Now().Before(deadline) {
		me, ok := h.me()
		if !ok {
			return false
		}
		if webwalk.Chebyshev(me, at) <= 1 {
			return true
		}
		// reaimed says the npc moved while we were walking, so the candidate list computed
		// against its old tile is stale and the whole pass starts again from where it is now.
		reaimed := false
		for _, dest := range approachTargets(me, at) {
			if !h.d.Client.Now().Before(deadline) {
				break
			}
			from, ok := h.me()
			if !ok {
				return false
			}
			if webwalk.Chebyshev(from, at) <= 1 {
				return true
			}
			if err := h.d.Client.Walk([]webwalk.Point{from, dest}, false); err != nil {
				continue
			}
			end := h.d.Client.Now().Add(approachBudget(from, dest))
			if end.After(deadline) {
				end = deadline
			}
			for h.d.Client.Now().Before(end) {
				if !h.delay(tick) {
					return false
				}
				cur, ok := h.me()
				if !ok {
					return false
				}
				if webwalk.Chebyshev(cur, at) <= 1 {
					return true
				}
				if relocate != nil {
					if again, ok := relocate(); ok && again != at {
						at, reaimed = again, true
						break
					}
				}
			}
			if reaimed {
				break
			}
		}
		// A whole pass over every candidate against an npc that never moved is the old
		// behaviour, and there is nothing left to try.
		if !reaimed {
			break
		}
	}
	cur, _ := h.me()
	return webwalk.Chebyshev(cur, at) <= 1
}

// npcHop is handleSpecialCrossing's NPC branch.
func (h *Handler) npcHop(e *webwalk.Edge, name string) bool {
	label := h.label(e)
	preferred := "Talk-to"
	if a := e.Action; a != "" && a != "Open" && a != "Go-through" && a != "Pull" {
		preferred = a
	}
	acts := []string{preferred}
	if preferred != "Talk-to" {
		acts = append(acts, "Talk-to")
	}
	stand := webwalk.Point{X: e.LocX, Z: e.LocZ, Level: e.From.Level}
	interacted := false
	for _, act := range acts {
		n, ok := h.findNPC(name, act, stand)
		if !ok {
			continue
		}
		// Follow the npc while walking at it. Captain Barnaby paces his Ardougne dock, and
		// aiming at where he stood when the hop started is how 1,063 approaches missed him.
		relocate := func() (webwalk.Point, bool) {
			if again, ok := h.findNPC(name, act, stand); ok {
				return again.at, true
			}
			return webwalk.Point{}, false
		}
		if !h.approach(n.at, relocate) {
			// Click the npc anyway: an OPNPC with no queued waypoints makes the engine path the player to it
			// itself (Player.pathToTarget, findPathToEntity for a SMART mover). Live 12 Oct the client-side
			// approach to Captain Barnaby, pacing his Ardougne dock, failed 552 times in four hours, every one
			// stopped at 2683,3271, and each failure threw the whole crossing away.
			here, _ := h.me()
			h.logf("%s: could not get beside %s at %s in %v (stopped at %s): clicking from here and letting the server walk", label, name, n.at, approachFor, here)
		}
		// The NPC may have stepped while we walked; look again.
		if again, ok := h.findNPC(name, act, stand); ok {
			n = again
		}
		if err := actions.OpNpc(h.d.Session, n.op, n.index); err != nil {
			h.logf("%s: %s %s: %v", label, act, name, err)
			continue
		}
		h.logf("%s: %s %s at %s", label, act, name, n.at)
		interacted = true
		break
	}
	if !interacted {
		if m, ok := h.opLocByName(name, preferred, stand); ok {
			h.logf("%s: %s %s (a loc) at %d,%d", label, preferred, name, m.X, m.Z)
			interacted = true
		}
	}
	if !interacted {
		h.logf("%s: '%s' not interactable near %s", label, name, stand)
		return false
	}
	return h.drive(e, label)
}

// opLocByName is an NPC hop's fallback when no NPC answers to the name: a loc of that name (any
// case) offering the option, within 5 tiles of the stand, operated. The spirit trees are locs in
// this content ([ent], [stronghold_ent], [spirittree_small], loc 1317: name "Spirit tree", op1
// Talk-to, content scripts/areas/area_gnome/configs/spirit_tree.loc), while the walk graph names
// them "Spirit Tree" on hops with no loc id. Live on 1306c66a (5 Oct 2026) not one spirit-tree
// crossing succeeded: "'Spirit Tree' not interactable" at all four trees, 56 times for spare103 alone,
// with the main account and spare103 walking in circles round the routes that relied on them.
func (h *Handler) opLocByName(name, act string, stand webwalk.Point) (webwalk.LocMatch, bool) {
	ms := h.d.Client.FindLocs(webwalk.LocQuery{At: stand, Radius: 5, Names: []string{name}, Op: act})
	if len(ms) == 0 {
		return webwalk.LocMatch{}, false
	}
	if err := h.d.Client.OpLoc(ms[0].Op, ms[0].X, ms[0].Z, ms[0].ID); err != nil {
		return webwalk.LocMatch{}, false
	}
	return ms[0], true
}

// locHop runs a special crossing on a loc: the op, then its dialogue.
func (h *Handler) locHop(e *webwalk.Edge) bool {
	label := h.label(e)
	// The Gnome stronghold's gate is gone for five ticks while a player is carried through, so a click behind the first
	// of a burst finds nothing: the fleet queues at it, three accounts at a time (webwalk.EnterLane, queue.go has the
	// evidence); the turn lasts until the landing, or until the first dialogue line (Femi's boxes talk leaves the gate
	// alone and takes half a minute).
	turn, _, stopped := webwalk.EnterLane(h.d.Client, e, h.d.Log)
	if stopped {
		return false
	}
	h.turn = turn
	defer func() {
		turn.Leave()
		h.turn = nil
	}()
	at := webwalk.Point{X: e.LocX, Z: e.LocZ, Level: e.From.Level}
	q := webwalk.LocQuery{At: at, Radius: 1, IDs: []int{e.LocID}, Op: e.Action}
	ms := h.d.Client.FindLocs(q)
	if len(ms) == 0 {
		q = webwalk.LocQuery{At: at, Radius: 5, Names: []string{e.LocName}, Op: e.Action}
		ms = h.d.Client.FindLocs(q)
	}
	if len(ms) == 0 {
		h.logf("%s: '%s' with %s not found near %s", label, e.LocName, e.Action, at)
		return false
	}
	if err := h.d.Client.OpLoc(ms[0].Op, ms[0].X, ms[0].Z, ms[0].ID); err != nil {
		h.logf("%s: %v", label, err)
		return false
	}
	if h.drive(e, label) {
		return h.finishOnLandingTile(e, label)
	}
	// A gate that opened after its dialogue still has to be stepped through.
	me, ok := h.me()
	if ok && me.Level == e.From.Level && webwalk.Chebyshev(e.From, e.To) == 1 && webwalk.Chebyshev(me, e.To) == 1 {
		_ = h.d.Client.Walk([]webwalk.Point{me, e.To}, false)
		for i := 0; i < 6; i++ {
			if !h.delay(tick) {
				return false
			}
			if cur, _ := h.me(); cur == e.To {
				h.logf("%s: stepped through", label)
				return true
			}
		}
	}
	return false
}

// finishOnLandingTile steps off a crossing's intermediate square onto the tile the edge claims
// to land on, and verifies that it got there.
//
// A crossing can deliver the player one tile short of its landing on purpose. The Al Kharid toll
// gate is the case this was written for: content teleports the payer onto the gate's OWN square
// and then walls all four sides of that square for three ticks, so a walker that replans from
// there reaches for the same toll edge again and pays again — 953 crossings at 10 coins in one
// measured run. The edge now lands one tile BEYOND the gate (see tollGateWhy in graph.go) so the
// planner always resumes from a tile it can continue from, and getting there is this step.
//
// The click is re-issued rather than sent once, because content's own inviswall refuses the
// first ticks of it. A step that never arrives returns false, which is the important half: the
// walker's refusal machinery then routes around the crossing instead of repeating it, where
// before an arrival "within two tiles" let a wrong-way shuttle report success forever.
func (h *Handler) finishOnLandingTile(e *webwalk.Edge, label string) bool {
	me, ok := h.me()
	if !ok {
		return false
	}
	if me == e.To {
		return true
	}
	if me.Level != e.To.Level || webwalk.Chebyshev(me, e.To) != 1 {
		h.logf("%s: ended at %s, which is not beside the landing %s", label, me, e.To)
		return false
	}
	for i := 0; i < stepOntoTicks; i++ {
		cur, ok := h.me()
		if !ok {
			return false
		}
		if cur == e.To {
			h.logf("%s: stepped off onto the landing %s", label, e.To)
			return true
		}
		if i%2 == 0 {
			_ = h.d.Client.Walk([]webwalk.Point{cur, e.To}, false)
		}
		if !h.delay(tick) {
			return false
		}
	}
	cur, _ := h.me()
	if cur == e.To {
		return true
	}
	h.logf("%s: could not step from %s onto the landing %s", label, cur, e.To)
	return false
}

// drive answers the crossing's dialogue until the player lands: the TS pass loop, one tick
// per pass, picking a listed choice, continuing, or closing a message box.
func (h *Handler) drive(e *webwalk.Edge, label string) bool {
	var choose []string
	if e.Special != nil {
		choose = e.Special.Choose
	}
	dlg := actions.NewChatDialog(h.d.Session, h.d.Components)
	start, _ := h.me()
	lastMove := h.d.Client.Now()
	sawDialogue, warned, flown := false, false, false
	// arrive is the landing's own bookkeeping: a landing rebuilds the scene, so give the next hop's loc a moment to
	// appear.
	arrive := func(extra string) bool {
		settle := tick
		if start.Level != e.To.Level {
			settle = 2 * tick
		}
		if !h.delay(settle) {
			return false
		}
		h.logf("%s: arrived at %s%s", label, e.To, extra)
		return true
	}
	for i := 0; i < dialogueSteps; i++ {
		if h.landed(e) {
			return arrive("")
		}
		acted := ""
		options := h.liveOptions(dlg)
		if pick, ok := pickOption(options, choose); ok {
			if err := actions.IfButton(h.d.Session, pick.ComID); err == nil {
				acted = "chose " + pick.Text
			}
		} else if nowhere, ok := pickOption(options, []string{"Nowhere."}); ok {
			// The run staller's jewellery rub left its teleport menu open ("Burthorpe Games Rooms." /
			// "Nowhere."): live 7 Oct 2026 it sat in front of 25 ship, toll-gate and spirit dialogues.
			if err := actions.IfButton(h.d.Session, nowhere.ComID); err == nil {
				acted = "dismissed a jewellery teleport menu"
			}
		} else if dlg.CanContinue() {
			if err := dlg.Continue(); err == nil {
				acted = "continue"
			}
		} else if main, _, _ := h.d.Session.State().Modals(); main != -1 {
			if com, ok := gliderButton(main, e.To); ok && h.isOKButton(com) {
				// The map stays open through the flight's p_delay; closing it is not the way out.
				if !flown {
					if err := actions.IfButton(h.d.Session, com); err == nil {
						acted, flown = "chose the destination on the glider map", true
					}
				}
			} else {
				_ = actions.CloseModal(h.d.Session)
				acted = "closed a message box"
			}
		} else if len(options) > 0 && !warned {
			// TS keeps passing until its budget runs out; say once what was on offer.
			warned = true
			h.logf("%s: no listed choice among %v", label, optionTexts(options))
		}
		if acted != "" {
			sawDialogue = true
			h.turn.Leave()
			h.logf("%s: %s", label, acted)
			lastMove = h.d.Client.Now()
		}
		if !h.delay(tick) {
			return false
		}
		if cur, ok := h.me(); ok && cur != start {
			start, lastMove = cur, h.d.Client.Now()
		}
		if !sawDialogue && !dlg.IsOpen() && h.d.Client.Now().Sub(lastMove) >= settleFor {
			h.logf("%s: nothing happened after the op", label)
			return false
		}
	}
	if h.landed(e) {
		return true
	}
	// A sailing lands a fixed delay after the last answer (set_sail's p_delay, then p_telejump), and a long dialogue can
	// use the whole pass budget before the ship has gone: the Brimhaven to Ardougne customs officer's takes 18 s at the
	// median and 24 s at the 90th percentile, so 21 of 523 crossings (and 3 of 581 Musa to Port Sarim) were called
	// failed at the 24 s budget, a second or two before the player landed on the deck (16 Oct 2026 logs). The
	// walker had by then replanned from the dock, was thrown by the landing a moment later, and ended "stalled" on a
	// deck it was not on yet when it started. Wait the delay out before calling an answered sailing failed.
	if e.Kind == webwalk.KindShip && sawDialogue {
		waited, landed, stopped := h.awaitLanding(e, lateLandingTicks)
		if stopped {
			return false
		}
		if landed {
			return arrive(fmt.Sprintf(" (%d ticks after the dialogue budget)", waited))
		}
	}
	me, _ := h.me()
	h.logf("%s: did not land at %s (at %s)", label, e.To, me)
	return false
}

// awaitLanding waits up to ticks ticks for the player to land, and says how many it waited. stopped is the client's stop.
func (h *Handler) awaitLanding(e *webwalk.Edge, ticks int) (waited int, landed, stopped bool) {
	for waited < ticks {
		if !h.delay(tick) {
			return waited, false, true
		}
		waited++
		if h.landed(e) {
			return waited, true, false
		}
	}
	return waited, false, false
}

// gliderMapRoot is the glider map a Gnome pilot opens after "Can you take me on the glider?"
// ("Of course!" then if_openmain(glidermap), content area_gnome/scripts/gnome_glider.rs2), and
// gliderButtons are its destination buttons by landing tile: content pack/interface.pack
// 802=glidermap and 824..828=glidermap:com_21..com_25, whose if_button scripts fly to ^gandius,
// ^ta_quir_priw, ^sindarpos, ^lemanto_andra and ^kar_hewo (area_gnome/configs/glider.constant).
// The flight is the button, then p_delay(3) and the teleport. drive used to close the map as a
// message box, which cancelled every flight: live on 50ec96f0 (5 Oct 2026) no glider hop landed.
const gliderMapRoot = 802

var gliderButtons = map[webwalk.Point]int{
	{X: 2971, Z: 2969, Level: 0}: 824, // Gandius
	{X: 2465, Z: 3501, Level: 3}: 825, // Ta Quir Priw, the Grand Tree hub
	{X: 2850, Z: 3497, Level: 0}: 826, // Sindarpos
	{X: 3320, Z: 3430, Level: 0}: 827, // Lemanto Andra
	{X: 3284, Z: 3211, Level: 0}: 828, // Kar-Hewo
}

// gliderButton is the glider map button that flies to the tile to, when main is the glider map.
func gliderButton(main int, to webwalk.Point) (int, bool) {
	if main != gliderMapRoot {
		return 0, false
	}
	com, ok := gliderButtons[to]
	return com, ok
}

// isOKButton reports whether the session's cache has com as a plain button, so a cache whose
// glider map differs from the content pack's closes the map as before rather than clicking
// something else.
func (h *Handler) isOKButton(com int) bool {
	c, ok := h.d.Components[com]
	return ok && c != nil && c.ButtonType == archive.ButtonOK
}

// liveOptions are the dialogue's options with the labels the server set. The archive's own
// labels on a multi-choice chat interface are placeholders ("option1", "option2"), and
// ChatDialog.Options returns those, so each button's IF_SETTEXT text is used when known.
func (h *Handler) liveOptions(dlg *actions.ChatDialog) []actions.Option {
	opts := dlg.Options()
	st := h.d.Session.State()
	for i := range opts {
		if text, ok := st.TextKnown(opts[i].ComID); ok && text != "" {
			opts[i].Text = text
		}
	}
	return opts
}

// pickOption is specialCrossings.ts's pickChoice: the first option containing any wanted
// choice, ignoring case.
func pickOption(options []actions.Option, choose []string) (actions.Option, bool) {
	for _, o := range options {
		text := strings.ToLower(o.Text)
		for _, c := range choose {
			if c != "" && strings.Contains(text, strings.ToLower(c)) {
				return o, true
			}
		}
	}
	return actions.Option{}, false
}

// pickChoice returns pickOption's text, or "".
func pickChoice(options []actions.Option, choose []string) string {
	o, _ := pickOption(options, choose)
	return o.Text
}

func optionTexts(options []actions.Option) []string {
	out := make([]string, len(options))
	for i, o := range options {
		out[i] = o.Text
	}
	return out
}
