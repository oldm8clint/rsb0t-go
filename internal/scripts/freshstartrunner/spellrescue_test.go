package freshstartrunner

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/tile"
)

// castSession counts every packet a call writes, so a test can see whether a cast was sent.
type castSession struct {
	state *gamestate.State
	sent  int
}

func newCastSession() *castSession { return &castSession{state: gamestate.New()} }

func (s *castSession) Send(func(*packet.Packet)) error { s.sent++; return nil }
func (s *castSession) State() *gamestate.State         { return s.state }
func (s *castSession) Cheats() *anticheat.Counters     { return &anticheat.Counters{} }

func (s *castSession) setStat(stat, level int) {
	w := packet.NewWriter(8)
	w.P1(byte(stat))
	w.P4(int32(1000))
	w.P1(byte(level))
	s.state.Handle(serverprot.UPDATE_STAT, w.Data[:w.Pos])
}

// setInv mirrors UPDATE_INV_FULL for one component: each item is {objID, count}.
func (s *castSession) setInv(comID int, items ...[2]int) {
	w := packet.NewWriter(64)
	w.P2(uint16(comID))
	w.P2(uint16(len(items)))
	for _, it := range items {
		w.P2(uint16(it[0] + 1))
		if it[1] >= 255 {
			w.P1(255)
			w.P4(int32(it[1]))
		} else {
			w.P1(byte(it[1]))
		}
	}
	s.state.Handle(serverprot.UPDATE_INV_FULL, w.Data[:w.Pos])
}

// rescueDeps is a Deps whose Delay stops at once, so a cast that is sent returns without its
// ten-second landing wait, and whose log lines are kept.
func rescueDeps(s *castSession, cfg *cachecfg.Config, lines *[]string) *Deps {
	return &Deps{Session: s, Config: cfg, Delay: func(time.Duration) bool { return false },
		Log: func(l string) { *lines = append(*lines, l) }}
}

// Live 2026-09-25: acct57 (Magic 26), Acct225 (Magic 22) and two more cast Lumbridge Teleport 9 to
// 40 times each from the sheep pen north of East Ardougne, each a ten-second wait for a cast the
// server had refused. Below Magic 31 nothing may be sent at all, and the reason is one line.
func TestSpellRescueDoesNotCastBelowMagic31(t *testing.T) {
	s := newCastSession()
	s.setStat(magicStat, 22)
	var lines []string
	start := time.Now()
	landed, cast := attemptSpellRescue(rescueDeps(s, nil, &lines), tile.Tile{X: 2650, Z: 3374})
	if landed || cast {
		t.Fatalf("landed=%v cast=%v at Magic 22, want neither", landed, cast)
	}
	if s.sent != 0 {
		t.Fatalf("sent %d packet(s) at Magic 22: the cast went out anyway", s.sent)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("took %v: the refusal waited for a landing", time.Since(start))
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "not casting Lumbridge Teleport") || !strings.Contains(lines[0], "Magic 22") {
		t.Fatalf("want one line giving the Magic level as the reason, got %q", lines)
	}
}

// Before the server has reported Magic at all the level reads 0: that is not a level to cast on.
func TestSpellRescueDoesNotCastOnAnUnreportedMagicLevel(t *testing.T) {
	s := newCastSession()
	var lines []string
	if _, cast := attemptSpellRescue(rescueDeps(s, nil, &lines), tile.Tile{}); cast || s.sent != 0 {
		t.Fatalf("cast=%v, sent %d with no Magic level reported", cast, s.sent)
	}
}

func TestLumbridgeTeleportRefusal(t *testing.T) {
	full := map[string]int{"Law rune": 1, "Earth rune": 1, "Air rune": 3}
	for _, tc := range []struct {
		name  string
		magic int
		known bool
		pack  map[string]int
		worn  []string
		ok    bool
		want  string
	}{
		{"castable", 31, true, full, nil, true, ""},
		{"level unknown", 99, false, full, nil, false, "not been reported"},
		{"Magic 30", 30, true, full, nil, false, "Magic 30"},
		{"no law rune", 40, true, map[string]int{"Earth rune": 5, "Air rune": 9}, nil, false, "Law rune"},
		{"two air, no staff", 40, true, map[string]int{"Law rune": 1, "Earth rune": 1, "Air rune": 2}, nil, false, "Air runes"},
		{"no air, Staff of air", 40, true, map[string]int{"Law rune": 1, "Earth rune": 1}, []string{"Staff of air"}, true, ""},
		{"no air, a staff that is not air", 40, true, map[string]int{"Law rune": 1, "Earth rune": 1}, []string{"Staff of fire"}, false, "Air runes"},
		{"no earth, Mystic lava staff", 40, true, map[string]int{"Law rune": 1, "Air rune": 3}, []string{"Mystic lava staff"}, true, ""},
		{"no earth, no staff", 40, true, map[string]int{"Law rune": 1, "Air rune": 3}, nil, false, "Earth rune"},
		{"names in any case", 31, true, map[string]int{"law rune": 1, "EARTH RUNE": 1, "air rune": 3}, nil, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			why := lumbridgeTeleportRefusal(tc.magic, tc.known, tc.pack, tc.worn)
			if (why == "") != tc.ok || !strings.Contains(why, tc.want) {
				t.Fatalf("refusal %q, want castable=%v mentioning %q", why, tc.ok, tc.want)
			}
		})
	}
}

// With the real item config: runes are read off the pack and the wielded staff by name, and a
// castable account really does cast. Skips without RSB0T_CONFIG_JAG.
func TestSpellRescueReadsRunesAndStaffWithTheRealConfig(t *testing.T) {
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// content/pack/obj.pack
	const airRune, earthRune, lawRune, staffOfAir = 556, 557, 563, 1381
	for _, tc := range []struct {
		name string
		pack [][2]int
		worn [][2]int
		cast bool
	}{
		{"law, earth and three air", [][2]int{{lawRune, 1}, {earthRune, 1}, {airRune, 3}}, nil, true},
		{"no law rune", [][2]int{{earthRune, 10}, {airRune, 30}}, nil, false},
		{"law and earth, Staff of air wielded", [][2]int{{lawRune, 1}, {earthRune, 1}}, [][2]int{{staffOfAir, 1}}, true},
		{"law and earth, no staff", [][2]int{{lawRune, 1}, {earthRune, 1}}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newCastSession()
			s.setStat(magicStat, 40)
			s.setInv(actions.PlayerPack, tc.pack...)
			s.setInv(actions.PlayerWorn, tc.worn...)
			var lines []string
			_, cast := attemptSpellRescue(rescueDeps(s, cfg, &lines), tile.Tile{})
			if cast != tc.cast || (s.sent > 0) != tc.cast {
				t.Fatalf("cast=%v sent=%d, want cast=%v; log %q", cast, s.sent, tc.cast, lines)
			}
		})
	}
}
