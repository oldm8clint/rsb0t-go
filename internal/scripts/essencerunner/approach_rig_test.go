package essencerunner

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/actions/npcsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/tile"
)

// The rig the approach tests share: Deps over the STRICT server (npcsim), stepped by hand. Every d.Delay is one engine tick, so a
// test is deterministic and takes no wall time, and a teleport lands five ticks after an accepted click, as the content script's
// p_delay(4) does. The tests need the cache config (the NPC's name and options): RSB0T_CONFIG_JAG.

const rigNpcIndex = 211

var (
	// guildDoor is the tile inside the Guild's east double door, where the logged failures stood.
	guildDoor = tile.Tile{X: 2596, Z: 3088}
	// mineLand is a tile of the essence mine, where a teleport lands.
	mineLand = tile.Tile{X: 2891, Z: 4830}
)

func gateway(t testing.TB, key string) essenceplan.Gateway {
	t.Helper()
	gw, ok := essenceplan.GatewayByKey(key)
	if !ok {
		t.Fatalf("no %q gateway", key)
	}
	return gw
}

var (
	configOnce sync.Once
	config     *cachecfg.Config
	configErr  error
)

// loadConfig is the cache config (parsed once for the whole test binary).
func loadConfig(t testing.TB) *cachecfg.Config {
	t.Helper()
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("set RSB0T_CONFIG_JAG")
	}
	configOnce.Do(func() { config, configErr = cachecfg.LoadFile(path) })
	if configErr != nil {
		t.Fatal(configErr)
	}
	return config
}

// npcTypeOf is the cache id of the gateway's NPC and the option number of his teleport.
func npcTypeOf(t testing.TB, cfg *cachecfg.Config, gw essenceplan.Gateway) (typ, op int) {
	t.Helper()
	for _, id := range cfg.NpcIDs(gw.NPC) {
		if n, ok := cfg.Npc(id); ok {
			if o := cachecfg.OpIndex(n.Ops, gw.Op); o != 0 {
				return id, o
			}
		}
	}
	t.Fatalf("no npc %q with a %q option in the config", gw.NPC, gw.Op)
	return 0, 0
}

type rig struct {
	t    testing.TB
	e    *npcsim.Engine
	sim  *actionsim.Session
	chat *chatlog.Log
	d    *Deps
	gw   essenceplan.Gateway
	typ  int
	op   int
	logs []string
}

// squareOf is the mapsquare of a tile.
func squareOf(at tile.Tile) [2]int { return [2]int{at.X >> 6, at.Z >> 6} }

// newRig puts the account on from and the gateway's NPC on npcAt over pack (open ground over the gateway's mapsquare when nil).
func newRig(t testing.TB, key string, pack *pathfind.Pack, from, npcAt tile.Tile) *rig {
	t.Helper()
	cfg := loadConfig(t)
	gw := gateway(t, key)
	typ, op := npcTypeOf(t, cfg, gw)
	if pack == nil {
		var err error
		if pack, err = npcsim.OpenPack(squareOf(gw.Stand)); err != nil {
			t.Fatal(err)
		}
	}
	r := &rig{t: t, gw: gw, typ: typ, op: op}
	r.sim = actionsim.New(actionsim.Objs{}, nil)
	r.chat = chatlog.New()
	r.e = npcsim.New(r.sim, r.chat, pack, time.Millisecond)
	r.e.Stand(from)
	r.e.Spawn(rigNpcIndex, typ, npcAt)
	r.e.OnOp = func(c npcsim.Click) {
		if c.Option == op {
			r.e.Delay(4)
			r.e.Teleport(5, mineLand)
		}
	}
	r.d = &Deps{
		Session: r.sim, Pack: pack, Config: cfg, Chat: r.chat,
		Delay: func(time.Duration) bool { r.e.Step(); return true },
		Log:   func(s string) { r.logs = append(r.logs, s) },
	}
	return r
}

func (r *rig) log() string { return strings.Join(r.logs, "\n") }

func touches(a, b tile.Tile) bool {
	dx, dz := a.X-b.X, a.Z-b.Z
	if dx < 0 {
		dx = -dx
	}
	if dz < 0 {
		dz = -dz
	}
	return dx+dz == 1
}

// onNpcClick calls fn, with the click's number, whenever the account SENDS an NPC option -- before the engine's next tick reads it,
// which is how a test moves an NPC "while the packet was in flight".
func (r *rig) onNpcClick(fn func(n int)) {
	orig := r.sim.OnSend
	n := 0
	r.sim.OnSend = func(b []byte) {
		if len(b) == 3 {
			switch int(b[0]) {
			case clientprot.OPNPC1, clientprot.OPNPC2, clientprot.OPNPC3, clientprot.OPNPC4, clientprot.OPNPC5:
				n++
				fn(n)
			}
		}
		orig(b)
	}
}

// onWalk calls fn, with the walk's number, whenever the account sends a walk.
func (r *rig) onWalk(fn func(n int)) {
	orig := r.sim.OnSend
	n := 0
	r.sim.OnSend = func(b []byte) {
		if len(b) > 0 && int(b[0]) == clientprot.MOVE_GAMECLICK {
			n++
			fn(n)
		}
		orig(b)
	}
}
