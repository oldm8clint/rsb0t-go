package randomevents

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// THE 18 OCT 2026 DEATH AT THE FISHING GUILD BANK, REPLAYED (plantfisher-1018). Log run/logs/fleet-20261018-125624-677-31320.log,
// a ProgressiveFisher with 24 hitpoints banking at the booth 2585,3418 from the stand tile 2586,3418, a Strange plant (npc 10015) on
// the other stand tile 2586,3419:
//
//	20:01:51 the Guardian takes up the plant ("walking to 2586,3419", "no plant reply yet" every 4 s, about 20 picks, no reply)
//	20:03:04 the fisher's bank code, a 30 s wait behind it, clicks the booth with the plant pickable no longer
//	20:03:06 the plant has turned (npc 10015 type 408, seen 97 ticks): evade, leash walk to 2592,3415, six steps
//	20:03:09 .. 20:03:35 eight legs, every one "moved 0 tile(s)"; ten "eating Kebab" clicks; HP 24 .. 0
//	20:03:36 DEATH at 2586,3422 killed by poison, food left 1
//
// Nothing the account sent was acted on for minutes (the diagnosis in the commit message): the replay is of what the BOT does with a
// session that swallows every click. The collision pack is the real map (RSB0T_COLLISION_PACK) and the cache config the real names
// (RSB0T_CONFIG_JAG); both skip when unset.

const (
	incidentPlant  = 10015
	incidentSpawnX = 2586
	incidentSpawnZ = 3419
	incidentStandX = 2586
	incidentStandZ = 3418
)

// incidentScene is the account on the stand tile with the plant beside it, a session that swallows every click, and a Guardian.
type incidentScene struct {
	s    *payloadSession
	g    *Guardian
	pack *pathfind.Pack
	log  *lineLog
}

type lineLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *lineLog) add(m string) {
	l.mu.Lock()
	l.lines = append(l.lines, m)
	l.mu.Unlock()
}

func (l *lineLog) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func (l *lineLog) has(sub string) bool { return strings.Contains(l.text(), sub) }

// newIncidentScene puts the account at the stand tile with the plant of the given type and age, with the view current (ticks fed this
// fast would read as a backlog) and the clocks of the handlers shortened to test speed.
func newIncidentScene(t *testing.T, plantType, age int) *incidentScene {
	t.Helper()
	return newIncidentSceneAt(t, plantType, age, incidentStandX, incidentStandZ)
}

// newIncidentSceneAt is newIncidentScene with the account at x,z instead of the stand tile.
func newIncidentSceneAt(t *testing.T, plantType, age, x, z int) *incidentScene {
	t.Helper()
	pack := realCollisionPack(t)
	withResolver(t, realNpcConfig(t))
	st := overlapState()
	setExactTile(st, x, z, 0)
	playSound(st, gamestate.SoundTriffidAppear) // the plant is this account's own
	addFacingNPCs(st, facingNPC{npcSpec{incidentPlant, plantType, incidentSpawnX - x, incidentSpawnZ - z}, playerFaceBase + st.Self()})
	advanceTicks(st, age)
	st.OverrideViewLag(&gamestate.ViewLag{})

	prevPoll, prevLeg, prevDeadline, prevReply, prevBudget, prevEat := fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline, plantReplyBase, plantLeaveWalkBudget, fleeEatConfirmWait
	fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline = 20*time.Millisecond, 120*time.Millisecond, 1500*time.Millisecond
	plantReplyBase, plantLeaveWalkBudget, fleeEatConfirmWait = 150*time.Millisecond, 500*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() {
		fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline = prevPoll, prevLeg, prevDeadline
		plantReplyBase, plantLeaveWalkBudget, fleeEatConfirmWait = prevReply, prevBudget, prevEat
	})

	s := &payloadSession{state: st}
	g := NewGuardian(s, pack)
	g.Chat = chatlog.New()
	lg := &lineLog{}
	g.Log = lg.add
	t.Cleanup(g.Stop)
	t.Cleanup(func() {
		lastResortCasts.Delete(s)
		recentCombat.Delete(s)
		leashSpawns.Delete(s)
		fleeNoEscapes.Delete(s)
	})
	return &incidentScene{s: s, g: g, pack: pack, log: lg}
}

// lastWalkTarget is the last corner of the last walk the session was sent.
func lastWalkTarget(s *payloadSession) (x, z int, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.sent) - 1; i >= 0; i-- {
		b := s.sent[i]
		if b[0] != clientprot.MOVE_GAMECLICK || len(b) < 7 {
			continue
		}
		x, z = int(b[3])<<8|int(b[4]), int(b[5])<<8|int(b[6])
		if n := (len(b) - 7) / 2; n > 0 {
			x += int(int8(b[7+2*(n-1)]))
			z += int(int8(b[8+2*(n-1)]))
		}
		return x, z, true
	}
	return 0, 0, false
}

func (s *payloadSession) count(op byte) int {
	n := 0
	for _, o := range s.ops() {
		if o == op {
			n++
		}
	}
	return n
}

// The pick that cannot land is not clicked on until the plant turns: an unpicked plant at 70 ticks is left, by the six-step walk the
// evade would have taken a plant later, before it can be hit, and the account stays out, with its script held, until the plant is gone.
func TestIncidentAnUnpickedPlantIsLeftBeforeItTurns(t *testing.T) {
	sc := newIncidentScene(t, 407, 70)
	sc.g.kick()

	if n := sc.s.count(clientprot.OPNPC1); n != 0 {
		t.Fatalf("%d pick click(s) went out at 70 ticks, 20 before the plant turns:\n%s", n, sc.log.text())
	}
	x, z, ok := lastWalkTarget(sc.s)
	if !ok {
		t.Fatalf("no walk went out:\n%s", sc.log.text())
	}
	if d := chebyshev(x, z, incidentSpawnX, incidentSpawnZ); d < 5 {
		t.Fatalf("the walk ends at %d,%d, %d tiles from the plant's tile: inside its leash", x, z, d)
	}
	if !sc.log.has("leaving its leash") {
		t.Fatalf("no line says why the account left:\n%s", sc.log.text())
	}
	if !sc.g.plants.hasLeft(incidentPlant) {
		t.Fatal("the Guardian does not know it left")
	}
	if !sc.g.Busy() || !sc.g.inputHeld() {
		t.Fatal("the account's script must be held (Busy and its input) while the plant lives")
	}

	// The fisher's bank code, one second later: its booth click and its walk to the next booth are dropped.
	before := len(sc.s.ops())
	fromScript(func() { scriptClicks(sc.s) })
	if got := len(sc.s.ops()); got != before {
		t.Fatalf("the script's clicks went out while the account was waiting out the plant: %v", sc.s.ops()[before:])
	}

	// The Guardian does not walk back to the plant to ask it again.
	sc.g.mu.Lock()
	for sig := range sc.g.retryUntil {
		sc.g.retryUntil[sig] = time.Time{}
	}
	sc.g.mu.Unlock()
	sc.g.kick()
	if sc.s.count(clientprot.OPNPC1) != 0 {
		t.Fatalf("the Guardian clicked the plant it had left:\n%s", sc.log.text())
	}

	// The plant is gone (it turned and found nobody in reach, or was deleted): the script is let go.
	addNPCs(sc.s.state)
	if sc.g.plantKeptOut() || sc.g.plantBusy() {
		t.Fatal("a plant that is gone still holds the account")
	}
	sc.g.holdTailUntil.Store(0)
	before = len(sc.s.ops())
	fromScript(func() { scriptClicks(sc.s) })
	if got := len(sc.s.ops()); got != before+4 {
		t.Fatalf("the script is still held after the plant went: %d of 4 clicks went out", got-before)
	}
}

// The compressed timeline: at 50 ticks the plant is pickable and the click goes out; the server never answers; by 70 the plant is
// left rather than clicked on again. The reply wait is no longer a fixed two seconds: the line says what the account knew.
func TestIncidentUnansweredPicksGiveWayToLeavingBeforeThePlantTurns(t *testing.T) {
	sc := newIncidentScene(t, 407, 50)
	sc.g.kick()
	if sc.s.count(clientprot.OPNPC1) != 1 {
		t.Fatalf("at 50 ticks the plant is pickable and must be clicked once, got %d:\n%s", sc.s.count(clientprot.OPNPC1), sc.log.text())
	}
	if !sc.log.has("no plant reply yet") || !sc.log.has("age 50t of 90") {
		t.Fatalf("the missing reply is not classifiable (age, wait, lag):\n%s", sc.log.text())
	}
	if sc.s.count(clientprot.MOVE_GAMECLICK) != 0 {
		t.Fatal("left a plant that still had time")
	}

	advanceTicks(sc.s.state, 20) // the clock keeps running while the server answers nothing
	sc.s.state.OverrideViewLag(&gamestate.ViewLag{})
	sc.g.mu.Lock()
	for sig := range sc.g.retryUntil {
		sc.g.retryUntil[sig] = time.Time{}
	}
	sc.g.mu.Unlock()
	sc.g.kick()
	if sc.s.count(clientprot.OPNPC1) != 1 {
		t.Fatalf("clicked again at 70 ticks:\n%s", sc.log.text())
	}
	if sc.s.count(clientprot.MOVE_GAMECLICK) == 0 || !sc.g.plants.hasLeft(incidentPlant) {
		t.Fatalf("did not leave at 70 ticks:\n%s", sc.log.text())
	}
}

// A click that waits on its reply while the clock runs out is cut short by the deadline, not held to its wait: the plant's clock is
// the account's, not the click's.
func TestIncidentTheReplyWaitEndsWhenTheClockRunsOut(t *testing.T) {
	sc := newIncidentScene(t, 407, 60)
	plantReplyBase = 3 * time.Second
	sc.s.onSend = func(op byte) {
		if op == clientprot.OPNPC1 {
			advanceTicks(sc.s.state, 10) // ten ticks pass before any answer
			sc.s.state.OverrideViewLag(&gamestate.ViewLag{})
		}
	}
	start := time.Now()
	sc.g.kick()
	if time.Since(start) > 2*time.Second {
		t.Fatalf("the wait ran to its 3 s end with the plant 10 ticks from its deadline:\n%s", sc.log.text())
	}
	if sc.s.count(clientprot.MOVE_GAMECLICK) == 0 {
		t.Fatalf("no walk out after the deadline passed during the wait:\n%s", sc.log.text())
	}
}

// A lag the view has shown moves the deadline forward: a plant at 50 ticks, with 20 ticks of lag in the last minutes, is left.
func TestIncidentLagMovesTheDeadlineForward(t *testing.T) {
	sc := newIncidentScene(t, 407, 50)
	sc.s.state.OverrideViewLag(&gamestate.ViewLag{Recent: gamestate.RecentLag{Ticks: 20, Ago: 30 * time.Second}})
	sc.g.kick()
	if sc.s.count(clientprot.OPNPC1) != 0 || sc.s.count(clientprot.MOVE_GAMECLICK) == 0 {
		t.Fatalf("with 20 ticks of lag a plant of 50 ticks turns in 40: it must be left, not clicked:\n%s", sc.log.text())
	}
}

// A growing plant is picked at as before: nothing about the clock changes the early game.
func TestIncidentAGrowingPlantIsStillPickedAt(t *testing.T) {
	sc := newIncidentScene(t, 407, 12)
	sc.g.kick()
	if sc.s.count(clientprot.OPNPC1) != 1 || sc.s.count(clientprot.MOVE_GAMECLICK) != 0 {
		t.Fatalf("a young plant must be clicked once and not left: clicks %d walks %d\n%s", sc.s.count(clientprot.OPNPC1), sc.s.count(clientprot.MOVE_GAMECLICK), sc.log.text())
	}
}

// The turn itself: the plant has turned (408, facing the account) and the account stands in its leash with the view current, hitpoints
// 14 of 24, a Kebab and a Ring of dueling, on a session that does not act on a click. Eight 3-second legs of "moved 0 tile(s)" killed it. Now
// the legs are judged in game ticks, the second stuck leg leaves by the ring, and the fisher's own clicks over the top of the flee are
// dropped (it walked toward the booth at 20:03:34, in the middle of it).
func TestIncidentAStuckFleeLeavesByTheRingAndTheScriptIsHeldOff(t *testing.T) {
	sc := newIncidentScene(t, 408, 97)
	st := sc.s.state
	withResolver(t, foodResolver{
		fakeResolver: fakeResolver{
			npcNames: map[int]string{407: "Strange plant", 408: "Strange plant"},
			npcOps:   map[int][5]string{408: {"", "Attack"}},
			objNames: map[int]string{333: "Kebab", ringID: "Ring of dueling(8)"},
		},
		objOps: map[int][5]string{333: {"Eat"}},
	})
	prev := currentInterfaceArchive()
	SetInterfaceArchive(rubArchive())
	t.Cleanup(func() { SetInterfaceArchive(prev) })
	setFullInventory(st, 3214, [2]int32{333, 1}, [2]int32{ringID, 1})
	setHP(st, 14, 24)
	hitSelf(st, 4)
	st.OverrideViewLag(&gamestate.ViewLag{})
	sc.s.onSend = func(op byte) {
		if op == clientprot.OPHELD4 {
			st.Handle(serverprot.IF_OPENCHAT, payload(func(p *packet.Packet) { p.P2(rubMenu) }))
			setLabel(st, rubOptA, "Al Kharid Duel Arena.")
			setLabel(st, rubOptB, "Castle Wars Arena.")
			setLabel(st, rubOptC, "Nowhere.")
		}
	}

	// The fisher: its bank code keeps clicking the booth and walking to the next one while the Guardian works.
	stop := make(chan struct{})
	var clicks atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !sc.g.handling.Load() { // the Guardian has taken the account: what the fisher sends from here on is what the fix is about
			time.Sleep(time.Millisecond)
		}
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = actions.OpLoc(sc.s, 2, 2585, 3418, 2213)
			_ = actions.Walk(sc.s, []pathfind.NavPoint{{X: incidentStandX, Z: incidentStandZ}, {X: 2585, Z: 3422}}, true)
			_ = actions.OpNpc(sc.s, 1, 77)
			clicks.Add(1)
			time.Sleep(5 * time.Millisecond)
		}
	}()
	sc.g.kick()
	close(stop)
	wg.Wait()

	log := sc.log.text()
	if !strings.Contains(log, "2 leg(s) in a row") {
		t.Fatalf("the stuck legs were not counted:\n%s", log)
	}
	if sc.s.count(clientprot.OPHELD4) != 1 || sc.s.count(clientprot.IF_BUTTON) == 0 {
		t.Fatalf("the stuck flee did not leave by the ring (rubs %d, buttons %d):\n%s", sc.s.count(clientprot.OPHELD4), sc.s.count(clientprot.IF_BUTTON), log)
	}
	if !strings.Contains(log, "combat flee: stuck") || !strings.Contains(log, "rubbed the Ring of dueling(8)") {
		t.Fatalf("no line says why or how it left:\n%s", log)
	}
	if clicks.Load() == 0 {
		t.Fatal("the fisher never ran")
	}
	if n := sc.s.count(clientprot.OPLOC2); n != 0 {
		t.Fatalf("%d booth click(s) of the fisher's went out while the Guardian fled", n)
	}
	if n := sc.s.count(clientprot.OPNPC1); n != 0 {
		t.Fatalf("%d NPC click(s) of the fisher's went out while the Guardian fled", n)
	}
	if !strings.Contains(log, "eating Kebab") {
		t.Fatalf("the flee never ate at 14 of 24:\n%s", log)
	}
}

// The same stall with the account not in danger (nothing hit it): no teleport is spent on a flee that is merely slow.
func TestIncidentASlowFleeThatIsNotBeingHitDoesNotTeleport(t *testing.T) {
	sc := newIncidentScene(t, 408, 97)
	st := sc.s.state
	withResolver(t, foodResolver{
		fakeResolver: fakeResolver{
			npcNames: map[int]string{407: "Strange plant", 408: "Strange plant"},
			npcOps:   map[int][5]string{408: {"", "Attack"}},
			objNames: map[int]string{ringID: "Ring of dueling(8)"},
		},
	})
	prev := currentInterfaceArchive()
	SetInterfaceArchive(rubArchive())
	t.Cleanup(func() { SetInterfaceArchive(prev) })
	setFullInventory(st, 3214, [2]int32{ringID, 1})
	setHP(st, 24, 24)
	if fleeStuckDanger(sc.s) {
		t.Fatal("a full-health account that nothing has hit reads as in danger")
	}
	hitSelf(st, 4)
	if !fleeStuckDanger(sc.s) {
		t.Fatal("hit by an angry plant, the account is in more danger than food answers")
	}
}

// An overdue plant is not walked up to: the walk to it is the walk into its leash. The account is four tiles from it, inside the leash's
// edge, and the only walk that goes out is away from the plant; the Guardian never closes to the pick's reach.
func TestIncidentAnOverduePlantIsNotWalkedUpTo(t *testing.T) {
	sc := newIncidentSceneAt(t, 407, 72, 2590, 3418)
	sc.g.kick()
	if sc.s.count(clientprot.OPNPC1) != 0 {
		t.Fatalf("clicked an overdue plant:\n%s", sc.log.text())
	}
	x, z, ok := lastWalkTarget(sc.s)
	if !ok {
		t.Fatalf("no walk:\n%s", sc.log.text())
	}
	if chebyshev(x, z, incidentSpawnX, incidentSpawnZ) < 5 {
		t.Fatalf("walked to %d,%d, inside the plant's leash:\n%s", x, z, sc.log.text())
	}
	if sc.log.has("walking to ") {
		t.Fatalf("the approach walk to the plant was logged:\n%s", sc.log.text())
	}
}

// A plant the account has left is not walked up to again when the Guardian looks at it next, whatever its age says.
func TestIncidentALeftPlantIsNotApproachedAgain(t *testing.T) {
	sc := newIncidentSceneAt(t, 407, 40, 2592, 3415)
	sc.g.plants.engage(incidentPlant)
	sc.g.plants.left(incidentPlant)
	sc.g.kick()
	if sc.s.count(clientprot.OPNPC1) != 0 || sc.s.count(clientprot.MOVE_GAMECLICK) != 0 {
		t.Fatalf("the Guardian went back to a plant it had left: ops %v\n%s", sc.s.ops(), sc.log.text())
	}
}

// An account that is already outside the leash when the deadline comes (it was fishing across the courtyard) is not walked anywhere: it
// stays where it is, out of the plant's reach, with its script held until the plant is gone.
func TestIncidentAnAccountAlreadyClearOfTheLeashStaysPut(t *testing.T) {
	sc := newIncidentSceneAt(t, 407, 72, 2592, 3415)
	sc.g.kick()
	if sc.s.count(clientprot.OPNPC1) != 0 || sc.s.count(clientprot.MOVE_GAMECLICK) != 0 {
		t.Fatalf("an account 6 tiles from the plant clicked or walked: %v\n%s", sc.s.ops(), sc.log.text())
	}
	if !sc.g.plants.hasLeft(incidentPlant) || !sc.g.Busy() {
		t.Fatalf("the plant is not being waited out:\n%s", sc.log.text())
	}
	if !sc.log.has("already") {
		t.Fatalf("no line says it was already clear:\n%s", sc.log.text())
	}
}
