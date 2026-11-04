package mastercraftrunner

// THE GAMES ROOM ROUTE's I/O (mastercraftplan/chestalch.go has the content facts and the decision): rub a
// Games necklace, take "Burthorpe Games Rooms." from its menu, and climb the room's two flights of stairs
// out to Burthorpe, from the exits the Guardian's catalogue holds (randomevents.strandsites.go, "the
// Burthorpe Games Room": 4622 at 2207,4935, then 4627 at 2205,4935 on level 1).
//
// The room is a pocket the walk graph has no edge out of, so the climb is done here, deterministically
// and without the walker: from the account's own tile the catalogue's step for it names the stands and the
// loc; a plain path over the collision pack walks to a stand, the loc is operated, and the next step is
// read from where that left the account.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/tile"
)

// GamesRoomSite is the catalogue entry's name, and GamesRoomExit the tile the stairs set the account down on in
// Burthorpe (content: the grey stairs lead to 2899,3564).
const (
	GamesRoomSite = "the Burthorpe Games Room"
	// GamesMenuOption is the first option of the necklace's teleport menu ("Burthorpe Games Rooms.").
	GamesMenuOption = "Burthorpe"
	// rubWait is how long the menu has to open after the rub, and landWait how long the teleport has to
	// land after the option is chosen.
	rubWait  = 5 * time.Second
	landWait = 8 * time.Second
	// climbBudget bounds the two flights of stairs together; stepWait is how long one Climb-up has to move
	// the account.
	climbBudget = 90 * time.Second
	stepWait    = 7 * time.Second
	standWait   = 25 * time.Second
)

// BurthorpeExit is where the second staircase sets the account down.
var BurthorpeExit = tile.Tile{X: 2899, Z: 3564, Level: 0}

// GamesNecklaceSlot is the first unnoted Games necklace in the pack, of any charge.
func GamesNecklaceSlot(d *fleetrunner.Deps) (gamestate.SlotItem, bool) {
	if d.Config == nil {
		return gamestate.SlotItem{}, false
	}
	for _, it := range packItems(d) {
		if o, ok := d.Config.Obj(int(it.ID)); ok && !o.Noted() && strings.HasPrefix(strings.ToLower(o.Name), "games necklace(") {
			return it, true
		}
	}
	return gamestate.SlotItem{}, false
}

// GamesNecklaceHeld is whether the pack holds a Games necklace.
func GamesNecklaceHeld(d *fleetrunner.Deps) bool {
	_, ok := GamesNecklaceSlot(d)
	return ok
}

// inGamesRoom is whether the account stands in the Games Room (either level).
func inGamesRoom(d *fleetrunner.Deps) bool {
	x, z, level, ok := d.Session.State().Tile()
	if !ok {
		return false
	}
	s, in := randomevents.StrandSiteAt(x, z, level)
	return in && s.Name == GamesRoomSite
}

// GamesRoute is what one RubToBurthorpe did.
type GamesRouteResult struct {
	// Rubbed says the menu was answered, Landed that the teleport put the account in the room, Out that the
	// stairs brought it to Burthorpe.
	Rubbed, Landed, Out bool
	// Climbs is the stairs operated.
	Climbs int
	// Took is the whole route.
	Took time.Duration
	// Why is the reason when Out is not set.
	Why string
}

// Describe is a one-line account for a log.
func (r GamesRouteResult) Describe() string {
	if r.Out {
		return fmt.Sprintf("Games Room route: rubbed, landed in the room and climbed %d flight(s) to Burthorpe in %s", r.Climbs, r.Took.Round(100*time.Millisecond))
	}
	return fmt.Sprintf("Games Room route failed after %s (rubbed %v, landed %v, %d climb(s)): %s", r.Took.Round(100*time.Millisecond), r.Rubbed, r.Landed, r.Climbs, r.Why)
}

// RubToBurthorpe rubs a Games necklace from the pack, chooses "Burthorpe Games Rooms." and climbs out of
// the room. hold runs before every send (false stops it). The necklace loses a charge only when the teleport
// lands (necklace_of_minigames.rs2 sets the new item after the teleport), so a menu that never answered costs
// none.
func RubToBurthorpe(d *fleetrunner.Deps, hold func() bool) (res GamesRouteResult) {
	start := time.Now()
	defer func() { res.Took = time.Since(start) }()
	if inGamesRoom(d) {
		res.Landed = true
		return climbOut(d, hold, res)
	}
	slot, ok := GamesNecklaceSlot(d)
	if !ok {
		res.Why = "no Games necklace in the pack"
		return res
	}
	if hold != nil && !hold() {
		res.Why = "held"
		return res
	}
	dlg := actions.NewChatDialog(d.Session, d.Components)
	// A rub's menu left open by the run staller is answered "Nowhere." by closing it first.
	if dlg.IsOpen() {
		_ = actions.CloseModal(d.Session)
		d.Delay(400 * time.Millisecond)
	}
	if err := actions.OpHeld(d.Session, 4, slot.ID, slot.Slot, fleetrunner.PlayerPack); err != nil {
		res.Why = "rubbing: " + err.Error()
		return res
	}
	var pick actions.Option
	found := false
	waitUntil(d, func() bool {
		for _, o := range liveOptions(d, dlg) {
			if strings.Contains(strings.ToLower(o.Text), strings.ToLower(GamesMenuOption)) {
				pick, found = o, true
				return true
			}
		}
		return false
	}, rubWait)
	if !found {
		_ = actions.CloseModal(d.Session)
		res.Why = "the rub's menu did not offer " + GamesMenuOption
		return res
	}
	if err := actions.IfButton(d.Session, pick.ComID); err != nil {
		res.Why = "choosing " + pick.Text + ": " + err.Error()
		return res
	}
	res.Rubbed = true
	waitUntil(d, func() bool { return inGamesRoom(d) }, landWait)
	if !inGamesRoom(d) {
		res.Why = "the teleport did not land in the Games Room (at " + fleetrunner.HereIs(d) + ")"
		return res
	}
	res.Landed = true
	return climbOut(d, hold, res)
}

// liveOptions are a dialogue's options with the labels the server set (the archive's own are placeholders).
func liveOptions(d *fleetrunner.Deps, dlg *actions.ChatDialog) []actions.Option {
	opts := dlg.Options()
	st := d.Session.State()
	for i := range opts {
		if text, ok := st.TextKnown(opts[i].ComID); ok && text != "" {
			opts[i].Text = text
		}
	}
	return opts
}

// climbOut climbs the room's stairs until the account stands outside it.
func climbOut(d *fleetrunner.Deps, hold func() bool, res GamesRouteResult) GamesRouteResult {
	deadline := time.Now().Add(climbBudget)
	failed := 0
	for inGamesRoom(d) && time.Now().Before(deadline) {
		if hold != nil && !hold() {
			res.Why = "held"
			return res
		}
		x, z, level, _ := d.Session.State().Tile()
		site, _ := randomevents.StrandSiteAt(x, z, level)
		step := site.StepFor(x, z, level)
		if step == nil {
			res.Why = fmt.Sprintf("the catalogue has no step for %d,%d,%d", x, z, level)
			return res
		}
		stands := step.Stands
		if len(stands) == 0 {
			stands = []randomevents.StrandTile{{X: x, Z: z, Level: level}}
		}
		stand := stands[failed%len(stands)]
		if x != stand.X || z != stand.Z || level != stand.Level {
			if !walkRoom(d, tile.Tile{X: x, Z: z, Level: level}, tile.Tile{X: stand.X, Z: stand.Z, Level: stand.Level}) {
				failed++
				continue
			}
		}
		beforeLevel := level
		if err := actions.OpLoc(d.Session, step.Op, step.LocX, step.LocZ, step.LocID); err != nil {
			res.Why = fmt.Sprintf("%s: %v", step.Name, err)
			return res
		}
		res.Climbs++
		waitUntil(d, func() bool {
			nx, nz, nl, ok := d.Session.State().Tile()
			if !ok {
				return false
			}
			if !inGamesRoom(d) || nl != beforeLevel {
				return true
			}
			return abs(nx-x)+abs(nz-z) >= max(step.MinMove, 2)
		}, stepWait)
		if nx, nz, nl, ok := d.Session.State().Tile(); ok && inGamesRoom(d) && nl == beforeLevel && abs(nx-x)+abs(nz-z) < max(step.MinMove, 2) {
			failed++ // the next click tries the next stand
		}
	}
	if inGamesRoom(d) {
		res.Why = "still in the Games Room after the climb (at " + fleetrunner.HereIs(d) + ")"
		return res
	}
	if here := hereTile(d); tile.Chebyshev(here, BurthorpeExit) > 20 || here.Level != 0 {
		res.Why = "out of the room but not in Burthorpe (at " + fleetrunner.HereIs(d) + ")"
		return res
	}
	res.Out = true
	return res
}

// walkRoom walks inside the room from one tile to a stand: a plain path over the collision pack, sent
// as one click, and waited for.
func walkRoom(d *fleetrunner.Deps, from, to tile.Tile) bool {
	a, b := pathfind.NavPoint{X: from.X, Z: from.Z, Level: from.Level}, pathfind.NavPoint{X: to.X, Z: to.Z, Level: to.Level}
	path := []pathfind.NavPoint{a, b}
	if d.Pack != nil {
		if res := d.Pack.FindPath(a, b); res.OK && len(res.Waypoints) > 1 {
			path = actions.SendableWaypoints(res.Waypoints)
		}
	}
	if err := actions.Walk(d.Session, path, true); err != nil {
		return false
	}
	waitUntil(d, func() bool { return hereTile(d) == to }, standWait)
	return hereTile(d) == to
}

func hereTile(d *fleetrunner.Deps) tile.Tile {
	x, z, level, _ := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
