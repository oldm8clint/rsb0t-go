package fleet

import "sort"

// THE START-OF-DAY LOGIN ORDER (CR-4 of docs/xp_rate_plan_2026-10-19.md, 19 Oct 2026).
//
// A fleet start launches its accounts in roster order, one every few hundred milliseconds, and each launch waits for a slot on its
// route and a token of that route's login gate (routes.go, loginrate.go). Nobody in a 360-account roster is first by merit: the main account was
// login 217 of 360 at the 12:14 start on 19 Oct and the RuneCarriers came 40 seconds to 8 minutes in, so the Nature gate, which is the
// single biggest xp source of any one account (41-44k Runecrafting experience an hour), did nothing for the first 3.1 minutes on
// average and ran at 2,500 essence an hour for the first quarter hour against 4,500 and more once it was full (0-15 minutes 2,500,
// 15-30 3,650, 30-60 3,950). The mule hands every carrier its kit and takes every coin and rune the swarm earns, so it is wanted
// before any of them.
//
// This file only says who goes first. It adds no login and no wait: the same accounts launch at the same pace through the same gates
// (the fleet-wide limit near 29 a minute, response 16, and the per-route pools and pins are exactly what they were); the priority
// accounts are simply the first ones launched, so they are first in every queue they stand in.

// The scripts whose accounts log in first, in this order.
const (
	// ScriptMule is the mule: the carriers' kits, the swarm's coins and runes.
	ScriptMule = "MuleReceiver"
	// ScriptCrafter is the main account, who stands at the Nature gate.
	ScriptCrafter = "MasterCrafter"
	// ScriptCarrier is a RuneCarrier, who brings the crafter his essence.
	ScriptCarrier = "RuneCarrier"
)

// LoginRank ranks the script an account will run for the start-of-day login order: 0 logs in first. Every script that is not named
// above, and an account that runs none, shares the last rank and keeps its roster order.
func LoginRank(script string) int {
	switch script {
	case ScriptMule:
		return 0
	case ScriptCrafter:
		return 1
	case ScriptCarrier:
		return 2
	}
	return 3
}

// OrderForLogin is the launch order of n accounts as the indexes 0..n-1: ascending rank, and within a rank the order they came in.
// It is a permutation of 0..n-1, always: no account is dropped or repeated, so a roster that names nothing in particular launches in
// exactly the order it was written in.
func OrderForLogin(n int, rank func(i int) int) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	if rank == nil {
		return order
	}
	sort.SliceStable(order, func(a, b int) bool { return rank(order[a]) < rank(order[b]) })
	return order
}

// PriorityLogins is how many of the n accounts rank ahead of the rest (LoginRank below the last), for the start-up line.
func PriorityLogins(n int, rank func(i int) int) int {
	last := LoginRank("")
	c := 0
	for i := 0; i < n; i++ {
		if rank(i) < last {
			c++
		}
	}
	return c
}
