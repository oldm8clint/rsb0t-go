package questplan

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// ---- the journal ----------------------------------------------------------------------------------------------------

// hjAll is every literal of hunt_journal.rs2 the renderer below uses, each as one appended text of the script, so that
// TestHuntJournalStringsAreTheScripts can look for every one of them in the file.
var hjAll []string

// hj joins the texts the script appends in one branch, recording each.
func hj(parts ...string) string {
	hjAll = append(hjAll, parts...)
	return strings.Join(parts, "")
}

// The strings of quests/quest_hunt/scripts/hunt_journal.rs2, one variable per branch's text, so that the renderer below is the
// script's own branch structure over the script's own words.
var (
	hjNotStarted = hj("@dbl@I can start this quest by speaking to @dre@Redbeard Frank@dbl@ who is at @dre@Port Sarim@dbl@.||",
		"@dbl@There aren't any requirements for this quest.")
	hjSpoke = hj("@dbl@I have spoken to @dre@Redbeard Frank@dbl@. He has agreed to tell me the location of some @dre@treasure@dbl@ for some @dre@Karamja Rum@dbl@.||")

	hjUnemployedRum = hj("@dbl@I have the @dre@rum@dbl@, and now I need to find a way to get the rum off @dre@Karamja@dbl@. This might be tricky, as the @dre@Customs Officers|",
		"@dbl@are searching people for it.")
	hjUnemployedNone = hj("@dbl@I need to go to @dre@Karamja@dbl@ and buy some @dre@rum@dbl@. I hope it is not too expensive.")

	hjShippedHistory = hj("@str@I have taken employment on the banana plantation, as the|@str@Customs Officers might not notice the rum if it is covered in|@str@bananas.||",
		"@str@I have hidden my rum in the crate. I should fill it with|@str@bananas and speak to Luthas to have it shipped over.||")
	hjShipped = hj("@dbl@I have spoken to @dre@Luthas@dbl@, and the crate has been shipped to @dre@Wydin's store@dbl@ in @dre@Port Sarim@dbl@. Now all I have to do is get to it.")

	hjPlantation = hj("@dbl@I have taken employment on the @dre@banana plantation@dbl@, as the @dre@Customs Officers@dbl@ might not notice the @dre@rum@dbl@ if it is covered in @dre@bananas@dbl@.||")
	hjHideNext   = hj("@dbl@I'm sure I will be able to hide my @dre@rum@dbl@ in the next crate destined for @dre@Wydin's store@dbl@.")
	hjFillIt     = hj("@dbl@I have hidden my @dre@rum@dbl@ in the crate. I should fill it with @dre@bananas@dbl@ and speak to @dre@Luthas@dbl@ and have it shipped over.")
	hjFilled     = hj("@dbl@I have hidden my @dre@rum@dbl@ in the crate and filled it with @dre@bananas@dbl@. I should speak to @dre@Luthas@dbl@ and have it shipped|",
		"@dbl@over.")
	hjNeedRum = hj("@dbl@Now all I need is some @dre@rum@dbl@ to hide in the next crate destined for @dre@Wydin's store@dbl@.")

	hjStoreHistory = hjShippedHistory + hj("@str@I have spoken to Luthas, and the crate has been shipped to|@str@to Wydin's store in Port Sarim. Now all I have to do is|",
		"@str@get to it.||")
	hjStoreJob = hj("@dbl@I have taken a job at @dre@Wydin's store@dbl@. I now have access to the back room of his shop where the @dre@rum@dbl@ is hidden.")
	hjInHand   = hj("@dbl@I have the @dre@Karamja Rum@dbl@. I should take it to @dre@Redbeard Frank@dbl@.")
	hjLost     = hj("@dbl@I had some @dre@rum@dbl@, but I seem to have lost it. I will need to smuggle some more off @dre@Karamja@dbl@.")

	hjKeyHistory = hj("@str@I have spoken to Redbeard Frank. He has agreed to tell me|@str@the location of some treasure for some Karamja Rum.||",
		"@str@I have smuggled some rum off Karamja, and retrieved it|@str@from the back room of Wydin's shop.||")
	hjGave    = hj("@dbl@I have given the rum to @dre@Redbeard Frank@dbl@. He has told me that the @dre@treasure@dbl@ is hidden in the chest in the upstairs room of the @dre@Blue Moon Inn@dbl@ in @dre@Varrock@dbl@.||")
	hjMessage = hj("@dbl@I have opened the chest in the @dre@Blue Moon@dbl@, and found a @dre@note @dbl@inside. I think it will tell me where to dig.")
	hjHaveKey = hj("@dbl@I have a @dre@key@dbl@ that can be used to unlock the chest that hold the treasure.")
	hjNoKey   = hj("@dbl@I have lost the @dre@key@dbl@ that @dre@Redbeard Frank@dbl@ gave me. I should see if he has another.")

	hjNoteHistory = hjKeyHistory + hj("@str@I have given the rum to Redbeard Frank. He has told me|@str@that the treasure is hidden in the chest in the upstairs|",
		"@str@room of the Blue Moon Inn in Varrock.||",
		"@str@I have opened the chest in the Blue Moon, and found a|@str@note inside. I think it will tell me where to dig.||")
	hjNoteReads = hj("@dbl@The note reads @dre@'Visit the city of the White Knights. In the park, Saradomin points to the X which marks the spot.'")
	hjDone      = hj("@str@The note reads 'Visit the city of the White Knights. In the|@str@park, Saradomin points to the X which marks the spot.'||",
		"@red@QUEST COMPLETE!")
)

// huntState is the varps and the pack the journal's script reads.
type huntState struct {
	hunt     int  // %hunt 0..4
	employed int  // %hunt_store_employed 0..3
	crateRum int  // %crate_rum 0..2
	bananas  int  // %crate_bananas 0..10
	rumHeld  bool // inv_total(inv, karamja_rum) > 0
	hasKey   bool
	hasNote  bool
}

// renderHuntJournal is hunt_journal.rs2's [if_button,questlist:hunt] over a state: its branches, in its order, with its
// words. The branch conditions are the script's (E = %hunt_store_employed, R = %crate_rum).
func renderHuntJournal(st huntState) string {
	E, R := st.employed, st.crateRum
	switch st.hunt {
	case 0:
		return hjNotStarted
	case 1:
		text := hjSpoke
		switch {
		case E == 0 && R < 2:
			if st.rumHeld {
				return text + hjUnemployedRum
			}
			return text + hjUnemployedNone
		case (E == 0 && R == 2) || (E == 1 && R == 2):
			return text + hjShippedHistory + hjShipped
		case E == 1:
			text += hjPlantation
			switch {
			case st.rumHeld && R == 0:
				return text + hjHideNext
			case R == 1:
				if st.bananas < 10 {
					return text + hjFillIt
				}
				return text + hjFilled
			}
			return text + hjNeedRum
		case E == 2:
			switch R {
			case 2:
				return text + hjStoreHistory + hjStoreJob
			case 0:
				if st.rumHeld {
					return text + hjInHand
				}
				return text + hjLost
			}
			return text // %hunt_store_employed 2 with the rum in the plantation crate: no branch, the header alone
		case E == 3:
			if R == 0 {
				if st.rumHeld {
					return text + hjInHand
				}
				return text + hjLost
			}
			return text + hjStoreJob
		}
		return text
	case 2:
		text := hjKeyHistory + hjGave
		switch {
		case st.hasNote:
			return text + hjMessage
		case st.hasKey:
			return text + hjHaveKey
		}
		return text + hjNoKey
	case 3:
		return hjNoteHistory + hjNoteReads
	}
	return hjNoteHistory + hjDone
}

// huntOracleStage is the stage the module should read from a state, derived from the SEMANTICS (who is employed where, where the
// rum is, whether it is held) and not from the text: the independent half of the check that the needles and their order read
// every page the script can render.
func huntOracleStage(st huntState) int {
	switch st.hunt {
	case 0:
		return HuntNotStarted
	case 2:
		return HuntReceivedKey
	case 3:
		return HuntReadNote
	case 4:
		return HuntComplete
	}
	E, R := st.employed, st.crateRum
	atPlantation, atStore := E&HuntEmployedPlantation != 0, E&HuntEmployedStore != 0
	switch {
	case R == HuntRumInStore && !atStore:
		return HuntShipped // the crate has sailed and Wydin has not hired the account
	case !atStore && !atPlantation:
		if st.rumHeld {
			return HuntRumUnemployed
		}
		return HuntNeedRum
	case atPlantation && !atStore:
		switch {
		case R == HuntRumInPlantation && st.bananas >= HuntCrateCapacity:
			return HuntCrateFull
		case R == HuntRumInPlantation:
			return HuntRumInCrate
		case st.rumHeld:
			return HuntRumHeldEmployed
		}
		return HuntEmployedNeedRum
	}
	// Employed at Wydin's store (alone, or as well as at the plantation).
	switch {
	case R == HuntRumNowhere && st.rumHeld:
		return HuntRumInHand
	case R == HuntRumNowhere:
		return HuntRumLost
	case atPlantation && R == HuntRumInPlantation, R == HuntRumInStore:
		return HuntStoreJob
	}
	return HuntFetchRum // %hunt_store_employed 2 with the rum in the plantation crate
}

// Every page the script can render, over every state of its varps and the pack, reads back as the stage the semantics say.
func TestParseHuntJournalReadsEveryRenderedPage(t *testing.T) {
	n := 0
	for hunt := 0; hunt <= 4; hunt++ {
		for employed := 0; employed <= 3; employed++ {
			for crateRum := 0; crateRum <= 2; crateRum++ {
				for _, bananas := range []int{0, 1, 9, 10} {
					for _, rumHeld := range []bool{false, true} {
						for _, hasKey := range []bool{false, true} {
							for _, hasNote := range []bool{false, true} {
								st := huntState{hunt, employed, crateRum, bananas, rumHeld, hasKey, hasNote}
								text := renderHuntJournal(st)
								want := huntOracleStage(st)
								for _, lines := range [][]string{{text}, strings.Split(text, "|")} {
									got, ok := ParseHuntJournal(lines)
									if !ok || got != want {
										t.Fatalf("%+v: read as %d (ok %v), the semantics say %d:\n%s", st, got, ok, want, text)
									}
								}
								n++
							}
						}
					}
				}
			}
		}
	}
	if n < 1900 {
		t.Fatalf("only %d states swept", n)
	}
}

// The ten faces of %hunt 1 all occur (so the sweep above proves the needles against a page for each), and the unreachable one is
// the header alone.
func TestHuntJournalHasTenSmuggleFaces(t *testing.T) {
	seen := map[int]bool{}
	for employed := 0; employed <= 3; employed++ {
		for crateRum := 0; crateRum <= 2; crateRum++ {
			for _, bananas := range []int{0, 10} {
				for _, rumHeld := range []bool{false, true} {
					st := huntState{hunt: 1, employed: employed, crateRum: crateRum, bananas: bananas, rumHeld: rumHeld}
					got, _ := ParseHuntJournal([]string{renderHuntJournal(st)})
					seen[got] = true
				}
			}
		}
	}
	want := []int{HuntFetchRum, HuntNeedRum, HuntRumUnemployed, HuntEmployedNeedRum, HuntRumHeldEmployed, HuntRumInCrate, HuntCrateFull,
		HuntShipped, HuntStoreJob, HuntRumInHand, HuntRumLost}
	for _, w := range want {
		if !seen[w] {
			t.Errorf("stage %d is never rendered", w)
		}
	}
	if len(seen) != len(want) {
		t.Errorf("stages read: %v, want %v", seen, want)
	}
}

// An unrecognised page is unknown, never a stage: an unread journal must not restart a finished quest or send the account to
// Karamja for a rum it already shipped.
func TestParseHuntJournalRefusesWhatItDoesNotKnow(t *testing.T) {
	for _, lines := range [][]string{nil, {""}, {"@dbl@Something else entirely."}, {"I can start this quest by talking to Frank"}} {
		if got, ok := ParseHuntJournal(lines); ok {
			t.Errorf("%q read as stage %d", lines, got)
		}
	}
}

// ---- the crate's messages --------------------------------------------------------------------------------------------

func TestParseHuntCrate(t *testing.T) {
	for _, c := range []struct {
		said string
		want HuntCrate
		ok   bool
	}{
		{"The crate is completely empty.", HuntCrate{}, true},
		{"There is some rum in here, although with no bananas to cover it. It is a little obvious.", HuntCrate{Rum: true}, true},
		{"The crate has 1 banana inside.", HuntCrate{Bananas: 1}, true},
		{"The crate has 7 bananas inside. There is also some rum stashed in here too.", HuntCrate{Rum: true, Bananas: 7}, true},
		{"The crate is full of bananas.", HuntCrate{Bananas: 10}, true},
		{"The crate is full of bananas. There is also some rum stashed in here too.", HuntCrate{Rum: true, Bananas: 10}, true},
		{"You pick a banana.", HuntCrate{}, false},
		{"", HuntCrate{}, false},
	} {
		got, ok := ParseHuntCrate(c.said)
		if ok != c.ok || got != c.want {
			t.Errorf("%q: %+v %v, want %+v %v", c.said, got, ok, c.want, c.ok)
		}
	}
}

func TestParseHuntStash(t *testing.T) {
	for said, want := range map[string]HuntStash{
		"You stash the rum in the crate.":                                             HuntStashed,
		"Why would I want to do that?":                                                HuntStashUnemployed,
		"There's already some rum in here...":                                         HuntStashPlanted,
		"There is already some rum in Wydin's store, I should go and get that first.": HuntStashInStore,
		"I see no reason to do that.":                                                 HuntStashNoReason,
		"Nothing interesting happens.":                                                HuntStashNone,
	} {
		if got := ParseHuntStash(said); got != want {
			t.Errorf("%q: %d, want %d", said, got, want)
		}
	}
}

// ---- Decide -----------------------------------------------------------------------------------------------------------

var (
	huntPortSarim = FrankAnchor
	huntVarrock   = tile.Tile{X: 3185, Z: 3436}
	huntZambo     = ZamboShop.Anchor
	huntDeck      = tile.Tile{X: 2956, Z: 3143, Level: 1}
)

// huntSnap is a snapshot of %hunt 1 or later at a stage.
type huntSnap struct {
	stage   int
	at      tile.Tile
	inv     map[int]int
	worn    map[int]int
	bank    map[int]int
	free    int // 0 is a roomy pack (20 free slots)
	full    bool
	ran     []string
	failed  []string
	blocked []string
}

func (h huntSnap) snapshot() Snapshot {
	s := Snapshot{Journal: StatusInProgress, Stage: h.stage, StageKnown: true, Tile: h.at, TileKnown: true, FreeSlots: h.free,
		Inv: map[string]int{}, InvIDs: map[int]int{}, Worn: map[string]int{}, WornIDs: map[int]int{},
		Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{}}
	switch {
	case h.full:
		s.FreeSlots = 0
	case h.free == 0:
		s.FreeSlots = 20
	}
	for id, n := range h.inv {
		s.InvIDs[id] = n
	}
	for id, n := range h.worn {
		s.WornIDs[id] = n
	}
	if h.bank != nil {
		s.BankKnown, s.BankIDs = true, h.bank
	}
	for _, k := range h.ran {
		s.Ran[k] = true
	}
	for _, k := range h.failed {
		s.Failed[k] = true
	}
	for _, k := range h.blocked {
		s.Blocked[k] = true
	}
	switch h.stage {
	case HuntNotStarted:
		s.Journal, s.StageKnown = StatusNotStarted, false
	case HuntComplete:
		s.Journal = StatusComplete
	}
	return s
}

func richPack(extra map[int]int) map[int]int {
	out := map[int]int{CoinsID: 1000}
	for id, n := range extra {
		out[id] = n
	}
	return out
}

// The whole quest as a table: where each stage sends an account standing where, holding what.
func TestDecidePiratesTreasureTable(t *testing.T) {
	apron := map[int]int{ObjWhiteApron: 1}
	rum := map[int]int{ObjKaramjaRum: 1}
	type want struct {
		kind   StepKind
		npc    string // a talk
		custom string
		kit    string
		item   int // an equip or an opHeld or a useOn
	}
	cases := []struct {
		name string
		h    huntSnap
		want want
	}{
		// the start and the sub-states before the crossing
		{"need rum with no coins banks for the fares", huntSnap{stage: HuntNeedRum, at: huntPortSarim, inv: map[int]int{CoinsID: 50}, worn: apron}, want{kind: StepProvision, kit: KitHuntSupplies}},
		{"need rum with no apron buys it first", huntSnap{stage: HuntNeedRum, at: huntPortSarim, inv: richPack(nil)}, want{kind: StepProvision, kit: KitHuntSupplies}},
		{"need rum, ready, on the mainland: buys at Zambo", huntSnap{stage: HuntNeedRum, at: huntPortSarim, inv: richPack(apron)}, want{kind: StepCustom, custom: CustomHuntRum}},
		{"need rum, the apron worn: buys at Zambo", huntSnap{stage: HuntNeedRum, at: huntPortSarim, inv: richPack(nil), worn: apron}, want{kind: StepCustom, custom: CustomHuntRum}},
		{"need rum on Karamja buys without banking", huntSnap{stage: HuntNeedRum, at: huntZambo, inv: map[int]int{CoinsID: 120}}, want{kind: StepCustom, custom: CustomHuntRum}},
		{"employed, no rum: buys", huntSnap{stage: HuntEmployedNeedRum, at: huntZambo, inv: richPack(nil)}, want{kind: StepCustom, custom: CustomHuntRum}},
		{"lost rum restarts the smuggle", huntSnap{stage: HuntRumLost, at: huntPortSarim, inv: richPack(apron)}, want{kind: StepCustom, custom: CustomHuntRum}},
		{"rum on the mainland, unemployed, goes to Luthas", huntSnap{stage: HuntRumUnemployed, at: huntPortSarim, inv: richPack(map[int]int{ObjKaramjaRum: 1, ObjWhiteApron: 1})}, want{kind: StepTalk, npc: "Luthas"}},
		{"rum on Karamja, unemployed, asks Luthas for work", huntSnap{stage: HuntRumUnemployed, at: huntZambo, inv: richPack(rum)}, want{kind: StepTalk, npc: "Luthas"}},
		{"rum on Karamja, employed: ships it", huntSnap{stage: HuntRumHeldEmployed, at: huntZambo, inv: richPack(rum)}, want{kind: StepCustom, custom: CustomHuntShip}},
		{"the rum in the crate: ships it", huntSnap{stage: HuntRumInCrate, at: huntPortSarim, inv: richPack(apron)}, want{kind: StepCustom, custom: CustomHuntShip}},
		{"the full crate: ships it", huntSnap{stage: HuntCrateFull, at: huntPlantationTile(), inv: richPack(nil)}, want{kind: StepCustom, custom: CustomHuntShip}},
		{"a bought rum the journal has not caught up with is stashed", huntSnap{stage: HuntNeedRum, at: huntZambo, inv: richPack(rum)}, want{kind: StepCustom, custom: CustomHuntShip}},
		{"rum in hand on Karamja is stashed, never carried to the ship", huntSnap{stage: HuntRumInHand, at: huntZambo, inv: richPack(rum)}, want{kind: StepCustom, custom: CustomHuntShip}},
		{"rum in hand on the ship's deck is stashed too", huntSnap{stage: HuntRumInHand, at: huntDeck, inv: richPack(rum)}, want{kind: StepCustom, custom: CustomHuntShip}},
		{"rum in hand on the mainland goes to Frank", huntSnap{stage: HuntRumInHand, at: huntPortSarim, inv: richPack(rum)}, want{kind: StepTalk, npc: "Redbeard Frank"}},
		// Wydin's
		{"shipped: Wydin hires the apron-wearer", huntSnap{stage: HuntShipped, at: huntPortSarim, inv: richPack(apron)}, want{kind: StepTalk, npc: "Wydin"}},
		{"shipped, apron worn: Wydin hires", huntSnap{stage: HuntShipped, at: huntPortSarim, inv: richPack(nil), worn: apron}, want{kind: StepTalk, npc: "Wydin"}},
		{"shipped with no apron fetches one (the bank, then Thessalia)", huntSnap{stage: HuntShipped, at: huntPortSarim, inv: richPack(nil)}, want{kind: StepProvision, kit: KitHuntSupplies}},
		{"shipped, still on Karamja with no apron: the supplies pass crosses to a bank", huntSnap{stage: HuntShipped, at: huntZambo, inv: richPack(nil)}, want{kind: StepProvision, kit: KitHuntSupplies}},
		{"store job: wears the apron before the door refuses it", huntSnap{stage: HuntStoreJob, at: huntPortSarim, inv: richPack(apron)}, want{kind: StepEquip, item: ObjWhiteApron}},
		{"store job with the apron worn searches the back room", huntSnap{stage: HuntStoreJob, at: huntPortSarim, inv: richPack(nil), worn: apron}, want{kind: StepCustom, custom: CustomHuntCollect}},
		{"store job with no apron at all fetches one", huntSnap{stage: HuntStoreJob, at: huntPortSarim, inv: richPack(nil)}, want{kind: StepProvision, kit: KitHuntSupplies}},
		{"store job on Karamja ships first (the rum may still be in the plantation crate)", huntSnap{stage: HuntStoreJob, at: huntZambo, inv: richPack(nil), worn: apron}, want{kind: StepCustom, custom: CustomHuntShip}},
		{"store job on Karamja after the crate sailed heads for the back room", huntSnap{stage: HuntStoreJob, at: huntZambo, inv: richPack(nil), worn: apron, ran: []string{CustomHuntShip}}, want{kind: StepCustom, custom: CustomHuntCollect}},
		{"an empty back room sends the account to the plantation", huntSnap{stage: HuntStoreJob, at: huntPortSarim, inv: richPack(nil), worn: apron, failed: []string{CustomHuntCollect}}, want{kind: StepCustom, custom: CustomHuntShip}},
		{"the plantation leg ran since an empty look: the account on the island looks again", huntSnap{stage: HuntStoreJob, at: huntZambo, inv: richPack(nil), worn: apron,
			failed: []string{CustomHuntCollect}, ran: []string{CustomHuntShip}}, want{kind: StepCustom, custom: CustomHuntCollect}},
		{"a second empty look after the plantation leg ends the tries", huntSnap{stage: HuntStoreJob, at: huntPortSarim, inv: richPack(nil), worn: apron,
			failed: []string{CustomHuntCollect}, ran: []string{CustomHuntShip}}, want{kind: StepWait}},
		{"neither crate holds it: the quest waits", huntSnap{stage: HuntStoreJob, at: huntPortSarim, inv: richPack(nil), worn: apron, failed: []string{CustomHuntCollect, CustomHuntShip}}, want{kind: StepWait}},
		// the treasure
		{"the key is re-issued by Frank", huntSnap{stage: HuntReceivedKey, at: huntVarrock, inv: richPack(nil)}, want{kind: StepTalk, npc: "Redbeard Frank"}},
		{"the key opens the chest", huntSnap{stage: HuntReceivedKey, at: huntVarrock, inv: richPack(map[int]int{ObjChestKey: 1})}, want{kind: StepCustom, custom: CustomHuntChest}},
		{"a chest that gave nothing waits", huntSnap{stage: HuntReceivedKey, at: huntVarrock, inv: richPack(map[int]int{ObjChestKey: 1}), failed: []string{CustomHuntChest}}, want{kind: StepWait}},
		{"the message is read", huntSnap{stage: HuntReceivedKey, at: huntVarrock, inv: richPack(map[int]int{ObjPirateMessage: 1, ObjChestKey: 1})}, want{kind: StepOpHeld, item: ObjPirateMessage}},
		{"a read message with no spade fetches one", huntSnap{stage: HuntReadNote, at: huntVarrock, inv: richPack(nil)}, want{kind: StepProvision, kit: KitHuntSpade}},
		{"the spade digs", huntSnap{stage: HuntReadNote, at: huntVarrock, inv: richPack(map[int]int{ObjSpade: 1})}, want{kind: StepCustom, custom: CustomHuntDig}},
		{"a dug hole waits for the tab", huntSnap{stage: HuntReadNote, at: huntVarrock, inv: richPack(map[int]int{ObjSpade: 1}), ran: []string{CustomHuntDig}}, want{kind: StepWait}},
		{"a blocked spade kit waits", huntSnap{stage: HuntReadNote, at: huntVarrock, inv: richPack(nil), blocked: []string{"provision:" + KitHuntSpade}}, want{kind: StepWait}},
		{"complete is done", huntSnap{stage: HuntComplete, at: huntVarrock}, want{kind: StepDone}},
		{"unstarted talks to Frank", huntSnap{stage: HuntNotStarted, at: huntPortSarim}, want{kind: StepTalk, npc: "Redbeard Frank"}},
	}
	for _, c := range cases {
		s := c.h.snapshot()
		got := DecidePiratesTreasure(s)
		if why := ValidateStep(got); why != "" {
			t.Errorf("%s: malformed step %+v: %s", c.name, got, why)
			continue
		}
		if got.Kind != c.want.kind {
			t.Errorf("%s: %s (%+v), want kind %s", c.name, got.Describe(), got, c.want.kind)
			continue
		}
		switch {
		case c.want.npc != "" && got.Stop.Npc != c.want.npc:
			t.Errorf("%s: talks to %q, want %q", c.name, got.Stop.Npc, c.want.npc)
		case c.want.custom != "" && got.Custom != c.want.custom:
			t.Errorf("%s: custom %q, want %q", c.name, got.Custom, c.want.custom)
		case c.want.kit != "" && got.KitName != c.want.kit:
			t.Errorf("%s: kit %q, want %q", c.name, got.KitName, c.want.kit)
		case c.want.item != 0 && got.ItemID != c.want.item:
			t.Errorf("%s: item %d, want %d", c.name, got.ItemID, c.want.item)
		}
	}
	// A tab that is not coloured and a journal that has not been read are waits, not stages.
	if s := (Snapshot{Journal: StatusUnknown}); DecidePiratesTreasure(s).Kind != StepWait {
		t.Error("an uncoloured tab does not wait")
	}
	if s := (Snapshot{Journal: StatusInProgress}); DecidePiratesTreasure(s).Kind != StepWait {
		t.Error("an unread journal does not wait")
	}
	if s := (huntSnap{stage: 99, at: huntPortSarim}).snapshot(); DecidePiratesTreasure(s).Kind != StepWait {
		t.Error("an unknown stage does not wait")
	}
}

func huntPlantationTile() tile.Tile { return PlantationCrate }

// The supplies pass asks for what the crossing needs and nothing the account has: coins as a flexible line, the apron only
// while Wydin's job is ahead, the apron worn counting as held.
func TestHuntSuppliesKit(t *testing.T) {
	s := (huntSnap{stage: HuntNeedRum, at: huntPortSarim, inv: map[int]int{CoinsID: 10}}).snapshot()
	step := DecidePiratesTreasure(s)
	if step.Kind != StepProvision || step.KitName != KitHuntSupplies || step.Bank != DraynorBank && step.Bank != NearestBank(huntPortSarim).Tile {
		t.Fatalf("step %+v", step)
	}
	byID := map[int]KitLine{}
	for _, l := range step.Kit {
		byID[l.ID] = l
		if why := ValidateKitLine(l); why != "" {
			t.Error(why)
		}
	}
	coins, apron := byID[CoinsID], byID[ObjWhiteApron]
	if coins.Qty != HuntCoinsQty || coins.Min != HuntCoinsMin || !coins.Stackable || coins.Optional {
		t.Errorf("coins line %+v", coins)
	}
	if apron.Qty != 1 || !apron.Worn || len(apron.Sources) != 1 || apron.Sources[0].Kind != SourceShop || apron.Sources[0].Shop.Npc != "Thessalia" {
		t.Errorf("apron line %+v", apron)
	}
	// The apron is also in the plan's keep list, with everything else the quest carries, so no bank visit banks it.
	for _, id := range []int{CoinsID, ObjKaramjaRum, ObjChestKey, ObjPirateMessage, ObjWhiteApron, ObjSpade, ObjBanana} {
		if !slices.Contains(step.KeepIDs, id) || !slices.Contains(PiratesTreasure.ToolIDs, id) || !slices.Contains(HuntKeepIDs(), id) {
			t.Errorf("object %d is not kept by the supplies pass, the module's tools and HuntKeepIDs", id)
		}
	}
	// Planned over a bank holding both, the pass draws the coins and the apron and asks nothing of any shop.
	plan := PlanProvision(step.Kit, ProvisionState{Pack: map[int]int{CoinsID: 10}, Bank: map[int]int{CoinsID: 5000, ObjWhiteApron: 2}, BankKnown: true,
		FreeSlots: 20, Levels: map[string]int{}, Exhausted: map[string]bool{}})
	if plan.Phase != PhaseBank || len(plan.Tasks) != 0 {
		t.Fatalf("plan %s", plan.Summary())
	}
	got := map[int]int{}
	for _, w := range plan.Withdraw {
		got[w.ID] = w.Count
	}
	if got[CoinsID] != HuntCoinsQty-10 || got[ObjWhiteApron] != 1 {
		t.Errorf("withdraws %v, want %d coins and 1 apron", got, HuntCoinsQty-10)
	}
	// With the bank dry of aprons Thessalia's shelf is the source; with coins it asks no more than the floor of the pack.
	plan = PlanProvision(step.Kit, ProvisionState{Pack: map[int]int{CoinsID: 300}, Bank: map[int]int{CoinsID: 5000}, BankKnown: true,
		FreeSlots: 20, Levels: map[string]int{}, Exhausted: map[string]bool{}})
	foundShop := false
	for _, w := range plan.Withdraw {
		if w.ID == CoinsID && w.Count > 0 && plan.Phase != PhaseBank {
			t.Errorf("coins drawn on top of a held floor in a non-bank phase: %s", plan.Summary())
		}
	}
	for _, task := range plan.Tasks {
		if task.Source.Kind == SourceShop && task.Source.Shop.Npc == "Thessalia" {
			foundShop = true
		}
	}
	if !foundShop && plan.Phase != PhaseBank {
		t.Errorf("no Thessalia task: %s", plan.Summary())
	}
	// A pack that holds the coin floor and the apron needs nothing.
	ok := (huntSnap{stage: HuntNeedRum, at: huntPortSarim, inv: richPack(map[int]int{ObjWhiteApron: 1})}).snapshot()
	if step := DecidePiratesTreasure(ok); step.Kind == StepProvision || step.Kind == StepDeposit {
		t.Errorf("a ready account is sent to a bank: %+v", step)
	}
	// And one whose pack is full is made room in, with the quest's tools kept.
	full := (huntSnap{stage: HuntNeedRum, at: huntPortSarim, inv: richPack(map[int]int{ObjWhiteApron: 1}), free: 2}).snapshot()
	if step := DecidePiratesTreasure(full); step.Kind != StepDeposit || !slices.Contains(step.KeepIDs, ObjKaramjaRum) {
		t.Errorf("a full pack: %+v", step)
	}
}

// The spade kit is the bank, the mule and the spawn beside the park, in the item rule's order.
func TestHuntSpadeKitFollowsTheItemRule(t *testing.T) {
	step := DecidePiratesTreasure((huntSnap{stage: HuntReadNote, at: huntVarrock, inv: richPack(nil)}).snapshot())
	if step.Kind != StepProvision || len(step.Kit) != 1 || step.Kit[0].ID != ObjSpade {
		t.Fatalf("step %+v", step)
	}
	for name, st := range map[string]ProvisionState{
		"bank": {Pack: map[int]int{}, Bank: map[int]int{ObjSpade: 1}, BankKnown: true, FreeSlots: 20},
		"mule": {Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, Mule: map[int]int{ObjSpade: 11}, MuleKnown: true, FreeSlots: 20},
		"park": {Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20},
	} {
		st.Levels, st.Exhausted = map[string]int{}, map[string]bool{}
		plan := PlanProvision(step.Kit, st)
		switch name {
		case "bank":
			if plan.Phase != PhaseBank || len(plan.Withdraw) != 1 || plan.Withdraw[0].ID != ObjSpade {
				t.Errorf("%s: %s", name, plan.Summary())
			}
		case "mule":
			if plan.Phase != PhaseMule || len(plan.MuleAsk) != 1 || plan.MuleAsk[0].ID != ObjSpade {
				t.Errorf("%s: %s", name, plan.Summary())
			}
		case "park":
			if plan.Phase != PhaseSource || len(plan.Tasks) != 1 || plan.Tasks[0].Source.Tile != SpadeSpawn {
				t.Errorf("%s: %s", name, plan.Summary())
			}
		}
	}
}

// The properties that keep an account safe and keep the quest from looping, over every stage, place, pack and memory a Decide can
// be asked about.
func TestDecidePiratesTreasureProperties(t *testing.T) {
	places := map[string]tile.Tile{"port sarim": huntPortSarim, "varrock": huntVarrock, "zambo": huntZambo, "deck": huntDeck, "crate": PlantationCrate}
	stages := []int{HuntFetchRum, HuntNeedRum, HuntRumUnemployed, HuntEmployedNeedRum, HuntRumHeldEmployed, HuntRumInCrate, HuntCrateFull,
		HuntShipped, HuntStoreJob, HuntRumInHand, HuntRumLost, HuntReceivedKey, HuntReadNote}
	customs := map[string]bool{CustomHuntRum: true, CustomHuntShip: true, CustomHuntCollect: true, CustomHuntChest: true, CustomHuntDig: true}
	kits := map[string]bool{KitHuntSupplies: true, KitHuntSpade: true}
	flags := []string{"fail:" + CustomHuntRum, "fail:" + CustomHuntShip, "fail:" + CustomHuntCollect, "ran:" + CustomHuntShip, "blocked:supplies"}
	n := 0
	for _, stage := range stages {
		for pname, at := range places {
			for _, rumHeld := range []int{0, 1} {
				for _, apron := range []string{"none", "held", "worn"} {
					for _, coins := range []int{0, 200, 1000} {
						for _, free := range []int{0, 20} {
							for _, key := range []int{0, 1} {
								for _, spade := range []int{0, 1} {
									for mask := 0; mask < 1<<len(flags); mask++ {
										h := huntSnap{stage: stage, at: at, full: free == 0, inv: map[int]int{CoinsID: coins}, worn: map[int]int{}}
										if rumHeld == 1 {
											h.inv[ObjKaramjaRum] = 1
										}
										switch apron {
										case "held":
											h.inv[ObjWhiteApron] = 1
										case "worn":
											h.worn[ObjWhiteApron] = 1
										}
										if key == 1 {
											h.inv[ObjChestKey] = 1
										}
										if spade == 1 {
											h.inv[ObjSpade] = 1
										}
										for i, f := range flags {
											if mask&(1<<i) == 0 {
												continue
											}
											kind, name, _ := strings.Cut(f, ":")
											switch kind {
											case "fail":
												h.failed = append(h.failed, name)
											case "ran":
												h.ran = append(h.ran, name)
											case "blocked":
												h.blocked = append(h.blocked, "provision:"+KitHuntSupplies, "provision:"+KitHuntSpade)
											}
										}
										step := DecidePiratesTreasure(h.snapshot())
										n++
										ctx := fmt.Sprintf("stage %d at %s rum %d apron %s coins %d free %d key %d spade %d flags %05b", stage, pname, rumHeld, apron, coins, free, key, spade, mask)
										if why := ValidateStep(step); why != "" {
											t.Fatalf("%s: malformed %+v: %s", ctx, step, why)
										}
										if step.Kind == StepCustom && !customs[step.Custom] {
											t.Fatalf("%s: unregistered custom %q", ctx, step.Custom)
										}
										if step.Kind == StepProvision && !kits[step.KitName] {
											t.Fatalf("%s: unknown kit %q", ctx, step.KitName)
										}
										island := OnKaramja(at)
										// A rum on the island never leaves it in the pack: only the steps that put it in the crate (or hire the account
										// that may), and a wait, are possible.
										if island && rumHeld == 1 && stage != HuntReceivedKey && stage != HuntReadNote {
											okStep := step.Kind == StepWait ||
												(step.Kind == StepCustom && step.Custom == CustomHuntShip) ||
												(step.Kind == StepTalk && step.Stop.ID == NpcLuthas)
											if !okStep {
												t.Fatalf("%s: a rum on Karamja and the step is %s", ctx, step.Describe())
											}
										}
										// Wydin hires an account that owns an apron; the back room is searched in it.
										if step.Kind == StepTalk && step.Stop.ID == NpcWydin && apron == "none" {
											t.Fatalf("%s: Wydin asked with no apron", ctx)
										}
										if step.Kind == StepCustom && step.Custom == CustomHuntCollect && apron != "worn" {
											t.Fatalf("%s: the back room searched without the apron worn", ctx)
										}
										if step.Kind == StepEquip && apron != "held" {
											t.Fatalf("%s: equip with the apron %s", ctx, apron)
										}
										if step.Kind == StepCustom && step.Custom == CustomHuntDig && spade == 0 {
											t.Fatalf("%s: dig with no spade", ctx)
										}
										if step.Kind == StepCustom && step.Custom == CustomHuntChest && key == 0 {
											t.Fatalf("%s: the chest opened with no key", ctx)
										}
										// No crossing without the fares: from the mainland, a step that takes the account across the water for the first
										// time (the rum, the plantation, Luthas) needs the coin floor, or is the supplies pass that draws it.
										if !island && stage >= HuntFetchRum && stage <= HuntRumLost && coins < HuntCoinsMin {
											crossing := (step.Kind == StepCustom && (step.Custom == CustomHuntRum || step.Custom == CustomHuntShip)) ||
												(step.Kind == StepTalk && step.Stop.ID == NpcLuthas)
											// Only the states that start the crossing: a store job's ship leg is the recovery of a rum already stashed.
											if crossing && stage != HuntStoreJob {
												t.Fatalf("%s: crossing with %d coins: %s", ctx, coins, step.Describe())
											}
										}
										// The supplies pass keeps the quest's tools.
										if step.Kind == StepProvision || step.Kind == StepDeposit {
											for _, id := range []int{ObjKaramjaRum, ObjWhiteApron, ObjBanana} {
												if !slices.Contains(step.KeepIDs, id) {
													t.Fatalf("%s: %s does not keep object %d", ctx, step.Describe(), id)
												}
											}
										}
										// Both crates empty, or the memory of it: the quest waits and parks, it does not go round again.
										if stage == HuntStoreJob && mask&0b00110 == 0b00110 && apron == "worn" && !island && step.Kind != StepWait {
											t.Fatalf("%s: both crates failed and the step is %s", ctx, step.Describe())
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if n < 50000 {
		t.Fatalf("only %d states swept", n)
	}
}

// Both crates empty is a wait that parks for hours, not the escalating minutes of a plain wait: nothing will move the rum.
func TestHuntNoRumAnywhereParksForHours(t *testing.T) {
	step := DecidePiratesTreasure((huntSnap{stage: HuntStoreJob, at: huntPortSarim, inv: richPack(nil), worn: map[int]int{ObjWhiteApron: 1},
		failed: []string{CustomHuntCollect, CustomHuntShip}}).snapshot())
	if step.Kind != StepWait || step.ParkFor < time.Hour || !strings.Contains(step.Reason, "neither") {
		t.Errorf("step %+v", step)
	}
}

// ---- the module -------------------------------------------------------------------------------------------------------

func TestPiratesTreasureModuleIdentity(t *testing.T) {
	m := PiratesTreasure
	if m.Record.ID != "hunt" || m.Record.Name != "Pirate's Treasure" || m.Record.QuestPoints != 2 || m.Decide == nil || m.ReadStage == nil {
		t.Fatalf("record %+v", m.Record)
	}
	if QuestTabName("hunt") != m.Record.Name {
		t.Errorf("QuestTabName %q", QuestTabName("hunt"))
	}
	if m.MaxDeaths != 2 || len(m.Sustain.FoodIDs) == 0 || m.Bank != DraynorBank || !m.BankSet {
		t.Errorf("module %+v", m)
	}
	f := LadderFacts["hunt"]
	if f.ID != "hunt" || f.Name != m.Record.Name || f.QP != 2 || !f.Filler || f.EstMinutes <= 0 || f.Region == "" || f.Source == "" || f.Notes == "" {
		t.Errorf("ladder row %+v", f)
	}
	if f.Finale || f.TrainSkill != "" || len(f.Gates) != 0 || len(f.Prereqs) != 0 || f.QPNeeded != 0 || f.MinCombat != 0 || len(f.XP) != 0 {
		t.Errorf("a filler with no gate, prerequisite or experience: %+v", f)
	}
	// Under the 60 minute stale slot, the longest a one-at-a-time quest may take.
	if slot := QuestSlot("hunt", 20*time.Minute); slot <= 0 || slot >= 60*time.Minute {
		t.Errorf("slot %v", slot)
	}
}
