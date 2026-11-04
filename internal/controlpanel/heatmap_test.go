package controlpanel

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var heatT0 = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

func pos(name string, x, z, level int) AccountPosition {
	return AccountPosition{Name: name, X: x, Z: z, Level: level}
}

// cellsOf turns a flat answer into x,z -> count.
func cellsOf(t *testing.T, b HeatmapBody) map[[2]int]int {
	t.Helper()
	if len(b.Cells)%3 != 0 {
		t.Fatalf("cells has %d values, not a multiple of 3", len(b.Cells))
	}
	out := map[[2]int]int{}
	for i := 0; i < len(b.Cells); i += 3 {
		out[[2]int{b.Cells[i], b.Cells[i+1]}] = b.Cells[i+2]
	}
	return out
}

func TestHeatKeyRoundTrip(t *testing.T) {
	for _, c := range [][3]int{{0, 0, 0}, {3222, 3218, 0}, {2809, 3435, 1}, {3000, 10362, 3}, {heatCoordMax, heatCoordMax, 3}} {
		k, ok := heatKey(c[0], c[1], c[2], 0)
		if !ok {
			t.Fatalf("%v: not packable", c)
		}
		if x, z, l := heatUnkey(k); x != c[0] || z != c[1] || l != c[2] {
			t.Errorf("%v came back as %d,%d,%d", c, x, z, l)
		}
	}
	for _, c := range [][3]int{{-1, 0, 0}, {0, heatCoordMax + 1, 0}, {0, 0, 4}, {0, 0, -1}} {
		if _, ok := heatKey(c[0], c[1], c[2], 0); ok {
			t.Errorf("%v should not pack", c)
		}
	}
}

func TestHeatRecordAggregatesByAccountLevelCellAndBox(t *testing.T) {
	h := newHeatStore(0)
	for i := 0; i < 3; i++ {
		h.record([]AccountPosition{pos("alice", 3222, 3218, 0), pos("bob", 3223, 3219, 0), pos("bob", 3222, 3218, 1)}, heatT0.Add(time.Duration(i)*time.Second))
	}
	all := h.query(heatQuery{level: -1, window: "all", cell: 1}, heatT0)
	got := cellsOf(t, all)
	if got[[2]int{3222, 3218}] != 6 || got[[2]int{3223, 3219}] != 3 || all.Total != 9 || all.Max != 6 {
		t.Fatalf("all accounts, all levels: %v total %d max %d", got, all.Total, all.Max)
	}
	if want := []string{"alice", "bob"}; strings.Join(all.Accounts, ",") != strings.Join(want, ",") {
		t.Errorf("accounts %v, want %v", all.Accounts, want)
	}
	bob0 := cellsOf(t, h.query(heatQuery{account: "bob", level: 0, window: "all", cell: 1}, heatT0))
	if len(bob0) != 1 || bob0[[2]int{3223, 3219}] != 3 {
		t.Errorf("bob on floor 0: %v", bob0)
	}
	// cell 4: 3220..3223 x 3216..3219 is one cell holding everything.
	coarse := h.query(heatQuery{level: -1, window: "all", cell: 4}, heatT0)
	if c := cellsOf(t, coarse); len(c) != 1 || c[[2]int{3220, 3216}] != 9 || coarse.Cell != 4 {
		t.Errorf("cell 4: %v (cell %d)", c, coarse.Cell)
	}
	boxed := cellsOf(t, h.query(heatQuery{level: -1, window: "all", cell: 1, bbox: true, x0: 3223, z0: 3219, x1: 3300, z1: 3300}, heatT0))
	if len(boxed) != 1 || boxed[[2]int{3223, 3219}] != 3 {
		t.Errorf("boxed: %v", boxed)
	}
	if nobody := h.query(heatQuery{account: "carol", level: -1, window: "all", cell: 1}, heatT0); len(nobody.Cells) != 0 {
		t.Errorf("an unknown account has cells: %v", nobody.Cells)
	}
}

// The live fleet stands on the surface (z about 3100-3500) and in the underground (z about
// 9500-10300). Fitting the panel to the box around both centred it on z ~6700, an empty band
// between them, at the furthest zoom out; the panel then asked for x 0-7036, z 4874-8558 and
// drew nothing. Areas reports each part separately, whatever box the query asked for.
func TestHeatAreasSplitSurfaceFromUndergroundAndIgnoreTheBox(t *testing.T) {
	h := newHeatStore(0)
	for i := 0; i < 5; i++ {
		h.record([]AccountPosition{pos("alice", 3222, 3218, 0), pos("bob", 2600, 3100, 0), pos("carol", 3100, 9900, 0)}, heatT0.Add(time.Duration(i)*time.Second))
	}
	h.record([]AccountPosition{pos("dave", 2900, 10300, 1)}, heatT0)
	// A box over the empty band between them: no cells, but the areas still say where to look.
	body := h.query(heatQuery{level: -1, window: "all", cell: 32, bbox: true, x0: 0, z0: 4874, x1: 7036, z1: 8558}, heatT0)
	if len(body.Cells) != 0 || body.Total != 0 {
		t.Fatalf("the empty band has cells: %v", body.Cells)
	}
	want := []HeatArea{
		{Name: "surface", X0: 2600, Z0: 3100, X1: 3222, Z1: 3218, Total: 10},
		{Name: "underground", X0: 2900, Z0: 9900, X1: 3100, Z1: 10300, Total: 6},
	}
	if len(body.Areas) != len(want) {
		t.Fatalf("areas %+v, want %+v", body.Areas, want)
	}
	for i := range want {
		if body.Areas[i] != want[i] {
			t.Errorf("area %d: %+v, want %+v", i, body.Areas[i], want[i])
		}
	}
	// Busiest first, and only the floor, account and window asked for.
	carol := h.query(heatQuery{account: "carol", level: -1, window: "all", cell: 1}, heatT0)
	if len(carol.Areas) != 1 || carol.Areas[0].Name != "underground" || carol.Areas[0].Total != 5 {
		t.Errorf("carol: %+v", carol.Areas)
	}
	floor1 := h.query(heatQuery{level: 1, window: "all", cell: 1}, heatT0)
	if len(floor1.Areas) != 1 || floor1.Areas[0] != (HeatArea{Name: "underground", X0: 2900, Z0: 10300, X1: 2900, Z1: 10300, Total: 1}) {
		t.Errorf("floor 1: %+v", floor1.Areas)
	}
	if nobody := h.query(heatQuery{account: "erin", level: -1, window: "all", cell: 1}, heatT0); nobody.Areas == nil || len(nobody.Areas) != 0 {
		t.Errorf("an unknown account: areas %#v, want an empty list", nobody.Areas)
	}
	// A coarsened store reports whole cells.
	h.coarsenLocked()
	h.coarsenLocked()
	coarse := h.query(heatQuery{level: -1, window: "all", cell: 1}, heatT0)
	if a := coarse.Areas[0]; a.Name != "surface" || a.X0 != 2600 || a.Z0 != 3100 || a.X1 != 3223 || a.Z1 != 3219 || a.Total != 10 {
		t.Errorf("coarsened surface: %+v", a)
	}
}

func TestHeatWindowsCoverTheLastHourAndDay(t *testing.T) {
	h := newHeatStore(0)
	h.record([]AccountPosition{pos("a", 3000, 3000, 0)}, heatT0)                                 // 5h ago
	h.record([]AccountPosition{pos("a", 3100, 3100, 0)}, heatT0.Add(4*time.Hour-10*time.Minute)) // 70 minutes ago
	h.record([]AccountPosition{pos("a", 3200, 3200, 0)}, heatT0.Add(5*time.Hour-time.Minute))    // just now
	now := heatT0.Add(5 * time.Hour)
	total := func(window string) uint64 {
		return h.query(heatQuery{level: -1, window: window, cell: 1}, now).Total
	}
	if got := total("hour"); got != 1 {
		t.Errorf("hour: %d samples, want 1", got)
	}
	if got := total("day"); got != 3 {
		t.Errorf("day: %d samples, want 3", got)
	}
	if got := total("all"); got != 3 {
		t.Errorf("all: %d samples, want 3", got)
	}
	// Two days on, the day window has emptied but all time keeps everything.
	later := heatT0.Add(50 * time.Hour)
	h.record(nil, later)
	if got := h.query(heatQuery{level: -1, window: "day", cell: 1}, later).Total; got != 0 {
		t.Errorf("day after two days: %d, want 0", got)
	}
	if got := h.query(heatQuery{level: -1, window: "all", cell: 1}, later).Total; got != 3 {
		t.Errorf("all after two days: %d, want 3", got)
	}
	// Aged-out buckets gave their cells back.
	h.mu.Lock()
	want := 2 * 3 // all-time per account + fleet, three tiles each; no bucket survives
	if h.cells != want {
		t.Errorf("cells %d after the tiers aged out, want %d", h.cells, want)
	}
	h.mu.Unlock()
}

func TestHeatCapCoarsensThenRefusesNewCells(t *testing.T) {
	h := newHeatStore(400)
	now := heatT0
	for i := 0; i < 2000; i++ {
		h.record([]AccountPosition{pos("a", 3000+i%97*9, 3000+i/97*9, 0)}, now)
		h.mu.Lock()
		// One sample can land at most 2+len(heatTiers) new cells per account before the
		// cap is checked, so that is the most it can ever be over by.
		if h.cells > h.maxCells+2+len(heatTiers) {
			t.Fatalf("step %d: %d cells over the %d cap", i, h.cells, h.maxCells)
		}
		h.mu.Unlock()
	}
	info := h.info()
	if info.StoreCell != 8 || !info.Full || info.Refused == 0 {
		t.Fatalf("after 2000 far-apart tiles under a 400-cell cap: %+v", info)
	}
	// Existing cells still count; totals are never lost to coarsening.
	if got := h.query(heatQuery{level: -1, window: "all", cell: 1}, now); got.Cell != 8 {
		t.Errorf("answer cell %d, want the store's 8", got.Cell)
	}
}

func TestHeatCoarsenKeepsTotals(t *testing.T) {
	h := newHeatStore(0)
	for i := 0; i < 50; i++ {
		h.record([]AccountPosition{pos("a", 3200+i, 3200+i%7, i%2)}, heatT0)
	}
	before := h.query(heatQuery{level: -1, window: "all", cell: 1}, heatT0).Total
	h.mu.Lock()
	h.coarsenLocked()
	h.coarsenLocked()
	h.mu.Unlock()
	after := h.query(heatQuery{level: -1, window: "day", cell: 1}, heatT0)
	if after.Total != before || after.Cell != 4 {
		t.Errorf("after coarsening to 4: total %d (was %d), cell %d", after.Total, before, after.Cell)
	}
}

// TestHeatSurvivesARestart is the owner's persistence requirement: record, save, start a new
// server on the same file, and the counts are intact and keep accumulating.
func TestHeatSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heat", DefaultHeatmapFile)
	cfg := Config{Enabled: true, Listen: "127.0.0.1:0", HeatmapFile: path}
	first, err := newServer(cfg, NewFakeSource(2, 1), testOptions())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		first.heat.store.record([]AccountPosition{pos("alice", 3222, 3218, 0), pos("bob", 2809, 3435, 1)}, time.Now())
	}
	first.heat.close() // the shutdown save
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no file after shutdown: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".*.tmp")); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}

	second, err := newServer(cfg, NewFakeSource(2, 1), testOptions())
	if err != nil {
		t.Fatal(err)
	}
	q := heatQuery{account: "alice", level: -1, window: "all", cell: 1}
	if got := cellsOf(t, second.heat.query(q)); got[[2]int{3222, 3218}] != 5 {
		t.Fatalf("after restart alice has %v, want 5 on 3222,3218", got)
	}
	if got := second.heat.query(heatQuery{level: 1, window: "day", cell: 1}); got.Total != 5 {
		t.Errorf("after restart the day window holds %d on floor 1, want bob's 5", got.Total)
	}
	second.heat.store.record([]AccountPosition{pos("alice", 3222, 3218, 0)}, time.Now())
	if got := cellsOf(t, second.heat.query(q)); got[[2]int{3222, 3218}] != 6 {
		t.Errorf("after restart and one more sample alice has %v, want 6", got)
	}
	second.heat.close()
	third, err := newServer(cfg, NewFakeSource(2, 1), testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got := third.heat.query(heatQuery{level: -1, window: "all", cell: 1}); got.Total != 11 || got.Store.Samples != 11 {
		t.Errorf("after a second restart: total %d samples %d, want 11", got.Total, got.Store.Samples)
	}
}

func TestHeatUnreadableFileIsMovedAsideNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "heat.json.gz")
	if err := os.WriteFile(path, []byte("not gzip"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := newServer(Config{Enabled: true, Listen: "127.0.0.1:0", HeatmapFile: path}, NewFakeSource(1, 1), testOptions())
	if err != nil {
		t.Fatal(err)
	}
	aside, _ := filepath.Glob(path + ".unreadable-*")
	if len(aside) != 1 {
		t.Fatalf("moved-aside files: %v", aside)
	}
	if data, _ := os.ReadFile(aside[0]); string(data) != "not gzip" {
		t.Errorf("the unreadable file's contents changed: %q", data)
	}
	s.heat.store.record([]AccountPosition{pos("a", 1, 1, 0)}, time.Now())
	if err := s.heat.save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("no fresh file: %v", err)
	}
}

func TestHeatOffAndPositionlessSourcesHaveNoHeatMap(t *testing.T) {
	h := newHarness(t, Config{HeatmapOff: true}, testOptions(), nil)
	if res := h.get("/api/heatmap"); res.StatusCode != http.StatusNotImplemented {
		t.Errorf("heat map off: %d, want 501", res.StatusCode)
	}
	h2 := newHarness(t, Config{}, testOptions(), &staticSource{})
	if res := h2.get("/api/heatmap"); res.StatusCode != http.StatusNotImplemented {
		t.Errorf("source without positions: %d, want 501", res.StatusCode)
	}
}

func TestHeatmapEndpoint(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), nil)
	h.srv.heat.store.record([]AccountPosition{pos("alice", 3222, 3218, 0), pos("bob", 3230, 3218, 2)}, time.Now())
	var body HeatmapBody
	decode(t, h.get("/api/heatmap?window=hour&level=2&cell=2"), &body)
	if body.Cell != 2 || body.Level != 2 || body.Window != "hour" || len(body.Cells) != 3 || body.Cells[0] != 3230 || body.Cells[2] != 1 {
		t.Errorf("floor 2, cell 2: %+v", body)
	}
	decode(t, h.get("/api/heatmap?account=alice&x0=3200&z0=3200&x1=3225&z1=3225"), &body)
	if body.Account != "alice" || body.Total != 1 {
		t.Errorf("alice boxed: %+v", body)
	}
	for _, bad := range []string{"?level=4", "?level=x", "?window=week", "?cell=3", "?cell=2048", "?x0=1", "?x0=5&z0=0&x1=1&z1=9", "?account=" + strings.Repeat("a", maxNameBytes+1)} {
		if res := h.get("/api/heatmap" + bad); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, res.StatusCode)
		}
	}
	if res := h.request(http.MethodPost, "/api/heatmap", nil, nil); res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/heatmap: %d, want 405", res.StatusCode)
	}
}

func TestHeatmapReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heat.json.gz")
	h := newHarness(t, Config{HeatmapFile: path}, testOptions(), nil)
	store := h.srv.heat.store
	store.record([]AccountPosition{pos("alice", 3222, 3218, 0), pos("bob", 3222, 3218, 0), pos("bob", 3300, 3300, 0)}, time.Now())
	if err := h.srv.heat.save(); err != nil {
		t.Fatal(err)
	}
	same := http.Header{"Origin": {h.ts.URL}, "Sec-Fetch-Site": {"same-origin"}, "Content-Type": {"application/json"}}
	post := func(body string, header http.Header) *http.Response {
		return h.request(http.MethodPost, "/api/heatmap/reset", strings.NewReader(body), header)
	}
	// Cross-origin, a form content type and GET are all refused, and nothing is cleared.
	if res := post(`{}`, http.Header{"Origin": {"http://evil.example"}, "Content-Type": {"application/json"}}); res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin: %d, want 403", res.StatusCode)
	}
	if res := post(`account=`, http.Header{"Origin": {h.ts.URL}, "Content-Type": {"application/x-www-form-urlencoded"}}); res.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("form post: %d, want 415", res.StatusCode)
	}
	if res := h.get("/api/heatmap/reset"); res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET reset: %d, want 405", res.StatusCode)
	}
	if res := post(`{"account":"carol"}`, same); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown account: %d, want 404", res.StatusCode)
	}
	if got := store.query(heatQuery{level: -1, window: "all", cell: 1}, time.Now()).Total; got != 3 {
		t.Fatalf("refused requests cleared something: total %d", got)
	}

	// One account: its cells go, the fleet total drops by exactly its counts, others stay.
	if res := post(`{"account":"bob"}`, same); res.StatusCode != http.StatusOK {
		t.Fatalf("reset bob: %d %s", res.StatusCode, bodyText(t, res))
	}
	all := store.query(heatQuery{level: -1, window: "all", cell: 1}, time.Now())
	if c := cellsOf(t, all); all.Total != 1 || c[[2]int{3222, 3218}] != 1 || strings.Join(all.Accounts, ",") != "alice" {
		t.Errorf("after resetting bob: %v total %d accounts %v", c, all.Total, all.Accounts)
	}
	if got := store.query(heatQuery{level: -1, window: "day", cell: 1}, time.Now()).Total; got != 1 {
		t.Errorf("day window after resetting bob: %d, want 1", got)
	}
	store.mu.Lock()
	if store.cells != 4 { // alice: all-time, fleet, hour bucket, day bucket
		t.Errorf("cells %d after resetting bob, want 4", store.cells)
	}
	store.mu.Unlock()
	// The file was rewritten: a restart sees bob gone.
	reloaded, err := newServer(Config{Enabled: true, Listen: "127.0.0.1:0", HeatmapFile: path}, NewFakeSource(1, 1), testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.heat.query(heatQuery{level: -1, window: "all", cell: 1}); got.Total != 1 {
		t.Errorf("reloaded after resetting bob: total %d, want 1", got.Total)
	}

	// Everything.
	if res := post(`{"account":""}`, same); res.StatusCode != http.StatusOK {
		t.Fatalf("reset all: %d", res.StatusCode)
	}
	if got := store.query(heatQuery{level: -1, window: "all", cell: 1}, time.Now()); got.Total != 0 || len(got.Accounts) != 0 || got.Store.Cells != 0 {
		t.Errorf("after reset all: %+v", got)
	}
	reloaded, err = newServer(Config{Enabled: true, Listen: "127.0.0.1:0", HeatmapFile: path}, NewFakeSource(1, 1), testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.heat.query(heatQuery{level: -1, window: "all", cell: 1}); got.Total != 0 {
		t.Errorf("reloaded after reset all: total %d, want 0", got.Total)
	}
}

// positionsSource is a staticSource that also reports fixed positions.
type positionsSource struct {
	staticSource
	positions []AccountPosition
}

func (p *positionsSource) Positions(dst []AccountPosition) []AccountPosition {
	return append(dst[:0], p.positions...)
}

// TestHeatRecorderSamplesAndSaves runs the real loop against a FakeSource.
func TestHeatRecorderSamplesAndSaves(t *testing.T) {
	oldSample, oldSave := heatSampleInterval, heatSaveInterval
	heatSampleInterval, heatSaveInterval = 5*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { heatSampleInterval, heatSaveInterval = oldSample, oldSave })
	src := &positionsSource{positions: []AccountPosition{pos("a", 3222, 3218, 0), pos("b", 3000, 3000, 0)}}
	path := filepath.Join(t.TempDir(), "heat.json.gz")
	r := newHeatRecorder(Config{HeatmapFile: path}, src, time.Now)
	r.start()
	waitFor(t, "samples and a save", func() bool {
		_, err := os.Stat(path)
		return r.store.info().Samples >= 4 && err == nil
	})
	r.close()
	reloaded := newHeatRecorder(Config{HeatmapFile: path}, src, time.Now)
	if got, want := reloaded.store.info().Samples, r.store.info().Samples; got != want {
		t.Errorf("reloaded %d samples, the closed recorder had %d", got, want)
	}
}

// TestHeatMemoryPerCell measures what one stored cell costs, for the numbers in
// DefaultHeatmapMaxCells' doc comment.
func TestHeatMemoryPerCell(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates ~40 MB")
	}
	const accounts, perAccount = 100, 2000
	before := heapAfterGC()
	h := newHeatStore(10 * accounts * perAccount)
	batch := make([]AccountPosition, accounts)
	for i := 0; i < perAccount; i++ {
		for a := range batch {
			batch[a] = pos(string(rune('A'+a%26))+string(rune('a'+a/26)), 2000+i%1000, 3000+a*3+i/1000, 0)
		}
		h.record(batch, heatT0)
	}
	after := heapAfterGC()
	cells := h.info().Cells
	perCell := float64(after-before) / float64(cells)
	t.Logf("%d cells in %.1f MB: %.1f bytes per cell", cells, float64(after-before)/(1<<20), perCell)
	if perCell > 100 {
		t.Errorf("%.1f bytes per cell", perCell)
	}
	f, _ := h.snapshotFile()
	data, err := encodeHeatFile(f)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("saved file %d bytes (%.1f bytes per cell)", len(data), float64(len(data))/float64(cells))
	runtime.KeepAlive(h)
}

// benchHeatStore is 266 accounts (the live roster) that have each visited 2000 tiles in the
// last hour: 266*2000*4 = ~2.1M entries, past the default cap, so it measures the coarsened
// worst case too.
func benchHeatStore(b *testing.B, perAccount int) *heatStore {
	b.Helper()
	h := newHeatStore(0)
	batch := make([]AccountPosition, 266)
	for i := 0; i < perAccount; i++ {
		for a := range batch {
			batch[a] = pos(string(rune('A'+a%26))+string(rune('a'+a/26)), 2400+a*4+i%40, 3000+i/40, 0)
		}
		h.record(batch, heatT0)
	}
	return h
}

func BenchmarkHeatRecord266(b *testing.B) {
	h := benchHeatStore(b, 500)
	batch := make([]AccountPosition, 266)
	for a := range batch {
		batch[a] = pos(string(rune('A'+a%26))+string(rune('a'+a/26)), 2400+a*4, 3000, 0)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.record(batch, heatT0)
	}
}

func BenchmarkHeatQueryAllAccountsDay(b *testing.B) {
	h := benchHeatStore(b, 500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.query(heatQuery{level: -1, window: "day", cell: 1}, heatT0)
	}
}

func BenchmarkHeatQueryAllTimeFleetView(b *testing.B) {
	h := benchHeatStore(b, 500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.query(heatQuery{level: -1, window: "all", cell: 4, bbox: true, x0: 2400, z0: 2900, x1: 3500, z1: 3200}, heatT0)
	}
}

func BenchmarkHeatSave(b *testing.B) {
	h := benchHeatStore(b, 500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, _ := h.snapshotFile()
		data, err := encodeHeatFile(f)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(len(data)), "bytes")
	}
}
