// Package archive ports rsb0t-main's src/client/io/JagFile.ts (the cache container
// format) and src/client/config/IfType.ts (the interface component definitions inside
// it) — what a headless client needs to resolve the bank's inventory component the same
// dynamic way ClientAdapter.bankComId does, since (unlike trade) there is no fixed
// literal for it.
package archive

import (
	"bytes"
	"compress/bzip2"
	"fmt"
	"io"

	"rsb0t-go/internal/packet"
)

// GenHash ports JagFile.genHash: a simple rolling hash over the uppercased name, used
// to look up a file by name in the directory table (the table stores hashes, not names).
func GenHash(name string) int32 {
	var hash int32
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		hash = hash*61 + int32(c) - 32
	}
	return hash
}

type fileEntry struct {
	hash         int32
	unpackedSize int
	packedSize   int
	offset       int
}

// JagFile is a parsed container: a directory of named (well, hashed) entries, each
// independently readable (and independently bzip2-compressed, in the case where the
// whole container was stored uncompressed but its members are not).
type JagFile struct {
	data     []byte
	unpacked bool // true if the whole container was already bunzip2'd as one blob
	entries  []fileEntry
}

// unbzip2 restores the standard "BZh<level>" stream header RS's storage omits (see
// README.md's Phase 2 notes — verified this session against the live engine's own
// interface archive) and decodes with the standard library; no custom decoder needed.
func unbzip2(body []byte) ([]byte, error) {
	withHeader := make([]byte, 0, len(body)+4)
	withHeader = append(withHeader, 'B', 'Z', 'h', '1')
	withHeader = append(withHeader, body...)
	r := bzip2.NewReader(bytes.NewReader(withHeader))
	return io.ReadAll(r)
}

// ParseJagFile ports the JagFile constructor: a 3-byte unpacked size, a 3-byte packed
// size, and then either the raw directory (sizes equal) or one bzip2 blob covering the
// whole rest of the container (sizes differ).
func ParseJagFile(src []byte) (*JagFile, error) {
	r := packet.NewReader(src)
	unpackedSize := int(r.G3())
	packedSize := int(r.G3())

	jf := &JagFile{}
	dr := r
	if unpackedSize == packedSize {
		// The directory follows the 6-byte header in src itself, so keep reading from r
		// (JagFile.ts keeps its `data` Packet at pos 6 here). Each entry is compressed on
		// its own — the config archive is stored this way.
		jf.data = src
		jf.unpacked = false
	} else {
		body, err := unbzip2(src[6:])
		if err != nil {
			return nil, err
		}
		jf.data = body
		jf.unpacked = true
		dr = packet.NewReader(jf.data)
	}

	fileCount := int(dr.G2())
	jf.entries = make([]fileEntry, fileCount)

	offset := dr.Pos + fileCount*10
	for i := 0; i < fileCount; i++ {
		hash := dr.G4()
		unpackedSz := int(dr.G3())
		packedSz := int(dr.G3())
		jf.entries[i] = fileEntry{hash: hash, unpackedSize: unpackedSz, packedSize: packedSz, offset: offset}
		offset += packedSz
	}
	return jf, nil
}

// Len is how many entries the container's directory holds. The directory stores name
// HASHES, not names, so there is no way to list what is in a container — but the count
// is a real, checkable property of it, and the only way a caller can walk one whose
// entry names it does not already know (which is the normal case for a cache archive
// from an era this repo has no name table for).
func (jf *JagFile) Len() int { return len(jf.entries) }

// Has reports whether the container holds an entry with this name, without decompressing
// it. Cheaper than Read when the question is "does this archive have X" — for instance
// when deciding whether a cache directory is the revision a caller expects.
func (jf *JagFile) Has(name string) bool {
	hash := GenHash(name)
	for _, e := range jf.entries {
		if e.hash == hash {
			return true
		}
	}
	return false
}

// Read looks up a file by name (hashed the same way the directory stores it) and
// returns its decompressed bytes.
func (jf *JagFile) Read(name string) ([]byte, error) {
	hash := GenHash(name)
	for i, e := range jf.entries {
		if e.hash == hash {
			return jf.ReadIndex(i)
		}
	}
	return nil, nil
}

// ReadIndex returns one directory entry's decompressed bytes.
func (jf *JagFile) ReadIndex(index int) ([]byte, error) {
	if index < 0 || index >= len(jf.entries) {
		return nil, nil
	}
	e := jf.entries[index]
	if e.offset < 0 || e.packedSize < 0 || e.offset+e.packedSize > len(jf.data) {
		return nil, fmt.Errorf("archive: entry %d (%d bytes at %d) runs past the %d-byte container", index, e.packedSize, e.offset, len(jf.data))
	}
	src := jf.data[e.offset : e.offset+e.packedSize]
	if jf.unpacked {
		// The whole container was already decompressed as one blob; this slice is plain.
		return src, nil
	}
	return unbzip2(src)
}
