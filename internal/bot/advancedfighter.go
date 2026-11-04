package bot

import (
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/fighterplan"
	"rsb0t-go/internal/scripts/fighterrunner"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/tile"
)

func init() {
	Register("AdvancedFighter", func() Bot { return &advancedFighterBot{} })
}

// advancedFighterBot ports AdvancedFighter.ts's own priority-ordered task list into the
// stage-dispatch shape every other port here uses (fighterplan.DecideStage 1:1 ports the
// task list's validate() order — see that function's own doc comment). Every Loop call
// re-derives the stage from live state and does one stage-appropriate action, the same
// "nothing remembered except what must survive one call to the next" shape essenceBot's own
// doc comment describes.
//
// This is a melee-only port at a fixed spot: target scoring/picking, smart/threshold eating,
// death recovery (re-equip gear), bank runs with a food/gear/loot-keep restock, bone
// burying, loot pickup, a stop-at goal (level/kills/minutes), and a flee-to-bank response to
// a real player attacking. See internal/scripts/fighterplan's own doc comment for the full
// "deliberately not ported" list (prayer, potions, ranged/mage, treasure trails, safespots,
// teleport-flee — none of them have a Go-side primitive yet).
//
// Settings, all optional (read via IntSetting/BoolSetting/StringSetting/FloatSetting, so
// both Go values and the float64/bool/string a JSON-decoded fleet.config.json roster entry
// produces work):
//
//	targets       string  default ""         — comma-separated NPC names to attack; blank is
//	                                            anything offering Attack.
//	anchorTile    tile    default the tile the run starts on — the killing spot.
//	leashRadius   int     default 10          — tiles from anchorTile a target/loot may be.
//	attackOthersTargets bool default false    — engage NPCs already fighting another player.
//	preferAttackers     bool default true     — score NPCs already hitting us above anything.
//	meleeStyle    string  default "strength"  — attack/strength/controlled/defence/progressive.
//	autoRetaliate bool    default true.
//	food          string  default ""          — blank takes the first edible thing in the pack.
//	foodWithdraw  int     default 10          — food drawn from the bank per trip, clamped 0-27.
//	eatMode       string  default "smart"     — "smart" or "threshold".
//	eatAtHp       int     default 50          — threshold mode's percent, clamped 10-90.
//	buryBones     bool    default false.
//	loot          string  default ""          — comma-separated "contains" patterns.
//	keep          string  default ""          — comma-separated "contains" patterns never
//	                                            deposited, beyond food/gear/coins/bones.
//	autoBank      bool    default true.
//	bankAtLootSlots int   default 12          — clamped 1-27.
//	bankEveryMinutes int  default 0           — 0 disables the timer trigger.
//	panicHp       int     default 25          — percent, clamped 0-90.
//	fleeFromPlayers bool  default false       — flee to the nearest bank when a player attacks.
//	maxDeaths     int     default 0           — 0 never halts on deaths.
//	stopAtLevel   string  default ""          — "skill:level", e.g. "attack:70".
//	stopAfterKills int    default 0.
//	stopAfterMinutes int  default 0.
//	verbose       bool    default false.
type advancedFighterBot struct {
	cfg advancedFighterSettings

	// live state that must survive one Loop call to the next — mirrors AdvancedFighter.ts's
	// own class fields, scoped to what this melee-only port actually uses.
	anchor      tile.Tile
	trackedGear []string
	startedAt   time.Time
	lastBankAt  time.Time
	xpAtStart   map[int]int

	kills       int
	killsByName map[string]int
	looted      int
	buried      int
	eats        int
	trips       int
	deaths      int

	died           bool
	afterDeath     bool
	foodKnownEmpty bool
	// gearSeeded is false until trackedGear was taken from a readable view of both panels. A
	// start made while the tabs are nulled has to seed later rather than run the whole session on
	// an empty gear list — see OnStart and Loop.
	gearSeeded    bool
	goalDone      string
	goalBanked    bool
	goalAnnounced bool

	skip      map[int]int64
	killSites []fighterplan.KillSite
	// loot attributes kills and pickups for the loot panel (lootcombat.go).
	loot lootKills

	// the currently-held fight target, across Loop calls within one Fight stage run.
	fightTargetIdx int
	fightEngagedAt time.Time
	// fightKill judges that target's death and remembers what it was, so a kill whose corpse
	// has already left the NPC list can still be named on the loot panel (killwatch.go).
	fightKill killWatch

	warnedAt map[string]time.Time

	// mule hand-off (donorplan/donorrunner — see handoffStep's own doc comment): the same
	// mechanism ProgressiveFisher/ProgressiveCrafter/EssenceMiner already use, so loot
	// banked here hands off to a mule the identical way a skilling script's catch does.
	handoffPending     bool
	handoffNextAllowed time.Time
	handoffsDone       int
	handedOver         int
	knownUntradable    map[int]bool
}

type advancedFighterSettings struct {
	targets             []string
	anchorSet           bool
	anchorTile          tile.Tile
	leashRadius         int
	attackOthersTargets bool
	preferAttackers     bool
	meleeStyle          string
	autoRetaliate       bool
	food                string
	foodWithdraw        int
	eatMode             fighterfood.EatMode
	eatAtFrac           float64
	buryBones           bool
	loot                []string
	keep                []string
	autoBank            bool
	bankAtLootSlots     int
	bankEveryMinutes    float64
	panicAtFrac         float64
	fleeFromPlayers     bool
	maxDeaths           int
	goal                fighterplan.Goal
	verbose             bool

	mule      string
	muleAt    int
	muleKeep  string
	muleStand tile.Tile
}

var advancedFighterBones = []string{"Bones", "Bat bones", "Big bones", "Burnt bones", "Wolf bones", "Monkey bones", "Jogre bones", "Babydragon bones", "Dragon bones"}

func readAdvancedFighterSettings(m map[string]interface{}) advancedFighterSettings {
	skill, level, _ := fighterplan.ParseLevelGoal(StringSetting(m, "stopAtLevel", ""))
	s := advancedFighterSettings{
		targets:             fighterplan.SplitNames(StringSetting(m, "targets", "")),
		leashRadius:         clampInt(IntSetting(m, "leashRadius", 10), 2, 40),
		attackOthersTargets: BoolSetting(m, "attackOthersTargets", false),
		preferAttackers:     BoolSetting(m, "preferAttackers", true),
		meleeStyle:          StringSetting(m, "meleeStyle", "strength"),
		autoRetaliate:       BoolSetting(m, "autoRetaliate", true),
		food:                strings.TrimSpace(StringSetting(m, "food", "")),
		foodWithdraw:        clampInt(IntSetting(m, "foodWithdraw", 10), 0, 27),
		eatMode:             fighterfood.ParseEatMode(StringSetting(m, "eatMode", "smart")),
		buryBones:           BoolSetting(m, "buryBones", false),
		loot:                lowerAll(fighterplan.SplitNames(StringSetting(m, "loot", ""))),
		keep:                fighterplan.SplitNames(StringSetting(m, "keep", "")),
		autoBank:            BoolSetting(m, "autoBank", true),
		bankAtLootSlots:     clampInt(IntSetting(m, "bankAtLootSlots", 12), 1, 27),
		bankEveryMinutes:    FloatSetting(m, "bankEveryMinutes", 0),
		fleeFromPlayers:     BoolSetting(m, "fleeFromPlayers", false),
		maxDeaths:           IntSetting(m, "maxDeaths", 0),
		goal: fighterplan.Goal{
			LevelSkill:  skill,
			LevelTarget: level,
			Kills:       IntSetting(m, "stopAfterKills", 0),
			Minutes:     FloatSetting(m, "stopAfterMinutes", 0),
		},
		verbose: BoolSetting(m, "verbose", false),

		mule:     StringSetting(m, "mule", ""),
		muleAt:   IntSetting(m, "muleAt", 0),
		muleKeep: StringSetting(m, "muleKeep", ""),
	}
	if s.muleAt < 0 {
		s.muleAt = 0
	}
	if s.muleAt > donorplan.MaxMuleAt {
		s.muleAt = donorplan.MaxMuleAt
	}
	s.muleStand = fisherTileSetting(m, "muleAtTile", mulereceiverplan.SeersStand)
	eatAtHp := FloatSetting(m, "eatAtHp", 50)
	s.eatAtFrac = clampFrac(eatAtHp, 10, 90) / 100
	panicHp := FloatSetting(m, "panicHp", 25)
	s.panicAtFrac = clampFrac(panicHp, 0, 90) / 100
	if at, ok := m["anchorTile"]; ok && at != nil {
		s.anchorTile = fisherTileSetting(m, "anchorTile", tile.Tile{})
		s.anchorSet = true
	}
	return s
}

func lowerAll(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = strings.ToLower(n)
	}
	return out
}

func clampFrac(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (b *advancedFighterBot) deps(ctx *Context) *fighterrunner.Deps {
	return &fighterrunner.Deps{
		Session:    ctx.Session,
		Pack:       ctx.Pack,
		Locs:       ctx.Locs,
		Config:     ctx.Config,
		Components: ctx.Components,
		Chat:       ctx.Chat,
		Delay:      ctx.Delay,
		Log:        ctx.Log,
	}
}

func (b *advancedFighterBot) OnStart(ctx *Context) error {
	b.cfg = readAdvancedFighterSettings(ctx.Settings)
	d := b.deps(ctx)

	// This script's own InCombat is its ordinary, desired operating state, not a hazard —
	// without this, fleet.go's own per-account Guardian would flee the account away from
	// every fight it starts (live-found via cmd/advancedfightertest, 2026-09-16; see
	// randomevents.Guardian.CombatExpected's own doc comment for the evidence and the
	// exact failure mode this closes). Guardian is nil for a bare unit-test Context.
	if ctx.Guardian != nil {
		ctx.Guardian.CombatExpected = true
	}

	if !ctx.DelayUntil(func() bool { _, _, _, ok := ctx.Session.State().Tile(); return ok }, 15*time.Second) {
		ctx.logf("advancedfighter: never got a starting position — starting anyway, the anchor will resolve once one arrives")
	}
	if b.cfg.anchorSet {
		b.anchor = b.cfg.anchorTile
	} else if x, z, level, ok := ctx.Session.State().Tile(); ok {
		b.anchor = tile.Tile{X: x, Z: z, Level: level}
	}
	// Seed trackedGear from a view that can actually be read. This list is taken ONCE and drives
	// the whole run — the keep list every bank trip deposits against, GearMissing, the after-death
	// re-withdraw — and nothing later re-reads it to correct a bad one. Equipment resolves both
	// panels through sidebar tabs 3 and 4, and content nulls those in ~macro_events_set_tabs (a
	// mime, which holds them until its mapzoneexit) and in ~set_sail/~set_sail_cairn (a crossing,
	// which gets them back at ~initalltabs before the arrival telejump, so that window is the
	// route's own p_delay rather than the whole voyage). The short one is enough to matter here,
	// because this seed is taken at one moment and never retaken: a start that lands inside
	// either reads "wearing and carrying nothing" and the run tracks no gear at all for its
	// entire length. Waiting costs at most 15s on a start that is genuinely blind; not waiting
	// cost the whole run's gear handling.
	if !ctx.DelayUntil(func() bool { return fighterrunner.ContainersReadable(d) }, 15*time.Second) {
		ctx.logf("advancedfighter: cannot read the backpack and worn panels yet — starting without a gear list and seeding it on the first pass that can read them")
	} else {
		b.trackedGear = fighterrunner.HeldNames(d)
		b.gearSeeded = true
	}
	b.startedAt = time.Now()
	b.lastBankAt = b.startedAt
	b.xpAtStart = map[int]int{}
	for _, stat := range fighterrunner.CombatSkillStats {
		b.xpAtStart[stat] = fighterrunner.SkillXP(d, stat)
	}
	b.killsByName = map[string]int{}
	b.skip = map[int]int64{}
	b.warnedAt = map[string]time.Time{}
	b.fightTargetIdx = -1
	b.knownUntradable = map[int]bool{}

	if b.cfg.food == "" {
		b.cfg.food = fighterrunner.DetectFood(d)
		if b.cfg.food == "" {
			b.cfg.food = "Trout"
		}
	}

	ctx.logf("advancedfighter: targets [%s] at %d,%d r%d, melee (%s), food %q x%d (%s), banking %s, loot [%s]%s",
		joinOr(b.cfg.targets, "anything attackable"), b.anchor.X, b.anchor.Z, b.cfg.leashRadius,
		b.cfg.meleeStyle, b.cfg.food, b.cfg.foodWithdraw, b.cfg.eatMode,
		map[bool]string{true: "auto", false: "none"}[b.cfg.autoBank], strings.Join(b.cfg.loot, ", "),
		map[bool]string{true: ", burying bones", false: ""}[b.cfg.buryBones])
	muleMsg := "no mule set"
	if donorplan.NewSettings(b.cfg.mule, b.cfg.muleAt, b.cfg.muleKeep).Enabled() {
		muleMsg = "handing loot over to " + b.cfg.mule + " at " + strconv.Itoa(b.cfg.muleAt) + " banked loot"
	}
	ctx.logf("advancedfighter: %s", muleMsg)
	return nil
}

func joinOr(names []string, def string) string {
	if len(names) == 0 {
		return def
	}
	return strings.Join(names, ", ")
}

func (b *advancedFighterBot) vlog(ctx *Context, format string, args ...any) {
	if b.cfg.verbose {
		ctx.logf("advancedfighter: "+format, args...)
	}
}

func (b *advancedFighterBot) warn(ctx *Context, key string, everyMs time.Duration, format string, args ...any) {
	if time.Since(b.warnedAt[key]) < everyMs {
		return
	}
	b.warnedAt[key] = time.Now()
	ctx.logf("advancedfighter: WARNING: "+format, args...)
}

// warnNoRoute says why a fight step found nothing to do when there were candidates in front of
// it. Without this line the reachability gate is invisible: the step simply returns, which is
// exactly what it does when the spot is empty, and the two readings would look identical in a
// log. Throttled, because a spot whose spawns are all inside a building answers this every pass.
func (b *advancedFighterBot) warnNoRoute(ctx *Context, recs []fighterplan.TargetRecord) {
	if len(recs) == 0 {
		return
	}
	b.warn(ctx, "noroute", time.Minute,
		"no route to any of the %d candidate(s) in front of us from where we are standing — waiting for one we can reach rather than clicking through a wall",
		len(recs))
}

// ensureGearStep equips tracked gear, and retires anything the equipment interface refuses.
//
// Retiring is the whole point. trackedGear comes from HeldNames at OnStart, which is worn items
// PLUS the pack, so a run started with food in the pack tracks the food as gear. Food cannot be
// worn, so GearMissing stays true forever, StageEnsureGear outranks StageFight on every pass,
// and the run never throws a punch — measured live at the Al Kharid stand: five minutes, zero
// kills, zero xp, and "equip Trout: actions: no option with that label" several hundred times.
// It is the same shape as the reachability wedge this file was opened for: a stage that can
// never complete, starving the one that does the work.
//
// An item that is still in the pack and still not worn after EnsureGear tried it is one this
// account cannot wear, so it leaves the list. A genuinely transient failure costs one lost
// auto-equip; retrying forever cost the whole run.
func (b *advancedFighterBot) ensureGearStep(ctx *Context, d *fighterrunner.Deps) {
	fighterrunner.EnsureGear(d, b.trackedGear)
	kept := b.trackedGear[:0]
	for _, name := range b.trackedGear {
		if fighterrunner.GearMissing(d, []string{name}) {
			b.vlog(ctx, "%q is carried but cannot be worn — not tracking it as gear any more", name)
			continue
		}
		kept = append(kept, name)
	}
	b.trackedGear = kept
}

func (b *advancedFighterBot) minutes() float64 { return time.Since(b.startedAt).Minutes() }

// halted ports AdvancedFighter.ts's own halted(): too many deaths, or the goal is met and
// banked and the run isn't told to keep fighting on (this port has no keepFightingOnGoal
// setting — it always idles at the bank once a goal is banked, matching the TS default).
func (b *advancedFighterBot) halted() string {
	if b.cfg.maxDeaths > 0 && b.deaths >= b.cfg.maxDeaths {
		return strconv.Itoa(b.deaths) + " deaths — stopped fighting"
	}
	if b.goalDone != "" && b.goalBanked {
		return "goal reached: " + b.goalDone
	}
	return ""
}

func (b *advancedFighterBot) lootFilter() fighterplan.LootFilter {
	return fighterplan.LootFilter{Patterns: b.cfg.loot, BuryBones: b.cfg.buryBones, Bones: advancedFighterBones}
}

func (b *advancedFighterBot) keepInput() fighterplan.KeepInput {
	extra := append([]string{}, b.cfg.keep...)
	return fighterplan.KeepInput{Food: b.cfg.food, Gear: b.trackedGear, Extra: extra, BuryBones: b.cfg.buryBones, Bones: advancedFighterBones}
}

func (b *advancedFighterBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !fighterrunner.WaitForGuardian(d) {
		return nil
	}
	if ctx.Components != nil {
		if dlg := actions.NewChatDialog(ctx.Session, ctx.Components); dlg.CanContinue() {
			_ = dlg.Continue()
			ctx.Delay(300 * time.Millisecond)
			return nil
		}
	}
	// The gear list OnStart could not take because both panels were unreadable. Re-taken on the
	// first pass that CAN read them, because trackedGear is otherwise a once-per-run reading and
	// an empty one silently disables gear tracking for the whole session.
	if !b.gearSeeded && fighterrunner.ContainersReadable(d) {
		b.trackedGear = fighterrunner.HeldNames(d)
		b.gearSeeded = true
		ctx.logf("advancedfighter: tracking gear [%s]", joinOr(b.trackedGear, "nothing"))
	}
	if ctx.Session.State().IsDead() && !b.died {
		b.died = true
	}

	if b.cfg.fleeFromPlayers && fighterrunner.PlayerThreat(d) && !b.died {
		b.fleeToBank(ctx, d)
		return nil
	}

	// The mule hand-off is checked before the ordinary stage dispatch, the same
	// cross-cutting-concern placement essenceBot/fisherBot use — a manual control-panel
	// trigger can fire regardless of which stage the fighter is in, and an auto-due
	// hand-off (set by bankRun once loot has been banked, see its own doc comment) should
	// not wait for a Fight/Loot stage to give it a turn.
	if manual := ctx.TakeHandoffRequest(); manual {
		b.handoffPending = true
	}
	threshold := b.cfg.muleAt
	if v, ok := ctx.MuleAtOverride(); ok {
		threshold = v
	}
	donorSettings := donorplan.NewSettings(b.cfg.mule, threshold, b.cfg.muleKeep)
	if donorSettings.Enabled() && b.handoffPending && !b.died {
		return b.handoffStep(ctx, d, donorSettings)
	}

	snap := b.snapshot(ctx, d)
	stage := fighterplan.DecideStage(snap.Snapshot)
	switch stage {
	case fighterplan.StageDeathRecovery:
		b.recoverFromDeath(ctx, d)
	case fighterplan.StageHalted:
		ctx.Delay(2 * time.Second)
	case fighterplan.StagePanicRetreat:
		b.panicRetreat(ctx, d)
	case fighterplan.StageEat:
		if fighterrunner.EatOnce(d, b.cfg.food) {
			b.eats++
		} else {
			ctx.Delay(600 * time.Millisecond)
		}
	case fighterplan.StageFixStyle:
		b.fixStyle(ctx, d)
	case fighterplan.StageEnsureGear:
		b.ensureGearStep(ctx, d)
	case fighterplan.StageBuryBones:
		if fighterrunner.BuryOne(d, advancedFighterBones) {
			b.buried++
		}
	case fighterplan.StageBankRun:
		b.bankRun(ctx, d, snap.bankReason)
	case fighterplan.StageLoot:
		b.lootStep(ctx, d)
	case fighterplan.StageFight:
		b.fightStep(ctx, d)
	case fighterplan.StageReturnAnchor:
		ctx.logf("advancedfighter: heading back to the spot")
		fighterrunner.WalkNear(d, b.anchor, 3)
	default:
		ctx.Delay(600 * time.Millisecond)
	}
	return nil
}

// snapshotResult wraps fighterplan.Snapshot with the extra bit of context (the actual bank
// reason string) the BankRun stage's own execute() needs but DecideStage's boolean doesn't
// carry.
type snapshotResult struct {
	fighterplan.Snapshot
	bankReason fighterplan.BankReason
}

func (b *advancedFighterBot) snapshot(ctx *Context, d *fighterrunner.Deps) snapshotResult {
	if goalMsg := b.checkGoal(d); goalMsg != "" && b.goalDone == "" {
		b.goalDone = goalMsg
		if !b.goalAnnounced {
			b.goalAnnounced = true
			ctx.logf("advancedfighter: goal reached: %s — banking, then idling", goalMsg)
		}
	}
	halted := b.halted() != ""

	hpFrac := fighterrunner.HPFraction(d)
	foodCount := fighterrunner.FoodCount(d, b.cfg.food)
	panicNeeded := fighterplan.ShouldPanic(hpFrac, b.cfg.panicAtFrac, foodCount) && b.cfg.foodWithdraw > 0

	needEat := !b.died && fighterfood.ShouldEat(fighterfood.EatInput{
		HP: hpVal(d), MaxHP: maxHPVal(d), Heal: fighterfood.FoodHealAmount(b.cfg.food),
		FoodCount: foodCount, Mode: b.cfg.eatMode, ThresholdFrac: b.cfg.eatAtFrac,
	})

	wantStyle := fighterplan.ParseMeleeStyle(b.cfg.meleeStyle)
	if wantStyle == "progressive" {
		wantStyle = fighterplan.NextMeleeStyle(fighterrunner.MeleeLevels(d))
	}
	styleWrong := !b.died && fighterrunner.StyleWrong(d, b.cfg.autoRetaliate, wantStyle)
	gearMissing := !b.died && fighterrunner.GearMissing(d, b.trackedGear)

	needBury := b.cfg.buryBones && !ctx.Session.State().InCombat() && hasBoneInPack(d)

	filter := b.lootFilter()
	keep := b.keepInput()
	lootSlots := fighterrunner.LootSlots(d, filter, keep)
	reason := fighterplan.DecideBankReason(fighterplan.BankTriggerState{
		AutoBank: b.cfg.autoBank, LootSlots: lootSlots, BankAtLootSlots: b.cfg.bankAtLootSlots,
		PackFull: fighterrunner.PackFull(d), MinutesSinceBank: time.Since(b.lastBankAt).Minutes(),
		BankEveryMinutes: b.cfg.bankEveryMinutes, FoodCount: foodCount, FoodWanted: b.cfg.foodWithdraw,
		FoodKnownEmpty: b.foodKnownEmpty, AfterDeath: b.afterDeath,
		GoalReached: b.goalDone != "" && !b.goalBanked,
	})
	bankDue := reason != fighterplan.BankNone && (!ctx.Session.State().InCombat() || reason == fighterplan.BankAfterDeath)

	_, lootAvailable := fighterrunner.FindLoot(d, filter, b.anchor, b.cfg.leashRadius+4, b.killSites, nowMs(), killDropWindowMs)
	lootAvailable = lootAvailable && !ctx.Session.State().InCombat()

	haveFight := !b.died && hpFrac >= b.cfg.panicAtFrac || foodCount > 0
	haveFight = haveFight && b.hasFightCandidate(d)

	anchorFar := !ctx.Session.State().InCombat() && b.anchorFar(d)

	return snapshotResult{
		Snapshot: fighterplan.Snapshot{
			Died: b.died, Halted: halted, PanicNeeded: panicNeeded, NeedEat: needEat,
			StyleWrong: styleWrong, GearMissing: gearMissing, NeedBury: needBury,
			BankDue: bankDue, LootAvailable: lootAvailable, HaveFight: haveFight, AnchorFar: anchorFar,
		},
		bankReason: reason,
	}
}

func hpVal(d *fighterrunner.Deps) int {
	hp, _ := d.Session.State().HP()
	return hp
}
func maxHPVal(d *fighterrunner.Deps) int {
	mh, _ := d.Session.State().MaxHP()
	return mh
}
func nowMs() int64 { return time.Now().UnixMilli() }

const killDropWindowMs = 180_000

func hasBoneInPack(d *fighterrunner.Deps) bool {
	for _, name := range fighterrunner.HeldNames(d) {
		if fighterplan.IsBone(name, advancedFighterBones) {
			return true
		}
	}
	return false
}

func (b *advancedFighterBot) hasFightCandidate(d *fighterrunner.Deps) bool {
	var h tile.Tile
	if x, z, l, ok := d.Session.State().Tile(); ok {
		h = tile.Tile{X: x, Z: z, Level: l}
	}
	npcs := fighterrunner.TargetNPCs(d, b.cfg.targets)
	if len(npcs) == 0 {
		return false
	}
	recs := make([]fighterplan.TargetRecord, 0, len(npcs))
	for _, n := range npcs {
		recs = append(recs, fighterrunner.ToTargetRecord(d, n, h))
	}
	ctx := fighterplan.TargetContext{
		Anchor: b.anchor, Leash: b.cfg.leashRadius, OwnIndex: b.fightTargetIdx, HaveOwn: b.fightTargetIdx >= 0,
		AttackOthersTargets: b.cfg.attackOthersTargets, PreferAttackers: b.cfg.preferAttackers,
		SkipUntil: b.skip, NowMs: nowMs(),
		// The same gate fightStep picks through, so the stage dispatcher cannot answer "there
		// is a fight here" about a monster the fight step will then refuse to click.
		Reachable: fighterrunner.ReachableRecord(d),
	}
	_, ok3 := fighterplan.PickTarget(recs, ctx)
	return ok3
}

func (b *advancedFighterBot) anchorFar(d *fighterrunner.Deps) bool {
	x, z, level, ok := d.Session.State().Tile()
	if !ok {
		return false
	}
	here := tile.Tile{X: x, Z: z, Level: level}
	if here.Level != b.anchor.Level {
		return true
	}
	return tile.Chebyshev(here, b.anchor) > b.cfg.leashRadius+6
}

// checkGoal ports CheckGoal's own validate()/execute() combined.
func (b *advancedFighterBot) checkGoal(d *fighterrunner.Deps) string {
	return fighterplan.GoalReached(b.cfg.goal, func(skill string) int {
		switch strings.ToLower(skill) {
		case "attack":
			return fighterrunner.SkillLevel(d, fighterrunner.AttackStat)
		case "strength":
			return fighterrunner.SkillLevel(d, fighterrunner.StrengthStat)
		case "defence":
			return fighterrunner.SkillLevel(d, fighterrunner.DefenceStat)
		case "hitpoints":
			return maxHPVal(d)
		default:
			return 0
		}
	}, b.kills, b.minutes())
}

func (b *advancedFighterBot) fixStyle(ctx *Context, d *fighterrunner.Deps) {
	if ctx.Session.State().AutoRetaliateOn() != b.cfg.autoRetaliate {
		fighterrunner.EnsureRetaliate(d, b.cfg.autoRetaliate)
		return
	}
	want := fighterplan.ParseMeleeStyle(b.cfg.meleeStyle)
	if want == "progressive" {
		want = fighterplan.NextMeleeStyle(fighterrunner.MeleeLevels(d))
	}
	fighterrunner.EnsureMeleeStyle(d, want)
}

func (b *advancedFighterBot) recoverFromDeath(ctx *Context, d *fighterrunner.Deps) {
	b.deaths++
	ctx.logf("advancedfighter: died! (%d so far) — respawning, then regearing at the bank", b.deaths)
	ctx.DelayUntil(func() bool { _, _, _, ok := ctx.Session.State().Tile(); return ok }, 20*time.Second)
	ctx.DelayTicks(3)
	ctx.Session.State().ClearDead()
	b.died = false
	b.afterDeath = true
	b.skip = map[int]int64{}
	b.fightTargetIdx = -1
}

func (b *advancedFighterBot) panicRetreat(ctx *Context, d *fighterrunner.Deps) {
	ctx.logf("advancedfighter: panic retreat — no food at %d%% hp", int(fighterrunner.HPFraction(d)*100))
	bank, ok := fighterrunner.OpenBankNear(d, b.anchor, b.cfg.leashRadius+10)
	if !ok {
		ctx.Delay(3 * time.Second)
		return
	}
	fighterrunner.WithdrawTo(d, bank, b.cfg.food, b.cfg.foodWithdraw)
	_ = bank.Close()
	if fighterrunner.FoodCount(d, b.cfg.food) == 0 {
		b.foodKnownEmpty = true
		ctx.logf("advancedfighter: no %q in the bank — waiting for hitpoints to regenerate", b.cfg.food)
		ctx.DelayUntil(func() bool { return fighterrunner.HPFraction(d) >= 0.9 || ctx.Session.State().InCombat() }, 300*time.Second)
	} else {
		b.foodKnownEmpty = false
	}
}

func (b *advancedFighterBot) fleeToBank(ctx *Context, d *fighterrunner.Deps) {
	ctx.logf("advancedfighter: a player is attacking us — fleeing to the bank")
	fighterrunner.OpenBankNear(d, b.anchor, b.cfg.leashRadius+15)
}

func (b *advancedFighterBot) bankRun(ctx *Context, d *fighterrunner.Deps, reason fighterplan.BankReason) {
	ctx.logf("advancedfighter: bank run: %s", reason)
	bank, ok := fighterrunner.OpenBankNear(d, b.anchor, b.cfg.leashRadius+10)
	if !ok {
		b.warn(ctx, "bank-open", 30*time.Second, "could not open a bank — retrying")
		ctx.Delay(3 * time.Second)
		return
	}
	keep := b.keepInput()
	filter := b.lootFilter()
	fighterrunner.DepositExceptKept(d, bank, keep)
	ctx.DelayTicks(1)

	if b.afterDeath {
		for _, gear := range b.trackedGear {
			if fighterrunner.HeldCount(d, gear) == 0 {
				fighterrunner.WithdrawTo(d, bank, gear, 1)
			}
		}
	}
	if b.cfg.foodWithdraw > 0 {
		got := fighterrunner.WithdrawTo(d, bank, b.cfg.food, b.cfg.foodWithdraw)
		b.vlog(ctx, "withdrew %d %s", got, b.cfg.food)
	}

	if fighterrunner.FoodCount(d, b.cfg.food) == 0 && b.cfg.foodWithdraw > 0 {
		b.foodKnownEmpty = true
		ctx.logf("advancedfighter: no %q in the bank — fighting on without food", b.cfg.food)
	} else {
		b.foodKnownEmpty = false
	}

	donorSettings := donorplan.NewSettings(b.cfg.mule, b.cfg.muleAt, b.cfg.muleKeep)
	if donorSettings.Enabled() {
		bankedLoot := 0
		for _, row := range fighterrunner.BankItemRows(d, bank) {
			if fighterplan.WantsLoot(row.Name, filter) {
				bankedLoot += row.Count
			}
		}
		if donorplan.HandoffDue(bankedLoot, b.cfg.muleAt, time.Now(), b.handoffNextAllowed) {
			b.handoffPending = true
			ctx.logf("advancedfighter: %d loot item(s) banked — handing off to %s", bankedLoot, b.cfg.mule)
		}
	}
	_ = bank.Close()

	fighterrunner.EnsureGear(d, b.trackedGear)

	b.afterDeath = false
	b.trips++
	b.lastBankAt = time.Now()
	if reason == fighterplan.BankGoal {
		b.goalBanked = true
		return
	}
	ctx.logf("advancedfighter: heading back to the spot")
	fighterrunner.WalkNear(d, b.anchor, 3)
}

func (b *advancedFighterBot) lootStep(ctx *Context, d *fighterrunner.Deps) {
	filter := b.lootFilter()
	drop, ok := fighterrunner.FindLoot(d, filter, b.anchor, b.cfg.leashRadius+4, b.killSites, nowMs(), killDropWindowMs)
	if !ok {
		return
	}
	if fighterrunner.PackFull(d) {
		// No slot-freeing dance ported (see fighterplan's doc comment) — a full pack with
		// loot waiting just banks on the next stage-decision pass instead.
		return
	}
	before := lootPackBefore(ctx)
	if fighterrunner.LootOnce(d, drop) {
		b.looted++
		b.loot.taken(ctx, before, drop.X, drop.Z, drop.Level)
	}
}

// fightStep runs one bounded fight session: pick or hold a target, attack, and handle
// upkeep (eating) between engagements, similar in spirit to AdvancedFighter.ts's own Fight
// task owning the bot for up to ninety seconds at a time — bounded shorter here (20s) so a
// stop request, a death, or a newly-due bank/panic condition is picked back up promptly by
// the ordinary stage dispatch.
func (b *advancedFighterBot) fightStep(ctx *Context, d *fighterrunner.Deps) {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Stopped() || b.died || ctx.Session.State().IsDead() {
			return
		}
		if ctx.Components != nil {
			if dlg := actions.NewChatDialog(ctx.Session, ctx.Components); dlg.CanContinue() {
				return
			}
		}
		if b.cfg.fleeFromPlayers && fighterrunner.PlayerThreat(d) {
			return
		}
		hpFrac := fighterrunner.HPFraction(d)
		foodCount := fighterrunner.FoodCount(d, b.cfg.food)
		if hpFrac < b.cfg.panicAtFrac && foodCount == 0 {
			return
		}
		if fighterfood.ShouldEat(fighterfood.EatInput{
			HP: hpVal(d), MaxHP: maxHPVal(d), Heal: fighterfood.FoodHealAmount(b.cfg.food),
			FoodCount: foodCount, Mode: b.cfg.eatMode, ThresholdFrac: b.cfg.eatAtFrac,
		}) {
			if fighterrunner.EatOnce(d, b.cfg.food) {
				b.eats++
			}
			continue
		}
		if b.cfg.buryBones && hasBoneInPack(d) && !ctx.Session.State().InCombat() {
			if fighterrunner.BuryOne(d, advancedFighterBones) {
				b.buried++
			}
			continue
		}

		var here tile.Tile
		if x, z, l, ok := ctx.Session.State().Tile(); ok {
			here = tile.Tile{X: x, Z: z, Level: l}
		}

		// Kill accounting for the held target.
		if b.fightTargetIdx >= 0 {
			live, found := fighterrunner.TrackNPC(d, b.fightTargetIdx)
			// Judged before the death test below, because this reading is what names the kill:
			// a target that has left the NPC list carries no type id, so it has to come from
			// the last reading in which it was present (killwatch.go).
			verdict := b.fightKill.observe(b.fightTargetIdx, live, found,
				found && live.TargetsPlayer(fighterrunner.SelfSlot(d)))
			dead := !found || (live.TotalHealth > 0 && live.Health == 0)
			if dead {
				if found || b.fightEngagedAt.Before(time.Now()) {
					b.kills++
					b.killsByName["target"]++
				}
				// Every kill this script counts now reaches the loot panel, including one
				// judged from a disappearance. Until 2026-09-22 this was `if found`, with
				// live.Type read from the miss: a corpse removed from the list between two
				// 600 ms polls was counted in b.kills and silently absent from the panel,
				// which is why the panel's kill count ran below the run's own.
				if npcType, x, z, level, took := b.fightKill.killed(verdict); took {
					b.loot.killed(ctx, npcType, "", x, z, level)
				}
				b.killSites = append(b.killSites, fighterplan.KillSite{Tile: here, AtMs: nowMs()})
				// Capped the way CombatTrainer's own list is. Without this the slice grows for
				// the whole life of the run: a long window is thousands of entries, every one
				// re-scanned by BoneFromKill on every loot check, and none of them useful once
				// past the 180s drop window.
				if len(b.killSites) > 16 {
					b.killSites = b.killSites[len(b.killSites)-16:]
				}
				b.fightTargetIdx = -1
				ctx.DelayTicks(1)
				continue
			}
			if !b.cfg.attackOthersTargets && live.TargetsAnotherPlayer(fighterrunner.SelfSlot(d)) {
				b.skip[live.Index] = nowMs() + 20_000
				b.fightTargetIdx = -1
				continue
			}
			if ctx.Session.State().InCombat() {
				ctx.Delay(600 * time.Millisecond)
				continue
			}
			if fighterplan.EngagementStale(nowMs(), b.fightEngagedAt.UnixMilli(), 8000) {
				// Ask the pathfinder before re-clicking, and this is the branch that actually
				// wedged runs. A target picked while it was reachable, or picked before the
				// pack could answer, stays HELD across Loop calls: the click is accepted
				// through the wall so AttackNPC returns true, the fight never starts so
				// InCombat() stays false, and this re-attacks the same NPC every eight seconds
				// for the rest of the window without writing a line. Dropping it and skipping
				// it for twenty seconds costs a genuinely reachable monster one retry; not
				// dropping it cost whole runs.
				at := tile.Tile{X: live.X, Z: live.Z, Level: live.Level}
				if reach, why := fighterrunner.Reachable(d, at); !reach {
					b.warn(ctx, "noroute-held", time.Minute,
						"no route from here to the held target at %d,%d,%d (%s) — dropping it rather than re-clicking through a wall",
						at.X, at.Z, at.Level, why)
					b.skip[live.Index] = nowMs() + 20_000
					b.fightTargetIdx = -1
					continue
				}
				if fighterrunner.AttackNPC(d, live) {
					b.fightEngagedAt = time.Now()
				} else {
					b.fightTargetIdx = -1
				}
			}
			ctx.Delay(600 * time.Millisecond)
			continue
		}

		if ctx.Session.State().InCombat() {
			ctx.Delay(600 * time.Millisecond)
			continue
		}

		npcs := fighterrunner.TargetNPCs(d, b.cfg.targets)
		recs := make([]fighterplan.TargetRecord, 0, len(npcs))
		byIndex := map[int]int{}
		for i, n := range npcs {
			recs = append(recs, fighterrunner.ToTargetRecord(d, n, here))
			byIndex[n.Index] = i
		}
		// passedOver counts the better-scoring candidates the gate refused before it found one
		// with a route. PickTarget stops asking once it has a winner, so this is exactly "how
		// many targets we would have clicked through a wall to get to", which is the number
		// worth seeing. Without it the gate is invisible in a healthy run: it quietly skips the
		// walled spawns and the log is identical to a stand that never had any.
		passedOver := 0
		reach := fighterrunner.ReachableRecord(d)
		pick, ok := fighterplan.PickTarget(recs, fighterplan.TargetContext{
			Anchor: b.anchor, Leash: b.cfg.leashRadius, OwnIndex: b.fightTargetIdx, HaveOwn: false,
			AttackOthersTargets: b.cfg.attackOthersTargets, PreferAttackers: b.cfg.preferAttackers,
			SkipUntil: b.skip, NowMs: nowMs(),
			Reachable: func(t fighterplan.TargetRecord) bool {
				if reach(t) {
					return true
				}
				passedOver++
				return false
			},
		})
		if !ok {
			b.warnNoRoute(ctx, recs)
			return
		}
		if passedOver > 0 {
			b.warn(ctx, "noroute-passed", time.Minute,
				"no route to %d nearer target(s) from where we are standing — fighting the %s at %d,%d,%d instead",
				passedOver, pick.Name, pick.Tile.X, pick.Tile.Z, pick.Tile.Level)
		}
		npc := npcs[byIndex[pick.Index]]
		if pick.Distance > 8 {
			fighterrunner.WalkNear(d, tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}, 5)
			continue
		}
		if fighterrunner.AttackNPC(d, npc) {
			b.fightTargetIdx = npc.Index
			b.fightEngagedAt = time.Now()
			ctx.Delay(600 * time.Millisecond)
		} else {
			b.skip[npc.Index] = nowMs() + 20_000
		}
	}
}

// handoffStep runs one full mule hand-over: open the fighter's own bank, withdraw loot
// (fighterplan.MuleRows — gear/food/bones-to-bury always stay, whatever muleKeep says),
// walk to the mule's post and drive donorrunner.Run, then walk back to the spot. Blocking,
// like bankRun/fightStep already are — one Loop call, cancellable throughout via ctx.Delay.
// Mirrors internal/bot/essence.go's own handoffStep, the simplest of the three existing
// donor scripts (no travel-fare coin split — a fighter's food, not coins, is what a bank
// trip actually needs, and AdvancedFighter.ts carries no such reserve either).
func (b *advancedFighterBot) handoffStep(ctx *Context, d *fighterrunner.Deps, settings donorplan.Settings) error {
	bank, ok := fighterrunner.OpenBankNear(d, b.anchor, b.cfg.leashRadius+10)
	if !ok {
		ctx.logf("advancedfighter: hand-off: could not reach or open the bank — will retry")
		ctx.Delay(5 * time.Second)
		return nil
	}
	filter := b.lootFilter()
	keep := b.keepInput()
	muleKeepNames := settings.Keep
	rows := fighterplan.MuleRows(fighterrunner.BankItemRows(d, bank), muleKeepNames, keep, filter, fighterfood.IsFoodItem)
	// THE WHOLE DRAW IS ONE BURST (twopertick-1012): note mode on, a Withdraw-All per row (a note takes one pack slot, so
	// as many rows as the pack has free slots) and note mode off go out together, five user events a tick, and are
	// confirmed once. It was a note-mode switch that waited on the varp, a Withdraw-All and a wait per row, and another
	// switch and wait.
	drawNames := make([]string, 0, len(rows))
	for _, row := range rows {
		drawNames = append(drawNames, row.Name)
	}
	noteDraw, asked := bank.DrawAllAsNotes(bank.RowIDsByName(d.Config, drawNames), actions.NoteResolver(d.Config), fighterrunner.FreeSlotsInPack(d), fighterrunner.Waiter(d))
	if asked > 0 {
		ctx.logf("advancedfighter: hand-off: %s", actions.VisitLog("note draw", asked+2, noteDraw, 0))
	}
	rowsLeft := len(fighterplan.MuleRows(fighterrunner.BankItemRows(d, bank), muleKeepNames, keep, filter, fighterfood.IsFoodItem))
	_ = bank.Close()

	packRows := func() []donorplan.ItemRow { return fighterrunner.PackSlotRows(d) }
	drawn := fighterplan.MuleHaul(packRows(), muleKeepNames, keep, filter, fighterfood.IsFoodItem)
	if drawn <= 0 {
		b.handoffPending = false
		b.handoffNextAllowed = time.Now().Add(donorplan.HandoffRetry)
		ctx.logf("advancedfighter: hand-off: nothing drawn from the bank — resuming the fight")
		return nil
	}
	ctx.logf("advancedfighter: hand-off to %q — %d row(s) in the bank, %d pack slot(s) drawn, %d row(s) left for another load",
		settings.Mule, len(rows), drawn, rowsLeft)

	rd := &donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		Bus: ctx.Bus, Me: ctx.Username,
	}
	// Reach a mule that is logged in to a different world (worldhopplan bounds it; a
	// one-world fleet no-ops). See bot.WireDonorWorlds.
	WireDonorWorlds(ctx, rd)
	res := donorrunner.Run(rd, donorrunner.Options{
		Mule: settings.Mule, Stand: b.cfg.muleStand,
		RowsInBank: len(rows), Drawn: drawn,
		Load: func() []donorplan.LoadGroup {
			return fighterplan.MuleLoad(packRows(), donorplan.DefaultMuleFreeSlots, muleKeepNames, keep, filter, fighterfood.IsFoodItem)
		},
		HaulLeft: func() int {
			return fighterplan.MuleHaul(packRows(), muleKeepNames, keep, filter, fighterfood.IsFoodItem)
		},
		KnownUntradable: b.knownUntradable,
	})

	b.handoffPending = false
	switch {
	case res.Stopped:
		ctx.logf("advancedfighter: hand-off interrupted by a stop request")
		return nil
	case res.GaveUp:
		b.handoffNextAllowed = time.Now().Add(donorplan.HandoffRetry)
		ctx.logf("advancedfighter: hand-off: %s — resuming the fight, next attempt no sooner than %s", res.Reason, b.handoffNextAllowed.Format(time.Kitchen))
	case res.Traded:
		b.handoffsDone++
		b.handedOver += res.Slots
		ctx.logf("advancedfighter: hand-off #%d complete — %d trade(s), %d pack slot(s) crossed to %q", b.handoffsDone, res.Loads, res.Slots, settings.Mule)
		if rowsLeft > 0 {
			b.handoffPending = true
			ctx.logf("advancedfighter: %d bank row(s) still for %q — drawing the next load", rowsLeft, settings.Mule)
		}
	default:
		ctx.logf("advancedfighter: hand-off ended without trading (%s)", res.Reason)
	}
	return nil
}

func (b *advancedFighterBot) OnStop(ctx *Context) {
	mins := b.minutes()
	ctx.logf("advancedfighter: stopped — %.1f min, %d kills, %d drops, %d bank trips, %d deaths, %d bites, %d bones, %d slot(s) handed to the mule in %d hand-over(s)",
		mins, b.kills, b.looted, b.trips, b.deaths, b.eats, b.buried, b.handedOver, b.handoffsDone)
}
