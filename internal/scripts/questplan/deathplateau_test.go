package questplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Pages assembled as death_journal.rs2 builds them for a given pair of tracks ("|" breaks are
// line breaks), so each case is the literal text the server would render.
func deathPage(equip, mp int) []string {
	var t string
	if mp == MapScouted {
		t = "@str@I have found another way up Death Plateau for Denulth."
	} else {
		t = "@dbl@I have offered to help @dre@Denulth@dbl@ by finding @dre@another way@dbl@ up|@dre@Death Plateau."
	}
	if equip < DeathUnlocked {
		t += "@dbl@ I also need to find the @dre@combination@dbl@ to the|@dre@equipment room@dbl@ and @dre@unlock@dbl@ the door.|"
	} else {
		t += "|@str@I have found the combination to the equipment room and|@str@unlocked the door.|"
	}
	if equip >= DeathEohric2 {
		t += "|@str@The equipment room guard is staying at the local inn, the @str@Toad and Chicken. The guard wouldn't talk to me!|"
	}
	if equip == DeathEohric2 {
		t += "@dre@Eohric@dbl@ says to buy the @dre@guard@dbl@ a @dre@drink"
	} else if equip >= DeathAle {
		t += "@str@I bought the guard a drink and he seemed more helpful."
	}
	if equip == DeathHarold {
		t += "|@str@The equipment room guard is staying at the local inn, the Toad @str@and Chicken. The guard wouldn't talk to me!"
	} else if equip == DeathEohric {
		t += "|@dre@Eohric@dbl@ said that the @dre@equipment room guard@dbl@ is staying at @dbl@the local inn, the @dre@Toad and Chicken"
	}
	if equip >= DeathIOU {
		t += " I @str@gambled with the guard until he ran out of money. He wrote @str@me an IOU.|"
	}
	if equip >= DeathCombo {
		t += "|@str@It turned out the IOU was written on the back of the|@str@combination!"
	}
	if equip >= DeathUnlocked {
		t += " I put the stone balls in the right places on the|@str@stone mechanism and unlocked the door."
	}
	switch {
	case mp >= MapSupplies:
		t += "||@str@I found the sherpa's house. I gave Tenzing the ten loaves|@str@of bread, ten cooked trout and the Spiked boots."
		if mp == MapScouted {
			t += "Tenzing|@str@has given me a map of the secret way. I checked the @str@secret way|@str@and it is safe for the Imperial Guard to use."
		} else if mp == MapGotMap {
			t += "Tenzing|@str@has given me a map of the secret way.|@dbl@I need to @dre@check@dbl@ that the @dre@secret way@dbl@ is @dre@safe@dbl@ for the @dre@Imperial|Guard@dbl@ to use."
		}
		t += "||@str@I have given Dunstan the certificate to prove that his son|@str@has been signed up for the Imperial Guard. Dunstan|@str@made me the Spiked boots."
	case mp >= MapTenzing:
		t += "||@str@I found the sherpa's house.||@dre@Tenzing@dbl@ will show me a @dre@secret way@dbl@ up Death Plateau if I get|him some items:"
		t += "|@dre@Ten loaves of bread.|@dre@Ten cooked trout.|@dre@Spiked boots."
		t += "||@dre@Tenzing@dbl@ gave me his @dre@climbing boots@dbl@, I need to take them to|@dre@Dunstan@dbl@ in @dre@Burthorpe@dbl@ for the @dre@spikes"
		if mp == MapGaveCert {
			t += "||@str@I have given Dunstan the certificate to prove that his son|@str@has been signed up for the Imperial Guard.|@dbl@I will need an @dre@Iron bar@dbl@ for the boots"
			break
		}
		if mp >= MapSmithy {
			t += "||@dre@Dunstan@dbl@ will help me if I get his @dre@son signed up@dbl@ for the @dre@Imperial Guard@dbl@. I will need an @dre@Iron bar@dbl@ for the boots"
		}
		if mp == MapCert {
			t += "||@dre@Denulth@dbl@ gave me a @dre@certificate@dbl@ to prove that Dunstan's son|has been signed up for the Imperial Guard."
		}
	case mp == MapSaba:
		t += "||@dre@Saba@dbl@ says that there is a @dre@sherpa@dbl@ living @dre@nearby@dbl@ that may|know another way up Death Plateau."
	}
	return strings.Split(t, "|")
}

func TestParseDeathPlateauJournalEveryPair(t *testing.T) {
	equips := []int{DeathStarted, DeathEohric, DeathHarold, DeathEohric2, DeathAle, DeathIOU, DeathCombo, DeathUnlocked}
	maps := []int{0, MapSaba, MapTenzing, MapSmithy, MapCert, MapGaveCert, MapGotMap, MapScouted}
	for _, e := range equips {
		for _, m := range maps {
			got, ok := ParseDeathPlateauJournal(deathPage(e, m))
			ge, gm := DeathTracks(got)
			if !ok || ge != e || gm != m {
				t.Errorf("equip %d map %d: read equip %d map %d (ok %v)", e, m, ge, gm, ok)
			}
		}
	}
	if st, ok := ParseDeathPlateauJournal([]string{"@dbl@I can start this quest by speaking to @dre@Denulth@dbl@ who is in his"}); !ok || st != 0 {
		t.Errorf("not started: %d %v", st, ok)
	}
	if st, ok := ParseDeathPlateauJournal([]string{"@str@I'm now an honorary member of the Imperial Guard!", "", "@red@QUEST COMPLETE!"}); !ok || st != DeathStage(DeathComplete, MapScouted) {
		t.Errorf("complete: %d %v", st, ok)
	}
}

func TestDeathDiceStakes(t *testing.T) {
	// purse+1 until the cap: 101, then 202 after a loss, 404, 808, then 1000.
	purse := HaroldPurseStart
	var stakes []int
	for i := 0; i < 5; i++ {
		s := NextDeathStake(purse, 5000)
		stakes = append(stakes, s)
		purse = DeathPurseAfter(purse, s, false)
	}
	want := []int{101, 202, 404, 808, 1000}
	for i := range want {
		if stakes[i] != want[i] {
			t.Fatalf("stakes %v, want %v", stakes, want)
		}
	}
	if 101+202+404+808 != DiceBankroll {
		t.Fatalf("the bankroll %d is not four straight losses", DiceBankroll)
	}
	if NextDeathStake(100, 50) != 50 {
		t.Fatalf("the stake must not exceed the pack")
	}
}

func TestDeathPlateauSteps(t *testing.T) {
	snap := func(equip, mp int) Snapshot {
		return Snapshot{Journal: StatusInProgress, Stage: DeathStage(equip, mp), StageKnown: true,
			InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: 20, BankKnown: true, BankIDs: map[int]int{},
			Ran: map[string]bool{}, Failed: map[string]bool{}, Tile: DenulthAnchor, TileKnown: true}
	}
	with := func(s Snapshot, ids ...int) Snapshot {
		for _, id := range ids {
			s.InvIDs[id] = 1
		}
		return s
	}
	trout := func(s Snapshot) Snapshot { s.InvIDs[ObjTrout] = 10; return s }
	banked := func(s Snapshot, ids ...int) Snapshot {
		for _, id := range ids {
			s.BankIDs[id] = 1
		}
		return s
	}
	unread := func(s Snapshot) Snapshot { s.BankKnown, s.BankIDs = false, nil; return s }
	full := func(s Snapshot) Snapshot { s.FreeSlots = 0; return s }
	blocked := func(s Snapshot, kit string) Snapshot { s.Blocked = map[string]bool{"provision:" + kit: true}; return s }
	ns := snap(0, 0)
	ns.Journal = StatusNotStarted
	rich := snap(DeathAle, 0)
	rich.InvIDs[CoinsID] = 2000
	supplies := with(snap(DeathUnlocked, MapGaveCert), ObjSpikedBoots)
	supplies.InvIDs[2309], supplies.InvIDs[ObjTrout] = 10, 10
	cave := snap(DeathUnlocked, MapSaba)
	cave.Tile = SabaInside
	cases := []struct {
		name  string
		snap  Snapshot
		kind  StepKind
		check func(Step) bool
	}{
		{"not started: Denulth", ns, StepTalk, func(s Step) bool { return s.Stop.ID == NpcDenulth }},
		{"10: Eohric", snap(DeathStarted, 0), StepTalk, func(s Step) bool { return s.Stop.ID == NpcEohric }},
		{"20: Harold", snap(DeathEohric, 0), StepCustom, func(s Step) bool { return s.Custom == CustomDeathHaroldTalk }},
		{"30: Eohric again", snap(DeathHarold, 0), StepTalk, func(s Step) bool { return s.Stop.ID == NpcEohric }},
		{"40 without ale: the ale kit", snap(DeathEohric2, 0), StepProvision, func(s Step) bool { return s.KitName == KitDeathAle }},
		{"40 with ale: Harold", with(snap(DeathEohric2, 0), ObjAsgarnianAle), StepCustom, func(s Step) bool { return s.Custom == CustomDeathAle }},
		{"50 poor: the bankroll", snap(DeathAle, 0), StepProvision, func(s Step) bool { return s.KitName == KitDeathDice }},
		{"50 rich: the dice", rich, StepCustom, func(s Step) bool { return s.Custom == CustomDeathDice }},
		{"55 with IOU: read it", with(snap(DeathIOU, 0), ObjDeathIOU), StepCustom, func(s Step) bool { return s.Custom == CustomDeathReadIOU }},
		{"55 with nothing: reclaim", snap(DeathIOU, 0), StepCustom, func(s Step) bool { return s.Custom == CustomDeathReclaim }},
		{"60: the balls", with(snap(DeathCombo, 0), ObjCombination), StepCustom, func(s Step) bool { return s.Custom == CustomDeathBalls }},
		{"60 with the combination banked: the balls, the paper is for Denulth", banked(snap(DeathCombo, 0), ObjCombination), StepCustom, func(s Step) bool { return s.Custom == CustomDeathBalls }},
		{"55 with the IOU banked: withdraw it", banked(snap(DeathIOU, 0), ObjDeathIOU), StepWithdraw, func(s Step) bool { return s.Items[0].ID == ObjDeathIOU }},
		{"55 with nothing in the pack and the bank unread: read the bank first", unread(snap(DeathIOU, 0)), StepScanBank, func(s Step) bool { return s.BankSet }},
		{"70 map 0: Saba", snap(DeathUnlocked, 0), StepCustom, func(s Step) bool { return s.Custom == CustomDeathSaba }},
		{"in the cave: out", cave, StepCustom, func(s Step) bool { return s.Custom == CustomDeathSaba }},
		{"map 1: Tenzing", snap(DeathUnlocked, MapSaba), StepCustom, func(s Step) bool { return s.Custom == CustomDeathTenzing }},
		{"map 2: Dunstan", snap(DeathUnlocked, MapTenzing), StepTalk, func(s Step) bool { return s.Stop.ID == NpcDunstan }},
		{"map 3: Denulth's certificate", snap(DeathUnlocked, MapSmithy), StepTalk, func(s Step) bool { return s.Stop.ID == NpcDenulth }},
		{"map 4 with the certificate: Dunstan", with(snap(DeathUnlocked, MapCert), ObjCertificate), StepTalk, func(s Step) bool { return s.Stop.ID == NpcDunstan }},
		{"map 5 without trout: the trout kit, before anything else", with(snap(DeathUnlocked, MapGaveCert), ObjClimbingBoots), StepProvision, func(s Step) bool { return s.KitName == KitDeathTrout }},
		{"map 5 with trout, no pair: a spare pair first", trout(with(snap(DeathUnlocked, MapGaveCert), ObjClimbingBoots)), StepProvision, func(s Step) bool { return s.KitName == KitDeathBoots }},
		{"map 5 with trout, no spare pair and no bar anywhere: parked for the bar", blocked(trout(with(snap(DeathUnlocked, MapGaveCert), ObjClimbingBoots)), KitDeathBoots), StepWait, func(s Step) bool { return s.ParkFor == DeathBarRetry }},
		{"map 5 with trout, no spare pair but a bar: the climbing boots, then Dunstan", blocked(trout(with(snap(DeathUnlocked, MapGaveCert), ObjIronBar)), KitDeathBoots), StepCustom, func(s Step) bool { return s.Custom == CustomDeathTenzing && s.ExpectItemID == ObjClimbingBoots }},
		{"map 5 with bar and boots: Dunstan", trout(with(snap(DeathUnlocked, MapGaveCert), ObjClimbingBoots, ObjIronBar)), StepTalk, func(s Step) bool { return s.Stop.ID == NpcDunstan }},
		{"map 5 spiked, trout, no bread: the bread kit", trout(with(snap(DeathUnlocked, MapGaveCert), ObjSpikedBoots)), StepProvision, func(s Step) bool { return s.KitName == KitDeathBread }},
		{"map 5 with everything: Tenzing", supplies, StepCustom, func(s Step) bool { return s.Custom == CustomDeathTenzing && s.ExpectItemID == ObjSecretMap }},
		{"map 7 with the map: scout", with(snap(DeathUnlocked, MapGotMap), ObjSecretMap), StepCustom, func(s Step) bool { return s.Custom == CustomDeathScout }},
		{"both done: Denulth", with(snap(DeathUnlocked, MapScouted), ObjSecretMap, ObjCombination), StepTalk, func(s Step) bool { return s.Stop.ID == NpcDenulth }},
		{"both done, no combination: reclaim", with(snap(DeathUnlocked, MapScouted), ObjSecretMap), StepCustom, func(s Step) bool { return s.Custom == CustomDeathReclaim && s.ExpectItemID == ObjCombination }},
		{"both done, combination banked: withdraw it, never reclaim", banked(with(snap(DeathUnlocked, MapScouted), ObjSecretMap), ObjCombination), StepWithdraw, func(s Step) bool { return s.Items[0].ID == ObjCombination }},
		{"both done, combination missing and the bank unread: read it", unread(with(snap(DeathUnlocked, MapScouted), ObjSecretMap)), StepScanBank, func(s Step) bool { return s.BankSet }},
		{"both done, no combination and a full pack: bank first, the re-issue would be dropped", full(with(snap(DeathUnlocked, MapScouted), ObjSecretMap)), StepDeposit, func(s Step) bool { return s.BankSet }},
		{"map 5 with trout and the pair banked: the boots kit draws it, no bar and no Tenzing", banked(trout(snap(DeathUnlocked, MapGaveCert)), ObjSpikedBoots), StepProvision, func(s Step) bool { return s.KitName == KitDeathBoots }},
		{"map 5 with trout and the pair, no bread: the bread kit asks for ten", trout(with(snap(DeathUnlocked, MapGaveCert), ObjSpikedBoots)), StepProvision, func(s Step) bool { return s.KitName == KitDeathBread && s.Kit[0].Qty == 10 }},
		{"map 1 with a full pack: bank first, Tenzing's boots would be dropped", full(snap(DeathUnlocked, MapSaba)), StepDeposit, func(s Step) bool { return s.BankSet }},
		{"map 4 without the certificate, banked: withdraw it", banked(snap(DeathUnlocked, MapCert), ObjCertificate), StepWithdraw, func(s Step) bool { return s.Items[0].ID == ObjCertificate }},
		{"map 7 without the map, both banked: one withdrawal", banked(snap(DeathUnlocked, MapGotMap), ObjSecretMap, ObjCombination), StepWithdraw, func(s Step) bool { return len(s.Items) == 2 }},
	}
	for _, c := range cases {
		st := DecideDeathPlateau(c.snap)
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed: %s", c.name, why)
		}
		if st.Kind != c.kind || !c.check(st) {
			t.Errorf("%s: got %s", c.name, st.Describe())
		}
	}
}

func TestStoneBallLayoutIsTheContentOne(t *testing.T) {
	want := map[int]tile.Tile{3110: {X: 2894, Z: 3562}, 3111: {X: 2895, Z: 3562}, 3109: {X: 2894, Z: 3563},
		3112: {X: 2895, Z: 3563}, 3113: {X: 2895, Z: 3564}}
	seen := map[tile.Tile]bool{}
	for _, b := range StoneBalls {
		if want[b.ID] != b.Target || seen[b.Target] {
			t.Fatalf("ball %d (%s) at %v", b.ID, b.Colour, b.Target)
		}
		seen[b.Target] = true
	}
}
