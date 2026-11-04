package questplan

// The Stage 5 quest ladder's facts, one row per rung, from the studies (lane Q,
// private evidence notes and the quest-chain studies in
// private evidence notes and the Waterfall study
// wf_d9202501-7a2__study.json). XP is 1x: content's stat_advance divided by ten. Every gate is
// labelled with how binding it is; a gate the content does not enforce is GateAdvised (it defers
// a rung, never inserts training), except the owner's own Waterfall-at-20-Hitpoints rule.

// OwnerLadder is the owner's order (2026-09-23 ~11:10-11:15), by rung id. Goblin Diplomacy, Romeo & Juliet and the fillers built
// after them (Ernest the Chicken, Pirate's Treasure, Prince Ali Rescue, Doric's Quest, Gertrude's Cat, Monk's Friend, the Clock
// Tower, Demon Slayer, Druidic Ritual, Jungle Potion, Tai Bwo Wannai Trio, Plague City, Biohazard, Tribal Totem, the Fishing Contest, the Observatory Quest, the Sea Slug Quest, Sheep Herder, Dwarf Cannon, Hazeel Cult, the Elemental Workshop and Black Knights' Fortress) are quest-point fillers (W13 of docs/quest_plan_2026-10-15.md, built 15-18 Oct 2026): the
// planner (PlanLadder) inserts one when a quest wants more quest points than the plan has, and the live ladder
// runs them as the last stops before Dragon Slayer, which wants 32. They are unproven, so only an account
// that allows unproven quests reaches them, in the tail. Dragon Slayer itself is HELD (HeldRungs): the fillers
// would carry accounts to its 32 quest points before W19 has hardened a module that has never run live, so the
// ladder lists it, plans it, and does not start it.
var OwnerLadder = []string{
	"waterfall", "merlin", "grail", "witch", "arena", "ghost", "pip", "spirit", "hetty", "imp",
	"treegnome", "vampire", "agility25", "grandtree", "scorpion", "deathplateau", "agility35",
	"barcrawl", "horror", "murder", "crafting40", "fremennik",
	// The short quest-point fillers that are built (W13 of docs/quest_plan_2026-10-15.md): Cook's Assistant and Sheep
	// Shearer, one point each, then Goblin Diplomacy and Romeo & Juliet, and the fillers below them, ahead of the finale whose 32
	// quest points they help to make up. They sit in the unproven tail with everything else that is not in ProvenQuests.
	"cook", "sheep", "gobdip", "romeo",
	// Ernest the Chicken (4 quest points, W13 of docs/quest_plan_2026-10-15.md), then Pirate's Treasure (2) and Prince Ali
	// Rescue (3): all ahead of the finale. Ernest and Pirate's Treasure were promoted on live evidence on 18 Oct 2026.
	"haunted", "hunt", "prince",
	// Doric's Quest and Gertrude's Cat (W13 of docs/quest_plan_2026-10-15.md, built 18 Oct 2026): one quest point each, no gate and no
	// fight, so they run after the larger fillers and before the finale like the other unproven stops.
	"doric", "fluffs",
	// Monk's Friend and the Clock Tower (one quest point each, W13 of docs/quest_plan_2026-10-15.md): the one-point fillers after
	// those. Monk's Friend was promoted on live evidence on 19 Oct 2026; the Clock Tower is unproven like the tail it joins.
	"drunkmonk", "cog",
	// Demon Slayer (3 quest points, W13 of docs/quest_plan_2026-10-15.md, built 18 Oct 2026): the bones, the three keys, Silverlight and
	// Delrith's stone circle. The circle is single-way combat ringed by dark wizards that hunt every account of combat 41 and under, and
	// a hunted account cannot start the fight, so it is not begun below combat 42 (questplan.DemonMinCombat, DemonHuntFit); above that
	// the visit is gated on hitpoints, swing and food (DemonFit). Promoted on live evidence on 19 Oct 2026, and ahead of Black Knights'
	// Fortress, whose 12 points it helps to make.
	"demon",
	// Druidic Ritual (4 quest points, 250 Herblore XP: Herblore 3) and Jungle Potion, which wants it and Herblore 3 (1 quest point):
	// W13/W22 of docs/quest_plan_2026-10-15.md, both promoted on live evidence on 19 Oct 2026. Jungle Potion follows Druidic Ritual at
	// once (the proven head keeps that order), and both come ahead of Black Knights' Fortress, whose 12 quest points they help to make.
	"druid", "junglepotion",
	// Tai Bwo Wannai Trio (2 quest points, W22 of docs/quest_plan_2026-10-15.md, built 19 Oct 2026): at once after Jungle Potion, which
	// Timfraku wants done. Four brothers' errands on Karamja (Karambwanji, a vessel, a spear and four doses of agility potion for a
	// cutscene hunt, a rum, a monkey and a Jogre's bones); the one fight is a Jogre, priced before the account leaves the mainland.
	// Unproven, like the tail it joins, and parked (with the reason) until the mule holds agility potions and a spear.
	"tbwt",
	// Plague City (one quest point, W22 of docs/quest_plan_2026-10-15.md, built 18 Oct 2026): the longest of the one-point quests,
	// after the other one-pointers so that a quest that fails on its shopping or its two dangerous legs costs the tail nothing ahead
	// of it, and before Black Knights' Fortress, whose twelve quest points it helps to make up. Unproven, like the tail it joins.
	"elena",
	// Biohazard (three quest points, W22 of docs/quest_plan_2026-10-15.md, built 19 Oct 2026): the quest Plague City opens, at once
	// after it. Two crossings of the plague wall, one level 13 sick mourner (priced, and the quest parks when the price says no), a
	// chemist's errand to Rimmington and Varrock and four ship fares: the longest of the fillers, so it follows the one-pointers it
	// does not depend on and stays ahead of Black Knights' Fortress, whose twelve quest points it helps to make. Unproven, like the
	// tail it joins.
	"biohazard",
	// Tribal Totem and the Fishing Contest (one quest point each, W13 of docs/quest_plan_2026-10-15.md): the last one-point fillers, behind
	// Thieving 21 and Fishing 10 (their records state the levels), unproven like the tail they join.
	"totem", "fishingcompo",
	// The Digsite Quest (2 quest points, 15,300 Mining and 2,000 Herblore experience, W13 of docs/quest_plan_2026-10-15.md): the longest of the
	// fillers, behind Thieving 25, Agility 10 and Herblore 10 (its record states the levels), and after Druidic Ritual and the Jungle Potion that
	// pay the Herblore towards them. Placed between the Fishing Contest and the Observatory Quest, unproven like the tail it joins.
	"itexam",
	// The Observatory Quest (2 quest points, a goblin guard of level 42 priced before the dungeon) and the Sea Slug Quest (1 quest point,
	// Firemaking 30): the last two fillers that gate nothing on the quests above them, unproven like the tail they join.
	"itgronigen", "seaslug",
	// Sheep Herder (4 quest points: no fight, a herd of four sheep by prodding, one account at a time) and Dwarf Cannon (1 quest point: six
	// railings by a Crafting roll, the watchtower, the goblins' cave and the cannon's four components; the giant bats near the camp and
	// the cave are priced), W13 of docs/quest_plan_2026-10-15.md, unproven like the tail they join and ahead of Black Knights' Fortress,
	// whose 12 quest points they help to make.
	"sheepherder", "mcannon",
	// Hazeel Cult (1 quest point, the cult's side: no monster is fought) and the Elemental Workshop (1 quest point, Mining, Smithing and
	// Crafting 20 on its record, one earth elemental priced before the descent): W13 of docs/quest_plan_2026-10-15.md, built 19 Oct 2026,
	// ahead of Black Knights' Fortress like the other fillers and unproven like the tail they join.
	"hazeelcult", "elemental_workshop",
	// Black Knights' Fortress (3 quest points, W13 of docs/quest_plan_2026-10-15.md): the filler that wants 12 of them, last in
	// line so that the proven quests and the fillers above have paid theirs. Unproven, like the tail it joins.
	"blackknight",
	"dragon",
}

// KQPrayerRungID is the Prayer rung that trains to the Queen's protection prayer (Protect from Magic,
// Prayer 37; Missiles 40), not to a quest's gate: the Kalphite Queen gate (kqGate) wants that level on
// every account, and the 43 of the prayer43 rung is ~50,000 xp of cow bones that no account reached
// (best 38, 15 Oct 2026). The Stage 5 ladder's tail starts with it (stage5QuestsKQPrayer), so an account
// below the level the Queen needs trains to THAT first; prayer43 is only ever a gate rung, inserted in
// front of Horror from the Deep (GateModule) by the "owner" expansion, never a training target of its own.
// Its level here is Protect from Magic's (the default kqPrayer); the factory takes the live one from the
// settings (internal/bot, kqPrayerRungLevel).
const KQPrayerRungID = "prayerkq"

// Object ids the facts' waivers name.
const (
	ObjRuneEssence  = 1436
	ObjNails        = 1539
	ObjSilverSickle = 2961
)

// LadderFacts is every rung's row.
var LadderFacts = map[string]RungFacts{
	"waterfall": {ID: "waterfall", Name: "Waterfall Quest", QP: 1,
		Gates: []Gate{{Skill: "hitpoints", Level: 20, Kind: GateOwner, Note: "the owner's rule: once we hit 20 Hitpoints"}},
		XP:    map[string]int{"attack": 13750, "strength": 13750}, Region: "Baxtorian Falls / East Ardougne", EstMinutes: 40,
		Source: "journals/wf_d9202501-7a2__study.json",
		Notes:  "fire-giant lane crossed twice at 20 HP; the 20-damage statue is gated by the pillar ledger"},
	"merlin": {ID: "merlin", Name: "Merlin's Crystal", QP: 6, MinCombat: 30,
		Region: "Camelot / Keep Le Faye / Catherby", EstMinutes: 45,
		Source: "journals/wf_240af715-c0f__study_merlin.json", Notes: "no XP; the gate to The Holy Grail and Excalibur; Sir Mordred is level 39"},
	"grail": {ID: "grail", Name: "The Holy Grail", QP: 2, Prereqs: []string{"merlin"}, MinCombat: 30,
		Gates:  []Gate{{Skill: "attack", Level: 20, Kind: GateServer, Note: "wield Excalibur for the Titan (levelrequire tier20.rs2)"}},
		XP:     map[string]int{"prayer": 11000, "defence": 15300},
		Region: "Camelot / Entrana / the Fisher Realm", EstMinutes: 60, Source: "journals/wf_240af715-c0f__study_grail.json",
		Notes: "Entrana forbids weapons and armour; the Black Knight Titan (120) dies only to Excalibur"},
	"witch": {ID: "witch", Name: "Witch's House", QP: 4, MinCombat: 35,
		XP:     map[string]int{"hitpoints": 6325},
		Region: "Taverley", EstMinutes: 40, Source: "journals/wf_240af715-c0f__study_witch.json",
		Notes: "four experiment forms, 144 hitpoints in all; leather gloves worn"},
	"arena": {ID: "arena", Name: "Fight Arena", QP: 2, MinCombat: 30,
		XP:     map[string]int{"attack": 12175, "thieving": 2175},
		Region: "East Ardougne / Khazard arena", EstMinutes: 45, Source: "journals/wf_240af715-c0f__study_arena.json",
		Notes: "the caster tactic: Fire Strike or Wind Bolt and flee from General Khazard"},
	"ghost": {ID: "ghost", Name: "The Restless Ghost", QP: 1,
		XP:     map[string]int{"prayer": 1125},
		Region: "Lumbridge / Lumbridge Swamp / Wizards' Tower", EstMinutes: 20, Source: "journals/wf_240af715-c0f__study_ghost.json",
		Notes: "the Ghostspeak amulet must survive into Nature Spirit"},
	"pip": {ID: "pip", Name: "Priest in Peril", QP: 1, MinCombat: 20,
		SoftPrereqs: []SoftPrereq{{ID: "runemysteries", WaiverItem: ObjRuneEssence, WaiverQty: 50,
			Note: "50 unnoted rune essence; mining them needs the Rune Mysteries teleport"}},
		XP:     map[string]int{"prayer": 1406},
		Region: "Varrock / Paterdomus", EstMinutes: 40, Source: "journals/wf_240af715-c0f__study_pip.json"},
	"runemysteries": {ID: "runemysteries", Name: "Rune Mysteries", QP: 1,
		Region: "Lumbridge / Wizards' Tower / Varrock", EstMinutes: 10, Source: "internal/scripts/questplan/runemysteries.go (proven live 2026-09-21)"},
	"spirit": {ID: "spirit", Name: "Nature Spirit", QP: 2, Prereqs: []string{"pip", "ghost"}, MinCombat: 25,
		Gates: []Gate{{Skill: "crafting", Level: 18, Kind: GateItem, Boostable: true, WaiverItem: ObjSilverSickle, WaiverQty: 1,
			Note: "the silver sickle is cast from a silver bar and the sickle mould"}},
		XP:     map[string]int{"crafting": 3000, "hitpoints": 2000, "defence": 2000},
		Region: "Paterdomus / Mort Myre", EstMinutes: 75, Source: "laneq/studies/spirit.md",
		Notes: "server checks Priest in Peril at stage 61 and a WORN Ghostspeak amulet; the druid pouch is never re-issued"},
	"hetty": {ID: "hetty", Name: "Witch's Potion", QP: 1,
		XP:     map[string]int{"magic": 325},
		Region: "Rimmington", EstMinutes: 25, Source: "journals/wf_240af715-c0f__study_hetty.json",
		Notes: "the study's 15 minutes held no walk: live 29 Sep - 2 Oct, 109 runs from a fresh start, 38 completed, 53 timed out at 20 minutes (26 of them still on the \"talk to Hetty\" step, which opens with the walk), 14 parked"},
	"imp": {ID: "imp", Name: "Imp Catcher", QP: 1,
		XP:     map[string]int{"magic": 875},
		Region: "Wizards' Tower + any imp spawn", EstMinutes: 45, Source: "laneq/studies/imp.md",
		Notes: "each bead drops 5/128 from an imp; about 53 kills on average"},
	"treegnome": {ID: "treegnome", Name: "Tree Gnome Village", QP: 2, MinCombat: 40,
		XP:     map[string]int{"attack": 11450},
		Region: "Tree Gnome Village / Khazard battlefield", EstMinutes: 35, Source: "laneq/studies/treegnome.md",
		Notes: "the Khazard Warlord (112, max 9) has magic defence 0: magic hits about 87%"},
	"vampire": {ID: "vampire", Name: "Vampire Slayer", QP: 3, MinCombat: 20,
		XP:     map[string]int{"attack": 4825},
		Region: "Draynor Village / Varrock", EstMinutes: 30, Source: "laneq/studies/vampire.md",
		Notes: "the killing blow needs a stake and a hammer in the pack. The study's 20 minutes held no walk: live 29 Sep - 2 Oct, 84 runs from a fresh start, 29 completed, 42 timed out at 20 minutes (33 of them in the Count fight)"},
	"agility25": {ID: "agility25", Name: "Gnome Stronghold agility to 25", TrainSkill: "agility", TrainLevel: 25,
		Region: "Gnome Stronghold", EstMinutes: 60, Source: "laneq/studies/agility25.md",
		Notes: "86.5 xp a lap, no failures, no damage; 91 laps at 1x"},
	"grandtree": {ID: "grandtree", Name: "The Grand Tree", QP: 5, MinCombat: 60,
		Gates:  []Gate{{Skill: "agility", Level: 25, Kind: GateServer, Boostable: true, Note: "the Tree climb at stage 120 checks the current level"}},
		XP:     map[string]int{"agility": 7900, "attack": 18400, "magic": 2150},
		Region: "Gnome Stronghold / Yanille / Karamja", EstMinutes: 75, Source: "laneq/studies/grandtree.md",
		Notes: "the Black Demon (172, max 16) has no known safespot"},
	"scorpion": {ID: "scorpion", Name: "Scorpion Catcher", QP: 1, Prereqs: []string{"barcrawl"}, MinCombat: 45,
		Gates:  []Gate{{Skill: "prayer", Level: 31, Kind: GateServer, Note: "the Edgeville Monastery upstairs (base level, not boostable)"}},
		XP:     map[string]int{"strength": 6625},
		Region: "Sorcerer's Tower / Barbarian Outpost / Monastery / Taverley dungeon", EstMinutes: 90, Source: "laneq/studies/scorpion.md",
		Notes: "scorpion B is inside the Barbarian Outpost (the Barcrawl gate); scorpion A is past the blue dragons"},
	"deathplateau": {ID: "deathplateau", Name: "Death Plateau", QP: 1, MinCombat: 20,
		XP:     map[string]int{"attack": 3000},
		Region: "Burthorpe", EstMinutes: 35, Source: "laneq/studies/deathplateau.md",
		Notes: "the quest varp is %death_equiproom (314); about 1,515 coins covers the dice; Tenzing wants ten cooked trout that nothing in the fleet supplies (the quest parks a day at that step), ten loaves, and a pair of spiked boots (a spare from the bank or the mule, else a bar); every NPC re-issue counts the bank, so a banked paper is withdrawn, never asked for"},
	"agility35": {ID: "agility35", Name: "Gnome Stronghold agility to 35", TrainSkill: "agility", TrainLevel: 35,
		Region: "Gnome Stronghold", EstMinutes: 165, Source: "laneq/studies/agility35.md",
		Notes: "the Barbarian course itself needs 35, so 25-35 is the gnome course"},
	"barcrawl": {ID: "barcrawl", Name: "Alfred Grimhand's Barcrawl", QP: 0,
		Region: "Asgarnia / Misthalin / Karamja / Kandarin", EstMinutes: 45, Source: "laneq/studies/agility35.md; internal/scripts/barcrawlplan (proven live)",
		Notes: "about 800 coins; seven bars drain Attack and Defence"},
	"horror": {ID: "horror", Name: "Horror from the Deep", QP: 2, Prereqs: []string{"barcrawl"}, MinCombat: 50,
		Gates: []Gate{
			{Skill: "agility", Level: 35, Kind: GateServer, Note: "the lighthouse key comes from Gunnjorn inside the Barbarian course, whose pipe needs 35"},
			{Skill: "smithing", Level: 34, Kind: GateItem, WaiverItem: ObjNails, WaiverQty: 8, Note: "nails: 2 per steel bar, no shop sells them"},
			{Skill: "prayer", Level: 43, Kind: GateModule, Note: "Protect from Missiles (40): the Dagannoth Mother's ranged attack never rolls for accuracy; the module waits at stage 5 below 40, and the rung trains to 43, which also zeroes the junior"},
		},
		XP:     map[string]int{"magic": 4662, "strength": 4662, "ranged": 4662},
		Region: "Lighthouse north of the Barbarian Outpost", EstMinutes: 90, Source: "laneq/studies/horror.md"},
	"murder": {ID: "murder", Name: "Murder Mystery", QP: 3,
		XP:     map[string]int{"crafting": 1406},
		Region: "Sinclair Mansion, north of Seers' Village", EstMinutes: 20, Source: "laneq/studies/murder.md",
		Notes: "the culprit is rolled 1-6 at the start; thread colour then fingerprints"},
	"cook": {ID: "cook", Name: "Cook's Assistant", QP: 1, Filler: true,
		XP:     map[string]int{"cooking": 300},
		Region: "Lumbridge castle / the farm north-east of it / the windmill", EstMinutes: 25,
		Source: "defs/cooksassistant.ts; content quest_cook/scripts/quest_cook.rs2",
		Notes:  "an egg, a bucket of milk and a pot of flour in the pack; the flour is Wydin's shelf for an account nearer Port Sarim than the Cook and the windmill otherwise; no combat"},
	"sheep": {ID: "sheep", Name: "Sheep Shearer", QP: 1, Filler: true,
		XP:     map[string]int{"crafting": 150},
		Region: "Lumbridge farm / the castle's spinning wheel / East Ardougne", EstMinutes: 25,
		Source: "defs/sheepshearer.ts; content fred_the_farmer.rs2 and quest_sheep.rs2",
		Notes:  "20 balls of wool, taken one at a time by Fred; Aemad sells them in East Ardougne (30 at 2 gp), else shears and the castle wheel; no combat"},
	"crafting40": {ID: "crafting40", Name: "Crafting to 40 (spin flax at Seers)", TrainSkill: "crafting", TrainLevel: 40,
		Region: "Seers' Village", EstMinutes: 190, Source: "laneq/studies/crafting40.md",
		Notes: "drive crafterrunner's Seers functions directly, not ProgressiveCrafter's Loop"},
	"cows20": {ID: "cows20", Name: "Hitpoints to 20 (Stage 5 cows20 rung)", TrainSkill: "hitpoints", TrainLevel: 20,
		Region: "Lumbridge / East Ardougne cow pens", Source: "AIO-PLAN A6 rung 1 (the existing Cow code)",
		Notes: "the owner's Waterfall gate: once we hit 20 Hitpoints"},
	"crafting18": {ID: "crafting18", Name: "Crafting to 18 (Nature Spirit's silver sickle)", TrainSkill: "crafting", TrainLevel: 18,
		Region: "Seers' Village", Source: "laneq/studies/spirit.md; crafterrunner's Seers flax spinning",
		Notes: "waived when the bank or the mule holds a Silver sickle (2961)"},
	"prayer31": {ID: "prayer31", Name: "Prayer to 31 (bury big bones)", TrainSkill: "prayer", TrainLevel: 31,
		Region: "Hill Giants (the giants rung) or any big-bones source", Source: "laneq/studies/scorpion.md; scarabplan boneyard",
		Notes: "Grail + Restless Ghost + Priest in Peril leave 13,531 XP (Prayer 30); 31 is 14,833: about 87 big bones"},
	"attack20": {ID: "attack20", Name: "Attack to 20 (Excalibur for the Grail's Titan)", TrainSkill: "attack", TrainLevel: 20,
		Region: "Lumbridge cows", Source: "journals/wf_240af715-c0f__study_grail.json",
		Notes: "normally never inserted: Waterfall's 13,750 Attack XP clears it first"},
	KQPrayerRungID: {ID: KQPrayerRungID, Name: "Prayer to the Queen's protection prayer (kqPrayer)", TrainSkill: "prayer", TrainLevel: 37,
		Region: "Lumbridge cows (bones) or any bones source", Source: "a user request (kqGate: correct prayer level); W17 of D:/gotmp/quest_plan.md",
		Notes: "37 is Protect from Magic (kqPrayer's default); Protect from Missiles is 40. The tail's first rung: an account at or above it reads done at once, and no quest's gate names this level, so it is never a gate rung"},
	"prayer43": {ID: "prayer43", Name: "Prayer to 43 (Horror's dagannoth mother)", TrainSkill: "prayer", TrainLevel: 43,
		Region: "Lumbridge cows (bones) or any big-bones source", Source: "laneq/studies/horror.md",
		Notes: "not server-enforced: the mother's ranged volley (0-24, no accuracy roll) is zeroed only by Protect from Missiles (40); 43 also zeroes the junior. The Horror module waits at stage 5 below 40, and Horror's GateModule makes the planner insert this rung"},
	"smithing34": {ID: "smithing34", Name: "Smithing to 34 (then smith nails from steel bars)", TrainSkill: "smithing", TrainLevel: 34,
		Region: "any anvil near a bank (Varrock west, Falador)", Source: "laneq/studies/horror.md, laneq/studies/dragon.md",
		Notes: "nails have no shop, spawn or drop; 2 per steel bar; the mule holds 756 steel bars and 0 nails; waived by 20 owned nails"},
	"woodcutting40": {ID: "woodcutting40", Name: "Woodcutting to 40", TrainSkill: "woodcutting", TrainLevel: 40,
		Region: "Seers' Village", EstMinutes: 120, Source: "laneq/studies/crafting40.md (ProgressiveWoodcutter)"},
	"fletching25": {ID: "fletching25", Name: "Fletching to 25", TrainSkill: "fletching", TrainLevel: 25,
		Region: "Seers' Village", EstMinutes: 45, Source: "laneq/studies/crafting40.md (BankFletcher)"},
	"fremennik": {ID: "fremennik", Name: "The Fremennik Trials", QP: 3, MinCombat: 35,
		Gates: []Gate{
			{Skill: "woodcutting", Level: 40, Kind: GateServer, Boostable: true, Note: "Olaf's lyre"},
			{Skill: "crafting", Level: 40, Kind: GateServer, Boostable: true, Note: "Olaf's lyre"},
			{Skill: "fletching", Level: 25, Kind: GateServer, Boostable: true, Note: "Olaf's lyre"},
			// Not the content's: the way to Rellekka. The walker keeps an account off the overland road (the wolves
			// shut it to combat 64 and under), so the way is the basalt rocks, and their northbound dice rock (spot 3,
			// loc 4552) keeps the graph's Agility 35 (webwalk/basalt.go). Below it every walk to Rellekka and to the
			// swaying tree ends "unreachable" (17 parks, 14 Oct 2026).
			{Skill: "agility", Level: 35, Kind: GateModule, Note: "the basalt rocks to Rellekka"},
		},
		XP: map[string]int{"attack": 2812, "defence": 2812, "strength": 2812, "hitpoints": 2812, "woodcutting": 2812,
			"fletching": 2812, "fishing": 2812, "crafting": 2812, "agility": 2812, "thieving": 2812},
		Region: "Rellekka", EstMinutes: 150, Source: "laneq/studies/fremennik.md",
		Notes: "Koschei is fought with no weapons, armour or runes"},
	"dragon": {ID: "dragon", Name: "Dragon Slayer", QP: 2, QPNeeded: 32, MinCombat: 45, Finale: true,
		Gates:  []Gate{{Skill: "smithing", Level: 34, Kind: GateItem, Boostable: true, WaiverItem: ObjNails, WaiverQty: 12, Note: "12 nails for the Lady Lumbridge's hull"}},
		XP:     map[string]int{"strength": 18650, "defence": 18650},
		Region: "Champions' Guild / Port Sarim / Crandor", EstMinutes: 120, Source: "laneq/studies/dragon.md",
		Notes: "Elvarg (83): the anti-dragon shield caps her fire at 10; a Stage 5 caster cannot realistically kill her"},
	"gobdip": {ID: "gobdip", Name: "Goblin Diplomacy", QP: 5, Filler: true,
		XP:     map[string]int{"crafting": 200},
		Region: "Port Sarim / Draynor / Falador park / the Goblin Village", EstMinutes: 40,
		Source: "design ruling (QP filler); questplan/goblindiplomacy.go (content quest_gobdip, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "three goblin mail, two of them dyed by Aggie (orange from a red and a yellow dye, and blue); the mail is farmed last, in the village the generals stand in (10/128 a kill); the redberries are ground spawns by the Varrock berry patch or Wydin's one-a-minute shelf"},
	"haunted": {ID: "haunted", Name: "Ernest the Chicken", QP: 4, Filler: true,
		Region: "Draynor Manor and the garden, fountain and compost heap round it", EstMinutes: 40,
		Source: "defs/ernest/*.ts; questplan/ernestchicken.go and ernestmaze.go (content quest_haunted, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "three parts for Professor Oddenstein: the oil can behind the basement's six-lever, nine-door maze (solved by search, 23 moves; the levers reset on the ladder up, not the way down), the gauge from the fountain whose piranhas a poisoned fish food kills (searched first: the varp is unreadable), the rubber tube from a sealed closet (compost-heap key, a spade, a skeleton of level 25 beside the tube); the front doors let nobody out, ten garden trees hit for 3"},
	"hunt": {ID: "hunt", Name: "Pirate's Treasure", QP: 2, Filler: true,
		Region: "Port Sarim / the Karamja banana plantation / Varrock / Falador park", EstMinutes: 45,
		Source: "defs/piratestreasure/*.ts; content quest_hunt/scripts (W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "the rum is bought from Zambo on Karamja and smuggled home in the plantation crate (stashed, ten bananas packed, shipped by Luthas, collected from Wydin's back room in a white apron), because the customs officer takes any rum from the pack; then Frank's key, the Blue Moon chest, and a spade in Falador park under a level 3 gardener; 2 quest points and no experience"},
	"prince": {ID: PrinceAliID, Name: PrinceAliName, QP: 3, Filler: true, MinCombat: 30,
		Region: "Al-Kharid / Draynor / Port Sarim / Rimmington / Lumbridge / Varrock", EstMinutes: 60,
		Source: "design ruling (QP filler); questplan/princeali.go (content quest_prince, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "Hassan and Osman in Al-Kharid, then a disguise (a blonde wig from three balls of wool and a yellow dye, a pink skirt, skin paste from flour, a redberry, water and ashes burned from logs), the key (a print of Keli's in soft clay, forged by Osman from a bronze bar, handed over by Leela), three beers for Joe and a rope for Keli; the mule keeps the bar, the skirt, the beers, the balls of wool, the logs and the tinderbox; four level-26 Jail guards (aggressive at every level, hits up to 3) stand round the compound, so each of the three visits (Keli's print, Joe's beers, the break-out) is priced on the hitpoints the account goes in with, since nothing can be eaten inside a chat, and parked when the model says it dies"},
	"blackknight": {ID: BlackKnightsFortressID, Name: BlackKnightsFortressName, QP: BlackKnightsFortressRecord.QuestPoints, QPNeeded: BKMinQuestPoints, Filler: true, MinCombat: 30,
		Region: "Falador / Ice Mountain: the Black Knights' Fortress", EstMinutes: 45,
		Source: "design ruling (QP filler); questplan/blackknightsfortress.go and blackknightsroute.go (content quest_blackknight, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "Sir Amik Varze wants 12 quest points; the disguise is an iron chainbody and a bronze med helm worn (bank, mule, Wayne's, Horvik's, Peksa's); an ordinary cabbage (the field south of Falador) goes down the hole above the witch's cauldron after the grill has been listened at. The fortress is a loop of ladders and doors with fifteen aggressive level-33 knights (a hit of up to 4): the grill and the hole are in different halves joined only by the banquet hall, so the route runs the hall twice. Never fought: gated on hitpoints and food against a priced number of knight attacks, walked round the three sentries in the open ground, a death parks it a day. 2,500 coins"},
	"demon": {ID: DemonSlayerID, Name: DemonSlayerName, QP: 3, Filler: true, MinCombat: DemonMinCombat,
		Region: "Varrock (the Gypsy, the palace, its kitchen drain and the sewer under it, the stone circle south of the city) / the Wizards' Tower / the Lumbridge cow pasture", EstMinutes: 45,
		Source: "defs/demonslayer.ts; questplan/demonslayer.go (content quest_demon, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "Gypsy Aris (one coin), Sir Prysin, then three keys: Traiborn's for 25 sets of bones (the cows', carried in a bare pack to the tower's first floor, his ritual a run of message boxes), Captain Rovin's for the asking, and the drain's, a bucket of water poured down the palace kitchen's drain that lies in the Varrock sewer for three minutes (the pour, the manhole and the key are one step). Silverlight must be WORN to hurt Delrith (level 27, 7 hitpoints, hits up to 3) at the stone circle, which is single-way combat ringed by dark wizards (levels 7 and 20) that hunt every account of combat 41 and under: while a wizard's spell has landed in the last eight ticks the server refuses the attack on Delrith (\"I'm already under attack!\"), so the quest is not begun below combat 42 (it parks 12 hours there, whatever the account carries) and the visit leaves the circle if the server refuses twice; the incantation is the fourth line of his chat menu. Above combat 41 it is gated on hitpoints, swing and food against Delrith's priced blows and the sewer's; a death at the circle or in the sewer parks it a day. The sword replaces the weapon in the right hand, which is put back at the end"},
	"romeo": {ID: "romeo", Name: "Romeo & Juliet", QP: 5, Filler: true,
		Region: "Varrock", EstMinutes: 25,
		Source: "design ruling (QP filler); questplan/romeojuliet.go (content quest_romeojuliet, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "seven talks and one cadava berry (three ground spawns south-east of Varrock); Juliet re-issues her message only while neither the pack nor the bank holds one"},
	"doric": {ID: "doric", Name: "Doric's Quest", QP: 1, Filler: true,
		XP:     map[string]int{"mining": 1300},
		Region: "Falador / Rimmington's mine", EstMinutes: 30,
		Source: "defs/doric.ts; questplan/doricsquest.go (content quest_doric/scripts/quest_doric.rs2)",
		Notes:  "6 clay, 4 copper ore and 2 iron ore to Doric in the pack; the bank and the mule first (the mule holds copper and iron ore, never clay), the rest mined in Rimmington with a pickaxe (iron needs Mining 15, below it the quest parks 6 hours rather than train); no combat"},
	"fluffs": {ID: "fluffs", Name: "Gertrude's Cat", QP: 1, Filler: true,
		XP:     map[string]int{"cooking": 1525},
		Region: "Varrock market / Gertrude's house / Port Sarim / the Lumbridge farm / the lumber yard", EstMinutes: 45,
		Source: "defs/gertrudescat.ts; questplan/gertrudescat.go (content quest_fluffs/scripts/quest_fluffs.rs2)",
		Notes:  "100 coins to Shilop and Wilough standing close together, a bucket of milk and a seasoned sardine (doogle leaves on a raw sardine) used on Fluffs on the lumber yard's walkway, the kitten from one of six crates; the yard is entered over the broken fence by hand; no combat"},
	"drunkmonk": {ID: "drunkmonk", Name: "Monk's Friend", QP: 1, Filler: true,
		XP:     map[string]int{"woodcutting": 2000},
		Region: "East Ardougne's monastery / the forest south of Ardougne / the thieves' cave under the ring of stones", EstMinutes: 30,
		Source: "defs/monksfriend.ts; questplan/monksfriend.go (content quest_drunkmonk, brother_omad.rs2 and brother_cedric.rs2, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "the child's blanket from a cave whose ladder the player's own timer grows beside the ring of stones (the thieves hit for 2 and 3 and are not fought), a jug of water from a sink and a log from a tree for the drunk Brother Cedric, then Omad's party, which must be waited out; 2,000 Woodcutting XP and 8 law runes"},
	"cog": {ID: "cog", Name: "Clock Tower", QP: 1, Filler: true,
		Region: "the Clock Tower south of East Ardougne and the dungeon under it", EstMinutes: 45,
		Source: "defs/clocktower.ts; questplan/clocktower.go (content quest_cog and brother_kojo.rs2, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "four cogs on four spindles, carried one at a time: blue behind a secret wall, black red-hot (a bucket of water), white behind the rat pen (the lever, the gate, poison on the food trough, the rat gate), red in the cave of three level 53 ogres (food, 30 Hitpoints, a day's park after a death there); 500 coins"},
	"druid": {ID: DruidicRitualID, Name: DruidicRitualName, QP: 4, Filler: true, MinCombat: 30,
		XP:     map[string]int{"herblore": 250},
		Region: "Taverley / the Taverley dungeon / the Lumbridge woods, farm and dairy field", EstMinutes: 50,
		Source: "defs/druidicritual.ts; questplan/druidicritual.go (content quest_druid, kaqemeex.rs2 and sanfew.rs2, W13/W22 of docs/quest_plan_2026-10-15.md)",
		Notes:  "Kaqemeex and Sanfew (upstairs in his house) bracket four raw meats dipped one by one in the Cauldron of Thunder (use the meat on it, only while the quest is at Sanfew's stage): bear, giant rat, chicken and cow, each a certain drop of its animal in the Lumbridge region (Wydin sells a beef and a chicken; the bank holds the enchanted ones a half-finished run left, the mule none). The cauldron room's door raises the suits of armour that stand beside it (level 19, 29 hitpoints, a hit of 3, one per opening, two openings), so the trip down the Taverley dungeon is priced on the hitpoints and food the account carries (eight sharks asked for) and refused when the model loses: the hall up to the door is nine tiles wide and the planned line passes within two tiles of five unarmed skeletons (level 25, a hit of 3, in the dungeon's single combat one at a time), so the price is those five, both suits and the same five again on the way out, twelve fights to the death, from wherever the account stands; one death parks it a day. The 250 Herblore XP is Herblore 3, which Jungle Potion asks for"},
	"junglepotion": {ID: JunglePotionID, Name: JunglePotionName, QP: 1, Filler: true, MinCombat: 30, Prereqs: []string{DruidicRitualID},
		XP:     map[string]int{"herblore": 775},
		Region: "Karamja: Tai Bwo Wannai, the swamp to the south west, the Shilo road and the pothole cave to the north", EstMinutes: 60,
		Source: "defs/junglepotion.ts; questplan/junglepotion.go (content quest_junglepotion and trufitus.rs2, W22 of docs/quest_plan_2026-10-15.md)",
		Notes:  "Trufitus asks for five herbs in turn, each picked fresh from its own loc (snake weed in the swamp's vine, ardrigal in a palm on the east shore, sito foil from scorched earth, volencia moss from a rock, rogues purse from the wall of the cave under the Rocks north of the village), identified (Herblore 3) and handed over. Walks round the jungle spiders and tribesmen (rectangles round their spawns; the planner finds a route to every stand on the real pack); the cave holds twelve level 53 jogres with no way round, so the run is priced on the hitpoints and the food the account sets out with and refused when the model loses. Karamja has no bank: coins for the ships (30 each way) and the food are drawn first. One death parks it a day. 775 Herblore XP"},
	"elena": {ID: PlagueCityID, Name: PlagueCityName, QP: PlagueCityRecord.QuestPoints, Filler: true,
		XP:     map[string]int{"mining": 2425},
		Region: "East Ardougne's garden and the sewer under it / West Ardougne / the plague house; shops in Taverley and Port Sarim, the hobgoblins' trail by the Crafting Guild, McGrubor's Wood", EstMinutes: 45,
		Source: "defs/plaguecity/*.ts; questplan/plaguecity.go (content quest_elena and areas/area_ardougne_west, W22 of docs/quest_plan_2026-10-15.md)",
		Notes:  "four buckets of water on the garden soil (the bucket comes back empty), a spade, a rope, the gas mask, Jethick and the Rehnisons' door, the mourner, the clerk, Bravek's hangover cure (a chocolate bar ground with a pestle, a bucket of milk, a snape grass from the hobgoblins' trail), the warrant and the key to Elena's cell; two dangerous legs (ten guard dogs round the dwellberries, nine hobgoblins round the snape grass) are priced and given a berth, never fought, a death on either parks it a day; 2,425 Mining XP (content's 24,250 tenths) and the Ardougne Teleport scroll; unlocks Biohazard"},
	"biohazard": {ID: BiohazardID, Name: BiohazardName, QP: BiohazardRecord.QuestPoints, Filler: true, MinCombat: 20, Prereqs: []string{PlagueCityID},
		XP:     map[string]int{"thieving": 1250},
		Region: "East Ardougne (Elena, Jerico, the wall) / West Ardougne and the Mourners' headquarters / Rimmington (the Chemist and his errand boys) / Varrock's east quarter (Guidor); Thessalia's shop; ships to Karamja and back", EstMinutes: 55,
		Source: "defs/biohazard/*.ts; questplan/biohazard.go (content quest_biohazard, areas/area_ardougne_west and area_varrock/east_gate.rs2, W22 of docs/quest_plan_2026-10-15.md)",
		Notes:  "Jerico's bird feed on the watch tower, a pigeon cage opened inside its box, Omart's rope ladder over the wall (Kilron's back), rotten apples from the ground into the Mourners' cauldron (over the yard's fence), a doctors' gown from the nurse's cupboard, the headquarters' door that opens for a doctor, the sick mourner upstairs who turns on a false doctor and drops the key (level 13, 19 hitpoints, a hit of 2: the only fight, priced on the hitpoints and food the account crosses with, topped up before the talk, and a refusal parks it six hours; the other mourners are never talked to or fought), the gate and the crate for Elena's distillator, then the Chemist's touch paper, the three vials to his errand boys (any other pairing loses the vial; Varrock's east gate takes every vial an account carries through it), a priest's gown and robe for Guidor's wife, Guidor, Elena and King Lathas; 1,250 Thieving XP; two deaths park it, one in the headquarters or the cupboard's hut a day"},
	"totem": {ID: TribalTotemID, Name: TribalTotemName, QP: 1, Filler: true,
		XP:     map[string]int{"thieving": 1775},
		Region: "Brimhaven (Kangai Mau) / East Ardougne: the R.P.D.T. depot, the Wizard Cromperty and Lord Handelmort's mansion", EstMinutes: 40,
		Source: "defs/tribaltotem.ts; questplan/tribaltotem.go (content quest_totem, kangai_mau.rs2, rdpt_employee.rs2 and wizard_cromperty.rs2, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "Kangai Mau starts it in Brimhaven; the address label off the mansion's crate at the depot covers the Wizards' Tower one, the employee delivers it, the Wizard Cromperty teleports the account into the mansion; inside, the security door's four dials are set to KURT, the trapped stairs are disarmed (Thieving 21, enforced there only) and climbed, and the chest upstairs gives the totem, which goes back to Kangai Mau over the front door that opens only from inside; two ships each way, the guard dogs of the garden are walked clear of (hunt range one tile); no combat, 1,775 Thieving XP and five swordfish"},
	"fishingcompo": {ID: FishingContestID, Name: FishingContestName, QP: 1, Filler: true,
		XP:     map[string]int{"fishing": 2437},
		Region: "the Mountain Dwarf west of White Wolf Mountain / Hemenster's competition ground / McGrubor's Wood / Morgan's house in Draynor", EstMinutes: 45,
		Source: "defs/fishingcontest/*.ts; questplan/fishingcontest.go (content quest_fishingcompo, bonzo.rs2, morris.rs2, mountain_dwarf.rs2 and mcgrubors_wood.rs2, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "the dwarf at the tunnel mouth (a six-menu chain) hands over a pass; garlic (bank, mule, then Morgan's cupboard), a fishing rod (bank, mule, then a fishing shop) and three red vine worms dug with a spade in McGrubor's Wood, where ten level-44 guard dogs hunt accounts of combat 88 or less within a tile (priced by hitpoints and food, topped up, walked with auto-retaliate off, one death a day); the pass past Morris, the garlic in the wall pipe BEFORE the 5-coin fee so that Bonzo seats the account at the pipes and a retry is free, three casts at the pipes for a giant carp and the trophy, back to the dwarf; Fishing 10; 2,437 Fishing XP"},
	"itgronigen": {ID: ObservatoryID, Name: ObservatoryName, QP: 2, Filler: true,
		XP:     map[string]int{"crafting": 2250},
		Region: "the Observatory south-west of Ardougne, the goblin cavern and dome under it, the Barbarian Outpost's planks, Ardougne's furnace and range", EstMinutes: 45,
		Source: "defs/observatory/*.ts; questplan/observatory.go (content quest_itgronigen, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "three planks (the Barbarian Outpost's spawns), a bronze bar (the mule's, else smelted), a molten glass (a bucket of sand from the goblin huts' spawn and a seaweed burnt to soda ash on a range, at the furnace) and the lens mould from the sack in the keep behind the cavern's gate, then the lens made by hand and the telescope in the dome. The one fight is the goblin guard (level 42, 43 hitpoints, a hit of up to 5) at the gate: priced on the account's levels and food before the ladder, parked 6 hours when the model loses; the cavern's goblins and rats do not attack, the six look-alike chests spawn a poisonous spider and are never touched; no Crafting level is asked for; the constellation's gift is one of twelve"},
	"seaslug": {ID: SeaSlugID, Name: SeaSlugName, QP: 1, Filler: true,
		// The dry sticks never catch below Firemaking 30 (opheld1,dry_sticks), so the Record states it as a requirement and the ladder never starts the
		// quest below it. It is listed here as advised, not as a server gate, on purpose: a hard gate must name a training rung the ladder can run
		// (TestEveryHardGateHasARungThatProducesIt), and no Firemaking rung exists. An account under 30 is left alone, not trained for one filler.
		Gates:  []Gate{{Skill: "firemaking", Level: SlugFiremaking, Kind: GateAdvised, Note: "server-enforced, held by the Record's requirement; no Firemaking rung trains it"}},
		XP:     map[string]int{"fishing": 7175},
		Region: "Witchaven / Holgart's boat / the fishing platform / Kent's island", EstMinutes: 40,
		Source: "defs/seaslug/*.ts; questplan/seaslug.go (content quest_seaslug and area_fishing_platform, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "a swamp paste (Port Khazard's store sells it) for Holgart's boat, Kennith behind the crates, Kent on the island, a torch lit from dry sticks (a damp stick dried with a piece of broken glass, both ground spawns on the platform), the loose panel, the crane, and Caroline. Nothing attacks: the possessed fishermen and the sea slugs have no combat stats, and the only hurt is a 4 hitpoint smack for climbing the ladder without a lit torch (never done) and a 3 hitpoint bite for taking a slug (never done); the platform and the island are not on the walk graph, so Holgart's boat is a pocket's only way out"},
	"sheepherder": {ID: SheepHerderID, Name: SheepHerderName, QP: 4, Filler: true,
		Region: "East Ardougne (Halgrive and Doctor Orbon), Farmer Brumty's enclosure north of the city and the four sheep's fields", EstMinutes: 30,
		Source: "defs/sheepherder/*.ts; questplan/sheepherder.go (content quest_sheepherder, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "the protective suit (100 coins, Doctor Orbon), the cattle prod from the barn inside the enclosure (its gate is a teleport that wants the suit worn), then each of four sheep herded to the fence by prodding (a push sends it one tile away from the account; a sheep on the three columns west of the fence is sent over by any prod; 37 to 255 pushes a sheep on the live def, the push planner proven against a simulation of the content's sheep on the real collision pack), fed the poison inside the enclosure and its remains burnt in the incinerator; Halgrive pays 3,100 coins. Nothing attacks anybody: no fight, no food. One account at a time (the sheep and the prod are shared); one death parks it a day"},
	"mcannon": {ID: DwarfCannonID, Name: DwarfCannonName, QP: 1, Filler: true,
		Gates:  []Gate{{Skill: "hitpoints", Level: CannonMinHitpoints, Kind: GateAdvised, Note: "the module's own floor: a railing's failed roll cuts up to 3 and the giant bats hit up to 3; held by the Record's requirement"}},
		XP:     map[string]int{"crafting": 750},
		Region: "the Black Guard camp north-west of the Fishing Guild (railings, shed, watchtower), the goblins' cave under it, Nulodion's camp south of Ice Mountain", EstMinutes: 45,
		Source: "defs/dwarfcannon/*.ts; questplan/dwarfcannon.go (content quest_mcannon, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "six railings mended by a Crafting roll (24% at Crafting 1, a failed roll cuts for up to 3 hitpoints and health comes back one in 100 ticks, so they are tried at a hitpoint floor with food), the dwarf remains from the watchtower's top floor (obj id 0: the pack never shows them, the journal does), the goblins' cave by a telejump entrance and the mud pile out, Gilob's son in the crate at its far end, the broken cannon's four components (a Crafting or Smithing roll each, no hurt), the Commander's long conversation about it, and Nulodion's mould and notes (his camp is in Asgarnia: no walk reaches it from Kandarin but the Wilderness, which walks avoid, and the Tree Gnome spirit tree, so an account crosses by the fleet's travel teleport or parks on its stalled walks). The goblins have no hunt mode; the giant bats of the camp and the cave (level 27, cowardly, hunt combat 54 and under) are priced in the shared fight model and refused when it loses; one death parks it a day. 750 Crafting XP"},
	"hazeelcult": {ID: HazeelCultID, Name: HazeelCultName, QP: 1, Filler: true,
		XP:     map[string]int{"thieving": 1500},
		Region: "the Carnillean mansion west of Ardougne's zoo, the forest south of it (the five sewer valves, the cave) and the cult's sewer hideout", EstMinutes: 40,
		Source: "defs/hazeelcult.ts; questplan/hazeelcult.go (content quest_hazeelcult, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "runs the CULT's side, which has no fight (the TS def runs the Carnillean side and kills Alomone): Ceril starts it, Clivet at the cave under the forest gives a vial of poison for \"Ok, count me in\", the poison goes on the range in the mansion's kitchen cellar, the five sewer valves in the forest are set (Turn-left on the third, Turn-right on the others; the raft is tried first and the valves set only when it will not move or stops short) and the raft takes the account to Alomone, who is talked to; then a rusty key from the crate in the kitchen cellar, the secret wall on the first floor knocked, the chest in the attic opened with the key for the scroll of Hazeel, and the raft again for Alomone's ritual (the chat is driven until the tab reads complete). The cave, the stairs, the raft and the secret wall are teleports, so the sewer and the ladder room are pockets with their own way out; two message boxes (the crate's, the chest's) are continued, never closed. An account that took the Carnillean side is parked for a day; the sewer's rats and the cult are never attacked; 1,500 Thieving XP and 2,000 coins"},
	"elemental_workshop": {ID: ElementalWorkshopID, Name: ElementalWorkshopName, QP: 1, Filler: true,
		// Mining, Smithing and Crafting 20 are content's own (the rock, the bench, the bellows) and the Record states them; they are listed here as
		// advised, not as server gates, on purpose: a hard gate must name a training rung the ladder can run (TestEveryHardGateHasARungThatProducesIt),
		// no Mining rung exists, the Smithing rung (smithing34, for nails) and the Crafting rungs (flax) are not run for one filler, and an
		// account under 20 is left alone, not trained.
		Gates: []Gate{
			{Skill: "mining", Level: EwSkill, Kind: GateAdvised, Note: "server-enforced at the rock, held by the Record's requirement; no Mining rung trains it"},
			{Skill: "smithing", Level: EwSkill, Kind: GateAdvised, Note: "server-enforced at the workbench, held by the Record's requirement; the Smithing rung (for nails) is not run for one filler"},
			{Skill: "crafting", Level: EwSkill, Kind: GateAdvised, Note: "server-enforced at the bellows, held by the Record's requirement; the Crafting rungs (flax) are not run for one filler"},
		},
		XP:     map[string]int{"crafting": 5000, "smithing": 5028},
		Region: "Seers' Village (the bookcase, the smithy's secret room) and the workshop under it; Catherby's and Al Kharid's shops for the hammer, a pickaxe and thread", EstMinutes: 45,
		Source: "defs/elementalworkshop/*.ts; questplan/elementalworkshop.go (content quest_elemental_workshop, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "the Battered book from the bookcase read, a knife through its spine for the key, the key on the smithy's Odd looking wall (it opens from outside only with the key, from inside with a Push), the spiral stairs down; the machine: the two valves in order and the lever for the water wheel, a needle and a leather from the crates (once per account) and a thread brought for the bellows (Crafting 20), a stone bowl from a crate filled at the lava trough and emptied on the furnace, then the Pile of Rock mined (Mining 20 and a pickaxe): it sets a level 35 earth elemental (35 hitpoints, a hit of up to 4 every 6 ticks) on the account, the ONE fight, priced on the account's levels and food before the descent and again at the rock, parked 6 hours when the model loses and a day after a death there; its ore and four coal smelted (the air lever pulled when the furnace asks for more heat) and the bar made into the shield on the workbench (a hammer, Smithing 20, the book in the pack). The other 20 elementals have no hunt mode and are never woken; the hammer and a pickaxe come from Arhein's store, the thread from Dommik's, the coal and the food from the bank or the mule; 5,000 Crafting and 5,028 Smithing XP"},
	"itexam": {ID: DigsiteID, Name: DigsiteName, QP: 2, Filler: true,
		// The levels are the content's own (the workmen's pockets, the winches, the second and third mixes), held by the Record's requirements. They are
		// listed as advised, not as server gates, on purpose: a hard gate must name a training rung the ladder can run, and no Thieving or Herblore rung
		// exists (Agility 10 is under the Agility rungs' 25 and 35). An account under a level is left alone, not trained for one filler.
		Gates: []Gate{
			{Skill: "thieving", Level: DigThieving, Kind: GateAdvised, Note: "server-enforced (the workmen's pockets, digworkman1); held by the Record's requirement; no Thieving rung trains it"},
			{Skill: "agility", Level: DigAgility, Kind: GateAdvised, Note: "server-enforced (both winches); held by the Record's requirement"},
			{Skill: "herblore", Level: DigHerblore, Kind: GateAdvised, Note: "server-enforced (the second and third mixes); held by the Record's requirement; no Herblore rung trains it"},
		},
		XP:     map[string]int{"mining": 15300, "herblore": 2000},
		Region: "Varrock (the Curator, the tea seller, the general store) / the Digsite and its Exam Centre / the shafts under it / Taverley (Jatix)", EstMinutes: 45,
		Source: "defs/digsite/*.ts; questplan/digsite.go (content quest_itexam, area_varrock/curator.rs2, W13 of docs/quest_plan_2026-10-15.md)",
		Notes:  "the Examiner's letter stamped by the Curator, then three exams answered with what the students tell: the green student's rock sample is a workman's pocket (Thieving 25, 3 in 11 while the first exam is the stage; a miss costs a hitpoint and can kill, so the pocket loop keeps to a hitpoint floor, eats or rests there, and an account the pocket would drain is given a little food first), the purple student's lies in one bush, the orange student's and the third exam's opal come out of the river (a tray from one spawn, a cup of tea for the panning guide, 3 and 3 in 40); the exam answers are the ones the content offers once an errand is answered. A find for the expert from the level 3 site (a trowel, a specimen jar from the sacks, a specimen brush from a pocket; the talisman is one dig in 20), his letter to a workman, then two winches (a rope each, Agility 10), the cave workman's key after four pleas, an arcenia root, the chest's powder and the barrel's liquid named by the expert, a charcoal dug and ground, and four mixes (Herblore 10) that explode on Drop (25 to 65 damage), so only an allow-list of dig loot is ever dropped and every dig comes before the first volatile item. The compound is poured on the bricks and lit from one tile (3379,9826) of the east shaft; the altar cave it runs the account into holds ten level 25 skeletons that hunt combat 50 and under and lie two tiles from the tablet, so the visit is priced on hitpoints and food below combat 51 and the account never fights. 15,300 Mining and 2,000 Herblore XP, two gold bars"},
	"tbwt": {ID: TaiBwoID, Name: TaiBwoName, QP: 2, Filler: true, MinCombat: 45, Prereqs: []string{JunglePotionID},
		// The Record states these levels, so the ladder never starts the quest below them; they are listed as advised and not as server gates on
		// purpose: a hard gate must name a training rung the ladder can run (TestEveryHardGateHasARungThatProducesIt), and no Cooking, Fishing,
		// Agility-15 or Magic rung exists. The content enforces Cooking 30 (the marinade) and Fishing 5 (the net) only; Agility 15 is the
		// journal's list and the owner's; Magic 43 is Superheat Item, the module's own way to burn the Jogre bones, because the fleet's
		// Firemaking is 1 (the tinderbox needs 30) and the only furnace on Karamja is behind Shilo Village.
		Gates: []Gate{
			{Skill: "cooking", Level: 30, Kind: GateAdvised, Note: "server-enforced for the marinade, held by the Record's requirement"},
			{Skill: "agility", Level: 15, Kind: GateAdvised, Note: "the journal's list, held by the Record's requirement"},
			{Skill: "fishing", Level: 5, Kind: GateAdvised, Note: "server-enforced for the Karambwanji net, held by the Record's requirement"},
			{Skill: "magic", Level: TbMinMagic, Kind: GateAdvised, Note: "the module's own: Superheat Item burns the Jogre bones, held by the Record's requirement"},
		},
		XP:     map[string]int{"fishing": 1500},
		Region: "Karamja: Tai Bwo Wannai (Timfraku, Tamayu, Tinsay), Brimhaven (Lubufu), the Holy Lake, the east coast (Tiadeche, Zambo's bar, the banana grove) and the Jogre pen", EstMinutes: 110,
		Source: "defs/tbwt/*.ts; questplan/taibwotrio.go (content quest_tbwt, tbwt_timfraku.rs2, trufitus.rs2 and the fishing, cooking, superheat and monkey scripts, W22 of docs/quest_plan_2026-10-15.md)",
		Notes:  "Timfraku's job, then four errands: Lubufu (20 raw Karambwanji netted at the Holy Lake in pack-sized rounds, three questions, a Karambwan vessel and 1,500 Fishing XP), Tiadeche (the vessel baited and handed over, his first raw Karambwan), Tamayu (a hunt watched, four doses of agility potion, a spear above bronze smeared with the Karambwan cooked on the jungle fire and ground, the killing hunt), Tinsay (a banana sliced into Zambo's Karamjan rum, a seaweed sandwiched in the skin of a monkey Wind-Bolted down and skinned by Tamayu, a Jogre's bones burnt with Superheat Item, smothered in Karambwanji paste and cooked on the fire) and Tiadeche's manual from a second vessel. THE SHAIKAHAN IS NOT FOUGHT: Tamayu's hunt is a cutscene the account watches; the level 83 beast that roams Tiadeche's ground is priced (a visit with nothing to eat in it) and never attacked. The one fight is a Jogre (level 53, 60 hitpoints, a hit of 7) for the bones it always drops, priced on the account's swing and food before it leaves the mainland. The walks keep out of rectangles round every spider, tribesman, guard dog and Jogre spawn. Parked, with the reason, until the bank or the mule holds four doses of agility potion, a spear above bronze and a nature rune (none does on 19 Oct 2026); Firemaking is 1 fleet-wide, so there is no tinderbox route. A death parks it for a day"},
}

// QuestTabName is the quest tab's own spelling of a ladder quest: the key quests.Status and
// ReadStatus take. LadderFacts names the rungs for reports, and for Rune Mysteries that is not the
// tab's spelling ("Rune Mysteries" against "Rune Mysteries Quest"), which would read as an
// unknown quest for ever.
func QuestTabName(id string) string {
	if id == RuneMysteriesID {
		return RuneMysteriesName
	}
	return LadderFacts[id].Name
}
