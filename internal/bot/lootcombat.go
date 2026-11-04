package bot

import (
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/combatplan"
)

// Loot attribution for the combat scripts (AdvancedFighter, CombatTrainer, and every script wired
// through KillRecorder): a kill is reported as one event for the NPC killed, and anything later
// picked up off the ground is measured as the pack's own change around the take and attributed to
// the kill it most plausibly came from. Nothing here changes what a script fights or picks up;
// every call is a no-op while Context.Loot is nil.
//
// The kill memory lives on the Context (Context.kills, a loot.KillLog), not on each script, so a
// kill reported through NoteKill — whichever script or hook reported it — is the one a later
// pickup finds. Until 2026-09-23 each combat script kept its own list, and CombatTrainer ended up
// with two reporters for one kill (see syncKills).

// lootPackComponent is the persistent inventory component, the same 3214 every runner's own
// PlayerPack names.
const lootPackComponent = 3214

// lootPickupWait bounds how long a confirmed take waits for the item to show in the pack (one
// tick); a take that already shows returns at once.
const lootPickupWait = 600 * time.Millisecond

// lootKills is a combat script's loot bookkeeping. It keeps only how far syncKills has read; the
// kills themselves are remembered on the Context.
type lootKills struct {
	// syncedMs is the newest KillSite.AtMs syncKills has already consumed.
	syncedMs int64
}

// syncKills consumes CombatTrainer's own kill sites and reports NOTHING.
//
// THE DOUBLE COUNT IT USED TO BE. CombatTrainer records each confirmed kill in its killSites list
// (fightStep, which this file does not touch), and this reported every new site as a kill — the
// only report CombatTrainer had until 2026-09-22. Commit 0008c228 then wired
// `OnKill: ctx.KillRecorder()` into its combatrunner.Deps, so combatrunner.WaitForKill reports the
// same kill the moment it lands, and this went on reporting it again at the next lootStep: every
// Killed or LikelyKilled kill on every rung, cows to giants, counted twice. Nothing had been
// persisted yet only because no CombatTrainer account had run while the Loot tab was tracking.
//
// OnKill is the better of the two reporters — it carries the npc type actually observed and the
// kill's own tile, and it also sees a kill whose verdict never becomes a kill site — so it stays
// and this stops. The sites are still consumed, so nothing changes if a caller ever does rely on
// syncedMs.
func (k *lootKills) syncKills(ctx *Context, sites []combatplan.KillSite, name string, level int) {
	for _, s := range sites {
		if s.AtMs > k.syncedMs {
			k.syncedMs = s.AtMs
		}
	}
}

// killed reports one observed kill of npcType at a tile (AdvancedFighter's killWatch verdict).
func (k *lootKills) killed(ctx *Context, npcType int, name string, x, z, level int) {
	ctx.noteKill(npcType, name, x, z, level)
}

// packBefore is the pack reading a take is measured against, with the account's random-event
// activity at the same moment (a take is thrown away when an event ran inside it: loot/take.go);
// nil when loot is not recorded. The activity is read FIRST: an event that began between the two
// readings would otherwise go unseen, and its reward lands seconds later, inside the take.
func lootPackBefore(ctx *Context) *loot.Reading {
	if ctx.Loot == nil {
		return nil
	}
	events := randomevents.ActivityOf(ctx.Session)
	return &loot.Reading{
		Counts:    loot.PackCounts(ctx.Session.State().Inventory(lootPackComponent)),
		Disturbed: events.Disturbed,
	}
}

// taken reports what a confirmed take of the drop at x,z,level added to the pack. A drop no
// recent kill can claim is not reported: it is not this script's loot to attribute.
func (k *lootKills) taken(ctx *Context, before *loot.Reading, x, z, level int) {
	if ctx.Loot == nil || before == nil {
		return
	}
	src, ok := ctx.kills.SourceFor(x, z, level, time.Now())
	if !ok {
		return
	}
	ctx.recordTake(src, before)
}

// recordTake measures what arrived in the pack since before and reports it under src with no
// events: the kill or pickup that made it is counted where it happened.
//
// Nothing is reported when a random event ran during the take (loot.TakeGained), and an event's own
// item (a Lamp, a Strange box) is never part of one. The lifetime Cow card held Feather x5,014 and
// Casket x70 from takes an event paid out inside (a user request).
func (c *Context) recordTake(src loot.Source, before *loot.Reading) {
	read := func() *gamestate.Inventory { return c.Session.State().Inventory(lootPackComponent) }
	gained, discarded := loot.TakeGained(read, before, c.Delay, lootPickupWait, nil)
	if discarded {
		c.logf("loot: not recording a take for %s: a random event ran while it was being measured", src.Name)
		return
	}
	if len(gained) == 0 {
		return
	}
	c.Loot(loot.Drop{Source: src, Items: loot.Items(c.Config, gained)})
}

// NotePickup reports what a confirmed ground pickup at x,z,level added to the pack: under the kill
// that left it when a recent kill of this account can claim it (KillLog's rule), otherwise as a
// pickup of the item itself (loot.Pickup) — a map spawn or a stray bone no kill of ours made.
// before is lootPackBefore's reading, taken just before the take was sent.
func (c *Context) NotePickup(before *loot.Reading, objID, x, z, level int) {
	if c == nil || c.Loot == nil || before == nil {
		return
	}
	src, ok := c.kills.SourceFor(x, z, level, time.Now())
	if !ok {
		// A rune or a bone no recent kill claimed still goes under the account's newest kill, never a
		// pickup card of its own (loot.PickupSource); a rune with no kill at all is not recorded.
		var picked bool
		src, _, picked = loot.PickupSource(c.Config, &c.kills, objID, "")
		if !picked {
			c.logf("loot: not recording a rune taken off the ground: this account has no kill to put it under")
			return
		}
	}
	c.recordTake(src, before)
}

// NoteKill records one observed kill for the control panel's loot tracker, and remembers it (with
// its tile) so a drop taken afterwards is attributed to it.
//
// WHY THIS IS ON Context. Until 2026-09-22 kills reached the loot tracker from exactly ONE
// script: advancedfighter.go was the only file in this package that owned a lootKills, so
// every other script that fights recorded nothing. The fleet owner's requirement is that
// every NPC the fleet can fight and kill is tracked, and a per-script field cannot deliver
// that — it is something each of twenty-two scripts would have to remember.
//
// Measured live before this existed: ScarabSwarm's Stage 3 trains Magic by killing Cows
// north of the Ardougne market, and `/api/loot` contained not one Cow, Cowhide, Bones or
// Raw beef — nor any kill from CombatTrainer, MeleeFlincher or Stage 5. The panel's own
// fake-data set had a Cow row all along, so the display was ready and nothing fed it.
//
// The caller passes the npc TYPE id; the name, combat level and drop attribution come from
// the cache through loot.NPC, so no caller has to repeat that lookup or disagree about it.
// Nil-safe in every direction: a Context with no Loot sink (every cmd/ harness and test)
// drops the call.
func (c *Context) NoteKill(npcType, x, z, level int) {
	c.noteKill(npcType, "", x, z, level)
}

func (c *Context) noteKill(npcType int, name string, x, z, level int) {
	if c == nil || c.Loot == nil || npcType < 0 {
		return
	}
	src := loot.NPC(c.Config, loot.ActionKill, npcType, name)
	c.Loot(loot.Drop{Source: src, Events: 1})
	c.kills.Note(src, x, z, level, time.Now())
}

// KillLog is this account's kill memory, for a runner package whose Deps cannot hold a *Context:
// ScarabSwarm's scarabrunner notes its own kills into it and attributes the bones it takes from
// it. Nil for a nil Context.
func (c *Context) KillLog() *loot.KillLog {
	if c == nil {
		return nil
	}
	return &c.kills
}

// ShareKills makes this account's kill memory note into and ask the shared kill log of the world
// it is on (loot.FleetKills), so a drop another account's kill left in a shared pen is still
// filed under that npc. internal/fleet calls it for every live account; a Context built anywhere
// else (a test, a cmd/ harness) keeps its kills to itself.
func (c *Context) ShareKills() {
	if c == nil {
		return
	}
	c.kills.Share(func() *loot.KillLog { return loot.FleetKills(c.World()) })
}

// KillRecorder returns a func suitable for combatrunner.Deps.OnKill, so wiring kill tracking
// into a script is one field rather than a closure each script writes for itself.
//
// Returns nil when this Context has no loot sink, which is what combatrunner's own nil check
// wants: a harness then takes exactly the path it took before the hook existed.
func (c *Context) KillRecorder() func(npcType, x, z, level int) {
	if c == nil || c.Loot == nil {
		return nil
	}
	return c.NoteKill
}

// KillSpentRecorder is KillRecorder for a script that casts spells: a func for
// combatrunner.Deps.OnKillSpent, which the runner calls INSTEAD of OnKill with the runes the fight
// measurably used up, so the panel nets them off that npc's drops and the Magic card counts the
// spell. Nil without a loot sink.
func (c *Context) KillSpentRecorder() func(npcType, x, z, level int, spent map[int32]int) {
	if c == nil || c.Loot == nil {
		return nil
	}
	return func(npcType, x, z, level int, spent map[int32]int) {
		if npcType < 0 {
			return
		}
		src := loot.NPC(c.Config, loot.ActionKill, npcType, "")
		c.Loot(loot.Drop{Source: src, Events: 1, Spent: c.runeItems(spent)})
		c.kills.Note(src, x, z, level, time.Now())
	}
}

// FightSpentRecorder is what combatrunner.Deps.OnFightSpent calls for a fight that used runes and
// ended with no kill: the runes are that npc's cost, with no kill added. Nil without a loot sink.
func (c *Context) FightSpentRecorder() func(npcType int, spent map[int32]int) {
	if c == nil || c.Loot == nil {
		return nil
	}
	return func(npcType int, spent map[int32]int) {
		if npcType < 0 || len(spent) == 0 {
			return
		}
		c.Loot(loot.Drop{Source: loot.NPC(c.Config, loot.ActionKill, npcType, ""), Spent: c.runeItems(spent)})
	}
}

// runeItems is a measured rune spend as loot items, named through the cache.
func (c *Context) runeItems(spent map[int32]int) []loot.Item {
	counts := loot.Counts{}
	for id, n := range spent {
		counts[int(id)] = int64(n)
	}
	return loot.Items(c.Config, counts)
}
