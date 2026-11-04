package runecraftrunner

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/actions/npcsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/tile"
)

// THE LIVE ENVIRONMENT STEPS BESIDE THE NPC WITH THE ROUND'S WALK. Live, 13-14 Oct 2026, the Guild stretch walked to "within two tiles
// of Wizard Distentor's stand" -- the Guild's door tile -- and clicked him from there; the strict server (Engine-TS 289) takes a click
// on an NPC only from a tile that touches him. The environment hands the round's own door-aware walk to the teleport: it is called
// with the Guild stand first (radius 2) and then with the exact tile beside him (radius 0), and the plain walk packets stay unused.
// Needs the cache config (RSB0T_CONFIG_JAG).
func TestTheLiveEnvironmentStepsBesideDistentorWithTheRoundsWalk(t *testing.T) {
	cfgPath := os.Getenv("RSB0T_CONFIG_JAG")
	if cfgPath == "" {
		t.Skip("set RSB0T_CONFIG_JAG")
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	npcType, op := -1, 0
	for _, id := range cfg.NpcIDs(guild.NPC) {
		if n, ok := cfg.Npc(id); ok {
			if o := cachecfg.OpIndex(n.Ops, guild.Op); o != 0 {
				npcType, op = id, o
				break
			}
		}
	}
	if npcType < 0 {
		t.Fatalf("no npc %q with a %q option", guild.NPC, guild.Op)
	}
	pack, err := npcsim.OpenPack([2]int{40, 48})
	if err != nil {
		t.Fatal(err)
	}
	sim := actionsim.New(actionsim.Objs{}, nil)
	chat := chatlog.New()
	e := npcsim.New(sim, chat, pack, time.Millisecond)
	e.Stand(tile.Tile{X: 2640, Z: 3100}) // far from the Guild: the round's walk brings the account in
	mine := tile.Tile{X: 2891, Z: 4830}
	e.OnOp = func(c npcsim.Click) {
		if c.Option == op {
			e.Delay(4)
			e.Teleport(5, mine)
		}
	}
	setMagic := func(level int) {
		xp := 0
		sim.State().Handle(serverprot.UPDATE_STAT, []byte{byte(fleetplan.MagicStat), byte(xp >> 24), byte(xp >> 16), byte(xp >> 8), byte(xp), byte(level)})
	}
	setMagic(70) // the Guild's door lets the account in

	var lines []string
	d := &fleetrunner.Deps{
		Session: sim, Pack: pack, Config: cfg, Chat: chat, Components: actionsim.Components(),
		Delay: func(time.Duration) bool { e.Step(); return true },
		Log:   func(s string) { lines = append(lines, s) },
	}
	type call struct {
		dest   tile.Tile
		radius int
	}
	var calls []call
	door, npcAt := tile.Tile{X: 2596, Z: 3088}, tile.Tile{X: 2592, Z: 3091}
	walk := func(dest tile.Tile, radius int, timeout time.Duration) bool {
		calls = append(calls, call{dest, radius})
		if radius == 2 && dest == guild.Stand {
			e.Stand(door) // within two tiles of his stand, and nowhere near beside him
			e.Spawn(211, npcType, npcAt)
			return true
		}
		e.Stand(dest)
		return true
	}
	env := NewLiveEnv(d, LiveOptions{Gateway: guild, Walk: walk})
	res := env.TeleportIn()
	if !res.Landed || res.At != mine {
		t.Fatalf("%+v\nlog:\n%s", res, strings.Join(lines, "\n"))
	}
	if len(calls) != 2 || calls[0].radius != 2 || calls[0].dest != guild.Stand || calls[1].radius != 0 {
		t.Fatalf("walk calls %+v, want the Guild stand within 2 and then one exact tile", calls)
	}
	var taken []npcsim.Click
	for _, c := range e.Clicks() {
		if c.Reached {
			taken = append(taken, c)
		}
	}
	if len(taken) != 1 || taken[0].From != calls[1].dest {
		t.Errorf("clicks %+v, want one taken, from %v", e.Clicks(), calls[1].dest)
	}
	if len(e.Walks()) != 0 {
		t.Errorf("the plain walk was used: %v", e.Walks())
	}
	// And below Magic 66 nothing is walked or clicked.
	setMagic(65)
	before, clicks := len(calls), len(e.Clicks())
	if r := env.TeleportIn(); r.Landed || r.Clicks != 0 || !strings.Contains(r.Reason, "Magic 66") || len(calls) != before || len(e.Clicks()) != clicks {
		t.Errorf("Magic 65: %+v, %d new walks", r, len(calls)-before)
	}
}
