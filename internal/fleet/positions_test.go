package fleet

import (
	"testing"

	"rsb0t-go/internal/client"
)

func TestPositionsReadsEachConnectedSessionOnce(t *testing.T) {
	var reads int
	old := sessionTile
	sessionTile = func(s *client.Session) (int, int, int, bool) {
		reads++
		return 3222, 3218, 1, true
	}
	defer func() { sessionTile = old }()

	m := &Manager{}
	m.accounts = []*account{
		{username: "online", session: &client.Session{}},
		{username: "offline"},
	}
	got := m.Positions(nil)
	if reads != 1 {
		t.Errorf("read tiles %d times, want once per connected session (1)", reads)
	}
	if len(got) != 1 || got[0] != (AccountPosition{Username: "online", X: 3222, Z: 3218, Level: 1}) {
		t.Errorf("got %+v", got)
	}
}

// A session with no game state yet, or no tile yet, reports nothing rather than panicking.
func TestPositionsSkipsASessionWithNoTile(t *testing.T) {
	m := &Manager{}
	m.accounts = []*account{{username: "fresh", session: &client.Session{}}}
	if got := m.Positions(nil); len(got) != 0 {
		t.Errorf("got %+v, want nothing", got)
	}
}
