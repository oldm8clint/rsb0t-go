package questrunner

// Fleet-wide quest slots: a quest whose key step admits one account at a time is run by one
// account at a time, and the ladder runs another quest meanwhile.
//
// Live on 73d922a6 (27 Sep 04:31-05:15) the Stage 5 cohort reached the new quests together:
// 98 "not let into the shed (someone inside?)" at Witch's House, whose shed holds one player,
// and 71 "no temple guardian (respawning after another account's kill?)" in Priest in Peril,
// whose guardian one account's kill removes for everyone. Every account in the process shares
// this table (the fleet runs in one process).

import (
	"sync"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// exclusiveQuests are the quests whose key step admits few accounts at once; slotCapacity says how
// many (one when unlisted).
var exclusiveQuests = map[string]bool{
	questplan.WitchsHouseID:   true,
	questplan.PriestInPerilID: true,
	questplan.RuneMysteriesID: true,
	// The Holy Grail's Black Knight Titan is one npc with one set of hitpoints and one aggressor:
	// "Someone else is fighting that." refuses a second caster for eight ticks after the first, and
	// a dozen accounts queueing in the arrival pocket (live 13 Oct 2026) only idle there. The slot
	// is held until the account is across his bridge (questplan.GrailPastTitan).
	questplan.HolyGrailID: true,
	// The Restless Ghost's skull is one ground spawn in the Wizards' Tower basement that comes back 100
	// ticks after a pickup. Live 15 Oct 2026 (d729e10a, the quest just promoted) 254 accounts started it
	// together: 127 "the skull is not on the altar" waits and 5 completions in the first ten minutes.
	// The slot is held until the skull is in hand (questplan.GhostPastSkull).
	questplan.RestlessGhostID: true,
	// Imp Catcher's four beads drop from the imps of one field and Cook's Assistant's egg, bucket and pot are single
	// ground spawns: promoted together on 16 Oct 2026, every proven-only account would reach them at once.
	questplan.ImpCatcherID:     true,
	questplan.CooksAssistantID: true,
	// Promoted 17 Oct 2026: Sheep Shearer's shears and pen, Goblin Diplomacy's Goblin Village goblins and Romeo & Juliet's
	// cadava bush are shared too.
	questplan.SheepShearerID:    true,
	questplan.GoblinDiplomacyID: true,
	questplan.RomeoJulietID:     true,
	// Ernest the Chicken's poison, fish food, spade, rubber tube and oil can are single ground spawns that come back 100 ticks
	// after a pickup, and each account needs every one of them. Promoted 18 Oct 2026, with Pirate's Treasure below.
	questplan.ErnestChickenID: true,
	// Pirate's Treasure's rum is a shelf of three at Zambo's that restocks one in a hundred ticks, its apron a shelf of three at
	// Thessalia's, its spade a single spawn and its gardener a single npc: a crowd of accounts reaching the quest together
	// would queue at one or another.
	questplan.PiratesTreasureID: true,
	// Prince Ali Rescue: Lady Keli and the prince are single npcs that one account's rope or rescue removes for every player for a
	// hundred ticks, the log and pickaxe spawns are shared, and four aggressive guards stand round the compound. The slot is
	// released once the prince is out (Module.ExclusiveDone).
	questplan.PrinceAliID: true,
	// Black Knights' Fortress: fifteen aggressive knights about one loop of ladders and doors, a banquet hall the route runs twice,
	// and iron chainbodies Wayne restocks one in five minutes. A crowd of accounts through it at once is a crowd of beatings; the
	// slot is released once the potion is ruined and the account is out (Module.ExclusiveDone).
	questplan.BlackKnightsFortressID: true,
	// Doric's Quest mines its clay in Rimmington, where the mine has two clay rocks (and a bronze pickaxe spawn that comes back after
	// 100 ticks) for everyone, and Gertrude's Cat takes doogle leaves from five ground spawns and a bucket from one: a crowd of
	// accounts sent to either together would queue at a rock or a spawn.
	questplan.DoricsQuestID:  true,
	questplan.GertrudesCatID: true,
	// Monk's Friend's blanket is one ground spawn in the thieves' cave (back 50 ticks after a pickup), and the Clock Tower's four
	// cogs, its rat poison and its bucket are single ground spawns too (100 ticks): every proven-only account would reach them at
	// once.
	questplan.MonksFriendID: true,
	questplan.ClockTowerID:  true,
	// Demon Slayer's drain key is a public ground item in the sewer for three minutes after a pour (any account there may take it, and a
	// crowd of them is a crowd of zombies and spiders on the corridor), Delrith is one npc that single combat gives to one account at a time
	// (the stone circle's wizards hunt every account of combat 41 and under, which is why those never get here: questplan.DemonHuntFit),
	// the bucket is a single spawn and the Wizards' Tower holds one Traiborn: held for the whole run (there is no step it can be released
	// at that the rest does not need).
	questplan.DemonSlayerID: true,
	// Druidic Ritual's four animals live on a handful of spawns that come back 50 to 100 ticks after a kill (two bears, ten
	// rats, the farm's chickens, the dairy field's cows), Wydin's food store keeps one raw beef and one raw chicken, and the
	// cauldron's door raises two suits of armour for each opening within five hundred ticks of the last; Jungle Potion's herbs
	// are four shared locs that stand empty for a hundred ticks after each pick, and its cave is a gauntlet of twelve jogres.
	// Neither is a place for the whole canary cohort at once. Both were promoted on 19 Oct 2026, with Monk's Friend and Demon Slayer above,
	// and so every proven-only account now reaches all four.
	questplan.DruidicRitualID: true,
	questplan.JunglePotionID:  true,
	// Plague City's bucket, spade and picture are single ground spawns, the hangover cure's pestle is a shelf of three at Jatix's and
	// its chocolate bar a shelf of one at Wydin's (a hundred ticks to restock), its snape grass lies among nine hobgoblins and its
	// dwellberry among ten guard dogs: a crowd of accounts through any of them is a queue, or a crowd of beatings.
	questplan.PlagueCityID: true,
	// Biohazard's rotten apples are one ground spawn, its pigeon cages three, its sick mourners two (an account that talks to one
	// turns it on itself and fights it, and the key drops to the account it is aggressive to), and its priest's gown and robe are a
	// shelf of three at Thessalia's (a hundred ticks to restock): a crowd of accounts through the headquarters is a queue at the
	// apples and a scrum round the mourners. The slot is released once Elena has the distillator (Module.ExclusiveDone).
	questplan.BiohazardID: true,
	// The Fishing Contest's Sinister Stranger is one npc whom a clove of garlic in the wall pipe moves from the pipes to the willow tree for
	// every player, and Bonzo seats a player at the pipes only while the Stranger is within eight tiles of the player: an account that
	// stashes its garlic while another is being seated finds the Stranger away. The slot is released once the account is seated at the
	// pipes (Module.ExclusiveDone, questplan.FcPastStranger); the pipes' spot, the vines and Morris are not shared in any way that
	// queues. Tribal Totem has no slot: its crates, its mansion and its lock are the same for everyone, the totem comes again for every
	// player, and the mansion holds as many players as come.
	questplan.FishingContestID: true,
	// The Observatory Quest's planks (four outpost spawns), its sand (one spawn in the goblin huts), its key chest (shut again 300 ticks
	// after a use), its goblin guard (one npc, back 500 ticks after his death) and its sack are shared; the Sea Slug Quest's damp sticks
	// and broken glass are single ground spawns on the platform, Bailey hands out one torch at a time and the platform is one scene.
	questplan.ObservatoryID: true,
	questplan.SeaSlugID:     true,
	// Sheep Herder's four sheep are world npcs with three map copies each, and the one the herd sends over the fence is a 150 tick npc_add at a
	// fixed tile that a second account herding the same sheep would feed and kill from under the first; the barn holds one prod that comes back
	// a hundred ticks after a pickup. One account at a time, released once the fourth sheep is burnt (Module.ExclusiveDone). Dwarf Cannon's
	// remains are one ground spawn on the watchtower's top floor and its dwarf child one npc that the crate adds for 120 ticks; the slot is
	// released once the child is out.
	questplan.SheepHerderID: true,
	questplan.DwarfCannonID: true,
	// The Digsite Quest's panning tray is one ground spawn (back 100 ticks after a pickup), its sample bush, sacks and chest are shared locs, its
	// cave workman is one npc in a closed shaft, its stone tablet one spawn in a cave of ten skeletons, and its pockets, panning points and
	// diggers are one site: held for the whole run (the tray, the cave workman and the tablet are needed at different stages, so there is no step
	// it can be released at).
	questplan.DigsiteID: true,
	// Tai Bwo Wannai Trio's monkeys (six beside Tamayu), its Jogre pen (seven Jogres back 60 ticks after a death), Zambo's rum shelf (three,
	// one back every hundred ticks), the Holy Lake's three Karambwanji spots and the cutscene of Tamayu's hunt, whose two npcs and
	// whose beast share their state between everyone watching (the executor also takes a process-wide turn at it): not a place for the
	// whole canary cohort at once. The slot is held for the whole run, which is long.
	questplan.TaiBwoID: true,
}

// slotRepeats are the exclusive quests whose shared step comes round again on a later run, so a
// finished run is no proof the account is past it: an account across the Titan's bridge that dies
// or is blown out of the realm meets him again from the arrival pocket. Their slot is released when
// the step is passed, but never remembered (HintSlotDone) as passed for good. Druidic Ritual is one: the account that held all four
// meats and lost them (a death, or the Stage 5 trainer banking the raw ones between two runs, which the next run's kit draws back
// only if the bank holds them) is back among the animals the next time.
var slotRepeats = map[string]bool{questplan.HolyGrailID: true, questplan.DruidicRitualID: true, questplan.FishingContestID: true}

// slotCapacity is how many accounts may run a crowded quest at once. Rune Mysteries has no
// one-account step, but its whole route goes through the Wizards' Tower door (3107,3162): live
// 3 Oct 07:19, ~100 accounts sent to it together stalled at that door for half an hour (3,413
// "stalled at 3106,3161" lines, 16 crossings), and the cap was set to four.
//
// The door was never the accounts' doing. It is a DIAGONAL door whose open leaf lands on the very
// tile the crossing aimed at, so the baked crossing was impossible for ONE account too, and the
// Imp Catcher accounts alone looped it the same way on 7 Oct (webwalk.wizardsDoorEdges, claude/
// wiztower-1007, COLLAB.md). With the corrected crossing, 28 crossings of 1536@3107,3162 in the
// 14 Oct 04:05-04:45 log, none refused ("did not step through" 0). Players do not block one
// another in this engine, so what the cap still protects is only the walk-in: twelve at a time.
//
// The cap was also the whole queue's bottleneck on 14 Oct: 8 Rune Mysteries runs held its four
// slots for 10-20 minutes each (nothing completed, see runemysteries.go) and 33 accounts waited on
// "busy: another account is on its one-at-a-time step". At four slots and the old ~15 minute
// runs the 299 accounts that need the quest would have taken about a day to clear.
//
// The Restless Ghost admits eight: with the whole crowd queueing the basement gave 31 completions in 20
// minutes (about 1.5 skulls a minute), and a fresh account spends about six minutes on the priests and the
// ghost before it reaches the basement, so eight holders keep the spawn busy without a queue in the basement
// outgrowing the step's four minute wait.
var slotCapacity = map[string]int{
	questplan.RuneMysteriesID:   12,
	questplan.RestlessGhostID:   8,
	questplan.ImpCatcherID:      12,
	questplan.CooksAssistantID:  6,
	questplan.SheepShearerID:    8,
	questplan.GoblinDiplomacyID: 12,
	questplan.RomeoJulietID:     12,
	// Six at a time: each spawn comes back once a minute, a run lasts about forty minutes and takes each item once, so six holders
	// ask any one spawn for six items in forty minutes, and the gathers' own wait (questmods ernestSpawnWait, 80 seconds) rides out a
	// pickup by another.
	questplan.ErnestChickenID: 6,
	// Eight: each account buys one rum in a 45 minute run against a shelf that refills about one a minute, and an account that
	// finds the shelf empty waits for the next (huntRum).
	questplan.PiratesTreasureID: 8,
	questplan.PrinceAliID:       3,
	// Two at a time: each run is a loop of about forty crossings through fifteen knights and the shops' two or three iron chainbodies.
	questplan.BlackKnightsFortressID: 2,
	// Six accounts share the two clay rocks (a rock is mined empty in a swing or two and comes back in six ticks); eight share the
	// doogle leaves' five spawns, which are held only for the one pickup each.
	questplan.DoricsQuestID:  6,
	questplan.GertrudesCatID: 8,
	// Four at a time each. The blanket is back in 30 seconds and the quest gives its slot up once the blanket is in hand
	// (questplan.MonkPastBlanket); the Clock Tower takes each of its four cogs once per account and its slot is held for the whole
	// run, and the thieves' cave and the ogres' cave are no place for a crowd.
	questplan.MonksFriendID: 4,
	questplan.ClockTowerID:  4,
	// Three at a time: a run is about forty-five minutes and its crowded steps (the pour and the sewer, the circle) are minutes of it, so
	// three holders seldom meet at either, and the cow pasture the bones are farmed in takes more.
	questplan.DemonSlayerID: 3,
	// Four at a time for the ritual's hunts (the slot is released once every meat is in the pack, questplan.DruidPastTheHunt: the
	// cauldron and the dungeon are the account's own) and three for the herbs (the slot is held for the whole run: each herb is one
	// loc for the island and the cave is no place for a crowd).
	questplan.DruidicRitualID: 4,
	questplan.JunglePotionID:  3,
	// Four at a time: a run is up to 56 minutes of errands across three towns, each shelf and spawn asked for once, and the dogs'
	// wood and the hobgoblins' trail are no place for a crowd.
	questplan.PlagueCityID: 4,
	// Three at a time: the two sick mourners are the narrowest place (a fight of a minute or two each, the respawn 140 ticks), and the
	// apples' spawn comes back by itself while the others work.
	questplan.BiohazardID: 3,
	// Four at a time: the Stranger is back at his post within a minute of a seating (a wander of five from his spawn), an account holds
	// its slot until it is seated, and a seat that fails costs the fee-paid account nothing but a second talk with Bonzo.
	questplan.FishingContestID: 4,
	// Three at a time each. The Observatory slot is given up once the lens is made (questplan.ObsPastMould: the planks, the sand, the
	// guard, the key and the sack are all behind the account by then) and the Sea Slug slot once the torch is lit (questplan.SlugPastTorch).
	questplan.ObservatoryID: 3,
	questplan.SeaSlugID:     3,
	// Sheep Herder is one at a time (the default); Dwarf Cannon admits three: its shared steps are minutes of a run whose railings are the
	// long part (hitpoint-bound rolls), and the remains come back a hundred ticks after each pickup.
	questplan.DwarfCannonID: 3,
	// Three at a time: a run is up to 56 minutes of errands over one site, each shared spawn (the tray, the tablet) asked for once, and the
	// altar cave is no place for a crowd.
	questplan.DigsiteID: 3,
	// Three at a time: a run is an hour and a half of errands across the island, each shared thing asked for a few times in it.
	questplan.TaiBwoID: 3,
}

func capacityOf(id string) int {
	if n := slotCapacity[id]; n > 0 {
		return n
	}
	return 1
}

// slotStale frees a slot whose holder never released it (a relog mid-quest drops its Deps). It is longer than
// the longest quest slot of the proven head, so a live holder is never dropped mid-run and its SlotSite choice is kept. That slot was
// Barcrawl's, Imp Catcher's and Pirate's Treasure's 56m15s (an hour was enough) until Druidic Ritual (50 minutes, 62m30s) and Jungle
// Potion (60 minutes, 75m, held for the whole run) were promoted on 19 Oct 2026: Jungle Potion's slot is the longest now, and 90 minutes
// leaves it a quarter of an hour over. A promotion of a longer quest raises this again (TestThePromotedOn19OctSlotsAreUnderTheStaleLimit).
const slotStale = 90 * time.Minute

type questSlot struct {
	holder *Deps
	at     time.Time
	// site is the place of the quest's crowded step this holder works (SlotSite), "" until it has chosen one. It lasts as
	// long as the slot does, and a holder that takes its own slot again keeps it.
	site string
}

var (
	slotMu sync.Mutex
	// slots holds each crowded quest's current holders, at most capacityOf(id) of them.
	slots = map[string][]questSlot{}
)

// tryHoldQuest claims one of id's slots for d, or reports that capacityOf(id) other accounts hold
// them. Quests without a crowded step always succeed. A holder older than slotStale is dropped.
func tryHoldQuest(id string, d *Deps, now time.Time) bool {
	if !exclusiveQuests[id] {
		return true
	}
	slotMu.Lock()
	defer slotMu.Unlock()
	live := slots[id][:0:0]
	site := ""
	for _, s := range slots[id] {
		if s.holder == d {
			site = s.site
			continue
		}
		if now.Sub(s.at) < slotStale {
			live = append(live, s)
		}
	}
	if len(live) >= capacityOf(id) {
		slots[id] = live
		return false
	}
	slots[id] = append(live, questSlot{holder: d, at: now, site: site})
	return true
}

// SlotsFree reports whether d could take a slot of at least one of ids right now, without taking it: a quest with no slot
// (not exclusive) always can, d's own held slot counts, and so does a table with fewer live holders than the capacity. It is
// the look a quest hold takes at the table to decide whether a ladder pass is worth making. The answer is only a hint, a
// pass claims atomically (tryHoldQuest) and may still find the slot gone. Holders older than slotStale are not live.
//
// Why a look and not a timer. Live 29 Sep 2026 a fixed ten minute retry of a slot-blocked ladder stopped and restarted the
// Stage 5 trainer six times an hour for nothing (the table was full each time), and the retry went to 45 minutes. The
// table is full for most of a long queue, so a hold that looked only every 45 minutes left a freed slot to whoever's
// timer fell next, and one that retried every ten kept paying for a pass that found the table full. A look costs a mutex.
func SlotsFree(d *Deps, ids []string, now time.Time) bool {
	slotMu.Lock()
	defer slotMu.Unlock()
	for _, id := range ids {
		if !exclusiveQuests[id] {
			return true
		}
		live := 0
		for _, s := range slots[id] {
			if s.holder == d {
				return true
			}
			if now.Sub(s.at) < slotStale {
				live++
			}
		}
		if live < capacityOf(id) {
			return true
		}
	}
	return false
}

// SlotSite is the site of quest id's crowded step that d works while it holds the quest's slot: the one it chose when it
// first asked, kept until the slot is released. choose is given how many OTHER holders work each site (a holder that has
// not chosen yet, or whose slot has gone stale, counts for none) and names the site for d.
//
// A one-at-a-time quest is a queue for one place; a quest several accounts may run at once is a crowd, and the crowd needs
// somewhere to spread. Imp Catcher admits twelve and farms two fields (questplan.PickImpSite). An account that holds no
// slot (a run outside the ladder, a harness) gets choose's answer for an empty table and nothing is remembered.
func SlotSite(id string, d *Deps, now time.Time, choose func(farmers map[string]int) string) string {
	slotMu.Lock()
	defer slotMu.Unlock()
	farmers := map[string]int{}
	own := -1
	for i, s := range slots[id] {
		if s.holder == d {
			own = i
			continue
		}
		if s.site != "" && now.Sub(s.at) < slotStale {
			farmers[s.site]++
		}
	}
	if own >= 0 && slots[id][own].site != "" {
		return slots[id][own].site
	}
	site := choose(farmers)
	if own >= 0 {
		slots[id][own].site = site
	}
	return site
}

// SlotFarmers is how many holders of quest id's slot work each site now (SlotSite), for a log line.
func SlotFarmers(id string, now time.Time) map[string]int {
	slotMu.Lock()
	defer slotMu.Unlock()
	farmers := map[string]int{}
	for _, s := range slots[id] {
		if s.site != "" && now.Sub(s.at) < slotStale {
			farmers[s.site]++
		}
	}
	return farmers
}

// releaseQuest frees id's slot if d holds one.
func releaseQuest(id string, d *Deps) {
	slotMu.Lock()
	defer slotMu.Unlock()
	kept := slots[id][:0:0]
	for _, s := range slots[id] {
		if s.holder != d {
			kept = append(kept, s)
		}
	}
	if len(kept) == 0 {
		delete(slots, id)
		return
	}
	slots[id] = kept
}

// HintSlotDone is the side-store hint (QuestRecord.Hints) that the account is past its quest's
// one-at-a-time step (Module.ExclusiveDone), so its later runs of the quest do not queue for the
// fleet-wide slot.
const HintSlotDone = "slot-done"

// slotNeeded is whether a ladder pass must hold id's slot before running it: the quest has a
// one-at-a-time step and, with narrow slots on, this account has not already passed it.
func (e *Engine) slotNeeded(id string) bool {
	if !exclusiveQuests[id] {
		return false
	}
	if e.NarrowSlots && e.Store != nil && !slotRepeats[id] {
		if r := e.Store.Quests[id]; r != nil && r.Hints[HintSlotDone] {
			return false
		}
	}
	return true
}

// freeSlotIfPast gives up id's slot, and records that the account is past the step, once the
// module says so. It reports whether it did. A quest whose slot was held for the whole run kept
// every other account out for as long as the run lasted: live, one account waited 24 hours for
// Priest in Peril while the 107 stuck on it queued behind whoever held the slot.
func (e *Engine) freeSlotIfPast(snap questplan.Snapshot) bool {
	m := e.quest.Module
	id := e.quest.ID()
	if !e.NarrowSlots || m.ExclusiveDone == nil || !exclusiveQuests[id] || !m.ExclusiveDone(snap) {
		return false
	}
	releaseQuest(id, e.D)
	if e.Store != nil && !slotRepeats[id] {
		r := e.Store.Record(id)
		if r.Hints == nil {
			r.Hints = map[string]bool{}
		}
		if !r.Hints[HintSlotDone] {
			r.Hints[HintSlotDone] = true
			e.save()
		}
	}
	return true
}

// busyReason is the Blocked reason a quest held by another account gets.
const busyReason = "busy: another account is on its one-at-a-time step"

// WaitingOnSlot reports whether a stuck ladder has a quest it could run but for another
// account's slot: that is a wait, not a failure, and must not count toward giving the quests up.
func WaitingOnSlot(res LadderResult) bool {
	if res.Outcome != LadderStuck {
		return false
	}
	for _, reasons := range res.Blocked {
		if len(reasons) == 1 && reasons[0] == busyReason {
			return true
		}
	}
	return false
}
