package fleetplan

// The Queen's own numbers.
const (
	// KQHitpointsPerPhase is one phase's health. She has two.
	KQHitpointsPerPhase = 255
	// KQPhases is the phase count: ai_queue3 changes her into kalphite_flyingqueen and
	// npc_statheal(hitpoints, 0, 100) puts her back to full.
	KQPhases = 2
	// KQMaxHit is randominc(31) for both her magic and her ranged attack.
	KQMaxHit = 31
	// KQAttackTicks is combat.param's attackrate default of 4, which she does not override.
	KQAttackTicks = 4
	// KQMagicHuntRadius is huntall(coord, 10, 1) in her mage attack: every player inside it is hit.
	KQMagicHuntRadius = 10
	// KQRangedHuntRadius and KQRangedTargets are huntall(coord, 5, 1) and the $i < 5 cap.
	KQRangedHuntRadius = 5
	KQRangedTargets    = 5
	// KQMagicLevel and KQMagicDefence are her magic stat and magicdefence param.
	KQMagicLevel   = 150
	KQMagicDefence = 100
	// KQDefenceLevel and the three melee defence bonuses, for the comparison in KQAttackRolls.
	KQDefenceLevel  = 300
	KQStabDefence   = 50
	KQSlashDefence  = 50
	KQCrushDefence  = 10
	KQRangedDefence = 100
	// PlayerAutocastRange is player_attackrange's own answer while autocasting: exactly 10, the
	// same radius as her magic huntall. Standing "out of her reach and plinking" is therefore not
	// available on the numbers alone — whether her huntall measures from her south-west corner
	// while the player's range measures from her five-tile footprint is a GEOMETRY question the
	// content does not settle, and cmd/scarabstage3test -phases kqstand is what asks the engine.
	PlayerAutocastRange = 10
)
