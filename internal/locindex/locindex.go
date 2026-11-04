// Package locindex is the static loc index: every loc placed by the map files (bank
// booths, flax, spinning wheels, doors, trees). The server never sends these as packets;
// the client reads them from its map files, and the engine only reports locs that differ
// from the map (LOC_ADD_CHANGE, LOC_DEL). Scripts need both halves, so Query merges live
// changes from gamestate over this index (see query.go).
//
// The index is baked once from the 289 map data by cmd/locbake and loaded once per
// process, read-only and shared by every account. Records are 5 bytes each, grouped by
// mapsquare and sorted by level, x, z and shape.
//
// File format (gzip-compressed):
//
//	"LCLI" magic, u8 version (1), u16 mapsquare count
//	per mapsquare: u8 mx, u8 mz, u32 record count, then records:
//	  u16 loc id, u8 x | level<<6, u8 z | angle<<6, u8 shape
//
// x and z are local to the mapsquare. All integers are big-endian.
package locindex

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

const (
	magic      = "LCLI"
	version    = 1
	recordSize = 5
	// maxShape is LocShape's last value (GROUND_DECOR = 22).
	maxShape = 22
)

// Loc is one placed loc in absolute world coordinates.
type Loc struct {
	ID    int
	X, Z  int
	Level int
	Shape int
	Angle int
}

// shapeToLayer is LocShape.ts's LOC_SHAPE_TO_LAYER: walls, wall decoration, ground
// objects, ground decoration.
var shapeToLayer = [maxShape + 1]int{0, 0, 0, 0, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 3}

// Layer returns the scene layer a loc shape occupies, or -1 for an invalid shape. A tile
// holds at most one loc per layer, and LOC_ADD_CHANGE/LOC_DEL replace by layer.
func Layer(shape int) int {
	if shape < 0 || shape > maxShape {
		return -1
	}
	return shapeToLayer[shape]
}

// Index is a loaded static loc index. It is safe for concurrent use.
type Index struct {
	squares map[uint16][]byte
	count   int
}

func squareKey(mx, mz int) uint16 { return uint16(mx&0xff)<<8 | uint16(mz&0xff) }

// New builds an index from locs, which may be in any order.
func New(locs []Loc) (*Index, error) {
	bySquare := map[uint16][]Loc{}
	for _, l := range locs {
		if l.X < 0 || l.Z < 0 || l.X>>6 > 0xff || l.Z>>6 > 0xff || l.Level < 0 || l.Level > 3 || l.Angle < 0 || l.Angle > 3 || Layer(l.Shape) < 0 || l.ID < 0 || l.ID > 0xffff {
			return nil, fmt.Errorf("locindex: loc out of range: %+v", l)
		}
		k := squareKey(l.X>>6, l.Z>>6)
		bySquare[k] = append(bySquare[k], l)
	}
	ix := &Index{squares: make(map[uint16][]byte, len(bySquare))}
	for k, list := range bySquare {
		sort.Slice(list, func(i, j int) bool { return less(list[i], list[j]) })
		recs := make([]byte, len(list)*recordSize)
		for i, l := range list {
			encode(recs[i*recordSize:], l)
		}
		ix.squares[k] = recs
		ix.count += len(list)
	}
	return ix, nil
}

func less(a, b Loc) bool {
	if a.Level != b.Level {
		return a.Level < b.Level
	}
	if a.X != b.X {
		return a.X < b.X
	}
	if a.Z != b.Z {
		return a.Z < b.Z
	}
	if a.Shape != b.Shape {
		return a.Shape < b.Shape
	}
	return a.ID < b.ID
}

func encode(dst []byte, l Loc) {
	binary.BigEndian.PutUint16(dst, uint16(l.ID))
	dst[2] = byte(l.X&0x3f) | byte(l.Level)<<6
	dst[3] = byte(l.Z&0x3f) | byte(l.Angle)<<6
	dst[4] = byte(l.Shape)
}

func decode(rec []byte, mx, mz int) Loc {
	return Loc{
		ID:    int(binary.BigEndian.Uint16(rec)),
		X:     mx<<6 | int(rec[2]&0x3f),
		Z:     mz<<6 | int(rec[3]&0x3f),
		Level: int(rec[2] >> 6),
		Angle: int(rec[3] >> 6),
		Shape: int(rec[4]),
	}
}

// Count is the number of locs in the index.
func (ix *Index) Count() int { return ix.count }

// Squares is the number of mapsquares that hold at least one loc.
func (ix *Index) Squares() int { return len(ix.squares) }

// RecordBytes is the memory held by the records themselves.
func (ix *Index) RecordBytes() int { return ix.count * recordSize }

// At returns the locs whose origin is this tile.
func (ix *Index) At(x, z, level int) []Loc {
	var out []Loc
	ix.Each(x, z, x, z, level, func(l Loc) { out = append(out, l) })
	return out
}

// Each calls fn for every loc whose origin tile lies in the inclusive rectangle on level.
func (ix *Index) Each(minX, minZ, maxX, maxZ, level int, fn func(Loc)) {
	if minX < 0 {
		minX = 0
	}
	if minZ < 0 {
		minZ = 0
	}
	if maxX < minX || maxZ < minZ {
		return
	}
	for mx := minX >> 6; mx <= maxX>>6 && mx <= 0xff; mx++ {
		for mz := minZ >> 6; mz <= maxZ>>6 && mz <= 0xff; mz++ {
			recs := ix.squares[squareKey(mx, mz)]
			// Records are sorted by level first, so find this level's run.
			n := len(recs) / recordSize
			lo := sort.Search(n, func(i int) bool { return int(recs[i*recordSize+2]>>6) >= level })
			for i := lo; i < n; i++ {
				rec := recs[i*recordSize : (i+1)*recordSize]
				if int(rec[2]>>6) != level {
					break
				}
				l := decode(rec, mx, mz)
				if l.X >= minX && l.X <= maxX && l.Z >= minZ && l.Z <= maxZ {
					fn(l)
				}
			}
		}
	}
}

// Write stores the index in the gzip file format.
func Write(w io.Writer, ix *Index) error {
	keys := make([]int, 0, len(ix.squares))
	for k := range ix.squares {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	zw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(zw)
	bw.WriteString(magic)
	bw.WriteByte(version)
	var u16 [2]byte
	binary.BigEndian.PutUint16(u16[:], uint16(len(keys)))
	bw.Write(u16[:])
	for _, k := range keys {
		recs := ix.squares[uint16(k)]
		var head [6]byte
		head[0], head[1] = byte(k>>8), byte(k)
		binary.BigEndian.PutUint32(head[2:], uint32(len(recs)/recordSize))
		bw.Write(head[:])
		bw.Write(recs)
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	return zw.Close()
}

// ErrFormat is returned for a file that is not a valid loc index.
var ErrFormat = errors.New("locindex: not a valid loc index file")

// Load reads an index file written by Write (gzip or already decompressed).
func Load(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("locindex: %w", err)
	}
	defer f.Close()
	return Read(f)
}

// Read decodes an index from r, streaming, so the file is never held whole.
func Read(r io.Reader) (*Index, error) {
	br := bufio.NewReaderSize(r, 32<<10)
	var src io.Reader = br
	if head, err := br.Peek(2); err == nil && head[0] == 0x1f && head[1] == 0x8b {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("locindex: %w", err)
		}
		defer zr.Close()
		src = zr
	}
	var head [7]byte
	if _, err := io.ReadFull(src, head[:]); err != nil || !bytes.Equal(head[:4], []byte(magic)) {
		return nil, ErrFormat
	}
	if head[4] != version {
		return nil, fmt.Errorf("locindex: unsupported version %d", head[4])
	}
	squares := int(binary.BigEndian.Uint16(head[5:]))
	ix := &Index{squares: make(map[uint16][]byte, squares)}
	for i := 0; i < squares; i++ {
		var sq [6]byte
		if _, err := io.ReadFull(src, sq[:]); err != nil {
			return nil, fmt.Errorf("%w: truncated at mapsquare %d of %d", ErrFormat, i, squares)
		}
		n := binary.BigEndian.Uint32(sq[2:])
		if n > 1<<20 {
			return nil, fmt.Errorf("%w: mapsquare %d claims %d records", ErrFormat, i, n)
		}
		recs := make([]byte, int(n)*recordSize)
		if _, err := io.ReadFull(src, recs); err != nil {
			return nil, fmt.Errorf("%w: truncated records at mapsquare %d", ErrFormat, i)
		}
		for j := 0; j < len(recs); j += recordSize {
			if recs[j+4] > maxShape {
				return nil, fmt.Errorf("%w: shape %d", ErrFormat, recs[j+4])
			}
		}
		ix.squares[uint16(sq[0])<<8|uint16(sq[1])] = recs
		ix.count += int(n)
	}
	return ix, nil
}
