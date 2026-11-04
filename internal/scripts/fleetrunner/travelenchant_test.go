package fleetrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/webwalk"
)

const (
	testBase  = int32(5001) // the piece cast on
	testFinal = int32(5002) // the piece that results
)

// enchantRig is a walk enchant over the simulated session: the pack holds n base pieces, Magic is 60, the clock is the
// test's and the server's work (the piece changing in its slot, a tick passing) is done by the test.
type enchantRig struct {
	sim *actionsim.Session
	d   *Deps
	tr  *Travel
	now time.Time
}

func newEnchantRig(t *testing.T, pieces int) *enchantRig {
	t.Helper()
	sim := actionsim.New(actionsim.Objs{}, nil)
	for i := 0; i < pieces; i++ {
		sim.Give(testBase, 1)
	}
	setStat(sim, fleetplan.MagicStat, 200000, 60)
	r := &enchantRig{sim: sim, now: time.Unix(1_000_000, 0)}
	r.d = &Deps{Session: sim, Log: func(string) {}, Delay: func(time.Duration) bool { return true }}
	r.tr = &Travel{Now: func() time.Time { return r.now }}
	r.tr.ArmEnchant(r.d, EnchantWalk{
		Spell: "Lvl-1 Enchant", BaseID: testBase, FinalID: testFinal,
		Refusal: func(said string) string {
			if strings.Contains(said, "You do not have enough") {
				return "runes"
			}
			return ""
		},
	})
	return r
}

// TickLen is one server tick: a PLAYER_INFO the client counts, and 600 ms on the test's clock.
func (r *enchantRig) TickLen(n int) {
	for i := 0; i < n; i++ {
		r.sim.State().Handle(serverprot.PLAYER_INFO, []byte{0})
		r.now = r.now.Add(600 * time.Millisecond)
	}
}

// enchantOne is the server doing a cast: the base piece becomes the final one, in its slot, and the Magic XP comes.
func (r *enchantRig) enchantOne() {
	r.sim.Remove(testBase, 1)
	r.sim.Give(testFinal, 1)
	_, xp := statOf(r.d, fleetplan.MagicStat)
	setStat(r.sim, fleetplan.MagicStat, xp+175, 60)
}

func statOf(d *Deps, stat int) (level, xp int) {
	sk, _ := d.Session.State().Skill(stat)
	return sk.Level, sk.XP
}

// farPoll is a walk poll with the click far ahead and not in this tick.
func enchFarPoll() webwalk.AsidePoll {
	return webwalk.AsidePoll{Me: webwalk.Point{X: 2980, Z: 3369}, Target: webwalk.Point{X: 2950, Z: 3369}, SinceClick: 2 * time.Second}
}

// THE CAST: sent on the first piece with the clear-move in front of it, once; the next only after the first showed
// and two more ticks passed; confirmed by the final piece rising; counted by EnchantWalkTake.
func TestWalkEnchantCastsAtTheSpellsOwnPaceAndCountsThem(t *testing.T) {
	r := newEnchantRig(t, 5)
	r.tr.enchantPoll(r.d, enchFarPoll())
	got := castsSent(r.sim)
	spell, _ := actions.ItemSpellComponent("Lvl-1 Enchant")
	if len(got) != 1 || got[0].obj != int(testBase) || got[0].slot != 0 || got[0].com != PlayerPack || got[0].spell != spell {
		t.Fatalf("casts %+v, want one Lvl-1 Enchant (%d) on the base piece in slot 0", got, spell)
	}
	if len(clearsSent(r.sim)) != 1 {
		t.Errorf("%d clear-moves, want one in front of the cast", len(clearsSent(r.sim)))
	}
	// The cast is on its way: nothing more while it is unseen, whatever the clock.
	r.TickLen(1)
	r.tr.enchantPoll(r.d, enchFarPoll())
	if n := len(castsSent(r.sim)); n != 1 {
		t.Fatalf("%d casts while the first had not shown", n)
	}
	// It shows: the piece changes. The tick it is seen on is the spell's; two more must pass before the next.
	r.enchantOne()
	r.tr.enchantPoll(r.d, enchFarPoll())
	if n := len(castsSent(r.sim)); n != 1 {
		t.Fatalf("%d casts on the tick the first was seen", n)
	}
	r.TickLen(1)
	r.tr.enchantPoll(r.d, enchFarPoll())
	if n := len(castsSent(r.sim)); n != 1 {
		t.Fatalf("%d casts one tick after the first landed", n)
	}
	r.TickLen(1)
	r.tr.enchantPoll(r.d, enchFarPoll())
	if n := len(castsSent(r.sim)); n != 2 {
		t.Fatalf("%d casts two ticks after the first landed, want the second", n)
	}
	r.enchantOne()
	res := r.tr.EnchantWalkTake(r.d)
	if res.Sent != 2 || res.Landed != 2 || res.Left != 3 || res.XP != 350 {
		t.Errorf("result %+v, want 2 sent, 2 landed, 3 left, +350 XP", res)
	}
	if r.tr.enchantActive() || r.tr.ench.on {
		t.Error("Take left the enchant armed")
	}
	if again := r.tr.EnchantWalkTake(r.d); again != (EnchantWalkResult{}) {
		t.Errorf("a second Take returned %+v", again)
	}
}

// The walk's own gates: no cast in the tick of a click, none when the click's target is near and the walk will click
// again inside the delay, one at a leg's end three tiles out, none in a fight or with an interface open.
func TestWalkEnchantRespectsTheWalk(t *testing.T) {
	r := newEnchantRig(t, 5)
	click := enchFarPoll()
	click.SinceClick = 0
	r.tr.enchantPoll(r.d, click)
	near := enchFarPoll()
	near.Target = webwalk.Point{X: 2974, Z: 3369} // 6 tiles ahead, walking: under seven
	r.tr.enchantPoll(r.d, near)
	if n := len(castsSent(r.sim)); n != 0 {
		t.Fatalf("%d casts with a click this tick or the next click inside the delay", n)
	}
	end := near
	end.Target, end.LegEnd = webwalk.Point{X: 2977, Z: 3369}, true // 3 tiles to the leg's end
	r.tr.enchantPoll(r.d, end)
	if n := len(castsSent(r.sim)); n != 1 {
		t.Fatalf("%d casts at the leg's end three tiles out, want one", n)
	}
	if !r.tr.enchantPoll(r.d, enchFarPoll()) {
		t.Error("a pending cast gave the walk's casts back")
	}
}

// A cast that never shows is taken as dropped after four ticks and widens the gap; three in a row end the walk's
// enchanting as stalled and hand the poll back to the walk's other casts.
func TestWalkEnchantStallsAfterThreeDroppedCasts(t *testing.T) {
	r := newEnchantRig(t, 5)
	for want := 1; want <= 3; want++ {
		if !r.tr.enchantPoll(r.d, enchFarPoll()) && want < 3 {
			t.Fatalf("handed the poll back after %d drops", want-1)
		}
		if n := len(castsSent(r.sim)); n != want {
			t.Fatalf("%d casts, want %d", n, want)
		}
		r.TickLen(4) // the cast does not show
		r.tr.enchantPoll(r.d, enchFarPoll())
		r.TickLen(5)
	}
	if r.tr.enchantActive() || r.tr.ench.reason != "stalled" {
		t.Errorf("after three drops: active %v, reason %q", r.tr.enchantActive(), r.tr.ench.reason)
	}
	if r.tr.enchantPoll(r.d, enchFarPoll()) {
		t.Error("a stalled enchant kept the walk's casts")
	}
	if res := r.tr.EnchantWalkTake(r.d); res.Reason != "stalled" || res.Landed != 0 || res.Sent != 3 {
		t.Errorf("result %+v, want 3 sent, none landed, stalled", res)
	}
}

// A refusal in the chat ends it at once with the reason, so the round does not ask for more.
func TestWalkEnchantStopsOnARefusal(t *testing.T) {
	r := newEnchantRig(t, 5)
	chat := chatlog.New()
	r.d.Chat = chat
	r.tr.enchantPoll(r.d, enchFarPoll())
	chat.Record("You do not have enough Cosmic Runes to cast this spell.")
	r.TickLen(1)
	if r.tr.enchantPoll(r.d, enchFarPoll()) {
		t.Error("a refused enchant kept the walk's casts")
	}
	if res := r.tr.EnchantWalkTake(r.d); res.Reason != "runes" || res.Sent != 1 || res.Landed != 0 {
		t.Errorf("result %+v, want 1 sent, none landed, runes", res)
	}
}

// With no piece left the enchant is done and the walk goes on with whatever else it casts; the Take says so.
func TestWalkEnchantIsDoneWhenNoPieceIsLeft(t *testing.T) {
	r := newEnchantRig(t, 1)
	r.tr.enchantPoll(r.d, enchFarPoll())
	r.enchantOne()
	r.TickLen(1)
	r.tr.enchantPoll(r.d, enchFarPoll()) // seen, lands
	r.TickLen(2)
	if r.tr.enchantPoll(r.d, enchFarPoll()) {
		t.Error("an enchant with no piece left kept the walk's casts")
	}
	res := r.tr.EnchantWalkTake(r.d)
	if res.Reason != "done" || res.Landed != 1 || res.Left != 0 {
		t.Errorf("result %+v, want 1 landed, none left, done", res)
	}
}

// A walk with only the enchant armed gets its poll hook; one with it done or never armed gets none; and a nil Travel
// is safe throughout.
func TestWalkEnchantGivesAWalkItsPollOnlyWhileArmed(t *testing.T) {
	r := newEnchantRig(t, 2)
	if r.tr.walkAside(r.d) == nil {
		t.Fatal("armed: no poll hook")
	}
	r.tr.EnchantWalkTake(r.d)
	if r.tr.walkAside(r.d) != nil {
		t.Fatal("taken: a poll hook")
	}
	var nilTravel *Travel
	nilTravel.ArmEnchant(r.d, EnchantWalk{Spell: "Lvl-1 Enchant", BaseID: 1, FinalID: 2})
	if nilTravel.enchantActive() || nilTravel.EnchantWalkTake(r.d) != (EnchantWalkResult{}) {
		t.Fatal("a nil Travel enchanted")
	}
	(&Travel{}).ArmEnchant(r.d, EnchantWalk{})
}

// The Take waits for a cast still on its way, and counts it when it shows.
func TestWalkEnchantTakeWaitsForTheLastCast(t *testing.T) {
	r := newEnchantRig(t, 3)
	r.tr.enchantPoll(r.d, enchFarPoll())
	waits := 0
	r.d.Delay = func(time.Duration) bool {
		if waits++; waits == 3 {
			r.enchantOne()
		}
		r.TickLen(0)
		return true
	}
	res := r.tr.EnchantWalkTake(r.d)
	if res.Landed != 1 || res.Sent != 1 || res.Left != 2 {
		t.Errorf("result %+v, want the cast that showed during the wait counted", res)
	}
}
