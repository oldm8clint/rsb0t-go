package donorplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// A donor waiting for a mule that is travelling to it. Without the mule's own promise every one
// of these gives up at MeetTimeout, because both existing timeouts run from the moment the load
// was drawn and a mule 84 tiles away cannot possibly be visible yet.
func meetingView(now time.Time, stand tile.Tile) MeetView {
	me := stand
	return MeetView{Now: now, Me: &me, Stand: stand}
}

// arrivedAtStart says the donor has been standing on the meet spot since the hand-off began.
//
// It exists because the meet clock moved on 2026-09-23. It used to run from Handover.Since --
// the moment the load was DRAWN -- which charged the donor's patience for the walk, so a
// Seers-to-Catherby hand-off timed out while the account was still 43-69 tiles short of the
// stand, 46 times in one run. It now runs from Handover.ArrivedAt. These tests model a donor
// already AT the stand, so they have to say so; without it they would be asserting the old,
// broken basis and passing for the wrong reason.
func arrivedAtStart(h *Handover, start time.Time) { h.ArrivedAt = start }

func TestMeetWaitsOutAMuleThatSaidItIsComing(t *testing.T) {
	stand := tile.Tile{X: 2809, Z: 3441}
	start := ms(0)
	h := Begin(start)
	h.Loaded(1, 1, start)
	arrivedAtStart(&h, start)

	past := start.Add(MeetTimeout + time.Second)
	if got := h.Meet(meetingView(past, stand)); got != MeetGiveUp {
		t.Fatalf("with no promise the unchanged timeout must still apply, got %s", got)
	}

	// The same moment, with the mule's published ETA still ahead.
	until := start.Add(3 * time.Minute)
	v := meetingView(past, stand)
	v.WaitUntil = &until
	if got := h.Meet(v); got != MeetWait {
		t.Errorf("a live promise should keep the donor waiting, got %s", got)
	}

	// The promised moment itself is not the end of it: the mule's estimate carries a grace,
	// because the estimate is a queue guess that runs late far more often than early.
	v = meetingView(until.Add(time.Second), stand)
	v.WaitUntil = &until
	if got := h.Meet(v); got != MeetWait {
		t.Errorf("a promise a second overdue must still be believed, got %s", got)
	}

	// Past the grace, the ordinary timeout decides again.
	v = meetingView(until.Add(ComingGrace+time.Second), stand)
	v.WaitUntil = &until
	if got := h.Meet(v); got != MeetGiveUp {
		t.Errorf("a lapsed promise must not keep the donor waiting, got %s", got)
	}
}

// The three give-ups measured live on 2026-09-23 (build 263ccedad), replayed exactly. Each donor
// was standing ONE TILE from the post, had been told an ETA, and walked away carrying its load
// about thirty seconds after the mule missed it. Every one of those loads is a future deadlock:
// see acct150, 26 of 28 slots held by a hand-off it never delivered, across four restarts.
func TestADonorDoesNotWalkOffOverAMuleRunningALittleLate(t *testing.T) {
	stand := tile.Tile{X: 2809, Z: 3441}
	for _, c := range []struct {
		who      string
		promised time.Duration
		gaveUpAt time.Duration
	}{
		{"acct165", 30 * time.Second, 85 * time.Second},
		{"acct139", 90 * time.Second, 121 * time.Second},
		{"Acct171", 120 * time.Second, 150 * time.Second},
	} {
		start := ms(0)
		h := Begin(start)
		h.Loaded(1, 1, start)
		arrivedAtStart(&h, start)

		until := start.Add(c.promised)
		v := meetingView(start.Add(c.gaveUpAt), stand)
		v.WaitUntil = &until
		if got := h.Meet(v); got != MeetWait {
			t.Errorf("%s: told %s and %s in, it must still be waiting, got %s",
				c.who, c.promised, c.gaveUpAt, got)
		}
	}
}

func TestAPromiseCannotOutliveMeetTimeoutComing(t *testing.T) {
	stand := tile.Tile{X: 2809, Z: 3441}
	start := ms(0)
	h := Begin(start)
	h.Loaded(1, 1, start)
	arrivedAtStart(&h, start)

	// A mule promising an hour away is not believed past the ceiling.
	until := start.Add(time.Hour)
	v := meetingView(start.Add(MeetTimeoutComing-time.Second), stand)
	v.WaitUntil = &until
	if got := h.Meet(v); got != MeetWait {
		t.Errorf("inside the ceiling the promise holds, got %s", got)
	}
	v = meetingView(start.Add(MeetTimeoutComing+time.Second), stand)
	v.WaitUntil = &until
	if got := h.Meet(v); got != MeetGiveUp {
		t.Errorf("past the ceiling a promise must stop counting, got %s", got)
	}
}

func TestMeetGivesUpAtOnceWhenTheMuleDeclined(t *testing.T) {
	stand := tile.Tile{X: 2809, Z: 3441}
	start := ms(0)
	h := Begin(start)
	h.Loaded(1, 1, start)

	v := meetingView(start.Add(time.Second), stand)
	v.Declined = true
	if got := h.Meet(v); got != MeetGiveUp {
		t.Errorf("a declined hand-off should end immediately rather than standing for the timeout, got %s", got)
	}
	// A declined message never overrides a trade that is already open.
	v.TradeOpen = true
	if got := h.Meet(v); got != MeetTrading {
		t.Errorf("an open trade screen still wins, got %s", got)
	}
}

func TestComingDoesNotChangeAnyOtherMeetDecision(t *testing.T) {
	// The promise only ever suppresses a give-up; everything else decides exactly as before.
	stand := tile.Tile{X: 2809, Z: 3441}
	start := ms(0)
	h := Begin(start)
	h.Loaded(1, 1, start)
	until := start.Add(3 * time.Minute)

	far := tile.Tile{X: 2860, Z: 3441}
	v := MeetView{Now: start, Me: &far, Stand: stand, WaitUntil: &until}
	if got := h.Meet(v); got != MeetWalk {
		t.Errorf("still walks to the stand, got %s", got)
	}
	beside := tile.Tile{X: 2809, Z: 3442}
	mule := tile.Tile{X: 2809, Z: 3441}
	v = MeetView{Now: start.Add(time.Second), Me: &beside, Mule: &mule, Stand: stand, WaitUntil: &until}
	if got := h.Meet(v); got != MeetRequest {
		t.Errorf("still requests once the mule is beside it, got %s", got)
	}
}
