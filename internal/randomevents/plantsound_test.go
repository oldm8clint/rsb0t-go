package randomevents

import (
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

func playSound(s *gamestate.State, id int) {
	s.Handle(serverprot.SYNTH_SOUND, payload(func(p *packet.Packet) { p.P2(uint16(id)); p.P1(1); p.P2(0) }))
}

func advanceTicks(s *gamestate.State, n int) {
	for i := 0; i < n; i++ {
		s.Handle(serverprot.PLAYER_INFO, bitPack(3, bitField{1, 0}, bitField{8, 0}, bitField{11, 2047}))
	}
}

// Live 2026-09-19 on 23704ea: content plays triffid_appear to the plant's owner alone; 111 of
// 111 accounts told "not here for you" had not heard it and both owners had. Stacked fishers
// then stop walking over to other accounts' plants.
func TestPlantWithoutOurSpawnSoundIsNotChecked(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Strange plant"}})
	s := overlapState()
	playSound(s, gamestate.SoundSwarmAppear) // sounds do reach this session
	addNPC(s, 1, 500, 1, 0)
	if ev := Detect(s); ev != nil {
		t.Fatalf("someone else's plant (no triffid_appear heard) was taken up: %+v", ev)
	}
}

func TestPlantAfterOurSpawnSoundIsChecked(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Strange plant"}})
	s := overlapState()
	playSound(s, gamestate.SoundTriffidAppear)
	addNPC(s, 1, 500, 1, 0)
	advanceTicks(s, 150) // still inside the plant's lifetime
	if ev := Detect(s); ev == nil || ev.NpcIndex != 1 {
		t.Fatalf("our own plant was ignored: %+v", ev)
	}
}

func TestPlantSoundFromAnEarlierPlantDoesNotLastForever(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Strange plant"}})
	s := overlapState()
	playSound(s, gamestate.SoundTriffidAppear)
	advanceTicks(s, plantLifetimeTicks+10)
	addNPC(s, 1, 500, 1, 0)
	if ev := Detect(s); ev != nil {
		t.Fatalf("a plant long after our last one was taken up: %+v", ev)
	}
}

// With no SYNTH_SOUND ever received there is no evidence either way: keep checking.
func TestPlantIsCheckedWhenNoSoundsHaveArrived(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Strange plant"}})
	s := overlapState()
	addNPC(s, 1, 500, 1, 0)
	if ev := Detect(s); ev == nil || ev.NpcIndex != 1 {
		t.Fatalf("plant ignored without any sound evidence: %+v", ev)
	}
}
