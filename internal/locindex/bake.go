package locindex

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// linkBelow is the land flag that makes a loc on the level above count as the level
// below (a bridge), as GameMap.loadLocations applies it.
const linkBelow = 0x2

// MapFile is one mapsquare's land (m) and loc (l) files, decompressed.
type MapFile struct {
	MX, MZ int
	Land   []byte
	Loc    []byte
}

// ReadEngineMaps reads the map files an Engine-TS checkout packed, from the same places
// rsb0t/tools/nav/build-collision.ts reads them: data/pack/.cache/maps-server.zip, or
// the data/pack/server/maps directory.
func ReadEngineMaps(engineDir string) ([]MapFile, error) {
	files := map[string][]byte{}
	zipPath := filepath.Join(engineDir, "data", "pack", ".cache", "maps-server.zip")
	dirPath := filepath.Join(engineDir, "data", "pack", "server", "maps")
	if zr, err := zip.OpenReader(zipPath); err == nil {
		defer zr.Close()
		for _, f := range zr.File {
			if f.Name == "" || (f.Name[0] != 'm' && f.Name[0] != 'l') {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			files[f.Name] = b
		}
	} else if entries, err := os.ReadDir(dirPath); err == nil {
		for _, e := range entries {
			name := e.Name()
			if name == "" || (name[0] != 'm' && name[0] != 'l') {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dirPath, name))
			if err != nil {
				return nil, err
			}
			files[name] = b
		}
	} else {
		return nil, fmt.Errorf("locindex: no maps found at %s or %s", zipPath, dirPath)
	}

	var out []MapFile
	for name, land := range files {
		if name[0] != 'm' {
			continue
		}
		mx, mz, ok := parseSquareName(name[1:])
		if !ok {
			continue
		}
		loc, ok := files["l"+name[1:]]
		if !ok {
			return nil, fmt.Errorf("locindex: %s has no loc file", name)
		}
		var err error
		if land, err = gunzipIfNeeded(land); err != nil {
			return nil, fmt.Errorf("locindex: %s: %w", name, err)
		}
		if loc, err = gunzipIfNeeded(loc); err != nil {
			return nil, fmt.Errorf("locindex: l%s: %w", name[1:], err)
		}
		out = append(out, MapFile{MX: mx, MZ: mz, Land: land, Loc: loc})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MX != out[j].MX {
			return out[i].MX < out[j].MX
		}
		return out[i].MZ < out[j].MZ
	})
	return out, nil
}

func parseSquareName(s string) (int, int, bool) {
	a, b, ok := strings.Cut(s, "_")
	if !ok {
		return 0, 0, false
	}
	mx, err1 := strconv.Atoi(a)
	mz, err2 := strconv.Atoi(b)
	return mx, mz, err1 == nil && err2 == nil
}

func gunzipIfNeeded(b []byte) ([]byte, error) {
	if len(b) < 2 || b[0] != 0x1f || b[1] != 0x8b {
		return b, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(zr)
}

var errShort = errors.New("locindex: map file ends early")

type mapReader struct {
	b   []byte
	pos int
	err error
}

func (r *mapReader) g1() int {
	if r.err != nil || r.pos >= len(r.b) {
		r.err = errShort
		return 0
	}
	v := r.b[r.pos]
	r.pos++
	return int(v)
}

// smart is the map files' unsigned smart (one byte below 0x80, else two bytes - 0x8000).
func (r *mapReader) smart() int {
	if r.err != nil || r.pos >= len(r.b) {
		r.err = errShort
		return 0
	}
	if r.b[r.pos] < 0x80 {
		return r.g1()
	}
	return (r.g1()<<8 | r.g1()) - 0x8000
}

func packCoord(x, z, level int) int { return z&0x3f | (x&0x3f)<<6 | (level&3)<<12 }

// landFlags ports tools/nav/lib.ts parseLands: the per-tile flag byte of each level.
func landFlags(land []byte) ([]int8, error) {
	flags := make([]int8, 4*64*64)
	r := &mapReader{b: land}
	for level := 0; level < 4; level++ {
		for x := 0; x < 64; x++ {
			for z := 0; z < 64; z++ {
				for r.err == nil {
					op := r.g1()
					if op == 0 {
						break
					} else if op == 1 {
						r.g1()
						break
					}
					if op <= 49 {
						r.g1()
					} else if op <= 81 {
						flags[packCoord(x, z, level)] = int8(op - 49)
					}
				}
			}
		}
	}
	return flags, r.err
}

// BakeSquare decodes one mapsquare's locs as GameMap.loadLocations places them: a loc on
// a tile whose level-1 land has the link-below flag moves down a level, and one that
// would fall below level 0 is dropped. keep, when not nil, filters by loc id.
func BakeSquare(m MapFile, keep func(id int) bool) ([]Loc, error) {
	flags, err := landFlags(m.Land)
	if err != nil {
		return nil, fmt.Errorf("locindex: m%d_%d: %w", m.MX, m.MZ, err)
	}
	r := &mapReader{b: m.Loc}
	var out []Loc
	id := -1
	for off := r.smart(); off != 0 && r.err == nil; off = r.smart() {
		id += off
		coord := 0
		for c := r.smart(); c != 0 && r.err == nil; {
			coord += c - 1
			z, x, level := coord&0x3f, (coord>>6)&0x3f, (coord>>12)&3
			info := r.g1()
			c = r.smart()

			var link int8
			if level == 1 {
				link = flags[coord] & linkBelow
			} else {
				link = flags[packCoord(x, z, 1)] & linkBelow
			}
			if link == linkBelow {
				level--
			}
			if level < 0 || (keep != nil && !keep(id)) {
				continue
			}
			out = append(out, Loc{ID: id, X: m.MX<<6 | x, Z: m.MZ<<6 | z, Level: level, Shape: info >> 2, Angle: info & 3})
		}
	}
	if r.err != nil {
		return nil, fmt.Errorf("locindex: l%d_%d: %w", m.MX, m.MZ, r.err)
	}
	return out, nil
}

// Bake builds an index from every mapsquare.
func Bake(maps []MapFile, keep func(id int) bool) (*Index, error) {
	var all []Loc
	for _, m := range maps {
		locs, err := BakeSquare(m, keep)
		if err != nil {
			return nil, err
		}
		all = append(all, locs...)
	}
	return New(all)
}
