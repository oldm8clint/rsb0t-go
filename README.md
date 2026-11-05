# rsb0t-go

> **rsb0t-go**, said "RS bot go".

> **Watch the launch video:** https://youtu.be/oh_qeoAd04I

A headless client for RuneScape 2 private servers on **revision 289**, such as the many servers built on the
LostCity engine (Engine-TS 289). Point it at any such server. It speaks the game's WebSocket protocol directly,
so there is no browser, no Playwright and no Electron: one small program can hold many accounts, run scripts on
them, and show all of it in a web panel.

- **One program, `rsb0t`.** Download it, run it, type your server's address, add an account. No Go toolchain,
  PowerShell or proxies needed.
- **Any revision-289 server.** Give it the server's website address; it finds the game connection, the game
  files and the login key itself, and checks them before anything logs in.
- **A panel in your browser** for every account: status, inventory, bank, loot, skills, logs, a map, the fleet
  at a glance. Try it on a simulated fleet before you add a real account.
- **33 scripts** (fishing, woodcutting, combat, crafting, thieving, quests, market buying, mule hand-overs ...),
  each a tested decision plan plus a thin runner, so they are easy to read and to extend.
- **One module dependency** (Gorilla WebSocket). Go 1.23 or newer to build from source.

> **Licence: [MIT](LICENSE).** Forks and pull requests are welcome ([CONTRIBUTING.md](CONTRIBUTING.md)). Use
> automation only where the server's rules and its owners allow it, and only on accounts you own.

> **Revision 289 only.** The client speaks exactly the game revision 289 protocol. A server on another revision
> will refuse every login (the panel then says "response 6"); that is not something a setting can fix.

## Quick start

You do not need to know anything about programming for this. It takes about five minutes.

1. **Download.** Open the [Releases page](https://github.com/oldm8clint/rsb0t-go/releases) and download the
   file for your computer (Windows, macOS or Linux). If no release is listed yet, see
   [Build from source](#build-from-source) below.
2. **Unpack.** Right-click the downloaded file and choose *Extract All* (Windows) or double-click it
   (macOS). You get a folder with a program called `rsb0t` in it.
3. **Start it.** Double-click `rsb0t` (on Windows `rsb0t.exe`). A small black window opens and stays open:
   that is the program running. Leave it open. Your web browser opens a page called **rsb0t-go setup** by
   itself. (If it does not, open your browser and go to <http://127.0.0.1:7274/>.)

   *Windows may say it "protected your PC". Click "More info", then "Run anyway". macOS may say it cannot check
   the developer: right-click `rsb0t`, choose Open, then Open again.*
4. **Enter your server's address.** At the top of the page, under **1. Your game server**, type the address of
   the server you play on: the address of its website or of the page where you play, for example
   `https://example-server.com`. The box starts with `rs2b2t.com` filled in, so leave it for that server or type
   your own. Press **Check connection**. After a few seconds you see a short list of
   checks, each marked **OK**, **Note** or **Problem**, and one sentence underneath that says whether rsb0t can
   use the server. When it says **Ready**, press **Use this server**.

   *If the sentence asks for a "login key" or a "game connection address", a box opens on the page that says
   what to type there. Both come from the server's staff if you cannot find them yourself. Fill the box and
   press Use this server again.*
5. **Add an account.** Under **2. Add an account**, type the account's login name and password. Use only
   accounts that are yours, and type the name exactly: on some servers, logging in with a name that does not
   exist yet creates a new account. Your password is kept only on this computer and is never shown again.
6. **Leave the script on "none" the first time** ([which script do I want?](docs/scripts.md)). The **Script** list says what the account does after it
   logs in. Leave it on *(none: just log in)* and press **Add**, so the first start only proves the login
   works. You can choose a script later under **3. Your accounts**, where **Settings** opens a short form
   for it.

   *Scripts marked "needs map data" walk around the world and need a map file that this download does not
   include (see [Data files for walking scripts](#data-files-for-walking-scripts)). Without it they refuse
   to start and say so. The scripts without the mark (such as `Alcher` or `CasketOpener`) start without it.*
7. **Press Start the panel.** The setup page turns into the panel: one card per account, showing it logging in
   and then what it is doing. That is it.

To stop, close the black window (or press `Ctrl+C` in it): every account logs out.

### Starting it again

Double-click `rsb0t` again. A **Welcome back** page opens with everything remembered: your server, accounts,
passwords, scripts and settings. Press **Start the panel**, or change anything on the page first. If you never
want to see that page, tick **Start straight away next time**; `rsb0t forget` brings it back.

Panel options you type after `--`, such as `rsb0t -- -listen 127.0.0.1:9000`, are remembered too, so you only
type them once. If rsb0t is already running, starting it again just opens the panel.

If something goes wrong after the window has closed, the last run's log is `last-run.log` in the `rsb0t-data`
folder. To try the demo, start rsb0t with a word after it, as the next section shows.

### Starting rsb0t with a word after it (setup, demo, ...)

A few things are started by typing `rsb0t` followed by a word, such as `rsb0t setup` or `rsb0t demo`. You type
that in a command window opened in the folder where `rsb0t` is:

- **Windows:** open the folder, click the address bar at the top of the window, type `cmd` and press Enter.
  In the black window that opens, type `rsb0t setup` (or `rsb0t demo`) and press Enter.
- **macOS:** open the Terminal app, type `cd ` (with a space), drag the folder onto the Terminal window,
  press Enter, then type `./rsb0t setup` and press Enter.
- **Linux:** open a terminal in the folder and type `./rsb0t setup`.

The words are:

| Type | What it does |
|---|---|
| `rsb0t setup` | Opens the setup page again: change the server, add or remove accounts, pick scripts. |
| `rsb0t demo` | **Try the demo first:** a pretend fleet of twelve accounts in the same panel. It connects to no game server and needs no account. |
| `rsb0t help` | Lists every command. `rsb0t server -h` and `rsb0t check-server -h` explain those two. |

### If something goes wrong

The panel shows a plain-English message on the account's card, and the black window shows the same. The
common ones:

| What you see | What it means |
|---|---|
| "Nothing answered at ..." | The address is wrong, the server is offline, or this computer is not online. |
| "This does not look like a LostCity revision-289 game server" | The address answers, but it is not a game server of this kind. Try the address of the page where you play, or a world address such as `w1.example-server.com`. |
| "rsb0t needs its login key" | The server's key could not be found by itself. Ask the server's staff for its "login public key" (two numbers) and paste them into the key boxes. |
| "The login key you pasted cannot be used" | A number in the key boxes is incomplete or has letters in it. Copy it again, or empty both boxes so rsb0t looks for the key itself. |
| "... is not its game connection" | The website answers, but the game runs on another address. Ask the server's staff for the game's address (it starts with `wss://`) and type it in the **Game connection address** box. |
| "the address is saved ..., but accounts cannot log in there until ..." | The server is remembered, so you can carry on adding accounts, but fix what is marked **Problem** (and press **Use this server** again) before you start the panel. |
| "response 6 ... out of date" | The server runs a different game revision than 289, or its key changed. Run **Check connection** again; if it still fails, this server cannot be used. |
| "response 7 ... world is full" | That world has no room. rsb0t keeps trying; give the account another world if the server has more. |
| "response 3 ... login name or password is wrong" | Remove the account on the setup page and add it again with the right password. |
| "no collision pack configured ... refusing to start" | That script walks and needs the map data. Choose *(none)* or a script without the "needs map data" mark. |

### The same from the command line

```sh
rsb0t check-server https://example-server.com   # read-only checks, nothing saved
rsb0t server https://example-server.com         # check it and save it
rsb0t account add MyAccount                     # asks for the password, not echoed
rsb0t account script MyAccount Alcher           # optional: choose a script
rsb0t                                           # start the panel
```

`rsb0t scripts` lists every script and `rsb0t help` every command. If the server's key cannot be found,
`rsb0t server https://example-server.com -modulus <first number> -exponent <second number>` saves it with the
key you pasted. If the game is on another address, add `-game wss://<that address>/`. A key you paste is kept
as it is: if the server changes its key later (some do when they restart), paste the new one.

Everything lives in one folder, `rsb0t-data`, beside the program, so moving the program's folder moves your
settings with it (`-data DIR` or the `RSB0T_HOME` variable put it elsewhere, and an `rsb0t-data` already in the
folder you start from is used first):

| File | What it is |
|---|---|
| `fleet.config.json` | The roster: the server, login pace, accounts and their scripts and settings. Never a password. |
| `options.json` | Your remembered panel options, and whether to skip the Welcome back page. |
| `last-run.log` | The last run's log. |
| `credentials.json` | Passwords, by login name, in plain text. On Linux and macOS only your user can read it; on Windows it has the folder's usual permissions, so keep the folder in your own user folder. Never share or commit it. |
| `panel-layout.json`, `stats-state.json`, `heatmap.json.gz` ... | What the panel saves between runs |

Add as many accounts as you like with `rsb0t account add`; `rsb0t account list` shows them, and
`rsb0t account remove NAME` removes one with its stored password. Logins are paced (three seconds apart by
default, `loginDelaySeconds` in the roster) so a big roster does not trip the server's flood protection
(its limit on logins from one address in a minute).
A server with several worlds is found when you give the address of one world (`w1.example-server.com`): the
check lists the worlds that answer, and an account can be given one with `"world": N` in the roster.

### Settings and the panel's options

A script's settings are JSON keys on the account in `fleet.config.json`, or are posted from the panel when you
start it. Defaults and meanings are in [docs/bot_script_settings.md](docs/bot_script_settings.md) and
[settings.example.json](settings.example.json).

The panel itself has options of its own (another port, the watchdog, ...). They go after two dashes, so rsb0t
knows they are for the panel and not for itself:

```sh
rsb0t -- -listen 127.0.0.1:9000 -watchdog act
```

### Data files for walking scripts

Scripts that walk use three generated files, named in the roster as `packPath`, `locsPath` and `configPath`:
a collision pack (which tiles are walls), a loc index (where doors, banks and trees are) and the game's config
archive (what every item and object is called). They are built from a local checkout of the public
Engine-TS 289 data, never committed:

```sh
go run ./cmd/cachecfgtest -http https://<your server>/ -save config.jag   # the config archive, from the server
go run ./cmd/locbake -engine <Engine-TS checkout> -config config.jag -out locs.lcli.gz
```

The collision pack (`collision.lcnav.gz`) is baked from the same checkout by a TypeScript tool that is not in
this repository yet, so for now the walking scripts need a pack someone has already made. See
[docs/fleet_config_reference.md](docs/fleet_config_reference.md) for the roster keys. Without these files an
account still logs in and the panel works; the walking scripts refuse to start and say what they are missing.

## Scripts

33 scripts: AdvancedFighter, Alcher, AntipoisonCollector, AntipoisonMaker, BankFletcher, BarbarianAgility,
BassOysterCaskets, BoneBurier, BowStringer, CasketOpener, CombatTrainer, EssenceMiner, FishingTrawler,
FlaxPicker, GemCutter, HerbCleaner, LootChestFisher, MarketBulkBuy, MasterCrafter, MeleeFlincher,
MuleReceiver, ProgressiveCrafter, ProgressiveFisher, ProgressiveThiever, ProgressiveWCFletch,
ProgressiveWoodcutter, QuestLadder, RuneCarrier, RuneRunner, ShopBuyout, SmelterBot, ToadCollector, VialFiller.
`rsb0t scripts` prints the list this build has.

**How to choose and start one:** [docs/scripts.md](docs/scripts.md) says what each script does, what it needs
first and which settings matter.

**Needs the map data:** every script except AdvancedFighter, Alcher, BoneBurier,
CasketOpener, MarketBulkBuy and MeleeFlincher refuses to start without a collision pack
(and those may still walk somewhere and need it then).

**Written for the server this project started on:** RuneRunner and MarketBulkBuy use that server's player
market (at Seers' Village), which a stock server does not have, so on another server they do not work. The
panel's hiscores, market-prices and world-map pictures are that server's pages too, so on another server they
stay empty. LootChestFisher, MuleReceiver and the hand-over settings of several scripts expect a fleet with a
mule account; on their own they fall back to their plain activity or do nothing.

Readiness differs by script. Each has a pure plan package under `internal/scripts/<name>plan` with unit tests,
a runner under `internal/scripts/<name>runner`, and a registration in `internal/bot`. To write your own, read
[docs/script_porting_framework.md](docs/script_porting_framework.md).

## Build from source

```sh
git clone https://github.com/oldm8clint/rsb0t-go.git
cd rsb0t-go
go build -o rsb0t ./cmd/rsb0t
go vet ./... && go test -p 1 ./...
```

Tests that need the generated data files skip themselves when the files are absent.

## How it fits together

```
rsb0t -> panel (internal/controlpanel) -> fleet (internal/fleet) -> scripts (internal/bot, internal/scripts)
      -> actions and queries (internal/actions) -> decoded state (internal/gamestate) -> protocol (internal/client)
```

Each account has its own WebSocket; the fleet manager paces logins and the panel reads the decoded session
state. [docs/architecture_overview.md](docs/architecture_overview.md) and
[docs/subsystem_diagrams.md](docs/subsystem_diagrams.md) have the detail.
[docs/trustworthy_harnesses.md](docs/trustworthy_harnesses.md) explains how the proof tools under `cmd/` are
meant to be read.
New to the codebase? Read [AGENTS.md](AGENTS.md) and [docs/agents/architecture.md](docs/agents/architecture.md).

## Forks, pull requests and new scripts are welcome

rsb0t-go is meant to be extended by other people. If you want a script that does not exist, a panel page, a
fix for a quirk of your server, or a better tool for the data files, **fork it and send a pull request**:

- Fork the repository, make a branch, and keep the checks green: `go build ./... && go vet ./... && go test -p 1 ./...`.
- A new script is three small pieces (a pure plan, a live runner, a registration), so a first contribution can be
  small. [docs/agents/adding_a_script.md](docs/agents/adding_a_script.md) walks through one end to end.
- Protocol changes are proven against vectors captured from the reference client, not by eye.
- The full process, the pull-request checklist and the issue templates are in [CONTRIBUTING.md](CONTRIBUTING.md).

Good first contributions are listed in [docs/agents/open_work.md](docs/agents/open_work.md): a collision-pack
baker in Go, an OS keychain for passwords, a login test on the setup page, more typed settings forms.

## Built with and for AI agents

Much of this code was written and reviewed by AI coding agents working together, and the repository carries
what they learned so the next agent (or person) does not have to learn it again. Start with
[AGENTS.md](AGENTS.md): how the project works, its conventions, how to test, and the traps that cost people days.
The detail is in [docs/agents/](docs/agents/README.md): an architecture map, how to add a script, the protocol
notes, testing with real game data, running a local game server, and the engineering lessons behind the rules.
If you point a coding agent at this repository, point it at `AGENTS.md` first; and if your agent learns something
this repository did not know, have it add that to `docs/agents/` in the same pull request.

## What is not done

- Accounts are managed on the setup page (`rsb0t setup`) or with `rsb0t account`, not inside the panel itself
  yet. The setup page's **Check connection** is read-only and never logs in, so it cannot confirm the server's
  revision: the first login does (a server on another revision answers response 6). Only scripts that declare a typed settings schema get a form
  there; the others take a JSON object.
- Passwords are stored in a plain file (`credentials.json`); an OS keychain is not used yet.
- The collision-pack baker is not in this repository yet (see above).
- The cheat tools for a local game server (`::tele`, `::give`) are for a server you run yourself. They are off
  unless you pass `-enable-cheats`, and a real server is never to be sent them.
- Some script settings still mention a swarm script that is not part of this release; those options do
  nothing here.
