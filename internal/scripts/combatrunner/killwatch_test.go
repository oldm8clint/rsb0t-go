package combatrunner

import "testing"

// The two refusals of content's player_in_combat_check, read off the chat said since the attack.
func TestRefusalIn(t *testing.T) {
	for _, c := range []struct {
		chat string
		want Refusal
	}{
		{"", ""},
		{"You swing your axe at the tree.\n", ""},
		{"Someone else is fighting that.\n", RefusedContested},
		{"I'm already under attack!\n", RefusedUnderAttack},
		{"Welcome to RuneScape.\nSomeone else is fighting that.\n", RefusedContested},
	} {
		if got := refusalIn(c.chat); got != c.want {
			t.Errorf("refusalIn(%q) = %q, want %q", c.chat, got, c.want)
		}
	}
}

// A death the account dealt no damage to is another player's kill; an unread reading is ours.
func TestKillIsOurs(t *testing.T) {
	if !killIsOurs(true, 1000, 1004) {
		t.Error("a death after our Hitpoints XP rose was not ours")
	}
	if killIsOurs(true, 1000, 1000) {
		t.Error("a death with no Hitpoints XP gained was counted as ours")
	}
	if !killIsOurs(false, 0, 0) {
		t.Error("a death with no reading was not counted, unlike before")
	}
}
