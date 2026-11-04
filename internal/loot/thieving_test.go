package loot

import "testing"

// TestEveryPickpocketAndStallShareOneThievingCard is the owner's "maybe combine pickpocketing to
// one panel" (2026-09-23).
//
// The live Loot tab drew four thieving cards: the three Man ids (already merged by name), Guard,
// Knight of Ardougne and the Baker's stall. Each target must now land on ONE card while keeping
// its own identity underneath — its own key, and its own row (PartKey) inside the card.
//
// On the old code this fails at the first check: a pickpocket grouped on "npc|pickpocket|man"
// and a steal on "loc|steal|baker's stall".
func TestEveryPickpocketAndStallShareOneThievingCard(t *testing.T) {
	man1 := NPC(nil, ActionPickpocket, 1, "Man")
	man3 := NPC(nil, ActionPickpocket, 3, "Man")
	guard := NPC(nil, ActionPickpocket, 32, "Guard")
	knight := NPC(nil, ActionPickpocket, 23, "Knight of Ardougne")
	stall := Stall(nil, 2561, "Baker's stall")
	chest := Loc(nil, ActionSteal, 2566, "Chest")

	card := man1.GroupKey()
	if card != "thieving|thieving" {
		t.Fatalf("a pickpocket groups on %q, want thieving|thieving", card)
	}
	for _, s := range []Source{man3, guard, knight, stall, chest} {
		if s.GroupKey() != card {
			t.Fatalf("%s %s groups on %q, not the Thieving card %q", s.Action, s.Name, s.GroupKey(), card)
		}
		if s.Skill != SkillThieving || s.Kind != KindThieving {
			t.Fatalf("%+v is not a thieving source", s)
		}
	}
	// Each target keeps its own row inside the card; the three Man ids are ONE row.
	if man1.PartKey() != man3.PartKey() {
		t.Fatalf("the Man ids split into rows %q and %q", man1.PartKey(), man3.PartKey())
	}
	rows := map[string]bool{}
	for _, s := range []Source{man1, guard, knight, stall} {
		rows[s.PartKey()] = true
	}
	if len(rows) != 4 {
		t.Fatalf("want four rows (Man, Guard, Knight, Baker's stall) inside the card, got %v", rows)
	}
	// And the identities a record is persisted under are the ones they always were.
	for want, s := range map[string]Source{"pickpocket:man:1": man1, "pickpocket:guard:32": guard, "steal:baker's stall:2561": stall} {
		if s.Key() != want {
			t.Fatalf("key moved: %q, want %q", s.Key(), want)
		}
	}
	// The panel draws each target's own model: an npc for a pickpocket, a loc for a stall.
	if man1.Model != ModelNPC || stall.Model != ModelLoc {
		t.Fatalf("models = %q / %q, want npc / loc", man1.Model, stall.Model)
	}
}

// TestAKilledManIsNotAThievingSource: only the pickpocket moves. A kill is a different row of
// loot, and a Skill on it would pull every monster in the game onto one card.
func TestAKilledManIsNotAThievingSource(t *testing.T) {
	kill := NPC(nil, ActionKill, 1, "Man")
	if kill.Kind != KindNPC || kill.Skill != "" || kill.GroupKey() != "npc|kill|man" {
		t.Fatalf("a killed Man = %+v (card %q), want an ordinary npc kill source", kill, kill.GroupKey())
	}
	if kill.GroupKey() == NPC(nil, ActionPickpocket, 1, "Man").GroupKey() {
		t.Fatal("a Man killed and a Man pickpocketed share a card")
	}
}

// TestMigrateMovesPersistedThievingOntoTheOneCard pins the migration against the shape an older
// build really persisted, written by hand rather than built with today's constructors — today's
// constructors cannot produce it, which is the point. The live file holds 231 pickpocket records
// {kind npc, no skill, no model} and 107 steal records {kind loc, model loc, no skill}.
func TestMigrateMovesPersistedThievingOntoTheOneCard(t *testing.T) {
	legacyPick := Source{Kind: KindNPC, Action: ActionPickpocket, Name: "Man", ID: 1, Level: 2}
	legacySteal := Source{Kind: KindLoc, Action: ActionSteal, Name: "Baker's stall", ID: 2561, Model: ModelLoc}

	for _, legacy := range []Source{legacyPick, legacySteal} {
		got := Migrate(legacy)
		if got.GroupKey() != "thieving|thieving" {
			t.Fatalf("migrated %+v groups on %q", legacy, got.GroupKey())
		}
		if got.Key() != legacy.Key() {
			t.Fatalf("migration moved the persisted key: %q -> %q", legacy.Key(), got.Key())
		}
		if Migrate(got) != got {
			t.Fatalf("Migrate is not idempotent on %+v", got)
		}
	}
	today := NPC(nil, ActionPickpocket, 1, "Man")
	today.Level = 2 // what the cache would have filled in
	if Migrate(legacyPick) != today {
		t.Fatalf("a migrated pickpocket %+v differs from one recorded today %+v", Migrate(legacyPick), today)
	}
	if Migrate(legacyPick).Level != 2 {
		t.Fatal("the migration dropped the target's combat level")
	}
	// Everything that is not a pickpocket or a steal is left exactly as it was.
	for _, src := range []Source{NPC(nil, ActionKill, 81, "Cow"), Openable("Casket", 405), Fishing(nil, 316), Fletching(nil, 1511, "Logs")} {
		if Migrate(src) != src {
			t.Fatalf("Migrate changed %+v -> %+v", src, Migrate(src))
		}
	}
}

// TestEatenLeftoversAreNeverAThievingTake: the Guard card carried "2/3 cake" and "Slice of cake"
// because the account ate inside the pickpocket's measurement window. The leftovers must be
// skipped, and a genuine Baker's stall yield must not be.
func TestEatenLeftoversAreNeverAThievingTake(t *testing.T) {
	before := Counts{995: 100, 1891: 3}
	after := Counts{995: 130, 1891: 2, 1893: 1} // 30 coins stolen, one Cake bitten to 2/3 cake
	got := Gained(before, after, IsEatenLeftover)
	if len(got) != 1 || got[995] != 30 {
		t.Fatalf("gained = %v, want only the 30 coins", got)
	}
	for _, genuine := range []int{1891 /* Cake */, 2309 /* Bread */, 1901 /* Chocolate slice */, 995} {
		if IsEatenLeftover(genuine) {
			t.Fatalf("obj %d is a real thieving yield and must not be skipped", genuine)
		}
	}
}

// TestLegacyGroundPickupIsRecognisedExactly: the restore-time fold must touch only the one shape
// scarabrunner used to write, never a real openable.
func TestLegacyGroundPickupIsRecognisedExactly(t *testing.T) {
	if !IsLegacyGroundPickup(Source{Kind: KindOpenable, Action: ActionOpen, Name: "Ground", ID: -1}) {
		t.Fatal("the live open:ground:-1 shape was not recognised")
	}
	for _, s := range []Source{Openable("Casket", 405), Openable("Ground", 405), {Kind: KindNPC, Action: ActionKill, Name: "Ground", ID: -1}} {
		if IsLegacyGroundPickup(s) {
			t.Fatalf("%+v was taken for a legacy ground pickup", s)
		}
	}
	if LegacyGroundKill.Key() != "kill:cow:81" {
		t.Fatalf("legacy ground pickups fold onto %q, want kill:cow:81", LegacyGroundKill.Key())
	}
}
