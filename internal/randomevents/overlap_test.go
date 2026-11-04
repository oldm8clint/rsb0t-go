package randomevents

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// The scheduler uses real decoded state; only the wire sender is captured. No
// synthetic routing or collision result is used to claim a successful walk.
type overlapSession struct {
	state    *gamestate.State
	sent     []byte
	onSend   func(byte)
	counters anticheat.Counters
}

func (s *overlapSession) State() *gamestate.State     { return s.state }
func (s *overlapSession) Cheats() *anticheat.Counters { return &s.counters }
func (s *overlapSession) Send(build func(*packet.Packet)) error {
	p := packet.NewWriter(256)
	build(p)
	op := p.Data[0]
	s.sent = append(s.sent, op)
	if s.onSend != nil {
		s.onSend(op)
	}
	return nil
}

func overlapState() *gamestate.State {
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	s.Handle(serverprot.UPDATE_PID, payload(func(p *packet.Packet) { p.P2(5); p.P1(0) }))
	return s
}

type facingNPC struct {
	npcSpec
	face int
}

func addFacingNPCs(s *gamestate.State, specs ...facingNPC) {
	fields := []bitField{{8, 0}}
	for _, n := range specs {
		fields = append(fields, bitField{14, uint32(n.index)}, bitField{11, uint32(n.typeID)},
			bitField{5, uint32(n.dx & 31)}, bitField{5, uint32(n.dz & 31)}, bitField{1, 0}, bitField{1, 1})
	}
	fields = append(fields, bitField{14, 16383})
	data := bitPack((8+37*len(specs)+14+7)/8, fields...)
	for _, n := range specs {
		face := uint16(n.face)
		data = append(data, 4, byte(face>>8), byte(face))
	}
	s.Handle(serverprot.NPC_INFO, data)
}

func TestDialogOwnershipBeatsDistanceAndUnconfirmedPreference(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie", 501: "Drunken Dwarf"}})
	s := overlapState()
	addFacingNPCs(s,
		facingNPC{npcSpec{1, 500, 1, 0}, 32768 + 9},  // someone else's nearest genie
		facingNPC{npcSpec{2, 500, 2, 0}, -1},         // previous, unconfirmed choice
		facingNPC{npcSpec{3, 501, 10, 0}, 32768 + 5}, // our dwarf
	)
	for _, ev := range []*Event{Detect(s), DetectPreferring(s, 2), DetectPreferring(s, 1)} {
		if ev == nil || ev.NpcIndex != 3 {
			t.Fatalf("selection = %+v, want our dwarf despite nearer unknown/rejected owners", ev)
		}
	}
}

func TestUnknownDialogOwnerStillNeedsChecking(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie"}})
	s := overlapState()
	heardOwnGenie(s) // without it an unknown-owner Genie is someone else's (genieMayBeOurs)
	addNPCFacing(s, 1, 500, 1, 0, -1)
	if ev := Detect(s); ev == nil || ev.NpcIndex != 1 {
		t.Fatalf("unknown owner = %+v, must remain a candidate", ev)
	}
	addNPCFacing(s, 1, 500, 1, 0, 32768+9)
	if ev := DetectPreferring(s, 1); ev != nil {
		t.Fatalf("confirmed other owner = %+v, preference must not restore it", ev)
	}
}

func TestPreferenceCannotSelectOutOfRangeDialog(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie"}})
	s := overlapState()
	addNPC(s, 1, 500, -16, 0)
	if ev := DetectPreferring(s, 1); ev != nil {
		t.Fatalf("out-of-range preferred NPC = %+v", ev)
	}
}

func TestGuardianRejectsSeveralNPCsWithoutHidingTheNext(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Strange plant"}})
	s := &overlapSession{state: overlapState()}
	addNPCs(s.state, npcSpec{1, 500, 1, 0}, npcSpec{2, 500, 2, 0}, npcSpec{3, 500, 3, 0})
	g := NewGuardian(s, nil)
	defer g.Stop()
	for _, index := range []int{1, 2} {
		markPlantRejected(s, index)
	}
	if len(g.rejectedUntil) != 2 {
		t.Fatal("second rejection overwrote the first")
	}
	if ev := g.nextEvent(); ev == nil || ev.NpcIndex != 3 {
		t.Fatalf("next event = %+v, want third plant", ev)
	}
	if !g.Busy() || !g.Pending() || !Busy(s) {
		t.Fatal("script gates must notice the remaining non-rejected event")
	}
	markPlantRejected(s, 3)
	if g.Busy() || g.Pending() || Busy(s) {
		t.Fatal("all rejected events must release the script")
	}
}

func TestCooldownIsPerNPCAndDoesNotHideAnotherOfTheSameName(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie"}})
	s := &overlapSession{state: overlapState()}
	heardOwnGenie(s.state)
	addNPCs(s.state, npcSpec{1, 500, 1, 0}, npcSpec{2, 500, 2, 0})
	g := NewGuardian(s, nil)
	defer g.Stop()
	first := g.nextEvent()
	g.cooldownUntil[eventSignature(first)] = time.Now().Add(time.Minute)
	if next := g.nextEvent(); next == nil || next.NpcIndex != 2 {
		t.Fatalf("next event = %+v, first NPC cooldown hid the second", next)
	}
	if !g.Busy() || !g.Pending() {
		t.Fatal("a cooled-down nearest NPC must not hide another from script gates")
	}
}

func TestDeferredGrowingPlantLetsOtherEventsRunAndRemainsRetryable(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Strange plant", 501: "Genie"}})
	s := &overlapSession{state: overlapState()}
	playSound(s.state, gamestate.SoundTriffidAppear) // both events are this account's own
	heardOwnGenie(s.state)
	addNPCs(s.state, npcSpec{1, 500, 1, 0}, npcSpec{2, 501, 2, 0})
	g := NewGuardian(s, nil)
	defer g.Stop()
	plant := g.nextEvent()
	g.stickyPlantIndex = 1
	deferEvent(s, plant, 4*time.Second)
	if next := g.nextEvent(); next == nil || next.NpcIndex != 2 {
		t.Fatalf("waiting on plant hid genie: %+v", next)
	}
	g.retryUntil[eventSignature(plant)] = time.Now().Add(-time.Second)
	if next := g.nextEvent(); next == nil || next.NpcIndex != 1 {
		t.Fatalf("grown plant must be retried: %+v", next)
	}
}

func TestRejectionExpiresAndIsDiscardedOnNPCReplacement(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie", 501: "Drunken Dwarf"}})
	s := &overlapSession{state: overlapState()}
	heardOwnGenie(s.state)
	heardOwnEvent(s.state, "Drunken Dwarf")
	addNPC(s.state, 1, 500, 1, 0)
	g := NewGuardian(s, nil)
	defer g.Stop()
	markPlantRejected(s, 1)
	g.rejectedUntil[npcIdentity{1, 500}] = time.Now().Add(-time.Second)
	if next := g.nextEvent(); next == nil {
		t.Fatal("expired rejection still suppressed the NPC")
	}
	markPlantRejected(s, 1)
	addNPC(s.state, 1, 501, 1, 0)
	if next := g.nextEvent(); next == nil || next.NpcType != 501 {
		t.Fatalf("replacement NPC inherited rejection: %+v", next)
	}
	if len(g.rejectedUntil) != 0 {
		t.Fatal("old identity should be removed after replacement")
	}
}

func TestCooledDownNPCDoesNotHideInventoryEvent(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie"}, objNames: map[int]string{831: "Lamp"}})
	s := &overlapSession{state: overlapState()}
	heardOwnGenie(s.state)
	addNPC(s.state, 1, 500, 1, 0)
	setFullInventory(s.state, 3214, [2]int32{831, 1})
	g := NewGuardian(s, nil)
	defer g.Stop()
	g.cooldownUntil[eventSignature(g.nextEvent())] = time.Now().Add(time.Minute)
	if next := g.nextEvent(); next == nil || next.Kind != KindBoxLamp {
		t.Fatalf("inventory lamp starved by NPC cooldown: %+v", next)
	}
}

func TestAngryPlantPreemptsPreferredDialogueButNotForAnotherOwner(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie", 501: "Strange plant"}, npcOps: map[int][5]string{501: {"", "Attack"}}})
	s := overlapState()
	heardOwnGenie(s)
	addFacingNPCs(s, facingNPC{npcSpec{1, 500, 1, 0}, -1}, facingNPC{npcSpec{2, 501, 10, 0}, 32768 + 5})
	if next := DetectPreferring(s, 1); next == nil || next.Kind != KindEvade || next.NpcIndex != 2 {
		t.Fatalf("angry plant did not preempt: %+v", next)
	}
	addFacingNPCs(s, facingNPC{npcSpec{1, 500, 1, 0}, -1}, facingNPC{npcSpec{2, 501, 10, 0}, 32768 + 9})
	if next := DetectPreferring(s, 1); next == nil || next.Kind != KindDialogNpc || next.NpcIndex != 1 {
		t.Fatalf("other owner's plant triggered flee: %+v", next)
	}
}

func TestNearbyHostileOwnershipMatchesDetection(t *testing.T) {
	s := overlapState()
	for _, tc := range []struct {
		face   int
		danger bool
	}{{32768 + 9, false}, {32768 + 5, true}, {-1, true}} {
		addNPCFacing(s, 1, 411, 1, 0, tc.face)
		if got := HostileEventNpcNearby(s, 3200, 3200, 15); got != tc.danger {
			t.Fatalf("face=%d: nearby=%v, want %v", tc.face, got, tc.danger)
		}
	}
}

func TestPlantNotMineReplyReleasesPromptlyAndChecksNextEvent(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Strange plant", 501: "Genie"}})
	s := &overlapSession{state: overlapState()}
	playSound(s.state, gamestate.SoundTriffidAppear) // both plausibly this account's own
	heardOwnGenie(s.state)
	addNPCs(s.state, npcSpec{1, 500, 1, 0}, npcSpec{2, 501, 2, 0})
	g := NewGuardian(s, nil)
	defer g.Stop()
	g.Chat = &chatlog.Log{}
	s.onSend = func(op byte) {
		if op == clientprot.OPNPC1 {
			g.Chat.Record("It's not here for you.")
		}
	}
	start := time.Now()
	if err := solveStrangePlant(s, nil, g.nextEvent(), g.Chat, nil); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("immediate rejection took %s", elapsed)
	}
	if next := g.nextEvent(); next == nil || next.NpcIndex != 2 {
		t.Fatalf("did not move on after rejection: %+v", next)
	}
}

func TestUrgentEventInterruptsReplyWait(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie"}})
	s := &overlapSession{state: overlapState()}
	addNPCs(s.state, npcSpec{1, 500, 1, 0}, npcSpec{2, 411, 2, 0})
	g := NewGuardian(s, nil)
	defer g.Stop()
	ev := newEvent(KindDialogNpc, "Genie")
	ev.NpcIndex = 1
	ev.NpcType = 500
	start := time.Now()
	if !waitEventReply(s, nil, ev, time.Second, nil, nil) {
		t.Fatal("hostile must interrupt reply wait")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("urgent event was not dispatched promptly")
	}
	if len(s.sent) == 0 || s.sent[len(s.sent)-1] != clientprot.CLOSE_MODAL {
		t.Fatal("handoff left modal open")
	}
}

func TestRejectedDwarfDialogueClosesWithoutNavigation(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Drunken Dwarf"}})
	components := archive.Components{100: &archive.Component{ID: 100, Children: []int{101}}, 101: &archive.Component{ID: 101, Type: archive.TypeText, Text: "You're not my matey!"}}
	previous := currentInterfaceArchive()
	SetInterfaceArchive(components)
	defer SetInterfaceArchive(previous)
	s := &overlapSession{state: overlapState()}
	heardOwnEvent(s.state, "Drunken Dwarf") // it may be ours until it answers otherwise
	addNPC(s.state, 1, 500, 1, 0)
	g := NewGuardian(s, nil)
	defer g.Stop()
	s.onSend = func(op byte) {
		if op == clientprot.OPNPC1 {
			s.state.Handle(serverprot.IF_OPENCHAT, payload(func(p *packet.Packet) { p.P2(100) }))
		}
	}
	start := time.Now()
	if err := HandleDialogNpc(s, nil, g.nextEvent(), nil); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("rejection waited through general dialogue timeout")
	}
	if actions.NewChatDialog(s, components).IsOpen() || !g.isRejected(newDialogEvent(1, 500, "Drunken Dwarf")) {
		t.Fatal("rejection not remembered or modal left open")
	}
}

func newDialogEvent(index, typeID int, name string) *Event {
	ev := newEvent(KindDialogNpc, name)
	ev.NpcIndex = index
	ev.NpcType = typeID
	return ev
}

func TestLampYieldsBeforeRubbingWhenHostileAppears(t *testing.T) {
	withResolver(t, fakeResolver{objNames: map[int]string{831: "Lamp"}})
	s := &overlapSession{state: overlapState()}
	setFullInventory(s.state, 3214, [2]int32{831, 1})
	addNPC(s.state, 2, 411, 2, 0)
	ev := newEvent(KindBoxLamp, "Lamp")
	ev.InvComID, ev.ItemID, ev.Slot = 3214, 831, 0
	if err := handleLamp(s, nil, ev, nil); err != nil {
		t.Fatal(err)
	}
	for _, op := range s.sent {
		if op != clientprot.CLOSE_MODAL {
			t.Fatalf("lamp action sent despite urgent hostile: %d", op)
		}
	}
}

func TestDangerHandoffRequiresSeparationAndEligibleOtherEvent(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie"}})
	s := &overlapSession{state: overlapState()}
	heardOwnGenie(s.state)
	addNPCs(s.state, npcSpec{1, 411, 2, 0}, npcSpec{2, 500, 1, 0})
	g := NewGuardian(s, nil)
	defer g.Stop()
	ev := Detect(s.state)
	if yieldSeparatedEvent(s, ev, nil) {
		t.Fatal("yielded while hostile still within attack area")
	}
	addNPC(s.state, 2, 500, 1, 0)
	if !yieldSeparatedEvent(s, ev, nil) {
		t.Fatal("safe separation did not release handler for genie")
	}
	markPlantRejected(s, 2)
	if yieldSeparatedEvent(s, ev, nil) {
		t.Fatal("non-owner genie caused unnecessary handoff")
	}
}

func TestDisappearedNPCReleasesRetryBookkeeping(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie"}})
	s := &overlapSession{state: overlapState()}
	heardOwnGenie(s.state)
	addNPC(s.state, 1, 500, 1, 0)
	g := NewGuardian(s, nil)
	defer g.Stop()
	ev := g.nextEvent()
	sig := eventSignature(ev)
	deferEvent(s, ev, time.Minute)
	g.attempts[sig] = 2
	g.cooldownUntil[sig] = time.Now().Add(time.Minute)
	addNPCs(s.state)
	if g.nextEvent() != nil || len(g.trackedEvents) != 0 || len(g.attempts) != 0 || len(g.cooldownUntil) != 0 || len(g.retryUntil) != 0 {
		t.Fatal("disappeared NPC left retry state behind")
	}
}

func TestTrappedSelectionHonorsInstanceFilter(t *testing.T) {
	for _, square := range [][2]int{{mimeSquareMX, mimeSquareMZ}, {mazeSquareMX, mazeSquareMZ}} {
		s := overlapState()
		setTile(s, square[0]*64+8, square[1]*64+8, 0)
		if ev := detectEligible(s, -1, func(ev *Event) bool { return ev.Kind != KindTrapped }); ev != nil {
			t.Fatalf("filtered trapped room reappeared: %+v", ev)
		}
	}
}

func TestHazardHandoffWaitsOutsideWhirlpoolDetectionRadius(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie"}})
	s := &overlapSession{state: overlapState()}
	heardOwnGenie(s.state)
	addNPC(s.state, 2, 500, 1, 0)
	g := NewGuardian(s, nil)
	defer g.Stop()
	ev := newEvent(KindHazard, "whirlpool")
	ev.ThreatX, ev.ThreatZ = 3203, 3200
	if yieldSeparatedEvent(s, ev, nil) {
		t.Fatal("yielded inside the whirlpool's three-tile detection radius")
	}
	ev.ThreatX = 3204
	if !yieldSeparatedEvent(s, ev, nil) {
		t.Fatal("did not hand off outside the hazard radius")
	}
}

func TestAcceptedGenieYieldsIfContinuingIntroducesHostile(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie"}, objNames: map[int]string{831: "Lamp"}})
	components := archive.Components{
		100: &archive.Component{ID: 100, Children: []int{101, 102}},
		101: &archive.Component{ID: 101, Type: archive.TypeText, Text: "Take this lamp!"},
		102: &archive.Component{ID: 102, ButtonType: archive.ButtonContinue},
	}
	s := &overlapSession{state: overlapState()}
	addNPC(s.state, 1, 500, 1, 0)
	setFullInventory(s.state, 3214, [2]int32{831, 1})
	s.onSend = func(op byte) {
		if op == clientprot.OPNPC1 {
			s.state.Handle(serverprot.IF_OPENCHAT, payload(func(p *packet.Packet) { p.P2(100) }))
		}
		if op == clientprot.RESUME_PAUSEBUTTON {
			addNPCs(s.state, npcSpec{1, 500, 1, 0}, npcSpec{2, 411, 2, 0})
		}
	}
	start := time.Now()
	if err := solveGenie(s, nil, newDialogEvent(1, 500, "Genie"), components, nil); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("continuation ignored urgent handoff")
	}
	for _, op := range s.sent {
		if op == clientprot.OPHELD1 || op == clientprot.IF_BUTTON {
			t.Fatal("continued claiming lamp after interruption")
		}
	}
}

func TestTradeDefersUnknownDialogueButOwnedEventPreempts(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie"}})
	for _, modal := range []int{actions.TradeOfferScreen, actions.TradeConfirmScreen} {
		for _, tc := range []struct {
			face int
			want bool
		}{{-1, false}, {32768 + 9, false}, {32768 + 5, true}} {
			s := overlapState()
			s.Handle(serverprot.IF_OPENMAIN, payload(func(p *packet.Packet) { p.P2(uint16(modal)) }))
			addNPCFacing(s, 1, 500, 1, 0, tc.face)
			if got := Detect(s) != nil; got != tc.want {
				t.Errorf("modal=%d face=%d detect=%v want=%v", modal, tc.face, got, tc.want)
			}
		}
	}
	s := overlapState()
	s.Handle(serverprot.IF_OPENMAIN, payload(func(p *packet.Packet) { p.P2(actions.TradeOfferScreen) }))
	hostile := 0
	for id := range hostileEventNpcIDs {
		hostile = id
		break
	}
	addNPCFacing(s, 1, hostile, 1, 0, 32768+5)
	if ev := Detect(s); ev == nil || ev.Kind != KindEvade {
		t.Fatalf("owned hostile must interrupt trade: %+v", ev)
	}
}

func TestWolfRouteAvoidanceAllowsOutwardEscape(t *testing.T) {
	s := overlapState()
	addNPCFacing(s, 1, 96, 4, 0, -1)
	avoid := visibleWolfAvoidance(&overlapSession{state: s}, pathfind.NavPoint{X: 3200, Z: 3200})
	if avoid == nil || !avoid(3201, 3200, 0) || avoid(3199, 3200, 0) {
		t.Fatal("wolf avoidance must block approach and allow escape")
	}
}

// Between two tries at an event the script is held (Busy), so its next action does not undo the
// try; a solver's longer wait (a growing plant) lets it work.
func TestBusyHoldsTheScriptBetweenTries(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Strange plant"}})
	s := &overlapSession{state: overlapState()}
	playSound(s.state, gamestate.SoundTriffidAppear)
	addNPCs(s.state, npcSpec{1, 500, 1, 0})
	g := NewGuardian(s, nil)
	defer g.Stop()
	plant := g.nextEvent()
	if plant == nil {
		t.Fatal("no plant event; the test proves nothing")
	}
	sig := eventSignature(plant)
	g.mu.Lock()
	g.trackedEvents[sig] = *plant
	g.retryUntil[sig] = time.Now().Add(2 * pollInterval)
	g.mu.Unlock()
	if !g.Busy() {
		t.Fatal("the script was let go between two tries")
	}
	g.mu.Lock()
	g.retryUntil[sig] = time.Now().Add(30 * time.Second)
	g.mu.Unlock()
	if g.retryImminent() {
		t.Fatal("a long solver wait held the script")
	}
}
