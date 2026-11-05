package bot

// MasterCrafter's trawler round (mastercraftplan/trawler.go): the main account is the Fishing Trawler's anchor.
//
//	prepare  the nearest bank (trawlerplan.NearestBank): deposit everything but the trawler kit, coins and the alchemy kit;
//	         draw the alchemy kit (natures, noted alchables) and a fire staff, wielded; coins for the store
//	kit      the Port Khazard store's Swamp paste, Rope and a Bailing bucket (trawlerrunner.BuyKit, the crew's own)
//	lobby    walk to the lobby, cross the gangplank and call the crew aboard on the wall bus (mulebus.TrawlerBoard) until the
//	         boat takes everyone aboard; alch while waiting
//	aboard   the crew's moves (trawlerplan.Aboard: leaks, the net, bailing), alch when there is nothing to do; NEVER leave
//	claim    off the boat after the game: Inspect the net, the best stacks into the pack, the rest dumped
//	         (trawlerrunner.Claim; the loot panel's Fishing Trawler row), then the catch banked
//
// Every walk casts High Level Alchemy (the round's Travel). The round ends after the claim; Decide starts the next one straight
// on (Decision.Continue) while the lobby still holds the quorum.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/alchplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/pearlplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/trawlerplan"
	"rsb0t-go/internal/scripts/trawlerrunner"
)

// The round's clocks.
const (
	// trawlerBoardWait is how long the anchor waits in the lobby to be taken aboard (the countdown is 100 ticks).
	trawlerBoardWait = 5 * time.Minute
	// trawlerGameMax bounds a game aboard (1,000 ticks and the 20-tick journey, with room).
	trawlerGameMax = 16 * time.Minute
	// trawlerCallEvery is how often the anchor repeats its board call while it waits.
	trawlerCallEvery = 5 * time.Second
	// trawlerCoins is the coins the anchor carries for the store (paste 10 each at most, rope and a bucket).
	trawlerCoins = 2000
	// trawlerMusterSettle is how long the lobby count must stop growing before the anchor boards; trawlerMusterMax bounds the
	// whole muster (the fleet walks from all over the map at a block's start).
	// trawlerAnchorPaste and trawlerAnchorRope are the anchor's kit; trawlerClaimSlots the slots it keeps free for the claim.
	trawlerAnchorPaste  = 150
	trawlerAnchorRope   = 10 // Crafting 96 mends every rip and refills at the store each game (4 Nov 2026: 5 was spent in a game's first minutes)
	trawlerClaimSlots   = 8
	trawlerMusterSettle = time.Minute
	// trawlerMusterShare is the share (percent) of the last muster's crew the anchor boards with once it is back.
	trawlerMusterShare = 90
	// trawlerStraightShare is the share (percent) of the last muster's crew that sends the anchor straight back to the boat after
	// a claim instead of banking: the bank and the muster after it cost some 2.5 minutes of a ~15.5-minute cycle, so boarding
	// at once pays while the crew already there is over 13/15.5 (84%) of the crew the wait would gather. Live 4 Nov 2026
	// 21:24-22:27, at 90, five games in a row banked first with 117-141 crew waiting and cycles of 15.5-16 minutes.
	trawlerStraightShare = 85
	trawlerMusterMax     = 15 * time.Minute
	// trawlerPollEvery spaces the cheap lobby count between checks (Loop).
	trawlerPollEvery = 30 * time.Second
)

// masterFishingStat is UPDATE_STAT's index for Fishing (gamestate.SkillNames[10]).
const masterFishingStat = 10

// trawlerXPNow is the Fishing, Magic and Fletching experience now (mastercraftplan.TrawlerLedger.BlockXP's order).
func (b *masterCrafterBot) trawlerXPNow(ctx *Context) [3]int {
	return [3]int{masterSkillXP(ctx, masterFishingStat), masterSkillXP(ctx, mastercraftrunner.MagicStat), masterSkillXP(ctx, masterFletchingStat)}
}

// trawlerRate says the block's experience so far and an hour: Fishing, and Magic (the alchemy) and Fletching (the tipping)
// beside it, over the time since the block started. Empty before the block's start was read.
func (b *masterCrafterBot) trawlerRate(ctx *Context) string {
	l := &b.ledger.Trawler
	now := b.trawlerXPNow(ctx)
	if l.BlockXP == [3]int{} || l.BlockXPAt.IsZero() || l.BlockStart.IsZero() {
		if now[0] > 0 && !l.BlockStart.IsZero() {
			l.BlockXP, l.BlockXPAt = now, time.Now() // a ledger from before BlockXP: count from here
		}
		return ""
	}
	took := time.Since(l.BlockXPAt)
	if took < time.Minute {
		return ""
	}
	per := func(i int) string {
		gained := now[i] - l.BlockXP[i]
		return fmt.Sprintf("+%s (%s/h)", masterXPShort(gained), masterXPShort(int(float64(gained)/took.Hours())))
	}
	return fmt.Sprintf("block %s: Fishing %s, Magic %s, Fletching %s", took.Round(time.Minute), per(0), per(1), per(2))
}

// masterXPShort writes an experience count as 512k or 1.2M.
func masterXPShort(n int) string {
	switch {
	case n >= 1_000_000 || n <= -1_000_000:
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	case n >= 1000 || n <= -1000:
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprint(n)
}

// trawlerSnapshot fills the trawler round's reading: the crew in the lobby on this world, where the account stands, its
// Fishing level.
func (b *masterCrafterBot) trawlerSnapshot(ctx *Context, s *mastercraftplan.Snapshot) {
	if ctx.Bus != nil {
		s.Trawler.Crew = mulebus.TrawlerLobbyCount(mulebus.ReadTrawler(ctx.Bus, time.Now(), 0), ctx.Username, ctx.World(), 0)
	}
	if ctx.Session != nil {
		if x, z, level, ok := ctx.Session.State().Tile(); ok {
			s.Trawler.Here, s.Trawler.HereKnown = tile.Tile{X: x, Z: z, Level: level}, true
		}
		if lvl, ok := skillLevel(ctx, masterFishingStat); ok {
			s.Trawler.FishingLevel = lvl
		}
		s.Trawler.FishingXP = masterSkillXP(ctx, masterFishingStat)
	}
	s.Trawler.Food, s.Trawler.FoodKnown = b.trawlerFood(ctx)
}

// trawlerFood is the RAW fish the mule's published bank holds that the trawler restocks (trawlerFoods): Raw shark, Raw sea turtle and
// Raw manta ray (a user request: a block starts when the mule is out of all three; it counted cooked and raw before). known is
// false while the mule's bank is not on the peer board.
func (b *masterCrafterBot) trawlerFood(ctx *Context) (n int, known bool) {
	count := muleBankCount(ctx, b.mule)
	if count == nil {
		return 0, false
	}
	for _, f := range trawlerFoods {
		c, _ := count(f)
		n += c
	}
	return n, true
}

// trawlerFoods are the foods trawlerFood counts.
var trawlerFoods = []string{"Raw shark", "Raw sea turtle", "Raw manta ray"}

// trawlerBlockNow reports whether the two-hour trawler block runs and no backoff holds it (mastercraftplan.TrawlerBlockOn).
func (b *masterCrafterBot) trawlerBlockNow(ctx *Context, now time.Time) bool {
	if _, held := b.ledger.BackedOff(mastercraftplan.ModeTrawler, now); held {
		return false
	}
	in := mastercraftplan.TrawlerInput{FishingXP: masterSkillXP(ctx, masterFishingStat)}
	in.Food, in.FoodKnown = b.trawlerFood(ctx)
	return mastercraftplan.TrawlerDue(b.set, in, &b.ledger, now)
}

// trawlerPoll, while ScarabSwarm runs between rounds and a block is on, publishes the gather call that brings the fleet to the
// lobby (mulebus.TrawlerGather) and turns the next pass into a check, so the round starts now rather than at the next interval.
// Cheap: every trawlerPollEvery.
func (b *masterCrafterBot) trawlerPoll(ctx *Context, now time.Time) {
	b.trawlerPrecall(ctx, now)
	if !b.set.Trawler || ctx.Bus == nil || b.pending.Mode != "" || now.Sub(b.trawlerPolled) < trawlerPollEvery {
		return
	}
	b.trawlerPolled = now
	if b.trawlerBlockNow(ctx, now) {
		b.trawlerGather(ctx)
		b.lastCheck = time.Time{}
	}
}

// trawlerPrecallLead is how long before a rest's end the anchor calls the fleet to the lobby: about the crew's walk from where
// it spends the rest. Called at the block's start, the crew came in over 15-19 minutes of the block's two hours and its first
// game sailed with 20-56 of them, against 110-160 in the games after (live 4 Nov 2026, 07:53 and 3 Nov 23:42 and 4 Nov 03:49).
const trawlerPrecallLead = 15 * time.Minute

// trawlerPrecall publishes the gather call in a rest's last trawlerPrecallLead, while the anchor still crafts or runs
// ScarabSwarm, so the crew is in the lobby when the block starts. A rest that ends early (nothing left to craft) is not
// foreseen: that block's crew is called at its start, as before.
func (b *masterCrafterBot) trawlerPrecall(ctx *Context, now time.Time) {
	if !b.set.Trawler || ctx.Bus == nil || now.Sub(b.trawlerPrecalled) < trawlerPollEvery {
		return
	}
	rest := b.ledger.Trawler.RestUntil
	if !now.Before(rest) || rest.Sub(now) > trawlerPrecallLead || masterSkillXP(ctx, masterFishingStat) >= mastercraftplan.MaxSkillXP {
		return
	}
	b.trawlerPrecalled = now
	b.trawlerGather(ctx)
}

// trawlerGather publishes the gather call from where the account stands.
func (b *masterCrafterBot) trawlerGather(ctx *Context) {
	if ctx.Bus == nil || ctx.Session == nil || ctx.Username == "" {
		return
	}
	if x, z, level, ok := ctx.Session.State().Tile(); ok {
		ctx.Bus.Publish(mulebus.WithWatch(mulebus.TrawlerMessage(ctx.Username, tile.Tile{X: x, Z: z, Level: level}, mulebus.TrawlerGather, ctx.World()), b.trawlerWatch()))
	}
}

// trawlerWatch is the crew watch the anchor calls to its next game (trawlerplan.GameWatch).
func (b *masterCrafterBot) trawlerWatch() int { return trawlerplan.GameWatch(b.ledger.Trawler.Games) }

// trawlerWaiting ends a long round (roundHold) when the two-hour trawler block starts, so the fleet gathered in the lobby is
// not kept waiting for hours of superheating or fletching; the round resumes in the next rest. A round started in a rest
// yields too when the rest's two hours are up (mastercraftplan.TrawlerBlockOn). Not while trawler rounds back off.
func (b *masterCrafterBot) trawlerWaiting(ctx *Context, now time.Time) bool {
	b.trawlerPrecall(ctx, now)
	if !b.set.Trawler || now.Sub(b.trawlerHeld) < trawlerPollEvery {
		return false
	}
	b.trawlerHeld = now
	return b.trawlerBlockNow(ctx, now)
}

// trawlerTravelFor is the round's walk alch: High Level Alchemy on the owner's alch stock with a fire staff, run kept on.
func (b *masterCrafterBot) trawlerTravelFor() *fleetrunner.Travel {
	return trawlerTravel(true, b.set.MembersWorld, false)
}

// trawlerCall publishes the anchor's role on the wall bus at most every trawlerCallEvery.
func (b *masterCrafterBot) trawlerCall(ctx *Context, d *fleetrunner.Deps, role string) {
	if ctx.Bus == nil || ctx.Username == "" || time.Since(b.trawlerCalled) < trawlerCallEvery {
		return
	}
	b.trawlerCalled = time.Now()
	ctx.Bus.Publish(mulebus.WithWatch(mulebus.TrawlerMessage(ctx.Username, trawlerrunner.Here(d), role, ctx.World()), b.trawlerWatch()))
}

// trawlerRound is the round: see the file comment.
func (b *masterCrafterBot) trawlerRound(ctx *Context, d *fleetrunner.Deps, started time.Time, dec mastercraftplan.Decision) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeTrawler}
	travel := b.trawlerTravelFor()
	d.Travel = travel
	l := &b.ledger.Trawler
	l.Rounds++
	defer func() {
		casts, _, _, _ := travel.Counters()
		b.logf(ctx, "trawler: round over after %s, %d alch cast(s): %s", time.Since(started).Round(time.Second), casts, out.Why)
	}()
	fail := func(format string, args ...any) masterRoundOutcome {
		out.Transient = true
		out.Why = fmt.Sprintf(format, args...)
		return out
	}

	escape := func() {
		if err := trawlerrunner.Escape(d); err != nil {
			b.logf(ctx, "WARNING: trawler: the boat sank: %v", err)
		}
	}
	if trawlerplan.InWreck(trawlerrunner.Here(d)) {
		escape()
		l.Claim = false
		out.Why = "the boat sank before this round: climbed onto a barrel"
		return out // a sinking is the crew's to fix, not a reason to back the block off (live 2 Nov 2026: master left for a pearl round)
	}
	if !trawlerplan.OnBoat(trawlerrunner.Here(d)) && !l.Claim {
		straight := b.trawlerStraight
		b.trawlerStraight = false
		if !straight {
			if why, ok := b.trawlerPrepare(ctx, d, travel); !ok {
				return fail("%s", why)
			}
		}
		if why, ok := b.trawlerBoard(ctx, d, travel); !ok {
			return fail("%s", why)
		}
	}
	if trawlerplan.OnBoat(trawlerrunner.Here(d)) {
		if !l.Claim {
			l.Claim, l.ClaimAfter = true, time.Now().Add(trawlerplan.GameAfterBoarding)
			b.saveLedger(ctx)
		}
		why, ok := b.trawlerAboard(ctx, d, travel)
		if !ok {
			return fail("%s", why)
		}
		if trawlerplan.InWreck(trawlerrunner.Here(d)) {
			escape()
			l.Claim = false
			b.saveLedger(ctx)
			out.Why = "the boat sank: climbed onto a barrel"
			return out // not a backoff: the next game starts now
		}
		l.ClaimAfter = time.Time{} // over: claim now
		b.saveLedger(ctx)
	}
	if ctx.Stopped() {
		out.Why = "stopped"
		return out
	}
	// A game this account left early (a disconnect) is still at sea: its catch is set only at the end.
	if wait := time.Until(l.ClaimAfter); wait > 0 && wait <= trawlerplan.GameAfterBoarding {
		b.logf(ctx, "trawler: the game boarded before the break ends in %s: waiting to claim its catch", wait.Round(time.Second))
		for time.Now().Before(l.ClaimAfter) {
			if ctx.Stopped() {
				out.Why = "stopped"
				return out
			}
			b.trawlerIdle(ctx, travel, d, 2*time.Second)
		}
	}
	// The claim: off the boat after the game.
	taken, rolls, err := trawlerrunner.Claim(d)
	l.Claim = false
	b.saveLedger(ctx)
	masterCloseDialog(ctx)
	if err != nil {
		// No catch (the boat sank, or another account's fish): nothing to retry.
		out.Why = "no catch to claim: " + err.Error()
		return out
	}
	n := 0
	for _, c := range taken {
		n += int(c)
	}
	l.Claims++
	l.Taken += n
	out.Produced = n
	b.logf(ctx, "trawler: claimed %d of the %d item(s) the net held: %s (the other fish spilled on the pier for the crew, theirs in a minute); %s",
		n, rolls, masterDescribe(namedCounts(d, taken)), b.trawlerRate(ctx))
	if crew, ok := b.trawlerStraightOn(ctx, d); ok {
		b.trawlerStraight = true
		b.logf(ctx, "trawler: %d crew of watch %d already in the lobby: straight back to the boat, the catch banked at a later game", crew, b.trawlerWatch())
	} else if bank, berr := trawlerrunner.BankCatch(d); berr != nil {
		b.logf(ctx, "WARNING: trawler: banking the catch: %v (the fish stay in the pack)", berr)
	} else {
		trawlerrunner.DrawAlchKit(d, bank, travel)
	}
	out.Why = fmt.Sprintf("claimed %d of %d item(s)", n, rolls)
	return out
}

// trawlerStraightNatures is the Nature runes the anchor must carry to skip a bank visit between games: the alchs of a few games.
const trawlerStraightNatures = 1000

// trawlerStraightOn reports whether the anchor skips the bank after a claim: the watch called next is already in the lobby in
// the numbers its muster boards with (trawlerMuster's enough), another game follows (not a block's last before a rest), and
// the alchemy kit lasts.
// The Fishing XP comes with the inspect, not with what the pack keeps (trawler_win.rs2: every roll's stat_advance happens as
// the window fills), so a full pack costs the next claim only the fish, which spill for the crew; the bank round trip to
// Yanille and back costs about 85 s of a 14-minute cycle (live 4 Nov 2026, 09:45:13-09:46:40, with watch 2 back by 09:45).
func (b *masterCrafterBot) trawlerStraightOn(ctx *Context, d *fleetrunner.Deps) (int, bool) {
	if ctx.Bus == nil || !b.trawlerBlockNow(ctx, time.Now()) || !fleetrunner.FireStaffWorn(d) ||
		fleetrunner.CountPackNamed(d, alchplan.NatureRune) < trawlerStraightNatures {
		return 0, false
	}
	t := b.ledger.Trawler
	if b.set.Sched.TrawlerRest > 0 && !t.BlockStart.IsZero() && time.Since(t.BlockStart) >= b.set.Sched.TrawlerBlock {
		return 0, false
	}
	watch := b.trawlerWatch()
	ref := t.Peaks[watch]
	if ref == 0 {
		return 0, false
	}
	crew := mulebus.TrawlerLobbyCount(mulebus.ReadTrawler(ctx.Bus, time.Now(), 0), ctx.Username, ctx.World(), watch)
	return crew, crew >= max(b.set.TrawlerQuorum, ref*trawlerStraightShare/100)
}

// namedCounts names an obj-id count map for the log.
func namedCounts(d *fleetrunner.Deps, c map[int]int64) map[string]int {
	out := map[string]int{}
	for id, n := range c {
		out[fleetrunner.ObjName(d, int32(id))] += int(n)
	}
	return out
}

// masterCloseDialog continues a chat dialog the claim left open (the net's "I'd better not steal other people's fish!").
func masterCloseDialog(ctx *Context) {
	dlg := actions.NewChatDialog(ctx.Session, ctx.Components)
	for i := 0; i < 3 && dlg.CanContinue(); i++ {
		_ = dlg.Continue()
		ctx.Delay(600 * time.Millisecond)
	}
}

// trawlerPrepare banks for the round (the trawler kit, coins and the alchemy kit stay; the rest is deposited so the claim has
// room), draws the alchemy kit and the store's coins, and buys the trawler kit at Port Khazard.
func (b *masterCrafterBot) trawlerPrepare(ctx *Context, d *fleetrunner.Deps, travel *fleetrunner.Travel) (string, bool) {
	set := trawlerplan.DefaultSettings()
	// The anchor's own kit (a user request: "more rope bailing bucket and swamp paste so that the inventory is full and he
	// has over 100 swamp paste"): 150 paste (one stack), trawlerAnchorRope rope, and buckets in every slot but
	// trawlerClaimSlots, which the claim needs for the best of the catch.
	set.KitPaste, set.KitRope = trawlerAnchorPaste, trawlerAnchorRope
	keep := func(it gamestate.SlotItem) bool {
		name := fleetrunner.ObjName(d, it.ID)
		switch {
		case it.ID == trawlerplan.PasteID, it.ID == trawlerplan.RopeID, it.ID == trawlerplan.BucketID, it.ID == trawlerplan.BucketFullID:
			return trawlerAnchorFixes
		case strings.EqualFold(name, "Coins"), alchplan.IsFireStaff(name), trawlerTeleRune(it.ID):
			return true
		case trawlerTipItem(it.ID):
			return trawlerTipUsable(it.ID, func(id int32) int { return int(fleetrunner.PackCountOf(d, int(id))) })
		}
		return travel.AlchKeeps(d, name)
	}
	// The mule first, then the bank: the ask's own visit to the bank at the post deposits what it does not keep, and took the
	// alchemy kit drawn before it (live 3 Nov 2026 17:34: master tipped with no Nature rune or alchable left, so cast nothing).
	// The mule is asked only from its post: from Yanille the ask came back with nothing.
	// From the mule's post, or once from anywhere when the pack has no tip use left: the tips last many games (live 3 Nov 2026
	// the mule held about 170,000 arrowtips while master tipped nothing).
	arrowsLow := trawlerArrowUses(d) < trawlerTipsMin && b.muleHasArrowtips(ctx)
	if uses := trawlerTipUses(d); !b.farFromMule(ctx) || ((uses == 0 || arrowsLow) && len(b.trawlerTipAsk(ctx, d)) > 0) {
		var lines []mastercraftplan.Line
		if laws := fleetrunner.PackCountOf(d, int(trawlerLawRune)); laws < trawlerLawsMin {
			lines = append(lines, mastercraftplan.Line{Item: "Law rune", Qty: trawlerLawsAsk}, mastercraftplan.Line{Item: "Water rune", Qty: 2 * trawlerLawsAsk}, mastercraftplan.Line{Item: "Air rune", Qty: 5 * trawlerLawsAsk})
		}
		if uses := trawlerTipUses(d); uses < trawlerTipsMin || arrowsLow {
			lines = append(lines, b.trawlerTipAsk(ctx, d)...)
		}
		if len(lines) > 0 {
			a := b.askMule(ctx, d, lines)
			b.logf(ctx, "trawler: asked %s for teleport runes and every tip it holds: %s (declined %v)", b.mule, masterDescribe(a.Got), a.Declined)
		}
	}
	at := trawlerplan.NearestBank(trawlerrunner.Here(d))
	bank, ok := fleetrunner.OpenBankDepositing(d, at, func(it gamestate.SlotItem) bool { return !keep(it) })
	if !ok || bank == nil {
		return fmt.Sprintf("the bank at %d,%d did not open (at %s)", at.X, at.Z, fleetrunner.HereIs(d)), false
	}
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	if coins := fleetrunner.PackCountOf(d, 995); coins < trawlerCoins/2 {
		for _, it := range bank.Items() {
			if it.ID == 995 {
				if err := bank.WithdrawX(995, int32(min(int(it.Count), trawlerCoins-coins)), w); err != nil {
					b.logf(ctx, "trawler: withdrawing coins: %v", err)
				}
				break
			}
		}
	}
	// The tips and the teleport runes first: the alchemy kit's draw closes the bank (live 2 Nov 2026: "required trade screen
	// is not open" for every withdrawal after it).
	trawlerDrawTips(ctx, bank)
	for _, id := range trawlerTeleRunes {
		if actions.CountByID(bank.Items(), id) > 0 {
			_ = bank.WithdrawAll(id)
		}
	}
	staff, natures := trawlerrunner.DrawAlchKit(d, bank, travel)
	b.logf(ctx, "trawler: banked at %d,%d: fire staff worn %v, %d Nature runes, %d free slots", at.X, at.Z, staff, natures, fleetrunner.FreeSlots(d))
	// An anchor that fixes nothing aboard carries no trawler kit: every free slot is the claim's (a user request: "doesnt
	// look like loot is properly looted" - the game drops what the pack cannot take, for this account alone, for 60 s).
	if !trawlerAnchorFixes {
		return "", true
	}
	held := trawlerrunner.Held(d)
	pasteSlot := 0
	if held.Paste == 0 {
		pasteSlot = 1
	}
	set.KitBuckets = max(1, fleetrunner.FreeSlots(d)-trawlerClaimSlots-max(0, set.KitRope-held.Rope)-pasteSlot+held.Bucket+held.BucketFull)
	c, complete, err := trawlerrunner.BuyKit(d, set)
	if !complete {
		b.logf(ctx, "trawler: the store could not fill the kit (%v): going with %d paste, %d rope, %d bucket", err, c.Paste, c.Rope, c.Bucket+c.BucketFull)
	}
	return "", true
}

// trawlerBoard walks to the lobby, crosses the gangplank and calls the crew aboard until the boat takes the account aboard.
func (b *masterCrafterBot) trawlerBoard(ctx *Context, d *fleetrunner.Deps, travel *fleetrunner.Travel) (string, bool) {
	if !trawlerplan.InLobby(trawlerrunner.Here(d)) && !b.trawlerWalkToLobby(ctx, d) {
		return fmt.Sprintf("could not reach the trawler lobby from %s", fleetrunner.HereIs(d)), false
	}
	if why, ok := b.trawlerMuster(ctx, d, travel); !ok {
		return why, false
	}
	deadline := time.Now().Add(trawlerBoardWait)
	var crossed time.Time
	for time.Now().Before(deadline) {
		if ctx.Stopped() {
			return "stopped", false
		}
		here := trawlerrunner.Here(d)
		if trawlerplan.OnBoat(here) {
			b.logf(ctx, "trawler: aboard after %s in the lobby (watch %d)", time.Since(deadline.Add(-trawlerBoardWait)).Round(time.Second), b.trawlerWatch())
			b.ledger.Trawler.Games++
			b.saveLedger(ctx)
			return "", true
		}
		// Moved off the pier (live 2 Nov 2026: a Genie's fail-teleport put master in the Wizards' Tower and his board call sent
		// 267 crew to sea without him): back to the lobby first, and no board call until he stands there.
		if !b.trawlerAtLobby(here) {
			b.logf(ctx, "trawler: moved off the pier to %s: walking back before the board call", fleetrunner.HereIs(d))
			if !trawlerrunner.WalkTo(d, trawlerplan.LobbyStand, 2) {
				return fmt.Sprintf("could not get back to the trawler lobby from %s", fleetrunner.HereIs(d)), false
			}
			continue
		}
		b.trawlerCall(ctx, d, mulebus.TrawlerBoard)
		if !trawlerplan.InLobby(trawlerrunner.Here(d)) && time.Since(crossed) >= 25*time.Second {
			if err := trawlerrunner.Cross(d); err != nil {
				b.logf(ctx, "trawler: gangplank: %v", err)
			} else {
				crossed = time.Now()
			}
		}
		b.trawlerIdle(ctx, travel, d, 2*time.Second)
	}
	return fmt.Sprintf("not taken aboard within %s of reaching the lobby", trawlerBoardWait), false
}

// trawlerLobbyWalks bounds the walks to the lobby a round makes while each gets it closer: one walk times out after 2m30s, and
// from the mule's post at Catherby the walk is about 450 tiles (live 3 Nov 2026 12:26: the tip ask's walk back timed out
// halfway and backed the trawler off for 30 minutes).
const trawlerLobbyWalks = 4

// trawlerWalkToLobby walks to the lobby, again while each walk ends closer than the last.
func (b *masterCrafterBot) trawlerWalkToLobby(ctx *Context, d *fleetrunner.Deps) bool {
	best := tile.Chebyshev(trawlerrunner.Here(d), trawlerplan.LobbyStand)
	for i := 0; i < trawlerLobbyWalks && !ctx.Stopped(); i++ {
		if trawlerrunner.WalkTo(d, trawlerplan.LobbyStand, 2) {
			return true
		}
		dist := tile.Chebyshev(trawlerrunner.Here(d), trawlerplan.LobbyStand)
		if dist >= best {
			return false
		}
		b.logf(ctx, "trawler: the walk to the lobby stopped %d tiles short at %s: walking on", dist, fleetrunner.HereIs(d))
		best = dist
	}
	return false
}

// trawlerAtLobby reports whether the anchor stands on the lobby deck or by the gangplank on the pier.
func (b *masterCrafterBot) trawlerAtLobby(t tile.Tile) bool {
	return trawlerplan.InLobby(t) || (t.Level == trawlerplan.LobbyStand.Level && tile.Chebyshev(t, trawlerplan.LobbyStand) <= 6)
}

// trawlerMuster waits at the lobby, calling the fleet (mulebus.TrawlerGather), until the crew there reaches the quorum and has
// stopped growing for trawlerMusterSettle -- every account aboard adds to the catch -- or trawlerMusterMax has passed. It fails
// when under half the quorum came.
func (b *masterCrafterBot) trawlerMuster(ctx *Context, d *fleetrunner.Deps, travel *fleetrunner.Travel) (string, bool) {
	if ctx.Bus == nil {
		return "", true
	}
	start := time.Now()
	best, grew, said := 0, start, start
	watch := b.trawlerWatch()
	peak := &b.ledger.Trawler.Peaks[watch]
	// ref is the crew this muster expects: the watch's own last boarding, else the other watch's (the two are halves of the
	// fleet). With neither (the first game after a restart) the muster runs its full trawlerMusterMax: the fleet walks from all
	// over the map, its count stalls early, and a 5-minute floor sailed watch 1 with 16 of about 150 (live 3 Nov 2026, 08:02),
	// a count the next musters would then have taken as the whole watch.
	ref := *peak
	if ref == 0 {
		for w, p := range b.ledger.Trawler.Peaks {
			if w != watch && p > 0 {
				ref = p
			}
		}
	}
	for {
		if ctx.Stopped() {
			return "stopped", false
		}
		now := time.Now()
		if !b.trawlerAtLobby(trawlerrunner.Here(d)) {
			b.logf(ctx, "trawler: moved off the pier during the muster (at %s): walking back", fleetrunner.HereIs(d))
			if !trawlerrunner.WalkTo(d, trawlerplan.LobbyStand, 2) {
				return fmt.Sprintf("could not get back to the trawler lobby from %s", fleetrunner.HereIs(d)), false
			}
		}
		b.trawlerCall(ctx, d, mulebus.TrawlerGather)
		crew := mulebus.TrawlerLobbyCount(mulebus.ReadTrawler(ctx.Bus, now, 0), ctx.Username, ctx.World(), watch)
		if crew > best {
			best, grew = crew, now
		}
		// Most of the last game's crew back is enough: the stragglers walk from Port Sarim after the hop and kept the muster at its
		// full 15 minutes (live 3 Nov 2026: a 30-minute cycle, half of it waiting).
		enough := ref > 0 && crew >= max(b.set.TrawlerQuorum, ref*trawlerMusterShare/100)
		// The stopped-growing rule only once the crowd is back: right after a game the crew are still hopping home and walking
		// from Port Sarim, the count stalls early, and game 15 sailed with 35 of 257 (live 3 Nov 2026, 03:48).
		settled := now.Sub(grew) >= trawlerMusterSettle && ref > 0 && crew >= ref/2
		switch {
		case enough || (crew >= b.set.TrawlerQuorum && (settled || now.Sub(start) >= trawlerMusterMax)):
			*peak = max(best, *peak*95/100)
			b.logf(ctx, "trawler: %d crew of watch %d in the lobby after %s: boarding", crew, watch, now.Sub(start).Round(time.Second))
			return "", true
		case now.Sub(start) >= trawlerMusterMax:
			if crew < b.set.TrawlerQuorum/2 {
				b.ledger.Trawler.Games++ // the other watch next: this one is scattered
				b.saveLedger(ctx)
				return fmt.Sprintf("only %d crew came to the lobby in %s (quorum %d): not boarding", crew, trawlerMusterMax, b.set.TrawlerQuorum), false
			}
			b.logf(ctx, "trawler: %d crew of watch %d in the lobby after %s (quorum %d): boarding with them", crew, watch, trawlerMusterMax, b.set.TrawlerQuorum)
			return "", true
		case now.Sub(said) >= time.Minute:
			said = now
			b.logf(ctx, "trawler: mustering watch %d: %d crew in the lobby (quorum %d, most %d), %s waited", watch, crew, b.set.TrawlerQuorum, best, now.Sub(start).Round(time.Second))
		}
		b.trawlerIdle(ctx, travel, d, 2*time.Second)
	}
}

// trawlerTips are the tip-and-base pairs the anchor puts together between the trawler's other actions (a user request:
// "adding arrowtips to headless arrows or pearl bolt tips onto bolts inbetween alchs and other actions as that can be done 1t
// with no delay"): content's make_bolts and make_arrows take no delay, up to a set of ten or fifteen a use. The best first
// (a user request: "master should tip the best arrows available first before moving to lower tiers"): fifteen arrows a use
// at 12.5, 10, 7.5 and 5 xp each (fletching arrows.dbrow), ten pearl bolts at 3.2.
var trawlerTips = []struct {
	tip, base, product int32
	tipName, name      string
}{
	{44, mastercraftplan.HeadlessID, 892, "Rune arrowtips", "Rune arrow"},
	{43, mastercraftplan.HeadlessID, 890, "Adamant arrowtips", "Adamant arrow"},
	{42, mastercraftplan.HeadlessID, 888, "Mithril arrowtips", "Mithril arrow"},
	{mastercraftplan.ArrowTipsID, mastercraftplan.HeadlessID, mastercraftplan.SteelArrowID, mastercraftplan.ArrowTips, mastercraftplan.SteelArrow},
	{pearlplan.TipsID, pearlplan.BoltID, pearlplan.PearlBoltID, pearlplan.Tips, pearlplan.PearlBolt},
	// a user request: headless arrows "it can do at the trawler while alching" (15 a use, 1 XP each).
	{mastercraftplan.FeatherID, mastercraftplan.ArrowShaftID, mastercraftplan.HeadlessID, mastercraftplan.Feather, mastercraftplan.Headless},
}

// trawlerTipsMin is the tip uses under which the trawler round asks the mule for every tip it holds, in one ask (owner, 3 Nov
// 2026: "should request all tips in 1 go from the mule"), with the headless arrows and bolts to match.
const trawlerTipsMin = 200

// trawlerTipUses is how many tip uses the pack holds: per pair, the sets its tips and bases make.
func trawlerTipUses(d *fleetrunner.Deps) int {
	n := 0
	bases := map[int32]int{}
	for _, p := range trawlerTips {
		if _, seen := bases[p.base]; !seen {
			bases[p.base] = int(fleetrunner.PackCountOf(d, int(p.base)))
		}
		k := min(int(fleetrunner.PackCountOf(d, int(p.tip))), bases[p.base])
		bases[p.base] -= k
		n += k / 10
	}
	return n
}

// trawlerArrowUses is the arrowtip uses the pack holds (the four arrow pairs, the best XP a use): the feathers and the pearl
// bolts alone are no reason to leave the mule's arrowtips where they are (live 3 Nov 2026 17:30: 125k Fletching an hour on
// pearl bolts and headless arrows while the mule held 170,000 arrowtips).
func trawlerArrowUses(d *fleetrunner.Deps) int {
	heads := int(fleetrunner.PackCountOf(d, int(mastercraftplan.HeadlessID)))
	n := 0
	for _, p := range trawlerTips[:4] {
		k := min(int(fleetrunner.PackCountOf(d, int(p.tip))), heads)
		heads -= k
		n += k / mastercraftplan.ArrowsPerUse
	}
	return n
}

// muleHasArrowtips reports whether the mule's published bank holds any of the four arrowtips.
func (b *masterCrafterBot) muleHasArrowtips(ctx *Context) bool {
	stock := muleBankCount(ctx, b.mule)
	if stock == nil {
		return false
	}
	for _, p := range trawlerTips[:4] {
		if n, _ := stock(p.tipName); n > 0 {
			return true
		}
	}
	return false
}

// trawlerTipAsk is the one ask for every tip the mule's published bank holds, and the bases to tip them: headless arrows past
// what the pack holds, and a bolt for every pearl bolttip.
func (b *masterCrafterBot) trawlerTipAsk(ctx *Context, d *fleetrunner.Deps) []mastercraftplan.Line {
	stock := muleBankCount(ctx, b.mule)
	if stock == nil {
		return nil
	}
	have := func(item string) int {
		n, _ := stock(item)
		return n
	}
	var lines []mastercraftplan.Line
	arrowtips := 0
	for _, p := range trawlerTips[:4] {
		arrowtips += have(p.tipName)
	}
	// TIPPING ONLY AT 100,000 (a user request): the arrows or bolts the mule holds to tip must reach craftArrowThreshold /
	// craftBoltThreshold before the anchor takes any tips aboard (what the pack already holds is tipped whatever the stock).
	arrowsDue, boltsDue := mastercraftplan.TipsDue(b.set, arrowtips, have(mastercraftplan.Headless), have(pearlplan.Tips), have("Bolts"))
	if !arrowsDue && !boltsDue {
		return nil
	}
	if arrowsDue {
		for _, p := range trawlerTips[:4] {
			if n := have(p.tipName); n > 0 {
				lines = append(lines, mastercraftplan.Line{Item: p.tipName, Qty: n})
			}
		}
	}
	if need := arrowtips - int(fleetrunner.PackCountOf(d, int(mastercraftplan.HeadlessID))); arrowsDue && need > 0 && have(mastercraftplan.Headless) > 0 {
		lines = append(lines, mastercraftplan.Line{Item: mastercraftplan.Headless, Qty: min(need, have(mastercraftplan.Headless))})
	}
	if bolts := min(have(pearlplan.Tips), have("Bolts")); boltsDue && bolts > 0 {
		lines = append(lines, mastercraftplan.Line{Item: pearlplan.Tips, Qty: bolts}, mastercraftplan.Line{Item: "Bolts", Qty: bolts})
	}
	if shafts := min(have(mastercraftplan.Feather), have(mastercraftplan.ArrowShaft)); shafts > 0 {
		lines = append(lines, mastercraftplan.Line{Item: mastercraftplan.Feather, Qty: shafts}, mastercraftplan.Line{Item: mastercraftplan.ArrowShaft, Qty: shafts})
	}
	return lines
}

// trawlerTeleRunes are the runes the trawler round's teleports cast from (Ardougne: 2 law, 2 water; Camelot: 1 law, 5 air);
// trawlerLawsMin the laws under which the round asks the mule for trawlerLawsAsk more.
var trawlerTeleRunes = []int32{trawlerLawRune, 555, 556}

const (
	trawlerLawRune int32 = 563
	trawlerLawsMin       = 20
	trawlerLawsAsk       = 200
)

func trawlerTeleRune(id int32) bool {
	for _, r := range trawlerTeleRunes {
		if id == r {
			return true
		}
	}
	return false
}

// trawlerTipItem reports whether id is one of trawlerTips' tips or bases (the trawler bank visit keeps them in the pack).
func trawlerTipItem(id int32) bool {
	for _, p := range trawlerTips {
		if id == p.tip || id == p.base {
			return true
		}
	}
	return false
}

// trawlerTipUsable reports whether a tip or base the pack holds (id) has its other half in the pack too (held counts the pack),
// headless arrows counting as there while the feathers and shafts that make them are. One without goes to the bank, where
// trawlerDrawTips draws the pair back once the bank holds both (a user request: "master keeps arrowtips in the inventory even
// though it has no headless arrows to fletch them": the slots are the claim's).
func trawlerTipUsable(id int32, held func(int32) int) bool {
	have := func(id int32) bool {
		if held(id) > 0 {
			return true
		}
		return id == mastercraftplan.HeadlessID && held(mastercraftplan.FeatherID) > 0 && held(mastercraftplan.ArrowShaftID) > 0
	}
	for _, p := range trawlerTips {
		if (id == p.tip && have(p.base)) || (id == p.base && have(p.tip)) {
			return true
		}
	}
	return false
}

// trawlerDrawTips withdraws every pair of trawlerTips the bank holds both halves of (stacks: two slots a pair).
func trawlerDrawTips(ctx *Context, bank *actions.Bank) {
	if bank == nil {
		return
	}
	items := bank.Items()
	for _, p := range trawlerTips {
		if actions.CountByID(items, p.tip) == 0 || actions.CountByID(items, p.base) == 0 {
			continue
		}
		for _, id := range []int32{p.tip, p.base} {
			if err := bank.WithdrawAll(id); err != nil {
				ctx.logf("trawler: withdrawing %d for tipping: %v", id, err)
			}
		}
	}
}

// trawlerIdle is the anchor's wait: a High Level Alchemy cast (or the plain wait), and through every tick of it
// trawlerTipsPerTick uses of a tip on its base. The engine takes five player actions a tick and a tip use is instant (owner,
// 2 Nov 2026: "is it possible for master to tip more bolts between the alch cycle"); a use the server refuses while the cast's
// delay runs costs nothing, and the loot reads what was actually made.
func (b *masterCrafterBot) trawlerIdle(ctx *Context, travel *fleetrunner.Travel, d *fleetrunner.Deps, wait time.Duration) {
	// The burst goes in the same tick as the cast, ahead of it: High Level Alchemy holds the player four ticks (alchemy.rs2
	// p_delay(3)) and the engine drops every use sent while it runs, so one use a tick landed about once a cast.
	b.trawlerTip(ctx, d, trawlerTipBurst)
	cast := travel != nil && travel.IdleAlch(d)
	if cast {
		wait = travel.IdleAlchWait()
	}
	for end := time.Now().Add(wait); time.Now().Before(end); {
		if !ctx.Delay(min(trawlerTick, time.Until(end))) {
			return
		}
		if !cast {
			b.trawlerTip(ctx, d, trawlerTipsPerTick)
		}
	}
}

// trawlerAnchorFixes lets the anchor fill leaks, mend the net, bail and climb; off, it only alchs and tips aboard. Live 2 Nov
// 2026 the server dropped master (and only master, of 260) four seconds after the flood teleport in three games running, which
// costs the whole catch (the login lands at Port Sarim). Standing still at 23:27 he rode six floods connected, so it is one of
// his actions: this step brings back alchemy and tipping alone (the crew alchs too, and fixes the boat without him).
const trawlerAnchorFixes = false

// trawlerTick is a game tick; trawlerTipsPerTick the tip uses sent in one. One, not four: live 2 Nov 2026 the server dropped
// master's connection twice aboard (20:49 and 21:44, two seconds after the flood teleport) and none of the 268 crew, whose only
// actions are the boat's; the anchor's place at the end of the game is worth far more than the tipping.
const (
	trawlerTick        = 600 * time.Millisecond
	trawlerTipsPerTick = 1
	// trawlerTipBurst is the tip uses sent just ahead of each cast, in its tick (four uses and the cast: the engine's five
	// actions a tick). The boat is held flooded since 3 Nov 2026, so the flood teleports that dropped master mid-burst are gone.
	trawlerTipBurst = 4
)

// trawlerTip uses the first pair of trawlerTips the pack holds both halves of trawlerTipsPerTick times, and reports what the last uses made on
// the loot tracker (the product's pack count, read before the next use).
func (b *masterCrafterBot) trawlerTip(ctx *Context, d *fleetrunner.Deps, uses int) {
	if ctx.Session == nil {
		return
	}
	for i, p := range trawlerTips {
		n := int(fleetrunner.PackCountOf(d, int(p.product)))
		if last, ok := b.tipSeen[i]; ok && n > last {
			lootMade(ctx, loot.Fletching(nil, int(p.tip), p.tipName), n-last, loot.Product(int(p.product), p.name, n-last))
		}
		if b.tipSeen == nil {
			b.tipSeen = map[int]int{}
		}
		b.tipSeen[i] = n
	}
	eq := actions.NewEquipment(ctx.Session, ctx.Components)
	for _, p := range trawlerTips {
		if fleetrunner.PackCountOf(d, int(p.tip)) > 0 && fleetrunner.PackCountOf(d, int(p.base)) > 0 {
			_, _ = eq.UseOnHeldStack(p.tip, p.base, uses)
			return
		}
	}
}

// trawlerAboard plays the game to its end: the crew's moves, alchemy when there is nothing to do. It never leaves. ok is true
// when the account is off the boat again (the game over), false on a stop or a game that did not end.
func (b *masterCrafterBot) trawlerAboard(ctx *Context, d *fleetrunner.Deps, travel *fleetrunner.Travel) (string, bool) {
	deadline := time.Now().Add(trawlerGameMax)
	leaks, mends := 0, 0
	said := time.Now()
	for time.Now().Before(deadline) {
		if ctx.Stopped() {
			return "stopped", false
		}
		if !trawlerplan.OnBoat(trawlerrunner.Here(d)) {
			b.logf(ctx, "trawler: the game is over (%d leak(s) filled, %d net(s) mended): back at %s", leaks, mends, fleetrunner.HereIs(d))
			return "", true
		}
		masterClearDead(ctx)
		// A rip opens "The net has ripped!" for everyone aboard (trawler_net.rs2): closed, or it holds every click.
		if dlg := actions.NewChatDialog(ctx.Session, ctx.Components); dlg.CanContinue() {
			_ = dlg.Continue()
		}
		b.trawlerCall(ctx, d, mulebus.TrawlerAboard)
		view := trawlerrunner.View(d)
		view.NetFirst = true // a user request: "master needs to climb the ladder and fix the net"
		act := trawlerplan.Aboard(view)
		if !trawlerAnchorFixes {
			act = trawlerplan.Action{Kind: trawlerplan.ActIdle}
		}
		switch act.Kind {
		case trawlerplan.ActFillLeak:
			if trawlerrunner.FillLeak(d, act.Target) {
				leaks++
			}
		case trawlerplan.ActFixNet:
			if trawlerrunner.FixNet(d, act.Target) {
				mends++
			}
		case trawlerplan.ActBail:
			trawlerrunner.Bail(d)
		case trawlerplan.ActEmpty:
			trawlerrunner.EmptyBucket(d)
		case trawlerplan.ActClimbUp:
			trawlerrunner.Climb(d, true)
		case trawlerplan.ActClimbDown:
			trawlerrunner.Climb(d, false)
		default:
			b.trawlerIdle(ctx, travel, d, 1500*time.Millisecond)
		}
		if time.Since(said) >= 2*time.Minute {
			said = time.Now()
			b.logf(ctx, "trawler: aboard: %s left, catch %q; %s", trawlerrunner.TimeText(d), trawlerrunner.Catch(d), b.trawlerRate(ctx))
		}
	}
	return fmt.Sprintf("still aboard after %s", trawlerGameMax), false
}
