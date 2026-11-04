package randomevents

import (
	"strings"
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// Round 2 of the crowd fix: the Mysterious Old Man and the Drunken Dwarf crowd the same way
// the Genie did, and any event NPC's later line can erase the greeting a bystander was
// skipping it on.

const (
	oldManType = 501
	dwarfType  = 502
)

func withCrowdNpcs(t *testing.T) {
	t.Helper()
	withResolver(t, fakeResolver{npcNames: map[int]string{
		genieType: "Genie", oldManType: "Mysterious Old Man", dwarfType: "Drunken Dwarf",
	}})
}

// npcSaysLine has NPC 1, the only NPC addGreetingNPC tracks, say line (OldVis op 0 + SAY).
func npcSaysLine(s *gamestate.State, line string) {
	bits := bitPack(4, bitField{8, 1}, bitField{1, 1}, bitField{2, 0}, bitField{14, 16383})
	s.Handle(serverprot.NPC_INFO, append(bits, payload(func(p *packet.Packet) { p.P1(8); p.PJStr(line) })...))
}

// The live 07:01:31 state on c0da98ee, as the 192 Catherby fishers saw it: they had skipped
// a non-fleet player's Old Man on its greeting, then its despawn line replaced the greeting
// (its owner was gone) and nothing showed it facing anyone. It vanished two seconds later.
func TestLiveCrowdOldManIsNotABystandersEvent(t *testing.T) {
	withCrowdNpcs(t)
	s := &overlapSession{state: overlapState()}
	setSelfName(t, s.state, "acct12")
	playSound(s.state, gamestate.SoundSwarmAppear) // sounds reach this session, just not that one
	addGreetingNPC(s.state, oldManType, -1, "Greetings Sprinter!")
	if ev := Detect(s.state); ev != nil {
		t.Fatalf("a bystander took up an Old Man that greeted someone else: %+v", ev)
	}
	npcSaysLine(s.state, "I lose more friends that way.")
	if ev := Detect(s.state); ev != nil {
		t.Fatalf("a bystander took up a despawning Old Man: %+v", ev)
	}
	g := NewGuardian(s, nil)
	defer g.Stop()
	if ev := g.nextEvent(); ev != nil {
		t.Fatalf("the Guardian would handle a despawning Old Man: %+v", ev)
	}
}

// Once a greeting has named the owner, a later line that names nobody leaves the verdict
// where the greeting put it — for a bystander (someone else's, even with the owner-only
// sound heard for an event of its own) and for the owner (ours, even after another
// player's Talk-to turned its face away).
func TestLaterLineDoesNotEraseTheGreetingsOwner(t *testing.T) {
	withCrowdNpcs(t)
	for _, tc := range []struct {
		typeID         int
		name, greeting string
	}{
		{genieType, "Genie", "Greetings master Sprinter!"},
		{oldManType, "Mysterious Old Man", "Greetings Sprinter!"},
		{dwarfType, "Drunken Dwarf", "'Ello der Sprinter! *hic*"},
	} {
		bystander := overlapState()
		setSelfName(t, bystander, "acct12")
		heardOwnEvent(bystander, tc.name) // the sound gate alone would let it through
		addGreetingNPC(bystander, tc.typeID, -1, tc.greeting)
		npcSaysLine(bystander, "Hmm... where was I?") // any line no template parses
		if ev := Detect(bystander); ev != nil {
			t.Errorf("%s: a later line erased the greeting naming someone else: %+v", tc.name, ev)
		}

		owner := overlapState()
		setSelfName(t, owner, "sprinter")
		addGreetingNPC(owner, tc.typeID, 32768+9, tc.greeting) // faces another player
		npcSaysLine(owner, "Hmm... where was I?")
		if ev := Detect(owner); ev == nil || ev.NpcIndex != 1 || ev.Name != tc.name {
			t.Errorf("%s: the owner lost its own event to a later line: %+v", tc.name, ev)
		}
	}
}

// A despawn or fail line means the event is over for everyone: nobody walks to it, not even
// the account it faces or once named.
func TestEventOverLinesAreNobodysEvent(t *testing.T) {
	withCrowdNpcs(t)
	for _, tc := range []struct {
		typeID         int
		name, greeting string
	}{
		{genieType, "Genie", "Greetings master Sprinter!"},
		{oldManType, "Mysterious Old Man", "Greetings Sprinter!"},
		{dwarfType, "Drunken Dwarf", "'Ello der Sprinter! *hic*"},
	} {
		for _, line := range eventOverLines[strings.ToLower(tc.name)] {
			s := &overlapSession{state: overlapState()}
			setSelfName(t, s.state, "sprinter")
			heardOwnEvent(s.state, tc.name)
			addGreetingNPC(s.state, tc.typeID, 32768+5, tc.greeting) // ours by greeting and face
			npcSaysLine(s.state, line)
			if ev := Detect(s.state); ev != nil {
				t.Errorf("%s %q: taken up although its event is over: %+v", tc.name, line, ev)
			}
			g := NewGuardian(s, nil)
			if ev := g.nextEvent(); ev != nil {
				t.Errorf("%s %q: the Guardian would handle it: %+v", tc.name, line, ev)
			}
			g.Stop()
		}
	}
	if eventOver("Genie", "Greetings master Sprinter!") || eventOver("Frog", "I lose more friends that way.") {
		t.Fatal("eventOver matched a line that does not end that NPC's event")
	}
	if !eventOver("Mysterious Old Man", " i lose more friends that way. ") {
		t.Fatal("eventOver must ignore case and surrounding space, as production's text may differ")
	}
}

// The Old Man and the Dwarf get the Genie's owner-sound gate: one that neither greeting nor
// face attributes is someone else's unless this account heard that NPC's owner-only sound
// within its lifetime (smokepuff at the Old Man's spawn, dwarf_whistle at the Dwarf's spawn
// and with each nag).
func TestUnknownOldManAndDwarfNeedTheirOwnerSound(t *testing.T) {
	withCrowdNpcs(t)
	for _, tc := range []struct {
		typeID int
		name   string
	}{{oldManType, "Mysterious Old Man"}, {dwarfType, "Drunken Dwarf"}} {
		snd := ownerSounds[strings.ToLower(tc.name)]

		without := overlapState()
		playSound(without, gamestate.SoundSwarmAppear)
		addNPCFacing(without, 1, tc.typeID, 1, 0, -1)
		if ev := Detect(without); ev != nil {
			t.Errorf("%s: taken up without its owner sound: %+v", tc.name, ev)
		}

		with := overlapState()
		playSound(with, snd.id)
		advanceTicks(with, snd.windowTicks-5)
		addNPCFacing(with, 1, tc.typeID, 1, 0, -1)
		if ev := Detect(with); ev == nil || ev.NpcIndex != 1 {
			t.Errorf("%s: the owner ignored its own event inside the window: %+v", tc.name, ev)
		}

		expired := overlapState()
		playSound(expired, snd.id)
		advanceTicks(expired, snd.windowTicks+10)
		addNPCFacing(expired, 1, tc.typeID, 1, 0, -1)
		if ev := Detect(expired); ev != nil {
			t.Errorf("%s: an owner sound older than the NPC's life still counted: %+v", tc.name, ev)
		}

		facing := overlapState()
		addNPCFacing(facing, 1, tc.typeID, 1, 0, 32768+5)
		if ev := Detect(facing); ev == nil || ev.NpcIndex != 1 {
			t.Errorf("%s: one facing this account needs no sound: %+v", tc.name, ev)
		}
	}
}

// Their rejections say whether the owner sound was heard, as the Genie's do.
func TestOldManAndDwarfRejectionsLogTheirOwnerSound(t *testing.T) {
	withCrowdNpcs(t)
	for _, tc := range []struct {
		typeID      int
		name, sound string
	}{{oldManType, "Mysterious Old Man", "smokepuff"}, {dwarfType, "Drunken Dwarf", "dwarf_whistle"}} {
		s := &overlapSession{state: overlapState()}
		playSound(s.state, gamestate.SoundSwarmAppear)
		addNPCFacing(s.state, 1, tc.typeID, 1, 0, 32768+9)
		var lines []string
		if err := rejectDialogNPC(s, newDialogEvent(1, tc.typeID, tc.name), func(l string) { lines = append(lines, l) }); err != nil {
			t.Fatal(err)
		}
		if want := tc.sound + " heard=false, sounds seen=true"; len(lines) != 1 || !strings.Contains(lines[0], want) {
			t.Errorf("%s: logged %q, want it to contain %q", tc.name, lines, want)
		}
	}
}
