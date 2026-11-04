package randomevents

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/pathfind"
)

// The Swarm policy's pure parts: what the engine's leash is, how far a walk aims, which NPCs are the
// threat and which are other attackers, and where the spawn tile is remembered. The walk itself, on
// the real map where the live fleet got stuck, is swarm_flee_realpack_test.go.

func TestSwarmBreakDistanceClearsTheEnginesLeash(t *testing.T) {
	// Npc.validateTarget: a target is lost at Chebyshev distance > maxrange+1 from the spawn tile.
	if first := swarmLeash + 2; leashBreakFor(swarmLeash, 1) <= first {
		t.Fatalf("a first walk to %d from the spawn does not clear the leash (first tile that does: %d)", leashBreakFor(swarmLeash, 1), first)
	}
	if leashBreakFor(swarmLeash, 1) != 18 {
		t.Fatalf("first walk aims %d tiles from the spawn, want 18 (the live 3022,9588 that cleared a Swarm was 18 away)", leashBreakFor(swarmLeash, 1))
	}
	prev := 0
	for attempt := 1; attempt <= 8; attempt++ {
		d := leashBreakFor(swarmLeash, attempt)
		if d < prev {
			t.Fatalf("attempt %d aims %d, less than the attempt before (%d)", attempt, d, prev)
		}
		if d > maxFleeDist {
			t.Fatalf("attempt %d aims %d, past the flee ceiling %d", attempt, d, maxFleeDist)
		}
		prev = d
	}
	if leashBreakFor(swarmLeash, 2) <= leashBreakFor(swarmLeash, 1) || leashBreakFor(swarmLeash, 0) != leashBreakFor(swarmLeash, 1) {
		t.Fatalf("escalation: attempt 0/1/2 = %d/%d/%d", leashBreakFor(swarmLeash, 0), leashBreakFor(swarmLeash, 1), leashBreakFor(swarmLeash, 2))
	}
}

func TestEveryHostileEventMonsterButTheDwarfIsLeashed(t *testing.T) {
	// The Drunken Dwarf's maxrange is 10000 (antimacro.npc): there is no distance that leaves it, and it
	// is not an event monster the table covers. Every other hostile event monster has a leash.
	for typ := range hostileEventNpcIDs {
		maxrange, ok := eventLeashOf(typ)
		if !ok {
			t.Errorf("hostile event monster %d has no leash in the table", typ)
			continue
		}
		if !isLeashedEvent(typ) {
			t.Errorf("isLeashedEvent(%d) disagrees with the table", typ)
		}
		if maxrange < 3 || maxrange > swarmLeash {
			t.Errorf("event monster %d: leash %d is outside the content's 3..15", typ, maxrange)
		}
	}
	for typ := range eventLeashes {
		if !hostileEventNpcIDs[typ] {
			t.Errorf("leashed type %d is not a hostile event monster the guardian detects", typ)
		}
	}
	if isLeashedEvent(956) {
		t.Error("the Drunken Dwarf (956, maxrange 10000) cannot be left by distance")
	}
	if isLeashedEvent(407) {
		t.Error("the passive Strange plant seed (407) attacks nobody and has no leash walk")
	}
	if !hostileEventNpcIDs[swarmNpcType] || !hostileEventNpcIDs[angryPlantType] {
		t.Fatal("the Swarm and the angry Strange plant must be event monsters the guardian detects")
	}
}

func TestLeashBreakDistanceClearsTheEnginesLeashForEveryMonster(t *testing.T) {
	for typ, maxrange := range eventLeashes {
		// Npc.validateTarget: a target is lost at Chebyshev distance > maxrange+1 from the spawn tile.
		first := leashBreakFor(maxrange, 1)
		if first <= maxrange+1 {
			t.Errorf("type %d: a first walk to %d from the spawn does not clear a leash of %d", typ, first, maxrange)
		}
		if first != maxrange+leashBreakMargin {
			t.Errorf("type %d: first walk aims %d, want %d", typ, first, maxrange+leashBreakMargin)
		}
		if leashBreakFor(maxrange, 2) <= first || leashBreakFor(maxrange, 0) != first {
			t.Errorf("type %d: escalation 0/1/2 = %d/%d/%d", typ, leashBreakFor(maxrange, 0), first, leashBreakFor(maxrange, 2))
		}
		for attempt := 1; attempt <= 10; attempt++ {
			if d := leashBreakFor(maxrange, attempt); d > max(maxFleeDist, maxrange+leashBreakMargin) {
				t.Errorf("type %d attempt %d aims %d, past the flee ceiling %d", typ, attempt, d, maxFleeDist)
			}
		}
	}
	// The plant and the Shade, the two with the shortest leash: six tiles clear them.
	if leashBreakFor(3, 1) != 6 {
		t.Errorf("a leash of 3 aims %d, want 6", leashBreakFor(3, 1))
	}
}

// The leash table against the content that defines it. Skipped without RSB0T_CONTENT_DIR.
func TestEventLeashTableMatchesContent(t *testing.T) {
	dir := os.Getenv("RSB0T_CONTENT_DIR")
	if dir == "" {
		t.Skip("RSB0T_CONTENT_DIR not set")
	}
	npcs, err := os.ReadFile(filepath.Join(dir, "scripts", "macro events", "configs", "antimacro.npc"))
	if err != nil {
		t.Skipf("antimacro.npc not readable: %v", err)
	}
	pack, err := os.ReadFile(filepath.Join(dir, "pack", "npc.pack"))
	if err != nil {
		t.Skipf("npc.pack not readable: %v", err)
	}
	ids := map[string]int{} // debugname -> id
	for _, line := range strings.Split(strings.ReplaceAll(string(pack), "\r", ""), "\n") {
		if id, name, ok := strings.Cut(line, "="); ok {
			if n, err := strconv.Atoi(id); err == nil {
				ids[name] = n
			}
		}
	}
	maxrange := map[string]int{}
	cur := ""
	for _, line := range strings.Split(strings.ReplaceAll(string(npcs), "\r", ""), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			cur = line[1 : len(line)-1]
		case cur != "" && strings.HasPrefix(line, "maxrange="):
			if n, err := strconv.Atoi(strings.TrimPrefix(line, "maxrange=")); err == nil {
				maxrange[cur] = n
			}
		}
	}
	if len(maxrange) < 30 {
		t.Fatalf("read only %d maxranges from antimacro.npc", len(maxrange))
	}
	// Every hostile event monster in the table is a content NPC with exactly that maxrange.
	for typ, want := range eventLeashes {
		found := false
		for name, id := range ids {
			if id != typ {
				continue
			}
			found = true
			got, ok := maxrange[name]
			if !ok {
				t.Errorf("type %d (%s) has no maxrange in antimacro.npc", typ, name)
			} else if got != want {
				t.Errorf("type %d (%s): the table says %d, content says %d", typ, name, want, got)
			}
		}
		if !found {
			t.Errorf("type %d is not in npc.pack", typ)
		}
	}
	// And every content NPC that is not in the table has a reason: no leash worth walking out of (the
	// Drunken Dwarf's 10000), the passive Strange plant seed, or an NPC that is not hostile at all.
	for name, got := range maxrange {
		id, ok := ids[name]
		if !ok || isLeashedEvent(id) {
			continue
		}
		switch {
		case got >= 1000, name == "macro_triffidseed", name == "macro_geni":
		default:
			t.Errorf("%s (type %d, maxrange %d) is neither in the leash table nor explained", name, id, got)
		}
	}
}

func TestSwarmSpawnIsTheFirstTileSeen(t *testing.T) {
	s := &overlapSession{state: overlapState()}
	if x, z := noteLeashSpawn(s, 15040, 411, 3040, 9588, 0); x != 3040 || z != 9588 {
		t.Fatalf("first sighting = %d,%d", x, z)
	}
	// It follows the account; the spawn stays where it first stood.
	if x, z := noteLeashSpawn(s, 15040, 411, 3041, 9590, 0); x != 3040 || z != 9588 {
		t.Fatalf("the spawn moved with the Swarm: %d,%d", x, z)
	}
	// Another Swarm (a recycled index, another type, another level) is a new spawn.
	for _, c := range []struct{ index, typ, x, z, level int }{{15041, 411, 3100, 3100, 0}, {15040, 412, 3101, 3101, 0}, {15040, 411, 3102, 3102, 1}} {
		if x, z := noteLeashSpawn(s, c.index, c.typ, c.x, c.z, c.level); x != c.x || z != c.z {
			t.Fatalf("%+v kept the old spawn: %d,%d", c, x, z)
		}
	}
	// Stale memory is not trusted.
	leashSpawns.Store(s, leashSpawn{index: 7, npcType: 411, x: 1, z: 1, at: time.Now().Add(-2 * leashSpawnMemory)})
	if x, z := noteLeashSpawn(s, 7, 411, 50, 60, 0); x != 50 || z != 60 {
		t.Fatalf("a spawn remembered for longer than %s was reused: %d,%d", leashSpawnMemory, x, z)
	}
	// And it is per account.
	other := &overlapSession{state: overlapState()}
	if x, z := noteLeashSpawn(other, 15040, 411, 2000, 2000, 0); x != 2000 || z != 2000 {
		t.Fatalf("another account saw %d,%d", x, z)
	}
}

func TestThreateningSwarmIsOnlyOneThatHasTheAccount(t *testing.T) {
	self := func() int { return overlapState().Self() }()
	cases := []struct {
		name string
		spec facingNPC
		want bool
	}{
		{"targeting us", facingNPC{npcSpec{1, 411, 1, 0}, 32768 + self}, true},
		{"targeting someone else", facingNPC{npcSpec{1, 411, 1, 0}, 32768 + self + 1}, false},
		{"new, unassigned and beside us", facingNPC{npcSpec{1, 411, 1, 0}, -1}, true},
		{"unassigned and far (another account's event)", facingNPC{npcSpec{1, 411, 12, 0}, -1}, false},
		{"a Rock Golem is leashed too", facingNPC{npcSpec{1, 413, 1, 0}, 32768 + self}, true},
		{"an angry Strange plant", facingNPC{npcSpec{1, angryPlantType, 1, 0}, 32768 + self}, true},
		{"a Shade", facingNPC{npcSpec{1, 429, 1, 0}, 32768 + self}, true},
		{"a Drunken Dwarf has no leash", facingNPC{npcSpec{1, 956, 1, 0}, 32768 + self}, false},
		{"the passive seed attacks nobody", facingNPC{npcSpec{1, 407, 1, 0}, 32768 + self}, false},
	}
	for _, c := range cases {
		s := overlapState()
		addFacingNPCs(s, c.spec)
		_, ok := threateningLeashed(s, 3200, 3200)
		if ok != c.want {
			t.Errorf("%s: threateningLeashed = %v, want %v", c.name, ok, c.want)
		}
	}
}

func TestOtherAttackersAreThingsThatFightButNotEventMonsters(t *testing.T) {
	withResolver(t, fakeResolver{
		npcNames: map[int]string{125: "Ice warrior", 411: "Swarm", 413: "Rock Golem", 99: "Banker", 100: "Hobgoblin"},
		npcOps: map[int][5]string{
			125: {"", "Attack"}, 411: {"", "Attack"}, 413: {"", "Attack"}, 99: {"Talk-to", "Bank"}, 100: {"", "Attack"},
		},
	})
	s := overlapState()
	addNPCs(s,
		npcSpec{1, 125, 4, 0},  // an Ice warrior: counts
		npcSpec{2, 411, 1, 0},  // the Swarm being left: not "other"
		npcSpec{3, 413, -3, 2}, // another event monster: hunts its own owner
		npcSpec{4, 99, 2, 2},   // a banker: cannot attack
		npcSpec{5, 100, -6, -6},
	)
	got := otherAttackers(s, pathfind.NavPoint{X: 3200, Z: 3200})
	want := map[hazardSite]bool{{3204, 3200}: true, {3194, 3194}: true}
	if len(got) != len(want) {
		t.Fatalf("other attackers = %v, want the Ice warrior and the Hobgoblin", got)
	}
	for _, h := range got {
		if !want[h] {
			t.Errorf("unexpected attacker at %v", h)
		}
	}
	// Level 1 sees none of them.
	if n := len(otherAttackers(s, pathfind.NavPoint{X: 3200, Z: 3200, Level: 1})); n != 0 {
		t.Fatalf("%d attackers on the wrong level", n)
	}
}

func TestSwarmLeashAvoidKeepsAMarginButNeverFromWhereTheAccountStands(t *testing.T) {
	origin := pathfind.NavPoint{X: 100, Z: 100}
	// One attacker 7 tiles east: a margin of 8 is capped at 7, so the account's own tile and every
	// tile as far from the attacker as it is are allowed, and anything nearer is not.
	avoid := leashAvoid(origin, []hazardSite{{107, 100}}, 8, nil)
	for _, c := range []struct {
		x, z int
		want bool
	}{{100, 100, false}, {99, 105, false}, {101, 100, true}, {104, 96, true}, {100, 112, false}} {
		if got := avoid(c.x, c.z, 0); got != c.want {
			t.Errorf("avoid(%d,%d) = %v, want %v", c.x, c.z, got, c.want)
		}
	}
	// A narrower margin leaves more ground.
	if leashAvoid(origin, []hazardSite{{107, 100}}, 3, nil)(104, 100, 0) {
		t.Error("a margin of 3 refused a tile 3 from the attacker")
	}
	// No margin applies no hazard, but the window and the level still hold.
	none := leashAvoid(origin, []hazardSite{{101, 100}}, 0, nil)
	if none(101, 100, 0) || !none(100, 100, 1) || !none(100+fleeWindowRadius+1, 100, 0) {
		t.Error("margin 0 must still refuse other levels and the far side of the window, and nothing else")
	}
	// The caller's extra ground is refused too.
	extra := leashAvoid(origin, nil, 0, func(x, z, level int) bool { return x == 5 })
	if !extra(5, 100, 0) || extra(6, 100, 0) {
		t.Error("extra predicate not honoured")
	}
}
