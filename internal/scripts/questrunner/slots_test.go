package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

func TestExclusiveQuestSlotsAdmitOneAccountAtATime(t *testing.T) {
	a, b := &Deps{}, &Deps{}
	now := time.Now()
	id := questplan.WitchsHouseID
	defer releaseQuest(id, a)
	defer releaseQuest(id, b)
	if !tryHoldQuest(id, a, now) {
		t.Fatal("the first account could not take a free slot")
	}
	if tryHoldQuest(id, b, now) {
		t.Fatal("a second account took Witch's House while the first held it")
	}
	if !tryHoldQuest(id, a, now) {
		t.Fatal("the holder could not re-take its own slot")
	}
	if !tryHoldQuest(id, b, now.Add(slotStale+time.Minute)) {
		t.Fatal("a stale slot was never freed")
	}
	releaseQuest(id, b)
	if !tryHoldQuest(id, a, now) {
		t.Fatal("a released slot stayed taken")
	}
	// Control: a quest with no one-at-a-time step never blocks.
	if !tryHoldQuest(questplan.VampireSlayerID, a, now) || !tryHoldQuest(questplan.VampireSlayerID, b, now) {
		t.Fatal("a non-exclusive quest blocked a second account")
	}
}

func TestWaitingOnSlotIsAWaitNotAFailure(t *testing.T) {
	busy := LadderResult{Outcome: LadderStuck, Blocked: map[string][]string{"witch": {busyReason}, "grail": {"needs 20 Smithing"}}}
	if !WaitingOnSlot(busy) {
		t.Fatal("a ladder held up by a busy slot was not a wait")
	}
	stuck := LadderResult{Outcome: LadderStuck, Blocked: map[string][]string{"grail": {"needs 20 Smithing"}}}
	if WaitingOnSlot(stuck) {
		t.Fatal("a ladder blocked only by requirements was read as waiting")
	}
}

// Rune Mysteries admits twelve accounts at once (it admitted four while the Wizards' Tower door
// looked like the accounts' doing, 3 Oct; the door was a walker bug, fixed 7 Oct, and the four slots
// were the queue's bottleneck on 14 Oct): the thirteenth waits, and a release lets it in.
func TestRuneMysteriesAdmitsTwelveAtOnce(t *testing.T) {
	id := questplan.RuneMysteriesID
	const twelve = 12
	if got := capacityOf(id); got != twelve {
		t.Fatalf("Rune Mysteries admits %d at once, want %d", got, twelve)
	}
	now := time.Now()
	ds := make([]*Deps, twelve+2)
	for i := range ds {
		ds[i] = &Deps{}
		defer releaseQuest(id, ds[i])
	}
	for i := 0; i < twelve; i++ {
		if !tryHoldQuest(id, ds[i], now) {
			t.Fatalf("holder %d refused", i)
		}
	}
	if tryHoldQuest(id, ds[twelve], now) {
		t.Fatal("a thirteenth account must wait")
	}
	if !tryHoldQuest(id, ds[0], now) {
		t.Fatal("a holder re-claiming its own slot must succeed")
	}
	releaseQuest(id, ds[1])
	if !tryHoldQuest(id, ds[twelve], now) {
		t.Fatal("a release must let the next account in")
	}
	if tryHoldQuest(id, ds[twelve+1], now) {
		t.Fatal("still twelve holders")
	}
}

// The other crowded quests keep their one slot: only Rune Mysteries was ever widened.
func TestTheOtherCrowdedQuestsKeepOneSlot(t *testing.T) {
	for _, id := range []string{questplan.WitchsHouseID, questplan.PriestInPerilID, questplan.HolyGrailID} {
		if got := capacityOf(id); got != 1 {
			t.Errorf("%s admits %d at once, want 1", id, got)
		}
	}
}

// The Restless Ghost's skull is one ground spawn: eight accounts run the quest at once, the ninth waits.
func TestTheRestlessGhostAdmitsEightAtOnce(t *testing.T) {
	id := questplan.RestlessGhostID
	if !exclusiveQuests[id] || capacityOf(id) != 8 {
		t.Fatalf("ghost exclusive %v capacity %d, want true and 8", exclusiveQuests[id], capacityOf(id))
	}
	now := time.Now()
	ds := make([]*Deps, 9)
	for i := range ds {
		ds[i] = &Deps{}
		defer releaseQuest(id, ds[i])
	}
	for i := 0; i < 8; i++ {
		if !tryHoldQuest(id, ds[i], now) {
			t.Fatalf("holder %d refused", i)
		}
	}
	if tryHoldQuest(id, ds[8], now) {
		t.Fatal("a ninth account must wait")
	}
	if questplan.RestlessGhost.ExclusiveDone == nil {
		t.Fatal("the module must say when the account is past the skull, or the slot is held for the whole run")
	}
}

// Imp Catcher and Cook's Assistant, promoted on 16 Oct 2026, run under slots: their beads and their egg, bucket and pot
// are shared spawns every proven-only account would reach at once.
func TestThePromotedSharedSpawnQuestsRunUnderSlots(t *testing.T) {
	for id, want := range map[string]int{questplan.ImpCatcherID: 12, questplan.CooksAssistantID: 6} {
		if !exclusiveQuests[id] || capacityOf(id) != want {
			t.Errorf("%s: exclusive %v capacity %d, want true and %d", id, exclusiveQuests[id], capacityOf(id), want)
		}
	}
}

// Imp Catcher admits twelve accounts and farms two fields: each holder is given the field the fewest accounts per spawn
// work, keeps it for as long as it holds the slot, and a release frees its place at that field for the next to come.
func TestSlotSiteSpreadsHoldersOverTheSitesAndKeepsEachOnesChoice(t *testing.T) {
	id := questplan.ImpCatcherID
	now := time.Now()
	const n = 12
	ds := make([]*Deps, n+1)
	for i := range ds {
		ds[i] = &Deps{}
		defer releaseQuest(id, ds[i])
	}
	pick := func(farmers map[string]int) string { return questplan.PickImpSite(farmers, tile.Tile{}, false).ID }
	got := map[string]int{}
	chosen := make([]string, n)
	for i := 0; i < n; i++ {
		if !tryHoldQuest(id, ds[i], now) {
			t.Fatalf("holder %d refused", i)
		}
		chosen[i] = SlotSite(id, ds[i], now, pick)
		got[chosen[i]]++
	}
	if got[questplan.ImpSiteArdougne.ID] != 6 || got[questplan.ImpSiteYanille.ID] != 6 {
		t.Fatalf("twelve holders over the sites: %v, want six and six", got)
	}
	// A holder asking again is told what it chose, whatever the others did since.
	for i := 0; i < n; i++ {
		if again := SlotSite(id, ds[i], now, func(map[string]int) string { return "elsewhere" }); again != chosen[i] {
			t.Errorf("holder %d chose %s and was then told %s", i, chosen[i], again)
		}
	}
	if f := SlotFarmers(id, now); f[questplan.ImpSiteArdougne.ID] != 6 || f[questplan.ImpSiteYanille.ID] != 6 {
		t.Errorf("SlotFarmers = %v", f)
	}
	// Re-taking the slot (the ladder does it each pass) keeps the site.
	if !tryHoldQuest(id, ds[0], now.Add(time.Minute)) || SlotSite(id, ds[0], now, func(map[string]int) string { return "elsewhere" }) != chosen[0] {
		t.Error("a holder that took its slot again lost its site")
	}
	// A release frees the place at the site: the thirteenth, let in, is sent where the room is.
	freed := chosen[3]
	releaseQuest(id, ds[3])
	if !tryHoldQuest(id, ds[n], now) {
		t.Fatal("the released slot was not free")
	}
	var seen map[string]int
	if site := SlotSite(id, ds[n], now, func(f map[string]int) string { seen = f; return pick(f) }); site != freed {
		t.Errorf("the newcomer went to %s, want %s, whose place was freed (others working: %v)", site, freed, seen)
	}
}

// A holder that has gone stale (its run died with its Deps) does not count as a farmer, and an account with no slot is
// answered for an empty table and remembered nowhere.
func TestSlotSiteIgnoresStaleHoldersAndUnslottedAccounts(t *testing.T) {
	id := questplan.ImpCatcherID
	now := time.Now()
	old, fresh, outsider := &Deps{}, &Deps{}, &Deps{}
	defer releaseQuest(id, old)
	defer releaseQuest(id, fresh)
	tryHoldQuest(id, fresh, now)
	// Taken 46 minutes ago as far as the table can tell (its clock is the caller's), and never released.
	tryHoldQuest(id, old, now.Add(-slotStale-time.Minute))
	SlotSite(id, old, now.Add(-slotStale-time.Minute), func(map[string]int) string { return questplan.ImpSiteYanille.ID })
	var seen map[string]int
	SlotSite(id, fresh, now, func(f map[string]int) string { seen = f; return questplan.ImpSiteArdougne.ID })
	if len(seen) != 0 {
		t.Errorf("a stale holder was counted: %v", seen)
	}
	if f := SlotFarmers(id, now); f[questplan.ImpSiteYanille.ID] != 0 || f[questplan.ImpSiteArdougne.ID] != 1 {
		t.Errorf("SlotFarmers counted a stale holder: %v", f)
	}
	calls := 0
	for i := 0; i < 2; i++ {
		SlotSite(id, outsider, now, func(map[string]int) string { calls++; return questplan.ImpSiteYanille.ID })
	}
	if calls != 2 {
		t.Errorf("an account holding no slot had its choice remembered (%d calls to choose, want 2)", calls)
	}
}

// Pirate's Treasure (W13 part C, promoted 18 Oct 2026) runs under a slot, so that its crowd does not empty Zambo's rum shelf of
// three: eight at a time, the shelf's refill rate over a 45 minute run.
func TestPiratesTreasureRunsUnderEightSlots(t *testing.T) {
	if !exclusiveQuests[questplan.PiratesTreasureID] || capacityOf(questplan.PiratesTreasureID) != 8 {
		t.Errorf("exclusive %v capacity %d, want true and 8", exclusiveQuests[questplan.PiratesTreasureID], capacityOf(questplan.PiratesTreasureID))
	}
	// A quest slot is held at most slotStale (90 minutes): the quest's own slot is under it, or a live holder would be dropped.
	if got := questplan.QuestSlot(questplan.PiratesTreasureID, 20*time.Minute); got >= slotStale {
		t.Errorf("the quest's slot %s outlasts the stale limit %s", got, slotStale)
	}
}

// Monk's Friend, Demon Slayer, Druidic Ritual and Jungle Potion were promoted on live evidence on 19 Oct 2026 and kept their slots
// (4, 3, 4 and 3: the tests of each quest pin them). A slot is held up to slotStale, and the promoted quests' own slots are under it, or a
// live holder would be dropped mid-run and a second account let into a crowded step. An hour no longer was enough: Druidic Ritual's slot is
// 62m30s (a 50 minute estimate) and Jungle Potion's, held for the whole run, 75m (60 minutes), which made slotStale 90 minutes.
func TestThePromotedOn19OctSlotsAreUnderTheStaleLimit(t *testing.T) {
	for id, want := range map[string]time.Duration{
		questplan.MonksFriendID:   37*time.Minute + 30*time.Second,
		questplan.DemonSlayerID:   56*time.Minute + 15*time.Second,
		questplan.DruidicRitualID: 62*time.Minute + 30*time.Second,
		questplan.JunglePotionID:  75 * time.Minute,
	} {
		if !exclusiveQuests[id] {
			t.Errorf("%s has no slot", id)
		}
		got := questplan.QuestSlot(id, 20*time.Minute)
		if got != want {
			t.Errorf("%s: slot %s, want %s (its estimate and a quarter)", id, got, want)
		}
		if got >= slotStale {
			t.Errorf("%s: slot %s reaches the stale limit %s", id, got, slotStale)
		}
	}
}

// Sheep Shearer, Goblin Diplomacy and Romeo & Juliet, promoted on 17 Oct 2026, run under slots: shears and sheep, the
// village goblins and the cadava bush are shared by every proven-only account.
func TestTheFillersPromotedOn17OctRunUnderSlots(t *testing.T) {
	for id, want := range map[string]int{questplan.SheepShearerID: 8, questplan.GoblinDiplomacyID: 12, questplan.RomeoJulietID: 12} {
		if !exclusiveQuests[id] || capacityOf(id) != want {
			t.Errorf("%s: exclusive %v capacity %d, want true and %d", id, exclusiveQuests[id], capacityOf(id), want)
		}
	}
}

// SlotsFree looks at the table without taking a slot: a quest with no slot is always free, a full table is not, the account's
// own slot is, a stale holder is no holder, and a release frees it. Looking changes nothing.
func TestSlotsFreeLooksWithoutTaking(t *testing.T) {
	id := questplan.GoblinDiplomacyID
	n := capacityOf(id)
	now := time.Now()
	ds := make([]*Deps, n+2)
	for i := range ds {
		ds[i] = &Deps{}
		defer releaseQuest(id, ds[i])
	}
	waiter := ds[n+1]
	if !SlotsFree(waiter, []string{id}, now) {
		t.Fatal("an empty table reads as full")
	}
	for i := 0; i < n; i++ {
		if !tryHoldQuest(id, ds[i], now) {
			t.Fatalf("holder %d refused", i)
		}
	}
	if SlotsFree(waiter, []string{id}, now) {
		t.Fatal("a full table reads as free")
	}
	if !SlotsFree(ds[0], []string{id}, now) {
		t.Error("a holder's own slot does not read as free to it")
	}
	if !SlotsFree(waiter, []string{id, questplan.VampireSlayerID}, now) {
		t.Error("a quest with no slot was not free")
	}
	if SlotsFree(waiter, nil, now) {
		t.Error("no quests asked about read as free")
	}
	if !SlotsFree(waiter, []string{id}, now.Add(slotStale+time.Minute)) {
		t.Error("holders older than slotStale still count")
	}
	releaseQuest(id, ds[3])
	if !SlotsFree(waiter, []string{id}, now) {
		t.Error("a released slot does not read as free")
	}
	// Looking took nothing: all but one of the holders are still in, and the waiter holds none.
	if !tryHoldQuest(id, ds[n], now) {
		t.Error("the free slot was gone after a look")
	}
	if tryHoldQuest(id, waiter, now) {
		t.Error("the looks took a slot for the waiter")
	}
}

// A pass held up by slots says which quests it waits on, and one held up by a park says when the park ends: what the quest
// hold retries by.
func TestRunLadderReportsTheSlotsAndTheParksItWaitsOn(t *testing.T) {
	holder := &Deps{}
	busy := questplan.WitchsHouseID
	if !tryHoldQuest(busy, holder, time.Now()) {
		t.Fatal("the test could not take the slot")
	}
	defer releaseQuest(busy, holder)
	e := fakeEngine(&logSink{})
	e.NarrowSlots = true
	until := time.Now().Add(7 * time.Minute)
	e.Store.Record("grail").ParkedReason = "no progress"
	e.Store.Record("grail").BackoffUntil = until.UnixMilli()
	done := map[string]bool{}
	res := e.RunLadder([]Quest{instantQuest("grail", done), instantQuest(busy, done)}, time.Hour, time.Hour)
	if res.Outcome != LadderStuck || !WaitingOnSlot(res) {
		t.Fatalf("outcome %s blocked %v", res.Outcome, res.Blocked)
	}
	if len(res.SlotBusy) != 1 || res.SlotBusy[0] != busy {
		t.Errorf("SlotBusy = %v, want [%s]", res.SlotBusy, busy)
	}
	got, ok := res.ParkedUntil["grail"]
	if !ok || got.UnixMilli() != until.UnixMilli() {
		t.Errorf("ParkedUntil = %v, want grail until %v", res.ParkedUntil, until)
	}
}

// Ernest the Chicken's poison, fish food, spade, rubber tube and oil can are single ground spawns that come back 100 ticks
// after a pickup and every account needs each of them: six accounts at a time, the seventh waits, and a release lets it in.
func TestErnestTheChickenRunsSixAtATime(t *testing.T) {
	id := questplan.ErnestChickenID
	if !exclusiveQuests[id] || capacityOf(id) != 6 {
		t.Fatalf("exclusive %v capacity %d, want true and 6", exclusiveQuests[id], capacityOf(id))
	}
	now := time.Now()
	ds := make([]*Deps, 8)
	for i := range ds {
		ds[i] = &Deps{}
		defer releaseQuest(id, ds[i])
	}
	for i := 0; i < 6; i++ {
		if !tryHoldQuest(id, ds[i], now) {
			t.Fatalf("holder %d refused", i)
		}
	}
	if tryHoldQuest(id, ds[6], now) {
		t.Fatal("a seventh account must wait")
	}
	releaseQuest(id, ds[2])
	if !tryHoldQuest(id, ds[6], now) {
		t.Fatal("a release must let the next account in")
	}
	if tryHoldQuest(id, ds[7], now) {
		t.Fatal("an eighth account must wait")
	}
}
