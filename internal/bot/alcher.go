package bot

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/alchplan"
	"rsb0t-go/internal/scripts/alchrunner"
)

func init() {
	Register("Alcher", func() Bot { return &alcherBot{} })
}

// alcherBot ports Alcher.ts's own loop() 1:1: draw a bank trip's worth of a noted item plus
// the runes for it, cast back to back until the pack can't fund another, restock, repeat until
// the bank has none of the listed items left.
//
// No mule hand-off: Alcher.ts imports no donorPlan/muleProtocol at all — coins earned from
// alching stay in the account's own pack/bank; there is nothing tradeable this script
// accumulates for a mule to collect.
//
// The one genuinely new Go-side primitive this port needed: casting a spell on a backpack
// item. See internal/actions/magic.go's CastOnItem/itemSpells and
// internal/scripts/alchrunner's own doc comment for how the OPHELDT payload and the two
// alchemy spell components (1162 low, 1178 high) were derived and confirmed.
//
// Settings, keyed exactly like Alcher.ts's own settingsSchema:
//
//	spell        string default "Best for level" — or "High Level Alchemy"/"Low Level Alchemy".
//	items        string default "Rune platebody" — comma-separated, alched in list order.
//	castsPerTrip int    default 100              — notes and runes drawn each bank trip.
//	useFireStaff bool   default true              — fetch and wear one from the bank if there is one.
//	logDetail    string default "Normal"          — "Verbose" logs every stage change.
type alcherBot struct {
	settings alchplan.Settings
	spell    *alchplan.AlchSpell

	staffChecked bool
	bankEmpty    bool
	stage        alchplan.Stage
	stopped      string

	misses    int
	casts     int
	coinsGot  int
	trips     int
	xpAtStart int
	startedAt time.Time
	warnedAt  map[string]time.Time
}

// missLimit is casts that dispatched but paid no xp in a row before the run warns — matches
// Alcher.ts's own MISS_LIMIT.
const missLimit = 3

func (b *alcherBot) deps(ctx *Context) *alchrunner.Deps {
	return &alchrunner.Deps{
		Session:    ctx.Session,
		Pack:       ctx.Pack,
		Locs:       ctx.Locs,
		Config:     ctx.Config,
		Components: ctx.Components,
		Chat:       ctx.Chat,
		Delay:      ctx.Delay,
		Log:        ctx.Log,
		Loot:       ctx.Loot,
	}
}

func (b *alcherBot) OnStart(ctx *Context) error {
	b.settings = alchplan.Settings{
		Spell:        StringSetting(ctx.Settings, "spell", alchplan.AutoSpell),
		Items:        StringSetting(ctx.Settings, "items", alchplan.DefaultItems),
		CastsPerTrip: IntSetting(ctx.Settings, "castsPerTrip", alchplan.DefaultCastsPerTrip),
		UseFireStaff: BoolSetting(ctx.Settings, "useFireStaff", true),
		LogDetail:    StringSetting(ctx.Settings, "logDetail", "Normal"),
	}.Normalised()
	b.warnedAt = map[string]time.Time{}
	b.startedAt = time.Now()
	d := b.deps(ctx)
	b.xpAtStart = alchrunner.MagicXP(d)

	magic := alchrunner.MagicLevel(d)
	b.spell = alchplan.ChooseSpell(b.settings.Spell, magic)
	if b.spell == nil {
		b.stop(ctx, "not castable at the account's current Magic level")
		return nil
	}
	if len(b.settings.ItemList()) == 0 {
		b.stop(ctx, "no items listed to alch")
		return nil
	}
	ctx.logf("alcher: %s on [%s], %d casts a trip, %s", b.spell.Name, b.settings.Items, b.settings.CastsPerTrip,
		map[bool]string{true: "fire staff if the bank has one", false: "fire runes only"}[b.settings.UseFireStaff])
	return nil
}

func (b *alcherBot) vlog(ctx *Context, format string, args ...any) {
	if b.settings.Verbose() {
		ctx.logf("alcher: "+format, args...)
	}
}

func (b *alcherBot) warn(ctx *Context, key string, everyMs time.Duration, format string, args ...any) {
	if time.Since(b.warnedAt[key]) < everyMs {
		return
	}
	b.warnedAt[key] = time.Now()
	ctx.logf("alcher: WARNING: "+format, args...)
}

func (b *alcherBot) stop(ctx *Context, reason string) {
	b.stopped = reason
	ctx.logf("alcher: stopped: %s", reason)
}

func (b *alcherBot) Loop(ctx *Context) error {
	if b.stopped != "" || b.spell == nil {
		ctx.Delay(3 * time.Second)
		return nil
	}
	d := b.deps(ctx)
	if !alchrunner.WaitForGuardian(d) {
		return nil
	}
	if ctx.Session.State().IsDead() {
		if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
			ctx.Session.State().ClearDead()
		}
	}
	dlg := actions.NewChatDialog(ctx.Session, ctx.Components)
	if dlg.CanContinue() {
		_ = dlg.Continue()
		ctx.Delay(300 * time.Millisecond)
		return nil
	}

	items := b.settings.ItemList()
	_, itemInPack := alchplan.NextItem(items, func(n string) int { return alchrunner.PackCountByName(d, n) })
	castsInPack := alchrunner.CastsAffordableFor(d, *b.spell, alchrunner.HasFireStaff(d))

	stage := alchplan.DecideStage(alchplan.Snapshot{
		ItemInPack:   itemInPack,
		CastsInPack:  castsInPack,
		StaffWanted:  b.settings.UseFireStaff,
		HasStaff:     alchrunner.HasFireStaff(d),
		StaffChecked: b.staffChecked,
		BankEmpty:    b.bankEmpty,
	})
	if stage != b.stage {
		b.stage = stage
		b.vlog(ctx, "-> %s", alchplan.DescribeStage(stage))
	}
	switch stage {
	case alchplan.StageGear:
		b.fetchStaffStep(ctx, d)
	case alchplan.StageRestock:
		b.restockStep(ctx, d, items)
	case alchplan.StageAlch:
		b.castStep(ctx, d)
	case alchplan.StageDone:
		b.stop(ctx, "the bank has none of the listed items left")
	}
	return nil
}

func (b *alcherBot) OnStop(ctx *Context) {
	ctx.logf("alcher: stopped — %d casts, %d bank trips, +%d coins, +%d magic xp",
		b.casts, b.trips, b.coinsGot, b.xpDelta(ctx))
}

func (b *alcherBot) xpDelta(ctx *Context) int {
	d := b.deps(ctx)
	return alchrunner.MagicXP(d) - b.xpAtStart
}

func (b *alcherBot) fetchStaffStep(ctx *Context, d *alchrunner.Deps) {
	bank, ok := alchrunner.OpenNearestBank(d, 30)
	if !ok {
		b.warn(ctx, "bank-open", 30*time.Second, "could not find or open a bank — retrying")
		ctx.Delay(3 * time.Second)
		return
	}
	name, found, ok := alchrunner.FetchFireStaff(d, bank)
	_ = bank.Close()
	b.staffChecked = true
	switch {
	case !ok:
		b.warn(ctx, "staff", 30*time.Second, "could not fetch/wear the %s — casting with fire runes", name)
	case !found:
		ctx.logf("alcher: no fire staff in the bank — casting with fire runes")
	default:
		ctx.logf("alcher: wielding the %s — casts need only nature runes now", name)
	}
}

func (b *alcherBot) restockStep(ctx *Context, d *alchrunner.Deps, items []string) {
	bank, ok := alchrunner.OpenNearestBank(d, 30)
	if !ok {
		b.warn(ctx, "bank-open", 30*time.Second, "could not find or open a bank — retrying")
		ctx.Delay(3 * time.Second)
		return
	}
	item, ok := alchrunner.Restock(d, bank, items, *b.spell, b.settings.CastsPerTrip, alchrunner.HasFireStaff(d))
	_ = bank.Close()
	if !ok {
		if item == "" {
			b.bankEmpty = true
		}
		b.warn(ctx, "restock", 30*time.Second, "restock did not complete — retrying")
		return
	}
	b.bankEmpty = false
	casts := alchrunner.CastsAffordableFor(d, *b.spell, alchrunner.HasFireStaff(d))
	if casts <= 0 {
		b.stop(ctx, "the bank has no runes left to cast with")
		return
	}
	b.trips++
	ctx.logf("alcher: trip %d: %s noted, runes for %d casts", b.trips, item, casts)
}

func (b *alcherBot) castStep(ctx *Context, d *alchrunner.Deps) {
	for i := 0; i < alchplan.CastsPerLoop; i++ {
		if ctx.Stopped() {
			return
		}
		name, ok := alchplan.NextItem(b.settings.ItemList(), func(n string) int { return alchrunner.PackCountByName(d, n) })
		if !ok || alchrunner.CastsAffordableFor(d, *b.spell, alchrunner.HasFireStaff(d)) <= 0 {
			return
		}
		if actions.NewChatDialog(ctx.Session, ctx.Components).CanContinue() {
			return
		}
		coinsBefore := alchrunner.PackCountByName(d, alchplan.Coins)
		outcome := alchrunner.CastOnce(d, *b.spell, name)
		switch outcome {
		case alchrunner.Cast:
			b.misses = 0
			b.casts++
			if gained := alchrunner.PackCountByName(d, alchplan.Coins) - coinsBefore; gained > 0 {
				b.coinsGot += gained
			}
			continue
		case alchrunner.LevelTooLow:
			b.stop(ctx, "the server refused the spell for the magic level")
			return
		case alchrunner.NotEnough:
			b.warn(ctx, "runes", 10*time.Second, "the server says the runes are short — restocking")
			b.miss(ctx)
			return
		case alchrunner.Refused:
			b.vlog(ctx, "%s on %s was refused — retrying", b.spell.Name, name)
			b.miss(ctx)
			return
		case alchrunner.NoXP:
			b.miss(ctx)
			return
		case alchrunner.NoItem:
			return
		}
	}
}

func (b *alcherBot) miss(ctx *Context) {
	b.misses++
	if b.misses >= missLimit {
		b.warn(ctx, "misses", 15*time.Second, "%d casts in a row did not land — retrying", b.misses)
		b.misses = 0
	}
	ctx.Delay(1200 * time.Millisecond)
}
