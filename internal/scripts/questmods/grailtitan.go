package questmods

import (
	"fmt"
	"regexp"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The Black Knight Titan, the I/O half. The rules, the facts behind them and the reason every
// account of 13 Oct 2026 stood on 2793,4721 swinging at nothing are questplan/grailtitan.go's: the
// account casts at him from a tile it walked to itself (the server's own walk to him dead-ends),
// with Excalibur worn, from two or more tiles off, and the quest is parked only for a reason that
// names the new fight.

// titanGroundData is the arrival pocket's line of sight and cast tiles, built once from the static
// pack, loc index and config.
var titanGroundData struct {
	sync.Mutex
	built bool
	sight func(x, z int) fleetplan.Sight
	cands []questplan.TitanCastTile
}

// titanGround is the line of sight casts at him trust (the pack's wall faces and the loc index's
// projectile blockers, scarabplan.BlockerSight) and the cast tiles, ranked. nil without a pack, loc
// index or config: then no cast is judged and the fight goes to the sword.
func titanGround(d *questrunner.Deps) (func(x, z int) fleetplan.Sight, []questplan.TitanCastTile) {
	if d == nil || d.Pack == nil || d.Locs == nil || d.Config == nil {
		return nil, nil
	}
	g := &titanGroundData
	g.Lock()
	defer g.Unlock()
	if g.built {
		return g.sight, g.cands
	}
	pk := d.Pack
	at := questplan.TitanTile
	blocked := map[[2]int]bool{}
	r := questplan.TitanCastMax + fleetplan.CastRange
	d.Locs.Each(at.X-r, at.Z-r, at.X+r, at.Z+r, at.Level, func(l locindex.Loc) {
		if l.Shape < 9 || l.Shape > 21 {
			return
		}
		t, ok := d.Config.Loc(l.ID)
		if !ok || !t.BlockRange {
			return
		}
		w, h := max(t.Width, 1), max(t.Length, 1)
		if l.Angle == 1 || l.Angle == 3 {
			w, h = h, w
		}
		for dx := 0; dx < w; dx++ {
			for dz := 0; dz < h; dz++ {
				blocked[[2]int{l.X + dx, l.Z + dz}] = true
			}
		}
	})
	sight := fleetplan.BlockerSight(
		func(x, z int) uint8 { return pk.WallMask(x, z, at.Level) },
		func(x, z int) bool { return blocked[[2]int{x, z}] })
	// The cast tiles are the arrival pocket's ground: the account that arrives walks to any of them.
	pocket := pk.Flood(pathfind.NavPoint{X: questplan.TitanArrival.X, Z: questplan.TitanArrival.Z, Level: at.Level}, 40)
	g.sight, g.built = sight, true
	g.cands = questplan.TitanCastTiles(func(t tile.Tile) bool {
		return pocket[[2]int{t.X, t.Z}] && questplan.OnTitanSide(t)
	}, sight)
	return g.sight, g.cands
}

// The server's refusals of a cast on him that are another caster's turn, not an error.
var titanContended = regexp.MustCompile(`(?i)someone else is fighting that|already under attack`)

// titanMeleeBudget is the longest the sword fallback runs on its own.
const titanMeleeBudget = 25 * time.Minute

// titanNoSwingBail ends a melee in which the account has not swung once for this long (the first
// attack never started): from the approach tile the first click is one step from the attack tile,
// so three minutes of nothing is something else.
const titanNoSwingBail = 3 * time.Minute

// titanFight is one fight with the Titan.
type titanFight struct {
	d     *questrunner.Deps
	e     *questrunner.Engine
	sus   questplan.Sustain
	start time.Time
	mark  time.Time
	cands []questplan.TitanCastTile
	stand tile.Tile
	// standFails counts failed walks to the stand.
	standFails int

	titan       entityquery.Entity
	unseenSince time.Time
	// prevSeen is whether he was in view at the last poll; fellAt is when he left view at low health.
	prevSeen bool
	fellAt   time.Time

	spell     questplan.TitanSpell
	haveSpell bool
	// casts counts casts the server took; contended the refusals of another caster's turn.
	casts, contended int
	// noRunes is set when the server refused a cast for want of runes (or Magic level).
	noRunes bool
	// contendedNow is whether the last cast was refused as another caster's turn.
	contendedNow   bool
	contendedSince time.Time
	meleeTried     bool
	castErrs       int
	// logged is the cast count the last progress line was written at.
	logged int
	// health and total are the last health bar seen on him.
	health, total int
}

// grailTitanFight kills the Titan by spells from a tile out of his reach, with the sword as a
// fallback, and reports whether the account is across his bridge. On a failure it blows the account
// out of the realm and parks the quest for the verdict's time under a reason that names the new
// fight.
func grailTitanFight(e *questrunner.Engine) bool {
	d := e.Deps()
	f := &titanFight{d: d, e: e, sus: questplan.HolyGrail.Sustain, start: time.Now(), mark: time.Now()}
	_, f.cands = titanGround(d)
	questrunner.ClearLeftoverModal(d)
	healToFull(d, f.sus.FoodIDs)
	if st, ok := questplan.ChooseTitanStand(questrunner.CurrentTile(d), f.cands); ok {
		f.stand = st
	}
	f.pickSpell()
	// The runes must be able to kill him as he stands: the gate DecideHolyGrail applies before the
	// whistles, applied again here for an account found in the pocket on a pack it did not weigh (a
	// begun fight is judged against the health he has left, so a resumed one is not turned away).
	magic, pack := questrunner.BaseLevels(d)["magic"], questrunner.PackCounts(d)
	health, total := 0, 0
	if t, seen := questrunner.FindNpcByID(d, questplan.NpcTitan, "", 24); seen && t.TotalHealth > 0 {
		health, total = t.Health, t.TotalHealth
	}
	enough, why := questplan.TitanRunesEnough(magic, pack, health, total)
	if !enough {
		logf(d, "the Titan: %s: leaving before the fight", why)
		f.giveUp(questplan.TitanVerdict{Act: questplan.TitanQuit, Why: why, ParkFor: questplan.TitanParkKit})
		return false
	}
	logf(d, "the Titan: the runes are enough: %s", why)
	if f.haveSpell {
		logf(d, "the Titan: casting %s (%d casts of runes held, Magic %d) from %d,%d; %d cast tiles in the pocket",
			f.spell.Name, f.affordable(), questrunner.BaseLevels(d)["magic"], f.stand.X, f.stand.Z, len(f.cands))
	} else {
		logf(d, "the Titan: no spell the pack has runes for (Magic %d): the sword from %d,%d if it comes to it",
			questrunner.BaseLevels(d)["magic"], questplan.TitanMeleeApproach.X, questplan.TitanMeleeApproach.Z)
	}
	return f.run()
}

// affordable is how many casts of the chosen spell the pack pays for.
func (f *titanFight) affordable() int { return f.spell.Casts(questrunner.PackCounts(f.d)) }

// pickSpell chooses the spell the pack's runes pay the most damage with, and keeps it once the fight
// has begun until its runes are spent (questplan.NextTitanSpell).
func (f *titanFight) pickSpell() {
	sp, ok := questplan.NextTitanSpell(f.spell, f.casts > 0, questrunner.BaseLevels(f.d)["magic"], questrunner.PackCounts(f.d))
	if ok && f.haveSpell && sp.Name != f.spell.Name {
		logf(f.d, "the Titan: now casting %s (%s's runes are spent)", sp.Name, f.spell.Name)
	}
	f.spell, f.haveSpell = sp, ok
}

// reading polls the world for questplan.DecideTitanFight.
func (f *titanFight) reading(here tile.Tile) questplan.TitanReading {
	d := f.d
	questrunner.EatIfNeeded(d, f.sus.FoodIDs, f.sus.EatAt)
	f.pickSpell()
	r := questplan.TitanReading{
		Excalibur: excaliburWorn(d),
		Died:      questrunner.DiedSince(d, f.mark),
		Past:      questplan.InGrailRealm(here) && !questplan.OnTitanSide(here),
		Here:      here, Stand: f.stand,
		HaveSpell:   f.haveSpell,
		NoRunesSaid: f.noRunes,
		Contended:   f.contendedNow,
		Elapsed:     time.Since(f.start),
		MeleeTried:  f.meleeTried,
	}
	if !f.contendedSince.IsZero() {
		r.ContendedFor = time.Since(f.contendedSince)
	}
	if t, seen := questrunner.FindNpcByID(d, questplan.NpcTitan, "", 24); seen {
		f.titan, f.unseenSince = t, time.Time{}
		r.TitanSeen = true
		if t.TotalHealth > 0 {
			f.health, f.total = t.Health, t.TotalHealth
		}
	} else {
		if f.unseenSince.IsZero() {
			f.unseenSince = time.Now()
		}
		r.UnseenFor = time.Since(f.unseenSince)
		// Last seen nearly dead and now gone: he fell. The pass teleport is part of his death queue.
		if f.prevSeen && f.casts > 0 && f.total > 0 && f.health*100 <= f.total*questplan.TitanFellHealthPct && f.fellAt.IsZero() && !r.Past {
			f.fellAt = time.Now()
			logf(d, "the Titan: left view at %d/%d health after %d casts: he fell; waiting %s for the pass", f.health, f.total, f.casts, questplan.TitanFellGrace)
		}
	}
	f.prevSeen = r.TitanSeen
	r.Fell = !f.fellAt.IsZero() && !r.Past && time.Since(f.fellAt) >= questplan.TitanFellGrace
	return r
}

// run is the fight's loop: ask the rules what next, do it, until the account is across or the rules
// give it up.
func (f *titanFight) run() bool {
	d := f.d
	for {
		here := questrunner.CurrentTile(d)
		if !questplan.InGrailRealm(here) {
			logf(d, "the Titan: the account is no longer in the realm (at %d,%d,%d) after %d casts", here.X, here.Z, here.Level, f.casts)
			return false
		}
		r := f.reading(here)
		v := questplan.DecideTitanFight(r)
		switch v.Act {
		case questplan.TitanPassed:
			f.summary("passed")
			return true
		case questplan.TitanQuit:
			f.summary("not passed: " + v.Why)
			f.giveUp(v)
			return false
		case questplan.TitanWait:
			wait := 1200 * time.Millisecond
			if r.Contended {
				// Another caster's turn: let it run, then try again (a refused cast costs nothing).
				wait, f.contendedNow = 2400*time.Millisecond, false
			}
			if !d.Delay(wait) {
				return false
			}
		case questplan.TitanWalk:
			f.walkToStand()
		case questplan.TitanMelee:
			logf(d, "the Titan: %s: the sword", v.Why)
			f.meleeTried = true
			titanMelee(d, f.titan, f.start.Add(questplan.TitanFightBudget), f.mark)
		case questplan.TitanCast:
			if !f.cast() {
				return false
			}
		}
		if f.casts-f.logged >= 10 {
			f.logged = f.casts
			f.progress()
		}
	}
}

// walkToStand walks onto the cast tile by the account's own movement. An Attack or a cast sent from
// anywhere else makes the SERVER walk the account, and its walk to him dead-ends on 2793,4721.
func (f *titanFight) walkToStand() {
	d := f.d
	if !questrunner.WalkNear(d, f.stand, 0) {
		f.standFails++
		logf(d, "the Titan: could not walk onto the cast tile %d,%d (%d times)", f.stand.X, f.stand.Z, f.standFails)
		if f.standFails >= 3 {
			f.stand = tile.Tile{}
		}
		d.Delay(1200 * time.Millisecond)
	}
}

// cast sends one cast and judges the server's answer.
func (f *titanFight) cast() bool {
	d := f.d
	at := time.Now()
	if err := actions.CastOnNpc(d.Session, f.spell.Name, f.titan.Index); err != nil {
		f.castErrs++
		logf(d, "the Titan: casting %s: %v", f.spell.Name, err)
		if f.castErrs >= 3 {
			f.noRunes = true
		}
		return d.Delay(1200 * time.Millisecond)
	}
	if !questrunner.WaitCast(d) {
		return false
	}
	lines := ""
	if d.Chat != nil {
		lines = d.Chat.Since(at)
	}
	switch {
	case actions.IsNoRunesLine(lines) || actions.IsSpellLevelLine(lines):
		logf(d, "the Titan: the server will not cast %s (%s) after %d casts", f.spell.Name, firstLine(lines), f.casts)
		f.noRunes = true
	case titanContended.MatchString(lines):
		f.contended++
		f.contendedNow = true
		if f.contendedSince.IsZero() {
			f.contendedSince = time.Now()
			logf(d, "the Titan: %s: waiting for the other caster's turn to end", firstLine(lines))
		}
	default:
		f.casts++
		f.contendedNow, f.contendedSince = false, time.Time{}
	}
	return true
}

// progress logs the fight every ten casts.
func (f *titanFight) progress() {
	logf(f.d, "the Titan: %d casts of %s, health %s, %s", f.casts, f.spell.Name, f.healthSeen(), time.Since(f.start).Round(time.Second))
}

func (f *titanFight) healthSeen() string {
	if f.total == 0 {
		return "not seen yet"
	}
	return fmt.Sprintf("%d/%d", f.health, f.total)
}

// summary logs the fight's end: "the Titan: N casts, HP seen, passed/not passed".
func (f *titanFight) summary(outcome string) {
	logf(f.d, "the Titan: %d casts (%d refused as another caster's turn), health %s, %s, %s",
		f.casts, f.contended, f.healthSeen(), time.Since(f.start).Round(time.Second), outcome)
}

// giveUp ends a fight that did not pass: out of the realm first (a park inside it leaves every other
// walk "unreachable", live 7 Oct 2026), then the park the verdict asks for.
func (f *titanFight) giveUp(v questplan.TitanVerdict) {
	d := f.d
	LeaveGrailRealm(d)
	if v.ParkFor > 0 {
		f.e.RequestPark(questplan.TitanParkReasonPrefix+v.Why+fmt.Sprintf(" (%d casts, health %s)", f.casts, f.healthSeen()), v.ParkFor)
	}
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

// titanMelee is the sword fallback: hit-and-step-back (the study's Tactic A,
// journals/wf_240af715-c0f__study_grail.json) from the approach tile, ONE STEP from the attack
// tile, instead of face-tanking him. He hits 0 to 15 every seven ticks and takes about 245
// Excalibur swings at Attack 30, which a Stage 5 account's hitpoints cannot stand. He has maxrange
// 0, so a target more than a tile from his spawn resets him, and a hit at tick T makes his first
// swing back land at T+4: the account swings, steps one tile east at once (it is there by T+2),
// and comes back in to swing again once its own five-tick delay has run out, eating only while it
// stands out of his reach. When he falls with Excalibur worn, content teleports the account past
// him (black_knight_titan.rs2 @defeat_titan).
//
// The first click is sent only from the approach tile the account walked to itself: from anywhere
// south of his row the server's own walk to him ends on 2793,4721 for good (grailtitan.go).
//
// UNPROVEN LIVE: the swing is read from the account's own animation (SelfAnim's tick), and a block
// animation from a hit that got through would read as a swing, so a Titan that lands a hit is a
// sign the timing is off and is logged.
func titanMelee(d *questrunner.Deps, titan entityquery.Entity, deadline, mark time.Time) bool {
	st := d.Session.State()
	sus := questplan.HolyGrail.Sustain
	if limit := time.Now().Add(titanMeleeBudget); limit.Before(deadline) {
		deadline = limit
	}
	past := func() bool { return !questplan.OnTitanSide(questrunner.CurrentTile(d)) }
	if !questrunner.WalkNear(d, questplan.TitanMeleeApproach, 0) {
		logf(d, "the Titan: could not walk onto the approach tile %d,%d for the sword", questplan.TitanMeleeApproach.X, questplan.TitanMeleeApproach.Z)
		return false
	}
	start := time.Now()
	_, lastAnim := st.SelfAnim()
	swings, hitsTaken := 0, 0
	lastSwing := start
	lastHP, _ := questrunner.Hitpoints(d)
	for time.Now().Before(deadline) && !past() {
		if time.Since(lastSwing) > titanNoSwingBail {
			recent := ""
			if d.Chat != nil {
				recent = firstLine(d.Chat.Since(start))
			}
			logf(d, "the Titan: no swing in %s (%d swings so far, me at %v, first game message %q): giving the fight up", titanNoSwingBail, swings, questrunner.CurrentTile(d), recent)
			return false
		}
		if questrunner.DiedSince(d, mark) {
			logf(d, "died at the Titan after %d swings (%d hits taken)", swings, hitsTaken)
			return false
		}
		if !excaliburWorn(d) {
			logf(d, "the Titan: Excalibur is no longer worn after %d swings: stopping", swings)
			return false
		}
		if hp, _ := questrunner.Hitpoints(d); hp < lastHP {
			hitsTaken++
			logf(d, "the Titan hit the account (%d -> %d hitpoints) after %d swings: the step-back was late (or poison)", lastHP, hp, swings)
		}
		if questrunner.CurrentTile(d) != questplan.TitanAttackStand {
			questrunner.EatIfNeeded(d, sus.FoodIDs, sus.EatAt)
		}
		lastHP, _ = questrunner.Hitpoints(d)
		if err := entityquery.Interact(d.Session, titan, "Attack"); err != nil {
			logf(d, "attack the Titan: %v", err)
			return false
		}
		swung := false
		for i := 0; i < 100 && !past(); i++ {
			if _, t := st.SelfAnim(); t != lastAnim {
				lastAnim, swung = t, true
				break
			}
			if !d.Delay(50 * time.Millisecond) {
				return false
			}
		}
		if past() {
			break
		}
		if !swung {
			continue
		}
		swings++
		lastSwing = time.Now()
		// Out of his reach at once, before his first swing back at T+4.
		here := questrunner.CurrentTile(d)
		_ = actions.Walk(d.Session, []pathfind.NavPoint{{X: here.X, Z: here.Z, Level: here.Level},
			{X: questplan.TitanMeleeApproach.X, Z: questplan.TitanMeleeApproach.Z, Level: here.Level}}, true)
		// Back in only when the account's own five-tick delay has run out.
		for st.Tick() < lastAnim+4 && !past() {
			if !d.Delay(50 * time.Millisecond) {
				return false
			}
		}
		if swings%25 == 0 {
			logf(d, "the Titan: %d swings, %d hits taken, %s", swings, hitsTaken, time.Since(start).Round(time.Second))
		}
	}
	logf(d, "the Titan: %s after %d swings (%d hits taken, %s)", map[bool]string{true: "passed", false: "not passed"}[past()], swings, hitsTaken, time.Since(start).Round(time.Second))
	return past()
}
