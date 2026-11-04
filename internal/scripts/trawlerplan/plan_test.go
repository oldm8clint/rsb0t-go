package trawlerplan

import (
	"fmt"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

func TestDecodeSettingsDefaultsAndStrict(t *testing.T) {
	s, err := DecodeSettings(nil)
	if err != nil || s != DefaultSettings() {
		t.Fatalf("defaults: %+v %v", s, err)
	}
	s, err = DecodeSettings(map[string]interface{}{"anchor": "boss", "world": "2", "bucket": "false", "kitRope": float64(5), "pinWorld": float64(2)})
	if err != nil || s.Anchor != "boss" || s.World != 2 || s.Bucket || s.KitRope != 5 {
		t.Fatalf("decoded: %+v %v", s, err)
	}
	for _, bad := range []map[string]interface{}{
		{"Anchor": "x"}, {"nope": 1}, {"world": "two"}, {"kitPaste": 501}, {"leaveAtSecondsLeft": 90},
		{"anchor": " "}, {"logDetail": "loud"}, {"bucket": 3}, {"kitRope": 20, "kitBuckets": 10},
	} {
		if _, err := DecodeSettings(bad); err == nil {
			t.Errorf("%v: expected an error", bad)
		}
	}
}

func TestParseTimeLeft(t *testing.T) {
	cases := []struct {
		in    string
		secs  int
		under bool
		ok    bool
	}{
		{"10 mins", 659, false, true}, {"1 min", 119, false, true}, {"Under 1 min", 60, true, true},
		{" 3 mins ", 239, false, true}, {"", 0, false, false}, {"soon", 0, false, false}, {"x mins", 0, false, false},
	}
	for _, c := range cases {
		secs, under, ok := ParseTimeLeft(c.in)
		if secs != c.secs || under != c.under || ok != c.ok {
			t.Errorf("%q: got %d %v %v", c.in, secs, under, ok)
		}
	}
}

func TestSecondsLeftCountsDownTheLastMinute(t *testing.T) {
	if got := SecondsLeft("Under 1 min", 10*time.Second); got != 50 {
		t.Errorf("10 s in: %d", got)
	}
	if got := SecondsLeft("Under 1 min", 90*time.Second); got != 0 {
		t.Errorf("past the minute: %d", got)
	}
	if got := SecondsLeft("2 mins", 0); got != 179 {
		t.Errorf("2 mins: %d", got)
	}
	if got := SecondsLeft("?", 0); got != -1 {
		t.Errorf("unknown: %d", got)
	}
}

func TestZones(t *testing.T) {
	if !OnBoat(tile.Tile{X: 1860, Z: 4810, Level: 1}) || OnFloodedBoat(tile.Tile{X: 1860, Z: 4810}) {
		t.Error("unflooded boat")
	}
	if !OnFloodedBoat(tile.Tile{X: 1990, Z: 4810}) || OnBoat(tile.Tile{X: 1950, Z: 4810}) {
		t.Error("flooded boat / gap")
	}
	if !InLobby(tile.Tile{X: 2671, Z: 3170, Level: 1}) || InLobby(tile.Tile{X: 2674, Z: 3170}) {
		t.Error("lobby")
	}
	if !FarFromKhazard(PortSarim) || FarFromKhazard(Stores[0]) || FarFromKhazard(tile.Tile{X: 1860, Z: 4810}) {
		t.Error("far from khazard")
	}
}

func TestShouldLeave(t *testing.T) {
	set := DefaultSettings()
	if ShouldLeave(-1, true, false, 0, set) || ShouldLeave(100, true, false, 0, set) {
		t.Error("too early / unknown")
	}
	if ShouldLeave(4, false, false, 0, set) {
		t.Error("left with the anchor not aboard")
	}
	if !ShouldLeave(12, true, false, 0, set) || ShouldLeave(13, true, false, 0, set) || !ShouldLeave(0, true, false, 0, set) {
		t.Error("should leave")
	}
	if ShouldLeave(4, true, true, -1, set) || ShouldLeave(4, true, true, 81, set) {
		t.Error("left a flooded boat at an unknown or high level")
	}
	if !ShouldLeave(4, true, true, 40, set) {
		t.Error("a mild flood should let the crew leave")
	}
}

func TestLeaveWorld(t *testing.T) {
	if LeaveWorld(1) != 2 || LeaveWorld(2) != 1 || LeaveWorld(3) != 1 || LeaveWorld(9) != 1 {
		t.Error("leave world")
	}
}

func TestDecidePhases(t *testing.T) {
	set := DefaultSettings()
	full := Snapshot{Paste: 60, Rope: 1, Bucket: 1, World: 1, SecondsLeft: -1}
	at := func(s Snapshot, t tile.Tile) Snapshot { s.Here = t; return s }
	khazard := tile.Tile{X: 2650, Z: 3165}
	cases := []struct {
		name string
		s    Snapshot
		want Phase
	}{
		{"no kit", at(Snapshot{World: 1, SecondsLeft: -1}, khazard), PhaseKit},
		{"kit held", at(full, khazard), PhaseTravel},
		{"stock out goes anyway", at(Snapshot{KitGaveUp: true, World: 1}, khazard), PhaseTravel},
		{"lobby", at(full, tile.Tile{X: 2671, Z: 3170, Level: 1}), PhaseLobby},
		{"port sarim", at(full, PortSarim), PhaseReturn},
		{"aboard", at(full, tile.Tile{X: 1860, Z: 4810, Level: 1}), PhaseAboard},
		{"sunk", at(full, tile.Tile{X: 1957, Z: 4823}), PhaseEscape},
	}
	for _, c := range cases {
		if got := Decide(c.s, set); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
	pinned := set
	pinned.World = 2
	if got := Decide(at(full, khazard), pinned); got != PhaseHome {
		t.Errorf("wrong world: %s", got)
	}
	if got := Decide(at(full, PortSarim), pinned); got != PhaseReturn {
		t.Errorf("port sarim on the wrong world walks first: %s", got)
	}
	leaving := at(full, tile.Tile{X: 1860, Z: 4810, Level: 1})
	leaving.SecondsLeft, leaving.AnchorAboard = 4, true
	if got := Decide(leaving, set); got != PhaseLeave {
		t.Errorf("leave: %s", got)
	}
	leaving.AnchorAboard = false
	if got := Decide(leaving, set); got != PhaseAboard {
		t.Errorf("stay without the anchor: %s", got)
	}
	noBucket := set
	noBucket.Bucket = false
	if !KitComplete(Snapshot{Paste: 60, Rope: 1}, noBucket) || KitComplete(Snapshot{Paste: 60, Rope: 1}, set) {
		t.Error("bucket requirement")
	}
}

func TestAboardChoices(t *testing.T) {
	deck := tile.Tile{X: 1860, Z: 4810, Level: 1}
	hold := tile.Tile{X: 1860, Z: 4810, Level: 0}
	fdeck := tile.Tile{X: 1990, Z: 4810, Level: 1}
	fhold := tile.Tile{X: 1990, Z: 4810, Level: 0}
	near, far := tile.Tile{X: 1862, Z: 4810, Level: 1}, tile.Tile{X: 1880, Z: 4810, Level: 1}
	cases := []struct {
		name string
		v    BoatView
		want ActionKind
		at   tile.Tile
	}{
		{"nearest leak", BoatView{Here: deck, Leaks: []tile.Tile{far, near}, Paste: 2}, ActFillLeak, near},
		{"leak but no paste: down to the hold", BoatView{Here: deck, Leaks: []tile.Tile{near}, Rope: 1}, ActClimbDown, tile.Tile{}},
		{"net beats leak (no fish while ripped)", BoatView{Here: deck, Leaks: []tile.Tile{near}, RippedNets: []tile.Tile{far}, NetRipped: true, Paste: 1, Rope: 1}, ActFixNet, far},
		{"fix net", BoatView{Here: deck, RippedNets: []tile.Tile{far}, NetRipped: true, Rope: 1}, ActFixNet, far},
		{"net but no rope: down to the hold", BoatView{Here: deck, RippedNets: []tile.Tile{far}, NetRipped: true}, ActClimbDown, tile.Tile{}},
		{"ripped, none visible, in the hold", BoatView{Here: hold, NetRipped: true, Rope: 1}, ActClimbUp, tile.Tile{}},
		{"bail in the flooded hold", BoatView{Here: fhold, Bucket: 1}, ActBail, tile.Tile{}},
		{"bail beats a leak in the flooded hold", BoatView{Here: fhold, Bucket: 1, Paste: 5, Leaks: []tile.Tile{{X: 1991, Z: 4810}}}, ActBail, tile.Tile{}},
		{"empty a full bucket", BoatView{Here: fhold, BucketFull: 1}, ActEmpty, tile.Tile{}},
		{"flooded deck goes down", BoatView{Here: fdeck, Bucket: 1}, ActClimbDown, tile.Tile{}},
		{"no bucket on the flooded deck: down to the hold", BoatView{Here: fdeck}, ActClimbDown, tile.Tile{}},
		{"the unflooded hold stays (every hull leak is there)", BoatView{Here: hold, Bucket: 1}, ActIdle, tile.Tile{}},
		{"idle on deck goes down", BoatView{Here: deck}, ActClimbDown, tile.Tile{}},
		{"idle in the hold", BoatView{Here: hold}, ActIdle, tile.Tile{}},
	}
	for _, c := range cases {
		got := Aboard(c.v)
		if got.Kind != c.want || (c.at != tile.Tile{} && got.Target != c.at) {
			t.Errorf("%s: got %+v want %s %v", c.name, got, c.want, c.at)
		}
	}
}

func TestAboardPostsAndNetFirst(t *testing.T) {
	post := HullPost(3, false)
	if post != (tile.Tile{X: 1888, Z: 4826}) || HullPost(11, true) != (tile.Tile{X: 2016, Z: 4823}) || HullPost(-5, false).X < 1885 {
		t.Fatalf("posts: %v %v", post, HullPost(11, true))
	}
	hold := tile.Tile{X: 1886, Z: 4824}
	far := tile.Tile{X: 1892, Z: 4823}
	// A leak at someone else's post is left to them; the account goes to its own.
	if a := Aboard(BoatView{Here: hold, Paste: 5, Leaks: []tile.Tile{far}, Post: &post}); a.Kind != ActGoTo || a.Target != (tile.Tile{X: 1888, Z: 4825}) {
		t.Fatalf("to the post: %+v", a)
	}
	// Its own leak: filled.
	if a := Aboard(BoatView{Here: tile.Tile{X: 1888, Z: 4825}, Paste: 5, Leaks: []tile.Tile{far, post}, Post: &post}); a.Kind != ActFillLeak || a.Target != post {
		t.Fatalf("own leak: %+v", a)
	}
	// At the post with nothing leaking: wait (alch).
	if a := Aboard(BoatView{Here: tile.Tile{X: 1888, Z: 4825}, Paste: 5, Post: &post}); a.Kind != ActIdle {
		t.Fatalf("waiting at the post: %+v", a)
	}
	// The anchor mends a ripped net before any leak.
	if a := Aboard(BoatView{Here: hold, Paste: 5, Rope: 1, Leaks: []tile.Tile{post}, NetRipped: true, NetFirst: true}); a.Kind != ActClimbUp {
		t.Fatalf("net first: %+v", a)
	}
}

func TestAboardNetNeedsCrafting(t *testing.T) {
	deck := tile.Tile{X: 1860, Z: 4810, Level: 1}
	net := tile.Tile{X: 1880, Z: 4826, Level: 1}
	if a := Aboard(BoatView{Here: deck, RippedNets: []tile.Tile{net}, NetRipped: true, Rope: 1, Crafting: 20}); a.Kind == ActFixNet {
		t.Fatalf("Crafting 20 leaves the net to better menders: %+v", a)
	}
	if a := Aboard(BoatView{Here: deck, RippedNets: []tile.Tile{net}, NetRipped: true, Rope: 1, Crafting: 60}); a.Kind != ActFixNet {
		t.Fatalf("Crafting 60 mends: %+v", a)
	}
}

func TestMurphyAndBoardClocks(t *testing.T) {
	for _, c := range []struct {
		line string
		n    int
		ok   bool
	}{{"9 minutes left!", 9, true}, {"1 minute left!", 1, true}, {"Arrh! Check that net!", 0, false}, {"10 minutes left!", 0, false}} {
		if n, ok := ParseMurphyMinutes(c.line); n != c.n || ok != c.ok {
			t.Errorf("%q: %d %v", c.line, n, ok)
		}
	}
	if MurphySecondsLeft(1, 55*time.Second) != 5 || MurphySecondsLeft(0, 0) != -1 {
		t.Error("murphy clock")
	}
	if BoardSecondsLeft(630*time.Second) != 5 {
		t.Errorf("board clock %d", BoardSecondsLeft(630*time.Second))
	}
}

func TestAboardNetFixerWaitsByTheNet(t *testing.T) {
	hold := tile.Tile{X: 1886, Z: 4824}
	deck := tile.Tile{X: 1883, Z: 4826, Level: 1}
	net := tile.Tile{X: 1880, Z: 4826, Level: 1}
	if a := Aboard(BoatView{Here: hold, Rope: 1, NetFixer: true}); a.Kind != ActClimbUp {
		t.Fatalf("a fixer climbs at once: %+v", a)
	}
	if a := Aboard(BoatView{Here: deck, Rope: 1, NetFixer: true}); a.Kind != ActIdle {
		t.Fatalf("a fixer waits on deck: %+v", a)
	}
	if a := Aboard(BoatView{Here: deck, Rope: 1, NetFixer: true, NetRipped: true, RippedNets: []tile.Tile{net}}); a.Kind != ActFixNet {
		t.Fatalf("a fixer mends a rip: %+v", a)
	}
	if a := Aboard(BoatView{Here: tile.Tile{X: 2014, Z: 4824}, Bucket: 1, NetFixer: true, Rope: 1}); a.Kind != ActBail {
		t.Fatalf("on the flooded boat a fixer with a bucket bails first: %+v", a)
	}
}

func TestAboardRippedNetBeatsBailingShortOfTheEmergency(t *testing.T) {
	fhold := tile.Tile{X: 2014, Z: 4824}
	rope := BoatView{Here: fhold, Bucket: 1, Rope: 2, NetRipped: true, FloodLevel: 29, BailAt: 15}
	if a := Aboard(rope); a.Kind != ActClimbUp {
		t.Fatalf("at 29 water a rope holder climbs to the ripped net rather than bail (live 4 Nov 2026, 08:19): %+v", a)
	}
	full := rope
	full.Bucket, full.BucketFull = 0, 1
	if a := Aboard(full); a.Kind != ActEmpty {
		t.Fatalf("a full bucket is emptied first: %+v", a)
	}
	high := rope
	high.FloodLevel = BailEmergency
	if a := Aboard(high); a.Kind != ActBail {
		t.Fatalf("from BailEmergency the bucket comes first: %+v", a)
	}
	unknown := rope
	unknown.FloodLevel = -1
	if a := Aboard(unknown); a.Kind != ActBail {
		t.Fatalf("with no water reading the bucket comes first: %+v", a)
	}
	low := rope
	low.Crafting = 20
	if a := Aboard(low); a.Kind != ActBail {
		t.Fatalf("under NetCrafting the rope holder bails: %+v", a)
	}
}

func TestWatchSplitsTheFleetInTwoStableHalves(t *testing.T) {
	n := [Watches + 1]int{}
	for i := 0; i < 400; i++ {
		n[Watch(fmt.Sprintf("swarm%d", i))]++
	}
	if n[0] != 0 || n[1] < 160 || n[2] < 160 {
		t.Fatalf("watches = %v, want two halves of 400", n)
	}
	if Watch("acct7") != Watch(" acct7") {
		t.Fatal("the watch is case- and space-blind")
	}
	if GameWatch(0) != 1 || GameWatch(1) != 2 || GameWatch(2) != 1 {
		t.Fatal("games alternate the watches from watch 1")
	}
}

func TestBailersHoldTheFloodedBoatBetweenTheirLevels(t *testing.T) {
	fhold := tile.Tile{X: 1990, Z: 4810, Level: 0}
	if a := Aboard(BoatView{Here: fhold, Bucket: 1, FloodLevel: 20, BailAt: 30}); a.Kind != ActIdle {
		t.Fatalf("under its level a bailer waits: %+v", a)
	}
	if a := Aboard(BoatView{Here: fhold, Bucket: 1, FloodLevel: 30, BailAt: 30}); a.Kind != ActBail {
		t.Fatalf("at its level a bailer bails: %+v", a)
	}
	if a := Aboard(BoatView{Here: fhold, Bucket: 1, FloodLevel: 0, BailAt: 30}); a.Kind != ActBail {
		t.Fatalf("no reading on the flooded boat: bail: %+v", a)
	}
	if a := Aboard(BoatView{Here: fhold, Bucket: 1, Rope: 1, NetFixer: true, FloodLevel: 20, BailAt: 90}); a.Kind != ActClimbUp {
		t.Fatalf("a net fixer climbs to the net on the flooded boat too: %+v", a)
	}
	if Bailing(true, true, 30, 20, 30) || !Bailing(true, true, 30, 40, 30) || Bailing(true, true, 10, 40, 30) {
		t.Fatal("the quiet period keeps only the bailers at their level bailing, until BailUntilSecondsLeft")
	}
	if BailAt(0) != BailLow || BailAt(BailRanks-1) >= BailEmergency || BailAt(BailRanks) < BailEmergency || BailAt(1000) >= 100 || BailAt(-1) < BailEmergency {
		t.Fatalf("bail levels: rank 0 %d, last %d, next %d", BailAt(0), BailAt(BailRanks-1), BailAt(BailRanks))
	}
	for r := 1; r < BailRanks; r++ {
		if BailAt(r) <= BailAt(r-1)-1 || BailAt(r)-BailAt(r-1) > 3 {
			t.Fatalf("bail levels rise slowly by rank: %d then %d", BailAt(r-1), BailAt(r))
		}
	}
	if CrewRank("bob", []string{"Alice", "bob", "carl"}) != 1 || CrewRank("Zed", nil) != 0 || CrewRank("aa", []string{"b", "c"}) != 0 {
		t.Fatal("crew rank by name, case-blind, self counted once")
	}

}

func TestCrewWaitsAsideWhileTheAnchorIsAtSea(t *testing.T) {
	set := Settings{KitPaste: 1, KitRope: 1}
	lobby := tile.Tile{X: 2671, Z: 3170, Level: 1}
	s := Snapshot{Here: lobby, Paste: 1, Rope: 1, AnchorAtSea: true}
	if p := Decide(s, set); p != PhaseAside {
		t.Fatalf("anchor at sea: %s, want aside", p)
	}
	s.AnchorCalled = true
	if p := Decide(s, set); p != PhaseLobby {
		t.Fatalf("anchor calling: %s, want lobby", p)
	}
	s.AnchorCalled, s.AnchorAtSea = false, false
	if p := Decide(s, set); p != PhaseLobby {
		t.Fatalf("anchor ashore: %s, want lobby", p)
	}
	s.AnchorAtSea, s.Paste = true, 0
	if p := Decide(s, set); p == PhaseAside {
		t.Fatal("a short kit is bought first")
	}
	seen := map[tile.Tile]int{}
	for i := 0; i < 200; i++ {
		seen[AsideSpot(fmt.Sprintf("swarm%d", i))]++
	}
	if len(seen) != len(AsideSpots) || AsideSpot("acct7") != AsideSpot("acct7") {
		t.Fatalf("aside spots used: %v", seen)
	}
	for _, n := range seen {
		if n < 60 {
			t.Fatalf("aside spots uneven: %v", seen)
		}
	}
}

func TestAPostGuardWithRopeClimbsToARippedNet(t *testing.T) {
	post := HullPost(3, true)
	v := BoatView{Here: postStand(post), Post: &post, Paste: 5, Rope: 1, Crafting: 50, NetRipped: true, FloodLevel: 40, BailAt: 85}
	v.Here.Level = 0
	if a := Aboard(v); a.Kind != ActClimbUp {
		t.Fatalf("a guard holding rope leaves its post for the ripped net: %+v", a)
	}
	v.Crafting = 20
	if a := Aboard(v); a.Kind == ActClimbUp {
		t.Fatalf("under NetCrafting the guard stays: %+v", a)
	}
}

func TestLootersTakeTheRelayedCatchAndNeverSail(t *testing.T) {
	set := Settings{KitPaste: 1, KitRope: 1}
	s := Snapshot{Here: LootStand, Looter: true, FreeSlots: 20, AnchorCalled: true}
	if p := Decide(s, set); p != PhaseLoot {
		t.Fatalf("nothing to take: %s, want loot (never the lobby, even when called)", p)
	}
	s.GroundFish = 5
	if p := Decide(s, set); p != PhasePickup {
		t.Fatalf("fish shown: %s, want pickup", p)
	}
	s.FreeSlots, s.CatchHeld = 0, 28
	if p := Decide(s, set); p != PhaseBank {
		t.Fatalf("full: %s, want bank", p)
	}
	n := 0
	for i := 0; i < 1000; i++ {
		if Looter(fmt.Sprintf("swarm%d", i)) {
			n++
		}
	}
	if n < 35 || n > 90 || Looter("acct7") != Looter("acct7") {
		t.Fatalf("looters = %d of 1000", n)
	}
}
