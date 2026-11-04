package bot

import (
	"strconv"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/casketplan"
	"rsb0t-go/internal/scripts/casketrunner"
)

func init() {
	Register("CasketOpener", func() Bot { return &casketOpenerBot{} })
}

// casketOpenerBot ports CasketOpener.ts's own loop() 1:1: open every casket in the pack, clear
// the reward boxes, and once none are left in the pack check the bank — deposit the rewards and
// any noted caskets, then withdraw the next load, or stop once the bank has none left either.
//
// No mule hand-off: CasketOpener.ts imports no donorPlan/muleProtocol at all — it is a
// one-shot "clear out every casket this account holds" utility, not an ongoing skilling loop
// with a surplus to hand off. Confirmed by reading the whole TS source before writing this.
//
// Settings (CasketOpener.ts itself takes none at all — "No parameters" per its own
// defineBot call):
//
//	logDetail string default "Normal" — "Verbose" logs every bank/withdraw step. Not present
//	                                     in the TS source (which has no settings at all); added
//	                                     here only because every other port's Settings struct
//	                                     has one and Context.Settings needs some key to read.
type casketOpenerBot struct {
	ids     casketrunner.CasketIDs
	idsOK   bool
	keepIDs map[int32]bool

	verbose bool

	stopped string
	opened  int
	coins   int
	finds   map[string]int
	inBank  int

	startedAt time.Time
}

// coinsName is the standard currency; CasketOpener.ts tallies it by its own fixed COINS_ID
// (995) — resolved by name here for the same content-pack-portability reason
// casketrunner.ResolveCasketIDs resolves "Casket" by name rather than trusting a hardcoded id.
const coinsName = "Coins"

func (b *casketOpenerBot) deps(ctx *Context) *casketrunner.Deps {
	return &casketrunner.Deps{
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

func (b *casketOpenerBot) OnStart(ctx *Context) error {
	b.verbose = StringSetting(ctx.Settings, "logDetail", "Normal") == "Verbose"
	b.finds = map[string]int{}
	b.startedAt = time.Now()
	if ctx.Config == nil {
		return nil
	}
	if ids, ok := casketrunner.ResolveCasketIDs(ctx.Config); ok {
		b.ids, b.idsOK = ids, true
	} else {
		ctx.logf("casketopener: WARNING: could not resolve \"Casket\" against the content pack — nothing to open")
	}
	b.keepIDs = map[int32]bool{}
	if b.idsOK {
		d := b.deps(ctx)
		for _, it := range casketrunner.PackItems(d) {
			if !b.ids.IsCasket(int(it.ID)) {
				b.keepIDs[it.ID] = true
			}
		}
	}
	ctx.logf("casketopener: opening every casket in the pack and the bank, then stopping")
	return nil
}

func (b *casketOpenerBot) vlog(ctx *Context, format string, args ...any) {
	if b.verbose {
		ctx.logf("casketopener: "+format, args...)
	}
}

func (b *casketOpenerBot) Loop(ctx *Context) error {
	if b.stopped != "" || !b.idsOK {
		ctx.Delay(3 * time.Second)
		return nil
	}
	d := b.deps(ctx)
	if !casketrunner.WaitForGuardian(d) {
		return nil
	}
	if ctx.Session.State().IsDead() {
		if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
			ctx.Session.State().ClearDead()
		}
	}

	if casketrunner.Openable(d, b.ids) > 0 {
		bank := actions.NewBank(ctx.Session, ctx.Components)
		if bank.IsOpen() {
			_ = bank.Close()
			return nil
		}
		res := casketrunner.OpenBatch(d, b.ids)
		b.opened += res.Opened
		for name, n := range res.Gained {
			casketplan.AddFind(b.finds, name, n)
			if name == coinsName {
				b.coins += n
			}
		}
		if res.Refused {
			ctx.logf("casketopener: WARNING: Open on the casket was refused — retrying")
			ctx.Delay(1200 * time.Millisecond)
		}
		return nil
	}
	dlg := actions.NewChatDialog(ctx.Session, ctx.Components)
	if dlg.CanContinue() {
		_ = dlg.Continue()
		ctx.Delay(200 * time.Millisecond)
		return nil
	}
	b.bankStop(ctx, d)
	return nil
}

func (b *casketOpenerBot) bankStop(ctx *Context, d *casketrunner.Deps) {
	bank, ok := casketrunner.OpenNearestBank(d, 30)
	if !ok {
		ctx.logf("casketopener: WARNING: could not find or open a bank — start at or near one; retrying")
		ctx.Delay(3 * time.Second)
		return
	}
	deposited := casketrunner.DepositRewards(d, bank, b.keepIDs)
	if deposited > 0 {
		b.vlog(ctx, "banked the rewards")
	}
	b.inBank = casketrunner.BankCasketCount(bank, b.ids)
	step := casketplan.BankStepFor(casketplan.BankSnapshot{
		BankCaskets: b.inBank,
		NotedHeld:   casketrunner.NotedHeld(d, b.ids),
		FreeSlots:   casketrunner.PackSlots - casketrunner.Openable(d, b.ids) - casketrunner.NotedHeld(d, b.ids),
	})
	switch step {
	case casketplan.StepDeposit:
		ctx.logf("casketopener: WARNING: noted caskets would not go into the bank — retrying")
		_ = bank.Close()
	case casketplan.StepDone:
		_ = bank.Close()
		b.stopped = "every casket is opened"
		ctx.logf("casketopener: %s — %d in all, %d coins", b.stopped, b.opened, b.coins)
	case casketplan.StepFull:
		_ = bank.Close()
		b.stopped = "caskets are left in the bank, but the pack has no room for one"
		ctx.logf("casketopener: %s — make room and start again", b.stopped)
	case casketplan.StepWithdraw:
		drawn := casketrunner.WithdrawCaskets(d, bank, b.ids)
		_ = bank.Close()
		if drawn > 0 {
			ctx.logf("casketopener: withdrew %d casket(s) (%d left in the bank)", drawn, b.inBank-drawn)
		} else {
			ctx.logf("casketopener: WARNING: the caskets would not come out of the bank — retrying")
		}
	}
}

func (b *casketOpenerBot) OnStop(ctx *Context) {
	found := ""
	for _, row := range casketplan.FindRows(b.finds) {
		if found != "" {
			found += ", "
		}
		found += strconv.Itoa(row.Count) + " " + row.Name
	}
	if found != "" {
		found = ": " + found
	}
	ctx.logf("casketopener: stopped — %d caskets opened%s", b.opened, found)
}
