package actions

import (
	"bytes"
	"testing"
)

func TestAutocastButtonsAreTheContentComponents(t *testing.T) {
	f := newFakeSession()
	if err := OpenAutocastMenu(f); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.lastSent(), ifButtonBytes(353)) {
		t.Fatalf("open sent %x, want IF_BUTTON 353 (combat_staff_2:auto_choose)", f.lastSent())
	}

	fire, ok := SpellByName("Fire Strike")
	if !ok {
		t.Fatal("Fire Strike must be in the table")
	}
	if err := ChooseAutocastSpell(f, fire); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.lastSent(), ifButtonBytes(1833)) {
		t.Fatalf("choose sent %x, want IF_BUTTON 1833 (staff_spells:ssb3)", f.lastSent())
	}

	if err := ToggleAutocast(f); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.lastSent(), ifButtonBytes(349)) {
		t.Fatalf("toggle sent %x, want IF_BUTTON 349 (combat_staff_2:auto_toggle)", f.lastSent())
	}
}

// %attackstyle_magic is 2 once a spell is chosen and 3 once autocasting is actually on, so a
// spell chosen but never toggled must NOT read as enabled — the distinction content's own
// player_autocast_enabled makes.
func TestAutocastEnabledMatchesContentsOwnTest(t *testing.T) {
	f := newFakeSession()
	if AutocastEnabled(f) {
		t.Fatal("nothing set must not read as autocasting")
	}

	// Deliberately WITHOUT setting varp 59: it is never transmitted on content 289 (proven
	// live — see actions.AutocastSpell's note), so every reader here must work from
	// attackstyle_magic alone.
	setVarp(f.state, VarpAttackStyleMagic, 2)
	if AutocastEnabled(f) {
		t.Fatal("a spell chosen but not toggled on must not read as autocasting")
	}
	if !AutocastSpellChosen(f) {
		t.Fatal("attackstyle_magic 2 means a spell is stored")
	}

	setVarp(f.state, VarpAttackStyleMagic, 3)
	if !AutocastEnabled(f) {
		t.Fatal("attackstyle_magic 3 must read as autocasting, with varp 59 silent as it always is")
	}

	setVarp(f.state, VarpAttackStyleMagic, 0)
	if AutocastEnabled(f) || AutocastSpellChosen(f) {
		t.Fatal("attackstyle_magic 0 means no spell and no autocasting")
	}
}

// varp 59 never arrives on this content build, so the spell-name reader must report "unknown"
// rather than inventing an answer — and nothing else may depend on it.
func TestAutocastSpellIsUnreadableOnThisBuild(t *testing.T) {
	f := newFakeSession()
	setVarp(f.state, VarpAttackStyleMagic, 3)
	if _, ok := AutocastSpell(f); ok {
		t.Fatal("with varp 59 never transmitted, the spell must read as unknown")
	}
	if !AutocastEnabled(f) {
		t.Fatal("autocast must still read as on without it")
	}
}

func TestAutocastSpellReadsTheVarp(t *testing.T) {
	f := newFakeSession()
	if _, ok := AutocastSpell(f); ok {
		t.Fatal("no spell set must report false")
	}
	// 51 is wind_strike's real content id — deliberately not 0, and not its button order.
	setVarp(f.state, VarpAutocastSpell, 51)
	s, ok := AutocastSpell(f)
	if !ok || s.Name != "Wind Strike" {
		t.Fatalf("got %+v, %v; want Wind Strike", s, ok)
	}
}

func TestSpellTableMatchesContent(t *testing.T) {
	all := AutocastSpells()
	if len(all) != 16 {
		t.Fatalf("got %d spells, want 16 (ssb0..ssb15)", len(all))
	}
	for i, s := range all {
		if want := 1830 + i; s.Button != want {
			t.Errorf("%s: button %d, want %d", s.Name, s.Button, want)
		}
	}
	// The four Waves are the only members-only spells in this content build.
	for _, s := range all {
		wantMembers := s.Name == "Wind Wave" || s.Name == "Water Wave" || s.Name == "Earth Wave" || s.Name == "Fire Wave"
		if s.Members != wantMembers {
			t.Errorf("%s: members = %v, want %v", s.Name, s.Members, wantMembers)
		}
	}
	// Spot-check the two ids most likely to be wrong if anyone ever guesses them.
	wind, _ := SpellByName("Wind Strike")
	if wind.ID != 51 {
		t.Errorf("Wind Strike id = %d, want 51 (spells.constant, deliberately not 0)", wind.ID)
	}
	fireWave, _ := SpellByName("Fire Wave")
	if fireWave.ID != 15 || fireWave.Level != 75 {
		t.Errorf("Fire Wave = id %d level %d, want id 15 level 75", fireWave.ID, fireWave.Level)
	}
}

func TestBestAutocastSpell(t *testing.T) {
	cases := []struct {
		name     string
		level    int
		element  string
		members  bool
		want     string
		wantNone bool
	}{
		{"level 1 free", 1, "", false, "Wind Strike", false},
		{"level 13 free picks the strongest strike", 13, "", false, "Fire Strike", false},
		{"level 41 free reaches Wind Blast", 41, "", false, "Wind Blast", false},
		{"level 75 free stops at Fire Blast", 75, "", false, "Fire Blast", false},
		{"level 75 members reaches Fire Wave", 75, "", true, "Fire Wave", false},
		{"element filter", 40, "earth", false, "Earth Bolt", false},
		{"below every spell", 0, "", false, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := BestAutocastSpell(c.level, c.element, c.members)
			if c.wantNone {
				if ok {
					t.Fatalf("got %s, want nothing castable", got.Name)
				}
				return
			}
			if !ok || got.Name != c.want {
				t.Fatalf("got %q (%v), want %q", got.Name, ok, c.want)
			}
		})
	}
}

// A free world must never be offered a Wave, which is the whole point of the members flag.
func TestBestAutocastSpellNeverOffersAWaveOnAFreeWorld(t *testing.T) {
	for level := 1; level <= 99; level++ {
		s, ok := BestAutocastSpell(level, "", false)
		if ok && s.Members {
			t.Fatalf("level %d: offered members-only %s on a free world", level, s.Name)
		}
	}
}

func TestIsNoRunesLine(t *testing.T) {
	// The exact line content builds in check_spell_requirements, with the rune interpolated.
	if !IsNoRunesLine("You do not have enough Fire Runes to cast this spell.") {
		t.Fatal("the server's own out-of-runes line must match")
	}
	if !IsNoRunesLine("you do not have enough air runes") {
		t.Fatal("matching is case-insensitive, as the TypeScript original is")
	}
	if IsNoRunesLine("You do not have enough money.") {
		t.Fatal("an unrelated 'not enough' line must not match")
	}
	if IsNoRunesLine("") {
		t.Fatal("empty must not match")
	}
}

func TestIsSpellLevelAndMembersLines(t *testing.T) {
	if !IsSpellLevelLine("Your Magic level is not high enough for this spell.") {
		t.Fatal("the level refusal must match")
	}
	if !IsMembersSpellLine("You need to be on a members' server to use this spell.") {
		t.Fatal("the members refusal must match")
	}
	if IsSpellLevelLine("You do not have enough Fire Runes to cast this spell.") {
		t.Fatal("the rune refusal is a different line")
	}
}
