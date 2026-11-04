package fleetrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// PlayerPack is the persistent inventory-tab component id, matching every sibling runner's own
// PlayerPack/playerPack constant.
const PlayerPack = 3214

// Delayer sleeps for d, returning false early if the caller should stop instead of true once d
// has fully elapsed — the same shape every sibling runner package's own Delayer uses.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration and never cancels — for a standalone tool.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one Stage 1 step needs from the outside world.
type Deps struct {
	Session    actions.Session
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	// Walker plans over internal/webwalk's door/gate/stair/ship-fare graph, and is what
	// freshstartrunner.WalkWithWalker needs to cross a level or a ship at all — a plain
	// pathfind.Pack.FindPath has no such edges and has stranded real accounts on Stage 2's own
	// Lumbridge->Ardougne route before (see freshstartrunner.walkWithWalker's own doc comment
	// for both live repros). Stage 1 never needed one: it never leaves the Lumbridge courtyard.
	// nil is safe — every walk falls back to plain same-level pathfinding, which is exactly
	// today's Stage 1 behaviour.
	Walker *webwalk.Walker
	// Log receives one already-formatted line per notable event; nil drops them.
	Log func(string)
	// Loot receives what each successful pickpocket really took (through freshstartrunner's
	// and thieverrunner's own pickpockets); nil records nothing. Set from bot.Context.Loot.
	Loot func(loot.Drop)
	// Kills remembers this account's recent kills, so the bones a kill leaves are recorded on
	// that kill (lootreport.go). Set from bot.Context.KillLog; nil gets a log private to this Deps.
	Kills *loot.KillLog
	// Me and World name this account on the swarm's shared boards (pens.go: the pen and combat
	// target boards), and TrainPen is the pen its training steps use -- the zero pen is the Cow
	// pen every stage used before pens existed ("scarabprod", 2026-09-23).
	Me       string
	World    int
	TrainPen fleetplan.TrainPen
	// MagicPens are the Magic levels each pen tier opens at (scarabplan.MagicPenLevels; zero
	// values are the defaults 20/30/40/50).
	MagicPens fleetplan.MagicPenLevels
	// SafeLoot is the safespot pens' loot rule (scarabplan.SafeLootSettings; the zero value loots
	// nothing, the bot sets the defaults).
	SafeLoot fleetplan.SafeLootSettings
	// TrainFloorHP is the HP at or below which TrainStep and TrainStepManual break a fight off and
	// walk out of the herd (scarabplan.TrainFloorHP); 0 keeps the old fight-to-the-end.
	TrainFloorHP int
	// KitKeepExtra names more items KitKeep keeps in the pack through a bundled ask's deposit:
	// the quest ladder's asks set it to everything the quest is carrying, because the deposit's
	// own keep list (coins, runes, staffs, food, rope) banked Glarial's amulet and urn in the
	// middle of Waterfall's dungeon kit on the live "acct" (2026-09-25), and the kit then found
	// the quest's own relics "short" and parked.
	KitKeepExtra []string
	// KitGive, when set, draws the bank hand-over that rides a bundled kit ask's own trade (a user request;
	// donorrunner/kitgive.go): RequestKitBundle calls it with the bank beside the mule's post open, after the
	// kit's deposit, and the stacks it returns (already in the pack) go on this side of the kit's trade screen.
	KitGive func(bank *actions.Bank) []donorrunner.GiveLine
	// NoBankFruit stops WithdrawAnyFoodAt drawing Strange fruit from the bank with the food (the
	// ownBankFirst=false hold-back, ownbank.go).
	NoBankFruit bool
	// NoCastClock turns the tick-aligned hand casts at a safespot off (safespotCastClock=false,
	// castclock.go): every cast waits scarabplan.CastGap of wall time after the last, as before 6 Oct 2026.
	NoCastClock bool
	// ClickRange is combatrunner.Deps.ClickRange for every fight this account starts (attackFirstRange; 0 walks
	// next to the target first, as before 7 Oct 2026).
	ClickRange int
	// NoAlchBetween stops a safespot account casting High Level Alchemy between its combat casts
	// (safespotAlchBetween=false): it alchs only while no target is castable, as before 6 Oct 2026.
	NoAlchBetween bool
	// CurseOpener opens a fresh target at a safespot pen with a Curse (stage3Curses, scarabplan/curse.go) and
	// CurseKills is the targets' worth of Curse runes PrepareTraining draws from the bank for it; off and 0 do neither.
	CurseOpener bool
	CurseKills  int
	// MagicGearSwap makes PrepareTraining take off armour with a negative magic attack bonus after
	// the kit is drawn (TakeOffMagicArmour); set from stage5MagicGearSwap.
	MagicGearSwap bool
	// WantMeleeRegear makes TrainerRearm draw the melee armour a Magic slice shed back out of the
	// bank and wear it (the checkpoint's MagicShed is non-empty).
	WantMeleeRegear bool
	// Travel is the account's walk alch and teleport travel (travel.go), used by WalkTo; nil
	// does neither.
	Travel *Travel
	// OnStun is thieverrunner.Deps.OnStun for every pickpocket this account makes (Thiev): the endgame loop
	// casts High Level Alchemy inside a stun's wait (scarabswarm_endgame.go). Nil waits as before.
	OnStun func(wait time.Duration)
	// Seek remembers the spawns this account has walked to look for a pickpocket rung's NPCs (seek.go); the bot
	// owns it so it outlives the Deps. Nil remembers nothing.
	Seek *PickSeek
}

func (d *Deps) Logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

// Fresh adapts d to freshstartrunner.Deps — the same field-for-field copy every sibling runner
// that calls into freshstartrunner already does (thieverrunner.fresh, crafterrunner.Fresh).
func Fresh(d *Deps) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: freshstartrunner.Delayer(d.Delay),
		Walker: d.Walker, Log: d.Log, Loot: d.Loot,
	}
}

func CurrentTile(d *Deps) *tile.Tile {
	x, z, level, ok := d.Session.State().Tile()
	if !ok {
		return nil
	}
	return &tile.Tile{X: x, Z: z, Level: level}
}

// emergencyFleeDriftTiles is 0 — Stage 1 has no single fixed "home" tile the way a distant
// skilling loop does; it always operates within a handful of tiles of the Lumbridge respawn.
const emergencyFleeDriftTiles = 0

var emergencyFleeHome pathfind.NavPoint

const guardianWaitLogEvery = 10 * time.Second

// WaitForGuardian pauses while a random event is present — mirrors every sibling runner's own
// WaitForGuardian exactly (thieverrunner.WaitForGuardian is the closest reference).
func WaitForGuardian(d *Deps) bool {
	randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
	if !randomevents.Busy(d.Session) {
		return true
	}
	d.Logf("[guardian] a random event is present; waiting")
	lastLog := time.Now()
	for randomevents.Busy(d.Session) {
		randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
		if !d.Delay(500 * time.Millisecond) {
			return false
		}
		if time.Since(lastLog) >= guardianWaitLogEvery {
			d.Logf("[guardian] still waiting on a random event")
			lastLog = time.Now()
		}
	}
	return true
}

// CoinsHeld is coins in the backpack right now.
func CoinsHeld(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if ok && strings.EqualFold(obj.Name, freshstart.Coins) {
			c := it.Count
			if c < 1 {
				c = 1
			}
			n += int(c)
		}
	}
	return n
}

func WaitFor(d *Deps, what string, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !d.Delay(150 * time.Millisecond) {
			return false
		}
	}
	d.Logf("timed out waiting for %s", what)
	return false
}
