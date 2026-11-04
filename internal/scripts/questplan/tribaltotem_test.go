package questplan

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Tribal Totem (W13, 18 Oct 2026): the journal parser over the pages the content renders, the decision over whole simulated quests
// (a world that answers every step the way the content's scripts do), every state of the decision, the lock's dials and the
// mansion's boxes. The content's own facts (ids, scripts, maps) are tribaltotem_content_test.go's.

// ttJournal is totem_journal.rs2's page for a stage, every string of a branch joined in order (`|` the client's line break).
func ttJournal(stage int) string {
	h := "@str@I agreed to help Kangai Mau on Brimhaven recover the|@str@tribal totem stolen from his village by Lord Handelmort.|"
	marked := "@str@I found a package due for delivery to Lord Handelmort at|@str@the R.P.D.T. depot, and swapped the label for the Wizard|@str@Cromperty's experimental teleport block.|"
	delivered := marked + "@str@I got the R.P.D.T. men to deliver the teleport block.|"
	inside := marked + "@str@I got the R.P.D.T. men to deliver the teleport block to Lord|@str@Handelmort and teleported myself inside.|"
	switch stage {
	case TotemNotStarted:
		return "@dbl@I can start this quest by speaking to @dre@Kangai Mau@dbl@ in @dre@the Shrimp & Parrot@dbl@ restaurant in @dre@Brimhaven|" +
			"@dbl@To complete this quest I need:|@dre@Level 21 Thieving"
	case TotemStarted:
		return h + "@dbl@I need to get into @dre@Lord Handelmorts Mansion@dbl@ in @dre@Ardougne"
	case TotemCrateMarked:
		return h + marked + "@dbl@I should get the @dre@R.P.D.T. men@dbl@ to deliver the crate now."
	case TotemCrateDelivered:
		return h + delivered + "@dbl@I should get @dre@Wizard Cromperty@dbl@ to teleport me inside now."
	case TotemTeleported:
		return h + inside + "@dbl@I need to find the combination for the @dre@security door@dbl@.|"
	case TotemUnlocked:
		return h + inside + "@str@I worked out the combination for the door.|" +
			"@dbl@I should find the @dre@Tribal Totem@dbl@ and return it!"
	case TotemComplete:
		return h + inside + "@str@After bypassing the traps and security inside the|@str@mansion I was able to reclaim the totem, and take it|" +
			"@str@back to Kangai Mau, who rewarded me for all of my help.||@red@QUEST COMPLETE!"
	}
	return ""
}

func TestParseTribalTotemJournalReadsEveryRenderedStage(t *testing.T) {
	for _, stage := range []int{TotemNotStarted, TotemStarted, TotemCrateMarked, TotemCrateDelivered, TotemTeleported, TotemUnlocked, TotemComplete} {
		page := ttJournal(stage)
		for _, lines := range [][]string{{page}, strings.Split(page, "|")} {
			got, ok := ParseTribalTotemJournal(lines)
			if !ok || got != stage {
				t.Errorf("stage %d: parsed %d %v from %q", stage, got, ok, page[:60])
			}
		}
	}
	// The totem in the pack changes the last line of the unlocked page, not its stage.
	withTotem := strings.Replace(ttJournal(TotemUnlocked), "@dbl@I should find the @dre@Tribal Totem@dbl@ and return it!",
		"@dbl@I should take the @dre@tribal totem@dbl@ back to @dre@Kangai Mau in Brimhaven@dbl@ and claim my @dre@reward", 1)
	if got, ok := ParseTribalTotemJournal([]string{withTotem}); !ok || got != TotemUnlocked {
		t.Errorf("the page with the totem in hand parsed %d %v", got, ok)
	}
	// Every page repeats the ones above it: the newest marker is tried first, so no earlier page is read as a later stage and no later one as
	// an earlier. (The stage 3 page names the teleport block and the stage 2 page the Wizard Cromperty's experimental one.)
	for _, lines := range [][]string{nil, {""}, {"@dbl@Something else entirely"}} {
		if got, ok := ParseTribalTotemJournal(lines); ok {
			t.Errorf("%q parsed as %d: an unrecognised journal is unknown, never stage 0", lines, got)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The simulated quest
// ---------------------------------------------------------------------------------------------------------------------

// ttWorld is Tribal Totem as the content runs it: %totemquest (41 for stage 4 with the lock solved), the trap's bit, and the scripts'
// answers to each talk and each custom.
type ttWorld struct {
	*kcWorld
	stage int
	// trapNoted is bit 21 of %handelmort_traps_disabled; level 1 is the floor the account stands on.
	trapNoted bool
	thieving  int
	// chestEmpty makes the chest say "The chest is empty." (a totem in the bank).
	chestEmpty bool
	// fellInTrap makes the climb drop the account into the sewers.
	fellInTrap bool
}

func newTTWorld(t *testing.T, stage int, inv, bank map[int]int) *ttWorld {
	w := &ttWorld{kcWorld: newKCWorld(t, inv, bank), stage: stage, thieving: 40}
	w.tile = ArdougneSouthBank
	return w
}

func (w *ttWorld) snapshot() Snapshot {
	j := StatusInProgress
	switch w.stage {
	case TotemNotStarted:
		j = StatusNotStarted
	case TotemComplete:
		j = StatusComplete
	}
	s := w.base(j)
	s.Levels["thieving"] = w.thieving
	s.Stage, s.StageKnown = w.stage, true
	return s
}

func (w *ttWorld) apply(st Step) {
	t := w.t
	t.Helper()
	if why := ValidateStep(st); why != "" {
		t.Fatalf("malformed step %s: %s", st.Describe(), why)
	}
	switch st.Kind {
	case StepProvision:
		key := "provision:" + st.KitName
		if w.provision(st) {
			w.ran[key] = true
			delete(w.failed, key)
		} else {
			w.failed[key] = true
		}
		w.log = append(w.log, key)
	case StepScanBank:
		w.bankKnown = true
		w.log = append(w.log, "scanbank")
	case StepDeposit:
		w.deposit(st)
		w.log = append(w.log, "deposit")
	case StepWithdraw:
		for _, it := range st.Items {
			n := minInt(it.Qty, w.bank[it.ID])
			w.bank[it.ID] -= n
			w.inv[it.ID] += n
		}
		w.log = append(w.log, "withdraw")
	case StepWalk:
		w.tile = st.Anchor
		w.log = append(w.log, "walk")
	case StepTalk:
		w.talk(st)
	case StepCustom:
		w.custom(st)
	default:
		t.Fatalf("unexpected step %s", st.Describe())
	}
}

func (w *ttWorld) talk(st Step) {
	switch st.Stop.ID {
	case NpcKangaiMau:
		switch w.stage {
		case TotemNotStarted:
			w.stage = TotemStarted
			w.log = append(w.log, "kangai-start")
		case TotemTeleported, TotemUnlocked:
			if w.inv[ObjTribalTotem] == 0 {
				w.t.Fatalf("the hand-in to Kangai Mau without the totem")
			}
			w.inv[ObjTribalTotem]--
			w.stage = TotemComplete
			w.log = append(w.log, "kangai-handin")
		default:
			w.t.Fatalf("a talk with Kangai Mau at stage %d", w.stage)
		}
	case NpcRPDTEmployee:
		if w.stage != TotemCrateMarked {
			w.t.Fatalf("a talk with the employee at stage %d", w.stage)
		}
		w.stage = TotemCrateDelivered
		w.log = append(w.log, "rpdt")
	default:
		w.t.Fatalf("a talk with npc %d", st.Stop.ID)
	}
	w.tile = st.Stop.Anchor
}

func (w *ttWorld) custom(st Step) {
	switch st.Custom {
	case CustomTotemLabel:
		w.inv[ObjTotemLabel] = 1 // horncrate op 2 hands over one only while the pack holds none
	case CustomTotemMark:
		if w.inv[ObjTotemLabel] == 0 {
			w.t.Fatalf("the label used with none in the pack")
		}
		w.inv[ObjTotemLabel]--
		w.stage = TotemCrateMarked
	case CustomTotemTeleport:
		w.tile = MansionArrival
		if w.stage == TotemCrateDelivered {
			w.stage = TotemTeleported
		}
	case CustomTotemLock:
		if !InTotemMansion(w.tile) {
			w.t.Fatalf("the lock solved from %v", w.tile)
		}
		w.stage = TotemUnlocked
		w.tile = ComboStand
	case CustomTotemSteal:
		if w.thieving < TotemThieving {
			w.t.Fatalf("the stairs climbed at Thieving %d", w.thieving)
		}
		if w.chestEmpty {
			w.failed[CustomTotemSteal] = true
			w.log = append(w.log, st.Custom)
			return
		}
		w.trapNoted = true
		w.inv[ObjTribalTotem]++
		w.tile = StairsLanding
	default:
		w.t.Fatalf("unexpected custom %q", st.Custom)
	}
	delete(w.failed, st.Custom)
	w.log = append(w.log, st.Custom)
}

func (w *ttWorld) run() (waited string, parkFor time.Duration) {
	w.t.Helper()
	for i := 0; i < 60; i++ {
		st := DecideTribalTotem(w.snapshot())
		switch st.Kind {
		case StepDone:
			return "", 0
		case StepWait:
			return st.Reason, st.ParkFor
		}
		before := fmt.Sprint(w.stage, w.inv, w.bank, w.bankKnown, w.ran, w.failed, w.tile)
		w.apply(st)
		if fmt.Sprint(w.stage, w.inv, w.bank, w.bankKnown, w.ran, w.failed, w.tile) == before {
			w.t.Fatalf("step %d, %s, changed nothing: the module would loop (log %v)", i, st.Describe(), w.log)
		}
	}
	w.t.Fatalf("60 steps and the quest is not done (stage %d, log %v)", w.stage, w.log)
	return "", 0
}

// A fresh account does the whole quest in the content's order, and the totem is the last thing it holds.
func TestAFreshAccountDoesTribalTotemInOrder(t *testing.T) {
	w := newTTWorld(t, TotemNotStarted, map[int]int{CoinsID: 300}, nil)
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	want := []string{"kangai-start", CustomTotemLabel, CustomTotemMark, "rpdt", CustomTotemTeleport, CustomTotemLock, CustomTotemSteal, "kangai-handin"}
	if !slices.Equal(w.log, want) {
		t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
	if w.inv[ObjTribalTotem] != 0 || w.inv[ObjTotemLabel] != 0 {
		t.Errorf("quest items left in the pack: %v", w.inv)
	}
}

// An account short of coins for the ships draws them once, before the first walk, and goes on if the bank has none.
func TestTheCoinPassIsMadeOnceBeforeTheFirstWalk(t *testing.T) {
	w := newTTWorld(t, TotemNotStarted, nil, map[int]int{CoinsID: 5000})
	w.bankKnown = true
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	if w.log[0] != "provision:"+KitTotemCoins || w.inv[CoinsID] != TotemCoinsQty {
		t.Errorf("log %v, coins held %d, want the coin kit first and %d coins", w.log, w.inv[CoinsID], TotemCoinsQty)
	}
	n := 0
	for _, l := range w.log {
		if l == "provision:"+KitTotemCoins {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the coin pass ran %d times: %v", n, w.log)
	}
	// An account with no coins and a bank without any goes on all the same (the fares are drawn by the travel hook as well).
	w = newTTWorld(t, TotemNotStarted, nil, nil)
	w.bankKnown = true
	if why, _ := w.run(); why != "" {
		t.Fatalf("a coinless account waited: %s (log %v)", why, w.log)
	}
	if !slices.Contains(w.log, "kangai-start") {
		t.Errorf("a coinless account never started: %v", w.log)
	}
}

// Thieving 21 is the content's gate (at the trap): an account short of it is parked for hours before it walks anywhere, and an account
// whose level is not read yet is not judged.
func TestAnAccountUnderThieving21IsParkedBeforeItWalks(t *testing.T) {
	w := newTTWorld(t, TotemNotStarted, map[int]int{CoinsID: 300}, nil)
	w.thieving = 20
	why, park := w.run()
	if !strings.Contains(why, "Thieving 21") || park != TotemLevelPark || len(w.log) != 0 {
		t.Fatalf("waited %q for %s after %v", why, park, w.log)
	}
	// In the mansion with the lock solved, the stairs are the step that wants the level.
	w = newTTWorld(t, TotemUnlocked, nil, nil)
	w.thieving = 20
	w.tile = ComboStand
	if why, park = w.run(); !strings.Contains(why, "Thieving 21") || park != TotemLevelPark || len(w.log) != 0 {
		t.Fatalf("in the mansion: waited %q for %s after %v", why, park, w.log)
	}
	s := newTTWorld(t, TotemNotStarted, map[int]int{CoinsID: 300}, nil).snapshot()
	for _, lv := range []map[string]int{nil, {}, {"hitpoints": 40}} {
		s.Levels = lv
		if st := DecideTribalTotem(s); st.Kind != StepTalk {
			t.Errorf("an unread Thieving (levels %v) decided %s", lv, st.Describe())
		}
	}
}

// The stage decides, and the pack and the place pick the step inside a stage.
func TestTribalTotemDecisionTable(t *testing.T) {
	type want struct {
		kind   StepKind
		custom string
		npc    int
		wait   string
	}
	label, totem := map[int]int{ObjTotemLabel: 1}, map[int]int{ObjTribalTotem: 1}
	for _, c := range []struct {
		name  string
		stage int
		inv   map[int]int
		bank  map[int]int
		known bool
		at    tile.Tile
		mod   func(*Snapshot)
		want  want
	}{
		{"not started, coins held", TotemNotStarted, map[int]int{CoinsID: 300}, nil, false, ArdougneSouthBank, nil, want{kind: StepTalk, npc: NpcKangaiMau}},
		{"started, no label", TotemStarted, nil, nil, false, KangaiAnchor, nil, want{kind: StepCustom, custom: CustomTotemLabel}},
		{"started, the label held", TotemStarted, label, nil, false, DepotCrates, nil, want{kind: StepCustom, custom: CustomTotemMark}},
		{"started, the label held and the pack full", TotemStarted, label, nil, false, DepotCrates,
			func(s *Snapshot) { s.FreeSlots = 0 }, want{kind: StepCustom, custom: CustomTotemMark}},
		{"started, no room for the label", TotemStarted, nil, nil, false, DepotCrates,
			func(s *Snapshot) { s.FreeSlots = 0 }, want{kind: StepDeposit}},
		{"marked", TotemCrateMarked, nil, nil, false, DepotCrates, nil, want{kind: StepTalk, npc: NpcRPDTEmployee}},
		{"delivered", TotemCrateDelivered, nil, nil, false, DepotCrates, nil, want{kind: StepCustom, custom: CustomTotemTeleport}},
		{"teleported, in the arrival bay, the lock open", TotemTeleported, nil, nil, false, MansionArrival, nil, want{kind: StepCustom, custom: CustomTotemLock}},
		{"unlocked, in the hall", TotemUnlocked, nil, nil, false, ComboStand, nil, want{kind: StepCustom, custom: CustomTotemSteal}},
		{"unlocked, upstairs", TotemUnlocked, nil, nil, false, StairsLanding, nil, want{kind: StepCustom, custom: CustomTotemSteal}},
		{"teleported, outside the mansion", TotemTeleported, nil, nil, false, MansionFrontOutside, nil, want{kind: StepCustom, custom: CustomTotemTeleport}},
		{"unlocked, outside the mansion", TotemUnlocked, nil, nil, false, ArdougneSouthBank, nil, want{kind: StepCustom, custom: CustomTotemTeleport}},
		{"the totem in hand, in the mansion", TotemUnlocked, totem, nil, false, StairsLanding, nil, want{kind: StepTalk, npc: NpcKangaiMau}},
		{"the totem in hand, upstairs at stage 4", TotemTeleported, totem, nil, false, StairsLanding, nil, want{kind: StepTalk, npc: NpcKangaiMau}},
		{"the totem in hand, in Brimhaven", TotemUnlocked, totem, nil, false, KangaiAnchor, nil, want{kind: StepTalk, npc: NpcKangaiMau}},
		{"the totem banked", TotemUnlocked, nil, totem, true, ArdougneSouthBank, nil, want{kind: StepWithdraw}},
		{"the bank read and empty, outside", TotemUnlocked, nil, nil, true, ArdougneSouthBank, nil, want{kind: StepCustom, custom: CustomTotemTeleport}},
		{"fallen into the sewers", TotemUnlocked, nil, nil, false, tile.Tile{X: 2640, Z: 9719}, nil, want{kind: StepWalk}},
		{"stage 5 but the tab is not green yet", TotemComplete, nil, nil, false, KangaiAnchor,
			func(s *Snapshot) { s.Journal = StatusInProgress }, want{kind: StepWait, wait: "turned green"}},
	} {
		w := newTTWorld(t, c.stage, c.inv, c.bank)
		w.bankKnown = c.known
		w.tile = c.at
		s := w.snapshot()
		if c.mod != nil {
			c.mod(&s)
		}
		st := DecideTribalTotem(s)
		if st.Kind != c.want.kind || st.Custom != c.want.custom || (c.want.npc != 0 && st.Stop.ID != c.want.npc) || !strings.Contains(st.Reason, c.want.wait) {
			t.Errorf("%s: decided %s (%+v), want %+v", c.name, st.Describe(), st, c.want)
		}
		if st.Kind != StepWait {
			if why := ValidateStep(st); why != "" {
				t.Errorf("%s: malformed step: %s", c.name, why)
			}
		}
	}
}

// The journal and the tab: an unread stage and an uncoloured tab wait, they do not guess.
func TestTribalTotemWaitsForTheTabAndTheJournal(t *testing.T) {
	w := newTTWorld(t, TotemStarted, nil, nil)
	s := w.snapshot()
	s.Journal = StatusUnknown
	if st := DecideTribalTotem(s); st.Kind != StepWait || !strings.Contains(st.Reason, "coloured") {
		t.Errorf("an uncoloured tab decided %s", st.Describe())
	}
	s = w.snapshot()
	s.StageKnown = false
	if st := DecideTribalTotem(s); st.Kind != StepWait || !strings.Contains(st.Reason, "stage") {
		t.Errorf("an unread stage decided %s", st.Describe())
	}
	s = w.snapshot()
	s.Journal = StatusComplete
	if st := DecideTribalTotem(s); st.Kind != StepDone {
		t.Errorf("a green tab decided %s", st.Describe())
	}
	s.Journal, s.Stage = StatusInProgress, 99
	if st := DecideTribalTotem(s); st.Kind != StepWait {
		t.Errorf("stage 99 decided %s", st.Describe())
	}
}

// A step that failed on its last run is not repeated at once: the quest parks for half an hour with the reason, and the chest that
// says it is empty sends the account to read its bank (the chest gives no totem to an account that holds one anywhere).
func TestTotemFailuresPark(t *testing.T) {
	for _, c := range []struct {
		name   string
		stage  int
		inv    map[int]int
		at     tile.Tile
		failed string
		want   string
	}{
		{"the label", TotemStarted, nil, DepotCrates, CustomTotemLabel, "address label"},
		{"the mark", TotemStarted, map[int]int{ObjTotemLabel: 1}, DepotCrates, CustomTotemMark, "address label"},
		{"the teleport", TotemCrateDelivered, nil, DepotCrates, CustomTotemTeleport, "teleport"},
		{"the lock", TotemTeleported, nil, MansionArrival, CustomTotemLock, "combination"},
	} {
		w := newTTWorld(t, c.stage, c.inv, nil)
		w.tile = c.at
		w.failed[c.failed] = true
		why, park := w.run()
		if !strings.Contains(why, c.want) || park != TotemRetryPark || len(w.log) != 0 {
			t.Errorf("%s: waited %q for %s after %v", c.name, why, park, w.log)
		}
	}
	// The chest: empty with the bank unread, the bank is read; read and empty, the quest parks.
	w := newTTWorld(t, TotemUnlocked, nil, nil)
	w.tile = ComboStand
	w.chestEmpty = true
	why, park := w.run()
	if !strings.Contains(why, "chest gave no totem") || park != TotemRetryPark {
		t.Fatalf("waited %q for %s (log %v)", why, park, w.log)
	}
	if want := []string{CustomTotemSteal, "scanbank"}; !slices.Equal(w.log, want) {
		t.Errorf("log %v, want the chest tried, then the bank read", w.log)
	}
	// And with the totem in the bank the same chest costs a withdrawal and the hand-in.
	w = newTTWorld(t, TotemUnlocked, nil, map[int]int{ObjTribalTotem: 1})
	w.tile = ComboStand
	w.chestEmpty = true
	if why, _ = w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	if want := []string{CustomTotemSteal, "scanbank", "withdraw", "kangai-handin"}; !slices.Equal(w.log, want) {
		t.Errorf("log %v, want %v", w.log, want)
	}
}

// Every step the decision can ask for, over every stage, every place and a spread of packs, is well formed, and none of them is a
// walk, a talk or a custom the module has no name for.
func TestEveryTotemStepIsWellFormed(t *testing.T) {
	customs := map[string]bool{CustomTotemLabel: true, CustomTotemMark: true, CustomTotemTeleport: true, CustomTotemLock: true, CustomTotemSteal: true}
	places := []tile.Tile{ArdougneSouthBank, KangaiAnchor, DepotCrates, CrompertyAnchor, MansionArrival, ComboStand, StairsStand, StairsLanding,
		MansionFrontOutside, {X: 2640, Z: 9719}}
	packs := []map[int]int{nil, {ObjTotemLabel: 1}, {ObjTribalTotem: 1}, {ObjTotemLabel: 1, ObjTribalTotem: 1, CoinsID: 500}, {CoinsID: 10}}
	n := 0
	for _, stage := range []int{TotemNotStarted, TotemStarted, TotemCrateMarked, TotemCrateDelivered, TotemTeleported, TotemUnlocked, TotemComplete} {
		for _, at := range places {
			for _, inv := range packs {
				for _, bank := range []map[int]int{nil, {ObjTribalTotem: 1}} {
					for _, known := range []bool{false, true} {
						for _, failed := range []string{"", CustomTotemLabel, CustomTotemMark, CustomTotemTeleport, CustomTotemLock, CustomTotemSteal} {
							w := newTTWorld(t, stage, inv, bank)
							w.tile, w.bankKnown = at, known
							if failed != "" {
								w.failed[failed] = true
							}
							st := DecideTribalTotem(w.snapshot())
							n++
							if st.Kind == StepWait && st.Reason == "" {
								t.Fatalf("stage %d at %v: a wait with no reason", stage, at)
							}
							if st.Kind == StepCustom && !customs[st.Custom] {
								t.Fatalf("stage %d at %v: unknown custom %q", stage, at, st.Custom)
							}
							if st.Kind != StepWait {
								if why := ValidateStep(st); why != "" {
									t.Fatalf("stage %d at %v: %s: %s", stage, at, st.Describe(), why)
								}
							}
						}
					}
				}
			}
		}
	}
	t.Logf("%d states swept", n)
}

// ---------------------------------------------------------------------------------------------------------------------
// The lock, the mansion and the module
// ---------------------------------------------------------------------------------------------------------------------

// The combination is lock values 10, 20, 17 and 19 of an A to Z wheel (quest_totem.rs2: ~get_tribal_door_lock(1) = 10 and so on).
func TestTheCombinationIsKURT(t *testing.T) {
	want := []int{10, 20, 17, 19}
	for i, c := range []byte(TotemCombination) {
		if int(c-'A') != want[i] {
			t.Errorf("dial %d: %c is lock value %d, the content wants %d", i+1, c, int(c-'A'), want[i])
		}
	}
	if len(TotemCombination) != 4 {
		t.Errorf("%d dials", len(TotemCombination))
	}
}

// From every letter to every letter the shortest way round is no more than half a turn, the arrow it names lands on the letter, and
// a tie takes the up arrow.
func TestTotemDialClicksTakeTheShortestWayRound(t *testing.T) {
	for from := byte('A'); from <= 'Z'; from++ {
		for to := byte('A'); to <= 'Z'; to++ {
			up, n := TotemDialClicks(from, to)
			if n < 0 || n > TotemLockLetters/2 {
				t.Fatalf("%c to %c: %d clicks", from, to, n)
			}
			at := int(from - 'A')
			if up {
				at = (at + n) % TotemLockLetters
			} else {
				at = ((at-n)%TotemLockLetters + TotemLockLetters) % TotemLockLetters
			}
			if byte(at)+'A' != to {
				t.Fatalf("%c to %c: %d clicks of the %s arrow land on %c", from, to, n, map[bool]string{true: "up", false: "down"}[up], byte(at)+'A')
			}
			if n == TotemLockLetters/2 && !up {
				t.Errorf("%c to %c is a half turn and took the down arrow", from, to)
			}
		}
	}
	// KURT from the wheel's first letter is 10 up, 6 down, 9 down, 7 down (23 and 19 clicks the long way).
	var total int
	for _, c := range []byte(TotemCombination) {
		_, n := TotemDialClicks('A', c)
		total += n
	}
	if total != 32 {
		t.Errorf("the lock takes %d clicks from AAAA, want 32", total)
	}
}

func TestTotemDialLetterReadsOnlyTheWheel(t *testing.T) {
	for text, want := range map[string]byte{"A": 'A', " k ": 'K', "z": 'Z', "T": 'T'} {
		if got, ok := TotemDialLetter(text); !ok || got != want {
			t.Errorf("%q read as %c %v", text, got, ok)
		}
	}
	for _, text := range []string{"", "AB", "7", "?", " "} {
		if got, ok := TotemDialLetter(text); ok {
			t.Errorf("%q read as %c", text, got)
		}
	}
	// The interface's components: four dial texts, a down and an up arrow under each, and the confirm.
	seen := map[int]bool{TotemLockConfirm: true}
	for i := 0; i < 4; i++ {
		for _, c := range []int{TotemDialText(i), TotemDialDown(i), TotemDialUp(i)} {
			if seen[c] || c <= TotemLockInterface {
				t.Errorf("component %d of dial %d is repeated or below the interface", c, i)
			}
			seen[c] = true
		}
	}
}

// The mansion's boxes hold the hall, the three bays and the floor above, and nothing outside the front door.
func TestTheMansionBoxes(t *testing.T) {
	for _, at := range []tile.Tile{MansionArrival, ComboStand, StairsStand, {X: 2635, Z: 3322}, {X: 2628, Z: 3321}, {X: 2632, Z: 3320}, StairsLanding, TotemChest,
		{X: 2638, Z: 3323, Level: 1}} {
		if !InTotemMansion(at) {
			t.Errorf("%v is not in the mansion", at)
		}
	}
	for _, at := range []tile.Tile{MansionFrontOutside, {X: 2635, Z: 3321}, {X: 2635, Z: 3318}, {X: 2644, Z: 3323}, {X: 2626, Z: 3323}, {X: 2638, Z: 3326},
		{X: 2641, Z: 3321, Level: 1}, {X: 2638, Z: 3321, Level: 2}, ArdougneSouthBank, {X: 2640, Z: 9719}} {
		if InTotemMansion(at) {
			t.Errorf("%v is in the mansion", at)
		}
	}
}

func TestTribalTotemModuleShape(t *testing.T) {
	m := TribalTotem
	if m.Record.ID != "totem" || m.Record.Name != "Tribal Totem" || m.Record.QuestPoints != 1 {
		t.Errorf("record %+v", m.Record)
	}
	if len(m.Record.Requirements.Skills) != 1 || m.Record.Requirements.Skills[0] != (SkillReq{Skill: "thieving", Level: 21}) {
		t.Errorf("requirements %+v: Thieving 21 is the content's gate", m.Record.Requirements)
	}
	if m.MaxDeaths != 1 || !m.NoRetaliate || m.Decide == nil || m.ReadStage == nil || !m.BankSet || m.Bank != ArdougneSouthBank {
		t.Errorf("module %+v", m)
	}
	// The quest's items are tools, the foods are only kept: a food that was a tool would be left to the quest by the floor bite.
	for _, id := range []int{ObjTotemLabel, ObjTribalTotem, CoinsID} {
		if !slices.Contains(m.ToolIDs, id) || !slices.Contains(TotemKeepIDs(), id) {
			t.Errorf("obj %d is not a tool and a keep", id)
		}
	}
	for _, id := range m.Sustain.FoodIDs {
		if slices.Contains(m.ToolIDs, id) || !slices.Contains(TotemKeepIDs(), id) {
			t.Errorf("food %d: tool %v keep %v", id, slices.Contains(m.ToolIDs, id), slices.Contains(TotemKeepIDs(), id))
		}
	}
	if m.ExclusiveDone != nil {
		t.Errorf("Tribal Totem has no one-at-a-time step: nothing in it is shared (questrunner/slots.go)")
	}
	// The record blocks an account under Thieving 21 and passes one at it.
	for _, c := range []struct {
		lvl int
		ok  bool
	}{{1, false}, {20, false}, {21, true}, {99, true}} {
		for _, r := range CheckRequirements(m.Record, PlayerState{SkillLevels: map[string]int{"thieving": c.lvl}}) {
			if r.OK != c.ok {
				t.Errorf("Thieving %d: requirement %+v", c.lvl, r)
			}
		}
	}
	// Every kit the module asks for is valid.
	if why := ValidateKitLine(totemCoinsKit()[0]); why != "" {
		t.Error(why)
	}
	if QuestTabName(TribalTotemID) != TribalTotemName {
		t.Errorf("tab name %q", QuestTabName(TribalTotemID))
	}
}
