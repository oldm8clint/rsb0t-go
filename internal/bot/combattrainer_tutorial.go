package bot

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/combatrunner"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
)

// CombatTrainer's Tutorial Island leg, so a genuinely fresh account can be dropped into the
// fleet and trained with no manual preparation.
//
// This is wiring, not new mechanism: freshstartrunner.AcceptAppearance and
// freshstartrunner.SkipTutorial already do the work and five other registered scripts already
// call them. Three things about it are specific to this script and worth knowing.
//
// FIRST, it has to run before everything else in Loop, and that matters more here than in the
// sibling scripts. Loop's own ContainersReadable gate returns a FATAL error once the backpack
// and worn panels have been unreadable for containersUnreadableLimit, which is right for the
// random event it was written for and exactly wrong for a fresh account: @start_tutorial's
// first act is if_settab(null, ^tab_inventory), so a tutorial account is blinded for the whole
// tutorial and the run would kill itself about five minutes in. Returning before that check
// also means no gear, food or bank decision is ever taken from a pack that reads empty because
// it cannot be seen rather than because it is empty.
//
// SECOND, AcceptAppearance is a hard precondition of the skip, not a nicety, and the ordering
// is content's rather than ours. runescape_guide.rs2's [opnpc1,newbie_basics_instructor] is a
// switch_int(%tutorial), and only case ^newbie_basics_instructor_designed_character — %tutorial
// exactly 1 — reaches the label that offers "Yes please."; 0 and everything above 1 fall to
// newbie_basics_instructor_return, which is three lines of speech with no option list at all.
// %tutorial becomes 1 only through [if_close,player_kit] -> queue(tutorial_designed_character),
// and a queue lands a tick later. So a guide click before the appearance screen has closed and
// settled cannot finish, however many times it is sent — which is what "the guide is adjacent
// and answers no Talk-to" turned out to mean. The stage machine below never clicks the guide
// while the design modal is open.
//
// THIRD, nothing here may return without delaying. Runner.run calls Bot.Loop back to back with
// no delay of its own, and SkipTutorial carries an unconditional 700ms floor precisely because
// a zero-delay return once resent the identical Talk-to click about twenty-five times in under
// a millisecond and got every fresh account forcibly disconnected by the server's own
// anti-cheat. That floor is not touched here. Every branch below adds its own delay on top, so
// re-entry from this Loop cannot be tighter than the floor rather than merely usually being
// looser.

// tutorialState is this run's progress off Tutorial Island. A named type so the field on
// combatTrainerBot costs combattrainer.go no new import.
type tutorialState struct {
	watch freshstart.TutorialWatch
	// done latches once the account is off the island with its sidebar back, so for the rest
	// of the run — and for every account in the fleet that was never on the island — the whole
	// check costs one bool per Loop call.
	done bool
	// announced keeps the "still on Tutorial Island" line to once per run, and doubles as the
	// record of whether this run actually did any tutorial: an account that was already
	// finished says nothing at all.
	announced bool
	// lastSaid throttles the per-situation progress line to one per change.
	lastSaid string
	// sawGuide and sawSkip separate the two ways this can fail: never finding the guide, and
	// finding it but never being offered the option. They name different causes, so the
	// give-up says which one happened.
	sawGuide bool
	sawSkip  bool
}

// guideSearchRadius is how far SkipTutorial itself looks for the guide, mirrored here so the
// action this step reports is the one SkipTutorial is about to take.
const guideSearchRadius = 30

// tutorialStep drives one pass of the tutorial. It returns done=true once the account is off
// the island and can see its own containers again, and an error when the tutorial cannot be
// finished. It never returns (false, nil) forever: freshstart.TutorialWatch bounds it.
func (b *combatTrainerBot) tutorialStep(ctx *Context) (bool, error) {
	now := time.Now()
	here := currentTileFor(ctx)
	mainModal, _, _ := ctx.Session.State().Modals()
	stage := freshstart.DecideTutorialStage(mainModal, here)

	// Asked before the archive check below, because "I cannot see my containers" is exactly
	// what a missing archive looks like and the two must not be confused.
	readable := ctx.Components != nil && combatrunner.ContainersReadable(b.deps(ctx))

	action := b.tutorialAction(ctx, stage, here)
	done, giveUp := b.tutorial.watch.Note(freshstart.TutorialProgress{
		Stage: stage, Action: action, Readable: readable,
	}, now)

	if done {
		b.tutorial.done = true
		if b.tutorial.announced {
			ctx.logf("combattrainer: off Tutorial Island at %s with the sidebar back, after %s — starting the run proper",
				describeTile(here), b.tutorial.watch.Elapsed(now).Round(time.Second))
			// The tutorial is real progress but it is not melee XP, and the heartbeat's stall
			// detector only knows about melee XP. Without this a run that spent four honest
			// minutes on the island opens by reporting itself STALLED.
			b.lastProgressAt, b.lastBeat = now, now
		}
		return true, nil
	}

	if !b.tutorial.announced {
		b.tutorial.announced = true
		ctx.logf("combattrainer: this account is still on Tutorial Island (%s at %s) — finishing the tutorial before training anything. Nothing reads the pack or the worn panel until it is done: the tutorial strips the sidebar tabs, and a blind read comes back empty rather than unknown",
			stage, describeTile(here))
	}

	if giveUp != "" {
		return false, b.tutorialGaveUp(ctx, giveUp, stage, action)
	}

	// Without the interface archive neither the Accept button nor the guide's option list can
	// be resolved at all, so waiting out the budget would achieve nothing. Say so immediately.
	if ctx.Components == nil {
		return false, fmt.Errorf("combattrainer: this account is on Tutorial Island (%s) and no interface archive is configured, so the appearance screen's Accept button and the RuneScape Guide's dialogue options cannot be resolved — the tutorial cannot be finished headless without one", stage)
	}

	if key := string(stage) + "/" + string(action); key != b.tutorial.lastSaid {
		b.tutorial.lastSaid = key
		ctx.logf("combattrainer: tutorial: %s — %s (%s of %s elapsed)",
			stage, action, b.tutorial.watch.Elapsed(now).Round(time.Second), freshstart.TutorialLimit)
	}

	switch stage {
	case freshstart.TutorialStageDesign:
		// AcceptAppearance delays on every one of its own paths; the floor on top is belt and
		// braces against a future edit dropping one of them.
		freshstartrunner.AcceptAppearance(b.tutorialDeps(ctx))
		ctx.Delay(400 * time.Millisecond)
	case freshstart.TutorialStageIsland:
		freshstartrunner.SkipTutorial(b.tutorialDeps(ctx), freshstart.DefaultGuideNPC, freshstart.DefaultSkipOption)
		ctx.Delay(400 * time.Millisecond)
	case freshstart.TutorialStageUnknown:
		// No position update yet. Nothing can be decided and clicking anything would be
		// guessing at where the account is standing.
		ctx.Delay(1200 * time.Millisecond)
	case freshstart.TutorialStageOff:
		// The teleport has landed but ~initalltabs has not been seen yet. Content does both in
		// its own [label,tutorial_complete], so this is a settling wait of a tick or two rather
		// than a state to act in.
		ctx.Delay(600 * time.Millisecond)
	}
	return false, nil
}

// tutorialGaveUp ends the run with a reason that names what it was waiting for, and adds the
// one world-level cause that is invisible from the client.
func (b *combatTrainerBot) tutorialGaveUp(ctx *Context, why string, stage freshstart.TutorialStage, action freshstart.TutorialAction) error {
	b.stopReason = why
	switch {
	case action == freshstart.TutorialNoGuide:
		ctx.logf("combattrainer: no NPC named %q offering %q was found within %d tiles. If the account really is standing beside the guide, this is the other cause: %s",
			freshstart.DefaultGuideNPC, freshstart.TalkOp, guideSearchRadius, freshstart.TutorialSkipUnavailable)
	case b.tutorial.sawGuide && !b.tutorial.sawSkip:
		ctx.logf("combattrainer: the guide was reached and talked to, but never offered an option list. %s", freshstart.TutorialSkipUnavailable)
	}
	return fmt.Errorf("combattrainer: %s — stopping rather than spinning. Last state: %q with %q to do", why, stage, action)
}

// tutorialAction reports what the tutorial step is about to do, so a give-up can name it.
//
// On the island it reuses freshstart.NextTutorialAction rather than re-deriving the rule, which
// is the point of that function being pure: the description a failure prints and the decision
// SkipTutorial actually makes cannot drift apart.
func (b *combatTrainerBot) tutorialAction(ctx *Context, stage freshstart.TutorialStage, here *tile.Tile) freshstart.TutorialAction {
	switch stage {
	case freshstart.TutorialStageDesign:
		return freshstart.TutorialAccept
	case freshstart.TutorialStageUnknown:
		return freshstart.TutorialWaitPos
	case freshstart.TutorialStageOff:
		return freshstart.TutorialWaitTabs
	}
	if ctx.Components == nil {
		return freshstart.TutorialNoArchive
	}
	dlg := actions.NewChatDialog(ctx.Session, ctx.Components)
	opts := dlg.Options()
	if len(opts) > 0 {
		b.tutorial.sawSkip = true
	}
	dist := b.guideDistance(ctx, here)
	if dist >= 0 {
		b.tutorial.sawGuide = true
	}
	return freshstart.NextTutorialAction(len(opts), dlg.CanContinue(), dist)
}

// guideDistance is the Chebyshev distance to the nearest RuneScape Guide offering Talk-to, or
// -1 when none is in sight. Mirrors the search SkipTutorial itself makes, including requiring
// the op: an NPC of the right name without Talk-to is not one this can talk to.
func (b *combatTrainerBot) guideDistance(ctx *Context, here *tile.Tile) int {
	if here == nil || ctx.Config == nil {
		return -1
	}
	best := -1
	for _, n := range ctx.Session.State().NPCs() {
		npcType, ok := ctx.Config.Npc(n.Type)
		if !ok || !strings.EqualFold(strings.TrimSpace(npcType.Name), freshstart.DefaultGuideNPC) {
			continue
		}
		if cachecfg.OpIndex(npcType.Ops, freshstart.TalkOp) == 0 {
			continue
		}
		d := tile.Chebyshev(*here, tile.Tile{X: n.X, Z: n.Z, Level: n.Level})
		if d > guideSearchRadius {
			continue
		}
		if best < 0 || d < best {
			best = d
		}
	}
	return best
}

// tutorialDeps is the freshstartrunner view of this run, on the same shape every sibling script
// uses. The walker is passed through when there is one, so the walk to the guide can use it.
func (b *combatTrainerBot) tutorialDeps(ctx *Context) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session:    ctx.Session,
		Pack:       ctx.Pack,
		Locs:       ctx.Locs,
		Config:     ctx.Config,
		Components: ctx.Components,
		Chat:       ctx.Chat,
		Delay:      ctx.Delay,
		Log:        ctx.Log,
		Walker:     b.walker,
	}
}

func describeTile(t *tile.Tile) string {
	if t == nil {
		return "an unknown tile"
	}
	return fmt.Sprintf("%d,%d,%d", t.X, t.Z, t.Level)
}
