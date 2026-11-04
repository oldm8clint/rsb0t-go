package questplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// fremJournal builds the in-progress journal as viking_journal.rs2 does, from the votes, each
// trial's status and the chain position (0 for none shown).
func fremJournal(votes int, status [fremTrials]int, chain int) []string {
	t := "@str@I spoke to Brundt the Fremennik chieftan, and he told me|@str@that I could join their tribe if I could pass some trials|@str@and get seven of the twelve council members votes.|"
	switch votes {
	case 7:
		t += "@dbl@I have @dre@seven votes@dbl@! I should go speak to @dre@Brundt@dbl@ again|"
	case 0:
		t += "@dbl@I don't have any votes yet|@dbl@I should try and find some of the @dre@council members|"
	case 1:
		t += "@dbl@I have @dre@one@dbl@ vote so far|"
	default:
		t += "@dbl@I have @dre@" + []string{"", "", "two", "three", "four", "five", "six"}[votes] + "@dbl@ votes so far|"
	}
	line := func(tr FremTrial, done, started string) {
		switch status[tr] {
		case TrialDone:
			t += done
		case TrialStarted:
			t += started
		default:
			t += "|"
		}
	}
	line(TrialSwensen, "@str@I now have the Navigator's vote at the council.|",
		"@dbl@The @dre@Navigator@dbl@ will vote for me if I can pass his trial|@dbl@I need to get from one end of his @dre@maze@dbl@ to the other|")
	if status[TrialSigmund] == TrialStarted {
		t += "@dbl@The @dre@Merchant@dbl@ will vote for me if I can pass his trial.|@dbl@Someone in this town has a @dre@rare flower@dbl@ that he wants.|"
	} else {
		t += "|"
	}
	switch chain {
	case 3:
		t += "@dbl@The @dre@bard@dbl@ is looking for some @dre@new boots@dbl@...||"
	case 9:
		t += "|@dbl@The @dre@navigator@dbl@ is looking for a @dre@weather forecast@dbl@...||"
	case 13:
		t += "|@dbl@All @dre@Askeladden@dbl@ wants is @dre@some money@dbl@!||"
	}
	if status[TrialSigmund] == TrialDone {
		t += "@str@I now have the Merchant's vote at the council.|"
	}
	line(TrialSigli, "@str@I now have the Hunter's vote at the council.|",
		"@dbl@The @dre@Hunter@dbl@ will vote for me if I can pass his trial.|@dbl@I need to @dre@track and defeat@dbl@ a creature called the @dre@Draugen|")
	line(TrialPeer, "@str@I now have the Seer's vote at the council|",
		"@dbl@The @dre@Seer@dbl@ will vote for me if I can pass his trial|@dbl@I need to get from @dre@one side@dbl@ of his house to @dre@the other|")
	line(TrialThorvald, "@str@I now have the Warrior's vote at the council|",
		"@dbl@The @dre@Warrior@dbl@ will vote for me if I can pass his trial|@dbl@I need to fight @dre@Koschei@dbl@ to the @dre@death@dbl@ in @dre@unarmed combat|")
	line(TrialManni, "@str@I now have the Reveller's vote at the council|",
		"@dbl@The @dre@Reveller@dbl@ will vote for me if I can pass his trial|@dbl@I need to defeat him in a @dre@drinking contest@dbl@ somehow!|")
	line(TrialOlaf, "@str@I now have the Bard's vote at the council.|",
		"@dbl@The @dre@Bard@dbl@ will vote for me if I can pass his trial|@dbl@I must make myself @dre@a lyre@dbl@ and play it in the @dre@longhall|")
	return strings.Split(t, "|")
}

func TestFremennikJournalReadsVotesTrialsAndChain(t *testing.T) {
	var st [fremTrials]int
	st[TrialManni], st[TrialOlaf], st[TrialSigli] = TrialDone, TrialDone, TrialStarted
	st[TrialSigmund] = TrialStarted
	for _, c := range []struct {
		votes, chain int
		status       [fremTrials]int
	}{
		{0, 0, [fremTrials]int{}},
		{2, 3, st},
		{2, 9, st},
		{2, 13, st},
		{7, 0, [fremTrials]int{2, 2, 2, 2, 2, 2, 2}},
	} {
		n, ok := ParseFremennikJournal(fremJournal(c.votes, c.status, c.chain))
		want := FremFacts{Votes: c.votes, Status: c.status, Chain: c.chain}
		if !ok || DecodeFremennik(n) != want {
			t.Errorf("votes %d chain %d: read %+v (ok %v), want %+v", c.votes, c.chain, DecodeFremennik(n), ok, want)
		}
	}
	start := "@dbl@I can start this quest by speaking to @dre@Chieftain Brundt@dbl@ in the @dre@Fremennik Longhall@dbl@, which is in the town of @dre@Rellekka@dbl@ to the north of @dre@Sinclair Mansion@dbl@.|"
	if n, ok := ParseFremennikJournal(strings.Split(start, "|")); !ok || n != 0 {
		t.Errorf("not started: %d %v", n, ok)
	}
	if n, ok := ParseFremennikJournal([]string{"@red@QUEST COMPLETE!"}); !ok || n != FremComplete {
		t.Errorf("complete: %d %v", n, ok)
	}
}

func TestFremennikPackRoundTrips(t *testing.T) {
	f := FremFacts{Votes: 5, Status: [fremTrials]int{2, 2, 2, 2, 2, 1, 0}, Chain: 11}
	if got := DecodeFremennik(f.Encode()); got != f {
		t.Fatalf("round trip %+v -> %+v", f, got)
	}
}

func fremSnap(f FremFacts) Snapshot {
	return Snapshot{Journal: StatusInProgress, StageKnown: true, Stage: f.Encode(), Inv: map[string]int{}, InvIDs: map[int]int{},
		Worn: map[string]int{}, WornIDs: map[int]int{}, Levels: map[string]int{}, Failed: map[string]bool{}, Ran: map[string]bool{},
		TileKnown: true, Tile: tile.Tile{X: 2659, Z: 3665}}
}

func TestFremennikDecideRunsTheTrialsInOrder(t *testing.T) {
	s := fremSnap(FremFacts{})
	if st := DecideFremennik(s); st.Kind != StepProvision || st.KitName != KitFremManni {
		t.Fatalf("nothing started: %+v, want Manni's kit before his talk", st)
	}
	s.Ran["provision:"+KitFremManni] = true
	if st := DecideFremennik(s); st.Kind != StepTalk || st.Stop.ID != NpcManni {
		t.Fatalf("Manni kitted: %+v, want his trial talk", st)
	}
	f := FremFacts{Votes: 0}
	f.Status[TrialManni] = TrialStarted
	s = fremSnap(f)
	s.BankKnown, s.BankIDs = true, map[int]int{} // a bank read this run; Manni's kegs have their own test
	if st := DecideFremennik(s); st.Kind != StepProvision || st.KitName != KitFremManni {
		t.Fatalf("Manni started: %+v, want his kit", st)
	}
	s.Ran["provision:"+KitFremManni] = true
	if st := DecideFremennik(s); st.Custom != CustomFremManni {
		t.Fatalf("Manni kitted: %+v", st)
	}
	f = FremFacts{Votes: 4, Status: [fremTrials]int{2, 2, 2, 2, 0, 0, 0}}
	s = fremSnap(f)
	if st := DecideFremennik(s); st.Kind != StepProvision || st.KitName != KitFremSigmund {
		t.Fatalf("four done: %+v, want Sigmund's kit", st)
	}
	s.Ran["provision:"+KitFremSigmund] = true
	if st := DecideFremennik(s); st.Kind != StepTalk || st.Stop.ID != NpcSigmund {
		t.Fatalf("four done, kitted: %+v, want Sigmund", st)
	}
	f = FremFacts{Votes: 7, Status: [fremTrials]int{2, 2, 2, 2, 2, 2, 2}}
	if st := DecideFremennik(fremSnap(f)); st.Kind != StepTalk || st.Stop.ID != NpcBrundt {
		t.Fatalf("seven votes: %+v, want Brundt", st)
	}
	s = fremSnap(FremFacts{Votes: 6, Status: [fremTrials]int{2, 2, 2, 2, 2, 2, 1}})
	s.Tile = tile.Tile{X: 2667, Z: 3692, Level: 1}
	if st := DecideFremennik(s); st.Custom != CustomFremLoft {
		t.Fatalf("on the loft: %+v", st)
	}
}

func TestMerchantNextFollowsGoodsThenTheChain(t *testing.T) {
	s := fremSnap(FremFacts{})
	if npc, give := MerchantNext(s, 0, false); npc != NpcSailor || give {
		t.Fatalf("chain 1: %d %v", npc, give)
	}
	if npc, _ := MerchantNext(s, 0, true); npc != NpcOlaf {
		t.Fatalf("chain 2: %d", npc)
	}
	if npc, _ := MerchantNext(s, 13, false); npc != NpcAskeladden {
		t.Fatalf("chain 13: %d", npc)
	}
	s.InvIDs[ObjNote2] = 1
	if npc, give := MerchantNext(s, 13, false); npc != NpcThora || !give {
		t.Fatalf("holding note 2: %d %v", npc, give)
	}
	s.InvIDs = map[int]int{ObjRareFlower: 1}
	if npc, _ := MerchantNext(s, 0, false); npc != NpcSigmund {
		t.Fatalf("holding the flower: %d", npc)
	}
	// Every chain position has an NPC, and every good has a taker.
	for c := 1; c <= 13; c++ {
		if MerchantAsk[c] == 0 {
			t.Errorf("chain %d has no NPC", c)
		}
	}
	if len(MerchantGive) != len(fremChainGoods) {
		t.Errorf("%d goods have takers, %d goods exist", len(MerchantGive), len(fremChainGoods))
	}
}

// A trial's kit is fetched at the Seers bank and the trial is in Rellekka, across the basalt rocks: the kit comes before
// the trial's first word, so the way over them is walked once, not three times (live 14 Oct 2026: Olaf's start talk, then
// the Seers bank for the kit, then Rellekka again).
func TestEveryTrialsKitComesBeforeItsFirstWord(t *testing.T) {
	for _, tr := range []FremTrial{TrialManni, TrialOlaf, TrialSigli, TrialSigmund, TrialThorvald} {
		kit := fremKits[tr]
		if kit == "" {
			t.Fatalf("trial %d has no kit", tr)
		}
		var f FremFacts
		for p := FremTrial(0); p < tr; p++ {
			f.Status[p] = TrialDone
			f.Votes++
		}
		s := fremSnap(f)
		s.InvIDs[ObjIronAxe] = 1 // an axe held: no scan of the bank
		if st := DecideFremennik(s); st.Kind != StepProvision || st.KitName != kit {
			t.Errorf("trial %d not started: %+v, want its kit %s first", tr, st, kit)
		}
		s.Ran["provision:"+kit] = true
		if st := DecideFremennik(s); st.Kind != StepTalk {
			t.Errorf("trial %d kitted, not started: %+v, want the start talk", tr, st)
		}
		f.Status[tr] = TrialStarted
		s = fremSnap(f)
		s.BankKnown, s.BankIDs = true, map[int]int{} // Manni's started trial reads the bank for its kegs first
		s.Ran["provision:"+kit] = true
		if st := DecideFremennik(s); st.Kind != StepCustom || st.Custom != fremCustoms[tr] {
			t.Errorf("trial %d started and kitted: %+v, want its custom step", tr, st)
		}
	}
}

func olafTurn() FremFacts {
	return FremFacts{Votes: 1, Status: [fremTrials]int{TrialManni: TrialDone}}
}

func olafKit(t *testing.T, s Snapshot) []KitLine {
	t.Helper()
	st := DecideFremennik(s)
	if st.Kind != StepProvision || st.KitName != KitFremOlaf {
		t.Fatalf("step %+v, want Olaf's kit", st)
	}
	return st.Kit
}

func kitLine(kit []KitLine, id int) (KitLine, bool) {
	for _, l := range kit {
		if l.ID == id {
			return l, true
		}
	}
	return KitLine{}, false
}

// The swaying tree takes any axe the Woodcutting level can use. Olaf's kit asked for an iron axe by id, with only the bank
// and the mule to take it from (and the mule holds no iron axe): "1 Iron axe: the mule publishes none; no ground, skill or
// shop source listed", 13 of 18 failed rounds on 14 Oct 2026.
func TestOlafsKitTakesTheAxeTheAccountOwnsAndBuysAnIronOneOtherwise(t *testing.T) {
	s := fremSnap(olafTurn())
	s.Levels["woodcutting"] = 40
	// The bank is not read: read it, an axe in it is the kit's axe.
	if st := DecideFremennik(s); st.Kind != StepScanBank || st.Bank != SeersBank {
		t.Fatalf("no axe and an unread bank: %+v, want the Seers bank read", st)
	}
	// A steel axe in the bank is the axe, with no shop behind it.
	s.BankKnown, s.BankIDs = true, map[int]int{ObjSteelAxe: 1}
	if l, ok := kitLine(olafKit(t, s), ObjSteelAxe); !ok || len(l.Sources) != 0 {
		t.Errorf("banked steel axe: %+v %v", l, ok)
	}
	if _, ok := kitLine(olafKit(t, s), ObjIronAxe); ok {
		t.Error("an iron axe is asked for beside the banked steel one")
	}
	// Nothing in the bank: an iron axe, from Aemad if the bank and the mule have none.
	s.BankIDs = map[int]int{}
	l, ok := kitLine(olafKit(t, s), ObjIronAxe)
	if !ok || len(l.Sources) != 1 || l.Sources[0].Kind != SourceShop || l.Sources[0].Shop.NpcID != AemadShop.NpcID {
		t.Errorf("no axe anywhere: %+v %v, want an iron axe with Aemad's shop", l, ok)
	}
	// An axe in the pack (or worn): the kit asks for none, and the bank is not read for one.
	s = fremSnap(olafTurn())
	s.InvIDs[ObjAdamantAxe] = 1
	kit := olafKit(t, s)
	for _, a := range Axes {
		if _, ok := kitLine(kit, a); ok {
			t.Errorf("axe %d is in the kit of an account that holds an adamant axe", a)
		}
	}
	// The knife has its ground spawn beside Rellekka, and the shark nothing but the bank and the mule.
	if l, ok := kitLine(kit, ObjKnife); !ok || len(l.Sources) != 1 || l.Sources[0].Tile != (tile.Tile{X: 2681, Z: 3717}) {
		t.Errorf("knife line %+v %v", l, ok)
	}
	if l, ok := kitLine(kit, ObjRawShark); !ok || len(l.Sources) != 0 {
		t.Errorf("raw shark line %+v %v", l, ok)
	}
}
