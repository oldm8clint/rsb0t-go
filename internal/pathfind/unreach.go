package pathfind

import "sync"

// Remembering where a search could not get to.
//
// A FindPath that answers "unreachable" has expanded every tile the start can reach (nothing
// stops the search early but the goal and the budget), so its closed set IS that start's forward
// reach. Reach is transitive: from any tile inside it, nothing outside it can be reached either.
// So a later search whose start lies in a remembered flood and none of whose goals do is
// unreachable too, exactly, without expanding a tile.
//
// THE COST THIS REMOVES. freshstartrunner.SailLeg asks the pack, before every boat leg, whether a
// plain walk already gets the account to East Ardougne's bank. Pack-only searches cross no doors,
// and Ardougne's are doors, so from Lumbridge, Draynor and Port Sarim the answer is always no
// after flooding the same 216,661-tile component: 0.5-1 s of CPU a call, measured on the real
// pack, and on live 73ff4228 (5 Oct 2026) SailLeg and the other pack searches were 13% of the
// control panel's CPU.
//
// Only a Pack read from a pack file remembers (Load, Parse); WithWalls's copies, whose live
// walls can open a way the static pack has none, and searches with an avoid predicate never use
// it.

// unreachKeep is how many floods are remembered; the oldest is forgotten first.
const unreachKeep = 8

// unreachCache is a Pack's remembered floods. Safe for concurrent use.
type unreachCache struct {
	mu     sync.Mutex
	floods []*flood
}

// flood is one search's closed set, as a bitset per 64x64 mapsquare level.
type flood struct {
	pages map[int64]*[64]uint64
}

func floodPage(id int64) (key int64, word int, bit uint) {
	key = id &^ (0x3f<<14 | 0x3f)
	i := int((id>>14)&0x3f)<<6 | int(id&0x3f)
	return key, i >> 6, uint(i & 63)
}

func (f *flood) has(id int64) bool {
	key, w, b := floodPage(id)
	pg := f.pages[key]
	return pg != nil && pg[w]&(1<<b) != 0
}

// excludes reports whether a remembered flood holds start and none of goals.
func (c *unreachCache) excludes(start int64, goals map[int64]bool) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	floods := c.floods
	c.mu.Unlock()
	for _, f := range floods {
		if !f.has(start) {
			continue
		}
		hit := false
		for g := range goals {
			if f.has(g) {
				hit = true
				break
			}
		}
		if !hit {
			return true
		}
	}
	return false
}

// remember keeps a complete flood (the closed set of a search that answered unreachable).
func (c *unreachCache) remember(closed map[int64]bool) {
	if c == nil || len(closed) == 0 {
		return
	}
	f := &flood{pages: map[int64]*[64]uint64{}}
	for id := range closed {
		key, w, b := floodPage(id)
		pg := f.pages[key]
		if pg == nil {
			pg = new([64]uint64)
			f.pages[key] = pg
		}
		pg[w] |= 1 << b
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	floods := append([]*flood(nil), c.floods...)
	floods = append(floods, f)
	if len(floods) > unreachKeep {
		floods = floods[len(floods)-unreachKeep:]
	}
	c.floods = floods
}
