package randomevents

import (
	"strings"
	"testing"

	"rsb0t-go/internal/doorescape"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// liveClient drives the Guardian's REAL executor (recoverStrand: strandObs, the ladder, the packets) against a
// castleWorld. The Guardian reads a real decoded gamestate.State, so every tick the world's doors are sent to it as
// the LOC_DEL and LOC_ADD_CHANGE packets the server sends, its position as a PLAYER_INFO, and the packets the
// Guardian wrote (MOVE_GAMECLICK, OPLOC) are decoded and fed to the world the way the server's handlers read them.
type liveClient struct {
	t      *testing.T
	sess   *recordingSession
	g      *Guardian
	w      *castleWorld
	logs   []string
	sentTo doorescape.State // the doors as far as the client has been told
	read   int              // packets already fed to the world
	last   doorescape.Tile
}

const (
	liveBaseX = 3240 // the build area's origin: the castle's region is within 104 tiles of it
	liveBaseZ = 3904
)

func newLiveClient(t *testing.T, at doorescape.Tile, s doorescape.State) *liveClient {
	t.Helper()
	sess := newRecordingSession()
	g := NewGuardian(sess, nil)
	t.Cleanup(g.Stop)
	c := &liveClient{t: t, sess: sess, g: g, w: newCastleWorld(t, at, s)}
	g.Log = func(s string) { c.logs = append(c.logs, s) }
	prev := strandClock
	strandClock = c.w.now
	t.Cleanup(func() { strandClock = prev })
	sess.state.Handle(serverprot.REBUILD_NORMAL, payload(func(p *packet.Packet) {
		p.P2(uint16(liveBaseX/8 + 6))
		p.P2(uint16(liveBaseZ/8 + 6))
	}))
	c.last = doorescape.Tile{X: -1}
	// Every door as the map has it, then the world's state on top.
	c.sentTo = c.w.cat.DefaultState
	c.syncDoors()
	c.syncPlayer()
	return c
}

func (c *liveClient) syncPlayer() {
	p := c.w.pos
	if p == c.last {
		return
	}
	c.last = p
	c.sess.state.Handle(serverprot.PLAYER_INFO, bitPack(3,
		bitField{1, 1}, bitField{2, 3}, bitField{2, uint32(p.Level)},
		bitField{7, uint32(p.X - liveBaseX)}, bitField{7, uint32(p.Z - liveBaseZ)},
		bitField{1, 0}, bitField{1, 0},
	))
}

// syncDoors tells the client what changed since it last heard, for the doors on the account's level (a client
// holds only its own level's loc changes).
func (c *liveClient) syncDoors() {
	cat := c.w.cat
	for i, d := range cat.Doors {
		if d.Level != c.w.pos.Level {
			continue
		}
		was, now := c.sentTo, c.w.state
		cl, op := d.Closed(), d.Open()
		if was.ClosedLeaf(i) && !now.ClosedLeaf(i) {
			liveLocChange(c.sess.state, liveBaseX, liveBaseZ, cl.X, cl.Z, -1, cl.Shape, cl.Angle)
		}
		if !was.ClosedLeaf(i) && now.ClosedLeaf(i) {
			liveLocChange(c.sess.state, liveBaseX, liveBaseZ, cl.X, cl.Z, cl.ID, cl.Shape, cl.Angle)
		}
		if !was.OpenLeaf(i) && now.OpenLeaf(i) {
			liveLocChange(c.sess.state, liveBaseX, liveBaseZ, op.X, op.Z, op.ID, op.Shape, op.Angle)
		}
		if was.OpenLeaf(i) && !now.OpenLeaf(i) {
			liveLocChange(c.sess.state, liveBaseX, liveBaseZ, op.X, op.Z, -1, op.Shape, op.Angle)
		}
		c.sentTo = c.sentTo.With(i, now.ClosedLeaf(i), now.OpenLeaf(i))
	}
}

// deliver decodes the packets the Guardian has written since the last call and hands them to the world.
func (c *liveClient) deliver() (walks, ops int) {
	c.sess.mu.Lock()
	defer c.sess.mu.Unlock()
	for ; c.read < len(c.sess.sent); c.read++ {
		b := c.sess.sent[c.read]
		switch {
		case len(b) >= 7 && b[0] == clientprot.MOVE_GAMECLICK:
			n := (int(b[1]) - 3) / 2 // corners, the first of which is the start tile
			sx, sz := int(b[3])<<8|int(b[4]), int(b[5])<<8|int(b[6])
			path := []doorescape.Tile{{X: sx, Z: sz, Level: c.w.pos.Level}}
			for k := 0; k < n-1; k++ {
				path = append(path, doorescape.Tile{X: sx + int(int8(b[7+2*k])), Z: sz + int(int8(b[8+2*k])), Level: c.w.pos.Level})
			}
			c.w.walk(path)
			walks++
		case len(b) >= 7:
			// OPLOCn: opcode, x, z, loc id (any anti-cheat packet in front of it is ignored)
			op := b[len(b)-7]
			n := 0
			switch op {
			case clientprot.OPLOC1:
				n = 1
			case clientprot.OPLOC2:
				n = 2
			case clientprot.OPLOC3:
				n = 3
			}
			if n == 0 {
				c.t.Fatalf("a packet the Guardian sent is neither a walk nor an OPLOC1-3: % x", b)
			}
			x, z, id := int(b[len(b)-6])<<8|int(b[len(b)-5]), int(b[len(b)-4])<<8|int(b[len(b)-3]), int(b[len(b)-2])<<8|int(b[len(b)-1])
			c.w.opLoc(n, x, z, id)
			ops++
		default:
			c.t.Fatalf("a packet shorter than a walk or a loc op: % x", b)
		}
	}
	return
}

// run drives the account until it is out of the compound and the visit has been closed in the log, or until the tick
// cap. It returns the ticks used.
func (c *liveClient) run(max int) int {
	for i := 0; i < max; i++ {
		c.w.Tick()
		c.syncDoors()
		c.syncPlayer()
		c.g.recoverStrand()
		c.deliver()
		if c.w.outside() {
			c.syncPlayer()
			c.g.recoverStrand() // closes the visit: "left Rogues' Castle"
			return i + 1
		}
	}
	return max
}

// runBusy is run that also counts the polls after which the Guardian would have held the scripts (Busy), and stops
// early with the number of polls made when the account has left.
func (c *liveClient) runBusy(max int) (ticks, busy int) {
	for i := 0; i < max; i++ {
		c.w.Tick()
		c.syncDoors()
		c.syncPlayer()
		c.g.recoverStrand()
		c.deliver()
		if c.g.Busy() {
			busy++
		}
		if c.w.outside() {
			c.syncPlayer()
			c.g.recoverStrand() // closes the visit: "left Rogues' Castle"
			return i + 1, busy
		}
	}
	return max, busy
}

func (c *liveClient) joined() string { return strings.Join(c.logs, "\n") }

// From the stairs room, with the real executor: the Guardian reads the doors off real LOC packets, plans, walks the
// account to the main door's east neighbour (not the tile the leaf lands on), opens it, sees the leaf, and walks
// the account out, in about a dozen ticks, with one open click and a log that says so.
func TestRealExecutorOpensTheMainDoorFromTheRightTile(t *testing.T) {
	cat, err := doorescape.Castle()
	if err != nil {
		t.Fatal(err)
	}
	c := newLiveClient(t, doorescape.Tile{X: 3281, Z: 3935, Level: 0}, cat.DefaultState)
	ticks := c.run(120)
	if !c.w.outside() {
		t.Fatalf("not out after %d ticks, at %v:\n%s", ticks, c.w.pos, c.joined())
	}
	log := c.joined()
	for _, want := range []string{"Rogues' Castle", "Open on loc 1530 at 3279,3939 from 3280,3939,0", "strand: left Rogues' Castle"} {
		if !strings.Contains(log, want) {
			t.Errorf("the log lacks %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "from 3279,3938,0") {
		t.Errorf("the door was opened from the tile its leaf lands on:\n%s", log)
	}
	if c.w.opsDone != 1 || c.w.pushes != 0 {
		t.Errorf("%d door ops and %d pushes, want one op and no push", c.w.opsDone, c.w.pushes)
	}
	if c.g.Busy() {
		t.Error("the Guardian is still Busy after the account left")
	}
	t.Logf("out in %d ticks:\n%s", ticks, log)
}

// From the roof, through the real executor: three staircases and the main door, four loc ops and no more, one arrival
// line and one "left" line, the scripts held while the Guardian works and let go when the account is out.
func TestRealExecutorGetsTheRoofOutOfTheCastle(t *testing.T) {
	cat, err := doorescape.Castle()
	if err != nil {
		t.Fatal(err)
	}
	c := newLiveClient(t, doorescape.Tile{X: 3282, Z: 3936, Level: 3}, cat.DefaultState)
	ticks, busy := c.runBusy(400)
	if !c.w.outside() {
		t.Fatalf("not out after %d polls, at %v:\n%s", ticks, c.w.pos, c.joined())
	}
	log := c.joined()
	for _, want := range []string{"on loc 1740 at 3280,3936", "on loc 1739 at 3280,3936", "on loc 1530 at 3279,3939"} {
		if !strings.Contains(log, want) {
			t.Errorf("the log lacks %q:\n%s", want, log)
		}
	}
	if n := strings.Count(log, "on loc 1739 at 3280,3936"); n != 2 {
		t.Errorf("%d clicks on the 1739 staircase, want one on each of the two middle floors", n)
	}
	if n := strings.Count(log, "Planning the way out of the walls"); n != 1 {
		t.Errorf("%d arrival lines, want one", n)
	}
	if n := strings.Count(log, "strand: left Rogues' Castle"); n != 1 {
		t.Errorf("%d \"left\" lines, want one", n)
	}
	if c.w.opsDone != 4 || c.w.closes != 0 || c.w.pushes != 0 || c.w.cantReach != 0 {
		t.Errorf("%d loc ops, %d doors shut, %d pushes, %d refused clicks, want four ops and nothing else", c.w.opsDone, c.w.closes, c.w.pushes, c.w.cantReach)
	}
	if busy == 0 || busy > ticks {
		t.Errorf("the scripts were held for %d of %d polls", busy, ticks)
	}
	if c.g.Busy() {
		t.Error("the Guardian is still Busy after the account left")
	}
	t.Logf("roof to the open ground in %d polls, held for %d:\n%s", ticks, busy, log)
}

// The main door another account opened (its leaf is in the loc changes, the closed leaf is gone): the executor reads
// that off the packets, sends no click on any door, and walks the account round the open leaf, which blocks the
// tile the old fixed stand was on.
func TestRealExecutorWalksOutThroughADoorAnotherAccountOpened(t *testing.T) {
	cat, err := doorescape.Castle()
	if err != nil {
		t.Fatal(err)
	}
	main := doorByName(t, cat, "main door (diagonal)")
	open := cat.DefaultState.With(main, false, true)
	for _, from := range []doorescape.Tile{{X: 3281, Z: 3935, Level: 0}, {X: 3280, Z: 3938, Level: 0}, {X: 3279, Z: 3937, Level: 0}} {
		c := newLiveClient(t, from, open)
		ticks, _ := c.runBusy(200)
		if !c.w.outside() {
			t.Errorf("from %v: not out after %d polls, at %v:\n%s", from, ticks, c.w.pos, c.joined())
			continue
		}
		if c.w.opsDone != 0 || c.w.opClicks != 0 {
			t.Errorf("from %v: %d loc clicks for a door that stands open:\n%s", from, c.w.opClicks, c.joined())
		}
	}
}

// With every door refusing, the executor says STRANDED once (the visit's one line for the operator), holds the scripts
// only while a round is running (not through the sleeps between rounds), and has stopped holding them when it is
// asleep after the last round.
func TestRealExecutorSaysStrandedOnceAndLetsTheScriptsRun(t *testing.T) {
	cat, err := doorescape.Castle()
	if err != nil {
		t.Fatal(err)
	}
	c := newLiveClient(t, doorescape.Tile{X: 3281, Z: 3935, Level: 0}, cat.DefaultState)
	for i := range cat.Doors {
		c.w.locked[i] = true
	}
	ticks, busy := c.runBusy(3500)
	if c.w.outside() {
		t.Fatal("the account got out through doors that never open")
	}
	log := c.joined()
	if n := strings.Count(log, "STRANDED in Rogues' Castle"); n != 1 {
		t.Errorf("%d STRANDED lines in %d polls, want one:\n%s", n, ticks, log)
	}
	if !strings.Contains(log, "trying again every 15m0s") {
		t.Errorf("the STRANDED line does not say the ladder keeps trying:\n%s", log)
	}
	if busy < 50 || busy > ticks/4 {
		t.Errorf("the scripts were held for %d of %d polls: want the rounds only, not the sleeps between them", busy, ticks)
	}
	if c.g.Busy() {
		t.Error("the Guardian holds the scripts while asleep after the last round")
	}
	if c.w.opsDone > 60 {
		t.Errorf("%d loc ops in %d polls of refusals, want a bounded handful", c.w.opsDone, ticks)
	}
}

// On open ground inside the walls the Guardian does not touch the account at first, announces nothing and holds
// nothing; after the stay limit it escorts the account out, announcing once.
func TestRealExecutorLeavesCourtyardAccountsAloneFirst(t *testing.T) {
	cat, err := doorescape.Castle()
	if err != nil {
		t.Fatal(err)
	}
	c := newLiveClient(t, doorescape.Tile{X: 3277, Z: 3937, Level: 0}, cat.DefaultState)
	for i := 0; i < 60; i++ {
		c.w.Tick()
		c.syncDoors()
		c.syncPlayer()
		c.g.recoverStrand()
		c.deliver()
		if c.g.Busy() {
			t.Fatalf("held the scripts at poll %d of an account that is free to walk out by itself", i)
		}
	}
	if len(c.logs) != 0 || c.w.walkClicks != 0 {
		t.Fatalf("acted within the stay limit: %d walk clicks, log:\n%s", c.w.walkClicks, c.joined())
	}
	ticks, _ := c.runBusy(200)
	if !c.w.outside() {
		t.Fatalf("not out after %d more polls, at %v:\n%s", ticks, c.w.pos, c.joined())
	}
	if n := strings.Count(c.joined(), "Planning the way out of the walls"); n != 1 || c.w.opsDone != 0 {
		t.Errorf("%d arrival lines and %d loc ops, want one and none:\n%s", n, c.w.opsDone, c.joined())
	}
}
