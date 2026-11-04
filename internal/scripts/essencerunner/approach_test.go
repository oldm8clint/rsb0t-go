package essencerunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/npcsim"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/tile"
)

// THE GATEWAY CLICK IS SENT FROM BESIDE THE NPC. Live, 13-14 Oct 2026, the Wizards' Guild stretch clicked Wizard Distentor from
// wherever its walk to his STAND left the account (the Guild's door tile; he wanders four tiles from the stand): 37 teleports
// landed in the logged runs and 57 did not, 4 of 4 at 11:24 on 14 Oct. Engine-TS 289 accepts an NPC option only from a tile that
// touches the NPC orthogonally with no wall between (Player.inOperableDistance -> reachedEntity); from anywhere else it tries a
// naive walk toward him, which every wall and solid tile in the Guild's hall stops, and then says "I can't reach that!" -- a line
// nothing read. The tests here run the teleport against npcsim, the STRICT server: an option read from a tile that does not touch
// the NPC does nothing, however near he is. (approach_real_test.go does it on the real Guild, Aubury's shop and Cromperty's.)

func tl(x, z int) tile.Tile { return tile.Tile{X: x, Z: z} }

// taken is the clicks the engine accepted. A click sent while the cast of an earlier one runs is read in the account's delay and
// dropped (teleportReclickTicks is shorter than the seven or eight ticks a teleport takes to be seen), so the account's click count
// is not the engine's: what matters is that exactly one was ever taken, and from beside the NPC.
func taken(clicks []npcsim.Click) (out []npcsim.Click) {
	for _, c := range clicks {
		if c.Reached {
			out = append(out, c)
		}
	}
	return out
}

var guild = essenceplan.GatewayGuild

// THE REGRESSION. The account stands on the Guild's door tile; Distentor, who wanders, is four tiles away. The old code clicked at
// once: the strict server answers "I can't reach that!", the account never moves, and the teleport never lands.
func TestTheTeleportClickIsSentFromBesideTheNpc(t *testing.T) {
	npcAt := tl(2592, 3091)
	r := newRig(t, guild, nil, guildDoor, npcAt)
	res := TeleportInVia(r.d, r.gw)
	if !res.Landed || res.At != mineLand {
		t.Fatalf("the teleport did not land from the door tile with Distentor at %v: %+v\nlog:\n%s", npcAt, res, r.log())
	}
	clicks := r.e.Clicks()
	if got := taken(clicks); len(got) != 1 || clicks[0] != got[0] || res.Clicks < 1 {
		t.Fatalf("clicks %+v (result says %d), want the first one taken and no other\nlog:\n%s", clicks, res.Clicks, r.log())
	}
	if c := clicks[0]; !touches(c.From, c.NPC) {
		t.Errorf("the click was sent from %v with Distentor at %v, which does not touch him", c.From, c.NPC)
	}
	if len(r.e.Walks()) == 0 {
		t.Error("the account never walked: it cannot have stepped beside him")
	}
	if out := r.log(); !strings.Contains(out, "teleport: stepped from 2596,3088 onto ") || !strings.Contains(out, "teleport: click 1 on Wizard Distentor (npc 211 at 2592,3091) from ") || !strings.Contains(out, "beside him") {
		t.Errorf("the step and the click are not in the log:\n%s", out)
	}
}

// The control: the strict engine really does refuse the click the old code sent, and says so. Without it the tests above would pass
// against an engine that takes every click.
func TestTheStrictEngineRefusesTheOldClick(t *testing.T) {
	r := newRig(t, guild, nil, guildDoor, tl(2592, 3091))
	npc, ok := FindGateway(r.d, r.gw)
	if !ok {
		t.Fatal("Distentor not in sight")
	}
	if err := actions.OpNpc(r.sim, r.op, npc.Index); err != nil {
		t.Fatal(err)
	}
	r.e.Step()
	if c := r.e.Clicks(); len(c) != 1 || c[0].Reached {
		t.Fatalf("clicks %+v: the strict engine took a click from %v on Distentor at %d,%d", c, guildDoor, npc.X, npc.Z)
	}
	if !strings.Contains(r.chat.Since(time.Time{}), npcsim.CantReach) {
		t.Errorf("the refusal was not said: %q", r.chat.Since(time.Time{}))
	}
	if !(chatMark{}).cantReach(r.d) {
		t.Error("cantReach does not read the line the engine says")
	}
	if markChat(r.d).cantReach(r.d) {
		t.Error("a line said before the mark answers a click sent after it")
	}
}

// The NPC wanders while the account walks beside him: the step is checked against where he is NOW, and the account steps again
// before it clicks, so the one click is the one the engine takes.
func TestTheNpcWanderingWhileTheAccountWalksIsReadAgain(t *testing.T) {
	r := newRig(t, guild, nil, guildDoor, tl(2592, 3091))
	away := tl(2590, 3094)
	r.onWalk(func(n int) {
		if n == 1 {
			r.e.MoveNPC(rigNpcIndex, away)
		}
	})
	res := TeleportInVia(r.d, r.gw)
	if !res.Landed {
		t.Fatalf("%+v\nlog:\n%s", res, r.log())
	}
	clicks := taken(r.e.Clicks())
	if len(clicks) != 1 || clicks[0].NPC != away {
		t.Fatalf("clicks %+v, want one, taken, on Distentor at %v", r.e.Clicks(), away)
	}
	if len(r.e.Walks()) < 2 {
		t.Errorf("walks %v: the account should have stepped twice", r.e.Walks())
	}
	if !strings.Contains(r.log(), "stepped from") || !strings.Contains(r.log(), "after 2 step(s)") {
		t.Errorf("the steps are not logged:\n%s", r.log())
	}
}

// A diagonal touches him at a corner only: the account on one is not beside him, and steps.
func TestAnAccountOnADiagonalStepsBesideTheNpc(t *testing.T) {
	npcAt := tl(2592, 3091)
	r := newRig(t, guild, nil, tl(2593, 3092), npcAt)
	res := TeleportInVia(r.d, r.gw)
	if !res.Landed {
		t.Fatalf("%+v\nlog:\n%s", res, r.log())
	}
	if c := taken(r.e.Clicks()); len(c) != 1 || !touches(c[0].From, npcAt) {
		t.Errorf("clicks %+v", r.e.Clicks())
	}
	if len(r.e.Walks()) == 0 {
		t.Error("the account stayed on the diagonal and clicked")
	}
	if !strings.Contains(r.log(), "corner to corner or behind a wall, which the engine refuses") {
		t.Errorf("the diagonal is not named in the log:\n%s", r.log())
	}
}

// Already beside him: no walk, one click, and it lands in the ticks the cast takes.
func TestAnAccountAlreadyBesideTheNpcJustClicks(t *testing.T) {
	r := newRig(t, guild, nil, tl(2593, 3091), tl(2592, 3091))
	res := TeleportInVia(r.d, r.gw)
	if !res.Landed || res.At != mineLand {
		t.Fatalf("%+v\nlog:\n%s", res, r.log())
	}
	if len(r.e.Walks()) != 0 || len(taken(r.e.Clicks())) != 1 || !r.e.Clicks()[0].Reached {
		t.Errorf("walked %v, clicks %+v", r.e.Walks(), r.e.Clicks())
	}
	if res.Ticks < 4 || res.Ticks > 9 {
		t.Errorf("landed after %d ticks, the cast holds about 5", res.Ticks)
	}
}

// The game answered the click "I can't reach that!" (he moved as it was in flight): the account steps beside him where he is now
// and clicks again at once -- not four ticks later -- and the second click is taken.
func TestACantReachIsAnsweredWithAStepAndASecondClick(t *testing.T) {
	r := newRig(t, guild, nil, tl(2593, 3091), tl(2592, 3091))
	away := tl(2589, 3093)
	r.onNpcClick(func(n int) {
		if n == 1 {
			r.e.MoveNPC(rigNpcIndex, away)
		}
	})
	res := TeleportInVia(r.d, r.gw)
	if !res.Landed || res.Clicks < 2 {
		t.Fatalf("%+v\nlog:\n%s", res, r.log())
	}
	c := r.e.Clicks()
	if len(c) < 2 || c[0].Reached || !c[1].Reached || c[1].NPC != away || !touches(c[1].From, away) || len(taken(c)) != 1 {
		t.Fatalf("clicks %+v", c)
	}
	for _, later := range c[2:] {
		if !later.Dropped {
			t.Errorf("a click after the taken one was not dropped in the cast: %+v", later)
		}
	}
	// It read the game's answer; it did not wait for the timer (the log says which of the two sent the second click).
	out := r.log()
	if !strings.Contains(out, `click 2 on Wizard Distentor (npc 211 at 2589,3093) from 2590,3093, beside him (again: the game said "I can't reach that!")`) {
		t.Errorf("the second click was not sent on the game's answer:\n%s", out)
	}
	if gap := c[1].Tick - c[0].Tick; gap >= teleportReclickTicks+3 {
		t.Errorf("the second click came %d ticks after the first: it waited for the timer", gap)
	}
}

// A click the server never read (the player was busy for a tick or two): nothing is said, nothing happens, and the account clicks
// again four ticks later, from where it stands -- it is still beside him.
func TestAClickTheServerNeverTookIsSentAgain(t *testing.T) {
	r := newRig(t, guild, nil, tl(2593, 3091), tl(2592, 3091))
	r.e.Delay(3)
	res := TeleportInVia(r.d, r.gw)
	if !res.Landed || res.Clicks < 2 {
		t.Fatalf("%+v\nlog:\n%s", res, r.log())
	}
	c := r.e.Clicks()
	if len(c) < 2 || !c[0].Dropped || !c[1].Reached || len(taken(c)) != 1 {
		t.Fatalf("clicks %+v", c)
	}
	if gap := c[1].Tick - c[0].Tick; gap < teleportReclickTicks {
		t.Errorf("the second click came %d ticks after the first, want at least %d", gap, teleportReclickTicks)
	}
	if !strings.Contains(r.log(), "ticks after the click and nothing landed") || len(r.e.Walks()) != 0 {
		t.Errorf("the reason for the second click is not in the log, or the account walked (%v):\n%s", r.e.Walks(), r.log())
	}
}

// A click dropped in silence while the wizard has wandered off: the account clicks again at the timer WITHOUT walking (a walk sent
// into a cast is dropped, and nothing yet says this click failed), the game answers "I can't reach that!", and only then does the
// account step beside him and click, taken.
func TestAClickDroppedWhileTheNpcWanderedIsRecoveredByTheGamesAnswer(t *testing.T) {
	r := newRig(t, guild, nil, tl(2593, 3091), tl(2592, 3091))
	r.e.Delay(3) // the first click is read while the player is busy: dropped without a word
	away := tl(2589, 3093)
	r.e.After(2, func() { r.e.MoveNPC(rigNpcIndex, away) })
	res := TeleportInVia(r.d, r.gw)
	if !res.Landed || res.Clicks < 3 {
		t.Fatalf("%+v\nlog:\n%s", res, r.log())
	}
	c := r.e.Clicks()
	if len(c) < 3 || !c[0].Dropped || c[1].Reached || c[1].Dropped || !c[2].Reached || !touches(c[2].From, away) || len(taken(c)) != 1 {
		t.Fatalf("clicks %+v", c)
	}
	// The timer click came first and the walk only after the game's answer to it.
	out := r.log()
	timer, cant, step := strings.Index(out, "ticks after the click and nothing landed"), strings.Index(out, `(again: the game said "I can't reach that!")`), strings.Index(out, "teleport: stepped from")
	if timer < 0 || cant < timer || step < timer || step > cant {
		t.Errorf("log order wrong (timer %d, step %d, game said %d):\n%s", timer, step, cant, out)
	}
}

// THE CAST GUARD. The click is taken, the wizard casts and the account is delayed; he wanders two tiles away meanwhile, so at the
// next resend the account no longer touches him. Nothing has said the click failed, so the account must NOT walk (a walk sent into
// a cast is dropped, and the walk's wait would still be running when the teleport lands): it clicks, harmlessly, and lands.
func TestAWalkIsNeverSentIntoACast(t *testing.T) {
	r := newRig(t, guild, nil, tl(2593, 3091), tl(2592, 3091))
	orig := r.e.OnOp
	r.e.OnOp = func(c npcsim.Click) {
		orig(c)
		r.e.After(2, func() { r.e.MoveNPC(rigNpcIndex, tl(2590, 3093)) })
	}
	res := TeleportInVia(r.d, r.gw)
	if !res.Landed || res.At != mineLand {
		t.Fatalf("%+v\nlog:\n%s", res, r.log())
	}
	if len(r.e.Walks()) != 0 {
		t.Errorf("the account walked into the cast: %v\nlog:\n%s", r.e.Walks(), r.log())
	}
	if res.Clicks < 2 || !strings.Contains(r.log(), "not beside him") {
		t.Errorf("the scenario did not happen (a resend with the wizard out of reach): %+v\nlog:\n%s", res, r.log())
	}
}

// A teleport that never lands says why: how many clicks, where the account and the NPC stood, and what the game said. The NPC
// moves out of reach of the account on every click, so every click is refused; the attempt clicks teleportMaxClicks times and
// walks at most teleportApproachTotal times, then waits out the rest.
func TestATeleportThatNeverLandsLogsWhatTheGameSaid(t *testing.T) {
	r := newRig(t, guild, nil, tl(2593, 3091), tl(2592, 3091))
	spots := []tile.Tile{{X: 2589, Z: 3093}, {X: 2591, Z: 3094}, {X: 2590, Z: 3092}, {X: 2594, Z: 3089}, {X: 2587, Z: 3090}}
	r.onNpcClick(func(int) {
		// Gone before the click is read: to a spot well away from wherever the account stands now.
		for _, s := range spots {
			if tile.Chebyshev(r.e.Here(), s) >= 3 {
				r.e.MoveNPC(rigNpcIndex, s)
				return
			}
		}
		t.Error("no spot far enough from the account")
	})
	res := teleportIn(r.d, r.gw, TeleportOptions{}, 400*time.Millisecond)
	if res.Landed || res.Clicks != teleportMaxClicks {
		t.Fatalf("%+v\nlog:\n%s", res, r.log())
	}
	out := r.log()
	for _, want := range []string{
		"did not take us after 8 click(s)",
		`the game said: "I can't reach that!`,
		"the account is on ",
		"Wizard Distentor on ",
		"teleport: click 8 on Wizard Distentor",
		"timed out waiting for the essence mine",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the log lacks %q:\n%s", want, out)
		}
	}
	if got := len(r.e.Clicks()); got != teleportMaxClicks {
		t.Errorf("the engine read %d clicks, want %d", got, teleportMaxClicks)
	}
	if got := len(r.e.Walks()); got > teleportApproachTotal*teleportStandsTried {
		t.Errorf("the account walked %d times, more than the approach budget allows", got)
	}
}

// The caller's own walk (the Guild's door-aware one) is used for the step, to the exact tile, and the plain walk is not.
func TestTheCallersWalkTakesTheStepBesideTheNpc(t *testing.T) {
	npcAt := tl(2592, 3091)
	r := newRig(t, guild, nil, guildDoor, npcAt)
	var calls []tile.Tile
	var radii []int
	opt := TeleportOptions{Walk: func(dest tile.Tile, radius int, timeout time.Duration) bool {
		calls, radii = append(calls, dest), append(radii, radius)
		r.e.Stand(dest)
		return true
	}}
	res := TeleportInWith(r.d, r.gw, opt)
	if !res.Landed || len(taken(r.e.Clicks())) != 1 {
		t.Fatalf("%+v\nlog:\n%s", res, r.log())
	}
	if len(calls) != 1 || radii[0] != 0 || !touches(calls[0], npcAt) {
		t.Errorf("walk calls %v radii %v, want one exact step onto a tile beside %v", calls, radii, npcAt)
	}
	if len(r.e.Walks()) != 0 {
		t.Errorf("the plain walk was used too: %v", r.e.Walks())
	}
}

// A walk of the caller's that fails is not the end of the attempt: the click goes out anyway, as it always did, and says so.
func TestAStepThatCannotBeWalkedStillClicks(t *testing.T) {
	r := newRig(t, guild, nil, guildDoor, tl(2592, 3091))
	walks := 0
	res := teleportIn(r.d, r.gw, TeleportOptions{Walk: func(tile.Tile, int, time.Duration) bool { walks++; return false }}, 400*time.Millisecond)
	// Three stands are tried for each approach, and the attempt makes at most teleportApproachTotal of them: the click is refused
	// from where the account stands, the game says so, and the account tries again, until the budget is spent.
	if walks < teleportStandsTried || walks > teleportApproachTotal*teleportStandsTried {
		t.Errorf("walks tried %d, want %d to %d", walks, teleportStandsTried, teleportApproachTotal*teleportStandsTried)
	}
	if res.Clicks == 0 || len(r.e.Clicks()) == 0 {
		t.Fatalf("no click was sent: %+v\nlog:\n%s", res, r.log())
	}
	if out := r.log(); !strings.Contains(out, "could not walk beside Wizard Distentor") || !strings.Contains(out, "clicking anyway") {
		t.Errorf("the failed step is not logged:\n%s", out)
	}
}

// With no collision pack there is nothing to ask: one click, as before, and nothing dereferences the pack.
func TestWithoutACollisionPackTheClickIsTheOldSingleClick(t *testing.T) {
	r := newRig(t, guild, nil, tl(2593, 3091), tl(2592, 3091))
	r.d.Pack = nil
	res := TeleportInVia(r.d, r.gw)
	if !res.Landed || len(r.e.Walks()) != 0 || len(taken(r.e.Clicks())) != 1 {
		t.Fatalf("%+v walks %v\nlog:\n%s", res, r.e.Walks(), r.log())
	}
	if !strings.Contains(r.log(), "no collision pack to say whether that touches him") {
		t.Errorf("log:\n%s", r.log())
	}
}

// An NPC out of sight is walked toward by the caller's walk to his stand within two tiles; still not seen, the attempt fails and
// says so.
func TestAnNpcNeverInSightFailsWithTheReason(t *testing.T) {
	r := newRig(t, guild, nil, guildDoor, tl(2610, 3100)) // beyond the gateway's leash
	var radii []int
	res := TeleportInWith(r.d, r.gw, TeleportOptions{Walk: func(dest tile.Tile, radius int, timeout time.Duration) bool {
		radii = append(radii, radius)
		return true
	}})
	if res.Landed || res.Clicks != 0 || !strings.Contains(res.Reason, "is not in sight at 2594,3089") {
		t.Fatalf("%+v", res)
	}
	if len(radii) != 1 || radii[0] != 2 {
		t.Errorf("walks toward his stand: radii %v, want one with radius 2", radii)
	}
	// And a walk toward his stand that does not arrive says so.
	r = newRig(t, guild, nil, guildDoor, tl(2610, 3100))
	res = TeleportInWith(r.d, r.gw, TeleportOptions{Walk: func(tile.Tile, int, time.Duration) bool { return false }})
	if res.Landed || !strings.Contains(res.Reason, "is not in sight and the walk to 2594,3089 did not arrive") {
		t.Fatalf("%+v", res)
	}
}

// ---- an NPC with no tile beside him ----

// sealedPocket is a pack in which the tile (2592,3091) has a solid tile on every side.
func sealedPocket(t *testing.T) *pathfind.Pack {
	t.Helper()
	pk, err := npcsim.OpenPackWith(npcsim.Terrain{Solid: []tile.Tile{tl(2593, 3091), tl(2591, 3091), tl(2592, 3092), tl(2592, 3090)}}, [2]int{40, 48})
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

// Sealed in a pocket (every side of him solid or walled), nothing can be stood on that touches him; he is wandering, so the account
// waits for him to move -- and goes on at once when he does.
func TestAnNpcWithNoTileBesideHimIsWaitedFor(t *testing.T) {
	r := newRig(t, guild, sealedPocket(t), guildDoor, tl(2592, 3091))
	r.e.After(6, func() { r.e.MoveNPC(rigNpcIndex, tl(2594, 3094)) })
	res := TeleportInVia(r.d, r.gw)
	if !res.Landed {
		t.Fatalf("%+v\nlog:\n%s", res, r.log())
	}
	if out := r.log(); !strings.Contains(out, "can be stood on (the account is on 2596,3088) - waiting for him to move") {
		t.Errorf("the wait is not logged:\n%s", out)
	}
	if c := r.e.Clicks(); len(c) == 0 || c[0].NPC != tl(2594, 3094) || !c[0].Reached || len(taken(c)) != 1 {
		t.Errorf("clicks %+v, want the first on the wizard where he moved to, taken", c)
	}
}

// He never moves: the attempt fails after teleportNoStandPolls looks, with the reason, and sends no click at all.
func TestAnNpcThatNeverLeavesTheSealedPocketFailsTheAttempt(t *testing.T) {
	r := newRig(t, guild, sealedPocket(t), guildDoor, tl(2592, 3091))
	res := TeleportInVia(r.d, r.gw)
	if res.Landed || res.Clicks != 0 || !strings.Contains(res.Reason, "no tile beside Wizard Distentor at 2592,3091 can be stood on") || len(r.e.Clicks()) != 0 {
		t.Fatalf("%+v clicks %v", res, r.e.Clicks())
	}
}

// ---- the stands ----

func hall(t *testing.T, terrain npcsim.Terrain) *pathfind.Pack {
	t.Helper()
	pk, err := npcsim.OpenPackWith(terrain, [2]int{40, 48})
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

// The four tiles that touch an NPC orthogonally are "beside him"; nothing else is, which is what a distance gets wrong: the
// diagonals are one tile away and his own tile is none.
func TestOnlyTheFourTouchingTilesAreBesideAnNpc(t *testing.T) {
	pk := hall(t, npcsim.Terrain{})
	npc := tl(2592, 3091)
	for _, c := range []struct {
		name string
		here tile.Tile
		want bool
	}{
		{"east", tl(2593, 3091), true}, {"west", tl(2591, 3091), true}, {"north", tl(2592, 3092), true}, {"south", tl(2592, 3090), true},
		{"north east", tl(2593, 3092), false}, {"south west", tl(2591, 3090), false},
		{"two east", tl(2594, 3091), false}, {"his tile", npc, false},
		{"the door", guildDoor, false},
	} {
		if got, _ := ApproachStands(pk, c.here, npc); got != c.want {
			t.Errorf("%s: beside him = %v, want %v", c.name, got, c.want)
		}
	}
	if got, _ := ApproachStands(pk, tile.Tile{X: 2593, Z: 3091, Level: 1}, npc); got {
		t.Error("a tile on another level is beside him")
	}
}

// From a diagonal the nearest ways beside him are the two tiles that touch him and the diagonal also touches: one step each; the
// stands never include his own tile or the tile the account is on.
func TestTheStandsAreTheTouchingTilesNearestByRouteFirst(t *testing.T) {
	pk := hall(t, npcsim.Terrain{})
	npc := tl(2592, 3091)
	reached, stands := ApproachStands(pk, tl(2593, 3092), npc)
	if reached {
		t.Fatal("a diagonal is beside him")
	}
	if len(stands) != 4 {
		t.Fatalf("stands %v, want the four touching tiles", stands)
	}
	if stands[0] != tl(2592, 3092) || stands[1] != tl(2593, 3091) {
		t.Errorf("stands %v: the two nearest (one step each, west of the tie by X) come first", stands)
	}
	for _, s := range stands {
		if s == npc {
			t.Errorf("his own tile is a stand: %v", stands)
		}
		if !touches(s, npc) {
			t.Errorf("%v does not touch him", s)
		}
	}
	// Far away: still the same four, the nearest to the account first.
	_, stands = ApproachStands(pk, guildDoor, npc)
	if len(stands) != 4 || stands[0] != tl(2593, 3091) {
		t.Errorf("from the door: %v, want the east tile (nearest) first of four", stands)
	}
}

// A wall on the side of him stops the click across it, and a solid tile cannot be stood on: neither is a stand.
func TestAWallOrASolidTileIsNotAStand(t *testing.T) {
	npc := tl(2592, 3091)
	pk := hall(t, npcsim.Terrain{
		Walls: []npcsim.Wall{{At: npc, Face: 1}}, // east of him
		Solid: []tile.Tile{tl(2592, 3092)},       // north of him
	})
	reached, stands := ApproachStands(pk, guildDoor, npc)
	if reached {
		t.Fatal("beside him from the door")
	}
	want := map[tile.Tile]bool{tl(2591, 3091): true, tl(2592, 3090): true}
	if len(stands) != len(want) {
		t.Fatalf("stands %v, want only the west and south tiles", stands)
	}
	for _, s := range stands {
		if !want[s] {
			t.Errorf("%v is a stand, but it is behind a wall or solid", s)
		}
	}
	// The east tile is one step from him and not beside him: across the wall is not touching.
	if got, _ := ApproachStands(pk, tl(2593, 3091), npc); got {
		t.Error("the tile on the other side of the wall is beside him")
	}
}

// walledRoom is a 3x3 room around centre with a wall on every outer face: the nearest the static pack comes to a room whose door is
// shut.
func walledRoom(centre tile.Tile) npcsim.Terrain {
	var walls []npcsim.Wall
	for d := -1; d <= 1; d++ {
		walls = append(walls,
			npcsim.Wall{At: tl(centre.X+d, centre.Z+1), Face: 0}, npcsim.Wall{At: tl(centre.X+d, centre.Z-1), Face: 2},
			npcsim.Wall{At: tl(centre.X+1, centre.Z+d), Face: 1}, npcsim.Wall{At: tl(centre.X-1, centre.Z+d), Face: 3})
	}
	return npcsim.Terrain{Walls: walls}
}

// Stands the plain walk cannot get to (a shut door between) are still listed, nearest by straight line first, for a caller whose
// walk opens doors.
func TestStandsBehindAShutDoorAreStillListedNearestFirst(t *testing.T) {
	npc := tl(2592, 3091)
	pk := hall(t, walledRoom(npc))
	reached, stands := ApproachStands(pk, guildDoor, npc)
	if reached || len(stands) != 4 {
		t.Fatalf("reached %v stands %v, want four", reached, stands)
	}
	want := []tile.Tile{tl(2593, 3091), tl(2592, 3090), tl(2592, 3092), tl(2591, 3091)}
	for i, s := range stands {
		if s != want[i] {
			t.Fatalf("stands %v, want %v", stands, want)
		}
		if res := pk.FindPath(pathfind.NavPoint{X: guildDoor.X, Z: guildDoor.Z}, pathfind.NavPoint{X: s.X, Z: s.Z}); res.OK && res.Waypoints[len(res.Waypoints)-1].X == s.X && res.Waypoints[len(res.Waypoints)-1].Z == s.Z {
			t.Errorf("%v can be walked to from the door, the premise of the test is wrong: %+v", s, res)
		}
	}
}

// ... and the caller's walk, which opens the door, takes the nearest of them.
func TestAStandBehindAShutDoorIsWalkedToByTheCallersWalk(t *testing.T) {
	npc := tl(2592, 3091)
	r := newRig(t, guild, hall(t, walledRoom(npc)), guildDoor, npc)
	var calls []tile.Tile
	res := TeleportInWith(r.d, r.gw, TeleportOptions{Walk: func(dest tile.Tile, radius int, timeout time.Duration) bool {
		calls = append(calls, dest)
		r.e.Stand(dest)
		return true
	}})
	if !res.Landed || len(taken(r.e.Clicks())) != 1 || len(calls) != 1 || calls[0] != tl(2593, 3091) {
		t.Fatalf("%+v walks %v\nlog:\n%s", res, calls, r.log())
	}
}
