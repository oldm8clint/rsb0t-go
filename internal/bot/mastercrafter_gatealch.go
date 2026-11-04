package bot

// The Nature gate's alchemy (a user request: "the main account could alch with fire staff and noted yew or magic strung bows can be alched
// inbetween trades"; the decisions and the why are runecraftplan/gatealch.go's).
//
// The kit is drawn at the mule's post (provision): craftRuneAlchBows noted strung bows, dearest first, from the main account's own bank and, when
// that holds none, from the mule (four rounds' worth in one ask). They are cast away on the road to the gate with the walk's own alchemy
// (scarabrunner.Travel: a cast does not stop a walk) and, if some are left on arrival, at the gate before the first trade is answered
// (BurnDown): a stack in the pack is a 4th slot besides the talisman, the Natures and the fare, and a trade of 25 essence needs all 25.
// Every cast at the gate goes through runecraftplan.DecideGateAlch: nothing waiting or open, the last cast's lock over, no carrier near
// enough to be asking before this one's lock is over. A round that cannot cast its stack away ends (AlchStuck) and the bank takes it back.

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// gateAlchStuckAfter is how long casts that were ALLOWED (runecraftplan.DecideGateAlch said cast) may go out without a bow going before
// the round gives the stack up: eight locks. Time the cast was not allowed (a screen, a random event, the lock) is not counted, so a
// stack that waits on something else is never called stuck for it (the burn-down is bounded by runecraftplan.AlchBurnMax instead).
const gateAlchStuckAfter = 25 * time.Second

// gateAlchOffFor is how long the alchemy stays off after a round that could not cast its stack away.
const gateAlchOffFor = 6 * time.Hour

// The gate alchemy's seams, as package variables a test replaces: whether a fire staff is worn, the players in view, and one cast (the
// walk alchemy's idle cast).
var (
	gateAlchFireStaff = fleetrunner.FireStaffWorn
	gateAlchPlayers   = func(d *fleetrunner.Deps) []gamestate.Player { return d.Session.State().Players() }
	gateAlchCast      = func(t *fleetrunner.Travel, d *fleetrunner.Deps) bool { return t.IdleAlch(d) }
)

// gateAlchState is the round's alchemy.
type gateAlchState struct {
	// on: the setting, the bows known to the object config, not off after a stuck round. bows are their ids.
	on   bool
	bows []runecraftrunner.AlchBowIDs
	// drawn is the bows the kit drew, arrived whether the walk to the gate is over (the burn-down clock starts then) and burnFrom when.
	drawn    int
	arrived  bool
	burnFrom time.Time
	// castAt is when the last cast was sent; lastStack the stack at the last look. stallFrom is when the first cast was allowed since
	// the stack last fell (zero: none since): the stuck clock runs from it, so only casts that were allowed and did nothing count.
	castAt, stallFrom time.Time
	lastStack         int
	// held is the bows by name at the last look (the coins a cast paid are the bow's content value, 0.6 of its cost).
	held map[string]int
	// The round's figures: casts (paid), experience and coins, the seconds of casting, and where the last log line was.
	casts0, xp0 int
	coins       int
	castSecs    time.Duration
	logged      int
	fireAt      time.Time
	fire        bool
	said        map[string]time.Time
	why         string
	stuck       string
	roadCasts   int
	// leg is the casts on the temple's walking legs that landed (mastercrafter_legcast.go).
	leg legCastState
}

// say logs once per `every` per key.
func (a *gateAlchState) say(key string, every time.Duration, f func()) {
	if a.said == nil {
		a.said = map[string]time.Time{}
	}
	if t, ok := a.said[key]; ok && time.Since(t) < every {
		return
	}
	a.said[key] = time.Now()
	f()
}

// initAlch sets the round's alchemy up from the settings and the account: on only with the setting, a bow count, the object config and
// the bows in it, and no stuck round in the last six hours.
func (w *masterNatureWorld) initAlch() {
	a := &w.alch
	a.on = false
	switch {
	case !w.set.GateAlch || w.set.AlchBows <= 0:
		return
	case w.ctx.Config == nil:
		return
	case time.Now().Before(w.b.gateAlchOffUntil):
		w.logf("alch: off until %s (the last round could not cast its bows away)", w.b.gateAlchOffUntil.Format("15:04"))
		return
	}
	a.bows = runecraftrunner.ResolveAlchBows(w.ctx.Config)
	a.on = len(a.bows) > 0
	if !a.on {
		w.logf("alch: none of the bows is in the object config, so the gate casts nothing")
	}
}

// fireStaff is whether a fire staff is worn, read at most every five seconds (the equipment is a component read).
func (w *masterNatureWorld) fireStaff() bool {
	a := &w.alch
	if time.Since(a.fireAt) > 5*time.Second {
		a.fire, a.fireAt = gateAlchFireStaff(w.d), time.Now()
	}
	return a.fire
}

// alchStack is the noted bows in the pack.
func (w *masterNatureWorld) alchStack() int {
	if !w.alch.on {
		return 0
	}
	return runecraftrunner.AlchStack(w.d, w.alch.bows)
}

// alchSettings is the walk alchemy's setting for the bows: the four of them, any value.
func alchSettings() fleetplan.WalkAlchSettings {
	return fleetplan.WalkAlchSettings{Enabled: true, MinMagic: runecraftplan.AlchMagic, Items: fleetplan.DefaultAlchItems, MinValue: 1}.Normalised()
}

// alchWant is what the visit at the post draws: nothing unless the account can cast.
func (w *masterNatureWorld) alchWant() runecraftrunner.AlchWant {
	a := &w.alch
	if !a.on {
		return runecraftrunner.AlchWant{}
	}
	magic := fleetrunner.MagicLevel(w.d)
	staff := w.fireStaff()
	waiting := 0
	for _, c := range w.carrierViews() {
		if c.Role == mulebus.CarrierWaiting {
			waiting++
		}
	}
	// The Natures the pack will hold: the campaign tops it up to the keep from the bank in the same visit. The bank is asked at the
	// visit (AlchWant.NatureFloor), so the kit here is only gated by the account's ability.
	setting := w.set.AlchBows
	if !w.set.AlchHold {
		// A stack still held at the gate holds the first trade up, so the draw is what the road took last time (the first round: 60).
		r := w.b.gateAlchRoad
		setting = runecraftplan.AlchRoadDraw(setting, r.drawn, r.casts, r.left)
	}
	n := runecraftplan.GateAlchKit(true, w.set.AlchHold, setting, magic, 1<<20, staff, waiting)
	if n <= 0 {
		a.say("cannot", time.Hour, func() {
			w.logf("alch: no bows drawn (Magic %d, fire staff worn %v: High Level Alchemy needs Magic %d and the Staff of fire wielded)", magic, staff, runecraftplan.AlchMagic)
		})
		return runecraftrunner.AlchWant{}
	}
	return runecraftrunner.AlchWant{Qty: n, Bows: a.bows, NatureFloor: runecraftplan.AlchNaturesFloor}
}

// alchShort is how many bows the visit drew short of what was wanted, and the ask to make for the rest ("" when none is worth asking).
func (w *masterNatureWorld) alchAsk(want runecraftrunner.AlchWant, res runecraftrunner.VisitResult) (name string, qty int) {
	if want.Qty <= 0 || res.AlchDrawn >= want.Qty/2 {
		return "", 0
	}
	bankHas := 0
	for _, n := range res.BankAlch {
		bankHas += n
	}
	if bankHas > 0 {
		return "", 0 // the bank holds some: what it holds is what was drawn
	}
	stock := muleBankCount(w.ctx, w.b.mule)
	for _, b := range want.Bows {
		q := b.AskAs
		if q == "" {
			q = bowstringplan.AskName(b.Name, int(b.Strung)) // an entry from before AskAs: a strung bow
		}
		if stock != nil {
			if n, _ := stock(q); n < want.Qty {
				continue
			}
		}
		return q, want.Qty * runecraftplan.AlchAskFactor
	}
	return "", 0
}

// provisionAlch tops the kit up from the mule when the bank held no bows, then draws again. It never fails the round: no bows is a gate
// that does not alch. It reports the bows the pack holds.
func (w *masterNatureWorld) provisionAlch(want runecraftrunner.AlchWant, res runecraftrunner.VisitResult) int {
	a := &w.alch
	if want.Qty <= 0 {
		return 0
	}
	if name, qty := w.alchAsk(want, res); name != "" {
		w.logf("alch: the bank holds no bows: asking %s for %s %s to cast at the gate", w.b.mule, runecraftplan.Thousands(qty), name)
		r := w.AskMule(runecraftplan.Step{Stage: runecraftplan.StageAskMule, Altar: runecraftplan.Nature, Lines: []runecraftplan.Line{{Item: name, Qty: qty}}})
		if r.Stopped {
			return 0
		}
		if !r.OK {
			w.logf("alch: the mule gave no bows (%s): the gate casts nothing this round", r.Why)
			return 0
		}
		res = runecraftrunner.Visit(w.d, w.post, w.gateWant(want), nil)
		if !res.Opened {
			w.logf("WARNING: the bank at the mule's post would not open to draw the bows (%s): the gate casts nothing this round", res.Err)
			return 0
		}
	}
	a.drawn = runecraftrunner.AlchStack(w.d, want.Bows)
	if a.drawn > 0 {
		held := runecraftrunner.AlchHeld(w.d, want.Bows)
		w.logf("alch: drew %d noted bow(s) %v to cast on the road to the gate and before the first trade (High Level Alchemy, the Staff of fire wielded, %d Nature runes held)",
			a.drawn, held, runecraftrunner.Natures(w.d))
	} else {
		w.logf("alch: no bows were drawn (the bank holds %v): the gate casts nothing this round", res.BankAlch)
	}
	a.lastStack, a.stallFrom = a.drawn, time.Time{}
	a.held = runecraftrunner.AlchHeld(w.d, want.Bows)
	a.casts0, a.xp0, _, _ = w.travelCounters()
	return a.drawn
}

// gateWant is the gate pack's visit with the alchemy's draw added.
func (w *masterNatureWorld) gateWant(alch runecraftrunner.AlchWant) runecraftrunner.Want {
	a := runecraftplan.Nature
	return runecraftrunner.Want{Talisman: int32(a.TalismanID), Coins: runecraftplan.GateFare, Keep: []int32{runecraftrunner.NatureID}, Natures: w.set.Keep, Alch: alch}
}

// alchTotals are the round's casts and Magic experience since the kit was drawn: the walk alchemy's (the road, the idle seconds) and the
// casts on the legs.
func (w *masterNatureWorld) alchTotals() (casts, xp int) {
	a := &w.alch
	c, x, _, _ := w.travelCounters()
	return c - a.casts0 + a.leg.casts, x - a.xp0 + a.leg.xp
}

func (w *masterNatureWorld) travelCounters() (casts, xp, teleports, saved int) {
	if w.d.Travel == nil {
		return 0, 0, 0, 0
	}
	return w.d.Travel.Counters()
}

// alchRoad switches the walk alchemy on for the walk to the gate (the stack is cast away while he walks) and off after it.
func (w *masterNatureWorld) alchRoad(on bool) {
	a := &w.alch
	t := w.d.Travel
	if t == nil {
		return
	}
	if !on {
		t.Alch.Enabled = false
		return
	}
	if !a.on || w.alchStack() <= 0 || fleetrunner.MagicLevel(w.d) < runecraftplan.AlchMagic {
		return
	}
	t.Alch = alchSettings()
}

// alchArrived starts the burn-down clock: the walk to the gate is over.
func (w *masterNatureWorld) alchArrived() {
	a := &w.alch
	a.arrived = true
	stack := w.alchStack()
	w.noteHeld()
	a.burnFrom, a.stallFrom = time.Now(), time.Time{}
	a.lastStack = stack
	if a.drawn > 0 {
		c, xp, _, _ := w.travelCounters()
		a.roadCasts = c - a.casts0
		w.b.gateAlchRoad.drawn, w.b.gateAlchRoad.casts, w.b.gateAlchRoad.left = a.drawn, a.roadCasts, stack
		w.logf("alch: reached the gate with %d of the %d bow(s) left: %d cast on the road (+%s Magic xp)", stack, a.drawn, a.roadCasts, runecraftplan.Thousands(xp-a.xp0))
		if w.set.AlchHold {
			w.logf("alch: the stack is held through the trades (craftRuneAlchHold): %d bow(s), cast in the idle seconds when no carrier is within %d tiles; the gate takes %d essence a trade while it lasts",
				stack, runecraftplan.AlchClearTiles, runecraftrunner.TradeCapacity(w.d))
		} else if stack > runecraftplan.AlchBurnWarn {
			w.logf("WARNING: %d bow(s) are left at the gate: no trade is answered until they are cast away (a trade of 25 essence needs the slot), about %s", stack,
				(time.Duration(stack) * 3 * time.Second).Round(time.Second))
		}
	}
}

// alchCanCast says the stack can still be cast away: the alchemy is on, the account can cast, and the casts that were allowed have been
// landing (a bow fell within gateAlchStuckAfter of the first allowed cast since the last one).
func (w *masterNatureWorld) alchCanCast() bool {
	a := &w.alch
	if !a.on || fleetrunner.MagicLevel(w.d) < runecraftplan.AlchMagic || !w.fireStaff() || runecraftrunner.Natures(w.d) <= runecraftplan.AlchNaturesFloor/2 {
		return false
	}
	return !(a.arrived && !a.stallFrom.IsZero() && time.Since(a.stallFrom) > gateAlchStuckAfter)
}

// alchBurning says the stack is being cast away before the gate trades (the road mode: with the stack held through the trades, it is not).
func (w *masterNatureWorld) alchBurning() bool {
	return !w.set.AlchHold && w.alch.arrived && w.alchStack() > 0
}

// alchHold is GateOptions.Hold: no request is answered while bows are held and can still be cast away.
func (w *masterNatureWorld) alchHold() bool {
	a := &w.alch
	if !a.arrived || w.set.AlchHold {
		return false
	}
	return runecraftplan.BurnDown(w.alchStack(), w.alchCanCast(), time.Since(a.burnFrom))
}

// alchLocked is GateOptions.Delayed: the last cast's lock is not over.
func (w *masterNatureWorld) alchLocked() bool {
	a := &w.alch
	// Any cast this session sent (the road's, this one's) is noted by the actions layer with the delay it leaves (actions.CastOnItem).
	if actions.DelayLeft(w.d.Session, time.Now()) > 0 {
		return true
	}
	if a.castAt.IsZero() {
		return false
	}
	wait := time.Duration(runecraftplan.AlchLockTicks) * 600 * time.Millisecond
	if t := w.d.Travel; t != nil {
		wait = t.IdleAlchWait()
	}
	return time.Since(a.castAt) < wait
}

// alchStuckWhy is why the round must end for the stack, "" while it need not.
func (w *masterNatureWorld) alchStuckWhy() string {
	a := &w.alch
	if a.stuck != "" {
		return a.stuck
	}
	if !a.arrived || w.set.AlchHold {
		return "" // a held stack is not a stuck one: the gate trades 24 a trade while it lasts
	}
	stack := w.alchStack()
	if stack <= 0 {
		return ""
	}
	if why := runecraftplan.AlchStuck(stack, w.alchCanCast(), time.Since(a.burnFrom)); why != "" {
		a.stuck = why
		w.b.gateAlchOffUntil = time.Now().Add(gateAlchOffFor)
		w.logf("WARNING: %s: the round ends, the bank at the post takes them back, and the alchemy stays off for %s", why, gateAlchOffFor)
		return why
	}
	return ""
}

// nearestCarrier is the tiles to the nearest of his carriers in view that did not trade a moment ago (-1: none).
func (w *masterNatureWorld) nearestCarrier() int {
	here := runecraftrunner.Here(w.d)
	best := -1
	for _, p := range gateAlchPlayers(w.d) {
		if !w.allowed(p.Name) {
			continue
		}
		if actions.SameUsername(p.Name, w.lastPartner) && time.Since(w.lastPartnerAt) < 60*time.Second {
			continue // it has just traded: it is on its way to the store, not asking
		}
		dist := tile.Chebyshev(here, tile.Tile{X: p.X, Z: p.Z, Level: p.Level})
		if best < 0 || dist < best {
			best = dist
		}
	}
	return best
}

// gateAlch is one idle poll's look at casting: it sends a cast when DecideGateAlch says so. It does not block (the cast is one packet; the
// lock is waited out by the polls that follow) and never while a screen is open or a request waits.
func (w *masterNatureWorld) gateAlch() {
	a := &w.alch
	if !a.on || !a.arrived {
		return
	}
	d := w.d
	stack := w.alchStack()
	w.alchProgress(stack)
	if stack <= 0 {
		return
	}
	natures := runecraftrunner.Natures(d)
	if natures <= runecraftplan.AlchNaturesFloor/2 {
		natures = 0
	}
	v := runecraftplan.GateAlchView{
		Enabled: true, Magic: fleetrunner.MagicLevel(d), FireStaff: w.fireStaff(), Natures: natures, Stack: stack,
		TradeOpen: actions.NewTrade(d.Session).Active(), Pending: w.watch.Pending(), Awaiting: w.watch.Awaiting(),
		Locked: w.alchLocked(), Busy: randomevents.Busy(d.Session), Nearest: w.nearestCarrier(), Burning: w.alchBurning(),
	}
	dec := runecraftplan.DecideGateAlch(v)
	a.why = dec.Why
	if !dec.Cast || d.Travel == nil {
		return
	}
	if a.stallFrom.IsZero() {
		a.stallFrom = time.Now() // the stuck clock runs while casts are allowed and no bow falls
	}
	d.Travel.Alch = alchSettings()
	sent := gateAlchCast(d.Travel, d)
	d.Travel.Alch.Enabled = false
	if sent {
		a.castAt = time.Now()
		a.castSecs += time.Duration(runecraftplan.AlchLockTicks) * 600 * time.Millisecond
	}
}

// noteHeld counts the coins the bows gone since the last look paid (0.6 of the bow's cost each: alchemy.rs2) and remembers what is held.
func (w *masterNatureWorld) noteHeld() {
	a := &w.alch
	now := runecraftrunner.AlchHeld(w.d, a.bows)
	for name, before := range a.held {
		if gone := before - now[name]; gone > 0 {
			a.coins += gone * runecraftplan.AlchPays(costOf(name))
		}
	}
	a.held = now
}

// alchProgress notes a bow gone (the cast landed), keeps the coins it paid and logs a line every ten casts.
func (w *masterNatureWorld) alchProgress(stack int) {
	a := &w.alch
	if stack < a.lastStack {
		a.stallFrom = time.Time{}
	}
	w.noteHeld()
	a.lastStack = stack
	if n, _ := w.alchTotals(); n >= a.logged+10 || (stack == 0 && n > a.logged && a.arrived && a.logged < n) {
		a.logged = n
		w.logf("%s", w.alchLine())
	}
}

// alchLine is the log line of the round's alchemy.
func (w *masterNatureWorld) alchLine() string {
	a := &w.alch
	casts, xp := w.alchTotals()
	bow := ""
	for name, n := range runecraftrunner.AlchHeld(w.d, a.bows) {
		if n > 0 && (bow == "" || runecraftplan.AlchPays(costOf(name)) > runecraftplan.AlchPays(costOf(bow))) {
			bow = name
		}
	}
	return runecraftplan.GateAlchLine(casts, a.coins, xp, a.castSecs, w.alchStack(), bow)
}

// costOf is a bow's content cost.
func costOf(name string) int {
	return fleetplan.KnownAlchValue(name) * 10 / 6 // the list's High Alchemy payout is 0.6 of the cost
}

// alchPace is the alchemy's part of the pace line: "" when nothing was drawn or cast.
func (w *masterNatureWorld) alchPace() string {
	a := &w.alch
	n, xp := w.alchTotals()
	if a.drawn == 0 && n == 0 {
		return ""
	}
	legs := ""
	if a.leg.casts > 0 {
		legs = fmt.Sprintf(" (%d on the temple legs)", a.leg.casts)
	}
	return fmt.Sprintf("; alchemy %d cast(s)%s, %s gp, %s Magic xp, %d bow(s) left", n, legs, runecraftplan.Thousands(a.coins), runecraftplan.Thousands(xp), w.alchStack())
}
