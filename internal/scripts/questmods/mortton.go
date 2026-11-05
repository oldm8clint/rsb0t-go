package questmods

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// MorttonShades is Shades of Mort'ton ready for the engine. The walk in, the kit round and the step-by-step bookkeeping are the engine's own steps; the customs are the parts the
// content makes special: the diary and the serum (a herb table that gives once, a vial from the floor and the sink), the two villagers a dose of serum cures for two minutes, Razmire's
// counters, the shades, the temple's rebuilding and its altar, the oil and the pyre, whose three stages last 49 ticks each.
func MorttonShades() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.MorttonShades,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomMtBurn:      mtBurnLogs,
			questplan.CustomMtTakeDiary: mtTakeDiary,
			questplan.CustomMtReadDiary: mtReadDiary,
			questplan.CustomMtHerbs:     mtHerbs,
			questplan.CustomMtIdentify:  mtIdentify,
			questplan.CustomMtVial:      mtVial,
			questplan.CustomMtWater:     mtWater,
			questplan.CustomMtMixUnf:    mtMixUnf,
			questplan.CustomMtMixSerum:  mtMixSerum,
			questplan.CustomMtDropJunk:  mtDropJunk,
			questplan.CustomMtSend:      mtSend,
			questplan.CustomMtHunt:      mtHunt,
			questplan.CustomMtHandOver:  mtHandOver,
			questplan.CustomMtGeneral:   mtGeneral,
			questplan.CustomMtBuilders:  mtBuilders,
			questplan.CustomMtShow:      mtShow,
			questplan.CustomMtTempleTlk: mtTempleTalk,
			questplan.CustomMtTemple:    mtTemple,
			questplan.CustomMtAltar:     mtAltar,
			questplan.CustomMtPyreLogs:  mtPyreLogs,
			questplan.CustomMtCremate:   mtCremate,
			questplan.CustomMtFinish:    mtFinish,
		},
	}
}

const (
	// mtBoxWait is how long a loc op or an item op gets to put its answer on the pack or the screen.
	mtBoxWait = 8 * time.Second
	// mtTalkBudget bounds one conversation with a villager (Razmire's hand-over is the longest, about twenty pages and a menu) and mtTalkQuiet is how long nothing may have been asked
	// for before it counts as over: these scripts p_delay between pages.
	mtTalkBudget = 120 * time.Second
	mtTalkQuiet  = 4 * time.Second
	// mtDoseWindow is how long a dose of serum keeps a villager talking (200 ticks, two minutes) less a margin: past it a talk spends another dose.
	mtDoseWindow = 170 * time.Second
	// mtFightBudget bounds one fight with a shade (38 hitpoints; two at once is priced), mtHuntBudget one hunt for one set of remains.
	mtFightBudget = 3 * time.Minute
	mtHuntBudget  = 12 * time.Minute
	// mtBuildBudget bounds one stay at the temple's walls, mtBuildClick how long a wall is left to its own loop before the account clicks it again (the server's build loop continues by
	// itself), and mtBuildStray how far from the temple's inside the account may drift (a fight with a shade pulls it out) before it walks back.
	mtBuildBudget = 15 * time.Minute
	mtBuildClick  = 4 * time.Second
	mtBuildStray  = 5
	// mtAltarBudget bounds the lighting and the oil.
	mtAltarBudget = 8 * time.Minute
	// mtAshesWait is how long the fires of a burn are waited out for their ashes (a fire burns 100 to 200 ticks).
	mtAshesWait = 4 * time.Minute
	// mtBookPages is the diary's pages to turn (page 12 is the last, which starts the quest) and mtBookNext the component of its forward arrow in the book interface (837).
	mtBookPages = 13
	mtBookNext  = 841
	// mtMaxRestocks is how many times one stay at the walls goes back to Razmire for material (a restock is five sets, about 4,000 points, and a temple takes some 1,400 of them).
	mtMaxRestocks = 3
	// mtStayFight bounds one answer to a shade that is hitting the builder: a fight that outlasts it is left to the next pass, which looks at the hitpoints and the food first.
	mtStayFight = time.Minute
)

// The stay's two ways of answering a monster that hits the builder (seams for the tests: the live ones are questrunner's). The fight is FightChosen, not FightBack: it says how
// the fight ended, and a lost one (the account died, or was losing and walked away to the quest's bank, which it recorded as a near-death) must end the stay.
var (
	mtAttacker = questrunner.Attacker
	mtFight    = questrunner.FightChosen
)

// mtDosed remembers when an account last used a dose of serum on a villager, by account and villager: the villager's own name is no oracle for the account's window when another
// account may have cured him (the type is shared, the bit that lets a talk through is the account's).
var mtDosed sync.Map

type mtDoseKey struct {
	d   *questrunner.Deps
	who string
}

func mtDoseAge(d *questrunner.Deps, who string) time.Duration {
	if v, ok := mtDosed.Load(mtDoseKey{d, who}); ok {
		return time.Since(v.(time.Time))
	}
	return 24 * time.Hour
}

func mtMarkDosed(d *questrunner.Deps, who string) { mtDosed.Store(mtDoseKey{d, who}, time.Now()) }

// mtVillager is one of the two villagers: his cured face and his afflicted one, and the stop his talks start from.
type mtVillager struct {
	name              string
	cured, afflicted  int
	stop              questplan.NpcStop
	curedOp, shopOpOK bool
}

var (
	mtRazmire  = mtVillager{name: "Razmire", cured: questplan.NpcMtRazmire, afflicted: questplan.NpcMtRazmireAfflicted, stop: questplan.MtRazmireStop}
	mtUlsquire = mtVillager{name: "Ulsquire", cured: questplan.NpcMtUlsquire, afflicted: questplan.NpcMtUlsquireAfflict, stop: questplan.MtUlsquireStop}
)

func mtSettle(d *questrunner.Deps) {
	for i := 0; i < 3; i++ {
		if !questrunner.ClearLeftoverModal(d) {
			return
		}
	}
}

// mtTopUp eats to full hitpoints before a visit to ground the shades walk: nothing can be eaten inside a conversation.
func mtTopUp(d *questrunner.Deps) {
	if hp, max := questrunner.Hitpoints(d); max > 0 && hp < max {
		questrunner.TopUp(d, questplan.MorttonShades.Sustain.FoodIDs, max)
	}
}

// mtSpendVial is the serum vial to spend a dose from: the sanctified ones first (their dose makes a villager's cure permanent), then the one with the fewest doses.
func mtSpendVial(d *questrunner.Deps) int {
	pack := questrunner.PackCounts(d)
	perm, plain := questplan.MtPermSerumIDs(), questplan.MtSerumIDs()
	for i := len(perm) - 1; i >= 0; i-- {
		if pack[perm[i]] > 0 {
			return perm[i]
		}
	}
	for i := len(plain) - 1; i >= 0; i-- {
		if pack[plain[i]] > 0 {
			return plain[i]
		}
	}
	return 0
}

// mtOpenTalk walks to a villager and opens a conversation with him: his own Talk while the account's dose still holds, and a dose of serum used on whichever face he shows otherwise (the use
// both cures him and starts the conversation).
func mtOpenTalk(d *questrunner.Deps, v mtVillager) bool {
	mtSettle(d)
	mtTopUp(d)
	if !questrunner.Travel(d, v.stop.Anchor, 3) {
		logf(d, "mortton: %s (%v) was not reached from %v", v.name, v.stop.Anchor, questrunner.CurrentTile(d))
		return false
	}
	if mtDoseAge(d, v.name) < mtDoseWindow {
		stop := v.stop
		stop.ID = v.cured
		if _, ok := questrunner.FindNpcByID(d, v.cured, "", 14); ok && questrunner.GotoNpc(d, stop, nil) && questrunner.OpenDialogue(d, stop) {
			return true
		}
	}
	vial := mtSpendVial(d)
	if vial == 0 {
		logf(d, "mortton: no serum 207 in the pack to use on %s", v.name)
		return false
	}
	npc, ok := questrunner.FindNpcByID(d, v.afflicted, "", 14)
	if !ok {
		npc, ok = questrunner.FindNpcByID(d, v.cured, "", 14)
	}
	if !ok {
		logf(d, "mortton: no %s beside %v", v.name, v.stop.Anchor)
		return false
	}
	if err := questrunner.UseItemOnNpc(d, vial, npc); err != nil {
		logf(d, "mortton: the serum on %s: %v", v.name, err)
		return false
	}
	mtMarkDosed(d, v.name)
	dl := questrunner.NewDialogue(d)
	if !waitUntil(d, mtBoxWait, dl.Ready) {
		logf(d, "mortton: the serum on %s opened no dialogue", v.name)
		return false
	}
	return true
}

// mtTalk is a whole conversation with a villager: the opening, the driving (each menu line once, the lines in picks first, then the leaving lines), and the clearing of a box left
// behind. until, when it is set, ends the driving early (a conversation that ends in a shop's interface).
func mtTalk(d *questrunner.Deps, v mtVillager, picks []string, until func() bool) bool {
	if !mtOpenTalk(d, v) {
		return false
	}
	spoke := chDriveUntil(d, picks, mtTalkQuiet, mtTalkBudget, until)
	if until == nil || !until() {
		questrunner.ClearLeftoverModal(d)
	}
	if !spoke {
		logf(d, "mortton: nothing was said by %s", v.name)
	}
	return spoke
}

func mtShopIsOpen(d *questrunner.Deps) bool { return actions.NewShop(d.Session).IsOpen() }

// ---------------------------------------------------------------------------------------------------------------------------------
// The fires
// ---------------------------------------------------------------------------------------------------------------------------------

// mtBurnLogs burns logs where the account stands (the log spawns by Varrock's east bank when it is outside the swamp): Step.Qty of them, and then waits out the fires for Step.Radius
// ashes. Each burn is Firemaking experience as well.
func mtBurnLogs(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	if questrunner.HeldID(d, questplan.ObjTinderbox) == 0 || questrunner.HeldID(d, questplan.ObjMtLogs) == 0 {
		logf(d, "mortton: nothing to burn with (a tinderbox %d, logs %d)", questrunner.HeldID(d, questplan.ObjTinderbox), questrunner.HeldID(d, questplan.ObjMtLogs))
		return false
	}
	here := questrunner.CurrentTile(d)
	if !questplan.InMortton(here) && !questplan.InMorttonSwamp(here) && tile.Chebyshev(here, questplan.MtVarrockLogs) > 12 {
		if !questrunner.Travel(d, questplan.MtVarrockLogs, 3) {
			logf(d, "mortton: the log spawns by Varrock's bank were not reached from %v", here)
			return false
		}
	}
	burns, ashes := max(step.Qty, 1), step.Radius
	lit := 0
	for lit < burns && questrunner.HeldID(d, questplan.ObjMtLogs) > 0 {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		mtSettle(d)
		before, mark := questrunner.HeldID(d, questplan.ObjMtLogs), time.Now()
		if err := questrunner.UseItemOnItem(d, questplan.ObjTinderbox, questplan.ObjMtLogs); err != nil {
			logf(d, "mortton: the tinderbox on the logs: %v", err)
			return lit > 0 && ashes == 0
		}
		if !waitUntil(d, mtBoxWait, func() bool {
			return questrunner.HeldID(d, questplan.ObjMtLogs) < before || cookSaw(d, mark, "can't light a fire")
		}) {
			logf(d, "mortton: the logs would not light")
			return lit > 0 && ashes == 0
		}
		if cookSaw(d, mark, "can't light a fire") {
			// A tile that takes no fire: two steps away and again.
			at := questrunner.CurrentTile(d)
			questrunner.WalkNear(d, tile.Tile{X: at.X + 2, Z: at.Z + 1, Level: at.Level}, 0)
			continue
		}
		lit++
		d.Delay(3500 * time.Millisecond) // the lighting and the step off the fire
	}
	if ashes <= 0 {
		return lit > 0
	}
	// The fires die in 100 to 200 ticks and leave ashes on their tiles.
	got := waitUntil(d, mtAshesWait, func() bool {
		questrunner.TakeGround(d, questplan.ObjAshes, questrunner.CurrentTile(d), 14)
		return questrunner.HeldID(d, questplan.ObjAshes) >= ashes
	})
	if !got {
		logf(d, "mortton: %d ashes of the %d wanted after %s", questrunner.HeldID(d, questplan.ObjAshes), ashes, mtAshesWait)
	}
	return got
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The diary and the serum
// ---------------------------------------------------------------------------------------------------------------------------------

// mtLocStep walks to a loc and operates it for an item: the shelf's diary, the table's herbs.
func mtLocFor(d *questrunner.Deps, id int, op string, at tile.Tile, radius int) (locindex.Match, bool) {
	if !questrunner.Travel(d, at, 3) {
		logf(d, "mortton: %v was not reached from %v", at, questrunner.CurrentTile(d))
		return locindex.Match{}, false
	}
	m, ok := questrunner.FindLoc(d, id, "", op, at, radius)
	if !ok {
		logf(d, "mortton: no loc %d offering %q near %v", id, op, at)
	}
	return m, ok
}

// mtTakeDiary searches the shelf for Herbi Flax's diary (a book on a shelf, once, to an account holding none).
func mtTakeDiary(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.HeldID(d, questplan.ObjMtDiary) > 0 {
		return true
	}
	mtSettle(d)
	m, ok := mtLocFor(d, questplan.LocMtShelf, "Search", questplan.MtShelf, 3)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		logf(d, "mortton: the shelf: %v", err)
		return false
	}
	got := waitUntil(d, mtBoxWait, func() bool { return questrunner.HeldID(d, questplan.ObjMtDiary) > 0 })
	mtSettle(d)
	return got
}

// mtReadDiary reads the diary to its last page, which starts the quest: the forward arrow of the book interface is pressed until the tab turns yellow.
func mtReadDiary(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjMtDiary) == 0 {
		logf(d, "mortton: no diary to read")
		return false
	}
	started := func() bool { return questrunner.ReadStatus(d, questplan.MorttonName) == questplan.StatusInProgress }
	if started() {
		return true
	}
	mtSettle(d)
	if err := questrunner.OpHeldByName(d, questplan.ObjMtDiary, "Read"); err != nil {
		logf(d, "mortton: reading the diary: %v", err)
		return false
	}
	// "You prepare to read the book" is a box on the script's Continue; the book opens when it is pressed.
	dl := questrunner.NewDialogue(d)
	if waitUntil(d, mtBoxWait, func() bool { return dl.Ready() || mtBookOpen(d) }) && dl.Ready() {
		dl.Drive(nil)
	}
	if !waitUntil(d, mtBoxWait, func() bool { return mtBookOpen(d) }) {
		logf(d, "mortton: the diary never opened the book interface")
		mtSettle(d)
		return false
	}
	for page := 0; page < mtBookPages && !started(); page++ {
		if err := actions.IfButton(d.Session, mtBookNext); err != nil {
			logf(d, "mortton: turning the page: %v", err)
			break
		}
		if !d.Delay(700 * time.Millisecond) {
			break
		}
	}
	ok := waitUntil(d, mtBoxWait, started)
	mtSettle(d)
	if !ok {
		logf(d, "mortton: the diary was read to the end and the quest tab did not turn yellow")
	}
	return ok
}

func mtBookOpen(d *questrunner.Deps) bool {
	main, _, _ := d.Session.State().Modals()
	return main != gamestate.NoModal
}

// mtHerbs searches the smashed table: two unidentified tarromin and a rogue's purse, once to an account. The table is searched from op 5.
func mtHerbs(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.FreeSlots(d) < 3 {
		logf(d, "mortton: %d free slots for the table's three herbs", questrunner.FreeSlots(d))
		return false
	}
	mtSettle(d)
	m, ok := mtLocFor(d, questplan.LocMtTable, "Search", questplan.MtTable, 3)
	if !ok {
		return false
	}
	before := questrunner.HeldID(d, questplan.ObjMtUnidTarromin)
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		logf(d, "mortton: the table: %v", err)
		return false
	}
	got := waitUntil(d, mtBoxWait, func() bool { return questrunner.HeldID(d, questplan.ObjMtUnidTarromin) > before })
	mtSettle(d)
	return got
}

// mtIdentify identifies one unidentified tarromin (Herblore 11).
func mtIdentify(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	mtSettle(d)
	before := questrunner.HeldID(d, questplan.ObjMtTarromin)
	if err := questrunner.OpHeldByName(d, questplan.ObjMtUnidTarromin, "Identify"); err != nil {
		logf(d, "mortton: identifying the herb: %v", err)
		return false
	}
	return waitUntil(d, mtBoxWait, func() bool { return questrunner.HeldID(d, questplan.ObjMtTarromin) > before })
}

// mtVial takes an empty vial off the floor of the experiment house (two spawns, back on their own timer).
func mtVial(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.FreeSlots(d) < 1 {
		return false
	}
	mtSettle(d)
	if !questrunner.Travel(d, questplan.MtVials, 2) {
		logf(d, "mortton: the experiment house's vials (%v) were not reached from %v", questplan.MtVials, questrunner.CurrentTile(d))
		return false
	}
	before := questrunner.HeldID(d, questplan.ObjMtVial)
	waitUntil(d, 40*time.Second, func() bool {
		if questrunner.TakeGround(d, questplan.ObjMtVial, questplan.MtVials, 4) {
			return true
		}
		return questrunner.HeldID(d, questplan.ObjMtVial) > before
	})
	return questrunner.HeldID(d, questplan.ObjMtVial) > before
}

// mtWater fills an empty vial at the sink (a vial used on the sink).
func mtWater(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	mtSettle(d)
	if !questrunner.Travel(d, questplan.MtSinkNear, 3) {
		logf(d, "mortton: the sink (%v) was not reached from %v", questplan.MtSink, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocMtSink, "", "", questplan.MtSink, 3)
	if !ok {
		logf(d, "mortton: no sink near %v", questplan.MtSink)
		return false
	}
	before := questrunner.HeldID(d, questplan.ObjMtVialWater)
	if err := questrunner.UseItemOnLoc(d, questplan.ObjMtVial, m); err != nil {
		logf(d, "mortton: the vial on the sink: %v", err)
		return false
	}
	return waitUntil(d, mtBoxWait, func() bool { return questrunner.HeldID(d, questplan.ObjMtVialWater) > before })
}

// mtMixUnf puts a tarromin in a vial of water.
func mtMixUnf(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	mtSettle(d)
	if err := questrunner.UseItemOnItem(d, questplan.ObjMtTarromin, questplan.ObjMtVialWater); err != nil {
		logf(d, "mortton: the tarromin in the vial of water: %v", err)
		return false
	}
	return waitUntil(d, mtBoxWait, func() bool { return questrunner.HeldID(d, questplan.ObjMtUnfTarromin) > 0 })
}

// mtMixSerum puts ashes in the unfinished potion: serum 207 (Herblore 15), and the quest's stage 10 when it is the first.
func mtMixSerum(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	mtSettle(d)
	before := questplan.MtDoses(mtSnap(d))
	if err := questrunner.UseItemOnItem(d, questplan.ObjAshes, questplan.ObjMtUnfTarromin); err != nil {
		logf(d, "mortton: the ashes in the unfinished potion: %v", err)
		return false
	}
	return waitUntil(d, mtBoxWait, func() bool { return questplan.MtDoses(mtSnap(d)) > before })
}

// mtSnap is a snapshot of the pack alone, for the dose arithmetic of the plan.
func mtSnap(d *questrunner.Deps) questplan.Snapshot {
	return questplan.Snapshot{InvIDs: questrunner.PackCounts(d)}
}

// mtDropJunk drops the rogue's purse and what the ghasts spoiled, and the diary once the quest has started (it has done its job).
func mtDropJunk(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	mtSettle(d)
	ids := []int{questplan.ObjMtRoguesPurse, questplan.ObjMtRottenFood}
	if questrunner.ReadStatus(d, questplan.MorttonName) == questplan.StatusInProgress {
		ids = append(ids, questplan.ObjMtDiary)
	}
	dropped := 0
	for _, id := range ids {
		for questrunner.HeldID(d, id) > 0 {
			before := questrunner.HeldID(d, id)
			if err := questrunner.OpHeldByName(d, id, "Drop"); err != nil {
				logf(d, "mortton: dropping %s: %v", questrunner.ObjName(d, id), err)
				break
			}
			if !waitUntil(d, 5*time.Second, func() bool { return questrunner.HeldID(d, id) < before }) {
				break
			}
			dropped++
		}
	}
	return dropped > 0
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The villagers and the shop
// ---------------------------------------------------------------------------------------------------------------------------------

// mtSend is Razmire's start: "What are all these shadow creatures?" and then "Yes, I'll dispatch those dark and evil creatures." (stage 15).
func mtSend(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !mtTalk(d, mtRazmire, questplan.MtSendPicks, nil) {
		return false
	}
	e.InvalidateStage()
	return e.StageIs(questplan.MtKillShades)
}

// mtHunt kills one Loar shade (a shadow until it hunts) and takes its remains: the shared field hunt over the town's streets.
func mtHunt(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.FreeSlots(d) < 1 {
		logf(d, "mortton: no room in the pack for a set of remains")
		return false
	}
	mtSettle(d)
	before := questrunner.HeldID(d, questplan.ObjMtRemains)
	h := &fieldHunt{d: d, label: "mortton shade", ids: map[int]bool{questplan.NpcMtShadow: true, questplan.NpcMtShade: true}, field: questplan.MtShadeField, radius: questplan.MtShadeFieldRadius,
		drop: questplan.ObjMtRemains, done: func() bool { return questrunner.HeldID(d, questplan.ObjMtRemains) > before }, sus: questplan.MorttonShades.Sustain, fightBudget: mtFightBudget}
	return h.run(mtHuntBudget)
}

// mtOpenCounter opens one of Razmire's counters: his own op while the account's dose holds, else a conversation (a dose of serum) that ends in the store menu, the line picks taking the
// counter wanted.
func mtOpenCounter(d *questrunner.Deps, op string, picks []string) bool {
	mtSettle(d)
	if mtDoseAge(d, mtRazmire.name) < mtDoseWindow {
		if npc, ok := questrunner.FindNpcByID(d, questplan.NpcMtRazmire, "", 14); ok && npc.Option(op) != 0 {
			if err := entityquery.Interact(d.Session, npc, op); err == nil && waitUntil(d, mtBoxWait, func() bool { return mtShopIsOpen(d) }) {
				return true
			}
		}
	}
	if !mtTalk(d, mtRazmire, picks, func() bool { return mtShopIsOpen(d) }) {
		return false
	}
	return waitUntil(d, mtBoxWait, func() bool { return mtShopIsOpen(d) })
}

// mtBuy buys n more of an object from the open shop and reports how many arrived.
func mtBuy(d *questrunner.Deps, id, n int) int {
	if n <= 0 {
		return 0
	}
	sh := actions.NewShop(d.Session)
	stocked := false
	for _, it := range sh.Stock() {
		if int(it.ID) == id && it.Count > 0 {
			stocked = true
		}
	}
	if !stocked {
		logf(d, "mortton: Razmire has no %s on the shelf", questrunner.ObjName(d, id))
		return 0
	}
	before := questrunner.HeldID(d, id)
	if err := sh.Buy(int32(id), int32(n)); err != nil {
		logf(d, "mortton: buying %s: %v", questrunner.ObjName(d, id), err)
		return 0
	}
	waitUntil(d, 6*time.Second, func() bool { return questrunner.HeldID(d, id) >= before+n })
	got := questrunner.HeldID(d, id) - before
	logf(d, "mortton: bought %d of the %d %s", got, n, questrunner.ObjName(d, id))
	return got
}

// mtStockBuilders buys sets of material from the open builders' store: a plank and a brick each and MtPastePerSet paste.
func mtStockBuilders(d *questrunner.Deps, sets int) bool {
	defer func() { _ = actions.NewShop(d.Session).Close() }()
	if sets <= 0 {
		return false
	}
	planks := mtBuy(d, questplan.ObjMtPlank, sets)
	bricks := mtBuy(d, questplan.ObjMtBrick, sets)
	paste := mtBuy(d, questplan.ObjMtPaste, sets*questplan.MtPastePerSet)
	return planks > 0 && bricks > 0 && paste >= 5
}

// mtBuildersBuy opens Razmire's builders' store (a dose of serum if his window has lapsed) and buys sets of material: the stage 55 conversation's "I keep running out of building
// materials!", the earlier stages' menu line, and the plain store line are the three ways the talk reaches the store.
func mtBuildersBuy(d *questrunner.Deps, sets int) bool {
	if sets <= 0 {
		logf(d, "mortton: no room in the pack for building material")
		return false
	}
	if !mtOpenCounter(d, "Trade-Builders-Store", questplan.MtBuildersPicks) {
		return false
	}
	return mtStockBuilders(d, sets)
}

// mtRestock is the temple's own refill: the pool has run dry, so the account goes to Razmire and buys what the pack has room for. It spends a dose of serum when his window has gone.
func mtRestock(d *questrunner.Deps) bool {
	logf(d, "mortton: the temple's pool is dry; restocking at Razmire's builders' store")
	return mtBuildersBuy(d, questplan.MtSetsThatFit(questrunner.FreeSlots(d)))
}

// mtBuilders is the plan's purchase of building material before the temple is begun.
func mtBuilders(e *questrunner.Engine, step questplan.Step) bool {
	return mtBuildersBuy(e.Deps(), max(step.Qty, 1))
}

// mtGeneral buys from the general store: the hammer or the olive oil (Step.ItemID).
func mtGeneral(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	id := step.ItemID
	if id == 0 {
		id = questplan.ObjHammer
	}
	if questrunner.HeldID(d, id) > 0 && id == questplan.ObjHammer {
		return true
	}
	if !mtOpenCounter(d, "Trade-General-Store", questplan.MtGeneralPicks) {
		return false
	}
	defer func() { _ = actions.NewShop(d.Session).Close() }()
	return mtBuy(d, id, 1) > 0
}

// mtHandOver is Razmire's first conversation after the fifth shade: he takes two sets of remains (the pack must hold five) and the talk ends in his store menu, where the builders'
// store is taken; the hammer is bought from the general store at once, while his dose holds.
func mtHandOver(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	before := questrunner.HeldID(d, questplan.ObjMtRemains)
	if before < questplan.MtRemainsWanted {
		logf(d, "mortton: %d sets of remains, Razmire wants %d", before, questplan.MtRemainsWanted)
		return false
	}
	if !mtTalk(d, mtRazmire, questplan.MtHandOverPicks, func() bool { return mtShopIsOpen(d) }) {
		return false
	}
	if questrunner.HeldID(d, questplan.ObjMtRemains) > before-2 {
		logf(d, "mortton: Razmire did not take the remains (%d held)", questrunner.HeldID(d, questplan.ObjMtRemains))
		mtSettle(d)
		return false
	}
	// What the general store is to give while his window holds: the hammer (the temple wants it), and the olive oil (the altar's), which the plan would otherwise spend another dose on.
	wantHammer, wantOil := questrunner.HeldID(d, questplan.ObjHammer) == 0, questplan.MtOliveDoses(mtSnap(d)) < questplan.MtPyreSacredDose && questplan.MtSacredDoses(mtSnap(d)) < questplan.MtPyreSacredDose
	reserve := 0
	if wantHammer {
		reserve++
	}
	if wantOil {
		reserve++
	}
	if mtShopIsOpen(d) {
		mtStockBuilders(d, questplan.MtSetsThatFit(questrunner.FreeSlots(d)-reserve))
	}
	if wantHammer || wantOil {
		if npc, ok := questrunner.FindNpcByID(d, questplan.NpcMtRazmire, "", 14); ok && npc.Option("Trade-General-Store") != 0 {
			if entityquery.Interact(d.Session, npc, "Trade-General-Store") == nil && waitUntil(d, mtBoxWait, func() bool { return mtShopIsOpen(d) }) {
				if wantHammer {
					mtBuy(d, questplan.ObjHammer, 1)
				}
				if wantOil {
					mtBuy(d, questplan.ObjMtOil3, 1)
				}
				_ = actions.NewShop(d.Session).Close()
			}
		}
	}
	mtSettle(d)
	return true
}

// mtShow shows Ulsquire a set of remains (stage 47) and mtTempleTalk asks him about the temple (50): the second talk is the same window's.
func mtShow(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	before := questrunner.HeldID(d, questplan.ObjMtRemains)
	if !mtTalk(d, mtUlsquire, nil, nil) {
		return false
	}
	return questrunner.HeldID(d, questplan.ObjMtRemains) < before
}

func mtTempleTalk(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !mtTalk(d, mtUlsquire, questplan.MtTemplePicks, nil) {
		return false
	}
	e.InvalidateStage()
	return e.StageIs(questplan.MtUlsquireTemple)
}

// mtFinish tells Ulsquire the shade is at rest: the quest's end, the scroll of its completion on the main modal.
func mtFinish(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !mtTalk(d, mtUlsquire, nil, nil) {
		return false
	}
	mtSettle(d)
	return waitUntil(d, 15*time.Second, func() bool { return questrunner.ReadStatus(d, questplan.MorttonName) == questplan.StatusComplete })
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The temple
// ---------------------------------------------------------------------------------------------------------------------------------

func mtRepaired(d *questrunner.Deps) int   { return d.Session.State().Varp(questplan.VarpMtRepaired) }
func mtPool(d *questrunner.Deps) int       { return d.Session.State().Varp(questplan.VarpMtResources) }
func mtSanctity(d *questrunner.Deps) int   { return d.Session.State().Varp(questplan.VarpMtSanctity) }
func mtHere(d *questrunner.Deps) tile.Tile { return questrunner.CurrentTile(d) }

// mtWallOffering is the nearest wall piece offering op inside the temple's reach: Repair on the pieces below their tenth level, Reinforce on the finished ones.
func mtWallOffering(d *questrunner.Deps, op string) (locindex.Match, bool) {
	var ids []int
	if op == "Reinforce" {
		ids = []int{questplan.LocMtWallDone, questplan.LocMtCornerDone}
	} else {
		for id := questplan.LocMtWallBase; id < questplan.LocMtWallDone; id++ {
			ids = append(ids, id)
		}
		for id := questplan.LocMtCornerBase; id < questplan.LocMtCornerDone; id++ {
			ids = append(ids, id)
		}
	}
	here := mtHere(d)
	var best locindex.Match
	bestD, found := 0, false
	for _, id := range ids {
		m, ok := questrunner.FindLoc(d, id, "", op, questplan.MtTempleInside, 8)
		if !ok {
			continue
		}
		if dist := tile.Chebyshev(tile.Tile{X: m.X, Z: m.Z, Level: m.Level}, here); !found || dist < bestD {
			best, bestD, found = m, dist, true
		}
	}
	return best, found
}

// mtAltarLocs is the altar in each of its three forms.
func mtAltarOf(d *questrunner.Deps, id int, op string) (locindex.Match, bool) {
	return questrunner.FindLoc(d, id, "", op, questplan.MtAltar, 4)
}

// mtStandInside walks the account to the inside of the temple's shell. It never walks the account in from outside the town: an account that stands outside it was walked there by a
// lost fight (to the quest's bank, across Mort Myre) or respawned at Lumbridge, and the way back across the swamp is no stay's to make.
func mtStandInside(d *questrunner.Deps) bool {
	here := mtHere(d)
	if tile.Chebyshev(here, questplan.MtTempleInside) <= mtBuildStray {
		return true
	}
	if !questplan.InMortton(here) {
		logf(d, "mortton: the account stands at %v, outside the town: not walking back to the temple", here)
		return false
	}
	if !questrunner.Travel(d, questplan.MtTempleInside, 2) {
		logf(d, "mortton: the temple's inside %v was not reached from %v", questplan.MtTempleInside, mtHere(d))
		return false
	}
	return true
}

// mtTick is what one pass at the walls found: nothing to report, the temple's pool dry, or the stay over.
type mtTick int

const (
	// mtTickWork: the pass clicked a wall, answered a box or a shade, or had nothing to do; the stay goes on.
	mtTickWork mtTick = iota
	// mtTickDry: the temple says its pool is dry; the account goes to Razmire for material.
	mtTickDry
	// mtTickLeave: the stay is over. A fight was lost (the account died, or walked away to the quest's bank), the bot is stopping, or the way back to the temple's inside was not
	// walked. Nothing more is done at the walls: the engine counts the death or the near-death after the step, and the plan's decision says whether the account may come back.
	mtTickLeave
)

// mtFightEnd names how a fight ended, for the log.
func mtFightEnd(r questrunner.FightResult) string {
	switch r {
	case questrunner.FightWon:
		return "won"
	case questrunner.FightGaveUp:
		return "given up"
	case questrunner.FightLost:
		return "lost"
	case questrunner.FightStopped:
		return "cut short by a stop"
	case questrunner.FightTimedOut:
		return "still on after " + mtStayFight.String()
	}
	return fmt.Sprint(int(r))
}

// mtStaySnap is the pack, the hitpoints and the levels as the plan reads them, for a price made in the middle of a stay.
func mtStaySnap(d *questrunner.Deps) questplan.Snapshot {
	snap := questrunner.BuildSnapshot(d, questplan.MorttonName, 0)
	snap.HP, snap.MaxHP = questrunner.Hitpoints(d)
	snap.Levels = questrunner.BaseLevels(d)
	return snap
}

// mtLostWhy says why the account is no longer at the temple's walls whatever the walls need, "" while it is: it died since the stay began (the game's death line; the engine counts
// it once the step returns), or it stands outside the town (a lost fight walked it to the quest's bank, a death respawned it at Lumbridge).
func mtLostWhy(d *questrunner.Deps, mark time.Time) string {
	if questrunner.DiedSince(d, mark) {
		return "the account died"
	}
	if here, ok := questrunner.CurrentTileOK(d); ok && !questplan.InMortton(here) {
		return fmt.Sprintf("the account stands at %v, outside the town", here)
	}
	return ""
}

// mtPackWhy says why the pack in hand can no longer see the stay through, "" while it can (questplan.MtStayVerdict: a floor of bites, then the shades priced as the entry's gate
// prices them). The step ends and the plan's own gate (MtShadesReady) decides whether the account may come back, with food or with more hitpoints.
func mtPackWhy(d *questrunner.Deps) string {
	if v := questplan.MtStayVerdict(mtStaySnap(d)); !v.OK {
		return v.Why
	}
	return ""
}

// mtBuildTick clicks the nearest wall that wants work (Repair, else Reinforce), no more often than the server's own loop needs, and answers boxes. It reports a dry pool as
// mtTickDry, and the end of the stay as mtTickLeave: a shade that hits the builder is fought with the shared fight (FightChosen, the quest's sustain), and a fight that is lost
// (the account died, or was losing and was walked away to the quest's bank) must not be followed by a walk back to the temple.
func mtBuildTick(e *questrunner.Engine, lastClick *time.Time) mtTick {
	d := e.Deps()
	dl := questrunner.NewDialogue(d)
	if dl.Ready() {
		texts := strings.ToLower(strings.Join(dl.Texts(), " "))
		dl.Drive(nil)
		questrunner.ClearLeftoverModal(d)
		if strings.Contains(texts, "material resource pool") || strings.Contains(texts, "repair the temple you need") {
			return mtTickDry
		}
		return mtTickWork
	}
	if questrunner.ClearLeftoverModal(d) {
		return mtTickWork
	}
	// Whatever is hitting the account is fought back (the shades come for the builder as well as the walls).
	if npc, hit := mtAttacker(d); hit {
		*lastClick = time.Time{}
		end := mtFight(d, npc, questplan.MorttonShades.Sustain, mtStayFight, nil)
		switch end {
		case questrunner.FightLost, questrunner.FightStopped:
			logf(d, "mortton: the fight with a shade at the temple was %s: the stay is over", mtFightEnd(end))
			return mtTickLeave
		}
		return mtTickWork
	}
	if !mtStandInside(d) {
		return mtTickLeave
	}
	if time.Since(*lastClick) < mtBuildClick {
		return mtTickWork
	}
	op := "Repair"
	wall, ok := mtWallOffering(d, op)
	if !ok {
		op = "Reinforce"
		wall, ok = mtWallOffering(d, op)
	}
	if !ok {
		return mtTickWork
	}
	if err := questrunner.OperateLoc(d, wall, op); err != nil {
		logf(d, "mortton: the wall: %v", err)
	}
	*lastClick = time.Now()
	return mtTickWork
}

// mtTemple builds the Flamtaer temple until the fire altar appears: the hammer on the walls (at Crafting 20 and a pool of material from the builders' store), the shades kept off the
// walls and the account by fighting back. The varps the temple transmits say when it is done; the pool being dry (the temple's own box) sends the account to Razmire for more. The
// stay is a run of fights, not one: every pass looks first at whether the account died or stands outside the town, and then at whether the pack in hand could still take two shades
// (mtPackWhy); a lost fight ends it at once (mtBuildTick), and an ended stay returns false with the reason, so the engine counts the death or the near-death and the plan's gate
// decides whether the account may go back.
func mtTemple(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjHammer) == 0 {
		logf(d, "mortton: no hammer in the pack for the temple")
		return false
	}
	mark := time.Now()
	mtSettle(d)
	mtTopUp(d)
	if !mtStandInside(d) {
		return false
	}
	deadline := time.Now().Add(mtBuildBudget)
	if left := e.RunLeft(); left > 0 && time.Now().Add(left).Before(deadline) {
		deadline = time.Now().Add(left)
	}
	var last time.Time
	built, restocks := false, 0
	lastSan, lastPool := mtSanctity(d), mtPool(d)
	saidAt := time.Now()
	for time.Now().Before(deadline) {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		if why := mtLostWhy(d, mark); why != "" {
			logf(d, "mortton: leaving the temple at %d%% repaired: %s", mtRepaired(d), why)
			return false
		}
		san, pool := mtSanctity(d), mtPool(d)
		built = built || san > lastSan || pool < lastPool
		lastSan, lastPool = san, pool
		if _, altar := mtAltarOf(d, questplan.LocMtAltarUnlit, "Light"); built && mtRepaired(d) >= 100 && altar {
			logf(d, "mortton: the temple is rebuilt: sanctity %d%%, pool %d%%", san, pool)
			return true
		}
		if why := mtPackWhy(d); why != "" {
			logf(d, "mortton: leaving the temple at %d%% repaired: %s", mtRepaired(d), why)
			return false
		}
		if time.Since(saidAt) >= 90*time.Second {
			saidAt = time.Now()
			logf(d, "mortton: building: repaired %d%% pool %d%% sanctity %d%%, %d planks, %d bricks, %d paste", mtRepaired(d), pool, san,
				questrunner.HeldID(d, questplan.ObjMtPlank), questrunner.HeldID(d, questplan.ObjMtBrick), questrunner.HeldID(d, questplan.ObjMtPaste))
		}
		switch mtBuildTick(e, &last) {
		case mtTickLeave:
			logf(d, "mortton: the stay at the temple ends at %d%% repaired", mtRepaired(d))
			return false
		case mtTickDry:
			logf(d, "mortton: out of building material at %d%% repaired, pool %d%%", mtRepaired(d), mtPool(d))
			if restocks >= mtMaxRestocks || !mtRestock(d) {
				return false
			}
			restocks++
			last = time.Time{}
			continue
		}
		if !d.Delay(900 * time.Millisecond) {
			return false
		}
	}
	logf(d, "mortton: the build budget ended at %d%% repaired", mtRepaired(d))
	return built && mtRepaired(d) >= 100
}

// mtAltar lights the altar and puts olive oil in the flame: sacred oil, and stage 65. The altar is struck with a tinderbox once the sanctity is at 10 percent (the walls' actions raise it,
// and it decays by a percent a minute), the flame burns for 99 ticks and the oil is poured at once.
func mtAltar(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	mark := time.Now()
	mtSettle(d)
	if !mtStandInside(d) {
		return false
	}
	deadline := time.Now().Add(mtAltarBudget)
	var last time.Time
	saidAt, restocks := time.Now(), 0
	for time.Now().Before(deadline) {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		if why := mtLostWhy(d, mark); why != "" {
			logf(d, "mortton: leaving the altar: %s", why)
			return false
		}
		if _, lit := mtAltarOf(d, questplan.LocMtAltarLit, ""); lit {
			break
		}
		if why := mtPackWhy(d); why != "" {
			logf(d, "mortton: leaving the altar: %s", why)
			return false
		}
		if time.Since(saidAt) >= 60*time.Second {
			saidAt = time.Now()
			logf(d, "mortton: lighting: repaired %d%% pool %d%% sanctity %d%%", mtRepaired(d), mtPool(d), mtSanctity(d))
		}
		if cold, ok := mtAltarOf(d, questplan.LocMtAltarUnlit, "Light"); ok && mtSanctity(d) >= 10 && questrunner.HeldID(d, questplan.ObjTinderbox) > 0 {
			mtSettle(d)
			if err := questrunner.OperateLoc(d, cold, "Light"); err != nil {
				logf(d, "mortton: striking the altar: %v", err)
			}
			// The server's own loop strikes until the flame catches (a roll every third tick); only a quiet minute is struck again.
			waitUntil(d, 40*time.Second, func() bool {
				_, lit := mtAltarOf(d, questplan.LocMtAltarLit, "")
				return lit
			})
			continue
		}
		// A broken altar, or too little sanctity: the walls' actions mend the one and raise the other.
		switch mtBuildTick(e, &last) {
		case mtTickLeave:
			logf(d, "mortton: the stay at the altar ends")
			return false
		case mtTickDry:
			logf(d, "mortton: no material for the altar's build tick (sanctity %d%%)", mtSanctity(d))
			if restocks >= mtMaxRestocks || !mtRestock(d) {
				return false
			}
			restocks++
			last = time.Time{}
			continue
		}
		if !d.Delay(900 * time.Millisecond) {
			return false
		}
	}
	lit, ok := mtAltarOf(d, questplan.LocMtAltarLit, "")
	if !ok {
		logf(d, "mortton: the sacred flame never caught (sanctity %d%%)", mtSanctity(d))
		return false
	}
	mtSettle(d)
	oil := 0
	for _, id := range questplan.MtOilIDs() {
		if questrunner.HeldID(d, id) > 0 && id != questplan.ObjMtOil1 {
			oil = id
			break
		}
	}
	if oil == 0 {
		logf(d, "mortton: no olive oil of two doses or more in the pack")
		return false
	}
	before := questplan.MtSacredDoses(mtSnap(d))
	if err := questrunner.UseItemOnLoc(d, oil, lit); err != nil {
		logf(d, "mortton: the oil on the altar: %v", err)
		return false
	}
	got := waitUntil(d, mtBoxWait, func() bool { return questplan.MtSacredDoses(mtSnap(d)) > before })
	mtSettle(d)
	return got
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The pyre
// ---------------------------------------------------------------------------------------------------------------------------------

// mtSacredVial is the sacred oil vial of two doses or more in the pack, 0 for none.
func mtSacredVial(d *questrunner.Deps) int {
	for _, id := range questplan.MtSacredIDs() {
		if id != questplan.ObjMtSacred1 && questrunner.HeldID(d, id) > 0 {
			return id
		}
	}
	return 0
}

// mtPyreLogs makes a pyre log: sacred oil used on a log (two doses of it for ordinary logs).
func mtPyreLogs(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjMtPyreLogs) > 0 {
		return true
	}
	oil := mtSacredVial(d)
	if oil == 0 || questrunner.HeldID(d, questplan.ObjMtLogs) == 0 {
		logf(d, "mortton: no sacred oil of two doses (%d) or no log (%d) for a pyre log", oil, questrunner.HeldID(d, questplan.ObjMtLogs))
		return false
	}
	mtSettle(d)
	if err := questrunner.UseItemOnItem(d, oil, questplan.ObjMtLogs); err != nil {
		logf(d, "mortton: the oil on the log: %v", err)
		return false
	}
	return waitUntil(d, mtBoxWait, func() bool { return questrunner.HeldID(d, questplan.ObjMtPyreLogs) > 0 })
}

// mtCremate is the three actions of the cremation back to back (each stage of the pyre lasts 49 ticks): the pyre logs stacked on an empty pyre, a set of remains laid on them, and the
// pyre lit (a Firemaking roll on every third tick until it takes).
func mtCremate(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	for _, id := range []int{questplan.ObjMtPyreLogs, questplan.ObjMtRemains, questplan.ObjTinderbox} {
		if questrunner.HeldID(d, id) == 0 {
			logf(d, "mortton: no %s for the cremation", questrunner.ObjName(d, id))
			return false
		}
	}
	mtSettle(d)
	if !questrunner.Travel(d, questplan.MtPyre, 3) {
		logf(d, "mortton: the funeral pyres (%v) were not reached from %v", questplan.MtPyre, mtHere(d))
		return false
	}
	empty, ok := questrunner.FindLoc(d, questplan.LocMtPyreEmpty, "", "", questplan.MtPyre, 8)
	if !ok {
		logf(d, "mortton: no empty funeral pyre near %v", questplan.MtPyre)
		return false
	}
	at := tile.Tile{X: empty.X, Z: empty.Z, Level: empty.Level}
	// Approach the pyre once, so that every action after it starts beside it.
	if !questrunner.ApproachLoc(d, empty) {
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjMtPyreLogs, empty); err != nil {
		logf(d, "mortton: the pyre logs on the pyre: %v", err)
		return false
	}
	loaded, ok := mtPyreOf(d, questplan.LocMtPyreLogs, at, mtBoxWait)
	if !ok {
		logf(d, "mortton: the pyre logs did not stack on the pyre at %v", at)
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjMtRemains, loaded); err != nil {
		logf(d, "mortton: the remains on the pyre: %v", err)
		return false
	}
	bodied, ok := mtPyreOf(d, questplan.LocMtPyreBones, at, mtBoxWait)
	if !ok {
		logf(d, "mortton: the remains did not lie on the pyre at %v", at)
		return false
	}
	if err := questrunner.OperateLoc(d, bodied, "Light"); err != nil {
		logf(d, "mortton: lighting the pyre: %v", err)
		return false
	}
	// The loaded pyre reverts to its empty form either when it has burned (two ticks after the light takes) or when its 49 ticks run out unlit: the journal says which.
	waitUntil(d, 90*time.Second, func() bool {
		_, still := mtPyreOf(d, questplan.LocMtPyreBones, at, 0)
		return !still
	})
	mtSettle(d)
	e.InvalidateStage()
	if !e.StageIs(questplan.MtLitPyre) {
		logf(d, "mortton: the pyre at %v went out before it caught", at)
		return false
	}
	return true
}

// mtPyreOf finds the pyre at a place in the form id (a live loc change), waiting up to wait for it.
func mtPyreOf(d *questrunner.Deps, id int, at tile.Tile, wait time.Duration) (locindex.Match, bool) {
	var m locindex.Match
	find := func() bool {
		var ok bool
		m, ok = questrunner.FindLoc(d, id, "", "", at, 1)
		return ok
	}
	if wait <= 0 {
		return m, find()
	}
	return m, waitUntil(d, wait, find)
}
