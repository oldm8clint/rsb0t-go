package sessionipc

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// TestStateMirrorMatchesDirectHandle proves the design's central read-path claim:
// forwarding the same (opcode, payload) a real session's ReadLoop already observes into
// a second, independent gamestate.State via StateMirror produces the identical result
// gamestate.State.Handle produces directly — see internal/gamestate/skills_test.go's
// own TestUpdateStat, which this mirrors byte for byte, and this package's own doc
// comments for why that equivalence is the whole point.
func TestStateMirrorMatchesDirectHandle(t *testing.T) {
	p := packet.NewWriter(6)
	p.P1(6)     // Fishing
	p.P4(12345) // whole XP, not tenths - see gamestate.applyUpdateStat's own doc comment
	p.P1(37)
	frame := PacketFrame{Opcode: serverprot.UPDATE_STAT, Payload: append([]byte(nil), p.Data[:p.Pos]...)}

	m := NewStateMirror()
	m.Apply(frame)

	sk, ok := m.State().Skill(6)
	if !ok || sk.Level != 37 || sk.XP != 12345 || sk.Generation != 1 {
		t.Fatalf("mirrored state mismatch: ok=%v skill=%+v", ok, sk)
	}
}

// TestStateMirrorAppliesFramesInOrder checks a short sequence, not just one frame — the
// mirror has to preserve order for Generation (and, in a real script, for anything that
// depends on transitions, like EventLevelUp) to mean the same thing it does today.
func TestStateMirrorAppliesFramesInOrder(t *testing.T) {
	m := NewStateMirror()
	for _, v := range []struct {
		level int
		xp    int32
	}{{10, 5000}, {11, 7500}} {
		p := packet.NewWriter(6)
		p.P1(0)
		p.P4(v.xp)
		p.P1(byte(v.level))
		m.Apply(PacketFrame{Opcode: serverprot.UPDATE_STAT, Payload: append([]byte(nil), p.Data[:p.Pos]...)})
	}
	sk, _ := m.State().Skill(0)
	if sk.Level != 11 || sk.XP != 7500 || sk.Generation != 2 {
		t.Fatalf("unexpected final skill after two frames: %+v", sk)
	}
}

// TestNewStateMirrorStartsEmpty checks a fresh mirror before any frame is applied:
// Skill's bool return is only an index-range check (see gamestate.State.Skill), so
// "never updated" instead shows up as Generation 0 — the same signal
// gamestate.applyUpdateStat itself uses to suppress a bogus first level-up event.
func TestNewStateMirrorStartsEmpty(t *testing.T) {
	m := NewStateMirror()
	sk, ok := m.State().Skill(0)
	if !ok || sk.Generation != 0 {
		t.Fatalf("expected a fresh mirror to report Generation 0 for an unseen skill, got ok=%v skill=%+v", ok, sk)
	}
}
