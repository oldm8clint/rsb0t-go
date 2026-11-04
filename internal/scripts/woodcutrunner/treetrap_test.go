package woodcutrunner

// The live wedge of 2026-09-23 and the crowd losses around it, replayed against the REAL collision
// pack, loc index and config archive with a fake session: Chop itself runs, exactly as it ships,
// and only the walk and the network are stood in for.
//
// Skipped unless RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG name the three
// files (the fleet loads the same three: its log prints the paths at start). The geometry is the
// whole point of these tests — a synthetic map would prove the code against a map somebody drew.

import (
	"os"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

var realData struct {
	once sync.Once
	pack *pathfind.Pack
	locs *locindex.Index
	cfg  *cachecfg.Config
	err  string
}

func loadRealData(t *testing.T) (*pathfind.Pack, *locindex.Index, *cachecfg.Config) {
	t.Helper()
	pp, lp, cp := os.Getenv("RSB0T_COLLISION_PACK"), os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if pp == "" || lp == "" || cp == "" {
		t.Skip("set RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG to replay the Seers camps")
	}
	realData.once.Do(func() {
		var err error
		if realData.pack, err = pathfind.Load(pp); err != nil {
			realData.err = err.Error()
			return
		}
		if realData.locs, err = locindex.Load(lp); err != nil {
			realData.err = err.Error()
			return
		}
		if realData.cfg, err = cachecfg.LoadFile(cp); err != nil {
			realData.err = err.Error()
		}
	})
	if realData.err != "" {
		t.Fatalf("loading the real data: %s", realData.err)
	}
	return realData.pack, realData.locs, realData.cfg
}

// sentSession is fakeSession that counts what it is asked to send.
type sentSession struct {
	state  *gamestate.State
	cheats anticheat.Counters
	sent   int
}

func (s *sentSession) Send(build func(w *packet.Packet)) error { s.sent++; return nil }
func (s *sentSession) State() *gamestate.State                 { return s.state }
func (s *sentSession) Cheats() *anticheat.Counters             { return &s.cheats }

// bits MSB-first packs (width, value) pairs, the write-side mirror of packet.Packet.GBit.
func bits(fields ...[2]int) []byte {
	n := 0
	for _, f := range fields {
		n += f[0]
	}
	buf := make([]byte, (n+7)/8)
	pos := 0
	for _, f := range fields {
		for i := f[0] - 1; i >= 0; i-- {
			if (uint32(f[1])>>uint(i))&1 == 1 {
				buf[pos>>3] |= 0x80 >> uint(pos&7)
			}
			pos++
		}
	}
	return buf
}

// scene is one fake account's view: a fixed build area, so every later move and every NPC or
// player placed in it is an offset from the same base.
type scene struct {
	s            *gamestate.State
	baseX, baseZ int
}

// newScene builds the area around x,z and puts the account there, with REBUILD_NORMAL and the
// PLAYER_INFO local teleport branch — the two packets the engine itself sends.
func newScene(x, z int) *scene {
	st := gamestate.New()
	zx, zz := x/8, z/8
	w := packet.NewWriter(8)
	w.P2(uint16(zx))
	w.P2(uint16(zz))
	st.Handle(serverprot.REBUILD_NORMAL, w.Data[:w.Pos])
	sc := &scene{s: st, baseX: (zx - 6) * 8, baseZ: (zz - 6) * 8}
	sc.moveTo(x, z)
	return sc
}

func (sc *scene) moveTo(x, z int) {
	sc.s.Handle(serverprot.PLAYER_INFO, bits([2]int{1, 1}, [2]int{2, 3}, [2]int{2, 0},
		[2]int{7, x - sc.baseX}, [2]int{7, z - sc.baseZ}, [2]int{1, 0}, [2]int{1, 0}))
}

// addPlayer puts another account on x,z: a PLAYER_INFO with no local update, no previously
// tracked players and one new one.
func (sc *scene) addPlayer(index, x, z int) {
	lx, lz, _, _ := sc.s.Tile()
	dx, dz := x-lx, z-lz
	sc.s.Handle(serverprot.PLAYER_INFO, bits([2]int{1, 0}, [2]int{8, 0},
		[2]int{11, index}, [2]int{5, dx & 0x1f}, [2]int{5, dz & 0x1f}, [2]int{1, 0}, [2]int{1, 0},
		[2]int{11, 2047}))
}

// addNpcs puts NPCs of one type on the given tiles, in ONE NPC_INFO (each packet replaces the set).
func (sc *scene) addNpcs(typeID int, at ...tile.Tile) {
	lx, lz, _, _ := sc.s.Tile()
	f := [][2]int{{8, 0}}
	for i, t := range at {
		f = append(f, [2]int{14, i + 1}, [2]int{11, typeID}, [2]int{5, (t.X - lx) & 0x1f}, [2]int{5, (t.Z - lz) & 0x1f}, [2]int{1, 0}, [2]int{1, 0})
	}
	f = append(f, [2]int{14, 16383})
	sc.s.Handle(serverprot.NPC_INFO, bits(f...))
}

// recordWalks replaces approachWalk for one test, recording each stand tile walked at.
func recordWalks(t *testing.T, walk func(to tile.Tile) bool) *[]tile.Tile {
	t.Helper()
	var got []tile.Tile
	prev := approachWalk
	approachWalk = func(d *Deps, to tile.Tile) bool {
		got = append(got, to)
		return walk(to)
	}
	t.Cleanup(func() { approachWalk = prev })
	return &got
}

func trapDeps(t *testing.T, sc *scene) (*Deps, *sentSession) {
	pk, locs, cfg := loadRealData(t)
	sess := &sentSession{state: sc.s}
	return &Deps{
		Session: sess, Pack: pk, Locs: locs, Config: cfg,
		// Never sleep, and stop any loop that would: every path these tests care about returns
		// before its first wait, and a chop that got as far as clicking returns "stopped".
		Delay:  func(time.Duration) bool { return false },
		Blocks: &woodcutplan.TreeBlocks{},
		Claims: woodcutplan.NewTreeClaims(),
		Log:    func(s string) { t.Log(s) },
	}, sess
}

// TestAnAccountInsideTheBuildingIsCutOffNotExcludingEveryTree is Acct212, 2026-09-23 02:35-02:57
// Perth: 21.9 minutes at 2730,3478 inside the walled building south of the Seers normal camp,
// zero xp, "every tree within reach has an Ent at it" 658 times, no Ent anywhere.
//
// On the old code every pass excluded one or two of the fifteen trees in reach as "unreachable"
// (none of their stand tiles has a route out of the building), the next pass the next ones, and
// within fourteen seconds all fifteen were excluded and Chop said "all-blocked" — blaming an Ent —
// until the blocks expired and it started again, forever. The fix is that Chop sees the account
// cannot reach the camp's anchor either and says "cut-off", excluding nothing, so the caller walks
// out through the door-aware walker instead.
func TestAnAccountInsideTheBuildingIsCutOffNotExcludingEveryTree(t *testing.T) {
	sc := newScene(2730, 3478)
	d, sess := trapDeps(t, sc)
	walks := recordWalks(t, func(tile.Tile) bool { return false })
	tier := woodcutplan.TIERS[0]
	if !woodcutplan.AtCamp(tile.Tile{X: 2730, Z: 3478}, tier) {
		t.Fatal("setup: the trap tile must be inside the normal camp's leash")
	}
	if Routable(d.Pack, tile.Tile{X: 2730, Z: 3478}, tier.Anchor) {
		t.Fatal("setup: the static pack must have no route from the trap tile to the anchor — the building's door is baked shut")
	}
	for pass := 1; pass <= 20; pass++ {
		res := Chop(d, tier)
		if res.Reason != "cut-off" {
			t.Fatalf("pass %d: Chop = %q, want \"cut-off\" (blocks now %d: %s)", pass, res.Reason, d.Blocks.Len(time.Now()), res.Excluded)
		}
	}
	if n := d.Blocks.Len(time.Now()); n != 0 {
		t.Errorf("a cut-off account excluded %d trees; the trees are fine, the account is in the wrong place", n)
	}
	if len(*walks) != 0 || sess.sent != 0 {
		t.Errorf("nothing should have been walked at or sent from inside: %d walks, %d packets", len(*walks), sess.sent)
	}
}

// TestFromTheCampAnUnreachableTreeIsStillExcluded is the control for the one above: the cut-off
// check must not fire from where the camp's trees are, or it would walk every account at the
// anchor back to the anchor instead of excluding a tree it genuinely cannot stand at.
func TestFromTheCampAnUnreachableTreeIsStillExcluded(t *testing.T) {
	tier := woodcutplan.TIERS[0]
	sc := newScene(tier.Anchor.X, tier.Anchor.Z)
	d, _ := trapDeps(t, sc)
	walks := recordWalks(t, func(tile.Tile) bool { return false }) // every walk stalls
	res := Chop(d, tier)
	if res.Reason != "unreachable" {
		t.Fatalf("Chop from the anchor with every walk failing = %q, want \"unreachable\"", res.Reason)
	}
	if len(*walks) == 0 {
		t.Error("from the anchor the stand tiles are routable, so the approach should have walked at one")
	}
	if n := d.Blocks.Len(time.Now()); n != 1 {
		t.Errorf("the tree that could not be stood at should be excluded: %d blocks", n)
	}
	if got := d.Blocks.Tally([]tile.Tile{}, time.Now()); got.Total() != 0 {
		t.Errorf("Tally over no candidates = %v", got)
	}
}

// TestAnOccupiedStandTileIsWalkedOntoNotExcluded is the crowd half, measured in the lab with two
// real accounts (occtest, 2026-09-23): account A on 2711,3510, the only stand tile of the willow at
// 2711,3511; account B at 2713,3511, where that willow is the nearest. On the old code B's Chop
// skipped the tile as occupied, walked nowhere, returned "unreachable" and excluded the willow for
// 30 seconds — and B then walked onto the same tile anyway and chopped +675 xp, because players do
// not block players. Across the live cohort that exclusion was 97% of every "every tree ... has an
// Ent at it" line in a 16-account crowd run.
func TestAnOccupiedStandTileIsWalkedOntoNotExcluded(t *testing.T) {
	willow, stand := tile.Tile{X: 2711, Z: 3511}, tile.Tile{X: 2711, Z: 3510}
	sc := newScene(2713, 3511)
	sc.addPlayer(7, stand.X, stand.Z)
	d, sess := trapDeps(t, sc)
	if busy, _ := standOccupied(d, stand); !busy {
		t.Fatal("setup: the fake session must see a player on the stand tile")
	}
	walks := recordWalks(t, func(to tile.Tile) bool {
		sc.moveTo(to.X, to.Z) // the account arrives, as B did in the lab
		return true
	})
	tier := woodcutplan.TIERS[2] // Willow
	res := Chop(d, tier)
	if len(*walks) != 1 || (*walks)[0] != stand {
		t.Fatalf("the approach should have walked onto the occupied stand tile %v: walks %v (Chop = %q)", stand, *walks, res.Reason)
	}
	if res.Reason != "stopped" {
		t.Errorf("Chop = %q, want \"stopped\" — it clicked the willow and the test's Delay ended the swing", res.Reason)
	}
	if d.Blocks.Blocked(willow, time.Now()) {
		t.Error("the willow was excluded because somebody else was standing at it")
	}
	if sess.sent == 0 {
		t.Error("no chop click was sent")
	}
}

// TestNoTreeInReachIsNoTreeWhateverIsExcluded: the old check was "any live block anywhere", so an
// account with nothing of its species in reach and one stale exclusion from earlier was told every
// tree had an Ent at it.
func TestNoTreeInReachIsNoTreeWhateverIsExcluded(t *testing.T) {
	sc := newScene(2725, 3493) // the Seers bank: no yew within reach of it
	d, _ := trapDeps(t, sc)
	d.Blocks.Block(tile.Tile{X: 2708, Z: 3462}, time.Now()) // an Ent at a yew, earlier
	if res := Chop(d, woodcutplan.TIERS[4]); res.Reason != "no-tree" {
		t.Errorf("Chop with no yew in reach = %q (%s), want \"no-tree\"", res.Reason, res.Excluded)
	}
}

// TestAnEntAtEveryTreeIsStillAWaitAndSaysEnt is genuine Ent avoidance, unchanged: with an Ent at
// every willow in reach Chop clicks none of them, reports "all-blocked", and the tally says Ent —
// which is what keeps the caller's StepAwayFrom interrupt and the wait.
func TestAnEntAtEveryTreeIsStillAWaitAndSaysEnt(t *testing.T) {
	tier := woodcutplan.TIERS[2]
	sc := newScene(tier.Anchor.X, tier.Anchor.Z)
	d, sess := trapDeps(t, sc)
	all := d.Locs.Find(d.Config, locindex.Query{X: tier.Anchor.X, Z: tier.Anchor.Z, Level: 0, Radius: tier.Leash + 2, Name: tier.Loc, Op: woodcutplan.ChopOp}, nil)
	if len(all) < 2 {
		t.Fatalf("setup: expected several willows in reach, found %d", len(all))
	}
	var trees []tile.Tile
	for _, m := range all {
		trees = append(trees, tile.Tile{X: m.X, Z: m.Z})
	}
	sc.addNpcs(woodcutplan.EntNpcFirst, trees...)
	walks := recordWalks(t, func(tile.Tile) bool { return true })
	res := Chop(d, tier)
	if res.Reason != "all-blocked" {
		t.Fatalf("Chop with an Ent at every willow = %q, want \"all-blocked\"", res.Reason)
	}
	if res.Excluded.Ent != len(dedupe(trees)) || res.Excluded.Unreachable != 0 {
		t.Errorf("Excluded = %+v, want %d Ent and nothing else", res.Excluded, len(dedupe(trees)))
	}
	if len(*walks) != 0 || sess.sent != 0 {
		t.Errorf("nothing may be walked at or clicked with an Ent at every tree: %d walks, %d packets", len(*walks), sess.sent)
	}
}

func dedupe(ts []tile.Tile) []tile.Tile {
	seen := map[tile.Tile]bool{}
	var out []tile.Tile
	for _, t := range ts {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}
