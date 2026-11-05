# Choosing and starting a script

A script is the job an account does after it logs in: fish, cut gems, train combat, and so on. This page
shows how to pick one, which one fits your goal, and what each one needs before you press start.

> **Reminder.** Use automation only where the server's rules and its owners allow it, and only on accounts
> you own. If you are not sure, ask the server's staff first.

New here? Read [getting_started.md](getting_started.md) first. It covers installing `rsb0t` and adding your
server and accounts. This page starts where that one ends.

## Choosing and starting a script

### On the setup page

1. Open the setup page: double-click `rsb0t`, or run `rsb0t setup`. It opens in your browser.
2. Section **3. Your accounts** lists every account you added. Each row has a **Script** list.
3. Open the list and pick a script. A script marked **(needs map data)** walks around the world and will not
   start without a collision pack (see below). The mark disappears when the pack is found.
4. Press **Settings** at the end of the row. The page title becomes "Settings for NAME (SCRIPT)".
   - A few scripts have a real form with a label and a help line for each setting: CombatTrainer,
     GemCutter, HerbCleaner, RuneCarrier, RuneRunner, ShopBuyout and SmelterBot. Change what you want and
     press **Save settings**.
   - Every other script shows one box called **Settings (a JSON object)**. Leave it as `{}` to use the
     defaults. To change something, write the setting name and value, for example
     `{"target": "Big net", "mule": ""}`. Names are case-sensitive, and most scripts refuse a name they do
     not know, so a typo stops the start with a message.
5. Press **Start the panel**. Each account starts its script as soon as it has logged in.

You can also choose a script when you add an account, but the first time it is wise to leave **Script** on
*(none: just log in)*, press **Add**, and only then pick a script.

### With commands

Open a command window in the rsb0t folder (see getting_started.md for how), then:

```sh
rsb0t scripts                       # lists every script this build has
rsb0t account list                  # lists your accounts and the script each starts with
rsb0t account script NAME SCRIPT    # sets the script for the account called NAME
rsb0t account script NAME           # with no script: clears it, so the account only logs in
```

For example `rsb0t account script MyAccount Alcher`. The name must match one that `rsb0t scripts` prints,
including capital letters. On macOS and Linux put `./` in front of `rsb0t`. The commands change the saved
roster. A script change takes effect the next time the account starts.

### What you will see once it starts

The panel shows one card per account. After the login, the card shows what the script is doing, counters
such as items made or banked, and recent log lines. If a script cannot start, it says so in plain words on
the card and in the black window. The usual reasons are a missing map pack, a setting it does not accept,
or a missing item or level.

### Changing or stopping a script

- **Change the script.** Close the black window (every account logs out), run `rsb0t setup`, pick another
  script or edit **Settings**, and press **Start the panel** again. In the panel, an account's card may also
  have a script list with **Start**, **Stop**, **Restart script** and **Apply & restart** buttons. They
  appear when the panel is allowed to control the account.
- **Stop one script** without closing everything: use the card's **Stop** button, if it has one.
- **Stop everything.** Close the black window, or press `Ctrl+C` in it. Every account logs out.
- **Run out of work.** Many scripts end by themselves when their job is done. Each section below says
  what happens.

### What "needs map data" means

Scripts that walk between places need three generated files: a collision pack (which tiles are walls),
a loc index (where doors, banks and trees are) and the game's config archive (what every item and object is
called). This download does not include them. Without the collision pack, a walking script refuses to
start and says what is missing. The account still logs in and the panel still works. How the files are made
is in [../README.md#data-files-for-walking-scripts](../README.md#data-files-for-walking-scripts).

Scripts without the mark (AdvancedFighter, Alcher, BoneBurier, CasketOpener, MarketBulkBuy,
MeleeFlincher) start without the pack. They may still walk somewhere and then need it.
MasterCrafter carries no mark on the setup page, but it runs the code of a walking
script. Treat it as needing the pack.

### Mules and "a fleet"

A **mule** is an account that other accounts trade their goods to, and that hands out supplies on request.
Several scripts can hand what they gather to a mule, or ask a mule for supplies. They need a second account
running **MuleReceiver**, and the mule has to list the sender in its `allowedPartners` setting.

If you have only one account, set `mule` to `""` (an empty value) in the settings of any script that has
that setting. The script then keeps everything in its own bank. Scripts that cannot work without a mule say
so in their section.

### Written for one server

RuneRunner and MarketBulkBuy use a player market (at Seers' Village) that a stock server does not have. On
another server they will not work. The panel's hiscores, market-price and world-map pictures are also
that server's pages and may stay empty elsewhere.

## Which script do I want?

| Goal | Script | In one line |
|---|---|---|
| **Train Fishing** | ProgressiveFisher | Climbs from small net to sharks; can start a brand-new account |
| | BassOysterCaskets | Big-net fishing for caskets and oysters, opened as you go |
| | LootChestFisher | Fisher that also runs keys to a chest; needs a mule |
| **Train Woodcutting** | ProgressiveWoodcutter | Climbs the tree ladder to yews and beyond |
| | ProgressiveWCFletch | Woodcutting that also fletches the logs |
| **Train Thieving** | ProgressiveThiever | Picks pockets and steals from stalls by level |
| **Train Agility** | BarbarianAgility | Runs the Barbarian Outpost course |
| **Train Combat** | CombatTrainer | Walks a ladder of monsters to set stat goals |
| | AdvancedFighter | Fights at one spot you choose, with food and banking |
| | MeleeFlincher | Hits and hides at a safe tile |
| **Train Prayer** | BoneBurier | Buries bones from the ground or the bank |
| **Train Crafting** | ProgressiveCrafter | Fresh-start road to flax and bow strings |
| | GemCutter | Cuts uncut gems from the bank |
| **Train Smithing** | SmelterBot | Smelts bars at the Al Kharid furnace |
| **Train Herblore** | HerbCleaner | Cleans grimy herbs from the bank |
| | VialFiller | Fills vials at the Falador fountain |
| | AntipoisonMaker | Brews antipoison and hands it to a mule |
| **Train Fletching** | BankFletcher | Cuts logs into arrow shafts at the bank |
| | BowStringer | Strings unstrung bows from a mule's stock |
| **Train Magic** | Alcher | Casts alchemy on noted items from the bank |
| **Train Mining / Runecrafting supply** | EssenceMiner | Mines rune essence through Aubury or other gateways |
| **Make money** | Alcher | Turns noted items into coins |
| | ShopBuyout | Buys out a shop's stock within a budget |
| | CasketOpener | Opens every casket you hold |
| | BassOysterCaskets | Fishes for caskets and oysters |
| **Supply other accounts** | MuleReceiver | The mule itself |
| | RuneRunner | Buys runes in shops and trades them to the mule |
| | RuneCarrier | Carries rune essence to a crafting account |
| | FlaxPicker | Picks flax and banks it |
| | ToadCollector | Collects Toad's legs for the mule |
| | AntipoisonCollector | Collects Antipoison doses for the mule |
| | MarketBulkBuy | One-off bulk purchase from a player market stall |
| | FishingTrawler | Crew member for the Fishing Trawler minigame |
| **Quests** | QuestLadder | Runs a list of quests in order |
| **Main account** | MasterCrafter | Crafting rounds for a main account with a mule |

## The scripts

Each section names the setting keys exactly as you type them in the JSON box. "Map data" says whether the
setup page marks the script as needing the collision pack. Every script also reads `logDetail` (`Normal` or
`Verbose`) unless its section says otherwise; `Verbose` writes more to the log.

Settings marked "sets up a local test" (`cheatSetup` and similar) are for a private test server only. Leave
them off on a real account.

### AdvancedFighter

Fights at one place in melee. It picks targets, eats at a health threshold, banks for food and loot, buries
bones if you ask, and flees toward a bank if a real player attacks and you turned that on.

- **Start:** standing at the spot where you want to fight. That tile is the default `anchorTile`.
  Bring food, or have it in the bank.
- **Map data:** not marked.
- **Main settings:**
  - `targets` (empty): comma-separated monster names. Empty fights anything attackable.
  - `anchorTile` (your start tile): the fighting spot, as `"x,z"`.
  - `leashRadius` (10): how far from the spot it will go for a target or loot. Between 2 and 40.
  - `meleeStyle` (`strength`): `attack`, `strength`, `controlled`, `defence` or `progressive`.
  - `food` (empty): a food name. Empty uses any food it finds.
  - `foodWithdraw` (10): food taken from the bank per trip, 0 to 27.
  - `eatMode` (`smart`) and `eatAtHp` (50): `smart` or `threshold`; the threshold is a health percentage.
  - `buryBones` (false), `loot` (empty), `keep` (empty): bury bones, pick up things whose names contain
    these words, and never deposit these.
  - `autoBank` (true) and `bankAtLootSlots` (12): bank when this many loot slots are filled.
  - `panicHp` (25): health percentage at which it panics.
  - `fleeFromPlayers` (false): run to a bank when a player attacks.
- **Stops:** when you stop it, or when a goal you set is reached: `stopAtLevel` (for example
  `"attack:70"`), `stopAfterKills`, `stopAfterMinutes`, or `maxDeaths` (0 means no limit for all four).
- **Caveats:** melee only. It does not use prayer, potions, ranged or magic. It uses `verbose` (true or
  false) instead of `logDetail`. It can hand loot to a mule with `mule`, `muleAt` (0, which means never),
  `muleKeep` and `muleAtTile`.

### Alcher

Casts High or Low Level Alchemy on items taken from the bank as notes, and banks the coins.

- **Start:** next to a bank booth. It needs the items, Nature runes, and Fire runes or a fire staff. Your Magic level must allow the spell (21 for Low, 55 for High).
- **Map data:** not marked.
- **Main settings:**
  - `spell` (`Best for level`): or `High Level Alchemy`, or `Low Level Alchemy`.
  - `items` (`Rune platebody`): comma-separated item names, alched in the order listed.
  - `castsPerTrip` (100): notes and runes drawn per bank trip, 1 to 5000.
  - `useFireStaff` (true): wear a fire staff from the bank if there is one.
- **Stops:** by itself when the spell is not castable at your Magic level, no items are listed, the bank has
  none of the listed items left, the bank has no runes left, or the server refuses the spell for the level.
  The reason is written to the log.
- **Caveats:** it does not use a mule; the coins stay on the account. With no Nature runes anywhere it takes
  no items out to alch.

### AntipoisonCollector

Picks up the two Antipoison(1) potions that lie on the ground at the Observatory (north of Castle Wars), over
and over, and hands them to a mule.

- **Start:** anywhere; it walks to the Observatory. It needs map data to walk and open doors.
- **Map data:** needs it.
- **Main settings:**
  - `mule` (`mule`): the receiving account. Set it to `""` to never hand over; a full pack is then banked at
    the nearest bank.
  - `handOffAt` (20): doses held that make a hand-off due.
  - `handOffMinutes` (30): hand over whatever is held after this long since the last hand-off.
- **Stops:** it runs until you stop it.
- **Caveats:** ground items belong to a world, so run one collector per world. A hand-off needs a running
  MuleReceiver that lists this account in `allowedPartners`.

### AntipoisonMaker

Brews Antipoison(3) with Herblore and hands the potions to a mule. It trains Herblore as it goes.

- **Start:** it works from the bank at the mule's post, so start near the mule. Keep a few hundred coins in
  its own bank; the mule does not hand coins out.
- **Needs:** Marrentill comes from the mule in kits. The script buys vials of water at a shop in East
  Ardougne, hunts unicorns north of Catherby for horns, and grinds them with a pestle and mortar it buys
  once in Taverley.
- **Map data:** needs it.
- **Main settings:**
  - `mule` (a built-in name you will not have): the account that holds the Marrentill and takes the potions.
    Set it to your own mule. It cannot be empty.
  - `handOffAt` (27): Antipoison(3) banked before a hand-off, 1 to 28.
  - `kitSize` (300): Marrentill asked per order, 1 to 1000.
  - `shopVials` (27): vials of water bought per shop run, 1 to 27.
  - `huntHPFloor` (8): a unicorn hunt breaks off at or below this many hitpoints.
  - `maxFruitlessAsks` (3): asks in a row that bring nothing before the mule is given up on (0 never).
  - `backoffMinutes` (5): wait after a failed ask, shop run or hand-off, 1 to 120.
- **Stops:** it runs until you stop it.
- **Caveats:** needs a mule that lists the account in `allowedPartners` and holds Marrentill.

### BankFletcher

Cuts logs with a knife into arrow shafts, using the bank at Seers' Village.

- **Start:** anywhere; it walks to the Seers' Village bank. Keep a knife and logs in the bank.
- **Map data:** needs it.
- **Main settings:** `material` (`Logs`): the log type to cut. Only plain Logs has been proven to work.
- **Stops:** the script does not stop by itself when the bank runs out; it backs off and logs a warning,
  so stop it yourself.
- **Caveats:** cutting mode only. It does not string bows or attach arrowheads.

### BarbarianAgility

Runs the lap at the Barbarian Outpost agility course: ropeswing, log balance, obstacle net, balancing ledge,
ladder and three crumbling walls. It eats, banks for food and recovers after a fall.

- **Start:** anywhere; it walks to the course. You need Agility 35 (the gate guard checks it). Bring food or
  keep some in the bank.
- **Map data:** needs it.
- **Main settings:**
  - `maxLaps` (0): stop after this many laps; 0 runs until stopped.
  - `food` (`Lobster`): the food to eat and to draw from the bank.
  - `foodWithdraw` (20): food drawn per bank trip, 1 to 28.
  - `minFood` (1): it banks for food when it holds fewer than this, 0 to 28.
  - `eatBelowHp` (50): health percentage to eat at, 10 to 90.
  - `bankTile` (`2725,3491,0`): where it banks, at Seers' Village.
  - `lap` (empty): obstacle names separated by commas. Empty uses the standard lap.
  - `cheatSetup` (true) sets up a local test, so **set it to `false`** on a real account. `agilityLevel`
    (60) only matters for that setup.
- **Stops:** after `maxLaps` laps, or when you stop it.

### BassOysterCaskets

Fishes with the Big net at the Fishing Guild (or Catherby below Fishing 68) for caskets and oysters. It is
ProgressiveFisher with the Big net fixed, and it reads all of ProgressiveFisher's settings except `target`.

- **Start:** like ProgressiveFisher. It needs a Big fishing net (it can buy tools) and a Fishing level high
  enough for the Big net (16).
- **Map data:** needs it.
- **What it does in the middle:** when the pack is nearly full it opens caskets (coins, gems and key halves
  stay), joins key halves into a crystal key, opens oysters, cuts oyster pearls into Pearl bolt tips when
  your Fletching is 41 and you carry a chisel, and drops junk such as raw cod and seaweed.
- **Main settings (all default `true`):** `openCaskets`, `openOysters`, `cutPearls`, `cycleOnWalks`, and
  `fletchTo41`. The last one trains Fletching to 41 from logs a mule supplies, so set it to `false` if you
  have no mule. All ProgressiveFisher settings also apply, notably `mule` and `muleAt`.
- **Stops:** like ProgressiveFisher.

### BoneBurier

Picks up bones from the ground and buries them, or takes them from the bank and buries them.

- **Start:** standing where the bones are. It searches `radius` tiles around its start. For bank mode, stand
  near a bank.
- **Map data:** not marked.
- **Main settings:**
  - `source` (`ground`): `ground` or `bank`.
  - `names` (`bones`): name fragments to collect, comma-separated.
  - `bankNames` (`big bones,dragon bones`): bones to bank in split mode.
  - `disposal` (`bury`): `bury`, `bank` or `split`.
  - `radius` (15): search radius, 1 to 50.
  - `buryWhenFull` (false): collect a full pack before burying.
  - `idleMins` (0): stop, after one last bank trip, once nothing was found for this long, up to 120.
    0 waits forever.
  - `goalLevel` (0): write one log line when Prayer reaches this level; it does not stop the script.
- **Stops:** after `idleMins` with nothing to bury, or when you stop it.
- **Caveats:** no mule; bones stay in the account's own bank.

### BowStringer

Strings unstrung bows with bow strings so they sell or alch for more. It banks at a mule's post and draws
bows and strings from its own bank; when the bank runs out it asks the mule for a kit of noted bows and strings.

- **Start:** it works on a members world beside the mule. It needs Fletching high enough for a bow it can
  string.
- **Map data:** needs it.
- **Main settings:**
  - `mule` (`mule`): the account that holds the stock. It cannot be empty.
  - `bows` (every bow it knows): bow names; the highest one your Fletching level allows goes first.
  - `kitSize` (1000): bows asked per order, 1 to 100000.
  - `maxFruitlessKits` (3): kits in a row that bring no bow before that bow is dropped (0 never).
  - `orderBackoffMinutes` (5): wait after a deferred or empty kit, 1 to 120.
- **Stops:** it stops by itself when no bow is left to string at your Fletching level, or when strings
  fail three times in a row.
- **Caveats:** the mule must list this account in `allowedPartners`. Strung bows stay in this account's bank.

### CasketOpener

Opens every casket in the pack, clears the reward boxes, then checks the bank for more caskets.

- **Start:** next to a bank, holding or banking caskets.
- **Map data:** not marked.
- **Main settings:** `logDetail` only. There are no other settings.
- **Stops:** by itself when every casket is opened, or when caskets remain in the bank but the pack has no
  room for one.

### CombatTrainer

Raises Attack, Strength, Defence, Ranged, Magic and Prayer to goals by walking up a ladder of monsters
(cows, Al Kharid warriors, giants, moss giants, ice giants, ogres, fire giants), buying gear along the way.

- **Start:** anywhere; a brand-new account can start. It buys reachable gear from shops, so coins help.
  Food matters: without food it stops unless you allow it to fight on.
- **Map data:** needs it.
- **Main settings (a form on the setup page):**
  - `goalAttack`, `goalStrength`, `goalDefence` (75 each, 1 to 75), `goalRanged` and `goalMagic` (75 each,
    0 to 75; 0 turns that training off), `goalPrayer` (43, 1 to 99).
  - `magicElement` (`Fire`): `Fire`, `Wind`, `Water` or `Earth`.
  - `membersWorld` (true): include members-only steps. It does not change worlds.
  - `food` (empty): a food name, or empty to use what it has.
  - `eatAtHp` (50): health percentage to eat at, 10 to 90.
  - `buyGear` (true): buy gear from shops.
  - `gearTripBudget` (true): combine shopping into one budgeted trip per band.
  - `fightWithoutFood` (false): keep fighting when food runs short. By default it stops instead.
  - `rung` (`auto`): pick a rung yourself with `cows`, `alkharid`, `giants`, `mossgiants`, `icegiants`,
    `ogres` or `firegiants`.
- **Stops:** by itself when every goal is met. Also when food runs short, unless `fightWithoutFood` is on.

### EssenceMiner

Mines rune essence. By default it teleports through Aubury in Varrock to the essence mine, mines until the
pack is full, and banks at the Varrock east bank.

- **Start:** it needs the quest Rune Mysteries finished, because Aubury refuses the teleport without it, and
  a pickaxe (it uses the best one it holds, or buys better ones).
- **Map data:** needs it.
- **Main settings:**
  - `pickaxe` (`Best available`): or a tier such as `Rune`.
  - `mule` (empty): the receiving account. Empty means it never hands over.
  - `muleAt` (10000): banked essence that starts a hand-over; 0 never. For the supply gateways below the
    default is 5000.
  - `muleKeep` (empty): extra bank rows to keep. Pickaxes are always kept.
  - `gateway` (`aubury`): `aubury`, `guild` (Wizard Distentor in the Wizards' Guild, Magic 66) or
    `cromperty` (East Ardougne). The last two run a supply loop that mines in stretches of `batch`
    (270) essence, hands one note stack to the mule, and pauses while the mule's published essence has
    reached `muleStopAt` (150000).
  - `mineTickManip` (false) and `buyPickaxe` (false) are extras for the supply loop. They are not proven on
    a live server.
- **Stops:** it runs until you stop it.
- **Caveats:** loading is strict: an unlisted key stops the start with a list of problems.

### FishingTrawler

Crew member for the Fishing Trawler minigame. It buys a kit (Swamp paste, Rope, a bailing bucket) at the
Port Khazard general store, boards the boat, mends and bails, and at the end leaves the world so the catch
goes to the anchor.

- **Start:** it needs coins for the kit. It works with an **anchor**: one account that stays aboard to the
  end and claims the catch. In this release the anchor is MasterCrafter's trawler round.
- **Map data:** needs it.
- **Main settings:**
  - `anchor` (a built-in name you will not have): the anchor account's name. Set it to your own.
  - `world` (0): the world to play on; 0 is the account's own.
  - `kitPaste` (60) and `kitRope` (0): paste bought each trip, and Rope for an account that cannot mend
    (Crafting under 40).
  - `kitMendRope` (8): Rope for an account with Crafting 40 or more, 0 to 20.
  - `bucket` (true) and `kitBuckets` (1): bailing buckets.
  - `leaveAtSecondsLeft` (25, 5 to 59) and `maxFloodToLeave` (60): when it leaves the game.
  - `alch` (true): cast High Level Alchemy while waiting.
  - `mule` (empty): the account asked for a fire staff and Nature runes the bank lacks.
- **Stops:** it runs until you stop it.
- **Caveats:** useless without an anchor on the same world. In this build it does not hand fish to a mule.

### FlaxPicker

Picks flax in the field at Seers' Village and banks it.

- **Start:** anywhere; it walks to the field (around 2742,3452) and the Seers' Village bank.
- **Map data:** needs it.
- **Main settings:** `flaxName` (`Flax`), `pickOp` (`Pick`) and `bankBooth` (`Bank booth`) name the object, the
  action and the bank. There is rarely a reason to change them.
- **Stops:** it runs until you stop it. It has no mule.

### GemCutter

Cuts uncut gems from the bank with a chisel, at the Seers' Village bank.

- **Start:** a chisel and uncut gems in the bank. Your Crafting level decides which gems it cuts
  (opal 1, jade 13, red topaz 16, sapphire 20, emerald 27, ruby 34, diamond 43, and dragonstone beyond).
- **Map data:** needs it.
- **Main settings (a form):**
  - `gems` (empty): which gems to cut. Empty means every gem your level allows. Otherwise a comma-separated
    list of gem names.
  - `membersWorld` (true): include the members-only gems. It does not change worlds.
  - `bankBooth` (`Bank booth`): name of the bank object.
- **Stops:** by itself, with a message, when the bank holds none of the eligible gems. It also stops without
  a chisel.

### HerbCleaner

Cleans grimy herbs from the bank at the Seers' Village bank.

- **Start:** grimy herbs in the bank. Your Herblore level decides which herbs it cleans.
- **Map data:** needs it.
- **Main settings (a form):**
  - `herbs` (empty): which herbs to clean. Empty means every herb your level allows.
  - `membersWorld` (true): false leaves out herbs the server only allows on a members world.
  - `bankBooth` (`Bank booth`): name of the bank object.
- **Stops:** by itself, with a message, when the bank holds none of the eligible herbs.

### LootChestFisher

Fishes like ProgressiveFisher, and when it has enough crystal keys it runs them to a chest in Falador.
It reads all of ProgressiveFisher's settings.

- **Start:** like ProgressiveFisher. It needs a mule that hands out crystal keys.
- **Map data:** needs it.
- **Main settings:** `minKeys` (6): the key stock needed for a chest outing; a value under 1 resets to 6.
  An explicitly empty `mule` stops the start. `muleAtTile` defaults to `2725,3491,0`.
- **Stops:** like ProgressiveFisher. Without keys it falls back to plain fishing.
- **Caveats:** it deposits everything at the mule's bank and walks to Falador carrying nothing, because the
  road crosses a dangerous mountain. It never hands crystal keys back to the mule.

### MarketBulkBuy

A one-off bulk purchase from one stall of a player market at Seers' Village. It is meant to run on the
mule account, started by hand, never automatically.

- **Start:** at the mule's post with coins in its bank. Stop MuleReceiver first, and start it again after.
- **Map data:** not marked.
- **Main settings:** `quantity` (required) and `maxUnitPrice` (required, unless `quoteOnly` is true):
  how many to buy and the most one may cost. `unitPrice` (the cap), `item` (`Chaos rune`),
  `seller` (`Seers Market`), `tradeCoinLimit` (5000000), `quoteOnly` (false: only ask the price and buy
  nothing), `runId` (required for a real buy: a run with the same id will not run twice).
- **Stops:** after the purchase, "MARKETBULK DONE" is logged and the account idles at its post. Stop it by hand.
- **Caveats:** start with `quoteOnly`, then a tiny quantity. It also writes a spending record under
  the folder named by `checkpointDir`. It only works on a server with that market.

### MasterCrafter

An advanced script for a main account with a mule. Every `craftCheckEveryMinutes` (20) it reads the mule's
published bank. When the mule and this account hold enough raw material, it stops its other work at a safe
point, goes to the mule, does a crafting round, hands the product back, and carries on. Rounds include cutting
gems, stringing bows, pearls and bolts, arrows, herbs, superheating, smithing, and moving rarely used
rows out of a nearly full mule bank into its own bank.

- **Start:** a logged-in main account that can reach the mule's post.
- **Map data:** needs it, although the setup page does not mark it.
- **Main settings:** every key starts with `craft`. An unknown `craft` key is an error. The main ones:
  `craftGemCutting` and `craftBowStringing` turn the two core rounds on or off; `craftGemThreshold` (20000)
  and `craftBowThreshold` (20000) set how much material starts a round; `craftCheckEveryMinutes` (20);
  `craftKitSize` (1000); `craftMaxRoundMinutes` (120); `craftSafeHpPercent` (80); `craftIdle` (what to do
  between rounds). The remaining `craft` keys cover shops, superheating, smithing and storage.
- **Stops:** it runs until you stop it.
- **Caveats:** it is written to run beside a larger routine for a main account, and what it does between
  rounds is not covered here. Treat it as an expert script and read the key list in `internal/scripts/mastercraftplan/settings.go` before using it.

### MeleeFlincher

Hits a monster from outside its reach and hides before it can answer, using the monster's combat timer.

- **Start:** standing on a **safe tile** the monster cannot reach, with a monster nearby.
- **Map data:** not marked.
- **Main settings:** `target` (empty): the monster name; empty takes the nearest attackable one.
  `safeTile` (empty): `"x,z"` or `"x,z,level"`; empty uses your start tile.
- **Stops:** it runs until you stop it.
- **Caveats:** the safe tile must really be unreachable for the target.

### MuleReceiver

The mule. It waits at a post, accepts trades from the accounts you list, banks what arrives, and hands out
supplies to those accounts when they ask.

- **Start:** at the post, by default the Catherby bank (`2809,3441,0`). It is the account other scripts name
  in their `mule` setting.
- **Map data:** needs it.
- **Main settings:**
  - `allowedPartners` (**required**): the accounts it trades with, as a list or a comma-separated string.
    An empty list is refused.
  - `postTile` (`2809,3441,0`): where it waits. Fishers meet at `2725,3491,0`, so set the post to match.
  - `bank` (true): bank what it receives.
  - `maxTrades` (0): stop after this many accepted trades; 0 is unlimited.
  - `handout` (true): serve other accounts' orders. `handoutAnything` (true) also serves items outside the
    built-in catalogue, within `anyMaxPerOrder` (1000) and `anyPerWindow` (10000).
  - `coinsPerOrder` and `coinsPerWindow` (0 keeps 500 and 25000): coin limits for hand-outs.
  - `handoutPerDonor` (1) and `handoutWindowMin` (30): orders per account per window.
  - `travelToRequests` (false): walk to a group of accounts that ask for pickup.
- **Stops:** after `maxTrades`, or when you stop it.
- **Caveats:** coin hand-outs are capped, and uncatalogued orders never include Coins, crystal keys or key
  halves. These are the built-in defaults.

### ProgressiveCrafter

Takes a new account along a long road: appearance screen, tutorial skip, Lumbridge store, shearing and
spinning wool to Crafting 10, pickpocketing a boat fare, a pink skirt, the boats to Ardougne, then the Seers'
Village loop of picking flax, spinning it into bow strings and banking them.

- **Start:** a new account, or one already at Seers'. It also needs the collision pack, loc index and
  config archive.
- **Map data:** needs it.
- **Main settings:**
  - `cheatSetup` (true) sets up a local test, so **set it to `false`** on a real account.
  - `flaxSource` (`Pick it`): `Pick it`, `Bank first, then pick`, or `Mule, then pick` (asks the mule for
    flax notes). `flaxKitSize` (1000) is the amount asked.
  - `maxCycles` (0): stop after this many bow-string bank trips; 0 is unlimited.
  - `mule` (a built-in name): where bow strings go. `""` never hands over. `muleAt` (5000): banked strings
    that start a hand-over. `muleKeep` (`Coins, Shears`).
  - `fare` (60) and `pickpocketTarget` (`Man`): how it raises the boat fare.
  - `spinManip` (true) and `walkRub` (true): speed-ups.
- **Stops:** after `maxCycles`, or when you stop it.
- **Caveats:** a manual start from the panel without settings uses `cheatSetup: true`. Always pass your
  settings.

### ProgressiveFisher

Climbs the fishing ladder, banks the catch, and can start a brand-new account by raising the boat fare and
skipping the tutorial.

- **Start:** a new account, or any account. It buys the tools it needs (net, harpoon, cage).
- **Map data:** needs it.
- **Main settings:**
  - `target` (`Big net`): the highest rung to reach: `Small net` (level 1), `Big net` (16), `Harpoon` (35),
    `Lobster cage` (40) or `Harpoon sharks` (76).
  - `maxCycles` (0): stop after this many fish-and-bank cycles; 0 is unlimited.
  - `mule` (`mule`): the account for hand-overs. **Set it to `""` if you have no mule.**
  - `muleAt` (4000): fish banked, after the target is reached, that start a hand-over.
  - `eatFood` (false), `food` (empty), `foodCount` (3), `eatBelowHp` (50): carry and eat food.
  - `fare` (60), `pickpocketTarget` (`Man`): the fresh-start fare.
  - `cheatSetup` (false): a local test; leave it off.
- **Stops:** after `maxCycles`; otherwise it keeps fishing.

### ProgressiveThiever

Steals by level. Its default schedule is: Man (level 1), Warrior woman (25), Guard (40), Knight of Ardougne
(55), Paladin (70), Hero (80). It eats, banks loot and can ask a mule for food.

- **Start:** a new account works. With `suicide` on, an account with no food starts by thieving at
  Lumbridge and accepts dying.
- **Map data:** needs it.
- **Main settings:**
  - `schedule`: the level-to-method text above. Each step is `LEVEL: Method`.
  - `food` (`Lobster`), `foodWithdraw` (20), `minFood` (1), `eatBelowHp` (50): food handling.
  - `bankAt` (20): bank when this many loot slots are filled.
  - `suicide` (true): allow the no-food Lumbridge start.
  - `mule` (empty): empty disables hand-offs and food requests. `askMuleForFood` (true). `muleAt`
    (20000) is a bank-row count, not coins. `muleKeep` (`Coins`).
- **Stops:** it runs until you stop it.
- **Caveats:** some methods need a quest done first (for example Fremennik needs The Fremennik Trials); the
  script logs a gate message and says nobody is there to rob.

### ProgressiveWCFletch

Woodcutting that also fletches. It reads every ProgressiveWoodcutter setting, and adds:

- **Main settings:** `fletch` (true): turn fletching on; false behaves like ProgressiveWoodcutter.
  `normalLogs` (`shafts`): `shafts` or `bows` for plain logs. `muleAtFletched` (1000): fletched units held
  before a hand-over. `walkFletch` (true): cut logs while walking to the bank (needs a knife).
  `chopManip` (true) and `walkRub` (true): speed-ups.
- **Seating settings (all true):** `spreadRungOne`, `spreadMaple`, `mapleOverflow`, `spreadYew`,
  `spreadYewWorlds` (with `yewWorlds`, default `"1,2,3"`) and `spreadMagic` spread several accounts over
  the tree camps. With one account they change little; set `spreadYewWorlds` to `false` unless you want
  the account to hop worlds.
- **Start, map data, stops:** as ProgressiveWoodcutter. A knife is needed.

### ProgressiveWoodcutter

Chops by tree tier (normal, oak, willow, maple, yew and beyond, by Woodcutting level), banks logs, and
hands them to a mule if you set one.

- **Start:** a new account works: it raises the boat fare by pickpocketing, then starts at the Draynor
  Manor camp. It buys an axe if you give it a price and budget.
- **Map data:** needs it.
- **Main settings:**
  - `target` (`Yew`): the highest tier to chase.
  - `mule` (a built-in name): `""` disables hand-offs. `muleAt` (1000): logs banked and held before a
    hand-over. `muleKeep` (`Coins`). `muleAtTile` (`2725,3491,0`).
  - `startCamp` (true): the Draynor Manor start camp.
  - `axeWanted` (`Rune axe`), `axePrice` (0), `axeBudget` (0): buy an axe from the player market. Both
    price and budget must be above 0 or buying is off.
  - `fare` (60), `pickpocketTarget` (`Man`), `restBelowHp` (40): the fresh-start fare.
- **Stops:** it runs until you stop it.
- **Caveats:** the market axe buying uses the Seers' Village market that stock servers lack; leave
  `axePrice` and `axeBudget` at 0 there.

### QuestLadder

Runs quests in the order you list.

- **Start:** anywhere. Items it needs come from the bank, the world, or a mule.
- **Map data:** needs it.
- **Main settings:**
  - `quests` (`["waterfall"]`): quest ids in order. An unknown id stops the start.
  - `allowUnproven` (false): by default only Waterfall is accepted. Other quests exist but are not proven
    to work; turn this on to try them.
  - `mule` (empty): an optional supplier. `mulePost` (`2655,3286,0`).
  - `perQuestMinutes` (90) and `budgetMinutes` (360): time limits for one quest and the whole run.
  - `waterfallMaxDeaths` (2): deaths before it parks Waterfall.
  - `finishWhenDone` (true): ends the script when the ladder is finished.
- **Stops:** when every quest is done (if `finishWhenDone`), or when a budget runs out.

### RuneCarrier

A helper for MasterCrafter's rune-crafting work. It carries unnoted Rune essence from the Jungle Store on
Karamja to a crafting account that is standing at the Nature altar.

- **Start:** it needs a crafting account (the one named in `runecraftCrafter`) and a mule. Without both it
  does nothing useful.
- **Map data:** needs it.
- **Main settings (a form):**
  - `runecraftCrafter` (empty): the crafting account's name. Blank never starts a shift.
  - `mule`: where the kit comes from and leftovers go. It must list this account in `allowedPartners`.
  - `carrierKitEssence` (3000): noted essence asked per shift, 100 to 20000.
  - `carrierKitCoins` (10000): coins asked per shift, 500 to 1000000.
  - `carrierMinHP` (25): least maximum hitpoints, 10 to 99.
  - `carrierShelfFloor` (40): essence left on the shelf after a visit, 0 to 99.
  - `walkRub` (true): rub a Games necklace or Ring of dueling while running.
- **Stops:** it runs until you stop it.
- **Caveats:** an unlisted key stops the start. `walkAlch` must stay `false`.

### RuneRunner

Buys runes from the rune shops (Betty, Aubury and Lundail) under price limits and trades them to the mule.
It can also buy ammunition and Staffs of air.

- **Start:** it needs a mule that lists it in `allowedPartners`, and coins in its bank (it can ask the mule).
- **Map data:** needs it.
- **Main settings (a form):**
  - `mule` (a built-in name): the receiving account. It cannot be empty.
  - `shops` (`["Aubury","Betty","Lundail"]`) and `worlds` (empty: worlds 1, 2 and 3 when rotating).
  - `runes` (empty: every rune the shops stock) and `priceCaps` (empty: three times each rune's base price).
    A cap is `Rune=gp`, one per listed rune.
  - `rotate` (`auto`): `on` to circle every shop on every world, `off` for the classic two-world ring.
  - `tripCoins` (1500000) and `coinReserve` (10000): coin drawn per trip and kept in the bank.
  - `stopAfterRounds` (0): finish after this many rounds; 0 never.
  - `ammoBuy` (false): also buy ammunition.
- **Stops:** after `stopAfterRounds`, or when you stop it.
- **Caveats:** it uses the player market at Seers' Village for some buying, which only the original server
  has. It is not yet proven in a lab. An unlisted key stops the start.

### ShopBuyout

Buys the stock of one shop, banks it, and repeats until the budget or the coins run out.

- **Start:** near the shop's bank. It needs coins in its bank.
- **Map data:** needs it.
- **Main settings (a form):**
  - `shop` (`Aubury's runes — Varrock (Varrock East bank)`): or `Lowe's arrows and bows — Varrock (Varrock
    East bank)`, or `Arhein's store — Catherby (Catherby bank)`.
  - `buyItems` (empty): empty buys everything stocked; otherwise only the names listed.
  - `budgetGp` (250000): total to spend this session, at least 100.
  - `perTripGp` (100000): coins drawn per trip, at least 100.
  - `stopFloorGp` (5000): stop when the bank balance drops below this.
  - `recheckSeconds` (60): restock wait, 5 to 600.
- **Stops:** by itself once about the budget is spent or the bank falls below `stopFloorGp`. It banks what
  it bought first.

### SmelterBot

Smelts bars at the Al Kharid furnace and banks them.

- **Start:** ore (and coal where the bar needs it) in the bank. The bank is the Al Kharid bank.
- **Map data:** needs it.
- **Main settings (a form):** `bar` (`Bronze bar`): `Bronze bar`, `Iron bar`, `Silver bar`, `Steel bar`,
  `Gold bar`, `Mithril bar`, `Adamantite bar` or `Runite bar`. Short names such as `steel` also work. An
  unknown name is reported and the default is used. The Smithing level needed is 1, 15, 20, 30, 40, 50, 70
  and 85.
- **Stops:** by itself when the bank runs out of ore.
- **Caveats:** iron bars can fail to smelt, so XP varies.

### ToadCollector

Collects Swamp toads that lie on the ground in the Gnome swamp (around 2418,3514), removes their legs at
once, and hands the Toad's legs to a mule.

- **Start:** anywhere; it walks to the swamp.
- **Map data:** needs it.
- **Main settings:** `mule` (`mule`): `""` never hands over and a full pack is banked. `handOffAt` (24):
  legs held that make a hand-off due, 1 to 28. `handOffMinutes` (30).
- **Stops:** it runs until you stop it.
- **Caveats:** ground items belong to a world, so run one collector per world.

### VialFiller

Fills empty vials with water at the Falador fountain and banks them.

- **Start:** empty vials in the bank (or turn on `buyVials`). It uses a Falador bank.
- **Map data:** needs it.
- **Main settings:** `bank` (`Falador West`): or `Falador East`. `buyVials` (false): restock vials from
  Jatix in Taverley. `buyEveryRuns` (5): trips between restocks. `buyQty` (27): vials bought. `coinsPerTrip`
  (1000): coins drawn for a restock.
- **Stops:** it does not stop when the vials run out. It logs a warning, so stop it yourself.
- **Caveats:** no mule; filled vials stay in the bank.
