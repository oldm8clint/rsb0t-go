package lootchestfisherplan

import "time"

// HandoffBackoffBase and HandoffBackoffMax bound how long LootChestFisher leaves the mule alone
// after a hand-off that did not complete.
//
// Minutes, not seconds, because of what a retry costs. Exactly one live account runs this script
// but the mule it visits is the fleet's only one: every attempt summons the mule ("on its way here
// by walk") away from the post ~150 ScarabSwarm accounts ask for runes at. Live 2026-09-26
// 06:40-06:43 a 5 s retry on a load that could never cross fired more than ten times in three
// minutes. A donorrunner.Run that returns without trading has ALREADY waited for the mule
// (its own meet and queue timeouts), so a failure is never "the mule was busy for a second".
const (
	HandoffBackoffBase = 2 * time.Minute
	HandoffBackoffMax  = 30 * time.Minute
)

// HandoffBackoff is the hold after the fails-th consecutive failed hand-off: 2, 4, 8, 16, then
// 30 minutes from there on. Zero failures is no hold.
func HandoffBackoff(fails int) time.Duration {
	if fails <= 0 {
		return 0
	}
	w := HandoffBackoffBase
	for i := 1; i < fails && w < HandoffBackoffMax; i++ {
		w *= 2
	}
	if w > HandoffBackoffMax {
		w = HandoffBackoffMax
	}
	return w
}
