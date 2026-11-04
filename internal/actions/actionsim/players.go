package actionsim

import (
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// ShowPlayers puts names in view at player indices 1..n (a PLAYER_INFO the client decodes).
func (s *Session) ShowPlayers(names ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Handle(serverprot.PLAYER_INFO, playersInView(offsetsOf(names)...))
}

// PlayerAt is a player in view and where he stands relative to the viewer, in tiles (each within -16..15).
type PlayerAt struct {
	Name   string
	DX, DZ int
}

// offsetsOf puts each name one tile east of the viewer, where ShowPlayers always put them.
func offsetsOf(names []string) []PlayerAt {
	out := make([]PlayerAt, len(names))
	for i, n := range names {
		out[i] = PlayerAt{Name: n, DX: 1}
	}
	return out
}

// ShowPlayersAt is ShowPlayers with the players where the test puts them: a player added at index i (1..n, in order) stands at the
// viewer's tile plus (DX, DZ). Players already in view are not moved by it: HidePlayers first to re-place them.
func (s *Session) ShowPlayersAt(players ...PlayerAt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Handle(serverprot.PLAYER_INFO, playersInView(players...))
}

// HidePlayers takes every player out of view (they walked out of range: a crafter inside his temple), as PLAYER_INFO's old-vis list
// does: one flag bit and the removal op (3) per player known.
func (s *Session) HidePlayers() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hidePlayersLocked()
}

func (s *Session) hidePlayersLocked() {
	n := len(s.st.Players())
	if n == 0 {
		return
	}
	fields := [][2]uint32{{1, 0}, {8, uint32(n)}}
	for i := 0; i < n; i++ {
		fields = append(fields, [2]uint32{1, 1}, [2]uint32{2, 3})
	}
	fields = append(fields, [2]uint32{11, 2047})
	bits := make([]byte, 8+n)
	pos := 0
	for _, f := range fields {
		for k := int(f[0]) - 1; k >= 0; k-- {
			bits[pos/8] |= byte((f[1]>>uint(k))&1) << uint(7-pos%8)
			pos++
		}
	}
	s.st.Handle(serverprot.PLAYER_INFO, bits[:(pos+7)/8])
}

// PlacePlayers replaces every player in view with these, in one step (a HidePlayers and a ShowPlayersAt with no moment between them in
// which the viewer sees nobody).
func (s *Session) PlacePlayers(players ...PlayerAt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hidePlayersLocked()
	s.st.Handle(serverprot.PLAYER_INFO, playersInView(players...))
}

// playersInView is mulerunner's test encoder of the same name: each name added at indices 1..n.
func playersInView(players ...PlayerAt) []byte {
	var fields [][2]uint32
	fields = append(fields, [2]uint32{1, 0}, [2]uint32{8, 0})
	for i, p := range players {
		fields = append(fields, [2]uint32{11, uint32(i + 1)}, [2]uint32{5, uint32(p.DX) & 31}, [2]uint32{5, uint32(p.DZ) & 31}, [2]uint32{1, 0}, [2]uint32{1, 1})
	}
	fields = append(fields, [2]uint32{11, 2047})
	bits := make([]byte, 64+len(players)*8)
	pos := 0
	for _, f := range fields {
		for n := int(f[0]) - 1; n >= 0; n-- {
			bits[pos/8] |= byte((f[1]>>uint(n))&1) << uint(7-pos%8)
			pos++
		}
	}
	bits = bits[:(pos+7)/8]
	for _, pl := range players {
		name := pl.Name
		p := packet.NewWriter(64)
		for i := 0; i < 19; i++ {
			p.P1(0)
		}
		for i := 0; i < 7; i++ {
			p.P2(0)
		}
		p.P8(userhash(name))
		p.P1(3)
		p.P2(0)
		bits = append(bits, 1, byte(p.Pos))
		bits = append(bits, p.Data[:p.Pos]...)
	}
	return bits
}

func userhash(name string) int64 {
	var hash int64
	for _, c := range name {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		hash *= 37
		switch {
		case c >= 'a' && c <= 'z':
			hash += int64(c-'a') + 1
		case c >= '0' && c <= '9':
			hash += int64(c-'0') + 27
		}
	}
	return hash
}
