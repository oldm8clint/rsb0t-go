package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/combatplan"
)

// TestCombatTrainerCountsEachKillOnce is the giants' double count.
//
// CombatTrainer reported a kill twice over: combatrunner.WaitForKill calls its OnKill hook
// (ctx.KillRecorder, wired by 0008c228) the moment the kill lands, and fightStep appends the same
// kill to killSites, which lootStep's syncKills used to report again. This replays exactly that
// sequence — one FightOne kill of a Hill Giant, then the next two lootSteps — and requires ONE
// event. On the old code it reports two.
func TestCombatTrainerCountsEachKillOnce(t *testing.T) {
	var got []loot.Drop
	ctx := &Context{Loot: func(d loot.Drop) { got = append(got, d) }}
	var k lootKills

	// FightOne's WaitForKill: the hook fires with the observed type and the kill's own tile.
	ctx.KillRecorder()(117, 3100, 9840, 0)
	// fightStep then records the site, and the next lootStep syncs (twice, as a loop would).
	sites := []combatplan.KillSite{{X: 3100, Z: 9840, Level: 0, AtMs: 1000}}
	k.syncKills(ctx, sites, "Hill Giant", 28)
	k.syncKills(ctx, sites, "Hill Giant", 28)

	events := 0
	for _, d := range got {
		events += d.Events
	}
	if events != 1 {
		t.Fatalf("one kill reported %d kill events: %+v", events, got)
	}
	if got[0].Source.Action != loot.ActionKill || got[0].Source.ID != 117 {
		t.Fatalf("the kill was reported as %+v, want kill of npc 117", got[0].Source)
	}
	if k.syncedMs != 1000 {
		t.Fatalf("sites were not consumed: syncedMs %d", k.syncedMs)
	}
	// The Big bones taken afterwards are attributed to that same kill, from the Context's memory.
	src, ok := ctx.kills.SourceFor(3101, 9841, 0, time.Now())
	if !ok || src != got[0].Source {
		t.Fatalf("a drop beside the kill went to %+v (%v), want the kill's own source", src, ok)
	}
}

// TestNoteKillRemembersTheKillForItsDrops: every script's NoteKill — MeleeFlincher,
// AdvancedFighter's killWatch, ScarabSwarm through KillLog — now leaves the kill where a pickup
// can find it; before this only AdvancedFighter's own list did.
func TestNoteKillRemembersTheKillForItsDrops(t *testing.T) {
	ctx := &Context{Loot: func(loot.Drop) {}}
	ctx.NoteKill(81, 2600, 3300, 0)
	src, ok := ctx.KillLog().SourceFor(2601, 3299, 0, time.Now())
	if !ok || src.ID != 81 || src.Action != loot.ActionKill {
		t.Fatalf("the cow kill was not remembered: %+v %v", src, ok)
	}
	// Without a loot sink nothing is reported and nothing is remembered.
	quiet := &Context{}
	quiet.NoteKill(81, 2600, 3300, 0)
	if _, ok := quiet.KillLog().SourceFor(2600, 3300, 0, time.Now()); ok {
		t.Fatal("a Context with no loot sink remembered a kill")
	}
	if (*Context)(nil).KillLog() != nil {
		t.Fatal("a nil Context returned a kill log")
	}
}
