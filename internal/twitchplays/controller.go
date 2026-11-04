package twitchplays

import "context"

// ActionSink is the only game-facing boundary CrowdPlay needs. Implementations
// should delegate to existing high-level bot/actions APIs rather than packets.
type ActionSink interface {
	ExecuteCrowdCommand(ctx context.Context, command Command) error
	Stop(ctx context.Context) error
}

// Controller serializes approved crowd commands for one target session.
type Controller struct {
	queue *Queue
	sink  ActionSink
}

func NewController(queue *Queue, sink ActionSink) *Controller {
	return &Controller{queue: queue, sink: sink}
}

// Step executes at most one command. Keeping one-command-per-step makes replay,
// cancellation and future telemetry deterministic.
func (c *Controller) Step(ctx context.Context) error {
	cmd, ok := c.queue.Pop()
	if !ok {
		return nil
	}
	if Normalize(cmd.Action) == "stop" {
		return c.sink.Stop(ctx)
	}
	return c.sink.ExecuteCrowdCommand(ctx, cmd)
}
