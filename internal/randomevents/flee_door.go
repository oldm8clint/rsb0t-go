package randomevents

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/fighterfood"
)

// When a combat flee finds nowhere to run, go back through the door the account just came in by.
//
// Live 2026-09-24, lane Q's Waterfall run 11: an account crossed the "Large door" pair at
// 2564-2565,9881 into the Waterfall dungeon's fire-giant room to answer a Mysterious Old Man, was
// hit at once by a Fire giant at 2564,9882, and logged "combat flee (walking): no reachable flee
// candidate found" thirty-five times until it died. The flee plans on the live pack (fleePack), but
// those doors TELEPORT a player across them (content's large-door script) rather than opening, so
// no live loc change ever unbakes them: on every pack the account is shut in a 112-tile room (x
// 2564..2581, z 9861..9882) where no tile is 15 from both it and the giant. Waiting and asking for
// ever-longer flees never changes that.
//
// fleeDoorBack is the fallback: a door or gate within fleeDoorRadius tiles with an "Open"-like op,
// used once; a door the account stands on or beside is the one it just crossed, since a flee starts
// the moment combat does. It reports whether the account moved through it.

// fleeDoorRadius is how close a door must be to count as the way the account came in.
const fleeDoorRadius = 2

// fleeDoorOps are the ops that take a player through a door or gate.
var fleeDoorOps = []string{"Open", "Go-through", "Pass", "Enter", "Walk-through"}

// fleeDoorCandidate is the nearest door or gate within fleeDoorRadius of x,z,level with a
// go-through op, resolved on ix with types. Pure over the loc index, for a real-pack test.
//
// It scans with Each rather than Find: Find with neither a Name nor IDs matches nothing, which is
// how the first version of this function never found a door (caught by the Waterfall real-pack
// test). A live change at a tile replaces the map file's loc there.
func fleeDoorCandidate(ix *locindex.Index, types locindex.Types, live []locindex.Change, x, z, level int) (locindex.Match, bool) {
	if ix == nil || types == nil {
		return locindex.Match{}, false
	}
	var best locindex.Match
	found, bestDist, bestRank := false, 0, 0
	consider := func(l locindex.Loc, isLive bool) {
		t, ok := types.Loc(l.ID)
		if !ok {
			return
		}
		name := strings.ToLower(t.Name)
		if !strings.Contains(name, "door") && !strings.Contains(name, "gate") || strings.Contains(name, "trapdoor") {
			return
		}
		// A door into known danger is no way out (review, 2026-09-25).
		if p := (pathfind.NavPoint{X: l.X, Z: l.Z, Level: l.Level}); inDangerZone(p) || inWolfTerritory(p) {
			return
		}
		d := chebyshev(x, z, l.X, l.Z)
		if d > fleeDoorRadius {
			return
		}
		for rank, label := range fleeDoorOps {
			op := cachecfg.OpIndex(t.Ops, label)
			if op == 0 {
				continue
			}
			if !found || d < bestDist || (d == bestDist && rank < bestRank) {
				best = locindex.Match{Loc: l, Name: t.Name, Op: op, Distance: d, Live: isLive}
				found, bestDist, bestRank = true, d, rank
			}
			return
		}
	}
	changed := map[[2]int]bool{}
	for _, c := range live {
		if c.Level == level {
			changed[[2]int{c.X, c.Z}] = true
		}
	}
	r := fleeDoorRadius
	ix.Each(x-r, z-r, x+r, z+r, level, func(l locindex.Loc) {
		if !changed[[2]int{l.X, l.Z}] {
			consider(l, false)
		}
	})
	for _, c := range live {
		if !c.Deleted && c.Level == level {
			consider(locindex.Loc{ID: c.ID, X: c.X, Z: c.Z, Level: c.Level, Shape: c.Shape, Angle: c.Angle}, true)
		}
	}
	return best, found
}

// fleeDoorBack goes back through the nearest door (fleeDoorCandidate), and reports whether the
// account's tile then moved to the far side of it, away from the threat at threatX,threatZ.
func fleeDoorBack(s actions.Session, threatX, threatZ int, log func(string)) bool {
	ix := currentLocIndex()
	res := currentResolver()
	types, ok := res.(locindex.Types)
	if ix == nil || !ok {
		return false
	}
	x, z, level, ok := s.State().Tile()
	if !ok {
		return false
	}
	live := locindex.ChangesFrom(s.State().LocChanges())
	m, found := fleeDoorCandidate(ix, types, live, x, z, level)
	if !found || recentlyFledThrough(s, m.X, m.Z, m.Level) {
		return false
	}
	noteFledThrough(s, m.X, m.Z, m.Level)
	if log != nil {
		log(fmt.Sprintf("combat flee: nowhere to run from %d,%d,%d — going back through the %s at %d,%d", x, z, level, locName(types, m.ID), m.X, m.Z))
	}
	if err := actions.OpLoc(s, m.Op, m.X, m.Z, m.ID); err != nil {
		return false
	}
	before := chebyshev(x, z, threatX, threatZ)
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(fleeWalkPollInterval)
		if nx, nz, nl, ok := s.State().Tile(); ok && (nx != x || nz != z || nl != level) && chebyshev(nx, nz, threatX, threatZ) >= before {
			return true
		}
	}
	return false
}

// fleeDoorMemory is how long a door an account fled through stays unusable to it. Without it an
// account attacked again on the far side found the same door within reach and went straight
// back into the room it had just left (review, 2026-09-25).
const fleeDoorMemory = 60 * time.Second

type fledDoor struct {
	x, z, level int
	at          time.Time
}

// fledDoors maps actions.Session -> fledDoor, recentCombat's per-session shape.
var fledDoors sync.Map

func noteFledThrough(s actions.Session, x, z, level int) {
	fledDoors.Store(s, fledDoor{x, z, level, time.Now()})
}

func recentlyFledThrough(s actions.Session, x, z, level int) bool {
	v, ok := fledDoors.Load(s)
	if !ok {
		return false
	}
	d := v.(fledDoor)
	return d.x == x && d.z == z && d.level == level && time.Since(d.at) < fleeDoorMemory
}

func locName(types locindex.Types, id int) string {
	if l, ok := types.Loc(id); ok && l.Name != "" {
		return fmt.Sprintf("%q", l.Name)
	}
	return fmt.Sprintf("loc %d", id)
}

// holdGroundResolver is what fleeHoldGround needs from the registered name resolver (in practice
// *cachecfg.Config): object options for food and NPC options for the attack.
type holdGroundResolver interface {
	Obj(id int) (cachecfg.Obj, bool)
	Npc(id int) (cachecfg.Npc, bool)
}

// holdGroundEatBelow is the share of max hitpoints below which a cornered account eats instead of
// fighting back.
const holdGroundEatBelow = 0.6

// holdGroundDialogueEatBelow is the share under which an account eats even with a dialogue open: eating closes the dialogue (the
// engine's OpHeldHandler clears the pending action), which is why the rule keeps a random event's dialogue open at 60%, and is the
// right price at a quarter of the pool, where the next two hits are the end of it.
const holdGroundDialogueEatBelow = 0.25

// fleeHoldGround is the last fallback when there is truly nowhere to go and the account is being
// hit: eat when hurt, otherwise fight back, but only when holdGroundChoice allows it. Standing
// still under a Fire giant was the death (lane Q, Waterfall run 11). Reports what it did ("" for
// nothing).
func fleeHoldGround(s actions.Session, log func(string)) string {
	return fleeHoldGroundAllowing(s, log, true)
}

// fleeEatIfLow eats one bite, on holdGroundChoice's rule, while a flee is still under way, and
// never fights. The walking flee used to eat only when it had nowhere at all to go: live
// 2026-09-29, acct126 fled a Strange plant among the West Ardougne Paladins, moved 0 tiles on
// three legs in a row and died without a bite, with food in the pack.
func fleeEatIfLow(s actions.Session, log func(string)) bool {
	return fleeHoldGroundAllowing(s, log, false) == "ate"
}

func fleeHoldGroundAllowing(s actions.Session, log func(string), allowFight bool) string {
	res, ok := currentResolver().(holdGroundResolver)
	if !ok {
		return ""
	}
	st := s.State()
	hp, hpKnown := st.HP()
	max, maxKnown := st.MaxHP()
	_, _, chat := st.Modals()
	var food []holdGroundFood
	if inv := st.Inventory(actions.PlayerPack); inv != nil {
		for _, it := range inv.NonEmpty() {
			if o, ok := res.Obj(int(it.ID)); ok {
				food = append(food, holdGroundFood{id: it.ID, slot: it.Slot, name: o.Name, op: o.IOps[0]})
			}
		}
	}
	att, attFound := currentAttacker(st)
	var npc cachecfg.Npc
	if attFound {
		npc, attFound = res.Npc(att.Type)
	}
	own, ownKnown := st.CombatLevel()
	lag := st.RecentViewLag().Ticks
	c := holdGroundChoice(holdGroundView{
		hp: hp, max: max, hpKnown: hpKnown && maxKnown, dialogueOpen: chat != gamestate.NoModal,
		food: food, attacker: att, attackerNpc: npc, haveAttacker: attFound,
		ownLevel: own, ownKnown: ownKnown,
		lagTicks: lag, lagHits: facingHitSum(st, res, lag, max, hp),
	})
	switch c.kind {
	case "eat":
		if !fleeEatBite(s, c.food, log) {
			return ""
		}
		if log != nil {
			where := "cornered"
			if !allowFight {
				where = "still fleeing"
			}
			log(fmt.Sprintf("combat flee: %s at %d/%d hitpoints — eating %s", where, hp, max, c.food.name))
		}
		return "ate"
	case "fight":
		if !allowFight {
			return ""
		}
		if actions.OpNpc(s, c.op, att.Index) != nil {
			return ""
		}
		if log != nil {
			log(fmt.Sprintf("combat flee: cornered — fighting back against %s (level %d)", npc.Name, npc.Level))
		}
		return "fought"
	}
	return ""
}

type holdGroundFood struct {
	id       int32
	slot     int
	name, op string
}

// holdGroundView is what holdGroundChoice decides from, read off the session by fleeHoldGround.
type holdGroundView struct {
	hp, max      int
	hpKnown      bool
	dialogueOpen bool
	// lagTicks is the most ticks one view lag episode hid in the last gamestate.ViewLagMemory (0 for none) and lagHits the sum of
	// the worst single hit of the NPCs facing the account (0 when unknown): after a lag the eat share is raised to what they could
	// have done in the episode (fighterfood.LagEatFrac).
	lagTicks, lagHits int
	food              []holdGroundFood
	attacker          gamestate.NPC
	attackerNpc       cachecfg.Npc
	haveAttacker      bool
	ownLevel          int
	ownKnown          bool
}

type holdGroundDecision struct {
	kind string // "eat", "fight" or "" for nothing
	food holdGroundFood
	op   int
}

// facingHitSum is the sum of the worst single hit of every NPC facing the account, for the lag-aware eat share: the client cache
// carries levels, not strengths, so each is fighterfood.LevelMaxHit of its level (the Watchman event of combat 61 to 90 is level 120,
// 11), and never under fighterfood.LagUnseenHit, which also stands for one unnamed hitter when none faces the account. 0 with no lag in
// the memory or hitpoints over the lag cap, where the share could not change the answer: the NPCs are not listed then.
func facingHitSum(st *gamestate.State, res holdGroundResolver, lagTicks, maxHP, hp int) int {
	if lagTicks <= 0 || maxHP <= 0 || hp > fighterfood.LagCap(maxHP) {
		return 0
	}
	sum, self := 0, st.Self()
	for _, n := range st.NPCs() {
		if !n.TargetsPlayer(self) {
			continue
		}
		hit := fighterfood.LagUnseenHit
		if info, ok := res.Npc(n.Type); ok {
			hit = max(hit, fighterfood.LevelMaxHit(info.Level))
		}
		sum += hit
	}
	if sum == 0 {
		sum = fighterfood.LagUnseenHit
	}
	return sum
}

// holdGroundChoice is fleeHoldGround's decision, pure (review, 2026-09-25):
//
//   - Eat below holdGroundEatBelow of max hitpoints, the food that heals most first (bestFood), never while a dialogue is open
//     (eating closes it, and a random event's dialogue must not be dismissed) unless under holdGroundDialogueEatBelow, and never
//     anything poisoned or the Strange fruit.
//   - Fight back only against a monster at or below the account's own combat level. A combat-3
//     account "fighting back" against a level-64 wolf only dies facing it, and auto retaliate
//     already answers hits.
//   - Never attack a random event's monster (hostileEventNpcIDs): the evade handler runs this very
//     flee for them, and attacking one both breaks the event rule and walks the account toward it.
func holdGroundChoice(v holdGroundView) holdGroundDecision {
	eatBelow := holdGroundEatBelow
	if v.lagTicks > 0 {
		// lageat-1018: a view that has lagged lately hides the hits it has taken; the share rises to what the NPCs on the account
		// could have done in the worst episode, up to 70%.
		eatBelow = fighterfood.LagEatFrac(v.max, holdGroundEatBelow, v.lagHits, v.lagTicks)
	}
	if v.hpKnown && v.max > 0 && float64(v.hp) < eatBelow*float64(v.max) && (!v.dialogueOpen || float64(v.hp) < holdGroundDialogueEatBelow*float64(v.max)) {
		if f, ok := bestFood(v.food); ok {
			return holdGroundDecision{kind: "eat", food: f}
		}
	}
	if !v.haveAttacker || hostileEventNpcIDs[v.attacker.Type] {
		return holdGroundDecision{}
	}
	if !v.ownKnown || v.attackerNpc.Level <= 0 || v.attackerNpc.Level > v.ownLevel {
		return holdGroundDecision{}
	}
	op := cachecfg.OpIndex(v.attackerNpc.Ops, "Attack")
	if op == 0 {
		return holdGroundDecision{}
	}
	return holdGroundDecision{kind: "fight", op: op}
}

// isDrinkFood is a food whose first op is Drink rather than Eat (Jug of wine): fighterfood's
// table decides, so a potion, whose op is Drink too, is never drunk as food.
func isDrinkFood(f holdGroundFood) bool {
	_, ok := fighterfood.BaseFood(f.name)
	return ok && strings.EqualFold(f.op, "Drink")
}

// edibleHeal is what one bite of f heals for ranking, and whether f is something to eat for hitpoints at all.
//
// Live 18 Oct 2026, the death at the Fishing Guild bank: the account's pack held one Kebab (4 hitpoints at 24 maximum), a Beer
// and twenty-one raw fish, and the flee ate "the first edible in pack order", which would have been the Strange fruit the plant
// gives (iop1 Eat, restores run energy and nothing else) had it been picked, and was a Kebab only because nothing else qualified.
// So a known food (fighterfood's table: content's own heals) ranks by what it heals; an unknown item with an Eat option ranks last
// at one; a poisoned item, the Strange fruit, a drink that is not a food (a Beer, a potion) and anything raw never qualify.
func edibleHeal(f holdGroundFood) (heal int, ok bool) {
	lower := strings.ToLower(strings.TrimSpace(f.name))
	if strings.Contains(lower, "poison") || lower == "strange fruit" || strings.HasPrefix(lower, "raw ") {
		return 0, false
	}
	if _, known := fighterfood.BaseFood(f.name); known {
		if strings.EqualFold(f.op, "Eat") || isDrinkFood(f) {
			return fighterfood.FoodHealAmount(f.name), true
		}
		return 0, false
	}
	if strings.EqualFold(f.op, "Eat") {
		return 1, true
	}
	return 0, false
}

// bestFood is the food that heals most (the first of equals, in pack order): a hurt account under attack takes the bite that
// buys the most time, not the one that happens to come first in the pack.
func bestFood(food []holdGroundFood) (holdGroundFood, bool) {
	var best holdGroundFood
	bestHeal, found := 0, false
	for _, f := range food {
		heal, ok := edibleHeal(f)
		if ok && (!found || heal > bestHeal) {
			best, bestHeal, found = f, heal, true
		}
	}
	return best, found
}

// fleeEatConfirmWait is how long a flee's bite is waited on for its heal to show: a tick and a half past the click, short enough
// that the walk it interrupts is not left unwatched. A bite that shows later is credited by the ledger and never eaten twice. A
// variable only so a test can shorten it; nothing else assigns it.
var fleeEatConfirmWait = 1200 * time.Millisecond

// fleeEatBite eats f through the bite ledger (actions.EatBite) instead of a bare click, as every other eater does since 15 Oct:
// it credits a bite that landed late rather than sending a second one, waits out a delay a cast or a rub is known to hold (the
// engine drops an eat inside one), logs a bite that took seconds to show, and records the lag. It reports whether a bite is known
// to have landed or is still on its way (a click went out and may yet show), false when nothing was sent.
func fleeEatBite(s actions.Session, f holdGroundFood, log func(string)) bool {
	st := s.State()
	if st == nil {
		return false
	}
	env := actions.BiteEnv{Session: s}
	if log != nil {
		env.Logf = func(format string, args ...any) { log("combat flee: " + fmt.Sprintf(format, args...)) }
	}
	ok := actions.EatBite(env, actions.BiteRequest{
		Food: f.name,
		Wait: fleeEatConfirmWait,
		Pick: func() (actions.BitePick, bool) {
			inv := st.Inventory(actions.PlayerPack)
			if inv == nil {
				return actions.BitePick{}, false
			}
			slot, count := -1, 0
			for _, it := range inv.NonEmpty() {
				if it.ID != f.id {
					continue
				}
				if slot < 0 || it.Slot == f.slot {
					slot = it.Slot
				}
				count += int(it.Count)
			}
			if slot < 0 {
				return actions.BitePick{}, false
			}
			return actions.BitePick{Obj: f.id, Slot: slot, Changed: func() bool {
				now := 0
				if inv := st.Inventory(actions.PlayerPack); inv != nil {
					for _, it := range inv.NonEmpty() {
						if it.ID == f.id {
							now += int(it.Count)
						}
					}
				}
				return now < count
			}}, true
		},
	})
	if ok {
		return true
	}
	_, inFlight := actions.BiteInFlight(st)
	return inFlight
}
