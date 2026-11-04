package questmods

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// HorrorFromTheDeep is Horror from the Deep ready for the engine. The pure decisions are in
// questplan/horror.go; these are its executors. NOT lab-proven: every step here is built from
// content 289's quest_horror scripts and the lane Q study, not from a run.
func HorrorFromTheDeep() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.Horror,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomHorrorBridge: horrorBridge,
			questplan.CustomHorrorDoor:   horrorDoor,
			questplan.CustomHorrorLight:  horrorLight,
			questplan.CustomHorrorDown:   horrorDown,
			questplan.CustomHorrorJunior: horrorJunior,
			questplan.CustomHorrorMother: horrorMother,
			questplan.CustomHorrorLeave:  horrorLeave,
		},
	}
}

func here(d *questrunner.Deps) tile.Tile { return questrunner.CurrentTile(d) }

// opLocAt operates the loc id near at with op, then waits for moved to hold.
func opLocAt(d *questrunner.Deps, id int, at tile.Tile, op string, moved func(tile.Tile) bool) bool {
	m, ok := questrunner.FindLoc(d, id, "", "", at, 6)
	if !ok {
		logf(d, "no loc %d near %d,%d,%d", id, at.X, at.Z, at.Level)
		return false
	}
	if err := questrunner.OperateLoc(d, m, op); err != nil {
		logf(d, "%s loc %d: %v", op, id, err)
		return false
	}
	return waitUntil(d, 8*time.Second, func() bool { return moved(here(d)) })
}

// useOnLocAt uses item on the loc id near at and reports whether the item count dropped. A
// refusal raises a mesbox, which is cleared.
func useOnLocAt(d *questrunner.Deps, item, id int, at tile.Tile) bool {
	m, ok := questrunner.FindLoc(d, id, "", "", at, 6)
	if !ok {
		logf(d, "no loc %d near %d,%d,%d", id, at.X, at.Z, at.Level)
		return false
	}
	before := questrunner.HeldID(d, item)
	if err := questrunner.UseItemOnLoc(d, item, m); err != nil {
		logf(d, "item %d on loc %d: %v", item, id, err)
		return false
	}
	used := waitUntil(d, 5*time.Second, func() bool { return questrunner.HeldID(d, item) < before })
	d.Delay(600 * time.Millisecond)
	questrunner.ClearLeftoverModal(d)
	return used
}

// horrorBridge puts a plank on each half from whichever bank the walk reached, crossing between
// them, and ends on the lighthouse side. A half already fixed refuses the plank ("already
// fixed") and keeps it; the journal says when both are done.
func horrorBridge(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !questrunner.Travel(d, questplan.BridgeWest, 2) && !questrunner.Travel(d, questplan.BridgeEast, 2) {
		return false
	}
	west := func(t tile.Tile) bool { return t.X <= questplan.BridgeLeftTile.X }
	east := func(t tile.Tile) bool { return t.X >= questplan.BridgeRightTile.X }
	first, second := questplan.LocBridgeLeft, questplan.LocBridgeRight
	firstAt, secondAt := questplan.BridgeLeftTile, questplan.BridgeRightTile
	across := east
	if east(here(d)) {
		first, second, firstAt, secondAt, across = second, first, secondAt, firstAt, west
	}
	useOnLocAt(d, 960, first, firstAt)
	if !opLocAt(d, first, firstAt, "Cross", across) {
		return false
	}
	useOnLocAt(d, 960, second, secondAt)
	if !west(here(d)) {
		return opLocAt(d, second, secondAt, "Cross", west)
	}
	return true
}

// horrorDoor walks through the lighthouse doorway: the first use with the key unlocks it, the
// next (bridge and key both done) enters the quest copy.
func horrorDoor(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !questrunner.Travel(d, questplan.HorrorDoorStand, 1) {
		return false
	}
	for i := 0; i < 3; i++ {
		if opLocAt(d, questplan.LocHorrorDoorway, questplan.HorrorDoorway, "Walk-through", questplan.InHorrorQuestCopy) {
			return true
		}
		questrunner.ClearLeftoverModal(d)
	}
	return questplan.InHorrorQuestCopy(here(d))
}

// intoQuestCopy gets the account into the lighthouse's quest copy from wherever it is.
func intoQuestCopy(e *questrunner.Engine) bool {
	d := e.Deps()
	t := here(d)
	switch {
	case questplan.InHorrorQuestCopy(t):
		return true
	case questplan.InRealLighthouseUpstairs(t):
		// At stages 4-9 the real top stairs and the middle stairs' Climb-down both route back
		// into the quest copy (quest_horror.rs2:190-212).
		if t.Level == 2 {
			return opLocAt(d, questplan.LocHorrorStairTop, t, "Climb-down", questplan.InHorrorQuestCopy)
		}
		return opLocAt(d, questplan.LocHorrorStairMid, t, "Climb-down", questplan.InHorrorQuestCopy)
	}
	return horrorDoor(e, questplan.Step{})
}

// horrorLight climbs the quest copy to the mechanism and uses tar, then the tinderbox, then the
// glass on it. The third completes the repair and teleports the account to the real top floor.
func horrorLight(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !intoQuestCopy(e) {
		return false
	}
	for i := 0; i < 3 && questplan.InHorrorQuestCopy(here(d)) && here(d).Level < 2; i++ {
		t := here(d)
		if t.Level == 0 {
			opLocAt(d, questplan.LocHorrorStairBase, questplan.CopyStairBase, "Climb-up", func(t tile.Tile) bool { return t.Level == 1 })
		} else {
			opLocAt(d, questplan.LocHorrorStairMid, questplan.CopyStairMid, "Climb-up", func(t tile.Tile) bool { return t.Level == 2 })
		}
	}
	if here(d).Level != 2 {
		logf(d, "did not reach the lighthouse top")
		return false
	}
	for _, item := range []int{questplan.ObjSwampTar, questplan.ObjTinderbox, questplan.ObjMoltenGlass} {
		if !questplan.InHorrorQuestCopy(here(d)) {
			break // the repair teleported the account out
		}
		if questrunner.HeldID(d, item) == 0 {
			continue
		}
		m, ok := questrunner.FindLoc(d, questplan.LocLightMechanism, "", "", questplan.LightMechanism, 6)
		if !ok {
			logf(d, "no lighting mechanism loaded")
			return false
		}
		if err := questrunner.UseItemOnLoc(d, item, m); err != nil {
			logf(d, "item %d on the mechanism: %v", item, err)
			return false
		}
		d.Delay(1800 * time.Millisecond)
	}
	return waitUntil(d, 5*time.Second, func() bool { return !questplan.InHorrorQuestCopy(here(d)) })
}

// wallItems are the six slot items, in the order they are tried.
var wallItems = []int{questplan.ObjFireRune, questplan.ObjWaterRune, questplan.ObjEarthRune, questplan.ObjAirRune,
	questplan.ObjBronzeDagger, questplan.ObjBronzeArrow}

// fillWall uses each held slot item on the mid-left wall from the south, answering "Yes". A
// slot already filled answers "no space" and keeps the item.
func fillWall(d *questrunner.Deps) {
	m, ok := questrunner.FindLoc(d, questplan.LocWallMidLeft, "", "", questplan.WallMidLeft, 4)
	if !ok {
		logf(d, "no strange wall loaded")
		return
	}
	for _, item := range wallItems {
		if questrunner.HeldID(d, item) == 0 {
			continue
		}
		if err := questrunner.UseItemOnLoc(d, item, m); err != nil {
			logf(d, "item %d on the wall: %v", item, err)
			continue
		}
		dl := questrunner.NewDialogue(d)
		if waitUntil(d, 3*time.Second, dl.Ready) {
			dl.DriveSequence([]string{"Yes"}, 8*time.Second)
		}
		questrunner.ClearLeftoverModal(d)
	}
}

// horrorDown takes the account from wherever it is to the cavern: the quest copy, down to its
// ladder, the basement, the strange wall and the ladder down.
func horrorDown(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	for i := 0; i < 10; i++ {
		t := here(d)
		switch {
		case questplan.InHorrorCavern(t):
			return true
		case questplan.InHorrorBasementNorth(t):
			opLocAt(d, questplan.LocCavernLadder, questplan.CavernLadderTop, "Climb", questplan.InHorrorCavern)
		case questplan.InHorrorBasement(t):
			if opLocAt(d, questplan.LocWallFarRight, questplan.WallFarRight, "Open", questplan.InHorrorBasementNorth) {
				continue
			}
			questrunner.ClearLeftoverModal(d)
			fillWall(d)
			if !opLocAt(d, questplan.LocWallFarRight, questplan.WallFarRight, "Open", questplan.InHorrorBasementNorth) {
				logf(d, "the strange wall stays shut: a slot item is missing")
				return false
			}
		case questplan.InHorrorQuestCopy(t):
			switch t.Level {
			case 2:
				opLocAt(d, questplan.LocHorrorStairTop, t, "Climb-down", func(t tile.Tile) bool { return t.Level < 2 })
			case 1:
				opLocAt(d, questplan.LocHorrorStairMid, questplan.CopyStairMid, "Climb-down", func(t tile.Tile) bool { return t.Level == 0 })
			default:
				opLocAt(d, questplan.LocHorrorLadder, questplan.CopyLadder, "Climb", questplan.InHorrorBasement)
			}
		default:
			if !intoQuestCopy(e) {
				return false
			}
		}
	}
	return questplan.InHorrorCavern(here(d))
}

var jossikStop = questplan.NpcStop{Npc: "Jossik", ID: questplan.NpcJossikInjured, Anchor: questplan.JossikAnchor, Leash: 8,
	Prefer: []string{"Jossik"}}

// summon talks to Jossik and waits for one of ids to appear, returning it.
func summon(d *questrunner.Deps, ids []int) (entityquery.Entity, bool) {
	find := func() (entityquery.Entity, bool) {
		for _, n := range entityquery.Npcs(d.Session.State(), d.Config).Within(20).Results() {
			for _, id := range ids {
				if n.ID == id {
					return n, true
				}
			}
		}
		return entityquery.Entity{}, false
	}
	if n, ok := find(); ok {
		return n, true
	}
	questrunner.ExecuteStep(d, questplan.Step{Kind: questplan.StepTalk, Stop: jossikStop}, nil)
	var got entityquery.Entity
	ok := waitUntil(d, 15*time.Second, func() bool {
		n, found := find()
		got = n
		return found
	})
	return got, ok
}

func setPrayer(d *questrunner.Deps, p actions.Prayer, on bool) {
	if lvl, ok := actions.PrayerLevel(d.Session); ok && lvl < p.Level {
		return
	}
	if actions.PrayerActive(d.Session, p) != on {
		_ = actions.SetPrayer(d.Session, p, on)
	}
}

// spellFor is the best castable spell of an element ("Wind", "Water" ...), or of any element
// when element is "".
func spellFor(d *questrunner.Deps, element string) (string, bool) {
	s, ok := questrunner.BestCastableSpellWhere(d, 3, func(sp actions.Spell) bool {
		return element == "" || strings.HasPrefix(strings.ToLower(sp.Name), strings.ToLower(element)+" ")
	})
	return s.Name, ok
}

// horrorJunior summons the junior and casts at it (Protect from Melee held when Prayer 43
// allows; its melee max is 9) until it dies.
func horrorJunior(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	jr, ok := summon(d, []int{questplan.NpcHorrorJunior, 1344, 1345, 1346})
	if !ok {
		logf(d, "Jossik summoned no junior dagannoth")
		return false
	}
	waitUntil(d, 6*time.Second, func() bool {
		n, found := questrunner.FindNpcByID(d, questplan.NpcHorrorJunior, "", 20)
		jr = n
		return found
	})
	setPrayer(d, actions.ProtectFromMelee, true)
	defer setPrayer(d, actions.ProtectFromMelee, false)
	spell, ok := spellFor(d, "")
	if !ok {
		logf(d, "no castable spell for the junior")
		return false
	}
	return castUntilGone(d, e.Current().Module.Sustain, jr.Index, func(entityquery.Entity) string { return spell }, nil)
}

// horrorMother summons the mother and fights her colour by colour: Protect from Missiles is held
// only while casting (a cast forces her ranged, and her volley ignores accuracy), the four
// elemental forms take that element's best bolt or strike, and the green and orange forms,
// which a caster cannot hurt, are met with a melee attack so she stays on melee with the
// prayer off. The fight is left up the cavern ladder when prayer or food runs out.
func horrorMother(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	mother, ok := summon(d, questplan.MotherForms)
	if !ok {
		logf(d, "Jossik summoned no dagannoth mother")
		return false
	}
	defer setPrayer(d, actions.ProtectFromMissiles, false)
	sus := e.Current().Module.Sustain
	pick := func(n entityquery.Entity) string {
		el := questplan.MotherElement(n.ID)
		if el == "" {
			return ""
		}
		s, ok := spellFor(d, el)
		if !ok {
			return ""
		}
		return s
	}
	bail := func() bool {
		pts, _ := actions.PrayerPoints(d.Session)
		hp, max := questrunner.Hitpoints(d)
		food := sus.FoodHeld(questrunner.PackCounts(d))
		return (pts < 3 && max > 0 && hp*2 < max) || (food == 0 && max > 0 && hp*2 < max)
	}
	won := castUntilGone(d, sus, mother.Index, pick, bail)
	if !won && questplan.InHorrorCavern(here(d)) && bail() {
		logf(d, "out of prayer or food: up the cavern ladder (she is deleted; talk re-summons her at full health)")
		opLocAt(d, questplan.LocCavernLadderUp, questplan.CavernLadderUp, "Climb", questplan.InHorrorBasement)
	}
	return won || questplan.InHorrorPostPocket(here(d))
}

// castUntilGone fights the npc with that index until it leaves the scene. spell picks the spell
// for its current form ("" means do not cast: melee it with the prayer off). bail ends the
// fight early.
func castUntilGone(d *questrunner.Deps, sus questplan.Sustain, index int, spell func(entityquery.Entity) string, bail func() bool) bool {
	mark := time.Now()
	deadline := mark.Add(20 * time.Minute)
	lastMelee := time.Time{}
	for time.Now().Before(deadline) {
		if questrunner.DiedSince(d, mark) {
			return false
		}
		if questplan.InHorrorPostPocket(here(d)) {
			return true
		}
		var npc entityquery.Entity
		alive := false
		for _, n := range entityquery.Npcs(d.Session.State(), d.Config).Within(20).Results() {
			if n.Index == index {
				npc, alive = n, true
			}
		}
		if !alive {
			return true
		}
		questrunner.EatIfNeeded(d, sus.FoodIDs, sus.EatAt)
		if bail != nil && bail() {
			return false
		}
		name := spell(npc)
		if name == "" {
			setPrayer(d, actions.ProtectFromMissiles, false)
			if time.Since(lastMelee) > 6*time.Second {
				_ = entityquery.Interact(d.Session, npc, "Attack")
				lastMelee = time.Now()
			}
			d.Delay(600 * time.Millisecond)
			continue
		}
		setPrayer(d, actions.ProtectFromMissiles, true)
		if d.Chat != nil && actions.IsNoRunesLine(d.Chat.Since(mark)) {
			logf(d, "out of runes for %s", name)
			return false
		}
		if err := actions.CastOnNpc(d.Session, name, npc.Index); err != nil {
			logf(d, "cast %s: %v", name, err)
			return false
		}
		if !questrunner.WaitCastEating(d, sus) {
			return false
		}
	}
	return false
}

// horrorLeave climbs out of the post-quest pocket, or out of the lighthouse pockets to the
// mainland when the light kit is short.
func horrorLeave(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	t := here(d)
	switch {
	case questplan.InHorrorPostPocket(t):
		return opLocAt(d, questplan.LocHorrorExitUp, questplan.PostQuestLadder, "Climb", func(t tile.Tile) bool { return !questplan.InHorrorPostPocket(t) })
	case questplan.InHorrorQuestCopy(t) && t.Level == 0:
		// The quest copy's doorway from inside teleports to the real lighthouse's front.
		return opLocAt(d, questplan.LocHorrorDoorway, tile.Tile{X: 2445, Z: 4596}, "Walk-through", func(t tile.Tile) bool { return !questplan.InHorrorQuestCopy(t) })
	case questplan.InHorrorQuestCopy(t):
		if t.Level == 2 {
			opLocAt(d, questplan.LocHorrorStairTop, t, "Climb-down", func(t tile.Tile) bool { return t.Level < 2 })
		} else {
			opLocAt(d, questplan.LocHorrorStairMid, questplan.CopyStairMid, "Climb-down", func(t tile.Tile) bool { return t.Level == 0 })
		}
		return false
	}
	return false
}

// makeGlass is the provisioner's skill source for molten glass: seaweed burnt to soda ash on the
// Seers range, then soda ash with a bucket of sand in a furnace (no menu for either when one of
// each is held). The seaweed and the sand are the kit's own optional lines (bank or mule).
func makeGlass(d *questrunner.Deps, task questplan.SourceTask) bool {
	if questrunner.HeldID(d, questplan.ObjSodaAsh) == 0 {
		if questrunner.HeldID(d, questplan.ObjSeaweed) == 0 {
			logf(d, "no seaweed or soda ash for glass")
			return false
		}
		if !questrunner.Travel(d, questplan.SeersRange, 3) {
			return false
		}
		m, ok := questrunner.FindLoc(d, 0, "Range", "", questplan.SeersRange, 6)
		if !ok {
			logf(d, "no range at Seers")
			return false
		}
		if err := questrunner.UseItemOnLoc(d, questplan.ObjSeaweed, m); err != nil {
			logf(d, "seaweed on the range: %v", err)
			return false
		}
		if waitUntil(d, 6*time.Second, func() bool {
			return questrunner.HeldID(d, questplan.ObjSodaAsh) > 0 || questrunner.IsMakeMenu(d)
		}) && questrunner.IsMakeMenu(d) {
			_ = questrunner.MakeOne(d, "Soda ash")
		}
		if !waitUntil(d, 10*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjSodaAsh) > 0 }) {
			logf(d, "the range made no soda ash")
			return false
		}
	}
	if questrunner.HeldID(d, questplan.ObjBucketOfSand) == 0 {
		logf(d, "no bucket of sand for glass")
		return false
	}
	at := task.Source.Tile
	if at == (tile.Tile{}) {
		at = questplan.ArdougneFurnace
	}
	if !questrunner.Travel(d, at, 3) {
		return false
	}
	m, ok := questrunner.FindLoc(d, 0, "Furnace", "", at, 6)
	if !ok {
		logf(d, "no furnace at %d,%d", at.X, at.Z)
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjSodaAsh, m); err != nil {
		logf(d, "soda ash on the furnace: %v", err)
		return false
	}
	return waitUntil(d, 10*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjMoltenGlass) > 0 })
}
