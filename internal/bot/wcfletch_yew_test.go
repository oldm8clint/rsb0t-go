package bot

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/wcfletchplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// church is the yew board's ladder camp; the counts below are made against it.
var church = wcfletchplan.YewCamps[0]

// yewTestRun makes names unique to one run, since the registry of our own names is process-wide and
// a second pass (-count 2) must not find the first one's. A player name is a base-37 hash of at
// most twelve letters and digits, so the number stays short.
var yewTestRun atomic.Int64

func yewTestTag() string { return fmt.Sprint(10000 + yewTestRun.Add(1)%90000) }

func playerAt(name string, dx, dz int) gamestate.Player {
	return gamestate.Player{Index: 1, Name: name, X: church.Anchor.X + dx, Z: church.Anchor.Z + dz, Level: church.Anchor.Level}
}

// TestCrowdAroundCountsOnlyWhoIsNotOurs is the sensing rule: players inside the camp's leash on its
// level, not this account, not one of the cohort, and with a name.
func TestCrowdAroundCountsOnlyWhoIsNotOurs(t *testing.T) {
	tag := yewTestTag()
	mate := "Yewmate_" + tag
	wcfletchplan.NoteOwn(mate)

	upstairs := playerAt("Upstairs "+tag, 1, 1)
	upstairs.Level = 1
	players := []gamestate.Player{
		playerAt("Stranger One "+tag, 0, 0),                        // at the anchor: counts
		playerAt("Stranger Two "+tag, church.Leash, -church.Leash), // on the edge of the leash: counts
		playerAt("Stranger Three "+tag, church.Leash+1, 0),         // one tile past it: does not
		playerAt("Stranger Far "+tag, 40, 40),                      // nowhere near
		upstairs,                                                   // another level
		playerAt(strings.ReplaceAll(mate, "_", " "), 2, 2),         // the cohort, as PLAYER_INFO spells it
		playerAt("Me_"+tag, 1, 0),                                  // this account
		playerAt("", 1, 1),                                         // no appearance yet: could be anyone
	}
	if got := crowdAround(players, church, "me_"+tag); got != 2 {
		t.Errorf("crowdAround = %d, want 2 (the strangers at the anchor and on the edge of the leash)", got)
	}
	if got := crowdAround(nil, church, "x"); got != 0 {
		t.Errorf("an empty list counted %d", got)
	}
	// Another camp's leash is its own: the Catherby yews' anchor is not the church's.
	if got := crowdAround(players, wcfletchplan.YewCamps[1], "me_"+tag); got != 0 {
		t.Errorf("the church's strangers counted at the Catherby yews: %d", got)
	}
}

// yewRig is a bot, a context and a session standing somewhere. Each test gives it a world of its
// own, so the process-wide boards its seats land on are its alone.
type yewRig struct {
	b     *wcfletchBot
	ctx   *Context
	d     *woodcutrunner.Deps
	sim   *actionsim.Session
	lines *[]string
}

func newYewRig(world int, who string, standAt tile.Tile) *yewRig {
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.StandAt(standAt.X, standAt.Z)
	var lines []string
	ctx := &Context{Username: who, CurrentWorld: world, Log: func(s string) { lines = append(lines, s) }}
	b := &wcfletchBot{cfg: wcfletchplan.DefaultSettings()}
	return &yewRig{b: b, ctx: ctx, d: &woodcutrunner.Deps{Session: sim}, sim: sim, lines: &lines}
}

func (r *yewRig) said(sub string) bool {
	for _, l := range *r.lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// THE SENSING, END TO END on a real gamestate: an account standing in its yew camp with three
// players in view, one of them its own cohort's, tells the board there are two strangers. Standing
// outside the camp it tells it nothing, and the look it did not take does not spend its clock.
func TestSeatYewCountsTheStrangersTheSessionSees(t *testing.T) {
	tag := yewTestTag()
	mate := "Mate" + tag
	wcfletchplan.NoteOwn(mate)
	const world = 94
	r := newYewRig(world, "Me"+tag, church.Anchor)
	r.sim.ShowPlayers("Wand"+tag, mate, "Pass"+tag)
	board := wcfletchplan.SharedYewBoardFor(world)

	camp := r.b.seatYew(r.ctx, r.d)
	if camp.Name != church.Name {
		t.Fatalf("an empty board seats the first account at the ladder's own camp, got %q", camp.Name)
	}
	if got := board.Crowd(r.b.yew.camp, time.Now()); got != 2 {
		t.Errorf("the board counts %d strangers at the camp, want 2 (three players in view, one of them ours)", got)
	}
	if !r.said("yew rung camp: "+church.Name) || !r.said("2 player(s) round the trees") {
		t.Errorf("the log should name the seat and the strangers: %q", *r.lines)
	}
	if !r.b.yew.have || r.b.yew.senseAt.IsZero() {
		t.Errorf("seat state %+v", r.b.yew)
	}
	r.b.leaveYew(r.ctx)

	// Outside the camp (the Seers bank, 16 tiles from the church anchor) it senses nothing.
	far := newYewRig(world+100, "Far"+tag, tile.Tile{X: 2725, Z: 3491})
	far.sim.ShowPlayers("Wand"+tag, "Pass"+tag)
	far.b.seatYew(far.ctx, far.d)
	if got := wcfletchplan.SharedYewBoardFor(world+100).Crowd(far.b.yew.camp, time.Now()); got != 0 {
		t.Errorf("an account at the bank counted %d strangers at a camp it is not in", got)
	}
	if !far.b.yew.senseAt.IsZero() {
		t.Error("a look that did not happen spent the clock")
	}
	far.b.leaveYew(far.ctx)
}

// The count is throttled: one snapshot of the player list per CrowdSenseEvery, however often the
// Loop passes.
func TestSenseYewIsThrottled(t *testing.T) {
	tag := yewTestTag()
	r := newYewRig(95, "Thr"+tag, church.Anchor)
	board := wcfletchplan.SharedYewBoardFor(95)
	r.sim.ShowPlayers("Wand" + tag)
	camp := r.b.seatYew(r.ctx, r.d)
	idx := r.b.yew.camp
	first := r.b.yew.senseAt
	if first.IsZero() || board.Crowd(idx, time.Now()) != 1 {
		t.Fatalf("the first pass should have looked: senseAt %v, crowd %d", first, board.Crowd(idx, time.Now()))
	}
	here := tile.Tile{X: church.Anchor.X, Z: church.Anchor.Z}
	r.b.senseYew(r.ctx, r.d, board, idx, camp, here, true, first.Add(wcfletchplan.CrowdSenseEvery-time.Second))
	if !r.b.yew.senseAt.Equal(first) {
		t.Error("looked again inside CrowdSenseEvery")
	}
	later := first.Add(wcfletchplan.CrowdSenseEvery)
	r.b.senseYew(r.ctx, r.d, board, idx, camp, here, true, later)
	if !r.b.yew.senseAt.Equal(later) {
		t.Error("did not look after CrowdSenseEvery")
	}
	// And not at all with no tile known yet.
	r.b.senseYew(r.ctx, r.d, board, idx, camp, here, false, later.Add(time.Hour))
	if !r.b.yew.senseAt.Equal(later) {
		t.Error("looked with no tile")
	}
	r.b.leaveYew(r.ctx)
}

// seatBoards is the Loop's wiring: the yew rung is seated on the yew board (and only with its
// setting on), the maple rung on the maple board, the magic rung on the magic board, any other rung
// on none, and moving from one to another gives the old seat back. A yew or magic camp is its rung's
// own, so the rung that comes back is the rung that went in.
func TestSeatBoardsSeatsEachRungOnItsOwnBoard(t *testing.T) {
	const world = 96
	tag := yewTestTag()
	r := newYewRig(world, "Boards"+tag, church.Anchor)
	yewBoard, mapleBoard, magicBoard := wcfletchplan.SharedYewBoardFor(world), wcfletchplan.SharedCampBoardFor(world), wcfletchplan.SharedMagicBoardFor(world)
	seats := func() (yew, maple int) {
		now := time.Now()
		return sum(yewBoard.Occupancy(now)), sum(mapleBoard.Occupancy(now))
	}
	magicSeats := func() int { return sum(magicBoard.Occupancy(time.Now())) }
	oak, _ := wcfletchplan.RungIndexForLogs(wcfletchplan.OakLogsID)

	camp, seated, rung := r.b.seatBoards(r.ctx, r.d, wcfletchplan.YewRung, 67)
	if !seated || rung != wcfletchplan.YewRung || camp.Rung != wcfletchplan.YewRung || camp.Loc != "Yew" {
		t.Fatalf("yew rung: camp %+v seated %v rung %d", camp, seated, rung)
	}
	if y, m := seats(); y != 1 || m != 0 || !r.b.yew.have || r.b.haveCamp {
		t.Fatalf("after a yew pass: yew seats %d, maple seats %d, %+v", y, m, r.b.yew)
	}

	// The account reaches the magic rung: it is seated on the magic board and the yew seat goes.
	camp, seated, rung = r.b.seatBoards(r.ctx, r.d, wcfletchplan.MagicRung, 82)
	if !seated || rung != wcfletchplan.MagicRung || camp.Rung != wcfletchplan.MagicRung || camp.Loc != "Magic tree" {
		t.Fatalf("magic rung: camp %+v seated %v rung %d", camp, seated, rung)
	}
	if y, m := seats(); y != 0 || m != 0 || r.b.yew.have || !r.b.magic.have || magicSeats() != 1 {
		t.Errorf("after a magic pass: yew seats %d, maple seats %d, magic seats %d, %+v", y, m, magicSeats(), r.b.magic)
	}
	// A rung with no board (the oaks) is seated nowhere, and the magic seat goes.
	if _, seated, rung := r.b.seatBoards(r.ctx, r.d, oak, 40); seated || rung != oak {
		t.Errorf("the oak rung is seated nowhere: seated %v rung %d", seated, rung)
	}
	if y, m := seats(); y != 0 || m != 0 || r.b.yew.have || r.b.magic.have || magicSeats() != 0 {
		t.Errorf("a rung with no board kept a seat: yew %d, maple %d, magic %d", y, m, magicSeats())
	}
	// spreadMagic off: the magic rung is not seated.
	r.b.cfg.SpreadMagic = false
	if _, seated, _ := r.b.seatBoards(r.ctx, r.d, wcfletchplan.MagicRung, 82); seated || magicSeats() != 0 {
		t.Errorf("spreadMagic off, yet seated=%v (%d magic seats)", seated, magicSeats())
	}
	r.b.cfg.SpreadMagic = true

	// The maple rung seats on the maple board and the yew seat is given back.
	r.b.seatBoards(r.ctx, r.d, wcfletchplan.YewRung, 67)
	camp, seated, rung = r.b.seatBoards(r.ctx, r.d, wcfletchplan.MapleRung, 55)
	if !seated || camp.Rung != rung || (rung != wcfletchplan.MapleRung && rung != wcfletchplan.WillowRung) {
		t.Fatalf("maple rung: camp %+v seated %v rung %d", camp, seated, rung)
	}
	if y, m := seats(); y != 0 || m != 1 || r.b.yew.have || !r.b.haveCamp {
		t.Errorf("after a maple pass: yew seats %d, maple seats %d, %+v", y, m, r.b.yew)
	}

	// spreadYew off: the yew rung is not seated, and a seat it held is given back.
	r.b.seatBoards(r.ctx, r.d, wcfletchplan.YewRung, 67)
	if y, _ := seats(); y != 1 {
		t.Fatalf("the yew rung did not seat: %d", y)
	}
	r.b.cfg.SpreadYew = false
	if _, seated, rung := r.b.seatBoards(r.ctx, r.d, wcfletchplan.YewRung, 67); seated || rung != wcfletchplan.YewRung {
		t.Errorf("spreadYew off, yet seated=%v rung=%d", seated, rung)
	}
	if y, m := seats(); y != 0 || m != 0 || r.b.yew.have {
		t.Errorf("spreadYew off kept a seat: yew %d, maple %d", y, m)
	}
	// ...and spreadMaple off does the same for the maple rung, exactly as before this board.
	r.b.cfg.SpreadMaple = false
	if _, seated, _ := r.b.seatBoards(r.ctx, r.d, wcfletchplan.MapleRung, 55); seated {
		t.Error("spreadMaple off, yet seated")
	}
}

// leaveYew gives the seat back and forgets the account's clocks, and does nothing twice.
func TestLeaveYewGivesTheSeatBack(t *testing.T) {
	const world = 97
	tag := yewTestTag()
	r := newYewRig(world, "Leaver"+tag, church.Anchor)
	board := wcfletchplan.SharedYewBoardFor(world)
	r.b.seatYew(r.ctx, r.d)
	if got := board.Occupancy(time.Now()); sum(got) != 1 {
		t.Fatalf("occupancy %v after one seat", got)
	}
	if note := r.b.yewStopNote(r.ctx); !strings.HasPrefix(note, ", yew rung at ") || !strings.Contains(note, wcfletchplan.YewCamps[r.b.yew.camp].Name) {
		t.Errorf("stop note %q", note)
	}
	r.b.yew.logged = 3
	r.b.leaveYew(r.ctx)
	if sum(board.Occupancy(time.Now())) != 0 || r.b.yew.have || r.b.yew.logged != 0 {
		t.Errorf("after leaveYew: occupancy %v, yew %+v", board.Occupancy(time.Now()), r.b.yew)
	}
	r.b.leaveYew(r.ctx) // idempotent, and cheap when not seated
	if note := r.b.yewStopNote(r.ctx); note != "" {
		t.Errorf("stop note for an unseated account: %q", note)
	}
}

// adviseYew says nothing on a board that is not crowded, and on a crowded one waits ten minutes,
// says it once with the other worlds' standing, and is quiet again for half an hour. It never asks
// for a hop: the Context here has no HopWorld at all.
func TestAdviseYewSaysSoOncePerHalfHourAndNeverHops(t *testing.T) {
	const world = 98
	tag := yewTestTag()
	r := newYewRig(world, "Advisee"+tag, church.Anchor)
	board := wcfletchplan.SharedYewBoardFor(world)
	r.b.seatYew(r.ctx, r.d)
	now := time.Now()
	step := func(at time.Time) { r.b.adviseYew(r.ctx, board, at) }
	adviceLines := func() (n int) {
		for _, l := range *r.lines {
			if strings.Contains(l, "yew camps crowded") {
				n++
			}
		}
		return
	}

	step(now.Add(time.Minute))
	step(now.Add(time.Minute + 20*time.Second))
	if adviceLines() != 0 {
		t.Fatalf("an empty board was called crowded: %v", *r.lines)
	}

	// Strangers at every camp, as an account at each would see them, refreshed as they would be.
	crowd := func(at time.Time) {
		for i := range wcfletchplan.YewCamps {
			board.ObserveCrowd(i, 40, at)
		}
	}
	start := now.Add(2 * time.Minute)
	for at := start; at.Before(start.Add(wcfletchplan.CrowdAdviceAfter)); at = at.Add(wcfletchplan.CrowdSenseEvery) {
		crowd(at)
		step(at)
	}
	if adviceLines() != 0 {
		t.Fatalf("said so before the ten minutes were up: %v", *r.lines)
	}
	at := start.Add(wcfletchplan.CrowdAdviceAfter + time.Second)
	crowd(at)
	step(at)
	if adviceLines() != 1 {
		t.Fatalf("want one advice line after ten crowded minutes, have %d: %v", adviceLines(), *r.lines)
	}
	var line string
	for _, l := range *r.lines {
		if strings.Contains(l, "yew camps crowded") {
			line = l
		}
	}
	for _, want := range []string{"wcfletch: yew camps crowded on world 98", "consider homeWorld", "none of ours", "Seers church yews", "+40 others", "% of what one account alone gets"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q is missing from %q", want, line)
		}
	}
	// Quiet for half an hour, even with the board as crowded as ever.
	for q := at.Add(time.Minute); q.Before(at.Add(wcfletchplan.CrowdAdviceEvery)); q = q.Add(time.Minute) {
		crowd(q)
		step(q)
	}
	if adviceLines() != 1 {
		t.Fatalf("said so again inside the half hour: %v", *r.lines)
	}
	crowd(at.Add(wcfletchplan.CrowdAdviceEvery))
	step(at.Add(wcfletchplan.CrowdAdviceEvery))
	if adviceLines() != 2 {
		t.Errorf("did not say so again after the half hour: %v", *r.lines)
	}
	// Throttled: a second call inside CrowdSenseEvery is a no-op, whatever the board says.
	n := adviceLines()
	step(at.Add(wcfletchplan.CrowdAdviceEvery + time.Second))
	if adviceLines() != n {
		t.Error("adviseYew ran twice inside CrowdSenseEvery")
	}
	r.b.leaveYew(r.ctx)
}

func sum(xs []int) int {
	n := 0
	for _, x := range xs {
		n += x
	}
	return n
}
