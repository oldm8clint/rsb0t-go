package bot

import (
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/muleledger"
	"rsb0t-go/internal/scripts/mulerunner"
)

// The MuleReceiver's writes to the fleet's trade record (internal/muleledger), kept out of
// mule.go's own flow: each is one call placed after the trade code has already decided, and
// nothing here is read back by any decision. Every call is a no-op while Context.TradeLog is nil.

// recordTrade writes one handled receive: an accepted trade with the stacks mulerunner verified
// arrived in the pack, or a failed one that moved nothing.
func recordTrade(ctx *Context, res mulerunner.CycleResult) {
	if ctx.TradeLog == nil || !res.Handled {
		return
	}
	e := muleledger.Entry{Mule: ctx.Username, Partner: res.Partner, Kind: muleledger.KindReceive, Outcome: muleledger.OutcomeFailed, Reason: res.Reason}
	if res.Accepted {
		e.Outcome, e.Reason = muleledger.OutcomeAccepted, ""
		for _, it := range res.Received {
			e.In = append(e.In, ledgerItem(ctx, int(it.ID), "", int64(it.Count)))
		}
	}
	ctx.TradeLog(e)
}

// recordHandout writes one hand-out attempt: a confirmed delivery of count of item to partner,
// or a failure with the order code's own reason.
func recordHandout(ctx *Context, partner string, res mulerunner.HandoutResult) {
	if ctx.TradeLog == nil || res.Stopped {
		return
	}
	e := muleledger.Entry{Mule: ctx.Username, Partner: partner, Kind: muleledger.KindHandout, Outcome: muleledger.OutcomeFailed, Reason: res.Reason}
	if res.Delivered {
		e.Outcome, e.Reason = muleledger.OutcomeAccepted, ""
		e.Out = []muleledger.Item{ledgerItem(ctx, -1, res.Item, int64(res.Crossed))}
	}
	ctx.TradeLog(e)
}

// ledgerItem names one stack through the cache config: by id when known, else the id of the
// unnoted item called name.
func ledgerItem(ctx *Context, id int, name string, count int64) muleledger.Item {
	it := muleledger.Item{ID: id, Name: name, Count: count}
	if ctx.Config == nil {
		return it
	}
	if id >= 0 {
		if o, ok := ctx.Config.Obj(id); ok && it.Name == "" {
			it.Name = o.Name
		}
		return it
	}
	for _, cand := range ctx.Config.ObjIDs(name) {
		if o, ok := ctx.Config.Obj(cand); ok && !o.Noted() && ctx.Config.IsNamed(name, cand) {
			it.ID = cand
			if _, _, qualified := cachecfg.SplitQualified(name); qualified {
				it.Name = o.Name // a qualified name ("Yew shortbow id68") is shown as the item it names
			}
			break
		}
	}
	return it
}
