# AGENTS.md

Guidance for AI coding agents (Claude Code, Codex, Cursor, Copilot, Aider, ...) and for people who want the short
version. `CLAUDE.md` points here. This file is the rulebook; the reasons and the detail are in
[docs/agents/](docs/agents/README.md).

**rsb0t-go** (said "RS bot go") is a headless Go client for RuneScape 2 private servers on **client revision
289**. It speaks the game's WebSocket protocol directly (no browser, Playwright or Electron), so one process can
hold many accounts, run scripts on them, and serve a web panel. The only module dependency is Gorilla WebSocket.
The program you run is `rsb0t`.

Forks and pull requests are welcome, from people and from agents. [CONTRIBUTING.md](CONTRIBUTING.md) has the
process. If you learn something this repository did not know, write it down in `docs/agents/` in the same pull
request: that is how the project remembers.

## First five minutes

```sh
go build ./...
go vet ./...
go test -p 1 ./...                 # -p 1: the suite is large; this keeps a laptop usable
go run ./cmd/rsb0t demo            # a simulated fleet at http://127.0.0.1:7274/, no network, no account
```

- Go **1.23 is the floor** (`go.mod`). Do not use anything newer: `os.Root`, `testing.B.Loop` (1.24),
  `sync.WaitGroup.Go`, `testing/synctest` (1.25). `min`/`max`/`clear`, `slices`, `maps`, `log/slog`,
  `math/rand/v2` and per-iteration loop variables are fine.
- Tests that need real game data (collision pack, loc index, config archive) **skip themselves** when the
  data is absent. A green run without that data does not exercise walking. See
  [docs/agents/testing.md](docs/agents/testing.md) to build it.
- Run every gate bare and read its exit code (`go build ./... ; echo build=$?`). Never pipe a gate into
  `tail` or `grep`: the pipeline reports the last command's status, not the gate's.

## Set it up for a user

Follow this when a person asks you to "set rsb0t up" for them. It needs no source checkout: a released `rsb0t`
binary is enough (or `go run ./cmd/rsb0t` in place of `rsb0t` below). Every step is a command whose exit code
you can read; none of them needs the person's password to pass through you.

**0. Ask two things, and nothing else secret.** The server's address (its website, the page where they play, or
one world such as `w1.example-server.com`) and the login names of the accounts. Confirm the accounts are theirs
and that the server's rules allow automation; stop if not. Never ask for a password in chat, never put one on a
command line, in a file you write, or in a log you show.

Use the login names exactly as the person gives them, and never log in with a name they did not give you (no
"test" account): on many LostCity servers, and on a stock Engine-TS with no login server, a login with a name
that does not exist yet **creates that account**.

**1. Check the server (read-only).**

```sh
rsb0t check-server https://example-server.com ; echo exit=$?
```

It never logs in. Exit 0 means *Ready*; exit 1 prints which step failed and what to do. The steps are:
Reachable, Game files (`/crc0`, at least 36 bytes: the nine archive checksums), Login key (from
`/client/client.js`), Game connection (the WebSocket opens), Worlds. Read the verdict line to the person.

- *Game files* fails: not a LostCity revision-289 server at that address. Try the address of the play page, or
  `w1.<site>` (the check already tries `w1.` once by itself).
- *Login key* fails: the key is not in `client.js` in a form rsb0t recognises (`BigInt("<modulus>"),BigInt("<exponent>")`,
  or a 250+ digit run with exponent 65537). Fetch `<httpBase>client/client.js` yourself and look; the key is
  public (it is sent to every browser), so you may read it and pass it on: `-modulus <decimal> -exponent <decimal>`.
- *Game connection* gives a note about browser headers: fine; saving turns `browserHeaders` on.
- *Game connection* fails with "the web server answered there (HTTP ...)": the website is fine but the game
  WebSocket is on another port or path. Ask the person (or the server's staff) for the game address and add
  `-game wss://<host>:<port>/<path>`. Do not guess ports.
- A pasted key is kept verbatim and never re-read. If the server changes its key when it restarts, logins then
  fail with response 6 until the new key is pasted, so prefer leaving the key to be found when it can be.

**2. Save it.** `rsb0t server https://example-server.com` (add `-game ADDRESS` and/or `-modulus N -exponent N`
when step 1 needed them; `rsb0t server -h` lists them). It runs the same checks and, when the game files
answered, writes the roster `rsb0t-data/fleet.config.json`. It saves even when a later check failed (exit 1,
"accounts cannot log in there until ..."), so accounts can be added meanwhile; do not go on to step 4 until a
re-run exits 0. Changing to another server drops the old server's hand-written `worlds` entries.

| Key | What it is |
|---|---|
| `server` | the address as given |
| `wsURL` | the game WebSocket, e.g. `wss://w1.example-server.com/` |
| `httpBase` | the web base, ending in `/`, e.g. `https://w1.example-server.com/` (`/crc0` and `client/client.js` hang off it) |
| `modulus`, `exponent` | only when pasted; otherwise the key is re-read from `client.js` at every start, so a server that rotates its key keeps working |
| `worldList` | the worlds that answered, when there is more than one |
| `browserHeaders` | `true` when the game connection wanted an Origin header |
| `accounts` | `{username, script, world, settings}`; never a password |

`rsb0t server` with no address prints what is saved. Do not hand-edit `wsURL`/`httpBase` unless the person asks:
re-run `rsb0t server` (with `-game` for a separate game address) so the checks run again. The roster is read
leniently: an unknown or misspelt key in `fleet.config.json` is ignored, not an error, so read back what you
changed (`rsb0t server`, `rsb0t account list`) instead of assuming. (Script settings and `settings.json` are
the strict ones; see "Settings are strict" below.)

**3. Add the accounts without seeing a password.** Pick one way:

- The setup page: `rsb0t setup` opens it in the person's browser; they type each password there. This is the
  one to suggest to someone who does not use a terminal.
- Their own terminal: ask them to run `rsb0t account add NAME` themselves; it prompts and does not echo. It
  refuses piped input, so a password cannot be fed in by accident.
- Their password manager's command-line tool piping into `rsb0t account add NAME -password-stdin`, run by them.
  (`-password-env VAR` also exists, but the variable has to live in a shell you share, which is easy to get
  wrong: prefer the two above.)

Passwords land in `rsb0t-data/credentials.json`, in plain text. On Linux and macOS it is readable by the
person's user only; on Windows it has the folder's normal permissions, so keep the data folder in their own
profile. Never `cat` it, never commit it. Never use the panel's runtime add-account API (it takes a `password`
in the request body) from a command line you run: that puts the password in your shell history and transcript.
Then choose each account's script: `rsb0t scripts` lists them, `rsb0t account script NAME SCRIPT` sets one,
`rsb0t account list` shows the roster with "password stored" or "NO PASSWORD" per account. Leave the script
empty for the first start. Most scripts walk, and a walking script refuses to start without `packPath` (a
collision pack), which cannot be built from this repository yet; `locsPath` and `configPath` are the other two
data files (README, "Data files for walking scripts"). Only AdvancedFighter, Alcher, BassOysterCaskets,
BoneBurier, CasketOpener, MarketBulkBuy and MeleeFlincher start without a pack, and
RuneRunner and MarketBulkBuy need a player market the original server has and a stock one does not. Do not
assign a script that cannot start; say what it needs instead.

**4. Ask, then start, and read the log.** Starting the panel logs **every** account in the roster in and starts
its script. Before the first start, tell the person exactly that and wait for a yes. Start with one account and
no script; add the rest, and the scripts, once that one reaches `ingame`.

```sh
rsb0t -no-browser > rsb0t-data/rsb0t.log 2>&1 &     # or just `rsb0t` for the person, in their own window
```

The panel is on <http://127.0.0.1:7274/>. Read state rather than guessing:

- `curl -s http://127.0.0.1:7274/api/status` lists every account with `state`, `activity`, `lastError` and
  `botError`. `state` is one of `queued`, `connecting`, `ingame`, `reconnecting`, `error`, `stopped`,
  `offline`; an account that logged in is `ingame` with a growing `packets`.
- The log: read its tail (`tail -n 50 rsb0t-data/rsb0t.log`), never the whole file. Lines that start
  `controlpanel:` are startup; `[NAME]` lines are one account. Usernames and chat are in it: do not paste it
  into an issue unredacted.
- A startup line `could not read the server's login key` means step 1's key problem: fix with step 2.
- `login rejected with response 6 (...)`: different revision, or a wrong pasted key. Re-run step 1; if the key
  is found and 6 persists, the server is not revision 289 and rsb0t cannot be used there. Say so plainly.
- `response 7 (this world is full ...)`: retried by itself; more worlds in `worldList` spread the accounts.
- `response 3`: wrong password; the person removes and re-adds the account (`rsb0t account remove NAME`).
- `response 5`: the account is logged in elsewhere; it must be logged out there. Do not retry in a loop.

**5. Report** what you checked, what you saved (no passwords), and the panel address. To stop: `Ctrl+C` in the
window, or end the process you started; every account logs out.

`rsb0t demo` is the safe way to show someone the panel first: twelve pretend accounts, no server, no network.

## Rules that are not negotiable

1. **Only your own accounts, only where the server allows automation.** Try everything against a local engine
   first ([docs/agents/local_engine.md](docs/agents/local_engine.md)). Never aim a tool at a server you do not
   run without the person who owns the accounts saying so.
2. **No credentials in the repository.** Not in code, tests, fixtures, docs, logs, commit messages or issue
   text. Passwords come from an environment variable (`passwordEnv`) or a credentials file outside the
   repository, and are held as `config.Secret`, which prints as `[redacted]`. Tools must never print one.
3. **Cheats stay on a server you run.** The `::` commands are staff-only. A tool that sends them must take an
   explicit `-enable-cheats` flag, go through `internal/harness.Cheats`, and be pointed at a local engine.
4. **Be gentle with the machine.** Build and test with `-p 1`. Run one local game server at a time. Keep any
   single tool under about 2 GB of memory: stream a log, never load it whole. Put scratch files and test
   binaries outside the checkout, and delete them when you are done.
5. **Never invent a result.** If you did not run it, say you did not run it. See "Believing a test" below.

## How the code is organised

```
rsb0t -> panel (internal/controlpanel) -> fleet (internal/fleet) -> scripts (internal/bot, internal/scripts)
      -> actions and queries (internal/actions) -> decoded state (internal/gamestate) -> protocol (internal/client)
```

| Layer | Packages | Notes |
|---|---|---|
| Wire | `isaac`, `rsa`, `packet`, `protocol`, `transport`, `login`, `client` | Byte-exact against vectors captured from the reference client. `protocol` is generated. |
| State | `gamestate`, `archive`, `cachecfg`, `locindex`, `chatlog` | What the server said, decoded; what the cache says things are called. |
| Doing | `actions`, `pathfind`, `webwalk`, `locreach`, `randomevents` | Named actions (walk, bank, trade, shop, dialogue, op NPC/loc), A\*, long-distance routes, random events. |
| Scripts | `bot`, `scripts/*plan`, `scripts/*runner` | One pure plan, one live runner, one registration per script. |
| Fleet | `fleet`, `config`, `settings`, `mulebus`, `peerbank`, `muleledger` | Many sessions, paced logins, strict settings, hand-offs between accounts. |
| Panel | `controlpanel`, `controlpanelapp`, `cmd/rsb0t`, `cmd/controlpanel` | HTTP API and the embedded web UI; `rsb0t` is the friendly front door. |
| Proof | `harness`, `cmd/*` | Live proofs against a local engine. Read the trustworthy-harness rules before writing one. |

The full map, with the reasons for each boundary, is [docs/agents/architecture.md](docs/agents/architecture.md).

## Conventions

**Pure plan, live runner, thin registration.** Every script is three pieces and never fewer:

- `internal/scripts/<name>plan`: decisions as pure functions of plain values. No client, actions, fleet, bot or
  I/O imports, no clock except one passed in. Unit-tested in milliseconds.
- `internal/scripts/<name>runner`: the live glue that reads the session, builds the plan's input, acts on its
  answer. Shared by the registered script and by any `cmd/` proof tool, so the exact sequence that was proven
  is the sequence that runs.
- `internal/bot/<name>.go`: the `Bot` (`OnStart`, `Loop`, `OnStop`), its settings, and an `init()` that calls
  `Register("Name", ...)`. `grep -rn 'Register(' internal/bot/*.go` is the real list of scripts.

[docs/agents/adding_a_script.md](docs/agents/adding_a_script.md) is the recipe.

**Settings are strict.** `internal/config` (`settings.json`) and each script's `DecodeSettings` reject unknown
keys, wrong case, `null`, out-of-range values and duplicate usernames (compared by userhash, so `Bob_Smith`
equals `bob smith`). A typo must be an error, not a silently ignored knob. `settings.example.json` states
every knob at its default, and a test fails if the two drift: change both together.

**Protocol authority.** The reference client is the authority for bytes: its `ClientProt.ts` sets
`CLIENT_VERSION = 289`, and its stream, packet, ISAAC and client files define the wire. Protocol code is proven
first by vectors captured from the real TypeScript (`*_reference_vector.ts` at the repository root write
`test/fixtures/*.json`; the Go tests compare byte for byte), then confirmed live against a local engine.
Opcode tables are generated (`go run ./internal/protocol/gen -engine <Engine-TS checkout>`), never edited by hand.

**Trustworthy harnesses.** Every tool under `cmd/` prints a verdict someone acts on. Use `internal/harness`:
`Cheats` refuses to run rather than silently send nothing, `Teleport` is not done until the tile is observed,
and `Verdict` cannot pass without a check that actually ran, and exits 2 (not 0 or 1) when a precondition was
never met. [docs/trustworthy_harnesses.md](docs/trustworthy_harnesses.md) lists every way this project's own
tools lied before this existed.

**Comments say why.** The code base explains its reasons in doc comments, often with the failure that taught
them. Keep that habit: a fix with no recorded cause gets undone by the next person.

## Believing a test

- A check must be **false until the thing has happened**. Assert what was gained (XP, items, kills), not where
  the account stood. A location is not an activity.
- **Read state twice**, before and after. One reading cannot tell "it worked" from "it was already like that".
- A passing run with the real-data packs absent proves less than it looks. Say which packs you ran with.
- A short measurement window flatters a change. Quote the window, and measure the mechanism (a walk's length)
  before the outcome (throughput), because throughput is noisy.
- If you cannot make a diagnostic trustworthy cheaply, say so in its doc comment rather than leaving a
  confident-looking PASS in place.

## Git and pull requests

- Fork, branch, commit small, subject line one imperative sentence describing the change.
- Before pushing: rebase on the upstream main, then `go build ./... ; go vet ./... ; go test -p 1 ./...`
  (read each exit code), and `gofmt -l` on the files you touched.
- Never force-push a shared branch, and never rewrite history that others have pulled.
- Protocol changes include the reference vector. New scripts include the plan's tests. Behaviour changes
  include a log line that lets a reader of a run judge the change.
- Update the docs your change makes untrue. Update `docs/agents/` when you learn something.

## Where to look next

| I want to ... | Read |
|---|---|
| understand the layers | [docs/agents/architecture.md](docs/agents/architecture.md) |
| add or change a script | [docs/agents/adding_a_script.md](docs/agents/adding_a_script.md), [docs/script_porting_framework.md](docs/script_porting_framework.md) |
| avoid known traps | [docs/agents/gotchas.md](docs/agents/gotchas.md) |
| touch the wire protocol | [docs/agents/protocol.md](docs/agents/protocol.md) |
| run the real-data tests | [docs/agents/testing.md](docs/agents/testing.md) |
| run a game server to test against | [docs/agents/local_engine.md](docs/agents/local_engine.md) |
| learn why the rules exist | [docs/agents/lessons.md](docs/agents/lessons.md) |
| find something to work on | [docs/agents/open_work.md](docs/agents/open_work.md) |
| write a live proof tool | [docs/trustworthy_harnesses.md](docs/trustworthy_harnesses.md) |
| configure accounts and scripts | [docs/fleet_config_reference.md](docs/fleet_config_reference.md), [docs/bot_script_settings.md](docs/bot_script_settings.md) |
