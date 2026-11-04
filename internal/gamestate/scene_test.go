package gamestate

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// idlePlayerInfo is a PLAYER_INFO with no local update and no other players.
func idlePlayerInfo(s *State) {
	s.Handle(serverprot.PLAYER_INFO, bitPack(3, bitField{1, 0}, bitField{8, 0}, bitField{11, 2047}))
}

func varpSmall(s *State, id int, value int8) {
	s.Handle(serverprot.VARP_SMALL, payload(func(p *packet.Packet) { p.P2(uint16(id)); p.P1(byte(value)) }))
}

func updateStat(s *State, stat, xp, level int) {
	s.Handle(serverprot.UPDATE_STAT, payload(func(p *packet.Packet) {
		p.P1(byte(stat))
		p.P4(int32(xp))
		p.P1(byte(level))
	}))
}

// levelUpsOnly passes only EventLevelUp through, for tests written before UPDATE_STAT also
// emitted EventSkillXP.
func levelUpsOnly(fn Subscriber) Subscriber {
	return func(ev Event) {
		if ev.Kind == EventLevelUp {
			fn(ev)
		}
	}
}

func collectEvents(s *State) *[]Event {
	var got []Event
	s.Subscribe(func(ev Event) { got = append(got, ev) })
	return &got
}

func TestSceneStateFollowsRebuildAndPlayerInfo(t *testing.T) {
	s := New()
	if s.SceneState() != SceneIdle || s.SceneReady() || s.InGame() {
		t.Fatalf("fresh state: scene=%d ready=%v ingame=%v", s.SceneState(), s.SceneReady(), s.InGame())
	}

	rebuild(s, 400, 400)
	if s.SceneState() != SceneBuilding || s.SceneReady() {
		t.Fatalf("after REBUILD_NORMAL: scene=%d ready=%v, want building", s.SceneState(), s.SceneReady())
	}

	teleportLocal(s, 0, 60, 60)
	if s.SceneState() != SceneReady || !s.SceneReady() || !s.InGame() {
		t.Fatalf("after PLAYER_INFO: scene=%d ready=%v ingame=%v, want ready", s.SceneState(), s.SceneReady(), s.InGame())
	}

	// Client.ts ignores a rebuild to the centre it already has while the scene is ready.
	rebuild(s, 400, 400)
	if !s.SceneReady() {
		t.Fatal("a rebuild to the same centre zone must not unready the scene")
	}

	rebuild(s, 404, 400)
	if s.SceneState() != SceneBuilding || s.SceneReady() {
		t.Fatalf("after a new centre: scene=%d ready=%v, want building", s.SceneState(), s.SceneReady())
	}
	idlePlayerInfo(s)
	if !s.SceneReady() {
		t.Fatal("the first PLAYER_INFO after a rebuild must finish the build")
	}

	s.Handle(serverprot.LOGOUT, nil)
	if s.SceneState() != SceneIdle || s.SceneReady() || s.InGame() {
		t.Fatalf("after LOGOUT: scene=%d ready=%v ingame=%v", s.SceneState(), s.SceneReady(), s.InGame())
	}
}

func TestSceneReadyNeedsAKnownTile(t *testing.T) {
	s := New()
	rebuild(s, 400, 400)
	idlePlayerInfo(s) // no local position update, so no tile yet
	if s.SceneState() != SceneReady {
		t.Fatalf("scene=%d, want ready", s.SceneState())
	}
	if s.SceneReady() {
		t.Fatal("SceneReady without a known tile")
	}
}

func TestTickCountsPlayerInfoAndEmitsEvents(t *testing.T) {
	s := New()
	got := collectEvents(s)
	idlePlayerInfo(s)
	idlePlayerInfo(s)
	if s.Tick() != 2 {
		t.Fatalf("Tick() = %d, want 2", s.Tick())
	}
	var ticks []int
	for _, ev := range *got {
		if ev.Kind == EventTick {
			ticks = append(ticks, ev.Tick)
		}
	}
	if len(ticks) != 2 || ticks[0] != 1 || ticks[1] != 2 {
		t.Fatalf("tick events = %v, want [1 2]", ticks)
	}
	if EventTick.String() != "tick" {
		t.Fatalf("EventTick.String() = %q", EventTick.String())
	}
}

func TestRunEnergyAndWeight(t *testing.T) {
	s := New()
	if _, ok := s.Energy(); ok {
		t.Fatal("Energy known before UPDATE_RUNENERGY")
	}
	if _, ok := s.Weight(); ok {
		t.Fatal("Weight known before UPDATE_RUNWEIGHT")
	}
	s.Handle(serverprot.UPDATE_RUNENERGY, []byte{73})
	s.Handle(serverprot.UPDATE_RUNWEIGHT, payload(func(p *packet.Packet) { p.P2(uint16(0xfffb)) })) // -5 kg
	if e, ok := s.Energy(); !ok || e != 73 {
		t.Fatalf("Energy() = %d,%v, want 73", e, ok)
	}
	if w, ok := s.Weight(); !ok || w != -5 {
		t.Fatalf("Weight() = %d,%v, want -5 (g2b is signed)", w, ok)
	}
	s.Handle(serverprot.UPDATE_RUNWEIGHT, payload(func(p *packet.Packet) { p.P2(31) }))
	if w, _ := s.Weight(); w != 31 {
		t.Fatalf("Weight() = %d, want 31", w)
	}
}

func TestRunRetaliateAndCombatModeVarps(t *testing.T) {
	s := New()
	if s.RunEnabled() || !s.AutoRetaliateOn() || s.CombatMode() != 0 {
		t.Fatalf("defaults: run=%v retaliate=%v mode=%d", s.RunEnabled(), s.AutoRetaliateOn(), s.CombatMode())
	}
	varpSmall(s, VarpRun, 1)
	varpSmall(s, VarpAutoRetaliate, 1)
	varpSmall(s, VarpCombatMode, 2)
	if !s.RunEnabled() || s.AutoRetaliateOn() || s.CombatMode() != 2 {
		t.Fatalf("after varps: run=%v retaliate=%v mode=%d", s.RunEnabled(), s.AutoRetaliateOn(), s.CombatMode())
	}
}

func TestVarpChangedFiresOnlyOnChange(t *testing.T) {
	s := New()
	got := collectEvents(s)
	varpSmall(s, VarpRun, 0) // 0 -> 0: the client's var array already read 0
	varpSmall(s, VarpRun, 1)
	varpSmall(s, VarpRun, 1)
	varpSmall(s, VarpRun, 0)
	s.Handle(serverprot.VARP_LARGE, payload(func(p *packet.Packet) { p.P2(101); p.P4(5000) }))

	want := []Event{
		{Kind: EventVarpChanged, Varp: VarpRun, Value: 1, PreviousValue: 0},
		{Kind: EventVarpChanged, Varp: VarpRun, Value: 0, PreviousValue: 1},
		{Kind: EventVarpChanged, Varp: 101, Value: 5000, PreviousValue: 0},
	}
	if len(*got) != len(want) {
		t.Fatalf("got %d events %+v, want %d", len(*got), *got, len(want))
	}
	for i := range want {
		if (*got)[i] != want[i] {
			t.Fatalf("event %d = %+v, want %+v", i, (*got)[i], want[i])
		}
	}
}

func TestSkillXPEvents(t *testing.T) {
	s := New()
	got := collectEvents(s)
	updateStat(s, 10, 1000, 10) // first sync: nothing
	updateStat(s, 10, 1050, 10) // xp only
	updateStat(s, 10, 1200, 11) // xp and a level
	updateStat(s, 10, 1200, 11) // unchanged

	want := []Event{
		{Kind: EventSkillXP, Skill: 10, Level: 10, XP: 1050, XPDelta: 50},
		{Kind: EventSkillXP, Skill: 10, Level: 11, XP: 1200, XPDelta: 150},
		{Kind: EventLevelUp, Skill: 10, Level: 11, PreviousLevel: 10, XP: 1200},
	}
	if len(*got) != len(want) {
		t.Fatalf("got %d events %+v, want %d", len(*got), *got, len(want))
	}
	for i := range want {
		if (*got)[i] != want[i] {
			t.Fatalf("event %d = %+v, want %+v", i, (*got)[i], want[i])
		}
	}
}

func TestToLocalAndToWorld(t *testing.T) {
	s := New()
	if _, _, ok := s.ToLocal(3200, 3200); ok {
		t.Fatal("ToLocal before any REBUILD_NORMAL")
	}
	rebuild(s, 400, 400) // origin 3152,3152
	if x, z, ok := s.MapBuildBase(); !ok || x != 3152 || z != 3152 {
		t.Fatalf("MapBuildBase() = %d,%d,%v", x, z, ok)
	}
	if lx, lz, ok := s.ToLocal(3212, 3160); !ok || lx != 60 || lz != 8 {
		t.Fatalf("ToLocal = %d,%d,%v, want 60,8", lx, lz, ok)
	}
	if _, _, ok := s.ToLocal(3152+104, 3160); ok {
		t.Fatal("ToLocal must reject a tile outside the 104-tile build area")
	}
	if x, z, ok := s.ToWorld(60, 8); !ok || x != 3212 || z != 3160 {
		t.Fatalf("ToWorld = %d,%d,%v", x, z, ok)
	}
}

// TestMultiwayFollowsSetMultiway: SET_MULTIWAY is one byte (Client.ts inMultizone = g1()).
func TestMultiwayFollowsSetMultiway(t *testing.T) {
	s := New()
	if _, ok := s.Multiway(); ok {
		t.Fatal("multiway known before SET_MULTIWAY")
	}
	s.Handle(serverprot.SET_MULTIWAY, []byte{1})
	if multi, ok := s.Multiway(); !ok || !multi {
		t.Fatalf("after SET_MULTIWAY 1: %v %v", multi, ok)
	}
	s.Handle(serverprot.SET_MULTIWAY, []byte{0})
	if multi, _ := s.Multiway(); multi {
		t.Fatal("still multiway after SET_MULTIWAY 0")
	}
}

// TestRebootCountdownRunsInTicks: UPDATE_REBOOT_TIMER carries g2 server ticks, and the
// countdown falls by one per observed PLAYER_INFO.
func TestRebootCountdownRunsInTicks(t *testing.T) {
	s := New()
	if _, ok := s.RebootInTicks(); ok {
		t.Fatal("reboot pending before any announcement")
	}
	s.Handle(serverprot.UPDATE_REBOOT_TIMER, payload(func(p *packet.Packet) { p.P2(100) }))
	if left, ok := s.RebootInTicks(); !ok || left != 100 {
		t.Fatalf("just announced: %d %v, want 100", left, ok)
	}
	for i := 0; i < 3; i++ {
		idlePlayerInfo(s)
	}
	if left, _ := s.RebootInTicks(); left != 97 {
		t.Fatalf("after 3 ticks: %d, want 97", left)
	}
	s.Handle(serverprot.UPDATE_REBOOT_TIMER, payload(func(p *packet.Packet) { p.P2(0) }))
	if _, ok := s.RebootInTicks(); ok {
		t.Fatal("a zero timer must cancel the countdown")
	}
}

// TestPlayerOpsAndMinimapFollowTheServer covers SET_PLAYER_OP (index g1, priority g1, op
// gjstr, "null" clears; an out-of-range index is ignored) and MINIMAP_TOGGLE (g1).
func TestPlayerOpsAndMinimapFollowTheServer(t *testing.T) {
	s := New()
	setOp := func(index int, op string) {
		s.Handle(serverprot.SET_PLAYER_OP, payload(func(p *packet.Packet) { p.P1(byte(index)); p.P1(0); p.PJStr(op) }))
	}
	setOp(4, "Trade with")
	setOp(2, "Attack")
	setOp(9, "Bogus")
	ui := s.renderUILocked()
	if ui.PlayerOps[3] != "Trade with" || !ui.PlayerOpPriority[3] {
		t.Fatalf("player options absent from render UI: %+v", ui.PlayerOps)
	}
	if got := s.PlayerOps(); got != [5]string{"", "Attack", "", "Trade with", ""} {
		t.Fatalf("ops = %q", got)
	}
	setOp(2, "null")
	if got := s.PlayerOps(); got[1] != "" {
		t.Fatalf("\"null\" left option 2 as %q", got[1])
	}
	if _, ok := s.MinimapState(); ok {
		t.Fatal("minimap state known before MINIMAP_TOGGLE")
	}
	s.Handle(serverprot.MINIMAP_TOGGLE, []byte{2})
	if st, ok := s.MinimapState(); !ok || st != 2 {
		t.Fatalf("minimap = %d %v, want 2", st, ok)
	}
}
