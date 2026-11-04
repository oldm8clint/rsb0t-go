# Additional bot script settings

Checked against the registered adapters on 2026-09-24. Use these keys inside an account's `settings` object. The [main settings reference](bot_script_settings.md) covers BarbarianAgility, ProgressiveFisher, ProgressiveCrafter and MuleReceiver. Together these pages name all 25 current registrations; ScarabSwarm's growing stage-specific surface is separately indexed below.

Defaults describe source behavior, not the private running roster. Implementation does not establish production proof. A `tile` normally accepts `"x,z[,level]"`, an array `[x,z,level]`, or an object `{"x":2725,"z":3491,"level":0}`. MeleeFlincher's `safeTile` is specifically a string. Use JSON numbers and booleans for adapters using the basic setting helpers; schema-backed adapters additionally accept string conversions.

Unless a section says otherwise, `logDetail` is a string defaulting to `Normal`; `Verbose` enables detailed progress logging. Only scripts whose tables list it read this key.

## Alcher

Source: [alcher.go](../internal/bot/alcher.go), [alchplan](../internal/scripts/alchplan).

| Key | Type / default | Meaning |
|---|---|---|
| `spell` | string, `Best for level` | Automatic choice, `High Level Alchemy`, or `Low Level Alchemy`. |
| `items` | string, `Rune platebody` | Comma-separated item names, processed in order. |
| `castsPerTrip` | int, 100 | Notes and runes drawn per bank trip. |
| `useFireStaff` | bool, true | Fetch and wear a banked fire staff when available. |
| `logDetail` | string, `Normal` | Logging detail. |

## BankFletcher

Source: [bankfletcher.go](../internal/bot/bankfletcher.go). This port cuts logs into arrow shafts; no configurable stringing/attachment mode is read.

| Key | Type / default | Meaning |
|---|---|---|
| `material` | string, `Logs` | Banked log material to cut. Plain Logs is the adapter's documented proven material. |
| `logDetail` | string, `Normal` | Logging detail. |

## BoneBurier

Source: [boneburier.go](../internal/bot/boneburier.go), [boneplan](../internal/scripts/boneplan).

| Key | Type / default | Meaning |
|---|---|---|
| `source` | string, `ground` | `ground` or `bank`. |
| `names` | string, `bones` | Comma-separated name fragments to collect. |
| `bankNames` | string, `big bones,dragon bones` | Bone names to bank in split mode. |
| `disposal` | string, `bury` | `bury`, `bank`, or `split`. |
| `radius` | int, 15 | Search radius; values below 1 reset to 15, values above 50 clamp to 50. The older adapter comment's zero-means-scene claim is not the normalized behavior. |
| `buryWhenFull` | bool, false | Gather a full pack before burying. |
| `idleMins` | int, 0 | Stop after an idle interval and final bank trip; 0 waits indefinitely. |
| `goalLevel` | int, 0 | Announce reaching this Prayer level; 0 disables the goal announcement. |
| `logDetail` | string, `Normal` | Logging detail. |

## CasketOpener

Source: [casketopener.go](../internal/bot/casketopener.go). The only setting is `logDetail` (string, `Normal`; use `Verbose` for bank/withdraw messages). Processes pack and bank caskets, then stops. There is no mule setting.

## EssenceMiner

Source: [essence.go](../internal/bot/essence.go), [essence settings](../internal/scripts/essenceplan/settings.go).

| Key | Type / default | Meaning |
|---|---|---|
| `pickaxe` | string, `Best available` | Best usable owned/banked pickaxe, or a specific tier such as `Rune`. |
| `mule` | string, empty | Recipient; empty disables hand-offs (at a supply-loop gateway every one: threshold, cap and a control-panel request). |
| `muleAt` | int, 10000 (5000 at a gateway other than `aubury`, when not set) | Banked essence threshold; 0 never hands over. |
| `muleKeep` | string, empty | Comma-separated bank rows to retain; pickaxes are always retained. |
| `logDetail` | string, `Normal` | Logging detail. |
| `gateway` | string, `aubury` | `aubury` (the original loop, Varrock east bank), `guild` (Wizard Distentor in the Wizards' Guild, Magic 66, the Yanille bank) or `cromperty` (East Ardougne). The last two run the **supply loop** ([essence_supply.go](../internal/bot/essence_supply.go)): stretches of `batch` essence, one note stack handed to the mule at `muleAt`, mining held while the mule's published Rune essence has reached `muleStopAt`. |
| `muleStopAt` | int, 150000 | Mining stops while the mule's published Rune essence has reached it (0 never; at most 1,000,000). |
| `muleItems` | string, `all` at aubury and `essence` elsewhere | What a hand-over carries: the whole bank but the pickaxes (`all`), or one note stack of Rune essence (`essence`). `all` at another gateway is an error. |
| `muleAtTile` | `x,z[,level]`, empty | Where the mule is met; empty is the post its heartbeat publishes (supply loop) or the Varrock east bank (original loop). |
| `batch` | int, 270 | Essence one supply-loop mining stretch brings (27 to 100000, whole loads). |
| `buyPickaxe` | bool, false | The supply loop fetches a better pickaxe from Nurmof's shop in the Dwarven Mine when the Mining level and the coins allow (not proven live). Skipped while the mining batch (`mineTickManip`) has its kit: with it every pickaxe rolls every 3 ticks. |
| `mineTickManip` | bool, false | The supply loop's mining stretches write a knife-on-log batch in the stale tick after every roll, so the rock rolls every 3 ticks instead of every pickaxe-rate-plus-one (bronze 8, iron 7, steel 6, mithril 5, adamant 4; a rune pickaxe's 3 is the floor and never runs it). Needs a Knife and a plain Log in the pack or the Yanille bank (the mule is asked once per half hour when neither holds them); a load is then 25 essence. Read from content 289 and modelled tick by tick ([minemanipplan](../internal/scripts/minemanipplan/plan.go), [minemanip.go](../internal/scripts/essencerunner/minemanip.go)), not yet run on the live server. Log lines: `essence: mine tick manipulation is ON`, `mine manip:`. |

Loading is **strict**: a key that is not listed (`homeWorld` and `pinWorld`, the fleet's own, apart), a value of the wrong type or one out of range stops the start with the list of problems. The full story, the numbers and the log lines: [runecraft_p2_2026-10-13.md](runecraft_p2_2026-10-13.md).

## RuneCarrier

Source: [runecarrier.go](../internal/bot/runecarrier.go), [runecarrier_shift.go](../internal/bot/runecarrier_shift.go), [settings](../internal/scripts/runecraftplan/carriersettings.go). Runecraft P3 ([runecraft_p3_2026-10-14.md](runecraft_p3_2026-10-14.md)): a ScarabSwarm account that, while MasterCrafter's Nature gate is open, carries unnoted Rune essence from Jiminua's Jungle Store on Karamja to him at the Nature ruins. Loading is **strict**: a key that is not listed (`homeWorld` and `pinWorld`, the fleet's own, apart), a value of the wrong type or out of range, `walkAlch` true and `carrierSource` `bank` stop the start with the list.

| Setting | Default | Meaning |
|---|---|---|
| `runecraftCrafter` | empty | MasterCrafter's login name (`master`). Blank never starts a shift. |
| `mule` | `mule1` | The MuleReceiver the kit comes from and the leftovers go back to. |
| `carrierSource` | `shelf` | `shelf` (notes un-noted at the Jungle Store); `bank` is not built. |
| `carrierKitEssence` | 3000 | Noted essence asked per shift, whole loads of 25 (100 to 20,000). |
| `carrierKitCoins` | 10000 | Coins asked per shift (500 to 1,000,000). |
| `carrierMinHP` | 25 | Least maximum hitpoints (10 to 99). |
| `carrierShelfFloor` | 40 | Essence left on the shelf after each visit (0 to 99). |
| `walkRub` | true | Rub a Games necklace or Ring of dueling every third tick of a run. |
| `walkAlch` | false | Refused when true. |
| `swarmSettings` | `{}` | ScarabSwarm's settings between shifts (object, or a string holding one). |

## FlaxPicker

Source: [flaxpicker.go](../internal/bot/flaxpicker.go).

| Key | Type / default | Meaning |
|---|---|---|
| `flaxName` | string, `Flax` | Resource location/item name. |
| `pickOp` | string, `Pick` | Location action name. |
| `bankBooth` | string, `Bank booth` | Bank location name. |
| `logDetail` | string, `Normal` | Logging detail. |

## GemCutter

Source: [GemCutterSettings](../internal/bot/gemcutter.go). Uses schema resolution.

| Key | Type / default | Meaning |
|---|---|---|
| `gems` | string list, empty | Empty selects all level-eligible gems; otherwise restrict to names in [GemNames](../internal/scripts/gemplan). Accepts an array or comma-separated string. |
| `bankBooth` | string, `Bank booth` | Bank location name. |
| `membersWorld` | bool, true | Whether to include members-only gems; does not change worlds. |
| `logDetail` | string, `Normal` | `Normal` or `Verbose`. |

## HerbCleaner

Source: [HerbCleanerSettings](../internal/bot/herbcleaner.go). Uses schema resolution.

| Key | Type / default | Meaning |
|---|---|---|
| `herbs` | string list, empty | Empty selects all level-eligible herbs; otherwise restrict to names in [HerbNames](../internal/scripts/herbplan). Accepts an array or comma-separated string. |
| `bankBooth` | string, `Bank booth` | Bank location name. |
| `membersWorld` | bool, true | False excludes herbs because the content requires a members world. Does not change worlds. |
| `logDetail` | string, `Normal` | `Normal` or `Verbose`. |

## VialFiller

Source: [vialfiller.go](../internal/bot/vialfiller.go).

| Key | Type / default | Meaning |
|---|---|---|
| `bank` | string, `Falador West` | `Falador West` or `Falador East`. |
| `buyVials` | bool, false | Enable Jatix restocking. |
| `buyEveryRuns` | int, 5 | Trips between restocks. |
| `buyQty` | int, 27 | Vials bought per restock. |
| `coinsPerTrip` | int, 1000 | Coins drawn for a restock trip. |
| `logDetail` | string, `Normal` | Logging detail. |

## SmelterBot

Source: [SmelterBotSettings](../internal/bot/smelterbot.go), [recipes](../internal/scripts/smeltplan).

| Key | Type / default | Meaning |
|---|---|---|
| `bar` | string, `Bronze bar` | Recipe name; recognized short names such as `steel` also work. Unknown names are reported and fall back to the default. |
| `bankBooth` | string, `Bank booth` | Bank location name. |
| `logDetail` | string, `Normal` | `Normal` or `Verbose`. |

The adapter does not read TS furnace/bank position settings. Recipe selection determines the ore withdrawal plan.

## ShopBuyout

Source: [ShopBuyoutSettings](../internal/bot/shopbuyout.go), [shop presets](../internal/scripts/shopplan/plan.go), [defaults](../internal/scripts/shopplan/settings.go).

| Key | Type / default | Meaning |
|---|---|---|
| `shop` | string, `Aubury's runes — Varrock (Varrock East bank)` | Preset supplying keeper, stand and bank; the other preset is `Lowe's arrows and bows — Varrock (Varrock East bank)`. |
| `buyItems` | string list, empty | Empty buys all stock; otherwise restrict to stocked item names. |
| `budgetGp` | number, 250000 | Session spending budget; minimum 100. |
| `perTripGp` | number, 100000 | Coins drawn per trip; minimum 100. |
| `stopFloorGp` | number, 5000 | Stop below this bank coin balance; minimum 0. |
| `recheckSeconds` | number, 60 | Restock wait, clamped to 5–600 seconds. |
| `bankBooth` | string, `Bank booth` | Bank location name. |
| `logDetail` | string, `Normal` | `Normal` or `Verbose`. |

## MeleeFlincher

Source: [meleeflincher.go](../internal/bot/meleeflincher.go).

| Key | Type / default | Meaning |
|---|---|---|
| `target` | string, empty | NPC name; empty chooses a nearby attackable NPC. |
| `safeTile` | string, empty | `x,z` or `x,z,level`; empty or unreadable input uses the starting tile. Choose a tile the target cannot reach. |

## AdvancedFighter

Source: [readAdvancedFighterSettings](../internal/bot/advancedfighter.go). This is the fixed-camp melee adapter; it is distinct from CombatTrainer's progression ladder.

| Key | Type / default | Meaning |
|---|---|---|
| `targets` | string, empty | Comma-separated NPC names; empty allows attackable NPCs. |
| `anchorTile` | tile, starting tile | Combat camp. |
| `leashRadius` | int, 10 | Target/loot radius, clamped to 2–40. |
| `attackOthersTargets` | bool, false | Allow targets fighting other players. |
| `preferAttackers` | bool, true | Prefer NPCs attacking this account. |
| `meleeStyle` | string, `strength` | `attack`, `strength`, `controlled`, `defence`, or `progressive`. |
| `autoRetaliate` | bool, true | Auto-retaliation preference. |
| `food` | string, empty | Food name; empty uses available edible food. |
| `foodWithdraw` | int, 10 | Bank withdrawal count, clamped to 0–27. |
| `eatMode` | string, `smart` | `smart` or `threshold`. |
| `eatAtHp` | number, 50 | Threshold HP percentage, clamped to 10–90. |
| `buryBones` | bool, false | Bury collected bones. |
| `loot` | string, empty | Comma-separated name fragments to collect. |
| `keep` | string, empty | Additional items to retain during deposit. |
| `autoBank` | bool, true | Enable bank runs. |
| `bankAtLootSlots` | int, 12 | Loot-slot bank threshold, clamped to 1–27. |
| `bankEveryMinutes` | number, 0 | Periodic bank interval; 0 disables it. |
| `panicHp` | number, 25 | Panic HP percentage, clamped to 0–90. |
| `fleeFromPlayers` | bool, false | Flee toward a bank on player attack. |
| `maxDeaths` | int, 0 | Death limit; 0 means no limit. |
| `stopAtLevel` | string, empty | Goal such as `attack:70`; empty disables it. |
| `stopAfterKills` | int, 0 | Kill goal; 0 disables it. |
| `stopAfterMinutes` | number, 0 | Time goal; 0 disables it. |
| `verbose` | bool, false | Detailed logging (this script does not read `logDetail`). |
| `mule` | string, empty | Loot recipient. |
| `muleAt` | int, 0 | Banked-loot hand-off threshold; default disables automatic hand-offs. |
| `muleKeep` | string, empty | Additional bank rows retained during hand-offs. |
| `muleAtTile` | tile, `2725,3491,0` | Mule meeting point. |

## CombatTrainer

Source: [CombatTrainerSettings](../internal/bot/combattrainer.go). Uses schema resolution. Ranged and Magic follow the melee goals; setting those two goals to 0 disables their training.

| Key | Type / default | Meaning |
|---|---|---|
| `goalAttack`, `goalStrength`, `goalDefence` | number, 75 each | Goals clamped to 1–75. |
| `goalRanged`, `goalMagic` | number, 75 each | Goals clamped to 0–75. |
| `goalPrayer` | number, 43 | Goal clamped to 1–99. |
| `magicElement` | string, `Fire` | `Fire`, `Wind`, `Water`, or `Earth`. |
| `membersWorld` | bool, true | Include members rungs; does not change worlds. |
| `food` | string, empty | Use available food, or choose an accepted food name. |
| `eatAtHp` | number, 50 | HP percentage, clamped to 10–90. |
| `buyGear` | bool, true | Buy reachable shop gear. |
| `gearTripBudget` | bool, true | Combine gear shopping into a budgeted journey per band. |
| `fightWithoutFood` | bool, false | Continue when the food supply cannot support the run. Default stops instead. |
| `rung` | string, `auto` | Automatic choice, or `cows`, `alkharid`, `giants`, `mossgiants`, `icegiants`, `ogres`, `firegiants`. |
| `logDetail` | string, `Normal` | `Normal` or `Verbose`. |

## ProgressiveThiever

Source: [thieverSettings](../internal/bot/thiever.go), [thieverplan](../internal/scripts/thieverplan).

| Key | Type / default | Meaning |
|---|---|---|
| `schedule` | string, `1: Man, 25: Warrior woman, 40: Guard, 55: Knight of Ardougne, 70: Paladin, 80: Hero` | Level-to-method schedule. |
| `suicide` | bool, true | Permit the initial Lumbridge no-food thieving/death cycle. |
| `food` | string, `Lobster` | Food to carry/eat. |
| `foodWithdraw` | int, 20 | Bank restock count. |
| `minFood` | int, 1 | Minimum food before restocking. |
| `eatBelowHp` | int, 50 | Eating threshold in percent. |
| `bankAt` | int, 20 | Loot-slot bank threshold. |
| `fare` | int, 60 | Boat-fare target. |
| `restBelowHp` | int, 40 | No-food funding rest threshold in percent. |
| `guideNpc` | string, `RuneScape Guide` | Tutorial guide. |
| `skipOption` | string, `Yes please` | Tutorial skip response. |
| `mule` | string, empty | Empty disables hand-offs and food requests. |
| `askMuleForFood` | bool, true | Request food from the configured mule. |
| `muleAt` | int, 20000 | Bank-row threshold, not a coin count. |
| `muleKeep` | string, `Coins` | Bank rows retained during hand-offs. |
| `logDetail` | string, `Normal` | Logging detail. |

The meeting point follows the current method's bank; this adapter does not read `muleAtTile`, `pickpocketTarget`, or `pickpocketTile`.

## ProgressiveWoodcutter

Source: [woodcutterSettings](../internal/bot/woodcutter.go), [defaults and normalization](../internal/scripts/woodcutplan/plan.go).

| Key | Type / default | Meaning |
|---|---|---|
| `target` | string, `Yew` | Highest tree tier to pursue. |
| `fare` | int, 60 | Boat-fare target. |
| `pickpocketTarget` | string, `Man` | Funding NPC. |
| `pickpocketTile` | tile, `3222,3218,0` | Funding stand. |
| `restBelowHp` | int, 40 | Funding rest threshold in percent. |
| `guideNpc` | string, `RuneScape Guide` | Tutorial guide. |
| `skipOption` | string, `Yes please` | Tutorial skip response. |
| `mule` | string, `Mulecon` | Recipient; explicit empty string disables hand-offs. |
| `muleAt` | int, 1000 | Target-species logs held in bank plus pack before a hand-off. |
| `muleKeep` | string, `Coins` | Retained bank rows; axes are retained automatically. |
| `muleAtTile` | tile, `2725,3491,0` | Mule meeting point. |
| `startCamp` | bool, true | Use the Draynor Manor start camp below the oak gate. |
| `axeWanted` | string, `Rune axe` | Desired purchased axe. |
| `axePrice` | int, 0 | Expected price; 0 disables buying. |
| `axeBudget` | int, 0 | Spending ceiling; 0 disables buying. Both price and budget must be set. |
| `marketStand` | tile, `2725,3493,0` | Market-character location at Seers bank. |
| `marketSeller` | string, empty | Restrict market trades to this seller when supplied. |
| `bobStand` | tile, `3232,3203,0` | Bob's Axes fallback; distinct from the Lumbridge general store. See [BobsAxes](../internal/scripts/woodcutplan/axe.go). |
| `logDetail` | string, `Normal` | Logging detail. |

## ProgressiveWCFletch

Source: [wcfletchSettings](../internal/bot/wcfletch.go). Reads all ProgressiveWoodcutter settings above, plus:

| Key | Type / default | Meaning |
|---|---|---|
| `fletch` | bool, true | Enable the fletching extension; false delegates to woodcutting. |
| `normalLogs` | string, `shafts` | `shafts` or `bows` for plain logs. |
| `treeGate` | string, `startAt` | Raised progression gates, or `content` for server minimum levels. |
| `muleAtFletched` | int, 1000 | Fletched units (bows plus shafts) held before a hand-off; distinct from the log threshold. |
| `spreadRungOne` | bool, true | Spread the first rung across available normal-tree patches. |
| `spreadMaple` | bool, true | Seat maple-rung accounts over the safe maple camps round Seers (and the willow overflow) by the supply model, instead of all at the one four-tree Seers maple camp. See [maplecamps.go](../internal/scripts/wcfletchplan/maplecamps.go). |
| `mapleOverflow` | bool, true | With `spreadMaple`, let a maple-rung account chop willows (cut into willow longbows) while the maple camps are crowded. `false` keeps it on maples. |
| `chopManip` | bool, true | Write a knife-on-log batch (knife on a log, Make 1, close, tree click, in one socket write) that the engine reads in the stale tick after each swing, so a tree rolls every 3 ticks instead of 4: +25% to +32% logs in the lab (see [the lab report](tick_manipulation_lab_2026-10-06.md) and [the Phase B page](wcfletch_chop_manip_2026-10-06.md)). Needs a knife and a log the Make 1 button can use in the pack (a plain log, or one whose shortbow the Fletching level allows). The controller measures how many ticks late its batches reach the engine and writes them that many ticks before the swing they are aimed at; it never pauses or switches itself off. Logs logs per 100 ticks with and without batches every 600 chopping ticks, and says when the aim changes. `false` is the plain one-click chop. ProgressiveWoodcutter never does it. |
| `spreadYew` | bool, true | Seat yew-rung accounts over the church yews, the Catherby yews and the lone road yew by the supply model instead of all at the church camp's three yews, counting the players an account sees at its camp who are not this cohort's (strangers, and other scripts' accounts) as extra occupants. Yews never overflow to another rung. When every camp on the world has been crowded for ten minutes the account logs which world would pay better (`spreadYewWorlds` is what moves it). See [yewcamps.go](../internal/scripts/wcfletchplan/yewcamps.go) and [campcrowd.go](../internal/scripts/wcfletchplan/campcrowd.go). |
| `spreadYewWorlds` | bool, true | Deal the yew-rung accounts over the worlds in `yewWorlds` (least loaded first, the world an account is on winning a tie, strangers at a world's yews counted as accounts) so each world's eleven yews feed a third of the cohort instead of one world's feeding all 23. The assigned world becomes the account's home: the script hops there through the `homeWorld` machinery (paced, waiting for a quiet login path up to 20 minutes), a hand-off to the mule on world 1 is a hop there and a hop back (the script marks a trip so the restart on world 1 does not go straight home; every account has the trip, dealt or not, so a magic account on world 2 or 3 by its `homeWorld` hands off too), and an account that loses 15% of a quarter hour to waiting for a yew moves to a lighter world, at most once an hour. `false` keeps every account on the world `homeWorld` puts it on. Needs `spreadYew`. Log lines: `wcfletch: yew world spread:`, `wcfletch: idle over the last 15 min`, `wcfletch: cohort idle over the last`. See [yewworlds.go](../internal/scripts/wcfletchplan/yewworlds.go). |
| `yewWorlds` | string, `"1,2,3"` | The worlds `spreadYewWorlds` deals the yew cohort over. Fewer than two spreads nothing; a world the deployment does not list is refused by the fleet and the account stays where it is. |
| `spreadMagic` | bool, true | Seat magic-rung accounts over the two safe magic camps the Seers bank can feed, by the supply model: the ladder's three trees west of the bank (74 steps, capacity 3) and the four in the hedged garden behind the Sorcerers' Tower (114 steps, capacity 5), both banking at Seers, the nearest safe bank to each. A world's eight magic accounts are seated four at each (about 21.6k xp/h each in the model, against 14.1k with all eight at the three trees); strangers at a camp are counted as the yew board counts them, and every world has its own board, so each account stays on the world `homeWorld` puts it on. A seat never changes what the account chops, cuts or banks. `false` puts every magic account at the ladder's three trees, as before 13 October 2026. Log line: `wcfletch: magic camp <name> (<trees> trees, bank <bank> <steps> steps): seated N of capacity M on world W`. See [magiccamps.go](../internal/scripts/wcfletchplan/magiccamps.go). |
| `walkRub` | bool, true | The run staller on the account's walks: a carried Games necklace or Ring of dueling is rubbed every 3rd tick of a run (no charge used, a third of the run energy drained); kept through deposits and hand-offs, drawn from the account's own bank, asked of the mule on a hand-off. See COLLAB.md, "Skilling tick manipulation (skillmanip-1007)". |
| `walkFletch` | bool, true | Cut the pack of logs on the walk to the Seers bank (a knife-on-logs Make 10 re-issued after every walk click, 2 ticks a log) instead of standing at the trees first. Needs `fletch` and a knife; switches itself off for 30 minutes after 3 walks that cut nothing. |

## RetaliateChopper

Source: [retaliatechopper.go](../internal/bot/retaliatechopper.go), [retaliateplan](../internal/scripts/retaliateplan), [retaliaterunner](../internal/scripts/retaliaterunner). Design and every engine fact: [two_tick_retaliate_research_2026-11-04.md](two_tick_retaliate_research_2026-11-04.md). The two-tick oak chopper: a shortbow worn with no ammo, auto-retaliate on, the Rapid style, and the two Falador farm rats hitting the account exactly 2 ticks apart; a Clear then a tree Click written on alternate ticks make the oak roll every 2 ticks. One account at a time (the site has two rats). Needs, in the pack or worn: a Shortbow the account can wield, an axe in the pack, and runes for a stat-lowering spell (Confuse is 1 body, 3 water, 2 earth). Never assign it to an account that also has `chopManip` set (it refuses to start).

| Key | Type / default | Meaning |
|---|---|---|
| `site` | string, `falador-oaks` | The site table. Only `falador-oaks` exists. |
| `stand` | string, empty | Stand tile `"x,z"`. Empty picks the best tile the collision pack and the loc reach model allow (two oaks, multi-combat, inside both rats' leash, two open neighbours; the documented first choice is 3003,3312). |
| `clear` | string, `drop` | What clears the interaction each hit: `drop` drops a log (a power-chop; the pack does not fill; presses the button instead when no log is free to aim at) or `button` presses Auto retaliate: On (keeps the logs). |
| `full` | string, `drop` | A full pack: `drop` drops the logs in a burst, `bank` banks them at the Falador east bank and sets the rats up again. |
| `aggro` | string, `confuse,weaken,curse` | Comma-separated spells that wake the rats, the first one the Magic level and runes allow wins. Wind Strike can kill a rat, so it is used only when listed. With no affordable spell the account chops the ordinary way for `fallbackMins` and tries again. |
| `phaseTries` | int, 6 | Re-phase tries (step away and back, or wake a missing rat) before the fallback chop. 1 to 20. |
| `minGain` | float, 0.7 | Share of roll gaps that must be 2 ticks for the loop to keep going. |
| `lead` | int, 1 | Starting write lead in ticks, 0 to 2. The controller re-aims it from the rolls it sees. |
| `stopHPPercent` | int, 70 | Stop, with auto-retaliate off, below this share of maximum hitpoints. The rats hit for 0, so real damage means another monster. |
| `fallback` | string, `knife` | `knife` (the 3-tick knife-on-log chop, plain if no knife or log) or `plain` chop while the rats cannot be phased. |
| `fallbackMins` | int, 10 | Minutes of fallback before the two-tick setup is tried again. |
| `reportEvery` | int, 300 | Ticks between rolling log lines. Minimum 50. |
| `logDetail` | string, `Normal` | `Verbose` logs every setup decision. |

Log lines to read on a first run (all start `retaliate:`): `stand tile`, `aggro:` (the two casts and the ticks between them), `looping at`, the rolling `ticks per roll` line (with `hits ... 2 ticks apart`, `locked`, `lead`, `logs/h`, `xp/h`), the `stats:` line (roll gaps 2/3/4/5-7/8+, aimed clicks that rolled on time, refused attacks), `re-phase`, `FALLBACK`, `STOPPED`.

## BassOysterCaskets

Source: [bassoyster.go](../internal/bot/bassoyster.go), the drop cycle's I/O in [fisherrunner/cycle.go](../internal/scripts/fisherrunner/cycle.go) and its rules in [fisherplan/cycle.go](../internal/scripts/fisherplan/cycle.go). Registered name: `BassOysterCaskets`. It is ProgressiveFisher with the Big net pinned and the Fishing Guild on: it reads every [ProgressiveFisher setting](bot_script_settings.md#progressivefisher) (`target`, `autoSharks` and `fishingGuild` are ignored: the rung is always the Big net and the Guild always on), plus these. Every one defaults **on**.

| Key | Type / default | Meaning |
|---|---|---|
| `openCaskets` | bool, true | Open caskets in the drop cycle instead of banking them (coins stack and free a slot; gems, a cosmic talisman and a key half stay), and join the two key halves into a crystal key as soon as the pack holds one of each. `false` banks caskets as caught, to be opened at the mule hand-off. |
| `openOysters` | bool, true | Open oysters in the drop cycle and drop the Empty oyster they leave (it is junk); the Oyster pearl stays. `false` banks oysters shut. |
| `cutPearls` | bool, true | Cut Oyster pearls into Pearl bolttips in the drop cycle, so the bank trip banks tips (one stack) instead of one slot a pearl. Needs a chisel in the pack and Fletching 41 (content's `[label,make_bolt_tips]`); below that, or with no chisel, a pearl is kept as a pearl. |
| `cycleOnWalks` | bool, true | Also run the drop cycle in the middle of a walk that is long enough to hold it (between fishing spots, to the bank, back): an OPHELD clears the fishing action but leaves the walk queue alone, so a cycle during a walk costs no fishing time. `false` runs it only when fishing is interrupted anyway. |
| `fletchTo41` | bool, true | Train Fletching to 41 from the mule's logs so pearls can be cut (needs `cutPearls`). On the mule hand-off the account asks, in one kit order and only for what the mule's published bank holds and the account's own bank and pack lack, for a knife, the logs of the climb as bank notes (plain Logs, Oak logs, Willow logs, sized from its Fletching experience with 3% and 10 over, at most half the mule's stock of a kind and 1,000 an order) and a Chisel; the logs, knife and chisel are never handed back to the mule. Then, as a Loop step while it is fishing, one training round at its bank per Loop call: bank everything but the tool and the knife, draw a pack of the best logs its level cuts (`fisherplan.ChooseFletch`: plain logs only ever into arrow shafts, oak and willow into the best unstrung bow the level allows: oak shortbow at 20, oak longbow at 25, willow shortbow at 35, willow longbow at 40), close the bank and cut it with the make menu. About 1,945 logs (607 plain, 794 oak, 544 willow) from Fletching 1, a bit over an hour once. A round that makes nothing backs off (30 s, 60 s) and the third in a row switches the training off for the run. The products go to the bank and on to the mule with the next hand-off. Logs `fletching training: round N cut ... into ...`, `fletching training done`, `hand-off kit: asking ...`. |

**The chisel** is carried only from Fletching 41 (`cutPearls`): kept through every bank deposit and every hand-off from then on, drawn from the account's own bank when the pack has none, asked of the mule in the kit when its published bank holds one, and otherwise bought once from Arhein's store at Catherby (the mule's post is 11 tiles from it) for a coin or two. Logs `pearl tips: bought a chisel from Arhein's store`.

BassOysterCaskets also takes `walkRub` (bool, default **true**) from the [ProgressiveFisher table](bot_script_settings.md#progressivefisher): the run staller rubs a carried Games necklace or Ring of dueling on every walk, so the long walks to the bank and back to the spot drain less run energy.

**The drop cycle** runs when the pack has two or fewer free slots and holds something for it (a casket, an oyster, a joinable key pair, a cuttable pearl, junk), when a walk is long enough, and at the bank before the booth is clicked. One cycle is two bursts of at most five clicks a game tick (the engine's user-event limit): the opens and the junk already held, then, from the pack as the opens left it, the joins, the cuts and the new junk (the Empty oysters). It drops Raw mackerel, Raw cod, Leather boots, Leather gloves, Seaweed and Empty oyster; a bank note is never touched. After a cycle that interrupted fishing the spot is clicked again. What it did is one line, `drop cycle (fishing|on a walk|at the bank, before banking|between fishing holds): opened N casket(s), opened M oyster(s) (P pearl(s), E empty shell(s) dropped), joined K key half pair(s) into crystal key(s), cut Q oyster pearl(s) into T Pearl bolttips, dropped J junk slot(s) (R raw fish) - C click(s) in T tick(s), F free slot(s) now`, and the stop line adds `drop cycle totals`.

**Loot panel:** the casket and oyster cards record what each open gave that the account keeps (the oyster card the pearls, the casket card coins, gems, talismans and key halves), never the Empty oysters or other junk it drops; the cut tips go on the Fletching card.

## LootChestFisher

Source: [lootchestfisher.go](../internal/bot/lootchestfisher.go). Reads the [ProgressiveFisher settings](bot_script_settings.md#progressivefisher) through its real fallback bot, plus `minKeys` (int, default **6**): minimum key stock for a chest outing; values below 1 reset to 6. An explicit empty `mule` fails startup. The shared `muleAtTile` defaults to `2725,3491,0`. Its mule must allow Crystal key hand-outs; a client setting does not override the mule's own catalogue or ledger limits.

## QuestLadder

Source: [questladder.go](../internal/bot/questladder.go). Current `ProvenQuests` admits only Waterfall by default; a factory existing in the source does not establish its lab acceptance.

| Key | Type / default | Meaning |
|---|---|---|
| `quests` | string list, `["waterfall"]` | Quest IDs in order; empty also selects Waterfall. Unknown IDs fail startup. |
| `allowUnproven` | bool, false | Allow registered quests outside `ProvenQuests`; intended for their lab validation. |
| `mule` | string, empty | Optional provisioner; empty uses the other acquisition methods. |
| `mulePost` | tile, `2655,3286,0` | Mule stand at East Ardougne south bank. |
| `storeDir` | string, `data/scarabswarm-checkpoints` | Root for `<account>/ScarabSwarmQuests.json`. |
| `perQuestMinutes` | int, 90 | Per-quest execution budget. |
| `budgetMinutes` | int, 360 | Whole ladder-pass budget. |
| `waterfallMaxDeaths` | int, 2 | Death threshold before parking Waterfall. |
| `finishWhenDone` | bool, true | Request script completion after finishing the ladder. |
| `provisionDisabled` | bool, false | **LOCAL LAB CONTROL ONLY:** deliberately fail provisioning. |

## RuneRunner

Source: [RuneRunnerSettings](../internal/bot/runerunner.go), [defaults and validation](../internal/scripts/runerunnerplan/settings.go). A dedicated rune buyer (not the mule) that buys the listed runes under their price caps from Betty, Aubury and Lundail on the mule's world and one other (optionally Staffs of air from Zaff), and trades them to the MuleReceiver. Design, rollout gates and what is not built: [RuneRunner handoff](runerunner_handoff_2026-09-26.md). **Not lab-proven yet.**

**Rotation (3 October 2026, `rotate`):** instead of the classic ring (the mule's world plus one other), a rotating runner works a circuit over every listed shop on every listed world (default worlds 1, 2, 3), buying every rune the shops stock (default) under caps of three times each rune's base price (default), hopping in place at each shop and skipping any (shop, world) pair whose restock estimate is not worth the hop or the walk. Runners on different `rotationPhase`s take different directions and world orders, and a hop holds its pair on the shared shop board so a second runner moves on. With `swarmWhileWaiting`, a circuit whose successor would buy fewer than `swarmResumeRunes` hands everything to the mule and runs ScarabSwarm (on `swarmSettings`) until the estimate recovers, then stops it at a safe point. Design: [rotation.go](../internal/scripts/runerunnerplan/rotation.go), [swarm.go](../internal/scripts/runerunnerplan/swarm.go). `rotate` `auto` keeps a runner whose `shops` names one shop on the classic ring.

Loading is strict: an unknown key, `pinWorld` ("RuneRunner hops worlds; remove pinWorld"), a value of the wrong type, and any number or option outside the ranges below fail startup rather than being clamped. The mule must list this account in its `allowedPartners`.

| Key | Type / default | Meaning |
|---|---|---|
| `mule` | string, `mule1` | The MuleReceiver the runes are traded to. Must not be empty. |
| `muleStand` | string `"x,z"`, empty | The mule's post; empty uses the post its heartbeat publishes, else `2809,3441` (Catherby bank). Level 0 only. |
| `shops` | string list, `["Aubury","Betty","Lundail"]` | Sites to buy at, no duplicates. `WizardsGuild` is refused (Magic 66; route not built). |
| `worlds` | int list, empty | Worlds to buy on, each at least 1, no duplicates. Rotating: the circuit's worlds, at most 6; empty is 1, 2, 3. Classic ring: the mule's world is always one and the first listed world that is not the mule's is the other; empty is the mule's world and the fleet's other (2 beside 1); at most two. |
| `runes` | string list, empty | Priority order; each must be sold by an enabled shop. Empty: rotating, every rune the enabled shops stock (Mind, Air, Water, Earth, Fire, Chaos, Body, Death, then Lundail's Nature, Law, Cosmic); classic ring, `["Mind rune","Air rune","Chaos rune"]`. |
| `priceCaps` | string list, empty | `Rune=gp`, exactly one per listed rune and none for an unlisted one, 1–1000. A unit is bought only while its price is at or under the cap. Empty: three times each rune's base price (Mind/Body 9, the elementals 12, Chaos/Cosmic 45, Nature 60, Death 90, Law 120), except the classic ring's own default runes, which keep `Mind rune=18, Air rune=5, Chaos rune=90`. Why three: the price climbs 1% a rune below base at Aubury and Betty (3% at Lundail), so three times is reached 200 below base (67 at Lundail), a depth an Aubury or Betty elemental, mind or body line regrows in 20 minutes (Lundail 33); a circuit that returns sooner loses no regrowth, so a higher cap buys the same hourly flow at up to twice the price. Air at 5 buys only the top 50 of Aubury's and Betty's shelves and 17 of Lundail's per visit; six times the cost is any rune's ceiling. |
| `rotate` | string, `auto` | `on`: the rotation circuit; `off`: the classic ring; `auto`: rotate unless `shops` names exactly one shop (so the one-shop runners of 3 October keep the classic ring until `on` is set). |
| `rotationPhase` | int, -1 | The runner's place on the circuit: phases 0 to 2×worlds−1 (0–5 for three worlds) pick the direction (0–2 Betty first, 3–5 Lundail first) and how far the world order is turned. -1 takes it from a hash of the account name, which can collide: give each runner its own. -1–99 (taken modulo). |
| `maxHopsPerHour` | int, 12 | A rotating runner's hops in any hour; hops are also two minutes apart (worldhopplan's MinInterval). A hop that would wait more than five minutes skips its pair. A full three-world circuit is seven hops. 1–30. |
| `homeWorld` | int, 0 | The world the control panel logs the account in on at a start (0: the roster's). The script does not read it. Before 3 October this key was refused, so a runner whose settings carried it never started. 0–255. |
| `swarmWhileWaiting` | bool, true | Rotating only: after a circuit, when the next one is estimated to buy fewer than `swarmResumeRunes`, hand everything held to the mule (a delivery-only round, any amount) and run ScarabSwarm; leave it at a safe point (MasterCrafter's rules, with the east side's surface also counted) once the estimate reaches `swarmResumeRunes`. The phase is saved and survives restarts. |
| `swarmCheckMinutes` | int, 20 | How often the ScarabSwarm phase reads the restock estimate; 5–240. |
| `trawlerStintMinutes` | int, 20 | While master's trawler block is on, the Runner's trawler crew takes the account only between two rounds: the new round's delivery to the mule made and no shop of it visited (a user request: "acct should only join trawler if they have completed thier full shopping route"). It then stays this long, about one game (the gate is asked again at every game's leave-hop restart), and walks the round before its next stint; 0 stays until the block ends; 0–600. A held account logs `trawler crew: master's block is on (world N) but RuneRunner is not ready: ...` at most every 5 minutes, a join `trawler crew: round=N done and round=N+1's delivery made: joining the trawler crew`. |
| `trawlerRoundHoldMaxMinutes` | int, 180 | The longest an unfinished round (or the rest of a round after a stint) keeps the account off the crew; past it the account joins anyway and the join line says `joining anyway`. 0 is no cap; otherwise 10–1,440. A Mage Bank trip or a hop under way always holds. |
| `swarmResumeRunes` | int, 1500 | The runes a whole circuit is estimated to buy under the caps at or above which the runner goes (back) to the circuit; 1–100,000. A fully regrown Aubury or Betty pair is about 1,600 at the default caps, a Lundail pair about 700. |
| `swarmSettings` | object, `{}` | ScarabSwarm's own settings, passed through unchanged while it runs (a JSON string of an object is accepted from the panel); `mule` defaults to this runner's. Use the ScarabSwarm cohort's settings. It must not hold a `swarmSettings` of its own. |
| `tripCoins` | int, 1500000 | Bound on each coin draw out of the bank (one per pack-paid leg; the Lundail crossing draws only its own purchase at Gundai); 1,000–10,000,000. The round's whole purse is held in the bank (see `topUpBelow`), so a round dearer than this takes more than one draw. |
| `coinReserve` | int, 10000 | Coins left in the bank; at least 0. |
| `fareCoins` | int, 300 | Coins carried for the boat fares and never spent in a shop; 60–5,000. |
| `minVisitRunes` | int, 50 | Smallest expected purchase a station is walked to for (and the smallest Lundail plan the lever is pulled for); 1–10,000. |
| `hopMinGain` | int, 200 | Smallest expected purchase on the other world worth its two hops (rotating: worth one hop to the pair's world); at least 0. |
| `maxHopsPerRound` | int, 2 | Classic ring: 2 (the other world) or 0 (a one-world buyer). Rotating with more than one world it must stay 2 (`maxHopsPerHour` bounds the hops). |
| `lundailMaxDeaths` | int, 2 | Lundail deaths in a rolling 24 hours that stop Lundail visits until the oldest ages out; 1–10. |
| `deliverMinRunes` | int, 300 | Smallest delivery; at least 1. |
| `deliverMaxAgeMinutes` | int, 180 | Deliver whatever is held once the last delivery is this old; 10–1,440. |
| `muleWaitMinutes` | int, 10 | How long a deferred delivery waits at the post before banking the runes; 0–60. |
| `idleMaxMinutes` | int, 20 | Longest idle wait after a round that bought and delivered nothing; 1–120. |
| `tickMs` | int, 600 | Engine tick for restock estimates; 100–1,000 (the lab engine runs 200). |
| `checkpointDir` | string, `data/runerunner-checkpoints` | Root for `<account>/RuneRunnerState.json`. |
| `stopAfterRounds` | int, 0 | Finish after this many rounds (0 = never; for the lab). |
| `logDetail` | string, `Normal` | `Normal` or `Verbose`. |
| `zaffStaffs` | int, 0 | Staffs of air to buy from Zaff (Varrock) after each Aubury visit; 0 turns the Zaff stations off; 0–10. His shelf holds 2, one back every 1000 ticks. Bought staffs are handed to the mule with the runes. |
| `zaffStaffCap` | int, 1600 | The most one Staff of air may cost; 1–100,000. At 1600 both staffs of a full shelf (1500 and 1530) are bought. |
| `topUpBelow` | int, 150000 | When the draw station (at the mule's post, on the mule's world, the mule heard) reads bank plus pack under this, it asks the mule for coins over the wall bus, the ordinary item order. At most one ask per 20 minutes, persisted across hops. 0 turns the top-up off; 0–6,000,000. With `buyAll` on (the default) it is raised to the round's FULL BILL: every pack-paid shop on every world still ahead and Lundail, each valued at the stock the board expects when the round reaches it (the freshest sighting, restocked, holds ignored), plus 20% (never past a full shelf), Zaff's staffs, the fares, the ammo stations' estimate (14 October 2026: the ammo is part of the bill the mule is asked to fill) and `coinReserve`, at most 8,000,000. The round-start `purse round_need=... bill=... target=...` log line shows the figure, what the bank and pack held and what was asked; `purse FUNDED`, `purse SHORT` and the round's closing `purse round=... received=... spent=...` and `COINS SHORT` lines say what came, what was spent and what stays. |
| `topUpAmount` | int, 300000 | The ask when the mule's coin stack is NOT published (no bank read lately), and the least it asks then: the shortfall to the bill with `buyAll`, in at most 40 orders of at most 1,000,000 (the mule clamps each to its `coinsPerOrder`; live mule1 1,000,000). With `topUpWholeStack` (the default) and a published stack it is NOT the ask: the ask is the mule's whole stack above `muleCoinFloor` (14 October 2026, owner: "request mules entire cash stack and spend it on everything instead of just running out"; live that day the runner asked 300,000 of a mule that published 11,229,284 and the ammo stops skipped). The ask ignores, and does not feed, the fleet's remembered "none in the bank" refusals. The mule's `coinsPerOrder`, `coinsPerWindow` and `handoutPerDonor` bound what it gives (the live 1,000,000 / 15,000,000 / 16 carry an 11.2M stack in twelve orders; a larger one is stopped by the window and logs `MULE LIMIT?`, the rest comes at the next ask). 0 turns the top-up off; 0–6,000,000. |
| `topUpWholeStack` | bool, true | When bank plus pack is under the round's full bill (see `topUpBelow`), ask the mule's ENTIRE published coin stack above `muleCoinFloor`, in as many orders as its `coinsPerOrder` needs, instead of `topUpAmount` or the shortfall; the runner spends it on every rune shelf, Zaff's staffs and every enabled ammo shop, and what is not spent stays in its own bank for the next round (the ScarabSwarm phase it runs between circuits hands the mule no coin: `consolidateCoinsReserve` is 2,000,000,000 unless `swarmSettings` says otherwise; the delivery never carries coins). False asks `topUpAmount` clamped to the mule's spare, as before 14 October 2026. |
| `muleCoinFloor` | int, 10000 | Coins a top-up leaves in the mule's bank (0–10,000,000; 0 takes every coin). It was 100,000 until 14 October 2026. The one consumer that needs any is the cohort's small coin asks (a kit's 1,000-coin line, 3 to 4 an hour in the 14 October log; a fisher's 500-coin tool fare; a trainer's 3,500-coin blade purse): a mule found empty answers them "none in the bank" and `donorrunner.Refusals` replays that to every account for 30 minutes, although its inflow (about 774,000 an hour) refills it within minutes. 10,000 covers ten of the usual lines. The market station's own ask keeps the floor too. |
| `shortageFirst` | bool, true | When the mule's published bank holds fewer than 2,000 Nature, 3,000 Chaos, 2,000 Death or 5,000 Fire runes, the runner buys those first (Nature, Chaos, Death, Fire, ahead of the rest) and up to the shop's price ceiling (six times base: Chaos 90, Death 180, Nature 120, Fire 24) instead of the price caps; a rune the mule holds enough of keeps its place and cap. Nothing changes while the mule has published no bank. Logged as `shortage:` at the start of each run and round. Off keeps `runes` and `priceCaps` exactly as written. |
| `marketBuy` | bool, true | The player-market station (after the draw): when a Seers stall lists a rune the mule is short of at a price under the ceiling, walk to Seers, buy it with the bank's coins, bank it there for the next delivery and walk back. One cached page read per `marketCheckMinutes` when nothing is worth a trip. `naturemarket-1007` in COLLAB.md. |
| `marketRunes` | list, `Nature rune=20000`, `Death rune=6000`, `Fire rune=10000`, `Chaos rune=8000` | The mule's stock goals, in priority order; a rune is bought only while the mule's published bank plus what this runner already holds for it is under the goal, and only if it is in `runes` (so it is delivered). |
| `marketStalls` | list, Seers Market, Tradevault, Sil Stuff, S15 | The stalls asked; one that is not among the visible players at the Seers bank is left alone for 3 hours. |
| `marketPricePercent` | 100-400, 150 | The price ceiling as a percentage of the trade book's median SELL price over the newest 15 trade pages, never above the fixed hard ceiling (Nature 1000, Death 600, Fire 100, Chaos 300). A book with under two sell trades, or whose trades disagree by more than ten times, is not used: the hard ceiling stands alone. |
| `marketCoins` | 1000-10,000,000, 1,000,000 | The most one market station spends, from the account's bank less `coinReserve`; a bigger lot asks the mule for the difference (its `coinsPerOrder` bounds each order, `topUpAmount` must be above 0). The station spends only what the bank holds above the float the shops still need for the rest of the round. |
| `marketCheckMinutes` | 1-240, 15 | Least time between two looks at the market. |
| `marketMinLot` | 1-100,000, 100 | Smallest lot worth the walk (a shortfall under it is bought whole). |
| `marketTradeCoins` | 1000-5,000,000, 1,000,000 | The stall's per-trade coin limit; a lot over it is split into trades (at most 3 a station). |
| `ammoBuy` | bool, false | Rotation only (13 October 2026, [ammo design](runerunner_ammo_sell_2026-10-13.md)): adds ammo stations to the round, no extra hop. Hickton (Catherby) right after the delivery, before the draw, and again before the final hop home; Gerrant (Port Sarim, Feathers) beside every Betty visit; Lowe (Varrock, Bolts) beside every Aubury visit. What is bought is delivered to the mule with the runes. Lowest priority: a stop spends only the pack's coins beyond the fare and the whole rune and staff need of the stations still ahead, and a round that has left runes on a shelf for coins buys no more ammo; the mule is not asked for ammo coins. |
| `ammoShops` | string list, `["Gerrant","Lowe","Hickton"]` | The ammo shops to use; empty is the default. Also accepts PortKhazard and Aemad (placed with Lundail, Khazard before Aemad) and Ned (placed with Betty), which `trawlerRope` adds by itself. Dargaud (Ranged 40), Lathas (Biohazard), Gulluck (no route) and FishingGuild (dropped) are accepted and left out of the round with the reason in the start log. A shop is placed with its anchor: Gerrant with Betty, Lowe with Aubury. |
| `ammoItems` | string list, Bolts, Steel arrowtips, Feather, Mithril arrowtips, Adamant arrowtips, Rune arrowtips, Arrow shaft | Parts in priority order (a short purse or the budget leaves the last behind). No built shop sells Arrow shaft: only Dargaud does. Mithril, Adamant and Rune arrowtips have no fletching consumer yet. |
| `ammoBuyAll` | bool, true | Buy each part down to the shop's own price ceiling (the owner's buy-all); false: three times its cost. An `ammoPriceCaps` entry wins for its part. |
| `ammoPriceCaps` | string list, empty | `Item=gp`, 1-1000, only for the parts that need a cap of their own (unlike `priceCaps` there is not one per part). |
| `ammoStopAbove` | string list, Steel arrowtips 150000, Bolts 1000000, Feather 250000, Arrow shaft 300000, Mithril/Adamant/Rune arrowtips 20000 | `Item=count`: a part is not bought while the mule's published bank plus what the runner carries reaches the count (0 or no entry: no gate). With the mule's bank unread only Bolts, Steel arrowtips and Feather are bought. |
| `ammoCoins` | int, 150000 | Most one round spends on ammo, bought in priority order; 0-10,000,000, 0 is unlimited. Part of the round's full bill (`bill=` on the purse line, `ammo=` beside `round_need`), so the mule ask covers it; a budget that a full-shelf round exceeds (full shelves cost 79,694 at Hickton, 22,995 at Lowe and 9,250 at Gerrant a visit) is raised here, not in the code. An ammo stop spends the pack's coins beyond the fare and the part of the runes-ahead need the BANK cannot fund (14 October 2026), not the whole need: a bank that holds the runes ahead leaves the pack free for ammo. |
| `ammoMinVisitCoins` | int, 3000 | Smallest planned purchase, in coins, an ammo stop is walked to for; 0-1,000,000. |
| `ammoSlotReserve` | int, 1 | Free pack slots an ammo stop leaves for the rune stacks still to be bought; 0-10. |
| `trawlerRope` | bool, true | The Fishing Trawler's Rope round (4 November 2026). With `ammoBuy` on it adds **PortKhazard** and **Aemad** (placed with the Lundail pairs, Khazard first) and **Ned** (with the Betty pairs, when `ropeFloor` is above 0) to the placed ammo shops, and `Rope` to the ammo parts, whatever `ammoShops` and `ammoItems` list; an unlisted Rope takes an `ammoStopAbove` gate of 1000, a listed one keeps the operator's (0 is no gate). Off adds nothing. Supply, per world, from the `.inv` files (tick 0.6 s): Port Khazard shelf 30, one back per 20 ticks (300 an hour, a bought-out shelf whole in six minutes, 25-30 coins each); Aemad shelf 20, one per 100 ticks (60 an hour); Arhein (Catherby) 2 per 300 ticks (not on the circuit); Ned no shelf, 15 coins a conversation. A pack takes 24 single Ropes, so a world's Khazard + Aemad visit is about 44 and a three-world round about 130. |
| `ropeFloor` | int, 250 | The Rope the mule's published bank is kept at by Ned (Draynor, 15 coins a conversation, drawn at the Draynor bank, about 7 s a rope): he buys only the part of the floor the mule and this runner's own Rope leave short, at most one pack (24), none under 6, and the Ropes are banked at Draynor. 0 turns Ned off and leaves the two shelves. A two-hour trawler block spends 150-200. 0-5000. |
| `khazardRopeOnly` | bool, true | Buy only the Rope at Port Khazard, whatever `ammoItems` list: the Swamp paste (500, five a minute back) and the Bailing buckets (30 per 100 ticks) on that shelf are the trawler crew's own kit. The Khazard stop is also skipped on the world where the anchor's trawler block runs, and after a visit that bought 6 or more Ropes the pack is banked at East Ardougne before Aemad's stop. |
| `deliverNoted` | string list, `["Rope"]` | Unstackable ammo parts (Rope, Vial of water, Bailing bucket) the delivery draws from the bank as **bank notes**, so any count crosses in one trade slot. Loose, a delivery moved one pack of 24 Ropes a round whatever the bank held (the mule held 37 on 4 November 2026). Parts that stack are refused. |

## FishingTrawler (the crew loop)

Read by `trawlerplan.DecodeSettings` for the FishingTrawler script and for every other script's trawler crew (`runner_trawler.go`, ScarabSwarm's `trawlerCrew`); an unknown key is an error. Rope is the **menders' kit** (4 November 2026): a net mend spends one Rope and rolls `stat_random(crafting, 30, 300)`, a crew member under Crafting 40 only bails (`trawlerplan.Aboard`), and the Port Khazard store holds 30 a world.

| Key | Type / default | Meaning |
|---|---|---|
| `kitMendRope` | int, 8 | Rope an account with Crafting 40 or more carries (`trawlerplan.KitRopeFor`), the standing net crew 10: bought at the store, drawn from its own bank at a bank visit, and asked of the mule when it holds under 3 (`NeedsRope`), at most every 30 minutes and in the same ask as the alchemy kit (an account under Magic 55 asks for its Rope alone). 0-20, with `kitBuckets` at most 26. 0 turns the menders' kit off. |
| `kitRope` | int, 0 | Rope an account under Crafting 40 buys (it would only bail; 1 until 4 November 2026, when every crew member bought one and the first 30 to arrive took the store's shelf). 0-26. |
| `kitPaste` | int, 60 | Swamp paste bought at the store each trip (stacks). 0-500. |
| `bucket`, `kitBuckets` | bool true, int 1 | Bailing buckets. |
| `anchor`, `world`, `leaveAtSecondsLeft`, `maxFloodToLeave`, `alch`, `mule`, `logDetail` | | As in `fishingtrawler.go`'s header. |

MasterCrafter's anchor keeps 10 Rope (`trawlerAnchorRope`, Crafting 96 mends every rip) and tops it up from the store each game.

## ScarabSwarm

This script has a staged, evolving settings surface. Start with the [24 September handoff](scarab_handoff_2026-09-24.md), which separates main, live and unmerged branches. Do not copy defaults or settings from an unmerged branch into a roster assuming main implements them.

Core keys read by [scarabswarm.go](../internal/bot/scarabswarm.go):

| Key | Type / default | Meaning |
|---|---|---|
| `targetThievingLevel` | int, 5 | Stage 1 target. |
| `recoverDeathLoot` | bool, false | Experimental recovery of dropped funds. |
| `guideNpc` | string, `RuneScape Guide` | Tutorial guide. |
| `skipOption` | string, `Yes please` | Tutorial skip response. |
| `checkpointDir` | string, `data/scarabswarm-checkpoints` | Per-account checkpoint root, relative to working directory. |
| `maxStage` | int, 6 | Highest stage to enter; does not set the persisted current stage. |
| `tickMs` | int, 600 | Tick-based pacing; accelerated lab engines must set their own rate. |
| `stage2CombatExpected` | bool, true | Treat ordinary thieving combat as expected; tracked random events are still handled. |
| `muleAskStartSpread` | bool, true | Spread initial mule requests across the cohort; false is a lab-mechanics control. |

Stage-specific keys and their default/normalization authorities are indexed here rather than flattening a changing production plan into an unqualified sample roster:

| Setting family | Executable reader / default authority |
|---|---|
| Stage 2 food, funds and stall/ladder choices | `scarabSwarmStage2Settings` in [scarabswarm.go](../internal/bot/scarabswarm.go), [stage2.go](../internal/scripts/scarabplan/stage2.go) |
| Stage 3 spells, rune trips, acquisition, mule and prices | [scarabswarm_stage3.go](../internal/bot/scarabswarm_stage3.go), [stage3.go](../internal/scripts/scarabplan/stage3.go) |
| Cohort rune supply and market budget | [scarabswarm_runesupply.go](../internal/bot/scarabswarm_runesupply.go), [scarabswarm_marketledger.go](../internal/bot/scarabswarm_marketledger.go) |
| Stage 5 hitpoints | [scarabswarm_stage5.go](../internal/bot/scarabswarm_stage5.go), [survival.go](../internal/scripts/scarabplan/survival.go). For the later integrated trainer and quest settings, see the revision-pinned [complete ScarabSwarm reference](scarabswarm_settings_reference.md). |
| Queen kit, survival, loot and rally | [scarabswarm_stage6.go](../internal/bot/scarabswarm_stage6.go), [kq.go](../internal/scripts/scarabplan/kq.go) |

This section is an operator entry point and source index, **not an exhaustive table of every experimental ScarabSwarm knob**. The original Luna task named the four adapters in the main reference; this additional page makes the newer registrations discoverable without claiming their entire acceptance backlog is complete.

## ScarabSwarm inventory completion - 25 September 2026

The [normalized ScarabSwarm reference](scarabswarm_settings_reference.md) covers all 113 direct adapter keys at main `afe3237d`, including defaults, normalization, optional modes and compatibility-only keys. The shorter section above is historical context, not the exhaustive authority.
