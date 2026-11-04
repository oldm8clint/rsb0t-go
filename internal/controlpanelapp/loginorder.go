package controlpanelapp

import (
	"log"

	"rsb0t-go/internal/controlpanel"
	"rsb0t-go/internal/fleet"
)

// usernames is the login names of accounts, in order.
func usernames(accounts []accountConfig) []string {
	out := make([]string, len(accounts))
	for i, a := range accounts {
		out[i] = a.Username
	}
	return out
}

// loginOrder is the order the start-of-day loop launches the accounts in: the mule, the main account and the RuneCarriers first (fleet.LoginRank, CR-4
// of docs/xp_rate_plan_2026-10-19.md), everyone else in roster order. It is the accounts the process will start, by the script each
// will run (a saved assignment wins over the roster, as startupScript says), and it launches the same accounts at the same pace: only
// who comes first changes, so the login limiter and the route pools see exactly the logins they always did. The roster slice is not
// reordered.
func loginOrder(accounts []accountConfig, store *controlpanel.ScriptAssignments) []accountConfig {
	scripts := make([]string, len(accounts))
	for i, a := range accounts {
		scripts[i], _, _ = startupScript(a, store, scriptRegistered)
	}
	rank := func(i int) int { return fleet.LoginRank(scripts[i]) }
	out := make([]accountConfig, 0, len(accounts))
	for _, i := range fleet.OrderForLogin(len(accounts), rank) {
		out = append(out, accounts[i])
	}
	if n := fleet.PriorityLogins(len(accounts), rank); n > 0 {
		counts := map[string]int{}
		for _, s := range scripts {
			if fleet.LoginRank(s) < fleet.LoginRank("") {
				counts[s]++
			}
		}
		log.Printf("controlpanel: login order: %d priority account(s) first (%d %s, %d %s, %d %s), then the other %d in roster order",
			n, counts[fleet.ScriptMule], fleet.ScriptMule, counts[fleet.ScriptCrafter], fleet.ScriptCrafter, counts[fleet.ScriptCarrier], fleet.ScriptCarrier, len(accounts)-n)
	}
	return out
}
