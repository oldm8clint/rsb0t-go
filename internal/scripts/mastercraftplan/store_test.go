package mastercraftplan

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/orderplan"
)

// storeRow is a published row the round may move: tradeable, with a bank note.
func storeRow(item string, n int) StoreRow {
	return StoreRow{Item: item, Count: n, Tradeable: true, HasNote: true}
}

// bankWith is a published bank of filler rows too big to move plus the given small ones.
func bankWith(filler int, small ...StoreRow) []StoreRow {
	rows := make([]StoreRow, 0, filler+len(small))
	for i := 0; i < filler; i++ {
		rows = append(rows, storeRow(fmt.Sprintf("Filler %d", i), 1000))
	}
	return append(rows, small...)
}

// coldInput is a reading in which nothing is protected, nothing was handed out lately, and the
// hand-out record is a week long.
func coldInput(rows []StoreRow) StoreInput {
	return StoreInput{
		Rows: rows, Hot: func(string) bool { return false }, Covered: 7 * 24 * time.Hour,
		Protected: func(string) bool { return false },
	}
}

func itemsOf(lines []Line) []string {
	var out []string
	for _, l := range lines {
		out = append(out, fmt.Sprintf("%d %s", l.Qty, l.Item))
	}
	return out
}

// The live loss, 5 Oct 2026: 265 rows of 288. The round takes the whole of each small cold row,
// lowest count first and by name among equals, enough to reach craftStoreToRows.
func TestPlanStoreAskTakesTheSmallestColdRowsFirst(t *testing.T) {
	set := orderSettings()
	rows := bankWith(256, storeRow("Ruby ring", 5), storeRow("Steel claws", 1), storeRow("Dragon dagger", 2), storeRow("Mime top", 1),
		storeRow("Steel battleaxe", 4), storeRow("Snape grass", 2), storeRow("Limpwurt root", 1), storeRow("Gold ring", 5),
		storeRow("Chocolate cake", 51)) // 264 rows + this one = 265; the 51 is over the size limit
	if n := StoreTotal(rows); n != 265 {
		t.Fatalf("test bank has %d rows", n)
	}
	got := itemsOf(PlanStoreAsk(set, coldInput(rows)))
	want := []string{"1 Limpwurt root", "1 Mime top", "1 Steel claws", "2 Dragon dagger", "2 Snape grass", "4 Steel battleaxe", "5 Gold ring", "5 Ruby ring"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("asked\n%v\nwant\n%v", got, want)
	}
	// 50 rows over the target, only eight movable: it takes the eight and no more.
	if StoreTotal(rows)-set.StoreToRows != 50 {
		t.Fatalf("the target is %d rows from %d", set.StoreToRows, StoreTotal(rows))
	}
}

func TestPlanStoreAskStopsAtTheTargetRows(t *testing.T) {
	set := orderSettings()
	set.StoreAtRows, set.StoreToRows = 10, 6
	rows := bankWith(2, storeRow("A", 1), storeRow("B", 1), storeRow("C", 1), storeRow("D", 1), storeRow("E", 1), storeRow("F", 2), storeRow("G", 3), storeRow("H", 4))
	got := itemsOf(PlanStoreAsk(set, coldInput(rows)))
	if want := []string{"1 A", "1 B", "1 C", "1 D"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("10 rows down to 6 is four rows: %v", got)
	}
	if got := PlanStoreAsk(set, coldInput(bankWith(2, storeRow("A", 1), storeRow("B", 1), storeRow("C", 1), storeRow("D", 1)))); got != nil {
		t.Fatalf("6 rows are at the target already: %v", got)
	}
}

// What never moves: Coins, protected rows, rows handed out lately, rows over the size limit, rows
// the mule cannot hand out (an unreadable name, one a kit cannot carry, one not tradeable, one more
// than a trade holds with no note), rows the caller skips. Parentheses read since 6 Oct 2026
// (orderplan.itemNameOK takes charged names for the run staller's Games necklace(8) ask), so
// "Waterskin(4)" is a movable row; a name with punctuation the grammar refuses is not.
func TestPlanStoreAskLeavesWhatItMust(t *testing.T) {
	set := orderSettings()
	set.StoreAtRows, set.StoreToRows = 5, 0
	rows := []StoreRow{
		storeRow("Coins", 3),
		storeRow("Shark", 4),        // protected
		storeRow("Rune axe", 1),     // hot
		storeRow("Pile of ore", 51), // over craftStoreMaxCount
		storeRow("Waterskin(4)", 1), // charges in parentheses read: movable
		storeRow("Adam full helm (g)", 1),
		storeRow("Pot of flour!", 1),                // the grammar refuses punctuation
		{Item: "Mime top", Count: 1, HasNote: true}, // not tradeable
		{Item: "Fine cloth", Count: 40},             // 40 non-stackable, no note: cannot cross
		{Item: "A name that is much too long for the mule to read", Count: 1, Tradeable: true, HasNote: true},
		storeRow("Left out", 2), // skipped by the caller
		storeRow("Dragon dagger", 2),
		storeRow("Dragon dagger", 3), // a name twice counts once
		storeRow("Zero", 0),
		storeRow("  ", 2),
		storeRow("Steel claws", 1),
	}
	in := coldInput(rows)
	in.Protected = func(item string) bool { return item == "Shark" }
	in.Hot = func(item string) bool { return item == "Rune axe" }
	in.Skip = func(item string) bool { return item == "Left out" }
	got := itemsOf(PlanStoreAsk(set, in))
	if want := []string{"1 Adam full helm (g)", "1 Steel claws", "1 Waterskin(4)", "2 Dragon dagger"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("asked %v, want %v", got, want)
	}
	_, c := storeCandidates(set, in)
	if c.Protected != 2 || c.Hot != 1 || c.Big != 1 || c.Unservable != 4 || c.Skipped != 1 || c.Movable != 4 || c.Total != 13 {
		t.Fatalf("census %+v: %s", c, c)
	}
	// Without a hand-out record every row is in use, and nothing moves.
	in.Hot = nil
	if got := PlanStoreAsk(set, in); got != nil {
		t.Fatalf("no record: asked %v", got)
	}
	// A nil Protected falls back to the derived set: Coins and runes are never moved.
	plain := coldInput([]StoreRow{storeRow("Coins", 3), storeRow("Law rune", 4), storeRow("Mime top", 1)})
	plain.Protected = nil
	set.StoreAtRows, set.StoreToRows = 1, 0
	if got := itemsOf(PlanStoreAsk(set, plain)); !reflect.DeepEqual(got, []string{"1 Mime top"}) {
		t.Fatalf("default protection: %v", got)
	}
}

// One kit is one trade: at most 28 lines and 28 slots (orderplan.DecideKit). A line takes one slot
// when it stacks, holds one item or has a note, and a slot an item otherwise.
func TestPlanStoreAskFitsOneTrade(t *testing.T) {
	set := orderSettings()
	set.StoreAtRows, set.StoreToRows = 40, 0
	var rows []StoreRow
	for i := 0; i < 40; i++ {
		rows = append(rows, storeRow(fmt.Sprintf("Piece %02d", i), 1))
	}
	lines := PlanStoreAsk(set, coldInput(rows))
	if len(lines) != orderplan.KitLinesMax || lines[0].Item != "Piece 00" || lines[27].Item != "Piece 27" {
		t.Fatalf("40 single pieces: %d lines, %v", len(lines), itemsOf(lines))
	}

	// Rows with no note take a slot an item: one piece, a three and six fours make 28 slots exactly,
	// and a five after them (33) does not fit, though it sorts last and nothing else is left to try.
	rows = nil
	for i := 0; i < 6; i++ {
		rows = append(rows, StoreRow{Item: fmt.Sprintf("Plate %d", i), Count: 4, Tradeable: true})
	}
	rows = append(rows, StoreRow{Item: "Heavy", Count: 5, Tradeable: true}, StoreRow{Item: "Triple", Count: 3, Tradeable: true},
		StoreRow{Item: "Zzz one", Count: 1, Tradeable: true})
	lines = PlanStoreAsk(set, coldInput(rows))
	slots := 0
	for _, l := range lines {
		slots += l.Qty
	}
	if len(lines) != 8 || slots != 28 || lines[0].Item != "Zzz one" || lines[1].Item != "Triple" {
		t.Fatalf("%d lines in %d slots: %v", len(lines), slots, itemsOf(lines))
	}
	for _, l := range lines {
		if l.Item == "Heavy" {
			t.Fatalf("a row of five does not fit the slots left: %v", itemsOf(lines))
		}
	}
	// A row that does not fit the slots left is passed over for a later one that does: 27 single
	// pieces, then a two with no note (29 slots), then a stack of 30 (one slot).
	rows = nil
	for i := 0; i < 27; i++ {
		rows = append(rows, StoreRow{Item: fmt.Sprintf("Piece %02d", i), Count: 1, Tradeable: true})
	}
	rows = append(rows, StoreRow{Item: "Duo", Count: 2, Tradeable: true}, StoreRow{Item: "Stack", Count: 30, Tradeable: true, Stackable: true})
	lines = PlanStoreAsk(set, coldInput(rows))
	if len(lines) != 28 || lines[27].Item != "Stack" {
		t.Fatalf("the two is passed over for the stack: %d lines, last %v", len(lines), itemsOf(lines)[len(lines)-1])
	}
	for _, l := range lines {
		if l.Item == "Duo" {
			t.Fatalf("the two does not fit: %v", itemsOf(lines))
		}
	}

	// A stackable takes one slot whatever its count; a non-stackable of 40 with no note is no kit's.
	for _, r := range []StoreRow{
		{Item: "Arrows", Count: 40, Tradeable: true, Stackable: true},
		{Item: "Noted pile", Count: 40, Tradeable: true, HasNote: true},
		{Item: "Plain pile", Count: 40, Tradeable: true},
		{Item: "Plain 28", Count: 28, Tradeable: true},
		{Item: "One", Count: 1, Tradeable: true},
		{Item: "None", Count: 0, Tradeable: true, Stackable: true},
	} {
		want := map[string]int{"Arrows": 1, "Noted pile": 1, "Plain pile": 0, "Plain 28": 28, "One": 1, "None": 0}[r.Item]
		if got := r.Slots(); got != want {
			t.Errorf("%s: %d slots, want %d", r.Item, got, want)
		}
	}
}

// This account's own bank keeps StoreOwnReserve rows free, and takes no more rows than it has room
// for.
func TestPlanStoreAskKeepsThisAccountsBankRoom(t *testing.T) {
	set := orderSettings()
	set.StoreAtRows, set.StoreToRows = 20, 0
	var rows []StoreRow
	for i := 0; i < 20; i++ {
		rows = append(rows, storeRow(fmt.Sprintf("Piece %02d", i), 1))
	}
	in := coldInput(rows)
	in.OwnRows = BankRows - StoreOwnReserve - 7
	if got := PlanStoreAsk(set, in); len(got) != 7 {
		t.Fatalf("room for 7 rows: asked %d", len(got))
	}
	in.OwnRows = BankRows - StoreOwnReserve
	if got := PlanStoreAsk(set, in); got != nil {
		t.Fatalf("no room: %v", got)
	}
	in.OwnRows = BankRows
	if got := PlanStoreAsk(set, in); got != nil {
		t.Fatalf("a full bank: %v", got)
	}
}

func TestStoreReadyThresholdsAndRecord(t *testing.T) {
	set := orderSettings()
	// Two movable rows: not worth a trade of their own (StoreMinMove).
	if r := StoreReady(set, coldInput(bankWith(240, storeRow("Mime top", 1), storeRow("Steel claws", 1)))); r.Ready || r.Move != 2 || !strings.Contains(r.Why, "only 2 can move") {
		t.Fatalf("242 rows, two movable: %+v", r)
	}
	var small []StoreRow
	for i := 0; i < 12; i++ {
		small = append(small, storeRow(fmt.Sprintf("Piece %02d", i), 1))
	}
	rows := bankWith(240, small...)
	in := coldInput(rows)
	if r := StoreReady(set, in); !r.Ready || r.Rows != 252 || r.Move != 12 || r.Census.Movable != 12 || !strings.Contains(r.Why, "252 rows") {
		t.Fatalf("252 rows, twelve movable: %+v", r)
	}
	if r := StoreReady(set, StoreInput{}); r.Ready || !strings.Contains(r.Why, "not published") {
		t.Fatalf("no bank: %+v", r)
	}
	under := coldInput(bankWith(200, storeRow("Mime top", 1)))
	if r := StoreReady(set, under); r.Ready || !strings.Contains(r.Why, "under "+KeyStoreAtRows+" 235") {
		t.Fatalf("201 rows: %+v", r)
	}
	// No hand-out record, or one shorter than a day, is not a reason to move anything.
	noRecord := in
	noRecord.Hot = nil
	if r := StoreReady(set, noRecord); r.Ready || !strings.Contains(r.Why, "hand-out record is not available") {
		t.Fatalf("no record: %+v", r)
	}
	short := in
	short.Covered = 23 * time.Hour
	if r := StoreReady(set, short); r.Ready || !strings.Contains(r.Why, "reaches back only 23h0m0s") {
		t.Fatalf("23 hours of record: %+v", r)
	}
	short.Covered = StoreMinCoverage
	if r := StoreReady(set, short); !r.Ready {
		t.Fatalf("a day of record is enough: %+v", r)
	}
	// Rows but none movable: said, with the census.
	stuck := coldInput(bankWith(240, storeRow("Big pile", 51)))
	if r := StoreReady(set, stuck); r.Ready || !strings.Contains(r.Why, "no row can move") || r.Census.Big != 241 {
		t.Fatalf("nothing small: %+v", r)
	}
	// This account's bank full: said.
	in.OwnRows = BankRows - StoreOwnReserve
	if r := StoreReady(set, in); r.Ready || !strings.Contains(r.Why, "no room") {
		t.Fatalf("a full own bank: %+v", r)
	}
	set.StoreOverflow = false
	in.OwnRows = 0
	if r := StoreReady(set, in); r.Ready || !strings.Contains(r.Why, "off") {
		t.Fatalf("switched off: %+v", r)
	}
}

// The owner's order: gems, bows, pearls, then the storage round, before the later rounds (shop,
// superheat, arrows, herbs); the crystal chest stays last of all.
func TestDecideStoreAfterPearlsBeforeTheLaterRounds(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	set := orderSettings()
	s := readySnap(now)
	s.Stock = stockMap(map[string]int{"Herb id199": 5000, "Iron ore": 5000, "Coal": 50000, "Nature rune": 5000})
	s.MagicLevel, s.SmithingLevel, s.HerbloreLevel, s.HerbloreKnown = 60, 60, 60, true
	var small []StoreRow
	for i := 0; i < 12; i++ {
		small = append(small, storeRow(fmt.Sprintf("Piece %02d", i), 1))
	}
	s.Store = coldInput(bankWith(240, small...))
	// Every later round is ready here: a herb round (5000 grimy guams), a superheat round.
	d := Decide(set, s)
	if d.Mode != ModeStore || d.StoreRows != 12 || !strings.Contains(d.Why, "252 rows") {
		t.Fatalf("the storage round comes before the herb and superheat rounds: %+v", d)
	}
	// Gems, bows and pearls come first.
	g := s
	g.Stock = stockMap(map[string]int{"Uncut sapphire": 600, "Chisel": 1})
	if d := Decide(set, g); d.Mode != ModeGems {
		t.Fatalf("gems first: %+v", d)
	}
	b := s
	b.Stock = stockMap(map[string]int{"Maple longbow": 500, "Bow string": 500})
	if d := Decide(set, b); d.Mode != ModeBows {
		t.Fatalf("bows first: %+v", d)
	}
	p := s
	p.Stock = stockMap(map[string]int{"Oyster pearl": 100000, "Bolts": 5000})
	if d := Decide(set, p); d.Mode != ModePearls {
		t.Fatalf("pearls first: %+v", d)
	}
	// Switched off, paused, backing off: the herb round follows.
	off := set
	off.StoreOverflow = false
	if d := Decide(off, s); d.Mode != ModeHerbs && d.Mode != ModeSuperheat {
		t.Fatalf("store off: %+v", d)
	}
	s.Ledger.StorePauseUntil = now.Add(time.Hour)
	d = Decide(set, s)
	if d.Mode == ModeStore {
		t.Fatalf("paused: %+v", d)
	}
	s.Ledger.StorePauseUntil = time.Time{}
	s.Ledger.Fail(ModeStore, now, set)
	if d := Decide(set, s); d.Mode == ModeStore {
		t.Fatalf("backing off: %+v", d)
	}
	s.Ledger.Succeed(ModeStore)
	// With every other round off and nothing to store, ScarabSwarm's Why says why the store is off.
	quiet := readySnap(now)
	quiet.Stock = stockMap(nil)
	quiet.Store = coldInput(bankWith(100))
	if d := Decide(set, quiet); d.Mode != ModeSwarm || !strings.Contains(d.Why, "store: the mule's bank has 100 rows, under "+KeyStoreAtRows) {
		t.Fatalf("%+v", d)
	}
	// The chest round is after it.
	c := s
	c.Stock = stockMap(map[string]int{ChestKeyItem: 400})
	if d := DecideWithChest(set, c, ChestInput{}); d.Mode != ModeStore {
		t.Fatalf("store before the chest: %+v", d)
	}
	c.Store = StoreInput{}
	if d := DecideWithChest(set, c, ChestInput{}); d.Mode != ModeChest {
		t.Fatalf("the chest after the rest: %+v", d)
	}
}

func TestStoreMayChainAfterAnotherRoundButNotAfterItself(t *testing.T) {
	if !Chain(ModeBows, Decision{Mode: ModeStore}) || !Chain(ModePearls, Decision{Mode: ModeStore}) {
		t.Error("a storage round follows another kind of round at once")
	}
	if Chain(ModeStore, Decision{Mode: ModeStore}) {
		t.Error("a second storage round waits for the mule to republish its bank")
	}
	if !Chain(ModeStore, Decision{Mode: ModeShop}) {
		t.Error("another round follows a storage round at once")
	}
}

func TestLedgerRemembersWhatTheStorageRoundStoredAndWhatTheMuleRefused(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	var l Ledger
	l.NoteStored("Mime top", 1)
	l.NoteStored("Dragon dagger", 2)
	l.NoteStored(" dragon dagger ", 3)
	l.NoteStored("", 4)
	l.NoteStored("Nothing", 0)
	if got := l.StoredNames(); !reflect.DeepEqual(got, []string{"dragon dagger", "mime top"}) || l.Stored["dragon dagger"] != 5 {
		t.Fatalf("stored %v %v", got, l.Stored)
	}
	var none *Ledger
	if none.StoredNames() != nil || none.StoreRefusedNow("x", now) {
		t.Fatal("a nil ledger remembers nothing")
	}
	l.NoteStoreRefused("Odd thing", now)
	if !l.StoreRefusedNow("odd THING", now.Add(StoreRefusedFor-time.Minute)) {
		t.Fatal("refused items are left out for the week")
	}
	if l.StoreRefusedNow("Odd thing", now.Add(StoreRefusedFor)) || len(l.StoreRefused) != 0 {
		t.Fatalf("a refusal that ran out is forgotten: %v", l.StoreRefused)
	}
	l.NoteStoreRefused("Mime top", now)
	l.NoteStored("Mime top", 1) // it was served after all
	if l.StoreRefusedNow("Mime top", now) {
		t.Fatal("an item that was stored is not refused")
	}
	for why, want := range map[string]bool{
		"declined: not an item I hand out":                  true,
		"I keep that one back":                              true,
		"item name has characters an item name cannot have": true,
		"too many to trade and it has no bank note":         true,
		"declined: none in the bank":                        false,
		"hand-out limit for that item is spent":             false,
		"already handed you one recently":                   false,
		"no room for it in this trade":                      false,
		"I cannot identify that item":                       false,
		"hand-outs are switched off":                        false,
		"you have no room in your pack for that":            false,
	} {
		if got := StoreRefusalIsTheItem(why); got != want {
			t.Errorf("%q: %v, want %v", why, got, want)
		}
	}
}

func TestStoreSettingsDecodeAndRefusals(t *testing.T) {
	d := orderSettings()
	if !d.StoreOverflow || d.StoreAtRows != 235 || d.StoreToRows != 215 || d.StoreMaxCount != 50 || d.StoreQuietDays != 7 {
		t.Fatalf("the defaults are the owner's plan: %+v", d)
	}
	s, err := DecodeSettings(map[string]interface{}{
		KeyStoreOverflow: "false", KeyStoreAtRows: "200", KeyStoreToRows: float64(150), KeyStoreMaxCount: 10, KeyStoreQuietDays: "3",
	})
	if err != nil || s.StoreOverflow || s.StoreAtRows != 200 || s.StoreToRows != 150 || s.StoreMaxCount != 10 || s.StoreQuietDays != 3 {
		t.Fatalf("%+v %v", s, err)
	}
	for name, raw := range map[string]map[string]interface{}{
		"at above a bank":      {KeyStoreAtRows: 289},
		"at zero":              {KeyStoreAtRows: 0},
		"to not under at":      {KeyStoreToRows: 235},
		"to negative":          {KeyStoreToRows: -1},
		"max count zero":       {KeyStoreMaxCount: 0},
		"quiet days zero":      {KeyStoreQuietDays: 0},
		"quiet days a year on": {KeyStoreQuietDays: 366},
		"not a bool":           {KeyStoreOverflow: "sometimes"},
		"typo":                 {"craftStoreOverflo": true},
	} {
		if _, err := DecodeSettings(raw); err == nil {
			t.Errorf("%s: accepted %v", name, raw)
		}
	}
	// The storage round alone does not make a MasterCrafter: like the chest and the shop trip it is
	// not one of the rounds that make something.
	if _, err := DecodeSettings(map[string]interface{}{KeyGemCutting: false, KeyBowStringing: false, KeyPearls: false,
		KeySuperheat: false, KeyArrows: false, KeyHerbs: false, KeySmith: false}); err == nil {
		t.Error("every making round off was accepted because the storage round is on")
	}
}

// The kit PlanStoreAsk builds is one the mule's own plan (orderplan.DecideKit) serves in FULL: every
// line, at its whole count, within the 28 slots of one trade, whatever mix of single pieces, stacks,
// noteable and non-noteable rows the bank holds. This pins the slot arithmetic of StoreRow.Slots to the
// mule's, which is the whole reason a round can ask for 28 lines at once.
func TestPlanStoreAskIsAKitTheMuleServesInFull(t *testing.T) {
	set := orderSettings()
	set.StoreAtRows, set.StoreToRows = 1, 0
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	seed := uint32(20261005)
	next := func(n int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>8) % n
	}
	for trial := 0; trial < 300; trial++ {
		var rows []StoreRow
		facts := map[string]orderplan.KitFacts{}
		stock := map[string]int{}
		for i, n := 0, 5+next(80); i < n; i++ {
			r := StoreRow{Item: fmt.Sprintf("Thing %03d", i), Count: 1 + next(40), Tradeable: true}
			switch next(4) {
			case 0:
				r.Stackable = true
			case 1, 2:
				r.HasNote = true
			}
			if next(3) == 0 {
				r.Count = 1
			}
			rows = append(rows, r)
			facts[r.Item] = orderplan.KitFacts{Stackable: r.Stackable, HasNote: r.HasNote}
			stock[r.Item] = r.Count
		}
		lines := PlanStoreAsk(set, coldInput(rows))
		if len(lines) == 0 {
			continue // every row was unservable (a plain pile over 28)
		}
		kit := make([]orderplan.KitLine, 0, len(lines))
		for _, l := range lines {
			kit = append(kit, orderplan.KitLine{Item: l.Item, Qty: l.Qty})
		}
		pol := orderplan.DefaultPolicy([]string{"main"})
		dec := orderplan.DecideKit(now, "main", kit, pol, orderplan.NewLedger(0),
			func(item string) int { return stock[item] },
			func(item string) (orderplan.KitFacts, bool) { f, ok := facts[item]; return f, ok }, 28)
		if dec.Serving() != len(lines) {
			t.Fatalf("trial %d: %d of %d lines served: %+v", trial, dec.Serving(), len(lines), dec.Lines)
		}
		for i, l := range dec.Lines {
			if l.Qty != lines[i].Qty || l.Item != lines[i].Item {
				t.Fatalf("trial %d: line %d is %+v, asked %+v", trial, i, l, lines[i])
			}
		}
		if got := dec.Slots(func(item string) (orderplan.KitFacts, bool) { f, ok := facts[item]; return f, ok }); got > 28 {
			t.Fatalf("trial %d: the kit takes %d slots", trial, got)
		}
	}
	// And the allow list is the only door: a donor the mule does not list is refused every line.
	dec := orderplan.DecideKit(now, "stranger", []orderplan.KitLine{{Item: "Mime top", Qty: 1}}, orderplan.DefaultPolicy([]string{"main"}),
		orderplan.NewLedger(0), func(string) int { return 1 }, nil, 28)
	if dec.Serving() != 0 || dec.Lines[0].Reason != "not on the allow list" {
		t.Fatalf("%+v", dec)
	}
}
