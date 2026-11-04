package eraassets

import (
	"os"
	"testing"

	"rsb0t-go/internal/archive"
)

// deriveFrameCount walks one sprite sheet the way decodeClassicSprite does, but instead of
// being TOLD how many frames to read it reads until the .dat's pixel data is exactly
// exhausted, and reports the count that lands on the last byte.
//
// This is the whole point of the test below. The sprite format carries no frame count, so
// a table that says the wrong number does not fail — it reads the NEXT sprite's metadata as
// this one's and returns images that look like something. Exact consumption is a check the
// file itself can answer: one frame too few leaves pixel bytes over, one too many runs off
// the end or reads a frame whose declared size cannot fit.
func deriveFrameCount(t *testing.T, spriteData, indexData []byte) (frames int, leftover int) {
	t.Helper()
	sp := &reader{data: spriteData}
	start, err := sp.u16()
	if err != nil || start > len(indexData) {
		t.Fatalf("sheet points at offset %d in a %d-byte index.dat (%v)", start, len(indexData), err)
	}
	idx := &reader{data: indexData, pos: start}
	if _, err := idx.u16(); err != nil { // plane width
		t.Fatalf("plane width: %v", err)
	}
	if _, err := idx.u16(); err != nil { // plane height
		t.Fatalf("plane height: %v", err)
	}
	palette, err := idx.u8()
	if err != nil || palette == 0 {
		t.Fatalf("palette size %d (%v)", palette, err)
	}
	for i := 1; i < palette; i++ {
		for c := 0; c < 3; c++ {
			if _, err := idx.u8(); err != nil {
				t.Fatalf("palette entry %d: %v", i, err)
			}
		}
	}
	for sp.pos < len(spriteData) {
		if _, err := idx.u8(); err != nil { // offsetX
			return frames, len(spriteData) - sp.pos
		}
		if _, err := idx.u8(); err != nil { // offsetY
			return frames, len(spriteData) - sp.pos
		}
		width, e1 := idx.u16()
		height, e2 := idx.u16()
		_, e3 := idx.u8() // pixel order
		if e1 != nil || e2 != nil || e3 != nil {
			return frames, len(spriteData) - sp.pos
		}
		if width <= 0 || height <= 0 || len(spriteData)-sp.pos < width*height {
			return frames, len(spriteData) - sp.pos
		}
		sp.pos += width * height
		frames++
	}
	return frames, len(spriteData) - sp.pos
}

func classicMedia58ForTest(t *testing.T) *archive.JagFile {
	t.Helper()
	root := os.Getenv(envRoot)
	if root == "" {
		t.Skipf("%s is not set: point it at an era asset directory to check real assets", envRoot)
	}
	store, err := Open(root)
	if err != nil {
		t.Skipf("no era assets under %s: %v", root, err)
	}
	if info := store.Era(EraClassic); !info.Present {
		t.Skipf("no Classic assets under %s: %s", store.Root(), info.Problem)
	}
	raw, err := store.ClassicArchive("media58.jag")
	if err != nil {
		t.Skipf("media58.jag is not where this test expects it: %v", err)
	}
	jf, err := archive.ParseJagFile(raw)
	if err != nil {
		t.Fatalf("media58.jag did not parse: %v", err)
	}
	return jf
}

// TestClassicSheetFrameCountsAreDerivedNotAssumed is the control for every hard-coded frame
// count in this package. It re-derives each sheet's count from the operator's own file and
// requires it to match the table — including the thirteen interface sheets, whose counts
// came from the reference decoder rather than from this method. Those thirteen agreeing is
// what lets the fourteen item sheets' derived 30 be trusted: the same method reproduces
// numbers it did not choose.
func TestClassicSheetFrameCountsAreDerivedNotAssumed(t *testing.T) {
	jf := classicMedia58ForTest(t)
	index, err := jf.Read("index.dat")
	if err != nil || len(index) == 0 {
		t.Fatalf("media58.jag index.dat: %v (%d bytes)", err, len(index))
	}
	type sheet struct {
		name   string
		frames int
		known  bool // the count came from the reference decoder, not from derivation
	}
	var sheets []sheet
	for _, e := range classicMedia58 {
		sheets = append(sheets, sheet{e.name, e.frames, true})
	}
	for _, e := range classicItemSheets {
		sheets = append(sheets, sheet{e.name, e.frames, false})
	}
	controls := 0
	for _, s := range sheets {
		body, err := jf.Read(s.name + ".dat")
		if err != nil || len(body) == 0 {
			t.Fatalf("media58.jag %s.dat: %v (%d bytes)", s.name, err, len(body))
		}
		got, leftover := deriveFrameCount(t, body, index)
		if got != s.frames || leftover != 0 {
			t.Errorf("%s.dat: table says %d frames, exact consumption says %d with %d bytes left over",
				s.name, s.frames, got, leftover)
		}
		if s.known {
			controls++
		}
	}
	if controls != len(classicMedia58) {
		t.Fatalf("only %d of %d reference-derived controls ran", controls, len(classicMedia58))
	}
}

// TestClassicItemIconsDecodeToRealImages checks the item art itself: every one of the 420
// frames decodes, at a sane icon size, and enough of them carry opaque pixels that the
// palette and transparency key are being applied rather than everything coming out clear.
func TestClassicItemIconsDecodeToRealImages(t *testing.T) {
	root := os.Getenv(envRoot)
	if root == "" {
		t.Skipf("%s is not set: point it at an era asset directory to check real assets", envRoot)
	}
	store, err := Open(root)
	if err != nil {
		t.Skipf("no era assets under %s: %v", root, err)
	}
	if info := store.Era(EraClassic); !info.Present {
		t.Skipf("no Classic assets under %s: %s", store.Root(), info.Problem)
	}
	raw, err := store.ClassicArchive("media58.jag")
	if err != nil {
		t.Skipf("media58.jag is not where this test expects it: %v", err)
	}
	icons, err := DecodeClassicItems(raw)
	if err != nil {
		t.Fatalf("DecodeClassicItems: %v", err)
	}
	if len(icons) != ClassicItemIconCount {
		t.Fatalf("decoded %d item icons, want %d", len(icons), ClassicItemIconCount)
	}
	opaque := 0
	for _, icon := range icons {
		if icon.Width <= 0 || icon.Height <= 0 || icon.Width > 48 || icon.Height > 48 {
			t.Fatalf("%s frame %d is %dx%d, which is not an item icon", icon.Name, icon.Frame, icon.Width, icon.Height)
		}
		if icon.PlaneWidth != 48 || icon.PlaneHeight != 32 {
			t.Fatalf("%s frame %d sits on a %dx%d plane; Classic's item plane is 48x32",
				icon.Name, icon.Frame, icon.PlaneWidth, icon.PlaneHeight)
		}
		bounds := icon.Image.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				if _, _, _, a := icon.Image.At(x, y).RGBA(); a > 0 {
					opaque++
				}
			}
		}
	}
	// Every icon is mostly transparent (an object on a 48x32 plane), but 420 of them
	// together must carry real coverage; near zero would mean the palette's transparency
	// key was matching everything.
	if opaque < 50000 {
		t.Fatalf("the 420 item icons hold only %d opaque pixels between them", opaque)
	}
}
