// Package trawlerrunner is the I/O half of the FishingTrawler crew: it reads the pack, the boat, the overlay and
// the visible players, buys the kit, crosses the gangplank and works the boat, driven by the pure decisions in
// internal/scripts/trawlerplan. The world hop that leaves a game and the loop itself live in
// internal/bot/fishingtrawler.go.
//
// Every blocking wait goes through the Deps' Delayer so a stop request cancels it.
package trawlerrunner

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/antipoisonmakerunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/trawlerplan"
)

// Deps is the fleet runner's dependency bundle (session, pack, locs, config, walker, delayer, log).
type Deps = fleetrunner.Deps

const (
	walkTimeout = 150 * time.Second
	// locRadius is how far from the account loc reads reach (the boat is 64 tiles across at most).
	locRadius = 30
)

func logf(d *Deps, format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

// Here is the account's tile.
func Here(d *Deps) tile.Tile {
	x, z, level, _ := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

func pack(d *Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(fleetrunner.PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

// Counts are the kit items in the pack.
type Counts struct{ Paste, Rope, Bucket, BucketFull int }

// Held reads the kit counts.
func Held(d *Deps) Counts {
	p := pack(d)
	return Counts{
		Paste:      int(actions.CountByID(p, trawlerplan.PasteID)),
		Rope:       int(actions.CountByID(p, trawlerplan.RopeID)),
		Bucket:     int(actions.CountByID(p, trawlerplan.BucketID)),
		BucketFull: int(actions.CountByID(p, trawlerplan.BucketFullID)),
	}
}

// ---- overlay ----

// TimeText is the overlay's "time left" text ("" before the overlay has been filled).
func TimeText(d *Deps) string { return d.Session.State().Text(trawlerplan.OverlayTimeLeft) }

// NetRipped reports whether the overlay says "Ripped!".
func NetRipped(d *Deps) bool {
	if strings.TrimSpace(d.Session.State().Text(trawlerplan.OverlayNetRipped)) != "" {
		return true
	}
	// The overlay is refreshed only within 8 tiles of the boat's centre (live 3 Nov 2026 the net stayed ripped from 00:51 to
	// the end, the catch frozen at 530, and nobody went up): a ripped net's loc on this copy of the boat, either level, says so.
	here := Here(d)
	for _, e := range locsOf(d, trawlerplan.LocNetTopL, trawlerplan.LocNetTopR) {
		if e.X-here.X < 64 && here.X-e.X < 64 {
			return true
		}
	}
	return false
}

// Catch is the overlay's fish text ("Nothing", "12 fish").
func Catch(d *Deps) string {
	return strings.TrimSpace(d.Session.State().Text(trawlerplan.OverlayCatch))
}

// normName lowers a player name and treats underscores and spaces alike.
func normName(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "_", " "))
}

// AnchorAboard reports whether a visible player named anchor stands in a boat zone now.
func AnchorAboard(d *Deps, anchor string) bool {
	want := normName(anchor)
	for _, p := range d.Session.State().Players() {
		if normName(p.Name) == want && trawlerplan.OnBoat(tile.Tile{X: p.X, Z: p.Z, Level: p.Level}) {
			return true
		}
	}
	return false
}

// ---- locs ----

func locsOf(d *Deps, ids ...int) []entityquery.Entity {
	want := map[int]bool{}
	for _, id := range ids {
		want[id] = true
	}
	return entityquery.Locs(d.Session.State(), d.Config, d.Locs, locRadius).
		Where(func(e entityquery.Entity) bool { return want[e.ID] }).Results()
}

// sameLevel keeps the locs on the account's own level of its own copy of the boat: the flooded copy is 128 tiles east and
// both are in view (live 2 Nov 2026: crew on the flooded boat clicked the dry boat's net, "I can't reach that!").
func sameLevel(d *Deps, es []entityquery.Entity) []entityquery.Entity {
	here := Here(d)
	lvl := here.Level
	var out []entityquery.Entity
	for _, e := range es {
		if e.Level == lvl && (e.X-here.X < 64 && here.X-e.X < 64) {
			out = append(out, e)
		}
	}
	return out
}

func tilesOf(es []entityquery.Entity) []tile.Tile {
	out := make([]tile.Tile, 0, len(es))
	for _, e := range es {
		out = append(out, tile.Tile{X: e.X, Z: e.Z, Level: e.Level})
	}
	return out
}

// View reads the boat as the account sees it.
func View(d *Deps) trawlerplan.BoatView {
	c := Held(d)
	return trawlerplan.BoatView{
		Here:       Here(d),
		Leaks:      reachableLeaks(d, tilesOf(sameLevel(d, locsOf(d, trawlerplan.LocLeak)))),
		RippedNets: reachableLeaks(d, tilesOf(sameLevel(d, locsOf(d, trawlerplan.LocNetTopL, trawlerplan.LocNetTopR)))),
		NetRipped:  NetRipped(d),
		Paste:      c.Paste, Rope: c.Rope, Bucket: c.Bucket, BucketFull: c.BucketFull,
	}
}

func locAt(d *Deps, t tile.Tile, ids ...int) (entityquery.Entity, bool) {
	for _, e := range locsOf(d, ids...) {
		if e.X == t.X && e.Z == t.Z && e.Level == t.Level {
			return e, true
		}
	}
	return entityquery.Entity{}, false
}

// ---- walking, kit, gangplank ----

// WalkTo walks with the webwalk walker to within radius of dest.
func WalkTo(d *Deps, dest tile.Tile, radius int) bool {
	return fleetrunner.WalkTo(d, dest, radius, walkTimeout)
}

// BuyKit buys what the kit still lacks at the Port Khazard general store and reports what the pack holds
// afterwards. complete is false when the store did not have enough (the caller then goes with what it has).
func BuyKit(d *Deps, set trawlerplan.Settings) (c Counts, complete bool, err error) {
	c = Held(d)
	lines := []antipoisonmakerunner.Line{
		{ID: trawlerplan.PasteID, N: set.KitPaste - c.Paste},
		{ID: trawlerplan.RopeID, N: set.KitRope - c.Rope},
	}
	if set.Bucket {
		lines = append(lines, antipoisonmakerunner.Line{ID: trawlerplan.BucketID, N: max(1, set.KitBuckets) - c.Bucket - c.BucketFull})
	}
	complete = true
	for _, ln := range lines {
		if ln.N <= 0 {
			continue
		}
		got, berr := buyOne(d, ln)
		if berr != nil && err == nil {
			err = berr
		}
		if got < ln.N {
			complete = false
		}
	}
	return Held(d), complete, err
}

// buyOne tries the nearer keeper's post first and then the other.
func buyOne(d *Deps, ln antipoisonmakerunner.Line) (int, error) {
	var last error
	got := 0
	posts := trawlerplan.Stores
	if tile.Chebyshev(Here(d), posts[1]) < tile.Chebyshev(Here(d), posts[0]) {
		posts = []tile.Tile{posts[1], posts[0]}
	}
	for _, post := range posts {
		n, err := antipoisonmakerunner.BuyAt(d, []string{"Shop keeper"}, post, walkTimeout, antipoisonmakerunner.Line{ID: ln.ID, N: ln.N - got})
		got += n
		if got >= ln.N {
			return got, nil
		}
		last = err
		if err == nil {
			// The shop opened and bought what it had; the other keeper shares the stock, so stop.
			return got, nil
		}
	}
	return got, last
}

// LocBarrels are the floating barrels of the wreck that take a climber (sinkingbarrel1 and 2: trawler_sink.rs2 has no op for
// sinkingbarrel3; live 2 Nov 2026 the accounts that clicked it stayed in the water).
var LocBarrels = []int{2159, 2160}

// barrelTry turns the escape to the next barrel after one that did not take (process-wide; accounts take turns anyway).
var barrelTry atomic.Int64

// Escape climbs onto the nearest floating barrel after the boat sank, continues the message box, and waits to land at
// Port Sarim (^trawler_escape).
func Escape(d *Deps) error {
	es := locsOf(d, LocBarrels...)
	if len(es) == 0 {
		return fmt.Errorf("no floating barrel in view at %s", fleetrunner.HereIs(d))
	}
	here := Here(d)
	sort.Slice(es, func(i, j int) bool {
		return tile.Chebyshev(here, tile.Tile{X: es[i].X, Z: es[i].Z, Level: es[i].Level}) < tile.Chebyshev(here, tile.Tile{X: es[j].X, Z: es[j].Z, Level: es[j].Level})
	})
	// Every barrel in view, nearest first, a few seconds each (live 3 Nov 2026 08:13: 52 crew landed in the wreck about three
	// tiles from the nearest barrel and each spent 20 s on one barrel that never took them, until the fleet restarted; on
	// 2 Nov the crew that landed beside a barrel were out in a second). The game's answer is kept for the warning.
	start := int(barrelTry.Add(1)) % min(len(es), 2)
	var tried []string
	for k := range es {
		b := es[(start+k)%len(es)]
		at := time.Now()
		if err := actions.OpLoc(d.Session, 1, b.X, b.Z, b.ID); err != nil {
			return fmt.Errorf("barrel OpLoc: %w", err)
		}
		dlg := actions.NewChatDialog(d.Session, d.Components)
		if waitUntil(d, barrelWait, func() bool {
			if dlg.CanContinue() {
				_ = dlg.Continue()
			}
			return !trawlerplan.InWreck(Here(d))
		}) {
			return nil
		}
		tried = append(tried, fmt.Sprintf("%d,%d (game said %q)", b.X, b.Z, strings.TrimSpace(fleetrunner.ChatSince(d, at))))
	}
	return fmt.Errorf("still in the wreck at %s after climbing on the barrels at %s", fleetrunner.HereIs(d), strings.Join(tried, ", "))
}

// barrelWait is how long one barrel has to take the account out of the wreck: the climb is a click, a mesbox continued and a
// teleport, a second or two when it takes.
const barrelWait = 7 * time.Second

// Cross clicks the gangplank (walking to the lobby first) and reports whether the click went out.
func Cross(d *Deps) error {
	// Any level: content places the gangplank on level 1 over the pier (a bridge tile), and a player on the pier is on
	// level 0, so the scene may report either.
	var e entityquery.Entity
	ok := false
	for _, c := range locsOf(d, trawlerplan.LocGangplank) {
		if c.X == trawlerplan.Gangplank.X && c.Z == trawlerplan.Gangplank.Z {
			e, ok = c, true
			break
		}
	}
	if !ok {
		return fmt.Errorf("no gangplank at %d,%d from %d,%d", trawlerplan.Gangplank.X, trawlerplan.Gangplank.Z, Here(d).X, Here(d).Z)
	}
	// 2 Nov 2026: 547 crossings logged "crossed" overnight and every account stayed on the pier, so a crossing is now
	// confirmed: the account must reach the lobby deck (InLobby) within crossWait, else the server's answer is returned.
	// 2 Nov 2026 11:45: every click from 2675,3168-2676,3170 was answered "I can't reach that!". Step onto one of the tiles
	// beside the plank (the next one each attempt) before clicking, so the server's own approach is one tile at most.
	stand := crossStands[int(crossTry.Add(1))%len(crossStands)]
	if Here(d) != stand {
		fleetrunner.StepTo(d, stand)
	}
	at := time.Now()
	if err := actions.OpLoc(d.Session, 1, e.X, e.Z, e.ID); err != nil {
		return fmt.Errorf("gangplank OpLoc: %w", err)
	}
	if waitUntil(d, crossWait, func() bool { return trawlerplan.InLobby(Here(d)) }) {
		return nil
	}
	said := strings.TrimSpace(fleetrunner.ChatSince(d, at))
	return fmt.Errorf("crossed loc %d at %d,%d level %d from %d,%d,%d (tried from %d,%d) but still not on the lobby deck (game said %q)",
		e.ID, e.X, e.Z, e.Level, Here(d).X, Here(d).Z, Here(d).Level, stand.X, stand.Z, said)
}

// crossStands are the tiles a crossing is tried from, in turn: beside the plank on the pier side, then its corners.
// Live 2 Nov 2026 every crossing that landed was clicked from the plank's own pier tile, 2674,3170,0; from beside it the game
// said "I can't reach that!".
var crossStands = []tile.Tile{{X: 2674, Z: 3170}, {X: 2674, Z: 3170}, {X: 2675, Z: 3170}}

// crossTry picks the next of crossStands (process-wide; accounts take turns anyway).
var crossTry atomic.Int64

// crossWait is how long a gangplank crossing has to land on the lobby deck.
const crossWait = 6 * time.Second

// ---- aboard ----

const actWait = 6 * time.Second

func waitUntil(d *Deps, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !d.Delay(200 * time.Millisecond) {
			return false
		}
	}
	return cond()
}

// FillLeak clicks the leak at t and waits for the paste to be used (or the leak to go). It reports success.
// leakMiss remembers, per account, the leak tiles a fill did not reach ("I can't reach that!" live 2 Nov 2026: the anchor clicked
// the same leak for minutes and cast nothing), and leakMissFor how long one is left alone.
var (
	leakMissMu sync.Mutex
	leakMiss   = map[string]map[tile.Tile]time.Time{}
)

const leakMissFor = 8 * time.Second

func reachableLeaks(d *Deps, ts []tile.Tile) []tile.Tile {
	leakMissMu.Lock()
	defer leakMissMu.Unlock()
	miss := leakMiss[d.Me]
	out := ts[:0]
	for _, t := range ts {
		if until, ok := miss[t]; ok && time.Now().Before(until) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func noteLeakMiss(d *Deps, t tile.Tile) {
	leakMissMu.Lock()
	defer leakMissMu.Unlock()
	if leakMiss[d.Me] == nil {
		leakMiss[d.Me] = map[tile.Tile]time.Time{}
	}
	leakMiss[d.Me][t] = time.Now().Add(leakMissFor)
}

func FillLeak(d *Deps, t tile.Tile) bool {
	e, ok := locAt(d, t, trawlerplan.LocLeak)
	if !ok {
		return false
	}
	before := Held(d).Paste
	if err := entityquery.Interact(d.Session, e, "Fill"); err != nil {
		logf(d, "trawler: fill leak at %d,%d: %v", t.X, t.Z, err)
		return false
	}
	ok = waitUntil(d, actWait, func() bool {
		_, still := locAt(d, t, trawlerplan.LocLeak)
		return Held(d).Paste < before || !still
	})
	if used := before - Held(d).Paste; used > 0 {
		reportLeak(d, used) // the paste this account spent: a cost of the Fishing Trawler row
	} else {
		noteLeakMiss(d, t) // filled by someone else, or out of reach: the next leak first
	}
	return ok
}

// FixNet clicks the ripped net at t and waits for the overlay to stop saying Ripped (the repair can fail on a
// skill roll, so false just means try again).
func FixNet(d *Deps, t tile.Tile) bool {
	e, ok := locAt(d, t, trawlerplan.LocNetTopL, trawlerplan.LocNetTopR)
	if !ok {
		return false
	}
	rope := Held(d).Rope
	// Stand beside the net first (map m29_75, level 1: the deck column east of the nets, x+2, is walkable from z 4823 to
	// 4827); live 2 Nov 2026 every Inspect sent from the ladder's top tile came back "I can't reach that!".
	here := Here(d)
	stand := tile.Tile{X: t.X + 2, Z: min(max(here.Z, t.Z), t.Z+5), Level: t.Level}
	if here != stand {
		if !fleetrunner.StepTo(d, stand) {
			logf(d, "trawler: could not step beside the net at %d,%d (to %d,%d from %s)", t.X, t.Z, stand.X, stand.Z, fleetrunner.HereIs(d))
		}
	}
	at := time.Now()
	if err := entityquery.Interact(d.Session, e, "Inspect"); err != nil {
		logf(d, "trawler: fix net at %d,%d: %v", t.X, t.Z, err)
		return false
	}
	// The mend is this account's when its rope went (content deletes one on success): another account's mend clears the
	// overlay too, and is not this one's to report.
	mended := waitUntil(d, actWait, func() bool { return Held(d).Rope < rope })
	if mended {
		reportMend(d, rope-Held(d).Rope)
	} else if NetRipped(d) {
		// Live 2 Nov 2026 no crew mended the net in 80 tries: say what the game answered.
		said := strings.TrimSpace(fleetrunner.ChatSince(d, at))
		logf(d, "trawler: the net at %d,%d,%d is still ripped after Inspect from %s (game said %q)", t.X, t.Z, t.Level, fleetrunner.HereIs(d), said)
		if strings.Contains(said, "reach") {
			// A mend fixes both halves (trawler_net.rs2): the half this account cannot reach is left for the other one (live
			// 2 Nov 2026: "I can't reach that!" on 1880,4819 from the ladder top).
			noteLeakMiss(d, t)
		}
	}
	return mended || !NetRipped(d)
}

func slotOf(d *Deps, id int32) (gamestate.SlotItem, bool) {
	for _, it := range pack(d) {
		if it.ID == id {
			return it, true
		}
	}
	return gamestate.SlotItem{}, false
}

// Bail fills the empty bucket from the flooded hold (opheld1); it reports whether a full bucket appeared.
func Bail(d *Deps) bool {
	it, ok := slotOf(d, trawlerplan.BucketID)
	if !ok {
		return false
	}
	if err := actions.OpHeld(d.Session, 1, it.ID, it.Slot, fleetrunner.PlayerPack); err != nil {
		logf(d, "trawler: bail: %v", err)
		return false
	}
	return waitUntil(d, actWait, func() bool { return Held(d).BucketFull > 0 })
}

// EmptyBucket tips the full bucket over the side (opheld1); it reports whether an empty bucket came back.
func EmptyBucket(d *Deps) bool {
	it, ok := slotOf(d, trawlerplan.BucketFullID)
	if !ok {
		return false
	}
	if err := actions.OpHeld(d.Session, 1, it.ID, it.Slot, fleetrunner.PlayerPack); err != nil {
		logf(d, "trawler: empty bucket: %v", err)
		return false
	}
	return waitUntil(d, actWait, func() bool { return Held(d).Bucket > 0 })
}

// Climb takes the nearest ladder (up = "Climb-up" from the hold, else "Climb-down" from the deck) and waits for
// the level to change.
func Climb(d *Deps, up bool) bool {
	id, op := trawlerplan.LocLadderDown, "Climb-down"
	if up {
		id, op = trawlerplan.LocLadderUp, "Climb-up"
	}
	from := Here(d)
	var best entityquery.Entity
	bestD, found := 1<<30, false
	for _, e := range sameLevel(d, locsOf(d, id)) {
		dist := tile.Chebyshev(from, tile.Tile{X: e.X, Z: e.Z, Level: e.Level})
		if up {
			// Up is only ever for the net, and only the west ladder (mapsquare 29,75 at 28,26) lands on the part of the deck
			// the nets are on (live 2 Nov 2026: from the east ladder's top every mend was "I can't reach that!").
			dist = e.X
		}
		if dist < bestD {
			best, bestD, found = e, dist, true
		}
	}
	if !found {
		return false
	}
	if up {
		// The west ladder (hold 28,26) is boxed in by the hold's crates and walls; its one open side is the hull tile east of it
		// (map m29_75; live 3 Nov 2026 every climb sent from the hold was "I can't reach that!"): step there first.
		if side := (tile.Tile{X: best.X + 1, Z: best.Z, Level: best.Level}); Here(d) != side {
			fleetrunner.StepTo(d, side)
		}
	} else if dx := best.X - Here(d).X; dx >= 1 && dx <= 3 {
		// Down from the net deck: the ladder top's open side is the tile west of it, where a climb up lands (live 3 Nov 2026:
		// from 1882,4826 "I can't reach that!").
		if side := (tile.Tile{X: best.X - 1, Z: best.Z, Level: best.Level}); Here(d) != side {
			fleetrunner.StepTo(d, side)
		}
	}
	at := time.Now()
	if err := entityquery.Interact(d.Session, best, op); err != nil {
		logf(d, "trawler: %s: %v", op, err)
		return false
	}
	if waitUntil(d, 8*time.Second, func() bool { return Here(d).Level != from.Level }) {
		return true
	}
	// Live 3 Nov 2026 rope holders chose climb-up for minutes and never left the hold: say what the game answered.
	logf(d, "trawler: %s the ladder (loc %d) at %d,%d from %s did not change level (game said %q)", op, best.ID, best.X, best.Z,
		fleetrunner.HereIs(d), strings.TrimSpace(fleetrunner.ChatSince(d, at)))
	return false
}
