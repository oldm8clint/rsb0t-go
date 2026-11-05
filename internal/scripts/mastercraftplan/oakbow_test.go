package mastercraftplan

import "testing"

func TestPlanOakBow(t *testing.T) {
	cases := []struct {
		name string
		h    OakBowHeld
		want OakBowStep
	}{
		{"strung worn", OakBowHeld{StrungWorn: 1}, OakBowHave},
		{"strung in the pack", OakBowHeld{StrungPack: 1, UnstrungMule: 5}, OakBowHave},
		{"both in the pack", OakBowHeld{UnstrungPack: 1, StringPack: 1}, OakBowString},
		{"the bank has both: no ask to the mule", OakBowHeld{BankKnown: true, UnstrungBank: 15, StringBank: 1150, UnstrungMule: 9, StringMule: 190850}, OakBowBank},
		{"bank bow, pack string", OakBowHeld{BankKnown: true, UnstrungBank: 15, StringPack: 1}, OakBowBank},
		{"bank has the bow only: the string comes from the mule", OakBowHeld{BankKnown: true, UnstrungBank: 15, StringMule: 100}, OakBowAsk},
		{"the bank has none: ask the mule", OakBowHeld{BankKnown: true, UnstrungMule: 3, StringMule: 190850}, OakBowAsk},
		{"bank unread, mule has both", OakBowHeld{UnstrungMule: 3, StringMule: 10}, OakBowAsk},
		{"nobody", OakBowHeld{BankKnown: true}, OakBowNone},
		{"no bow anywhere", OakBowHeld{BankKnown: true, StringBank: 5, StringMule: 5}, OakBowNone},
	}
	for _, c := range cases {
		if got := PlanOakBow(c.h); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestOakBowAskLinesAskOnlyForWhatIsMissing(t *testing.T) {
	h := OakBowHeld{UnstrungMule: 3, StringMule: 10}
	lines := OakBowAskLines(h, "Shortbow id50", "Bow string")
	if len(lines) != 2 || lines[0].Item != "Shortbow id50" || lines[1].Item != "Bow string" {
		t.Fatalf("%+v", lines)
	}
	h.StringBank = 2
	if lines := OakBowAskLines(h, "Shortbow id50", "Bow string"); len(lines) != 1 || lines[0].Item != "Shortbow id50" {
		t.Fatalf("a string in the bank is not asked for: %+v", lines)
	}
}
