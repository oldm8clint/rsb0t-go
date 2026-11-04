package fleetplan

import "rsb0t-go/internal/scripts/fighterfood"

// EatLineLag is EatLine against a view whose worst recent episode hid lagTicks ticks (gamestate.RecentLag.Ticks; 0 for none): the
// line EatLine gives, raised to what hits for fighterfood.LagVolleys(lagTicks) volleys would take, up to fighterfood.LagCapPercent
// of the maximum. With nothing facing the account the lag adds nothing: there is nothing to hit it that the view could have hidden.
func EatLineLag(maxHP int, hits []int, lagTicks int) int {
	line := EatLine(maxHP, hits)
	if lagTicks <= 0 || len(hits) == 0 {
		return line
	}
	sum := 0
	for _, h := range hits {
		sum += max(h, 0)
	}
	return fighterfood.LagEatLine(maxHP, line, sum, lagTicks)
}

// EatDueLag is EatDue against EatLineLag.
func EatDueLag(hp, maxHP int, hits []int, lagTicks int) bool {
	return maxHP > 0 && hp > 0 && hp <= EatLineLag(maxHP, hits, lagTicks)
}

// EventTierStrength is the strength of tiers 1 to 6 of every random event monster that scales with the account's combat level: content's
// macro_watchman1 to 6 and the river troll, rock golem, zombie, shade and tree spirit blocks beside them (antimacro.npc; no strength
// bonus, attackrate the default 4). TestEventMonsterMaxHitsMatchContent re-derives it from a checkout.
var EventTierStrength = [6]int{9, 22, 38, 65, 100, 130}

// EventBlockStart is the NPC type of tier 1 of each of those families (river troll 391, rock golem 413, zombie 419, shade 425, Watchman
// 431, tree spirit 438; each is six consecutive types, tier 1 to 6), npc.pack's order and the ids randomevents' hostileEventNpcIDs holds.
var EventBlockStart = [...]int{391, 413, 419, 425, 431, 438}

// EventMonsterMaxHit is the most one hit of a random event monster of this NPC type deals, from its tier's strength; false for a type
// that is not one of the six families.
func EventMonsterMaxHit(npcType int) (int, bool) {
	for _, start := range EventBlockStart {
		if npcType >= start && npcType < start+len(EventTierStrength) {
			return NPCMaxHit(EventTierStrength[npcType-start], 0), true
		}
	}
	return 0, false
}

// NPCTypeMaxHit is the most one melee hit of the NPC of this type and name deals: the event monsters' tier hit by type (a Watchman
// event of combat 61 to 90 hits for 11, where the Yanille Watchman it shares a name with hits for 4), else MaxHitOf the name.
func NPCTypeMaxHit(npcType int, name string) int {
	if h, ok := EventMonsterMaxHit(npcType); ok {
		return h
	}
	return MaxHitOf(name)
}

// WatchmanEventTier is the index into macroWatchmanStrength the event uses for an account of this combat level: content's
// macro_event_combat_level (macro_events.rs2) steps at 11, 21, 41, 61 and 91.
func WatchmanEventTier(combat int) int {
	switch {
	case combat <= 0:
		return 5 // unknown: the hardest, as an unnamed attacker is assumed hard
	case combat < 11:
		return 0
	case combat < 21:
		return 1
	case combat < 41:
		return 2
	case combat < 61:
		return 3
	case combat < 91:
		return 4
	}
	return 5
}

// WatchmanEventMaxHit is the most one hit of the Watchman event ("you are under arrest!") deals an account of this combat level:
// 11 for combat 61 to 90, which is what killed the 18 Oct 04:41 account (type 435, macro_watchman5). Content raises it from
// macro_randomthieving, which pickpocket.rs2 (attempt_pick_pocket), stealing.rs2 and trapped_chest.rs2 call on an afk_event roll,
// npc_adds it one tile from the thief and sets it on the thief at once (opplayer2). It is hit-for-hit what the thief meets while
// it steals, not only where a failed steal is hit back.
func WatchmanEventMaxHit(combat int) int {
	return NPCMaxHit(EventTierStrength[WatchmanEventTier(combat)], 0)
}

// ThievingHits is the worst single hit of each thing a steal can bring down on the account, for a lag-aware eat line: the Watchman
// the steal can raise (WatchmanEventMaxHit for the account's combat level, 0 for unknown, which is the highest tier) and the
// rung's own failed-steal damage (stunDamage: 3 on a Paladin or Knight, 4 on a Hero).
func ThievingHits(combat, stunDamage int) []int {
	return []int{WatchmanEventMaxHit(combat), max(stunDamage, 0)}
}

// lagEatBelow raises an eat threshold of the decider's kind (a bite when HP < eat) for a view that has lagged lately: the line it
// stands for is eat-1, raised by fighterfood.LagEatLine to what hitSum could have done in the worst recent episode, and never above the
// maximum. 0 (never eat) stays 0, and with no lag in the memory or nothing that can hit the account the threshold is returned as it was.
func (s Stage3Snapshot) lagEatBelow(eat, hitSum int) int {
	if eat <= 0 || s.LagTicks <= 0 || s.MaxHP <= 0 {
		return eat
	}
	line := fighterfood.LagEatLine(s.MaxHP, eat-1, hitSum, s.LagTicks)
	return min(max(eat, line+1), s.MaxHP)
}

// SumHits adds up hits, ignoring negatives.
func SumHits(hits ...int) int {
	n := 0
	for _, h := range hits {
		n += max(h, 0)
	}
	return n
}
