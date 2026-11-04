package questmods

import (
	"sort"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// TaiBwoTrio is Tai Bwo Wannai Trio ready for the engine (questplan/taibwotrio.go has the story, taibwodecide.go the order). Every
// step of it is a custom: the island's walks keep out of the hunters' ground (questplan.TbwtAvoid, which the generic talk step's walk
// does not), and every conversation has gaps longer than the generic driver sits out (if_close; p_delay), so a talk is driven until
// it has been quiet for several seconds instead of until the first lull. Each executor reports what it can prove from the pack (the
// item that arrived or left); what the journal says of the stage is the engine's to read again afterwards (InvalidateStage).
func TaiBwoTrio() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.TaiBwoTrio,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomTbStart:         tbStart,
			questplan.CustomTbReport:        tbReport,
			questplan.CustomTbFish:          tbFish,
			questplan.CustomTbLubufuMeet:    tbLubufuMeet,
			questplan.CustomTbLubufuGive:    tbLubufuGive,
			questplan.CustomTbLubufuAsk:     tbLubufuAsk,
			questplan.CustomTbVessel:        tbVessel,
			questplan.CustomTbBait:          tbCombine(questplan.TbObjRawKarambwanji, questplan.TbObjVessel, questplan.TbObjVesselLoaded, "bait the vessel"),
			questplan.CustomTbTiadecheMeet:  tbTiadecheTalk("talk to Tiadeche", 0),
			questplan.CustomTbTiadecheGift:  tbTiadecheTalk("ask Tiadeche for another raw Karambwan", questplan.TbObjRawKarambwan),
			questplan.CustomTbTiadecheCatch: tbTiadecheCatch,
			questplan.CustomTbKarambwan:     tbKarambwan,
			questplan.CustomTbTamayuMeet:    tbTamayuMeet,
			questplan.CustomTbTamayuHunt:    tbTamayuHunt,
			questplan.CustomTbTamayuGive:    tbTamayuGive,
			questplan.CustomTbCook:          tbCook,
			questplan.CustomTbGrind:         tbGrind,
			questplan.CustomTbPoison:        tbPoison,
			questplan.CustomTbRum:           tbRum,
			questplan.CustomTbBanana:        tbBanana,
			questplan.CustomTbSlice:         tbCombine(questplan.ObjKnife, questplan.ObjBanana, questplan.TbObjSlicedBanana, "slice the banana"),
			questplan.CustomTbMix:           tbCombine(questplan.TbObjSlicedBanana, questplan.ObjKaramjaRum, questplan.TbObjRumSlicedBanana, "drop the banana slices into the rum"),
			questplan.CustomTbMonkey:        tbMonkey,
			questplan.CustomTbSkin:          tbSkin,
			questplan.CustomTbSandwich:      tbCombine(questplan.ObjSeaweed, questplan.TbObjMonkeySkin, questplan.TbObjSandwich, "sandwich the seaweed"),
			questplan.CustomTbJogre:         tbJogre,
			questplan.CustomTbBurn:          tbBurn,
			questplan.CustomTbPasteBones:    tbCombine(questplan.TbObjBurntJogreBones, questplan.TbObjKarambwanjiPaste, questplan.TbObjBurntBonesInPaste, "smother the bones in paste"),
			questplan.CustomTbTinsayMeet:    tbTinsayMeet,
			questplan.CustomTbTinsayGive:    tbTinsayGive,
			questplan.CustomTbManual:        tbManual,
		},
	}
}

const (
	// tbBoxWait is how long a use-on gets to raise its conversation.
	tbBoxWait = 8 * time.Second
	// tbQuiet is the quiet that ends an ordinary talk: the longest pause in these scripts is p_delay(10), six seconds.
	tbQuiet = 8 * time.Second
	// tbStartQuiet is the start's: Timfraku's pause is four ticks, but the scene around it is longer.
	tbStartQuiet = 9 * time.Second
	// tbLubufuQueue is how long Lubufu's brush-off queue lasts (ten ticks) with margin for the click the walker re-sent.
	tbLubufuQueue = 11 * time.Second
	// tbProduct is how long a combination gets to put its product in the pack.
	tbProduct = 8 * time.Second
	// tbFishBudget bounds one round at the Holy Lake; tbFishCast one cast's wait for a catch.
	tbFishBudget = 6 * time.Minute
	tbFishCast   = 25 * time.Second
	// tbKarambwanCasts bounds the vessel's casts (42% each at Fishing 65).
	tbKarambwanCasts = 24
	// tbHuntBack is how long the account is given to be carried out of the sealed map the hunt is watched in, and into it.
	tbHuntIn   = 25 * time.Second
	tbHuntBack = 150 * time.Second
	// tbCookWait is how long a cook gets to turn the input into a product or a burnt one.
	tbCookWait = 14 * time.Second
	// tbMonkeyBudget bounds the cast hunt; tbJogreBudget the Jogre's.
	tbMonkeyBudget = 7 * time.Minute
	tbJogreBudget  = 10 * time.Minute
	// tbFightBudget bounds one fight with a Jogre (60 hitpoints; the model says about a hundred and fifty ticks).
	tbFightBudget = 4 * time.Minute
	// tbReach is the square (half its side) the open-ground flood covers round the account, and tbReachEvery how long a flood is reused.
	tbReach      = 34
	tbReachEvery = 2 * time.Second
	// tbSearch is how far from the account a target may be chosen.
	tbSearch = 24
	// tbSkipFor and tbSkipBusy are how long a target given up on, and one another player was fighting, is left alone.
	tbSkipFor  = 2 * time.Minute
	tbSkipBusy = 20 * time.Second
	// tbCrowd is how close another Jogre makes a target a crowded one.
	tbCrowd = 5
)

// tbHuntMu serialises the accounts of this process through Tamayu's hunt, and tbHuntQueue is how long one waits its turn (a hunt takes
// about two and a half minutes and the slots hold three accounts of the quest). A var so that a test can shorten it.
var (
	tbHuntMu    sync.Mutex
	tbHuntQueue = 8 * time.Minute
)

// tbAvoidRects are questplan.TbwtAvoid as the walker's rectangles, made once.
var tbAvoidRects = sync.OnceValue(func() []webwalk.Rect {
	var out []webwalk.Rect
	for _, r := range questplan.TbwtAvoid() {
		out = append(out, webwalk.Rect{MinX: r.MinX, MaxX: r.MaxX, MinZ: r.MinZ, MaxZ: r.MaxZ, Level: r.Level})
	}
	return out
})

// tbGo walks to within radius of dest keeping out of the hunters' ground. The ships have fares: the same pre-walk step Travel takes
// (Deps.BeforeTravel) runs first, and the avoiding walk is the one TravelAvoiding makes. A walk that cannot be made round the hunters
// is the caller's to park on; none of the quest's walks falls back to the plain one.
func tbGo(d *questrunner.Deps, dest tile.Tile, radius int) bool {
	if here, ok := questrunner.CurrentTileOK(d); ok && here.Level == dest.Level && tile.Chebyshev(here, dest) <= radius {
		return true
	}
	if d.BeforeTravel != nil {
		d.BeforeTravel(dest)
	}
	return questrunner.TravelAvoiding(d, dest, radius, tbAvoidRects())
}

// tbwtReadState is the journal's reading of the quest, for the executors that need to know whether a conversation moved it. A var,
// so that a test can answer it without a quest tab.
var tbwtReadState = func(d *questrunner.Deps) (questplan.TbwtState, bool) {
	lines, err := questrunner.ReadJournal(d, questplan.TaiBwoName)
	if err != nil {
		logf(d, "tbwt: reading the journal: %v", err)
		return questplan.TbwtState{}, false
	}
	n, ok := questplan.ParseTaiBwoJournal(lines)
	return questplan.TbwtStateOf(n), ok
}

// tbDrive drives a conversation to its end through the pauses these scripts make (if_close; p_delay(n) takes the chat away for up to
// six seconds, and the generic driver calls the first lull the end): it answers what is asked from prefer until nothing has been asked
// for quiet. It reports false for a menu with no preferred option (it stops rather than guesses) and for a conversation that never
// ended.
func tbDrive(d *questrunner.Deps, prefer []string, quiet time.Duration) bool {
	dl := questrunner.NewDialogue(d)
	for i := 0; i < 60; i++ {
		if !dl.Ready() && !waitUntil(d, quiet, dl.Ready) {
			return true
		}
		if !dl.Drive(prefer) {
			if dl.Ready() {
				return false
			}
		}
	}
	return false
}

// tbTopUp eats to full hitpoints before a visit to ground the Shaikahan roams: nothing can be eaten inside a conversation.
func tbTopUp(d *questrunner.Deps) {
	if hp, max := questrunner.Hitpoints(d); max > 0 && hp < max {
		questrunner.TopUp(d, questplan.TaiBwoTrio.Sustain.FoodIDs, max)
	}
}

// tbOpen walks to the stop round the hunters and opens its conversation.
func tbOpen(d *questrunner.Deps, stop questplan.NpcStop) bool {
	if !tbGo(d, stop.Anchor, 3) {
		logf(d, "tbwt: %s (%v) was not reached from %v round the hunters", stop.Npc, stop.Anchor, questrunner.CurrentTile(d))
		return false
	}
	if !questrunner.GotoNpc(d, stop, nil) || !questrunner.OpenDialogue(d, stop) {
		logf(d, "tbwt: no conversation with %s from %v", stop.Npc, questrunner.CurrentTile(d))
		return false
	}
	return true
}

// tbTalk is a conversation: the walk, the opening, the driving.
func tbTalk(d *questrunner.Deps, stop questplan.NpcStop, quiet time.Duration) bool {
	if !tbOpen(d, stop) {
		return false
	}
	ok := tbDrive(d, stop.Prefer, quiet)
	questrunner.ClearLeftoverModal(d)
	if !ok {
		logf(d, "tbwt: the conversation with %s did not end cleanly", stop.Npc)
	}
	return ok
}

// tbUseOn walks to the stop, uses a held item on its npc and drives what that raised.
func tbUseOn(d *questrunner.Deps, stop questplan.NpcStop, itemID int, quiet time.Duration) bool {
	if questrunner.HeldID(d, itemID) == 0 {
		logf(d, "tbwt: no %s in the pack to give %s", questrunner.ObjName(d, itemID), stop.Npc)
		return false
	}
	if !tbGo(d, stop.Anchor, 3) || !questrunner.GotoNpc(d, stop, nil) {
		logf(d, "tbwt: %s (%v) was not reached from %v", stop.Npc, stop.Anchor, questrunner.CurrentTile(d))
		return false
	}
	npc, ok := questrunner.FindNpc(d, stop, stop.Leash+2)
	if !ok {
		logf(d, "tbwt: no %s in reach", stop.Npc)
		return false
	}
	if err := questrunner.UseItemOnNpc(d, itemID, npc); err != nil {
		logf(d, "tbwt: %s on %s: %v", questrunner.ObjName(d, itemID), stop.Npc, err)
		return false
	}
	if !waitUntil(d, tbBoxWait, questrunner.NewDialogue(d).Ready) {
		logf(d, "tbwt: %s did not answer the %s", stop.Npc, questrunner.ObjName(d, itemID))
		return false
	}
	ok = tbDrive(d, stop.Prefer, quiet)
	questrunner.ClearLeftoverModal(d)
	return ok
}

// ---------------------------------------------------------------------------------------------------------------------
// Timfraku
// ---------------------------------------------------------------------------------------------------------------------

// tbStart takes the job: the title, "Trufitus sent me.", the gratitude and "Yes". It is proved by the journal's reading of %tbwt_main.
func tbStart(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !tbTalk(d, questplan.TbTimfrakuStartStop, tbStartQuiet) {
		return false
	}
	st, read := tbwtReadState(d)
	if !read {
		return true
	}
	if st.Phase < questplan.TbPhaseStarted {
		logf(d, "tbwt: Timfraku was talked to and the journal still reads the start (Jungle Potion unfinished, or the offer refused)")
		return false
	}
	logf(d, "tbwt: took Timfraku's job")
	return true
}

// tbReport tells Timfraku his sons are home: his reward is 2,000 coins and the quest ends in a queue a few ticks after the talk.
func tbReport(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !tbTalk(d, questplan.TbTimfrakuRewardStop, tbStartQuiet) {
		return false
	}
	if !waitUntil(d, 15*time.Second, func() bool {
		return questrunner.ReadStatus(d, questplan.TaiBwoName) == questplan.StatusComplete
	}) {
		logf(d, "tbwt: Timfraku was told and the quest tab has not turned green")
		return false
	}
	return true
}

// ---------------------------------------------------------------------------------------------------------------------
// Lubufu and the Holy Lake
// ---------------------------------------------------------------------------------------------------------------------

// tbDropShrimp drops the shrimps the Karambwanji net brings up; they are no use and each one is a slot.
func tbDropShrimp(d *questrunner.Deps) {
	for i := 0; i < 8 && questrunner.HeldID(d, tbObjRawShrimp) > 0; i++ {
		if err := questrunner.OpHeldByName(d, tbObjRawShrimp, "Drop"); err != nil {
			logf(d, "tbwt: dropping a shrimp: %v", err)
			return
		}
		d.Delay(700 * time.Millisecond)
	}
}

// tbObjRawShrimp is [raw_shrimp].
const tbObjRawShrimp = 317

// tbShoalOf is the nearest of the lake's Karambwanji spots to the account.
func tbShoalOf(d *questrunner.Deps) (entityquery.Entity, bool) {
	return questrunner.FindNpcByID(d, questplan.TbNpcKarambwanjiSpot, "", 20)
}

// tbFish nets raw Karambwanji at the Holy Lake until the pack holds Qty, or has no room for another, or the round is up. Fishing is
// automatic after the first cast (fish_karambwanji re-sends itself) and a drop interrupts it, so the shrimps are dropped between
// casts and the net is cast again.
func tbFish(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	held := func() int { return questrunner.HeldID(d, questplan.TbObjRawKarambwanji) }
	start := held()
	if start >= step.Qty {
		return true
	}
	if questrunner.HeldID(d, questplan.TbObjNet) == 0 {
		logf(d, "tbwt: no small fishing net in the pack for the Karambwanji")
		return false
	}
	here := questrunner.CurrentTile(d)
	shoals := append([]tile.Tile(nil), questplan.TbLakeShoals...)
	sort.SliceStable(shoals, func(i, j int) bool { return tile.Chebyshev(here, shoals[i]) < tile.Chebyshev(here, shoals[j]) })
	if !tbGo(d, shoals[0], 5) {
		logf(d, "tbwt: the Holy Lake (%v) was not reached from %v round the hunters", shoals[0], here)
		return false
	}
	deadline := time.Now().Add(tbFishBudget)
	shrimp := func() int { return questrunner.HeldID(d, tbObjRawShrimp) }
	for time.Now().Before(deadline) && held() < step.Qty {
		if !questrunner.WaitForGuardian(d) {
			return held() > start
		}
		if shrimp() > 0 {
			tbDropShrimp(d)
		}
		if questrunner.FreeSlots(d) < 1 {
			break
		}
		spot, ok := tbShoalOf(d)
		if !ok {
			logf(d, "tbwt: no Karambwanji shoal in view of the lake's shore")
			break
		}
		if !fluffsApproach(d, tile.Tile{X: spot.X, Z: spot.Z, Level: spot.Level}) {
			logf(d, "tbwt: could not stand beside the Karambwanji shoal at %d,%d", spot.X, spot.Z)
			break
		}
		if err := entityquery.Interact(d.Session, spot, "Net"); err != nil {
			logf(d, "tbwt: casting the net: %v", err)
			break
		}
		// The net keeps casting by itself (fish_karambwanji sends itself again), so the wait goes on while the catches come and ends
		// when they stop, when the shrimps want dropping or the pack is nearly full.
		last := held()
		for time.Now().Before(deadline) && held() < step.Qty && shrimp() < 3 && questrunner.FreeSlots(d) >= 2 {
			if !waitUntil(d, tbFishCast, func() bool {
				return held() > last || shrimp() >= 3 || questrunner.FreeSlots(d) < 2
			}) {
				break
			}
			last = held()
		}
	}
	tbDropShrimp(d)
	logf(d, "tbwt: %d raw Karambwanji in the pack (%d at the start, %d wanted)", held(), start, step.Qty)
	return held() >= step.Qty || held() > start
}

// tbLubufuMeet is the introduction: a talk, the brush-off queue waited out, a talk. The first answer to a stranger arms the queue and
// the second walks the menus to the offer to collect his bait; an account that has met him already needs only the second, and the
// journal says which.
func tbLubufuMeet(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !tbTalk(d, questplan.TbLubufuMeetStop, tbQuiet) {
		return false
	}
	if st, read := tbwtReadState(d); read && st.Lubufu >= questplan.TbLubufuBait {
		return true
	}
	if !d.Delay(tbLubufuQueue) {
		return false
	}
	if !tbTalk(d, questplan.TbLubufuMeetStop, tbQuiet) {
		return false
	}
	st, read := tbwtReadState(d)
	return !read || st.Lubufu >= questplan.TbLubufuBait
}

// tbLubufuGive hands over the Karambwanji; he counts them in himself and keeps what he is owed. It is proved by the pack.
func tbLubufuGive(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	before := questrunner.HeldID(d, questplan.TbObjRawKarambwanji)
	if before == 0 {
		logf(d, "tbwt: no raw Karambwanji to hand Lubufu")
		return false
	}
	if !tbTalk(d, questplan.TbLubufuGiveStop, tbQuiet) {
		return false
	}
	after := questrunner.HeldID(d, questplan.TbObjRawKarambwanji)
	logf(d, "tbwt: handed Lubufu %d raw Karambwanji", before-after)
	return after < before
}

// tbLubufuAsk asks the questions of the hand-in's aftermath until the apprenticeship is offered and accepted and the vessel is in the
// pack. The questions are counted and the journal cannot count them, so it asks until the vessel is there.
func tbLubufuAsk(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	has := func() bool {
		return questrunner.HeldID(d, questplan.TbObjVessel) > 0 || questrunner.HeldID(d, questplan.TbObjVesselLoaded) > 0
	}
	for ask := 0; ask < 6 && !has(); ask++ {
		if questrunner.FreeSlots(d) < 1 {
			logf(d, "tbwt: no room for the vessel Lubufu is about to hand over")
			return false
		}
		if !tbTalk(d, questplan.TbLubufuAskStop, tbQuiet) {
			return false
		}
	}
	return has()
}

// tbVessel asks Lubufu for another vessel (stage 31 has it for anyone carrying none; stage 30 hands it over unasked).
func tbVessel(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	has := func() bool {
		return questrunner.HeldID(d, questplan.TbObjVessel) > 0 || questrunner.HeldID(d, questplan.TbObjVesselLoaded) > 0
	}
	if has() {
		return true
	}
	if questrunner.FreeSlots(d) < 1 {
		logf(d, "tbwt: no room for a vessel")
		return false
	}
	if !tbTalk(d, questplan.TbLubufuVesselStop, tbQuiet) {
		return false
	}
	return waitUntil(d, 6*time.Second, has)
}

// tbCombine uses one held item on another and waits for the product.
func tbCombine(use, on, product int, what string) questrunner.CustomFunc {
	return func(e *questrunner.Engine, step questplan.Step) bool {
		d := e.Deps()
		defer e.InvalidateStage()
		if questrunner.HeldID(d, product) > 0 {
			return true
		}
		if err := questrunner.UseItemOnItem(d, use, on); err != nil {
			logf(d, "tbwt: %s: %v", what, err)
			return false
		}
		if !waitUntil(d, tbProduct, func() bool { return questrunner.HeldID(d, product) > 0 }) {
			logf(d, "tbwt: %s: the pack holds no %s", what, questrunner.ObjName(d, product))
			return false
		}
		questrunner.ClearLeftoverModal(d)
		return true
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Tiadeche
// ---------------------------------------------------------------------------------------------------------------------

// tbTiadecheTalk is a plain talk with Tiadeche, topped up first (the Shaikahan roams his ground); a gift is the item it is proved by.
func tbTiadecheTalk(what string, gift int) questrunner.CustomFunc {
	return func(e *questrunner.Engine, _ questplan.Step) bool {
		d := e.Deps()
		defer e.InvalidateStage()
		tbTopUp(d)
		if !tbTalk(d, questplan.TbTiadecheStop, tbQuiet) {
			return false
		}
		if gift != 0 && questrunner.HeldID(d, gift) == 0 {
			logf(d, "tbwt: %s: nothing was given", what)
			return false
		}
		return true
	}
}

// tbTiadecheCatch hands Tiadeche the baited vessel and takes his first catch.
func tbTiadecheCatch(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	tbTopUp(d)
	if !tbUseOn(d, questplan.TbTiadecheStop, questplan.TbObjVesselLoaded, tbQuiet) {
		return false
	}
	if questrunner.HeldID(d, questplan.TbObjRawKarambwan) == 0 {
		logf(d, "tbwt: Tiadeche took the vessel and the pack holds no raw Karambwan")
		return false
	}
	return true
}

// tbKarambwan lowers a baited vessel at Lubufu's spot (Fishing 65) until a raw Karambwan comes up or the casts are spent. The vessel
// comes back each time, empty after a miss, and one Karambwanji baits it again.
func tbKarambwan(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	has := func() bool { return questrunner.HeldID(d, questplan.TbObjRawKarambwan) > 0 }
	if has() {
		return true
	}
	dest := tile.Tile{X: 2768, Z: 3165}
	if !tbGo(d, dest, 4) {
		logf(d, "tbwt: Lubufu's Karambwan spot (%v) was not reached from %v", dest, questrunner.CurrentTile(d))
		return false
	}
	for cast := 0; cast < tbKarambwanCasts && !has(); cast++ {
		if !questrunner.WaitForGuardian(d) {
			return has()
		}
		if questrunner.HeldID(d, questplan.TbObjVesselLoaded) == 0 {
			if questrunner.HeldID(d, questplan.TbObjRawKarambwanji) == 0 || questrunner.HeldID(d, questplan.TbObjVessel) == 0 {
				logf(d, "tbwt: no vessel or no Karambwanji to bait it with after %d cast(s)", cast)
				return false
			}
			if err := questrunner.UseItemOnItem(d, questplan.TbObjRawKarambwanji, questplan.TbObjVessel); err != nil {
				logf(d, "tbwt: baiting the vessel: %v", err)
				return false
			}
			if !waitUntil(d, tbProduct, func() bool { return questrunner.HeldID(d, questplan.TbObjVesselLoaded) > 0 }) {
				return false
			}
		}
		spot, ok := questrunner.FindNpcByID(d, questplan.TbNpcKarambwanSpotC, "", 12)
		if !ok {
			logf(d, "tbwt: no Karambwan spot beside Lubufu")
			return false
		}
		if !fluffsApproach(d, tile.Tile{X: spot.X, Z: spot.Z, Level: spot.Level}) {
			logf(d, "tbwt: could not stand beside the Karambwan spot")
			return false
		}
		if err := entityquery.Interact(d.Session, spot, "Fish"); err != nil {
			logf(d, "tbwt: lowering the vessel: %v", err)
			return false
		}
		waitUntil(d, 14*time.Second, func() bool { return has() || questrunner.HeldID(d, questplan.TbObjVesselLoaded) == 0 })
	}
	return has()
}

// ---------------------------------------------------------------------------------------------------------------------
// Tamayu
// ---------------------------------------------------------------------------------------------------------------------

func tbTamayuMeet(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	return tbTalk(d, questplan.TbTamayuStop, tbQuiet)
}

// tbTamayuGive hands Tamayu a bottle or a spear (Qty is the object id) and proves it by the pack.
func tbTamayuGive(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !tbUseOn(d, questplan.TbTamayuStop, step.Qty, tbQuiet) {
		return false
	}
	if questrunner.HeldID(d, step.Qty) > 0 {
		logf(d, "tbwt: Tamayu gave the %s back", questrunner.ObjName(d, step.Qty))
		return false
	}
	return true
}

// tbTamayuHunt follows Tamayu on a hunt (Qty 0 the first, 1 the killing one). The hunt is a cutscene: the script closes the chat,
// carries the account to the sealed copy of the map, points the camera, and carries it back beside Tamayu; the account does nothing
// in between. A hunt that did not kill ends in Tamayu's verdict on the agility and the spear, which is read and logged; the
// journal says whether the hunt was the last.
func tbTamayuHunt(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	killing := step.Qty == 1
	// The cutscene's hunter and beast are two npcs at fixed squares that every watcher shares (tbwt_tamayu.rs2 finds them by npc_find
	// and keeps its timers in their %npc_int), so only one account of the process watches at a time; the others queue beside Tamayu.
	if !waitUntil(d, tbHuntQueue, tbHuntMu.TryLock) {
		logf(d, "tbwt: another account has been watching Tamayu's hunt for %s: leaving the queue", tbHuntQueue)
		return false
	}
	defer tbHuntMu.Unlock()
	tbTopUp(d)
	if !tbOpen(d, questplan.TbTamayuStop) {
		return false
	}
	if !tbDrive(d, questplan.TbTamayuStop.Prefer, 3*time.Second) {
		logf(d, "tbwt: the conversation with Tamayu before the hunt did not end cleanly")
	}
	in := func() bool { return questplan.InTbwtCutscene(questrunner.CurrentTile(d)) }
	if !waitUntil(d, tbHuntIn, in) {
		logf(d, "tbwt: Tamayu did not take the account to the hunt (at %v)", questrunner.CurrentTile(d))
		questrunner.ClearLeftoverModal(d)
		return false
	}
	logf(d, "tbwt: watching Tamayu's hunt (killing: %v)", killing)
	if !waitUntil(d, tbHuntBack, func() bool { return !in() }) {
		logf(d, "tbwt: the account is still in the sealed map %s into the hunt (at %v)", tbHuntBack, questrunner.CurrentTile(d))
		return false
	}
	// Tamayu's verdict follows a hunt that did not kill, as a conversation; read it.
	dl := questrunner.NewDialogue(d)
	var said []string
	if waitUntil(d, 6*time.Second, dl.Ready) {
		for i := 0; i < 8 && dl.Ready(); i++ {
			said = append(said, dl.Texts()...)
			dl.Drive(nil)
		}
	}
	questrunner.ClearLeftoverModal(d)
	verdict := strings.ToLower(strings.Join(said, " "))
	if verdict != "" {
		logf(d, "tbwt: Tamayu says after the hunt: %s", strings.TrimSpace(verdict))
	}
	if killing && (strings.Contains(verdict, "cannot match") || strings.Contains(verdict, "not enough damage") || strings.Contains(verdict, "cannot do enough") ||
		strings.Contains(verdict, "healing too quickly")) {
		logf(d, "tbwt: the killing hunt did not kill: Tamayu doubts his agility or his spear")
		return false
	}
	return true
}

// tbCook uses an item on the jungle fire (Qty is the object id: the raw Karambwan, or the burnt bones in paste) and reports whether
// the fire took it. A burnt Karambwan is not a failure of the step (the item was cooked and the roll lost): the Decide sees the burnt
// one in the pack and sources another.
func tbCook(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	id := step.Qty
	if questrunner.HeldID(d, id) == 0 {
		return true
	}
	if !tbGo(d, questplan.TbFire, 6) {
		logf(d, "tbwt: the jungle fire (%v) was not reached from %v round the hunters", questplan.TbFire, questrunner.CurrentTile(d))
		return false
	}
	fire, ok := questrunner.FindLoc(d, questplan.TbLocFire, "", "", questplan.TbFire, 6)
	if !ok {
		logf(d, "tbwt: no fire (loc %d) near %v", questplan.TbLocFire, questplan.TbFire)
		return false
	}
	if err := questrunner.UseItemOnLoc(d, id, fire); err != nil {
		logf(d, "tbwt: %s on the fire: %v", questrunner.ObjName(d, id), err)
		return false
	}
	if !waitUntil(d, tbCookWait, func() bool { return questrunner.HeldID(d, id) == 0 }) {
		logf(d, "tbwt: the fire did not take the %s", questrunner.ObjName(d, id))
		questrunner.ClearLeftoverModal(d)
		return false
	}
	questrunner.ClearLeftoverModal(d)
	return true
}

// tbGrind grinds a held item with the pestle (Qty is the object id) and waits for the paste.
func tbGrind(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	product := questplan.TbObjKarambwanjiPaste
	if step.Qty == questplan.TbObjPoorKarambwan {
		product = questplan.TbObjPoisonPaste
	}
	return tbCombine(questplan.TbObjPestle, step.Qty, product, "grind the "+questrunner.ObjName(d, step.Qty))(e, step)
}

// tbPoison smears the poisonous paste over a spear (Qty is the spear's id).
func tbPoison(e *questrunner.Engine, step questplan.Step) bool {
	for _, sp := range questplan.TbSpears {
		if sp.ID == step.Qty {
			return tbCombine(sp.ID, questplan.TbObjPoisonPaste, sp.PoisonID, "smear the paste over the "+sp.Name)(e, step)
		}
	}
	logf(e.Deps(), "tbwt: no spear with the id %d", step.Qty)
	return false
}

// ---------------------------------------------------------------------------------------------------------------------
// Tinsay's rum
// ---------------------------------------------------------------------------------------------------------------------

// tbBanana picks a banana in the plantation's grove: the bearing trees are found by id and searched (huntPickBananas, the same
// grove Pirate's Treasure packs its crate from).
func tbBanana(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	has := func() bool {
		return questrunner.HeldID(d, questplan.ObjBanana) > 0 || questrunner.HeldID(d, questplan.TbObjSlicedBanana) > 0
	}
	if has() {
		return true
	}
	if questrunner.FreeSlots(d) < 1 {
		logf(d, "tbwt: no room for a banana")
		return false
	}
	if !tbGo(d, questplan.BananaGrove, 5) {
		logf(d, "tbwt: the banana grove (%v) was not reached from %v", questplan.BananaGrove, questrunner.CurrentTile(d))
		return false
	}
	huntPickBananas(d, 1, time.Now().Add(2*time.Minute))
	return has()
}

// tbRum buys a Karamjan rum from Zambo (untradeable, three on the shelf, one back every hundred ticks; the customs officer at the
// Musa Point ship takes it from anyone sailing, so it is bought on the island and given on the island).
func tbRum(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjKaramjaRum) > 0 {
		return true
	}
	if need, have := questrunner.FareReserve+questplan.HuntRumPrice, questrunner.HeldID(d, questplan.CoinsID); have < need {
		logf(d, "tbwt: %d coins in the pack; the rum and the fare home want %d", have, need)
		return false
	}
	if !tbGo(d, questplan.ZamboShop.Anchor, 3) {
		logf(d, "tbwt: Zambo (%v) was not reached from %v", questplan.ZamboShop.Anchor, questrunner.CurrentTile(d))
		return false
	}
	deadline := time.Now().Add(huntRumBudget)
	for time.Now().Before(deadline) {
		if questrunner.BuyFromShopAt(d, questplan.ZamboShop, questplan.ObjKaramjaRum, 1, questplan.HuntRumPrice) > 0 ||
			questrunner.HeldID(d, questplan.ObjKaramjaRum) > 0 {
			logf(d, "tbwt: a Karamjan rum from Zambo")
			return true
		}
		logf(d, "tbwt: Zambo's shelf gave no rum: looking again in %s", huntRumRetry)
		if !d.Delay(huntRumRetry) {
			return false
		}
	}
	return false
}

// tbTinsayMeet is a plain talk with Tinsay, at the stages where he asks nothing the pack can answer (the first meeting and the two
// "Can we go now?" talks after a hand-in).
func tbTinsayMeet(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	return tbTalk(d, questplan.TbTinsayStop, 10*time.Second)
}

// tbTinsayGive hands Tinsay what he asked for (Qty is the object id: the rum, the sandwich, the marinade, or a vessel for the manual).
func tbTinsayGive(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	id := step.Qty
	if !tbUseOn(d, questplan.TbTinsayStop, id, 10*time.Second) {
		return false
	}
	if questrunner.HeldID(d, id) > 0 {
		logf(d, "tbwt: Tinsay did not keep the %s", questrunner.ObjName(d, id))
		return false
	}
	return true
}

// tbManual talks to Tiadeche with the manual in the pack: at the stage that has it, the talk takes it ("Yes. He gave me this.").
func tbManual(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	tbTopUp(d)
	if !tbTalk(d, questplan.TbTiadecheStop, tbQuiet) {
		return false
	}
	if questrunner.HeldID(d, questplan.TbObjManual) > 0 {
		// The talk has no use for it at the stage the pack disagrees with: use it on him.
		if !tbUseOn(d, questplan.TbTiadecheStop, questplan.TbObjManual, tbQuiet) {
			return false
		}
	}
	return questrunner.HeldID(d, questplan.TbObjManual) == 0
}

// ---------------------------------------------------------------------------------------------------------------------
// The monkey and the Jogre
// ---------------------------------------------------------------------------------------------------------------------

// tbHunter is a hunt's open-ground reach: the flood round the account, reused while it stands still.
type tbHunter struct {
	d         *questrunner.Deps
	reach     *pathfind.Reach
	reachFrom tile.Tile
	reachAt   time.Time
	skip      map[int]time.Time
}

func newTbHunter(d *questrunner.Deps) *tbHunter { return &tbHunter{d: d, skip: map[int]time.Time{}} }

func (h *tbHunter) floodFrom(here tile.Tile) *pathfind.Reach {
	if h.d.Pack == nil {
		return nil
	}
	if h.reach != nil && h.reachFrom == here && time.Since(h.reachAt) < tbReachEvery {
		return h.reach
	}
	reach := h.d.Pack.ReachFrom(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, tbReach, nil)
	if reach.Size() == 0 {
		reach = nil
	}
	h.reach, h.reachFrom, h.reachAt = reach, here, time.Now()
	return reach
}

// stepsTo is the walk over open ground to a tile the engine accepts an attack on t from (-1 for none; without a pack, the distance).
func (h *tbHunter) stepsTo(here, t tile.Tile) int {
	return interactSteps(h.d.Pack, h.floodFrom(here), here, t)
}

// near lists the npcs of one id in view within tbSearch of the account that are not set aside and that no other player is fighting.
func (h *tbHunter) near(id int) []entityquery.Entity {
	d := h.d
	now, self := time.Now(), d.Session.State().Self()
	var out []entityquery.Entity
	for _, e := range entityquery.Npcs(d.Session.State(), d.Config).Within(tbSearch).Results() {
		if e.ID == id && now.After(h.skip[e.Index]) && !goblinBusy(e, self) {
			out = append(out, e)
		}
	}
	return out
}

// tbMonkey casts Wind Bolts at monkeys until one drops its corpse. A monkey deflects every melee blow and every arrow while the quest
// runs (monkey.rs2 [opnpc2,monkey]); a cast goes through [opnpct], which that script does not answer. The monkey is chosen by a
// walk over open ground, so one behind a fence is not stood under; it dies in two or three casts (6 hitpoints) and drops its corpse
// where it falls, to the account that hurt it.
func tbMonkey(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	has := func() bool {
		return questrunner.HeldID(d, questplan.TbObjMonkeyCorpse) > 0 || questrunner.HeldID(d, questplan.TbObjMonkeySkin) > 0 ||
			questrunner.HeldID(d, questplan.TbObjSandwich) > 0
	}
	if has() {
		return true
	}
	if questrunner.FreeSlots(d) < 1 {
		logf(d, "tbwt: no room in the pack for a monkey's corpse")
		return false
	}
	if !tbGo(d, questplan.TbMonkeys, 5) {
		logf(d, "tbwt: the monkeys (%v) were not reached from %v round the hunters", questplan.TbMonkeys, questrunner.CurrentTile(d))
		return false
	}
	spell, ok := questrunner.BestCastableSpellWhere(d, 2, func(s actions.Spell) bool { return s.Name == "Wind Bolt" })
	if !ok {
		logf(d, "tbwt: no Wind Bolt to cast: Magic %d and %d air, %d chaos runes", questrunner.BaseLevels(d)["magic"],
			questrunner.HeldID(d, questplan.ObjAirRune), questrunner.HeldID(d, questplan.ObjChaosRune))
		e.RequestPark("no Wind Bolt castable at the monkeys (its runes, or Magic 17)", questplan.TbLegPark)
		return false
	}
	h := newTbHunter(d)
	sus := questplan.TaiBwoTrio.Sustain
	deadline := time.Now().Add(tbMonkeyBudget)
	killed, casted := 0, 0
	for time.Now().Before(deadline) && !has() {
		if !questrunner.WaitForGuardian(d) {
			return has()
		}
		here := questrunner.CurrentTile(d)
		var target entityquery.Entity
		best, found := 0, false
		for _, m := range h.near(questplan.TbNpcMonkey) {
			n := h.stepsTo(here, tile.Tile{X: m.X, Z: m.Z, Level: m.Level})
			if n < 0 {
				continue
			}
			if !found || n < best {
				target, best, found = m, n, true
			}
		}
		if !found {
			logf(d, "tbwt: no monkey in reach of %v; looking again", here)
			if tile.Chebyshev(here, questplan.TbMonkeys) > 12 {
				tbGo(d, questplan.TbMonkeys, 5)
			}
			if !d.Delay(3 * time.Second) {
				return has()
			}
			continue
		}
		last := tile.Tile{X: target.X, Z: target.Z, Level: target.Level}
		casts, began := 0, time.Now()
		for time.Now().Before(deadline) {
			cur, alive := tbAlive(d, target)
			if !alive {
				break
			}
			last = tile.Tile{X: cur.X, Z: cur.Z, Level: cur.Level}
			if casts >= 4 && cur.TotalHealth == 0 {
				logf(d, "tbwt: four casts at the monkey at %d,%d drew no health bar: leaving it", cur.X, cur.Z)
				h.skip[target.Index] = time.Now().Add(tbSkipFor)
				break
			}
			if time.Since(began) > time.Minute {
				h.skip[target.Index] = time.Now().Add(tbSkipFor)
				break
			}
			if d.Chat != nil && actions.IsNoRunesLine(d.Chat.Since(began)) {
				logf(d, "tbwt: out of runes for %s", spell.Name)
				e.RequestPark("out of the runes for the monkey's Wind Bolt", questplan.TbLegPark)
				return has()
			}
			questrunner.EatIfNeeded(d, sus.FoodIDs, sus.EatAt)
			if err := actions.CastOnNpc(d.Session, spell.Name, target.Index); err != nil {
				logf(d, "tbwt: cast %s: %v", spell.Name, err)
				return has()
			}
			casts++
			casted++
			if !questrunner.WaitCastEating(d, sus) {
				return has()
			}
		}
		d.Delay(900 * time.Millisecond)
		if questrunner.TakeGround(d, questplan.TbObjMonkeyCorpse, last, 4) || has() {
			killed++
			logf(d, "tbwt: a monkey's corpse from the kill at %d,%d (%d casts)", last.X, last.Z, casts)
		}
	}
	logf(d, "tbwt: the monkey hunt ends with %d kill(s) and %d cast(s): corpse held %v", killed, casted, has())
	return has()
}

// tbAlive is the npc as it stands now, and whether it is still in view.
func tbAlive(d *questrunner.Deps, e entityquery.Entity) (entityquery.Entity, bool) {
	return entityquery.Npcs(d.Session.State(), d.Config).Within(tbSearch + 4).
		Where(func(n entityquery.Entity) bool { return n.Index == e.Index && n.ID == e.ID }).Nearest()
}

// tbJogre kills a Jogre at the pen for its bones, which it always drops (jogre.rs2: death_drop tbwt_jogre_bones). The target is the
// loneliest one (the fewest other Jogres within tbCrowd, then the nearest by a walk over open ground): the pen holds seven within a
// few tiles of each other, and a cowardly Jogre hunts from two tiles away. The fight is the shared melee loop, priced by the Decide
// (MeleeChain on the account's levels and food) and given up when no blow lands or another player has the Jogre. An iron or better
// spear dropped on the way (4 in 129) is taken if the quest still wants one.
func tbJogre(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.TbObjJogreBones) > 0 {
		return true
	}
	if questrunner.FreeSlots(d) < 2 {
		logf(d, "tbwt: no room in the pack for the Jogre's bones")
		return false
	}
	if !tbGo(d, questplan.TbJogreStage, 3) {
		logf(d, "tbwt: the Jogre pen (%v) was not reached from %v round the hunters", questplan.TbJogreStage, questrunner.CurrentTile(d))
		return false
	}
	h := newTbHunter(d)
	sus := questplan.TaiBwoTrio.Sustain
	deadline := time.Now().Add(tbJogreBudget)
	for time.Now().Before(deadline) && questrunner.HeldID(d, questplan.TbObjJogreBones) == 0 {
		if !questrunner.WaitForGuardian(d) {
			break
		}
		here := questrunner.CurrentTile(d)
		jogres := h.near(questplan.NpcJogre)
		var target entityquery.Entity
		bestScore, found := 0, false
		for _, j := range jogres {
			at := tile.Tile{X: j.X, Z: j.Z, Level: j.Level}
			steps := h.stepsTo(here, at)
			if steps < 0 || tile.Chebyshev(at, questplan.TbJogreTarget) > 12 {
				continue
			}
			crowd := 0
			for _, o := range jogres {
				if o.Index != j.Index && tile.Chebyshev(at, tile.Tile{X: o.X, Z: o.Z, Level: o.Level}) <= tbCrowd {
					crowd++
				}
			}
			score := crowd*1000 + steps
			if !found || score < bestScore {
				target, bestScore, found = j, score, true
			}
		}
		if !found {
			logf(d, "tbwt: no Jogre in reach of %v; looking again", here)
			if tile.Chebyshev(here, questplan.TbJogreStage) > 6 {
				tbGo(d, questplan.TbJogreStage, 3)
			}
			if !d.Delay(3 * time.Second) {
				break
			}
			continue
		}
		began := time.Now()
		last := tile.Tile{X: target.X, Z: target.Z, Level: target.Level}
		why := ""
		giveUp := func() bool {
			cur, ok := tbAlive(d, target)
			if !ok {
				return false
			}
			last = tile.Tile{X: cur.X, Z: cur.Z, Level: cur.Level}
			since := time.Since(began)
			switch {
			case h.stepsTo(questrunner.CurrentTile(d), last) < 0:
				why = "no walk over open ground reaches it"
			case since >= 4*time.Second && goblinBusy(cur, d.Session.State().Self()):
				why = "another player is fighting it"
			case since >= 30*time.Second && cur.TotalHealth == 0:
				why = "no blow has landed on it"
			}
			return why != ""
		}
		logf(d, "tbwt: fighting the Jogre at %d,%d", target.X, target.Z)
		switch questrunner.FightChosen(d, target, sus, tbFightBudget, giveUp) {
		case questrunner.FightWon:
			d.Delay(900 * time.Millisecond)
			if questrunner.TakeGround(d, questplan.TbObjJogreBones, last, 4) || questrunner.HeldID(d, questplan.TbObjJogreBones) > 0 {
				logf(d, "tbwt: the Jogre's bones from the kill at %d,%d", last.X, last.Z)
				tbTakeSpear(d, last)
			} else {
				logf(d, "tbwt: the Jogre at %d,%d died and no bones were found beside it", last.X, last.Z)
			}
		case questrunner.FightGaveUp:
			h.skip[target.Index] = time.Now().Add(tbSkipFor)
			logf(d, "tbwt: gave up on the Jogre at %d,%d: %s", target.X, target.Z, why)
		case questrunner.FightTimedOut:
			h.skip[target.Index] = time.Now().Add(tbSkipFor)
			logf(d, "tbwt: the Jogre at %d,%d is still standing after %s", target.X, target.Z, tbFightBudget)
		case questrunner.FightLost:
			logf(d, "tbwt: the fight with the Jogre was lost; ending the hunt")
			return questrunner.HeldID(d, questplan.TbObjJogreBones) > 0
		case questrunner.FightStopped:
			return questrunner.HeldID(d, questplan.TbObjJogreBones) > 0
		}
	}
	return questrunner.HeldID(d, questplan.TbObjJogreBones) > 0
}

// tbTakeSpear picks up a spear above bronze the kill dropped, when the pack holds none and room remains.
func tbTakeSpear(d *questrunner.Deps, at tile.Tile) {
	for _, sp := range questplan.TbSpears {
		if questrunner.HeldID(d, sp.ID) > 0 || questrunner.HeldID(d, sp.PoisonID) > 0 {
			return
		}
	}
	if questrunner.FreeSlots(d) < 1 {
		return
	}
	for _, sp := range questplan.TbSpears {
		if questrunner.TakeGround(d, sp.ID, at, 4) {
			logf(d, "tbwt: a %s dropped by the Jogre", sp.Name)
			return
		}
	}
}

// tbBurn burns the Jogre bones with Superheat Item (Magic 43, four fire runes and a nature rune): the script deletes the runes and
// turns the bones in their slot into burnt ones.
func tbBurn(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.TbObjBurntJogreBones) > 0 {
		return true
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	for _, it := range eq.Inventory() {
		if int(it.ID) != questplan.TbObjJogreBones {
			continue
		}
		if err := actions.CastOnItem(d.Session, "Superheat Item", it.ID, it.Slot, actions.PlayerPack); err != nil {
			logf(d, "tbwt: casting Superheat Item on the Jogre bones: %v", err)
			return false
		}
		if !waitUntil(d, 10*time.Second, func() bool { return questrunner.HeldID(d, questplan.TbObjBurntJogreBones) > 0 }) {
			logf(d, "tbwt: Superheat Item did not burn the Jogre bones (Magic 43, four fire runes and a nature rune)")
			return false
		}
		d.Delay(1500 * time.Millisecond)
		return true
	}
	logf(d, "tbwt: no Jogre bones in the pack to burn")
	return false
}

// tbSkin gives Tamayu the monkey's corpse: he skins it (the monkey's skin and a monkey's bones) once his own hunt is over.
func tbSkin(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.TbObjMonkeySkin) > 0 {
		return true
	}
	if questrunner.FreeSlots(d) < 1 {
		logf(d, "tbwt: no room for the skin and the bones Tamayu hands back")
		return false
	}
	if !tbUseOn(d, questplan.TbTamayuStop, questplan.TbObjMonkeyCorpse, tbQuiet) {
		return false
	}
	return questrunner.HeldID(d, questplan.TbObjMonkeySkin) > 0
}
