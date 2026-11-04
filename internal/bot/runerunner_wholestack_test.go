package bot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/runerunnerplan"
	"rsb0t-go/internal/scripts/tile"
)

// THE OWNER'S ASK, 14 October 2026: "youll need to request mules entire cash stack and spend it on everything
// instead of just running out". These tests rebuild that afternoon's purse (a 1.1 million-coin round, 857,720 held,
// the mule publishing 11,229,284) and the ammo stops that skipped "nothing to spend on ammo" with 824,379 coins in
// the pack and 764,906 in the bank.

// muleRig makes the rig's mule known, heard on the rig's world and publishing this many coins.
func muleRig(r *rotRig, coins int) {
	r.ctx.Peers = peerbank.New()
	publishRuneMule(r, map[string]int{"Coins": coins})
	r.ctx.WorldOf = func(string) int { return r.ctx.World() }
	r.ctx.Bus = mulebus.NewBus()
	r.ctx.Bus.Publish(mulebus.Message{Kind: mulebus.KindMuleHere, From: "mule", World: r.ctx.World(), State: mulebus.StateHome})
}

// The ask for a runner short of the round's bill is the mule's whole published stack above the floor, not the
// shortfall and not topUpAmount: the log says what was held, what the mule holds and what is asked.
func TestPurseAsksForTheMulesWholeStack(t *testing.T) {
	r := ammoRig(t, "wholestack1", 301, nil)
	defer releaseBoards("wholestack1")
	muleRig(r, 11229284)
	seedBoards(r, 40*time.Minute)
	rn := r.b.roundNeed(r.ctx, r.afterDraw(t))
	if rn.Ammo <= 0 {
		t.Fatalf("the rig's round carries no ammo: %s", rn)
	}
	// 200,000 short of the bill: the old ask was topUpAmount (300,000), the live number.
	bank := rn.Bill() + r.b.set.CoinReserve - 200000
	d := r.b.deps(r.ctx)

	r.b.set.TopUpWholeStack = false
	v, held, below := r.b.purseVerdict(r.ctx, d, bank, rn)
	if v.Ask != 300000 || held != bank || below != rn.Bill()+r.b.set.CoinReserve {
		t.Fatalf("whole stack off: ask %d held %d below %d (%s)", v.Ask, held, below, v.Reason)
	}

	r.logs = nil
	r.b.set.TopUpWholeStack = true
	v, held, below = r.b.purseVerdict(r.ctx, d, bank, rn)
	if want := 11229284 - r.b.set.MuleCoinFloor; v.Ask != want {
		t.Fatalf("whole stack on: ask %d, want %d (%s)", v.Ask, want, v.Reason)
	}
	if below != rn.Bill()+r.b.set.CoinReserve || held >= below {
		t.Errorf("below %d held %d: the runner must be under the bill (with its ammo) to ask", below, held)
	}
	for _, want := range []string{
		"purse round_need=", fmt.Sprintf("bill=%d", rn.Bill()), fmt.Sprintf("target=%d", rn.Bill()+r.b.set.CoinReserve),
		"mule_bank=11229284", "mule_floor=10000", "whole_stack=true", fmt.Sprintf("ask=%d", v.Ask), "whole published stack",
	} {
		if !strings.Contains(r.log(), want) {
			t.Errorf("the purse line lacks %q\n%s", want, r.log())
		}
	}
	// The ammo is part of what the runner must hold: at the rune total alone it is still under the bill.
	if v, _, _ := r.b.purseVerdict(r.ctx, d, rn.Total()+r.b.set.CoinReserve, rn); v.Ask <= 0 {
		t.Errorf("a runner at the rune total (%d) but under the bill with its ammo (%d) was not asked", rn.Total()+r.b.set.CoinReserve, rn.Bill()+r.b.set.CoinReserve)
	}
	// A runner that holds the bill asks for nothing.
	if v, _, _ := r.b.purseVerdict(r.ctx, d, rn.Bill()+r.b.set.CoinReserve, rn); v.Ask != 0 {
		t.Errorf("a funded runner asked for %d", v.Ask)
	}
	// muleCoinFloor 0 takes every coin.
	r.b.set.MuleCoinFloor = 0
	if v, _, _ := r.b.purseVerdict(r.ctx, d, bank, rn); v.Ask != 11229284 {
		t.Errorf("floor 0: ask %d", v.Ask)
	}
}

// With the mule's bank unpublished the ask is the whole shortfall to the bill, never the old 300,000.
func TestPurseAsksTheShortfallWhenTheStackIsNotPublished(t *testing.T) {
	r := ammoRig(t, "wholestack2", 305, nil)
	defer releaseBoards("wholestack2")
	muleRig(r, 11229284)
	r.ctx.Peers = peerbank.New() // nothing published
	seedBoards(r, 40*time.Minute)
	rn := r.b.roundNeed(r.ctx, r.afterDraw(t))
	bank := 300000
	v, held, below := r.b.purseVerdict(r.ctx, r.b.deps(r.ctx), bank, rn)
	if want := below - held; v.Ask != want || want <= 300000 || !strings.Contains(v.Reason, "not published") {
		t.Errorf("ask %d, want the shortfall %d (%s)", v.Ask, want, v.Reason)
	}
	if !strings.Contains(r.log(), "mule_bank=unknown") {
		t.Errorf("no unknown stack in the line\n%s", r.log())
	}
}

// The line after a purse that needed no ask, and the one after a short purse, say what was held against the bill.
func TestMulePurseSaysFundedOrShort(t *testing.T) {
	r := ammoRig(t, "wholestack3", 309, nil)
	defer releaseBoards("wholestack3")
	muleRig(r, 11229284)
	seedBoards(r, 40*time.Minute)
	rn := r.b.roundNeed(r.ctx, r.afterDraw(t))
	d := r.b.deps(r.ctx)
	post := tile.Tile{X: 2809, Z: 3441}
	funded := rn.Bill() + r.b.set.CoinReserve + 250000
	if _, read, ok := r.b.mulePurse(r.ctx, d, nil, runerunner.BankRead{Coins: funded}, post, rn); !ok || read.Coins != funded {
		t.Fatalf("a funded purse: ok %v read %+v", ok, read)
	}
	for _, want := range []string{"ask=0", "purse FUNDED", fmt.Sprintf("%d coins held", funded), fmt.Sprintf("bill of %d", rn.Bill()), "250000 to spare", "asked=0 received=0"} {
		if !strings.Contains(r.log(), want) {
			t.Errorf("funded: no %q\n%s", want, r.log())
		}
	}
	if strings.Contains(r.log(), "purse SHORT") || strings.Contains(r.log(), "top-up asking") {
		t.Errorf("a funded purse asked or said short\n%s", r.log())
	}

	// Short after an ask: the line names the shortfall to the bill including the ammo.
	r.logs = nil
	short := rn.Bill() + r.b.set.CoinReserve - 400000
	r.b.purseShort(r.ctx, rn, short, 2130077)
	want := fmt.Sprintf("purse SHORT by 400000: %d coins held against a target of %d (the mule just gave 2130077)", short, rn.Bill()+r.b.set.CoinReserve)
	if !strings.Contains(r.log(), want) {
		t.Errorf("short line, want %q\n%s", want, r.log())
	}
	// And a funded one after a successful ask reports what was asked and received.
	r.logs = nil
	r.b.purseFunded(r.ctx, rn, 12000000, 11219284, 11219284)
	if !strings.Contains(r.log(), "purse FUNDED: 12000000 coins held") || !strings.Contains(r.log(), "asked=11219284 received=11219284") {
		t.Errorf("funded after an ask\n%s", r.log())
	}
	// Top-up off: nothing is said about a purse nobody sizes.
	r.logs = nil
	r.b.set.TopUpAmount = 0
	r.b.purseFunded(r.ctx, rn, 12000000, 0, 0)
	if strings.Contains(r.log(), "purse FUNDED") {
		t.Errorf("top-up off still said funded\n%s", r.log())
	}
}

// bankRig is an ammo rig standing at its first Hickton with the mule publishing a Bolt, and the pack-paid need of
// the runes ahead of it. Each step below has a rig of its own: a buying visit moves the world's shop board.
func bankRig(t *testing.T, name string, base int) (*rotRig, int) {
	t.Helper()
	r := ammoRig(t, name, base, nil)
	publishRuneMule(r, map[string]int{"Bolts": 1})
	r.atKey(t, fmt.Sprintf("Hickton@%d", base))
	need := r.b.roundNeed(r.ctx, r.b.st.Cursor+1).PackNeed()
	if need < 1000000 {
		t.Fatalf("the rig's round holds only %d for the runes ahead", need)
	}
	return r, need
}

// LIVE 14 October 13:2x: 824,379 coins in the pack, 764,906 in the bank, 949,886 needed by the runes ahead. The pack-only
// hold skipped all three ammo stops; the bank funds the runes ahead at their own shops, so the pack is free for ammo.
func TestAmmoStationSpendsWhatTheBankFreesUp(t *testing.T) {
	// No bank reading: the whole need is held, as before.
	r, need := bankRig(t, "wholestack4", 313)
	pack := need / 2 // a pack short of the runes ahead, as in the live round
	r.pack(pack, 20, nil)
	visits := 0
	r.shelf(hicktonShelf(), pack, &visits, 1)
	if got := r.runAmmo(); got.Outcome != runerunnerplan.OutcomeSkipped || visits != 0 || !strings.Contains(got.Reason, "nothing to spend on ammo") {
		t.Fatalf("no bank reading: %+v visits %d", got, visits)
	}

	// A bank that holds the runes ahead (the mule's whole stack went into it): the pack is free for ammo.
	r, need = bankRig(t, "wholestack4b", 333)
	pack = need / 2
	r.pack(pack, 20, nil)
	r.b.st.BankCoins, r.b.st.BankReadAt = need+r.b.set.CoinReserve+500000, time.Now()
	if hold := r.b.ammoHold(r.ctx); hold != 0 {
		t.Fatalf("a bank over the need still holds %d back", hold)
	}
	visits = 0
	r.shelf(hicktonShelf(), pack, &visits, 1)
	got := r.runAmmo()
	if got.Outcome != runerunnerplan.OutcomeBought || got.Spent <= 0 || visits != 1 {
		t.Fatalf("a bank over the need: %+v visits %d\n%s", got, visits, r.log())
	}

	// A bank that covers part of the need: the hold is the rest, so a pack at the hold plus a little buys a little.
	r, need = bankRig(t, "wholestack4c", 337)
	r.b.st.BankCoins, r.b.st.BankReadAt = r.b.set.CoinReserve+need/4, time.Now()
	hold := r.b.ammoHold(r.ctx)
	if want := need - need/4; hold != want {
		t.Fatalf("a bank at a quarter of the need: hold %d, want %d", hold, want)
	}
	r.pack(hold+r.b.set.FareCoins+20000, 20, nil)
	r.shelf(hicktonShelf(), hold+r.b.set.FareCoins+20000, nil, 1)
	if got = r.runAmmo(); got.Outcome != runerunnerplan.OutcomeBought || got.Spent < 14000 || got.Spent > 20000 {
		t.Errorf("a bank at a quarter of the need, 20,000 beyond the hold: %+v\n%s", got, r.log())
	}

	// A bank that is empty (the whole mule stack was already taken and spent): the runes come first, and the
	// skip line shows the sums.
	r, need = bankRig(t, "wholestack4d", 341)
	r.b.st.BankCoins, r.b.st.BankReadAt = r.b.set.CoinReserve, time.Now()
	r.pack(5000, 20, nil)
	visits = 0
	r.shelf(hicktonShelf(), 5000, &visits, 1)
	got = r.runAmmo()
	if got.Outcome != runerunnerplan.OutcomeSkipped || visits != 0 {
		t.Fatalf("an empty bank: %+v visits %d", got, visits)
	}
	for _, want := range []string{"5000 coins in the pack", "300 for the fare", fmt.Sprintf("%d held back for the runes ahead (they need %d and the bank adds 0)", need, need), "nothing to spend on ammo"} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("skip reason %q lacks %q", got.Reason, want)
		}
	}
}

// A round that has already left runes on a shelf for coins still buys no ammo, whatever the bank reads: that is the
// truly-short case (after the whole mule stack was taken), where the runes come first.
func TestAmmoStationStillWaitsWhenTheRoundIsTrulyShort(t *testing.T) {
	r := ammoRig(t, "wholestack5", 317, nil)
	publishRuneMule(r, map[string]int{"Bolts": 1})
	r.atKey(t, "Hickton@317")
	r.b.st.BankCoins, r.b.st.BankReadAt = 20000000, time.Now()
	r.b.st.Done["Betty@319"] = runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeBought, ShortRunes: 40, ShortCoins: 9000}
	visits := 0
	r.pack(5000000, 20, nil)
	r.shelf(hicktonShelf(), 5000000, &visits, 1)
	if got := r.runAmmo(); got.Outcome != runerunnerplan.OutcomeSkipped || visits != 0 || !strings.Contains(got.Reason, "the runes come first") {
		t.Errorf("a short round bought ammo: %+v visits %d", got, visits)
	}
}

// Zaff's staffs follow the same bank: a pack with 1,000 coins beyond a staff's price buys one when the bank holds the
// runes ahead, where half the pack was held back before.
func TestStaffHoldCountsTheBank(t *testing.T) {
	r := ammoRig(t, "wholestack6", 321, nil)
	r.atKey(t, "Zaff@322")
	pack := 2300
	old := r.b.staffHold(r.ctx, pack)
	if old != (pack-r.b.set.FareCoins)/2 {
		t.Fatalf("no bank reading: hold %d, want half the pack's spare %d", old, (pack-r.b.set.FareCoins)/2)
	}
	r.b.st.BankCoins, r.b.st.BankReadAt = 30000000, time.Now()
	if hold := r.b.staffHold(r.ctx, pack); hold != 0 {
		t.Errorf("a bank that holds the runes ahead: hold %d, want 0", hold)
	}
	r.b.st.BankCoins = r.b.set.CoinReserve
	if hold := r.b.staffHold(r.ctx, pack); hold != old {
		t.Errorf("an empty bank: hold %d, want the old %d", hold, old)
	}
}

// The round-close line: what the mule gave, what the stations paid shops (the ammo share apart) and what is kept.
func TestRoundPurseLineReadsTheRound(t *testing.T) {
	r := ammoRig(t, "wholestack7", 325, nil)
	r.b.st.StartRound(r.b.st.Ring, 0, time.Now())
	r.b.st.NoteTopUp(1000000)
	r.b.st.NoteTopUp(130077)
	r.b.st.BankCoins = 9500000
	r.b.st.Done = map[string]runerunnerplan.StationResult{
		"Betty@327":    {Outcome: runerunnerplan.OutcomeBought, Spent: 300000},
		"Aubury@326":   {Outcome: runerunnerplan.OutcomeBought, Spent: 400000},
		"Hickton@325":  {Outcome: runerunnerplan.OutcomeBought, Spent: 40000},
		"Gerrant@327":  {Outcome: runerunnerplan.OutcomeBought, Spent: 9000},
		"Lundail@326":  {Outcome: runerunnerplan.OutcomeBought, Spent: 111000},
		"deliver@325":  {Outcome: runerunnerplan.OutcomeDelivered},
		"Zaff@326":     {Outcome: runerunnerplan.OutcomeBought, Spent: 3000},
		"Lowe@326":     {Outcome: runerunnerplan.OutcomeSkipped},
		"market@325":   {Outcome: runerunnerplan.OutcomeSkipped},
		"purse@325":    {Outcome: runerunnerplan.OutcomeSkipped},
		"draw@325":     {Outcome: runerunnerplan.OutcomeDrew},
		"WizardsGuild": {Outcome: runerunnerplan.OutcomeSkipped},
	}
	line := r.b.roundPurse(480000).String()
	for _, want := range []string{
		fmt.Sprintf("purse round=%d", r.b.st.Round), "received=1130077", "spent=863000", "runes_and_staffs=814000", "ammo=49000",
		"kept=9980000 (bank 9500000 + pack 480000)", "no coin goes back to the mule",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("%q lacks %q", line, want)
		}
	}
}

// The purse the mule is asked to fill is sized from the bill with its ammo: two rigs with the same shelves, ammo on and
// off, differ by exactly the ammo estimate (while the rune need the shop banks and the market read does not move).
func TestPurseAskIncludesTheAmmoEstimate(t *testing.T) {
	r := ammoRig(t, "wholestack8", 329, map[string]interface{}{"marketCoins": 1000000.0})
	defer releaseBoards("wholestack8")
	rn := r.b.roundNeed(r.ctx, r.afterDraw(t))
	if rn.Ammo <= 0 {
		t.Fatalf("no ammo in the round: %s", rn)
	}
	on, _ := r.b.purseAsk(rn, 0)
	plain := rn
	plain.Ammo = 0
	off, _ := r.b.purseAsk(plain, 0)
	if on-off != rn.Ammo || on != rn.Bill()+r.b.set.CoinReserve {
		t.Errorf("below %d against %d without the ammo (%d): want a difference of %d", on, off, rn.Ammo, rn.Ammo)
	}
}
