package pathfind

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// packEnv names the collision pack for the tests that need the real one. There is no
// default path: the pack lives in whichever rsb0t checkout the developer has, so these
// tests skip when it is unset.
const packEnv = "RSB0T_COLLISION_PACK"

// refPack reads an LCNV pack the plainest possible way, straight off the decompressed
// bytes with no sharing, so the tests can hold Pack's storage to it tile for tile.
type refPack struct {
	version int
	slots   map[int]refSlot
}

type refSlot struct{ exit, walk, wall []byte }

func parseRef(t testing.TB, b []byte) *refPack {
	t.Helper()
	r := &refPack{version: int(b[4]), slots: map[int]refSlot{}}
	count := int(b[8]) | int(b[9])<<8
	pos := 10
	for i := 0; i < count; i++ {
		mx, mz, mask := int(b[pos]), int(b[pos+1]), b[pos+2]
		pos += 3
		for level := 0; level < levels; level++ {
			if mask&(1<<uint(level)) == 0 {
				continue
			}
			s := refSlot{exit: b[pos : pos+exitBytes], walk: b[pos+exitBytes : pos+exitBytes+walkBytes]}
			pos += exitBytes + walkBytes
			if r.version >= 2 {
				s.wall = b[pos : pos+wallBytes]
				pos += wallBytes
			}
			r.slots[level<<16|mx<<8|mz] = s
		}
	}
	if pos != len(b) {
		t.Fatalf("reference reader ended at %d of %d", pos, len(b))
	}
	return r
}

func (r *refPack) tiles(x, z, level int) (walk bool, exit, wall byte) {
	s, ok := r.slots[level<<16|(x>>6)<<8|z>>6]
	if !ok {
		return false, 0, 0
	}
	idx := (x&0x3f)*64 + z&0x3f
	walk = s.walk[idx>>3]&(1<<uint(idx&7)) != 0
	exit = s.exit[idx]
	if s.wall != nil {
		wall = s.wall[idx>>1]
		if idx&1 != 0 {
			wall >>= 4
		}
		wall &= 0xf
	}
	return walk, exit, wall
}

func comparePack(t *testing.T, p *Pack, r *refPack, extra [][3]int) {
	t.Helper()
	check := func(x, z, level int) {
		w, e, wl := r.tiles(x, z, level)
		if p.Walkable(x, z, level) != w || p.ExitMask(x, z, level) != e || p.WallMask(x, z, level) != wl {
			t.Fatalf("tile %d,%d,%d: pack (%v,%d,%d) reference (%v,%d,%d)", x, z, level,
				p.Walkable(x, z, level), p.ExitMask(x, z, level), p.WallMask(x, z, level), w, e, wl)
		}
	}
	for key := range r.slots {
		level, mx, mz := key>>16, (key>>8)&0xff, key&0xff
		for lx := 0; lx < 64; lx++ {
			for lz := 0; lz < 64; lz++ {
				check(mx<<6|lx, mz<<6|lz, level)
			}
		}
	}
	for _, c := range extra {
		check(c[0], c[1], c[2])
	}
}

// syntheticPack builds a small pack with repeated and all-zero slots, the two cases the
// loader shares storage for.
func syntheticPack(version int) []byte {
	rng := rand.New(rand.NewSource(7))
	slotLen := exitBytes + walkBytes
	if version >= 2 {
		slotLen += wallBytes
	}
	random := make([]byte, slotLen)
	rng.Read(random)
	zero := make([]byte, slotLen)
	other := make([]byte, slotLen)
	rng.Read(other)

	var b bytes.Buffer
	b.Write([]byte{'L', 'C', 'N', 'V', byte(version), 1, 0, 0, 3, 0})
	b.Write([]byte{50, 50, 0b0101})
	b.Write(random)
	b.Write(other)
	b.Write([]byte{50, 51, 0b0001})
	b.Write(zero)
	b.Write([]byte{51, 50, 0b1001})
	b.Write(random)
	b.Write(zero)
	return b.Bytes()
}

func TestSyntheticPacksMatchReference(t *testing.T) {
	outside := [][3]int{{0, 0, 0}, {50<<6 - 1, 50 << 6, 0}, {50 << 6, 50 << 6, 1}, {52 << 6, 50 << 6, 0}}
	for _, version := range []int{1, 2} {
		raw := syntheticPack(version)
		p, err := Parse(raw)
		if err != nil {
			t.Fatalf("version %d: %v", version, err)
		}
		if p.Version != version || !p.Members || p.Mapsquares != 3 {
			t.Fatalf("version %d header: %+v", version, p)
		}
		comparePack(t, p, parseRef(t, raw), outside)

		var gz bytes.Buffer
		zw := gzip.NewWriter(&gz)
		zw.Write(raw)
		zw.Close()
		path := filepath.Join(t.TempDir(), "pack.lcnav.gz")
		if err := os.WriteFile(path, gz.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(path)
		if err != nil {
			t.Fatalf("version %d gzip load: %v", version, err)
		}
		comparePack(t, loaded, parseRef(t, raw), outside)
	}
}

func TestMalformedPacks(t *testing.T) {
	raw := syntheticPack(2)
	cases := map[string][]byte{
		"magic":     append([]byte("XCNV"), raw[4:]...),
		"version":   append(append([]byte{}, raw[:4]...), append([]byte{9}, raw[5:]...)...),
		"truncated": raw[:len(raw)-1],
		"trailing":  append(append([]byte{}, raw...), 0),
		"short":     raw[:5],
	}
	for name, b := range cases {
		if _, err := Parse(b); err == nil {
			t.Errorf("%s: parsed without error", name)
		}
	}
}

func realPack(t *testing.T) (raw []byte, path string) {
	t.Helper()
	path = os.Getenv(packEnv)
	if path == "" {
		t.Skipf("set %s to the collision pack (collision.lcnav or .gz) to run this test", packEnv)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if data, err = io.ReadAll(zr); err != nil {
			t.Fatal(err)
		}
	}
	return data, path
}

func TestRealPackMatchesReference(t *testing.T) {
	raw, path := realPack(t)
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	comparePack(t, p, parseRef(t, raw), [][3]int{{0, 0, 0}, {3200, 3200, 3}})
}

type corpusRoute struct {
	From, To  NavPoint
	OK        bool
	Reason    string
	Expanded  int
	Waypoints []NavPoint
}

type routeCorpus struct {
	PackSHA256 string
	Routes     []corpusRoute
}

const corpusPath = "testdata/routes.json"

// TestRoutesMatchCorpus replays a fixed corpus of FindPath calls, recorded with the
// original eager loader, and requires identical results: waypoints, reasons and expansion
// counts. Set RSB0T_WRITE_ROUTE_CORPUS=1 to record it again.
func TestRoutesMatchCorpus(t *testing.T) {
	raw, path := realPack(t)
	sum := sha256.Sum256(raw)
	packSHA := hex.EncodeToString(sum[:])
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if os.Getenv("RSB0T_WRITE_ROUTE_CORPUS") == "1" {
		c := routeCorpus{PackSHA256: packSHA}
		for _, pair := range corpusPairs(parseRef(t, raw)) {
			r := p.FindPath(pair[0], pair[1])
			c.Routes = append(c.Routes, corpusRoute{From: pair[0], To: pair[1], OK: r.OK, Reason: r.Reason, Expanded: r.Expanded, Waypoints: r.Waypoints})
		}
		out, _ := json.MarshalIndent(c, "", " ")
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(corpusPath, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d routes", len(c.Routes))
		return
	}

	data, err := os.ReadFile(corpusPath)
	if err != nil {
		t.Fatal(err)
	}
	var c routeCorpus
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if c.PackSHA256 != packSHA {
		t.Skipf("corpus was recorded against pack %s, this pack is %s", c.PackSHA256[:12], packSHA[:12])
	}
	for i, want := range c.Routes {
		got := p.FindPath(want.From, want.To)
		if got.OK != want.OK || got.Reason != want.Reason || got.Expanded != want.Expanded || !reflect.DeepEqual(got.Waypoints, want.Waypoints) {
			t.Fatalf("route %d %v -> %v: got ok=%v reason=%q expanded=%d waypoints=%v, want ok=%v reason=%q expanded=%d waypoints=%v",
				i, want.From, want.To, got.OK, got.Reason, got.Expanded, got.Waypoints, want.OK, want.Reason, want.Expanded, want.Waypoints)
		}
	}
}

// corpusPairs picks deterministic route queries off the reference reader: short and
// medium walks from walkable tiles on every level, with targets that may be solid so the
// beside-a-loc goal selection is exercised too.
func corpusPairs(r *refPack) [][2]NavPoint {
	keys := make([]int, 0, len(r.slots))
	for key := 0; key < levels<<16; key++ {
		if _, ok := r.slots[key]; ok {
			keys = append(keys, key)
		}
	}
	rng := rand.New(rand.NewSource(289))
	var pairs [][2]NavPoint
	for len(pairs) < 160 {
		key := keys[rng.Intn(len(keys))]
		level, mx, mz := key>>16, (key>>8)&0xff, key&0xff
		x, z := mx<<6|rng.Intn(64), mz<<6|rng.Intn(64)
		if walk, exit, _ := r.tiles(x, z, level); !walk || exit == 0 {
			continue
		}
		reach := 12
		switch {
		case len(pairs)%8 == 7:
			reach = 60
		case len(pairs)%3 == 2:
			reach = 30
		}
		to := NavPoint{X: x + rng.Intn(2*reach+1) - reach, Z: z + rng.Intn(2*reach+1) - reach, Level: level}
		pairs = append(pairs, [2]NavPoint{{X: x, Z: z, Level: level}, to})
	}
	return pairs
}
