package essenceplan

import (
	"testing"

	"rsb0t-go/internal/scripts/donorplan"
)

// The knife and the log of the mining batch (mineTickManip) stay in the pack through every bank visit, so a pack that holds only
// them and the pickaxe is an empty pack to the stage decision: it goes back through the gateway, it does not loop to the bank.
func TestTheMiningKitIsNotAHaulAndTheStageAfterTheBankIsTheTeleport(t *testing.T) {
	kit := []string{"Knife", "Logs"}
	pack := []donorplan.ItemRow{{Name: "Bronze pickaxe", ID: 1265, Count: 1}, {Name: "Knife", ID: 946, Count: 1}, {Name: "Logs", ID: 1511, Count: 1}}
	if n := MuleHaul(pack, kit); n != 0 {
		t.Fatalf("haul %d: the pickaxe and the kit are not for the mule", n)
	}
	got := DecideStage(MinerSnapshot{Haul: MuleHaul(pack, kit), PickHeld: true})
	if got != StageTeleport {
		t.Fatalf("stage %q after the bank, want %q", got, StageTeleport)
	}
	// Without the kit in the keep list the same pack is a haul of two and the loop would bank it for ever: why the keep list is
	// passed (internal/scripts/runecraftrunner liveEnv.Haul).
	if n := MuleHaul(pack, nil); n != 2 {
		t.Fatalf("haul %d without the keep list, want 2", n)
	}
	// Essence is a haul whatever is kept.
	pack = append(pack, donorplan.ItemRow{Name: "Rune essence", ID: 1436, Count: 1})
	if got := DecideStage(MinerSnapshot{Haul: MuleHaul(pack, kit), PickHeld: true}); got != StageBank {
		t.Fatalf("stage %q with essence in the pack, want %q", got, StageBank)
	}
	// A full pack inside the mine is full with the kit in it: 28 slots, 25 of them essence.
	if got := DecideStage(MinerSnapshot{InMine: true, PackFull: true, Haul: 25, PickHeld: true}); got != StagePortal {
		t.Fatalf("stage %q for a full pack in the mine", got)
	}
}
