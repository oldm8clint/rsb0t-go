package questmods

import (
	"testing"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The Khazard stronghold is a pocket for every quest's walks (questrunner.Travel): the live deaths of 16 Oct 2026
// left the first account upstairs with the next quest (Merlin's strip) planning its first walk on the chest floor, and
// every plan came back "unreachable". A walk that starts inside leaves by the stronghold's own exit first.
func TestKhazardStrongholdIsAPocket(t *testing.T) {
	for _, at := range []tile.Tile{
		questplan.KhazardHallLanding, questplan.KhazardInnerDoorStand, questplan.KhazardLadderStand, questplan.KhazardFrontDoorStand,
		questplan.KhazardChestStand, questplan.KhazardLadderTopStand, {X: 2505, Z: 3256, Level: 1}, {X: 2504, Z: 3252, Level: 1},
	} {
		if name, ok := questrunner.PocketAt(at); !ok || name != "the Khazard stronghold" {
			t.Errorf("%v: pocket %q %v", at, name, ok)
		}
	}
	for _, at := range []tile.Tile{
		questplan.KhazardWallSouth, questplan.KhazardDoorTile, {X: 2502, Z: 3249}, questplan.EastArdougneNorth, questplan.BolrenAnchor,
		{X: 2509, Z: 3260}, {X: 2506, Z: 3258, Level: 2},
	} {
		if name, ok := questrunner.PocketAt(at); ok && name == "the Khazard stronghold" {
			t.Errorf("%v is outside the stronghold, got pocket %q", at, name)
		}
	}
}

// Every custom step the plan can name is registered, the new way out among them.
func TestTreeGnomeRegistersEveryCustomStep(t *testing.T) {
	customs := TreeGnomeVillage().Customs
	for _, name := range []string{questplan.CustomTreeBallista, questplan.CustomTreeOrb, questplan.CustomTreeWarlord,
		questplan.CustomTreeCeremony, questplan.CustomTreeLeave} {
		if customs[name] == nil {
			t.Errorf("no executor for %q", name)
		}
	}
}

// An account outside the stronghold is out: the way out does nothing and says it is out.
func TestLeaveStrongholdIsDoneOutside(t *testing.T) {
	for _, at := range []tile.Tile{questplan.KhazardWallSouth, questplan.KhazardDoorTile, questplan.EastArdougneNorth} {
		d, _ := stripRig(t, at)
		if !leaveStronghold(d) {
			t.Errorf("leaveStronghold from %v (outside) is not done", at)
		}
		if got := questrunner.CurrentTile(d); got != at {
			t.Errorf("an account outside the stronghold was moved from %v to %v", at, got)
		}
	}
}
