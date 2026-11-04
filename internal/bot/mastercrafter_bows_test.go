package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The bow round's four-tick hook steps aside when craftFastBows is off, and, once the bank could
// not fund a load, until something new (a kit's notes) is in the pack.
func TestMasterCrafterBowHookStepsAside(t *testing.T) {
	const strung, unstrung, str = 851, 62, 1777
	ids := bowstringrunner.IDs{Unstrung: unstrung, Strung: strung, String: str}
	r := newSimRig(t, []actionsim.Stack{{ID: unstrung, Count: 1}, {ID: str, Count: 1}}, nil, nil)
	bs := &bowStringerBot{strung: map[string]int{}}
	bow := bowstringplan.Bow{Name: "Maple longbow", Level: 55}
	r.b.set.FastBows = false
	hook := r.b.fastBowHook(tile.Tile{X: 2809, Z: 3441}, time.Now(), bs)
	if hook(r.ctx, r.b.deps(r.ctx), bow, ids) {
		t.Fatal("the hook ran with craftFastBows off")
	}
	d := r.b.deps(r.ctx)
	if packHasOther(d, ids) {
		t.Fatal("a bow and a string are the bow's own items")
	}
	r.sim.Give(unstrung+1, 1) // a note from a kit
	if !packHasOther(d, ids) {
		t.Fatal("a kit's note is something new")
	}
}

func TestBowXPTenthsFromContent(t *testing.T) {
	for name, want := range map[string]int{"Yew longbow": 750, "maple longbow": 582, "Shortbow": 50, "Magic longbow": 915} {
		if got, ok := bowstringplan.XPTenths(name); !ok || got != want {
			t.Errorf("%s: %d %v, want %d", name, got, ok, want)
		}
	}
	if _, ok := bowstringplan.XPTenths("Magic crossbow"); ok {
		t.Error("not a stringable bow")
	}
}

// After a run the bank could not fund, the runs wait for a kit and start again once BowStringer
// has landed one, though it banked the kit's notes itself and the pack shows nothing new (live
// 0050e8c9, 5 Oct 2026: the round stayed on the slow trips after its first kit).
func TestMasterFastBowsResumeWhenAKitLands(t *testing.T) {
	fb := &masterFastBows{idle: true, idleBow: "Yew longbow", idleLanded: 3}
	if !fb.waiting("Yew longbow", 3, false) {
		t.Fatal("no kit yet: the runs should still wait")
	}
	if fb.waiting("Yew longbow", 4, false) {
		t.Fatal("a kit landed and was banked: the runs should start again")
	}
	if fb.idle {
		t.Fatal("still idle after the kit landed")
	}
	fb = &masterFastBows{idle: true, idleBow: "Yew longbow", idleLanded: 3}
	if fb.waiting("Yew longbow", 3, true) {
		t.Fatal("a kit's notes in the pack: the runs should start again")
	}
	if (&masterFastBows{}).waiting("Yew longbow", 0, false) {
		t.Fatal("not idle: nothing to wait for")
	}
	// Live on def92a2e: the Yew longbows ran out and BowStringer moved on to the Yew shortbows in
	// the main account's own bank; no kit lands for those, so the wait must not carry over to them.
	fb = &masterFastBows{idle: true, idleBow: "Yew longbow", idleLanded: 3}
	if fb.waiting("Yew shortbow", 3, false) {
		t.Fatal("another bow: the runs should start again at once")
	}
	if fb.idle {
		t.Fatal("still idle after moving on to another bow")
	}
}
