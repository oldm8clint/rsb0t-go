package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/combatplan"
)

// The cow used throughout: npc type 81, the Cow the panel's own fake-data set has always had a
// row for (internal/controlpanel/fakeloot.go) and the monster ScarabSwarm's Stage 3 actually
// trains Magic on, so the ids here are the ones a live reading carries.
func killWatchCow(health, total int) gamestate.NPC {
	return gamestate.NPC{Index: 7, Type: 81, X: 3250, Z: 3270, Level: 0, Health: health, TotalHealth: total}
}

// A kill whose corpse has already left the NPC list is what AdvancedFighter counted in b.kills
// and then dropped from the loot panel: the old line read live.Type off the miss, and
// gamestate.NPC zero-values on a miss, so the only id available was 0. This is the case that
// fix exists for — the type and tile have to come from the last reading in which the target was
// present, and they have to survive the reading that shows it gone.
func TestKillWatchNamesAKillWhoseCorpseAlreadyLeftTheNPCList(t *testing.T) {
	var w killWatch
	if v := w.observe(7, killWatchCow(5, 40), true, true); v != combatplan.Alive {
		t.Fatalf("a cow at 5 of 40 hitpoints judged %q, want %q — it is still standing", v, combatplan.Alive)
	}
	if _, _, _, _, took := w.killed(combatplan.Alive); took {
		t.Fatal("a living target was recorded as a kill; only a death may reach the loot panel")
	}
	v := w.observe(7, gamestate.NPC{}, false, false)
	if v != combatplan.LikelyKilled {
		t.Fatalf("a cow that was fighting us at 5 of 40 hitpoints and then left the list judged %q, want %q", v, combatplan.LikelyKilled)
	}
	npcType, x, z, level, took := w.killed(v)
	if !took {
		t.Fatal("the kill was not recorded, so MeleeFlincher/AdvancedFighter would again count it and show nothing on the panel")
	}
	if npcType != 81 || x != 3250 || z != 3270 || level != 0 {
		t.Fatalf("the kill was named %d at %d,%d,%d, want type 81 at 3250,3270,0 — the last reading in which it was present", npcType, x, z, level)
	}
	if _, _, _, _, again := w.killed(v); again {
		t.Fatal("the same death was handed out twice; a fight loop that asks again would double the panel's kill count")
	}
}

// A zero-health reading is the only confirmed death this client has, and the tile recorded has
// to be the one it died on rather than the one it was first seen on: drops land around the
// corpse, and lootKills.sourceFor matches them within four tiles of the recorded kill.
func TestKillWatchRecordsAConfirmedDeathAtTheTileItDiedOn(t *testing.T) {
	var w killWatch
	w.observe(7, killWatchCow(40, 40), true, true)
	moved := killWatchCow(0, 40)
	moved.X, moved.Z = 3256, 3277
	v := w.observe(7, moved, true, true)
	if v != combatplan.Killed {
		t.Fatalf("a zero-health reading judged %q, want %q — this is the one confirmed death signal there is", v, combatplan.Killed)
	}
	npcType, x, z, level, took := w.killed(v)
	if !took || npcType != 81 || x != 3256 || z != 3277 || level != 0 {
		t.Fatalf("recorded %d at %d,%d,%d (took=%v), want type 81 at 3256,3277,0 — where it actually died, so its drops can be attributed", npcType, x, z, level, took)
	}
}

// The trap docs/trustworthy_harnesses.md lists as number 4, and the one combatplan exists to
// refuse: an NPC that is simply not in the list any more may have died, walked out of range,
// despawned, or been killed by somebody else. A held target nothing ever hit carries no
// evidence whatsoever, so it must not put a kill on the panel — even though AdvancedFighter's
// own b.kills counter does count it.
func TestKillWatchRefusesAVanishedTargetNothingEverHit(t *testing.T) {
	var w killWatch
	unhit := killWatchCow(0, 0) // TotalHealth 0: no hitmark has ever landed on it
	if v := w.observe(7, unhit, true, false); v != combatplan.Unknown {
		t.Fatalf("an NPC nobody has hit judged %q, want %q — its Health field means nothing yet", v, combatplan.Unknown)
	}
	v := w.observe(7, gamestate.NPC{}, false, false)
	if v != combatplan.Vanished {
		t.Fatalf("an unhit NPC that left the list judged %q, want %q", v, combatplan.Vanished)
	}
	if _, _, _, _, took := w.killed(v); took {
		t.Fatal("a monster this account never landed a hit on was recorded as a kill — that is a fabricated row on the loot panel, not an undercount")
	}
	// But one that was hit and then vanished at high health is still this script's kill: it was
	// held inside the leash and damaged, and the corpse leaving between two 600 ms polls is the
	// ordinary way a fight ends.
	var hit killWatch
	hit.observe(7, killWatchCow(30, 40), true, false)
	v = hit.observe(7, gamestate.NPC{}, false, false)
	if v != combatplan.Vanished {
		t.Fatalf("a hit NPC at 30 of 40 hitpoints that left the list judged %q, want %q", v, combatplan.Vanished)
	}
	if _, _, _, _, took := hit.killed(v); !took {
		t.Fatal("a damaged target that left the list was not recorded, which is the undercount AdvancedFighter already had")
	}
}

// A target never read at all must name nothing. Without this an empty watch could hand out its
// zero fields and file the kill under npc type 0.
func TestKillWatchNamesNothingWhenTheTargetWasNeverRead(t *testing.T) {
	var w killWatch
	v := w.observe(7, gamestate.NPC{}, false, false)
	if v != combatplan.Unknown {
		t.Fatalf("a target never seen present judged %q, want %q", v, combatplan.Unknown)
	}
	for _, try := range []combatplan.Verdict{combatplan.Unknown, combatplan.Killed, combatplan.Vanished} {
		if npcType, _, _, _, took := w.killed(try); took || npcType != -1 {
			t.Fatalf("verdict %q named npc type %d from a watch that read nothing (took=%v), want -1 and no record", try, npcType, took)
		}
	}
}

// NPC list indexes are reused, so one target's identity must never be handed out for another's
// death — that would put the wrong monster, and the wrong drop table, on the panel. A reported
// death also has to start a fresh watch, or the next NPC to arrive under that index would be
// silently uncountable for the rest of the run.
func TestKillWatchDoesNotCarryOneTargetsIdentityToTheNext(t *testing.T) {
	var w killWatch
	w.observe(7, killWatchCow(5, 40), true, true)
	v := w.observe(7, gamestate.NPC{}, false, false)
	if _, _, _, _, took := w.killed(v); !took {
		t.Fatal("the first kill was not recorded")
	}

	// A different NPC arrives under a different index and vanishes before anything reads it.
	if v := w.observe(9, gamestate.NPC{}, false, false); v != combatplan.Unknown {
		t.Fatalf("a fresh index judged %q, want %q — nothing about it has been read", v, combatplan.Unknown)
	}
	if npcType, _, _, _, took := w.killed(combatplan.Vanished); took || npcType != -1 {
		t.Fatalf("index 9 borrowed index 7's identity (type %d, took=%v)", npcType, took)
	}

	// A goblin now reuses index 7. Its own death must be countable, as itself.
	goblin := gamestate.NPC{Index: 7, Type: 100, X: 3100, Z: 3200, Health: 0, TotalHealth: 12}
	w.observe(7, gamestate.NPC{Index: 7, Type: 100, X: 3100, Z: 3200, Health: 6, TotalHealth: 12}, true, true)
	v = w.observe(7, goblin, true, true)
	npcType, x, _, _, took := w.killed(v)
	if !took || npcType != 100 || x != 3100 {
		t.Fatalf("the reused index recorded type %d at x %d (took=%v), want the goblin, type 100 at 3100", npcType, x, took)
	}
}

// Nil-safety is the claim every one of these changes rests on: every cmd/ harness and every
// existing test runs a Context with no Loot sink, and must behave exactly as it did before the
// hook existed. "Additive" is a claim, so this asserts it on the two paths a kill can take to
// the panel — Context.NoteKill (MeleeFlincher) and lootKills.killed (AdvancedFighter).
func TestRecordingAKillWithNoLootSinkDoesNothing(t *testing.T) {
	var w killWatch
	w.observe(7, killWatchCow(5, 40), true, true)
	v := w.observe(7, gamestate.NPC{}, false, false)
	npcType, x, z, level, took := w.killed(v)
	if !took {
		t.Fatal("the kill was not judged, so this test would prove nothing")
	}

	quiet := &Context{} // no Loot: every harness, and the panel-less case
	quiet.NoteKill(npcType, x, z, level)
	var quietKills lootKills
	quietKills.killed(quiet, npcType, "", x, z, level)
	if src, ok := quiet.KillLog().SourceFor(x, z, level, time.Now()); ok {
		t.Fatalf("a kill was remembered for drop attribution with no loot sink: %+v", src)
	}

	var got []loot.Drop
	wired := &Context{Loot: func(d loot.Drop) { got = append(got, d) }}
	wired.NoteKill(npcType, x, z, level)
	if len(got) != 1 {
		t.Fatalf("a wired Context recorded %d drops, want exactly 1 kill event", len(got))
	}
	if got[0].Events != 1 || got[0].Source.Action != loot.ActionKill || got[0].Source.ID != 81 {
		t.Fatalf("recorded %+v, want one kill event for npc type 81 — the panel resolves the name and combat level from the type", got[0])
	}
}
