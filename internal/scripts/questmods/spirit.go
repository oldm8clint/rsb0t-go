package questmods

import (
	"strings"
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// NatureSpirit is Nature Spirit ready for the engine.
func NatureSpirit() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.NatureSpirit,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomSpiritDrezel: spiritDrezel,
			questplan.CustomSpiritTalk: func(e *questrunner.Engine, _ questplan.Step) bool {
				return spiritTalkTopic(e, "I'm wearing an amulet of ghost speak!")
			},
			questplan.CustomSpiritScroll: func(e *questrunner.Engine, _ questplan.Step) bool {
				return spiritTalkTopic(e, "Could I have another bloom scroll please?")
			},
			questplan.CustomSpiritHelp:     spiritHelp,
			questplan.CustomSpiritMirror:   spiritMirror,
			questplan.CustomSpiritJournal:  spiritJournal,
			questplan.CustomSpiritBloom:    spiritBloomScroll,
			questplan.CustomSpiritStones:   spiritStones,
			questplan.CustomSpiritPuzzle:   spiritPuzzle,
			questplan.CustomSpiritEnter:    func(e *questrunner.Engine, _ questplan.Step) bool { return enterGrotto(e.Deps()) },
			questplan.CustomSpiritLeave:    func(e *questrunner.Engine, _ questplan.Step) bool { return leaveGrotto(e.Deps()) },
			questplan.CustomSpiritGrotto:   spiritGrottoTalk,
			questplan.CustomSpiritDipBless: spiritDip,
			questplan.CustomSpiritSmelt:    spiritSmelt,
			questplan.CustomSpiritCast:     spiritCastSickle,
			questplan.CustomSpiritHarvest:  spiritHarvest,
			questplan.CustomSpiritFill:     spiritFill,
			questplan.CustomSpiritGhast:    spiritGhast,
		},
	}
}

var spiritLeave = []string{"Ok, thanks.", "Ok thanks."}

func filliman(d *questrunner.Deps) (entityquery.Entity, bool) {
	return questrunner.FindNpcByID(d, questplan.NpcFilliman, "", 8)
}

// spiritDrezel goes down to the mausoleum and talks to the freed Drezel (the start, and the
// blessing at stage 35).
func spiritDrezel(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questplan.IsUnderground(questrunner.CurrentTile(d)) && !pipDown(d) {
		return false
	}
	ok := e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: questplan.DrezelFree})
	e.InvalidateStage()
	return ok
}

// driveOnce drives Filliman's looping topic menu: the topic once (when offered), then the
// goodbye. Each menu is answered by its exact text through DriveSequence, which also continues
// the pages that follow it up to the next menu.
func driveOnce(d *questrunner.Deps, topic string) bool {
	dl := questrunner.NewDialogue(d)
	taken := topic == ""
	for i := 0; i < 20; i++ {
		if !dl.Ready() && !waitUntil(d, 2*time.Second, dl.Ready) {
			return taken
		}
		opts := dl.OptionTexts()
		if len(opts) == 0 {
			dl.Drive(nil)
			continue
		}
		if !taken {
			if pick, ok := questplan.PickPreferred(opts, []string{topic}); ok {
				taken = true
				dl.DriveSequence([]string{pick}, 20*time.Second)
				continue
			}
		}
		leave, ok := questplan.PickPreferred(opts, spiritLeave)
		if !ok {
			logf(d, "no goodbye in [%s]", strings.Join(opts, " | "))
			return taken
		}
		dl.DriveSequence([]string{leave}, 20*time.Second)
		return taken
	}
	return taken
}

// summonFilliman walks to the camp and, when he is not there, knocks on the grotto door, which
// npc_adds him for 100 ticks (and before the ritual opens his dialogue at once).
func summonFilliman(d *questrunner.Deps) bool {
	if !questrunner.Travel(d, questplan.SpiritCamp, 3) {
		return false
	}
	if _, ok := filliman(d); ok {
		return true
	}
	m, ok := questrunner.FindLoc(d, questplan.LocGrottoDoor, "", "Enter", questplan.GrottoDoorTile, 3)
	if !ok {
		logf(d, "no grotto door at the camp to summon Filliman with")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Enter"); err != nil {
		return false
	}
	dl := questrunner.NewDialogue(d)
	return waitUntil(d, 8*time.Second, func() bool { _, ok := filliman(d); return ok || dl.Ready() })
}

// spiritTalkTopic summons Filliman and takes one topic from his menu.
func spiritTalkTopic(e *questrunner.Engine, topic string) bool {
	d := e.Deps()
	if !summonFilliman(d) {
		return false
	}
	dl := questrunner.NewDialogue(d)
	if !dl.Ready() {
		stop := questplan.NpcStop{Npc: "Filliman Tarlock", ID: questplan.NpcFilliman, Anchor: questplan.FillimanTile, Leash: 4, Prefer: spiritLeave}
		if !questrunner.OpenDialogue(d, stop) {
			return false
		}
	}
	ok := driveOnce(d, topic)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return ok
}

func spiritHelp(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	spiritTalkTopic(e, "How can I help?")
	return waitUntil(d, 6*time.Second, func() bool { return held(d, questplan.ObjBloomSpell) })
}

// useOnFilliman summons him and uses a held item on him, then drives the talk that follows.
func useOnFilliman(e *questrunner.Engine, item int, done func() bool) bool {
	d := e.Deps()
	if !summonFilliman(d) {
		return false
	}
	questrunner.ClearLeftoverModal(d)
	npc, ok := filliman(d)
	if !ok {
		return false
	}
	if err := questrunner.UseItemOnNpc(d, item, npc); err != nil {
		logf(d, "item %d on Filliman: %v", item, err)
		return false
	}
	questrunner.NewDialogue(d).DriveUntil(done, spiritLeave, 30*time.Second)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return true
}

// spiritMirror takes the washing bowl (which uncovers a mirror), the mirror, and shows it to him.
func spiritMirror(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !held(d, questplan.ObjMirror) {
		if !questrunner.Travel(d, questplan.WashingBowlTile, 1) {
			return false
		}
		if len(questrunner.GroundItemsNear(d, questplan.ObjMirror, questplan.WashingBowlTile, 1)) == 0 {
			questrunner.TakeGround(d, 2964, questplan.WashingBowlTile, 1)
			waitUntil(d, 6*time.Second, func() bool {
				return len(questrunner.GroundItemsNear(d, questplan.ObjMirror, questplan.WashingBowlTile, 1)) > 0
			})
			questrunner.ClearLeftoverModal(d)
		}
		if !questrunner.TakeGround(d, questplan.ObjMirror, questplan.WashingBowlTile, 1) {
			logf(d, "no mirror under the washing bowl")
			return false
		}
	}
	return useOnFilliman(e, questplan.ObjMirror, func() bool { return false })
}

// spiritJournal searches the grotto tree for the journal and hands it over.
func spiritJournal(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !held(d, questplan.ObjFillJournal) {
		if !questrunner.Travel(d, questplan.SpiritCamp, 3) {
			return false
		}
		m, ok := questrunner.FindLoc(d, questplan.LocGrottoTree, "", "Search", questplan.GrottoTreeTile, 3)
		if !ok {
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
			return false
		}
		questrunner.NewDialogue(d).DriveUntil(func() bool { return held(d, questplan.ObjFillJournal) }, nil, 8*time.Second)
		questrunner.ClearLeftoverModal(d)
		if !held(d, questplan.ObjFillJournal) {
			return false
		}
	}
	return useOnFilliman(e, questplan.ObjFillJournal, func() bool { return !held(d, questplan.ObjFillJournal) })
}

// ---------------------------------------------------------------------------
// Bloom
// ---------------------------------------------------------------------------

var bloomables = []string{"Rotting log", "Rotting branch", "A small bush"}

var pickables = []struct{ name, op string }{
	{"Fungi on log", "Pick"}, {"Budding branch", "Take-cutting"}, {"A golden pear bush", "Pick"},
}

// pickable is a bloomed loc within reach and its pick op.
func pickable(d *questrunner.Deps) (string, string, bool) {
	here := questrunner.CurrentTile(d)
	for _, p := range pickables {
		if _, ok := questrunner.FindLoc(d, 0, p.name, p.op, here, 2); ok {
			return p.name, p.op, true
		}
	}
	return "", "", false
}

// standBeside stands orthogonally next to a bloomable (every one is blockwalk=no, and the bloom
// touches only the eight tiles around the caster, so standing ON one misses it).
func standBeside(d *questrunner.Deps, names []string) bool {
	if !questplan.InMortMyre(questrunner.CurrentTile(d)) && !questrunner.Travel(d, questplan.SpiritCamp, 3) {
		return false
	}
	beside := func() bool {
		here := questrunner.CurrentTile(d)
		for _, n := range names {
			if m, ok := questrunner.FindLoc(d, 0, n, "", here, 1); ok && (m.X != here.X || m.Z != here.Z) {
				return true
			}
		}
		return false
	}
	if beside() {
		return true
	}
	here := questrunner.CurrentTile(d)
	for _, n := range names {
		m, ok := questrunner.FindLoc(d, 0, n, "", here, 40)
		if !ok {
			continue
		}
		for _, off := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			stand := tile.Tile{X: m.X + off[0], Z: m.Z + off[1]}
			if questrunner.Travel(d, stand, 0) && beside() {
				return true
			}
		}
	}
	logf(d, "nothing bloomable within reach")
	return beside()
}

func pickOnce(d *questrunner.Deps, count func() int) bool {
	name, op, ok := pickable(d)
	if !ok {
		return false
	}
	m, found := questrunner.FindLoc(d, 0, name, op, questrunner.CurrentTile(d), 2)
	if !found {
		return false
	}
	before := count()
	if err := questrunner.OperateLoc(d, m, op); err != nil {
		return false
	}
	return waitUntil(d, 8*time.Second, func() bool { return count() > before })
}

// spiritBloomScroll casts the paper scroll beside a rotting log and picks the fungus it grows,
// in one step: a bloom reverts after 25 ticks.
func spiritBloomScroll(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	fungi := func() int { return questrunner.HeldID(d, questplan.ObjMortFungi) }
	for cycle := 0; cycle < 8 && fungi() == 0; cycle++ {
		if _, _, ok := pickable(d); ok {
			pickOnce(d, fungi)
			continue
		}
		if !standBeside(d, []string{"Rotting log"}) {
			return false
		}
		if !held(d, questplan.ObjBloomSpell) {
			return false
		}
		if err := questrunner.OpHeldByName(d, questplan.ObjBloomSpell, "Cast"); err != nil {
			logf(d, "casting the bloom scroll: %v", err)
			return false
		}
		waitUntil(d, 8*time.Second, func() bool { _, _, ok := pickable(d); return ok })
	}
	e.InvalidateStage()
	return fungi() > 0
}

// useOnStone uses a held item on the ritual stone at its own tile (all three are "Stone").
func useOnStone(d *questrunner.Deps, item int, id int, at tile.Tile) bool {
	if !questrunner.Travel(d, at, 2) {
		return false
	}
	m, ok := questrunner.FindLoc(d, id, "", "", at, 0)
	if !ok {
		logf(d, "no ritual stone at %d,%d", at.X, at.Z)
		return false
	}
	if err := questrunner.UseItemOnLoc(d, item, m); err != nil {
		logf(d, "item %d on the stone: %v", item, err)
		return false
	}
	ok = questrunner.NewDialogue(d).DriveUntil(func() bool { return !held(d, item) }, spiritLeave, 15*time.Second)
	questrunner.ClearLeftoverModal(d)
	return ok
}

// spiritStones feeds the fungus to the nature stone and a bloom scroll (used or not) to the
// spirit stone, whichever the journal says is still hungry.
func spiritStones(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	e.InvalidateStage()
	snap := e.Snapshot(0)
	_, nature, spirit := questplan.SpiritTracks(snap.Stage)
	ok := true
	if !nature {
		ok = useOnStone(d, questplan.ObjMortFungi, questplan.LocNatureStone, questplan.NatureStoneTile)
	}
	if ok && !spirit {
		scroll := questplan.ObjUsedSpell
		if !held(d, scroll) {
			scroll = questplan.ObjBloomSpell
		}
		ok = useOnStone(d, scroll, questplan.LocSpiritStone, questplan.SpiritStoneTile)
	}
	e.InvalidateStage()
	return ok
}

// spiritPuzzle summons Filliman, stands on the faith stone (the ritual is judged on the tile the
// account stands on when the option is chosen) and tells him the puzzle is solved.
func spiritPuzzle(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !summonFilliman(d) {
		return false
	}
	// A summon before stage 55 opens his talk at once; let it finish first.
	driveOnce(d, "")
	questrunner.ClearLeftoverModal(d)
	questrunner.WalkNear(d, questplan.FaithStoneTile, 0)
	here := questrunner.CurrentTile(d)
	if here.X != questplan.FaithStoneTile.X || here.Z != questplan.FaithStoneTile.Z {
		logf(d, "not on the faith stone (%v)", here)
		return false
	}
	npc, ok := filliman(d)
	if !ok {
		logf(d, "Filliman despawned between the summon and the stone")
		return false
	}
	op, _ := talkOp(npc)
	if err := entityquery.Interact(d.Session, npc, op); err != nil {
		return false
	}
	dl := questrunner.NewDialogue(d)
	waitUntil(d, 6*time.Second, dl.Ready)
	driveOnce(d, "I think I've solved the puzzle!")
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return true
}

func talkOp(npc entityquery.Entity) (string, bool) {
	for _, op := range npc.Ops {
		if strings.EqualFold(op, "Talk-to") {
			return op, true
		}
	}
	return "Talk-to", false
}

// ---------------------------------------------------------------------------
// The grotto
// ---------------------------------------------------------------------------

func enterGrotto(d *questrunner.Deps) bool {
	if questplan.InGrotto(questrunner.CurrentTile(d)) {
		return true
	}
	if !questrunner.Travel(d, questplan.GrottoDoorTile, 2) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocGrottoDoor, "", "Enter", questplan.GrottoDoorTile, 3)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Enter"); err != nil {
		return false
	}
	return questrunner.NewDialogue(d).DriveUntil(func() bool { return questplan.InGrotto(questrunner.CurrentTile(d)) }, spiritLeave, 15*time.Second)
}

func leaveGrotto(d *questrunner.Deps) bool {
	if !questplan.InGrotto(questrunner.CurrentTile(d)) {
		return true
	}
	// FindLoc matches by id or by name, never by op alone: with neither, the questproof EMPTY lab
	// run spirit-empty-1 read "no grotto exit in the scene" standing beside it.
	m, ok := questrunner.FindLoc(d, 0, "Grotto", "Exit", questrunner.CurrentTile(d), 16)
	if !ok {
		logf(d, "no grotto exit in the scene")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Exit"); err != nil {
		return false
	}
	return waitUntil(d, 15*time.Second, func() bool { return !questplan.InGrotto(questrunner.CurrentTile(d)) })
}

// spiritGrottoTalk talks to whichever form is in the grotto, searching the pool to summon one.
func spiritGrottoTalk(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !enterGrotto(d) {
		return false
	}
	find := func() (entityquery.Entity, bool) {
		if n, ok := questrunner.FindNpcByID(d, questplan.NpcNatureSpirit, "", 12); ok {
			return n, true
		}
		return questrunner.FindNpcByID(d, questplan.NpcFilliman, "", 12)
	}
	dl := questrunner.NewDialogue(d)
	if _, ok := find(); !ok {
		m, found := questrunner.FindLoc(d, questplan.LocGrottoPool, "", "Search", questplan.GrottoPoolTile, 12)
		if !found {
			logf(d, "no grotto pool to search")
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
			return false
		}
		waitUntil(d, 8*time.Second, func() bool { _, ok := find(); return ok || dl.Ready() })
	}
	if !dl.Ready() {
		npc, ok := find()
		if !ok {
			return false
		}
		op, _ := talkOp(npc)
		if err := entityquery.Interact(d.Session, npc, op); err != nil {
			return false
		}
		waitUntil(d, 6*time.Second, dl.Ready)
	}
	driveOnce(d, "")
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return true
}

// spiritDip replaces a lost blessed sickle: a plain sickle used on the grotto pool.
func spiritDip(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !enterGrotto(d) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocGrottoPool, "", "", questplan.GrottoPoolTile, 12)
	if !ok {
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjSilverSickle, m); err != nil {
		return false
	}
	return questrunner.NewDialogue(d).DriveUntil(func() bool { return held(d, questplan.ObjSickleBlessed) }, nil, 15*time.Second)
}

// ---------------------------------------------------------------------------
// The sickle
// ---------------------------------------------------------------------------

func atFurnace(d *questrunner.Deps, use int, product string, done func() bool) bool {
	if done() {
		return true
	}
	if !questrunner.Travel(d, questplan.AlKharidFurnace, 2) {
		return false
	}
	m, ok := questrunner.FindLoc(d, 0, "Furnace", "", questplan.AlKharidFurnace, 6)
	if !ok {
		logf(d, "no furnace at Al Kharid")
		return false
	}
	if err := questrunner.UseItemOnLoc(d, use, m); err != nil {
		logf(d, "item %d on the furnace: %v", use, err)
		return false
	}
	// Ore on a furnace smelts at once with no menu (smelting.rs2's @smelt_ore_single); only a
	// bar opens one (jewellery.rs2 offers the sickle).
	if !waitUntil(d, 8*time.Second, func() bool { return done() || questrunner.IsMakeMenu(d) }) {
		logf(d, "the furnace neither made %s nor opened a make menu", product)
		return false
	}
	if done() {
		return true
	}
	if err := questrunner.MakeOne(d, product); err != nil {
		logf(d, "make %s: %v", product, err)
		return false
	}
	return waitUntil(d, 30*time.Second, done)
}

func spiritSmelt(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	return atFurnace(d, questplan.ObjSilverOre, "Silver", func() bool { return held(d, questplan.ObjSilverBar) })
}

func spiritCastSickle(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	return atFurnace(d, questplan.ObjSilverBar, "sickle", func() bool { return held(d, questplan.ObjSilverSickle) })
}

// mineSilver is the provisioner's skill source for silver ore (Mining 20, a pickaxe needed).
func mineSilver(d *questrunner.Deps, task questplan.SourceTask) bool {
	return questrunner.ExecuteStep(d, questplan.Step{Kind: questplan.StepMineRock, LocID: 2100, Loc: "Rocks",
		ItemID: questplan.ObjSilverOre, Item: "Silver ore", Qty: task.Qty, Anchor: task.Source.Tile}, nil)
}

// ---------------------------------------------------------------------------
// Harvest and ghasts
// ---------------------------------------------------------------------------

func harvestHeld(d *questrunner.Deps) int {
	return questrunner.HeldID(d, questplan.ObjMortPear) + questrunner.HeldID(d, questplan.ObjMortStem) + questrunner.HeldID(d, questplan.ObjMortFungi)
}

// rechargePrayer tops Prayer up at the Paterdomus altar (the grotto's altar is post-quest only).
func rechargePrayer(d *questrunner.Deps) bool {
	if !leaveGrotto(d) || !questrunner.Travel(d, questplan.PaterdomusAltar, 2) {
		return false
	}
	m, ok := questrunner.FindLoc(d, 0, "Altar", "Pray-at", questplan.PaterdomusAltar, 8)
	if !ok {
		logf(d, "no altar to pray at near Paterdomus")
		return false
	}
	before := questrunner.CurrentLevel(d, "prayer")
	if err := questrunner.OperateLoc(d, m, "Pray-at"); err != nil {
		return false
	}
	return waitUntil(d, 8*time.Second, func() bool { return questrunner.CurrentLevel(d, "prayer") > before })
}

// spiritHarvest casts Bloom with the blessed sickle beside something bloomable and picks, until
// three harvests are held; each cast drains 1-7 Prayer.
func spiritHarvest(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	for cycle := 0; cycle < 12 && harvestHeld(d) < 3; cycle++ {
		if _, _, ok := pickable(d); ok {
			pickOnce(d, func() int { return harvestHeld(d) })
			continue
		}
		if questrunner.CurrentLevel(d, "prayer") < questplan.BloomMaxCost {
			if !rechargePrayer(d) {
				return false
			}
		}
		if !standBeside(d, bloomables) {
			return false
		}
		if err := questrunner.OpHeldByName(d, questplan.ObjSickleBlessed, "Cast Bloom"); err != nil {
			logf(d, "Cast Bloom: %v", err)
			return false
		}
		waitUntil(d, 6*time.Second, func() bool { _, _, ok := pickable(d); return ok })
	}
	e.InvalidateStage()
	return harvestHeld(d) >= 3
}

func spiritFill(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	pouch := questplan.ObjPouchEmpty
	if held(d, questplan.ObjPouch) {
		pouch = questplan.ObjPouch
	}
	before := questrunner.HeldID(d, questplan.ObjPouch)
	if err := questrunner.OpHeldByName(d, pouch, "Fill"); err != nil {
		logf(d, "filling the pouch: %v", err)
		return false
	}
	e.InvalidateStage()
	return waitUntil(d, 6*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjPouch) > before })
}

// spiritGhast pops an invisible ghast with a pouch charge and kills what appears: the reward
// counts only for the ghast's own hero.
func spiritGhast(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	visible := func() (entityquery.Entity, bool) { return questrunner.FindNpcByID(d, questplan.NpcGhastShown, "", 12) }
	if _, ok := visible(); !ok {
		hidden, found := questrunner.FindNpcByID(d, questplan.NpcGhastHidden, "", 12)
		if !found {
			questrunner.Travel(d, questplan.GhastHunt, 3)
			logf(d, "no ghast in the scene: moving through the swamp until one hunts")
			return false
		}
		if err := questrunner.UseItemOnNpc(d, questplan.ObjPouch, hidden); err != nil {
			return false
		}
		if !waitUntil(d, 8*time.Second, func() bool { _, ok := visible(); return ok }) {
			logf(d, "the pouch charge did not make a ghast visible")
			return false
		}
	}
	g, ok := visible()
	if !ok {
		return false
	}
	won := fightBest(d, questplan.Step{Kind: questplan.StepFight, TargetID: questplan.NpcGhastShown, Target: "Ghast"},
		questplan.NatureSpirit.Sustain)
	_ = g
	e.InvalidateStage()
	return won
}
