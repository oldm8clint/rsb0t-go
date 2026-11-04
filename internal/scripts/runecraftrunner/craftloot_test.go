package runecraftrunner

import (
	"os"
	"testing"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/runecraftplan"
)

// THE LOOT PANEL'S RUNECRAFT CARD IS FED FROM Craft ITSELF (a user request: "runecrafting loot panel is needed"),
// because this is where a craft is known to have happened. These run the real Craft over the simulated engine of
// runner_test.go and read what it handed to Deps.Loot.

func sink(into *[]loot.Drop) func(loot.Drop) { return func(d loot.Drop) { *into = append(*into, d) } }

func bodyAltarMatch() locindex.Match {
	b := runecraftplan.Body
	return locindex.Match{Loc: locindex.Loc{ID: b.AltarID, X: b.AltarAt.X, Z: b.AltarAt.Z, Shape: 10}, Name: "Altar", Op: 1}
}

// A verified craft reports ONE drop: the altar's row, 54 runes (two an essence at level 11), the 27 essence it bound as a
// spend, the 135 experience the stat gained and the level read afterwards.
func TestAVerifiedCraftReportsItsRunesEssenceAndExperience(t *testing.T) {
	_, d := newEngine(t, runecraftplan.Air, airKit(27), runecraftplan.Air.Enter, 11)
	var drops []loot.Drop
	d.Loot = sink(&drops)
	res := Craft(d, runecraftplan.Air, airAltarMatch())
	if !res.OK() || res.Runes != 54 {
		t.Fatalf("the craft: %+v", res)
	}
	if len(drops) != 1 {
		t.Fatalf("got %d drops, want 1: %+v", len(drops), drops)
	}
	dr := drops[0]
	if dr.Events != 1 || dr.XP != 135 {
		t.Errorf("events %d xp %d, want 1 and 135", dr.Events, dr.XP)
	}
	if len(dr.Items) != 1 || dr.Items[0].ID != runecraftplan.Air.RuneID || dr.Items[0].Count != 54 || dr.Items[0].Name != "Air rune" {
		t.Errorf("items %+v, want 54 Air rune", dr.Items)
	}
	if len(dr.Spent) != 1 || dr.Spent[0].ID != runecraftplan.EssenceID || dr.Spent[0].Count != 27 {
		t.Errorf("spent %+v, want 27 Rune essence", dr.Spent)
	}
	if s := dr.Source; !loot.IsRunecraft(s) || s.Name != "Air altar" || s.ID != runecraftplan.Air.RuneID || s.Level != 11 {
		t.Errorf("source %+v", s)
	}
}

// The level it reports is the one read AFTER the craft, so a craft that levels up is the new level.
func TestACraftThatLevelsUpReportsTheNewLevel(t *testing.T) {
	e2, d := newEngine(t, runecraftplan.Air, airKit(27), runecraftplan.Air.Enter, 19)
	e2.mu.Lock()
	e2.xpT = (4470 - 10) * 10
	e2.setStat()
	e2.mu.Unlock()
	var drops []loot.Drop
	d.Loot = sink(&drops)
	if res := Craft(d, runecraftplan.Air, airAltarMatch()); !res.OK() || res.Level1 != 20 {
		t.Fatalf("the craft: %+v", res)
	}
	if len(drops) != 1 || drops[0].Source.Level != 20 {
		t.Fatalf("drops %+v, want one at level 20", drops)
	}
}

// Another altar is another row, drawn with its own rune.
func TestABodyCraftIsTheBodyAltarsRow(t *testing.T) {
	b := runecraftplan.Body
	_, d := newEngine(t, b, append([]actionsim.Stack(nil), essence(27)...), b.Enter, 20)
	var drops []loot.Drop
	d.Loot = sink(&drops)
	if res := Craft(d, b, bodyAltarMatch()); !res.OK() {
		t.Fatalf("the craft: %+v", res)
	}
	if len(drops) != 1 || drops[0].Source.Name != "Body altar" || drops[0].Source.ID != b.RuneID || drops[0].Items[0].ID != b.RuneID || drops[0].XP != 27*b.XP10/10 {
		t.Fatalf("drops %+v", drops)
	}
	if loot.Runecrafting("Body", b.RuneID).Key() != drops[0].Source.Key() {
		t.Error("the altar's row is not the one the card builds for it")
	}
}

// What was not a craft reports nothing: no essence, the game's refusal, and essence that went with no experience.
func TestACraftThatDidNotHappenReportsNothing(t *testing.T) {
	var drops []loot.Drop
	_, d := newEngine(t, runecraftplan.Air, []actionsim.Stack{{ID: int32(runecraftplan.Air.TalismanID), Count: 1}}, runecraftplan.Air.Enter, 1)
	d.Loot = sink(&drops)
	if res := Craft(d, runecraftplan.Air, airAltarMatch()); res.OK() {
		t.Fatalf("an empty pack crafted: %+v", res)
	}
	b := runecraftplan.Body
	_, d2 := newEngine(t, b, append([]actionsim.Stack(nil), essence(27)...), b.Enter, 5)
	d2.Loot = sink(&drops)
	if res := Craft(d2, b, bodyAltarMatch()); res.OK() {
		t.Fatalf("a level too low crafted: %+v", res)
	}
	e3, d3 := newEngine(t, runecraftplan.Air, airKit(27), runecraftplan.Air.Enter, 1)
	e3.noXP = true
	d3.Loot = sink(&drops)
	if res := Craft(d3, runecraftplan.Air, airAltarMatch()); res.OK() {
		t.Fatalf("an unverified craft counted: %+v", res)
	}
	if len(drops) != 0 {
		t.Errorf("reported %+v", drops)
	}
}

// A craft whose first click was swallowed by a delay and sent again is still ONE craft.
func TestACraftClickedTwiceIsReportedOnce(t *testing.T) {
	e, d := newEngine(t, runecraftplan.Air, airKit(27), runecraftplan.Air.Enter, 1)
	e.ignoreFirstAltar = true
	var drops []loot.Drop
	d.Loot = sink(&drops)
	if res := Craft(d, runecraftplan.Air, airAltarMatch()); !res.OK() || res.Clicks != 2 {
		t.Fatalf("the craft: %+v", res)
	}
	if len(drops) != 1 {
		t.Errorf("got %d drops, want 1", len(drops))
	}
}

// On the real item config the names and the unit costs come from the cache, and the ids the card uses are those of
// Rune essence and the three runes (needs RSB0T_CONFIG_JAG; skips without it).
func TestTheCraftsItemsAreTheCachesOwn(t *testing.T) {
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("set RSB0T_CONFIG_JAG")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range runecraftplan.Altars() {
		if o, ok := cfg.Obj(a.RuneID); !ok || o.Name != a.Rune {
			t.Errorf("%s: obj %d is %q (known %v), want %q", a.Name, a.RuneID, o.Name, ok, a.Rune)
		}
	}
	if o, ok := cfg.Obj(loot.ObjRuneEssence); !ok || o.Name != loot.RuneEssenceName {
		t.Errorf("obj %d is %q (known %v), want %q", loot.ObjRuneEssence, o.Name, ok, loot.RuneEssenceName)
	}
	_, d := newEngine(t, runecraftplan.Air, airKit(27), runecraftplan.Air.Enter, 11)
	var drops []loot.Drop
	d.Loot, d.Config = sink(&drops), cfg
	if res := Craft(d, runecraftplan.Air, airAltarMatch()); !res.OK() {
		t.Fatalf("the craft: %+v", res)
	}
	if len(drops) != 1 || drops[0].Items[0].Name != "Air rune" || drops[0].Items[0].Value <= 0 || drops[0].Spent[0].Name != "Rune essence" {
		t.Fatalf("drops %+v", drops)
	}
}
