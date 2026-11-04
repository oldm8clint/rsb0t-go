package fleetplan

import (
	"testing"
)

var bassLine = []AskLine{{Choices: []string{"Bass"}, Count: 1000, FairShare: true, Bulk: true}}

func rungOf(t *testing.T, plan []AskPlan) ItemCount {
	t.Helper()
	if len(plan) != 1 || len(plan[0].Rungs) != 1 {
		t.Fatalf("plan = %+v, want one line with one rung", plan)
	}
	return plan[0].Rungs[0]
}
