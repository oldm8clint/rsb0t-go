package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/tile"
)

func guildSnap(level int, at tile.Tile, tools ...string) fisherplan.FishSnapshot {
	return fisherplan.FishSnapshot{Tile: &at, Level: level, Target: "Big net", HeldTools: tools, BankTools: []string{}}
}

func TestFishingGuildSettingDefaultsOn(t *testing.T) {
	for _, m := range []map[string]interface{}{nil, {}, {"fishingGuild": "nonsense"}, {"fishingGuild": true}} {
		if b := (&fisherBot{guild: readFisherGuild(m, "someone")}); !b.guild.enabled {
			t.Fatalf("%v: fishingGuild must default on (owner 2026-09-19)", m)
		}
	}
}

func TestFishingGuildSettingCanBeTurnedOff(t *testing.T) {
	for _, m := range []map[string]interface{}{{"fishingGuild": false}} {
		b := &fisherBot{guild: readFisherGuild(m, "someone")}
		if b.guild.enabled {
			t.Fatalf("%v: an explicit false must turn the Guild off", m)
		}
		s := guildSnap(99, fisherplan.FishingGuild().Bank, "Big fishing net")
		b.selectFishingArea(&Context{}, &s)
		if s.Guild || b.guild.active || b.layout != fisherplan.Catherby() {
			t.Fatalf("%v: an account with the setting off must keep Catherby: %+v", m, b.layout)
		}
		if b.guildMuleAt(1234) != 1234 {
			t.Fatalf("%v: the muleAt threshold must be unchanged", m)
		}
		if _, _, ok := b.guildTravelTarget(fisherplan.PhaseTravel, s); ok {
			t.Fatalf("%v: no Guild walk with the setting off", m)
		}
		if b.guildDriftRef(nil) != nil {
			t.Fatalf("%v: no Guild drift reference with the setting off", m)
		}
	}
}

func TestFishingGuildNeedsLevel68AndAGuildRung(t *testing.T) {
	on := map[string]interface{}{"fishingGuild": true}
	cases := []struct {
		level int
		at    tile.Tile
		tools []string
		guild bool
	}{
		{67, fisherplan.Catherby().Bank, []string{"Big fishing net"}, false},
		{68, fisherplan.Catherby().Bank, []string{"Big fishing net"}, true},
		{68, fisherplan.FishingGuild().Bank, []string{"Big fishing net"}, true},
		// A lost Big net is fetched from the Guild's spawn, so the Guild is selected even
		// with only a small net (or nothing) held.
		{80, fisherplan.Catherby().Bank, []string{"Small fishing net"}, true},
		{80, fisherplan.Catherby().Bank, nil, true},
		// Away from the Guild's mainland it recovers by way of Catherby first.
		{80, tile.Tile{X: 3222, Z: 3218}, []string{"Big fishing net"}, false},
	}
	for _, c := range cases {
		b := &fisherBot{guild: readFisherGuild(on, "someone")}
		s := guildSnap(c.level, c.at, c.tools...)
		b.selectFishingArea(&Context{}, &s)
		if s.Guild != c.guild || b.guild.active != c.guild {
			t.Fatalf("%+v: guild=%v", c, s.Guild)
		}
		want := fisherplan.Catherby()
		if c.guild {
			want = fisherplan.FishingGuild()
		}
		if b.layout != want {
			t.Fatalf("%+v: layout %+v", c, b.layout)
		}
	}
}

func TestFishingGuildMuleThreshold(t *testing.T) {
	b := &fisherBot{guild: readFisherGuild(map[string]interface{}{"fishingGuild": true}, "someone")}
	s := guildSnap(70, fisherplan.FishingGuild().Bank, "Big fishing net")
	b.selectFishingArea(&Context{}, &s)
	got := b.guildMuleAt(1000)
	// The band is derived from the constant, not written out: it was hardcoded to
	// 7,000-13,000 around a FishingGuildMuleAt of 10,000, so doubling that constant broke a
	// test that had nothing to say about the change (2026-09-20).
	lo := fisherplan.FishingGuildMuleAt - fisherplan.FishingGuildMuleAt*muleAtJitterPercent/100
	hi := fisherplan.FishingGuildMuleAt + fisherplan.FishingGuildMuleAt*muleAtJitterPercent/100
	if got < lo || got > hi || got != jitterMuleAt(fisherplan.FishingGuildMuleAt, "someone") {
		t.Fatalf("Guild threshold = %d, want FishingGuildMuleAt (%d) spread per account, within %d..%d", got, fisherplan.FishingGuildMuleAt, lo, hi)
	}
	// Level 67: the planner keeps Catherby and its threshold.
	s = guildSnap(67, fisherplan.Catherby().Bank, "Big fishing net")
	b.selectFishingArea(&Context{}, &s)
	if b.guildMuleAt(1000) != 1000 {
		t.Fatal("a Catherby-planned account keeps muleAt")
	}
	// An explicit muleAt is kept even at the Guild.
	b = &fisherBot{guild: readFisherGuild(map[string]interface{}{"fishingGuild": true, "muleAt": 800.0}, "someone")}
	s = guildSnap(70, fisherplan.FishingGuild().Bank, "Big fishing net")
	b.selectFishingArea(&Context{}, &s)
	if b.guildMuleAt(800) != 800 {
		t.Fatal("an explicit muleAt must win over the Guild threshold")
	}
}

func TestFishingGuildTravelTargets(t *testing.T) {
	b := &fisherBot{guild: readFisherGuild(map[string]interface{}{"fishingGuild": true}, "someone")}
	// A Guild account at Catherby walks to the Guild to bank or fish, but buys in Catherby.
	s := guildSnap(70, fisherplan.Catherby().Bank, "Big fishing net")
	b.selectFishingArea(&Context{}, &s)
	for _, p := range []fisherplan.FishPhase{fisherplan.PhaseBank, fisherplan.PhaseFish} {
		if dest, _, ok := b.guildTravelTarget(p, s); !ok || dest != fisherplan.FishingGuild().Bank {
			t.Fatalf("%s at Catherby should walk to the Guild", p)
		}
	}
	for _, p := range []fisherplan.FishPhase{fisherplan.PhaseBuy, fisherplan.PhaseSell} {
		if _, _, ok := b.guildTravelTarget(p, s); ok {
			t.Fatalf("%s must stay in Catherby", p)
		}
	}
	// Inside the Guild nothing is intercepted.
	s = guildSnap(70, fisherplan.FishingGuild().Camp, "Big fishing net")
	b.selectFishingArea(&Context{}, &s)
	if _, _, ok := b.guildTravelTarget(fisherplan.PhaseFish, s); ok {
		t.Fatal("fishing inside the Guild is an ordinary phase")
	}
	// A Catherby-planned account inside the Guild (here: a toolless one whose spawn attempt
	// is backing off) walks out to Catherby through the doors.
	b.guild.spawnBackoffUntil = time.Now().Add(time.Minute)
	s = guildSnap(70, fisherplan.FishingGuild().Bank)
	b.selectFishingArea(&Context{}, &s)
	if dest, _, ok := b.guildTravelTarget(fisherplan.PhaseTravel, s); !ok || dest != fisherplan.Catherby().Bank {
		t.Fatal("a toolless account in the Guild should walk to Catherby")
	}
}

func TestFishingGuildToolComesFromTheSpawn(t *testing.T) {
	on := map[string]interface{}{"fishingGuild": true}
	b := &fisherBot{guild: readFisherGuild(on, "someone")}
	// Bank read, no net anywhere: the spawn.
	s := guildSnap(70, fisherplan.FishingGuild().Bank)
	b.selectFishingArea(&Context{}, &s)
	sp, ok := b.guildToolTarget(s)
	if !ok || sp.Tool != "Big fishing net" || sp.ObjID != 305 {
		t.Fatalf("want the Big net spawn, got %+v ok=%v", sp, ok)
	}
	// Bank never read: read it (at the Guild) first.
	s = guildSnap(70, fisherplan.FishingGuild().Bank)
	s.BankTools = nil
	b.selectFishingArea(&Context{}, &s)
	if _, ok := b.guildToolTarget(s); ok || !s.Guild {
		t.Fatal("an unread bank is read at the Guild before the spawn is visited")
	}
	// The net in the bank: withdraw it, no spawn.
	s = guildSnap(70, fisherplan.FishingGuild().Bank)
	s.BankTools = []string{"Big fishing net"}
	b.selectFishingArea(&Context{}, &s)
	if _, ok := b.guildToolTarget(s); ok {
		t.Fatal("a banked net is withdrawn, not fetched")
	}
	// Level 67 buys in Catherby.
	s = guildSnap(67, fisherplan.Catherby().Bank)
	b.selectFishingArea(&Context{}, &s)
	if _, ok := b.guildToolTarget(s); ok || s.Guild {
		t.Fatal("level 67 must stay at Catherby and buy")
	}
	// A spawn backing off buys in Catherby.
	b.guild.spawnBackoffUntil = time.Now().Add(time.Minute)
	s = guildSnap(70, fisherplan.Catherby().Bank)
	b.selectFishingArea(&Context{}, &s)
	if _, ok := b.guildToolTarget(s); ok || s.Guild {
		t.Fatal("after a failed spawn the account buys in Catherby")
	}
	// Setting off: never.
	b = &fisherBot{guild: readFisherGuild(map[string]interface{}{"fishingGuild": false}, "someone")}
	s = guildSnap(70, fisherplan.FishingGuild().Bank)
	b.selectFishingArea(&Context{}, &s)
	if _, ok := b.guildToolTarget(s); ok {
		t.Fatal("setting off: no spawn")
	}
}
