package fleetrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// packSession is an actions.Session over a real gamestate.State whose pack the test fills with
// the server's own UPDATE_INV_PARTIAL packets.
type packSession struct {
	st     *gamestate.State
	cheats anticheat.Counters
}

func (p *packSession) Send(func(*packet.Packet)) error { return nil }

func (p *packSession) State() *gamestate.State { return p.st }

func (p *packSession) Cheats() *anticheat.Counters { return &p.cheats }

// put sets one pack slot to count of obj id (0 empties it).
func (p *packSession) put(slot, id, count int) {
	enc := id + 1
	if id == 0 {
		enc = 0
	}
	b := []byte{byte(PlayerPack >> 8), byte(PlayerPack & 0xff), byte(slot), byte(enc >> 8), byte(enc & 0xff)}
	if count < 255 {
		b = append(b, byte(count))
	} else {
		b = append(b, 255, byte(count>>24), byte(count>>16), byte(count>>8), byte(count))
	}
	p.st.Handle(serverprot.UPDATE_INV_PARTIAL, b)
}

func testDeps() (*Deps, *packSession, *[]loot.Drop) {
	var got []loot.Drop
	s := &packSession{st: gamestate.New()}
	d := &Deps{Session: s, Loot: func(dr loot.Drop) { got = append(got, dr) }, Delay: func(time.Duration) bool { return true }}
	return d, s, &got
}

// TestCowBonesLandOnTheCowNotOnGround is "currently cows show no loot even though theres kills".
//
// The live panel had kill:cow:81 at 97 events with no items and the same accounts' 106 Bones on
// an openable named "Ground", id -1, with zero events: bones.go recorded every take under that
// generic source. Replayed here exactly as Stage 3 and Stage 5 run it — the kill noted at the
// cow's tile, then the bones beside it taken — and the take must land on the cow. On the old code
// the take is recorded as open:ground:-1.
func TestCowBonesLandOnTheCowNotOnGround(t *testing.T) {
	d, s, got := testDeps()
	NoteKill(d, 81, 2605, 3300, 0)
	before := LootBefore(d)
	s.put(0, 526, 1) // the bones arrive
	RecordGroundTake(d, before, gamestate.GroundItem{ID: 526, X: 2606, Z: 3300, Level: 0})

	if len(*got) != 2 {
		t.Fatalf("recorded %d drops, want the kill and the take: %+v", len(*got), *got)
	}
	kill, take := (*got)[0], (*got)[1]
	if kill.Events != 1 || kill.Source.Action != loot.ActionKill || kill.Source.ID != 81 {
		t.Fatalf("kill = %+v", kill)
	}
	if take.Source != kill.Source {
		t.Fatalf("the bones went to %+v, not to the cow %+v", take.Source, kill.Source)
	}
	if loot.IsLegacyGroundPickup(take.Source) {
		t.Fatal("the take was recorded under the retired Ground openable")
	}
	if take.Events != 0 || len(take.Items) != 1 || take.Items[0].ID != 526 || take.Items[0].Count != 1 {
		t.Fatalf("take = %+v, want one Bones with no extra kill counted", take)
	}
}

// TestAPileNoKillOfOursMadeIsAPickup: a take with no recent kill to claim it is a pickup of the
// item itself, never the retired Ground openable and never some other npc's kill.
func TestAPileNoKillOfOursMadeIsAPickup(t *testing.T) {
	d, s, got := testDeps()
	before := LootBefore(d)
	s.put(0, 526, 1)
	RecordGroundTake(d, before, gamestate.GroundItem{ID: 526, X: 2606, Z: 3300})
	if len(*got) != 1 || (*got)[0].Source.Kind != loot.KindPickup || (*got)[0].Events != 1 {
		t.Fatalf("recorded %+v, want one pickup of the bones", *got)
	}
}
