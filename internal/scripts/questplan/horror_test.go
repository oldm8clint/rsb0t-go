package questplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// horrorJournal builds the journal lines as horror_journal.rs2 does (split on '|').
func horrorJournal(stage int, bridge, key, glass, tar, lit bool) []string {
	var t string
	switch {
	case stage == 0:
		t = "@dbl@I can start this quest by speaking to @dre@Larrissa@dbl@ at the @dre@Lighthouse@dbl@ to the @dre@North@dbl@ of the @dre@Barbarian Outpost@dbl@.|"
	case stage == 10:
		t = "@str@I travelled to an isolated lighthouse north of the Barbarian|@red@QUEST COMPLETE!"
	default:
		if stage > 1 {
			t += "@str@I repaired the bridge leading to Rellekka and got a key @str@from Gunnjorn so I could enter the lighthouse.|"
		} else {
			t += "@dbl@Larrissa is very worried about her boyfriend Jossik.|"
			if bridge {
				t += "@str@I need to repair the bridge leading to Rellekka.|"
			} else {
				t += "@dbl@I need to @dre@repair the bridge@dbl@ leading to Rellekka.|"
			}
			if key {
				t += "@dbl@I also need to use the key I got from Gunnjorn to enter @dbl@the lighthouse.|"
			} else {
				t += "@dbl@I also need to get the @dre@lighthouse key@dbl@ from her cousin|@dre@Gunnjorn@dbl@.|"
			}
		}
		if stage > 3 {
			t += "@str@I managed to repair the lighthouse light with some molten @str@glass, some swamp tar and a tinderbox.|"
		} else if stage == 2 {
			if glass {
				t += "@str@I have fixed the lighthouse lens.|"
			}
			if tar {
				t += "@str@I have re-tarred the lighthouse torch.|"
			}
			if lit {
				t += "@str@I have relit the lighthouse torch.|"
			} else {
				t += "@dbl@Now I need to find some way of @dre@fixing@dbl@ the @dre@lighthouse @dre@lamp@dbl@.|"
			}
		}
		if stage > 4 {
			t += "@str@I found Jossik in an underground cavern, behind a strange|@str@wall where he had been attacked by some sea @str@creatures."
		} else if stage == 4 {
			t += "@dbl@I must @dre@search this place@dbl@ and find out what has happened to @dbl@Larrissa's boyfriend @dre@Jossik@dbl@!"
		}
		if stage == 5 {
			t += "|@dbl@I must defeat these sea monsters to save him!"
		}
	}
	return strings.Split(t, "|")
}

func TestHorrorJournalReadsEveryStageAndFlag(t *testing.T) {
	for _, c := range []struct {
		name                         string
		stage                        int
		bridge, key, glass, tar, lit bool
		want                         HorrorFacts
	}{
		{"not started", 0, false, false, false, false, false, HorrorFacts{}},
		{"started", 1, false, false, false, false, false, HorrorFacts{Stage: 1}},
		{"key only", 1, false, true, false, false, false, HorrorFacts{Stage: 1, Key: true}},
		{"bridge only", 1, true, false, false, false, false, HorrorFacts{Stage: 1, Bridge: true}},
		{"both", 1, true, true, false, false, false, HorrorFacts{Stage: 1, Bridge: true, Key: true}},
		{"entered", 2, false, false, false, false, false, HorrorFacts{Stage: 2, Bridge: true, Key: true}},
		{"tar", 2, false, false, false, true, false, HorrorFacts{Stage: 2, Bridge: true, Key: true, Tar: true}},
		{"tar lit", 2, false, false, false, true, true, HorrorFacts{Stage: 2, Bridge: true, Key: true, Tar: true, Lit: true}},
		{"glass tar", 2, false, false, true, true, false, HorrorFacts{Stage: 2, Bridge: true, Key: true, Glass: true, Tar: true}},
		{"repaired", 4, false, false, false, false, false, HorrorFacts{Stage: 4, Bridge: true, Key: true}},
		{"junior dead", 5, false, false, false, false, false, HorrorFacts{Stage: 5, Bridge: true, Key: true}},
	} {
		n, ok := ParseHorrorJournal(horrorJournal(c.stage, c.bridge, c.key, c.glass, c.tar, c.lit))
		if !ok || DecodeHorror(n) != c.want {
			t.Errorf("%s: read %d (%+v, ok %v), want %+v", c.name, n, DecodeHorror(n), ok, c.want)
		}
	}
	if n, ok := ParseHorrorJournal(horrorJournal(10, false, false, false, false, false)); !ok || n != HorrorComplete {
		t.Errorf("complete: read %d ok %v", n, ok)
	}
	if _, ok := ParseHorrorJournal(nil); ok {
		t.Error("an empty journal must not read as a stage")
	}
}

func horrorSnap(stage int) Snapshot {
	return Snapshot{Journal: StatusInProgress, StageKnown: true, Stage: stage, Inv: map[string]int{}, InvIDs: map[int]int{},
		Worn: map[string]int{}, WornIDs: map[int]int{}, Levels: map[string]int{"agility": 35, "prayer": 43, "magic": 30, "smithing": 34},
		Failed: map[string]bool{}, Ran: map[string]bool{}, TileKnown: true, Tile: tile.Tile{X: 2725, Z: 3491}}
}

func TestHorrorDecideWalksTheQuest(t *testing.T) {
	s := horrorSnap(HorrorStarted)
	if st := DecideHorror(s); st.Kind != StepTalk || st.Stop.ID != NpcGunnjorn {
		t.Fatalf("no key: %+v, want Gunnjorn", st)
	}
	s.Levels["agility"] = 34
	if st := DecideHorror(s); st.Kind != StepWait {
		t.Fatalf("Agility 34: %+v, want a wait", st)
	}
	s = horrorSnap(HorrorFacts{Stage: 1, Key: true}.Encode())
	if st := DecideHorror(s); st.Kind != StepProvision || st.KitName != KitHorrorBridge {
		t.Fatalf("key, no planks: %+v, want the bridge kit", st)
	}
	s.InvIDs[960], s.InvIDs[ObjNails], s.InvIDs[ObjHammer] = 2, 8, 1
	if st := DecideHorror(s); st.Kind != StepCustom || st.Custom != CustomHorrorBridge {
		t.Fatalf("bridge kit held: %+v", st)
	}
	s = horrorSnap(HorrorFacts{Stage: 1, Key: true, Bridge: true}.Encode())
	if st := DecideHorror(s); st.Custom != CustomHorrorDoor {
		t.Fatalf("bridge and key: %+v, want the door", st)
	}
	s = horrorSnap(HorrorFacts{Stage: 2, Key: true, Bridge: true}.Encode())
	if st := DecideHorror(s); st.Kind != StepProvision || st.KitName != KitHorrorLight {
		t.Fatalf("entered, no light kit: %+v", st)
	}
	s.InvIDs[ObjSwampTar], s.InvIDs[ObjTinderbox], s.InvIDs[ObjMoltenGlass] = 1, 1, 1
	if st := DecideHorror(s); st.Custom != CustomHorrorLight {
		t.Fatalf("light kit held: %+v", st)
	}
	s = horrorSnap(HorrorFacts{Stage: 4, Key: true, Bridge: true}.Encode())
	if st := DecideHorror(s); st.Kind != StepProvision || st.KitName != KitHorrorFight {
		t.Fatalf("repaired: %+v, want the fight kit", st)
	}
	has := map[int]bool{}
	for _, l := range horrorFightKit(HorrorRepaired) {
		has[l.ID] = true
	}
	if !has[ObjBronzeDagger] || !has[ObjBronzeArrow] {
		t.Fatalf("the stage 4 kit lacks the wall's weapon or arrow")
	}
	s.Ran["provision:"+KitHorrorFight] = true
	s.WornIDs[ObjStaffOfAir] = 1
	if st := DecideHorror(s); st.Custom != CustomHorrorDown {
		t.Fatalf("kitted: %+v, want down", st)
	}
	s.Tile = tile.Tile{X: 2516, Z: 4636}
	if st := DecideHorror(s); st.Custom != CustomHorrorJunior {
		t.Fatalf("in the cavern at 4: %+v", st)
	}
	s.Stage = HorrorFacts{Stage: 5, Key: true, Bridge: true}.Encode()
	if st := DecideHorror(s); st.Custom != CustomHorrorMother {
		t.Fatalf("in the cavern at 5: %+v", st)
	}
	s.Tile = tile.Tile{X: 2725, Z: 3491}
	s.Levels["prayer"] = 31
	if st := DecideHorror(s); st.Kind != StepWait {
		t.Fatalf("stage 5 at Prayer 31 outside: %+v, want a wait", st)
	}
}

func TestHorrorPostQuestPocketLeaves(t *testing.T) {
	s := horrorSnap(0)
	s.Journal = StatusComplete
	s.Tile = tile.Tile{X: 2515, Z: 10001, Level: 1}
	if st := DecideHorror(s); st.Custom != CustomHorrorLeave {
		t.Fatalf("complete in the pocket: %+v", st)
	}
	s.Tile = tile.Tile{X: 2725, Z: 3491}
	if st := DecideHorror(s); st.Kind != StepDone {
		t.Fatalf("complete outside: %+v", st)
	}
}

func TestMotherElements(t *testing.T) {
	for id, want := range map[int]string{NpcMotherAir: "Wind", NpcMotherWater: "Water", NpcMotherEarth: "Earth", NpcMotherFire: "Fire", NpcMotherRanged: "", NpcMotherMelee: ""} {
		if got := MotherElement(id); got != want {
			t.Errorf("form %d: %q, want %q", id, got, want)
		}
	}
}
