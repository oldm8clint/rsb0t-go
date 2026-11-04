package donorplan

import (
	"testing"
	"time"
)

// TestClassifyRefusalSeparatesStockAnswersFromEverythingElse is the whole safety property of
// this file in one table. Remembering the wrong kind of decline is not a lost optimisation, it
// is a lie the account then acts on for half an hour.
//
// Every string below is a real orderplan reason, quoted from orderplan.Decide/Prescreen, not an
// invented one — if one of them is reworded there, this test is what notices.
func TestClassifyRefusalSeparatesStockAnswersFromEverythingElse(t *testing.T) {
	for _, tc := range []struct {
		reason string
		want   RefusalKind
		why    string
	}{
		{"none in the bank", RefusalNoStock,
			"orderplan.Decide's own wording after it has really read the bank — the one answer that is definitely about the item"},
		{"only 46 Crystal key banked, holding until 100", RefusalHeldBack,
			"the batch floor: the mule has some and is keeping them, which is just as much a reason not to ask again"},
		{"I keep that one back", RefusalHeldBack,
			"orderplan.DeclineKeptBack, for an item on OpenStock.Never — a standing policy, not a moment"},
		{"not an item I hand out", RefusalNotOffered,
			"the mule's policy has no room for it at all"},
		{"hand-out limit for that item is spent", RefusalNotOffered,
			"the per-window ceiling for this item is gone, and the ceiling's window is the memory's own cooldown"},

		// The ones that must NEVER be remembered, and the live incidents behind them.
		{"you are not in sight of me", RefusalNotAboutTheItem,
			"measured 2026-09-21: a cohort read four POSITIONAL refusals as four scarcity answers and settled for the worst axe. It says nothing about stock"},
		{"already handed you one recently", RefusalNotAboutTheItem,
			"this donor's own per-window cap. Remembering it would lock 268 other accounts out over one account's quota"},
		{"not on the allow list", RefusalNotAboutTheItem,
			"about the asker's name"},
		{"hand-outs are switched off", RefusalNotAboutTheItem,
			"about the mule, and it changes with a setting rather than with stock"},
		{"cannot read my own stock", RefusalNotAboutTheItem,
			"the mule admitting it does not know — the opposite of an answer"},
		{"the trade closed before the confirm screen", RefusalNotAboutTheItem,
			"a mechanical failure mid-trade; the item was there"},
		{"", RefusalNotAboutTheItem, "a silence is not an answer"},
		{"some wording nobody has written yet", RefusalNotAboutTheItem,
			"an unrecognised reason must fall back to blind asking, never to a guess"},
	} {
		if got := ClassifyRefusal(tc.reason); got != tc.want {
			t.Errorf("ClassifyRefusal(%q) = %v, want %v — %s", tc.reason, got, tc.want, tc.why)
		}
	}
}

// TestClassifyRefusalSurvivesTruncation pins the substring matching. A reason travels as
// mulebus.DeclinedMessage.Reason and is truncated at mulebus.ReasonMax (80 bytes), so a long
// one can arrive with its tail missing; equality matching would have read a truncated batch-floor
// decline as an unknown reason and gone back to asking blind.
func TestClassifyRefusalSurvivesTruncation(t *testing.T) {
	full := "only 46 Crystal key banked, holding until 100"
	if got := ClassifyRefusal(full[:len(full)-4]); got != RefusalHeldBack {
		t.Errorf("a truncated batch-floor decline classified as %v, want RefusalHeldBack", got)
	}
	if got := ClassifyRefusal("NONE IN THE BANK"); got != RefusalNoStock {
		t.Errorf("case-folded reason classified as %v — a reason is free text and casing is not a contract", got)
	}
}

// TestRefusalMemoryStopsTheLoopTheOwnerReported is the fleet owner's 2026-09-22 sentence turned
// into an assertion: "accounts are still requesting items the mule does not have".
//
// The measured loop it stands for: a woodcutter walked 83 tiles to Catherby, asked for a Mithril
// axe, was told "none in the bank" by a mule whose only chopping axe is an Adamant axe, threw
// the answer away and asked again two minutes later, forever.
func TestRefusalMemoryStopsTheLoopTheOwnerReported(t *testing.T) {
	m := NewRefusalMemory()
	now := time.Now()

	if _, _, refused := m.Refused(now, "mule", "Mithril axe"); refused {
		t.Fatal("an empty memory refused an ask — the first attempt must always go through")
	}
	if !m.Record(now, "mule", "Mithril axe", "none in the bank", RefusalCooldown) {
		t.Fatal("a 'none in the bank' decline was not remembered, so the loop is unchanged")
	}

	reason, age, refused := m.Refused(now.Add(2*time.Minute), "mule", "Mithril axe")
	if !refused {
		t.Fatal("the same ask two minutes later was not suppressed — two minutes is exactly the live retry interval that produced the loop")
	}
	if reason != "none in the bank" {
		t.Errorf("reason = %q, want the mule's own words verbatim: callers PARSE it (woodcutplan.MuleDeclineIsEmptyStock, lootchestfisherplan.ParseBankedCount), so rewording it would break them", reason)
	}
	if age != 2*time.Minute {
		t.Errorf("age = %v, want 2m so a log line can say how old the answer is", age)
	}

	// Bounded, in both directions.
	if _, _, refused := m.Refused(now.Add(RefusalCooldown+time.Second), "mule", "Mithril axe"); refused {
		t.Error("the memory did not lapse — a mule that has restocked must not be shut out by an answer it gave half an hour ago")
	}
}

// TestRefusalMemoryIgnoresAnswersThatAreNotAboutTheItem is the other half, and the one that
// actually protects the fleet: Record is handed every decline by donorrunner.Await, so it has to
// throw the wrong ones away itself rather than trusting the caller to.
func TestRefusalMemoryIgnoresAnswersThatAreNotAboutTheItem(t *testing.T) {
	m := NewRefusalMemory()
	now := time.Now()
	for _, reason := range []string{
		"you are not in sight of me",
		"already handed you one recently",
		"cannot read my own stock",
		"",
	} {
		if m.Record(now, "mule", "Big fishing net", reason, RefusalCooldown) {
			t.Errorf("%q was remembered as an item fact", reason)
		}
		if _, _, refused := m.Refused(now, "mule", "Big fishing net"); refused {
			t.Fatalf("after %q the next ask was suppressed — this is the exact mistake that made a cohort settle for the worst axe for a reason that had nothing to do with stock", reason)
		}
	}
}

// TestRefusalMemoryIsPerMuleAndPerItem: two mules are two banks, and an answer about one item
// says nothing about another. Names fold the way every other name in this repo folds, because an
// account's login username uses an underscore where its display name shows a space — the exact
// class of bug mulerunner.normalisePlayerName was added to fix live.
func TestRefusalMemoryIsPerMuleAndPerItem(t *testing.T) {
	m := NewRefusalMemory()
	now := time.Now()
	m.Record(now, "mule", "Mithril axe", "none in the bank", RefusalCooldown)

	if _, _, refused := m.Refused(now, "mule_two", "Mithril axe"); refused {
		t.Error("a second mule was tarred with the first mule's answer — they have different banks")
	}
	if _, _, refused := m.Refused(now, "mule", "Adamant axe"); refused {
		t.Error("a different item was suppressed by an answer about Mithril — the live mule holds an Adamant axe and no mithril, so this would suppress the one ask that works")
	}
	for _, spelling := range []string{"Mule", " mule ", "MULE"} {
		if _, _, refused := m.Refused(now, spelling, "mithril AXE"); !refused {
			t.Errorf("%q did not match the remembered refusal for mule", spelling)
		}
	}

	// And the underscore/space fold itself, which is the live bug class: an account's login
	// username uses an underscore where its in-game display name shows a space, and both
	// spellings reach this code from different places (settings vs Players()).
	m.Record(now, "big_net", "Harpoon", "none in the bank", RefusalCooldown)
	if _, _, refused := m.Refused(now, "Big Net", "Harpoon"); !refused {
		t.Error(`"Big Net" did not match a refusal recorded under "big_net" — mulerunner.normalisePlayerName folds these two together and so must this`)
	}
}

// TestRefusalMemoryYieldsToEvidence. A delivery is proof; a remembered decline is not. Without
// this, a mule that was restocked (restockplan buys axes) would be shut out for the rest of the
// cooldown by an answer that is no longer true.
func TestRefusalMemoryYieldsToEvidence(t *testing.T) {
	m := NewRefusalMemory()
	now := time.Now()
	m.Record(now, "mule", "Mithril axe", "none in the bank", RefusalCooldown)
	m.Clear("MULE", "mithril axe") // folded the same way, from a caller's own spelling
	if _, _, refused := m.Refused(now, "mule", "Mithril axe"); refused {
		t.Error("Clear did not forget a refusal that a completed hand-out had just disproved")
	}

	m.Record(now, "mule", "Shark", "none in the bank", RefusalCooldown)
	m.Record(now, "mule", "Coins", "I keep that one back", RefusalCooldown)
	m.Forget("mule")
	if n := m.Count(now); n != 0 {
		t.Errorf("Forget left %d entries for the mule it was told to forget", n)
	}
}

// TestRefusalMemoryIsSafeOnANilReceiver, because donorrunner.AwaitOptions.Memory is an optional
// override and a test or a caller may leave it nil. Nil must behave as "no memory at all", which
// is the pre-2026-09-22 behaviour, never a panic in the middle of a live fleet.
func TestRefusalMemoryIsSafeOnANilReceiver(t *testing.T) {
	var m *RefusalMemory
	now := time.Now()
	if m.Record(now, "mule", "Shark", "none in the bank", RefusalCooldown) {
		t.Error("a nil memory claimed to remember something")
	}
	if _, _, refused := m.Refused(now, "mule", "Shark"); refused {
		t.Error("a nil memory suppressed an ask")
	}
	m.Clear("mule", "Shark")
	m.Forget("mule")
	m.Sweep(now)
	if m.Count(now) != 0 {
		t.Error("a nil memory counted something")
	}

	// And the zero value works too, so a struct field needs no constructor.
	var z RefusalMemory
	z.Record(now, "mule", "Shark", "none in the bank", RefusalCooldown)
	if _, _, refused := z.Refused(now, "mule", "Shark"); !refused {
		t.Error("the zero value did not remember")
	}
}

// TestRefusalCooldownMatchesTheMulesOwnWindow. RefusalCooldown is deliberately the same period
// orderplan measures its caps over: a re-ask inside it cannot produce an answer the previous one
// did not already have. The constant is written out rather than imported because donorplan keeps
// no dependencies, so this is what keeps the two from drifting apart silently.
func TestRefusalCooldownMatchesTheMulesOwnWindow(t *testing.T) {
	const orderplanDefaultWindow = 30 * time.Minute // orderplan.DefaultWindow
	if RefusalCooldown != orderplanDefaultWindow {
		t.Errorf("RefusalCooldown = %v but orderplan.DefaultWindow is %v — asking again inside the mule's own cap window cannot produce a new answer, and asking again outside it should always be allowed",
			RefusalCooldown, orderplanDefaultWindow)
	}
}

// TestRefusalMemorySweepDropsLapsedEntries. A fleet process runs for days; without this the map
// grows one entry per (mule, item) ever refused and never shrinks.
func TestRefusalMemorySweepDropsLapsedEntries(t *testing.T) {
	m := NewRefusalMemory()
	now := time.Now()
	m.Record(now, "mule", "Mithril axe", "none in the bank", time.Minute)
	m.Record(now, "mule", "Steel axe", "none in the bank", time.Hour)
	m.Sweep(now.Add(2 * time.Minute))
	if n := m.Count(now.Add(2 * time.Minute)); n != 1 {
		t.Errorf("Count = %d after a sweep, want 1: the minute-old entry lapsed and the hour-long one did not", n)
	}
}
