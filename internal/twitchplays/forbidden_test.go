package twitchplays

import (
	"testing"
	"time"
)

// Banking, trading, dropping and giving items away are refused by the command model itself,
// whatever the arguments, so no queue, controller or sink is ever handed one.
func TestForbiddenActionsAreNeverValidCommands(t *testing.T) {
	for _, text := range []string{"!bank", "!BANK all", "!deposit", "!withdraw 100 coins", "!trade bob", "!drop", "!drop all",
		"!give bob coins", "!giveaway", "!sell fish", "!buy net", "!stake", "!duel bob", "!alch"} {
		if _, ok := Parse(text, "discord", "viewer", "m1", time.Unix(1, 0)); ok {
			t.Errorf("%q parsed as a valid command", text)
		}
		if Refusal(text) == "" {
			t.Errorf("%q has no refusal reason", text)
		}
		q := NewQueue(4)
		parts := Command{Action: text[1:], Author: "viewer", MessageID: "m1"}
		if q.Push(parts) {
			t.Errorf("queue accepted %q", text)
		}
	}
	for _, text := range []string{"!walk n", "!fish", "hello", "!unknown"} {
		if Refusal(text) != "" {
			t.Errorf("%q wrongly refused as forbidden", text)
		}
	}
	if _, ok := AllowedActions["bank"]; ok {
		t.Error("bank is an allowed action")
	}
	if _, ok := GoalFromCommand(Command{Action: "bank"}); ok {
		t.Error("bank is a crowd goal")
	}
}

func TestCommandArgumentsAreBounded(t *testing.T) {
	ok := func(text string) bool {
		_, v := Parse(text, "discord", "viewer", "m1", time.Unix(1, 0))
		return v
	}
	for _, text := range []string{"!walk n", "!walk sw 10", "!walk east 1", "!fish", "!wait", "!stop", "!interact goblin", "!interact fishing spot", "!interact man"} {
		if !ok(text) {
			t.Errorf("%q refused", text)
		}
	}
	for _, text := range []string{"!walk", "!walk up", "!walk n 11", "!walk n 0", "!walk n x", "!walk n 2 3",
		"!interact", "!interact banker", "!interact bank booth", "!interact shop keeper", "!interact general store",
		"!interact a b c d", "!interact <script>", "!interact abcdefghijklmnopqrstuvwxyz", "!fish a b c"} {
		if ok(text) {
			t.Errorf("%q accepted", text)
		}
	}
	c, _ := Parse("!walk ne 4", "discord", "viewer", "m1", time.Unix(1, 0))
	if dx, dz, n, err := Walk(c); err != nil || dx != 1 || dz != 1 || n != 4 {
		t.Errorf("walk ne 4 = %d,%d x%d %v", dx, dz, n, err)
	}
	c, _ = Parse("!walk w", "discord", "viewer", "m1", time.Unix(1, 0))
	if _, _, n, _ := Walk(c); n != DefaultWalkSteps {
		t.Errorf("default walk = %d tiles", n)
	}
}
