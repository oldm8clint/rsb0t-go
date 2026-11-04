package freshstartrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/tile"
)

// A STOPPING BOT DOES NOT HUNT FOR DOORS (live 14 Oct 2026, build d5362bb2). A stop request or a dropped connection makes
// every Delay answer false, so a walk fails at its first poll and the unreachable-route recovery in walkNearCore had
// nothing left to do but fail: it ran its six passes anyway, each pass a failed search flooding the 110,000-tile
// component plus a door hunt at the nearest closed "Door" (2815,3449, a house 12 tiles from the Catherby bank, on no
// route there), then the STRANDED handler, then WalkToUse went on to the next stand tile and the next booth. One account's
// connection dropped at 04:17:13 and its round unwound at 04:18:36, 83 seconds and 89 "no path" lines later;
// at the fleet stop (04:45:07) two more accounts did the same on their last seconds.

// placeOn puts the account on a tile, the way the server's rebuild and player-info packets do.
func placeOn(s *gamestate.State, x, z, level int) {
	w := packet.NewWriter(8)
	w.P2(uint16(x/8 + 6))
	w.P2(uint16(z/8 + 6))
	s.Handle(serverprot.REBUILD_NORMAL, w.Data[:w.Pos])
	s.Handle(serverprot.PLAYER_INFO, stopBitPack(3,
		[2]uint32{1, 1}, [2]uint32{2, 3}, [2]uint32{2, uint32(level)},
		[2]uint32{7, uint32(x % 8)}, [2]uint32{7, uint32(z % 8)}, [2]uint32{1, 0}, [2]uint32{1, 0}))
}

// stopBitPack MSB-first bit-packs (width, value) fields into totalBytes bytes, the write side of packet.GBit.
func stopBitPack(totalBytes int, fields ...[2]uint32) []byte {
	buf := make([]byte, totalBytes)
	pos := 0
	for _, f := range fields {
		n, v := int(f[0]), f[1]
		for n > 0 {
			bytePos, bitOff := pos>>3, pos&7
			free := 8 - bitOff
			take := min(n, free)
			chunk := (v >> uint(n-take)) & ((1 << uint(take)) - 1)
			buf[bytePos] |= byte(chunk << uint(free-take))
			pos += take
			n -= take
		}
	}
	return buf
}

func countLines(lines []string, sub string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

// stoppedBankWalker is a Deps standing on the Catherby street at 2810,3437 whose bot is stopping.
func stoppedBankWalker(t *testing.T) (*Deps, *[]string) {
	t.Helper()
	pk := realRoutePack(t)
	ix, cfg := realLocs(t)
	s := newCastSession()
	placeOn(s.state, 2810, 3437, 0)
	var lines []string
	d := &Deps{
		Session: s, Pack: pk, Locs: ix, Config: cfg,
		Delay: func(time.Duration) bool { return false },
		Log:   func(l string) { lines = append(lines, l) },
	}
	return d, &lines
}

// Behind the bank counter (2810,3443, the north stand of the booth at 2810,3442) has no route from the street. For a
// stopping bot the walk says so once and returns: no door hunt, no six passes, no STRANDED.
func TestAStoppingBotGivesUpAnUnreachableWalkAtOnce(t *testing.T) {
	d, lines := stoppedBankWalker(t)
	began := time.Now()
	if WalkNear(d, tile.Tile{X: 2810, Z: 3443}, 0, time.Minute) {
		t.Fatal("a tile behind the counter was reached")
	}
	if got := countLines(*lines, "the bot is stopping"); got != 1 {
		t.Errorf("%d stop lines, want 1: %v", got, *lines)
	}
	for _, bad := range []string{"no path from", "opening \"Door\"", "STRANDED", "could not reach any of its 4 approach tiles", "pocket escape"} {
		if countLines(*lines, bad) != 0 {
			t.Errorf("a stopping bot wrote %q: %v", bad, *lines)
		}
	}
	if time.Since(began) > 5*time.Second {
		t.Errorf("took %s", time.Since(began))
	}
}

// WalkToUse tries the booth's stand tiles nearest first. The nearest (2810,3441, straight below the booth) fails for a
// stopping bot like every walk, and the far-side stand behind the counter is then never tried.
func TestWalkToUseTriesNoFurtherStandWhenStopping(t *testing.T) {
	d, lines := stoppedBankWalker(t)
	if WalkToUse(d, tile.Tile{X: 2810, Z: 3442}, time.Minute) {
		t.Fatal("a stopping bot reached the booth")
	}
	if got := countLines(*lines, "2810,3443"); got != 0 {
		t.Errorf("the far side of the counter was tried %d time(s): %v", got, *lines)
	}
	if got := countLines(*lines, "FindPath 2810,3437,0 -> 2810,3441,0"); got != 1 {
		t.Errorf("the south stand was walked %d time(s), want once: %v", got, *lines)
	}
}
