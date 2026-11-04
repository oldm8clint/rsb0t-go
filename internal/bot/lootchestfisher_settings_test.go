package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/lootchestfisherplan"
	"rsb0t-go/internal/scripts/lootchestfisherrunner"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/tile"
)

func TestLootChestFisherIsRegistered(t *testing.T) {
	b, ok := New("LootChestFisher")
	if !ok {
		t.Fatal("LootChestFisher is not registered: a roster entry cannot select it by name")
	}
	if _, isRight := b.(*lootChestBot); !isRight {
		t.Fatalf("the registry builds %T, not *lootChestBot", b)
	}
}

// The fallback has to be the REGISTERED fisher, not a copy of it. This asserts the two
// factories build the same type, which is what makes "hands back to ProgressiveFisher" true.
func TestTheFallbackIsTheRegisteredProgressiveFisher(t *testing.T) {
	reg, ok := New("ProgressiveFisher")
	if !ok {
		t.Fatal("ProgressiveFisher is not registered")
	}
	var fallback Bot = &fisherBot{}
	if got, want := typeName(fallback), typeName(reg); got != want {
		t.Fatalf("the fallback builds %s but the registry builds %s", got, want)
	}
}

func typeName(b Bot) string {
	switch b.(type) {
	case *fisherBot:
		return "*fisherBot"
	default:
		return "other"
	}
}

// muleAtTile is deliberately the SAME setting key ProgressiveFisher reads, so switching an
// existing swarm account over needs no second tile in its roster entry.
func TestLootChestFisherReadsTheFishersOwnMuleTile(t *testing.T) {
	want := tile.Tile{X: 2725, Z: 3491, Level: 0}
	if got := fisherTileSetting(map[string]interface{}{"muleAtTile": "2725,3491"}, "muleAtTile", mulereceiverplan.SeersStand); got != want {
		t.Fatalf("muleAtTile read as %v, want %v", got, want)
	}
	if got := fisherTileSetting(map[string]interface{}{}, "muleAtTile", mulereceiverplan.SeersStand); got != mulereceiverplan.SeersStand {
		t.Fatalf("a missing muleAtTile read as %v, want the Seers' Village stand %v", got, mulereceiverplan.SeersStand)
	}
}

func TestMinKeysSettingFallsBackToTheDerivedDefault(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]interface{}
		want int
	}{
		{"missing", map[string]interface{}{}, lootchestfisherplan.MinKeysDefault},
		{"explicit", map[string]interface{}{"minKeys": 12}, 12},
		{"json number", map[string]interface{}{"minKeys": float64(9)}, 9},
		{"zero is not a threshold", map[string]interface{}{"minKeys": 0}, lootchestfisherplan.MinKeysDefault},
		{"negative is not a threshold", map[string]interface{}{"minKeys": -3}, lootchestfisherplan.MinKeysDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IntSetting(tc.in, "minKeys", lootchestfisherplan.MinKeysDefault)
			if got < 1 {
				got = lootchestfisherplan.MinKeysDefault
			}
			if got != tc.want {
				t.Fatalf("minKeys = %d, want %d", got, tc.want)
			}
		})
	}
}

// A blank mule means "never hand anything over" for ProgressiveFisher, but LootChestFisher
// cannot work at all without one: the mule is the only source of keys. OnStart refuses rather
// than running a loop that can never do anything.
func TestABlankMuleIsAStartupError(t *testing.T) {
	if got := fisherMuleSetting(map[string]interface{}{"mule": ""}); got != "" {
		t.Fatalf("an explicit blank mule read as %q, want an empty string", got)
	}
	if got := fisherMuleSetting(map[string]interface{}{}); got == "" {
		t.Fatal("a missing mule should fall back to the fleet default, not to blank")
	}
}

// The ship fare is the one thing that must survive a bank trip at the far end. Every long leg
// is the ship chain, so a fare deposited at Falador leaves the account with no way back to the
// mule — and a fare counted as haul sends it to the mule to offer nothing (the framework's own
// bug class #11).
func TestTheShipFareIsKeptThroughEveryBankTrip(t *testing.T) {
	b := &lootChestBot{keys: lootchestfisherrunner.KeyIDs{
		Unnoted: map[int]bool{989: true},
		Noted:   map[int]bool{990: true},
		First:   989,
	}}
	keep := b.keepIDs()
	if !keep[lootchestfisherplan.Coins] {
		t.Fatal("coins are not kept: the fare would be banked at Falador and the account could not get back to the mule")
	}
	for _, id := range []int{989, 990} {
		if !keep[id] {
			t.Fatalf("crystal key id %d is not kept: the next chest's keys would be deposited at the wrong end", id)
		}
	}
	if keep[lootchestfisherplan.UncutDragonstone] {
		t.Fatal("the chest's own loot is being kept rather than banked")
	}
	if len(keep) != 3 {
		t.Fatalf("keep holds %d ids, want exactly coins plus the two crystal key ids: %v", len(keep), keep)
	}
}
