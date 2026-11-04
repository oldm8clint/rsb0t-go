package randomevents

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/protocol/clientprot"
)

func TestPlantFreshFruitProvesPickWithoutChat(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Strange plant"}, objNames: map[int]string{464: "Strange fruit"}})
	s := &overlapSession{state: overlapState()}
	addNPCFacing(s.state, 1, 500, 1, 0, -1)
	setFullInventory(s.state, 3214, [2]int32{464, 3})
	g := NewGuardian(s, nil)
	defer g.Stop()
	s.onSend = func(op byte) {
		if op == clientprot.OPNPC1 {
			setFullInventory(s.state, 3214, [2]int32{464, 4})
		}
	}
	ev := Detect(s.state)
	var lines []string
	if err := solveStrangePlant(s, nil, ev, chatlog.New(), func(line string) { lines = append(lines, line) }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "picked the fruit") || time.Until(g.retryUntil[eventSignature(ev)]) < 10*time.Second {
		t.Fatalf("fresh receipt did not prove pick/protect despawn: %v", lines)
	}
	if !npcStillPresent(s, 1) {
		t.Fatal("fixture must retain the plant during its delayed despawn")
	}
}

func TestPlantReceiptUsesOnlyBackpackAndFreshDelta(t *testing.T) {
	withResolver(t, fakeResolver{objNames: map[int]string{464: "Strange fruit", 995: "Coins"}})
	s := &overlapSession{state: overlapState()}
	setFullInventory(s.state, 5382, [2]int32{464, 500})
	setFullInventory(s.state, 3416, [2]int32{464, 20})
	setFullInventory(s.state, 3214, [2]int32{464, 3}, [2]int32{995, 100})
	before := strangeFruitCount(s)
	setFullInventory(s.state, 5382, [2]int32{464, 501})
	setFullInventory(s.state, 3214, [2]int32{464, 3}, [2]int32{995, 101})
	if before != 3 || strangeFruitCount(s) != before {
		t.Fatal("bank/trade receipts or unrelated backpack changes proved a plant pick")
	}
}
