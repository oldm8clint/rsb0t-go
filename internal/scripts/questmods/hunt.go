package questmods

import (
	"strings"
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// PiratesTreasure is Pirate's Treasure ready for the engine. Its talks (Frank, Luthas, Wydin), the equip of the apron, the key on
// the chest, the reading of the message and the provisioning of the coins, the apron and the spade are the generic steps of its
// module; the five customs below are what has no generic step: the rum bought from Zambo's shelf, the plantation crate (the
// rum stashed, ten bananas packed, Luthas's shipping), Wydin's back-room crate, the key on Hector's chest (whose script runs
// for five seconds, which a generic use-on would not wait for) and the dig under the gardener's eye.
func PiratesTreasure() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.PiratesTreasure,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomHuntRum:     huntRum,
			questplan.CustomHuntShip:    huntShip,
			questplan.CustomHuntCollect: huntCollect,
			questplan.CustomHuntChest:   huntChest,
			questplan.CustomHuntDig:     huntDig,
		},
	}
}

// huntSaid is the game messages since mark, lower case; a deps without a chat log hears nothing.
func huntSaid(d *questrunner.Deps, mark time.Time) string {
	if d.Chat == nil {
		return ""
	}
	return strings.ToLower(d.Chat.Since(mark))
}

// huntRumBudget bounds the wait for Zambo's shelf: three rums, one more every 100 ticks, and every account that is smuggling
// buys from it.
const huntRumBudget = 5 * time.Minute

// huntRumRetry is the pause between two looks at an empty shelf.
const huntRumRetry = 20 * time.Second

// huntRum buys the Karamjan rum from Zambo. The walk to the bar is the crossing from the mainland (the Port Sarim ship, 30 coins),
// so the coins are looked at first: BuyFromShop never spends the FareReserve it keeps for the ship home, and the rum is about 30.
func huntRum(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.HeldID(d, questplan.ObjKaramjaRum) > 0 {
		return true
	}
	if need, have := questrunner.FareReserve+questplan.HuntRumPrice, questrunner.HeldID(d, questplan.CoinsID); have < need {
		logf(d, "hunt: %d coins in the pack; the rum and the fare home want %d", have, need)
		return false
	}
	deadline := time.Now().Add(huntRumBudget)
	for time.Now().Before(deadline) {
		if questrunner.BuyFromShopAt(d, questplan.ZamboShop, questplan.ObjKaramjaRum, 1, questplan.HuntRumPrice) > 0 ||
			questrunner.HeldID(d, questplan.ObjKaramjaRum) > 0 {
			logf(d, "hunt: a Karamjan rum from Zambo")
			return true
		}
		logf(d, "hunt: Zambo's shelf gave no rum (empty, or another account bought the last): looking again in %s", huntRumRetry)
		if !d.Delay(huntRumRetry) {
			return false
		}
	}
	logf(d, "hunt: no rum from Zambo in %s", huntRumBudget)
	return false
}

// huntCrateLoc is the plantation crate. It is found by id: the plain crates beside it offer the same Search under the same name,
// and four stand within six tiles of Wydin's grocery crate as well.
func huntCrateLoc(d *questrunner.Deps) (locindex.Match, bool) {
	m, ok := questrunner.FindLoc(d, questplan.LocBananaCrate, "", "Search", questplan.PlantationCrate, 12)
	if !ok {
		logf(d, "hunt: no plantation crate (loc %d) near %d,%d", questplan.LocBananaCrate, questplan.PlantationCrate.X, questplan.PlantationCrate.Z)
	}
	return m, ok
}

// huntReadCrate walks to the plantation crate, searches it and reads its messages (questplan.ParseHuntCrate): the only
// reading of the rum and the bananas the client has. The rum's line follows the bananas' in the same tick, so a second look
// is taken a moment after the first.
func huntReadCrate(d *questrunner.Deps) (questplan.HuntCrate, bool) {
	m, ok := huntCrateLoc(d)
	if !ok {
		return questplan.HuntCrate{}, false
	}
	mark := time.Now()
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		logf(d, "hunt: searching the plantation crate: %v", err)
		return questplan.HuntCrate{}, false
	}
	if !waitUntil(d, 8*time.Second, func() bool { _, read := questplan.ParseHuntCrate(huntSaid(d, mark)); return read }) {
		logf(d, "hunt: the plantation crate said nothing readable: [%s]", strings.TrimSpace(huntSaid(d, mark)))
		return questplan.HuntCrate{}, false
	}
	d.Delay(900 * time.Millisecond)
	crate, _ := questplan.ParseHuntCrate(huntSaid(d, mark))
	logf(d, "hunt: the plantation crate holds rum %v and %d banana(s)", crate.Rum, crate.Bananas)
	return crate, true
}

// huntStashResult is what putting the rum in the plantation crate came to.
type huntStashResult int

const (
	// huntStashFailed: the crate would not take it, for a reason the module has no answer to.
	huntStashFailed huntStashResult = iota
	// huntStashedRum: the rum is in the crate.
	huntStashedRum
	// huntStashHeld: the crate already held a rum (the one in the pack is spare).
	huntStashHeld
	// huntStashShop: Wydin's crate holds the rum (the crate has sailed already, and the one in the pack is spare).
	huntStashShop
)

// huntStash puts the rum in the plantation crate. An account that Luthas has paid for a crate is no longer employed and the
// crate refuses its rum ("Why would I want to do that?", banana_crate.rs2 oplocu); the journal does not say which of the two
// the account is on the pages the second smuggle reads, so the refusal is how it is learned, and Luthas hires it again.
func huntStash(e *questrunner.Engine, d *questrunner.Deps) huntStashResult {
	for attempt := 0; attempt < 3; attempt++ {
		if questrunner.HeldID(d, questplan.ObjKaramjaRum) == 0 {
			return huntStashedRum
		}
		m, ok := huntCrateLoc(d)
		if !ok {
			return huntStashFailed
		}
		mark := time.Now()
		if err := questrunner.UseItemOnLoc(d, questplan.ObjKaramjaRum, m); err != nil {
			logf(d, "hunt: the rum on the plantation crate: %v", err)
			return huntStashFailed
		}
		waitUntil(d, 8*time.Second, func() bool {
			return questrunner.HeldID(d, questplan.ObjKaramjaRum) == 0 || questplan.ParseHuntStash(huntSaid(d, mark)) != questplan.HuntStashNone
		})
		said := questplan.ParseHuntStash(huntSaid(d, mark))
		questrunner.ClearLeftoverModal(d)
		switch {
		case said == questplan.HuntStashed || questrunner.HeldID(d, questplan.ObjKaramjaRum) == 0:
			logf(d, "hunt: the rum is in the plantation crate")
			return huntStashedRum
		case said == questplan.HuntStashUnemployed:
			logf(d, "hunt: the crate refused the rum (not employed): asking Luthas for work again")
			if !e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: questplan.LuthasHireStop}) {
				logf(d, "hunt: Luthas's talk did not finish")
			}
		case said == questplan.HuntStashPlanted:
			logf(d, "hunt: the crate already holds a rum")
			return huntStashHeld
		case said == questplan.HuntStashInStore:
			logf(d, "hunt: the rum is already in Wydin's store")
			return huntStashShop
		default:
			logf(d, "hunt: the crate would not take the rum (%d): [%s]", said, strings.TrimSpace(huntSaid(d, mark)))
			return huntStashFailed
		}
	}
	return huntStashFailed
}

// huntDropRum drops the rum in the pack. A spare rum is worth nothing (the quest takes one) and cannot leave the island: the
// customs officer takes it and the ship does not sail, so it is dropped here rather than carried to the pier.
func huntDropRum(d *questrunner.Deps, why string) {
	for try := 0; try < 3 && questrunner.HeldID(d, questplan.ObjKaramjaRum) > 0; try++ {
		before := questrunner.HeldID(d, questplan.ObjKaramjaRum)
		if err := questrunner.OpHeldByName(d, questplan.ObjKaramjaRum, "Drop"); err != nil {
			logf(d, "hunt: dropping the spare rum: %v", err)
			return
		}
		waitUntil(d, 3*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjKaramjaRum) < before })
	}
	logf(d, "hunt: dropped the spare rum (%s)", why)
}

// huntFillBudget bounds the picking and packing of one shipment.
const huntFillBudget = 8 * time.Minute

// huntPickBananas picks bananas from the grove's trees until the pack holds target of them, the grove gives no more, or the
// deadline passes. The bearing trees are found by id (questplan.BananaTreeLocs): an empty tree keeps the name and the Search op,
// and a name search picks it for ever. A tree that gives nothing is set aside.
func huntPickBananas(d *questrunner.Deps, target int, deadline time.Time) {
	refused := map[[2]int]bool{}
	for questrunner.HeldID(d, questplan.ObjBanana) < target && time.Now().Before(deadline) && questrunner.FreeSlots(d) > 0 {
		tree, ok := huntTree(d, refused)
		if !ok {
			// Nothing bears in reach of the grove's middle: the trees come back 500 ticks after the last pick.
			questrunner.Travel(d, questplan.BananaGrove, 4)
			if tree, ok = huntTree(d, refused); !ok {
				logf(d, "hunt: no bearing banana tree in the grove (%d set aside)", len(refused))
				return
			}
		}
		before := questrunner.HeldID(d, questplan.ObjBanana)
		if err := questrunner.OperateLoc(d, tree, "Search"); err != nil {
			logf(d, "hunt: banana tree at %d,%d: %v", tree.X, tree.Z, err)
			refused[[2]int{tree.X, tree.Z}] = true
			continue
		}
		if !waitUntil(d, 5*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjBanana) > before }) {
			refused[[2]int{tree.X, tree.Z}] = true
		}
	}
}

// huntTree is the nearest bearing banana tree to the account that is not set aside.
func huntTree(d *questrunner.Deps, refused map[[2]int]bool) (locindex.Match, bool) {
	if d.Locs == nil || d.Config == nil {
		return locindex.Match{}, false
	}
	here := questrunner.CurrentTile(d)
	if here.Level != 0 || tile.Chebyshev(here, questplan.BananaGrove) > 40 {
		here = questplan.BananaGrove
	}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	for _, m := range d.Locs.Find(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: 0, Radius: 25, IDs: questplan.BananaTreeLocs, Op: "Search"}, live) {
		if !refused[[2]int{m.X, m.Z}] {
			return m, true
		}
	}
	return locindex.Match{}, false
}

// huntFillCrate packs bananas into the plantation crate until it says it is full. Each pass picks what the crate still lacks (as
// many as the pack has room for), packs them one use-on at a time, and reads the crate again: the crate's own count is the
// truth, so no tally is kept here.
func huntFillCrate(d *questrunner.Deps, crate questplan.HuntCrate) bool {
	deadline := time.Now().Add(huntFillBudget)
	for pass := 0; pass < 8 && time.Now().Before(deadline); pass++ {
		want := questplan.HuntCrateCapacity - crate.Bananas
		if want <= 0 {
			return true
		}
		if have := questrunner.HeldID(d, questplan.ObjBanana); have < want {
			huntPickBananas(d, min(want, have+questrunner.FreeSlots(d)), deadline)
		}
		if questrunner.HeldID(d, questplan.ObjBanana) == 0 {
			logf(d, "hunt: the grove gave no banana")
			return false
		}
		missed := 0
		for packed := 0; packed < want && questrunner.HeldID(d, questplan.ObjBanana) > 0 && missed < 3; {
			m, ok := huntCrateLoc(d)
			if !ok {
				return false
			}
			before := questrunner.HeldID(d, questplan.ObjBanana)
			mark := time.Now()
			if err := questrunner.UseItemOnLoc(d, questplan.ObjBanana, m); err != nil {
				logf(d, "hunt: a banana on the plantation crate: %v", err)
				missed++
				continue
			}
			if waitUntil(d, 5*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjBanana) < before }) {
				packed++
			} else {
				missed++
				if strings.Contains(huntSaid(d, mark), "the crate is already full") {
					packed = want
				}
			}
		}
		questrunner.ClearLeftoverModal(d)
		read, ok := huntReadCrate(d)
		if !ok {
			return false
		}
		crate = read
	}
	return crate.Bananas >= questplan.HuntCrateCapacity
}

// huntShip is the plantation leg: read the crate; stash the rum if it is not in there; fill the crate with ten bananas; have
// Luthas pay for it and ship it; read the crate again to see it has gone. It begins wherever the account stands (the
// crossing is the walk to the crate) and it ends on the island; the next decision walks to Wydin's. The journal says the
// same thing for two smuggle states, so nothing here depends on it.
func huntShip(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	crate, ok := huntReadCrate(d)
	if !ok {
		return false
	}
	if crate.Rum && questrunner.HeldID(d, questplan.ObjKaramjaRum) > 0 {
		huntDropRum(d, "the crate already holds one")
	}
	if !crate.Rum {
		if questrunner.HeldID(d, questplan.ObjKaramjaRum) == 0 {
			logf(d, "hunt: the plantation crate holds no rum and none is carried")
			return false
		}
		switch huntStash(e, d) {
		case huntStashShop:
			// The crate sailed earlier; there is nothing to fill or ship, and the rum in the pack is spare.
			huntDropRum(d, "Wydin's crate holds the rum")
			return true
		case huntStashHeld:
			huntDropRum(d, "the crate already holds one")
		case huntStashedRum:
		default:
			return false
		}
		if crate, ok = huntReadCrate(d); !ok || !crate.Rum {
			logf(d, "hunt: the rum is not in the crate after the stash")
			return false
		}
	}
	if crate.Bananas < questplan.HuntCrateCapacity && !huntFillCrate(d, crate) {
		logf(d, "hunt: the crate could not be filled with bananas")
		return false
	}
	// Luthas, shown ten bananas, pays 30 coins and ships the crate (luthas.rs2); the menu after it is only closed. His dialogue
	// shuts and reopens within three ticks, so whether the talk "finished" says nothing: the crate does.
	e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: questplan.LuthasShipStop})
	questrunner.ClearLeftoverModal(d)
	after, ok := huntReadCrate(d)
	if !ok {
		return false
	}
	if after.Rum {
		logf(d, "hunt: Luthas did not ship the crate (it holds %d banana(s))", after.Bananas)
		return false
	}
	logf(d, "hunt: the crate has sailed for Wydin's store")
	return true
}

// huntCollect searches Wydin's back-room crate for the shipped rum. The walk in is through the door at 3012,3204, which opens
// for an account wearing the apron while Wydin is within ten tiles (the caller has equipped it). The crate gives the rum back
// when %crate_rum is 2 and then asks "Do you want to take a banana?": the answer is No, whatever it found.
func huntCollect(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjKaramjaRum) > 0 {
		return true
	}
	if questrunner.WornCounts(d)[questplan.ObjWhiteApron] == 0 {
		logf(d, "hunt: the white apron is not worn: Wydin's back-room door would refuse the account")
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocGroceryCrate, "", "Search", questplan.StoreCrate, 8)
	if !ok {
		logf(d, "hunt: no grocery crate (loc %d) near %d,%d", questplan.LocGroceryCrate, questplan.StoreCrate.X, questplan.StoreCrate.Z)
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		logf(d, "hunt: searching Wydin's crate: %v", err)
		return false
	}
	// "There are a lot of bananas in the crate.", three ticks, the rum when it is there, then the banana prompt.
	dl := questrunner.NewDialogue(d)
	waitUntil(d, 12*time.Second, dl.Ready)
	if dl.Ready() {
		dl.Drive([]string{"No"})
	}
	questrunner.ClearLeftoverModal(d)
	if questrunner.HeldID(d, questplan.ObjKaramjaRum) == 0 {
		logf(d, "hunt: Wydin's back-room crate held no rum")
		return false
	}
	logf(d, "hunt: the rum from Wydin's back room")
	return true
}

// huntChestBudget bounds the wait for the chest's message: one tick before the loc changes and seven after the first line.
const huntChestBudget = 12 * time.Second

// huntChest opens Hector's chest upstairs in the Blue Moon Inn with the key (pirate_message.rs2 [oplocu,piratechest]): the engine
// reaches the chest from one tile only (ChestStand, east of it), the script then runs a second and a half before the message
// is taken and the key spent, so the step waits for the message and never sends the use a second time into that delay. The
// chest is open (and cannot be used on) for ten ticks after any account's use, so an attempt that finds it so waits and tries again.
func huntChest(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjPirateMessage) > 0 {
		return true
	}
	for attempt := 0; attempt < 3; attempt++ {
		if questrunner.HeldID(d, questplan.ObjChestKey) == 0 {
			logf(d, "hunt: no chest key in the pack")
			return false
		}
		m, ok := questrunner.FindLoc(d, questplan.LocPirateChest, "", "", questplan.ChestStand, 6)
		if !ok {
			// Another account's use has it open for ten ticks.
			logf(d, "hunt: Hector's chest (loc %d) is not shut near %d,%d,%d: waiting for it", questplan.LocPirateChest,
				questplan.ChestTile.X, questplan.ChestTile.Z, questplan.ChestTile.Level)
			if !d.Delay(8 * time.Second) {
				return false
			}
			continue
		}
		mark := time.Now()
		if err := questrunner.UseItemOnLoc(d, questplan.ObjChestKey, m); err != nil {
			logf(d, "hunt: the key on the chest: %v", err)
			return false
		}
		if waitUntil(d, huntChestBudget, func() bool { return questrunner.HeldID(d, questplan.ObjPirateMessage) > 0 }) {
			logf(d, "hunt: the pirate message from Hector's chest")
			return true
		}
		logf(d, "hunt: the chest gave no message in %s: [%s]", huntChestBudget, strings.TrimSpace(huntSaid(d, mark)))
	}
	return false
}

// huntDigAttempts bounds the dig: each attempt is a walk to the X, the gardener if he is near, and one use of the spade.
const huntDigAttempts = 4

// huntGardener is the Falador gardener within ten tiles of the account, if one is.
func huntGardener(d *questrunner.Deps) (entityquery.Entity, bool) {
	return entityquery.Npcs(d.Session.State(), d.Config).Within(10).
		Where(func(n entityquery.Entity) bool { return n.ID == questplan.NpcFaladorGardener }).Nearest()
}

// huntDig digs at the X in Falador park. spade.rs2 refuses while a gardener is within ten tiles of the digger and the refusal
// sets him on the digger (dig.rs2 pirate_irate_gardener_attack); he cannot be waited out because he wanders inside that radius
// for ever, so he is killed (level 3, 7 hitpoints, respawning 50 ticks later) and the dig follows at once. The fight ends where the
// gardener fell, so the account walks back onto the X (the spade works within one tile of it). The dig line comes first and the
// treasure four ticks after it; the quest-complete scroll is closed before the step returns.
func huntDig(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjSpade) == 0 {
		logf(d, "hunt: no spade in the pack")
		return false
	}
	sus := questplan.PiratesTreasure.Sustain
	for attempt := 0; attempt < huntDigAttempts; attempt++ {
		if !questrunner.TravelCautious(d, questplan.DigSite, 1) {
			logf(d, "hunt: the walk to the X in Falador park did not arrive")
			return false
		}
		if g, near := huntGardener(d); near {
			logf(d, "hunt: the gardener stands %d tiles from the X: clearing him first", g.Distance)
			questrunner.FightNpc(d, questplan.Step{Kind: questplan.StepFight, TargetID: questplan.NpcFaladorGardener,
				Target: "Gardener", Anchor: questplan.DigSite}, sus)
		}
		if !questrunner.Travel(d, questplan.DigSite, 0) {
			continue
		}
		mark := time.Now()
		if err := questrunner.OpHeldByName(d, questplan.ObjSpade, "Dig"); err != nil {
			logf(d, "hunt: the spade: %v", err)
			return false
		}
		if waitUntil(d, 8*time.Second, func() bool { return strings.Contains(huntSaid(d, mark), "you dig a hole in the ground") }) {
			waitUntil(d, 6*time.Second, func() bool { return strings.Contains(huntSaid(d, mark), "and find a little bag of treasure") })
			questrunner.ClearLeftoverModal(d)
			logf(d, "hunt: dug up the treasure")
			return true
		}
		logf(d, "hunt: the dig found nothing (attempt %d): [%s]", attempt+1, strings.TrimSpace(huntSaid(d, mark)))
	}
	return false
}
