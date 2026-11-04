// Package cachecfg resolves obj, npc and loc type ids to their names and options — what a
// script needs to find "Bank booth" or "Man" instead of a raw id from gamestate.
//
// A Config is decoded once per process from the config archive (config.jag, served by the
// engine as GET /config<crc>, crc = /crc0 index 2) and is read-only afterwards, so every
// account shares one copy. It keeps only the fields scripts use: names, options, and a
// few flags (members, stackable, noted, loc size and blocking). Storage is compact: all
// names and options live in one string, repeated option sets are stored once, and every
// accessor returns strings that slice that arena, so lookups do not allocate.
//
// The container is opened with internal/archive.ParseJagFile; this package only adds the
// record decoders (ports of ObjType/NpcType/LocType.decode) and the index.
package cachecfg

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"rsb0t-go/internal/archive"
)

// Obj is one item type.
type Obj struct {
	ID        int
	Name      string
	Members   bool
	Stackable bool
	Cost      int32
	// CertLink is the unnoted item for a note (-1 otherwise); CertTemplate is the note
	// template id (-1 for an ordinary item).
	CertLink     int
	CertTemplate int
	// Ops are the ground options (op1-op5), IOps the inventory options; an absent or
	// "hidden" option is "".
	Ops  [5]string
	IOps [5]string
}

// Noted reports whether the item is a bank note.
func (o Obj) Noted() bool { return o.CertTemplate != -1 }

// Npc is one NPC type.
type Npc struct {
	ID   int
	Name string
	Size int
	// Level is the combat level shown on the menu (vislevel), -1 when none.
	Level int
	Ops   [5]string
}

// Loc is one loc (scenery object) type.
type Loc struct {
	ID            int
	Name          string
	Width, Length int
	BlockWalk     bool
	BlockRange    bool
	// Active is whether the loc is interactive, resolved as LocType.decode does when the
	// archive leaves it implicit.
	Active        bool
	ForceApproach int
	Ops           [5]string
}

// OpIndex returns the 1-based option number whose label matches (ignoring case), the n in
// OPOBJn/OPNPCn/OPLOCn/OPHELDn, or 0 when no option has that label.
func OpIndex(ops [5]string, label string) int {
	for i, op := range ops {
		if op != "" && strings.EqualFold(op, label) {
			return i + 1
		}
	}
	return 0
}

// span is a substring of Config.text.
type span struct {
	off uint32
	n   uint32
}

const none = 0xffff

type objRec struct {
	name               span
	ops, iops          uint16
	cost               int32
	certlink, certtmpl uint16
	flags              uint8
}

type npcRec struct {
	name  span
	ops   uint16
	size  int16
	level int16
}

type locRec struct {
	name          span
	ops           uint16
	width, length uint8
	forceapproach uint8
	flags         uint8
}

const (
	flagMembers = 1 << iota
	flagStackable
	flagBlockWalk
	flagBlockRange
	flagActive
)

// Config is a decoded config archive. It is safe for concurrent use.
type Config struct {
	text   string
	opSets [][5]span

	objs []objRec
	npcs []npcRec
	locs []locRec

	// byName holds each table's ids sorted by lower-cased name, then id.
	objByName, npcByName, locByName []int32
}

// LoadFile decodes a config archive from disk.
func LoadFile(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cachecfg: %w", err)
	}
	return Parse(raw)
}

// Parse decodes a config archive (the whole JagFile container).
func Parse(archiveBytes []byte) (*Config, error) {
	jag, err := archive.ParseJagFile(archiveBytes)
	if err != nil {
		return nil, fmt.Errorf("cachecfg: %w", err)
	}
	files := map[string][]byte{}
	for _, name := range []string{"obj.dat", "obj.idx", "npc.dat", "npc.idx", "loc.dat", "loc.idx"} {
		data, err := jag.Read(name)
		if err != nil {
			return nil, fmt.Errorf("cachecfg: %s: %w", name, err)
		}
		if data == nil {
			return nil, fmt.Errorf("cachecfg: archive has no %s", name)
		}
		files[name] = data
	}
	return build(files)
}

// build decodes the six .dat/.idx files. It is separate from Parse so tests can feed
// hand-made records without a bzip2 container.
func build(files map[string][]byte) (*Config, error) {
	b := newBuilder()
	c := &Config{}

	var objs []rawObj
	if _, err := records("obj", files["obj.idx"], files["obj.dat"], func(r *reader) { objs = append(objs, decodeObj(r)) }); err != nil {
		return nil, err
	}
	var npcs []rawNpc
	if _, err := records("npc", files["npc.idx"], files["npc.dat"], func(r *reader) { npcs = append(npcs, decodeNpc(r)) }); err != nil {
		return nil, err
	}
	var locs []rawLoc
	if _, err := records("loc", files["loc.idx"], files["loc.dat"], func(r *reader) { locs = append(locs, decodeLoc(r)) }); err != nil {
		return nil, err
	}

	c.objs = make([]objRec, len(objs))
	for id := range objs {
		o := resolveCert(objs, id)
		rec := objRec{name: b.text(o.name), ops: b.ops(o.ops), iops: b.ops(o.iops), cost: o.cost, certlink: id16(o.certlink), certtmpl: id16(o.certtemplate)}
		if o.members {
			rec.flags |= flagMembers
		}
		if o.stackable {
			rec.flags |= flagStackable
		}
		c.objs[id] = rec
	}
	c.npcs = make([]npcRec, len(npcs))
	for id, n := range npcs {
		c.npcs[id] = npcRec{name: b.text(n.name), ops: b.ops(n.ops), size: int16(n.size), level: int16(n.vislevel)}
	}
	c.locs = make([]locRec, len(locs))
	for id, l := range locs {
		rec := locRec{name: b.text(l.name), ops: b.ops(l.ops), width: uint8(l.width), length: uint8(l.length), forceapproach: uint8(l.forceapproach)}
		if l.blockwalk {
			rec.flags |= flagBlockWalk
		}
		if l.blockrange {
			rec.flags |= flagBlockRange
		}
		if l.active {
			rec.flags |= flagActive
		}
		c.locs[id] = rec
	}

	c.text = b.buf.String()
	c.opSets = b.opSets
	c.objByName = c.sortByName(len(c.objs), func(id int) span { return c.objs[id].name })
	c.npcByName = c.sortByName(len(c.npcs), func(id int) span { return c.npcs[id].name })
	c.locByName = c.sortByName(len(c.locs), func(id int) span { return c.locs[id].name })
	return c, nil
}

// resolveCert ports ObjType.genCert: a note takes its name, members flag and cost from
// the item it links to and is always stackable. The TS resolves the link through
// ObjType.list, which applies genCert to the link too, hence the recursion.
func resolveCert(objs []rawObj, id int) rawObj {
	return resolveCertDepth(objs, id, 0)
}

func resolveCertDepth(objs []rawObj, id, depth int) rawObj {
	o := objs[id]
	if o.certtemplate == -1 || o.certlink < 0 || o.certlink >= len(objs) || depth > 4 {
		return o
	}
	link := resolveCertDepth(objs, o.certlink, depth+1)
	o.name = link.name
	o.members = link.members
	o.cost = link.cost
	o.stackable = true
	return o
}

func id16(v int) uint16 {
	if v < 0 || v >= none {
		return none
	}
	return uint16(v)
}

func id32(v uint16) int {
	if v == none {
		return -1
	}
	return int(v)
}

type builder struct {
	buf    strings.Builder
	spans  map[string]span
	opSets [][5]span
	opKeys map[[5]span]uint16
}

func newBuilder() *builder {
	b := &builder{spans: map[string]span{}, opKeys: map[[5]span]uint16{}}
	b.opSets = append(b.opSets, [5]span{})
	b.opKeys[[5]span{}] = 0
	return b
}

func (b *builder) text(s string) span {
	if s == "" {
		return span{}
	}
	if sp, ok := b.spans[s]; ok {
		return sp
	}
	sp := span{off: uint32(b.buf.Len()), n: uint32(len(s))}
	b.buf.WriteString(s)
	b.spans[s] = sp
	return sp
}

func (b *builder) ops(ops [5]string) uint16 {
	var key [5]span
	for i, op := range ops {
		key[i] = b.text(op)
	}
	if idx, ok := b.opKeys[key]; ok {
		return idx
	}
	idx := uint16(len(b.opSets))
	b.opSets = append(b.opSets, key)
	b.opKeys[key] = idx
	return idx
}

func (c *Config) str(s span) string { return c.text[s.off : s.off+s.n] }

func (c *Config) opStrings(set uint16) [5]string {
	var out [5]string
	for i, s := range c.opSets[set] {
		out[i] = c.str(s)
	}
	return out
}

func (c *Config) sortByName(count int, name func(int) span) []int32 {
	ids := make([]int32, 0, count)
	for id := 0; id < count; id++ {
		if name(id).n > 0 {
			ids = append(ids, int32(id))
		}
	}
	sort.SliceStable(ids, func(i, j int) bool {
		return foldCompare(c.str(name(int(ids[i]))), c.str(name(int(ids[j])))) < 0
	})
	return ids
}

// foldCompare orders strings as if both were lower-cased, byte by byte (names are ASCII
// or Latin-1, and only ASCII letters fold, matching toLowerCase on the names in use).
func foldCompare(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		ca, cb := lower(a[i]), lower(b[i])
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

func (c *Config) lookup(sorted []int32, name string, nameOf func(int) span) []int {
	i := sort.Search(len(sorted), func(i int) bool {
		return foldCompare(c.str(nameOf(int(sorted[i]))), name) >= 0
	})
	var out []int
	for ; i < len(sorted) && foldCompare(c.str(nameOf(int(sorted[i]))), name) == 0; i++ {
		out = append(out, int(sorted[i]))
	}
	return out
}

// Counts returns how many obj, npc and loc types the archive defines.
func (c *Config) Counts() (objs, npcs, locs int) { return len(c.objs), len(c.npcs), len(c.locs) }

// Obj returns an item type by id.
func (c *Config) Obj(id int) (Obj, bool) {
	if id < 0 || id >= len(c.objs) {
		return Obj{}, false
	}
	r := c.objs[id]
	return Obj{
		ID: id, Name: c.str(r.name), Members: r.flags&flagMembers != 0, Stackable: r.flags&flagStackable != 0,
		Cost: r.cost, CertLink: id32(r.certlink), CertTemplate: id32(r.certtmpl),
		Ops: c.opStrings(r.ops), IOps: c.opStrings(r.iops),
	}, true
}

// Npc returns an NPC type by id.
func (c *Config) Npc(id int) (Npc, bool) {
	if id < 0 || id >= len(c.npcs) {
		return Npc{}, false
	}
	r := c.npcs[id]
	return Npc{ID: id, Name: c.str(r.name), Size: int(r.size), Level: int(r.level), Ops: c.opStrings(r.ops)}, true
}

// Loc returns a loc type by id.
func (c *Config) Loc(id int) (Loc, bool) {
	if id < 0 || id >= len(c.locs) {
		return Loc{}, false
	}
	r := c.locs[id]
	return Loc{
		ID: id, Name: c.str(r.name), Width: int(r.width), Length: int(r.length),
		BlockWalk: r.flags&flagBlockWalk != 0, BlockRange: r.flags&flagBlockRange != 0, Active: r.flags&flagActive != 0,
		ForceApproach: int(r.forceapproach), Ops: c.opStrings(r.ops),
	}, true
}

// ObjIDs returns every item type id with this name, ignoring case, in id order.
//
// A QUALIFIED name (QualifiedName: "Herb id199") names one obj and its bank note only: the obj
// with that id, provided its display name is the qualified name's base, and every note whose
// CertLink is that id. It exists for display names several objs share (every grimy herb in
// content 289 is named "Herb": skill_herblore/configs/identifying/unids.obj), so the mule's
// published bank, a kit order and its hand-out can address one of them by name without any of
// them learning about ids.
func (c *Config) ObjIDs(name string) []int {
	if base, id, ok := SplitQualified(name); ok {
		return c.qualifiedObjIDs(base, id)
	}
	return c.lookup(c.objByName, name, func(id int) span { return c.objs[id].name })
}

// qualifierWord is the word before the id in a qualified name. Letters, digits and spaces only,
// so a qualified name passes every item-name check a plain one does (orderplan's itemNameOK).
const qualifierWord = " id"

// QualifiedName spells obj id by its display name and its id ("Herb id199"), which ObjIDs
// resolves to that obj alone (and its note).
func QualifiedName(name string, id int) string {
	return strings.TrimSpace(name) + qualifierWord + fmt.Sprint(id)
}

// SplitQualified reads a QualifiedName back: the base name and the id, ok false for a plain name.
func SplitQualified(name string) (base string, id int, ok bool) {
	name = strings.TrimSpace(name)
	i := strings.LastIndex(strings.ToLower(name), qualifierWord)
	if i <= 0 || i+len(qualifierWord) >= len(name) {
		return "", 0, false
	}
	digits := name[i+len(qualifierWord):]
	n := 0
	for _, r := range digits {
		if r < '0' || r > '9' || n > 1<<20 {
			return "", 0, false
		}
		n = n*10 + int(r-'0')
	}
	return strings.TrimSpace(name[:i]), n, true
}

func (c *Config) qualifiedObjIDs(base string, id int) []int {
	o, ok := c.Obj(id)
	if !ok || !strings.EqualFold(strings.TrimSpace(o.Name), base) {
		return nil
	}
	out := []int{id}
	for _, n := range c.lookup(c.objByName, base, func(id int) span { return c.objs[id].name }) {
		if n == id {
			continue
		}
		if note, ok := c.Obj(n); ok && note.Noted() && note.CertLink == id {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

// IsNamed reports whether obj id answers to name. A plain name is every obj carrying that display
// name, ignoring case, a bank note included (the set ObjIDs returns). A QUALIFIED name ("Yew
// shortbow id68") is that one obj and the notes whose CertLink is it, and nothing else with the same
// display name: the answer a pack count or a trade screen needs when several objs share a name and
// only one of them was asked for (content 289 names a strung bow and its unstrung form alike).
func (c *Config) IsNamed(name string, id int) bool {
	o, ok := c.Obj(id)
	if !ok {
		return false
	}
	if base, qid, qualified := SplitQualified(name); qualified {
		if !strings.EqualFold(strings.TrimSpace(o.Name), base) {
			return false
		}
		return id == qid || (o.Noted() && o.CertLink == qid)
	}
	return strings.EqualFold(strings.TrimSpace(o.Name), strings.TrimSpace(name))
}

// Dearest is the un-noted obj of this name with the highest cost (the one a High Alchemy cast pays
// most for: a strung bow, not its unstrung form), and whether more than one un-noted obj carries the
// name, so that a plain name would not say which is meant. ok is false when no un-noted obj has it.
// For a qualified name it is that obj alone.
func (c *Config) Dearest(name string) (id int, shared, ok bool) {
	best, n := -1, 0
	var bestCost int32
	for _, cand := range c.ObjIDs(name) {
		o, found := c.Obj(cand)
		if !found || o.Noted() {
			continue
		}
		n++
		if best < 0 || o.Cost > bestCost {
			best, bestCost = cand, o.Cost
		}
	}
	return best, n > 1, best >= 0
}

// coinsName is the display name both coin objs carry; currencyObjID is the real currency.
const (
	coinsName     = "Coins"
	currencyObjID = 995
)

// CurrencyID returns the coin object id: the currency, not the members obj that shares
// its name.
//
// Why this exists rather than ObjIDs("Coins")[0]: this build has TWO objs named "Coins" —
// 617, a members obj, and 995, the currency every ::give, every trade and every bank path
// actually moves. ObjIDs returns ids in id order, so the first-id form hands back 617, and
// code that counts a pack or a bank by that id reads zero however much money moved. Found
// live 2026-09-20 by cmd/mulequeuetest, whose own precondition caught it; cmd/handofftest
// had been resolving coins that way and reporting a working reserve split on an item
// nobody held.
//
// Prefers 995 by id, then falls back to the one "Coins" obj that is stackable, not a
// members item and not a note, so a cache that ever renumbers the currency still resolves
// instead of trusting a hardcoded number that has quietly stopped being true.
func (c *Config) CurrencyID() (int, bool) {
	// The preferred ID can be checked without materializing every name match.
	// Keep lookup's ASCII case folding and its fallback for renumbered caches.
	if currencyObjID < len(c.objs) && foldCompare(c.str(c.objs[currencyObjID].name), coinsName) == 0 {
		return currencyObjID, true
	}
	ids := c.ObjIDs(coinsName)
	for _, id := range ids {
		if obj, ok := c.Obj(id); ok && obj.Stackable && !obj.Members && !obj.Noted() {
			return id, true
		}
	}
	return 0, false
}

// NpcIDs returns every NPC type id with this name, ignoring case, in id order.
func (c *Config) NpcIDs(name string) []int {
	return c.lookup(c.npcByName, name, func(id int) span { return c.npcs[id].name })
}

// LocIDs returns every loc type id with this name, ignoring case, in id order.
func (c *Config) LocIDs(name string) []int {
	return c.lookup(c.locByName, name, func(id int) span { return c.locs[id].name })
}
