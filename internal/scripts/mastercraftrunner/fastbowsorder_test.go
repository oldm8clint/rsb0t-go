package mastercraftrunner

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/levels"
)

// loadOps splits the engine's handled events of a run into its loads: 20 a load (three bank events,
// 14 uses, two fillers, the booth op), the last load without its booth op. The booth op that opened
// the bank for the run's first load is not the run's: it is the one before it.
func loadOps(t *testing.T, ops []string, loads int) [][]string {
	t.Helper()
	if len(ops) > 0 && ops[0] == "booth" {
		ops = ops[1:]
	}
	if want := 20*loads - 1; len(ops) != want {
		t.Fatalf("the engine handled %d events for %d loads, want %d: %v", len(ops), loads, want, ops)
	}
	var out [][]string
	for i := 0; i < loads; i++ {
		end := min((i+1)*20, len(ops))
		out = append(out, ops[i*20:end])
	}
	return out
}

func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// THE OWNER'S ORDER (6 Oct 2026: "after the first withdraw of 14 bows 14 bow string, when it banks it
// should withdraw all bow string, bank the strung bows, withdraw all unstrung bows this will save us
// doing withdraw x inputs"): from an empty pack the first load draws its 14 bows with Withdraw-X and a
// count and the strings with Withdraw-All; every load after it is Withdraw-All of the strings,
// Deposit-All of the strung bows, Withdraw-All of the unstrung bows, and never a count dialog again.
func TestFastBowsSendsTheOwnersOrderAfterTheFirstLoad(t *testing.T) {
	r := newBowRig(t, 14*6, 14*6, levels.XPAtLevel(80)*10)
	const loads = 3
	res := FastBows(r.d, r.q, "Yew longbow", tBowIDs, tBowXP, FastOptions{MaxLoads: loads})
	if res.Reason != "done" || res.Loads != loads {
		t.Fatalf("%s", res.Describe())
	}
	per := loadOps(t, r.e.ops, loads)
	tail := func(booth bool) []string {
		out := append(repeat("use", 14), "close", "close")
		if booth {
			out = append(out, "booth")
		}
		return out
	}
	firstHead := []string{fmt.Sprintf("withdraw-x:%d", tBowU), "count:14", fmt.Sprintf("withdraw-all:%d", tString)}
	steadyHead := []string{fmt.Sprintf("withdraw-all:%d", tString), fmt.Sprintf("deposit-all:%d", tBowStrung), fmt.Sprintf("withdraw-all:%d", tBowU)}
	for i, ops := range per {
		want := append(append([]string(nil), steadyHead...), tail(i < loads-1)...)
		if i == 0 {
			want = append(append([]string(nil), firstHead...), tail(true)...)
		}
		if !reflect.DeepEqual(ops, want) {
			t.Errorf("load %d sent\n  %v\nwant\n  %v", i, ops, want)
		}
	}
	// No count dialog after the first load, and no Withdraw-X.
	for _, ops := range per[1:] {
		for _, op := range ops {
			if strings.HasPrefix(op, "withdraw-x") || strings.HasPrefix(op, "count") {
				t.Errorf("a later load sent %q", op)
			}
		}
	}
	if r.e.refused != 0 {
		t.Fatalf("the engine refused %d event(s)", r.e.refused)
	}
	r.conserve(t)
}

// A run that starts from a finished load (14 strung bows in the pack, as every run after the first of a
// round does) takes the steady order from its very first load: nothing is drawn with Withdraw-X.
func TestFastBowsStartsAFinishedLoadWithTheSteadyOrder(t *testing.T) {
	r := newBowRig(t, 14*6, 14*6, levels.XPAtLevel(80)*10)
	if first := FastBows(r.d, r.q, "Yew longbow", tBowIDs, tBowXP, FastOptions{MaxLoads: 1}); first.Reason != "done" || first.Loads != 1 {
		t.Fatalf("the first run: %s", first.Describe())
	}
	r.e.ops = nil
	if !r.q.Prime(r.d) {
		t.Fatal("the booth op did not go out")
	}
	res := FastBows(r.d, r.q, "Yew longbow", tBowIDs, tBowXP, FastOptions{MaxLoads: 2})
	if res.Reason != "done" || res.Loads != 2 {
		t.Fatalf("%s", res.Describe())
	}
	per := loadOps(t, r.e.ops, 2)
	steadyHead := []string{fmt.Sprintf("withdraw-all:%d", tString), fmt.Sprintf("deposit-all:%d", tBowStrung), fmt.Sprintf("withdraw-all:%d", tBowU)}
	for i, ops := range per {
		if !reflect.DeepEqual(ops[:3], steadyHead) {
			t.Errorf("load %d starts %v, want %v", i, ops[:3], steadyHead)
		}
	}
	if r.e.refused != 0 {
		t.Fatalf("the engine refused %d event(s)", r.e.refused)
	}
	r.conserve(t)
}
