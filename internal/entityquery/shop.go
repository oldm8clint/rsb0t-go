package entityquery

import (
	"errors"
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
)

// ErrNoShopkeeper is returned by OpenShop when no NPC by that name offers Trade.
var ErrNoShopkeeper = errors.New("entityquery: no NPC with that name offers Trade")

// ErrKeeperOutOfReach means the keeper is too far to trade with and the engine's own approach
// walk is not closing the gap, so more clicks cannot help.
//
// This is a caller's problem to fix rather than a fault in the primitive: upstream's
// Shop.open(npcName) does not walk either, and this function is given only a Session and a type
// resolver, so it has no pathfinder to walk with. Measured live, an account standing nine tiles
// from the Varrock sword shop's keeper gets walked partway by the engine and then stops outside
// the shop wall, because the server's own routefinder will not take it through the door. Saying
// so in one distinct error lets a caller walk and retry (which shoprunner.OpenKeeperShop and
// combatrunner.BuyFrom both already do) instead of reading it as the interface being flaky.
var ErrKeeperOutOfReach = errors.New("entityquery: the keeper is out of reach and the account is not getting closer")

const (
	// shopOpenAttempts is how many Trade clicks one call will spend.
	shopOpenAttempts = 4
	// shopOpenAttemptTimeout is the ceiling on ONE click, not on the call. It is deliberately
	// much longer than the three seconds this used to allow: the engine does the approach walk
	// itself (OpNpcHandler sets the interaction and Player.pathToPathingTarget walks us there),
	// so after a guardian flee the click is legitimately still in progress for ten seconds or
	// more. The old timeout expired mid-walk, and the retry then called clearPendingAction()
	// server-side and restarted the very walk it was waiting on, which is how one live run
	// burned about 90 seconds on a shop that two other runs opened in eleven.
	shopOpenAttemptTimeout = 12 * time.Second
	// shopOpenStall is how long the account may make no progress before the click is treated as
	// lost and re-sent. This is what keeps the longer timeout from being a longer hang: a click
	// that is working moves us, and a click that was dropped does not.
	shopOpenStall = 3 * time.Second
	// shopReachTiles is how close counts as being at the keeper, and it is ADJACENT, matching the
	// engine: Player.inOperableDistance calls reachedEntity for an NPC, which wants the tiles to
	// touch. Two tiles looks close enough to a reader and is not: measured live, an account the
	// engine walked to exactly 2 tiles from the Varrock sword keeper spent all four Trade clicks
	// there and never opened the shop, because the last step was never available to it.
	shopReachTiles = 1
)

// OpenShop ports Shop.open(npcName): Trade with the nearest NPC of that name offering Trade, and
// wait for the shop to appear. An open shop returns nil at once. Like upstream it does not walk
// to the keeper itself, so stand near one first; what it does now handle is the engine's own
// walk toward a keeper a few tiles off. A nil Waiter polls.
//
// What changed here, and what is honestly still unknown:
//
//   - A single fixed wait cannot tell a click that is still walking us to the keeper from one
//     that was dropped. The old 3s ceiling expired mid-walk and the retry then called
//     clearPendingAction() server-side, restarting the very approach it was waiting on. The wait
//     now ends early on a STALL instead, so a lost click is re-sent in about three seconds while
//     a working one is given as long as it needs. This is the structural fix.
//   - Out of range is now its own answer, ErrKeeperOutOfReach, instead of four clicks and a
//     generic timeout.
//   - A failure says what was seen: how far away the keeper was and how many clicks the server
//     refused outright, rather than only that a timeout elapsed.
//
// The ~90s intermittent failure this was written for was NOT reproduced: 8 of 8 opens from the
// stand and 8 of 8 with a dialogue already on screen, against a local engine
// (cmd/primitivebugtest -mode shop). The modal is cleared before each click for tidiness, not as
// a cure — see actions.ClearInteractionBlock, where the measurement that disproved that theory is
// recorded.
func OpenShop(s actions.Session, types Types, npcName string, w actions.Waiter) error {
	shop := actions.NewShop(s)
	rejectedBefore := actions.InteractionRejections(s)
	var keeper Entity
	clicks := 0
	stranded := false
	// best is the closest this call has got the account to the keeper. Comparing against it
	// across attempts, rather than against the previous attempt alone, is what catches a walk
	// that made progress and then stopped short.
	best := -1
	for attempt := 0; attempt < shopOpenAttempts; attempt++ {
		if shop.IsOpen() {
			return nil
		}
		found, ok := Npcs(s.State(), types).Name(npcName).Action("Trade").Nearest()
		if !ok {
			return ErrNoShopkeeper
		}
		keeper = found
		if err := actions.ClearInteractionBlock(s, w); err != nil {
			return fmt.Errorf("entityquery: nothing could be clicked in front of %s's shop: %w", npcName, err)
		}
		if err := Interact(s, keeper, "Trade"); err != nil {
			return err
		}
		clicks++
		if waitForShopOpen(s, shop, w) {
			return nil
		}
		// Where did that click leave us? Re-read the keeper, since both it and we may have moved.
		reach := keeper.Distance
		if after, ok := Npcs(s.State(), types).Name(npcName).Action("Trade").Nearest(); ok {
			keeper = after
			reach = after.Distance
		}
		if reach <= shopReachTiles {
			// Standing next to it and it still did not open: that is the interface failing, not
			// the walk, so spend the remaining clicks on it.
			continue
		}
		// Out of reach, and no closer than the best this call has managed. The engine's own walk
		// has stopped short, and more clicks only restart it from the same place.
		if best >= 0 && reach >= best {
			stranded = true
			break
		}
		if best < 0 || reach < best {
			best = reach
		}
	}
	if shop.IsOpen() {
		return nil
	}
	if stranded {
		return fmt.Errorf("entityquery: %s is %d tiles away at %d,%d and the engine's own walk is not closing that gap, so walk there first: %w",
			npcName, keeper.Distance, keeper.X, keeper.Z, ErrKeeperOutOfReach)
	}
	return fmt.Errorf("entityquery: %s's shop did not open after %d Trade clicks (keeper %d tiles away at %d,%d; the server refused %d clicks in that time): %w",
		npcName, clicks, keeper.Distance, keeper.X, keeper.Z,
		actions.InteractionRejections(s)-rejectedBefore, actions.ErrNotConfirmed)
}

// waitForShopOpen waits out one Trade click, ending early once the account has stopped making
// progress toward the keeper — see shopOpenStall. It reports the shop's real state rather than
// the wait's, so a stall and an expiry are the same answer to the caller: click again.
func waitForShopOpen(s actions.Session, shop *actions.Shop, w actions.Waiter) bool {
	where, movedAt := positionOf(s), time.Now()
	waitUntil(w, func() bool {
		if shop.IsOpen() {
			return true
		}
		if now := positionOf(s); now != where {
			where, movedAt = now, time.Now()
			return false
		}
		return time.Since(movedAt) >= shopOpenStall
	}, shopOpenAttemptTimeout)
	return shop.IsOpen()
}

// standing is the account's own tile, the evidence that the engine is walking us to the keeper.
type standing struct{ x, z, level int }

func positionOf(s actions.Session) standing {
	x, z, level, _ := s.State().Tile()
	return standing{x: x, z: z, level: level}
}

func waitUntil(w actions.Waiter, cond func() bool, timeout time.Duration) bool {
	if w != nil {
		return w.DelayUntil(cond, timeout)
	}
	deadline := time.Now().Add(timeout)
	for !cond() {
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
	return true
}
