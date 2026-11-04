package bot

// RuneCarrier (runecraft P3, docs/runecraft_p3_2026-10-14.md). a user request: "runecraft with runners in the fleet, maxing
// nature runes; the main account (MasterCrafter, the main account) gets the Runecraft levels and sits at the altar; other ScarabSwarm accounts
// run essence". A RuneCarrier is a ScarabSwarm account that, while MasterCrafter's Nature gate is open, shuttles UNNOTED Rune
// essence from Jiminua's Jungle Store on Karamja to him at the Nature ruins:
//
//	between shifts   ScarabSwarm, whole (runerunner_swarm.go's pattern: its settings swapped in, its checkpoint resumed);
//	                 every minute a cheap look at the bus decides whether a SHIFT is due (runecraftplan.DecideCarrierShift:
//	                 the gate heard and open, on this world, the mule holding a kit's worth of essence less what other
//	                 carriers have claimed, hitpoints, the cap of ten carriers), and it starts at ScarabSwarm's safe point
//	a shift          the mule's kit (3,000 NOTED essence, 10,000 coins, a Ring of dueling to rub), the ship to Brimhaven, then
//	                 round and round: Jiminua's shelf turns 25 notes into 25 essence for about 50 gp (it sells the notes and buys
//	                 the units back: a sold note is stored as the plain item, runecraftplan.PlanShelf), the 122 steps to the
//	                 ruins around the jungle spiders (runecraftrunner.WalkKaramja), a one-way trade to the crafter
//	                 (runecraftrunner.TradeWithCrafter), and back. The kit's end is a walk home by the ship, the leftovers
//	                 handed to the mule (and, as a courier, the Natures the crafter holds beyond his keep)
//	the run stall    every walk of the shift rubs the ring about every third tick (scarabrunner.Travel.Rub), which holds the
//	                 player's tick and saves the run energy; walk alchemy is refused (a cast burns a Nature rune, about as many as
//	                 a carrier delivers)
//
// Everything is decided afresh each step from the live state (runecraftplan.DecideCarrierStage), so a restart anywhere (on the
// road with the kit, at the gate with a load) carries on: Loop starts a shift at once when the pack holds a noted kit
// (runecarrier_shift.go). The loop is runCarrierShift over a carrierSteps world so the whole shift is tested against a simulated one.
//
// ROLLOUT GATES, none of them this code's: the owner's go; the accounts assigned the script (it takes an account off a plain
// ScarabSwarm assignment; the swarmSettings object keeps its settings); the mule listing each carrier in allowedPartners; the mule
// holding noted-able Rune essence (EssenceMiner, P2) and Ring of dueling (8).

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/settings"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("RuneCarrier", func() Bot { return &runeCarrierBot{} })
}

// RuneCarrierSettings is the script's declared schema for the control panel. Its keys are exactly
// runecraftplan.CarrierKnownKeys plus homeWorld (a test keeps them identical). Loading is STRICT and is
// runecraftplan.DecodeCarrierSettings's, not the schema's: an unknown key, a value of the wrong type or a number outside its range
// stops the start with the whole list.
var RuneCarrierSettings = settings.Schema{
	runecraftplan.KeyCrafter: {Type: settings.TypeString, Default: "", Label: "Crafter (MasterCrafter's login name)",
		Help: "the account standing at the Nature gate; the carrier works for the gate he publishes. Blank never starts a shift"},
	runecraftplan.KeyCarrierMule: {Type: settings.TypeString, Default: runecraftplan.DefaultCarrierMule, Label: "Mule",
		Help: "the MuleReceiver the kit comes from and the leftovers go back to; it must list this account in allowedPartners"},
	runecraftplan.KeySource: {Type: settings.TypeString, Default: runecraftplan.SourceShelf, Options: []string{runecraftplan.SourceShelf}, Label: "Essence source",
		Help: "shelf: the mule's noted essence is un-noted at Jiminua's Jungle Store (no bank anywhere on Karamja)"},
	runecraftplan.KeyKitEssence: {Type: settings.TypeNumber, Default: float64(runecraftplan.KitEssenceDefault),
		Min: settings.Float(runecraftplan.MinKitEssence), Max: settings.Float(runecraftplan.MaxKitEssence), Label: "Noted essence asked of the mule per shift",
		Help: "whole loads of 25, at most what the mule holds less what other carriers have claimed; 3,000 is 120 trips (4.4 hours)"},
	runecraftplan.KeyKitCoins: {Type: settings.TypeNumber, Default: float64(runecraftplan.KitCoinsDefault),
		Min: settings.Float(runecraftplan.MinKitCoins), Max: settings.Float(runecraftplan.MaxKitCoins), Label: "Coins asked of the mule per shift",
		Help: "the shelf costs about 50 coins a trip, the ship 30 each way"},
	runecraftplan.KeyMinHP: {Type: settings.TypeNumber, Default: float64(runecraftplan.DefaultCarrierHP),
		Min: settings.Float(runecraftplan.MinCarrierHP), Max: settings.Float(runecraftplan.MaxCarrierHP), Label: "Least maximum hitpoints",
		Help: "an account under it never starts a shift: the corridor's snakes bite for a few and a spider for about five"},
	runecraftplan.KeyShelfFloor: {Type: settings.TypeNumber, Default: float64(runecraftplan.DefaultShelfFloor),
		Min: settings.Float(runecraftplan.MinFloor), Max: settings.Float(runecraftplan.MaxFloor), Label: "Shelf kept at Jiminua",
		Help: "essence the carrier leaves on her shelf after each visit; from 38 up a unit costs the 2 coin floor"},
	runecraftplan.KeyKeepNatures: {Type: settings.TypeNumber, Default: float64(runecraftplan.DefaultKeepNatures),
		Min: settings.Float(0), Max: settings.Float(runecraftplan.MaxKeepNatures), Label: "Nature runes kept from a courier haul",
		Help: "of the Natures a courier trip brings home, this many stay in the carrier's own bank for its own alchemy instead of going to the mule and being asked back; its own stock counts, 0 hands everything over"},
	runecraftplan.KeyWalkRub: {Type: settings.TypeBoolean, Default: true, Label: "Rub a Ring of dueling while walking",
		Help: "the run stall: a rub about every third tick holds the player's tick, and a held tick neither drains nor recovers run energy"},
	runecraftplan.KeyWalkAlch: {Type: settings.TypeBoolean, Default: false, Label: "Walk alchemy (refused)",
		Help: "a RuneCarrier never alchemises on the road: a cast burns a Nature rune, about as many as a carrier delivers; true stops the start"},
	runecraftplan.KeySwarm: {Type: settings.TypeString, Default: "", Label: "ScarabSwarm settings (JSON object)",
		Help: "passed to ScarabSwarm unchanged between shifts; mule defaults to this carrier's"},
	"homeWorld": {Type: settings.TypeNumber, Default: float64(0), Min: settings.Float(0), Max: settings.Float(255),
		Label: "Login world (0 = the roster's)", Help: "the control panel logs the account in here at a start; the script does not read it. The crafter's gate is on world 1"},
}

// The check and the shift's pacing. Variables so a test can shorten them.
var (
	// carrierCheckEvery is how often the bus is read for a shift while ScarabSwarm runs.
	carrierCheckEvery = time.Minute
	// carrierBackoffs is the wait after a shift that made no trip, doubling up to an hour.
	carrierBackoffs = []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 40 * time.Minute, time.Hour}
	// carrierDiedBackoff is the wait after a death on a shift (the kit is gone and the road may have killed once).
	carrierDiedBackoff = 30 * time.Minute
)

// carrierClaimTTL is how long a kit claim outlives its carrier's ask if nothing releases it.
const carrierClaimTTL = 35 * time.Minute

// carrierKitClaims is the fleet's one record of the Rune essence the carriers on their way to the mule have already counted on:
// the mule's published bank is a reading, not a reservation, and eight carriers reading 3,000 at the same moment would all walk to
// the post for it (scarabplan.MuleClaims' lesson, 13,695 of 17,163 orders back with nothing). Process-wide, as the fleet is one process.
type carrierKitClaims struct {
	mu sync.Mutex
	by map[string]carrierClaim
}

type carrierClaim struct {
	n  int
	at time.Time
}

var carrierClaims = &carrierKitClaims{by: map[string]carrierClaim{}}

func claimKeyOf(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// others is the essence claimed by carriers other than me that the mule's published bank (read at `since`) does not show yet: a
// claim made before the mule last published is already out of the figure it publishes.
func (c *carrierKitClaims) others(me string, now, since time.Time) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for k, cl := range c.by {
		if now.Sub(cl.at) > carrierClaimTTL {
			delete(c.by, k)
			continue
		}
		if k != claimKeyOf(me) && cl.at.After(since) {
			n += cl.n
		}
	}
	return n
}

// claim records that me counts on n essence from the mule.
func (c *carrierKitClaims) claim(me string, n int, now time.Time) {
	c.mu.Lock()
	c.by[claimKeyOf(me)] = carrierClaim{n: n, at: now}
	c.mu.Unlock()
}

// release gives the claim back.
func (c *carrierKitClaims) release(me string) {
	c.mu.Lock()
	delete(c.by, claimKeyOf(me))
	c.mu.Unlock()
}

// muleEssence is the Rune essence the mule's published bank holds and when it published it (ok false: it has not, or the reading is
// stale: muleBankCount's rule).
func muleEssence(ctx *Context, mule string) (n int, updated time.Time, ok bool) {
	stock := muleBankCount(ctx, mule)
	if stock == nil {
		return 0, time.Time{}, false
	}
	n, _ = stock(runecraftplan.EssenceName)
	want := strings.ToLower(strings.TrimSpace(mule))
	for _, e := range ctx.Peers.Entries(time.Now()) {
		if strings.ToLower(strings.TrimSpace(e.Account)) == want {
			updated = e.UpdatedAt
		}
	}
	return n, updated, true
}

// carrierOutcome is what one shift came to.
type carrierOutcome struct {
	// Trips are the essence trades made, Essence the essence handed to the crafter, Natures the Nature runes carried home.
	Trips, Essence, Natures int
	// Paid and Earned are the coins the shelf cost and paid in all.
	Paid, Earned int
	// Died is set when the account died on the shift; Stopped when the bot was stopped; Failed when the shift ended for a passing
	// reason without finishing (the backoff applies when it made no trip).
	Died, Stopped, Failed bool
	// Delivered are the leftovers the mule took.
	Delivered map[string]int
	Took      time.Duration
	Why       string
}

type runeCarrierBot struct {
	set    runecraftplan.CarrierSettings
	walker *webwalk.Walker
	travel *fleetrunner.Travel

	// The ScarabSwarm phase between shifts: the embedded swarm, how to make one (nil: the real scarabSwarmBot; a test supplies
	// another) and the carrier's own settings map while ctx.Settings holds ScarabSwarm's.
	swarm       masterSwarm
	newSwarm    func() masterSwarm
	ownSettings map[string]interface{}

	lastCheck   time.Time
	pending     string
	waitSaid    time.Time
	checkSaid   string
	checkSaidAt time.Time

	backoffUntil time.Time
	fails        int

	// shift replaces the live shift in a test.
	shift func(ctx *Context, why string, resumed bool) carrierOutcome

	startedAt                                     time.Time
	shifts, trips, essence, natures, paid, earned int
	deaths, failedShifts                          int
	lastShiftEnd                                  time.Time
}

func (b *runeCarrierBot) logf(ctx *Context, format string, args ...any) {
	ctx.logf("runecarrier: "+format, args...)
}

func (b *runeCarrierBot) deps(ctx *Context) *fleetrunner.Deps {
	return &fleetrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Walker: b.walker,
		Log: ctx.Log, Loot: ctx.Loot, Kills: ctx.KillLog(), Me: ctx.Username, World: ctx.World(),
		Travel: b.travel,
	}
}

func (b *runeCarrierBot) OnStart(ctx *Context) error {
	// Settings first: a refusal (a typo, walkAlch true, a bank source) names the setting before anything else is checked.
	set, err := runecraftplan.DecodeCarrierSettings(ctx.Settings)
	if err != nil {
		return fmt.Errorf("runecarrier: %w", err)
	}
	if ctx.Pack == nil {
		return fmt.Errorf("runecarrier: no collision pack configured (fleet.config.json needs a packPath) - refusing to start rather than panic later inside pathfinding")
	}
	b.set = set
	b.startedAt = time.Now()
	b.lastCheck = time.Time{} // the first Loop looks at the bus at once
	// The walker ScarabSwarm builds: the boat chain between Ardougne and Brimhaven needs the fare and dialogue hops.
	if w, werr := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); werr != nil {
		b.logf(ctx, "no webwalk (%v) - the ship cannot be planned; walks fall back to plain pathfinding", werr)
	} else {
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{
				Client: w.Client, Session: ctx.Session, Config: ctx.Config,
				Components: ctx.Components, Log: ctx.Log,
			}))
		}
		b.walker = w
	}
	// The run stall on every walk of a shift: rub the ring about every third tick, run on whenever there is energy.
	b.travel = &fleetrunner.Travel{Rub: set.WalkRub, KeepRunning: true, Members: ctx.Pack.Members}
	if b.walker != nil {
		b.walker.Prepare = b.travel.WalkPrepare(func() *fleetrunner.Deps { return b.deps(ctx) })
	}
	b.logf(ctx, "RuneCarrier for %q: kit %d noted Rune essence (%d trips) and %d coins from %q, shelf floor %d at Jiminua, run stall (rub) %s, least maximum hitpoints %d; a shift needs the gate open and the mule holding %d+ essence (less what other carriers have claimed)",
		set.Crafter, set.KitEssence, runecraftplan.ShiftTrips(set.KitEssence), set.KitCoins, set.Mule, set.ShelfFloor, onOff(set.WalkRub), set.MinHP,
		min(runecraftplan.ShiftMinEssence, set.KitEssence))
	if set.Crafter == "" {
		b.logf(ctx, "WARNING: runecraftCrafter is not set - this account runs ScarabSwarm and never a shift")
	}
	if ctx.Bus == nil {
		b.logf(ctx, "WARNING: no wall bus - the gate and the mule cannot be heard, so no shift runs")
	}
	return b.startSwarm(ctx)
}

// startSwarm starts a fresh ScarabSwarm on its own settings; it resumes from its checkpoint.
func (b *runeCarrierBot) startSwarm(ctx *Context) error {
	mk := b.newSwarm
	if mk == nil {
		mk = func() masterSwarm { return idleSwarm(nil) }
	}
	sw := mk()
	b.ownSettings = ctx.Settings
	ctx.Settings = b.set.SwarmSettingsMap()
	if err := sw.OnStart(ctx); err != nil {
		ctx.Settings, b.ownSettings = b.ownSettings, nil
		return err
	}
	b.swarm = sw
	return nil
}

// stopSwarm ends ScarabSwarm's run (its OnStop saves its checkpoint) and gives ctx back its own settings.
func (b *runeCarrierBot) stopSwarm(ctx *Context) {
	if b.swarm == nil {
		return
	}
	b.swarm.OnStop(ctx)
	b.swarm = nil
	if b.ownSettings != nil {
		ctx.Settings, b.ownSettings = b.ownSettings, nil
	}
}

func (b *runeCarrierBot) OnStop(ctx *Context) {
	b.stopSwarm(ctx)
	carrierClaims.release(ctx.Username)
	b.logf(ctx, "stopped after %s - %d shift(s) (%d failed, %d death(s)): %d trip(s), %d essence handed to %q, %d Nature runes carried home, the shelf cost %d gp and paid %d gp",
		time.Since(b.startedAt).Round(time.Second), b.shifts, b.failedShifts, b.deaths, b.trips, b.essence, b.set.Crafter, b.natures, b.paid, b.earned)
}

func (b *runeCarrierBot) Loop(ctx *Context) error {
	if b.swarm == nil {
		if err := b.startSwarm(ctx); err != nil {
			return err
		}
	}
	now := time.Now()
	// A kit in the pack (a restart on the road, a shift that ended mid-way) is a shift under way: it carries on where the account
	// stands, before the check and the safe point (runecarrier_shift.go).
	if why := b.resumeWhy(ctx); why != "" {
		if now.Before(b.backoffUntil) {
			// A shift that has just failed is not started again at once because the kit is still in the pack: ScarabSwarm goes on
			// until the wait is over (the levelling round's resume ignored its backoff and came back every minute).
			if time.Since(b.waitSaid) >= masterCrafterWaitSay {
				b.waitSaid = time.Now()
				b.logf(ctx, "a shift is under way (%s) but the last one failed: backing off until %s", why, b.backoffUntil.Format("15:04"))
			}
		} else {
			return b.runShift(ctx, why, true)
		}
	}
	if mastercraftplan.CheckDue(b.lastCheck, now, carrierCheckEvery) {
		b.check(ctx, now)
	}
	if b.pending != "" {
		in := b.swarm.safeInput(ctx)
		in.FarFromMule = b.farFromMule(ctx)
		safe, why := mastercraftplan.SafePoint(in, mastercraftplan.DefaultSafeHpPercent)
		if safe {
			return b.runShift(ctx, b.pending, false)
		}
		if time.Since(b.waitSaid) >= masterCrafterWaitSay {
			b.waitSaid = time.Now()
			b.logf(ctx, "a shift is due (%s) but ScarabSwarm is not at a safe point: %s - carrying on", b.pending, why)
		}
	}
	return b.swarm.Loop(ctx)
}

// resumeWhy says why a shift is already under way: the pack holds a noted kit (a carrier holds 25 or more noted Rune essence;
// nothing else in the fleet does), or a load of unnoted essence on the island. An unnoted stack off the island is a quest account's
// (Priest in Peril wants 24), not a carrier's, and is left alone.
func (b *runeCarrierBot) resumeWhy(ctx *Context) string {
	if !masterAccountRead(ctx) {
		return ""
	}
	d := b.deps(ctx)
	notes, unnoted := runecraftrunner.Notes(d), runecraftrunner.Essence(d)
	here := runecraftrunner.Here(d)
	switch {
	case notes >= runecraftplan.CarrierOffer:
		return fmt.Sprintf("the pack holds %d noted Rune essence at %s: a kit", notes, fleetrunner.HereIs(d))
	case unnoted >= runecraftplan.MinLoad && runecraftplan.PlaceOf(here).OnIsland():
		return fmt.Sprintf("the pack holds %d Rune essence on the island at %s: a load", unnoted, fleetrunner.HereIs(d))
	}
	return ""
}

// swarmHold is what the carrier reads of the real ScarabSwarm beyond the safe point's input (a test's swarm may not have it): whether the
// Stage 5 quest hold has the account settled at a bank, nothing under way, as of its last pass within the given time.
type swarmHold interface {
	idlingAtBank(within time.Duration) bool
}

// farFromMule is the safe point's place rule: MasterCrafter's (anywhere but the surface on the mule's side of White Wolf Mountain or a
// short walk from its post) and one more place, a bank on the far side of the water where ScarabSwarm's quest hold has settled the
// account (idleAtHoldBank). The hold waits at the NEAREST bank, and for a Varrock quest that is Varrock's: live on 17 Oct 2026 one
// carrier sat at Varrock's west bank for 25 minutes with a shift due and logged "not at a safe point: not on the mule's side of White
// Wolf Mountain or near its post" six times. The shift's first stage walks from there to the post by the ships
// (carrierWorld.askKit pays the fares first and plans around the dens).
func (b *runeCarrierBot) farFromMule(ctx *Context) bool {
	x, z, level, ok := ctx.Session.State().Tile()
	if !ok {
		return true
	}
	t := tile.Tile{X: x, Z: z, Level: level}
	if consolidateSafePlace(&t) {
		return false
	}
	post := scarabMulePresenceOf(ctx, b.set.Mule).Post
	if t.Level == post.Level && tile.Chebyshev(t, post) <= masterNearPost {
		return false
	}
	return !b.idleAtHoldBank(t)
}

// idleAtHoldBank reports whether ScarabSwarm's quest hold has the account settled at the bank it stands at and is only waiting there: the
// hold's last pass was an idle one (swarmHold.idlingAtBank), and the account still stands in the bank's square on the ground floor. An
// account at a bank in the middle of a quest is not idle, and stays where the old rule left it.
func (b *runeCarrierBot) idleAtHoldBank(t tile.Tile) bool {
	h, ok := b.swarm.(swarmHold)
	if !ok || !h.idlingAtBank(fleetplan.HoldSettledFresh) {
		return false
	}
	return fleetplan.HoldSettled(t, questplan.NearestBank(t).Tile)
}

// gateInfo is the crafter's gate as the bus has it.
func (b *runeCarrierBot) gateInfo(ctx *Context, now time.Time) (runecraftplan.GateInfo, mulebus.GateView) {
	g := mulebus.ReadGate(ctx.Bus, b.set.Crafter, now, 0)
	return runecraftplan.GateInfo{Heard: g.Heard, State: string(g.State), Surplus: g.Surplus, Cap: g.Capacity}, g
}

// otherCarriers is how many carriers other than this one are at work for the crafter.
func (b *runeCarrierBot) otherCarriers(ctx *Context, now time.Time) int {
	n := 0
	for _, c := range mulebus.ReadCarriers(ctx.Bus, b.set.Crafter, now, 0) {
		if c.Role != mulebus.CarrierHome && !strings.EqualFold(strings.TrimSpace(c.Name), strings.TrimSpace(ctx.Username)) {
			n++
		}
	}
	return n
}

// shiftView reads the bus, the mule's published bank and the account into runecraftplan.DecideCarrierShift's input.
func (b *runeCarrierBot) shiftView(ctx *Context, now time.Time) runecraftplan.ShiftView {
	gate, view := b.gateInfo(ctx, now)
	p := scarabMulePresenceOf(ctx, b.set.Mule)
	v := runecraftplan.ShiftView{
		Now: now, CrafterSet: b.set.Crafter != "", Gate: gate, GateWorld: view.World, MyWorld: ctx.World(),
		Carriers: b.otherCarriers(ctx, now), MinHP: b.set.MinHP,
		MuleHeard: p.Heard && ctx.Bus != nil, MuleSameWorld: p.SameWorld, KitWant: b.set.KitEssence,
		BackedOffUntil: b.backoffUntil, Members: ctx.Pack != nil && ctx.Pack.Members,
	}
	if maxHP, ok := ctx.Session.State().MaxHP(); ok {
		v.MaxHP = maxHP
	} else {
		v.MaxHP = 1 << 20 // not read yet: the check is made again in a minute
	}
	if n, updated, ok := muleEssence(ctx, b.set.Mule); ok {
		v.MuleKnown = true
		v.MuleEssence = max(0, n-carrierClaims.others(ctx.Username, now, updated))
	}
	return v
}

// check reads the bus and decides whether a shift is due, saying why not when the reason changes (and every ten minutes).
func (b *runeCarrierBot) check(ctx *Context, now time.Time) {
	b.lastCheck = now
	b.publishStandby(ctx, now)
	start, why := runecraftplan.DecideCarrierShift(b.shiftView(ctx, now))
	if start {
		if b.pending == "" {
			b.logf(ctx, "check: a shift is due - %s", why)
			b.waitSaid = time.Time{}
		}
		b.pending = why
		return
	}
	if b.pending != "" {
		b.logf(ctx, "the shift that was due is no longer: %s", why)
	}
	b.pending = ""
	if why != b.checkSaid || now.Sub(b.checkSaidAt) >= 10*time.Minute {
		b.checkSaid, b.checkSaidAt = why, now
		b.logf(ctx, "check: staying on ScarabSwarm - %s", why)
	}
}

// publishStandby tells the crafter this account is ready for a shift, once a check (runecraft P4): his campaign counts the carriers it
// can start with before any gate is open, and a carrier between shifts is on ScarabSwarm and says nothing otherwise. Ready is
// the part of runecraftplan.DecideCarrierShift's list that does not depend on the gate or the mule: the crafter set, a members
// world, maximum hitpoints at or above the floor, no backoff after a failed shift, and the crafter's own world. It is a message to the
// crafter by name and is not "at work" (mulebus.ReadCarriers leaves it out).
func (b *runeCarrierBot) publishStandby(ctx *Context, now time.Time) {
	if ctx.Bus == nil || ctx.Username == "" || b.set.Crafter == "" || ctx.Pack == nil || !ctx.Pack.Members || now.Before(b.backoffUntil) {
		return
	}
	if maxHP, ok := ctx.Session.State().MaxHP(); !ok || maxHP < b.set.MinHP {
		return
	}
	// The gate is on the crafter's world: a carrier on another one could never start a shift for it and must not be counted.
	if ctx.WorldOf != nil {
		if cw := ctx.WorldOf(b.set.Crafter); cw > 0 && cw != ctx.World() {
			return
		}
	}
	x, z, level, _ := ctx.Session.State().Tile()
	ctx.Bus.Publish(mulebus.OnWorld(mulebus.StandbyMessage(ctx.Username, b.set.Crafter, tile.Tile{X: x, Z: z, Level: level}), ctx.World()))
}

// runShift is the whole of a shift: stop ScarabSwarm, run it, record it, start ScarabSwarm again.
func (b *runeCarrierBot) runShift(ctx *Context, why string, resumed bool) error {
	word := "starting"
	if resumed {
		word = "resuming"
	}
	b.logf(ctx, "SHIFT %s at %s: %s", word, fleetrunner.HereIs(b.deps(ctx)), why)
	b.stopSwarm(ctx)
	ctx.SetPhase("runecarrier/shift")
	run := b.shift
	if run == nil {
		run = b.liveShift
	}
	started := time.Now()
	out := run(ctx, why, resumed)
	out.Took = time.Since(started)
	b.afterShift(ctx, out)
	b.pending = ""
	b.lastCheck = time.Now()
	if ctx.Stopped() {
		return nil
	}
	b.logf(ctx, "back to ScarabSwarm")
	return b.startSwarm(ctx)
}

// afterShift records a shift: the totals, the backoff of a shift that made no trip, and the one line that says what it came to.
func (b *runeCarrierBot) afterShift(ctx *Context, out carrierOutcome) {
	b.shifts++
	b.trips += out.Trips
	b.essence += out.Essence
	b.natures += out.Natures
	b.paid += out.Paid
	b.earned += out.Earned
	b.lastShiftEnd = time.Now()
	carrierClaims.release(ctx.Username)
	now := time.Now()
	switch {
	case out.Stopped:
		// A Stop says nothing about the shift and sets no wait.
	case out.Died:
		b.deaths++
		b.failedShifts++
		b.fails++
		b.backoffUntil = now.Add(carrierDiedBackoff)
	case out.Trips > 0:
		b.fails = 0
		b.backoffUntil = time.Time{}
	case out.Failed:
		b.failedShifts++
		b.fails++
		b.backoffUntil = now.Add(carrierBackoffs[min(b.fails, len(carrierBackoffs))-1])
	}
	per := "no trips"
	if out.Trips > 0 {
		per = fmt.Sprintf("%d gp a trip", (out.Paid-out.Earned)/out.Trips)
	}
	wait := ""
	if !b.backoffUntil.IsZero() && now.Before(b.backoffUntil) {
		wait = fmt.Sprintf("; backing off until %s after %d shift(s) without a trip", b.backoffUntil.Format("15:04"), b.fails)
	}
	b.logf(ctx, "SHIFT over after %s: %d trip(s), %s essence handed to %q (%s/h), %d Nature runes carried home, the shelf cost %d gp and paid %d gp (%s), leftovers to the mule %v; died %v: %s%s",
		out.Took.Round(time.Second), out.Trips, runecraftplan.Thousands(out.Essence), b.set.Crafter, runecraftplan.Thousands(perHourOf(out.Essence, out.Took)), out.Natures,
		out.Paid, out.Earned, per, out.Delivered, out.Died, out.Why, wait)
}

// perHourOf is n an hour over d.
func perHourOf(n int, d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(float64(n) / d.Hours())
}

// compile-time: the live bot is a Bot.
var _ Bot = (*runeCarrierBot)(nil)
