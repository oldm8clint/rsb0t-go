package gamestate

import (
	"sort"
	"strings"
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// Every IF_* / TUT_* server opcode, and what this client does with it. This table is the
// interface-coverage audit: a stream viewer can only draw what the session decoded, and
// until 2026-09-21 eight of these were silently dropped, which is why every NPC dialogue
// drew an empty head box. A new opcode added to the generated table with no row here fails
// this test, so the next person has to make the same decision deliberately.
//
// "decoded" means Handle really changes state for it; the test proves that by feeding a
// plausible payload and requiring the snapshot to change.
var interfaceOpcodeCoverage = map[string]string{
	"IF_OPENMAIN":      "decoded",
	"IF_OPENSIDE":      "decoded",
	"IF_OPENCHAT":      "decoded",
	"IF_OPENMAIN_SIDE": "decoded",
	"IF_CLOSE":         "decoded",
	"IF_SETTEXT":       "decoded",
	"IF_SETCOLOUR":     "decoded",
	"IF_SETICON":       "decoded",
	"IF_SETOBJECT":     "decoded",
	"IF_SETMODEL":      "decoded",
	"IF_SETNPCHEAD":    "decoded",
	"IF_SETPLAYERHEAD": "decoded",
	"IF_SETANIM":       "decoded",
	"IF_SETHIDE":       "decoded",
	"IF_OPENOVERLAY":   "decoded",
	"IF_SETSCROLLPOS":  "decoded",
	"IF_SETPOSITION":   "decoded",

	// Deliberately not decoded, with the reason. Change the reason, not just the row,
	// if one of these ever starts mattering.
	"IF_SHOWICON": "skipped: the engine checkout contains no sender for it (0 hits for IfShowIcon), " +
		"and the stream view's own tab selection is the viewer's, not the server's",
	"TUT_OPEN":  "skipped: Tutorial Island only; no fleet account is ever on it",
	"TUT_FLASH": "skipped: Tutorial Island only; no fleet account is ever on it",
}

func TestEveryInterfaceOpcodeIsAccountedFor(t *testing.T) {
	var missing []string
	for name := range serverprot.ByName {
		if !strings.HasPrefix(name, "IF_") && !strings.HasPrefix(name, "TUT_") {
			continue
		}
		if _, ok := interfaceOpcodeCoverage[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("interface opcodes with no coverage decision: %v — add a row to "+
			"interfaceOpcodeCoverage saying whether the stream view needs it", missing)
	}
	for name := range interfaceOpcodeCoverage {
		if _, ok := serverprot.ByName[name]; !ok {
			t.Fatalf("%s is in the coverage table but not in the generated opcode table", name)
		}
	}
}

// Each opcode the table calls "decoded" must really change what a renderer would draw. A
// row can otherwise go stale the moment a case is deleted.
func TestDecodedInterfaceOpcodesReallyChangeTheSnapshot(t *testing.T) {
	// One payload per opcode, in its real wire shape (sizes are in ServerProtSizes and
	// checked below), chosen so applying it to a fresh State is observable.
	payloads := map[string]func(p *packet.Packet){
		"IF_OPENMAIN":      func(p *packet.Packet) { p.P2(5292) },
		"IF_OPENSIDE":      func(p *packet.Packet) { p.P2(5063) },
		"IF_OPENCHAT":      func(p *packet.Packet) { p.P2(4882) },
		"IF_OPENMAIN_SIDE": func(p *packet.Packet) { p.P2(5292); p.P2(5063) },
		"IF_SETTEXT":       func(p *packet.Packet) { p.P2(4884); p.PJStr("Hans") },
		"IF_SETCOLOUR":     func(p *packet.Packet) { p.P2(4884); p.P2(0x7C00) },
		"IF_SETICON":       func(p *packet.Packet) { p.P1(3); p.P2(3213) },
		"IF_SETOBJECT":     func(p *packet.Packet) { p.P2(306); p.P2(1777); p.P2(200) },
		"IF_SETNPCHEAD":    func(p *packet.Packet) { p.P2(4883); p.P2(0) },
		"IF_SETPLAYERHEAD": func(p *packet.Packet) { p.P2(4903) },
		"IF_SETANIM":       func(p *packet.Packet) { p.P2(4883); p.P2(588) },
		"IF_SETHIDE":       func(p *packet.Packet) { p.P2(1234); p.P1(1) },
		"IF_OPENOVERLAY":   func(p *packet.Packet) { p.P2(197) },
		"IF_SETSCROLLPOS":  func(p *packet.Packet) { p.P2(6969); p.P2(240) },
		"IF_SETPOSITION":   func(p *packet.Packet) { p.P2(70); p.P2(8); p.P2(12) },
	}
	for name, decision := range interfaceOpcodeCoverage {
		if decision != "decoded" {
			continue
		}
		build, ok := payloads[name]
		switch name {
		case "IF_CLOSE", "IF_SETMODEL":
			// Both are only observable against a state they undo; ifobject_test.go and
			// the modal tests already cover them against a prepared state.
			continue
		}
		if !ok {
			t.Fatalf("%s is marked decoded but this test has no payload for it", name)
		}
		op := serverprot.ByName[name]
		body := payload(build)
		if want := serverprot.ServerProtSizes[op]; want >= 0 && len(body) != want {
			t.Fatalf("%s payload is %d bytes, but ServerProtSizes says %d", name, len(body), want)
		}
		before := renderUIOf(New())
		s := New()
		s.Handle(op, body)
		after := renderUIOf(s)
		if renderUIKey(before) == renderUIKey(after) {
			t.Fatalf("%s changed nothing a renderer can see — the coverage table says decoded", name)
		}
	}
}

// renderUIKey is a cheap total ordering of the parts a renderer draws from.
func renderUIKey(ui RenderUI) string {
	var b strings.Builder
	fmtInt := func(label string, v int) {
		b.WriteString(label)
		b.WriteByte(':')
		b.WriteString(itoa(v))
		b.WriteByte(' ')
	}
	fmtInt("main", ui.Main)
	fmtInt("side", ui.Side)
	fmtInt("chat", ui.ChatModal)
	fmtInt("overlay", ui.Overlay)
	fmtInt("texts", len(ui.Texts))
	fmtInt("colours", len(ui.Colours))
	fmtInt("icons", len(ui.SideIcons))
	fmtInt("objects", len(ui.Objects))
	fmtInt("heads", len(ui.Heads))
	fmtInt("anims", len(ui.Anims))
	fmtInt("hidden", len(ui.Hidden))
	fmtInt("scroll", len(ui.ScrollPos))
	fmtInt("positions", len(ui.Positions))
	return b.String()
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}
