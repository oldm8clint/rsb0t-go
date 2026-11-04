package bot

// MasterCrafter's storage round (a user request: "the mule can use masters bank for extra space").
// the mule's bank was at 265 rows of 288 and, with the 48 ScarabSwarm's loot hand-off keeps free,
// refused every new loot type (mastercraftplan/store.go has the live evidence, the content facts and
// the rules). The round asks the mule for the whole of each cold, small, tradeable row of its
// published bank, 28 lines a kit, and banks what arrives in the main account's own bank.
//
//	1. at the mule's post: bank the pack, read this account's own bank (its rows are what a kit may
//	   bring in)
//	2. plan a kit (mastercraftplan.PlanStoreAsk) from the mule's published bank, less what the round
//	   has already moved or been refused, and ask for it (askMule, the bundled kit every round uses)
//	3. bank what arrived, count what crossed in full (a freed row), what came short, what was
//	   declined (and remember a refusal that belongs to the item), and plan the next kit; at most
//	   masterSupplyAsks kits, fewer when the plan says the mule's bank is down to craftStoreToRows
//
// The mule's published bank is re-read for every kit but lags a bank visit behind the mule, so rows
// the round already moved are taken out of it by name.
//
// THE ITEMS STAY HERE. Nothing hands them back: a later ask for one of them goes to this account,
// and ScarabSwarm's loot hand-off, which runs on this account between rounds, is told to keep every
// name the round has stored (Ledger.Stored, scarabSwarmBot.extraKeep).

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fisherrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/peerplan"
	"rsb0t-go/internal/scripts/peerrunner"
	"rsb0t-go/internal/scripts/tile"
)

// storeObjs is what the row builder reads of the cache: *cachecfg.Config is one, a test supplies a
// table.
type storeObjs interface {
	ObjIDs(name string) []int
	Obj(id int) (cachecfg.Obj, bool)
}

// storeRowFor weighs one published holding for the round: whether its name is ONE obj that content
// lets players trade, and what the mule's slot plan will read of it (stackable, a bank note). A name
// several objs share ("Key", the 14 grimy herbs' "Herb", every clue scroll step) is not tradeable
// here: the mule's bank holds one of them and a published row cannot say which, so neither can
// tradeable=no. With no cache the row is not tradeable at all.
func storeRowFor(cfg storeObjs, h peerbank.Holding) mastercraftplan.StoreRow {
	row := mastercraftplan.StoreRow{Item: strings.TrimSpace(h.Item), Count: h.Count}
	if cfg == nil || row.Item == "" {
		return row
	}
	var plain []cachecfg.Obj
	var notes []cachecfg.Obj
	for _, id := range cfg.ObjIDs(row.Item) {
		o, ok := cfg.Obj(id)
		switch {
		case !ok:
		case o.Noted():
			notes = append(notes, o)
		default:
			plain = append(plain, o)
		}
	}
	if len(plain) != 1 {
		return row
	}
	o := plain[0]
	row.Tradeable = !donorplan.Untradeable(o.ID)
	row.Stackable = o.Stackable
	for _, n := range notes {
		if n.CertLink == o.ID {
			row.HasNote = true
		}
	}
	return row
}

// muleBankHoldings is the mule's published bank (the peer board) as it holds it, ok false when it
// has not published one recently (peerplan.DefaultMaxAge, as muleBankKinds and muleBankCount read it).
func muleBankHoldings(ctx *Context, mule string) ([]peerbank.Holding, bool) {
	if ctx == nil || ctx.Peers == nil {
		return nil, false
	}
	want := strings.ToLower(strings.TrimSpace(mule))
	now := time.Now()
	for _, e := range ctx.Peers.Entries(now) {
		if strings.ToLower(strings.TrimSpace(e.Account)) != want {
			continue
		}
		if e.UpdatedAt.IsZero() || now.Sub(e.UpdatedAt) > peerplan.DefaultMaxAge || len(e.Bank) == 0 {
			return nil, false
		}
		return e.Bank, true
	}
	return nil, false
}

// storeObjects is the cache the row builder reads: the bot's own (a test supplies a table), else the
// context's, else none.
func (b *masterCrafterBot) storeObjects(ctx *Context) storeObjs {
	switch {
	case b.objs != nil:
		return b.objs
	case ctx.Config != nil:
		return ctx.Config
	}
	return nil
}

// muleStoreRows is the mule's published bank as the round's rows: every holding of it with an item,
// except the merged rows of peerrunner.IDKeyedNames ("Herb" counts every grimy herb, a bow's name
// both its strung and unstrung form: they are each published again under their own qualified name,
// and counting both would count a row that is not in the bank), and none of the names in gone (rows
// this round has already moved). nil when the mule has not published.
func muleStoreRows(ctx *Context, mule string, cfg storeObjs, gone map[string]bool) []mastercraftplan.StoreRow {
	hs, ok := muleBankHoldings(ctx, mule)
	if !ok {
		return nil
	}
	rows := make([]mastercraftplan.StoreRow, 0, len(hs))
	for _, h := range peerrunner.ObjRows(hs) {
		name := strings.ToLower(strings.TrimSpace(h.Item))
		if name == "" || h.Count <= 0 || gone[name] {
			continue
		}
		rows = append(rows, storeRowFor(cfg, h))
	}
	return rows
}

// storeHot reads the mule's hand-out record (Context.MuleOut) for the quiet days: hot says an item
// was handed out in them, covered how far back the record reaches. nil hot, 0 covered when the
// context has no record at all.
func (b *masterCrafterBot) storeHot(ctx *Context, now time.Time) (hot func(string) bool, covered time.Duration) {
	if ctx.MuleOut == nil {
		return nil, 0
	}
	since := now.Add(-time.Duration(b.set.StoreQuietDays) * 24 * time.Hour)
	items, from := ctx.MuleOut(b.mule, since)
	if !from.IsZero() {
		covered = max(0, now.Sub(from))
	}
	return func(item string) bool { return items[strings.ToLower(strings.TrimSpace(item))] > 0 }, covered
}

// storeInput is the round's reading of the mule at now: the published rows less gone, the hand-out
// record, and what to skip (gone, skip, and refusals the ledger remembers). ok false when the mule
// has not published.
func (b *masterCrafterBot) storeInput(ctx *Context, now time.Time, ownRows int, gone, skip map[string]bool) (mastercraftplan.StoreInput, bool) {
	rows := muleStoreRows(ctx, b.mule, b.storeObjects(ctx), gone)
	if rows == nil {
		return mastercraftplan.StoreInput{}, false
	}
	hot, covered := b.storeHot(ctx, now)
	return mastercraftplan.StoreInput{
		Rows: rows, OwnRows: ownRows, Hot: hot, Covered: covered,
		Protected: storeProtected,
		Skip: func(item string) bool {
			key := strings.ToLower(strings.TrimSpace(item))
			return skip[key] || gone[key] || b.ledger.StoreRefusedNow(item, now)
		},
	}, true
}

// storeSnapshot fills the storage round's part of a decision snapshot.
func (b *masterCrafterBot) storeSnapshot(ctx *Context, s *mastercraftplan.Snapshot) {
	if !b.set.StoreOverflow {
		return
	}
	if in, ok := b.storeInput(ctx, s.Now, len(ownBankItems(ctx)), nil, nil); ok {
		s.Store = in
	}
}

// storeProtected is mastercraftplan.StoreProtected plus what only a runner package knows: the caskets
// and oysters the mule's own idle pass opens (fisherrunner.Openables), which it banks for that.
func storeProtected(item string) bool {
	return mastercraftplan.StoreProtected(item) || fisherrunner.IsOpenable(item)
}

// storeKeep is the names ScarabSwarm's loot hand-off must keep on this account: everything a storage
// round moved into its bank, and while the Runecrafting round is on and unfinished its talismans and its
// essence (the swarm's consolidate classes them as loot and would carry them straight back to the mule).
// runeLevel is the Runecrafting level the last check or round read (0 until one has: kept).
func (b *masterCrafterBot) storeKeep() []string {
	return append(b.ledger.StoredNames(), mastercraftplan.RuneKeepNames(b.set, b.runeLevel)...)
}

// storeRound is the storage round: see the file comment.
func (b *masterCrafterBot) storeRound(ctx *Context, d *fleetrunner.Deps, started time.Time) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeStore}
	post, why, ok := b.muleHere(ctx)
	if !ok {
		out.Transient, out.Why = true, why
		return out
	}
	if why, ok := b.arrive(ctx, d, post); !ok {
		out.Transient, out.Why = true, why
		return out
	}
	// 1. The pack banked and this account's own bank read. The full visit teaches the quick bank
	// every deposit after a kit uses (the a user request: "it should use the fast option for
	// banking"): live on def92a2e two full visits a kit, the kit's own and this one, took 36 s.
	var qb mastercraftrunner.QuickBank
	v := mastercraftrunner.FullVisit(d, post, &qb, nil, nil, false).Visit
	if !v.Opened || v.Err != "" {
		out.Transient, out.Why = true, "the bank at the mule's post: "+v.Err
		return out
	}
	ownRows := len(v.Banked)
	reason := ""
	hold := b.roundHold(ctx, d, started, &reason)
	gone := map[string]bool{} // rows that crossed in full this round
	skip := map[string]bool{} // rows a kit of this round came back without
	rows, items, asks, mulesRows := 0, 0, 0, 0
	for asks < masterSupplyAsks && hold() {
		in, known := b.storeInput(ctx, time.Now(), ownRows, gone, skip)
		if !known {
			reason = "the mule has not published its bank"
			break
		}
		if mulesRows == 0 {
			mulesRows = mastercraftplan.StoreTotal(in.Rows)
		}
		lines := mastercraftplan.PlanStoreAsk(b.set, in)
		if len(lines) == 0 {
			if asks == 0 {
				r := mastercraftplan.StoreReady(b.set, in)
				reason = "nothing to move: " + r.Why
			}
			break
		}
		asks++
		// As notes: a row of 4 kiteshields is one trade slot, not four (the a user request:
		// "the mule should hand over items noted"). The pack was banked just before, so the kit makes
		// no deposit visit of its own.
		a := b.askMuleWith(ctx, d, lines, masterAskOpts{noted: true, banked: true})
		b.logf(ctx, "store: asked %s for %d row(s), %s: %s", b.mule, len(lines), describeLines(lines), masterDescribe(a.Got))
		if a.Stopped {
			reason = "stopped"
			break
		}
		// What came: banked first, so the next kit is planned against the room this account has now.
		dv := storeBank(ctx, d, post, &qb)
		if dv.Opened {
			ownRows = len(dv.Banked)
		}
		moved, units := b.noteStored(ctx, lines, a, gone, skip, time.Now())
		rows, items = rows+moved, items+units
		b.saveLedger(ctx) // what is stored here is what ScarabSwarm's hand-off must keep: not lost to a crash
		if !dv.Opened || dv.Err != "" {
			out.Transient, reason = true, "the bank at the mule's post, after the kit: "+dv.Err
			break
		}
		if a.Traded == 0 {
			// No row came. A mule that holds none of them (the published bank was stale) or refused
			// every line for its own reasons is a round that found nothing (a pause); a kit it deferred
			// or a trade that failed is a passing failure (a backoff).
			out.Transient = !a.declinedEmpty() && !declinedAllForTheItem(a)
			reason = firstNonEmpty(a.Why, "no row arrived")
			break
		}
	}
	b.ledger.RowsStored += rows
	b.ledger.ItemsStored += items
	out.Produced = rows
	out.Why = fmt.Sprintf("%d row(s) (%d item(s)) moved from %s's bank of %d published row(s) to this account's bank, which now holds %d row(s) (%s)",
		rows, items, b.mule, mulesRows, ownRows, firstNonEmpty(reason, "the mule's bank is down to the target"))
	return out
}

// storeBank banks what a storage kit brought: a quick visit (the booth op, Deposit-All on each kind
// in the pack and the close, sent together) when the account stands where the full visit opened
// the bank, else a full visit, which teaches the quick one again. Banked is this account's bank
// as the session saw it after the deposit.
func storeBank(ctx *Context, d *fleetrunner.Deps, post tile.Tile, qb *mastercraftrunner.QuickBank) mastercraftrunner.Visit {
	if qb.Ready(d) {
		if qv := qb.Visit(d, nil, nil); qv.Done {
			return mastercraftrunner.Visit{Opened: true, Banked: ownBankItems(ctx)}
		}
		qb.Forget()
	}
	return mastercraftrunner.FullVisit(d, post, qb, nil, nil, false).Visit
}

// declinedAllForTheItem says every line the mule declined was declined for a reason that belongs to
// the item (mastercraftplan.StoreRefusalIsTheItem), which another kit of other rows will not meet.
func declinedAllForTheItem(a masterAsk) bool {
	if len(a.Declined) == 0 {
		return false
	}
	for _, why := range a.Declined {
		if !mastercraftplan.StoreRefusalIsTheItem(why) {
			return false
		}
	}
	return true
}

// noteStored settles one kit against what arrived: a line that came in full is a freed row (gone,
// and its units remembered in the ledger as stored here); one that came short is kept for the rest
// of the round; one that was declined is skipped for the round and, when the mule refused it for a
// reason that belongs to the item, remembered across rounds. It returns the rows that arrived (in
// full or short) and the items.
func (b *masterCrafterBot) noteStored(ctx *Context, lines []mastercraftplan.Line, a masterAsk, gone, skip map[string]bool, now time.Time) (rows, items int) {
	for _, l := range lines {
		key := strings.ToLower(strings.TrimSpace(l.Item))
		got := a.Got[l.Item]
		switch {
		case got >= l.Qty:
			gone[key] = true
		default:
			skip[key] = true
		}
		if got > 0 {
			rows++
			items += got
			b.ledger.NoteStored(l.Item, got)
			continue
		}
		if why := a.Declined[l.Item]; why != "" && mastercraftplan.StoreRefusalIsTheItem(why) {
			b.ledger.NoteStoreRefused(l.Item, now)
			b.logf(ctx, "store: %s will not hand out %s (%s): leaving it out for %d days", b.mule, l.Item, why, int(mastercraftplan.StoreRefusedFor/(24*time.Hour)))
		}
	}
	return rows, items
}
