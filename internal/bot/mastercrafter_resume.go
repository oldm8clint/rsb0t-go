package bot

// Carrying on a crystal chest round after a restart (a user request: "it needs to remember what it
// was doing last time the script was running and continue that if theres any supplies left for it
// before just heading to the mule upon startup").
//
// The decision is mastercraftplan.DecideWithChest's (an account on the Falador side with keys of its own
// is in the middle of its trips) and the round's own start is masterChest's (ChestResumes: no walk to the
// post, no ask). Three things in the way a restart runs stood between them, all of them here:
//
//  1. The safe point. A round may only start where the walk to the mule's post is short or plain
//     (mastercraftplan.SafeInput.FarFromMule): anywhere east of White Wolf Mountain is "far", and the
//     Falador road is east of it. A resumed round walks nowhere near the post, so it does not ask for it.
//  2. The first check. A fleet restart starts this script before the mule's heartbeat is heard and often
//     before the account's tile and pack are read, so the first decision is made blind (no chest round
//     under way is seen) and the next one is two minutes away (masterCrafterRecheck), by which time the
//     idle walk to the post (masterPostIdle) has begun and is a long walk: Loop decides again the first
//     pass that can read both (decideAgainWhenReady).
//  3. The idle walk itself. A resumed round that has to wait for its safe point (a hit lately, hitpoints
//     under the floor) must not be walked to the post meanwhile: the idle swarm holds still for it
//     (masterPostIdle.Loop).

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// smithResumeNear is how close to the Varrock west bank (tiles) an account counts as on Varrock's side, where a
// resumed anvil round starts without a walk to the mule's post.
const smithResumeNear = 150

// masterAccountRead is whether the session has read what a decision about this account needs: where it
// stands and what its pack holds (a restart finds both empty for a moment).
func masterAccountRead(ctx *Context) bool {
	if ctx == nil || ctx.Session == nil {
		return false
	}
	st := ctx.Session.State()
	if _, _, _, ok := st.Tile(); !ok {
		return false
	}
	return st.Inventory(actions.PlayerPack) != nil
}

// decideAgainWhenReady turns the next pass into a check when the last one was made blind (blindCheck)
// and the mule is heard and the account read now.
func (b *masterCrafterBot) decideAgainWhenReady(ctx *Context) {
	if !b.blindCheck || !masterAccountRead(ctx) {
		return
	}
	if p := scarabMulePresenceOf(ctx, b.mule); !p.Heard || !p.SameWorld {
		return
	}
	b.blindCheck = false
	b.lastCheck = time.Time{}
	b.logf(ctx, "check: the last one was made before the mule was heard or the account was read: deciding again now")
}

// resumesHere is whether dec is a round that carries on where the account stands: a chest round decided
// as a resume, the account holding keys of its own, on the Falador side (mastercraftplan.ChestResumes);
// or a jewellery round decided as a resume while the account stands on the Falador side, where the
// bank and the furnace are: a batch the script's restart interrupted there is carried on from the
// pack and the bank, with no walk to the mule's post first (the post is for the hand-over).
func (b *masterCrafterBot) resumesHere(ctx *Context, dec mastercraftplan.Decision) bool {
	if dec.Mode == mastercraftplan.ModeTrawler {
		// The trawler round works at Port Khazard and its banks, never at the mule's post: no walk to the post first.
		return true
	}
	if !dec.Resume {
		return false
	}
	switch dec.Mode {
	case mastercraftplan.ModeNature:
		// A gate round under way (in the Nature temple, or at the gate) carries on where the account stands: the walk to the mule's
		// post is for the end of the round.
		return true
	case mastercraftplan.ModeRunecraft:
		// A levelling round under way (inside a temple or the essence mine, or on the road with a load and the
		// talisman) carries on where the account stands: from inside a temple there is no walk to the post.
		return true
	case mastercraftplan.ModeChest:
		return mastercraftplan.ChestResumes(dec, b.ownChestKeys(ctx), b.onFaladorSide(ctx))
	case mastercraftplan.ModeJewellery:
		return b.onFaladorSide(ctx)
	case mastercraftplan.ModeCannonballs:
		// The Steel bars are in this account's own bank: a resumed cannonball round on the Falador side starts at the Falador west bank.
		return b.cannonResumesHere(ctx)
	case mastercraftplan.ModeSmith, mastercraftplan.ModePlatebody:
		// The bars are in this account's own bank: a resumed anvil round walks to Varrock west, not to the post.
		x, z, level, ok := ctx.Session.State().Tile()
		return ok && level == 0 && tile.Chebyshev(tile.Tile{X: x, Z: z}, mastercraftrunner.VarrockWestBank) <= smithResumeNear
	}
	return false
}
