# Bot script settings

This is the operator-facing reference for per-account `settings` in `fleet.config.json`. The original four adapters were rechecked against their setting readers on **2026-09-24**. Executable readers and plan defaults take precedence over source comments, some of which describe earlier implementations. For the other registered scripts, see the [additional script reference](bot_script_settings_additional.md).

> **Validation note:** implementation and validation are separate facts. A setting can be implemented without the corresponding behavior being production-proven. In particular, `cheatSetup` settings are for local-engine testing and should be disabled for normally provisioned accounts.

The validation paragraphs below retain historical proof summaries; this settings audit did not rerun those scenarios. Consult the dated parity/handoff evidence for current acceptance status.

## Where settings go

Each startup roster entry has the shape:

```json
{
  "username": "example",
  "script": "ProgressiveFisher",
  "settings": {
    "target": "Big net"
  }
}
```

The `script` value must match a registered `internal/bot` name. The `settings` object is passed to that adapter as a flexible `map[string]interface{}`; JSON numbers arrive as `float64`, while adapter helpers convert supported values to the documented setting types.

---

## BarbarianAgility

Registered name: `BarbarianAgility`.

Runs the Barbarian Outpost agility course with stage-driven recovery, food management, banking, and obstacle progression.

| Setting | Type | Default | Operational effect |
|---|---|---:|---|
| `cheatSetup` | bool | `true` | Runs `::setstat agility` and teleports to the course starting position before the first run. **LOCAL-ENGINE-ONLY.** Disable for a normally prepared/real account. |
| `agilityLevel` | int | `60` | Agility level used by `cheatSetup`. Has no effect when `cheatSetup` is false. |
| `maxLaps` | int | `0` | Stops after this many completed laps. `0` means unlimited until stopped. |
| `food` | string | `Lobster` | Food name matched through the food helper; partial multi-bite food forms are supported. |
| `foodWithdraw` | int | `20` | Number of food items requested from the bank on each restock trip. |
| `minFood` | int | `1` | Banking is selected when the pack contains fewer than this many matching food items. |
| `eatBelowHp` | number | `50` | Eat below this HP percentage. Clamped to `10`–`90`. |
| `bankTile` | tile | `2725,3491,0` | Bank destination; accepts the shared tile formats described below. |
| `lap` | string | standard eight obstacles | Comma-separated obstacle names. Blank uses Ropeswing, Log balance, Obstacle net, Balancing ledge, Ladder, and three Crumbling walls. The entry pipe is filtered out of the lap. |

`foodWithdraw` is clamped to 1–28 and `minFood` to 0–28. Source: [barbarian.go](../internal/bot/barbarian.go), [barbarianplan](../internal/scripts/barbarianplan/plan.go).

**Validation:** the collaboration record reports an 8/8 obstacle production lap, including fall recovery and the ropeswing/pit recovery path. Long-distance post-death recovery remains dependent on available pathfinding/transport capabilities.

---

## ProgressiveFisher

Registered name: `ProgressiveFisher`.

Progresses through the fishing ladder, handles fishing/banking phases, can fund a fresh account through the existing fresh-start path, and supports the registered bot's mule hand-off controls.

| Setting | Type | Default | Operational effect |
|---|---|---:|---|
| `cheatSetup` | bool | `false` | Explicitly enables local teleport/coin setup. **LOCAL-ENGINE-ONLY.** Leave false for production. |
| `cheatLevel` | int | `0` | If `cheatSetup` is enabled and this is greater than zero, sets fishing level first. |
| `target` | string | `Big net` | Highest fishing ladder rung to pursue, resolved through `fisherplan.Rungs`. |
| `targetFish` | int | `5` | Raw-fish target for one fishing session/phase. |
| `fishTimeoutSec` | int | `180` | Maximum duration of one fishing phase before it gives up and re-evaluates. |
| `maxCycles` | int | `0` | Stops after this many complete fish+bank cycles. `0` means unlimited until stopped. |
| `fare` | int | `60` | Coin amount the fresh-start path wants for the two boat fares. |
| `pickpocketTarget` | string | `Man` | NPC name used by the fresh-start pickpocket phase to fund the fare. |
| `restBelowHp` | int | `40` | HP percentage below which pickpocketing pauses when the account has no food; resumes at 90%. Clamped to `5`–`90`. |
| `guideNpc` | string | `RuneScape Guide` | Guide NPC name used during the fresh-start dialogue/bootstrap path. |
| `skipOption` | string | `Yes please` | Dialogue option used when skipping the fresh-start/tutorial step. |
| `mule` | string | `mule1` | Mule account name used for automatic hand-offs. Blank disables threshold-triggered hand-offs. |
| `muleAt` | int | `4000` | Number of fish banked, after the target rung is reached, that triggers a mule hand-off. |
| `muleKeep` | string | `Coins` | Bank rows that stay with the fisher rather than going to the mule, in addition to ladder tools which are always retained. |
| `pickpocketTile` | tile | `3222,3218` | Where the fresh-start fund phase pickpockets (the Lumbridge castle courtyard). Accepts `"x,z[,level]"`, `{"x","z","level"}` or `[x, z, level]`. |
| `muleAtTile` | tile | `2725,3491` (Seers bank stand) | Where the hand-over trade happens. The TS original defaults to the Catherby bank; the Go port keeps the Seers stand the running fleet's MuleReceiver posts at, so a roster without this key behaves as before. |
| `eatFood` | bool | `false` | Carries food, eats it before HP is critical, and runs to the Catherby bank when critical under attack, or at the first hit in combat once out of food. With no food it rests at the bank (restocking if the bank holds food) until HP is back to 90%. Defaults off (account owner's own call, 2026-09-16) — opt in per account/fleet with `true`. |
| `food` | string | `""` (any) | Comma-separated foods to eat and withdraw, by name (`Shrimps`, `Bread`, `Trout`, `Lobster`, ...). Blank or `any` means every known cooked food. Raw fish never counts. |
| `foodCount` | int | `3` | Bites each bank trip leaves in the pack, withdrawn from the bank. Clamped `0`–`14`; every bite is a pack slot not holding fish. |
| `eatBelowHp` | number | `50` | Eat at or below this HP percentage (never below `maxHit`+1 HP). Clamped `10`–`90`. |
| `fleeBelowHp` | number | `30` | Critical at or below this HP percentage (never below `maxHit` HP): flee when under attack (with no food the account flees at the first hit anyway). Clamped `5`–`80`, never above `eatBelowHp`. |
| `maxHit` | int | `4` | The largest single hit expected. `4` is the level-38 White wolf near the fishing spots; the level-25 White wolf and the Giant bat hit `3`. Clamped `0`–`30`. |
| `fishingGuild` | bool | `true` (since `8bc05a7`) | Fish the Fishing Guild; `false` keeps the account at Catherby. When the live Fishing level is 68 or more (the entry door's own check; 67 is refused) and the rung being fished is one the Guild's spots offer (Big net, Harpoon, Lobster cage, Harpoon sharks — not Small net), the account banks at the Guild bank (stand 2586,3420) and fishes the Guild camp (2603,3416), walking between Catherby and the Guild through both Guild doors with the shared door-aware walker. Tools are still bought and fish still sold in Catherby. The hand-off threshold becomes 10,000 banked fish (spread ±30% per account like `muleAt`) unless `muleAt` is set explicitly; the notes are drawn at the Guild bank and walked to `muleAtTile`, and the donor walks back into the Guild afterwards. Three walks toward the Guild in a row with no progress fall back to Catherby for 20 minutes. A Guild-eligible account that owns its rung's tool nowhere (pack or bank) takes it from its ground spawn just inside the entrance (Big fishing net 2605,3395, Harpoon 2605,3396, Lobster pot 2608,3397) instead of buying it; if the spawn stays empty for 150 ticks it buys in Catherby as before for the next 20 minutes. Off, nothing changes. |
| `walkRub` | bool | `true` | The run staller, shared with BassOysterCaskets and ScarabSwarm's key of the same name: on a walk, while running, a carried Games necklace or Ring of dueling (any charge, never a note) is rubbed every third tick (`scarabplan.ShouldRub`: op 4 on the jewellery; the rub's `p_delay(1)` stops run energy draining for that tick, a 65% cut in the tick lab, and no charge is used). The teleport menu it opens is never answered and is closed when the walk ends. The jewellery is kept through every bank deposit and the mule hand-off, drawn from the account's own bank when the pack holds none, and asked of the mule (one, as a rider on the hand-off, only when the mule's published bank holds one). An account with none never rubs. `false` turns all of it off. Logs `run staller: N rub(s) on that walk, run energy A% to B%`. |
| `donateCoins` | bool | `false` | Answer the configured mule's restock requests while ordinarily fishing. Requires a mule name and the bus. Keeps 2,000 coins and offers only donations of at least 5,000 coins. |
| `traceDecisions` | bool | `false` | Record decision evidence for offline analysis. A recorder startup failure is logged and fishing continues. |
| `tracePath` | string | `traces/<username>.jsonl` | Trace destination when enabled; `traces/fisher.jsonl` if no username is available. Relative to the process working directory. |
| `traceExperimentId` | string | `progressive-fisher` | Experiment identifier attached to decision records. |

`mule` defaults to `mule1` when the key is missing; an explicit `""` never hands over. (The TS default is `Mulecon`; the Go default is unchanged so existing rosters keep their behaviour.) The ordinary `muleAt` threshold is spread by ±30% per account, too; the number in the table is the base, not each account's exact trigger.

Source: [fisher.go](../internal/bot/fisher.go), [guild settings](../internal/bot/fisherguild.go), [coin donations](../internal/bot/fisherdonate.go).

`rung` is **not** a current `ProgressiveFisher` adapter setting. Older documentation described it that way, but the current adapter uses the string `target` setting and derives the rung index from it.

Food survives every keep rule: the bank deposit keeps the food being carried, the Lumbridge store trip no longer sells the starter kit's Shrimps and Bread, and the mule hand-over leaves every allowed food in the bank and the pack. Catherby has no shop that sells cooked food (Harry sells tools and buys raw fish, Arhein's general store, the candle and archery shops stock none), so the food comes from the starter kit and the bank.

The hand-over load is drawn from the bank as notes. When the fisher is short of coins for the next tool it sells fish to Harry, then Arhein, drawing banked fish when the pack has none (PhaseSell). A new account turns the brightness up to very bright before its first store trip (PhaseBright).

**Validation:** the current collaboration record distinguishes the small-net/local mechanism proof from broader progression and production validation. Do not infer production parity merely from a configured `target` value.

---

## ProgressiveCrafter

Registered name: `ProgressiveCrafter`.

Runs ProgressiveCrafter.ts's whole road: appearance screen, tutorial skip, brightness, the Lumbridge store (starter kit sold, shears bought or the farm pair fetched), shearing and spinning wool to crafting 10, pickpocketing the boat fare, a pink skirt from Thessalia, the boats to Ardougne, then the Seers flax → spinning wheel → bow strings → bank loop, with bow strings handed to the mule once `muleAt` are banked.

| Setting | Type | Default | Operational effect |
|---|---|---:|---|
| `cheatSetup` | bool | `true` | `::setstat crafting` and a teleport to the flax field. **LOCAL-ENGINE-ONLY.** Disable for a normally prepared/real account (and always for a fresh account that should take the wool road). |
| `craftingLevel` | int | `10` | Crafting level used by `cheatSetup`. Has no effect when `cheatSetup` is false. |
| `pickBatch` | int | `30` | Target amount of flax for one pick step before the state machine re-evaluates. |
| `stepTimeoutSec` | int | `300` | Maximum duration for one pick or spin step before it gives up and re-evaluates. |
| `maxCycles` | int | `0` | Stops after this many bow-string bank trips. `0` means unlimited until stopped. |
| `spinManip` | bool | `true` | Spin the flax through the wheel's Spin menu in batches (one flax every 2 ticks) instead of one use-on-wheel at a time (one per 3 ticks at best, about 4 live); measured against the plain use on the account and switched off for 20 minutes when it is not clearly better (crafterplan/spinmanip.go). |
| `walkRub` | bool | `true` | The run staller: a carried Games necklace or Ring of dueling is rubbed every 3rd tick of a walk so it drains a third of the run energy; the walks also turn running on at 20% energy whatever this says (walkstall.EnsureRun). |
| `guideNpc` | string | `RuneScape Guide` | The NPC whose dialogue offers the tutorial skip. |
| `skipOption` | string | `Yes please` | Dialogue option picked to skip (contains match). |
| `fare` | int | `60` | Coins raised for the two ship fares (plus the skirt). |
| `dropAt` | int | `27` | Balls of wool in the pack that trigger a drop pass. Minimum 1. |
| `pickpocketTarget` | string | `Man` | Who pays the fare. |
| `pickpocketTile` | tile | `3222,3218` | Pickpocket spot (Lumbridge courtyard). |
| `restBelowHp` | int | `40` | With no food, pickpocketing pauses at this HP% and resumes at 90%. Clamped to 5–90. |
| `skirtCost` | int | `3` | Coins raised on top of the fare for the pink skirt. |
| `storeStand` | tile | `3209,3247` | Lumbridge general store stand. |
| `penTile` | tile | `3197,3266` | Sheep pen tile. |
| `wheelTile` | tile | `3209,3213,1` | Stand beside the Lumbridge castle spinning wheel. |
| `flaxSource` | string | `Pick it` | `Bank first, then pick` spins the flax already banked before visiting the field; every bank visit re-reads the count, and banked flax is held back from the mule while this is set. `Mule, then pick` draws the bank the same way, then (bank and pack empty of flax) walks to the mule's post, asks it for a kit of `flaxKitSize` flax as bank notes, banks the notes beside the mule and goes back to Seers to spin; a kit that brings none backs off and the loop picks meanwhile. Banked flax is held back from the mule in this mode too, and the bow string hand-over meets the mule at its post instead of the Seers stand. |
| `flaxKitSize` | int | `1000` | Flax asked for per mule kit (`Mule, then pick` only). Clamped to 28–10000; a default mule serves at most 1000 of an uncatalogued item per order (`anyMaxPerOrder`). |
| `flaxKitBackoffMinutes` | int | `15` | Wait after a mule kit that brought no flax (mule away, on another world, queue too long, per-donor window, walk failed), doubled for each fruitless kit in a row, capped at 120. A mule that says it holds none waits 120 minutes at once. Clamped to 1–120. |
| `mule` | string | `mulecon` | MuleReceiver account bow strings go to. An explicit `""` never hands over. |
| `muleAt` | int | `5000` | Banked bow strings that start a hand-over. Live-overridable from the control panel. |
| `muleKeep` | string | `Coins, Shears` | Comma-separated bank rows that stay with the crafter. |
| `logDetail` | string | `Normal` | Accepted for TS parity; the Go adapter logs the same either way. |

The adapter requires a collision pack, location index, and interface archive Components. The hand-over trades at the Seers bank stand (`mulereceiverplan.SeersStand`) as notes, or at the mule's own post (its heartbeat's, Catherby for `mule1`) with `flaxSource: "Mule, then pick"`; the control panel's manual hand-off trigger works as for ProgressiveFisher.

Flax from the mule needs the mule to list the account in `allowedPartners` and is bounded by the mule's hand-out caps (`handoutPerDonor` kits per donor per `handoutWindowMin`, `anyMaxPerOrder` per kit, `anyPerWindow` per window to everyone). In that mode an account standing within 20 tiles of the mule's post past Crafting 10 counts as the Seers loop, so a relog mid-trip walks back to Seers instead of starting the boat road. A real account needs `cheatSetup: false`.

Walking: the Lumbridge legs (store, farm shears, pen, castle wheel), the walk to Thessalia and the Seers field/wheel/bank legs go through `internal/webwalk` (doors, gates, stairs, ladders), built once per account in `OnStart`; if the walker cannot be built they fall back to the older same-level walkers. The fare pickpocketing and the boat legs still use `freshstartrunner`. Wool is spun one per use on the castle wheel rather than through the Spin make menu.

A manual `POST /api/bot/start` without a `settings` object runs with the adapter defaults, which include `cheatSetup: true`; pass the roster settings when restarting a real account.

HP handling is `restBelowHp` during pickpocketing, as in the TS original (it has no food handling).

**Validation:** the current branch records 3/3 clean direct crafter cycles and one complete registered-`Runner` cycle locally, plus separate live flax-picking proof. The long Field↔Bank walk has a separate reproducible position-tracking issue and should not be treated as fully cheat-free production proof.

---

## MuleReceiver

Registered name: `MuleReceiver`.

Receives trades from allowed partners, verifies receipts, banks items, and serves item/kit orders through the in-process bus. Optional travel groups pickup requests into camps. Source: [startup settings](../internal/bot/mule.go), [travel](../internal/bot/muletrip.go), [restocking](../internal/bot/mulerestock.go), [hand-out policy](../internal/scripts/orderplan/order.go). The early comments in `mule.go` saying travel and outbound trades do not exist are historical; the executable paths now implement them.

| Setting | Type | Default | Operational effect |
|---|---|---:|---|
| `allowedPartners` | string list | **required** | Names of partners this mule may reciprocate trades with or accept pickup requests from. Accepts a Go `[]string`, a JSON array of strings, or one comma-separated string. Empty is rejected. |
| `bank` | bool | `true` | After an accepted trade, best-effort deposit of received items in a nearby bank. This is not a full trip-home banking system. |
| `maxTrades` | int | `0` | Stops after this many accepted trades. `0` means unlimited until stopped. |
| `idleTimeoutSec` | int | `60` | Maximum time one `Loop` call waits for a trade request before yielding back to the Runner. |
| `wallBus` | bool | `true` | Enables the in-process `internal/mulebus` heartbeat/message-board path. When false, the bot ignores the bus. |
| `postTile` | tile | `2809,3441,0` | Home/trade post at Catherby. Set explicitly to match donors; fishers default their meeting point to Seers, `2725,3491,0`. Unreadable input logs a warning and uses Catherby. |
| `travelToRequests` | bool | `false` | Travel to grouped donor camps; requires bus requests. |
| `walkRadius` | int | `150` | Maximum distance eligible for walking; minimum 1. Trips needing unsupported teleports are refused. |
| `maxLegTiles` | int | `150` | Planner cap per leg; minimum 1. |
| `campRadius` | int | `25` | Grouping/arrival radius; minimum 0. |
| `campDwellSec` | int | `180` | Batch dwell limit; minimum 30 seconds. |
| `maxCampsInARow` | int | `3` | Fairness cap while another camp waits; 0 disables the cap, negatives become 0. |
| `handout` | bool | `true` | Fulfil allowed partners' bus orders. Requires `wallBus`. |
| `handoutItems` | string list | curated catalogue | Nonempty input **replaces** the catalogue, including its per-item coin/key protections. An empty list leaves the default catalogue. |
| `handoutMaxPerOrder` | int | `1` | Per-item quantity cap for a custom `handoutItems` catalogue; minimum 1. |
| `coinsPerOrder` | int | `0` (retain 500) | Positive override of the default catalogue's coin order cap. Applied before a custom catalogue replaces it. |
| `coinsPerWindow` | int | `0` (retain 25,000) | Positive override of its coin window cap. Applied before a custom catalogue replaces it. |
| `handoutAnything` | bool | `true` | Also fulfil uncatalogued items actually held in the bank, subject to the following limits. Catalogue entries take precedence. |
| `anyMaxPerOrder` | int | `1000` | Quantity limit for an uncatalogued item per order. |
| `anyPerWindow` | int | `10000` | Quantity limit for each uncatalogued item across all donors per window. |
| `anyKeepBack` | int | `0` | Bank reserve for uncatalogued items. |
| `handoutNever` | string list | no additions | Adds exclusions to the uncatalogued path's built-in Coins, Crystal key, and Half of a key exclusions. Does not override explicit catalogue entries. |
| `handoutRuneFloor` | int | `20000` | THE RUNE POOL (a user request: "stack up 20k of runes before it starts handing out 1k of each"): a gated rune is handed out only while the bank holds at least this many of it. A gate, not a reserve: at 20,000 it serves, at 19,000 it waits until the stack is rebuilt. 0 switches the rule off. A catalogue entry for the rune (`handoutItems`) is on its own terms, and the gate replaces `anyKeepBack` for a gated rune. The ScarabSwarm accounts read the same rule (`muleRuneFloor`). [Rule](../internal/scripts/orderplan/runegate.go). |
| `handoutRuneMax` | int | `1000` | The most of one gated rune a single hand-out gives (a larger order, or kit line, is cut to it). |
| `handoutRunes` | string list | `["Chaos rune","Death rune","Blood rune"]` | The gated runes, the ones the pickpockets pay; every other rune is handed out as before (the strike ladder and the alch and teleport kits run on Water, Mind, Nature and Law). An empty list gates none. |
| `handoutPerDonor` | int | `1` | Fulfilled orders per donor per window. Nonpositive values refuse orders. |
| `handoutPerItem` | int | `10` | Shared per-item window limit when the catalogue entry has no dedicated limit. |
| `handoutWindowMin` | int | `30` | Ledger window in minutes; nonpositive durations fall back to 30. |
| `cookIdle` | bool | `false` | Cook banked raw fish at Catherby during quiet periods. |
| `cookAfterIdleSec` | int | `0` | Additional quiet-time delay before cooking; pending work still interrupts it. |
| `joinKeyHalves` | bool | `true` | Join banked halves into crystal keys during idle processing. |
| `restock` | bool | `false` | Ask fleet donors for coin top-ups. Requires resolvable Coins and a usable restock floor/target. |
| `restockFloor` / `restockTarget` | int | `0` / `0` (derive from coin window) | Overrides apply only as a pair with floor > 0 and target > floor. Otherwise use the policy-derived values. |
| `restockDonorsAtOnce` | int | `3` | Concurrent restock grants; values below 1 fall back to 3. |

The default catalogue and its individual limits are defined by `DefaultCatalog` in the linked hand-out policy. `handoutAnything: false` restricts service to that catalogue. These are source defaults, not a description of the running mule's private configuration.

**Validation:** the current collaboration record reports a supervised local two-account proof of request detection, player-name resolution, reciprocal trade, both acceptance screens, and receipt of the item. The timing race on the first offer-screen snapshot was fixed with a stable-offer wait before acceptance.

---

## LOCAL-ENGINE-ONLY settings

A setting marked **LOCAL-ENGINE-ONLY** exists to make deterministic local-engine testing convenient. It is not a normal gameplay configuration. For a normally provisioned account:

- disable `cheatSetup`;
- make sure the account is legitimately at the expected starting location/level/state;
- provide the real shared primitives required by the script (`packPath`, `locsPath`, and, where required, the interface/config archive inputs).

Never copy a local-engine cheat setup into a production roster simply because it makes a smoke test easier.

## Settings and ML/LLM experiments

ML/LLM systems may recommend setting changes, but they must not silently mutate live configuration. The intended promotion path is:

```text
experiment
  ↓
evidence
  ↓
proposal
  ↓
settings/schema validation
  ↓
sandbox + regression evaluation
  ↓
review/promotion gate
  ↓
explicit fleet configuration
```

Experiments should use the same high-level action vocabulary as the real script. Packet-level shortcuts should not be introduced merely to improve an offline score.

## Maintenance

When a registered adapter changes, update this page from its source comment and keep the following synchronized:

1. the adapter's settings handling/source documentation;
2. `docs/bot_script_settings.md`;
3. `fleet.config.example.json` where an example setting is shown;
4. any parity/status documentation that makes a validation claim.

Do not document a setting merely because it existed in an older TS or Go port: the current registered adapter is the source of truth for what `fleet.config.json` can actually configure.
