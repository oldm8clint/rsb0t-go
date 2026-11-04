package bot

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/combatrunner"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/fighterplan"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/settings"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("CombatTrainer", func() Bot { return &combatTrainerBot{} })
}

// CombatTrainerSettings is the declared schema, resolved through internal/settings so a value
// arriving as a bare string from a panel is typed and a missing key takes the default.
var CombatTrainerSettings = settings.Schema{
	// Max is 75 on all five banded goals, and that is the LADDER's ceiling rather than a taste:
	// its top rung ends at 75 exclusive, so a goal above 75 asks PickRung for a band no rung
	// covers, gets BlockedNoBand, and stops the run with an error after it has already trained
	// for hours. These used to offer 99. combatplan.MaxTrainableGoal computes the real number
	// and TestCombatTrainerGoalsAreCappedAtTheLadderCeiling fails if the two ever drift apart.
	// goalPrayer is deliberately still 99: Prayer is not banded, so PrayerRung answers at any
	// level and a Prayer goal is slow rather than terminal.
	"goalAttack":   {Type: settings.TypeNumber, Default: 75.0, Min: settings.Float(1), Max: settings.Float(75), Label: "Attack goal"},
	"goalStrength": {Type: settings.TypeNumber, Default: 75.0, Min: settings.Float(1), Max: settings.Float(75), Label: "Strength goal"},
	"goalDefence":  {Type: settings.TypeNumber, Default: 75.0, Min: settings.Float(1), Max: settings.Float(75), Label: "Defence goal"},
	"goalRanged": {
		Type: settings.TypeNumber, Default: 75.0, Min: settings.Float(0), Max: settings.Float(75),
		Label: "Ranged goal",
		Help:  "0 turns Ranged off and makes this a melee-only run. Ranged is trained after every melee goal is met, at the same rungs with a bow. Lowe's shelf caps the EQUIPMENT at oak bows and steel arrows, because willow and better sit in the Ranging Guild behind a Ranged 40 door on a route nobody has walked with this client. That stops the bow improving, not the level: the same oak bow keeps earning Ranged all the way to 75",
	},
	"goalMagic": {
		Type: settings.TypeNumber, Default: 75.0, Min: settings.Float(0), Max: settings.Float(75),
		Label: "Magic goal",
		Help:  "0 turns Magic off. Magic is trained last, autocasting from an elemental staff. Fire Blast at 59 is the strongest SPELL the reachable shops can keep in runes — the four Waves need blood runes, sold only behind the Magic Guild's Magic 66 door — but that caps the spell, not the level: the run keeps casting Fire Blast and Magic keeps rising to 75",
	},
	"magicElement": {
		Type: settings.TypeString, Default: "Fire", Options: []string{"Fire", "Wind", "Water", "Earth"},
		Label: "Magic element",
		Help:  "which elemental spell line to autocast, and so which staff to buy. Fire is the default because it has the highest max hit at every tier and its staff is the same 1000 coins as the others",
	},
	"goalPrayer": {
		Type: settings.TypeNumber, Default: 43.0, Min: settings.Float(1), Max: settings.Float(99),
		Label: "Prayer goal",
		Help:  "reached by burying the big bones every giant rung drops, not by a separate trip: one melee stat from 20 to 75 on the giant rungs is about 4,600 kills and so about 4,600 big bones, which is 69,585 Prayer XP against the 50,339 that 43 costs",
	},
	"membersWorld": {
		Type: settings.TypeBoolean, Default: true,
		Label: "Members world",
		Help:  "the free-to-play giant ladder is 12 Giants, 7 Moss giants and 3 Ice giants in total, which cannot support a cohort. Members adds 77 Ogre and 23 Fire giant spawns, though no route to those has been walked with this client yet",
	},
	"food": {
		Type: settings.TypeString, Default: "", Options: fighterfood.FoodOptions,
		Label: "Food",
		Help:  "leave empty to use whatever edible thing the pack already holds",
	},
	"eatAtHp": {
		Type: settings.TypeNumber, Default: 50.0, Min: settings.Float(10), Max: settings.Float(90),
		Label: "Eat below (% hp)",
	},
	"buyGear": {
		Type: settings.TypeBoolean, Default: true,
		Label: "Buy gear",
		Help:  "buy and wear the best tier a reachable shop stocks for the account's level. Shops stop below rune, so from Attack or Defence 40 the run stays on adamant",
	},
	"gearTripBudget": {
		Type: settings.TypeBoolean, Default: true,
		Label: "Budget gear shopping trips",
		Help:  "on, because gear buying is what stops a 1-to-45 run finishing. Measured on two arms differing only in buyGear: the same Al Kharid band gained 68 XP/min with shopping on against 506 with it off, 19 shop trips bought 4 items and the toll gate was crossed 205 times against 9. With this on, the run makes ONE buying journey per band, visiting the nearest keepers first and walking home once, instead of a separate cross-map round trip per gear slot. Turn it off to restore the old trip-per-slot behaviour",
	},
	"fightWithoutFood": {
		Type: settings.TypeBoolean, Default: false,
		Label: "Fight on without food",
		Help:  "off, because with no mule an empty bank is the end of the food supply: every cooked-food stock line in the shops a free-to-play combat account can reach is 0, and a 0 never rises. A run that carries on anyway donates its levels to the respawn — one measured arm died 13 times after its food ran out. Turn it on only to watch that happen deliberately",
	},
	"rung": {
		Type: settings.TypeString, Default: "auto",
		Label: "Rung",
		Help:  "auto picks by the lowest melee stat; naming a rung key (cows, alkharid, giants, mossgiants, icegiants, ogres, firegiants) pins it",
	},
	"logDetail": {Type: settings.TypeString, Default: "Normal", Options: []string{"Normal", "Verbose"}, Label: "Log detail"},
}

// combatTrainerBot is milestone 2 of docs/combat_trainer_plan.md: the rung runtime, the
// gear-buying loop and the registration.
//
// The shape is this codebase's usual one — every Loop call re-derives what to do from live
// state, and the only things remembered between calls are what genuinely must survive one call
// to the next (the chosen rung, the counters, and which one-off warnings have been said).
//
// What this script deliberately does NOT have, and the consequence, stated here because it is
// the single most important thing to know about it:
//
//	There is no mule. The account owner descoped the mule coin/item path (primitive 5 of the
//	plan) because it overlaps another session's claim, so this trainer funds itself entirely
//	from what its own kills drop. Every melee tier up to adamant is shop-buyable and the run
//	handles that by itself. Rune is not: rune scimitar, two-hander, full helm, med helm,
//	kiteshield and square shield are sold nowhere in content 289, and the rune longsword,
//	sword, mace, chainbody, platelegs and plateskirt are sold only behind the Champions'
//	Guild's 32-quest-point door, which a combat-only run never opens. So from Attack or
//	Defence 40 onward the account wears adamant while its levels would allow rune. That is a
//	ceiling, not a failure — adamant still trains to 75 — and combatplan.CheckGearCeiling makes
//	the run say so once, out loud, rather than looping quietly on a shop trip that can never
//	succeed.
//
// Ranged and Magic (milestone 4) ARE here now, and they are what makes the account owner's goal
// of base 75 in all five combat stats reachable at all: the script trained melee only before, so
// it could never have finished. They train after melee, in that order, at the same rungs with a
// bow or a staff, and each has a supply ceiling the shops impose rather than the ladder. See
// combattrainer_styles.go.
//
// Deliberately not here: the Prayer top-up trip (milestone 5). Prayer still reaches its goal,
// because burying is part of the fighting loop at every giant rung.
type combatTrainerBot struct {
	cfg combatTrainerSettings

	// embed is set when another bot runs this trainer as one of its stages (ScarabSwarm's Stage
	// 5); nil for the registered run, which it leaves unchanged. See combattrainer_embed.go.
	embed *combatTrainerEmbed

	// walker takes the long way to a rung — through the trapdoor, the manhole or the ice
	// dungeon ladder. Nil degrades travel to same-level pathfinding.
	walker *webwalk.Walker

	ladder   []combatplan.Rung
	rung     combatplan.Rung
	haveRung bool

	startedAt   time.Time
	xpAtStart   map[int]int
	kills       int
	buried      int
	looted      int
	bought      int
	deaths      int
	trips       int
	eats        int
	killSites   []combatplan.KillSite
	loot        lootKills // loot panel attribution only (lootcombat.go)
	said        map[string]bool
	warnedAt    map[string]time.Time
	foodEmpty   bool
	travelFails int
	// bladeFails counts CONSECUTIVE passes on which the containers could be READ and no bladed
	// weapon was worn or could be worn out of the pack. Only readable passes count: an empty
	// answer from a panel that is not resolving is not evidence about the account. See
	// combatplan.BladeGate for the run this distinction cost.
	bladeFails int
	// unreadableSince is when the backpack and worn panels last stopped resolving, or the zero
	// time while they resolve. A macro event nulls the sidebar tabs for as long as it holds the
	// account, and every pack and worn read in this process comes back empty until it lets go.
	unreadableSince time.Time
	// foodScarce latches when the bank cannot fill the pack past the break-off reserve AND the
	// run has been told to carry on anyway.
	//
	// It is reachable ONLY with "fightWithoutFood" set, and the comment here used to claim a
	// role it does not play in a default run: bankFoodPolicy tests the same reading one branch
	// earlier and STOPS the run there, returning before this latch is ever set. So on the
	// default configuration the clean stop is what happens, not this. The latch is kept because
	// a fightWithoutFood run still needs it — without it a nearly-empty bank turns the reserve
	// into a trip every single pass, which is the bank spin this script has already grown twice.
	foodScarce bool
	// foodShort latches when a completed trip proved the bank cannot fill a TRIP-SIZED load.
	//
	// It is a different fact from foodScarce and it closes a different spin. foodScarce asks "can
	// this account still feed itself", judged against the danger floor; foodShort asks "can this
	// bank pay for the walk", judged against the distance-sized load. Without it the two rules
	// deadlock on the same reading: a fresh account holding 5 Trout is at the reserve of the cows
	// rung's 20-bite load and so must bank, and is comfortably above the reserve of its 4-bite
	// danger floor and so must not stop. Measured as 29 bank trips in five minutes with 0 kills.
	// See combatplan.BreakOffLoad. It clears the moment a trip fills a whole load again.
	foodShort bool
	// packStuck latches when a completed bank trip proved the pack cannot be emptied further.
	//
	// The food half of the bank decision has latched since the food fix (foodEmpty, foodShort);
	// the pack half never did, and that asymmetry IS the 273-trip spin measured at the giants
	// rung on 2026-09-19. A pack the keep-filter cannot empty left needsBank true on every pass,
	// and because the trip still withdrew something the anti-spin floor below was never reached,
	// so the run walked back into the bank about 17 times a minute with its melee XP frozen for
	// over sixteen minutes. The latch clears the moment the pack has room again.
	packStuck bool
	// stuckNames is what was still being carried when packStuck latched, so the one line that
	// announces it can say WHAT is stuck rather than only that something is.
	stuckNames []string
	// tripsSinceXP counts completed bank trips since the melee XP last rose, and xpAtLastTrip is
	// the reading it is compared against.
	//
	// This is the guard that does not care WHICH pair of rules deadlocked. The script has grown
	// five zero-progress bank loops, each a different pair, and all five share one shape no
	// healthy run has: trips accumulating while the server's own stat table holds still. See
	// combatplan.BankLoopExhausted.
	tripsSinceXP int
	xpAtLastTrip int
	// stopReason is why the run ended itself short of its goals — today only the empty food
	// supply. Held so OnStop's summary says it too, because the line that announced it may be
	// thousands of lines further up a long log.
	stopReason string
	// The heartbeat's own bookkeeping. lastProgressAt is the last time melee XP actually rose,
	// which is what separates a long fight from a wedged run; bestXP is the reading it was last
	// seen at. See heartbeat.
	lastBeat       time.Time
	lastBeatXP     int
	lastProgressAt time.Time
	bestXP         int
	// unavailable names gear the shops turned out not to stock, keyed lower-case. The gear
	// table records which shop LISTS an item, read from that shop's own .inv, but those lines
	// carry a stock count too and several are zero — so the best item the table can name for a
	// slot is sometimes one the shelf never has. What the run learns in game goes here.
	unavailable map[string]bool
	// slotFails counts failed trips per slot and slotGaveUp retires a slot for the run, so one
	// shop that cannot be reached does not starve every other slot behind it in the plan.
	slotFails  map[combatplan.Slot]int
	slotGaveUp map[combatplan.Slot]bool
	// unreachable remembers targets the run could not walk to or could not finish, by NPC
	// index, so it stops picking the same one every pass. A flat window is enough now that
	// pickTarget asks the pathfinder before it ranks: what this holds is a recently-answered
	// search, not a guess that has to escalate its way out of a wall.
	unreachable map[int]time.Time
	// walledAt is when the run last walked back to the rung's stand because no monster inside the
	// leash had a route (pickTarget): a shut hut door around the account, not the monsters.
	walledAt time.Time
	// pen is the ground of the chosen rung's fenced pen (combatplan.Rung.Pen) for the rung penKey, nil at a rung that is not one;
	// penIdleSince is when the run last had no monster in reach (the zero time while it has one), penBeatAt the next stop of the
	// pen's beat, penWalks how many stops of it the run has walked to and penBeatFails how many walks in a row failed. See
	// combattrainer_pen.go.
	pen          *combatrunner.PenGround
	penKey       string
	penIdleSince time.Time
	penBeatAt    int
	penWalks     int
	penBeatFails int
	// doors is the budget of targets taken from behind a door since the last kill (combatplan.DoorBudget), and
	// pickedDoors the shut doors the target pickTarget last returned is behind (0: on this side of every door), which
	// the fight that follows reads to count it. See pickTarget.
	doors       combatplan.DoorBudget
	pickedDoors int
	// lastKillAt is when this run's own last kill was seen (stillUnderAttack's settle).
	lastKillAt time.Time
	// gearTrips, lastGearTrip and killsAtLastGearTrip enforce the rule that a shop trip is paid
	// for with a kill. See mayShop, which today gates only the Ranged and Magic SUPPLY trips;
	// gear buying is gated by the trip budget below instead.
	gearTrips           int
	lastGearTrip        time.Time
	killsAtLastGearTrip int
	// The gear-buying trip budget. A buying RUN is one journey away from the rung that may visit
	// several keepers before walking back, which is the whole point: the measured failure was
	// five gear slots turning into five cross-map round trips because the plan is ordered by slot
	// and those slots' keepers stand in five different settlements. See combatplan/shoptrip.go
	// for the measurement and the rules.
	//
	// shopRunOpen says a journey is in progress, so the run keeps shopping rather than walking
	// home between keepers. shopStopsThisRun bounds one journey so a journey whose every stop
	// fails still ends. shopBandKey and shopRunsThisBand are the per-band allowance, reset when
	// the rung changes. killsAtRunEnd is what a further journey in the same band is priced
	// against.
	shopRunOpen      bool
	shopStopsThisRun int
	shopBandKey      string
	shopRunsThisBand int
	killsAtRunEnd    int
	// trainingStat is the stat the style is currently set for, so a change is logged once
	// rather than every Loop call.
	trainingStat combatplan.Skill
	// trainingStyle is the discipline being advanced now: melee until every melee goal is met,
	// then Ranged, then Magic. Held so the switch is logged once rather than every Loop call,
	// and because changing it has to clear the rung: the ladder bands on the level of whichever
	// stat is being trained, and that number jumps when the discipline changes.
	trainingStyle combatplan.Style
	// autocastSpell is the spell this run last set successfully, by name.
	//
	// It is a field rather than a reading because there is nothing to read. Content declares
	// autocast_spell (varp 59) without transmit=yes, so the engine never puts it on the wire and
	// no client can see which spell is set; it was measured reading 0 while autocasting was
	// plainly on. The server will say THAT we are autocasting (varp 108 reads 3) and never WHAT.
	// So the script remembers, and an empty value means "we have not set one this run".
	autocastSpell string
	// supplyFails counts failed supply trips per item, so a shop that cannot be reached retires
	// the same way a gear slot does instead of being attempted forever.
	supplyFails map[string]int
	// arrowsTaken counts recovered arrows, which is worth its own number: four fired arrows in
	// five land on the target's tile, so what this counts is the difference between an arrow
	// budget that lasts a session and one that lasts minutes.
	arrowsTaken int
	// arrowName is the arrow this run is firing, remembered so the fight loop knows what to pick
	// back up. Empty until a Ranged supply step has chosen one.
	arrowName string
	// tutorial is this run's progress off Tutorial Island, for an account dropped into the
	// fleet brand new. See combattrainer_tutorial.go, which owns the whole leg — including
	// why it has to run before the ContainersReadable gate below rather than after it.
	tutorial tutorialState
}

type combatTrainerSettings struct {
	goals            combatplan.Goals
	members          bool
	food             string
	eatFrac          float64
	buyGear          bool
	gearBudget       bool
	fightWithoutFood bool
	forcedRung       string
	magicElement     string
	verbose          bool
}

// readCombatTrainerSettings turns a resolved bag into the run's own typed settings. Kept
// separate and pure so it can be tested without a session, mirroring every sibling script's
// own *_settings_test.go.
func readCombatTrainerSettings(bag *settings.Bag) combatTrainerSettings {
	return combatTrainerSettings{
		goals: combatplan.Goals{
			Attack:   bag.Int("goalAttack", 75),
			Strength: bag.Int("goalStrength", 75),
			Defence:  bag.Int("goalDefence", 75),
			Ranged:   bag.Int("goalRanged", 75),
			Magic:    bag.Int("goalMagic", 75),
			Prayer:   bag.Int("goalPrayer", 43),
		},
		magicElement:     strings.TrimSpace(bag.Str("magicElement", "Fire")),
		members:          bag.Bool("membersWorld", true),
		food:             strings.TrimSpace(bag.Str("food", "")),
		eatFrac:          float64(bag.Int("eatAtHp", 50)) / 100,
		buyGear:          bag.Bool("buyGear", true),
		gearBudget:       bag.Bool("gearTripBudget", true),
		fightWithoutFood: bag.Bool("fightWithoutFood", false),
		forcedRung:       strings.ToLower(strings.TrimSpace(bag.Str("rung", "auto"))),
		verbose:          strings.EqualFold(bag.Str("logDetail", "Normal"), "Verbose"),
	}
}

func (b *combatTrainerBot) deps(ctx *Context) *combatrunner.Deps {
	clickRange := 0
	if embed := b.embed; embed != nil { // (a host's attackFirstRange)
		clickRange = embed.clickRange
	}
	return &combatrunner.Deps{
		ClickRange: clickRange,
		// The rungs' monsters stand in rooms: the palace, the castle, the long hall. A target behind a shut door is
		// reached by opening it (combatrunner.ReachFor, FightOne), not passed over as "no route".
		Doors:      true,
		Session:    ctx.Session,
		Config:     ctx.Config,
		Components: ctx.Components,
		Pack:       ctx.Pack,
		Locs:       ctx.Locs,
		Walker:     b.walker,
		Chat:       ctx.Chat,
		Delay:      ctx.Delay,
		Log:        ctx.Log,
		// Every NPC this script kills reaches the loot panel. Before 2026-09-22 only
		// AdvancedFighter fed it, so CombatTrainer's kills were invisible.
		OnKill: ctx.KillRecorder(),
		// What its Magic rungs cast costs, netted off the npc killed and counted on the Magic card
		// (a user request); the runner calls this instead of OnKill when a loot sink is set.
		OnKillSpent: ctx.KillSpentRecorder(), OnFightSpent: ctx.FightSpentRecorder(),
	}
}

// checkGoalsReachable refuses a goal this ladder cannot band for, at OnStart, rather than at
// the end of a run that has already spent hours earning every level beneath it.
//
// The failure it replaces was silent until the very last moment. The ladder's top rung ends at
// 75 EXCLUSIVE, so 75 is the highest goal the five banded disciplines can finish, and it only
// finishes because AllGoalsMet fires before the band is ever asked. One level higher and
// PickRung answers BlockedNoBand, chooseRung returns an error and Runner turns it into
// StateError — after the training, not before it.
//
// The schema now clamps those five to the same ceiling, so this cannot normally fire from a
// panel. It stays for two reasons: the ceiling belongs to the LADDER and a schema constant can
// drift away from it, and a message naming the setting to change is worth more to whoever
// reads it than "no rung available at magic level 75".
//
// Prayer is deliberately not checked. It is not banded: PrayerRung asks "where do bones drop
// cheapest" and answers at any level, so a Prayer goal is slow rather than terminal.
func (b *combatTrainerBot) checkGoalsReachable() error {
	ceiling := combatplan.MaxTrainableGoal(b.ladder, b.cfg.members)
	for _, g := range []struct {
		setting string
		want    int
	}{
		{"goalAttack", b.cfg.goals.Attack},
		{"goalStrength", b.cfg.goals.Strength},
		{"goalDefence", b.cfg.goals.Defence},
		{"goalRanged", b.cfg.goals.Ranged},
		{"goalMagic", b.cfg.goals.Magic},
	} {
		if g.want > ceiling {
			return fmt.Errorf("combattrainer: %s is %d, but the top rung of this ladder ends at %d and no rung covers a level above that — the run would train for hours and then stop with \"no rung available\". Set %s to %d or lower (goalPrayer is exempt, because Prayer is not banded)",
				g.setting, g.want, ceiling, g.setting, ceiling)
		}
	}
	return nil
}

func (b *combatTrainerBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("combattrainer: no collision pack configured — refusing to start rather than panic later inside pathfinding")
	}
	if ctx.Locs == nil {
		return fmt.Errorf("combattrainer: no loc index configured — refusing to start rather than fail at the first bank trip")
	}
	b.cfg = readCombatTrainerSettings(CombatTrainerSettings.Resolve(b.settingsSource(ctx)))
	if b.embed != nil && b.embed.liftGoals {
		b.cfg.goals = liftTrainerGoals(b.cfg.goals, b.settingsSource(ctx)) // the endgame stints' goals past 75 (their ladder is extended)
	}
	b.ladder = combatplan.MeleeLadder()
	if b.embed != nil && len(b.embed.ladder) > 0 {
		b.ladder = b.embed.ladder // the host's wider ladder (Stage 5's spread over SpreadLadder)
	}
	if err := b.checkGoalsReachable(); err != nil {
		return err
	}
	b.startedAt = time.Now()
	b.lastBeat = time.Now()
	b.lastProgressAt = time.Now()
	b.said = map[string]bool{}
	b.warnedAt = map[string]time.Time{}
	b.xpAtStart = map[int]int{}
	b.unavailable = map[string]bool{}
	b.loadShopMemory()
	b.slotFails = map[combatplan.Slot]int{}
	b.slotGaveUp = map[combatplan.Slot]bool{}
	b.unreachable = map[int]time.Time{}
	b.supplyFails = map[string]int{}
	for _, stat := range []int{combatrunner.AttackStat, combatrunner.StrengthStat, combatrunner.DefenceStat, combatrunner.HitpointsStat, combatrunner.PrayerStatIdx} {
		b.xpAtStart[stat] = b.skillXP(ctx, stat)
	}

	// This script's own InCombat is its ordinary operating state, not a hazard. Without this
	// the per-account Guardian flees it away from every fight it starts — live-found through
	// cmd/advancedfightertest, see randomevents.Guardian.CombatExpected's own doc comment.
	if ctx.Guardian != nil {
		ctx.Guardian.CombatExpected = true
	}

	if w, err := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); err != nil {
		ctx.logf("combattrainer: no webwalk (%v) — travel falls back to same-level pathfinding, which cannot reach any dungeon rung", err)
	} else {
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{
				Client: w.Client, Session: ctx.Session, Config: ctx.Config,
				Components: ctx.Components, Log: ctx.Log,
			}))
		}
		if b.embed != nil && b.embed.onWalker != nil {
			b.embed.onWalker(w) // the host's own walk work (ScarabSwarm's run staller) on every walk this trainer makes
		}
		b.walker = w
	}

	d := b.deps(ctx)
	if b.cfg.food == "" {
		if found := detectTrainerFood(d); found != "" {
			b.cfg.food = found
		} else {
			b.cfg.food = "Trout"
		}
	}

	l := combatrunner.TrainerLevels(d)
	ctx.logf("combattrainer: goals attack %d, strength %d, defence %d, ranged %d, magic %d, prayer %d — at %d/%d/%d melee, %d ranged, %d magic now, %s world, food %q",
		b.cfg.goals.Attack, b.cfg.goals.Strength, b.cfg.goals.Defence, b.cfg.goals.Ranged, b.cfg.goals.Magic, b.cfg.goals.Prayer,
		l.Attack, l.Strength, l.Defence, l.Ranged, l.Magic,
		map[bool]string{true: "members", false: "free"}[b.cfg.members], b.cfg.food)
	if b.cfg.goals.Ranged > 0 || b.cfg.goals.Magic > 0 {
		ctx.logf("combattrainer: order is melee to its goals, then Ranged, then Magic. Ranged tops out at Lowe's oak bow and steel arrows, and Magic at Fire Blast, because the Ranging Guild and the Magic Guild are both behind doors this run never opens")
	}
	ctx.logf("combattrainer: NO MULE — this run funds itself from its own drops. Everything to adamant is shop-buyable; rune is not sold anywhere a combat-only account can reach, so the gear stops at adamant and the run says so when it gets there")
	return nil
}

func detectTrainerFood(d *combatrunner.Deps) string {
	for _, name := range combatrunner.PackNames(d) {
		if base, ok := fighterfood.BaseFood(name); ok {
			return base
		}
	}
	return ""
}

func (b *combatTrainerBot) skillXP(ctx *Context, stat int) int {
	sk, ok := ctx.Session.State().Skill(stat)
	if !ok {
		return 0
	}
	return sk.XP
}

func (b *combatTrainerBot) vlog(ctx *Context, format string, args ...any) {
	if b.cfg.verbose {
		ctx.logf("combattrainer: "+format, args...)
	}
}

// sayOnce logs a line at most once per run, for a standing condition (a gear ceiling, a
// blocked rung) that would otherwise repeat every Loop call.
func (b *combatTrainerBot) sayOnce(ctx *Context, key, format string, args ...any) {
	if b.said[key] {
		return
	}
	b.said[key] = true
	ctx.logf("combattrainer: "+format, args...)
}

// containersUnreadableLimit is how long the backpack and worn panels may stay unresolvable
// before the run gives up on them.
//
// It exists so that waiting cannot become its own silent wedge, and it is deliberately far
// longer than any random event holds an account: the mime releases the tabs at its mapzoneexit,
// and the whole event is a handful of emotes. Five minutes of a genuinely dead interface is a
// different fault, and the run should say so rather than sit there.
const containersUnreadableLimit = 5 * time.Minute

func (b *combatTrainerBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	b.heartbeat(ctx, d)
	if !combatrunner.WaitForGuardian(d) {
		return nil
	}
	// Tutorial Island comes before every other step, including the death check and the
	// containers gate below. A brand-new account is blinded for the whole tutorial — content's
	// @start_tutorial strips the sidebar tabs — so reaching the ContainersReadable gate would
	// end the run with its five-minute fatal error, and reaching anything else would have it
	// buying gear and food against a pack it cannot see. See combattrainer_tutorial.go.
	if !b.tutorial.done {
		done, err := b.tutorialStep(ctx)
		if err != nil {
			return err
		}
		if !done {
			return nil
		}
	}
	if ctx.Session.State().IsDead() {
		if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
			b.deaths++
			ctx.logf("combattrainer: died (%d so far) — re-gearing from the pack and heading back", b.deaths)
			ctx.Session.State().ClearDead()
			b.haveRung = false
			combatrunner.WaitForRespawn(d, 12, 30*time.Second)
		}
	}
	if ctx.Components != nil {
		if dlg := actions.NewChatDialog(ctx.Session, ctx.Components); dlg.CanContinue() {
			_ = dlg.Continue()
			ctx.Delay(300 * time.Millisecond)
			return nil
		}
	}

	// Nothing below this line can be trusted while the backpack and worn panels are unreadable.
	// Every "what am I holding" question in combatrunner resolves through the sidebar tab that
	// owns the panel, and when that tab is null the answer comes back EMPTY rather than unknown:
	// the gear plan would re-buy a kit the account is wearing, needsBank would see a pack with
	// nothing in it, and the blade check would conclude the run had lost its sword. A macro
	// event does exactly this — the mime's entry proc calls ~macro_events_set_tabs, which nulls
	// tabs 0-6, 11 and 12 for the whole event — and on 2026-09-20 one Loop call that believed it
	// ended a run 22 minutes into a clean climb from Defence 1. Waiting costs a pass.
	if !combatrunner.ContainersReadable(d) {
		if b.unreadableSince.IsZero() {
			b.unreadableSince = time.Now()
			ctx.logf("combattrainer: the backpack and worn panels are not readable — a random event nulls the sidebar tabs while it holds the account, so every pack and worn read would come back empty. Waiting rather than reading that as a lost kit")
		}
		if held := time.Since(b.unreadableSince); held > containersUnreadableLimit {
			return fmt.Errorf("combattrainer: the backpack and worn panels have been unreadable for %s, far longer than any random event holds an account — this is a stuck interface rather than an event, and no gear, food or bank decision can be made without them",
				held.Round(time.Second))
		}
		ctx.Delay(2 * time.Second)
		return nil
	}
	if !b.unreadableSince.IsZero() {
		ctx.logf("combattrainer: the backpack and worn panels are readable again after %s — carrying on", time.Since(b.unreadableSince).Round(time.Second))
		b.unreadableSince = time.Time{}
	}

	l := combatrunner.TrainerLevels(d)
	prayer := combatrunner.SkillLevel(d, combatrunner.PrayerStatIdx)

	// Finished for good: every goal met, across all five combat stats and Prayer.
	if combatplan.AllGoalsMet(l, b.cfg.goals, prayer) {
		return b.finish(ctx, d, fmt.Sprintf("goals met — attack %d, strength %d, defence %d, ranged %d, magic %d, prayer %d",
			l.Attack, l.Strength, l.Defence, l.Ranged, l.Magic, prayer))
	}

	// Which discipline to advance now: melee until all three melee goals are met, then Ranged,
	// then Magic. That order is the account owner's and it is one discipline at a time on
	// purpose, because switching means re-gearing and a different consumable supply.
	style, haveStyle := combatplan.StyleToTrain(l, b.cfg.goals)
	// prayerOnly is the end of a run: every combat goal is met and Prayer is the last one short.
	// It changes where the run fights, and until it did, this was the case that stopped the script
	// with an error two bones from its own finish line. See chooseRung.
	prayerOnly := !haveStyle
	if prayerOnly {
		// Keep fighting the discipline we were on rather than standing still; Prayer arrives from
		// the big bones the giant rungs drop.
		style = b.trainingStyle
		if style == "" {
			style = combatplan.StyleMelee
		}
	}
	if b.trainingStyle != style {
		if b.trainingStyle != "" {
			// The ladder bands on the level of the stat being trained, and that number drops a
			// long way when the discipline changes (melee 75 to Ranged 1), so the rung has to be
			// re-derived rather than carried across.
			b.haveRung = false
			b.travelFails = 0
			ctx.logf("combattrainer: %s goals are met — switching to %s", b.trainingStyle, style)
		}
		b.trainingStyle = style
	}

	// The ceilings, each said once. All three exist for the same reason: with no mule, a tier
	// the shops do not stock is permanent, so it is stated rather than retried forever.
	b.announceCeilings(ctx, l, style)

	// Gear: buy what a shop can still improve, then wear the best held. The gate is inside
	// gearStep now rather than here, because the budget's first question is "is there anything
	// left worth buying", which only the plan can answer.
	if b.cfg.buyGear {
		if done, err := b.gearStep(ctx, d, l, style); err != nil {
			return err
		} else if done {
			return nil
		}
	}
	for _, it := range combatplan.EquipPlan(l, nil, combatrunner.WornNames(d), combatrunner.PackNames(d)) {
		// A Ranged or Magic run keeps its melee weapon in the PACK, never on the body, and
		// without this exception the two halves of the script fight each other for the weapon
		// slot every single pass. EquipPlan asks about SlotWeapon, a bow is SlotBow and a staff
		// is SlotStaff, so while either is worn the melee weapon slot reads empty and the plan
		// never stops wanting to fill it: the sword goes on here, pointWeapon puts the bow back
		// after the walk, and round it goes. Measured on the first live Ranged run, which bought
		// an Adamant longsword and wore it over the bow it had just nocked.
		//
		// The sword is still worth carrying. The sewer rungs need a blade in the right hand for
		// the ROUTE, which is what wearBladeForTravel puts on and what this swaps back off.
		if it.Slot == combatplan.SlotWeapon && !wearsMeleeWeapon(style) {
			continue
		}
		combatrunner.EquipByName(d, it.Name, 5*time.Second)
	}

	// Ranged and Magic need a consumable rather than a gear tier: arrows, or runes. Kept
	// separate from gearStep because the failure that matters is a different one — running dry
	// mid-fight, rather than wearing a worse tier than the level allows — and because an empty
	// quiver stops a Ranged run dead the way an empty bank stops a melee one.
	if b.cfg.buyGear {
		if done, err := b.supplyStep(ctx, d, l, style); err != nil {
			return err
		} else if done {
			return nil
		}
	}

	// Which rung. A rung covers a band of the LOWEST melee stat, and that stat moves while the
	// run trains, so the choice has to be re-derived rather than made once and kept.
	//
	// Why this check exists: without it the rung chosen on the first Loop call was held for the
	// whole run, because haveRung was only ever cleared by a death or a shop trip. An account
	// that trained past a band boundary carried on fighting the rung it had outgrown — it kept
	// gaining XP, so nothing looked broken, but the ladder below it was decorative and no run
	// could ever reach a dungeon by itself. A pinned rung is exempt: pinning is the operator
	// saying "this one", and re-deriving it would only re-pick the same key every call.
	band := b.bandLevel(l, style)
	// A Prayer-only run is exempt from the band-drift check, and it has to be: its rung is chosen
	// for the bones it drops rather than for the band it covers, so comparing it against a band of
	// 75 would re-derive the same rung on every single pass and never fight at it.
	if !prayerOnly && b.haveRung && (b.cfg.forcedRung == "" || b.cfg.forcedRung == "auto") {
		// An embedding host that spreads its accounts (ScarabSwarm's Stage 5) may seat one on a rung
		// below its band when the band is crowded (combatplan.Rung.OverflowFor); outgrowing that
		// rung's band is the point of it, not a drift.
		overflow := b.embed != nil && b.embed.pickRung != nil && band >= b.rung.ToLevel && b.rung.FromLevel >= 5
		if band < b.rung.FromLevel || (band >= b.rung.ToLevel && !overflow) {
			ctx.logf("combattrainer: %s level is %d, outside rung %q's band of %d-%d — re-deriving the rung",
				style, band, b.rung.Key, b.rung.FromLevel, b.rung.ToLevel)
			b.haveRung = false
			// Travel failures belong to the rung that was being walked to, not the new one.
			b.travelFails = 0
		}
	}
	// A host that caps its rungs (ScarabSwarm's Stage 5) moves the newest accounts of an over-full rung on.
	if !prayerOnly && b.haveRung && b.embed != nil && b.embed.shedRung != nil && (b.cfg.forcedRung == "" || b.cfg.forcedRung == "auto") &&
		b.embed.shedRung(b.rung) {
		ctx.logf("combattrainer: rung %q is over its seats — re-deriving the rung", b.rung.Key)
		b.haveRung = false
		b.travelFails = 0
	}
	if !b.haveRung {
		if err := b.chooseRung(ctx, d, l, band, style, prayerOnly); err != nil {
			return err
		}
	}

	// The sewer web is cut from the right hand, so a blade has to be WORN, not carried — and
	// that is a property of the ROUTE rather than of the fight, so it binds whatever discipline
	// the run is training. A Ranged or Magic run therefore walks in wearing its sword and swaps
	// to the bow or the staff once it has arrived, which is what a player does. Without the swap
	// the whole 25-45 band would be closed to Ranged, since a bow cannot cut a web.
	//
	// The three-answer gate is the fix for a run this ended. It used to be two answers — the
	// blade is on, or the run is over — and a mime event that nulled the tabs made "I cannot
	// read the pack" indistinguishable from "the pack is empty". See combatplan.BladeGate.
	if b.rung.NeedsWieldedBlade {
		wielding := combatrunner.WieldingBlade(d)
		wore := false
		if !wielding {
			wore = b.wearBladeForTravel(ctx, d)
		}
		verdict, fails := combatplan.BladeGate(combatrunner.ContainersReadable(d), wielding, wore, b.bladeFails, combatplan.BladeFailLimit)
		b.bladeFails = fails
		switch verdict {
		case combatplan.BladeStop:
			b.sayOnce(ctx, "blade", "rung %q crosses the Varrock sewer web, which the server cuts from the RIGHT-HAND slot only, and no bladed weapon is worn or carried", b.rung.Key)
			return fmt.Errorf("combattrainer: rung %q needs a bladed weapon worn and %d consecutive readable passes found none worn or carried; buy or equip one first", b.rung.Key, fails)
		case combatplan.BladeWait:
			b.warn(ctx, "blade-wait", time.Minute,
				"rung %q needs a bladed weapon in the right hand for the sewer web and none is worn or carried yet (readable pass %d of %d) — waiting for the gear or bank step to supply one rather than ending the run",
				b.rung.Key, fails, combatplan.BladeFailLimit)
			ctx.Delay(2 * time.Second)
			return nil
		}
	}
	combatrunner.EnsureRetaliate(d, true)

	// Bank when there is nothing to fight with or nowhere to put a drop.
	if need, why := b.needsBank(d); need {
		b.bankStep(ctx, d, why)
		return nil
	}

	// Walk to the stand only when we are not already AT the rung, and "at the rung" is the
	// rung's own leash rather than three tiles of the stand.
	//
	// This was a measured shuttle rather than a tidiness point. fightStep is bounded at 25
	// seconds so a due bank trip or a death is picked up promptly, so Loop re-runs every 25
	// seconds all day — and it used to travel to Stand with radius 3 on every one of those
	// passes, while the giants rung's leash is 12. An ordinary chase ends four to eight tiles
	// from the stand, which is inside the rung and outside the radius, so the run paid a full
	// webwalk plan-and-walk back to the stand after almost every fight. That is most of the 217
	// travel lines against 30 combat lines measured at this rung on 2026-09-20, and it is on top
	// of the bank commute rather than part of it.
	//
	// Standing anywhere inside the leash is standing at the rung by the rung's own definition:
	// pickTarget anchors MonstersNear on the STAND, not on us, so the target set is the same
	// from anywhere in it, and FightOne walks the last stretch itself.
	//
	// A rung whose ground is a fenced pen (combatplan.Rung.Pen) is at only while the account is inside the fence, and is walked
	// to by the pen's gate (travelToRung).
	if !b.atRung(ctx, d) {
		if !b.travelToRung(ctx, d) {
			b.travelFails++
			if b.travelFails >= 3 {
				return fmt.Errorf("combattrainer: could not reach rung %q (%s) in %d attempts — entrance: %s",
					b.rung.Key, b.rung.Note, b.travelFails, b.rung.Entrance)
			}
			ctx.logf("combattrainer: could not reach rung %q (attempt %d/3) — retrying", b.rung.Key, b.travelFails)
			ctx.Delay(3 * time.Second)
			return nil
		}
	}
	b.travelFails = 0

	// Point the weapon at what we came to train, now that we are standing at the rung.
	//
	// After the walk rather than before it, because the walk may have needed a different weapon
	// entirely: the sewer rungs demand a blade in the right hand for the route, so a Ranged or
	// Magic run arrives wearing its sword and swaps here. Without this step the discipline choice
	// is decorative — the server advances whichever style the weapon is set to, so a run that
	// decided to train Ranged and never touched the weapon would train Attack instead.
	if !b.pointWeapon(ctx, d, l, style) {
		return nil
	}

	b.fightStep(ctx, d)
	return nil
}

// chooseRung asks the ladder where to train and refuses, loudly, when nothing qualifies.
//
// band is the level the ladder bands on, which is the level of whatever discipline is being
// trained rather than always the lowest melee stat. That distinction is what makes the ladder
// usable by Ranged and Magic at all: an account that has finished melee is 75/75/75, and banding
// a Ranged 1 run on "lowest melee 75" falls outside every band on the ladder, so PickRung answers
// BlockedNoBand and the run stops with its goals unmet.
// prayerOnly says every combat goal is met and Prayer is the only one left, which needs a
// different question asked of the ladder. The ladder bands on the level of the stat being
// trained, and a finished account is 75/75/75, so the band is 75 — which no rung covers, because
// icegiants ends at 75 exclusive. PickRung answered BlockedNoBand and this function turned that
// into a returned error, so a run that was a few bones short of 43 Prayer STOPPED, with an error
// rather than the banked logout its own finish condition promises. That was live-reproduced
// before it was fixed (cmd/combattrainertest -mode prayer -prayerarm topped). Prayer asks "where
// do bones drop cheapest", which is combatplan.PrayerRung.
func (b *combatTrainerBot) chooseRung(ctx *Context, d *combatrunner.Deps, l combatplan.Levels, band int, style combatplan.Style, prayerOnly bool) error {
	lowest := band
	combat := combatrunner.CombatLevelOf(d)

	if b.cfg.forcedRung != "" && b.cfg.forcedRung != "auto" {
		r, ok := combatplan.RungByKey(b.ladder, b.cfg.forcedRung)
		if !ok {
			return fmt.Errorf("combattrainer: no rung named %q — the ladder has cows, alkharid, giants, mossgiants, icegiants, mossgiants-brimhaven, ogres, firegiants", b.cfg.forcedRung)
		}
		if !r.TravelProven {
			return fmt.Errorf("combattrainer: rung %q has no route this client has ever walked (%s) — it is in the ladder for its verified spawn count, not because it can be reached", r.Key, r.Entrance)
		}
		b.rung, b.haveRung = r, true
	} else if prayerOnly {
		choice := combatplan.PrayerRung(b.ladder, combat, b.cfg.members)
		if !choice.OK {
			return fmt.Errorf("combattrainer: every combat goal is met and Prayer is %d of %d, but no rung on this ladder drops big bones for a combat level %d account on a %s world: %s (%s)",
				combatrunner.SkillLevel(d, combatrunner.PrayerStatIdx), b.cfg.goals.Prayer, combat,
				map[bool]string{true: "members", false: "free"}[b.cfg.members], choice.Why, choice.Detail)
		}
		b.rung, b.haveRung = choice.Rung, true
		b.sayOnce(ctx, "prayer-rung", "every combat goal is met and only Prayer is left (%d of %d) — finishing it on rung %q, whose %s drops a big bone every kill and is the cheapest kill on the ladder that does",
			combatrunner.SkillLevel(d, combatrunner.PrayerStatIdx), b.cfg.goals.Prayer, choice.Rung.Key, choice.Rung.Monster.Name)
	} else {
		choice := b.pickRungChoice(lowest, combat)
		if !choice.OK {
			// A blocked ladder stops the run with the reason in the message. This is the
			// alternative to a silent spin: with no mule to ask for help, an unreachable
			// rung is terminal and should read that way in the log.
			return fmt.Errorf("combattrainer: no rung available at %s level %d, combat level %d: %s (%s)",
				style, lowest, combat, choice.Why, choice.Detail)
		}
		b.rung, b.haveRung = choice.Rung, true
	}

	ctx.logf("combattrainer: rung %q — %s (combat %d, %d hp, max hit %d), %d verified spawn(s), leash %d, bank %d,%d. Training %s. %s",
		b.rung.Key, b.rung.Monster.Name, b.rung.Monster.CombatLevel, b.rung.Monster.Hitpoints, b.rung.Monster.MaxHit,
		b.rung.SpawnCount(), b.rung.Leash, b.rung.Bank.X, b.rung.Bank.Z, b.describeTraining(l, style), b.rung.Entrance)
	if b.rung.Note != "" {
		ctx.logf("combattrainer: rung %q note: %s", b.rung.Key, b.rung.Note)
	}

	// Where this account should head for when something moves it and it has no direction of its
	// own — its own rung's bank. Unset, every flee helper and the fail-teleport recovery fall
	// back to the Catherby bank, which is right for a ProgressiveFisher and several hundred
	// tiles wrong for a combat account in Al Kharid. See randomevents.Guardian.SafeRef.
	if ctx.Guardian != nil {
		ref := pathfind.NavPoint{X: b.rung.Bank.X, Z: b.rung.Bank.Z, Level: b.rung.Bank.Level}
		ctx.Guardian.SafeRef = &ref
	}
	return nil
}

// gearTripEvery is the longest a run goes without being allowed a shop trip, for the case where
// it cannot kill anything to pay for one.
const gearTripEvery = 10 * time.Minute

// mayShop reports whether the run has earned another shop trip.
//
// A shop trip has to be paid for with a kill, and that rule exists because the buying loop
// starved the fighting it is supposed to serve. gearStep reports "done" after every trip,
// successful or failed, so Loop returns before it ever reaches the rung; a run with coins and a
// long gear ladder therefore shops, and only shops. Measured: a 90-minute run staged at melee 8
// with a 20,000 purse bought three pieces, failed several more trips, and took **zero kills in
// the entire window** — the script's whole purpose, starved by its own shopping.
//
// The first trip is free, because an account with nothing worth swinging should arm itself
// before it fights. After that a trip costs a kill, and a run that cannot find anything to kill
// still gets one every ten minutes, so a thin or blocked rung cannot freeze its gear for good.
func (b *combatTrainerBot) mayShop() bool {
	if b.gearTrips == 0 {
		return true
	}
	if b.kills > b.killsAtLastGearTrip {
		return true
	}
	return time.Since(b.lastGearTrip) >= gearTripEvery
}

// gearAttemptsPerSlot is how many failed trips a slot gets before the run stops attempting it.
//
// Three, because the failures worth retrying are the transient ones — a walk that lost its way,
// a keeper who had stepped behind a counter — and the ones that are not are permanent for the
// whole run. Measured case: the Al Kharid legs shop sits behind the toll gate, and the walker
// crossed that gate 122 times in one trip without ever reaching the shop. Retrying a fourth
// time buys nothing; what it costs is every slot further down the plan, because the plan is
// ordered and the run never got past the one it could not do.
const gearAttemptsPerSlot = 3

// gearStep makes at most one SHOP STOP per Loop call, so a stop request lands promptly and so a
// failing shop cannot monopolise the run. It reports done=true when it acted.
//
// A "buying run" here is one journey away from the rung that may visit several keepers before it
// walks back, and that shape is the fix for a measured starvation rather than a tidier way to
// write the same thing. Two arms from Defence 1 differing only in buyGear (COLLAB, 0a4385f)
// scored 68 XP/min with gear buying on against 506 with it off, on the same band with identical
// fighting: 19 shop trips bought 4 items and the Al Kharid toll gate was crossed 205 times
// against 9. The cause was three reasonable-looking rules multiplying together — BuyPlan is
// ordered by slot and those five slots' keepers stand in five different settlements, gearStep
// bought one slot per trip and then cleared haveRung so the account walked all the way back, and
// mayShop re-armed after a single kill. So five slots cost five cross-map round trips, each paid
// for by one ~20s kill.
//
// Now: the run asks the budget once per journey, visits the nearest keepers first buying
// everything each one can supply, and clears haveRung ONCE when the journey ends.
func (b *combatTrainerBot) gearStep(ctx *Context, d *combatrunner.Deps, l combatplan.Levels, style combatplan.Style) (bool, error) {
	// Shops across the water are planned only for an account that can pay the ships there and back
	// (combatplan.PlanReachable): live 7 Oct 2026 an Ardougne account chose Falador's and Barbarian
	// Village's shops with a few coins and made three failed trips to each.
	var here *tile.Tile
	if x, z, level, ok := ctx.Session.State().Tile(); ok {
		here = &tile.Tile{X: x, Z: z, Level: level}
	}
	plan, skippedShops := combatplan.PlanReachable(l, nil, combatrunner.WornNames(d), combatrunner.PackNames(d), b.unavailable, here, combatrunner.CoinsHeld(d))
	for _, sk := range skippedShops {
		b.warn(ctx, "farefar-"+sk.Shop, 10*time.Minute, "not shopping at %s (%s): it is across the water and the ships need %d coins there and back, %d held — choosing a shop on this side or training on",
			sk.Keeper, sk.Shop, sk.Fares, sk.Coins)
	}
	var wanted []combatplan.Purchase
	for _, p := range plan {
		if b.slotGaveUp[p.Slot] {
			continue
		}
		// A Ranged or Magic run buys no melee weapon upgrades. It cannot swing them, so every
		// such trip is a cross-map walk that costs the run a kill under the shopping rule and
		// buys nothing it will ever use. The armour slots below are still worth buying, because
		// a bow user takes the same hits a swordsman does. One blade in the pack is all the
		// sewer route needs, and a run that reached Ranged at all earned that during its melee
		// phase.
		if p.Slot == combatplan.SlotWeapon && !wearsMeleeWeapon(style) {
			continue
		}
		wanted = append(wanted, p)
	}

	// A host that is not having a walk made now (ScarabSwarm: the account is hurt with nothing to eat) holds the trip before anything is booked:
	// no run opened, no stop counted, no trip charged to the budget. With nothing wanted there is no trip to hold, and the run's close-out
	// below still happens.
	if len(wanted) > 0 && b.shopHeld(ctx) {
		return false, nil
	}

	// The pre-budget behaviour, kept deliberately so a before/after can be measured on ONE binary
	// with one variable rather than by comparing two builds with different instrumentation. It is
	// also the escape hatch if the budget ever misbehaves on a live roster.
	//
	// Faithful to what it replaces: one slot per trip, the trip priced at a single kill by
	// mayShop, and the walk home (haveRung) cleared on a successful purchase only — which is
	// exactly the combination that turned five gear slots into five cross-map round trips.
	if !b.cfg.gearBudget {
		if len(wanted) == 0 || !b.mayShop() {
			return false, nil
		}
		next := wanted[0]
		coins := combatrunner.CoinsHeld(d)
		if coins <= 0 {
			b.sayOnce(ctx, "nocoins", "no coins for the next upgrade (%s from %s) — training on in what we have until the drops pay for it. There is no mule to ask", next.Item.Name, next.Site.Keeper)
			return false, nil
		}
		b.vlog(ctx, "buying %s from %s at %d,%d (carrying %d coins)", next.Item.Name, next.Site.Keeper, next.Site.Stand.X, next.Site.Stand.Z, coins)
		b.gearTrips++
		b.lastGearTrip = time.Now()
		b.killsAtLastGearTrip = b.kills
		boughtBefore := b.bought
		done, err := b.buyOneItem(ctx, d, next)
		if b.bought > boughtBefore {
			b.haveRung = false // the shop is nowhere near the rung; re-derive the trip
		}
		return done, err
	}

	// The band the allowance is measured against. A rung change is a genuinely new allowance,
	// because the gear worth having is different on the other side of a boundary. Read from the
	// rung rather than the level so it cannot drift from what the run is actually fighting.
	if b.haveRung && b.rung.Key != b.shopBandKey {
		b.shopBandKey = b.rung.Key
		b.shopRunsThisBand = 0
	}

	if !b.shopRunOpen {
		v := combatplan.MayStartRun(combatplan.TripRequest{
			Budget:        combatplan.DefaultTripBudget,
			Band:          b.shopBandKey,
			RunsThisBand:  b.shopRunsThisBand,
			KillsSinceRun: b.kills - b.killsAtRunEnd,
			TripsThisRun:  b.gearTrips,
			WantToBuy:     len(wanted) > 0,
		})
		if !v.Allowed {
			if len(wanted) > 0 {
				b.warn(ctx, "shopbudget", 5*time.Minute, "staying at the rung: %s", v.Why)
			}
			return false, nil
		}
		b.shopRunOpen, b.shopStopsThisRun = true, 0
		b.shopRunsThisBand++
		ctx.logf("combattrainer: starting a buying run for %d slot(s) — %s", len(wanted), v.Why)
	}

	// The journey ends when there is nothing left to buy or the stop cap is reached. Both are
	// bounded and both shrink on their own: every stop below either buys something (removing it
	// from the plan), strikes an item the shelf does not have, or counts a failure toward
	// retiring the slot. That is what stops this becoming the fifth zero-progress loop in this
	// script — there is no latch here that a stuck state can hold true.
	if len(wanted) == 0 || !combatplan.MayMakeStop(combatplan.DefaultTripBudget, b.shopStopsThisRun) {
		b.closeShopRun(ctx, len(wanted))
		return false, nil
	}

	coins := combatrunner.CoinsHeld(d)
	if coins <= 0 {
		// No mule means no coin top-up: the run simply trains in what it has until its own
		// drops pay for the next tier. Said once, then never again.
		b.sayOnce(ctx, "nocoins", "no coins for the next upgrade (%s from %s) — training on in what we have until the drops pay for it. There is no mule to ask", wanted[0].Item.Name, wanted[0].Site.Keeper)
		b.closeShopRun(ctx, len(wanted))
		return false, nil
	}

	// Nearest keeper first, measured from where the account actually is rather than from the
	// rung: once the journey is under way the question is what is cheapest to reach from here,
	// and doing Falador, then Al Kharid, then Barbarian Village in plan order crosses the map
	// three times where distance order crosses it once.
	stops := combatplan.ClusterBySite(wanted)
	if x, z, level, ok := ctx.Session.State().Tile(); ok {
		stops = combatplan.OrderStopsFrom(stops, tile.Tile{X: x, Z: z, Level: level})
	}
	stop := stops[0]

	// Booked before the trip rather than after it, because a stop that fails still costs the run
	// its time and must still count against the journey's cap.
	b.shopStopsThisRun++
	b.gearTrips++
	b.lastGearTrip = time.Now()
	b.killsAtLastGearTrip = b.kills
	b.vlog(ctx, "buying run stop %d/%d: %s from %s at %d,%d (carrying %d coins)",
		b.shopStopsThisRun, combatplan.DefaultTripBudget.StopsPerRun, stop.Slots(), stop.Site.Keeper,
		stop.Site.Stand.X, stop.Site.Stand.Z, coins)

	// Everything this keeper can supply, while the account is standing at the counter. The second
	// and later items cost no travel at all: combatrunner.BuyFrom's own TravelTo returns straight
	// away when the account is already inside ShopStandRadius of the keeper's tile.
	for _, next := range stop.Buy {
		if _, err := b.buyOneItem(ctx, d, next); err != nil {
			return true, err
		}
	}
	return true, nil
}

// closeShopRun ends a buying journey and sends the account back to its rung.
//
// The haveRung clear happens HERE and nowhere else, which is the single most important line in
// this change: it used to fire after every individual purchase, so a five-slot gear ladder meant
// five separate walks home across the map. Now one journey pays for one walk home.
func (b *combatTrainerBot) closeShopRun(ctx *Context, stillWanted int) {
	if !b.shopRunOpen {
		return
	}
	b.shopRunOpen = false
	b.killsAtRunEnd = b.kills
	b.haveRung = false
	ctx.logf("combattrainer: buying run finished after %d stop(s) with %d slot(s) still wanted — walking back to the rung",
		b.shopStopsThisRun, stillWanted)
}

// buyOneItem is one purchase at a keeper the account is already standing at. It reports whether
// it bought anything; the journey carries on to the next item either way, because the remaining
// items at this stop cost no further travel.
func (b *combatTrainerBot) buyOneItem(ctx *Context, d *combatrunner.Deps, next combatplan.Purchase) (bool, error) {
	res, err := combatrunner.BuyFrom(d, next.Site, next.Item.Name, 1)
	if err != nil {
		if errors.Is(err, combatrunner.ErrNotOnShelf) {
			// Not a failure so much as a discovery, and one trip is enough to make it: the
			// keeper's whole shelf came back with the error, so everything this shop is listed
			// as selling in that slot but does not actually have is struck off at once. Marking
			// only the item we asked for would send the run back to the same shop for the next
			// tier down, and in this content that is three more wasted trips before it reaches
			// one the shelf has.
			onShelf := map[string]bool{}
			for _, n := range res.Shelf {
				onShelf[strings.ToLower(n)] = true
			}
			struck := 0
			for _, it := range combatplan.Items() {
				if it.Slot != next.Item.Slot || it.Shop != next.Item.Shop {
					continue
				}
				key := strings.ToLower(it.Name)
				if onShelf[key] || b.unavailable[key] {
					continue
				}
				b.unavailable[key] = true
				struck++
			}
			ctx.logf("combattrainer: %s does not stock %s — the gear table knows which shop lists an item, not how many it keeps. Struck %d %s item(s) this shelf does not have; the next trip takes the best tier it does",
				next.Site.Keeper, next.Item.Name, struck, next.Item.Slot)
			return true, nil
		}
		b.slotFails[next.Slot]++
		if b.slotFails[next.Slot] >= gearAttemptsPerSlot {
			b.slotGaveUp[next.Slot] = true
			ctx.logf("combattrainer: giving up on the %s upgrade for this run after %d failed trips to %s (%v) — the slots behind it in the plan are not held up by it",
				next.Slot, b.slotFails[next.Slot], next.Site.Keeper, err)
		} else {
			b.warn(ctx, "buy-"+next.Item.Name, time.Minute, "could not buy %s from %s (attempt %d of %d): %v",
				next.Item.Name, next.Site.Keeper, b.slotFails[next.Slot], gearAttemptsPerSlot, err)
		}
		ctx.Delay(2 * time.Second)
		return true, nil
	}
	if res.Bought <= 0 {
		// "The shop has run out of stock." is a verdict, not a setback, and telling it apart
		// from an empty purse is the difference between filling the shield slot and never
		// filling it. Cassie lists a mithril square shield at stock 0, and World.ts:1183 tops a
		// line up only TOWARD its stockcount, so a 0 never rises: no amount of waiting or coins
		// changes that answer. Treating it as affordability cost three cross-map trips to
		// Falador, each of which has to be paid for with a kill, before the plan dropped a tier
		// — which is longer than any run measured so far has lasted, so the shield was the one
		// slot this trainer could never fill. An empty purse still gets the retries, because
		// that refusal really does come right once the drops have paid for it.
		if outOfStockSaid(res.Said) {
			b.unavailable[strings.ToLower(next.Item.Name)] = true
			b.slotFails[next.Slot] = 0
			ctx.logf("combattrainer: %s keeps no %s at all — the server said %q, which is a permanently empty stock line rather than a price. Striking it now instead of after %d trips, and taking the best tier the shelf does keep",
				next.Site.Keeper, next.Item.Name, strings.TrimSpace(res.Said), gearAttemptsPerSlot)
			return true, nil
		}
		// The keeper had it listed and still would not sell it, which on this engine is an
		// empty stock line as often as it is an empty purse. Counted per slot so a shelf that
		// never restocks cannot hold the plan up for the whole run.
		b.slotFails[next.Slot]++
		if b.slotFails[next.Slot] >= gearAttemptsPerSlot {
			b.unavailable[strings.ToLower(next.Item.Name)] = true
			ctx.logf("combattrainer: %s would not sell us %s in %d attempts (last said %q) — striking that item and taking the next tier down",
				next.Site.Keeper, next.Item.Name, b.slotFails[next.Slot], res.Said)
			b.slotFails[next.Slot] = 0
			return true, nil
		}
		b.warn(ctx, "afford-"+next.Item.Name, time.Minute, "%s would not sell us %s — the server said %q. Training on and trying again when the drops have paid for it",
			next.Site.Keeper, next.Item.Name, res.Said)
		return false, nil
	}
	b.slotFails[next.Slot] = 0
	b.bought += res.Bought
	ctx.logf("combattrainer: bought %s from %s", next.Item.Name, next.Site.Keeper)
	combatrunner.EquipByName(d, next.Item.Name, 5*time.Second)
	// No haveRung clear here any more. The walk back to the rung belongs to the END of the
	// journey (closeShopRun), not to every individual purchase — clearing it here is what turned
	// a five-slot gear ladder into five separate round trips across the map.
	return true, nil
}

// outOfStockSaid reports whether a keeper's refusal was content's own empty-stock line. Matched
// on the phrase rather than the whole sentence because the server sends it with a trailing
// newline, and observed live at Cassie's counter as "The shop has run out of stock.\n".
func outOfStockSaid(said string) bool {
	return strings.Contains(strings.ToLower(said), "run out of stock")
}

// heartbeatEvery is how often the run says it is still alive, and stallWarnAfter how long it
// may go without a point of melee XP before that line becomes a warning.
//
// Why an unconditional line rather than a verbose one: a run's whole vocabulary — the fight
// heartbeat, the bank trip, the "waiting for a respawn" — is behind logDetail Verbose, so a
// Normal run that is working and a Normal run that is wedged look identical, and one measured
// arm was read as "0 CPU for ten minutes" when the log simply had nothing to say. A line a
// minute costs nothing and tells them apart. It fires from the top of Loop, so the longest
// silence it can leave is one Loop call: about two minutes for the regenerate wait and up to
// five for a cross-map shop trip, which is honest rather than perfect.
const (
	heartbeatEvery = time.Minute
	stallWarnAfter = 5 * time.Minute
	// embedStallEnd is how long a run inside a host (ScarabSwarm's Stage 5) may go without melee
	// experience before it hands back to the host to be restarted.
	embedStallEnd = 15 * time.Minute
)

// meleeXP totals the three stats this ladder trains, which is the only progress signal the bot
// cannot fabricate: it comes off the server's own stat table.
func (b *combatTrainerBot) meleeXP(ctx *Context) int {
	return b.skillXP(ctx, combatrunner.AttackStat) +
		b.skillXP(ctx, combatrunner.StrengthStat) +
		b.skillXP(ctx, combatrunner.DefenceStat)
}

// heartbeat prints one line a minute saying where the run is and whether it is still earning
// anything, and escalates to a STALLED line once the melee XP has not moved for a while.
func (b *combatTrainerBot) heartbeat(ctx *Context, d *combatrunner.Deps) {
	if time.Since(b.lastBeat) < heartbeatEvery {
		return
	}
	xp := b.meleeXP(ctx)
	if xp > b.bestXP {
		b.bestXP, b.lastProgressAt = xp, time.Now()
	}
	gained := xp - b.lastBeatXP
	if b.lastBeatXP == 0 {
		gained = 0
	}
	b.lastBeat, b.lastBeatXP = time.Now(), xp

	rung := "no rung yet"
	if b.haveRung {
		rung = b.rung.Key
	}
	hp, _ := ctx.Session.State().HP()
	where := "position unknown"
	if x, z, level, ok := ctx.Session.State().Tile(); ok {
		where = fmt.Sprintf("%d,%d,%d", x, z, level)
	}
	state := fmt.Sprintf("rung %s at %s, %d hp, %d kills, %d bank trips, %d deaths, %d melee xp (+%d this minute)",
		rung, where, hp, b.kills, b.trips, b.deaths, xp, gained)

	if stalled := time.Since(b.lastProgressAt); stalled >= stallWarnAfter {
		ctx.logf("combattrainer: STALLED: no melee xp for %s — %s", stalled.Round(time.Second), state)
		if b.embed != nil && stalled >= embedStallEnd {
			// A host restarts the run, which walks there afresh (the walker opens doors) and
			// re-picks its rung. Live 29 Sep: 76 Stage 5 accounts sat 40 minutes behind a shut
			// Al Kharid palace door or in a crowd at the Hill giants, "waiting for one we can reach".
			b.endRun(ctx, fmt.Sprintf("no melee xp for %s", stalled.Round(time.Minute)), false)
		}
		return
	}
	ctx.logf("combattrainer: alive %s — %s", time.Since(b.startedAt).Round(time.Second), state)
}

// waitWatching is Context.DelayUntil with the heartbeat still beating through the wait.
//
// It exists because the first version of the heartbeat was not enough, and the gap was measured
// rather than reasoned about. Beating from the top of Loop makes a beat DUE once a minute, but
// it can only LAND when a Loop call returns — and the longest waits in this script sit inside a
// single call. The out-of-food regenerate wait below is a flat 120 seconds of DelayUntil, so a
// run doing exactly what it should was still silent for 3m10s of a 12-minute proof, which is the
// shape of the ten-minute silence this whole change exists to make impossible.
//
// Honest limit: this fixes the wait it is used for. A walk to a rung can still hold one Loop
// call for TravelWalkTimeout (two minutes) and a cross-map shop trip for ShopTravelTimeout
// (five), because those block inside internal/scripts/combatrunner, which has no way to call
// back here. So the guarantee is "a sign of life at least once per Loop call, and once a minute
// through the waits this script owns", not "once a minute come what may".
func (b *combatTrainerBot) waitWatching(ctx *Context, d *combatrunner.Deps, cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		b.heartbeat(ctx, d)
		if !ctx.Delay(2 * time.Second) {
			return false
		}
	}
	return cond()
}

func (b *combatTrainerBot) warn(ctx *Context, key string, every time.Duration, format string, args ...any) {
	if time.Since(b.warnedAt[key]) < every {
		return
	}
	b.warnedAt[key] = time.Now()
	ctx.logf("combattrainer: WARNING: "+format, args...)
}

// foodPerTrip is this rung's own food load, and the number the break-off reserve is a fraction
// of. Sized from the monster's max hit by combatplan.FoodPerTrip.
func (b *combatTrainerBot) foodPerTrip() int {
	return combatplan.FoodLoad(b.rung, fighterfood.FoodHealAmount(b.cfg.food), combatrunner.PackSize)
}

// dangerFloor is the load the MONSTER justifies, with no term for the walk. It is what both the
// scarcity rule and the break-off fall back to once the bank has proven it cannot pay for a
// trip-sized load.
func (b *combatTrainerBot) dangerFloor() int {
	return combatplan.FoodPerTrip(b.rung, fighterfood.FoodHealAmount(b.cfg.food))
}

// breakOffLoad is the number the break-off reserve is a fraction of: the trip-sized load while the
// bank can still fill one, and the danger floor once it has proven it cannot. Asking for the big
// load and BREAKING OFF at a fraction of it are two different decisions, and only the first one
// survives an empty bank. See combatplan.BreakOffLoad for the 29-trip spin that made this
// distinction necessary.
func (b *combatTrainerBot) breakOffLoad() int {
	return combatplan.BreakOffLoad(b.foodPerTrip(), b.dangerFloor(), !b.foodShort)
}

// atRung reports whether the account is already standing inside the chosen rung's own fighting
// area, which is what makes a walk back to the stand unnecessary. See the call site in Loop.
//
// Within the leash is not enough at a fenced pen: the road outside the fence is within the leash and no cow is a walk from it
// (combatplan/pen.go), so there the account must also stand inside the fence.
func (b *combatTrainerBot) atRung(ctx *Context, d *combatrunner.Deps) bool {
	if !b.haveRung {
		return false
	}
	x, z, level, ok := ctx.Session.State().Tile()
	if !ok {
		return false
	}
	here := tile.Tile{X: x, Z: z, Level: level}
	if here.Level != b.rung.Stand.Level || tile.Chebyshev(here, b.rung.Stand) > b.rung.Leash {
		return false
	}
	if g := b.penOf(d); g != nil && !g.Inside(here) {
		return false
	}
	return true
}

// travelBudget is how long a walk to dest is allowed to take, sized from how far it actually
// has to go. See combatrunner.TravelBudget for why one fixed timeout was not enough.
func (b *combatTrainerBot) travelBudget(ctx *Context, dest tile.Tile) time.Duration {
	x, z, level, ok := ctx.Session.State().Tile()
	if !ok {
		return combatrunner.TravelWalkTimeout
	}
	return combatrunner.TravelBudget(tile.Tile{X: x, Z: z, Level: level}, dest)
}

// needsBank is true when the pack cannot hold another drop, or the food is down to the reserve
// and the bank has not already been proven unable to top it up.
//
// The decision itself lives in combatplan.NeedsBank, pure and tested, and it answers WHICH rule
// fired as well as whether. That second half is not decoration: the 273-trip spin at the giants
// rung could not be diagnosed from its own log because the three reasons for banking were three
// anonymous boolean branches, so 273 identical lines said "bank trip" and none said why.
func (b *combatTrainerBot) needsBank(d *combatrunner.Deps) (bool, combatplan.BankReason) {
	used, _ := packContents(d)
	return combatplan.NeedsBank(combatplan.BankNeed{
		Food:        b.foodCount(d),
		FoodPerTrip: b.breakOffLoad(),
		PackUsed:    used,
		PackSize:    combatrunner.PackSize,
		HaveRung:    b.haveRung,
		FoodEmpty:   b.foodEmpty,
		FoodScarce:  b.foodScarce,
		PackStuck:   b.packStuck,
	})
}

func (b *combatTrainerBot) bankStep(ctx *Context, d *combatrunner.Deps, why combatplan.BankReason) {
	bank, ok := combatrunner.OpenBankNear(d, b.rung.Bank, 6)
	if !ok {
		b.warn(ctx, "bank-open", time.Minute, "could not open the bank at %d,%d — retrying", b.rung.Bank.X, b.rung.Bank.Z)
		ctx.Delay(3 * time.Second)
		return
	}
	worn := combatrunner.WornNames(d)
	keep := func(name string) bool {
		if fighterfood.IsFoodItem(name, b.cfg.food) || strings.EqualFold(name, "Coins") {
			return true
		}
		if b.embed != nil && b.embed.keepExtra != nil && b.embed.keepExtra(name) {
			return true
		}
		for _, w := range worn {
			if strings.EqualFold(w, name) {
				return true
			}
		}
		// Bones are kept only to be buried, and burying happens at the rung, not here.
		return false
	}
	slotsBefore, namesBefore := packContents(d)
	dep := combatrunner.DepositAllExcept(d, bank, keep)
	slotsAfter, _ := packContents(d)

	// The same number the break-off reserve is a fraction of, so a trip cannot come back holding
	// less than the rule that sent it would immediately ask for again.
	want := b.foodPerTrip()
	wd := combatrunner.WithdrawTo(d, bank, b.cfg.food, want)
	got := wd.After
	b.announceFoodBudget(ctx, d, bank)
	if b.embed != nil && b.embed.atBank != nil {
		b.embed.atBank(bank) // the host's own draw (ScarabSwarm's run staller jewellery), after the food
	}
	_ = bank.Close()
	b.trips++

	// Bank trips are counted against the one progress signal this script cannot fabricate: the
	// server's own melee XP. A trip that follows a kill resets the count; trips that follow
	// nothing accumulate, and the guard at the end of this function ends the run when they reach
	// the limit. Read here rather than on a timer because a trip is the event being counted.
	xp := b.meleeXP(ctx)
	if xp > b.xpAtLastTrip {
		b.tripsSinceXP = 0
	} else {
		b.tripsSinceXP++
	}
	b.xpAtLastTrip = xp

	// The latch that stops a distance-sized load from becoming a permanent reason to bank. A trip
	// that could not reach the load has had its answer: that number is not available to this run.
	// It stops being what the break-off is measured against until a later trip proves otherwise,
	// so the run goes back to fighting on the margin the monster justifies and reaches the clean
	// stop rather than walking back in. Measured before it was fixed: 29 trips in five minutes
	// with 0 kills, and then — once a pack that could not be emptied was exempted from the latch —
	// 351 trips and a 44-minute frozen window. See combatplan.JudgeTripLoad for why there is no
	// longer an exemption for any of it.
	switch reach := combatplan.JudgeTripLoad(got, want, wd.PackFull, wd.NotInBank); {
	case !reach.Reached && !b.foodShort:
		b.foodShort = true
		ctx.logf("combattrainer: this trip came away with %d %s against the %d this rung's trip is sized for — %s. Measuring the break-off against the %d-bite danger floor instead, so we fight on rather than walk straight back in",
			got, b.cfg.food, want, reach.Why, b.dangerFloor())
	case reach.Reached && b.foodShort:
		b.foodShort = false
		b.vlog(ctx, "a whole %d-bite trip load again — the break-off goes back to the trip-sized number", want)
	}

	// The pack AFTER the withdrawal, which is a different reading from the pack after the
	// deposit: withdrawing food fills those slots straight back up. The first fixed run measured
	// what confusing the two costs — the pack oscillated 25/28 -> 28/28 twice a cycle, so the
	// stuck latch cleared on the post-deposit reading and re-latched on the very next pass, seven
	// times in twelve minutes. Whether the pack is stuck is a question about the pack the run
	// walks AWAY with.
	slotsFinal, namesFinal := packContents(d)

	trip := combatplan.BankTrip{SlotsBefore: slotsBefore, SlotsAfter: slotsAfter, Withdrew: wd.Gained()}
	stuckTrip := combatplan.BankTrip{SlotsBefore: slotsBefore, SlotsAfter: slotsFinal, Withdrew: wd.Gained()}

	// Every number here is a delta or a pair of readings rather than a total, because totals are
	// what made the spin unreadable: 273 trips all logged "carrying 3 Trout" and 3 was simply
	// what the account walked in with. The side panel is reported next to the backpack component
	// so that a divergence between the two views — they are different interface components
	// listening to the same inventory — shows up in the log instead of being a silent wrong read.
	b.vlog(ctx, "bank trip %d (%s) — deposited %d kind(s), freed %d slot(s), withdrew %d, carrying %d %s; pack %d/%d -> %d/%d, side panel %d -> %d",
		b.trips, why, dep.Clicked, dep.Freed(), wd.Gained(), got, b.cfg.food,
		slotsBefore, combatrunner.PackSize, slotsAfter, combatrunner.PackSize, dep.PanelBefore, dep.PanelAfter)

	if dep.PackAfter != dep.PanelAfter {
		b.warn(ctx, "panel-divergence", time.Minute,
			"the backpack component and the bank's side panel disagree after the deposit: %d slots against %d. They listen to the same inventory, so one of the two readings this script makes its decisions from is stale",
			dep.PackAfter, dep.PanelAfter)
	}
	if dep.Unnamed > 0 {
		b.warn(ctx, "unnamed-pack-items", time.Minute,
			"%d pack slot(s) hold an object id the config cannot name, so the deposit skipped them and nothing will ever bank them",
			dep.Unnamed)
	}

	// The latch the pack half never had. A trip that leaves the pack full having freed nothing
	// has proven that this pack cannot be emptied, so it must stop counting as a reason to come
	// back — otherwise needsBank is true forever and the run banks instead of training.
	// Judged on the pack the run walks away with, and announced ONCE per run. The latch itself
	// may toggle legitimately — eating a bite frees a slot — but a run that announces every
	// toggle is only a quieter version of the same noise, and the first fixed arm printed this
	// line seven times in twelve minutes for exactly that reason.
	if combatplan.PackIsStuck(stuckTrip, combatrunner.PackSize) {
		if !b.packStuck {
			b.packStuck = true
			b.stuckNames = namesFinal
		}
		b.sayOnce(ctx, "packstuck",
			"the pack is full and banking cannot empty it, so a full pack stops counting as a reason to bank. Deposited %d kind(s) and freed %d slot(s); walking away holding [%s], carried in [%s]. The keep-filter keeps worn gear, coins and food, so anything here matching those will never be banked",
			dep.Clicked, dep.Freed(), strings.Join(namesFinal, ", "), strings.Join(namesBefore, ", "))
	} else if b.packStuck && slotsFinal < combatrunner.PackSize {
		b.packStuck = false
		b.vlog(ctx, "the pack has room again (%d/%d) — a full pack counts as a reason to bank once more", slotsFinal, combatrunner.PackSize)
	}

	if wd.PackFull {
		b.warn(ctx, "withdraw-packfull", 30*time.Second,
			"could not withdraw %s: the pack is full at %d/%d. Content's bank_withdraw_request answers that with \"You don't have enough inventory space.\" and moves nothing, which is why this used to read as a timeout",
			b.cfg.food, slotsAfter, combatrunner.PackSize)
	}

	// The scarcity rule is judged against the DANGER floor, not against the trip-sized load.
	// See bankFoodPolicy: one of these two numbers decides how much to carry and the other
	// decides whether the account can still feed itself at all, and they are not the same
	// question.
	if b.bankFoodPolicy(ctx, dep, wd, combatplan.FoodPerTrip(b.rung, fighterfood.FoodHealAmount(b.cfg.food)), got) {
		return
	}

	// The floor, and it is unconditional on purpose.
	//
	// The previous version only reached this when the trip had withdrawn nothing AND the bank was
	// known empty, so a trip that withdrew a single item skipped it entirely — which is exactly
	// how the giants-rung spin ran at full speed while "carrying 3 Trout". The question that
	// decides whether walking straight back in is sane is not how much food is held, it is
	// whether this trip changed anything at all. Bug class 2 in docs/script_porting_framework.md,
	// now fixed in the third place this script has grown it.
	if !combatplan.TripMadeProgress(trip) {
		b.warn(ctx, "bank-spin", 30*time.Second,
			"bank trip %d (%s) freed no slot and gained nothing — waiting rather than walking straight back in. Pack %d/%d, %d kind(s) clicked, %d stuck: [%s]",
			b.trips, why, slotsAfter, combatrunner.PackSize, dep.Clicked, len(dep.Stuck), strings.Join(dep.Stuck, ", "))
		ctx.Delay(10 * time.Second)
	}

	// The last line of defence, and the only one in this file that does not care what went wrong.
	//
	// Every fix above closes one specific pair of rules that deadlocked, and there have been five
	// of those. This one closes the SHAPE instead: bank trips accumulating while the melee XP
	// does not move. It is deliberately placed after the food policy, so a run that has a
	// specific, better reason to stop gives that reason instead of this one; and it is
	// deliberately not conditional on the food settings, because a wedged run is wedged whatever
	// it was configured to do about food.
	if stop, reason := combatplan.BankLoopExhausted(combatplan.BankLoop{
		TripsSinceXP: b.tripsSinceXP,
		Limit:        combatplan.BankLoopLimit,
		Why:          why,
		Food:         combatrunner.FoodCount(d, b.cfg.food),
		BreakOff:     b.breakOffLoad(),
		PackUsed:     slotsFinal,
		PackSize:     combatrunner.PackSize,
	}); stop {
		b.stopReason = reason
		ctx.logf("combattrainer: STOPPING — %s. Standing at a bank making trips is not training, and this script has grown that loop five times, so it ends here with its gear worn and %d kills rather than adding to the count. Restock the bank, empty the pack, or pick a rung this account can sustain",
			b.stopReason, b.kills)
		b.endRun(ctx, b.stopReason, false)
	}
}

// announceFoodBudget says once, at the first bank trip, how much food is actually banked against
// how much the goals need.
//
// The 2026-09-20 end-to-end run reached Defence 42 of a wanted 45 and stopped on the food policy
// three hours in, and nothing before that moment had said the supply was short — the number was
// knowable at the first bank trip and nobody was told. With no mule and no reachable shop that
// stocks cooked food, the bank supply staged at the start is the entire supply for the run, so
// what it is sized for belongs in the log at the top rather than in the post mortem.
func (b *combatTrainerBot) announceFoodBudget(ctx *Context, d *combatrunner.Deps, bank *actions.Bank) {
	if b.said["foodbudget"] || d.Config == nil {
		return
	}
	banked := 0
	for _, it := range bank.Items() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && fighterfood.IsFoodItem(obj.Name, b.cfg.food) {
			banked += int(it.Count)
		}
	}
	need := 0
	for _, g := range []struct {
		stat int
		goal int
	}{
		{combatrunner.AttackStat, b.cfg.goals.Attack},
		{combatrunner.StrengthStat, b.cfg.goals.Strength},
		{combatrunner.DefenceStat, b.cfg.goals.Defence},
	} {
		if short := levels.XPAtLevel(g.goal) - b.skillXP(ctx, g.stat); short > 0 {
			need += short
		}
	}
	budget := combatplan.FoodBudget(need, fighterfood.FoodHealAmount(b.cfg.food))
	verdict := "which should see the melee goals out"
	if banked+combatrunner.FoodCount(d, b.cfg.food) < budget {
		verdict = "which is SHORT: this run will stop on the food policy before it gets there unless the bank is restocked"
	}
	b.sayOnce(ctx, "foodbudget",
		"the bank holds %d %s and the melee goals are %d XP away, which at this ladder's measured %d XP per hitpoint healed wants about %d bite(s) — %s",
		banked, b.cfg.food, need, combatplan.XPPerHPHealed, budget, verdict)
}

// bankFoodPolicy applies the food rules to a completed trip and reports whether it ended the run.
//
// Split out of bankStep so that every path through the food policy is followed by the anti-spin
// floor rather than returning past it. The old shape returned straight out of the "we got some
// food" branch, which is why the floor below it never ran in the measured spin.
// floor is the DANGER load — what the monster's max hit justifies — and deliberately not the
// trip-sized load the withdrawal asked for. The two answer different questions and conflating
// them is a regression this nearly shipped: once a load is sized from the walk as well as the
// danger, the cows rung asks for 20 bites where it used to ask for 4, and FoodReserve of 20 is
// 5. Judging scarcity against that number would stop a fresh account whose bank holds 4 Trout —
// an account that trained perfectly well before, because a cow hits for 1. Carrying more because
// the bank is far away must never make the run pickier about how much food is enough to fight.
func (b *combatTrainerBot) bankFoodPolicy(ctx *Context, dep combatrunner.Deposit, wd combatrunner.Withdrawal, floor, got int) bool {
	if got > 0 {
		b.foodEmpty = false
		// A bank that cannot fill us past the reserve is the end of the food supply, and the run
		// should end with it rather than fight its last bites down.
		//
		// The first version of this latched a "scarce" flag and carried on to the empty-pack stop
		// below, to avoid breaking off every pass. That stopped the bank spin and did nothing at
		// all about the deaths, which was the whole point: a measured long run with this fix
		// banked at the reserve, found 2 Trout, fell back to fighting to zero, and died FOUR
		// times before the empty-pack stop fired — against five deaths before any of this. Those
		// two bites are the last food this account will ever have, because there is no mule and
		// every cooked-food stock line in reach is 0, so fighting them down only donates levels to
		// the respawn. Stopping here with food still in the pack is the honest end.
		// A herd rung needs more than the reserve: the guardian's bites while an event beside the account is
		// handled come out of the same pack. Two trips of the Ice warrior cave's sent accounts in with 11 and 5 of
		// the 20 bites the trip is sized for ("fight on at the 5-bite danger floor") and both ate the pack in three
		// to ten minutes, then died at food 0. The run ends and the rung is avoided (combatplan.AvoidEnd), so the
		// next pick goes where the bank can pay for the account.
		if herd := combatplan.HerdFoodFloor(b.rung, fighterfood.FoodHealAmount(b.cfg.food)); herd > 0 && got < herd && !b.cfg.fightWithoutFood {
			b.stopReason = fmt.Sprintf("%s: the bank gave %d %s and rung %q, where an event monster beside the account has no way out of the herd, needs %d",
				combatplan.HerdFoodShort, got, b.cfg.food, b.rung.Key, herd)
			ctx.logf("combattrainer: STOPPING — %s. Fighting a herd on a thin pack is what killed two accounts in the Ice warrior cave, so this run ends here with its gear worn and %d kills; the next pick leaves the rung alone for a while",
				b.stopReason, b.kills)
			b.endRun(ctx, b.stopReason, false)
			return true
		}
		if combatplan.ShouldRestock(got, floor) && !b.cfg.fightWithoutFood {
			b.stopReason = fmt.Sprintf("the bank could only give us %d %s, at or under the %d-bite reserve for rung %q, and there is no other source of food",
				got, b.cfg.food, combatplan.FoodReserve(floor), b.rung.Key)
			ctx.logf("combattrainer: STOPPING — %s. Fighting the last bites down is what killed four accounts in one measured run, so this run ends here with its gear worn and %d kills. Restock the bank, or set \"fightWithoutFood\" to carry on regardless",
				b.stopReason, b.kills)
			b.endRun(ctx, b.stopReason, false)
			return true
		}
		if combatplan.ShouldRestock(got, floor) {
			if !b.foodScarce {
				b.foodScarce = true
				ctx.logf("combattrainer: the bank could only give us %d %s, at or under the %d-bite reserve for rung %q — fighting on anyway because \"fightWithoutFood\" is set",
					got, b.cfg.food, combatplan.FoodReserve(floor), b.rung.Key)
			}
		} else if b.foodScarce {
			b.foodScarce = false
			ctx.logf("combattrainer: the bank filled us back to %d %s — breaking off at the reserve again", got, b.cfg.food)
		}
		return false
	}

	// The bank has no food, and with no mule there is nowhere else to get any: every cooked-food
	// stock line in the shops a free-to-play combat account can reach is 0, and World.ts:1183
	// tops a line up only TOWARD its stockcount, so a 0 never rises. There is no trip that fixes
	// this, which is why the default is to stop rather than to carry on.
	//
	// Carrying on was measured and it is not survivable: that arm died 18 times, 13 of them after
	// the food ran out, re-gearing from the pack and walking back each time to die again. A run
	// that cannot feed itself donates its levels to the respawn.
	if !b.cfg.fightWithoutFood {
		b.stopReason = fmt.Sprintf("out of %s: the pack is empty, the bank has none, and no shop this run can reach stocks any", b.cfg.food)
		ctx.logf("combattrainer: STOPPING — %s. Fighting on costs levels to the respawn (a measured run died 13 times that way), so this run ends here with its gear worn and %d kills. Restock the bank, or set \"fightWithoutFood\" to carry on regardless",
			b.stopReason, b.kills)
		b.endRun(ctx, b.stopReason, false)
		return true
	}

	b.foodEmpty = true
	b.sayOnce(ctx, "nofood", "no %q left in the bank and no mule to ask for more — fighting on without food because \"fightWithoutFood\" is set, which the %s rung's max hit of %d makes risky",
		b.cfg.food, b.rung.Key, b.rung.Monster.MaxHit)

	// The zero-progress floor that used to live here now runs in bankStep for EVERY trip, not
	// only for a trip that withdrew nothing. That older shape is what let the giants-rung spin
	// run at full speed: it withdrew a Trout each time, so it returned before ever reaching this
	// point. See bankStep's own note.
	return false
}

// packContents is how many pack slots are in use and what is in them, for the bank diagnostic
// above. Quantities are per name rather than per slot, so twelve Trout read as one entry.
func packContents(d *combatrunner.Deps) (int, []string) {
	inv := d.Session.State().Inventory(combatrunner.PlayerPack)
	if inv == nil {
		return 0, nil
	}
	used := inv.NonEmpty()
	count := map[string]int{}
	var order []string
	for _, it := range used {
		name := fmt.Sprintf("id %d", it.ID)
		if d.Config != nil {
			if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Name != "" {
				name = obj.Name
			}
		}
		if count[name] == 0 {
			order = append(order, name)
		}
		count[name] += int(it.Count)
	}
	out := make([]string, 0, len(order))
	for _, n := range order {
		out = append(out, fmt.Sprintf("%s x%d", n, count[n]))
	}
	return len(used), out
}

// eatFracNow is the share of the maximum hitpoints the trainer eats under now: its setting, raised after a view lag (lageat-1018) to
// what the monsters facing it could have done in the worst recent episode (fighterfood.LagEatFrac): the rung's monster at its
// content max hit, or fighterfood.LagUnseenHit when the rung is not known, once for each NPC facing the account and at least one,
// up to 70%. With no lag in the memory, or hitpoints over the lag cap, it is the setting as it was. hp is the hitpoints just read.
func (b *combatTrainerBot) eatFracNow(ctx *Context, hp int) float64 {
	frac := b.cfg.eatFrac
	st := ctx.Session.State()
	if st == nil {
		return frac
	}
	lag := st.RecentViewLag().Ticks
	maxHP, ok := st.MaxHP()
	if lag <= 0 || !ok || maxHP <= 0 || hp > fighterfood.LagCap(maxHP) {
		return frac
	}
	hit := fighterfood.LagUnseenHit
	if b.haveRung && b.rung.Monster.MaxHit > 0 {
		hit = b.rung.Monster.MaxHit
	}
	facing, self := 0, st.Self()
	for _, n := range st.NPCs() {
		if n.TargetsPlayer(self) {
			facing++
		}
	}
	return fighterfood.LagEatFrac(maxHP, frac, hit*max(1, facing), lag)
}

// fightStep runs one bounded fighting session. Bounded so a newly-due bank trip, a death or a
// stop request is picked back up by the ordinary dispatch instead of waiting out a whole rung.
func (b *combatTrainerBot) fightStep(ctx *Context, d *combatrunner.Deps) {
	// Run is on between fights: the walk to the next target and to a drop is at two tiles a tick, and the server
	// turns it off itself at 0 energy ("combatspeed", 7 Oct 2026).
	combatrunner.EnsureRun(d)
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Stopped() || ctx.Session.State().IsDead() {
			return
		}
		hp, _ := ctx.Session.State().HP()
		maxHP, _ := ctx.Session.State().MaxHP()
		eatFrac := b.eatFracNow(ctx, hp)
		if fighterfood.ShouldEat(fighterfood.EatInput{
			HP: hp, MaxHP: maxHP, Heal: fighterfood.FoodHealAmount(b.cfg.food),
			FoodCount: b.foodCount(d),
			Mode:      fighterfood.EatThreshold, ThresholdFrac: eatFrac,
		}) {
			if b.eatOne(d) { // combatrunner.EatOnce unless an embedding host passed its own eater
				b.eats++
			}
			continue
		}
		// Break off to the bank while there is still food in the pack. The branch below this one
		// fires only once the LAST bite is gone and the account is already under its eat
		// threshold, which is far too late to be a defence: a measured long run
		// (-mode long -buygear=false) put all five of its deaths in the last seventy log lines,
		// having fought its food all the way down and died repeatedly on the way. Leaving at the
		// reserve means the walk to the bank still has something to eat, so a healthy run never
		// reaches the branch below at all.
		if !b.foodEmpty && !b.foodScarce && combatplan.ShouldRestock(b.foodCount(d), b.breakOffLoad()) {
			b.warn(ctx, "restock", 30*time.Second,
				"down to %d %s at %d%% hp against a %d-bite reserve for rung %q — breaking off to the bank while we can still eat on the way",
				b.foodCount(d), b.cfg.food, int(combatrunner.HPFraction(d)*100),
				combatplan.FoodReserve(b.breakOffLoad()), b.rung.Key)
			return
		}
		// Out of food and hurt.
		if b.foodCount(d) == 0 && combatrunner.HPFraction(d) < eatFrac {
			if !b.foodEmpty {
				// The bank still has food: go and get it.
				ctx.logf("combattrainer: out of food at %d%% hp — breaking off to bank", int(combatrunner.HPFraction(d)*100))
				return
			}
			// The bank has none either, and with no mule there is nowhere else to get any.
			// Waiting to regenerate is the only move left. Returning from here WITHOUT a wait
			// is what turned this branch into a zero-delay busy loop on its first live run:
			// needsBank is already false once the bank is known empty, so Loop re-entered
			// fightStep and hit this same line again immediately, dozens of times a second,
			// and the run took 0 kills in three minutes. That is bug class 2 in
			// docs/script_porting_framework.md, reproduced here and fixed here.
			b.warn(ctx, "regen", time.Minute,
				"no %q in the pack or the bank and %d%% hp — waiting to regenerate rather than fighting on",
				b.cfg.food, int(combatrunner.HPFraction(d)*100))
			// waitWatching rather than ctx.DelayUntil: this is the longest wait the script owns,
			// and a flat DelayUntil through it is what left a working run silent for 3m10s of a
			// 12-minute proof. See waitWatching's own note.
			b.waitWatching(ctx, d, func() bool {
				return combatrunner.HPFraction(d) >= 0.9 || b.foodCount(d) > 0
			}, 120*time.Second)
			return
		}
		// A bone is buried as soon as nothing living is on the account: the kill's settle (killSettle, the tick or two
		// before content lets the next attack through) is time the pick cannot use, and a bone buried inside it is a
		// bone not buried between two fights ("combatspeed", 7 Oct 2026).
		//
		// NOT at a Herd rung (combatplan.Rung.Herd): a buried bone is where a Shade or a Zombie comes from (content
		// bury_bone.rs2 runs macro_event_prayer, which spawns one for one bury in ten each when the account has been
		// still a while), and in a cave of aggressive Ice warriors an event monster beside the account has no way
		// out. Seven bones were buried in the run whose Shade killed an account on 17 Oct 2026; the plain Bones
		// there are worth 4.5 Prayer experience each.
		if combatrunner.HasBone(d) && !b.rung.Herd && !b.underFire(ctx) {
			if combatrunner.BuryOneBone(d) {
				b.buried++
			}
			continue
		}
		if b.lootStep(ctx, d) {
			continue
		}
		if b.stillUnderAttack(ctx) {
			// A fight says nothing while it is going well, and that turned out to matter: one
			// band proof sat at a single kill for four and a half minutes with verbose logging
			// on and wrote not one line, so a long fight and a wedged run looked identical from
			// the log. A heartbeat costs nothing and tells them apart.
			if time.Since(b.warnedAt["fighting"]) >= 15*time.Second {
				b.warnedAt["fighting"] = time.Now()
				hp, _ := ctx.Session.State().HP()
				b.vlog(ctx, "still in combat at %d hp, %d kills so far on rung %q", hp, b.kills, b.rung.Key)
			}
			ctx.Delay(600 * time.Millisecond)
			continue
		}

		// An account that has ended up outside a fenced pen (a walk cut short, a random event) has no monster to pick: it leaves the
		// fight for Loop, which walks it in by the gate.
		if b.strayedFromPen(ctx, d) {
			return
		}
		npc, ok := b.pickTarget(ctx, d)
		if !ok {
			b.vlog(ctx, "no reachable %s within %d tiles of the stand — waiting for a respawn", b.rung.Monster.Name, b.rung.Leash)
			if b.embed != nil && b.embed.idle != nil && b.embed.idle(b.lastKillAt, b.rung.Monster.RespawnTicks) {
				continue
			}
			// At a fenced pen the wait is spent walking its beat, which shows the account the cows its view of one corner does not.
			if b.walkBeat(ctx, d) {
				continue
			}
			// A tick, not a second: the spawn that has just come back is clicked in the tick it appears.
			ctx.Delay(600 * time.Millisecond)
			continue
		}
		b.penFoundTarget()
		// combatrunner.FightOne, watched between its polls when an embedding host asked for it
		// (combattrainer_embed.go); the registered run's fight is unchanged.
		res, watch := b.fightOne(ctx, d, npc)
		if watch != nil {
			b.eats += watch.bites
			if watch.brokeOff && watch.atReserve {
				// A bite took the food down to the reserve: leave the fight, and let this loop's own
				// eat and restock rules feed the account and send it to the bank.
				continue
			}
			if watch.brokeOff {
				// Not the target's fault: leave it off the unreachable list, and let Loop's
				// bank step (the pack holds no food now) walk the account away.
				b.warn(ctx, "brokeoff", 30*time.Second,
					"broke off the fight with the %s at %d hp: no %s left to eat and under the %d%% eat threshold — leaving for the bank",
					b.rung.Monster.Name, watch.brokeAt, b.cfg.food, int(b.cfg.eatFrac*100))
				return
			}
		}
		if b.pickedDoors > 0 {
			b.noteDoorTarget(ctx, npc, res)
		}
		if !res.Attacked {
			b.noteUnreachable(ctx, npc)
		}
		switch res.Refused {
		case combatrunner.RefusedContested:
			// Another player's fight: leave that one alone for a while and pick the next.
			b.noteContested(ctx, npc)
			continue
		case combatrunner.RefusedUnderAttack:
			// Something else is on the account; the server lets it fight only that one, and the
			// gate above waits it out. The target is not at fault.
			b.vlog(ctx, "attack on the %s at %d,%d refused: already under attack", b.rung.Monster.Name, npc.X, npc.Z)
			continue
		}
		if res.NotOurs && (res.Verdict == combatplan.Killed || res.Verdict == combatplan.LikelyKilled || res.Verdict == combatplan.Vanished) {
			b.vlog(ctx, "the %s at %d,%d died without a hit from us — another player's kill, not counted", b.rung.Monster.Name, npc.X, npc.Z)
			continue
		}
		switch res.Verdict {
		case combatplan.Killed, combatplan.LikelyKilled:
			b.lastKillAt = time.Now()
			b.doors.Kill()
			b.kills++
			b.killSites = append(b.killSites, res.Site)
			if len(b.killSites) > 16 {
				b.killSites = b.killSites[len(b.killSites)-16:]
			}
			b.vlog(ctx, "kill %d (%s, %s)", b.kills, b.rung.Monster.Name, res.Verdict)
		case combatplan.Vanished:
			b.vlog(ctx, "target vanished without a confirmed death — not counting it as a kill")
		default:
			// Alive or Unknown: the fight ended with the target still standing. This branch
			// wedged an entire window, and it wedged it SILENTLY, because the switch had no case
			// for either verdict and so wrote nothing at all.
			//
			// What put a run here was the "tiles must touch" rule from the other side: FightOne
			// skipped its approach walk whenever the target was already Chebyshev 1 away, so it
			// never asked the pathfinder, and Chebyshev 1 includes a tile on the far side of a
			// palace wall. That is now checked in FightOne and in pickTarget, before a click
			// goes out, so what is left here is the honest case — a reachable monster the run
			// swung at for the whole timeout without killing it. It is still retired for a
			// window, because whatever the reason, trying the same one again immediately is how
			// a run spends ninety seconds at a time getting nowhere.
			if res.Attacked {
				b.vlog(ctx, "no kill after %s on the %s at %d,%d (verdict %q)",
					fightTimeout, b.rung.Monster.Name, npc.X, npc.Z, res.Verdict)
				b.noteUnreachable(ctx, npc)
			}
		}
	}
}

// unreachableFor is how long a target the run could not walk to is passed over. Long enough
// that the run gets on with something it can fight, short enough that a monster which has since
// wandered back out of its room is tried again rather than written off for the session.
const unreachableFor = 60 * time.Second

// fightTimeout is how long one FightOne waits for a kill before giving up on that attempt. It is
// a named constant so the line reporting a fight that ended with the target still standing quotes
// the same number the call actually used, rather than a literal that can drift away from it.
const fightTimeout = 90 * time.Second

// pickTarget is the nearest monster inside the leash that this run can actually walk to.
//
// Nearest alone is what wedged two long windows, and the Al Kharid rung is where it shows: the
// stand is outside the palace wall and several of its verified spawns sit inside it, so the
// closest warrior by straight-line distance is regularly one with no route to it. An escalating
// pass-over used to paper over that — it had to, because a flat window simply handed the same
// walled-in warrior back the moment it expired — and it is gone now, because the run asks the
// pathfinder instead of guessing. Candidates are tried nearest first and the first one with a
// route wins, so the healthy case costs exactly one bounded search.
//
// A route may cross doors. The rungs at the Al Kharid palace, Falador castle and the Barbarian Village long hall
// have most of their spawns in rooms, and the stand each walks to is within three tiles of a wall, so an account
// arrived on one side of a door and read every monster on the other as "no route" (live 14 Oct 2026, 04:05-04:45:
// 1,049 such warnings and 399 waits for "one we can reach"). A monster on this side of every door always beats one
// behind a door; only when none is left is the one with the cheapest route through the doors taken
// (combatplan.PickDoorTarget), at most DoorStrikeLimit times between two kills, so a crossing is never a shuttle.
func (b *combatTrainerBot) pickTarget(ctx *Context, d *combatrunner.Deps) (gamestate.NPC, bool) {
	b.pickedDoors = 0
	here := b.rung.Stand
	if x, z, level, ok := ctx.Session.State().Tile(); ok {
		here = tile.Tile{X: x, Z: z, Level: level}
	}
	now := time.Now()
	at := func(n gamestate.NPC) tile.Tile { return tile.Tile{X: n.X, Z: n.Z, Level: n.Level} }

	var cands []gamestate.NPC
	for _, n := range combatrunner.MonstersNear(d, b.rung.Monster.HerdKey(), b.rung.Stand, b.rung.Leash) {
		if last, skipped := b.unreachable[n.Index]; skipped && now.Sub(last) < unreachableFor {
			continue
		}
		cands = append(cands, n)
	}
	st := ctx.Session.State()
	multi, known := st.Multiway()
	// A fenced pen picks by the walk over open ground and goes through no door: its one gate is crossed on the way in (travelToRung).
	if g := b.penOf(d); g != nil {
		return b.pickPenTarget(ctx, g, here, cands, st.Self(), multi, known)
	}
	cands = rankTargets(cands, here, st.Self(), multi, known)

	// The first monster in the ranking that is a walk with nothing to open wins, whatever stands behind a door.
	type behind struct {
		n    gamestate.NPC
		why  string
		rank int
	}
	var notPlain []behind
	for i, n := range cands {
		r := combatrunner.ReachPlain(d, at(n))
		if r.OK {
			return n, true
		}
		notPlain = append(notPlain, behind{n: n, why: r.Why, rank: i})
	}
	// None is. The planner is asked about each remaining one, unless the doors are held shut (the budget is spent),
	// which reads a target behind a door as no route and passes over it for the usual minute.
	doorsOK := b.doors.Allowed(now)
	type viaDoor struct {
		n     gamestate.NPC
		reach combatrunner.Reach
		rank  int
	}
	var viaDoors []viaDoor
	noRoute := 0
	for _, nb := range notPlain {
		if !doorsOK {
			noRoute++
			b.noteNoRoute(ctx, nb.n, nb.why+"; the doors are held shut after "+fmt.Sprint(combatplan.DoorStrikeLimit)+" door targets without a kill")
			continue
		}
		reach := combatrunner.ReachThroughDoors(d, at(nb.n), nb.why)
		if reach.OK {
			viaDoors = append(viaDoors, viaDoor{n: nb.n, reach: reach, rank: nb.rank})
			continue
		}
		noRoute++
		b.noteNoRoute(ctx, nb.n, reach.Why)
	}
	if len(viaDoors) > 0 {
		costs := make([]combatplan.DoorCand, len(viaDoors))
		for i, v := range viaDoors {
			costs[i] = combatplan.DoorCand{Rank: v.rank, Cost: v.reach.Cost}
		}
		if i, ok := combatplan.PickDoorTarget(costs); ok {
			v := viaDoors[i]
			b.pickedDoors = v.reach.Doors
			if time.Since(b.warnedAt["doorpick"]) >= 20*time.Second {
				b.warnedAt["doorpick"] = time.Now()
				ctx.logf("combattrainer: no %s left on this side of the doors at rung %q — going through %d door(s) (first: %s) to the one at %d,%d,%d, a %d-tile route (%d of %d door targets since the last kill)",
					b.rung.Monster.Name, b.rung.Key, v.reach.Doors, v.reach.First, v.n.X, v.n.Z, v.n.Level, v.reach.Cost,
					b.doors.Strikes()+1, combatplan.DoorStrikeLimit)
			}
			return v.n, true
		}
	}
	if noRoute > 0 {
		b.warn(ctx, "noroute", time.Minute,
			"%d of the %s(s) inside rung %q's leash have no route from where we are standing — waiting for one we can reach rather than clicking through a wall",
			noRoute, b.rung.Monster.Name, b.rung.Key)
		// Every monster unreachable means the account is the one walled in: live 7 Oct 2026 Acct192
		// stood in a Barbarian Village hut whose door had been shut and waited 15 minutes for "one we
		// can reach". The baked pack treats a shut door as a wall; the door-aware walk opens it.
		if noRoute == len(cands) && noRoute >= 2 && now.Sub(b.walledAt) >= 30*time.Second {
			b.walledAt = now
			ctx.logf("combattrainer: no %s inside rung %q's leash has a route from %v — walking back to the stand %v through any door",
				b.rung.Monster.Name, b.rung.Key, here, b.rung.Stand)
			// To the stand's own side of any wall: a travel to within two tiles of it is satisfied on the wrong side of
			// the palace's Large door, and did nothing (combatrunner.ReachStand). With the doors held shut it is that
			// travel still, so a hold is a hold.
			if doorsOK {
				combatrunner.ReachStand(d, b.rung.Stand, 60*time.Second)
			} else {
				combatrunner.TravelTo(d, b.rung.Stand, 2, 60*time.Second)
			}
		}
	}
	return gamestate.NPC{}, false
}

// rankTargets orders the rung's monsters for the next attack: one already attacking this account
// first (in a single-way area it is the only one the server lets it attack), then free ones
// nearest first, then the ones another player is fighting, nearest first. In a known single-way
// area those last are left out altogether: content refuses the attack ("Someone else is fighting
// that.", player_combat.rs2 player_in_combat_check), and the old nearest-first pick clicked them
// and waited up to ninety seconds for a fight it was never given. In a multiway area, or one not
// known yet, a busy monster is still taken when no free one is left.
func rankTargets(cands []gamestate.NPC, here tile.Tile, self int, multiway, known bool) []gamestate.NPC {
	return rankTargetsBy(cands, self, multiway, known, func(n gamestate.NPC) int {
		return tile.Chebyshev(here, tile.Tile{X: n.X, Z: n.Z, Level: n.Level})
	})
}

// rankTargetsBy is rankTargets with the nearness measured by dist (the straight distance above; at a fenced pen the walk over open
// ground). Ties keep the order cands came in.
func rankTargetsBy(cands []gamestate.NPC, self int, multiway, known bool, dist func(gamestate.NPC) int) []gamestate.NPC {
	class := func(n gamestate.NPC) int {
		switch {
		case n.TargetsPlayer(self):
			return 0
		case fighterplan.TakenByAnother(false, n.InCombat, false, n.TargetsAnotherPlayer(self)):
			return 2
		}
		return 1
	}
	out := make([]gamestate.NPC, 0, len(cands))
	for _, n := range cands {
		if known && !multiway && class(n) == 2 {
			continue
		}
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := class(out[i]), class(out[j])
		if ci != cj {
			return ci < cj
		}
		return dist(out[i]) < dist(out[j])
	})
	return out
}

// killSettle is how long after its own kill the account waits before the next attack: content's
// npc_death.rs2 clears %lastcombat only when the death script runs, a tick or two after the
// killing hit, and an attack clicked before that is refused "I'm already under attack!" in a
// single-way area.
const killSettle = 1800 * time.Millisecond

// stillUnderAttack is whether the account is still in a fight and must not start another: hit in
// the last two ticks, a living monster in view facing it, or its own kill under killSettle ago.
// It replaces the bare InCombat() gate, whose window runs ten ticks from the last hit: every kill
// waited four to six seconds after the target died before the next click (melee idle analysis,
// 6 Oct 2026: 4.2 s a kill at the Giants, 3.9 at the White Knights).
func (b *combatTrainerBot) stillUnderAttack(ctx *Context) bool {
	st := ctx.Session.State()
	if !st.InCombat() {
		return false
	}
	if time.Since(b.lastKillAt) < killSettle {
		return true
	}
	return b.underFire(ctx)
}

// underFire is whether something living is on the account: hit in the last two ticks, or a monster in view that
// is not dead facing it. stillUnderAttack without the kill's settle, which holds back the next ATTACK but not a
// bone's burial.
func (b *combatTrainerBot) underFire(ctx *Context) bool {
	st := ctx.Session.State()
	if !st.InCombat() {
		return false
	}
	if st.HitWithin(2) {
		return true
	}
	self := st.Self()
	for _, n := range st.NPCs() {
		if n.TargetsPlayer(self) && !(n.TotalHealth > 0 && n.Health == 0) {
			return true
		}
	}
	return false
}

// contestedFor is how long a monster another player is fighting is passed over after the server
// refused the attack (scarabplan.TargetSkipFor's figure).
const contestedFor = 30 * time.Second

// noteDoorTarget counts a target taken from behind a door against the budget between two kills, and says so when it
// spends the last of it. The kill that follows a good crossing clears the count (see the kill case in fightStep).
func (b *combatTrainerBot) noteDoorTarget(ctx *Context, n gamestate.NPC, res combatrunner.FightResult) {
	outcome := "fought"
	switch {
	case res.DoorFailed:
		outcome = "the crossing did not land"
	case !res.Attacked:
		outcome = "not attacked"
	}
	if b.doors.Strike(time.Now()) {
		ctx.logf("combattrainer: %d door targets in a row without a kill (the last: the %s at %d,%d,%d, %s) — keeping to this side of the doors for %s",
			combatplan.DoorStrikeLimit, b.rung.Monster.Name, n.X, n.Z, n.Level, outcome, combatplan.DoorHold)
	}
}

// noteContested passes over a monster the server said someone else is fighting, for contestedFor:
// stored in the unreachable map, back-dated so it lapses after contestedFor rather than
// unreachableFor.
func (b *combatTrainerBot) noteContested(ctx *Context, n gamestate.NPC) {
	if b.unreachable == nil {
		b.unreachable = map[int]time.Time{}
	}
	b.unreachable[n.Index] = time.Now().Add(contestedFor - unreachableFor)
	b.vlog(ctx, "the %s at %d,%d is someone else's fight — passing over it for %s", b.rung.Monster.Name, n.X, n.Z, contestedFor)
}

// noteNoRoute records a target the pathfinder says there is no route to, so the next pass does
// not pay for the same search again.
//
// It is a different fact from noteUnreachable's, and the line says so: this one never had a
// route, rather than being clicked and surviving. The log is throttled per NPC index because a
// rung whose spawns are inside a building answers this every pass, and the interesting reading
// is which tile, once.
func (b *combatTrainerBot) noteNoRoute(ctx *Context, n gamestate.NPC, why string) {
	if b.unreachable == nil {
		b.unreachable = map[int]time.Time{}
	}
	b.unreachable[n.Index] = time.Now()
	b.warn(ctx, fmt.Sprintf("noroute-%d", n.Index), 5*time.Minute,
		"no route from here to the %s at %d,%d,%d (%s) — passing over it for %s",
		b.rung.Monster.Name, n.X, n.Z, n.Level, why, unreachableFor)
}

// noteUnreachable records a target the run clicked and could not finish, so the next pass tries
// another one rather than spending the same ninety seconds again.
func (b *combatTrainerBot) noteUnreachable(ctx *Context, n gamestate.NPC) {
	if b.unreachable == nil {
		b.unreachable = map[int]time.Time{}
	}
	b.unreachable[n.Index] = time.Now()
	b.vlog(ctx, "could not finish the %s at %d,%d — passing over it for %s", b.rung.Monster.Name, n.X, n.Z, unreachableFor)
}

// lootStep takes one big-bones drop attributable to our own recent kill. It returns true when
// it acted, so the caller re-checks the world rather than assuming.
func (b *combatTrainerBot) lootStep(ctx *Context, d *combatrunner.Deps) bool {
	b.loot.syncKills(ctx, b.killSites, b.rung.Monster.Name, b.rung.Monster.CombatLevel) // loot panel only (lootcombat.go)
	// Arrows first, and they are worth taking at every rung rather than only at the ones that
	// drop bones. Content destroys one fired arrow in five and drops the other four on the
	// target's own tile (ranged_dropammo_npc against ^dropammo_chance = 5), so a run that never
	// picks them up spends five times the arrows and a run that does can train on one purchase.
	if b.takeArrows(ctx, d) {
		return true
	}
	if b.takeDrops(ctx, d) {
		return true
	}
	// The bone this rung's monster leaves: Big bones at the giants and Ogres as always, and plain
	// Bones (death_drop defaults to bones) at the rungs that drop them, only while Prayer is short
	// of its goal (combatplan.WantBones).
	bone := b.rung.Monster.BoneItem()
	if bone == "" || b.rung.Herd || !combatplan.WantBones(b.rung.Monster, combatrunner.SkillLevel(d, combatrunner.PrayerStatIdx), b.cfg.goals.Prayer) {
		return false
	}
	inv := d.Session.State().Inventory(combatrunner.PlayerPack)
	if inv != nil && len(inv.NonEmpty()) >= 28 {
		return false
	}
	now := time.Now().UnixMilli()
	for _, g := range combatrunner.GroundItemsNamed(d, bone) {
		if !combatplan.DropFromKill(g.X, g.Z, g.Level, b.killSites, now, 180_000) {
			continue
		}
		before := lootPackBefore(ctx)
		if err := combatrunner.ApproachAndTake(d, g, 1, b.rung.Leash, 20*time.Second); err != nil {
			b.vlog(ctx, "could not take the %s: %v", strings.ToLower(bone), err)
			return false
		}
		b.looted++
		b.loot.taken(ctx, before, g.X, g.Z, g.Level)
		return true
	}
	return false
}

// takeDrops picks up one drop of this run's own kills that combatplan.WorthTrainLoot takes: every
// stackable the pack has room for (runes from the giants, feathers, coins) and a valuable
// non-stackable only into a free slot, never dropping food for it (a user request).
func (b *combatTrainerBot) takeDrops(ctx *Context, d *combatrunner.Deps) bool {
	inv := d.Session.State().Inventory(combatrunner.PlayerPack)
	free, held := 28, map[int]bool{}
	if inv != nil {
		items := inv.NonEmpty()
		free = 28 - len(items)
		for _, it := range items {
			held[int(it.ID)] = true
		}
	}
	now := time.Now().UnixMilli()
	for _, g := range d.Session.State().GroundItems() {
		if !combatplan.DropFromKill(g.X, g.Z, g.Level, b.killSites, now, 180_000) {
			continue
		}
		obj, ok := d.Config.Obj(g.ID)
		if !ok || !combatplan.WorthTrainLoot(combatplan.TrainLootDrop{Name: obj.Name, Stackable: obj.Stackable, Cost: int(obj.Cost), Stacks: obj.Stackable && held[g.ID], ID: g.ID}, free) {
			continue
		}
		before := lootPackBefore(ctx)
		if err := combatrunner.ApproachAndTake(d, g, 1, b.rung.Leash, 20*time.Second); err != nil {
			b.vlog(ctx, "could not take the %s: %v", obj.Name, err)
			return false
		}
		b.looted++
		b.loot.taken(ctx, before, g.X, g.Z, g.Level)
		return true
	}
	return false
}

// finish ends the run for good: bank what is carried, log out, and tell the fleet not to bring
// the account back. RequestFinish is what stops internal/fleet's reconnect loop from logging
// straight back in and restarting the script, which is a real trap that was measured with a
// control run — without it an account that logs out on purpose is simply resumed.
func (b *combatTrainerBot) finish(ctx *Context, d *combatrunner.Deps, reason string) error {
	if b.embed != nil {
		// A stage of another script: its goals are met, so hand back with the loot and gear
		// where they are -- the host decides what comes next, never a logout.
		ctx.logf("combattrainer: %s — handing back to the host script", reason)
		b.endRun(ctx, reason, true)
		return nil
	}
	ctx.logf("combattrainer: %s — banking, then logging out for good", reason)
	// A run whose goals were already met on its very first Loop call never chose a rung, so
	// there is no bank tile to walk to. Logging out with the loot still carried is the right
	// answer there; walking to tile 0,0 is not.
	if !b.haveRung {
		ctx.logf("combattrainer: no rung was ever chosen, so there is no bank to walk to — logging out as we are")
	} else if bank, ok := combatrunner.OpenBankNear(d, b.rung.Bank, 6); ok {
		worn := combatrunner.WornNames(d)
		combatrunner.DepositAllExcept(d, bank, func(name string) bool {
			for _, w := range worn {
				if strings.EqualFold(w, name) {
					return true
				}
			}
			return false
		})
		_ = bank.Close()
	} else {
		ctx.logf("combattrainer: could not reach a bank to store the loot — logging out anyway, the gear stays worn")
	}
	if err := combatrunner.LogoutForGood(d, func() { ctx.RequestFinish(reason) }, 60*time.Second); err != nil {
		ctx.logf("combattrainer: %v", err)
	}
	// requestStop is unexported, which is exactly why this adapter lives in package bot: a
	// script that has finished its whole job has to be able to end its own run.
	ctx.requestStop()
	return nil
}

func (b *combatTrainerBot) OnStop(ctx *Context) {
	b.saveShopMemory()
	d := b.deps(ctx)
	l := combatrunner.TrainerLevels(d)
	prayer := combatrunner.SkillLevel(d, combatrunner.PrayerStatIdx)
	rung := "none"
	if b.haveRung {
		rung = b.rung.Key
	}
	style := b.trainingStyle
	if style == "" {
		style = combatplan.StyleMelee
	}
	ctx.logf("combattrainer: stopped after %s on rung %s training %s — %d kills, %d bones buried, %d drops taken, %d arrows recovered, %d items bought, %d bank trips, %d bites, %d deaths. Now attack %d, strength %d, defence %d, ranged %d, magic %d, prayer %d",
		time.Since(b.startedAt).Round(time.Second), rung, style,
		b.kills, b.buried, b.looted, b.arrowsTaken, b.bought, b.trips, b.eats, b.deaths,
		l.Attack, l.Strength, l.Defence, l.Ranged, l.Magic, prayer)
	// Repeated here on purpose: the line that announced the stop may be thousands of lines up a
	// long log, and the summary is what a reader looks at first.
	if b.stopReason != "" {
		ctx.logf("combattrainer: the run ended itself short of its goals — %s", b.stopReason)
	}
}

// unusedTileRef keeps the tile import honest if a future edit drops the last use; it costs
// nothing and documents that rung tiles are tile.Tile values.
var _ = tile.Tile{}
