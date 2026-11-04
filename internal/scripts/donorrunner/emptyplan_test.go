package donorrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/tile"
)

// wcfletchLogsPack is the pack the live loop started from (build 3dfb543a, 2026-09-23): a knife,
// an axe and 26 UNNOTED logs, withdrawn by bankRung's "fletch what is already banked" step on the
// same bank visit that decided the hand-off was due. 1511 is content's plain Logs.
func wcfletchLogsPack() []donorplan.ItemRow {
	rows := []donorplan.ItemRow{{Name: "Knife", ID: 946, Count: 1}, {Name: "Rune axe", ID: 1359, Count: 1}}
	for i := 0; i < 26; i++ {
		rows = append(rows, donorplan.ItemRow{Name: "Logs", ID: 1511, Count: 1})
	}
	return rows
}

func wcfletchRules() donorplan.Rules {
	return donorplan.Rules{Keep: []string{"Knife", "Rune axe", "Coins"}}
}

// The live shape, through the real planners: HaulOf counts the 26 logs, and PlanLoad cannot put a
// 26-slot group on a 23-slot trade screen, so the plan is empty while the haul is not. This is the
// condition that looped 144 empty trades in forty minutes.
func TestTheLiveWcfletchPackIsHaulWithNothingToOffer(t *testing.T) {
	pack := wcfletchLogsPack()
	r := wcfletchRules()
	if got := donorplan.HaulOf(pack, r); got != 26 {
		t.Fatalf("HaulOf = %d, want the 26 log slots", got)
	}
	if got := donorplan.PlanLoad(pack, donorplan.DefaultMuleFreeSlots, r); len(got) != 0 {
		t.Fatalf("PlanLoad = %+v, want nothing — a 26-slot group never fits %d free slots", got, donorplan.DefaultMuleFreeSlots)
	}
	o := Options{
		Load:     func() []donorplan.LoadGroup { return donorplan.PlanLoad(pack, donorplan.DefaultMuleFreeSlots, r) },
		HaulLeft: func() int { return donorplan.HaulOf(pack, r) },
	}
	if why := emptyPlanReason(&Deps{}, o, nil); !strings.Contains(why, "26 pack slot(s)") {
		t.Fatalf("emptyPlanReason = %q, want it to name the 26 unofferable slots", why)
	}
}

func TestEmptyPlanReasonStaysQuietWhenThereIsSomethingToOffer(t *testing.T) {
	for _, c := range []struct {
		name string
		load []donorplan.LoadGroup
		haul int
	}{
		{"a plannable load", []donorplan.LoadGroup{{Name: "Arrow shaft", ID: 52, Slots: 1}}, 1},
		{"nothing left at all", nil, 0},
	} {
		o := Options{
			Load:     func() []donorplan.LoadGroup { return c.load },
			HaulLeft: func() int { return c.haul },
		}
		if why := emptyPlanReason(&Deps{}, o, nil); why != "" {
			t.Errorf("%s: emptyPlanReason = %q, want empty", c.name, why)
		}
	}
	// A caller that supplies no Load/HaulLeft is never refused: there is nothing to judge.
	if why := emptyPlanReason(&Deps{}, Options{}, nil); why != "" {
		t.Errorf("no callbacks: emptyPlanReason = %q, want empty", why)
	}
}

// Coins at or below the travel reserve are dropped from the plan by effectiveLoad; a caller that
// still counted that slot as haul would meet the mule to trade nothing, so the guard covers it too.
func TestEmptyPlanReasonCoversCoinsInsideTheReserve(t *testing.T) {
	o := Options{
		Load:      func() []donorplan.LoadGroup { return []donorplan.LoadGroup{{Name: "Coins", ID: 995, Slots: 1}} },
		HaulLeft:  func() int { return 1 },
		CoinsName: "Coins", CoinsID: 995, Reserve: 500,
		CoinsHeld: func() int { return 400 },
	}
	if why := emptyPlanReason(&Deps{}, o, nil); why == "" {
		t.Fatal("coins inside the reserve are the only haul, so there is nothing to offer — want a reason")
	}
}

// CONTROL: on the code before this guard, Run with the live pack walked toward the mule (its
// first action is a MeetWalk), which with no tile to read spins on Delay until the caller stops
// it — so the old result is Stopped, not GaveUp, and it has already committed to the meet. The
// guard ends the hand-off before a single Delay or packet, with a reason the caller logs.
func TestRunRefusesToMeetTheMuleWithAnEmptyPlan(t *testing.T) {
	s := &confirmSession{state: gamestate.New()}
	sent := 0
	s.onSend = func([]byte) { sent++ }
	delays := 0
	d := &Deps{
		Session: s, Chat: chatlog.New(), Log: func(string) {},
		Delay: func(time.Duration) bool {
			delays++
			return delays < 50 // a stop request, so the old code returns instead of spinning
		},
	}
	pack := wcfletchLogsPack()
	r := wcfletchRules()
	res := Run(d, Options{
		Mule: "mule", Stand: tile.Tile{X: 2809, Z: 3441},
		RowsInBank: 17, Drawn: 26,
		Load:     func() []donorplan.LoadGroup { return donorplan.PlanLoad(pack, donorplan.DefaultMuleFreeSlots, r) },
		HaulLeft: func() int { return donorplan.HaulOf(pack, r) },
	})
	if !res.GaveUp || res.Stopped || res.Traded {
		t.Fatalf("Result = %+v, want GaveUp only (so the caller puts the load back and retries later)", res)
	}
	if !strings.Contains(res.Reason, "none of it can go on a trade screen") {
		t.Errorf("Reason = %q, want it to say why", res.Reason)
	}
	if delays != 0 || sent != 0 {
		t.Errorf("the hand-off waited %d time(s) and sent %d packet(s) before refusing; want neither", delays, sent)
	}
}
