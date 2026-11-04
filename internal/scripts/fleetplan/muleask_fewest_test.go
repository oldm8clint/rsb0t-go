package fleetplan

import "testing"

// THE POINT (a user request): the alchables come out of the mule's smallest stacks first, so the
// fleet empties them and frees the mule's bank slots; each takes its whole stack until the kit is
// met, at most FewestFirstItems kinds.
func TestFilterAskTakesTheFewestFirst(t *testing.T) {
	stock := map[string]int{"Yew longbow": 50000, "Magic shortbow": 37, "Yew shortbow": 2000, "Maple longbow": 0}
	of := func(item string) (int, bool) { n, ok := stock[item]; return n, ok }
	line := AskLine{Choices: []string{"Magic shortbow", "Yew longbow", "Yew shortbow", "Maple longbow"}, Count: 1000, FewestFirst: true}
	plan, none := FilterAskByBank([]AskLine{line}, of)
	if len(none) != 0 || len(plan) != 2 {
		t.Fatalf("plan %+v none %v", plan, none)
	}
	if r := plan[0].Rungs[0]; r.Item != "Magic shortbow" || r.Count != 37 {
		t.Fatalf("first %+v", r)
	}
	if r := plan[1].Rungs[0]; r.Item != "Yew shortbow" || r.Count != 963 {
		t.Fatalf("second %+v", r)
	}
	if kit := AskPlanKit(plan); len(kit) != 2 {
		t.Fatalf("kit %+v", kit)
	}
	// Many small stacks: at most FewestFirstItems kinds, even short of the count.
	many := map[string]int{"A": 1, "B": 2, "C": 3, "D": 4, "E": 5}
	plan, _ = FilterAskByBank([]AskLine{{Choices: []string{"E", "D", "C", "B", "A"}, Count: 1000, FewestFirst: true}}, func(item string) (int, bool) { n, ok := many[item]; return n, ok })
	if len(plan) != FewestFirstItems || plan[0].Rungs[0].Item != "A" || plan[3].Rungs[0].Item != "D" {
		t.Fatalf("many %+v", plan)
	}
	// Nothing held: the line is reported as none.
	if plan, none := FilterAskByBank([]AskLine{{Choices: []string{"Maple longbow"}, Count: 10, FewestFirst: true}}, of); len(plan) != 0 || len(none) != 1 {
		t.Fatalf("empty: %+v %v", plan, none)
	}
}
