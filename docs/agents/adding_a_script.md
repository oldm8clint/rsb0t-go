# How to add a script

A script is three small pieces. Keep them separate; the separation is what makes scripts testable without a
server, and what lets a proof tool run exactly the code the fleet runs.

```
internal/scripts/<name>plan/     pure decisions, no I/O           tests: plain structs in, plain values out
internal/scripts/<name>runner/   live glue                        tests: a fake session where it pays
internal/bot/<name>.go           Bot + settings + Register(...)   tests: settings decoding
```

The smallest complete example is **VialFiller**: `internal/bot/vialfiller.go` (about 160 lines),
`internal/scripts/vialplan/`, `internal/scripts/vialrunner/`. **BoneBurier** (`boneplan`, `boneburierrunner`) is
a second small one with a stage dispatcher. The long form of this page, with the reasoning, is
[script_porting_framework.md](../script_porting_framework.md). Read one existing script before writing yours.

## 1. Decide what the script is

Write down, before any code: the loop in one sentence ("bank, walk to the trees, chop until the pack is full,
repeat"), what it needs (a collision pack to walk? the config archive to resolve names? a bank?), what it takes
from the player as settings, and when it stops. Check `grep -rn 'Register(' internal/bot/*.go` so you are not
duplicating a script.

## 2. The plan package: `internal/scripts/<name>plan`

No imports of `client`, `actions`, `fleet`, `bot` or anything that does I/O. It holds:

- **`Settings`**, with `DefaultSettings()`, a `Normalised()` that applies bounds, and a strict
  `DecodeSettings(raw map[string]interface{}) (Settings, error)`. Strict means: an unknown key, a value of the
  wrong type or an out-of-range number is an error that names the key. A typo in a setting must stop the
  script from starting, not run it with the default. Numbers may arrive as JSON numbers or as numeric
  strings (the panel's forms send strings). See `essenceplan/settings.go` for a full example.
- **Decisions as pure functions of plain values**: which stage comes next, whether a threshold is crossed,
  which item name matches, whether a stall has gone on too long. Anything time-dependent takes `time.Time` as a
  parameter so a test never sleeps.
- **Places and names**: tiles, item and loc names. Look names up in the config archive; do not trust an id you
  copied from somewhere (see the first entry of [gotchas.md](gotchas.md#world-interaction)).

Write the table tests as you go. If you cannot test a decision without a fake session, it does not belong here.

## 3. The runner package: `internal/scripts/<name>runner`

Everything that touches the world: walking, clicks, bank open/deposit/withdraw, dialogue, shops, waiting.
It takes a `Deps` struct (the session, pack, loc index, config, interface components, chat log, a `Delay`
function and a logger) rather than the bot, so a `cmd/` tool can use it with `RealDelay` and the registered
script can use the cancellable `ctx.Delay`.

Rules the existing runners follow, each from a failure:

- Call `WaitForGuardian` before acting, so a random event is dealt with before you click anything.
- Handle the standing interrupts first: dead, a dialogue to continue, a modal you did not open.
- Never send an action and assume it worked. Read state afterwards (`Inventory`, position, XP, messages).
- Read `MESSAGE_GAME` after an interaction that "did nothing": the server usually said why.
- Resolve locs and NPCs by name through the loc index and config, and stand where
  [`internal/locreach`](../../internal/locreach) says the interaction can run.
- Walk with `webwalk`/`pathfind`; an action never walks for you.
- Give every wait an upper bound and every retry a cap. A loop that cannot give up is a stuck account.
- Log one clear line per behaviour, including the *reason* for a skip or a retry, so a reader of a run can
  judge the change without a debugger.

## 4. The registration: `internal/bot/<name>.go`

```go
package bot

func init() { Register("MyScript", func() Bot { return &myBot{} }) }

type myBot struct {
	settings myplan.Settings
	// only what must survive from one Loop call to the next
}

func (b *myBot) OnStart(ctx *Context) error {
	set, err := myplan.DecodeSettings(ctx.Settings) // strict: unknown keys fail the start
	if err != nil { return err }
	if ctx.Pack == nil { // fail clearly instead of panicking later inside pathfinding
		return fmt.Errorf("myscript: no collision pack configured")
	}
	b.settings = set
	ctx.logf("myscript: starting, ...")
	return nil
}

func (b *myBot) Loop(ctx *Context) error {
	// One unit of work per call (one trip, one obstacle, one lap) so a stop takes effect quickly.
	// Return nil to be called again; return an error to stop with that message.
	return nil
}

func (b *myBot) OnStop(ctx *Context) { ctx.logf("myscript: stopped") }
```

- `Register` panics on a duplicate name; that is intended.
- The bot file never talks to the session directly. It builds `Deps`, calls into the runner, and reads
  decisions out of the plan.
- Keep fields on the bot only for state that must survive between `Loop` calls. State the bot *learned* (a shelf
  that was empty, a route that failed) should live somewhere that survives a restart of the bot if a parent
  can restart it, or it will be re-learned, expensively, every time.
- For a form in the setup page, also declare a typed `settings.Schema` and add it to `scriptSchemas` in
  `internal/bot/schemas.go` (see `HerbCleaner`). Scripts without one are configured with a JSON object.

## 5. Tests

- `internal/scripts/<name>plan/*_test.go`: the decisions and the settings (defaults, bounds, unknown key,
  wrong type).
- `internal/bot/<name>_settings_test.go`: that the registered script decodes its documented settings and
  rejects a typo. Copy `vialfiller_settings_test.go`.
- A runner test where a fake session pays for itself (see `internal/scripts/fisherrunner/containers_test.go`).
- If the script walks, run the real-data tests ([testing.md](testing.md)) so walking is exercised.

## 6. Prove it on a game server you run

Unit tests prove your decisions, not that the game agrees. Run the script on an account against a local engine
([local_engine.md](local_engine.md)) and read the log. If you write a `cmd/` proof tool for it, build it on
`internal/harness` and read [trustworthy_harnesses.md](../trustworthy_harnesses.md) first: assert what was
*gained* (items, XP, kills), not where the account stood, and make the setup a precondition that voids the run
when it fails.

## 7. Document it

- Add the script to the list in `README.md` and its settings to
  [bot_script_settings.md](../bot_script_settings.md).
- If you had to learn something the hard way, add it to [gotchas.md](gotchas.md).
- Say honestly how far it got: ported, unit-tested, run on a local engine, run for hours. Do not call it
  finished if you only did the first.

## Checklist

- [ ] Plan package has no I/O imports; decisions are table-tested.
- [ ] `DecodeSettings` is strict and tested with a typo.
- [ ] Runner takes `Deps`, calls `WaitForGuardian`, verifies results from state, bounds every wait.
- [ ] Bot file only registers, decodes settings, and delegates; `ctx.Delay` instead of `time.Sleep`.
- [ ] One unit of work per `Loop` call, so a stop is prompt.
- [ ] Loc and NPC lookups are by name; interaction tiles come from `locreach`.
- [ ] Log lines say what happened and why.
- [ ] `go build ./... ; go vet ./... ; go test -p 1 ./...` all exit 0.
- [ ] README, settings docs and (if relevant) `docs/agents/` updated.
