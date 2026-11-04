package questmods

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// MurderMystery is Murder Mystery ready for the engine.
func MurderMystery() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.MurderMystery,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomMurderWindow: murderWindow,
			questplan.CustomMurderPrints: murderPrints,
			questplan.CustomMurderPoison: murderPoison,
		},
	}
}

func anyThread(d *questrunner.Deps) int {
	for _, id := range []int{questplan.ObjThreadRed, questplan.ObjThreadGreen, questplan.ObjThreadBlue} {
		if held(d, id) {
			return id
		}
	}
	return 0
}

// murderWindow investigates the smashed window: the thread bit, and a thread (again, if none
// is owned anywhere).
func murderWindow(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.SmashedWindowTile, 2) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocSmashedWindow, "", "Investigate", questplan.SmashedWindowTile, 3)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Investigate"); err != nil {
		return false
	}
	questrunner.NewDialogue(d).DriveUntil(func() bool { return anyThread(d) > 0 }, nil, 10*time.Second)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return anyThread(d) > 0
}

// operateUntil operates a loc (op) near a tile, driving the dialogue that follows, until done.
func operateUntil(d *questrunner.Deps, id int, op string, at tile.Tile, prefer []string, done func() bool) bool {
	if done() {
		return true
	}
	if !questrunner.Travel(d, at, 2) {
		return false
	}
	m, ok := questrunner.FindLoc(d, id, "", op, at, 3)
	if !ok {
		logf(d, "no loc %d (%s) near %d,%d,%d", id, op, at.X, at.Z, at.Level)
		return false
	}
	if err := questrunner.OperateLoc(d, m, op); err != nil {
		return false
	}
	ok = questrunner.NewDialogue(d).DriveUntil(done, prefer, 12*time.Second)
	questrunner.ClearLeftoverModal(d)
	return ok
}

// dust floors an item (flour from the mansion barrel into the empty pot first) and lifts its
// print with flypaper from the sacks: plain -> dusted -> print.
func dust(d *questrunner.Deps, plain, dusted, print int) bool {
	if held(d, print) {
		return true
	}
	if !held(d, dusted) {
		if !operateUntil(d, questplan.LocFlourBarrel, "Take From", questplan.FlourBarrelTile, nil,
			func() bool { return held(d, questplan.ObjPotOfFlour) }) {
			logf(d, "no pot of flour (an empty pot is needed)")
			return false
		}
		if err := questrunner.UseItemOnItem(d, questplan.ObjPotOfFlour, plain); err != nil {
			logf(d, "flour on item %d: %v", plain, err)
			return false
		}
		if !waitUntil(d, 6*time.Second, func() bool { return held(d, dusted) }) {
			return false
		}
		questrunner.ClearLeftoverModal(d)
	}
	if !operateUntil(d, questplan.LocMurderSacks, "Investigate", questplan.MurderSacksTile, []string{"Yes, it might be useful."},
		func() bool { return held(d, questplan.ObjFlypaper) }) {
		logf(d, "no flypaper from the sacks")
		return false
	}
	if err := questrunner.UseItemOnItem(d, questplan.ObjFlypaper, dusted); err != nil {
		logf(d, "flypaper on item %d: %v", dusted, err)
		return false
	}
	ok := waitUntil(d, 6*time.Second, func() bool { return held(d, print) })
	questrunner.ClearLeftoverModal(d)
	return ok
}

// murderPrints lifts the dagger's print, then each thread candidate's print from their silver,
// and compares: a match turns the unknown print into the Killers' print (the fingerprint bit);
// a mismatch destroys the suspect print.
func murderPrints(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !held(d, questplan.ObjUnknownPrint) && !held(d, questplan.ObjKillersPrint) {
		if !held(d, questplan.ObjMurderDagger) && !held(d, questplan.ObjDaggerDust) {
			if !questrunner.Travel(d, questplan.MurderDaggerTile, 1) ||
				!questrunner.TakeGround(d, questplan.ObjMurderDagger, questplan.MurderDaggerTile, 1) {
				logf(d, "the dagger is not on the study floor (banked?)")
				return false
			}
		}
		if !dust(d, questplan.ObjMurderDagger, questplan.ObjDaggerDust, questplan.ObjUnknownPrint) {
			return false
		}
	}
	for _, i := range questplan.ThreadCandidates(anyThread(d)) {
		if held(d, questplan.ObjKillersPrint) {
			break
		}
		sp := questplan.Suspects[i]
		if !held(d, sp.Print) && !held(d, sp.SilverDust) && !held(d, sp.Silver) {
			if !operateUntil(d, sp.Barrel, "Search", sp.BarrelTile, nil, func() bool { return held(d, sp.Silver) }) {
				logf(d, "%s's barrel gave no silver item", sp.Name)
				continue
			}
		}
		if !dust(d, sp.Silver, sp.SilverDust, sp.Print) {
			return false
		}
		mark := time.Now()
		if err := questrunner.UseItemOnItem(d, sp.Print, questplan.ObjUnknownPrint); err != nil {
			logf(d, "%s's print on the unknown print: %v", sp.Name, err)
			return false
		}
		questrunner.NewDialogue(d).DriveUntil(func() bool {
			return held(d, questplan.ObjKillersPrint) || !held(d, sp.Print)
		}, nil, 10*time.Second)
		questrunner.ClearLeftoverModal(d)
		said := ""
		if d.Chat != nil {
			said = strings.ToLower(d.Chat.Since(mark))
		}
		logf(d, "%s's print: %s", sp.Name, map[bool]string{true: "a match", false: "cleared"}[held(d, questplan.ObjKillersPrint)])
		_ = said
	}
	e.InvalidateStage()
	return held(d, questplan.ObjKillersPrint)
}

// murderPoison runs the poison chain in one step, since the salesman's line resets poison
// progress to 1 whenever it is asked: the salesman, the poison question to the candidates (only
// the culprit's answer moves progress on), then their locs, until the journal says proved.
func murderPoison(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	proved := func() bool {
		e.InvalidateStage()
		_, _, poison := questplan.MurderTracks(e.Snapshot(0).Stage)
		return poison
	}
	seller := questplan.NpcStop{Npc: "Poison Salesman", ID: questplan.NpcPoisonSeller, Anchor: questplan.PoisonSellerAt, Leash: 10,
		Prefer: []string{"Talk about the Murder Mystery Quest", "Who did you sell Poison to at the house?"}}
	if !e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: seller}) {
		return false
	}
	snap := e.Snapshot(0)
	candidates := questplan.ThreadCandidates(anyThread(d))
	if c := questplan.Culprit(snap); c >= 0 {
		candidates = []int{c}
	}
	for _, i := range candidates {
		sp := questplan.Suspects[i]
		stop := questplan.NpcStop{Npc: sp.Name, ID: sp.Npc, Anchor: sp.Anchor, Leash: 8, Prefer: []string{"Why'd you buy poison the other day?"}}
		e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: stop})
		questrunner.ClearLeftoverModal(d)
	}
	for _, i := range candidates {
		sp := questplan.Suspects[i]
		operateUntil(d, sp.PoisonLoc, "Investigate", sp.PoisonTile, nil, func() bool { return false })
		if proved() {
			return true
		}
	}
	return proved()
}
