package webwalk

import "sync"

// nodePage holds the search records of one level's 64x64 mapsquare.
type nodePage [64 * 64]node

var nodePages = sync.Pool{New: func() any { return new(nodePage) }}

// nodeStore is a search's records, paged by mapsquare. A search touches a few dozen
// mapsquares and nearly every lookup lands in the page the last one did, so this replaces
// a map[int64]node whose hashing was most of the planner's CPU on the live fleet.
type nodeStore struct {
	pages   map[int64]*nodePage
	lastKey int64
	last    *nodePage
}

func newNodeStore() *nodeStore {
	return &nodeStore{pages: make(map[int64]*nodePage, 64), lastKey: -1}
}

// pageKey drops a node id's low six bits of x and z (see nodeID).
func pageKey(id int64) int64 { return id &^ (0x3f<<14 | 0x3f) }

func pageSlot(id int64) int { return int((id>>14)&0x3f)<<6 | int(id&0x3f) }

func (s *nodeStore) page(id int64, create bool) *nodePage {
	key := pageKey(id)
	if key == s.lastKey {
		return s.last
	}
	pg := s.pages[key]
	if pg == nil {
		if !create {
			return nil
		}
		pg = nodePages.Get().(*nodePage)
		s.pages[key] = pg
	}
	s.lastKey, s.last = key, pg
	return pg
}

// get returns the record for id; its seen field is false when the search has not reached it.
func (s *nodeStore) get(id int64) node {
	pg := s.page(id, false)
	if pg == nil {
		return node{}
	}
	return pg[pageSlot(id)]
}

// at returns the record for id to write, creating its page.
func (s *nodeStore) at(id int64) *node { return &s.page(id, true)[pageSlot(id)] }

// release clears the pages and returns them to the pool.
func (s *nodeStore) release() {
	for _, pg := range s.pages {
		*pg = nodePage{}
		nodePages.Put(pg)
	}
	s.pages, s.last, s.lastKey = nil, nil, -1
}
