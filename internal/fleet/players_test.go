package fleet

import (
	"testing"

	"rsb0t-go/internal/client"
	"rsb0t-go/internal/gamestate"
)

func TestMergeSeenPlayersDedupesAndExcludesOwnAccounts(t *testing.T) {
	own := map[string]bool{PlayerNameKey("spare_22"): true, PlayerNameKey("acct01"): true}
	views := [][]gamestate.Player{
		{{Name: "Zezima", X: 2810, Z: 3440}, {Name: "Spare 22", X: 2811, Z: 3441}, {Name: "", X: 1, Z: 1}},
		{{Name: "zezima", X: 2810, Z: 3440, InCombat: true}, {Name: "Acct01", X: 5, Z: 5}, {Name: "Bob", X: 3222, Z: 3218, Level: 1}},
		{{Name: "Bob", X: 3222, Z: 3218, Level: 1}, {Name: "Bob", X: 3222, Z: 3218, Level: 1}},
	}
	got := MergeSeenPlayers(views, own)
	if got.Observers != 3 || got.Unnamed != 1 {
		t.Fatalf("observers=%d unnamed=%d, want 3 and 1", got.Observers, got.Unnamed)
	}
	if len(got.Players) != 2 {
		t.Fatalf("players = %+v, want Bob and Zezima only (own accounts excluded, duplicates merged)", got.Players)
	}
	bob, zez := got.Players[0], got.Players[1]
	if bob.Name != "Bob" || bob.SeenBy != 2 || bob.Level != 1 {
		t.Errorf("Bob = %+v, want seen by 2 sessions (a duplicate within one view counts once)", bob)
	}
	if zez.Name != "Zezima" || zez.SeenBy != 2 || !zez.InCombat || zez.X != 2810 {
		t.Errorf("Zezima = %+v, want seen by 2, in combat from either view", zez)
	}
}

// SeenPlayers must read each CONNECTED session's players exactly once and take no other
// per-session lock: that read holds the same state lock as the session's packet read loop.
func TestSeenPlayersReadsEachConnectedSessionOnce(t *testing.T) {
	var reads int
	old := sessionPlayers
	sessionPlayers = func(*client.Session) []gamestate.Player {
		reads++
		return []gamestate.Player{{Name: "Stranger", X: 3200, Z: 3200}, {Name: "Online", X: 3201, Z: 3200}}
	}
	defer func() { sessionPlayers = old }()

	m := &Manager{}
	m.accounts = []*account{
		{username: "online", session: &client.Session{}},
		{username: "second", session: &client.Session{}},
		{username: "offline"},
	}
	got := m.SeenPlayers()
	if reads != 2 {
		t.Errorf("read players %d times, want once per connected session (2)", reads)
	}
	if got.Observers != 2 || len(got.Players) != 1 || got.Players[0].Name != "Stranger" || got.Players[0].SeenBy != 2 {
		t.Errorf("got %+v, want only the stranger, seen by both sessions", got)
	}
}

// A zero-value session (no game state yet) must not panic the map's player read.
func TestSeenPlayersToleratesASessionWithNoState(t *testing.T) {
	m := &Manager{}
	m.accounts = []*account{{username: "online", session: &client.Session{}}}
	if got := m.SeenPlayers(); got.Observers != 1 || len(got.Players) != 0 {
		t.Errorf("got %+v", got)
	}
}
