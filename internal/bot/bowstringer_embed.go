package bot

// Running BowStringer as one round of another script (MasterCrafter's bow round,
// mastercrafter_rounds.go), without a second copy of its bank-and-kit loop.
//
// The registered "BowStringer" run owns its whole Context: it decodes ctx.Settings strictly, and
// when it is done -- no bow left, a kit the mule would not fill -- it ends the RUN (RequestFinish and
// a stop, so the fleet does not log the account back in). Neither is right for a host that goes back
// to its own work afterwards. An embed changes exactly those things, and one more: the mule's
// published bank counts bows by NAME, so strung and unstrung ones are one number; a host that hands
// the strung bows back to the mule hands in an estimate of the unstrung ones instead. With embed nil
// every path is the registered script's, unchanged.

import (
	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
)

// bowStringerEmbed is set on a bowStringerBot that another bot runs.
type bowStringerEmbed struct {
	// set replaces the decoded ctx.Settings.
	set bowstringplan.Settings
	// stock, when set, turns the mule's published bank reading into the one the kit planner reads.
	stock func(base func(item string) (int, bool)) func(item string) (int, bool)
	// ended is called once when the round is over, with the reason: where the registered script
	// stops the run (stop) or waits to ask again (backoff).
	ended  func(reason string)
	done   bool
	reason string
	// fast, when set, is offered each pass before BowStringer's own step, with the bow and its ids:
	// it reports whether it did the pass's work (the host's four-tick stringing runs,
	// mastercrafter_bows.go). False lets the registered script's bank step, stringing and kit order run
	// as ever.
	fast func(ctx *Context, d *fleetrunner.Deps, bow bowstringplan.Bow, ids bowstringrunner.IDs) bool
	// bankFails and useFails count the bank visits that failed and the stringing actions that could
	// not be sent, in a row. The registered script retries both for ever (the account stands at the
	// bank with nothing else to do); a round of a host that stopped its swarm for it must not.
	bankFails, useFails int
}

// embedFailLimit is how many of the same failure in a row end an embedded round as a passing
// failure ("deferred:"): the host backs the mode off rather than the round sitting at the bank.
const embedFailLimit = 3

// fail counts one failure (*n) and ends the round once embedFailLimit have come in a row. It reports
// whether the round ended.
func (e *bowStringerEmbed) fail(ctx *Context, n *int, why string) bool {
	*n++
	if *n < embedFailLimit {
		return false
	}
	ctx.logf("bowstringer: %s %d times in a row — ending the round", why, *n)
	e.finish("deferred: " + why)
	return true
}

// finish ends the round, once.
func (e *bowStringerEmbed) finish(reason string) {
	if e.done {
		return
	}
	e.done, e.reason = true, reason
	if e.ended != nil {
		e.ended(reason)
	}
}

// embedEnded reports whether an embedded round has handed control back.
func (b *bowStringerBot) embedEnded() bool { return b.embed != nil && b.embed.done }

// settingsFor is the script's settings: the host's, or the strict decode of ctx.Settings.
func (b *bowStringerBot) settingsFor(ctx *Context) (bowstringplan.Settings, error) {
	if b.embed != nil {
		return b.embed.set, b.embed.set.Validate()
	}
	return bowstringplan.DecodeSettings(ctx.Settings)
}

// bowIDsOf resolves a bow name to its unstrung and strung obj ids through the context's object config
// (bowstringrunner.ResolveIDs), for the readers of the mule's per-obj rows: nil with no config.
func bowIDsOf(ctx *Context) bowstringplan.BowIDs {
	if ctx == nil || ctx.Config == nil {
		return nil
	}
	cfg := ctx.Config
	return func(bow string) (int, int, bool) {
		ids, err := bowstringrunner.ResolveIDs(cfg, bow)
		if err != nil {
			return 0, 0, false
		}
		return int(ids.Unstrung), int(ids.Strung), true
	}
}

// muleStock is the mule's published bank as the kit planner should read it: nil when it has not
// published, else the host's adjustment of it when it gave one.
func (b *bowStringerBot) muleStock(ctx *Context, mule string) func(item string) (int, bool) {
	base := muleBankCount(ctx, mule)
	if base == nil || b.embed == nil || b.embed.stock == nil {
		return base
	}
	return b.embed.stock(base)
}
