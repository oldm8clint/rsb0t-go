# Script porting framework

A checklist for turning one more `rsb0t-scripts/src/*.ts` bot into a registered
`internal/bot` script, distilled from the five ports that actually exist as of this writing
(`BarbarianAgility`, `ProgressiveFisher`, `ProgressiveCrafter`, `MuleReceiver`, and
`EssenceMiner` — the last one landed alongside this document, as its own proof that the
recipe below is real and not aspirational). Every claim here cites the file, function, or
COLLAB.md incident it comes from. If you are about to port script #6, start here, not from
scratch.

Companion reading, in this order:
1. `reference/rsb0t-scripts/README.md` — the TS→Go primitive mapping for the four originally
   curated scripts. Narrower than this document (one port's own after-the-fact notes,
   not a forward-looking recipe) but still the right place to check whether a Go primitive
   your new script needs already has a proven TS-side analogue.
2. `docs/script_parity_status.md` — the authoritative per-script status. Update it when you're
   done, honestly reflecting whichever stage you actually reached.
3. `docs/script_api_coverage.md` — whether the scripting-API surface your script needs
   (walking, a specific interface, a settings key shape) already exists or needs adding.
4. `PLAN.md`'s C3/C4 sections — the original dependency-ordered rollout plan and sizing notes.

## The three-layer shape

Every ported script is exactly three files/packages, never more, never fused:

```
internal/scripts/<name>plan/     pure decision logic — no I/O, unit-tested
internal/scripts/<name>runner/   live glue — walking, clicking, banking, one script's own I/O
internal/bot/<name>.go           the thin adapter: Bot interface, settings, Register()
```

This is not a style preference; it is what makes each layer testable and reusable at all:
`<name>plan` has zero imports of `internal/actions`/`internal/client`/`internal/bot` (grep any
existing `plan.go` — `essenceplan/plan.go`'s own package doc comment states this explicitly:
"no client, actions, fleet or bot imports and no I/O"), so its tests run in milliseconds with
no live server. `<name>runner` is shared between a registered `internal/bot` script and any
standalone `cmd/*test` tool that wants the identical live-proven sequence (see
`fisherrunner`'s own package doc: "shared by `cmd/fishtest` ... and `internal/bot`'s registered
... script, so the exact live-proven sequencing ... lives in exactly one place"). `internal/bot/<name>.go`
never talks to the session directly — it calls into `<name>runner` and reads decisions out of
`<name>plan`.

### How to read a TS script and split it

Open the TS file (`EssenceMiner.ts`, `CasketOpener.ts`, whichever is next) and its own
`*Plan.ts`/`*Stage.ts` sibling if one exists (`essencePlan.ts`, `fishPlan.ts`,
`craftStage.ts`, `barbarianPlan.ts`). The split the TS authors already made is almost always
the split you want:

- **Goes in `<name>plan`**: anything that is a pure function of already-known state —
  a `decideStage`/`minerStage`/`decidePhase`-style dispatcher, a lookup table (pickaxe tiers,
  fishing rungs, obstacle order), a threshold check (`handoffDue`, `needsPickaxeCheck`), a
  string/regex classifier (`isQuestRefusalMessage`, `isRawFish`). If you could write a table
  test for it with plain structs and no mock session, it belongs here. Every existing
  `<name>plan` package is built this way — `essenceplan/plan.go`'s `DecideStage`,
  `ResolvePick`, `MineStalled`, `TeleportGivingUp` all take plain values and return plain
  values; none of them touch a clock except by taking `time.Time` as a parameter (see
  `MineStalled(lastGain, now time.Time)` — testable without `time.Sleep`).
- **Goes in `<name>runner`**: anything that touches `Game.tile()`, `Inventory.*`, `Bank.*`,
  `Npcs.query()`, `Locs.query()`, `Shop.*`, `ChatDialog.*`, `Execution.delay*`, or awaits a
  promise at all. This is the walking, the `OpNpc`/`OpLoc` clicks, the bank open/deposit/
  withdraw sequences, the "find Aubury within 8 tiles" search. It reads live state, builds a
  `<name>plan` input struct from it, and acts on the plan's decision.
- **Goes in `internal/bot/<name>.go`**: the TS class's own field list (what must survive one
  `loop()` call to the next — `checkedPickaxe`, `teleportFails`, `questRefused`,
  `handoffPending` are exactly `essenceBot`'s own struct fields, ported 1:1 from
  `EssenceMiner`'s `private` fields) plus the settings schema, `OnStart`/`Loop`/`OnStop`, and
  wiring the mule hand-off in ahead of the ordinary stage dispatch (see below).
- **Never ported** (confirm this for every new script, don't assume): the HUD/panel/overlay
  file (`*Hud.ts`, `hudPanel.ts`), the update-checker, the bank/tally feed publishers — no
  headless Go client has an on-screen overlay to feed. `essenceplan/plan.go`'s own doc comment
  spells this out ("Deliberately not ported here, and why") — copy that pattern: say what you
  left out and why, in the new package's doc comment, not just silently drop it.

One TS→Go structural difference worth calling out explicitly, because all five ports made the
same call: **TS's `loop()` is often a resumable async state machine (`this.handoff = {phase:
'wait', ...}`, checked again next `loop()` tick); the Go port instead runs a whole hand-off, or
a whole obstacle attempt, or a whole bank trip, as **one blocking `Loop()` call**, using
`ctx.Delay`/`d.Delay` throughout so a stop request still lands promptly. Don't try to
faithfully reconstruct TS's `phase`-object resumable-async shape in Go — `bot.Bot.Loop`'s own
doc comment says this outright: *"A script with no natural small unit of work can instead run
its own internal loop inside a single Loop call, checking `ctx.Stopped()`/`ctx.Delay` between
its own iterations."* `fisherBot.handoffStep`/`crafterBot`'s twin/`essenceBot.handoffStep` all
do exactly this: one call drives `donorrunner.Run` to completion (traded, gave up, or
stopped) rather than splitting it across several `Loop` calls the way TS's `stepHandoff`
does.

## Primitive reference: what almost every script needs

Check this table before writing a new helper — five ports independently needing the identical
"walk near, re-click, check drift" loop is why it now has one entry, not five.

| Primitive | Where it lives | Exact call shape | Who already calls it |
|---|---|---|---|
| Walking to a tile | `internal/actions.Walk` + `pathfind.Pack.FindPath` | Duplicated **on purpose** per runner package as `WalkNear(d *Deps, dest tile.Tile, radius int) bool` — see "why duplicated" below | `fisherrunner.WalkNear`, `crafterrunner`'s twin, `barbarianrunner`'s twin, `essencerunner.WalkNear` |
| Random-event pause | `internal/randomevents.Busy` + `.EmergencyFlee` | `WaitForGuardian(d *Deps) bool` — one-line wrapper: `if !randomevents.Busy(s) { return true }`, then poll every 500ms logging every `guardianWaitLogEvery` (10s) | Every runner package; call it first thing in every `Loop`/step function that's about to interact |
| Death recovery | *(usually nothing extra to write)* | `ctx.Session.State().IsDead()` / `.ClearDead()` — clear the flag once `HP()>0`, then let the ordinary stage machine resume | `fisherBot.Loop`, `essenceBot.Loop` — see "death recovery is not separate code" below |
| Fresh-start bootstrap (tutorial skip, pickpocket-fund-a-fare, boat legs) | `internal/scripts/freshstart` (pure) + `internal/scripts/freshstartrunner` (I/O) | `freshstartrunner.PickpocketStep`/`BuySkirtStep`/`SailLeg`, driven by `freshstart.DecidePhase(snapshot)` | `fisherrunner.travelViaFreshStart`, `crafterrunner` |
| Mule hand-off | `internal/scripts/donorplan` (pure) + `internal/scripts/donorrunner` (I/O) | `donorrunner.Run(deps, donorrunner.Options{Mule, Stand, RowsInBank, Drawn, Load, HaulLeft, ...})` — see full worked call below | `fisherBot.handoffStep`, `crafterBot`'s twin, `essenceBot.handoffStep` (three independent scripts, zero copies of the trade state machine) |
| Bank open/deposit/withdraw | `internal/actions.Bank` (+ `internal/locindex` to find the booth) | `openNearestBankBooth`-shaped helper, duplicated per package (see below); `bank.DepositAll`/`WithdrawOne`/`WithdrawAll`/`SetNoteMode`/`WithdrawX` | Every runner touching a bank |
| NPC-by-name search | `d.Config.Npc(type)` + `d.Session.State().NPCs()` | `findNpcByName(d, name, near tile.Tile, radius int, requireOp *string)`, duplicated per package | `fisherrunner`, `essencerunner.FindAubury` |
| Loc-by-name search | `internal/locindex.Index.Find`/`.Nearest` | `d.Locs.Nearest(d.Config, locindex.Query{X,Z,Level,Radius,Name,Op}, live)` where `live := locindex.ChangesFrom(d.Session.State().LocChanges())` | Every runner package that opens a bank booth, a gate, a staircase, a mineable rock, a portal |
| Quest status | `internal/quests.Status` | `quests.Status(state, "Rune Mysteries Quest")` — coarse red/yellow/green only, see that package's own doc comment for why nothing finer is possible | `essenceBot.OnStart`'s warning |
| Chat-line scanning | `internal/chatlog.Log.Since(t)` | Capture `clickAt := time.Now()` before an interaction, then `d.Chat.Since(clickAt)` after, matched against a `regexp` | `essencerunner.TeleportIn` (Aubury's quest-refusal line), `barbarianrunner`, `crafterrunner`'s diagnostic logging |
| Dialogue continue/options | `internal/actions.ChatDialog` | `actions.NewChatDialog(session, components).CanContinue()`/`.Continue()`/`.Options()`/`.ChooseOption()` | Every script's `Loop`, ahead of the ordinary dispatch (a level-up dialog blocks everything) |
| Skill level/XP | `gamestate.State.Skill(statIndex)` | `FishingStat=10`, `CraftingStat=12`, `AgilityStat=16`, `ThievingStat=17`, `MiningStat=14` — see `gamestate/skills.go`'s own names table before guessing a new one | One `<name>runner.<Skill>Level`/`<Skill>XP` pair per script |
| Buying from a shop by name | `internal/actions.Shop`/`OpNpc` | find the keeper via `findNpcByName`, resolve the "Trade" op off `cachecfg.OpIndex` (fall back to option 3 for this content build), `shop.Stock()` matched by `d.Config.Obj(id).Name`, then `shop.Buy(id, n)` | `fisherrunner.BuyTool` (Harry), `essencerunner.BuyFromNurmof` (Nurmof, plus the Dwarven Mine trapdoor hop first — see `ToolAcquire.ts`'s own `walkToToolVendor` if a future vendor also needs one) |

### Why `WalkNear`/bank-booth-open/`findNpcByName` are duplicated, not shared

This looks like an obvious refactor (five nearly-identical `WalkNear` functions!) and every
session that has looked at it has deliberately left it alone. Each copy differs in one
real way — its own `EmergencyFlee` reference point (`fisherrunner` uses
`randomevents.CatherbyBankRef`+150 drift tiles; `donorrunner`/`essencerunner` use 0 drift
tiles because they have no single fixed "home," see `donorrunner.emergencyFleeDriftTiles`'s own
doc comment) — and past sessions found that a shared helper's "one more parameter to thread
through" pressure is exactly how a real bug (a wrong drift reference for one caller) gets
introduced silently. Keep duplicating this specific handful of ~30-60 line functions per
`<name>runner` package unless you have a concrete reason to unify them; it is established
convention here, not an oversight.

### `donorrunner` is already script-agnostic — use it directly, do not reimplement

Confirmed by reading its own package doc comment before this port started: *"Deliberately
built script-agnostic ... every skilling script hands its bank to the same mule at the same
Seers' Village stand ... today [ProgressiveFisher] and ProgressiveCrafter/EssenceRunner
whenever they're wired up next all drive the identical meet-and-trade sequence."* `EssenceMiner`
became the third real caller with zero changes to `donorrunner` itself — only a new
`essenceplan.MuleRows`/`MuleHaul`/`MuleLoad` trio (the same three-function shape
`fisherplan`/`crafterplan` already have) and a `donorrunner.Options` literal in
`essenceBot.handoffStep`. The exact call shape, copied from `internal/bot/essence.go`:

```go
rd := &donorrunner.Deps{
    Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
    Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
}
res := donorrunner.Run(rd, donorrunner.Options{
    Mule: settings.Mule, Stand: <the meet tile — reuse the ordinary bank stand if the TS
                                 script's own meetTile() does>,
    RowsInBank: len(rows), Drawn: drawn,
    Load:     func() []donorplan.LoadGroup { return <plan>.MuleLoad(packRows(), donorplan.DefaultMuleFreeSlots, keep) },
    HaulLeft: func() int { return <plan>.MuleHaul(packRows(), keep) },
    // CoinsName/CoinsID/CoinsHeld/Reserve only if the TS script keeps a travel-fare reserve
    // (ProgressiveFisher does — a fixed 500gp; EssenceMiner does not, since Aubury's
    // teleport is free — check the TS source, don't assume either way).
    KnownUntradable: b.knownUntradable, // map[int]bool, lazily allocated once per bot struct
})
```

`Result.Traded`/`.GaveUp`/`.Stopped` are mutually informative but not exhaustive — always
check `Stopped` first (a plain cancellation, no retry cooldown should apply) before treating
anything else as a real outcome, exactly as `fisherBot.handoffStep`/`essenceBot.handoffStep`
both do.

### Death recovery is not separate code

None of the five ported TS scripts have dedicated death-recovery logic, and neither does the
Go port. `fisherBot`'s own doc comment states the reasoning directly: a death drops the account
near Lumbridge with too little money, which is exactly the state the script's own fund/travel
phase already detects and resolves for an unrelated reason (a fresh account starting the same
way). The only Go-specific addition is clearing `gamestate.State.IsDead()`'s one-way latch once
`HP()>0` is observed again, so a dashboard doesn't show `dead=true` forever after a single
death several minutes ago (found live: a 200-account soak showed 33/200 flagged dead while
actively, normally playing). Do this in every new script's `Loop`, near the top:

```go
if ctx.Session.State().IsDead() {
    if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
        ctx.Session.State().ClearDead()
    }
}
```

Do **not** invent a bespoke "walk back to the course/camp/mine" recovery path unless the TS
source actually has one (`BarbarianAgility`'s `recover()` is the one exception — it explicitly
waits for the respawn teleport and calls `ClearDead`, because agility's own course-position
assumptions don't self-heal the way a bank-centric skilling loop's phase machine does).

## Common bug classes already found — check your new port against every one of these

These are not hypothetical. Each was found live, root-caused, and fixed once; a new port that
reintroduces the same class of bug is a regression, not a new discovery.

1. **Bank-booth-open retrying the same (possibly bad) candidate forever.** Live-found
   2026-09-16: one account timed out for minutes standing right next to a "Bank booth" the
   static collision bake placed slightly wrong, while a second account's identical logic
   opened a different booth instance of the same type two tiles further along without issue
   (`fisherrunner.openNearestBankBooth`'s own doc comment). **Fix pattern**: try up to 4
   nearest-first candidates from `d.Locs.Find`, not just the single nearest one, before
   giving up. `essencerunner.openNearestBankBooth` copies this pattern directly.
2. **Zero-delay busy-loop when a walk fails instantly.** `donorrunner`'s `MeetWalk` case: a
   `FindPath` failure with no delay before retrying produced "12M+ identical log lines in
   under 5 minutes on a real production account" (`acct`, 2026-09-14). **Fix pattern**: every
   retry branch, even one that looks like it can only be reached after a `Delay` elsewhere in
   the loop, needs its own explicit `if !d.Delay(...) { return }` — verify this for the
   specific failure path you're adding, don't assume an earlier branch's delay covers it.
3. **A trade plan recomputed live mid-trade sees its own account's offered items "reappear."**
   `donorrunner.driveTrade`'s own doc comment: recomputing `o.Load()` every tick let `accepted`
   go stale as new "unoffered" groups appeared out of a shrinking pack, looping real
   production hand-offs forever. **Fix pattern**: snapshot the plan once per `Run` call
   (`plan := withoutSkipped(effectiveLoad(o), skip)`), never inside the polling loop.
4. **An untradeable item stalls every future hand-off on rediscovery.** A Genie lamp or
   similar random-event drop that a trade screen silently refuses gets re-offered every single
   hand-off, burning `maxOfferTries` (4) attempts each time. **Fix pattern**:
   `donorrunner.Options.KnownUntradable map[int]bool`, owned by the bot struct (not
   `donorrunner` itself) so it survives across many `Run` calls — every one of the three
   donor scripts allocates one in `OnStart` and passes it through.
5. **WalkNear timeouts tuned against a near-zero-latency local test engine are too short for
   production.** Both `fisherrunner.walkNearTimeout` and `crafterrunner`'s twin were raised
   from a 30s flat cap to 120s after live data showed several independent accounts giving up
   at the exact same real, distance-appropriate waypoint, not an off-route drift. **Fix
   pattern**: don't assume a timeout proven against a local engine survives production
   round-trip time; budget generously (120s is now this project's floor for a "walk somewhere
   nearby" primitive) and let the loop's own within-radius check return early on genuine
   success.
6. **`randomevents.Detect()` still reports an event Guardian has already given up on.** Once
   Guardian's own cooldown kicks in, the world hasn't changed (the NPC/item is still there),
   so a caller checking raw `Detect()` can block forever waiting for a fix that isn't coming.
   **Fix pattern**: use `randomevents.Busy(session)` (cooldown-aware), never bare `Detect()`,
   in a `WaitForGuardian`-style pause.
7. **The Guardian/script action-lock race.** At least four independent sessions rediscovered
   this before it was fixed once (COLLAB.md, 2026-09-14, "essenceplan worktree" entry cites
   the count directly): a script's own bank/shop sequence and Guardian's own random-event
   handler can both touch modal state concurrently with nothing serializing them, so whichever
   click landed last "wins" a value the other side reads as ground truth. **Fix, already
   built, use it, don't reimplement**: `randomevents.Lock(session)`/`.TryLock(session)` — a
   real `sync.Mutex` (`Guardian.actionMu`). Hold `Lock` only around the actual modal-touching
   portion of a bank/shop sequence (not the walk beforehand — see `fisherrunner.BankFish`'s own
   comment on exactly where the lock starts and why), and use `TryLock` inside
   `EmergencyFlee`-style safety checks that should skip rather than block when Guardian
   already owns it.
8. **Position/NPC-position drift after a `REBUILD_NORMAL` zone rebuild.** A real, twice-found
   bug (COLLAB.md "The real blocker at Brimhaven: NPC positions silently drift after every
   zone rebuild," plus the earlier player-position version of the identical bug): the
   rebuild's new `mapBuildBase` was applied, but the next ordinary small-step `PLAYER_INFO`
   update kept adding its delta to the *un-rebased* local coordinate, silently drifting the
   tracked absolute position by the exact rebuild offset from that tick on — with no crash, no
   error, just a client that believes it's somewhere it isn't. **If your new script crosses a
   zone boundary a lot (a big open area, several mapsquares apart)** and starts seeing
   "unreachable"/stuck-in-place symptoms that don't reproduce on a short local test, suspect
   this class of bug first and cross-check with a live `::getcoord` before assuming your own
   new code is wrong.
9. **A decoy NPC sharing a name with the one you actually want.** `BoardShip`'s NPC search
   matched "Customs officer" by name within a flat radius with no way to tell two physically
   distinct NPCs using the identical dialogue script apart — one at Port Sarim, one at Musa
   Point, both named identically, so every account that landed at Musa Point immediately
   reboarded straight back where it came from (commit `dd48769`). **Fix pattern**: when an
   NPC-by-name search can plausibly match more than one real, differently-purposed instance,
   gate the search by *distance from the specific place you expect the real one to stand*, not
   just by name+radius — or by more than one op, the way `fisherrunner.FindSpot` requires both
   `rung.Op` and `rung.Pair` to tell a small-net-and-bait spot apart from a cage-and-harpoon
   one sharing a generic "Fishing spot" name.
10. **Disconnected collision-pack components that look like "one closed door" but aren't.**
    After the Musa Point decoy above was closed off, the destination-leg walk target still
    failed — `cmd/doordiag` proved the two areas were genuinely disconnected flood-fill
    components in the baked pack, joined only by a closed gate pair far outside the ordinary
    nearby-door search radius. **Fix pattern**: when a `WalkNear`/`FindPath` failure persists
    after fixing the obvious cause, don't assume it's a timing issue — check whether the start
    and destination are even in the same reachable component at all (a `cmd/doordiag`-style
    flood-fill probe) before spending more time retrying.
11. **Coins (or any partial-quantity row) counted as haul when only a reserve amount remains.**
    `fisherBot.haulAfterReserve`'s own doc comment: a donor holding exactly its 500gp travel
    reserve (nothing above it) still counted as "1 slot of haul," so it requested and declined
    an empty trade with the mule every ~10 seconds forever. **Fix pattern**: whenever a script
    splits a row at a reserve threshold instead of an all-or-nothing keep, explicitly subtract
    it back out of any haul/stranding computation, not just out of what gets offered.

## What "done" looks like at each stage

Mirrors `docs/script_parity_status.md`'s own tiers — use this table to state your new port's
real status, not an aspirational one:

| Stage | Bar to clear |
|---|---|
| **Pure plan logic** | `<name>plan` package exists, imports nothing from `internal/actions`/`client`/`bot`, and every non-trivial function has a table test in `<name>plan/*_test.go` (every existing `<name>plan` package meets this — `essenceplan/plan_test.go` has 15 test functions covering every exported decision function). |
| **Runtime adapter wired** | `<name>runner` package exists and is called from a registered `internal/bot/<name>.go` (`Register("<Name>", ...)` in an `init()`), covering the TS script's own `loop()` dispatch **and** its settings schema (every TS `settingsSchema` key has a Go `IntSetting`/`StringSetting`/`BoolSetting`/`FloatSetting` read). |
| **Local-engine proof** | At least one real run against a local Lost City test engine, with the actual log lines (or a `cmd/*test` tool's own output) recorded — see `docs/script_parity_status.md`'s "Live evidence" sections for the expected level of detail (exact settings, exact log lines, what was found and fixed). Screenshots/vibes don't count; copy-pasted log output does. |
| **Real-fleet proof** | A recorded run on a real server by the maintainers, or an explicit statement that this hasn't happened yet and why. |
| **Production-proven** | All of `docs/script_parity_status.md`'s own "What parity actually means" eight criteria — TS behavior documented, plan logic tested, runtime translated, cancellation/timeouts/random-events/recovery handled, repeatable local-engine test, real-server validation recorded, control-panel observable, replay/evidence-capable. |

Never claim a stage you haven't actually reached. `docs/script_parity_status.md`'s own
existing entries are deliberately blunt about this (e.g. ProgressiveCrafter's boat leg:
"started live ... but not followed to Seers" — not marked proven).

## Skeleton, not a generator

A `cmd/newscript`-style code generator was considered and deliberately not built: five ports
in, the actual bottleneck has never been typing out three boilerplate files — it's reading the
TS source correctly and finding the right existing primitive instead of reinventing one. A
generator would need its own maintenance every time the `Bot`/`Deps`/`Register` shapes evolve,
for a problem a doc section solves just as fast. Copy this skeleton by hand (or have an agent
do it — it's small enough to hold in one prompt) and fill in the blanks:

**`internal/scripts/<name>plan/plan.go`**
```go
// Package <name>plan is <ScriptName>'s own decisions, ported from rsb0t-scripts'
// <name>Plan.ts / the state machine embedded in <ScriptName>.ts. It is pure: no client,
// actions, fleet or bot imports and no I/O.
//
// Deliberately not ported here, and why:
//   - <hudFile>.ts — the Electron wall's on-screen overlay; no headless equivalent.
//   - <anything else the TS script does that has no Go-side analogue yet>
package <name>plan

// DecideStage/DecidePhase — the TS script's own dispatcher, ported 1:1.
func DecideStage(s Snapshot) Stage { ... }
```

**`internal/scripts/<name>plan/plan_test.go`** — one `Test<Function>` per exported decision
function, table-driven, covering the TS source's own edge cases (a level below a gate, a
threshold exactly met, an empty vs. nil slice).

**`internal/scripts/<name>runner/runner.go`**
```go
// Package <name>runner is the I/O half of the <ScriptName> port... [own doc comment,
// citing what's deliberately not ported and why, matching <name>plan's own list]
package <name>runner

type Delayer func(d time.Duration) bool
func RealDelay(d time.Duration) bool { time.Sleep(d); return true }

type Deps struct {
    Session    actions.Session
    Pack       *pathfind.Pack
    Locs       *locindex.Index
    Config     *cachecfg.Config
    Components archive.Components
    Chat       *chatlog.Log
    Delay      Delayer
    Log        func(string)
}
// + logf, currentTile, waitFor, WaitForGuardian, WalkNear — copy these five nearly verbatim
// from the closest existing sibling runner (see "why duplicated" above), then the
// script-specific reads (Level/Coins/PackFull/...) and step functions (one per Stage/Phase).
```

**`internal/bot/<name>.go`**
```go
func init() { Register("<ScriptName>", func() Bot { return &<name>Bot{} }) }

type <name>Bot struct {
    settings <name>plan.Settings
    // + whatever must survive one Loop call to the next — copy the TS class's own private
    // field list, it's almost always the exact right list.
}

func (b *<name>Bot) OnStart(ctx *Context) error { /* read settings, log a summary line, warn on any gate the TS onStart warns on */ }
func (b *<name>Bot) Loop(ctx *Context) error {
    d := b.deps(ctx)
    if !<name>runner.WaitForGuardian(d) { return nil }
    // death-recovery clear, stray-trade guard, dialogue-continue guard — copy verbatim,
    // every script needs all three
    // hand-off check BEFORE the ordinary stage dispatch, if this script has a mule
    stage := <name>plan.DecideStage(<snapshot built from d>)
    switch stage { /* one case per Stage, each calling one <name>runner function */ }
    return nil
}
func (b *<name>Bot) OnStop(ctx *Context) { ctx.logf("<name>: stopped — <counters>") }
```

Then: register it, write `<name>_settings_test.go` for the settings-reading logic (pure,
mirrors `fisher_settings_test.go`/`essence_settings_test.go`), `go build ./... && go vet ./...
&& go test ./...`, and update `docs/script_parity_status.md`.

## Picking the next script

`PLAN.md`'s C4 section lists the remaining catalogue: `AdvancedFighter`, `Alcher`,
`BoneBurier`, `CasketOpener`, `MeleeFlincher`, `NaturePair`, plus whatever's added upstream
since. `ProgressiveThiever`, `ProgressiveWoodcutter`, `AdvancedFighter`, `MeleeFlincher`,
`BoneBurier`, `CasketOpener` and `Alcher` are now ported — see `docs/script_parity_status.md`'s
own sections for exactly what's proven live and what still needs a healthy local engine to
confirm. `NaturePair` has a fully-tested pure plan package (`naturepairplan`) but no runner/bot
wiring yet and no live proof at all — its own section explains why honestly (it needs two real,
coordinated accounts trading over real Trade windows to prove at all, a materially bigger lift
than any script ported so far) and lists exactly what a future session should build next. Before
assuming a script needs a
brand-new port from zero, check `internal/scripts/` for an existing `<name>plan` package first
— `essenceplan` existed, fully tested, for some time before this session wired its runner and
bot adapter; a future script may be in the identical half-finished state. `docs/PLAN.md`'s own
per-script complexity notes (combat packets for the fighters, spell-casting for
alching/nature-running) tell you which primitive gap to check `docs/script_api_coverage.md`
against first.

**Before writing off a TS constant as "unreachable, lives in the private `@rsb0t/api`
package"**: search every sibling checkout under the parent directory, not just
`reference/rsb0t-scripts/` and the sibling `rsb0t-scripts` checkout. This session's own
`EssenceMiner` port initially did exactly that — concluded `NURMOF_VENDOR`/
`PICKAXE_SHOP_COSTS` were unreachable after checking those two locations, shipped
`upgradePickaxeStep` with the Nurmof leg unported — before a broader grep across the whole
parent directory turned up the real `@rsb0t/api` package source sitting in a sibling
`rsb0t-main` checkout (`rsb0t-main/packages/rsb0t-api`, built from
`src/bot/api/acquisition/ToolAcquire.ts`), with the real vendor tile, hop loc, and price table
right there. Fixed the same session (see `docs/script_parity_status.md`'s EssenceMiner section
for the correction), but it cost a wasted "not ported" writeup that a wider grep up front would
have avoided. A one-line habit: `grep -rl "<THE_CONSTANT_NAME>" <parent-dir>` before writing the
doc-comment that says a primitive can't be reached.
