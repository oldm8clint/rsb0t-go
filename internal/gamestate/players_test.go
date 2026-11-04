package gamestate

import (
	"strings"
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// bitField is one (width, value) pair for bitPack.
type bitField struct {
	n int
	v uint32
}

// bitPack MSB-first bit-packs fields into a totalBytes-long buffer — the write-side
// mirror of internal/packet.Packet.GBit's own read algorithm (there is no exported
// bit-level writer in that package, since PLAYER_INFO/NPC_INFO are server->client-only
// wire formats production code never emits). Same technique as
// internal/randomevents/state_helpers_test.go's own bitPack, reimplemented here since
// test helpers aren't exported across packages.
func bitPack(totalBytes int, fields ...bitField) []byte {
	buf := make([]byte, totalBytes)
	pos := 0
	for _, f := range fields {
		n, v := f.n, f.v
		for n > 0 {
			bytePos := pos >> 3
			bitOff := pos & 7
			free := 8 - bitOff
			take := n
			if take > free {
				take = free
			}
			shift := n - take
			chunk := (v >> uint(shift)) & ((1 << uint(take)) - 1)
			buf[bytePos] |= byte(chunk << uint(free-take))
			pos += take
			n -= take
		}
	}
	return buf
}

// testUserHash ports JString.ts's toUserhash — the write-side mirror of
// playerRawUsername, used here only to build synthetic APPEARANCE blobs with a known
// name, the same way a real server packs a username before sending it.
func testUserHash(name string) int64 {
	name = strings.TrimSpace(name)
	var l int64
	for i := 0; i < len(name) && i < 12; i++ {
		c := name[i]
		l *= 37
		switch {
		case c >= 'A' && c <= 'Z':
			l += int64(c) + 1 - 'A'
		case c >= 'a' && c <= 'z':
			l += int64(c) + 1 - 'a'
		case c >= '0' && c <= '9':
			l += int64(c) + 27 - '0'
		}
	}
	for l != 0 && l%37 == 0 {
		l /= 37
	}
	return l
}

// buildAppearanceBlob builds a synthetic APPEARANCE sub-block byte-for-byte matching
// ClientPlayer.ts's setAppearance field order: gender, headicons, the 12-part equipment
// loop (or the transmog early-exit case), 5 recolour bytes, 7 two-byte anim ids, the
// base-37-packed name, then combatLevel/skillLevel filler (never read by
// playerAppearanceName, included anyway so the blob matches a real one's shape).
func buildAppearanceBlob(name string, transmog bool) []byte {
	p := packet.NewWriter(64)
	p.P1(0) // gender
	p.P1(0) // headicons
	if transmog {
		p.P1(0xFF)
		p.P1(0xFF)
		p.P2(1234) // transmog npc type id
	} else {
		for i := 0; i < 12; i++ {
			p.P1(0)
		}
	}
	for i := 0; i < 5; i++ {
		p.P1(0) // recolours
	}
	for i := 0; i < 7; i++ {
		p.P2(0xFFFF) // anim ids, all "none"
	}
	p.P8(testUserHash(name))
	p.P1(3)  // combatLevel filler
	p.P2(50) // skillLevel filler
	return p.Data[:p.Pos]
}

func TestPlayerRawUsernameAndScreenNameRoundTrip(t *testing.T) {
	cases := []struct {
		raw    string
		screen string
	}{
		{"abc_def", "Abc Def"},
		{"main", "Main"},
		{"iron_ladle", "Iron Ladle"},
		{"a1b2_c3", "A1b2 C3"},
	}
	for _, c := range cases {
		hash := testUserHash(c.raw)
		gotRaw := playerRawUsername(hash)
		if gotRaw != c.raw {
			t.Errorf("playerRawUsername(hash(%q)) = %q, want %q", c.raw, gotRaw, c.raw)
		}
		gotScreen := playerScreenName(gotRaw)
		if gotScreen != c.screen {
			t.Errorf("playerScreenName(%q) = %q, want %q", gotRaw, gotScreen, c.screen)
		}
	}
}

func TestPlayerRawUsernameInvalid(t *testing.T) {
	if got := playerRawUsername(0); got != "invalid_name" {
		t.Errorf("playerRawUsername(0) = %q, want invalid_name", got)
	}
	if got := playerRawUsername(-1); got != "invalid_name" {
		t.Errorf("playerRawUsername(-1) = %q, want invalid_name", got)
	}
}

func TestPlayerAppearanceNameDecodesPlainAndTransmog(t *testing.T) {
	if got := playerAppearanceName(buildAppearanceBlob("foo", false)); got != "Foo" {
		t.Errorf("plain appearance name = %q, want Foo", got)
	}
	if got := playerAppearanceName(buildAppearanceBlob("bar_baz", true)); got != "Bar Baz" {
		t.Errorf("transmog appearance name = %q, want Bar Baz", got)
	}
}

func TestPlayerAppearanceNameHandlesShortBuffer(t *testing.T) {
	for _, n := range []int{0, 1, 2, 5, 10} {
		data := make([]byte, n)
		if got := playerAppearanceName(data); got != "" {
			t.Errorf("playerAppearanceName(%d zero bytes) = %q, want \"\"", n, got)
		}
	}
}

// TestPlayersNewVisDecodesAppearanceName exercises the full path: a brand-new player
// appears via NewVis with extendedInfo=1, its APPEARANCE mask carries a name, and
// Players() reports it at the correct absolute tile.
func TestPlayersNewVisDecodesAppearanceName(t *testing.T) {
	s := New()
	s.mapBuildBaseX = 3200
	s.mapBuildBaseZ = 3200

	bits := bitPack(6,
		bitField{1, 0},     // local: no update
		bitField{8, 0},     // OldVis: 0 previously-tracked players
		bitField{11, 5},    // NewVis: index 5
		bitField{5, 3},     // dx = 3
		bitField{5, 30},    // dz raw 30 -> -2
		bitField{1, 0},     // jump = 0
		bitField{1, 1},     // extendedInfo = 1
		bitField{11, 2047}, // NewVis terminator
	)
	blob := buildAppearanceBlob("abc_def", false)
	ext := append([]byte{0x01, byte(len(blob))}, blob...)
	payload := append(bits, ext...)

	s.Handle(serverprot.PLAYER_INFO, payload)

	players := s.Players()
	if len(players) != 1 {
		t.Fatalf("Players() = %+v, want 1 entry", players)
	}
	p := players[0]
	if p.Index != 5 || p.Name != "Abc Def" || p.X != 3203 || p.Z != 3198 || p.Level != 0 {
		t.Fatalf("unexpected player: %+v", p)
	}
}

// TestPlayersOldVisWalkPreservesNameAndMoves feeds a walk-op update (no extended data)
// for an already-tracked player and checks the position moves while the name — which
// only ever arrives via an APPEARANCE update — is untouched.
func TestPlayersOldVisWalkPreservesNameAndMoves(t *testing.T) {
	s := New()
	s.players[7] = &playerEntry{name: "Seed", localX: 10, localZ: 10}
	s.playerIDs = []int{7}

	bits := bitPack(4,
		bitField{1, 0},     // local: no update
		bitField{8, 1},     // OldVis: 1 previously-tracked player
		bitField{1, 1},     // has update
		bitField{2, 1},     // op 1: walk
		bitField{3, 0},     // dir 0 -> walkDX[0]=-1, walkDZ[0]=1
		bitField{1, 0},     // extendedInfo = 0
		bitField{11, 2047}, // NewVis terminator (no new entities)
	)

	s.Handle(serverprot.PLAYER_INFO, bits)

	players := s.Players()
	if len(players) != 1 {
		t.Fatalf("Players() = %+v, want 1 entry", players)
	}
	p := players[0]
	if p.Index != 7 || p.Name != "Seed" || p.X != 9 || p.Z != 11 {
		t.Fatalf("unexpected player after walk: %+v", p)
	}
}

// TestPlayersRemoval checks op 3 drops the tracked player entirely.
func TestPlayersRemoval(t *testing.T) {
	s := New()
	s.players[3] = &playerEntry{name: "Gone"}
	s.playerIDs = []int{3}

	bits := bitPack(3,
		bitField{1, 0},     // local: no update
		bitField{8, 1},     // OldVis: 1 previously-tracked player
		bitField{1, 1},     // has update
		bitField{2, 3},     // op 3: removal
		bitField{11, 2047}, // NewVis terminator
	)

	s.Handle(serverprot.PLAYER_INFO, bits)

	if players := s.Players(); len(players) != 0 {
		t.Fatalf("Players() after removal = %+v, want empty", players)
	}
	if len(s.playerIDs) != 0 {
		t.Fatalf("playerIDs after removal = %+v, want empty", s.playerIDs)
	}
}

// TestPlayerInfoOthersUsesUpdatedLocalTile proves applyPlayerInfoOthers positions a
// NewVis player relative to the local player's ALREADY-UPDATED tile this same tick
// (matching Client.ts's getPlayerPosLocal running before getPlayerPosOldVis/NewVis), not
// last tick's — the ordering the doc comment on applyPlayerInfoOthers depends on. Also
// confirms the local branch itself is unaffected by the others' decode that follows it.
func TestPlayerInfoOthersUsesUpdatedLocalTile(t *testing.T) {
	s := New()
	s.mapBuildBaseX = 800
	s.mapBuildBaseZ = 800

	bits := bitPack(7,
		bitField{1, 1},     // local: has update
		bitField{2, 1},     // op 1: walk
		bitField{3, 0},     // dir 0 -> walkDX[0]=-1, walkDZ[0]=1
		bitField{1, 0},     // local extendedInfo = 0
		bitField{8, 0},     // OldVis: 0 previously-tracked players
		bitField{11, 9},    // NewVis: index 9
		bitField{5, 2},     // dx = 2
		bitField{5, 1},     // dz = 1
		bitField{1, 0},     // jump = 0
		bitField{1, 0},     // extendedInfo = 0
		bitField{11, 2047}, // NewVis terminator
	)

	s.Handle(serverprot.PLAYER_INFO, bits)

	x, z, level, ok := s.Tile()
	if !ok || x != 799 || z != 801 || level != 0 {
		t.Fatalf("local tile = (%d,%d,%d) ok=%v, want (799,801,0) true", x, z, level, ok)
	}

	players := s.Players()
	if len(players) != 1 {
		t.Fatalf("Players() = %+v, want 1 entry", players)
	}
	p := players[0]
	// Relative to the ALREADY-UPDATED local tile (799,801): +2,+1 -> (801,802).
	if p.Index != 9 || p.X != 801 || p.Z != 802 {
		t.Fatalf("unexpected new player: %+v (local tile was 799,801)", p)
	}
}

// TestPlayersDoesNotMisattributeLocalPlayersOwnAppearance is a regression test for a bug
// reproduced live against the local engine (see COLLAB.md): when the LOCAL player's own
// extendedInfo bit is set, Client.ts's getPlayerPosLocal queues a LOCAL_PLAYER_INDEX
// sentinel into the SAME entityUpdateIds list getPlayerPosOldVis/NewVis append to, ahead
// of every other player's entry — so the local player's own appearance bytes come FIRST
// in the shared extended-data stream. Before localHasQueuedExtended accounted for this,
// applyPlayerInfoExtended assigned that leading (local) appearance block to whichever
// OTHER player happened to be first in this file's own updated list instead — observed
// live as account B's Players() view of account A showing B's OWN name on A's entry,
// with A's position still correct. This test reproduces the same shape synthetically: a
// local walk with extendedInfo=1, plus an already-tracked other player (index 4) whose
// OWN OldVis update also carries an APPEARANCE block.
func TestPlayersDoesNotMisattributeLocalPlayersOwnAppearance(t *testing.T) {
	s := New()
	s.players[4] = &playerEntry{name: "Stale", localX: 5, localZ: 5}
	s.playerIDs = []int{4}

	bits := bitPack(4,
		bitField{1, 1},     // local: has update
		bitField{2, 1},     // op 1: walk
		bitField{3, 1},     // dir 1 -> walkDX[1]=0, walkDZ[1]=1
		bitField{1, 1},     // local extendedInfo = 1 (queues local's own appearance FIRST)
		bitField{8, 1},     // OldVis: 1 previously-tracked player
		bitField{1, 1},     // has update
		bitField{2, 0},     // op 0: queues an update unconditionally, no dir bits
		bitField{11, 2047}, // NewVis terminator
	)

	localBlob := buildAppearanceBlob("local_self", false)
	otherBlob := buildAppearanceBlob("other_guy", false)
	ext := append([]byte{0x01, byte(len(localBlob))}, localBlob...)
	ext = append(ext, 0x01, byte(len(otherBlob)))
	ext = append(ext, otherBlob...)
	payload := append(bits, ext...)

	s.Handle(serverprot.PLAYER_INFO, payload)

	players := s.Players()
	if len(players) != 1 {
		t.Fatalf("Players() = %+v, want 1 entry", players)
	}
	p := players[0]
	if p.Index != 4 || p.Name != "Other Guy" {
		t.Fatalf("player 4's name = %q, want \"Other Guy\" (got the local player's own name instead: local branch's queued extended update must be skipped, not attributed to another entity) — full: %+v", p.Name, p)
	}
}

// findByIndex mirrors cmd/playertest's own helper of the same name (not exported across
// packages) — returns the Player whose Index matches, if present.
func findByIndex(players []Player, index int) (Player, bool) {
	for _, p := range players {
		if p.Index == index {
			return p, true
		}
	}
	return Player{}, false
}

// TestPlayersReSightingWithoutAppearanceUsesCachedName is a regression test for a real bug
// reproduced live against the local engine (see COLLAB.md, worktrees agent-afacfcbaec77bf555
// and agent-ada602387b8fc2de1): a genuinely NEW player entity for THIS index (wasNew==true,
// arriving via NewVis, not OldVis) does not necessarily carry an APPEARANCE sub-block.
// Confirmed with a raw hex capture: this engine only sends APPEARANCE the FIRST time it
// tells a connection about a given player index — a later re-sighting of that SAME index
// (the player left view, then came back — exactly the shape of a mule hand-off: both
// accounts are mutually visible once at login/spawn, teleport apart to their own posts, then
// come back into view at the meet spot) arrives with extendedInfo=1 but a mask that does NOT
// include APPEARANCE (observed live: 0x20, FACESQUARE only). This mirrors Client.ts's own
// design exactly: getPlayerPosNewVis checks a per-index playerAppearanceBuffer cache —
// populated only when APPEARANCE actually arrives, and NEVER cleared when the entity leaves
// view — and immediately applies it to a freshly-recreated entity without waiting for the
// server to resend it. Before this fix, players.go had no such cache: a player's
// playerEntry (and its .name) was discarded entirely on visibility loss, so a re-sighting
// without a fresh APPEARANCE block left .name == "" forever — exactly what
// donorrunner.findMule/findMuleSlot and mulerunner.ResolvePlayerSlot both match against to
// find a trade partner by name.
func TestPlayersReSightingWithoutAppearanceUsesCachedName(t *testing.T) {
	s := New()

	// Packet 1: index 6 appears via NewVis, WITH an APPEARANCE block — populates both
	// s.players[6].name and the persistent s.playerAppearanceNames[6] cache.
	bits1 := bitPack(6,
		bitField{1, 0},     // local: no update
		bitField{8, 0},     // OldVis: 0 previously-tracked players
		bitField{11, 6},    // NewVis: index 6
		bitField{5, 0},     // dx = 0
		bitField{5, 0},     // dz = 0
		bitField{1, 0},     // jump = 0
		bitField{1, 1},     // extendedInfo = 1
		bitField{11, 2047}, // NewVis terminator
	)
	blob := buildAppearanceBlob("mule_one", false)
	ext1 := append([]byte{0x01, byte(len(blob))}, blob...)
	s.Handle(serverprot.PLAYER_INFO, append(bits1, ext1...))

	if p, ok := findByIndex(s.Players(), 6); !ok || p.Name != "Mule One" {
		t.Fatalf("after first sighting: Players() = %+v, want index 6 named \"Mule One\"", s.Players())
	}

	// Packet 2: index 6 leaves view entirely (op 3, removal) — s.players[6] is deleted,
	// but the cache must survive this.
	bits2 := bitPack(3,
		bitField{1, 0},     // local: no update
		bitField{8, 1},     // OldVis: 1 previously-tracked player
		bitField{1, 1},     // has update
		bitField{2, 3},     // op 3: removal
		bitField{11, 2047}, // NewVis terminator
	)
	s.playerIDs = []int{6}
	s.Handle(serverprot.PLAYER_INFO, bits2)
	if players := s.Players(); len(players) != 0 {
		t.Fatalf("after removal: Players() = %+v, want empty", players)
	}

	// Packet 3: index 6 re-appears via NewVis, extendedInfo=1, but the mask this time is
	// FACESQUARE (0x20) only — NOT APPEARANCE — exactly what a real re-sighting looked
	// like live. Without the cache, name would stay "".
	bits3 := bitPack(6,
		bitField{1, 0},     // local: no update
		bitField{8, 0},     // OldVis: 0 previously-tracked players
		bitField{11, 6},    // NewVis: index 6 again
		bitField{5, 1},     // dx = 1
		bitField{5, 0},     // dz = 0
		bitField{1, 0},     // jump = 0
		bitField{1, 1},     // extendedInfo = 1
		bitField{11, 2047}, // NewVis terminator
	)
	// mask=0x20 (FACESQUARE), then 4 bytes of faceSquareX/Z to skip.
	ext3 := []byte{0x20, 0, 0, 0, 0}
	s.Handle(serverprot.PLAYER_INFO, append(bits3, ext3...))

	p, ok := findByIndex(s.Players(), 6)
	if !ok {
		t.Fatalf("after re-sighting: Players() = %+v, want index 6 present", s.Players())
	}
	if p.Name != "Mule One" {
		t.Fatalf("after re-sighting without a fresh APPEARANCE block: name = %q, want cached \"Mule One\"", p.Name)
	}
}
