package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/fisherrunner"
	"rsb0t-go/internal/scripts/fleetplan"
)

func TestFisherStallDefaultsOnAndSwitchesOff(t *testing.T) {
	if !readFisherStall(nil).On || !readFisherStall(map[string]interface{}{}).On {
		t.Fatal("walkRub does not default on")
	}
	if readFisherStall(map[string]interface{}{"walkRub": false}).On {
		t.Fatal("walkRub: false left the staller on")
	}
	if !readFisherStall(map[string]interface{}{"walkRub": true}).On {
		t.Fatal("walkRub: true turned the staller off")
	}
}

// Both scripts carry the same staller: the setting is read for ProgressiveFisher and BassOysterCaskets
// through one bot, and its Deps carry it.
func TestBothFishersShareTheStall(t *testing.T) {
	for _, bass := range []bool{false, true} {
		b := &fisherBot{bassOyster: bass}
		b.stall = readFisherStall(nil)
		if d := b.deps(&Context{}); d.Stall != b.stall || !d.Stall.On {
			t.Fatalf("bassOyster %v: the Deps do not carry the bot's staller", bass)
		}
	}
}

// The hand-off never gives the mule the staller's jewellery: every charge of both items is kept while the
// stall is on, and nothing is kept for it while it is off.
func TestStallKeepNamesOnlyWhileOn(t *testing.T) {
	b := &fisherBot{}
	if b.stallKeepNames() != nil {
		t.Fatal("a bot with no stall keeps names")
	}
	b.stall = &fisherrunner.Stall{On: false}
	if b.stallKeepNames() != nil {
		t.Fatal("a stall that is off keeps names")
	}
	b.stall.On = true
	names := b.stallKeepNames()
	if len(names) != 16 || names[0] != "Games necklace(8)" || names[8] != "Ring of dueling(8)" {
		t.Fatalf("stallKeepNames = %v", names)
	}
	for _, n := range names {
		if !fleetplan.IsRubItem(n) {
			t.Errorf("%q is kept but is not a rub item", n)
		}
	}
}
