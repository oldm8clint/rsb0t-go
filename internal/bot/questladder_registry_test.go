package bot

import (
	"os"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// N0 of docs/quest_selfsufficiency_plan_2026-10-19.md. The engines the bots build (QuestLadder's and ScarabSwarm's) read a
// prerequisite their ladder does not list through questRegistry, the quest factories: the tail's Scorpion Catcher and Horror from
// the Deep wait for the Barcrawl, which the head runs, and the Barcrawl has no quest tab to read.

func TestTheQuestRegistryBuildsTheQuestsTheFactoriesDo(t *testing.T) {
	reg := questRegistry(nil)
	q, ok := reg(" Barcrawl ")
	if !ok || q.ID() != "barcrawl" || q.Done == nil {
		t.Fatalf("the Barcrawl from the registry: ok %v, id %q, Done set %v", ok, q.ID(), q.Done != nil)
	}
	if _, ok := reg("nosuchquest"); ok {
		t.Error("the registry built a quest nothing registered")
	}
	for id, f := range QuestFactories {
		if got, ok := reg(id); !ok || got.ID() != f(nil).ID() {
			t.Errorf("registry(%q) = %q, %v", id, got.ID(), ok)
		}
	}
}

// Whatever a quest waits for that has no tab must be readable through the registry: the Barcrawl is the one today, and a
// miniquest added as a prerequisite tomorrow fails here until it has a factory and a Done of its own.
func TestEveryTablessPrerequisiteIsInTheRegistry(t *testing.T) {
	reg := questRegistry(nil)
	wanted := map[string]string{}
	for id, f := range questplan.LadderFacts {
		for _, p := range f.Prereqs {
			wanted[p] = id
		}
		for _, p := range f.SoftPrereqs {
			wanted[p.ID] = id
		}
	}
	for id, f := range QuestFactories {
		for _, p := range f(nil).Module.Record.Requirements.Quests {
			wanted[p] = id
		}
	}
	for p, by := range wanted {
		if questrunner.HasQuestTab(questplan.QuestTabName(p)) {
			continue
		}
		if q, ok := reg(p); !ok || q.Done == nil {
			t.Errorf("%s waits for %s, which has no quest tab and no registry quest with a Done (registered %v, Done set %v)", by, p, ok, q.Done != nil)
		}
	}
	if _, ok := wanted["barcrawl"]; !ok {
		t.Error("nothing waits for the Barcrawl any more: this test, and N0, no longer guard anything")
	}
}

// Both engines the bots build carry the registry. The constructions need a live Context, so the source says it.
func TestBothQuestEnginesTakeTheRegistry(t *testing.T) {
	for _, file := range []string{"questladder.go"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		src := strings.ReplaceAll(string(raw), "\r\n", "\n")
		i := strings.Index(src, "&questrunner.Engine{")
		if i < 0 {
			t.Fatalf("%s builds no questrunner.Engine", file)
		}
		end := strings.Index(src[i:], "}\n")
		if end < 0 || !strings.Contains(src[i:i+end], "QuestFor: questRegistry(ctx.Settings)") {
			t.Errorf("%s: the engine it builds does not take QuestFor: questRegistry(ctx.Settings)", file)
		}
	}
}
