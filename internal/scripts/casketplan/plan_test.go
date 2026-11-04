package casketplan

import "testing"

func TestPackCounts(t *testing.T) {
	got := PackCounts([]Stack{{ID: 405, Count: 1}, {ID: 405, Count: 1}, {ID: 995, Count: 50}})
	if got[405] != 2 || got[995] != 50 {
		t.Errorf("PackCounts = %v", got)
	}
}

func isCasket405(id int) bool { return id == 405 }

func TestGainedExcludesEveryCasketID(t *testing.T) {
	before := map[int]int{405: 3, 3603: 1, 995: 100}
	after := map[int]int{405: 1, 3603: 1, 995: 150, 1163: 5}
	isCasket := func(id int) bool { return id == 405 || id == 3603 }
	got := Gained(before, after, isCasket)
	if got[405] != 0 || got[3603] != 0 {
		t.Errorf("every casket variant's own count should never appear in gained, got %v", got)
	}
	if got[995] != 50 || got[1163] != 5 {
		t.Errorf("Gained = %v", got)
	}
}

func TestGainedIgnoresIDsThatWentDownOrUnchanged(t *testing.T) {
	before := map[int]int{995: 100}
	after := map[int]int{995: 90}
	if got := Gained(before, after, isCasket405); len(got) != 0 {
		t.Errorf("a decreased id should not appear, got %v", got)
	}
}

func TestBankStepFor(t *testing.T) {
	cases := []struct {
		name string
		s    BankSnapshot
		want BankStep
	}{
		{"noted still held", BankSnapshot{NotedHeld: 2, BankCaskets: 0, FreeSlots: 5}, StepDeposit},
		{"nothing left anywhere", BankSnapshot{BankCaskets: 0, FreeSlots: 5}, StepDone},
		{"bank has more but pack is full", BankSnapshot{BankCaskets: 10, FreeSlots: 0}, StepFull},
		{"withdraw the next load", BankSnapshot{BankCaskets: 10, FreeSlots: 5}, StepWithdraw},
	}
	for _, c := range cases {
		if got := BankStepFor(c.s); got != c.want {
			t.Errorf("%s: BankStepFor = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestFindRowsSortsByCountThenName(t *testing.T) {
	tally := map[string]int{"Coins": 500, "Uncut sapphire": 2, "Uncut ruby": 2}
	rows := FindRows(tally)
	if len(rows) != 3 || rows[0].Name != "Coins" {
		t.Fatalf("FindRows = %+v", rows)
	}
	if rows[1].Name != "Uncut ruby" || rows[2].Name != "Uncut sapphire" {
		t.Errorf("a tie should break alphabetically: %+v", rows)
	}
}

func TestAddFindIgnoresZeroOrNegative(t *testing.T) {
	tally := map[string]int{}
	AddFind(tally, "Coins", 0)
	AddFind(tally, "Coins", -5)
	if len(tally) != 0 {
		t.Errorf("a non-positive find should not be tallied, got %v", tally)
	}
	AddFind(tally, "Coins", 10)
	AddFind(tally, "Coins", 5)
	if tally["Coins"] != 15 {
		t.Errorf("AddFind should accumulate, got %d", tally["Coins"])
	}
}
