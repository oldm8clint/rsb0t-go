package questmods

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// MerlinsCrystal is Merlin's Crystal ready for the engine.
func MerlinsCrystal() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.MerlinsCrystal,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomMerlinKeep:      merlinKeep,
			questplan.CustomMerlinLeaveKeep: merlinLeaveKeep,
			questplan.CustomMerlinBat:       merlinBat,
			questplan.CustomMerlinWax:       merlinWax,
			questplan.CustomMerlinExcalibur: merlinExcalibur,
			questplan.CustomMerlinAltar:     merlinAltar,
			questplan.CustomMerlinStrip:     merlinStrip,
			questplan.CustomMerlinSummon:    merlinSummon,
			questplan.CustomMerlinCrystal:   merlinCrystal,
		},
	}
}

func level(d *questrunner.Deps) int { return questrunner.CurrentTile(d).Level }

// climb operates the nearest loc named name offering op within radius of near and waits for the
// floor to change.
func climb(d *questrunner.Deps, name, op string, near tile.Tile) bool {
	m, ok := questrunner.FindLoc(d, 0, name, op, near, 6)
	if !ok {
		logf(d, "no %s offering %q near %d,%d,%d", name, op, near.X, near.Z, near.Level)
		return false
	}
	before := level(d)
	if err := questrunner.OperateLoc(d, m, op); err != nil {
		logf(d, "%s %s: %v", op, name, err)
		return false
	}
	return waitUntil(d, 8*time.Second, func() bool { return level(d) != before })
}

// driveYes answers every "Yes."/"No." prompt with Yes until cond holds or the budget runs out,
// continuing the message boxes in between (the crate rides are twenty mesboxes long).
func driveYes(d *questrunner.Deps, cond func() bool, budget time.Duration) bool {
	dl := questrunner.NewDialogue(d)
	return dl.DriveUntil(cond, []string{"Yes."}, budget)
}

// merlinKeep rides Arhein's crate into Keep Le Faye, climbs to Sir Mordred, fights him to 0 and
// answers Morgan Le Faye (stage 3 to 4).
func merlinKeep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questplan.InsideKeep(questrunner.CurrentTile(d)) {
		if !questrunner.Travel(d, tile.Tile{X: 2801, Z: 3443}, 1) {
			return false
		}
		m, ok := questrunner.FindLoc(d, questplan.LocCatherbyCrate, "", "Hide-in", questplan.CatherbyCrate, 3)
		if !ok {
			logf(d, "no Catherby crate")
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Hide-in"); err != nil {
			return false
		}
		inKeepL0 := func() bool {
			t := questrunner.CurrentTile(d)
			return questplan.InsideKeep(t) && !questplan.IsUnderground(t)
		}
		if !driveYes(d, inKeepL0, 90*time.Second) {
			logf(d, "the crate ride did not land in the keep")
			return false
		}
		questrunner.ClearLeftoverModal(d)
	}
	if level(d) == 0 {
		if !questrunner.Travel(d, tile.Tile{X: 2769, Z: 3403}, 1) {
			return false
		}
		if !climb(d, "Staircase", "Climb-up", tile.Tile{X: 2769, Z: 3404}) {
			return false
		}
	}
	if level(d) == 1 {
		if !questrunner.WalkNear(d, tile.Tile{X: 2769, Z: 3397, Level: 1}, 1) {
			return false
		}
		if !climb(d, "Staircase", "Climb-up", tile.Tile{X: 2769, Z: 3398, Level: 1}) {
			return false
		}
	}
	healToFull(d, questplan.MerlinFoodIDs)
	dl := questrunner.NewDialogue(d)
	if !dl.Ready() {
		if why := merlinMordred(d, dl); why != "" {
			// Counted as a lost fight (the engine parks the quest for it) and walked out of: the keep is not a
			// place the engine's own retreat can leave, and the next run's gate (MerlinMordredReady) judges the
			// pack again before the crate ride.
			logf(d, "fight: losing to Sir Mordred (%s): leaving the keep", why)
			questrunner.NoteLostFight(d, "Sir Mordred", why)
			merlinLeaveKeep(e, questplan.Step{})
			return false
		}
		waitUntil(d, 6*time.Second, dl.Ready)
	}
	if !dl.Ready() {
		logf(d, "Mordred fell without Morgan appearing (the killing tick was busy?): the next pass fights again")
		return false
	}
	dl.Drive(questplan.MorganPrefer)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return true
}

// merlinMordred fights Sir Mordred until Morgan Le Faye's plea opens, and not one cast longer.
// Content never lets him die at stage 3: at his death it heals him to full, spawns Morgan beside
// him and opens her dialogue (sir_mordred.rs2, @lefaye_spare_son). So he never leaves the scene,
// and a fight that waits for that (FightByMagic, FightNpc) kills him again and again, each kill
// cutting her plea off, until the account has eaten everything and dies: the first Merlin EMPTY
// lab run (merlin-empty-1) cast thirty Wind Bolts, ate three cakes and died with Mordred alive.
// It casts the best spell the pack pays for (his magic defence is 1), chosen again before every cast so a
// lower spell takes over when the higher one's runes are spent, and eats only below the guard's line: a
// bite at the killing tick delays the player, and then Morgan does not come.
//
// It returns "" when the fight ended as it was meant to (the plea opened, the bot is stopping, the budget ran
// out) and a reason when it gave the fight up: the race is lost (questplan.RaceLost: the pace so far says
// the rest costs more than the account holds), or the runes are spent and the account's own swing cannot
// finish him (questplan.MeleeOdds). Live 16 Oct 2026 01:15-01:18 an account out of Fire Bolts after nine
// seconds meleed him for three minutes, ate twenty bites of 4 and 5 hitpoints at 8 to 12 of 53 and died
// with none left, without once taking a tenth of his 38 hitpoints off: the caller walks out of the keep
// and counts a lost fight.
func merlinMordred(d *questrunner.Deps, dl *questrunner.Dialogue) string {
	g := questrunner.NewFightGuard(d, questplan.NpcMordred, questplan.MerlinsCrystal.Sustain)
	pleaded := func() bool {
		if dl.Ready() {
			return true
		}
		_, ok := questrunner.FindNpcByID(d, questplan.NpcMorganLeFaye, "", 8)
		return ok
	}
	mordred, ok := questrunner.FindNpcByID(d, questplan.NpcMordred, "", 12)
	if !ok {
		logf(d, "no Sir Mordred in view")
		return ""
	}
	g.Prepare()
	banned := map[string]bool{}
	pick := func() (actions.Spell, bool) {
		return questrunner.BestCastableSpellWhere(d, 1, func(s actions.Spell) bool { return !banned[s.Name] })
	}
	if spell, casting := pick(); casting {
		logf(d, "casting %s at Sir Mordred until Morgan Le Faye pleads", spell.Name)
	} else {
		logf(d, "no castable spell: meleeing Sir Mordred until Morgan Le Faye pleads")
	}
	mark := time.Now()
	deadline := mark.Add(4 * time.Minute)
	castAt, clicked := mark, time.Time{}
	said := false
	for time.Now().Before(deadline) {
		if pleaded() {
			return ""
		}
		if questrunner.DiedSince(d, mark) {
			return ""
		}
		if m, seen := questrunner.FindNpcByID(d, questplan.NpcMordred, "", 12); seen {
			mordred = m
		}
		// A lost race is given up BEFORE the bite, so the last piece of food is kept for the way out.
		if v := g.Poll(mordred.Health, mordred.TotalHealth); v.Action == questplan.FightRetreat {
			return v.Why
		}
		if g.Eat() {
			clicked = time.Time{} // eating drops the attack: the swing is sent again
		}
		if pleaded() {
			return ""
		}
		spell, casting := pick()
		if casting && d.Chat != nil && actions.IsNoRunesLine(d.Chat.Since(castAt)) {
			logf(d, "out of runes for %s", spell.Name)
			banned[spell.Name] = true
			castAt = time.Now() // the next spell is judged on its own casts, not on this line
			continue
		}
		if casting {
			castAt = time.Now()
			if err := actions.CastOnNpc(d.Session, spell.Name, mordred.Index); err != nil {
				logf(d, "cast %s: %v", spell.Name, err)
				return ""
			}
			clicked = time.Time{}
			if !g.WaitCast() {
				return ""
			}
			continue
		}
		// Nothing castable: the swing, if the swing can finish him.
		if !said {
			said = true
			if ok, why := g.MeleeWinnable(mordred.Health); !ok {
				return "out of runes and " + why
			}
			logf(d, "out of runes: meleeing Sir Mordred")
		}
		if time.Since(clicked) >= 6*time.Second {
			if err := entityquery.Interact(d.Session, mordred, "Attack"); err != nil {
				logf(d, "attack Sir Mordred: %v", err)
				return ""
			}
			clicked = time.Now()
		}
		if !d.Delay(600 * time.Millisecond) {
			return ""
		}
	}
	logf(d, "Morgan Le Faye never pleaded in %s", time.Since(mark).Round(time.Second))
	return ""
}

// merlinLeaveBudget is how long the walk to the pier crate gets: the engine's six minutes of retried "locked" door was
// the death.
const merlinLeaveBudget = 90 * time.Second

// merlinParkFor is how long a keep the account cannot leave unhurt keeps Merlin's Crystal out of the ladder.
const merlinParkFor = 6 * time.Hour

// merlinLeaveKeep walks down to the ground floor and rides the pier crate home, falling back on
// the front door (which opens from inside only).
func merlinLeaveKeep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	// Eat to full before the stairs: live 6-7 Oct 2026 (Off Da Chain, 23:42 and 00:00) the account came down from Mordred
	// at 21/27 into the hall's Renegade Knights, was at 9-12 hitpoints for eight minutes while its walk to the pier
	// crate retried a front door that "is securely locked", and died at 2763,3402 with five sharks and eight slices
	// eaten along the way.
	healToFull(d, questplan.MerlinFoodIDs)
	for i := 0; i < 3 && level(d) > 0; i++ {
		stand := tile.Tile{X: 2769, Z: 3399, Level: level(d)}
		if !climb(d, "Staircase", "Climb-down", stand) {
			questrunner.WalkNear(d, stand, 2)
			if !climb(d, "Staircase", "Climb-down", stand) {
				return false
			}
		}
	}
	out := func() bool { return !questplan.InsideKeep(questrunner.CurrentTile(d)) }
	// The pier crate first, reached by Travel: the hall's Large door to the pier is shut from this
	// side and the plain approach has no door edge ("no path ... unreachable" in merlin-empty-2),
	// and the front door's fallback drops the account among the Giant bats, where the second
	// Merlin EMPTY lab run died at 14 hitpoints.
	if !questrunner.TravelFor(d, tile.Tile{X: 2778, Z: 3401}, 1, merlinLeaveBudget) {
		hp, maxHP := questrunner.Hitpoints(d)
		logf(d, "the walk to the pier crate did not arrive inside %s (hitpoints %d/%d, at %v): the keep's hall is where Mordred's knights hit, so the quest is parked if the account is hurt", merlinLeaveBudget, hp, maxHP, questrunner.CurrentTile(d))
		if maxHP > 0 && hp*2 < maxHP {
			e.RequestPark(fmt.Sprintf("could not leave Keep Le Faye under attack: hitpoints %d/%d", hp, maxHP), merlinParkFor)
			return false
		}
	}
	if m, ok := questrunner.FindLoc(d, questplan.LocKeepCrate, "", "Hide-in", questplan.KeepCrate, 6); ok {
		if err := questrunner.OperateLoc(d, m, "Hide-in"); err == nil && driveYes(d, out, 90*time.Second) {
			questrunner.ClearLeftoverModal(d)
			return true
		}
	}
	if m, ok := questrunner.FindLoc(d, 0, "Large door", "Open", tile.Tile{X: 2763, Z: 3401}, 8); ok {
		_ = questrunner.OperateLoc(d, m, "Open")
		waitUntil(d, 5*time.Second, out)
	}
	return out()
}

// merlinBat kills a giant bat west of the keep's front door and takes its bones.
func merlinBat(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.KeepBats, 6) {
		return false
	}
	bat, ok := questrunner.FindNpcByID(d, questplan.NpcGiantBat, "", 14)
	if !ok {
		logf(d, "no giant bat in view")
		d.Delay(5 * time.Second)
		return false
	}
	// Cast when the pack can (the fight kit's leftover runes, drawn back by the stage-4 kit): a
	// bat (32 hitpoints, magic defence 1) meleed by a 20-hitpoint account at Stage 5 levels
	// takes about 24 hitpoints off it (the study's 15 in the open at better levels). The staff of
	// air the kit drew back pays the air runes only when worn.
	if questrunner.HeldID(d, 1381) > 0 {
		questrunner.Equip(d, 1381)
	}
	fightBest(d, questplan.Step{Kind: questplan.StepFight, TargetID: questplan.NpcGiantBat,
		Target: "Giant bat", Anchor: questplan.KeepBats}, questplan.MerlinsCrystal.Sustain)
	// The bones land on the bat's own tile, which a cast kill leaves some tiles away.
	near := []tile.Tile{questrunner.CurrentTile(d), {X: bat.X, Z: bat.Z, Level: bat.Level}}
	for i := 0; i < 10 && questrunner.HeldID(d, questplan.ObjBatBones) == 0; i++ {
		if questrunner.TakeGround(d, questplan.ObjBatBones, near[i%2], 12) {
			break
		}
		d.Delay(500 * time.Millisecond)
	}
	return questrunner.HeldID(d, questplan.ObjBatBones) > 0
}

// merlinWax uses the insect repellent on a beehive (which calms it; the repellent is kept), then
// the bucket on it for a bucket of wax.
func merlinWax(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.BeehiveTile, 2) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocBeehive, "", "", questplan.BeehiveTile, 8)
	if !ok {
		logf(d, "no beehive in view")
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjInsectRepel, m); err != nil {
		logf(d, "repellent on the hive: %v", err)
		return false
	}
	// bees.rs2 answers both uses with a mesbox, and the wax comes only once the second is
	// continued: merlin-empty-4 waited on it, never pressed continue and parked with no wax.
	dl := questrunner.NewDialogue(d)
	dl.DriveUntil(func() bool { return false }, nil, 3*time.Second)
	questrunner.ClearLeftoverModal(d)
	if err := questrunner.UseItemOnLoc(d, questplan.ObjBucket, m); err != nil {
		logf(d, "bucket on the hive: %v", err)
		return false
	}
	if dl.DriveUntil(func() bool { return questrunner.HeldID(d, questplan.ObjBucketOfWax) > 0 }, nil, 10*time.Second) {
		questrunner.ClearLeftoverModal(d)
		return true
	}
	// The questproof EMPTY run merlin-empty-4 parked here with no game message at all.
	at := questrunner.CurrentTile(d)
	logf(d, "no bucket of wax after the repellent and the bucket, from %d,%d,%d (hive at %d,%d; repellent %d, bucket %d)",
		at.X, at.Z, at.Level, m.X, m.Z, questrunner.HeldID(d, questplan.ObjInsectRepel), questrunner.HeldID(d, questplan.ObjBucket))
	return false
}

// merlinExcalibur takes the Lady of the Lake's test and opens the Port Sarim jeweller's door,
// which spawns the beggar; the bread answers him and the Lady hands Excalibur over.
func merlinExcalibur(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	lady := questplan.NpcStop{Npc: "The Lady of the Lake", ID: questplan.NpcLadyOfTheLake, Anchor: questplan.LadyAnchor,
		Leash: 6, Prefer: []string{"I seek the sword Excalibur."}}
	if !e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: lady}) {
		return false
	}
	if !questrunner.Travel(d, questplan.JewellerDoorStand, 1) {
		return false
	}
	beggarPrefer := []string{"Yes certainly.", "Yes, here you go."}
	got := func() bool { return questrunner.HeldID(d, questplan.ObjExcalibur) > 0 }
	if b, ok := questrunner.FindNpcByID(d, questplan.NpcBeggar, "", 6); ok {
		stop := questplan.NpcStop{Npc: "Beggar", ID: questplan.NpcBeggar, Anchor: tile.Tile{X: b.X, Z: b.Z}, Leash: 6, Prefer: beggarPrefer}
		if questrunner.OpenDialogue(d, stop) {
			questrunner.NewDialogue(d).DriveUntil(got, beggarPrefer, 40*time.Second)
		}
	} else if m, ok := questrunner.FindLoc(d, questplan.LocJewellerDoor, "", "Open", questplan.JewellerDoorStand, 3); ok {
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			return false
		}
		dl := questrunner.NewDialogue(d)
		waitUntil(d, 8*time.Second, dl.Ready)
		dl.DriveUntil(got, beggarPrefer, 40*time.Second)
	}
	questrunner.ClearLeftoverModal(d)
	return got()
}

// merlinAltar checks the Wilderness chaos altar for Thrantax's words (a varp bit only the
// journal shows).
func merlinAltar(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.ChaosAltarStand, 2) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocChaosAltarWord, "", "Check", questplan.ChaosAltarStand, 4)
	if !ok {
		logf(d, "no chaos altar with a Check op")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Check"); err != nil {
		return false
	}
	d.Delay(1800 * time.Millisecond)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return true
}

// merlinSummon drops the bat bones on the magic symbol with the lit candle held and gives
// Thrantax the words (Drive stops rather than guess, and a wrong answer is never on the list).
func merlinSummon(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.MagicSymbol, 1) {
		return false
	}
	if err := questrunner.OpHeldByName(d, questplan.ObjBatBones, "Drop"); err != nil {
		logf(d, "drop the bat bones: %v", err)
		return false
	}
	dl := questrunner.NewDialogue(d)
	if !waitUntil(d, 10*time.Second, dl.Ready) {
		logf(d, "Thrantax did not answer the bones")
		questrunner.TakeGround(d, questplan.ObjBatBones, questrunner.CurrentTile(d), 3)
		return false
	}
	dl.Drive([]string{questplan.ThrantaxWords})
	questrunner.ClearLeftoverModal(d)
	// The bones stay on the floor on success: pick them up again in case a retry needs them.
	questrunner.TakeGround(d, questplan.ObjBatBones, questrunner.CurrentTile(d), 3)
	e.InvalidateStage()
	return true
}

// merlinCrystal climbs Merlin's tower and uses Excalibur (from the pack) on the Giant crystal,
// then climbs back down. A missing crystal is not success (another account's break deletes it
// for 50 ticks): the journal decides.
func merlinCrystal(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if level(d) < 2 {
		questrunner.Travel(d, questplan.MerlinCrystalTile, 2)
	}
	if level(d) == 0 {
		questrunner.Travel(d, tile.Tile{X: 2769, Z: 3494}, 1)
		climb(d, "Ladder", "Climb-up", tile.Tile{X: 2769, Z: 3493})
	}
	if level(d) == 1 {
		climb(d, "Ladder", "Climb-up", tile.Tile{X: 2767, Z: 3491, Level: 1})
	}
	if level(d) != 2 {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocGiantCrystal, "", "", questplan.MerlinCrystalTile, 6)
	if !ok {
		logf(d, "no Giant crystal (broken by someone else for 50 ticks?): waiting")
		d.Delay(10 * time.Second)
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjExcalibur, m); err != nil {
		logf(d, "Excalibur on the crystal: %v", err)
		return false
	}
	d.Delay(2 * time.Second)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	for i := 0; i < 3 && level(d) > 0; i++ {
		climb(d, "Ladder", "Climb-down", questrunner.CurrentTile(d))
	}
	return true
}

// fightBest casts at the target from where the account stands when a combat spell is castable
// from the pack (bosses without a magic stat defend at 1), and melees otherwise.
func fightBest(d *questrunner.Deps, s questplan.Step, sus questplan.Sustain) bool {
	if _, ok := questrunner.BestCastableSpell(d, 5); ok {
		s.Spell = "best"
		return questrunner.FightByMagic(d, s, sus)
	}
	return questrunner.FightNpc(d, s, sus)
}

// healToFull eats until hitpoints are full or the food runs out.
func healToFull(d *questrunner.Deps, foods []int) {
	for i := 0; i < 28; i++ {
		hp, max := questrunner.Hitpoints(d)
		if max == 0 || hp >= max {
			return
		}
		if !questrunner.EatIfNeeded(d, foods, max-1) {
			return
		}
	}
}

// merlinStrip takes everything off and banks all but coins and food at the nearest bank, so the
// Wilderness altar trip risks nothing the quest needs. The run staller's kit goes to the bank too (a bare deposit): the
// altar trip is questplan.WildernessSafe, a pack of coins and food, and the kit the ordinary deposit kept (or drew back
// out of the bank behind itself) failed it for ever (live 14-15 Oct 2026: ten of ten Merlin runs parked in merlin-strip).
func merlinStrip(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	bank := questplan.NearestBank(questrunner.CurrentTile(d))
	if !questrunner.Travel(d, bank.Tile, 4) {
		return false
	}
	questrunner.PackVisible(d)
	questrunner.StripWorn(d)
	b, ok := questrunner.OpenBankAt(d, bank.Tile)
	if !ok {
		return false
	}
	food := map[int]bool{}
	for _, f := range questplan.MerlinFoodIDs {
		food[f] = true
	}
	clean := questrunner.DepositBare(d, b, func(id int) bool { return id == questplan.CoinsID || food[id] })
	_ = b.Close()
	return clean && len(questrunner.WornCounts(d)) == 0
}
