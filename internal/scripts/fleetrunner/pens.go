package fleetrunner

import (
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// Targets is the swarm's combat-target claim and skip book, for the whole process.
var Targets = fleetplan.NewTargetBoard()

// Pen is the pen this account trains at: Deps.TrainPen, or the Cow pen when none was chosen, which
// is exactly what every caller got before pens existed.
func (d *Deps) Pen() fleetplan.TrainPen { return d.TrainPen.OrCowPen() }

// OnPenHazard reports whether the account stands on one of its pen's hazards (scarabplan.PenHazard).
func OnPenHazard(d *Deps) (fleetplan.PenHazard, bool) {
	here := CurrentTile(d)
	if here == nil {
		return fleetplan.PenHazard{}, false
	}
	return d.Pen().HazardAt(*here)
}

// StepOffPenHazard walks an account standing on its pen's hazard out to the pen's retreat tile,
// auto-retaliate off, and reports whether it had to. Every training step calls it first: a target
// can wander onto the hazard mid-fight and take the account with it, and an account with no target
// otherwise waits wherever it stands. Live 2026-09-26 22:08-22:26 on e2c53db4: acct104 sat 16
// minutes at 2560,3401 inside the goblins pen's Moss giant ground and a giant killed it.
func StepOffPenHazard(d *Deps) bool {
	h, on := OnPenHazard(d)
	if !on {
		return false
	}
	d.Logf("training: standing on %s (%s's hazard) at %s — walking out to %v", h.Name, d.Pen().Name, HereIs(d), PenRetreat(d))
	RetreatFromCows(d)
	return true
}

// PenRetreat is where a fight broken off at the health floor walks to, for this account's pen.
func PenRetreat(d *Deps) tile.Tile { return d.Pen().Retreat }

// NpcNamed reports whether npc type typ is named name (case-insensitive); false without a config. name
// may be a herd key (scarabplan.TrainPen.Herd: the Barbarians and the Barbarian women), and then any of
// its names matches.
func NpcNamed(d *Deps, typ int, name string) bool {
	if d == nil || d.Config == nil || name == "" {
		return false
	}
	t, ok := d.Config.Npc(typ)
	return ok && combatplan.HerdMatch(name, t.Name)
}

// Contested reports whether a candidate target belongs to someone else: the client's own view of
// it (another player's fight), another swarm account's claim, or a refusal this account was given.
func Contested(d *Deps, n gamestate.NPC, now time.Time) bool {
	if Targets.Skipped(d.World, n.Index, d.Me, now) || Targets.ClaimedByOther(d.World, n.Index, d.Me, now) {
		return true
	}
	self := d.Session.State().Self()
	ours := Targets.HeldBy(d.World, n.Index, d.Me, now)
	return fleetplan.TargetContested(ours, n.InCombat, n.TargetsPlayer(self), n.TargetsAnotherPlayer(self))
}

// ClaimTarget takes the chosen target for this account, and reports false if another swarm
// account got there first.
func ClaimTarget(d *Deps, index int) bool {
	return Targets.Claim(d.World, index, d.Me, time.Now(), fleetplan.TargetClaimTTL)
}

// EndFight releases this account's claim on a target and, when the server refused the attack with
// scarabplan.ContestedLine since `since`, leaves the target alone for a while.
func EndFight(d *Deps, index int, since time.Time) (refused bool) {
	Targets.Release(d.World, index, d.Me)
	if d.Chat != nil && fleetplan.SaidContested(d.Chat.Since(since)) {
		Targets.Skip(d.World, index, d.Me, time.Now().Add(fleetplan.TargetSkipFor))
		d.Logf("training: %q -- leaving that target alone for %s", fleetplan.ContestedLine, fleetplan.TargetSkipFor)
		return true
	}
	return false
}
