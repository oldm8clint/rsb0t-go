// Package pathfind loads rsb0t-main's pre-baked "LCNV" collision pack
// (out/collision.lcnav[.gz], built by tools/nav/build-collision.ts from a live engine's
// map data) and runs A* over it — the same pack rsb0t-main's own PathFinder.ts
// consumes, ported directly rather than reinvented, because it already encodes every
// wall/loc collision shape correctly per tile (as a per-tile 8-directional exit
// bitmask) so this port never needs to touch loc/wall geometry itself.
//
// v1 scope: the raw walkable-tile graph only (doors/transports/teleports/requires —
// PathFinder.ts's addEdges/teleport-catalog/policy layers — are not ported yet). That
// already covers point-to-point walking within one town/area, which is what the four
// requested scripts mostly need; cross-map travel (boats, teleports) is handled by
// each script's own explicit logic, not Traversal, in the TS originals too.
//
// Memory: a Pack is loaded once per process and shared by every account. The loader
// streams the (optionally gzipped) file slot by slot and keeps one copy of each distinct
// exit, walk and wall array — most of the map is ocean or unused levels, so the 14.2 MB
// pack holds only a few MB of distinct arrays — and indexes slots through small rows
// allocated only for the mapsquare columns that exist. Lookups return exactly the bytes
// the file holds; see pack_test.go for the tile-by-tile and route-corpus checks.
package pathfind

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"hash/maphash"
	"io"
	"os"
)

const (
	magic0, magic1, magic2, magic3 = 'L', 'C', 'N', 'V'

	mapsquareTiles = 64   // one mapsquare is 64x64 tiles
	exitBytes      = 4096 // 64*64, one byte per tile
	walkBytes      = 512  // 64*64 bits, packed 8/byte
	wallBytes      = 2048 // 64*64 nibbles, packed 2/byte (version >= 2 only)
	levels         = 4
)

// levelSlot is one (level, mapsquare) block's three parallel per-tile arrays. Arrays
// are shared between slots whose bytes are identical, so they must never be written.
type levelSlot struct {
	exit []byte // 4096 bytes: 8-directional exit bitmask per tile
	walk []byte // 512 bytes: walkable bit per tile
	wall []byte // 2048 bytes or nil (pre-v2 packs): wall-facing nibble per tile
}

// Pack is a loaded LCNV collision pack: everything FindPath needs to know which tiles
// are walkable and which of the 8 directions each one may step into.
type Pack struct {
	exitOverrides map[int64]byte
	wallOverrides map[int64]byte
	Version       int
	Members       bool
	Mapsquares    int

	// index[level][mx] is a row of slot numbers for that mapsquare column, allocated
	// only when the column has data: 0 means absent, n means slots[n-1]. It replaces a
	// flat (level<<16)|(mx<<8)|mz pointer table, which cost 2 MB however little of the
	// world the pack covers.
	index [levels][256]*[256]uint16
	slots []levelSlot

	// unreach remembers searches that answered unreachable (unreach.go); nil remembers nothing.
	unreach *unreachCache
}

// Load reads an LCNV pack from disk, transparently gunzipping if the bytes indicate
// gzip — matching how rsb0t-main ships both collision.lcnav and collision.lcnav.gz.
// The file is streamed, so neither the compressed nor the decompressed pack is ever
// held whole.
func Load(path string) (*Pack, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("pathfind: reading %s: %w", path, err)
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 64<<10)
	var r io.Reader = br
	if head, err := br.Peek(2); err == nil && head[0] == 0x1f && head[1] == 0x8b {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("pathfind: gzip: %w", err)
		}
		defer zr.Close()
		r = zr
	}
	return decode(r)
}

// Parse decodes an already-decompressed LCNV pack. Format (from PathFinder.ts's own
// constructor, ported byte-for-byte):
//
//	[0:4]   magic "LCNV"
//	[4]     version (1 or 2)
//	[5]     members flag
//	[6:8]   reserved/unused
//	[8:10]  mapsquare count, little-endian u16
//	then, count times:
//	  [0] mx, [1] mz, [2] levelMask (bit per level 0-3)
//	  for each set level bit, in level order: exit[4096] walk[512] wall[2048 if version>=2]
func Parse(pack []byte) (*Pack, error) {
	return decode(bytes.NewReader(pack))
}

func decode(r io.Reader) (*Pack, error) {
	var head [10]byte
	if _, err := io.ReadFull(r, head[:]); err != nil || head[0] != magic0 || head[1] != magic1 || head[2] != magic2 || head[3] != magic3 {
		return nil, fmt.Errorf("pathfind: not an LCNV pack")
	}
	version := int(head[4])
	if version != 1 && version != 2 {
		return nil, fmt.Errorf("pathfind: unsupported LCNV version %d", version)
	}
	p := &Pack{Version: version, Members: head[5] == 1, unreach: &unreachCache{}}

	need := exitBytes + walkBytes
	if version >= 2 {
		need += wallBytes
	}
	buf := make([]byte, need)
	shared := newInterner()

	count := int(head[8]) | int(head[9])<<8
	var square [3]byte
	for i := 0; i < count; i++ {
		if _, err := io.ReadFull(r, square[:]); err != nil {
			return nil, fmt.Errorf("pathfind: pack truncated in mapsquare header %d/%d", i, count)
		}
		mx, mz, levelMask := int(square[0]), int(square[1]), square[2]
		for level := 0; level < levels; level++ {
			if levelMask&(1<<level) == 0 {
				continue
			}
			if _, err := io.ReadFull(r, buf); err != nil {
				return nil, fmt.Errorf("pathfind: pack truncated in tile data at mapsquare %d/%d level %d", i, count, level)
			}
			slot := levelSlot{
				exit: shared.intern(buf[:exitBytes]),
				walk: shared.intern(buf[exitBytes : exitBytes+walkBytes]),
			}
			if version >= 2 {
				slot.wall = shared.intern(buf[exitBytes+walkBytes:])
			}
			if len(p.slots) == 0xffff {
				return nil, fmt.Errorf("pathfind: pack has more than %d level slots", 0xffff)
			}
			p.slots = append(p.slots, slot)
			row := p.index[level][mx]
			if row == nil {
				row = new([256]uint16)
				p.index[level][mx] = row
			}
			row[mz] = uint16(len(p.slots))
		}
	}

	var extra [1]byte
	n, err := io.ReadFull(r, extra[:])
	if n > 0 {
		return nil, fmt.Errorf("pathfind: pack has trailing bytes after reading %d mapsquares", count)
	}
	if err != io.EOF {
		return nil, fmt.Errorf("pathfind: reading pack: %w", err)
	}
	p.Mapsquares = count
	return p, nil
}

// interner keeps one copy of each distinct byte array seen while loading.
type interner struct {
	seed maphash.Seed
	seen map[uint64][][]byte
}

func newInterner() *interner {
	return &interner{seed: maphash.MakeSeed(), seen: make(map[uint64][][]byte)}
}

func (in *interner) intern(b []byte) []byte {
	h := maphash.Bytes(in.seed, b)
	for _, have := range in.seen[h] {
		if bytes.Equal(have, b) {
			return have
		}
	}
	own := make([]byte, len(b))
	copy(own, b)
	in.seen[h] = append(in.seen[h], own)
	return own
}

func (p *Pack) slotAt(x, z, level int) *levelSlot {
	mx, mz := x>>6, z>>6
	if level < 0 || level >= levels || mx < 0 || mx > 255 || mz < 0 || mz > 255 {
		return nil
	}
	row := p.index[level][mx]
	if row == nil {
		return nil
	}
	n := row[mz]
	if n == 0 {
		return nil
	}
	return &p.slots[n-1]
}

func tileIndex(x, z int) int {
	return (x&0x3f)*mapsquareTiles + (z & 0x3f)
}

// Walkable reports whether a tile can be stood on at all.
func (p *Pack) Walkable(x, z, level int) bool {
	slot := p.slotAt(x, z, level)
	if slot == nil {
		return false
	}
	idx := tileIndex(x, z)
	return slot.walk[idx>>3]&(1<<uint(idx&7)) != 0
}

// ExitMask returns the tile's 8-directional exit bitmask (bit N set = direction N in
// the DX/DZ tables in astar.go is a legal step from this tile), 0 if the mapsquare
// isn't in the pack.
func (p *Pack) ExitMask(x, z, level int) byte {
	if v, ok := p.exitOverrides[nodeID(x, z, level)]; ok {
		return v
	}
	slot := p.slotAt(x, z, level)
	if slot == nil {
		return 0
	}
	return slot.exit[tileIndex(x, z)]
}

// WallMask returns the tile's wall-facing nibble (bit 0=north,1=east,2=south,3=west
// blocked by a wall on that face — matching PathFinder.ts's CARDINAL_SIDES facing bit
// order), 0 for a pre-v2 pack or a tile outside it.
func (p *Pack) WallMask(x, z, level int) byte {
	if v, ok := p.wallOverrides[nodeID(x, z, level)]; ok {
		return v
	}
	slot := p.slotAt(x, z, level)
	if slot == nil || slot.wall == nil {
		return 0
	}
	idx := tileIndex(x, z)
	b := slot.wall[idx>>1]
	if idx&1 != 0 {
		b >>= 4
	}
	return b & 0xf
}
