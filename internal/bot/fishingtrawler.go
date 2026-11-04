package bot

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/trawlerplan"
	"rsb0t-go/internal/scripts/trawlerrunner"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("FishingTrawler", func() Bot { return &fishingTrawlerBot{kitOptional: true} })
}

// fishingTrawlerBot is the Fishing Trawler CREW role: buy a kit (Swamp paste, Rope, a Bailing bucket) at the Port
// Khazard general store, enter the lobby, cross the gangplank, keep the boat afloat (fill leaks, mend the net,
// bail the flooded hold), and in the last seconds log out of the world so the catch is shared by fewer players
// and pays the ANCHOR, the account that stays aboard to the end and claims it.
//
// Settings (trawlerplan.DecodeSettings; an unknown key is an error), all optional:
//
//	anchor              string default "master"  — stays aboard; crew leaves only while it is seen aboard
//	world               int    default 0        — world the game is played on (0: this account's home world)
//	kitPaste, kitRope   int    default 60, 0    — kitRope: the Rope of an account that cannot mend (Crafting under 40)
//	kitMendRope         int    default 8        — the Rope of a mender (Crafting 40+): the store, its bank, then the mule
//	bucket              bool   default true
//	leaveAtSecondsLeft  int    default 25       — 5..59 (the overlay only times the last minute)
//	maxFloodToLeave     int    default 60
//	alch                bool   default true     — High Level Alchemy in every wait (fire staff, own bank's natures)
//	mule                string default ""       — the mule asked for a fire staff and natures the bank lacks
//	logDetail           string default "Normal"
//
// PRESENCE (mulebus/trawler.go): every 15 s the crew member publishes its role on the wall bus -- waiting in the lobby,
// aboard, or aboard with the anchor seen -- and in the lobby it crosses the gangplank only on the anchor's board call
// (trawlerplan.ShouldCross), so no crew member starts a game without the anchor. The anchor (MasterCrafter's trawler round)
// counts the lobby on the same messages.
//
// THE CATCH: back from Port Sarim, a crew member picks the anchor's dumped fish off the floor by the net while its pack has
// room, banks them at the nearest bank (trawlerplan.NearestBank) and goes on (trawlerplan/catch.go says why the reference
// engine shows that floor to the anchor alone). Every wait -- the lobby, the walks, idle aboard -- casts High Level Alchemy
// (fleetrunner.Travel); the kit comes from the account's own bank, else the mule.
//
// Leaving is a world hop (Context.RequestWorldHop to the lowest of worlds 1..3 that is not the current one): the
// hop is a logout and a login, this script restarts on the other world, and the login on a trawler zone lands
// at Port Sarim (content trawler.rs2), so the next run walks back to Port Khazard, hops home and starts again.
// The script keeps no state across the hop: the phase is derived from the world and the tile each time.
//
// Not in any roster; the anchor is MasterCrafter's trawler round (mastercrafter_trawler.go).
type fishingTrawlerBot struct {
	settings trawlerplan.Settings
	walker   *webwalk.Walker
	travel   *fleetrunner.Travel

	published     time.Time // the last presence message
	bankChecked   time.Time // the last bank visit for the alchemy kit
	muleAsked     time.Time // the last mule ask for it
	muleFishGiven time.Time // the last walk of the raw manta and turtle to the mule (fishingtrawler_mulefish.go)
	pickups       int

	startedAt time.Time
	lastSaid  time.Time

	kitTries      int
	kitGaveUp     bool
	kitOptional   bool // a short store is no reason to wait or hop: board with what is held (owner: "ensure all accounts make it on")
	viewSaid      time.Time
	underSince    time.Time // when "Under 1 min" first showed in this game
	murphyMin     int       // Murphy's latest "N minutes left!" this game (0: not heard)
	murphyAt      time.Time // when it was first heard
	boardedSeen   time.Time // the first snapshot aboard this game
	anchorSeen    time.Time
	lastCross     time.Time
	leaveBlocked  time.Time
	lastPhase     trawlerplan.Phase
	trips, fixes  int
	leaks, bails  int
	boardedAt     time.Time
	lastBoatCatch string
	offWatch      bool      // the anchor's board call is for the other watch (trawlerplan.Watch)
	fareBankAt    time.Time // the last bank visit for the ship fares (fares)
	homeFails     int       // walks back to Port Khazard that failed in a row (Stranded)
	hopFails      int       // hops to the anchor's world refused in a row (Stranded)
	asideFailed   time.Time // the last walk to the aside spot that failed (asideRetry)
	bailRank      int       // this account's place among the crew aboard (trawlerplan.CrewRank), read every bailRankEvery
	bailRanked    time.Time
}

// trawlerBankKeepFree is the pack slots a mender's Rope draw leaves free at the bank for the alchemy kit, the coins and the catch: a
// single Rope takes a slot each.
const trawlerBankKeepFree = 6

// crafting is the account's Crafting level, 0 until the skill is read.
func (b *fishingTrawlerBot) crafting(ctx *Context) int {
	if sk, ok := ctx.Session.State().Skill(gemrunner.CraftingStat); ok {
		return sk.Level
	}
	return 0
}

// ropeShort is how many Rope this account is short of its kit (trawlerplan.RopeShort): the menders' kit, not the crew's.
func (b *fishingTrawlerBot) ropeShort(ctx *Context, d *fleetrunner.Deps) int {
	if trawlerplan.Looter(ctx.Username) {
		return 0 // never aboard (trawlerplan.LooterKit)
	}
	return trawlerplan.RopeShort(b.crafting(ctx), trawlerrunner.Held(d).Rope, b.settings, b.netCrew(ctx))
}

// ropeAsk is the Rope the mule is asked for: the shortfall of a mender holding under trawlerplan.MendRopeLow, else none.
func (b *fishingTrawlerBot) ropeAsk(ctx *Context, d *fleetrunner.Deps) int {
	if trawlerplan.Looter(ctx.Username) || !trawlerplan.NeedsRope(b.crafting(ctx), trawlerrunner.Held(d).Rope, b.settings) {
		return 0
	}
	return b.ropeShort(ctx, d)
}

// netCrew reports whether the account is one of the standing net crew: about one crew member in eight with Crafting
// NetCrafting+ (a mend rolls stat_random(crafting, 30, 300): about half at 40) waits on the net deck from boarding on (owner,
// 3 Nov 2026: "keep accounts at the net to fix right away instead of waiting for it to break").
func (b *fishingTrawlerBot) netCrew(ctx *Context) bool {
	sk, ok := ctx.Session.State().Skill(gemrunner.CraftingStat)
	return ok && sk.Level >= trawlerplan.NetCrafting && b.postKey(ctx)%8 == 0
}

// fares draws the coins the ships home cost from the nearest bank when the pack is short of them (fleetplan.DecideFare): the land
// way from Port Sarim or Karamja to Port Khazard goes by the wolf dens the walker avoids, so a crew member with no coins had no
// way back (live 3 Nov 2026 18:30: 104 accounts of other scripts and FishingTrawler logged "unreachable" every 15 s). ScarabSwarm
// crew have ScarabSwarm's own fares (scarabswarm_fares.go).
func (b *fishingTrawlerBot) fares(ctx *Context, d *fleetrunner.Deps, here tile.Tile) {
	if time.Since(b.fareBankAt) < 10*time.Minute {
		return
	}
	dest := trawlerplan.LobbyStand
	dec := fleetplan.DecideFare(fleetplan.FareInput{Here: &here, Dest: &dest, Coins: fleetrunner.CoinsHeld(d)})
	if dec.Action != fleetplan.FareDraw {
		return
	}
	b.fareBankAt = time.Now()
	bank := questplan.NearestBank(here)
	got, why := fleetrunner.WithdrawCoinsTo(d, bank.Tile, dec.Draw)
	b.say(ctx, "fares home: %s; drew at the %s bank: carrying %d %s", dec.Why, bank.Name, got, why)
}

// trawlerCrewCoins is the coins a crew member keeps for the ships home (live 3 Nov 2026: the end-of-game hop lands every crew
// member at Port Sarim, the ships to Kandarin cost about 60, and 15 crew with none were stranded); under trawlerCrewCoinsMin it
// draws them at its bank visit, or asks the mule.
const (
	trawlerCrewCoins    = 500
	trawlerCrewCoinsMin = 120
)

// trawlerHomeFails is the failed walks back in a row that make a crew member stranded, and trawlerStrandedWait how long a
// stranded FishingTrawler account waits before trying again.
const (
	trawlerHomeFails    = 8
	trawlerStrandedWait = 5 * time.Minute
)

// Stranded reports whether the crew member cannot get back to Port Khazard (trawlerHomeFails walks in a row failed).
func (b *fishingTrawlerBot) Stranded() bool {
	return b.homeFails >= trawlerHomeFails || b.hopFails >= trawlerHopFails
}

// trawlerHopFails is the refused hops to the anchor's world in a row that make a crew member stranded: a full world refuses the
// hop and the hand-off waits for a seat, and the crew loop's re-asks were refused "already in progress" every 20 s while the account
// did nothing (live 4 Nov 2026 15:25-16:25: 9, then 32 crew idle on worlds 2 and 3 with world 1 full). Stranded, it goes back to its
// own script for trawlerCrewStrandedSkip and tries again after.
const trawlerHopFails = 3

// asideRetry is how long a crew member whose walk to its aside spot failed waits where it stands before trying again.
const asideRetry = 3 * time.Minute

// bailRankEvery spaces the reads of the crew aboard that rank this account's bail level.
const bailRankEvery = 15 * time.Second

// rankAboard is the account's place by name among the crew aboard on its world (the anchor not counted), from the wall bus.
func (b *fishingTrawlerBot) rankAboard(ctx *Context) int {
	if ctx.Bus == nil {
		return -1
	}
	if time.Since(b.bailRanked) < bailRankEvery {
		return b.bailRank
	}
	b.bailRanked = time.Now()
	var names []string
	for _, v := range mulebus.ReadTrawler(ctx.Bus, time.Now(), 0) {
		// Bucket holders only (live 3 Nov 2026 17:42: every crew member ranked under 33 held no bucket, the water sat at 80-89
		// on the emergency bailers alone and the boat sank once they stopped for the end).
		if (v.Role != mulebus.TrawlerAboard && v.Role != mulebus.TrawlerAboardAnchor) || !v.Bucket || mulebus.NamesMatch(v.Name, b.settings.Anchor) {
			continue
		}
		if w := ctx.World(); w > 0 && v.World > 0 && v.World != w {
			continue
		}
		names = append(names, v.Name)
	}
	b.bailRank = trawlerplan.CrewRank(ctx.Username, names)
	return b.bailRank
}

func (b *fishingTrawlerBot) deps(ctx *Context) *fleetrunner.Deps {
	return &fleetrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Walker: b.walker,
		Log: ctx.Log, Loot: ctx.Loot, Me: ctx.Username, World: ctx.World(), Travel: b.travel,
	}
}

// trawlerTravel is the walk alch every trawler wait and walk carries: High Level Alchemy on the owner's alch stock with a fire
// staff, run kept on. Nil with alch off.
func trawlerTravel(on, members, crew bool) *fleetrunner.Travel {
	if !on {
		return nil
	}
	// A whole trawler session's casts (a user request: "fishing trawler accounts should bring more alchs"): about 400 casts
	// fit a game and its waits, and the crew walks to the mule rarely.
	alch := fleetplan.WalkAlchSettings{Enabled: true, MinMagic: 55, Items: fleetplan.DefaultAlchItems, MinValue: 1, Kit: 10000, Carry: 10000} // owner 2 Nov: "say 10k alches for each account including master"
	if crew {
		// The mule holds no strung bow or plate for the crew (master alchs those), so the crew alchs the mule's iron gear at a
		// loss (a user request: alch "Iron gear at a loss"), a share of it each: about 21,000 casts for ~350 crew.
		alch.Items = append(append([]string(nil), fleetplan.DefaultAlchItems...), "Iron full helm", "Iron kiteshield")
		alch.Kit, alch.Carry = trawlerCrewAlchKit, trawlerCrewAlchKit
	}
	// Teleports on every walk they save 60 tiles on (a user request: "master should beable to teleport to ardougne and to
	// camelot but doesnt"), whenever the pack holds the runes.
	return &fleetrunner.Travel{Alch: alch.Normalised(), KeepRunning: true, Members: members,
		Tele: fleetplan.TeleTravelSettings{Enabled: true, AnyWalk: true, AirRunes: true, MinSave: 60}}
}

// trawlerCrewAlchKit is the alchables a crew member asks the mule for: a game's waits, and a fair share of the mule's iron gear.
const trawlerCrewAlchKit = 150

// idleAlch casts once while the account waits and waits the cast out; otherwise it waits wait. It reports false on a stop.
func idleAlch(ctx *Context, t *fleetrunner.Travel, d *fleetrunner.Deps, wait time.Duration) bool {
	if t != nil && t.IdleAlch(d) {
		return ctx.Delay(t.IdleAlchWait())
	}
	return ctx.Delay(wait)
}

// trawlerWaterVarp is %trawler_water_overlay (varp 391): trawler_overlay.rs2 sets it to the boat's flood status every step.
// Unread it was -1, and a crew member on the flooded boat never left (ShouldLeave holds an unknown flood).
const trawlerWaterVarp = 391

// murphyMinutes reads Murphy's newest "N minutes left!" from the NPCs in view.
func (b *fishingTrawlerBot) murphyMinutes(ctx *Context) (int, bool) {
	best, ok := 0, false
	for _, n := range ctx.Session.State().NPCs() {
		for _, line := range n.SpeechLog {
			if m, mok := trawlerplan.ParseMurphyMinutes(line); mok && (!ok || m < best) {
				best, ok = m, true
			}
		}
	}
	return best, ok
}

// postKey spreads the crew over the hull posts by name (trawlerplan.HullPost).
func (b *fishingTrawlerBot) postKey(ctx *Context) int {
	h := 0
	for _, c := range strings.ToLower(ctx.Username) {
		h = h*31 + int(c)
	}
	return h
}

func (b *fishingTrawlerBot) say(ctx *Context, format string, args ...any) {
	b.lastSaid = time.Now()
	ctx.logf("trawler: "+format, args...)
}

// wantWorld is the world the game is played on: the setting, else this account's home world.
func (b *fishingTrawlerBot) wantWorld(ctx *Context) int {
	if b.settings.World > 0 {
		return b.settings.World
	}
	return ctx.HomeWorldNumber()
}

func (b *fishingTrawlerBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("trawler: no collision pack configured (fleet.config.json needs a packPath)")
	}
	set, err := trawlerplan.DecodeSettings(ctx.Settings)
	if err != nil {
		return err
	}
	b.settings = set
	b.travel = trawlerTravel(set.Alch, true, true)
	if w, werr := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); werr != nil {
		ctx.logf("trawler: no webwalk (%v) - walks fall back to plain pathfinding", werr)
	} else {
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{Client: w.Client, Session: ctx.Session, Config: ctx.Config, Components: ctx.Components, Log: ctx.Log}))
		}
		b.walker = w
	}
	b.startedAt = time.Now()
	b.lastSaid = b.startedAt
	// Every game costs a crew member two hops (out before the end so the catch pays the anchor, back to its world), one a 14-minute
	// game: the fleet's default bound of four in thirty minutes refused the leave hop and the account stayed aboard, splitting the
	// anchor's catch (live 4 Nov 2026: the 16:03, 16:55 and 17:48 claims held a half or a third, "hop budget spent (4 in 30m0s) --
	// staying home on world 1" at 12, 7 and 2 s left). OnStop restores the default.
	if ctx.SetHopBudget != nil {
		if err := ctx.SetHopBudget(trawlerCrewHops, 30*time.Minute); err != nil {
			ctx.logf("trawler: hop budget not raised: %v", err)
		}
	}
	ctx.logf("trawler: crew for anchor %q on world %d (kit %d paste, %d rope, bucket %v; leaving at %d s left)",
		set.Anchor, b.wantWorld(ctx), set.KitPaste, set.KitRope, set.Bucket, set.LeaveAtSecondsLeft)
	return nil
}

// trawlerCrewHops is a crew member's hop bound in thirty minutes: two games' two hops each, and room for a refused or repeated one.
const trawlerCrewHops = 12

func (b *fishingTrawlerBot) OnStop(ctx *Context) {
	if ctx.SetHopBudget != nil {
		_ = ctx.SetHopBudget(0, 0) // the fleet's default again (fleet.Manager.SetHopBudget)
	}
	casts := 0
	if b.travel != nil {
		casts, _, _, _ = b.travel.Counters()
	}
	ctx.logf("trawler: stopped - %d trip(s), %d leak(s) filled, %d net(s) mended, %d bail(s), %d dumped fish picked up, %d alch cast(s)", b.trips, b.leaks, b.fixes, b.bails, b.pickups, casts)
}

// snapshot reads the world into trawlerplan.Snapshot and keeps the "Under 1 min" clock and the anchor sighting.
func (b *fishingTrawlerBot) snapshot(ctx *Context, d *fleetrunner.Deps) trawlerplan.Snapshot {
	now := time.Now()
	here := trawlerrunner.Here(d)
	text := trawlerrunner.TimeText(d)
	if _, under, ok := trawlerplan.ParseTimeLeft(text); ok && under {
		if b.underSince.IsZero() {
			b.underSince = now
		}
	} else {
		b.underSince = time.Time{}
	}
	secs := -1
	if trawlerplan.OnBoat(here) {
		var since time.Duration
		if !b.underSince.IsZero() {
			since = now.Sub(b.underSince)
		}
		secs = trawlerplan.SecondsLeft(text, since)
		// The overlay is only refreshed within 8 tiles of the boat's centre (trawler_overlay.rs2) and froze at "8 mins" all
		// game long for the whole crew (live 2 Nov 2026: nobody left, the catch was split 266 ways). Murphy's "N minutes left!"
		// every 100 ticks and the time since boarding (the game ends about 639 s after the teleport aboard) bound it too.
		if n, ok := b.murphyMinutes(ctx); ok && n != b.murphyMin {
			b.murphyMin, b.murphyAt = n, now
		}
		if b.boardedSeen.IsZero() {
			b.boardedSeen = now
		}
		// Murphy's call when heard, else the boarding: live 3 Nov 2026 a 255-player game ran 80 s past the boarding estimate (the
		// server's ticks slow down), and the crew left a minute early.
		clocks := []int{trawlerplan.BoardSecondsLeft(now.Sub(b.boardedSeen))}
		if b.murphyMin > 0 {
			clocks = []int{trawlerplan.MurphySecondsLeft(b.murphyMin, now.Sub(b.murphyAt))}
		}
		for _, est := range clocks {
			if est >= 0 && (secs < 0 || est < secs) {
				secs = est
			}
		}
		// The anchor seen aboard, or saying so on the wall bus: with ~270 players on the boat the anchor can be outside the
		// players this client is sent, and the crew never left (live 3 Nov 2026, 00:23: "anchor aboard false" everywhere).
		if trawlerrunner.AnchorAboard(d, b.settings.Anchor) || mulebus.TrawlerAnchorAboard(mulebus.ReadTrawler(ctx.Bus, now, 30*time.Second), b.settings.Anchor) {
			b.anchorSeen = now
		}
	} else {
		b.murphyMin, b.boardedSeen = 0, time.Time{}
	}
	c := trawlerrunner.Held(d)
	views := mulebus.ReadTrawler(ctx.Bus, now, 0)
	watch := trawlerplan.Watch(ctx.Username)
	called := mulebus.TrawlerAnchorCalled(views, b.settings.Anchor, ctx.World(), watch)
	b.offWatch = !called && mulebus.TrawlerAnchorCalled(views, b.settings.Anchor, ctx.World(), 0)
	return trawlerplan.Snapshot{
		Here: here, World: ctx.World(),
		Paste: c.Paste, Rope: c.Rope, Bucket: c.Bucket, BucketFull: c.BucketFull,
		KitGaveUp: b.kitGaveUp, SecondsLeft: secs,
		AnchorAboard: !b.anchorSeen.IsZero() && now.Sub(b.anchorSeen) < 90*time.Second,
		FloodLevel:   ctx.Session.State().Varp(trawlerWaterVarp), // %trawler_water_overlay, the flood status every step
		GroundFish:   len(trawlerrunner.GroundFish(d)), FreeSlots: fleetrunner.FreeSlots(d), CatchHeld: trawlerrunner.CatchHeld(d),
		NeedBank:     b.needBank(ctx, d, now),
		BusKnown:     ctx.Bus != nil,
		AnchorCalled: called,
		AnchorAtSea:  !trawlerplan.OnBoat(here) && mulebus.TrawlerAnchorAboard(mulebus.ReadTrawler(ctx.Bus, now, 30*time.Second), b.settings.Anchor),
		Looter:       trawlerplan.Looter(ctx.Username),
	}
}

// trawlerKitEvery is how often a crew member short of fares or Rope goes to its bank for them, trawlerAlchKitEvery how often
// one without a fire staff, natures or alchables goes for them (and, the bank short, asks the mule), and trawlerMuleEvery how
// often it asks the mule. The alchemy kit's visit is a detour on the way back from Port Sarim, the return that sets how soon
// the crew's watch can sail again, and a bank without the kit has none at the next visit either (live 4 Nov 2026, 09:37 and
// 09:42: acct4 walked to the Yanille bank on each of its two logins of one return and found no staff or natures both times).
const (
	trawlerKitEvery     = 30 * time.Minute
	trawlerAlchKitEvery = 2 * time.Hour
	trawlerMuleEvery    = 30 * time.Minute
)

// trawlerBanked is each crew member's last bank visit by name, kept across the script restarts the end-of-game hop makes (a
// restarted crew loop starts with no memory of its own).
var trawlerBanked = struct {
	sync.Mutex
	at map[string]time.Time
}{at: map[string]time.Time{}}

// lastBank is the account's last bank visit, this loop's or an earlier one's.
func (b *fishingTrawlerBot) lastBank(ctx *Context) time.Time {
	trawlerBanked.Lock()
	defer trawlerBanked.Unlock()
	if at := trawlerBanked.at[strings.ToLower(ctx.Username)]; at.After(b.bankChecked) {
		return at
	}
	return b.bankChecked
}

// banked records a bank visit at now.
func (b *fishingTrawlerBot) banked(ctx *Context, now time.Time) {
	b.bankChecked = now
	trawlerBanked.Lock()
	trawlerBanked.at[strings.ToLower(ctx.Username)] = now
	trawlerBanked.Unlock()
}

// needBank says a bank visit is due: the fares or a mender's Rope short, not looked for in trawlerKitEvery; or the alchemy kit
// (alch on, Magic 55, and no fire staff worn, no natures or no alchables carried) not looked for in trawlerAlchKitEvery.
func (b *fishingTrawlerBot) needBank(ctx *Context, d *fleetrunner.Deps, now time.Time) bool {
	last := b.lastBank(ctx)
	if now.Sub(last) < trawlerKitEvery {
		return false
	}
	if fleetrunner.CoinsHeld(d) < trawlerCrewCoinsMin {
		return true // the fares home (trawlerCrewCoins)
	}
	if b.ropeAsk(ctx, d) > 0 {
		return true // a mender with no Rope left: its bank, then the mule (bank)
	}
	if now.Sub(last) < trawlerAlchKitEvery || b.travel == nil || fleetrunner.MagicLevel(d) < 55 {
		return false
	}
	natures, _, alchables, staff := b.travel.WalkAlchCarried(d)
	held := trawlerrunner.Held(d)
	return !staff || natures == 0 || alchables == 0 || held.Bucket+held.BucketFull == 0
}

// publish says where the crew member is on the wall bus every mulebus.TrawlerEvery: waiting in the lobby (or aside while the
// anchor is at sea), aboard, or aboard with the anchor seen. Other phases say nothing (the anchor counts the lobby only).
func (b *fishingTrawlerBot) publish(ctx *Context, phase trawlerplan.Phase, snap trawlerplan.Snapshot) {
	if ctx.Bus == nil || ctx.Username == "" || time.Since(b.published) < mulebus.TrawlerEvery {
		return
	}
	role := ""
	switch phase {
	case trawlerplan.PhaseLobby, trawlerplan.PhaseAside:
		// Aside is the lobby's watch waiting out the anchor's game 25 tiles off: counted, the anchor sees its next watch ready at
		// the claim, not only once back from the bank (live 4 Nov 2026, 12:19 and 12:32: no straight boarding while 158-163 waited).
		role = mulebus.TrawlerLobby
	case trawlerplan.PhaseAboard, trawlerplan.PhaseLeave:
		role = mulebus.TrawlerAboard
		if snap.AnchorAboard {
			role = mulebus.TrawlerAboardAnchor
		}
	default:
		return
	}
	b.published = time.Now()
	msg := mulebus.WithWatch(mulebus.TrawlerMessage(ctx.Username, snap.Here, role, ctx.World()), trawlerplan.Watch(ctx.Username))
	ctx.Bus.Publish(mulebus.WithBucket(msg, snap.Bucket+snap.BucketFull > 0))
}

func (b *fishingTrawlerBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !fleetrunner.WaitForGuardian(d) {
		return nil
	}
	if ctx.Session.State().IsDead() {
		if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
			ctx.Session.State().ClearDead()
		}
	}
	dlg := actions.NewChatDialog(ctx.Session, ctx.Components)
	if dlg.CanContinue() {
		_ = dlg.Continue()
		ctx.Delay(300 * time.Millisecond)
		return nil
	}

	set := b.settings
	set.World = b.wantWorld(ctx)
	// Rope is the menders' kit (trawlerplan.KitRopeFor): the store's 30 a world are not spent on crew who would only bail.
	set.KitRope = trawlerplan.KitRopeFor(b.crafting(ctx), set, b.netCrew(ctx))
	snap := b.snapshot(ctx, d)
	phase := trawlerplan.Decide(snap, set)
	if phase != b.lastPhase {
		if b.settings.Verbose() || phase == trawlerplan.PhaseAboard || phase == trawlerplan.PhaseLobby {
			b.say(ctx, "-> %s at %d,%d,%d (world %d)", phase, snap.Here.X, snap.Here.Z, snap.Here.Level, snap.World)
		}
		if phase == trawlerplan.PhaseAboard {
			b.boardedAt = time.Now()
			b.trips++
		}
		// The size of each pile and what a full pack leaves of it: how much of the catch the crew's pickup takes.
		if phase == trawlerplan.PhasePickup {
			b.say(ctx, "%d dumped fish on the floor by the net, %d free slot(s)", snap.GroundFish, snap.FreeSlots)
		} else if b.lastPhase == trawlerplan.PhasePickup && snap.GroundFish > 0 {
			b.say(ctx, "pack full: %d dumped fish left on the floor by the net", snap.GroundFish)
		}
		b.lastPhase = phase
	}
	b.publish(ctx, phase, snap)

	switch phase {
	case trawlerplan.PhaseKit:
		c, complete, err := trawlerrunner.BuyKit(d, set)
		b.kitTries++
		switch {
		case complete:
			b.kitTries = 0
			b.say(ctx, "kit ready: %d paste, %d rope, %d bucket", c.Paste, c.Rope, c.Bucket+c.BucketFull)
		case b.kitOptional:
			b.kitGaveUp = true
			b.say(ctx, "kit short (%d paste, %d rope, %d bucket; %v): boarding with what is held", c.Paste, c.Rope, c.Bucket+c.BucketFull, err)
		case b.kitTries >= 2:
			// This world's store is out (30 rope and 30 buckets a world): buy the rest on the next world, and only then
			// go home to wait on the pier ready (a user request).
			b.kitTries = 0
			next := ctx.World()%3 + 1
			b.say(ctx, "kit short (%d paste, %d rope, %d bucket; %v): this world's store is out, hopping to world %d to buy the rest", c.Paste, c.Rope, c.Bucket+c.BucketFull, err, next)
			if herr := ctx.RequestWorldHop(next, "trawler: buying the kit on another world's store"); herr != nil {
				b.say(ctx, "WARNING: world hop to %d refused: %v", next, herr)
				ctx.Delay(30 * time.Second)
			}
		default:
			b.say(ctx, "kit short (%d paste, %d rope, %d bucket; %v) - retrying", c.Paste, c.Rope, c.Bucket+c.BucketFull, err)
			ctx.Delay(5 * time.Second)
		}
	case trawlerplan.PhaseTravel:
		if !trawlerrunner.WalkTo(d, trawlerplan.LobbyStand, 2) {
			b.say(ctx, "WARNING: could not reach the lobby from %d,%d,%d - retrying", snap.Here.X, snap.Here.Z, snap.Here.Level)
			ctx.Delay(5 * time.Second)
		}
	case trawlerplan.PhaseLobby:
		b.lobby(ctx, d, snap)
	case trawlerplan.PhaseAside:
		spot := trawlerplan.AsideSpot(ctx.Username)
		// A walk that failed waits asideRetry before the next (live 3 Nov 2026 09:42: from the lobby deck one account's walk was
		// "unreachable" every 5 s): the account waits where it stands meanwhile.
		if (tile.Chebyshev(snap.Here, spot) > 3 || snap.Here.Level != spot.Level) && time.Since(b.asideFailed) >= asideRetry {
			b.say(ctx, "%s is at sea: waiting aside at %d,%d, out of the lobby's crowd", b.settings.Anchor, spot.X, spot.Z)
			if !trawlerrunner.WalkTo(d, spot, 2) {
				b.asideFailed = time.Now()
				b.say(ctx, "WARNING: could not reach the aside spot %d,%d from %d,%d,%d: waiting here", spot.X, spot.Z, snap.Here.X, snap.Here.Z, snap.Here.Level)
			}
			return nil
		}
		idleAlch(ctx, b.travel, d, 2*time.Second)
	case trawlerplan.PhaseLoot:
		if tile.Chebyshev(snap.Here, trawlerplan.LootStand) > 3 || snap.Here.Level != trawlerplan.LootStand.Level {
			if time.Since(b.asideFailed) >= asideRetry {
				b.say(ctx, "looter: waiting by the net at %d,%d for the catch %s relays", trawlerplan.LootStand.X, trawlerplan.LootStand.Z, b.settings.Anchor)
				if !trawlerrunner.WalkTo(d, trawlerplan.LootStand, 2) {
					b.asideFailed = time.Now()
					b.say(ctx, "WARNING: could not reach the loot stand from %d,%d,%d: waiting here", snap.Here.X, snap.Here.Z, snap.Here.Level)
				}
				return nil
			}
		}
		idleAlch(ctx, b.travel, d, 2*time.Second)
	case trawlerplan.PhaseAboard:
		b.aboard(ctx, d, snap)
	case trawlerplan.PhaseLeave:
		b.leave(ctx, snap)
	case trawlerplan.PhaseEscape:
		if err := trawlerrunner.Escape(d); err != nil {
			b.say(ctx, "WARNING: the boat sank: %v", err)
			ctx.Delay(2 * time.Second)
		} else {
			b.say(ctx, "the boat sank: climbed onto a barrel and swam to Port Sarim")
		}
	case trawlerplan.PhaseReturn:
		b.fares(ctx, d, snap.Here)
		b.say(ctx, "back from %d,%d - walking to Port Khazard", snap.Here.X, snap.Here.Z)
		if !trawlerrunner.WalkTo(d, trawlerplan.Stores[0], 6) {
			b.homeFails++
			if b.homeFails >= trawlerHomeFails {
				// Stranded (live 3 Nov 2026: no coins for the ships and none in the bank, or on Karamja with no bank at all, or shut
				// in a house): a crew member of another script goes back to it (Stranded, runner_trawler.go); a FishingTrawler
				// account tries again every trawlerStrandedWait rather than every 15 s.
				b.say(ctx, "WARNING: stranded at %d,%d: %d walks back to Port Khazard failed in a row - trying again in %s", snap.Here.X, snap.Here.Z, b.homeFails, trawlerStrandedWait)
				ctx.Delay(trawlerStrandedWait)
				return nil
			}
			b.say(ctx, "WARNING: could not walk back to Port Khazard from %d,%d - retrying", snap.Here.X, snap.Here.Z)
			ctx.Delay(10 * time.Second)
		} else {
			b.homeFails = 0
		}
	case trawlerplan.PhasePickup:
		if trawlerrunner.PickUpFish(d) {
			b.pickups++
			if b.pickups == 1 || b.pickups%50 == 0 {
				b.say(ctx, "picked up dumped fish by the net (%d this run)", b.pickups)
			}
		} else {
			ctx.Delay(time.Second)
		}
	case trawlerplan.PhaseBank:
		b.bank(ctx, d, snap)
	case trawlerplan.PhaseHome:
		b.say(ctx, "hopping from world %d to world %d to play", snap.World, set.World)
		if err := ctx.RequestWorldHop(set.World, "trawler: back to the game world"); err != nil {
			b.hopFails++
			b.say(ctx, "WARNING: world hop to %d refused (%d in a row): %v", set.World, b.hopFails, err)
			if b.Stranded() {
				return nil
			}
		} else {
			b.hopFails = 0
		}
		ctx.Delay(20 * time.Second)
	}
	return nil
}

// bank takes the fish to the nearest bank, draws the alchemy kit there, and asks the mule for what the bank lacked.
func (b *fishingTrawlerBot) bank(ctx *Context, d *fleetrunner.Deps, snap trawlerplan.Snapshot) {
	b.banked(ctx, time.Now())
	held := snap.CatchHeld
	at := trawlerplan.NearestBank(snap.Here)
	bank, err := trawlerrunner.BankCatch(d)
	if err != nil {
		b.say(ctx, "WARNING: banking: %v - retrying later", err)
		ctx.Delay(5 * time.Second)
		return
	}
	// A looter never sails: the trawler kit goes to the bank, and its slots to the catch (trawlerplan.LooterKit).
	if trawlerplan.Looter(ctx.Username) {
		_ = bank.BatchDepositAllMatching(func(it gamestate.SlotItem) bool { return trawlerplan.LooterKit(int(it.ID)) }, thieverrunner.Waiter(fleetrunner.Thiev(d)))
	}
	// The fares home first (DrawAlchKit closes the bank).
	if c := fleetrunner.CoinsHeld(d); c < trawlerCrewCoins {
		for _, it := range bank.Items() {
			if it.ID == fleetrunner.CoinsID {
				_ = bank.WithdrawX(fleetrunner.CoinsID, int32(min(int(it.Count), trawlerCrewCoins-c)), thieverrunner.Waiter(fleetrunner.Thiev(d)))
				break
			}
		}
	}
	// A mender's Rope from its own bank, before DrawAlchKit closes it.
	if want := b.ropeShort(ctx, d); want > 0 {
		if got := trawlerrunner.DrawRope(d, bank, want, trawlerBankKeepFree); got > 0 {
			b.say(ctx, "drew %d Rope from the bank for the net (%d wanted)", got, want)
		}
	}
	muleFish := b.muleFishBanked(d, bank)
	staff, natures := trawlerrunner.DrawAlchKit(d, bank, b.travel)
	if held > 0 {
		b.say(ctx, "banked %d trawler fish at %d,%d", held, at.X, at.Z)
	}
	if muleFish >= trawlerplan.MuleFishMin {
		b.giveMuleFish(ctx, d, muleFish)
	}
	// Nothing to alch is as much a reason to ask as no staff or natures (a user request: "doesnt appear that any other
	// account but master is alching while on the trawler": 112 of 177 crew had the staff and natures and no alchables, and
	// never asked).
	if b.travel != nil {
		held := trawlerrunner.Held(d)
		noBucket := held.Bucket+held.BucketFull == 0 && !trawlerplan.Looter(ctx.Username)
		if _, _, alchables, _ := b.travel.WalkAlchCarried(d); !staff || natures == 0 || alchables == 0 || noBucket || fleetrunner.CoinsHeld(d) < trawlerCrewCoinsMin || b.ropeAsk(ctx, d) > 0 {
			b.askMule(ctx, d, staff, natures)
		}
	}
}

// askMule asks the mule for a fire staff and the walk alch's natures and alchables (fleetplan.WalkAlchAskLines, filtered by the
// mule's published bank), and a mender for the Rope its bank did not hold (ropeAsk), at most every trawlerMuleEvery and only with the
// mule heard on this world. An account under Magic 55 asks for its Rope alone.
func (b *fishingTrawlerBot) askMule(ctx *Context, d *fleetrunner.Deps, staff bool, natures int) {
	mule := b.settings.Mule
	ropeWant := b.ropeAsk(ctx, d)
	alchOK := b.travel != nil && fleetrunner.MagicLevel(d) >= 55
	if mule == "" || ctx.Bus == nil || time.Since(b.muleAsked) < trawlerMuleEvery || (!alchOK && ropeWant == 0) {
		return
	}
	b.muleAsked = time.Now()
	p := scarabMulePresenceOf(ctx, mule)
	if !p.Heard || !p.SameWorld {
		b.say(ctx, "no fire staff, natures or Rope, and the mule cannot be asked: %s", p.Why)
		return
	}
	stock := muleBankCount(ctx, mule)
	var lines []fleetplan.AskLine
	if alchOK {
		n, fire, alchables, _ := b.travel.WalkAlchCarried(d)
		in := fleetplan.WalkAlchAskInput{Magic: fleetrunner.MagicLevel(d), Natures: n, Fire: fire, Alchables: alchables, FireStaff: staff,
			Mule: stock, AskAs: alchAskName(ctx.Config)}
		lines = fleetplan.WalkAlchAskLines(b.travel.Alch, in)
		// A bailing bucket from the mule's (live 3 Nov 2026 17:42: the store's 30 a world left most of the whole-fleet crew without
		// one, and the boat sank on too few bailers).
		if held := trawlerrunner.Held(d); held.Bucket+held.BucketFull == 0 && !trawlerplan.Looter(ctx.Username) {
			lines = append(lines, fleetplan.AskLine{Choices: []string{"Bailing bucket"}, Count: 1})
		}
		if c := fleetrunner.CoinsHeld(d); c < trawlerCrewCoinsMin {
			lines = append(lines, fleetplan.AskLine{Choices: []string{"Coins"}, Count: trawlerCrewCoins - c})
		}
		if !staff {
			lines = append(lines, fleetplan.AskLine{Choices: []string{"Staff of fire"}, Count: 1})
		}
	}
	// A mender's Rope (4 Nov 2026: the net froze for want of it; the store's 30 a world went round the whole crew).
	if ropeWant > 0 {
		lines = append(lines, fleetplan.AskLine{Choices: []string{"Rope"}, Count: ropeWant})
	}
	plan, none := fleetplan.FilterAskByBank(lines, stock)
	if len(plan) == 0 {
		b.say(ctx, "nothing to ask for: %s's published bank holds none of %v", mule, none)
		return
	}
	b.say(ctx, "asking %s for the kit (fire staff %v, %d natures carried, %d Rope wanted)", mule, staff, natures, ropeWant)
	if !fleetrunner.WalkTo(d, p.Post, 3, 8*time.Minute) {
		b.say(ctx, "WARNING: could not reach %s's post at %d,%d", mule, p.Post.X, p.Post.Z)
		return
	}
	keep := append([]string{"Swamp paste", "Rope", "Bailing bucket"}, b.travel.TravelKeep()...)
	res, info := fleetrunner.RequestKitBundle(d, ctx.Bus, ctx.Username, mule, fleetplan.AskPlanKit(plan), fleetrunner.KitBundleOptions{
		Bank: p.Post, Keep: keep, World: ctx.World(), Timeout: 12 * time.Minute, // the mule's queue runs 5-10 minutes
	})
	b.say(ctx, "%s handed %v (declined %v) %s", mule, res.Got, res.Declined, info.Reason)
	trawlerrunner.DrawAlchKit(d, nil, b.travel) // wields a staff the trade brought
}

func (b *fishingTrawlerBot) lobby(ctx *Context, d *fleetrunner.Deps, snap trawlerplan.Snapshot) {
	if !trawlerplan.ShouldCross(snap.BusKnown, snap.AnchorCalled) {
		if time.Since(b.lastSaid) >= trawlerplan.HeartbeatEvery {
			if b.offWatch {
				b.say(ctx, "waiting in the lobby: %s calls the other watch, this account is watch %d", b.settings.Anchor, trawlerplan.Watch(ctx.Username))
			} else {
				b.say(ctx, "waiting in the lobby for %s's board call (watch %d)", b.settings.Anchor, trawlerplan.Watch(ctx.Username))
			}
		}
		idleAlch(ctx, b.travel, d, 2*time.Second)
		return
	}
	if !trawlerplan.InLobby(snap.Here) && time.Since(b.lastCross) >= 25*time.Second {
		if err := trawlerrunner.Cross(d); err != nil {
			b.say(ctx, "WARNING: gangplank: %v", err)
		} else {
			b.lastCross = time.Now()
			b.say(ctx, "crossed the gangplank; waiting to be taken aboard")
		}
	}
	if time.Since(b.lastSaid) >= trawlerplan.HeartbeatEvery {
		b.say(ctx, "waiting in the lobby for the boat")
	}
	idleAlch(ctx, b.travel, d, 2*time.Second)
}

func (b *fishingTrawlerBot) aboard(ctx *Context, d *fleetrunner.Deps, snap trawlerplan.Snapshot) {
	held := trawlerrunner.Held(d)
	bailAt := trawlerplan.BailAt(b.rankAboard(ctx))
	bailing := trawlerplan.Bailing(trawlerplan.OnFloodedBoat(snap.Here), held.Bucket > 0 || held.BucketFull > 0, snap.SecondsLeft, snap.FloodLevel, bailAt)
	if snap.SecondsLeft >= 0 && snap.SecondsLeft <= trawlerplan.QuietSecondsLeft && !bailing {
		ctx.Delay(600 * time.Millisecond) // still for the logout (trawlerplan.QuietSecondsLeft)
		return
	}
	v := trawlerrunner.View(d)
	post := trawlerplan.HullPost(b.postKey(ctx), trawlerplan.OnFloodedBoat(v.Here))
	v.Post = &post
	if sk, ok := ctx.Session.State().Skill(gemrunner.CraftingStat); ok {
		v.Crafting = sk.Level
	}
	v.NetFixer = b.netCrew(ctx)
	v.FloodLevel, v.BailAt = snap.FloodLevel, bailAt
	act := trawlerplan.Aboard(v)
	if b.settings.Verbose() || time.Since(b.viewSaid) >= 15*time.Second {
		b.viewSaid = time.Now()
		ctx.logf("trawler: aboard %s at %d,%d,%d (flooded %v, water %d, bails at %d, secs left %d, anchor aboard %v, %d leak(s) seen, ripped %v, %d paste, %d rope, %d+%d bucket)",
			act.Kind, v.Here.X, v.Here.Z, v.Here.Level, trawlerplan.OnFloodedBoat(v.Here), v.FloodLevel, v.BailAt, snap.SecondsLeft, snap.AnchorAboard, len(v.Leaks), v.NetRipped,
			v.Paste, v.Rope, v.Bucket, v.BucketFull)
	}
	switch act.Kind {
	case trawlerplan.ActFillLeak:
		if trawlerrunner.FillLeak(d, act.Target) {
			b.leaks++
			b.say(ctx, "filled a leak at %d,%d (%d paste left)", act.Target.X, act.Target.Z, trawlerrunner.Held(d).Paste)
		}
	case trawlerplan.ActFixNet:
		if trawlerrunner.FixNet(d, act.Target) {
			b.fixes++
			b.say(ctx, "mended the net")
		}
	case trawlerplan.ActBail:
		if trawlerrunner.Bail(d) {
			b.bails++
		}
	case trawlerplan.ActEmpty:
		trawlerrunner.EmptyBucket(d)
	case trawlerplan.ActClimbUp:
		trawlerrunner.Climb(d, true)
	case trawlerplan.ActClimbDown:
		trawlerrunner.Climb(d, false)
	case trawlerplan.ActGoTo:
		fleetrunner.StepTo(d, act.Target)
	default:
		idleAlch(ctx, b.travel, d, 1500*time.Millisecond)
	}
	if time.Since(b.lastSaid) >= trawlerplan.HeartbeatEvery || (time.Since(b.lastSaid) >= 90*time.Second && snap.SecondsLeft >= 0 && snap.SecondsLeft < 120) {
		b.say(ctx, "aboard: %s left, catch %q, anchor aboard %v, %d leak(s) filled, %d net(s) mended",
			trawlerrunner.TimeText(d), trawlerrunner.Catch(d), snap.AnchorAboard, b.leaks, b.fixes)
	}
}

func (b *fishingTrawlerBot) leave(ctx *Context, snap trawlerplan.Snapshot) {
	if time.Now().Before(b.leaveBlocked) {
		ctx.Delay(2 * time.Second)
		return
	}
	to := trawlerplan.LeaveWorld(ctx.World())
	reason := fmt.Sprintf("trawler: leaving before the end so the catch pays %s", b.settings.Anchor)
	b.say(ctx, "%d s left and %s is aboard - leaving to world %d", snap.SecondsLeft, b.settings.Anchor, to)
	if err := ctx.RequestWorldHop(to, reason); err != nil {
		b.say(ctx, "WARNING: could not leave by world hop: %v - staying aboard", err)
		b.leaveBlocked = time.Now().Add(30 * time.Second)
	}
	// An accepted hop closes the session and replaces this script; wait for it rather than re-asking.
	ctx.Delay(5 * time.Second)
}
