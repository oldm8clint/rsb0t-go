package bot

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/tile"
)

// objTable is a cache of a few objs: storeObjs without an archive. Ids from 30001 are tradeable; an
// id in donorplan's untradeable table (3058, the Mime top) is not.
type objTable struct{ objs []cachecfg.Obj }

func (t *objTable) ObjIDs(name string) []int {
	var out []int
	for _, o := range t.objs {
		if strings.EqualFold(o.Name, name) {
			out = append(out, o.ID)
		}
	}
	return out
}

func (t *objTable) Obj(id int) (cachecfg.Obj, bool) {
	for _, o := range t.objs {
		if o.ID == id {
			return o, true
		}
	}
	return cachecfg.Obj{}, false
}

// add puts an item (and, with a note, its bank note at id+1) in the table.
func (t *objTable) add(id int, name string, stackable, note bool) *objTable {
	t.objs = append(t.objs, cachecfg.Obj{ID: id, Name: name, Stackable: stackable, CertLink: -1, CertTemplate: -1})
	if note {
		t.objs = append(t.objs, cachecfg.Obj{ID: id + 1, Name: name, CertLink: id, CertTemplate: 799})
	}
	return t
}

func TestStoreRowForReadsTheCache(t *testing.T) {
	cfg := (&objTable{}).
		add(30001, "Dragon dagger", false, true).
		add(30003, "Arrow bundle", true, false).
		add(30005, "Odd key", false, false).
		add(30007, "Key", false, true).
		add(30009, "Key", false, true). // a name two objs share
		add(3058, "Mime top", false, true)
	cases := []struct {
		item string
		want mastercraftplan.StoreRow
	}{
		{"Dragon dagger", mastercraftplan.StoreRow{Item: "Dragon dagger", Count: 2, Tradeable: true, HasNote: true}},
		{" dragon dagger ", mastercraftplan.StoreRow{Item: "dragon dagger", Count: 2, Tradeable: true, HasNote: true}},
		{"Arrow bundle", mastercraftplan.StoreRow{Item: "Arrow bundle", Count: 2, Tradeable: true, Stackable: true}},
		{"Odd key", mastercraftplan.StoreRow{Item: "Odd key", Count: 2, Tradeable: true}},
		{"Key", mastercraftplan.StoreRow{Item: "Key", Count: 2}},                          // ambiguous: not tradeable here
		{"Mime top", mastercraftplan.StoreRow{Item: "Mime top", Count: 2, HasNote: true}}, // content: tradeable=no
		{"Unknown thing", mastercraftplan.StoreRow{Item: "Unknown thing", Count: 2}},
	}
	for _, c := range cases {
		got := storeRowFor(cfg, peerbank.Holding{Item: c.item, Count: 2})
		if got.Item != c.want.Item || got.Count != c.want.Count || got.Tradeable != c.want.Tradeable || got.Stackable != c.want.Stackable || got.HasNote != c.want.HasNote {
			t.Errorf("%q: %+v, want %+v", c.item, got, c.want)
		}
	}
	if got := storeRowFor(nil, peerbank.Holding{Item: "Dragon dagger", Count: 2}); got.Tradeable {
		t.Errorf("with no cache nothing is known to be tradeable: %+v", got)
	}
	if got := storeRowFor(cfg, peerbank.Holding{Item: "  ", Count: 2}); got.Tradeable || got.Item != "" {
		t.Errorf("an unnamed row: %+v", got)
	}
}

// publishRows puts a published bank of the given counts on the board as the mule's.
func publishRows(r *masterRig, rows map[string]int) {
	var hs []peerbank.Holding
	for k, v := range rows {
		hs = append(hs, peerbank.Holding{Item: k, Count: v})
	}
	r.ctx.Peers.Publish("mule", tile.Tile{X: 2809, Z: 3441}, true, hs, nil, false)
}

// The mule's published bank as rows: the merged "Herb" row is not a row of the bank (the grimy
// herbs are published again under their own names), a row this round has moved is gone, and a stale
// or empty bank is not a reading at all.
func TestMuleStoreRowsDropsTheMergedHerbRowAndWhatMoved(t *testing.T) {
	r := newSimRig(t, nil, nil, map[string]int{"Dragon dagger": 2, "Herb": 40, "Herb id199": 30, "Herb id207": 10, "Mime top": 1})
	cfg := (&objTable{}).add(30001, "Dragon dagger", false, true).add(3058, "Mime top", false, true)
	rows := muleStoreRows(r.ctx, "mule", cfg, map[string]bool{"mime top": true})
	got := map[string]int{}
	for _, row := range rows {
		got[row.Item] = row.Count
	}
	if !reflect.DeepEqual(got, map[string]int{"Dragon dagger": 2, "Herb id199": 30, "Herb id207": 10}) {
		t.Fatalf("rows %v", got)
	}
	if n := mastercraftplan.StoreTotal(rows); n != 3 {
		t.Fatalf("the bank has %d rows, want 3", n)
	}
	// No bank published, and one the mule has not refreshed in ten minutes: not a reading.
	r.ctx.Peers = peerbank.New()
	if muleStoreRows(r.ctx, "mule", cfg, nil) != nil {
		t.Fatal("nothing published is not an empty bank")
	}
}

func TestStoreHotReadsTheMulesHandOutRecord(t *testing.T) {
	r := newSimRig(t, nil, nil, nil)
	now := time.Now()
	if hot, covered := r.b.storeHot(r.ctx, now); hot != nil || covered != 0 {
		t.Fatalf("a context with no record: %v %v", hot != nil, covered)
	}
	var askedSince time.Time
	r.ctx.MuleOut = func(mule string, since time.Time) (map[string]int64, time.Time) {
		if mule != "mule" {
			t.Errorf("asked about %q", mule)
		}
		askedSince = since
		return map[string]int64{"rune axe": 2, "shark": 0}, now.Add(-50 * time.Hour)
	}
	hot, covered := r.b.storeHot(r.ctx, now)
	if askedSince != now.Add(-7*24*time.Hour) || covered != 50*time.Hour {
		t.Fatalf("since %v covered %v, want the 7 quiet days and 50 hours of record", askedSince, covered)
	}
	if !hot("Rune axe") || !hot(" rune AXE ") || hot("Shark") || hot("Mime top") {
		t.Fatal("hot: handed out in the quiet days, and only that")
	}
	// A record with nothing for this mule reaches back nowhere.
	r.ctx.MuleOut = func(string, time.Time) (map[string]int64, time.Time) { return nil, time.Time{} }
	if hot, covered := r.b.storeHot(r.ctx, now); hot == nil || covered != 0 || hot("anything") {
		t.Fatalf("an empty record: %v %v", hot != nil, covered)
	}
}

// storeRig is a MasterCrafter at the mule's post whose mule publishes a bank of 238 filler rows (1000
// of each: too big to move), the 30 single pieces "Piece 00" to "Piece 29", a Rune axe the mule
// handed out yesterday, and four Sharks (protected: food). The kit ask is a stub: the mule gives
// whatever the kit names (and decline says it declines), as a trade into the pack.
type storeRig struct {
	*masterRig
	ids   map[string]int32
	stock map[string]int // what the mule publishes
	asks  [][]fleetplan.ItemCount
	opts  []fleetrunner.KitBundleOptions
	serve func(it fleetplan.ItemCount) (int, string) // how many of a line arrive, or why not
}

func newStoreRig(t *testing.T, ownBank []actionsim.Stack) *storeRig {
	t.Helper()
	stock := map[string]int{"Rune axe": 1, "Shark": 4}
	for i := 0; i < 238; i++ {
		stock[fmt.Sprintf("Filler %03d", i)] = 1000
	}
	cfg := &objTable{}
	ids := map[string]int32{}
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("Piece %02d", i)
		stock[name] = 1
		id := 30001 + 2*i
		ids[name] = int32(id)
		cfg.add(id, name, false, true)
	}
	r := &storeRig{masterRig: newSimRig(t, nil, ownBank, stock), ids: ids, stock: stock}
	r.b.objs = cfg
	now := time.Now()
	r.ctx.MuleOut = func(string, time.Time) (map[string]int64, time.Time) {
		return map[string]int64{"rune axe": 1}, now.Add(-72 * time.Hour)
	}
	r.serve = func(it fleetplan.ItemCount) (int, string) { return it.Count, "" }
	scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, items []fleetplan.ItemCount, opts fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		r.asks = append(r.asks, append([]fleetplan.ItemCount(nil), items...))
		r.opts = append(r.opts, opts)
		res := fleetrunner.KitResult{Got: map[string]int{}, Declined: map[string]string{}}
		for _, it := range items {
			n, why := r.serve(it)
			if n > 0 {
				r.sim.Give(r.ids[it.Item], n)
				res.Got[it.Item] = n
			}
			if why != "" {
				res.Declined[it.Item] = why
			}
		}
		return res, fleetrunner.KitBundleInfo{Bundled: true}
	}
	return r
}

func (r *storeRig) bankHolds(name string) int { return simCount(r.sim.Bank(), r.ids[name]) }

// republish is the mule's bank after it has served kits: the rows gone from it, published again.
func (r *storeRig) republish(gone ...string) {
	for _, g := range gone {
		delete(r.stock, g)
	}
	publishRows(r.masterRig, r.stock)
}

// The whole round: 270 published rows against a target of 215, 30 single pieces to move and nothing
// else that can. It asks in kits of 28 lines (one trade), banks what arrives between kits, does not
// ask for the same row twice although the published bank has not changed, and ends when nothing is
// left to move. Rune axe (handed out lately) and Shark (food) stay.
func TestStoreRoundMovesColdSmallRowsInKitsOf28(t *testing.T) {
	r := newStoreRig(t, []actionsim.Stack{{ID: 1511, Count: 10}})
	out := r.b.storeRound(r.ctx, r.b.deps(r.ctx), time.Now())
	if out.Mode != mastercraftplan.ModeStore || out.Transient || out.Produced != 30 || out.HandoverTried {
		t.Fatalf("%+v", out)
	}
	if len(r.asks) != 2 || len(r.asks[0]) != 28 || len(r.asks[1]) != 2 {
		var sizes []int
		for _, a := range r.asks {
			sizes = append(sizes, len(a))
		}
		t.Fatalf("kits of %v lines, want 28 then 2", sizes)
	}
	if first := r.asks[0][0]; first.Item != "Piece 00" || first.Count != 1 || r.asks[1][0].Item != "Piece 28" {
		t.Fatalf("asked %v then %v", r.asks[0][:2], r.asks[1])
	}
	// Every kit is asked as notes, with no deposit visit of the kit's own: the round banked the pack
	// just before each ask (the a user request).
	for i, o := range r.opts {
		if !o.Noted || o.Bank != (tile.Tile{}) || !o.KeepNotes {
			t.Fatalf("kit %d asked with %+v, want noted, kept as notes, no bank visit before it", i, o)
		}
	}
	for _, ask := range r.asks {
		for _, it := range ask {
			if it.Item == "Rune axe" || it.Item == "Shark" || strings.HasPrefix(it.Item, "Filler") {
				t.Fatalf("asked for %s", it.Item)
			}
		}
	}
	for name := range r.ids {
		if r.bankHolds(name) != 1 {
			t.Fatalf("%s is not in this account's bank: %v", name, r.sim.Bank())
		}
	}
	if len(r.sim.Pack()) != 0 {
		t.Fatalf("everything that arrived is banked: %v", r.sim.Pack())
	}
	l := &r.b.ledger
	if l.RowsStored != 30 || l.ItemsStored != 30 || len(l.StoredNames()) != 30 || l.Stored["piece 07"] != 1 {
		t.Fatalf("ledger %+v", l)
	}
	if !r.logged("store: asked mule for 28 row(s)") || !strings.Contains(out.Why, "30 row(s) (30 item(s))") || !strings.Contains(out.Why, "which now holds 31 row(s)") {
		t.Fatalf("%q\n%v", out.Why, r.logs)
	}
	r.b.afterRound(r.ctx, out, time.Second)
	if l.StoreRounds != 1 || !l.StorePauseUntil.IsZero() || l.Fails(mastercraftplan.ModeStore) != 0 {
		t.Fatalf("a round that moved rows sets no wait: %+v", l)
	}
	// What the round stored is what ScarabSwarm's loot hand-off is told to keep.
	if keep := r.b.storeKeep(); len(keep) != 30 || keep[0] != "piece 00" {
		t.Fatalf("keep %v", keep)
	}
}

// A kit stops at the target: 244 rows to 215 is 29, and one kit carries 28 lines, so the second kit
// asks for one more and the round ends there with 29 moved. Nothing is asked when the mule is
// already at the target.
func TestStoreRoundStopsAtTheTargetRows(t *testing.T) {
	r := newStoreRig(t, nil)
	r.b.set.StoreToRows = 241 // 270 published rows: 29 to move
	out := r.b.storeRound(r.ctx, r.b.deps(r.ctx), time.Now())
	if out.Produced != 29 || len(r.asks) != 2 || len(r.asks[0]) != 28 || len(r.asks[1]) != 1 {
		t.Fatalf("produced %d in %d kits: %+v", out.Produced, len(r.asks), out)
	}
	r2 := newStoreRig(t, nil)
	r2.b.set.StoreToRows = 270
	r2.b.set.StoreAtRows = 271
	out = r2.b.storeRound(r2.ctx, r2.b.deps(r2.ctx), time.Now())
	if len(r2.asks) != 0 || out.Produced != 0 || out.Transient || !strings.Contains(out.Why, "nothing to move") {
		t.Fatalf("asks %d: %+v", len(r2.asks), out)
	}
	r2.b.afterRound(r2.ctx, out, time.Second)
	if r2.b.ledger.StorePauseUntil.IsZero() {
		t.Fatal("a round that found nothing pauses the storage rounds")
	}
}

// A line the mule declines for a reason that belongs to the item is remembered across rounds; a
// line that comes back "none in the bank" (a stale published row) is only left out for the rest of
// this round, and so is one that is never answered.
func TestStoreRoundRemembersRefusalsThatBelongToTheItem(t *testing.T) {
	r := newStoreRig(t, nil)
	r.serve = func(it fleetplan.ItemCount) (int, string) {
		switch it.Item {
		case "Piece 03":
			return 0, "declined: not an item I hand out"
		case "Piece 04":
			return 0, "declined: none in the bank"
		case "Piece 05":
			return 0, "" // never answered
		}
		return it.Count, ""
	}
	out := r.b.storeRound(r.ctx, r.b.deps(r.ctx), time.Now())
	if out.Produced != 27 || out.Transient || len(r.asks) != 2 || len(r.asks[0]) != 28 || len(r.asks[1]) != 2 {
		t.Fatalf("%d kits: %+v", len(r.asks), out)
	}
	if r.asks[1][0].Item != "Piece 28" || r.asks[1][1].Item != "Piece 29" {
		t.Fatalf("the second kit asks again for what was left out: %v", r.asks[1])
	}
	l := &r.b.ledger
	if !l.StoreRefusedNow("Piece 03", time.Now()) || l.StoreRefusedNow("Piece 04", time.Now()) || l.StoreRefusedNow("Piece 05", time.Now()) {
		t.Fatalf("refusals %v", l.StoreRefused)
	}
	if !r.logged("store: mule will not hand out Piece 03") {
		t.Fatalf("the refusal is not logged: %v", r.logs)
	}
	// The mule has banked what it gave away; the next round leaves Piece 03 out for the week and asks
	// for the other two again.
	var moved []string
	for name := range r.ids {
		if r.bankHolds(name) > 0 {
			moved = append(moved, name)
		}
	}
	r.republish(moved...)
	r.asks = nil
	r.serve = func(it fleetplan.ItemCount) (int, string) { return it.Count, "" }
	out = r.b.storeRound(r.ctx, r.b.deps(r.ctx), time.Now())
	if len(r.asks) != 1 || len(r.asks[0]) != 2 || r.asks[0][0].Item != "Piece 04" || r.asks[0][1].Item != "Piece 05" || out.Produced != 2 {
		t.Fatalf("%v: %+v", r.asks, out)
	}
}

// A kit the mule defers (its per-donor window) or fails is a passing failure: the storage rounds back
// off. A mule that holds none of the rows, or refuses every one for its own reasons, is a round that
// found nothing: they pause.
func TestStoreRoundFailuresBackOffAndEmptyAsksPause(t *testing.T) {
	for name, c := range map[string]struct {
		serve     func(fleetplan.ItemCount) (int, string)
		transient bool
	}{
		"deferred":       {func(fleetplan.ItemCount) (int, string) { return 0, "declined: already handed you one recently" }, true},
		"never answered": {func(fleetplan.ItemCount) (int, string) { return 0, "" }, true},
		"none in bank":   {func(fleetplan.ItemCount) (int, string) { return 0, "declined: none in the bank" }, false},
		"refused all":    {func(fleetplan.ItemCount) (int, string) { return 0, "declined: not an item I hand out" }, false},
	} {
		r := newStoreRig(t, nil)
		r.serve = c.serve
		out := r.b.storeRound(r.ctx, r.b.deps(r.ctx), time.Now())
		if out.Transient != c.transient || out.Produced != 0 || len(r.asks) != 1 {
			t.Fatalf("%s: %d kits, %+v", name, len(r.asks), out)
		}
		r.b.afterRound(r.ctx, out, time.Second)
		l := &r.b.ledger
		if c.transient && (l.Fails(mastercraftplan.ModeStore) != 1 || !l.StorePauseUntil.IsZero()) {
			t.Fatalf("%s: fails %d pause %v", name, l.Fails(mastercraftplan.ModeStore), l.StorePauseUntil)
		}
		if !c.transient && (l.Fails(mastercraftplan.ModeStore) != 0 || l.StorePauseUntil.IsZero()) {
			t.Fatalf("%s: fails %d pause %v", name, l.Fails(mastercraftplan.ModeStore), l.StorePauseUntil)
		}
	}
}

// The round moves nothing it cannot call unused: no hand-out record, a record under a day, or this
// account's bank too full to keep its reserve.
func TestStoreRoundAsksNothingBlind(t *testing.T) {
	r := newStoreRig(t, nil)
	r.ctx.MuleOut = nil
	out := r.b.storeRound(r.ctx, r.b.deps(r.ctx), time.Now())
	if len(r.asks) != 0 || out.Transient || !strings.Contains(out.Why, "hand-out record is not available") {
		t.Fatalf("no record: %d kits, %+v", len(r.asks), out)
	}
	r = newStoreRig(t, nil)
	r.ctx.MuleOut = func(string, time.Time) (map[string]int64, time.Time) {
		return nil, time.Now().Add(-6 * time.Hour)
	}
	if out := r.b.storeRound(r.ctx, r.b.deps(r.ctx), time.Now()); len(r.asks) != 0 || !strings.Contains(out.Why, "reaches back only") {
		t.Fatalf("a short record: %d kits, %+v", len(r.asks), out)
	}
	var full []actionsim.Stack
	for i := 0; i < mastercraftplan.BankRows-mastercraftplan.StoreOwnReserve; i++ {
		full = append(full, actionsim.Stack{ID: int32(50000 + i), Count: 1})
	}
	r = newStoreRig(t, full)
	if out := r.b.storeRound(r.ctx, r.b.deps(r.ctx), time.Now()); len(r.asks) != 0 || !strings.Contains(out.Why, "no room") {
		t.Fatalf("a full own bank: %d kits, %+v", len(r.asks), out)
	}
}

// A mule that is not here is not walked to, and a walk that does not arrive ends the round before any
// bank is touched: both passing failures.
func TestStoreRoundNeedsTheMuleAtItsPost(t *testing.T) {
	r := newStoreRig(t, nil)
	r.ctx.Bus = mulebus.NewBus() // no heartbeat
	if out := r.b.storeRound(r.ctx, r.b.deps(r.ctx), time.Now()); !out.Transient || len(r.asks) != 0 {
		t.Fatalf("no mule: %+v", out)
	}
	r = newStoreRig(t, nil)
	masterWalkTo = func(*fleetrunner.Deps, tile.Tile, int, time.Duration) bool { return false }
	if out := r.b.storeRound(r.ctx, r.b.deps(r.ctx), time.Now()); !out.Transient || !strings.Contains(out.Why, "could not reach") || len(r.asks) != 0 {
		t.Fatalf("no arrival: %+v", out)
	}
}

// decideLive reads the mule's published bank, the hand-out record and this account's own bank: 270
// rows and a record three days long make a storage round due, and it is due ahead of the later
// rounds only (gems, bows and pearls first).
func TestMasterCrafterDecidesAStorageRoundFromThePublishedBank(t *testing.T) {
	r := newStoreRig(t, nil)
	r.b.decide = nil // the real one
	mindHoldStat(r.ctx.Session.State(), gemrunner.CraftingStat, levels.XPAtLevel(30), 30)
	mindHoldStat(r.ctx.Session.State(), bowstringrunner.FletchingStat, levels.XPAtLevel(60), 60)
	d := r.b.decideLive(r.ctx)
	if d.Mode != mastercraftplan.ModeStore || d.StoreRows != 30 || !strings.Contains(d.Why, "270 rows") {
		t.Fatalf("%+v", d)
	}
	// Bows the mule holds come first.
	rows := map[string]int{"Maple longbow": 400, "Bow string": 400}
	for i := 0; i < 270; i++ {
		rows[fmt.Sprintf("Filler %03d", i)] = 1000
	}
	publishRows(r.masterRig, rows)
	if d := r.b.decideLive(r.ctx); d.Mode != mastercraftplan.ModeBows {
		t.Fatalf("bows before the storage round: %+v", d)
	}
	// No record, no round, and said so.
	r.ctx.MuleOut = nil
	publishRows(r.masterRig, map[string]int{"Mime top": 1})
	if d := r.b.decideLive(r.ctx); d.Mode != mastercraftplan.ModeSwarm || !strings.Contains(d.Why, "store: the mule's bank has 1 rows") {
		t.Fatalf("%+v", d)
	}
	// Switched off, the decision does not even read the record.
	r.b.set.StoreOverflow = false
	read := false
	r.ctx.MuleOut = func(string, time.Time) (map[string]int64, time.Time) { read = true; return nil, time.Time{} }
	r.b.decideLive(r.ctx)
	if read {
		t.Fatal("a switched-off storage round read the hand-out record")
	}
}

// The bot adds what only a runner package knows to the plan's protection: the caskets and oysters the
// mule's own idle pass opens.
func TestStoreProtectedAddsTheMulesOwnOpenables(t *testing.T) {
	for _, n := range []string{"Casket", "oyster", " Oyster "} {
		if !storeProtected(n) {
			t.Errorf("%q is the mule's own work and must stay", n)
		}
	}
	if storeProtected("Dragon longsword") || !storeProtected("Law rune") {
		t.Error("the plan's own rules still apply")
	}
}
