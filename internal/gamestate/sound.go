package gamestate

import "rsb0t-go/internal/packet"

// Sound ids from Content's synth pack that mark a random event spawning. Content plays them
// with sound_synth inside the owner's own spawn script, so only the event's owner receives
// them (lab-checked 2026-09-19: two accounts on one tile, each heard only its own event).
const (
	SoundTriffidAppear = 350
	SoundSwarmAppear   = 342
	// SoundGenieAppear is genie_appear, played by [proc,macro_geni_spawn] (lab-checked
	// 2026-09-23 by cmd/crowdgenietest: the owner heard it, six bystanders on its tile did not).
	SoundGenieAppear = 353
	// SoundSmokepuff is smokepuff, played by [proc,macro_mysterious_old_man_spawn] and again
	// whenever [proc,macro_event_lost] teleports an event NPC back beside its owner. Unlike
	// the others it is not unique to random events (spells, quest NPCs and imps use it too).
	SoundSmokepuff = 300
	// SoundDwarfWhistle is dwarf_whistle, played by [proc,macro_dwarf_spawn] and again with
	// each of the Drunken Dwarf's four nags.
	SoundDwarfWhistle = 344
)

// applySynthSound records the tick SYNTH_SOUND last played each synth id (p2 id, p1 loops,
// p2 delay). The caller holds s.mu.
func (s *State) applySynthSound(r *packet.Packet) {
	id := int(r.G2())
	if s.soundTicks == nil {
		s.soundTicks = make(map[int]int)
	}
	s.soundTicks[id] = s.scene.tick
	s.soundsHeard++
}

// HeardSoundNear reports whether synth id last played within slack ticks of tick, and whether
// this session has received any SYNTH_SOUND at all (so a false answer means something).
func (s *State) HeardSoundNear(id, tick, slack int) (heard, anySounds bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	at, ok := s.soundTicks[id]
	d := at - tick
	if d < 0 {
		d = -d
	}
	return ok && d <= slack, s.soundsHeard > 0
}
