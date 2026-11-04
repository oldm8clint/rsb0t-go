package questplan

import "rsb0t-go/internal/scripts/tile"

// The Holy Grail's quest items live in the bank as often as in the pack, and content hands them out again only to an
// account that holds none (or, for some, even to one that does). The stage 5 trainer banks everything a quest does not
// name in its keep list between two runs, so an account that parked on the Titan came back to the quest with its napkin
// and its whistles in the bank and asked for them all again.
//
// Live 16 Oct 2026, GET /api/bank-all: 23 of 359 banks hold Holy table napkins (441 of them in all) and Magic whistles
// (405), 8 hold Grail bells (15). That is one Galahad visit and one climb to Draynor Manor's top floor (800 steps of
// route from the Catherby bank, whose door throws the account into the same corner of the manor each time) for every
// run the Titan, a rune shortage or a restart cut short. brother_galahad.rs2 offers the napkin to an account holding none
// in its PACK at stage 4 (no bank check) but not at all to one with a napkin in the bank at stage 7 ("inv_total(bank, ...)"):
// the second case loops on his tea for ever. The whistle door gives two more whistles to any account carrying fewer than
// two (quest_grail.rs2 whistledoor), whatever the bank holds. So the bank is read first and what it holds is drawn, and
// only then is the NPC or the door asked.

// GrailNapkinBank is the bank of the napkin leg: the Seers' Village booth, which lies on the way from the Camelot
// teleport's landing (2757,3478) to Galahad's house (2612,3474), where the Catherby booth the rest of the quest uses is
// the far side of the landing.
var GrailNapkinBank = SeersBank

// grailRoomKeep is what a bank visit of the napkin leg leaves in the pack: the quest's tools, the fight kit's runes and
// its food.
func grailRoomKeep() []int {
	return append(append([]int(nil), grailKeep...), grailFoods...)
}

// grailWhistlesNeeded is the whistles the realm trip uses: one blown in at the six heads, one blown out of the realm.
const grailWhistlesNeeded = 2

// grailHasSpare reports whether the pack holds something a deposit would bank.
func grailHasSpare(s Snapshot) bool {
	keep := map[int]bool{}
	for _, id := range grailRoomKeep() {
		keep[id] = true
	}
	for id := range s.InvIDs {
		if !keep[id] {
			return true
		}
	}
	return false
}

// grailNapkinStep is the step that puts the Holy table napkin into the pack, ok false when one is there already.
//
//   - The bank is read first (an unread bank is not an empty one): Galahad gives a second napkin to an account that
//     holds one in the bank at stage 4, and none at stage 7, and the napkin in the bank is the cheaper one.
//   - A full pack cannot take it. brother_galahad_borrow_cloth is `inv_add(inv, holy_table_napkin, 1)` after a message
//     box, with no room check, so a napkin for a pack of 28 never arrives: live 14 Oct to 16 Oct 2026, all five parks on
//     "talk to Brother Galahad" (no progress for 8 ticks) reported "0 free slots" on the line before them. The bank trip
//     comes first when the pack holds something to bank.
func grailNapkinStep(s Snapshot) (Step, bool) {
	if s.HeldID(ObjNapkin) > 0 {
		return Step{}, false
	}
	if !s.BankKnown {
		return Step{Kind: StepScanBank, Bank: GrailNapkinBank, BankSet: true,
			Reason: "a napkin the bank holds is not lost: Galahad hands out another only to an account that owns none anywhere"}, true
	}
	// The napkin is the key of the whistle door and nothing else: an account whose pack and bank already hold both whistles
	// the trip needs has no use for it (and no reason to walk to Galahad's house for one).
	if banked, _ := s.BankedID(ObjMagicWhistle); s.HeldID(ObjMagicWhistle)+banked >= grailWhistlesNeeded {
		return Step{}, false
	}
	if n, _ := s.BankedID(ObjNapkin); n > 0 {
		return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Holy table napkin", ID: ObjNapkin, Qty: 1}},
			Bank: GrailNapkinBank, BankSet: true, KeepIDs: grailRoomKeep(),
			Reason: "the napkin the bank holds, which Galahad counts as the account's own"}, true
	}
	if s.FreeSlots < 1 && grailHasSpare(s) {
		return Step{Kind: StepDeposit, KeepIDs: grailRoomKeep(), Bank: GrailNapkinBank, BankSet: true,
			Reason: "a free slot for Galahad's napkin: his script adds it to a full pack to no effect"}, true
	}
	return Step{Kind: StepTalk, Stop: GalahadStop, UntilItemID: ObjNapkin}, true
}

// grailWhistleStep is the step that puts need Magic whistles into the pack, ok false when the pack holds that many. The
// whistles the bank holds are drawn (up to need, counting the ones in the pack); only an account the bank cannot supply
// goes to Draynor Manor's top floor. The bank is read first. A banked Grail bell for the realm trip is drawn along with
// them (bell: the trip is the one before the realm, where the bell is needed and nothing can be drawn).
func grailWhistleStep(s Snapshot, need int, bell bool, bank tile.Tile, reason string) (Step, bool) {
	held := s.HeldID(ObjMagicWhistle)
	if held >= need {
		return Step{}, false
	}
	if !s.BankKnown {
		return Step{Kind: StepScanBank, Bank: bank, BankSet: true,
			Reason: "whistles the bank holds need no climb to Draynor Manor: the door gives two more to any account carrying fewer"}, true
	}
	banked, _ := s.BankedID(ObjMagicWhistle)
	if banked == 0 {
		return Step{Kind: StepCustom, Custom: CustomGrailWhistles, Reason: reason}, true
	}
	draw := min(need-held, banked)
	items := []WithdrawItem{{Name: "Magic whistle", ID: ObjMagicWhistle, Qty: draw}}
	if st, ok := grailBellDraw(s, bell); ok {
		items = append(items, st)
	}
	return Step{Kind: StepWithdraw, Items: items, Bank: bank, BankSet: true, KeepIDs: grailRoomKeep(),
		Reason: "the whistles the bank holds"}, true
}

// grailBellDraw is the withdrawal line for a Grail bell the bank holds while the pack has none.
func grailBellDraw(s Snapshot, want bool) (WithdrawItem, bool) {
	if !want || s.HeldID(ObjGrailBell) > 0 {
		return WithdrawItem{}, false
	}
	if n, _ := s.BankedID(ObjGrailBell); n > 0 {
		return WithdrawItem{Name: "Grail bell", ID: ObjGrailBell, Qty: 1}, true
	}
	return WithdrawItem{}, false
}

// grailBellStep draws the bell the bank holds before the whistle sets the account down in the realm, where the Fisherman's
// bell lies on the ground for 500 ticks and a bell in the bank saves his dialogue; ok false when there is nothing to draw.
func grailBellStep(s Snapshot, bank tile.Tile) (Step, bool) {
	it, ok := grailBellDraw(s, true)
	if !ok {
		return Step{}, false
	}
	return Step{Kind: StepWithdraw, Items: []WithdrawItem{it}, Bank: bank, BankSet: true, KeepIDs: grailRoomKeep(),
		Reason: "the bell the bank holds"}, true
}

// grailGrailStep is the step before the renewed realm's table: content refuses a second Holy Grail to an account that
// owns one anywhere (`~obj_gettotal(holy_grail)` counts the bank, "taking more than one Holy Grail might be greedy"), so
// the bank is read, and the Grail drawn when it holds one. ok false when the pack holds the Grail or the bank was read
// and holds none.
func grailGrailStep(s Snapshot) (Step, bool) {
	if s.HeldID(ObjHolyGrail) > 0 {
		return Step{}, false
	}
	if !s.BankKnown {
		return Step{Kind: StepScanBank, Bank: CatherbyBank, BankSet: true,
			Reason: "the Grail the bank holds: content refuses a second one to an account that owns it anywhere"}, true
	}
	if n, _ := s.BankedID(ObjHolyGrail); n > 0 {
		return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Holy Grail", ID: ObjHolyGrail, Qty: 1}},
			Bank: CatherbyBank, BankSet: true, KeepIDs: grailRoomKeep()}, true
	}
	return Step{}, false
}
