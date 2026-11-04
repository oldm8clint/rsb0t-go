package bot

import "testing"

func TestAdvancedFighterSettingsDefaults(t *testing.T) {
	for _, values := range []map[string]interface{}{{}, nil} {
		s := readAdvancedFighterSettings(values)
		if len(s.targets) != 0 {
			t.Errorf("targets default = %v, want empty (anything attackable)", s.targets)
		}
		if s.leashRadius != 10 {
			t.Errorf("leashRadius default = %d, want 10", s.leashRadius)
		}
		if !s.preferAttackers {
			t.Error("preferAttackers should default true")
		}
		if s.attackOthersTargets {
			t.Error("attackOthersTargets should default false")
		}
		if s.meleeStyle != "strength" {
			t.Errorf("meleeStyle default = %q, want strength", s.meleeStyle)
		}
		if !s.autoRetaliate {
			t.Error("autoRetaliate should default true")
		}
		if s.foodWithdraw != 10 {
			t.Errorf("foodWithdraw default = %d, want 10", s.foodWithdraw)
		}
		if !s.autoBank {
			t.Error("autoBank should default true")
		}
		if s.bankAtLootSlots != 12 {
			t.Errorf("bankAtLootSlots default = %d, want 12", s.bankAtLootSlots)
		}
		if s.panicAtFrac != 0.25 {
			t.Errorf("panicAtFrac default = %v, want 0.25 (25%%)", s.panicAtFrac)
		}
		if s.eatAtFrac != 0.5 {
			t.Errorf("eatAtFrac default = %v, want 0.5 (50%%)", s.eatAtFrac)
		}
		if s.maxDeaths != 0 {
			t.Errorf("maxDeaths default = %d, want 0 (never halt)", s.maxDeaths)
		}
		if s.mule != "" || s.muleAt != 0 {
			t.Errorf("mule hand-off must default off, got mule=%q muleAt=%d", s.mule, s.muleAt)
		}
	}
}

func TestAdvancedFighterSettingsOverrides(t *testing.T) {
	s := readAdvancedFighterSettings(map[string]interface{}{
		"targets":        "Goblin, Chicken",
		"leashRadius":    5,
		"meleeStyle":     "progressive",
		"foodWithdraw":   20,
		"eatAtHp":        70.0,
		"panicHp":        15.0,
		"buryBones":      true,
		"loot":           "Rune,Uncut sapphire",
		"mule":           "mulecon",
		"muleAt":         500,
		"muleKeep":       "Coins",
		"stopAtLevel":    "attack:70",
		"stopAfterKills": 100,
	})
	if len(s.targets) != 2 || s.targets[0] != "Goblin" || s.targets[1] != "Chicken" {
		t.Errorf("targets = %v", s.targets)
	}
	if s.leashRadius != 5 {
		t.Errorf("leashRadius = %d, want 5", s.leashRadius)
	}
	if s.meleeStyle != "progressive" {
		t.Errorf("meleeStyle = %q, want progressive", s.meleeStyle)
	}
	if s.foodWithdraw != 20 {
		t.Errorf("foodWithdraw = %d, want 20", s.foodWithdraw)
	}
	if s.eatAtFrac != 0.7 {
		t.Errorf("eatAtFrac = %v, want 0.7", s.eatAtFrac)
	}
	if s.panicAtFrac != 0.15 {
		t.Errorf("panicAtFrac = %v, want 0.15", s.panicAtFrac)
	}
	if !s.buryBones {
		t.Error("buryBones should be true")
	}
	if s.mule != "mulecon" || s.muleAt != 500 {
		t.Errorf("mule=%q muleAt=%d, want mulecon/500", s.mule, s.muleAt)
	}
	if s.goal.LevelSkill != "attack" || s.goal.LevelTarget != 70 {
		t.Errorf("goal level = %q %d, want attack 70", s.goal.LevelSkill, s.goal.LevelTarget)
	}
	if s.goal.Kills != 100 {
		t.Errorf("goal kills = %d, want 100", s.goal.Kills)
	}
}

func TestAdvancedFighterSettingsClamping(t *testing.T) {
	s := readAdvancedFighterSettings(map[string]interface{}{
		"leashRadius":     1,     // below the 2 floor
		"foodWithdraw":    999,   // above the 27 ceiling
		"bankAtLootSlots": 0,     // below the 1 floor
		"eatAtHp":         200.0, // above the 90 ceiling
		"panicHp":         -5.0,  // below the 0 floor
		"muleAt":          -100,
	})
	if s.leashRadius < 2 {
		t.Errorf("leashRadius = %d, want clamped to >=2", s.leashRadius)
	}
	if s.foodWithdraw > 27 {
		t.Errorf("foodWithdraw = %d, want clamped to <=27", s.foodWithdraw)
	}
	if s.bankAtLootSlots < 1 {
		t.Errorf("bankAtLootSlots = %d, want clamped to >=1", s.bankAtLootSlots)
	}
	if s.eatAtFrac > 0.9 {
		t.Errorf("eatAtFrac = %v, want clamped to <=0.9", s.eatAtFrac)
	}
	if s.panicAtFrac < 0 {
		t.Errorf("panicAtFrac = %v, want clamped to >=0", s.panicAtFrac)
	}
	if s.muleAt < 0 {
		t.Errorf("muleAt = %d, want clamped to >=0", s.muleAt)
	}
}
