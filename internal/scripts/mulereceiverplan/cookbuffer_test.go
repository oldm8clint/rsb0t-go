package mulereceiverplan

import (
	"testing"
	"time"
)

func baseBuffer() CookBufferInput {
	return CookBufferInput{
		Buffer: 2000, Cooked: 300, CookedKnown: true, Raw: 5000,
		Kits: 3, MinKitWait: 9 * time.Minute, SinceLastBufferRound: time.Hour,
	}
}

func TestCookBufferDueWhenUnderBufferWithKitsQueuedAndTimeToSpare(t *testing.T) {
	if !CookBufferDue(baseBuffer()) {
		t.Fatal("a mule 1,700 under its buffer, only kits queued with 9 minutes to wait, did not cook")
	}
}

func TestCookBufferNeverRunsWhenOffOrFullOrNothingToCook(t *testing.T) {
	in := baseBuffer()
	in.Buffer = 0
	if CookBufferDue(in) {
		t.Error("cooked with the buffer off")
	}
	in = baseBuffer()
	in.Cooked = 2000
	if CookBufferDue(in) {
		t.Error("cooked with the buffer full")
	}
	in = baseBuffer()
	in.Raw = 0
	if CookBufferDue(in) {
		t.Error("cooked with no raw fish banked")
	}
	in = baseBuffer()
	in.CookedKnown = false
	if CookBufferDue(in) {
		t.Error("cooked on an unread bank (unknown is not empty)")
	}
}

func TestCookBufferNeverDelaysATimeCriticalTrade(t *testing.T) {
	for name, mut := range map[string]func(*CookBufferInput){
		"trade request": func(i *CookBufferInput) { i.QueuedRequests = 1 },
		"donor coming":  func(i *CookBufferInput) { i.Incoming = 1 },
		"single order":  func(i *CookBufferInput) { i.Orders = 1 },
		"on a trip":     func(i *CookBufferInput) { i.OnTrip = true },
	} {
		in := baseBuffer()
		mut(&in)
		if CookBufferDue(in) {
			t.Errorf("%s: a buffer round started in front of time-critical work", name)
		}
	}
}

func TestCookBufferWaitsForAKitDonorWhoWouldGiveUp(t *testing.T) {
	in := baseBuffer()
	in.MinKitWait = 3 * time.Minute // a ~100 s round and the walk back leave less than the kit needs
	if CookBufferDue(in) {
		t.Error("started a round that a kit donor would not wait out")
	}
}

func TestCookBufferAlternatesWithTheKitQueue(t *testing.T) {
	in := baseBuffer()
	in.SinceLastBufferRound = 20 * time.Second
	if CookBufferDue(in) {
		t.Error("two buffer rounds with no kit between them")
	}
	in.SinceLastBufferRound = CookBufferMinGap
	if !CookBufferDue(in) {
		t.Error("a round was still refused after the gap")
	}
}

func TestCookBufferIsLeftToTheIdleGateWhenNoKitIsQueued(t *testing.T) {
	in := baseBuffer()
	in.Kits = 0
	if CookBufferDue(in) {
		t.Error("the waiver answered for a mule with no kit waiting; the ordinary idle gate owns that")
	}
}

func TestCookThroughputIsAboutAThousandAnHour(t *testing.T) {
	if got := CookThroughputPerHour(); got < 700 || got > 1200 {
		t.Fatalf("throughput %d/h is not the ~1,000/h the mule measured (28 per ~99 s round)", got)
	}
}

// A line of kits is served before any buffer round: one round would delay every kit in it.
func TestCookBufferWaitsForALongKitLine(t *testing.T) {
	in := CookBufferInput{Buffer: 2000, Cooked: 400, CookedKnown: true, Raw: 5000, Kits: 1,
		MinKitWait: 20 * time.Minute, SinceLastBufferRound: time.Hour}
	if !CookBufferDue(in) {
		t.Fatal("one kit with time to spare: a round is due")
	}
	in.Kits = 12
	if CookBufferDue(in) {
		t.Fatal("12 kits queued: serve the line first")
	}
}
