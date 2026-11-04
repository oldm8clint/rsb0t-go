package sessionipc

import (
	"encoding/json"
	"errors"
	"fmt"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
)

// RemoteSession is HOTRELOAD.md's Phase 2 deliverable: a real actions.Session
// implementation backed entirely by sessionipc — no *client.Session, no socket, no ISAAC
// cipher of its own. A script (internal/bot's Runner/Context, and every function in
// internal/actions it calls) runs against this exactly as it runs against a real
// *client.Session today, with zero source changes to either package — see this file's
// own tests for a script-facing proof (calling actions.IfButton/actions.Walk completely
// unmodified) rather than just an assertion.
//
// State() is "free" per HOTRELOAD.md's own claim — it is StateMirror.State(), rebuilt
// purely from a forwarded packet stream. Send() is the hard half the design doc is
// explicit about NOT having a generic solution for: a closure cannot cross a process
// boundary, and the ISAAC cipher + anticheat.Counters state a Send call may depend on are
// inherently host-side. This implementation resolves it for a genuinely real (not faked)
// subset of internal/actions — every function whose Send closure writes exactly one
// opcode via P1Enc with no Cheats() side-packet (see sendDecoders, below) — by building
// the closure's output locally with Random left nil (P1Enc(opcode) with a nil cipher
// writes the literal, unobfuscated opcode byte — see packet.Packet.P1Enc's own doc
// comment), decoding that plaintext buffer back into the semantic call (opcode + typed
// args) internal/actions originally made, and shipping THAT over the wire as a named
// ActionCall. The host's own registered Handler (see RegisterActionHandlers) then calls
// the real, unmodified internal/actions function again with the real *client.Session,
// which is what actually runs the real ISAAC cipher and produces the real wire bytes —
// this process never builds or transmits a byte that goes near a socket.
//
// An action whose Send closure isn't in sendDecoders (starkly, anything touching
// Cheats() — OpPlayer4, InvButton's option-1 case — see actions.go's own doc comment on
// why those side-packets are host-only stateful) returns a clear, typed error instead of
// silently sending something wrong. Filling out sendDecoders for the rest of
// internal/actions's functions, and giving Cheats()-dependent actions a real design (not
// attempted here — see HOTRELOAD.md), is exactly Phase 3's scope, unstarted.
type RemoteSession struct {
	account string
	client  *Client
	mirror  *StateMirror

	// cheats is a real, but purely local, *anticheat.Counters, switched OFF: present so
	// Cheats() never returns nil, and off so no action's closure prepends a side-packet to
	// the bytes Send decodes. The side-packets are the host's to write: it replays each
	// decoded call against the real session, whose own counters add them exactly as a
	// direct call would. Since 2026-09-25 OpLoc, OpObj, OpLocU and OpHeld carry side-packets
	// too, and with this on they failed to decode as soon as one was due (review).
	cheats *anticheat.Counters
}

// NewRemoteSession builds a RemoteSession for account, sending action calls over client
// and reading state from mirror (see NewStateMirror — the caller is responsible for
// feeding it a forwarded packet stream, exactly like a real host would).
func NewRemoteSession(account string, client *Client, mirror *StateMirror) *RemoteSession {
	cheats := &anticheat.Counters{}
	cheats.SetEnabled(false)
	return &RemoteSession{account: account, client: client, mirror: mirror, cheats: cheats}
}

// State implements actions.Session.
func (r *RemoteSession) State() *gamestate.State { return r.mirror.State() }

// Cheats implements actions.Session — see the field's own doc comment for why this is
// safe to call but not safe to depend on for anything Send() cannot yet forward.
func (r *RemoteSession) Cheats() *anticheat.Counters { return r.cheats }

// errNoRemoteHandler is wrapped with the offending opcode by Send below.
var errNoRemoteHandler = errors.New("sessionipc: this action has no remote handler yet (see HOTRELOAD.md Phase 3)")

// Send implements actions.Session. See this type's own doc comment for the full
// mechanism; in short: build the closure's output locally with no cipher, decode it back
// into a named+typed call, and round-trip that over r.client to the host, which replays
// the real internal/actions function against the real session.
func (r *RemoteSession) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(64)
	build(w)
	raw := w.Data[:w.Pos]
	if len(raw) == 0 {
		return errors.New("sessionipc: Send built an empty packet")
	}
	name, args, err := decodeSend(raw)
	if err != nil {
		return err
	}
	data, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("sessionipc: encoding %s's args: %w", name, err)
	}
	res, err := r.client.Call(ActionCall{Account: r.account, Name: name, Args: data})
	if err != nil {
		return fmt.Errorf("sessionipc: calling %s: %w", name, err)
	}
	if !res.OK() {
		return fmt.Errorf("sessionipc: host rejected %s: %s", name, res.Err)
	}
	return nil
}

// ifButtonArgs/walkArgs mirror the JSON shape RegisterActionHandlers' own handlers
// decode — kept next to the encode side so the two can never drift out of sync silently
// (see remote_test.go for a round-trip test that would catch it if they did).
type ifButtonArgs struct {
	ComID int `json:"comId"`
}
type walkArgs struct {
	Waypoints []pathfind.NavPoint `json:"waypoints"`
	Run       bool                `json:"run"`
}

// opLocArgs/opHeldArgs/invButtonArgs are the three-more-functions-wide allowlist grown
// here (HOTRELOAD.md's own "Concrete next step"): actions.OpLoc, actions.OpHeld (which
// actions.Eat/actions.Bury/Equipment.Equip all delegate to — see equipment.go/bury.go's
// own doc comments), and actions.InvButton for option 2-5 only. All three share OpLoc's
// exact wire shape (P1Enc(opcode) + P2 + P2 + P2, 7 bytes, one opcode per option 1..5).
// OpLoc and OpHeld option 4 do call s.Cheats() since 2026-09-25, but RemoteSession's local
// counters are off (see its cheats field), so what reaches decodeSend is the action alone.
//
// InvButton is the one exception worth spelling out: actions.go's InvButton calls
// s.Cheats().BeforeInvButton1(w, objID) *before* its own P1Enc, but only when option==1
// (see actions.go's own InvButton, and HOTRELOAD.md's "design question" section for why
// that breaks the generic raw[0]==opcode decode trick the same way OpPlayer4 does). For
// option 2-5 no such call happens, so INV_BUTTON2..5's P1Enc byte is genuinely raw[0] with
// nothing prepended — decodeSend below matches those four opcodes only and deliberately
// leaves INV_BUTTON1 (44) out of the switch, so an option-1 call (e.g. a bank
// Withdraw-1, or Equipment's wornitems Remove, which always uses option 1 — see
// equipment.go's removeOption) correctly falls through to errNoRemoteHandler instead of
// being silently mis-decoded. This is chosen, not accidental: verified by reading
// actions.go's InvButton source, not assumed from the option number alone.
// opNpcArgs/opObjArgs/opLocUArgs grow the allowlist further (this project's own
// "Concrete next step": "grow decodeSend's allowlist further ... check what other
// scripts now registered in internal/bot actually call"). Checked directly against every
// currently-registered script's own runner package (fisherrunner, essencerunner,
// crafterrunner, fighterrunner, flinchrunner, mulerunner — a grep-driven checklist, not
// guesswork): actions.OpNpc (talking to/attacking an NPC — fisherrunner, essencerunner,
// fighterrunner, flinchrunner all call it directly), actions.OpObj (the OP_OBJn family
// actions.TakeGroundItem/InteractGroundItem delegate to unmodified — fighterrunner calls
// TakeGroundItem, so this also covers that call site with zero changes there), and
// actions.OpLocU (a loc used with an inventory item, e.g. crafterrunner/firstday.go's
// bank-deposit-box or furnace interactions). Every other action these six scripts call
// resolves to something already in this allowlist (IfButton — SetRun/SetAutoRetaliate/
// SetCombatMode all delegate to it; OpHeld — Eat/Bury/Equipment.Equip do too; Walk; OpLoc)
// or to a pure, local, no-Send helper (CountByID, FreeSlots, ResolveCombatStyle, ...) that
// never touches Session.Send at all and so needs no dispatcher entry — confirmed by
// reading each runner file's own actions.* call sites, not assumed. NewTrade, NewBank,
// NewShop, NewChatDialog, NewEquipment (fisherrunner/essencerunner/crafterrunner/
// mulerunner/fighterrunner's own UI-flow helpers) are still out of scope: each is a
// multi-step, multi-Send flow built from IfButton/OpHeld/etc plus interface-state reads,
// not a single decodable opcode of its own — they already work call-by-call through the
// primitives above, but the compound helpers themselves are not, and don't need to be,
// individually registered here.
type opNpcArgs struct {
	Option   int `json:"option"`
	NpcIndex int `json:"npcIndex"`
}
type opObjArgs struct {
	Option   int `json:"option"`
	X        int `json:"x"`
	Z        int `json:"z"`
	ItemType int `json:"itemType"`
}
type opLocUArgs struct {
	X       int   `json:"x"`
	Z       int   `json:"z"`
	LocType int   `json:"locType"`
	UseObj  int32 `json:"useObj"`
	UseSlot int   `json:"useSlot"`
	UseCom  int   `json:"useCom"`
}

type opLocArgs struct {
	Option  int `json:"option"`
	X       int `json:"x"`
	Z       int `json:"z"`
	LocType int `json:"locType"`
}
type opHeldArgs struct {
	Option int   `json:"option"`
	ObjID  int32 `json:"objId"`
	Slot   int   `json:"slot"`
	ComID  int   `json:"comId"`
}
type invButtonArgs struct {
	Option int   `json:"option"`
	ObjID  int32 `json:"objId"`
	Slot   int   `json:"slot"`
	ComID  int   `json:"comId"`
}

// opLocOpcodeOption/opHeldOpcodeOption/invButtonOpcodeOption invert clientprot's own
// OPLOC1..5/OPHELD1..5/INV_BUTTON1..5 opcode tables (see internal/actions/loc.go's
// opLocOpcodes, equipment.go's opHeldOpcodes, actions.go's invButtonOpcodes) so decodeSend
// can recover which 1-based option a matched opcode byte represents, ok=false for any
// opcode not in that family (including, deliberately, INV_BUTTON1 — see the doc comment
// above).
func opLocOpcodeOption(op byte) (int, bool) {
	switch int(op) {
	case clientprot.OPLOC1:
		return 1, true
	case clientprot.OPLOC2:
		return 2, true
	case clientprot.OPLOC3:
		return 3, true
	case clientprot.OPLOC4:
		return 4, true
	case clientprot.OPLOC5:
		return 5, true
	default:
		return 0, false
	}
}

func opHeldOpcodeOption(op byte) (int, bool) {
	switch int(op) {
	case clientprot.OPHELD1:
		return 1, true
	case clientprot.OPHELD2:
		return 2, true
	case clientprot.OPHELD3:
		return 3, true
	case clientprot.OPHELD4:
		return 4, true
	case clientprot.OPHELD5:
		return 5, true
	default:
		return 0, false
	}
}

// invButtonOpcodeOption intentionally omits INV_BUTTON1 (44) — see the doc comment above.
func invButtonOpcodeOption(op byte) (int, bool) {
	switch int(op) {
	case clientprot.INV_BUTTON2:
		return 2, true
	case clientprot.INV_BUTTON3:
		return 3, true
	case clientprot.INV_BUTTON4:
		return 4, true
	case clientprot.INV_BUTTON5:
		return 5, true
	default:
		return 0, false
	}
}

// opNpcOpcodeOption/opObjOpcodeOption invert clientprot's own OPNPC1..5/OPOBJ1..5 opcode
// tables (internal/actions/npc.go's opNpcOpcodes, grounditem.go's opObjOpcodes), the same
// technique opLocOpcodeOption above already uses.
func opNpcOpcodeOption(op byte) (int, bool) {
	switch int(op) {
	case clientprot.OPNPC1:
		return 1, true
	case clientprot.OPNPC2:
		return 2, true
	case clientprot.OPNPC3:
		return 3, true
	case clientprot.OPNPC4:
		return 4, true
	case clientprot.OPNPC5:
		return 5, true
	default:
		return 0, false
	}
}

func opObjOpcodeOption(op byte) (int, bool) {
	switch int(op) {
	case clientprot.OPOBJ1:
		return 1, true
	case clientprot.OPOBJ2:
		return 2, true
	case clientprot.OPOBJ3:
		return 3, true
	case clientprot.OPOBJ4:
		return 4, true
	case clientprot.OPOBJ5:
		return 5, true
	default:
		return 0, false
	}
}

// decodeOpLocShaped decodes the shared 7-byte P1Enc+P2+P2+P2 layout OpLoc/OpHeld/InvButton
// all use, returning the three uint16 fields in wire order.
func decodeOpLocShaped(raw []byte) (a, b, c int, err error) {
	if len(raw) != 7 {
		return 0, 0, 0, fmt.Errorf("sessionipc: want 7 bytes, got %d", len(raw))
	}
	a = int(raw[1])<<8 | int(raw[2])
	b = int(raw[3])<<8 | int(raw[4])
	c = int(raw[5])<<8 | int(raw[6])
	return a, b, c, nil
}

// decodeSend recovers the semantic (name, args) call that produced raw — a plaintext
// (nil-cipher) packet.Packet buffer — for every opcode this slice supports. raw[0] is
// always the buffer's first byte, which for every function in sendDecoders' allowlist is
// that function's one and only P1Enc call (verified by reading each function's own
// source in internal/actions — see the doc comment above for which are excluded and
// why), so it doubles as the opcode ID with no ambiguity.
func decodeSend(raw []byte) (name string, args interface{}, err error) {
	switch raw[0] {
	case clientprot.IF_BUTTON:
		// actions.IfButton: P1Enc(opcode) + P2(comId) — 3 bytes total.
		if len(raw) != 3 {
			return "", nil, fmt.Errorf("sessionipc: IF_BUTTON: want 3 bytes, got %d", len(raw))
		}
		comID := int(raw[1])<<8 | int(raw[2])
		return "IfButton", ifButtonArgs{ComID: comID}, nil
	case clientprot.MOVE_GAMECLICK:
		// actions.Walk: P1Enc(opcode) + P1(size) + P1(ctrlHeld) + P2(startX) + P2(startZ)
		// + 2 signed bytes per further waypoint — see walk.go's own doc comment for the
		// exact layout this mirrors.
		if len(raw) < 7 {
			return "", nil, fmt.Errorf("sessionipc: MOVE_GAMECLICK: want at least 7 bytes, got %d", len(raw))
		}
		size := int(raw[1])
		if len(raw) != size+2 {
			return "", nil, fmt.Errorf("sessionipc: MOVE_GAMECLICK: size byte says %d, buffer has %d", size, len(raw)-2)
		}
		if (size-3)%2 != 0 {
			return "", nil, fmt.Errorf("sessionipc: MOVE_GAMECLICK: size %d is not a valid waypoint count encoding", size)
		}
		n := (size - 3) / 2
		run := raw[2] != 0
		startX := int(raw[3])<<8 | int(raw[4])
		startZ := int(raw[5])<<8 | int(raw[6])
		waypoints := make([]pathfind.NavPoint, n)
		waypoints[0] = pathfind.NavPoint{X: startX, Z: startZ}
		pos := 7
		for i := 1; i < n; i++ {
			dx := int(int8(raw[pos]))
			dz := int(int8(raw[pos+1]))
			pos += 2
			waypoints[i] = pathfind.NavPoint{X: startX + dx, Z: startZ + dz}
		}
		return "Walk", walkArgs{Waypoints: waypoints, Run: run}, nil
	case clientprot.OPLOCU:
		// actions.OpLocU: P1Enc(opcode) + P2(x) + P2(z) + P2(locType) + P2(useObj) +
		// P2(useSlot) + P2(useCom) — 13 bytes, one fixed opcode (unlike OpLoc/OpObj's
		// 5-option family); its CYCLELOGIC2 side-packet is the host's to write (see cheats).
		if len(raw) != 13 {
			return "", nil, fmt.Errorf("sessionipc: OPLOCU: want 13 bytes, got %d", len(raw))
		}
		g2 := func(i int) int { return int(raw[i])<<8 | int(raw[i+1]) }
		return "OpLocU", opLocUArgs{
			X: g2(1), Z: g2(3), LocType: g2(5),
			UseObj: int32(g2(7)), UseSlot: g2(9), UseCom: g2(11),
		}, nil
	default:
		if option, ok := opLocOpcodeOption(raw[0]); ok {
			// actions.OpLoc: P1Enc(opcode) + P2(x) + P2(z) + P2(locType).
			x, z, locType, err := decodeOpLocShaped(raw)
			if err != nil {
				return "", nil, fmt.Errorf("sessionipc: OPLOC%d: %w", option, err)
			}
			return "OpLoc", opLocArgs{Option: option, X: x, Z: z, LocType: locType}, nil
		}
		if option, ok := opHeldOpcodeOption(raw[0]); ok {
			// actions.OpHeld (used directly and via Eat/Bury/Equipment.Equip):
			// P1Enc(opcode) + P2(objId) + P2(slot) + P2(comId).
			objID, slot, comID, err := decodeOpLocShaped(raw)
			if err != nil {
				return "", nil, fmt.Errorf("sessionipc: OPHELD%d: %w", option, err)
			}
			return "OpHeld", opHeldArgs{Option: option, ObjID: int32(objID), Slot: slot, ComID: comID}, nil
		}
		if option, ok := invButtonOpcodeOption(raw[0]); ok {
			// actions.InvButton, option 2-5 only (see invButtonOpcodeOption's own doc
			// comment for why option 1 is deliberately excluded here).
			objID, slot, comID, err := decodeOpLocShaped(raw)
			if err != nil {
				return "", nil, fmt.Errorf("sessionipc: INV_BUTTON%d: %w", option, err)
			}
			return "InvButton", invButtonArgs{Option: option, ObjID: int32(objID), Slot: slot, ComID: comID}, nil
		}
		if option, ok := opObjOpcodeOption(raw[0]); ok {
			// actions.OpObj (used directly and via TakeGroundItem/InteractGroundItem):
			// P1Enc(opcode) + P2(x) + P2(z) + P2(itemType) — the same shape as OpLoc.
			x, z, itemType, err := decodeOpLocShaped(raw)
			if err != nil {
				return "", nil, fmt.Errorf("sessionipc: OPOBJ%d: %w", option, err)
			}
			return "OpObj", opObjArgs{Option: option, X: x, Z: z, ItemType: itemType}, nil
		}
		if option, ok := opNpcOpcodeOption(raw[0]); ok {
			// actions.OpNpc: P1Enc(opcode) + P2(npcIndex) — 3 bytes, same shape as
			// IfButton but a distinct opcode family.
			if len(raw) != 3 {
				return "", nil, fmt.Errorf("sessionipc: OPNPC%d: want 3 bytes, got %d", option, len(raw))
			}
			npcIndex := int(raw[1])<<8 | int(raw[2])
			return "OpNpc", opNpcArgs{Option: option, NpcIndex: npcIndex}, nil
		}
		return "", nil, fmt.Errorf("%w (opcode %d)", errNoRemoteHandler, raw[0])
	}
}

// RegisterActionHandlers wires disp's "IfButton" and "Walk" names (the ones decodeSend
// above can produce) to the real, unmodified internal/actions functions of the same
// name, run against whatever real actions.Session sessionFor(account) returns — in
// production that would be the host's real *client.Session; in a test or the
// cmd/remotebottest demo, any actions.Session works, proving the dispatcher itself does
// not care which. A sessionFor that returns nil for an unknown account fails the call
// clearly rather than panicking.
func RegisterActionHandlers(disp *Dispatcher, sessionFor func(account string) actions.Session) {
	disp.Register("IfButton", func(account string, raw json.RawMessage) error {
		var a ifButtonArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("sessionipc: decoding IfButton args: %w", err)
		}
		sess := sessionFor(account)
		if sess == nil {
			return fmt.Errorf("sessionipc: no host session for account %q", account)
		}
		return actions.IfButton(sess, a.ComID)
	})
	disp.Register("Walk", func(account string, raw json.RawMessage) error {
		var a walkArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("sessionipc: decoding Walk args: %w", err)
		}
		sess := sessionFor(account)
		if sess == nil {
			return fmt.Errorf("sessionipc: no host session for account %q", account)
		}
		return actions.Walk(sess, a.Waypoints, a.Run)
	})
	disp.Register("OpLoc", func(account string, raw json.RawMessage) error {
		var a opLocArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("sessionipc: decoding OpLoc args: %w", err)
		}
		sess := sessionFor(account)
		if sess == nil {
			return fmt.Errorf("sessionipc: no host session for account %q", account)
		}
		return actions.OpLoc(sess, a.Option, a.X, a.Z, a.LocType)
	})
	disp.Register("OpHeld", func(account string, raw json.RawMessage) error {
		var a opHeldArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("sessionipc: decoding OpHeld args: %w", err)
		}
		sess := sessionFor(account)
		if sess == nil {
			return fmt.Errorf("sessionipc: no host session for account %q", account)
		}
		return actions.OpHeld(sess, a.Option, a.ObjID, a.Slot, a.ComID)
	})
	disp.Register("InvButton", func(account string, raw json.RawMessage) error {
		var a invButtonArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("sessionipc: decoding InvButton args: %w", err)
		}
		sess := sessionFor(account)
		if sess == nil {
			return fmt.Errorf("sessionipc: no host session for account %q", account)
		}
		return actions.InvButton(sess, a.Option, a.ObjID, a.Slot, a.ComID)
	})
	disp.Register("OpNpc", func(account string, raw json.RawMessage) error {
		var a opNpcArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("sessionipc: decoding OpNpc args: %w", err)
		}
		sess := sessionFor(account)
		if sess == nil {
			return fmt.Errorf("sessionipc: no host session for account %q", account)
		}
		return actions.OpNpc(sess, a.Option, a.NpcIndex)
	})
	disp.Register("OpObj", func(account string, raw json.RawMessage) error {
		var a opObjArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("sessionipc: decoding OpObj args: %w", err)
		}
		sess := sessionFor(account)
		if sess == nil {
			return fmt.Errorf("sessionipc: no host session for account %q", account)
		}
		return actions.OpObj(sess, a.Option, a.X, a.Z, a.ItemType)
	})
	disp.Register("OpLocU", func(account string, raw json.RawMessage) error {
		var a opLocUArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("sessionipc: decoding OpLocU args: %w", err)
		}
		sess := sessionFor(account)
		if sess == nil {
			return fmt.Errorf("sessionipc: no host session for account %q", account)
		}
		return actions.OpLocU(sess, a.X, a.Z, a.LocType, a.UseObj, a.UseSlot, a.UseCom)
	})
}
