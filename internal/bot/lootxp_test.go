package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
)

func xpEv(stat, delta int) gamestate.Event {
	return gamestate.Event{Kind: gamestate.EventSkillXP, Skill: stat, XPDelta: delta}
}

// TestSkillCardsTakeTheirOwnSkillsExperience: the logs a window reports take the Woodcutting experience
// that arrived since the last report, once, and nothing else's.
func TestSkillCardsTakeTheirOwnSkillsExperience(t *testing.T) {
	w := newXPWatch()
	w.onEvent(xpEv(8, 25))  // Woodcutting
	w.onEvent(xpEv(8, 37))  // Woodcutting
	w.onEvent(xpEv(10, 40)) // Fishing, not this card's
	d := loot.Drop{Source: loot.Woodcutting(nil, 1276, "Tree"), Events: 2, Items: []loot.Item{{ID: 1511, Count: 2}}}
	w.attach(&d)
	if d.XP != 62 {
		t.Fatalf("woodcutting drop took %d xp, want 62", d.XP)
	}
	again := loot.Drop{Source: loot.Woodcutting(nil, 1276, "Tree"), Events: 1, Items: []loot.Item{{ID: 1511, Count: 1}}}
	w.attach(&again)
	if again.XP != 0 {
		t.Fatalf("the same experience was taken twice: %d", again.XP)
	}
	fish := loot.Drop{Source: loot.Fishing(nil, 316), Events: 1, Items: []loot.Item{{ID: 317, Count: 1}}}
	w.attach(&fish)
	if fish.XP != 40 {
		t.Fatalf("fishing drop took %d xp, want the 40 that was its own", fish.XP)
	}
}

// TestAKillTakesTheCombatExperienceBySkill: Attack, Strength, Defence and Hitpoints from the fight, and
// not a Woodcutting log; a pickup that is no kill takes nothing.
func TestAKillTakesTheCombatExperienceBySkill(t *testing.T) {
	w := newXPWatch()
	w.onEvent(xpEv(2, 80)) // Strength
	w.onEvent(xpEv(2, 40))
	w.onEvent(xpEv(3, 40)) // Hitpoints
	w.onEvent(xpEv(8, 25))
	cow := loot.NPC(nil, loot.ActionKill, 81, "Cow")
	take := loot.Drop{Source: cow, Items: []loot.Item{{ID: 526, Count: 1}}}
	w.attach(&take)
	if len(take.Skills) != 0 {
		t.Fatalf("a take with no kill took %+v", take.Skills)
	}
	kill := loot.Drop{Source: cow, Events: 1}
	w.attach(&kill)
	got := map[string]int64{}
	for _, k := range kill.Skills {
		got[k.Name] = k.Count
	}
	if len(got) != 2 || got["Strength"] != 120 || got["Hitpoints"] != 40 {
		t.Fatalf("kill took %v, want Strength 120 and Hitpoints 40", got)
	}
	// And the Woodcutting stays for the card that earned it.
	logs := loot.Drop{Source: loot.Woodcutting(nil, 1276, "Tree"), Events: 1}
	w.attach(&logs)
	if logs.XP != 25 {
		t.Fatalf("woodcutting xp = %d, want 25", logs.XP)
	}
}

// TestExperienceNotAttributedWhenItCannotBeTrusted: a quest-sized rise, a teleport's, a stale one, the
// Prayer card's own, and the Combat spells mirror row.
func TestExperienceNotAttributedWhenItCannotBeTrusted(t *testing.T) {
	w := newXPWatch()
	w.onEvent(xpEv(8, 13750)) // a quest reward
	d := loot.Drop{Source: loot.Woodcutting(nil, 1276, "Tree"), Events: 1}
	w.attach(&d)
	if d.XP != 0 {
		t.Fatalf("a quest reward was attributed to a tree: %d", d.XP)
	}
	w.onEvent(xpEv(loot.MagicStat, 35)) // a teleport
	w.discard(loot.MagicStat, 35)
	w.onEvent(xpEv(loot.MagicStat, 65)) // an alchemy
	alch := loot.Drop{Source: loot.Alchemy(1, "Yew shortbow", true, true), Events: 1, Items: []loot.Item{{ID: 995, Count: 100}}}
	w.attach(&alch)
	if alch.XP != 65 {
		t.Fatalf("alchemy took %d xp, want only its own 65", alch.XP)
	}
	w.onEvent(xpEv(loot.PrayerStat, 4))
	bury := loot.Drop{Source: loot.Bury(nil, 526, ""), Events: 1, XP: 4}
	w.attach(&bury)
	if bury.XP != 4 {
		t.Fatalf("a burial's own experience was replaced: %d", bury.XP)
	}
	w.onEvent(xpEv(loot.MagicStat, 20))
	mirror := loot.Drop{Source: loot.CombatSpells(), Events: 1, Spent: []loot.Item{{ID: 554, Count: 3}}}
	w.attach(&mirror)
	if mirror.XP != 0 || len(mirror.Skills) != 0 {
		t.Fatalf("the Combat spells mirror took experience: %+v", mirror)
	}
	// Stale: held for longer than the pending TTL.
	w2 := newXPWatch()
	now := time.Now()
	w2.now = func() time.Time { return now }
	w2.onEvent(xpEv(8, 25))
	w2.now = func() time.Time { return now.Add(xpPendingTTL + time.Minute) }
	late := loot.Drop{Source: loot.Woodcutting(nil, 1276, "Tree"), Events: 1}
	w2.attach(&late)
	if late.XP != 0 {
		t.Fatalf("stale experience was attributed: %d", late.XP)
	}
}

// TestTrackExperienceWrapsTheSinkOnce: the wrapper hands the experience to the sink's drop, a Context
// with no sink is left alone, and a second call does not stack another wrapper.
func TestTrackExperienceWrapsTheSinkOnce(t *testing.T) {
	var got []loot.Drop
	ctx := &Context{Loot: func(d loot.Drop) { got = append(got, d) }}
	ctx.TrackExperience()
	wrapped := ctx.xp
	ctx.TrackExperience()
	if wrapped == nil || ctx.xp != wrapped {
		t.Fatal("a second TrackExperience replaced the watch")
	}
	wrapped.onEvent(xpEv(17, 8)) // Thieving
	ctx.Loot(loot.Drop{Source: loot.NPC(nil, loot.ActionPickpocket, 1, "Man"), Events: 1, Items: []loot.Item{{ID: 995, Count: 3}}})
	if len(got) != 1 || got[0].XP != 8 {
		t.Fatalf("sink saw %+v, want the pickpocket with its 8 thieving xp", got)
	}
	bare := &Context{}
	bare.TrackExperience()
	bare.watchExperience()
	if bare.Loot != nil || bare.xp != nil {
		t.Fatal("a Context with no sink was wrapped")
	}
}
