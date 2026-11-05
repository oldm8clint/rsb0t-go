package questplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func arravSnap(g Gang, stage int, inv map[int]int) Snapshot {
	s := Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: inv, FreeSlots: 20,
		Failed: map[string]bool{}, Ran: map[string]bool{}, Tile: tile.Tile{X: 3210, Z: 3430},
		Pair: PairState{Known: true, Partner: "other", Gang: g}}
	if inv == nil {
		s.InvIDs = map[int]int{}
	}
	return s
}

func TestParseArravJournalReadsBothGangs(t *testing.T) {
	cases := []struct {
		text  string
		stage int
	}{
		{"I can start this quest by speaking to @dre@Reldo@dbl@ in @dre@Varrock's|Palace Library", 0},
		{"@dre@Reldo@dbl@ says there is a @dre@quest@dbl@ hidden in one of the books in|his library", 1},
		{"I read about a valuable shield. I should ask @dre@Reldo@dbl@ in the palace library if he knows anything more about this.", 2},
		{"Reldo told me that the fur trader in Varrock, named Baraek, knows about the Phoenix Gang.", 3},
		{"I read about a valuable shield. Baraek told me that the 'Phoenix Gang' have a hideout in the south-eastern part of Varrock, disguising themselves as the VTAM Corporation", 4},
		{"Baraek told me ... south-eastern part of Varrock. I spoke to Straven in the Phoenix Gang Headquarters", 8},
		{"I spoke to Straven ... gave this Report to Straven and he allowed me to join the Phoenix Gang. I can now search", 9},
		{"I took my Certificate to King Roald. @red@QUEST COMPLETE! Straven", 10},
		{"To start this quest, I spoke to the Tramp in Varrock. He directed me to speak to Katrine in the Gang Headquarters.", 101},
		{"I spoke to Katrine in the Black Arm Gang Headquarters. She offered to let me join the gang in return for stealing two Phoenix Crossbows", 102},
		{"I gave them to Katrine and she allowed me to join the Black Arm Gang. I can now search the Gang Headquarters", 103},
		{"Katrine allowed me to join the Black Arm Gang. QUEST COMPLETE!", 104},
	}
	for _, c := range cases {
		got, ok := ParseArravJournal([]string{c.text})
		if !ok || got != c.stage {
			t.Errorf("%q -> %d, %v; want %d", c.text[:30], got, ok, c.stage)
		}
	}
	if _, ok := ParseArravJournal(nil); ok {
		t.Error("an empty journal must not read as a stage")
	}
}

func TestPairAssignIsSymmetricAndMakePairsRule(t *testing.T) {
	ga, oka := PairAssign("Zed", "alice")
	gb, okb := PairAssign("alice", "Zed")
	if !oka || !okb || ga != GangBlackArm || gb != GangPhoenix {
		t.Fatalf("zed %v alice %v", ga, gb)
	}
	if _, ok := PairAssign("x", "X"); ok {
		t.Error("an account cannot partner itself")
	}
	if _, ok := PairAssign("x", ""); ok {
		t.Error("no partner is no pair")
	}
}

func TestArravWithoutAPartnerWaitsAnHourAndStartsNothing(t *testing.T) {
	s := arravSnap(GangNone, 0, nil)
	s.Pair = PairState{}
	s.Journal = StatusNotStarted
	st := DecideArrav(s)
	if st.Kind != StepWait || st.ParkFor == 0 || !strings.Contains(st.Reason, "stage5ThroneChainPartner") {
		t.Fatalf("%+v", st)
	}
}

func TestPhoenixRouteInOrder(t *testing.T) {
	steps := []struct {
		stage int
		inv   map[int]int
		want  StepKind
		note  string
	}{
		{0, nil, StepTalk, "Reldo"},
		{1, nil, StepInteractLoc, "the bookcase"},
		{1, map[int]int{ObjArravBook: 1}, StepOpHeld, "read it"},
		{2, nil, StepTalk, "Reldo"},
		{3, nil, StepProvision, "coins"},
		{3, map[int]int{CoinsID: 20}, StepTalk, "Baraek"},
		{4, nil, StepCustom, "down"},
		{8, nil, StepCustom, "Jonny"},
		{8, map[int]int{ObjArravReport: 1}, StepCustom, "down to hand the report in"},
		{9, nil, StepCustom, "the chest"},
		{9, map[int]int{ObjArravShield1: 1}, StepCustom, "meet"},
		{9, map[int]int{ObjArravCert: 2}, StepCustom, "meet and give one away"},
		{9, map[int]int{ObjArravCert: 1}, StepTalk, "King Roald"},
	}
	for _, c := range steps {
		s := arravSnap(GangPhoenix, c.stage, c.inv)
		if c.stage == 0 {
			s.Journal = StatusNotStarted
		}
		st := DecideArrav(s)
		if st.Kind != c.want {
			t.Errorf("stage %d (%s): %+v", c.stage, c.note, st)
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("stage %d: invalid step %q", c.stage, why)
		}
	}
	// In the cellar the Straven talks are talks, not another descent.
	s := arravSnap(GangPhoenix, 4, nil)
	s.Tile = tile.Tile{X: 3245, Z: 9781}
	if st := DecideArrav(s); st.Kind != StepTalk || st.Stop.Npc != "Straven" {
		t.Errorf("in the cellar: %+v", st)
	}
	// On the way to Jonny from the cellar the account climbs out first.
	s = arravSnap(GangPhoenix, 8, nil)
	s.Tile = tile.Tile{X: 3245, Z: 9781}
	if st := DecideArrav(s); st.Custom != CustomArravSurface {
		t.Errorf("out of the cellar first: %+v", st)
	}
}

func TestBlackArmRouteInOrder(t *testing.T) {
	steps := []struct {
		stage int
		inv   map[int]int
		want  StepKind
		cust  string
	}{
		{0, nil, StepTalk, ""},
		{101, nil, StepTalk, ""},
		{102, nil, StepCustom, CustomArravKey},
		{102, map[int]int{ObjArravKey: 1}, StepCustom, CustomArravCross},
		{102, map[int]int{ObjArravCrossbow: 2}, StepTalk, ""},
		{103, nil, StepCustom, CustomArravBaHalf},
		{103, map[int]int{ObjArravShield2: 1}, StepCustom, CustomArravMeet},
		{103, map[int]int{ObjArravCert: 1}, StepTalk, ""},
	}
	for _, c := range steps {
		s := arravSnap(GangBlackArm, c.stage, c.inv)
		if c.stage == 0 {
			s.Journal = StatusNotStarted
		}
		st := DecideArrav(s)
		if st.Kind != c.want || (c.cust != "" && st.Custom != c.cust) {
			t.Errorf("stage %d inv %v: %+v", c.stage, c.inv, st)
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("stage %d: invalid step %q", c.stage, why)
		}
	}
}

func TestAFailedMeetingParksInsteadOfLooping(t *testing.T) {
	s := arravSnap(GangBlackArm, 103, map[int]int{ObjArravShield2: 1})
	s.Failed["custom:"+CustomArravMeet] = true
	if st := DecideArrav(s); st.Kind != StepWait || st.ParkFor == 0 {
		t.Fatalf("%+v", st)
	}
}

func TestArravNextHandsTheSecondCertificateBack(t *testing.T) {
	me := ArravMe{Gang: GangPhoenix, Stage: PhoenixJoined, HasCert: true, Certs: 2}
	if st, _ := ArravNext(me, ArravPartnerView{Present: true, Gang: GangBlackArm}, 0, 10); st != ArravGiveCert {
		t.Errorf("partner here: %v", st)
	}
	if st, _ := ArravNext(me, ArravPartnerView{Gang: GangBlackArm}, 0, 10); st != ArravWaitPartner {
		t.Errorf("partner away: %v", st)
	}
	// A Black Arm account that gave its half waits for the certificate, not for another half.
	ba := ArravMe{Gang: GangBlackArm, Stage: BlackArmJoined}
	if st, _ := ArravNext(ba, ArravPartnerView{Present: true, Gang: GangPhoenix, HasBoth: true}, 0, 10); st != ArravTakeCert {
		t.Errorf("gave its half: %v", st)
	}
	// One certificate is the King's.
	if st, _ := ArravNext(ArravMe{Gang: GangPhoenix, Stage: PhoenixJoined, HasCert: true, Certs: 1}, ArravPartnerView{}, 0, 10); st != ArravClaimReward {
		t.Errorf("one certificate: %v", st)
	}
}
