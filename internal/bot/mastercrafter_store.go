package bot

// MasterCrafter's side store, MasterCrafter.json beside ScarabSwarm's checkpoint under
// data/scarabswarm-checkpoints/<account>/ (the same checkpointDir setting).
//
// It holds mastercraftplan.Ledger: the strung bows handed to the mule that its published count may
// still include, the pause after a round that found nothing, products or uncut gems still in the
// bank, and the lifetime counters. The rules are the ones ScarabSwarmAsk.json keeps
// (scarabswarm_askstore.go): decoded leniently with fields a newer build wrote kept and written
// back; a file that cannot be read is moved aside (never overwritten) and the account starts with
// an empty ledger, which costs at worst one useless round, never progress.

import (
	"fmt"
	"os"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/settings"
)

// MasterCrafterStoreID is the side store's settings.Store key.
const MasterCrafterStoreID = "MasterCrafter"

// loadLedger sets b.ledger from the store, or leaves it empty. It never fails the start.
func (b *masterCrafterBot) loadLedger(ctx *Context) {
	b.ledger = mastercraftplan.Ledger{}
	b.ledgerKeep = settings.Unknown{}
	b.ledgerBlocked = ""
	if ctx.Username == "" || b.store == nil {
		return
	}
	path, err := b.store.Path(ctx.Username, MasterCrafterStoreID)
	if err != nil {
		return // Save fails the same way, so nothing can be overwritten
	}
	bag, err := b.store.Load(ctx.Username, MasterCrafterStoreID)
	if err == nil {
		var led mastercraftplan.Ledger
		keep, decErr := bag.DecodeCompat(&led)
		if decErr == nil {
			b.ledger, b.ledgerKeep = led, keep
			if !keep.Empty() {
				b.logf(ctx, "%s carries %d field(s) this build does not know (%s) — kept and written back on every save",
					path, len(keep.Paths()), strings.Join(keep.Paths(), ", "))
			}
			return
		}
		err = decErr
	}
	aside := fmt.Sprintf("%s.bad-%s", path, time.Now().UTC().Format("20060102T150405Z"))
	if rerr := os.Rename(path, aside); rerr != nil {
		b.ledgerBlocked = path
		b.logf(ctx, "the ledger %s cannot be read (%v) and could not be moved aside (%v) — starting empty, and the file is left exactly as it is",
			path, err, rerr)
		return
	}
	b.logf(ctx, "the ledger %s cannot be read (%v) — kept as %s; starting empty", path, err, aside)
}

// saveLedger writes b.ledger, with every field a newer build wrote put back.
func (b *masterCrafterBot) saveLedger(ctx *Context) {
	if b.store == nil || ctx.Username == "" || b.ledgerBlocked != "" {
		return
	}
	bag := settings.NewBag()
	if err := bag.EncodeCompat(b.ledger, b.ledgerKeep); err != nil {
		b.logf(ctx, "could not encode the ledger: %v", err)
		return
	}
	if err := b.store.Save(ctx.Username, MasterCrafterStoreID, bag); err != nil {
		b.logf(ctx, "could not save the ledger: %v", err)
	}
}
