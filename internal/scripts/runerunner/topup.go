package runerunner

import (
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runerunnerplan"
)

// TopUpResult is what a mule top-up delivered, by pack delta -- never by a trade closing.
type TopUpResult struct {
	Got      int
	Orders   int
	Declined bool
	Stopped  bool
	Reason   string
}

// AskMuleForCoins asks the mule for `ask` coins over the wall bus with the ordinary item order
// (donorrunner.Await, the path ScarabSwarm's RequestKitFromMule and every fisher already use), in
// orders sized by runerunnerplan.TopUpNextOrder: the mule clamps each to its coinsPerOrder, so a
// short order is followed by one for the rest, and an order that brings nothing ends the ask. A
// whole-stack ask (the mule's entire published coin stack, 11,229,284 on 14 October 2026) is one order per
// million, up to runerunnerplan.TopUpMaxOrders.
// rd is scarabrunner.MuleDeps for this account; the account stands where the mule can see it.
//
// The ask never reads or feeds the fleet's shared refusal memory (CoinAwaitOptions). LIVE 12 October
// 2026, 13 of 28 coin asks (8,146,426 of 17,642,374 coins asked) never reached the mule: another
// account's "none in the bank" was replayed to every account for thirty minutes, while the mule was
// taking in 774,000 coins an hour, and each real decline (the second order of an ask that had just
// emptied the mule) re-armed it for the whole fleet.
func AskMuleForCoins(d *fleetrunner.Deps, rd *donorrunner.Deps, mule, me string, ask int) TopUpResult {
	var res TopUpResult
	last := 0
	memory := donorplan.NewRefusalMemory()
	for {
		want := runerunnerplan.TopUpNextOrder(ask, res.Got, res.Orders, last)
		if want <= 0 {
			return res
		}
		before := coinsInPack(d)
		ans := donorrunner.Await(rd, CoinAwaitOptions(mule, me, want, rd.Bus, memory))
		res.Orders++
		last = coinsInPack(d) - before
		if last < 0 {
			last = 0
		}
		res.Got += last
		if last > 0 {
			// Evidence beats memory: a mule that has just handed coins over is not out of them,
			// whatever another account was told earlier.
			donorrunner.Refusals.Clear(mule, CoinsName)
		}
		logf(d, "runerunner: top-up order %d asked %s for %d coins, got %d (%s); %d of the %d asked so far", res.Orders, mule, want, last, ans.Reason, res.Got, ask)
		if ans.Stopped {
			res.Stopped = true
			return res
		}
		if last <= 0 {
			res.Declined = ans.Declined
			res.Reason = ans.Reason
			return res
		}
	}
}

// CoinAwaitOptions is one coin order's ask: it asks whatever the fleet's remembered refusals say
// (AskAnyway: a runner that needs coins to buy runes knows better than another account's earlier
// "none in the bank") and keeps what the mule answers in `memory`, a memory of its own, so a decline
// here does not shut every other coin asker out for thirty minutes. memory must not be nil and must
// not be donorrunner.Refusals.
func CoinAwaitOptions(mule, me string, want int, bus *mulebus.Bus, memory *donorplan.RefusalMemory) donorrunner.AwaitOptions {
	return donorrunner.AwaitOptions{
		Mule: mule, Me: me, Item: CoinsName, Qty: want, Bus: bus, Timeout: runerunnerplan.TopUpAskTimeout,
		AskAnyway: true, Memory: memory,
	}
}

// coinsInPack counts every pack stack named Coins ("Coins" names two object ids in this config).
func coinsInPack(d *fleetrunner.Deps) int {
	return PackCounts(d, []string{CoinsName})[CoinsName]
}
