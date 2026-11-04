package runecraftrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// The Guild miner's hand-over leg (runecraft P2): from the Yanille bank to the mule's post and back, on the walker's own
// graph and collision pack. It is on foot (no ship), a few hundred steps, and the same walk brings the miner back to
// Wizard Distentor for the next stretch.
func TestTheGuildMinersWalkToTheMulesPostPlansOnTheRealPack(t *testing.T) {
	r := loadReal(t)
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	planner := webwalk.NewPlanner(r.pk, g)
	plan := func(from, to tile.Tile) webwalk.Route {
		return planner.Plan(webwalk.Point{X: from.X, Z: from.Z, Level: from.Level}, webwalk.Point{X: to.X, Z: to.Z, Level: to.Level}, webwalk.Options{})
	}
	guild, _ := essenceplan.GatewayByKey(essenceplan.GatewayGuild)
	catherby := tile.Tile{X: 2809, Z: 3441}
	for _, c := range []struct {
		name     string
		from, to tile.Tile
		max      int
	}{
		{"the Yanille bank to the Catherby post", guild.Bank, catherby, 700},
		{"the Catherby post to Wizard Distentor", catherby, guild.Stand, 700},
		{"the Yanille bank to the Seers stand", guild.Bank, mulereceiverplan.SeersStand, 600},
	} {
		route := plan(c.from, c.to)
		if !route.OK || route.Cost > c.max {
			t.Errorf("%s: ok %v cost %d (%s), want at most %d", c.name, route.OK, route.Cost, route.Reason, c.max)
			continue
		}
		t.Logf("%s: cost %d", c.name, route.Cost)
	}
	// The Aubury bank to the Catherby post, for comparison.
	t.Logf("Varrock east bank to the Catherby post: cost %d", plan(essenceplan.BankStand, catherby).Cost)
}
