package fleetplan

// KQPrayerName is the prayer PlanZeroDamage picks and the one this script holds.
//
// Magic rather than Missiles, and the reason is the target cap rather than the level: her magic
// huntall is huntall(coord, 10, 1) with NO cap, so it reaches every account within ten tiles of
// her target, while her ranged huntall is radius 5 and capped at five accounts. Protecting against
// the uncapped one is worth more to a swarm, and it is also the cheaper level (37 against 40).
const KQPrayerName = "Protect from Magic"
