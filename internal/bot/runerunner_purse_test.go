package bot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/runerunnerplan"
)

// The tests below price the live ring of the one RuneRunner, spare104 (13 October 2026): Magic 66,
// rotationPhase 2, three worlds, Zaff's staffs on. Boards are per world and process-wide, so each
// test works on worlds of its own.

// purseRig is a rotation runner at the mule's post with its round planned. base is the first of its
// three worlds, which is also the mule's.
func purseRig(t *testing.T, name string, base int) *rotRig {
	t.Helper()
	w := float64(base)
	r := newRotRig(t, name, base, 2809, 3441, map[string]interface{}{
		"worlds": []interface{}{w, w + 1, w + 2}, "rotationPhase": 2.0, "zaffStaffs": 2.0, "marketBuy": false,
	})
	r.setMagic(66)
	r.b.newRound(r.ctx, runerunnerplan.AreaOf(fleetrunner.Here(r.b.deps(r.ctx))), false)
	return r
}

// setMagic puts the account's Magic level where the runner reads it (noteMagic).
func (r *rotRig) setMagic(level int) {
	mindHoldStat(r.sim.State(), fleetplan.MagicStat, levels.XPAtLevel(level), level)
	r.b.noteMagic(r.ctx)
}

// afterDraw is the ring index the draw station's round need is read from.
func (r *rotRig) afterDraw(t *testing.T) int {
	t.Helper()
	i := runerunnerplan.Index(r.b.st.Ring, fmt.Sprintf("draw@%d", r.ctx.World()))
	if i < 0 {
		t.Fatalf("no draw station in %s", runerunnerplan.PlanString(r.b.st.Ring, 0))
	}
	return i + 1
}

// seedBoards records every shelf of every shop on the rig's worlds as emptied `ago` ago.
func seedBoards(r *rotRig, ago time.Duration) {
	at := time.Now().Add(-ago)
	for _, w := range r.b.set.Worlds {
		board := fleetrunner.ShopsFor(w)
		for _, sh := range []fleetplan.RuneShop{runerunnerplan.Betty, fleetplan.Aubury, fleetplan.Lundail, fleetplan.WizardsGuild} {
			for _, l := range sh.Lines {
				board.Observe(sh, l.Rune, 0, at, "purseseed")
			}
		}
	}
}

// Nobody has looked at the shelves (a fleet restart empties the board): the round is every shelf
// full -- 5,490,585 pack-paid, 384,603 for Lundail, 9,090 for Zaff, the investigation's figures -- and
// the margin, which never lifts a pair past a full shelf, adds nothing.
func TestRoundNeedAtFullShelves(t *testing.T) {
	r := purseRig(t, "purse1", 61)
	n := r.b.roundNeed(r.ctx, r.afterDraw(t))
	if n.Pack != 5490585 || n.Lundail != 384603 || n.Staffs != 9090 || n.Margin() != 0 || n.Fares != r.b.set.FareCoins {
		t.Fatalf("full-shelf round: %s", n)
	}
	if n.Stations != 15 {
		t.Errorf("stations %d in %s", n.Stations, runerunnerplan.PlanString(r.b.st.Ring, 0))
	}
	// A draw from the middle of the round counts what is left of it only.
	mid := runerunnerplan.Index(r.b.st.Ring, "Aubury@62")
	if m := r.b.roundNeed(r.ctx, mid); m.Pack >= n.Pack || m.Pack <= 0 {
		t.Errorf("from Aubury@62: pack %d of %d", m.Pack, n.Pack)
	}
}

// The shelves are valued when the round REACHES them, from the freshest sighting any account made,
// holds ignored; and a pair that cannot be visited drops out. LIVE 12 October the draw-instant
// estimate was 1.8 to 2.2 times too low in the rounds that had the money.
func TestRoundNeedProjectsToArrivalAndIgnoresHolds(t *testing.T) {
	r := purseRig(t, "purse2", 64)
	seedBoards(r, 38*time.Minute)
	from := r.afterDraw(t)
	n := r.b.roundNeed(r.ctx, from)
	full := 5490585
	if n.Pack <= 0 || n.Pack >= full {
		t.Fatalf("steady-state pack %d against a full round of %d", n.Pack, full)
	}
	if n.PackMargin <= 0 || n.PackMargin > n.Pack/4 {
		t.Errorf("margin %d on %d", n.PackMargin, n.Pack)
	}

	// The draw-instant value of the same shelves is lower: that is the under-estimate.
	atDraw := 0
	now := time.Now()
	for _, s := range r.b.st.Ring[from:] {
		if s.Kind != runerunnerplan.KindBuy {
			continue
		}
		site, _ := runerunnerplan.Site(s.Site)
		atDraw += runerunnerplan.EstCost([]runerunnerplan.StationEstimate{{Shop: site.Shop, Stock: r.b.estimateAt(site, s.World, now)}}, r.b.set.RuneList, r.b.set.CapTable)
	}
	if atDraw <= 0 || n.Pack <= atDraw {
		t.Errorf("projected pack %d is not above the draw-instant %d", n.Pack, atDraw)
	}

	// Another account's hold on a shelf lowers what this runner may PLAN on (estimate) but not the
	// coins its round may need.
	betty, _ := runerunnerplan.Site(runerunnerplan.SiteBetty)
	board := fleetrunner.ShopsFor(65)
	defer board.Release(betty.Shop, "purseother")
	before := r.b.estimate(r.ctx, betty, 65, now)["Mind rune"]
	board.Reserve(betty.Shop, "Mind rune", "purseother", 200, now, time.Hour, r.b.set.Tick())
	if after := r.b.estimate(r.ctx, betty, 65, now)["Mind rune"]; after >= before {
		t.Fatalf("the hold did not lower the planning estimate: %d -> %d", before, after)
	}
	if again := r.b.roundNeed(r.ctx, from); again != n {
		t.Errorf("a hold changed the round's coins:\n%s\n%s", n, again)
	}

	// A pair struck off the ring, Lundail stopped, and a Magic level that no longer opens the guild.
	r.b.st.PairOffUntil["Aubury@65"] = now.Add(time.Hour)
	noAubury := r.b.roundNeed(r.ctx, from)
	if noAubury.Pack >= n.Pack || noAubury.Lundail != n.Lundail {
		t.Errorf("Aubury@65 off: pack %d -> %d, lundail %d -> %d", n.Pack, noAubury.Pack, n.Lundail, noAubury.Lundail)
	}
	r.b.lundailOff = "no blade"
	if noLundail := r.b.roundNeed(r.ctx, from); noLundail.Lundail != 0 || noLundail.LundailMargin != 0 || noLundail.Pack != noAubury.Pack {
		t.Errorf("Lundail off: %s", noLundail)
	}
	r.setMagic(50)
	if noGuild := r.b.roundNeed(r.ctx, from); noGuild.Pack >= noAubury.Pack {
		t.Errorf("Magic 50: the guild's shelves are still counted: %s", noGuild)
	}
	// Zaff's staffs: three stations, two of them left once one pair is struck off.
	r.b.st.PairOffUntil["Zaff@65"] = now.Add(time.Hour)
	if n2 := r.b.roundNeed(r.ctx, from); n2.Staffs != 2*runerunnerplan.StaffEstCost(2, r.b.set.ZaffStaffCap) {
		t.Errorf("staffs %d", n2.Staffs)
	}
}

// The seam replaces the reading in a test; the market station keeps its hands off the float the shops
// still need: the bank less the reserve less (the round's estimate less the pack's coins beyond the fare).
func TestMarketBudgetLeavesTheShopsTheirFloat(t *testing.T) {
	r := newRotRig(t, "purse3", 41, 2809, 3441, map[string]interface{}{"marketCoins": 1000000.0})
	defer releaseBoards("purse3")
	r.b.roundNeedSeam = func(int) runerunnerplan.RoundNeed {
		return runerunnerplan.RoundNeed{Pack: 700000, PackMargin: 140000, Lundail: 50000, Fares: 300}
	}
	// Est 890,000; the pack carries 300,000 beyond its fare: the shops still need 590,000 from the bank.
	pack := 300300
	r.b.st.BankCoins = 2000000
	if got := r.b.marketBudget(r.ctx, pack); got != 1000000 {
		t.Errorf("a rich bank: %d, want marketCoins", got)
	}
	r.b.st.BankCoins = 1000000 // 990,000 spare, 590,000 of it the shops'
	if got := r.b.marketBudget(r.ctx, pack); got != 400000 {
		t.Errorf("bank 1,000,000: %d, want 400000", got)
	}
	r.b.st.BankCoins = 600000
	if got := r.b.marketBudget(r.ctx, pack); got != 0 {
		t.Errorf("bank 600,000: %d, want 0", got)
	}
	// The pack carrying the whole estimate leaves the bank free.
	if got := r.b.marketBudget(r.ctx, 900000); got != 590000 {
		t.Errorf("a pack that carries the round: %d, want the bank's 590000 spare", got)
	}
	r.b.st.BankCoins = 5000
	if got := r.b.marketBudget(r.ctx, pack); got != 0 {
		t.Errorf("under the reserve: %d", got)
	}
}

// The purse line says so when the fleet's coins fell short of the round's target after the ask, and
// stays quiet when they did not or when buyAll has no target to hold.
func TestPurseShortLine(t *testing.T) {
	r := newRotRig(t, "purse4", 41, 2809, 3441, nil)
	defer releaseBoards("purse4")
	rn := runerunnerplan.RoundNeed{Pack: 400000, PackMargin: 80000, Lundail: 100000, LundailMargin: 20000, Fares: 300}
	target := rn.Total() + r.b.set.CoinReserve
	r.b.purseShort(r.ctx, rn, target, 0)
	if strings.Contains(r.log(), "purse SHORT") {
		t.Fatalf("a purse at its target is not short\n%s", r.log())
	}
	r.b.purseShort(r.ctx, rn, 384541, 384541)
	want := fmt.Sprintf("purse SHORT by %d: 384541 coins held against a target of %d (the mule just gave 384541)", target-384541, target)
	if !strings.Contains(r.log(), want) {
		t.Fatalf("no short line, want %q\n%s", want, r.log())
	}
	off := newRotRig(t, "purse5", 41, 2809, 3441, map[string]interface{}{"buyAll": false})
	defer releaseBoards("purse5")
	off.b.purseShort(off.ctx, rn, 1, 0)
	if strings.Contains(off.log(), "purse SHORT") {
		t.Errorf("buyAll off has no target\n%s", off.log())
	}
}

// A visit that leaves a Law rune for lack of coins counts it, at what the rest of the shelf costs, and
// the round's COINS SHORT line adds the stations that skipped for coins.
func TestCoinsShortIsCountedAcrossTheRound(t *testing.T) {
	r := purseRig(t, "purse6", 67)
	site, _ := runerunnerplan.Site(runerunnerplan.SiteWizardsGuild)
	sh := site.Shop
	seen := map[string]int{"Mind rune": 1000, "Law rune": 34}
	spendable := sh.CostToBuy("Mind rune", 1000, 1000) + 10
	plan := runerunnerplan.PlanVisit(sh, seen, spendable, r.b.set.RuneList, r.b.set.CapTable, nil)
	v := fleetrunner.ShopVisit{Seen: seen, Bought: map[string]int{"Mind rune": 1000}, Spent: plan.Coins}
	res := r.b.visitResult(r.ctx, site, 69, "WizardsGuild@69", v, plan, spendable, nil)
	if res.ShortRunes != 34 || res.ShortCoins != sh.CostToBuy("Law rune", 34, 34) || res.Outcome != runerunnerplan.OutcomeBought {
		t.Fatalf("visit result %+v", res)
	}
	if !strings.Contains(r.log(), "left 34 Law rune shop=WizardsGuild world=69: coins: ") {
		t.Errorf("no left line\n%s", r.log())
	}

	// The round: that visit and a station that skipped for coins.
	r.b.st.Done = map[string]runerunnerplan.StationResult{
		"WizardsGuild@69": res,
		"Betty@67":        skippedForCoins("cannot afford: 900 coins buy only 12 runes here"),
		"Aubury@68":       skipped("worth 3"),
	}
	r.b.st.BankCoins = 10500
	line := r.b.roundShortLine(300)
	for _, want := range []string{"COINS SHORT", "stations_skipped_for_coins=Betty@67", "runes_left_for_coins=34", fmt.Sprintf("(about %d coins", res.ShortCoins), "bank=10500", "pack=300"} {
		if !strings.Contains(line, want) {
			t.Errorf("round line %q lacks %q", line, want)
		}
	}
	// A round that bought everything says nothing.
	r.b.st.Done = map[string]runerunnerplan.StationResult{"Aubury@68": skipped("worth 3")}
	if line := r.b.roundShortLine(0); line != "" {
		t.Errorf("a round with no coin shortfall said %q", line)
	}
	// finish records a coins skip as one.
	r.b.st.StartRound(r.b.st.Ring, 0, time.Now())
	r.b.finish(r.ctx, skippedForCoins("no coins"))
	if stations, _, _ := r.b.st.RoundShort(); len(stations) != 1 {
		t.Errorf("finish lost the coins skip: %v", stations)
	}
}

// firstStation is the ring index of the first station of a kind.
func firstStation(t *testing.T, ring []runerunnerplan.Station, kind runerunnerplan.Kind) int {
	t.Helper()
	for i, s := range ring {
		if s.Kind == kind {
			return i
		}
	}
	t.Fatalf("no %v station in %s", kind, runerunnerplan.PlanString(ring, 0))
	return -1
}

// A short purse's Lundail share is worked out at the round's draw and held through the shop-bank
// top-ups that follow it (State.LundailKeep): a 610,000-coin bank against a full-shelf round kept
// 39,216, then about 2,500, then under 200 when each top-up worked the share out afresh, and Lundail met about 190 coins
// at Gundai. Draw, top-up, top-up, and the bank still holds the keep; once the Lundail stations have
// passed the keep is free.
func TestLundailKeepHoldsThroughTheBankDraws(t *testing.T) {
	r := purseRig(t, "purse7", 70)
	defer releaseBoards("purse7")
	ring := r.b.st.Ring
	reserve := r.b.set.CoinReserve
	bank := 610000

	rn := r.b.roundNeed(r.ctx, r.afterDraw(t))
	d, keep := r.b.bankDraw(rn, true, bank, 0, rn.PackNeed())
	if keep < 39000 || keep > 40000 || r.b.st.LundailKeep != keep {
		t.Fatalf("the draw's keep %d (stored %d), want about 39,216 stored", keep, r.b.st.LundailKeep)
	}
	bank -= d.Amount
	if bank != reserve+keep {
		t.Fatalf("after the draw the bank holds %d, want reserve %d + keep %d", bank, reserve, keep)
	}

	// Two shop-bank top-ups, read from the first pack-paid shop station.
	shop := firstStation(t, ring, runerunnerplan.KindBuy)
	for i := 0; i < 2; i++ {
		top := r.b.roundNeed(r.ctx, shop)
		d, k := r.b.bankDraw(top, false, bank, 0, min(top.PackNeed(), r.b.set.TripCoins))
		if k != keep {
			t.Errorf("top-up %d held %d, want the stored %d", i+1, k, keep)
		}
		if !d.NoCoins || d.Amount != 0 {
			t.Errorf("top-up %d found only the keep above the reserve and drew %+v", i+1, d)
		}
		bank -= d.Amount
	}
	if bank != reserve+keep {
		t.Fatalf("the bank holds %d after two top-ups, want %d", bank, reserve+keep)
	}

	// The Lundail stations have passed: nothing is owed, so the last top-up may take the keep.
	lastLundail := -1
	for i, s := range ring {
		if s.Kind == runerunnerplan.KindLundail {
			lastLundail = i
		}
	}
	after := r.b.roundNeed(r.ctx, lastLundail+1)
	if after.Lundail != 0 {
		t.Fatalf("Lundail is still ahead of station %d: %s", lastLundail+1, after)
	}
	d, k := r.b.bankDraw(after, false, bank, 0, min(after.PackNeed(), r.b.set.TripCoins))
	if k != 0 || d.NoCoins || d.Amount != keep {
		t.Errorf("after Lundail: held %d, drew %+v, want the keep %d drawn", k, d, keep)
	}
}

// The visit's reserve follows the bank: a bank that holds the stations ahead's coins refills the pack
// at the next top-up, so nothing is held back for them (the Law, Nature and Death left on the Wizards'
// Guild shelf for coins the bank held); a bank never read, or one that is short, keeps the hold.
func TestVisitReserveFollowsTheBank(t *testing.T) {
	r := purseRig(t, "purse8", 73)
	defer releaseBoards("purse8")
	guild := firstStation(t, r.b.st.Ring, runerunnerplan.KindBuy)
	for i, s := range r.b.st.Ring {
		if s.Kind == runerunnerplan.KindBuy && s.Site == runerunnerplan.SiteWizardsGuild {
			guild = i
			break
		}
	}
	r.b.st.Cursor = guild
	ahead := r.b.aheadEstimates(r.ctx, guild+1)
	if len(ahead) < 2 {
		t.Fatalf("setup: %d pack-paid stations after the first guild visit in %s", len(ahead), runerunnerplan.PlanString(r.b.st.Ring, 0))
	}
	raw := runerunnerplan.ReserveAhead(ahead, r.b.set.RuneList, r.b.set.CapTable)
	if raw["Law rune"] <= 0 {
		t.Fatalf("setup: no hold for Law runes: %v", raw)
	}

	// Never read: the hold stands.
	r.b.st.BankReadAt, r.b.st.BankCoins = time.Time{}, 50000000
	if got := r.b.visitReserve(r.ctx, ahead); got["Law rune"] != raw["Law rune"] {
		t.Errorf("an unread bank changed the hold: %d of %d", got["Law rune"], raw["Law rune"])
	}
	// A bank holding the whole round: no hold at all.
	r.b.st.BankReadAt, r.b.st.BankCoins = time.Now(), 50000000
	if got := r.b.visitReserve(r.ctx, ahead); len(got) != 0 {
		t.Errorf("a bank with 50M still held %v back", got)
	}
	// A bank at its reserve funds nothing.
	r.b.st.BankCoins = r.b.set.CoinReserve
	if got := r.b.visitReserve(r.ctx, ahead); got["Law rune"] != raw["Law rune"] {
		t.Errorf("a bank at its reserve changed the hold: %d of %d", got["Law rune"], raw["Law rune"])
	}
	// Half the shops' need in the bank: about half the hold.
	need := r.b.roundNeed(r.ctx, guild+1).PackNeed()
	r.b.st.BankCoins = r.b.set.CoinReserve + need/2
	got := r.b.visitReserve(r.ctx, ahead)["Law rune"]
	if want := raw["Law rune"] / 2; got < want*9/10 || got > want*11/10 {
		t.Errorf("a half-funded bank: hold %d, want about %d of %d", got, want, raw["Law rune"])
	}

	// Lundail is still ahead of a Betty visit: its keep is not the shops' coins, so a bank holding only
	// the reserve and the keep funds nothing there.
	betty := firstStation(t, r.b.st.Ring, runerunnerplan.KindBuy)
	r.b.st.Cursor = betty
	bettyAhead := r.b.aheadEstimates(r.ctx, betty+1)
	bettyRaw := runerunnerplan.ReserveAhead(bettyAhead, r.b.set.RuneList, r.b.set.CapTable)
	r.b.st.LundailKeep = 300000
	r.b.st.BankCoins = r.b.set.CoinReserve + 300000
	if got := r.b.visitReserve(r.ctx, bettyAhead); got["Law rune"] != bettyRaw["Law rune"] || bettyRaw["Law rune"] <= 0 {
		t.Errorf("at Betty a bank holding only the reserve and Lundail's keep changed the hold: %d of %d", got["Law rune"], bettyRaw["Law rune"])
	}
}

// A coin ask that came back short says why: a hand-out limit with coins still in the mule's bank is a
// roster setting to raise (the rest is asked next round), an empty-handed decline is the allow list.
func TestTopUpDeclineNotes(t *testing.T) {
	r := newRotRig(t, "purse9", 41, 2809, 3441, nil)
	defer releaseBoards("purse9")
	limit := runerunner.TopUpResult{Got: 4000000, Orders: 9, Declined: true, Reason: "already handed you one recently"}
	r.b.noteTopUpDecline(r.ctx, 5884578, limit)
	if !strings.Contains(r.log(), "MULE LIMIT? mule stopped a coin ask at 4000000 of 5884578 after 9 orders") || strings.Contains(r.log(), "ALLOWLIST?") {
		t.Fatalf("a limit decline\n%s", r.log())
	}
	// A first order that is already over the limit is a limit too, not the allow list.
	r.b.noteTopUpDecline(r.ctx, 500000, runerunner.TopUpResult{Orders: 1, Declined: true, Reason: "hand-out limit for that item is spent"})
	if strings.Contains(r.log(), "ALLOWLIST?") {
		t.Errorf("a window limit read as the allow list\n%s", r.log())
	}
	r.b.noteTopUpDecline(r.ctx, 500000, runerunner.TopUpResult{Orders: 1, Declined: true, Reason: "none in the bank"})
	if !strings.Contains(r.log(), "ALLOWLIST? mule declined a coin top-up (none in the bank)") {
		t.Errorf("an empty-handed decline\n%s", r.log())
	}
	before := strings.Count(r.log(), "\n")
	r.b.noteTopUpDecline(r.ctx, 500000, runerunner.TopUpResult{Got: 500000, Orders: 1})
	r.b.noteTopUpDecline(r.ctx, 500000, runerunner.TopUpResult{Got: 120000, Orders: 2, Declined: true, Reason: "none in the bank"})
	if strings.Count(r.log(), "\n") != before {
		t.Errorf("a filled ask or a mule that gave what it had was reported\n%s", r.log())
	}
	if got := muleBankLabel(0, false); got != "unknown" {
		t.Errorf("unknown label %q", got)
	}
	if got := muleBankLabel(636748, true); got != "636748" {
		t.Errorf("known label %q", got)
	}
}
