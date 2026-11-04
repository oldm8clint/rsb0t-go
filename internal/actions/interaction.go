package actions

import (
	"errors"

	"rsb0t-go/internal/gamestate"
)

// Interaction helpers: what to clear before sending an OP_NPC/OP_LOC/OP_OBJ click, and how to
// tell a click the server refused outright from one that is still in flight.
//
// This file began from a reading of the engine that turned out to be WRONG, and the correction is
// kept here because the wrong version is the intuitive one and has already been believed twice in
// this repo. Player.canAccess() is `!protect && !(delayed || containsModalInterface())`, and
// every step that executes an interaction is gated on it, so it looks as though a click sent
// while a dialogue is open must be accepted and then ignored forever, with no error, no chat line
// and no UNSET_MAP_FLAG to observe. It is not so: OpNpcHandler, OpObjHandler and OpLocHandler all
// call player.clearPendingAction() BEFORE setInteraction, and clearPendingAction is
// `this.clearInteraction(); this.closeModal();`. The click closes the dialogue itself.
//
// Measured rather than reasoned: with content's own level-up box on screen (~objbox, which is
// if_openchat plus p_pausebutton, the same shape levelup.rs2 opens on every level-up of every
// skill), an UNGUARDED ground-item take landed 3 times out of 3 in about 200ms
// (cmd/primitivebugtest -mode take). That tool keeps the counterfactual permanently, inverted, so
// this belief cannot quietly come back.
//
// So ClearInteractionBlock is NOT the cure for a stuck interaction, and nothing may be described
// as one on its account. It stays because closing a modal a script no longer wants is still the
// right thing to do before clicking, it costs a single packet, and it states the caller's intent
// instead of relying on the engine clearing the screen as a side effect.
//
// InteractionRejections is the genuinely new capability here. A click the server refuses outright
// — the obj is no longer there, the NPC is not visible to us, the tile is outside the build area,
// the player is delayed — answers UnsetMapFlag and is dropped, which this client already decoded
// as State.MapFlagUnsetCount. Nothing ever consulted it, so a refused click was indistinguishable
// from one still in flight.

// ErrInteractionBlocked is returned when a main or chat modal would not close.
var ErrInteractionBlocked = errors.New("actions: a main or chat modal stayed open, and the engine never runs an interaction while one is")

const (
	// modalClearAttempts bounds the retry: a level-up box that arrives while we are closing the
	// previous one is normal, an interface that reopens forever is not.
	modalClearAttempts = 3
	// modalClearTicks is how long the engine gets to act on our CLOSE_MODAL. It is not
	// cosmetic: CloseModalHandler deliberately does not close anything itself, it only sets
	// player.requestModalClose, which processQueues turns into a real Player.closeModal() on
	// the next tick. A click sent inside that window is still refused.
	modalClearTicks = 2
)

// InteractionBlocked reports whether an interaction sent right now would be accepted and then
// ignored. It mirrors the engine's own Player.containsModalInterface(): main or chat, not side.
func InteractionBlocked(s Session) bool {
	main, _, chat := s.State().Modals()
	return main != gamestate.NoModal || chat != gamestate.NoModal
}

// ClearInteractionBlock closes whatever would make the engine swallow the next interaction, and
// returns only once nothing is in the way. It is a no-op when nothing is open.
//
// Note that the returned state cannot be read straight off InteractionBlocked by the caller:
// CloseModal applies the client's own optimistic local close, so our view of the modal clears
// immediately, a tick or so before the engine's does. That is exactly why this waits
// modalClearTicks rather than polling our own flag, and why it re-checks afterwards — the second
// check catches a server-sent box that arrived in the meantime rather than our own stale read.
func ClearInteractionBlock(s Session, w Waiter) error {
	if !InteractionBlocked(s) {
		return nil
	}
	wait := waiterOr(w)
	for attempt := 0; attempt < modalClearAttempts; attempt++ {
		if err := CloseModal(s); err != nil {
			return err
		}
		if !wait.DelayTicks(modalClearTicks) {
			return ErrInteractionBlocked
		}
		if !InteractionBlocked(s) {
			return nil
		}
	}
	return ErrInteractionBlocked
}

// InteractionRejections is how many clicks this session has seen the server refuse outright, as
// a running total. A caller snapshots it before sending and compares after, the same way
// State.MapFlagUnsetCount's own doc comment describes for walk clicks.
//
// Treat a rise as corroborating evidence for a failure, never as the failure itself: the engine
// also unsets the map flag on some ordinary paths, so this belongs in the error message that
// explains a timeout rather than being the thing that decides one.
func InteractionRejections(s Session) int { return s.State().MapFlagUnsetCount() }
