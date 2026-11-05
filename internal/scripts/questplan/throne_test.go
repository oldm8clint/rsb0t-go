package questplan

import (
	"reflect"
	"testing"
)

func TestThroneChainIsOrderedByItsNeeds(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range ThroneChain {
		for _, n := range s.Needs {
			if !seen[n] {
				t.Errorf("%s needs %s, which does not come before it", s.ID, n)
			}
		}
		seen[s.ID] = true
	}
	if _, ok := ThroneStageOf(ThroneID); !ok {
		t.Fatal("the chain ends at the Throne")
	}
	got := ThroneRunnable()
	want := []string{"zanaris", MerlinsCrystalID, DragonSlayerID, FremennikID, ArravID}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runnable %v", got)
	}
	if !reflect.DeepEqual(ThroneUnbuilt(), []string{HeroID, ThroneID}) {
		t.Fatalf("unbuilt %v", ThroneUnbuilt())
	}
}

func TestThroneNext(t *testing.T) {
	next, blocked := ThroneNext(map[string]bool{}, 0)
	if next != "zanaris" {
		t.Fatalf("a bare account starts with Lost City: %q", next)
	}
	if blocked[HeroID] == "" || blocked[ThroneID] == "" {
		t.Fatalf("hero and the throne are blocked: %v", blocked)
	}
	done := map[string]bool{"zanaris": true, MerlinsCrystalID: true, FremennikID: true}
	next, blocked = ThroneNext(done, 32)
	if next != DragonSlayerID {
		t.Fatalf("dragon next, got %q", next)
	}
	if blocked[ArravID] != "" {
		t.Fatalf("arrav has a module now and is not blocked: %q", blocked[ArravID])
	}
	done[DragonSlayerID] = true
	next, _ = ThroneNext(done, 40)
	if next != ArravID {
		t.Fatalf("arrav is the next built stage, got %q", next)
	}
	done[ArravID] = true
	next, blocked = ThroneNext(done, 40)
	if next != "" {
		t.Fatalf("with the built stages done nothing is runnable, got %q", next)
	}
	if blocked[HeroID] == "" {
		t.Fatal("hero is still blocked: it has no module")
	}
	_, blocked = ThroneNext(map[string]bool{"zanaris": true, MerlinsCrystalID: true, DragonSlayerID: true, ArravID: true}, 50)
	if blocked[HeroID] != "needs 55 quest points (have 50)" {
		t.Fatalf("%v", blocked[HeroID])
	}
}

func TestUnholdDragon(t *testing.T) {
	if ok, _ := UnholdDragon(MeleeFloor(54, 46, 50, 56), 50); ok {
		t.Fatal("strength 46 holds the floor under 50")
	}
	if ok, why := UnholdDragon(MeleeFloor(54, 52, 50, 56), 50); !ok || why == "" {
		t.Fatal("floor 50 passes")
	}
}

func TestIceQueenFit(t *testing.T) {
	ok := IceQueenFit{Mining: 50, Pickaxe: true, HP: 60, MaxHP: 60, Food: 25, FoodHeal: 12, Magic: 75, BestSpellHit: 16, Prayer: 43, MaxDeaths: 1}
	if good, why := ok.Check(); !good {
		t.Fatalf("a prepared account fits: %s", why)
	}
	for name, mod := range map[string]func(f *IceQueenFit){
		"mining":   func(f *IceQueenFit) { f.Mining = 49 },
		"pickaxe":  func(f *IceQueenFit) { f.Pickaxe = false },
		"prayer":   func(f *IceQueenFit) { f.Prayer = 42 },
		"nospell":  func(f *IceQueenFit) { f.BestSpellHit = 0 },
		"death":    func(f *IceQueenFit) { f.Dead = 1 },
		"nofood":   func(f *IceQueenFit) { f.Food = 1 },
		"noheal":   func(f *IceQueenFit) { f.FoodHeal = 0 },
		"weakcast": func(f *IceQueenFit) { f.BestSpellHit = 2; f.Food = 10 },
	} {
		f := ok
		mod(&f)
		if good, why := f.Check(); good || why == "" {
			t.Errorf("%s: should not fit, reason %q", name, why)
		}
	}
	if !IceQueenStop(30, 60) || IceQueenStop(45, 60) {
		t.Fatal("stop rule: under half of 60 (30) or under 36")
	}
	if !IceQueenStop(35, 40) {
		t.Fatal("a small hitpoint pool stops under a maximum hit and a half")
	}
}

func TestArravPairNeedsOneOfEachGang(t *testing.T) {
	pairs, odd := MakePairs([]string{"a", "b", "b", "c", "d", "e", ""})
	if len(pairs) != 2 || pairs[0] != (Pair{"a", "b"}) || pairs[1] != (Pair{"c", "d"}) || len(odd) != 1 || odd[0] != "e" {
		t.Fatalf("%v %v", pairs, odd)
	}
	if p, g, ok := PartnerOf(pairs, "d"); !ok || p != "c" || g != GangBlackArm {
		t.Fatal("partner of d")
	}
	if _, _, ok := PartnerOf(pairs, "e"); ok {
		t.Fatal("e is unpaired")
	}
	if s, _ := ArravNext(ArravMe{Gang: GangPhoenix, Stage: 9, HasHalf: true}, ArravPartnerView{Gang: GangPhoenix, Present: true}, 0, 10); s != ArravStop {
		t.Fatalf("two of one gang stop: %v", s)
	}
	if s, _ := ArravNext(ArravMe{}, ArravPartnerView{}, 0, 10); s != ArravStop {
		t.Fatal("no gang stops")
	}
}

func TestArravRolesNeverDeadlock(t *testing.T) {
	ph := ArravMe{Gang: GangPhoenix, Stage: PhoenixJoined, HasHalf: true}
	ba := ArravMe{Gang: GangBlackArm, Stage: BlackArmJoined, HasHalf: true}
	here := ArravPartnerView{Present: true, HasHalf: true}
	if s, _ := ArravNext(ph, here, 0, 10); s != ArravTakeCert {
		t.Fatalf("phoenix waits for the half: %v", s)
	}
	if s, _ := ArravNext(ba, here, 0, 10); s != ArravGiveHalf {
		t.Fatalf("black arm gives its half: %v", s)
	}
	ph.HasBoth = true
	if s, _ := ArravNext(ph, ArravPartnerView{Present: true}, 0, 10); s != ArravHandIn {
		t.Fatalf("phoenix with both halves hands in: %v", s)
	}
	ph.HasBoth, ph.HasCert = false, true
	if s, _ := ArravNext(ph, ArravPartnerView{}, 0, 10); s != ArravClaimReward {
		t.Fatalf("a certificate goes to the king: %v", s)
	}
	if s, _ := ArravNext(ArravMe{Gang: GangPhoenix, Stage: PhoenixComplete}, ArravPartnerView{}, 0, 10); s != ArravDone {
		t.Fatal("done")
	}
}

func TestArravWaitIsBounded(t *testing.T) {
	me := ArravMe{Gang: GangBlackArm, Stage: BlackArmJoined, HasHalf: true}
	if s, _ := ArravNext(me, ArravPartnerView{}, 3, 10); s != ArravWaitPartner {
		t.Fatal("waits")
	}
	if s, why := ArravNext(me, ArravPartnerView{}, 10, 10); s != ArravStop || why == "" {
		t.Fatalf("an endless wait is a stop: %v %q", s, why)
	}
	if s, _ := ArravNext(ArravMe{Gang: GangPhoenix, Stage: 3}, ArravPartnerView{}, 0, 10); s != ArravDoSolo {
		t.Fatal("not yet joined means solo errands")
	}
	if s, _ := ArravNext(ArravMe{Gang: GangPhoenix, Stage: PhoenixJoined}, ArravPartnerView{}, 0, 10); s != ArravFetchHalf {
		t.Fatal("a joined account without the half fetches it")
	}
}

func TestHeroPairReady(t *testing.T) {
	all := map[string]bool{"zanaris": true, MerlinsCrystalID: true, DragonSlayerID: true, ArravID: true}
	if ok, _ := HeroPairReady(all, all, 60, 55); !ok {
		t.Fatal("ready")
	}
	one := map[string]bool{"zanaris": true, MerlinsCrystalID: true, DragonSlayerID: true}
	if ok, _ := HeroPairReady(all, one, 60, 60); ok {
		t.Fatal("one account lacks arrav")
	}
	if ok, why := HeroPairReady(all, all, 60, 54); ok || why == "" {
		t.Fatal("quest points")
	}
}
