package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/mulerunner"
)

// A donor's NEW hand-off announcement clears the "just served" mark its last trade left, so the
// mule may ask it first again (mulerunner.RequestWatcher.Announced, called from pollBus). A repeat
// of an announcement already queued changes nothing. CONTROL: without the announcement the mark
// stays, which is what keeps the mule from asking a donor that has only just finished.
func TestANewAnnouncementLetsTheMuleAskTheDonorFirstAgain(t *testing.T) {
	ctx := orderCtx()
	b := &muleBot{
		wallBus:    true,
		busWatcher: mulebus.NewWatcher(),
		watcher:    mulerunner.NewRequestWatcher(),
		incoming:   map[string]time.Time{},
		allowed:    mulerunner.AllowList([]string{"donorb"}),
	}
	b.watcher.NoteServed("Donorb")
	b.pollBus(ctx)
	if b.watcher.MissedFirstRequests("donorb") == 0 {
		t.Fatal("control: with no announcement the post-trade mark must stay")
	}
	ctx.Bus.Publish(mulebus.ReadyMessage("donorb", -1))
	b.pollBus(ctx)
	if got := b.watcher.MissedFirstRequests("donorb"); got != 0 {
		t.Fatalf("after a new announcement the donor's misses are %d, want 0", got)
	}
	// A repeat announcement while still queued does not clear a mark set since.
	b.watcher.NoteServed("Donorb")
	ctx.Bus.Publish(mulebus.ReadyMessage("donorb", -1))
	b.pollBus(ctx)
	if b.watcher.MissedFirstRequests("donorb") == 0 {
		t.Fatal("a repeat of an announcement already queued cleared the mark")
	}
}
