package twitchplays

import "strings"

// Intent is an interpreted crowd goal. It is deliberately separate from a
// game command: several chat messages can express one goal, and the AI can
// ask for clarification instead of firing every message at the client.
type Intent struct {
	Goal       string
	Confidence float64
	Evidence   []string
}

// GoalFromCommand provides a deterministic starting point. A future LLM
// advisor may produce richer intents, but the controller still validates the
// resulting goal against the same allow-list.
func GoalFromCommand(c Command) (Intent, bool) {
	goal := strings.ToLower(c.Action)
	switch goal {
	case "fish", "walk", "interact", "wait":
		return Intent{Goal: goal, Confidence: 1, Evidence: []string{c.MessageID}}, true
	default:
		return Intent{}, false
	}
}
