package randomevents

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
)

// THE LOST AXE AND PICKAXE (content/scripts/macro events/scripts/woodcutting/macro_event_lost_axe.rs2 and mining/macro_event_lost_pickaxe.rs2).
//
// a user request: "we need to ensure we never lose pickaxe heads and axe heads as it appears we never pick them up and reattach them".
//
// The event swaps the tool for a handle (macro_hatchethandle "Axe handle", macro_pickaxehandle "Pickaxe handle"; in the right hand when the tool was
// wielded, else in the pack) and throws the head to a walkable square 1 to 7 tiles away, where it lies for ^lootdrop_duration (200 ticks, two minutes).
// Using the head on the handle (reattach_axe, reattach_pickaxe: both in the PACK, either way round) deletes both and adds the tool back.
//
// WHY THEY WERE NEVER RECOVERED. The recovery existed in two scripts only: woodcutrunner.RecoverAxeHead, called from ProgressiveWoodcutter's own
// event step, and essencerunner.RecoverPickaxeHead, called from the essence miner's mine loop. Every other script that chops or mines (ScarabSwarm's logs
// and bark, WCFletch, MasterCrafter's oak round, the retaliate runner, a rune supply's mining stretch, the fleet's embedded chopping) calls the shared chop or
// mine primitives and never looks at the event, so an account holding a handle clicked its tree or rock through "You need an axe/pickaxe" until the head
// despawned, and the handle stayed in the pack for good (a stranded, idle account is the usual symptom). Recovery was only as good as the number of
// scripts that had remembered to copy it.
//
// THE ONE SHARED PLACE is here, in the Guardian: it already runs beside every script of every account, already owns the action lock and the input hold, and
// already handles the other random events whatever the script is. kick calls recoverToolHead before it looks for an event; an account that holds a handle
// and has the head in its pack or in sight on the ground gets it back within a poll or two. The scripts' own recoveries stay (they act first when the
// script is paying attention); this is what makes writing the next one unnecessary.

// The event's four item names. Every tier's head has the same name, and so does every handle (content antimacro.obj).
const (
	axeHandleName     = "Axe handle"
	axeHeadName       = "Axe head"
	pickaxeHandleName = "Pickaxe handle"
	pickaxeHeadName   = "Pickaxe head"
)

const (
	// toolHeadRadius is how far from the account a head is looked for: content throws it at most 7 tiles, plus one for a step taken since.
	toolHeadRadius = 8
	// toolHeadPackSlots is the backpack's size.
	toolHeadPackSlots = 28
	// toolHeadMaxFails is how many failed steps in a row are tried before the Guardian leaves the head alone for toolHeadBackoff.
	toolHeadMaxFails = 4
	toolHeadBackoff  = 30 * time.Second
	// toolHeadWait bounds the wait for one step's result to show in the pack.
	toolHeadWait = 5 * time.Second
)

// toolOfHandle and toolOfHead say which tool an item name is the handle or the head of: "axe" or "pickaxe", "" for neither.
func toolOfHandle(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "axe handle":
		return "axe"
	case "pickaxe handle":
		return "pickaxe"
	}
	return ""
}

func toolOfHead(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "axe head":
		return "axe"
	case "pickaxe head":
		return "pickaxe"
	}
	return ""
}

// toolHeadSituation is what the plan needs to know, read from the account.
type toolHeadSituation struct {
	HandleInPack bool
	HandleWorn   bool
	HeadInPack   bool
	// HeadOnGround is true when a head of the handle's tool lies within toolHeadRadius on the account's level.
	HeadOnGround bool
	Free         int
	// CanMakeRoom is true when the pack holds something that can be dropped for a slot.
	CanMakeRoom bool
}

// toolHeadStep is the one thing to do next.
type toolHeadStep int

const (
	stepNothing toolHeadStep = iota // no handle, or nothing to put on it: leave it be
	stepMakeRoom
	stepTake
	stepUnequip
	stepAttach
	stepNoRoom // a slot is needed, none is free and nothing can be dropped
)

// nextToolHeadStep is the plan: nothing without a handle or without a head to put on it; room first (a head to take is one slot, a worn handle to take
// off is another); then take the head, take the handle off the hand, and use the one on the other.
func nextToolHeadStep(s toolHeadSituation) toolHeadStep {
	handle := s.HandleInPack || s.HandleWorn
	if !handle || (!s.HeadInPack && !s.HeadOnGround) {
		return stepNothing
	}
	need := 0
	if !s.HeadInPack {
		need++
	}
	if s.HandleWorn && !s.HandleInPack {
		need++
	}
	if s.Free < need {
		if s.CanMakeRoom {
			return stepMakeRoom
		}
		return stepNoRoom
	}
	switch {
	case !s.HeadInPack:
		return stepTake
	case s.HandleWorn && !s.HandleInPack:
		return stepUnequip
	}
	return stepAttach
}

// toolHeadState is the Guardian's memory of its recovery: failures in a row and when to try again.
type toolHeadState struct {
	mu    sync.Mutex
	fails int
	until time.Time
	noted string // the tool of the last logged start, so one event logs once
}

// recoverToolHead puts a lost axe or pickaxe back together, one step per call, and reports whether it acted (kick then does nothing else this poll). It
// reads the account without a lock and takes the action lock only when there is something to do.
func (g *Guardian) recoverToolHead() bool {
	r := currentResolver()
	if r == nil {
		return false
	}
	g.toolHead.mu.Lock()
	wait := time.Now().Before(g.toolHead.until)
	g.toolHead.mu.Unlock()
	if wait {
		return false
	}
	eq := actions.NewEquipment(g.session, currentInterfaceArchive())
	tool, sit, _, _ := readToolHead(g.session.State(), eq, r)
	if tool == "" || nextToolHeadStep(sit) == stepNothing {
		return false
	}
	if !g.actionMu.TryLock() {
		return false
	}
	g.inFlight.Store(true)
	defer func() { g.inFlight.Store(false); g.actionMu.Unlock() }()
	endEvent := g.BeginEvent()
	defer endEvent()
	defer g.takeInput()()

	// Read again under the lock: the script may have acted since.
	tool, sit, headTile, headID := readToolHead(g.session.State(), eq, r)
	step := nextToolHeadStep(sit)
	if step == stepNothing {
		return false
	}
	g.toolHead.mu.Lock()
	first := g.toolHead.noted != tool
	g.toolHead.noted = tool
	g.toolHead.mu.Unlock()
	if first {
		g.log(fmt.Sprintf("lost %s: holding a handle with the head %s - putting it back together", tool, headWhere(sit)))
	}
	ok, reason := g.runToolHeadStep(step, tool, headTile, headID, eq, r)
	g.toolHead.mu.Lock()
	if ok {
		g.toolHead.fails = 0
	} else {
		g.toolHead.fails++
		if g.toolHead.fails >= toolHeadMaxFails || step == stepNoRoom {
			g.toolHead.fails = 0
			g.toolHead.until = time.Now().Add(toolHeadBackoff)
		}
	}
	if ok && step == stepAttach {
		g.toolHead.noted = ""
	}
	g.toolHead.mu.Unlock()
	switch {
	case !ok:
		g.log(fmt.Sprintf("lost %s: %s", tool, reason))
	case step == stepAttach:
		g.log(fmt.Sprintf("lost %s: put back together", tool))
	}
	return true
}

func headWhere(s toolHeadSituation) string {
	if s.HeadInPack {
		return "in the pack"
	}
	return "on the ground in sight"
}

// readToolHead reads the account: the tool it has lost a handle of ("" for none), the plan's situation, and the head on the ground (its tile and obj id).
func readToolHead(state *gamestate.State, eq *actions.Equipment, r NameResolver) (tool string, sit toolHeadSituation, headAt pathfind.NavPoint, headID int) {
	var inv []gamestate.SlotItem
	if pk := state.Inventory(actions.PlayerPack); pk != nil {
		inv = pk.NonEmpty()
	}
	for _, it := range inv {
		o, ok := r.Obj(int(it.ID))
		if !ok || o.Noted() {
			continue
		}
		if t := toolOfHandle(o.Name); t != "" {
			tool, sit.HandleInPack = t, true
		}
	}
	if tool == "" {
		for _, it := range eq.Worn() {
			if o, ok := r.Obj(int(it.ID)); ok {
				if t := toolOfHandle(o.Name); t != "" {
					tool, sit.HandleWorn = t, true
				}
			}
		}
	}
	if tool == "" {
		return "", sit, headAt, 0
	}
	sit.Free = toolHeadPackSlots - len(inv)
	for _, it := range inv {
		o, ok := r.Obj(int(it.ID))
		if !ok || o.Noted() {
			continue
		}
		if toolOfHead(o.Name) == tool {
			sit.HeadInPack = true
		}
		if droppableForHead(o.Name) {
			sit.CanMakeRoom = true
		}
	}
	if !sit.HeadInPack {
		if x, z, level, ok := state.Tile(); ok {
			best := toolHeadRadius + 1
			for _, g := range state.GroundItems() {
				o, ok := r.Obj(g.ID)
				if !ok || toolOfHead(o.Name) != tool || g.Level != level {
					continue
				}
				if d := chebyshev(x, z, g.X, g.Z); d <= toolHeadRadius && d < best {
					best, headAt, headID = d, pathfind.NavPoint{X: g.X, Z: g.Z, Level: g.Level}, g.ID
					sit.HeadOnGround = true
				}
			}
		}
	}
	return tool, sit, headAt, headID
}

// droppableForHead is whether an item may be dropped for the slot the head needs: anything but the event's own pieces and the tools themselves. The
// choice among them is dropVictim's.
func droppableForHead(name string) bool {
	if toolOfHandle(name) != "" || toolOfHead(name) != "" {
		return false
	}
	n := strings.ToLower(name)
	return !strings.Contains(n, "axe")
}

// dropVictim is the pack slot to drop for room: the cheapest unnoted item by cache cost. An empty name is none.
func dropVictim(inv []gamestate.SlotItem, r NameResolver) (id int32, name string) {
	best := int64(1 << 62)
	for _, it := range inv {
		o, ok := r.Obj(int(it.ID))
		if !ok || o.Noted() || !droppableForHead(o.Name) {
			continue
		}
		if v := int64(o.Cost); v < best {
			best, id, name = v, it.ID, o.Name
		}
	}
	return id, name
}

// runToolHeadStep does one step of the plan and says whether it worked.
func (g *Guardian) runToolHeadStep(step toolHeadStep, tool string, headAt pathfind.NavPoint, headID int, eq *actions.Equipment, r NameResolver) (bool, string) {
	state := g.session.State()
	pack := func() []gamestate.SlotItem {
		if inv := state.Inventory(actions.PlayerPack); inv != nil {
			return inv.NonEmpty()
		}
		return nil
	}
	find := func(match func(string) bool) (int32, bool) {
		for _, it := range pack() {
			if o, ok := r.Obj(int(it.ID)); ok && !o.Noted() && match(o.Name) {
				return it.ID, true
			}
		}
		return 0, false
	}
	isHead := func(n string) bool { return toolOfHead(n) == tool }
	isHandle := func(n string) bool { return toolOfHandle(n) == tool }
	waitFor := func(cond func() bool) bool {
		deadline := time.Now().Add(toolHeadWait)
		for time.Now().Before(deadline) {
			if cond() {
				return true
			}
			time.Sleep(100 * time.Millisecond)
		}
		return cond()
	}
	if main, _, _ := state.Modals(); main != gamestate.NoModal {
		_ = actions.CloseModal(g.session)
		time.Sleep(600 * time.Millisecond)
	}
	switch step {
	case stepNoRoom:
		return false, "the pack is full and holds nothing to drop, so the head cannot be picked up"
	case stepMakeRoom:
		id, name := dropVictim(pack(), r)
		if name == "" {
			return false, "nothing to drop for room"
		}
		before := len(pack())
		if err := eq.InteractHeld(r, id, "Drop"); err != nil {
			return false, "could not drop a " + strings.ToLower(name) + " for room: " + err.Error()
		}
		if !waitFor(func() bool { return len(pack()) < before }) {
			return false, "the drop of a " + strings.ToLower(name) + " never showed"
		}
		g.log(fmt.Sprintf("lost %s: dropped a %s to make room", tool, strings.ToLower(name)))
		return true, ""
	case stepTake:
		if x, z, level, ok := state.Tile(); ok && (x != headAt.X || z != headAt.Z || level != headAt.Level) {
			if err := walkTo(g.session, g.pack, x, z, level, headAt); err != nil {
				return false, "could not walk to the head: " + err.Error()
			}
		}
		if err := actions.OpObj(g.session, 3, headAt.X, headAt.Z, headID); err != nil {
			return false, "could not take the head: " + err.Error()
		}
		if !waitFor(func() bool { _, ok := find(isHead); return ok }) {
			return false, "the head never reached the pack"
		}
		return true, ""
	case stepUnequip:
		for _, it := range eq.Worn() {
			if o, ok := r.Obj(int(it.ID)); ok && isHandle(o.Name) {
				if err := eq.Unequip(it.ID); err != nil {
					return false, "could not take the handle off: " + err.Error()
				}
				if !waitFor(func() bool { _, ok := find(isHandle); return ok }) {
					return false, "the handle never reached the pack"
				}
				return true, ""
			}
		}
		return false, "the worn handle is gone"
	case stepAttach:
		handle, okHandle := find(isHandle)
		head, okHead := find(isHead)
		if !okHandle || !okHead {
			return false, "a piece is missing from the pack"
		}
		if err := eq.UseOnHeld(handle, head); err != nil {
			return false, "the engine refused the re-attach: " + err.Error()
		}
		if !waitFor(func() bool { _, h := find(isHandle); _, d := find(isHead); return !h && !d }) {
			return false, "the pieces did not become a " + tool
		}
		return true, ""
	}
	return false, "no step"
}
