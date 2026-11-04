package freshstart

import (
	"reflect"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// newAccount.ts has no unit tests in rsb0t-scripts; these table tests cover its decisions.

func TestAcceptButton(t *testing.T) {
	buttons := []Button{{ComID: 1, Label: "Accept", Hidden: true}, {ComID: 2, Label: "Randomise"}, {ComID: 3, Label: "ACCEPT"}}
	if got := AcceptButton(7, buttons); got != 7 {
		t.Errorf("by caption = %d", got)
	}
	if got := AcceptButton(-1, buttons); got != 3 {
		t.Errorf("first visible accept = %d", got)
	}
	if got := AcceptButton(-1, buttons[1:2]); got != -1 {
		t.Errorf("none = %d", got)
	}
}

func TestChooseSkipOption(t *testing.T) {
	cases := []struct {
		options []string
		want    string
		index   int
		match   SkipMatch
	}{
		{nil, "Yes please", -1, SkipNone},
		{[]string{"No thanks", "Yes please, skip it"}, "yes please", 1, SkipWanted},
		{[]string{"No", "Yes"}, "Skip", 1, SkipYes},
		{[]string{"Tell me more", "Goodbye"}, "Yes please", 0, SkipFirst},
	}
	for _, c := range cases {
		i, m := ChooseSkipOption(c.options, c.want)
		if i != c.index || m != c.match {
			t.Errorf("ChooseSkipOption(%v, %q) = %d, %d; want %d, %d", c.options, c.want, i, m, c.index, c.match)
		}
	}
}

func TestNextTutorialAction(t *testing.T) {
	cases := []struct {
		options  int
		cont     bool
		distance int
		want     TutorialAction
	}{
		{2, true, 1, TutorialChoose},
		{0, true, 1, TutorialContinue},
		{0, false, -1, TutorialNoGuide},
		{0, false, 4, TutorialWalk},
		{0, false, 3, TutorialTalk},
	}
	for _, c := range cases {
		if got := NextTutorialAction(c.options, c.cont, c.distance); got != c.want {
			t.Errorf("NextTutorialAction(%+v) = %s", c, got)
		}
	}
}

func TestBrightness(t *testing.T) {
	if id, ok := VeryBrightButton(904); !ok || id != 911 {
		t.Error("standard options tab")
	}
	if id, ok := VeryBrightButton(4445); !ok || id != 6276 {
		t.Error("low-detail options tab")
	}
	if _, ok := VeryBrightButton(1); ok {
		t.Error("an unknown tab has no button")
	}
	var b Brightness
	if b.Done(2) || !b.Done(VeryBright) {
		t.Error("done follows the varp")
	}
	if b.Missed() || b.Missed() || !b.Missed() || !b.Done(2) {
		t.Error("gives up after three misses")
	}
}

func TestSellQueueSkipsRefusedNames(t *testing.T) {
	pack := []PackItem{{"Bronze dagger", 1}, {"Bones", 1}, {"Coins", 5}}
	refused := map[string]bool{"bones": true}
	want := []SellLine{{"Bronze dagger", 1}}
	if got := SellQueue(pack, []string{"Coins"}, refused); !reflect.DeepEqual(got, want) {
		t.Errorf("SellQueue = %+v", got)
	}
	if !Refused(0, 5) || !Refused(1, 0) || Refused(1, 3) {
		t.Error("refused")
	}
}

func TestFareThief(t *testing.T) {
	var f FareThief
	if f.Stunned(0) {
		t.Error("stunned before any failure")
	}
	f.NoteChat("You fail to pick the man's pocket.", 100)
	if !f.Stunned(100+StunTicks) || f.Stunned(101+StunTicks) {
		t.Error("stun lasts StunTicks")
	}
	if resting, started := f.Rest(0.4, RestBelowHP); !resting || !started {
		t.Error("rest starts at the low mark")
	}
	if resting, started := f.Rest(0.6, RestBelowHP); !resting || started {
		t.Error("rest holds below the resume mark")
	}
	if resting, _ := f.Rest(0.95, RestBelowHP); resting {
		t.Error("rest ends at the resume mark")
	}
	f.Pocketed(3)
	f.Pocketed(0)
	if f.Pockets != 2 || f.StolenGP != 3 {
		t.Errorf("pockets %d, gp %d", f.Pockets, f.StolenGP)
	}
}

func TestPickpocketTargets(t *testing.T) {
	if spot, away := PickpocketSpotFor(at(lumbridge), PickpocketSpot); away || spot != PickpocketSpot {
		t.Error("lumbridge uses the spot")
	}
	if spot, away := PickpocketSpotFor(at(ardougne), PickpocketSpot); !away || spot != ardougne {
		t.Error("far off the mainland uses where it stands")
	}
	spot := PickpocketSpot
	npcs := []NPC{
		{Name: "Man", Tile: tile.Tile{X: spot.X + 5, Z: spot.Z}, Distance: 5, Ops: []string{"Talk-to", "Pickpocket"}},
		{Name: "Woman", Tile: spot, Distance: 1, Ops: []string{"Pickpocket"}},
		{Name: "man", Tile: tile.Tile{X: spot.X + 2, Z: spot.Z}, Distance: 1, Ops: []string{"pickpocket"}},
		{Name: "Man", Tile: tile.Tile{X: spot.X + 30, Z: spot.Z}, Distance: 0, Ops: []string{"Pickpocket"}},
		{Name: "Man", Tile: tile.Tile{X: spot.X, Z: spot.Z, Level: 1}, Distance: 0, Ops: []string{"Pickpocket"}},
	}
	if got := ChooseTarget(npcs, "Man", spot); got != 2 {
		t.Errorf("ChooseTarget = %d, want the adjacent man", got)
	}
	if got := ChooseTarget(npcs[3:], "Man", spot); got != -1 {
		t.Errorf("out of the leash or on another level = %d", got)
	}
	if NoTargetAction(true, 0) != FundWaitAway || NoTargetAction(false, 7) != FundWalk || NoTargetAction(false, 6) != FundWaitHere {
		t.Error("no-target actions")
	}
}

func TestBoatLeg(t *testing.T) {
	dest := seersBank
	if to, leg := BoatLeg(lumbridge, dest); leg != LegPier || to != BrimhavenPier {
		t.Error("the mainland sails for the pier")
	}
	if to, leg := BoatLeg(musa, dest); leg != LegDestination || to != dest {
		t.Error("karamja sails on")
	}
}
