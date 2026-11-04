package loot

import "testing"

// A stall and a thievable chest are LOCS. Before 2026-09-22 the taxonomy had no Source that
// could describe one — npc, openable and trail were the only kinds — so
// thieverrunner.StealStall and .SearchChest recorded nothing at all while Pickpocket beside
// them recorded every take. ScarabSwarm's Stage 2 works the Baker's stall for its whole
// larder, and none of it ever reached the panel.
//
// These cases pin the shape rather than the wiring: that a loc source exists, that it is
// distinguishable from the other kinds, and that it carries the model the panel needs to
// draw it.

func TestStallIsItsOwnKindAndAction(t *testing.T) {
	// cfg is nil here on purpose: the fallback name must work, because a harness and an
	// account whose cache has not loaded both hit it.
	s := Stall(nil, 4874, "Baker's stall")

	// A steal is a THIEVING source since 2026-09-23, so the stall shares the one Thieving card
	// with every pickpocket (thieving.go); before that it was KindLoc on a card of its own.
	if s.Kind != KindThieving || s.Skill != SkillThieving {
		t.Errorf("Kind/Skill = %q/%q, want %q/%q", s.Kind, s.Skill, KindThieving, SkillThieving)
	}
	if s.Action != ActionSteal {
		t.Errorf("Action = %q, want %q — a steal from a stall is not an Open (a casket in the "+
			"pack) and not a Pickpocket (an npc)", s.Action, ActionSteal)
	}
	if s.ID != 4874 {
		t.Errorf("ID = %d, want the loc id 4874", s.ID)
	}
	if s.Name != "Baker's stall" {
		t.Errorf("Name = %q, want the fallback when cfg is nil", s.Name)
	}
	if s.Model != ModelLoc {
		t.Errorf("Model = %q, want %q so the panel draws the loc's own model rather than "+
			"guessing from the kind", s.Model, ModelLoc)
	}
}

func TestLocKindIsDistinctFromTheOtherKinds(t *testing.T) {
	// The whole reason this kind was added: a stall could not be expressed as any of these.
	for _, other := range []string{KindNPC, KindOpenable, KindTrail} {
		if KindLoc == other {
			t.Errorf("KindLoc collides with %q", other)
		}
	}
	// And a steal must not collapse into an open, or a casket opened and a stall robbed
	// would share one card and one count.
	if ActionSteal == ActionOpen || ActionSteal == ActionPickpocket {
		t.Error("ActionSteal collides with ActionOpen or ActionPickpocket")
	}
}

func TestLocTakesAnyActionSoAChestIsNotForcedToBeAStall(t *testing.T) {
	// Loc is the general constructor; Stall is the common case. A future caller that wants
	// to distinguish a chest search from a stall steal can, without a new kind.
	s := Loc(nil, ActionSteal, 2693, "Chest")
	if s.Kind != KindThieving || s.Model != ModelLoc || s.ID != 2693 || s.Name != "Chest" {
		t.Errorf("Loc(...) = %+v, want a loc source for id 2693 named Chest", s)
	}
}

// GroupKey decides which card a source lands on. A stall and the same-named npc must not
// collapse together, or "Baker's stall" steals would be counted as kills.
func TestAStallAndAnNPCDoNotShareACard(t *testing.T) {
	stall := Stall(nil, 4874, "Guard")
	npc := NPC(nil, ActionKill, 4874, "Guard")
	if stall.GroupKey() == npc.GroupKey() {
		t.Errorf("a loc and an npc with the same id and name share the card key %q — the kind "+
			"has to be part of it", stall.GroupKey())
	}
}
