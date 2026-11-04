package acquiretask

import (
	"reflect"
	"testing"
)

func TestShortfall(t *testing.T) {
	cases := []struct {
		held, need int32
		want       int32
	}{
		{0, 5, 5},
		{3, 5, 2},
		{5, 5, 0},
		{9, 5, 0}, // held more than needed: never negative
	}
	for _, c := range cases {
		if got := Shortfall(c.held, c.need); got != c.want {
			t.Errorf("Shortfall(%d, %d) = %d, want %d", c.held, c.need, got, c.want)
		}
	}
}

func TestPlan_AlreadyHeld(t *testing.T) {
	steps := Plan(5, Need{Item: "Pot", Count: 5}, 100, 100)
	if steps != nil {
		t.Fatalf("Plan with enough already held = %v, want nil", steps)
	}
	steps = Plan(10, Need{Item: "Pot", Count: 5}, 0, 0)
	if steps != nil {
		t.Fatalf("Plan with more than enough already held = %v, want nil", steps)
	}
}

func TestPlan_BankCoversFully(t *testing.T) {
	got := Plan(0, Need{Item: "Bucket", Count: 4}, 10, 10)
	want := []Step{{Source: SourceBank, Count: 4}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Plan = %v, want %v", got, want)
	}
}

func TestPlan_BankPartialThenShop(t *testing.T) {
	got := Plan(0, Need{Item: "Bucket", Count: 10}, 3, 20)
	want := []Step{
		{Source: SourceBank, Count: 3},
		{Source: SourceShop, Count: 7},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Plan = %v, want %v", got, want)
	}
}

func TestPlan_ShopOnly(t *testing.T) {
	got := Plan(0, Need{Item: "Bucket", Count: 5}, 0, 20)
	want := []Step{{Source: SourceShop, Count: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Plan = %v, want %v", got, want)
	}
}

func TestPlan_ShopStockCapped_TrailingGather(t *testing.T) {
	got := Plan(0, Need{Item: "Bucket", Count: 10}, 0, 4)
	want := []Step{
		{Source: SourceShop, Count: 4},
		{Source: SourceGather, Count: 6},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Plan = %v, want %v", got, want)
	}
}

func TestPlan_NoSourcesAtAll(t *testing.T) {
	got := Plan(2, Need{Item: "Rune essence", Count: 30}, 0, 0)
	want := []Step{{Source: SourceGather, Count: 28}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Plan = %v, want %v", got, want)
	}
}

func TestPlan_NeverOverAsksASourceWithSurplus(t *testing.T) {
	// The bank has plenty, but only the shortfall (2) should ever be requested from it.
	got := Plan(3, Need{Item: "Bucket", Count: 5}, 1000, 1000)
	want := []Step{{Source: SourceBank, Count: 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Plan = %v, want %v", got, want)
	}
}

func TestHeld(t *testing.T) {
	if !Held(5, 5) {
		t.Error("Held(5, 5) = false, want true")
	}
	if !Held(6, 5) {
		t.Error("Held(6, 5) = false, want true")
	}
	if Held(4, 5) {
		t.Error("Held(4, 5) = true, want false")
	}
}

func TestHasAll(t *testing.T) {
	needs := []Need{{Item: "Pot", Count: 2}, {Item: "Bucket", Count: 1}}
	if !HasAll(needs, map[string]int32{"Pot": 2, "Bucket": 3}) {
		t.Error("HasAll = false, want true")
	}
	if HasAll(needs, map[string]int32{"Pot": 1, "Bucket": 3}) {
		t.Error("HasAll = true, want false (Pot short)")
	}
	if HasAll(needs, map[string]int32{"Pot": 2}) {
		t.Error("HasAll = true, want false (Bucket missing entirely)")
	}
}

func TestStepString(t *testing.T) {
	cases := []struct {
		step Step
		want string
	}{
		{Step{Source: SourceBank, Count: 4}, "withdraw 4 from bank"},
		{Step{Source: SourceShop, Count: 7}, "buy 7 from shop"},
		{Step{Source: SourceGather, Count: 6}, "6 still short — must be gathered"},
	}
	for _, c := range cases {
		if got := c.step.String(); got != c.want {
			t.Errorf("Step.String() = %q, want %q", got, c.want)
		}
	}
}
