package bot

// MasterCrafter's rounds (mastercrafter.go switches to them at a safe point). Every round works at
// the MULE'S OWN POST, where its bank is, so the ask, the bank, the work and the hand-over need no
// walking between them: the mule's heartbeat gives the post (scarabMulePresenceOf), the kit ask is
// ScarabSwarm's bundled one (scarabrunner.RequestKitBundle), the hand-over RuneRunner's
// (runerunner.Deliver, a donorrunner trade the mule banks).
//
//	gem round   visit the bank (deposit all, read), ask the mule for every cuttable kind of uncut gem
//	            it holds (and a chisel when none is held), then loop: bank visit (deposit the cut
//	            gems, draw a chisel and 27 of the best uncut gem the Crafting level allows), cut them
//	            five a game tick (gemrunner.CutAll), until the bank holds none; hand every cut and
//	            crushed gem to the mule. Every visit after the first is a quick one
//	            (mastercraftrunner.QuickBank) and the cut is pipelined (gemrunner.UseAll): seven
//	            ticks a load of 27.
//	pearl round mastercrafter_pearls.go: every oyster pearl and Bolts the mule holds, cut into
//	            tips, tipped into Pearl bolts, handed back.
//	store round mastercrafter_storage.go: the cold, small rows of a nearly full mule bank asked for
//	            and banked here, in this account's own bank.
//	bow round   BowStringer itself (bowstringer_embed.go): bank, draw 14 bows and 14 strings, string
//	            them five a tick, order a kit when the bank runs dry, until the mule has none to give
//	            or defers; hand every strung bow to the mule.
//	smith round mastercrafter_smith.go: the Steel and better bars into arrowtips, the Iron bars into
//	            platebodies, at the Varrock west anvil.
//	deliver     hand the product an earlier round left in the bank to the mule.
//
// A round never ends the run, logs the account out or stops the swarm for good: when it is over,
// whatever the reason, the caller starts ScarabSwarm again.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/pearlplan"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/tile"
)

// masterWalkTo is the walk to the mule's post (scarabrunner.WalkTo unless a test replaced it).
var masterWalkTo = fleetrunner.WalkTo

// masterDeliver is runerunner.Deliver, the trade to the mule, unless a test replaced it.
var masterDeliver = runerunner.Deliver

// masterFastMaxLoads bounds one six-tick run (about six minutes), so the pace line comes now and
// then; the next level and the bank's stock bound it too.
const masterFastMaxLoads = 100

// masterFastMaxDesyncs is how many six-tick runs in a row may fall out of step before a round makes
// only 7-tick loads.
const masterFastMaxDesyncs = 3

// masterHandoverPasses bounds how many draw-and-trade passes one hand-over makes: a product with no
// bank note (crushed gemstones) takes a slot a unit, so a big haul crosses in several loads.
const masterHandoverPasses = 6

// masterClearDead clears the one-way dead latch once the account has hitpoints again, which
// ScarabSwarm's own Loop does on every pass and a round has to do for itself: a death near the
// bank leaves the account respawned and walking, with the latch still set.
func masterClearDead(ctx *Context) {
	st := ctx.Session.State()
	if !st.IsDead() {
		return
	}
	if hp, ok := st.HP(); ok && hp > 0 {
		st.ClearDead()
	}
}

// runRound is the real round: the phase the panel shows, then the mode's own.
func (b *masterCrafterBot) runRound(ctx *Context, dec mastercraftplan.Decision) masterRoundOutcome {
	masterClearDead(ctx)
	d := b.deps(ctx)
	ctx.SetPhase("mastercraft/" + string(dec.Mode))
	started := time.Now()
	switch dec.Mode {
	case mastercraftplan.ModeNature: // mastercrafter_nature.go
		return b.natureRound(ctx, d, started, dec)
	case mastercraftplan.ModeTrawler: // mastercrafter_trawler.go
		return b.trawlerRound(ctx, d, started, dec)
	case mastercraftplan.ModeRunecraft: // mastercrafter_runecraft.go
		return b.runecraftRound(ctx, d, started, dec)
	case mastercraftplan.ModeChest: // mastercrafter_chest.go
		return b.chestRound(ctx, d, started, dec)
	case mastercraftplan.ModeJewellery: // mastercrafter_jewellery.go
		return b.jewelleryRound(ctx, d, started, dec)
	case mastercraftplan.ModeGems:
		return b.gemRound(ctx, d, started)
	case mastercraftplan.ModeBows:
		return b.bowRound(ctx, d, started)
	case mastercraftplan.ModePearls:
		return b.pearlRound(ctx, d, started)
	case mastercraftplan.ModeStore: // mastercrafter_storage.go
		return b.storeRound(ctx, d, started)
	case mastercraftplan.ModeShop:
		return b.shopRound(ctx, d, started, dec)
	case mastercraftplan.ModeSuperheat:
		return b.superheatRound(ctx, d, started, dec)
	case mastercraftplan.ModeSmith: // mastercrafter_smith.go
		return b.smithRound(ctx, d, started, dec)
	case mastercraftplan.ModePlatebody: // mastercrafter_platebody.go
		return b.plateRound(ctx, d, started, dec)
	case mastercraftplan.ModeStaves: // mastercrafter_staves.go
		return b.stavesRound(ctx, d, started, dec)
	case mastercraftplan.ModeBones: // mastercrafter_bones.go
		return b.bonesRound(ctx, d, started, dec)
	case mastercraftplan.ModeCannonballs: // mastercrafter_cannonballs.go
		return b.cannonballRound(ctx, d, started, dec)
	case mastercraftplan.ModeCannonRun: // mastercrafter_cannonrun.go
		return b.cannonRunRound(ctx, d, started, dec)
	case mastercraftplan.ModeArrows:
		return b.arrowRound(ctx, d, started)
	case mastercraftplan.ModeHerbs:
		return b.herbRound(ctx, d, started)
	default:
		return b.deliverRound(ctx, d)
	}
}

// arrive is the walk a round begins with, to the mule's post: the time budget of ScarabSwarm's own
// trip to the mule (scarabConsolidateWalk, 8 minutes; scarabrunner.OpenBank's own walk gives 4) on
// scarabrunner.WalkTo's wolf-free route (White Wolf Mountain's dens and the Ardougne bears are no-go
// rectangles). A walk that does not arrive ends the round as a passing failure, with the bank
// never touched.
func (b *masterCrafterBot) arrive(ctx *Context, d *fleetrunner.Deps, post tile.Tile) (why string, ok bool) {
	if masterWalkTo(d, post, 3, scarabConsolidateWalk) {
		return "", true
	}
	return fmt.Sprintf("could not reach %s's post at %d,%d from %s within %s", b.mule, post.X, post.Z, fleetrunner.HereIs(d), scarabConsolidateWalk), false
}

// muleHere is the mule's post when its heartbeat is on the bus and on this account's world.
func (b *masterCrafterBot) muleHere(ctx *Context) (post tile.Tile, why string, ok bool) {
	p := scarabMulePresenceOf(ctx, b.mule)
	if !p.Heard || !p.SameWorld {
		return tile.Tile{}, p.Why, false
	}
	return p.Post, "", true
}

// masterAsk is what a kit ask came to.
type masterAsk struct {
	// Got is what arrived (items and notes), Declined the mule's reason per line.
	Got      map[string]int
	Declined map[string]string
	Traded   int
	// Why is the reason nothing arrived.
	Why     string
	Stopped bool
}

// declinedEmpty says every line the mule declined was declined as "none in the bank": the
// published bank was stale, and asking again will not help until it is stocked.
func (a masterAsk) declinedEmpty() bool {
	if len(a.Declined) == 0 {
		return false
	}
	for _, why := range a.Declined {
		if !bowstringplan.DeclineMeansEmpty(why) {
			return false
		}
	}
	return true
}

// askMule asks the mule for the lines in one bundled kit, standing at its post. The notes it
// answers a big line with stay notes in the pack for the next bank visit to bank (KeepNotes).
func (b *masterCrafterBot) askMule(ctx *Context, d *fleetrunner.Deps, lines []mastercraftplan.Line) masterAsk {
	return b.askMuleWith(ctx, d, lines, masterAskOpts{})
}

// masterAskOpts are the ways a round's kit ask differs from askMule's.
type masterAskOpts struct {
	// noted asks for every line as notes (orderplan.KitLine.Noted); banked says the round has
	// just banked the pack, so the kit's own deposit visit before the ask is skipped.
	noted, banked bool
	// keep is what stays in the pack through the kit's deposit (fleetrunner.KitBundleOptions.Keep).
	keep []string
}

// askMuleWith is askMule with opts.
func (b *masterCrafterBot) askMuleWith(ctx *Context, d *fleetrunner.Deps, lines []mastercraftplan.Line, opts masterAskOpts) masterAsk {
	res := masterAsk{Got: map[string]int{}, Declined: map[string]string{}}
	if ctx.Bus == nil || ctx.Username == "" {
		res.Why = "no wall bus to ask the mule on"
		return res
	}
	p := scarabMulePresenceOf(ctx, b.mule)
	if !p.Heard || !p.SameWorld {
		res.Why = p.Why
		return res
	}
	if why, long := scarabMuleQueueTooLong(ctx, b.mule, donorrunner.DefaultKitTimeout); long {
		res.Why = why
		return res
	}
	if !masterWalkTo(d, p.Post, 3, 5*time.Minute) {
		res.Why = fmt.Sprintf("could not reach %s's post at %d,%d", b.mule, p.Post.X, p.Post.Z)
		return res
	}
	items := make([]fleetplan.ItemCount, 0, len(lines))
	for _, l := range lines {
		items = append(items, fleetplan.ItemCount{Item: l.Item, Count: l.Qty})
	}
	b.logf(ctx, "asking %s for %s", b.mule, fleetrunner.DescribeKit(items))
	bank := p.Post
	if opts.banked {
		bank = tile.Tile{} // the pack was banked a moment ago: no second visit before the ask
	}
	kit, info := scarabKitBundle(d, ctx.Bus, ctx.Username, b.mule, items, fleetrunner.KitBundleOptions{
		Timeout: donorrunner.DefaultKitTimeout, Bank: bank, Keep: opts.keep, KeepNotes: true, Noted: opts.noted, PerOrder: bowstringplan.KitMax,
		FallbackTimeout: 90 * time.Second, World: ctx.CurrentWorld,
	})
	res.Stopped = info.Stopped
	for k, n := range kit.Got {
		res.Got[k] += n
	}
	for k, n := range info.NotedHeld {
		res.Got[k] += n
	}
	for k, why := range kit.Declined {
		res.Declined[k] = why
	}
	res.Traded = mastercraftrunner.Total(res.Got)
	if res.Traded == 0 {
		res.Why = "nothing arrived"
		if len(res.Declined) > 0 {
			var parts []string
			for k, why := range res.Declined {
				parts = append(parts, k+": "+why)
			}
			res.Why += " (" + strings.Join(parts, "; ") + ")"
		} else if info.Reason != "" {
			res.Why += " (" + info.Reason + ")"
		}
	}
	return res
}

// eligibleUncut is how many uncut gems of the kinds in gems a bank reading holds.
func eligibleUncut(banked map[string]int, gems []gemplan.Gem) int {
	n := 0
	for _, g := range gems {
		n += banked[g.Key]
	}
	return n
}

// gemRound is the gem round: see the file comment.
func (b *masterCrafterBot) gemRound(ctx *Context, d *fleetrunner.Deps, started time.Time) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeGems}
	post, why, ok := b.muleHere(ctx)
	if !ok {
		out.Transient, out.Why = true, why
		return out
	}
	if why, ok := b.arrive(ctx, d, post); !ok {
		out.Transient, out.Why = true, why
		return out
	}
	gd := mastercraftrunner.GemDeps(d)
	gemsNow := func() []gemplan.Gem {
		return mastercraftplan.GemsBestFirst(masterCraftingLevel(ctx), b.set.GemKinds, b.denied, b.set.MembersWorld)
	}

	// 1. Bank the pack, read the bank, ask the mule for what it holds. This first visit is a full
	// one, and teaches the quick bank the booth and the tile for every visit after it.
	var qb mastercraftrunner.QuickBank
	v := mastercraftrunner.GemBankVisitLearning(d, post, nil, &qb)
	if !v.Opened || v.Err != "" {
		out.Transient, out.Why = true, "the bank at the mule's post: "+v.Err
		return out
	}
	level := masterCraftingLevel(ctx)
	_, haveBanked := mastercraftplan.PickGem(gemsNow(), mastercraftrunner.UncutCounts(v.Banked))
	var stock mastercraftplan.Stock
	if f := muleBankCount(ctx, b.mule); f != nil {
		stock = f
	}
	ask := mastercraftplan.PlanGemAsk(level, b.set, b.denied, stock, v.ChiselHeld)
	switch {
	case len(ask.Lines) > 0:
		a := b.askMule(ctx, d, ask.Lines)
		b.logf(ctx, "gems: asked %s for %d uncut gem(s) at Crafting %d: %s", b.mule, ask.Gems, level, masterDescribe(a.Got))
		if a.Stopped {
			out.Why = "stopped"
			return out
		}
		if a.Traded == 0 && !haveBanked {
			out.Transient = !a.declinedEmpty()
			out.Why = a.Why
			return out
		}
	case !haveBanked:
		out.Why = ask.Why
		return out
	}

	// 2. Cut: draw the best kind, cut the pack five a tick, bank the cut gems, again. Every visit
	// after the first is a quick one (mastercraftrunner.QuickBank: the deposit, Withdraw-All and the
	// close in one tick, the booth op for the next visit right behind the last burst), falling back
	// to a full visit whenever a quick one cannot be made or falls short.
	var cut, crushed, fails, stalls int
	var pace masterPace
	reason := "every uncut gem in the bank is cut"
	// The six-tick runs: on while craftFastGems is and they keep in step; fastGem is the gem a full
	// load of was just cut with the booth op primed behind it.
	fastOn, fastDesyncs := b.set.FastGems, 0
	var fastGem gemplan.Gem
	fastHold := func() bool {
		st := ctx.Session.State()
		return !ctx.Stopped() && !mastercraftplan.RoundExpired(started, time.Now(), b.set.MaxRound) &&
			!st.IsDead() && !randomevents.Busy(ctx.Session)
	}
cutting:
	for {
		if ctx.Stopped() {
			reason = "stopped"
			break
		}
		if mastercraftplan.RoundExpired(started, time.Now(), b.set.MaxRound) {
			reason = "the round's time (craftMaxRoundMinutes) is used up"
			break
		}
		masterClearDead(ctx)
		if !fleetrunner.WaitForGuardian(d) {
			reason = "stopped"
			break
		}
		gems := gemsNow()
		pick := func(uncut map[string]int) (gemplan.Gem, bool) { return mastercraftplan.PickGem(gems, uncut) }
		masterDismissChat(ctx) // a level-up left open stops the uses and the bank alike (mastercrafter_chat.go)
		visitAt := time.Now()
		var v mastercraftrunner.GemVisit
		quick, visited := false, false
		// The six-tick run (mastercraftrunner.FastGems), from a primed quick bank after a full load
		// of fastGem: the stream is sent ahead, and the 7-tick path takes over whenever it falls
		// out of step.
		if fastOn && fastGem.Key != "" && qb.Primed() && qb.Ready(d) {
			fr := mastercraftrunner.FastGems(d, &qb, fastGem, pick, mastercraftrunner.FastOptions{MaxLoads: masterFastMaxLoads, Hold: fastHold})
			cut += fr.Cuts
			if fr.Cuts > 0 {
				pace.addFast(fr.Loads, fr.Took, fr.Ticks, fr.XP)
			}
			if fr.Reason != "not-fast" {
				b.logf(ctx, "gems: %s; %s", fr.Describe(), pace.say())
			}
			ran := fastGem
			fastGem = gemplan.Gem{}
			lootGems(ctx, ran, fr.Cuts, 0) // a fast run is only ever a gem that cannot be smashed
			switch fr.Reason {
			case "done":
				fastDesyncs = 0
				continue // the pack holds a full load cut: the next visit is the 7-tick one
			case "not-fast":
				if f := fr.Fallback; f != nil && f.Opened && f.Err == "" {
					v, quick, visited = *f, true, true
				}
			case "desync":
				pace.desyncs++
				if fastDesyncs++; fastDesyncs >= masterFastMaxDesyncs {
					fastOn = false
					b.logf(ctx, "gems: %d six-tick runs in a row fell out of step; the 7-tick loads carry on for the rest of the round", fastDesyncs)
				}
				continue // the next visit resyncs from whatever the pack holds
			case "refused", "members":
				b.denied[ran.Key] = true
				b.logf(ctx, "gems: cutting %s was refused (%s) — leaving it for the rest of the run", ran.Name, fr.Reason)
				continue
			case "no-open":
				pace.fallbacks++
				b.logf(ctx, "gems: the bank did not open for the six-tick run (%s); the 7-tick visit tries again", fr.Why)
			default: // held, stopped: the top of the loop sees to it
				continue
			}
		}
		if !visited && qb.Ready(d) && gemrunner.HasChisel(gd) {
			var qv mastercraftrunner.QuickVisit
			v, qv = mastercraftrunner.GemQuickVisit(d, &qb, pick)
			quick = qv.Done
			if !qv.Done {
				b.logf(ctx, "gems: quick bank visit fell short (%s); making a full one", qv.Err)
				pace.fallbacks++
			}
		}
		if !quick {
			v = mastercraftrunner.GemBankVisitLearning(d, post, pick, &qb)
		}
		bankTook := time.Since(visitAt)
		switch {
		case v.NoChisel:
			reason = "no chisel in the pack or the bank"
			break cutting
		case !v.Opened || (v.Err != "" && v.Drawn == 0):
			fails++
			b.logf(ctx, "gems: bank visit failed: %s (%d in a row)", v.Err, fails)
			if fails >= 3 {
				out.Transient = true
				reason = "the bank would not open or draw: " + v.Err
				break cutting
			}
			ctx.Delay(3 * time.Second)
			continue
		}
		fails = 0
		if v.Drawn == 0 {
			break // nothing of a kind the level can cut is left
		}
		left := eligibleUncut(mastercraftrunner.UncutCounts(v.Banked), gems) - v.Drawn
		cutAt := time.Now()
		// The next visit's booth op goes out right behind the last burst of this load.
		r := gemrunner.CutAllThen(gd, v.Gem, func() {
			if qb.Ready(d) {
				qb.Prime(d)
			}
		})
		cut, crushed = cut+r.Cuts, crushed+r.Crushed
		lootGems(ctx, v.Gem, r.Cuts, r.Crushed)
		pace.add(quick, bankTook, time.Since(cutAt), r.Ticks, r.XPGained)
		if r.Reason == "done" && r.Crushed == 0 && r.Cuts == mastercraftplan.FastLoadUses {
			fastGem = v.Gem // a full load cut, the booth op primed: the next can be a six-tick run
		}
		if r.Reason != "done" || pace.due() {
			b.logf(ctx, "gems: %s; about %d uncut left in the bank; %s", r.Describe(), max(0, left), pace.say())
		}
		switch r.Reason {
		case "refused":
			b.denied[v.Gem.Key] = true
			b.logf(ctx, "gems: Crafting %d is needed for %s — leaving it for the rest of the run", r.RefusedLevel, v.Gem.Name)
		case "members":
			b.denied[v.Gem.Key] = true
			b.logf(ctx, "gems: %s is members-only and this world refuses it — leaving it for the rest of the run", v.Gem.Name)
		case "no-chisel":
			reason = "the chisel is gone from the pack"
			break cutting
		case "stalled":
			if stalls++; stalls >= 3 {
				reason = "cutting " + v.Gem.Name + " changed nothing three times in a row"
				break cutting
			}
		case "stopped":
			reason = "stopped"
			break cutting
		default:
			stalls = 0
		}
	}
	if pace.loads > 0 {
		b.logf(ctx, "gems: round's cutting over: %s", pace.say())
	}
	out.Produced = cut + crushed
	out.Why = fmt.Sprintf("%d gems cut, %d smashed (%s)", cut, crushed, reason)

	// 3. Hand every cut and crushed gem to the mule.
	products := mastercraftrunner.GemProducts()
	ho := b.handOver(ctx, d, post, products)
	handed := mastercraftrunner.Total(ho.Handed)
	out.Handed = handed
	b.ledger.GemsHanded += handed
	out.HandoverTried, out.HandoverFailed = true, ho.failed()
	// Only ever added to here: this hand-over carried gems only, so it says nothing about bows a
	// failed bow hand-over left behind. The deliver round, which carries both kinds, clears it.
	b.ledger.PendingDeliver = b.ledger.PendingDeliver || out.HandoverFailed
	if ho.Opened {
		b.ledger.PendingCut = eligibleUncut(mastercraftrunner.UncutCounts(ho.Banked), gemsNow()) > 0
	}
	if ho.Why != "" {
		out.Why += "; hand-over: " + ho.Why
	}
	return out
}

// bowRound is the bow round: BowStringer, embedded, then the hand-over of every strung bow.
func (b *masterCrafterBot) bowRound(ctx *Context, d *fleetrunner.Deps, started time.Time) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeBows}
	post, why, ok := b.muleHere(ctx)
	if !ok {
		out.Transient, out.Why = true, why
		return out
	}
	if why, ok := b.arrive(ctx, d, post); !ok {
		out.Transient, out.Why = true, why
		return out
	}
	endReason := ""
	bs := &bowStringerBot{embed: &bowStringerEmbed{
		set: bowstringplan.Settings{
			Mule: b.mule, Bows: b.set.BowNames, KitSize: b.set.KitSize,
			MaxFruitlessKits:    bowstringplan.DefaultMaxFruitlessKits,
			OrderBackoffMinutes: bowstringplan.DefaultOrderBackoffMinutes, LogDetail: "Normal",
		},
		// The unstrung bows the mule holds, from its per-obj rows (the plain name counts the strung
		// bows too, and the strung bows this account handed it).
		stock: func(base func(item string) (int, bool)) func(item string) (int, bool) {
			return mastercraftplan.AdjustedStock(base, &b.ledger, bowIDsOf(ctx))
		},
		ended: func(reason string) { endReason = reason },
	}}
	bs.embed.fast = b.fastBowHook(post, started, bs)
	if err := bs.OnStart(ctx); err != nil {
		out.Why = err.Error()
		return out
	}
	for !bs.embedEnded() && !ctx.Stopped() {
		if mastercraftplan.RoundExpired(started, time.Now(), b.set.MaxRound) {
			endReason = "the round's time (craftMaxRoundMinutes) is used up"
			break
		}
		if err := bs.Loop(ctx); err != nil {
			endReason = err.Error()
			break
		}
	}
	bs.OnStop(ctx)
	strung := 0
	for _, n := range bs.strung {
		strung += n
	}
	out.Produced = strung
	out.Transient = strung == 0 && strings.HasPrefix(endReason, "deferred:")
	out.Why = fmt.Sprintf("%d bows strung (%s)", strung, firstNonEmpty(endReason, "stopped"))

	handed, failed, hwhy := b.handOverBows(ctx, d, post, nil)
	out.Handed = handed
	out.HandoverTried, out.HandoverFailed = true, failed
	// Bows only: added to, never cleared, here (see gemRound).
	b.ledger.PendingDeliver = b.ledger.PendingDeliver || failed
	if hwhy != "" {
		out.Why += "; hand-over: " + hwhy
	}
	return out
}

// handOverBows hands the mule the strung bows (counted in the ledger, so the next published count
// is not mistaken for unstrung ones). extra is more products to cross in the same hand-over (a
// deliver round's gems), counted by name into the result.
//
// WHAT THE ROUND DID NOT STRING STAYS. The unstrung bows and the Bow strings used to go back to
// the mule after every round, because the decision read only the mule's bank and 700 bows left
// here would never have been strung. It reads this account's bank too now
// (mastercraftplan.BowsReadyWithOwn) and a kit asks only for what the bank lacks, so handing them
// back was a round trip: live on def92a2e (5 Oct 2026) a round cut short by a restart handed the
// mule 80,425 items and the next kit took 95,260 of them straight back.
func (b *masterCrafterBot) handOverBows(ctx *Context, d *fleetrunner.Deps, post tile.Tile, extra []mastercraftrunner.Product) (handed int, failed bool, why string) {
	products := append(append([]mastercraftrunner.Product(nil), extra...), mastercraftrunner.BowProducts(d, b.set.BowNames)...)
	ho := b.handOver(ctx, d, post, products)
	for name, n := range ho.Handed {
		switch _, isBow := bowstringplan.LevelFor(name); {
		case strings.EqualFold(name, pearlplan.PearlBolt):
			b.ledger.PearlBoltsHanded += n
		case !isBow:
			b.ledger.GemsHanded += n
		}
	}
	b.noteBowsHanded(ho.Handed)
	handed = mastercraftrunner.Total(ho.Handed)
	return handed, ho.failed(), ho.Why
}

// noteBowsHanded records strung bows given to the mule in the ledger (so the next published count
// is not mistaken for unstrung ones) and the lifetime count.
func (b *masterCrafterBot) noteBowsHanded(handed map[string]int) {
	for name, n := range handed {
		if _, isBow := bowstringplan.LevelFor(name); !isBow {
			continue
		}
		b.ledger.NoteStrungDelivered(name, n)
		b.ledger.BowsHanded += n
	}
}

// deliverRound hands over what an earlier round left in the bank.
func (b *masterCrafterBot) deliverRound(ctx *Context, d *fleetrunner.Deps) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeDeliver}
	post, why, ok := b.muleHere(ctx)
	if !ok {
		out.Transient, out.Why = true, why
		return out
	}
	if why, ok := b.arrive(ctx, d, post); !ok {
		out.Transient, out.Why = true, why
		return out
	}
	products := append(mastercraftrunner.GemProducts(), mastercraftrunner.PearlProducts()...)
	// The bars an anvil round has drawn into the bank are its stock, not a product to give back.
	if !b.smithKeepsBars() && !b.cannonKeepsBars() && !b.plateKeepsBars() {
		products = append(products, mastercraftrunner.BarProducts()...)
	}
	products = append(products, mastercraftrunner.CannonProducts()...)
	products = append(products, mastercraftrunner.SmithProducts()...)
	products = append(products, mastercraftrunner.PlateProducts()...)
	products = append(products, mastercraftrunner.StavesProducts()...)
	products = append(products, mastercraftrunner.ArrowProducts()...)
	products = append(products, mastercraftrunner.HerbProducts()...)
	// The trawler's tips and their bases are master's own stock, not products (live 3 Nov 2026 13:22: a rest's hand-over
	// gave the mule back 125,780 arrowtips the trawler round had just asked it for).
	if b.set.Trawler {
		kept := products[:0]
		for _, p := range products {
			if !trawlerTipItem(p.ID) {
				kept = append(kept, p)
			}
		}
		products = kept
	}
	handed, failed, hwhy := b.handOverBows(ctx, d, post, products)
	out.Handed = handed
	out.HandoverTried, out.HandoverFailed = true, failed
	b.ledger.PendingDeliver = failed
	out.Why = firstNonEmpty(hwhy, fmt.Sprintf("handed over %d item(s)", handed))
	return out
}

// masterHandover is what a hand-over came to.
type masterHandover struct {
	// Handed is what crossed to the mule, by product name; Left what the bank still holds of the
	// products afterwards.
	Handed, Left map[string]int
	// Banked is the last reading of the bank; Opened whether there was one.
	Banked []gamestate.SlotItem
	Opened bool
	// Unknown is set when the bank could not be read at the end, so Left is not known.
	Unknown bool
	Why     string
}

// failed says products are still in the bank after the hand-over, or the bank could not be read to
// say they are not.
func (h masterHandover) failed() bool {
	return h.Unknown || mastercraftrunner.Total(h.Left) > 0
}

// handOver draws every product out of the bank as notes and trades them to the mule at its post
// (runerunner.Deliver: it announces itself on the bus and the mule queues it), in up to
// masterHandoverPasses loads, then banks whatever did not cross and reads the bank.
func (b *masterCrafterBot) handOver(ctx *Context, d *fleetrunner.Deps, post tile.Tile, products []mastercraftrunner.Product) masterHandover {
	res := masterHandover{Handed: map[string]int{}, Left: map[string]int{}}
	if len(products) == 0 {
		return res // nothing this hand-over could carry (no bow's ids resolve): nothing is left either
	}
	names := mastercraftrunner.Names(products)
	p := scarabMulePresenceOf(ctx, b.mule)
	switch {
	case ctx.Bus == nil || ctx.Username == "":
		res.Why = "no wall bus to hand over on"
	case !p.Heard || !p.SameWorld:
		res.Why = "the mule is not here: " + p.Why
	default:
		for pass := 0; pass < masterHandoverPasses && !ctx.Stopped(); pass++ {
			masterClearDead(ctx)
			h := mastercraftrunner.DrawProducts(d, post, products)
			if !h.Opened || h.Err != "" && h.Rows == 0 {
				res.Why = "drawing the products: " + h.Err
				break
			}
			if h.Rows == 0 {
				break // none in the bank
			}
			rd := fleetrunner.MuleDeps(d, ctx.Bus, ctx.Username)
			WireDonorWorlds(ctx, rd)
			rd.HomeWorld = 0 // gated on the mule being on this world: no hop
			r := masterDeliver(d, rd, b.mule, post, h.Rows, h.Slots, names)
			result := "traded"
			switch {
			case r.Hopping:
				result = "hopping"
			case r.GaveUp:
				result = "gave-up"
			case !r.Traded:
				result = "not-traded"
			}
			b.logf(ctx, "HANDOVER mule=%s drawn=%s delivered=%s result=%s reason=%q pass=%d",
				b.mule, masterDescribe(h.Got), masterDescribe(r.Delivered), result, r.Reason, pass+1)
			for k, n := range r.Delivered {
				res.Handed[k] += n
			}
			if r.Hopping {
				res.Unknown, res.Why = true, "the session is hopping worlds"
				return res
			}
			if !r.Traded {
				res.Why = fmt.Sprintf("the trade did not happen (%s): %s", result, r.Reason)
				break
			}
		}
	}
	// What did not cross goes back in the bank, and the bank says what is left.
	v := mastercraftrunner.DepositAll(d, post)
	if !v.Opened {
		res.Unknown = true
		res.Why = firstNonEmpty(res.Why, "the bank would not open to bank what was left: "+v.Err)
		return res
	}
	res.Opened, res.Banked = true, v.Banked
	res.Left = mastercraftrunner.CountProducts(v.Banked, products)
	return res
}
