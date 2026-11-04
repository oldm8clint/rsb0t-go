package donorrunner

import (
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/gamestate"
)

// Content 289 gives "Bones" to several objects, and the members' variants (280-283) come before the 526 every bank holds.
// UnnoteKit resolved the name to the FIRST un-noted object, drew that one back from the bank and found none, so every
// "Bones" kit ask (the Prayer rungs' bulk ask of the mule, 16 Oct 2026) would have left all its bones banked and reported
// none in the pack. The item to draw back is the one the bank holds once the notes are deposited.
func TestUnnoteKitDrawsBackTheBonesTheBankHolds(t *testing.T) {
	cfg := kitConfig(t)
	if first, _ := nonNotedID(cfg, "Bones"); first == kitBones {
		t.Skipf("the first un-noted Bones is %d now: the shared name this test is about is gone", first)
	}
	noteBones := noteOf(cfg, kitBones)
	sim := actionsim.New(simObjs(cfg), []actionsim.Stack{{ID: noteBones, Count: 800}})
	sim.OpenBank(nil)
	d := &Deps{Session: sim, Config: cfg, Components: actionsim.Components(), Log: func(string) {}, Delay: fastDelay}
	res := UnnoteKit(d, actions.NewBank(sim, d.Components), []string{"Bones"})
	if res.Unnoted["Bones"] != 28 || res.Banked["Bones"] != 772 {
		t.Fatalf("unnoted %v banked %v, want 28 drawn back (a pack) and 772 banked", res.Unnoted, res.Banked)
	}
	if count(sim.Pack(), kitBones) != 28 || count(sim.Pack(), noteBones) != 0 || count(sim.Bank(), kitBones) != 772 {
		t.Fatalf("pack %+v bank %+v", sim.Pack(), sim.Bank())
	}
}

// The pure choice: of the un-noted objects of a name, the one the bank holds; the first usable one when it holds none.
func TestBankHeldIDPrefersTheObjectTheBankHolds(t *testing.T) {
	ids := []int32{280, 281, 526, 2530}
	held := func(have ...int32) func(int32) bool {
		return func(id int32) bool {
			for _, h := range have {
				if h == id {
					return true
				}
			}
			return false
		}
	}
	for _, c := range []struct {
		name string
		have []int32
		want int32
	}{{"the bank holds the 526", []int32{526}, 526}, {"holds two: the first by id", []int32{2530, 526}, 526}, {"holds none: the first usable", nil, 280}} {
		if got, ok := pickBankHeldID(ids, held(c.have...)); !ok || got != c.want {
			t.Errorf("%s: %d %v, want %d", c.name, got, ok, c.want)
		}
	}
	if _, ok := pickBankHeldID(nil, held()); ok {
		t.Error("no ids resolved")
	}
	_ = gamestate.SlotItem{}
}
