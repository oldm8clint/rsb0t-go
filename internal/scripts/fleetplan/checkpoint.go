package fleetplan

// The Scarab Swarm stage numbers, matching docs/fleet_expansion_2026-09-18.md's own table
// order. Only StageTutorialFunds has a decider (this package) and a runner
// (the ScarabSwarm runner package) as of this writing — the rest exist here purely as named
// destinations a persisted Checkpoint can point at once a future session builds them, so this
// package's own shape does not need to change when that happens.
const (
	StageTutorialFunds  = 1 // "Tutorial / starter funds" — this package's own Phase machine.
	StageFoodAndFunds   = 2 // Westward traversal to East Ardougne, bakery stall thieving to 55.
	StageMagicPrep      = 3 // Aubury runes, Zaff staff, initial 13 Magic.
	StageRuneSupply     = 4 // Yanille Wizard Guild at 66 Magic.
	StageSurvivalCombat = 5 // Waterfall, Hill Giants, Prayer via bone burying.
	StageKQEntry        = 6 // Shantay pass, rope coordination, group entry.
	StageCannonSupply   = 7 // Optional: Dwarf Cannon quest, cannonball smithing.
	StageMiningSmithing = 8 // Iron/coal near Yanille, superheat, cannonballs.
)
