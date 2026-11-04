package bot

// The yew rung's board and the magic rung's, as ProgressiveWCFletch uses them: where an account on
// either rung chops, what it sees of the players round that camp who are not this cohort's, and when
// it says every camp on its world is too crowded to be worth a seat. The magic board
// (wcfletchplan/magiccamps.go, 12 October 2026) runs exactly the yew board's steps over its own camps.
//
// The camps and the supply model are wcfletchplan/yewcamps.go's, the strangers' count and the
// advice are wcfletchplan/campcrowd.go's. The live loss this is for (build 73ff4228, 5 October
// 2026): all 23 yew-rung accounts at the ladder's one church camp of three yews, 45 bows an
// account an hour against 180 for an account with a tree to itself.
//
// WHAT THIS DOES NOT DO: hop worlds. A hop is a logout and a login, and it interacts with the
// fleet's login pacing, its hop limits and every mule hand-off (a hand-off is a trade, and a
// trade needs both accounts on one world), so the account only SAYS, once, that another world
// would pay better. The owner moves accounts with the homeWorld setting (homeworld.go).

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/wcfletchplan"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// yewLogEvery is the least time between two "players near the trees" lines from one account.
const yewLogEvery = 5 * time.Minute

// yewSeat is one account's side of a camp board, the yew board's or the magic board's (the same
// type for both: each rung has its own field on the bot).
type yewSeat struct {
	// camp is the account's camp, an index into the world's board; have is false while it is not
	// seated (any other rung, or the board's setting off), which means the ladder's own camp.
	camp int
	have bool
	// senseAt and adviseAt are when the account last counted the players round its camp and last
	// judged the board, each at most every wcfletchplan.CrowdSenseEvery: the count is one snapshot
	// of the session's player list, and nothing here runs more often than that.
	senseAt, adviseAt time.Time
	// logged is the count last said in the log, and loggedAt when.
	logged   int
	loggedAt time.Time
	// crowded is this account's clock on "every camp on this world is crowded".
	crowded wcfletchplan.CrowdWatch
}

// boardSide is one rung's camp board as this script uses it: the yew rung's (wcfletchplan/yewcamps.go)
// or the magic rung's (wcfletchplan/magiccamps.go). The two run the same seating, sensing and advice;
// they differ in the board, the bot field that holds the seat and the words the log uses.
type boardSide struct {
	// label names the rung in the log: "yew", "magic".
	label string
	// trees is what the log calls the trees: "yews", "magic trees".
	trees    string
	camps    []wcfletchplan.Camp
	boardFor func(world int) *wcfletchplan.CampBoard
}

var (
	yewSide   = boardSide{label: "yew", trees: "yews", camps: wcfletchplan.YewCamps, boardFor: wcfletchplan.SharedYewBoardFor}
	magicSide = boardSide{label: "magic", trees: "magic trees", camps: wcfletchplan.MagicCamps, boardFor: wcfletchplan.SharedMagicBoardFor}
)

// seatOf is the bot's seat on side's board.
func (b *wcfletchBot) seatOf(side boardSide) *yewSeat {
	if side.label == magicSide.label {
		return &b.magic
	}
	return &b.yew
}

// seatYew takes or keeps this account's seat on the world's yew board and returns the camp. A new
// seat is logged once, with what the supply model expects there and the board's occupancy and
// strangers, so a live log can say where the yew cohort went and why. While seated it also counts
// the players round the camp and judges the board (senseYew).
func (b *wcfletchBot) seatYew(ctx *Context, d *woodcutrunner.Deps) wcfletchplan.Camp {
	return b.seatSide(ctx, d, yewSide)
}

// seatMagic is seatYew for the magic rung's board: the ladder's three trees west of the Seers bank and
// the four in the Sorcerers' Tower garden (wcfletchplan/magiccamps.go). Its new-seat line names the
// camp's bank and walk and the camp's capacity (the accounts its trees feed at full speed).
func (b *wcfletchBot) seatMagic(ctx *Context, d *woodcutrunner.Deps) wcfletchplan.Camp {
	return b.seatSide(ctx, d, magicSide)
}

func (b *wcfletchBot) seatSide(ctx *Context, d *woodcutrunner.Deps, side boardSide) wcfletchplan.Camp {
	board := side.boardFor(ctx.World())
	seat := b.seatOf(side)
	here, haveHere := woodcutplan.Seers, false
	if x, z, level, ok := d.Session.State().Tile(); ok {
		here, haveHere = tile.Tile{X: x, Z: z, Level: level}, true
	}
	now := time.Now()
	// Neither board overflows: every camp on each is its rung's own.
	st := board.Seat(ctx.Username, here, false, now)
	camps := board.Camps()
	c := camps[st.Camp]
	if st.Moved || !seat.have || seat.camp != st.Camp {
		others := ""
		if st.Crowd > 0 {
			others = fmt.Sprintf(" and %d other player(s) round its trees", st.Crowd)
		}
		desc := wcfletchplan.DescribeCampsWithCrowd(camps, board.Occupancy(now), board.Crowds(now))
		if side.label == magicSide.label {
			capacity := 1
			if ps := board.Prices(); st.Camp < len(ps) {
				capacity = ps[st.Camp].Capacity()
			}
			ctx.logf("wcfletch: magic camp %s (%d trees, bank %s %d steps): seated %d of capacity %d on world %d%s, about %.0f xp/h each by the supply model — board: %s",
				c.Name, c.Trees, c.BankOf().Name, c.Steps, st.Occupants, capacity, ctx.World(), others, st.Share, desc)
		} else {
			ctx.logf("wcfletch: yew rung camp: %s (%d yew trees, %d account(s) seated there%s, about %.0f xp/h each by the supply model) — board: %s",
				c.Name, c.Trees, st.Occupants, others, st.Share, desc)
		}
		// A new camp is looked at as soon as the account is standing in it.
		seat.senseAt, seat.logged, seat.loggedAt = time.Time{}, 0, time.Time{}
	}
	seat.camp, seat.have = st.Camp, true
	b.senseCamp(ctx, d, side, board, st.Camp, c, here, haveHere, now)
	b.adviseCamp(ctx, side, board, now)
	return c
}

// leaveYew gives this account's yew-board seat back, so the board counts it only while it is on the
// yew rung.
func (b *wcfletchBot) leaveYew(ctx *Context) { b.leaveSide(ctx, yewSide) }

// leaveMagic gives this account's magic-board seat back, so the board counts it only while it is on
// the magic rung.
func (b *wcfletchBot) leaveMagic(ctx *Context) { b.leaveSide(ctx, magicSide) }

func (b *wcfletchBot) leaveSide(ctx *Context, side boardSide) {
	seat := b.seatOf(side)
	if !seat.have {
		return
	}
	side.boardFor(ctx.World()).Leave(ctx.Username)
	*seat = yewSeat{}
}

// senseYew tells the yew board how many players that are not this cohort's stand round the camp the
// account is seated at, every wcfletchplan.CrowdSenseEvery, while the account is standing in it.
func (b *wcfletchBot) senseYew(ctx *Context, d *woodcutrunner.Deps, board *wcfletchplan.CampBoard, idx int, c wcfletchplan.Camp, here tile.Tile, haveHere bool, now time.Time) {
	b.senseCamp(ctx, d, yewSide, board, idx, c, here, haveHere, now)
}

// senseCamp is senseYew for either board.
func (b *wcfletchBot) senseCamp(ctx *Context, d *woodcutrunner.Deps, side boardSide, board *wcfletchplan.CampBoard, idx int, c wcfletchplan.Camp, here tile.Tile, haveHere bool, now time.Time) {
	seat := b.seatOf(side)
	if !haveHere || !woodcutplan.AtCamp(here, c.Tier()) || now.Sub(seat.senseAt) < wcfletchplan.CrowdSenseEvery {
		return
	}
	seat.senseAt = now
	others := crowdAround(d.Session.State().Players(), c, ctx.Username)
	board.ObserveCrowd(idx, others, now)
	switch {
	case others == 0:
		seat.logged = 0
	case others != seat.logged && (seat.loggedAt.IsZero() || now.Sub(seat.loggedAt) >= yewLogEvery):
		seat.logged, seat.loggedAt = others, now
		ctx.logf("wcfletch: %s camp %s: %d player(s) round the trees that are not this cohort's (strangers, or another script's accounts) — the board counts each as one more account there when it seats the %s cohort",
			side.label, c.Name, others, side.label)
	}
}

// crowdAround counts the players in the camp's leash on its level who are not this account and
// not one of this cohort (wcfletchplan.IsOwn). A player whose name has not arrived yet is left out:
// it is as likely to be ours as not, and it will have a name by the next count.
func crowdAround(players []gamestate.Player, c wcfletchplan.Camp, self string) int {
	n := 0
	for _, p := range players {
		if p.Name == "" || p.Level != c.Anchor.Level {
			continue
		}
		if tile.Chebyshev(tile.Tile{X: p.X, Z: p.Z, Level: p.Level}, c.Anchor) > c.Leash {
			continue
		}
		if actions.SameUsername(p.Name, self) || wcfletchplan.IsOwn(p.Name) {
			continue
		}
		n++
	}
	return n
}

// adviseYew says, once the whole yew board has been too crowded for wcfletchplan.CrowdAdviceAfter
// and then no more than every wcfletchplan.CrowdAdviceEvery, that another world would pay better
// and which. It hops nobody.
func (b *wcfletchBot) adviseYew(ctx *Context, board *wcfletchplan.CampBoard, now time.Time) {
	b.adviseCamp(ctx, yewSide, board, now)
}

// adviseCamp is adviseYew for either board.
func (b *wcfletchBot) adviseCamp(ctx *Context, side boardSide, board *wcfletchplan.CampBoard, now time.Time) {
	seat := b.seatOf(side)
	if now.Sub(seat.adviseAt) < wcfletchplan.CrowdSenseEvery {
		return
	}
	seat.adviseAt = now
	st := board.Standing(now)
	if !seat.crowded.Step(st.Crowded(), now) {
		return
	}
	adv := wcfletchplan.AdviseWorld(ctx.World(), wcfletchplan.AdviceWorlds, side.boardFor, now)
	ctx.logf("wcfletch: %s camps crowded on world %d (the best seat left would earn %.0f%% of what one account alone gets; %s); %s",
		side.label, ctx.World(), 100*st.Ratio,
		wcfletchplan.DescribeCampsWithCrowd(board.Camps(), board.Occupancy(now), board.Crowds(now)), adv.Describe())
}

// yewStopNote is the stop line's note on where the account was seated on the yew board, "" when it
// was not.
func (b *wcfletchBot) yewStopNote(ctx *Context) string { return b.stopNote(ctx, yewSide) }

// stopNote is the stop line's note on where the account was seated on side's board, "" when it was not.
func (b *wcfletchBot) stopNote(ctx *Context, side boardSide) string {
	seat := b.seatOf(side)
	if !seat.have {
		return ""
	}
	if cs := side.boardFor(ctx.World()).Camps(); seat.camp >= 0 && seat.camp < len(cs) {
		return ", " + side.label + " rung at " + cs[seat.camp].Name
	}
	return ""
}

// seatedCamp is the camp the account is seated at on the yew or the magic board, and whether it is.
func (b *wcfletchBot) seatedCamp(ctx *Context) (wcfletchplan.Camp, bool) {
	for _, side := range []boardSide{yewSide, magicSide} {
		seat := b.seatOf(side)
		if !seat.have {
			continue
		}
		if cs := side.boardFor(ctx.World()).Camps(); seat.camp >= 0 && seat.camp < len(cs) {
			return cs[seat.camp], true
		}
	}
	return wcfletchplan.Camp{}, false
}
