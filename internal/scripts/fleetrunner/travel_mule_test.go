package fleetrunner

import (
	"testing"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// THE OWNER'S RULE, 6 Oct 2026: "they should only teleport if they require to go back to the mule". A walk
// starts with a travel teleport only when it is the walk to the mule's post (WalkToMule) -- or the operator
// switched teleTravelAnyWalk on, the behaviour of 3-6 Oct.
func TestOnlyTheWalkToTheMuleMayTeleport(t *testing.T) {
	tr := &Travel{Tele: fleetplan.TeleTravelSettings{Enabled: true}.Normalised()}
	if tr.mayTeleport() {
		t.Fatal("an ordinary walk may teleport")
	}
	inside := false
	got := tr.ForMuleWalk(func() bool { inside = tr.mayTeleport(); return true })
	if !got || !inside {
		t.Fatalf("the walk to the mule: walk result %v, may teleport inside it %v", got, inside)
	}
	if tr.mayTeleport() {
		t.Fatal("the permission outlived the walk to the mule")
	}
	// A walk that does not arrive reports so, and still clears the permission.
	if tr.ForMuleWalk(func() bool { return false }) || tr.mayTeleport() {
		t.Fatal("a failed walk to the mule left the permission set or reported success")
	}
	// Not armed: the cheap gate answers before anything is read (a nil Deps is safe).
	if tr.teleportBeforeWalk(nil, tile.Tile{X: 2809, Z: 3441}) {
		t.Fatal("an unarmed walk teleported")
	}
	// The any-walk setting is the old behaviour.
	anyWalk := &Travel{Tele: fleetplan.TeleTravelSettings{Enabled: true, AnyWalk: true}.Normalised()}
	if !anyWalk.mayTeleport() {
		t.Fatal("teleTravelAnyWalk: any walk may teleport")
	}
	// Travel off, or no Travel at all: never, even for the walk to the mule.
	off := &Travel{Tele: fleetplan.TeleTravelSettings{Enabled: false}.Normalised()}
	if off.ForMuleWalk(func() bool { return off.mayTeleport() }) {
		t.Fatal("teleTravel off still teleported")
	}
	var nilTravel *Travel
	if nilTravel.mayTeleport() || !nilTravel.ForMuleWalk(func() bool { return true }) {
		t.Fatal("a nil Travel must walk as before")
	}
}

// The trip kit's runes stay in the pack through a bank deposit (the training kit's, the Stage 5 trainer's), for an account
// whose Magic opens a teleport (Varrock at 25, since 13 Oct 2026; Camelot's 45 before): laws, fire for Varrock and air when
// travel may spend pack air. Below Magic 25, with the setting at 0 or with travel off, a rune is banked like any other item.
func TestAlchKeepsTheTripLawsForTheTeleportToTheMule(t *testing.T) {
	cfg := travelTestConfig(t)
	at := func(magic int) *Deps {
		sim := actionsim.New(actionsim.Objs{}, nil)
		setStat(sim, fleetplan.MagicStat, 200000, magic)
		return &Deps{Session: sim, Config: cfg}
	}
	on := &Travel{Tele: fleetplan.TeleTravelSettings{Enabled: true, TripLaws: fleetplan.DefaultTripLaws}.Normalised()}
	onAir := &Travel{Tele: fleetplan.TeleTravelSettings{Enabled: true, TripLaws: fleetplan.DefaultTripLaws, AirRunes: true}.Normalised()}
	for _, c := range []struct {
		name  string
		tr    *Travel
		magic int
		item  string
		want  bool
	}{
		{"Magic 56 keeps its laws", on, 56, "Law rune", true},
		{"Magic 45 keeps its laws", on, 45, "Law rune", true},
		{"Magic 25 keeps them: Varrock opens a kit", on, 25, "Law rune", true},
		{"Magic 24 banks them", on, 24, "Law rune", false},
		{"fire runes stay for Varrock", on, 56, "Fire rune", true},
		{"air runes are banked while travel may not spend them", on, 56, "Air rune", false},
		{"air runes stay when it may", onAir, 56, "Air rune", true},
		{"air runes at Magic 24 are banked", onAir, 24, "Air rune", false},
		{"it keeps no other rune", on, 56, "Chaos rune", false},
		{"it keeps no coins", on, 56, "Coins", false},
		{"no laws a trip: banked", &Travel{Tele: fleetplan.TeleTravelSettings{Enabled: true, TripLaws: 0}.Normalised()}, 56, "Law rune", false},
		{"travel off: banked", &Travel{Tele: fleetplan.TeleTravelSettings{Enabled: false, TripLaws: 2}.Normalised()}, 56, "Law rune", false},
		{"no Travel: banked", nil, 56, "Law rune", false},
	} {
		if got := c.tr.AlchKeeps(at(c.magic), c.item); got != c.want {
			t.Errorf("%s: AlchKeeps(%q) = %v, want %v", c.name, c.item, got, c.want)
		}
	}
}
