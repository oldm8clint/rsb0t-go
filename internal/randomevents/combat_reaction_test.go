package randomevents

import (
	"testing"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// A trainer fighting an aggressive herd on purpose is always "in combat". Every event handler used to answer
// that with a 20 s flee that had nowhere to go, holding the action lock (and so parking the trainer) the whole
// time: a harmless Strange box was handled 21 times in 7 minutes that way, and a Strange plant never got
// picked because its pick's reply was never read (live 16-17 Oct 2026, the Asgarnian ice dungeon).

const (
	hpStat    = 3
	magicStat = 6
)

// setStat sends UPDATE_STAT for stat at the given current level.
func setStat(s *gamestate.State, stat, level int) {
	s.Handle(serverprot.UPDATE_STAT, payload(func(p *packet.Packet) { p.P1(byte(stat)); p.P4(0); p.P1(byte(level)) }))
}

// setHP puts the account at hp of max hitpoints.
func setHP(s *gamestate.State, hp, max int) {
	setStat(s, hpStat, max) // the highest level ever seen is the maximum
	setStat(s, hpStat, hp)
}

// underAttackBy puts the account under attack by a monster of typeID one tile east that is facing it: a
// recorded attacker still pursuing it, which is what underAttack reads between hits.
func underAttackBy(s *overlapSession, index, typeID int) {
	addNPCFacing(s.state, index, typeID, 1, 0, playerFaceBase+testSelfSlot)
	noteCombatFlee(s, 3200, 3200, 0)
}

func forgetCombat(s *overlapSession) { recentCombat.Delete(s) }

func boxEvent() *Event {
	return &Event{Kind: KindBoxLamp, Name: "Strange box", NpcIndex: -1, NpcType: -1, ItemID: 3062, Slot: 4, InvComID: 3214}
}

func TestCombatReactionFlees(t *testing.T) {
	cases := []struct {
		name       string
		expected   bool
		ev         *Event
		hp, max    int
		resolveAtk bool
		want       combatReaction
	}{
		{"a script whose combat is not expected flees", false, boxEvent(), 55, 55, false, reactFlee},
		{"an event monster is always fled", true, &Event{Kind: KindEvade, Name: "event monster", NpcIndex: 1, NpcType: 429}, 55, 55, false, reactFlee},
		{"a hazard is always fled", true, &Event{Kind: KindHazard, Name: "whirlpool", NpcIndex: 1}, 55, 55, false, reactFlee},
		{"a box is carried on with hitpoints to spare", true, boxEvent(), 55, 55, false, reactCarryOn},
		{"a box is carried on at exactly half", true, boxEvent(), 28, 55, false, reactCarryOn},
		{"a box steps aside below half", true, boxEvent(), 27, 55, false, reactStepAside},
		{"a plant seed is carried on", true, &Event{Kind: KindDialogNpc, Name: "Strange plant", NpcIndex: 1, NpcType: 407}, 40, 55, false, reactCarryOn},
		{"a plant that has turned hostile is fled", true, &Event{Kind: KindDialogNpc, Name: "Strange plant", NpcIndex: 1, NpcType: 407}, 55, 55, true, reactFlee},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ops := map[int][5]string{407: {"Pick"}}
			if c.resolveAtk {
				ops[407] = [5]string{"", "Attack"} // the type the NPC list shows has gone hostile
			}
			withResolver(t, fakeResolver{npcNames: map[int]string{407: "Strange plant"}, npcOps: ops})
			s := &overlapSession{state: overlapState()}
			g := NewGuardian(s, nil)
			g.CombatExpected = c.expected
			defer g.Stop()
			setHP(s.state, c.hp, c.max)
			addNPCs(s.state, npcSpec{1, 407, 1, 0})
			if got := combatReactionFor(s, c.ev); got != c.want {
				t.Fatalf("reaction = %v, want %v", got, c.want)
			}
		})
	}
	// Hitpoints not read yet: the safe answer is to let the script act.
	s := &overlapSession{state: overlapState()}
	g := NewGuardian(s, nil)
	g.CombatExpected = true
	defer g.Stop()
	if got := combatReactionFor(s, boxEvent()); got != reactStepAside {
		t.Fatalf("hitpoints unknown: reaction = %v, want to step aside", got)
	}
	if got := combatReactionFor(s, nil); got != reactFlee {
		t.Fatalf("no event: reaction = %v, want flee", got)
	}
}

// The reply wait of a handler: stepping aside returns at once with the event deferred, the guardian let go of
// (Busy false), the attempt given back, and no walk sent; carrying on waits out the wait and walks nowhere.
func TestWaitEventReplyStepsAsideForTheScriptAndCarriesOnWhenWell(t *testing.T) {
	withResolver(t, nil)
	s := &overlapSession{state: overlapState()}
	g := NewGuardian(s, nil)
	g.CombatExpected = true
	defer g.Stop()
	t.Cleanup(func() { forgetCombat(s) })
	underAttackBy(s, 77, 1234)
	ev := boxEvent()
	sig := eventSignature(ev)
	g.noteAttempt(sig)

	var sent []byte
	s.onSend = func(op byte) { sent = append(sent, op) }

	// Hurt: steps aside.
	setHP(s.state, 20, 55)
	var lines []string
	started := time.Now()
	if !waitEventReply(s, nil, ev, 5*time.Second, nil, func(m string) { lines = append(lines, m) }) {
		t.Fatal("a hurt account's handler did not return to give the script the lock")
	}
	if took := time.Since(started); took > time.Second {
		t.Fatalf("stepping aside took %s", took)
	}
	g.mu.Lock()
	until, deferred := g.retryUntil[sig]
	attempts := g.attempts[sig]
	g.mu.Unlock()
	if !deferred || time.Until(until) < combatEventDefer-2*time.Second {
		t.Fatalf("the event was not deferred for %s (deferred %v, %s left)", combatEventDefer, deferred, time.Until(until))
	}
	if attempts != 0 {
		t.Fatalf("stepping aside counted as an attempt (%d): four of them would make the guardian give the event up", attempts)
	}
	g.mu.Lock()
	g.trackedEvents[sig] = *ev
	g.mu.Unlock()
	if g.retryImminent() {
		t.Fatal("the script is held for a retry that is 20 s away")
	}
	if len(lines) != 1 {
		t.Fatalf("log lines: %v", lines)
	}
	for _, op := range sent {
		if op != byte(clientprot.CLOSE_MODAL) {
			t.Fatalf("packet %d was sent: stepping aside closes the dialogue and nothing else (no flee walk)", op)
		}
	}

	// Well: carries on. The wait is short here and runs out with the handler still working.
	g.mu.Lock()
	delete(g.retryUntil, sig)
	g.mu.Unlock()
	setHP(s.state, 55, 55)
	sent = nil
	if waitEventReply(s, nil, ev, 300*time.Millisecond, nil, nil) {
		t.Fatal("a well account's handler abandoned the box")
	}
	if len(sent) != 0 {
		t.Fatalf("carrying on sent %d packets: the account fled or walked", len(sent))
	}
}

// A script whose combat is NOT expected is fled for, as before: stepping aside is for the fighter only.
func TestWaitEventReplyFleesForAScriptWhoseCombatIsNotExpected(t *testing.T) {
	withResolver(t, nil)
	s := &overlapSession{state: overlapState()}
	g := NewGuardian(s, nil) // CombatExpected false
	defer g.Stop()
	t.Cleanup(func() { forgetCombat(s) })
	underAttackBy(s, 77, 1234)
	setHP(s.state, 55, 55)
	prev := fleeWalkDeadline
	fleeWalkDeadline = 300 * time.Millisecond
	t.Cleanup(func() { fleeWalkDeadline = prev })
	ev := boxEvent()
	var lines []string
	if !waitEventReply(s, nil, ev, 5*time.Second, nil, func(m string) { lines = append(lines, m) }) {
		t.Fatal("the handler did not return after fleeing")
	}
	found := false
	for _, l := range lines {
		if len(l) > 0 && l[:min(len(l), 30)] == "in combat while checking the e" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no flee line: %v", lines)
	}
	g.mu.Lock()
	_, deferred := g.retryUntil[eventSignature(ev)]
	g.mu.Unlock()
	if deferred {
		t.Fatal("the event was deferred instead of fled from")
	}
}

func TestLeashSpawnOfAPlantIsTheSeedsTile(t *testing.T) {
	s := &overlapSession{state: overlapState()}
	if x, z := noteLeashSpawn(s, 4100, plantSeedType, 3053, 9578, 0); x != 3053 || z != 9578 {
		t.Fatalf("first sighting of the seed = %d,%d", x, z)
	}
	// 54 s later it turns hostile and has chased the account two tiles: the leash is still measured from the seed.
	if x, z := noteLeashSpawn(s, 4100, angryPlantType, 3055, 9578, 0); x != 3053 || z != 9578 {
		t.Fatalf("the angry plant's spawn = %d,%d, want the seed's 3053,9578", x, z)
	}
	// A different NPC of the same type is a new spawn.
	if x, z := noteLeashSpawn(s, 4101, angryPlantType, 3100, 3100, 0); x != 3100 || z != 3100 {
		t.Fatalf("another plant kept the old spawn: %d,%d", x, z)
	}
}

func TestLastResortWantedOnlyWhenHurtEnoughToNeedIt(t *testing.T) {
	cases := []struct {
		name        string
		hp, max     int
		known, food bool
		want        bool
	}{
		{"healthy", 50, 55, true, false, false},
		{"hurt with food: the bite comes first", 25, 55, true, true, false},
		{"nearly dead with food", 16, 55, true, true, true},
		{"hurt with no food", 27, 55, true, false, true},
		{"half with no food is not yet", 28, 55, true, false, false},
		{"unknown hitpoints never", 1, 55, false, false, false},
		{"dead", 0, 55, true, false, false},
	}
	for _, c := range cases {
		if got := lastResortWanted(c.hp, c.max, c.known, c.food); got != c.want {
			t.Errorf("%s: lastResortWanted(%d/%d, known %v, food %v) = %v, want %v", c.name, c.hp, c.max, c.known, c.food, got, c.want)
		}
	}
}

// foodResolver is fakeResolver with item options, which strandFood reads.
type foodResolver struct {
	fakeResolver
	objOps map[int][5]string
}

func (f foodResolver) Obj(id int) (cachecfg.Obj, bool) {
	o, ok := f.fakeResolver.Obj(id)
	if ok {
		o.IOps = f.objOps[id]
	}
	return o, ok
}

// A cornered account that can pay for a teleport casts one once it is hurt enough, once, and only with the
// Magic level and the runes (or a staff) the spell takes.
func TestLastResortCastsTheTeleportThePackCanPay(t *testing.T) {
	const law, air, fire, bass = 563, 556, 554, 365
	withResolver(t, foodResolver{
		fakeResolver: fakeResolver{objNames: map[int]string{law: "Law rune", air: "Air rune", fire: "Fire rune", bass: "Bass"}},
		objOps:       map[int][5]string{bass: {"Eat"}},
	})
	s := &overlapSession{state: overlapState()}
	g := NewGuardian(s, nil)
	defer g.Stop()
	t.Cleanup(func() { lastResortCasts.Delete(s) })
	setHP(s.state, 12, 55)
	setStat(s.state, magicStat, 66)
	sends := 0
	s.onSend = func(byte) { sends++ }

	// No runes yet: nothing to cast.
	if lastResortExit(s, "test", nil) {
		t.Fatal("cast a teleport with no runes")
	}
	setFullInventory(s.state, 3214, [2]int32{law, 5}, [2]int32{air, 30}, [2]int32{fire, 10})
	var lines []string
	if !lastResortExit(s, "test", func(m string) { lines = append(lines, m) }) || sends == 0 {
		t.Fatalf("a hurt cornered account with the runes did not cast (sends %d, %v)", sends, lines)
	}
	if len(lines) != 1 {
		t.Fatalf("log: %v", lines)
	}
	// Not again while the first is landing.
	sends = 0
	if lastResortExit(s, "test", nil) || sends != 0 {
		t.Fatal("cast a second teleport within the gap")
	}
	// Healthy with food in the pack: no.
	lastResortCasts.Delete(s)
	setHP(s.state, 50, 55)
	if lastResortExit(s, "test", nil) {
		t.Fatal("a healthy account teleported")
	}
	// Hurt, food in the pack and not that hurt: eat first.
	setFullInventory(s.state, 3214, [2]int32{law, 5}, [2]int32{air, 30}, [2]int32{fire, 10}, [2]int32{bass, 4})
	setHP(s.state, 24, 55)
	if lastResortExit(s, "test", nil) {
		t.Fatal("teleported with food to eat and hitpoints above the food floor")
	}
	// Magic too low for any of the three.
	setStat(s.state, magicStat, 20)
	setHP(s.state, 6, 55)
	if lastResortExit(s, "test", nil) {
		t.Fatal("cast with Magic 20")
	}
}

// The walking flee's end condition. A trainer in a herd is hit all the time; the flee it runs is from an event
// monster and is over when none is near, not when the warriors stop hitting (they never do).
func TestWalkerThreatenedForAnAccountWhoseCombatIsExpected(t *testing.T) {
	withResolver(t, nil)
	warriors := func(s *overlapSession) { underAttackBy(s, 77, iceWarriorType) }
	cases := []struct {
		name     string
		expected bool
		monster  bool // an angry Strange plant targets the account
		want     bool
	}{
		{"expected, only the herd is hitting", true, false, false},
		{"expected, the plant is on it", true, true, true},
		{"not expected, the herd is hitting", false, false, true},
		{"not expected, the plant is on it", false, true, true},
	}
	for _, c := range cases {
		s := &overlapSession{state: overlapState()}
		g := NewGuardian(s, nil)
		g.CombatExpected = c.expected
		warriors(s)
		if c.monster {
			// both in one NPC_INFO: addNPCFacing replaces the tracked set
			addFacingNPCs(s.state, facingNPC{npcSpec{77, iceWarriorType, 1, 0}, playerFaceBase + testSelfSlot},
				facingNPC{npcSpec{78, angryPlantType, 0, 1}, playerFaceBase + testSelfSlot})
		}
		if got := walkerThreatened(s, 3200, 3200); got != c.want {
			t.Errorf("%s: walkerThreatened = %v, want %v", c.name, got, c.want)
		}
		forgetCombat(s)
		g.Stop()
	}
}
