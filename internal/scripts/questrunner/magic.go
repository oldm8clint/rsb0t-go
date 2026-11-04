package questrunner

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// Casting fights. Several quest bosses have no magic defence at all (NPC magic defaults to 1:
// the Khazard warlord, Fight Arena's beasts, Sir Mordred), and a Stage 5 ScarabSwarm account is a
// caster, so a fight step can cast instead of meleeing. Each cast is a manual OPNPCT
// (actions.CastOnNpc), which needs only the spellbook tab, never the autocast list: autocasting
// needs the combat tab to be the staff's, which content refreshes only at login, so a staff
// wielded mid-quest (after an Entrana strip, say) cannot autocast until a relog.

// staffRunes is which rune a worn elemental staff makes free.
var staffRunes = map[int]string{
	1381: "Air rune",   // staff of air
	1397: "Air rune",   // air battlestaff
	1405: "Air rune",   // mystic air staff
	1383: "Water rune", // staff of water
	1395: "Water rune",
	1403: "Water rune",
	1385: "Earth rune", // staff of earth
	1399: "Earth rune",
	1407: "Earth rune",
	1387: "Fire rune", // staff of fire
	1393: "Fire rune",
	1401: "Fire rune",
}

// BestCastableSpell is the highest-level combat spell the account's Magic level allows whose
// runes the pack holds for at least minCasts casts (a worn elemental staff pays its element).
// ok is false when nothing is castable.
func BestCastableSpell(d *Deps, minCasts int) (actions.Spell, bool) {
	return BestCastableSpellWhere(d, minCasts, nil)
}

// BestCastableSpellWhere is BestCastableSpell over the spells keep accepts (nil keeps all): the
// Horror mother's forms each take one element only.
func BestCastableSpellWhere(d *Deps, minCasts int, keep func(actions.Spell) bool) (actions.Spell, bool) {
	if minCasts < 1 {
		minCasts = 1
	}
	magic := BaseLevels(d)["magic"]
	free := map[string]bool{}
	for id := range WornCounts(d) {
		if r, ok := staffRunes[id]; ok {
			free[strings.ToLower(r)] = true
		}
	}
	pack := PackCounts(d)
	held := func(name string) int {
		n := 0
		if d.Config != nil {
			for _, id := range d.Config.ObjIDs(name) {
				n += pack[id]
			}
		}
		return n
	}
	best, found := actions.Spell{}, false
	for _, s := range actions.AutocastSpells() {
		if s.Level > magic || (keep != nil && !keep(s)) {
			continue
		}
		ok := true
		for _, r := range s.Runes {
			if free[strings.ToLower(r.Name)] {
				continue
			}
			if held(r.Name) < r.Count*minCasts {
				ok = false
				break
			}
		}
		if ok && (!found || s.Level > best.Level) {
			best, found = s, true
		}
	}
	return best, found
}

// castInterval is the most one cast is waited out: its action delay (5 ticks at 600 ms), with a
// little slack, for a session whose ticks cannot be counted.
const castInterval = 3200 * time.Millisecond

// castTicks is one combat spell's action delay in server ticks. A cast is waited out in TICKS,
// counted from the server's own PLAYER_INFO packets, not in wall-clock time: on the 200 ms lab
// engine a fixed 3.2 s was sixteen ticks, so the account cast once for every four swings the
// boss took at it, and a fight that production wins on food was a death in the lab.
const castTicks = 5

// WaitCast waits out one combat cast's action delay in server ticks (castTicks), at most
// castInterval; false when the bot is stopping. Every quest cast loop paces itself with it.
func WaitCast(d *Deps) bool {
	return waitTicks(d.Delay, d.Session.State().Tick, castTicks, castInterval)
}

// waitTicks waits until tick() has advanced by n, or max has passed, in delay steps so a stop
// still ends it. It reports false when delay does (the bot is stopping).
func waitTicks(delay Delayer, tick func() int, n int, max time.Duration) bool {
	start := tick()
	deadline := time.Now().Add(max)
	for tick()-start < n && time.Now().Before(deadline) {
		if !delay(100 * time.Millisecond) {
			return false
		}
	}
	return true
}

// FightByMagic stands on the step's Anchor (a safespot when the quest has one) and casts at the
// target until it leaves the scene, the account dies, the runes run out, or the budget ends.
// The spell is the step's Spell, or the best castable one when that is "best" or empty.
func FightByMagic(d *Deps, s questplan.Step, sus questplan.Sustain) bool {
	if s.Anchor != (tile.Tile{}) {
		here := currentTile(d)
		if here != s.Anchor {
			if tile.Chebyshev(here, s.Anchor) > 8 || here.Level != s.Anchor.Level {
				Travel(d, s.Anchor, 0)
			}
			WalkNear(d, s.Anchor, 0)
		}
	}
	npc, ok := pickFightTarget(d, s.TargetID, s.Target, 15)
	if !ok {
		d.logf("no %s in range to cast at", s.Target)
		return false
	}
	spell := s.Spell
	if spell == "" || strings.EqualFold(spell, "best") {
		best, found := BestCastableSpell(d, 5)
		if !found {
			d.logf("no castable combat spell for %s (Magic %d)", s.Target, BaseLevels(d)["magic"])
			return false
		}
		spell = best.Name
	}
	d.logf("casting %s at %s from %d,%d,%d", spell, s.Target, currentTile(d).X, currentTile(d).Z, currentTile(d).Level)
	// The eat line comes from the foe's hardest hit when the tables know it, and the account starts topped up.
	sus, foe, _ := VsFoe(d, sus, npc.ID)
	PrepareForFight(d, sus.FoodIDs, foe.PeakHit())
	f := newLiveFight(d, npc, sus)
	mark := f.mark
	deadline := mark.Add(fightTimeout)
	w := fightWatch{peak: foe.PeakHit()}
	w.observe(mark, f.View(), sus.EatAt) // the baseline for the bite counter
	for time.Now().Before(deadline) {
		if DiedSince(d, mark) {
			return false
		}
		v := f.View()
		if !v.Alive {
			return true
		}
		// A fight that is being lost is given up BEFORE the bite, as meleeFight does, so the last piece
		// of food is kept for the walk away. A cast is sent every loop, so the re-attack half of the
		// verdict has nothing to do here.
		if verdict := questplan.DecideFight(w.observe(time.Now(), v, sus.EatAt)); verdict.Action == questplan.FightRetreat {
			retreatFromFight(d, fightLabel(npc), verdict.Why)
			return false
		}
		EatIfNeeded(d, sus.FoodIDs, sus.EatAt)
		if d.Chat != nil && actions.IsNoRunesLine(d.Chat.Since(mark)) {
			d.logf("out of runes for %s", spell)
			return false
		}
		if err := actions.CastOnNpc(d.Session, spell, npc.Index); err != nil {
			d.logf("cast %s: %v", spell, err)
			return false
		}
		if !waitCastEating(d, sus) {
			return false
		}
	}
	return false
}
