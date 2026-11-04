package questmods

import (
	"os"
	"strings"
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The Titan fight's spell table is questplan's (pure, no actions import); it must say what the
// client's autocast table says, spell for spell: every non-members spell of the staff list, with the
// same level and the same runes, castable by name as a manual cast.
func TestTitanSpellsMatchTheAutocastTable(t *testing.T) {
	have := map[string]questplan.TitanSpell{}
	for _, s := range questplan.TitanSpells {
		have[s.Name] = s
	}
	for _, a := range actions.AutocastSpells() {
		s, ok := have[a.Name]
		if a.Members {
			if ok {
				t.Errorf("%s is a members' spell and is in the Titan table", a.Name)
			}
			continue
		}
		if !ok {
			t.Errorf("%s (level %d) is missing from the Titan table", a.Name, a.Level)
			continue
		}
		if s.Level != a.Level {
			t.Errorf("%s: level %d, the client says %d", a.Name, s.Level, a.Level)
		}
		if len(s.Runes) != len(a.Runes) {
			t.Errorf("%s: runes %v, the client says %v", a.Name, s.Runes, a.Runes)
			continue
		}
		for _, ar := range a.Runes {
			found := false
			for _, sr := range s.Runes {
				if strings.EqualFold(sr.Name, ar.Name) && sr.Count == ar.Count {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: no %d %s in %v", a.Name, ar.Count, ar.Name, s.Runes)
			}
		}
		if _, ok := actions.CombatSpellComponent(a.Name); !ok {
			t.Errorf("%s has no spellbook component for a manual cast", a.Name)
		}
		delete(have, a.Name)
	}
	for name := range have {
		t.Errorf("%s is in the Titan table and not in the client's", name)
	}
	// The rune ids are the objects the config names.
	pp := os.Getenv("RSB0T_CONFIG_JAG")
	if pp == "" {
		return
	}
	cfg, err := cachecfg.LoadFile(pp)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range questplan.TitanSpells {
		for _, r := range s.Runes {
			found := false
			for _, id := range cfg.ObjIDs(r.Name) {
				found = found || id == r.ID
			}
			if !found {
				t.Errorf("%s: %s is not object %d in the config (%v)", s.Name, r.Name, r.ID, cfg.ObjIDs(r.Name))
			}
		}
	}
}

// Every custom step the plan can ask for has an executor, and the Titan's executor set is complete.
func TestHolyGrailRegistersEveryCustomStep(t *testing.T) {
	q := HolyGrail()
	for _, c := range []string{
		questplan.CustomGrailMerlin, questplan.CustomGrailEntrana, questplan.CustomGrailLeaveEntrana, questplan.CustomGrailWhistles,
		questplan.CustomGrailRealm, questplan.CustomGrailLeaveRealm, questplan.CustomGrailPercival, questplan.CustomGrailTake,
	} {
		if q.Customs[c] == nil {
			t.Errorf("no executor for %q", c)
		}
	}
	if q.Module.ExclusiveDone == nil {
		t.Error("the Titan is a one-at-a-time step")
	}
}

// What the server answers a second caster with, and what is not that.
func TestTitanContendedLines(t *testing.T) {
	for line, want := range map[string]bool{
		"Someone else is fighting that.":      true,
		"someone else is fighting that.":      true,
		"I'm already under attack.":           true,
		"You do not have enough Chaos runes.": false,
		"Your Magic level is not high enough": false,
		"":                                    false,
	} {
		if got := titanContended.MatchString(line); got != want {
			t.Errorf("%q: %v", line, got)
		}
	}
	if firstLine("a\nb") != "a" || firstLine("a") != "a" || firstLine("") != "" {
		t.Error("firstLine")
	}
}

// The cast tiles the live fight builds from the real pack, loc index and config.
func TestTitanGroundOnTheRealPack(t *testing.T) {
	pp, ip, cp := os.Getenv("RSB0T_COLLISION_PACK"), os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if pp == "" || ip == "" || cp == "" {
		t.Skip("RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX or RSB0T_CONFIG_JAG not set")
	}
	pk, err := pathfind.Load(pp)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := locindex.Load(ip)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	if sight, cands := titanGround(nil); sight != nil || cands != nil {
		t.Fatal("ground without deps")
	}
	d := &questrunner.Deps{Pack: pk, Locs: ix, Config: cfg}
	titanGroundData.built = false
	sight, cands := titanGround(d)
	if sight == nil || len(cands) < 30 {
		t.Fatalf("%d cast tiles", len(cands))
	}
	want := tile.Tile{X: questplan.TitanTile.X + questplan.TitanCastPrefer, Z: questplan.TitanTile.Z}
	if cands[0].Tile != want {
		t.Errorf("first cast tile %v, want %v", cands[0].Tile, want)
	}
	// From the arrival tile the stand is on the account's side, a short walk off.
	stand, ok := questplan.ChooseTitanStand(questplan.TitanArrival, cands)
	if !ok || !questplan.OnTitanSide(stand) || tile.Chebyshev(stand, questplan.TitanArrival) > 12 {
		t.Errorf("stand from the arrival tile: %v %v", stand, ok)
	}
	for _, c := range cands {
		if tile.Chebyshev(c.Tile, questplan.TitanTile) < 2 {
			t.Errorf("a cast tile %v within his reach", c.Tile)
		}
	}
	// Cached: a second call answers the same.
	if _, again := titanGround(d); len(again) != len(cands) {
		t.Error("the cached ground changed")
	}
}
