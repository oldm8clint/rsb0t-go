package bot

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/peerplan"
	"rsb0t-go/internal/scripts/peerrunner"
	"rsb0t-go/internal/scripts/questmods"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/waterfallplan"
	"rsb0t-go/internal/scripts/waterfallrunner"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("QuestLadder", func() Bot { return &questLadderBot{} })
}

// questLadderBot is the standalone quest bot on the ONE quest engine (questplan/questrunner):
// it runs one quest, or the ladder of quests it is given, so each quest is provable on its own
// before ScarabSwarm's Stage 5 ladder hooks the same engine in (a later lane; this bot touches
// no scarab file).
//
// Settings (all optional):
//
//	quests          []string  default ["waterfall"]   — quest ids in order; the ladder runs the
//	                                                    first one that is not complete, not parked
//	                                                    and whose requirements hold.
//	mule            string    default ""              — the mule's name; empty means no mule (the
//	                                                    provisioner then goes bank, ground, skill,
//	                                                    shop). The mule is an accelerator, never a
//	                                                    dependency.
//	mulePost        tile      default 2655,3286       — where to stand for the mule (East Ardougne
//	                                                    south bank).
//	storeDir        string    default data/scarabswarm-checkpoints — the quest side store's root:
//	                                                    <storeDir>/<account>/ScarabSwarmQuests.json.
//	perQuestMinutes int       default 90
//	waterfallMaxDeaths int    default 2               — deaths before Waterfall parks.
//	finishWhenDone  bool      default true            — ask the fleet to finish the script once the
//	                                                    ladder is done rather than idling.
//	provisionDisabled bool    default false           — a harness CONTROL arm only: every kit fails.
//	budgetMinutes   int       default 360             — one ladder pass's overall budget.
type questLadderBot struct {
	engine   *questrunner.Engine
	quests   []questrunner.Quest
	perQuest time.Duration
	budget   time.Duration
	finish   bool
	storeAt  string
	done     bool
}

// QuestFactories are the quests this bot can run, by id.
var QuestFactories = map[string]func(settings map[string]interface{}) questrunner.Quest{
	waterfallplan.QuestID: func(m map[string]interface{}) questrunner.Quest {
		set := waterfallplan.DefaultSettings()
		set.MaxDeaths = IntSetting(m, "waterfallMaxDeaths", 2)
		return waterfallrunner.Quest(set)
	},
	questplan.RestlessGhostID:    func(map[string]interface{}) questrunner.Quest { return questmods.RestlessGhost() },
	questplan.WitchsPotionID:     func(map[string]interface{}) questrunner.Quest { return questmods.WitchsPotion() },
	questplan.ImpCatcherID:       func(map[string]interface{}) questrunner.Quest { return questmods.ImpCatcher() },
	questmods.BarcrawlID:         func(map[string]interface{}) questrunner.Quest { return questmods.Barcrawl() },
	questplan.VampireSlayerID:    func(map[string]interface{}) questrunner.Quest { return questmods.VampireSlayer() },
	questplan.MerlinsCrystalID:   func(map[string]interface{}) questrunner.Quest { return questmods.MerlinsCrystal() },
	questplan.PriestInPerilID:    func(map[string]interface{}) questrunner.Quest { return questmods.PriestInPeril() },
	questplan.TreeGnomeVillageID: func(map[string]interface{}) questrunner.Quest { return questmods.TreeGnomeVillage() },
	questplan.HolyGrailID:        func(map[string]interface{}) questrunner.Quest { return questmods.HolyGrail() },
	questplan.FightArenaID:       func(map[string]interface{}) questrunner.Quest { return questmods.FightArena() },
	questplan.WitchsHouseID:      func(map[string]interface{}) questrunner.Quest { return questmods.WitchsHouse() },
	questplan.DeathPlateauID:     func(map[string]interface{}) questrunner.Quest { return questmods.DeathPlateau() },
	questplan.NatureSpiritID:     func(map[string]interface{}) questrunner.Quest { return questmods.NatureSpirit() },
	"agility25":                  func(map[string]interface{}) questrunner.Quest { return questmods.GnomeAgility("agility25", 25) },
	"agility35":                  func(map[string]interface{}) questrunner.Quest { return questmods.GnomeAgility("agility35", 35) },
	"crafting18":                 func(map[string]interface{}) questrunner.Quest { return questmods.CraftingSpin("crafting18", 18) },
	"crafting40":                 func(map[string]interface{}) questrunner.Quest { return questmods.CraftingSpin("crafting40", 40) },
	"cows20":                     func(map[string]interface{}) questrunner.Quest { return questmods.CowsRung("cows20", "hitpoints", 20) },
	"prayer31":                   func(map[string]interface{}) questrunner.Quest { return questmods.CowsRung("prayer31", "prayer", 31) },
	"attack20":                   func(map[string]interface{}) questrunner.Quest { return questmods.CowsRung("attack20", "attack", 20) },
	"prayer43":                   func(map[string]interface{}) questrunner.Quest { return questmods.CowsRung("prayer43", "prayer", 43) },
	"smithing34":                 func(map[string]interface{}) questrunner.Quest { return questmods.SmithingRung("smithing34", 34) },
	"woodcutting40":              func(map[string]interface{}) questrunner.Quest { return questmods.WoodcuttingRung("woodcutting40", 40) },
	"fletching25":                func(map[string]interface{}) questrunner.Quest { return questmods.FletchingRung("fletching25", 25) },
	questplan.MurderMysteryID:    func(map[string]interface{}) questrunner.Quest { return questmods.MurderMystery() },
	questplan.CooksAssistantID:   func(map[string]interface{}) questrunner.Quest { return questmods.CooksAssistant() },
	questplan.SheepShearerID:     func(map[string]interface{}) questrunner.Quest { return questmods.SheepShearer() },
	questplan.ScorpionCatcherID:  func(map[string]interface{}) questrunner.Quest { return questmods.ScorpionCatcher() },
	questplan.GrandTreeID:        func(map[string]interface{}) questrunner.Quest { return questmods.GrandTree() },
	questplan.DragonSlayerID:     func(map[string]interface{}) questrunner.Quest { return questmods.DragonSlayer() },
	questplan.HorrorID:           func(map[string]interface{}) questrunner.Quest { return questmods.HorrorFromTheDeep() },
	questplan.FremennikID:        func(map[string]interface{}) questrunner.Quest { return questmods.FremennikTrials() },
	// The quest-point fillers (W13): unproven, the tail of the owner's ladder just ahead of Dragon Slayer.
	questplan.GoblinDiplomacyID: func(map[string]interface{}) questrunner.Quest { return questmods.GoblinDiplomacy() },
	questplan.RomeoJulietID:     func(map[string]interface{}) questrunner.Quest { return questmods.RomeoJuliet() },
	// Ernest the Chicken (W13): unproven, in the tail ahead of Dragon Slayer.
	questplan.ErnestChickenID: func(map[string]interface{}) questrunner.Quest { return questmods.ErnestTheChicken() },
	// Pirate's Treasure (W13, part C): unproven, in the tail ahead of Dragon Slayer.
	questplan.PiratesTreasureID: func(map[string]interface{}) questrunner.Quest { return questmods.PiratesTreasure() },
	// Prince Ali Rescue (W13): unproven, in the tail ahead of Dragon Slayer.
	questplan.PrinceAliID: func(map[string]interface{}) questrunner.Quest { return questmods.PrinceAli() },
	// Black Knights' Fortress (W13): unproven, the last filler ahead of Dragon Slayer; wants 12 quest points.
	questplan.BlackKnightsFortressID: func(map[string]interface{}) questrunner.Quest { return questmods.BlackKnightsFortress() },
	// Doric's Quest and Gertrude's Cat (18 Oct 2026): one quest point each, unproven, after the larger fillers.
	questplan.DoricsQuestID:  func(map[string]interface{}) questrunner.Quest { return questmods.DoricsQuest() },
	questplan.GertrudesCatID: func(map[string]interface{}) questrunner.Quest { return questmods.GertrudesCat() },
	// Monk's Friend and the Clock Tower (W13): unproven, one quest point each, after Gertrude's Cat.
	questplan.MonksFriendID: func(map[string]interface{}) questrunner.Quest { return questmods.MonksFriend() },
	questplan.ClockTowerID:  func(map[string]interface{}) questrunner.Quest { return questmods.ClockTower() },
	// Demon Slayer (W13): unproven, in the tail ahead of Black Knights' Fortress and Dragon Slayer.
	questplan.DemonSlayerID: func(map[string]interface{}) questrunner.Quest { return questmods.DemonSlayer() },
	// Druidic Ritual and Jungle Potion (W22): unproven, in the tail ahead of Black Knights' Fortress and Dragon Slayer.
	questplan.DruidicRitualID: func(map[string]interface{}) questrunner.Quest { return questmods.DruidicRitual() },
	questplan.JunglePotionID:  func(map[string]interface{}) questrunner.Quest { return questmods.JunglePotion() },
	// Plague City (W22): unproven, one quest point, ahead of Black Knights' Fortress and the finale.
	questplan.PlagueCityID: func(map[string]interface{}) questrunner.Quest { return questmods.PlagueCity() },
	// Biohazard (W22): unproven, three quest points, the quest Plague City opens, ahead of Black Knights' Fortress and the finale.
	questplan.BiohazardID: func(map[string]interface{}) questrunner.Quest { return questmods.Biohazard() },
	// Tribal Totem and the Fishing Contest (W13): unproven, the last one-point fillers of the owner's ladder before Black Knights'
	// Fortress and Dragon Slayer.
	questplan.TribalTotemID:    func(map[string]interface{}) questrunner.Quest { return questmods.TribalTotem() },
	questplan.FishingContestID: func(map[string]interface{}) questrunner.Quest { return questmods.FishingContest() },
	// The Observatory Quest and the Sea Slug Quest (W13): unproven, two more fillers ahead of Black Knights' Fortress and Dragon Slayer.
	questplan.ObservatoryID: func(map[string]interface{}) questrunner.Quest { return questmods.Observatory() },
	questplan.SeaSlugID:     func(map[string]interface{}) questrunner.Quest { return questmods.SeaSlug() },
	// Sheep Herder and Dwarf Cannon (W13): unproven, five more quest points ahead of Black Knights' Fortress and Dragon Slayer.
	questplan.SheepHerderID: func(map[string]interface{}) questrunner.Quest { return questmods.SheepHerder() },
	questplan.DwarfCannonID: func(map[string]interface{}) questrunner.Quest { return questmods.DwarfCannon() },
	// Hazeel Cult and the Elemental Workshop (W13): unproven, one quest point each, after the Sea Slug Quest and ahead of Black Knights' Fortress.
	questplan.HazeelCultID:        func(map[string]interface{}) questrunner.Quest { return questmods.HazeelCult() },
	questplan.ElementalWorkshopID: func(map[string]interface{}) questrunner.Quest { return questmods.ElementalWorkshop() },
	// Lost City (W13): unproven, three quest points, ahead of Black Knights' Fortress and the finale; gated on the Tree spirit's odds in its own decide.
	questplan.LostCityID: func(map[string]interface{}) questrunner.Quest { return questmods.LostCity() },
	questplan.ArravID:    func(map[string]interface{}) questrunner.Quest { return questmods.ArravQuest() },
	// The Knight's Sword (W22): unproven, one quest point, the last filler ahead of Black Knights' Fortress and Dragon Slayer; the Asgarnian
	// Ice Dungeon is priced and refused below 60 hitpoints and combat 58.
	questplan.KnightsSwordID: func(map[string]interface{}) questrunner.Quest { return questmods.KnightsSword() },
	// The Digsite Quest (W13): unproven, two quest points, ahead of the Observatory Quest in the owner's ladder.
	questplan.DigsiteID: func(map[string]interface{}) questrunner.Quest { return questmods.Digsite() },
	// Tai Bwo Wannai Trio (W22): unproven, the last filler ahead of Black Knights' Fortress and Dragon Slayer.
	questplan.TaiBwoID: func(map[string]interface{}) questrunner.Quest { return questmods.TaiBwoTrio() },
	// The Watch Tower (W13): unproven, four quest points and 15,300 Magic experience, ahead of Black Knights' Fortress and Dragon Slayer.
	questplan.WatchtowerID: func(map[string]interface{}) questrunner.Quest { return questmods.Watchtower() },
	// The Tourist Trap (W22): unproven, two quest points, after the Elemental Workshop and ahead of Black Knights' Fortress.
	questplan.TouristTrapID: func(map[string]interface{}) questrunner.Quest { return questmods.TouristTrap() },
	// Big Chompy Bird Hunting (W22): unproven, two quest points, after the Elemental Workshop and ahead of Black Knights' Fortress.
	questplan.ChompyID: func(map[string]interface{}) questrunner.Quest { return questmods.BigChompy() },
	// Shades of Mort'ton (W22): unproven, three quest points, Herblore 15 and Crafting 20, through Mort Myre (Nature Spirit started) to the town and its temple.
	questplan.MorttonID: func(map[string]interface{}) questrunner.Quest { return questmods.MorttonShades() },
	questplan.RuneMysteriesID: func(map[string]interface{}) questrunner.Quest {
		return questrunner.Quest{Module: questplan.RuneMysteries}
	},
	// The Queen's Prayer (W17): the level kqPrayer holds at her, from the settings (37 Protect from Magic, 40
	// Missiles, or the gate's explicit floor), not a quest's gate. The tail's first rung (stage5QuestsKQPrayer);
	// prayer43 stays the Horror gate rung.
	questplan.KQPrayerRungID: func(m map[string]interface{}) questrunner.Quest {
		return questmods.CowsRung(questplan.KQPrayerRungID, "prayer", max(kqPrayerRungLevel(m), 1))
	},
}

// QuestFactoryFor is the factory for quest or rung id: a registered one (QuestFactories), else a generic training rung the
// trainer catalogue can build (docs/quest_selfsufficiency_plan_2026-10-19.md, 3.2 C, lane N2).
//
// A generic rung is named train:<skill>:<level> (questplan.TrainRungID, what a gate between two named rungs and a planned
// stint ask for) or <skill><level> (mining20, the named style); the catalogue row whose range reaches the level gives the
// rung family, questmods builds it, and the rung stops at that level: its Done is the base level. A level no row reaches has
// no factory, and QuestFactoryWhy says which gap names it. Before N2 the ids between the twelve named rungs had no factory at
// all (TestEveryHardGateHasARungThatProducesIt said "no factory"), the ladder left the rung out, and the quest behind the
// gate was held for want of a level nothing trained; the exact-level restriction (Crafting 18 and 40, never 19) is gone.
//
// Generic rungs are unproven: ProvenQuests does not list them, so only an account that allows unproven quests runs one.
// A generic Prayer rung draws its own bank and the cows and never asks the mule for bones (questplan.PrayerRungAsksMule
// names the two rungs that may: the mule's bones are one finite stock).
func QuestFactoryFor(id string) (func(settings map[string]interface{}) questrunner.Quest, bool) {
	f, _ := questFactory(id)
	return f, f != nil
}

// QuestFactoryWhy is QuestFactoryFor with the reason when there is none: an id that is no rung's, or the named gap that
// keeps a rung's level from being trained.
func QuestFactoryWhy(id string) (func(settings map[string]interface{}) questrunner.Quest, string) {
	return questFactory(id)
}

func questFactory(id string) (func(settings map[string]interface{}) questrunner.Quest, string) {
	id = strings.ToLower(strings.TrimSpace(id))
	if f, ok := QuestFactories[id]; ok {
		return f, ""
	}
	res, why := questplan.SharedCatalogue().ResolveRung(id)
	if res.ID == "" {
		return nil, why
	}
	// A row that names a family nobody built, or one that does not train its skill, promises a stint nothing can run.
	if skills, ok := questmods.RungFamilySkills(res.Trainer.Family); !ok || !slices.Contains(skills, res.Skill) {
		return nil, fmt.Sprintf("trainer %s names rung family %q, which does not train %s", res.Trainer.ID, res.Trainer.Family, res.Skill)
	}
	return func(map[string]interface{}) questrunner.Quest {
		q, _ := questmods.BuildRung(res.Trainer.Family, res.ID, res.Skill, res.Level)
		return q
	}, ""
}

// StintQuest builds the rung of a planned stint (questplan.Stint): the quest that trains t's skill to level under id. The
// row is the planner's own pick, so a generic id that two rows could reach is built with the row the stint was planned for
// and not the catalogue's static first. A registered rung (agility25) is its registered factory, so the named rungs run
// exactly as they always did.
func StintQuest(t questplan.TrainerSpec, id string, level int, settings map[string]interface{}) (questrunner.Quest, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if f, ok := QuestFactories[id]; ok {
		return f(settings), nil
	}
	if t.Via != questplan.ViaRung {
		return questrunner.Quest{}, fmt.Errorf("trainer %s is %s, not a rung: nothing to stint", t.ID, t.Via)
	}
	return questmods.BuildRung(t.Family, id, t.Skill, level)
}

func (b *questLadderBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil || ctx.Locs == nil || ctx.Config == nil || ctx.Components == nil {
		return fmt.Errorf("questladder: needs the collision pack, loc index, config and interface archive")
	}
	ctx.logf("questladder: build %s", BuildRevision())
	qs, err := SelectQuests(ctx.Settings)
	if err != nil {
		return err
	}
	b.quests = qs
	b.perQuest = time.Duration(IntSetting(ctx.Settings, "perQuestMinutes", 90)) * time.Minute
	// A quest's fights and its walks through hostile dungeons are expected combat: the
	// guardian's blind "in combat with no tracked event, flee" ran the first Waterfall lab
	// account up and down the Tree Gnome Village dungeon among ten hobgoblins until it died.
	// Real random events are still caught by their own detection.
	if ctx.Guardian != nil {
		ctx.Guardian.CombatExpected = true
	}
	b.finish = BoolSetting(ctx.Settings, "finishWhenDone", true)

	d := &questrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log, Loot: ctx.Loot,
	}
	if w, err := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log, Run: true,
	}); err != nil {
		ctx.logf("questladder: no webwalk (%v) — travel falls back to the same-level walker", err)
	} else {
		hops.Install(w, hops.New(hops.Deps{
			Client: w.Client, Session: ctx.Session, Config: ctx.Config, Components: ctx.Components, Log: ctx.Log,
		}))
		d.Walker = w
	}

	prov := &questrunner.Provisioner{Purpose: "quest", DefaultBank: waterfallplan.ArdougneBank,
		Disabled:        BoolSetting(ctx.Settings, "provisionDisabled", false),
		SourcesDisabled: BoolSetting(ctx.Settings, "provisionSourcesDisabled", false), Methods: questmods.SkillMethods()}
	b.budget = time.Duration(IntSetting(ctx.Settings, "budgetMinutes", 360)) * time.Minute
	if mule := strings.TrimSpace(StringSetting(ctx.Settings, "mule", "")); mule != "" {
		prov.Mule = &donorMuleAsker{ctx: ctx, d: d, mule: mule,
			post: fisherTileSetting(ctx.Settings, "mulePost", tile.Tile{X: 2655, Z: 3286})}
	}
	b.engine = &questrunner.Engine{D: d, Prov: prov, Account: ctx.Username, Build: BuildIdentity(), QuestFor: questRegistry(ctx.Settings)}

	dir := StringSetting(ctx.Settings, "storeDir", "data/scarabswarm-checkpoints")
	path, err := questrunner.StorePath(dir, ctx.Username)
	if err != nil {
		ctx.logf("questladder: %v — running without a side store", err)
		return nil
	}
	store, lerr := questrunner.LoadStore(path)
	if lerr != nil {
		ctx.logf("questladder: the quest store at %s: %v", path, lerr)
	}
	if store == nil {
		return fmt.Errorf("questladder: cannot open the quest store at %s: %v", path, lerr)
	}
	b.storeAt = path
	b.engine.Store = store
	b.engine.Save = func() {
		if err := questrunner.SaveStore(path, store); err != nil {
			ctx.logf("questladder: saving %s: %v", path, err)
		}
	}
	return nil
}

// ProvenQuests are the quests whose own lab EMPTY and CONTROL pair has passed review. Only they
// run by default: every other registered quest is inert code until its pair passes, because an
// unproven module can walk an account somewhere it dies (Merlin's Crystal crosses the
// Wilderness with a full pack). A quest is added here only when its pair passes review.
//
// PROMOTING A QUEST is two edits in one commit: add its id here, and add the same id, with the
// evidence (the passing EMPTY and failing CONTROL logs, the tip, the review), to provenEvidence
// in questladder_test.go. TestTheProvenSetMatchesItsRecordedEvidence fails until both agree, so
// a quest cannot be promoted without its proof being written down beside the test.
var ProvenQuests = map[string]bool{
	waterfallplan.QuestID:     true,
	questplan.WitchsPotionID:  true,
	questplan.VampireSlayerID: true,
	questplan.MurderMysteryID: true,
	questplan.PriestInPerilID: true,
	// The Restless Ghost (questplan.RestlessGhostID) is promoted on LIVE evidence with no lab control: it is
	// provision-free, so a control that fails for want of a kit cannot exist (the lead's ruling (b), 15 Oct
	// 2026). provenEvidence holds the numbers and the one reading they rest on: the rule's parks per run and
	// deaths hold from cd221b9b, the Wizards' Tower diagonal-door fix, not from the module's own last change.
	questplan.RestlessGhostID: true,
	// Rune Mysteries (questplan.RuneMysteriesID) is not an owner-ladder rung of its own: the Stage 5
	// ladder runs it just before Priest in Peril (ownerRungPrereqs), whose essence is mined through
	// Aubury's teleport and that teleport needs it. Proof: cmd/runemysteriestest, 21 Sep 2026, a fresh
	// account with cheats off, 4 of 4 checks (the essence gate observed SHUT first, the tab red to
	// green, quest points 0 to 1, the journal QUEST COMPLETE, and the same Aubury teleport then
	// landing in the essence mine); see provenEvidence.
	questplan.RuneMysteriesID: true,
	// Imp Catcher, Alfred Grimhand's Barcrawl and Cook's Assistant are promoted on LIVE evidence (the lead's rule:
	// 10+ completions on 10+ accounts since the module changed, 0 deaths, at most 0.5 parks per run), read by
	// cmd/questpromote on 16 Oct 2026; provenEvidence holds the numbers. Imp and Cook run under fleet-wide slots
	// (questrunner/slots.go): their beads and their egg, bucket and pot are shared spawns.
	questplan.ImpCatcherID:     true,
	questmods.BarcrawlID:       true,
	questplan.CooksAssistantID: true,
	// Sheep Shearer, Goblin Diplomacy and Romeo & Juliet are promoted on LIVE evidence (cmd/questpromote, 17 Oct 2026 06:50; the
	// numbers are in provenEvidence). Their shears and sheep, Goblin Village's goblins and the cadava bush are shared, so they run
	// under fleet-wide slots (questrunner/slots.go).
	questplan.SheepShearerID:    true,
	questplan.GoblinDiplomacyID: true,
	questplan.RomeoJulietID:     true,
	// Ernest the Chicken and Pirate's Treasure are promoted on LIVE evidence (cmd/questpromote, 18 Oct 2026 09:26; the numbers are
	// in provenEvidence). Their poison, fish food, spade, rubber tube and oil can, and Zambo's rum shelf, Thessalia's apron shelf and
	// the Falador gardener are shared, so they keep their fleet-wide slots (questrunner/slots.go: 6 and 8).
	questplan.ErnestChickenID:   true,
	questplan.PiratesTreasureID: true,
	// Monk's Friend, Demon Slayer, Druidic Ritual and Jungle Potion are promoted on LIVE evidence (cmd/questpromote, 19 Oct 2026 11:25; the
	// numbers are in provenEvidence). Monk's Friend's blanket, Demon Slayer's keys and Delrith, Druidic Ritual's animals and Jungle Potion's herbs
	// are shared, so they keep their fleet-wide slots (questrunner/slots.go: 4, 3, 4 and 3). Jungle Potion wants Druidic Ritual done, and
	// questplan.OwnerLadder runs Druidic Ritual first, so the proven head keeps druid ahead of junglepotion. The two longest slots of the
	// head are now theirs (Druidic Ritual 62m30s, Jungle Potion 75m), which is why questrunner.slotStale was raised past them.
	questplan.MonksFriendID:   true,
	questplan.DemonSlayerID:   true,
	questplan.DruidicRitualID: true,
	questplan.JunglePotionID:  true,
	// Witch's House (questplan.WitchsHouseID) was withdrawn on 29 September 2026 after live
	// accounts were trapped in the shed and died there (Acct225 at 05:46); its lab pair was
	// witch-empty-7 PASS 7/7 and witch-control-2 FAIL 5 of 5 on 5d8a7f5d. It comes back only
	// with a new pair that covers the shed.
}

// questRegistry is questrunner.Engine.QuestFor for the bots' engines: the registered quest built under id with the
// settings, so a ladder that does not list a prerequisite with no quest tab (the tail's Scorpion Catcher and Horror from
// the Deep, whose Barcrawl is in the proven head) reads it through that quest's own Done and not from a tab it does not
// have. Live 15-19 Oct 2026 it did not: "prerequisite quest not complete: barcrawl" held those two quests on 24 to 51
// accounts a day. The answer is a quest to read, never one to run: a ladder runs only the quests it was given.
func questRegistry(settings map[string]interface{}) func(id string) (questrunner.Quest, bool) {
	return func(id string) (questrunner.Quest, bool) {
		f, _ := QuestFactoryFor(id)
		if f == nil {
			return questrunner.Quest{}, false
		}
		return f(settings), true
	}
}

// SelectQuests builds the configured quests (setting "quests", default Waterfall), refusing
// any id that is not registered, and any unproven one unless "allowUnproven" is true (the lab
// harness's own proofs).
func SelectQuests(settings map[string]interface{}) ([]questrunner.Quest, error) {
	ids, err := questIDSetting(settings, "quests")
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		ids = []string{waterfallplan.QuestID}
	}
	allow := BoolSetting(settings, "allowUnproven", false)
	var qs []questrunner.Quest
	for _, raw := range ids {
		id := strings.ToLower(strings.TrimSpace(raw))
		f, why := QuestFactoryWhy(id)
		if f == nil {
			if _, _, rung := questplan.ParseRungID(id); rung && why != "" {
				return nil, fmt.Errorf("questladder: no rung %q: %s", raw, why)
			}
			return nil, fmt.Errorf("questladder: no quest %q (have %v)", raw, questFactoryIDs())
		}
		if !ProvenQuests[id] && !allow {
			return nil, fmt.Errorf("questladder: quest %q has not passed its lab proof; set allowUnproven to run it anyway", raw)
		}
		qs = append(qs, f(settings))
	}
	return qs, nil
}

// questIDSetting reads a list of quest ids as written. It is not stringListSetting: that one
// normalises PLAYER names and drops anything longer than a name may be, which silently turned
// quests: ["woodcutting40"] into an empty list, and so into the Waterfall default.
func questIDSetting(m map[string]interface{}, key string) ([]string, error) {
	var raw []string
	switch v := m[key].(type) {
	case nil:
		return nil, nil
	case string:
		raw = strings.Split(v, ",")
	case []string:
		raw = v
	case []interface{}:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("questladder: %s holds a non-string %v", key, item)
			}
			raw = append(raw, s)
		}
	default:
		return nil, fmt.Errorf("questladder: %s must be a list of quest ids", key)
	}
	var out []string
	for _, s := range raw {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

func questFactoryIDs() []string {
	out := make([]string, 0, len(QuestFactories))
	for id := range QuestFactories {
		out = append(out, id)
	}
	return out
}

func (b *questLadderBot) Loop(ctx *Context) error {
	if b.done {
		if b.finish {
			ctx.RequestFinish("questladder: every quest in the ladder is complete")
		}
		ctx.Delay(30 * time.Second)
		return nil
	}
	res := b.engine.RunLadder(b.quests, b.perQuest, b.budget)
	ctx.logf("questladder: ladder %s; complete %v; blocked %v", res.Outcome, res.Completed, res.Blocked)
	switch res.Outcome {
	case questrunner.LadderDone:
		b.done = true
	case questrunner.LadderStuck:
		// Everything left is parked or blocked: wait out the shortest backoff.
		ctx.Delay(2 * time.Minute)
	}
	return nil
}

func (b *questLadderBot) OnStop(ctx *Context) {
	if b.engine != nil && b.engine.Save != nil {
		b.engine.Save()
	}
}

// ---------------------------------------------------------------------------
// The mule, through the engine's one door
// ---------------------------------------------------------------------------

// donorMuleAsker adapts this fleet's existing mule plumbing to questrunner.MuleAsker: the
// published bank from the peer directory, and one ask per line through donorrunner.Await (the
// per-item order AIO-PLAN A7 keeps as the fallback until the bundled kit order lands). It is the
// standalone bot's adapter; ScarabSwarm will pass its own single mule-ask entry instead.
type donorMuleAsker struct {
	ctx  *Context
	d    *questrunner.Deps
	mule string
	post tile.Tile
}

// Published reads the mule's last bank publication, by object id. A missing or stale row is
// "not known", never "empty".
func (m *donorMuleAsker) Published() (map[int]int, bool) {
	ctx := m.ctx
	if ctx.Peers == nil {
		return nil, false
	}
	now := time.Now()
	for _, e := range ctx.Peers.Entries(now) {
		if !strings.EqualFold(strings.TrimSpace(e.Account), m.mule) {
			continue
		}
		if e.UpdatedAt.IsZero() || now.Sub(e.UpdatedAt) > peerplan.DefaultMaxAge {
			return nil, false
		}
		return holdingsByID(ctx, e.Bank), true
	}
	return nil, false
}

func holdingsByID(ctx *Context, hs []peerbank.Holding) map[int]int {
	out := map[int]int{}
	// A row per obj: the merged row of a name several objs share (bows, grimy herbs) is those rows
	// added up, and summing it as well counted every one of them twice.
	for _, h := range peerrunner.ObjRows(hs) {
		if h.Count <= 0 {
			continue
		}
		for _, id := range ctx.Config.ObjIDs(h.Item) {
			if o, ok := ctx.Config.Obj(id); ok && !o.Noted() {
				out[id] += h.Count
				break
			}
		}
	}
	return out
}

// Ask walks to the mule's post and asks for each line in turn, stopping a line at its first
// decline or timeout, and returns what arrived.
func (m *donorMuleAsker) Ask(purpose string, lines []questplan.ItemCount) (map[int]int, string) {
	ctx, d := m.ctx, m.d
	got := map[int]int{}
	if ctx.Bus == nil {
		return got, "no wall bus configured"
	}
	if !questrunner.Travel(d, m.post, 4) {
		return got, fmt.Sprintf("could not reach the mule post at %d,%d", m.post.X, m.post.Z)
	}
	dd := &donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: donorrunner.Delayer(ctx.Delay), Log: ctx.Log,
		Bus: ctx.Bus, Me: ctx.Username,
	}
	var reasons []string
	for _, l := range lines {
		name := questrunner.ObjName(d, l.ID)
		before := questrunner.HeldID(d, l.ID)
		ans := donorrunner.Await(dd, donorrunner.AwaitOptions{Mule: m.mule, Me: ctx.Username, Item: name, Qty: l.Count, Bus: ctx.Bus, Timeout: 3 * time.Minute})
		if n := questrunner.HeldID(d, l.ID) - before; n > 0 {
			got[l.ID] += n
		}
		if ans.Stopped {
			reasons = append(reasons, "stopped")
			break
		}
		if got[l.ID] < l.Count {
			reasons = append(reasons, fmt.Sprintf("%s: %s", name, ans.Reason))
		}
	}
	ctx.logf("questladder: %s asked %s for %d line(s), got %v", purpose, m.mule, len(lines), got)
	return got, strings.Join(reasons, "; ")
}
