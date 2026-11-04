package entityquery

import (
	"bytes"
	"testing"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

type fakeTypes struct{}

func (fakeTypes) Npc(id int) (cachecfg.Npc, bool) {
	switch id {
	case 41:
		return cachecfg.Npc{ID: 41, Name: "Chicken", Level: 1, Ops: [5]string{"", "Attack"}}, true
	case 520:
		return cachecfg.Npc{ID: 520, Name: "Shop keeper", Level: -1, Ops: [5]string{"Talk-to", "", "Trade"}}, true
	}
	return cachecfg.Npc{}, false
}

func (fakeTypes) Obj(id int) (cachecfg.Obj, bool) {
	switch id {
	case 526:
		return cachecfg.Obj{ID: 526, Name: "Bones"}, true
	case 995:
		return cachecfg.Obj{ID: 995, Name: "Coins"}, true
	}
	return cachecfg.Obj{}, false
}

func (fakeTypes) Loc(id int) (cachecfg.Loc, bool) {
	switch id {
	case 1276:
		return cachecfg.Loc{ID: 1276, Name: "Tree", Width: 1, Length: 1, Ops: [5]string{"Chop down"}}, true
	case 2213:
		return cachecfg.Loc{ID: 2213, Name: "Bank booth", Width: 1, Length: 1, Ops: [5]string{"", "Use-quickly"}}, true
	}
	return cachecfg.Loc{}, false
}

// bitWriter packs fields most-significant bit first.
type bitWriter struct {
	buf []byte
	pos int
}

func (b *bitWriter) put(n int, v uint32) {
	for i := n - 1; i >= 0; i-- {
		for len(b.buf) <= b.pos/8 {
			b.buf = append(b.buf, 0)
		}
		if (v>>uint(i))&1 == 1 {
			b.buf[b.pos/8] |= 0x80 >> uint(b.pos%8)
		}
		b.pos++
	}
}

func handle(st *gamestate.State, opcode int, build func(p *packet.Packet)) {
	p := packet.NewWriter(64)
	build(p)
	st.Handle(opcode, p.Data[:p.Pos])
}

// scene puts the player at world tile (3212, 3212) level 0: origin 3152,3152, local 60,60.
func scene() *gamestate.State {
	st := gamestate.New()
	handle(st, serverprot.REBUILD_NORMAL, func(p *packet.Packet) { p.P2(400); p.P2(400) })
	var b bitWriter
	b.put(1, 1)
	b.put(2, 3)
	b.put(2, 0)
	b.put(7, 60)
	b.put(7, 60)
	b.put(1, 0)
	b.put(1, 0)
	b.put(8, 0)
	b.put(11, 2047)
	st.Handle(serverprot.PLAYER_INFO, b.buf)
	return st
}

// addNpcs sends one NPC_INFO adding each [index, type, dx, dz] relative to the player.
func addNpcs(st *gamestate.State, npcs ...[4]int) {
	var b bitWriter
	b.put(8, 0)
	for _, n := range npcs {
		b.put(14, uint32(n[0]))
		b.put(11, uint32(n[1]))
		b.put(5, uint32(n[2])&0x1f)
		b.put(5, uint32(n[3])&0x1f)
		b.put(1, 0)
		b.put(1, 0)
	}
	b.put(14, 16383)
	st.Handle(serverprot.NPC_INFO, b.buf)
}

// objAdd drops a stack at world (x, z) inside the zone at local (56, 56) = world 3208,3208.
func objAdd(st *gamestate.State, x, z, id, count int) {
	handle(st, serverprot.UPDATE_ZONE_PARTIAL_FOLLOWS, func(p *packet.Packet) { p.P1(56); p.P1(56) })
	handle(st, serverprot.OBJ_ADD, func(p *packet.Packet) {
		p.P1(byte(((x - 3208) << 4) | (z - 3208)))
		p.P2(uint16(id))
		p.P2(uint16(count))
	})
}

func TestQueryFiltersAndTerminals(t *testing.T) {
	entities := []Entity{
		{Kind: KindNpc, Index: 1, Name: "Chicken", Ops: [5]string{"", "Attack"}, X: 10, Z: 10, Distance: 4},
		{Kind: KindNpc, Index: 2, Name: " chicken ", Ops: [5]string{"", "Attack"}, X: 20, Z: 20, Distance: 2},
		{Kind: KindNpc, Index: 3, Name: "Cow", Ops: [5]string{"", "Attack"}, X: 30, Z: 30, Distance: 1},
		{Kind: KindNpc, Index: 4, Name: "", X: 0, Z: 0, Distance: 0},
	}
	q := FromEntities(entities).Name("CHICKEN")
	if q.Count() != 2 || !q.Exists() {
		t.Fatalf("Name: count %d", q.Count())
	}
	if e, _ := q.First(); e.Index != 1 {
		t.Fatalf("First = %d, want 1 (snapshot order)", e.Index)
	}
	if e, _ := q.Nearest(); e.Index != 2 {
		t.Fatalf("Nearest = %d, want 2", e.Index)
	}
	if n := FromEntities(entities).Action("attack").Within(2).Count(); n != 2 {
		t.Fatalf("Action+Within count = %d, want 2", n)
	}
	if n := FromEntities(entities).Inside(Area{MinX: 10, MaxX: 20, MinZ: 10, MaxZ: 20}).Count(); n != 2 {
		t.Fatalf("Inside count = %d, want 2 (bounds inclusive)", n)
	}
	if n := FromEntities(entities).WithinOf(21, 19, 1).Count(); n != 1 {
		t.Fatalf("WithinOf count = %d, want 1", n)
	}
	if n := FromEntities(entities).Where(func(e Entity) bool { return e.Index%2 == 0 }).Count(); n != 2 {
		t.Fatalf("Where count = %d", n)
	}
	if _, ok := FromEntities(entities).Name("Goblin").Nearest(); ok {
		t.Fatal("Nearest on no results")
	}
	// NearestPreferLocal: nothing chicken-named within 1 tile, so all chickens are considered.
	if e, _ := FromEntities(entities).Name("chicken").NearestPreferLocal(1); e.Index != 2 {
		t.Fatalf("NearestPreferLocal fallback = %d", e.Index)
	}
	if e, _ := FromEntities(entities).NearestPreferLocal(3); e.Index != 4 {
		t.Fatalf("NearestPreferLocal = %d", e.Index)
	}
	if got := entities[0].Actions(); len(got) != 1 || got[0] != "Attack" {
		t.Fatalf("Actions = %v", got)
	}
}

func TestNpcsFromGamestate(t *testing.T) {
	st := scene()
	addNpcs(st, [4]int{7, 41, 3, 0}, [4]int{8, 41, -1, -1}, [4]int{9, 520, 5, 5})
	all := NpcEntities(st, fakeTypes{})
	if len(all) != 3 || all[0].Anim != -1 || all[0].FaceEntity != -1 || all[0].Index != 8 || all[0].Distance != 1 || all[0].X != 3211 {
		t.Fatalf("NpcEntities = %+v", all)
	}
	chicken, ok := Npcs(st, fakeTypes{}).Name("chicken").Action("Attack").Nearest()
	if !ok || chicken.Index != 8 || chicken.CombatLevel != 1 {
		t.Fatalf("nearest chicken = %+v, %v", chicken, ok)
	}
	if got := NearestNpcs(st, fakeTypes{}, 2); len(got) != 2 || got[1].Index != 7 {
		t.Fatalf("NearestNpcs = %+v", got)
	}
	if Npcs(gamestate.New(), fakeTypes{}).Exists() {
		t.Fatal("a query without a known player tile must be empty")
	}
}

func TestGroundItemsDefaultTake(t *testing.T) {
	st := scene()
	objAdd(st, 3210, 3212, 526, 1)
	objAdd(st, 3215, 3212, 995, 25)
	bones, ok := GroundItems(st, fakeTypes{}).Name("Bones").Action("take").First()
	if !ok || bones.Distance != 2 || bones.Option("Take") != 3 {
		t.Fatalf("bones = %+v, %v", bones, ok)
	}
	coins, _ := GroundItems(st, nil).Where(func(e Entity) bool { return e.ID == 995 }).First()
	if coins.Count != 25 || coins.Option("Take") != 3 || coins.Name != "" {
		t.Fatalf("coins without types = %+v", coins)
	}
}

func TestLocsMergeMapFileAndLiveChanges(t *testing.T) {
	st := scene()
	ix, err := locindex.New([]locindex.Loc{
		{ID: 1276, X: 3214, Z: 3212, Level: 0, Shape: 10},
		{ID: 1276, X: 3209, Z: 3212, Level: 0, Shape: 10},
		{ID: 1276, X: 3212, Z: 3300, Level: 0, Shape: 10}, // outside the radius
	})
	if err != nil {
		t.Fatal(err)
	}
	// A live LOC_DEL removes the tree at 3209,3212; a live booth appears at 3213,3213.
	handle(st, serverprot.UPDATE_ZONE_PARTIAL_FOLLOWS, func(p *packet.Packet) { p.P1(56); p.P1(56) })
	handle(st, serverprot.LOC_DEL, func(p *packet.Packet) { p.P1(byte((1 << 4) | 4)); p.P1(10 << 2) })
	handle(st, serverprot.LOC_ADD_CHANGE, func(p *packet.Packet) { p.P1(byte((5 << 4) | 5)); p.P1(10 << 2); p.P2(2213) })

	locs := LocEntities(st, fakeTypes{}, ix, 20)
	if len(locs) != 2 {
		t.Fatalf("LocEntities = %+v, want the booth and one tree", locs)
	}
	if locs[0].Name != "Bank booth" || !locs[0].Live || locs[1].Name != "Tree" || locs[1].X != 3214 {
		t.Fatalf("LocEntities order/contents = %+v", locs)
	}
	if n := Locs(st, fakeTypes{}, nil, 0).Count(); n != 1 {
		t.Fatalf("live-only loc count = %d, want 1", n)
	}
	if _, ok := Locs(st, fakeTypes{}, ix, 20).Name("tree").Action("Chop down").Nearest(); !ok {
		t.Fatal("no choppable tree found")
	}
}

type recSession struct {
	st   *gamestate.State
	c    anticheat.Counters
	sent [][]byte
}

func (r *recSession) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(32)
	build(w)
	r.sent = append(r.sent, append([]byte(nil), w.Data[:w.Pos]...))
	return nil
}
func (r *recSession) State() *gamestate.State     { return r.st }
func (r *recSession) Cheats() *anticheat.Counters { return &r.c }

func TestInteractSendsTheLabelledOption(t *testing.T) {
	s := &recSession{st: gamestate.New()}
	npc := Entity{Kind: KindNpc, Index: 9, Ops: [5]string{"Talk-to", "", "Trade"}}
	if err := Interact(s, npc, "trade"); err != nil {
		t.Fatal(err)
	}
	want := packet.NewWriter(8)
	want.P1Enc(clientprot.OPNPC3)
	want.P2(9)
	if !bytes.Equal(s.sent[0], want.Data[:want.Pos]) {
		t.Fatalf("sent %x, want %x", s.sent[0], want.Data[:want.Pos])
	}
	if err := Interact(s, npc, "Attack"); err == nil {
		t.Fatal("an absent option must fail")
	}
	if err := Interact(s, Entity{Kind: KindPlayer}, "Follow"); err != ErrNoActions {
		t.Fatalf("player interact err = %v", err)
	}
}
