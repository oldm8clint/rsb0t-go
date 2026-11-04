package essencerunner

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/tile"
)

// THE GATEWAY CLICK IS SENT FROM BESIDE THE NPC.
//
// Engine-TS 289 takes an option on an NPC only when Player.inOperableDistance holds: reachedEntity, which wants the two tiles to
// TOUCH orthogonally with no wall between them (a diagonal does not count, and neither does two tiles away). A click from anywhere
// else sets an interaction, and with clientRoutefinder on (its default) the server walks the account toward the NPC with the NAIVE
// strategy: a straight line, stopped by every wall and solid tile on the way. A player that cannot get closer is told "I can't
// reach that!" and the interaction is dropped -- a line nothing used to read.
//
// Wizard Distentor, Aubury and Wizard Cromperty wander (wanderrange 4, 5 and 4 around their spawn tiles) and the rooms they stand
// in are full of furniture. Live, 13-14 Oct 2026, the Wizards' Guild stretch walked to "within two tiles of his stand" -- which the
// Guild's door tile is -- and clicked from there: 37 teleports landed and 57 did not, and at 11:24 on 14 Oct four in a row, every
// one from the door tile, ended the main account's Runecraft round with no essence. On the real collision pack the door tile touches him on
// two of the 36 tiles he can be on.
//
// So the click is preceded by a step: read the NPC's tile now, ask the collision pack which tiles the engine would take the click
// from (pathfind.Pack.CanInteractFrom's rule), walk onto the nearest, read him again (he wanders while the account walks), and only
// then click. When the engine answers a click "I can't reach that!" the account steps beside him where he is now and clicks again at
// once; a click that is merely unanswered is sent again every teleportReclickTicks ticks from where the account stands, because a
// click sent into a cast is dropped harmlessly and a walk sent into one is not.
//
// Every step, click and give-up is logged ("teleport: ..."), with how the account stood to the NPC at each click.
const (
	// teleportApproachSteps is how many walks onto a tile beside the NPC one approach makes before it clicks: he wanders while the
	// account walks (a new destination one tick in eight, a tile a tick), so he is read again after every walk, and a fourth walk
	// means he has been walking away from the account the whole time.
	teleportApproachSteps = 4
	// teleportApproachTotal is how many such walks the whole attempt (the first approach and every resend) may make.
	teleportApproachTotal = 10
	// teleportStandsTried is how many of the tiles a click is taken from one walk tries to walk onto.
	teleportStandsTried = 3
	// teleportApproachWalk bounds one such walk: a few tiles, not a journey.
	teleportApproachWalk = 20 * time.Second
	// teleportMaxClicks is how many times one attempt clicks the NPC.
	teleportMaxClicks = 8
	// teleportNoStandPolls is how many times, 300 ms apart (10 s), the first approach looks again at an NPC standing where no tile
	// beside him can be stood on: he is wandering, and a tile away is usually enough.
	teleportNoStandPolls = 33
	// cantReachLine is the game message of a click the engine could not satisfy (Player.processInteraction), in lower case and
	// without its "!".
	cantReachLine = "can't reach that"
)

// TeleportOptions tunes TeleportInWith.
type TeleportOptions struct {
	// Walk walks the account to dest within radius tiles in at most timeout and reports whether it arrived: the caller's own walk,
	// which opens the doors a plain one cannot (the Wizards' Guild's east door is a double door). Nil is the plain collision-pack
	// walk (WalkNear). It is used for the step beside the NPC as well as for the walk toward his stand.
	Walk func(dest tile.Tile, radius int, timeout time.Duration) bool
}

func (o TeleportOptions) walk(d *Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
	if o.Walk != nil {
		return o.Walk(dest, radius, timeout)
	}
	return walkNearFor(d, dest, radius, timeout)
}

func npcTile(n gamestate.NPC) tile.Tile { return tile.Tile{X: n.X, Z: n.Z, Level: n.Level} }

func navPoint(t tile.Tile) pathfind.NavPoint {
	return pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level}
}

// reachesNpc is the engine's inOperableDistance for an NPC standing at npc: the two tiles touch orthogonally with no wall between.
// Never from the NPC's own tile (the engine's naive walk moves an account standing under its target away rather than interacting).
func reachesNpc(pack *pathfind.Pack, here, npc tile.Tile) bool {
	if here.Level != npc.Level || (here.X == npc.X && here.Z == npc.Z) {
		return false
	}
	return pack.CanInteractFrom(navPoint(here), navPoint(npc))
}

// ApproachStands says whether the engine would take an option on an NPC standing at npc from the tile here, and when it would not,
// the tiles it WOULD take one from, the nearest by the pack's own route first. It asks the collision pack (the engine's
// reachedEntity rule), not a distance: Chebyshev 1 includes the four diagonals, which touch at a corner only, and "two tiles from
// his stand" includes everything behind a wall.
//
// A stand the pack cannot walk to from here (a closed door between them) is listed after every one it can, nearest by straight line
// first, for a caller whose walk opens doors.
func ApproachStands(pack *pathfind.Pack, here, npc tile.Tile) (reached bool, stands []tile.Tile) {
	if reachesNpc(pack, here, npc) {
		return true, nil
	}
	type cand struct {
		at       tile.Tile
		steps    int
		walkable bool
	}
	var cands []cand
	for _, p := range pack.InteractTiles(navPoint(npc)) {
		if p.X == npc.X && p.Z == npc.Z {
			continue
		}
		if p.X == here.X && p.Z == here.Z && p.Level == here.Level {
			continue
		}
		c := cand{at: tile.Tile{X: p.X, Z: p.Z, Level: p.Level}}
		// A route that ends anywhere but on the tile is no route to it (FindPath settles for a tile near an unreachable goal).
		if res := pack.FindPath(navPoint(here), p); res.OK && len(res.Waypoints) > 0 && res.Waypoints[len(res.Waypoints)-1].X == p.X && res.Waypoints[len(res.Waypoints)-1].Z == p.Z {
			c.steps, c.walkable = res.Steps(), true
		} else {
			c.steps = tile.Chebyshev(here, c.at)
		}
		cands = append(cands, c)
	}
	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.walkable != b.walkable {
			return a.walkable
		}
		if a.steps != b.steps {
			return a.steps < b.steps
		}
		if a.at.X != b.at.X {
			return a.at.X < b.at.X
		}
		return a.at.Z < b.at.Z
	})
	for _, c := range cands {
		stands = append(stands, c.at)
	}
	return false, stands
}

// describeReach says, for the log, how the account stands to an NPC.
func describeReach(d *Deps, here, npc tile.Tile) string {
	switch {
	case d.Pack == nil:
		return "no collision pack to say whether that touches him"
	case reachesNpc(d.Pack, here, npc):
		return "beside him"
	case here.Level == npc.Level && here.X == npc.X && here.Z == npc.Z:
		return "on his tile, which the engine refuses"
	case here.Level == npc.Level && tile.Chebyshev(here, npc) <= 1:
		return "corner to corner or behind a wall, which the engine refuses"
	default:
		return strconv.Itoa(tile.Chebyshev(here, npc)) + " tile(s) away, not beside him"
	}
}

// whereAreWe is the account's and the NPC's tiles, for the log.
func whereAreWe(d *Deps, gw essenceplan.Gateway) string {
	here := currentTile(d)
	out := "the account is on " + strconv.Itoa(here.X) + "," + strconv.Itoa(here.Z)
	if npc, ok := FindGateway(d, gw); ok {
		out += " and " + gw.NPC + " on " + strconv.Itoa(npc.X) + "," + strconv.Itoa(npc.Z) + " (" + describeReach(d, here, npcTile(npc)) + ")"
	} else {
		out += " and " + gw.NPC + " is not in sight"
	}
	return out
}

// saidSince is the game's chat since t on one line, for the log.
func saidSince(d *Deps, t time.Time) string {
	if d.Chat == nil {
		return "(no chat log)"
	}
	say := strings.Join(strings.Fields(strings.ReplaceAll(strings.TrimSpace(d.Chat.Since(t)), "\n", " | ")), " ")
	if say == "" {
		return "nothing"
	}
	if len(say) > 240 {
		say = say[:240] + "..."
	}
	return strconv.Quote(say)
}

// chatMark is a place in the game's chat: the lines said after it answer whatever was sent after it. It counts lines rather than
// reading the clock, so a click and the answer to it are never mistaken for each other when they are stamped in the same instant.
type chatMark struct{ n int }

func markChat(d *Deps) chatMark {
	if d.Chat == nil {
		return chatMark{}
	}
	return chatMark{n: d.Chat.Total()}
}

// cantReach reports whether the engine said "I can't reach that!" after the mark.
func (m chatMark) cantReach(d *Deps) bool {
	if d.Chat == nil {
		return false
	}
	lines, _ := d.Chat.ReadAfter(m.n, time.Time{})
	for _, l := range lines {
		if strings.Contains(strings.ToLower(l), cantReachLine) {
			return true
		}
	}
	return false
}

// approacher walks the account beside one gateway NPC, one approach at a time, within a budget for the whole attempt.
type approacher struct {
	d   *Deps
	gw  essenceplan.Gateway
	opt TeleportOptions
	// left is how many walks onto a tile beside the NPC the attempt may still make.
	left int
}

func newApproacher(d *Deps, gw essenceplan.Gateway, opt TeleportOptions) *approacher {
	return &approacher{d: d, gw: gw, opt: opt, left: teleportApproachTotal}
}

// first finds the gateway NPC -- walking toward his stand when he is out of sight -- and puts the account beside him. It returns him
// as last read, or the reason it could not. Without a collision pack there is nothing to ask and it returns him as found.
func (a *approacher) first() (gamestate.NPC, string) {
	d, gw := a.d, a.gw
	stand := strconv.Itoa(gw.Stand.X) + "," + strconv.Itoa(gw.Stand.Z)
	npc, ok := FindGateway(d, gw)
	if !ok {
		if !a.opt.walk(d, gw.Stand, 2, walkNearTimeout) {
			return npc, gw.NPC + " is not in sight and the walk to " + stand + " did not arrive"
		}
		if npc, ok = FindGateway(d, gw); !ok {
			return npc, gw.NPC + " is not in sight at " + stand
		}
	}
	if d.Pack == nil {
		return npc, ""
	}
	return a.beside(npc, true)
}

// beside puts the account on a tile the engine takes a click on npc from, reading him again after every walk (he wanders). A walk
// that cannot be made, or an NPC that keeps moving, is not a failure: it returns him as last read and the click is sent anyway, as
// it always was, for the server to walk the rest. canWait lets it wait for an NPC standing where nothing can be stood beside him.
func (a *approacher) beside(npc gamestate.NPC, canWait bool) (gamestate.NPC, string) {
	d, gw := a.d, a.gw
	steps, polls := 0, 0
	for {
		at, here := npcTile(npc), currentTile(d)
		if here.Level != at.Level {
			return npc, ""
		}
		reached, stands := ApproachStands(d.Pack, here, at)
		if reached {
			if steps > 0 {
				d.logf("teleport: beside %s at %d,%d, standing on %d,%d, after %d step(s)", gw.NPC, at.X, at.Z, here.X, here.Z, steps)
			}
			return npc, ""
		}
		if len(stands) == 0 {
			// Sealed in (say behind his counter): nothing to stand on that touches him. He is wandering, so wait for him to move.
			sealed := "no tile beside " + gw.NPC + " at " + strconv.Itoa(at.X) + "," + strconv.Itoa(at.Z) + " can be stood on"
			if !canWait {
				return npc, sealed
			}
			if polls == 0 {
				d.logf("teleport: %s (the account is on %d,%d) - waiting for him to move", sealed, here.X, here.Z)
			}
			if polls >= teleportNoStandPolls {
				return npc, sealed + ", and he did not move in " + strconv.Itoa(polls) + " looks"
			}
			polls++
			if !d.Delay(300 * time.Millisecond) {
				return npc, "stopped"
			}
			n, ok := FindGateway(d, gw)
			if !ok {
				return npc, gw.NPC + " went out of sight while the account waited for him to move"
			}
			npc = n
			continue
		}
		if steps >= teleportApproachSteps || a.left <= 0 {
			d.logf("teleport: the account is still not beside %s after %d step(s) (%d left for this attempt): it is on %d,%d and he is on %d,%d; clicking anyway, the server will walk it the rest", gw.NPC, steps, a.left, here.X, here.Z, at.X, at.Z)
			return npc, ""
		}
		a.left--
		steps++
		moved := false
		for i, s := range stands {
			if i >= teleportStandsTried {
				break
			}
			if a.opt.walk(d, s, 0, teleportApproachWalk) {
				d.logf("teleport: stepped from %d,%d onto %d,%d beside %s at %d,%d (%s)", here.X, here.Z, s.X, s.Z, gw.NPC, at.X, at.Z, describeReach(d, here, at))
				moved = true
				break
			}
			d.logf("teleport: could not walk onto %d,%d beside %s at %d,%d", s.X, s.Z, gw.NPC, at.X, at.Z)
		}
		if !moved {
			if !d.Delay(0) {
				return npc, "stopped"
			}
			d.logf("teleport: could not walk beside %s at %d,%d from %d,%d; clicking anyway, the server will walk it", gw.NPC, at.X, at.Z, here.X, here.Z)
			return npc, ""
		}
		n, ok := FindGateway(d, gw)
		if !ok {
			return npc, gw.NPC + " went out of sight while the account stepped beside him"
		}
		npc = n
	}
}

// resend is why a click is being sent again.
type resend struct {
	// CantReach is set when the game answered the last click "I can't reach that!".
	CantReach bool
	// Ticks is how many server ticks have passed since the last click.
	Ticks int
}

func (r resend) why() string {
	if r.CantReach {
		return `the game said "I can't reach that!"`
	}
	return fmt.Sprintf("%d ticks after the click and nothing landed", r.Ticks)
}

// again is the click sent again, from beside the NPC when the game said the last one failed. The NPC is read afresh (he wanders),
// and when the engine answered "I can't reach that!" and the account is not beside him now, the account steps beside him first.
// Without that answer it only clicks, even when he has wandered off: a click sent into a cast is dropped harmlessly, but a WALK sent
// into one is not (the walk's own wait would still be running when the teleport lands), and a click he cannot be reached for is
// answered at once with the line this acts on. Returns the chat mark of the click sent, and whether one was.
func (a *approacher) again(r resend, click func(npc gamestate.NPC, again string) (chatMark, bool)) (chatMark, bool) {
	d := a.d
	npc, ok := FindGateway(d, a.gw)
	if !ok {
		return chatMark{}, false
	}
	if d.Pack != nil && r.CantReach && !reachesNpc(d.Pack, currentTile(d), npcTile(npc)) {
		n, why := a.beside(npc, false)
		if why != "" {
			d.logf("teleport: %s, and cannot step beside %s: %s", r.why(), a.gw.NPC, why)
			return chatMark{}, false
		}
		npc = n
	}
	return click(npc, r.why())
}
