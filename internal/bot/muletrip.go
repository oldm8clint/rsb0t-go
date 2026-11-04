package bot

// MuleReceiver travelling to where a pickup was asked from, serving one camp's requests as a
// batch before moving on.
//
// The account owner's own instruction (2026-09-20): "the mule script should navigate to where the
// request was made — for the fishers, likely Catherby bank; for wood cutters, Seers' Village
// bank", then "go ahead and do batch requests". Everything in this file is behind the
// travelToRequests setting, which defaults to OFF: with it off b.trip is always nil and every
// branch here is skipped, so a roster that names no new key behaves exactly as it did.
//
// What was already built and simply never connected: mulereceiverplan.PlanTrip (the whole trip
// decision, six branches, tested), mulereceiverplan.PickupQueue (the batching substrate, already
// filled by pollBus) and mulebus.ComingMessage (the "on my way, eta n" wire shape). What was
// missing was anything calling them, and a donor that would wait — see donorplan.MeetTimeoutComing.
//
// Deliberately NOT built, stated plainly rather than half-done: a teleport trip. PlanTrip can
// choose one and this file refuses it out loud, because casting a teleport is an interaction this
// bot has never performed, and a teleport that half-works strands the mule away from the bank
// every donor is waiting at — the exact failure PlanTrip's own "both legs or nothing" rule exists
// to prevent. Walking covers the case users asked for: the two banks this fleet uses are 84
// tiles apart, well inside the default walk radius.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/phaselog"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/mulerunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

const (
	// magicStat is the Magic skill's index, as alchrunner/combatrunner already name it.
	magicStat = 6
	// muleCampArriveRadius is how close to the camp counts as arrived. A donor steps over to a
	// mule it can see (donorplan.SightRange is 15) and trades from donorplan.MeetRange, so the
	// mule does not need the donor's exact tile — but it must be close enough to be seen.
	muleCampArriveRadius = 3
	// comingRepeat re-publishes the "I am coming" promise while travelling, so a donor that
	// started its hand-off after the mule set off still hears one.
	comingRepeat = 5 * time.Second
	// campGlance bounds one trade wait while a trip is on, so the drain and dwell checks run
	// often instead of once per full idle timeout.
	campGlance = 15 * time.Second
)

// travelSettings is the batching configuration a roster can set. Its zero value is the shipped
// default: travelling OFF, which is what makes "a roster that names no new key behaves exactly as
// it did" a property of the code rather than a promise.
type travelSettings struct {
	on             bool
	walkRadius     int
	maxLegTiles    int
	campRadius     int
	maxCampsInARow int
	dwell          time.Duration
}

// Defaults for the batching settings. walkRadius is deliberately larger than
// mulereceiverplan.MuleDefaults' own 50: PlanTrip walks anything inside the radius and otherwise
// needs a castable teleport for BOTH legs, so a mule with no Magic level and a 50-tile radius
// would refuse the 84-tile trip between the Catherby and Seers' Village banks — the exact trip
// this feature exists to make. 150 covers that pair with room and still refuses a cross-map haul.
const (
	defaultWalkRadius     = 150
	defaultCampRadius     = 25
	defaultCampDwell      = 3 * time.Minute
	defaultMaxCampsInARow = 3
	minCampDwell          = 30 * time.Second
)

// readTravelSettings reads the batching keys off the roster, clamped so a typo cannot produce a
// mule that travels nowhere or never comes back.
func readTravelSettings(m map[string]interface{}) travelSettings {
	s := travelSettings{
		on:             BoolSetting(m, "travelToRequests", false),
		walkRadius:     IntSetting(m, "walkRadius", defaultWalkRadius),
		maxLegTiles:    IntSetting(m, "maxLegTiles", mulereceiverplan.MuleDefaults().MaxLegTiles),
		campRadius:     IntSetting(m, "campRadius", defaultCampRadius),
		maxCampsInARow: IntSetting(m, "maxCampsInARow", defaultMaxCampsInARow),
		dwell:          time.Duration(IntSetting(m, "campDwellSec", int(defaultCampDwell/time.Second))) * time.Second,
	}
	if s.walkRadius < 1 {
		s.walkRadius = 1
	}
	if s.maxLegTiles < 1 {
		s.maxLegTiles = 1
	}
	if s.campRadius < 0 {
		s.campRadius = 0
	}
	if s.maxCampsInARow < 0 {
		s.maxCampsInARow = 0
	}
	if s.dwell < minCampDwell {
		s.dwell = minCampDwell
	}
	return s
}

// muleTrip is one journey to one camp.
type muleTrip struct {
	camp mulereceiverplan.Camp
	// startedAt and budget bound the walk; arrived flips once the mule is standing there.
	startedAt time.Time
	budget    time.Duration
	arrived   bool
	// until is when the dwell cap expires, set on arrival rather than at departure so travel
	// time never eats the time spent actually serving.
	until      time.Time
	nextComing time.Time
}

// travelling reports whether a trip is under way at all.
func (b *muleBot) travelling() bool { return b.trip != nil }

// busState is what the heartbeat says this mule is doing. With travelToRequests off this is
// always StateHome, exactly what announce published before this file existed.
func (b *muleBot) busState(ctx *Context) mulebus.MuleState {
	if b.trip != nil {
		if b.trip.arrived {
			return mulebus.StateMeeting
		}
		return mulebus.StateOutbound
	}
	if b.travel && b.homeTile != nil {
		if here := currentTile(ctx); here != nil && !atPost(*here, *b.homeTile) {
			return mulebus.StateHomebound
		}
	}
	return mulebus.StateHome
}

// casterWorld reads the caster fresh for one trip decision: the Magic level, the quests that gate
// a teleport, what is worn (a staff pays for a rune) and what runes the pack holds. PlanTrip
// refuses a trip the pack cannot pay for both ways, so these readings are what stands between the
// mule and stranding itself away from its own bank.
func (b *muleBot) casterWorld(ctx *Context) mulereceiverplan.CasterWorld {
	st := ctx.Session.State()
	magic := 0
	if sk, ok := st.Skill(magicStat); ok {
		magic = sk.Level
	}
	var worn []string
	if ctx.Components != nil && ctx.Config != nil {
		for _, it := range actions.NewEquipment(ctx.Session, ctx.Components).Worn() {
			if obj, ok := ctx.Config.Obj(int(it.ID)); ok {
				worn = append(worn, obj.Name)
			}
		}
	}
	return mulereceiverplan.CasterWorld{
		MagicLevel: magic,
		QuestStatus: func(name string) string {
			if status, ok := quests.Status(st, name); ok && status == quests.Complete {
				return mulereceiverplan.QuestComplete
			}
			return ""
		},
		Worn: worn,
		Held: func(rune string) int {
			inv := st.Inventory(mulerunner.PlayerPack)
			if inv == nil || ctx.Config == nil {
				return 0
			}
			n := 0
			for _, it := range inv.NonEmpty() {
				obj, ok := ctx.Config.Obj(int(it.ID))
				if !ok || !strings.EqualFold(obj.Name, rune) {
					continue
				}
				c := int(it.Count)
				if c < 1 {
					c = 1
				}
				n += c
			}
			return n
		},
	}
}

// manageTrip is the whole batching decision, run once per Loop call while travelToRequests is on.
// It reports whether the trip state changed, so Loop can hand control straight back rather than
// sitting in a full idle wait before setting off.
func (b *muleBot) manageTrip(ctx *Context) bool {
	if b.pickups == nil || ctx.Bus == nil {
		return false
	}
	now := time.Now()
	camps := mulereceiverplan.GroupByCamp(b.pickups.Pending(now), b.campRadius)

	if b.trip != nil {
		// A request that arrived at this camp while the mule was travelling joins the batch by
		// simply being in the camp's own list now — that is what "batched by camp" means, and
		// why the camp is re-read every call rather than frozen at departure.
		var still *mulereceiverplan.Camp
		for i := range camps {
			if mulereceiverplan.SameCamp(camps[i], b.trip.camp.At, b.campRadius) {
				still = &camps[i]
				break
			}
		}
		switch {
		case still == nil:
			b.endTrip(ctx, "every request here is served or has gone quiet")
		case b.trip.arrived && now.After(b.trip.until):
			// The batch outlived the donors' own patience. Whoever is left gives up and comes
			// back after donorplan.HandoffRetry, and their re-ask is then the oldest in the
			// queue, so the next trip to this camp serves them first.
			b.trip.camp = *still
			b.endTrip(ctx, fmt.Sprintf("the %s dwell cap ran out with %d request(s) still waiting", b.campDwell, len(still.Requests)))
		default:
			b.trip.camp = *still
			if !b.trip.arrived && now.After(b.trip.nextComing) {
				b.announceComing(ctx)
			}
			return false
		}
	}

	if len(camps) == 0 {
		return b.trip == nil && false
	}
	here := currentTile(ctx)
	if here == nil {
		return false
	}
	choice, ok := mulereceiverplan.ChooseCamp(mulereceiverplan.ChooseCampInput{
		Camps: camps, Here: *here, ServedAt: b.servedAt, InARow: b.servedInARow,
		MaxInARow: b.maxCampsInARow, SameCampRadius: b.campRadius,
	})
	if !ok {
		return false
	}
	// Already standing in the chosen camp: there is nothing to travel for, and the ordinary
	// trade loop serves it. This is the whole of the fixed-post case — a mule whose donors come
	// to its own bank never starts a trip at all.
	if mulereceiverplan.SameCamp(choice.Camp, *here, b.campRadius) {
		return false
	}
	return b.startTrip(ctx, choice, *here)
}

// startTrip asks PlanTrip how to reach the camp and either sets off or says why it will not.
func (b *muleBot) startTrip(ctx *Context, choice mulereceiverplan.CampChoice, here tile.Tile) bool {
	camp := choice.Camp
	plan := mulereceiverplan.PlanTrip(mulereceiverplan.PlanTripInput{
		Me: here, Home: b.postTile(), Target: camp.At,
		NearRadius: b.walkRadius, MaxLegTiles: b.maxLegTiles, World: b.casterWorld(ctx),
	})
	refuse := func(reason string) bool {
		ctx.logf("mule: not going to the pickup camp at %d,%d,%d (%d request(s): %s): %s",
			camp.At.X, camp.At.Z, camp.At.Level, len(camp.Requests), strings.Join(camp.Names(), ", "), reason)
		// Telling them beats leaving them to stand for their whole timeout for an answer
		// already decided, and dropping the requests stops this camp being chosen again on
		// every single Loop call.
		for _, r := range camp.Requests {
			ctx.Bus.Publish(mulebus.DeclinedMessage(ctx.Username, r.From, reason))
			b.pickups.Drop(r.From)
		}
		return true
	}
	switch plan.Mode {
	case mulereceiverplan.TripDecline:
		return refuse(plan.Reason)
	case mulereceiverplan.TripTeleport:
		return refuse(fmt.Sprintf("it would need a %s teleport and this mule does not cast (walk radius is %d tiles, the camp is %d away)",
			plan.Spell.ID, b.walkRadius, tile.Chebyshev(here, camp.At)))
	}
	b.trip = &muleTrip{camp: camp, startedAt: time.Now(), budget: mulereceiverplan.LegTimeout(plan.Tiles)}
	ctx.logf("mule: going to the pickup camp at %d,%d,%d for %d request(s) (%s) — %d tiles, %s; chose it because %s",
		camp.At.X, camp.At.Z, camp.At.Level, len(camp.Requests), strings.Join(camp.Names(), ", "), plan.Tiles, plan.Note, choice.Reason)
	b.announceComing(ctx)
	return true
}

// endTrip closes the current trip and records where it was, for the starvation cap.
func (b *muleBot) endTrip(ctx *Context, why string) {
	if b.trip == nil {
		return
	}
	at := b.trip.camp.At
	if b.servedAt != nil && mulereceiverplan.SameCamp(b.trip.camp, *b.servedAt, b.campRadius) {
		b.servedInARow++
	} else {
		b.servedInARow = 1
	}
	b.servedAt = &at
	ctx.logf("mule: leaving the pickup camp at %d,%d,%d (%d batch(es) here in a row) — %s", at.X, at.Z, at.Level, b.servedInARow, why)
	b.trip = nil
	b.returningHome = true
}

// announceComing publishes the promise that lets a donor wait: without it every donor at the camp
// gives up donorplan.MeetTimeout after drawing its load, which is always before a mule walking
// tens of tiles could arrive. See donorplan.MeetTimeoutComing.
func (b *muleBot) announceComing(ctx *Context) {
	if b.trip == nil || ctx.Bus == nil {
		return
	}
	eta := time.Until(b.trip.startedAt.Add(b.trip.budget))
	if eta < 0 {
		eta = 0
	}
	for _, r := range b.trip.camp.Requests {
		ctx.Bus.Publish(mulebus.ComingMessage(ctx.Username, r.From, mulebus.TravelWalk, eta))
	}
	b.trip.nextComing = time.Now().Add(comingRepeat)
}

// atCampOrTravel is ensureHome's trip branch: stand at the camp, or walk to it.
func (b *muleBot) atCampOrTravel(ctx *Context, here, camp tile.Tile) bool {
	if b.trip.arrived || (here.Level == camp.Level && tile.Chebyshev(here, camp) <= muleCampArriveRadius) {
		if !b.trip.arrived {
			b.trip.arrived = true
			b.trip.until = time.Now().Add(b.campDwell)
			ctx.logf("mule: at the pickup camp %d,%d,%d after %s — serving %d request(s) for up to %s",
				camp.X, camp.Z, camp.Level, time.Since(b.trip.startedAt).Round(time.Second), len(b.trip.camp.Requests), b.campDwell)
			ctx.SetPhase(phaselog.Idle)
		}
		return true
	}
	return b.travelToCamp(ctx, camp)
}

// travelToCamp walks one leg to the camp, wolf territory avoided exactly as the walk home is (see
// mulehome.go: a combat-3 mule does not survive White Wolf Mountain). A leg that blows its budget
// gives the trip up and tells the donors, rather than walking forever.
func (b *muleBot) travelToCamp(ctx *Context, camp tile.Tile) bool {
	ctx.SetPhase(phaselog.Walking)
	if ctx.Pack == nil {
		b.abandonTrip(ctx, "no collision pack configured, so the mule cannot walk anywhere")
		return false
	}
	if b.walker != nil {
		res := b.walker.WalkTo(wwPoint(camp), webwalk.WalkOptions{
			Radius: muleCampArriveRadius, Timeout: b.trip.budget, AvoidRects: muleWolfRects(),
		})
		if res.Arrived() {
			return b.atCampOrTravel(ctx, camp, camp)
		}
		if res.Outcome == webwalk.OutcomeStopped {
			return false
		}
		ctx.logf("mule: the walk to the pickup camp did not arrive (%v: %s) at %v", res.Outcome, res.Reason, res.End)
	} else if freshstartrunner.WalkNear(muleFreshStartDeps(b.deps(ctx), b.walker), camp, muleCampArriveRadius, b.trip.budget) {
		return b.atCampOrTravel(ctx, camp, camp)
	}
	if time.Since(b.trip.startedAt) > b.trip.budget {
		b.abandonTrip(ctx, fmt.Sprintf("could not reach the camp within %s", b.trip.budget))
	}
	return false
}

// abandonTrip gives up a journey, tells everyone waiting for it and drops their requests so the
// same unreachable camp is not chosen again on the next call.
func (b *muleBot) abandonTrip(ctx *Context, reason string) {
	if b.trip == nil {
		return
	}
	for _, r := range b.trip.camp.Requests {
		if ctx.Bus != nil {
			ctx.Bus.Publish(mulebus.DeclinedMessage(ctx.Username, r.From, reason))
		}
		b.pickups.Drop(r.From)
	}
	b.endTrip(ctx, reason)
}
