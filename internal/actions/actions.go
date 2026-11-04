// Package actions ports the packet-level half of ClientAdapter.actions + Client.ts's
// doAction for the menu actions Phase 2 uses. Each function writes exactly the bytes the
// real client writes for that MiniMenuAction, including the anti-cheat side-packets that
// ride along, via a Session's Send (which supplies the ISAAC-keyed writer).
//
// What is NOT replicated here: tryMove. Client.ts runs its A* pathfinder and queues a
// walk alongside every OP_PLAYER/OP_NPC/OP_LOC click. That is webwalking (Phase 3+);
// Phase 2's mule stands still and its partners come to it, so these send the action
// packet alone. Whether the server accepts an OP_PLAYER4 with no accompanying movement
// is verified empirically by cmd/tradetest, not assumed.
package actions

import (
	"errors"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

// CheatEnabled gates every call to Cheat below. Default false — safe by construction for
// any caller that forgets to check it themselves, which is the point: added 2026-09-16
// right before this codebase's first live server run, once it became clear that several
// of the same day's own random-event/combat fixes (fleeFromCombat, checkFailTeleport,
// HandleEvade, HandleHazard — see internal/randomevents/handle.go) had grown unconditional
// actions.Cheat calls with no local-only guard at all, unlike freshstartrunner's own
// existing LocalRescueTeleport flag. CLIENT_CHEAT (see Cheat's own doc comment) is not a
// public chat message a human bystander could see — it is a dedicated opcode a real,
// unmodified client only ever sends from its own staff-only admin command entry, which a
// production the live server account cannot reach. A real client therefore never transmits this
// opcode at all unless the account is staff; the engine's own ClientCheatHandler.ts gates
// every effect behind staffModLevel, but the mere act of a non-staff connection sending
// this opcode is already something no genuine client would ever do, independent of
// whether the requested command succeeds — exactly the kind of client-fingerprint signal
// an anti-cheat system would key on. Set true only by a command-line flag meant for a
// local Lost City engine under this project's own control (see cmd/controlpanel's
// -enable-cheats), never for wss://w1.example.invalid.
var CheatEnabled = false

// ErrCheatDisabled is what Cheat returns when CheatEnabled is false — every existing call
// site already logs a Cheat error and carries on (see fleeFromCombat's own "teleport
// handler error" logging), so this fails safe with no code changes needed at any of them.
var ErrCheatDisabled = errors.New("cheat commands disabled (production mode) — pass -enable-cheats only against a local test engine")

// Session is what actions need from a live connection; *client.Session satisfies it.
type Session interface {
	Send(build func(w *packet.Packet)) error
	State() *gamestate.State
	Cheats() *anticheat.Counters
}

// invButtonOpcodes maps inv option 1..5 to the opcode doAction sends for INV_BUTTON1..5.
var invButtonOpcodes = [5]int{
	clientprot.INV_BUTTON1,
	clientprot.INV_BUTTON2,
	clientprot.INV_BUTTON3,
	clientprot.INV_BUTTON4,
	clientprot.INV_BUTTON5,
}

// OpPlayer4 ports the OP_PLAYER4 branch of doAction ("Trade with" is player op 4 —
// login.rs2 set_player_op("Trade with", 4)): the oplogic5 side-packet if due, then
// OPPLAYER4 + p2(targetIndex).
func OpPlayer4(s Session, targetIndex int32) error {
	return s.Send(func(w *packet.Packet) {
		s.Cheats().BeforeOpPlayer4(w, targetIndex)
		w.P1Enc(clientprot.OPPLAYER4)
		w.P2(uint16(targetIndex))
	})
}

// OpPlayer2 ports the OP_PLAYER2 branch of doAction ("Attack" is player op 2 in the Wilderness —
// wilderness.rs2 set_player_op("Attack", 2)): OPPLAYER2 + p2(targetIndex), with no anticheat
// side-packet (Client.ts sends none for this op).
func OpPlayer2(s Session, targetIndex int32) error {
	return s.Send(func(w *packet.Packet) {
		w.P1Enc(clientprot.OPPLAYER2)
		w.P2(uint16(targetIndex))
	})
}

// InvButton ports the INV_BUTTON1..5 branch: opcode for the option, then
// p2(objId) p2(slot) p2(comId). option is 1-based like Input.invButton's `op`.
func InvButton(s Session, option int, objID int32, slot int, comID int) error {
	if option < 1 || option > 5 {
		return errBadOption
	}
	return s.Send(func(w *packet.Packet) {
		if option == 1 {
			s.Cheats().BeforeInvButton1(w, objID)
		}
		w.P1Enc(byte(invButtonOpcodes[option-1]))
		w.P2(uint16(objID))
		w.P2(uint16(slot))
		w.P2(uint16(comID))
	})
}

// IfButton ports the IF_BUTTON branch for a plain server-side button (clientCode == 0,
// which is what trade's accept/decline are): IF_BUTTON + p2(comId).
func IfButton(s Session, comID int) error {
	return s.Send(func(w *packet.Packet) {
		w.P1Enc(clientprot.IF_BUTTON)
		w.P2(uint16(comID))
	})
}

// CloseModal ports the client's private closeModal(): CLOSE_MODAL with no payload, and
// the local bookkeeping that treats everything as closed immediately.
func CloseModal(s Session) error {
	err := s.Send(func(w *packet.Packet) {
		w.P1Enc(clientprot.CLOSE_MODAL)
	})
	if err == nil {
		s.State().LocalCloseModal()
	}
	return err
}

// ResumePauseButton ports the PAUSE_BUTTON branch ("Click here to continue"): sent at
// most once per interface open, gated by the resumedPauseButton latch.
func ResumePauseButton(s Session, comID int) error {
	if s.State().PauseButtonResumed() {
		return nil
	}
	err := s.Send(func(w *packet.Packet) {
		w.P1Enc(clientprot.RESUME_PAUSEBUTTON)
		w.P2(uint16(comID))
	})
	if err == nil {
		s.State().MarkPauseButtonResumed()
	}
	return err
}

// ResumeCountDialog sends RESUME_P_COUNTDIALOG with value whether or not the client has seen the
// dialog yet: for a stream sent ahead, where the Withdraw-X that opens the dialog is in the same
// message and the engine resumes the script in the same read (ResumePCountDialogHandler).
func ResumeCountDialog(s Session, value int32) error {
	return s.Send(func(w *packet.Packet) {
		w.P1Enc(clientprot.RESUME_P_COUNTDIALOG)
		w.P4(max(value, 0))
	})
}

// AnswerCountDialog ports actions.answerCountDialog: RESUME_P_COUNTDIALOG + p4(value),
// only while the count dialog is actually open.
func AnswerCountDialog(s Session, value int32) error {
	if !s.State().CountDialogOpen() {
		return errNoCountDialog
	}
	if value < 0 {
		value = 0
	}
	err := s.Send(func(w *packet.Packet) {
		w.P1Enc(clientprot.RESUME_P_COUNTDIALOG)
		w.P4(value)
	})
	if err == nil {
		s.State().ClearDialogInput()
	}
	return err
}

// Cheat ports the `::command` path of the chat handler: CLIENT_CHEAT, a 1-byte length
// (len+1 for the newline terminator pjstr adds), then the command without its `::`.
// Only useful against a local engine where the account has the staff level the command
// needs (see engine ClientCheatHandler.ts) — never against production.
func Cheat(s Session, command string) error {
	if !CheatEnabled {
		return ErrCheatDisabled
	}
	return s.Send(func(w *packet.Packet) {
		w.P1Enc(clientprot.CLIENT_CHEAT)
		w.P1(byte(len(command) + 1))
		w.PJStr(command)
	})
}

// settleCloseWait bounds SettleAfterClose: two ticks at the engine's normal speed and a margin, for a session that sees no
// ticks (a test's, or a link that has stopped).
const settleCloseWait = 2 * time.Second

// SettleAfterClose waits until a CLOSE_MODAL this client sent has certainly been applied by the engine
// (gamestate.State.ModalCloseSettled), at most settleCloseWait, and reports false when the caller should stop. Call it between
// closing a modal (a bank) and a click that opens one (knife on logs, a make menu, a dialogue): the engine closes the modal
// in its player turn, after the tick's input, so the new one would be closed with it (see gamestate/modalclose.go). It
// returns at once when no close was sent or it was long ago.
func SettleAfterClose(s Session, w Waiter) bool {
	st := s.State()
	if st.ModalCloseSettled() {
		return true
	}
	w = waiterOr(w)
	w.DelayUntil(st.ModalCloseSettled, settleCloseWait)
	return true
}
