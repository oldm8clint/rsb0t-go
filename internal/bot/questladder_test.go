package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/questplan"
)

// Every rung of the owner's ladder is either a quest factory under the same id the ladder
// planner writes, or one of the rungs still to be ported (named here, so a port that forgets
// to register itself, or an id that drifts from the planner's, fails this test).
func TestEveryLadderRungHasAFactoryOrIsKnownUnported(t *testing.T) {
	unported := map[string]bool{}
	for _, id := range questplan.OwnerLadder {
		f, ok := QuestFactories[id]
		switch {
		case ok && unported[id]:
			t.Errorf("%s is registered: take it off the unported list", id)
		case !ok && !unported[id]:
			t.Errorf("ladder rung %q has no quest factory", id)
		case ok:
			if q := f(nil); q.ID() != id {
				t.Errorf("factory %q builds a quest with id %q", id, q.ID())
			}
		}
	}
}

// Every training rung the planner can insert (LadderFacts with a TrainSkill) has a factory
// under its own id, or is named here as still unported.
func TestEveryTrainingRungHasAFactoryOrIsKnownUnported(t *testing.T) {
	unported := map[string]bool{}
	for id, f := range questplan.LadderFacts {
		if f.TrainSkill == "" {
			continue
		}
		q, ok := QuestFactories[id]
		switch {
		case ok && unported[id]:
			t.Errorf("%s is registered: take it off the unported list", id)
		case !ok && !unported[id]:
			t.Errorf("training rung %q has no quest factory", id)
		case ok:
			if got := q(nil).ID(); got != id {
				t.Errorf("factory %q builds a quest with id %q", id, got)
			}
		}
	}
}

// Every hard gate a quest has (server, owner, item or module) names a level the planner inserts
// a rung for, and that rung has a factory: a gate no rung produces is a ladder livelock, the
// quest's module waiting for a level nothing trains (Horror's Prayer 40 was one). The factory is
// QuestFactoryFor's, so the rung may be a generic stop (train:crafting:19) as well as a named one
// (lane N2), and it must build a quest under the rung's own id.
func TestEveryHardGateHasARungThatProducesIt(t *testing.T) {
	for id, f := range questplan.LadderFacts {
		for _, g := range f.Gates {
			if g.Kind == questplan.GateAdvised {
				continue
			}
			rung := questplan.GateRungID(g)
			build, why := QuestFactoryWhy(rung)
			if build == nil {
				t.Errorf("%s: gate %s %d (%s) inserts %q, which has no factory (%s)", id, g.Skill, g.Level, g.Kind, rung, why)
				continue
			}
			if got := build(nil).ID(); got != rung {
				t.Errorf("%s: gate %s %d inserts %q and its factory builds %q", id, g.Skill, g.Level, rung, got)
			}
		}
	}
}

// Only proven quests run by default: an unproven id is refused unless allowUnproven is set,
// an unknown id is always refused, and the default is Waterfall.
func TestAnUnprovenQuestIsRefusedByDefault(t *testing.T) {
	if _, err := SelectQuests(map[string]interface{}{"quests": []interface{}{"merlin"}}); err == nil {
		t.Fatal("an unproven quest ran without allowUnproven")
	}
	if qs, err := SelectQuests(map[string]interface{}{"quests": []interface{}{"merlin"}, "allowUnproven": true}); err != nil || len(qs) != 1 {
		t.Fatalf("allowUnproven: %v %v", qs, err)
	}
	if _, err := SelectQuests(map[string]interface{}{"quests": []interface{}{"nosuchquest"}, "allowUnproven": true}); err == nil {
		t.Fatal("an unknown quest id was accepted")
	}
	qs, err := SelectQuests(nil)
	if err != nil || len(qs) != 1 || qs[0].ID() != "waterfall" {
		t.Fatalf("default: %v %v", qs, err)
	}
	if _, err := SelectQuests(map[string]interface{}{"quests": []interface{}{"Waterfall"}}); err != nil {
		t.Fatalf("the proven quest was refused: %v", err)
	}
}

// provenEvidence is the recorded proof behind every entry of ProvenQuests. Promoting a quest
// means adding it to BOTH (see ProvenQuests' doc comment).
var provenEvidence = map[string]string{
	"waterfall": "lab EMPTY wf-empty-13 PASS 9/9 and CONTROL wf-control-1 FAIL, both on 819029b6; " +
		"review of claude/questlane, landed on main as be71dd3e; " +
		"re-proved WITH worn gear after the tomb strip's re-equip landed (staff of air, leather armour and boots, " +
		"50 bronze arrows): EMPTY wfwear-empty-1 PASS 11/11 incl. the gear worn again with the stack whole, and " +
		"CONTROL wfwear-control-1 FAIL 8 of 9, both on ba83b9a6 (claude/questpath-0925, 2026-09-25); " +
		"re-proved after the live full-pack faults (the ledge-door livelock, the one rope shop, the relics banked " +
		"by the mule ask) with 22 cakes in the pack: EMPTY wffull-empty-2 PASS 12/12 incl. no door taken over and " +
		"over, and CONTROL wffull-control-1 FAIL 8 of 10, both on 5e42018e (claude/questpath-0926, 2026-09-26)",
	"hetty": "lab EMPTY hetty-empty-4 PASS 7/7 (complete in 3m30s, 1 QP, 3,250 Magic XP at xpRate 10, " +
		"journal complete, the empty mule never asked, ::getvar hetty 3) and CONTROL hetty-control-2 FAIL 5 of 5 " +
		"(parked on no eye of newt), both on 2afcf679 (claude/questproof-0926, 2026-09-26/27); " +
		"logs private evidence notes re-passed EMPTY hetty-empty-5 7/7 on 5017ce45 " +
		"after the shared NPC-approach, fare-reserve and mesbox changes",
	"vampire": "lab EMPTY vampire-empty-2 PASS 7/7 (complete in 6m11s, 3 QP, Attack +48,350 at xpRate 10, " +
		"journal complete, the empty mule never asked, ::getvar vampire 3) and CONTROL vampire-control-1 FAIL 5 of 5 " +
		"(parked on no coins for Dr Harlow's beer), both on c43b5044 (claude/questproof-0926, 2026-09-27); " +
		"an earlier EMPTY vampire-empty-1 PASS 7/7 on 2afcf679 agrees; " +
		"logs private evidence notes re-passed EMPTY vampire-empty-3 7/7 on 5017ce45",
	"murder": "lab EMPTY murder-empty-3 PASS 7/7 (complete in 4m0s, 3 QP, Crafting +14,060 at xpRate 10, " +
		"journal complete, the empty mule never asked, ::getvar murderquest 2) and CONTROL murder-control-1 FAIL 5 of 5 " +
		"(parked on the pot kit), both on 08989613 (claude/questproof-0926, 2026-09-27), after the window stand " +
		"(ApproachLoc's webwalker pass) and the flour barrel's walled stand were fixed; " +
		"logs private evidence notes re-passed EMPTY murder-empty-4 7/7 on 5017ce45",
	"pip": "lab EMPTY pip-empty-4 PASS 7/7 (complete in 26m42s, quest points 1 -> 2, Prayer +14,060 at xpRate 10, " +
		"journal complete, the empty mule never asked, ::getvar priestperil 60) and CONTROL pip-control-1 FAIL 5 of 5 " +
		"(parked on the bucket kit, priestperil 5), both on 930d44ef (claude/questproof-0926, 2026-09-27), staged at " +
		"its owner-ladder levels (Attack 35, Strength 32, Defence 30, Hitpoints 28) with Rune Mysteries set done; " +
		"logs private evidence notes",
	"ghost": "promoted on LIVE evidence with no lab control (the lead's ruling (b) of 15 Oct 2026: the quest is provision-free, " +
		"so a control that fails for want of a kit cannot exist; Father Urhney hands the Ghostspeak amulet out and re-issues it to an " +
		"account that owns none, \"I've lost the amulet.\", and Nature Spirit withdraws and wears it): cmd/questpromote over the fleet's " +
		"256 logs of 17 Sep to 15 Oct 2026 and 345 quest stores, run 15 Oct 2026. Since the Wizards' Tower diagonal-door fix cd221b9b " +
		"(first live in build 8afe4821, 7 Oct 10:09): 27 completions on 27 distinct accounts, 0 deaths and 0 lost fights, 8 parks in " +
		"39 runs (0.21 per run, every one \"its walks cannot get there\" on three accounts stranded elsewhere). Since the module's " +
		"own last change 7a75cf7a (7 Oct 01:55, first live in 184df864 at 03:01) the same logs hold 29 completions on 29 accounts " +
		"but 118 parks in 156 runs (0.76) and one fight lost; 110 of those parks and the lost fight fall in builds 184df864, 0525338b " +
		"and 097c4b4f, whose accounts stalled at the Wizards' Tower west room's diagonal door (the lost fight was a failed walk out of the " +
		"skull basement with the skeleton still attacking), so the rule as written (at most 0.5 parks per run and no deaths since the " +
		"module changed) holds from cd221b9b and not from 7a75cf7a: this promotion rests on that reading. 38 of the 39 quest stores " +
		"that hold the quest read it complete",
	"imp": "promoted on LIVE evidence: cmd/questpromote -after 2026-10-15, run 16 Oct 2026 07:45, since the module's last change " +
		"93a0d4fd (the nearest-bank kit, auto-retaliate off on kit legs and the quest-walk eat floor; first live in 8cd271b6, 16 Oct 02:10): " +
		"12 completions on 12 distinct accounts, 0 deaths and 0 lost fights, 13 parks in 34 runs (0.38 per run). The six earlier " +
		"deaths were walks to the Draynor bank the module forced, which that change removed",
	"barcrawl": "promoted on LIVE evidence: cmd/questpromote -after 2026-10-15, run 16 Oct 2026 07:45, since the module's last change " +
		"bf45f993 (draw the banked card, keep card and coins; first live in d729e10a, 15 Oct 23:09): 20 completions on 20 distinct " +
		"accounts, 0 deaths, 0.48 parks per run. Before it no card was ever issued (2,196 guard talks on 13-15 Oct)",
	"cook": "promoted on LIVE evidence: cmd/questpromote -after 2026-10-15, run 16 Oct 2026 07:45, since the module's first commit " +
		"64872a38 (first live in d4aef220, 16 Oct 04:39): 12 completions on 12 distinct accounts, 0 deaths, 0 parks per run",
	"sheep": "promoted on LIVE evidence: cmd/questpromote -after 2026-10-15, run 17 Oct 2026 06:50, since the module's first commit " +
		"64872a38 (first live in d4aef220, 16 Oct 04:39): 20 completions on 20 distinct accounts, 0 deaths, 0 parks per run",
	"gobdip": "promoted on LIVE evidence: cmd/questpromote -after 2026-10-15, run 17 Oct 2026 06:50, since the module's first commit " +
		"b2144fac (first live in d4aef220): 14 completions on 14 distinct accounts, 0 deaths, 0.46 parks per run",
	"romeo": "promoted on LIVE evidence: cmd/questpromote -after 2026-10-15, run 17 Oct 2026 06:50, since the module's first commit " +
		"b2144fac (first live in d4aef220): 16 completions on 16 distinct accounts, 0 deaths, 0 parks per run",
	"haunted": "promoted on LIVE evidence: cmd/questpromote -after 2026-10-15, run 18 Oct 2026 09:26, since the module's last change " +
		"6207f17e (Add Ernest the Chicken as an unproven quest-point filler, 17 Oct 20:19; first live in 9430e64e, 18 Oct 01:24): " +
		"23 completions on 23 distinct accounts, 0 deaths and 0 lost fights, 12 parks in 38 runs (0.32 per run)",
	"hunt": "promoted on LIVE evidence: cmd/questpromote -after 2026-10-15, run 18 Oct 2026 09:26, since the module's last change " +
		"72cc099c (merge of claude/questhunt-1017 into claude/questwave4int-1018, 18 Oct 04:49; first live in 97d93978, 18 Oct 06:32): " +
		"21 completions on 21 distinct accounts, 0 deaths and 0 lost fights, 4 parks in 38 runs (0.11 per run)",
	"drunkmonk": "promoted on LIVE evidence: cmd/questpromote -after 2026-10-15, run 19 Oct 2026 11:25, since the module's last change " +
		"ca01ac45 (merge of claude/questmonkcog-1017 into claude/questwave4int-1018, 18 Oct 05:11; first live in 97d93978, 18 Oct 06:32): " +
		"26 completions on 26 distinct accounts, 0 deaths and 0 lost fights, 4 parks in 35 runs (0.11 per run)",
	"demon": "promoted on LIVE evidence: cmd/questpromote -after 2026-10-15, run 19 Oct 2026 11:25, since the module's last change " +
		"1ce2bdcc (Send the Lumbridge cow farms in by the pasture gate and make them walk the pen, 19 Oct 00:09; first live in 30660141, " +
		"19 Oct 00:51): 16 completions on 16 distinct accounts, 0 deaths and 0 lost fights, 1 park in 23 runs (0.04 per run; 5 of the " +
		"23 ended in a timeout, which is not a park)",
	"druid": "promoted on LIVE evidence: cmd/questpromote -after 2026-10-15, run 19 Oct 2026 11:25, since the module's last change " +
		"2c00124d (Price the Taverley dungeon trip as the skeletons the route meets, both ways, with the door's suits between, 18 Oct 12:59; " +
		"first live in cc7889f4, 18 Oct 21:30): 14 completions on 14 distinct accounts, 0 deaths and 0 lost fights, 9 parks in 27 runs " +
		"(0.33 per run)",
	"junglepotion": "promoted on LIVE evidence: cmd/questpromote -after 2026-10-15, run 19 Oct 2026 11:25, since the module's last change " +
		"3259b8f5 (Add Druidic Ritual and Jungle Potion to the unproven tail of the quest ladder, 18 Oct 11:30; first live in cc7889f4, " +
		"18 Oct 21:30): 11 completions on 11 distinct accounts, 0 deaths and 0 lost fights, 1 park in 12 runs (0.08 per run)",
	"runemysteries": "cmd/runemysteriestest PASS 4/4 on a fresh account with cheats off, 21 Sep 2026 (COLLAB_ARCHIVE.md, " +
		"\"RUNE MYSTERIES: PASS, 4 CHECKS, PROVEN BY THE GATE ESSENCEMINER IS BLOCKED BY\"; docs/script_parity_status.md " +
		"\"Rune Mysteries: what was actually proven\"): 6m11s, the decide walk Duke Horacio -> Sedridor -> Aubury -> Sedridor -> done; " +
		"the essence gate observed SHUT first (Aubury's teleport answering \"You need to have completed the Rune Mysteries Quest\", " +
		"the account not moving, which is also its control: a run that starts past the gate is VOID at exit 2, not a pass), then the " +
		"tab red to green (the game's own IF_SETCOLOUR), quest points 0 -> 1 (varp 101), the journal QUEST COMPLETE, and the same " +
		"teleport landing in mapsquare 45,75 (the essence mine). Also the quest Priest in Peril's own EMPTY lab pass pip-empty-4 " +
		"(930d44ef, claude/questproof-0926) was staged with it done. Ladder use: questplan.OwnerLadder's pip rung pulls it in " +
		"just before it (bot.ownerRungPrereqs, 2 Oct 2026, claude/questfix-1002)",
}

func TestTheProvenSetMatchesItsRecordedEvidence(t *testing.T) {
	for id := range ProvenQuests {
		if provenEvidence[id] == "" {
			t.Errorf("%q is in ProvenQuests with no recorded evidence: add its passing lab pair to provenEvidence", id)
		}
		if _, ok := QuestFactories[id]; !ok {
			t.Errorf("%q is proven but has no factory", id)
		}
	}
	for id := range provenEvidence {
		if !ProvenQuests[id] {
			t.Errorf("%q has recorded evidence but is not in ProvenQuests", id)
		}
	}
	// Every registered quest outside the proven set is refused by default.
	for id := range QuestFactories {
		_, err := SelectQuests(map[string]interface{}{"quests": []interface{}{id}})
		if ProvenQuests[id] && err != nil {
			t.Errorf("proven %q refused: %v", id, err)
		}
		if !ProvenQuests[id] && err == nil {
			t.Errorf("unproven %q ran without allowUnproven", id)
		}
	}
}

// Quest ids are read as written: a long id was once dropped by the player-name normaliser and
// the ladder silently fell back to Waterfall.
func TestQuestIDsAreReadAsWritten(t *testing.T) {
	qs, err := SelectQuests(map[string]interface{}{"quests": []interface{}{"woodcutting40", "runemysteries"}, "allowUnproven": true})
	if err != nil || len(qs) != 2 || qs[0].ID() != "woodcutting40" || qs[1].ID() != "runemysteries" {
		t.Fatalf("got %v %v", qs, err)
	}
	if _, err := SelectQuests(map[string]interface{}{"quests": []interface{}{42}}); err == nil {
		t.Fatal("a non-string quest id was accepted")
	}
}
