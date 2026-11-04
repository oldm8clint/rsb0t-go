package bot

// Fleet-wide peer item sharing, wired into a Context so any script can use it in one line.
//
// Kept entirely in its own file, with no state on any existing bot struct and no edits to any
// script, deliberately: several sessions are working in internal/bot at once, and every other
// recent addition here (fisherhandout.go, woodcutteraxe.go) took the same shape for the same
// reason. See COLLAB.md.
//
// WHAT A SCRIPT HAS TO DO, in full:
//
//	 1. Once per bank trip, while the bank is ALREADY OPEN:
//	        bot.PublishBank(ctx, bank.Items(), working)
//	    That is the entire cost of fleet-wide bank visibility — no snapshot, no fleet poll,
//	    nothing on /api/status's locks. See internal/peerbank's package doc.
//
//	 2. Where the script currently walks to the mule for an item, ask the fleet first:
//	        res := bot.AskPeerFirst(ctx, opts)
//	        if res.Got { ... }            // it arrived, receipt-verified
//	        if res.UseMule { ...existing mule path... }
//	        if res.Busy { ...go back to work, retry after peerplan.BusyRetry... }
//
//	 3. Once per Loop, answer other accounts:
//	        bot.ServePeers(ctx, w, allowed, bankItems)
//
// NOTHING IS WIRED INTO ANY SCRIPT BY THIS FILE, and that is on purpose rather than an
// omission. The obvious first caller is ProgressiveWoodcutter's axe path — 94 accounts walking
// 83 tiles to a mule that holds one axe, while a dozen of their own cohort sit on banked bronze
// axes — but internal/bot/woodcutter*.go is held by another session right now. The three calls
// above are what that wiring is, and they are proven end to end by cmd/peersharetest.

import (
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/peerplan"
	"rsb0t-go/internal/scripts/peerrunner"
	"rsb0t-go/internal/scripts/tile"
)

// PeerDeps builds the runner dependencies from a Context. Returns nil when peer sharing is not
// configured for this fleet, which every caller below treats as "off".
func PeerDeps(ctx *Context) *peerrunner.Deps {
	if ctx == nil || ctx.Peers == nil || ctx.Bus == nil || ctx.Username == "" {
		return nil
	}
	return &peerrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat,
		Me: ctx.Username, Bus: ctx.Bus, Dir: ctx.Peers,
		Delay: ctx.Delay,
		Log:   ctx.Log,
	}
}

// PublishBank records this account's bank in the shared directory. Call it once per bank trip
// with a bank that is already open; it costs one map write and reads nothing extra.
//
// working says the account is mid-task rather than standing at a bank, which only ever costs
// it a ranking penalty when somebody is choosing whom to interrupt.
func PublishBank(ctx *Context, bankItems []gamestate.SlotItem, working bool) {
	d := PeerDeps(ctx)
	if d == nil {
		return
	}
	peerrunner.PublishSelf(d, bankItems, working)
}

// ForgetPeer drops an account's row, for a caller that knows a session has gone away. A stale
// row is aged out by peerplan.DefaultMaxAge anyway, so this is an optimisation rather than a
// correctness requirement.
func ForgetPeer(ctx *Context, account string) {
	if ctx != nil && ctx.Peers != nil {
		ctx.Peers.Forget(account)
	}
}

// AskPeerOptions is one attempt to source an item from the fleet before falling back to the
// mule. It is peerrunner.AskOptions with the fleet-wide defaults already applied.
type AskPeerOptions struct {
	// Item and Qty are what is needed.
	Item string
	Qty  int
	// MuleName and MuleStand describe the script's existing fallback, so the peer trip is
	// compared against it in the same unit and a peer that does not genuinely beat it is
	// never interrupted. Leave MuleName empty when the mule cannot supply this item at all
	// — which is the live case for an axe — and any reachable peer then wins by default.
	MuleName  string
	MuleStand tile.Tile
	// Travel walks this account to within radius of a tile. Required: pass whatever the
	// calling script already uses (woodcutrunner.TravelTo, freshstartrunner.WalkNear).
	Travel func(dest tile.Tile, radius int, timeout time.Duration) bool
	// Reserve overrides the spare rule; zero-valued uses peerplan.DefaultReserve.
	Reserve peerplan.Reserve
	// Timeout bounds the whole attempt, walk included.
	Timeout time.Duration
}

// AskPeerFirst sources Item from the nearest peer holding a genuine spare.
//
// Every outcome leaves the caller able to do exactly what it would have done without this
// feature, which is the contract that makes it safe to drop into an existing script:
//
//   - Got: it arrived, and the receipt is this account's own pack rising (donorrunner.Await's
//     rule, unchanged) — never a trade closing.
//   - UseMule: no peer is worth interrupting, so run the existing mule path. This is the
//     policy WORKING, not failing: it is what stops the feature ever being slower than what it
//     replaced.
//   - Busy: every holder is already serving somebody. Go back to work and retry after
//     peerplan.BusyRetry. Do NOT queue — queueing is what made the mule a 3.3x bottleneck.
func AskPeerFirst(ctx *Context, o AskPeerOptions) peerrunner.AskResult {
	d := PeerDeps(ctx)
	if d == nil {
		return peerrunner.AskResult{UseMule: true, Reason: "peer sharing is not configured for this fleet"}
	}
	return peerrunner.Ask(d, peerrunner.AskOptions{
		Item: o.Item, Qty: o.Qty,
		Reserve:   o.Reserve,
		MuleName:  o.MuleName,
		MuleStand: o.MuleStand,
		Travel:    o.Travel,
		Timeout:   o.Timeout,
	})
}

// NewPeerWatcher is the per-bot watcher ServePeers needs. Create one in OnStart.
func NewPeerWatcher() *peerrunner.Watcher { return peerrunner.NewWatcher() }

// ServePeers answers at most ONE peer request per call out of this account's own bank, and
// reports whether it spent this pass on one.
//
// At most one, deliberately: a hand-out costs a bank trip and a trade, and the holder's actual
// job is whatever script it is running. This is the same "one per Loop" rule
// internal/bot/mule.go's serveOrder already applies for the same reason.
//
// allowed is the fleet's own account names. The bus carries NO AUTHENTICATION of any kind, so
// an empty list serves nobody — which is the safe default, not an oversight.
//
// bankItems is this account's most recent bank reading, the same slice PublishBank was given.
// The spare check is made against it rather than against the directory row, because the
// directory is explicitly not proof of anything — and peerplan.Spare is recomputed from live
// holdings every time, which is what makes a holder undrainable without any ledger.
func ServePeers(ctx *Context, w *peerrunner.Watcher, allowed []string, bankItems []gamestate.SlotItem) bool {
	d := PeerDeps(ctx)
	if d == nil || w == nil {
		return false
	}
	for _, msg := range w.Poll(d) {
		peerrunner.Serve(d, peerrunner.ServeOptions{
			Allowed: allowed,
			Bank:    func() []gamestate.SlotItem { return bankItems },
			Watcher: w,
		}, msg)
		return true
	}
	return false
}

// PeerCount is how many accounts the directory currently knows about, for a status line.
func PeerCount(ctx *Context) int {
	if ctx == nil || ctx.Peers == nil {
		return 0
	}
	return ctx.Peers.Count()
}

// NewPeerDirectory builds the shared directory a fleet hands to every Context. One per
// process, exactly like mulebus.NewBus.
func NewPeerDirectory() *peerbank.Directory { return peerbank.New() }

// PeerAccounts is every account the directory currently knows about, for ServePeers' allow
// list.
//
// The directory IS the allow list, and that is stronger than a configured one rather than a
// shortcut around it: the only way to appear in it is to have called PublishBank, which needs
// a live Context in THIS process. An account outside the fleet cannot publish, so it cannot be
// served, whatever it puts on the bus. A configured list would additionally have to be kept in
// step with the roster by hand, and the roster changes.
func PeerAccounts(ctx *Context) []string {
	if ctx == nil || ctx.Peers == nil {
		return nil
	}
	entries := ctx.Peers.Entries(time.Now())
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Account)
	}
	return out
}
