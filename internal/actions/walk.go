package actions

import (
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
)

// maxWaypoints mirrors Client.ts's own MOVE_GAMECLICK builder: bufferSize is capped at
// 25 corners (including the start tile), and anything beyond that is simply not sent —
// the real client re-clicks as it walks and reaches the truncated tail naturally, which
// is exactly what internal/pathfind.FindPath's caller should do too for a route this long.
const maxWaypoints = 25

// maxOffset is the furthest a corner can be from the start tile in either axis: each is sent as
// a signed byte.
const maxOffset = 127

// SendableWaypoints is the prefix of waypoints one MOVE_GAMECLICK can carry: at most
// maxWaypoints corners, and none further than maxOffset tiles from the first in either axis. A
// corner beyond that used to be sent anyway and wrapped around as a signed byte -- 130 tiles east
// became 126 tiles WEST -- so a long route sent the account the wrong way. Like the client, the
// caller walks the rest with its next click.
func SendableWaypoints(waypoints []pathfind.NavPoint) []pathfind.NavPoint {
	if len(waypoints) > maxWaypoints {
		waypoints = waypoints[:maxWaypoints]
	}
	if len(waypoints) == 0 {
		return waypoints
	}
	start := waypoints[0]
	inRange := func(p pathfind.NavPoint) bool {
		dx, dz := p.X-start.X, p.Z-start.Z
		return dx <= maxOffset && dx >= -maxOffset && dz <= maxOffset && dz >= -maxOffset
	}
	for i, wp := range waypoints[1:] {
		if inRange(wp) {
			continue
		}
		// Stop on the furthest tile of this leg still in range. A corner-compressed route's legs
		// are straight (orthogonal or diagonal), so every tile along one is on the route.
		out := append([]pathfind.NavPoint(nil), waypoints[:i+1]...)
		prev := waypoints[i]
		sx, sz := sign(wp.X-prev.X), sign(wp.Z-prev.Z)
		p := prev
		for {
			next := pathfind.NavPoint{X: p.X + sx, Z: p.Z + sz, Level: p.Level}
			if !inRange(next) || (next.X == wp.X && next.Z == wp.Z) {
				break
			}
			p = next
		}
		if p != prev {
			out = append(out, p)
		}
		return out
	}
	return waypoints
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// Walk sends a MOVE_GAMECLICK for the given corner-compressed waypoints (as returned by
// pathfind.Pack.FindPath, which starts at the player's own tile) with ctrlHeld set for running.
// Ported from Client.ts's outgoing-click builder (~line 6008): a 1-byte ctrlHeld flag, the
// absolute start tile, then each further corner as a pair of signed bytes relative to the start.
//
// The start is the route's first corner, never the tile the player stands on. Client.ts traces its
// route back from the destination and stops before the player's tile, so its packet starts at the
// first turn (or at the destination for a straight line). The engine walks the start as the first
// waypoint (MoveClickHandler, queueWaypoints): a start the player stands on costs the tick's step to
// pop it, and one the server has already walked past, because this view is a tick behind, walks the
// player back to it. The tick lab measured both (claude/tickmanip-1005, 6 Oct 2026): every fresh
// click lost its tick, and with 300 ms of round trip 63% of the walker's clicks started behind the
// player, walking 82-90 tiles for a 70-tile route. So a first waypoint on the player's tile is
// dropped when more follow; a lone one is a stop and is sent as it is.
func Walk(s Session, waypoints []pathfind.NavPoint, run bool) error {
	return sendWalk(s, clientprot.MOVE_GAMECLICK, waypoints, run)
}

// WalkOpClick sends the same route as Walk but as MOVE_OPCLICK, the walk half of a click on a
// loc, npc or item. The server treats the two alike except for one thing: a MOVE_GAMECLICK clears
// the player's pending action and closes any dialogue they have open (Engine-TS MoveClickHandler
// "if (!message.opClick) player.clearPendingAction()"), a MOVE_OPCLICK does neither, because the
// real client always follows it with the op packet that does. Sent on its own it walks the player
// with a dialogue still up, and while a dialogue is up the player is "busy" and timers that need
// protected access (a NORMAL timer such as the maze's reward countdown) do not run.
//
// The real client never sends one alone, so use it only where that is the point, and check that
// the player moves: a server that drops it leaves them standing. Dropped, like Walk, while another
// caller holds the account's movement.
func WalkOpClick(s Session, waypoints []pathfind.NavPoint, run bool) error {
	return sendWalk(s, clientprot.MOVE_OPCLICK, waypoints, run)
}

func sendWalk(s Session, opcode int, waypoints []pathfind.NavPoint, run bool) error {
	if len(waypoints) == 0 {
		return errNoWaypoints
	}
	if movementHeld(s) {
		return nil // another caller holds this account's movement (movehold.go)
	}
	if len(waypoints) > 1 {
		if st := s.State(); st != nil {
			if x, z, level, ok := st.Tile(); ok && waypoints[0].X == x && waypoints[0].Z == z && waypoints[0].Level == level {
				waypoints = waypoints[1:]
			}
		}
	}
	waypoints = SendableWaypoints(waypoints)
	start := waypoints[0]
	ctrlHeld := byte(0)
	if run {
		ctrlHeld = 1
	}
	size := 2*len(waypoints) + 3

	return s.Send(func(w *packet.Packet) {
		w.P1Enc(byte(opcode))
		w.P1(byte(size))
		w.P1(ctrlHeld)
		w.P2(uint16(start.X))
		w.P2(uint16(start.Z))
		for _, wp := range waypoints[1:] {
			w.P1(byte(int8(wp.X - start.X)))
			w.P1(byte(int8(wp.Z - start.Z)))
		}
	})
}
