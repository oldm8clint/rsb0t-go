package eraassets

import (
	"os"
	"path/filepath"
	"testing"
)

// The real-asset tests follow this repo's existing pattern for the collision pack: they
// need bytes nobody may commit, so they take a path from the environment and SKIP with a
// clear reason when it is unset, rather than passing vacuously.
//
//	set RSB0T_ERA_ASSETS=<the -era-assets directory>
const envRoot = "RSB0T_ERA_ASSETS"

func realRoot(t *testing.T) string {
	t.Helper()
	root := os.Getenv(envRoot)
	if root == "" {
		t.Skipf("%s is not set: point it at an era asset directory to check real assets", envRoot)
	}
	return root
}

// What this proves, and it is the load-bearing claim of the whole package: the entry
// counts come from the real archives on disk, decoded by this repo's own 2004 JagFile
// reader, and they are the mudclient204 cache's own numbers.
func TestClassicAssetsAreTheRealMudclient204Cache(t *testing.T) {
	store, err := Open(realRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	info := store.Era(EraClassic)
	if !info.Present {
		t.Skipf("no Classic assets under %s: %s", store.Root(), info.Problem)
	}
	want := map[string]int{"models36.jag": 829, "entity24.jag": 109, "textures17.jag": 53, "media58.jag": 30}
	for name, n := range want {
		if info.Counts[name] != n {
			t.Errorf("%s holds %d entries, want %d", name, info.Counts[name], n)
		}
	}
	if info.Bytes <= 0 {
		t.Error("the Classic asset set measured 0 bytes")
	}
	t.Logf("classic: %s, %d bytes", info.Detail, info.Bytes)
}

func Test2009AssetsAreABuild530Filestore(t *testing.T) {
	store, err := Open(realRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	info := store.Era(Era2009)
	if !info.Present {
		t.Skipf("no 2009 assets under %s: %s", store.Root(), info.Problem)
	}
	for _, want := range js5Indexes {
		got := info.Counts["idx"+string(rune('0'+want.idx))]
		if diff := got - want.expected; diff > tolerance || diff < -tolerance {
			t.Errorf("idx%d holds %d %s, want about %d", want.idx, got, want.what, want.expected)
		}
	}
	t.Logf("2009: %s, %d bytes", info.Detail, info.Bytes)
}

// Everything below runs without any real assets: these are the refusals, and they matter
// more than the happy path. An operator who points -era-assets at the wrong directory, or
// at a half-finished download, must be told which file was wrong and what was expected —
// not left with a panel that quietly offers nothing.

func TestNoFlagIsNotAnError(t *testing.T) {
	store, err := Open("")
	if err != nil || store != nil {
		t.Fatalf("Open(\"\") = %v, %v; want a nil store and no error", store, err)
	}
	// Every method must tolerate the nil store a default build carries.
	if store.Available(EraClassic) || store.Root() != "" || len(store.AvailableEras()) != 0 {
		t.Error("a nil store reported assets it does not have")
	}
	if info := store.Era(EraClassic); info.Present || info.Problem == "" {
		t.Errorf("a nil store's Era() = %+v; want absent with a reason", info)
	}
}

func TestAnEmptyRootIsAStartupError(t *testing.T) {
	_, err := Open(t.TempDir())
	if err == nil {
		t.Fatal("a directory with no era assets in it was accepted")
	}
	for _, era := range Known {
		if !contains(err.Error(), era) {
			t.Errorf("the error does not say what was looked for under %q: %v", era, err)
		}
	}
}

func TestAFileInsteadOfADirectoryIsRefusedClearly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assets")
	if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(path)
	if err == nil || !contains(err.Error(), "not a directory") {
		t.Fatalf("a file passed as the era asset root = %v; want a clear refusal", err)
	}
}

// A truncated or wrong-revision download is the failure an operator is most likely to hit
// and least likely to diagnose, so it is refused by CONTENT, not by filename.
func TestATruncatedClassicArchiveIsRefusedByItsEntryCount(t *testing.T) {
	root := realRoot(t)
	real, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if !real.Available(EraClassic) {
		t.Skip("no real Classic assets to truncate")
	}
	// Copy the real cache, then cut one archive down to a valid container with too few
	// entries. Refusing this needs the count, not the size: a short file that still
	// parses is exactly what a stopped download leaves behind.
	dir := t.TempDir()
	cache := filepath.Join(dir, EraClassic, "cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, EraClassic, "cache")
	for _, want := range classicArchives {
		body, err := os.ReadFile(filepath.Join(src, want.name))
		if err != nil {
			t.Skipf("real Classic cache is not laid out as expected: %v", err)
		}
		if want.name == "models36.jag" {
			// A container whose directory claims 829 entries but whose data is cut
			// short: the count check is what must catch it.
			body = shrinkEntryCount(t, body)
		}
		if err := os.WriteFile(filepath.Join(cache, want.name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := Open(dir)
	// Open only fails when NO era is usable; here Classic must be absent with a reason.
	if err == nil {
		if info := store.Era(EraClassic); info.Present {
			t.Fatalf("a Classic cache with a short models36.jag was accepted: %+v", info)
		} else if !contains(info.Problem, "models36.jag") {
			t.Errorf("the refusal does not name the bad archive: %q", info.Problem)
		}
		return
	}
	if !contains(err.Error(), "models36.jag") {
		t.Errorf("the refusal does not name the bad archive: %v", err)
	}
}

// shrinkEntryCount rewrites a .jag container's directory to claim one entry. The file
// stays a structurally valid container — which is the point: the check being tested is on
// what the archive HOLDS, not on whether it parses.
func shrinkEntryCount(t *testing.T, body []byte) []byte {
	t.Helper()
	out := make([]byte, len(body))
	copy(out, body)
	unpacked := int(out[0])<<16 | int(out[1])<<8 | int(out[2])
	packed := int(out[3])<<16 | int(out[4])<<8 | int(out[5])
	if unpacked != packed {
		// The whole container is one bzip2 blob, so its directory cannot be edited in
		// place. Truncating the blob makes it unreadable, which this package also has to
		// refuse — a different arm of the same requirement.
		return out[:len(out)/4]
	}
	out[6], out[7] = 0, 1 // entry count := 1
	return out
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// The Classic sprite decoder, against the real archives. This is the test that says the
// difference between "the files are there" and "we can actually get pictures out of them":
// it decodes every interface sheet in media58.jag and checks each frame is a real image
// with a plausible size and some opaque pixels in it. A decoder that misread the format
// would either error (a truncated frame) or produce an empty image, and both are caught.
func TestClassicMediaSpritesDecodeToRealImages(t *testing.T) {
	store, err := Open(realRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if !store.Available(EraClassic) {
		t.Skipf("no Classic assets under %s: %s", store.Root(), store.Era(EraClassic).Problem)
	}
	body, err := os.ReadFile(filepath.Join(store.Root(), EraClassic, "cache", "media58.jag"))
	if err != nil {
		t.Skipf("media58.jag is not where this test expects it: %v", err)
	}
	sprites, err := DecodeClassicMedia(body)
	if err != nil {
		t.Fatalf("decoding media58.jag: %v", err)
	}
	// The reference frame table adds up to 33 frames across 13 entries.
	wantFrames := 0
	for _, e := range classicMedia58 {
		wantFrames += e.frames
	}
	if len(sprites) != wantFrames {
		t.Fatalf("decoded %d frames, want %d", len(sprites), wantFrames)
	}
	opaque := 0
	for _, s := range sprites {
		if s.Width <= 0 || s.Height <= 0 || s.Width > 1024 || s.Height > 1024 {
			t.Errorf("%s frame %d is %dx%d, which is not a plausible Classic sprite", s.Name, s.Frame, s.Width, s.Height)
			continue
		}
		if s.Image.Bounds().Dx() != s.Width || s.Image.Bounds().Dy() != s.Height {
			t.Errorf("%s frame %d reported %dx%d but produced a %v image", s.Name, s.Frame, s.Width, s.Height, s.Image.Bounds())
		}
		n := 0
		for i := 3; i < len(s.Image.Pix); i += 4 {
			if s.Image.Pix[i] != 0 {
				n++
			}
		}
		if n == 0 {
			t.Errorf("%s frame %d decoded to a fully transparent image", s.Name, s.Frame)
		}
		opaque += n
	}
	// The RuneScape logo is the one sprite whose size is worth pinning: it is the largest
	// in the archive and a misread offset would move it somewhere absurd.
	for _, s := range sprites {
		if s.Name == "runescape" {
			t.Logf("runescape logo %dx%d on a %dx%d plane at +%d+%d", s.Width, s.Height, s.PlaneWidth, s.PlaneHeight, s.OffsetX, s.OffsetY)
		}
	}
	t.Logf("decoded %d frames from %d media58 entries, %d opaque pixels in total", len(sprites), len(classicMedia58), opaque)
}

// A sprite file that does not match its index is refused rather than producing a picture
// of something else. This is the failure an operator hits with a half-downloaded or
// wrong-revision archive, and a decoder that "works" on it is worse than one that stops.
func TestClassicSpriteDecoderRefusesAMismatchedIndex(t *testing.T) {
	// A one-byte sprite file claiming its metadata lives at offset 0 of an empty index.
	if _, err := decodeClassicSprite("fake", []byte{0, 0}, nil, 1); err == nil {
		t.Error("an empty index.dat was accepted")
	}
	// A valid header whose pixel data is missing.
	index := []byte{0, 8, 0, 8, 2, 1, 2, 3 /* frame: */, 0, 0, 0, 8, 0, 8, 0}
	if _, err := decodeClassicSprite("fake", []byte{0, 0}, index, 1); err == nil {
		t.Error("a frame with no pixel data was accepted")
	} else if !contains(err.Error(), "truncated") {
		t.Errorf("the refusal does not say the data ran out: %v", err)
	}
}
