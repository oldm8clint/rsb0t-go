package quests

import (
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

func TestAllListsEveryMappedQuestSorted(t *testing.T) {
	state := gamestate.New()
	w := packet.NewWriter(4)
	w.P2(7333)     // cook's assistant
	w.P2(31 << 10) // red
	state.Handle(serverprot.IF_SETCOLOUR, w.Data[:w.Pos])

	all := All(state)
	if len(all) != len(Known()) {
		t.Fatalf("All has %d entries, Known %d", len(all), len(Known()))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Name > all[i].Name {
			t.Fatalf("not sorted: %q before %q", all[i-1].Name, all[i].Name)
		}
	}
	for _, e := range all {
		want := Unknown
		if e.Name == "cook's assistant" {
			want = NotStarted
		}
		if e.Status != want {
			t.Errorf("%s = %v, want %v", e.Name, e.Status, want)
		}
	}
}
