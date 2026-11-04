package orderplan

import (
	"strings"
	"testing"
	"time"
)

func TestFormatParseRoundTrips(t *testing.T) {
	for _, tc := range []struct {
		item string
		qty  int
	}{
		{"Small fishing net", 1},
		{"Big fishing net", 3},
		{"Lobster pot", 1},
		{"Fly fishing rod", 2},
	} {
		got, err := Parse(Format(tc.item, tc.qty))
		if err != nil {
			t.Fatalf("Parse(Format(%q, %d)): %v", tc.item, tc.qty, err)
		}
		if got.Item != tc.item || got.Qty != tc.qty {
			t.Fatalf("round trip %q x%d -> %+v", tc.item, tc.qty, got)
		}
	}
}

func TestParseRejectsMalformedRatherThanGuessing(t *testing.T) {
	// The safety requirement is exact: a malformed or unknown order is declined, never
	// interpreted into something that might match the catalogue.
	for _, tc := range []struct {
		text string
		want ParseError
	}{
		{"", ErrEmpty},
		{"   ", ErrEmpty},
		{"give me a net", ErrBadQty},      // wrong verb is caught first only when it is not "need"
		{"needless 1 Harpoon", ErrNoVerb}, // a word merely starting with the verb is not the verb
		{"need", ErrNoQty},                // verb alone
		{"need Harpoon", ErrBadQty},       // no number where the number goes
		{"need 0 Harpoon", ErrBadQty},     // zero is not a quantity
		{"need -2 Harpoon", ErrBadQty},    // nor is a negative one
		// Expressed against QtyMax rather than a literal: this case was "need 1000000 Harpoon"
		// until QtyMax itself was raised to a million on 2026-09-21, at which point the literal
		// quietly became a LEGAL quantity and the test failed. What is being asserted is "one
		// past the bound is refused", so say that.
		{Format("Harpoon", QtyMax+1), ErrQtyTooLarge},
		{"need 1", ErrNoItem},
		{"need 1 " + strings.Repeat("a", ItemNameMax+1), ErrItemTooLong},
		{"need 1 Harpoon; ::give coins 1000", ErrItemChars}, // a smuggled cheat string is not a name
		{"need 1 Harpoon\ncollect", ErrItemChars},
	} {
		got, err := Parse(tc.text)
		if err == nil {
			t.Fatalf("Parse(%q) accepted %+v, want rejection", tc.text, got)
		}
		if tc.text == "give me a net" {
			// This one only has to be rejected, not with a specific error: "give" fails
			// the verb check before the quantity is ever read.
			if err != ErrNoVerb {
				t.Fatalf("Parse(%q) = %v, want %v", tc.text, err, ErrNoVerb)
			}
			continue
		}
		if err != tc.want {
			t.Fatalf("Parse(%q) = %v, want %v", tc.text, err, tc.want)
		}
	}
}

func TestParseTolerantOfWhitespaceButNotOfShape(t *testing.T) {
	got, err := Parse("  NEED   2   Big fishing net  ")
	if err != nil {
		t.Fatalf("whitespace-padded order: %v", err)
	}
	if got.Item != "Big fishing net" || got.Qty != 2 {
		t.Fatalf("whitespace-padded order parsed as %+v", got)
	}
}

func TestParseErrorsFitADeclineReason(t *testing.T) {
	// Every parse error is published straight back to the donor inside
	// mulebus.DeclinedMessage, whose Reason is truncated at 80 bytes. A truncated reason
	// would still work but would read as garbage in a log, so keep them short by test.
	for _, e := range []ParseError{ErrEmpty, ErrNoVerb, ErrNoQty, ErrBadQty, ErrQtyTooLarge, ErrNoItem, ErrItemTooLong, ErrItemChars} {
		if len(e.Error()) > 80 {
			t.Fatalf("parse error %q is %d bytes, too long for a decline reason", e, len(e.Error()))
		}
	}
}

func fixedPolicy() Policy {
	return DefaultPolicy([]string{"spare28", "big_net"})
}

func alwaysStocked(string) int { return 5 }

func TestDecideHonoursAGenuineOrder(t *testing.T) {
	now := time.Now()
	led := NewLedger(DefaultWindow)
	d := Decide(now, "spare28", Format("Big fishing net", 1), fixedPolicy(), led, alwaysStocked)
	if d.Verdict != Fulfil {
		t.Fatalf("a real order from an allow-listed donor was declined: %+v", d)
	}
	if d.Item != "Big fishing net" || d.Qty != 1 || d.Clamped {
		t.Fatalf("decision = %+v", d)
	}
}

func TestDecideReportsTheCanonicalSpellingNotTheDonorsOwn(t *testing.T) {
	// The caller resolves Decision.Item against the game's object config. Handing it back
	// the donor's own casing would make that lookup depend on how a bus message was typed.
	d := Decide(time.Now(), "spare28", "need 1 bIG fIsHiNg NeT", fixedPolicy(), NewLedger(DefaultWindow), alwaysStocked)
	if d.Verdict != Fulfil || d.Item != "Big fishing net" {
		t.Fatalf("decision = %+v, want the catalogue's own spelling", d)
	}
}

func TestDecideRefusesANameOffTheAllowList(t *testing.T) {
	// Non-negotiable: the bus carries no authentication, so this is the only gate.
	d := Decide(time.Now(), "someone_else", Format("Big fishing net", 1), fixedPolicy(), NewLedger(DefaultWindow), alwaysStocked)
	if d.Verdict != Decline {
		t.Fatalf("an order from an unknown name was honoured: %+v", d)
	}
}

func TestDecideMatchesAllowListAcrossUnderscoreAndSpace(t *testing.T) {
	// An account's login username uses an underscore where its in-game display name shows a
	// space; both spellings reach this code from different places (settings vs Players()).
	// This is the exact class of bug mulerunner.normalisePlayerName was added to fix live.
	for _, name := range []string{"big_net", "Big Net", "BIG NET", " big net "} {
		if !AllowedFrom(fixedPolicy(), name) {
			t.Fatalf("allow-list did not match %q", name)
		}
	}
	if AllowedFrom(fixedPolicy(), "") {
		t.Fatal("an empty name matched the allow list")
	}
	if AllowedFrom(fixedPolicy(), "bignet") {
		t.Fatal("a name that is not on the list matched")
	}
}

// TestDecideRefusesAnItemOffTheCatalogueWhenOpenStockIsOff is what this test asserted for the
// whole of the catalogue's life, and it is still the rule for a policy that has not opted in.
//
// It used to run against DefaultPolicy, and stopped being true there on 2026-09-22 when the
// users asked for the opposite ("mulereciever wasnt updated to include handing out of all
// items in bank if requested from another account"). The assertion is kept rather than deleted
// because it is still exactly right for the policies that matter most: peerrunner.onePolicy
// authorises ONE item for ONE donor and must keep refusing everything else, and a mule with
// handoutAnything off must behave as it did before the widening existed.
func TestDecideRefusesAnItemOffTheCatalogueWhenOpenStockIsOff(t *testing.T) {
	pol := fixedPolicy()
	pol.Open = OpenStock{} // the zero value: exactly what a hand-built policy carries
	for _, item := range []string{"Rune platebody", "Raw lobster", "Dragon dagger", "Uncut diamond"} {
		d := Decide(time.Now(), "spare28", Format(item, 1), pol, NewLedger(DefaultWindow), alwaysStocked)
		if d.Verdict != Decline {
			t.Fatalf("%q is off the catalogue and this policy has no OpenStock, but it was honoured: %+v — a zero-valued OpenStock must mean the pre-2026-09-22 behaviour exactly", item, d)
		}
	}
}

// realMuleStock is the LIVE the mule bank, read on 2026-09-22 by a read-only
// GET /api/bank?account=the mule against the running panel on 127.0.0.1:7300 while the fleet was
// up. Only the rows the tests below reason about; everything else in that bank (226 distinct
// items) answers zero here, which is what makes the axe assertions meaningful.
//
// The one fact the whole widening exists for: there is exactly ONE chopping axe in it, an
// Adamant axe, and NO mithril, steel, iron or bronze axe — so every rung of AxeHandouts, which
// is the ladder a woodcutter actually asks down, is empty.
func realMuleStock(item string) int {
	switch strings.ToLower(strings.TrimSpace(item)) {
	case "adamant axe":
		return 1
	case "shark":
		return 13317
	case "coins":
		return 2576948
	case "crystal key":
		return 46
	case "half of a key":
		return 11
	case "small fishing net":
		return 1
	case "fishing rod":
		return 18
	default:
		// Mithril axe, Steel axe, Iron axe, Bronze axe and everything else: none.
		return 0
	}
}

// TestDecideServesTheOneAxeTheLiveMuleActuallyHolds is the owner's report turned into an
// assertion, against the measured bank above rather than an invented one.
//
// Before this change a woodcutter could ask for four axes, all four absent, and could not ask
// for the one that was there: AxeHandouts stops at mithril and its own doc comment excludes
// adamant deliberately. The catalogue, not the bank, was the limiter — orderplan.Decide had
// already read the bank correctly and answered honestly.
func TestDecideServesTheOneAxeTheLiveMuleActuallyHolds(t *testing.T) {
	pol := DefaultPolicy([]string{"acct134"})

	d := Decide(time.Now(), "acct134", Format("Adamant axe", 1), pol, NewLedger(DefaultWindow), realMuleStock)
	if d.Verdict != Fulfil {
		t.Fatalf("an explicit order for the Adamant axe the live mule holds was declined (%q) — this is the fleet owner's 2026-09-22 report, and the item is genuinely in the bank", d.Reason)
	}
	if d.Qty != 1 {
		t.Errorf("Qty = %d, want 1: the bank holds one Adamant axe and an account needs one axe", d.Qty)
	}
	if !strings.EqualFold(d.Item, "Adamant axe") {
		t.Errorf("Item = %q, want the name that was asked for so the caller can resolve it against the object config", d.Item)
	}

	// The control, and the reason the case above matters: every rung the ladder CAN ask for is
	// genuinely absent, and must still be declined honestly rather than widened into a lie.
	for _, h := range AxeHandouts() {
		if realMuleStock(h.Item) > 0 {
			continue // the Adamant axe joined the ladder on 2026-10-04 and is the case above
		}
		d := Decide(time.Now(), "acct134", Format(h.Item, 1), pol, NewLedger(DefaultWindow), realMuleStock)
		if d.Verdict != Decline {
			t.Errorf("%q was fulfilled from a bank holding none of it: %+v", h.Item, d)
		}
		if !strings.Contains(d.Reason, "none in the bank") {
			t.Errorf("%q declined with %q, want the bank-read wording every asking script already reads (woodcutplan.MuleDeclineIsEmptyStock, donorplan.ClassifyRefusal)", h.Item, d.Reason)
		}
	}
}

// TestOpenStockServesTheFoodAndRunesTheMuleIsStandingOn is the same defect in the other script.
// Live the mule's roster entry carries NO handoutItems list, so before this change every Scarab
// kit ask — food, runes — was answered "not an item I hand out" by a mule holding 13,317 Sharks.
func TestOpenStockServesTheFoodAndRunesTheMuleIsStandingOn(t *testing.T) {
	pol := DefaultPolicy([]string{"acct7"})
	d := Decide(time.Now(), "acct7", Format("Shark", 20), pol, NewLedger(DefaultWindow), realMuleStock)
	if d.Verdict != Fulfil || d.Qty != 20 {
		t.Fatalf("an order for 20 Sharks against 13,317 banked came back %+v, want all 20: a combat account that cannot draw food from the mule cannot be fed at all", d)
	}
	if d.Clamped {
		t.Errorf("Clamped = true for 20 of 13,317 — nothing should have narrowed this")
	}
}

// TestOpenStockKeepsTheArguedOverItemsOnTheirCatalogueTerms is the whole compromise, pinned.
//
// The widening must not become a way around a bound somebody argued for. Two halves:
// a catalogue entry always wins over the open path, and the three names on NeverOpen can ONLY
// leave under a catalogue entry — so replacing the catalogue wholesale (which
// internal/bot/mule.go's handoutItems setting does) cannot silently drop them into the open path
// at OpenMaxPerOrder.
func TestOpenStockKeepsTheArguedOverItemsOnTheirCatalogueTerms(t *testing.T) {
	// Half 1: the catalogue wins. Coins are catalogued at CoinsPerOrder, and an order for a
	// million must clamp to that and not to OpenMaxPerOrder.
	pol := DefaultPolicy([]string{"acct134"})
	d := Decide(time.Now(), "acct134", Format("Coins", 1_000_000), pol, NewLedger(DefaultWindow), realMuleStock)
	if d.Verdict != Fulfil || d.Qty != CoinsPerOrder {
		t.Fatalf("coins came back %+v, want a clamp to CoinsPerOrder (%d): the catalogue's own ceiling, not the open path's", d, CoinsPerOrder)
	}

	// Half 2: with the catalogue replaced by one that names none of them, the open path must
	// still refuse all three rather than inventing terms for them.
	bare := DefaultPolicy([]string{"acct134"})
	bare.Catalog = []Handout{{Item: "Harpoon", Max: 1}}
	for _, item := range NeverOpen() {
		d := Decide(time.Now(), "acct134", Format(item, 10), bare, NewLedger(DefaultWindow), realMuleStock)
		if d.Verdict != Decline {
			t.Errorf("%q was handed out by the open path with no catalogue entry: %+v — its ceiling (coins), batch floor (crystal keys) or the mule's own key production (half of a key) would have been routed around", item, d)
		}
		if d.Reason != DeclineKeptBack {
			t.Errorf("%q declined with %q, want DeclineKeptBack: an asking script must be able to tell \"I have it and am keeping it\" from \"no such item in my policy\"", item, d.Reason)
		}
	}
}

// TestOpenStockDoesNotRouteAroundTheCrystalKeyFloor is the same point at the level that actually
// bit this repo once. The crystal key's Floor is the account owner's own number (100) and the
// live bank held 46 when this was written; an explicit order must get the batch decline WITH the
// real count in it, because lootchestfisherplan.ParseBankedCount reads that count.
func TestOpenStockDoesNotRouteAroundTheCrystalKeyFloor(t *testing.T) {
	pol := DefaultPolicy([]string{"spare104"})
	d := Decide(time.Now(), "spare104", Format("Crystal key", 500), pol, NewLedger(DefaultWindow), realMuleStock)
	if d.Verdict != Decline {
		t.Fatalf("46 banked against a floor of %d was fulfilled: %+v", KeysFloor, d)
	}
	if !strings.Contains(d.Reason, "46") || !strings.Contains(d.Reason, "holding until") {
		t.Errorf("reason = %q, want the real banked count and the floor — lootchestfisherplan.ParseBankedCount reads exactly this sentence", d.Reason)
	}
}

// TestOpenStockCapsOneOrderAndOneWindow pins the two bounds that replace the catalogue as the
// thing standing between a bus message and a bank of 226 items.
func TestOpenStockCapsOneOrderAndOneWindow(t *testing.T) {
	pol := DefaultPolicy([]string{"acct7"})
	led := NewLedger(DefaultWindow)
	now := time.Now()

	// Per order: clamped down, never refused — a donor asking for more than it may have is
	// still a donor that needs some (Handout.Max's own rule, applied here too).
	d := Decide(now, "acct7", Format("Shark", 5000), pol, led, realMuleStock)
	if d.Verdict != Fulfil || d.Qty != OpenMaxPerOrder || !d.Clamped {
		t.Fatalf("an order for 5000 came back %+v, want a clamp to OpenMaxPerOrder (%d) with Clamped set", d, OpenMaxPerOrder)
	}

	// Per window, across everyone: the bound that actually caps a bug or a compromised donor
	// name, since the per-donor cap means little across a ~269-account fleet.
	pol.Allowed = append(pol.Allowed, "acct8")
	for spent := 0; spent < OpenPerWindow; spent += OpenMaxPerOrder {
		led.Record(now, "acct8", "Shark", OpenMaxPerOrder)
	}
	after := Decide(now, "acct7", Format("Shark", 10), pol, led, realMuleStock)
	if after.Verdict != Decline {
		t.Fatalf("with %d Sharks already handed out this window the next order was honoured: %+v", OpenPerWindow, after)
	}
}

// TestOpenStockKeepBackIsAReserveAndNotOnlyAGate exists because this package has already made
// the opposite mistake once, in the other direction: Handout.Floor was DOCUMENTED as a gate and
// IMPLEMENTED as a permanent reserve, and a mule with 418 keys handed over all 418 because
// nothing after the gate consulted the floor again. OpenStock.KeepBack says reserve, so it must
// behave as one on every order, not once.
func TestOpenStockKeepBackIsAReserveAndNotOnlyAGate(t *testing.T) {
	pol := DefaultPolicy([]string{"acct7"})
	pol.Open.KeepBack = 13000 // against 13,317 Sharks banked

	d := Decide(time.Now(), "acct7", Format("Shark", 1000), pol, NewLedger(DefaultWindow), realMuleStock)
	if d.Verdict != Fulfil {
		t.Fatalf("an order against stock above the reserve was declined: %+v", d)
	}
	if d.Qty != 317 || !d.Clamped {
		t.Errorf("Qty = %d (clamped %v), want 317 = 13,317 banked minus the 13,000 reserve — a reserve that is only a gate lets the first asker take the lot", d.Qty, d.Clamped)
	}

	// At or below the reserve nothing may leave: the reserve is what STAYS, so an order needs
	// stock above it to be fillable at all.
	atFloor := DefaultPolicy([]string{"acct7"})
	atFloor.Open.KeepBack = 13317
	d = Decide(time.Now(), "acct7", Format("Shark", 10), atFloor, NewLedger(DefaultWindow), realMuleStock)
	if d.Verdict != Decline {
		t.Errorf("an order against exactly the reserve was fulfilled: %+v", d)
	}
}

// TestInCatalogStillMeansTheCuratedListOnly guards a subtle way this change could break two
// callers that ask InCatalog a DIFFERENT question. restockplan.WindowFor derives a mule's
// restock floor from a catalogued WindowMax and must read "no ceiling configured" (0) for an
// item that has none; internal/bot's startup checks use it to warn an operator that a script's
// item is not on the default list. Widening InCatalog itself would have silently broken both,
// which is why Entitlement is a separate function.
func TestInCatalogStillMeansTheCuratedListOnly(t *testing.T) {
	pol := DefaultPolicy(nil) // open stock ON
	if _, listed := InCatalog(pol, "Shark"); listed {
		t.Error("InCatalog says Shark is catalogued — it is not; only Entitlement should serve it")
	}
	if _, ok := Entitlement(pol, "Shark"); !ok {
		t.Error("Entitlement refuses Shark under a default policy, so the widening is not reachable at all")
	}
	if !WillHandOut(pol, "Shark") || WillHandOut(pol, "Half of a key") {
		t.Error("WillHandOut disagrees with Entitlement about Shark or about a NeverOpen item")
	}
	// And the catalogued items still resolve through InCatalog with their own terms.
	if h, listed := InCatalog(pol, "Crystal key"); !listed || h.Floor != KeysFloor {
		t.Errorf("the crystal key's catalogue entry changed: %+v listed=%v", h, listed)
	}
}

func TestDecideNeverHandsOutAnUnboundedQuantity(t *testing.T) {
	d := Decide(time.Now(), "spare28", Format("Harpoon", QtyMax), fixedPolicy(), NewLedger(DefaultWindow), alwaysStocked)
	if d.Verdict != Fulfil {
		t.Fatalf("a large but legal order should be capped, not refused: %+v", d)
	}
	if d.Qty != 1 || !d.Clamped {
		t.Fatalf("quantity was not capped to the catalogue's own Max: %+v", d)
	}
}

func TestDecideStopsRepeatedOrdersEmptyingTheBank(t *testing.T) {
	now := time.Now()
	pol := fixedPolicy()
	led := NewLedger(DefaultWindow)

	first := Decide(now, "spare28", Format("Harpoon", 1), pol, led, alwaysStocked)
	if first.Verdict != Fulfil {
		t.Fatalf("first order: %+v", first)
	}
	led.Record(now, "spare28", first.Item, first.Qty)

	second := Decide(now.Add(time.Minute), "spare28", Format("Harpoon", 1), pol, led, alwaysStocked)
	if second.Verdict != Decline {
		t.Fatalf("a second order inside the window was honoured: %+v", second)
	}

	// Once the window has passed the same donor may ask again — the cap is a rate, not a
	// permanent lockout, or a genuinely unlucky account could never be helped twice.
	later := Decide(now.Add(DefaultWindow+time.Minute), "spare28", Format("Harpoon", 1), pol, led, alwaysStocked)
	if later.Verdict != Fulfil {
		t.Fatalf("the per-donor cap did not age out: %+v", later)
	}
}

func TestDecideStopsManyDonorsBetweenThemEmptyingTheBank(t *testing.T) {
	// The per-donor cap alone is no protection at all with a ~255-account fleet on the bus.
	now := time.Now()
	pol := DefaultPolicy(nil)
	var names []string
	for i := 0; i < 30; i++ {
		names = append(names, "donor"+string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	pol.Allowed = names
	led := NewLedger(DefaultWindow)

	handed := 0
	for _, name := range names {
		d := Decide(now, name, Format("Harpoon", 1), pol, led, alwaysStocked)
		if d.Verdict == Fulfil {
			handed += d.Qty
			led.Record(now, name, d.Item, d.Qty)
		}
	}
	if handed != pol.MaxUnitsPerItem {
		t.Fatalf("handed out %d harpoons across %d donors, want the fleet-wide cap of %d", handed, len(names), pol.MaxUnitsPerItem)
	}
}

func TestDecideFailsClosed(t *testing.T) {
	now := time.Now()
	ok := fixedPolicy()

	// A zero-valued policy hands out nothing, so a caller that forgets to configure one
	// cannot accidentally open the bank.
	if d := Decide(now, "spare28", Format("Harpoon", 1), Policy{}, NewLedger(DefaultWindow), alwaysStocked); d.Verdict != Decline {
		t.Fatalf("a zero policy honoured an order: %+v", d)
	}
	if d := Decide(now, "spare28", Format("Harpoon", 1), ok, nil, alwaysStocked); d.Verdict != Decline {
		t.Fatalf("a missing ledger honoured an order: %+v", d)
	}
	if d := Decide(now, "spare28", Format("Harpoon", 1), ok, NewLedger(DefaultWindow), nil); d.Verdict != Decline {
		t.Fatalf("an unreadable stock honoured an order: %+v", d)
	}
	if d := Decide(now, "spare28", Format("Harpoon", 1), ok, NewLedger(DefaultWindow), func(string) int { return 0 }); d.Verdict != Decline {
		t.Fatalf("an empty bank honoured an order: %+v", d)
	}
}

func TestPrescreenRefusesWithoutEverNeedingABankRead(t *testing.T) {
	// The point of Prescreen existing at all: an unauthorised or malformed message is refused
	// from the message alone, so a bus full of junk can never cost the mule bank trips.
	now := time.Now()
	for _, tc := range []struct{ from, text string }{
		{"stranger", Format("Harpoon", 1)},
		{"spare28", "need 1 Harpoon; ::give coins 1"},
		// "Rune platebody" stood here as the third case until 2026-09-22 and no longer
		// belongs: DefaultPolicy now serves an uncatalogued item out of real stock, so it
		// PASSES Prescreen and is settled by the bank read in Decide, exactly as intended.
		// Replaced by a NeverOpen name, which is the case that must still be refused from
		// the message alone — its ceilings only exist on its catalogue entry, so the open
		// path must never reach a bank for it. (fixedPolicy has no "Coins" entry problem:
		// coins ARE catalogued there, which is why this uses the bare-catalogue policy.)
		{"spare28", Format("Crystal key", 1)},
	} {
		pol := fixedPolicy()
		if tc.text == Format("Crystal key", 1) {
			pol.Catalog = []Handout{{Item: "Harpoon", Max: 1}}
		}
		if d := Prescreen(now, tc.from, tc.text, pol, NewLedger(DefaultWindow)); d.Verdict != Decline {
			t.Fatalf("%+v screened as %+v", tc, d)
		}
	}
	d := Prescreen(now, "spare28", Format("Harpoon", 9), fixedPolicy(), NewLedger(DefaultWindow))
	if d.Verdict != Fulfil || d.Qty != 1 || d.Item != "Harpoon" {
		t.Fatalf("a genuine order screened as %+v", d)
	}
}

func TestDecideNeverPromisesMoreThanTheBankHolds(t *testing.T) {
	pol := fixedPolicy()
	pol.Catalog = []Handout{{Item: "Harpoon", Max: 5}}
	d := Decide(time.Now(), "spare28", Format("Harpoon", 5), pol, NewLedger(DefaultWindow), func(string) int { return 2 })
	if d.Verdict != Fulfil || d.Qty != 2 || !d.Clamped {
		t.Fatalf("decision = %+v, want a fulfil capped at the 2 actually in stock", d)
	}
}

func TestDecideReasonsFitADeclineMessage(t *testing.T) {
	// Every decline a donor can be sent has to fit mulebus.DeclinedMessage's 80-byte Reason,
	// including the two the 2026-09-22 widening added: DeclineKeptBack, and the open path's
	// own bank-read answer.
	for _, tc := range []struct{ from, text string }{
		{"stranger", Format("Harpoon", 1)},
		{"spare28", "garbage"},
		{"spare28", Format("Coins", 1)},          // on NeverOpen with no catalogue entry
		{"spare28", Format("Rune platebody", 1)}, // open path, nothing in the bank
	} {
		pol := fixedPolicy()
		pol.Catalog = []Handout{{Item: "Harpoon", Max: 1}}
		stock := alwaysStocked
		if tc.text == Format("Rune platebody", 1) {
			stock = func(string) int { return 0 }
		}
		d := Decide(time.Now(), tc.from, tc.text, pol, NewLedger(DefaultWindow), stock)
		if d.Verdict != Decline {
			t.Fatalf("%+v was not declined", tc)
		}
		if d.Reason == "" {
			t.Fatalf("%+v declined with no reason — the donor would just hang", tc)
		}
		if len(d.Reason) > 80 {
			t.Fatalf("decline reason %q is too long for mulebus.DeclinedMessage", d.Reason)
		}
	}
}

// ---- the donor's receive screen ----

func receiveView() ReceiveView {
	return ReceiveView{
		Screen:  ScreenOffer,
		Partner: "mule",
		Mule:    "mule",
		Want:    Order{Item: "Big fishing net", Qty: 1},
		Theirs:  []ItemCount{{Name: "Big fishing net", Count: 1}},
	}
}

func TestDecideReceiveAcceptsExactlyWhatWasAsked(t *testing.T) {
	act, _ := DecideReceive(receiveView())
	if act != ReceiveAccept {
		t.Fatalf("the exact requested offer was not accepted: %v", act)
	}
	v := receiveView()
	v.Screen = ScreenConfirm
	if act, _ := DecideReceive(v); act != ReceiveConfirm {
		t.Fatalf("the confirm screen was not confirmed: %v", act)
	}
	v = receiveView()
	v.Accepted = true
	if act, _ := DecideReceive(v); act != ReceiveWait {
		t.Fatalf("an already-accepted screen should wait, got %v", act)
	}
}

func TestDecideReceiveWaitsForAnOfferRatherThanDecliningAnEmptyOne(t *testing.T) {
	// The mirror of the bug mulerunner.WaitForStableOffer already fixes in the other
	// direction: the screen opens a moment before the offer lands on it.
	v := receiveView()
	v.Theirs = nil
	if act, _ := DecideReceive(v); act != ReceiveWait {
		t.Fatalf("an empty screen was not waited on: %v", act)
	}
}

func TestDecideReceiveDeclinesAnythingButTheTradeItAskedFor(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*ReceiveView)
	}{
		{"a different partner", func(v *ReceiveView) { v.Partner = "someone else" }},
		{"being asked to give something", func(v *ReceiveView) { v.Mine = []ItemCount{{Name: "Coins", Count: 500}} }},
		{"an item we did not order", func(v *ReceiveView) {
			v.Theirs = []ItemCount{{Name: "Big fishing net", Count: 1}, {Name: "Coins", Count: 1000}}
		}},
		{"more than we ordered", func(v *ReceiveView) { v.Theirs = []ItemCount{{Name: "Big fishing net", Count: 4}} }},
	} {
		v := receiveView()
		tc.mut(&v)
		act, reason := DecideReceive(v)
		if act != ReceiveDecline {
			t.Fatalf("%s was not declined: %v", tc.name, act)
		}
		if reason == "" {
			t.Fatalf("%s declined with no reason logged", tc.name)
		}
	}
}

func TestDecideReceiveMatchesTheMuleAcrossUnderscoreAndSpace(t *testing.T) {
	v := receiveView()
	v.Mule = "mule_me"
	v.Partner = "Mule Me"
	if act, reason := DecideReceive(v); act == ReceiveDecline {
		t.Fatalf("the mule's own display name was rejected: %v %s", act, reason)
	}
}

func TestDecideReceiveOnAClosedScreen(t *testing.T) {
	v := receiveView()
	v.Screen = ScreenNone
	if act, _ := DecideReceive(v); act != ReceiveClosed {
		t.Fatalf("a closed screen = %v", act)
	}
}

func TestCoinsAreHandedOutCappedPerOrderAndPerWindow(t *testing.T) {
	// 2026-09-20: a fisher short of a tool's price may ask for coins, capped at CoinsPerOrder
	// per order and CoinsPerWindow to everyone per window.
	richBank := func(string) int { return 100000 }
	pol := DefaultPolicy([]string{"spare28", "acct3"})
	led := NewLedger(DefaultWindow)
	now := time.Now()
	d := Decide(now, "spare28", Format("coins", 900), pol, led, richBank)
	if d.Verdict != Fulfil || d.Item != "Coins" || d.Qty != CoinsPerOrder || !d.Clamped {
		t.Fatalf("an order for 900 coins = %+v, want %d coins, clamped", d, CoinsPerOrder)
	}
	d = Decide(now, "acct3", Format("Coins", 500), pol, led, richBank)
	if d.Verdict != Fulfil || d.Qty != 500 || d.Clamped {
		t.Fatalf("an order for 500 coins = %+v", d)
	}
	// Not the tools' per-item cap of 10: coins have their own window ceiling.
	led.Record(now, "someone", "Coins", CoinsPerWindow-200)
	d = Decide(now, "acct3", Format("Coins", 500), pol, led, richBank)
	if d.Verdict != Fulfil || d.Qty != 200 || !d.Clamped {
		t.Fatalf("near the window ceiling = %+v, want the 200 left", d)
	}
	led.Record(now, "someone else", "Coins", 200)
	if d = Decide(now, "acct3", Format("Coins", 500), pol, led, richBank); d.Verdict != Decline {
		t.Fatalf("past the window ceiling = %+v, want decline", d)
	}
	// A tool is still capped at the policy's own per-item figure.
	led2 := NewLedger(DefaultWindow)
	led2.Record(now, "a", "Harpoon", 10)
	if d = Decide(now, "acct3", Format("Harpoon", 1), pol, led2, richBank); d.Verdict != Decline {
		t.Fatalf("an 11th harpoon in the window = %+v, want decline", d)
	}
}

// --- the batch floor (Handout.Floor) ---

// TestDecideHoldsCrystalKeysBelowTheFloor is the account owner's own rule (2026-09-20): the
// account that spends crystal keys walks Catherby -> Falador and back for one expedition, so
// the mule holds them until there is a stockpile worth the trip.
func TestDecideHoldsCrystalKeysBelowTheFloor(t *testing.T) {
	pol := DefaultPolicy([]string{"spare104"})
	for _, held := range []int{1, 6, KeysFloor - 1} {
		stock := func(string) int { return held }
		d := Decide(time.Now(), "spare104", Format("Crystal key", KeysPerOrder), pol, NewLedger(DefaultWindow), stock)
		if d.Verdict == Fulfil {
			t.Fatalf("%d banked key(s) against a floor of %d was fulfilled: %+v", held, KeysFloor, d)
		}
	}
}

// TestDecideHandsCrystalKeysOverAtTheFloor pins the other side: the floor must actually open,
// and an order must take the WHOLE stock rather than a trickle — the account owner's own ask
// (2026-09-20). Since KeysPerOrder became 500 (keys cross as bank notes, which stack), the
// rule is min(KeysPerOrder, banked), not a fixed pack: a bank holding 100 hands over all 100.
// 417 is what the mule really held while spare104 was fishing with none.
func TestDecideHandsCrystalKeysOverAtTheFloor(t *testing.T) {
	pol := DefaultPolicy([]string{"spare104"})
	for _, held := range []int{KeysFloor + 1, 417, KeysPerOrder + 250} {
		stock := func(string) int { return held }
		d := Decide(time.Now(), "spare104", Format("Crystal key", KeysPerOrder), pol, NewLedger(DefaultWindow), stock)
		if d.Verdict != Fulfil {
			t.Fatalf("%d banked key(s) at or above the floor was declined: %+v", held, d)
		}
		// THE WHOLE STOCK, not the stock minus the floor. Floor is a GATE -- Handout.Floor's
		// own doc has always said so -- and the reserve reading of it was what the fleet
		// owner hit three times: "the mule gave spare104 13 unnoted keys... the mule still has 102
		// crystal keys available", and again at 106 held where the account would have got 6.
		//
		// A mule that must always keep 100 back can never answer "give me the hundred you are
		// holding", which is the policy. Draining is still bounded, by the gate below the
		// floor and by KeysPerOrder here; an item that needs a real reserve sets
		// Handout.FloorKeepsBack, which crystal keys deliberately do not.
		want := held
		if want > KeysPerOrder {
			want = KeysPerOrder
		}
		if d.Qty != want {
			t.Fatalf("%d banked: handed %d key(s), want %d (the whole stock, capped at KeysPerOrder)", held, d.Qty, want)
		}
	}
}

// TestDecideHandsOverAllHundredAndTenKeys is the live case of 2026-09-26: the mule banked 110
// crystal keys and spare104 came away with 10. The owner: "it should have taken 110 keys". The
// mule side must fill an order for the whole 110 as asked (not clamped to 10), and a blind
// ask for KeysPerOrder must be clamped to the whole 110 too.
func TestDecideHandsOverAllHundredAndTenKeys(t *testing.T) {
	pol := DefaultPolicy([]string{"spare104"})
	stock := func(string) int { return 110 }
	d := Decide(time.Now(), "spare104", Format("Crystal key", 110), pol, NewLedger(DefaultWindow), stock)
	if d.Verdict != Fulfil || d.Qty != 110 || d.Clamped {
		t.Fatalf("an order for 110 against 110 banked = %+v, want all 110 unclamped", d)
	}
	d = Decide(time.Now(), "spare104", Format("Crystal key", KeysPerOrder), pol, NewLedger(DefaultWindow), stock)
	if d.Verdict != Fulfil || d.Qty != 110 || !d.Clamped {
		t.Fatalf("an order for %d against 110 banked = %+v, want 110 (clamped to the bank)", KeysPerOrder, d)
	}
	if !TravelsAsNotes(d.Qty, false) {
		t.Errorf("110 crystal keys must cross as bank notes (one slot); TravelsAsNotes(110) = false")
	}
}

// TestDecideFloorLeavesFloorlessItemsAlone is the control. Every other catalogue item has no
// Floor, so a bank holding exactly one of it must still hand it over — the floor must not
// become a fleet-wide "keep one in reserve" rule by accident. The Big fishing net case is the
// original incident orderplan exists for.
func TestDecideFloorLeavesFloorlessItemsAlone(t *testing.T) {
	pol := DefaultPolicy([]string{"spare28"})
	stock := func(string) int { return 1 }
	d := Decide(time.Now(), "spare28", Format("Big fishing net", 1), pol, NewLedger(DefaultWindow), stock)
	if d.Verdict != Fulfil {
		t.Fatalf("a single banked net against no floor was declined: %+v", d)
	}
}

// TestAxesJoinedTheCatalogueWithoutWeakeningIt pins the deconflict, because DefaultCatalog is
// shared and the axe rows were APPENDED to a catalogue another session had just added bounds to.
//
// The crystal key's Floor (100) and the coin WindowMax are what keep LootChestFisher's own flow
// and the fleet's bank safe. An append must not touch either, and an axe row must not quietly
// arrive with a bound of its own that it should not have.
func TestAxesJoinedTheCatalogueWithoutWeakeningIt(t *testing.T) {
	byItem := map[string]Handout{}
	for _, h := range DefaultCatalog() {
		if _, dup := byItem[h.Item]; dup {
			t.Errorf("%q is in DefaultCatalog twice — a duplicate row makes which bound applies ambiguous", h.Item)
		}
		byItem[h.Item] = h
	}

	// The bounds the other flows depend on, unchanged.
	if got := byItem["Crystal key"]; got.Floor != KeysFloor || got.Max != KeysPerOrder || got.WindowMax != KeysPerWindow {
		t.Errorf("the crystal key's bounds changed: %+v", got)
	}
	if got := byItem["Coins"]; got.Max != CoinsPerOrder || got.WindowMax != CoinsPerWindow {
		t.Errorf("the coin bounds changed: %+v", got)
	}
	// Every fishing tool still present and still one-per-order.
	for _, tool := range []string{"Small fishing net", "Big fishing net", "Fishing rod", "Fly fishing rod", "Harpoon", "Lobster pot"} {
		if got, ok := byItem[tool]; !ok || got.Max != 1 {
			t.Errorf("fishing tool %q: %+v ok=%v", tool, got, ok)
		}
	}

	// The axes: present, one per order, and with no Floor or WindowMax of their own.
	for _, a := range AxeHandouts() {
		got, ok := byItem[a.Item]
		if !ok {
			t.Errorf("%q is in AxeHandouts but not in DefaultCatalog", a.Item)
			continue
		}
		if got.Max != 1 {
			t.Errorf("%q: Max = %d, want 1 — an account needs one axe", a.Item, got.Max)
		}
		if got.Floor != 0 {
			t.Errorf("%q: Floor = %d, want 0 — a floor blocks exactly the case axes exist for", a.Item, got.Floor)
		}
	}

	// THE WEALTH DECISION, pinned so it cannot drift by accident. Adamant and rune were left out
	// while the fleet held one adamant and no rune; they joined on 2026-10-04 on the owner's word,
	// when the mule held 45 Rune axes bought for the woodcutters. Any further tier is the owner's
	// call again, not a code edit.
	// Best first, because the list doubles as a donor's fallback ladder.
	want := []string{"Rune axe", "Adamant axe", "Mithril axe", "Steel axe", "Iron axe", "Bronze axe"}
	if len(AxeHandouts()) != len(want) {
		t.Fatalf("AxeHandouts has %d tiers, want %d %v", len(AxeHandouts()), len(want), want)
	}
	for i, a := range AxeHandouts() {
		if a.Item != want[i] {
			t.Errorf("AxeHandouts[%d] = %q, want %q — the order is the ask order", i, a.Item, want[i])
		}
	}
}

// TestDeclineIsPositionalSeparatesWhereFromWhat pins the distinction a live incident turned out
// to need, and pins that the mule and the donor agree on the exact string.
//
// MEASURED on the production fleet 2026-09-21: a woodcutter asking from the Seers bank while
// the mule posts at Catherby, 83 tiles away, was told "you are not in sight of me" for one axe
// tier after another. The asking script steps DOWN its axe ladder on any Declined, so it read
// those positional failures as scarcity answers and ended up asking for the worst axe for a
// reason that had nothing to do with the mule's bank — which held Mithril x6, Bronze x15 and
// Iron x4. A decline about WHERE an account is standing says nothing about WHAT the mule has.
func TestDeclineIsPositionalSeparatesWhereFromWhat(t *testing.T) {
	if !DeclineIsPositional(DeclineNotInSight) {
		t.Fatalf("DeclineIsPositional(%q) must be true — it is the mule's own wording", DeclineNotInSight)
	}
	// Whitespace and case are not the point; the meaning is.
	for _, same := range []string{"  you are not in sight of me  ", "You Are Not In Sight Of Me"} {
		if !DeclineIsPositional(same) {
			t.Errorf("DeclineIsPositional(%q) = false", same)
		}
	}
	// Everything a mule says about STOCK OR POLICY must stay on the other side of the line, or
	// a genuinely empty tier would never step the ladder down and the account would ask for an
	// axe that is not there until it gave up.
	for _, stock := range []string{
		"I have none of those",
		"not on the allow list",
		"that is not something I hand out",
		"I have only 3 and my floor is 100",
		"",
	} {
		if DeclineIsPositional(stock) {
			t.Errorf("DeclineIsPositional(%q) = true — that is a stock or policy answer, not a positional one", stock)
		}
	}
}

// A mule funding a market PURCHASE needs a ceiling sized to the purchase. The default 500 is a
// ladder tool at stock price, and against a ~150,000 rune axe it clamps the order so the buyer
// reaches the market holding nothing -- observed on production 2026-09-21 as accounts parked on
// "waiting for the mule to bring a rune axe".
func TestWithCoinLimitsRaisesOnlyTheCoinEntry(t *testing.T) {
	cat := DefaultCatalog()
	out := WithCoinLimits(cat, 160000, 2000000)

	coins, ok := InCatalog(Policy{Catalog: out}, "Coins")
	if !ok {
		t.Fatal("Coins fell out of the catalogue")
	}
	if coins.Max != 160000 || coins.WindowMax != 2000000 {
		t.Errorf("coins = %d/%d, want 160000/2000000", coins.Max, coins.WindowMax)
	}
	// Nothing else may move: raising a coin ceiling must not quietly raise a key or axe one.
	for _, name := range []string{"Crystal key", "Mithril axe", "Big fishing net"} {
		before, _ := InCatalog(Policy{Catalog: cat}, name)
		after, okAfter := InCatalog(Policy{Catalog: out}, name)
		if !okAfter || after != before {
			t.Errorf("%s changed: %+v -> %+v", name, before, after)
		}
	}
	// And the input is not mutated, so a caller reusing DefaultCatalog() is unaffected.
	orig, _ := InCatalog(Policy{Catalog: cat}, "Coins")
	if orig.Max != CoinsPerOrder {
		t.Errorf("the source catalogue was mutated: coins Max = %d, want %d", orig.Max, CoinsPerOrder)
	}
}

// Zero or negative leaves that ceiling alone, so setting one of the pair is meaningful and an
// unconfigured mule keeps the conservative bound.
func TestWithCoinLimitsIgnoresNonPositive(t *testing.T) {
	base, _ := InCatalog(Policy{Catalog: DefaultCatalog()}, "Coins")
	for _, c := range []struct{ perOrder, perWindow int }{{0, 0}, {-1, -1}, {0, 900000}, {9000, 0}} {
		got, _ := InCatalog(Policy{Catalog: WithCoinLimits(DefaultCatalog(), c.perOrder, c.perWindow)}, "Coins")
		wantMax, wantWindow := base.Max, base.WindowMax
		if c.perOrder > 0 {
			wantMax = c.perOrder
		}
		if c.perWindow > 0 {
			wantWindow = c.perWindow
		}
		if got.Max != wantMax || got.WindowMax != wantWindow {
			t.Errorf("(%d,%d) -> %d/%d, want %d/%d", c.perOrder, c.perWindow, got.Max, got.WindowMax, wantMax, wantWindow)
		}
	}
}

// A mule deliberately configured not to hand out coins must not start doing so because someone
// raised a limit.
func TestWithCoinLimitsInventsNothing(t *testing.T) {
	cat := []Handout{{Item: "Big fishing net", Max: 1}}
	out := WithCoinLimits(cat, 160000, 2000000)
	if len(out) != 1 {
		t.Fatalf("catalogue grew to %d entries", len(out))
	}
	if _, ok := InCatalog(Policy{Catalog: out}, "Coins"); ok {
		t.Error("a Coins entry was invented for a mule that had none")
	}
}

// TestRuneAxeCoinOrderIsNotRefusedByTheGrammar is a regression test for a live failure on
// 2026-09-21: every woodcutter that tried to fund a rune axe was refused by the PARSER.
//
// The catalogue said one thing and the grammar another. The mule announced "at most 170000 per
// order" in the same run in which it answered a 150,000-coin request with "quantity is too
// large", because QtyMax was 1000 and Parse rejects before any catalogue cap is consulted. The
// account then walked off to chop with no axe money, so the rune axe users asked for could
// never be bought.
//
// Two things are asserted, and the second is the one that keeps the fix honest: the order must
// PARSE, and it must be CLAMPED to the catalogue's cap rather than fulfilled at whatever was
// asked for. If a later change makes Prescreen decline-on-over-cap instead of clamping, raising
// QtyMax would become unsafe and this test says so.
func TestRuneAxeCoinOrderIsNotRefusedByTheGrammar(t *testing.T) {
	const runeAxe = 150_000

	if _, err := Parse(Format("Coins", runeAxe)); err != nil {
		t.Fatalf("parsing a %d-coin order: %v", runeAxe, err)
	}

	pol := fixedPolicy()
	pol.Catalog = []Handout{{Item: "Coins", Max: 170_000, WindowMax: 1_000_000}}
	// A mule with real coin stock: the live one held 29,837,076 when this was written, and
	// alwaysStocked's 5 would clamp the answer long before the catalogue cap was reached.
	richMule := func(string) int { return 30_000_000 }
	d := Decide(time.Now(), "spare28", Format("Coins", runeAxe), pol, NewLedger(DefaultWindow), richMule)
	if d.Verdict != Fulfil {
		t.Fatalf("a %d-coin order was %v (%s), want Fulfil", runeAxe, d.Verdict, d.Reason)
	}
	if d.Qty != runeAxe {
		t.Errorf("handed %d coins, want the %d asked for", d.Qty, runeAxe)
	}

	// Over the catalogue cap must CLAMP, never over-hand -- this is what makes a large QtyMax safe.
	over := Decide(time.Now(), "spare28", Format("Coins", 900_000), pol, NewLedger(DefaultWindow), richMule)
	if over.Verdict != Fulfil || over.Qty != 170_000 || !over.Clamped {
		t.Errorf("a 900000-coin order = %v qty %d clamped %v, want Fulfil clamped to 170000",
			over.Verdict, over.Qty, over.Clamped)
	}

	// And an absurd quantity must still be refused by the grammar.
	if _, err := Parse(Format("Coins", QtyMax+1)); err != ErrQtyTooLarge {
		t.Errorf("parsing QtyMax+1 = %v, want ErrQtyTooLarge", err)
	}
}

// The gate still gates, and FloorKeepsBack still reserves. Both halves matter: the owner's
// policy needs the whole stock handed over, and the 418-in-one-order incident the reserve was
// written for needs an item to still be able to opt into the old behaviour.
func TestAFloorGatesByDefaultAndReservesOnlyWhenAsked(t *testing.T) {
	pol := DefaultPolicy([]string{"spare104"})

	// Below and AT the floor: nothing crosses. An order of zero is a decline, not a fulfil.
	for _, held := range []int{0, 1, KeysFloor - 1, KeysFloor} {
		stock := func(string) int { return held }
		d := Decide(time.Now(), "spare104", Format("Crystal key", KeysPerOrder), pol, NewLedger(DefaultWindow), stock)
		if d.Verdict == Fulfil {
			t.Errorf("%d banked (floor %d) was fulfilled — the gate must hold until the stock is worth a trip", held, KeysFloor)
		}
	}

	// Opting in restores the reserve exactly as it was.
	reserved := DefaultPolicy([]string{"spare104"})
	for i := range reserved.Catalog {
		if reserved.Catalog[i].Item == "Crystal key" {
			reserved.Catalog[i].FloorKeepsBack = true
		}
	}
	stock := func(string) int { return 113 }
	d := Decide(time.Now(), "spare104", Format("Crystal key", KeysPerOrder), reserved, NewLedger(DefaultWindow), stock)
	if d.Verdict != Fulfil || d.Qty != 13 {
		t.Errorf("with FloorKeepsBack: verdict %v qty %d, want Fulfil of 13 (113 - the 100 floor)", d.Verdict, d.Qty)
	}
}

// Content's charged and dosed names carry parentheses, and an order for one parses: the run
// staller's "Games necklace(8)" was declined as malformed (live 6 Oct 2026).
func TestParseTakesChargedNames(t *testing.T) {
	for _, name := range []string{"Games necklace(8)", "Ring of dueling(1)", "Waterskin(4)"} {
		o, err := Parse(Format(name, 1))
		if err != nil || o.Item != name || o.Qty != 1 {
			t.Errorf("Parse(%q) = %+v, %v", Format(name, 1), o, err)
		}
	}
	if _, err := Parse("need 1 Games necklace{8}"); err != ErrItemChars {
		t.Errorf("braces still refused: %v", err)
	}
}
