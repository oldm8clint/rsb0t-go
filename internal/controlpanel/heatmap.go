package controlpanel

// The travel heat map: where our accounts have spent their time, accumulated for as long as
// the panel has run (and across restarts, through a small gzip file).
//
// Recording. A background loop asks the Source for every in-game account's tile once a
// second (heatSampleInterval) through PositionSource, which the live fleet answers with one
// gamestate lock per connected session (fleet.Manager.Positions) — deliberately NOT a
// whole-fleet status snapshot, which takes each session's lock about eight times and copies
// its inventories and log lines (see snapshotcache.go and cfbdf17 for why /api/status must
// not be starved). Each sample adds one to the account's tile, so a count is "seconds spent
// on that tile" at the default cadence.
//
// Storage. Counts are sparse maps keyed by a packed (level, x, z) cell, kept:
//   - all time, per account and for the fleet as a whole;
//   - in 5-minute buckets for the last hour ("hour" window), per account;
//   - in hourly buckets for the last 24 hours ("day" window), per account.
// Every map entry anywhere counts against one cap (Config.HeatmapMaxCells). Past it the
// whole store is coarsened — every cell becomes 2x2 tiles, then 4x4, then 8x8 — and past
// that no NEW cell is created (existing cells still count); the API reports both. The cap
// is checked after each sample, so the store can be over it by at most one sample's new
// cells (2+len(heatTiers) per account).
//
// Persistence. The whole store is written atomically (temp file, fsync, rename) every
// heatSaveInterval and on shutdown, and loaded at startup, so the map keeps building across
// fleet restarts. A file that cannot be read is moved aside, never silently overwritten.

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"
)

// PositionSource is optional. A Source that can report where its in-game accounts stand,
// cheaply, feeds the travel heat map; one without it has no heat map (/api/heatmap: 501).
type PositionSource interface {
	// Positions appends every in-game account's current tile to dst[:0].
	Positions(dst []AccountPosition) []AccountPosition
}

// AccountPosition is one in-game account's tile.
type AccountPosition struct {
	Name  string
	X, Z  int
	Level int
}

const (
	// DefaultHeatmapMaxCells bounds the store: 1M map entries. TestHeatMemoryPerCell
	// measured about 17 bytes of heap and 3.3 bytes of saved file per entry (Go 1.27), so
	// roughly 17 MB of heap and 3.5 MB on disk in the worst case.
	DefaultHeatmapMaxCells = 1_000_000
	// DefaultHeatmapFile is where cmd/controlpanel keeps the heat map unless told otherwise.
	DefaultHeatmapFile = "heatmap.json.gz"
	heatFileVersion    = 1
	heatMaxShift       = 3 // 8x8-tile cells at the coarsest
	heatCoordBits      = 14
	heatCoordMax       = 1<<heatCoordBits - 1 // world x and z are well under 16384
	heatMaxLevel       = 3
	// maxHeatResponseCells bounds one /api/heatmap answer; a bigger answer is re-aggregated
	// to a coarser grid until it fits.
	maxHeatResponseCells = 60_000
	maxHeatFileBytes     = 512 << 20
)

// Variables so tests can run the loop quickly.
var (
	heatSampleInterval = time.Second
	heatSaveInterval   = 2 * time.Minute
)

// heatTierSpec is one time-bucketed tier: buckets of width seconds, keep of them (the
// newest being the one still filling).
type heatTierSpec struct {
	name  string
	width int64
	keep  int
	span  int64 // how far back the tier's window reaches
}

var heatTiers = [...]heatTierSpec{
	{name: "hour", width: 300, keep: 13, span: 3600},
	{name: "day", width: 3600, keep: 25, span: 24 * 3600},
}

// heatCounts is one sparse count map, keyed by heatKey.
type heatCounts map[uint32]uint32

type heatBucket struct {
	start    int64 // unix seconds
	accounts map[string]heatCounts
}

// heatStore is the in-memory heat map. Every method takes mu.
type heatStore struct {
	mu       sync.Mutex
	shift    uint // cells are (1<<shift) tiles square
	all      map[string]heatCounts
	fleet    heatCounts
	tiers    [len(heatTiers)][]*heatBucket // oldest first
	cells    int                           // map entries across every map above
	maxCells int
	full     bool // at the coarsest shift and at the cap: no new cells
	since    int64
	last     int64
	samples  uint64
	refused  uint64 // new-cell insertions refused while full
	gen      uint64
	dirty    bool
}

func newHeatStore(maxCells int) *heatStore {
	if maxCells <= 0 {
		maxCells = DefaultHeatmapMaxCells
	}
	return &heatStore{all: map[string]heatCounts{}, fleet: heatCounts{}, maxCells: maxCells}
}

// heatKey packs a tile into a cell key at the given shift; false for a tile outside the
// packable range (nothing in this game's world is).
func heatKey(x, z, level int, shift uint) (uint32, bool) {
	if x < 0 || z < 0 || x > heatCoordMax || z > heatCoordMax || level < 0 || level > heatMaxLevel {
		return 0, false
	}
	return uint32(level)<<(2*heatCoordBits) | uint32(x>>shift)<<heatCoordBits | uint32(z>>shift), true
}

// heatUnkey returns a key's cell coordinates (tile coordinates >> shift) and level.
func heatUnkey(k uint32) (cx, cz, level int) {
	return int(k >> heatCoordBits & heatCoordMax), int(k & heatCoordMax), int(k >> (2 * heatCoordBits))
}

func (h *heatStore) bump(m heatCounts, key uint32) {
	if c, ok := m[key]; ok {
		if c < math.MaxUint32 {
			m[key] = c + 1
		}
		return
	}
	if h.full || (h.shift >= heatMaxShift && h.cells >= h.maxCells) {
		h.refused++
		return
	}
	m[key] = 1
	h.cells++
}

func (h *heatStore) countsFor(m map[string]heatCounts, name string) heatCounts {
	c := m[name]
	if c == nil {
		c = heatCounts{}
		m[name] = c
	}
	return c
}

// record adds one sample for every position given.
func (h *heatStore) record(positions []AccountPosition, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	sec := now.Unix()
	if h.since == 0 {
		h.since = sec
	}
	h.rollLocked(sec)
	var current [len(heatTiers)]*heatBucket
	for i := range heatTiers {
		current[i] = h.tiers[i][len(h.tiers[i])-1]
	}
	for _, p := range positions {
		key, ok := heatKey(p.X, p.Z, p.Level, h.shift)
		if !ok || p.Name == "" {
			continue
		}
		h.bump(h.countsFor(h.all, p.Name), key)
		h.bump(h.fleet, key)
		for _, b := range current {
			h.bump(h.countsFor(b.accounts, p.Name), key)
		}
		h.samples++
	}
	h.gen++
	if len(positions) > 0 {
		h.last = sec
		h.dirty = true
	}
	h.enforceCapLocked()
}

// rollLocked makes sure each tier's newest bucket covers sec, dropping buckets that have
// aged out of the tier.
func (h *heatStore) rollLocked(sec int64) {
	for i, spec := range heatTiers {
		start := sec - sec%spec.width
		buckets := h.tiers[i]
		if n := len(buckets); n == 0 || buckets[n-1].start < start {
			buckets = append(buckets, &heatBucket{start: start, accounts: map[string]heatCounts{}})
		}
		oldest := start - int64(spec.keep-1)*spec.width
		drop := 0
		for drop < len(buckets) && buckets[drop].start < oldest {
			for _, c := range buckets[drop].accounts {
				h.cells -= len(c)
			}
			buckets[drop] = nil
			drop++
		}
		h.tiers[i] = append(buckets[:0:0], buckets[drop:]...)
	}
	h.full = h.shift >= heatMaxShift && h.cells >= h.maxCells
}

// enforceCapLocked coarsens the store until it is under its cap, or marks it full.
func (h *heatStore) enforceCapLocked() {
	for h.cells > h.maxCells && h.shift < heatMaxShift {
		h.coarsenLocked()
	}
	h.full = h.shift >= heatMaxShift && h.cells >= h.maxCells
}

func coarsen(m heatCounts) heatCounts {
	out := make(heatCounts, len(m)/2+1)
	for k, c := range m {
		cx, cz, level := heatUnkey(k)
		nk := uint32(level)<<(2*heatCoordBits) | uint32(cx>>1)<<heatCoordBits | uint32(cz>>1)
		sum := uint64(out[nk]) + uint64(c)
		if sum > math.MaxUint32 {
			sum = math.MaxUint32
		}
		out[nk] = uint32(sum)
	}
	return out
}

func (h *heatStore) coarsenLocked() {
	h.shift++
	cells := 0
	for name, c := range h.all {
		h.all[name] = coarsen(c)
		cells += len(h.all[name])
	}
	h.fleet = coarsen(h.fleet)
	cells += len(h.fleet)
	for i := range h.tiers {
		for _, b := range h.tiers[i] {
			for name, c := range b.accounts {
				b.accounts[name] = coarsen(c)
				cells += len(b.accounts[name])
			}
		}
	}
	h.cells = cells
}

// reset clears one account, or everything when account is empty. It reports whether
// anything was cleared.
func (h *heatStore) reset(account string, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.gen++
	h.dirty = true
	if account == "" {
		h.shift, h.all, h.fleet, h.tiers, h.cells, h.full = 0, map[string]heatCounts{}, heatCounts{}, [len(heatTiers)][]*heatBucket{}, 0, false
		h.since, h.last, h.samples, h.refused = now.Unix(), 0, 0, 0
		return true
	}
	c, ok := h.all[account]
	if !ok {
		return false
	}
	for k, n := range c {
		have, ok := h.fleet[k]
		if !ok {
			continue
		}
		if have > n {
			h.fleet[k] = have - n
		} else {
			delete(h.fleet, k)
			h.cells--
		}
	}
	h.cells -= len(c)
	delete(h.all, account)
	for i := range h.tiers {
		for _, b := range h.tiers[i] {
			h.cells -= len(b.accounts[account])
			delete(b.accounts, account)
		}
	}
	h.full = h.shift >= heatMaxShift && h.cells >= h.maxCells
	return true
}

// ---- queries ----

// heatQuery is one /api/heatmap request, already validated.
type heatQuery struct {
	account string // "" = every account
	level   int    // -1 = every level summed
	window  string // "hour", "day" or "all"
	cell    int    // requested output cell size in tiles, a power of two
	bbox    bool
	x0, z0  int // inclusive tile bounds when bbox
	x1, z1  int
}

// HeatmapBody is the /api/heatmap answer. Cells is flat: x, z, count per cell, where x/z
// is the cell's south-west tile and the cell is Cell tiles square.
type HeatmapBody struct {
	Account     string   `json:"account"`
	Level       int      `json:"level"`
	Window      string   `json:"window"`
	Cell        int      `json:"cell"`
	WindowStart int64    `json:"windowStartUnixMs"`
	Cells       []int    `json:"cells"`
	Max         uint64   `json:"max"`
	Total       uint64   `json:"total"`
	Accounts    []string `json:"accounts"`
	// Areas says where the heat for this account, floor and window lies, whatever box was
	// asked for: one entry per part of the world with any heat, busiest first. The panel fits
	// its view to one area, because the surface and the underground (6400 tiles further
	// north) are too far apart to show together: fitting to both centred the map on the
	// empty band between them at the furthest zoom, and the panel drew nothing.
	Areas []HeatArea       `json:"areas"`
	Store HeatmapStoreInfo `json:"store"`
}

// heatUndergroundZ is where the underground starts: the game puts every cave and dungeon
// 6400 tiles north of the surface above it, and no surface tile is that far north.
const heatUndergroundZ = 6400

// HeatArea is one part of the world (surface or underground) and the tile box holding all
// of its heat, inclusive.
type HeatArea struct {
	Name  string `json:"name"`
	X0    int    `json:"x0"`
	Z0    int    `json:"z0"`
	X1    int    `json:"x1"`
	Z1    int    `json:"z1"`
	Total uint64 `json:"total"`
}

// HeatmapStoreInfo describes the whole store, whatever was asked for.
type HeatmapStoreInfo struct {
	SinceUnixMs    int64  `json:"sinceUnixMs"`
	LastUnixMs     int64  `json:"lastUnixMs"`
	Samples        uint64 `json:"samples"`
	StoreCell      int    `json:"storeCell"`
	Cells          int    `json:"cells"`
	MaxCells       int    `json:"maxCells"`
	Full           bool   `json:"full"`
	Refused        uint64 `json:"refused"`
	Gen            uint64 `json:"gen"`
	SampleMs       int64  `json:"sampleMs"`
	Persisted      bool   `json:"persisted"`
	LastSaveUnixMs int64  `json:"lastSaveUnixMs,omitempty"`
	SaveError      string `json:"saveError,omitempty"`
}

func (h *heatStore) query(q heatQuery, now time.Time) HeatmapBody {
	h.mu.Lock()
	defer h.mu.Unlock()
	var sources []heatCounts
	windowStart := h.since
	switch q.window {
	case "all":
		if q.account == "" {
			sources = append(sources, h.fleet)
		} else if c := h.all[q.account]; c != nil {
			sources = append(sources, c)
		}
	default:
		tier := 0
		for i, spec := range heatTiers {
			if spec.name == q.window {
				tier = i
			}
		}
		spec := heatTiers[tier]
		cutoff := now.Unix() - spec.span
		windowStart = 0
		for _, b := range h.tiers[tier] {
			if b.start+spec.width <= cutoff {
				continue
			}
			if windowStart == 0 {
				windowStart = b.start
			}
			if q.account == "" {
				for _, c := range b.accounts {
					sources = append(sources, c)
				}
			} else if c := b.accounts[q.account]; c != nil {
				sources = append(sources, c)
			}
		}
		if windowStart == 0 || windowStart < h.since {
			windowStart = h.since
		}
	}
	storeCell := 1 << h.shift
	cell := q.cell
	if cell < storeCell {
		cell = storeCell
	}
	var agg map[uint32]uint64
	for {
		agg = aggregate(sources, h.shift, cell, q)
		if len(agg) <= maxHeatResponseCells || cell >= 1<<heatCoordBits {
			break
		}
		cell *= 2
	}
	body := HeatmapBody{Account: q.account, Level: q.level, Window: q.window, Cell: cell, WindowStart: windowStart * 1000, Cells: make([]int, 0, 3*len(agg)), Accounts: make([]string, 0, len(h.all)), Store: h.infoLocked()}
	keys := make([]uint32, 0, len(agg))
	for k := range agg {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, k := range keys {
		n := agg[k]
		body.Total += n
		if n > body.Max {
			body.Max = n
		}
		if n > math.MaxInt32 {
			n = math.MaxInt32
		}
		body.Cells = append(body.Cells, int(k>>16)*cell, int(k&0xffff)*cell, int(n))
	}
	for name := range h.all {
		body.Accounts = append(body.Accounts, name)
	}
	sort.Strings(body.Accounts)
	body.Areas = heatAreas(sources, h.shift, q.level)
	return body
}

// heatAreas finds the tile box and total of the heat on the surface and in the underground,
// ignoring any query box, busiest area first.
func heatAreas(sources []heatCounts, shift uint, level int) []HeatArea {
	areas := [2]HeatArea{{Name: "surface"}, {Name: "underground"}}
	size := 1 << shift
	for _, m := range sources {
		for k, c := range m {
			cx, cz, l := heatUnkey(k)
			if level >= 0 && l != level {
				continue
			}
			x, z := cx<<shift, cz<<shift
			a := &areas[0]
			if z >= heatUndergroundZ {
				a = &areas[1]
			}
			if a.Total == 0 {
				a.X0, a.Z0, a.X1, a.Z1 = x, z, x+size-1, z+size-1
			} else {
				a.X0, a.Z0 = min(a.X0, x), min(a.Z0, z)
				a.X1, a.Z1 = max(a.X1, x+size-1), max(a.Z1, z+size-1)
			}
			a.Total += uint64(c)
		}
	}
	out := make([]HeatArea, 0, len(areas))
	for _, a := range areas {
		if a.Total > 0 {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Total > out[j].Total })
	return out
}

// aggregate sums sources onto a grid of cell tiles, keyed gx<<16|gz.
func aggregate(sources []heatCounts, shift uint, cell int, q heatQuery) map[uint32]uint64 {
	out := map[uint32]uint64{}
	for _, m := range sources {
		for k, c := range m {
			cx, cz, level := heatUnkey(k)
			if q.level >= 0 && level != q.level {
				continue
			}
			x, z := cx<<shift, cz<<shift
			if q.bbox && (x+(1<<shift)-1 < q.x0 || x > q.x1 || z+(1<<shift)-1 < q.z0 || z > q.z1) {
				continue
			}
			out[uint32(x/cell)<<16|uint32(z/cell)] += uint64(c)
		}
	}
	return out
}

func (h *heatStore) infoLocked() HeatmapStoreInfo {
	return HeatmapStoreInfo{SinceUnixMs: h.since * 1000, LastUnixMs: h.last * 1000, Samples: h.samples, StoreCell: 1 << h.shift, Cells: h.cells, MaxCells: h.maxCells, Full: h.full, Refused: h.refused, Gen: h.gen, SampleMs: heatSampleInterval.Milliseconds()}
}

// ---- persistence ----

// heatFile is the on-disk form (gzip-compressed JSON). Every count list is flat
// key, count pairs sorted by key, where key packs level<<28 | (x>>shift)<<14 | (z>>shift).
type heatFile struct {
	Version  int                 `json:"version"`
	Shift    uint                `json:"shift"`
	Since    int64               `json:"since"`
	Last     int64               `json:"last"`
	Samples  uint64              `json:"samples"`
	Refused  uint64              `json:"refused"`
	Fleet    []uint32            `json:"fleet"`
	Accounts map[string][]uint32 `json:"accounts"`
	Tiers    []heatFileTier      `json:"tiers"`
}

type heatFileTier struct {
	Width   int64            `json:"width"`
	Buckets []heatFileBucket `json:"buckets"`
}

type heatFileBucket struct {
	Start    int64               `json:"start"`
	Accounts map[string][]uint32 `json:"accounts"`
}

func flatten(m heatCounts) []uint32 {
	out := make([]uint32, 2*len(m))
	// Sort keys in the first half of the output, then expand backwards so no
	// unread key is overwritten. Saving needs no separate key scratch slice.
	keys := out[:len(m)]
	i := 0
	for k := range m {
		keys[i] = k
		i++
	}
	slices.Sort(keys)
	for i := len(keys) - 1; i >= 0; i-- {
		k := keys[i]
		out[2*i], out[2*i+1] = k, m[k]
	}
	return out
}

func unflatten(flat []uint32) (heatCounts, error) {
	if len(flat)%2 != 0 {
		return nil, errors.New("odd-length count list")
	}
	m := make(heatCounts, len(flat)/2)
	for i := 0; i < len(flat); i += 2 {
		if _, _, level := heatUnkey(flat[i]); level > heatMaxLevel {
			return nil, fmt.Errorf("key %d has level %d", flat[i], level)
		}
		if flat[i+1] > 0 {
			m[flat[i]] += flat[i+1]
		}
	}
	return m, nil
}

func flattenAll(m map[string]heatCounts) map[string][]uint32 {
	out := make(map[string][]uint32, len(m))
	for name, c := range m {
		if len(c) > 0 {
			out[name] = flatten(c)
		}
	}
	return out
}

// snapshotFile copies the store into its file form under the lock; encoding happens after.
func (h *heatStore) snapshotFile() (heatFile, uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f := heatFile{Version: heatFileVersion, Shift: h.shift, Since: h.since, Last: h.last, Samples: h.samples, Refused: h.refused, Fleet: flatten(h.fleet), Accounts: flattenAll(h.all)}
	for i, spec := range heatTiers {
		t := heatFileTier{Width: spec.width}
		for _, b := range h.tiers[i] {
			t.Buckets = append(t.Buckets, heatFileBucket{Start: b.start, Accounts: flattenAll(b.accounts)})
		}
		f.Tiers = append(f.Tiers, t)
	}
	h.dirty = false
	return f, h.gen
}

func encodeHeatFile(f heatFile) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	if err := json.NewEncoder(zw).Encode(f); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeHeatFile(r io.Reader) (heatFile, error) {
	var f heatFile
	zr, err := gzip.NewReader(r)
	if err != nil {
		return f, err
	}
	defer zr.Close()
	if err := json.NewDecoder(io.LimitReader(zr, maxHeatFileBytes)).Decode(&f); err != nil {
		return f, err
	}
	if f.Version != heatFileVersion {
		return f, fmt.Errorf("version %d, want %d", f.Version, heatFileVersion)
	}
	if f.Shift > heatMaxShift {
		return f, fmt.Errorf("shift %d is over %d", f.Shift, heatMaxShift)
	}
	return f, nil
}

// restore replaces the store's contents with a decoded file.
func (h *heatStore) restore(f heatFile) error {
	fleet, err := unflatten(f.Fleet)
	if err != nil {
		return err
	}
	all := make(map[string]heatCounts, len(f.Accounts))
	cells := len(fleet)
	for name, flat := range f.Accounts {
		if all[name], err = unflatten(flat); err != nil {
			return err
		}
		cells += len(all[name])
	}
	var tiers [len(heatTiers)][]*heatBucket
	for _, t := range f.Tiers {
		for i, spec := range heatTiers {
			if t.Width != spec.width {
				continue // a tier this build no longer keeps is dropped
			}
			for _, fb := range t.Buckets {
				b := &heatBucket{start: fb.Start, accounts: make(map[string]heatCounts, len(fb.Accounts))}
				for name, flat := range fb.Accounts {
					if b.accounts[name], err = unflatten(flat); err != nil {
						return err
					}
					cells += len(b.accounts[name])
				}
				tiers[i] = append(tiers[i], b)
			}
			sort.Slice(tiers[i], func(a, b int) bool { return tiers[i][a].start < tiers[i][b].start })
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.shift, h.since, h.last, h.samples, h.refused = f.Shift, f.Since, f.Last, f.Samples, f.Refused
	h.all, h.fleet, h.tiers, h.cells = all, fleet, tiers, cells
	h.gen++
	h.enforceCapLocked()
	return nil
}

// ---- the recorder: sampling loop plus the file ----

type heatRecorder struct {
	store *heatStore
	src   PositionSource
	path  string
	now   func() time.Time

	saveMu   sync.Mutex // one writer of the file at a time
	stateMu  sync.Mutex
	lastSave int64
	saveErr  string

	stop chan struct{}
	wg   sync.WaitGroup
	once sync.Once
}

// newHeatRecorder returns nil when the source cannot report positions or the heat map is
// switched off.
func newHeatRecorder(cfg Config, src Source, now func() time.Time) *heatRecorder {
	ps, ok := src.(PositionSource)
	if !ok || cfg.HeatmapOff {
		return nil
	}
	r := &heatRecorder{store: newHeatStore(cfg.HeatmapMaxCells), src: ps, path: cfg.HeatmapFile, now: now}
	r.load()
	return r
}

func (r *heatRecorder) load() {
	if r.path == "" {
		return
	}
	f, err := os.Open(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err == nil {
		var hf heatFile
		hf, err = decodeHeatFile(f)
		f.Close()
		if err == nil {
			err = r.store.restore(hf)
		}
		if err == nil {
			info := r.store.info()
			log.Printf("controlpanel: heat map loaded from %s: %d cells, %d samples since %s", r.path, info.Cells, info.Samples, time.UnixMilli(info.SinceUnixMs).Format(time.RFC3339))
			return
		}
	}
	aside := fmt.Sprintf("%s.unreadable-%d", r.path, r.now().Unix())
	if renameErr := os.Rename(r.path, aside); renameErr != nil {
		log.Printf("controlpanel: heat map %s is unreadable (%v) and could not be moved aside (%v); recording in memory only", r.path, err, renameErr)
		r.path = ""
		return
	}
	log.Printf("controlpanel: heat map %s is unreadable (%v); moved it to %s and started empty", r.path, err, aside)
}

func (h *heatStore) info() HeatmapStoreInfo { h.mu.Lock(); defer h.mu.Unlock(); return h.infoLocked() }

func (r *heatRecorder) start() {
	if r == nil {
		return
	}
	r.stop = make(chan struct{})
	r.wg.Add(1)
	go r.run()
}

func (r *heatRecorder) close() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		if r.stop != nil {
			close(r.stop)
			r.wg.Wait()
		}
		if err := r.save(); err != nil {
			log.Printf("controlpanel: heat map: final save failed: %v", err)
		}
	})
}

func (r *heatRecorder) run() {
	defer r.wg.Done()
	sample := newTicker(heatSampleInterval)
	defer sample.Stop()
	save := newTicker(heatSaveInterval)
	defer save.Stop()
	var buf []AccountPosition
	for {
		select {
		case <-r.stop:
			return
		case <-sample.C:
			buf = r.src.Positions(buf[:0])
			r.store.record(buf, r.now())
			if cap(buf) > 2*MaxAccounts {
				buf = nil
			}
		case <-save.C:
			if err := r.save(); err != nil {
				log.Printf("controlpanel: heat map: save failed: %v", err)
			}
		}
	}
}

// save writes the store if it changed since the last save. Atomic: a temp file beside the
// target, fsynced, then renamed over it, so a crash mid-write leaves the previous file.
func (r *heatRecorder) save() error {
	if r.path == "" {
		return nil
	}
	r.saveMu.Lock()
	defer r.saveMu.Unlock()
	r.store.mu.Lock()
	dirty := r.store.dirty
	r.store.mu.Unlock()
	if !dirty {
		return nil
	}
	f, _ := r.store.snapshotFile()
	data, err := encodeHeatFile(f)
	if err == nil {
		err = writeFileAtomic(r.path, data)
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if err != nil {
		r.store.mu.Lock()
		r.store.dirty = true
		r.store.mu.Unlock()
		r.saveErr = err.Error()
		return err
	}
	r.saveErr = ""
	r.lastSave = r.now().UnixMilli()
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}

// reset clears the store (one account, or all) and immediately rewrites the file so the
// cleared state is what a restart loads.
func (r *heatRecorder) reset(account string) (bool, error) {
	cleared := r.store.reset(account, r.now())
	if !cleared {
		return false, nil
	}
	return true, r.save()
}

func (r *heatRecorder) query(q heatQuery) HeatmapBody {
	body := r.store.query(q, r.now())
	body.Store.Persisted = r.path != ""
	r.stateMu.Lock()
	body.Store.LastSaveUnixMs, body.Store.SaveError = r.lastSave, r.saveErr
	r.stateMu.Unlock()
	return body
}
