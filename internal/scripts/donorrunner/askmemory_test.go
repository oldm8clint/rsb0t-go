package donorrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/donorplan"
)

// THE FLEET OWNER'S REPORT, 2026-09-22: "accounts are still requesting items the mule does not
// have". These pin the half of that sentence that lives on the ASKING side.
//
// Await is the single point every ask in this repo goes through — ProgressiveFisher,
// LootChestFisher, ScarabSwarm, ProgressiveWoodcutter and peerrunner all call it — so the memory
// sits here rather than in each of them, and these tests are what keep it from changing what a
// caller sees.

// TestAwaitDoesNotAskForWhatTheMuleJustSaidItHasNot is the loop-breaker.
//
// The measured loop, live 2026-09-21/22: a woodcutter walked 83 tiles to Catherby, asked for a
// Mithril axe, was told "none in the bank" by a mule whose ONLY chopping axe is an Adamant axe,
// discarded the answer, and asked again two minutes later — forever, at zero experience.
//
// The Deps here are deliberately empty: no session, no object config, no walker. That is the
// point of the check running before any of them is touched — a suppressed ask must cost a map
// read, not a bank trip, a trade wait or the 90-second budget.
func TestAwaitDoesNotAskForWhatTheMuleJustSaidItHasNot(t *testing.T) {
	mem := donorplan.NewRefusalMemory()
	mem.Record(time.Now(), "mule", "Mithril axe", "none in the bank", donorplan.RefusalCooldown)

	var logged []string
	d := &Deps{Log: func(s string) { logged = append(logged, s) }}
	res := Await(d, AwaitOptions{
		Mule: "mule", Me: "acct134", Item: "Mithril axe", Qty: 1,
		Bus: mulebus.NewBus(), Memory: mem,
	})

	if !res.Declined {
		t.Fatalf("result = %+v, want Declined: the mule has already answered this exact question and the answer cannot change with the quantity asked for", res)
	}
	if res.Reason != "none in the bank" {
		t.Errorf("Reason = %q, want the mule's OWN words verbatim — callers parse them (woodcutplan.MuleDeclineIsEmptyStock reads this one, lootchestfisherplan.ParseBankedCount reads the batch-floor one), so a reworded replay would break them", res.Reason)
	}
	if res.Got || res.Stopped {
		t.Errorf("result = %+v, want a plain decline and nothing else", res)
	}
	if len(logged) == 0 {
		t.Error("nothing was logged: a suppressed ask must be visible, or the next person debugging a quiet account has no way to tell it from an account that never asked")
	}
}

// TestAwaitStillAsksForWhatTheMuleActuallyHolds is the control, and it is the case the whole
// change exists to serve. The live mule holds ONE Adamant axe and no mithril; a memory of the
// mithril answer must not suppress the adamant ask.
//
// It gets as far as the object-config lookup (nil Config here, so "I cannot identify"), which is
// exactly the proof wanted: the memory did not short-circuit it, and the result is NOT Declined.
func TestAwaitStillAsksForWhatTheMuleActuallyHolds(t *testing.T) {
	mem := donorplan.NewRefusalMemory()
	mem.Record(time.Now(), "mule", "Mithril axe", "none in the bank", donorplan.RefusalCooldown)

	res := Await(&Deps{}, AwaitOptions{
		Mule: "mule", Me: "acct134", Item: "Adamant axe", Qty: 1,
		Bus: mulebus.NewBus(), Memory: mem,
	})
	if res.Declined {
		t.Fatalf("the Adamant axe ask was suppressed by an answer about a Mithril axe: %+v", res)
	}
}

// TestAwaitIgnoresARefusalThatIsNotAboutTheItem. The live incident: a cohort asking from the
// Seers bank while the mule posts at Catherby, 83 tiles away, was told "you are not in sight of
// me" for every tier in turn. That says nothing whatever about stock, and remembering it would
// stop the account ever asking again from a position where it WOULD work.
func TestAwaitIgnoresARefusalThatIsNotAboutTheItem(t *testing.T) {
	mem := donorplan.NewRefusalMemory()
	mem.Record(time.Now(), "mule", "Big fishing net", "you are not in sight of me", donorplan.RefusalCooldown)

	res := Await(&Deps{}, AwaitOptions{
		Mule: "mule", Me: "spare28", Item: "Big fishing net", Qty: 1,
		Bus: mulebus.NewBus(), Memory: mem,
	})
	if res.Declined {
		t.Fatalf("a positional refusal suppressed a later ask: %+v — the account only had to move", res)
	}
}

// TestAwaitAsksAnywayWhenTheCallerHasALiveReading. peerrunner.Ask chooses its counterparty out
// of the peerbank directory precisely BECAUSE that account published the item, and it has
// already walked to it by the time Await runs. A half-hour-old refusal must not veto a fresh
// reading plus a completed trip.
func TestAwaitAsksAnywayWhenTheCallerHasALiveReading(t *testing.T) {
	mem := donorplan.NewRefusalMemory()
	mem.Record(time.Now(), "acct12", "Bronze axe", "none in the bank", donorplan.RefusalCooldown)

	res := Await(&Deps{}, AwaitOptions{
		Mule: "acct12", Me: "acct134", Item: "Bronze axe", Qty: 1,
		Bus: mulebus.NewBus(), Memory: mem, AskAnyway: true,
	})
	if res.Declined {
		t.Fatalf("AskAnyway did not bypass the memory: %+v", res)
	}
}

// TestAwaitBehavesAsBeforeWithNothingRemembered is the additive check. An existing caller passes
// no Memory and no AskAnyway; with an empty shared memory it must reach exactly the code it
// always did, and the early guards must still answer in the same order.
func TestAwaitBehavesAsBeforeWithNothingRemembered(t *testing.T) {
	// Package-level Refusals is shared; use a scratch one so this test cannot be perturbed by
	// another, and assert the DEFAULT (nil Memory) path separately below.
	for _, tc := range []struct {
		what string
		o    AwaitOptions
		want string
	}{
		{"no bus", AwaitOptions{Mule: "mule", Me: "me", Item: "Harpoon"}, "no wall bus configured"},
		{"no mule", AwaitOptions{Me: "me", Item: "Harpoon", Bus: mulebus.NewBus()}, "nothing to ask for"},
		{"no item", AwaitOptions{Mule: "mule", Me: "me", Bus: mulebus.NewBus()}, "nothing to ask for"},
	} {
		if res := Await(&Deps{}, tc.o); res.Reason != tc.want {
			t.Errorf("%s: Reason = %q, want %q — these guards run before the memory and must be unchanged", tc.what, res.Reason, tc.want)
		}
	}

	// The default path: nil Memory falls back to the shared Refusals, which knows nothing about
	// this item, so the ask proceeds and is NOT declined.
	res := Await(&Deps{}, AwaitOptions{
		Mule: "mule", Me: "me", Item: "An item nobody has ever asked about", Qty: 1, Bus: mulebus.NewBus(),
	})
	if res.Declined {
		t.Errorf("an unremembered item was declined from the shared memory: %+v", res)
	}
}
