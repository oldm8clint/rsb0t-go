package fleet

import "sync"

// PER-WORLD LOGIN PACING. Built, wired, and DEFAULT OFF until the experiment says otherwise.
//
// THE OWNER'S IDEA, and it is a good one: "logging in can be sped up by logging 1 account in
// each world each login cycle." If the server's login-rate budget is counted per world, then
// a fleet spread over three worlds has three budgets and fills in a third of the time.
//
// THE EVIDENCE FOR IT, from the engine's own source on branch 289 rather than from
// behaviour. Response 16 ("login attempts exceeded") is produced in
// engine/src/engine/World.ts, from `this.loginDeviceAttempts`, a map keyed
// `${uid}@${client.remoteAddress}` and held IN MEMORY ON THE WORLD NODE. A world node is one
// process per world. So on that code the rate counter is genuinely per world per address,
// and two worlds really do have two independent budgets. That is the strongest argument the
// idea has, and it is a real one.
//
// THE EVIDENCE AGAINST ACTING ON IT YET, which is why this is off. Three gaps, each of which
// alone would be enough:
//
//  1. The live server IS NOT THE REFERENCE ENGINE. The ~90-per-address CONCURRENT ceiling the live
//     fleet hits every day arrives as login response 9, and in the reference checkout NOTHING
//     generates response 9 for a connection count at all — the only paths to it are a
//     members-only world and the login thread's reply 6, which nothing sends. So the live server runs
//     a modified or newer login server, and the one limit this fleet actually collides with
//     is not in the source that says the rate counter is per node. Reasoning from that source
//     about a different server's added limits is exactly the kind of inference this
//     repository has been burned by before.
//  2. A FRONT PROXY WOULD COLLAPSE THEM. If the live server terminates every world behind one
//     address-aware front end, the "per node" property of an in-memory map on the node is
//     irrelevant — the limit that matters is wherever that front end counts.
//  3. THE COST OF BEING WRONG IS ASYMMETRIC. Right, this halves a restart's ramp, which is a
//     few minutes once a day. Wrong, it doubles the fleet's aggregate attempt rate against a
//     shared budget, trips response 16, and loginRateGate.Penalize halts EVERY login
//     fleet-wide for two minutes — extending on each further rejection. The fleet has
//     already live-locked exactly this way once (see maxLoginPenalty's own doc comment).
//
// So the machinery is here and one boolean turns it on, once the experiment in
// docs/multiworld.md has been run and says the budgets are separate. Until then every world
// shares the one gate, which is precisely today's behaviour.

// worldGates holds one loginRateGate per world, created on demand. Its zero value is not
// usable — build one with newWorldGates.
//
// The shape deliberately mirrors the route pool's: routes.go already gives each ADDRESS its
// own gate, for the same reason and with the same evidence ("the server's login-rate and
// connection verdicts are per address"). Per-world pacing is the same claim along the other
// axis, and if it holds the two compose — a gate per (route, world) pair — which is what
// gateFor keys on.
type worldGates struct {
	mu      sync.Mutex
	enabled bool
	gates   map[string]*loginRateGate
	shared  *loginRateGate
}

func newWorldGates(shared *loginRateGate) *worldGates {
	return &worldGates{gates: map[string]*loginRateGate{}, shared: shared}
}

// enable turns per-world pacing on. Off is the default and is today's behaviour exactly.
func (w *worldGates) enable(on bool) {
	w.mu.Lock()
	w.enabled = on
	w.mu.Unlock()
}

// gateFor returns the gate a login on this (route, world) pair should wait on. With pacing
// off — the default — it returns the gate it was given, so every caller keeps the single
// shared or per-route gate it has always used and not one byte of behaviour changes.
//
// routeGate is the gate the route pool already chose (the Manager's own with no pool). It is
// what this falls back to, so turning the feature off can never lose the route's pacing.
func (w *worldGates) gateFor(routeKey string, world int, routeGate *loginRateGate) *loginRateGate {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.enabled || world <= 0 {
		return routeGate
	}
	key := routeKey + "#" + itoa(world)
	g, ok := w.gates[key]
	if !ok {
		g = newLoginRateGate()
		w.gates[key] = g
	}
	return g
}

// close stops every gate this pool created.
func (w *worldGates) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, g := range w.gates {
		g.Close()
	}
	w.gates = map[string]*loginRateGate{}
}

// itoa avoids pulling strconv in for one call in a hot-ish path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// SetPerWorldLoginPacing turns per-world login pacing on or off at runtime. OFF by default.
//
// DO NOT TURN THIS ON BEFORE THE EXPERIMENT. Read this file's own doc comment: if the
// server's rate budget is shared across worlds rather than per world, this doubles the
// fleet's aggregate attempt rate against one budget, trips response 16, and halts every
// login fleet-wide for two minutes at a time. The experiment that settles it is in
// docs/multiworld.md and takes two accounts and about three minutes.
func (m *Manager) SetPerWorldLoginPacing(on bool) {
	m.worldGatesOnce.Do(func() { m.wgates = newWorldGates(m.loginRate) })
	m.wgates.enable(on)
	if on {
		m.logf("fleet: per-world login pacing ENABLED — each world now has its own rate budget. If the server's budget is NOT per world this will trip response 16 and halt every login fleet-wide; see internal/fleet/loginrateworld.go")
	} else {
		m.logf("fleet: per-world login pacing disabled — every world shares one login budget")
	}
}

// worldGateFields is what this file adds to Manager, embedded for the same reason
// worldFields is: one line in fleet.go.
type worldGateFields struct {
	worldGatesOnce sync.Once
	wgates         *worldGates
}

// worldGateFor is the Manager-level accessor loginOnce uses.
func (m *Manager) worldGateFor(routeKey string, world int, routeGate *loginRateGate) *loginRateGate {
	m.worldGatesOnce.Do(func() { m.wgates = newWorldGates(m.loginRate) })
	return m.wgates.gateFor(routeKey, world, routeGate)
}
