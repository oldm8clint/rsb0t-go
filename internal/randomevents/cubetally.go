package randomevents

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
)

const (
	// cubeModalWait bounds each wait for the cube modal to open after Open, or to close
	// after an answer. It is a ceiling, not a fixed sleep: each wait ends as soon as the
	// modal changes, and it covers a slow production tick plus a proxied round trip.
	cubeModalWait = 4 * time.Second
	// cubeDecodeSettle is how long decode may wait for the new round's question and model
	// packets once the modal is open, before the modal is closed and reopened.
	cubeDecodeSettle = 1500 * time.Millisecond
)

func cubeModalOpen(s actions.Session) bool {
	main, _, _ := s.State().Modals()
	return main == cubeModalID
}

// cubeTally records what one solveStrangeBox call did, so a live account that keeps its
// boxes says why in a single line: how many rounds decoded, which question and shapes an
// undecoded round showed, and what the server answered.
type cubeTally struct {
	start                      time.Time
	boxesBefore                int
	rounds, decoded, undecoded int
	firstUndecoded             string
}

func newCubeTally(s actions.Session, ev *Event) *cubeTally {
	return &cubeTally{start: time.Now(), boxesBefore: cubeBoxCount(s, ev)}
}

func cubeBoxCount(s actions.Session, ev *Event) int {
	n := 0
	for _, it := range s.State().KnownInventories()[ev.InvComID] {
		if it.ID == ev.ItemID {
			n += int(it.Count)
		}
	}
	return n
}

func (t *cubeTally) noteUndecoded(s actions.Session) {
	if t.firstUndecoded != "" {
		return
	}
	question, _ := s.State().TextKnown(cubeQuestionID)
	var objs []string
	for _, com := range []int{cubeCenterModelID, cubeSideModelID, cubeTopModelID} {
		if id, ok := s.State().InterfaceObject(com); ok {
			objs = append(objs, fmt.Sprint(id))
		} else {
			objs = append(objs, "none")
		}
	}
	t.firstUndecoded = fmt.Sprintf("question %q objects %s", question, strings.Join(objs, ","))
}

func (t *cubeTally) report(s actions.Session, ev *Event, log func(string)) {
	if log == nil || t.rounds == 0 {
		return
	}
	outcome := "no chat log"
	if chat := chatFor(s); chat != nil {
		seen := chat.Since(t.start)
		// The last box's right answer grants the reward with no message, so "right"
		// undercounts by one when the pack empties.
		outcome = fmt.Sprintf("%d right, %d wrong", strings.Count(seen, "You have completed this cube"),
			strings.Count(seen, "you have answered the question incorrectly"))
	}
	line := fmt.Sprintf("cube solve: %d rounds, %d decoded, %d undecoded, %s, boxes %d -> %d in %s",
		t.rounds, t.decoded, t.undecoded, outcome, t.boxesBefore, cubeBoxCount(s, ev), time.Since(t.start).Round(time.Second))
	if t.firstUndecoded != "" {
		line += "; first undecoded " + t.firstUndecoded
	}
	log(line)
}
