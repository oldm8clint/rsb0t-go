package combatplan

import "testing"

func TestMetalTierLevels(t *testing.T) {
	cases := []struct {
		tier  string
		level int
	}{
		{"Bronze", 1}, {"Iron", 1}, {"Steel", 5}, {"Black", 10},
		{"Mithril", 20}, {"Adamant", 30}, {"Rune", 40}, {"Dragon", 60},
	}
	for _, c := range cases {
		got, ok := MetalTierLevel(c.tier)
		if !ok || got != c.level {
			t.Errorf("%s = %d (%v), want %d", c.tier, got, ok, c.level)
		}
	}
	if _, ok := MetalTierLevel("Granite"); ok {
		t.Error("Granite is not a metal tier in this table")
	}
}

// Weapons go by Attack and armour by Defence: the same number, a different stat. A table that
// got this backwards would still pass a levels-only check, so assert the skill explicitly.
func TestWeaponsNeedAttackAndArmourNeedsDefence(t *testing.T) {
	scim, ok := ItemByName("Rune scimitar")
	if !ok || scim.Skill != SkillAttack || scim.Level != 40 {
		t.Fatalf("Rune scimitar = %+v, want Attack 40", scim)
	}
	body, ok := ItemByName("Rune platebody")
	if !ok || body.Skill != SkillDefence || body.Level != 40 {
		t.Fatalf("Rune platebody = %+v, want Defence 40", body)
	}
}

func TestBestWearableByLevel(t *testing.T) {
	cases := []struct {
		name  string
		slot  Slot
		lv    Levels
		want  string
		wantN bool
	}{
		{"fresh account weapon", SlotWeapon, Levels{Attack: 1}, "Bronze scimitar", false},
		{"steel at 5", SlotWeapon, Levels{Attack: 5}, "Steel scimitar", false},
		{"mithril at 20", SlotWeapon, Levels{Attack: 20}, "Mithril scimitar", false},
		{"rune at 40", SlotWeapon, Levels{Attack: 40}, "Rune scimitar", false},
		{"armour follows Defence", SlotBody, Levels{Defence: 30}, "Adamant chainbody", false},
		{"no Attack, no weapon", SlotWeapon, Levels{Attack: 0}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := BestWearable(c.slot, c.lv, nil)
			if c.wantN {
				if ok {
					t.Fatalf("got %s, want nothing wearable", got.Name)
				}
				return
			}
			if !ok || got.Name != c.want {
				t.Fatalf("got %q (%v), want %q", got.Name, ok, c.want)
			}
		})
	}
}

// The headline fact for the trainer: shops stop short of rune for almost everything, so the
// best weapon a shop will sell at 40 Attack is NOT the best weapon at 40 Attack.
func TestShopsStopShortOfTheBestGear(t *testing.T) {
	at40 := Levels{Attack: 40, Defence: 40}

	best, _ := BestWearable(SlotWeapon, at40, nil)
	if best.Name != "Rune scimitar" {
		t.Fatalf("best wearable weapon = %q, want Rune scimitar", best.Name)
	}
	if best.Shop != "" {
		t.Fatalf("Rune scimitar claims shop %q — no shop in this content build stocks it", best.Shop)
	}

	buyable, ok := BestBuyable(SlotWeapon, at40, nil)
	if !ok || buyable.Name != "Rune longsword" {
		t.Fatalf("best buyable weapon at 40 = %q (%v), want Rune longsword", buyable.Name, ok)
	}
	if buyable.ShopNote == "" {
		t.Error("the runite shop is behind the Champions' Guild door and must say so")
	}
}

func TestBestBuyableWeaponAtThirty(t *testing.T) {
	got, ok := BestBuyable(SlotWeapon, Levels{Attack: 30}, nil)
	if !ok || got.Name != "Adamant longsword" {
		t.Fatalf("got %q (%v), want Adamant longsword (Al Kharid's scimitars stop at mithril)", got.Name, ok)
	}
}

// Correcting docs/combat_trainer_plan.md, which claimed Falador sells a mithril kiteshield.
// It stocks mithril only as a SQUARE shield; its best kiteshield is steel.
func TestFaladorsBestKiteshieldIsSteelNotMithril(t *testing.T) {
	mithKite, ok := ItemByName("Mithril kiteshield")
	if !ok {
		t.Fatal("the item exists, it is just not sold")
	}
	if mithKite.Shop != "" {
		t.Fatalf("Mithril kiteshield claims shop %q, but falador.inv's shieldshop stocks mithril_sq_shield only", mithKite.Shop)
	}
	steelKite, _ := ItemByName("Steel kiteshield")
	if steelKite.Shop != ShopFaladorShield {
		t.Fatalf("Steel kiteshield shop = %q, want the Falador shield shop", steelKite.Shop)
	}
	// And so the best buyable shield at 20 Defence is the mithril square shield.
	got, ok := BestBuyable(SlotShield, Levels{Defence: 20}, nil)
	if !ok || got.Name != "Mithril sq shield" {
		t.Fatalf("best buyable shield at 20 = %q (%v), want Mithril sq shield", got.Name, ok)
	}
}

func TestQuestGatedGearIsRefusedUntilTheQuestIsDone(t *testing.T) {
	body, ok := ItemByName("Rune platebody")
	if !ok || body.Quest != QuestDragonSlayer {
		t.Fatalf("Rune platebody quest = %q, want %q", body.Quest, QuestDragonSlayer)
	}
	at40 := Levels{Defence: 40}
	if Wearable(body, at40, nil) {
		t.Error("40 Defence alone must not be enough for a rune platebody")
	}
	if !Wearable(body, at40, map[string]bool{QuestDragonSlayer: true}) {
		t.Error("with Dragon Slayer done it must be wearable")
	}
	if body.Shop != ShopEdgevilleOziach {
		t.Errorf("Rune platebody shop = %q, want Oziach in Edgeville", body.Shop)
	}
}

// Dragon is in the tier table at 60, but most dragon pieces simply do not exist in content
// 289 — a table that generated them blindly would send a script shopping for a phantom.
func TestDragonPiecesThatDoNotExistAreAbsent(t *testing.T) {
	for _, name := range []string{"Dragon scimitar", "Dragon platebody", "Dragon full helm", "Dragon kiteshield", "Dragon plateskirt"} {
		if _, ok := ItemByName(name); ok {
			t.Errorf("%s must not be in the table: no such item in content 289", name)
		}
	}
	for _, name := range []string{"Dragon dagger", "Dragon longsword", "Dragon med helm", "Dragon chainbody", "Dragon platelegs", "Dragon sq shield"} {
		if _, ok := ItemByName(name); !ok {
			t.Errorf("%s does exist in content 289 and must be in the table", name)
		}
	}
}

func TestBowLevels(t *testing.T) {
	cases := []struct {
		name  string
		level int
	}{
		{"Shortbow", 1}, {"Oak shortbow", 5}, {"Willow shortbow", 20},
		{"Maple shortbow", 30}, {"Yew shortbow", 40}, {"Magic shortbow", 50},
	}
	for _, c := range cases {
		it, ok := ItemByName(c.name)
		if !ok || it.Level != c.level || it.Skill != SkillRanged {
			t.Errorf("%s = %+v, want Ranged %d", c.name, it, c.level)
		}
	}
}

// Arrows are limited by the BOW, not by the player: player_ranged_check_ammo compares the two
// items' own levelrequire params.
func TestArrowFitsBowComparesTheTwoItemsNotThePlayer(t *testing.T) {
	willow, _ := ItemByName("Willow shortbow") // param 20
	yew, _ := ItemByName("Yew shortbow")       // param 40
	mith, _ := ItemByName("Mithril arrow")     // param 20
	rune_, _ := ItemByName("Rune arrow")       // param 40

	if !ArrowFitsBow(mith, willow) {
		t.Error("mithril arrows (20) must fit a willow bow (20)")
	}
	if ArrowFitsBow(rune_, willow) {
		t.Error("rune arrows (40) must NOT fit a willow bow (20) — 'Your bow isn't powerful enough'")
	}
	if !ArrowFitsBow(rune_, yew) {
		t.Error("rune arrows must fit a yew bow (40)")
	}
	// The pairing is item-to-item, so a maxed player still cannot fire rune arrows from a
	// willow bow.
	if ArrowFitsBow(rune_, willow) {
		t.Error("the player's Ranged level does not enter into the check")
	}
}

func TestBestArrowForBow(t *testing.T) {
	oak, _ := ItemByName("Oak shortbow")
	got, ok := BestArrowForBow(oak, true)
	if !ok || got.Name != "Steel arrow" {
		t.Fatalf("best buyable arrow for an oak bow = %q (%v), want Steel arrow", got.Name, ok)
	}
	yew, _ := ItemByName("Yew shortbow")
	got, ok = BestArrowForBow(yew, true)
	if !ok || got.Name != "Rune arrow" {
		t.Fatalf("best buyable arrow for a yew bow = %q (%v), want Rune arrow", got.Name, ok)
	}
}

// Maple, yew and magic bows are sold nowhere, which is why the plan routes them through the
// mule.
func TestTheBowsNoShopSells(t *testing.T) {
	for _, name := range []string{"Maple shortbow", "Yew shortbow", "Magic shortbow"} {
		it, ok := ItemByName(name)
		if !ok {
			t.Fatalf("%s missing from the table", name)
		}
		if it.Shop != "" {
			t.Errorf("%s claims shop %q; no .inv in content 289 stocks it", name, it.Shop)
		}
	}
	willow, _ := ItemByName("Willow shortbow")
	if willow.Shop != ShopRangingGuild {
		t.Errorf("Willow shortbow shop = %q, want the Ranging Guild", willow.Shop)
	}
}

// Arrows carry no wear requirement of their own in any tier file, so a level-1 account can
// hold rune arrows — it just cannot fire them from a weak bow.
func TestArrowsHaveNoWearRequirement(t *testing.T) {
	rune_, _ := ItemByName("Rune arrow")
	if rune_.Level != 0 {
		t.Fatalf("Rune arrow wear level = %d, want 0: no tier file registers an arrow", rune_.Level)
	}
	if !Wearable(rune_, Levels{}, nil) {
		t.Error("an arrow must be wearable at any level")
	}
}
