package runecraftrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/essencerunner"
	"rsb0t-go/internal/scripts/fleetrunner"
)

// THE MINING STRETCH. The main account's essence, when his bank and the mule's stock hold none, is his own: through a
// gateway into the Rune essence mine (the Wizards' Guild's Wizard Distentor at Magic 66, best because the
// Yanille bank is about 20 steps from the mine's exit), 27 essence a load, the portal out, the bank, and
// again, until the bank holds the stretch's goal. The loop is essenceplan.DecideStage (the miner's own,
// ported from EssenceMiner.ts) over an environment, so the whole loop is tested against a fake one.
//
// The legs have never been seen to succeed in the live fleet: in the log window of 13 Oct 2026 every
// Priest in Peril essence attempt ended in 39 "timed out waiting for the essence mine after teleporting" or
// 25 "Aubury refuses ... Rune Mysteries is not complete". So this stretch fails FAST on the refusal
// (essencerunner.TeleportInWith), logs every teleport's landing tile and tick count, every load's time and
// every bank's total, and bounds its failures.

// MineEnv is the world the stretch loop runs in.
type MineEnv interface {
	// InMine, PackFull, Haul and PickHeld are essenceplan.MinerSnapshot's fields.
	InMine() bool
	PackFull() bool
	Haul() int
	PickHeld() bool
	// PickName is the pickaxe held, for the log.
	PickName() string
	// Mine mines until the pack is full (or the rock fails); PortalOut leaves the mine.
	Mine() essencerunner.MineResult
	PortalOut() bool
	// BankDeposit walks to the gateway's bank, banks everything but the pickaxe and reads the bank: how many
	// essence it holds now, and how many were banked.
	BankDeposit() (bankEssence, banked int, ok bool)
	// FetchPickaxe draws the best usable pickaxe from the gateway's bank.
	FetchPickaxe() (name string, ok bool)
	// TeleportIn walks to the gateway's NPC and teleports.
	TeleportIn() essencerunner.TeleportResult
	// Tick is the server tick count, Stopped whether the bot has been told to stop.
	Tick() int
	Stopped() bool
}

// StretchOptions tunes RunStretch.
type StretchOptions struct {
	// Goal is the essence to bring into the bank this stretch.
	Goal int
	// Gateway names the way in (for the log); MaxFails bounds failures in a row; Budget bounds the stretch.
	Gateway  essenceplan.Gateway
	MaxFails int
	Budget   time.Duration
	// Hold, when set, is asked before every step: false ends the stretch (the round's time is used up).
	Hold func() bool
	// Logf receives the stretch's lines.
	Logf func(format string, args ...any)
	// Now is the clock (time.Now when nil).
	Now func() time.Time
}

// StretchResult is what a stretch did.
type StretchResult struct {
	// Banked is the essence added to the bank this stretch; BankEssence the bank's total after the last
	// deposit (-1 when it never deposited).
	Banked, BankEssence int
	// Loads is the full packs mined and banked; Teleports the ones that landed, Fails the failures counted.
	Loads, Teleports, Fails int
	// Refused is set when the gateway refused the teleport (Rune Mysteries): nothing more can be done here.
	Refused bool
	// Done is set when the goal was reached.
	Done bool
	// InMine is set when the stretch ended with the account still INSIDE the mine because the portal would not take
	// it out (after LeaveTries more tries): the caller must not hand the account on from there.
	InMine bool
	// Reason is why the stretch ended.
	Reason string
	// Took is the stretch's wall time.
	Took time.Duration
}

// LeaveTries is how many times a stretch that is ending takes the portal out of the mine before it reports the
// account still inside.
const LeaveTries = 3

// loadSize is the essence a full pack held, for the stretch's log line: the live environment reads the pack (25 with the knife and
// the log of the batch in it, 27 without), any other environment is a plain load of essenceplan.LoadSize.
func loadSize(env MineEnv) int {
	if h, ok := env.(interface{ EssenceHeld() int }); ok {
		if n := h.EssenceHeld(); n > 0 {
			return n
		}
	}
	return essenceplan.LoadSize
}

// LeaveMine takes the portal out of the mine, up to tries times, and reports whether the account is outside. The
// live portal waits for the departure itself (essencerunner.PortalOut: up to 15 s), so the tries are spaced by it.
// Already outside, or a stop: nothing is clicked.
func LeaveMine(env MineEnv, tries int) bool {
	for i := 0; i < tries && env.InMine() && !env.Stopped(); i++ {
		env.PortalOut()
	}
	return !env.InMine()
}

func (o StretchOptions) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

func (o StretchOptions) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// RunStretch mines and banks until opt.Goal essence has been added to the bank, the gateway refuses, the
// failures in a row reach opt.MaxFails, the budget or the round's hold runs out, or the bot is stopped.
//
// A stretch NEVER ends inside the mine (a stop apart, when nothing more can be done): whatever ends it -- the
// round's clock, the budget, the failures in a row, a stage nobody handles -- the portal is taken out first
// (LeaveMine), because the account is handed on to the caller and from there to ScarabSwarm, and the mine has
// no bank and nothing for it to do. Those conditions are checked at the top of every step, including while the
// account is inside, and used to return right there. Only a portal that fails LeaveTries times in a row leaves
// the account inside, and then StretchResult.InMine says so.
func RunStretch(env MineEnv, opt StretchOptions) (res StretchResult) {
	if opt.MaxFails < 1 {
		opt.MaxFails = 4
	}
	started := opt.now()
	res.BankEssence = -1
	defer func() { res.Took = opt.now().Sub(started) }()
	// end finishes the stretch for reason: out of the mine first.
	end := func(reason string, done bool) StretchResult {
		res.Done, res.Reason = done, reason
		if !env.InMine() {
			return res
		}
		opt.logf("essencerunner: %s: the stretch is over (%s) with the account inside the mine - leaving by the portal first", opt.Gateway.Key, reason)
		if LeaveMine(env, LeaveTries) {
			opt.logf("essencerunner: %s: out of the mine", opt.Gateway.Key)
			return res
		}
		res.InMine = true
		res.Reason += fmt.Sprintf("; STILL INSIDE the mine after %d more portal tries", LeaveTries)
		opt.logf("WARNING: essencerunner: %s: still inside the mine after %d portal tries on top of the failures", opt.Gateway.Key, LeaveTries)
		return res
	}
	fails := 0
	var loadTick int
	for {
		switch {
		case env.Stopped():
			res.Reason = "stopped" // nothing can be clicked once stopped: a restart resumes from wherever the account stands
			return res
		case res.Banked >= opt.Goal:
			return end(fmt.Sprintf("goal reached: %d essence banked (%d in the bank)", res.Banked, res.BankEssence), true)
		case opt.Budget > 0 && opt.now().Sub(started) > opt.Budget:
			return end("the stretch's time budget "+opt.Budget.String()+" is used up", false)
		case opt.Hold != nil && !opt.Hold():
			return end("the round's time is used up", false)
		case fails >= opt.MaxFails:
			return end(fmt.Sprintf("%d failures in a row", fails), false)
		}
		switch stage := essenceplan.DecideStage(essenceplan.MinerSnapshot{InMine: env.InMine(), PackFull: env.PackFull(), Haul: env.Haul(), PickHeld: env.PickHeld()}); stage {
		case essenceplan.StageMine:
			if loadTick == 0 {
				loadTick = env.Tick()
			}
			m := env.Mine()
			switch m.Reason {
			case "no-rock":
				fails++
				res.Fails++
				opt.logf("essencerunner: %s: no essence rock in sight inside the mine (%d in a row)", opt.Gateway.Key, fails)
			case "full":
				opt.logf("essencerunner: %s: %d essence in %d ticks (%s)", opt.Gateway.Key, loadSize(env), env.Tick()-loadTick, env.PickName())
				loadTick = 0
			case "pickaxe-head":
				// The lost-pickaxe event: the next Mine call puts the head back on, or, when it is gone, the failures end the stretch
				// and the bank read finds no pickaxe (and the supply loop asks the mule or buys one).
				fails++
				res.Fails++
				opt.logf("essencerunner: %s: the lost-pickaxe event took the pickaxe (%d in a row)", opt.Gateway.Key, fails)
			case "stalled":
				// A rock that gives nothing for the whole stall wait is a failure like the others, so the stretch ends
				// (and leaves by the portal) instead of clicking it again for ever: live 15 Oct 2026 a miner stood 17
				// minutes beside a rock the engine would not let it mine from, with not one log line.
				if m.Gained == 0 {
					fails++
					res.Fails++
					opt.logf("essencerunner: %s: the rock gave nothing for %s (%d in a row)", opt.Gateway.Key, essenceplan.MineStallTimeout, fails)
				}
			}
		case essenceplan.StagePortal:
			if !env.PortalOut() {
				fails++
				res.Fails++
				opt.logf("essencerunner: %s: the portal did not take us out (%d in a row)", opt.Gateway.Key, fails)
			}
		case essenceplan.StageBank:
			bankEssence, banked, ok := env.BankDeposit()
			if !ok {
				fails++
				res.Fails++
				opt.logf("essencerunner: %s: could not bank at %s (%d in a row)", opt.Gateway.Key, opt.Gateway.BankName, fails)
				break
			}
			fails = 0
			res.Banked += banked
			res.BankEssence = bankEssence
			if banked > 0 {
				res.Loads++
				opt.logf("essencerunner: %s: banked %d at %s; the bank holds %d; stretch %d of %d", opt.Gateway.Key, banked, opt.Gateway.BankName, bankEssence, res.Banked, opt.Goal)
			}
		case essenceplan.StagePickaxe:
			name, ok := env.FetchPickaxe()
			if !ok {
				fails++
				res.Fails++
				opt.logf("essencerunner: %s: no usable pickaxe in the pack, the worn items or the %s bank (%d in a row)", opt.Gateway.Key, opt.Gateway.BankName, fails)
				break
			}
			opt.logf("essencerunner: %s: drew a %s from the %s bank", opt.Gateway.Key, name, opt.Gateway.BankName)
		case essenceplan.StageTeleport:
			tp := env.TeleportIn()
			switch {
			case tp.Landed:
				fails = 0
				res.Teleports++
				opt.logf("essencerunner: %s: %s Teleport ok, landed (%d,%d) after %d ticks", opt.Gateway.Key, strings.TrimPrefix(opt.Gateway.NPC, "Wizard "), tp.At.X, tp.At.Z, tp.Ticks)
			case tp.QuestRefused:
				res.Refused, res.Fails = true, res.Fails+1
				res.Reason = opt.Gateway.NPC + " refused: " + tp.Reason
				opt.logf("essencerunner: %s: %s refused (%s) - failing at once, not waiting for the teleport", opt.Gateway.Key, strings.TrimPrefix(opt.Gateway.NPC, "Wizard "), tp.Reason)
				return res
			default:
				fails++
				res.Fails++
				opt.logf("essencerunner: %s: the teleport did not land (%s) (%d in a row)", opt.Gateway.Key, tp.Reason, fails)
			}
		default:
			return end("no stage for "+string(stage), false)
		}
	}
}

// ---- the live environment ----

// LiveOptions are what the live environment needs from the round.
type LiveOptions struct {
	// Gateway is the way into the mine.
	Gateway essenceplan.Gateway
	// Walk is the door-aware webwalk the round walks with (the Guild's east door is a double door, which the
	// miner's own plain walk cannot open).
	Walk WalkFunc
	// Manip, when set and Enabled, mines with the knife-on-log batch (essencerunner/minemanip.go): the rock rolls every 3 ticks
	// instead of every pickaxe-rate-plus-one. The kit (a knife and a plain log) is kept through every bank visit and drawn from the
	// bank when the pack lacks it; a load is then 25 essence. Nil is the plain mine.
	Manip *essencerunner.ManipConfig
	// Pickaxe is the pickaxe selection (essenceplan.BestAvailable when blank): the best tier the Mining level allows,
	// or one named tier. WalkTimeout bounds the walk to the gateway's NPC (5 minutes when zero; a Guild miner coming
	// back from the mule's post at Catherby, about 450 tiles, gives it ten).
	Pickaxe     string
	WalkTimeout time.Duration
}

func (o LiveOptions) pick() string {
	if p := strings.TrimSpace(o.Pickaxe); p != "" {
		return p
	}
	return essenceplan.BestAvailable
}

func (o LiveOptions) walkTimeout() time.Duration {
	if o.WalkTimeout > 0 {
		return o.WalkTimeout
	}
	return 5 * time.Minute
}

type liveEnv struct {
	d   *fleetrunner.Deps
	ed  *essencerunner.Deps
	opt LiveOptions
}

// EssenceDeps is the essence miner's Deps over the round's.
func EssenceDeps(d *fleetrunner.Deps) *essencerunner.Deps {
	return &essencerunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config, Components: d.Components,
		Chat: d.Chat, Delay: essencerunner.Delayer(d.Delay), Loot: d.Loot, Log: d.Log,
	}
}

// kitKeep is what a bank visit of the stretch leaves in the pack besides the pickaxe, and what the stage decision does not count
// as a haul: the knife and the log of the batch, when the stretch runs it (nil otherwise).
func (e *liveEnv) kitKeep() []string { return essencerunner.KitKeep(e.ed) }

// NewLiveEnv is the stretch's environment over a live session.
func NewLiveEnv(d *fleetrunner.Deps, opt LiveOptions) MineEnv {
	ed := EssenceDeps(d)
	ed.Manip = opt.Manip
	return &liveEnv{d: d, opt: opt, ed: ed}
}

func (e *liveEnv) InMine() bool   { return essencerunner.InMine(e.ed) }
func (e *liveEnv) PackFull() bool { return essencerunner.PackFull(e.ed) }
func (e *liveEnv) Haul() int {
	return essenceplan.MuleHaul(essencerunner.PackSlotRows(e.ed), e.kitKeep())
}

// EssenceHeld is the Rune essence in the pack: the load the stretch just mined (the stretch's log line reads it).
func (e *liveEnv) EssenceHeld() int { return essencerunner.EssenceHeld(e.ed) }

func (e *liveEnv) Tick() int     { return e.d.Session.State().Tick() }
func (e *liveEnv) Stopped() bool { return stopped(e.d) }

func (e *liveEnv) PickHeld() bool {
	return essenceplan.ResolvePick(e.opt.pick(), essencerunner.MiningLevel(e.ed), essencerunner.HeldNames(e.ed), nil).Kind == essenceplan.PickHeld
}

func (e *liveEnv) PickName() string {
	res := essenceplan.ResolvePick(e.opt.pick(), essencerunner.MiningLevel(e.ed), essencerunner.HeldNames(e.ed), nil)
	if res.Kind == essenceplan.PickHeld {
		return strings.ToLower(res.Item)
	}
	return "no pickaxe"
}

func (e *liveEnv) Mine() essencerunner.MineResult { return essencerunner.Mine(e.ed) }

func (e *liveEnv) PortalOut() bool { return essencerunner.PortalOut(e.ed) }

func (e *liveEnv) BankDeposit() (bankEssence, banked int, ok bool) {
	held := essencerunner.HeldNames(e.ed)
	keep := essenceplan.HeldPickaxeToKeep(e.opt.pick(), essencerunner.MiningLevel(e.ed), held)
	bank, opened := OpenBank(e.d, e.opt.Gateway.Bank)
	if !opened || bank == nil {
		return 0, 0, false
	}
	defer func() { _ = bank.Close() }()
	before := essencerunner.EssenceHeld(e.ed)
	var keepList []string
	if keep != "" {
		keepList = []string{keep}
	}
	keepList = append(keepList, e.kitKeep()...)
	essencerunner.DepositAllExcept(e.ed, bank, keepList)
	// The knife and the log of the batch come back from the bank when the pack lacks them (a leaked carrier, a first visit).
	essencerunner.DrawKit(e.ed, bank)
	ess, _, _ := countsOf(bank.Items())
	return ess, before, true
}

func (e *liveEnv) FetchPickaxe() (string, bool) {
	bank, opened := OpenBank(e.d, e.opt.Gateway.Bank)
	if !opened || bank == nil {
		return "", false
	}
	defer func() { _ = bank.Close() }()
	level := essencerunner.MiningLevel(e.ed)
	if essencerunner.PackFull(e.ed) {
		essencerunner.DepositAllExcept(e.ed, bank, nil)
	}
	res := essenceplan.ResolvePick(e.opt.pick(), level, essencerunner.HeldNames(e.ed), essencerunner.BankNames(e.ed, bank))
	switch res.Kind {
	case essenceplan.PickHeld:
		essencerunner.DrawKit(e.ed, bank)
		return strings.ToLower(res.Item), true
	case essenceplan.PickWithdraw:
		if essencerunner.WithdrawOneOf(e.ed, bank, res.Item) {
			essencerunner.DrawKit(e.ed, bank)
			return strings.ToLower(res.Item), true
		}
	}
	return "", false
}

func (e *liveEnv) TeleportIn() essencerunner.TeleportResult {
	gw := e.opt.Gateway
	if ok, why := gw.Allows(fleetrunner.MagicLevel(e.d)); !ok {
		return essencerunner.TeleportResult{Reason: why}
	}
	if e.opt.Walk != nil && !e.opt.Walk(gw.Stand, 2, e.opt.walkTimeout()) {
		return essencerunner.TeleportResult{Reason: "the walk to " + gw.NPC + " did not arrive (at " + fleetrunner.HereIs(e.d) + ")"}
	}
	// The click is sent from BESIDE the NPC (he wanders four tiles from his stand and the engine takes a click only from a tile that
	// touches him): the walk above only brings the account within two tiles of where he is spawned, which is the Guild's door tile.
	// The step beside him is made with the round's own door-aware walk, as the plain one cannot open the Guild's double door.
	return essencerunner.TeleportInWith(e.ed, gw, essencerunner.TeleportOptions{Walk: e.opt.Walk})
}

// Compile-time check that the live environment is the interface.
var _ MineEnv = (*liveEnv)(nil)
