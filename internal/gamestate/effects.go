package gamestate

import "rsb0t-go/internal/packet"

// RenderCombat preserves the client's four simultaneous hitmark slots.
type RenderCombat struct {
	Spot   RenderSpot   `json:"spot"`
	Hits   [4]RenderHit `json:"hits"`
	Health int          `json:"health"`
	Total  int          `json:"total"`
	Tick   int          `json:"tick"`
	Seen   bool         `json:"seen"`
}

type RenderSpot struct {
	ID     int  `json:"id"`
	Tick   int  `json:"tick"`
	Height int  `json:"height"`
	Delay  int  `json:"delay"`
	Seen   bool `json:"seen"`
}

func (c *RenderCombat) readSpot(r *packet.Packet, tick int) {
	id, packed := int(r.G2()), uint32(r.G4())
	c.Spot = RenderSpot{ID: id, Tick: tick, Height: int(packed >> 16), Delay: int(packed & 65535), Seen: id != 65535}
}

type RenderHit struct {
	Damage int  `json:"damage"`
	Type   int  `json:"type"`
	Tick   int  `json:"tick"`
	Seen   bool `json:"seen"`
}

// read takes one hitmark block (four bytes) into the four hit slots and returns its damage type.
func (c *RenderCombat) read(r *packet.Packet, tick int) (damageType int) {
	h := RenderHit{Damage: int(r.G1()), Type: int(r.G1()), Tick: tick, Seen: true}
	c.Health, c.Total, c.Tick, c.Seen = int(r.G1()), int(r.G1()), tick, true
	for i := range c.Hits {
		if !c.Hits[i].Seen || tick-c.Hits[i].Tick >= 3 {
			c.Hits[i] = h
			break
		}
	}
	return h.Type
}

// RenderEffect uses server ticks plus 20ms client-cycle delays from zone packets.
type RenderEffect struct {
	ID         int  `json:"id"`
	Tick       int  `json:"tick"`
	Level      int  `json:"level"`
	X          int  `json:"x"`
	Z          int  `json:"z"`
	ToX        int  `json:"toX"`
	ToZ        int  `json:"toZ"`
	Height     int  `json:"height"`
	EndHeight  int  `json:"endHeight"`
	Start      int  `json:"start"`
	End        int  `json:"end"`
	Slope      int  `json:"slope"`
	Arc        int  `json:"arc"`
	Target     int  `json:"target"`
	Projectile bool `json:"projectile"`
}

// effectsWatchTicks is how long after the last RenderSnapshot (the stream view or a capture
// reading this account) spot and projectile animations are kept: 50 ticks, 30 seconds. An
// account nobody watches skips their bytes as it always did, so the hundreds of accounts no
// one is looking at do no effects work at all.
const effectsWatchTicks = 50

// watchRender marks this account as rendered now (RenderSnapshot; tests). Atomic, because
// RenderSnapshot holds only the read lock.
func (s *State) watchRender() { s.renderTick.Store(int64(max(s.scene.tick, 1))) }

// renderWatched reports a RenderSnapshot within effectsWatchTicks. Caller holds the lock.
func (s *State) renderWatched() bool {
	t := s.renderTick.Load()
	return t > 0 && int64(s.scene.tick)-t <= effectsWatchTicks
}

func (s *State) applyEffect(r *packet.Packet, projectile bool) {
	size := 6
	if projectile {
		size = 15
	}
	if r.Available() < size {
		return
	}
	if !s.renderWatched() {
		r.Skip(size)
		s.effects = nil
		return
	}
	x, z := s.zoneTile(r.G1())
	e := RenderEffect{Tick: s.scene.tick, Level: s.level, X: x, Z: z, Projectile: projectile}
	if projectile {
		e.ToX, e.ToZ = x+int(int8(r.G1())), z+int(int8(r.G1()))
		e.Target = int(int16(r.G2()))
		e.ID = int(r.G2())
		e.Height, e.EndHeight = int(r.G1())*4, int(r.G1())*4
		e.Start, e.End = int(r.G2()), int(r.G2())
		e.Slope, e.Arc = int(r.G1()), int(r.G1())
	} else {
		e.ID, e.Height, e.Start = int(r.G2()), int(r.G1()), int(r.G2())
		e.End = e.Start + 250 // bounded retention; renderer uses the cache sequence duration
	}
	if e.ID == 65535 || e.End < e.Start {
		return
	}
	kept := s.effects[:0]
	for _, old := range s.effects {
		if (s.scene.tick-old.Tick)*30 <= old.End+30 && old.Level == s.level {
			kept = append(kept, old)
		}
	}
	s.effects = append(kept, e)
	if len(s.effects) > 256 {
		s.effects = s.effects[len(s.effects)-256:]
	}
}
