package runecraftrunner

import (
	"errors"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
)

// THE ONE-WRITE ACCEPT (CR-2; runecraftplan/together.go has the why, the engine rules and the failure).
//
// GateTogether lets the crafter's GateStep write his confirm-screen Accept and the talisman on the ruins as ONE websocket message, the
// moment the carrier's accept is on his screen ("Other player has accepted."): the accept is then the second, it completes the trade inside
// its own packet, and the use, read after it in the same tick, finds the trade over and runs. Nothing here weakens the handshake: the offers
// are checked against what was agreed before anything is written, a carrier that has not accepted is WAITED for (GateTogetherWait) and
// then accepted the plain way, and what comes of the write is judged by the pack -- a trade that did not bring the essence is a failed trade,
// and says the talisman had gone.

// TalismanSent is a use of the talisman on the ruins that has been written and not yet seen to land.
type TalismanSent struct {
	// Tick is the tick the client had seen when it was written, At the time; Withdraw takes back the announcement made to the Guardian
	// (randomevents.ExpectLandings) that a landing in the temple is coming.
	Tick     int
	At       time.Time
	Withdraw func()
}

// GateTogether is the crafter's offer to write the talisman with his accept. A nil one (GateOptions.Together) is the accept on its own.
type GateTogether struct {
	// Altar and Ruins are the temple whose ruins the talisman is used on.
	Altar runecraftplan.Altar
	Ruins locindex.Match
	// Plan is asked ONCE per confirm screen whose offers match the trade, before anything is waited for or written. It says whether this
	// cycle is to try: the account stands where the engine accepts a use on the ruins, holds the talisman, is not locked by a cast, the
	// view is current, and the one-write accept is not in its back-off. False accepts the plain way at once.
	Plan func() bool
}

// TogetherInfo is what the one-write accept did in one handshake.
type TogetherInfo struct {
	// Planned: Plan said yes for this screen. Sent: the accept and the talisman went out in one message (Talisman says when).
	Planned, Sent bool
	// WaitedOut: the carrier's accept did not show in GateTogetherWait and the accept went out on its own. Why says why a planned cycle
	// was plain (this or an input held by another caller, a message that could not be built).
	WaitedOut bool
	Why       string
	// ConfirmTick is the tick the matching confirm screen was first seen in, OtherTick the tick the carrier's accept was first seen in
	// (0: never), Talisman the use that was written.
	ConfirmTick, OtherTick int
	Talisman               *TalismanSent
}

// errTogetherPack means the talisman is not in the pack: nothing to write.
var errTogetherPack = errors.New("the talisman is not in the pack")

// send writes the confirm accept and the talisman use as one message and says what it wrote. held is set, with nothing written, when another
// caller holds the account's input (actions.MovementHeld: the Guardian handling an event); the caller accepts the plain way then.
func (g *GateTogether) send(d *fleetrunner.Deps) (sent *TalismanSent, held bool, err error) {
	if actions.MovementHeld(d.Session) {
		return nil, true, nil
	}
	slot, ok := slotOf(d, int32(g.Altar.TalismanID))
	if !ok {
		return nil, false, errTogetherPack
	}
	b := actions.NewCoalescer(d.Session)
	s := b.Session()
	// The accept first: it is the packet that completes the trade, and the use is read after it.
	if err := actions.NewTrade(s).Accept(); err != nil {
		b.Discard()
		return nil, false, err
	}
	if err := actions.OpLocU(s, g.Ruins.X, g.Ruins.Z, g.Ruins.ID, int32(g.Altar.TalismanID), slot, PlayerPack); err != nil {
		b.Discard()
		return nil, false, err
	}
	if b.Pending() != 2 {
		// A part that queued nothing would leave an accept without its use or the other way about.
		b.Discard()
		return nil, false, errors.New("the accept and the use did not both queue")
	}
	withdraw := expect(d, "runecraft: the "+g.Altar.Talisman+" on the "+g.Altar.Name+" ruins (written with the trade's accept)", randomevents.LandingNear(g.Altar.Enter.X, g.Altar.Enter.Z, 3))
	sent = &TalismanSent{Tick: d.Session.State().Tick(), At: time.Now(), Withdraw: withdraw}
	if err := b.Flush(); err != nil {
		withdraw()
		return nil, false, err
	}
	return sent, false, nil
}
