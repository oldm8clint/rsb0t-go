package donorrunner

import (
	"testing"

	"rsb0t-go/internal/mulebus"
)

// Owner 2026-09-19: donors tell their mule when they want to hand off, so the mule can cook
// without idle waits the rest of the time.
func TestHandoffIsAnnouncedToTheDonorsOwnMuleAndClosedAfter(t *testing.T) {
	bus := mulebus.NewBus()
	w := mulebus.NewWatcher()
	done := announceHandoff(&Deps{Bus: bus, Me: "acct6"}, Options{Mule: "mule", Drawn: 12})
	msgs := w.Poll(bus)
	if len(msgs) != 1 || msgs[0].Kind != mulebus.KindReady || msgs[0].To != "mule" || msgs[0].Need == nil || *msgs[0].Need != 12 {
		t.Fatalf("start of hand-off: %+v", msgs)
	}
	done()
	// The watcher is time-based, so a same-instant Ready may come back again; the latest message
	// is what the mule acts on last.
	msgs = w.Poll(bus)
	if len(msgs) == 0 || msgs[len(msgs)-1].Kind != mulebus.KindCancel || msgs[len(msgs)-1].To != "mule" {
		t.Fatalf("end of hand-off: %+v", msgs)
	}
}

func TestNoAnnouncementWithoutABus(t *testing.T) {
	announceHandoff(&Deps{Me: "acct6"}, Options{Mule: "mule"})()
}
