package bot

// ProgressiveWoodcutter's use of fleet-wide peer item sharing (see peershare.go).
//
// Kept in its own file for the reason that file states: several sessions work in internal/bot
// at once, and woodcutter.go is large and frequently edited. Nothing here holds state on
// woodcutterBot beyond the one watcher field.
//
// WHY THE WOODCUTTER IS THE FIRST CALLER. Its axe path is the exact shape the feature exists
// for: 94 accounts that all bank at Seers, a mule posted 83 tiles away at Catherby that takes
// one trade partner at a time, and a dozen of those accounts sitting on superseded bronze and
// mithril axes they will never use again. A peer holding a spare axe is a few tiles away; the
// mule is 83 tiles and a queue measured at a 3.3x demand shortfall.
//
// The arithmetic is worth stating honestly rather than letting the feature sound bigger than it
// is: twelve banked spare axes satisfy twelve accounts, not ninety-four. The rest still need
// the coins-and-market purchase path in woodcutteraxe.go. What this buys is twelve accounts off
// that queue entirely, and each of them an 83-tile round trip saved.

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/peerplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// peerAskTimeout bounds one peer attempt, walk included. Generous next to the 83-tile walk it
// is trying to replace, and short enough that a silent peer costs a pass rather than a shift.
const peerAskTimeout = 90 * time.Second

// askPeerForAnAxe tries to source an axe from a nearer account before the mule is walked to.
//
// It returns true when the caller should stop for this pass: either an axe arrived, or every
// holder is busy and the right move is to go back to chopping and retry later. False means
// "carry on exactly as you would have" — no peer is worth interrupting, so the existing mule
// and market paths run untouched. That contract is what makes this safe to drop in.
func (b *woodcutterBot) askPeerForAnAxe(ctx *Context, d *woodcutrunner.Deps) bool {
	if PeerDeps(ctx) == nil {
		return false
	}
	want := bestAxeAnyPeerBanks(ctx, ctx.Username)
	if want == "" {
		return false
	}
	res := AskPeerFirst(ctx, AskPeerOptions{
		Item: want,
		Qty:  1,
		// MuleName is deliberately EMPTY. The mule's catalogue stops at a mithril axe and it
		// cannot supply the rune axe this path wants at all, so there is no mule trip to
		// compare against and any reachable peer wins. peershare.go documents this case.
		Travel: func(dest tile.Tile, radius int, timeout time.Duration) bool {
			return woodcutrunner.TravelTo(d, dest, radius)
		},
		Timeout: peerAskTimeout,
	})
	switch {
	case res.Got:
		ctx.logf("woodcutter: a fleet account handed me a %s — no trip to %q needed", want, b.settings.Mule)
		return true
	case res.Busy:
		ctx.logf("woodcutter: every account holding a spare axe is already serving somebody — chopping on and asking again later")
		ctx.Delay(3 * time.Second)
		return true
	default:
		return false
	}
}

// bestAxeAnyPeerBanks is the best axe the directory says some other account is holding in its
// bank, or "" when nobody is.
//
// Asking the directory first, rather than naming an axe and hoping, is what turns this into ONE
// bus round trip instead of seven. It also asks for the right thing: what peers actually hold
// spare is the axe they have OUTGROWN -- a superseded bronze or mithril, banked and never used
// again -- so asking for the rune axe the purchase path wants would match almost nobody.
//
// The directory is explicitly not proof of anything (see internal/peerbank's package doc): the
// holder recomputes what is genuinely spare from its own live bank when it is asked, and can
// refuse. This only decides what is worth asking for.
func bestAxeAnyPeerBanks(ctx *Context, me string) string {
	if ctx == nil || ctx.Peers == nil {
		return ""
	}
	var names []string
	for _, e := range ctx.Peers.Entries(time.Now()) {
		if strings.EqualFold(e.Account, me) {
			continue
		}
		// NEVER ASK A PEER ON ANOTHER WORLD. A hand-over is a TRADE, so the two accounts have
		// to stand together, and accounts on different worlds cannot see each other however
		// close their tiles read -- the directory stores a tile, and the same tile on two
		// worlds is two different players.
		//
		// Without this the fleet's own world split would send an account walking to a peer it
		// can never meet, and the peer would decline with "you are not in sight of me" after
		// the trip. ctx.WorldOf reports 0 for an account the fleet does not know, which is
		// left alone rather than excluded: a single-world fleet has no world information at
		// all and must behave exactly as it did.
		if mine, theirs := ctx.World(), ctx.WorldOf(e.Account); mine > 0 && theirs > 0 && mine != theirs {
			continue
		}
		for _, h := range e.Bank {
			if h.Count > 0 {
				names = append(names, h.Item)
			}
		}
	}
	return woodcutplan.BestAxe(names)
}

// muleBankCount answers "how many of this item is the mule known to hold, and do we know at
// all?" from the shared bank directory, or nil when there is nothing to go on.
//
// THE FLEET OWNER'S REPORT, 2026-09-22: "woodcutters are asking the mule for axes they dont even
// have instock ... they should beable to know whats in their bank to request". This is the
// reading that answers it. The mule publishes its own bank at every moment mulerunner already
// has it open (internal/bot/mule.go's Deps.OnBankRead), so this costs one map read.
//
// THE RETURNED CLOSURE ANSWERS ok=true FOR EVERYTHING, INCLUDING ZERO, and that is the whole
// point rather than a shortcut: a bank publication is a COMPLETE reading, so an item missing
// from the row is genuinely absent from the bank, not unknown. woodcutplan.MuleAxeLadder relies
// on exactly that distinction -- an unknown rung is kept, a known-empty one is dropped.
//
// nil is returned, and must be, in three cases, each of which leaves the caller behaving
// exactly as it did before this existed:
//   - the fleet has no directory at all (no peer sharing configured);
//   - no mule is named;
//   - the mule has never published, or its row is older than peerplan.DefaultMaxAge. A stale
//     bank reading is a guess, and "the mule has no axe" is too consequential a conclusion to
//     draw from a guess -- it stops the account walking there at all. Ten minutes is the same
//     staleness bound peerplan.Reserve already applies before it will walk an account to a
//     peer on the strength of a row.
func muleBankCount(ctx *Context, mule string) func(item string) (int, bool) {
	if ctx == nil || ctx.Peers == nil {
		return nil
	}
	mule = strings.ToLower(strings.TrimSpace(mule))
	if mule == "" {
		return nil
	}
	now := time.Now()
	for _, e := range ctx.Peers.Entries(now) {
		if strings.ToLower(strings.TrimSpace(e.Account)) != mule {
			continue
		}
		if e.UpdatedAt.IsZero() || now.Sub(e.UpdatedAt) > peerplan.DefaultMaxAge {
			return nil
		}
		counts := make(map[string]int, len(e.Bank))
		for _, h := range e.Bank {
			if h.Count > 0 {
				counts[strings.ToLower(strings.TrimSpace(h.Item))] += h.Count
			}
		}
		return func(item string) (int, bool) {
			return counts[strings.ToLower(strings.TrimSpace(item))], true
		}
	}
	return nil
}

// publishBankToPeers records this account's bank for the fleet, from a bank that is already
// open. One map write; it reads nothing extra and touches no lock /api/status uses.
func (b *woodcutterBot) publishBankToPeers(ctx *Context, bank *actions.Bank) {
	if bank == nil {
		return
	}
	PublishBank(ctx, bank.Items(), false)
}

// servePeersOnce answers at most one peer request per Loop, out of this account's own bank.
// allowed is the fleet's own account names; the bus carries no authentication, so an empty list
// serves nobody, which is the safe default.
func (b *woodcutterBot) servePeersOnce(ctx *Context) {
	if b.peerWatch == nil {
		return
	}
	ServePeers(ctx, b.peerWatch, PeerAccounts(ctx), b.lastBankItems)
}
