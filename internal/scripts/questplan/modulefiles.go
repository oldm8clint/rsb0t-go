package questplan

// MODULE FILES: which source files ARE a quest, for the one question cmd/questpromote asks of git, "what
// is the last commit that changed this module, and which builds contain it".
//
// A completion proves the module it ran, not the module as it reads today. The promotion rule counts
// completions "since the module last changed", so a module needs a definition: the pure plan
// (questplan), the live steps (questmods, or waterfallplan/waterfallrunner for Waterfall), and the
// helper files only that quest uses. Shared machinery (the engine, the walker, the provisioner) is not in
// here on purpose: it changes in nearly every build, and a rule that reset every quest's evidence for
// each one would never let anything be promoted. The cost of that choice is real: a fix in shared code
// (a walker bug that parked every Restless Ghost run until the 10:09 build on 7 Oct 2026) does not start
// a new window, so cmd/questpromote prints a per-build breakdown (-breakdown) for a human to read next to
// the verdict.
//
// A name ending in "/" is a directory: every non-test .go file in it. All paths are repo-relative with
// forward slashes. TestEveryQuestFactoryHasModuleFiles (internal/bot) fails for a registered quest with no
// entry here, and TestModuleFilesExist for an entry naming a file that is gone, so the table cannot rot
// quietly when a file is renamed.
var ModuleFiles = map[string][]string{
	"waterfall":     {"internal/scripts/waterfallplan/", "internal/scripts/waterfallrunner/"},
	"runemysteries": {"internal/scripts/questplan/runemysteries.go"},
	"pip":           {"internal/scripts/questplan/priestinperil.go", "internal/scripts/questmods/pip.go"},
	"hetty":         {"internal/scripts/questplan/witchspotion.go", "internal/scripts/questmods/hetty.go"},
	"vampire":       {"internal/scripts/questplan/vampireslayer.go", "internal/scripts/questmods/vampire.go"},
	"murder":        {"internal/scripts/questplan/murdermystery.go", "internal/scripts/questmods/murder.go"},
	"cook":          {"internal/scripts/questplan/cooksassistant.go", "internal/scripts/questplan/shopontheway.go", "internal/scripts/questmods/cook.go"},
	"sheep":         {"internal/scripts/questplan/sheepshearer.go", "internal/scripts/questplan/shopontheway.go", "internal/scripts/questmods/sheep.go"},
	"ghost":         {"internal/scripts/questplan/restlessghost.go", "internal/scripts/questmods/ghost.go"},
	"imp":           {"internal/scripts/questplan/impcatcher.go", "internal/scripts/questmods/imp.go"},
	"merlin":        {"internal/scripts/questplan/merlinscrystal.go", "internal/scripts/questmods/merlin.go"},
	"grail": {"internal/scripts/questplan/holygrail.go", "internal/scripts/questplan/grailtitan.go",
		"internal/scripts/questplan/titanrunes.go", "internal/scripts/questmods/grail.go", "internal/scripts/questmods/grailtitan.go"},
	"witch":        {"internal/scripts/questplan/witchshouse.go", "internal/scripts/questmods/witch.go"},
	"arena":        {"internal/scripts/questplan/fightarena.go", "internal/scripts/questplan/fightwatch.go", "internal/scripts/questmods/arena.go"},
	"spirit":       {"internal/scripts/questplan/naturespirit.go", "internal/scripts/questmods/spirit.go"},
	"treegnome":    {"internal/scripts/questplan/treegnome.go", "internal/scripts/questplan/treegnome_warlord.go", "internal/scripts/questplan/treegnome_stronghold.go", "internal/scripts/questmods/treegnome.go", "internal/scripts/questmods/treegnome_warlord.go"},
	"grandtree":    {"internal/scripts/questplan/grandtree.go", "internal/scripts/questmods/grandtree.go"},
	"scorpion":     {"internal/scripts/questplan/scorpioncatcher.go", "internal/scripts/questmods/scorpion.go"},
	"deathplateau": {"internal/scripts/questplan/deathplateau.go", "internal/scripts/questmods/death.go"},
	"barcrawl":     {"internal/scripts/questmods/barcrawl.go"},
	"horror":       {"internal/scripts/questplan/horror.go", "internal/scripts/questmods/horror.go"},
	"fremennik":    {"internal/scripts/questplan/fremennik.go", "internal/scripts/questmods/fremennik.go"},
	"dragon":       {"internal/scripts/questplan/dragonslayer.go", "internal/scripts/questmods/dragon.go"},
	"gobdip":       {"internal/scripts/questplan/goblindiplomacy.go", "internal/scripts/questmods/gobdip.go"},
	"romeo":        {"internal/scripts/questplan/romeojuliet.go", "internal/scripts/questmods/romeo.go"},
	"haunted": {"internal/scripts/questplan/ernestchicken.go", "internal/scripts/questplan/ernestmaze.go", "internal/scripts/questplan/ernestescape.go",
		"internal/scripts/questmods/ernest.go",
		"internal/scripts/questmods/ernestmaze.go"},
	"hunt": {"internal/scripts/questplan/piratestreasure.go", "internal/scripts/questmods/hunt.go"},
	// The wig's wool is the Sheep Shearer module's own shear-and-spin (its executor and its shears kit).
	"prince": {"internal/scripts/questplan/princeali.go", "internal/scripts/questplan/shopontheway.go", "internal/scripts/questplan/sheepshearer.go",
		"internal/scripts/questmods/prince.go", "internal/scripts/questmods/sheep.go"},
	// The cabbage's shop rule (Wydin's shelf for an account nearer Port Sarim than the farm) is the fillers' shared ShopOnTheWay.
	"blackknight": {"internal/scripts/questplan/blackknightsfortress.go", "internal/scripts/questplan/blackknightsroute.go", "internal/scripts/questplan/shopontheway.go",
		"internal/scripts/questmods/blackknight.go"},
	"doric": {"internal/scripts/questplan/doricsquest.go", "internal/scripts/questmods/doric.go"},
	"fluffs": {"internal/scripts/questplan/gertrudescat.go", "internal/scripts/questplan/cooksassistant.go", "internal/scripts/questmods/fluffs.go",
		"internal/scripts/questmods/cook.go"},
	"drunkmonk": {"internal/scripts/questplan/monksfriend.go", "internal/scripts/questmods/monk.go"},
	"cog":       {"internal/scripts/questplan/clocktower.go", "internal/scripts/questmods/clocktower.go"},
	// The bank rule (the nearest booth, Draynor for Lumbridge and Al Kharid) is the fillers' shared cookSheepBank.
	"demon": {"internal/scripts/questplan/demonslayer.go", "internal/scripts/questplan/shopontheway.go", "internal/scripts/questmods/demon.go"},
	// The ritual's Wydin shelf rule (an account nearer Port Sarim than the fields) and bank are the fillers' shared ShopOnTheWay and
	// cookSheepBank; its hunts and the cauldron's door are its own executors.
	"druid":        {"internal/scripts/questplan/druidicritual.go", "internal/scripts/questplan/shopontheway.go", "internal/scripts/questmods/druid.go"},
	"junglepotion": {"internal/scripts/questplan/junglepotion.go", "internal/scripts/questplan/shopontheway.go", "internal/scripts/questmods/junglepotion.go"},
	"elena":        {"internal/scripts/questplan/plaguecity.go", "internal/scripts/questmods/plague.go"},
	// Biohazard shares Plague City's plan (its boxes, its foods and its leg helpers) and its customs' drivers, so a change to either resets it.
	"biohazard": {"internal/scripts/questplan/biohazard.go", "internal/scripts/questplan/plaguecity.go", "internal/scripts/questmods/biohazard.go",
		"internal/scripts/questmods/plague.go"},
	"totem": {"internal/scripts/questplan/tribaltotem.go", "internal/scripts/questmods/totem.go"},
	// The garlic from Morgan's cupboard is Vampire Slayer's own step, and the guard dog is priced by the fight tables.
	"fishingcompo": {"internal/scripts/questplan/fishingcontest.go", "internal/scripts/questplan/vampireslayer.go", "internal/scripts/questmods/fishing.go",
		"internal/scripts/questmods/vampire.go"},
	// The Observatory Quest and the Sea Slug Quest: the pure plan and the live executors each.
	"itgronigen": {"internal/scripts/questplan/observatory.go", "internal/scripts/questmods/observatory.go"},
	"seaslug":    {"internal/scripts/questplan/seaslug.go", "internal/scripts/questmods/seaslug.go"},
	// Sheep Herder and Dwarf Cannon: the pure plan (the herding loop and the push planner beside it) and the live executors each.
	"sheepherder": {"internal/scripts/questplan/sheepherder.go", "internal/scripts/questplan/sheepherdpath.go", "internal/scripts/questplan/sheepherdloop.go",
		"internal/scripts/questmods/sheepherder.go"},
	"mcannon": {"internal/scripts/questplan/dwarfcannon.go", "internal/scripts/questmods/dwarfcannon.go"},
	// Hazeel Cult and the Elemental Workshop: the pure plan and the live executors each.
	"hazeelcult":         {"internal/scripts/questplan/hazeelcult.go", "internal/scripts/questmods/hazeel.go"},
	"elemental_workshop": {"internal/scripts/questplan/elementalworkshop.go", "internal/scripts/questmods/workshop.go"},
	// The Digsite Quest: the pure plan and the live executors.
	"itexam": {"internal/scripts/questplan/digsite.go", "internal/scripts/questmods/digsite.go"},
	// Tai Bwo Wannai Trio: the pure plan (the story and journal, the hazards, the decision) and the live executors. The banana grove and
	// the rum shelf are Pirate's Treasure's tables (piratestreasure.go), the bank rule the fillers' shared cookSheepBank.
	"tbwt": {"internal/scripts/questplan/taibwotrio.go", "internal/scripts/questplan/taibwohazards.go", "internal/scripts/questplan/taibwodecide.go",
		"internal/scripts/questplan/piratestreasure.go", "internal/scripts/questplan/shopontheway.go", "internal/scripts/questmods/tbwt.go"},

	// The training rungs: one module file per family, so a change to the Cows rung resets all four of
	// its levels (they are the same code with a different skill and target).
	"agility25":     {"internal/scripts/questplan/gnomecourse.go", "internal/scripts/questmods/gnome.go"},
	"agility35":     {"internal/scripts/questplan/gnomecourse.go", "internal/scripts/questmods/gnome.go"},
	"crafting18":    {"internal/scripts/questmods/crafting.go"},
	"crafting40":    {"internal/scripts/questmods/crafting.go"},
	"cows20":        {"internal/scripts/questmods/cows.go"},
	"prayer31":      {"internal/scripts/questmods/cows.go", "internal/scripts/questmods/prayerbones.go", "internal/scripts/questplan/prayerbones.go"},
	"prayerkq":      {"internal/scripts/questmods/cows.go", "internal/scripts/questmods/prayerbones.go", "internal/scripts/questplan/prayerbones.go"},
	"attack20":      {"internal/scripts/questmods/cows.go"},
	"prayer43":      {"internal/scripts/questmods/cows.go", "internal/scripts/questmods/prayerbones.go", "internal/scripts/questplan/prayerbones.go"},
	"smithing34":    {"internal/scripts/questmods/smith.go"},
	"woodcutting40": {"internal/scripts/questmods/woodfletch.go"},
	"fletching25":   {"internal/scripts/questmods/woodfletch.go"},
}
