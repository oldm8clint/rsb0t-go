package randomevents

import (
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/fighterfood"
)

// THE GUARDIAN'S EAT SHARE AFTER A VIEW LAG (lageat-1018, 18 Oct 2026). The account that died at 04:41:54 was handled as an "event
// monster" (the Watchman), fled it and ate at the walking flee's 60% share. The share is raised, after a lag, to what the NPCs on the
// account could have done in the worst recent episode.

// The Watchman is an event monster because it is one: content's macro_watchman1 to 6 are NPC types 431 to 436 and a steal raises them
// (pickpocket.rs2, stealing.rs2 and trapped_chest.rs2 call macro_randomthieving on an afk_event roll), one tile from the thief and set on
// it at once (opplayer2). The Watchman a Yanille thief robs (npc type 34, yanille_watchman) is a different NPC with the same name.
func TestTheWatchmanEventIsAnEventMonsterAndTheYanilleWatchmanIsNot(t *testing.T) {
	for id := 431; id <= 436; id++ {
		if !hostileEventNpcIDs[id] {
			t.Errorf("npc %d (macro_watchman%d) is not a hostile event monster", id, id-430)
		}
	}
	if hostileEventNpcIDs[34] {
		t.Error("npc 34, the Yanille Watchman a thief robs, is a hostile event monster")
	}
}

func lagHoldView(hp, lag, hits int) holdGroundView {
	return holdGroundView{
		hp: hp, max: 54, hpKnown: true,
		food:     []holdGroundFood{{id: 333, slot: 1, name: "Bass", op: "Eat"}},
		lagTicks: lag, lagHits: hits,
	}
}

func TestHoldGroundEatsAtTheLagShareAfterAViewLag(t *testing.T) {
	for _, c := range []struct {
		name         string
		hp, lag, hit int
		want         string
	}{
		{"35 of 54 on a current view is over the 60% share", 35, 0, 22, ""},
		{"32 of 54 on a current view is under it", 32, 0, 0, "eat"},
		{"35 of 54 after a 12-tick lag with two event monsters on it", 35, 12, 22, "eat"},
		{"38 is the cap (70% of 54)", 38, 12, 22, "eat"},
		{"39 is over it", 39, 12, 22, ""},
		{"35 of 54 after a lag with nothing known to hit it", 35, 12, 0, ""},
		{"33 of 54 against one Watchman after 12 ticks: three volleys of 11", 33, 12, 11, "eat"},
		{"34 of 54 against one Watchman after 12 ticks", 34, 12, 11, ""},
		{"a short stall, one volley of 11, under the share", 35, 3, 11, ""},
	} {
		if got := holdGroundChoice(lagHoldView(c.hp, c.lag, c.hit)).kind; got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	// Never during a dialogue, lag or not.
	v := lagHoldView(20, 12, 22)
	v.dialogueOpen = true
	if holdGroundChoice(v).kind == "eat" {
		t.Error("ate with a dialogue open")
	}
}

type levelResolver map[int]int

func (r levelResolver) Obj(id int) (cachecfg.Obj, bool) { return cachecfg.Obj{}, false }
func (r levelResolver) Npc(id int) (cachecfg.Npc, bool) {
	lvl, ok := r[id]
	return cachecfg.Npc{ID: id, Name: "x", Level: lvl}, ok
}

// The levels the client cache carries stand in for strengths: the event Watchman of combat 61 to 90 is level 120 and hits for 11.
func TestFacingHitSumIsTheLevelEstimateOfWhatFacesTheAccount(t *testing.T) {
	s := gamestate.New()
	setTile(s, 2584, 3288, 0)
	s.Handle(serverprot.UPDATE_PID, payload(func(p *packet.Packet) { p.P2(5); p.P1(0) }))
	res := levelResolver{435: 120, 114: 53}
	// No NPCs at all: one unnamed hitter.
	if got := facingHitSum(s, res, 12, 54, 30); got != fighterfood.LagUnseenHit {
		t.Errorf("nothing facing the account = %d, want the unnamed hitter %d", got, fighterfood.LagUnseenHit)
	}
	addNPCFacing(s, 40, 435, 1, 0, playerFaceBase+5) // the Watchman event, on this account
	if got := facingHitSum(s, res, 12, 54, 30); got != 11 {
		t.Errorf("the event Watchman facing the account = %d, want 11", got)
	}
	// Over the lag cap, or with no lag in the memory, nothing is listed.
	if got := facingHitSum(s, res, 12, 54, 39); got != 0 {
		t.Errorf("39 of 54 (over the cap of 38) = %d, want 0", got)
	}
	if got := facingHitSum(s, res, 0, 54, 30); got != 0 {
		t.Errorf("no lag = %d, want 0", got)
	}
}
