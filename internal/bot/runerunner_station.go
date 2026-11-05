package bot

// RuneRunner's stations: one function per kind, each ending in b.finish (the station is done,
// whatever it bought) or returning without it (a wait, a hop, a trip leg) so the next Loop picks
// it up again.

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/phaselog"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/runerunnerplan"
	"rsb0t-go/internal/scripts/tile"
)

func (b *runeRunnerBot) runStation(ctx *Context, d *fleetrunner.Deps, s runerunnerplan.Station, here *tile.Tile) {
	switch s.Kind {
	case runerunnerplan.KindDeliver:
		ctx.SetPhase(phaselog.Handoff)
		b.deliverStation(ctx, d, s, here)
	case runerunnerplan.KindDraw:
		ctx.SetPhase(phaselog.Banking)
		b.drawStation(ctx, d, s)
	case runerunnerplan.KindPurse:
		ctx.SetPhase(phaselog.Banking)
		b.purseStation(ctx, d, s)
	case runerunnerplan.KindMarket:
		ctx.SetPhase(phaselog.Shopping)
		b.marketStation(ctx, d, s)
	case runerunnerplan.KindBuy:
		ctx.SetPhase(phaselog.Shopping)
		b.buyStation(ctx, d, s)
	case runerunnerplan.KindLundail:
		ctx.SetPhase(phaselog.Shopping)
		b.lundailStation(ctx, d, s)
	case runerunnerplan.KindStaff:
		ctx.SetPhase(phaselog.Shopping)
		b.staffStation(ctx, d, s)
	case runerunnerplan.KindAmmo:
		ctx.SetPhase(phaselog.Shopping)
		b.ammoStation(ctx, d, s)
	case runerunnerplan.KindHop:
		ctx.SetPhase(phaselog.Walking)
		b.hopStation(ctx, s, here)
	default:
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeSkipped, Reason: "unknown station kind " + string(s.Kind)})
	}
}

func skipped(format string, args ...interface{}) runerunnerplan.StationResult {
	return runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeSkipped, Reason: fmt.Sprintf(format, args...), At: time.Now()}
}

// skippedForCoins is skipped when coins were the reason: the round's COINS SHORT line counts it.
func skippedForCoins(format string, args ...interface{}) runerunnerplan.StationResult {
	r := skipped(format, args...)
	r.NoCoins = true
	return r
}

// runeRunnerLundailLead is how long the Lundail kit, the lever and the walk to Gundai take: the
// Gundai withdrawal is planned from the shelf as it will be then.
const runeRunnerLundailLead = 4 * time.Minute

// mulePost is where the mule stands: the muleStand setting, else its heartbeat's post, else the
// Catherby bank. The post is at a bank, which is also where the delivery's runes come out.
func (b *runeRunnerBot) mulePost(ctx *Context) tile.Tile {
	if b.set.HaveStand {
		return b.set.Stand
	}
	if p := scarabMulePresenceOf(ctx, b.set.Mule).Post; p.X > 0 {
		return p
	}
	return runerunnerplan.DefaultMulePost
}

// ---- hops ----

// requestHop saves the state with HopPending set and asks the fleet to hop. A refusal is logged
// and returned; a refusal that means this deployment cannot hop at all turns hops off for the run.
func (b *runeRunnerBot) requestHop(ctx *Context, to int, reason string) error {
	from := ctx.World()
	at := time.Now()
	b.st.HopPending = to
	b.st.NoteHop(at)
	b.save(ctx)
	if err := ctx.RequestWorldHop(to, reason); err != nil {
		b.st.HopPending = 0
		b.st.UnnoteHop(at)
		b.logf(ctx, "hop-refused to=%d err=%v", to, err)
		if errors.Is(err, ErrNoWorlds) || strings.Contains(err.Error(), "only one world") {
			b.hopsOff = err.Error()
		}
		b.save(ctx)
		return err
	}
	b.logf(ctx, "hop from=%d to=%d reason=%q", from, to, reason)
	b.hopAsked, b.hopTo = time.Now(), to
	return nil
}

// hopStation is stations 5 and 9. The opening hop re-checks the other world's worth first; a hop
// that cannot happen skips the rest of the O block (opening) or leaves the way home to the
// delivery's own gate (closing).
func (b *runeRunnerBot) hopStation(ctx *Context, s runerunnerplan.Station, here *tile.Tile) {
	if b.st.Rotation {
		b.rotationHopStation(ctx, s, here)
		return
	}
	now := time.Now()
	openAt, closeAt, ok := runerunnerplan.OBlock(b.st.Ring)
	opening := ok && b.st.Cursor == openAt
	skip := func(why string) {
		if !opening {
			b.finish(ctx, skipped("%s", why))
			return
		}
		b.st.Done[s.Key()] = skipped("%s", why)
		b.logf(ctx, "skip station=%s reason=%q; the other world's block is skipped this round", s.Key(), why)
		b.quietSince = time.Time{}
		b.st.JumpTo(closeAt + 1)
		if b.st.Cursor >= len(b.st.Ring) {
			b.endRound(ctx)
			return
		}
		b.save(ctx)
	}
	switch {
	case ctx.World() == s.World:
		b.finish(ctx, skipped("already on world %d", s.World))
		return
	case b.hopsOff != "":
		skip("hops unavailable: " + b.hopsOff)
		return
	case b.st.HopsThisRound >= b.set.MaxHopsPerRound:
		skip(fmt.Sprintf("hop budget spent (%d this round, maxHopsPerRound %d)", b.st.HopsThisRound, b.set.MaxHopsPerRound))
		return
	}
	if opening {
		m, _ := b.muleWorld(ctx)
		w := runerunnerplan.OWorth(runerunnerplan.RoundInput{M: m, O: s.World, Shops: b.set.ShopList,
			Worth: func(site string, world int) int { return b.worth(ctx, site, world) }})
		if w < b.set.HopMinGain {
			skip(fmt.Sprintf("world %d now expects %d runes, under hopMinGain %d", s.World, w, b.set.HopMinGain))
			return
		}
	}
	if ok, why := runerunnerplan.MayHop(b.hopCheck(ctx, here)); !ok {
		if b.quietSince.IsZero() {
			b.quietSince = now
		}
		if now.Sub(b.quietSince) >= runeRunnerQuietWait {
			skip(fmt.Sprintf("not quiet enough to hop for %s: %s", runeRunnerQuietWait, why))
			return
		}
		b.sayEvery(ctx, "quiet", time.Minute, "waiting to hop to world %d: %s", s.World, why)
		ctx.Delay(3 * time.Second)
		return
	}
	// The station is done the moment the hop is asked for: the copy of this script that starts
	// on the other world resumes at the next station.
	b.st.Done[s.Key()] = runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeHopped, At: now}
	b.st.HopsThisRound++
	b.st.JumpTo(b.st.Cursor + 1)
	b.quietSince = time.Time{}
	reason := fmt.Sprintf("round %d: %s", b.st.Round, map[bool]string{true: "buying on the other world", false: "back to the mule's world"}[opening])
	if err := b.requestHop(ctx, s.World, reason); err != nil {
		b.st.HopsThisRound--
		b.st.JumpTo(b.st.Cursor - 1)
		delete(b.st.Done, s.Key())
		skip(fmt.Sprintf("hop refused: %v", err))
	}
}

// rotationHopStation is a rotation round's hop (runerunnerplan/rotation.go): every shop station
// has one just before it. On its world already, it holds the block's shelves and finishes; else
// it hops only when the block's pairs expect at least hopMinGain runes, the hop budget allows
// (RotationHopGap apart, maxHopsPerHour), and MayHop says the account is quiet. A hop that cannot
// happen skips its block -- the pairs it would have hopped for -- and the circuit goes on to the
// next pair.
func (b *runeRunnerBot) rotationHopStation(ctx *Context, s runerunnerplan.Station, here *tile.Tile) {
	now := time.Now()
	shops := runerunnerplan.BlockShops(b.st.Ring, b.st.Cursor)
	skip := func(why string) {
		end := runerunnerplan.BlockEnd(b.st.Ring, b.st.Cursor)
		b.st.Done[s.Key()] = skipped("%s", why)
		b.logf(ctx, "skip station=%s reason=%q; skipping %s", s.Key(), why, runerunnerplan.PlanString(b.st.Ring[b.st.Cursor+1:end], 0))
		b.quietSince = time.Time{}
		b.releaseRotationHolds(ctx, shops)
		b.st.JumpTo(end)
		if b.st.Cursor >= len(b.st.Ring) {
			b.endRound(ctx)
			return
		}
		b.save(ctx)
	}
	if ctx.World() == s.World {
		b.holdRotationPairs(ctx, shops, now)
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeHere, Reason: fmt.Sprintf("already on world %d", s.World), At: now})
		return
	}
	if b.hopsOff != "" {
		skip("hops unavailable: " + b.hopsOff)
		return
	}
	if len(shops) > 0 {
		worth := 0
		for _, st := range shops {
			worth += b.worth(ctx, st.Site, st.World)
		}
		// The block's ammo stops count too (runerunnerplan/hopworth.go): Aemad's and Port Khazard's shelves were left on two worlds of three
		// because the Lundail runes beside them were worth nothing.
		var stops []runerunnerplan.AmmoHopStop
		for _, st := range runerunnerplan.BlockAmmo(b.st.Ring, b.st.Cursor) {
			site, ok := runerunnerplan.AmmoSiteFor(st.Site)
			if !ok {
				continue
			}
			stops = append(stops, runerunnerplan.AmmoHopStop{Key: st.Key(), Coins: b.ammoStationCost(ctx, st, now), MinVisit: site.MinVisit(b.set.AmmoMinVisitCoins)})
		}
		if ok, why := runerunnerplan.HopWorthy(worth, b.set.HopMinGain, stops); !ok {
			skip(fmt.Sprintf("world %d at %s: %s", s.World, runerunnerplan.PlanString(shops, 0), why))
			return
		} else if worth < b.set.HopMinGain {
			b.logf(ctx, "hop for ammo: world %d: %s", s.World, why)
		}
	} else if stops := runerunnerplan.BlockAmmo(b.st.Ring, b.st.Cursor); len(stops) > 0 && ctx.World() != s.World {
		// A block of ammo stops alone (Hickton on a world the circuit does not end on) is hopped to only for what its shelf is
		// worth in coins: ammo never decides a hop beside a rune shop, but here it is the block's whole reason.
		coins := 0
		for _, st := range stops {
			coins += b.ammoStationCost(ctx, st, now)
		}
		if ok, why := runerunnerplan.AmmoWorthVisiting(coins, b.set.AmmoMinVisitCoins); !ok {
			skip(fmt.Sprintf("world %d: %s at %s", s.World, why, runerunnerplan.PlanString(stops, 0)))
			return
		}
	}
	if wait, why := runerunnerplan.HopBudget(b.st.HopTimes, now, b.set.MaxHopsPerHour); wait > 0 {
		if wait > runerunnerplan.RotationHopWaitMax {
			skip(fmt.Sprintf("hop budget: %s; the next hop is %s away", why, wait.Round(time.Second)))
			return
		}
		b.sayEvery(ctx, "hopbudget", time.Minute, "waiting %s to hop to world %d: %s", wait.Round(time.Second), s.World, why)
		b.heartbeat(ctx, s.Key(), here)
		if wait > 5*time.Second {
			wait = 5 * time.Second
		}
		ctx.Delay(wait)
		return
	}
	if ok, why := runerunnerplan.MayHop(b.hopCheck(ctx, here)); !ok {
		if b.quietSince.IsZero() {
			b.quietSince = now
		}
		if now.Sub(b.quietSince) >= runeRunnerQuietWait {
			skip(fmt.Sprintf("not quiet enough to hop for %s: %s", runeRunnerQuietWait, why))
			return
		}
		b.sayEvery(ctx, "quiet", time.Minute, "waiting to hop to world %d: %s", s.World, why)
		ctx.Delay(3 * time.Second)
		return
	}
	// Hold the shelves first, so a runner deciding a moment later sees them taken; then the
	// station is done the moment the hop is asked for, and the copy of this script that starts on
	// the other world resumes at the next station.
	b.holdRotationPairs(ctx, shops, now)
	b.st.Done[s.Key()] = runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeHopped, At: now}
	b.st.HopsThisRound++
	b.st.JumpTo(b.st.Cursor + 1)
	b.quietSince = time.Time{}
	reason := fmt.Sprintf("round %d: rotation to world %d", b.st.Round, s.World)
	if len(shops) > 0 {
		reason += " for " + runerunnerplan.PlanString(shops, 0)
	}
	if err := b.requestHop(ctx, s.World, reason); err != nil {
		b.st.HopsThisRound--
		b.st.JumpTo(b.st.Cursor - 1)
		delete(b.st.Done, s.Key())
		skip(fmt.Sprintf("hop refused: %v", err))
	}
}

// holdRotationPairs reserves, on each pair's world board, what this runner would buy there under
// its caps (the same hold a shop station places before its walk), so other runners and the
// cohort's planners see the shelf taken. The station replaces the hold and releases it when it
// ends (finish).
func (b *runeRunnerBot) holdRotationPairs(ctx *Context, shops []runerunnerplan.Station, now time.Time) {
	for _, st := range shops {
		if off, _ := b.pairGated(st.Site, st.World, now); off {
			continue
		}
		site, ok := runerunnerplan.Site(st.Site)
		if !ok {
			continue
		}
		est := b.estimate(ctx, site, st.World, now)
		board := fleetrunner.ShopsFor(st.World)
		for _, r := range b.set.RuneList {
			if n, _ := runerunnerplan.BuyableUnderCap(site.Shop, r, est[r], b.set.CapTable.For(r)); n > 0 {
				board.Reserve(site.Shop, r, ctx.Username, n, now, site.HoldTTL, b.set.Tick())
			}
		}
	}
}

// releaseRotationHolds drops this runner's holds at the pairs' shops.
func (b *runeRunnerBot) releaseRotationHolds(ctx *Context, shops []runerunnerplan.Station) {
	for _, st := range shops {
		if site, ok := runerunnerplan.Site(st.Site); ok {
			fleetrunner.ShopsFor(st.World).Release(site.Shop, ctx.Username)
		}
	}
}

// soldBy is the runes in `runes` the site stocks.
func soldBy(site runerunnerplan.ShopSite, runes []string) []string {
	var out []string
	for _, r := range runes {
		if site.Shop.Sells(r) {
			out = append(out, r)
		}
	}
	return out
}

// ---- station 1: the delivery ----

// deliverVerdict is DeliverDecision on the live gate for a delivery of runes alone.
func (b *runeRunnerBot) deliverVerdict(ctx *Context, held int, here *tile.Tile) runerunnerplan.DeliverVerdict {
	return b.deliverVerdictAmmo(ctx, held, 0, here)
}

// deliverVerdictAmmo is DeliverDecision on the live gate: held is the runes (and staffs) in the pack and bank,
// ammoHeld the ammo parts, which ride along but never make a delivery due by size.
func (b *runeRunnerBot) deliverVerdictAmmo(ctx *Context, held, ammoHeld int, here *tile.Tile) runerunnerplan.DeliverVerdict {
	m, known := ctx.MuleWorld(b.set.Mule)
	hopOK, hopWhy := runerunnerplan.MayHop(b.hopCheck(ctx, here))
	if b.hopsOff != "" {
		hopOK, hopWhy = false, b.hopsOff
	}
	if ok, why := b.st.DeliverHopAllowed(); hopOK && !ok {
		hopOK, hopWhy = false, why
	}
	wait := time.Duration(b.set.MuleWaitMinutes) * time.Minute
	if wait < time.Minute {
		wait = time.Minute
	}
	queueWhy, tooLong := scarabMuleQueueTooLong(ctx, b.set.Mule, wait)
	minRunes := b.set.DeliverMinRunes
	if b.st.SwarmDue {
		// The hand-over before ScarabSwarm: everything held goes, however little -- the ammo too.
		minRunes = 1
		held, ammoHeld = held+ammoHeld, 0
	}
	return runerunnerplan.DeliverDecision(runerunnerplan.DeliverInput{
		Held: held, HeldAmmo: ammoHeld, LastDelivery: b.st.LastDelivery, Now: time.Now(),
		MinRunes: minRunes, MaxAge: time.Duration(b.set.DeliverMaxAgeMinutes) * time.Minute,
		MuleWorld: m, MuleWorldKnown: known, MyWorld: ctx.World(),
		MuleHeard: scarabWorldMuleHeard(ctx, b.set.Mule), QueueTooLong: tooLong, QueueWhy: queueWhy,
		HopAllowed: hopOK, HopWhy: hopWhy,
	})
}

// deliverStation is station 1: at the mule's post, read the bank, decide, and trade, wait, hop
// or skip.
func (b *runeRunnerBot) deliverStation(ctx *Context, d *fleetrunner.Deps, s runerunnerplan.Station, here *tile.Tile) {
	post := b.mulePost(ctx)
	if !runerunner.WalkTries(d, s.Key(), post, 4, runerunner.ShopWalkTimeout, 3) {
		b.stuck(ctx, fmt.Sprintf("stuck: could not reach the mule's post %d,%d from %s", post.X, post.Z, fleetrunner.HereIs(d)))
		return
	}
	bank, read, ok := runerunner.OpenBankAt(d, post)
	if !ok {
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeAborted, Reason: "the bank at the mule's post would not open"})
		return
	}
	b.st.NoteBank(read.Runes, read.Coins, time.Now())
	items := b.set.DeliverItems()
	byRune, held, ammoHeld := runerunnerplan.DeliverablesSplit(runerunner.PackCounts(d, items), runerunner.BankCounts(d, bank, items),
		b.set.DeliverRuneItems(), b.set.DeliverAmmoItems())
	v := b.deliverVerdictAmmo(ctx, held, ammoHeld, fleetrunner.Here(d))
	switch v.Action {
	case runerunnerplan.DeliverSkip:
		runerunner.CloseBank(d, bank)
		b.finish(ctx, skipped("%s", v.Reason))
	case runerunnerplan.DeliverHop:
		runerunner.CloseBank(d, bank)
		m, _ := ctx.MuleWorld(b.set.Mule)
		// Not a ring hop: the cursor stays on the delivery, which runs again on the mule's world.
		// One such hop per round (DeliverHopRound), so a hop that does not land cannot repeat.
		b.st.DeliverHopRound = b.st.Round
		if err := b.requestHop(ctx, m, "deliver: "+v.Reason); err != nil {
			b.deferDelivery(ctx, d, fmt.Sprintf("the hop to the mule's world was refused: %v", err), byRune, post)
		}
	case runerunnerplan.DeliverDefer:
		runerunner.CloseBank(d, bank)
		b.deferDelivery(ctx, d, v.Reason, byRune, post)
	case runerunnerplan.DeliverNow:
		b.deliverNow(ctx, d, bank, post, byRune)
	}
}

// deferDelivery waits at the post up to muleWaitMinutes for the gate to clear, re-checking each
// minute; then it banks the pack's runes and moves on. The runes go next round.
func (b *runeRunnerBot) deferDelivery(ctx *Context, d *fleetrunner.Deps, reason string, byRune map[string]int, post tile.Tile) {
	since := "never"
	if !b.st.LastDelivery.IsZero() {
		since = time.Since(b.st.LastDelivery).Round(time.Minute).String()
	}
	until := time.Now().Add(time.Duration(b.set.MuleWaitMinutes) * time.Minute)
	for {
		b.logf(ctx, "deliver-deferred reason=%q held=%s since_last=%s", reason, runerunnerplan.FormatCounts(byRune), since)
		if !time.Now().Before(until) {
			break
		}
		if !ctx.Delay(time.Minute) {
			return
		}
		held, ammoHeld := splitHeld(byRune)
		v := b.deliverVerdictAmmo(ctx, held, ammoHeld, fleetrunner.Here(d))
		switch v.Action {
		case runerunnerplan.DeliverNow:
			bank, _, ok := runerunner.OpenBankAt(d, post)
			if !ok {
				reason = "the bank at the mule's post would not open"
				continue
			}
			b.deliverNow(ctx, d, bank, post, byRune)
			return
		case runerunnerplan.DeliverHop:
			m, _ := ctx.MuleWorld(b.set.Mule)
			b.st.DeliverHopRound = b.st.Round
			if b.requestHop(ctx, m, "deliver: "+v.Reason) == nil {
				return
			}
		}
		reason = v.Reason
	}
	b.bankPack(ctx, d, post)
	b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeDeferred, Reason: reason})
}

// splitHeld totals a delivery's held counts: the runes and staffs, and the ammo parts apart.
func splitHeld(byItem map[string]int) (runes, ammo int) {
	for name, n := range byItem {
		if _, isAmmo := runerunnerplan.AmmoItemFor(name); isAmmo {
			ammo += n
		} else {
			runes += n
		}
	}
	return runes, ammo
}

// bankPack deposits everything but coins at the bank by `at` and records the reading.
func (b *runeRunnerBot) bankPack(ctx *Context, d *fleetrunner.Deps, at tile.Tile) {
	if b.bankPackSeam != nil {
		b.bankPackSeam(at)
		return
	}
	bank, _, ok := runerunner.OpenBankAt(d, at)
	if !ok {
		b.logf(ctx, "could not open the bank at %d,%d to put the runes back; they ride in the pack", at.X, at.Z)
		return
	}
	runerunner.DepositExcept(d, bank, []string{runerunner.CoinsName})
	read := runerunner.ReadBank(d, bank)
	b.st.NoteBank(read.Runes, read.Coins, time.Now())
	runerunner.CloseBank(d, bank)
}

// deliverNow withdraws every listed item (the runes, and Staffs of air when bought) and trades
// them to the mule (donorrunner.Run with the mule rules: those items only, minds first). The ring
// owns every hop but the one Run may make to reach the mule, so the donor's hop home is off.
func (b *runeRunnerBot) deliverNow(ctx *Context, d *fleetrunner.Deps, bank *actions.Bank, post tile.Tile, byRune map[string]int) {
	items := b.set.DeliverItems()
	// The unstackable parts (Rope) go through the bank and come out as notes (deliverNoted): a pack takes 24 single Ropes, so loose
	// ones moved one pack a round whatever the bank held (the mule held 37 on 4 Nov 2026), where a note stack crosses in one slot.
	runerunner.DepositExcept(d, bank, append([]string{runerunner.CoinsName}, withoutFold(items, b.set.DeliverNotedList)...))
	notedGot := runerunner.WithdrawNoted(d, bank, b.set.DeliverNotedList)
	rows, drawn, _ := runerunner.WithdrawItems(d, bank, items)
	if len(notedGot) > 0 {
		b.logf(ctx, "deliver noted=%s", runerunnerplan.FormatCounts(notedGot))
		rows += len(notedGot)
		drawn += len(notedGot)
	}
	read := runerunner.ReadBank(d, bank)
	b.st.NoteBank(read.Runes, read.Coins, time.Now())
	runerunner.CloseBank(d, bank)

	offered := runerunner.PackCounts(d, items)
	// Runes carried in from the shops were in the pack before the withdrawal: the hand-over's row
	// and slot counts cover everything that will cross.
	if slots := runerunner.PackSlots(d, items); drawn < slots {
		drawn = slots
	}
	if rows < len(offered) {
		rows = len(offered)
	}
	if len(offered) == 0 {
		b.finish(ctx, skipped("nothing to deliver after the withdrawal (expected %s)", runerunnerplan.FormatCounts(byRune)))
		return
	}
	rd := fleetrunner.MuleDeps(d, ctx.Bus, ctx.Username)
	WireDonorWorlds(ctx, rd)
	rd.HomeWorld = 0
	m, _ := ctx.MuleWorld(b.set.Mule)
	res := runerunner.Deliver(d, rd, b.set.Mule, post, rows, drawn, items)
	result := "traded"
	switch {
	case res.Hopping:
		result = "hopping"
	case res.GaveUp:
		result = "gave-up"
	case !res.Traded:
		result = "not-traded"
	}
	b.logf(ctx, "deliver mule=%s world=%d offered=%s delivered=%s result=%s reason=%q",
		b.set.Mule, ctx.World(), runerunnerplan.FormatCounts(offered), runerunnerplan.FormatCounts(res.Delivered), result, res.Reason)
	switch {
	case res.Hopping:
		b.st.HopPending = m
		b.st.DeliverHopRound = b.st.Round
		b.st.NoteHop(time.Now())
		b.hopAsked, b.hopTo = time.Now(), m
		b.save(ctx)
	case res.Traded:
		b.st.LastDelivery = time.Now()
		b.st.DeliverRefusals = 0
		b.askTravelKit(ctx, d)
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeDelivered, Delivered: res.Delivered, Reason: res.Reason})
	case res.Stopped:
		b.save(ctx)
	case res.GaveUp:
		b.st.DeliverRefusals++
		if runerunnerplan.AllowListHint(b.st.DeliverRefusals) {
			b.logf(ctx, "ALLOWLIST? %s refused %d trades in a row: is this account on %s's allowedPartners?",
				b.set.Mule, b.st.DeliverRefusals, b.set.Mule)
		}
		b.bankPack(ctx, d, post)
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeDeferred, Reason: "the mule gave up: " + res.Reason})
	default:
		b.bankPack(ctx, d, post)
		b.finish(ctx, skipped("not traded: %s", res.Reason))
	}
}

// ---- station 2: the draw ----

// drawStation is station 2: at the Catherby bank, everything but coins goes in and the east leg's
// coins come out (DrawAmount over the pack-paid stations still ahead).
func (b *runeRunnerBot) drawStation(ctx *Context, d *fleetrunner.Deps, s runerunnerplan.Station) {
	post := b.mulePost(ctx)
	if !runerunner.WalkTries(d, s.Key(), post, 4, runerunner.ShopWalkTimeout, 3) {
		b.stuck(ctx, fmt.Sprintf("stuck: could not reach the bank at %d,%d from %s", post.X, post.Z, fleetrunner.HereIs(d)))
		return
	}
	bank, _, ok := runerunner.OpenBankAt(d, post)
	if !ok {
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeAborted, Reason: "the bank would not open"})
		return
	}
	runerunner.DepositExcept(d, bank, []string{runerunner.CoinsName})
	read := runerunner.ReadBank(d, bank)
	b.st.NoteBank(read.Runes, read.Coins, time.Now())
	// The purse is sized for the WHOLE round (every shop on every world still ahead, valued when the
	// round reaches it, plus a margin): the mule is asked for it, and the pack draws the pack-paid part.
	rn := b.roundNeed(ctx, b.st.Cursor+1)
	need := rn.PackNeed()
	if bank, read, ok = b.mulePurse(ctx, d, bank, read, post, rn); !ok {
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeAborted, Reason: "the bank would not reopen after the mule's top-up"})
		return
	}
	inPack := fleetrunner.CoinsHeld(d)
	draw, keep := b.bankDraw(rn, true, read.Coins, inPack, need)
	if draw.NoCoins {
		runerunner.CloseBank(d, bank)
		b.noCoins = true
		b.logf(ctx, "NO COINS bank=%d reserve=%d need=%d", read.Coins, b.set.CoinReserve, draw.Need)
		b.finish(ctx, skippedForCoins("no coins: bank %d, reserve %d", read.Coins, b.set.CoinReserve))
		return
	}
	got, err := runerunner.WithdrawCoins(d, bank, draw.Amount)
	if err != nil {
		b.logf(ctx, "drawing %d coins: %v", draw.Amount, err)
	}
	runerunner.CloseBank(d, bank)
	b.st.BankCoins = read.Coins - got
	b.noCoins = false
	b.logf(ctx, "coins drew=%d at=%d,%d bank_left=%d cap=%d reserve=%d lundail_keep=%d est_cost=%d in_pack=%d why=%q", got, post.X, post.Z, read.Coins-got, b.set.TripCoins, b.set.CoinReserve, keep, need, inPack+got, draw.Why)
	b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeDrew, Reason: fmt.Sprintf("drew %d of %d", got, draw.Amount)})
}

// purseAsk is the top-up's thresholds for the round from ring index `from` on: runerunnerplan.CircuitTopUp over the
// round's FULL bill (the rune, staff and Lundail need and the ammo stations' estimate beside it, RoundNeed.Bill)
// plus the bank's reserve and the fare, so a runner that holds the bill has its ammo funded too and one that
// holds less asks for the mule's stack.
func (b *runeRunnerBot) purseAsk(rn runerunnerplan.RoundNeed, held int) (below, amount int) {
	return runerunnerplan.CircuitTopUp(b.set.BuyAll, b.set.TopUpBelow, b.set.TopUpAmount, rn.Est()+rn.Ammo, b.set.CoinReserve, b.set.FareCoins, held)
}

// mulePurse is the draw station's coin top-up from the mule (runerunnerplan.MuleTopUp): with the
// bank open at the mule's post and bank plus pack under the round's full bill, it closes the bank, asks the
// mule for its WHOLE published coin stack above muleCoinFloor (topUpWholeStack; topUpAmount when the stack is not
// published) over the wall bus, in as many orders as its coinsPerOrder needs (runerunner.AskMuleForCoins), then
// reopens the bank and banks every coin so the draw below plans from one number. It returns the open bank and its
// reading; ok is false only when the bank would not reopen.
func (b *runeRunnerBot) mulePurse(ctx *Context, d *fleetrunner.Deps, bank *actions.Bank, read runerunner.BankRead, post tile.Tile, rn runerunnerplan.RoundNeed) (*actions.Bank, runerunner.BankRead, bool) {
	now := time.Now()
	v, held, below := b.purseVerdict(ctx, d, read.Coins, rn)
	if v.Ask <= 0 {
		if held < below {
			b.logf(ctx, "top-up not asked: %s", v.Reason)
		}
		b.purseShort(ctx, rn, held, 0)
		b.purseFunded(ctx, rn, held, 0, 0)
		return bank, read, true
	}
	runerunner.CloseBank(d, bank)
	b.st.LastTopUpAsk = now
	b.save(ctx)
	b.logf(ctx, "top-up asking mule=%s coins=%d (%s)", b.set.Mule, v.Ask, v.Reason)
	rd := fleetrunner.MuleDeps(d, ctx.Bus, ctx.Username)
	res := runerunner.AskMuleForCoins(d, rd, b.set.Mule, ctx.Username, v.Ask)
	b.st.NoteTopUp(res.Got)
	b.logf(ctx, "top-up mule=%s asked=%d got=%d orders=%d declined=%v reason=%q round_received=%d total_received=%d",
		b.set.Mule, v.Ask, res.Got, res.Orders, res.Declined, res.Reason, b.st.RoundTopUp, b.st.TopUpReceived)
	b.noteTopUpDecline(ctx, v.Ask, res)
	b.save(ctx)
	bank, _, ok := runerunner.OpenBankAt(d, post)
	if !ok {
		return nil, runerunner.BankRead{}, false
	}
	// Every coin into the bank, so the draw plans from the bank alone.
	runerunner.DepositExcept(d, bank, nil)
	read = runerunner.ReadBank(d, bank)
	b.st.NoteBank(read.Runes, read.Coins, time.Now())
	heldNow := read.Coins + fleetrunner.CoinsHeld(d)
	b.purseShort(ctx, rn, heldNow, res.Got)
	b.purseFunded(ctx, rn, heldNow, v.Ask, res.Got)
	return bank, read, true
}

// purseVerdict is the draw station's top-up decision (runerunnerplan.MuleTopUp) for a bank that reads bankCoins, and
// the round's `purse ...` line: what the round needs, what is held, what the mule's published bank holds and what it
// is asked for. target is the full bill (runes, staffs, Lundail, margin, fares AND ammo) and the bank's reserve.
// It returns the coins held (bank and pack) and the threshold the ask was made against.
func (b *runeRunnerBot) purseVerdict(ctx *Context, d *fleetrunner.Deps, bankCoins int, rn runerunnerplan.RoundNeed) (v runerunnerplan.TopUpVerdict, held, below int) {
	m, known := ctx.MuleWorld(b.set.Mule)
	pack := fleetrunner.CoinsHeld(d)
	held = bankCoins + pack
	below, amount := b.purseAsk(rn, held)
	muleCoins, muleCoinsKnown := muleHeld(ctx, b.set.Mule, runerunner.CoinsName)
	v = runerunnerplan.MuleTopUp(runerunnerplan.TopUpInput{
		Below: below, Amount: amount,
		BankCoins: bankCoins, PackCoins: pack,
		MuleWorld: m, MuleWorldKnown: known, MyWorld: ctx.World(),
		MuleHeard: scarabWorldMuleHeard(ctx, b.set.Mule), LastAsk: b.st.LastTopUpAsk, Now: time.Now(),
		MuleCoins: muleCoins, MuleCoinsKnown: muleCoinsKnown,
		MuleFloor: b.set.MuleCoinFloor, WholeStack: b.set.TopUpWholeStack,
	})
	b.logf(ctx, "purse %s reserve=%d target=%d bill=%d bank=%d in_pack=%d held=%d below=%d mule_bank=%s mule_floor=%d whole_stack=%v ask=%d (%s)", rn, b.set.CoinReserve,
		rn.Bill()+b.set.CoinReserve, rn.Bill(), bankCoins, pack, held, below, muleBankLabel(muleCoins, muleCoinsKnown),
		b.set.MuleCoinFloor, b.set.TopUpWholeStack, v.Ask, v.Reason)
	return v, held, below
}

// purseFunded says so when bank plus pack holds the round's whole bill and the reserve: what was held, asked
// and received. Quiet while the purse is short (purseShort says that) and for a top-up that is off.
func (b *runeRunnerBot) purseFunded(ctx *Context, rn runerunnerplan.RoundNeed, held, asked, got int) {
	if b.set.TopUpBelow <= 0 || b.set.TopUpAmount <= 0 || held < rn.Bill()+b.set.CoinReserve {
		return
	}
	b.logf(ctx, "%s", runerunnerplan.PurseFunded(held, rn.Bill(), b.set.CoinReserve, asked, got))
}

// muleBankLabel is the mule's published coin count for a log line, "unknown" when it published none lately.
func muleBankLabel(coins int, known bool) string {
	if !known {
		return "unknown"
	}
	return itoa(coins)
}

// noteTopUpDecline says why a coin ask that came back short was: a mule that declined with nothing
// (the allow list or a zero limit) or one that stopped at a hand-out limit with coins still in its bank.
// A first full-shelf ask is twelve orders of 500,000, and eight orders a window is all one donor is handed
// at the mule's coinsPerOrder 500,000 / handoutPerDonor 8; the rest comes at the next round's ask.
func (b *runeRunnerBot) noteTopUpDecline(ctx *Context, asked int, res runerunner.TopUpResult) {
	switch {
	case res.Got <= 0 && res.Declined && !runerunnerplan.LimitDecline(res.Reason):
		b.logf(ctx, "ALLOWLIST? %s declined a coin top-up (%s): is this account on its allowedPartners, and are its coinsPerOrder/coinsPerWindow above 0?", b.set.Mule, res.Reason)
	case res.Declined && res.Got < asked && runerunnerplan.LimitDecline(res.Reason):
		b.logf(ctx, "MULE LIMIT? %s stopped a coin ask at %d of %d after %d orders (%s): raise its coinsPerOrder (500,000 is twelve orders for 6M) and handoutPerDonor (8 orders a window); the rest is asked next round",
			b.set.Mule, res.Got, asked, res.Orders, res.Reason)
	}
}

// purseShort says so when the bank and the pack hold less than the round's target (the full bill and the reserve)
// after the mule was (or could not be) asked: the fleet's coins are what is short, not the plan. Only a buyAll
// runner has a target.
func (b *runeRunnerBot) purseShort(ctx *Context, rn runerunnerplan.RoundNeed, held, got int) {
	target := rn.Bill() + b.set.CoinReserve
	if !b.set.BuyAll || held >= target {
		return
	}
	b.logf(ctx, "purse SHORT by %d: %d coins held against a target of %d (the mule just gave %d); the shelves will be bought in rune priority order with what is there",
		target-held, held, target, got)
}

// affordable is the pre-walk coin check (runerunnerplan.Affordable): what the pack carries beyond
// the fare, plus what a bank on the way could still spare, must buy minVisitRunes here.
func (b *runeRunnerBot) affordable(d *fleetrunner.Deps, site runerunnerplan.ShopSite, est map[string]int, packCoins, fare int) (bool, string) {
	coins := runerunnerplan.SpendableFor(packCoins, fare, !b.st.BankReadAt.IsZero(), b.st.BankCoins, b.set.CoinReserve, b.set.TripCoins, true)
	ok, _, why := runerunnerplan.Affordable(runerunnerplan.AffordInput{
		Shop: site.Shop, Stock: est, Runes: b.set.RuneList, Caps: b.set.CapTable, Coins: coins, MinVisitRunes: b.set.MinVisitRunes,
	})
	return ok, why
}

// ---- stations 3, 4, 6, 7: Betty and Aubury ----

// buyStation walks to Betty or Aubury and buys every listed rune under its cap, priority first,
// keeping the fare and the later stations' higher-priority runes funded.
func (b *runeRunnerBot) buyStation(ctx *Context, d *fleetrunner.Deps, s runerunnerplan.Station) {
	now := time.Now()
	key := s.Key()
	site, ok := runerunnerplan.Site(s.Site)
	switch {
	case !ok:
		b.finish(ctx, skipped("unknown shop %q", s.Site))
		return
	case ctx.World() != s.World:
		b.finish(ctx, skipped("on world %d; the station is for world %d", ctx.World(), s.World))
		return
	case b.noCoins:
		b.finish(ctx, skippedForCoins("no coins"))
		return
	}
	if off, why := b.st.PairOff(key, now); off {
		b.finish(ctx, skipped("%s", why))
		return
	}
	// The Wizards' Guild door reads the CURRENT Magic level: checked again here, at the station,
	// because the round was planned earlier (a drained level, or a round resumed from a save).
	b.noteMagic(ctx)
	if !runerunnerplan.Eligible(site, b.magic) {
		b.finish(ctx, skipped("%s needs Magic %d; this account has %d", site.Key, site.MinMagic, b.magic))
		return
	}
	est := b.estimate(ctx, site, s.World, now)
	worth := runerunnerplan.Worth(site.Shop, est, b.set.RuneList, b.set.CapTable)
	if ok, why := runerunnerplan.WorthVisiting(worth, b.set.MinVisitRunes); !ok {
		b.finish(ctx, skipped("%s (est=%d)", why, worth))
		return
	}
	if ok, why := b.affordable(d, site, est, fleetrunner.CoinsHeld(d), b.set.FareCoins); !ok {
		b.finish(ctx, skippedForCoins("cannot afford: %s", why))
		return
	}

	// Coins: a pack short of this station and the rest of the pack-paid round stops at the nearest
	// bank first (a respawn, a restart, a first start east, an estimate that ran low). The need is the
	// round's from this station on, each shop valued when the round reaches it, plus the margin.
	ahead := b.aheadEstimates(ctx, b.st.Cursor+1)
	rn := b.roundNeed(ctx, b.st.Cursor)
	// A pack holds at most one draw (tripCoins): a round dearer than that is drawn leg by leg, and a
	// pack already at its draw has nothing more to fetch.
	need := min(rn.PackNeed(), b.set.TripCoins)
	bankRead := !b.st.BankReadAt.IsZero()
	// Due when the pack is a quarter short of the rest of the round, or when it cannot pay THIS station
	// its expected cost and the margin on it and the bank can add to it (a shelf is never left for coins the
	// bank holds).
	if inPack := fleetrunner.CoinsHeld(d); runerunnerplan.TopUpDue(inPack, runerunnerplan.FundableNeed(inPack, need, bankRead, b.st.BankCoins, b.set.CoinReserve), b.set.FareCoins) ||
		runerunnerplan.StationShort(inPack, rn.Next+rn.NextMargin, b.set.FareCoins, bankRead, b.st.BankCoins, b.set.CoinReserve) {
		if !b.topUp(ctx, d, site, rn, inPack) {
			return
		}
	}
	if fleetrunner.CoinsHeld(d) <= b.set.FareCoins {
		b.finish(ctx, skippedForCoins("no coins in the pack beyond the %d-coin fare", b.set.FareCoins))
		return
	}
	// Room: runes stack, so a visit needs one free slot per listed rune not already stacked.
	if runerunner.FreeSlots(d) < runerunner.SlotsWanted(d, soldBy(site, b.set.RuneList)) {
		b.logf(ctx, "pack-full station=%s free=%d: banking at %s", key, runerunner.FreeSlots(d), site.BankName)
		if !runerunner.WalkTries(d, site.Key+" bank", site.Bank, 4, runerunner.ShopWalkTimeout, 2) || !runerunner.MakeRoom(d, site.Bank, b.set.RuneList) {
			b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeAborted, Reason: fmt.Sprintf("the pack is full and the %s bank could not be reached", site.BankName)})
			return
		}
	}

	// Hold what this visit plans on the shared board, so the cohort's own planners see it.
	board := fleetrunner.ShopsFor(s.World)
	for _, r := range b.set.RuneList {
		if n, _ := runerunnerplan.BuyableUnderCap(site.Shop, r, est[r], b.set.CapTable.For(r)); n > 0 {
			board.Reserve(site.Shop, r, ctx.Username, n, now, site.HoldTTL, b.set.Tick())
		}
	}
	reserve := b.visitReserve(ctx, ahead)
	var spendable int
	planFor := func(seen map[string]int, coins int) fleetplan.PurchasePlan {
		spendable = coins - b.set.FareCoins
		return runerunnerplan.PlanVisit(site.Shop, seen, spendable, b.set.RuneList, b.set.CapTable, reserve)
	}
	v := runerunner.BuyAt(d, site, planFor)
	at := time.Now()
	if v.Walked {
		fleetrunner.ObserveVisit(board, site.Shop, v.ShopVisit, at, ctx.Username)
	}
	board.Release(site.Shop, ctx.Username)
	switch {
	case !v.Walked:
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeWalkFailed, Reason: b.strikeWhy(key, at, fmt.Sprintf("could not reach %s", site.Key))})
		return
	case v.OpenFailed:
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeAborted, Reason: b.strikeWhy(key, at, v.Reason)})
		return
	}
	b.st.ClearStrikes(key)
	b.finish(ctx, b.visitResult(ctx, site, s.World, key, v.ShopVisit, v.Plan, spendable, reserve))
}

// visitReserve is what a visit holds back for the higher-priority runes at the pack-paid stations still
// ahead (runerunnerplan.ReserveAhead), cut to the share of those stations the bank cannot fund
// (runerunnerplan.ReserveUncovered): a bank that holds their coins refills the pack at the next station's
// top-up, and a hold against it only left Death, Nature and Law on the Wizards' Guild shelf. A bank never
// read, or one that is short, keeps the hold.
func (b *runeRunnerBot) visitReserve(ctx *Context, ahead []runerunnerplan.StationEstimate) map[string]int {
	reserve := runerunnerplan.ReserveAhead(ahead, b.set.RuneList, b.set.CapTable)
	if b.st.BankReadAt.IsZero() {
		return reserve
	}
	rn := b.roundNeed(ctx, b.st.Cursor+1)
	spare := b.st.BankCoins - b.set.CoinReserve - rn.KeepNow(b.st.LundailKeep)
	return runerunnerplan.ReserveUncovered(reserve, rn.PackNeed(), spare)
}

// topUp draws coins at a shop's nearest bank before the visit. It reports whether the station
// should go on (false: it was finished here, as NO COINS or a bank that would not open).
func (b *runeRunnerBot) topUp(ctx *Context, d *fleetrunner.Deps, site runerunnerplan.ShopSite, rn runerunnerplan.RoundNeed, inPack int) bool {
	need := rn.PackNeed()
	if !runerunner.WalkTries(d, site.Key+" bank", site.Bank, 4, runerunner.ShopWalkTimeout, 2) {
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeWalkFailed, Reason: fmt.Sprintf("could not reach the %s bank for coins", site.BankName)})
		return false
	}
	bank, read, ok := runerunner.OpenBankAt(d, site.Bank)
	if !ok {
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeAborted, Reason: fmt.Sprintf("the %s bank would not open for coins", site.BankName)})
		return false
	}
	b.st.NoteBank(read.Runes, read.Coins, time.Now())
	draw, keep := b.bankDraw(rn, false, read.Coins, inPack, need)
	if draw.NoCoins {
		runerunner.CloseBank(d, bank)
		b.noCoins = true
		b.logf(ctx, "NO COINS bank=%d reserve=%d need=%d", read.Coins, b.set.CoinReserve, draw.Need)
		b.finish(ctx, skippedForCoins("no coins at %s", site.BankName))
		return false
	}
	// The pack is emptied of everything but coins and the listed runes while the bank is open,
	// so the visit has room for its stacks.
	runerunner.DepositExcept(d, bank, append([]string{runerunner.CoinsName}, b.set.RuneList...))
	got, err := runerunner.WithdrawCoins(d, bank, draw.Amount)
	if err != nil {
		b.logf(ctx, "drawing %d coins at %s: %v", draw.Amount, site.BankName, err)
	}
	runerunner.CloseBank(d, bank)
	b.st.BankCoins = read.Coins - got
	b.logf(ctx, "coins drew=%d at=%s bank_left=%d cap=%d reserve=%d lundail_keep=%d est_cost=%d in_pack=%d why=%q", got, site.BankName, read.Coins-got, b.set.TripCoins, b.set.CoinReserve, keep, need, inPack+got, draw.Why)
	return true
}

// bankDraw is a pack-paid leg's coin draw from a bank that reads bankCoins: DrawAmount over the bank less
// Lundail's keep. The keep is worked out ONCE per round, at the draw station (first), from the whole
// round's purse (RoundNeed.BankKeep) and stored in State.LundailKeep; a shop-bank top-up leaves what
// RoundNeed.KeepNow of that stored figure still owes the bank-paid stations ahead. Worked out afresh from
// the bank it found, as it was before, the keep of a short purse shrank at every top-up (39,216, then
// about 2,500, then under 200 in a 610,000-coin round) and Lundail met about 190 coins at Gundai.
//
// The draw is the rune need, plus the ammo estimate only from coins that are left once the whole rune need is
// covered (runerunnerplan.AmmoDrawShare): a purse that is short of the runes gets exactly the draw it would get
// without ammo, and a round that has already left runes on a shelf for coins draws none for ammo.
func (b *runeRunnerBot) bankDraw(rn runerunnerplan.RoundNeed, first bool, bankCoins, inPack, need int) (runerunnerplan.Draw, int) {
	var keep int
	if first {
		keep = rn.BankKeep(bankCoins, b.set.CoinReserve)
		b.st.LundailKeep = keep
	} else {
		keep = rn.KeepNow(b.st.LundailKeep)
	}
	ammo := 0
	if waits, _ := runerunnerplan.AmmoWaitsForRunes(b.st); !waits {
		ammo = runerunnerplan.AmmoDrawShare(rn.Ammo, need, inPack, b.set.FareCoins, bankCoins-keep-b.set.CoinReserve)
	}
	draw := runerunnerplan.DrawAmount(runerunnerplan.DrawInput{
		TripCoins: b.set.TripCoins, BankCoins: bankCoins - keep, CoinReserve: b.set.CoinReserve,
		FareCoins: b.set.FareCoins, InPack: inPack, EstCost: need + ammo,
	})
	if ammo > 0 {
		draw.Why += fmt.Sprintf("; up to %d of it for ammo, from coins beyond the rune need of %d", ammo, need)
	}
	return draw, keep
}

// visitResult logs one visit line by line, runs the price check and says how the station ended.
func (b *runeRunnerBot) visitResult(ctx *Context, site runerunnerplan.ShopSite, world int, key string, v fleetrunner.ShopVisit, plan fleetplan.PurchasePlan, spendable int, reserve map[string]int) runerunnerplan.StationResult {
	r := runerunnerplan.StationResult{Bought: map[string]int{}, Spent: v.Spent, Reason: v.Reason}
	anyStock, anyBought := false, false
	for _, rune := range b.set.RuneList {
		if !site.Shop.Sells(rune) {
			continue
		}
		seen := v.Seen[rune]
		got := v.Bought[rune]
		capGP := b.set.CapTable.For(rune)
		switch {
		case seen <= 0:
			b.logf(ctx, "empty shop=%s world=%d rune=%s", site.Key, world, rune)
		case got <= 0 && site.Shop.PriceAt(rune, seen) > capGP:
			anyStock = true
			b.logf(ctx, "capped shop=%s world=%d rune=%s price=%d cap=%d stock=%d", site.Key, world, rune, site.Shop.PriceAt(rune, seen), capGP, seen)
		default:
			anyStock = true
			b.logf(ctx, "buy shop=%s world=%d rune=%s seen=%d bought=%d spent=%d left=%d cap=%d",
				site.Key, world, rune, seen, got, site.Shop.CostToBuy(rune, seen, got), seen-got, capGP)
		}
		if got > 0 {
			anyBought = true
			r.Bought[rune] = got
		}
	}
	// What the shelf still holds and why: the owner's per-station check that a buy-everything
	// runner left nothing it could have bought.
	for _, l := range runerunnerplan.LeftOnShelf(runerunnerplan.LeftInput{
		Shop: site.Shop, Runes: b.set.RuneList, Caps: b.set.CapTable, Seen: v.Seen, Bought: v.Bought,
		Plan: plan, Spendable: spendable, Reserve: reserve, VisitReason: v.Reason,
	}) {
		b.logf(ctx, "left %d %s shop=%s world=%d: %s", l.Left, l.Rune, site.Key, world, l.Reason)
		if l.ForCoins {
			r.ShortRunes += l.Left
			r.ShortCoins += l.Cost
		}
	}
	predicted := runerunnerplan.PredictedSpend(site.Shop, plan, v.Bought)
	if mismatch, disabled := b.st.NotePrice(key, predicted, v.Spent); mismatch {
		b.logf(ctx, "PRICE MODEL MISMATCH shop=%s world=%d predicted=%d spent=%d disabled=%v", site.Key, world, predicted, v.Spent, disabled)
	}
	switch {
	case anyBought:
		r.Outcome = runerunnerplan.OutcomeBought
	case anyStock:
		r.Outcome = runerunnerplan.OutcomeCapped
	default:
		r.Outcome = runerunnerplan.OutcomeEmpty
	}
	return r
}

// ---- stations 8 and 10: Lundail ----

// lundailStation is a Lundail station in Ardougne: the kit, the empty-crossing rule, the lever.
// The trip's legs then run in lundailLeg until the account is back.
func (b *runeRunnerBot) lundailStation(ctx *Context, d *fleetrunner.Deps, s runerunnerplan.Station) {
	now := time.Now()
	key := s.Key()
	switch {
	case ctx.World() != s.World:
		b.finish(ctx, skipped("on world %d; the station is for world %d", ctx.World(), s.World))
		return
	case b.lundailOff != "":
		b.finish(ctx, skipped("Lundail is off for this run: %s", b.lundailOff))
		return
	}
	if off, why := b.st.PairOff(key, now); off {
		b.finish(ctx, skipped("%s", why))
		return
	}
	deathsOK, deathsWhy := runerunnerplan.LundailAllowed(b.st.LundailDeaths, now, b.set.LundailMaxDeaths)
	if !deathsOK {
		b.finish(ctx, skipped("%s", deathsWhy))
		return
	}
	site, _ := runerunnerplan.Site(runerunnerplan.SiteLundail)
	est := b.estimate(ctx, site, s.World, now)
	worth := runerunnerplan.Worth(site.Shop, est, b.set.RuneList, b.set.CapTable)
	if ok, why := runerunnerplan.WorthVisiting(worth, b.set.MinVisitRunes); !ok {
		b.finish(ctx, skipped("%s (est=%d)", why, worth))
		return
	}
	// The kit banks the pack's coins in East Ardougne and draws the purchase at Gundai, so what
	// Lundail can spend is bank plus pack less the reserve, capped at tripCoins.
	if ok, why := b.affordable(d, site, est, fleetrunner.CoinsHeld(d), 0); !ok {
		b.finish(ctx, skippedForCoins("cannot afford: %s (last bank reading %d, reserve %d)", why, b.st.BankCoins, b.set.CoinReserve))
		return
	}

	// Ardougne first, on the long walk: PrepareKit's own bank walk is bounded at four minutes, and
	// from Varrock the boat chain takes far longer.
	if !runerunner.WalkTries(d, key, site.Approach, 4, runerunner.ShopWalkTimeout, 2) {
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeWalkFailed, Reason: b.strikeWhy(key, time.Now(), "could not reach the East Ardougne bank")})
		return
	}
	// The kit: everything banked in East Ardougne, one blade out, the purchase planned from the
	// bank's coins -- which stay in the bank until Gundai (the owner's Wilderness rule).
	//
	// Gundai is asked for the purchase only -- never the pack's float -- but for the purchase as the
	// shelf will be when the account gets there: planned from the board's hold-free upper estimate a
	// few minutes ahead, with the margin, never above a full shelf. LIVE 12 October (acct152): the plan
	// was 20,950 coins for 608 runes and the shelf held 713. What is not spent is banked again at Gundai.
	upper := b.estimateAt(site, s.World, now.Add(runeRunnerLundailLead))
	fullShelf := runerunnerplan.FullShelfCost(site.Shop, b.set.RuneList, b.set.CapTable)
	withdraw := 0
	planner := func(_ fleetplan.RuneStock, bankCoins int) fleetplan.PurchasePlan {
		spend := bankCoins - b.set.CoinReserve
		if spend > b.set.TripCoins {
			spend = b.set.TripCoins
		}
		withdraw = runerunnerplan.LundailWithdraw(runerunnerplan.PlanVisit(site.Shop, upper, spend, b.set.RuneList, b.set.CapTable, nil).Coins, fullShelf, spend)
		return runerunnerplan.PlanVisit(site.Shop, est, spend, b.set.RuneList, b.set.CapTable, nil)
	}
	kit := fleetrunner.PrepareKit(d, planner)
	if kit.BankRunes != nil {
		b.st.NoteBank(kit.BankRunes, kit.BankCoins, time.Now())
	}
	for _, p := range kit.Verdict.Problems {
		if strings.Contains(p, "no sword or knife") {
			b.lundailOff = "no sword or knife in the bank (the Mage Bank webs need one)"
			b.logf(ctx, "Lundail is off for this run: %s", b.lundailOff)
		}
	}
	go_, why := runerunnerplan.LeverGo(runerunnerplan.LeverCheck{
		KitOK: kit.Verdict.OK, KitWhy: kit.Reason, PlanCoins: kit.Plan.Coins, PlanRunes: kit.Plan.Runes(),
		MinVisitRunes: b.set.MinVisitRunes, DeathsOK: deathsOK, DeathsWhy: deathsWhy,
	})
	if !go_ {
		b.finish(ctx, skipped("%s", why))
		return
	}
	planned := max(kit.Plan.Coins, withdraw)
	b.st.Trip = fleetplan.Trip{PlannedCoins: planned}
	b.st.TripWorld = s.World
	b.st.TripResult = nil
	b.save(ctx)
	board := fleetrunner.ShopsFor(s.World)
	for _, l := range kit.Plan.Lines {
		board.Reserve(site.Shop, l.Rune, ctx.Username, l.Count, now, site.HoldTTL, b.set.Tick())
	}
	if !fleetrunner.PullArdougneLever(d) {
		board.Release(site.Shop, ctx.Username)
		b.st.Trip = fleetplan.Trip{}
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeAborted, Reason: "the Ardougne lever did not land in the Wilderness"})
		return
	}
	b.st.Trip.Active = true
	b.logf(ctx, "lundail world=%d phase=lever planned_coins=%d plan_coins=%d plan_runes=%d", s.World, planned, kit.Plan.Coins, kit.Plan.Runes())
	b.save(ctx)
}

// lundailLeg runs the trip under way: one leg per Loop in the Wilderness and the Mage Bank, and
// the trip's end once the account is back in Ardougne (or anywhere else, which is a death).
func (b *runeRunnerBot) lundailLeg(ctx *Context, d *fleetrunner.Deps, here *tile.Tile, area runerunnerplan.Area) {
	now := time.Now()
	site, _ := runerunnerplan.Site(runerunnerplan.SiteLundail)
	world := b.st.TripWorld
	if world <= 0 {
		world = ctx.World()
	}
	switch area {
	case runerunnerplan.AreaArdougne:
		// Home: the station is done, bought or not. Return so the next Loop decides afresh; never
		// fall through to a leg chosen against the old trip.
		r := b.st.EndTrip(now, "the trip came back without buying")
		fleetrunner.ShopsFor(world).Release(site.Shop, ctx.Username)
		b.logf(ctx, "lundail world=%d phase=home outcome=%s bought=%s", world, r.Outcome, runerunnerplan.FormatCounts(r.Bought))
		if b.stationIsLundail() {
			b.finish(ctx, r)
			return
		}
		b.save(ctx)
		return
	case runerunnerplan.AreaWild:
		// In the Wilderness or the Mage Bank: a trip is under way whatever was saved (the lever was
		// pulled just before a restart).
		b.st.Trip.Active = true
	default:
		// Out of the Wilderness and not in Ardougne mid-trip: a respawn.
		b.st.NoteLundailDeath(now)
		r := b.st.EndTrip(now, "left the trip somewhere other than Ardougne (a death)")
		b.logf(ctx, "DIED station=%s world=%d tile=%d,%d wild=0 lundail_deaths_24h=%d (trip ended away from Ardougne)",
			stationKeyAt(b.st), world, here.X, here.Z, len(b.st.LundailDeaths))
		if b.stationIsLundail() {
			b.st.Finish(r)
		}
		b.reenter = true
		b.save(ctx)
		return
	}
	var lundailCoins int
	planFor := func(seen map[string]int, coins int) fleetplan.PurchasePlan {
		lundailCoins = coins
		return runerunnerplan.PlanVisit(site.Shop, seen, coins, b.set.RuneList, b.set.CapTable, nil)
	}
	est := b.estimate(ctx, site, world, now)
	worthBuying := func(coins int) bool {
		return !runerunnerplan.GundaiShort(runerunnerplan.AffordInput{
			Shop: site.Shop, Stock: est, Runes: b.set.RuneList, Caps: b.set.CapTable, Coins: coins, MinVisitRunes: b.set.MinVisitRunes,
		})
	}
	leg := runerunner.LundailStep(d, &b.st.Trip, &b.webs, ctx.Session.State().InCombat(), planFor, worthBuying)
	if leg.Short {
		key := fmt.Sprintf("%s@%d", site.Key, world)
		r := runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeAborted, At: now,
			Reason: b.strikeWhy(key, now, "Gundai left too few coins to buy: "+leg.Note)}
		b.st.TripResult = &r
	}
	b.verbosef(ctx, "lundail world=%d phase=%s planned_coins=%d %s", world, leg.Phase, b.st.Trip.PlannedCoins, leg.Note)
	if leg.Note != "" && b.set.LogDetail != "Verbose" {
		b.logf(ctx, "lundail world=%d phase=%s %s", world, leg.Phase, leg.Note)
	}
	if leg.Visit != nil {
		board := fleetrunner.ShopsFor(world)
		fleetrunner.ObserveVisit(board, site.Shop, *leg.Visit, time.Now(), ctx.Username)
		board.Release(site.Shop, ctx.Username)
		r := b.visitResult(ctx, site, world, fmt.Sprintf("%s@%d", site.Key, world), *leg.Visit, leg.Plan, lundailCoins, nil)
		b.st.TripResult = &r
	}
	if leg.Deposited != nil && leg.Deposited.Runes != nil {
		b.st.NoteBank(leg.Deposited.Runes, leg.Deposited.Coins, time.Now())
	}
	if leg.Changed || leg.Visit != nil || leg.Deposited != nil {
		b.save(ctx)
	}
	if leg.Failed {
		ctx.Delay(3 * b.set.Tick())
	}
	b.heartbeat(ctx, "lundail", here)
}

// strikeWhy records a strike on a pair (a keeper that would not open, or a walk that failed) and
// returns the reason, saying so when the strike took the pair off the ring.
func (b *runeRunnerBot) strikeWhy(pair string, at time.Time, reason string) string {
	if b.st.Strike(pair, at) {
		reason += fmt.Sprintf("; %d strikes in a row: off the ring for %s", runerunnerplan.StrikeLimit, runerunnerplan.PairOffFor)
	}
	return reason
}

// ---- Zaff: Staffs of air (zaffStaffs > 0) ----

// staffHold is the coins Zaff's station leaves in a pack of packCoins for the runes ahead: their estimated cost at
// the stations still ahead, less what the bank can fund for them at their own shops (runerunnerplan.HoldAhead: a
// staff is paid after the runes, not instead of them), then cut to half the pack's spare (CappedReserve) as it always was.
func (b *runeRunnerBot) staffHold(ctx *Context, packCoins int) int {
	hold := runerunnerplan.EstCost(b.aheadEstimates(ctx, b.st.Cursor+1), b.set.RuneList, b.set.CapTable)
	if !b.st.BankReadAt.IsZero() {
		rn := b.roundNeed(ctx, b.st.Cursor+1)
		hold = runerunnerplan.HoldAhead(hold, true, b.st.BankCoins-b.set.CoinReserve-rn.KeepNow(b.st.LundailKeep))
	}
	return runerunnerplan.CappedReserve(hold, packCoins-b.set.FareCoins)
}

// zaffLine is one thing Zaff's station buys: the item, how many a visit and the most one may cost.
type zaffLine struct {
	item       string
	want, capG int
}

// staffStation buys up to zaffStaffs Staffs of air, zaffFireStaffs Staffs of fire and zaffBattlestaffs Battlestaffs from Zaff, none dearer
// than its cap. The staffs are the ring's lowest priority: the fare and the estimated cost of every listed
// rune at the stations still ahead stay in the pack.
func (b *runeRunnerBot) staffStation(ctx *Context, d *fleetrunner.Deps, s runerunnerplan.Station) {
	now := time.Now()
	key := s.Key()
	switch {
	case ctx.World() != s.World:
		b.finish(ctx, skipped("on world %d; the station is for world %d", ctx.World(), s.World))
		return
	case b.noCoins:
		b.finish(ctx, skippedForCoins("no coins"))
		return
	}
	if off, why := b.st.PairOff(key, now); off {
		b.finish(ctx, skipped("%s", why))
		return
	}
	lines := []zaffLine{
		{fleetplan.StaffOfAir, b.set.ZaffStaffs, b.set.ZaffStaffCap},
		{runerunnerplan.Battlestaff, b.set.ZaffBattlestaffs, b.set.ZaffBattlestaffCap},
		{runerunnerplan.StaffOfFire, b.set.ZaffFireStaffs, b.set.ZaffFireStaffCap},
	}
	r := runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeEmpty, At: time.Now()}
	var reasons []string
	bought := map[string]int{}
	tried := false
	for _, l := range lines {
		want := min(l.want, runerunner.FreeSlots(d))
		if l.want <= 0 {
			continue
		}
		if want <= 0 {
			reasons = append(reasons, "the pack has no free slot for a "+l.item)
			continue
		}
		hold := b.staffHold(ctx, fleetrunner.CoinsHeld(d))
		spendable := fleetrunner.CoinsHeld(d) - b.set.FareCoins - hold
		line, _ := runerunnerplan.Zaff.Line(l.item)
		if n, _ := runerunnerplan.PlanZaff(l.item, line.Base, want, l.capG, spendable); n <= 0 {
			reasons = append(reasons, fmt.Sprintf("%d coins spendable after the fare and %d held back for the runes ahead: not a %s at cap %d", spendable, hold, l.item, l.capG))
			continue
		}
		tried = true
		v := runerunner.BuyFromZaff(d, l.item, want, l.capG, spendable)
		at := time.Now()
		switch {
		case !v.Walked:
			b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeWalkFailed, Reason: b.strikeWhy(key, at, "could not reach Zaff")})
			return
		case v.OpenFailed:
			b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeAborted, Reason: b.strikeWhy(key, at, v.Reason)})
			return
		}
		b.st.ClearStrikes(key)
		predicted := runerunnerplan.PredictedSpend(runerunnerplan.Zaff, v.Plan, map[string]int{l.item: v.Bought})
		if mismatch, disabled := b.st.NotePrice(key, predicted, v.Spent); mismatch {
			b.logf(ctx, "PRICE MODEL MISMATCH shop=Zaff world=%d item=%s predicted=%d spent=%d disabled=%v", s.World, l.item, predicted, v.Spent, disabled)
		}
		r.Spent += v.Spent
		r.At = at
		if v.Reason != "" {
			reasons = append(reasons, v.Reason)
		}
		switch {
		case v.Bought > 0:
			r.Outcome = runerunnerplan.OutcomeBought
			bought[l.item] += v.Bought
			b.logf(ctx, "buy shop=Zaff world=%d item=%s seen=%d bought=%d spent=%d cap=%d", s.World, l.item, v.Seen, v.Bought, v.Spent, l.capG)
		case v.Seen <= 0:
			b.logf(ctx, "empty shop=Zaff world=%d item=%s", s.World, l.item)
		default:
			if r.Outcome != runerunnerplan.OutcomeBought {
				r.Outcome = runerunnerplan.OutcomeCapped
			}
			b.logf(ctx, "capped shop=Zaff world=%d item=%s price=%d cap=%d stock=%d spendable=%d", s.World, l.item,
				runerunnerplan.Zaff.PriceAt(l.item, v.Seen), l.capG, v.Seen, spendable)
		}
	}
	if !tried {
		b.finish(ctx, skipped("%s", strings.Join(reasons, "; ")))
		return
	}
	if len(bought) > 0 {
		r.Bought = bought
	}
	r.Reason = strings.Join(reasons, "; ")
	b.finish(ctx, r)
}
