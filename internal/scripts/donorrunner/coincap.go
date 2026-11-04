package donorrunner

// The mule's coin cap. a user request: "we might come up to the stack limit for coins later, we need to hold off
// handing over coins when the mule holds over 1b coins to ensure we dont ever hit the limit". A coin stack cannot
// pass 2,147,483,647, and the mule held 1.31 billion that day. While the mule's published bank shows MuleCoinCap
// coins or more, every hand-off leaves its coins out (effectiveLoad), so they stay in the donor's own bank; a
// hand-off that was only coins is then not walked to at all (emptyPlanReason).
//
// The fleet installs the reader (SetMuleStock) from its shared bank directory; without one (a standalone tool) the
// cap reads nothing and coins cross as before.

import (
	"strings"
	"sync/atomic"
)

// MuleCoinCap is the mule's coins at which hand-offs stop carrying coins.
const MuleCoinCap = 1_000_000_000

// muleStock reads a mule's published bank: the count of an item and whether the mule's bank is known.
var muleStock atomic.Pointer[func(mule, item string) (int, bool)]

// SetMuleStock installs the reader of the mule's published bank.
func SetMuleStock(f func(mule, item string) (int, bool)) { muleStock.Store(&f) }

// MuleHolds is the mule's published count of an item, and whether its bank is known at all.
func MuleHolds(mule, item string) (int, bool) {
	f := muleStock.Load()
	if f == nil || strings.TrimSpace(mule) == "" {
		return 0, false
	}
	return (*f)(mule, item)
}

// muleCoinsFull reports whether the mule holds MuleCoinCap coins or more, and how many.
func muleCoinsFull(mule, coinsName string) (int, bool) {
	f := muleStock.Load()
	if f == nil || strings.TrimSpace(mule) == "" {
		return 0, false
	}
	if coinsName == "" {
		coinsName = "Coins"
	}
	n, known := (*f)(mule, coinsName)
	return n, known && n >= MuleCoinCap
}
