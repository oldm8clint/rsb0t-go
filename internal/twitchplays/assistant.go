package twitchplays

import (
	"context"
	"fmt"
)

// ChatAssistant is intentionally provider-neutral. Implementations may use a
// local model, a hosted model, or a deterministic mock for tests.
type ChatAssistant interface {
	Interpret(ctx context.Context, messages []Command, currentGoal string) (AssistantReply, error)
}

type AssistantReply struct {
	Message string
	Goal    Intent
	Ask     bool
}

// BuildAssistantContext bounds the amount of chat sent to an AI provider.
// Callers should pass only normalized, non-sensitive messages.
func BuildAssistantContext(messages []Command, max int) []Command {
	if max < 1 {
		return nil
	}
	if len(messages) > max {
		messages = messages[len(messages)-max:]
	}
	out := make([]Command, len(messages))
	copy(out, messages)
	return out
}

// MockAssistant gives the offline replay lab a useful assistant without any
// network dependency. It groups repeated goals and asks for clarification on
// conflicting goals instead of generating game actions itself.
type MockAssistant struct{}

func (MockAssistant) Interpret(_ context.Context, messages []Command, currentGoal string) (AssistantReply, error) {
	counts := map[string]int{}
	for _, m := range messages {
		if i, ok := GoalFromCommand(m); ok {
			counts[i.Goal]++
		}
	}
	best, n := "", 0
	for goal, count := range counts {
		if count > n {
			best, n = goal, count
		}
	}
	if best == "" {
		return AssistantReply{Message: "I don't understand the current goal yet. Try !fish, !walk, !interact or !wait.", Ask: true}, nil
	}
	if currentGoal != "" && currentGoal != best && n == counts[currentGoal] {
		return AssistantReply{Message: fmt.Sprintf("I'm seeing two competing goals (%s and %s). Which one should we focus on?", currentGoal, best), Ask: true}, nil
	}
	return AssistantReply{Message: fmt.Sprintf("I think the crowd wants us to focus on %s. I'll group commands around that goal rather than firing every message immediately.", best), Goal: Intent{Goal: best, Confidence: 0.5}, Ask: false}, nil
}
