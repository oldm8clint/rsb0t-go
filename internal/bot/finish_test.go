package bot

import (
	"errors"
	"testing"
)

// finishingBot declares itself finished on its first Loop, the way a script that has reached
// its goal and logged out would.
type finishingBot struct {
	reason string
	loops  int
}

func (b *finishingBot) OnStart(ctx *Context) error { return nil }

func (b *finishingBot) Loop(ctx *Context) error {
	b.loops++
	ctx.RequestFinish(b.reason)
	ctx.requestStop()
	return nil
}

func (b *finishingBot) OnStop(ctx *Context) {}

func TestRequestFinishIsVisibleAndDoesNotClear(t *testing.T) {
	ctx := NewContext(nil, "acct", nil, nil, nil, nil, nil, nil, nil, nil, nil)

	if _, finished := ctx.FinishRequested(); finished {
		t.Fatal("a fresh context must not read as finished")
	}

	ctx.RequestFinish("reached 43 Prayer")

	// Unlike a hand-off request, this must NOT clear on read: the read loop, the reconnect
	// loop and any dashboard all read it independently, and a one-shot flag would let the
	// account be reconnected by whichever of them read second.
	for i := 0; i < 3; i++ {
		reason, finished := ctx.FinishRequested()
		if !finished || reason != "reached 43 Prayer" {
			t.Fatalf("read %d: got %q, %v; want the reason to persist", i, reason, finished)
		}
	}
}

// Stopping a run is not finishing it: an ordinary Stop must leave the account reconnectable.
func TestStoppingIsNotFinishing(t *testing.T) {
	ctx := NewContext(nil, "acct", nil, nil, nil, nil, nil, nil, nil, nil, nil)
	ctx.requestStop()
	if !ctx.Stopped() {
		t.Fatal("requestStop must stop the run")
	}
	if _, finished := ctx.FinishRequested(); finished {
		t.Fatal("a stopped run must not read as finished for good — that is what would keep it logged out")
	}
}

func TestRunnerReportsFinishRequested(t *testing.T) {
	b := &finishingBot{reason: "goal reached"}
	ctx := NewContext(nil, "acct", nil, nil, nil, nil, nil, nil, nil, nil, nil)
	r := NewRunner("Finisher", b, ctx)

	if _, finished := r.FinishRequested(); finished {
		t.Fatal("must not read as finished before the bot runs")
	}

	r.Start()
	r.Wait()

	reason, finished := r.FinishRequested()
	if !finished || reason != "goal reached" {
		t.Fatalf("runner reported %q, %v; want the bot's own reason", reason, finished)
	}
	if got := r.Status().State; got != StateStopped {
		t.Fatalf("state = %q, want stopped", got)
	}
}

// A bot that fails must not be mistaken for one that finished on purpose.
type failingBot struct{}

func (failingBot) OnStart(ctx *Context) error { return nil }
func (failingBot) Loop(ctx *Context) error    { return errors.New("boom") }
func (failingBot) OnStop(ctx *Context)        {}

func TestAFailedRunIsNotFinished(t *testing.T) {
	ctx := NewContext(nil, "acct", nil, nil, nil, nil, nil, nil, nil, nil, nil)
	r := NewRunner("Failer", failingBot{}, ctx)
	r.Start()
	r.Wait()

	if r.Status().State != StateError {
		t.Fatalf("state = %q, want error", r.Status().State)
	}
	if _, finished := r.FinishRequested(); finished {
		t.Fatal("a crashed script must stay reconnectable, not be treated as finished")
	}
}
