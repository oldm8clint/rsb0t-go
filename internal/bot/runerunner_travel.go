package bot

// RuneRunner's travel (runerunnerplan/travel.go): the run staller, walk alchemy and teleports on every walk of its
// circuit, the jewellery kept through its bank deposits, and the kit asked of the mule right after a delivery.
//
// The walk itself is scarabrunner.WalkTo, which every RuneRunner walk already makes: with Deps.Travel set it starts a
// long walk with a teleport (teleportBeforeWalk, AnyWalk on for this script), casts High Level Alchemy or rubs the
// jewellery as it goes, and closes the rub's menu after it. Walks made straight through the Walker (hops, the door and
// stair crossings inside a walk) get the same aside through Walker.Prepare (WalkPrepare).

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/orderplan"
	"rsb0t-go/internal/scripts/runerunnerplan"
	"rsb0t-go/internal/walkstall"
	"rsb0t-go/internal/webwalk"
)

// runeRunnerTravel builds the account's Travel from its settings; nil when all three are off.
func runeRunnerTravel(set runerunnerplan.Resolved) *fleetrunner.Travel {
	if !set.WalkAlch && !set.WalkRub && !set.TeleTravel {
		return nil
	}
	alch := fleetplan.WalkAlchSettings{Enabled: set.WalkAlch, MinMagic: 55, MinValue: 1, Kit: runerunnerplan.TravelAlchKit}
	// The shared teleport defaults (teleport-walks, 13 Oct 2026): the price of a cast in ticks, the caps and the law price
	// are the fleet's, not RuneRunner's own. It has always considered every walk (AnyWalk) and spent pack air.
	tele := fleetplan.TeleTravelSettings{
		Enabled: set.TeleTravel, AirRunes: true, Kit: runerunnerplan.TravelTeleKit,
		MuleLawFloor: 1000, AnyWalk: true, TripLaws: fleetplan.DefaultTripLaws, TripLawFloor: fleetplan.DefaultTripLawFloor,
	}
	return &fleetrunner.Travel{Alch: alch.Normalised(), Tele: tele.Normalised(), Rub: set.WalkRub, Members: true, Tick: set.Tick()}
}

// installTravel builds the Travel and gives the Walker its asides: run on at 20% energy (the stall with no rub of its
// own) composed with the Travel's walk alch and rub, and its teleport decision (webwalk.Walker.BeforeFollow, a seam of its
// own that the composed Prepare does not touch).
func (b *runeRunnerBot) installTravel(ctx *Context) {
	b.travel = runeRunnerTravel(b.set)
	if b.travel != nil {
		b.travel.MuleLaws = func() (int, bool) {
			if count := muleBankCount(ctx, b.set.Mule); count != nil {
				return count("Law rune")
			}
			return 0, false
		}
	}
	if b.walker == nil {
		return
	}
	b.travel.InstallTeleportHook(b.walker, func() *fleetrunner.Deps { return b.deps(ctx) })
	ws := &walkStall{stall: walkstall.New(false), label: "runerunner"}
	var other func(o *webwalk.WalkOptions) func()
	if b.travel != nil {
		other = b.travel.WalkPrepare(func() *fleetrunner.Deps { return b.deps(ctx) })
	}
	ws.install(ctx, b.walker, other)
}

// refreshTravel gates walk alchemy on the mule's Nature stock (runerunnerplan.AlchSpare), once per round and at start,
// and says so when the answer changes. Teleports and the rub are not gated: they cost a law rune and a necklace.
func (b *runeRunnerBot) refreshTravel(ctx *Context) {
	if b.travel == nil || !b.set.WalkAlch {
		return
	}
	n, known := 0, false
	if count := muleBankCount(ctx, b.set.Mule); count != nil {
		n, known = count("Nature rune")
	}
	spare, why := runerunnerplan.AlchSpare(n, known)
	if spare != b.travel.Alch.Enabled || b.travelSaid.IsZero() {
		b.travelSaid = time.Now()
		if spare {
			b.logf(ctx, "travel: walk alch on: the mule holds %d Nature runes; teleports=%v rub=%v", n, b.set.TeleTravel, b.set.WalkRub)
		} else {
			b.logf(ctx, "travel: walk alch off: %s; teleports=%v rub=%v", why, b.set.TeleTravel, b.set.WalkRub)
		}
	}
	b.travel.Alch.Enabled = spare
}

// travelKitInput reads the pack and the mule's published bank into a runerunnerplan.TravelKitInput.
func (b *runeRunnerBot) travelKitInput(ctx *Context, d *fleetrunner.Deps) runerunnerplan.TravelKitInput {
	t := b.travel
	staff, worn := fleetrunner.StaffOnHand(d)
	if !worn {
		staff = ""
	}
	natures, fire, alchables, fireStaff := t.WalkAlchCarried(d)
	return runerunnerplan.TravelKitInput{
		Alch: t.Alch, Tele: t.Tele, Rub: t.Rub, RubHeld: fleetrunner.RubItemHeld(d), Members: t.Members,
		Magic: fleetrunner.MagicLevel(d), Natures: natures, Fire: fire, Alchables: alchables, FireStaff: fireStaff,
		Pack: fleetrunner.PackRunes(d), Staff: staff, QuestDone: fleetrunner.QuestDoneFunc(d),
		Mule: muleBankCount(ctx, b.set.Mule), AskAs: alchAskName(ctx.Config),
	}
}

// askTravelKit asks the mule for the travel kit on a delivery that has just traded: the account stands at the mule's
// post with an emptied pack, so it costs one more trade and no walk. At most every runerunnerplan.KitAskEvery.
func (b *runeRunnerBot) askTravelKit(ctx *Context, d *fleetrunner.Deps) {
	if b.travel == nil || ctx.Bus == nil || time.Now().Before(b.kitAskAt) {
		return
	}
	in := b.travelKitInput(ctx, d)
	if in.Mule == nil {
		return // the mule's bank is not published, or the reading is stale: nothing to judge by
	}
	plan, none, notes := runerunnerplan.TravelKit(in)
	b.kitAskAt = time.Now().Add(runerunnerplan.KitAskEvery)
	for _, n := range notes {
		b.logf(ctx, "travel kit: %s", n)
	}
	if len(plan) == 0 {
		if len(none) > 0 {
			b.logf(ctx, "travel kit: %q's published bank holds none of %s", b.set.Mule, strings.Join(none, ", "))
		}
		return
	}
	var kit []orderplan.KitLine
	var parts []string
	for _, p := range plan {
		if len(p.Rungs) == 0 || p.Rungs[0].Count <= 0 {
			continue
		}
		r := p.Rungs[0]
		kit = append(kit, orderplan.KitLine{Item: r.Item, Qty: r.Count, Noted: b.travel.KitKeepsNoted(r.Item)})
		parts = append(parts, fmt.Sprintf("%d %s", r.Count, r.Item))
	}
	if len(kit) == 0 {
		return
	}
	b.logf(ctx, "travel kit: asking %q for %s on the delivery (teleports=%v walk alch=%v rub=%v)",
		b.set.Mule, strings.Join(parts, ", "), in.Tele.Enabled, in.Alch.Enabled, in.Rub)
	res := donorrunner.AwaitKit(&donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}, donorrunner.KitAsk{Mule: b.set.Mule, Me: ctx.Username, Lines: kit, Bus: ctx.Bus})
	b.logf(ctx, "travel kit: the mule handed over %v (%s)", res.Got, strings.TrimSpace(res.Reason))
}

// travelLog reports the session's travel every ten minutes while it did anything.
func (b *runeRunnerBot) travelLog(ctx *Context) {
	if b.travel == nil || time.Since(b.travelLogged) < 10*time.Minute {
		return
	}
	casts, xp, tps, saved := b.travel.Counters()
	if casts == 0 && tps == 0 && b.travel.Rubs() == 0 {
		return
	}
	b.travelLogged = time.Now()
	b.logf(ctx, "travel: %d alchs while walking (%d Magic xp), %d travel teleports (about %d tiles saved), %d rubs this session", casts, xp, tps, saved, b.travel.Rubs())
}
