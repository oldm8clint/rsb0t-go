package mulebus

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

func TestTrawlerLobbyCountsDistinctCrewOnTheAnchorsWorld(t *testing.T) {
	b := NewBus()
	now := time.Now()
	lobby := tile.Tile{X: 2671, Z: 3170, Level: 1}
	put(b, now.Add(-30*time.Second), TrawlerMessage("crew_a", lobby, TrawlerLobby, 2))
	put(b, now.Add(-10*time.Second), TrawlerMessage("Crew A", lobby, TrawlerLobby, 2)) // the same account again
	put(b, now.Add(-5*time.Second), TrawlerMessage("crew_b", lobby, TrawlerLobby, 2))
	put(b, now.Add(-5*time.Second), TrawlerMessage("crew_c", lobby, TrawlerLobby, 1))  // another world
	put(b, now.Add(-5*time.Second), TrawlerMessage("crew_d", lobby, TrawlerAboard, 2)) // aboard, not waiting
	put(b, now.Add(-4*time.Minute), TrawlerMessage("crew_e", lobby, TrawlerLobby, 2))  // stale
	put(b, now.Add(-5*time.Second), TrawlerMessage("master", lobby, TrawlerLobby, 2))  // the anchor is no crew
	put(b, now.Add(-5*time.Second), HereMessage("donor", tile.Tile{X: 2809, Z: 3441})) // a plain KindHere
	views := ReadTrawler(b, now, 0)
	if got := TrawlerLobbyCount(views, "master", 2, 0); got != 2 {
		t.Fatalf("lobby count on world 2 = %d, want 2 (%+v)", got, views)
	}
	if got := TrawlerLobbyCount(views, "master", 0, 0); got != 3 {
		t.Fatalf("lobby count on any world = %d, want 3", got)
	}
}

func TestTrawlerNewestMessageWinsPerAccount(t *testing.T) {
	b := NewBus()
	now := time.Now()
	at := tile.Tile{X: 1880, Z: 4830, Level: 1}
	put(b, now.Add(-20*time.Second), TrawlerMessage("crew_a", at, TrawlerLobby, 1))
	put(b, now.Add(-3*time.Second), TrawlerMessage("crew_a", at, TrawlerAboardAnchor, 1))
	views := ReadTrawler(b, now, 0)
	if len(views) != 1 || views[0].Role != TrawlerAboardAnchor || views[0].World != 1 {
		t.Fatalf("views = %+v", views)
	}
	if TrawlerLobbyCount(views, "master", 1, 0) != 0 {
		t.Fatal("an account now aboard is not waiting in the lobby")
	}
}

func TestTrawlerAnchorCalledReadsTheAnchorsNewestRole(t *testing.T) {
	b := NewBus()
	now := time.Now()
	at := tile.Tile{X: 2672, Z: 3170, Level: 1}
	if TrawlerAnchorCalled(ReadTrawler(b, now, 0), "master", 1, 0) {
		t.Fatal("an empty bus calls nobody")
	}
	put(b, now.Add(-4*time.Second), TrawlerMessage("Master", at, TrawlerBoard, 1))
	if !TrawlerAnchorCalled(ReadTrawler(b, now, 0), "master", 1, 0) {
		t.Fatal("the anchor's board call is heard")
	}
	if TrawlerAnchorCalled(ReadTrawler(b, now, 0), "master", 2, 0) {
		t.Fatal("a call on world 1 is no call on world 2")
	}
	put(b, now.Add(-1*time.Second), TrawlerMessage("master", at, TrawlerAboard, 1))
	if TrawlerAnchorCalled(ReadTrawler(b, now, 0), "master", 1, 0) {
		t.Fatal("the anchor aboard has stopped calling")
	}
}

func TestTrawlerBlockReadsTheAnchorsNewestRole(t *testing.T) {
	b := NewBus()
	now := time.Now()
	if _, on := TrawlerBlock(ReadTrawler(b, now, TrawlerBlockTTL), "master"); on {
		t.Fatal("no anchor message: no block")
	}
	b.Publish(TrawlerMessage("master", tile.Tile{X: 2676, Z: 3170}, TrawlerGather, 1))
	b.Publish(TrawlerMessage("crew1", tile.Tile{X: 2671, Z: 3170, Level: 1}, TrawlerLobby, 1))
	if w, on := TrawlerBlock(ReadTrawler(b, now, TrawlerBlockTTL), "master"); !on || w != 1 {
		t.Fatalf("got world %d on %v, want world 1 on", w, on)
	}
}

func TestTrawlerWatchesSplitTheLobbyAndTheCall(t *testing.T) {
	b := NewBus()
	now := time.Now()
	lobby := tile.Tile{X: 2671, Z: 3170, Level: 1}
	put(b, now.Add(-5*time.Second), WithWatch(TrawlerMessage("crew_a", lobby, TrawlerLobby, 1), 1))
	put(b, now.Add(-5*time.Second), WithWatch(TrawlerMessage("crew_b", lobby, TrawlerLobby, 1), 2))
	put(b, now.Add(-5*time.Second), TrawlerMessage("crew_c", lobby, TrawlerLobby, 1)) // named no watch: counts for both
	put(b, now.Add(-2*time.Second), WithWatch(TrawlerMessage("master", lobby, TrawlerBoard, 1), 2))
	views := ReadTrawler(b, now, 0)
	if got := TrawlerLobbyCount(views, "master", 1, 1); got != 2 {
		t.Fatalf("watch 1 count = %d, want 2", got)
	}
	if got := TrawlerLobbyCount(views, "master", 1, 0); got != 3 {
		t.Fatalf("any-watch count = %d, want 3", got)
	}
	if TrawlerAnchorCalled(views, "master", 1, 1) || !TrawlerAnchorCalled(views, "master", 1, 2) || !TrawlerAnchorCalled(views, "master", 1, 0) {
		t.Fatal("a call for watch 2 calls watch 2 only")
	}
}
