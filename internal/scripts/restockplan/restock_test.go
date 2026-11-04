package restockplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/orderplan"
)

// The reserve is argued from two numbers this repo already committed to. If either of them is
// ever raised, this reserve must not silently fall below it — a donor stripped to less than its
// own death-recovery float is the exact failure fisherplan.DeathFloatCoins was written after.
func TestReserveCoversTheRepoSExistingFloats(t *testing.T) {
	if DonorReserveCoins < fisherplan.DeathFloatCoins {
		t.Fatalf("DonorReserveCoins %d is below fisherplan.DeathFloatCoins %d: a donation could strip an account of its own death-recovery money",
			DonorReserveCoins, fisherplan.DeathFloatCoins)
	}
	if DonorReserveCoins < donorplan.DefaultTravelReserve {
		t.Fatalf("DonorReserveCoins %d is below donorplan.DefaultTravelReserve %d: a donation could leave an account unable to pay a fare home",
			DonorReserveCoins, donorplan.DefaultTravelReserve)
	}
	if MinDonationCoins <= DonorReserveCoins {
		t.Fatalf("MinDonationCoins %d is not above DonorReserveCoins %d: the minimum worth walking for must be larger than the float that never moves",
			MinDonationCoins, DonorReserveCoins)
	}
}

func TestSpareCoins(t *testing.T) {
	cases := []struct {
		name                      string
		banked, pocket, earmarked int
		want                      int
		ok                        bool
	}{
		{"nothing at all", 0, 0, 0, 0, false},
		{"exactly the reserve is not spare", DonorReserveCoins, 0, 0, 0, false},
		{"one over the reserve is under the minimum", DonorReserveCoins + 1, 0, 0, 0, false},
		{"reserve plus the minimum, to the coin", DonorReserveCoins + MinDonationCoins, 0, 0, MinDonationCoins, true},
		{"a real fisher balance", 60000, 400, 0, 60400 - DonorReserveCoins, true},
		{"pocket and bank count together", 1000, 8000, 0, 9000 - DonorReserveCoins, true},
		{"earmarked money is not spare", 200000, 0, 200000, 0, false},
		{"the axe-money loop: 150k held, 150k earmarked", 150000, 0, 150000, 0, false},
		{"negative readings pledge nothing", -5, -5, 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok, why := SpareCoins(c.banked, c.pocket, c.earmarked)
			if ok != c.ok || got != c.want {
				t.Fatalf("SpareCoins(%d,%d,%d) = %d,%v (%s); want %d,%v", c.banked, c.pocket, c.earmarked, got, ok, why, c.want, c.ok)
			}
			if !ok && why == "" {
				t.Fatal("a refusal must say why: the donor logs it and an operator reads it")
			}
		})
	}
}

// The negative control the live proof also runs: an account at or under the reserve must never
// be asked for anything, at any appeal size.
func TestAnAccountAtTheReserveIsNeverDrained(t *testing.T) {
	now := time.Now()
	for _, banked := range []int{0, 1, DonorReserveCoins - 1, DonorReserveCoins} {
		qty, ok, why := DecideAnswer(now, Appeal{Item: CoinsItem, Qty: 15_000_000}, DonorState{Banked: banked})
		if ok || qty != 0 {
			t.Fatalf("banked %d: pledged %d (%v) — an account at or below the reserve must keep every coin", banked, qty, ok)
		}
		if why == "" {
			t.Fatalf("banked %d: refused with no reason", banked)
		}
	}
}

func TestDecideAnswer(t *testing.T) {
	now := time.Now()
	rich := DonorState{Banked: 500_000}

	if _, ok, why := DecideAnswer(now, Appeal{Item: "Mithril axe", Qty: 5}, rich); ok {
		t.Fatalf("answered an appeal for an item with no spare rule: %s", why)
	}
	if _, ok, _ := DecideAnswer(now, Appeal{Item: CoinsItem, Qty: 0}, rich); ok {
		t.Fatal("answered an appeal for nothing")
	}
	busy := rich
	busy.Busy = true
	if _, ok, _ := DecideAnswer(now, Appeal{Item: CoinsItem, Qty: 1000000}, busy); ok {
		t.Fatal("interrupted a busy script for a donation")
	}
	cooling := rich
	cooling.LastDonated = now.Add(-AnswerCooldown / 2)
	if _, ok, _ := DecideAnswer(now, Appeal{Item: CoinsItem, Qty: 1000000}, cooling); ok {
		t.Fatal("answered again inside the cooldown")
	}
	cooled := rich
	cooled.LastDonated = now.Add(-AnswerCooldown - time.Second)
	if qty, ok, why := DecideAnswer(now, Appeal{Item: CoinsItem, Qty: 1000000}, cooled); !ok || qty != 500_000-DonorReserveCoins {
		t.Fatalf("past the cooldown: %d,%v (%s); want %d", qty, ok, why, 500_000-DonorReserveCoins)
	}
	// Never pledge more than was asked for.
	if qty, ok, _ := DecideAnswer(now, Appeal{Item: CoinsItem, Qty: 10_000}, rich); !ok || qty != 10_000 {
		t.Fatalf("pledged %d against an appeal for 10000; want 10000", qty)
	}
	// Case folding on the item name, since the mule's own catalogue spelling is what arrives.
	if _, ok, why := DecideAnswer(now, Appeal{Item: "coins", Qty: 1000}, rich); !ok {
		t.Fatalf("a lower-case item name was refused: %s", why)
	}
}

func TestFloorAndTargetTrackTheConfiguredWindow(t *testing.T) {
	if f, tg := FloorAndTarget(0); f != 0 || tg != 0 {
		t.Fatalf("an unconfigured window must fail closed, got floor %d target %d", f, tg)
	}
	// The live fleet's own configuration on 2026-09-21: 6,000,000 coins per 30-minute window.
	f, tg := FloorAndTarget(6_000_000)
	if f != 12_000_000 || tg != 24_000_000 {
		t.Fatalf("live window 6M: floor %d target %d; want 12M/24M", f, tg)
	}
	if tg <= f {
		t.Fatal("the target must be above the floor or the appeal asks for nothing")
	}
	// orderplan's own conservative default must also produce something sane.
	f, tg = FloorAndTarget(orderplan.CoinsPerWindow)
	if f != 2*orderplan.CoinsPerWindow || tg != 4*orderplan.CoinsPerWindow {
		t.Fatalf("default window %d: floor %d target %d", orderplan.CoinsPerWindow, f, tg)
	}
}

func TestWindowForReadsTheSameCeilingOrderplanEnforces(t *testing.T) {
	pol := orderplan.DefaultPolicy([]string{"someone"})
	if got := WindowFor(pol, CoinsItem); got != orderplan.CoinsPerWindow {
		t.Fatalf("WindowFor(Coins) = %d, want orderplan.CoinsPerWindow %d", got, orderplan.CoinsPerWindow)
	}
	// An item with no WindowMax of its own falls back to the policy-wide cap, exactly as
	// orderplan.Prescreen does.
	if got := WindowFor(pol, "Harpoon"); got != pol.MaxUnitsPerItem {
		t.Fatalf("WindowFor(Harpoon) = %d, want MaxUnitsPerItem %d", got, pol.MaxUnitsPerItem)
	}
	if got := WindowFor(pol, "Dragon axe"); got != 0 {
		t.Fatalf("WindowFor(an item not in the catalogue) = %d, want 0", got)
	}
	// And the live configuration path: a mule whose coin ceilings were raised must derive its
	// floor from the RAISED number, which is the whole reason WindowFor reads the policy.
	live := orderplan.Policy{Catalog: orderplan.WithCoinLimits(orderplan.DefaultCatalog(), 170_000, 6_000_000), MaxUnitsPerItem: 10}
	if got := WindowFor(live, CoinsItem); got != 6_000_000 {
		t.Fatalf("WindowFor on a live-configured mule = %d, want 6000000", got)
	}
}

func TestDecideAppeal(t *testing.T) {
	now := time.Now()
	base := Need{Item: CoinsItem, Held: 9_000_000, Floor: 12_000_000, Target: 24_000_000}

	// The live situation on 2026-09-21: 9M held against a 12M floor.
	a, ok, why := DecideAppeal(now, base)
	if !ok {
		t.Fatalf("a mule at 9M against a 12M floor said nothing: %s", why)
	}
	if a.Item != CoinsItem || a.Qty != 15_000_000 {
		t.Fatalf("appeal = %+v; want 15,000,000 Coins", a)
	}

	// Above the floor, silence.
	full := base
	full.Held = 12_000_000
	if _, ok, why := DecideAppeal(now, full); ok {
		t.Fatalf("appealed at the floor exactly: %s", why)
	}

	// Money already walking towards it counts as held: no double-asking.
	promised := base
	promised.Promised = 4_000_000
	if _, ok, _ := DecideAppeal(now, promised); ok {
		t.Fatal("appealed again while 4M was already granted and in flight")
	}
	promised.Promised = 1_000_000
	a, ok, _ = DecideAppeal(now, promised)
	if !ok || a.Qty != 24_000_000-10_000_000 {
		t.Fatalf("with 1M promised: %+v ok=%v; want a want of 14,000,000", a, ok)
	}

	// Rate limited.
	recent := base
	recent.LastAppeal = now.Add(-AppealInterval / 2)
	if _, ok, _ := DecideAppeal(now, recent); ok {
		t.Fatal("re-broadcast inside AppealInterval")
	}
	recent.LastAppeal = now.Add(-AppealInterval - time.Second)
	if _, ok, why := DecideAppeal(now, recent); !ok {
		t.Fatalf("did not re-broadcast after the interval: %s", why)
	}

	// Fails closed on a configuration that cannot be acted on.
	for _, bad := range []Need{
		{Item: "", Held: 0, Floor: 10, Target: 20},
		{Item: CoinsItem, Floor: 0, Target: 0},
		{Item: CoinsItem, Floor: 100, Target: 100},
		{Item: CoinsItem, Floor: 100, Target: 50},
	} {
		if _, ok, _ := DecideAppeal(now, bad); ok {
			t.Fatalf("appealed on an unusable configuration %+v", bad)
		}
	}
}

func TestBoardGrantsTheRichestFirstAndBoundsTheCrowd(t *testing.T) {
	now := time.Now()
	b := NewBoard(3)
	// Ninety donors answer, which is the stampede this exists to stop.
	for i := 0; i < 90; i++ {
		b.Pledge(now, nameOf(i), 1000*(i+1))
	}
	if got := b.Waiting(now); got != 90 {
		t.Fatalf("Waiting = %d, want 90", got)
	}
	grants := b.NextGrants(now, 0)
	if len(grants) != 3 {
		t.Fatalf("granted %d donors; a board of 3 must never authorise more", len(grants))
	}
	// Richest first: donors 89, 88, 87 (90000, 89000, 88000).
	want := []int{90000, 89000, 88000}
	for i, g := range grants {
		if g.Qty != want[i] {
			t.Fatalf("grant %d carried %d; want %d — the richest pledge must go first", i, g.Qty, want[i])
		}
	}
	if got := b.Outstanding(now); got != 90000+89000+88000 {
		t.Fatalf("Outstanding = %d", got)
	}
	// Full: no more grants until one comes back.
	if extra := b.NextGrants(now, 0); len(extra) != 0 {
		t.Fatalf("granted %d more with every slot full", len(extra))
	}
	if !b.Granted(now, grants[0].To) {
		t.Fatal("a granted donor does not read as granted")
	}
	if !b.Arrived(now, grants[0].To) {
		t.Fatal("Arrived did not find the grant it just made")
	}
	if b.Granted(now, grants[0].To) {
		t.Fatal("a returned donor still reads as granted")
	}
	if got := b.InFlight(now); got != 2 {
		t.Fatalf("InFlight = %d after one arrival, want 2", got)
	}
	if next := b.NextGrants(now, 0); len(next) != 1 {
		t.Fatalf("a freed slot granted %d donors, want 1", len(next))
	}
}

func TestBoardStopsGrantingOnceTheWantIsCovered(t *testing.T) {
	now := time.Now()
	b := NewBoard(3)
	b.Pledge(now, "rich", 60000)
	b.Pledge(now, "middling", 20000)
	b.Pledge(now, "small", 6000)
	grants := b.NextGrants(now, 50000)
	if len(grants) != 1 || grants[0].To != "rich" {
		t.Fatalf("grants = %+v; one donor covers the 50000 want, so only one should walk", grants)
	}
	if got := b.Waiting(now); got != 2 {
		t.Fatalf("Waiting = %d after one grant, want the other two still queued", got)
	}
}

func TestBoardExpiry(t *testing.T) {
	start := time.Now()
	b := NewBoard(1)
	b.Pledge(start, "alice", 10000)
	// A pledge goes stale and is not granted.
	stale := start.Add(PledgeTTL + time.Second)
	if got := b.Waiting(stale); got != 0 {
		t.Fatalf("a pledge %s old still counts as waiting", PledgeTTL)
	}
	if grants := b.NextGrants(stale, 0); len(grants) != 0 {
		t.Fatalf("granted a stale pledge: %+v", grants)
	}
	// A grant expires and returns its slot.
	b.Pledge(stale, "bob", 10000)
	if len(b.NextGrants(stale, 0)) != 1 {
		t.Fatal("bob was not granted")
	}
	later := stale.Add(GrantTTL + time.Second)
	if got := b.InFlight(later); got != 0 {
		t.Fatalf("a grant %s old is still in flight", GrantTTL)
	}
	if got := b.Outstanding(later); got != 0 {
		t.Fatalf("an expired grant still counts %d as promised — the mule would never appeal again", got)
	}
}

func TestBoardRefreshesRatherThanQueueingTwice(t *testing.T) {
	now := time.Now()
	b := NewBoard(3)
	// The real case: an account's login username uses an underscore where its in-game display
	// name shows a space, so the mule hears both spellings for one account.
	b.Pledge(now, "alice smith", 5000)
	b.Pledge(now.Add(time.Second), "Alice Smith", 9000)
	b.Pledge(now.Add(2*time.Second), "Alice_Smith", 12000)
	if got := b.Waiting(now.Add(2 * time.Second)); got != 1 {
		t.Fatalf("Waiting = %d; the same account under three spellings must be one pledge", got)
	}
	grants := b.NextGrants(now.Add(2*time.Second), 0)
	if len(grants) != 1 || grants[0].Qty != 12000 {
		t.Fatalf("grants = %+v; the freshest reading must win", grants)
	}
}

func TestNewBoardNeverSilentlySwitchesItselfOff(t *testing.T) {
	for _, n := range []int{0, -1} {
		if got := NewBoard(n).MaxInFlight(); got != DefaultMaxInFlight {
			t.Fatalf("NewBoard(%d).MaxInFlight() = %d; a board that can never grant is a silent kill switch", n, got)
		}
	}
}

// The appeal deliberately does NOT travel through orderplan's grammar — see the note at the
// bottom of restock.go. This pins the reason so nobody "tidies it up" by routing it through
// Format/Parse: the live mule's own want is 15,000,000 and orderplan.QtyMax is a million.
func TestAnAppealIsTooLargeForTheHandOutGrammar(t *testing.T) {
	want := DecideAppealQty(t)
	if want <= orderplan.QtyMax {
		t.Fatalf("the live appeal want %d no longer exceeds orderplan.QtyMax %d; re-read restock.go's closing note before changing anything",
			want, orderplan.QtyMax)
	}
	if _, err := orderplan.Parse(orderplan.Format(CoinsItem, want)); err == nil {
		t.Fatalf("orderplan parsed an appeal-sized quantity (%d) — the two grammars have converged and the note at the bottom of restock.go is now wrong", want)
	}
}

// DecideAppealQty is the want DecideAppeal produces for the live mule's own configuration.
func DecideAppealQty(t *testing.T) int {
	t.Helper()
	a, ok, why := DecideAppeal(time.Now(), Need{Item: CoinsItem, Held: 9_000_000, Floor: 12_000_000, Target: 24_000_000})
	if !ok {
		t.Fatalf("the live configuration produced no appeal: %s", why)
	}
	return a.Qty
}

func nameOf(i int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	return "donor" + string(letters[i/26]) + string(letters[i%26])
}
