package fleetplan

// SpawnWindowSettings tunes the two hard rules.
type SpawnWindowSettings struct {
	// Enabled is the whole feature's switch, default off until a lab run says otherwise.
	Enabled bool `json:"kqSpawnWindow"`
	// HPFloorPercent is the share of MaxHP below which the window is spent resting or eating
	// rather than fighting. Her max hit is 31, so an account this cohort's size is one roll from
	// death whatever it does; the floor is about not compounding that.
	HPFloorPercent int `json:"kqSpawnWindowHpFloorPercent"`
	// MaxSideRange is how far from the zero-damage stand an account may wander for side work.
	// Beyond this the swarm stops being a swarm and starts being a column.
	MaxSideRange int `json:"kqSpawnWindowRange"`
	// AbandonDamageRatio is the "not more damage than it earns" rule, as damage per loot taken.
	// Above it, side fighting turns itself off for the rest of the life.
	AbandonDamageRatio int `json:"kqSpawnWindowAbandonRatio"`
	// PreferCacti puts the free, harmless loot ahead of the fight.
	PreferCacti bool `json:"kqSpawnWindowPreferCacti"`
}

// DefaultSpawnWindowSettings is deliberately OFF. Nothing in this file has been run against a
// live cohort yet, and an unproven feature that costs damage is exactly the sort of thing this
// repo's own notes warn about being switched on by default.
func DefaultSpawnWindowSettings() SpawnWindowSettings {
	return SpawnWindowSettings{
		Enabled:            false,
		HPFloorPercent:     60,
		MaxSideRange:       12,
		AbandonDamageRatio: 8,
		PreferCacti:        true,
	}
}
