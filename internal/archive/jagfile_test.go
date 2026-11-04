package archive

import "testing"

// A container whose header sizes are equal stores its directory right after the 6-byte
// header, with each entry compressed separately (the config archive is stored this way).
// The directory must be read from offset 6 and entry offsets counted from the start of
// the container, as JagFile.ts does.
func TestUncompressedDirectoryStartsAfterHeader(t *testing.T) {
	src := []byte{
		0, 0, 30, 0, 0, 30, // unpacked size == packed size
		0, 2, // two entries
		0, 0, 0, 1, 0, 0, 5, 0, 0, 3, // hash 1, unpacked 5, packed 3
		0, 0, 0, 2, 0, 0, 9, 0, 0, 4, // hash 2, unpacked 9, packed 4
		1, 2, 3, 4, 5, 6, 7,
	}
	jf, err := ParseJagFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if jf.unpacked || len(jf.entries) != 2 {
		t.Fatalf("unpacked=%v entries=%d", jf.unpacked, len(jf.entries))
	}
	want := []fileEntry{{hash: 1, unpackedSize: 5, packedSize: 3, offset: 28}, {hash: 2, unpackedSize: 9, packedSize: 4, offset: 31}}
	for i, e := range jf.entries {
		if e != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, e, want[i])
		}
	}
}

func TestReadIndexPastTheEndIsAnError(t *testing.T) {
	jf := &JagFile{data: make([]byte, 10), entries: []fileEntry{{hash: 1, packedSize: 50, offset: 6}}}
	if _, err := jf.ReadIndex(0); err == nil {
		t.Fatal("an entry past the end of the container read without error")
	}
}
