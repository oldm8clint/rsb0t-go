package questmods

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/crafterrunner"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// FremennikTrials is The Fremennik Trials ready for the engine: questplan/fremennik.go decides,
// these execute one trial each. NOT lab-proven: every step is built from content 289's
// quest_viking scripts and the lane Q study, not from a run.
func FremennikTrials() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.Fremennik,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomFremManni:    fremManni,
			questplan.CustomFremOlaf:     fremOlaf,
			questplan.CustomFremSigli:    fremSigli,
			questplan.CustomFremSwensen:  fremSwensen,
			questplan.CustomFremSigmund:  fremSigmund,
			questplan.CustomFremPeer:     fremPeer,
			questplan.CustomFremThorvald: fremThorvald,
			questplan.CustomFremLoft:     fremLoft,
		},
	}
}

// sawSince reports whether a game message since mark contains needle (case-insensitive).
func sawSince(d *questrunner.Deps, mark time.Time, needle string) bool {
	if d.Chat == nil {
		return false
	}
	return strings.Contains(strings.ToLower(d.Chat.Since(mark)), strings.ToLower(needle))
}

func talk(d *questrunner.Deps, stop questplan.NpcStop) bool {
	return questrunner.ExecuteStep(d, questplan.Step{Kind: questplan.StepTalk, Stop: stop}, nil)
}

// talkForDeps talks to stop and keeps answering until cond holds.
func talkForDeps(d *questrunner.Deps, stop questplan.NpcStop, cond func() bool) bool {
	if cond() {
		return true
	}
	talk(d, stop)
	dl := questrunner.NewDialogue(d)
	dl.DriveUntil(cond, stop.Prefer, 20*time.Second)
	return waitUntil(d, 3*time.Second, cond)
}

func useOnNpc(d *questrunner.Deps, item, npc int, anchor tile.Tile) bool {
	return questrunner.ExecuteStep(d, questplan.Step{Kind: questplan.StepUseOn, ItemID: item, Item: questrunner.ObjName(d, item),
		TargetKind: questplan.TargetNpc, TargetID: npc, Target: "", Anchor: anchor}, nil)
}

func hint(e *questrunner.Engine, key string) bool {
	if e.Store == nil {
		return false
	}
	return e.Store.Record(questplan.FremennikID).Hints[key]
}

func setHint(e *questrunner.Engine, key string, on bool) {
	if e.Store == nil {
		return
	}
	r := e.Store.Record(questplan.FremennikID)
	if r.Hints == nil {
		r.Hints = map[string]bool{}
	}
	r.Hints[key] = on
	if e.Save != nil {
		e.Save()
	}
}

// ---------------------------------------------------------------------------
// Manni: the drinking contest, won with a keg swapped for low-alcohol beer while a firecracker
// in the longhall's drain pipe distracts him.
// ---------------------------------------------------------------------------

// kegTableWait is how long Manni's trial keeps trying the longhall's table when a pickup finds no keg of beer on it:
// another account has just taken it, and it is back two ticks later (viking.obj [viking_beerkeg] respawnrate=2), so a
// few seconds is the whole of the wait. A table that stays empty longer is not a respawn.
const kegTableWait = 8 * time.Second

// bankHolding says how many of an object the engine's last bank read saw, in the log's words.
func bankHolding(e *questrunner.Engine, id int) string {
	pv := e.Provisioner()
	if pv == nil || pv.Bank == nil || !pv.Bank.Known {
		return "the bank has not been read"
	}
	return fmt.Sprintf("the last bank read, %s ago, holds %d", time.Since(pv.Bank.At).Round(time.Second), pv.Bank.Counts[id])
}

// fremTakeKeg takes the keg of beer off the longhall's table and reports whether the pack has it, saying why when not.
//
// The table refuses a keg to an account that owns one ANYWHERE: ~obj_gettotal counts the pack, the bank and the worn
// items. That refusal is the answer to look for (questplan.ManniKegRefusal), not an empty table: the account's bank
// holds the keg, and the next step of the trial (questplan.fremManniBank) takes it out. Waiting for a respawn that is
// not coming turned each such step into twelve seconds of retries and the quest into a park (live 13-14 Oct 2026, one
// account, 27 lines).
func fremTakeKeg(e *questrunner.Engine, d *questrunner.Deps) bool {
	held := func() bool { return questrunner.HeldID(d, questplan.ObjBeerKeg) > 0 }
	mark := time.Now()
	for end := mark.Add(kegTableWait); !held() && time.Now().Before(end); {
		if questrunner.TakeGround(d, questplan.ObjBeerKeg, questplan.LonghallKegSpot, 1) {
			break
		}
		if sawSince(d, mark, questplan.ManniKegRefusal) {
			logf(d, "the longhall will not give a keg of beer to an account that owns one already; %s", bankHolding(e, questplan.ObjBeerKeg))
			return false
		}
		d.Delay(time.Second)
	}
	if !held() {
		logf(d, "no keg of beer on the table after %s", kegTableWait)
		return false
	}
	return true
}

func fremManni(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	held := func(id int) bool { return questrunner.HeldID(d, id) > 0 }
	if !hint(e, "keg-swapped") {
		if !hint(e, "firecracker-placed") && !held(questplan.ObjFirecracker) && !held(questplan.ObjFirecrackerLt) {
			if !held(questplan.ObjBeer) {
				logf(d, "no beer for the council workman")
				return false
			}
			useOnNpc(d, questplan.ObjBeer, questplan.NpcWorkman, questplan.WorkmanAnchor)
			if !waitUntil(d, 6*time.Second, func() bool { return held(questplan.ObjFirecracker) }) {
				logf(d, "the workman gave no firecracker")
				return false
			}
			questrunner.NewDialogue(d).DriveUntil(func() bool { return false }, []string{"Yes"}, 6*time.Second)
		}
		if !held(questplan.ObjLowAlcKeg) {
			stop := questplan.NpcStop{Npc: "Poison Salesman", ID: questplan.NpcPoisonMan, Anchor: questplan.PoisonManAnchor, Leash: 12,
				Prefer: []string{"Talk about the Fremennik Trials", "Yes"}}
			if !talkForDeps(d, stop, func() bool { return held(questplan.ObjLowAlcKeg) }) {
				logf(d, "the poison salesman sold no low-alcohol keg")
				return false
			}
		}
		if !hint(e, "firecracker-placed") {
			if !questrunner.Travel(d, questplan.LonghallPipe, 3) {
				return false
			}
			if held(questplan.ObjFirecracker) {
				if err := questrunner.UseItemOnItem(d, questplan.ObjTinderbox, questplan.ObjFirecracker); err != nil {
					logf(d, "tinderbox on the firecracker: %v", err)
					return false
				}
				waitUntil(d, 4*time.Second, func() bool { return held(questplan.ObjFirecrackerLt) })
			}
			if !held(questplan.ObjFirecrackerLt) {
				logf(d, "no lit firecracker")
				return false
			}
			// It explodes in the pack 140-200 ticks after lighting: straight into the pipe.
			if !useOnLocAt(d, questplan.ObjFirecrackerLt, questplan.LocLonghallPipe, questplan.LonghallPipe) {
				// The pipe is a wall decoration the engine reaches from its own tile only (locreach.WallDecorStands).
				here := questrunner.CurrentTile(d)
				logf(d, "the pipe took no firecracker (standing at %d,%d, the pipe's own tile is %d,%d)", here.X, here.Z, questplan.LonghallPipe.X, questplan.LonghallPipe.Z)
				return false
			}
			logf(d, "the lit firecracker is in the longhall's drain pipe")
			setHint(e, "firecracker-placed", true)
		}
		if !held(questplan.ObjBeerKeg) {
			if !questrunner.Travel(d, questplan.LonghallInside, 3) {
				return false
			}
			if !fremTakeKeg(e, d) {
				return false
			}
		}
		mark := time.Now()
		if err := questrunner.UseItemOnItem(d, questplan.ObjLowAlcKeg, questplan.ObjBeerKeg); err != nil {
			logf(d, "low-alcohol keg on the keg: %v", err)
			return false
		}
		if !waitUntil(d, 5*time.Second, func() bool { return !held(questplan.ObjLowAlcKeg) || sawSince(d, mark, "refill it with low alcohol") }) {
			questrunner.ClearLeftoverModal(d)
			logf(d, "the keg swap was refused")
			setHint(e, "firecracker-placed", false)
			return false
		}
		setHint(e, "keg-swapped", true)
	}
	if !held(questplan.ObjBeerKeg) {
		// A swapped keg is the one in the pack; without it the swap has to be redone.
		setHint(e, "keg-swapped", false)
		return false
	}
	talk(d, questplan.FremStop(questplan.NpcManni, "Ask about becoming a Fremennik", "Yes"))
	won := waitUntil(d, 15*time.Second, func() bool { return !held(questplan.ObjBeerKeg) })
	setHint(e, "keg-swapped", false)
	return won
}

// ---------------------------------------------------------------------------
// Olaf: a lyre from the swaying tree, golden wool from Lalli's stew, Fossegrimen's blessing for
// a raw shark, and a song on the longhall stage.
// ---------------------------------------------------------------------------

func fremOlaf(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	held := func(id int) bool { return questrunner.HeldID(d, id) > 0 }
	for i := 0; i < 12; i++ {
		switch {
		case held(questplan.ObjEnchantedLyre):
			return olafPerform(d)
		case held(questplan.ObjStrungLyre):
			if !held(questplan.ObjRawShark) {
				logf(d, "no raw shark for Fossegrimen")
				return false
			}
			if !useOnLocAt(d, questplan.ObjRawShark, questplan.LocAltarFosse, questplan.FosseAltar) &&
				!waitUntil(d, 3*time.Second, func() bool { return held(questplan.ObjEnchantedLyre) }) {
				if !questrunner.Travel(d, questplan.FosseAltar, 2) || !useOnLocAt(d, questplan.ObjRawShark, questplan.LocAltarFosse, questplan.FosseAltar) {
					return false
				}
			}
		case held(questplan.ObjUnstrungLyre) && held(questplan.ObjGoldenWool):
			_ = questrunner.UseItemOnItem(d, questplan.ObjGoldenWool, questplan.ObjUnstrungLyre)
			if !waitUntil(d, 4*time.Second, func() bool { return held(questplan.ObjStrungLyre) }) {
				logf(d, "the golden wool did not string the lyre (Fletching 25)")
				return false
			}
		case held(questplan.ObjGoldenFleece):
			if !spinFleece(d) {
				return false
			}
		case held(questplan.ObjLyreBranch):
			_ = questrunner.UseItemOnItem(d, questplan.ObjKnife, questplan.ObjLyreBranch)
			if !waitUntil(d, 4*time.Second, func() bool { return held(questplan.ObjUnstrungLyre) }) {
				logf(d, "the knife made no lyre (Crafting 40)")
				return false
			}
		case !held(questplan.ObjUnstrungLyre):
			if !questrunner.Travel(d, questplan.SwayingTree, 2) {
				return false
			}
			m, ok := questrunner.FindLoc(d, questplan.LocSwayingTree, "", "", questplan.SwayingTree, 6)
			if !ok || questrunner.OperateLoc(d, m, "Cut-branch") != nil {
				logf(d, "no swaying tree to cut")
				return false
			}
			if !waitUntil(d, 10*time.Second, func() bool { return held(questplan.ObjLyreBranch) }) {
				logf(d, "the swaying tree gave no branch (Woodcutting 40 and an axe)")
				return false
			}
		default:
			// An unstrung lyre and no wool: the stew, then Lalli's fleece.
			if !olafFleece(d) {
				return false
			}
		}
	}
	return false
}

// spinFleece spins the golden fleece on the Seers wheel (Rellekka's is Fremennik-only).
func spinFleece(d *questrunner.Deps) bool {
	cd := crafterDeps(d)
	if !crafterrunner.GoToWheel(cd) {
		return false
	}
	m, ok := questrunner.FindLoc(d, 0, "Spinning wheel", "", questrunner.CurrentTile(d), 6)
	if !ok {
		logf(d, "no spinning wheel")
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjGoldenFleece, m); err != nil {
		logf(d, "fleece on the wheel: %v", err)
		return false
	}
	return waitUntil(d, 6*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjGoldenWool) > 0 })
}

// olafFleece makes Lalli's stew (pet rock, cabbage, potato, onion) and takes the fleece.
func olafFleece(d *questrunner.Deps) bool {
	held := func(id int) bool { return questrunner.HeldID(d, id) > 0 }
	for _, v := range []struct {
		id   int
		name string
	}{{questplan.ObjCabbage, "Cabbage"}, {questplan.ObjPotato, "Potato"}, {questplan.ObjOnion, "Onion"}} {
		if held(v.id) {
			continue
		}
		if !questrunner.Travel(d, questplan.RellekkaVeg, 4) {
			return false
		}
		m, ok := questrunner.FindLoc(d, 0, v.name, "Pick", questplan.RellekkaVeg, 8)
		if !ok || questrunner.OperateLoc(d, m, "Pick") != nil {
			logf(d, "no %s to pick at Rellekka", v.name)
			return false
		}
		if !waitUntil(d, 6*time.Second, func() bool { return held(v.id) }) {
			logf(d, "picked no %s", v.name)
			return false
		}
	}
	if !held(questplan.ObjPetRock) {
		// Askeladden hands the rock over only once Lalli has sent the account to him ("Other
		// human?" sets olaf 2); at olaf 1 he only talks (viking_askelapen.rs2).
		talk(d, questplan.FremStop(questplan.NpcLalli, "Other human?"))
		questrunner.ClearLeftoverModal(d)
		talk(d, questplan.FremStop(questplan.NpcAskeladden, "Ask about becoming a Fremennik"))
		if !waitUntil(d, 4*time.Second, func() bool { return held(questplan.ObjPetRock) }) {
			logf(d, "Askeladden gave no pet rock")
			return false
		}
	}
	// With the rock held, Lalli's talk sets the plan (spoken_lalli2); the stew then needs the four.
	talk(d, questplan.FremStop(questplan.NpcLalli, "Other human?"))
	questrunner.ClearLeftoverModal(d)
	for _, id := range []int{questplan.ObjPetRock, questplan.ObjCabbage, questplan.ObjPotato, questplan.ObjOnion} {
		if held(id) {
			useOnLocAt(d, id, questplan.LocLalliCauldron, questplan.LalliCauldron)
		}
	}
	talk(d, questplan.FremStop(questplan.NpcLalli, "Other human?"))
	return waitUntil(d, 4*time.Second, func() bool { return held(questplan.ObjGoldenFleece) })
}

// olafPerform plays the enchanted lyre on the longhall stage, entered by the backstage door.
func olafPerform(d *questrunner.Deps) bool {
	if !questrunner.Travel(d, questplan.BackstageDoor, 2) {
		return false
	}
	opLocAt(d, questplan.LocBackstageDoor, questplan.BackstageDoor, "Open", func(t tile.Tile) bool { return t.X < questplan.BackstageDoor.X })
	questrunner.WalkNear(d, questplan.StageTile, 1)
	mark := time.Now()
	if err := questrunner.OpHeldByName(d, questplan.ObjEnchantedLyre, "Play"); err != nil {
		logf(d, "play the lyre: %v", err)
		return false
	}
	return waitUntil(d, 30*time.Second, func() bool {
		return questrunner.HeldID(d, questplan.ObjEnchantedLyre) == 0 || sawSince(d, mark, "musical ability from the fossegrimen leave")
	})
}

// ---------------------------------------------------------------------------
// Sigli: track the Draugen with the talisman and kill it in melee (magic defence 500).
// ---------------------------------------------------------------------------

var talismanDirs = map[string][2]int{
	"north-west": {-1, 1}, "north-east": {1, 1}, "south-west": {-1, -1}, "south-east": {1, -1},
	"north": {0, 1}, "south": {0, -1}, "east": {1, 0}, "west": {-1, 0},
}

func fremSigli(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	held := func(id int) bool { return questrunner.HeldID(d, id) > 0 }
	if held(questplan.ObjTalismanFull) {
		talk(d, questplan.TrialStop(questplan.NpcSigli))
		return waitUntil(d, 5*time.Second, func() bool { return !held(questplan.ObjTalismanFull) })
	}
	if !held(questplan.ObjTalismanEmpty) {
		talk(d, questplan.TrialStop(questplan.NpcSigli))
		if !waitUntil(d, 4*time.Second, func() bool { return held(questplan.ObjTalismanEmpty) }) {
			logf(d, "Sigli gave no talisman")
			return false
		}
	}
	sus := e.Current().Module.Sustain
	for i := 0; i < 40; i++ {
		if n, ok := questrunner.FindNpcByID(d, questplan.NpcDraugen, "", 12); ok {
			questrunner.FightBack(d, n, sus)
			if waitUntil(d, 3*time.Second, func() bool { return held(questplan.ObjTalismanFull) }) {
				talk(d, questplan.TrialStop(questplan.NpcSigli))
				return waitUntil(d, 5*time.Second, func() bool { return !held(questplan.ObjTalismanFull) })
			}
			continue
		}
		mark := time.Now()
		if err := questrunner.OpHeldByName(d, questplan.ObjTalismanEmpty, "Locate"); err != nil {
			logf(d, "locate: %v", err)
			return false
		}
		d.Delay(1200 * time.Millisecond)
		text := strings.ToLower(d.Chat.Since(mark))
		if strings.Contains(text, "the draugen is here") {
			waitUntil(d, 4*time.Second, func() bool { _, ok := questrunner.FindNpcByID(d, questplan.NpcDraugen, "", 12); return ok })
			continue
		}
		var dir [2]int
		found := false
		for _, name := range []string{"north-west", "north-east", "south-west", "south-east", "north", "south", "east", "west"} {
			if strings.Contains(text, "guides you "+name) {
				dir, found = talismanDirs[name], true
				break
			}
		}
		if !found {
			logf(d, "the talisman pointed nowhere readable")
			return false
		}
		here := questrunner.CurrentTile(d)
		step := 10
		to := tile.Tile{X: here.X + dir[0]*step, Z: here.Z + dir[1]*step}
		if !questrunner.Travel(d, to, 2) {
			questrunner.WalkNear(d, tile.Tile{X: here.X + dir[0]*4, Z: here.Z + dir[1]*4}, 2)
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Swensen: the maze of portals, fixed by loc id.
// ---------------------------------------------------------------------------

func fremSwensen(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !questplan.InSwensenMaze(questrunner.CurrentTile(d)) {
		if !questrunner.Travel(d, questplan.MazeLadder, 2) {
			return false
		}
		if !opLocAt(d, questplan.LocMazeLadder, questplan.MazeLadder, "Climb-down", questplan.InSwensenMaze) {
			return false
		}
	}
	// The next portal is the nearest correct one: the chain's landings never sit next to an
	// earlier portal, so after a restart mid-maze the nearest is the one to take.
	for i := 0; i < 12; i++ {
		here := questrunner.CurrentTile(d)
		if m, ok := questrunner.FindLoc(d, questplan.LocMazeExit, "", "", questplan.MazeExit, 3); ok && tile.Chebyshev(here, questplan.MazeExit) <= 6 {
			if err := questrunner.OperateLoc(d, m, "Climb-up"); err != nil {
				logf(d, "the maze exit: %v", err)
				return false
			}
			return waitUntil(d, 8*time.Second, func() bool { return !questplan.InSwensenMaze(questrunner.CurrentTile(d)) })
		}
		best, bestD := -1, 1<<30
		for j, p := range questplan.MazePortals {
			if dd := tile.Chebyshev(here, p.At); dd < bestD {
				best, bestD = j, dd
			}
		}
		if best < 0 {
			return false
		}
		p := questplan.MazePortals[best]
		if !opLocAt(d, p.ID, p.At, "Use", func(t tile.Tile) bool { return tile.Chebyshev(t, here) > 3 }) {
			logf(d, "portal %d at %d,%d did not move the account", p.ID, p.At.X, p.At.Z)
			return false
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Sigmund: the chain of trades, data-driven from questplan.MerchantNext.
// ---------------------------------------------------------------------------

func fremSigmund(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	for i := 0; i < 30; i++ {
		snap := e.Snapshot(0)
		f := questplan.DecodeFremennik(snap.Stage)
		if f.Status[questplan.TrialSigmund] == questplan.TrialDone {
			return true
		}
		npc, give := questplan.MerchantNext(snap, f.Chain, hint(e, "asked-sailor"))
		if npc == questplan.NpcSigmund && !give {
			// Sigmund resets a chain that holds no goods at position 13: never talk to him
			// without the flower.
			return false
		}
		before := questrunner.PackCounts(d)
		talk(d, questplan.MerchantStop(npc))
		questrunner.ClearLeftoverModal(d)
		if npc == questplan.NpcSailor && !give {
			setHint(e, "asked-sailor", true)
		}
		e.InvalidateStage()
		if npc == questplan.NpcSigmund {
			return true
		}
		changed := false
		after := questrunner.PackCounts(d)
		for id, n := range after {
			if before[id] != n {
				changed = true
			}
		}
		for id, n := range before {
			if after[id] != n {
				changed = true
			}
		}
		if give && !changed {
			logf(d, "%s took nothing", questplan.MerchantStop(npc).Npc)
			return false
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Peer: the riddle door (every answer tried), an empty pack, the house puzzle, the far door.
// ---------------------------------------------------------------------------

// The combination lock (interface combolockdoor): per wheel a minus and a plus button, then
// enter. Wheels start at A.
const (
	comLockEnter = 10109
	ifComboLock  = 10051
)

var comLockMinus = [4]int{10099, 10101, 10103, 10105}
var comLockPlus = [4]int{10100, 10102, 10104, 10106}

// PeerAnswers are the six riddles' answers (viking_peer.rs2 [if_button,combolockdoor:com_57]).
var PeerAnswers = []string{"MIND", "TREE", "LIFE", "FIRE", "TIME", "WIND"}

func enterWord(d *questrunner.Deps, word string) {
	for w, r := range word {
		n := int(r - 'A')
		com, times := comLockPlus[w], n
		if n > 13 {
			com, times = comLockMinus[w], 26-n
		}
		for i := 0; i < times; i++ {
			_ = actions.IfButton(d.Session, com)
			d.Delay(250 * time.Millisecond)
		}
	}
	_ = actions.IfButton(d.Session, comLockEnter)
}

func fremPeer(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	here := questrunner.CurrentTile(d)
	if !questplan.InSeerHouse(here) {
		if !hint(e, "peer-riddle") {
			if !peerRiddle(d) {
				return false
			}
			setHint(e, "peer-riddle", true)
		}
		if len(questrunner.PackCounts(d)) > 0 || len(questrunner.WornCounts(d)) > 0 {
			// Peer banks the pack AND the worn items on a "Yes".
			talk(d, questplan.FremStop(questplan.NpcPeer, "Ask about becoming a Fremennik", "Yes", "Bank your equipment"))
			if !waitUntil(d, 5*time.Second, func() bool { return len(questrunner.PackCounts(d)) == 0 && len(questrunner.WornCounts(d)) == 0 }) {
				logf(d, "Peer's deposit left items held")
				return false
			}
		}
		if !opLocAt(d, questplan.LocSeerDoor1, questplan.SeerDoor1, "Open", questplan.InSeerHouse) {
			return false
		}
	}
	return peerPuzzle(e)
}

func peerRiddle(d *questrunner.Deps) bool {
	if !questrunner.Travel(d, questplan.PeerAnchor, 3) {
		return false
	}
	for _, word := range PeerAnswers {
		m, ok := questrunner.FindLoc(d, questplan.LocSeerDoor1, "", "", questplan.SeerDoor1, 3)
		if !ok || questrunner.OperateLoc(d, m, "Open") != nil {
			logf(d, "no Seer's door")
			return false
		}
		dl := questrunner.NewDialogue(d)
		dl.DriveUntil(func() bool { main, _, _ := d.Session.State().Modals(); return main == ifComboLock },
			[]string{"Solve the riddle"}, 10*time.Second)
		if main, _, _ := d.Session.State().Modals(); main != ifComboLock {
			logf(d, "the combination lock did not open")
			return false
		}
		mark := time.Now()
		enterWord(d, word)
		waitUntil(d, 3*time.Second, func() bool { return sawSince(d, mark, "the riddle") })
		if sawSince(d, mark, "you have solved the riddle") {
			return true
		}
	}
	return false
}

// peerPuzzle works the house: two red disks (the unicorn's eye, the bull's eye reddened with
// the herring's goop) into the mural for the vase lid; the jug and bucket measure 4 for the
// scales chest's vase; the vase filled, lidded and frozen yields the key in ice, the range
// melts it, and the key opens the far door.
func peerPuzzle(e *questrunner.Engine) bool {
	d := e.Deps()
	held := func(id int) bool { return questrunner.HeldID(d, id) > 0 }
	const (
		bucketEmpty, bucket2, bucket4, bucket5 = 3727, 3725, 3723, 3722
		jugEmpty, jug2, jug3                   = 3732, 3730, 3729
		vase, vaseWater, vaseLidWater          = 3734, 3735, 3740
		lid, keyIce, key                       = 3737, 3741, 3745
		herring, goop, redDisk, plainDisk      = 3742, 3746, 3743, 3744
	)
	up := func() bool {
		if questrunner.CurrentTile(d).Level == 2 {
			return true
		}
		return opLocAt(d, questplan.LocSeerLadderUp, tile.Tile{X: 2631, Z: 3663}, "Climb-up", func(t tile.Tile) bool { return t.Level == 2 })
	}
	down := func() bool {
		if questrunner.CurrentTile(d).Level == 0 {
			return true
		}
		return opLocAt(d, 4173, tile.Tile{X: 2631, Z: 3663, Level: 2}, "Climb-down", func(t tile.Tile) bool { return t.Level == 0 })
	}
	at := func(x, z int) tile.Tile { return tile.Tile{X: x, Z: z, Level: 2} }
	op := func(id int, t tile.Tile, verb string) {
		if m, ok := questrunner.FindLoc(d, id, "", "", t, 2); ok {
			_ = questrunner.OperateLoc(d, m, verb)
			d.Delay(1800 * time.Millisecond)
			questrunner.ClearLeftoverModal(d)
		}
	}
	use := func(item, loc int, t tile.Tile) { useOnLocAt(d, item, loc, t) }
	combine := func(a, b int) { _ = questrunner.UseItemOnItem(d, a, b); d.Delay(1200 * time.Millisecond) }

	if !up() {
		return false
	}
	if !held(lid) && !held(vaseLidWater) && !held(keyIce) && !held(key) {
		if !held(redDisk) {
			op(4181, at(2632, 3660), "Study")
		}
		if !held(plainDisk) && !held(goop) {
			op(4182, at(2634, 3660), "Study")
		}
		if !held(goop) && held(plainDisk) {
			if !held(herring) {
				op(4171, at(2634, 3665), "Search")
			}
			use(herring, 4172, at(2629, 3663))
		}
		if held(goop) && held(plainDisk) {
			combine(goop, plainDisk)
		}
		if !down() {
			return false
		}
		for i := 0; i < 2 && held(redDisk); i++ {
			use(redDisk, 4179, tile.Tile{X: 2634, Z: 3663})
		}
		if !held(lid) {
			op(4179, tile.Tile{X: 2634, Z: 3663}, "Study")
		}
		if !held(lid) {
			logf(d, "the mural gave no vase lid")
			return false
		}
		if !up() {
			return false
		}
	}
	if !held(vase) && !held(vaseWater) && !held(vaseLidWater) && !held(keyIce) && !held(key) {
		if !held(jugEmpty) && !held(jug2) && !held(jug3) {
			op(4167, at(2635, 3660), "Open")
			op(4168, at(2635, 3660), "Search")
		}
		if !held(bucketEmpty) && !held(bucket2) && !held(bucket4) && !held(bucket5) {
			op(4177, at(2629, 3660), "Open")
			op(4178, at(2629, 3660), "Search")
		}
		tap, drain := at(2629, 3661), at(2629, 3662)
		use(bucketEmpty, 4176, tap) // bucket 5
		combine(bucket5, jugEmpty)  // bucket 2, jug 3
		use(jug3, 4175, drain)      // jug empty
		combine(bucket2, jugEmpty)  // bucket empty, jug 2
		use(bucketEmpty, 4176, tap) // bucket 5
		combine(bucket5, jug2)      // bucket 4, jug 3
		if !held(bucket4) {
			logf(d, "the jug and bucket did not measure four")
			return false
		}
		use(bucket4, 4170, at(2632, 3665))
		if !held(vase) {
			logf(d, "the scales chest gave no vase")
			return false
		}
	}
	if held(vase) {
		use(vase, 4176, at(2629, 3661))
	}
	if held(vaseWater) && held(lid) {
		combine(lid, vaseWater)
	}
	if held(vaseLidWater) {
		use(vaseLidWater, 4169, at(2638, 3665))
	}
	if held(keyIce) {
		use(keyIce, 4172, at(2629, 3663))
	}
	if !held(key) {
		logf(d, "no key from the ice")
		return false
	}
	if !down() {
		return false
	}
	use(key, questplan.LocSeerDoor2, questplan.SeerDoor2)
	return waitUntil(d, 5*time.Second, func() bool { return !questplan.InSeerHouse(questrunner.CurrentTile(d)) })
}

// ---------------------------------------------------------------------------
// Thorvald: Koschei unarmed, down the arena ladder with nothing but food; the fourth form's
// "honour death" passes the trial and lifts the account to the loft.
// ---------------------------------------------------------------------------

func fremThorvald(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	sus := e.Current().Module.Sustain
	if !questplan.InThorvaldArena(questrunner.CurrentTile(d)) {
		if !thorvaldStrip(e) {
			return false
		}
		if !questrunner.Travel(d, questplan.WarriorLadder, 2) {
			return false
		}
		if !opLocAt(d, questplan.LocWarriorLadder, questplan.WarriorLadder, "Climb-down", questplan.InThorvaldArena) {
			logf(d, "Thorvald's ladder refused (something forbidden is held or worn)")
			return false
		}
	}
	deadline := time.Now().Add(15 * time.Minute)
	mark := time.Now()
	for time.Now().Before(deadline) {
		t := questrunner.CurrentTile(d)
		if questplan.InThorvaldLoft(t) {
			return fremLoft(e, questplan.Step{})
		}
		if !questplan.InThorvaldArena(t) || questrunner.DiedSince(d, mark) {
			return false
		}
		var foe entityquery.Entity
		found := false
		for _, n := range entityquery.Npcs(d.Session.State(), d.Config).Within(20).Results() {
			if n.ID >= questplan.NpcKoscheiFirst && n.ID <= questplan.NpcKoscheiFourth {
				foe, found = n, true
			}
		}
		if !found {
			d.Delay(1200 * time.Millisecond)
			continue
		}
		// Eat only against the first three forms: the fourth hits 1 a tick and the pass is its
		// hit landing on exactly 0, which eating only delays.
		if foe.ID < questplan.NpcKoscheiFourth {
			questrunner.EatIfNeeded(d, sus.FoodIDs, sus.EatAt)
		}
		// The fight loop judges the first three forms by their pace (a race lost there walks the account out and
		// counts a near-death) and leaves the fourth alone (questplan.ScriptedLoss): its pass is the account's
		// hitpoints landing on exactly 0, so no retreat is right however little of his 255 hitpoints is gone.
		questrunner.FightBack(d, foe, questplan.Sustain{})
	}
	return false
}

// thorvaldStrip banks everything but food at Seers, worn items included. The run staller's kit goes with it (a bare
// deposit): Thorvald's ladder refuses anything but food, and the ordinary deposit kept the kit in the pack.
func thorvaldStrip(e *questrunner.Engine) bool {
	return thorvaldStripWith(e.Deps(), e.Current().Module.Sustain)
}

// thorvaldStripWith is thorvaldStrip for an account d and the quest's food sus.
func thorvaldStripWith(d *questrunner.Deps, sus questplan.Sustain) bool {
	clean := len(questrunner.WornCounts(d)) == 0
	for id := range questrunner.PackCounts(d) {
		if !sus.IsFood(id) {
			clean = false
		}
	}
	if clean {
		return true
	}
	if !questrunner.Travel(d, questplan.SeersBank, 4) {
		return false
	}
	questrunner.PackVisible(d)
	questrunner.StripWorn(d)
	b, ok := questrunner.OpenBankAt(d, questplan.SeersBank)
	if !ok {
		return false
	}
	banked := questrunner.DepositBare(d, b, sus.IsFood)
	_ = b.Close()
	return banked && len(questrunner.WornCounts(d)) == 0
}

// fremLoft climbs down from the longhall loft the honour death lands on.
func fremLoft(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	return opLocAt(d, questplan.LocLoftLadder, questplan.LoftLadder, "Climb-down", func(t tile.Tile) bool { return t.Level == 0 })
}
