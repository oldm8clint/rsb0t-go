package fleetrunner

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/thieverplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// CoinsID is the real coin object (995), matched by id like every sibling runner does.
const CoinsID int32 = 995

func ObjName(d *Deps, id int32) string {
	if d.Config == nil {
		return ""
	}
	if o, ok := d.Config.Obj(int(id)); ok {
		return o.Name
	}
	return ""
}

func KitItems(d *Deps, items []gamestate.SlotItem) []fleetplan.KitItem {
	out := make([]fleetplan.KitItem, 0, len(items))
	for _, it := range items {
		if it.ID <= 0 || it.Count <= 0 {
			continue
		}
		name := ObjName(d, it.ID)
		if name == "" {
			name = fmt.Sprintf("obj %d", it.ID)
		}
		out = append(out, fleetplan.KitItem{Name: name, Count: int(it.Count)})
	}
	return out
}

func PackSlotItems(d *Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

// WornItems is what the account is wearing, by name.
func WornItems(d *Deps) []fleetplan.KitItem {
	if d.Components == nil {
		return nil
	}
	return KitItems(d, actions.NewEquipment(d.Session, d.Components).Worn())
}

// PackItems is the pack, by name.
func PackItems(d *Deps) []fleetplan.KitItem { return KitItems(d, PackSlotItems(d)) }

// CurrentKit is worn plus pack, the input CheckWildernessKit takes.
func CurrentKit(d *Deps) fleetplan.Kit {
	return fleetplan.Kit{Worn: WornItems(d), Pack: PackItems(d)}
}

// RuneCounts sums runes by name over item rows.
func RuneCounts(d *Deps, items []gamestate.SlotItem) fleetplan.RuneStock {
	out := fleetplan.RuneStock{}
	for _, it := range items {
		name := ObjName(d, it.ID)
		if fleetplan.IsRuneName(name) {
			out[name] += int(it.Count)
		}
	}
	return out
}

// PackRunes is the runes in the pack.
func PackRunes(d *Deps) fleetplan.RuneStock { return RuneCounts(d, PackSlotItems(d)) }

// PackValue is the shop value (content's obj cost; coins at face value) of everything in the pack: the figure the Paladin or Hero
// test's loot value an hour is measured with (scarabplan/rungtest.go). 0 with no config.
func PackValue(d *Deps) int {
	if d == nil || d.Config == nil {
		return 0
	}
	total := 0
	for _, it := range PackSlotItems(d) {
		if obj, ok := d.Config.Obj(int(it.ID)); ok {
			total += int(it.Count) * int(obj.Cost)
		}
	}
	return total
}

// OnlyBladeLeft reports whether worn and pack hold nothing but blades — the condition for
// leaving the Mage Bank with nothing a death could cost.
func OnlyBladeLeft(d *Deps) bool {
	k := CurrentKit(d)
	for _, it := range append(append([]fleetplan.KitItem{}, k.Worn...), k.Pack...) {
		if !fleetplan.IsWildernessBlade(it.Name) {
			return false
		}
	}
	return true
}

// Here is the account's tile.
func Here(d *Deps) *tile.Tile { return CurrentTile(d) }

// DriveDialogue answers dialogue until done() is true: pick the first option containing one of
// choose, else continue, else close a leftover message box — one pass a tick, the same loop
// webwalk/hops runs for a dialogue crossing. It gives up after timeout, or after settle with no
// dialogue ever seen (the op did nothing).
func DriveDialogue(d *Deps, label string, choose []string, done func() bool, timeout, settle time.Duration) bool {
	dlg := actions.NewChatDialog(d.Session, d.Components)
	started := time.Now()
	sawDialogue := false
	for time.Since(started) < timeout {
		if done() {
			return true
		}
		acted := false
		if opts := liveOptions(d, dlg); len(opts) > 0 {
			for _, o := range opts {
				text := strings.ToLower(o.Text)
				for _, c := range choose {
					if strings.Contains(text, strings.ToLower(c)) {
						if err := actions.IfButton(d.Session, o.ComID); err == nil {
							d.Logf("%s: chose %q", label, o.Text)
							acted = true
						}
						break
					}
				}
				if acted {
					break
				}
			}
		}
		if !acted && dlg.CanContinue() {
			if err := dlg.Continue(); err == nil {
				acted = true
			}
		}
		if acted {
			sawDialogue = true
		}
		if !d.Delay(600 * time.Millisecond) {
			return false
		}
		if !sawDialogue && !dlg.IsOpen() && time.Since(started) >= settle {
			if !done() {
				main, side, chat := d.Session.State().Modals()
				d.Logf("%s: no dialogue appeared within %s (modals main=%d side=%d chat=%d, at %s; game chat %q)", label, settle, main, side, chat, HereIs(d), ChatSince(d, started))
			}
			return done()
		}
	}
	if !done() {
		main, side, chat := d.Session.State().Modals()
		d.Logf("%s: gave up after %s (modals main=%d side=%d chat=%d, options %v, at %s; game chat %q)", label, timeout, main, side, chat, liveOptions(d, dlg), HereIs(d), ChatSince(d, started))
	}
	return done()
}

func ChatSince(d *Deps, t time.Time) string {
	if d.Chat == nil {
		return ""
	}
	return strings.TrimSpace(d.Chat.Since(t))
}

func liveOptions(d *Deps, dlg *actions.ChatDialog) []actions.Option {
	opts := dlg.Options()
	st := d.Session.State()
	for i := range opts {
		if text, ok := st.TextKnown(opts[i].ComID); ok && text != "" {
			opts[i].Text = text
		}
	}
	return opts
}

// FindLoc is the nearest loc named name offering op within radius of at, from the static index
// merged with live changes (a cut web is a live change to "Slashed web").
func FindLoc(d *Deps, name, op string, at tile.Tile, radius int) (entityquery.Entity, bool) {
	q := entityquery.Locs(d.Session.State(), d.Config, d.Locs, 30).Name(name).Action(op).WithinOf(at.X, at.Z, radius)
	return q.Nearest()
}

// WildWalkTimeout bounds one Wilderness walk. The longest leg is ~70 tiles.
const WildWalkTimeout = 120 * time.Second

// WildExitTimeout bounds the walk south out of the Wilderness when the Keep lever is out of
// reach: about 440 tiles from the Rogues' Castle to the Wilderness line, three to four minutes
// running.
const WildExitTimeout = 6 * time.Minute

// WalkTo walks with the webwalk Walker (doors, levels, ships) — the only walk Stage 3 uses —
// with White Wolf Mountain's dens as no-go rectangles.
//
// Live-found 2026-09-19 (cmd/scarabstage3test -phases botstaff): a combat-3 Stage 3 account sent
// from East Ardougne to Zaff in Varrock walked the land route over the pass and died at
// 2850,3508 with two White wolves (38) and two Ice warriors (57) beside it, losing the staff
// money. The named "white-wolf-mountain" zone is not automatic, so a default walk goes straight
// over it. MuleReceiver already solved this for its own route home (bot/mulehome.go): plan with
// randomevents.WolfTerritorySquares as AvoidRects, which from the Ardougne side leaves the boat
// chain (Brimhaven and Port Sarim ships) as the way east. Stage 3 now does the same for every
// walk. A route the planner cannot find without the dens is NOT retried over them; only a short
// local walk falls back to plain pathfinding.
//
// With Deps.Travel set (travel.go), a long walk may start with a teleport, and the walk casts
// High Level Alchemy on carried bows as it goes; both are off unless their settings are on.
func WalkTo(d *Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
	return WalkToAvoiding(d, dest, radius, timeout, nil)
}

// WalkToAvoiding is WalkTo with extra rectangles the route keeps out of, beside the wolves' dens and the Ardougne bears: the
// Karamja corridor's spiders, for a RuneCarrier (runecraftplan.AvoidBoxes; the walker's own hostile avoidance knows two
// species and neither is a jungle spider). A route the planner cannot find around them is not retried over them, as with the
// wolves; only a short local walk falls back to plain pathfinding.
func WalkToAvoiding(d *Deps, dest tile.Tile, radius int, timeout time.Duration, extra []webwalk.Rect) bool {
	if d.Walker == nil {
		return freshstartrunner.WalkWithWalker(Fresh(d), dest, radius, timeout)
	}
	// The Walker's own hook decides from the route it plans (telehook.go); the pre-walk estimate is for a Walker without one.
	if d.Walker.BeforeFollow == nil {
		d.Travel.teleportBeforeWalk(d, dest)
	}
	d.Travel.beginWalk(d)
	rects := AvoidRects()
	if len(extra) > 0 {
		rects = append(append(make([]webwalk.Rect, 0, len(rects)+len(extra)), rects...), extra...)
	}
	// A walk that starts on a monster pen's ground leaves it by the way that holds the least of the herd's tiles, never clicks
	// blind there, and eats at every wait (penwalk.go).
	var stepCost func(x, z, level int) int
	var noBlind func(webwalk.Point) bool
	inPen := false
	if from := CurrentTile(d); from != nil {
		stepCost, noBlind, _ = PenWalkGuard(*from)
		inPen = noBlind != nil && noBlind(webwalk.Point{X: from.X, Z: from.Z, Level: from.Level})
	}
	// A walk made hurt and with nothing to eat (the food path itself, or an escape: the ones that are neither are not begun, bot
	// scarabswarm_walkrest.go) keeps off every pen's hunting ground it can and applies every level-based danger zone (hurtwalk.go).
	hurt := walkHurt(d)
	if hurt {
		stepCost = hurtStepCost()
		sayHurtWalk(d, dest)
	}
	eat := WalkEater(d)
	aside := d.Travel.walkAside(d)
	res := d.Walker.WalkTo(webwalk.Point{X: dest.X, Z: dest.Z, Level: dest.Level}, webwalk.WalkOptions{
		Radius: radius, Timeout: timeout, AvoidRects: rects,
		AvoidHostiles: freshstartrunner.HostileEvadeEnabled,
		Hurt:          hurt,
		Aside: func(p webwalk.AsidePoll) {
			if aside != nil {
				aside(p)
			}
			eat(p)
		},
		Watch:      func() { eat(webwalk.AsidePoll{}) },
		ClickSteps: d.Travel.clickSteps(),
		Search:     webwalk.Options{StepCost: stepCost},
		NoBlind:    noBlind,
	})
	d.Travel.CloseRubMenu(d)
	d.Travel.endWalk(d)
	if res.Arrived() {
		return true
	}
	here := CurrentTile(d)
	// The plain walk is for a short local walk the wolf rectangles cannot have caused to fail. From a pen's ground it is the
	// straight line through the herd the route above was planned to avoid.
	if here != nil && !inPen && here.Level == dest.Level && tile.Chebyshev(*here, dest) <= 30 {
		return freshstartrunner.WalkWithWalker(Fresh(d), dest, radius, timeout)
	}
	note := ""
	if res.Outcome == webwalk.OutcomeUnreachable {
		note = " (planned around the wolf dens and the Ardougne bears; no route is tried over them)"
	}
	d.Logf("walk to %d,%d,%d did not arrive (%v: %s), at %s%s", dest.X, dest.Z, dest.Level, res.Outcome, res.Reason, HereIs(d), note)
	return false
}

// WolfRects is White Wolf Mountain's dens (content spawns, widened 2 tiles) as webwalk rectangles.
func WolfRects() []webwalk.Rect {
	squares := randomevents.WolfTerritorySquares(2)
	out := make([]webwalk.Rect, 0, len(squares))
	for _, s := range squares {
		out = append(out, webwalk.Rect{MinX: s.MinX, MaxX: s.MaxX, MinZ: s.MinZ, MaxZ: s.MaxZ, Level: 0})
	}
	return out
}

// ArdougneBears is the Brown bears' ground east of the Stage 3 cow field (nine level-21 spawns at
// 2691-2718,3330-3344 in maps/m42_52.jm2, wanderrange 4, huntmode cowardly, huntrange 1 — they
// attack a low-level account that walks past them), widened by the wander range.
//
// Live-found 2026-09-19 (cmd/scarabstage3test -phases e2e): a combat-5 account leaving the cow
// field for the bank took the field's east gate (2675,3350) on a plain FindPath, walked south past
// them and died at 2687,3341 with two Bears beside it.
var ArdougneBears = webwalk.Rect{MinX: 2685, MaxX: 2724, MinZ: 3324, MaxZ: 3350, Level: 0}

// AvoidRects is every rectangle a Scarab walk keeps out of.
func AvoidRects() []webwalk.Rect {
	return append(WolfRects(), ArdougneBears)
}

// OpenBank walks to a bank on WalkTo — the webwalk with avoidRects — and only then lets
// thieverrunner.OpenBankAt cover the last few tiles and click the booth. OpenBankAt's own walk is
// a plain FindPath that knows nothing of the bears or the wolves; it is what walked the e2e
// account into the bears. A long walk that does not arrive is not handed to it either.
func OpenBank(d *Deps, at tile.Tile) (*actions.Bank, bool) {
	if here := CurrentTile(d); here == nil || here.Level != at.Level || tile.Chebyshev(tile.Tile{X: here.X, Z: here.Z}, at) > 6 {
		if !WalkTo(d, at, 4, 4*time.Minute) {
			d.Logf("bank at %d,%d: the walk did not arrive (at %s); not falling back to a plain path", at.X, at.Z, HereIs(d))
			return nil, false
		}
	}
	return thieverrunner.OpenBankAt(Thiev(d), at)
}

// OpenBankDepositing is OpenBank opening blind (actions.OpenBankBlindOps, the speed of
// MasterCrafter's six-tick gem stream): standing at the booth, the click goes out as the fifth
// handled user event of its tick with a Deposit-All of every pack kind match accepts (nil: all)
// right behind it, read the tick after the bank opens, then confirmed once; what the blind burst
// missed is sent again paced. A caller's own deposit call after it finds nothing left to send.
func OpenBankDepositing(d *Deps, at tile.Tile, match func(gamestate.SlotItem) bool) (*actions.Bank, bool) {
	if here := CurrentTile(d); here == nil || here.Level != at.Level || tile.Chebyshev(tile.Tile{X: here.X, Z: here.Z}, at) > 6 {
		if !WalkTo(d, at, 4, 4*time.Minute) {
			d.Logf("bank at %d,%d: the walk did not arrive (at %s); not falling back to a plain path", at.X, at.Z, HereIs(d))
			return nil, false
		}
	}
	t := Thiev(d)
	return thieverrunner.OpenBankAtWith(t, at, func() []actions.BankOp { return thieverrunner.PackDeposits(t, match) })
}

// walkWildUntil walks toward dest, stopping early when stop() turns true (combat starting, so
// the caller can re-pick the heading).
func walkWildUntil(d *Deps, dest tile.Tile, radius int, stop func() bool) bool {
	return freshstartrunner.WalkWithWalkerUntil(Fresh(d), dest, radius, WildWalkTimeout, stop)
}

// EnsureRun turns run on when it is off. Best effort: run energy is not modelled here.
func EnsureRun(d *Deps) {
	if !d.Session.State().RunEnabled() {
		_ = actions.SetRun(d.Session, true)
	}
}

// KitPrep is what PrepareKit read and did.
type KitPrep struct {
	Verdict fleetplan.KitVerdict
	Plan    fleetplan.PurchasePlan
	// BankRunes and BankCoins are the bank's rune and coin counts once everything was deposited
	// and before the purchase coins came out.
	BankRunes fleetplan.RuneStock
	BankCoins int
	// BankStaff is the best autocast staff in the bank (a Staff of air before any other), "" for
	// none — the rune maths needs it: a Staff of air makes every air rune free.
	BankStaff string
	Reason    string
}

// Planner turns the bank's contents into the trip's purchase — scarabplan's decision, handed in
// so the runner never decides how many coins the trip spends.
type Planner func(bankRunes fleetplan.RuneStock, bankCoins int) fleetplan.PurchasePlan

// PrepareKit banks the account down to the owner's Wilderness kit in East Ardougne: every worn
// item that is not a sword comes off, everything in the pack goes in the bank, one blade comes
// back out and is wielded, and the bank's runes and coins are read so plan() can decide the
// purchase. No coins leave the bank here — they are drawn at Gundai inside the Mage Bank (owner,
// 2026-09-19). The kit is then re-read and checked with CheckWildernessKit — the verdict, not this
// function's intent, says whether the account may pull the lever.
func PrepareKit(d *Deps, plan Planner) KitPrep {
	var out KitPrep
	eq := actions.NewEquipment(d.Session, d.Components)
	// 1. Unequip anything worn that is not a sword (a staff, a shield...). Done before the bank
	//    opens: the equipment tab is what Unequip clicks.
	StripWornExcept(d, eq, "stage 3 kit", isSwordName)
	bank, ok := OpenBankDepositing(d, thieverplan.ArdougneBank, nil)
	if !ok {
		out.Reason = "could not open the East Ardougne bank"
		return out
	}
	defer func() { _ = bank.Close() }()

	// 2. Everything in the pack goes in — blades included, so a duplicate can never ride along —
	//    and then, unless a sword is already wielded, exactly one blade comes back out (a sword
	//    first; a knife only when there is no sword).
	if err := bank.BatchDepositInventory(thieverrunner.Waiter(Thiev(d))); err != nil {
		d.Logf("stage 3 kit: depositing the pack: %v", err)
	}
	keep := ""
	for _, it := range eq.Worn() {
		if n := ObjName(d, it.ID); fleetplan.IsWildernessBlade(n) {
			keep = n
		}
	}
	if keep == "" {
		keep = withdrawBlade(d, bank)
	}

	// 4. Read the bank and plan.
	items := bank.Items()
	out.BankRunes = RuneCounts(d, items)
	for _, it := range items {
		if it.ID == CoinsID {
			out.BankCoins += int(it.Count)
		}
		if n := ObjName(d, it.ID); fleetplan.BetterStaff(n, out.BankStaff, fleetplan.Spell{}) {
			out.BankStaff = n
		}
	}
	out.Plan = plan(out.BankRunes, out.BankCoins)
	_ = bank.Close()
	d.Delay(600 * time.Millisecond)

	// 5. Wield the sword (a knife stays in the pack; it cannot be wielded).
	if keep != "" && !strings.EqualFold(keep, fleetplan.KnifeItem) {
		for _, it := range eq.Inventory() {
			if strings.EqualFold(ObjName(d, it.ID), keep) {
				if err := eq.Equip(it.ID); err != nil {
					d.Logf("stage 3 kit: could not wield %s: %v", keep, err)
				}
				WaitFor(d, "wield "+keep, 3*time.Second, func() bool { return eq.IsWorn(it.ID) })
				break
			}
		}
	}
	out.Verdict = fleetplan.CheckWildernessKit(CurrentKit(d))
	if !out.Verdict.OK && out.Reason == "" {
		out.Reason = strings.Join(out.Verdict.Problems, "; ")
	}
	return out
}

func withdrawBlade(d *Deps, bank *actions.Bank) string {
	var sword, knife *gamestate.SlotItem
	for _, it := range bank.Items() {
		it := it
		n := ObjName(d, it.ID)
		if !fleetplan.IsWildernessBlade(n) {
			continue
		}
		if o, ok := d.Config.Obj(int(it.ID)); ok && o.Noted() {
			continue
		}
		if strings.EqualFold(n, fleetplan.KnifeItem) {
			if knife == nil {
				knife = &it
			}
		} else if sword == nil {
			sword = &it
		}
	}
	pick := sword
	if pick == nil {
		pick = knife
	}
	if pick == nil {
		d.Logf("stage 3 kit: no sword or knife in the bank either — the webs cannot be cut")
		return ""
	}
	name := ObjName(d, pick.ID)
	if err := bank.WithdrawOne(pick.ID); err != nil {
		d.Logf("stage 3 kit: withdrawing %s: %v", name, err)
		return ""
	}
	WaitFor(d, name+" in the pack", 3*time.Second, func() bool {
		for _, it := range PackSlotItems(d) {
			if it.ID == pick.ID {
				return true
			}
		}
		return false
	})
	return name
}

// PullArdougneLever walks to the East Ardougne lever, pulls it, answers the warning ("Yes I'm
// brave.") and waits to land in the Wilderness.
//
// The walk is radius 0 — onto the lever's own tile. Live-found 2026-09-19 (cmd/scarabstage3test
// -phases e2e): the lever is a straight wall decoration (loc 1814, shape 4, maps/m40_51.jm2) and
// the Keep lever lands an account one tile east of it (^wilderness_to_ardougne_coord 2562,3311).
// A radius-1 walk counted that as arrived, and every pull from there answered "I can't reach
// that!" — twenty minutes of it, until something else walked the account away.
func PullArdougneLever(d *Deps) bool {
	if !WalkTo(d, fleetplan.ArdougneLever, 0, 60*time.Second) {
		d.Logf("stage 3: could not reach the Ardougne lever from %s", HereIs(d))
		return false
	}
	lever, ok := FindLoc(d, fleetplan.LeverName, fleetplan.LeverOp, fleetplan.ArdougneLever, 2)
	if !ok {
		d.Logf("stage 3: no Lever with Pull at %d,%d", fleetplan.ArdougneLever.X, fleetplan.ArdougneLever.Z)
		return false
	}
	inWild := func() bool {
		t := CurrentTile(d)
		return t != nil && fleetplan.WildernessLevel(*t) > 0
	}
	landed := false
	for attempt := 1; attempt <= 3 && !landed; attempt++ {
		if err := entityquery.Interact(d.Session, lever, fleetplan.LeverOp); err != nil {
			d.Logf("stage 3: pulling the Ardougne lever: %v", err)
			return false
		}
		landed = DriveDialogue(d, "Ardougne lever", []string{fleetplan.ArdougneLeverBrave}, inWild, 20*time.Second, 5*time.Second)
	}
	if !landed {
		d.Logf("stage 3: the Ardougne lever did not land in the Wilderness (at %s)", HereIs(d))
		return false
	}
	d.Logf("stage 3: pulled the Ardougne lever — Wilderness level %d at %s", fleetplan.WildernessLevel(*CurrentTile(d)), HereIs(d))
	return true
}

// PullKeepLever walks to the Deserted Keep lever (fleeing is the same walk) and pulls it.
func PullKeepLever(d *Deps) bool {
	EnsureRun(d)
	if !cutWebsFromLadder(d, nil) {
		return false
	}
	if here := CurrentTile(d); here != nil && here.Z > fleetplan.KeepWeb.Z {
		// North of the Keep web (coming back from the house): cross it by hand first.
		if !crossWeb(d, fleetplan.KeepWeb, fleetplan.KeepWebNorth, fleetplan.KeepWebSouth, nil) {
			return false
		}
	}
	// Radius 0, as for the Ardougne lever: the Keep lever is the same straight wall decoration
	// (loc 1815, shape 4, maps/m49_61.jm2) and the Ardougne lever lands an account diagonally next
	// to it (3154,3924), where a pull answers "I can't reach that!" (cmd/scarabstage3test
	// -phases leverloop, 2026-09-19).
	if !walkWildUntil(d, fleetplan.KeepLever, 0, nil) {
		// From the east the lever's only route slashes the Keep web from its north side, which
		// the walker will not hop (see KeepWeb) and an account without a blade cannot. Walking
		// south out of the Wilderness is the other way home: outside it, Region3Elsewhere walks
		// the account back. Live 6 Oct 2026: acct113 (Stage 5's magic half, no blade) logged
		// "could not reach the Keep lever" from the Rogues' Castle every 25 s for hours.
		d.Logf("stage 3: could not reach the Keep lever from %s — walking south out of the Wilderness instead", HereIs(d))
		out := func() bool {
			t := CurrentTile(d)
			return t != nil && fleetplan.WildernessLevel(*t) == 0
		}
		if !freshstartrunner.WalkWithWalkerUntil(Fresh(d), fleetplan.WildernessSouthExit, 2, WildExitTimeout, out) {
			d.Logf("stage 3: did not get out of the Wilderness (at %s)", HereIs(d))
		}
		return false
	}
	lever, ok := FindLoc(d, fleetplan.LeverName, fleetplan.LeverOp, fleetplan.KeepLever, 2)
	if !ok {
		d.Logf("stage 3: no Lever with Pull at the Keep")
		return false
	}
	if err := entityquery.Interact(d.Session, lever, fleetplan.LeverOp); err != nil {
		d.Logf("stage 3: pulling the Keep lever: %v", err)
		return false
	}
	home := func() bool {
		t := CurrentTile(d)
		return t != nil && fleetplan.NearArdougneLever(*t)
	}
	if !DriveDialogue(d, "Keep lever", nil, home, 15*time.Second, 6*time.Second) {
		d.Logf("stage 3: the Keep lever did not land in Ardougne (at %s)", HereIs(d))
		return false
	}
	d.Logf("stage 3: back in East Ardougne at %s", HereIs(d))
	return true
}

// WebsCut counts web slashes that worked and failed across a trip, for the report.
type WebsCut struct {
	Cut, Failed int
}

// EnterMageBank walks to the ruined house, cuts whichever of the two webs are standing, and climbs
// down the ladder. Under attack it keeps going — the ladder IS the nearer escape once the account
// is at the house. Returns false with the reason logged.
func EnterMageBank(d *Deps, webs *WebsCut) bool {
	EnsureRun(d)
	if here := CurrentTile(d); here != nil && here.Z < fleetplan.KeepWeb.Z && here.X > fleetplan.LadderHouseDoor.X+20 {
		// Still south of the Keep web: cross it by hand first.
		if !crossWeb(d, fleetplan.KeepWeb, fleetplan.KeepWebSouth, fleetplan.KeepWebNorth, webs) {
			return false
		}
	}
	if !walkWildUntil(d, fleetplan.LadderHouseDoor, 1, nil) {
		d.Logf("stage 3: could not reach the Mage Bank house from %s", HereIs(d))
		return false
	}
	if !cutWebsToLadder(d, webs) {
		return false
	}
	ladder, ok := FindLoc(d, fleetplan.LadderName, fleetplan.LadderDownOp, fleetplan.MageBankLadder, 2)
	if !ok {
		d.Logf("stage 3: no Ladder with Climb-down at %d,%d", fleetplan.MageBankLadder.X, fleetplan.MageBankLadder.Z)
		return false
	}
	for attempt := 0; attempt < 4; attempt++ {
		at := time.Now()
		// Step up to the ladder first rather than letting the engine route to it: measured live
		// (2026-09-19), a Climb-down sent from beyond the cut web answered "I can't reach that!"
		// every time, even with both webs open server-side.
		beside := tile.Tile{X: fleetplan.MageBankLadder.X + 1, Z: fleetplan.MageBankLadder.Z}
		if here := CurrentTile(d); here == nil || *here != beside {
			StepTo(d, beside)
		}
		if err := entityquery.Interact(d.Session, ladder, fleetplan.LadderDownOp); err != nil {
			d.Logf("stage 3: climbing down: %v", err)
			return false
		}
		if WaitFor(d, "the Mage Bank", 10*time.Second, func() bool {
			t := CurrentTile(d)
			return t != nil && fleetplan.InMageBank(*t)
		}) {
			d.Logf("stage 3: in the Mage Bank at %s", HereIs(d))
			return true
		}
		d.Logf("stage 3: the ladder did not take us down from %s; game chat %q", HereIs(d), ChatSince(d, at))
		// A cut web grows back after 100 ticks (20 seconds on the 3x lab engine), so the one
		// between us and the ladder may be whole again: cut only the webs still ahead (west).
		if !cutWebsToLadder(d, webs) {
			return false
		}
	}
	return false
}

// StepTo walks a short straight line with a plain walk packet (no planner), for the few tiles
// inside the ruined house where a planned route would re-slash a web behind the account.
func StepTo(d *Deps, dest tile.Tile) bool {
	here := CurrentTile(d)
	if here == nil {
		return false
	}
	pts := []pathfind.NavPoint{{X: here.X, Z: here.Z, Level: here.Level}, {X: dest.X, Z: dest.Z, Level: here.Level}}
	if err := actions.Walk(d.Session, pts, true); err != nil {
		d.Logf("stage 3: step to %d,%d: %v", dest.X, dest.Z, err)
		return false
	}
	ok := WaitFor(d, fmt.Sprintf("the step to %d,%d", dest.X, dest.Z), 6*time.Second, func() bool {
		t := CurrentTile(d)
		return t != nil && t.X == dest.X && t.Z == dest.Z
	})
	if !ok {
		d.Logf("stage 3: step to %d,%d stopped at %s; game chat %q", dest.X, dest.Z, HereIs(d), ChatSince(d, time.Now().Add(-6*time.Second)))
	}
	return ok
}

// cutWebsToLadder cuts, in order, every web still between the account and the ladder — the
// ones west of it. Webs behind it are left alone: walking back through one to "cut" it again
// is exactly the loop the first live run fell into.
func cutWebsToLadder(d *Deps, webs *WebsCut) bool {
	for _, w := range fleetplan.MageBankWebs {
		here := CurrentTile(d)
		if here != nil && here.X <= w.X {
			continue // already past it
		}
		if !cutWeb(d, w, webs) {
			return false
		}
	}
	return true
}

// cutWebsFromLadder is cutWebsToLadder for the way out: inside the ruined house, cut and step
// through every web still east of the account, inner first, with the same straight steps.
//
// Live-found 2026-09-20 (cmd/scarabstage3test -phases bot356, trip 8): the way out used to leave
// these two webs to the webwalk's own Slash hop. Straight after the ladder climb that hop failed
// three times, the walker then refused the web for the rest of the session ("refused 3 crossings,
// routing around it from now on"), and the house has no other exit — the account stood at
// 3091,3956 with no route anywhere. The way in never had this failure because it cuts by hand.
func cutWebsFromLadder(d *Deps, webs *WebsCut) bool {
	for i := len(fleetplan.MageBankWebs) - 1; i >= 0; i-- {
		w := fleetplan.MageBankWebs[i]
		here := CurrentTile(d)
		// The house: west of the web and within a couple of tiles of its row (the up-ladder lands
		// at 3091,3956, one row off).
		if here == nil || here.Level != 0 || here.X >= w.X || w.X-here.X > 6 || here.Z < w.Z-2 || here.Z > w.Z+2 {
			continue // already past it, or not in the house
		}
		if !cutWebFrom(d, w, nil, webs) {
			return false
		}
		if !StepTo(d, tile.Tile{X: w.X + 1, Z: w.Z}) {
			return false
		}
	}
	return true
}

// cutWeb cuts the web at w if it is standing: the Slash op with a wielded sword, or a Knife used
// on it. Each attempt is web.rs2's coin flip; the chat line says which way it went.
func cutWeb(d *Deps, w tile.Tile, tally *WebsCut) bool {
	return cutWebFrom(d, w, nil, tally)
}

// crossWeb cuts the web at w standing on `from`, then steps through to `to` — the Keep web, which
// the walker's own hop was measured to give up on. A web already open is simply walked through.
func crossWeb(d *Deps, w, from, to tile.Tile, tally *WebsCut) bool {
	if here := CurrentTile(d); here == nil || *here != from {
		if !WalkTo(d, from, 0, WildWalkTimeout) {
			d.Logf("stage 3: could not reach %d,%d beside the web at %d,%d (at %s)", from.X, from.Z, w.X, w.Z, HereIs(d))
			return false
		}
	}
	if !cutWebFrom(d, w, &from, tally) {
		return false
	}
	return StepTo(d, to)
}

func cutWebFrom(d *Deps, w tile.Tile, from *tile.Tile, tally *WebsCut) bool {
	for attempt := 0; attempt < 12; attempt++ {
		web, standing := FindLoc(d, fleetplan.WebName, fleetplan.WebOp, w, 0)
		if !standing {
			if attempt == 0 {
				d.Logf("stage 3: web at %d,%d is already open (%s)", w.X, w.Z, describeLocsAt(d, w))
			}
			return true
		}
		// Stand beside the web on the account's own side first (east, walking in): measured live
		// (2026-09-19), a slash from two tiles off answered nothing, and a walk planned to the far
		// side makes the walker slash its own way back through a web already behind us.
		side := tile.Tile{X: w.X + 1, Z: w.Z}
		if here := CurrentTile(d); here != nil && here.X < w.X {
			side = tile.Tile{X: w.X - 1, Z: w.Z}
		}
		if from != nil {
			side = *from
		}
		if here := CurrentTile(d); here == nil || *here != side {
			if here != nil && tile.Chebyshev(*here, side) <= 3 {
				// Inside the house: a straight step, because a planned route would slash its own
				// way back through a web the account has already cut.
				StepTo(d, side)
			} else {
				WalkTo(d, side, 0, 20*time.Second)
			}
		}
		at := time.Now()
		var err error
		if knife, ok := PackItemNamed(d, fleetplan.KnifeItem); ok && !SwordWielded(d) {
			err = actions.NewEquipment(d.Session, d.Components).UseOnLoc(knife.ID, web.X, web.Z, web.ID)
		} else {
			err = entityquery.Interact(d.Session, web, fleetplan.WebOp)
		}
		if err != nil {
			d.Logf("stage 3: slashing the web at %d,%d: %v", w.X, w.Z, err)
			return false
		}
		var said string
		answered := WaitFor(d, "the web's answer", 6*time.Second, func() bool {
			if d.Chat != nil {
				said = d.Chat.Since(at)
			}
			_, still := FindLoc(d, fleetplan.WebName, fleetplan.WebOp, w, 0)
			return !still || strings.Contains(said, fleetplan.WebFailLine) || strings.Contains(said, fleetplan.WebNoBlade)
		})
		if !answered {
			d.Logf("stage 3: slash at %d,%d from %s — no answer; game chat %q", w.X, w.Z, HereIs(d), ChatSince(d, at))
		}
		if strings.Contains(said, fleetplan.WebNoBlade) {
			d.Logf("stage 3: the web refused the blade (%q) — the kit is wrong", fleetplan.WebNoBlade)
			return false
		}
		if _, still := FindLoc(d, fleetplan.WebName, fleetplan.WebOp, w, 0); !still {
			if tally != nil {
				tally.Cut++
			}
			d.Logf("stage 3: cut the web at %d,%d from %s (chat %q)", w.X, w.Z, HereIs(d), ChatSince(d, at))
			return true
		}
		if tally != nil {
			tally.Failed++
		}
	}
	d.Logf("stage 3: gave up on the web at %d,%d after 12 slashes", w.X, w.Z)
	return false
}

// describeLocsAt lists every loc the account currently sees on a tile, for a log line.
func describeLocsAt(d *Deps, at tile.Tile) string {
	var parts []string
	for _, e := range entityquery.LocEntities(d.Session.State(), d.Config, d.Locs, 30) {
		if e.X == at.X && e.Z == at.Z {
			parts = append(parts, fmt.Sprintf("%s id=%d live=%v", e.Name, e.ID, e.Live))
		}
	}
	if len(parts) == 0 {
		return "nothing there"
	}
	return strings.Join(parts, ", ")
}

func PackItemNamed(d *Deps, name string) (gamestate.SlotItem, bool) {
	for _, it := range PackSlotItems(d) {
		if strings.EqualFold(ObjName(d, it.ID), name) {
			return it, true
		}
	}
	return gamestate.SlotItem{}, false
}

func SwordWielded(d *Deps) bool {
	for _, it := range WornItems(d) {
		if fleetplan.IsWildernessBlade(it.Name) && !strings.EqualFold(it.Name, fleetplan.KnifeItem) {
			return true
		}
	}
	return false
}

// ClimbOutOfMageBank walks to the tile the down-ladder lands on (beside the up-ladder) and climbs
// back to the ruined house. Measured live (2026-09-19): a Climb-up sent from Gundai's counter did
// nothing, the same "let the engine route to the ladder" failure the way in had.
func ClimbOutOfMageBank(d *Deps) bool {
	beside := fleetplan.MageBankUpApproach
	for attempt := 0; attempt < 3; attempt++ {
		if here := CurrentTile(d); here == nil || *here != beside {
			WalkTo(d, beside, 0, 30*time.Second)
		}
		ladder, ok := FindLoc(d, fleetplan.LadderName, fleetplan.LadderUpOp, fleetplan.MageBankLadderUp, 3)
		if !ok {
			d.Logf("stage 3: no Ladder with Climb-up in the Mage Bank (%s)", describeLocsAt(d, fleetplan.MageBankLadderUp))
			return false
		}
		at := time.Now()
		if err := entityquery.Interact(d.Session, ladder, fleetplan.LadderUpOp); err != nil {
			d.Logf("stage 3: climbing up: %v", err)
			return false
		}
		if WaitFor(d, "the ruined house", 10*time.Second, func() bool {
			t := CurrentTile(d)
			return t != nil && fleetplan.WildernessLevel(*t) > 0
		}) {
			return true
		}
		d.Logf("stage 3: the ladder did not take us up from %s; game chat %q", HereIs(d), ChatSince(d, at))
	}
	return false
}

// ShopVisit is what one Lundail visit saw and bought, measured from the shop's own panels.
type ShopVisit struct {
	// Seen is the stock by rune name when the shop opened, before this account bought.
	Seen map[string]int
	// Bought and Spent are pack deltas per rune.
	Bought map[string]int
	Spent  int
	Reason string
}

// BuyFromLundailPlanned is BuyFromLundail with the purchase decided AFTER the shop opens, from the
// stock actually on her shelf and the coins actually in the pack — the only moment the shared
// stock is known rather than estimated. planFor is scarabplan's decision (PlanPurchase over the
// seen stock), handed in.
func BuyFromLundailPlanned(d *Deps, planFor func(seen map[string]int, coins int) fleetplan.PurchasePlan) ShopVisit {
	v := ShopVisit{Seen: map[string]int{}, Bought: map[string]int{}}
	sh := actions.NewShop(d.Session)
	if !sh.IsOpen() {
		err := entityquery.OpenShop(d.Session, d.Config, fleetplan.Lundail.Keeper, thieverrunner.Waiter(Thiev(d)))
		if errors.Is(err, entityquery.ErrKeeperOutOfReach) {
			// OpenShop only trades from a tile touching the keeper, and the engine's own approach
			// walk stops short in the Mage Bank (measured 2026-09-19: "Lundail is 3 tiles away").
			// Walk beside her ourselves and ask once more.
			if lundail, ok := entityquery.Npcs(d.Session.State(), d.Config).Name(fleetplan.Lundail.Keeper).Nearest(); ok {
				WalkTo(d, tile.Tile{X: lundail.X, Z: lundail.Z, Level: lundail.Level}, 1, 20*time.Second)
			}
			err = entityquery.OpenShop(d.Session, d.Config, fleetplan.Lundail.Keeper, thieverrunner.Waiter(Thiev(d)))
		}
		if err != nil {
			v.Reason = fmt.Sprintf("opening Lundail's shop: %v", err)
			return v
		}
	}
	if !WaitFor(d, "Lundail's stock", 5*time.Second, func() bool { return len(sh.Stock()) > 0 }) {
		v.Reason = "the shop opened with no stock panel"
		return v
	}
	idOf := map[string]int32{}
	for _, row := range sh.Stock() {
		n := ObjName(d, row.ID)
		if fleetplan.IsRuneName(n) {
			v.Seen[n] = int(row.Count)
			idOf[n] = row.ID
		}
	}
	coinsBefore := countItem(sh.Pack(), CoinsID)
	plan := planFor(v.Seen, int(coinsBefore))
	for _, line := range plan.Lines {
		id, ok := idOf[line.Rune]
		if !ok || v.Seen[line.Rune] <= 0 {
			continue
		}
		held := countItem(sh.Pack(), id)
		if _, err := sh.BuyCount(id, int32(line.Count), thieverrunner.Waiter(Thiev(d))); err != nil {
			d.Logf("stage 3: buying %d %s: %v", line.Count, line.Rune, err)
		}
		d.Logf("%s (%s)", sh.LastPass(), line.Rune)
		if got := int(countItem(sh.Pack(), id) - held); got > 0 {
			v.Bought[line.Rune] += got
		}
	}
	v.Spent = int(coinsBefore - countItem(sh.Pack(), CoinsID))
	_ = sh.Close()
	d.Delay(600 * time.Millisecond)
	return v
}

func countItem(items []gamestate.SlotItem, id int32) int32 {
	var n int32
	for _, it := range items {
		if it.ID == id {
			n += it.Count
		}
	}
	return n
}

// BankedAtGundai is what the Mage Bank's bank held after the deposit.
type BankedAtGundai struct {
	Runes  fleetplan.RuneStock
	Coins  int
	OK     bool
	Reason string
}

// openGundaiBank opens the Mage Bank's bank: Gundai has no Bank op, only Talk-to, and the bank
// is his dialogue's first option.
func openGundaiBank(d *Deps) (*actions.Bank, string) {
	bank := actions.NewBank(d.Session, d.Components)
	if bank.IsOpen() {
		return bank, ""
	}
	// Up to three Talk-tos. Live-found 2026-09-19 (cmd/scarabstage3test -phases e2e): one Talk-to
	// walked the account to Gundai and then nothing came back — no dialogue, no chat line — and
	// with a single attempt the whole Mage Bank trip bought nothing. gundai.rs2 has no branch on
	// account state, so a lost op is the only reading; the lever gets the same three tries.
	for attempt := 1; attempt <= gundaiTalkAttempts; attempt++ {
		gundaiTalkPrelude(d)
		gundai, ok := entityquery.Npcs(d.Session.State(), d.Config).Name(fleetplan.GundaiName).Action("Talk-to").Nearest()
		if !ok {
			return nil, "Gundai is not in view"
		}
		if err := entityquery.Interact(d.Session, gundai, "Talk-to"); err != nil {
			return nil, fmt.Sprintf("talking to Gundai: %v", err)
		}
		if DriveDialogue(d, "Gundai", []string{fleetplan.GundaiBankOption}, bank.IsOpen, 25*time.Second, 8*time.Second) {
			d.Logf("Gundai: the bank opened on attempt %d of %d", attempt, gundaiTalkAttempts)
			d.Delay(600 * time.Millisecond)
			return bank, ""
		}
		d.Logf("Gundai: attempt %d of %d did not open the bank", attempt, gundaiTalkAttempts)
	}
	return nil, "Gundai's dialogue never opened the bank"
}

// gundaiTalkAttempts is how many Talk-tos openGundaiBank sends before the Mage Bank trip gives up.
// Five, not three: a give-up wastes a whole Wilderness round trip.
const gundaiTalkAttempts = 5

// gundaiTalkPrelude clears anything the engine would swallow a Talk-to behind, before each one:
// actions.ClearInteractionBlock for a modal the client still shows, then a CLOSE_MODAL sent
// whatever the client shows, and two ticks for the engine to act on it (actions/interaction.go:
// CloseModalHandler only requests the close, processed next tick, and the client's own close is
// optimistic, so an empty modal read here proves nothing).
//
// LIVE 2026-09-26 (runebatch4/5): Gundai's Talk-to opened nothing in about half of all attempts
// (150 "no dialogue appeared within 8s" against 139 successes, not correlated with other accounts
// in the bank) and 40 Mage Bank trips gave up after three; in the lab AIO run (scarabe2e sbe2) one
// account failed 34 times in a row standing next to Lundail after buying, while an isolated probe
// opened the dialogue 9 of 9 times. The cause is not proven; a modal the engine still holds is the
// reading that fits "the op did nothing, no chat line". The "opened on attempt N" line measures it.
func gundaiTalkPrelude(d *Deps) {
	w := thieverrunner.Waiter(Thiev(d))
	_ = actions.ClearInteractionBlock(d.Session, w)
	_ = actions.CloseModal(d.Session)
	w.DelayTicks(2)
}

// WithdrawAtGundai draws exactly coins from the shared bank inside the Mage Bank — the only place
// the purchase coins ever leave the bank — and reports how many are now in the pack.
func WithdrawAtGundai(d *Deps, coins int) (int, string) {
	if coins <= 0 {
		return CoinsHeld(d), ""
	}
	bank, why := openGundaiBank(d)
	if bank == nil {
		return CoinsHeld(d), why
	}
	reason := ""
	if err := bank.QuickWithdrawX(CoinsID, int32(coins), thieverrunner.Waiter(Thiev(d))); err != nil {
		reason = fmt.Sprintf("withdrawing %d coins: %v", coins, err)
	}
	WaitFor(d, "the purchase coins in the pack", 4*time.Second, func() bool { return CoinsHeld(d) >= coins })
	_ = bank.Close()
	d.Delay(600 * time.Millisecond)
	return CoinsHeld(d), reason
}

// DepositAtGundai opens the bank through Gundai's dialogue and deposits everything but the blade
// — runes AND leftover coins, so the walk out risks the blade alone — then reads the bank.
func DepositAtGundai(d *Deps) BankedAtGundai {
	var out BankedAtGundai
	// Two passes at most: a bank cannot take what is WORN, so anything worn that is not the blade
	// comes off first, and a second pass banks what the first one could not take off for want of a
	// free slot. Before this an account that reached the Mage Bank with a staff and armour on (one
	// that was dropped in the Wilderness and walked in, not one that came through PrepareKit) looped
	// "the pack still holds more than the blade" every six seconds for twelve hours, because
	// OnlyBladeLeft counts what is worn and nothing in this function ever took it off.
	for pass := 0; pass < 2; pass++ {
		stripWornToBlade(d)
		bank, why := openGundaiBank(d)
		if bank == nil {
			out.Reason = why
			return out
		}
		keep := []string{}
		for _, it := range bank.Inventory() {
			if n := ObjName(d, it.ID); fleetplan.IsWildernessBlade(n) {
				keep = append(keep, n)
			}
		}
		thieverrunner.DepositAllExcept(Thiev(d), bank, keep)
		items := bank.Items()
		out.Runes = RuneCounts(d, items)
		out.Coins = 0
		for _, it := range items {
			if it.ID == CoinsID {
				out.Coins += int(it.Count)
			}
		}
		_ = bank.Close()
		d.Delay(600 * time.Millisecond)
		out.OK = OnlyBladeLeft(d)
		if out.OK {
			return out
		}
	}
	// Say WHAT is left: the old line did not, so for twelve hours nobody could tell a worn staff from a
	// full bank.
	k := CurrentKit(d)
	d.Logf("stage 3 deposit: after two passes still worn %v, in the pack %v", kitLabels(k.Worn), kitLabels(k.Pack))
	out.Reason = "the pack still holds more than the blade"
	return out
}

// kitLabels renders kit items as "Name xN" for a log line.
func kitLabels(items []fleetplan.KitItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, fmt.Sprintf("%s x%d", it.Name, it.Count))
	}
	return out
}

// stripWornToBlade takes off everything worn that is not a sword, the way PrepareKit does before it
// opens the bank, so the next deposit can bank it. Best effort: an unequip needs a free pack slot.
func stripWornToBlade(d *Deps) {
	eq := actions.NewEquipment(d.Session, d.Components)
	StripWornExcept(d, eq, "stage 3 deposit", isSwordName)
}

// isSwordName reports whether a worn piece is the wilderness sword the kit keeps (the knife is not one).
func isSwordName(name string) bool {
	return fleetplan.IsWildernessBlade(name) && !strings.EqualFold(name, fleetplan.KnifeItem)
}

// StripWornExcept takes every worn piece off except those keep names, in ONE burst (twopertick-1012): five Remove
// clicks to a game tick and one confirm on the worn panel, where each piece was a click and a wait of up to three
// seconds. A piece a full pack refuses stays on (the batch log says how many).
func StripWornExcept(d *Deps, eq *actions.Equipment, label string, keep func(name string) bool) actions.EquipBatch {
	var ids []int32
	for _, it := range eq.Worn() {
		if it.ID > 0 && !keep(ObjName(d, it.ID)) {
			ids = append(ids, it.ID)
		}
	}
	if len(ids) == 0 {
		return actions.EquipBatch{}
	}
	r := eq.UnequipMany(ids, thieverrunner.Waiter(Thiev(d)))
	d.Logf("%s: %s", label, r.Log("took off"))
	return r
}

// EatAnyFood eats one bite of a non-bakery food (a mule shark, a lobster) through thieverrunner's
// own eater. Bakery food goes through EatBakeryFood instead, which confirms by the food leaving
// the pack rather than by HP rising (see its doc comment for the starvation bug that avoids).
func EatAnyFood(d *Deps, food string) bool {
	return thieverrunner.EatFood(Thiev(d), food)
}

// PickpocketRung walks to a rung when out of its leash, then pickpockets once. With none of the rung's NPCs in
// sight it walks to where they spawn (seekRung, seek.go) and reports Reason "walked" when it did, so a caller that
// waits out "no-target" does not wait on top of the walk; with nowhere to go it reports "no-target" as before.
func PickpocketRung(d *Deps, m thieverplan.Method) thieverrunner.PickResult {
	// A steal sent from a silent or catching-up view is sent blind: on 18 Oct 04:41 one that went out in a 7 s silence raised the
	// Watchman that killed the thief. Nothing is stolen until the view is current (viewlag.go).
	if ViewLagging(d) {
		d.Delay(time.Second)
		return thieverrunner.PickResult{Reason: "lag"}
	}
	if t := CurrentTile(d); t == nil || tile.Chebyshev(tile.Tile{X: t.X, Z: t.Z}, m.Anchor) > m.Leash || t.Level != m.Anchor.Level {
		if !MoveToMethod(d, m, 0) {
			return thieverrunner.PickResult{Reason: "walked"}
		}
	}
	res := thieverrunner.Pickpocket(Thiev(d), m)
	if res.Reason == "no-target" && seekRung(d, m) {
		res.Reason = "walked"
	}
	return res
}
